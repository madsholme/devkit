# Agent CLI expansion implementation plan — v1

> **For agentic workers:** Use `superpowers-extended-cc:executing-plans` to implement
> this plan task by task as a single agent. Do not spawn agents or model subprocesses
> without explicit user authorization. Track execution with native `update_plan`;
> the adjacent `.tasks.json` preserves the implementation tasks across sessions.

**Goal:** Deliver exact-preview logs, CLI inventories, a host-maintenance skill,
private-preview connectivity checks and structured history/share/GC reports.

**Architecture:** Reuse local collectors, exact identity/ownership checks and
`operationReport`; add bounded CLI adapters and typed projections. Share GC selection
and execution with its existing text command. Keep raw logs separate from reports
and distribute the new skill with the existing single-file installer.

**Tech stack:** Go 1.27.1 (mise), standard library only; existing Python Docker
acceptance runner and Go test seams; Markdown skill assets. No frontend changes.

**Spec:** [Agent CLI expansion design v1](2026-11-17-agent-cli-expansion-design-v1.md).
Read it with this plan; it fixes command semantics, output limits, network policy
and the intentionally bounded connectivity claim.

**Status:** All seven implementation/verification tasks are complete. Full Go checks,
trusted-TLS tests, six-skill distribution and full Docker-bridge workflow acceptance
pass. The loopback smoke path timed out on this host; evidence and the bounded probe
profile are recorded below. Runtime deployment and active-client skill installation
remain separate. [ADR 20](../adr/0020-agent-cli-observability.md) is accepted.

## Global constraints and decisions

**User decisions already made:** Plan all five additions from the preceding review;
use Superpowers; work as one agent; follow existing repository architecture and
plan conventions; preserve work and avoid unrelated changes. The subsequent user request authorizes implementation of this plan. Changing a
deployed host remains separate.

Implementation defaults are specified in the approved design. There are no
unresolved questions preventing an actionable plan. For implementation, reuse this
task worktree/branch if still suitable. Integration follows repository main/push
conventions after verifying the actual destination; do not create a PR or assume
`origin` is the user's development remote. Deployment remains separate.

- Keep old CLI text and JSON contracts, dashboard response shapes, legacy recipes,
  preview locks/recovery and vault authorization intact.
- Reports use one versioned JSON object and 0/1/2 exit codes, with unavailable data
  distinct from empty/zero. Extend invalid-invocation handling in `dispatch` for
  every newly supported JSON command. Do not serialize arbitrary errors or structs.
- Read-only commands never launch hooks, fetch Git, read secrets, evaluate credential
  sources or append mutation history. GC apply retains existing mutation history.
- Exact log/check targets stay inside the caller's Git common directory. Host-wide
  inventory only observes; action capabilities are revalidated by existing commands.
- Use disposable HOME/config/state and existing seams/fixtures. No real secret
  values, installed skill writes, deployed proxy changes or VM restarts in tests.
- Keep one authoritative plan for this workstream. The completed earlier operations,
  guidance and restore plans remain delivery records. Archive this v1 and create v2
  only for material requirements/architecture/strategy changes, not progress updates.

## Review focus

1. Foreign clones, duplicate services, deleted recipes and partial rebuilds must not
   redirect diagnostics to the wrong container. Tasks 1 and 4 own these tests.
2. Partial discovery, a first CPU sample and a renamed/recreated container must not
   become healthy empty inventory or a metric attached to the wrong resource.
   Tasks 2 and 3 own these tests.
3. DNS changes, foreign destinations, invalid certificates and redirects must not
   turn a diagnostic into unrestricted network access. Task 4 owns these tests.
4. Old event details and subprocess failures may contain credentials; reports must
   omit them, and interrupted GC must retain exact partial outcomes. Task 5 owns
   these tests.
5. Skill upgrades must preserve edits, and operator guidance must distinguish
   checksum checks, real restore rehearsal and authorized cutover. Task 6 owns
   distribution tests and decision scenarios; Task 7 records behavioral limits.

## Sequence and file responsibilities

