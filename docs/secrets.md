# Development secrets

Devkit stores development passwords in a managed OpenBao instance. An agent can save or generate a password through the CLI; you find, reveal and copy it in the project's **Secrets** tab. Settings → Secrets contains vault status, cleanup, encrypted backups, integration credentials and agent skill repair.

This is a trusted single-user development host. Project operations validate scope and cannot address system credentials. An unrestricted process running as your user, or with Docker access, is still trusted; this is not isolation between hostile agents. Existing T3, Codex, Claude Code, Git and SSH workflows remain in place.

## How the vault and Devkit work together

OpenBao owns encrypted KV versions and persistent Raft data. Devkit keeps a private metadata registry mapping projects, branches and worktrees to those entries; its CLI, dashboard and external secrets API all use that registry for inheritance, version checks and cleanup. A paired login stores username/password together in one entry; ordinary values remain supported. Devkit's own DNS/integration/session credentials use separate system paths.

External applications can use [scoped Devkit API tokens](secrets-api.md) to get the same project inheritance and cleanup behavior. The native OpenBao HTTPS API is also available for separately managed app-owned paths, with independent OpenBao authentication. Direct native writes to Devkit-owned paths are unsupported because they bypass its registry. The API guide includes issuance/revocation, permissions, request examples, a Python client and a native OpenBao policy example. Both routes use existing LAN/Tailscale HTTPS access; no public exposure is enabled.

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
```

Setup asks for share count/threshold (defaults 1/1), confirms that you saved recovery material off-host, then prompts for unseal shares, the bootstrap token and your chosen admin password. No separate commands or setup restart are needed. Rerun setup after interruption to resume from live state. `--yes` stops when human input is required. Standalone `vault init`, `vault unseal` and `vault provision` remain available for recovery.

Choose your own share count/threshold. One share is the simple single-user option; losing it loses recovery access. Init writes recovery material only to the controlling terminal. Store it outside this host. Unseal and provisioning use hidden terminal prompts, not command arguments or web jobs. Provisioning establishes a human `userpass` login (`admin`), a restricted renewable backend identity, and revokes the bootstrap token. Devkit does not save unseal keys or the bootstrap token. Existing installations retain their previously provisioned username (`devkit-admin` on earlier versions); updating Devkit does not rename accounts or change passwords. Native administrator password custody is yours. API timeouts start after credential entry, so taking time at a hidden prompt does not expire the request. Provisioning briefly retries the known KV initialization response on first startup; other KV configuration failures stop with a stage-specific error. If provisioning stops before completion, retry it with the original bootstrap token—do not initialize the vault again. Root is revoked only after backend verification succeeds.

After a restart, run `devkit vault unseal` for each required share. The backend token is renewed while Devkit web runs; if it has expired after extended downtime, use a human administrator token with `vault provision` to replace it. Recovery through the local CLI does not depend on Traefik, Cloudflare, or a dashboard session. A sealed/unavailable vault fails closed; signing in and authenticated dashboard actions also depend on the vault-backed session key.

## LAN and Tailscale access

`devkit vault ui` prints `https://vault.devkit.<domain>/ui/`. Completing `devkit setup --secrets-provider openbao` automatically configures this route and requests its exact certificate through the existing Cloudflare DNS resolver. Setup waits for trusted certificates for both the dashboard and vault before continuing; a vault certificate failure stops setup and identifies the hostname. Traefik routes the vault only on explicit private listeners. `devkit vault setup` alone starts the container; complete the full setup flow above to configure HTTPS. The existing `*.<domain>` certificate alone does not cover it. Keep Traefik on port 443; do not turn T3's Tailscale Serve back on.

Use the Tailscale IP for remote tailnet DNS. For direct LAN clients, add the host's stable LAN address to `proxy.listen` and configure local split DNS for the vault hostname to that LAN address. Clients on your LAN do not need Tailscale. `devkit doctor` probes the configured private listeners and exact certificate; only a client on each network can confirm its DNS/firewall path.

DNS credentials are fetched into a private memory-backed runtime directory, then mounted read-only for Traefik. Cloudflared uses a runtime token file too. Certificate/ACME state remains in Traefik's existing protected local volume. A running proxy can continue serving existing certificates while the vault is sealed; fresh credential delivery and issuance/renewal require an unsealed vault. After reboot, unseal and run `devkit proxy up` to repopulate runtime files. There is no persisted plaintext token cache.

## Save and use a password

Run from the intended Git checkout. Enrollment is explicit and records project, branch and worktree identities without changing Git:

