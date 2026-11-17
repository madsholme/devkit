package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperationalReportsHistoryProjection(t *testing.T) {
	freshHistory(t)
	for _, action := range []string{"live", "stop"} {
		record(event{Action: action, Preview: "demo", By: "secret-SENTINEL", Detail: "secret-SENTINEL", OK: true})
	}
	r := historyReport("demo", 30)
	b, _ := json.Marshal(r)
	if r.exitCode() != 0 || strings.Contains(string(b), "SENTINEL") || !strings.Contains(string(b), `"by":"other"`) || strings.Index(string(b), `"action":"live"`) > strings.Index(string(b), `"action":"stop"`) {
		t.Fatal(string(b))
	}
}
func TestOperationalReportsShares(t *testing.T) {
	c := labelled(map[string]string{managedLabel: "true", "devkit.service": "share", "devkit.preview": "demo", "devkit.share.url": "https://public.example.com", "devkit.share.host": "public.example.com", "devkit.share.expires": "bad", "devkit.share.tunnel": "SENTINEL"})
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	r := sharesReport(time.Now())
	b, _ := json.Marshal(r)
	if r.exitCode() != 0 || !strings.Contains(string(b), `"expires":null`) || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(string(b))
	}
	c.Config.Labels["devkit.share.url"] = "https://user:SECRET-SENTINEL@public.example.com/"
	r = sharesReport(time.Now())
	b, _ = json.Marshal(r)
	if r.Complete || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(string(b))
	}
}
func gcFixture(t *testing.T) gcPlan {
	t.Helper()
	freshHistory(t)
	root := t.TempDir()
	saved := map[string]map[string]string{}
	for _, id := range []string{"a", "b", "c"} {
		saved[id] = map[string]string{"devkit.worktree": "/gone/" + id, "devkit.mode": "live"}
		os.MkdirAll(runDir(root, id), 0700)
		os.WriteFile(filepath.Join(runDir(root, id), "compose.json"), []byte("retained"), 0600)
	}
	swap(t, &repository, func() (string, string, error) { return root, root, nil })
	swap(t, &savedPreviews, func(string) (map[string]map[string]string, error) { return saved, nil })
	swap(t, &worktreesOf, func(string) ([]worktree, error) { return nil, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &unshareIfShared, func(string) error { return nil })
	swap(t, &gcSilentUnshare, func(string) error { return nil })
	swap(t, &stdout, io.Discard)
	p, err := planGC(root, root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestOperationalReportsGCParity(t *testing.T) {
	p := gcFixture(t)
	swap(t, &destroyPreview, func(string) error { t.Fatal("dry run mutated"); return nil })
	if err := gc(false); err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 3 {
		t.Fatal(p)
	}
	code, r := validationCLI(t, "gc", "--json")
	if code != 0 || r["complete"] != true {
		t.Fatal(r)
	}
}
func TestOperationalReportsGCPartialFailure(t *testing.T) {
	p := gcFixture(t)
	calls := []string{}
	swap(t, &destroyPreview, func(path string) error {
		id := filepath.Base(filepath.Dir(path))
		calls = append(calls, id)
		if id == "b" {
			return fail("SECRET-SENTINEL")
		}
		return os.Remove(path)
	})
	result, err := applyGC(p)
	if err == nil || strings.Join(calls, ",") != "a,b" || result.Targets[0].Outcome != "destroyed" || result.Targets[1].Outcome != "failed" || result.Targets[2].Outcome != "not_attempted" {
		t.Fatal(result, err, calls)
	}
	if _, err := os.Stat(filepath.Join(runDir(p.Common, "c"), "compose.json")); err != nil {
		t.Fatal(err)
	}
	events, _ := readHistory("", 0)
	if len(events) != 1 || events[0].Action != "destroy" {
		t.Fatal(events)
	}
}
func TestOperationalReportsGCRevalidation(t *testing.T) {
	for _, reason := range []string{"source", "owner", "recovery", "lock"} {
		t.Run(reason, func(t *testing.T) {
			p := gcFixture(t)
			swap(t, &destroyPreview, func(string) error { t.Fatal("unsafe destroy"); return nil })
			switch reason {
			case "source":
				swap(t, &worktreesOf, func(string) ([]worktree, error) { return []worktree{{Path: "/gone/a"}}, nil })
			case "owner":
				swap(t, &assertOwner, func(string, string) error { return fail("foreign") })
			case "recovery":
				if err := saveRebuildOperation(rebuildOperation{Preview: "a", Phase: "failed"}); err != nil {
					t.Fatal(err)
				}
			case "lock":
				unlock, err := lockPreview("a")
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}
			result, err := applyGC(p)
			if err == nil || result.Targets[0].Outcome != "failed" || result.Targets[1].Outcome != "not_attempted" {
				t.Fatal(result, err)
			}
		})
	}
}
func TestOperationalReportsJSONFailures(t *testing.T) {
	for _, args := range [][]string{{"history", "--json", "-n", "bad"}, {"shares", "--json", "extra"}, {"gc", "--json", "extra"}} {
		code, r := validationCLI(t, args...)
		if code != 2 || r["complete"] != false {
			t.Fatal(args, code, r)
		}
	}
	gcFixture(t)
	swap(t, &gcSilentDestroy, func(string) error { return fail("SECRET-SENTINEL") })
	code, r := validationCLI(t, "gc", "--yes", "--json")
	b, _ := json.Marshal(r)
	if code != 1 || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(code, string(b))
	}
}
func TestOperationalReportsGCUnshareWarning(t *testing.T) {
	p := gcFixture(t)
	swap(t, &unshareIfShared, func(string) error { return fail("secret-SENTINEL") })
	swap(t, &destroyPreview, func(string) error { return nil })
	result, err := applyGC(p)
	if err != nil || result.Targets[0].Outcome != "destroyed" || len(result.Targets[0].Warnings) != 1 {
		t.Fatal(result, err)
	}
}
