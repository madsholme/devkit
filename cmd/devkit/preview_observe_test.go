package main

import (
	"context"
	"strings"
	"testing"
)

func observedFixture(t *testing.T) container {
	t.Helper()
	c := labelled(map[string]string{managedLabel: "true", "devkit.preview": "demo-main", "devkit.repository": "/repo/.git", "devkit.worktree": "/repo", "devkit.mode": "live", "devkit.web": "true", "devkit.url": "https://demo-main.dev.test", "com.docker.compose.service": "web"})
	c.ID = strings.Repeat("a", 64)
	c.State.Status = "exited"
	swap(t, &observeRepository, func(context.Context) (string, string, error) { return "/repo", "/repo/.git", nil })
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	return c
}
func TestPreviewObserveOwnership(t *testing.T) {
	c := observedFixture(t)
	for _, foreign := range []bool{false, true} {
		rows := []container{c}
		if foreign {
			other := c
			other.Config.Labels = map[string]string{}
			for k, v := range c.Config.Labels {
				other.Config.Labels[k] = v
			}
			other.Config.Labels["devkit.repository"] = "/other/.git"
			rows = append(rows, other)
		}
		swap(t, &observeContainers, func(context.Context) ([]container, error) { return rows, nil })
		target, err := resolveObservedPreview(context.Background(), "demo-main")
		if (err != nil) != foreign {
			t.Fatalf("foreign=%v: %v", foreign, err)
		}
		if !foreign && (target.ID != "demo-main" || target.CommonDir != "/repo/.git") {
			t.Fatal(target)
		}
	}
}
func TestPreviewObserveAmbiguousService(t *testing.T) {
	c := observedFixture(t)
	other := c
	other.ID = strings.Repeat("b", 64)
	target := observedPreviewTarget{Containers: []container{c, other}}
	if _, err := selectObservedService(target, ""); err == nil {
		t.Fatal("ambiguous web selected")
	}
	if _, err := selectObservedService(target, "../web"); err == nil {
		t.Fatal("invalid service selected")
	}
}
func TestPreviewObserveMissingRecipe(t *testing.T) {
	observedFixture(t)
	t.Chdir(t.TempDir())
	for _, name := range []string{"demo-main", ""} {
		target, err := resolveObservedPreview(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		c, err := selectObservedService(target, "")
		if err != nil || c.State.Status != "exited" {
			t.Fatal(c, err)
		}
	}
}
func TestPreviewObserveMissingAndConflictingIdentity(t *testing.T) {
	c := observedFixture(t)
	for _, key := range []string{"devkit.mode", "com.docker.compose.service"} {
		t.Run(key, func(t *testing.T) {
			c.Config.Labels[key] = ""
			if _, err := resolveObservedPreview(context.Background(), "demo-main"); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return nil, nil })
	if _, err := resolveObservedPreview(context.Background(), "demo-main"); err == nil {
		t.Fatal("missing selected")
	}
}
