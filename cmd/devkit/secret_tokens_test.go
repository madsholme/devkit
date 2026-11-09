package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecretAPITokenLifecycle(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	if _, err := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); err != nil {
		t.Fatal(err)
	}
	token, raw, err := createSecretToken(repo.root, "test-app", "worktree", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || token.ID == "" {
		t.Fatal("missing token")
	}
	b, err := os.ReadFile(filepath.Join(vaultDir(), "api-tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), raw) || strings.Contains(string(b), strings.Split(raw, ".")[1]) {
		t.Fatal("plaintext token stored")
	}
	cred, err := parseSecretBearer("Bearer " + raw)
	if err != nil {
		t.Fatal(err)
	}
	r, err := loadSecretRegistry()
	if err != nil {
		t.Fatal(err)
	}
	q := secretRequest{Operation: "list"}
	if _, err = authorizeSecretToken(cred, &r, &q); err != nil {
		t.Fatal(err)
	}
	if q.Root != repo.root || q.Scope != "worktree" {
		t.Fatal("context not bound")
	}
	q.Operation = "set"
	if _, err = authorizeSecretToken(cred, &r, &q); err == nil {
		t.Fatal("read token wrote")
	}
	if err = revokeSecretToken(token.ID); err != nil {
		t.Fatal(err)
	}
	q = secretRequest{Operation: "list"}
	if _, err = authorizeSecretToken(cred, &r, &q); err == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestSecretTokenListingAndFileSafety(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	if _, err := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); err != nil {
		t.Fatal(err)
	}
	record, raw, err := createSecretToken(repo.root, "listing", "project", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := stdout
	stdout = &output
	defer func() { stdout = previous }()
	if _, err := secretTokensCLI([]string{"list", "--json"}); err != nil {
		t.Fatal(err)
	}
	var listed []secretAPIToken
	if json.Unmarshal(output.Bytes(), &listed) != nil || len(listed) != 1 || listed[0].ID != record.ID || listed[0].Hash != "" {
		t.Fatal("invalid listing")
	}
	if strings.Contains(output.String(), raw) || strings.Contains(output.String(), secretTokenHash(raw)) {
		t.Fatal("listing leaked credential/hash")
	}
	file := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(file, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := secretTokensCLI([]string{"create", "replacement", "--scope", "project", "--output", file}); err == nil {
		t.Fatal("overwrote credential")
	}
	contents, _ := os.ReadFile(file)
	if string(contents) != "preserve" {
		t.Fatal("existing file changed")
	}
	store, err := loadSecretTokens()
	if err != nil || len(store.Tokens) != 1 {
		t.Fatal("failed delivery created a token")
	}
}
