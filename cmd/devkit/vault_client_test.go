package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVaultClientKVAndRedactedErrors(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-backend-token" {
			t.Error("missing authentication")
		}
		paths = append(paths, r.URL.Path)
		switch r.Method {
		case "GET":
			if strings.Contains(r.URL.Path, "denied") {
				w.WriteHeader(403)
				w.Write([]byte(`{"errors":["secret-SENTINEL"]}`))
				return
			}
			w.Write([]byte(`{"data":{"data":{"value":"saved-password"},"metadata":{"version":3}}}`))
		case "POST":
			var body struct {
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Options.CAS != 3 || body.Data["value"] != "new-password" {
				t.Errorf("wrong CAS/write: %+v", body)
			}
			w.Write([]byte(`{"data":{"version":4}}`))
		}
	}))
	defer server.Close()
	c, err := newVaultClient(server.URL, "test-backend-token")
	if err != nil {
		t.Fatal(err)
	}
	value, version, err := c.read(context.Background(), "projects/p/shared/PASSWORD")
	if err != nil || value != "saved-password" || version != 3 {
		t.Fatalf("read: %q %d %v", value, version, err)
	}
	if _, err = c.write(context.Background(), "projects/p/shared/PASSWORD", "new-password", 3); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.read(context.Background(), "devkit/system/denied")
	if err == nil || strings.Contains(err.Error(), "SENTINEL") || strings.Contains(err.Error(), "test-backend-token") {
		t.Fatalf("unsafe error: %v", err)
	}
	before := len(paths)
	for _, key := range []string{"../system/key", "projects//key", "projects/p?token=x", "projects/p%2fkey", "/devkit/key"} {
		if _, _, err = c.read(context.Background(), key); err == nil {
			t.Errorf("accepted unsafe path %q", key)
		}
	}
	if len(paths) != before {
		t.Error("unsafe paths reached network")
	}
}

func TestVaultClientRejectsRedirectAndSealed(t *testing.T) {
	hit := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "seal-status") {
			w.Write([]byte(`{"initialized":true,"sealed":true,"version":"2.7.2"}`))
			return
		}
		http.Redirect(w, r, destination.URL, 302)
	}))
	defer server.Close()
	c, _ := newVaultClient(server.URL, "token")
	status, err := c.status(context.Background())
	if err != nil || status.State != "sealed" {
		t.Fatalf("status %+v %v", status, err)
	}
	if _, _, err = c.read(context.Background(), "projects/p/shared/X"); err == nil || hit {
		t.Fatal("redirect followed or accepted")
	}
	for _, addr := range []string{"http://remote.example:8200", "http://user:pass@127.0.0.1:8200", "http://127.0.0.1:8200/?x=y"} {
		if _, err := newVaultClient(addr, "token"); err == nil {
			t.Errorf("accepted unsafe address %s", addr)
		}
	}
}

func TestVaultConfigRenderingAndPrivateStorage(t *testing.T) {
	cfg := defaultVaultConfig()
	compose, configuration := vaultFiles(cfg, t.TempDir())
	b, _ := json.Marshal(compose)
	s := string(b)
	if !strings.Contains(s, "/openbao/data:/bao/data") || strings.Contains(s, "-dev") || !strings.Contains(s, "512m") {
		t.Fatalf("unsafe container: %s", s)
	}
	listenerJSON, _ := json.Marshal(configuration["listener"])
	if !strings.Contains(string(listenerJSON), "127.0.0.1:8210") {
		t.Fatal("listener not loopback")
	}
	if configuration["ui"] != true || configuration["storage"] == nil {
		t.Fatalf("missing persistent server configuration: %+v", configuration)
	}
	if err := validateVaultConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Address = "http://0.0.0.0:8200"
	if err := validateVaultConfig(cfg); err == nil {
		t.Fatal("accepted public vault listener")
	}
}

func TestVaultBindConfigurationUsesAdjacentPrivateClusterPort(t *testing.T) {
	cfg := defaultVaultConfig()
	cfg.Address = "http://127.0.0.1:18210"
	_, config := vaultFiles(cfg, t.TempDir())
	if config["cluster_addr"] != "https://127.0.0.1:18211" {
		t.Fatalf("cluster port ignored configured API address: %v", config["cluster_addr"])
	}
}

func TestVaultAuditIsConfiguredDeclaratively(t *testing.T) {
	_, config := vaultFiles(defaultVaultConfig(), t.TempDir())
	b, _ := json.Marshal(config["audit"])
	var audit []struct {
		File map[string]struct {
			Options map[string]string `json:"options"`
		} `json:"file"`
	}
	if json.Unmarshal(b, &audit) != nil || len(audit) != 1 || audit[0].File["devkit"].Options["file_path"] != "/bao/data/audit.jsonl" || audit[0].File["devkit"].Options["log_raw"] != "false" {
		t.Fatalf("missing private declarative audit configuration: %s", b)
	}
	if config["unsafe_allow_api_audit_creation"] == true {
		t.Fatal("unsafe audit API enabled")
	}
}
