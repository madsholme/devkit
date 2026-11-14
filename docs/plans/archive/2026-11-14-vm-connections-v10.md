# VM home and away connection instructions — v10

Supersedes [v9](archive/2026-11-14-vm-connections-v9.md). V9 implementation is committed
as f5a20c5 on main/fork; not deployed. Retain all v9 requirements and passing evidence.
The user clarified that instructions must explicitly show inside-home-network and
external variants, and selected **Tailscale/VPN** for external access. The reachable
LAN deployment address is 192.168.0.198; verified running version is 9252aa1.

## Design / changes from v9

Add labeled At home (LAN) and Away (Tailscale) SSH destinations, preserving the exact
VM listener as the forwarding target and the existing single-tunnel API field.
Show both variants side by side in meaning, within the tunnel section, and tell the
user to choose one tunnel before the shared viewer/guest SSH step. Serial-console
commands receive the same network labels. Direct endpoint metadata carries its
network label; LAN-only displays explain using the Tailscale tunnel while away.
Never advertise direct access on an address the VM does not listen on. Loopback-only
VMs still work through either SSH destination; existing guests remain unchanged.

Discover active private host interface addresses, excluding conventional Docker,
libvirt guest-bridge and veth interfaces to avoid advertising internal guest networks.
Use the configured/detected Tailscale host as the existing fallback. Do not open
public ports or add a public connection/proxy configuration. The UI continues to
prefer SPICE, retain VNC/SSH, show exact details and wrap/stack on phones.

## Tasks

1. Regression-test both home/away SSH commands, classification, serial-console
   parity, and preservation of listener target; implement additive metadata.
2. Show labeled variants in shared UI, test commands/copying and desktop/mobile
   layout, update launcher rule/docs and regenerate assets.
3. Run affected/full Go tests/vet and frontend checks. Integrate/push main. Build
   static binary, preserve remote rollback binary, restart only devkit-web and verify
   exact served assets/version plus unchanged VM/container/T3 identities and config.
4. Record deployment receipt/host notes and remove the merged task worktree.

## Checkpoint

Home/away and console-network regressions pass; full Go suite/vet and all 34 frontend
contracts pass. Production build passes. Three browser regressions pass with fresh
fixtures: desktop/mobile LAN and Tailscale clipboard commands, direct details, protocol
switching, project view and overflow. The first browser run reused stale fixture-server
data; restarting that task-owned server resolved the harness issue without code changes.
Single-agent review and screenshot inspection complete. Reuse v9 disposable QEMU and
unchanged dashboard/project flow evidence. Main integration and guarded live deployment
are the remaining operations; the supplied LAN SSH address is verified reachable.


## Completed rollout

Implemented/integrated/pushed as e4ad5e6 (following f5a20c5). Deployed static binary
SHA-256 `2dd47d954d5e8000eb1f1d0e3f6142344da7f6af9a69900be874ed53ad686041`
to the LAN host on 2026-11-14 using its existing guarded deployment runner, extended
with VM PID/start-tick preservation. Only devkit-web restarted. Exact executable,
trusted HTTPS commit and three entry assets verified; inventory APIs healthy. All five
running QEMU identities, 17 running container start identities, T3 PID, host config
and secret registry were unchanged; vault remains ready/unsealed. No VM launchers changed.

Live read-only browser verification passed on the requested VM route: SPICE default,
LAN and Tailscale addresses/commands, seven inventory rows and no page errors.
Existing loopback listeners correctly remain tunnel-only. Direct client sessions from
a separate machine were not attempted; external access requires Tailscale connectivity.

Host receipt/rollback directory:
`~/.local/state/devkit/deployments/vm-connections-e4ad5e6-20261114/`.
Host STATUS.md records the rollout. This completion note changes documentation only;
the deployed code revision is e4ad5e6. Task worktree cleanup follows integration.
