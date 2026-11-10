# Preview secrets quick access — v4

Supersedes [v3](archive/2026-11-10-preview-layout-v3.md). The user wants the overlay
focused on copying secrets, with no scope selection or add/management controls.

Reuse ProjectSecrets with an opt-in quick-access presentation. Show effective,
non-deleted secrets, including inherited entries, with existing reveal/copy controls
and Refresh. Keep the exact preview checkout/worktree context, errors, empty/loading
states and transient-value cleanup. Hide scope selectors, registration, context tutorial,
editor, history, rotate/delete/restore and retirement controls. The normal Secrets page
retains its full interface. No API or permission changes, no automatic registration.

1. Implement the opt-in presentation and update dashboard docs.
2. Verify hidden controls, fixed requests, copying, close cleanup, desktop/mobile bounds,
   and full-page management regressions; build frontend and run existing checks.
3. Commit/push main and deploy with the existing guarded procedure, verifying assets
   and preserved host services.

## Verification checkpoint

Production build, 30 frontend tests, 11 targeted browser regressions and Go suite pass.
Browser tests verify fixed checkout/scope requests, no mutation operations, absence of
management controls, copying without displaying values, reveal cleanup and desktop/mobile
bounds. Existing full-page creation, paired-login and history tests pass. Screenshots
inspected at desktop and phone widths.
