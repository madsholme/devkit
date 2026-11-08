# Project documents — v1

## Behavior and scope

Add Documents to each project's existing local navigation. Select the main checkout or
an exact linked worktree; show nested Markdown files under docs/ and root AGENTS.md or
AGENT.md (case-insensitive names). Render headings, lists, tables, links and fenced code
for easy plan/spec reading. Agent files are read-only. Signed-in users can create docs
and edit existing Markdown under docs/. Save ordinary working-tree files, never commit,
execute hooks, delete documents or alter Git history. Keep global navigation unchanged.

Use a file sidebar, readable document pane, source editor and preview. Show checkout,
path, dirty editor state and errors. Preserve unsaved text on errors, authentication
expiry or save conflicts; guard checkout/file/navigation changes while dirty. New docs
accept a relative docs/ path including nested folders. Empty docs directories are useful
states, not errors. Relative Markdown links navigate within this document browser.

## Boundaries

Resolve exact registered checkouts at both request and CLI execution. Reads follow the
existing public dashboard model; creation/editing require same-origin authentication.
Restrict paths to non-hidden Markdown under docs/ or the allowlisted root agent names.
Reject traversal, symlinks, hardlinks, special files and arbitrary host paths. Bound
individual UTF-8 documents to 256 KiB, directory depth to 16, and listings to 1000 files
and 5000 visited entries. Report skipped/limited files. Do not return other source files.

CLI: `devkit docs list`, `devkit docs read PATH` (JSON with version), and
`devkit docs save PATH --expected VERSION|new --file FILE|-`, operating on the current
registered checkout. Dashboard saves call that CLI with content on stdin, not in logged
arguments. Hash/version checks reject stale edits; new files must not already exist.
Serialize devkit saves with a directory lock, write a sibling temporary file and publish
atomically, preserve permissions, and recheck observed external changes before publication.
External editors do not share the lock, so changes after the final check remain a small
race; no forced-overwrite option. Temporary files are cleaned up, never existing files.

Render Markdown with a dedicated frontend parser, raw HTML disabled, no remote image
loads and only safe links. Keep Go standard-library-only. No rich text editor, diagrams,
uploads, arbitrary asset serving, rename/delete or agent-file editing in this iteration.

## Implementation and verification

1. Add project_documents.go with bounded filesystem access, listing/read/save, CLI parsing,
   version checks and authenticated action planning. Cover discovery, nested docs, agent
   aliases/read-only, bounds, traversal/symlink/hardlink rejection, stale saves, duplicates,
   concurrency, mode preservation and content excluded from logs.
2. Add Documents to project navigation and a checkout-aware reader/editor using SwiftDesign
   controls. Add Markdown rendering/security unit tests, synthetic fixtures and browser
   coverage for reading, checkout changes, create/edit/preview, conflict retention and
   unsaved navigation protection. Keep current route/deep-link conventions.
3. Exercise real authenticated HTTP → CLI saves with temporary fixtures, run Go/frontend
   checks, inspect desktop/mobile rendering, update project/dashboard/CLI documentation
   and an ADR, rebuild embedded assets, and commit on codex/swiftdesign-ui. Keep branch
   unmerged and live server unchanged.

## Checkpoint

Implemented in the existing redesign worktree on codex/swiftdesign-ui. Documents includes
checkout selection, filtered nested paths, readable Markdown and outlines, root agent
viewing, creation, editing, preview, conflict retention and unsaved-navigation guards.
The Markdown reader is loaded on demand. No scope deviations; live server unchanged.

Verification passed: full Go suite and vet; focused document tests with race detection,
followed by the added listing-bound/special-file checks and final-save ordering check;
frontend typecheck/build and 12 unit/contract tests; all 12 Playwright scenarios. Real
HTTP → CLI acceptance verifies an 84 KiB plan, creation/read/edit, stale rejection,
private stdin/history and unchanged Git HEAD. Desktop/mobile reader and editor screenshots
inspected. Generated embedded assets rebuilt. Review with the synthetic branch preview;
its actions are demonstrations and do not modify real host projects.
