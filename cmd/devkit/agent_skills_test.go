package main

import (
	"context"
	"encoding/json"
	projecttemplate "github.com/steenfeldt/devkit/project-template"
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

func TestManagedAgentSkillsDistributeVMRulesAndPreserveEdits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := managedAgentSkills("install"); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{".agents", ".claude"} {
		path := filepath.Join(home, folder, "skills", "devkit-vms", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("VM skill was not distributed", err)
		}
		for _, rule := range []string{"SPICE", "Tailscale", "firewall", "devkit-secrets", "restart"} {
			if !strings.Contains(string(data), rule) {
				t.Fatalf("missing %s", rule)
			}
		}
	}
	path := filepath.Join(home, ".agents", "skills", "devkit-vms", "SKILL.md")
	original, _ := os.ReadFile(path)
	edited := append(original, []byte("\nUser customization\n")...)
	if err := os.WriteFile(path, edited, 0600); err != nil {
		t.Fatal(err)
	}
	status, err := managedAgentSkills("status")
	if err != nil || status["codex"] != "edited" || status["claude"] != "current" {
		t.Fatal(status, err)
	}
	if _, err := managedAgentSkills("repair"); err == nil {
		t.Fatal("edited VM skill overwritten")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(edited) {
		t.Fatal("user VM rules changed")
	}
}

func TestAgentCLIInstallsBothProjectGuides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newGitRepo(t)
	path := filepath.Join(repo.root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Keep project rules\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := agentsSkillsCLI([]string{"skills", "install", "--project", repo.root}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(repo.root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"<!-- devkit-secrets begin", "<!-- devkit-vms begin"} {
			if strings.Count(string(data), marker) != 1 {
				t.Fatalf("%s missing/duplicated %s", name, marker)
			}
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# Keep project rules\n") {
		t.Fatal("project rules lost")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("mode changed")
	}
	edited := strings.Replace(string(data), "Use the devkit-vms skill", "My VM policy", 1)
	os.WriteFile(path, []byte(edited), 0600)
	if _, err := agentsSkillsCLI([]string{"skills", "repair", "--project", repo.root}); err == nil {
		t.Fatal("edited VM guidance overwritten")
	}
	after, _ := os.ReadFile(path)
	if string(after) != edited {
		t.Fatal("edited guidance changed")
	}
}

func TestVMSkillUpgradeAndSelectedAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := managedAgentSkillsFor("install", "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("unselected agent modified", err)
	}
	dir := filepath.Join(home, ".agents", "skills", "devkit-vms")
	old := []byte("Previous managed VM guidance\n")
	m, _ := json.Marshal(secretsSkillManifest{"0", skillHash(old)})
	os.WriteFile(filepath.Join(dir, "SKILL.md"), old, 0600)
	os.WriteFile(filepath.Join(dir, ".devkit-managed.json"), m, 0600)
	status, err := managedAgentSkillsFor("status", "codex")
	if err != nil || status["codex"] != "outdated" {
		t.Fatal(status, err)
	}
	if _, err := managedAgentSkillsFor("repair", "codex"); err != nil {
		t.Fatal(err)
	}
	status, _ = managedAgentSkillsFor("status", "codex")
	if status["codex"] != "current" {
		t.Fatal(status)
	}
	outside := filepath.Join(home, "custom.md")
	os.WriteFile(outside, []byte("preserve"), 0600)
	os.Remove(filepath.Join(dir, "SKILL.md"))
	if err := os.Symlink(outside, filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := managedAgentSkillsFor("repair", "codex"); err == nil {
		t.Fatal("symlink accepted")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "preserve" {
		t.Fatal("symlink target changed")
	}
}

func TestProjectTemplatesContainCurrentVMGuidance(t *testing.T) {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		data, err := projecttemplate.Files.ReadFile(name)
		if err != nil || !strings.Contains(string(data), vmInstructionBody) {
			t.Fatal(name, "VM template differs from installed guidance", err)
		}
		marker := "<!-- devkit-vms begin sha256:" + skillHash([]byte(vmInstructionBody)) + " -->"
		if !strings.Contains(string(data), marker) {
			t.Fatal(name, "invalid managed block")
		}
	}
}

func TestProjectGuidancePreservesClaudeSiblingAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newGitRepo(t)
	alias := filepath.Join(repo.root, "CLAUDE.md")
	os.Remove(alias)
	if err := os.Symlink("AGENTS.md", alias); err != nil {
		t.Fatal(err)
	}
	if _, err := agentsSkillsCLI([]string{"skills", "install", "--project", repo.root}); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(alias); err != nil || link != "AGENTS.md" {
		t.Fatal("alias changed", link, err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.root, "AGENTS.md"))
	if !strings.Contains(string(data), vmInstructionBody) {
		t.Fatal("missing shared VM guidance")
	}
	for _, check := range agentProjectChecks(repo.root) {
		if check.Name == "CLAUDE.md" && check.Status != "ok" {
			t.Fatal(check)
		}
	}
	os.Remove(alias)
	outside := filepath.Join(t.TempDir(), "AGENTS.md")
	os.WriteFile(outside, []byte("outside"), 0600)
	os.Symlink(outside, alias)
	if _, err := agentsSkillsCLI([]string{"skills", "install", "--project", repo.root}); err == nil {
		t.Fatal("external symlink accepted")
	}
	data, _ = os.ReadFile(outside)
	if string(data) != "outside" {
		t.Fatal("external instructions changed")
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
		if e := installProjectAgentInstructions(filepath.Join(repo.root, name)); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range agentProjectChecks(repo.root) {
		if c.Status != "ok" {
			t.Fatal(c.Name, c.Detail)
		}
	}
}
