package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registeredContainerFixture(t *testing.T) (string, *container) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	projectHost(t, filepath.Dir(root))
	if err := os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"app","existing_compose":[{"project":"restored-app","working_dir":"runtime"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := &container{ID: strings.Repeat("b", 64), Name: "/restored-app-web-1"}
	c.State.Status = "exited"
	c.Config.Labels = map[string]string{"com.docker.compose.project": "restored-app", "com.docker.compose.project.working_dir": filepath.Join(root, "runtime"), "com.docker.compose.service": "web"}
	swap(t, &hostContainers, func(context.Context) ([]container, error) { return []container{*c}, nil })
	swap(t, &dockerContainers, withContainers())
	return root, c
}

func TestExistingComposeRegistrationAndStartPlan(t *testing.T) {
	_, c := registeredContainerFixture(t)
	data, err := containersOverview(context.Background())
	if err != nil || len(data.Containers) != 1 {
		t.Fatalf("inventory: %+v %v", data, err)
	}
	row := data.Containers[0]
	if row.Project != "app" || row.Managed || strings.Join(row.Actions, ",") != "start" {
		t.Fatalf("registration not applied: %+v", row)
	}
	p, err := planAction(actionRequest{Action: "container-start", Container: c.ID})
	if err != nil || strings.Join(p.args, " ") != "start --container "+c.ID {
		t.Fatalf("start plan: %+v %v", p, err)
	}
	projects, err := projectsOverview()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(projects)
	if !strings.Contains(string(encoded), c.ID) {
		t.Fatalf("container absent from project: %s", encoded)
	}
}

func TestExistingComposeRejectsUnsafeSelectors(t *testing.T) {
	root, _ := registeredContainerFixture(t)
	for _, selector := range []string{
		`{"project":"restored-app"}`,
		`{"project":"restored-app","working_dir":"../other"}`,
		`{"project":"restored-app","working_dir":"/tmp"}`,
		`{"project":"*","working_dir":"runtime"}`,
		`{"project":"--all","working_dir":"runtime"}`,
	} {
		os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"app","existing_compose":[`+selector+`]}`), 0600)
		if _, err := loadConfig(root); err == nil {
			t.Errorf("accepted selector %s", selector)
		}
	}
}

func TestExistingComposeWrongDirectoryAndRevocation(t *testing.T) {
	root, c := registeredContainerFixture(t)
	c.Config.Labels["com.docker.compose.project.working_dir"] = t.TempDir()
	if _, err := planAction(actionRequest{Action: "container-start", Container: c.ID}); err == nil {
		t.Fatal("claimed other directory")
	}
	c.Config.Labels["com.docker.compose.project.working_dir"] = filepath.Join(root, "runtime")
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"app"}`), 0600)
	if _, err := planAction(actionRequest{Action: "container-start", Container: c.ID}); err == nil {
		t.Fatal("revoked registration retained authority")
	}
}

