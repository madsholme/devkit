package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSkillsInstallPreservesUserEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "devkit-secrets")
	if e := installSecretsSkill(path); e != nil {
		t.Fatal(e)
	}
	if got := secretsSkillStatus(path); got != "current" {
		t.Fatal(got)
	}
	if e := installSecretsSkill(path); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(path, "SKILL.md")
	original, e := os.ReadFile(target)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(original), "--scope worktree") || !strings.Contains(string(original), "--stdin") {
		t.Fatal("missing CLI workflow")
	}
	edited := append(original, []byte("\nUser addition\n")...)
	if e = os.WriteFile(target, edited, 0600); e != nil {
		t.Fatal(e)
	}
	if e = installSecretsSkill(path); e == nil {
		t.Fatal("overwrote edited skill")
	}
	actual, _ := os.ReadFile(target)
	if string(actual) != string(edited) {
		t.Fatal("user edit changed")
	}
	if e = os.Remove(target); e != nil {
		t.Fatal(e)
	}
	if e = installSecretsSkill(path); e != nil {
		t.Fatal("missing asset repair", e)
	}
}
func TestAgentInstructionsPreserveSurroundingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if e := os.WriteFile(path, []byte("# User rules\nKeep these.\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := installSecretsInstructions(path); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# User rules\nKeep these.\n") {
		t.Fatal("lost user instructions")
	}
	if e := installSecretsInstructions(path); e != nil {
		t.Fatal(e)
	}
	again, _ := os.ReadFile(path)
	if string(data) != string(again) {
		t.Fatal("not idempotent")
	}
	edited := strings.Replace(string(data), "Use the devkit-secrets skill", "My custom instruction", 1)
	os.WriteFile(path, []byte(edited), 0644)
	if e := installSecretsInstructions(path); e == nil {
		t.Fatal("overwrote edited managed block")
	}
}

func TestAgentInstructionsUpgradePreservesProjectRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	oldBody := "Use the devkit-secrets skill for development credentials.\n"
	before := "# Project rules\nKeep these.\n\n<!-- devkit-secrets begin sha256:" + skillHash([]byte(oldBody)) + " -->\n" + oldBody + secretsInstructionEnd + "\nCustom trailing rules.\n"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installSecretsInstructions(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), secretsInstructionBody) || !strings.HasPrefix(string(data), "# Project rules\nKeep these.\n") || !strings.HasSuffix(string(data), "\nCustom trailing rules.\n") {
		t.Fatal("upgrade did not preserve surrounding project rules")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("upgrade did not preserve permissions", err)
	}
}

func TestAgentDoctorChecksProjectContextAndGuidance(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	missing := agentProjectChecks(repo.root)
	failures := 0
	for _, c := range missing {
		if c.Status != "ok" {
			failures++
		}
	}
	if failures == 0 {
		t.Fatal("missing project setup went unreported")
	}
	if _, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if e := installSecretsInstructions(filepath.Join(repo.root, name)); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range agentProjectChecks(repo.root) {
		if c.Status != "ok" {
			t.Fatal(c.Name, c.Detail)
		}
	}
}
