package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func secretFixture(t *testing.T) {
	t.Helper()
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	type entry struct {
		Data    map[string]string
		Version int
		Deleted bool
	}
	data := map[string]entry{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/seal-status" {
			fmt.Fprint(w, `{"initialized":true,"sealed":false}`)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/v1/kv/")
		kind, path, _ := strings.Cut(p, "/")
		item, ok := data[path]
		if kind == "data" {
			if r.Method == "GET" {
				if !ok || item.Deleted {
					w.WriteHeader(404)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": item.Data, "metadata": map[string]int{"version": item.Version}}})
				return
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Options.CAS != item.Version {
				w.WriteHeader(400)
				return
			}
			item.Data = body.Data
			item.Version++
			item.Deleted = false
			data[path] = item
			fmt.Fprintf(w, `{"data":{"version":%d}}`, item.Version)
			return
		}
		if kind == "metadata" {
			if r.Method == "DELETE" {
				delete(data, path)
				return
			}
			if !ok {
				w.WriteHeader(404)
				return
			}
			versions := map[string]any{}
			for i := 1; i <= item.Version; i++ {
				versions[fmt.Sprint(i)] = map[string]any{"destroyed": false, "deletion_time": ""}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"current_version": item.Version, "versions": versions}})
			return
		}
		if kind == "delete" {
			item.Deleted = true
			data[path] = item
		}
		if kind == "undelete" {
			item.Deleted = false
			data[path] = item
		}
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	cfg := defaultVaultConfig()
	cfg.Address = server.URL
	if e := saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if e := writePrivate(filepath.Join(vaultDir(), "backend-token"), []byte("test")); e != nil {
		t.Fatal(e)
	}
}
func TestSecretsSaveRevealInheritanceAndNoPlaintextState(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	run := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, e := executeSecret(context.Background(), q)
		if e != nil {
			t.Fatalf("%s: %v", q.Operation, e)
		}
		return out
	}
	registered := run(secretRequest{Operation: "register"})
	if registered.Context.Project == "" || registered.Context.Worktree == "" {
		t.Fatal("missing IDs")
	}
	run(secretRequest{Operation: "set", Scope: "project", Key: "PASSWORD", Value: "parent-SENTINEL"})
	got := run(secretRequest{Operation: "list"})
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "SENTINEL") || len(got.Records) != 1 || !got.Records[0].Inherited {
		t.Fatalf("list leakage or inheritance: %s", b)
	}
	run(secretRequest{Operation: "set", Scope: "worktree", Key: "PASSWORD", Value: "child-SENTINEL"})
	got = run(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true})
	if got.Value != "child-SENTINEL" {
		t.Fatal("precedence")
	}
	got = run(secretRequest{Operation: "get", Scope: "project", Key: "PASSWORD", Reveal: true})
	if got.Value != "parent-SENTINEL" {
		t.Fatal("parent overwritten")
	}
	if _, e := executeSecret(context.Background(), secretRequest{Root: repo.root, Operation: "update", Scope: "worktree", Key: "PASSWORD", Value: "stale", Expected: 0}); e == nil {
		t.Fatal("stale write succeeded")
	}
	if _, e := executeSecret(context.Background(), secretRequest{Root: repo.root, Operation: "get", Key: "../devkit/system/web-secret", Reveal: true}); e == nil {
		t.Fatal("system path injection")
	}
	state, e := os.ReadFile(filepath.Join(secretStateDir(), "registry.json"))
	if e != nil || strings.Contains(string(state), "SENTINEL") {
		t.Fatal("plaintext registry")
	}
}
func TestSecretsContextRejectsOtherProjectSelectors(t *testing.T) {
	secretFixture(t)
	a, b := newGitRepo(t), newGitRepo(t)
	first, e := executeSecret(context.Background(), secretRequest{Root: a.root, Operation: "register"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = executeSecret(context.Background(), secretRequest{Root: b.root, Operation: "register"}); e != nil {
		t.Fatal(e)
	}
	if _, e = executeSecret(context.Background(), secretRequest{Root: b.root, Operation: "set", Project: first.Context.Project, Scope: "project", Key: "KEY", Value: "secret"}); e == nil {
		t.Fatal("cross-project selector accepted")
	}
}

func TestSecretsRetiredWorktreeDoesNotFallBackToParent(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	enrolled, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	r, _ := loadSecretRegistry()
	r.Resources[enrolled.Context.Worktree].State = "retired"
	if e = saveSecretRegistry(&r); e != nil {
		t.Fatal(e)
	}
	if _, e = executeSecret(context.Background(), secretRequest{Operation: "list", Root: repo.root}); e == nil {
		t.Fatal("retired context inherited active secrets")
	}
}
func TestSecretsCannotRegisterWithLocalProvider(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	repo := newGitRepo(t)
	if _, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); e == nil {
		t.Fatal("enrolled while disabled")
	}
}

func TestSecretsReconcileReportsExternalDeletionWithoutRetiring(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	if _, e := git(repo.root, "branch", "external-branch"); e != nil {
		t.Fatal(e)
	}
	if _, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); e != nil {
		t.Fatal(e)
	}
	if _, e := git(repo.root, "branch", "-D", "external-branch"); e != nil {
		t.Fatal(e)
	}
	out, e := executeSecret(context.Background(), secretRequest{Operation: "reconcile", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "missing-branch") {
		t.Fatalf("missing reconciliation evidence: %s", b)
	}
	r, _ := loadSecretRegistry()
	for _, resource := range r.Resources {
		if resource.Name == "external-branch" && resource.State != "active" {
			t.Fatal("inferred destructive retirement")
		}
	}
}

func TestSecretsRebindRefusesAnAlreadyRegisteredBranchIdentity(t *testing.T) {
	secretFixture(t)
	repo := newGitRepo(t)
	old, e := executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = git(repo.root, "branch", "-m", "renamed"); e != nil {
		t.Fatal(e)
	}
	if _, e = executeSecret(context.Background(), secretRequest{Operation: "register", Root: repo.root}); e != nil {
		t.Fatal(e)
	}
	q := secretRequest{Operation: "bind", Root: repo.root, Resource: old.Context.Branch}
	preview, e := executeSecret(context.Background(), q)
	if e != nil {
		return
	}
	q.Confirm = preview.Confirmation
	if _, e = executeSecret(context.Background(), q); e == nil {
		t.Fatal("two identities now claim the same branch")
	}
}

func TestSecretsContextCountsOnlyActiveOwnedEntries(t *testing.T) {
	secretFixture(t)
	repo, other := newGitRepo(t), newGitRepo(t)
	run := func(root string, q secretRequest) secretResponse {
		t.Helper()
		q.Root = root
		out, err := executeSecret(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	registered := run(repo.root, secretRequest{Operation: "register"})
	run(other.root, secretRequest{Operation: "register"})
	for _, scope := range []string{"project", "branch", "worktree"} {
		run(repo.root, secretRequest{Operation: "set", Scope: scope, Key: "PASSWORD", Value: "count-SENTINEL"})
	}
	run(other.root, secretRequest{Operation: "set", Scope: "project", Key: "OTHER", Value: "other-SENTINEL"})
	run(repo.root, secretRequest{Operation: "set", Scope: "worktree", Key: "DELETED", Value: "deleted-SENTINEL"})
	preview := run(repo.root, secretRequest{Operation: "delete", Scope: "worktree", Key: "DELETED"})
	run(repo.root, secretRequest{Operation: "delete", Scope: "worktree", Key: "DELETED", Confirm: preview.Confirmation})
	check := func(want int) {
		t.Helper()
		out := run(repo.root, secretRequest{Operation: "context"})
		encoded, _ := json.Marshal(out)
		var response struct {
			Counts map[string]int `json:"counts"`
		}
		if json.Unmarshal(encoded, &response) != nil || response.Counts == nil {
			t.Fatal("context lacks counts")
		}
		total := 0
		for _, count := range response.Counts {
			total += count
		}
		if total != want || response.Counts[registered.Context.Project] != 1 {
			t.Fatalf("counts: %v", response.Counts)
		}
		if strings.Contains(string(encoded), "SENTINEL") {
			t.Fatal("count response leaks values")
		}
	}
	check(3)
	// An inactive branch's stored entries are excluded without affecting its parent.
	r, err := loadSecretRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r.Resources[registered.Context.Branch].State = "destroyed"
	if err := saveSecretRegistry(&r); err != nil {
		t.Fatal(err)
	}
	check(2)
}