func TestExistingComposeStartRetainsIDAndRevalidates(t *testing.T) {
	root, c := registeredContainerFixture(t)
	var calls []string
	swap(t, &containerStartRun, func(ctx context.Context, args []string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded start")
		}
		calls = args
		c.State.Running = true
		c.State.Status = "running"
		return nil
	})
	if code, err := dispatch([]string{"start", "--container", c.ID}); err != nil || code != 0 {
		t.Fatalf("start: %d %v", code, err)
	}
	if strings.Join(calls, " ") != "docker start "+c.ID {
		t.Fatal(calls)
	}
	calls = nil
	if err := controlContainer("start", c.ID); err != nil || len(calls) != 0 {
		t.Fatalf("idempotent start: %v %v", err, calls)
	}
	c.State.Running = false
	c.State.Status = "exited"
	n := 0
	swap(t, &hostContainers, func(context.Context) ([]container, error) {
		n++
		if n == 2 {
			os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"app"}`), 0600)
		}
		return []container{*c}, nil
	})
	if err := controlContainer("start", c.ID); err == nil || len(calls) != 0 {
		t.Fatalf("revoked ownership reached Docker: %v %v", err, calls)
	}
}

func TestExistingComposeAmbiguityAndIncompleteDiscovery(t *testing.T) {
	root, c := registeredContainerFixture(t)
	other := filepath.Join(filepath.Dir(root), "other")
	os.Mkdir(other, 0700)
	// Two recipes may share a directory only via an explicit root registration;
	// containment prevents escaping to another clone even with symlinks.
	os.Symlink(filepath.Join(root, "runtime"), filepath.Join(other, "runtime"))
	os.WriteFile(filepath.Join(other, "devkit.json"), []byte(`{"project":"other","existing_compose":[{"project":"restored-app","working_dir":"runtime"}]}`), 0600)
	if _, err := loadConfig(other); err == nil {
		t.Fatal("symlink escaped project root")
	}
	if _, err := registeredContainer(context.Background(), c.ID); err == nil {
		t.Fatal("invalid catalog granted authority")
	}
	os.Remove(filepath.Join(other, "devkit.json"))
	if err := writeConfigFile(map[string]any{"projects_dir": filepath.Join(root, "missing")}); err != nil {
		t.Fatal(err)
	}
	if _, err := registeredContainer(context.Background(), c.ID); err == nil {
		t.Fatal("incomplete catalog granted authority")
	}
}

func TestExistingComposeDetailsAuthorityAndMasking(t *testing.T) {
	root, c := registeredContainerFixture(t)
	c.Config.Env = []string{"PASSWORD=do-not-show", "MODE=test"}
	h, err := newHandler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		return r
	}
	r := get("/api/container?container=" + c.ID)
	if r.Code != 200 || strings.Contains(r.Body.String(), "do-not-show") || !strings.Contains(r.Body.String(), "test") {
		t.Fatalf("details: %d %s", r.Code, r.Body.String())
	}
	for _, target := range []string{c.ID[:12], "missing", strings.Repeat("a", 64)} {
		for _, endpoint := range []string{"container", "logs"} {
			if r := get("/api/" + endpoint + "?container=" + target); r.Code == 200 {
				t.Fatal("unauthorized read", endpoint, target)
			}
		}
	}
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"app"}`), 0600)
	if r := get("/api/container?container=" + c.ID); r.Code == 200 {
		t.Fatal("revoked details still available")
	}
}

func TestExistingComposeStartGuards(t *testing.T) {
	_, c := registeredContainerFixture(t)
	for _, state := range []string{"paused", "restarting", "removing", "dead", "unknown"} {
		c.State.Status = state
		if _, err := planAction(actionRequest{Action: "container-start", Container: c.ID}); err == nil {
			t.Fatal("unsafe start", state)
		}
	}
	c.State.Status = "exited"
	c.HostConfig.AutoRemove = true
	if _, err := planAction(actionRequest{Action: "container-start", Container: c.ID}); err == nil {
		t.Fatal("auto-remove start")
	}
	c.HostConfig.AutoRemove = false
	for _, target := range []string{c.Name, c.ID[:12], "--all"} {
		if _, err := planAction(actionRequest{Action: "container-start", Container: target}); err == nil {
			t.Fatal("non-full-ID dashboard start", target)
		}
	}
}

func TestExistingComposeConflictingClaims(t *testing.T) {
	root, c := registeredContainerFixture(t)
	nested := filepath.Join(root, "runtime")
	os.WriteFile(filepath.Join(nested, "devkit.json"), []byte(`{"project":"other","existing_compose":[{"project":"restored-app","working_dir":"."}]}`), 0600)
	if err := writeConfigFile(map[string]any{"projects_dir": filepath.Dir(root), "vm_projects": []string{nested}}); err != nil {
		t.Fatal(err)
	}
	if _, err := registeredContainer(context.Background(), c.ID); err == nil {
		t.Fatal("conflicting roots granted authority")
	}
	data, err := containersOverview(context.Background())
	if err != nil || len(data.Warnings) == 0 || data.Containers[0].Registered {
		t.Fatalf("conflict not surfaced: %+v %v", data, err)
	}
}
