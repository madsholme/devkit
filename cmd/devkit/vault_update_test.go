package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVaultUpdateRequiresBackupBeforeMutation(t *testing.T) {
	cfg := defaultVaultConfig()
	cfg.Image = "openbao/openbao:2.7.1"
	calls := 0
	swap(t, &runCommand, func([]string, runOpts) (string, error) { calls++; return "", nil })
	swap(t, &backupBeforeVaultUpdate, func() (string, error) { return "", errors.New("backup unavailable") })
	if _, e := applyVaultUpdate(cfg, "openbao/openbao:2.7.2", true); e == nil {
		t.Fatal("updated without backup")
	}
	if calls != 0 {
		t.Fatal("mutated before backup")
	}
	if _, e := applyVaultUpdate(cfg, "openbao/openbao:latest", true); e == nil {
		t.Fatal("accepted unpinned target")
	}
	if _, e := applyVaultUpdate(cfg, "openbao/openbao:2.6.3", true); e == nil {
		t.Fatal("accepted downgrade")
	}
	preview, e := applyVaultUpdate(cfg, "openbao/openbao:2.7.2", false)
	if e != nil || !strings.Contains(preview, "backup") || calls != 0 {
		t.Fatalf("preview %q %v calls=%d", preview, e, calls)
	}
}
func TestVaultBackupRejectsDataDirectoryAndMissingEncryption(t *testing.T) {
	cfg := defaultVaultConfig()
	cfg.Backup.Directory = secretStateDir()
	cfg.Backup.Recipient = "age1example"
	if e := validateBackupDestination(cfg); e == nil {
		t.Fatal("backup mixed with source state")
	}
	cfg.Backup.Directory = t.TempDir()
	cfg.Backup.Recipient = ""
	if e := validateBackupDestination(cfg); e == nil {
		t.Fatal("unencrypted backup allowed")
	}
}

func TestVaultWaitsForExpectedVersionAfterRestart(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"initialized":true,"sealed":true,"version":"2.7.2"}`)
	}))
	defer s.Close()
	c, _ := newVaultClient(s.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, e := waitVaultVersion(ctx, c, "2.7.2")
	if e != nil || got.State != "sealed" || calls < 2 {
		t.Fatalf("%+v %v calls=%d", got, e, calls)
	}
}

func TestVaultBackupRetentionOnlyPrunesOwnedVerifiedCopies(t *testing.T) {
	cfg := defaultVaultConfig()
	cfg.Backup.Directory = t.TempDir()
	cfg.Backup.Retain = 1
	for i, owner := range []string{"ours", "ours", "other"} {
		id := fmt.Sprintf("20261108T00000%d.000000000Z", i)
		dir := filepath.Join(cfg.Backup.Directory, id)
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
		m := vaultBackupManifest{ID: id, Installation: owner, Files: map[string]string{}}
		for _, name := range []string{"raft.snap.age", "host.json.age"} {
			content := []byte("encryptedfixture")
			if e := writePrivate(filepath.Join(dir, name), content); e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(content)
			m.Files[name] = hex.EncodeToString(sum[:])
		}
		b, _ := json.Marshal(m)
		if e := writePrivate(filepath.Join(dir, "manifest.json"), b); e != nil {
			t.Fatal(e)
		}
	}
	if e := pruneVaultBackups(cfg, "ours"); e != nil {
		t.Fatal(e)
	}
	if exists(filepath.Join(cfg.Backup.Directory, "20261108T000000.000000000Z")) {
		t.Fatal("old owned backup retained")
	}
	if !exists(filepath.Join(cfg.Backup.Directory, "20261108T000001.000000000Z")) || !exists(filepath.Join(cfg.Backup.Directory, "20261108T000002.000000000Z")) {
		t.Fatal("new or foreign backup deleted")
	}
}
