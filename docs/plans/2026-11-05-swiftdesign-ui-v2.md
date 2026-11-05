# SwiftDesign dashboard — v2: project workspaces

## Change from v1

The user requested SwiftConfig's host directory/detail flow for projects, including
project information, worktree resource drill-down, and project-wide containers and VMs
with worktree/branch context. Keep the global top navigation unchanged. V1 is preserved
in `archive/2026-11-05-swiftdesign-ui-v1.md`; its completed migration and safety constraints
remain applicable. Continue in the existing codex/swiftdesign-ui worktree.

## Behavior and boundaries

Projects becomes a searchable directory table. Selecting a project opens a bookmarked
project detail route with Back to Projects, identity/path, and local Overview, Worktrees,
Containers and Virtual machines navigation. Overview shows repository/recipe information
and resource summaries. Worktrees lists the main checkout and linked worktrees; selecting
one opens local information, containers and VM tabs without changing global scope.
Resource tables at project level include exact worktree and branch links. Existing Git,
preview, container and VM controls and connection commands remain reachable and retain
the same server-enforced capabilities and confirmation payloads.

Use project paths as encoded route identities, never names or array indices. Browser
back/forward and reload restore the selected project, section and worktree. Unknown or
removed selections show an explanatory state with a return link. Independent API failures
retain prior data and display an error. Preserve the project directory search on return.

Association is presentation only, never authorization. Registered containers use their
existing checkout attachment/worktree field; managed containers use matching preview
service IDs and exact preview worktree paths. Do not group unrelated Compose containers
by name alone. VM project assignment stays server-owned; expose an opaque worktree key
only when the full backend recipe discovery finds a unique claim, using existing
UUID-before-name rules. Checkout rows expose the same opaque key.
Keep commands/control configuration and VM discovery paths private. Reject duplicate VM
identities or multiple checkout claims and keep project-level VMs without an exact worktree explicitly marked as unassigned or
ambiguous. Do not expose private VM discovery paths or infer ownership from process cwd.
Project-name collisions across repositories must not silently duplicate unassigned VMs.

## Implementation and verification

1. Add the opaque worktree-key contract and a pure frontend project-inventory mapper.
   Cover serialization without private paths or control data; test exact container joins, dedup,
   UUID priority, ambiguous VM claims, duplicate project names and unavailable providers.
2. Replace ProjectsView with the directory and add ProjectDetailView plus reusable scoped
   resource tables/VM connections. Update router and page heading behavior only for project
   detail routes. Use existing shared primitives, controls and theme tokens; no dependencies.
3. Extend synthetic fixtures and browser checks for list/detail/back/deep links, project-wide
   branch columns, worktree filtering, ambiguous mappings, missing projects and mobile flow.
   Run frontend tests/build, relevant Go tests and full Go suite, inspect light/dark desktop
   and mobile renders. Regenerate embedded assets and update dashboard/architecture docs.

## Acceptance

The global menu remains global and Projects stays active through local navigation. Project
and worktree inventories are reachable without jumping to global tabs. No incorrect branch
is presented as an ownership fact. Anonymous read access and authenticated actions retain
existing behavior. Existing browser scenarios remain covered with updated directory
expectations. Keep the committed changes on the branch; no merge, push or live deployment.

## Checkpoint

Completed on the existing branch. The directory and both levels of local navigation use
shared SwiftDesign primitives and SwiftConfig's host section pattern. Backend changes are
limited to read-only worktree metadata; lifecycle permissions and action targets are unchanged.

Validation:
- Nine frontend unit tests pass, including six project association cases.
- Eight full browser scenarios pass. A ninth scoped-action scenario verifies Git fetch targets
  the selected checkout, confirmed stop targets the exact container ID, and VM start targets
  its server control ID. All operations use the synthetic server.
- Frontend build/typecheck, full Go suite, vet, Go binary build, gofmt and diff checks pass.
- Captured the SwiftConfig host overview and Docker references with its existing synthetic
  harness (two passing reference checks). Compared project directory/detail/table layouts in
  RN Light and RN at 1440px, and project/worktree detail at 390px. No page overflow or browser
  errors in the captures. Screenshots use only invented project data.

The browser run caught and fixed lost directory search after detail reload and discovery
errors announced as status instead of alerts. A later action-test selector was corrected to
use the existing VM button label, Start; no product behavior changed for that correction.

Preserved the global menu, resource monitor, auth/confirmation handling and global inventories.
Project detail adds SwiftConfig's back-link/title/local-tab hierarchy and resource grouping.
VMs without unique worktree ownership remain project-level, or global when project grouping
itself is ambiguous. Nothing was deployed or exercised against real host runtimes. V1 evidence
is retained in the archive; this document is the authoritative current plan.
