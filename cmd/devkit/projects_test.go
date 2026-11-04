package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectHost(t *testing.T, root string) {
	t.Helper()
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	if err := writeConfigFile(map[string]any{"projects_dir": root, "prune": map[string]any{"daily": false}}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectDirectoryDiscoversGitAndLaunchesWithoutPreviews(t *testing.T) {
	r := newGitRepo(t)
	linked := filepath.Join(t.TempDir(), "feature")
	r.git("worktree", "add", "-b", "feature", linked)
	projectHost(t, r.temp)
	swap(t, &dockerContainers, withContainers())
	repos := knownRepos(nil)
	if len(repos[r.common]) != 2 {
		t.Fatalf("missing main/linked checkouts: %+v", repos)
	}
	for _, path := range []string{r.root, linked} {
		p, err := planAction(actionRequest{Action: "up", Worktree: path})
		if err != nil || p.dir != path || strings.Join(p.args, " ") != "up" {
			t.Errorf("launch %s: %+v %v", path, p, err)
		}
	}
	for _, path := range []string{t.TempDir(), filepath.Join(r.root, "subdir")} {
		if _, err := planAction(actionRequest{Action: "up", Worktree: path}); err == nil {
			t.Errorf("arbitrary path accepted: %s", path)
		}
	}
	projectHost(t, t.TempDir())
	if _, err := planAction(actionRequest{Action: "up", Worktree: r.root}); err == nil {
		t.Fatal("removed root still authorized")
	}
}

func TestProjectDirectoryFindsStoppedVMRecipes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "vm-only")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "devbox.json"), []byte(`{"project":"vm-only","vms":[{"name":"offline","control":{"start":"true","stop":"true"}}]}`), 0600)
	projectHost(t, parent)
	cfg, err := loadHostConfig()
	if err != nil {
		t.Fatal(err)
	}
	roots, _ := vmProjectRoots(context.Background(), cfg, nil)
	if !containsVMRoot(roots, root) {
		t.Fatalf("stopped recipe not discovered: %v", roots)
	}
	projects, _ := readVMProjects(roots)
	found := false
	for _, p := range projects {
		if p.Project == "vm-only" {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy VM recipe missing")
	}
}

func TestProjectsDirectoryRejectsRelativeConfig(t *testing.T) {
	projectHost(t, "relative/path")
	if _, err := loadHostConfig(); err == nil {
		t.Fatal("relative projects_dir accepted")
	}
}

func TestProjectsAPIShowsUnconfiguredFoldersWithoutDocker(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"plain", "broken", ".hidden"} {
		os.Mkdir(filepath.Join(parent, name), 0700)
	}
	os.WriteFile(filepath.Join(parent, "broken", "devkit.json"), []byte(`{"project":"bad","vms":[{}]}`), 0600)
	os.Symlink(t.TempDir(), filepath.Join(parent, "outside"))
	projectHost(t, parent)
	swap(t, &dockerContainers, func() ([]container, error) { return nil, errors.New("docker offline") })
	handler, err := newHandler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/projects", nil))
	if response.Code != 200 {
		t.Fatalf("projects unavailable: %d %s", response.Code, response.Body.String())
	}
	var data struct {
		Projects []struct {
			Name string
			Main struct {
				Docker bool
				Note   string
			}
		}
		Warnings []string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Projects) != 2 || len(data.Warnings) == 0 {
		t.Fatalf("wrong catalog: %s", response.Body.String())
	}
	for _, p := range data.Projects {
		if p.Main.Docker || p.Main.Note == "" {
			t.Errorf("unconfigured project launchable: %+v", p)
		}
	}
	if strings.Contains(response.Body.String(), "outside") || strings.Contains(response.Body.String(), ".hidden") {
		t.Fatal("followed excluded folders")
	}
}

func TestProjectsTreeUsesGitIdentityAndPreservesCheckoutState(t *testing.T) {
	r := newGitRepo(t)
	linked := filepath.Join(t.TempDir(), "feature")
	r.git("worktree", "add", "-b", "feature", linked)
	os.WriteFile(filepath.Join(linked, "untracked.txt"), []byte("local"), 0600)
	projectHost(t, r.temp)
	cfg, _ := loadConfig(r.root)
	if err := writeConfigFile(map[string]any{"projects_dir": r.temp, "harness": map[string]any{"worktrees": linked}, "vm_projects": []string{r.root}}); err != nil {
		t.Fatal(err)
	}
	web := runningWeb("sample-main")
	web.Config.Labels["devkit.repository"] = r.common
	web.Config.Labels["devkit.worktree"] = r.root
	web.Config.Labels["devkit.mode"] = "live"
	swap(t, &dockerContainers, withContainers(web))
	data, err := projectsOverview()
	if err != nil || len(data.Projects) != 1 {
		t.Fatalf("not deduplicated: %+v %v", data, err)
	}
	row := data.Projects[0]
	if row.Path != r.root || row.Main.Branch != "main" || row.Main.Project != cfg.Project || !row.Main.Docker || len(row.Main.Previews) != 1 {
		t.Fatalf("main: %+v", row)
	}
	if len(row.Worktrees) != 1 || row.Worktrees[0].Path != linked || row.Worktrees[0].Branch != "feature" || row.Worktrees[0].Changes != 1 || !row.Worktrees[0].Docker {
		t.Fatalf("linked: %+v", row.Worktrees)
	}
	// A recipe is revalidated on each launch, even after the catalog was rendered.
	os.WriteFile(filepath.Join(r.root, "devkit.json"), []byte(`{"project":"sample","vms":[{}]}`), 0600)
	if _, err := planAction(actionRequest{Action: "up", Worktree: r.root}); err == nil {
		t.Fatal("invalid recipe launched")
	}
}

