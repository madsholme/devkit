package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type cleanupConfig struct {
	Automatic   bool      `json:"automatic"`
	Branch      string    `json:"branch"`
	Worktree    string    `json:"worktree"`
	Project     string    `json:"project"`
	ResumeAfter time.Time `json:"resume_after,omitempty"`
}

func defaultCleanupConfig() cleanupConfig {
	return cleanupConfig{Automatic: true, Branch: "7d", Worktree: "7d", Project: "30d"}
}
func retentionDuration(s string) (time.Duration, error) {
	if s == "never" {
		return 0, nil
	}
	var d time.Duration
	var e error
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 32)
		if err != nil || n < 1 || n > 36500 {
			return 0, fail("Retention must be 1h–36500d, or never")
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		d, e = time.ParseDuration(s)
	}
	if e != nil || d < time.Hour || d > 36500*24*time.Hour {
		return 0, fail("Retention must be 1h–36500d, or never")
	}
	return d, nil
}
func validateCleanupConfig(c cleanupConfig) error {
	for _, s := range []string{c.Branch, c.Worktree, c.Project} {
		if _, e := retentionDuration(s); e != nil {
			return e
		}
	}
	return nil
}
func loadCleanupConfig() (cleanupConfig, error) {
	c := defaultCleanupConfig()
	b, e := readPrivate(filepath.Join(secretStateDir(), "cleanup.json"), 65536)
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if json.Unmarshal(b, &c) != nil {
		return c, fail("Invalid cleanup configuration")
	}
	return c, validateCleanupConfig(c)
}
func saveCleanupConfig(c cleanupConfig) error {
	if e := validateCleanupConfig(c); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writePrivate(filepath.Join(secretStateDir(), "cleanup.json"), b)
}
func retentionValue(c cleanupConfig, kind string) string {
	switch kind {
	case "branch":
		return c.Branch
	case "worktree":
		return c.Worktree
	case "project":
		return c.Project
	}
	return "invalid"
}
func retentionDeadline(c cleanupConfig, kind string, now time.Time) (*time.Time, error) {
	d, e := retentionDuration(retentionValue(c, kind))
	if e != nil || d == 0 {
		return nil, e
	}
	at := now.Add(d)
	return &at, nil
}
func cleanupDue(c cleanupConfig, r secretResource, now time.Time) bool {
	return c.Automatic && r.State == "retired" && r.Deadline != nil && retentionValue(c, firstOf(r.RetentionKind, r.Kind)) != "never" && !now.Before(*r.Deadline) && (c.ResumeAfter.IsZero() || r.Deadline.After(c.ResumeAfter))
}
func extendRetirement(r *secretResource, deadline *time.Time) {
	if r.State == "retired" {
		if r.Deadline == nil {
			return
		}
		if deadline != nil && r.Deadline.After(*deadline) {
			return
		}
	}
	r.Deadline = deadline
}
