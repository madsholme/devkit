package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const managedLabel = "devbox.managed"

type projectConfig struct {
	Project    string            `json:"project"`
	WebService string            `json:"web_service"`
	WebPort    any               `json:"web_port"`
	Compose    map[string]string `json:"compose"`
	Hooks      map[string]string `json:"hooks"`
	Routes     []route           `json:"routes"`
	MediaPorts []mediaPort       `json:"media_ports"`
	// WaitTimeout is how many seconds live, review and start wait for the preview to become healthy.
	WaitTimeout any `json:"wait_timeout"`
}

// route sends more of the preview's traffic to a service besides the web service: requests whose path
// starts with one of Paths, or (Subdomains) every host one level below the preview's host.
type route struct {
	Service    string   `json:"service"`
	Port       any      `json:"port"`
	Paths      []string `json:"paths"`
	Subdomains bool     `json:"subdomains"`
}

func (c projectConfig) webService() string {
	if c.WebService == "" {
		return "web"
	}
	return c.WebService
}

func (c projectConfig) webPort() string {
	if c.WebPort == nil {
		return "8000"
	}
	return fmt.Sprint(c.WebPort)
}

var projectSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func loadConfig(root string) (projectConfig, error) {
	var cfg projectConfig
	data, err := os.ReadFile(filepath.Join(root, "devbox.json"))
	if err != nil {
		return cfg, fail("Missing devbox.json. Adapt the project-template first.")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fail("devbox.json: %v", err)
	}
	if !projectSlug.MatchString(cfg.Project) {
		return cfg, fail("devbox.json project must be a unique lowercase project slug (1–32 characters).")
	}
	if cfg.WaitTimeout != nil {
		if seconds, err := strconv.Atoi(fmt.Sprint(cfg.WaitTimeout)); err != nil || seconds < 1 {
			return cfg, fail("devbox.json wait_timeout must be a whole number of seconds.")
		}
	}
	for _, r := range cfg.Routes {
		if err := r.validate(); err != nil {
			return cfg, err
		}
	}
	if err := validateMediaPorts(cfg.MediaPorts); err != nil {
		return cfg, err
	}
	return cfg, nil
}

var routePath = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)

func (r route) port() string { return fmt.Sprint(r.Port) }

func (r route) validate() error {
	if !serviceName.MatchString(r.Service) {
		return fail("devbox.json routes need a Compose service name.")
	}
	if port, err := strconv.Atoi(r.port()); err != nil || port < 1 || port > 65535 {
		return fail("devbox.json route to %s needs a port number.", r.Service)
	}
	if len(r.Paths) == 0 && !r.Subdomains {
		return fail("devbox.json route to %s needs paths, subdomains or both.", r.Service)
	}
	for _, p := range r.Paths {
		if !routePath.MatchString(p) || p == "/" {
			return fail("devbox.json route path %q must start with / and name a prefix.", p)
		}
	}
	return nil
}

var domainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// domain is the preview domain: DEV_DOMAIN if set, the host config's otherwise.
func domain() (string, error) {
	value := os.Getenv("DEV_DOMAIN")
	if value == "" {
		cfg, err := loadHostConfig()
		if err != nil {
			return "", err
		}
		value = cfg.Domain
	}
	value = strings.TrimRight(strings.ToLower(value), ".")
	if !validDomain(value, 180) {
		return "", fail("Set the preview domain with devbox setup (or DEV_DOMAIN), without a wildcard prefix.")
	}
	return value, nil
}

// repository returns the current worktree root and the repository's common Git directory.
var repository = func() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root, err := git(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	common, err := git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return root, common, err
}

type worktree struct{ Path, Branch string }

// worktreesOf lists each existing worktree of the repository with its branch ("" if detached), in Git's order.
var worktreesOf = func(root string) ([]worktree, error) {
	out, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var trees []worktree
	path, branch, prunable, locked := "", "", false, false
	for _, line := range append(strings.Split(out, "\n"), "") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path, branch, prunable, locked = strings.TrimPrefix(line, "worktree "), "", false, false
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case strings.HasPrefix(line, "prunable"):
			prunable = true
		case strings.HasPrefix(line, "locked"):
			locked = true
		case line == "" && path != "":
			// A prunable worktree's directory is gone; Git just hasn't forgotten it yet.
			// A locked one counts as existing even when its directory is unavailable.
			if info, err := os.Stat(path); locked || (!prunable && err == nil && info.IsDir()) {
				trees = append(trees, worktree{path, branch})
			}
			path = ""
		}
	}
	return trees, nil
}

