package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var kit, _ = filepath.Abs("../..")

// swap replaces a package variable for the duration of a test.
func swap[T any](t *testing.T, target *T, value T) {
	t.Helper()
	old := *target
	*target = value
	t.Cleanup(func() { *target = old })
}

func noOwnerCheck(string, string) error { return nil }

func withContainers(list ...container) func() ([]container, error) {
	return func() ([]container, error) { return list, nil }
}

func labelled(labels map[string]string) container {
	var c container
	c.Config.Labels = labels
	return c
}

const sampleCommit = "3052f1ef86b4ab10022727676e67097c1f22906e"

func TestReadableNames(t *testing.T) {
	cases := []struct{ mode, branch, identity, want string }{
		{"live", "fix/login", "fix/login", "shop-fix-login"},
		{"live", "t3code/halloween-theme", "t3code/halloween-theme", "shop-t3code-halloween-theme"},
		{"review", "v2", sampleCommit, "shop-v2-3052f1e"},
		{"review", "", sampleCommit, "shop-3052f1e"},
		{"live", "", "detached-abc", "shop-detached-abc"},
	}
	for _, c := range cases {
		if got := previewName("shop", c.mode, c.branch, sampleCommit, c.identity); got != c.want {
			t.Errorf("previewName(%q, %q) = %q, want %q", c.mode, c.branch, got, c.want)
		}
	}
	valid := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	for _, mode := range []string{"live", "review"} {
		for _, branch := range []string{strings.Repeat("x", 400), "🍋", "Fix/Login"} {
			name := previewName(strings.Repeat("shop", 20), mode, branch, sampleCommit, branch)
			if len(name)+clash > 63 || !valid.MatchString(name) {
				t.Errorf("bad name %q", name)
			}
		}
	}
}

func TestClashingNamesGetADigest(t *testing.T) {
	swap(t, &savedPreviews, func(string) (map[string]map[string]string, error) { return nil, nil })
	swap(t, &dockerContainers, withContainers(labelled(map[string]string{"devbox.preview": "shop-fix-login"})))
	a, _ := newPreviewName("/r/.git", "shop", "live", "fix-login", sampleCommit, "fix-login")
	b, _ := newPreviewName("/r/.git", "shop", "live", "fix/login", sampleCommit, "fix/login")
	free, _ := newPreviewName("/r/.git", "shop", "live", "other", sampleCommit, "other")
	withDigest := regexp.MustCompile(`^shop-fix-login-[0-9a-f]{4}$`)
	if !withDigest.MatchString(a) || !withDigest.MatchString(b) || a == b {
		t.Errorf("clash names %q %q", a, b)
	}
	if free != "shop-other" {
		t.Errorf("free name %q", free)
	}
	if n := clashName(strings.Repeat("x", 58), "/r/.git", "i", "live"); len(n) > 63 {
		t.Errorf("clash name too long: %q", n)
	}
}

func TestExistingPreviewsAreFoundByIdentityOrLegacyName(t *testing.T) {
	legacy := legacyName("shop", "v2", "live")
	saved := map[string]map[string]string{
		legacy:         {"devbox.mode": "live", "devbox.project": "shop"},
		"shop-3052f1e": {"devbox.identity": "3052f1e", "devbox.mode": "review", "devbox.project": "shop"},
	}
	if got := findSaved(saved, "shop", "live", "v2"); got != legacy {
		t.Errorf("legacy lookup = %q", got)
	}
	if got := findSaved(saved, "shop", "review", "3052f1e"); got != "shop-3052f1e" {
		t.Errorf("identity lookup = %q", got)
	}
	if got := findSaved(saved, "shop", "live", "main"); got != "" {
		t.Errorf("unexpected match %q", got)
	}
}

func TestLegacyNamesMatchThePythonScheme(t *testing.T) {
	// Existing previews on the host were named by the Python version; they must still resolve.
	if got := legacyName("demo", "main", "live"); got != "demo-main-live-9f7f4fac13" {
		t.Errorf("legacyName = %q", got)
	}
	if got := legacyName("qrbase", "v2", "live"); got != "qrbase-v2-live-1d919ae20a" {
		t.Errorf("legacyName = %q", got)
	}
}

func TestHostnameRejectsConfigInjection(t *testing.T) {
	for _, value := range []string{"", "*.dev.example.com", "dev.example.com`)", "a..com"} {
		t.Setenv("DEV_DOMAIN", value)
		if _, err := domain(); err == nil {
			t.Errorf("domain accepted %q", value)
		}
	}
}

