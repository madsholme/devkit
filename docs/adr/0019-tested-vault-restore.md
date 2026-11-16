# 0019 — Tested same-host vault restore

Status: accepted, 2026-11-16. Extends [0013](0013-openbao-secrets-boundary.md)
and [0016](0016-external-secret-api-tokens.md).

Encrypted component checksums do not demonstrate that OpenBao can unseal, that its
native administrator can authenticate, or that the local registry matches Raft data.
Replacing a bind directory without proving those facts risks losing the working
vault and obscuring which metadata belongs to which database.

`vault backup restore` therefore defaults to read-only inspection. `--test` decrypts
into private tmpfs and restores a disposable instance on an internal local Docker
network without published ports. It checks the original unseal material, native login,
restricted backend policy and registry/version consistency. `--yes` always repeats
that test before replacement; there is no cached approval or test-bypass flag.

Recovery is same-host only. Installation, domain and exact data path must match;
missing/corrupt local registry identity needs an explicit matching installation ID.
Current host configuration and TLS certificates are preserved, except for the vault
image necessarily selected by the snapshot. Certificate archives are validated but
manual certificate restoration and host migration remain separate operations.

The operator stops the web service explicitly. Under the secret-state lock, Devkit
verifies exact container/bind ownership and directory identities, writes a durable
private journal, stops the managed vault, retains old data/metadata, and installs the
tested replacement. Incomplete journals block normal secret operations and maintenance.
Explicit rollback uses only the latest journal, revalidates ownership and retained
metadata, and keeps both data sets. It does not silently resume an ambiguous mutation
or downgrade an existing Raft directory. Unrecoverable prior data requires manual
inspection; there is no forced bypass or automatic purge of recovery material.

App API-token hashes remain outside backup contents and all current tokens are
revoked on restore and rollback. Browser sessions and backend identities are refreshed;
automatic cleanup stays off. Native OpenBao authorization follows snapshot semantics
and is documented separately. Secret input uses hidden controlling-terminal prompts;
raw child diagnostics and secret values never become status/history output.

The tradeoff is additional disk space, local Docker/tmpfs requirements and manual
service resumption. These are preferable to untested in-place overwrite, automatic
cutover, hidden path rebinding or restoring revoked app authorization. Acceptance
uses synthetic local containers only; it does not establish production-backup health.
