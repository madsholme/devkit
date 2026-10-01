package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain keeps every test's history out of the real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "devbox-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("DEVBOX_STATE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func freshHistory(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("DEVBOX_STATE_DIR", dir)
	return filepath.Join(dir, "history.jsonl")
}

func TestHistoryReadsNewestFirstAndSkipsBrokenLines(t *testing.T) {
	path := freshHistory(t)
	swap(t, &actor, "dashboard")
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	happened("live", "demo-main", "created at 3052f1ef86b4")
	record(event{Time: start.Add(time.Minute), Action: "share", Preview: "demo-feature", Detail: "https://x", OK: true})
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString(`{"time": "2026-10-01T09:0` + "\n")
	f.Close()
	failed("stop", "demo-main", fail("Preview demo-main is gone."))
	events, err := readHistory("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Action != "stop" || events[0].OK || events[2].Action != "live" || !events[2].OK {
		t.Fatalf("events %+v", events)
	}
	if events[0].By != "dashboard" || events[0].Detail != "Preview demo-main is gone." {
		t.Errorf("failure %+v", events[0])
	}
	mine, _ := readHistory("demo-main", 0)
	last, _ := readHistory("", 1)
	if len(mine) != 2 || len(last) != 1 || last[0].Action != "stop" {
		t.Errorf("filtered %+v, last %+v", mine, last)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("history mode %v", info.Mode().Perm())
	}
}

func TestHistoryMovesAsideWhenFullAndStillReadsBoth(t *testing.T) {
	path := freshHistory(t)
	happened("start", "demo-old", "")
	os.WriteFile(path, append(bytes.Repeat([]byte("\n"), historyLimit), []byte(`{"action":"stop","preview":"demo-old","ok":true}`+"\n")...), 0o600)
	happened("start", "demo-new", "")
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal("full history was not moved aside")
	}
	events, _ := readHistory("", 0)
	if len(events) != 2 || events[0].Preview != "demo-new" || events[1].Action != "stop" {
		t.Errorf("events %+v", events)
	}
}

func TestStopExpiryAndFailuresAreRecorded(t *testing.T) {
	freshHistory(t)
	shareHost(t)
	newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &actor, "auto")
	swap(t, &dockerContainers, withContainers(
		shareContainer("demo-old", "demo-old.share.test", "tun-old", "dns-old", time.Now().Add(-time.Minute))))
	if _, err := reapExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	common := t.TempDir()
	path := filepath.Join(common, "devbox/runs/demo-live-123/compose.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{}"), 0o600)
	swap(t, &repository, func() (string, string, error) { return common, common, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &composeRun, func(string, ...string) error { return nil })
	swap(t, &actor, "cli")
	if _, err := dispatch([]string{"stop", "demo-live-123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch([]string{"start", "demo-missing"}); err == nil {
		t.Fatal("started a preview without a saved configuration")
	}
	events, _ := readHistory("", 0)
	var got []string
	for _, e := range events {
		got = append(got, strings.Join([]string{e.By, e.Action, e.Preview, e.Detail}, " "))
		if e.OK == (e.Preview == "demo-missing") {
			t.Errorf("ok %v for %+v", e.OK, e)
		}
	}
	want := []string{
		"cli start demo-missing No saved preview configuration in this repository.",
		"cli stop demo-live-123 ",
		"auto expire demo-old https://demo-old.share.test",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("history:\n%s", strings.Join(got, "\n"))
	}
}

func TestHistoryCommandPrintsOldestFirst(t *testing.T) {
	freshHistory(t)
	happened("live", "demo-main", "created at 3052f1ef86b4")
	failed("share", "demo-main", fail("Preview demo-main is not running; start it first."))
	var out bytes.Buffer
	swap[io.Writer](t, &stdout, &out)
	if _, err := dispatch([]string{"history", "-n", "5", "demo-main"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "live ") || !strings.Contains(lines[2], "share (failed)") ||
		!strings.Contains(lines[2], "not running") {
		t.Errorf("output:\n%s", out.String())
	}
	if _, err := dispatch([]string{"history", "-n", "0"}); err == nil {
		t.Error("accepted -n 0")
	}
}
