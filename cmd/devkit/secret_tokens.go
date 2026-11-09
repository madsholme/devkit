package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type secretAPIToken struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Hash         string        `json:"hash,omitempty"`
	Installation string        `json:"installation"`
	Context      secretContext `json:"context"`
	Scope        string        `json:"scope"`
	BranchName   string        `json:"branch_name,omitempty"`
	Permissions  []string      `json:"permissions"`
	Created      time.Time     `json:"created"`
	Expires      *time.Time    `json:"expires,omitempty"`
}
type secretTokenStore struct {
	Schema int                       `json:"schema"`
	Tokens map[string]secretAPIToken `json:"tokens"`
}

func loadSecretTokens() (secretTokenStore, error) {
	s := secretTokenStore{Schema: 1, Tokens: map[string]secretAPIToken{}}
	b, err := readPrivate(filepath.Join(vaultDir(), "api-tokens.json"), 1<<20)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if json.Unmarshal(b, &s) != nil || s.Schema != 1 || s.Tokens == nil {
		return s, fail("Invalid API token store")
	}
	return s, nil
}
func saveSecretTokens(s secretTokenStore) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fail("API token store limit reached")
	}
	return writePrivate(filepath.Join(vaultDir(), "api-tokens.json"), b)
}
func createSecretToken(root, name, scope string, permissions []string, ttl time.Duration) (secretAPIToken, string, error) {
	var token secretAPIToken
	if !vaultSegment.MatchString(name) || name == "." || name == ".." {
		return token, "", fail("Use a safe application name")
	}
	if scope != "project" && scope != "branch" && scope != "worktree" {
		return token, "", fail("Choose --scope project, branch or worktree")
	}
	seen := map[string]bool{}
	for _, p := range permissions {
		if (p != "read" && p != "write" && p != "cleanup") || seen[p] {
			return token, "", fail("Permissions must be distinct read,write,cleanup values")
		}
		seen[p] = true
	}
	if len(seen) == 0 || ttl < 0 {
		return token, "", fail("Choose permissions and a positive TTL, or never")
	}
	unlock, err := lockSecretState()
	if err != nil {
		return token, "", err
	}
	defer unlock()
	r, err := loadSecretRegistry()
	if err != nil {
		return token, "", err
	}
	c, err := inspectSecretCheckout(root)
	if err != nil {
		return token, "", err
	}
	sc, err := resolveSecretContext(&r, c, secretRequest{})
	if err != nil {
		return token, "", err
	}
	if contextResources(sc, scope)[0] == "" || (scope != "project" && c.Branch != "" && sc.Branch == "") {
		return token, "", fail("Register an unambiguous secret context first")
	}
	s, err := loadSecretTokens()
	if err != nil {
		return token, "", err
	}
	for _, v := range s.Tokens {
		if v.Name == name {
			return token, "", fail("Token name already exists; revoke it or choose another name")
		}
	}
	if len(s.Tokens) >= 1000 {
		return token, "", fail("API token limit reached; revoke unused tokens")
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return token, "", err
	}
	token = secretAPIToken{ID: secretID(), Name: name, Installation: r.Installation, Context: sc, Scope: scope, BranchName: c.Branch, Permissions: permissions, Created: secretsNow().UTC()}
	if ttl > 0 {
		expiry := token.Created.Add(ttl)
		token.Expires = &expiry
	}
	raw := "dks_" + token.ID + "." + base64.RawURLEncoding.EncodeToString(random)
	token.Hash = secretTokenHash(raw)
	if err = secretAudit("token-create", contextResources(sc, scope)[0], token.ID); err != nil {
		return secretAPIToken{}, "", err
	}
	s.Tokens[token.ID] = token
	if err = saveSecretTokens(s); err != nil {
		return secretAPIToken{}, "", err
	}
	token.Hash = ""
	return token, raw, nil
}
func secretTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func revokeSecretToken(id string) error {
	unlock, err := lockSecretState()
	if err != nil {
		return err
	}
	defer unlock()
	s, err := loadSecretTokens()
	if err != nil {
		return err
	}
	if _, ok := s.Tokens[id]; !ok {
		return fail("API token not found")
	}
	if err = secretAudit("token-revoke", "", id); err != nil {
		return err
	}
	delete(s.Tokens, id)
	return saveSecretTokens(s)
}

