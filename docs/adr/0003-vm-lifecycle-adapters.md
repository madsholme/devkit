# 3. Opt-in VM lifecycle adapters

Status: accepted, 2026-11-03. Extends ADRs 1 and 2.

The user wants Docker-like lifecycle controls and selected existing launchers plus
libvirt. Add explicit control settings alongside VM selectors, retaining read-only
behavior for plain mappings. Project hooks bridge standalone QEMU and SwiftConfig's
native engine; local libvirt uses UUIDs and native start/shutdown commands.

Definitions are the source of stopped inventory. Opaque recipe/selector IDs connect
dashboard actions to server-discovered recipes; requests cannot supply commands,
URIs or directories. Conflicting control definitions are rejected even across
worktrees with the same project slug. A process lock and fresh state checks protect
CLI/dashboard concurrency. No force shutdown or storage deletion is introduced.

This avoids reconstructing commands from process arguments, exposing secrets or
inventing a new VM provisioning format. The tradeoff is a small project adapter
contract: launch must detach/return and stop must safely terminate only that guest.
Startup verification detects the VM process/domain, not guest application health.
