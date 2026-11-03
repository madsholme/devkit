package main

import (
	"fmt"
	"strings"
)

// Every review is a fresh build, so build cache and old images pile up as previews come and go.
// prune trims both host-wide; devkit web runs it once a day unless the host config says
// "prune": {"daily": false}. How much it keeps comes from the same section.

// prune trims the Docker build cache to build_cache_gb and removes images that no container,
// running or stopped, uses and that are older than image_age_days. Volumes are never touched:
// they hold preview data.
func prune(apply bool) error {
	cfg, err := loadHostConfig()
	if err != nil {
		return err
	}
	limit := fmt.Sprintf("%dGB", cfg.Prune.BuildCacheGB)
	age := fmt.Sprintf("%dh", cfg.Prune.ImageAgeDays*24)
	if !apply {
		out, err := capture("docker", "system", "df")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, out)
		fmt.Fprintf(stdout, "Run devkit prune --yes to trim the build cache to %s and remove unused images older than %d days. Volumes are kept.\n",
			limit, cfg.Prune.ImageAgeDays)
		return nil
	}
	cache, err := capture("docker", "builder", "prune", "--force", "--max-used-space", limit)
	if err != nil {
		return err
	}
	images, err := capture("docker", "image", "prune", "--all", "--force", "--filter", "until="+age)
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
