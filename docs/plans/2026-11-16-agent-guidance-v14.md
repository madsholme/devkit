# Project onboarding and external secrets guidance — v14

Supersedes [v13](archive/2026-11-14-agent-guidance-v13.md). The user authorized
`devkit-projects`, expanded secrets guidance, and practical Codex/Claude scenario
checks. Isolated Codex sessions are authorized; Claude access is unavailable, so
its live checks remain pending. Preserve the existing preview, troubleshooting,
VM, credential and installation protections.

## Scope and acceptance

- Add a focused project skill for new-project creation, adapting an existing app's
  recipe/live/review files, and registering an existing Compose stack without
  recreation or shared database writers. Preserve Git ownership and user files.
- Extend secrets discovery and guidance for choosing Devkit versus native OpenBao,
  least-privilege context-bound tokens, backend-only client use, expiry/revocation,
  stale writes, uncertain results and sealed-vault failures.
- Use the existing single-file skill catalog and installer for both agents. Update
  managed blocks, templates and docs; preserve edited assets, modes and safe aliases.
- Verify distribution/upgrade/preservation in disposable homes and review realistic
  scenarios using the installed guidance. Record actual Codex results separately
  from CLI contract tests and pending Claude execution; do not claim model tests
  from text matching. No real credentials, public exposure or workload mutations.

## Implementation

1. Establish baseline scenario gaps and failing distribution coverage; reuse
   existing project, Compose-registration, API-token and authorization tests.
2. Add the project asset/catalog/block, extend secrets and bump its managed version,
   synchronize templates, and update current documentation.
3. Run Codex decision scenarios with synthetic inputs and read-only execution;
   run both installation profiles, Go tests/vet, skill validation and a binary build.
   Record evidence and Claude's access limitation in the scenario documentation.
4. Integrate verified changes into main and push under repository conventions.
   Build the binary, install the bundled skills for both clients while preserving
   edits, and remove the clean task worktree. This workstation has no installed
   Devkit binary or devkit-web user service, so no running dashboard is restarted;
   remote host deployment is outside this task.

No reference-directory installer expansion, host-maintenance skill, frontend change,
new runtime API or VM restart is needed. `docs/agent-scenarios.md`
provides reusable prompts, acceptance criteria and the actual observed results.

## Checkpoint

Baseline existing agent/catalog tests pass. The default Go cache was read-only in
the sandbox; subsequent commands use a task cache under /tmp.

The new project distribution tests failed on missing assets/blocks before catalog
implementation. Both skills, catalog/version, managed blocks, templates and current
docs are updated. Full Go tests/vet, a trial build, skill validation and all-five
asset checks pass. Fresh Codex 0.167.0 baseline/candidate runs establish the missing
reference contracts and passing decisions in all six cases; see
[scenario evidence](../agent-scenarios.md) for boundaries and pending Claude coverage.
The new skill remains a single file; no installer architecture change was needed.

Local main and fork/main both started at d2e6cdf. Origin is the upstream repository
and is behind the user's development fork; integration targets fork/main without
changing tracking configuration. Local installation now reports both Codex and Claude
current. No dashboard service is installed here. Implementation and verification are
complete; the delivery commit/main history records integration.
