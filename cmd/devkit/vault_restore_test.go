package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func restoreFixture(t *testing.T) (vaultConfig, string) {
	t.Helper()
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	os.Chmod(configDir(), 0700)
	os.Chmod(stateDir(), 0700)
	cfg := defaultVaultConfig()
	cfg.Backup.Directory = t.TempDir()
	cfg.Backup.Recipient = "age1fixture"
	if e := saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	id := "20261116T120000.000000000Z"
	dir := filepath.Join(cfg.Backup.Directory, id)
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	m := vaultBackupManifest{ID: id, Installation: "0123456789abcdef0123456789abcdef", Image: cfg.Image, Files: map[string]string{}}
	for _, name := range []string{"raft.snap.age", "host.json.age", "registry.json.age", "cleanup.json.age"} {
		b := []byte("encrypted-fixture-" + name)
		sum := sha256.Sum256(b)
		m.Files[name] = hex.EncodeToString(sum[:])
		if e := os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := json.Marshal(m)
	if e := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	return cfg, id
}

func TestVaultRestoreDefaultIsReadOnlyPlan(t *testing.T) {
	_, id := restoreFixture(t)
	calls := 0
	swap(t, &runCommand, func([]string, runOpts) (string, error) { calls++; return "", nil })
	swap(t, &restoreCommand, func(context.Context, ...string) (string, error) { calls++; return "", nil })
	var out bytes.Buffer
	swap(t, &stdout, io.Writer(&out))
	code, e := vaultBackupCLI([]string{"restore", id})
	if e != nil || code != 0 {
		t.Fatalf("restore plan failed: code=%d error=%v", code, e)
	}
	if calls != 0 {
		t.Fatal("plan ran an external command")
	}
	if !bytes.Contains(out.Bytes(), []byte("isolated")) {
		t.Fatal("plan omitted required isolated test")
	}
	if _, e := os.Stat(filepath.Join(secretStateDir(), "restore.json")); !os.IsNotExist(e) {
		t.Fatal("plan changed restore state")
	}
}

func TestVaultRestoreRejectsUnsafeBackup(t *testing.T) {
	for _, name := range []string{"extra", "tamper", "missing", "symlink", "fifo", "public-mode", "bad-image"} {
		t.Run(name, func(t *testing.T) {
			cfg, id := restoreFixture(t)
			dir := filepath.Join(cfg.Backup.Directory, id)
			file := filepath.Join(dir, "raft.snap.age")
			switch name {
			case "extra":
				os.WriteFile(filepath.Join(dir, "unexpected"), []byte("x"), 0600)
			case "tamper":
				os.WriteFile(file, []byte("changed"), 0600)
			case "missing":
				os.Remove(filepath.Join(dir, "registry.json.age"))
			case "symlink":
				os.Remove(file)
				os.Symlink("/etc/passwd", file)
			case "fifo":
				os.Remove(file)
				if e := syscall.Mkfifo(file, 0600); e != nil {
					t.Fatal(e)
				}
			case "public-mode":
				os.Chmod(file, 0644)
			case "bad-image":
				b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
				var m vaultBackupManifest
				json.Unmarshal(b, &m)
				m.Image = "attacker/image:latest"
				b, _ = json.Marshal(m)
				os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
			}
			if _, e := inspectRestoreBackup(cfg, id); e == nil {
				t.Fatal("unsafe backup accepted")
			}
		})
	}
}
func TestVaultRestoreArchiveRejectsTraversalAndLinks(t *testing.T) {
	for _, entry := range []tar.Header{{Name: "../acme.json", Typeflag: tar.TypeReg, Mode: 0600}, {Name: "acme.json", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0600}, {Name: "acme.json", Typeflag: tar.TypeReg, Mode: 0644}} {
		var b bytes.Buffer
		w := tar.NewWriter(&b)
		w.WriteHeader(&entry)
		w.Close()
		if e := validateRestoreACME(b.Bytes()); e == nil {
			t.Fatal("unsafe archive accepted")
		}
	}
}
func TestVaultRestoreFlagsFailBeforeCommands(t *testing.T) {
	_, id := restoreFixture(t)
	calls := 0
	swap(t, &runCommand, func([]string, runOpts) (string, error) { calls++; return "", nil })
	swap(t, &restoreCommand, func(context.Context, ...string) (string, error) { calls++; return "", nil })
	for _, args := range [][]string{{}, {id, "--test"}, {id, "--yes"}, {id, "--test", "--yes", "--identity", "/x"}, {id, "--identity", "/x"}, {id, "--password", "SENTINEL"}} {
		if code, e := vaultRestoreCLI(args); code != 2 || e == nil {
			t.Fatalf("invalid restore arguments accepted: %v", args)
		}
	}
	if calls != 0 {
		t.Fatal("invalid invocation ran command")
	}
}

func TestVaultRestoreInterruptedJournalBlocksSecretLock(t *testing.T) {
	restoreFixture(t)
	if e := writePrivate(filepath.Join(secretStateDir(), "restore.json"), []byte(`{"phase":"replacing"}`)); e != nil {
		t.Fatal(e)
	}
	release, e := lockSecretState()
	if e == nil {
		release()
		t.Fatal("interrupted restore allowed normal secret operations")
	}
}

func TestVaultRestoreRetainsDestroyedResourceTombstones(t *testing.T) {
	cfg, id := restoreFixture(t)
	b, e := inspectRestoreBackup(cfg, id)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	p := "11111111111111111111111111111111"
	r := secretRegistry{Schema: 1, Installation: b.Manifest.Installation, Resources: map[string]*secretResource{p: {ID: p, Project: p, Kind: "project", State: "destroyed"}}, Keys: map[string]*secretKey{}}
	for name, v := range map[string]any{"registry.json": r, "cleanup.json": defaultCleanupConfig(), "host.json": restoreHostMetadata{Vault: cfg, DataDirectory: restoreDataPath()}} {
		raw, _ := json.Marshal(v)
		os.WriteFile(filepath.Join(root, name), raw, 0600)
	}
	if e = validateRestoreContents(&restoreContents{Root: root}, b); e != nil {
		t.Fatal(e)
	}
	for _, schema := range []int{2, 3} {
		r.Schema = schema
		raw, _ := json.Marshal(r)
		if e = os.WriteFile(filepath.Join(root, "registry.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
		e = validateRestoreContents(&restoreContents{Root: root}, b)
		if (e == nil) != (schema == 2) {
			t.Fatal("incorrect recovered registry schema validation")
		}
	}
}

func TestVaultRestoreGuardBlocksOtherMutations(t *testing.T) {
	restoreFixture(t)
	if e := writePrivate(restoreJournalPath(), []byte(`{"schema":1,"id":"0123456789abcdef0123456789abcdef","phase":"prepared"}`)); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"setup"}, {"init", "--shares", "1", "--threshold", "1"}, {"unseal"}, {"provision"}, {"update", "--check"}, {"backup", "configure", "--daily", "off"}} {
		if _, e := vaultCLI(args); e == nil || !strings.Contains(e.Error(), "incomplete") {
			t.Errorf("unguarded command %v: %v", args, e)
		}
	}
	runSecretsMaintenance(context.Background())
	if _, e := os.Stat(filepath.Join(secretStateDir(), "maintenance.json")); !os.IsNotExist(e) {
		t.Fatal("maintenance wrote during interrupted restore")
	}
}

func TestVaultRestoreCleanupFindsInterruptedContainer(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "retained"), []byte("encrypted data"), 0600)
	c := &restoreCandidate{Operation: strings.Repeat("a", 32), Root: root, Data: filepath.Join(root, "data")}
	id := strings.Repeat("b", 64)
	removed := false
	swap(t, &restoreCommand, func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "docker ps":
			return id, nil
		case "docker inspect":
			return `[{"Id":"` + id + `","Config":{"Labels":{"devkit.restore":"` + c.Operation + `"}},"Mounts":[{"Type":"bind","Source":"` + c.Data + `","Destination":"/bao/data"}]}]`, nil
		case "docker rm":
			removed = true
			return "", nil
		case "docker network":
			if len(args) > 2 && args[2] == "ls" {
				return "", nil
			}
		}
		return "", fail("unexpected command")
	})
	if e := c.close(true); e != nil {
		t.Fatal(e)
	}
	if !removed {
		t.Fatal("cleanup deleted storage without finding partially created container")
	}
}

func TestVaultRestoreRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{`{"id":"first","id":"second"}`, `{"files":{"x":"a","x":"b"}}`, `{} {}`} {
		var value any
		if e := restoreDecode([]byte(raw), &value); e == nil {
			t.Fatal("ambiguous metadata accepted")
		}
	}
}
func TestVaultRestoreRefusesUnsafeLocalJournal(t *testing.T) {
	restoreFixture(t)
	if e := privateDir(secretStateDir()); e != nil {
		t.Fatal(e)
	}
	if e := syscall.Mkfifo(restoreJournalPath(), 0600); e != nil {
		t.Fatal(e)
	}
	if e := vaultRestoreGuard(); e == nil {
		t.Fatal("FIFO journal accepted")
	}
}
func TestVaultRestoreLocalDockerOnly(t *testing.T) {
	for _, endpoint := range []string{"ssh://test-host", "tcp://127.0.0.1:2375", "unix:///var/run/docker.sock"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Setenv("DOCKER_HOST", "")
			swap(t, &restoreCommand, func(context.Context, ...string) (string, error) { return endpoint, nil })
			e := restoreLocalDocker(context.Background())
			if (e == nil) != strings.HasPrefix(endpoint, "unix:///") {
				t.Fatal("incorrect Docker endpoint validation")
			}
		})
	}
}

