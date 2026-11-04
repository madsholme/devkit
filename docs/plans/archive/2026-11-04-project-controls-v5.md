# Project Git controls and container inventory — version 5

Supersedes [v4](2026-11-04-projects-v4.md). All previous VM, project discovery,
launch, authentication, identity and retention requirements remain. Continue as one
agent in the existing codex/projects-qemu worktree. This extends the combined branch;
it does not deploy, merge or change any user repository through the new controls.

## Behavior and decisions

- Add explicit authenticated Fetch origin, Sync from origin and Push to origin actions
  to each discovered Git checkout. CLI first: devkit git fetch|sync|push. Fetch updates
  origin tracking refs without changing working files. Sync fetches then fast-forwards
  the current branch to its configured origin upstream. Push uses that exact branch
  and upstream destination, never force or implicit push.default behavior.
- Sync/push require an existing origin upstream, clean checkout (including untracked
  files/submodules), attached HEAD, and no merge/rebase/cherry-pick/revert in progress.
  Refuse divergence; never stash, reset, rebase, commit or create branches. Missing
  upstream/configuration displays an actionable reason. Fetch is allowed when dirty.
- Revalidate checkout membership and Git state when executing dashboard jobs. Serialize
  devkit Git operations per shared repository; bound commands and disable interactive
  prompts. Recheck branch/HEAD/clean state after network fetch. Preserve Git's own locks.
  Docker outages must not disable independently registered Git projects.
- Add GET /api/containers and a Containers tab for all running and stopped host Docker
  containers. Expose only names, IDs, status, image, project/preview/service and ports.
  No environment, commands, mounts or raw labels. Existing managed previews retain
  Details/Logs links; unrelated containers have read-only inventory, no new lifecycle API.
- Use consistent authored SVG Git, VM and container icons beside text in tabs and
  project rows. Retain headless/display distinction. Add container filtering, count,
  refresh, loading/empty/error states and narrow-screen layout. Reuse vanilla JS/CSS.

## Implementation tasks

1. Git backend: git_control.go + CLI/action/catalog wiring; real local bare-origin
   tests for fetch/fast-forward/push, divergence, dirty checkout, detached/missing or
   non-origin upstream, in-progress operations, concurrent lock and changed checkout.
   Assert arbitrary paths and anonymous actions are rejected; Docker not required.
2. Containers backend: bounded Docker probe and allowlisted API; tests cover stopped
   health state, managed/unmanaged metadata, secret omission and Docker errors.
3. Dashboard: resource icons, Git controls/reasons, Containers tab and post-job refresh.
   Verify auth gating, action payloads, filtering, empty/errors, desktop/mobile render.
4. Docs and verification: update CLI/README/project/dashboard/testing and agent notes;
   record ADR for opt-in Git mutations. Run full Go tests/vet/format/diff, representative
   CLI integration against disposable local repositories, and browser checks.

## Validation status

All four implementation tasks are complete. No user repository was synced or pushed
by the new code, and this change has not been deployed.

- Full `go test ./...`, `go vet ./...`, build, gofmt and diff checks pass.
- `DEVKIT_GIT_BINARY=/tmp/devkit-git-controls python3 tests/git_runtime_acceptance.py`
  passes against temporary bare origins: authenticated HTTP jobs execute the actual
  CLI for fetch, fast-forward and separate push; anonymous requests and dirty sync
  are rejected; local edits remain and success/failure history is recorded.
- Backend regressions cover repository locks, pre-push hook refusal, changed branch
  or dirty state during fetch, exact destination with differently named tracking
  branch, mirror/followTags configuration, and ignored-file preservation. Review
  reproduced Git's default ignored-file overwrite, then verified the explicit
  --no-overwrite-ignore guard. Container coverage includes stopped/paused health,
  IPv6 bindings, unmanaged identity, metadata allowlisting and Docker failure.
- Headless Firefox synthetic fixtures pass: signed-in Git buttons, dirty checkout
  fetch-only capability, separate action payloads, sign-out hiding controls, resource
  SVG icons, Containers deep link/count, text/status filters, managed Details/Logs,
  empty/error/recovery and no JS errors. Inspected 1280x914 and 500x758 captures;
  neither viewport has document overflow. Fixtures/screenshots are outside the repo.
  The UI detector reports five existing generic panel-padding warnings; rendered
  project content and container rows have explicit padding and were inspected.
- Single-agent review preserved Git hooks, disabled implicit extra branch/tag/mirror
  pushes, and used submodule push checks without pushing submodules. Existing QEMU
  and Docker lifecycle paths are unchanged; their runtime suites were not repeated.

The next useful validation is deploying this combined build to the dev server and
checking its registered projects' Git capabilities and container inventory. The local
branch/worktree is retained for that test and later PR integration.
