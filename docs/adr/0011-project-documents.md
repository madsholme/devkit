# 11. Checkout-scoped Markdown documents

Status: accepted, 2026-11-05. Extends the project detail flow and CLI-backed action boundary.

Projects need readable plans/specs and agent guidance, plus a way to create or edit docs.
Expose bounded Markdown files under docs/ and root AGENTS.md/AGENT.md for each exact
registered checkout. Main and linked worktrees retain their own files. Keep root agent
files read-only; authenticated edits are restricted to docs/ and leave Git changes
uncommitted. Discovery does not write files or execute hooks.

Reuse authenticated same-origin jobs with `devkit docs save` and private stdin content.
Use SHA-256 content versions, exclusive creation, pinned directory handles without
symlink traversal, regular single-link files, directory locks and atomic publication.
Revalidate checkout membership before saving; reject stale and observed concurrent
edits. An external editor can still race the final check because it does not share our
lock. No forced overwrite, deletion, rename, arbitrary-path access or auto-commit.

The frontend loads its Markdown parser only when Documents opens. Disable raw HTML,
restrict links and avoid automatic image requests. Keep drafts through failures and
sign-in expiry; guard navigation but do not persist drafts in local storage. Reads use
the dashboard's existing public-on-private-network model. Size and listing bounds keep
this a document browser rather than a general file manager. Go remains standard-library-only.
