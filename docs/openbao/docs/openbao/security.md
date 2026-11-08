# Security, identity and recovery

## Trust boundaries

- Devkit backend is privileged broker. Native OpenBao administration is a distinct human-authenticated surface.
- Codex and Claude may retrieve authorized plaintext project secrets, but not Devkit system/integration paths, other projects, root token, seal or recovery material.
- Project scopes use narrow OpenBao identities/policies. **Logical scope is not hard security** if untrusted agents share one unrestricted Unix account, Docker socket, OpenBao credential store or backend control plane. Inspect actual process isolation; don't claim separation that isn't enforced.
- No root or broad admin token in `AGENTS.md`, `CLAUDE.md`, skills, project directories, env files or Devkit settings.

## Bootstrap

OpenBao must be initialized and unsealed before it serves credentials. Its own authentication needs protected initial material; minimize and isolate this exception (restricted service identity or vetted machine identity). Deliver unseal/recovery material only to the human during first run. Manual-unseal after reboot is acceptable if documented; automatic unseal requires separately secured trust material, not a convenient plaintext key next to the vault.

## Runtime handling

- List without values. Plaintext reveal available only via explicit command/action and authorization; prefer scoped runtime injection.
- Never place secrets into command arguments, URL queries, normal app logs, metrics, diagnostic bundles, generated source code, or agent instructions.
- Understand limits: `--reveal` can enter agent tool transcripts; environment variables can be inspectable by privileged processes/containers. Redaction cannot recover data already emitted.
- Audit actor, timestamp, scope, key name, action and result without values. Verify logs/errors/traces against sentinel credentials.
- Disable or encrypt swap used by OpenBao. Bind internal listener to private network and expose native UI only via authenticated HTTPS through existing Traefik on LAN/Tailscale.

## Recovery

Take encrypted/off-host backups and periodically restore-test them. Separate unseal/recovery material from backed-up encrypted storage. Never reinitialize populated volumes on upgrade/reinstall. Explain that permanent live-vault deletion does not erase historical secrets inside older retained backups; backup retention and access require a separate policy.
