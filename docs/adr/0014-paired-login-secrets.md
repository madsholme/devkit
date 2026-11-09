# ADR 0014: Single values and paired login entries

Status: accepted by the user, 2026-11-09. Implementation plan: [OpenBao Lite v8](../plans/2026-11-09-openbao-v8.md).

Devkit supports both ordinary single-value secrets and named login entries. A login is one OpenBao KV v2 entry/version containing username and password, named for its purpose rather than for the mutable username. Separate username/password keys remain valid single values; no automatic merging or migration occurs.

The alternative of grouping two unrelated keys only in the UI would leave partial rotation, inheritance and deletion behavior. One typed entry makes those operations atomic while retaining the existing project/branch/worktree precedence and cleanup contract. Username and password are protected values; metadata contains only the type and optional environment-variable bindings. Native OpenBao displays the two fields under the same key.

CLI and UI share typed operations. Explicit field reveal/copy returns only that field. Login rotation changes only the stored password, preserving username/bindings; it does not update a remote account. Optional, explicit bindings map the two fields to distinct environment variables. Unmapped logins are skipped during injection. Reserved or colliding environment destinations fail before child execution; mappings are never guessed from a label or username.

Inheritance selects a whole entry, not independent fields. The effective entry's type cannot be changed by an override or update. Existing single-value payloads remain readable. Registry schema 2 is selected on the first login write; old binaries reject it rather than treating a login as a scalar. Recovery and write reconciliation retain type/bindings with matching KV versions and operation IDs. Binary rollback after typed data exists requires a compatible version or explicit matching vault/registry restoration; never overwrite current storage as a deployment side effect.

The accepted trusted single-user model, authenticated metadata boundary, bind storage, manual unseal, configurable cleanup (including off/never), Git ownership, and T3/Codex/Claude workflows remain unchanged.