func TestVaultRestoreChangedBackupCannotReplaceSource(t *testing.T) {
	cfg, id := restoreFixture(t)
	b, e := inspectRestoreBackup(cfg, id)
	if e != nil {
		t.Fatal(e)
	}
	r := secretRegistry{Schema: 1, Installation: b.Manifest.Installation, Resources: map[string]*secretResource{}, Keys: map[string]*secretKey{}}
	if e = saveSecretRegistry(&r); e != nil {
		t.Fatal(e)
	}
	host, e := loadHostConfig()
	if e != nil {
		t.Fatal(e)
	}
	plain := &restoreContents{Registry: r, Host: restoreHostMetadata{Domain: host.Domain, DataDirectory: restoreDataPath()}}
	if e = os.WriteFile(filepath.Join(b.Directory, "raft.snap.age"), []byte("changed after rehearsal"), 0600); e != nil {
		t.Fatal(e)
	}
	swap(t, &restoreWebStopped, func(context.Context) error { return nil })
	swap(t, &hostContainers, func(context.Context) ([]container, error) {
		t.Fatal("changed backup reached source inventory")
		return nil, nil
	})
	operation, e := applyVaultRestore(context.Background(), cfg, b, plain, &restoreCandidate{}, nil, "")
	if e == nil || operation != "" {
		t.Fatal("changed backup passed replacement preflight")
	}
	if _, e = os.Stat(restoreJournalPath()); !os.IsNotExist(e) {
		t.Fatal("changed backup began replacement")
	}
}
func TestVaultRestoreLockContention(t *testing.T) {
	restoreFixture(t)
	unlock, e := lockSecretStateForRestore()
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if second, e := lockSecretStateForRestore(); e == nil {
		second()
		t.Fatal("competing recovery lock accepted")
	}
}
func TestVaultRestoreCleanupRefusesForeignContainer(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "retained")
	os.WriteFile(data, []byte("encrypted"), 0600)
	c := &restoreCandidate{Operation: strings.Repeat("a", 32), Root: root, Data: filepath.Join(root, "data")}
	id := strings.Repeat("b", 64)
	swap(t, &restoreCommand, func(_ context.Context, args ...string) (string, error) {
		if args[1] == "ps" {
			return id, nil
		}
		if args[1] == "inspect" {
			return `[{"Id":"` + id + `","Config":{"Labels":{"devkit.restore":"foreign"}}}]`, nil
		}
		t.Fatal("cleanup attempted mutation on foreign container")
		return "", nil
	})
	if e := c.close(true); e == nil {
		t.Fatal("foreign container cleanup accepted")
	}
	if _, e := os.Stat(data); e != nil {
		t.Fatal("uncertain storage removed")
	}
}

