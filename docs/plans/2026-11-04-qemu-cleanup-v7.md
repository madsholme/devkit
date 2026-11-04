# QEMU cleanup — implementation plan, version 7

Supersedes [v6](archive/2026-11-04-devbox-vm-visibility-v6.md) as the current work plan.
V6 is complete; its host repair and evidence remain intact. Work as one agent in the
existing codex/projects-qemu worktree. User requested a cleanup command by exact VM
name or all VMs in a project and chose: preserve disks, optional --purge.

## Behavior and constraints

- Add CLI `devkit cleanup --vm NAME [--purge] [--yes]` and
  `devkit cleanup --project SLUG [--purge] [--yes]`. Exactly one selector is required.
  Dry run by default lists every target, planned stop/unregister/purge and any blocker.
  `--yes` executes. Docker commands and dashboard mutation API remain unchanged.
- Names resolve in the current recipe when present, otherwise must be globally unique;
  UUID/control ID selection may reuse the existing identity convention. Project selects
  all explicitly registered standalone QEMU guests across discovered recipes with that
  exact project slug, including stopped registrations. Never infer ownership by prefix,
  cwd, disk location or an arbitrary PID.
- Ordinary cleanup gracefully stops a running guest through its configured stop adapter,
  verifies absence, and removes only its registration from devkit.json/devbox.json.
  Retain disks, firmware, snapshots, logs and launcher files. A stopped plain mapping can
  be removed. A running plain mapping without stop authority blocks cleanup.
- Purge requires an explicit nonempty `control.purge` shell command in each selected
  standalone definition. Run it only after stop is verified, with the same project
  environment and bounded launcher execution as existing controls. Trusted adapter owns
  its exact guest's disposable resources; devkit never infers deletable paths from QEMU.
  Control continues requiring start and stop; libvirt controls cannot supply purge.
- Libvirt-controlled/observed guests are unsupported for this command; fail clearly and
  retain registration. No virsh destroy, undefine or automatic storage removal.
- Preflight the entire selected batch before any mutation: reject missing/ambiguous
  identity, conflicting recipes, incomplete QEMU inventory/configured project discovery,
  unreadable or symlink recipes, missing required stop/purge capability. List blockers.
  No partial batch starts when preflight fails.
- Use the existing host-wide lifecycle lock for apply. Revalidate selected recipe bytes
  and fresh identity/state before each step; abort if either changes. After hooks verify
  the selected VM remains absent. Stop or purge failure retains registration and halts
  the remaining batch, reporting completed targets; no forced shutdown or rollback of
  successful guest operations. Record successful stop/purge/cleanup events separately so
  partial success is visible, and command failures through dispatch's existing history.
- Preserve unknown JSON fields/numbers, unrelated VM entries and recipe mode; replace
  via temporary file and atomic rename. Reject observed concurrent recipe changes rather
  than overwrite them. The lifecycle lock coordinates devkit, not arbitrary external
  editors or VM launchers; avoid claims of a transaction across those processes.
- No deployment, modification of live PlayWarden recipes, cleanup of real user VMs,
  automatic commit/push/merge, or new dashboard cleanup controls in this assignment.

## Tasks

1. Specify and record: archive v6 and its task record, retain old requirements, record ADR
   for explicit cleanup/purge authority. Verify links and existing branch/worktree.
2. Implement with regression tests: add vm_cleanup.go/tests; extend vmControl validation
   and CLI parsing. Test real recipe preservation/removal, dry-run and confirmation,
   exact name/project selection, unknown/duplicate/conflicting targets, mixed batch
   blockers, stopped plain mappings, missing stop/purge authority, failed stop/purge,
   concurrent recipe/identity changes, lock exclusion, history and metadata privacy.
   Fail tests before implementation, then targeted and full Go checks.
3. Verify disposable flows and docs: extend existing QEMU lifecycle runner using its
   isolated fake providers and temporary disks to cover name/project cleanup, retained
   disks, explicit purge, CLI inventory disappearing and partial failures. Run only
   disposable TCG guests; inspect existing fixture cleanup before extending.
   Update README command table, projects/dashboard/testing docs and AGENTS.
4. Single-agent review/handoff: assess destructive boundaries and partial results,
   run full Go tests/vet, gofmt/diff checks, build and isolated runtime check. Reconcile
   plan/ADR/docs with actual results. Report branch/worktree and uncommitted changes.

## Acceptance

- Named cleanup removes exactly the selected registration after a verified stop and
  retains disk bytes without --purge.
- Project cleanup handles all safe registered project targets; any preflight blocker
  prevents all mutations. Mid-run failure leaves failed/unattempted registrations and
  reports already completed work accurately.
- Purge runs only an explicitly configured trusted purge adapter after verified stop;
  absent/failing adapter cannot remove the registration.
- Dry run never invokes hooks, stops guests or writes project recipes.
- Existing lifecycle and Docker behavior stays covered and passing; real user VMs
  remain untouched. A disposable real QEMU flow demonstrates cleanup outcomes.

## Completion and verification

All four tasks are complete, implemented and reviewed by one agent in the original
codex/projects-qemu worktree. At implementation handoff, changes were uncommitted and no deployment or live
project configuration/lifecycle action had been performed.

- Failing tests first demonstrated the absent cleanup command and missing purge
  validation. Focused regressions now cover named/project selection, dry runs,
  all-target preflight, disk retention, purge ordering/failure, stopped mappings,
  exact ownership, legacy/unknown JSON preservation, lock exclusion, changed recipe/
  definition/process identity, respawning guests, timeouts and partial progress.
- Single-agent safety review found stale definition acceptance and a bypass of the
  existing 128-control catalog limit. Regression tests failed first, then passed after
  focused fixes. Atomic recipe replacement has no fallible bookkeeping after rename.
- Final `go test ./...`, `go vet ./...`, `gofmt` and `git diff --check` pass.
  A final binary was built at /tmp/devkit-vm-cleanup; the checkout/live binary was not replaced.
- The existing disposable TCG acceptance runner passes with added name/project cleanup,
  read-only dry runs, retained disk hashes, explicit purge, failed second purge and
  retry of only the remaining registration. Existing lifecycle and authenticated
  HTTP/cache checks also pass. Runtime evidence is reused across the final catalog-
  limit-only guard, whose boundary was separately covered in the final Go suite.
- Documentation, ADR 0006 and agent notes match the implemented CLI-only contract.
  Existing dashboard actions remain unchanged. Libvirt cleanup is intentionally unsupported.
- Temporary source-independent logs: /tmp/devkit-cleanup-suite.log and
  /tmp/devkit-cleanup-runtime.log. No real user VM, disk, recipe, or service was changed.

Next useful action: configure exact graceful-stop adapters (and optional guest-specific
purge adapters) for read-only restored VM mappings before using cleanup on those guests.
The command deliberately refuses to invent that authority.

## Authorized release follow-up — 2026-11-04

The user authorized committing on codex/projects-qemu, deploying the committed build
to the existing host, updating the upstream PR, then merging into local main and
pushing main to the user's fork. Preserve the feature branch/worktree for the open PR.
Back up the installed binary, retain the corrected group wrapper and project mappings,
and verify exact deployed revision/checksum, service health, VM visibility and cleanup
dry-run guards without applying cleanup to real guests. Private host receipts record
deployment results; Git refs and the upstream PR record integration.
