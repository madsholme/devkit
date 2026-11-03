package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// vmSelector is an explicit project claim; identifying a VM never grants access to it.
type vmSelector struct {
	Name    string     `json:"name,omitempty"`
	Control *vmControl `json:"control,omitempty"`
	UUID    string     `json:"uuid,omitempty"`
}

func validateVMSelectors(selectors []vmSelector) error {
	if len(selectors) > 128 {
		return fail("devkit.json vms accepts at most 128 mappings.")
	}
	seen := map[string]bool{}
	for _, s := range selectors {
		if (s.Name == "") == (s.UUID == "") {
			return fail("devkit.json each vms entry needs exactly one name or uuid.")
		}
		key := "uuid:" + strings.ToLower(s.UUID)
		if s.Name != "" {
			if len(s.Name) > 255 || strings.TrimSpace(s.Name) != s.Name || strings.IndexFunc(s.Name, unicode.IsControl) >= 0 {
				return fail("devkit.json vms name must be 1–255 characters without outer whitespace or control characters.")
			}
			key = "name:" + s.Name
		} else if !vmUUID.MatchString(s.UUID) {
			return fail("devkit.json vms uuid must be a valid VM UUID.")
		}
		if seen[key] {
			return fail("devkit.json vms contains a duplicate selector.")
		}
		if err := s.Control.validate(s); err != nil {
			return err
		}
		seen[key] = true
	}
	return nil
}

// Reuse Docker's devkit.worktree labels and the harness worktree glob, without
// running Git scans or requiring Docker for VM-only projects. Never read environments.
func vmProjectRoots(ctx context.Context, cfg hostConfig, rows []virtualMachine) ([]string, []string) {
	roots := append([]string{}, cfg.VMProjects...)
	warnings := []string{}
	for _, root := range cfg.VMProjects {
		if info, err := os.Stat(expandHome(root)); err != nil || !info.IsDir() {
			warnings = append(warnings, "A vm_projects directory is unavailable; check the host configuration.")
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	for _, vm := range rows {
		if vm.cwd != "" {
			roots = append(roots, vm.cwd)
		}
	}
	if cfg.Harness != nil && cfg.Harness.Worktrees != "" {
		trees, _ := filepath.Glob(expandHome(cfg.Harness.Worktrees))
		roots = append(roots, trees...)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		out, err := vmProbe(probeCtx, "docker", "ps", "-a", "--filter", "label="+managedLabel+"=true", "--format", `{{.Label "devkit.worktree"}}`)
		if err != nil {
			warnings = append(warnings, "Docker project discovery unavailable; VM mappings from other sources still apply. Set vm_projects for VM-only projects.")
		} else {
			for _, line := range strings.Split(out, "\n") {
				if path := strings.TrimSpace(line); filepath.IsAbs(path) {
					roots = append(roots, path)
				}
			}
		}
	}
	return roots, warnings
}

// The closest recipe wins for a nested launch directory. A recipe discovers
// selectors only: proximity itself never assigns VM ownership.
func readVMProjects(roots []string) ([]projectConfig, []string) {
	projects := []projectConfig{}
	warnings := []string{}
	seen := map[string]bool{}
	for _, root := range roots {
		root = expandHome(root)
		if !filepath.IsAbs(root) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		for dir := resolved; ; dir = filepath.Dir(dir) {
			path := configFile(dir)
			if _, err := os.Stat(path); err == nil {
				if seen[path] {
					break
				}
				if len(seen) >= 128 {
					return projects, append(warnings, "VM project discovery limited to 128 recipes; inventory may be unassigned.")
				}
				seen[path] = true
				cfg, err := loadConfig(dir)
				if err != nil {
					warnings = append(warnings, "A VM project recipe is invalid or unreadable; check devkit.json/devbox.json and its vms selectors.")
				} else if len(cfg.VMs) > 0 {
					projects = append(projects, cfg)
				}
				break
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	return projects, warnings
}

func assignVMProjects(rows []virtualMachine, projects []projectConfig) {
	names, uuids := map[string]int{}, map[string]int{}
	byName, byUUID := map[string]map[string]bool{}, map[string]map[string]bool{}
	add := func(index map[string]map[string]bool, key, project string) {
		if index[key] == nil {
			index[key] = map[string]bool{}
		}
		index[key][project] = true
	}
	for _, vm := range rows {
		names[vm.Name]++
		if vm.UUID != "" {
			uuids[strings.ToLower(vm.UUID)]++
		}
	}
	for _, cfg := range projects {
		for _, s := range cfg.VMs {
			if s.UUID != "" {
				add(byUUID, strings.ToLower(s.UUID), cfg.Project)
			} else {
				add(byName, s.Name, cfg.Project)
			}
		}
	}
	for i := range rows {
		vm := &rows[i]
		vm.Project = ""
		vm.ProjectNote = ""
		claims := byUUID[strings.ToLower(vm.UUID)]
		count := uuids[strings.ToLower(vm.UUID)]
		if len(claims) == 0 {
			claims = byName[vm.Name]
			count = names[vm.Name]
		}
		if len(claims) == 0 {
			continue
		}
		if count > 1 {
			vm.ProjectNote = "Project mapping is ambiguous: multiple active VMs share this identifier. Use a unique VM UUID or name."
			continue
		}
		if len(claims) > 1 {
			vm.ProjectNote = "Project mapping conflicts: multiple projects claim this VM. Resolve their vms entries."
			continue
		}
		for project := range claims {
			vm.Project = project
		}
	}
}

func groupVMProjects(rows []virtualMachine) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Project != b.Project {
			if a.Project == "" {
				return false
			}
			if b.Project == "" {
				return true
			}
			return a.Project < b.Project
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
}

func discoverVMProjects(ctx context.Context, cfg hostConfig, rows []virtualMachine) []string {
	if len(rows) == 0 {
		return nil
	}
	roots, warnings := vmProjectRoots(ctx, cfg, rows)
	projects, problems := readVMProjects(roots)
	assignVMProjects(rows, projects)
	groupVMProjects(rows)
	return append(warnings, problems...)
}