func TestProjectsDirectoryLimitsAndHomeExpansion(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("HOME", parent)
	projects := filepath.Join(parent, "Projects")
	os.Mkdir(projects, 0700)
	for i := 0; i < 129; i++ {
		os.Mkdir(filepath.Join(projects, fmt.Sprintf("p-%03d", i)), 0700)
	}
	roots, warnings := projectDirectories(hostConfig{ProjectsDir: "~/Projects"})
	if len(roots) != 128 || len(warnings) != 1 || roots[0] != filepath.Join(projects, "p-000") || roots[127] != filepath.Join(projects, "p-127") {
		t.Fatalf("bound/expansion: %d %v", len(roots), warnings)
	}
	roots, warnings = projectDirectories(hostConfig{ProjectsDir: filepath.Join(parent, "missing")})
	if len(roots) != 0 || len(warnings) != 1 {
		t.Fatalf("missing directory: %v %v", roots, warnings)
	}
}

func TestProjectsKeepDistinctClonesAndUnconfiguredRepositories(t *testing.T) {
	a, b := newGitRepo(t), newGitRepo(t)
	os.Remove(filepath.Join(b.root, "devkit.json"))
	projectHost(t, a.temp)
	if err := writeConfigFile(map[string]any{"projects_dir": a.temp, "vm_projects": []string{b.root}}); err != nil {
		t.Fatal(err)
	}
	swap(t, &dockerContainers, withContainers())
	data, err := projectsOverview()
	if err != nil || len(data.Projects) != 2 {
		t.Fatalf("independent clones merged: %+v %v", data, err)
	}
	for _, p := range data.Projects {
		if p.Path == b.root && (p.Main.Docker || p.Main.Note == "") {
			t.Fatalf("missing recipe enabled: %+v", p)
		}
	}
}

func TestProjectsLaunchUsesComposeDefaultsAndExplicitRoots(t *testing.T) {
	r := newGitRepo(t)
	os.WriteFile(filepath.Join(r.root, "devkit.json"), []byte(`{"project":"sample"}`), 0600)
	os.WriteFile(filepath.Join(r.root, "compose.live.yaml"), []byte("services: {}"), 0600)
	projectHost(t, "")
	if err := writeConfigFile(map[string]any{"vm_projects": []string{r.root}}); err != nil {
		t.Fatal(err)
	}
	swap(t, &dockerContainers, withContainers())
	data, err := projectsOverview()
	if err != nil || len(data.Projects) != 1 || !data.Projects[0].Main.Docker {
		t.Fatalf("default compose not launchable: %+v %v", data, err)
	}
	if _, err := planAction(actionRequest{Action: "up", Worktree: r.root}); err != nil {
		t.Fatalf("visible project cannot launch: %v", err)
	}
	// The CLI rejects Compose paths escaping the worktree; the tree must agree.
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	os.WriteFile(outside, []byte("services: {}"), 0600)
	os.Remove(filepath.Join(r.root, "compose.live.yaml"))
	os.Symlink(outside, filepath.Join(r.root, "compose.live.yaml"))
	if checkoutProject(worktree{Path: r.root}, nil).Docker {
		t.Fatal("escaping Compose file advertised as launchable")
	}
}

func TestIncompleteProjectDiscoveryBlocksVMControls(t *testing.T) {
	root := controlFixture(t)
	for _, kind := range []string{"missing", "limited"} {
		t.Run(kind, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "projects")
			if kind == "limited" {
				os.Mkdir(parent, 0700)
				for i := 0; i < 129; i++ {
					os.Mkdir(filepath.Join(parent, fmt.Sprintf("p-%03d", i)), 0700)
				}
			}
			if err := writeConfigFile(map[string]any{"projects_dir": parent, "vm_projects": []string{root}}); err != nil {
				t.Fatal(err)
			}
			if _, err := vmControlCatalog(context.Background()); err == nil {
				t.Fatal("incomplete directory discovery allowed VM control")
			}
			data := newVMInventory().snapshot(context.Background())
			for _, vm := range data.VMs {
				if len(vm.Actions) > 0 {
					t.Fatalf("incomplete inventory advertised controls: %+v", vm)
				}
			}
		})
	}
}
