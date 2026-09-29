package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(value string, limit int) string {
	readable := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(value), "-"), "-")
	if readable == "" {
		readable = "task"
	}
	if limit < len(readable) {
		readable = readable[:max(limit, 0)]
	}
	return strings.TrimRight(readable, "-")
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// legacyName is the preview ID scheme before readable names; existing previews keep it.
func legacyName(project, identity, mode string) string {
	return slug(project, 14) + "-" + slug(identity, 20) + "-" + mode + "-" + digest(project, identity, mode)[:10]
}

// clash is the room kept for "-" plus a 4-character digest when a readable name is taken.
const clash = 5

// previewName is the readable preview ID: <project>-<branch> for live, <project>-<branch>-<sha7> for review.
func previewName(project, mode, branch, commit, identity string) string {
	head, sha := slug(project, 20), ""
	if mode == "review" {
		sha = "-" + commit[:min(7, len(commit))]
	}
	label := branch
	if label == "" && mode == "live" {
		label = identity
	}
	middle := ""
	if label != "" {
		middle = "-" + slug(label, 63-clash-len(head)-len(sha)-1)
	}
	return head + middle + sha
}

func clashName(name, common, identity, mode string) string {
	return name + "-" + digest(common, identity, mode)[:clash-1]
}

// findSaved returns the saved preview for this identity: labelled by devbox.identity, or named the legacy way.
func findSaved(saved map[string]map[string]string, project, mode, identity string) string {
	for _, name := range sortedKeys(saved) {
		labels := saved[name]
		if labels["devbox.identity"] == identity && labels["devbox.mode"] == mode && labels["devbox.project"] == project {
			return name
		}
	}
	if legacy := legacyName(project, identity, mode); saved[legacy] != nil {
		return legacy
	}
	return ""
}

// newPreviewName picks a readable ID no other preview on this host uses; a short digest is added only on a clash.
func newPreviewName(common, project, mode, branch, commit, identity string) (string, error) {
	saved, err := savedPreviews(common)
	if err != nil {
		return "", err
	}
	containers, err := dockerContainers()
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for name := range saved {
		taken[name] = true
	}
	for _, c := range containers {
		taken[c.Config.Labels["devbox.preview"]] = true
	}
	name := previewName(project, mode, branch, commit, identity)
	if taken[name] {
		name = clashName(name, common, identity, mode)
		if taken[name] {
			return "", fail("Preview ID %s is already in use.", name)
		}
	}
	return name, nil
}
