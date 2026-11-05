package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// existingCompose associates an existing stack without changing its containers.
// Both labels are required: a Compose project name alone is not ownership proof.
type existingCompose struct {
	Project    string `json:"project"`
	WorkingDir string `json:"working_dir"`
}
type containerProject struct{ Project, Root string }

var composeProjectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

func validateExistingCompose(root string, entries []existingCompose) error {
	if len(entries) > 128 {
		return fail("existing_compose accepts at most 128 registrations.")
	}
	seen := map[string]bool{}
	for _, s := range entries {
		if !composeProjectName.MatchString(s.Project) || s.WorkingDir == "" || filepath.IsAbs(s.WorkingDir) || !within(resolve(filepath.Join(root, s.WorkingDir)), resolve(root)) {
			return fail("existing_compose needs an exact Compose project and a working_dir within this recipe's directory.")
		}
		key := s.Project + "\x00" + resolve(filepath.Join(root, s.WorkingDir))
		if seen[key] {
			return fail("existing_compose contains a duplicate registration.")
		}
		seen[key] = true
	}
	return nil
}

// Read only explicitly discovered roots and their Git worktrees. Do not follow
// container-supplied paths to discover recipes. Any incomplete catalog fails closed.
func existingContainerProjects(rows []container) (map[string]containerProject, error) {
	host, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	roots, warnings := projectDirectories(host)
	if len(warnings) > 0 {
		return nil, fail("Existing-container registration unavailable: project discovery is incomplete.")
	}
	roots = append(roots, host.VMProjects...)
	if host.Harness != nil && host.Harness.Worktrees != "" {
		watched, err := filepath.Glob(expandHome(host.Harness.Worktrees))
		if err != nil {
			return nil, err
		}
		roots = append(roots, watched...)
	}
	// Git supplies linked checkouts; no hooks or runtime commands execute here.
	for _, root := range append([]string{}, roots...) {
		root = resolve(expandHome(root))
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			trees, err := worktreesOf(root)
			if err != nil {
				return nil, fail("Existing-container registration unavailable: cannot inspect Git worktrees.")
			}
			for _, tree := range trees {
				roots = append(roots, tree.Path)
			}
		}
	}
	configs := []projectConfig{}
	seen := map[string]bool{}
	for _, root := range roots {
		root = resolve(expandHome(root))
		if seen[root] {
			continue
		}
		seen[root] = true
		if len(seen) > 256 {
			return nil, fail("Existing-container registration limited to 256 project directories.")
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			return nil, fail("Existing-container registration unavailable: a project directory is missing.")
		}
		if _, err := os.Stat(configFile(root)); os.IsNotExist(err) {
			continue
		}
		cfg, err := loadConfig(root)
		if err != nil {
			return nil, fail("Existing-container registration unavailable: a project recipe is invalid.")
		}
		configs = append(configs, cfg)
	}
	result := map[string]containerProject{}
	for _, c := range rows {
		if c.Config.Labels[managedLabel] == "true" || !fullContainerID.MatchString(c.ID) {
			continue
		}
		project, dir := c.Config.Labels["com.docker.compose.project"], c.Config.Labels["com.docker.compose.project.working_dir"]
		if project == "" || !filepath.IsAbs(dir) {
			continue
		}
		claims := map[string]containerProject{}
		for _, cfg := range configs {
			for _, s := range cfg.ExistingCompose {
				if project == s.Project && resolve(dir) == resolve(filepath.Join(cfg.root, s.WorkingDir)) {
					claims[cfg.root] = containerProject{cfg.Project, cfg.root}
				}
			}
		}
		if len(claims) > 1 {
			return nil, fail("Existing-container registrations conflict; resolve duplicate ownership claims.")
		}
		for _, claim := range claims {
			result[c.ID] = claim
		}
	}
	return result, nil
}

func registeredContainer(ctx context.Context, target string) (container, error) {
	if target == "" || strings.HasPrefix(target, "-") {
		return container{}, fail("Use an exact registered container name or full ID.")
	}
	rows, err := hostContainers(ctx)
	if err != nil {
		return container{}, fail("Cannot read container inventory; check Docker access.")
	}
	owners, err := existingContainerProjects(rows)
	if err != nil {
		return container{}, err
	}
	matches := []container{}
	for _, c := range rows {
		if c.ID == target || strings.TrimPrefix(c.Name, "/") == target {
			matches = append(matches, c)
		}
	}
	if len(matches) != 1 || owners[matches[0].ID].Root == "" {
		return container{}, fail("Container is not uniquely registered by an existing_compose recipe on this host.")
	}
	return matches[0], nil
}
