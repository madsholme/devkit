package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Dashboard actions run the devbox binary itself, exactly as the CLI would, as background jobs whose
// output the page follows. Every target is checked against what exists on this host first: a preview
// devbox manages, or a worktree of a repository devbox knows.

type actionRequest struct {
	Action   string `json:"action"`
	Preview  string `json:"preview"`
	Worktree string `json:"worktree"`
	Repo     string `json:"repo"`
	Host     string `json:"host"`
	Hours    int    `json:"hours"`
	Confirm  string `json:"confirm"`
}

// plannedAction is a checked request: the devbox arguments, where to run them and what they lock.
type plannedAction struct {
	args   []string
	dir    string
	target string
	title  string
}

// knownRepos maps each repository devbox knows (from previews and harness worktrees) to its worktrees.
func knownRepos(containers []container) map[string][]worktree {
	repos := map[string][]worktree{}
	for _, c := range containers {
		if common := c.Config.Labels["devbox.repository"]; common != "" {
			repos[common] = nil
		}
	}
	for _, common := range watchedRepositories() {
		repos[common] = nil
	}
	for common := range repos {
		repos[common], _ = worktreesOf(common)
	}
	return repos
}

// repoDir is a directory inside the repository to run commands from: the preferred worktree if it
// still exists, otherwise the first one Git lists (the main worktree).
func repoDir(trees []worktree, preferred string) string {
	if _, ok := branchOf(trees, preferred); ok && preferred != "" {
		return preferred
	}
	if len(trees) > 0 {
		return trees[0].Path
	}
	return ""
}

func planAction(req actionRequest) (plannedAction, error) {
	containers, err := dockerContainers()
	if err != nil {
		return plannedAction{}, err
	}
	repos := knownRepos(containers)
	switch req.Action {
	case "start", "stop", "destroy", "share", "unshare":
		if !previewID.MatchString(req.Preview) {
			return plannedAction{}, fail("Use an exact preview ID.")
		}
		var labels map[string]string
		for _, c := range containers {
			if c.Config.Labels["devbox.preview"] == req.Preview {
				labels = c.Config.Labels
				break
			}
		}
		if labels == nil {
			return plannedAction{}, fail("No preview %s on this host.", req.Preview)
		}
		dir := repoDir(repos[labels["devbox.repository"]], labels["devbox.worktree"])
		if dir == "" {
			return plannedAction{}, fail("The repository of %s is gone.", req.Preview)
		}
		verb := req.Action
		if verb == "unshare" {
			verb = "stop sharing"
		}
		p := plannedAction{args: []string{req.Action, req.Preview}, dir: dir, target: "preview " + req.Preview,
			title: verb + " " + req.Preview}
		switch req.Action {
		case "destroy":
			// The page confirms with a click and sends the preview ID; a stray request without it is refused.
			if req.Confirm != req.Preview {
				return plannedAction{}, fail("Confirm destroying %s and its data.", req.Preview)
			}
		case "share":
			if req.Hours < 0 {
				return plannedAction{}, fail("Hours must be positive.")
			}
			if req.Hours > 0 {
				p.args = append(p.args, "--hours", strconv.Itoa(req.Hours))
			}
			if req.Host != "" {
				if !domainLabel.MatchString(req.Host) {
					return plannedAction{}, fail("Use one of the configured share hosts.")
				}
				p.args = append(p.args, "--host", req.Host)
			}
		}
		return p, nil
	case "live", "review":
		for _, trees := range repos {
			if _, ok := branchOf(trees, req.Worktree); ok && req.Worktree != "" {
				p := plannedAction{args: []string{req.Action}, dir: req.Worktree, target: "worktree " + req.Worktree,
					title: req.Action + " " + filepath.Base(req.Worktree)}
				if req.Action == "review" {
					p.args = append(p.args, "HEAD")
				}
				return p, nil
			}
		}
		return plannedAction{}, fail("Not a worktree of a repository devbox knows.")
	case "gc":
		trees, ok := repos[req.Repo]
		dir := repoDir(trees, "")
		if !ok || dir == "" {
			return plannedAction{}, fail("Not a repository devbox knows.")
		}
		if req.Confirm != "remove" {
			return plannedAction{}, fail("Confirm removing the previews of removed worktrees.")
		}
		return plannedAction{args: []string{"gc", "--yes"}, dir: dir, target: "repo " + req.Repo,
			title: "clean up " + filepath.Base(filepath.Dir(req.Repo))}, nil
	}
	return plannedAction{}, fail("Unknown action %q.", req.Action)
}

// job is one running or finished action; its output grows while it runs.
type job struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Started  time.Time `json:"started"`
	Done     bool      `json:"done"`
	OK       bool      `json:"ok"`
	mu       sync.Mutex
	output   bytes.Buffer
	finished time.Time
	target   string
}

func (j *job) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output.Write(p)
}

// since returns the output after offset, and whether the job has finished.
func (j *job) since(offset int) (string, int, bool, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	data := j.output.Bytes()
	offset = min(max(offset, 0), len(data))
	return string(data[offset:]), len(data), j.Done, j.OK
}

type jobs struct {
	mu   sync.Mutex
	list map[string]*job
	next int
}

// runAction is how a job executes; tests replace it.
var runAction = func(p plannedAction, out *job) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, p.args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = p.dir, out, out
	return cmd.Run()
}

// start runs a planned action unless another job is already working on the same target.
func (s *jobs) start(p plannedAction) (*job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.list {
		j.mu.Lock()
		busy, old := !j.Done && j.target == p.target, j.Done && time.Since(j.finished) > time.Hour
		j.mu.Unlock()
		if busy {
			return nil, fail("%s is busy: %s is still running.", p.target, j.Title)
		}
		if old {
			delete(s.list, id)
		}
	}
	s.next++
	j := &job{ID: strconv.Itoa(s.next), Title: p.title, Started: time.Now(), target: p.target}
	s.list[j.ID] = j
	fmt.Fprintf(j, "$ devbox %s\n", strings.Join(p.args, " "))
	go func() {
		err := runAction(p, j)
		j.mu.Lock()
		j.Done, j.OK, j.finished = true, err == nil, time.Now()
		j.mu.Unlock()
		if err != nil {
			fmt.Fprintf(j, "\n%v\n", err)
		}
	}()
	return j, nil
}

func (s *jobs) get(id string) *job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list[id]
}
