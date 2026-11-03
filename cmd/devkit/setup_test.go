package main

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// hostWith writes a host config into a fresh config directory.
func hostWith(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVKIT_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDomainComesFromTheConfigUnlessDevDomainIsSet(t *testing.T) {
	hostWith(t, `{"domain": "Dev.Example.com."}`)
	t.Setenv("DEV_DOMAIN", "")
	if got, err := domain(); err != nil || got != "dev.example.com" {
		t.Errorf("domain() = %q, %v", got, err)
	}
	t.Setenv("DEV_DOMAIN", "other.example.org")
	if got, _ := domain(); got != "other.example.org" {
		t.Errorf("DEV_DOMAIN should win, got %q", got)
	}
	hostWith(t, `{"domain": "dev.example.com`+"`"+`)"}`)
	t.Setenv("DEV_DOMAIN", "")
	if _, err := domain(); err == nil {
		t.Error("injected domain accepted")
	}
}

func TestSecretSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	os.WriteFile(path, []byte(" from-file \n"), 0o600)
	if got, err := (secretSource{File: path}).read(); err != nil || got != "from-file" {
		t.Errorf("file: %q, %v", got, err)
	}
	os.Chmod(path, 0o640)
	if _, err := (secretSource{File: path}).read(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("group-readable file accepted: %v", err)
	}
	t.Setenv("DEVKIT_TEST_TOKEN", "from-env")
	if got, err := (secretSource{Env: "DEVKIT_TEST_TOKEN"}).read(); err != nil || got != "from-env" {
		t.Errorf("env: %q, %v", got, err)
	}
	if _, err := (secretSource{Env: "DEVKIT_TEST_UNSET"}).read(); err == nil {
		t.Error("unset variable accepted")
	}
	var ran []string
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		ran = args
		return "from-command", nil
	})
	if got, err := (secretSource{Command: "op read op://dev/cf/token"}).read(); err != nil || got != "from-command" {
		t.Errorf("command: %q, %v", got, err)
	}
	if !slices.Equal(ran, []string{"sh", "-c", "op read op://dev/cf/token"}) {
		t.Errorf("ran %q", ran)
	}
	for _, s := range []secretSource{{}, {File: path, Env: "X"}} {
		if _, err := s.read(); err == nil {
			t.Errorf("%+v accepted", s)
		}
	}
}

func TestShareTokenCanComeFromTheEnvironment(t *testing.T) {
	hostWith(t, `{"share": {"domain": "share.test", "account_id": "acct", "token": {"env": "DEVKIT_TEST_CF"}}}`)
	t.Setenv("DEVKIT_TEST_CF", "env-token")
	cfg, ok, err := loadShareConfig()
	if err != nil || !ok || cfg.token != "env-token" {
		t.Errorf("token %q ok=%v err=%v", cfg.token, ok, err)
	}
	// The older token_file key still works.
	hostWith(t, `{"share": {"domain": "share.test", "account_id": "acct", "token_file": "/nonexistent/token"}}`)
	if _, _, err := loadShareConfig(); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("token_file ignored: %v", err)
	}
}

func proxyHost() hostConfig {
	cfg := hostConfig{Domain: "dev.example.com",
		Harness: &harnessConfig{Name: "T3 Code", Host: "t3", URL: "http://127.0.0.1:3773"},
		Proxy: &proxyConfig{Email: "me@example.com", DNSProvider: "cloudflare",
			DNSEnv: map[string]secretSource{"CF_DNS_API_TOKEN": {Env: "X"}}}}
	cfg.Web.Port = 3780
	return cfg
}

