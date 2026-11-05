# Existing Compose containers — v1

The user wants PlayWarden containers grouped under its project with dashboard
management, retaining all existing data. The live restored stack is being used by
M5. The user requires preserving that runtime and its current endpoints. No
container recreation, stop/start of user services, image changes, data migration,
volume changes, VM changes or public exposure is part of this integration.

## Design

Add explicit `existing_compose` registrations to a discovered project's recipe.
Each entry requires an exact Compose `project` and a relative `working_dir` within
that recipe's directory. Match both existing Docker labels; never infer ownership
from a container name. Resolve only discovered recipes, reject ambiguous claims
and fail closed for incomplete discovery. Keep these containers separate from
managed previews: no destroy, GC, share, Compose rebuild or automatic start.

Use existing full-ID graceful stop and authenticated CLI-backed jobs. Add start
for registered containers only, with fresh ownership/state checks and the same
per-container lock. Details and logs require fresh explicit registration, reusing
the existing masking and rendering. Public inventory remains allowlisted. Group
registered rows in their owning checkout; unrelated containers retain existing
stop-only capabilities. Read-only discovery must never mutate Docker.

## Ordered tasks and acceptance

1. Registration and authority: validate selectors, exact matching, unique ownership,
   discovery failure handling and project grouping. Tests reject wrong directories,
   unregistered containers, prefixes, conflicts and removed registrations.
2. Management and UI: start by immutable ID without creation, same lifecycle lock,
   confirmed graceful stop, registered details/logs and both project/container views.
   Test CLI/action/auth guards, stopped/running states and secret masking.
3. Documentation and verification: update recipe, CLI and dashboard documentation.
   Go tests/vet/format/build; synthetic browser checks; real disposable Docker
   start/stop/logs/details with retained data and unchanged container identity.
4. Deployment: preserve the deployed binary and remote recipe, record live IDs,
   start times, images, mounts, ports and secret hashes privately. Atomically deploy
   the verified binary, restart only devkit-web, and append the exact restored-stack
   registration while preserving all VM entries. Verify project/API association,
   health and unchanged runtime/storage/endpoints. Roll back binary/recipe on failure.

The stopped M5 database is a distinct stack and stays unregistered. Existing M5
worktree edits stay untouched. Original volumes and keys stay in place. UI actions
are available for the user, but live PlayWarden controls are not exercised during
this task. Normal preview migration and a new web endpoint are deferred while M5
uses the runtime. This replaces the initial migration strategy after live inspection;
it does not modify the milestone roadmap or its accepted security requirements.

## Verification checkpoint

- Registration/guard regressions observed failing before implementation, then passing.
- Full Go suite passes; vet, formatting and diff checks pass.
- Real disposable Docker test passes project grouping, masked details, logs,
  revoked access, anonymous action rejection, confirmed stop, authenticated start,
  same container ID/image/mounts/volume contents, and successful history events.
- Actual Chromium desktop/mobile checks pass anonymous reads, sign-in,
  cancel/confirm stop, start, status refresh, Details/Logs and both inventory views;
  no JavaScript errors or mobile horizontal overflow. The isolated fixture has no
  Traefik certificate, so its certificate warning is expected.
- Deployed source commit `43dea08` as `existing-compose-20261105` on devbox.
  The three healthy restored PlayWarden containers now appear under the project;
  existing VM entries and unrelated stopped M5 database remain unchanged.
- Private before/after evidence matches all PlayWarden IDs, start times, images,
  configuration hashes, mounts, published ports, networks and secret-file hashes.
  Only devkit-web restarted; no live PlayWarden or VM action was invoked.
- Read-only browser verification confirms the deployed project grouping, original
  UI/device endpoints, VM section and Details/Logs/sign-in hints. Its first check
  ran before the independently loaded VM inventory arrived; waiting for that
  existing asynchronous load passed. No application repair was needed.
- The isolated runtime fixture was signaled to clean up. Previous dashboard binary
  and exact host recipe are retained in the host's private devkit integration
  backup, alongside matching before/after snapshots and the verified receipt.

## Delivered scope

Existing-stack registration is complete. It does not add public sharing or a new
preview URL. Endpoint migration remains deferred per the user's active-M5 constraint.
Use the existing project controls only when intentionally operating that runtime;
future preview conversion must be coordinated with M5 and preserve its retained data.
