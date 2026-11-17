package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type previewLogOptions struct {
	tail   int
	since  string
	follow bool
}

func normalizeLogSince(value string, now time.Time) (string, error) {
	if d, err := time.ParseDuration(value); err == nil {
		if d <= 0 || d > 720*time.Hour {
			return "", fail("Since duration must be positive and at most 720h.")
		}
		return now.Add(-d).UTC().Format(time.RFC3339Nano), nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.After(now) {
		return "", fail("Since must be a past RFC3339 time or a positive duration up to 720h.")
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}
func logsCLI(args []string) (int, error) {
	p, v, err := splitArgs(args, map[string]bool{"--preview": true, "-s": true, "--service": true, "--tail": true, "--since": true}, map[string]bool{"-f": true, "--follow": true})
	if err != nil || len(p) != 0 {
		return 2, usageError("Use logs [--preview ID] [-s SERVICE] [--tail N] [--since TIME] [-f]")
	}
	opts := previewLogOptions{tail: 200}
	_, f := v["-f"]
	_, follow := v["--follow"]
	opts.follow = f || follow
	name := v["--preview"]
	service := firstOf(v["-s"], v["--service"])
	_, shortService := v["-s"]
	_, longService := v["--service"]
	if n, ok := v["--preview"]; ok && !previewID.MatchString(n) {
		return 2, fail("Use an exact preview ID.")
	}
	if (v["-s"] != "" && v["--service"] != "") || ((shortService || longService) && !serviceName.MatchString(service)) {
		return 2, fail("Use one exact service name.")
	}
	if s, ok := v["--tail"]; ok {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 10000 {
			return 2, fail("Tail must be an integer from 1 to 10000.")
		}
		opts.tail = n
	}
	if s, ok := v["--since"]; ok {
		opts.since, err = normalizeLogSince(s, time.Now())
		if err != nil {
			return 2, err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	observeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	target, err := resolveObservedPreview(observeCtx, name)
	if err != nil {
		return 1, err
	}
	c, err := selectObservedService(target, service)
	if err != nil {
		return 1, err
	}
	// Resolve again before opening the stream; never silently substitute a recreated container.
	fresh, err := resolveObservedPreview(observeCtx, target.ID)
	if err != nil {
		return 1, err
	}
	current, err := selectObservedService(fresh, service)
	if err != nil || current.ID != c.ID || fresh.Mode != target.Mode {
		return 1, fail("Preview changed during log selection; inspect it again.")
	}
	runCtx := observeCtx
	if opts.follow {
		runCtx = ctx
	}
	if err := runPreviewLogs(runCtx, c.ID, opts); err != nil {
		return 1, err
	}
	return 0, nil
}

var previewLogCommand = func(ctx context.Context, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Run()
}

type previewLogWriter struct {
	mu        sync.Mutex
	out       io.Writer
	remaining int
	cancel    context.CancelFunc
	overflow  bool
}

func (w *previewLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > w.remaining {
		n, err := w.out.Write(p[:w.remaining])
		w.remaining -= n
		w.overflow = true
		w.cancel()
		if err != nil {
			return n, err
		}
		return n, fail("Log output truncated at 4 MiB.")
	}
	n, err := w.out.Write(p)
	w.remaining -= n
	return n, err
}
func runPreviewLogs(ctx context.Context, id string, opts previewLogOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"logs", "--tail", strconv.Itoa(opts.tail)}
	if opts.since != "" {
		args = append(args, "--since", opts.since)
	}
	if opts.follow {
		args = append(args, "--follow")
	}
	args = append(args, id)
	var out io.Writer = stdout
	bounded := &previewLogWriter{out: stdout, remaining: 4 << 20, cancel: cancel}
	if !opts.follow {
		out = bounded
	}
	err := previewLogCommand(ctx, args, out)
	if bounded.overflow {
		return fail("Log output truncated at 4 MiB; narrow --tail or --since.")
	}
	if err != nil {
		return fail("Could not finish reading logs; container unavailable, command failed or reading cancelled.")
	}
	return nil
}
