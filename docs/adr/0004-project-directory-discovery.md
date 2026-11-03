# 4. Project directory discovery

Status: accepted, 2026-11-04. Extends ADRs 2 and 3.

A project must be visible before its first preview or VM starts. Add one optional
host projects_dir and enumerate direct project folders, without scanning the home
folder or requiring T3. Keep explicit vm_projects and existing Docker/harness sources.
Git common directories establish repository/worktree membership; VM name/UUID mappings
continue to establish VM association. Directory proximity never grants VM control.

Use a separate read-only projects endpoint so Docker failure cannot hide the project
catalog or prevent VM controls. Return allowlisted checkout metadata and preview summaries;
never expose launch scripts or disk paths. Existing authenticated CLI-backed actions
remain the only mutation path. Invalid/unconfigured folders are visible but cannot launch.

Bound enumeration to 128 non-hidden directories and skip symlink children. This avoids
unintentionally trusting directories outside the configured parent; explicit vm_projects
and Git-linked worktrees still support deliberately registered external locations.

If projects_dir is unavailable or truncated, VM lifecycle controls are withheld until
discovery is complete: an unseen recipe might contain a conflicting identity claim.
