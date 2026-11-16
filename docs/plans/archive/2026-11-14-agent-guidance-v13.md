# Preview and troubleshooting agent guidance — v13

Supersedes [v12](2026-11-14-vm-connections-v12.md), delivered on main and
the host as 88c55dd. Preserve the VM/password defaults and existing skill protections.
The user now authorizes bundling `devkit-previews` and `devkit-troubleshooting`,
committing/pushing main, and updating the running dashboard. No VM restart is allowed.

Preview guidance covers checkout ownership, live/exec/bounded logs, exact-commit
review handoff, resume versus rebuild, data-preserving lifecycle decisions, and
explicit authority for destructive cleanup, public sharing and publishing. It does
not create Git authority or introduce extra approvals for already authorized work.

Troubleshooting guidance starts with bounded, read-only evidence and separates app,
container, proxy/TLS/DNS and client LAN/VPN failures. It protects credentials, retains
the actual hostname when testing HTTPS, and does not infer repair/restart authority
from diagnostic requests. Refer VM and credential cases to their existing skills.

Reuse the installed skill catalog for both assets and project instruction blocks,
preserving status JSON compatibility, user edits, permissions, and the exact sibling
CLAUDE.md alias. Setup/operator repair and doctor must cover all four skills; new
templates and explicit existing-project installation must contain matching blocks.
Do not silently replace old project-specific preview guidance or run its hooks.

1. Add failing distribution, preservation and template/project-block contracts.
2. Write the two skills and update catalog, managed blocks, templates and docs.
3. Verify Go tests/vet and review practical scenarios without subagents. Push main,
   deploy the exact build with rollback, install guidance locally and in registered
   host checkouts, and verify unchanged VM/container/T3 identities and configuration.

Review scenarios: wrong checkout, changed Dockerfile with valuable volumes, dirty
review input, public share request, failed HTTPS with missing VPN route, unhealthy
application versus proxy failure, edited guidance, and a sealed credential source.
Fresh agent sessions may be needed to refresh skill discovery. Host receipts record
deployment/install evidence; reuse existing guarded runners and completed VM checks.

## Verification checkpoint

Initial regressions demonstrated absent workflow assets and managed project blocks.
Full Go tests and vet pass after implementation. Contracts cover both agent catalogs,
read-only status, missing-asset repair, user-edit preservation for all four blocks,
current templates, missing-guidance diagnosis and the retained safe sibling alias.
The practical scenarios above were reviewed against the existing CLI/docs without
subagents or live workload mutations. No frontend source/assets or VM skill defaults
changed. Push/deployment and host installation use the existing guarded runners;
final host receipts preserve the rollout evidence and unchanged runtime identities.
