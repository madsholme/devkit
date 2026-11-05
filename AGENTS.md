# Agent notes for devkit

Read README.md for what the kit does and `docs/` for how each part works. On a host that runs it, `~/.config/devkit/STATUS.md`
may record that host's state; it is not part of the repository.
This file covers what you need to change the kit itself.

devkit was called devbox until 2026-11. Projects may still have `devbox.json` and read
`DEVBOX_*` variables; `configFile` and `withEnv` keep those working. Keep that until the
old names are dropped on purpose.

## Layout

- `cmd/devkit/`: the whole CLI in Go, standard library only. Keep it that way.
  `main.go` parses commands; `preview.go` has live/review/lifecycle/gc/ls/publish/
  exec/logs; `repo.go` Git, config and saved previews; `naming.go` preview IDs;
  `docker.go` inspect data; `overview.go`, `resources.go` and `server.go` the web
  backend. Public shares: `share.go` (lifecycle, expiry, leftover sweep),
  `cloudflare.go` (API client) and `hostconfig.go` (`~/.config/devkit/config.json`: domain,
  harness, proxy, web, share, and secret sources). Host setup: `setup.go` (`devkit setup`),
  `proxy.go` (`devkit proxy up` renders and starts Traefik) and `doctor.go` (`devkit doctor`).
  `vms.go` discovers active QEMU/libvirt VMs read-only (`devkit vms`, `/api/vms`):
  allowlisted metadata only, bounded probes, UUID deduplication and private SSH connection commands.
  `vm_projects.go` matches explicit project `vms` selectors (UUID before name), discovers recipes
  from projects_dir/Docker/harness/cwd and host `vm_projects` roots, and leaves ambiguous claims unassigned.
  `projects.go` discovers direct project folders from host `projects_dir`, groups Git checkouts
  by common directory, and serves the independent `/api/projects` tree with launch capabilities.
  Discovery never runs hooks; authenticated `up` actions revalidate Git membership and recipe.
  `git_control.go` provides authenticated, CLI-backed origin fetch/sync/push with
  checkout revalidation, clean-state checks, bounded commands and per-repository locking.
  `container_inventory.go` provides all-host Docker inventory with allowlisted
  metadata; `container_control.go` adds confirmed graceful stop and registered start by full ID.
  `container_projects.go` maps explicit `existing_compose` project/directory pairs, with fresh
  unique ownership checks for start/details/logs. Registration never recreates containers;
  unrelated containers retain stop-only authority.
  `vm_control.go` provides opt-in launcher/libvirt lifecycle adapters, stopped managed inventory,
  exact control resolution and a host-wide lifecycle lock. Plain mappings do not authorize adapters. `vm_shutdown.go` separately grants verified native stop-only capabilities to discovered QMP/libvirt instances.
  `vm_cleanup.go` implements CLI-only exact-name/project QEMU cleanup, dry-run by default;
  stop adapters precede recipe removal, and opt-in `control.purge` owns data deletion.
  Stopped plain registrations may be removed; running plain mappings block cleanup.
  Preserve unknown recipe JSON and mode, reject symlinks/conflicts/observed concurrent edits,
  preflight the whole batch, and retain failed/unattempted registrations.
  `pair.go` issues T3 Code pairing links (`devkit pair`, `/api/pair`) and `qr.go` draws them as QR
  codes (its own small encoder; tests pin output that was checked with a real decoder).
  `history.go` is the event log (`~/.local/state/devkit/history.jsonl`). Package variables such as `runCommand`, `dockerContainers` and
  `repository` are the seams tests replace.
- `bootstrap.sh`: installs the system prerequisites (Docker, Tailscale, docker group,
  firewall for tailscale0, lingering) on Ubuntu/Debian (and derivatives) and Fedora with sudo. POSIX sh, idempotent.
  Optional `--qemu` installs native QEMU/KVM, libvirt, UEFI and TPM prerequisites. devkit itself never uses sudo.
- `bundle.sh`: builds `bin/devkit-linux-<arch>.tar.gz` (static binary, bootstrap.sh, template,
  README, docs, LICENSE) for hosts without Go; `DEVKIT_VERSION` stamps a release tag into the binary.
  `.github/workflows/release.yml` runs it for amd64 and arm64 on a `v*` tag and publishes a
  GitHub release; a manual run only builds the bundles as an artifact.
- `bin/devkit`: the built binary (gitignored). Build with
  `go build -o bin/devkit ./cmd/devkit`; the Go version is pinned in `mise.toml`.
- `frontend/`: Vue 3 dashboard using SwiftDesign components and themes; SwiftConfig is
  the layout reference. `src/dashboard.ts` owns API/state/action orchestration; `src/views/`
  holds the six global views plus project detail and `src/components/` shared controls/dialogs.
  `src/project-inventory.ts` joins exact container/worktree identities and server-provided VM
  worktree keys for project detail tabs. These read-only associations never grant action authority.
  `pnpm --dir frontend build` type-checks and generates committed `web/dist/` assets.
  `web/web.go` embeds those assets and icons; Go-only builds use the checked-in output.
  `frontend/vendor/README.md` records the pinned SwiftDesign package provenance.
  `web/logo.svg`, `favicon.ico` and `apple-touch-icon.png` are the CRT skin of the
  devkit logo; `loadAssets()` only publishes embedded production files in its `assets` map.
