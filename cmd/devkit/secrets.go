package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type secretRequest struct {
	secretEntryMeta
	Username  string             `json:"username,omitempty"`
	Password  string             `json:"password,omitempty"`
	Field     string             `json:"field,omitempty"`
	ClearEnv  bool               `json:"clear_env,omitempty"`
	Service   string             `json:"service,omitempty"`
	Image     string             `json:"image,omitempty"`
	Backup    *vaultBackupConfig `json:"backup,omitempty"`
	Operation string             `json:"operation"`
	Root      string             `json:"root,omitempty"`
	Project   string             `json:"project,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Worktree  string             `json:"worktree,omitempty"`
	Scope     string             `json:"scope,omitempty"`
	Resource  string             `json:"resource,omitempty"`
	Key       string             `json:"key,omitempty"`
	Value     string             `json:"value,omitempty"`
	Expected  int                `json:"expected"`
	Length    int                `json:"length,omitempty"`
	Reveal    bool               `json:"reveal,omitempty"`
	Confirm   string             `json:"confirm,omitempty"`
	Cleanup   *cleanupConfig     `json:"cleanup,omitempty"`
}
type secretRecord struct {
	secretEntryMeta
	Name      string     `json:"name"`
	Resource  string     `json:"resource"`
	Scope     string     `json:"scope"`
	Inherited bool       `json:"inherited"`
	Effective bool       `json:"effective"`
	Version   int        `json:"version"`
	Updated   time.Time  `json:"updated"`
	Deleted   bool       `json:"deleted"`
	Deadline  *time.Time `json:"deadline,omitempty"`
}
type secretResponse struct {
	Counts       map[string]int    `json:"counts,omitempty"`
	Management   map[string]any    `json:"management,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
	Context      secretContext     `json:"context"`
	Records      []secretRecord    `json:"records"`
	Resources    []*secretResource `json:"resources,omitempty"`
	Value        string            `json:"value,omitempty"`
	Confirmation string            `json:"confirmation,omitempty"`
	Message      string            `json:"message,omitempty"`
	Cleanup      *cleanupConfig    `json:"cleanup,omitempty"`
	History      any               `json:"history,omitempty"`
}

var secretsNow = time.Now