func TestProxyFilesBindTheTailscaleIPAndRouteTheDashboardAndHarness(t *testing.T) {
	cfg := proxyHost()
	compose, dynamic := proxyFiles(cfg, "dev.example.com", []string{"100.64.0.9"})
	traefik := object(object(compose, "services"), "traefik")
	command := traefik["command"].([]string)
	for _, want := range []string{"--entrypoints.websecure.address=100.64.0.9:443",
		"--certificatesresolvers.dns.acme.storage=/certs/acme.json",
		"--certificatesresolvers.dns.acme.caserver=" + productionCA,
		"--certificatesresolvers.dns.acme.dnschallenge.provider=cloudflare"} {
		if !slices.Contains(command, want) {
			t.Errorf("missing %s in %q", want, command)
		}
	}
	if traefik["image"] != defaultTraefik || compose["name"] != proxyProject {
		t.Errorf("image %v, name %v", traefik["image"], compose["name"])
	}
	if env := object(traefik, "environment"); !strings.HasPrefix(env["CF_DNS_API_TOKEN"].(string), "${CF_DNS_API_TOKEN:?") {
		t.Errorf("the token must be interpolated, not written: %v", env)
	}
	routers := object(object(dynamic, "http"), "routers")
	if object(routers, "devkit")["rule"] != "Host(`devkit.dev.example.com`)" || object(routers, "harness")["rule"] != "Host(`t3.dev.example.com`)" {
		t.Errorf("routers %v", routers)
	}

	cfg.Proxy.Staging, cfg.Harness = true, nil
	compose, dynamic = proxyFiles(cfg, "dev.example.com", []string{"100.64.0.9"})
	command = object(object(compose, "services"), "traefik")["command"].([]string)
	if !slices.Contains(command, "--certificatesresolvers.dns.acme.storage=/certs/acme-staging.json") ||
		!slices.Contains(command, "--certificatesresolvers.dns.acme.caserver="+stagingCA) {
		t.Errorf("staging not used: %q", command)
	}
	if _, has := object(object(dynamic, "http"), "routers")["harness"]; has {
		t.Error("harness routed without a harness")
	}
}

func TestProxyConfigRejectsInjection(t *testing.T) {
	for _, change := range []func(*proxyConfig){
		func(e *proxyConfig) { e.Email = "me@example.com --api.insecure" },
		func(e *proxyConfig) { e.Email = "${HOME}@example.com" },
		func(e *proxyConfig) { e.DNSProvider = "cloudflare --x" },
		func(e *proxyConfig) { e.DNSEnv = map[string]secretSource{"bad key": {Env: "X"}} },
		func(e *proxyConfig) { e.DNSEnv = nil },
	} {
		e := *proxyHost().Proxy
		change(&e)
		if err := checkProxy(&e); err == nil {
			t.Errorf("accepted %+v", e)
		}
	}
	if err := checkProxy(proxyHost().Proxy); err != nil {
		t.Error(err)
	}
	for _, h := range []harnessConfig{{Host: "t3.evil"}, {URL: "http://x`)"}, {URL: "file:///etc"}} {
		if err := checkHarness(&h); err == nil {
			t.Errorf("accepted %+v", h)
		}
	}
}

func TestProxyUpPassesSecretsOnlyThroughTheEnvironment(t *testing.T) {
	cfg := proxyHost()
	data, _ := json.Marshal(cfg)
	dir := hostWith(t, string(data))
	t.Setenv("X", "cf-secret")
	t.Setenv("DEV_DOMAIN", "")
	swap(t, &tailscaleIP, func() (string, error) { return "100.64.0.9", nil })
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	var up runOpts
	var calls [][]string
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		calls = append(calls, args)
		if slices.Contains(args, "up") {
			up = o
		}
		return "", nil
	})
	if err := proxyUp(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(up.env, "CF_DNS_API_TOKEN=cf-secret") {
		t.Error("compose did not get the token")
	}
	for _, name := range []string{"compose.yaml", "dynamic.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, "proxy", name))
		if err != nil || strings.Contains(string(data), "cf-secret") {
			t.Errorf("%s: %v, or it holds the secret", name, err)
		}
	}
	if !proxyCurrent(cfg, "dev.example.com", "100.64.0.9") {
		t.Error("files on disk do not match the config")
	}
	cfg.Web.Port = 9999
	if proxyCurrent(cfg, "dev.example.com", "100.64.0.9") {
		t.Error("a changed config should make the files stale")
	}
	if !slices.ContainsFunc(calls, func(c []string) bool {
		return slices.Equal(c, []string{"docker", "network", "inspect", "--format", "{{.Name}}", proxyNetwork})
	}) {
		t.Errorf("network not checked: %q", calls)
	}
}

