# Devkit OpenBao Lite — implementation kickoff

**Status:** requirements and implementation handoff, not deployed code. **Target:** the user's Devkit fork on one development server. **Audience:** Codex implementing the feature. **Version:** 1.0 — 2026-11-08.

## 1. Mission

Add a small, reliable secrets manager to Devkit without building another full vault product. Devkit manages one self-hosted OpenBao instance and offers a simple, consistent **CLI and Web UI** for Devkit settings and development project secrets. Codex and Claude Code automatically learn to use it through a shared skill and managed project instructions.

**First action for Codex:** Inspect the repository's actual backend, CLI, Web UI, first-run setup, Docker/Traefik deployment, settings model, project/branch/worktree lifecycle, and agent configuration. Produce a short, repository-specific plan with exact files and tests. Ask for review before changing code. Do not invent stack details.

## 2. Scope: keep / defer

**Build now**

- Devkit-managed OpenBao, **enabled and recommended by default** at first run, with a clear opt-out to existing local configuration.
- OpenBao native Web UI at `https://vault.devkit.<configured-domain>/ui/` via the **existing Traefik container**, accessible on the internal network and from remote Tailscale clients. Use existing DNS/TLS patterns; don't expose a raw OpenBao port publicly.
- One restricted Devkit backend identity; KV v2 storage paths for Devkit system settings and project/branch/worktree scopes. Keep human OpenBao administration separate.
- Cloudflare API token and other Devkit integration passwords/tokens saved **directly to OpenBao during setup** if selected; avoid plaintext `.env` or settings files. Non-sensitive settings remain in ordinary config.
- Backend API used by **both** native Web UI and `devkit` CLI: generate, set, list, get/reveal, update, rotate, and inject secrets.
- Context-aware secret views on project, branch and worktree pages, including inherited records and source labels.
- Automatic cleanup: branch **7 days**, worktree **7 days**, project **30 days** recoverable retention before permanent deletion.
- Automatically installed/maintained **Codex and Claude Code skills**, with managed `AGENTS.md` / `CLAUDE.md` instructions and doctor command.
- Simple backup procedure (scheduled backup, protected recovery material and documented restore), health indicator and minimal audit events.

**Defer until needed**

- Complex multi-user authorization/admin UI, per-worktree OpenBao AppRoles, HA clustering, dynamic database credentials, MCP server, multiple vault providers beyond existing local opt-out, elaborate backup/upgrade dashboards, custom audit analytics.

## 3. Deployment and first run

Start with a single OpenBao service using durable storage, **not OpenBao dev mode**. Prefer integrated Raft for a one-node setup unless repository/operational inspection favors a supported alternative. Pin a tested version.

| Resource | Initial budget | Notes |
|---|---|---|
| RAM | **512 MiB limit** | Starting estimate, not guaranteed; monitor and raise if needed. |
| CPU | **0.5 vCPU budget** | Prefer soft sharing/reservation over aggressive throttling. |
| Disk | **1–2 GiB persistent** | Monitor data, snapshots and logs; backup separately. |
| Services | One OpenBao container | Integrated Raft needs no Postgres/Redis. |

### Install sequence

1. First-run wizard selects **Managed OpenBao (recommended)** by default, with opt-out.
2. Detect existing instance/volume; **never reinitialize persisted data**.
3. Start OpenBao with persistent storage; guide the human through initialization and unsealing. Give recovery/unseal material to human only, outside agent logs and Devkit's own vault-accessible settings. Never persist a root token in Devkit configuration.
4. Establish minimal protected host-side authentication for Devkit backend; assign narrow access to its own system/integrations path and the authorized project paths. Agents get no direct OpenBao root or backend credentials.
5. **Before collecting Cloudflare/GitHub/GitLab or other lasting credentials**, ensure OpenBao is ready. Submit tokens through secure setup inputs and save directly into KV v2; store only `openbao://...` references in ordinary Devkit settings.
6. If Cloudflare DNS/TLS setup currently precedes OpenBao, reorder the wizard so local vault bootstrap comes first. If temporarily needed, handle token in memory only and never persist in `.env`, Compose `environment`, logs or CLI argv. Pause setup if vault isn't ready.
7. Register `vault.devkit.<configured-domain>` in **existing Traefik**. Confirm DNS/TLS/login from LAN and, where test clients are available, remote Tailscale. Expose a link in Devkit Settings.
8. Migrate old plaintext settings through an optional verified import. Only remove originals after successful readback and user confirmation; account for old backups/overrides.

OpenBao sealed/unavailable: report status and fail closed for vault-backed secrets. Never silently return stale local values. Simple scheduled backups must be restore-tested and preserved independently of Devkit upgrades. Protect unseal/recovery material separately. A single node has no HA.

