# Agent CLI expansion design — v1

Status: approved through the user's implementation request, 2026-11-17.
All five additions are implemented. Go/TLS/distribution checks and combined Docker
acceptance through the fixture bridge pass. The published-loopback smoke path timed
out on this host; see the implementation plan for evidence and scope.

## Purpose and relationship to existing work

Give an agent direct, bounded access to the operational facts it needs: logs from
the intended preview, host/project inventory, private-route connectivity, recent
events and cleanup outcomes. Add operator guidance for host maintenance and vault
recovery without turning application work into host administration.

This is a new additive workstream. It does not supersede the completed
[agent operations v1](2026-11-16-agent-operations-v1.md),
[agent guidance v14](2026-11-16-agent-guidance-v14.md), or
[vault restore v1](2026-11-16-vault-restore-v1.md). Their behavior, acceptance
requirements and evidence remain intact. The implementation plan is
[agent CLI expansion v1](2026-11-17-agent-cli-expansion-v1.md); architecture is
recorded in accepted [ADR 20](../adr/0020-agent-cli-observability.md).

## Approach

Reuse local Go collectors, ownership checks, the versioned operation report and
the single-file bundled skill installer. Add focused command adapters and safe
projections; make small collector changes only where completeness, cancellation
or deterministic identity cannot otherwise be established.

Alternatives considered:

- Dashboard HTTP wrappers would reuse endpoints but require a running web service
  precisely when diagnostics are most useful, and would inherit UI-shaped output.
- More shell instructions in skills would leave targeting, parsing and safety
  decisions to each agent and would not supply missing runtime facts.
- Direct CLI adapters reuse the working backend while retaining offline/local
  access and explicit machine-readable failure states. This is the selected
  implementation approach. No generic provider framework or new dependency is needed.

## Command contract

| Addition | Interface | Scope |
|---|---|---|
| Exact logs | `devkit logs [--preview ID] [-s SERVICE] [--tail N] [--since TIME] [-f]` | Managed live/review previews belonging to the current Git common directory. |
| Project inventory | `devkit projects [--json]` | Existing host project discovery and linked checkout grouping. |
| Container inventory | `devkit containers [--json]` | Existing all-host inventory and explicit registration/capability evidence. |
| Resource inventory | `devkit resources [--json]` | One bounded host and container sample, independent of the web sampler. |
| Connectivity | `devkit check [PREVIEW] [--timeout DURATION] [--json]` | Observed primary private HTTPS route of one owned preview, from this host. |
| Operational reports | `devkit history [PREVIEW] [-n N] [--json]`, `devkit shares [--json]`, `devkit gc [--yes] [--json]` | Structured equivalents of existing operations, including GC partial results. |
| Operator guidance | `devkit-host` bundled skill | Explicit host setup, updates, proxy maintenance, vault backup/recovery. |

New reports reuse `schema_version: 1`, `command`, `complete`, `data`, `checks` and
the existing check statuses. Exit 0 means complete evidence with no failed checks;
1 means an operational failure or required evidence unavailable; 2 means invalid
invocation. Warnings alone may return 0. JSON invocations emit exactly one object
on stdout, including usage and operational failures. No progress or subprocess
output may contaminate it. Existing text output and existing JSON formats remain
compatible. No new JSON mode is added to raw logs.

## 1. Exact-target logs

- With no preview selector, retain current-worktree live selection and default web
  service. Extend it to retained stopped/exited containers. An explicit ID may
  select a live or review preview from a sibling checkout of the same repository.
- Establish the common Git directory and compare every matching managed
  container's repository, preview, mode and Compose-service identity. Reject
  foreign clones, conflicting ownership, malformed IDs and ambiguous candidates.
  Do not select the first container or infer authority from a display name.
- Default web selection uses observed `isWeb` metadata, including its legacy
  fallback. Explicit service selection uses the exact Compose service label.
  Duplicate containers for a requested service fail with a bounded diagnostic;
  replica aggregation and arbitrary-container log access are outside this version.
- An explicit preview can be diagnosed even when the current recipe was edited
  or removed. Saved records may corroborate identity but cannot manufacture a
  container. Missing/removed containers fail without starting or recreating them.
- `--tail` defaults to 200 and accepts integers 1–10000; reject `all`, negative,
  zero, malformed and excessive values. `--since` accepts a positive Go duration
  up to 720h or RFC3339/RFC3339Nano with an explicit zone. Convert relative time
  once to UTC; reject future absolute times. Keep flags before execution arguments
  and invoke Docker with an argument array, never a shell.
