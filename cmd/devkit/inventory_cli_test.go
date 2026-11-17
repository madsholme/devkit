package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func emptyObservedHost(t *testing.T) {
	t.Helper()
	projectHost(t, t.TempDir())
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return nil, nil })
}
func TestInventoryCLIOutsideCheckout(t *testing.T) {
	emptyObservedHost(t)
	t.Chdir(t.TempDir())
	for _, cmd := range []string{"projects", "containers"} {
		code, r := validationCLI(t, cmd, "--json")
		if code != 0 || r["complete"] != true {
			t.Fatal(cmd, code, r)
		}
	}
}
func TestInventoryCLIPartialDocker(t *testing.T) {
	r := newGitRepo(t)
	projectHost(t, r.temp)
	t.Chdir(t.TempDir())
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return nil, fail("credential-SENTINEL") })
	report := projectsReport(context.Background())
	b, _ := json.Marshal(report)
	if report.Complete || report.exitCode() != 1 || !strings.Contains(string(b), r.root) || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(string(b))
	}
}
func TestInventoryCLIIncompleteRegistration(t *testing.T) {
	parent := t.TempDir()
	projectHost(t, parent)
	for _, name := range []string{"one", "two"} {
		root := filepath.Join(parent, name)
		os.Mkdir(root, 0700)
		os.Symlink(filepath.Join(parent, "shared"), filepath.Join(root, "stack"))
		os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"`+name+`","existing_compose":[{"project":"demo","working_dir":"."}]}`), 0600)
	}
	// Invalid recipe fails the entire registration catalog closed, preserving stop-only observation.
	os.WriteFile(filepath.Join(parent, "two", "devkit.json"), []byte(`{bad`), 0600)
	c := labelled(map[string]string{"com.docker.compose.project": "demo", "com.docker.compose.project.working_dir": filepath.Join(parent, "one")})
	c.ID = strings.Repeat("a", 64)
	c.State.Status = "exited"
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	r := containersReport(context.Background())
	b, _ := json.Marshal(r)
	if r.Complete || strings.Contains(string(b), `"registered":true`) || strings.Contains(string(b), `"start"`) {
		t.Fatal(string(b))
	}
}
func TestInventoryCLIStableSafeProjection(t *testing.T) {
	emptyObservedHost(t)
	a := labelled(map[string]string{"unsafe": "SECRET-SENTINEL"})
	a.ID = strings.Repeat("a", 64)
	a.Name = "/same"
	a.Config.Env = []string{"TOKEN=SECRET-SENTINEL"}
	a.Config.Cmd = []string{"SECRET-SENTINEL"}
	b := a
	b.ID = strings.Repeat("b", 64)
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{b, a}, nil })
	r := containersReport(context.Background())
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "SENTINEL") || strings.Index(string(encoded), a.ID) > strings.Index(string(encoded), b.ID) {
		t.Fatal(string(encoded))
	}
	for _, cmd := range []string{"projects", "containers"} {
		code, report := validationCLI(t, cmd, "--json", "--invalid")
		if code != 2 || report["complete"] != false {
			t.Fatal(code, report)
		}
	}
}
func TestInventoryCLIDeadline(t *testing.T) {
	emptyObservedHost(t)
	swap(t, &observeContainers, func(ctx context.Context) ([]container, error) { <-ctx.Done(); return nil, ctx.Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	r := projectsReport(ctx)
	if r.Complete || r.exitCode() != 1 {
		t.Fatal(r)
	}
}

func TestInventoryCLIChildDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := observationCommand(ctx, "sh", "-c", "exec sleep 20")
	if err == nil || time.Since(started) > time.Second {
		t.Fatal(err, time.Since(started))
	}
}

func TestInventoryCLIConflictingRegistration(t *testing.T) {
	root, c := registeredContainerFixture(t)
	nested := filepath.Join(root, "runtime")
	if err := os.WriteFile(filepath.Join(nested, "devkit.json"), []byte(`{"project":"other","existing_compose":[{"project":"restored-app","working_dir":"."}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(map[string]any{"projects_dir": filepath.Dir(root), "vm_projects": []string{nested}}); err != nil {
		t.Fatal(err)
	}
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{*c}, nil })
	r := containersReport(context.Background())
	b, _ := json.Marshal(r)
	if r.Complete || strings.Contains(string(b), `"registered":true`) || strings.Contains(string(b), `"start"`) {
		t.Fatal(string(b))
	}
}
