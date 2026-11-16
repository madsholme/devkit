# 18. Agent reports and runtime-specific maintenance boundaries

Status: proposed, 2026-11-16. Implementation is specified by
[agent operations v1](../plans/2026-11-16-agent-operations-v1.md).
Extends ADRs 1, 3, 8 and 17; it does not replace their authority boundaries.

Agents currently combine human-readable preview/doctor output with separate VM
inventory, and project onboarding often discovers errors only during launch.
Existing preview resume uses saved Compose; refreshing configuration currently
requires a separately managed recreation/data-retention decision. QEMU launchers
already support explicit lifecycle operations but are arbitrary scripts.

Adopt versioned, allowlisted reports for project validation, checkout context and
new JSON output modes. Share report types and existing validation/identity logic,
while keeping runtime-specific collectors and mutations separate. Preserve existing
VM/secrets JSON contracts and human-readable commands. Unknown evidence remains
unknown; reports never grant authority to a later mutation.

Extract structural Compose checks from rendering so validation does not inspect an
unbuilt review image, reserve media ports or run hooks. Retain existing runtime
validation rather than weakening it to make preflight easier. New diagnostics may
expose unsafe existing configurations without silently changing launch semantics.

Offer a planned, explicitly applied live-preview rebuild with unchanged persistent
storage and tightly bounded supported changes. Shared per-preview locking and private
recovery records cover all relevant saved-config mutations, including share expiry.
No automatic database rollback, secret disclosure or storage deletion is introduced.

For QEMU/libvirt, add validation/context and a read-only view of the existing restart
operation. Preserve opt-in launcher/libvirt authority, native stop-only boundaries,
graceful shutdown and private connection rules. Do not reconstruct desired storage
or networking from shell scripts or treat a process/listener check as guest health.
VM rebuild, disk replacement, snapshots and automatic reconfiguration are excluded.

A universal runtime apply engine was rejected because the existing launcher contract
does not describe resources sufficiently for transactional application or rollback.
Additional skill prose alone was rejected because it cannot establish runtime facts.
The tradeoff is explicit unsupported/unknown cases and a narrower rebuild capability,
in exchange for preserving data, identity and existing lifecycle ownership.
