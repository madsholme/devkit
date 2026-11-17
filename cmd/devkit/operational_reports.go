package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type historyEventView struct {
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	Preview  string    `json:"preview,omitempty"`
	Worktree string    `json:"worktree,omitempty"`
	By       string    `json:"by"`
	OK       bool      `json:"ok"`
}

func historyReport(preview string, limit int) operationReport {
	rows := []historyEventView{}
	r := newReport("history", nil)
	if preview != "" && !previewID.MatchString(preview) {
		r.unavailable("history.target", "history", "Preview ID is invalid.")
		r.Data = map[string]any{"events": rows}
		return r
	}
	events, err := readHistory(preview, limit)
	if err != nil {
		r.unavailable("history.read", "history", "History is unavailable.")
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		by := e.By
		switch by {
		case "cli", "dashboard", "auto":
		default:
			by = "other"
		}
		rows = append(rows, historyEventView{e.Time.UTC(), e.Action, e.Preview, e.Worktree, by, e.OK})
	}
	r.Data = map[string]any{"events": rows}
	return r
}

type operationalShareView struct {
	Preview string     `json:"preview"`
	URL     string     `json:"url"`
	Host    string     `json:"host"`
	Expires *time.Time `json:"expires"`
	Fixed   bool       `json:"fixed"`
}

func sharesReport(now time.Time) operationReport {
	views := []operationalShareView{}
	r := newReport("shares", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := observeContainers(ctx)
	if err != nil {
		r.unavailable("shares.inventory", "docker", "Local share inventory is unavailable.")
	} else {
		for _, c := range rows {
			if c.Config.Labels[managedLabel] != "true" {
				continue
			}
			s, ok := shareOf(c)
			if !ok {
				continue
			}
			u, e := url.Parse(s.URL)
			if e != nil || u.Scheme != "https" || u.User != nil || u.Host != s.Host || !validDomain(s.Host, 253) || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !previewID.MatchString(s.Preview) {
				r.unavailable("shares.metadata", "share", "Local share identity or URL is invalid.")
				continue
			}
			v := operationalShareView{Preview: s.Preview, URL: s.URL, Host: s.Host, Fixed: s.Fixed}
			if !s.Expires.IsZero() {
				expires := s.Expires.UTC()
				v.Expires = &expires
			} else {
				r.add("shares.expiry", "warn", s.Preview, "Local share expiry is unknown.")
			}
			views = append(views, v)
		}
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Preview == views[j].Preview {
			return views[i].Host < views[j].Host
		}
		return views[i].Preview < views[j].Preview
	})
	r.Data = map[string]any{"observed_at": now.UTC(), "source": "local-connectors", "shares": views}
	return r
}