## 4. Minimal secrets model

Use immutable resource IDs from Devkit; names/paths are display data, not security identifiers.

```text
kv/devkit/system/<key>
kv/devkit/integrations/<service>/<key>
kv/projects/<project-id>/shared/<key>
kv/projects/<project-id>/branches/<branch-id>/<key>
kv/projects/<project-id>/worktrees/<worktree-id>/<key>
```

| Current UI context | Show | Effective precedence |
|---|---|---|
| Project | Project-shared secrets | Project |
| Branch | Branch-owned + project-shared | Branch > project |
| Worktree | Worktree-owned + linked branch + project | Worktree > branch > project |

Show `name`, `source scope`, `owned/inherited`, `effective value source`, `last updated`. Values are masked and **not returned on list APIs**. Reveal requires an explicit action. Creating an override must not accidentally update an inherited parent secret. A detached worktree only inherits branch secrets if explicitly linked in Devkit. A project/branch/worktree deletion only affects keys it owns; project deletion cascades to all its descendant scopes.

Keep a lightweight registry mapping immutable resource ID → secret key paths, deletion state and expiry. Prefer existing Devkit database/model rather than introducing another database. Project system secrets are inaccessible to agents through normal Devkit interfaces.

## 5. CLI — required in v1

**Every operation available in Devkit's secrets UI must have a scriptable CLI equivalent.** Adapt commands to existing CLI naming conventions but preserve behavior. Provide `--json`, clear exit codes and `--project`, `--branch` and `--worktree` selectors where relevant. The backend verifies context instead of trusting directory names.

```bash
# Vault operator basics
devkit vault status --json
devkit vault setup
devkit vault unseal                 # secure prompt, no key in argv
devkit vault ui                     # print/open native UI URL
devkit vault backup
devkit vault health

# Contextual secrets
devkit secrets context --json
devkit secrets list --json
devkit secrets list --worktree <id> --json
devkit secrets generate DATABASE_PASSWORD --length 32 --scope worktree
devkit secrets set API_KEY --scope project     # secure stdin/prompt, not argv
devkit secrets get DATABASE_PASSWORD          # masked/metadata by default
devkit secrets get DATABASE_PASSWORD --reveal # explicit plaintext
devkit secrets rotate DATABASE_PASSWORD
devkit secrets run -- npm run dev              # runtime injection
devkit secrets history DATABASE_PASSWORD
devkit secrets restore DATABASE_PASSWORD

# Lifecycle
devkit secrets cleanup preview <resource-id> --json
devkit secrets cleanup status --json
devkit secrets cleanup restore <resource-id>

# Devkit's own integration tokens
devkit integrations cloudflare configure --token-stdin
devkit integrations cloudflare status --json
devkit integrations cloudflare rotate-token --token-stdin

# Agent setup/repair
devkit doctor --agents
devkit agents skills install
devkit agents skills status --json
```

Do **not** print generated credentials by default. Explicit reveal is supported because agents sometimes need plaintext, but remind implementers that tool output/agent transcripts may retain secrets; runtime injection is preferred. Never use root OpenBao credentials for routine CLI operations. Avoid passing secrets via command arguments, URL queries or log messages.

## 6. Web UI — minimum pages

- **Settings → Secrets**: OpenBao status, enable/disable choice at setup, native UI link, and Devkit integration credentials shown masked with source (`OpenBao`/`Local`). Secure create/rotate/reveal controls.
- **Project → Secrets**: list project secrets, generate/set/reveal/rotate, clear provenance.
- **Branch → Secrets**: branch-owned plus inherited project secrets. Source labels and explicit override scope.
- **Worktree → Secrets**: worktree-owned plus linked branch and project secrets. Source/effective labels and explicit override.
- **Resource deletion dialog**: number/names of owned secrets affected, retention deadline and restoration option; never show secret values.
- **Agent setup status**: Codex and Claude skill installed/version plus repair action.

All UI actions reuse Devkit backend API and have CLI parity. Do not reproduce OpenBao's full administrative UI; link to its native UI for advanced operations.

## 7. Cleanup lifecycle — simple but safe

| Removed resource | Soft-delete / recovery window | Then |
|---|---|---|
| Branch | **7 days** | Permanently destroy all owned secret versions and metadata. |
| Worktree | **7 days** | Permanently destroy all owned secret versions and metadata. |
| Project | **30 days** | Destroy all project/branch/worktree-owned secret versions and metadata. |

When deletion is confirmed in Devkit: resolve exact owned keys, check active worktree references, mark pending, restrict reads, apply KV v2 soft delete, enqueue expiry. Allow restoration during retention, and run idempotent destruction/retry afterward. Do not erase shared/inherited secrets when deleting a child scope. A project deletion uses 30 days for the whole project tree. External Git deletion should trigger careful reconciliation, not immediate destructive inference. Older backups may still contain deleted secrets until backup expiration.