func TestHarnessWorktreesAreWatchedOnlyWhenConfigured(t *testing.T) {
	hostWith(t, `{"domain": "dev.example.com"}`)
	if got := watchedRepositories(); got != nil {
		t.Errorf("watched %q without a harness", got)
	}
}

func TestWebUnitRunsThisBinaryWithoutEnvironment(t *testing.T) {
	unit := webUnit("/opt/dev box/devkit")
	if !strings.Contains(unit, `ExecStart="/opt/dev box/devkit" web`) || strings.Contains(unit, "DEV_DOMAIN") {
		t.Error(unit)
	}
}

func TestSetupTriesStagingBeforeProduction(t *testing.T) {
	hostWith(t, `{}`)
	swap(t, &acmeReachable, func(string) error { return nil })
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	swap(t, &certificatePoll, time.Millisecond)
	stage := "none" // what Traefik serves: none, staging, production
	var ups []bool
	proxy := proxyHost().Proxy
	swap(t, &setupProxy, func() error {
		ups = append(ups, proxy.Staging)
		if proxy.Staging {
			stage = "staging"
		} else {
			stage = "production"
		}
		return nil
	})
	swap(t, &servedCertificate, func(ip, host string) (*x509.Certificate, error, error) {
		switch stage {
		case "staging":
			return &x509.Certificate{Issuer: pkix.Name{CommonName: "(STAGING) Counterfeit Cashew R10"}}, errors.New("untrusted"), nil
		case "production":
			return &x509.Certificate{Issuer: pkix.Name{CommonName: "R10"}}, nil, nil
		}
		return &x509.Certificate{Issuer: pkix.Name{CommonName: "TRAEFIK DEFAULT CERT"}}, errors.New("untrusted"), nil
	})
	raw := map[string]any{}
	if err := startProxyWithCertificate(raw, proxy, "dev.example.com", "100.64.0.9"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ups, []bool{true, false}) {
		t.Errorf("proxy started with staging=%v", ups)
	}
	if cfg, _ := loadHostConfig(); cfg.Proxy == nil || cfg.Proxy.Staging {
		t.Errorf("config left on staging: %+v", cfg.Proxy)
	}

	// With a trusted certificate already served, setup goes straight to production.
	swap(t, &acmeReachable, func(string) error { t.Error("a trusted certificate needs no ACME check"); return nil })
	ups = nil
	if err := startProxyWithCertificate(raw, proxy, "dev.example.com", "100.64.0.9"); err != nil || !slices.Equal(ups, []bool{false}) {
		t.Errorf("ups %v, %v", ups, err)
	}

	// A certificate that never comes fails with a pointer to the logs.
	swap(t, &acmeReachable, func(string) error { return nil })
	swap(t, &certificateTimeout, 5*time.Millisecond)
	stage = "stuck"
	ups = nil
	swap(t, &setupProxy, func() error { ups = append(ups, proxy.Staging); return nil })
	if err := startProxyWithCertificate(raw, proxy, "dev.example.com", "100.64.0.9"); err == nil || !strings.Contains(err.Error(), "docker logs") {
		t.Errorf("timeout: %v", err)
	}
	if !slices.Equal(ups, []bool{true}) {
		t.Errorf("issuance failure must not fall back to production: %v", ups)
	}
}

