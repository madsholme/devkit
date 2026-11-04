# Host VM visibility repair — version 6 (completed)

Supersedes [v5](2026-11-04-project-controls-v5.md). The source implementation remains
unchanged; this was a host configuration repair. The exact original plan, task record,
process IDs, local paths, domain, API captures and screenshots are retained in the
operator's private evidence archive, not published with this repository.

## Findings and repair

- The web service's group-refresh wrapper retained a supplementary group's identity
  as its final primary group. Same-user QEMU process executable links were readable
  in a login shell but not by that service. Restoring the account's normal primary
  group after refreshing already-granted groups fixed process discovery.
- The project registered older guest names. Two newer restored guests needed their
  exact names added as read-only mappings. Existing lifecycle definitions were retained.

## Completed task boundaries

1. Captured private byte-for-byte wrapper/recipe backups, modes, service identity,
   binary checksum/revision, API/CLI baseline and guest PID/start-time identity.
2. Prepared and shell-checked a candidate group wrapper; proved normal primary group,
   original supplementary groups and readable QEMU metadata before installation.
   Atomically installed it, restarted only the web service, and verified service,
   Docker inventory and both running guest entries. Rollback was prepared but not needed.
3. Added exactly two plain-name selectors, preserving all existing recipe values and
   older lifecycle definitions. Checked CLI/API project grouping, uniqueness and no
   new guest-control authority. Preserved the user's untracked recipe.
4. Verified trusted HTTPS in the real browser: both guests appeared exactly once as
   running in Virtual machines and under their project. Repeated both checks after
   refresh more than ten seconds apart; captured screenshots/text and a clean console.
5. Recorded host status, private receipt and rollback instructions. Guest process
   identities and binary checksum were unchanged; no VM lifecycle or disk action occurred.

## Constraints retained

Use one agent and the existing task worktree. Do not grant group membership, relax
security policy, infer ownership, reuse old guest adapters for new guests, overwrite
concurrent edits, or mutate guests to obtain passing checks. Real browser evidence is
required; API-only checks cannot substitute for the two visible views. If a guest exits
independently, record the changed state rather than restarting it.

Rollback restores only task-owned changes after checking for newer edits, restarts only
the web service, and verifies guest preservation. The full originals and evidence remain
private; this sanitized archive preserves requirements and outcomes for repository review.