## 8. Codex and Claude Code — automatically discoverable skill

Ship **one canonical `devkit-secrets/SKILL.md`** in the Devkit source tree, installed/updated into both agents' supported skill directories by Devkit setup/upgrade. The skill should have strong triggering metadata:

```yaml
---
name: devkit-secrets
description: Use when creating, retrieving, storing, configuring, injecting, rotating or troubleshooting development passwords, tokens, API keys and credentials in a Devkit project, branch or worktree.
---
```

Core skill workflow:

1. Run `devkit secrets context --json`, confirm correct active scope.
2. Run `devkit secrets list --json` to find an existing key before generating another.
3. Pick project/branch/worktree ownership explicitly; understand inheritance and overrides.
4. Use `devkit secrets generate/set/get/rotate/run`, not direct OpenBao admin calls or Git-tracked plaintext files.
5. Use explicit `--reveal` only if the task requires plaintext; never echo values in final messages or logs.
6. Let Devkit handle 7/7/30-day cleanup; never purge raw vault paths.
7. On sealed/unavailable state, use `devkit vault status`; do not fall back to stale plaintext.

Devkit maintains bounded instruction blocks in project files **without replacing user content**:

**`AGENTS.md` for Codex, `CLAUDE.md` for Claude Code:**

```markdown
<!-- devkit-managed:secrets:start -->
This project uses Devkit-managed secrets. For credentials, consult the
installed `devkit-secrets` skill and use `devkit secrets` commands.
Run `devkit secrets context` and `devkit secrets list` first; never
commit plaintext credentials. Project/branch/worktree secret inheritance
is explicit. Use `--reveal` only when needed and never print values in
messages or logs. Devkit owns secret cleanup; do not manually purge vault
paths. Check `devkit secrets --help` when uncertain.
<!-- devkit-managed:secrets:end -->
```

`devkit doctor --agents` checks both agents' skill installation/version, managed instruction blocks, CLI path and effective project context. On new projects and worktrees, instructions must remain discoverable. Test fresh Codex and Claude sessions without reminding them about Devkit secrets.

## 9. Lightweight delivery plan — three milestones

**Before implementation: repository review (planning, not a full phase).** Map Devkit's actual code, Traefik/Tailscale routing, first-run order, settings, IDs and agent OS privileges. Produce exact file list and risks; ask user to approve.

1. **Foundation:** managed OpenBao defaults, persistent storage and recovery, first-run credentials (Cloudflare first), Traefik/Tailscale native URL, simple backups, Devkit system secret references. Verify install/restart/retrieve and absence of plaintext credentials.
2. **Secrets in Devkit:** common backend, complete minimal CLI, project/branch/worktree UI and inheritance, 7/7/30 cleanup/recovery. Verify effective precedence, parent protection, and CLI/UI parity.
3. **Agents and hardening:** shared Codex/Claude skill, idempotent AGENTS/CLAUDE fragments, doctor/repair, redaction and integration tests. Verify real OS permissions: a shared unrestricted process identity may bypass logical isolation even if UI/API policies work.

### Acceptance tests / definition of done

- A fresh install selects OpenBao by default but local opt-out works.
- Cloudflare and other Devkit integration credentials are stored directly in OpenBao and work after restart. A scan of generated config/env/Compose/database/logs for controlled test values finds no plaintext copies.
- `vault.devkit.<configured-domain>/ui/` works over HTTPS from LAN and remote Tailscale with native authentication (record remote test as pending if not available).
- CLI can create, retrieve plaintext on explicit request, inject and rotate secrets; every UI operation has CLI coverage.
- Project, branch and worktree pages show own and inherited secrets correctly, with safe writes and overrides.
- Deleting resources invokes 7/7/30 recovery windows then permanent KV v2 version+metadata destruction; unrelated secrets are unaffected.
- Codex and Claude Code automatically discover their skills/instructions in a fresh session.
- No root credentials in agent scope, no accidental plaintext logs; sealed vault fails closed.

## 10. Prompt to paste into Codex

> Read `Devkit_OpenBao_Lite_Implementation_Guide.md`. This replaces the older elaborate OpenBao plan for the initial build. Inspect the repository and produce a short, file-specific implementation plan for the three milestones. Preserve all functional requirements: vault-first Cloudflare/Devkit secrets, CLI/UI parity, scoped project/branch/worktree views, 7/7/30 cleanup, native OpenBao UI via existing Traefik on LAN/Tailscale, and automatically installed Codex/Claude skills. Defer advanced admin/HA/multi-user features. Do not change code or live secrets until I approve the plan.
