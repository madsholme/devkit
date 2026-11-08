package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

func lockSecretState() (func(), error) {
	dir := secretStateDir()
	if e := privateDir(dir); e != nil {
		return nil, e
	}
	fd, e := syscall.Open(filepath.Join(dir, "lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "lock")
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fail("Another secrets operation is in progress")
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
}
func validateBackupDestination(c vaultConfig) error {
	if !strings.HasPrefix(c.Backup.Recipient, "age1") || strings.ContainsAny(c.Backup.Recipient, " \r\n\t") {
		return fail("Configure an age encryption recipient before backing up")
	}
	dir := filepath.Clean(c.Backup.Directory)
	if !filepath.IsAbs(dir) {
		return fail("Configure an absolute off-host backup directory (for example a mounted NAS)")
	}
	for _, root := range []string{stateDir(), configDir()} {
		relative, e := filepath.Rel(filepath.Clean(root), dir)
		if e == nil && (relative == "." || (!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != "..")) {
			return fail("Backup destination must be separate from Devkit config/state")
		}
	}
	return nil
}

type vaultBackupManifest struct {
	Installation string            `json:"installation"`
	ID           string            `json:"id"`
	Created      time.Time         `json:"created"`
	Image        string            `json:"image"`
	Files        map[string]string `json:"files"`
}

func encryptBackupPart(ctx context.Context, dir, name, recipient string, in io.Reader) error {
	target := filepath.Join(dir, name+".age")
	cmd := exec.CommandContext(ctx, "age", "-r", recipient, "-o", target)
	cmd.Stdin = in
	if e := cmd.Run(); e != nil {
		return fail("Backup encryption failed; check age installation and recipient")
	}
	return os.Chmod(target, 0600)
}
func createVaultBackup() (string, error) {
	unlock, e := lockSecretState()
	if e != nil {
		return "", e
	}
	defer unlock()
	return createVaultBackupLocked()
}
func createVaultBackupLocked() (id string, err error) {
	cfg, e := loadVaultConfig()
	if e != nil {
		return "", e
	}
	if e = validateBackupDestination(cfg); e != nil {
		return "", e
	}
	if _, e = exec.LookPath("age"); e != nil {
		return "", fail("Install age before creating encrypted backups")
	}
	if e = privateDir(cfg.Backup.Directory); e != nil {
		return "", e
	}
	id = time.Now().UTC().Format("20060102T150405.000000000Z")
	dir, e := os.MkdirTemp(cfg.Backup.Directory, ".incomplete-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(dir)
	c, e := managedVaultClient()
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	status, e := c.status(ctx)
	if e != nil || status.State != "ready" {
		return "", fail("OpenBao must be ready before backup")
	}
	registry, e := loadSecretRegistry()
	if e != nil {
		return "", e
	}
	if e = saveSecretRegistry(&registry); e != nil {
		return "", e
	}
	// Quiesce Traefik only when its existing project is present; absence is recorded.
	proxyID, e := probe("docker", "compose", "-f", filepath.Join(proxyDir(), "compose.yaml"), "ps", "-q", "traefik")
	proxyConfigured := exists(filepath.Join(proxyDir(), "compose.yaml"))
	if proxyConfigured && (e != nil || !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(proxyID)) {
		return "", fail("Cannot identify the configured Traefik container for a complete backup")
	}
	if proxyConfigured {
		if _, e = runCommand([]string{"docker", "stop", proxyID}, runOpts{capture: true, quiet: true}); e != nil {
			return "", fail("Could not pause Traefik for a consistent backup")
		}
		defer func() {
			if _, restartErr := runCommand([]string{"docker", "start", proxyID}, runOpts{capture: true, quiet: true}); restartErr != nil {
				err = fail("Backup captured but Traefik restart failed; restart the proxy explicitly")
			}
		}()
	}
	req, e := http.NewRequestWithContext(ctx, "GET", c.address+"/v1/sys/storage/raft/snapshot", nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("X-Vault-Token", c.token)
	resp, e := c.http.Do(req)
	if e != nil {
		return "", fail("OpenBao snapshot unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", vaultError{resp.StatusCode}
	}
	if e = encryptBackupPart(ctx, dir, "raft.snap", cfg.Backup.Recipient, resp.Body); e != nil {
		return "", e
	}
	for _, name := range []string{"registry.json", "cleanup.json"} {
		b, e := readPrivate(filepath.Join(secretStateDir(), name), 16<<20)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if e = encryptBackupPart(ctx, dir, name, cfg.Backup.Recipient, strings.NewReader(string(b))); e != nil {
			return "", e
		}
	}
	host, e := loadHostConfig()
	if e != nil {
		return "", e
	}
	metadata, _ := json.Marshal(map[string]any{"domain": host.Domain, "vault": cfg, "acme_included": proxyConfigured, "data_directory": filepath.Join(secretStateDir(), "openbao", "data")})
	if e = encryptBackupPart(ctx, dir, "host.json", cfg.Backup.Recipient, strings.NewReader(string(metadata))); e != nil {
		return "", e
	}
	if proxyConfigured {
		if host.Proxy == nil {
			return "", fail("Proxy configuration missing; refusing incomplete certificate backup")
		}
		// Container mounts are selected by the exact verified proxy ID, never by a user path.
		reader, writer := io.Pipe()
		cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "--entrypoint", "tar", "--volumes-from", proxyID+":ro", firstOf(host.Proxy.Image, defaultTraefik), "-C", "/certs", "-cf", "-", ".")
		cmd.Stdout = writer
		if e = cmd.Start(); e != nil {
			reader.Close()
			writer.Close()
			return "", fail("Cannot read the proxy certificate volume")
		}
		done := make(chan error, 1)
		go func() { e := cmd.Wait(); writer.CloseWithError(e); done <- e }()
		e = encryptBackupPart(ctx, dir, "acme.tar", cfg.Backup.Recipient, reader)
		reader.Close()
		captureErr := <-done
		if e != nil || captureErr != nil {
			return "", fail("Certificate backup failed")
		}
	}
	manifest := vaultBackupManifest{Installation: registry.Installation, ID: id, Created: time.Now().UTC(), Image: cfg.Image, Files: map[string]string{}}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return "", e
	}
	for _, entry := range entries {
		f, e := os.Open(filepath.Join(dir, entry.Name()))
		if e != nil {
			return "", e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return "", e
		}
		manifest.Files[entry.Name()] = hex.EncodeToString(h.Sum(nil))
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	if e = writePrivate(filepath.Join(dir, "manifest.json"), b); e != nil {
		return "", e
	}
	if e = os.Rename(dir, filepath.Join(cfg.Backup.Directory, id)); e != nil {
		return "", e
	}
	if e = pruneVaultBackups(cfg, registry.Installation); e != nil {
		return id, fail("Backup captured but retention failed; inspect backup directory")
	}
	happened("vault backup", id, "encrypted snapshot and recovery components; no recovery keys")
	return id, nil
}
func vaultBackupCLI(args []string) (int, error) {
	op := "create"
	if len(args) > 0 {
		op = args[0]
		args = args[1:]
	}
	pos, v, e := splitArgs(args, map[string]bool{"--directory": true, "--recipient": true, "--retain": true, "--daily": true}, map[string]bool{"--json": true})
	if e != nil {
		return 2, e
	}
	cfg, e := loadVaultConfig()
	if e != nil {
		return 1, e
	}
	switch op {
	case "configure":
		if len(pos) > 0 {
			return 2, fail("Unexpected backup argument")
		}
		if x, ok := v["--directory"]; ok {
			cfg.Backup.Directory = x
		}
		if x, ok := v["--recipient"]; ok {
			cfg.Backup.Recipient = x
		}
		if x, ok := v["--retain"]; ok {
			if _, e = fmt.Sscan(x, &cfg.Backup.Retain); e != nil {
				return 2, fail("Invalid backup retention")
			}
		}
		if x, ok := v["--daily"]; ok {
			if x != "on" && x != "off" {
				return 2, fail("--daily needs on or off")
			}
			cfg.Backup.Daily = x == "on"
		}
		if e = validateBackupDestination(cfg); e != nil {
			return 2, e
		}
		return 0, saveVaultConfig(cfg)
	case "create":
		if len(pos) > 0 {
			return 2, fail("Unexpected backup argument")
		}
		id, e := createVaultBackup()
		if e == nil {
			fmt.Fprintln(stdout, id)
		}
		return 0, e
	case "list":
		entries, e := os.ReadDir(cfg.Backup.Directory)
		if e != nil {
			return 1, e
		}
		var ids []string
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				ids = append(ids, entry.Name())
			}
		}
		sort.Strings(ids)
		return 0, json.NewEncoder(stdout).Encode(ids)
	case "verify":
		if len(pos) != 1 {
			return 2, fail("backup verify needs an ID")
		}
		return 0, verifyVaultBackup(cfg, pos[0])
	}
	return 2, fail("usage: devkit vault backup create|list|verify ID|configure --directory PATH --recipient AGE_RECIPIENT")
}
func verifyVaultBackup(cfg vaultConfig, id string) error {
	if e := verifyVaultBackupComponents(cfg, id); e != nil {
		return e
	}
	fmt.Fprintln(stdout, "Encrypted component checksums verified. This is not a decrypt/restore test.")
	return nil
}
func verifyVaultBackupComponents(cfg vaultConfig, id string) error {
	if !regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`).MatchString(id) {
		return fail("Invalid backup ID")
	}
	dir := filepath.Join(cfg.Backup.Directory, id)
	if st, e := os.Lstat(dir); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fail("Unsafe backup directory")
	}
	b, e := readPrivate(filepath.Join(dir, "manifest.json"), 65536)
	if e != nil {
		return e
	}
	var m vaultBackupManifest
	if json.Unmarshal(b, &m) != nil || m.ID != id || m.Files["raft.snap.age"] == "" || m.Files["host.json.age"] == "" {
		return fail("Incomplete backup manifest")
	}
	for name, want := range m.Files {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".age") {
			return fail("Invalid backup component")
		}
		fd, e := syscall.Open(filepath.Join(dir, name), syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return e
		}
		f := os.NewFile(uintptr(fd), name)
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return fail("Backup checksum mismatch")
		}
	}
	return nil
}

// Retention touches only verified snapshots from this installation and refuses unknown files.
func pruneVaultBackups(cfg vaultConfig, installation string) error {
	entries, e := os.ReadDir(cfg.Backup.Directory)
	if e != nil {
		return e
	}
	var owned []vaultBackupManifest
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		b, e := readPrivate(filepath.Join(cfg.Backup.Directory, entry.Name(), "manifest.json"), 65536)
		if e != nil {
			continue
		}
		var m vaultBackupManifest
		if json.Unmarshal(b, &m) != nil || m.Installation != installation || m.ID != entry.Name() {
			continue
		}
		if e = verifyVaultBackupComponents(cfg, m.ID); e != nil {
			return e
		}
		owned = append(owned, m)
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].ID > owned[j].ID })
	for i := cfg.Backup.Retain; i < len(owned); i++ {
		m := owned[i]
		dir := filepath.Join(cfg.Backup.Directory, m.ID)
		children, e := os.ReadDir(dir)
		if e != nil {
			return e
		}
		for _, entry := range children {
			if entry.Name() != "manifest.json" && m.Files[entry.Name()] == "" {
				return fail("Unknown files in backup; retention stopped")
			}
			if !entry.Type().IsRegular() {
				return fail("Unsafe backup component; retention stopped")
			}
		}
		for _, entry := range children {
			if e = os.Remove(filepath.Join(dir, entry.Name())); e != nil {
				return e
			}
		}
		if e = os.Remove(dir); e != nil {
			return e
		}
	}
	return nil
}
