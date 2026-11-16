# Agent notes

This project runs on a devkit host: the app and its services run in Docker, one
preview per Git worktree, at a private HTTPS URL on the tailnet. The host may not
have the project's language or tools installed; the containers do.

Keep the "Previews with devkit" guidance when adapting this project. Update "This
project" as the app changes; additional project instructions can follow below.

## Previews with devkit

- Work in your own worktree and run every `devkit` command from it. devkit acts on
  the current worktree's preview; never act on another worktree's.
- Run project commands (tests, linters, migrations, package managers, scripts) in
  the container: `devkit exec <command...>`, or `devkit exec -s <service> ...` for
  another Compose service (e.g. the database). Files it writes belong to you.
- `devkit exec` needs this worktree's live preview. If it says there is none, run
  `devkit live`; it prints the preview URL. Starting a live preview is fine whenever
  you need one.
- The live preview mounts the worktree's files, so edits show up as the app's own
  reload allows. It does not rebuild images: after changing a Dockerfile, dependencies
  or Compose files, inspect `devkit rebuild PREVIEW --json`. Apply an authorized
  supported update with `--yes`; it preserves storage and URL. Unsupported storage
  changes require explicit migration work. Never destroy data to refresh code.
- Read logs with `devkit logs` (the last 200 lines). Never use `devkit logs -f`: it
  does not return.
- To hand work over for review: commit, then run `devkit review HEAD`. It builds from
  the committed files only, so uncommitted changes are not in it. Report the URL it
  prints and the full commit SHA.
- `devkit ls` lists previews and their URLs; `devkit history -n 10` shows what devkit
  did recently, including failures.

Ask first, and only do when asked:

- `devkit destroy`, `devkit gc --yes` and `devkit prune --yes`: they delete data or
  affect the whole host.
- `devkit share`: it makes a preview public on the internet.
- `devkit publish`: it deploys to the team.

Never:

- run `docker`, `docker compose` or the app directly on the host to get around devkit;
- publish ports, set `container_name` or `network_mode` in Compose files, or put
  host secrets into an image;
- create or remove worktrees, or commit, push or merge, unless asked (devkit only makes the initial commit during explicit project creation);
- fix the host yourself (Traefik, DNS, certificates, `devkit setup`). Report what
  `devkit doctor` says instead.

When editing Compose files: every service needs a health check and named volumes; the
review file uses the built image (`${DEVKIT_IMAGE}`) with no `build:` and no source
mounts. `DEVKIT_URL` is the preview's URL; while shared, the web service also gets
`DEVKIT_PUBLIC_URL`. Hooks (`prepare`, `check`, `build`, `publish`) live in
`devkit.json`.



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

## This project

<!-- devkit-vms begin sha256:43e8aa075ab6c5523fd048b7a1ff116674f13b9de3a1c9dc743db51ead6d677d -->
## VM connections with Devkit

Use the devkit-vms skill whenever creating, changing, starting or connecting to QEMU/libvirt VMs on this host. Default new graphical guests to SPICE without a display password unless the user requests one, with fixed per-VM ports and both direct access and optional SSH tunnels. Preserve existing display passwords unless removal is explicitly authorized. Discover actual host addresses and show separate At home (LAN) and Away (Tailscale/VPN) connection details; a public dashboard URL is not a VM route. Prefer explicit private binds. A wildcard serving both networks requires a verified private-interface/source/destination firewall guard before opening any listener; never expose public ports or disable guest SSH authentication. Use devkit-secrets when display credentials are requested. Preserve running VMs during discovery/UI work; launcher changes take effect on the next QEMU start, and restarts require explicit authorization unless already given. Preserve project-specific instructions and report untested client routes honestly. Use devkit project validate --runtime vms --json and devkit restart --vm TARGET --dry-run --json to inspect exact authority and blockers. Opaque launcher settings and stopped guest networking remain unknown; there is no VM rebuild or new disk inspection.
<!-- devkit-vms end -->

<!-- devkit-previews begin sha256:0425d77ccc42ace0afb94c2ac80c2137deefe49a9f11601ae62cd859421464cf -->
## Preview workflows with Devkit

Use the devkit-previews skill for development, testing, review and preview lifecycle work. Confirm the owning checkout and exact preview; run project tools through devkit exec and use bounded logs. Resuming a preview does not rebuild images or reread saved Compose. Preserve data when recreation is needed. Review the intended committed revision and report its URL, full SHA and checks. Public sharing, publishing and destructive cleanup need explicit authorization unless already given; development work does not grant Git or host-maintenance authority. Honor project-specific commands and user constraints. Use devkit context --json and inspect devkit rebuild PREVIEW --json before an authorized --yes application. Rebuild retains storage and identity; app code can still migrate data. Interrupted rebuilds require --recover inspection and explicit same-candidate recovery, never automatic rollback or clearing records.
<!-- devkit-previews end -->

<!-- devkit-troubleshooting begin sha256:149627b7dc0761ee72a8e99e71de1d863da9ba8803df15bffa304d14f8067386 -->
## Troubleshooting with Devkit

Use the devkit-troubleshooting skill for unavailable, unhealthy or stale previews, dashboard/private URLs and agent integration failures. Begin with bounded read-only evidence: version, preview identity, status, logs, history and doctor. Distinguish application/container failures from DNS, TLS, proxy and client LAN/VPN routing. Preserve HTTPS hostname verification and keep credentials out of output. Diagnostics alone do not authorize restarts, host/firewall changes or data deletion; continue repairs already authorized by the user. Verify the affected behavior and report untested routes honestly. Use devkit-vms and devkit-secrets for their respective boundaries. Prefer context/ls/doctor --json and doctor --agents --json for structured evidence. Inspect complete and failed checks; distinguish saved configuration, source HEAD and observed images. Use rebuild --recover diagnostics after interrupted application.
<!-- devkit-troubleshooting end -->

<!-- devkit-projects begin sha256:872fd960d0c0607204eac7c954fba2af27403910d25c94cb8f25ac25be416f78 -->
## Projects with Devkit

Use the devkit-projects skill when creating a project, adapting an existing application for previews, or registering an existing Compose stack. Preserve the effective devkit.json/devbox.json recipe, VM mappings, user files and data. Distinguish isolated preview storage from existing-stack registration; registration never recreates containers or grants review/share/destroy authority. Follow the skill for live/review hooks, initial GitHub push recovery and project guidance installation. Use devkit project validate --json before launch and devkit context --json for exact checkout associations; missing inputs or incomplete evidence are not permission to launch.
<!-- devkit-projects end -->
