# Devkit OpenBao implementation handoff

Start here. This package specifies an optional-but-default Devkit-managed OpenBao service for development secrets, with full CLI, Web UI, Codex and Claude Code support.

**For the implementing Codex agent:** first inspect the actual Devkit fork. Do not assume framework, data model, CLI conventions or Traefik configuration. Start with Phase 0 in [implementation-plan.md](docs/openbao/implementation-plan.md), produce a repository-specific implementation plan, and obtain approval before implementation.

## Documentation map

| Document | Responsibility |
|---|---|
| [Architecture](docs/openbao/architecture.md) | Scope, boundaries, decisions, data model |
| [Deployment](docs/openbao/deployment.md) | Service management, sizing, backup, Traefik/Tailscale |
| [First run](docs/openbao/first-run-setup.md) | Vault-first installation and credentials migration |
| [Secrets management](docs/openbao/secrets-management.md) | Ownership, inheritance, conflict resolution |
| [CLI/API](docs/openbao/cli-api.md) | All Devkit CLI commands and backend requirements |
| [Web UI](docs/openbao/web-ui.md) | User-facing settings and contextual secret views |
| [Security](docs/openbao/security.md) | Threat model, identities, bootstrap and auditing |
| [Cleanup](docs/openbao/lifecycle-cleanup.md) | 7/7/30 day retention, recovery, permanent destruction |
| [Agent integration](docs/openbao/agent-integration.md) | Codex/Claude skills, managed instructions, checks |
| [Implementation plan](docs/openbao/implementation-plan.md) | Phases, tests, acceptance criteria |

## Templates

- `templates/devkit-secrets/SKILL.md`: canonical skill to be adapted and installed for **both Codex and Claude Code**.
- `templates/AGENTS.md.fragment`: managed Codex project-instruction block.
- `templates/CLAUDE.md.fragment`: managed Claude Code project-instruction block.

These templates are source materials for implementation, **not** already installed or functional integrations. Devkit should own their versioning and managed updates.

## Decisions

- Managed OpenBao enabled by default in first-run setup; explicit local-provider opt-out.
- System/integration credentials (including Cloudflare API token) stored in OpenBao as soon as the managed vault is ready; only minimal bootstrap material remains outside.
- Native UI: `https://vault.devkit.<configured-domain>/ui/` through existing **Traefik**, accessible on LAN and remote Tailscale.
- Initial limits: **512 MiB RAM**, **0.5 vCPU starting budget**, **1–2 GiB starting persistent disk**; measure and adjust.
- Every Web UI action has a corresponding documented, scriptable Devkit CLI operation.
- Project/branch/worktree views show owned and inherited secrets, with scope and effective-value provenance.
- Retention: **branch 7 days**, **worktree 7 days**, **project 30 days**.
- Agents may retrieve authorized plaintext secrets, with exposure risks acknowledged and runtime injection preferred.

## Kickoff prompt

> Read `README.md` and every document in `docs/openbao/`. Inspect the Devkit repository. Perform Phase 0 only: map deployment, settings, backend, CLI, Web UI, first-run setup, Traefik, DNS/Tailscale, project/branch/worktree lifecycles, and Codex/Claude integrations. Produce a phased repository-specific plan with filenames, migrations, tests, security boundaries, rollback and open decisions. Do not deploy or modify live credentials before approval.
