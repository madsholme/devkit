---
name: devkit-vms
description: Use when creating, changing, discovering, starting or connecting to QEMU or libvirt virtual machines on a devkit host, including SPICE, VNC, guest SSH, LAN, Tailscale and SSH tunnel connection issues.
---

# Devkit VM connections

Graphical QEMU guests default to **SPICE without a display password**, unless the
user requests password authentication. VNC is an optional compatibility
fallback; headless guests use SSH. Every VM must support direct private-network
access and an optional SSH tunnel, with distinct **At home (LAN)** and **Away
(Tailscale/VPN)** details.

## Inspect before changing

Run from the intended project checkout. Read its AGENTS.md/CLAUDE.md, nested launcher
guidance, saved launcher configuration, and the host's `~/.config/devkit/STATUS.md`
when present. Start with read-only discovery:

```sh
devkit vms --json
ip -brief address
tailscale ip -4
ss -ltn
```

Use the actual host LAN interface and Tailscale address. Do not copy another host's
addresses, use Docker/libvirt bridge addresses as workstation routes, or treat a
public dashboard hostname as a SPICE endpoint. A loopback listener requires a tunnel.
Binding a port does not create a client route: away access requires an active VPN
and applicable tailnet policy. Check routing on the client as well as the host.

## Launcher rules

- Reserve fixed, distinct per-VM ports for SPICE and guest SSH. Preserve disks,
  firmware, CPU/memory, guest credentials and existing unrelated launcher options.
- Prefer explicit reachable private bind addresses. QEMU SPICE accepts one bind
  address. To serve both LAN and Tailscale with a wildcard listener, first establish
  a deny-by-default guard for those ports, allowing only the intended private
  interfaces, source networks and destination addresses, plus loopback for tunnels.
  Apply and verify the guard **before** opening listeners; fail closed if it fails
  or the host addresses change. Disable unprotected address families. A wildcard
  bind alone is insufficient. Firewall changes require authorized host setup.
- Retain guest SSH authentication. New SPICE launchers default to no display password
  (`disable-ticketing=on`) unless the user requests one. This default applies only
  behind the private-network restrictions above; never expose an unprotected public
  display. Preserve existing display passwords unless their removal is explicitly
  authorized. Do not interpret this preference as permission to remove SSH authentication.
- When a SPICE password is requested, use the **devkit-secrets** skill: select the
  intended project/worktree context and keep values out of Git, argv, logs and URIs.
  Supply QEMU's secret object through protected runtime input, such as a memory-only
  file descriptor. A sealed vault blocks password-protected startup; never silently
  fall back to a password-free listener or plaintext storage.
- Check actual listening sockets for port conflicts; closed connections in TIME_WAIT
  are not active listeners. Let QEMU reserve its ports atomically and handle failure.
- Preserve user-edited launchers and managed instruction blocks. Reconcile conflicting
  older loopback-only rules with the user's authorized policy; do not silently delete
  project instructions or assume this skill overrides explicit user constraints.

## Activate and hand over

Discovery and connection/UI work never authorizes changing running VMs. Changing
saved launchers applies on the next QEMU start, not an in-guest OS reboot. Obtain
restart approval when it is not already explicit. For an authorized rollout, restart
one guest at a time gracefully, restore its previously active services, and verify
before continuing. Do not force-stop a stalled guest without authorization.

For **both** home LAN and away Tailscale, record protocol, host, port, viewer URI or
SSH command, tunnel command and its matching destination. Keep local tunnel binds
on loopback; forward to the actual reachable listener. Report password key/scope,
never its value. Verify the requested display-authentication mode, guest SSH and direct reachability from an
appropriate client. Distinguish host-only checks from real VPN/client sessions.
Devkit discovers actual listeners; it does not enforce this policy or provision
firewalls through inventory. Never advertise staged settings as already active.