| Task | Outcome | Dependencies |
|---|---|---|
| 1 | Exact-target retained-container logs and shared observation resolver | None |
| 2 | Local projects and containers reports | None; execute after 1 to avoid shared-file churn |
| 3 | One-shot resource report with reliable identities | 2 |
| 4 | Private primary-route check with trusted TLS evidence | 1 |
| 5 | Structured history, shares and GC partial results | 1–4 for consistent CLI/report integration |
| 6 | Sixth skill and updated agent workflows | 1–5 |
| 7 | Combined acceptance, documentation reconciliation and delivery | 1–6 |

Keep one owner and execute in numeric order. These are delivery boundaries, not
separate orchestration projects. Each task contains its tests and affected docs;
task 7 reconciles and verifies the combined result. No task is complete merely
because its test names or metadata exist.

### Task 1: Exact-preview logs

**Goal:** Read bounded logs from the exact owned live/review service, including a
retained stopped container, without granting new exec or arbitrary-container access.

**Files:** Create `cmd/devkit/preview_observe.go`, `cmd/devkit/preview_observe_test.go`,
`cmd/devkit/preview_logs.go`, `cmd/devkit/preview_logs_test.go`; modify
`cmd/devkit/main.go`, `cmd/devkit/preview.go`, `README.md`, `docs/previews.md`.
Reuse `docker.go`, `repo.go`, `naming.go`, `overview.go` and existing test fixtures.

**Interfaces:** In `preview_observe.go`, define
`resolveObservedPreview(ctx context.Context, name string) (observedPreviewTarget, error)`
and `selectObservedService(target observedPreviewTarget, service string) (container, error)`.
The internal target holds ID, common directory, mode, private URL and observed
containers; never serialize it directly. Empty name selects the current live
identity; empty service selects the observed web container. In `preview_logs.go`,
define `logsCLI(args []string) (int, error)` and
`runPreviewLogs(ctx context.Context, id string, opts previewLogOptions) error`.
The options hold tail count, normalized since time and follow mode.

**Acceptance Criteria:**
- [x] No-selector behavior stays current-live/default-web; explicit ID reaches an
  owned review or sibling-worktree preview, including stopped/exited containers.
- [x] Foreign/conflicting ownership, duplicate service candidates and removed
  containers fail before Docker logs; edited/missing recipe does not block an
  otherwise verified explicit target. Existing exec behavior is unchanged.
- [x] Tail/since limits, 15s/4 MiB non-follow bounds and follow cancellation match
  the design. Invalid input exits 2; log/process failures exit 1.
- [x] No start/recreate, hook, secret lookup or history write occurs.

**Verify:** `go test ./cmd/devkit -run 'TestPreviewObserve|TestPreviewLogs|TestLogsRejectAnythingButAPreviewID|TestRefuseAnotherClone' -count=1` → PASS, including zero forbidden calls.

**Steps:**
- [x] Add table cases `TestPreviewObserveOwnership`, `TestPreviewObserveAmbiguousService`,
  `TestPreviewObserveMissingRecipe`, `TestPreviewLogsStoppedReview`,
  `TestPreviewLogsArgumentLimits`, `TestPreviewLogsOutputLimitAndCancel` using
  `newGitRepo`, `labelled`, `swap` and `runCommand`-style seams. Pin selected full IDs,
  zero lifecycle calls, exact argument arrays, return codes and raw-log preservation.
  Include malformed/negative/excessive tail, duration/RFC3339 normalization, foreign
  clone, duplicate web, absent container and rebuild candidate ambiguity.
- [x] Run the focused command and observe the intended missing-command/behavior
  failures. Implement the resolver and bounded reader without changing `execLive`.
  Reuse existing ID validation/legacy naming; do not broaden dashboard authority by
  swapping its resolver wholesale. Revalidate the full ID immediately before logs.
- [x] Wire flags/help, document raw-log privacy and missing-container limits, rerun
  the focused command and commit the coherent change as `Add exact preview logs`.

### Task 2: Project and container inventory reports

**Goal:** Expose existing discovery from the CLI without a running web server, with
safe fields, stable ordering and honest incomplete evidence.

**Files:** Create `cmd/devkit/inventory_cli.go`, `cmd/devkit/inventory_cli_test.go`;
modify `cmd/devkit/main.go`, `cmd/devkit/projects.go`,
`cmd/devkit/container_inventory.go`, `cmd/devkit/projects_test.go`,
`cmd/devkit/container_inventory_test.go`, `README.md`, `docs/projects.md`.
Reuse `operation_report.go`, `container_projects.go` and bounded probe helpers.