You can also run commands from a subdirectory of that checkout. Running them from a non-Git directory such as `~` reports: `Run this command from a project's Git checkout: cd /path/to/project, then try again.`

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

Generation does not print its value. The dashboard lists name, version, owner, inheritance and effective source; values are fetched only on Reveal/Copy. Reveal displays the value within its own row and hides it after 30 seconds or context/logout changes. Each row has a copy icon (labeled Copy username, Copy password or Copy value according to its name); copying writes directly to the clipboard without displaying the value, and briefly shows a checkmark. Clipboard history may retain copied values. `secrets run` injects effective values into the child environment and preserves its exit status/signals. Avoid environment dumps, tracing, or programs that log their credentials.

The project Secrets tab badge counts active saved entries across all project scopes (overrides count separately); deleted entries are excluded. Worktree tab badges count entries saved in that worktree. Counts require sign-in, and an unavailable count is shown as a dash rather than zero. The panel identifies the selected branch and checkout path, lets you choose a checkout, and reports the effective entries in that scope, including inherited values.

Inheritance is **worktree > linked branch > project**. Detached worktrees inherit project values only. Branch selection is available in the Secrets panel; worktree details also have their own Secrets tab. Optional `--project ID`, `--branch ID`, and `--worktree ID` selectors use registered IDs and remain constrained to the current project.

Writes require an explicit scope. Saving the same name in a child makes an override. Updating/rotating an existing owned key requires `--expected VERSION` from `list`; conflicts require a refresh. `history KEY` returns version metadata only. Stored-password rotation does not change the password in an application or revoke a remote API token.

Moved worktrees and renamed/recreated branches need explicit `resources bind ID` preview/confirmation. `resources reconcile` reports missing or ambiguous Git resources without deleting secrets. Missing inventory is not proof of deletion. Branches with unavailable/rewritten reflog identity require investigation; Devkit does not silently transfer old secrets. Temporary Devkit review checkouts cannot register secret scopes. Non-Git folders do not currently support secret contexts.

Use Devkit for contextual writes. Native OpenBao administration is available for advanced recovery, but direct edits to Devkit-managed KV paths can create version/registry conflicts requiring reconciliation; listing arbitrary native keys is not a registry import.

## Paired logins and single values

Choose **Single value** for an API token or one environment value. Choose **Login (username + password)** when the two fields describe one account. A login named `dev-admin` is stored as one OpenBao KV entry with `username` and `password` fields; the username is not its key name. Existing separate username/password entries remain unchanged. Each login is one row and one counted entry, with independent reveal/copy controls for both fields. Copying does not reveal the field on screen.

```sh
# Hidden prompts for both fields:
devkit secrets set dev-admin --type login --scope worktree
# Hidden username prompt; generate the password and return metadata only:
devkit secrets generate test-admin --type login --scope worktree --length 32
# Explicitly reveal one field:
devkit secrets get dev-admin --field username --reveal
devkit secrets get dev-admin --field password --reveal
# Rotate only the stored password, preserving username and mappings:
devkit secrets rotate dev-admin --scope worktree --expected 1
```

For programmatic input, add `--stdin`: `set/update --type login` accepts one JSON object with string `username` and `password` fields; `generate --type login` accepts `username` only. Use a pipe from the credential producer or a protected input file, never literal values in shell arguments/history. Neither field is included in routine metadata, audit events or the registry. Explicit reveal prints the selected field to your terminal; use runtime injection when possible.

The generic Git HTTPS helper continues to bind a single-value project token and its separately configured username; paired logins do not automatically replace existing Git bindings.

Logins used only for human sign-in need no environment mapping. To use one in a child process, supply both mappings when saving/generating:

```sh
devkit secrets set dev-admin --type login --scope worktree \
  --username-env APP_USERNAME --password-env APP_PASSWORD
devkit secrets run -- your-command
```

Unmapped logins are skipped by `secrets run`. Mapped logins supply the two named variables; a collision with any other effective entry stops injection before the child starts. Reserved `DEVKIT_`, `VAULT_` and `BAO_` prefixes are refused. Updates with omitted mappings preserve an owned login's mappings; `--no-env` explicitly removes them. The dashboard exposes the same mapping toggle and field names. An override selects the whole login, not a mixture of parent and child fields. Existing entry types cannot be changed under the same effective name; create a new entry instead. Delete/restore, retirement and purge apply to both fields together. Rotation does not change the real application's password.

Legacy single-value entries and registry schema 1 remain readable. The first login write upgrades the registry to schema 2 to prevent older binaries from overwriting typed entries. After that, use a login-capable Devkit binary: a binary-only downgrade cannot resume project-secret operations. Keep the current vault and registry together during backup/recovery; do not restore an older registry alone. No automatic combination, conversion or deletion of existing entries occurs.

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

