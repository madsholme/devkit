# Agent operations and QEMU checks — v1

Status: proposed implementation plan, 2026-11-16. Planning is authorized; runtime
implementation, deployment and VM restarts have not been requested by this task.

This is the authoritative plan for the next operations work. The completed
[agent guidance v14](2026-11-16-agent-guidance-v14.md) remains its own delivery record;
it is not superseded. Material changes to the scope or strategy below require v2
and archiving v1. Architecture is recorded in proposed
[ADR 18](../adr/0018-agent-operation-reports.md).

## Goal and approach

Give humans, Codex and Claude reliable answers to three questions: is this project
configured correctly, which resources belong to this checkout, and can its running
environment be refreshed without Devkit deleting persistent data? Cover Docker
previews and QEMU/libvirt where their existing contracts support the same question.

Implement CLI commands first using existing Go validators, inventory and authority
checks. Share small typed report structures and pure validation functions; keep
Docker rendering, VM discovery and lifecycle execution separate. No new dependencies,
generic provider framework or VM provisioning format.

Considered approaches:

- More skill prose alone: cheap, but agents would still reconstruct state and
  discover invalid configuration through attempted launches.
- A shared report contract over existing commands and bounded runtime-specific
  checks: recommended; it improves automation without replacing working adapters.
- A universal rebuild/apply engine for containers and VMs: rejected for this scope.
  Arbitrary VM launcher scripts do not expose desired disks, firmware or networking
  as a declarative configuration that Devkit can safely apply or roll back.

Deliver validation and context before the higher-risk container rebuild. QEMU gets
validation, context and an exact-target restart preflight, not a disk rebuild.

## Intended command surface

These commands are proposals, not currently implemented CLI documentation.

| Command | Contract |
|---|---|
| `devkit project validate [--runtime all\|containers\|vms] [--json]` | Validate the current recipe and requested configured components; default runtime is all. Never launch hooks, containers or guests. |
| `devkit project validate --vm TARGET [--json]` | Validate one exact mapping in the current recipe and its observed control/connection evidence; implies vms and cannot combine with `--runtime`. |
| `devkit context [--json]` | Current checkout/Git identity, effective recipe, exact preview associations and VM associations/capabilities, with explicit unavailable evidence. |
| `devkit ls --json` | Structured equivalent of existing preview listing, with full commit identifiers and preserved text-mode behavior. |
| `devkit doctor [--agents] --json` | Structured existing diagnostics; preserve existing checks, text mode and failure exit behavior. |
| `devkit rebuild [PREVIEW] [--yes] [--json]` | Plan a supported live-preview rebuild by default; `--yes` applies after fresh validation. Retain volumes and preview identity. |
| `devkit restart --vm TARGET --dry-run [--json]` | Describe the existing restart operation and blockers without executing it; ordinary restart remains the explicit mutation. JSON is initially supported for this dry run only. |

No dashboard actions or new HTTP endpoints are needed in v1. Existing dashboard
mutations must still respect new shared lifecycle locks. Skills use the CLI reports.

## Shared reporting and privacy

New JSON modes emit exactly one object on stdout. Progress and sanitized errors
belong on stderr. Reports contain `schema_version: 1`, `command`, `complete`, `data`
and `checks`. A check has stable `code`, `status` (`ok`, `warn`, `fail`, `skip`),
`target`, `message` and optional `remediation`; remediation is advice, not executable
shell supplied by a recipe. Sort resource/check lists deterministically.

Use explicit null/unknown values instead of treating missing evidence as zero,
stopped, healthy or safe. New validation/context/rebuild commands return 0 for a
complete result with no failed checks, 1 for a failure or required unavailable
evidence, and 2 for invalid invocation. Warnings alone can return 0; non-applicable
components are skips. Preserve doctor's existing warning/failure exit semantics
and make `complete: false` visible if probes are unavailable. Existing `vms --json`,
secrets JSON and agent status formats retain their current shape.

Allowlist fields rather than serializing recipes or internal structs. Do not emit
environment values, rendered Compose, shell hook text, credential sources, raw QEMU
arguments/XML, disk paths or secret-bearing command diagnostics. Current checkout
and effective recipe paths are allowed in local context output; do not enumerate
unrelated private paths. VM connections retain existing sanitized home/away details.
Private comparison artifacts containing rendered environment must remain mode 0600
inside owner-only state. Do not publish secret-derived configuration hashes.

Context and validation do not fetch Git, inspect vault values, run credential-source
commands, repair guidance, write recipes, allocate ports or record mutations.
Do not call the full host doctor as a shortcut: its existing operational diagnostics
have a broader probing scope. Adding `doctor --json` changes serialization only.
Existing doctor detail strings include credential-source descriptions: add safe
structured messages/codes at their producers rather than serializing those strings
or trying to redact arbitrary text after the fact. Preserve text-mode diagnostics.

