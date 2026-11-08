# Acceptance check

1. Run `devkit project create sample` in an isolated configured projects_dir; verify its initial commit.
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


Project discovery and QEMU regression tests run with `go test ./...`. They cover
shallow directory discovery, external linked worktrees, recipe validation, Docker
failure and rejection of arbitrary or removed launch paths.
Git control tests use disposable local bare origins and clones, without contacting
project remotes. They cover fetch-only, fast-forward sync, separate exact-target push,
dirty/detached/diverged branches, ignored-file preservation, upstream validation, in-progress operations, repository
locks, pre-push hook rejection, discovery revalidation and anonymous action denial.
Build `bin/devkit`, then run `python3 tests/git_runtime_acceptance.py` for the actual
authenticated dashboard job → CLI → local Git round trip (or set `DEVKIT_GIT_BINARY`
to an alternate build). It uses temporary credentials and stubbed Docker housekeeping,
and removes its server and disposable repositories afterward.
Container inventory tests cover managed/unmanaged metadata, stopped/paused status, secret
omission and Docker failures. These run in `go test ./...`.

For UI changes, exercise the Projects Git controls and Containers tab using synthetic
fixtures: signed-in/out states, action targets, text/status filters, empty/error/recovery,
Details/Logs availability, tab deep links and narrow viewports. Never test Git actions
against a user's project merely to validate the UI.

Installer behavior is
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

QEMU cleanup regressions cover exact name/project selection, dry-run and --yes, blocked
batches, missing stop/purge authority, retained files, adapter failures/timeouts, changed
identities/recipes, symlink rejection, legacy recipe names, unrelated JSON preservation,
lifecycle locking and partial-success history. The lifecycle acceptance runner also
starts its disposable guest for named and project cleanup, checks retained disk hashes,
and verifies --purge removes only its explicitly configured fixture files. No real
user guest or disk is used.


### Native resource shutdown

Build the binary, then run:

```sh
DEVKIT_SHUTDOWN_RUNTIME_TEST=1 DEVKIT_TEST_IMAGE=YOUR_LOCAL_IMAGE \
  python3 tests/resource_shutdown_acceptance.py
```

Choose an existing image with `/bin/sh` and `sleep`; the test never pulls images.
`DEVKIT_VM_BINARY` can point at an alternate build. This creates one isolated Docker
container/volume and a small disposable ACPI-aware QEMU/TCG boot guest. The guest
handles the virtual power button and exits itself. The test checks both VNC/SPICE,
authenticated actions, missing confirmation, actual graceful exit, unchanged disk and
recipe hashes, retained container data/volume, history and inventory refresh. Docker
visibility and mutation are restricted to the exact fixture ID; optional providers are
stubbed. Cleanup removes only test-created resources. Set `DEVKIT_SHUTDOWN_UI_READY`
to a temporary descriptor path to pause for browser testing; create that path plus
`.continue` afterward. Its generated password is test-only; do not publish descriptors.

## Existing-container registration

`DEVKIT_EXISTING_RUNTIME_TEST=1 DEVKIT_TEST_IMAGE=<existing-sh-image> python3 tests/existing_containers_acceptance.py`
uses only its own network-isolated container and named volume. Build first, or set
`DEVKIT_VM_BINARY` to the candidate binary. The test proves project grouping,
registered details/logs with masked environment, revocation, authentication,
confirmation, start/stop history, and unchanged container identity/image/mounts/data.
A scoped Docker wrapper prevents the test server from mutating user resources.
`DEVKIT_QA_HOLD=1` retains the fixture for up to 15 minutes for a browser check and
prints the location of a private fixture login descriptor; create `finish` alongside
that descriptor to clean up early. This flag never retains user credentials.

## Frontend development and checks

Use Node 22.12+ (24 recommended) and pnpm 10.33.0. SwiftDesign snapshots are included in
`frontend/vendor/` with provenance, so installation needs no private package token or
sibling checkout. Dependencies are pinned by `frontend/pnpm-lock.yaml`.

```sh
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend test
pnpm --dir frontend build
pnpm --dir frontend exec playwright install chromium
pnpm --dir frontend test:ui
go test ./...
go vet ./...
go build -o bin/devkit ./cmd/devkit
```

To use installed Chromium, set `CHROMIUM_PATH=/usr/bin/chromium-browser` for `test:ui`.
The browser suite starts an isolated server on `127.0.0.1:4178` with invented host data.
It covers all six views, anonymous/signed-in controls, exact confirmation targets,
sharing, details, log follow, pairing, system sampling, failed discovery, expired
sessions, theme selection and mobile navigation. Project checks cover list/detail navigation,
worktree resource isolation, project-wide branch links, ambiguous VM assignments, deep links,
reload/back behavior and missing selections. Unit tests pin exact container association and
opaque VM worktree mapping; Go tests retain UUID precedence, duplicate-claim handling and
private-path/command exclusion. It executes no host actions.
Screenshots are written to `/tmp/devkit-swift-*.png`; test artifacts stay gitignored.

For an interactive synthetic preview, run `node frontend/tests/fixture-server.mjs`
after building and open `http://127.0.0.1:4178`. Its password is `demo-password`; it
simulates operations only. For development against a deliberately chosen local devkit
server, `pnpm --dir frontend dev` proxies `/api` to `127.0.0.1:3780`. The production
server retains its same-origin checks; use the embedded build to verify real sign-in
and mutations, or configure a separate authorized test host.

Commit generated `web/dist/` with frontend source. The frontend CI job rebuilds and
rejects stale assets, while Go asset tests check that HTML references (including theme
boot) are actually served with the correct content type and caching. Release bundles
continue to build directly with Go, using these checked-in assets. Do not hand-edit
`web/dist/`. The standard-library-only backend and its auth/ownership tests remain intact.

