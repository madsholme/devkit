package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func secretAPICall(t *testing.T, token string, q secretRequest) (int, secretResponse) {
	t.Helper()
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "https://devkit.test/api/secrets/v1", strings.NewReader(string(b)))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	secretsAPIHandler(w, req)
	var out secretResponse
	if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("invalid API JSON")
	}
	return w.Code, out
}
func TestSecretAPIInheritanceWritesAndCleanup(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	local := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, err := executeSecret(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	local(secretRequest{Operation: "register"})
	local(secretRequest{Operation: "set", Scope: "project", Key: "PASSWORD", Value: "parent-SENTINEL"})
	token, raw, err := createSecretToken(repo.root, "app", "worktree", []string{"read", "write", "cleanup"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	call := func(q secretRequest) secretResponse {
		t.Helper()
		code, out := secretAPICall(t, raw, q)
		if code != 200 {
			t.Fatalf("%s status %d", q.Operation, code)
		}
		return out
	}
	if got := call(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}).Value; got != "parent-SENTINEL" {
		t.Fatal("inheritance lost")
	}
	call(secretRequest{Operation: "set", Key: "PASSWORD", Value: "child-SENTINEL"})
	if got := call(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}).Value; got != "child-SENTINEL" {
		t.Fatal("override lost")
	}
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "update", Key: "PASSWORD", Value: "stale"}); code != 400 {
		t.Fatal("CAS bypass")
	}
	preview := call(secretRequest{Operation: "delete", Key: "PASSWORD"})
	if preview.Confirmation == "" {
		t.Fatal("missing confirmation")
	}
	if got := call(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}).Value; got != "child-SENTINEL" {
		t.Fatal("preview deleted value")
	}
	call(secretRequest{Operation: "delete", Key: "PASSWORD", Confirm: preview.Confirmation})
	if got := call(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}).Value; got != "parent-SENTINEL" {
		t.Fatal("deleted override did not inherit")
	}
	preview = call(secretRequest{Operation: "restore", Key: "PASSWORD"})
	call(secretRequest{Operation: "restore", Key: "PASSWORD", Confirm: preview.Confirmation})
	pair := secretRequest{Operation: "generate", Key: "admin", Username: "user-SENTINEL"}
	pair.Type = "login"
	call(pair)
	if got := call(secretRequest{Operation: "get", Key: "admin", Field: "username", Reveal: true}).Value; got != "user-SENTINEL" {
		t.Fatal("pair username failed")
	}
	if got := call(secretRequest{Operation: "get", Key: "admin", Field: "password", Reveal: true}).Value; len(got) != 32 {
		t.Fatal("pair generation failed")
	}
	preview = call(secretRequest{Operation: "cleanup-retire"})
	call(secretRequest{Operation: "cleanup-retire", Confirm: preview.Confirmation})
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "list"}); code == 200 {
		t.Fatal("retired context accessible")
	}
	preview = call(secretRequest{Operation: "cleanup-restore"})
	call(secretRequest{Operation: "cleanup-restore", Confirm: preview.Confirmation})
	if got := call(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}).Value; got != "child-SENTINEL" {
		t.Fatal("scope restore lost value")
	}
	audit, err := os.ReadFile(filepath.Join(secretStateDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), "api-token:"+token.ID) || strings.Contains(string(audit), raw) || strings.Contains(string(audit), "SENTINEL") {
		t.Fatal("audit identity/redaction failed")
	}
}
func TestSecretAPIAuthorizationBoundaries(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	other := newGitRepo(t)
	for _, root := range []string{repo.root, other.root} {
		if _, err := executeSecret(context.Background(), secretRequest{Operation: "register", Root: root}); err != nil {
			t.Fatal(err)
		}
	}
	record, raw, err := createSecretToken(repo.root, "reader", "worktree", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []secretRequest{
		{Operation: "list", Root: other.root}, {Operation: "list", Scope: "project"}, {Operation: "list", Project: record.Context.Project}, {Operation: "list", Branch: record.Context.Branch}, {Operation: "list", Worktree: record.Context.Worktree},
		{Operation: "set", Key: "X", Value: "wrong"}, {Operation: "cleanup-retire"}, {Operation: "cleanup-config"}, {Operation: "cleanup-resume"}, {Operation: "register"}, {Operation: "bind"}, {Operation: "reconcile"}, {Operation: "vault-health"}, {Operation: "agents-install"}, {Operation: "integration-list"},
	} {
		if code, _ := secretAPICall(t, raw, q); code != 403 {
			t.Fatalf("%s accepted: %d", q.Operation, code)
		}
	}
	_, writer, err := createSecretToken(repo.root, "writer", "worktree", []string{"write", "cleanup"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, writer, secretRequest{Operation: "get", Key: "X", Reveal: true}); code != 403 {
		t.Fatal("writer read value")
	}
	if code, _ := secretAPICall(t, writer, secretRequest{Operation: "cleanup-retire", Resource: record.Context.Project}); code != 403 {
		t.Fatal("cleanup escalated to parent")
	}
	code, metadata := secretAPICall(t, raw, secretRequest{Operation: "context"})
	if code != 200 {
		t.Fatal(code)
	}
	if len(metadata.Resources) != 3 {
		t.Fatalf("context ancestry count %d", len(metadata.Resources))
	}
	oldNow := secretsNow
	secretsNow = func() time.Time { return record.Created.Add(2 * time.Hour) }
	code, _ = secretAPICall(t, raw, secretRequest{Operation: "list"})
	secretsNow = oldNow
	if code != 401 {
		t.Fatal("expired token accepted")
	}
	if err := revokeSecretToken(record.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "list"}); code != 401 {
		t.Fatal("revoked token accepted")
	}
	if _, err := git(repo.root, "checkout", "-b", "new-context"); err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, writer, secretRequest{Operation: "set", Key: "X", Value: "wrong"}); code != 403 {
		t.Fatal("token followed branch switch")
	}
}
func TestSecretAPITransportAndParsing(t *testing.T) {
	for _, tc := range []struct {
		name, method, body, auth, origin, remote string
		tls                                      bool
		want                                     int
	}{
		{name: "missing", method: "POST", body: `{}`, tls: true, want: 401},
		{name: "invalid", method: "POST", auth: "Bearer bad", body: `{}`, tls: true, want: 401},
		{name: "http", method: "POST", remote: "192.168.0.2:1234", want: 403},
		{name: "origin", method: "POST", origin: "https://evil.test", tls: true, want: 403},
		{name: "method", method: "GET", tls: true, want: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://devkit.test/api/secrets/v1", strings.NewReader(tc.body))
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-Proto", "https")
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.remote != "" {
				req.RemoteAddr = tc.remote
			}
			w := httptest.NewRecorder()
			secretsAPIHandler(w, req)
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
	secretFixture(t)
	repo := newGitRepo(t)
	executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	_, raw, err := createSecretToken(repo.root, "parser", "project", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"operation":"list","unknown":true}`, `{"operation":"list"} {}`, strings.Repeat("x", 129<<10)} {
		req := httptest.NewRequest("POST", "https://devkit.test/api/secrets/v1", strings.NewReader(body))
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		secretsAPIHandler(w, req)
		if w.Code != 400 {
			t.Fatalf("bad body status %d", w.Code)
		}
	}
	// A sealed API must refuse metadata too, even with a valid credential.
	sealed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"initialized":true,"sealed":true}`)) }))
	defer sealed.Close()
	cfg, err := loadVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Address = sealed.URL
	if err = saveVaultConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "context"}); code != 503 {
		t.Fatalf("sealed metadata status %d", code)
	}
}

