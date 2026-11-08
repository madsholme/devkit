# ADR 0013: OpenBao authorization on a trusted development host

Status: **Lite guide and configurable-cleanup amendment replace the initial-build
design, 2026-11-08. Retained decisions below apply; plan v7 is approved and implementation is in progress.**
Implementation authorized on 2026-11-08; no live deployment or secret migration performed.

Current plan: [OpenBao Lite v7](../plans/2026-11-08-openbao-v7.md).
Current specification: [Lite guide](../plans/Devkit_OpenBao_Lite_Implementation_Guide.md),
with the later user amendment making retention configurable and automatic cleanup optional.
Previous proposals: [archived v5](../plans/archive/2026-11-08-openbao-v5.md), [archived v4](../plans/archive/2026-11-08-openbao-v4.md),
[v3](../plans/archive/2026-11-08-openbao-v3.md),
[v2](../plans/archive/2026-11-08-openbao-v2.md) and
[v1](../plans/archive/2026-11-08-openbao-v1.md).
Historical input: [original handoff](../openbao/README.md), superseded for the initial build.

## Context

Devkit runs a Go CLI and user-service dashboard, starts T3 as a user service,
and grants its operator Docker access through bootstrap. Project hooks and
Compose execute project-controlled code. The dashboard has human authentication;
the existing CLI has no distinct agent principal. Git common directories, paths
and branch names currently establish project/worktree associations.

The initial Phase 0 proposal made OS-isolated agent workers a prerequisite for
system-secret storage. During review, the user clarified that this is a development
kit rather than a hardened environment and explicitly accepted the trusted
single-user model. Existing T3, Codex, Claude Code, Docker and project execution
workflows must continue without a mandatory isolated-worker migration.

## Accepted decision D1

Devkit targets a trusted, single-user development host. Enforce project/system
separation through Devkit’s validated operation boundary, and prevent
accidental credential exposure. Preserve existing agent and Docker workflows.
Protection against arbitrary code exercising the host user's filesystem, sudo or
Docker privileges is outside the v1 product threat model.

The source handoff's wording that agents cannot access system/other-project
secrets is therefore an **API scope guarantee on the project-secret interface**,
not an OS-isolation guarantee. The Lite guide replaces per-project credentials
and policies with one restricted backend identity: Devkit validates scope while
OpenBao limits the backend to managed paths. Shared-user logical policies do not
contain arbitrary host code. The original source handoff is preserved unchanged as historical input;
this accepted review decision governs that interpretation.

Consequences for the design:

1. Use a same-user secrets broker and the existing user-service conventions.
   Private socket/config/state permissions protect against other users; they do
   not distinguish a same-user human from an agent. No dedicated service account,
   VM/container worker, T3 launcher replacement or Docker/sudo removal is required.
2. CLI and UI use the same backend operations and context/ownership validation.
   Project requests cannot address system paths or unrelated owners through
   forged selectors. IDs/cwd/`DEVKIT_BY` are not authentication. Lite has no
   per-project grant-management UI or per-worktree AppRoles.
3. Separate explicit operator management from project secret operations; require
   browser authentication for all secret endpoints. One restricted OpenBao backend
   identity stays in private host configuration, never project responses/files or
   child environments. No raw OpenBao API proxy or automatic project-to-operator
   retry. Same-user CLI commands cannot distinguish a human from an agent.
4. Same-user code can potentially read private operator/service credential files
   or use host Docker to bypass these controls. Document that limit. Claims about
   denied agent access must name the project interface and validated scope tested.
5. Preserve root retirement, human recovery-material handling, authenticated
   metadata/reveal, secret-free routine output, explicit ownership, safe runtime
   injection and fail-closed vault access. Skills guide workflow; authorization is
   enforced by code. D1 does not relax these requirements.
6. Replace mandatory OS boundary tests with real API-scope/backend-policy tests
   and credential-propagation tests. Verify current agent/preview compatibility;
   do not introduce an execution-runtime rewrite as part of secrets integration.

## Accepted decision D2

Existing tools retain Git lifecycle. T3, Codex, Claude and Git create, switch,
rename, move and remove branches/worktrees as they do today. Devkit manages secret
context, inheritance, registration/rebinding, reconciliation, retention and cleanup.

The user explicitly accepted this division. It amends the original handoff's
additional requirement for general branch/worktree create/switch/delete commands.
Every Devkit secret-management feature still requires a documented CLI equivalent.
Do not add those Git mutations to the dashboard merely to satisfy the old CLI list.

- Preserve existing `devkit project create`, Git fetch/sync/push and preview
  lifecycle. The existing temporary detached review-worktree behavior remains an
  internal exception; it does not enroll a persistent secret scope.
