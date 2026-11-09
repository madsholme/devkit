package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed agent-assets/devkit-secrets/SKILL.md
var secretsSkillAssets embed.FS

const secretsSkillVersion = "2"

type secretsSkillManifest struct {
	Version string `json:"version"`
	Hash    string `json:"sha256"`
}

func skillHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func canonicalSecretsSkill() []byte {
	b, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-secrets/SKILL.md")
	return b
}
func secretsSkillStatus(dir string) string {
	b, e := readPrivate(filepath.Join(dir, "SKILL.md"), 128<<10)
	if os.IsNotExist(e) {
		return "missing"
	}
	if e != nil {
		return "unsafe"
	}
	m, e := readPrivate(filepath.Join(dir, ".devkit-managed.json"), 4096)
	var manifest secretsSkillManifest
	if e != nil || json.Unmarshal(m, &manifest) != nil {
		return "unmanaged"
	}
	if skillHash(b) != manifest.Hash {
		return "edited"
	}
	if manifest.Version != secretsSkillVersion || !bytes.Equal(b, canonicalSecretsSkill()) {
		return "outdated"
	}
	return "current"
}
func installSecretsSkill(dir string) error {
	status := secretsSkillStatus(dir)
	if status == "current" {
		return nil
	}
	if status != "missing" && status != "outdated" {
		return fail("Preserving %s skill at %s; move your custom version before installing", status, dir)
	}
	if e := privateDir(dir); e != nil {
		return e
	}
	b := canonicalSecretsSkill()
	if e := writePrivate(filepath.Join(dir, "SKILL.md"), b); e != nil {
		return e
	}
	manifest, _ := json.Marshal(secretsSkillManifest{secretsSkillVersion, skillHash(b)})
	return writePrivate(filepath.Join(dir, ".devkit-managed.json"), manifest)
}

const secretsInstructionBody = "Use the devkit-secrets skill for development credentials. Save generated passwords with devkit secrets; never put values in chat, Git or permanent dotenv files. Choose an explicit project, branch or worktree scope. Use runtime injection and metadata-only output. Preserve existing Git/T3/agent workflows. Cleanup retention is configurable, including never and automatic off.\n"
const secretsInstructionEnd = "<!-- devkit-secrets end -->"

