package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loginTestRequest(t *testing.T, root string, body string) secretRequest {
	t.Helper()
	var q secretRequest
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatal(err)
	}
	q.Root = root
	return q
}

func TestSecretLoginStoresPairAndRevealsOneField(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(body string) secretResponse {
		t.Helper()
		out, err := executeSecret(context.Background(), loginTestRequest(t, repo.root, body))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	run(`{"operation":"register"}`)
	run(`{"operation":"set","scope":"worktree","key":"dev-admin","type":"login","username":"user-SENTINEL","password":"password-SENTINEL","username_env":"APP_USERNAME","password_env":"APP_PASSWORD"}`)
	metadata := run(`{"operation":"list"}`)
	encoded, _ := json.Marshal(metadata)
	if strings.Contains(string(encoded), "SENTINEL") || !strings.Contains(string(encoded), `"type":"login"`) {
		t.Fatalf("invalid metadata: %s", encoded)
	}
	if got := run(`{"operation":"get","key":"dev-admin","reveal":true,"field":"username"}`).Value; got != "user-SENTINEL" {
		t.Fatalf("username: %q", got)
	}
	if got := run(`{"operation":"get","key":"dev-admin","reveal":true,"field":"password"}`).Value; got != "password-SENTINEL" {
		t.Fatal("password differs")
	}
	if _, err := executeSecret(context.Background(), loginTestRequest(t, repo.root, `{"operation":"get","key":"dev-admin","reveal":true}`)); err == nil {
		t.Fatal("login revealed without a field")
	}
	injected := run(`{"operation":"inject"}`)
	if injected.Values["APP_USERNAME"] != "user-SENTINEL" || injected.Values["APP_PASSWORD"] != "password-SENTINEL" || len(injected.Values) != 2 {
		t.Fatal("login mapping failed")
	}
	run(`{"operation":"rotate","scope":"worktree","key":"dev-admin","expected":1,"length":32}`)
	if got := run(`{"operation":"get","key":"dev-admin","field":"username","reveal":true}`).Value; got != "user-SENTINEL" {
		t.Fatal("rotation changed username")
	}
	if got := run(`{"operation":"get","key":"dev-admin","field":"password","reveal":true}`).Value; len(got) != 32 || got == "password-SENTINEL" {
		t.Fatal("rotation failed")
	}
	registry, err := os.ReadFile(filepath.Join(secretStateDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registry), "SENTINEL") {
		t.Fatal("registry contains protected fields")
	}
	var r secretRegistry
	if json.Unmarshal(registry, &r) != nil || r.Schema != 2 {
		t.Fatal("login did not select compatible registry schema")
	}
	run(`{"operation":"update","scope":"worktree","key":"dev-admin","type":"login","username":"new-user","password":"new-password","expected":2,"clear_env":true}`)
	if got := run(`{"operation":"inject"}`); len(got.Values) != 0 {
		t.Fatal("explicit no-env retained login mappings")
	}
	run(`{"operation":"set","scope":"worktree","key":"API_TOKEN","value":"token-SENTINEL"}`)
	if got := run(`{"operation":"inject"}`); len(got.Values) != 1 || got.Values["API_TOKEN"] != "token-SENTINEL" {
		t.Fatal("single values stopped working after typed registry upgrade")
	}
}

func TestSecretLoginCLIOptionsAndProtectedInput(t *testing.T) {
	q, _, _, err := parseSecretsArgs([]string{"generate", "dev-admin", "--type", "login", "--scope", "worktree", "--username-env", "APP_USER", "--password-env", "APP_PASS", "--stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != "login" || q.UsernameEnv != "APP_USER" || q.PasswordEnv != "APP_PASS" {
		t.Fatal("login flags missing")
	}
	q, _, _, err = parseSecretsArgs([]string{"get", "dev-admin", "--field", "username", "--reveal"})
	if err != nil || q.Field != "username" {
		t.Fatal("field flag missing")
	}
}

func TestSecretLoginInheritanceRecoveryAndConflicts(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(body string) secretResponse {
		t.Helper()
		out, err := executeSecret(context.Background(), loginTestRequest(t, repo.root, body))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	reject := func(body string) {
		t.Helper()
		if _, err := executeSecret(context.Background(), loginTestRequest(t, repo.root, body)); err == nil {
			t.Fatal("expected rejection")
		}
	}
	run(`{"operation":"register"}`)
	run(`{"operation":"set","scope":"project","key":"login","type":"login","username":"parent-user","password":"parent-password"}`)
	if got := run(`{"operation":"inject"}`); len(got.Values) != 0 {
		t.Fatal("unmapped login injected")
	}
	run(`{"operation":"generate","scope":"worktree","key":"login","type":"login","username":"child-user","username_env":"APP_USER","password_env":"APP_PASS"}`)
	if got := run(`{"operation":"get","key":"login","field":"username","reveal":true}`).Value; got != "child-user" {
		t.Fatal("child not selected")
	}
	if got := run(`{"operation":"get","scope":"project","key":"login","field":"username","reveal":true}`).Value; got != "parent-user" {
		t.Fatal("parent changed")
	}
	reject(`{"operation":"update","scope":"worktree","key":"login","type":"value","value":"wrong","expected":1}`)
	reject(`{"operation":"rotate","scope":"worktree","key":"login","expected":0}`)
	preview := run(`{"operation":"delete","scope":"worktree","key":"login"}`)
	q := loginTestRequest(t, repo.root, `{"operation":"delete","scope":"worktree","key":"login"}`)
	q.Confirm = preview.Confirmation
	if _, err := executeSecret(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if got := run(`{"operation":"get","key":"login","field":"username","reveal":true}`).Value; got != "parent-user" {
		t.Fatal("delete did not expose inherited parent")
	}
	preview = run(`{"operation":"restore","scope":"worktree","key":"login"}`)
	q.Operation = "restore"
	q.Confirm = preview.Confirmation
	if _, err := executeSecret(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if got := run(`{"operation":"get","key":"login","field":"username","reveal":true}`).Value; got != "child-user" {
		t.Fatal("restore lost login")
	}
	run(`{"operation":"set","scope":"worktree","key":"APP_USER","value":"collision"}`)
	reject(`{"operation":"inject"}`)
	for _, body := range []string{
		`{"operation":"set","scope":"worktree","key":"bad","type":"login","username":"u","password":"p","username_env":"DEVKIT_TOKEN","password_env":"APP_PASS"}`,
		`{"operation":"set","scope":"worktree","key":"bad","type":"login","username":"u","password":"p","username_env":"USER","password_env":"USER"}`,
		`{"operation":"set","scope":"worktree","key":"bad","type":"login","username":"u","password":"p","username_env":"USER"}`,
		`{"operation":"set","scope":"worktree","key":"bad","type":"unknown","value":"p"}`,
	} {
		reject(body)
	}
}

func TestSecretLoginReconcileRestoresOnlyMetadata(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(body string) secretResponse {
		t.Helper()
		out, err := executeSecret(context.Background(), loginTestRequest(t, repo.root, body))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ctx := run(`{"operation":"register"}`).Context
	run(`{"operation":"set","scope":"worktree","key":"login","type":"login","username":"user-SENTINEL","password":"pass-SENTINEL","username_env":"APP_USER","password_env":"APP_PASS"}`)
	r, err := loadSecretRegistry()
	if err != nil {
		t.Fatal(err)
	}
	c, err := managedVaultClient()
	if err != nil {
		t.Fatal(err)
	}
	path, err := resourceKVPath(r.Resources[ctx.Worktree], "login")
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := c.call(context.Background(), "GET", "kv/data/"+path, nil, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Data.Data["username"] != "user-SENTINEL" || raw.Data.Data["password"] != "pass-SENTINEL" || raw.Data.Data["value"] != "" {
		t.Fatal("native KV does not contain paired fields")
	}
	delete(r.Keys, secretKeyID(ctx.Worktree, "login"))
	r.Pending = &secretOperation{ID: raw.Data.Data["operation_id"], Kind: "write", Resource: ctx.Worktree, Key: "login", Expected: 0}
	if err := saveSecretRegistry(&r); err != nil {
		t.Fatal(err)
	}
	run(`{"operation":"reconcile"}`)
	r, err = loadSecretRegistry()
	if err != nil {
		t.Fatal(err)
	}
	k := r.Keys[secretKeyID(ctx.Worktree, "login")]
	if k == nil || k.Type != "login" || k.UsernameEnv != "APP_USER" || k.PasswordEnv != "APP_PASS" {
		t.Fatal("reconciliation lost typed metadata")
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "SENTINEL") {
		t.Fatal("reconciliation leaked fields")
	}
}

func TestSecretLoginJSONInputAndLegacyRegistry(t *testing.T) {
	q := secretRequest{Operation: "set"}
	if err := readLoginInput(strings.NewReader(`{"username":"user","password":"pass"}`), &q); err != nil || q.Username != "user" || q.Password != "pass" {
		t.Fatal("protected JSON input failed")
	}
	for _, input := range []string{`{"username":"user","unknown":"pass"}`, `{"username":"user"} {}`, `not JSON`} {
		if err := readLoginInput(strings.NewReader(input), &q); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	q.Operation = "generate"
	if err := readLoginInput(strings.NewReader(`{"username":"user","password":"pass"}`), &q); err == nil {
		t.Fatal("generate accepted a password")
	}
	secretFixture(t)
	repo := newGitRepo(t)
	if _, err := executeSecret(context.Background(), secretRequest{Root: repo.root, Operation: "register"}); err != nil {
		t.Fatal(err)
	}
	if _, err := executeSecret(context.Background(), secretRequest{Root: repo.root, Operation: "set", Scope: "project", Key: "LEGACY", Value: "legacy"}); err != nil {
		t.Fatal(err)
	}
	r, err := loadSecretRegistry()
	if err != nil || r.Schema != 1 {
		t.Fatal("ordinary values unnecessarily changed registry schema")
	}
	out, err := executeSecret(context.Background(), secretRequest{Root: repo.root, Operation: "get", Key: "LEGACY", Reveal: true})
	if err != nil || out.Value != "legacy" {
		t.Fatal("legacy value changed")
	}
}
