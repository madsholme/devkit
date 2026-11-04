# 6. Explicit QEMU cleanup and optional purge

Status: accepted, 2026-11-04. Extends [VM lifecycle adapters](0003-vm-lifecycle-adapters.md).

The user needs cleanup by exact VM name or all registered VMs in an exact project.
They chose disk retention by default and an explicit optional purge. Add a dry-run
CLI with --yes execution, backed by the existing host lifecycle lock and project
recipes. Stop running guests using their configured adapter; verify absence before
removing only the corresponding recipe entry. Stopped plain mappings may be removed
without acquiring guest-control authority. Running read-only mappings block cleanup.

Data deletion is delegated to an opt-in standalone control.purge command. Reuse the
existing trusted-shell adapter contract, identity environment, timeout and process-group
cancellation. Never discover disk ownership from command-line arguments, remove storage
automatically, force-stop discovered PIDs, or extend this operation to libvirt domains.

Preflight the entire batch, then revalidate recipes/identities before each mutation.
Persist each successful unregister atomically, preserving unrelated JSON and file mode.
Conflicting ownership, incomplete configured discovery and symlink recipes fail closed.
Stop/purge errors preserve registration and halt subsequent targets. Log successful
steps independently so partial operations are visible. No cross-guest rollback is
promised: disk purge is irreversible and external launchers/editors do not use our lock.

The tradeoff is that projects supply safe, guest-specific stop/purge adapters and must
restore a removed registration to reuse retained disks through devkit. This avoids a
new VM storage manager and preserves existing read-only discovery boundaries.