**Interfaces:** `projectsCLI(args []string) (int, error)`,
`containersCLI(args []string) (int, error)`,
`projectsReport(ctx context.Context) operationReport`, and
`containersReport(ctx context.Context) operationReport` live in `inventory_cli.go`.
If collector completeness needs extraction, use
`collectProjects(ctx context.Context) (projectsData, []operationCheck, error)` and
`collectContainers(ctx context.Context) (containersData, []operationCheck, error)`
behind existing web wrappers. Maintain their old shapes; map typed checks to the
old warnings for HTTP rather than parsing free-form warning strings for CLI codes.

**Acceptance Criteria:**
- [x] Both commands run outside Git and without web, expose the design's allowlist,
  preserve existing grouping/registration and sort by identity on equal names.
- [x] Missing Docker returns retained project facts plus incomplete evidence/exit 1;
  conflicting registration removes unsupported capabilities. Empty is not unavailable.
- [x] A 30s command budget and existing inventory caps cancel subprocesses and
  preserve partial evidence; no hidden hook, fetch, secret or mutation occurs.
- [x] Existing HTTP contracts and exact capability checks continue passing.

**Verify:** `go test ./cmd/devkit -run 'TestInventoryCLI|TestProjects|TestProjectCheckout|TestContainerInventory|TestExistingContainer' -count=1` → PASS; JSON parses as one object on success, invalid invocation and collector failure.

**Steps:**
- [x] Add `TestInventoryCLIOutsideCheckout`, `TestInventoryCLIPartialDocker`,
  `TestInventoryCLIConflictingRegistration`, `TestInventoryCLIStableSafeProjection`
  and `TestInventoryCLIDeadline`. Assert full IDs/grouping, `complete == false`
  on unavailable evidence, no sentinel environment/argv/error strings, no invented
  permissions, and cancellation reaching the child. Run to establish failures.
- [x] Implement adapters and small shared collector extraction where necessary.
  Propagate unavailable project-directory/Git/registration evidence rather than
  hiding existing swallowed errors in the new complete report. Keep old UI output
  structurally compatible; no broad inventory redesign or new cache.
- [x] Wire command/JSON-invocation handling, update docs, rerun focused tests and
  commit as `Expose project and container inventories`.

### Task 3: One-shot resource sampling

**Goal:** Report bounded host/container resource use with real sample timing and
stable container identity, without starting the web sampler.

**Files:** Create `cmd/devkit/resources_cli.go`, `cmd/devkit/resources_cli_test.go`;
modify `cmd/devkit/resources.go`, `cmd/devkit/main.go`, `README.md`,
`docs/dashboard.md`. Reuse `/proc` parsers and Docker stats parsing.

**Interfaces:** `resourcesCLI(args []string) (int, error)` and
`resourcesReport(ctx context.Context) operationReport` in `resources_cli.go`;
`sampleResourcesOnce(ctx context.Context) (resourceSnapshot, []operationCheck)`
returns a typed CLI snapshot: measured interval/timestamp, nullable host metrics
and container samples keyed by full ID. Keep any shared parsing extraction small;
the existing dashboard loop and its output remain unchanged.

**Acceptance Criteria:**
- [x] CPU uses two samples 250 ms apart; absent/invalid deltas remain null with a
  check. Memory/swap/disk zeros remain valid numbers, not missing values.
- [x] One 10s deadline bounds sampling and child output. No sampler goroutine leaks
  or dependency on a web process; no process command lines enter output.
- [x] Docker stats join exact IDs, not names; replacement/disappearance yields
  unknown/omitted association with a check, never a stale owner.
- [x] Docker/proc failure retains other evidence and returns incomplete/exit 1.

**Verify:** `go test ./cmd/devkit -run 'TestResourcesCLI|TestResourceSample' -count=1` → PASS with deterministic clock/probe fixtures and real cancellation coverage.

