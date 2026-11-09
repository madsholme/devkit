# ADR 0016: Scoped Devkit tokens for external secrets clients

Accepted by user request, 2026-11-09; [plan v10](../plans/2026-11-09-openbao-v10.md).

External applications authenticate to Devkit using independently revocable hashed bearer tokens, each bound to one registered secret context with read/write/cleanup permissions and expiry. Existing typed secret operations remain the only mutation path, preserving inheritance, CAS and cleanup. Authorize within the state lock, revalidate live ownership and refuse host-wide administration or client context changes. The trusted host owner issues tokens locally; applications cannot issue tokens or execute commands.

Use a distinct versioned API endpoint; preserve dashboard cookie and same-origin checks. Require HTTPS except direct loopback. Do not enable cross-origin browser access. Return only authorized values/metadata and audit token IDs, never credentials.

Native OpenBao API access remains available privately with its own authentication. App-owned native paths have native KV semantics; Devkit's managed paths must use the Devkit API. External OpenBao server configuration is not part of this request. Token hashes are host authorization state, excluded from vault backup/restore to avoid resurrecting revoked tokens; issue new tokens on replacement hosts.
