@AGENTS.md

<!-- devkit-secrets begin sha256:27eb6717cd7d9f630b24b617b3936cc7ea653b8e3b45d062eaad656800f6bde5 -->
## Development credentials with Devkit

Use the devkit-secrets skill whenever creating, generating, saving or using development usernames, passwords or API tokens, or connecting an external application to Devkit secrets or managed OpenBao. Store credentials through Devkit's integrated vault so the human can find and copy them in this project's Secrets tab.

- Run from the intended project checkout. Check `devkit secrets context --json` and `devkit secrets list --json` before creating credentials; reuse suitable existing entries instead of silently replacing them. Follow the skill for registration or context mismatches.
- Save a username and its password together as a login entry (`--type login`) with a descriptive key such as `dev-admin`, not the username. Standalone passwords and API tokens remain single-value secrets. Do not silently combine or delete existing entries.
- Choose an explicit scope. Use worktree scope for temporary checkout credentials; choose branch or project scope only when sharing is intended. Worktree overrides branch, which overrides project. Never modify an inherited parent merely to create a local override.
- Generate and store passwords with `devkit secrets generate`; send existing values through protected stdin or hidden prompts. Follow the skill for paired-login input and explicit username/password environment mappings.
- Use `devkit secrets run` for runtime injection. Never put credential values in chat, Git, recipes, command arguments, logs or permanent dotenv files. Report only the entry name, scope and location in the Secrets tab.
- Saving or rotating a vault entry does not create or update the application's account. Apply credentials to the application only within the authorized task, and verify without printing them.
- Preserve existing Git/T3/agent workflows. Cleanup retention is configurable, including never and automatic off; do not change cleanup settings or delete credentials as an incidental step.
- If the vault is sealed or unavailable, report it and ask the human to restore access. Never fall back to plaintext storage or read host backend tokens, root tokens or unseal material.
- External clients use context-bound, least-privilege Devkit API tokens for inherited project secrets; native OpenBao is for separately authorized app-owned storage. Follow the skill for endpoint/auth selection, private token delivery, expiry/revocation and version/confirmation handling. Keep credentials server-side; never bundle them into browser code or reuse host backend/admin tokens. Host provisioning, vault upgrades, encrypted backups and restore/recovery belong to devkit-host under explicit operator authority.
<!-- devkit-secrets end -->

<!-- devkit-vms begin sha256:43e8aa075ab6c5523fd048b7a1ff116674f13b9de3a1c9dc743db51ead6d677d -->
## VM connections with Devkit

Use the devkit-vms skill whenever creating, changing, starting or connecting to QEMU/libvirt VMs on this host. Default new graphical guests to SPICE without a display password unless the user requests one, with fixed per-VM ports and both direct access and optional SSH tunnels. Preserve existing display passwords unless removal is explicitly authorized. Discover actual host addresses and show separate At home (LAN) and Away (Tailscale/VPN) connection details; a public dashboard URL is not a VM route. Prefer explicit private binds. A wildcard serving both networks requires a verified private-interface/source/destination firewall guard before opening any listener; never expose public ports or disable guest SSH authentication. Use devkit-secrets when display credentials are requested. Preserve running VMs during discovery/UI work; launcher changes take effect on the next QEMU start, and restarts require explicit authorization unless already given. Preserve project-specific instructions and report untested client routes honestly. Use devkit project validate --runtime vms --json and devkit restart --vm TARGET --dry-run --json to inspect exact authority and blockers. Opaque launcher settings and stopped guest networking remain unknown; there is no VM rebuild or new disk inspection.
<!-- devkit-vms end -->

<!-- devkit-previews begin sha256:a8084d1d7ae8fb5134b79e3e977c993e28b71e0d4ff52f5e6fcad154ac4763de -->
## Preview workflows with Devkit

Use the devkit-previews skill for development, testing, review and preview lifecycle work. Confirm the owning checkout and exact preview; run project tools through devkit exec and use bounded logs. Resuming a preview does not rebuild images or reread saved Compose. Preserve data when recreation is needed. Review the intended committed revision and report its URL, full SHA and checks. Public sharing, publishing and destructive cleanup need explicit authorization unless already given; development work does not grant Git or host-maintenance authority. Honor project-specific commands and user constraints. Use devkit context --json and inspect devkit rebuild PREVIEW --json before an authorized --yes application. Rebuild retains storage and identity; app code can still migrate data. Interrupted rebuilds require --recover inspection and explicit same-candidate recovery, never automatic rollback or clearing records. Use devkit logs --preview PREVIEW --tail 200 --since 10m for exact retained live/review logs, including stopped containers. Raw logs may contain secrets. Use devkit check PREVIEW --json for host-only primary-route evidence; redirects/auth challenges are warnings, not application proof. Inspect gc --json before an authorized gc --yes --json and preserve partial outcomes.
<!-- devkit-previews end -->

<!-- devkit-troubleshooting begin sha256:913bee85af61624613bc9065ffe597ca5e5ad67f61137fb15e151864b07e4c1d -->
## Troubleshooting with Devkit

Use the devkit-troubleshooting skill for unavailable, unhealthy or stale previews, dashboard/private URLs and agent integration failures. Begin with bounded read-only evidence: version, preview identity, status, logs, history and doctor. Distinguish application/container failures from DNS, TLS, proxy and client LAN/VPN routing. Preserve HTTPS hostname verification and keep credentials out of output. Diagnostics alone do not authorize restarts, host/firewall changes or data deletion; continue repairs already authorized by the user. Verify the affected behavior and report untested routes honestly. Use devkit-vms and devkit-secrets for their respective boundaries. Prefer context/ls/doctor --json and doctor --agents --json for structured evidence. Inspect complete and failed checks; distinguish saved configuration, source HEAD and observed images. Use rebuild --recover diagnostics after interrupted application. Prefer projects/containers/resources/history/shares --json for bounded metadata and logs --preview PREVIEW for exact raw logs. Use check PREVIEW --json without disabling TLS or following redirects; host success does not prove remote-client access. Route authorized host maintenance and vault recovery to devkit-host.
<!-- devkit-troubleshooting end -->

<!-- devkit-projects begin sha256:07a12e84171b712e3ce5d58dee0be0b5c7acfd79d3f0302e3cfb03ebe3aeef12 -->
## Projects with Devkit

Use the devkit-projects skill when creating a project, adapting an existing application for previews, or registering an existing Compose stack. Preserve the effective devkit.json/devbox.json recipe, VM mappings, user files and data. Distinguish isolated preview storage from existing-stack registration; registration never recreates containers or grants review/share/destroy authority. Follow the skill for live/review hooks, initial GitHub push recovery and project guidance installation. Use devkit project validate --json before launch and devkit context --json for exact checkout associations; missing inputs or incomplete evidence are not permission to launch. Use devkit projects --json and devkit containers --json for bounded inventory; inspect incomplete evidence and revalidate exact ownership before any action.
<!-- devkit-projects end -->

<!-- devkit-host begin sha256:fbcbc33a0d8430c75f2ffc81d8ac42bc892fb576405613e28ee21a3046db9b58 -->
## Host operations with Devkit

Use the devkit-host skill for host provisioning/upgrades, private proxy maintenance and managed OpenBao backup, rehearsal, restore and recovery. Establish the exact installation and operator authority; project work does not authorize service changes. Preserve private networking, recovery material and rollback paths. Checksum verification is not restore rehearsal. Restore cutover repeats rehearsal, requires explicit web-service handling, revokes app tokens and leaves cleanup off. Never disclose unseal shares, private keys or backend credentials; interrupted recovery must retain its journal and data.
<!-- devkit-host end -->
