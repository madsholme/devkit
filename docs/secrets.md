# Development secrets

Devkit stores development passwords in a managed OpenBao instance. An agent can save or generate a password through the CLI; you find, reveal and copy it in the project's **Secrets** tab. Settings → Secrets contains vault status, cleanup, encrypted backups, integration credentials and agent skill repair.

This is a trusted single-user development host. Project operations validate scope and cannot address system credentials. An unrestricted process running as your user, or with Docker access, is still trusted; this is not isolation between hostile agents. Existing T3, Codex, Claude Code, Git and SSH workflows remain in place.

## Storage and first setup

OpenBao uses a **bind-mounted directory**, not a Docker named volume:

- Data: `~/.local/state/devkit/secrets/openbao/data` → `/bao/data` in the container.
- Compose/server configuration: `~/.config/devkit/secrets/openbao/`.
- Restricted backend token: `~/.config/devkit/secrets/backend-token` (0600).
- Resource registry, cleanup policy and metadata audit: `~/.local/state/devkit/secrets/`.

`DEVKIT_STATE_DIR` and `DEVKIT_CONFIG_DIR` relocate their corresponding roots. Data directories are owner-only and run under your UID/GID. Recreating or updating the container keeps the bind directory. Never delete it to fix a sealed vault. Data capacity is not capped at an estimate: monitor the directory, including the audit log, as it grows. The container starts with 512 MiB memory, no additional swap, a soft CPU weight, and core dumps disabled.

The native file audit device is declared in the generated server configuration, with sensitive values HMAC-protected (`log_raw=false`) and an owner-only audit file. Provisioning verifies that device before creating the backend identity; API audit creation remains disabled.

Fresh setup defaults to OpenBao. Existing hosts retain their current provider until explicitly selected. To preserve local Devkit credentials and disable managed project secrets, use `devkit setup --secrets-provider local`. There is no parallel local implementation of project-secret versioning.

```sh
devkit setup --secrets-provider openbao
# Setup stops before collecting credentials when human initialization is needed.
devkit vault status --json
devkit vault init --shares 1 --threshold 1
devkit vault unseal
devkit vault provision
devkit setup --secrets-provider openbao
```

Choose your own share count/threshold. One share is the simple single-user option; losing it loses recovery access. Init writes recovery material only to the controlling terminal. Store it outside this host. Unseal and provisioning use hidden terminal prompts, not command arguments or web jobs. Provisioning establishes a human `userpass` login (`devkit-admin`), a restricted renewable backend identity, and revokes the bootstrap token. Devkit does not save unseal keys or the bootstrap token. Native administrator password custody is yours.

After a restart, run `devkit vault unseal` for each required share. The backend token is renewed while Devkit web runs; if it has expired after extended downtime, use a human administrator token with `vault provision` to replace it. Recovery through the local CLI does not depend on Traefik, Cloudflare, or a dashboard session. A sealed/unavailable vault fails closed; signing in and authenticated dashboard actions also depend on the vault-backed session key.

## LAN and Tailscale access

`devkit vault ui` prints `https://vault.devkit.<domain>/ui/`. Traefik requests an exact certificate for this nested hostname and routes it only on explicit private listeners. The existing `*.<domain>` certificate alone does not cover it. Keep Traefik on port 443; do not turn T3's Tailscale Serve back on.

Use the Tailscale IP for remote tailnet DNS. For direct LAN clients, add the host's stable LAN address to `proxy.listen` and configure local split DNS for the vault hostname to that LAN address. Clients on your LAN do not need Tailscale. `devkit doctor` probes the configured private listeners and exact certificate; only a client on each network can confirm its DNS/firewall path.

DNS credentials are fetched into a private memory-backed runtime directory, then mounted read-only for Traefik. Cloudflared uses a runtime token file too. Certificate/ACME state remains in Traefik's existing protected local volume. A running proxy can continue serving existing certificates while the vault is sealed; fresh credential delivery and issuance/renewal require an unsealed vault. After reboot, unseal and run `devkit proxy up` to repopulate runtime files. There is no persisted plaintext token cache.

## Save and use a password

Run from the intended Git checkout. Enrollment is explicit and records project, branch and worktree identities without changing Git:

```sh
devkit secrets resources register
devkit secrets context --json
devkit secrets list --json
devkit secrets generate DATABASE_PASSWORD --scope worktree --length 32
devkit secrets set API_KEY --scope project             # hidden input
# A producer can pipe its output into this command; never put a literal secret in shell text:
# producer | devkit secrets set API_KEY --scope project --stdin
devkit secrets get DATABASE_PASSWORD                   # metadata only
devkit secrets get DATABASE_PASSWORD --reveal          # explicit plaintext
devkit secrets run --scope worktree -- your-command
```

Generation does not print its value. The dashboard lists name, version, owner, inheritance and effective source; values are fetched only on Reveal/Copy and hidden after 30 seconds or context/logout changes. Clipboard history may retain copied values. `secrets run` injects effective values into the child environment and preserves its exit status/signals. Avoid environment dumps, tracing, or programs that log their credentials.

