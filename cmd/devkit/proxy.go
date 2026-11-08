package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// devkit proxy up writes the Traefik proxy (Compose project devkit-proxy) from the host config into
// ~/.config/devkit/proxy and starts it. Traefik listens on the Tailscale IP (and wherever proxy.listen
// adds), gets one wildcard
// certificate for the domain by DNS-01, routes the dashboard and the harness from its file provider
// and previews from Docker labels. The DNS credentials reach the container through the environment
// of docker compose, so they are not written to the proxy directory.

const (
	proxyProject = "devkit-proxy"
	proxyNetwork = "devkit-proxy"
	stagingCA    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	productionCA = "https://acme-v02.api.letsencrypt.org/directory"
)

var (
	envName      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	providerName = regexp.MustCompile(`^[a-z0-9]+$`)
	emailAddress = regexp.MustCompile(`^[^\s@$"'\x60\\{}]+@[a-z0-9.-]+\.[a-z]+$`)
)

func proxyDir() string { return filepath.Join(configDir(), "proxy") }

// checkProxy validates the proxy section before any of it reaches Traefik's command line or rules.
func checkProxy(e *proxyConfig) error {
	if e == nil {
		return fail("Traefik is not configured; run devkit setup.")
	}
	if !emailAddress.MatchString(e.Email) {
		return fail("proxy.email %q is not a usable email address.", e.Email)
	}
	if !providerName.MatchString(e.DNSProvider) {
		return fail("proxy.dns_provider %q is not a lego DNS provider name (see https://go-acme.github.io/lego/dns/).", e.DNSProvider)
	}
	if len(e.DNSEnv) == 0 {
		return fail("proxy.dns_env names no credentials for the %s DNS provider.", e.DNSProvider)
	}
	for key := range e.DNSEnv {
		if !envName.MatchString(key) {
			return fail("proxy.dns_env: %q is not an environment variable name.", key)
		}
	}
	if strings.ContainsAny(e.Image, " $\"'") {
		return fail("proxy.image %q is not an image reference.", e.Image)
	}
	for _, entry := range e.Listen {
		if ip, err := netip.ParseAddr(entry); entry != "tailscale" && (err != nil || ip.Zone() != "") {
			return fail("proxy.listen: %q is not an IP address.", entry)
		}
	}
	return nil
}

// listenAddresses are the addresses Traefik binds: the Tailscale IP, which DNS points at, and those
// in proxy.listen ("tailscale" there names it too). An unspecified address (0.0.0.0, or :: which takes IPv4 too) covers every interface, so it stands
// alone: binding a specific address next to it would fail.
func listenAddresses(e *proxyConfig, tailscale string) []string {
	var out []string
	all := ""
	for _, entry := range append([]string{"tailscale"}, e.Listen...) {
		if entry == "tailscale" {
			entry = tailscale
		}
		ip, err := netip.ParseAddr(entry)
		switch {
		case err != nil:
		case ip.IsUnspecified():
			if all == "" || ip.Is6() {
				all = ip.String()
			}
		case !slices.Contains(out, ip.String()):
			out = append(out, ip.String())
		}
	}
	if all != "" {
		return []string{all}
	}
	return out
}

// hostAddresses are this host's own IP addresses; Traefik can only bind those.
var hostAddresses = func() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr().String())
		}
	}
	return out
}

// checkListen fails for an added address this host does not have, e.g. a LAN address DHCP changed.
// The Tailscale IP is Tailscale's to check.
func checkListen(addrs []string) error {
	have := hostAddresses()
	for _, a := range addrs {
		if ip, _ := netip.ParseAddr(a); !ip.IsUnspecified() && !tailscaleRange.Contains(ip) && !slices.Contains(have, a) {
			return fail("proxy.listen: %s is not an address of this host (ip -brief address lists them).", a)
		}
	}
	return nil
}

// publicAddresses are the host's addresses the internet may reach: an unspecified listen address
// exposes the dashboard and every preview on them.
func publicAddresses() []string {
	var out []string
	for _, a := range hostAddresses() {
		ip, _ := netip.ParseAddr(a)
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && !tailscaleRange.Contains(ip) {
			out = append(out, a)
		}
	}
	return out
}

// listenNote says who can reach Traefik besides the tailnet, or nothing when only the tailnet can.
func listenNote(addrs []string, tailscale, dom string) string {
	if len(addrs) == 1 && addrs[0] == tailscale {
		return ""
	}
	if ip, _ := netip.ParseAddr(addrs[0]); ip.IsUnspecified() {
		if public := publicAddresses(); len(public) > 0 {
			return "Traefik listens on every interface, including the public " + strings.Join(public, ", ") +
				": anyone who reaches port 443 there reaches the dashboard and every preview, so let a firewall decide who can."
		}
		return "Traefik listens on every interface: anything that reaches this host on port 443 reaches the dashboard and every preview."
	}
	return "Devices off the tailnet need DNS that answers *." + dom + " with one of these addresses (e.g. your router or Pi-hole), and a firewall that lets port 443 in there."
}