- Non-follow execution has a 15-second deadline and a 4 MiB combined output cap.
  On overflow, stop the reader/process, print a clear truncation diagnostic and
  return 1. Explicit `-f` keeps streaming until cancellation and has no total byte
  cap; kill/wait the child on cancellation. Docker failures return nonzero.
- Logs are raw application data and can contain secrets. Do not persist them in
  history or JSON reports, promise redaction, or include them in agent summaries
  without inspection. Keep `exec` restricted to its current live-preview contract.

## 2. CLI inventory

`projects` reuses project directory discovery, Git-common-directory grouping and
checkout associations. `containers` reuses full-ID inventory and exact existing
Compose registrations. Neither requires the web service or changes Git, recipes,
registrations, services or credentials. Both work outside a Git checkout.

Use typed CLI projections rather than serializing dashboard structures wholesale.
Project output includes discovered roots, checkout paths/branches, worktree keys,
preview associations and observed capabilities; host inventory intentionally lists
configured project paths. Container output includes full ID, name, image, observed
state, project/preview/service associations, published ports, registration and
capabilities. Omit raw environments, commands, mounts, arbitrary labels, remotes,
credential sources and free-form command errors. Capability fields are observations;
subsequent mutations still revalidate through their existing authority paths.

Use stable sorting with full identity tie-breakers. Docker unavailability retains
available project rows but sets `complete: false`, exit 1 and stable check codes.
Discovery/registration failures must remain typed unavailable evidence, not be
silently converted into empty lists or confident capabilities. A genuinely empty
configured catalog is an empty complete result; missing required host configuration
is a failure. Preserve current HTTP response shapes and graceful UI degradation.

One-shot `resources` samples host CPU twice, 250 ms apart, then reports the measured
interval, timestamp, cores, load, memory, swap, root-filesystem usage and uptime.
Container usage retains full container IDs, names and exact known associations.
Reuse the sampling/parsing logic, not `newResources()` (which starts a background
loop). No process command lines, per-process inventory or history buffer is exposed.
Do not report an unavailable first CPU sample as zero load. Keep unavailable metrics
nullable, valid zero values numeric, and never associate samples by name alone.
The whole resources command has a 10-second budget; projects/containers have a
30-second budget. Propagate cancellation into new subprocess paths, with bounded
output. Exceeded budgets/limits produce partial reports, not healthy empty results.

## 3. Host skill

Add one concise, single-file `devkit-host` asset through the existing catalog and
installer, version 1. Installation, status, repair, setup, doctor and managed
project blocks recognize six skills. Both Codex and Claude templates stay aligned;
edited assets, modes, unrelated guidance and the supported sibling alias survive.

Activate for requested host provisioning/maintenance, Devkit binary/service updates,
private proxy configuration, managed vault backup/update/restore and recovery.
Application secrets remain in `devkit-secrets`; preview/VM work stays in its current
skills. Host guidance must establish the actual machine, install method, current
and served versions, relevant STATUS.md, service state and existing authorization.

Document existing workflows, not new maintenance commands: inspect before changing;
keep rollback artifacts; handle source builds versus release binaries; use the
applicable distro's bootstrap only for authorized prerequisites; preserve private
listeners and Traefik's ownership of 443. Verify affected behavior after updates.
Do not assume a local checkout or service exists on every agent machine.

For vault work distinguish checksum verification, isolated restore rehearsal and
actual cutover. Preserve same-host identity, human-held recovery material, explicit
web-service stop, mandatory rehearsal on apply, journal rollback, retained old data,
revoked Devkit API tokens and required service resumption. Never read human recovery
material into chat or claim that a synthetic test proves a real backup recoverable.
Reference matching-release documentation; if it is unavailable locally, locate
that documentation rather than inventing a consuming project's `docs/secrets.md`.

## 4. Connectivity checks

Resolve the same owned preview identity as logs and inspect all its service states.
Probe only the primary observed `devkit.url` HTTPS origin on port 443 at `/`, with
no userinfo, query or fragment; compare the observed web router's exact Host rule
and backend service/port against the selected web container. Do not rebuild a URL
from an edited recipe or treat a public share as the private endpoint. Report
additional paths/subdomains as outside the checked scope, without probing them.
Reject unsupported/ambiguous router evidence rather than parsing arbitrary Traefik
expressions. A partial rebuild remains visible; diagnostics can read it but never
clear its journal or resume stale configuration.

