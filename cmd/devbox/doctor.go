package main

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

// devbox doctor checks everything a working host needs and says how to fix what is missing.

type check struct {
	Status string // "ok", "warn" or "fail"
	Name   string
	Detail string
}

func ok(name, format string, args ...any) check {
	return check{"ok", name, fmt.Sprintf(format, args...)}
}
func warn(name, format string, args ...any) check {
	return check{"warn", name, fmt.Sprintf(format, args...)}
}
func bad(name, format string, args ...any) check {
	return check{"fail", name, fmt.Sprintf(format, args...)}
}

// lookupHost resolves a name as browsers on the tailnet would; tests replace it.
var lookupHost = net.LookupHost

// servedCertificate fetches the certificate Traefik serves for host on ip:443 and verifies it.
var servedCertificate = func(ip, host string) (*x509.Certificate, error, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", net.JoinHostPort(ip, "443"),
		&tls.Config{ServerName: host, InsecureSkipVerify: true})
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()
	chain := conn.ConnectionState().PeerCertificates
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	_, verr := chain[0].Verify(x509.VerifyOptions{DNSName: host, Intermediates: intermediates})
	return chain[0], verr, nil
}

// dnsZone finds the Cloudflare zone that holds name, trying it and each parent domain.
func dnsZone(c *cloudflare, name string) (string, error) {
	parts := strings.Split(name, ".")
	for i := 0; i+2 <= len(parts); i++ {
		candidate := strings.Join(parts[i:], ".")
		var zones []struct {
			ID string `json:"id"`
		}
		if err := c.call("GET", "/zones", url.Values{"name": {candidate}}, nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			c.zone = zones[0].ID
			return candidate, nil
		}
	}
	return "", fail("this token sees no Cloudflare zone for %s", name)
}

func randomLabel() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "doctor-" + hex.EncodeToString(b)
}

// webExecutable is the binary the dashboard service should run: this one.
func webExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(self)
}

func unitPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", "devbox-web.service")
}

// webUnit is the dashboard's systemd user service; the binary reads everything else from the host config.
func webUnit(executable string) string {
	if strings.ContainsAny(executable, " \t") {
		executable = `"` + executable + `"`
	}
	return `# Written by devbox setup; run it again rather than editing this file.
[Unit]
Description=devbox web overview
After=docker.service

[Service]
Type=simple
ExecStart=` + executable + ` web
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`
}

// lingering reports whether this user's services keep running without a login session.
func lingering() bool {
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	out, err := capture("loginctl", "show-user", name, "--property=Linger", "--value")
	return err == nil && strings.TrimSpace(out) == "yes"
}

func doctorChecks() []check {
	var checks []check
	cfg, err := loadHostConfig()
	if err != nil {
		return append(checks, bad("config", "%v", err))
	}
	dom, err := domain()
	if err != nil {
		checks = append(checks, bad("config", "no preview domain in %s: run devbox setup", configPath()))
	} else {
		checks = append(checks, ok("config", "%s, previews at *.%s", configPath(), dom))
	}

	if version, err := capture("docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		checks = append(checks, bad("docker", "Docker does not answer for this user: install Docker Engine and add yourself to the docker group (bootstrap.sh in the devbox checkout does it on Ubuntu, Debian and Fedora)"))
	} else if compose, err := capture("docker", "compose", "version", "--short"); err != nil {
		checks = append(checks, bad("docker", "Docker %s has no Compose v2 plugin: install docker-compose-plugin", version))
	} else {
		checks = append(checks, ok("docker", "Docker %s with Compose %s", version, compose))
	}

	ip, err := tailscaleIP()
	if err != nil {
		checks = append(checks, bad("tailscale", "%v", err))
	} else {
		checks = append(checks, ok("tailscale", "%s", ip))
	}

	if dom != "" && ip != "" {
		probe := randomLabel() + "." + dom
		addresses, err := lookupHost(probe)
		switch {
		case err != nil:
			checks = append(checks, bad("dns", "%s does not resolve: create the DNS-only record *.%s A %s", probe, dom, ip))
		case !slices.Contains(addresses, ip):
			checks = append(checks, bad("dns", "*.%s resolves to %s, not %s: point the wildcard record at the Tailscale IP", dom, strings.Join(addresses, ", "), ip))
		default:
			checks = append(checks, ok("dns", "*.%s resolves to %s", dom, ip))
		}
	}

	if _, err := capture("docker", "network", "inspect", "--format", "{{.Name}}", proxyNetwork); err != nil {
		checks = append(checks, bad("network", "Docker network %s is missing: run devbox proxy up", proxyNetwork))
	} else {
		checks = append(checks, ok("network", "%s", proxyNetwork))
	}

	checks = append(checks, proxyChecks(cfg, dom, ip)...)

	if self, err := webExecutable(); err == nil {
		want := webUnit(self)
		have, _ := os.ReadFile(unitPath())
		active, _ := capture("systemctl", "--user", "is-active", "devbox-web")
		switch {
		case have == nil:
			checks = append(checks, bad("dashboard", "devbox-web is not installed: run devbox setup"))
		case string(have) != want:
			checks = append(checks, warn("dashboard", "%s differs from what devbox setup writes for %s: run devbox setup", unitPath(), self))
		case strings.TrimSpace(active) != "active":
			checks = append(checks, bad("dashboard", "devbox-web is %s: systemctl --user restart devbox-web; journalctl --user -u devbox-web", strings.TrimSpace(active)))
		default:
			checks = append(checks, ok("dashboard", "devbox-web is running %s", self))
		}
	}
	if lingering() {
		checks = append(checks, ok("linger", "user services run without a login"))
	} else {
		checks = append(checks, warn("linger", "user services stop when you log out: sudo loginctl enable-linger $USER (or bootstrap.sh in the devbox checkout)"))
	}

	if h := cfg.Harness; h == nil {
		checks = append(checks, ok("harness", "none configured"))
	} else if h.URL != "" {
		if s := harnessStatus(*h, dom); s.Status == "up" {
			checks = append(checks, ok("harness", "%s answers at %s", h.Name, h.URL))
		} else {
			checks = append(checks, bad("harness", "%s does not answer at %s (%s)", h.Name, h.URL, s.Detail))
		}
	}

	if share, configured, err := loadShareConfig(); err != nil {
		checks = append(checks, bad("share", "%v", err))
	} else if configured {
		c := newCloudflare(share)
		if _, err := c.zoneID(share.Domain); err != nil {
			checks = append(checks, bad("share", "token from %s cannot reach %s: %v", share.Token, share.Domain, err))
		} else {
			checks = append(checks, ok("share", "public shares on %s", share.Domain))
		}
	}

	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		checks = append(checks, bad("history", "%s is not writable: %v", stateDir(), err))
	}
	return checks
}