func branchOf(trees []worktree, path string) (string, bool) {
	for _, t := range trees {
		if t.Path == path {
			return t.Branch, true
		}
	}
	return "", false
}

// sourceWorktree returns the worktree a preview came from and whether it still exists; ok is false if unknown.
func sourceWorktree(labels map[string]string, trees []worktree) (path string, exists, ok bool) {
	if wt := labels["devbox.worktree"]; wt != "" {
		_, exists = branchOf(trees, wt)
		return wt, exists, true
	}
	// Previews made before devbox.worktree was recorded: live ones are identified by branch.
	if branch := labels["devbox.branch"]; labels["devbox.mode"] == "live" && branch != "" {
		for _, t := range trees {
			if t.Branch == branch {
				return t.Path, true, true
			}
		}
		return "(branch " + branch + ")", false, true
	}
	return "", false, false
}

func runDir(common, name string) string { return filepath.Join(common, "devbox", "runs", name) }

// savedPreviews returns the labels of every preview with saved configuration in this repository, by preview ID.
var savedPreviews = func(common string) (map[string]map[string]string, error) {
	paths, _ := filepath.Glob(filepath.Join(common, "devbox", "runs", "*", "compose.json"))
	sort.Strings(paths)
	previews := map[string]map[string]string{}
	for _, path := range paths {
		project, err := readCompose(path)
		if err != nil {
			return nil, err
		}
		labels := map[string]string{}
		services := object(project, "services")
		for _, name := range sortedKeys(services) {
			if l := stringMap(object(asObject(services[name]), "labels")); l[managedLabel] == "true" {
				labels = l
				break
			}
		}
		previews[filepath.Base(filepath.Dir(path))] = labels
	}
	return previews, nil
}

func liveIdentity(root string) (branch, identity string, err error) {
	abbrev, err := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	if abbrev != "HEAD" {
		if branch, err = git(root, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
			return "", "", err
		}
		return branch, branch, nil
	}
	return "", "detached-" + digest(root)[:10], nil
}

// currentPreview resolves the single preview belonging to the current worktree.
func currentPreview(root, common string) (string, error) {
	trees, err := worktreesOf(root)
	if err != nil {
		return "", err
	}
	saved, err := savedPreviews(common)
	if err != nil {
		return "", err
	}
	candidates := map[string]bool{}
	for name, labels := range saved {
		if path, _, ok := sourceWorktree(labels, trees); ok && path == root {
			candidates[name] = true
		}
	}
	if isFile(filepath.Join(root, "devbox.json")) {
		// The preview devbox live would use here, even if it predates devbox.worktree.
		cfg, err := loadConfig(root)
		if err != nil {
			return "", err
		}
		_, identity, err := liveIdentity(root)
		if err != nil {
			return "", err
		}
		if live := findSaved(saved, cfg.Project, "live", identity); live != "" {
			candidates[live] = true
		}
	}
	names := sortedKeys(candidates)
	switch len(names) {
	case 0:
		return "", fail("No preview belongs to this worktree (%s). Pass a preview ID from devbox ls.", root)
	case 1:
		return names[0], nil
	}
	lines := []string{"Several previews belong to this worktree; pass one explicitly:"}
	for _, name := range names {
		mode := saved[name]["devbox.mode"]
		if mode == "" {
			mode = "?"
		}
		lines = append(lines, "  "+name+"\t"+mode)
	}
	return "", fail("%s", strings.Join(lines, "\n"))
}

// hook runs a project hook from devbox.json; a missing hook is skipped.
var hook = func(root string, cfg projectConfig, name string, env []string) (bool, error) {
	command := cfg.Hooks[name]
	if command == "" {
		return false, nil
	}
	_, err := runCommand([]string{"sh", "-euc", command}, runOpts{dir: root, env: env})
	return true, err
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
