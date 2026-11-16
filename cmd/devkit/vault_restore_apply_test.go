package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Real directory swaps and journals, bounded synthetic API and container inventory.
// No Docker, host service or real credentials are used by these interruption tests.
func TestVaultRestoreInterruptionRollback(t *testing.T) {
	for _, phase := range []string{"prepared", "source-stopped", "old-moved", "new-installed", "metadata-installed"} {
		t.Run(phase, func(t *testing.T) {
			cfg, id := restoreFixture(t)
			var sealed atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/sys/seal-status":
					json.NewEncoder(w).Encode(map[string]any{"initialized": true, "sealed": sealed.Load(), "t": 1})
				case "/v1/sys/unseal":
					sealed.Store(false)
					w.Write([]byte(`{}`))
				case "/v1/auth/userpass/login/admin":
					w.Write([]byte(`{"auth":{"client_token":"synthetic-admin"}}`))
				case "/v1/auth/token/create":
					w.Write([]byte(`{"auth":{"client_token":"synthetic-backend"}}`))
				case "/v1/auth/token/lookup-self":
					w.Write([]byte(`{"data":{"policies":["devkit-backend"]}}`))
				case "/v1/auth/token/revoke-self":
					w.WriteHeader(204)
				case "/v1/kv/data/devkit/system/web-session-signing-key":
					if r.Method == "GET" {
						w.Write([]byte(`{"data":{"data":{"value":"old-key"},"metadata":{"version":1}}}`))
					} else {
						w.Write([]byte(`{"data":{"version":2}}`))
					}
				default:
					t.Errorf("unexpected API path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cfg.Address = server.URL
			if e := saveVaultConfig(cfg); e != nil {
				t.Fatal(e)
			}
			b, e := inspectRestoreBackup(cfg, id)
			if e != nil {
				t.Fatal(e)
			}
			r := secretRegistry{Schema: 1, Installation: b.Manifest.Installation, Resources: map[string]*secretResource{}, Keys: map[string]*secretKey{}}
			if e = saveSecretRegistry(&r); e != nil {
				t.Fatal(e)
			}
			if e = saveCleanupConfig(defaultCleanupConfig()); e != nil {
				t.Fatal(e)
			}
			if e = privateDir(restoreDataPath()); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(restoreDataPath(), "old"), []byte("previous encrypted data"), 0600); e != nil {
				t.Fatal(e)
			}
			root, e := os.MkdirTemp(filepath.Dir(restoreDataPath()), "candidate-")
			if e != nil {
				t.Fatal(e)
			}
			candidate := &restoreCandidate{Operation: secretID(), Root: root, Data: filepath.Join(root, "data"), Backend: "new-backend"}
			os.Mkdir(candidate.Data, 0700)
			os.WriteFile(filepath.Join(candidate.Data, "new"), []byte("tested encrypted data"), 0600)
			host, e := loadHostConfig()
			if e != nil {
				t.Fatal(e)
			}
			plain := &restoreContents{Registry: r, Cleanup: defaultCleanupConfig(), Host: restoreHostMetadata{Domain: host.Domain, DataDirectory: restoreDataPath()}}
			var row container
			row.ID = strings.Repeat("c", 64)
			row.Config.Image = cfg.Image
			row.Config.Labels = map[string]string{"com.docker.compose.project": restoreManagedProject(), "com.docker.compose.service": "openbao"}
			row.State.Running = true
			raw, _ := json.Marshal([]map[string]any{{"Type": "bind", "Source": restoreDataPath(), "Destination": "/bao/data", "RW": true}, {"Type": "bind", "Source": filepath.Join(vaultDir(), "openbao", "server.json"), "Destination": "/bao/config/server.json", "RW": false}})
			json.Unmarshal(raw, &row.Mounts)
			swap(t, &hostContainers, func(context.Context) ([]container, error) { return []container{row}, nil })
			swap(t, &restoreWebStopped, func(context.Context) error { return nil })
			swap(t, &restoreCommand, func(_ context.Context, args ...string) (string, error) {
				switch strings.Join(args[:2], " ") {
				case "docker ps", "docker network":
					return "", nil
				case "docker context":
					return "unix:///var/run/docker.sock", nil
				case "docker stop":
					row.State.Running = false
					return "", nil
				case "docker compose":
					row.State.Running = true
					sealed.Store(true)
					return "", nil
				}
				return "", fail("unexpected recovery command")
			})
			swap(t, &restoreCheckpoint, func(got string) error {
				if got == phase {
					return fail("injected interruption")
				}
				return nil
			})
			input := &restoreCredentials{Share: func(vaultStatus) (string, error) { return "synthetic-share", nil }, Login: func() (string, string, error) { return "admin", "synthetic-password", nil }}
			op, e := applyVaultRestore(context.Background(), cfg, b, plain, candidate, input, "")
			if e == nil || op != candidate.Operation {
				t.Fatalf("missing interruption: op=%s error=%v", op, e)
			}
			if e = vaultRestoreGuard(); e == nil {
				t.Fatal("interruption did not block secrets")
			}
			if e = rollbackVaultRestore(context.Background(), strings.Repeat("f", 32), input); e == nil {
				t.Fatal("stale operation accepted")
			}
			if e = rollbackVaultRestore(context.Background(), op, input); e != nil {
				t.Fatal(e)
			}
			if e = vaultRestoreGuard(); e != nil {
				t.Fatal("rollback did not release guard", e)
			}
			if raw, e := os.ReadFile(filepath.Join(restoreDataPath(), "old")); e != nil || string(raw) != "previous encrypted data" {
				t.Fatal("old data not restored")
			}
			if phase == "new-installed" || phase == "metadata-installed" {
				if _, e = os.Stat(filepath.Join(restoreOperationRoot(op), "replaced-data", "new")); e != nil {
					t.Fatal("replacement discarded")
				}
			} else if _, e = os.Stat(filepath.Join(candidate.Data, "new")); e != nil {
				t.Fatal("candidate discarded")
			}
			if e = rollbackVaultRestore(context.Background(), op, input); e != nil {
				t.Fatal("rollback retry failed", e)
			}
		})
	}
}
