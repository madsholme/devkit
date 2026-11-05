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
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

// devkit doctor checks everything a working host needs and says how to fix what is missing.

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

// lookupHost uses this host's resolver, including local split DNS; tests replace it.
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
	return filepath.Join(dir, "systemd", "user", "devkit-web.service")
}

// webUnit is the dashboard's systemd user service; the binary reads everything else from the host config.
func webUnit(executable string) string {
	if strings.ContainsAny(executable, " \t") {
		executable = `"` + executable + `"`
	}
	return `# Written by devkit setup; run it again rather than editing this file.
[Unit]
Description=devkit web overview
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
	out, err := probe("loginctl", "show-user", name, "--property=Linger", "--value")
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
		checks = append(checks, bad("config", "no preview domain in %s: run devkit setup", configPath()))
	} else {
		checks = append(checks, ok("config", "%s, previews at *.%s", configPath(), dom))
	}

	if version, err := probe("docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		checks = append(checks, bad("docker", "Docker does not answer for this user: install Docker Engine and add yourself to the docker group (bootstrap.sh in the devkit checkout does it on Ubuntu, Debian and Fedora)"))
	} else if compose, err := probe("docker", "compose", "version", "--short"); err != nil {
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
		listening := []string{ip}
		if cfg.Proxy != nil {
			listening = listenAddresses(cfg.Proxy, ip)
			if all, _ := netip.ParseAddr(listening[0]); all.IsUnspecified() {
				listening = nil
				for _, local := range hostAddresses() {
					addr, _ := netip.ParseAddr(local)
					if all.Is6() || addr.Is4() {
						listening = append(listening, local)
					}
				}
			}
		}
		switch {
		case err != nil:
			checks = append(checks, bad("dns", "%s does not resolve: create the DNS-only record *.%s A %s", probe, dom, ip))
		case !slices.ContainsFunc(addresses, func(address string) bool { return slices.Contains(listening, address) }):
			checks = append(checks, bad("dns", "*.%s resolves to %s, not a listener (%s): use the Tailscale IP in public DNS or a configured listen address in local DNS", dom, strings.Join(addresses, ", "), strings.Join(listening, ", ")))
		default:
			checks = append(checks, ok("dns", "*.%s resolves to %s", dom, strings.Join(addresses, ", ")))
		}
	}

	if _, err := probe("docker", "network", "inspect", "--format", "{{.Name}}", proxyNetwork); err != nil {
		fix := "devkit proxy up"
		if cfg.Proxy == nil {
			fix = "devkit setup"
		}
		checks = append(checks, bad("network", "Docker network %s is missing: run %s", proxyNetwork, fix))
	} else {
		checks = append(checks, ok("network", "%s", proxyNetwork))
	}

	checks = append(checks, proxyChecks(cfg, dom, ip)...)

	if self, err := webExecutable(); err == nil {
		want := webUnit(self)
		have, _ := os.ReadFile(unitPath())
		active, _ := probe("systemctl", "--user", "is-active", "devkit-web")
		switch {
		case have == nil:
			checks = append(checks, bad("dashboard", "devkit-web is not installed: run devkit setup"))
		case string(have) != want:
			checks = append(checks, warn("dashboard", "%s differs from what devkit setup writes for %s: run devkit setup", unitPath(), self))
		case strings.TrimSpace(active) != "active":
			checks = append(checks, bad("dashboard", "devkit-web is %s: systemctl --user restart devkit-web; journalctl --user -u devkit-web", strings.TrimSpace(active)))
		default:
			checks = append(checks, ok("dashboard", "devkit-web is running %s", self))
		}
	}
	if lingering() {
		checks = append(checks, ok("linger", "user services run without a login"))
	} else {
		checks = append(checks, warn("linger", "user services stop when you log out: sudo loginctl enable-linger $USER (or bootstrap.sh in the devkit checkout)"))
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

	checks = append(checks, shareChecks()...)

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
	if ip != "" {
		addrs := listenAddresses(cfg.Proxy, ip)
		if err := checkListen(addrs); err != nil {
			checks = append(checks, bad("listen", "%v", err))
		} else if note := listenNote(addrs, ip, dom); note == "" {
			checks = append(checks, ok("listen", "tailnet only (%s)", ip))
		} else if all, _ := netip.ParseAddr(addrs[0]); all.IsUnspecified() && publicAddresses() != nil {
			checks = append(checks, warn("listen", "%s", note))
		} else {
			checks = append(checks, ok("listen", "%s. %s", strings.Join(addrs, ", "), note))
		}
	}
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
		return append(checks, bad("traefik", "not running: run devkit proxy up"))
	}
	c := containers[0]
	status := containerStatus(c.State)
	if status != "running" && status != "healthy" {
		return append(checks, bad("traefik", "%s: docker logs %s", status, strings.TrimPrefix(c.Name, "/")))
	}
	if dir := c.Config.Labels["com.docker.compose.project.working_dir"]; dir != proxyDir() {
		checks = append(checks, warn("traefik", "running from %s, not devkit's %s: run devkit proxy up", dir, proxyDir()))
	} else if dom != "" && ip != "" && !proxyCurrent(cfg, dom, ip) {
		checks = append(checks, warn("traefik", "the host config changed since Traefik started: run devkit proxy up"))
	} else {
		checks = append(checks, ok("traefik", "running %s", c.Config.Image))
	}

	if dom == "" || ip == "" {
		return checks
	}
	host := "devkit." + dom
	cert, verr, err := servedCertificate(ip, host)
	switch {
	case err != nil:
		checks = append(checks, bad("certificate", "nothing answers on %s:443: docker logs %s", ip, strings.TrimPrefix(c.Name, "/")))
	case verr != nil && strings.Contains(cert.Issuer.CommonName, "STAGING"):
		checks = append(checks, warn("certificate", "a Let's Encrypt staging certificate, which browsers distrust: run devkit setup to switch to production"))
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

// shareChecks looks at public shares without changing anything: the token's access to the zone, the
// account's tunnels and the DNS records, each share's cloudflared and tunnel, shares past their
// expiry, and this machine's leftovers in Cloudflare.
func shareChecks() []check {
	cfg, configured, err := loadShareConfig()
	if err != nil {
		return []check{bad("share", "%v", err)}
	}
	if !configured {
		return nil
	}
	cf := newCloudflare(cfg)
	if _, err := cf.zoneID(cfg.Domain); err != nil {
		return []check{bad("share", "token from %s cannot reach %s: %v", cfg.Token, cfg.Domain, err)}
	}
	tunnels, err := cf.listTunnels(tunnelPrefix())
	if err != nil {
		return []check{bad("share", "token from %s cannot list tunnels in account %s; it needs Account / Cloudflare Tunnel / Edit: %v", cfg.Token, cfg.AccountID, err)}
	}
	records, err := shareRecords(cf)
	if err != nil {
		return []check{bad("share", "token from %s cannot read DNS records of %s; it needs Zone / DNS / Edit: %v", cfg.Token, cfg.Domain, err)}
	}
	containers, err := dockerContainers()
	if err != nil {
		return []check{warn("share", "cannot list the share containers: %v", err)}
	}
	status := map[string]string{}
	for _, t := range tunnels {
		status[t.ID] = t.Status
	}
	var checks []check
	var shares []share
	for _, c := range containers {
		s, isShare := shareOf(c)
		if !isShare {
			continue
		}
		shares = append(shares, s)
		name := strings.TrimPrefix(c.Name, "/")
		switch state := containerStatus(c.State); {
		case state != "running" && state != "healthy":
			checks = append(checks, bad("share", "%s: cloudflared is %s: docker logs %s", s.URL, state, name))
		case status[s.Tunnel] == "degraded":
			checks = append(checks, warn("share", "%s: Cloudflare reports its tunnel degraded: docker logs %s", s.URL, name))
		case status[s.Tunnel] != "healthy":
			checks = append(checks, bad("share", "%s: Cloudflare reports its tunnel %s: docker logs %s", s.URL, firstOf(status[s.Tunnel], "missing"), name))
		}
		if !s.Expires.IsZero() && time.Now().After(s.Expires) {
			checks = append(checks, warn("share", "%s expired at %s but is still public; devkit-web ends shares when they expire: devkit unshare --expired", s.URL, localTime(s.Expires)))
		}
	}
	lostTunnels, lostRecords := leftovers(tunnels, records, shares, 10*time.Minute)
	if len(lostTunnels)+len(lostRecords) > 0 {
		var names []string
		for _, t := range lostTunnels {
			names = append(names, "tunnel "+t.Name)
		}
		for _, r := range lostRecords {
			names = append(names, "DNS record "+r.Name)
		}
		checks = append(checks, warn("share", "leftovers of this machine in Cloudflare (%s): devkit unshare --expired removes them", strings.Join(names, ", ")))
	}
	if len(checks) == 0 && len(shares) == 0 {
		checks = append(checks, ok("share", "public shares on %s; none active", cfg.Domain))
	} else if len(checks) == 0 {
		checks = append(checks, ok("share", "public shares on %s; %d active, all connected", cfg.Domain, len(shares)))
	}
	return checks
}

// proxyCurrent reports whether the proxy files on disk are what the host config renders now.
func proxyCurrent(cfg hostConfig, dom, ip string) bool {
	compose, dynamic := proxyFiles(cfg, dom, listenAddresses(cfg.Proxy, ip))
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

// doctor is devkit doctor: it prints every check and fails if any did.
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
