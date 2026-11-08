# SwiftDesign dashboard — v1

## Design and scope

Replace the embedded vanilla dashboard with Vue 3 using real SwiftDesign components.
SwiftConfigV2's shared AppShell, page headings, operator tables and theme system are
visual authority. Keep devkit branding and all six inventory views. The user approved
implementation including a frontend build step and preservation of the single Go binary.

The Go CLI stays standard-library-only. Authentication, target ownership, capabilities,
confirmation tokens, discovery, secret masking and CLI-backed jobs remain server-owned.
No live host mutation or deployment is needed to verify this branch.

Use the shared shell/navigation, buttons, inputs, badges, dialogs, dropdowns, selects,
progress and tables. Separate data loading/action logic from Vue view components.
Keep public reads, sign-in/out, preview lifecycle/shares/cleanup, checkout launch/review,
Git controls, container and VM controls, private connection commands, details, bounded
streaming logs, job output, pairing, system monitoring and history. Keep existing hash
links and remembered filters. Independent inventory failures retain prior results with
visible errors; in-flight requests cannot overwrite newer selections. Dialog close
aborts streams, clears one-time pairing data and restores focus. Never auto-retry mutations.

Package snapshots of @madsholme/ui and themes are pinned locally with provenance so
builds do not depend on a developer's absolute checkout. Generated assets are committed
for direct Go builds and bundles; frontend changes must regenerate them. Serve only
embedded production assets with correct MIME/cache policy, never source/package files.

## Ordered implementation

1. Inspect dashboard/API behavior and SwiftConfig/SwiftDesign integration. Record the
   migration decision and parity criteria (this document and ADR-0009).
2. Add frontend package/lock/build configuration, shared theme and shell, reusable data
   and action controls, six views and supporting dialogs. Embed the generated assets.
3. Verify Go tests/vet/build and frontend type/build checks; test auth and confirmation
   payloads, capability visibility, stale/error states, route compatibility, stream
   cleanup and core UI flows against synthetic API fixtures. Inspect desktop/mobile and
   light/dark theme rendering against SwiftConfig's layout. Update dashboard/build docs.

## Acceptance

All existing authorized workflows remain reachable with the original API semantics.
SwiftDesign owns actual rendered components (no legacy HTML iframe or CSS imitation).
No runtime Node/CDN or sibling checkout dependency. No new Go dependencies. Responsive
navigation and tables, keyboard dialogs and theme persistence work. Go build from a clean
checkout embeds the frontend. Keep changes on codex/swiftdesign-ui for review.

## Checkpoint

- Branch/worktree created from local main at 10d0784.
- References inspected: SwiftConfigV2/frontend shared AppShell, PageHeader and DashboardView;
  SwiftDesign local package APIs and template integration.
- All three implementation steps completed. Actual shared Vue primitives render the six
  views, dialogs and controls; compiled assets are embedded with no runtime Node dependency.
- Frontend build/typecheck, three boundary tests and five Chromium browser scenarios pass.
  Browser coverage includes six routes, legacy hashes, filters, auth expiry without replay,
  exact confirmation targets, independent failures, logs, pairing and system metrics.
- `go test ./...`, `go vet ./...`, Go binary build, `gofmt` and `git diff --check` pass.
  Go tests use an isolated /tmp cache. Asset tests check entry references, MIME/cache headers
  and absence of source/package routes. Existing backend safety tests remain unchanged.
- Single-agent review inspected frontend action/stream state and the asset-serving diff.
  No backend lifecycle, authorization, discovery or ownership behavior changed.
- Synthetic screenshots replaced the README/dashboard images; no real host inventory or
  credentials appear. Local fixture preview is independent of the deployed dashboard.

## Visual comparison and limits

Compared the rendered SwiftConfigV2 dashboard/hosts at 1440px in rn-light and rn with
this dashboard, plus devkit's 390px mobile layout. Four reference capture checks passed.

| Reference trait | Delivered result |
| --- | --- |
| Shared top navigation | SwiftDesign desktop/mobile navigation with devkit's six views |
| Typography and page headings | Shared font tokens, icon-led 30px desktop headings |
| Light/dark palette | Actual rn-light/rn tokens and the shared persistent theme controller |
| Tables and elevation | Shared table primitives, muted headers, borders and raised surfaces |
| Controls and icons | Shared buttons, selects, dialogs and menus with Lucide icons |
| Responsive layout | Collapsed mobile navigation, wrapping filters, internally scrolling tables |

Screenshot review corrected search-icon padding and app utility-layer priority so desktop
filters remain compact. Browser inspection found no page-level mobile overflow or console
errors; keyboard skip navigation focuses the main region without changing the route.
Intentional differences are devkit branding, host/resource summary, project grouping and
operator actions. SwiftConfig's product data and administration workflows are not copied.

No live Docker, VM, Git-control or public-share mutation was exercised. Those controls were
verified with synthetic browser fixtures and the existing Go suite. Nothing was deployed,
restarted, merged or pushed. Keep the dedicated branch/worktree for review.
