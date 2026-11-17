---
name: devkit-secrets
description: Use when generating or saving development credentials, running commands that need secrets, retiring a Devkit secret scope, or integrating an external application with Devkit's secrets API or managed OpenBao.
---

# Devkit secrets

Store development credentials in Devkit so the human can find and copy them in the project's Secrets tab. Never place values in chat, commits, recipes, command arguments, history, logs, or permanent dotenv files. Do not read Devkit's backend token, root/unseal material, or unrelated host credentials. This is a trusted single-user development host; the skill is workflow guidance, not OS isolation.

Run from the intended checkout:

```sh
devkit secrets context --json
devkit secrets list --json
```

If explicitly told the project is new, enroll it with `devkit secrets resources register`. Do not rebind a mismatched/moved context automatically. Explain the mismatch and request the intended identity. Git/T3/Codex/Claude own branches and worktrees; secret cleanup never removes them.

Choose the narrowest useful scope. Worktree values override linked branch values, which override project values. Detached checkouts inherit project values only. Saving an inherited name creates a child override; never rotate/delete its parent to work around a missing child.

To generate and save a temporary development password without exposing it:

```sh
devkit secrets generate DATABASE_PASSWORD --scope worktree --length 32 --json
```

To store an already generated value, send it through stdin using a pipe from the generating program (not a literal value in shell text), or use the hidden prompt:

```sh
devkit secrets set DATABASE_PASSWORD --scope worktree
# A generating program can pipe directly into:
# devkit secrets set DATABASE_PASSWORD --scope worktree --stdin
```

Use a **login entry** when a username and password belong together. Use a descriptive key such as `dev-admin`, not the username. Keep API tokens and unrelated values as ordinary single-value secrets. Do not silently combine or delete existing separate entries.

```sh
devkit secrets set dev-admin --type login --scope worktree
# Hidden prompts collect username and password.
devkit secrets generate dev-admin --type login --scope worktree --stdin
# A producer supplies JSON with username only; Devkit generates the password.
```

For paired input from a program, `set/update --type login --stdin` reads one JSON object with `username` and `password`; generation accepts only `username`. Pipe from the producer: do not embed either value in shell arguments, literal shell commands or chat. The entire entry inherits, versions, deletes and restores together. Type cannot change under an existing name.

To inject a login, explicitly set both `--username-env APP_USERNAME --password-env APP_PASSWORD` when saving/generating it. Unmapped logins are skipped by `secrets run`; do not assume their label is an environment variable. Mappings must be distinct and cannot collide with other effective secrets. `DEVKIT_`, `VAULT_` and `BAO_` names are reserved for injection. Ordinary single-value secret names are their environment names.

A requested field can be accessed with `get dev-admin --field username|password --reveal`; never paste the result into chat. A login's `rotate --scope ... --expected VERSION` generates only a new stored password and preserves the username/mappings. Updates require both fields through protected input; omitted mappings retain an owned login's existing mappings, and explicit `--no-env` removes them. Never change an application's account unless separately authorized.

Generation returns metadata only. For updates, use `--expected VERSION` from the owned record in `list`; a version conflict means reread metadata and reconsider, never force overwrite. `rotate` changes the saved password, not the application's account. Update the application separately when authorized.

Prefer runtime injection:

```sh
devkit secrets run --scope worktree -- your-command
```

The child receives effective values by environment. Do not use environment-dumping or tracing commands. Explicit `get KEY --reveal` is for a requested use; never copy its output into the conversation. Tell the human the key, scope, and where to reveal it in Devkit instead.

`history KEY` shows metadata. `delete KEY --scope worktree` and resource retirement return a preview and confirmation token. Show the affected scope/keys and obtain authorization for destructive changes unless already explicitly authorized. `restore` recovers stored values; it does not recreate Git resources or reset a remote password. Permanent purge cannot be undone from the live vault.

Retention is configurable: branch/worktree/project durations or `never`, with a global automatic-off option. Never assume a password expires after seven days. Do not change cleanup settings, enable overdue purging, manage OpenBao, or import operator credentials as part of a routine project task. When sealed/unavailable, report it and ask the human to unseal; never fall back to plaintext storage.