**Steps:**
- [x] Add `TestResourceSampleCPUDelta`, `TestResourcesCLIPartialMetrics`,
  `TestResourcesCLIContainerReplacement`, `TestResourcesCLIDeadline` and
  `TestResourcesCLINoProcessArguments`. Assert interval, null versus numeric zero,
  exact IDs, stable ordering and no background-loop startup. Run to show failures.
- [x] Extract only reusable sample/parsing pieces. Request full IDs from bounded
  Docker stats and match the observed inventory; keep a test seam for time/probes.
  Do not call `newResources()` or return its initially empty history snapshot.
- [x] Wire CLI/report errors, document metric units and host filesystem scope,
  rerun focused tests and commit as `Add one-shot resource reports`.

### Task 4: Private preview connectivity checks

**Goal:** Diagnose the selected private primary route through observed service,
router, DNS, TCP, trusted TLS and HTTP evidence without changing runtime state.

**Files:** Create `cmd/devkit/preview_check.go`, `cmd/devkit/preview_check_test.go`;
modify `cmd/devkit/main.go`, `README.md`, `docs/previews.md`, `docs/testing.md`.
Reuse task 1 resolver, `docker.go` route conventions, `hostconfig.go`, `proxy.go`
listener rules and address discovery in `doctor.go`/`vms.go`.

**Interfaces:** `checkCLI(args []string) (int, error)`,
`checkPreview(ctx context.Context, target observedPreviewTarget) operationReport`,
and `probePreviewEndpoint(ctx context.Context, endpoint previewEndpoint) endpointObservation`.
`previewEndpoint` holds the validated primary HTTPS hostname and exact approved
IP set; `endpointObservation` contains only attempted/selected IPs, stage status,
numeric HTTP status and elapsed times. Transport/DNS/trust seams are replaceable
in tests, never exposed as insecure or arbitrary-destination CLI flags.

**Acceptance Criteria:**
- [x] Timeout parsing/defaults, primary-route scope and all result classifications
  match the design. Output always declares host vantage and no application proof.
- [x] Ownership/observed router evidence is required; edited recipes, foreign
  labels and unsupported router expressions cannot invent a valid route.
- [x] All DNS answers are validated before dial; exact private listener matching,
  DNS pinning, certificate verification, proxy disabling and no redirects hold.
- [x] Real trusted-TLS fixture succeeds; wrong-host/untrusted certificates, foreign
  addresses and a redirect target never receive unauthorized application requests.
- [x] No mutation, secrets, hooks or health-command execution; partial rebuild
  records remain intact and remote-client reachability remains unverified.

**Verify:** `go test ./cmd/devkit -run 'TestPreviewCheck' -count=1` → PASS, including local real TCP/TLS/HTTP exchanges and zero forbidden dials/mutations.

**Steps:**
- [x] Write `TestPreviewCheckPrimaryRouteAndHealth`, `TestPreviewCheckSavedObservedDrift`,
  `TestPreviewCheckDNSPinningAndPrivatePeers`, `TestPreviewCheckTLSVerification`,
  `TestPreviewCheckHTTPClassification`, `TestPreviewCheckTotalDeadline` and
  `TestPreviewCheckRecoveryReadOnly`. Table-test the design's entire classification
  table, missing health, non-web one-shot ambiguity, multiple addresses and the
  eight-address bound. Assert bodies/headers/errors containing sentinels are absent.
- [x] Build an in-process HTTPS fixture with a generated test CA and matching SAN;
  pass its trust and ephemeral listener through test-only dependencies while running
  the production probe path. Assert 2xx passes and wrong SAN/untrusted CA fail;
  a second redirect server gets zero requests, proxy traps get zero connections,
  and a changed DNS answer is never used after validation. Run to show failures.
- [x] Implement bounded observed-route parsing and stage-specific reports using
  task 1. Pin approved dials, preserve SNI, disable redirects/proxies, bound headers
  and close unread bodies. Do not reuse doctor's diagnostic TLS mode as an insecure
  transport or mistake existing acceptance `curl -k` for TLS verification.
- [x] Wire CLI/error envelope, document host-only scope and warning semantics,
  rerun focused tests and commit as `Check private preview connectivity`.

### Task 5: Structured events, shares and GC results

**Goal:** Add safe JSON observations and report precise GC batch outcomes while
preserving the existing destructive operation and concurrency contract.

