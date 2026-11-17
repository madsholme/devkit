package main

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// observationCommand never forwards raw diagnostics or unbounded child output.
var observationCommand = func(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var out inspectionBuffer
	cmd.Stdout = &out
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil || out.overflow {
		return "", fail("Observation unavailable.")
	}
	return strings.TrimSpace(out.String()), nil
}
var observeRepository = func(ctx context.Context) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fail("Checkout unavailable.")
	}
	root, err := observationCommand(ctx, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", fail("Run from the owning Git repository.")
	}
	common, err := observationCommand(ctx, "git", "--no-optional-locks", "-c", "core.fsmonitor=false", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return root, common, err
}
var observeContainers = func(ctx context.Context) ([]container, error) { return hostContainers(ctx) }

type observedPreviewTarget struct {
	ID, CommonDir, Mode, URL string
	Containers               []container
}

func resolveObservedPreview(ctx context.Context, name string) (observedPreviewTarget, error) {
	var target observedPreviewTarget
	if name != "" && !previewID.MatchString(name) {
		return target, fail("Use an exact preview ID.")
	}
	root, common, err := observeRepository(ctx)
	if err != nil {
		return target, err
	}
	rows, err := observeContainers(ctx)
	if err != nil {
		return target, fail("Preview inventory unavailable.")
	}
	if name == "" {
		ids := map[string]bool{}
		for _, c := range rows {
			l := c.Config.Labels
			if l[managedLabel] == "true" && l["devkit.mode"] == "live" && l["devkit.worktree"] == root && l["devkit.repository"] == common {
				ids[l["devkit.preview"]] = true
			}
		}
		if len(ids) != 1 {
			return target, fail("No unique live preview for this checkout; use --preview with an exact owned ID.")
		}
		for id := range ids {
			name = id
		}
	}
	if !previewID.MatchString(name) {
		return target, fail("Preview identity is unavailable.")
	}
	target.ID, target.CommonDir = name, common
	for _, c := range rows {
		l := c.Config.Labels
		if l[managedLabel] != "true" || l["devkit.preview"] != name {
			continue
		}
		if l["devkit.repository"] != common {
			return observedPreviewTarget{}, fail("Preview belongs to another repository.")
		}
		if l["devkit.service"] == "share" {
			continue
		}
		mode := l["devkit.mode"]
		if !fullContainerID.MatchString(c.ID) || (mode != "live" && mode != "review") || !serviceName.MatchString(l["com.docker.compose.service"]) || (target.Mode != "" && target.Mode != mode) {
			return observedPreviewTarget{}, fail("Preview has conflicting or incomplete container identity.")
		}
		target.Mode = mode
		target.Containers = append(target.Containers, c)
	}
	if len(target.Containers) == 0 {
		return target, fail("No retained container for this preview.")
	}
	sort.Slice(target.Containers, func(i, j int) bool { return target.Containers[i].ID < target.Containers[j].ID })
	if web, err := selectObservedService(target, ""); err == nil {
		target.URL = web.Config.Labels["devkit.url"]
	}
	if err := ctx.Err(); err != nil {
		return observedPreviewTarget{}, fail("Preview observation timed out.")
	}
	return target, nil
}
func selectObservedService(target observedPreviewTarget, service string) (container, error) {
	if service != "" && !serviceName.MatchString(service) {
		return container{}, fail("Use an exact Compose service name.")
	}
	var matches []container
	for _, c := range target.Containers {
		l := c.Config.Labels
		if (service == "" && isWeb(l)) || (service != "" && l["com.docker.compose.service"] == service) {
			matches = append(matches, c)
		}
	}
	if len(matches) != 1 {
		return container{}, fail("No unique retained container for that service.")
	}
	return matches[0], nil
}
