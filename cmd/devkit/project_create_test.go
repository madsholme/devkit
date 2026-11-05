package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func creationHost(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	projectHost(t, root)
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(gitConfig, []byte("[user]\n name = Project Test\n email = test@example.test\n"), 0600)
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return root
}
func createdGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestProjectCreateCLICompleteTemplateAndCommit(t *testing.T) {
	root := creationHost(t)
	notesFile := filepath.Join(t.TempDir(), "notes.md")
	const notes = "Use Go for this app.\nPreserve this text: $(touch NEVER) <script>literal</script>"
	os.WriteFile(notesFile, []byte(notes), 0600)
	var out bytes.Buffer
	swap(t, &stdout, io.Writer(&out))
	code, err := dispatch([]string{"project", "create", "new-app", "--remote", "git@example.test:team/new-app.git", "--agents-file", notesFile})
	if err != nil || code != 0 {
		t.Fatalf("create: code=%d err=%v", code, err)
	}
	path := filepath.Join(root, "new-app")
	for _, name := range []string{".gitignore", ".dockerignore", "Dockerfile", "app.py", "message.txt", "compose.live.yaml", "compose.review.yaml", "t3.json", "CLAUDE.md", "AGENTS.md", "devkit.json"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Errorf("template %s: %v", name, err)
		}
	}
	base, _ := os.ReadFile(filepath.Join(kit, "project-template", "AGENTS.md"))
	agents, _ := os.ReadFile(filepath.Join(path, "AGENTS.md"))
	if !bytes.HasPrefix(agents, base) || !strings.Contains(string(agents), "## Project-specific instructions\n\n"+notes) {
		t.Fatal("agent instructions replaced or lost")
	}
	var recipe struct{ Project string }
	data, _ := os.ReadFile(filepath.Join(path, "devkit.json"))
	json.Unmarshal(data, &recipe)
	if recipe.Project != "new-app" {
		t.Fatalf("recipe: %s", data)
	}
	if createdGit(t, path, "branch", "--show-current") != "main" || createdGit(t, path, "rev-list", "--count", "HEAD") != "1" {
		t.Fatal("missing initial main commit")
	}
	if createdGit(t, path, "status", "--porcelain") != "" {
		t.Fatal("template was not fully committed")
	}
	if createdGit(t, path, "remote", "get-url", "origin") != "git@example.test:team/new-app.git" {
		t.Fatal("wrong remote")
	}
	if strings.Contains(out.String(), notes) {
		t.Fatal("notes leaked to output")
	}
	if _, err := os.Stat(filepath.Join(path, "NEVER")); !os.IsNotExist(err) {
		t.Fatal("notes executed")
	}
	if _, err := os.Stat(filepath.Join(path, "embed.go")); !os.IsNotExist(err) {
		t.Fatal("implementation file copied")
	}
}
func TestProjectCreateRefusesExistingAndUnsafeNames(t *testing.T) {
	root := creationHost(t)
	os.Mkdir(filepath.Join(root, "empty"), 0755)
	os.WriteFile(filepath.Join(root, "file"), []byte("keep"), 0600)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "link"))
	os.Symlink(filepath.Join(outside, "gone"), filepath.Join(root, "dangling"))
	for _, name := range []string{"empty", "file", "link", "dangling", "../escape", "/absolute", "bad/name", ".hidden", "-flag", "Upper", "", strings.Repeat("a", 33)} {
		if _, err := dispatch([]string{"project", "create", name}); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	data, _ := os.ReadFile(filepath.Join(root, "file"))
	if string(data) != "keep" {
		t.Fatal("existing file changed")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("symlink followed")
	}
}
func TestProjectCreateConcurrentAttemptsNeverOverwrite(t *testing.T) {
	root := creationHost(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := dispatch([]string{"project", "create", "same"}); errs <- err }()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
	if createdGit(t, filepath.Join(root, "same"), "rev-list", "--count", "HEAD") != "1" {
		t.Fatal("duplicate commit")
	}
}
func TestProjectCreatePreflightRejectsBadRemoteAndMissingIdentity(t *testing.T) {
	root := creationHost(t)
	for _, remote := range []string{"/tmp/repo", "../repo", "file:///tmp/repo", "ext::sh command", "https://user:token@example.test/a.git", "https://example.test/a?token=secret", "--upload-pack=bad", "git@-host:path", "git@host:-flag", "ssh://-host/path", "ssh://-user@host/path", "ssh://user%0Aname@host/path", "git@example.test:a\ncommand"} {
		if _, err := dispatch([]string{"project", "create", "new", "--remote", remote}); err == nil {
			t.Errorf("accepted remote %q", remote)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	if _, err := dispatch([]string{"project", "create", "new"}); err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatal("preflight created folder")
	}
}

func TestProjectCreateFailureRetainsPartialFolderAndBlocksRetry(t *testing.T) {
	root := creationHost(t)
	original := creationGit
	swap(t, &creationGit, func(ctx context.Context, dir string, identity bool, args ...string) (string, error) {
		for _, arg := range args {
			if arg == "commit" {
				return "", fmt.Errorf("simulated commit failure")
			}
		}
		return original(ctx, dir, identity, args...)
	})
	_, err := dispatch([]string{"project", "create", "partial"})
	if err == nil || !strings.Contains(err.Error(), "Partial project retained") {
		t.Fatalf("failure: %v", err)
	}
	path := filepath.Join(root, "partial", "AGENTS.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch([]string{"project", "create", "partial"}); err == nil {
		t.Fatal("retry overwrote partial folder")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("partial files changed")
	}
}

func TestProjectCreateRejectsNotesAndChangedParent(t *testing.T) {
	root := creationHost(t)
	for _, notes := range []string{strings.Repeat("x", maxAgentNotes+1), "bad\x00note", string([]byte{255})} {
		if err := createProject(createProjectOptions{name: "new", notes: notes}); err == nil {
			t.Fatal("invalid notes accepted")
		}
	}
	if err := createProject(createProjectOptions{name: "new", expectedParent: t.TempDir()}); err == nil {
		t.Fatal("changed parent accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("preflight wrote files")
	}
}

func TestProjectCreateAuthenticatedPlanKeepsNotesOffCommandLine(t *testing.T) {
	server, started, release := webHost(t)
	defer close(release)
	root := t.TempDir()
	cfg, err := readConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg["projects_dir"] = root
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(os.Getenv("DEVKIT_CONFIG_DIR"), "config.json"), data, 0600)
	global := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(global, []byte("[user]\nname = Test\nemail = test@example.test\n"), 0600)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	req := actionRequest{Action: "project-create", Name: "new-app", Remote: "https://example.test/new.git", AgentNotes: "Private instructions $(literal)"}
	if resp, _ := post(t, server, "/api/action", server.URL, "", req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("anonymous create accepted")
	}
	login, _ := post(t, server, "/api/login", server.URL, "", map[string]string{"password": "dashboard-password"})
	var cookie string
	for _, c := range login.Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login failed")
	}
	if resp, _ := post(t, server, "/api/action", "https://foreign.example", cookie, req); resp.StatusCode != http.StatusForbidden {
		t.Fatal("foreign origin accepted")
	}
	resp, body := post(t, server, "/api/action", server.URL, cookie, req)
	if resp.StatusCode != 200 {
		t.Fatalf("create %d %s", resp.StatusCode, body)
	}
	select {
	case plan := <-started:
		if plan.stdin != req.AgentNotes || strings.Contains(strings.Join(plan.args, " "), req.AgentNotes) || plan.dir != root {
			t.Fatalf("wrong plan: %+v", plan)
		}
		if strings.Join(plan.args, " ") != "project create new-app --projects-dir "+root+" --agents-file - --remote https://example.test/new.git" {
			t.Fatalf("args: %v", plan.args)
		}
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	os.Mkdir(filepath.Join(root, "exists"), 0755)
	req.Name = "exists"
	if resp, _ := post(t, server, "/api/action", server.URL, cookie, req); resp.StatusCode != 400 {
		t.Fatalf("existing path: %d", resp.StatusCode)
	}
}

func TestProjectCreateIgnoresInheritedRepositoryAndTemplateHooks(t *testing.T) {
	root := creationHost(t)
	outside := t.TempDir()
	createdGit(t, outside, "init", "--initial-branch=main")
	template := t.TempDir()
	os.Mkdir(filepath.Join(template, "hooks"), 0755)
	os.WriteFile(filepath.Join(template, "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 77\n"), 0755)
	global := os.Getenv("GIT_CONFIG_GLOBAL")
	data, _ := os.ReadFile(global)
	data = append(data, []byte("[init]\n templateDir = "+template+"\n[commit]\n gpgSign = true\n")...)
	os.WriteFile(global, data, 0600)
	t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(outside, "foreign-index"))
	if err := createProject(createProjectOptions{name: "isolated"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")
	t.Setenv("GIT_INDEX_FILE", "")
	os.Unsetenv("GIT_DIR")
	os.Unsetenv("GIT_WORK_TREE")
	os.Unsetenv("GIT_INDEX_FILE")
	if _, err := os.Stat(filepath.Join(outside, "foreign-index")); !os.IsNotExist(err) {
		t.Fatal("foreign index changed")
	}
	if _, err := os.Stat(filepath.Join(outside, ".git", "refs", "heads", "main")); !os.IsNotExist(err) {
		t.Fatal("foreign repository committed")
	}
	if createdGit(t, filepath.Join(root, "isolated"), "rev-list", "--count", "HEAD") != "1" {
		t.Fatal("missing isolated commit")
	}
	if _, err := os.Stat(filepath.Join(root, "isolated", ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatal("global hook copied")
	}
}
