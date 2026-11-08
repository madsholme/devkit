# Managed deployment, resources and network

OpenBao is a persistent, production-mode service managed through Devkit's installer, upgrades, health checks and backup tooling. **Never use OpenBao dev mode** for stored credentials. Pin and test a stable image version. Prefer one node using integrated Raft, subject to validation against current OpenBao operational guidance. No PostgreSQL or Redis is necessary with this backend.

## Initial sizing (planning estimates, not guaranteed minima)

| Resource | Start with | Action |
|---|---|---|
| RAM | **512 MiB limit** | Monitor working set/OOM; allow increase to 1 GiB or more. |
| CPU | **0.5 vCPU budget** | Favor soft reservation/share over strict throttling. |
| Persistent disk | **1–2 GiB** | Track Raft data, snapshots, log growth and backups. |
| Instances | 1 OpenBao service | No HA; restart and recovery must be tested. |

Persist data outside disposable containers, use SSD-backed storage, avoid unencrypted swap for the OpenBao process, and verify snapshots with a real restore test. Protect root/unseal/recovery material outside Devkit runtime. Health states: not installed, uninitialized, sealed, ready, unavailable, degraded. Data must survive Devkit upgrade and reinstall without reinitialization.

## Traefik, LAN and Tailscale

Native OpenBao Web UI: `https://vault.devkit.<configured-domain>/ui/`.

- Discover **existing Traefik container**, its Docker labels/file provider, networks, TLS resolver, reverse-proxy conventions and DNS handling. Integrate with it; don't launch another proxy.
- Route `Host(vault.devkit.<configured-domain>)` to OpenBao's internal listener. Preserve `/ui/`, redirect and API routes, static assets and proper forwarded HTTPS headers.
- Make hostname resolution work on **internal LAN and remote Tailscale clients** via the existing DNS/split-horizon arrangement. Do not assume MagicDNS or Cloudflare automatically supplies this.
- Require valid trusted TLS for both paths. Traefik routing is not login: OpenBao's native UI requires its own authentication.
- Do not directly expose OpenBao's port or admin API to the public internet. Keep UI available to intended internal/Tailscale networks with appropriate network ACLs.
- Test from separate LAN and actual remote Tailscale clients: DNS, HTTPS, certificate, authentication, `/ui/` assets, and API health. Mark external tests pending if unavailable.

## Operations

Devkit CLI and UI expose status, setup, seal/unseal guidance, backup create/list/verify/restore, upgrade check/apply, native UI link, connectivity diagnostics, health, logs without secrets and rollback. Backup restore/upgrade need previews and explicit human approval; no destructive reinitialization.