- The proxy (Compose project `devkit-proxy`) is generated by `proxy.go` into
  `~/.config/devkit/proxy/`: its file provider routes devkit.<domain> to the dashboard and
  the harness host (t3.<domain> here) to the harness; previews are routed by Docker labels.
  Traefik listens on the Tailscale IP, plus any `proxy.listen` addresses (`listenAddresses`); routers
  name no entrypoint, so they answer on all of them. `setup.go` writes the `devkit-web` user unit.
  "Edge" is kept free for a possible public edge that could replace Cloudflare for shares;
  don't use it for the private proxy.
  Why Traefik and not Caddy: it routes previews from Docker labels and has lego's DNS
  providers for the wildcard certificate built in, so devkit runs the stock, pinned image.
  Caddy needs plugins (caddy-docker-proxy, a DNS module) and therefore a custom build.
- `project-template/`: the sample project recipe (`devkit.json`, Compose files and a
  `t3.json` with T3 actions for the devkit commands).
- `t3.json`: T3 actions for working on the kit itself, and its project icon.
- `tests/`: unit tests, plus a Docker runtime acceptance check.

## Running things

- Tests: `go test ./...` (`gofmt -l .` and `go vet ./...` should be clean).
  Docker check: `DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`; it
  drives `bin/devkit` from outside, so build first.
- `~/.local/bin/devkit` points at `bin/devkit` in this checkout, so a rebuild takes
  effect immediately. The domain comes from the host config; `DEV_DOMAIN` overrides it.
- The dashboard runs as the user service `devkit-web` from `bin/devkit`. After
  changing Go code or `web/`, rebuild and run `systemctl --user restart devkit-web`
  (run `pnpm --dir frontend build` first after frontend edits; generated output is embedded).
- VM checks: `python3 tests/bootstrap_acceptance.py` uses mocked system commands;
  `DEVKIT_VM_RUNTIME_TEST=1 python3 tests/vm_runtime_acceptance.py` uses temporary paused QEMU guests (build first).
  `DEVKIT_VM_RUNTIME_TEST=1 python3 tests/vm_lifecycle_acceptance.py` exercises real launcher lifecycle, disk retention, named/project cleanup and explicit purge.
- UI checks: `pnpm --dir frontend test` for contracts, `pnpm --dir frontend test:ui`
  for Playwright against the synthetic fixture server. Set `CHROMIUM_PATH` for a system
  browser; otherwise install Playwright Chromium. No real host mutations in UI tests.
  Run `pnpm --dir frontend install --frozen-lockfile` with Node 22.12+ (24 recommended)
  and pnpm 10.33.0. Commit regenerated `web/dist/` with source changes.

## Conventions

- Commit straight to `main` and push. No PRs. Short imperative commit subjects.
- Reading the dashboard needs no sign-in; its actions do (`auth.go`, `actions.go`).
  Actions run the devkit binary itself, so add a CLI command first and a dashboard
  action on top. `planAction` must only accept targets that exist on the host. The
  web service also expires shares in the background. Mask secret-looking values when showing container environment.
- Project hooks live in `devkit.json` under `"hooks"` (`prepare`, `check`,
  `build`, `publish`) and run with `sh -eu` in the project root. `build` is
  required for review and `publish` for publishing; the others are optional.
  There is no justfile any more.
- A command that changes something records it with `happened(...)` once it succeeded;
  `noted(...)` in `dispatch` records failures. Tests write history to a temporary
  directory (`TestMain` sets `DEVKIT_STATE_DIR`).
- devkit never creates/deletes worktrees, commits, stashes, rebases or force-pushes.
  Explicit `git fetch|sync|push` actions may fetch origin, fast-forward a clean tracked
  branch or push to its exact origin upstream. Never make these discovery side effects.
- Update the docs when you change commands or behaviour: README.md is the front page (pitch,
  quick start, command table), `docs/` has the details (setup, previews, projects, dashboard,
  shares, testing). `docs/images/` holds the README screenshots, taken from the real page with
  made-up demo data; never use a real host's previews or names for them.

## Gotchas

- `devkit live` does not re-read Compose files for an existing preview. It reuses
  the rendered `<git-common-dir>/devkit/runs/<preview>/compose.json`. To pick up
  Compose changes, edit that file or destroy and recreate the preview (which
  deletes its data).
- Do not re-enable T3's Tailscale Serve on 443. Traefik owns that listener.
- Credentials (`~/.config/devkit/dns-token`, `cloudflare-token`, or whatever source the
  host config names) are private host files and must never be committed. Tests set
  `DEVKIT_CONFIG_DIR` (`TestMain`) so they never read the real host config.
- Share clean-up must only touch this machine's Cloudflare resources: tunnels named
  `devkit-<machine>-*` and DNS records with the comment `devkit share <machine>`.
- Don't use `pkill -f` with a pattern that appears in your own shell command; it
  kills that shell. Kill by PID, or use a pattern like `[g]eckodriver`.
