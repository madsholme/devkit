# Previews

## Three modes

- Live: source bind mount, independent task data. The sample rereads `message.txt`
  on every request. Python code edits need a restart; adapt the recipe to your
  real application's hot-reload server. Creating a live preview builds its Compose images from
  the current files; resuming one does not, so after changing a Dockerfile or dependencies,
  inspect `devkit rebuild PREVIEW` and apply a supported update with `--yes`.
- Local review: a detached checkout of an exact commit supplies build inputs.
  The web image is resolved to a Docker image ID; no review service may bind-mount
  host files. The temporary checkout is removed after the build. Test data remains
  mutable. Repeating review for the same commit resumes the existing artifact.
- Team review: explicit deployment through the project's `publish` hook, e.g. to
  Hetzner/Dokploy. The adapter needs destination configuration; see [publishing](publishing.md).

Live and review services get `restart: unless-stopped`, so running previews come back after a
host reboot (with Traefik, which uses the same policy); stopped ones stay stopped. Previews saved
without this policy gain it the next time `devkit live`, `devkit review` or `devkit start` brings
them up.

Commit the project recipe before using `review`; it reads configuration from the
selected commit. Generated inputs should be deterministic and dependencies locked.
This v1 sample does not initialize submodules or Git LFS automatically.

## Lifecycle and clean-up

Run lifecycle commands inside the owning Git repository. `ls` works anywhere.
Without a preview ID, `stop`, `start` and `destroy` act on the preview that belongs
to the current worktree: its live preview, or a review started from it. If several
match (e.g. a live and a review preview), devkit lists them and asks for an ID.
Each preview records the worktree it was started from in its `devkit.worktree` label.
To remove a finished worktree, run `devkit destroy` in it and then remove it with
your tool or `git worktree remove <path>`; Git refuses removal of uncommitted work.
If a tool already deleted the worktree, `devkit gc` lists the orphaned previews and
`devkit gc --yes` destroys them (containers, networks, volumes and run dirs). It
only considers previews of the current repository and never touches a preview
whose worktree still exists. Review previews created before devkit recorded
`devkit.worktree` are never collected; destroy them by ID.
Branches are retained until you explicitly delete them.

Every review is a fresh build, so build cache and images pile up as previews come
and go. `devkit prune --yes` trims the Docker build cache to 30 GB and removes images
older than a week that no container, running or stopped, uses. It works host-wide,
not only on devkit images, and never touches volumes. The web service runs it once a
day; without it, run it yourself. `devkit prune` alone shows `docker system df`.
The limits and the daily run are set in the host config's `prune` section.

## References

- https://git-scm.com/docs/git-worktree
- https://docs.docker.com/compose/how-tos/project-name/

## Inspect and rebuild a live preview

`devkit context --json` distinguishes current source HEAD/dirty state, saved preview
configuration and observed container image IDs. `devkit ls --json` provides full
commit IDs for managed previews. Saved configuration is not evidence that a runtime
is running or healthy. Reports are snapshots; mutations always resolve again.

After editing a Dockerfile or supported Compose settings, inspect:

```sh
devkit project validate --runtime containers --json
devkit rebuild PREVIEW --json
# Apply when the requested work authorizes this update:
devkit rebuild PREVIEW --yes --json
```

Rebuild retains the live preview ID, URL, Compose project and storage. It accepts
only the owning checkout and unchanged service topology, mounts, volume definitions,
networks and routing. It builds services with `build` declarations using unique
operation tags, then pins resolved image IDs. It refuses anonymous mounts, changed
unbuilt service images/startup environment, credential-like environment changes,
remote/out-of-checkout build contexts, additional build contexts/tags/cache exports/SSH forwarding, active shares and incomplete
ownership evidence. Existing media allocation must match exactly; no ports are
allocated. Review previews and existing-stack registrations cannot be rebuilt.

No prepare/check/build/publish hook executes. Compose builds do execute Dockerfiles;
new images or app startup may migrate database contents. Storage retention is not
an application transaction, backup or rollback guarantee. Builds use the current
worktree; reported HEAD and dirty state do not promise reproducibility.

