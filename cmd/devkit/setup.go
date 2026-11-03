package main

import (
	"bufio"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// devkit setup configures this host step by step and can be run again at any time: it offers the
// current settings as defaults, keeps what still works and only changes what you choose. It checks
// for Docker, Tailscale and systemd but never installs system software.

type setupOptions struct {
	yes                    bool // take every default and fail where there is none
	domain, email, harness string
}

// prompter asks questions on stderr and reads answers from stdin.
type prompter struct {
	in  *bufio.Reader
	yes bool
}

func (p *prompter) ask(question, def string) (string, error) {
	if p.yes {
		if def == "" {
			return "", fail("%s: no default; run devkit setup without --yes.", question)
		}
		return def, nil
	}
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", question)
	}
	line, err := p.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fail("No answer to %q.", question)
	}
	if answer := strings.TrimSpace(line); answer != "" {
		return answer, nil
	}
	return def, nil
}

func (p *prompter) confirm(question string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	if p.yes {
		return def, nil
	}
	answer, err := p.ask(question+" ("+hint+")", "")
	if err != nil || answer == "" {
		return def, err
	}
	return strings.HasPrefix(strings.ToLower(answer), "y"), nil
}

func (p *prompter) choose(question string, options []string, def string) (string, error) {
	for {
		answer, err := p.ask(question+" ("+strings.Join(options, "/")+")", def)
		if err != nil || slices.Contains(options, answer) {
			return answer, err
		}
		fmt.Fprintf(os.Stderr, "Please answer one of %s.\n", strings.Join(options, ", "))
		if p.yes {
			return "", fail("%s: %q is not one of %s.", question, answer, strings.Join(options, ", "))
		}
	}
}

func step(format string, args ...any) { fmt.Fprintf(stdout, "\n== "+format+"\n", args...) }

var t3Harness = harnessConfig{Name: "T3 Code", Host: "t3", URL: "http://127.0.0.1:3773", Worktrees: "~/.t3/worktrees/*/*"}

// t3Installer is T3 Code's official installer: a self-contained t3 in ~/.local/bin, no Node needed.
const t3Installer = "curl -fsSL https://t3.codes/install.sh | sh"

// The T3 step's probes; tests replace them.
var (
	harnessUp = func(h harnessConfig) bool { return harnessStatus(h, "").Status == "up" }
	findT3    = func() string {
		if path, err := exec.LookPath("t3"); err == nil {
			return path
		}
		if home, err := os.UserHomeDir(); err == nil && exists(filepath.Join(home, ".local", "bin", "t3")) {
			return filepath.Join(home, ".local", "bin", "t3")
		}
		return ""
	}
	t3Wait = time.Minute
	t3Poll = 2 * time.Second
)

// Setup's slow or outward steps; tests replace them.
var (
	setupProxy         = proxyUp
	certificateTimeout = 4 * time.Minute
	certificatePoll    = 5 * time.Second
)