Inheritance is **worktree > linked branch > project**. Detached worktrees inherit project values only. Branch selection is available in the Secrets panel; worktree details also have their own Secrets tab. Optional `--project ID`, `--branch ID`, and `--worktree ID` selectors use registered IDs and remain constrained to the current project.

Writes require an explicit scope. Saving the same name in a child makes an override. Updating/rotating an existing owned key requires `--expected VERSION` from `list`; conflicts require a refresh. `history KEY` returns version metadata only. Stored-password rotation does not change the password in an application or revoke a remote API token.

Moved worktrees and renamed/recreated branches need explicit `resources bind ID` preview/confirmation. `resources reconcile` reports missing or ambiguous Git resources without deleting secrets. Missing inventory is not proof of deletion. Branches with unavailable/rewritten reflog identity require investigation; Devkit does not silently transfer old secrets. Temporary Devkit review checkouts cannot register secret scopes. Non-Git folders do not currently support secret contexts.

Use Devkit for contextual writes. Native OpenBao administration is available for advanced recovery, but direct edits to Devkit-managed KV paths can create version/registry conflicts requiring reconciliation; listing arbitrary native keys is not a registry import.

## Cleanup is configurable

Defaults: branch 7 days, worktree 7 days, project 30 days, automatic cleanup enabled. Each retention accepts a positive duration from 1 hour through 36,500 days, or `never`.

```sh
devkit secrets cleanup config show --json
devkit secrets cleanup config set --automatic off
# Review the result, then repeat with --confirm PREVIEW_ID.
devkit secrets cleanup config set --branch-retention never --worktree-retention 14d --project-retention 90d
devkit secrets cleanup status --json
```

**Automatic off means no automatic retirement, deletion or purge.** `never` prevents automatic expiry for that scope type. Explicit deletion, retirement, restoration and confirmed permanent deletion remain available. These controls do not use OpenBao TTL deletion that could bypass them. At 1,000 stored versions, further writes are refused to prevent silent version eviction; manage the key explicitly before continuing.

```sh
devkit secrets delete KEY --scope worktree              # preview first
devkit secrets restore KEY --scope worktree
devkit secrets cleanup preview RESOURCE_ID
devkit secrets cleanup retire RESOURCE_ID
devkit secrets cleanup restore RESOURCE_ID
devkit secrets cleanup purge RESOURCE_ID                # irreversible live deletion
```

Repeat a previewed mutation with its `--confirm PREVIEW_ID`. Retirement starts the recovery window at confirmation, affects only owned keys, and never deletes Git/files. Project retirement cascades to descendants without shortening existing longer/indefinite retirement protection. A branch still used by a worktree cannot be retired. Restoring a scope preserves independently deleted keys and independently retired descendants.

Settings changes apply to new retirements. Turning automation off retains existing deadlines without acting on them. Turning it on creates a barrier: `cleanup resume` previews overdue resources before an explicit confirmation permits catch-up. A `never` policy still blocks automatic purge. `secrets cleanup reconcile` resumes an interrupted, previously authorized operation. Backup copies may retain deleted values independently of cleanup policy.

## Integration credentials and existing-host import

```sh
devkit integrations cloudflare configure                 # hidden input
# producer | devkit integrations cloudflare configure --token-stdin
devkit integrations cloudflare status --json
devkit integrations cloudflare get --reveal
devkit integrations cloudflare rotate-token              # saves an already issued token
devkit integrations github configure
devkit integrations github status --verify
```

Cloudflare `token` binds the private proxy's DNS credential. A share token may need broader Tunnel permissions; keep it separate and point `share.token.openbao` to its saved integration reference. GitHub verification checks token identity, not access to every repository. Tailscale API credentials can be stored explicitly as an integration; normal Tailscale connectivity needs no API credential. SSH keys/agent are unchanged. Managed DNS runtime delivery currently supports Cloudflare.

Existing source files are imported only by explicit preview/confirmation:

```sh
devkit vault migrate --source dns --remove-source
devkit vault migrate --source share --remove-source
devkit vault migrate --source session --remove-source
# Repeat each with --confirm PREVIEW_ID after reviewing it.
```

Import verifies private readback and consumer read access before switching a reference. It checks DNS zone access (plus tunnel account access for shares); real issuance/tunnel creation still need operational validation. File removal is optional and refuses an observed changed original. Env/command sources are not executed during discovery/import; configure their values explicitly through protected stdin. Old backups/copies are not erased. Managed consumers reject legacy plaintext sources rather than falling back.

For generic Git HTTPS, save a **project-owned** credential and bind one exact remote:

```sh
devkit secrets set GIT_PASSWORD --scope project
devkit integrations git bind --url https://git.example.com/team/repo.git --key GIT_PASSWORD --username YOUR_LOGIN
# Review, then repeat with --yes.
devkit integrations git status --url https://git.example.com/team/repo.git
devkit integrations git unbind --url https://git.example.com/team/repo.git --yes
```

