package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// devbox proxy up writes the Traefik proxy (Compose project devbox-proxy) from the host config into
// ~/.config/devbox/proxy and starts it. Traefik listens only on the Tailscale IP, gets one wildcard
// certificate for the domain by DNS-01, routes the dashboard and the harness from its file provider
// and previews from Docker labels. The DNS credentials reach the container through the environment
// of docker compose, so they are not written to the proxy directory.

const (
	proxyProject = "devbox-proxy"
	proxyNetwork = "devbox-proxy"
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
		return fail("Traefik is not configured; run devbox setup.")
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
	return nil
}

// proxyFiles renders Traefik's Compose project and file-provider configuration.
func proxyFiles(cfg hostConfig, dom, ip string) (compose, dynamic map[string]any) {
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
		environment[key] = "${" + key + ":?Start Traefik with devbox proxy up}"
	}
	compose = map[string]any{
		"name": proxyProject,
		"services": map[string]any{"traefik": map[string]any{
			"image":        image,
			"restart":      "unless-stopped",
			"network_mode": "host",
			"command": []string{
				"--entrypoints.websecure.address=" + ip + ":443",
				"--providers.docker=true",
				"--providers.docker.exposedbydefault=false",
				"--providers.docker.network=" + proxyNetwork,
				"--providers.file.filename=/etc/traefik/dynamic.yaml",
				"--api.dashboard=false",
				"--certificatesresolvers.dns.acme.email=" + e.Email,
				"--certificatesresolvers.dns.acme.storage=" + storage,
				"--certificatesresolvers.dns.acme.caserver=" + ca,
				"--certificatesresolvers.dns.acme.dnschallenge.provider=" + e.DNSProvider,
				"--certificatesresolvers.dns.acme.dnschallenge.resolvers=1.1.1.1:53,8.8.8.8:53",
			},
			"environment": environment,
			"volumes": []string{
				"/var/run/docker.sock:/var/run/docker.sock:ro",
				"certificates:/certs",
				"./dynamic.yaml:/etc/traefik/dynamic.yaml:ro",
			},
		}},
		"volumes": map[string]any{"certificates": map[string]any{}},
	}
	routers := map[string]any{"devbox": map[string]any{
		"rule": "Host(`devbox." + dom + "`)", "entryPoints": []string{"websecure"}, "service": "devbox", "tls": map[string]any{}}}
	services := map[string]any{"devbox": map[string]any{
		"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": fmt.Sprintf("http://127.0.0.1:%d", cfg.Web.Port)}}}}}
	if h := cfg.Harness; h != nil && h.Host != "" && h.URL != "" {
		routers["harness"] = map[string]any{
			"rule": "Host(`" + h.Host + "." + dom + "`)", "entryPoints": []string{"websecure"}, "service": "harness", "tls": map[string]any{}}
		services["harness"] = map[string]any{
			"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": h.URL}}}}
	}
	dynamic = map[string]any{
		// The default certificate is the wildcard; it covers one level below the domain, not the domain itself.
		"tls": map[string]any{"stores": map[string]any{"default": map[string]any{"defaultGeneratedCert": map[string]any{
			"resolver": "dns", "domain": map[string]any{"main": "*." + dom}}}}},
		"http": map[string]any{"routers": routers, "services": services},
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
	if _, err := capture("docker", "network", "inspect", "--format", "{{.Name}}", proxyNetwork); err == nil {
		return nil
	}
	_, err := runCommand([]string{"docker", "network", "create", proxyNetwork}, runOpts{capture: true})
	return err
}

// proxyUp is devbox proxy up: (re)write Traefik's files and start or update it.
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
	secrets := map[string]string{}
	for _, key := range sortedKeys(cfg.Proxy.DNSEnv) {
		if secrets[key], err = cfg.Proxy.DNSEnv[key].read(); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	if err := ensureProxyNetwork(); err != nil {
		return err
	}
	compose, dynamic := proxyFiles(cfg, dom, ip)
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
	fmt.Fprintf(stdout, "Traefik is up on %s:443 for *.%s (Let's Encrypt %s).\n", ip, dom, ca)
	happened("proxy up", "", fmt.Sprintf("*.%s on %s, %s certificates", dom, ip, ca))
	return nil
}
