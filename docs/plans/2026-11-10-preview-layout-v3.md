# Preview secrets overlay — v3

Supersedes [v2](archive/2026-11-10-preview-layout-v2.md): the user requested an inline
or overlay secrets list instead of page navigation. All desktop/mobile layout and
preview disclosure behavior from v2 remains required.

Use the existing ProjectSecrets component inside the shared accessible Dialog, opened
by Actions → Secrets. Match the exact unique registered checkout as before, default
to worktree scope including inherited entries, and retain authentication and explicit
reveal/copy controls. Unmount the panel on close, sign-out, or loss of checkout ownership
so transient values and pending responses are cleared. Preserve list filters, expanded
rows and route. Bound the overlay to the viewport with vertical scrolling.

1. Replace the route link with the overlay, reusing the existing panel and dialog.
2. Verify exact checkout requests, unchanged URL/list state, no automatic reveal,
   close/reopen cleanup including delayed responses, and desktop/mobile bounds.
   Run frontend tests/build and Go embed tests; update dashboard documentation.
3. Commit/push main and deploy with the established guarded binary procedure; verify
   served revision/assets and unchanged containers, T3 and vault state.

## Verification checkpoint

Production build, 30 frontend contract tests, 11 targeted browser regressions and Go
suite pass. Browser coverage checks desktop/mobile bounds, exact checkout selection,
unchanged route/filter/disclosure state, no implicit reveal, close/reopen clearing and
late reveal responses. Desktop and mobile screenshots inspected; the portaled dialog
uses a dedicated global class to apply its viewport bounds.