Cloudflare `token` binds the private proxy's DNS credential. A share token may need broader Tunnel permissions; keep it separate and point `share.token.openbao` to its saved integration reference. GitHub verification checks token identity, not access to every repository. Explicit [project creation](projects.md#create-a-project) can use this host integration to create a GitHub repository and optionally push its initial commit; it does not install or share the credential in the new project. Tailscale API credentials can be stored explicitly as an integration; normal Tailscale connectivity needs no API credential. SSH keys/agent are unchanged. Managed DNS runtime delivery currently supports Cloudflare.

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

## GitHub token for project creation

The host GitHub integration lets **Create Project** create a private repository and,
when selected, push its initial commit. The owner and push option are chosen per
project; saving a token does not set a default owner or enable automatic pushing.

### Create the token

Sign in to GitHub as the account that will create the repositories, then open
[Generate a fine-grained personal access token](https://github.com/settings/personal-access-tokens/new).
Give it a descriptive name such as `Devkit server` and an expiration, such as 90 days.

| Setting | Value |
|---|---|
| Resource owner | Your personal account, or the organization where you will create repositories |
| Repository access | **All repositories**, so the token covers newly created repositories |
| Repository creation | **Read and write**; if this permission is unavailable, use **Administration: Read and write** |
| Contents | **Read and write**, needed for the initial push |
| Metadata | Keep the automatically included **Read-only** permission |

All-repository access also grants these permissions on the owner's existing repositories.
Organization policy may require approval or restrict token use. GitHub documents the
accepted creation permissions in its [repository API reference](https://docs.github.com/en/rest/repos/repos#create-a-repository-for-the-authenticated-user)
and token setup in its [personal access token guide](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens).
A classic token with the `repo` scope is an alternative, but grants broader access to
repositories the account can access.

### Save it in devkit

Generate the token and copy it directly into **Secrets → Integrations** on your devkit
host. Set **Service** to `github`, **Key** to `token`, paste the value into **Token**,
and select **Save credential**. This stores the credential in OpenBao and configures
`integrations.github.token`. Keep the token out of project files, chat and shell arguments.

The equivalent host CLI command prompts for hidden input:

```sh
devkit integrations github configure
devkit integrations github status --verify
```

Verification checks that GitHub accepts the stored credential; it does not create a
repository or push a commit. A successful identity/read check, including a repository
response showing `permissions.push`, does not prove the fine-grained token has every
required creation/write grant. A successful create-and-push operation is the definitive
end-to-end check. Replace an expired or revoked token through the same integration form,
or use `devkit integrations github rotate-token` to save an already issued replacement.

### Create a private repository and push

In **Projects → Create Project**, enable **Create GitHub repository**, enter the owner
matching your token's resource owner and a new repository name, leave **Public repository**
unchecked, and select **Push initial commit**. For a personal account, the authenticated
GitHub user must match the owner. Organization access remains subject to GitHub permissions.

The equivalent CLI is:

```sh
devkit project create my-app --github-owner YOUR_USER_OR_ORG --github-repo my-app --github-push
```

The initial push includes the starter files and additional agent instructions. Omitting
`--github-push` leaves the commit local and creates an empty remote. The host token is
used only for creation and the optional initial push; later Git operations need their
own configured credentials. See [project creation](projects.md#create-a-project) for
partial-failure recovery and the existing integration section above for exact project
Git credential bindings.

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

Managed setup installs the embedded versioned `devkit-secrets` skill into `~/.agents/skills/devkit-secrets` for Codex and `~/.claude/skills/devkit-secrets` for Claude Code. New project templates include bounded guidance. Existing projects opt in explicitly. After upgrading Devkit, rerun the project installation command below to refresh unedited managed blocks in both `AGENTS.md` and `CLAUDE.md`; updating the template does not change existing repositories:

```sh
devkit agents skills install --project /absolute/path/to/checkout
devkit agents skills status --json
devkit agents skills repair
devkit doctor --agents
devkit doctor
```

`doctor --agents` checks skill status, CLI availability, current project context and guidance. Installation is idempotent and preserves unrelated instructions. Edited/unmanaged skill files and modified managed instruction blocks are not overwritten. Resolve those changes yourself before reinstalling. Restart an agent session after installation so discovery refreshes. The skill tells agents to check context/list, choose scope, save without printing values, and use runtime injection. Fresh-session discovery still needs validation in the actual Codex and Claude environments; installing a file alone does not prove discovery.