Bindings match protocol, host, port, repository path and username. They reset helpers only for that URL, enable `useHttpPath`, and preserve unrelated/global helpers. Tokens never enter Git config/URLs/argv. Existing URL-specific settings block binding. A failed bound lookup tells Git to stop rather than consult stale helpers. `store`/`erase` do not silently write or delete vault credentials. A moved checkout or changed helper configuration needs explicit rebinding/repair; operator GitHub credentials are not silently shared with projects.

## Backups and updates

Install `age` and `age-keygen` (`sudo dnf install age` on Fedora, or `sudo apt-get install age` on Debian/Ubuntu). Create its recovery key outside Devkit and retain the private key separately; Devkit stores only the public recipient. Choose an absolute mounted off-host backup directory:

```sh
devkit vault backup configure --directory /mnt/backup/devkit-vault --recipient age1YOUR_RECIPIENT --daily on --retain 30
devkit vault backup create
devkit vault backup list --json
devkit vault backup verify BACKUP_ID
devkit vault update --check
devkit vault update --image openbao/openbao:VERSION --check
devkit vault update --image openbao/openbao:VERSION --yes
```

`--check` without an image reports the currently installed and compiled candidate image; it does not poll a release service. Updates are explicit, accept a newer patch or next minor within the same major, require a successful encrypted backup, preserve the bind directory, and verify the expected running version. A sealed upgraded instance is reported as needing manual unseal. No automatic updates or downgrade of a migrated directory. The [Docker upgrade profile](testing.md#openbao-lite) validates 2.7.1 → 2.7.2 and recovery of its pre-upgrade backup into fresh 2.7.1 storage. Other version transitions need separate validation.

Daily backups run while `devkit web` is running. They stream a Raft snapshot and local recovery metadata through `age`, capture registry/cleanup settings, and encrypt Traefik ACME state when configured. Capturing ACME briefly stops Traefik and attempts restart on every exit path. Incomplete backups are not published. Retention deletes only verified copies belonging to this installation; unknown files stop pruning. Unseal keys, decryption keys, and backend token files are excluded. This is not a full host backup: keep host configuration, Git checkouts/recipes, instruction files and Git helper bindings separately.

`backup verify` checks encrypted component checksums, **not successful decryption or restoration**. The [encrypted Docker acceptance profile](testing.md#openbao-lite) exercises fresh-instance recovery with generated secrets and an offline test certificate; it does not verify your real backup or public certificate issuance. Test recovery before relying on a backup:

1. Preserve the live bind directory. Use a disposable Docker instance with separate storage, ports and Compose project (on the same or a separate host), running the backup's exact OpenBao image; never test a downgrade against the live directory.
2. Verify checksums, decrypt components with the human-held `age` key into a private memory-backed temporary directory, and inspect `host.json`. Do not copy recovery material into an agent chat.
3. Initialize/unseal the disposable instance with temporary recovery credentials. With its human administrator token, restore the original snapshot through OpenBao's `operator raft snapshot restore -force` procedure. The restored instance then requires the **original snapshot's unseal shares**.
4. Stop its Devkit web service while restoring the matching `registry.json` and `cleanup.json` as owner-only files under its state directory. Preserve installation/resource IDs. Set automatic cleanup off during validation. Rebind verified checkout paths explicitly; do not map resources by names alone.
5. Log in with the restored native administrator, provision a fresh restricted backend identity, and verify a known key, version history, inheritance, recovery settings, and a sample restoration. Reconfigure host references/agent guidance/Git bindings as needed for the new host.
6. Restore decrypted ACME files into the intended proxy certificate volume with Traefik stopped and original restrictive permissions. Verify the volume owner and certificate hostname before starting it. Validate LAN and Tailscale DNS/TLS from their clients.
7. Only after that validation choose a production cutover. Restore the compatible pre-update backup into a fresh directory if an update must be rolled back. Keep the failed directory until recovery is confirmed; never automatically downgrade it.

See OpenBao's [Raft storage API and snapshot recovery](https://openbao.org/api-docs/system/storage/raft/) and [integrated storage](https://openbao.org/docs/configuration/storage/raft/). The pinned image tracks the reviewed [2.7.2 release](https://github.com/openbao/openbao/releases/tag/v2.7.2); actual deployment/recovery validation remains host-specific.

## Agent guidance and diagnostics

Managed setup installs the embedded versioned `devkit-secrets` skill into `~/.agents/skills/devkit-secrets` for Codex and `~/.claude/skills/devkit-secrets` for Claude Code. New project templates include bounded guidance. Existing projects opt in explicitly:

```sh
devkit agents skills install --project /absolute/path/to/checkout
devkit agents skills status --json
devkit agents skills repair
devkit doctor --agents
devkit doctor
```

`doctor --agents` checks skill status, CLI availability, current project context and guidance. Installation is idempotent and preserves unrelated instructions. Edited/unmanaged skill files and modified managed instruction blocks are not overwritten. Resolve those changes yourself before reinstalling. Restart an agent session after installation so discovery refreshes. The skill tells agents to check context/list, choose scope, save without printing values, and use runtime injection. Fresh-session discovery still needs validation in the actual Codex and Claude environments; installing a file alone does not prove discovery.
