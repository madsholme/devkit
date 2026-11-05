package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func documentHost(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	projectHost(t, parent)
	swap(t, &dockerContainers, withContainers())
	root := filepath.Join(parent, "app")
	os.Mkdir(root, 0755)
	os.MkdirAll(filepath.Join(root, "docs", "plans"), 0755)
	os.WriteFile(filepath.Join(root, "docs", "plans", "one.md"), []byte("# Plan\n\nOriginal\n"), 0640)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Agent guidance"), 0644)
	return root
}
func TestDocumentsReadListAndSave(t *testing.T) {
	root := documentHost(t)
	list, err := listDocuments(root)
	if err != nil || len(list.Files) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	d, err := readDocument(root, "docs/plans/one.md")
	if err != nil || !d.Editable || d.Content != "# Plan\n\nOriginal\n" {
		t.Fatalf("read %+v %v", d, err)
	}
	if err := saveDocument(root, d.Path, d.Version, "# Updated\n"); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, d.Path))
	if info.Mode().Perm() != 0640 {
		t.Fatal("mode changed")
	}
	if err := saveDocument(root, d.Path, d.Version, "stale"); err == nil {
		t.Fatal("stale overwrite")
	}
	if err := saveDocument(root, "docs/specs/new.md", "new", "# Spec"); err != nil {
		t.Fatal(err)
	}
	if err := saveDocument(root, "docs/specs/new.md", "new", "duplicate"); err == nil {
		t.Fatal("duplicate overwrite")
	}
	agent, err := readDocument(root, "AGENTS.md")
	if err != nil || agent.Editable {
		t.Fatal("agent not readable/read-only")
	}
	if err := saveDocument(root, "AGENTS.md", agent.Version, "replace"); err == nil {
		t.Fatal("agent edited")
	}
}
func TestDocumentsRejectUnsafeFilesAndUnregisteredCheckouts(t *testing.T) {
	root := documentHost(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	os.WriteFile(secret, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(root, "docs", "linked"))
	os.Symlink(secret, filepath.Join(root, "docs", "link.md"))
	os.Link(secret, filepath.Join(root, "docs", "hard.md"))
	syscall.Mkfifo(filepath.Join(root, "docs", "fifo.md"), 0600)
	for _, path := range []string{"../secret.md", "/etc/passwd", "docs/../AGENTS.md", "docs/.hidden.md", "docs/file.txt", "docs/linked/secret.md", "docs/link.md", "docs/hard.md", "docs/fifo.md", ".git/config", "docs//one.md"} {
		if _, err := readDocument(root, path); err == nil {
			t.Errorf("read unsafe %q", path)
		}
		if err := saveDocument(root, path, "new", "bad"); err == nil {
			t.Errorf("write unsafe %q", path)
		}
	}
	data, _ := os.ReadFile(secret)
	if string(data) != "private" {
		t.Fatal("secret changed")
	}
	if _, err := listDocuments(outside); err == nil {
		t.Fatal("unregistered root accepted")
	}
	os.WriteFile(filepath.Join(root, "docs", "big.md"), []byte(strings.Repeat("x", maxDocumentBytes+1)), 0644)
	os.WriteFile(filepath.Join(root, "docs", "binary.md"), []byte{0, 255}, 0644)
	for _, path := range []string{"docs/big.md", "docs/binary.md"} {
		if _, err := readDocument(root, path); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
	if err := saveDocument(root, "docs/new.md", "new", strings.Repeat("x", maxDocumentBytes+1)); err == nil {
		t.Fatal("oversized write")
	}
}
func TestDocumentsConcurrentSaveHasOneWinner(t *testing.T) {
	root := documentHost(t)
	d, _ := readDocument(root, "docs/plans/one.md")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"first", "second"} {
		wg.Add(1)
		go func(content string) { defer wg.Done(); results <- saveDocument(root, d.Path, d.Version, content) }(text)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}

func TestDocumentsLinkedWorktreeAndRemovedRegistration(t *testing.T) {
	r := newGitRepo(t)
	linked := filepath.Join(t.TempDir(), "feature")
	r.git("worktree", "add", "-b", "feature-docs", linked)
	projectHost(t, r.temp)
	swap(t, &dockerContainers, withContainers())
	if err := saveDocument(linked, "docs/plan.md", "new", "# Branch plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "docs", "plan.md")); !os.IsNotExist(err) {
		t.Fatal("wrong checkout modified")
	}
	if data, err := readDocument(linked, "docs/plan.md"); err != nil || data.Content != "# Branch plan" {
		t.Fatalf("linked read: %+v %v", data, err)
	}
	projectHost(t, t.TempDir())
	if err := saveDocument(linked, "docs/other.md", "new", "no"); err == nil {
		t.Fatal("removed checkout writable")
	}
}
func TestDocumentsAgentAliasesEmptyDocsAndStdinPlan(t *testing.T) {
	root := documentHost(t)
	os.Rename(filepath.Join(root, "AGENTS.md"), filepath.Join(root, "agent.md"))
	if data, err := readDocument(root, "agent.md"); err != nil || data.Editable {
		t.Fatal("agent alias missing")
	}
	plan, err := planAction(actionRequest{Action: "document-save", Worktree: root, Path: "docs/new.md", Expected: "new", Content: "private content"})
	if err != nil || plan.stdin != "private content" || strings.Contains(strings.Join(plan.args, " "), "private content") {
		t.Fatalf("plan %+v %v", plan, err)
	}
	if _, err := planAction(actionRequest{Action: "document-save", Worktree: root, Path: "agent.md", Expected: "new", Content: "replace"}); err == nil {
		t.Fatal("agent write planned")
	}
	empty := filepath.Join(filepath.Dir(root), "empty")
	os.Mkdir(empty, 0755)
	if data, err := listDocuments(empty); err != nil || len(data.Files) != 0 {
		t.Fatalf("empty: %+v %v", data, err)
	}
}

func TestDocumentsListingBounds(t *testing.T) {
	root := documentHost(t)
	for i := 0; i < 1001; i++ {
		os.WriteFile(filepath.Join(root, "docs", fmt.Sprintf("file-%04d.md", i)), []byte("# Note"), 0644)
	}
	data, err := listDocuments(root)
	if err != nil || len(data.Files) != 1000 || len(data.Warnings) == 0 {
		t.Fatalf("unbounded list: %d %+v %v", len(data.Files), data.Warnings, err)
	}
}
