# Secrets UI refinement — v1

Scope: refine the existing OpenBao feature UI without changing storage, inheritance, or deployment. Builds on OpenBao v8; its retained rollout work remains unchanged.

Requested behavior: inset content with clear scope/form/list sections; worktree and branch dropdowns; per-secret history in an accessible modal; explain why worktree secrets are absent from the parent branch. Preserve existing themes, protected values, CAS and mutation confirmations. No automatic movement of secrets.

Implementation: reuse ProjectSecrets and SwiftDesign Dialog. Keep a worktree selector even with one checkout, use registered branch IDs, show scope inheritance guidance, and render only typed history version metadata. Dismissal/context changes cancel late history responses. Match existing dashboard spacing; keep mobile layout usable.

Acceptance: selectors send the intended scope/root/branch; modal shows newest versions first, handles loading/error/empty states, closes by Escape/button and ignores stale responses; existing reveal/copy/create tests pass; desktop/mobile content has comfortable insets and no page overflow; production assets regenerated.

Verification: complete locally. All 30 frontend unit tests and four focused Playwright secrets tests pass. Production build/type-check passes (existing large-chunk advisory remains); embedded assets regenerated. Desktop and 390px mobile screenshots inspected, including paired fields and the history modal. Panel/dialog internal-overflow assertions cover clipping hidden by the outer surface. The Impeccable detector reported no findings. Diff whitespace check passes.

Additional corrected boundary: choosing a different branch scope and returning to Worktree must show the checkout's actual branch, not the prior branch selection; regression covered. No backend/storage changes or host mutations. Existing secrets are untouched. User authorized commit and Devbox update on 2026-11-09. Commit on codex/openbao-phase0 and deploy its binary with a prior-binary rollback copy; preserve T3, container start identities, configuration and secret registry. Verify the running executable and exact served JS/CSS hashes. No merge requested. The host deployment receipt and STATUS.md record the resulting release and verification. Next useful check: refresh the live Secrets tab and review selectors and history with real project metadata.