func secretAudit(operation, resource, key string) error {
	if e := privateDir(secretStateDir()); e != nil {
		return e
	}
	fd, e := syscall.Open(filepath.Join(secretStateDir(), "audit.jsonl"), syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return fail("Secret audit unavailable")
	}
	f := os.NewFile(uintptr(fd), "audit")
	defer f.Close()
	b, _ := json.Marshal(map[string]any{"at": secretsNow().UTC(), "operation": operation, "resource": resource, "key": key, "actor": "host-owner"})
	if _, e = f.Write(append(b, '\n')); e != nil {
		return fail("Secret audit unavailable")
	}
	return f.Sync()
}
func executeSecret(ctx context.Context, q secretRequest) (secretResponse, error) {
	out := secretResponse{Records: []secretRecord{}}
	if operatorSecretOperation(q.Operation) && !strings.HasPrefix(q.Operation, "cleanup-") {
		return executeSecretOperator(ctx, q)
	}
	unlock, e := lockSecretState()
	if e != nil {
		return out, e
	}
	defer unlock()
	r, e := loadSecretRegistry()
	if e != nil {
		return out, e
	}
	if strings.HasPrefix(q.Operation, "cleanup-") {
		return executeCleanup(ctx, &r, q)
	}
	provider, e := loadVaultConfig()
	if e != nil {
		return out, e
	}
	if provider.Provider != "openbao" {
		return out, fail("Managed secrets are disabled; select OpenBao in setup")
	}
	c, e := inspectSecretCheckout(q.Root)
	if e != nil {
		return out, e
	}
	if q.Operation == "register" {
		out.Context, e = registerSecretContext(&r, c)
		if e != nil {
			return out, e
		}
		if e = secretAudit("register", out.Context.Project, ""); e != nil {
			return out, e
		}
		e = saveSecretRegistry(&r)
		return out, e
	}
	if q.Operation == "bind" {
		return bindSecretResource(&r, c, q)
	}
	out.Context, e = resolveSecretContext(&r, c, q)
	if e != nil {
		return out, e
	}
	if q.Operation == "context" || q.Operation == "resources" {
		out.Counts = map[string]int{}
		for _, v := range r.Resources {
			if v.Project == out.Context.Project {
				out.Resources = append(out.Resources, v)
				if v.State == "active" {
					out.Counts[v.ID] = 0
				}
			}
		}
		for _, key := range r.Keys {
			if _, active := out.Counts[key.Resource]; active && !key.Deleted {
				out.Counts[key.Resource]++
			}
		}
		sort.Slice(out.Resources, func(i, j int) bool { return out.Resources[i].ID < out.Resources[j].ID })
		return out, nil
	}
	client, e := managedVaultClient()
	if e != nil {
		return out, e
	}
	if r.Pending != nil {
		if q.Operation != "reconcile" {
			return out, fail("An interrupted secret operation needs secrets resources reconcile before further access")
		}
		if e = reconcileSecretWrite(ctx, client, &r); e != nil {
			return out, e
		}
		out.Message = "Interrupted operation reconciled"
		return out, nil
	}
	if q.Operation == "reconcile" {
		observed, err := reconcileSecretInventory(&r, c, out.Context.Project)
		if err != nil {
			return out, err
		}
		out.Management = map[string]any{"reconciliation": observed}
		out.Message = "Inventory revalidated; missing or ambiguous resources are reported without retirement or Git changes"
		return out, nil
	}
	if q.Scope != "" && q.Scope != "project" && q.Scope != "branch" && q.Scope != "worktree" {
		return out, fail("Scope must be project, branch or worktree")
	}
	chain := contextResources(out.Context, q.Scope)
	selected := chain[0]
	if selected == "" {
		if q.Scope != "" {
			return out, fail("Selected scope is not registered or its branch binding is ambiguous")
		}
		for _, id := range chain {
			if id != "" {
				selected = id
				break
			}
		}
	}
	effective := map[string]*secretKey{}
	for _, id := range chain {
		if id == "" {
			continue
		}
		for _, k := range r.Keys {
			if k.Resource == id {
				source := r.Resources[id]
				if source == nil || source.State != "active" {
					continue
				}
				_, exists := effective[k.Name]
				if !k.Deleted && !exists {
					effective[k.Name] = k
				}
				out.Records = append(out.Records, secretRecord{secretEntryMeta: k.secretEntryMeta, Name: k.Name, Resource: id, Scope: source.Kind, Inherited: id != selected, Effective: !k.Deleted && !exists, Version: k.Version, Updated: k.Updated, Deleted: k.Deleted, Deadline: k.Deadline})
			}
		}
	}
	sort.Slice(out.Records, func(i, j int) bool {
		if out.Records[i].Name == out.Records[j].Name {
			return out.Records[i].Scope < out.Records[j].Scope
		}
		return out.Records[i].Name < out.Records[j].Name
	})
	if q.Operation == "inject" {
		out.Values = map[string]string{}
		if e = secretAudit("inject", selected, ""); e != nil {
			return out, e
		}
		out.Values, e = injectSecretEntries(ctx, client, &r, effective)
		if e != nil {
			return secretResponse{}, e
		}
		return out, nil
	}
	if q.Operation == "list" {
		return out, nil
	}
	if !vaultSegment.MatchString(q.Key) || q.Key == "." || q.Key == ".." {
		return out, fail("Invalid secret name")
	}
	key := r.Keys[secretKeyID(selected, q.Key)]
	switch q.Operation {
	case "get", "history":
		current := effective[q.Key]
		if current == nil {
			return out, fail("Secret not found in this context")
		}
		path, _ := resourceKVPath(r.Resources[current.Resource], q.Key)
		if q.Operation == "history" {
			var meta struct {
				Data struct {
					CurrentVersion int `json:"current_version"`
					Versions       map[string]struct {
						Created   string `json:"created_time"`
						Deleted   string `json:"deletion_time"`
						Destroyed bool   `json:"destroyed"`
					} `json:"versions"`
				} `json:"data"`
			}
			e = client.call(ctx, "GET", "kv/metadata/"+path, nil, &meta)
			out.History = meta.Data
			return out, e
		}
		if q.Reveal {
			if e = secretAudit("reveal", current.Resource, q.Key); e != nil {
				return out, e
			}
			var data secretEntryData
			data, e = readSecretEntry(ctx, client, path, current)
			if e != nil {
				return secretResponse{}, e
			}
			if data.Type == "login" {
				switch q.Field {
				case "username":
					out.Value = data.Username
				case "password":
					out.Value = data.Password
				default:
					return out, fail("Select --field username or --field password to reveal a login")
				}
			} else {
				if q.Field != "" && q.Field != "value" {
					return out, fail("Single-value secrets have only the value field")
				}
				out.Value = data.Value
			}
		}
		return out, e
	case "set", "update", "generate", "rotate":
		if q.Scope == "" {
			return out, fail("Select an explicit --scope before writing a secret")
		}
		if key != nil && key.Deleted {
			return out, fail("Restore the deleted secret before editing")
		}
		version := 0
		if key != nil {
			version = key.Version
		}
		if q.Expected != version {
			return out, fail("Secret version changed; refresh before updating")
		}
		if q.Operation == "update" || q.Operation == "rotate" {
			if key == nil {
				return out, fail("No owned secret to update; create an explicit override")
			}
		}
		path, e := resourceKVPath(r.Resources[selected], q.Key)
		if e != nil {
			return out, e
		}
		data, e := prepareSecretEntry(ctx, client, path, q, key, effective[q.Key])
		if e != nil {
			return out, e
		}

		// Refuse automatic KV eviction even when cleanup is disabled.
		if version >= 1000 {
			return out, fail("Version retention limit reached; archive/purge explicitly before another write")
		}
		if e = secretAudit(q.Operation, selected, q.Key); e != nil {
			return out, e
		}
		if data.Type == "login" {
			r.Schema = 2
		}
		r.Pending = &secretOperation{ID: secretID(), Kind: "write", Resource: selected, Key: q.Key, Expected: version}
		if e = saveSecretRegistry(&r); e != nil {
			return out, e
		}
		var result struct {
			Data struct {
				Version int `json:"version"`
			} `json:"data"`
		}
		data.OperationID = r.Pending.ID
		e = client.call(ctx, "POST", "kv/data/"+path, map[string]any{"options": map[string]int{"cas": version}, "data": data}, &result)
		if e != nil {
			return out, e
		}
		r.Keys[secretKeyID(selected, q.Key)] = &secretKey{secretEntryMeta: data.secretEntryMeta, Resource: selected, Name: q.Key, Version: result.Data.Version, Updated: secretsNow().UTC()}
		r.Pending = nil
		if e = saveSecretRegistry(&r); e != nil {
			return out, e
		}
		out.Message = "Secret saved; use explicit reveal or runtime injection"
		out.Records = nil
		return out, nil
	case "delete", "restore":
		return changeSecretKey(ctx, client, &r, q, out, selected)
	}
	return out, fail("Unknown secret operation")
}
func reconcileSecretWrite(ctx context.Context, c *vaultClient, r *secretRegistry) error {
	op := r.Pending
	if op == nil {
		return nil
	}
	if op.Kind != "write" {
		return reconcileSecretDeletion(ctx, c, r)
	}
	path, e := resourceKVPath(r.Resources[op.Resource], op.Key)
	if e != nil {
		return e
	}
	var got struct {
		Data struct {
			Data     secretEntryData `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	e = c.call(ctx, "GET", "kv/data/"+path, nil, &got)
	var ve vaultError
	if e != nil && (!errors.As(e, &ve) || ve.Code != 404) {
		return e
	}
	if e == nil && got.Data.Metadata.Version == op.Expected+1 && got.Data.Data.OperationID == op.ID {
		if e = validateEntryData(got.Data.Data); e != nil {
			return e
		}
		if got.Data.Data.Type == "login" {
			r.Schema = 2
		}
		r.Keys[secretKeyID(op.Resource, op.Key)] = &secretKey{secretEntryMeta: got.Data.Data.secretEntryMeta, Resource: op.Resource, Name: op.Key, Version: got.Data.Metadata.Version, Updated: secretsNow().UTC()}
	} else if !(e != nil && op.Expected == 0) && got.Data.Metadata.Version != op.Expected {
		return fail("Vault/registry conflict; preserve both and resolve explicitly")
	}
	r.Pending = nil
	return saveSecretRegistry(r)
}
func bindSecretResource(r *secretRegistry, c secretCheckout, q secretRequest) (secretResponse, error) {
	out := secretResponse{Records: []secretRecord{}}
	v := r.Resources[q.Resource]
	p := findSecretProject(r, c)
	if p == nil || v == nil || v.Project != p.ID || v.State != "active" {
		return out, fail("Binding requires a resource in this verified project")
	}
	for _, other := range r.Resources {
		if other.ID != v.ID && other.Project == p.ID && other.State != "destroyed" && other.Kind == v.Kind && ((v.Kind == "branch" && other.Name == c.Branch) || (v.Kind == "worktree" && other.Anchor == c.WorktreeAnchor)) {
			return out, fail("Target already has a secret identity; resolve the existing registration before rebinding")
		}
	}
	out.Confirmation = fmt.Sprintf("bind:%s:%d:%s", v.ID, r.Generation, digest(c.Root+"\n"+c.Branch+"\n"+c.WorktreeAnchor))
	if q.Confirm != out.Confirmation {
		out.Message = "Confirm binding to this existing Git resource; no Git changes"
		return out, nil
	}
	switch v.Kind {
	case "worktree":
		if v.Anchor != c.WorktreeAnchor {
			return out, fail("Worktree identity changed; register a new scope")
		}
		v.Path = c.Root
	case "branch":
		if c.Branch == "" {
			return out, fail("Checkout is detached")
		}
		v.Name = c.Branch
		v.RefAnchor = branchSecretAnchor(c, c.Branch)
	case "project":
		v.Path = c.Root
	default:
		return out, fail("Unknown resource")
	}
	if e := secretAudit("bind", v.ID, ""); e != nil {
		return out, e
	}
	return out, saveSecretRegistry(r)
}
