# VM agent guidance distribution — v12

Supersedes [v11](archive/2026-11-14-vm-connections-v11.md); retain the completed UI,
discovery and deployment behavior. The five host VMs have since been separately
migrated and restarted with user approval. No VM restart is authorized in this task.

The remaining gap is agent discovery: devkit installs only its secrets skill, and
project agents do not inherit this repository's AGENTS.md. Add a versioned
`devkit-vms` skill to the existing Codex/Claude setup, install, status, repair and
doctor paths. Preserve existing per-agent status JSON fields and edited/unmanaged
files. Project installation adds a separately managed VM instruction block to
AGENTS.md and CLAUDE.md; new templates include the same guidance.

The skill requires SPICE for graphical QEMU guests; fixed ports; direct LAN and
Tailscale/VPN plus optional SSH tunnels; real host/interface discovery; guest SSH authentication; optional display passwords
through devkit-secrets; explicit restart authority; and honest reachability evidence.
Prefer explicit private binds. Where one bind must serve both private networks,
allow a wildcard only after an interface/source/destination-specific deny guard is
verified before opening the listener. No public exposure or disabled guest SSH authentication.
Do not encode this host's addresses, credentials or five guest launchers in the skill.

1. Regression-test both skill distribution and project instruction preservation.
2. Reuse the existing managed asset/block machinery; update rule/docs/template/ADR.
3. Run Go tests/vet and asset checks. Commit, fast-forward main and push the owned
   fork. Deploy the exact main build, restart only devkit-web, install both agent
   skills and relevant host project guidance, and verify unchanged VM identities.

Use the existing task worktree. Single-agent skill review covers new graphical VM,
loopback-only existing VM, missing VPN route, sealed vault and edited guidance cases;
do not spawn agents contrary to the user's working instructions. Fresh sessions may
be needed for Codex/Claude skill catalogs to refresh; report this limitation.

## Change from v11

The user explicitly requested password-free SPICE by default unless told otherwise.
New launchers use no display password behind the same private-only firewall guard.
Existing display passwords remain unchanged unless removal is authorized. Requested
passwords use devkit-secrets and fail closed if unavailable. No VM restart or live
authentication change is authorized in this rollout. Update skill, managed project
block, template, general rule and ADR together so agents see a consistent default.

## Verification checkpoint

Distribution regressions failed against the old secrets-only installer, then passed
with both assets and project blocks. Full `go test ./...` and `go vet ./...` passed;
template contracts pin both managed blocks. Tests cover selected-agent isolation,
managed upgrades, edited-file preservation and symlink rejection. Single-agent review
confirmed the new no-password private-only default, optional password/vault failure
behavior, existing-listener preservation, restart authority and missing VPN route
reporting. No frontend source/assets changed. Main push, matching deployment and
host guidance installation follow; host deployment receipts record final evidence.

Host installation exposed SwiftDesign's existing `CLAUDE.md -> AGENTS.md` alias.
Added bounded support for that exact sibling alias, preserving the link and writing
only the regular AGENTS.md through the existing no-follow/concurrency guards.
Regression coverage rejects external aliases; no VM operation is involved.
