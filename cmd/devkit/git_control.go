package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Keep remote diagnostics private: a configured URL may contain credentials.
// Commands are noninteractive and bounded, including their SSH/helper children.
var gitControlCommand = func(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = withEnv(os.Environ(), map[string]string{"GIT_TERMINAL_PROMPT": "0", "GCM_INTERACTIVE": "never", "SSH_ASKPASS": "/bin/false", "SSH_ASKPASS_REQUIRE": "force"})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 250 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", fail("Git command failed or timed out; check repository access and credentials on the host.")
	}
	return strings.TrimSpace(string(out)), nil
}

type gitControls struct {
	Actions []string `json:"actions"`
	Note    string   `json:"note"`
}

type gitSyncState struct{ branch, head, remoteRef string }

func readGitSyncState(ctx context.Context, root string) (gitSyncState, error) {
	s := gitSyncState{}
	var err error
	if s.branch, err = gitControlCommand(ctx, root, "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return s, fail("Check out a branch before syncing or pushing.")
	}
	if s.head, err = gitControlCommand(ctx, root, "rev-parse", "--verify", "HEAD"); err != nil {
		return s, fail("Create an initial commit before syncing or pushing.")
	}
	branch := strings.TrimPrefix(s.branch, "refs/heads/")
	remote, _ := gitControlCommand(ctx, root, "config", "--get", "branch."+branch+".remote")
	s.remoteRef, _ = gitControlCommand(ctx, root, "config", "--get-all", "branch."+branch+".merge")
	if remote != "origin" || !strings.HasPrefix(s.remoteRef, "refs/heads/") || strings.Contains(s.remoteRef, "\n") {
		return s, fail("Configure this branch to track one branch on origin before syncing or pushing.")
	}
	if _, err = gitControlCommand(ctx, root, "check-ref-format", s.remoteRef); err != nil {
		return s, fail("The origin upstream branch is invalid.")
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "BISECT_START"} {
		path, e := gitControlCommand(ctx, root, "rev-parse", "--git-path", marker)
		if e != nil {
			return s, e
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, e = os.Stat(path); e == nil || !os.IsNotExist(e) {
			return s, fail("Finish the current Git operation before syncing or pushing.")
		}
	}
	dirty, err := gitControlCommand(ctx, root, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return s, err
	}
	if dirty != "" {
		return s, fail("Commit or move local changes, including untracked files, before syncing or pushing.")
	}
	return s, nil
}

func checkoutGitControls(root string) gitControls {
	result := gitControls{Actions: []string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := gitControlCommand(ctx, root, "remote", "get-url", "origin"); err != nil {
		result.Note = "Configure an origin remote on the host to enable Git controls."
		return result
	}
	result.Actions = append(result.Actions, "fetch")
	if _, err := readGitSyncState(ctx, root); err != nil {
		result.Note = err.Error()
	} else {
		result.Actions = append(result.Actions, "sync", "push")
	}
	return result
}

func knownGitCheckout(root string) bool {
	// A Docker failure must not disable projects registered through other sources.
	containers, _ := dockerContainers()
	for _, trees := range knownRepos(containers) {
		if _, ok := branchOf(trees, root); ok && root != "" {
			return true
		}
	}
	return false
}

func planGitAction(req actionRequest) (plannedAction, error) {
	action := strings.TrimPrefix(req.Action, "git-")
	if action != "fetch" && action != "sync" && action != "push" {
		return plannedAction{}, fail("Unsupported Git action.")
	}
	if !knownGitCheckout(req.Worktree) {
		return plannedAction{}, fail("Not a worktree of a repository devkit knows.")
	}
	return plannedAction{args: []string{"git", action, "--dashboard"}, dir: req.Worktree,
		target: "worktree " + req.Worktree, title: action + " origin · " + filepath.Base(req.Worktree)}, nil
}

func runGitAction(root, action string, dashboard bool) error {
	if dashboard && !knownGitCheckout(root) {
		return fail("This checkout is no longer registered with devkit.")
	}
	return controlGit(root, action)
}

func controlGit(root, action string) error {
	if action != "fetch" && action != "sync" && action != "push" {
		return fail("Use devkit git fetch, sync or push.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	common, err := gitControlCommand(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(common, "devkit-git.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail("Another devkit Git operation is running in this repository.")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, err = gitControlCommand(ctx, root, "remote", "get-url", "origin"); err != nil {
		return fail("Configure an origin remote on the host first.")
	}
	var before gitSyncState
	if action != "fetch" {
		if before, err = readGitSyncState(ctx, root); err != nil {
			return err
		}
	}
	// Explicit destinations prevent custom fetch refspecs from updating local branches.
	refspec := "+refs/heads/*:refs/remotes/origin/*"
	tracking := ""
	if action != "fetch" {
		tracking = "refs/remotes/origin/" + strings.TrimPrefix(before.remoteRef, "refs/heads/")
		refspec = "+" + before.remoteRef + ":" + tracking
	}
	fmt.Fprintln(stdout, "Fetching origin…")
	if _, err = gitControlCommand(ctx, root, "fetch", "--no-tags", "--no-recurse-submodules", "--refmap=", "origin", refspec); err != nil {
		return err
	}
	if action != "fetch" {
		after, e := readGitSyncState(ctx, root)
		if e != nil {
			return e
		}
		if before != after {
			return fail("The checkout changed during fetch; refresh before retrying.")
		}
		remoteHead, e := gitControlCommand(ctx, root, "rev-parse", "--verify", tracking+"^{commit}")
		if e != nil {
			return fail("The upstream branch is missing on origin.")
		}
		localBehind := false
		if _, e = gitControlCommand(ctx, root, "merge-base", "--is-ancestor", before.head, remoteHead); e == nil {
			localBehind = true
		}
		localAhead := false
		if _, e = gitControlCommand(ctx, root, "merge-base", "--is-ancestor", remoteHead, before.head); e == nil {
			localAhead = true
		}
		if !localBehind && !localAhead {
			return fail("Local and origin history have diverged; resolve this manually. Nothing was merged or pushed.")
		}
		if action == "sync" && localBehind {
			_, err = gitControlCommand(ctx, root, "-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", remoteHead)
			if err != nil {
				return fail("Fast-forward failed. Check for local files (including ignored files) that conflict with origin, Git locks or hooks; inspect the checkout before retrying.")
			}
		} else if action == "push" {
			if !localAhead {
				return fail("Origin has new commits; sync before pushing.")
			}
			// Pin the source commit; ignore push.default, push refspecs, tags and mirror settings.
			_, err = gitControlCommand(ctx, root, "-c", "remote.origin.mirror=false", "-c", "push.followTags=false", "push", "--recurse-submodules=check", "origin", before.head+":"+before.remoteRef)
			if err != nil {
				return fail("Push failed. Check origin access, Git hooks, unpublished submodules or new remote commits on the host, then retry.")
			}
		}
	}
	fmt.Fprintln(stdout, "Git", action, "completed.")
	record(event{Action: "git-" + action, Worktree: root, Detail: "origin", OK: true})
	return nil
}