## Project validation

Reuse `configFile`/`loadConfig` and the existing VM, route, media and Compose ownership
validators. Preserve `devbox.json` precedence/aliases and unknown recipe fields.
Validation never rewrites a file. Keep malformed/missing recipes distinct from
missing runtime prerequisites and invalid recipe fields.

For `all`, include container previews when the recipe declares Compose files or
the default live/review Compose files exist; include existing-stack registrations
and VM mappings when present. An empty recipe with no usable component fails.
VM-only and existing-stack-only recipes do not require preview Compose files or a
review build hook. Explicitly selecting containers/vms with no such component fails.

Preview checks cover both live and review capability. A missing review file/build
hook is a warning that review is unavailable for a live-only recipe; a declared but
invalid review configuration fails. Validate the web service/port, route targets,
health checks, Compose-scoped volumes/networks, forbidden fixed container names/host
ports/network mode, and review build/bind-mount restrictions. Report missing health
checks as failed validation without silently tightening existing launch behavior.
Require the review build hook when validating a supplied review configuration;
`publish` remains optional. Do not execute prepare/check/build/publish hooks.

Split structural Compose validation out of `render`: today it also injects routing,
handles media variables and inspects a built review image. Preflight must work
before that image exists, using marked validation-only DEVKIT/DEVBOX identifiers.
Render through bounded `docker compose config --format json`; never invoke image
pull/build, digest resolution, port reservation or `savePreviewCompose`. Missing
user interpolation inputs are reported as unresolved requirements, not fabricated
secrets. Do not print rendered output or Compose stderr containing values. Refuse
remote includes/model inputs; local Compose includes/env files must remain within
the explicitly validated checkout. Existing ordinary launch behavior is preserved.

The Compose plugin may be needed for model parsing, but a stopped/unavailable Docker
daemon does not prevent static recipe/Compose checks. Runtime ownership checks that
need the daemon are explicitly unavailable. Existing-stack validation uses exact
project/working-directory labels and existing unique-owner rules; it never adopts,
starts or recreates a container. Do not claim runtime health from structural validity.

## Context and QEMU coverage

Context reuses Git worktree identity, saved preview metadata, Docker inventory,
project mappings and bounded VM discovery. Separate saved configuration, observed
runtime state and current source HEAD; a live checkout SHA is not proof of the
contents of an image. Include full SHAs, dirty state, resource IDs, modes, URLs,
ownership/capability reasons and which observations are unavailable. Show all exact
matches instead of silently choosing between live/review or ambiguous VM claims.

Non-Git VM projects are supported through their recipe/mapping context with `git`
null. Docker failure must not erase VM results; libvirt failure must not erase
container results. Partial inventory must never grant an action. A response is a
snapshot, not durable authorization: mutations resolve/revalidate again.

QEMU/libvirt validation covers selector syntax, UUID/name uniqueness, provider
matching, mapping versus control authority, configured local libvirt URI, timeout
bounds, known stopped/running/paused state and incomplete discovery. Extract a
read-only target lookup that can explain unavailable targets: do not use
`resolveVMControl` unchanged because it rejects targets with no permitted actions.
Do not equate a plain mapping with launcher authority or remove native stop-only
capabilities already granted by separate exact-identity checks.
Report availability of the selected provider's tools and observable permissions.
Do not infer a required QEMU binary or KVM acceleration mode from an opaque shell
launcher, or fail a valid TCG guest merely because `/dev/kvm` is unavailable.

Connections report observed SPICE/VNC/SSH listeners, distinct home LAN and away
Tailscale direct/tunnel destinations, unavailable routes, conflicting observed ports
and public/wildcard exposure concerns. An absent listener on a stopped guest is
unknown, not failure of guest networking. Wildcard presence cannot prove a firewall
guard; report it as unverified and do not claim policy compliance. Host-only evidence
never proves actual LAN/VPN client reachability. Preserve existing display passwords
and SSH authentication. Inventory/validation never changes firewall or listeners.

Opaque shell launchers are not executed or parsed as declarative desired state.
For stopped standalone guests, future disk/firmware/network settings and launcher
dependency changes are unknown. Do not compare launcher text hashes to running
state and call that a reliable configuration drift check. Disk inspection, image
repair, snapshots, guest login and live/inactive libvirt XML reconciliation are
outside v1. QEMU explicitly warns that image queries can be inconsistent while an
image is being modified; this validator will not add live `qemu-img` probes.

