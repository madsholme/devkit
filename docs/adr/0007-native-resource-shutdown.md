# 7. Verified native shutdown of discovered resources

Status: accepted, 2026-11-04. Extends ADRs 1 and 3; preserves ADR 6 cleanup authority.

The user explicitly requested Turn off for all shown VMs and Docker containers,
including resources outside devkit projects. Extend discovery with narrow stop-only
capabilities while retaining authenticated CLI jobs, confirmations and fresh identity
checks. Existing VM adapters and their ownership guards take precedence.

Standalone QEMU uses only an absolute local QMP Unix server socket discovered in its
arguments. An opaque identity includes PID/start time/socket; peer credentials must
match that process and the service user, followed by another identity check before
system_powerdown. Local libvirt uses its discovered URI/UUID. Never signal a guest PID
or force shutdown; wait for observed exit and report failure/timeout honestly.

Docker resolves exact names to immutable full IDs, revalidates after locking, and calls
stop with the daemon's kill timeout disabled. The bounded client may time out while
Docker still awaits graceful exit. Refuse auto-remove containers to retain data.
This grants neither unmanaged detail/log access nor start/restart/delete authority.
Configured cleanup remains opt-in and never acquires native stop/purge authority.

Alternatives rejected: generic process signals lack guest cooperation; inventing
start/stop recipes from arguments risks replaying secrets and stale launch state.
No new dependencies, privilege elevation, network listeners or guest reconfiguration.

Protocol references: [QEMU QMP](https://www.qemu.org/docs/master/interop/qemu-qmp-ref.html)
and [Docker stop](https://docs.docker.com/reference/cli/docker/container/stop/).