func TestSecretAPIProjectAndBranchScope(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	if _, err := git(repo.root, "branch", "sibling"); err != nil {
		t.Fatal(err)
	}
	if _, err := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"project", "branch", "worktree"} {
		_, err := executeSecret(context.Background(), secretRequest{Operation: "set", Root: repo.root, Scope: scope, Key: "SAME", Value: scope})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, scope := range []string{"project", "branch", "worktree"} {
		_, raw, err := createSecretToken(repo.root, "reader-"+scope, scope, []string{"read"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		code, out := secretAPICall(t, raw, secretRequest{Operation: "get", Key: "SAME", Reveal: true})
		if code != 200 || out.Value != scope {
			t.Fatalf("%s context incorrect", scope)
		}
		code, out = secretAPICall(t, raw, secretRequest{Operation: "context"})
		if code != 200 || len(out.Resources) != i+1 || len(out.Counts) != i+1 {
			t.Fatalf("%s leaked sibling metadata", scope)
		}
		if scope == "project" && (out.Context.Branch != "" || out.Context.Worktree != "") {
			t.Fatal("project context exposes child identities")
		}
	}
}

func TestSecretAPIBranchCleanupAfterGitSwitch(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	if _, err := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); err != nil {
		t.Fatal(err)
	}
	_, raw, err := createSecretToken(repo.root, "branch-cleaner", "branch", []string{"read", "cleanup"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "cleanup-retire"}); code != 400 {
		t.Fatal("active branch retirement allowed")
	}
	if _, err := git(repo.root, "checkout", "-b", "replacement"); err != nil {
		t.Fatal(err)
	}
	if code, _ := secretAPICall(t, raw, secretRequest{Operation: "list"}); code != 403 {
		t.Fatal("read followed branch change")
	}
	for _, operation := range []string{"cleanup-retire", "cleanup-restore"} {
		code, preview := secretAPICall(t, raw, secretRequest{Operation: operation})
		if code != 200 || preview.Confirmation == "" {
			t.Fatalf("%s preview %d", operation, code)
		}
		if code, _ := secretAPICall(t, raw, secretRequest{Operation: operation, Confirm: preview.Confirmation}); code != 200 {
			t.Fatalf("%s confirmation %d", operation, code)
		}
	}
}