// entrypointName keeps the first entrypoint's old name, so previews started before proxy.listen
// existed, whose routers name it, keep answering until they are next started.
func entrypointName(i int) string {
	if i == 0 {
		return "websecure"
	}
	return fmt.Sprintf("websecure%d", i+1)
}

// proxyFiles renders Traefik's Compose project and file-provider configuration.
// Routers name no entrypoint, so they answer on all of them.
func proxyFiles(cfg hostConfig, dom string, addrs []string) (compose, dynamic map[string]any) {
	e := cfg.Proxy
	ca, storage := productionCA, "/certs/acme.json"
	if e.Staging {
		// Separate storage keeps staging certificates out of the production store.
		ca, storage = stagingCA, "/certs/acme-staging.json"
	}
	image := e.Image
	if image == "" {
		image = defaultTraefik
	}
	environment := map[string]any{}
	for _, key := range sortedKeys(e.DNSEnv) {
		if e.DNSEnv[key].OpenBao != "" {
			environment[key+"_FILE"] = "/run/devkit-secrets/dns-" + key
		} else {
			environment[key] = "${" + key + ":?Start Traefik with devkit proxy up}"
		}
	}
	var command []string
	for i, a := range addrs {
		command = append(command, "--entrypoints."+entrypointName(i)+".address="+net.JoinHostPort(a, "443"))
	}
	compose = map[string]any{
		"name": proxyProject,
		"services": map[string]any{"traefik": map[string]any{
			"image":        image,
			"restart":      "unless-stopped",
			"network_mode": "host",
			"command": append(command,
				"--providers.docker=true",
				"--providers.docker.exposedbydefault=false",
				"--providers.docker.network="+proxyNetwork,
				"--providers.file.filename=/etc/traefik/dynamic.yaml",
				"--api.dashboard=false",
				"--certificatesresolvers.dns.acme.email="+e.Email,
				"--certificatesresolvers.dns.acme.storage="+storage,
				"--certificatesresolvers.dns.acme.caserver="+ca,
				"--certificatesresolvers.dns.acme.dnschallenge.provider="+e.DNSProvider,
				"--certificatesresolvers.dns.acme.dnschallenge.resolvers=1.1.1.1:53,8.8.8.8:53",
			),
			"environment": environment,
			"volumes": []string{
				"/var/run/docker.sock:/var/run/docker.sock:ro",
				"certificates:/certs",
				"./dynamic.yaml:/etc/traefik/dynamic.yaml:ro",
			},
		}},
		"volumes": map[string]any{"certificates": map[string]any{}},
	}
	for _, source := range e.DNSEnv {
		if source.OpenBao != "" {
			service := compose["services"].(map[string]any)["traefik"].(map[string]any)
			service["volumes"] = append(service["volumes"].([]string), secretRuntimeDir()+":/run/devkit-secrets:ro")
			break
		}
	}
	routers := map[string]any{"devkit": map[string]any{
		"rule": "Host(`devkit." + dom + "`)", "service": "devkit", "tls": map[string]any{}}}
	services := map[string]any{"devkit": map[string]any{
		"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": fmt.Sprintf("http://127.0.0.1:%d", cfg.Web.Port)}}}}}
	if h := cfg.Harness; h != nil && h.Host != "" && h.URL != "" {
		routers["harness"] = map[string]any{
			"rule": "Host(`" + h.Host + "." + dom + "`)", "service": "harness", "tls": map[string]any{}}
		services["harness"] = map[string]any{
			"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": h.URL}}}}
	}
	if vaultEnabled(cfg) {
		routers["vault"] = map[string]any{"rule": "Host(`vault.devkit." + dom + "`)", "service": "vault", "entryPoints": vaultPrivateEntrypoints(addrs), "middlewares": []string{"vault-private"}, "tls": map[string]any{"certResolver": "dns", "domains": []any{map[string]string{"main": "vault.devkit." + dom}}}}
		services["vault"] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"url": cfg.Secrets.Address}}}}
	}
	dynamic = map[string]any{
		// The default certificate is the wildcard; it covers one level below the domain, not the domain itself.
		"tls": map[string]any{"stores": map[string]any{"default": map[string]any{"defaultGeneratedCert": map[string]any{
			"resolver": "dns", "domain": map[string]any{"main": "*." + dom}}}}},
		"http": map[string]any{"routers": routers, "services": services},
	}
	if vaultEnabled(cfg) {
		dynamic["http"].(map[string]any)["middlewares"] = map[string]any{"vault-private": map[string]any{"ipAllowList": map[string]any{"sourceRange": []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"}}}}
	}
	return compose, dynamic
}

// checkHarness validates the harness section before it reaches Traefik.
func checkHarness(h *harnessConfig) error {
	if h == nil {
		return nil
	}
	if h.Host != "" && !domainLabel.MatchString(h.Host) {
		return fail("harness.host %q must be a single DNS label.", h.Host)
	}
	if h.URL != "" && !regexp.MustCompile(`^https?://[A-Za-z0-9.:\[\]-]+(/[A-Za-z0-9._~/-]*)?$`).MatchString(h.URL) {
		return fail("harness.url %q must be a plain http(s) URL.", h.URL)
	}
	return nil
}

// writeProxyFiles writes the project files; JSON is valid YAML for both Compose and Traefik.
// The files are rewritten in place: Traefik watches dynamic.yaml through a bind mount of the file,
// which a rename would detach.
func writeProxyFiles(compose, dynamic map[string]any) error {
	if err := os.MkdirAll(proxyDir(), 0o700); err != nil {
		return err
	}
	for name, value := range map[string]map[string]any{"compose.yaml": compose, "dynamic.yaml": dynamic} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(proxyDir(), name), append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// ensureProxyNetwork creates the shared network previews and Traefik meet on.
func ensureProxyNetwork() error {
	if _, err := probe("docker", "network", "inspect", "--format", "{{.Name}}", proxyNetwork); err == nil {
		return nil
	}
	_, err := runCommand([]string{"docker", "network", "create", proxyNetwork}, runOpts{capture: true})
	return err
}

// proxyUp is devkit proxy up: (re)write Traefik's files and start or update it.
func proxyUp() error {
	cfg, err := loadHostConfig()
	if err != nil {
		return err
	}
	if err := checkProxy(cfg.Proxy); err != nil {
		return err
	}
	if err := checkHarness(cfg.Harness); err != nil {
		return err
	}
	dom, err := domain()
	if err != nil {
		return err
	}
	ip, err := tailscaleIP()
	if err != nil {
		return err
	}
	addrs := listenAddresses(cfg.Proxy, ip)
	if err := checkListen(addrs); err != nil {
		return err
	}
	if vaultEnabled(cfg) && len(vaultPrivateEntrypoints(addrs)) == 0 {
		return fail("OpenBao needs an explicit private LAN/Tailscale proxy listener")
	}
	secrets := map[string]string{}
	for _, key := range sortedKeys(cfg.Proxy.DNSEnv) {
		if vaultEnabled(cfg) && cfg.Proxy.DNSEnv[key].OpenBao == "" {
			return fail("Managed DNS needs OpenBao references; use vault migrate --source dns")
		}
		if secrets[key], err = cfg.Proxy.DNSEnv[key].read(); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	for key, source := range cfg.Proxy.DNSEnv {
		if source.OpenBao != "" {
			if cfg.Proxy.DNSProvider != "cloudflare" {
				return fail("Managed DNS file delivery currently supports Cloudflare; retain local provider for other DNS consumers")
			}
			if _, err := writeRuntimeSecret("dns-"+key, secrets[key]); err != nil {
				return err
			}
			delete(secrets, key)
		}
	}
	if err := ensureProxyNetwork(); err != nil {
		return err
	}
	compose, dynamic := proxyFiles(cfg, dom, addrs)
	if err := writeProxyFiles(compose, dynamic); err != nil {
		return err
	}
	args := []string{"docker", "compose", "--project-directory", proxyDir(), "-f", filepath.Join(proxyDir(), "compose.yaml"),
		"up", "-d", "--remove-orphans"}
	if _, err := runCommand(args, runOpts{env: withEnv(os.Environ(), secrets)}); err != nil {
		return err
	}
	ca := "production"
	if cfg.Proxy.Staging {
		ca = "staging"
	}
	where := make([]string, len(addrs))
	for i, a := range addrs {
		where[i] = net.JoinHostPort(a, "443")
	}
	fmt.Fprintf(stdout, "Traefik is up on %s for *.%s (Let's Encrypt %s).\n", strings.Join(where, ", "), dom, ca)
	if note := listenNote(addrs, ip, dom); note != "" {
		fmt.Fprintln(stdout, note)
	}
	happened("proxy up", "", fmt.Sprintf("*.%s on %s, %s certificates", dom, strings.Join(addrs, ", "), ca))
	return nil
}
