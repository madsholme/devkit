# Viewer protocols and resource shutdown — version 8

Supersedes [completed v7](archive/2026-11-04-qemu-cleanup-v7.md). Single-agent execution
in the existing codex/projects-qemu worktree. New requirement: VNC and SPICE connections
and turn-off actions for VMs and individual Docker containers.

## Design and scope

Retain existing discovered endpoints and SSH tunnels; display both protocols when a VM
exposes both. Never invent a SPICE endpoint for a VNC-only guest or reconfigure/restart
a real guest to add one. Fix display labels being overwritten by argument order.

Reuse authenticated CLI-backed dashboard jobs and existing managed VM adapters. Add
stop-only capabilities for live resources outside those adapters: local QMP Unix socket
for same-user standalone QEMU, native shutdown for discovered local libvirt domains,
and individual Docker stop by full container ID. UI calls this Turn off and confirms
the exact target. No deletion or force-off feature; preserve disks and registrations.
CLI: stop --vm CONTROL_ID (including discovered live IDs), stop --container NAME_OR_ID.

Native QEMU authority binds PID/start time/socket to an opaque ID, freshly resolves it,
and verifies Unix peer UID/PID before QMP capabilities and system_powerdown. Bound I/O,
reject unsupported/missing sockets, stale identities, incomplete discovery and paused
states; wait for observed shutdown, report timeout without killing. Existing managed
controls retain their ownership/conflict guards. Local libvirt uses discovered UUID/URI.
Docker actions re-resolve full IDs; graceful stop waits without daemon force-kill, with
a bounded client deadline; reject auto-remove containers to preserve data. Action jobs,
confirmation, auth, history and inventory refresh apply to both resource types.

Approach: extend existing capabilities and jobs; avoid converting plain mappings to
launcher recipes or adding generic process killing. This supports the actual restored
guests through their existing QMP sockets without guessing how to restart them.
The user confirmed all discovered resources with identity checks and graceful shutdown; resources
without a safe native mechanism show a reason instead of an unusable button.

## Tasks and verification

1. Connections: regressions for both protocols and argument/XML ordering; preserve
   individual URI/tunnel generation and TLS/socket exclusions. Fix labels and docs.
2. VM shutdown: add QMP parsing/identity checks, bounded protocol exchange, live
   capability catalog and stop dispatch. Test peer identity, PID reuse, malformed/error
   responses, missing sockets, unsupported state, timeout, auth and managed precedence.
3. Docker shutdown: exact-name/full-ID resolution, fixed CLI arguments, state and data
   retention guards, fresh revalidation, confirmation and history tests.
4. Dashboard and docs: Turn off buttons in VM/project/container views, confirmation,
   anonymous hints and refresh after jobs. Exercise synthetic UI and real disposable
   QEMU/container flows, including VNC and SPICE on one guest.
5. Single-agent review: full Go suite, vet, formatting, build, diff and acceptance checks.
   Preserve running user guests/services during verification. Reuse prior authorized
   release workflow after verification: commit/push branch, deploy, update PR, merge to
   own main and push; retain the worktree and open upstream PR.

## Progress

- Baseline protocol tests pass. New dual-protocol regression fails: local/last display
  overwrites the combined label. Live devbox guests expose VNC only; restored guests
  have no configured adapters, and the running Docker containers are outside devkit.

- Implementation and single-agent review complete. Full Go suite/vet and formatting
  checks pass. Real Docker shutdown and ACPI guest exit pass, retaining disk/recipe
  hashes and container volume/data. Browser verification covers anonymous hints,
  sign-in, project/VM/container controls, cancel/confirm, both viewer URIs, successful
  jobs and refresh; no browser console warnings/errors in the corrected run.
- End-to-end verification found an exited QEMU awaiting parent reaping was treated
  as unreadable inventory. A failing regression reproduced this; discovery now ignores
  zombie guests even after their executable link disappears. Fresh runtime/UI run
  passes. The first run's failed job evidence was retained privately; it is not counted
  as a pass. Tests also require successful history events, not just matching names.
- Graceful Docker controls reject auto-remove and SIGKILL-configured containers.
  No live user guest/container was shut down. Real native libvirt shutdown remains
  covered through code/fixtures and existing provider tests, not a live libvirt guest.
- Final binary acceptance also passes the pre-existing launcher lifecycle and named/
  project cleanup/purge runner. Final Go suite and vet pass. Formatting and diff checks
  are clean. Implementation verified; release operations continue under the earlier
  user authorization, with private deployment receipts and Git/PR refs as evidence.