func TestT3FilesUseKnownFieldsAndIcons(t *testing.T) {
	// T3 silently ignores the whole t3.json when any field is invalid.
	icons := map[string]bool{"play": true, "test": true, "lint": true, "configure": true, "build": true, "debug": true}
	top := map[string]bool{"$schema": true, "iconPath": true, "defaultThreadEnvMode": true, "scripts": true}
	fields := map[string]bool{"name": true, "command": true, "icon": true, "runOnWorktreeCreate": true,
		"previewUrl": true, "autoOpenPreview": true}
	for _, path := range []string{filepath.Join(kit, "t3.json"), filepath.Join(kit, "project-template", "t3.json")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var file map[string]json.RawMessage
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatal(err)
		}
		for key := range file {
			if !top[key] {
				t.Errorf("%s: unknown field %q", path, key)
			}
		}
		var icon struct{ IconPath string }
		json.Unmarshal(data, &icon)
		if icon.IconPath != "" && !isFile(filepath.Join(filepath.Dir(path), icon.IconPath)) {
			t.Errorf("%s: iconPath %q missing", path, icon.IconPath)
		}
		var scripts struct{ Scripts []map[string]any }
		json.Unmarshal(data, &scripts)
		for _, script := range scripts.Scripts {
			for key := range script {
				if !fields[key] {
					t.Errorf("%s: unknown script field %q", path, key)
				}
			}
			name, _ := script["name"].(string)
			command, _ := script["command"].(string)
			if strings.TrimSpace(name) == "" || strings.TrimSpace(command) == "" {
				t.Errorf("%s: script needs name and command", path)
			}
			if i, ok := script["icon"].(string); ok && !icons[i] {
				t.Errorf("%s: unknown icon %q", path, i)
			}
		}
	}
}

func composeEnv() []string {
	return withEnv(os.Environ(), map[string]string{"DEVBOX_PREVIEW": "demo-review-123",
		"DEVBOX_HOST": "demo-review-123.dev.example.com", "DEVBOX_IMAGE": "devbox/demo:abc", "DEVBOX_COMMIT": "abc"})
}

// fakeImage answers docker image inspect and passes every other command to the real runner.
func fakeImage(t *testing.T, config string) {
	real := runCommand
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		if len(args) >= 3 && args[0] == "docker" && args[1] == "image" && args[2] == "inspect" {
			return `[{"Id": "sha256:fixed-image"}]`, nil
		}
		if config != "" {
			return config, nil
		}
		return real(args, o)
	})
}

func requireCompose(t *testing.T) {
	if exec.Command("docker", "compose", "version").Run() != nil {
		t.Skip("docker compose is not available")
	}
}

