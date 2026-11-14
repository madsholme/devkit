# VM agent guidance distribution — v11

Supersedes [v10](archive/2026-11-14-vm-connections-v10.md); retain its completed UI,
discovery and deployment behavior. The five host VMs have since been separately
migrated and restarted with user approval. No VM restart is authorized in this task.

The remaining gap is agent discovery: devkit installs only its secrets skill, and
project agents do not inherit this repository's AGENTS.md. Add a versioned
`devkit-vms` skill to the existing Codex/Claude setup, install, status, repair and
doctor paths. Preserve existing per-agent status JSON fields and edited/unmanaged
files. Project installation adds a separately managed VM instruction block to
AGENTS.md and CLAUDE.md; new templates include the same guidance.

The skill requires SPICE for graphical QEMU guests; fixed ports; direct LAN and
Tailscale/VPN plus optional SSH tunnels; real host/interface discovery; authentication
through devkit-secrets; explicit restart authority; and honest reachability evidence.
Prefer explicit private binds. Where one bind must serve both private networks,
allow a wildcard only after an interface/source/destination-specific deny guard is
verified before opening the listener. No public exposure or disabled authentication.
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