A running preview is recreated without deleting volumes and must pass health checks.
A stopped preview receives the staged configuration and remains stopped; verify it
with an explicit `devkit start PREVIEW` later. Ordinary `live`/`start` retain their
saved-configuration resume behavior. Rebuild output suppresses potentially sensitive
build diagnostics; inspect the relevant Dockerfile and use an explicitly authorized,
private debugging workflow when a build fails.

### Interrupted rebuilds

Each preview has a nonblocking lifecycle lock shared with live/start/stop/destroy,
share/unshare, expiry and GC. Busy expiry retries without removing its share record.
Locks protect Devkit operations; manual Docker commands are outside this lock.

Private recovery evidence is stored under
`$DEVKIT_STATE_DIR/rebuilds/PREVIEW/` (default
`~/.local/state/devkit/rebuilds/PREVIEW/`), owner-only. `operation.json` retains old
and candidate models, source/input fingerprints and image IDs; `candidate.json` is
the staged Compose model. These contain environment values: do not paste them into
chat, logs or Git. The latest operation replaces the previous completed record.

A failed build leaves saved configuration and existing containers untouched. A
failed apply can leave old and candidate containers together. Devkit never rolls
back database/application state automatically. Inspect:

```sh
devkit rebuild PREVIEW --recover --json
# Retry the same built candidate after resolving its external health blocker:
devkit rebuild PREVIEW --recover --yes --json
```

Recovery never rebuilds or reruns hooks. It requires unchanged checkout identity,
HEAD, Compose/include/env inputs, effective model and owned storage. Ordinary
resume/share/destroy is blocked while recovery is pending; exact graceful stop
remains available and records the stopped state. Recovery after a stop stages the
candidate without restarting it.

If a build never produced a candidate, or recorded inputs have changed, automatic
recovery is unavailable. An operator must privately inspect the phase, old/candidate
image IDs, actual containers and volume identities. Preserve artifacts and take an
appropriate backup before choosing forward repair or rollback compatibility. Only
after establishing a consistent saved/runtime state and obtaining authority for the
chosen recovery may the operator archive/remove that exact operation record.
Removing a record is not itself a repair, and `destroy` is not a recovery shortcut.

## Exact preview logs

`devkit logs` selects this checkout's live web container, including a retained
stopped container. Use `devkit logs --preview ID -s SERVICE --tail 200 --since 1h`
for an exact live or review preview in the same Git repository. Foreign clones and
ambiguous service replicas are rejected; removed containers cannot supply logs.
Explicit preview selection uses observed metadata even if the recipe has changed.

Tail accepts 1–10000 lines (default 200). Since accepts a positive duration up to
720h or a past RFC3339 timestamp with a zone. Non-follow output is capped at 4 MiB
and 15 seconds; truncation/failure returns nonzero. Explicit `-f` streams until
cancelled. Logs are raw application output and may contain credentials: inspect
before sharing, and never treat them as sanitized report metadata. No container is
started or recreated, and `exec` remains restricted to the live preview.

## Check a private preview route

`devkit check PREVIEW --json` observes the exact owned preview's services and primary
private HTTPS route from this host. Without an ID it selects the current live
preview. It validates the observed web router, resolves DNS once, verifies that
every destination is a configured private local proxy listener, and probes TCP,
trusted TLS and HTTP without credentials, redirects or environment HTTP proxies.
The default total timeout is 15s; `--timeout` accepts 1s through 60s. At most eight
DNS addresses are accepted. No service or recovery record is changed.

Reports identify `vantage: host`, `scope: primary-route` and
`application_verified: false`. HTTP 2xx passes; redirects and 401/403 warn that
the authentication/redirect flow is untested; other 4xx/5xx fail. Missing health
checks warn rather than implying application health. Stopped/paused web services
fail and skip network probes. Additional paths/subdomains and remote LAN/VPN client
access are not checked. Missing or conflicting identity, router, listener or DNS
evidence fails closed. Response bodies, headers and raw errors are omitted.
