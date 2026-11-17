# 20. Local CLI observations and operator guidance

Status: accepted for implementation, 2026-11-17. Extends [ADR 18](0018-agent-operation-reports.md)
and preserves [ADR 19](0019-tested-vault-restore.md).

The dashboard has project/container/resource facts that CLI agents cannot query
directly. CLI logs select only a running live container, and human-readable event,
share and GC output leaves agents reconstructing identity and operation results.
Vault recovery and host maintenance also lack a dedicated bundled skill.

Use the existing local collectors with explicit typed projections under the
versioned operation report. Do not depend on a running web service or serialize UI
objects and free-form errors wholesale. Resource sampling is a bounded one-shot
operation. Exact preview diagnostics require the caller's repository ownership;
read-only association/capability evidence never grants mutation authority.

A private-route check is a host observation of one existing preview. Validate
observed routing, resolve once, restrict destinations to verified private host
listeners and pin the dial while retaining hostname/TLS validation. Do not follow
redirects or carry credentials. Unknown health and remote-client reachability stay
unknown. Raw application logs remain distinct from sanitized metadata reports.

Structured GC reuses selection and locked mutation behavior and records partial
outcomes without retrying completed deletions. No job framework or new cleanup
authority is introduced. Add a sixth single-file skill, `devkit-host`, to document
existing operator workflows separately from project credential use.

Dashboard wrappers and additional shell recipes were rejected as the main approach:
the former require an available web service, and the latter duplicate resolution,
parsing and privacy decisions across agents. The cost of direct adapters is a small
maintained CLI schema and collector cancellation/completeness work. This is justified
by useful diagnostics during service outages and safer exact-target operations.

Behavior and limits are specified in the [design](../plans/2026-11-17-agent-cli-expansion-design-v1.md)
and delivered through the [implementation plan](../plans/2026-11-17-agent-cli-expansion-v1.md).

Implementation and Go/TLS/distribution/Docker-bridge workflow acceptance are
complete. The implementation plan records evidence and the host-specific published
loopback limitation separately from this architectural decision.
