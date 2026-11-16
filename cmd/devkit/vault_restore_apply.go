package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

type restoreDirectoryID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type restoreSavedFile struct {
	Present bool   `json:"present"`
	Hash    string `json:"hash"`
}
type vaultRestoreJournal struct {
	Schema          int                         `json:"schema"`
	ID              string                      `json:"id"`
	Backup          string                      `json:"backup"`
	Installation    string                      `json:"installation"`
	Phase           string                      `json:"phase"`
	PreviousImage   string                      `json:"previous_image"`
	RestoredImage   string                      `json:"restored_image"`
	OldData         restoreDirectoryID          `json:"old_data"`
	NewData         restoreDirectoryID          `json:"new_data"`
	Files           map[string]restoreSavedFile `json:"files"`
	InstalledConfig string                      `json:"installed_config,omitempty"`
	SourceRunning   bool                        `json:"source_running"`
}

func restoreJournalPath() string { return filepath.Join(secretStateDir(), "restore.json") }
func restoreOperationRoot(id string) string {
	return filepath.Join(secretStateDir(), "restore-operations", id)
}
func vaultRestoreGuard() error {
	b, e := restoreReadOptional(restoreJournalPath(), 65536)
	if os.IsNotExist(e) {
		return nil
	}
	var j vaultRestoreJournal
	if e != nil || restoreDecode(b, &j) != nil || j.Schema != 1 || !restoreOperationPattern.MatchString(j.ID) {
		return fail("Vault recovery journal needs operator attention; normal secret operations are blocked")
	}
	if j.Phase != "complete" && j.Phase != "rolled-back" {
		return fail("Vault restore is incomplete; use vault backup restore --rollback OPERATION --yes")
	}
	return nil
}
func saveRestoreJournal(j *vaultRestoreJournal) error {
	if !restoreOperationPattern.MatchString(j.ID) {
		return fail("Invalid recovery operation")
	}
	if e := restorePrivateDirectory(restoreOperationRoot(j.ID)); e != nil {
		return e
	}
	b, e := json.MarshalIndent(j, "", "  ")
	if e != nil {
		return e
	}
	if e = writePrivate(filepath.Join(restoreOperationRoot(j.ID), "journal.json"), b); e != nil {
		return e
	}
	return writePrivate(restoreJournalPath(), b)
}
func restoreDataPath() string { return filepath.Join(secretStateDir(), "openbao", "data") }
func restoreFilePaths() map[string]string {
	return map[string]string{
		"registry": filepath.Join(secretStateDir(), "registry.json"), "cleanup": filepath.Join(secretStateDir(), "cleanup.json"),
		"backend": filepath.Join(vaultDir(), "backend-token"), "config": configPath(),
		"compose": filepath.Join(vaultDir(), "openbao", "compose.json"), "server": filepath.Join(vaultDir(), "openbao", "server.json"),
	}
}
func restoreFileHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func restoreDirIdentity(path string) (restoreDirectoryID, error) {
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return restoreDirectoryID{}, nil
	}
	if e != nil {
		return restoreDirectoryID{}, fail("Cannot inspect recovery storage")
	}
	if restoreRealPath(path) != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return restoreDirectoryID{}, fail("Recovery data directory is unsafe")
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(s.Uid) != os.Getuid() {
		return restoreDirectoryID{}, fail("Recovery data directory ownership changed")
	}
	return restoreDirectoryID{uint64(s.Dev), s.Ino}, nil
}
func restoreRename(from, to string) error {
	if _, e := os.Lstat(to); !os.IsNotExist(e) {
		return fail("Recovery rename destination already exists")
	}
	if e := os.Rename(from, to); e != nil {
		return fail("Recovery directory rename failed; preserve both data sets")
	}
	for _, p := range []string{filepath.Dir(from), filepath.Dir(to)} {
		d, e := os.Open(p)
		if e != nil {
			return e
		}
		e = d.Sync()
		d.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func restoreSaveFiles(j *vaultRestoreJournal) error {
	for _, path := range []string{configDir(), secretStateDir(), vaultDir(), filepath.Join(vaultDir(), "openbao"), filepath.Join(restoreOperationRoot(j.ID), "previous")} {
		if e := restorePrivateDirectory(path); e != nil {
			return e
		}
	}
	j.Files = map[string]restoreSavedFile{}
	for name, path := range restoreFilePaths() {
		b, e := restoreReadOptional(path, 16<<20)
		if os.IsNotExist(e) {
			j.Files[name] = restoreSavedFile{}
			continue
		}
		if e != nil || restoreRealPath(path) != nil {
			return fail("Existing recovery metadata is unsafe")
		}
		if e = writePrivate(filepath.Join(restoreOperationRoot(j.ID), "previous", name), b); e != nil {
			return e
		}
		j.Files[name] = restoreSavedFile{true, restoreFileHash(b)}
	}
	return nil
}
func restoreFilesUnchanged(j vaultRestoreJournal) error {
	for name, path := range restoreFilePaths() {
		before, ok := j.Files[name]
		if !ok {
			return fail("Incomplete recovery file journal")
		}
		b, e := restoreReadOptional(path, 16<<20)
		if !before.Present && os.IsNotExist(e) {
			continue
		}
		if e != nil || !before.Present || restoreFileHash(b) != before.Hash {
			return fail("Host metadata changed while preparing recovery")
		}
	}
	return nil
}
func restorePreviousFileData(j vaultRestoreJournal) (map[string][]byte, error) {
	// Verify the complete rollback set before writing any destination.
	data := map[string][]byte{}
	if e := restoreRealPath(restoreOperationRoot(j.ID)); e != nil {
		return nil, e
	}
	for _, path := range []string{configDir(), secretStateDir(), vaultDir()} {
		if e := restorePrivateDirectory(path); e != nil {
			return nil, e
		}
	}
	for name := range restoreFilePaths() {
		saved, ok := j.Files[name]
		if !ok {
			return nil, fail("Incomplete rollback file journal")
		}
		if saved.Present {
			b, e := restoreReadOptional(filepath.Join(restoreOperationRoot(j.ID), "previous", name), 16<<20)
			if e != nil || restoreFileHash(b) != saved.Hash {
				return nil, fail("Rollback metadata is missing or changed")
			}
			data[name] = b
		}
	}
	return data, nil
}
func restorePreviousFiles(j vaultRestoreJournal) error {
	data, e := restorePreviousFileData(j)
	if e != nil {
		return e
	}
	for name, path := range restoreFilePaths() {
		if j.Files[name].Present {
			if e := writePrivate(path, data[name]); e != nil {
				return e
			}
		} else {
			if st, e := os.Lstat(path); e == nil {
				if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
					return fail("Unsafe rollback destination")
				}
				if e = os.Remove(path); e != nil {
					return e
				}
			} else if !os.IsNotExist(e) {
				return e
			}
		}
	}
	return nil
}

// Kept as seams so acceptance uses real disposable Docker projects, never devkit-vault.
var restoreManagedProject = func() string { return "devkit-vault" }
var restoreWebStopped = func(ctx context.Context) error {
	state, e := restoreCommand(ctx, "systemctl", "--user", "show", "devkit-web", "--property=ActiveState", "--value")
	if e != nil || (state != "inactive" && state != "failed") {
		return fail("Stop devkit-web explicitly before restore/rollback; recovery never controls the web service")
	}
	return nil
}

func restoreManagedContainer(ctx context.Context, cfg vaultConfig) (container, error) {
	return restoreManagedContainerImages(ctx, []string{cfg.Image})
}
func restoreManagedContainerImages(ctx context.Context, images []string) (container, error) {
	rows, e := hostContainers(ctx)
	if e != nil {
		return container{}, fail("Cannot verify managed vault container ownership")
	}
	var found container
	for _, row := range rows {
		owned := row.Config.Labels["com.docker.compose.project"] == restoreManagedProject() && row.Config.Labels["com.docker.compose.service"] == "openbao"
		mounted := false
		for _, m := range row.Mounts {
			if m.Source == restoreDataPath() {
				mounted = true
			}
		}
		if !owned && !mounted {
			continue
		}
		if !owned || !mounted || found.ID != "" || !slices.Contains(images, row.Config.Image) || !fullContainerID.MatchString(row.ID) {
			return container{}, fail("Managed vault ownership or image is ambiguous")
		}
		dataOK, configOK := false, false
		for _, m := range row.Mounts {
			if m.Destination == "/bao/data" {
				dataOK = m.Type == "bind" && m.Source == restoreDataPath() && m.RW
			}
			if m.Destination == "/bao/config/server.json" {
				configOK = m.Type == "bind" && m.Source == filepath.Join(vaultDir(), "openbao", "server.json") && !m.RW
			}
		}
		if !dataOK || !configOK {
			return container{}, fail("Managed vault bind ownership does not match this host")
		}
		found = row
	}
	return found, nil
}
func startRestoredVault(ctx context.Context, cfg vaultConfig) error {
	compose, server := vaultFiles(cfg, filepath.Join(vaultDir(), "openbao"))
	compose["name"] = restoreManagedProject()
	for name, value := range map[string]any{"compose.json": compose, "server.json": server} {
		b, _ := json.MarshalIndent(value, "", "  ")
		if e := writePrivate(filepath.Join(vaultDir(), "openbao", name), b); e != nil {
			return e
		}
	}
	_, e := restoreCommand(ctx, "docker", "compose", "-f", filepath.Join(vaultDir(), "openbao", "compose.json"), "up", "-d", "--force-recreate", "openbao")
	return e
}
func verifySameHostRestore(cfg vaultConfig, b restoreBackup, c *restoreContents, installation string) error {
	if c.Host.DataDirectory != restoreDataPath() {
		return fail("Backup belongs to a different host data path; migration is not supported")
	}
	host, e := loadHostConfig()
	if e != nil || host.Domain != c.Host.Domain {
		return fail("Backup domain does not match this host")
	}
	raw, e := restoreReadOptional(filepath.Join(secretStateDir(), "registry.json"), 16<<20)
	var current secretRegistry
	if e == nil && json.Unmarshal(raw, &current) == nil && current.Installation != "" {
		if current.Installation != b.Manifest.Installation {
			return fail("Backup belongs to a different installation")
		}
	} else if installation != b.Manifest.Installation {
		return fail("Missing or damaged local registry: explicitly select --installation from the restore plan")
	}
	if installation != "" && installation != b.Manifest.Installation {
		return fail("Selected installation does not match backup")
	}
	return nil
}

var restoreCheckpoint = func(string) error { return nil } // failure injection, not an operator bypass
func restorePhase(j *vaultRestoreJournal, phase string) error {
	j.Phase = phase
	if e := saveRestoreJournal(j); e != nil {
		return e
	}
	return restoreCheckpoint(phase)
}
func applyVaultRestore(ctx context.Context, cfg vaultConfig, b restoreBackup, c *restoreContents, candidate *restoreCandidate, input *restoreCredentials, installation string) (string, error) {
	unlock, e := lockSecretState()
	if e != nil {
		return "", e
	}
	defer unlock()
	if e = restoreWebStopped(ctx); e != nil {
		return "", e
	}
	if e = verifySameHostRestore(cfg, b, c, installation); e != nil {
		return "", e
	}
	current, e := loadVaultConfig()
	if e != nil || current != cfg {
		return "", fail("Vault configuration changed during restore test")
	}
	latest, e := inspectRestoreBackup(cfg, b.Manifest.ID)
	if e != nil || latest.Fingerprint != b.Fingerprint {
		return "", fail("Backup changed during restore test")
	}
	source, e := restoreManagedContainer(ctx, cfg)
	if e != nil {
		return "", e
	}
	oldID, e := restoreDirIdentity(restoreDataPath())
	if e != nil {
		return "", e
	}
	newID, e := restoreDirIdentity(candidate.Data)
	if e != nil || newID.Inode == 0 {
		return "", fail("Tested candidate storage is unavailable")
	}
	if oldID.Inode != 0 && oldID.Device != newID.Device {
		return "", fail("Recovery staging must share the vault filesystem")
	}
	j := vaultRestoreJournal{Schema: 1, ID: candidate.Operation, Backup: b.Manifest.ID, Installation: b.Manifest.Installation, PreviousImage: cfg.Image, RestoredImage: b.Manifest.Image, OldData: oldID, NewData: newID, SourceRunning: source.State.Running}
	if e = restoreSaveFiles(&j); e != nil {
		return "", e
	}
	if e = restoreFilesUnchanged(j); e != nil {
		return "", e
	}
	// Stop/remove only the verified rehearsal, retaining its tested encrypted storage.
	if e = candidate.close(false); e != nil {
		return "", e
	}
	if e = restorePhase(&j, "prepared"); e != nil {
		return j.ID, e
	}
	if source.ID != "" && source.State.Running {
		if _, e = restoreCommand(ctx, "docker", "stop", source.ID); e != nil {
			return j.ID, e
		}
	}
	if e = restorePhase(&j, "source-stopped"); e != nil {
		return j.ID, e
	}
	checked, e := restoreManagedContainer(ctx, cfg)
	if e != nil || checked.ID != source.ID || checked.State.Running {
		return j.ID, fail("Vault container changed during recovery")
	}
	oldNow, e := restoreDirIdentity(restoreDataPath())
	if e != nil || oldNow != oldID {
		return j.ID, fail("Original data identity changed before replacement")
	}
	newNow, e := restoreDirIdentity(candidate.Data)
	if e != nil || newNow != newID {
		return j.ID, fail("Tested data identity changed before replacement")
	}
	if e = restoreFilesUnchanged(j); e != nil {
		return j.ID, e
	}
	if oldID.Inode != 0 {
		if e = restoreRename(restoreDataPath(), filepath.Join(restoreOperationRoot(j.ID), "previous-data")); e != nil {
			return j.ID, e
		}
	}
	if e = restorePhase(&j, "old-moved"); e != nil {
		return j.ID, e
	}
	if e = restoreRename(candidate.Data, restoreDataPath()); e != nil {
		return j.ID, e
	}
	if e = restorePhase(&j, "new-installed"); e != nil {
		return j.ID, e
	}
	c.Cleanup.Automatic = false
	c.Cleanup.ResumeAfter = secretsNow().UTC()
	for name, value := range map[string]any{"registry.json": c.Registry, "cleanup.json": c.Cleanup} {
		raw, _ := json.MarshalIndent(value, "", "  ")
		if e = writePrivate(filepath.Join(secretStateDir(), name), raw); e != nil {
			return j.ID, e
		}
	}
	if e = writePrivate(filepath.Join(vaultDir(), "backend-token"), []byte(candidate.Backend)); e != nil {
		return j.ID, e
	}
	// Authorization records are never loaded from the snapshot; all current app tokens are revoked.
	if e = saveSecretTokens(secretTokenStore{Schema: 1, Tokens: map[string]secretAPIToken{}}); e != nil {
		return j.ID, e
	}
	cfg.Image = b.Manifest.Image
	if e = saveVaultConfig(cfg); e != nil {
		return j.ID, e
	}
	raw, e := restoreReadOptional(configPath(), 16<<20)
	if e != nil {
		return j.ID, e
	}
	j.InstalledConfig = restoreFileHash(raw)
	if e = restorePhase(&j, "metadata-installed"); e != nil {
		return j.ID, e
	}
	if e = startRestoredVault(ctx, cfg); e != nil {
		return j.ID, e
	}
	target, _ := newVaultClient(cfg.Address, candidate.Backend)
	if e = waitRestoreState(ctx, target, "sealed"); e != nil {
		return j.ID, e
	}
	if e = restoreUnseal(ctx, target, input, true); e != nil {
		return j.ID, e
	}
	if e = verifyRestoredRegistry(ctx, target, c.Registry); e != nil {
		return j.ID, e
	}
	if e = restorePhase(&j, "complete"); e != nil {
		return j.ID, e
	}
	happened("vault restore", b.Manifest.ID, "verified recovery "+j.ID+"; previous data retained; cleanup off; app tokens revoked")
	return j.ID, nil
}
func rollbackVaultRestore(ctx context.Context, id string, input *restoreCredentials) (err error) {
	if !restoreOperationPattern.MatchString(id) {
		return fail("Invalid restore operation ID")
	}
	unlock, e := lockSecretStateForRestore()
	if e != nil {
		return e
	}
	defer unlock()
	if e = restoreWebStopped(ctx); e != nil {
		return e
	}
	raw, e := restoreReadOptional(restoreJournalPath(), 65536)
	var j vaultRestoreJournal
	if e != nil || restoreDecode(raw, &j) != nil || j.Schema != 1 || j.ID != id || j.Files == nil || !pinnedVaultImage.MatchString(j.PreviousImage) || !pinnedVaultImage.MatchString(j.RestoredImage) {
		return fail("Rollback journal is invalid or not the latest operation")
	}
	if j.Phase == "rolled-back" {
		return nil
	}
	live, e := restoreDirIdentity(restoreDataPath())
	if e != nil {
		return e
	}
	previous := filepath.Join(restoreOperationRoot(id), "previous-data")
	old, e := restoreDirIdentity(previous)
	if e != nil {
		return e
	}
	if live != j.NewData && live != j.OldData && live.Inode != 0 {
		return fail("Vault storage identity changed; rollback refused")
	}
	if j.OldData.Inode != 0 && old != j.OldData && live != j.OldData {
		return fail("Previous vault data is missing or changed")
	}
	if old.Inode != 0 && old != j.OldData {
		return fail("Unexpected previous vault data")
	}
	cfg, e := loadVaultConfig()
	if e != nil {
		return e
	}
	if cfg.Image != j.PreviousImage && cfg.Image != j.RestoredImage {
		return fail("Vault image changed outside restore")
	}
	if j.Phase == "complete" {
		raw, e := restoreReadOptional(configPath(), 16<<20)
		if e != nil || restoreFileHash(raw) != j.InstalledConfig {
			return fail("Host configuration changed after restore; automatic rollback refused")
		}
	}
	source, e := restoreManagedContainerImages(ctx, []string{j.PreviousImage, j.RestoredImage})
	if e != nil {
		return e
	}
	if _, e = restorePreviousFileData(j); e != nil {
		return e
	}
	if e = restoreLocalDocker(ctx); e != nil {
		return e
	}
	if e = restorePhase(&j, "rolling-back"); e != nil {
		return e
	}
	if source.ID != "" && source.State.Running {
		if _, e = restoreCommand(ctx, "docker", "stop", source.ID); e != nil {
			return e
		}
	}
	if live == j.NewData && live.Inode != 0 {
		if e = restoreRename(restoreDataPath(), filepath.Join(restoreOperationRoot(id), "replaced-data")); e != nil {
			return e
		}
	}
	if old.Inode != 0 && live != j.OldData {
		if e = restoreRename(previous, restoreDataPath()); e != nil {
			return e
		}
	}
	if e = restorePreviousFiles(j); e != nil {
		return e
	}
	// Never undo revocations on rollback, even if the failed apply had not yet revoked tokens.
	if e = saveSecretTokens(secretTokenStore{Schema: 1, Tokens: map[string]secretAPIToken{}}); e != nil {
		return e
	}
	if j.OldData.Inode != 0 {
		cfg, e = loadVaultConfig()
		if e != nil {
			return e
		}
		if e = startRestoredVault(ctx, cfg); e != nil {
			return e
		}
		client, _ := newVaultClient(cfg.Address, "")
		if e = waitRestoreState(ctx, client, "sealed"); e != nil {
			return e
		}
		if e = restoreUnseal(ctx, client, input, false); e != nil {
			return e
		}
		if e = restoreLogin(ctx, client, input); e != nil {
			return e
		}
		defer client.call(context.Background(), "POST", "auth/token/revoke-self", nil, nil)
		token, e := restoreBackend(ctx, client)
		if e != nil {
			return e
		}
		backend, _ := newVaultClient(cfg.Address, token)
		if e = restoreRotateSession(ctx, backend); e != nil {
			return e
		}
		if e = writePrivate(filepath.Join(vaultDir(), "backend-token"), []byte(token)); e != nil {
			return e
		}
		if !j.SourceRunning {
			row, e := restoreManagedContainer(ctx, cfg)
			if e != nil || row.ID == "" {
				return fail("Cannot preserve prior stopped state")
			}
			if _, e = restoreCommand(ctx, "docker", "stop", row.ID); e != nil {
				return e
			}
		}
	}
	cleanup, e := loadCleanupConfig()
	if e != nil {
		return e
	}
	cleanup.Automatic = false
	cleanup.ResumeAfter = secretsNow().UTC()
	if e = saveCleanupConfig(cleanup); e != nil {
		return e
	}
	if e = restorePhase(&j, "rolled-back"); e != nil {
		return e
	}
	happened("vault restore rollback", id, "previous data restored; replacement retained; cleanup off; app tokens revoked")
	return nil
}
