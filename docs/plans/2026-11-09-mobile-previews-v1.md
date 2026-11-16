# Mobile previews — v1

Goal: operate previews on a phone without horizontal scrolling; preserve the existing desktop table. Based on deployed a5d8261, with the existing SwiftDesign workspace conventions.

At widths below 768px, stack each preview's identity, status, uptime, source and actions within the available width. Keep project grouping and filter controls. Expand services into vertically arranged labeled details with accessible inspect/log buttons and wrapping paths/images/routes. Give mobile buttons 44px touch targets. Reuse existing controls and handlers, authentication visibility and destructive-action confirmation; no new API, dependencies or backend behavior.

Tasks: add synthetic mobile/desktop acceptance checks; implement scoped responsive styles and descriptive cell labels; rebuild embedded assets, inspect phone/desktop rendering, update dashboard docs.

Acceptance: no internal table or page overflow at 320, 390 and 767px, including long metadata and expanded services. Actions are visible without horizontal scrolling and preserve exact target/cancel behavior. Desktop retains table headers, columns and controls. Anonymous visitors retain read-only access. Filters and empty state work. Production build and focused browser checks pass.

Verification: production build/type-check passed; six existing dashboard browser scenarios and two new mobile/desktop scenarios passed. The final mobile checks cover 320, 390 and 767px with long metadata, internal overflow, 44px action targets, expanded service details, destructive-action cancellation, filters and anonymous access. Desktop and mobile screenshots inspected; desktop remains a table. Impeccable detector and diff whitespace checks are clean. Embedded assets rebuilt. Existing large-chunk build advisory remains; Go tests were not rerun for this frontend-only change. Worktree /tmp/devkit-mobile-previews, branch codex/mobile-previews. User authorized commit to this branch and Devbox deployment on 2026-11-09. Deploy the committed binary with a guarded prior-binary rollback, restarting only devkit-web. Verify exact executable and served asset hashes, HTTPS and vault health; preserve T3, containers, configuration and secret registry. The host deployment receipt and STATUS.md record the result. No merge to main requested; retain this task branch/worktree for integration. Next useful check: refresh Previews on a phone and open its Actions menu.

## Integration — 2026-11-16

The worktree cleanup audit found this historical branch outside main ancestry. Its
mobile implementation and unchanged acceptance tests were already present in main,
alongside newer preview details/secrets behavior and rebuilt assets. Preserve those
newer files, retain this original plan and record the branch history as integrated.
The former pending-merge/worktree instructions above describe the original handoff;
they are superseded by this integration. No runtime behavior changes in this merge.
