package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type secretContext struct {
	Project  string `json:"project"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Root     string `json:"root"`
}
type secretCheckout struct{ Root, Common, GitDir, Branch, ProjectAnchor, WorktreeAnchor string }

func directoryAnchor(path string) (string, error) {
	s, e := os.Stat(path)
	if e != nil || !s.IsDir() {
		return "", fail("Secret context directory is unavailable")
	}
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fail("Filesystem identity unavailable")
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}
func inspectSecretCheckout(root string) (secretCheckout, error) {
	var c secretCheckout
	if !filepath.IsAbs(root) {
		return c, fail("An absolute existing checkout is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var e error
	c.Root, e = gitControlCommand(ctx, root, "rev-parse", "--show-toplevel")
	if e != nil {
		if errors.Is(e, errNotGitRepository) {
			return c, fail("Run this command from a project's Git checkout: cd /path/to/project, then try again.")
		}
		return c, e
	}
	c.Root = resolve(c.Root)
	c.Common, e = gitControlCommand(ctx, c.Root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return c, e
	}
	c.Common = resolve(c.Common)
	c.GitDir, e = gitControlCommand(ctx, c.Root, "rev-parse", "--absolute-git-dir")
	if e != nil {
		return c, e
	}
	c.GitDir = resolve(c.GitDir)
	if rel, e := filepath.Rel(filepath.Join(c.Common, "devkit", "checkouts"), c.Root); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return c, fail("Temporary review checkouts do not own secret scopes")
	}
	c.Branch, _ = gitControlCommand(ctx, c.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if c.ProjectAnchor, e = directoryAnchor(c.Common); e != nil {
		return c, e
	}
	c.WorktreeAnchor, e = directoryAnchor(c.GitDir)
	return c, e
}
func branchSecretAnchor(c secretCheckout, name string) string {
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return ""
	}
	path := filepath.Join(c.Common, "logs", "refs", "heads", name)
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return ""
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return ""
	}
	hash := sha256.Sum256([]byte(scanner.Text()))
	return hex.EncodeToString(hash[:])
}
func findSecretProject(r *secretRegistry, c secretCheckout) *secretResource {
	for _, v := range r.Resources {
		if v.Kind == "project" && v.Anchor == c.ProjectAnchor && v.State != "destroyed" {
			return v
		}
	}
	return nil
}
func registerSecretContext(r *secretRegistry, c secretCheckout) (secretContext, error) {
	p := findSecretProject(r, c)
	if p == nil {
		id := secretID()
		p = &secretResource{ID: id, Kind: "project", Project: id, Name: filepath.Base(c.Root), Path: c.Root, Anchor: c.ProjectAnchor, State: "active"}
		r.Resources[id] = p
	}
	if p.State != "active" {
		return secretContext{}, fail("Project secrets are retired; restore first")
	}
	// Explicit enrollment records current refs. Discovery/context never creates identities.
	names, e := git(c.Root, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if e != nil {
		return secretContext{}, e
	}
	if c.Branch != "" && !strings.Contains("\n"+names+"\n", "\n"+c.Branch+"\n") {
		names += "\n" + c.Branch
	}
	for _, name := range strings.Split(names, "\n") {
		if name == "" {
			continue
		}
		found := false
		for _, v := range r.Resources {
			if v.Kind == "branch" && v.Project == p.ID && v.Name == name && v.State != "destroyed" {
				found = true
				break
			}
		}
		if !found {
			id := secretID()
			r.Resources[id] = &secretResource{ID: id, Kind: "branch", Project: p.ID, Name: name, RefAnchor: branchSecretAnchor(c, name), State: "active"}
		}
	}
	found := false
	for _, v := range r.Resources {
		if v.Kind == "worktree" && v.Project == p.ID && v.Anchor == c.WorktreeAnchor && v.State != "destroyed" {
			found = true
			if v.Path != c.Root {
				return secretContext{}, fail("Worktree moved; preview an explicit resource bind before transferring context")
			}
			break
		}
	}
	if !found {
		id := secretID()
		r.Resources[id] = &secretResource{ID: id, Kind: "worktree", Project: p.ID, Name: filepath.Base(c.Root), Path: c.Root, Anchor: c.WorktreeAnchor, State: "active"}
	}
	return resolveSecretContext(r, c, secretRequest{})
}
func resolveSecretContext(r *secretRegistry, c secretCheckout, q secretRequest) (secretContext, error) {
	result := secretContext{Root: c.Root}
	p := findSecretProject(r, c)
	if p == nil {
		return result, fail("Project is not enrolled; run devkit secrets resources register")
	}
	result.Project = p.ID
	if q.Project != "" && q.Project != p.ID {
		return result, fail("Project selector does not own this checkout")
	}
	if p.State != "active" {
		return result, fail("Project secrets are pending deletion")
	}
	for _, v := range r.Resources {
		if v.Project == p.ID && v.State != "active" && v.State != "destroyed" && ((v.Kind == "worktree" && v.Anchor == c.WorktreeAnchor) || (v.Kind == "branch" && v.Name == c.Branch)) {
			return result, fail("Secret context is retired; restore or explicitly rebind before access")
		}
		if v.Project != p.ID || v.State != "active" {
			continue
		}
		if v.Kind == "branch" && v.Name == c.Branch && v.RefAnchor != "" && v.RefAnchor == branchSecretAnchor(c, c.Branch) {
			result.Branch = v.ID
		}
		if v.Kind == "worktree" && v.Path == c.Root && v.Anchor == c.WorktreeAnchor {
			result.Worktree = v.ID
		}
	}
	if q.Worktree != "" {
		v := r.Resources[q.Worktree]
		if v == nil || v.Kind != "worktree" || v.Project != p.ID || v.State != "active" {
			return result, fail("Invalid worktree selector")
		}
		observed, e := inspectSecretCheckout(v.Path)
		if e != nil || observed.ProjectAnchor != p.Anchor || observed.WorktreeAnchor != v.Anchor {
			return result, fail("Worktree binding no longer matches")
		}
		result.Worktree = v.ID
		result.Branch = ""
		for _, b := range r.Resources {
			if b.Kind == "branch" && b.Project == p.ID && b.State == "active" && b.Name == observed.Branch && b.RefAnchor != "" && b.RefAnchor == branchSecretAnchor(observed, b.Name) {
				result.Branch = b.ID
			}
		}
	}
	if q.Branch != "" {
		b := r.Resources[q.Branch]
		if b == nil || b.Kind != "branch" || b.Project != p.ID || b.State != "active" || b.RefAnchor == "" || b.RefAnchor != branchSecretAnchor(c, b.Name) {
			return result, fail("Invalid or ambiguous branch selector")
		}
		if q.Worktree != "" && result.Branch != b.ID {
			return result, fail("Worktree is not linked to selected branch")
		}
		result.Branch = b.ID
		if q.Worktree == "" {
			result.Worktree = ""
		}
	}
	return result, nil
}
func contextResources(c secretContext, scope string) []string {
	switch scope {
	case "project":
		return []string{c.Project}
	case "branch":
		return []string{c.Branch, c.Project}
	default:
		return []string{c.Worktree, c.Branch, c.Project}
	}
}

func reconcileSecretInventory(r *secretRegistry, c secretCheckout, project string) ([]map[string]string, error) {
	names, e := git(c.Root, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if e != nil {
		return nil, e
	}
	refs := map[string]bool{}
	for _, name := range strings.Split(names, "\n") {
		refs[name] = true
	}
	trees, e := worktreesOf(c.Root)
	if e != nil {
		return nil, e
	}
	anchors := map[string]string{}
	for _, tree := range trees {
		observed, e := inspectSecretCheckout(tree.Path)
		if e == nil && observed.ProjectAnchor == c.ProjectAnchor {
			anchors[observed.WorktreeAnchor] = observed.Root
		}
	}
	var result []map[string]string
	for _, v := range r.Resources {
		if v.Project != project || v.State == "destroyed" {
			continue
		}
		status := "verified"
		switch v.Kind {
		case "branch":
			if !refs[v.Name] {
				status = "missing-branch"
			} else if v.RefAnchor == "" || v.RefAnchor != branchSecretAnchor(c, v.Name) {
				status = "ambiguous-branch-needs-bind"
			}
		case "worktree":
			if path, ok := anchors[v.Anchor]; !ok {
				status = "unavailable-worktree"
			} else if path != v.Path {
				status = "moved-worktree-needs-bind"
			}
		}
		result = append(result, map[string]string{"resource": v.ID, "name": v.Name, "observed": status, "state": v.State})
	}
	sort.Slice(result, func(i, j int) bool { return result[i]["resource"] < result[j]["resource"] })
	return result, nil
}
