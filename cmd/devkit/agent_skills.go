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

//go:embed agent-assets/*/SKILL.md
var secretsSkillAssets embed.FS

const secretsSkillVersion = "4"

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
	return agentSkillStatus(dir, secretsSkillVersion, canonicalSecretsSkill())
}
func agentSkillStatus(dir, version string, content []byte) string {
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
	if manifest.Version != version || !bytes.Equal(b, content) {
		return "outdated"
	}
	return "current"
}
func installSecretsSkill(dir string) error {
	return installAgentSkill(dir, secretsSkillVersion, canonicalSecretsSkill())
}
func installAgentSkill(dir, version string, content []byte) error {
	status := agentSkillStatus(dir, version, content)
	if status == "current" {
		return nil
	}
	if status != "missing" && status != "outdated" {
		return fail("Preserving %s skill at %s; move your custom version before installing", status, dir)
	}
	if e := privateDir(dir); e != nil {
		return e
	}
	b := content
	if e := writePrivate(filepath.Join(dir, "SKILL.md"), b); e != nil {
		return e
	}
	manifest, _ := json.Marshal(secretsSkillManifest{version, skillHash(b)})
	return writePrivate(filepath.Join(dir, ".devkit-managed.json"), manifest)
}

const secretsInstructionBody = "## Development credentials with Devkit\n\nUse the devkit-secrets skill whenever creating, generating, saving or using development usernames, passwords or API tokens, or connecting an external application to Devkit secrets or managed OpenBao. Store credentials through Devkit's integrated vault so the human can find and copy them in this project's Secrets tab.\n\n- Run from the intended project checkout. Check `devkit secrets context --json` and `devkit secrets list --json` before creating credentials; reuse suitable existing entries instead of silently replacing them. Follow the skill for registration or context mismatches.\n- Save a username and its password together as a login entry (`--type login`) with a descriptive key such as `dev-admin`, not the username. Standalone passwords and API tokens remain single-value secrets. Do not silently combine or delete existing entries.\n- Choose an explicit scope. Use worktree scope for temporary checkout credentials; choose branch or project scope only when sharing is intended. Worktree overrides branch, which overrides project. Never modify an inherited parent merely to create a local override.\n- Generate and store passwords with `devkit secrets generate`; send existing values through protected stdin or hidden prompts. Follow the skill for paired-login input and explicit username/password environment mappings.\n- Use `devkit secrets run` for runtime injection. Never put credential values in chat, Git, recipes, command arguments, logs or permanent dotenv files. Report only the entry name, scope and location in the Secrets tab.\n- Saving or rotating a vault entry does not create or update the application's account. Apply credentials to the application only within the authorized task, and verify without printing them.\n- Preserve existing Git/T3/agent workflows. Cleanup retention is configurable, including never and automatic off; do not change cleanup settings or delete credentials as an incidental step.\n- If the vault is sealed or unavailable, report it and ask the human to restore access. Never fall back to plaintext storage or read host backend tokens, root tokens or unseal material.\n- External clients use context-bound, least-privilege Devkit API tokens for inherited project secrets; native OpenBao is for separately authorized app-owned storage. Follow the skill for endpoint/auth selection, private token delivery, expiry/revocation and version/confirmation handling. Keep credentials server-side; never bundle them into browser code or reuse host backend/admin tokens. Host provisioning, vault upgrades, encrypted backups and restore/recovery belong to devkit-host under explicit operator authority.\n"
const secretsInstructionEnd = "<!-- devkit-secrets end -->"

func installSecretsInstructions(path string) error {
	return installAgentInstructions(path, "devkit-secrets", secretsInstructionBody)
}

const projectsInstructionBody = "## Projects with Devkit\n\nUse the devkit-projects skill when creating a project, adapting an existing application for previews, or registering an existing Compose stack. Preserve the effective devkit.json/devbox.json recipe, VM mappings, user files and data. Distinguish isolated preview storage from existing-stack registration; registration never recreates containers or grants review/share/destroy authority. Follow the skill for live/review hooks, initial GitHub push recovery and project guidance installation. When adding a separate API, follow the skill to pass the private origin and opt the API into runtime custom share URLs in both Compose recipes. Use devkit project validate --json before launch and devkit context --json for exact checkout associations; missing inputs or incomplete evidence are not permission to launch. Use devkit projects --json and devkit containers --json for bounded inventory; inspect incomplete evidence and revalidate exact ownership before any action.\n"