func templateConfig(t *testing.T) (string, projectConfig) {
	root := filepath.Join(kit, "project-template")
	cfg, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func TestRealComposeTemplateAndPinnedWebImage(t *testing.T) {
	requireCompose(t)
	fakeImage(t, "")
	root, cfg := templateConfig(t)
	output, err := render(root, cfg, "review", composeEnv(), map[string]string{managedLabel: "true"})
	if err != nil {
		t.Fatal(err)
	}
	web := object(object(output, "services"), "web")
	if web["image"] != "sha256:fixed-image" || web["pull_policy"] != "never" {
		t.Errorf("web image not pinned: %v %v", web["image"], web["pull_policy"])
	}
	volumes, _ := web["volumes"].([]any)
	for _, v := range volumes {
		if str(asObject(v), "type") == "bind" {
			t.Error("review web service has a bind mount")
		}
	}
	if name := str(object(object(output, "volumes"), "data"), "name"); name != "demo-review-123_data" {
		t.Errorf("volume name %q", name)
	}
	if n := str(object(web, "labels"), "traefik.docker.network"); n != "devbox-proxy" {
		t.Errorf("traefik network %q", n)
	}
}

func TestLiveTemplateResolvesAbsoluteSourceMount(t *testing.T) {
	requireCompose(t)
	root, cfg := templateConfig(t)
	output, err := render(root, cfg, "live", composeEnv(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	web := object(object(output, "services"), "web")
	volumes, _ := web["volumes"].([]any)
	source := ""
	for _, v := range volumes {
		if str(asObject(v), "type") == "bind" {
			source = str(asObject(v), "source")
			break
		}
	}
	if source != root {
		t.Errorf("bind source %q, want %q", source, root)
	}
	if web["restart"] != "unless-stopped" {
		t.Errorf("restart %v", web["restart"])
	}
}

func TestReviewHasNoRestartPolicy(t *testing.T) {
	fakeImage(t, `{"services": {"web": {}}}`)
	output, err := render(filepath.Join(kit, "project-template"), projectConfig{Project: "demo"}, "review", composeEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := object(object(output, "services"), "web")["restart"]; has {
		t.Error("review web service has a restart policy")
	}
}

func TestSavedLivePreviewGainsRestartPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.json")
	os.WriteFile(path, []byte(`{"services": {
		"web": {"labels": {"devbox.mode": "live"}},
		"seed": {"labels": {"devbox.mode": "live"}, "restart": "no"}}}`), 0o600)
	swap(t, &composeRun, func(string, ...string) error { return nil })
	if err := up(path); err != nil {
		t.Fatal(err)
	}
	saved, _ := readCompose(path)
	services := object(saved, "services")
	if object(services, "web")["restart"] != "unless-stopped" || object(services, "seed")["restart"] != "no" {
		t.Errorf("restart policies: %v", services)
	}
}

func TestRefuseSourceMountsInReviewAndSharedData(t *testing.T) {
	for _, config := range []string{
		`{"services": {"web": {"volumes": [{"type": "bind", "source": "/tmp"}]}}}`,
		`{"services": {"web": {"ports": [{"published": "8000"}]}}}`,
		`{"services": {"web": {}}, "volumes": {"data": {"name": "shared-data"}}}`,
		`{"services": {"web": {}}, "networks": {"db": {"external": true}}}`,
	} {
		fakeImage(t, config)
		_, err := render(filepath.Join(kit, "project-template"), projectConfig{Project: "demo"}, "review", composeEnv(), nil)
		var user userError
		if !errors.As(err, &user) {
			t.Errorf("accepted %s (err %v)", config, err)
		}
	}
}

func TestStopPreservesAndDestroyRemovesVolumes(t *testing.T) {
	common := t.TempDir()
	path := filepath.Join(common, "devbox/runs/demo-live-123/compose.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{}"), 0o600)
	swap(t, &repository, func() (string, string, error) { return common, common, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &unshareIfShared, func(string) error { return nil })
	var calls [][]string
	swap(t, &composeRun, func(p string, args ...string) error {
		calls = append(calls, append([]string{p}, args...))
		return nil
	})
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	if err := lifecycle("stop", "demo-live-123"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls[len(calls)-1], " "); got != path+" stop" || !isFile(path) {
		t.Errorf("stop: %q, file kept %v", got, isFile(path))
	}
	if err := lifecycle("destroy", "demo-live-123"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls[len(calls)-1], " "); got != path+" down --volumes --remove-orphans" || isFile(path) {
		t.Errorf("destroy: %q, file kept %v", got, isFile(path))
	}
}

func TestNoArgumentResolvesCurrentWorktree(t *testing.T) {
	temp := t.TempDir()
	root, other, common := filepath.Join(temp, "wt"), filepath.Join(temp, "other"), filepath.Join(temp, ".git")
	saved := map[string]map[string]string{
		"demo-a-live-1":     {"devbox.mode": "live", "devbox.worktree": root},
		"demo-b-live-2":     {"devbox.mode": "live", "devbox.worktree": other},
		"demo-old-live-3":   {"devbox.mode": "live", "devbox.branch": "old"},
		"demo-abc-review-4": {"devbox.mode": "review"},
	}
	trees := []worktree{{root, "a"}, {other, "b"}}
	swap(t, &savedPreviews, func(string) (map[string]map[string]string, error) { return saved, nil })
	swap(t, &worktreesOf, func(string) ([]worktree, error) { return trees, nil })
	if got, err := currentPreview(root, common); got != "demo-a-live-1" {
		t.Errorf("current = %q, %v", got, err)
	}
	saved["demo-abc-review-5"] = map[string]string{"devbox.mode": "review", "devbox.worktree": root}
	if _, err := currentPreview(root, common); err == nil ||
		!regexp.MustCompile(`demo-a-live-1.*\n.*demo-abc-review-5`).MatchString(err.Error()) {
		t.Errorf("expected ambiguity error, got %v", err)
	}
	// A legacy preview without devbox.worktree is found through its branch.
	trees[0].Branch = "old"
	delete(saved, "demo-a-live-1")
	delete(saved, "demo-abc-review-5")
	if got, err := currentPreview(root, common); got != "demo-old-live-3" {
		t.Errorf("legacy current = %q, %v", got, err)
	}
	trees[0].Branch = "elsewhere"
	if _, err := currentPreview(root, common); err == nil || !strings.Contains(err.Error(), "No preview") {
		t.Errorf("expected no preview, got %v", err)
	}
}

func TestGCOnlyRemovesPreviewsOfRemovedWorktrees(t *testing.T) {
	common := t.TempDir()
	saved := map[string]map[string]string{
		"demo-kept-live-1":   {"devbox.mode": "live", "devbox.worktree": "/wt/kept"},
		"demo-gone-live-2":   {"devbox.mode": "live", "devbox.worktree": "/wt/gone"},
		"demo-gone-review-3": {"devbox.mode": "review", "devbox.worktree": "/wt/gone"},
		"demo-old-live-4":    {"devbox.mode": "live", "devbox.branch": "deleted"},
		"demo-old-review-5":  {"devbox.mode": "review"},
	}
	swap(t, &repository, func() (string, string, error) { return common, common, nil })
	swap(t, &savedPreviews, func(string) (map[string]map[string]string, error) { return saved, nil })
	swap(t, &worktreesOf, func(string) ([]worktree, error) { return []worktree{{"/wt/kept", "kept"}}, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &unshareIfShared, func(string) error { return nil })
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	removed := map[string]bool{}
	swap(t, &destroyPreview, func(path string) error {
		removed[filepath.Base(filepath.Dir(path))] = true
		return nil
	})
	gc(false)
	if len(removed) != 0 {
		t.Errorf("dry run removed %v", removed)
	}
	gc(true)
	want := map[string]bool{"demo-gone-live-2": true, "demo-gone-review-3": true, "demo-old-live-4": true}
	if !equalSets(removed, want) {
		t.Errorf("removed %v, want %v", removed, want)
	}
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func TestRefuseAnotherClone(t *testing.T) {
	swap(t, &dockerContainers, withContainers(labelled(map[string]string{
		"devbox.preview": "demo-live", "devbox.repository": "/other/.git"})))
	if err := assertOwner("demo-live", "/mine/.git"); err == nil {
		t.Error("accepted a preview of another clone")
	}
}

func webContainer(service string, running bool, health string, web bool) container {
	labels := map[string]string{"devbox.preview": "demo-live-1", "devbox.mode": "live", "devbox.project": "demo",
		"com.docker.compose.service": service}
	if web {
		labels["traefik.enable"] = "true"
	}
	c := labelled(labels)
	c.ID = service
	c.State.Running, c.State.StartedAt = running, "2026-01-01T00:00:00Z"
	c.State.Status = "exited"
	if running {
		c.State.Status = "running"
	}
	c.State.Health = &struct {
		Status        string
		FailingStreak int
		Log           []struct {
			Output   string
			ExitCode int
		}
	}{Status: health}
	return c
}

func TestStoppedPreviewReportsStateNotStaleHealth(t *testing.T) {
	swap(t, &dockerContainers, withContainers(webContainer("web", false, "unhealthy", true), webContainer("db", true, "healthy", false)))
	swap(t, &hostFactsFn, func() (any, error) { return map[string]any{}, nil })
	data, err := overview()
	if err != nil || len(data.Previews) != 1 {
		t.Fatalf("overview: %v %v", data, err)
	}
	p := data.Previews[0]
	if p.Status != "exited" || p.Started != nil {
		t.Errorf("status %q started %v", p.Status, p.Started)
	}
	got := map[string]string{}
	for _, s := range p.Services {
		got[s.Name] = s.Status
	}
	if got["web"] != "exited" || got["db"] != "healthy" {
		t.Errorf("services %v", got)
	}
}

func TestPortsMarkTheTraefikRoutedPort(t *testing.T) {
	c := labelled(map[string]string{"traefik.http.services.x.loadbalancer.server.port": "8000"})
	c.Config.ExposedPorts = map[string]struct{}{"5432/tcp": {}, "53/udp": {}}
	want := []port{{53, "udp", false}, {5432, "tcp", false}, {8000, "tcp", true}}
	got := containerPorts(c)
	if len(got) != len(want) {
		t.Fatalf("ports %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("port %d = %v, want %v", i, got[i], want[i])
		}
	}
	if ports := containerPorts(container{}); ports == nil || len(ports) != 0 {
		t.Errorf("empty ports %v", ports)
	}
}

func TestDetailsMaskSecretLookingEnvValues(t *testing.T) {
	inspectOutput := `[{"Id": "abc", "Name": "/x-db-1", "Image": "sha256:1", "Created": "t", "RestartCount": 0,
		"State": {"Status": "running"}, "HostConfig": {}, "Mounts": [], "NetworkSettings": {"Networks": {}},
		"Config": {"Image": "postgres:15", "Labels": {}, "Env": [
			"POSTGRES_PASSWORD=hunter2", "API_TOKEN=t", "DB_HOST=db", "EMPTY_SECRET="]}}]`
	swap(t, &previewContainer, func(string, *string) (string, error) { return "abc", nil })
	swap(t, &runCommand, func([]string, runOpts) (string, error) { return inspectOutput, nil })
	db := "db"
	details, err := containerDetails("x", &db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range details["env"].([]envVar) {
		got[e.Name] = e.Value
	}
	want := map[string]string{"POSTGRES_PASSWORD": "••••••", "API_TOKEN": "••••••", "DB_HOST": "db", "EMPTY_SECRET": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestLogsRejectAnythingButAPreviewID(t *testing.T) {
	for _, name := range []string{"", "../etc", "a b", "x;rm"} {
		if _, err := previewLogs(name, nil); err == nil {
			t.Errorf("accepted preview %q", name)
		}
	}
	for _, svc := range []string{"", "../db", "db;rm", "-f"} {
		s := svc
		if _, err := previewContainer("demo-live-1", &s); err == nil {
			t.Errorf("accepted service %q", svc)
		}
	}
}

// gitRepo is a temporary repository made from the project template, with one commit.
type gitRepo struct {
	t      *testing.T
	temp   string
	root   string
	common string
	commit string
}

func newGitRepo(t *testing.T) *gitRepo {
	temp := t.TempDir()
	r := &gitRepo{t: t, temp: temp, root: filepath.Join(temp, "repo")}
	if out, err := exec.Command("cp", "-r", filepath.Join(kit, "project-template"), r.root).CombinedOutput(); err != nil {
		t.Fatalf("copy template: %v %s", err, out)
	}
	r.git("init", "-b", "main")
	r.git("config", "user.name", "Devbox test")
	r.git("config", "user.email", "test@example.invalid")
	r.git("config", "commit.gpgsign", "false")
	r.git("config", "core.hooksPath", "/dev/null")
	r.git("add", ".")
	r.git("commit", "-m", "sample")
	r.commit = r.git("rev-parse", "HEAD")
	r.common = filepath.Join(r.root, ".git")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", r.root}, args...)...).Output()
	if err != nil {
		r.t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestReviewUsesCommittedFilesAndRemovesCheckout(t *testing.T) {
	r := newGitRepo(t)
	message := filepath.Join(r.root, "message.txt")
	original, _ := os.ReadFile(message)
	os.WriteFile(message, []byte("Uncommitted live change"), 0o644)
	var observed []string
	swap(t, &repository, func() (string, string, error) { return r.root, r.common, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &hook, func(root string, cfg projectConfig, name string, env []string) (bool, error) {
		data, _ := os.ReadFile(filepath.Join(root, "message.txt"))
		observed = append(observed, string(data))
		if envValue(env, "DEVBOX_COMMIT") != r.commit {
			t.Errorf("hook got commit %q", envValue(env, "DEVBOX_COMMIT"))
		}
		return true, nil
	})
	swap(t, &render, func(root string, cfg projectConfig, mode string, env []string, metadata map[string]string) (map[string]any, error) {
		labels := map[string]any{}
		for k, v := range metadata {
			labels[k] = v
		}
		return map[string]any{"services": map[string]any{"web": map[string]any{"labels": labels}}}, nil
	})
	swap(t, &composeRun, func(string, ...string) error { return nil })
	swap(t, &dockerContainers, withContainers())
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	t.Setenv("DEV_DOMAIN", "dev.example.com")
	if err := preparePreview("review", r.commit); err != nil {
		t.Fatal(err)
	}
	saved, _ := savedPreviews(r.common)
	name := "demo-main-" + r.commit[:7]
	if len(saved) != 1 || saved[name]["devbox.identity"] != r.commit {
		t.Errorf("saved previews %v", saved)
	}
	if len(observed) == 0 {
		t.Error("no hooks ran")
	}
	for _, value := range observed {
		if value != string(original) {
			t.Errorf("hook saw %q, want the committed file", value)
		}
	}
	if data, _ := os.ReadFile(message); string(data) != "Uncommitted live change" {
		t.Error("live file changed")
	}
	if n := strings.Count(r.git("worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Errorf("%d worktrees left", n)
	}
}

func TestHooksRunFromDevboxJSONAndMissingOnesAreSkipped(t *testing.T) {
	r := newGitRepo(t)
	cfg := projectConfig{Hooks: map[string]string{"check": `echo "$DEVBOX_COMMIT" > hook.out`}}
	ran, err := hook(r.root, cfg, "check", withEnv(os.Environ(), map[string]string{"DEVBOX_COMMIT": "abc"}))
	if !ran || err != nil {
		t.Fatalf("check hook: %v %v", ran, err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.root, "hook.out")); string(data) != "abc\n" {
		t.Errorf("hook output %q", data)
	}
	if ran, _ := hook(r.root, cfg, "prepare", os.Environ()); ran {
		t.Error("missing hook ran")
	}
	var cmdErr *commandError
	if _, err := hook(r.root, projectConfig{Hooks: map[string]string{"check": "false"}}, "check", os.Environ()); !errors.As(err, &cmdErr) {
		t.Errorf("failing hook: %v", err)
	}
}

func TestWorktreesSkipRemovedDirectories(t *testing.T) {
	r := newGitRepo(t)
	kept, removed, locked := filepath.Join(r.temp, "kept"), filepath.Join(r.temp, "removed"), filepath.Join(r.temp, "locked")
	r.git("worktree", "add", "-b", "kept", kept)
	r.git("worktree", "add", "--detach", removed)
	r.git("worktree", "add", "--lock", "-b", "locked", locked)
	os.RemoveAll(removed)
	os.RemoveAll(locked)
	trees, err := worktreesOf(r.root)
	if err != nil {
		t.Fatal(err)
	}
	want := []worktree{{r.root, "main"}, {kept, "kept"}, {locked, "locked"}}
	if len(trees) != len(want) {
		t.Fatalf("worktrees %v", trees)
	}
	for i := range want {
		if trees[i] != want[i] {
			t.Errorf("worktree %d = %v, want %v", i, trees[i], want[i])
		}
	}
}

func TestWorktreeStateReportsHeadChangesAndLastCommit(t *testing.T) {
	r := newGitRepo(t)
	state := worktreeState(r.root)
	if state.Head != r.commit || state.Changes != 0 || state.Upstream != "" || state.Subject != "sample" || state.Committed == nil {
		t.Errorf("state %+v", state)
	}
	os.WriteFile(filepath.Join(r.root, "message.txt"), []byte("edited"), 0o644)
	os.WriteFile(filepath.Join(r.root, "new.txt"), []byte("untracked"), 0o644)
	if n := worktreeState(r.root).Changes; n != 2 {
		t.Errorf("changes %d", n)
	}
}

func TestCommitsBetweenCountsNewCommits(t *testing.T) {
	r := newGitRepo(t)
	r.git("commit", "--allow-empty", "-m", "one")
	r.git("commit", "--allow-empty", "-m", "two")
	head := r.git("rev-parse", "HEAD")
	if n := commitsBetween(r.root, r.commit, head); n == nil || *n != 2 {
		t.Errorf("between = %v", n)
	}
	if n := commitsBetween(r.root, head, head); n == nil || *n != 0 {
		t.Errorf("same = %v", n)
	}
	if n := commitsBetween(r.root, "", head); n != nil {
		t.Errorf("unknown = %v", *n)
	}
}

func TestLsShowsLiveWorktreeCommitAndStoppedState(t *testing.T) {
	r := newGitRepo(t)
	started := r.commit
	r.git("commit", "--allow-empty", "-m", "later")
	head := r.git("rev-parse", "HEAD")
	previewOf := func(name, mode string, running bool, health string) container {
		c := webContainer("web", running, health, true)
		for k, v := range map[string]string{"devbox.preview": name, "devbox.mode": mode, "devbox.commit": started,
			"devbox.url": "https://" + name + ".x", "devbox.repository": r.common, "devbox.worktree": r.root} {
			c.Config.Labels[k] = v
		}
		return c
	}
	swap(t, &dockerContainers, withContainers(previewOf("demo-main", "live", true, "healthy"),
		previewOf("demo-main-abc1234", "review", false, "unhealthy")))
	var buf bytes.Buffer
	swap[io.Writer](t, &stdout, &buf)
	if err := listing(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")[1:]
	live, review := strings.Split(lines[0], "\t"), strings.Split(lines[1], "\t")
	if live[2] != "healthy" || live[3] != head[:12] {
		t.Errorf("live row %v", live)
	}
	if review[2] != "exited" || review[3] != started[:12] {
		t.Errorf("review row %v", review)
	}
}

func TestExistingCheckoutIsNeverRemovedOnFailure(t *testing.T) {
	r := newGitRepo(t)
	checkout := filepath.Join(r.common, "devbox/checkouts", "demo-main-"+r.commit[:7])
	os.MkdirAll(checkout, 0o755)
	sentinel := filepath.Join(checkout, "keep")
	os.WriteFile(sentinel, []byte("existing work"), 0o644)
	swap(t, &repository, func() (string, string, error) { return r.root, r.common, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &dockerContainers, withContainers())
	var user userError
	if err := preparePreview("review", r.commit); !errors.As(err, &user) {
		t.Errorf("expected a user error, got %v", err)
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "existing work" {
		t.Error("existing checkout was touched")
	}
}

func TestSavedComposeRoundTripsUnchanged(t *testing.T) {
	// Saved compose.json files written by the Python version are rewritten by up(); nothing may change.
	original := `{"name": "x", "services": {"web": {"image": "a<b>&c", "mem_limit": "1073741824",
		"shm_size": 9007199254740993, "cpu_shares": 1.5, "networks": {"proxy": null},
		"environment": {"GREETING": "hej 🍋"}, "deploy": {"resources": {}}, "x-unknown": [1, "two", false]}}}`
	path := filepath.Join(t.TempDir(), "compose.json")
	value, err := decodeObject([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCompose(path, value); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	var before, after any
	d := json.NewDecoder(strings.NewReader(original))
	d.UseNumber()
	d.Decode(&before)
	d = json.NewDecoder(bytes.NewReader(written))
	d.UseNumber()
	d.Decode(&after)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("round trip changed the file:\n%s", written)
	}
	if !strings.Contains(string(written), "9007199254740993") || !strings.Contains(string(written), "a<b>&c") {
		t.Errorf("numbers or characters were rewritten:\n%s", written)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
}

func TestRoutesExposeMoreServicesOfAPreview(t *testing.T) {
	fakeImage(t, `{"services": {"web": {}, "api": {}, "core": {}, "db": {}}}`)
	cfg := projectConfig{Project: "demo", WebPort: 80, Routes: []route{
		{Service: "api", Port: 8080, Paths: []string{"/api", "/admin"}},
		{Service: "core", Port: 8002, Subdomains: true},
	}}
	output, err := render(filepath.Join(kit, "project-template"), cfg, "live", composeEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	services := object(output, "services")
	labels := func(name string) map[string]any { return object(object(services, name), "labels") }
	host := "demo-review-123.dev.example.com"
	for _, want := range []struct{ service, key, value string }{
		{"web", "devbox.web", "true"},
		{"web", "traefik.http.routers.demo-review-123.rule", "Host(`" + host + "`)"},
		{"api", "devbox.web", "false"},
		{"api", "traefik.enable", "true"},
		{"api", "traefik.http.routers.demo-review-123-route1.rule", "Host(`" + host + "`) && (PathPrefix(`/api`) || PathPrefix(`/admin`))"},
		{"api", "traefik.http.routers.demo-review-123-route1.service", "demo-review-123-api-8080"},
		{"api", "traefik.http.services.demo-review-123-api-8080.loadbalancer.server.port", "8080"},
		{"core", "traefik.http.routers.demo-review-123-route2.rule", "HostRegexp(`^[a-z0-9-]+\\.demo-review-123\\.dev\\.example\\.com$`)"},
		{"core", "traefik.http.routers.demo-review-123-route2.tls.certresolver", "dns"},
		{"core", "traefik.http.routers.demo-review-123-route2.tls.domains[0].sans", "*." + host},
		{"db", "traefik.enable", "false"},
	} {
		if got := str(labels(want.service), want.key); got != want.value {
			t.Errorf("%s %s = %q, want %q", want.service, want.key, got, want.value)
		}
	}
	for _, name := range []string{"web", "api", "core"} {
		if _, ok := object(object(services, name), "networks")["proxy"]; !ok {
			t.Errorf("%s is not on the proxy network", name)
		}
	}
	if _, ok := object(object(services, "db"), "networks")["proxy"]; ok {
		t.Error("db is on the proxy network")
	}
}

func TestRoutesNeedAnExistingServiceAndSafeRules(t *testing.T) {
	fakeImage(t, `{"services": {"web": {}}}`)
	cfg := projectConfig{Project: "demo", Routes: []route{{Service: "api", Port: 80, Paths: []string{"/api"}}}}
	if _, err := render(filepath.Join(kit, "project-template"), cfg, "live", composeEnv(), nil); err == nil {
		t.Error("accepted a route to a missing service")
	}
	for _, r := range []route{
		{Service: "api", Port: 80},
		{Service: "api", Port: "x", Paths: []string{"/api"}},
		{Service: "api", Port: 80, Paths: []string{"/"}},
		{Service: "api", Port: 80, Paths: []string{"/api`) || Host(`evil"}},
		{Service: "../api", Port: 80, Subdomains: true},
	} {
		if r.validate() == nil {
			t.Errorf("accepted route %+v", r)
		}
	}
	if err := (route{Service: "api", Port: json.Number("80"), Paths: []string{"/api/v1"}}).validate(); err != nil {
		t.Errorf("rejected a valid route: %v", err)
	}
}

func TestWebContainerOfPreviewsWithAndWithoutRoutes(t *testing.T) {
	for _, c := range []struct {
		labels map[string]string
		web    bool
	}{
		{map[string]string{"traefik.enable": "true"}, true},
		{map[string]string{"traefik.enable": "false"}, false},
		{map[string]string{"traefik.enable": "true", "devbox.web": "true"}, true},
		{map[string]string{"traefik.enable": "true", "devbox.web": "false"}, false},
	} {
		if isWeb(c.labels) != c.web {
			t.Errorf("isWeb(%v) = %v", c.labels, !c.web)
		}
	}
}

func TestWaitTimeoutTravelsWithTheSavedPreview(t *testing.T) {
	fakeImage(t, `{"services": {"web": {}}}`)
	cfg := projectConfig{Project: "demo", WaitTimeout: json.Number("600")}
	output, err := render(filepath.Join(kit, "project-template"), cfg, "live", composeEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	swap(t, &composeRun, func(_ string, a ...string) error { args = a; return nil })
	if err := composeUp("compose.json", output); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "up -d --wait --wait-timeout 600" {
		t.Errorf("compose %s", got)
	}
	if err := composeUp("compose.json", map[string]any{}); err != nil || args[len(args)-1] != "120" {
		t.Errorf("default timeout %v", args)
	}
}

func TestNewPreviewsBuildAndResumedOnesDoNot(t *testing.T) {
	r := newGitRepo(t)
	swap(t, &repository, func() (string, string, error) { return r.root, r.common, nil })
	swap(t, &assertOwner, noOwnerCheck)
	swap(t, &hook, func(string, projectConfig, string, []string) (bool, error) { return true, nil })
	swap(t, &render, func(root string, cfg projectConfig, mode string, env []string, metadata map[string]string) (map[string]any, error) {
		labels := map[string]any{}
		for k, v := range metadata {
			labels[k] = v
		}
		return map[string]any{"services": map[string]any{"web": map[string]any{"labels": labels, "build": "."}}}, nil
	})
	var calls []string
	swap(t, &composeRun, func(_ string, args ...string) error { calls = append(calls, args[0]); return nil })
	swap(t, &dockerContainers, withContainers())
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	t.Setenv("DEV_DOMAIN", "dev.example.com")
	if err := preparePreview("live", ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, " "); got != "build up" {
		t.Errorf("new preview ran compose %q", got)
	}
	calls = nil
	if err := preparePreview("live", ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, " "); got != "up" {
		t.Errorf("resumed preview ran compose %q", got)
	}
}

func TestPreviewsWithoutBuildsSkipComposeBuild(t *testing.T) {
	if hasBuild(map[string]any{"services": map[string]any{"web": map[string]any{"image": "python:3.13-slim"}}}) {
		t.Error("a project of prebuilt images needs no compose build")
	}
	if !hasBuild(map[string]any{"services": map[string]any{"db": map[string]any{}, "web": map[string]any{"build": map[string]any{"context": "."}}}}) {
		t.Error("a service with build needs compose build")
	}
}

func TestRouteURLsComeFromTheRouterRules(t *testing.T) {
	fakeImage(t, `{"services": {"web": {}, "api": {}, "core": {}, "assets": {}}}`)
	cfg := projectConfig{Project: "demo", Routes: []route{
		{Service: "api", Port: 80, Paths: []string{"/api", "/admin"}},
		{Service: "core", Port: 8002, Subdomains: true},
		{Service: "assets", Port: 3000, Paths: []string{"/_assets"}, Subdomains: true},
	}}
	output, err := render(filepath.Join(kit, "project-template"), cfg, "live", composeEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	host := "demo-review-123.dev.example.com"
	for name, want := range map[string][]string{
		"web":    {"https://" + host},
		"api":    {"https://" + host + "/api", "https://" + host + "/admin"},
		"core":   {"https://*." + host},
		"assets": {"https://*." + host + "/_assets"},
	} {
		labels := map[string]string{}
		for k, v := range object(object(object(output, "services"), name), "labels") {
			labels[k], _ = v.(string)
		}
		if got := routeURLs(labels); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	if got := routeURLs(map[string]string{"traefik.enable": "false"}); len(got) != 0 {
		t.Errorf("unrouted container: %v", got)
	}
}
