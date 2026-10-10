package main

import (
	"fmt"
	"strings"
)

// Every review is a fresh build, so build cache and old images pile up as previews come and go.
// prune trims both host-wide; devbox web runs it once a day.
const (
	pruneCacheLimit = "30GB" // build cache kept so rebuilds stay fast
	pruneImageAge   = "168h" // unused images older than this are removed
)

// prune trims the Docker build cache to pruneCacheLimit and removes images that no container,
// running or stopped, uses and that are older than pruneImageAge. Volumes are never touched:
// they hold preview data.
func prune(apply bool) error {
	if !apply {
		out, err := capture("docker", "system", "df")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, out)
		fmt.Fprintf(stdout, "Run devbox prune --yes to trim the build cache to %s and remove unused images older than %s. Volumes are kept.\n",
			pruneCacheLimit, pruneImageAge)
		return nil
	}
	cache, err := capture("docker", "builder", "prune", "--force", "--max-used-space", pruneCacheLimit)
	if err != nil {
		return err
	}
	images, err := capture("docker", "image", "prune", "--all", "--force", "--filter", "until="+pruneImageAge)
	if err != nil {
		return err
	}
	detail := fmt.Sprintf("build cache %s, images %s", reclaimed(cache), reclaimed(images))
	fmt.Fprintf(stdout, "Reclaimed %s.\n", detail)
	happened("prune", "", detail)
	return nil
}

// reclaimed is the size from Docker's "Total reclaimed space:" line, or 0B when it prints none.
func reclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if size, ok := strings.CutPrefix(strings.TrimSpace(line), "Total reclaimed space:"); ok {
			return strings.TrimSpace(size)
		}
		if size, ok := strings.CutPrefix(strings.TrimSpace(line), "Total:"); ok {
			return strings.TrimSpace(size)
		}
	}
	return "0B"
}
