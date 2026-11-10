# Mobile previews — v1

Goal: operate previews on a phone without horizontal scrolling; preserve the existing desktop table. Based on deployed a5d8261, with the existing SwiftDesign workspace conventions.

At widths below 768px, stack each preview's identity, status, uptime, source and actions within the available width. Keep project grouping and filter controls. Expand services into vertically arranged labeled details with accessible inspect/log buttons and wrapping paths/images/routes. Give mobile buttons 44px touch targets. Reuse existing controls and handlers, authentication visibility and destructive-action confirmation; no new API, dependencies or backend behavior.

Tasks: add synthetic mobile/desktop acceptance checks; implement scoped responsive styles and descriptive cell labels; rebuild embedded assets, inspect phone/desktop rendering, update dashboard docs.

Acceptance: no internal table or page overflow at 320, 390 and 767px, including long metadata and expanded services. Actions are visible without horizontal scrolling and preserve exact target/cancel behavior. Desktop retains table headers, columns and controls. Anonymous visitors retain read-only access. Filters and empty state work. Production build and focused browser checks pass.

Verification: production build/type-check passed; six existing dashboard browser scenarios and two new mobile/desktop scenarios passed. The final mobile checks cover 320, 390 and 767px with long metadata, internal overflow, 44px action targets, expanded service details, destructive-action cancellation, filters and anonymous access. Desktop and mobile screenshots inspected; desktop remains a table. Impeccable detector and diff whitespace checks are clean. Embedded assets rebuilt. Existing large-chunk build advisory remains; Go tests were not rerun for this frontend-only change. Worktree /tmp/devkit-mobile-previews, branch codex/mobile-previews. User authorized commit to this branch and Devbox deployment on 2026-11-09. Deploy the committed binary with a guarded prior-binary rollback, restarting only devkit-web. Verify exact executable and served asset hashes, HTTPS and vault health; preserve T3, containers, configuration and secret registry. The host deployment receipt and STATUS.md record the result. No merge to main requested; retain this task branch/worktree for integration. Next useful check: refresh Previews on a phone and open its Actions menu.


## Integration correction — 2026-11-10

The earlier branch-only deployment was replaced by main builds without this change.
The original mobile tests reproduce the regression on main: internal preview table
overflow at phone widths. Restore the original view, tests and dashboard guidance
unchanged onto current main, retaining GitHub project creation and current agent guidance.
This supersedes the earlier no-merge handoff; accepted behavior and scope are unchanged.
Rebuild production assets, run the original mobile/desktop tests and GitHub creation
checks, push to madsholme/devkit and deploy the combined binary with the existing
guarded backup procedure. Preserve T3, containers, vault and host configuration.

Restoration verification: the two original mobile tests failed on the old main build
with internal table overflow, then passed after restoring the original source. Current
Go suite, all 30 frontend tests, production typecheck/build and all 26 browser scenarios
passed, including mobile previews, desktop behavior and GitHub creation. Phone/desktop
screenshots inspected. Deployment will verify exact served HTML/JS/CSS hashes in addition
to binary identity and health, so the tested mobile assets are proven to be served.