func TestSetupAsksProductionDirectlyWhenStagingIsOutOfReach(t *testing.T) {
	for _, c := range []struct {
		name       string
		production error // what reaching production gives
		trusted    bool  // whether production's certificate is trusted
		want       string
	}{
		{"production issues", nil, true, ""},
		{"production must still be trusted", nil, false, "No trusted certificate"},
		{"both out of reach", errors.New("connection refused"), false, "Check outbound HTTPS"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := hostWith(t, `{}`)
			swap[io.Writer](t, &stdout, &bytes.Buffer{})
			swap(t, &certificateTimeout, 5*time.Millisecond)
			swap(t, &certificatePoll, time.Millisecond)
			swap(t, &acmeReachable, func(directory string) error {
				if directory == stagingCA {
					return errors.New("connection reset by peer")
				}
				return c.production
			})
			proxy := proxyHost().Proxy
			var ups []bool
			swap(t, &setupProxy, func() error { ups = append(ups, proxy.Staging); return nil })
			swap(t, &servedCertificate, func(ip, host string) (*x509.Certificate, error, error) {
				cert := &x509.Certificate{Issuer: pkix.Name{CommonName: "YR1"}}
				if len(ups) > 0 && c.trusted {
					return cert, nil, nil
				}
				return cert, errors.New("untrusted"), nil
			})
			err := startProxyWithCertificate(map[string]any{}, proxy, "dev.example.com", "100.64.0.9")
			if (c.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if c.production != nil {
				if data, _ := os.ReadFile(filepath.Join(dir, "config.json")); len(ups) != 0 || string(data) != `{}` {
					t.Errorf("out of reach, yet the proxy started (%v) or the config changed: %s", ups, data)
				}
			} else if !slices.Equal(ups, []bool{false}) {
				t.Errorf("proxy started with staging=%v", ups)
			}
		})
	}
}

func TestACMEReachableWantsTheDirectory(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer server.Close()
	if err := acmeReachable(server.URL); err != nil {
		t.Error(err)
	}
	status = http.StatusServiceUnavailable
	if err := acmeReachable(server.URL); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: %v", err)
	}
}

func TestPrompterTakesDefaultsAndRepeatsInvalidChoices(t *testing.T) {
	p := &prompter{in: bufioReader("\nmaybe\nnone\n")}
	if got, _ := p.ask("Domain", "dev.example.com"); got != "dev.example.com" {
		t.Errorf("ask = %q", got)
	}
	if got, _ := p.choose("Harness", []string{"t3", "none"}, "t3"); got != "none" {
		t.Errorf("choose = %q", got)
	}
	yes := &prompter{yes: true}
	if _, err := yes.ask("Email", ""); err == nil {
		t.Error("--yes without a default should fail")
	}
}

func bufioReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func TestSetupInstallsT3OnlyWhereMissingAndAsked(t *testing.T) {
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	swap(t, &t3Poll, time.Millisecond)
	swap(t, &t3Wait, 50*time.Millisecond)
	var up, installed, service bool
	var ran []string
	swap(t, &harnessUp, func(harnessConfig) bool { return up })
	swap(t, &findT3, func() string {
		if installed {
			return "/home/u/.local/bin/t3"
		}
		return ""
	})
	swap(t, &runCommand, func(args []string, _ runOpts) (string, error) {
		cmd := strings.Join(args, " ")
		ran = append(ran, cmd)
		switch {
		case cmd == "sh -c "+t3Installer:
			installed = true
		case strings.HasSuffix(cmd, "/home/u/.local/bin/t3"): // service install
			service, up = true, true
		case cmd == "systemctl --user cat t3code.service" && !service:
			return "", errors.New("no unit")
		case cmd == "systemctl --user start t3code":
			up = true
		}
		return "", nil
	})

	// Answering yes twice installs T3 and its service, then waits for it.
	if err := ensureT3(&prompter{in: bufioReader("\ny\n")}, "dev.example.com"); err != nil || !up {
		t.Fatalf("install: up=%v, %v (ran %q)", up, err, ran)
	}
	if len(ran) != 3 || ran[0] != "sh -c "+t3Installer || !strings.Contains(ran[2], "service install") {
		t.Errorf("ran %q", ran)
	}

	// Already answering: nothing runs.
	ran = nil
	if err := ensureT3(&prompter{in: bufioReader("")}, "dev.example.com"); err != nil || len(ran) != 0 {
		t.Errorf("up: ran %q, %v", ran, err)
	}

	// Installed but stopped: setup starts the existing service without asking.
	up, ran = false, nil
	if err := ensureT3(&prompter{in: bufioReader("")}, "dev.example.com"); err != nil || !up || ran[len(ran)-1] != "systemctl --user start t3code" {
		t.Errorf("stopped: ran %q, %v", ran, err)
	}

	// Declining the installer leaves the host alone.
	up, installed, service, ran = false, false, false, nil
	if err := ensureT3(&prompter{in: bufioReader("n\n")}, "dev.example.com"); err != nil || len(ran) != 0 || installed {
		t.Errorf("declined: ran %q, %v", ran, err)
	}
}

