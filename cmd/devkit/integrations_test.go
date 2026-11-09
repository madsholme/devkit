package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIntegrationRuntimeFileAndVaultProxy(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	swap(t, &runtimeMemoryBacked, func(string) bool { return true })
	file, e := writeRuntimeSecret("dns-CF_DNS_API_TOKEN", "sentinel-password")
	if e != nil {
		t.Fatal(e)
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0600 {
		t.Fatal("runtime secret permissions")
	}
	b, _ := os.ReadFile(file)
	if string(b) != "sentinel-password" {
		t.Fatal("wrong file content")
	}
	swap(t, &runtimeMemoryBacked, func(string) bool { return false })
	if _, e := writeRuntimeSecret("other", "secret"); e == nil {
		t.Fatal("persistent fallback accepted")
	}
	v := defaultVaultConfig()
	cfg := hostConfig{Secrets: &v, Proxy: &proxyConfig{DNSProvider: "cloudflare", DNSEnv: map[string]secretSource{"CF_DNS_API_TOKEN": {OpenBao: "openbao://devkit/integrations/cloudflare/token"}}}}
	compose, dynamic := proxyFiles(cfg, "dev.example.com", []string{"100.64.0.1", "192.168.1.20"})
	data, _ := json.Marshal(compose)
	if strings.Contains(string(data), "sentinel-password") || !strings.Contains(string(data), "CF_DNS_API_TOKEN_FILE") {
		t.Fatalf("unsafe rendered env %s", data)
	}
	data, _ = json.Marshal(dynamic)
	if !strings.Contains(string(data), "vault.devkit.dev.example.com") || !strings.Contains(string(data), "127.0.0.1:8210") || !strings.Contains(string(data), "sourceRange") {
		t.Fatalf("missing private vault TLS %s", data)
	}
	router := dynamic["http"].(map[string]any)["routers"].(map[string]any)["vault"].(map[string]any)
	tls := router["tls"].(map[string]any)
	if tls["certResolver"] != "dns" || tls["domains"].([]any)[0].(map[string]string)["main"] != "vault.devkit.dev.example.com" {
		t.Fatal("vault must request its exact hostname certificate through the existing DNS resolver")
	}
	if !slices.Equal(router["entryPoints"].([]string), []string{"websecure", "websecure2"}) || !slices.Equal(router["middlewares"].([]string), []string{"vault-private"}) {
		t.Fatal("vault must serve both private LAN and Tailscale listeners with the private allowlist")
	}
	if _, e := writeRuntimeSecret("../escape", "secret"); e == nil {
		t.Fatal("unsafe runtime filename accepted")
	}
	if _, e := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "escape")); e == nil {
		t.Fatal("wrote outside runtime dir")
	}
}

func TestVaultBackedSessionDoesNotFallBackAndTunnelTokenUsesFile(t *testing.T) {
	secretFixture(t)
	if e := os.WriteFile(filepath.Join(configDir(), "web-secret"), []byte(strings.Repeat("old", 20)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := sessionSecret(); e == nil {
		t.Fatal("missing vault signing key fell back to local key")
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	swap(t, &runtimeMemoryBacked, func(string) bool { return true })
	var args []string
	var env []string
	swap(t, &runCommand, func(a []string, o runOpts) (string, error) { args = a; env = o.env; return "container-id", nil })
	_, e := startCloudflared(shareConfig{Image: defaultCloudflared}, container{}, share{Preview: "example"}, "tunnel-SENTINEL")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(args, " "), fmt.Sprintf("--user %d:%d", os.Getuid(), os.Getgid())) {
		t.Fatal("tunnel process cannot read its owner-only token file")
	}
	if strings.Contains(strings.Join(args, " "), "SENTINEL") || strings.Contains(strings.Join(env, " "), "SENTINEL") || !strings.Contains(strings.Join(args, " "), "--token-file") {
		t.Fatal("tunnel token leaked or not delivered through file")
	}
}

func TestIntegrationManagedProviderRejectsLocalConsumerSources(t *testing.T) {
	secretFixture(t)
	raw, _ := readConfigFile()
	raw["share"] = map[string]any{"domain": "share.example.test", "account_id": "account", "token": map[string]string{"file": filepath.Join(configDir(), "legacy")}}
	if e := writeConfigFile(raw); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(configDir(), "legacy"), []byte("legacy-value"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e := loadShareConfig(); e == nil {
		t.Fatal("managed provider read legacy plaintext consumer token")
	}
}
