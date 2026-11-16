# Vault backup restore — v1 design

Status: same-host scope approved on 2026-11-16. Implementation is
authorized, but the running Devkit server must not be changed. Development and
acceptance use isolated local fixtures, synthetic credentials and separate Docker
identities. No deployment, SSH to Devbox, production backup access or live recovery
is part of this task.

## Goal and existing contracts

Provide a CLI restore for Devkit's encrypted vault backups, with a real isolated
restore test before any replacement. Reuse the existing backup manifest, private
file helpers, OpenBao client, hidden terminal prompts, secret-state lock and Docker
acceptance runner. Keep Go standard-library-only and retain the existing `age`
dependency. Existing `backup verify` remains a checksum check.

This extends OpenBao Lite v10 and ADRs 0013/0016. Git checkouts, project data,
external application accounts and VM state remain outside vault recovery.
Decryption identities and original OpenBao unseal shares remain human-held.
Devkit API-token revocations must never be resurrected from a snapshot.

## Approved first-version scope

The user selected same-host recovery: restore the configured managed vault while
preserving host configuration and registered checkout identities. Replacement-host
recovery would additionally need explicit checkout rebinding, host-address/domain
mapping and new authorization state; that is outside this version.

Do not invent substitute keys or assume the original vault is healthy. A stopped,
sealed or broken source must remain recoverable when its backup and required human
recovery credentials are available. Missing/corrupt local identity metadata requires
an explicit operator-selected installation identity, never a guessed match.

## CLI behavior

Implemented command surface:

```sh
devkit vault backup restore BACKUP_ID
devkit vault backup restore BACKUP_ID --test --identity /private/recovery.agekey
devkit vault backup restore BACKUP_ID --yes --identity /private/recovery.agekey
```

The default inspects the backup and reports the exact target and requirements;
it does not decrypt, create containers, stop services or modify vault state.
`--test` decrypts and restores to disposable, uniquely named storage/containers;
it never changes the configured vault, proxy, web service or secret registry.
`--yes` repeats the isolated test against the exact selected backup before entering
the replacement phase. An old success receipt is not authority to bypass testing.
No web action or external-secret API operation is added.

Original unseal shares and native administrator credentials use the controlling
terminal with echo disabled, never flags, environment, job output or history.
The identity argument is a private filename, not key material. Output contains
bounded status/identity information and no secret values or raw tool diagnostics.

## Safe test

1. Validate bounded manifest/component sets, regular private files and ownership,
   no-follow paths, sizes, checksums, installation identity and exact official image.
   Reject unsupported schemas, missing components, symlinks, unknown files, malformed
   metadata and unresolved operations. Pin the inspected input bytes for the attempt.
2. Decrypt to private memory-backed temporary storage; fail closed when that storage
   is unavailable. Authenticate complete ciphertext before accepting a component.
3. Start an isolated instance using the backup's exact image and empty encrypted
   Raft storage. Use a unique internal Docker bridge with no published ports; access its exact
   inspected private endpoint from the host. No proxy labels, host network,
   source-vault mounts or production Compose project. Docker internal networks do
   not publish loopback ports, so this is the verified networking implementation.
4. Initialize with ephemeral test credentials, restore the snapshot, then unseal
   with the original shares. Verify native administrator authentication and create
   a fresh restricted backend identity. Do not reset the administrator password.
5. Validate matching registry and cleanup metadata, managed key/version/history
   consistency and representative inheritance/deleted-key recovery in fixtures.
   Validate certificate archives without following links or extracting arbitrary
   paths; test certificates with isolated listeners and fixture trust only.
6. Remove only attempt-owned containers and temporary plaintext on success/failure.
   Cleanup failures are explicit failures, with safe identifiers for follow-up.
   Assert source container start identities and source files remain unchanged.

## Confirmed replacement and failure handling

After testing succeeds, acquire/revalidate host ownership and the secret-state lock.
Keep a durable private recovery journal before the first mutation. Quiesce relevant
Devkit services and verify the exact managed container/storage identities; unrelated
containers and processes must remain untouched. Preserve old encrypted data,
metadata, backend identity and certificate state as a coherent rollback set.

Install the tested replacement with its matching registry and cleanup configuration.
Disable automatic cleanup until the operator explicitly resumes it. Preserve host
configuration except fields necessarily selected by the verified recovery plan.
Never overwrite checkout files, rebind resources by name or import API token hashes
from backups. Invalidate browser sessions with a fresh signing key. Native OpenBao
identities follow snapshot semantics; document this separately from Devkit tokens.

Verify the replacement and required service health before reporting success. An
interrupted or partially applied operation blocks normal secret access, maintenance,
setup/update and competing restore operations. Provide explicit journal-based
recovery/rollback; do not silently clear a journal, retry an ambiguous snapshot
mutation, mix old/new metadata or delete either data set. Rollback must also validate
ownership and restore the coherent prior state. Prior data is never automatically
purged by restore or backup retention.

## Acceptance and implementation boundaries