func installSecretsInstructions(path string) error {
	// Lock the directory, preserve permissions and reject symlinks/concurrent changes.
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	if e = syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fail("Instruction directory is busy")
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	read := func() ([]byte, os.FileMode, error) {
		fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if os.IsNotExist(e) {
			return nil, 0644, nil
		}
		if e != nil {
			return nil, 0, e
		}
		f := os.NewFile(uintptr(fd), path)
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() > 256<<10 {
			return nil, 0, fail("Unsafe instruction file")
		}
		b := make([]byte, st.Size())
		_, e = f.ReadAt(b, 0)
		return b, st.Mode().Perm(), e
	}
	before, mode, e := read()
	if e != nil {
		return e
	}
	text := string(before)
	start := "<!-- devkit-secrets begin sha256:"
	block := start + skillHash([]byte(secretsInstructionBody)) + " -->\n" + secretsInstructionBody + secretsInstructionEnd
	if i := strings.Index(text, start); i >= 0 {
		suffix := text[i:]
		headerEnd := strings.Index(suffix, " -->\n")
		end := strings.Index(suffix, secretsInstructionEnd)
		if headerEnd < 0 || end < headerEnd || strings.Count(text, start) != 1 || strings.Count(text, secretsInstructionEnd) != 1 {
			return fail("Malformed managed instruction block; preserving it")
		}
		want := suffix[len(start):headerEnd]
		body := suffix[headerEnd+len(" -->\n") : end]
		if skillHash([]byte(body)) != want {
			return fail("Managed instruction block was edited; preserving it")
		}
		text = text[:i] + block + suffix[end+len(secretsInstructionEnd):]
	} else {
		if strings.Contains(text, secretsInstructionEnd) {
			return fail("Incomplete managed instruction block")
		}
		text = strings.TrimRight(text, "\n") + "\n\n" + block + "\n"
	}
	if text == string(before) {
		return nil
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".devkit-agents-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.WriteString(text); e == nil {
		e = f.Chmod(mode)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	current, currentMode, e := read()
	if e != nil || !bytes.Equal(before, current) || currentMode != mode {
		return fail("Instructions changed during save; retry")
	}
	if before == nil {
		if e = os.Link(f.Name(), path); e != nil {
			return e
		}
	} else {
		if e = os.Rename(f.Name(), path); e != nil {
			return e
		}
	}
	return dir.Sync()
}
func agentsSkillsCLI(args []string) (int, error) {
	if len(args) < 2 || args[0] != "skills" {
		return 2, fail("usage: devkit agents skills install|status|repair [--agent codex|claude|both] [--project PATH]")
	}
	pos, v, e := splitArgs(args[2:], map[string]bool{"--agent": true, "--project": true}, map[string]bool{"--json": true})
	if e != nil || len(pos) != 0 {
		return 2, fail("Invalid agent skill arguments")
	}
	op := args[1]
	if op != "install" && op != "status" && op != "repair" {
		return 2, fail("Unknown agent skill operation")
	}
	selected := firstOf(v["--agent"], "both")
	if selected != "both" && selected != "codex" && selected != "claude" {
		return 2, fail("Select codex, claude or both")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return 1, e
	}
	paths := map[string]string{"codex": filepath.Join(home, ".agents", "skills", "devkit-secrets"), "claude": filepath.Join(home, ".claude", "skills", "devkit-secrets")}
	results := map[string]string{}
	for _, agent := range []string{"codex", "claude"} {
		if selected != "both" && selected != agent {
			continue
		}
		path := paths[agent]
		if op != "status" {
			if e = installSecretsSkill(path); e != nil {
				return 1, e
			}
		}
		results[agent] = secretsSkillStatus(path)
	}
	if root := v["--project"]; root != "" && op != "status" {
		checkout, e := inspectSecretCheckout(root)
		if e != nil {
			return 1, e
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			if e = installSecretsInstructions(filepath.Join(checkout.Root, name)); e != nil {
				return 1, e
			}
		}
	}
	if op != "status" {
		happened("agents skills "+op, selected, "version "+secretsSkillVersion+"; user edits preserved")
	}
	return 0, json.NewEncoder(stdout).Encode(results)
}
func doctorAgents() (int, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return 1, e
	}
	code := 0
	for _, agent := range []string{".agents", ".claude"} {
		status := secretsSkillStatus(filepath.Join(home, agent, "skills", "devkit-secrets"))
		fmt.Fprintf(stdout, "%s: %s (devkit agents skills install|repair)\n", agent, status)
		if status != "current" {
			code = 1
		}
	}
	if path, e := exec.LookPath("devkit"); e != nil {
		fmt.Fprintln(stdout, "CLI: devkit is not on PATH")
		code = 1
	} else {
		fmt.Fprintln(stdout, "CLI:", path)
	}
	if root, e := os.Getwd(); e == nil {
		for _, c := range agentProjectChecks(root) {
			fmt.Fprintf(stdout, "%s: %s — %s\n", c.Name, c.Status, c.Detail)
			if c.Status != "ok" {
				code = 1
			}
		}
	}
	return code, nil
}

func managedAgentSkills(operation string) (map[string]string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	result := map[string]string{}
	for agent, folder := range map[string]string{"codex": ".agents", "claude": ".claude"} {
		path := filepath.Join(home, folder, "skills", "devkit-secrets")
		if operation == "install" || operation == "repair" {
			if e = installSecretsSkill(path); e != nil {
				return nil, e
			}
		}
		result[agent] = secretsSkillStatus(path)
	}
	return result, nil
}

var setupAgentSkills = func() error { _, e := managedAgentSkills("install"); return e }

func agentProjectChecks(root string) []check {
	checkout, e := inspectSecretCheckout(root)
	if e != nil {
		return []check{warn("agent-context", "No verified Git checkout here; run doctor --agents inside the intended project")}
	}
	var checks []check
	registry, e := loadSecretRegistry()
	if e != nil {
		checks = append(checks, bad("agent-context", "Secret registry unavailable"))
	} else if context, e := resolveSecretContext(&registry, checkout, secretRequest{}); e != nil {
		checks = append(checks, warn("agent-context", "%v", e))
	} else {
		checks = append(checks, ok("agent-context", "project %s, worktree %s", context.Project, context.Worktree))
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(checkout.Root, name)
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
			checks = append(checks, warn(name, "Missing/unsafe guidance; use agents skills install --project PATH"))
			continue
		}
		b, e := os.ReadFile(path)
		if e != nil || !strings.Contains(string(b), secretsInstructionBody) {
			checks = append(checks, warn(name, "Managed guidance missing or edited; preserve edits when repairing"))
		} else {
			checks = append(checks, ok(name, "Current secrets guidance present"))
		}
	}
	return checks
}
