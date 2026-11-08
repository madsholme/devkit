# Phased implementation and acceptance plan

**Do not begin implementation until Phase 0 findings and plan have been reviewed by the user.** This is a design-level plan, not a claim about current repository components.

## Phase 0 — Inspect Devkit

Map actual repo architecture, existing setup, services, persistent data, Traefik routes, internal/Tailscale DNS/TLS, settings storage, CLI patterns, project/branch/worktree IDs and lifecycle, agent launch identity/permissions, CI/tests. Identify conflicts, exact files/migrations, security boundaries and rollback points. Report what cannot be verified without a live server.

## Phase 1 — OpenBao service and first run

Default-on managed provider, persistent non-dev OpenBao, 512 MiB RAM starting limit and 0.5 vCPU budget, secure setup/unseal, restricted identity, KV v2, audit and backup. Native UI accessible through existing Traefik at `vault.devkit.<configured-domain>/ui/` on LAN and remote Tailscale. No reinitialization on restart/reinstall. Test seal/unseal and recoverability.

## Phase 2 — Backend and CLI

Resource registry, KV v2 API adapter, authorized project/branch/worktree scopes, inheritance, typed errors, JSON CLI and Devkit sensitive-settings references. First-run Cloudflare/GitHub/etc. credentials saved straight to vault; migration path for legacy plaintext values. Full UI-parity CLI scope.

## Phase 3 — Web UI

Settings provider and Devkit credentials, project/branch/worktree contextual secret views with provenance and effective precedence; explicit reveal and targeted create/edit/rotate. Native UI link, health and cleanup dashboard. Test UI/CLI parity.

## Phase 4 — Cleanup

Implement 7-day branch, 7-day worktree, 30-day project retention with soft-delete, restore, permanent version+metadata destruction and safe idempotent retry. Test active worktree links, rename/recreate, outages and project cascade using virtual time.

## Phase 5 — Agent integrations and hardening

Install/maintain canonical skill for Codex and Claude, managed AGENTS/CLAUDE blocks, doctor/repair, operator docs, tests for fresh agent sessions. Security review includes actual OS process/credential isolation, plaintext leak scans and backups.

## Definition of done

- Fresh setup defaults to OpenBao while opt-out works; Cloudflare and other integration credentials never persist in plaintext Devkit configuration with OpenBao selected.
- OpenBao native UI works through Traefik on internal and remote Tailscale routes with trusted TLS and authentication.
- Every Devkit UI action has an equivalent documented CLI command.
- Project, branch and worktree UIs show owned/inherited secrets and no accidental parent edits.
- Devkit's system secrets are inaccessible to project agents.
- Codex and Claude skill workflows are installed, updated, discovered and verified.
- Cleanup safely follows 7/7/30 retention and recovery, including permanent version/metadata destruction after expiry.
- Backups, disaster restore, seal/unseal, upgrade and fail-closed behavior are exercised.

## Questions to resolve in the repository

Actual domain and DNS/Tailscale routing; existing Traefik labels/config; identity model and immutable resource IDs; current settings backend; agent OS permissions and Docker socket access; safe location for vault bootstrap material; backup destination and policy. Ask user only where repo/server inspection cannot resolve the answer.
