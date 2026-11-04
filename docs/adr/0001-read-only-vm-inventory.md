# 1. Read-only QEMU and libvirt inventory

Status: accepted for the initial VM dashboard, 2026-11-03.

The host runs both standalone scripted QEMU guests and potentially libvirt domains.
Requiring libvirt registration would omit the existing headless and SPICE workflows.
Use Linux process metadata for standalone guests and optional read-only virsh queries
for session/system domains, deduplicated by UUID. Keep VM inventory in a separate
API request so libvirt failures do not block Docker previews. Cache briefly and bound
external probes; project remains standard-library Go and dependency-free browser JS.

Expose selected metadata and generated private SSH commands, never raw arguments,
XML, credentials or disk paths. No QMP control, VM lifecycle, guest agent, registration
store or public tunnel is introduced. Display and guest SSH access use the user's
existing SSH authentication; unsupported connections remain explicit manual cases.

This keeps the host's existing VM workflows intact. The tradeoff is incomplete data
for unreadable processes, advanced block graphs, bridged guest IPs and GPU usage.
These must remain unavailable rather than inferred. Bootstrap is optional, with
bounded distro/architecture package profiles and normal local libvirt permissions.