**Files:** Create `cmd/devkit/operational_reports.go`,
`cmd/devkit/operational_reports_test.go`; modify `cmd/devkit/main.go`,
`cmd/devkit/history.go`, `cmd/devkit/share.go`, `cmd/devkit/preview.go`,
`cmd/devkit/history_test.go`, `cmd/devkit/devkit_test.go`, `README.md`,
`docs/previews.md`, `docs/shares.md`. Reuse `preview_lock.go` and recovery guards.

**Interfaces:** `historyReport(preview string, limit int) operationReport`,
`sharesReport(now time.Time) operationReport`, `gcCLI(args []string) (int, error)`.
Extract `planGC(root, common string) (gcPlan, error)` and
`applyGC(plan gcPlan) (gcResult, error)` for both text and JSON. `gcPlan` owns exact
candidate/kept identities; `gcResult` owns ordered target outcomes and safe warnings.
The existing `gc(apply bool) error` may stay as a compatibility wrapper for callers.
Do not build a second mutation implementation for JSON.

**Acceptance Criteria:**
- [x] History ordering/default/filter/rotation remain compatible; JSON excludes
  free-form detail and normalizes actors. Shares remain local observations with UTC
  expiry/null and warnings, no cloud queries or raw backend identifiers.
- [x] GC dry-run is side-effect free and matches current selection, including
  legacy branch mapping and unknown sources; no new reclaimability guarantees.
- [x] Apply revalidates under existing locks/guards. A later failure reports earlier
  destroyed targets and remaining not-attempted targets; no replay or fake rollback.
- [x] Unshare warnings, partial destructive failures and history events remain
  accurate. Both successful and failed JSON applications emit exactly one object.

**Verify:** `go test ./cmd/devkit -run 'TestOperationalReports|TestHistory|TestGC|TestPreviewLock|TestPreviewRebuildRecovery|TestReap|TestShare' -count=1` → PASS; old lifecycle/lock/history behavior remains covered.

**Steps:**
- [x] Add `TestOperationalReportsHistoryProjection`, `TestOperationalReportsShares`,
  `TestOperationalReportsGCParity`, `TestOperationalReportsGCPartialFailure`,
  `TestOperationalReportsGCRevalidation` and `TestOperationalReportsJSONFailures`.
  For three sorted candidates, make the second destroy fail: assert outcomes equal
  `destroyed, failed, not_attempted`, only two mutation attempts, one success event,
  no first-target replay and preserved third-target files. Test unshare warning plus
  subsequent success, owner/source drift, recovery block, lock conflict and unsafe
  sentinel errors; run the focused command to establish failures.
- [x] Add allowlisted projections and extract selection/application minimally.
  Separate presentation from mutation, route child output away from JSON stdout,
  and report only safe operation summaries. Preserve `noted`/`happened` semantics
  without recording failures twice. A failed target may already have side effects.
- [x] Wire new JSON modes and usage errors, update docs, rerun focused regression
  tests and commit as `Add structured operational reports`.

### Task 6: Host skill and workflow integration

**Goal:** Distribute `devkit-host` as the sixth skill and teach existing skills to
use the new commands with correct authority, privacy and evidence boundaries.

**Files:** Create `cmd/devkit/agent-assets/devkit-host/SKILL.md`; modify
`cmd/devkit/agent_skills.go`, `cmd/devkit/agent_skills_test.go`,
`cmd/devkit/agent-assets/devkit-previews/SKILL.md`,
`cmd/devkit/agent-assets/devkit-troubleshooting/SKILL.md`,
`cmd/devkit/agent-assets/devkit-projects/SKILL.md`,
`cmd/devkit/agent-assets/devkit-secrets/SKILL.md`,
`project-template/AGENTS.md`, `project-template/CLAUDE.md`,
`tests/agent_workflow_scenarios.txt`, `tests/agent_secrets_acceptance.py`,
`docs/agent-skills.md`, `docs/agent-scenarios.md`, `docs/setup.md`,
`docs/secrets.md`, `README.md`, `AGENTS.md`.

**Interfaces:** Existing `bundledAgentSkills`, `installProjectAgentInstructions`,
version/hash manifest, managed block and selected-agent contracts. New catalog
entry is `devkit-host`, version `1`; bump only changed existing assets. Preserve
the single-file installer and existing Go standard-library dependency boundary.