- Resolve project/branch/worktree context from verified existing Git state and
  registered immutable IDs. UI context selection does not switch Git branches.
  Explicit secret registration changes registry metadata only; discovery is read-only.
- Observe externally made changes and preview reconciliation. Confirmed rebinds
  after rename/move can retain an ID. A reused name/path or equal commit alone
  cannot transfer an old scope. Ambiguity requires operator resolution.
- Missing/unreadable paths and incomplete inventory never prove deletion. A
  confirmed exact-scope retirement uses configurable retention, defaulting to
  7 days for branch/worktree and 30 for project descendants; D7 allows no automatic
  cleanup. Keep observation time separate
  from confirmation; do not shorten retention using a guessed deletion timestamp.
- Active worktree references block branch-scope retirement. Retirement never
  removes source files, Git refs or unrelated runtime resources. Preview gc is
  not secret-cleanup authority.
- Secret restore restores values and registry state, not Git branches/worktrees.
  Ordinary context access resumes only after required verified rebinding succeeds;
  existing Git tools remain responsible for restoring missing source resources.

Optional Git wrappers would provide clearer intent when used, but external Git
changes would still require reconciliation. They add unnecessary lifecycle and
failure-recovery scope to this secrets integration and are excluded. Existing Git
ADRs 0005/0010 remain in force; no general Git lifecycle expansion is authorized.

## Accepted decision D6

Keep ACME account and certificate private-key state in Traefik's existing
protected local certificates volume, with encrypted backups. DNS tokens remain
in OpenBao and are delivered through protected memory-backed runtime files.

This explicitly amends the handoff's rule that only minimal authentication
bootstrap material may remain outside the vault. The exception covers Traefik
ACME state only; it does not extend to DNS or other integration tokens. Private
local storage is not OpenBao encryption at rest, and the trusted-user limits of
D1 still apply. The original handoff remains unchanged as historical input.

- Keep Traefik the sole writer; preserve production/staging separation, owner-only
  ACME file permissions and restrictive directory access/mounts. Never return
  private keys through Devkit APIs, logs, jobs or agent output.
- Remove the proposed ACME-to-vault synchronization and tmpfs restoration. Do not
  migrate or retire the existing certificates volume. This preserves stock
  Traefik storage and avoids a second authority for certificate renewals.
- Include consistent ACME-state copies in encrypted recovery archives. The
  proposed simplest capture procedure briefly stops Traefik, validates and
  streams the state into encryption, then restarts the proxy even if backup
  fails. Expose that interruption in the backup plan. Restore with Traefik
  stopped, preserve ownership/mode and verify usable TLS in an isolated target.
  A Raft snapshot without the local certificate state is not a full host backup.
- Test cold proxy startup with existing certificates, absent runtime DNS tokens
  and a sealed vault against the pinned Traefik image/configuration. Retaining
  certificates alone does not guarantee successful startup. Distinguish serving
  existing certificates from issuance/renewal; report blocked behavior and verify
  recovery after unseal. Never add persistent plaintext DNS-token fallback.
- Keep local human CLI unseal independent of Traefik, DNS credentials and the
  vault-backed web-session key. HTTPS recovery convenience must not become the
  only way to recover HTTPS dependencies.

Vault-backed ACME state would centralize storage, but adds renewal checkpoints,
concurrent-write handling and cold-start restoration dependencies. The accepted
local-volume exception is simpler for this trusted development host. Encrypted
off-host backup destination, custody and retention remain D4 review inputs.

## Accepted D3 access, D4 unseal and D5 integration scope

The vault Web UI must work from both the user's local network and remote Tailscale
clients at the same trusted HTTPS hostname. Direct LAN access must work without
Tailscale on that client. Reuse existing Traefik with the Tailscale address, an
explicit stable LAN listener and local split DNS; preserve existing dashboard and
harness routing. Keep native OpenBao authentication and private network controls.
Actual LAN address/subnet/resolver and both client probes remain deployment inputs.

Manual human unseal after OpenBao restart is accepted. Keep its recovery path
independent of DNS tokens, Traefik and vault-backed web sessions. No auto-unseal or
host-stored unseal key is implied. The user has not selected custody/share threshold,
backup destination/recipient/transport or retention. A single human-held key with
a separate recovery copy is the current proposal for this single-user kit; the
earlier unapproved 2-of-3 proposal is not a requirement. Daily encrypted off-host
backups, 30 daily copies and quarterly restore checks remain proposed defaults.

The user requests Cloudflare, generic Git and GitHub integration and explicitly
selected HTTPS credentials while preserving existing SSH keys and agent:

