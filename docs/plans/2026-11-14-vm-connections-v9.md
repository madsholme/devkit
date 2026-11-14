# VM connection defaults and direct access — v9

Extends completed [v8](2026-11-04-resource-shutdown-v8.md) and ADRs 1/3.
Single-agent task in `codex/vm-connections`. User confirmed: update devkit and the future-launcher rule; leave running VMs and existing launchers unchanged.

## Design

Prefer discovered SPICE ahead of VNC and guest SSH. Preserve protocol fallback and
read-only discovery; never invent a SPICE endpoint or restart a guest to create one.
Establish a durable launcher rule: graphical QEMU defaults to SPICE; VMs must support
SSH tunnels and direct private-network connections. Bind explicitly to a reachable
private LAN/Tailscale address and retain authentication. Headless guests use SSH.

Preserve actual listener addresses in sanitized metadata. Accept loopback, wildcard,
private LAN IPv4/IPv6 and Tailscale CGNAT listeners. Generate tunnels to the actual
listener (loopback for wildcard). Direct commands use explicit private listeners or
matching-family private addresses discovered on local interfaces for wildcard binds.
Do not advertise loopback, wildcard, public or link-local addresses as direct targets.
Show host, port, URI/command and availability explanation. No reachability guarantee:
client routing/firewall and existing authentication still apply. Unsupported TLS/socket
configurations retain manual guidance; do not bypass TLS or expose credentials.

UI: Connections disclosure names the VM; protocol selector with SPICE selected initially, preferred badge,
separate SSH tunnel and Direct sections, numbered tunnel steps, clear endpoint details.
Use the shared component in global/project views. Stack at narrow widths and wrap
commands. Keep existing theme and copy controls.

Approach: extend existing discovery/command model and shared Vue component. Avoid a
new VM provisioner or network proxy; those would duplicate launcher ownership and
change running workloads. Direct availability remains honest for old loopback guests.

## Tasks / acceptance

1. Backend: tests for explicit private, wildcard, loopback, IPv6, invalid/public,
   unavailable SSH metadata, stable SPICE priority and libvirt/SSH parity. Extend
   vmConnection additively with listener/direct endpoint metadata; preserve CLI commands.
2. UI: shared accessible grouping; test SPICE ordering, both methods, copying,
   unavailable direct, serial console, global/project pages and mobile overflow.
3. Docs: dashboard/project guidance, README and template/root agent rule. Record the
   boundary change in an ADR. Build committed production assets.
4. Verify Go suite/vet, frontend contracts/build, targeted Playwright desktop/mobile,
   inspect screenshots and diff. Integrate to main and push the owned fork per project
   convention; preserve unrelated commits/worktrees. Rebuild/restart dashboard only
   after checks, with no VM changes.

## Checkpoint

Implementation complete. Go suite and vet pass; all 33 frontend contracts pass;
production assets rebuilt. Browser verification passes global/project connection flows,
SPICE default and VNC switching, clipboard contents, 1440px/390px layout and overflow.
Existing dashboard (7) and project (4) browser regressions pass. Inspected synthetic
desktop/mobile screenshots. Mechanical design scan has no findings. Disposable paused
QEMU acceptance passes; existing guests and launchers were not changed.

Review corrections: unknown libvirt bind addresses never advertise direct endpoints;
matching-family wildcard results exclude public/link-local/loopback and deduplicate.
The mobile panel uses the inventory container width and overrides inherited table
nowrap, while the resource table retains its existing horizontal scroll behavior.

No actual remote-client direct SPICE/SSH session was attempted. Existing Vite large-chunk
warning remains. Integration targets main and fork/main. Live deployment is pending: this workstation has no
configured devkit-web unit, and SSH to the screenshot host 100.126.112.0 returns
Network is unreachable. A reachable host address is required; no VM was changed.
