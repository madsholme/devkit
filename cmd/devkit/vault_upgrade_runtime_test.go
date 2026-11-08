package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Uses the real CLI handler, encryption and Docker lifecycle. The only command
// substitution is the parent fixture's unique Compose project, protecting the host.
func runtimeBackupUpgrade(t *testing.T, cfg vaultConfig, backend *vaultClient, unsealKey, path, sentinel string) string {
	t.Helper()
	const target = "openbao/openbao:2.7.2"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	compose := filepath.Join(vaultDir(), "openbao", "compose.json")
	containerID := func() string {
		t.Helper()
		id, e := probe("docker", "compose", "-f", compose, "ps", "-q", "openbao")
		if e != nil || len(id) != 64 {
			t.Fatal("Missing disposable upgrade container")
		}
		return id
	}
	beforeID := containerID()
	dataMount := func(id string) string {
		t.Helper()
		var mounts []struct{ Type, Source, Destination string }
		data := runtimeDocker(t, nil, "inspect", "--format", "{{json .Mounts}}", id)
		if json.Unmarshal(data, &mounts) != nil {
			t.Fatal("Cannot inspect upgrade storage")
		}
		for _, mount := range mounts {
			if mount.Destination == "/bao/data" && mount.Type == "bind" {
				return mount.Source
			}
		}
		t.Fatal("OpenBao upgrade did not use a data bind mount")
		return ""
	}
	beforeMount := dataMount(beforeID)
	beforeConfig, e := os.ReadFile(configPath())
	if e != nil {
		t.Fatal(e)
	}
	beforeCompose, e := os.ReadFile(compose)
	if e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	swap(t, &stdout, io.Writer(&output))
	if _, e = vaultUpdateCLI([]string{"--image", target, "--check", "--yes"}); e != nil {
		t.Fatal(e)
	}
	afterConfig, e := os.ReadFile(configPath())
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(beforeConfig, afterConfig) || containerID() != beforeID || exists(cfg.Backup.Directory) || exists(filepath.Join(vaultDir(), "update.json")) {
		t.Fatal("Update preview changed config, storage, container or backups")
	}
	// Invoke the actual updater with a real encryption failure; it must not recreate OpenBao.
	invalid := cfg
	invalid.Backup.Recipient = "age1invalid"
	if e = saveVaultConfig(invalid); e != nil {
		t.Fatal(e)
	}
	if _, e = vaultUpdateCLI([]string{"--image", target, "--yes"}); e == nil {
		t.Fatal("Upgrade proceeded after backup encryption failed")
	}
	still, e := loadVaultConfig()
	if e != nil || still.Image != cfg.Image {
		t.Fatal("Failed backup changed configured OpenBao version")
	}
	afterCompose, e := os.ReadFile(compose)
	if e != nil {
		t.Fatal(e)
	}
	if containerID() != beforeID || !bytes.Equal(beforeCompose, afterCompose) || exists(filepath.Join(vaultDir(), "update.json")) {
		t.Fatal("Failed backup changed the OpenBao container or update state")
	}
	entries, e := os.ReadDir(cfg.Backup.Directory)
	if e != nil || len(entries) != 0 {
		t.Fatal("Failed pre-upgrade backup left output")
	}
	value, _, e := backend.read(ctx, path)
	if e != nil || value != sentinel {
		t.Fatal("Failed upgrade disrupted source vault")
	}
	if e = saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	t.Log("Upgrade --check is read-only; failed encryption blocks the update and leaves the original vault running")
	output.Reset()
	if _, e = vaultUpdateCLI([]string{"--image", target, "--yes"}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(output.String(), "2.7.2; state sealed") {
		t.Fatal("Upgrade did not report target version and manual-unseal state")
	}
	var record struct {
		Previous string `json:"previous_image"`
		Target   string `json:"target_image"`
		Backup   string `json:"backup_id"`
	}
	b, e := readPrivate(filepath.Join(vaultDir(), "update.json"), 65536)
	if e != nil || json.Unmarshal(b, &record) != nil || record.Previous != cfg.Image || record.Target != target || record.Backup == "" {
		t.Fatal("Upgrade recovery record missing exact previous image/backup")
	}
	if e = verifyVaultBackupComponents(cfg, record.Backup); e != nil {
		t.Fatal(e)
	}
	var manifest vaultBackupManifest
	b, e = readPrivate(filepath.Join(cfg.Backup.Directory, record.Backup, "manifest.json"), 65536)
	if e != nil || json.Unmarshal(b, &manifest) != nil || manifest.Image != cfg.Image || len(manifest.Files) != 5 {
		t.Fatal("Upgrade backup omitted original image or recovery components")
	}
	afterID := containerID()
	if beforeID == afterID || dataMount(afterID) != beforeMount {
		t.Fatal("Upgrade failed to recreate container with the same data bind")
	}
	image := runtimeDocker(t, nil, "inspect", "--format", "{{.Config.Image}}", afterID)
	if strings.TrimSpace(string(image)) != target {
		t.Fatal("Upgrade container uses wrong image")
	}
	active, e := loadVaultConfig()
	if e != nil || active.Image != target {
		t.Fatal("Upgrade configuration did not record target image")
	}
	c, e := newVaultClient(cfg.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	state, e := c.status(ctx)
	if e != nil || state.Version != "2.7.2" || state.State != "sealed" {
		t.Fatal("Upgraded vault did not preserve sealed initialized storage")
	}
	if _, _, e = backend.read(ctx, path); e != (vaultError{Code: 503}) {
		t.Fatal("Sealed upgrade did not fail closed")
	}
	unseal := func() {
		t.Helper()
		if e := c.call(ctx, "POST", "sys/unseal", map[string]string{"key": unsealKey}, nil); e != nil {
			t.Fatal(e)
		}
		runtimeVaultState(t, ctx, c, "ready")
		got, version, e := backend.read(ctx, path)
		if e != nil || got != sentinel || version != 1 {
			t.Fatal("Upgrade lost existing credential, key or version")
		}
	}
	unseal()
	if _, e = applyVaultUpdate(active, cfg.Image, true); e == nil || containerID() != afterID {
		t.Fatal("Updater allowed in-place data downgrade")
	}
	// A second restart verifies persistent data, rather than only the initial upgrade response.
	if _, e = runCommand([]string{"docker", "compose", "-f", compose, "restart"}, runOpts{capture: true, quiet: true}); e != nil {
		t.Fatal(e)
	}
	state, e = waitVaultVersion(ctx, c, "2.7.2")
	if e != nil || state.State != "sealed" {
		t.Fatal("Upgraded restart lost initialized storage")
	}
	unseal()
	t.Log("Real 2.7.1 → 2.7.2 upgrade created an encrypted recovery backup, preserved its bind directory and secrets, required manual unseal, survived restart and rejected downgrade")
	return record.Backup
}