func TestVaultRestoreSameHostIdentity(t *testing.T) {
	cfg, id := restoreFixture(t)
	b, e := inspectRestoreBackup(cfg, id)
	if e != nil {
		t.Fatal(e)
	}
	host, e := loadHostConfig()
	if e != nil {
		t.Fatal(e)
	}
	c := &restoreContents{Host: restoreHostMetadata{Domain: host.Domain, DataDirectory: restoreDataPath()}}
	if e = verifySameHostRestore(cfg, b, c, ""); e == nil {
		t.Fatal("missing registry did not require explicit installation")
	}
	if e = verifySameHostRestore(cfg, b, c, b.Manifest.Installation); e != nil {
		t.Fatal(e)
	}
	if e = writePrivate(filepath.Join(secretStateDir(), "registry.json"), []byte(`{"installation":"another-installation"}`)); e != nil {
		t.Fatal(e)
	}
	if e = verifySameHostRestore(cfg, b, c, b.Manifest.Installation); e == nil {
		t.Fatal("explicit installation overrode different host identity")
	}
	os.Remove(filepath.Join(secretStateDir(), "registry.json"))
	c.Host.DataDirectory = filepath.Join(t.TempDir(), "other-host")
	if e = verifySameHostRestore(cfg, b, c, b.Manifest.Installation); e == nil {
		t.Fatal("replacement-host migration accepted")
	}
}
func TestVaultRestorePreservedCandidateStopsGracefully(t *testing.T) {
	c := &restoreCandidate{Operation: strings.Repeat("a", 32), Root: t.TempDir(), Data: "/tmp/synthetic-candidate"}
	id := strings.Repeat("b", 64)
	stopped := false
	swap(t, &restoreCommand, func(_ context.Context, args ...string) (string, error) {
		switch args[1] {
		case "ps":
			return id, nil
		case "inspect":
			return `[{"Id":"` + id + `","Config":{"Labels":{"devkit.restore":"` + c.Operation + `"}},"Mounts":[{"Type":"bind","Source":"` + c.Data + `","Destination":"/bao/data"}]}]`, nil
		case "stop":
			stopped = true
			return "", nil
		case "rm":
			if !stopped || slices.Contains(args, "-f") {
				t.Error("persisted candidate was killed instead of gracefully stopped")
			}
			return "", nil
		case "network":
			return "", nil
		}
		return "", fail("unexpected command")
	})
	if e := c.close(false); e != nil {
		t.Fatal(e)
	}
}

func TestVaultRestoreACMEProductionAndStagingStores(t *testing.T) {
	type entry struct {
		name, body string
		mode       int64
		kind       byte
	}
	valid := `{"dns":{"Certificates":[]}}`
	cases := []struct {
		name    string
		entries []entry
		accept  bool
	}{
		{"production with empty staging", []entry{{"./acme-staging.json", "", 0600, tar.TypeReg}, {"./acme.json", valid, 0600, tar.TypeReg}}, true},
		{"staging only", []entry{{"acme-staging.json", valid, 0600, tar.TypeReg}}, true},
		{"initialized empty production", []entry{{"acme.json", "", 0600, tar.TypeReg}}, true},
		{"both populated", []entry{{"acme.json", valid, 0600, tar.TypeReg}, {"acme-staging.json", valid, 0600, tar.TypeReg}}, true},
		{"duplicate alias", []entry{{"acme.json", valid, 0600, tar.TypeReg}, {"./acme.json", valid, 0600, tar.TypeReg}}, false},
		{"unknown file", []entry{{"acme.json", valid, 0600, tar.TypeReg}, {"other.json", valid, 0600, tar.TypeReg}}, false},
		{"staging traversal", []entry{{"../acme-staging.json", valid, 0600, tar.TypeReg}}, false},
		{"public staging", []entry{{"acme-staging.json", valid, 0644, tar.TypeReg}}, false},
		{"staging link", []entry{{"acme-staging.json", "", 0600, tar.TypeSymlink}}, false},
		{"invalid staging JSON", []entry{{"acme-staging.json", "invalid", 0600, tar.TypeReg}}, false},
		{"invalid staging certificate", []entry{{"acme-staging.json", `{"dns":{"Certificates":[{"Certificate":"eA==","Key":"eA=="}]}}`, 0600, tar.TypeReg}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			w := tar.NewWriter(&b)
			if e := w.WriteHeader(&tar.Header{Name: "./", Mode: 0755, Typeflag: tar.TypeDir}); e != nil {
				t.Fatal(e)
			}
			for _, v := range tc.entries {
				h := &tar.Header{Name: v.name, Mode: v.mode, Typeflag: v.kind, Size: int64(len(v.body))}
				if v.kind == tar.TypeSymlink {
					h.Linkname = "acme.json"
				}
				if e := w.WriteHeader(h); e != nil {
					t.Fatal(e)
				}
				if _, e := w.Write([]byte(v.body)); e != nil {
					t.Fatal(e)
				}
			}
			if e := w.Close(); e != nil {
				t.Fatal(e)
			}
			if e := validateRestoreACME(b.Bytes()); (e == nil) != tc.accept {
				t.Fatalf("accept=%v, error=%v", tc.accept, e)
			}
		})
	}
}
