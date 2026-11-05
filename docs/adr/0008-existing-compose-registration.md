# 8. Register existing Compose stacks without migration

Status: accepted, 2026-11-05. Extends ADRs 4 and 7.

PlayWarden's active M5 stack must retain its container identity, database volumes,
certificates and endpoints while becoming manageable in its project. Recreating
containers to add labels or copying its database would interrupt ongoing work.

Add `existing_compose` project/directory pairs to discovered recipes. Require both
existing Compose labels, canonical directory containment and one owning recipe.
Fail closed on ambiguous claims or incomplete recipe discovery. Revalidate full
container IDs and current registrations before each privileged read or start.

Registration grants project grouping, existing-container start, details and logs.
Reuse masked preview detail rendering, authenticated CLI jobs, per-container locks,
and the existing confirmed graceful stop. Keep data-deleting operations, preview
creation, sharing and port changes outside this authority. Discovery is read-only.
Individual start does not orchestrate dependencies or reconstruct removed containers.

Alternative rejected: manufacturing devkit labels/saved preview state would bypass
preview isolation and could make retained volumes subject to disposable cleanup.
No runtime migration or host networking change is required for registration.
