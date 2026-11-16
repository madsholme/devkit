---
name: devkit-previews
description: Use when developing, testing, reviewing, starting, stopping, rebuilding, sharing or cleaning up an application's devkit preview, including devkit live, exec, review and preview lifecycle commands.
---

# Devkit previews

Operate on the intended checkout and preview. Preserve its data and report the
actual runtime, URL and commit that were verified.

## Establish the target

Read the project's AGENTS.md/CLAUDE.md and recipe (`devkit.json`, or legacy
`devbox.json`). Existing project-specific commands and user constraints still apply.
Before changing a runtime, check:

```sh
git rev-parse --show-toplevel
git status --short
devkit context --json
devkit ls --json
devkit history -n 10
```

Match the checkout, preview identity and mode. Run lifecycle commands inside the
owning repository. When multiple previews match, select the explicit intended ID;
do not guess or operate on another worktree's environment. Git and the user's agent
tools own worktrees and commits; devkit does not create/delete worktrees or authorize
commits, merges or pushes in existing projects.

## Develop and verify

- Use `devkit live` to start/resume the current checkout's preview when development
  work authorizes it. Honor instructions to leave existing runtimes unchanged.
- Run project tests and tools through `devkit exec <command>`, or
  `devkit exec -s SERVICE <command>` for another service. Use the project's documented
  commands. Do not bypass devkit with host application processes or raw Compose.
- Read bounded output with `devkit logs` or `devkit logs -s SERVICE`; do not use
  `logs -f` in a task that needs to finish. For failures, use devkit-troubleshooting.
- A live preview mounts source, but reload behavior belongs to the application's
  development server. Resuming an existing preview does not rebuild images or reread
  its saved rendered Compose configuration. Dockerfile, dependency or recipe edits
  need `devkit rebuild PREVIEW --json` to inspect supported changes. Apply an authorized
  rebuild with `--yes`; it retains storage and the URL, while new app code can still
  migrate data. It runs Compose builds, not project hooks. A stopped preview stays
  stopped. Storage/topology/network changes and active shares block rebuilding;
  handle those as explicit migration work. Never destroy volumes to refresh code.
- If rebuild is interrupted or unhealthy, inspect `devkit rebuild PREVIEW --recover
  --json`. An authorized `--recover --yes` retries the same built candidate without
  rebuilding. Do not clear recovery records, rerun hooks, or guess a rollback.
  Exact graceful stop remains available; ordinary start/resume is blocked until
  recovery completes. Input drift requires manual compatibility review.

## Review, share and publish

For handoff, confirm the intended changes are committed under the user's Git
authorization, then use `devkit review HEAD` (or an explicit commit). Review builds
from that commit; uncommitted changes are not included. Repeating the same review
resumes its existing artifact. Verify health and relevant behavior, then report the
preview URL, full commit SHA, checks performed and any limitations.

Public sharing and team publishing need explicit authorization unless already
given. `devkit share PREVIEW --hours N` exposes that preview publicly; prefer a
review preview, choose an appropriate expiry, and report the public URL and expiry.
Sharing/unsharing recreates the web container to update its public URL, so account
for interruption. `devkit unshare PREVIEW` ends the share.

`devkit publish COMMIT` uses an existing reviewed image and the project's publish
hook. Verify the adapter exists and the destination is intended. Do not rebuild a
different image and call it the reviewed artifact, or claim success from a missing
adapter. Use devkit-secrets for credentials; never print secret values.

## Lifecycle and data

`stop`/`start` retain preview data. `destroy` deletes disposable named volumes;
`gc --yes` destroys orphaned previews; `prune --yes` affects host-wide build cache
and unused images. Use dry runs where available, identify exact targets/data, and
obtain destructive-action authorization when it is not already explicit. A failed
health check alone does not authorize cleanup, public exposure or host repair.
VM lifecycle work belongs to devkit-vms; do not restart VMs as preview maintenance.
