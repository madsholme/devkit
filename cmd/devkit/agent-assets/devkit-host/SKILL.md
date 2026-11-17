---
name: devkit-host
description: Use when provisioning, upgrading or maintaining a Devkit host, changing its private proxy, or operating managed OpenBao backups, restore rehearsals, cutover and recovery.
---

# Devkit host operations

Establish the exact host and authorized maintenance before changing services. Project
credential tasks belong to devkit-secrets; preview diagnostics alone grant no host
administration. Continue already authorized work without asking again unnecessarily.

## Establish the installation

Read the host's `~/.config/devkit/STATUS.md` when available and the matching Devkit
release's setup/secrets documentation. Those files may not exist in the consuming
application repository. Identify source-build versus release-bundle installation,
actual binary path/version, served version and service state before selecting commands.
Use bounded read-only evidence:

```sh
devkit version
devkit doctor --json
devkit resources --json
devkit history -n 20 --json
devkit vault status
```

Inspect `complete` and failed checks; absent evidence is not health. Reports exclude
secret values; raw service/application logs may contain them. Never dump environments,
backend tokens, recovery files or arbitrary configuration into chat or job output.

## Provisioning and upgrades

Use the matching release's supported installation path. Bootstrap installs system
prerequisites with sudo only for explicitly requested provisioning; Devkit itself
never uses sudo. Source installations require the documented Go/frontend build steps;
bundles use the intended architecture and explicit release. Retain the previous
binary/configuration and record service state for rollback. Rebuilding an application
does not authorize rebuilding or restarting Devkit. After authorized deployment,
verify the served version and affected behavior, not merely the build exit code.

Traefik owns private HTTPS port 443. Preserve configured private LAN/Tailscale
listeners and DNS/TLS verification; never re-enable Tailscale Serve on that listener
or expose public ports as a repair. `devkit proxy up` renders and starts the proxy;
use it only within authorized host scope, considering interruption. A host-side
`devkit check PREVIEW --json` proves only primary-route observations; verify actual
LAN and away/VPN client access separately. Keep firewall and certificate changes
explicit. A sealed vault may leave existing certificates working while blocking
fresh credential delivery/renewal. Have the human restore vault access; no plaintext
credential fallback.

## Backup and managed OpenBao updates

Inspect configured encrypted backup destination/recipients and protected recovery-key
availability without reading key contents. `devkit vault backup create` creates an
encrypted backup; capturing configured ACME state briefly stops Traefik. Account for
that interruption. `devkit vault backup verify BACKUP_ID` checks encrypted checksums,
not decryption or restore success. Backups exclude unseal/decryption keys and backend
token files and are not full host backups: retain host config, Git checkouts/recipes
and agent instructions separately.

`devkit vault update --check` inspects the installed/compiled candidate, not an online
latest release. Inspect an explicit candidate with `--image openbao/openbao:VERSION
--check`; an authorized `--yes` update requires an encrypted backup and preserves the
data directory. Follow supported version transitions and verify the running version.
A sealed result needs human unseal. Never downgrade a migrated directory or infer
that a successful checksum check makes an untested real backup recoverable.

## Rehearsal, replacement and interruption

First inspect `devkit vault backup restore BACKUP_ID`. Rehearse that exact backup:

```sh
devkit vault backup restore BACKUP_ID --test --identity /private/recovery.agekey --admin-user YOUR_USERNAME
```

The human enters the backup's original unseal shares and native administrator password
at hidden terminal prompts. Never request their contents in chat, flags, environment
or pipelines. The identity path must refer to a private owned regular file. Rehearsal
uses isolated temporary storage/networking; it leaves the configured vault unchanged.
Record what actually passed, without claiming a different backup was tested.

Replacement is separately authorized, same-host work with matching installation,
domain and exact data path. Stop the managed web service and any manual web process
explicitly; restore never stops/starts web for you. Apply the same command with
`--yes` instead of `--test`; it repeats rehearsal before replacement. Preserve retained
old data and recovery metadata. After verification of vault status, recovered entries
and integrations without disclosure, explicitly resume the web service. App API tokens
are revoked, browser/backend identities refreshed and automatic cleanup left off;
re-enroll external clients and review retention before explicitly re-enabling cleanup.
Native OpenBao identities follow snapshot expiry/state semantics, not a blanket
revocation guarantee. Replacement-host migration is unsupported.

On interruption, keep web stopped, retain the journal and both data sets, inspect the
reported operation ID and use the documented recovery path. Authorized rollback uses
`devkit vault backup restore --rollback OPERATION_ID --yes --admin-user YOUR_USERNAME`
and the previous vault's credentials, which may differ from the backup's. It retains
token revocations and cleanup-off state; later writes are not merged. Do not delete
the journal, force ownership changes, purge retained data or blindly rerun replacement.