- Keep Cloudflare DNS/share credentials separate. Add generic Git HTTPS credential
  delivery for explicitly configured hosts/repositories and GitHub token/API
  validation. Replace the unapproved GitLab-specific adapter with generic Git;
  prove generic behavior against a non-GitHub HTTPS fixture as well as GitHub.
- Proposed delivery uses Git's credential-helper protocol with explicit
  repository-local bind/unbind/status and CLI/UI parity. Preserve unrelated
  helpers and global configuration; an enrolled scope must not silently fall
  through to old credentials when OpenBao is sealed or denies access. Validate
  scheme, exact host/port/path, registered project and authorized context. Cwd or remote
  URL alone cannot authorize a value. No helper invocation during discovery.
- Project-owned Git credentials remain project-scoped. Operator GitHub API
  credentials stay integration-scoped; no automatic system-token delegation or
  copy into projects. Git's consumer pipe may carry an authorized credential;
  jobs, logs, config, URLs and ordinary JSON must not. Explicit configure/rotate
  owns writes; helper store/erase must not silently import or destroy secrets.
- Preserve SSH keys/agent and existing Git operations. No new Git lifecycle or
  GitHub repository/issue/PR-management scope. Stored-token replacement is not
  a promise to revoke or issue a token at its remote provider.
- Tailscale was raised as a question, not an agreed API-automation use case.
  Existing bootstrap uses interactive login and the daemon owns node identity;
  normal connectivity requires no new Devkit-held API token. Recommend retaining
  this behavior. API/OAuth/auth-key automation may be added only for a selected
  use case, storing configured credentials in OpenBao without moving node keys
  or making host connectivity depend on the vault recovery path.

These decisions do not approve the whole implementation plan. Git helper mechanics
remain proposed until that review; backup and deployment inputs remain open. The
Lite guide retains credential support but supersedes the per-project grant system.

## Lite simplification and accepted decision D7

The user's Lite guide replaces the older elaborate plan for the initial build.
Use one restricted backend identity and three delivery milestones. Remove the
per-project grant administration, multiple service-role design, elaborate backup/
upgrade UI and equivalent second secret-provider implementation. Existing local
opt-out preserves current local settings; managed project secrets are unavailable
when OpenBao is disabled. Keep simple scheduled encrypted backup and tested manual
restore, health and minimal audit events. Preserve D1's trusted-user boundary,
D2's Git ownership, LAN/Tailscale access, manual unseal, Git HTTPS/GitHub scope and
D6's protected local ACME exception. Never claim API scopes isolate arbitrary
same-user code.

The user subsequently required configurable cleanup, including no automatic
cleanup. This explicitly overrides the guide's fixed-retention wording:

- Branch/worktree/project retention settings default to 7/7/30 days; each accepts
  a positive duration or `never`. A global automatic-cleanup off switch is exposed
  in both authenticated UI and scriptable CLI.
- Off prevents automatic retirement, soft deletion and purge. Explicit previewed,
  confirmed retirement/manual purge and recovery remain available; retired values
  stay recoverable without automatic destruction. OpenBao TTLs must not bypass it.
- Settings apply prospectively. Keep existing deadlines as metadata while off;
  changing settings or re-enabling must not silently shorten retention or purge
  overdue values. Require preview/confirmation before resuming an overdue batch.
- Project cascade uses its configured policy and must not silently shorten longer
  or indefinite existing protection. Preserve settings and deadlines over restart
  and backup/restore. Backups have a separate retention policy.

Plan v6 defines the concrete controls/tests. No product code or live state changes
are authorized by these planning amendments.

## Remaining proposed decisions

- Keep standard-library Go and the existing Vue/SwiftDesign frontend. Add a small
  versioned private registry and durable operation journals under existing Devkit
  state-directory conventions. Immutable IDs are distinct from mutable Git paths,
  branch names and preview identities; ambiguous external changes require review.
- Preserve preview names/data and exclude temporary detached review checkouts
  from automatic persistent secret enrollment. Preview gc does not authorize
  secret deletion. Implement the accepted D2 observation/reconciliation boundary;
  do not add general Git lifecycle commands.
- Use production-mode single-node OpenBao with persistent Raft; bootstrap before
  DNS credentials, with accepted manual unseal. Never
  reinitialize populated storage. Root/recovery material remains human-held.
- Reuse Traefik at the requested nested hostname with correct TLS/private routing.
  D3 access on both networks is accepted; actual LAN/Tailscale DNS inputs remain. Apply accepted
  D6 local ACME custody and keep DNS tokens in OpenBao.
- Compute worktree > linked branch > project inheritance in the broker. List and
  history omit values; reveal uses transient responses, never retained jobs.
  Parent edits and updates require explicit ownership/version checks. Copy/promote
  is deferred from the initial Lite command set; explicit set/override remains.