The total timeout defaults to 15s, accepts Go durations from 1s through 60s, and
covers inventory, DNS, TCP, TLS and HTTP. Allowlist actual private local listener
addresses from host configuration and observed interfaces/Tailscale state. Expand
wildcard binds only to verified local private addresses; allow loopback only when
explicitly configured. A DNS destination must belong to that exact set. Reject
public, foreign private, link-local, multicast and unspecified destinations. Treat
Tailscale addresses by exact verified host identity, not a broad private-CIDR guess.

Resolve once, sort and validate all results, and pin connection attempts to those
IPs while preserving hostname/SNI and ordinary certificate verification. Check at
most eight addresses; larger sets fail as unsupported. Disable environment HTTP
proxies, credential injection, cookies and redirects. Do not re-resolve through the
HTTP transport. Record attempted IPs and selected peer; warn when only some approved
addresses work. No `--insecure`, arbitrary URL, public-share or address-override flag.

Check codes distinguish target/route evidence, service state, DNS mismatch, TCP
failure, certificate failure and HTTP response. Never copy response bodies, headers,
redirect URLs, health command output or raw TLS/tool errors into reports. The TLS
and HTTP checks use standard-library clients with bounded headers and close response
bodies without reading them into the report.

| Observation | Result |
|---|---|
| Running web with successful configured health check | Service check passes. |
| Running web without a health check | Warning: process running; application health unknown. |
| Unhealthy, restarting, paused or stopped web | Failure; stopped/paused networking is skipped. |
| Non-web service stopped | Warning; do not guess whether a one-shot service should run. |
| Running non-web service unhealthy | Failure. |
| HTTP 2xx | Endpoint response passes; not proof of an authenticated user workflow. |
| HTTP 3xx, 401 or 403 | Warning: endpoint responded, redirect/authentication flow not checked. |
| Other HTTP 4xx/5xx | Failure with numeric status only. |
| Missing evidence, timeout, invalid TLS or unsafe destination | Failure with explicit unavailable/skipped downstream checks. |

Every result includes `vantage: "host"`, `scope: "primary-route"`, actual identity,
observed states, stage results and `application_verified: false`. Complete means
required observations finished, not that the application is healthy. This command
never verifies a remote client's LAN/VPN route and never repairs or launches anything.

## 5. Structured operational output

- `history --json` reuses the existing history reader and filtering. Keep the text
  command's oldest-first display order, default 30, and existing `-n` acceptance.
  Allowlist timestamp, action, preview, worktree and success flag; normalize actor
  to `cli`, `dashboard`, `auto` or `other`. Omit free-form `detail`, whose old records
  may contain command errors. Preserve malformed-line handling and rotated-history
  behavior; missing history is complete and empty.
- `shares --json` is local connector metadata, not Cloudflare verification. Include
  preview, validated public URL/hostname, expiry in UTC, fixed-host flag and an
  observation timestamp. Omit tunnel/DNS identifiers and repository paths. Invalid
  expiry remains null with a warning; an expired local connector stays visible.
  No cloud calls, credential-source evaluation, renewal or automatic cleanup.
- `gc --json` reports the existing dry-run selection, unknown/kept sources, exact
  preview IDs, modes, source identity and deletion categories (containers, networks,
  disposable named volumes and run directory). It does not claim an exact volume
  manifest or bytes reclaimable. No new selection algorithm or worktree deletion.
- `gc --yes --json` uses the same locked per-target executor as text mode. Retain
  fresh worktree/owner checks, preview-before-media lock ordering, recovery guards,
  existing share cleanup behavior and success/failure history semantics. Emit each
  target's outcome (`destroyed`, `failed`, `not_attempted`, `kept`) and safe warnings.
  Stop at the first destructive failure as today; earlier successes are not rolled
  back or replayed. Failure may follow partial side effects, so `failed` must not
  imply an untouched target. Unshare failures remain explicit in the report even
  if current destruction policy proceeds. No persisted job/plan token is added.

## Boundaries and acceptance

No new web endpoints, UI behavior, dependencies, arbitrary-container log authority,
review exec, remote-client execution, VM lifecycle changes, automatic repair,
public exposure, database backup framework or generic maintenance engine. Existing
legacy devbox recipes/variables and saved previews remain supported. Diagnostics
do not run hooks, Git fetch, credential commands, secret reads or mutation history.

Acceptance requires the five additions, documented CLI examples, six-skill
distribution/preservation, exact ownership and partial-result tests, a trusted-TLS
local HTTP fixture, and disposable Docker log/GC outcomes with retained-data
sentinels. Existing HTTP and CLI regression checks must pass. Live model sessions,
deployed host changes, real vault recovery and VM restarts are separate authority;
record unrun behavioral scenarios honestly rather than substituting string checks.
