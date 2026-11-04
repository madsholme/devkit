package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func originFixture(t *testing.T) (*gitRepo, *gitRepo, string) {
	t.Helper()
	a := newGitRepo(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "clone", "--bare", a.root, remote).CombinedOutput(); err != nil {
		t.Fatalf("bare origin: %v %s", err, out)
	}
	a.git("remote", "add", "origin", remote)
	a.git("fetch", "origin")
	a.git("branch", "--set-upstream-to=origin/main")
	b := &gitRepo{t: t, root: filepath.Join(t.TempDir(), "peer")}
	if out, err := exec.Command("git", "clone", remote, b.root).CombinedOutput(); err != nil {
		t.Fatalf("peer: %v %s", err, out)
	}
	b.git("config", "user.name", "Devkit test")
	b.git("config", "user.email", "test@example.invalid")
	b.git("config", "commit.gpgsign", "false")
	b.git("config", "core.hooksPath", "/dev/null")
	return a, b, remote
}

func commitFile(r *gitRepo, name string) string {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.root, name), []byte(name), 0600); err != nil {
		r.t.Fatal(err)
	}
	r.git("add", name)
	r.git("commit", "-m", name)
	return r.git("rev-parse", "HEAD")
}

func TestGitControlFetchSyncAndExplicitPush(t *testing.T) {
	a, b, _ := originFixture(t)
	head := commitFile(b, "remote-change")
	b.git("push", "origin", "main")
	if err := controlGit(a.root, "fetch"); err != nil {
		t.Fatal(err)
	}
	if a.git("rev-parse", "HEAD") != a.commit {
		t.Fatal("fetch changed working branch")
	}
	if err := controlGit(a.root, "sync"); err != nil {
		t.Fatal(err)
	}
	if a.git("rev-parse", "HEAD") != head {
		t.Fatal("sync did not fast-forward")
	}
	head = commitFile(a, "local-change")
	a.git("config", "push.default", "matching")
	a.git("branch", "must-not-push")
	a.git("config", "remote.origin.mirror", "true")
	a.git("config", "push.followTags", "true")
	a.git("tag", "-a", "keep-local", "-m", "local tag")
	if err := controlGit(a.root, "push"); err != nil {
		t.Fatal(err)
	}
	b.git("fetch", "origin")
	if b.git("rev-parse", "origin/main") != head {
		t.Fatal("push did not publish current branch")
	}
	if strings.Contains(b.git("ls-remote", "--heads", "origin"), "must-not-push") {
		t.Fatal("pushed unrelated branch")
	}
	if strings.Contains(b.git("ls-remote", "--tags", "origin"), "keep-local") {
		t.Fatal("pushed unrelated tag")
	}
}

func TestGitControlRefusesUnsafeCheckouts(t *testing.T) {
	for _, kind := range []string{"dirty", "staged", "untracked", "detached", "no-upstream", "other-remote", "merge", "rebase", "diverged"} {
		t.Run(kind, func(t *testing.T) {
			a, b, remote := originFixture(t)
			switch kind {
			case "dirty":
				os.WriteFile(filepath.Join(a.root, "message.txt"), []byte("edited"), 0600)
			case "staged":
				os.WriteFile(filepath.Join(a.root, "new"), []byte("local"), 0600)
				a.git("add", "new")
			case "untracked":
				os.WriteFile(filepath.Join(a.root, "new"), []byte("local"), 0600)
			case "detached":
				a.git("checkout", "--detach")
			case "no-upstream":
				a.git("branch", "--unset-upstream")
			case "other-remote":
				a.git("remote", "add", "other", remote)
				a.git("config", "branch.main.remote", "other")
			case "merge":
				os.WriteFile(filepath.Join(a.common, "MERGE_HEAD"), []byte(a.commit), 0600)
			case "rebase":
				os.Mkdir(filepath.Join(a.common, "rebase-merge"), 0700)
			case "diverged":
				commitFile(a, "local")
				commitFile(b, "remote")
				b.git("push", "origin", "main")
			}
			before := a.git("rev-parse", "HEAD")
			for _, action := range []string{"sync", "push"} {
				if err := controlGit(a.root, action); err == nil {
					t.Fatalf("%s accepted %s", action, kind)
				}
			}
			if a.git("rev-parse", "HEAD") != before {
				t.Fatal("changed rejected checkout")
			}
		})
	}
}