- Journal recoverable cleanup with configurable defaults of branch/worktree 7 days
  and project/descendant 30 days, plus optional automatic cleanup under D7. Active
  references block destruction. Backups preserve coherent
  identity/deletion state and have an independent retention policy.
- Install one canonical versioned skill for both agents in their existing account
  and home directories. Preserve user instructions, test fresh-session discovery,
  and never treat skills as permissions.

## Alternatives considered for D1

- **Separate service account:** adds filesystem separation from ordinary direct
  reads, but host Docker/sudo can bypass it. Optional future defence in depth;
  not necessary for the accepted model and not required by this implementation.
- **Isolated project workers/VMs:** can enforce a stronger boundary if host
  privileges and cross-project access are removed. Introduces launcher, mount,
  credential and runtime changes disproportionate to the accepted development-host
  scope. Deferred unless a stronger threat model is explicitly requested later.
- **Instructions alone:** insufficient. Even on a trusted host, scoped API
  validation must reject out-of-context/system-path requests on project routes;
  OpenBao must restrict the backend identity to its managed paths.

## Review status and change record

D1–D3, manual unseal under D4, Cloudflare/Git HTTPS/GitHub under D5 and D6 are
accepted. Recovery custody/threshold, backup profile/destination/retention and
actual network configuration remain inputs. Tailscale API automation remains an
optional proposal. Lite simplification and D7 configurable cleanup are accepted
requirements; the replacement plan still needs review. None authorizes implementation,
migration or deployment.
Existing Git ADRs 0005/0010 remain in force under accepted D2.

Revision on 2026-11-08: removed mandatory OS accounts, worker runtimes and host
privilege denial from the proposal; updated the guarantee, service/state locations,
task acceptance and agent installation assumptions. The superseded implementation
plan and task records are retained in the plan archive. No product code changed.

D2 revision on 2026-11-08: replaced the proposed general Git lifecycle wrappers
with secret-resource registration/reconciliation/retirement, clarified retention
start and recovery semantics, and preserved existing Git/agent workflows. Plan v2
and its task records were archived; v3 was authoritative at that review. ACME
decision D6 was then unapproved. No product code changed.

D6 revision on 2026-11-08: accepted protected local ACME storage with encrypted
backups and vault-held DNS tokens; removed vault synchronization, added the narrow
storage exception and explicit cold-start/backup/restore verification. Plan v3 and
its task records were archived; v4 was authoritative at that review. D3–D5 and the
overall plan were then under review. No product code changed.

D3/D4/D5 revision on 2026-11-08: recorded direct LAN plus remote Tailscale UI access
and manual unseal; replaced the proposed GitLab adapter with generic Git HTTPS
and GitHub support, preserving SSH keys/agent and existing Git lifecycle. Kept
Tailscale connectivity independent of vault credentials and marked API automation
as optional. Plan v4 and task records were archived; v5 was authoritative at that review.
No product code changed and all implementation tasks remained pending.

Lite/D7 revision on 2026-11-08: adopted the replacement guide, reduced the backend
to one restricted identity and three milestones, retained prior workflow decisions,
and changed fixed cleanup windows to configurable defaults with no-autocleanup
support. Plan v5 and task records are archived; v6 is authoritative and all three
implementation milestones remain pending. Original specification inputs are preserved.

Update/storage amendment on 2026-11-08: user authorized Lite implementation, requested
explicit OpenBao updates and selected a bind-mounted data directory. Use private
`stateDir()/secrets/openbao/data`, preserve it during updates, and require encrypted
backup before a pinned image change. Verify post-update version/health and report
manual unseal; no automatic updates or in-place data downgrade. Plan v7 is current.

Implementation checkpoint, 2026-11-08: the worktree implements the accepted Lite
boundary, bind storage, explicit backup-gated updates and configurable cleanup.
Local tests/builds and disposable local Docker acceptance pass, including bind
persistence, raw Raft recovery and cleanup disabled past expiry. Native file auditing
is declared in server configuration; provisioning verifies it without enabling API
audit creation. Encrypted backup recovery is verified with age and fresh OpenBao bind
storage; restored registry, cleanup, values/history and a separate Traefik volume's
certificate are checked. ACME uses an offline generated certificate, so public
issuance/renewal, consumer/network acceptance and fresh-agent discovery remain pending.
The real backup-gated 2.7.1 → 2.7.2 update is verified, including a failed-backup gate,
manual unseal/restart, downgrade rejection, and restoration of the updater's exact
backup into fresh 2.7.1 storage. Other version transitions require their own validation. The current plan records evidence and
limitations. No live deployment or migration has taken place.