**Acceptance Criteria:**
- [x] Fresh install/status/repair/setup/doctor include all six skills for both
  clients, and all six managed blocks/templates are consistent.
- [x] Five-to-six upgrades preserve edited assets/blocks, modes, unrelated content,
  selected-agent isolation and supported `CLAUDE.md -> AGENTS.md` alias behavior.
- [x] Host skill covers the design's operator workflow and matching-release docs;
  app secret use does not trigger vault/host administration.
- [x] Existing workflows use exact logs/inventories/check/reports without claiming
  raw-log secrecy, remote-client proof, extra action authority or recovery approval.
- [x] Scenario definitions distinguish installed-file checks from actual agent
  behavior; no model session is launched without user authorization.

**Verify:** `go test ./cmd/devkit -run 'TestAgent|TestManagedAgentSkills|TestWorkflowSkills|TestProjectTemplates|TestProjectGuidance|TestHostSkill' -count=1`; `go build -o bin/devkit ./cmd/devkit`; `python3 tests/agent_secrets_acceptance.py --devkit bin/devkit` → PASS; the distribution runner writes only to its disposable directories.

**Steps:**
- [x] Add `TestHostSkillCatalogUpgrade`, `TestHostSkillSelectedAgentAndEdits` and
  `TestHostSkillTemplatesAndDoctor` using existing installer fixtures; establish
  missing-sixth-skill failures. Extend the existing acceptance runner's asset tuple
  to include `devkit-host`; do not invent a new installer runner.
- [x] Write the host skill, add its catalog/block and route explicit operator work
  from the secrets/troubleshooting skills. Update preview/project examples and
  changed asset versions; regenerate managed template block hashes through existing
  machinery instead of hand-inventing them. Update installation counts everywhere
  current behavior is described, preserving historical evidence about five skills.
- [x] Add scenarios: crashed review logs; Docker-down inventory; foreign-DNS refusal;
  HTTP redirect warning; GC second-target failure; sealed vault operator handoff;
  backup checksum versus rehearsal; interrupted restore with token revocation and
  retained data. Define expected decisions/tool calls and forbidden mutations.
- [x] Run focused tests, existing single-file skill validator where available and
  disposable distribution acceptance; record live Codex/Claude scenarios as unrun
  unless separately authorized. Commit as `Add host maintenance guidance`.

### Task 7: Combined acceptance and delivery

**Goal:** Verify the delivered CLI workflows together, reconcile documentation with
actual behavior and hand off an honest implementation/deployment state.

**Files:** Modify `tests/runtime_acceptance.py`, `docs/testing.md`,
`docs/agent-scenarios.md`, this plan and its `.tasks.json`, and the proposed
`docs/adr/0020-agent-cli-observability.md` when its decision is accepted.
Reconcile affected README/command/skill docs from tasks 1–6.

**Acceptance Criteria:**
- [x] Existing runner demonstrates distinct live/review log markers, retrieval
  after stop and no container/data mutation from diagnostics.
- [x] Disposable orphan GC JSON reports exact targets/outcomes; surviving preview
  volume/data sentinel remains unchanged. Foreign clone and unknown source stay safe.
- [x] Trusted-TLS local fixture proves production network validation; existing
  insecure smoke curl is not cited as certificate evidence.
- [x] Go tests/vet/build, formatting/diff checks and disposable distribution/runtime
  checks pass, or concrete blockers remain explicitly unfinished.
- [x] Docs/spec/task status reflect delivery; no deployment or fresh-model evidence
  is claimed without execution. Worktree cleanup waits for verified integration.

**Verify:** `go test ./...`; `go vet ./...`; `gofmt -l cmd/devkit`; `git diff --check`;
`go build -o bin/devkit ./cmd/devkit`; then, on an authorized disposable Docker
environment, `DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py` and
`python3 tests/agent_secrets_acceptance.py --devkit bin/devkit` → all required
checks pass, formatting/diff checks have no output, fixtures clean up their resources.

**Steps:**
- [x] Extend the existing runtime runner at its live/review/stopped-preview points;
  use unique test-owned resource IDs, temporary config/state and existing teardown.
  Add bounded log assertions before destruction, and structured GC assertions with
  a surviving data sentinel. Do not add real Cloudflare, vault or VM dependencies.
