package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecretsCLIParsingNoValueArguments(t *testing.T) {
	q, _, _, e := parseSecretsArgs([]string{"generate", "PASSWORD", "--scope", "worktree", "--length", "40", "--json"})
	if e != nil || q.Operation != "generate" || q.Length != 40 {
		t.Fatalf("parse %+v %v", q, e)
	}
	for _, args := range [][]string{{"set", "KEY", "plaintext"}, {"set", "KEY", "--value", "plaintext"}, {"get", "../system"}, {"cleanup", "config", "set", "--branch-retention", "-1d"}} {
		if _, _, _, e := parseSecretsArgs(args); e == nil {
			t.Errorf("accepted unsafe args %v", args)
		}
	}
	q, _, _, e = parseSecretsArgs([]string{"cleanup", "config", "set", "--automatic", "off", "--branch-retention", "never"})
	if e != nil || q.Cleanup == nil || q.Cleanup.Automatic || q.Cleanup.Branch != "never" {
		t.Fatalf("cleanup off parse %+v %v", q, e)
	}
}
func TestSecretsAPIRequiresAuthAndNoRetainedReveal(t *testing.T) {
	secretFixture(t)
	req := httptest.NewRequest("POST", "http://devkit.test/api/secrets", strings.NewReader(`{"operation":"list"}`))
	w := httptest.NewRecorder()
	secretsWebHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read status %d", w.Code)
	}
}
func TestSecretsCleanupDisabledPersistsAndKeepsManualRecovery(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		o, e := executeSecret(context.Background(), q)
		if e != nil {
			t.Fatalf("%s %v", q.Operation, e)
		}
		return o
	}
	ctx := run(secretRequest{Operation: "register"}).Context
	run(secretRequest{Operation: "set", Scope: "worktree", Key: "TEMP", Value: "stored-temporary-value"})
	cfg := defaultCleanupConfig()
	cfg.Automatic = false
	cfg.Worktree = "1h"
	q := secretRequest{Operation: "cleanup-config", Cleanup: &cfg}
	preview := run(q)
	q.Confirm = preview.Confirmation
	run(q)
	restored, e := loadCleanupConfig()
	if e != nil || restored.Automatic {
		t.Fatal("off not persisted")
	}
	q = secretRequest{Operation: "cleanup-retire", Resource: ctx.Worktree}
	preview = run(q)
	q.Confirm = preview.Confirmation
	run(q)
	q = secretRequest{Operation: "cleanup-restore", Resource: ctx.Worktree}
	preview = run(q)
	q.Confirm = preview.Confirmation
	run(q)
	got := run(secretRequest{Operation: "get", Key: "TEMP", Reveal: true})
	if got.Value != "stored-temporary-value" {
		b, _ := json.Marshal(got)
		t.Fatalf("manual recovery failed %s", b)
	}
}

func TestSecretsProjectProtocolRejectsOperatorRoutes(t *testing.T) {
	for _, op := range []string{"vault-update", "integration-set", "cleanup-config", "agents-install"} {
		r := httptest.NewRequest("POST", "http://localhost/project", strings.NewReader(`{"operation":"`+op+`"}`))
		w := httptest.NewRecorder()
		secretProtocolHandler(false).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("%s was not rejected as operator-only: %d", op, w.Code)
		}
	}
}
