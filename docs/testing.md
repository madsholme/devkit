# Acceptance check

1. Create a Git repository from the sample and commit it.
2. Create worktrees for branches `try/a` and `try-a` (e.g. `git worktree add -b try/a ../try-a`);
   verify distinct names/URLs.
3. Start both live previews. Change each `message.txt`; verify different responses.
4. Increment one preview's counter; the other must stay unchanged.
5. Commit one task; run `devkit review HEAD`. Change its live message again; the
   review must keep the committed message.
6. Increment the review counter, stop/start it, and verify the count persists.
7. Open the same URLs from another computer on the tailnet. Disconnect the first client; agents and
   previews should continue on the Linux host.
8. Configure the publishing adapter and complete one team review on your destination.

Local validation commands: `go test ./...` and `bin/devkit --help`. Full runtime acceptance requires a Docker daemon,
the Linux host, real domain/DNS settings and a configured publishing destination.

The Docker-only check can run with `DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`.
It uses temporary repositories and containers with loopback-only test HTTPS,
does not issue certificates or alter DNS, and removes its test containers/volumes.
The media check is `DEVKIT_RUNTIME_TEST=1 python3 tests/media_acceptance.py`; it
requires local Tailscale, creates temporary UDP/TCP echo previews, checks distinct
ports and restart persistence through the tailnet IP, and cleans them up.


QEMU discovery regression tests run with `go test ./...`. Installer behavior is
checked without sudo, package installation, or host changes by
`python3 tests/bootstrap_acceptance.py` (mocked Ubuntu/Fedora commands, architecture
profiles, existing daemon preservation, repeated group setup and failures).

Build the binary, then run `DEVKIT_VM_RUNTIME_TEST=1 python3 tests/vm_runtime_acceptance.py`
for the optional x86_64 QEMU smoke check. It needs `qemu-system-x86_64` with SPICE and
`qemu-img`. It creates two paused, 64 MiB TCG guests with no OS, one with a temporary
32 MiB qcow2 disk and SPICE, the other with a localhost SSH forwarding listener.
It verifies real process discovery, sizes, connection commands, metadata privacy
project mapping (including an unmatched guest), and `/api/vms` CPU sampling. Its temporary web server stubs Docker to isolate
background cleanup from real containers. It terminates only its own processes and
removes its temporary files. This does not test a booted guest login or graphical
session; those need an existing test guest and client. Libvirt parsing, access
failure and deduplication are covered by fixtures; live libvirt validation needs
an active domain visible to the devkit account.

### VM lifecycle acceptance

Build the binary, then run `DEVKIT_VM_RUNTIME_TEST=1 python3 tests/vm_lifecycle_acceptance.py`.
It creates a temporary paused 64 MiB TCG guest and project launcher, verifies
start/idempotence/restart/stop, failed-stop protection, stopped inventory and
unchanged disk contents, and cleans up its own QMP socket/guest. It never uses a
real user's VM. `DEVKIT_VM_BINARY` can select a test binary outside the checkout.
The no-OS fixture uses QMP quit; production adapters must use graceful guest shutdown.
Go tests cover local libvirt command arguments/state handling, missing/denied
providers, conflicts, lifecycle locking and authenticated dashboard target checks.