type gcTarget struct {
	ID       string   `json:"id"`
	Mode     string   `json:"mode"`
	Source   string   `json:"source"`
	Outcome  string   `json:"outcome"`
	Reason   string   `json:"reason,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
type gcPlan struct {
	Root, Common string
	Targets      []gcTarget
	silent       bool
}
type gcResult struct {
	Apply   bool       `json:"apply"`
	Targets []gcTarget `json:"targets"`
	Deletes []string   `json:"deletion_categories"`
}

func planGC(root, common string) (gcPlan, error) {
	plan := gcPlan{Root: root, Common: common, Targets: []gcTarget{}}
	trees, err := worktreesOf(root)
	if err != nil {
		return plan, err
	}
	saved, err := savedPreviews(common)
	if err != nil {
		return plan, err
	}
	for _, name := range sortedKeys(saved) {
		labels := saved[name]
		path, exists, known := sourceWorktree(labels, trees)
		target := gcTarget{ID: name, Mode: firstOf(labels["devkit.mode"], "?"), Source: path, Outcome: "planned"}
		if !known {
			target.Outcome = "kept"
			target.Reason = "source_unknown"
		} else if exists {
			target.Outcome = "kept"
			target.Reason = "source_present"
		}
		plan.Targets = append(plan.Targets, target)
	}
	return plan, nil
}

// Silent profiles change output plumbing only; mutation/locking code is shared.
func gcQuietCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err := cmd.Run(); err != nil {
		return fail("Cleanup command failed or timed out; it may have partially completed.")
	}
	return nil
}

var gcSilentDestroy = func(path string) error {
	return destroyPreviewWith(path, func(path string, args ...string) error {
		dir := filepath.Dir(path)
		return gcQuietCommand(append([]string{"docker", "compose", "--project-directory", dir, "-f", path, "-p", filepath.Base(dir)}, args...)...)
	})
}
var gcSilentUnshare = func(id string) error {
	return unshareCleanupWith(id, io.Discard, func(container string) error { return gcQuietCommand("docker", "rm", "-f", container) })
}

func applyGC(plan gcPlan) (gcResult, error) {
	result := gcResult{Apply: true, Targets: append([]gcTarget{}, plan.Targets...), Deletes: []string{"containers", "networks", "disposable_named_volumes", "run_directory"}}
	for i := range result.Targets {
		if result.Targets[i].Outcome == "planned" {
			result.Targets[i].Outcome = "not_attempted"
		}
	}
	for i := range result.Targets {
		target := &result.Targets[i]
		if target.Outcome == "kept" {
			continue
		}
		err := func() error {
			unlock, err := lockPreview(target.ID)
			if err != nil {
				return err
			}
			defer unlock()
			if err := previewRecoveryGuard(target.ID, "destroy"); err != nil {
				return err
			}
			saved, err := savedPreviews(plan.Common)
			if err != nil {
				return err
			}
			trees, err := worktreesOf(plan.Root)
			if err != nil {
				return err
			}
			source, exists, known := sourceWorktree(saved[target.ID], trees)
			if !known || exists || source != target.Source {
				return fail("GC target changed after planning; inspect a fresh dry run.")
			}
			if err := assertOwner(target.ID, plan.Common); err != nil {
				return err
			}
			unshare, destroy := unshareIfShared, destroyPreview
			if plan.silent {
				unshare, destroy = gcSilentUnshare, gcSilentDestroy
			}
			if err := unshare(target.ID); err != nil {
				target.Warnings = append(target.Warnings, "Share cleanup failed; external resources may remain.")
				if plan.silent {
					failed("unshare", target.ID, fail("Share cleanup failed; external resources may remain."))
				} else {
					fmt.Fprintf(os.Stderr, "devkit: %v\n", err)
					failed("unshare", target.ID, err)
				}
			}
			if err := destroy(filepath.Join(runDir(plan.Common, target.ID), "compose.json")); err != nil {
				return err
			}
			happened("destroy", target.ID, "clean-up: worktree "+target.Source+" is gone")
			return nil
		}()
		if err != nil {
			target.Outcome = "failed"
			return result, err
		}
		target.Outcome = "destroyed"
	}
	return result, nil
}
func gc(apply bool) error {
	root, common, err := repository()
	if err != nil {
		return err
	}
	plan, err := planGC(root, common)
	if err != nil {
		return err
	}
	unknown, candidates := []gcTarget{}, []gcTarget{}
	for _, t := range plan.Targets {
		if t.Reason == "source_unknown" {
			unknown = append(unknown, t)
		}
		if t.Outcome == "planned" {
			candidates = append(candidates, t)
		}
	}
	if len(unknown) > 0 {
		fmt.Fprintln(stdout, "Kept (source worktree not recorded; destroy explicitly if unneeded):")
		for _, t := range unknown {
			fmt.Fprintf(stdout, "  %s\n", t.ID)
		}
	}
	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "No previews with a removed worktree.")
		return nil
	}
	if apply {
		fmt.Fprintln(stdout, "Destroying:")
	} else {
		fmt.Fprintln(stdout, "Would destroy (worktree no longer exists):")
	}
	for _, t := range candidates {
		fmt.Fprintf(stdout, "  %s\t%s\t%s\n", t.ID, t.Mode, t.Source)
	}
	if !apply {
		fmt.Fprintln(stdout, "Run devkit gc --yes to remove their containers, networks, volumes and run dirs.")
		return nil
	}
	if _, err := applyGC(plan); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Destroyed %d preview(s).\n", len(candidates))
	return nil
}
func gcCLI(args []string) (int, error) {
	p, v, err := splitArgs(args, nil, map[string]bool{"--json": true, "--yes": true})
	if err != nil || len(p) > 0 {
		return 2, usageError("Use gc [--yes] [--json]")
	}
	_, asJSON := v["--json"]
	_, apply := v["--yes"]
	if !asJSON {
		if apply {
			return 0, noted("gc", "", gc(true))
		}
		return 0, gc(false)
	}
	r := newReport("gc", nil)
	root, common, err := repository()
	if err != nil {
		r.unavailable("gc.repository", "checkout", "Owning repository is unavailable.")
		if apply {
			noted("gc", "", fail("Owning repository is unavailable."))
		}
		return r.write(true)
	}
	plan, err := planGC(root, common)
	if err != nil {
		r.unavailable("gc.plan", "checkout", "Cleanup selection is unavailable.")
		if apply {
			noted("gc", "", fail("Cleanup selection is unavailable."))
		}
		return r.write(true)
	}
	result := gcResult{Apply: false, Targets: plan.Targets, Deletes: []string{"containers", "networks", "disposable_named_volumes", "run_directory"}}
	if apply {
		plan.silent = true
		result, err = applyGC(plan)
		if err != nil {
			r.unavailable("gc.apply", "preview", "Cleanup stopped after a failure; inspect per-target outcomes before retrying.")
			noted("gc", "", fail("Cleanup stopped after a failure; inspect per-target outcomes."))
		}
	}
	for _, t := range result.Targets {
		if t.Reason == "source_unknown" {
			r.add("gc.source", "warn", t.ID, "Unknown source identity is kept.")
		}
		for _, warning := range t.Warnings {
			r.add("gc.share", "warn", t.ID, warning)
		}
	}
	r.Data = result
	return r.write(true)
}