func TestGitActionsRevalidateMembershipWithoutDocker(t *testing.T) {
	a, _, _ := originFixture(t)
	projectHost(t, a.temp)
	swap(t, &dockerContainers, func() ([]container, error) { return nil, errors.New("offline") })
	for _, action := range []string{"fetch", "sync", "push"} {
		p, err := planAction(actionRequest{Action: "git-" + action, Worktree: a.root})
		if err != nil || p.dir != a.root || strings.Join(p.args, " ") != "git "+action+" --dashboard" {
			t.Fatalf("%s: %+v %v", action, p, err)
		}
		if _, err := planAction(actionRequest{Action: "git-" + action, Worktree: t.TempDir()}); err == nil {
			t.Fatal("arbitrary path allowed")
		}
	}
	projectHost(t, t.TempDir())
	if err := runGitAction(a.root, "sync", true); err == nil {
		t.Fatal("execution trusted stale discovery")
	}
}

func TestGitControlRechecksAfterFetch(t *testing.T) {
	a, _, _ := originFixture(t)
	original := gitControlCommand
	swap(t, &gitControlCommand, func(ctx context.Context, root string, args ...string) (string, error) {
		out, err := original(ctx, root, args...)
		if len(args) > 0 && args[0] == "fetch" {
			os.WriteFile(filepath.Join(root, "appeared-during-fetch"), []byte("keep"), 0600)
		}
		return out, err
	})
	if err := controlGit(a.root, "sync"); err == nil {
		t.Fatal("ignored changes during fetch")
	}
}

func TestGitControlLockAndHookAreRespected(t *testing.T) {
	a, b, _ := originFixture(t)
	lock, err := os.OpenFile(filepath.Join(a.common, "devkit-git.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err = controlGit(a.root, "fetch"); err == nil || !strings.Contains(err.Error(), "Another devkit") {
		t.Fatalf("ignored lock: %v", err)
	}
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	commitFile(a, "do-not-publish")
	hooks := t.TempDir()
	os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0700)
	a.git("config", "core.hooksPath", hooks)
	if err = controlGit(a.root, "push"); err == nil {
		t.Fatal("bypassed pre-push policy")
	}
	b.git("fetch", "origin")
	if b.git("rev-parse", "origin/main") != a.commit {
		t.Fatal("published despite rejecting hook")
	}
}

func TestGitControlDifferentTrackingNameAndDirtyFetch(t *testing.T) {
	a, b, _ := originFixture(t)
	a.git("branch", "-m", "local-name")
	os.WriteFile(filepath.Join(a.root, "keep-local"), []byte("untracked"), 0600)
	if err := controlGit(a.root, "fetch"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.root, "keep-local")); err != nil {
		t.Fatal("fetch removed local file")
	}
	a.git("add", "keep-local")
	a.git("commit", "-m", "local")
	if err := controlGit(a.root, "push"); err != nil {
		t.Fatal(err)
	}
	b.git("fetch", "origin")
	if b.git("rev-parse", "origin/main") != a.git("rev-parse", "HEAD") {
		t.Fatal("wrong push destination")
	}
	if strings.Contains(b.git("ls-remote", "--heads", "origin"), "local-name") {
		t.Fatal("created unexpected remote branch")
	}
}

func TestGitActionRequiresSignIn(t *testing.T) {
	server, _, release := webHost(t)
	defer close(release)
	for _, action := range []string{"git-fetch", "git-sync", "git-push"} {
		resp, _ := post(t, server, "/api/action", server.URL, "", actionRequest{Action: action, Worktree: "/repo"})
		if resp.StatusCode != 401 {
			t.Fatalf("anonymous %s: %d", action, resp.StatusCode)
		}
	}
}

func TestGitControlRefusesCheckoutSwitchDuringFetch(t *testing.T) {
	a, _, _ := originFixture(t)
	original := gitControlCommand
	swap(t, &gitControlCommand, func(ctx context.Context, root string, args ...string) (string, error) {
		out, err := original(ctx, root, args...)
		if len(args) > 0 && args[0] == "fetch" {
			a.git("checkout", "-b", "another", "--track", "origin/main")
		}
		return out, err
	})
	if err := controlGit(a.root, "sync"); err == nil || !strings.Contains(err.Error(), "changed during fetch") {
		t.Fatalf("checkout switch ignored: %v", err)
	}
}

func TestGitSyncPreservesIgnoredLocalFiles(t *testing.T) {
	a, b, _ := originFixture(t)
	os.WriteFile(filepath.Join(a.common, "info/exclude"), []byte("local-data\n"), 0600)
	os.WriteFile(filepath.Join(a.root, "local-data"), []byte("user data"), 0600)
	commitFile(b, "local-data")
	b.git("push", "origin", "main")
	if err := controlGit(a.root, "sync"); err == nil {
		t.Fatal("sync overwrote ignored local data")
	}
	data, _ := os.ReadFile(filepath.Join(a.root, "local-data"))
	if string(data) != "user data" || a.git("rev-parse", "HEAD") != a.commit {
		t.Fatal("ignored data or HEAD changed")
	}
}