The restart dry run shows exact control ID, provider, current state, stop/start
sequence and blockers using the same authority checks as execution. It says Devkit
invokes no purge/disk operation; it cannot guarantee what an arbitrary user launcher
does. It never runs the stop/start commands or restarts a guest. Execution remains
the existing graceful, host-locked, opt-in adapter path. Revalidate after a dry run;
if shutdown fails, do not start another instance or force-stop the guest. VM restarts
still require explicit user authority. There is no `rebuild --vm` in this plan.

## Data-preserving live-preview rebuild

Default is an inspectable plan. Applying requires `--yes`, exact ownership and an
existing saved live preview belonging to the current checkout/identity. No implicit
new preview, cross-worktree reassignment, review mutation or existing-stack conversion.
Retain the preview ID, private URL, Compose project and persistent volume identities.

V1 supports unchanged service topology and mount definitions, rebuilding services
with Compose `build` declarations and changing their non-storage configuration.
Reject added/removed services, anonymous volumes, changed volume names/drivers/options,
changed bind targets/sources or permissions, and changed unbuilt service images.
Database engine upgrades, data moves, credentials requiring account migration,
network/media changes and active public shares are explicit blockers with guidance;
do not unshare or reset data automatically. Existing media allocations can be reused
only with identical declarations/address/lease ownership, never reallocated during
preflight. An unsupported edit remains staged for a separately designed migration.

The storage promise is that Devkit does not delete/repoint storage. New app images,
entrypoints and user hooks may change database contents; this is not an application
transaction or backup/restore guarantee. Do not automatically run project prepare,
check, build or publish hooks during rebuild. Compose builds run their Dockerfiles;
required preparation/migration is explicit project work before retrying. Ordinary
`live`/`start` keep their current resume semantics.

Execution sequence:

1. Lock the exact preview, then re-read checkout identity, saved configuration,
   container/volume ownership and any active operation. Reject stale/ambiguous or
   incomplete evidence before mutation. An incomplete previous rebuild is a recovery
   blocker, not permission to start a new one.
2. Render privately, compare against the saved model and expose only allowlisted
   changes/blockers. Recheck file/model fingerprints before application. Track all
   local Compose/include/env inputs; build source remains a live worktree, so report
   its HEAD/dirty state without promising a reproducible commit build.
3. Create a bounded private operation record and retain old config plus resolved
   image IDs. Build candidates using operation-specific image tags so an existing
   running preview's mutable image tags are not overwritten. Build failure leaves
   the saved configuration and running containers unchanged.
4. Recheck identity, supported mounts, ownership and observed inputs after the build.
   For a running preview, apply candidate containers without `down --volumes`,
   `--renew-anon-volumes` or orphan removal; wait for defined health checks. For a
   stopped preview, update staged saved configuration without starting services and
   mark runtime verification pending until a later explicit start.
5. Publish saved configuration atomically only after success, retain private recovery
   evidence on failure, and record one success event. On apply/health failure, do not
   automatically roll back application/database state or rerun hooks. Preserve old
   and candidate config/image IDs and describe observed runtime state; make any later
   rollback an explicit, separately authorized operation after compatibility review.

A private bounded record tracks prepared/built/applying/complete/failed phases and
old/candidate artifacts. Interrupted application blocks ordinary lifecycle actions
that could resume stale saved config; diagnostics and exact graceful stop remain
available. A stop records the observed stopped state without clearing recovery or
recreating anything. Add an explicit
`devkit rebuild PREVIEW --recover [--yes] [--json]`: dry-run reports the existing
operation, and apply retries that same candidate without rebuilding only after
identity/config/storage checks. It must fail if candidate inputs no longer match;
it does not discard the record or guess a rollback. Document manual recovery for
unrecoverable interrupted state. Keep successful records bounded (latest only).

Use one nonblocking per-preview lock shared by live/start/stop/destroy/rebuild,
share/unshare (including expiry) and GC. Acquire preview locks before media locks,
never acquire in reverse, and process multi-preview cleanup one target at a time.
Share expiry must retry busy targets without dropping its ownership/expiry record.
Locks and recovery checks protect CLI/dashboard concurrency; they cannot serialize
arbitrary manual Docker edits, which require fresh observed-state checks.

## Implementation tasks and verification

Each task includes its code, focused tests and affected docs. Work as one agent;
reuse the existing seams/fixtures. Do not launch model subprocesses or live workloads
without authorization for that execution session.

