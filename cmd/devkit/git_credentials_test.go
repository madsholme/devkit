package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCredentialsExactHTTPSBinding(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	registered, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	_, e = executeSecret(context.Background(), secretRequest{Operation: "set", Root: repo.root, Scope: "project", Key: "GIT_PASSWORD", Value: "git-SENTINEL"})
	if e != nil {
		t.Fatal(e)
	}
	b := gitCredentialBinding{Root: repo.root, Project: registered.Context.Project, URL: "https://git.example.test:8443/team/repo.git", Username: "dev", Key: "GIT_PASSWORD"}
	swap(t, &gitSecretRequest, func(q secretRequest) (secretResponse, error) { return executeSecret(context.Background(), q) })
	for _, input := range []string{"protocol=http\nhost=git.example.test:8443\npath=team/repo.git\n", "protocol=https\nhost=git.example.test\npath=team/repo.git\n", "protocol=https\nhost=git.example.test:8443\npath=other/repo.git\n", "protocol=https\nhost=git.example.test:8443\npath=team/repo.git\nusername=someone-else\n"} {
		got, e := gitCredentialResponse(b, "get", strings.NewReader(input))
		if e == nil || strings.Contains(got, "SENTINEL") {
			t.Fatalf("credential escaped exact binding: %q %v", got, e)
		}
	}
	got, e := gitCredentialResponse(b, "get", strings.NewReader("protocol=https\nhost=git.example.test:8443\npath=team/repo.git\n"))
	if e != nil || !strings.Contains(got, "password=git-SENTINEL") {
		t.Fatalf("%q %v", got, e)
	}
	for _, op := range []string{"store", "erase"} {
		got, e = gitCredentialResponse(b, op, strings.NewReader("password=do-not-store\n"))
		if e != nil || got != "" {
			t.Fatal(op, e)
		}
	}
	other := newGitRepo(t)
	b.Root = other.root
	if got, e = gitCredentialResponse(b, "get", strings.NewReader("protocol=https\nhost=git.example.test:8443\npath=team/repo.git\n")); e == nil || strings.Contains(got, "SENTINEL") {
		t.Fatal("cross-project credential lookup")
	}
}
func TestGitCredentialsRejectUnsafeRemote(t *testing.T) {
	for _, raw := range []string{"http://host/a", "ssh://host/a", "https://user:pass@host/a", "https://host/a?token=x", "https://host/a#x", "https://host/", "https://host/a\n"} {
		if _, e := parseGitCredentialURL(raw); e == nil {
			t.Fatal(raw)
		}
	}
}

func TestGitCredentialsBindPreservesOtherHelpersAndHasNoPlaintext(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	swap(t, &gitSecretRequest, func(q secretRequest) (secretResponse, error) { return executeSecret(context.Background(), q) })
	_, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	_, e = executeSecret(context.Background(), secretRequest{Operation: "set", Root: repo.root, Scope: "project", Key: "GIT_PASSWORD", Value: "bound-SENTINEL"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = git(repo.root, "config", "--local", "credential.helper", "existing-helper"); e != nil {
		t.Fatal(e)
	}
	remote := "https://git.example.test/team/repo.git"
	if _, e = gitCredentialsCLI([]string{"bind", "--root", repo.root, "--url", remote, "--key", "GIT_PASSWORD", "--username", "dev", "--yes"}); e != nil {
		t.Fatal(e)
	}
	generic, _ := git(repo.root, "config", "--local", "--get", "credential.helper")
	if generic != "existing-helper" {
		t.Fatal("global helper changed")
	}
	data, e := os.ReadFile(filepath.Join(repo.root, ".git", "config"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "SENTINEL") {
		t.Fatal("token in git config")
	}
	if !strings.Contains(string(data), "useHttpPath = true") || !strings.Contains(string(data), "helper = \n") {
		t.Fatalf("missing helper reset/path matching: %s", data)
	}
	if _, e = gitCredentialsCLI([]string{"unbind", "--root", repo.root, "--url", remote, "--yes"}); e != nil {
		t.Fatal(e)
	}
	generic, _ = git(repo.root, "config", "--local", "--get", "credential.helper")
	if generic != "existing-helper" {
		t.Fatal("existing helper removed")
	}
}
