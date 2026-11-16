@AGENTS.md

<!-- devkit-secrets begin sha256:8c85120ece7d2edb7bdd665c2da60df3a9b3b743eaf66b1e337a9a643ede89e5 -->
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
- External clients use context-bound, least-privilege Devkit API tokens for inherited project secrets; native OpenBao is for separately authorized app-owned storage. Follow the skill for endpoint/auth selection, private token delivery, expiry/revocation and version/confirmation handling. Keep credentials server-side; never bundle them into browser code or reuse host backend/admin tokens.
<!-- devkit-secrets end -->

<!-- devkit-vms begin sha256:53f89cfcf87dbe1b055ac111eaa5eb0ea4b2e5f0a2c50786485ca90f620bea62 -->
## VM connections with Devkit

Use the devkit-vms skill whenever creating, changing, starting or connecting to QEMU/libvirt VMs on this host. Default new graphical guests to SPICE without a display password unless the user requests one, with fixed per-VM ports and both direct access and optional SSH tunnels. Preserve existing display passwords unless removal is explicitly authorized. Discover actual host addresses and show separate At home (LAN) and Away (Tailscale/VPN) connection details; a public dashboard URL is not a VM route. Prefer explicit private binds. A wildcard serving both networks requires a verified private-interface/source/destination firewall guard before opening any listener; never expose public ports or disable guest SSH authentication. Use devkit-secrets when display credentials are requested. Preserve running VMs during discovery/UI work; launcher changes take effect on the next QEMU start, and restarts require explicit authorization unless already given. Preserve project-specific instructions and report untested client routes honestly.
<!-- devkit-vms end -->

<!-- devkit-previews begin sha256:70112a2a7bb5fe45e4d681efb6b71a858b8bfe58c7895449928fa76df3c749a4 -->
## Preview workflows with Devkit

Use the devkit-previews skill for development, testing, review and preview lifecycle work. Confirm the owning checkout and exact preview; run project tools through devkit exec and use bounded logs. Resuming a preview does not rebuild images or reread saved Compose. Preserve data when recreation is needed. Review the intended committed revision and report its URL, full SHA and checks. Public sharing, publishing and destructive cleanup need explicit authorization unless already given; development work does not grant Git or host-maintenance authority. Honor project-specific commands and user constraints.
<!-- devkit-previews end -->

<!-- devkit-troubleshooting begin sha256:82e1ef7105bf6bf0398f3ded917c9ddb9c2ecc085ff13e44152fe81733e70c04 -->
## Troubleshooting with Devkit

Use the devkit-troubleshooting skill for unavailable, unhealthy or stale previews, dashboard/private URLs and agent integration failures. Begin with bounded read-only evidence: version, preview identity, status, logs, history and doctor. Distinguish application/container failures from DNS, TLS, proxy and client LAN/VPN routing. Preserve HTTPS hostname verification and keep credentials out of output. Diagnostics alone do not authorize restarts, host/firewall changes or data deletion; continue repairs already authorized by the user. Verify the affected behavior and report untested routes honestly. Use devkit-vms and devkit-secrets for their respective boundaries.
<!-- devkit-troubleshooting end -->

<!-- devkit-projects begin sha256:35413f09ea1a92392458b3ae86651fd14305604301334b89aa7ff28a825ea581 -->
## Projects with Devkit

Use the devkit-projects skill when creating a project, adapting an existing application for previews, or registering an existing Compose stack. Preserve the effective devkit.json/devbox.json recipe, VM mappings, user files and data. Distinguish isolated preview storage from existing-stack registration; registration never recreates containers or grants review/share/destroy authority. Follow the skill for live/review hooks, initial GitHub push recovery and project guidance installation.
<!-- devkit-projects end -->
