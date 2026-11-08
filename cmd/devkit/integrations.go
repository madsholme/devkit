package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var runtimeMemoryBacked = func(path string) bool {
	var s syscall.Statfs_t
	return syscall.Statfs(path, &s) == nil && s.Type == 0x01021994
}

func secretRuntimeDir() string { return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "devkit-secrets") }
func writeRuntimeSecret(name, value string) (string, error) {
	root := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(root) || !vaultSegment.MatchString(name) || name == "." || name == ".." {
		return "", fail("A private XDG_RUNTIME_DIR and safe secret name are required")
	}
	if !runtimeMemoryBacked(root) {
		return "", fail("Secret runtime files require memory-backed XDG_RUNTIME_DIR; no persistent fallback")
	}
	dir := secretRuntimeDir()
	if e := privateDir(dir); e != nil {
		return "", e
	}
	path := filepath.Join(dir, name)
	if e := writePrivate(path, []byte(value)); e != nil {
		return "", e
	}
	return path, nil
}
func vaultPrivateEntrypoints(addrs []string) []string {
	var result []string
	for i, a := range addrs {
		ip, e := netip.ParseAddr(a)
		if e == nil && (ip.IsPrivate() || ip.IsLoopback() || tailscaleRange.Contains(ip)) {
			result = append(result, entrypointName(i))
		}
	}
	return result
}
func vaultEnabled(cfg hostConfig) bool {
	return cfg.Secrets != nil && cfg.Secrets.Provider == "openbao"
}
func integrationPath(service, key string) (string, error) {
	if !vaultSegment.MatchString(service) || !vaultSegment.MatchString(key) || service == ".." || key == ".." {
		return "", fail("Invalid integration/key")
	}
	return "devkit/integrations/" + service + "/" + key, nil
}
func setIntegration(service, key, value string) (secretSource, error) {
	path, e := integrationPath(service, key)
	if e != nil {
		return secretSource{}, e
	}
	c, e := managedVaultClient()
	if e != nil {
		return secretSource{}, e
	}
	_, version, e := c.read(context.Background(), path)
	var ve vaultError
	if e != nil && (!errors.As(e, &ve) || ve.Code != 404) {
		return secretSource{}, e
	}
	if _, e = c.write(context.Background(), path, value, version); e != nil {
		return secretSource{}, e
	}
	got, _, e := c.read(context.Background(), path)
	if e != nil || got != value {
		return secretSource{}, fail("Integration readback verification failed")
	}
	source := secretSource{OpenBao: "openbao://" + path}
	raw, e := readConfigFile()
	if e != nil {
		return secretSource{}, e
	}
	integrations, _ := raw["integrations"].(map[string]any)
	if integrations == nil {
		integrations = map[string]any{}
	}
	keys, _ := integrations[service].(map[string]any)
	if keys == nil {
		keys = map[string]any{}
	}
	keys[key] = source
	integrations[service] = keys
	raw["integrations"] = integrations
	if e = writeConfigFile(raw); e != nil {
		return secretSource{}, e
	}
	return source, nil
}
func integrationsCLI(args []string) (int, error) {
	if len(args) > 0 && args[0] == "git" {
		return gitCredentialsCLI(args[1:])
	}
	if len(args) < 2 {
		return 2, fail("usage: devkit integrations SERVICE configure|status|get|rotate-token [--token-stdin] [--key KEY] [--reveal]")
	}
	service, op := args[0], args[1]
	pos, v, e := splitArgs(args[2:], map[string]bool{"--key": true}, map[string]bool{"--token-stdin": true, "--json": true, "--reveal": true, "--verify": true})
	if e != nil || len(pos) > 0 {
		return 2, fail("Invalid integration arguments")
	}
	key := firstOf(v["--key"], "token")
	path, e := integrationPath(service, key)
	if e != nil {
		return 2, e
	}
	switch op {
	case "configure", "rotate-token":
		var value string
		if _, ok := v["--token-stdin"]; ok {
			b, e := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+1))
			if e != nil || len(b) > 64<<10 {
				return 1, fail("Token input is too large or unreadable")
			}
			value = strings.TrimRight(string(b), "\r\n")
		} else {
			value, e = readSecret("Integration token (hidden): ", bufio.NewReader(os.Stdin))
			if e != nil {
				return 1, e
			}
		}
		if value == "" {
			return 2, fail("Token is empty")
		}
		source, e := setIntegration(service, key, value)
		if e != nil {
			return 1, e
		}
		if service == "cloudflare" && key == "token" {
			raw, e := readConfigFile()
			if e != nil {
				return 1, e
			}
			cfg, e := loadHostConfig()
			if e != nil {
				return 1, e
			}
			if cfg.Proxy == nil {
				return 1, fail("Configure the proxy before binding its DNS token")
			}
			cfg.Proxy.DNSEnv = map[string]secretSource{"CF_DNS_API_TOKEN": source}
			raw["proxy"] = cfg.Proxy
			if e = writeConfigFile(raw); e != nil {
				return 1, e
			}
		}
		happened("integration "+op, service, "stored credential; remote token issuance/revocation is separate")
		fmt.Fprintln(stdout, "Credential saved in OpenBao. Restart affected consumers to use the new value.")
		return 0, nil
	case "status", "get":
		c, e := managedVaultClient()
		if e != nil {
			return 1, e
		}
		value, version, e := c.read(context.Background(), path)
		if e != nil {
			return 1, e
		}
		if _, verify := v["--verify"]; verify {
			if service != "github" {
				return 2, fail("API verification currently supports GitHub; Cloudflare is verified during setup")
			}
			req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
			req.Header.Set("Authorization", "Bearer "+value)
			client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, e := client.Do(req)
			if e != nil {
				return 1, fail("GitHub verification unavailable")
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return 1, fail("GitHub rejected this credential")
			}
		}
		if _, reveal := v["--reveal"]; reveal && op == "get" {
			fmt.Fprintln(stdout, value)
			return 0, nil
		}
		return 0, json.NewEncoder(stdout).Encode(map[string]any{"service": service, "key": key, "source": "OpenBao", "version": version, "value": "********"})
	}
	return 2, fail("Unknown integration operation")
}

// Setup preserves existing local installations until an explicit provider choice.
func setupSecretsProvider(p *prompter, requested string, raw map[string]any) error {
	cfg, e := loadVaultConfig()
	if e != nil {
		return e
	}
	choice := requested
	if choice == "" {
		choice = cfg.Provider
		if len(raw) == 0 {
			choice = "openbao"
		}
		choice, e = p.choose("Secrets provider (OpenBao recommended)", []string{"openbao", "local"}, choice)
		if e != nil {
			return e
		}
	}
	if choice != "openbao" && choice != "local" {
		return fail("Secrets provider must be openbao or local")
	}
	cfg.Provider = choice
	raw["secrets"] = cfg
	if e = writeConfigFile(raw); e != nil {
		return e
	}
	if choice == "local" {
		return nil
	}
	if e = startVault(cfg); e != nil {
		return e
	}
	c, e := managedVaultClient()
	if e != nil {
		return e
	}
	status, e := c.status(context.Background())
	if e != nil || status.State != "ready" {
		return fail("OpenBao is not ready. Run vault init/unseal/provision, then rerun setup; no DNS credentials collected")
	}
	return nil
}