func setup(opts setupOptions) error {
	p := &prompter{in: bufio.NewReader(os.Stdin), yes: opts.yes}

	step("Prerequisites")
	var missing []string
	if version, err := probe("docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		missing = append(missing, "Docker Engine, usable without sudo (add yourself to the docker group, then log in again)")
	} else if compose, err := probe("docker", "compose", "version", "--short"); err != nil {
		missing = append(missing, "the Docker Compose v2 plugin (docker-compose-plugin)")
	} else {
		fmt.Fprintf(stdout, "Docker %s with Compose %s\n", version, compose)
	}
	ip, err := tailscaleIP()
	if err != nil {
		missing = append(missing, "Tailscale, connected to your tailnet ("+err.Error()+")")
	} else {
		fmt.Fprintf(stdout, "Tailscale IP %s\n", ip)
	}
	if _, err := probe("systemctl", "--user", "show-environment"); err != nil {
		missing = append(missing, "a systemd user session (systemctl --user)")
	}
	if len(missing) > 0 {
		return fail("Install these first, then run devkit setup again:\n  - %s", strings.Join(missing, "\n  - "))
	}
	if !lingering() {
		fmt.Fprintln(stdout, "Note: user services stop when you log out. To keep them running: sudo loginctl enable-linger $USER")
	}

	step("Settings")
	raw, err := readConfigFile()
	if err != nil {
		return err
	}
	cfg, err := loadHostConfig()
	if err != nil {
		return err
	}
	dom := strings.TrimRight(strings.ToLower(firstOf(opts.domain, cfg.Domain, os.Getenv("DEV_DOMAIN"))), ".")
	if dom, err = p.ask("Preview domain (previews get <name>.<domain>)", dom); err != nil {
		return err
	}
	dom = strings.TrimRight(strings.ToLower(dom), ".")
	if !validDomain(dom, 180) {
		return fail("%q is not a domain name; give it without a wildcard, e.g. dev.example.com.", dom)
	}
	if env := os.Getenv("DEV_DOMAIN"); env != "" && strings.TrimRight(strings.ToLower(env), ".") != dom {
		fmt.Fprintf(stdout, "Note: DEV_DOMAIN=%s is set and wins over the config; remove it from your shell and services.\n", env)
	}
	harness, err := askHarness(p, opts.harness, cfg.Harness, raw)
	if err != nil {
		return err
	}
	proxy := cfg.Proxy
	if proxy == nil {
		proxy = &proxyConfig{DNSProvider: "cloudflare", Staging: true}
	}
	email := firstOf(opts.email, proxy.Email)
	if email == "" {
		email, _ = capture("git", "config", "--global", "user.email")
	}
	if proxy.Email, err = p.ask("Email for the Let's Encrypt account", email); err != nil {
		return err
	}
	provider := proxy.DNSProvider
	if proxy.DNSProvider, err = p.ask("DNS provider for the certificate (a lego provider: cloudflare, route53, hetzner, ...)", provider); err != nil {
		return err
	}
	if proxy.DNSProvider != provider {
		proxy.DNSEnv = nil
	}
	listen, err := p.ask("Traefik listens on the Tailscale IP. Other addresses of this host for it, or 0.0.0.0 for every interface (empty for none)",
		strings.Join(proxy.Listen, " "))
	if err != nil {
		return err
	}
	proxy.Listen = strings.Fields(strings.ReplaceAll(listen, ",", " "))

	step("Credentials")
	if proxy.DNSEnv, err = askDNSCredentials(p, proxy.DNSProvider, proxy.DNSEnv, dom); err != nil {
		return err
	}
	raw["domain"] = dom
	if harness == nil {
		raw["harness"] = nil
	} else {
		raw["harness"] = harness
	}
	raw["proxy"] = proxy
	if err := writeConfigFile(raw); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Saved %s\n", configPath())
	if err := checkProxy(proxy); err != nil {
		return err
	}
	if err := checkHarness(harness); err != nil {
		return err
	}

	step("DNS")
	if err := ensureWildcard(p, dom, ip, proxy); err != nil {
		return err
	}

	step("Traefik and certificate")
	if err := startProxyWithCertificate(raw, proxy, dom, ip); err != nil {
		return err
	}

	step("Dashboard service")
	if err := installWebService(); err != nil {
		return err
	}
	if harness != nil && *harness == t3Harness {
		step("T3 Code")
		if err := ensureT3(p, dom); err != nil {
			return err
		}
	}
	happened("setup", "", "*."+dom)

	step("Doctor")
	if _, err := doctor(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nDone. The dashboard is at https://devkit.%s\n", dom)
	if passwordHash() == "" {
		fmt.Fprintln(stdout, "Set a password with devkit web password to act from it.")
	}
	if _, configured, _ := loadShareConfig(); !configured {
		fmt.Fprintln(stdout, "Public shares are optional; docs/shares.md explains the share section.")
	}
	return nil
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// askHarness picks the agent tool: T3 Code, none, or one described by hand.
func askHarness(p *prompter, flag string, current *harnessConfig, raw map[string]any) (*harnessConfig, error) {
	def := flag
	switch {
	case def != "":
	case current != nil && *current == t3Harness:
		def = "t3"
	case current != nil:
		def = "custom"
	case hasKey(raw, "harness"): // set to null: no harness
		def = "none"
	default:
		def = "t3" // setup installs it when it is missing
	}
	choice, err := p.choose("Agent harness on this host", []string{"t3", "none", "custom"}, def)
	if err != nil || choice == "none" {
		return nil, err
	}
	if choice == "t3" {
		h := t3Harness
		return &h, nil
	}
	h := harnessConfig{}
	if current != nil {
		h = *current
	}
	if h.Name, err = p.ask("Name shown on the dashboard", firstOf(h.Name, "Orca")); err != nil {
		return nil, err
	}
	if h.URL, err = p.ask("Its local URL (empty for none)", h.URL); err != nil {
		return nil, err
	}
	if h.URL != "" {
		if h.Host, err = p.ask("Route it at <host>.<domain>; host (empty for no route)", h.Host); err != nil {
			return nil, err
		}
	}
	if h.Worktrees, err = p.ask("Glob of its worktrees, e.g. ~/.orca/worktrees/*/* (empty for none)", h.Worktrees); err != nil {
		return nil, err
	}
	return &h, checkHarness(&h)
}

// ensureT3 installs T3 Code and its background service where they are missing, asking first,
// and waits for it to answer where Traefik expects it.
func ensureT3(p *prompter, dom string) error {
	if harnessUp(t3Harness) {
		fmt.Fprintf(stdout, "T3 Code answers at %s.\n", t3Harness.URL)
		return nil
	}
	t3 := findT3()
	if t3 == "" {
		install, err := p.confirm("T3 Code is not installed. Install it with its official installer ("+t3Installer+")?", true)
		if err != nil {
			return err
		}
		if !install {
			fmt.Fprintln(stdout, "Skipped. Install it from https://t3.codes, then run devkit setup again.")
			return nil
		}
		if _, err := runCommand([]string{"sh", "-c", t3Installer}, runOpts{}); err != nil {
			return err
		}
		if t3 = findT3(); t3 == "" {
			return fail("The T3 Code installer finished, but there is no t3 in ~/.local/bin.")
		}
	}
	if _, err := probe("systemctl", "--user", "cat", "t3code.service"); err != nil {
		service, err := p.confirm("Run T3 Code as a background service (t3 service install)?", true)
		if err != nil {
			return err
		}
		if !service {
			fmt.Fprintln(stdout, "Skipped. Start it later with t3 service install.")
			return nil
		}
		if info, err := meminfo(); err == nil && info["MemTotal"] < 2<<30 {
			fmt.Fprintln(stdout, "Note: this host has less than 2 GB of memory; T3 Code and previews will be tight.")
		}
		// T3 asks nothing here; </dev/null keeps it off setup's own answers.
		if _, err := runCommand([]string{"sh", "-c", `exec "$0" service install </dev/null`, t3}, runOpts{}); err != nil {
			return err
		}
	} else if _, err := runCommand([]string{"systemctl", "--user", "start", "t3code"}, runOpts{capture: true}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Waiting for T3 Code at %s (up to %s)...\n", t3Harness.URL, t3Wait)
	for deadline := time.Now().Add(t3Wait); !harnessUp(t3Harness); time.Sleep(t3Poll) {
		if time.Now().After(deadline) {
			fmt.Fprintln(stdout, "T3 Code does not answer yet; see ~/.t3/userdata/logs/boot-service.log and t3 service status.")
			return nil
		}
	}
	fmt.Fprintf(stdout, "T3 Code is up at https://t3.%s. To connect a device, run t3 pair on this host.\n", dom)
	fmt.Fprintln(stdout, "Skip t3 up: Traefik already serves T3 on the tailnet, and t3 up's Tailscale Serve would take its port 443.")
	return nil
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// askDNSCredentials collects the DNS provider's API credentials, keeping ones that still read.
func askDNSCredentials(p *prompter, provider string, current map[string]secretSource, dom string) (map[string]secretSource, error) {
	keys := sortedKeys(current)
	if provider == "cloudflare" {
		keys = []string{"CF_DNS_API_TOKEN"}
		fmt.Fprintf(stdout, "Traefik needs a Cloudflare API token with Zone / DNS / Edit and Zone / Zone / Read for the zone of %s.\n", dom)
	} else if len(keys) == 0 {
		fmt.Fprintf(stdout, "See https://go-acme.github.io/lego/dns/%s/ for the variables the %s provider needs.\n", provider, provider)
		answer, err := p.ask("Its environment variables, separated by spaces", "")
		if err != nil {
			return nil, err
		}
		keys = strings.Fields(answer)
	}
	result := map[string]secretSource{}
	for _, key := range keys {
		if !envName.MatchString(key) {
			return nil, fail("%q is not an environment variable name.", key)
		}
		if source, ok := current[key]; ok {
			if _, err := source.read(); err == nil {
				keep, err := p.confirm(fmt.Sprintf("%s comes from %s. Keep it?", key, source), true)
				if err != nil {
					return nil, err
				}
				if keep {
					result[key] = source
					continue
				}
			} else {
				fmt.Fprintf(stdout, "%s from %s does not work: %v\n", key, source, err)
			}
		}
		file := filepath.Join(configDir(), strings.ToLower(strings.ReplaceAll(key, "_", "-")))
		if key == "CF_DNS_API_TOKEN" {
			file = filepath.Join(configDir(), "dns-token")
		}
		source, err := askSecret(p, key, file)
		if err != nil {
			return nil, err
		}
		result[key] = source
	}
	if token, ok := result["CF_DNS_API_TOKEN"]; ok {
		value, err := token.read()
		if err != nil {
			return nil, err
		}
		zone, err := dnsZone(&cloudflare{token: value, client: cloudflareClient()}, dom)
		if err != nil {
			return nil, fail("The Cloudflare token does not work for %s: %v", dom, err)
		}
		fmt.Fprintf(stdout, "The token reaches zone %s.\n", zone)
	}
	return result, nil
}

// askSecret asks where one secret comes from; a typed one is stored in file with chmod 600.
func askSecret(p *prompter, key, file string) (secretSource, error) {
	if p.yes {
		return secretSource{}, fail("%s is not configured; run devkit setup without --yes.", key)
	}
	fmt.Fprintf(os.Stderr, "Where should devkit get %s?\n"+
		"  file     type it now; it is saved to %s (chmod 600)\n"+
		"  env      an environment variable, e.g. under op run, bws run or doppler run\n"+
		"  command  a command that prints it, e.g. op read op://vault/item/field\n", key, file)
	choice, err := p.choose("Source", []string{"file", "env", "command"}, "file")
	if err != nil {
		return secretSource{}, err
	}
	var source secretSource
	switch choice {
	case "file":
		value, err := readSecret(key+" (hidden): ", p.in)
		if err != nil {
			return source, err
		}
		if value = strings.TrimSpace(value); value == "" {
			return source, fail("Nothing entered for %s.", key)
		}
		if err := writeSecretFile(file, value); err != nil {
			return source, err
		}
		source.File = file
	case "env":
		if source.Env, err = p.ask("Variable name", key); err != nil {
			return source, err
		}
		fmt.Fprintf(stdout, "devkit proxy up and devkit doctor then need %s set, e.g. op run --env-file=... -- devkit proxy up\n", source.Env)
	case "command":
		if source.Command, err = p.ask("Command", ""); err != nil {
			return source, err
		}
	}
	if _, err := source.read(); err != nil {
		return source, err
	}
	return source, nil
}

// ensureWildcard checks that *.domain resolves to the Tailscale IP and, on Cloudflare, offers to create the record.
func ensureWildcard(p *prompter, dom, ip string, proxy *proxyConfig) error {
	if addresses, err := lookupHost(randomLabel() + "." + dom); err == nil && slices.Contains(addresses, ip) {
		fmt.Fprintf(stdout, "*.%s resolves to %s.\n", dom, ip)
		return nil
	}
	fmt.Fprintf(stdout, "Previews need a DNS-only record: *.%s A %s\n", dom, ip)
	source, ok := proxy.DNSEnv["CF_DNS_API_TOKEN"]
	if proxy.DNSProvider != "cloudflare" || !ok {
		fmt.Fprintln(stdout, "Create it with your DNS provider; devkit doctor checks it. The certificate does not depend on it.")
		return nil
	}
	token, err := source.read()
	if err != nil {
		return err
	}
	c := &cloudflare{token: token, client: cloudflareClient()}
	if _, err := dnsZone(c, dom); err != nil {
		return err
	}
	name := "*." + dom
	records, err := c.dnsRecords(url.Values{"name": {name}})
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Type == "A" && r.Content == ip {
			fmt.Fprintln(stdout, "Cloudflare already has it; it may take a few minutes to resolve here.")
			return nil
		}
		fmt.Fprintf(stdout, "Cloudflare already has %s %s %s; change it by hand if it is wrong.\n", name, r.Type, r.Content)
		return nil
	}
	create, err := p.confirm("Create it in Cloudflare now?", true)
	if err != nil || !create {
		return err
	}
	body := map[string]any{"type": "A", "name": name, "content": ip, "proxied": false, "ttl": 1,
		"comment": "devkit previews on " + machine()}
	if err := c.call("POST", "/zones/"+url.PathEscape(c.zone)+"/dns_records", nil, body, nil); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Created. It may take a few minutes to resolve.")
	happened("dns", "", name+" A "+ip)
	return nil
}

// probeACMEDirectory checks connectivity without creating an account or ordering a certificate.
func probeACMEDirectory(endpoint string) error {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail("%s returned HTTP %d", endpoint, resp.StatusCode)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return err
}

// startProxyWithCertificate starts Traefik. Without a trusted certificate yet it proves issuance against
// staging first, unless that service is unavailable and production is reachable.
func startProxyWithCertificate(raw map[string]any, proxy *proxyConfig, dom, ip string) error {
	host := "devkit." + dom
	run := func(staging bool, want string, accept func(*x509.Certificate, error) bool) error {
		proxy.Staging = staging
		raw["proxy"] = proxy
		if err := writeConfigFile(raw); err != nil {
			return err
		}
		if err := setupProxy(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Waiting for a %s certificate for *.%s (up to %s)...\n", want, dom, certificateTimeout)
		deadline := time.Now().Add(certificateTimeout)
		for {
			if cert, verr, err := servedCertificate(ip, host); err == nil && accept(cert, verr) {
				fmt.Fprintf(stdout, "Got it, issued by %s, valid until %s.\n", cert.Issuer.CommonName, cert.NotAfter.Format("2 Jan 2006"))
				return nil
			}
			if time.Now().After(deadline) {
				return fail("No %s certificate after %s. See docker logs %s-traefik-1, fix the cause and run devkit setup again.",
					want, certificateTimeout, proxyProject)
			}
			time.Sleep(certificatePoll)
		}
	}
	trusted := func(_ *x509.Certificate, verr error) bool { return verr == nil }
	if cert, verr, err := servedCertificate(ip, host); err == nil && verr == nil {
		fmt.Fprintf(stdout, "Traefik already serves a trusted certificate (until %s).\n", cert.NotAfter.Format("2 Jan 2006"))
		return run(false, "trusted", trusted)
	}
	fmt.Fprintln(stdout, "Checking Let's Encrypt staging connectivity (up to 10s)...")
	if stagingErr := probeACMEDirectory(stagingCA); stagingErr != nil {
		fmt.Fprintf(stdout, "Let's Encrypt staging is unavailable: %v\nChecking production connectivity (up to 10s)...\n", stagingErr)
		if productionErr := probeACMEDirectory(productionCA); productionErr != nil {
			return fail("Cannot reach Let's Encrypt staging (%v) or production (%v). Check outbound HTTPS access and run devkit setup again.", stagingErr, productionErr)
		}
		fmt.Fprintln(stdout, "Production is reachable; skipping staging and requesting a trusted certificate. Production rate limits apply.")
		return run(false, "trusted", trusted)
	}
	staging := func(cert *x509.Certificate, _ error) bool { return strings.Contains(cert.Issuer.CommonName, "STAGING") }
	if err := run(true, "staging", staging); err != nil {
		return err
	}
	return run(false, "trusted", trusted)
}

// installWebService writes the dashboard's user service for this binary and (re)starts it.
func installWebService() error {
	self, err := webExecutable()
	if err != nil {
		return err
	}
	if strings.HasPrefix(self, os.TempDir()) {
		return fail("This devkit binary is temporary (%s); build it with go build -o bin/devkit ./cmd/devkit and run that.", self)
	}
	unit := webUnit(self)
	current, _ := os.ReadFile(unitPath())
	if string(current) != unit {
		if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(unitPath(), []byte(unit), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Wrote %s for %s\n", unitPath(), self)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "devkit-web"}, {"restart", "devkit-web"}} {
		if _, err := runCommand(append([]string{"systemctl", "--user"}, args...), runOpts{capture: true}); err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "devkit-web is running.")
	return nil
}
