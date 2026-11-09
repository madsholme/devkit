package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

func keyVersions(ctx context.Context, c *vaultClient, path string) ([]int, error) {
	var meta struct {
		Data struct {
			Versions map[string]struct {
				Destroyed bool `json:"destroyed"`
			} `json:"versions"`
		} `json:"data"`
	}
	if e := c.call(ctx, "GET", "kv/metadata/"+path, nil, &meta); e != nil {
		return nil, e
	}
	var versions []int
	for s, v := range meta.Data.Versions {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || len(versions) > 1000 {
			return nil, fail("Unexpected version metadata")
		}
		if !v.Destroyed {
			versions = append(versions, n)
		}
	}
	sort.Ints(versions)
	return versions, nil
}
func changeSecretKey(ctx context.Context, c *vaultClient, r *secretRegistry, q secretRequest, out secretResponse, selected string) (secretResponse, error) {
	k := r.Keys[secretKeyID(selected, q.Key)]
	if k == nil {
		return out, fail("This scope does not own the key; choose its owner explicitly")
	}
	out.Confirmation = fmt.Sprintf("%s:%s:%s:%d", q.Operation, selected, q.Key, r.Generation)
	if q.Confirm != out.Confirmation {
		out.Message = "Confirm the selected owned key; inherited values are unchanged"
		return out, nil
	}
	cfg, e := loadCleanupConfig()
	if e != nil {
		return out, e
	}
	path, e := resourceKVPath(r.Resources[selected], q.Key)
	if e != nil {
		return out, e
	}
	if q.Operation == "delete" {
		if k.Deleted {
			return out, fail("Key is already deleted")
		}
		k.Versions, e = keyVersions(ctx, c, path)
		if e != nil {
			return out, e
		}
		k.Deadline, e = retentionDeadline(cfg, r.Resources[selected].Kind, secretsNow())
		if e != nil {
			return out, e
		}
	} else if !k.Deleted {
		return out, fail("Key is not deleted")
	}
	r.Pending = &secretOperation{ID: secretID(), Kind: q.Operation + "-key", Resource: selected, Key: q.Key}
	if e = secretAudit(q.Operation, selected, q.Key); e != nil {
		return out, e
	}
	if e = saveSecretRegistry(r); e != nil {
		return out, e
	}
	e = reconcileSecretDeletion(ctx, c, r)
	out.Message = "Owned key " + q.Operation + " completed"
	return out, e
}
func affectedSecretResources(r *secretRegistry, id string) []*secretResource {
	var resources []*secretResource
	v := r.Resources[id]
	if v == nil {
		return resources
	}
	for _, item := range r.Resources {
		if item.State != "destroyed" && (item.ID == id || (v.Kind == "project" && item.Project == id)) {
			resources = append(resources, item)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	return resources
}
func checkActiveBranchReferences(r *secretRegistry, v *secretResource) error {
	if v.Kind != "branch" {
		return nil
	}
	p := r.Resources[v.Project]
	if p == nil {
		return fail("Project registration missing")
	}
	trees, e := worktreesOf(p.Path)
	if e != nil {
		return fail("Cannot verify active branch references")
	}
	for _, tree := range trees {
		if tree.Branch == v.Name {
			return fail("An existing worktree still uses this branch; switch/remove it using Git first")
		}
	}
	return nil
}
func executeCleanup(ctx context.Context, r *secretRegistry, q secretRequest) (secretResponse, error) {
	out := secretResponse{Records: []secretRecord{}}
	cfg, e := loadCleanupConfig()
	if e != nil {
		return out, e
	}
	out.Cleanup = &cfg
	switch q.Operation {
	case "cleanup-config":
		if q.Cleanup == nil {
			return out, nil
		}
		next := *q.Cleanup
		if e = validateCleanupConfig(next); e != nil {
			return out, e
		}
		// A caller cannot erase the resume barrier by omitting it from a JSON request.
		next.ResumeAfter = cfg.ResumeAfter
		if !cfg.Automatic && next.Automatic {
			next.ResumeAfter = secretsNow().UTC()
		}
		out.Confirmation = fmt.Sprintf("cleanup-config:%d:%s:%s", r.Generation, cleanupConfigDigest(cfg), cleanupPolicyDigest(next))
		if q.Confirm != out.Confirmation {
			out.Cleanup = &next
			out.Message = "New retention applies to new retirements. Existing deadlines remain protected; overdue purge requires a separate resume confirmation."
			return out, nil
		}
		if e = secretAudit("cleanup-config", "", ""); e != nil {
			return out, e
		}
		e = saveCleanupConfig(next)
		out.Cleanup = &next
		return out, e
	case "cleanup-resume":
		out.Confirmation = fmt.Sprintf("cleanup-resume:%d:%s", r.Generation, cleanupConfigDigest(cfg))
		for _, v := range r.Resources {
			if v.State == "retired" && v.Deadline != nil && !cfg.ResumeAfter.IsZero() && !v.Deadline.After(cfg.ResumeAfter) {
				out.Resources = append(out.Resources, v)
			}
		}
		for _, key := range r.Keys {
			if key.Deleted && key.Deadline != nil && !cfg.ResumeAfter.IsZero() && !key.Deadline.After(cfg.ResumeAfter) {
				out.Records = append(out.Records, secretRecord{secretEntryMeta: key.secretEntryMeta, Name: key.Name, Resource: key.Resource, Scope: r.Resources[key.Resource].Kind, Version: key.Version, Deleted: true, Deadline: key.Deadline})
			}
		}
		if q.Confirm != out.Confirmation {
			out.Message = "Confirm allowing automatic purge of overdue retired secrets (subject to current retention settings)"
			return out, nil
		}
		if !cfg.Automatic {
			return out, fail("Enable automatic cleanup before resuming")
		}
		cfg.ResumeAfter = time.Time{}
		if e = secretAudit("cleanup-resume", "", ""); e != nil {
			return out, e
		}
		return out, saveCleanupConfig(cfg)
	case "cleanup-reconcile":
		if r.Pending == nil {
			out.Message = "No interrupted operation"
			return out, nil
		}
		c, e := managedVaultClient()
		if e != nil {
			return out, e
		}
		return out, reconcileSecretWrite(ctx, c, r)
	case "cleanup-status":
		for _, v := range r.Resources {
			if v.State != "active" {
				out.Resources = append(out.Resources, v)
			}
		}
		return out, nil
	}
	if r.Pending != nil {
		return out, fail("Reconcile interrupted operations before starting cleanup")
	}
	v := r.Resources[q.Resource]
	if v == nil || v.State == "destroyed" {
		return out, fail("Unknown or permanently deleted resource")
	}
	affected := affectedSecretResources(r, v.ID)
	out.Resources = affected
	ids := map[string]bool{}
	for _, item := range affected {
		ids[item.ID] = true
	}
	for _, k := range r.Keys {
		if ids[k.Resource] {
			out.Records = append(out.Records, secretRecord{secretEntryMeta: k.secretEntryMeta, Name: k.Name, Resource: k.Resource, Scope: r.Resources[k.Resource].Kind, Version: k.Version, Deleted: k.Deleted, Deadline: k.Deadline})
		}
	}
	op := q.Operation
	out.Confirmation = fmt.Sprintf("%s:%s:%d:%s", op, v.ID, r.Generation, cleanupConfigDigest(cfg))
	if op == "cleanup-preview" {
		out.Message = "Owned secrets only; retirement never deletes Git/files"
		return out, nil
	}
	if op != "cleanup-retire" && op != "cleanup-restore" && op != "cleanup-purge" {
		return out, fail("Unknown cleanup operation")
	}
	if op == "cleanup-retire" {
		if v.State != "active" {
			return out, fail("Resource is already retired")
		}
		if e = checkActiveBranchReferences(r, v); e != nil {
			return out, e
		}
	}
	if op == "cleanup-retire" && q.Confirm != out.Confirmation {
		deadline, err := retentionDeadline(cfg, v.Kind, secretsNow())
		if err != nil {
			return out, err
		}
		out.Resources = nil
		for _, item := range affected {
			copy := *item
			extendRetirement(&copy, deadline)
			out.Resources = append(out.Resources, &copy)
		}
	}
	if q.Confirm != out.Confirmation {
		out.Message = "Confirm owned secret " + op + "; no source or Git files change"
		return out, nil
	}
	if op == "cleanup-purge" {
		for _, item := range affected {
			if item.Kind == "branch" {
				if e = checkActiveBranchReferences(r, item); e != nil {
					return out, e
				}
			}
		}
	}
	if e = secretAudit(op, v.ID, ""); e != nil {
		return out, e
	}
	if op == "cleanup-retire" {
		deadline, e := retentionDeadline(cfg, v.Kind, secretsNow())
		if e != nil {
			return out, e
		}
		now := secretsNow().UTC()
		for _, item := range affected {
			extendRetirement(item, deadline)
			if item.State == "active" {
				item.RetiredBy = v.ID
			}
			item.State = "retiring"
			item.RetiredAt = &now
			item.RetentionKind = v.Kind
		}
	}
	r.Pending = &secretOperation{ID: secretID(), Kind: op, Resource: v.ID}
	if e = saveSecretRegistry(r); e != nil {
		return out, e
	}
	c, e := managedVaultClient()
	if e != nil {
		return out, e
	}
	e = reconcileSecretDeletion(ctx, c, r)
	return out, e
}
func cleanupConfigDigest(c cleanupConfig) string { b, _ := json.Marshal(c); return digest(string(b)) }
func reconcileSecretDeletion(ctx context.Context, c *vaultClient, r *secretRegistry) error {
	op := r.Pending
	if op == nil {
		return nil
	}
	var keys []*secretKey
	affected := affectedSecretResources(r, op.Resource)
	ids := map[string]bool{}
	for _, v := range affected {
		ids[v.ID] = true
	}
	if op.Kind == "delete-key" || op.Kind == "restore-key" || op.Kind == "purge-key" {
		k := r.Keys[secretKeyID(op.Resource, op.Key)]
		if k == nil {
			return fail("Pending operation lost its owned key")
		}
		keys = append(keys, k)
	} else {
		for _, k := range r.Keys {
			if ids[k.Resource] {
				keys = append(keys, k)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return secretKeyID(keys[i].Resource, keys[i].Name) < secretKeyID(keys[j].Resource, keys[j].Name)
	})
	for _, k := range keys {
		path, e := resourceKVPath(r.Resources[k.Resource], k.Name)
		if e != nil {
			return e
		}
		switch op.Kind {
		case "delete-key", "cleanup-retire":
			if k.Deleted {
				continue
			}
			if len(k.Versions) == 0 {
				k.Versions, e = keyVersions(ctx, c, path)
				if e != nil {
					return e
				}
				if e = saveSecretRegistry(r); e != nil {
					return e
				}
			}
			if e = c.call(ctx, "POST", "kv/delete/"+path, map[string]any{"versions": k.Versions}, nil); e != nil {
				return e
			}
			k.Deleted = true
			if op.Kind == "cleanup-retire" {
				k.DeletedBy = op.Resource
			} else {
				k.DeletedBy = ""
			}
			if op.Kind == "cleanup-retire" {
				k.Deadline = r.Resources[k.Resource].Deadline
			}
		case "restore-key", "cleanup-restore":
			if op.Kind == "cleanup-restore" && k.DeletedBy != op.Resource {
				continue
			}
			if !k.Deleted {
				continue
			}
			if e = c.call(ctx, "POST", "kv/undelete/"+path, map[string]any{"versions": k.Versions}, nil); e != nil {
				return e
			}
			k.Deleted = false
			k.DeletedBy = ""
			k.Deadline = nil
			k.Versions = nil
		case "cleanup-purge", "purge-key":
			// Metadata deletion permanently removes every version, including versions added by native admin.
			if e = c.call(ctx, "DELETE", "kv/metadata/"+path, nil, nil); e != nil {
				return e
			}
			delete(r.Keys, secretKeyID(k.Resource, k.Name))
		default:
			return fail("Unknown pending cleanup operation")
		}
		if e = saveSecretRegistry(r); e != nil {
			return e
		}
	}
	if op.Kind == "cleanup-retire" {
		for _, v := range affected {
			v.State = "retired"
		}
	}
	if op.Kind == "cleanup-restore" {
		for _, v := range affected {
			if v.RetiredBy != op.Resource {
				continue
			}
			v.RetiredBy = ""
			v.State = "active"
			v.Deadline = nil
			v.RetiredAt = nil
			v.RetentionKind = ""
		}
	}
	if op.Kind == "cleanup-purge" {
		for _, v := range affected {
			v.State = "destroyed"
			v.Deadline = nil
		}
	}
	r.Pending = nil
	return saveSecretRegistry(r)
}

func cleanupPolicyDigest(c cleanupConfig) string {
	c.ResumeAfter = time.Time{}
	return cleanupConfigDigest(c)
}
