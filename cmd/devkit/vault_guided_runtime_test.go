package main

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Invoked only by the PTY runner. OpenBao and all terminal operations are real;
// outward host/network effects use existing seams, never the user's services.
func TestGuidedSetupLocalHostProcess(t *testing.T) {
	root := os.Getenv("DEVKIT_GUIDED_SETUP_ROOT")
	project := os.Getenv("DEVKIT_GUIDED_SETUP_PROJECT")
	if root == "" {
		t.Skip("use tests/guided_setup_acceptance.py")
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Base(root), "devkit-guided-") || !strings.HasPrefix(project, "devkit-guided-") {
		t.Fatal("unsafe test identity")
	}
	for key, value := range map[string]string{"DEVKIT_CONFIG_DIR": filepath.Join(root, "config"), "DEVKIT_STATE_DIR": filepath.Join(root, "state"), "HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "home", ".config"), "XDG_RUNTIME_DIR": filepath.Join(root, "runtime"), "DEV_DOMAIN": ""} {
		t.Setenv(key, value)
	}
	for _, dir := range []string{configDir(), stateDir(), os.Getenv("HOME"), os.Getenv("XDG_RUNTIME_DIR")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// All external provider traffic stays in this process's loopback fixture.
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" || r.Header.Get("Authorization") != "Bearer guided-dns-SENTINEL" {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"result":[{"id":"fixture-zone"}]}`)
	}))
	defer cf.Close()
	swap(t, &cloudflareAPI, cf.URL)
	swap(t, &tailscaleIP, func() (string, error) { return "100.64.0.9", nil })
	swap(t, &lookupHost, func(string) ([]string, error) { return []string{"100.64.0.9"}, nil })
	swap(t, &servedCertificate, func(ip, host string) (*x509.Certificate, error, error) {
		return &x509.Certificate{DNSNames: []string{host}, NotAfter: time.Now().Add(60 * 24 * time.Hour), Issuer: pkix.Name{CommonName: "ISOLATED CERTIFICATE FIXTURE"}}, nil, nil
	})
	swap(t, &acmeReachable, func(string) error { return fail("public ACME is forbidden in this test") })
	swap(t, &setupProxy, func() error {
		cfg, err := loadHostConfig()
		if err != nil {
			return err
		}
		compose, dynamic := proxyFiles(cfg, "guided.example.test", []string{"100.64.0.9"})
		for name, value := range map[string]any{"compose.json": compose, "dynamic.json": dynamic} {
			b, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if err := writePrivate(filepath.Join(proxyDir(), name), b); err != nil {
				return err
			}
		}
		return nil
	})
	actualRun := runCommand
	swap(t, &runCommand, func(args []string, opts runOpts) (string, error) {
		command := strings.Join(args, " ")
		switch {
		case command == "docker version --format {{.Server.Version}}", command == "docker compose version --short":
			return actualRun(args, opts)
		case len(args) == 6 && args[0] == "docker" && args[1] == "compose" && args[2] == "-f" && args[3] == filepath.Join(vaultDir(), "openbao", "compose.json") && strings.Join(args[4:], " ") == "up -d":
			return actualRun(append([]string{"docker", "compose", "-p", project}, args[2:]...), opts)
		case strings.HasPrefix(command, "systemctl --user "):
			switch strings.Join(args[2:], " ") {
			case "show-environment", "daemon-reload", "enable devkit-web", "restart devkit-web":
				return "", nil
			case "is-active devkit-web":
				return "active", nil
			}
		case len(args) == 5 && args[0] == "loginctl":
			return "yes", nil
		case command == "docker network inspect --format {{.Name}} devkit-proxy":
			return "devkit", nil
		case command == "docker ps -aq --filter label=com.docker.compose.project=devkit-proxy":
			return "fixture-proxy", nil
		case command == "docker inspect fixture-proxy":
			return fmt.Sprintf(`[{"Id":"fixture-proxy","Name":"fixture-proxy","Config":{"Image":"fixture-only","Labels":{"com.docker.compose.project.working_dir":%q}},"State":{"Status":"running","Running":true}}]`, proxyDir()), nil
		}
		return "", fail("Test refused outward command: %s", command)
	})
	// setup is the production coordinator; only the above external boundaries vary.
	if err := setup(setupOptions{domain: "guided.example.test", email: "test@example.test", harness: "none", secretsProvider: "openbao"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath()); err != nil {
		t.Fatal("service definition not written in disposable home")
	}
	for _, path := range []string{filepath.Join(os.Getenv("HOME"), ".agents", "skills", "devkit-secrets"), filepath.Join(os.Getenv("HOME"), ".claude", "skills", "devkit-secrets")} {
		if secretsSkillStatus(path) != "current" {
			t.Fatal("skill installation incomplete")
		}
	}
	fmt.Fprintln(stdout, "GUIDED_SETUP_COMPLETE")
}
