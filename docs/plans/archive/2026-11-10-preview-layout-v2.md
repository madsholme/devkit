# Preview layout and navigation — v2

Extends the accepted mobile-preview behavior in [archived v1](2026-11-09-mobile-previews-v1.md).
The user requested a desktop overflow fix, preview-name clicks to open the same
information as the disclosure arrow, and a Secrets shortcut under Actions. Existing
work is committed on main before these changes. Reuse the current task worktree.

## Behavior

Keep the desktop table within the viewport even with long preview names, checkout
paths, branch names, health summaries and expanded service metadata. Constrain column
widths and wrap metadata rather than hide content or make actions inaccessible. Preserve
mobile stacked rows and 44px targets. Preview names toggle the same inline details as
the arrow, including keyboard activation and synchronized expanded state. The explicit
external-open icon continues to open the running preview in another tab.

Signed-in Actions menus include Secrets, routed through the existing project/worktree
Secrets view for the exact registered checkout. Never infer ownership from a project or
branch name. Missing/ambiguous checkout associations disable the shortcut. Existing
secret authentication, scope selection and explicit reveal remain unchanged; navigating
does not reveal secret values or write anything. No new API or dependency.

## Implementation and verification

1. Reproduce screenshot-like desktop overflow using multiple non-web services and long
   metadata. Add expansion and exact-checkout Secrets navigation tests, including missing
   and ambiguous associations. Preserve existing mobile regression tests.
2. Update only the preview view and related dashboard documentation; rebuild embedded
   assets. Check desktop widths 768/1024/1440/1674 and existing mobile widths 320/390/767.
   Inspect screenshots; run frontend contracts/build/browser suite and Go embed checks.
3. Commit and push main to madsholme/devkit. Deploy the verified combined binary using
   the existing guarded backup/restart procedure; verify served assets and preserve
   T3, containers, vault/configuration and the GitHub creation feature.

## Verification checkpoint

Implemented with regenerated embedded assets. All 30 frontend contract tests and 30
Playwright tests pass, including desktop overflow, keyboard disclosure, exact-checkout
Secrets routing, unavailable associations and existing mobile behavior. Frontend build,
`go test ./...` and `git diff --check` pass. Inspected collapsed/expanded desktop screenshots.
