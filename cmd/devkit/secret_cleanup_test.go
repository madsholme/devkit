package main

import (
	"context"
	"testing"
	"time"
)

func TestSecretCleanupPolicyOffNeverAndResume(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	cfg := defaultCleanupConfig()
	if cfg.Branch != "7d" || cfg.Worktree != "7d" || cfg.Project != "30d" || !cfg.Automatic {
		t.Fatalf("defaults %+v", cfg)
	}
	d, e := retentionDeadline(cfg, "branch", now)
	if e != nil || d == nil || !d.Equal(now.Add(7*24*time.Hour)) {
		t.Fatal("default deadline")
	}
	cfg.Branch = "2d"
	d, e = retentionDeadline(cfg, "branch", now)
	if e != nil || !d.Equal(now.Add(48*time.Hour)) {
		t.Fatal("custom duration")
	}
	cfg.Branch = "never"
	d, e = retentionDeadline(cfg, "branch", now)
	if e != nil || d != nil {
		t.Fatal("never retention")
	}
	for _, bad := range []string{"0d", "-1d", "500000000000d", "garbage", "0h"} {
		cfg.Branch = bad
		if e = validateCleanupConfig(cfg); e == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	cfg = defaultCleanupConfig()
	deadline := now.Add(-time.Hour)
	r := secretResource{ID: "b", Kind: "branch", State: "retired", Deadline: &deadline}
	if !cleanupDue(cfg, r, now) {
		t.Fatal("expired default not due")
	}
	cfg.Automatic = false
	if cleanupDue(cfg, r, now) {
		t.Fatal("automatic off purges")
	}
	cfg.Automatic = true
	cfg.ResumeAfter = now.Add(time.Minute)
	if cleanupDue(cfg, r, now) {
		t.Fatal("overdue batch purged without resume confirmation")
	}
	cfg.ResumeAfter = time.Time{}
	cfg.Branch = "never"
	if cleanupDue(cfg, r, now) {
		t.Fatal("never purges existing key")
	}
}
func TestSecretCleanupParentCannotShortenRetention(t *testing.T) {
	now := time.Now()
	old := now.Add(90 * 24 * time.Hour)
	proposed := now.Add(30 * 24 * time.Hour)
	r := secretResource{State: "retired", Deadline: &old}
	extendRetirement(&r, &proposed)
	if !r.Deadline.Equal(old) {
		t.Fatal("shortened existing protection")
	}
	r.Deadline = nil
	extendRetirement(&r, &proposed)
	if r.Deadline != nil {
		t.Fatal("shortened indefinite protection")
	}
}

func TestSecretCleanupAutomaticOffAndNeverDoNotDestroyVersions(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	execute := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, e := executeSecret(context.Background(), q)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	resource := execute(secretRequest{Operation: "register"}).Context.Worktree
	execute(secretRequest{Operation: "set", Scope: "worktree", Key: "PASSWORD", Value: "recover-me"})
	q := secretRequest{Operation: "cleanup-retire", Resource: resource}
	q.Confirm = execute(q).Confirmation
	execute(q)
	cfg := defaultCleanupConfig()
	cfg.Automatic = false
	if e := saveCleanupConfig(cfg); e != nil {
		t.Fatal(e)
	}
	now := time.Now().Add(365 * 24 * time.Hour)
	if e := runSecretCleanup(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	r, e := loadSecretRegistry()
	if e != nil || r.Resources[resource].State != "retired" || len(r.Keys) != 1 {
		t.Fatal("off destroyed stored versions")
	}
	cfg.Automatic = true
	cfg.Worktree = "never"
	if e = saveCleanupConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if e = runSecretCleanup(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	r, _ = loadSecretRegistry()
	if len(r.Keys) != 1 {
		t.Fatal("never destroyed versions")
	}
	cfg.Worktree = "7d"
	if e = saveCleanupConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if e = runSecretCleanup(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	r, _ = loadSecretRegistry()
	if len(r.Keys) != 0 || r.Resources[resource].State != "destroyed" {
		t.Fatal("expired scope not fully destroyed")
	}
}

func TestSecretCleanupReenableConfirmationIsStable(t *testing.T) {
	secretFixture(t)
	cfg := defaultCleanupConfig()
	cfg.Automatic = false
	if e := saveCleanupConfig(cfg); e != nil {
		t.Fatal(e)
	}
	cfg.Automatic = true
	q := secretRequest{Operation: "cleanup-config", Cleanup: &cfg}
	first, e := executeSecret(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	q.Confirm = first.Confirmation
	out, e := executeSecret(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	actual, e := loadCleanupConfig()
	if e != nil || !actual.Automatic || actual.ResumeAfter.IsZero() {
		t.Fatalf("re-enable not applied or missing overdue barrier: %+v output %+v", actual, out)
	}
}

func TestSecretScopeRestorePreservesIndependentDeletion(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, e := executeSecret(context.Background(), q)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	registered := run(secretRequest{Operation: "register"})
	run(secretRequest{Operation: "set", Scope: "worktree", Key: "OLD", Value: "old"})
	preview := run(secretRequest{Operation: "delete", Scope: "worktree", Key: "OLD"})
	run(secretRequest{Operation: "delete", Scope: "worktree", Key: "OLD", Confirm: preview.Confirmation})
	run(secretRequest{Operation: "set", Scope: "worktree", Key: "CURRENT", Value: "current"})
	retire := secretRequest{Operation: "cleanup-retire", Resource: registered.Context.Worktree}
	preview = run(retire)
	if len(preview.Resources) != 1 || preview.Resources[0].Deadline == nil {
		t.Fatal("retirement preview omits deadline")
	}
	retire.Confirm = preview.Confirmation
	run(retire)
	restore := secretRequest{Operation: "cleanup-restore", Resource: registered.Context.Worktree}
	preview = run(restore)
	restore.Confirm = preview.Confirmation
	run(restore)
	r, e := loadSecretRegistry()
	if e != nil {
		t.Fatal(e)
	}
	if !r.Keys[secretKeyID(registered.Context.Worktree, "OLD")].Deleted {
		t.Fatal("restored separately deleted password")
	}
	if r.Keys[secretKeyID(registered.Context.Worktree, "CURRENT")].Deleted {
		t.Fatal("failed to restore scope password")
	}
}

func TestSecretCleanupResumeDoesNotFinishOverduePendingPurgeWithoutReview(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	created, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = executeSecret(context.Background(), secretRequest{Operation: "set", Root: repo.root, Scope: "worktree", Key: "PASSWORD", Value: "retained"}); e != nil {
		t.Fatal(e)
	}
	r, _ := loadSecretRegistry()
	key := r.Keys[secretKeyID(created.Context.Worktree, "PASSWORD")]
	past := time.Now().Add(-time.Hour)
	key.Deleted = true
	key.Deadline = &past
	r.Pending = &secretOperation{ID: secretID(), Kind: "purge-key", Resource: key.Resource, Key: key.Name}
	if e = saveSecretRegistry(&r); e != nil {
		t.Fatal(e)
	}
	cfg := defaultCleanupConfig()
	cfg.ResumeAfter = time.Now()
	if e = saveCleanupConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if e = runSecretCleanup(context.Background(), time.Now()); e != nil {
		t.Fatal(e)
	}
	r, _ = loadSecretRegistry()
	if r.Keys[secretKeyID(key.Resource, key.Name)] == nil {
		t.Fatal("overdue pending purge bypassed resume review")
	}
}
