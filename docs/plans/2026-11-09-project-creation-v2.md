# Project creation — v2

## Scope and changes from v1

The user requested optional GitHub repository creation with owner, repository name,
private visibility by default, and optional initial push. This extends the existing
creation flow; the original local-only and explicit remote URL modes remain intact.
Archived v1 preserves the previous hosted-repository exclusion. No general Git lifecycle
or discovery authority changes.

## Behavior and boundaries

CLI accepts --github-owner OWNER --github-repo NAME, optional --github-public and
--github-push. Both owner and name are required and validated, mutually exclusive
with --remote. Dashboard exposes equivalent inputs; push starts unchecked.
Use the host integrations.github.token secret source and GitHub REST API, with no
new dependencies. Support github.com personal accounts and organizations. Verify
identity/owner type and credentials before local reservation; GitHub enforces owner
permissions. Never adopt an existing remote repository. Create an empty repository
only after local initialization succeeds, then set HTTPS origin. Push only the initial
main commit when explicitly selected, set its origin upstream, never force push.

Keep credentials out of arguments, persisted Git configuration, output and history.
Use bounded requests/commands, refuse redirects, sanitize external errors, and preserve
Git environment isolation. Local identity and pinned-path checks remain mandatory.
Retain local and remote resources after partial failures. Never automatically retry
repository creation after an uncertain response; report the exact intended repository
for inspection. Push failures retain origin and give a manual retry command.

## Implementation and verification

1. Extend CLI/action options and validation; test missing credentials and invalid combinations.
2. Add bounded API creation and isolated initial push; test user/org routing, privacy,
   response identity, API/push failures, no accidental push, secret handling and upstream.
3. Extend existing form with shared controls; test defaults and action payloads, retained
   input after validation failure, desktop/mobile rendering. Update docs and embedded assets.
4. Run Go tests/vet, frontend tests/build and focused Playwright creation tests. Integrate
   into main and push under repository conventions. No live GitHub test repositories or
   service deployment are implied by this feature implementation.

## Checkpoint

Implemented and reviewed on codex/github-project-create. CLI/dashboard support owner,
repository name, private default, explicit public visibility and optional initial push.
The dashboard defaults an omitted repository name to the project name. No new dependencies.

Verified: full Go suite and vet; project creation/GitHub tests under the race detector;
30 frontend contract tests; frontend typecheck/build; existing creation flow and new
GitHub flow in Playwright at desktop/mobile widths, with screenshots inspected. API
fixtures cover personal/org endpoints, privacy, response mismatch, API failure, retained
local state, push failure and token redaction. A disposable bare Git remote verifies the
initial commit and origin/main upstream. No live GitHub repository was created for testing.
Build retains the existing large-chunk warning. The host has no devkit-web user unit;
there is no running dashboard service here to restart.

Integration follows the repository's main-and-push convention. Task worktree is removed
after successful integration; the original checkout and unrelated worktrees are preserved.