// proxyChecks covers Traefik: configuration, credentials, the running container and its certificate.
func proxyChecks(cfg hostConfig, dom, ip string) []check {
	if err := checkProxy(cfg.Proxy); err != nil {
		return []check{bad("traefik", "%v", err)}
	}
	var checks []check
	tokens := true
	for _, key := range sortedKeys(cfg.Proxy.DNSEnv) {
		source := cfg.Proxy.DNSEnv[key]
		value, err := source.read()
		if err != nil {
			checks = append(checks, bad("dns token", "%s: %v", key, err))
			tokens = false
			continue
		}
		if key == "CF_DNS_API_TOKEN" && dom != "" {
			if zone, err := dnsZone(&cloudflare{token: value, client: cloudflareClient()}, dom); err != nil {
				checks = append(checks, bad("dns token", "%s from %s: %v", key, source, err))
				tokens = false
			} else {
				checks = append(checks, ok("dns token", "%s from %s reaches zone %s", key, source, zone))
			}
		}
	}
	if tokens && len(checks) == 0 {
		checks = append(checks, ok("dns token", "%s read", strings.Join(sortedKeys(cfg.Proxy.DNSEnv), ", ")))
	}

	ids, _ := capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+proxyProject)
	containers, err := inspect(strings.Fields(ids)...)
	if err != nil || len(containers) == 0 {
		return append(checks, bad("traefik", "not running: run devbox proxy up"))
	}
	c := containers[0]
	status := containerStatus(c.State)
	if status != "running" && status != "healthy" {
		return append(checks, bad("traefik", "%s: docker logs %s", status, strings.TrimPrefix(c.Name, "/")))
	}
	if dir := c.Config.Labels["com.docker.compose.project.working_dir"]; dir != proxyDir() {
		checks = append(checks, warn("traefik", "running from %s, not devbox's %s: run devbox proxy up", dir, proxyDir()))
	} else if dom != "" && ip != "" && !proxyCurrent(cfg, dom, ip) {
		checks = append(checks, warn("traefik", "the host config changed since Traefik started: run devbox proxy up"))
	} else {
		checks = append(checks, ok("traefik", "running %s", c.Config.Image))
	}

	if dom == "" || ip == "" {
		return checks
	}
	host := "devbox." + dom
	cert, verr, err := servedCertificate(ip, host)
	switch {
	case err != nil:
		checks = append(checks, bad("certificate", "nothing answers on %s:443: docker logs %s", ip, strings.TrimPrefix(c.Name, "/")))
	case verr != nil && strings.Contains(cert.Issuer.CommonName, "STAGING"):
		checks = append(checks, warn("certificate", "a Let's Encrypt staging certificate, which browsers distrust: run devbox setup to switch to production"))
	case verr != nil:
		checks = append(checks, bad("certificate", "%s serves an untrusted certificate (%s): docker logs %s", host, cert.Issuer.CommonName, strings.TrimPrefix(c.Name, "/")))
	default:
		days := int(math.Floor(time.Until(cert.NotAfter).Hours() / 24))
		if days < 14 {
			checks = append(checks, warn("certificate", "expires in %d days; Traefik should have renewed it: docker logs %s", days, strings.TrimPrefix(c.Name, "/")))
		} else {
			checks = append(checks, ok("certificate", "*.%s, %d days left", dom, days))
		}
	}
	return checks
}

// proxyCurrent reports whether the proxy files on disk are what the host config renders now.
func proxyCurrent(cfg hostConfig, dom, ip string) bool {
	compose, dynamic := proxyFiles(cfg, dom, ip)
	for name, want := range map[string]map[string]any{"compose.yaml": compose, "dynamic.yaml": dynamic} {
		data, err := os.ReadFile(filepath.Join(proxyDir(), name))
		if err != nil {
			return false
		}
		var have, normal map[string]any
		encoded, _ := json.Marshal(want)
		if json.Unmarshal(data, &have) != nil || json.Unmarshal(encoded, &normal) != nil || !reflect.DeepEqual(have, normal) {
			return false
		}
	}
	return true
}

// doctor is devbox doctor: it prints every check and fails if any did.
func doctor() (int, error) {
	failed := 0
	for _, c := range doctorChecks() {
		fmt.Fprintf(stdout, "%-4s  %-11s  %s\n", c.Status, c.Name, c.Detail)
		if c.Status == "fail" {
			failed++
		}
	}
	if failed > 0 {
		return 1, fail("%d check(s) failed.", failed)
	}
	return 0, nil
}
