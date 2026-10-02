package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

// The history is an append-only log of what devbox changed on this host: previews started, stopped
// and destroyed, shares created, extended, ended and expired, and commands that failed. Every devbox
// process appends one JSON line per event; the dashboard and devbox history read it back. It is the
// event record outside Docker labels; losing it loses nothing but the past.

type event struct {
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	Preview  string    `json:"preview,omitempty"`
	Worktree string    `json:"worktree,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	By       string    `json:"by"`
	OK       bool      `json:"ok"`
}

// historyLimit is the size at which the log moves to history.jsonl.1, replacing the older one.
const historyLimit = 1 << 20

func stateDir() string {
	if dir := os.Getenv("DEVBOX_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "devbox")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "devbox")
}

func historyPath() string { return filepath.Join(stateDir(), "history.jsonl") }

// actor says who caused an event: "dashboard" for actions the page runs (it sets DEVBOX_BY),
// "auto" for the dashboard's own background work such as expiring shares, and "cli" otherwise.
var actor = func() string {
	if by := os.Getenv("DEVBOX_BY"); by != "" {
		return by
	}
	return "cli"
}()

// record appends an event. A history that cannot be written never fails the command itself.
func record(e event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Time = e.Time.UTC().Truncate(time.Second)
	if e.By == "" {
		e.By = actor
	}
	if err := appendEvent(e); err != nil {
		fmt.Fprintf(os.Stderr, "devbox: history: %v\n", err)
	}
}

func appendEvent(e event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		return err
	}
	path := historyPath()
	if info, err := os.Stat(path); err == nil && info.Size() > historyLimit {
		// Rename is atomic; a process still holding the old file finishes its line there.
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	// One write per line keeps concurrent appends from interleaving.
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// happened records something devbox did.
func happened(action, preview, detail string) {
	record(event{Action: action, Preview: preview, Detail: detail, OK: true})
}

// noted records a failed command and passes its error on.
func noted(action, preview string, err error) error {
	if err != nil {
		failed(action, preview, err)
	}
	return err
}

// failed records a command that ended with an error.
func failed(action, preview string, err error) {
	detail := err.Error()
	if len(detail) > 400 {
		detail = detail[:400] + "…"
	}
	worktree := ""
	if preview == "" {
		worktree, _ = os.Getwd()
	}
	record(event{Action: action, Preview: preview, Worktree: worktree, Detail: detail})
}

// readHistory returns the most recent events, newest first, optionally only those of one preview.
func readHistory(preview string, limit int) ([]event, error) {
	var events []event
	for _, path := range []string{historyPath() + ".1", historyPath()} {
		f, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			var e event
			// A line cut short by a crash is skipped rather than hiding the rest.
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				continue
			}
			if preview == "" || e.Preview == preview {
				events = append(events, e)
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func shortHome(path string) string {
	home, _ := os.UserHomeDir()
	if rest, ok := strings.CutPrefix(path, home+"/"); ok && home != "" {
		return "~/" + rest
	}
	return path
}

// showHistory is devbox history: recent events, oldest first so the newest ends up next to the prompt.
func showHistory(preview string, limit int) error {
	if preview != "" && !previewID.MatchString(preview) {
		return fail("Use an exact preview ID from devbox ls.")
	}
	events, err := readHistory(preview, limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintln(stdout, "No history yet.")
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tBY\tACTION\tPREVIEW\tDETAIL")
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		action := e.Action
		if !e.OK {
			action += " (failed)"
		}
		target := e.Preview
		if target == "" {
			target = shortHome(e.Worktree)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Time.Local().Format("2006-01-02 15:04"), e.By, action,
			target, strings.ReplaceAll(e.Detail, "\n", " "))
	}
	return w.Flush()
}