Project creation coverage in `go test ./...` checks complete template copying, notes preservation,
origin and initial commit, duplicate paths/symlinks, concurrency, invalid input, missing identity,
retained partial failures and authenticated action planning. `tests/git_runtime_acceptance.py`
also exercises real HTTP job → CLI stdin → template/Git creation and duplicate rejection using
temporary directories. `DEVKIT_GIT_BINARY` selects the binary. Frontend `test:ui` covers the
Create Project modal, cancel, preserved guidance, duplicate errors, retained input and catalog refresh.

Document tests cover registered main/linked checkouts, empty catalogs, root agent aliases,
read-only guidance, nested creation, preserved modes, stale/concurrent writes, traversal,
links, special/oversized/non-text files and private stdin action plans. The real Git HTTP
acceptance test includes document creation above the old 64 KiB request limit, read/edit,
stale rejection and unchanged Git HEAD. Frontend tests cover Markdown escaping and safe
links; browser tests cover reading, checkout selection, relative links, create/edit preview,
authentication expiry, conflict retention and accepted/cancelled unsaved navigation.

## OpenBao Lite

Unit tests use synthetic credentials, temporary config/state and an in-process HTTP
KV fixture. They cover CAS/inheritance, protocol boundaries, disabled/`never`
cleanup, recovery and overdue-resume barriers, private runtime delivery, migration,
update backup ordering, Git HTTPS matching and preservation of edited agent assets.
The browser secret test verifies generation/metadata/reveal and clearing on
navigation/logout. These do not prove a real OpenBao restore or external DNS/TLS.

```sh
go test ./...
go test -race ./cmd/devkit -run 'TestSecrets|TestSecretCleanup|TestVault|TestGitCredentials'
python3 tests/bootstrap_acceptance.py
pnpm --dir frontend test
pnpm --dir frontend build
pnpm --dir frontend test:ui
# Build into a disposable path, not the running host binary:
go build -o /tmp/devkit-secrets-acceptance ./cmd/devkit
python3 tests/agent_secrets_acceptance.py --devkit /tmp/devkit-secrets-acceptance
DEVKIT_OPENBAO_RUNTIME_TEST=1 python3 tests/openbao_runtime_acceptance.py
# Also requires age and age-keygen (Fedora: sudo dnf install age;
# Debian/Ubuntu: sudo apt-get install age):
DEVKIT_OPENBAO_RUNTIME_TEST=1 DEVKIT_OPENBAO_ENCRYPTED_TEST=1 python3 tests/openbao_runtime_acceptance.py
# Includes encrypted recovery of the pre-upgrade backup:
DEVKIT_OPENBAO_RUNTIME_TEST=1 DEVKIT_OPENBAO_UPGRADE_TEST=1 python3 tests/openbao_runtime_acceptance.py
```

The OpenBao runner requires Docker, pulls the pinned image if absent, and uses a
unique test Compose project, private temporary bind directory and loopback ports.
It tests real initialization/provisioning, root retirement, restart/manual unseal,
Raft snapshot restore, KV version deletion/restoration, cleanup disabled past expiry,
explicit scope recovery/purge, and metadata/native-audit/container configuration
redaction. The native audit device is declared in server configuration and verified
during provisioning; API audit creation stays disabled. It never
uses deployed credentials or the `devkit-vault` Compose project. Opting in without
Docker is a failure, not a simulated pass.

The encrypted profile invokes Devkit's actual backup pipeline with a disposable
Traefik certificate volume. It checks encrypted Raft/registry/cleanup/host/ACME
components, wrong-key and tampered-ciphertext rejection, and proxy restart/partial
output cleanup after encryption failure. It restores into fresh OpenBao bind storage
using the original unseal key, logs in, provisions a new backend identity, and checks
values, inheritance, history, deleted-key recovery and cleanup off. A separate
Traefik volume restores the ACME archive with mode/owner checks and serves the
original generated test certificate through hostname-verified TLS. Test decryption
keys stay in a private `/dev/shm` directory and decrypted components in memory;
all disposable containers, volumes and files are cleaned up. It uses no public CA,
Cloudflare credentials or deployed proxy state. Release CI runs this profile.

The upgrade profile starts with OpenBao 2.7.1 and invokes the real `vault update`
CLI handler for 2.7.2. It checks read-only `--check`, failed encryption blocking the
upgrade, a complete pre-upgrade encrypted backup, preservation of the same bind
directory, target image/version, sealed reads, original-key manual unseal, secret
persistence across another restart, and rejection of an in-place downgrade. It
then decrypts the exact backup named in the update record and exercises the recovery
profile in fresh **2.7.1** storage, leaving the upgraded instance intact. This proves
that recovery route for this patch pair; it does not validate every permitted
minor-version transition. Release CI runs both the normal and upgrade profiles.

Actual public certificate issuance/renewal and real Cloudflare consumers,
and LAN/Tailscale clients still require the operational checks in
[Development secrets](secrets.md#backups-and-updates).

The agent asset runner creates a disposable home and checks both install targets,
identity/version and user-edit preservation. It **does not launch agents** or claim
fresh-session discovery. For that separate acceptance, use a disposable enrolled
project and a fresh Codex session, then a fresh Claude Code session in their real
launch environment (including T3 when used). Request a temporary development
password without mentioning the skill. Verify context/list precede creation,
explicit scope, no value in chat/logs, correct UI metadata and runtime injection.
Repeat with a sealed vault and cleanup off; neither agent should use plaintext
fallback or change retention. Record each actual agent/version/result, plus LAN
and remote Tailscale client DNS/TLS/native-login results. Unavailable environments
remain pending. Respect any instruction prohibiting agent subprocesses.