- [x] Run an early representative log/check fixture as soon as task 4 is ready;
  retain valid evidence for unchanged paths. At this task run combined acceptance
  and broad Go checks once. Failures after two repair rounds require reassessment
  of the shared contract, not another narrow harness patch.
- [x] Review the diff against the design and all five review-focus risks as one
  agent. Reconcile help/examples, JSON shapes, limits, installed asset counts and
  limitations; record actual commands/results and unrun checks. No frontend build
  is needed if frontend/web assets remain untouched.
- [x] Commit the final tests/docs as `Verify agent CLI workflows`. At authorized
  implementation delivery, report branch/worktree, commits and verification;
  integrate/push according to the verified project remote convention. Remove only
  task-created clean worktrees/fully merged branches after confirmed integration.
  Leave runtime deployment and installed-skill rollout for explicit host scope.

## Plan review and resume state

All five requested additions map to tasks: logs → 1; inventory → 2–3; host skill → 6;
connectivity → 4; structured reports → 5. Task 7 verifies their combined outcome.
The planned test names are new requirements, not evidence that those tests exist
or pass. Existing useful seams and regression tests are named above.

Task worktree: `/home/mads/.codex/worktrees/agent-cli-expansion/devkit`.
Branch: `codex/agent-cli-expansion`; base `5a1aa86`. Tasks 1–6 are committed in
`b5bdd2c`, `b760b65`, `f25dfd4`, `5ae7bc2`, `48ba71c` and `e0d4a40`.

The final review retained shared GC selection/execution, ownership/registration
checks, private pinned TLS probes and single-file skill distribution. It corrected
the reused CPU parser's double-counting of guest time and malformed/overflow input,
and added actual nested-root registration-conflict coverage. These are focused
contract corrections, not a new implementation strategy or plan version. CLI project
`live_recipe` means declared recipe presence, not launch authorization; Git
synchronization details are intentionally omitted from the read-only projection.

Verified: full `go test ./...`, `go vet ./...`, binary build, formatting/diff checks,
trusted local TCP/TLS/HTTP tests, all changed skill validators and disposable
`tests/agent_secrets_acceptance.py`. Go commands use
`GOCACHE=/tmp/devkit-agent-cli-go-cache` because the default cache is read-only.
No frontend build is needed: no frontend/web assets changed.

The full runtime command
`DEVKIT_RUNTIME_TEST=1 DEVKIT_PROXY_PROBE=bridge python3 tests/runtime_acceptance.py`
passed, including exact live/review log isolation, stopped-container log retrieval,
GC JSON selection/application and surviving container/volume/SQLite identities.
Existing rebuild, build-failure, interrupted recovery and stopped-staging checks also
passed. All fixture containers/volumes were removed; image caches remain.

The initial daemon-access failure was transient. Once Docker was reachable outside
the sandbox, the default smoke probe connected to its published loopback port but
stalled during TLS. The same proxy/Host route returned HTTP 200 from its fixture
bridge. The original run was interrupted and cleaned up, then the same runner was
rerun with an explicit bridge probe profile and bounded request/connect timeouts.
This bounded platform profile does not change product networking, lifecycle tests,
certificate verification requirements or the design strategy. Published-loopback and
remote LAN/VPN reachability are not established by bridge success. The real local
trusted-TLS Go fixture provides certificate/hostname validation evidence; neither
insecure runtime smoke profile does. Logs remain at
`/tmp/devkit-agent-cli-runtime.log` and `/tmp/devkit-agent-cli-runtime-bridge.log`.

Fresh Codex/Claude model scenarios remain unrun under the single-agent instruction;
installed-file distribution is separate evidence. No deployed binary/service or
active-client skill assets were changed. The useful next operational step is an
explicit binary/guidance rollout, followed by actual client route verification.

Integration follows the repository's main/push convention. The user's `fork` main
was verified at the implementation base `5a1aa86`; configured `main` tracking says
`origin`, so push explicitly to `fork` and never infer the destination from tracking.
Confirm integrated/pushed identity before archiving only this task-created worktree.
Final handoff records the resulting commit and cleanup state.
