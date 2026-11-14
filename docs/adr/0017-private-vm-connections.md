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
