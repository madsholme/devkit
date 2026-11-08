package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// projectDirectories is deliberately shallow. Symlink children do not implicitly
// authorize unrelated paths; Git still supplies explicitly linked worktrees.
func projectDirectories(cfg hostConfig) ([]string, []string) {
	roots, warnings := []string{}, []string{}
	if cfg.ProjectsDir == "" {
		return roots, warnings
	}
	parent := resolve(expandHome(cfg.ProjectsDir))
	entries, err := os.ReadDir(parent)
	if err != nil {
		return roots, append(warnings, "Projects directory is unavailable; check projects_dir in the host configuration.")
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if len(roots) == 128 {
			warnings = append(warnings, "Project discovery is limited to 128 folders.")
			break
		}
		roots = append(roots, filepath.Join(parent, entry.Name()))
	}
	return roots, warnings
}

type projectPreview struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
	URL    string `json:"url"`
}
type projectCheckout struct {
	WorktreeKey string           `json:"worktree_key,omitempty"`
	Containers  []containerRow   `json:"containers,omitempty"`
	Path        string           `json:"path"`
	Branch      string           `json:"branch"`
	Project     string           `json:"project"`
	Git         bool             `json:"git"`
	GitControls gitControls      `json:"git_controls"`
	Docker      bool             `json:"docker"`
	Note        string           `json:"note"`
	Previews    []projectPreview `json:"previews"`
	worktreeInfo
}

type projectRow struct {
	Name      string            `json:"name"`
	Path      string            `json:"path"`
	Main      projectCheckout   `json:"main"`
	Worktrees []projectCheckout `json:"worktrees"`
}
type projectsData struct {
	Directory string       `json:"directory"`
	Projects  []projectRow `json:"projects"`
	Warnings  []string     `json:"warnings"`
}

// checkoutProject exposes capabilities, never recipe commands or arbitrary config.
func checkoutProject(tree worktree, containers []container) projectCheckout {
	row := projectCheckout{Path: tree.Path, Branch: tree.Branch, Previews: []projectPreview{}}
	if _, err := os.Stat(filepath.Join(tree.Path, ".git")); err == nil {
		row.Git = true
		row.worktreeInfo = worktreeState(tree.Path)
		row.GitControls = checkoutGitControls(tree.Path)
	}
	cfg, err := loadConfig(tree.Path)
	switch {
	case err != nil:
		if _, statErr := os.Stat(configFile(tree.Path)); os.IsNotExist(statErr) {
			row.Note = "Setup needed: add a devkit.json recipe."
		} else {
			row.Note = "Recipe unavailable or invalid; check devkit.json."
		}
	default:
		row.Project = cfg.Project
		row.WorktreeKey = digest(cfg.root)
		compose := resolve(filepath.Join(tree.Path, orDefault(cfg.Compose["live"], "compose.live.yaml")))
		row.Docker = row.Git && within(compose, resolve(tree.Path)) && isFile(compose)
		if !row.Git && len(cfg.Compose) > 0 {
			row.Note = "Container previews need a Git checkout."
		} else if !row.Docker && len(cfg.VMs) == 0 && len(cfg.ExistingCompose) == 0 {
			row.Note = "Setup needed: configure a live Compose file or VMs."
		}
	}
	byName := map[string]projectPreview{}
	for _, c := range containers {
		labels := c.Config.Labels
		if labels["devkit.worktree"] != tree.Path || labels["devkit.preview"] == "" {
			continue
		}
		name := labels["devkit.preview"]
		if _, exists := byName[name]; !exists {
			byName[name] = projectPreview{Name: name, Mode: labels["devkit.mode"], Status: "missing", URL: labels["devkit.url"]}
		}
		if isWeb(labels) {
			byName[name] = projectPreview{Name: name, Mode: labels["devkit.mode"], Status: containerStatus(c.State), URL: labels["devkit.url"]}
		}
	}
	for _, name := range sortedKeys(byName) {
		row.Previews = append(row.Previews, byName[name])
	}
	return row
}

// projectsOverview keeps the catalog available when Docker is down. VM metadata
// continues to come from the independent cached /api/vms inventory.
func projectsOverview() (projectsData, error) {
	data := projectsData{Projects: []projectRow{}, Warnings: []string{}}
	cfg, err := loadHostConfig()
	if err != nil {
		return data, err
	}
	data.Directory = cfg.ProjectsDir
	roots, warnings := projectDirectories(cfg)
	data.Warnings = append(data.Warnings, warnings...)
	containers, err := dockerContainers()
	if err != nil {
		containers = nil
		data.Warnings = append(data.Warnings, "Docker inventory unavailable; container status and launches may be unavailable.")
	}
	registered, registrationErr := containersOverview(context.Background())
	if registrationErr != nil {
		data.Warnings = append(data.Warnings, "Existing container inventory unavailable.")
	}
	data.Warnings = append(data.Warnings, registered.Warnings...)
	attach := func(row projectCheckout) projectCheckout {
		for _, c := range registered.Containers {
			if c.Registered && c.Worktree == row.Path {
				row.Containers = append(row.Containers, c)
			}
		}
		return row
	}
	repos := knownRepos(containers)
	roots = append(roots, cfg.VMProjects...)
	for _, common := range sortedKeys(repos) {
		if len(repos[common]) > 0 {
			roots = append(roots, repos[common][0].Path)
		}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		root = resolve(expandHome(root))
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		key := root
		trees := []worktree{}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			if common, err := git(root, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
				key = common
				if known, ok := repos[common]; ok {
					trees = known
				} else {
					trees, _ = worktreesOf(common)
				}
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		main := worktree{Path: root}
		if len(trees) > 0 {
			main = trees[0]
			trees = trees[1:]
		}
		row := projectRow{Name: filepath.Base(main.Path), Path: main.Path, Main: attach(checkoutProject(main, containers)), Worktrees: []projectCheckout{}}
		for _, tree := range trees {
			row.Worktrees = append(row.Worktrees, attach(checkoutProject(tree, containers)))
		}
		data.Projects = append(data.Projects, row)
	}
	sort.Slice(data.Projects, func(i, j int) bool {
		a, b := data.Projects[i], data.Projects[j]
		if a.Name == b.Name {
			return a.Path < b.Path
		}
		return a.Name < b.Name
	})
	return data, nil
}