- Unit tests: CLI flag conflicts/default no-op; malformed/tampered/changed backup;
  wrong identity and original shares; unsupported image/schema; path traversal,
  archive symlinks and size limits; lock contention; failed cleanup; interruption
  at each replacement boundary; stale journal/identity; no token/session revival.
- Real local Docker acceptance: synthetic encrypted backup, isolated restore test,
  failed test leaves source untouched, confirmed replacement on disposable source,
  recovered values/history/inheritance and fresh backend identity, explicit rollback,
  source preservation and secret-free output. Reuse the existing OpenBao fixtures.
- Terminal acceptance: hidden multi-share/admin input, cancellation and lack of
  controlling terminal; no credentials in stdout/stderr/history or process arguments.
- Documentation: CLI help/README, secrets recovery guide, testing instructions and
  an ADR for tested replacement and recovery-journal ownership. Preserve the existing
  manual recovery procedure as the advanced fallback.
- No real server access or deployment. Real-backup validation requires a later
  operator session with original unseal material; synthetic acceptance must not be
  described as validation of the user's production backup.

## Implementation plan

1. `vault_restore_backup.go` and tests: strict pinned backup loading/decryption,
   bounded metadata/archive validation, same-host installation selection and read-only
   plan. Observe failing malformed-input/path tests before implementing.
2. `vault_restore_test.go` / `vault_restore_runtime.go`: isolated Docker rehearsal,
   hidden recovery-input adapter, exact-image restore/unseal/login and managed-version
   verification. Reuse the encrypted acceptance fixture and its synthetic credentials.
3. `vault_restore.go` / `vault_restore_apply.go`: CLI modes, journaled cutover and
   explicit rollback, shared operation guard and source ownership checks. Require the
   web service stopped before cutover/rollback; never stop/start a user web service
   implicitly. Preserve currently installed TLS certificates on same-host recovery;
   validate backed-up certificate contents in rehearsal and retain manual certificate
   recovery instructions. This avoids replacing newer, working certificates.
4. Tests/docs/ADR: failure-injection boundaries, CLI/terminal refusal and output
   secrecy, real disposable apply/rollback, full Go/vet checks, README and secrets
   guide. Update this authoritative plan with exact evidence and remaining limits.

No subagents under the user's single-agent instruction. Reuse the existing isolated
chat worktree, which now holds this task’s implementation and verification changes. Existing validated code is not
retested without a changed boundary; run the full suite at completion.

## Delivery and verification

Completed: strict backup inspection/decryption, isolated production-code rehearsal,
confirmed same-host replacement, guarded interruption rollback, hidden terminal input,
recovery documentation and ADR 0019. Release CI enables the restore profile alongside
its existing encrypted backup fixture. No deployed server changes were made.

The internal-bridge connection is an implementation correction: Docker internal
networks do not publish ports. The rehearsal uses its exact inspected private endpoint
without weakening managed OpenBao's loopback-address validation. The tested candidate
is stopped gracefully before its encrypted data is installed. These refinements
preserve the approved isolation and same-host scope.

Verification on 2026-11-16:

- `go test ./...` and `go vet ./...`: passed; formatting and diff checks clean.
- `go test -race ./cmd/devkit -run TestVaultRestore -count=1`: passed.
- Production encrypted restore profile: passed in 94.2 seconds with synthetic local
  OpenBao/Traefik fixtures. Covered wrong decryption identity, original share and
  native login; pre-existing container preservation; real snapshot/version recovery;
  stopped-source replacement; retained old storage; fresh backend/session identities;
  app-token revocation; explicit rollback and preservation of the stopped state.
  The preceding running-source replacement/rollback profile also passed.
- Five injected replacement-boundary failures: each blocked normal secret access and
  recovered the old data through explicit rollback; candidate/replacement data retained.
  Stale operation IDs, lock contention, changed backup, foreign cleanup identity,
  unsupported metadata, unsafe paths/files and remote Docker endpoints are rejected.
- Extended existing PTY runner: passed delayed unseal, two hidden restore shares,
  hidden administrator input, SIGTERM cancellation with echo restoration, and
  refusal without a controlling terminal. No recovery credentials in output/history.

Single-agent review followed the user's instruction; no reviewer subagent was spawned.
The tested boundaries are synthetic. Production backup validity, actual server recovery,
external integration credentials and real DNS/TLS remain operator checks. Current
certificates are deliberately preserved; replacement-host mapping and automatic purging
of retained recovery material are outside this version. No server install, restart,
SSH access, production snapshot or credential retrieval occurred.

Compatibility correction from real-backup rehearsal: the certificate volume may
contain both `acme.json` and `acme-staging.json`, including an empty initialized
store. The original single-file validator incorrectly refused this supported proxy
layout. Restore now accepts only those two exact filenames, with duplicate, type,
permission, size and populated certificate/key validation preserved. Synthetic
regressions and the encrypted Docker recovery profile pass with this layout. The
real local copy reaches the original-unseal prompt; full real-backup verification
still requires the operator's private shares and native login. The running server's
web PID/start identity and vault container/start identity remained unchanged.
