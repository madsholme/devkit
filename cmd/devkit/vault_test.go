package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultSetupPreservesDataAndRejectsUnknownStorage(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"initialized":false,"sealed":true}`)) }))
	defer server.Close()
	cfg := defaultVaultConfig()
	cfg.Address = server.URL
	if e := saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	calls := 0
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) { calls++; return "", nil })
	if e := startVault(cfg); e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(secretStateDir(), "openbao", "data")
	if e := os.WriteFile(filepath.Join(data, "existing"), []byte("preserve"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := checkVaultInitStorage(); e == nil {
		t.Fatal("initialization allowed for unknown existing storage")
	}
	if e := startVault(cfg); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(data, "existing"))
	if string(b) != "preserve" || calls != 2 {
		t.Fatal("restarted by deleting existing data")
	}
}

func TestVaultReferencesFailClosed(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte(`{"errors":["private-sentinel"]}`))
	}))
	defer server.Close()
	cfg := defaultVaultConfig()
	cfg.Address = server.URL
	if e := saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if e := writePrivate(filepath.Join(vaultDir(), "backend-token"), []byte("backend")); e != nil {
		t.Fatal(e)
	}
	s := secretSource{OpenBao: "openbao://devkit/integrations/cloudflare/token"}
	if v, e := s.read(); e == nil || v != "" || strings.Contains(e.Error(), "sentinel") {
		t.Fatalf("not fail-closed: %q %v", v, e)
	}
	s.Env = "SECRET_FALLBACK"
	t.Setenv("SECRET_FALLBACK", "old-plaintext")
	if v, e := s.read(); e == nil || v != "" {
		t.Fatal("mixed source accepted")
	}
}

func TestVaultProvisionRestrictedIdentity(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	var policy string
	var revoked bool
	rejectBackend := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/mounts" {
			w.Write([]byte(`{"data":{}}`))
			return
		}
		if r.URL.Path == "/v1/sys/auth" {
			w.Write([]byte(`{"data":{}}`))
			return
		}
		if r.URL.Path == "/v1/sys/audit" {
			w.Write([]byte(`{"data":{"devkit/":{"type":"file","options":{"file_path":"/bao/data/audit.jsonl","log_raw":"false"}}}}`))
			return
		}
		if r.URL.Path == "/v1/sys/policies/acl/devkit-backend" {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			policy = b["policy"]
		}
		if r.URL.Path == "/v1/auth/token/lookup-self" {
			if rejectBackend {
				w.WriteHeader(403)
			} else {
				w.Write([]byte(`{"data":{"policies":["devkit-backend"]}}`))
			}
			return
		}
		if r.URL.Path == "/v1/auth/token/create" {
			w.Write([]byte(`{"auth":{"client_token":"restricted-child"}}`))
			return
		}
		if r.URL.Path == "/v1/auth/userpass/login/devkit-admin" {
			w.Write([]byte(`{"auth":{"client_token":"human-session"}}`))
			return
		}
		if r.URL.Path == "/v1/auth/token/revoke-self" && r.Header.Get("X-Vault-Token") == "bootstrap-root" {
			revoked = true
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	c, _ := newVaultClient(server.URL, "bootstrap-root")
	if e := provisionVault(context.Background(), c, "human-password-123"); e == nil || revoked {
		t.Fatal("bootstrap revoked before verifying backend identity")
	}
	rejectBackend = false
	if e := provisionVault(context.Background(), c, "human-password-123"); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(policy, `path "*"`) || strings.Contains(policy, "sys/init") || !strings.Contains(policy, "kv/data/projects/") {
		t.Fatalf("bad backend policy %s", policy)
	}
	b, e := readPrivate(filepath.Join(vaultDir(), "backend-token"), 4096)
	if e != nil || string(b) != "restricted-child" || !revoked {
		t.Fatalf("provision %s %v revoked=%v", b, e, revoked)
	}
}

func TestVaultMigrationVerifiesBeforeSwitchAndPreservesChangedSource(t *testing.T) {
	secretFixture(t)
	file := filepath.Join(configDir(), "dns-token")
	if e := os.WriteFile(file, []byte("legacy-SENTINEL"), 0600); e != nil {
		t.Fatal(e)
	}
	raw, _ := readConfigFile()
	raw["domain"] = "dev.example.test"
	raw["proxy"] = map[string]any{"dns_provider": "cloudflare", "dns_env": map[string]any{"CF_DNS_API_TOKEN": map[string]string{"file": file}}}
	if e := writeConfigFile(raw); e != nil {
		t.Fatal(e)
	}
	swap(t, &verifyImportedCredential, func(kind, value string) error { return errors.New("consumer verification failed") })
	preview, e := migrateVaultCredential("dns", "", true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = migrateVaultCredential("dns", preview.Confirmation, true); e == nil {
		t.Fatal("switched unverified credential")
	}
	cfg, _ := loadHostConfig()
	if cfg.Proxy.DNSEnv["CF_DNS_API_TOKEN"].File != file {
		t.Fatal("changed source on failed verification")
	}
	swap(t, &verifyImportedCredential, func(kind, value string) error {
		if value != "legacy-SENTINEL" {
			t.Fatal("wrong credential")
		}
		return nil
	})
	if _, e = migrateVaultCredential("dns", preview.Confirmation, true); e != nil {
		t.Fatal(e)
	}
	cfg, _ = loadHostConfig()
	if cfg.Proxy.DNSEnv["CF_DNS_API_TOKEN"].OpenBao == "" || exists(file) {
		t.Fatal("verified migration did not retire exact source")
	}
}

func TestVaultInitRecognizesOwnedFreshRaftFiles(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	swap(t, &runCommand, func([]string, runOpts) (string, error) { return "", nil })
	if e := startVault(defaultVaultConfig()); e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(secretStateDir(), "openbao", "data")
	if e := os.Mkdir(filepath.Join(data, "raft"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(data, "vault.db"), []byte("fresh-storage"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := checkVaultInitStorage(); e != nil {
		t.Fatal(e)
	}
}
