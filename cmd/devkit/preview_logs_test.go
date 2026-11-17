package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewLogsStoppedReview(t *testing.T) {
	c := observedFixture(t)
	c.Config.Labels["devkit.mode"] = "review"
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	var got []string
	swap(t, &previewLogCommand, func(ctx context.Context, args []string, out io.Writer) error {
		got = args
		_, err := out.Write([]byte("raw-secret-SENTINEL\n"))
		return err
	})
	var out bytes.Buffer
	swap(t, &stdout, io.Writer(&out))
	code, err := dispatch([]string{"logs", "--preview", "demo-main", "--tail", "12", "--since", "2026-01-01T00:00:00Z"})
	if err != nil || code != 0 || !strings.Contains(strings.Join(got, " "), "--tail 12 --since 2026-01-01T00:00:00Z "+c.ID) || out.String() != "raw-secret-SENTINEL\n" {
		t.Fatal(code, err, got, out.String())
	}
}
func TestPreviewLogsArgumentLimits(t *testing.T) {
	observedFixture(t)
	for _, args := range [][]string{{"--tail", "all"}, {"--tail", "0"}, {"--tail", "10001"}, {"--tail", "-1"}, {"--since", "-1h"}, {"--since", "721h"}, {"--since", "2999-01-01T00:00:00Z"}, {"--preview", "../x"}, {"-s", "../x"}, {"extra"}} {
		code, _ := logsCLI(args)
		if code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	got, err := normalizeLogSince("1h", now)
	if err != nil || got != "2026-11-17T11:00:00Z" {
		t.Fatal(got, err)
	}
}
func TestPreviewLogsOutputLimitAndCancel(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &previewLogWriter{out: &out, remaining: 4, cancel: cancel}
	n, err := w.Write([]byte("123456"))
	if n != 4 || err == nil || out.String() != "1234" || ctx.Err() == nil {
		t.Fatal(n, err, out.String(), ctx.Err())
	}
	// A real child is killed and reaped on cancellation, including inherited pipes.
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 20\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx2, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	started := time.Now()
	err = previewLogCommand(ctx2, []string{"logs", "id"}, io.Discard)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatal(err, time.Since(started))
	}
}
func TestPreviewLogsRevalidation(t *testing.T) {
	c := observedFixture(t)
	calls := 0
	swap(t, &observeContainers, func(context.Context) ([]container, error) {
		calls++
		if calls == 1 {
			return []container{c}, nil
		}
		return nil, nil
	})
	swap(t, &previewLogCommand, func(context.Context, []string, io.Writer) error { t.Fatal("logs after disappearance"); return nil })
	if code, _ := logsCLI(nil); code != 1 {
		t.Fatal(code)
	}
}
