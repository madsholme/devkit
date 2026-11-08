package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type secretResource struct {
	RetiredBy     string     `json:"retired_by,omitempty"`
	RetentionKind string     `json:"retention_kind,omitempty"`
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Name          string     `json:"name"`
	Project       string     `json:"project"`
	Branch        string     `json:"branch,omitempty"`
	Path          string     `json:"path,omitempty"`
	Anchor        string     `json:"anchor,omitempty"`
	RefAnchor     string     `json:"ref_anchor,omitempty"`
	State         string     `json:"state"`
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
	Deadline      *time.Time `json:"deadline,omitempty"`
}
type secretKey struct {
	DeletedBy string     `json:"deleted_by,omitempty"`
	Resource  string     `json:"resource"`
	Name      string     `json:"name"`
	Version   int        `json:"version"`
	Updated   time.Time  `json:"updated"`
	Deleted   bool       `json:"deleted"`
	Deadline  *time.Time `json:"deadline,omitempty"`
	Versions  []int      `json:"versions,omitempty"`
}
type secretOperation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Resource string `json:"resource"`
	Key      string `json:"key,omitempty"`
	Expected int    `json:"expected,omitempty"`
	Versions []int  `json:"versions,omitempty"`
}
type secretRegistry struct {
	Schema       int                        `json:"schema"`
	Generation   int64                      `json:"generation"`
	Installation string                     `json:"installation"`
	Resources    map[string]*secretResource `json:"resources"`
	Keys         map[string]*secretKey      `json:"keys"`
	Pending      *secretOperation           `json:"pending,omitempty"`
}

func secretID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic("system random source unavailable")
	}
	return hex.EncodeToString(b)
}
func loadSecretRegistry() (secretRegistry, error) {
	b, e := readPrivate(filepath.Join(secretStateDir(), "registry.json"), 16<<20)
	if errors.Is(e, os.ErrNotExist) {
		return secretRegistry{Schema: 1, Installation: secretID(), Resources: map[string]*secretResource{}, Keys: map[string]*secretKey{}}, nil
	}
	var r secretRegistry
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil || r.Schema != 1 || r.Installation == "" || r.Resources == nil || r.Keys == nil {
		return r, fail("Invalid secrets registry; restore its matching backup")
	}
	return r, nil
}
func saveSecretRegistry(r *secretRegistry) error {
	r.Generation++
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return writePrivate(filepath.Join(secretStateDir(), "registry.json"), b)
}
func secretKeyID(resource, name string) string { return resource + "/" + name }
func resourceKVPath(r *secretResource, key string) (string, error) {
	if r == nil || !vaultSegment.MatchString(key) || key == "." || key == ".." || !vaultSegment.MatchString(r.ID) || !vaultSegment.MatchString(r.Project) {
		return "", fail("Invalid resource or key")
	}
	switch r.Kind {
	case "project":
		return "projects/" + r.ID + "/shared/" + key, nil
	case "branch":
		return "projects/" + r.Project + "/branches/" + r.ID + "/" + key, nil
	case "worktree":
		return "projects/" + r.Project + "/worktrees/" + r.ID + "/" + key, nil
	}
	return "", fail("Invalid secret resource kind")
}
