package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func observationGit(ctx context.Context, root string, args ...string) (string, error) {
	return observationCommand(ctx, append([]string{"git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", root}, args...)...)
}
func observationTrees(ctx context.Context, root string) ([]worktree, error) {
	out, err := observationGit(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}
func reportCLI(command string, args []string, budget time.Duration, collect func(context.Context) operationReport) (int, error) {
	p, v, err := splitArgs(args, nil, map[string]bool{"--json": true})
	if err != nil || len(p) > 0 {
		return 2, usageError("Use " + command + " [--json]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	_, asJSON := v["--json"]
	return collect(ctx).write(asJSON)
}
func projectsCLI(args []string) (int, error) {
	return reportCLI("projects", args, 30*time.Second, projectsReport)
}
func containersCLI(args []string) (int, error) {
	return reportCLI("containers", args, 30*time.Second, containersReport)
}

// These projections deliberately omit free-form notes, Git subjects/remotes and raw labels.
type inventoryCheckout struct {
	Path        string           `json:"path"`
	Branch      string           `json:"branch"`
	Project     string           `json:"project"`
	WorktreeKey string           `json:"worktree_key"`
	Git         bool             `json:"git"`
	LiveRecipe  bool             `json:"live_recipe"`
	Previews    []projectPreview `json:"previews"`
	Containers  []containerRow   `json:"containers"`
}
type inventoryProject struct {
	Name      string              `json:"name"`
	Path      string              `json:"path"`
	Main      inventoryCheckout   `json:"main"`
	Worktrees []inventoryCheckout `json:"worktrees"`
}

func inventoryCheckoutView(row projectCheckout) inventoryCheckout {
	cs := append([]containerRow{}, row.Containers...)
	for i := range cs {
		cs[i].ControlNote = ""
	}
	return inventoryCheckout{row.Path, row.Branch, row.Project, row.WorktreeKey, row.Git, row.Docker, row.Previews, cs}
}
func observeContainerRows(ctx context.Context, r *operationReport) ([]container, containersData) {
	data := containersData{Containers: []containerRow{}}
	rows, err := observeContainers(ctx)
	if err != nil {
		r.unavailable("containers.inventory", "docker", "Docker inventory is unavailable.")
		return nil, data
	}
	owners, err := existingContainerProjectsWith(rows, func(root string) ([]worktree, error) { return observationTrees(ctx, root) })
	if err != nil {
		owners = nil
		r.unavailable("containers.registration", "projects", "Container registration evidence is incomplete or conflicting.")
	}
	data.Containers = projectContainerRows(rows, owners)
	for i := range data.Containers {
		data.Containers[i].ControlNote = ""
	}
	if ctx.Err() != nil {
		r.unavailable("observation.timeout", "host", "Observation deadline exceeded.")
	}
	return rows, data
}
func containersReport(ctx context.Context) operationReport {
	r := newReport("containers", nil)
	_, data := observeContainerRows(ctx, &r)
	r.Data = map[string]any{"containers": data.Containers}
	return r
}
func projectsReport(ctx context.Context) operationReport {
	result := []inventoryProject{}
	r := newReport("projects", nil)
	cfg, err := loadHostConfig()
	if err != nil {
		r.unavailable("projects.config", "host", "Host configuration is unavailable.")
		r.Data = map[string]any{"projects": result}
		return r
	}
	if _, err := os.Stat(configPath()); err != nil {
		r.unavailable("projects.config", "host", "Host configuration is unavailable.")
	}
	roots, warnings := projectDirectories(cfg)
	if len(warnings) > 0 {
		r.unavailable("projects.discovery", "projects", "Project directory discovery is incomplete.")
	}
	roots = append(roots, cfg.VMProjects...)
	if cfg.Harness != nil && cfg.Harness.Worktrees != "" {
		watched, err := filepath.Glob(expandHome(cfg.Harness.Worktrees))
		if err != nil {
			r.unavailable("projects.harness", "projects", "Harness discovery is unavailable.")
		} else {
			roots = append(roots, watched...)
		}
	}
	rows, registered := observeContainerRows(ctx, &r)
	managed := []container{}
	repos := map[string][]worktree{}
	for _, c := range rows {
		if c.Config.Labels[managedLabel] == "true" {
			managed = append(managed, c)
			if common := c.Config.Labels["devkit.repository"]; common != "" {
				repos[common] = nil
			}
		}
	}
	for _, common := range sortedKeys(repos) {
		trees, err := observationTrees(ctx, common)
		if err != nil {
			r.unavailable("projects.worktrees", common, "Git worktree discovery is unavailable.")
			continue
		}
		repos[common] = trees
		if len(trees) > 0 {
			roots = append(roots, trees[0].Path)
		}
	}
	profile := projectReadProfile{
		git:      func(root string, args ...string) (string, error) { return observationGit(ctx, root, args...) },
		trees:    func(root string) ([]worktree, error) { return observationTrees(ctx, root) },
		checkout: func(tree worktree, cs []container) projectCheckout { return checkoutProjectWith(tree, cs, false) },
		problem: func(code, target string) {
			r.unavailable(code, target, "Project identity or directory evidence is unavailable.")
		},
	}
	// Bound repeated roots from multiple discovery sources before traversing checkouts.
	if len(roots) > 512 {
		roots = roots[:512]
		r.unavailable("projects.limit", "projects", "Project discovery exceeds its directory limit.")
	}
	for _, row := range groupProjectRoots(roots, managed, registered, repos, profile) {
		item := inventoryProject{Name: row.Name, Path: row.Path, Main: inventoryCheckoutView(row.Main), Worktrees: []inventoryCheckout{}}
		for _, tree := range row.Worktrees {
			item.Worktrees = append(item.Worktrees, inventoryCheckoutView(tree))
		}
		sort.Slice(item.Worktrees, func(i, j int) bool { return item.Worktrees[i].Path < item.Worktrees[j].Path })
		result = append(result, item)
	}
	if ctx.Err() != nil {
		r.unavailable("observation.timeout", "host", "Observation deadline exceeded.")
	}
	r.Data = map[string]any{"directory": strings.TrimSpace(cfg.ProjectsDir), "projects": result}
	return r
}
