package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var backupBeforeVaultUpdate = createVaultBackupLocked

func imageVersion(image string) ([3]int, error) {
	var n [3]int
	s := strings.Split(strings.Split(image, ":")[1], "@")[0]
	_, e := fmt.Sscanf(s, "%d.%d.%d", &n[0], &n[1], &n[2])
	return n, e
}
func applyVaultUpdate(cfg vaultConfig, target string, apply bool) (string, error) {
	if !pinnedVaultImage.MatchString(target) || !pinnedVaultImage.MatchString(cfg.Image) {
		return "", fail("Update requires an exact official OpenBao image version")
	}
	from, _ := imageVersion(cfg.Image)
	to, _ := imageVersion(target)
	if to[0] != from[0] || to[1] < from[1] || to[1] > from[1]+1 || (to[1] == from[1] && to[2] <= from[2]) {
		return "", fail("Update supports a newer patch or the next minor in the same major; no downgrade or skipped minor")
	}
	preview := fmt.Sprintf("Update %s to %s after encrypted backup; preserve bind-mounted data. Manual unseal required; restore compatible backup for rollback.", cfg.Image, target)
	if !apply {
		return preview, nil
	}
	unlock, e := lockSecretState()
	if e != nil {
		return "", e
	}
	defer unlock()
	id, e := backupBeforeVaultUpdate()
	if e != nil {
		return "", e
	}
	// All data-changing operations follow the verified encrypted recovery copy.
	if _, e = runCommand([]string{"docker", "pull", target}, runOpts{capture: true, quiet: true}); e != nil {
		return "", fail("Target image could not be pulled; current OpenBao is unchanged")
	}
	previous := cfg
	cfg.Image = target
	b, _ := json.Marshal(map[string]any{"previous_image": previous.Image, "target_image": target, "backup_id": id, "state": "awaiting_unseal"})
	if e = writePrivate(filepath.Join(vaultDir(), "update.json"), b); e != nil {
		return "", e
	}
	if e = saveVaultConfig(cfg); e != nil {
		return "", e
	}
	if e = startVault(cfg); e != nil {
		return "", fail("Update did not start; preserve data and recover using backup %s (no automatic downgrade)", id)
	}
	c, _ := newVaultClient(cfg.Address, "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	expected := strings.Split(strings.Split(target, ":")[1], "@")[0]
	status, e := waitVaultVersion(ctx, c, expected)
	if e != nil {
		return "", fail("Update started but health is not yet verified; run vault health, backup %s", id)
	}
	happened("vault update", target, "backup "+id)
	return fmt.Sprintf("OpenBao %s; state %s. Backup %s. Unseal if required, then run vault health.", status.Version, status.State, id), nil
}
func vaultUpdateCLI(args []string) (int, error) {
	pos, v, e := splitArgs(args, map[string]bool{"--image": true}, map[string]bool{"--check": true, "--yes": true, "--json": true})
	if e != nil || len(pos) > 0 {
		return 2, fail("usage: vault update --image PINNED_IMAGE [--check|--yes]")
	}
	cfg, e := loadVaultConfig()
	if e != nil {
		return 1, e
	}
	target := v["--image"]
	if target == "" {
		return 0, json.NewEncoder(stdout).Encode(map[string]string{"current_image": cfg.Image, "candidate_image": defaultOpenBaoImage, "instruction": "Select an exact tested image with --image; updates are never automatic"})
	}
	_, apply := v["--yes"]
	if _, check := v["--check"]; check {
		apply = false
	}
	out, e := applyVaultUpdate(cfg, target, apply)
	if e == nil {
		fmt.Fprintln(stdout, out)
	}
	return 0, e
}

func waitVaultVersion(ctx context.Context, c *vaultClient, expected string) (vaultStatus, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, e := c.status(ctx)
		if e == nil && status.Version == expected && (status.State == "ready" || status.State == "sealed") {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return vaultStatus{}, fail("OpenBao expected version did not become healthy or sealed before timeout")
		case <-ticker.C:
		}
	}
}