const vmInstructionBody = "## VM connections with Devkit\n\nUse the devkit-vms skill whenever creating, changing, starting or connecting to QEMU/libvirt VMs on this host. Default new graphical guests to SPICE without a display password unless the user requests one, with fixed per-VM ports and both direct access and optional SSH tunnels. Preserve existing display passwords unless removal is explicitly authorized. Discover actual host addresses and show separate At home (LAN) and Away (Tailscale/VPN) connection details; a public dashboard URL is not a VM route. Prefer explicit private binds. A wildcard serving both networks requires a verified private-interface/source/destination firewall guard before opening any listener; never expose public ports or disable guest SSH authentication. Use devkit-secrets when display credentials are requested. Preserve running VMs during discovery/UI work; launcher changes take effect on the next QEMU start, and restarts require explicit authorization unless already given. Preserve project-specific instructions and report untested client routes honestly. Use devkit project validate --runtime vms --json and devkit restart --vm TARGET --dry-run --json to inspect exact authority and blockers. Opaque launcher settings and stopped guest networking remain unknown; there is no VM rebuild or new disk inspection.\n"

const previewsInstructionBody = "## Preview workflows with Devkit\n\nUse the devkit-previews skill for development, testing, review and preview lifecycle work. Confirm the owning checkout and exact preview; run project tools through devkit exec and use bounded logs. Resuming a preview does not rebuild images or reread saved Compose. Preserve data when recreation is needed. Review the intended committed revision and report its URL, full SHA and checks. Public sharing, publishing and destructive cleanup need explicit authorization unless already given; development work does not grant Git or host-maintenance authority. Honor project-specific commands and user constraints. Use devkit context --json and inspect devkit rebuild PREVIEW --json before an authorized --yes application. Rebuild retains storage and identity; app code can still migrate data. Interrupted rebuilds require --recover inspection and explicit same-candidate recovery, never automatic rollback or clearing records. Use devkit logs --preview PREVIEW --tail 200 --since 10m for exact retained live/review logs, including stopped containers. Raw logs may contain secrets. Use devkit check PREVIEW --json for host-only primary-route evidence; redirects/auth challenges are warnings, not application proof. Inspect gc --json before an authorized gc --yes --json and preserve partial outcomes.\n"

const troubleshootingInstructionBody = "## Troubleshooting with Devkit\n\nUse the devkit-troubleshooting skill for unavailable, unhealthy or stale previews, dashboard/private URLs and agent integration failures. Begin with bounded read-only evidence: version, preview identity, status, logs, history and doctor. Distinguish application/container failures from DNS, TLS, proxy and client LAN/VPN routing. Preserve HTTPS hostname verification and keep credentials out of output. Diagnostics alone do not authorize restarts, host/firewall changes or data deletion; continue repairs already authorized by the user. Verify the affected behavior and report untested routes honestly. Use devkit-vms and devkit-secrets for their respective boundaries. Prefer context/ls/doctor --json and doctor --agents --json for structured evidence. Inspect complete and failed checks; distinguish saved configuration, source HEAD and observed images. Use rebuild --recover diagnostics after interrupted application. Prefer projects/containers/resources/history/shares --json for bounded metadata and logs --preview PREVIEW for exact raw logs. Use check PREVIEW --json without disabling TLS or following redirects; host success does not prove remote-client access. Route authorized host maintenance and vault recovery to devkit-host.\n"

const hostInstructionBody = "## Host operations with Devkit\n\nUse the devkit-host skill for host provisioning/upgrades, private proxy maintenance and managed OpenBao backup, rehearsal, restore and recovery. Establish the exact installation and operator authority; project work does not authorize service changes. Preserve private networking, recovery material and rollback paths. Checksum verification is not restore rehearsal. Restore cutover repeats rehearsal, requires explicit web-service handling, revokes app tokens and leaves cleanup off. Never disclose unseal shares, private keys or backend credentials; interrupted recovery must retain its journal and data.\n"

func installProjectAgentInstructions(path string) error {
	for _, skill := range bundledAgentSkills() {
		if err := installAgentInstructions(path, skill.name, skill.instructions); err != nil {
			return err
		}
	}
	return nil
}

func projectAgentInstructionPath(root, name string) string {
	path := filepath.Join(root, name)
	// Share the verified sibling for this conventional alias. Never follow an
	// arbitrary symlink or replace the CLAUDE.md alias itself.
	if name == "CLAUDE.md" {
		if target, err := os.Readlink(path); err == nil && target == "AGENTS.md" {
			sibling := filepath.Join(root, "AGENTS.md")
			if info, err := os.Lstat(sibling); err == nil && info.Mode().IsRegular() {
				return sibling
			}
		}
	}
	return path
}

