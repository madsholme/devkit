# 5. Explicit origin sync and separate push

Status: accepted, 2026-11-04. Extends ADR 4 and the previous read-only Git boundary.

The user requested Git sync controls in the combined project/QEMU dashboard and
selected safe fetch/pull with push separate. Add CLI-backed authenticated actions
on discovered checkouts. Read-only discovery continues to use local Git metadata;
it never fetches or changes a checkout. Docker is not a prerequisite for registered
Git projects.

Sync means fetch plus fast-forward to the current branch's existing origin upstream.
Dirty, detached, unborn, conflicted/in-progress and diverged checkouts are refused.
Push explicitly names that same destination and the checked source commit, bypassing
implicit push.default/refspec selection without bypassing Git hooks. Never force,
stash, rebase, commit, reset, create a branch or change remote/tracking configuration.
Ignored local files are protected from overwrite during fast-forward. Separate
controls make publishing local commits an intentional action.

Serialize devkit Git operations in the shared Git directory, preserve Git's locks
and hooks, revalidate membership at job execution and state after fetch, bound time,
and disable credential prompts. Redact remote diagnostics because URLs may contain
credentials. External Git processes do not take devkit's lock; users should finish
manual Git operations before invoking dashboard actions. Fetch can still update
remote tracking refs when a later safety check rejects sync/push.

Container inventory is independently read-only and includes unmanaged containers.
Existing managed-preview identity checks remain the boundary for details and logs.