func TestTraefikListensOnTheTailnetAndWhereTheConfigAdds(t *testing.T) {
	for _, c := range []struct{ listen, want []string }{
		{nil, []string{"100.64.0.9"}},
		{[]string{"192.168.1.20", "tailscale", "192.168.1.20"}, []string{"100.64.0.9", "192.168.1.20"}},
		{[]string{"fd00::5"}, []string{"100.64.0.9", "fd00::5"}},
		{[]string{"192.168.1.20", "0.0.0.0"}, []string{"0.0.0.0"}},
		{[]string{"0.0.0.0", "::"}, []string{"::"}},
	} {
		if got := listenAddresses(&proxyConfig{Listen: c.listen}, "100.64.0.9"); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.listen, got, c.want)
		}
	}
	cfg := proxyHost()
	compose, dynamic := proxyFiles(cfg, "dev.example.com", []string{"100.64.0.9", "fd00::5"})
	command := object(object(compose, "services"), "traefik")["command"].([]string)
	for _, want := range []string{"--entrypoints.websecure.address=100.64.0.9:443", "--entrypoints.websecure2.address=[fd00::5]:443"} {
		if !slices.Contains(command, want) {
			t.Errorf("missing %s in %q", want, command)
		}
	}
	for name, router := range object(object(dynamic, "http"), "routers") {
		if _, pinned := asObject(router)["entryPoints"]; pinned {
			t.Errorf("router %s should answer on every entrypoint", name)
		}
	}
	for _, listen := range [][]string{{"lan"}, {"fe80::1%eth0"}, {"192.168.1.20:443"}} {
		cfg.Proxy.Listen = listen
		if checkProxy(cfg.Proxy) == nil {
			t.Errorf("accepted listen %q", listen)
		}
	}
	cfg.Proxy.Listen = []string{"tailscale", "0.0.0.0", "192.168.1.20"}
	if err := checkProxy(cfg.Proxy); err != nil {
		t.Error(err)
	}
}

func TestListenAddressesMustBeThisHostsAndEveryInterfaceIsCalledOut(t *testing.T) {
	swap(t, &hostAddresses, func() []string { return []string{"127.0.0.1", "100.64.0.9", "192.168.1.20", "fd7a:115c:a1e0::9"} })
	if err := checkListen([]string{"100.64.0.9", "192.168.1.20"}); err != nil {
		t.Error(err)
	}
	if err := checkListen([]string{"0.0.0.0"}); err != nil {
		t.Error(err)
	}
	if err := checkListen([]string{"100.64.0.9", "192.168.1.21"}); err == nil || !strings.Contains(err.Error(), "192.168.1.21") {
		t.Errorf("a missing address: %v", err)
	}
	if note := listenNote([]string{"100.64.0.9"}, "100.64.0.9", "dev.example.com"); note != "" {
		t.Errorf("tailnet only needs no note: %q", note)
	}
	if note := listenNote([]string{"100.64.0.9", "192.168.1.20"}, "100.64.0.9", "dev.example.com"); !strings.Contains(note, "*.dev.example.com") {
		t.Errorf("a LAN address needs local DNS: %q", note)
	}
	if note := listenNote([]string{"0.0.0.0"}, "100.64.0.9", "dev.example.com"); strings.Contains(note, "public") {
		t.Errorf("no public address here: %q", note)
	}
	swap(t, &hostAddresses, func() []string { return []string{"100.64.0.9", "203.0.113.5", "2001:db8::5"} })
	if note := listenNote([]string{"0.0.0.0"}, "100.64.0.9", "dev.example.com"); !strings.Contains(note, "public 203.0.113.5, 2001:db8::5") {
		t.Errorf("every interface on a public host: %q", note)
	}
}