const secretTokensUsage = `Usage:
  devkit secrets tokens create NAME --scope project|branch|worktree [--permissions read,write,cleanup] [--ttl 720h|never] [--output NEW_PRIVATE_FILE]
  devkit secrets tokens list [--json]
  devkit secrets tokens revoke ID
Create from a registered checkout. Default: read-only, 30 days. Tokens are shown once
on the controlling terminal or written to a new 0600 file. Token IDs are not credentials.
`

func secretTokensCLI(args []string) (int, error) {
	if len(args) == 0 {
		return 2, fail(secretTokensUsage)
	}
	if args[0] == "--help" {
		fmt.Fprint(stdout, secretTokensUsage)
		return 0, nil
	}
	pos, v, err := splitArgs(args[1:], map[string]bool{"--scope": true, "--permissions": true, "--ttl": true, "--output": true}, map[string]bool{"--json": true})
	if err != nil {
		return 2, err
	}
	switch args[0] {
	case "list":
		if len(pos) != 0 || len(v) > 1 || (len(v) == 1 && !hasTokenFlag(v, "--json")) {
			return 2, fail(secretTokensUsage)
		}
		unlock, err := lockSecretState()
		if err != nil {
			return 1, err
		}
		defer unlock()
		s, err := loadSecretTokens()
		if err != nil {
			return 1, err
		}
		list := []secretAPIToken{}
		for _, t := range s.Tokens {
			t.Hash = ""
			list = append(list, t)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		return 0, json.NewEncoder(stdout).Encode(list)
	case "revoke":
		if len(pos) != 1 || len(v) != 0 {
			return 2, fail(secretTokensUsage)
		}
		if err := revokeSecretToken(pos[0]); err != nil {
			return 1, err
		}
		fmt.Fprintln(stdout, "API token revoked.")
		return 0, nil
	case "create":
		if len(pos) != 1 || hasTokenFlag(v, "--json") {
			return 2, fail(secretTokensUsage)
		}
		ttl := 30 * 24 * time.Hour
		if value, ok := v["--ttl"]; ok {
			if value == "never" {
				ttl = 0
			} else {
				ttl, err = time.ParseDuration(value)
				if err != nil || ttl <= 0 {
					return 2, fail("TTL must be a positive duration such as 720h, or never")
				}
			}
		}
		root, err := os.Getwd()
		if err != nil {
			return 1, err
		}
		var output *os.File
		outputPath := v["--output"]
		if outputPath != "" {
			output, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		} else {
			output, err = humanTerminal()
		}
		if err != nil {
			return 1, fail("Token delivery needs a controlling terminal or a new --output file")
		}
		defer output.Close()
		token, raw, err := createSecretToken(root, pos[0], v["--scope"], strings.Split(firstOf(v["--permissions"], "read"), ","), ttl)
		if err != nil {
			if outputPath != "" {
				os.Remove(outputPath)
			}
			return 1, err
		}
		if _, err = fmt.Fprintln(output, raw); err == nil && outputPath != "" {
			err = output.Sync()
		}
		if err != nil {
			revokeErr := revokeSecretToken(token.ID)
			return 1, errors.Join(fail("Token delivery failed; revoke token ID %s if needed", token.ID), revokeErr)
		}
		return 0, json.NewEncoder(stdout).Encode(token)
	}
	return 2, fail(secretTokensUsage)
}
func hasTokenFlag(v map[string]string, key string) bool { _, ok := v[key]; return ok }

type secretTokenCredential struct{ ID, Hash string }
type secretTokenContextKey struct{}

var errSecretTokenAuth = errors.New("Invalid, expired or revoked API token")
var errSecretTokenScope = errors.New("API token does not permit this operation or context")

func parseSecretBearer(header string) (secretTokenCredential, error) {
	parts := strings.Split(header, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return secretTokenCredential{}, errSecretTokenAuth
	}
	id, secret, ok := strings.Cut(parts[1], ".")
	if !ok || !strings.HasPrefix(id, "dks_") || len(id) != 36 {
		return secretTokenCredential{}, errSecretTokenAuth
	}
	if _, err := hex.DecodeString(id[4:]); err != nil {
		return secretTokenCredential{}, errSecretTokenAuth
	}
	b, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(b) != 32 {
		return secretTokenCredential{}, errSecretTokenAuth
	}
	return secretTokenCredential{ID: id[4:], Hash: secretTokenHash(parts[1])}, nil
}
func tokenOperationPermission(op string) string {
	switch op {
	case "context", "list", "get", "history", "inject":
		return "read"
	case "set", "update", "generate", "rotate", "delete", "restore":
		return "write"
	case "cleanup-preview", "cleanup-retire", "cleanup-restore", "cleanup-purge":
		return "cleanup"
	}
	return ""
}
func authorizeSecretToken(cred secretTokenCredential, r *secretRegistry, q *secretRequest) (secretAPIToken, error) {
	s, err := loadSecretTokens()
	if err != nil {
		return secretAPIToken{}, errSecretTokenAuth
	}
	token, ok := s.Tokens[cred.ID]
	if !ok || subtle.ConstantTimeCompare([]byte(token.Hash), []byte(cred.Hash)) != 1 || (token.Expires != nil && !secretsNow().Before(*token.Expires)) || token.Installation != r.Installation {
		return token, errSecretTokenAuth
	}
	permission := tokenOperationPermission(q.Operation)
	allowed := false
	for _, p := range token.Permissions {
		if p == permission && permission != "" {
			allowed = true
		}
	}
	if !allowed || q.Root != "" && q.Root != token.Context.Root || q.Scope != "" && q.Scope != token.Scope || q.Project != "" || q.Branch != "" || q.Worktree != "" || q.Service != "" || q.Image != "" || q.Backup != nil || q.Cleanup != nil {
		return token, errSecretTokenScope
	}
	chain := contextResources(token.Context, token.Scope)
	selected := chain[0]
	if selected == "" || q.Resource != "" && q.Resource != selected {
		return token, errSecretTokenScope
	}
	c, err := inspectSecretCheckout(token.Context.Root)
	if err != nil {
		return token, errSecretTokenScope
	}
	project := r.Resources[token.Context.Project]
	if project == nil || project.Kind != "project" || project.Anchor != c.ProjectAnchor || project.State == "destroyed" {
		return token, errSecretTokenScope
	}
	resource := r.Resources[selected]
	if resource == nil || resource.Kind != token.Scope || resource.Project != project.ID || resource.State == "destroyed" {
		return token, errSecretTokenScope
	}
	if token.Scope != "project" {
		// Branch retirement requires Git to have switched away first. Cleanup
		// stays pinned to the original branch identity, never the new branch.
		if c.Branch != token.BranchName && !(token.Scope == "branch" && permission == "cleanup") {
			return token, errSecretTokenScope
		}
		if token.Context.Branch != "" {
			b := r.Resources[token.Context.Branch]
			if b == nil || b.Project != project.ID || b.State == "destroyed" || b.RefAnchor == "" || b.RefAnchor != branchSecretAnchor(c, token.BranchName) {
				return token, errSecretTokenScope
			}
		}
	}
	if token.Scope == "worktree" && (resource.Anchor != c.WorktreeAnchor || resource.Path != c.Root) {
		return token, errSecretTokenScope
	}
	q.Root = token.Context.Root
	q.Scope = token.Scope
	q.Project = token.Context.Project
	if permission == "cleanup" {
		q.Resource = selected
	} else if q.Resource != "" {
		return token, errSecretTokenScope
	}
	return token, nil
}
