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

func runtimeRestoreRehearsal(t *testing.T, cfg vaultConfig, id, identity, share string, source *vaultClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	baseline, e := hostContainers(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b, e := inspectRestoreBackup(cfg, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = decryptRestoreBackup(ctx, b, filepath.Join(filepath.Dir(identity), "wrong")); e == nil {
		t.Fatal("restore reader accepted wrong decryption identity")
	}
	plain, e := decryptRestoreBackup(ctx, b, identity)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(plain.Root)
	input := &restoreCredentials{Share: func(vaultStatus) (string, error) { return share, nil }, Login: func() (string, string, error) { return "admin", "runtime-human-password-strong", nil }}
	candidate, e := rehearseVaultRestore(ctx, b, plain, input, "")
	if e != nil {
		t.Fatal(e)
	}
	if candidate.Address == source.address {
		t.Fatal("rehearsal reused source address")
	}
	if e = candidate.close(true); e != nil {
		t.Fatal(e)
	}
	if s, e := source.status(ctx); e != nil || s.State != "ready" {
		t.Fatal("rehearsal changed source health")
	}
	after, e := hostContainers(ctx)
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]containerState{}
	for _, row := range after {
		states[row.ID] = row.State
	}
	for _, row := range baseline {
		if states[row.ID].StartedAt != row.State.StartedAt || states[row.ID].Running != row.State.Running {
			t.Fatal("rehearsal changed a pre-existing container")
		}
	}
	runtimeRestoreCutover(t, ctx, cfg, b, identity, share, source)
	t.Log("Production restore rehearsal recovered real encrypted snapshot in isolated internal-network Docker instance; original vault preserved")
}

func runtimeRestoreCutover(t *testing.T, ctx context.Context, cfg vaultConfig, b restoreBackup, identity, share string, source *vaultClient) {
	t.Helper()
	rows, e := hostContainers(ctx)
	if e != nil {
		t.Fatal(e)
	}
	project := ""
	for _, row := range rows {
		for _, m := range row.Mounts {
			if m.Source == restoreDataPath() {
				project = row.Config.Labels["com.docker.compose.project"]
			}
		}
	}
	if !strings.HasPrefix(project, "devkit-vault-test-") {
		t.Fatal("refusing non-fixture cutover")
	}
	swap(t, &restoreManagedProject, func() string { return project })
	swap(t, &restoreWebStopped, func(context.Context) error { return nil }) // No web service belongs to this fixture.
	credentials := func() *restoreCredentials {
		return &restoreCredentials{Share: func(vaultStatus) (string, error) { return share, nil }, Login: func() (string, string, error) { return "admin", "runtime-human-password-strong", nil }}
	}
	before, e := restoreManagedContainer(ctx, cfg)
	if e != nil || before.ID == "" {
		t.Fatal("source ownership", e)
	}
	oldData, e := restoreDirIdentity(restoreDataPath())
	if e != nil {
		t.Fatal(e)
	}
	oldKey, _, e := source.read(ctx, "devkit/system/web-session-signing-key")
	if e != nil {
		t.Fatal(e)
	}
	// A failed human login must leave the source and host metadata untouched.
	files := map[string]string{}
	for name, path := range restoreFilePaths() {
		raw, e := os.ReadFile(path)
		if e == nil {
			files[name] = restoreFileHash(raw)
		}
	}
	bad := credentials()
	bad.Login = func() (string, string, error) { return "admin", "wrong-synthetic-password", nil }
	if _, e = runVaultRestore(ctx, cfg, b, identity, "", true, bad); e == nil {
		t.Fatal("wrong administrator password accepted")
	}
	bad = credentials()
	bad.Share = func(vaultStatus) (string, error) { return "invalid-synthetic-unseal-share", nil }
	if _, e = runVaultRestore(ctx, cfg, b, identity, "", true, bad); e == nil {
		t.Fatal("wrong original share accepted")
	}
	after, e := restoreManagedContainer(ctx, cfg)
	if e != nil || after.ID != before.ID || after.State.StartedAt != before.State.StartedAt {
		t.Fatal("failed rehearsal touched source")
	}
	for name, hash := range files {
		raw, e := os.ReadFile(restoreFilePaths()[name])
		if e != nil || restoreFileHash(raw) != hash {
			t.Fatal("failed rehearsal changed host metadata", name)
		}
	}
	// Distinguish previous data from the backup; this value must return on rollback.
	if _, e = source.write(ctx, "devkit/system/restore-rollback-probe", "synthetic-after-backup", 0); e != nil {
		t.Fatal(e)
	}
	if e = saveSecretTokens(secretTokenStore{Schema: 1, Tokens: map[string]secretAPIToken{"synthetic-revocation-probe": {}}}); e != nil {
		t.Fatal(e)
	}
	// Recovery must not depend on a healthy/running configured vault.
	if _, e = restoreCommand(ctx, "docker", "stop", before.ID); e != nil {
		t.Fatal(e)
	}
	operation, e := runVaultRestore(ctx, cfg, b, identity, "", true, credentials())
	if e != nil {
		t.Fatal("apply", e)
	}
	restored, e := managedVaultClient()
	if e != nil {
		t.Fatal(e)
	}
	if restored.token == source.token {
		t.Fatal("backend identity reused")
	}
	newKey, _, e := restored.read(ctx, "devkit/system/web-session-signing-key")
	if e != nil || newKey == oldKey {
		t.Fatal("session key reused")
	}
	if _, _, e = restored.read(ctx, "devkit/system/restore-rollback-probe"); e == nil {
		t.Fatal("backup did not replace later source data")
	}
	previous, e := restoreDirIdentity(filepath.Join(restoreOperationRoot(operation), "previous-data"))
	if e != nil || previous != oldData {
		t.Fatal("old data not retained")
	}
	checkRevoked := func() {
		raw, e := readPrivate(filepath.Join(vaultDir(), "api-tokens.json"), 1<<20)
		var store secretTokenStore
		if e != nil || json.Unmarshal(raw, &store) != nil || len(store.Tokens) != 0 {
			t.Fatal("API token revocations not preserved")
		}
	}
	checkRevoked()
	cleanup, e := loadCleanupConfig()
	if e != nil || cleanup.Automatic {
		t.Fatal("automatic cleanup enabled after restore")
	}
	if e = rollbackVaultRestore(ctx, operation, credentials()); e != nil {
		t.Fatal("rollback", e)
	}
	checkRevoked()
	stopped, e := restoreManagedContainer(ctx, cfg)
	if e != nil || stopped.State.Running {
		t.Fatal("rollback did not preserve prior stopped state")
	}
	// Resume this test fixture explicitly for the runner's remaining checks.
	if _, e = restoreCommand(ctx, "docker", "start", stopped.ID); e != nil {
		t.Fatal(e)
	}
	if e = waitRestoreState(ctx, source, "sealed"); e != nil {
		t.Fatal(e)
	}
	if e = restoreUnseal(ctx, source, credentials(), false); e != nil {
		t.Fatal(e)
	}
	rollback, e := managedVaultClient()
	if e != nil {
		t.Fatal(e)
	}
	value, _, e := rollback.read(ctx, "devkit/system/restore-rollback-probe")
	if e != nil || value != "synthetic-after-backup" {
		t.Fatal("previous source data not recovered")
	}
	key, _, e := rollback.read(ctx, "devkit/system/web-session-signing-key")
	if e != nil || key == oldKey || key == newKey {
		t.Fatal("rollback revived browser sessions")
	}
	if e = rollbackVaultRestore(ctx, operation, credentials()); e != nil {
		t.Fatal("completed rollback not idempotent", e)
	}
	raw, e := os.ReadFile(historyPath())
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{share, source.token, restored.token, rollback.token, "runtime-human-password-strong"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("recovery history leaked credential")
		}
	}
	t.Log("Verified wrong identity/share/login refusal, source preservation, stopped-source recovery, retained old storage, revoked app tokens, fresh sessions/backend and idempotent rollback")
}
