# 2. Explicit VM project mappings

Status: accepted, 2026-11-03. Extends [read-only inventory](0001-read-only-vm-inventory.md).

VMs need the same project grouping as Docker previews. Unlike Docker labels,
standalone QEMU and libvirt do not share a devkit project metadata convention.
Store exact name/UUID selectors in each project's existing recipe, with optional
host `vm_projects` roots for discovering VM-only recipes. No new registry database
or launch wrapper is required. Reuse Docker worktree labels and harness glob settings
for recipe discovery; process cwd is a discovery hint, never an ownership claim.

UUID claims outrank names. Duplicate identifiers and cross-project conflicts stay
unassigned instead of depending on discovery order. Same-slug worktrees form one
project, as Docker previews already do. This describes association and does not
change permissions. The tradeoff is that users must add explicit mappings; guessed
name prefixes and disk paths could silently attribute a guest to the wrong project.

The API remains a flat inventory with project metadata; CLI and dashboard group
it by slug with unmatched VMs last. Discovery failures cannot hide VM inventory.
