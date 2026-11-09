package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretCheckoutOutsideRepositoryExplainsWhereToRun(t *testing.T) {
	_, err := inspectSecretCheckout(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Run this command from a project's Git checkout") || !strings.Contains(err.Error(), "cd /path/to/project") {
		t.Fatalf("expected checkout guidance, got %v", err)
	}
}

func TestSecretCheckoutPreservesOtherGitFailures(t *testing.T) {
	want := errors.New("Git command failed or timed out")
	swap(t, &gitControlCommand, func(context.Context, string, ...string) (string, error) { return "", want })
	_, err := inspectSecretCheckout(t.TempDir())
	if !errors.Is(err, want) {
		t.Fatalf("lost original Git error: %v", err)
	}
}

func TestSecretCheckoutAcceptsProjectSubdirectory(t *testing.T) {
	repo := newGitRepo(t)
	sub := filepath.Join(repo.root, "nested")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	checkout, err := inspectSecretCheckout(sub)
	if err != nil || checkout.Root != repo.root {
		t.Fatalf("checkout: %v, error: %v", checkout, err)
	}
}