## External applications

Choose the API by data ownership. Use `https://devkit.<domain>/api/secrets/v1` for
project secrets with Devkit inheritance, versions and cleanup. Use
`https://vault.devkit.<domain>/v1/` only for app-owned native OpenBao storage outside
Devkit's registry. Discover the configured domain; private DNS, LAN/Tailscale routing
and normal TLS verification must work. Neither choice authorizes public exposure.

For a backend needing inherited secrets, issue a Devkit token from the verified
registered checkout, with the narrowest useful context and permissions. Register a
new context explicitly when authorized; never repair an identity mismatch by rebinding.
For example, from the intended worktree, choose an unused private output path:

```sh
devkit secrets tokens create example-backend --scope worktree \
  --permissions read --ttl 168h --output /absolute/private/example-backend.token
devkit secrets tokens list --json
```

The output file is created mode 0600 and must not already exist or be a symlink.
Deliver it privately to server-side runtime storage; do not print it. Tokens and
returned values never belong in browser bundles, frontend build-time environment,
URLs or logs. This is a backend API: requests with an `Origin` header are refused.
Use a server-side integration rather than removing browser security controls.

Send **POST JSON** with `Authorization: Bearer <credential>` to the Devkit endpoint.
Load the credential in memory from protected storage, not literal shell arguments.
For example the body `{"operation":"list"}` returns metadata; `{"operation":"get",
"key":"APP_PASSWORD","reveal":true}` reads a value for authorized backend use.
A login also requires `"field":"username"` or `"field":"password"`. `inject`
returns effective environment pairs; it does not execute a command on the host.
Keep response values out of diagnostics. Remote clients require HTTPS; direct
loopback HTTP is allowed for local use.

The token fixes context. Omit `root`, `scope`, `project`, `branch` and `worktree`
selectors. A project token targets shared project secrets; branch/worktree tokens
include ancestor inheritance. Add `write` or `cleanup` permission only for the
requested operations; write alone does not allow secret reads. Application tokens
never authorize registration, token management, integrations or vault/host administration.

- A branch switch or moved/missing context can invalidate access. Verify the intended
  identity and issue a separate token for a new context explicitly; do not widen
  scope or substitute paths in the request. Bound branch cleanup can still target
  its original branch after switching away; it never targets the new branch.
- On expiry, create a replacement, update the consumer, verify access, then run
  `devkit secrets tokens revoke TOKEN_ID` for the old token when authorized. There
  is no renewal API. Revoked/expired credentials must not be retried as if usable.
- Updates need the owned record's current `expected` version; zero is for creation.
  Reconcile stale versions before writing. After a timeout, inspect metadata/history
  before retrying a possibly completed mutation.
- `delete`, `restore` and cleanup retire/restore/purge are two-step API operations:
  request a preview, inspect the affected target, then submit the same operation
  with its returned `confirm` token under existing authorization. Stale confirmations
  require a new preview. Child deletion never deletes inherited parent entries;
  removing an override exposes its inherited value. Cleanup never removes Git
  worktrees or enables host-wide automatic cleanup.
- A sealed/unavailable vault (503) blocks secret operations. Report the operator
  action needed; retain pending intent without plaintext fallback or blind retries.

For native OpenBao, use a dedicated app identity restricted to an app-owned path,
such as `kv/data/external/example/*`, with `X-Vault-Token` authentication. Never use
the Devkit backend or human administrator token in an application, or write directly
under `kv/data/projects/*` or `kv/data/devkit/*`. Native values do not appear in the
project's Secrets tab or follow Devkit inheritance/cleanup. KV v2 reads use GET;
writes use POST with `options.cas` (0 for creation, observed version for updates).
Policy/auth provisioning requires separate operator authority. For full protocol
details, consult the matching Devkit release's `docs/secrets-api.md`; do not assume
that file exists in the consuming application's repository.

For explicitly authorized host provisioning, vault updates, encrypted backups or
restore/recovery, use devkit-host. Project-secret access does not grant those operator
permissions. A sealed vault needs human recovery, without reading backend tokens or
unseal material. Backup checksum verification is distinct from restore rehearsal.
