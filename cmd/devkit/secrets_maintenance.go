package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func runSecretCleanup(ctx context.Context, now time.Time) error {
	unlock, e := lockSecretState()
	if e != nil {
		return e
	}
	defer unlock()
	cfg, e := loadCleanupConfig()
	if e != nil || !cfg.Automatic {
		return e
	}
	r, e := loadSecretRegistry()
	if e != nil {
		return e
	}
	c, e := managedVaultClient()
	if e != nil {
		return e
	}
	if r.Pending != nil {
		if r.Pending.Kind == "write" {
			return fail("Interrupted write needs explicit reconciliation")
		}
		if r.Pending.Kind == "cleanup-purge" {
			v := r.Resources[r.Pending.Resource]
			if v == nil || !cleanupDue(cfg, *v, now) {
				return nil
			}
		}
		if r.Pending.Kind == "purge-key" {
			key := r.Keys[secretKeyID(r.Pending.Resource, r.Pending.Key)]
			resource := r.Resources[r.Pending.Resource]
			if key == nil || resource == nil {
				return fail("Pending key purge lost its metadata")
			}
			candidate := *resource
			candidate.State = "retired"
			candidate.RetentionKind = ""
			candidate.Deadline = key.Deadline
			if !cleanupDue(cfg, candidate, now) {
				return nil
			}
		}
		return reconcileSecretDeletion(ctx, c, &r)
	}
	ids := make([]string, 0, len(r.Resources))
	for id := range r.Resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := r.Resources[id]
		if !cleanupDue(cfg, *v, now) {
			continue
		}
		affected := affectedSecretResources(&r, id)
		allowed := true
		for _, child := range affected {
			if !cleanupDue(cfg, *child, now) {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}
		if v.Kind == "branch" {
			if e = checkActiveBranchReferences(&r, v); e != nil {
				return e
			}
		}
		if e = secretAudit("automatic-purge", id, ""); e != nil {
			return e
		}
		r.Pending = &secretOperation{ID: secretID(), Kind: "cleanup-purge", Resource: id}
		if e = saveSecretRegistry(&r); e != nil {
			return e
		}
		if e = reconcileSecretDeletion(ctx, c, &r); e != nil {
			return e
		}
	}
	for _, key := range r.Keys {
		v := r.Resources[key.Resource]
		if v == nil || !key.Deleted || v.State != "active" {
			continue
		}
		candidate := *v
		candidate.State = "retired"
		candidate.RetentionKind = ""
		candidate.Deadline = key.Deadline
		if !cleanupDue(cfg, candidate, now) {
			continue
		}
		if e = secretAudit("automatic-key-purge", v.ID, key.Name); e != nil {
			return e
		}
		r.Pending = &secretOperation{ID: secretID(), Kind: "purge-key", Resource: v.ID, Key: key.Name}
		if e = saveSecretRegistry(&r); e != nil {
			return e
		}
		if e = reconcileSecretDeletion(ctx, c, &r); e != nil {
			return e
		}
	}
	return nil
}

type secretMaintenanceState struct {
	LastBackup time.Time `json:"last_backup,omitempty"`
	LastRenew  time.Time `json:"last_renew,omitempty"`
	Error      string    `json:"error,omitempty"`
}

func runSecretsMaintenance(ctx context.Context) {
	cfg, e := loadVaultConfig()
	if e != nil || cfg.Provider != "openbao" {
		return
	}
	now := time.Now().UTC()
	path := filepath.Join(secretStateDir(), "maintenance.json")
	var state secretMaintenanceState
	if b, e := readPrivate(path, 65536); e == nil {
		_ = json.Unmarshal(b, &state)
	}
	state.Error = ""
	if e = runSecretCleanup(ctx, now); e != nil {
		state.Error = e.Error()
	}
	if now.Sub(state.LastRenew) >= time.Hour {
		if c, e := managedVaultClient(); e == nil {
			if e = c.call(ctx, "POST", "auth/token/renew-self", map[string]string{"increment": "24h"}, nil); e == nil {
				state.LastRenew = now
			} else {
				state.Error = "Backend credential renewal failed; check vault status and human provisioning"
			}
		}
	}
	if cfg.Backup.Daily && cfg.Backup.Directory != "" && now.Sub(state.LastBackup) >= 24*time.Hour {
		if _, e = createVaultBackup(); e == nil {
			state.LastBackup = now
		} else {
			state.Error = e.Error()
		}
	}
	b, _ := json.Marshal(state)
	_ = writePrivate(path, b)
}
func startSecretsMaintenance() func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runSecretsMaintenance(ctx)
			}
		}
	}()
	return cancel
}

// A local status report never prints values or backend credentials.
func secretsMaintenanceStatus() string {
	b, e := readPrivate(filepath.Join(secretStateDir(), "maintenance.json"), 65536)
	if os.IsNotExist(e) {
		return "Not yet checked"
	}
	if e != nil {
		return "Maintenance state unavailable"
	}
	var s secretMaintenanceState
	if json.Unmarshal(b, &s) != nil {
		return "Maintenance state invalid"
	}
	if s.Error != "" {
		return s.Error
	}
	return fmt.Sprintf("Last successful backup: %s", s.LastBackup.Format(time.RFC3339))
}