func installAgentInstructions(path, name, instructionBody string) error {
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
	start := "<!-- " + name + " begin sha256:"
	endMarker := "<!-- " + name + " end -->"
	block := start + skillHash([]byte(instructionBody)) + " -->\n" + instructionBody + endMarker
	if i := strings.Index(text, start); i >= 0 {
		suffix := text[i:]
		headerEnd := strings.Index(suffix, " -->\n")
		end := strings.Index(suffix, endMarker)
		if headerEnd < 0 || end < headerEnd || strings.Count(text, start) != 1 || strings.Count(text, endMarker) != 1 {
			return fail("Malformed managed instruction block; preserving it")
		}
		want := suffix[len(start):headerEnd]
		body := suffix[headerEnd+len(" -->\n") : end]
		if skillHash([]byte(body)) != want {
			return fail("Managed instruction block was edited; preserving it")
		}
		text = text[:i] + block + suffix[end+len(endMarker):]
	} else {
		if strings.Contains(text, endMarker) {
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
	results, e := managedAgentSkillsFor(op, selected)
	if e != nil {
		return 1, e
	}
	if root := v["--project"]; root != "" && op != "status" {
		checkout, e := inspectSecretCheckout(root)
		if e != nil {
			return 1, e
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			if e = installProjectAgentInstructions(projectAgentInstructionPath(checkout.Root, name)); e != nil {
				return 1, e
			}
		}
	}
	if op != "status" {
		happened("agents skills "+op, selected, "bundled skills; user edits preserved")
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
		for _, skill := range bundledAgentSkills() {
			status := agentSkillStatus(filepath.Join(home, agent, "skills", skill.name), skill.version, skill.content)
			fmt.Fprintf(stdout, "%s/%s: %s (devkit agents skills install|repair)\n", agent, skill.name, status)
			if status != "current" {
				code = 1
			}
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

type bundledAgentSkill struct {
	name, version string
	content       []byte
	instructions  string
}

func bundledAgentSkills() []bundledAgentSkill {
	host, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-host/SKILL.md")
	projects, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-projects/SKILL.md")
	vm, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-vms/SKILL.md")
	previews, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-previews/SKILL.md")
	troubleshooting, _ := secretsSkillAssets.ReadFile("agent-assets/devkit-troubleshooting/SKILL.md")
	return []bundledAgentSkill{
		{"devkit-secrets", secretsSkillVersion, canonicalSecretsSkill(), secretsInstructionBody},
		{"devkit-vms", "2", vm, vmInstructionBody},
		{"devkit-previews", "4", previews, previewsInstructionBody},
		{"devkit-troubleshooting", "3", troubleshooting, troubleshootingInstructionBody},
		{"devkit-projects", "4", projects, projectsInstructionBody},
		{"devkit-host", "1", host, hostInstructionBody},
	}
}
func managedAgentSkills(operation string) (map[string]string, error) {
	return managedAgentSkillsFor(operation, "both")
}
func managedAgentSkillsFor(operation, selected string) (map[string]string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	result := map[string]string{}
	for _, agent := range []string{"codex", "claude"} {
		if selected != "both" && selected != agent {
			continue
		}
		folder := ".agents"
		if agent == "claude" {
			folder = ".claude"
		}
		result[agent] = "current"
		for _, skill := range bundledAgentSkills() {
			path := filepath.Join(home, folder, "skills", skill.name)
			if operation == "install" || operation == "repair" {
				if e = installAgentSkill(path, skill.version, skill.content); e != nil {
					return nil, e
				}
			}
			status := agentSkillStatus(path, skill.version, skill.content)
			if result[agent] == "current" {
				result[agent] = status
			}
		}
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
		path := projectAgentInstructionPath(checkout.Root, name)
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
			checks = append(checks, warn(name, "Missing/unsafe guidance; use agents skills install --project PATH"))
			continue
		}
		b, e := os.ReadFile(path)
		current := e == nil
		for _, skill := range bundledAgentSkills() {
			if !strings.Contains(string(b), skill.instructions) {
				current = false
			}
		}
		if !current {
			checks = append(checks, warn(name, "Managed guidance missing or edited; preserve edits when repairing"))
		} else {
			checks = append(checks, ok(name, "Current bundled agent guidance present"))
		}
	}
	return checks
}
