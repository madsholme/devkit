# Resource-linked secret cleanup

| Resource removal | Recoverable retention | Permanent deletion scope |
|---|---:|---|
| Branch | **7 days** | Only branch-owned keys; reconcile active worktree links first. |
| Worktree | **7 days** | Only worktree-owned keys. |
| Project | **30 days** | All project-owned keys including branches/worktrees; never Devkit system/other projects. |

## Required state machine

1. Resolve exact immutable resource ID, associated keys and active references; show UI/CLI dry-run preview.
2. Mark resource pending deletion and revoke normal access to its secrets; persist deadline and retryable cleanup record.
3. Perform OpenBao KV v2 soft deletion of applicable versions so they remain recoverable during retention.
4. Restore resource and its versions/references if user requests restoration before deadline.
5. On deadline, permanently **destroy all versions** of owned keys and remove their KV v2 metadata; audit without values.
6. Retry idempotently if OpenBao is sealed/unavailable or only partially completes a step. Report status in UI/CLI.

## Edge cases

- Branch still checked out in an active worktree: defer or adjust branch cleanup; don't destroy its referenced secrets prematurely.
- Detached HEAD, renamed branch, moved worktree: use immutable IDs, not names or directory paths.
- Recreated branch name: never inherit orphaned secrets from deleted branch's old ID.
- Worktree deletion must not delete project-shared or branch-owned records.
- Project deletion gets **30 days for all descendants**, not nested 7-day deadlines.
- Git changes made outside Devkit: reconcile carefully and require confirmation where ambiguous; temporary filesystem absence is not proof of deletion.
- Backups retain historical data independently of live-vault destruction: document backup expiration and privacy implications.

## Tests

Use a controllable clock. Verify soft delete, day 6/day 7 behavior, restoration before deadline, all-version destruction, metadata cleanup, queued retries during outage, active worktree references, and project 30-day cascade. Test that another project's secrets and Devkit system credentials survive untouched.
