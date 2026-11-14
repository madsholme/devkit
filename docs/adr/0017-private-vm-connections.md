# 17. SPICE preference and direct private VM connections

Status: accepted, 2026-11-14. Extends ADR 1 and the v8 connection behavior.

The user requires SPICE as the default graphical connection, both SSH-tunnel and
direct access, and visually distinct instructions. Existing launchers/running VMs
must remain unchanged. New/changed launchers follow docs/projects.md's connection rule.

Keep discovery read-only and accept explicit private LAN/Tailscale listeners as well
as loopback/wildcard binds. Preserve the bind address for tunnel targets. Add sanitized
listener/direct endpoint metadata to the existing connection contract; retain old
command/tunnel fields. Wildcards use only matching-family locally assigned private
addresses, never the SSH hostname as a guessed direct endpoint. No remote probing,
firewall changes, launcher rewriting or authentication changes.

SPICE sorts first and is selected by default in the shared connection UI. VNC/SSH
remain selectable; show direct unavailable with a reason for loopback-only listeners.
TLS/socket/public/link-local configurations retain manual fallback. Address/port
availability is configuration evidence, not a guarantee of routing from the client.

This extends the original tunnel-only boundary without making discovery a provisioning
system. The cost is that old launchers need a separately authorized migration before
they can meet the direct-access rule. Multiple wildcard private addresses may include
bridge/VPN addresses; the client must select a reachable route.

The user's subsequent clarification requires explicit home/away variants. Show LAN
and Tailscale SSH host destinations separately, with network labels on direct endpoints.
Both SSH variants forward to the actual VM listener, including loopback-only guests.
External access is Tailscale/VPN only. Exclude inactive and conventional Docker/libvirt
bridge/veth interfaces from host address suggestions; retain explicit VM bind addresses.

Agent distribution extension, 2026-11-14: ship a versioned `devkit-vms` skill through
the existing protected Codex/Claude asset installer, host setup, operator repair and
diagnostics. Setup installs guidance regardless of vault configuration. Project
installation manages separate secrets/VM blocks and preserves edits; templates
include the VM block. Existing per-agent status JSON remains compatible and reports
current only when both assets are current. New sessions discover installed skills;
project instructions explicitly direct VM tasks to the skill.

Explicit private binds remain preferred. The user approved firewall-restricted wildcard
listeners where a single SPICE bind must serve LAN and Tailscale, provided a verified
interface/source/destination firewall guard precedes startup and denies other access.
This is an authorized provisioning rule, never an inventory side effect. Host-specific
addresses and credentials are discovered/configured locally, not bundled into skills.

The subsequent user preference defaults new SPICE launchers to no display password
unless requested. Private-network guards remain mandatory; SSH authentication and
existing display passwords are preserved unless explicitly authorized otherwise.
Requested display passwords use devkit-secrets and fail closed when unavailable.
This instruction change does not reconfigure or restart existing VMs.