| Task | Files and implementation | Acceptance evidence |
|---|---|---|
| 1. Reports and pure validation | New `cmd/devkit/operation_report.go`, `project_validate.go`, tests; extract structural checks from `preview.go`, reuse `repo.go`/`media.go`; wire `main.go`. | `TestProjectValidation*`: valid/invalid live/review, legacy recipe, missing inputs, local/remote include boundaries, secret sentinel redaction, no hooks/image inspection/leases or runtime writes. Existing render/review/media tests retain their outcomes. |
| 2. QEMU validation and restart preview | New `vm_validate.go` and tests; reuse/refactor target lookup in `vm_control.go`, selectors in `vm_projects.go`, bounded observations in `vms.go`; wire `main.go`. | `TestVMValidation*`/`TestVMRestartPlan*`: VM-only without Docker, stopped opaque launcher, ambiguous claims, incomplete inventory, libvirt provider mismatch, paused/unknown state, public/wildcard/loopback connection evidence; zero lifecycle calls. Existing restart guards unchanged. |
| 3. Context and structured diagnostics | New `context.go`/tests and report projections in `preview.go`, `doctor.go`, `agent_skills.go`; reuse `overview.go`, `projects.go`, inventory. | `TestContext*`, `TestListingJSON*`, `TestDoctorJSON*`: full identities, dirty checkout, multiple previews, non-Git VM context, unavailable provider, no secret/argv leakage, one valid JSON object on success/failure, unchanged old text/JSON formats. |
| 4. Rebuild planning and concurrency | New `preview_rebuild.go`, `preview_lock.go` and tests; update existing preview/share/media/GC mutation entry points to share locks and recovery guards. | `TestPreviewRebuildPlan*`/`TestPreviewLock*`: every unsupported storage/topology/share/review change refused before build, exact owner checks, stale recipe/env/model, no side effects on dry run, CLI/dashboard conflicts and share-expiry retry. |
| 5. Apply and recovery | Add private staging/operation record/atomic publication and phase recovery in rebuild files; reuse bounded `composeRun`, history and existing owner checks. | `TestPreviewRebuildApply*`/`TestPreviewRebuildRecovery*`: failed build leaves runtime untouched; stopped stays stopped; healthy update retains volume identity; post-build drift blocks application; failed/partial apply retains recovery evidence; retry resumes only the same safe candidate, never deletes data or silently rolls back. |
| 6. Runtime acceptance and guidance | Extend `tests/runtime_acceptance.py` with a rebuild case; extend VM fixture tests only where new diagnostics require it. Update README, previews/projects/testing/agent docs and relevant bundled skills/blocks/templates. | Disposable Docker fixture retains DB sentinel across app rebuild and sees new app behavior at the same URL; build failure and recovery paths checked without deleting volumes. Synthetic QEMU checks prove validation does not restart/change guests. Fresh Codex/Claude decision checks only when authorized and available. |

Dependencies: task 1 precedes 2–3; tasks 1–3 establish the agent-facing contract
before 4; 4 precedes 5; 6 closes the implemented scope. An initial useful release
can contain tasks 1–3 with documentation, but rebuild remains explicitly unfinished
until 4–6 meet acceptance. Do not call the whole plan complete after that milestone.

Run focused tests during each task, then `go test ./...`, `go vet ./...`, `gofmt -l .`,
`git diff --check` and `go build -o bin/devkit ./cmd/devkit` for delivery. Use
`DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py` for the authorized
disposable Docker acceptance. Reuse VM lifecycle tests; changes to lifecycle
execution require `DEVKIT_VM_RUNTIME_TEST=1 python3 tests/vm_lifecycle_acceptance.py`
on an authorized disposable host. Do not restart real VMs to validate inventory.
No frontend build is required unless the scope is explicitly expanded to UI changes.

## Review focus and delivery

During task 4, before implementing task 5, verify an early end-to-end fixture: validate a project,
read context, make a supported Dockerfile change, and show the proposed rebuild
without altering resources. Before shipping task 5, exercise the sentinel-preserving
update and interrupted-operation path. These outcomes matter more than test counts.

Review cross-cutting risks: secret-bearing Compose errors, incomplete discovery
granting authority, live/review identity confusion, concurrent share expiry, volume
retention versus app data migration, and claiming VM connection/firewall verification
from insufficient evidence. Each maps to tests above; unresolved failures block
the affected feature rather than weakening its advertised contract.

Commit/push main per repository conventions after authorized implementation. Deploy
only to the explicitly selected host, with existing recovery procedures, then verify
served CLI/dashboard version and install guidance for both clients without overwriting
edits. Claude checks remain pending if access is unavailable. Record actual evidence,
unrun checks and stopped-versus-running outcomes; retain unfinished worktrees.

Current state: planning only. No production source, runtime, host configuration or
installed skill changes are part of this planning commit.

## Primary references

- [Compose config](https://docs.docker.com/reference/cli/docker/compose/config/):
  canonical rendering, interpolation and validation options.
- [Compose up](https://docs.docker.com/reference/cli/docker/compose/up/): container
  recreation can retain mounted volumes; it is not a database rollback guarantee.
- [QEMU image utility](https://www.qemu.org/docs/master/tools/qemu-img.html): offline
  image operations and the limitations of querying images being modified.
