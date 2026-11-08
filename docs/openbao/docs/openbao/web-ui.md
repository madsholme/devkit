# Devkit Web UI requirements

Use Devkit's existing UI framework, design system and access patterns. Never embed OpenBao's admin UI as the only project-secret interface; keep a dedicated link to native OpenBao at `https://vault.devkit.<configured-domain>/ui/`.

## Surfaces

1. **First-run wizard:** Managed OpenBao selected by default; opt-out local provider, bootstrap/initialization and secure recovery ceremony, readiness, Cloudflare and other credentials stored directly in vault, LAN/Tailscale connection tests.
2. **Settings → Secrets Provider:** status (sealed/unsealed/etc.), configured domain, Traefik/Tailscale health, native UI link, backup/restore, upgrades, diagnostics and provider selection.
3. **Settings → Devkit Credentials:** masked system/integration entries with source (`OpenBao` or `Local`), creation/rotation, explicit authorized reveal, migration and import verification. Project agents cannot access this page's values.
4. **Project → Secrets:** project-shared CRUD/generate/reveal/rotate/history, effective source, and CLI equivalents.
5. **Branch → Secrets:** branch-owned entries **and** inherited project entries. Distinguish source and ownership; prevent changing parent values unintentionally.
6. **Worktree → Secrets:** worktree-owned entries, associated branch secrets, and project secrets. Mark effective value using precedence `worktree > branch > project`, with clear override actions.
7. **Deleted resources / Secret cleanup:** pending deletion, deadline, recovery, failed/retried jobs and audit history. Deletion confirmation lists affected key names and preserved inherited secrets, never values.
8. **Agent integrations:** installation/update status of canonical `devkit-secrets` skill for both Codex and Claude Code, managed instruction status, doctor/repair actions.

## UX and safety

- Mask values by default. Reveal/copy is explicit and recorded; avoid browser analytics, URL parameters, logs and persistent client caches containing plaintext.
- On deletion, show exact scope, retention (7/7/30 days) and recoverability.
- On vault sealed/unavailable, show actionable diagnostics; do not silently render obsolete local values.
- Show effective scope context and path-independent immutable identity, with friendly display names.
- For **each** button or mutation, document/test the equivalent `devkit` CLI operation.
