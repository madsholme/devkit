package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMSelectorsValidateWithProjectConfig(t *testing.T) {
	root := t.TempDir()
	for _, selectors := range []string{`[{}]`, `[{"name":"one","uuid":"11111111-1111-1111-1111-111111111111"}]`, `[{"uuid":"bad"}]`, `[{"name":" "}]`, `[{"name":"one"},{"name":"one"}]`} {
		os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"demo","vms":`+selectors+`}`), 0600)
		if _, err := loadConfig(root); err == nil {
			t.Fatalf("invalid selectors accepted: %s", selectors)
		}
	}
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"demo","vms":[{"name":"worker"}]}`), 0600)
	cfg, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.VMs) != 1 || cfg.VMs[0].Name != "worker" {
		t.Fatalf("mapping missing: %+v", cfg)
	}
}

func TestVMProjectsExactMatchesAndUUIDPrecedence(t *testing.T) {
	uuid := "aaaaaaaa-1111-1111-1111-111111111111"
	rows := []virtualMachine{{Name: "worker", UUID: uuid}, {Name: "desktop"}, {Name: "worker-extra"}}
	projects := []projectConfig{{Project: "swiftconfig", VMs: []vmSelector{{UUID: strings.ToUpper(uuid)}}}, {Project: "playwarden", VMs: []vmSelector{{Name: "desktop"}, {Name: "worker"}}}}
	assignVMProjects(rows, projects)
	if rows[0].Project != "swiftconfig" || rows[1].Project != "playwarden" || rows[2].Project != "" {
		t.Fatalf("exact project matching: %+v", rows)
	}
}

func TestVMProjectAmbiguitiesStayUnassigned(t *testing.T) {
	uuid := "aaaaaaaa-1111-1111-1111-111111111111"
	rows := []virtualMachine{{Name: "duplicate"}, {Name: "duplicate"}, {Name: "conflict", UUID: uuid}, {Name: "same-project"}}
	projects := []projectConfig{
		{Project: "alpha", VMs: []vmSelector{{Name: "duplicate"}, {UUID: uuid}, {Name: "same-project"}}},
		{Project: "beta", VMs: []vmSelector{{UUID: uuid}}},
		{Project: "alpha", VMs: []vmSelector{{Name: "same-project"}}},
	}
	assignVMProjects(rows, projects)
	for _, i := range []int{0, 1, 2} {
		if rows[i].Project != "" || rows[i].ProjectNote == "" {
			t.Fatalf("ambiguity hidden: %+v", rows[i])
		}
	}
	if rows[3].Project != "alpha" || rows[3].ProjectNote != "" {
		t.Fatalf("same project worktrees conflict: %+v", rows[3])
	}
	rows = []virtualMachine{{Name: "one", UUID: uuid}, {Name: "two", UUID: uuid}}
	assignVMProjects(rows, projects[:1])
	for _, v := range rows {
		if v.Project != "" || v.ProjectNote == "" {
			t.Fatalf("duplicate UUID claimed: %+v", v)
		}
	}
}

func TestVMProjectRecipeDiscovery(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	sub := filepath.Join(one, "vms", "work")
	os.MkdirAll(sub, 0700)
	os.MkdirAll(two, 0700)
	os.WriteFile(filepath.Join(one, "devbox.json"), []byte(`{"project":"legacy","vms":[{"name":"old"}]}`), 0600)
	os.WriteFile(filepath.Join(two, "devkit.json"), []byte(`{"project":"vm-only","vms":[{"name":"new"}]}`), 0600)
	projects, warnings := readVMProjects([]string{sub, one, two})
	if len(projects) != 2 || len(warnings) != 0 {
		t.Fatalf("discovery: %+v %v", projects, warnings)
	}
	rows := []virtualMachine{{Name: "old"}, {Name: "new"}}
	assignVMProjects(rows, projects)
	if rows[0].Project != "legacy" || rows[1].Project != "vm-only" {
		t.Fatalf("recipes: %+v", rows)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), root) {
		t.Fatalf("path leak: %s", encoded)
	}
	os.WriteFile(filepath.Join(two, "devkit.json"), []byte(`{"project":"vm-only","vms":[{}]}`), 0600)
	projects, warnings = readVMProjects([]string{one, two})
	if len(projects) != 1 || len(warnings) != 1 {
		t.Fatalf("invalid recipe swallowed: %+v %v", projects, warnings)
	}
	if strings.Contains(strings.Join(warnings, " "), root) {
		t.Fatal("warnings expose discovery paths")
	}
}

func TestVMProjectRootsSurviveDockerFailure(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.Mkdir(bin, 0700)
	os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0700)
	t.Setenv("PATH", bin)
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded docker probe")
		}
		return "PRIVATE", errors.New("failed")
	})
	cfg := hostConfig{VMProjects: []string{root}}
	roots, warnings := vmProjectRoots(context.Background(), cfg, []virtualMachine{{cwd: filepath.Join(root, "guest")}})
	if !containsVMRoot(roots, root) || !containsVMRoot(roots, filepath.Join(root, "guest")) || len(warnings) != 1 {
		t.Fatalf("roots: %v %v", roots, warnings)
	}
}
func containsVMRoot(roots []string, expected string) bool {
	for _, r := range roots {
		if r == expected {
			return true
		}
	}
	return false
}

func TestVMProjectSourcesAndGroupOrdering(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	tree := filepath.Join(root, "harness", "task")
	os.MkdirAll(bin, 0700)
	os.MkdirAll(tree, 0700)
	os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	t.Setenv("PATH", bin)
	dockerRoot := filepath.Join(root, "docker-project")
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") != `docker ps -a --filter label=devkit.managed=true --format {{.Label "devkit.worktree"}}` {
			t.Fatalf("unexpected probe: %v", args)
		}
		return dockerRoot + "\n", nil
	})
	cfg := hostConfig{Harness: &harnessConfig{Worktrees: filepath.Join(root, "harness", "*")}}
	roots, w := vmProjectRoots(context.Background(), cfg, nil)
	if len(w) != 0 || !containsVMRoot(roots, tree) || !containsVMRoot(roots, dockerRoot) {
		t.Fatalf("sources: %v %v", roots, w)
	}
	rows := []virtualMachine{{Project: "", Name: "first"}, {Project: "beta", Name: "b"}, {Project: "alpha", Name: "z"}, {Project: "alpha", Name: "a"}}
	groupVMProjects(rows)
	if rows[0].Name != "a" || rows[1].Name != "z" || rows[2].Project != "beta" || rows[3].Project != "" {
		t.Fatalf("groups: %+v", rows)
	}
}

func TestVMProjectsHostRootsValidate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEVKIT_CONFIG_DIR", root)
	os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"vm_projects":["relative/path"]}`), 0600)
	if _, err := loadHostConfig(); err == nil {
		t.Fatal("relative project root accepted")
	}
}
