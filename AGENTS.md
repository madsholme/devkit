# Agent notes for devbox-kit

Read README.md for what the kit does and STATUS.md for the state of this host.
This file covers what you need to change the kit itself.

## Layout

- `cmd/devbox/`: the whole CLI in Go, standard library only. Keep it that way.
  `main.go` parses commands; `preview.go` has live/review/lifecycle/gc/ls/publish/
  exec/logs; `repo.go` Git, config and saved previews; `naming.go` preview IDs;
  `docker.go` inspect data; `overview.go`, `resources.go` and `server.go` the web
  backend. Package variables such as `runCommand`, `dockerContainers` and
  `repository` are the seams tests replace.
- `bin/devbox`: the built binary (gitignored). Build with
  `go build -o bin/devbox ./cmd/devbox`; the Go version is pinned in `mise.toml`.
- `web/index.html`: the dashboard page. Vanilla JS, no build step, no dependencies.
  `web/web.go` embeds it and the icons into the binary.
  `web/logo.svg`, `favicon.ico` and `apple-touch-icon.png` are the CRT skin of the
  devkit logo; `serve()` only serves files listed in its `assets` map.
- `box/traefik/`: the edge proxy (container `devbox-edge`). `dynamic.yaml` is the
  file provider: it routes t3.DEV_DOMAIN to T3 on localhost:3773 and
  devbox.DEV_DOMAIN to the dashboard on localhost:3780. Previews are routed by
  Docker labels. Traefik listens only on the Tailscale IP 100.69.127.125:443.
- `box/systemd/devbox-web.service`: reference copy of the installed user unit.
- `project-template/`: the sample project recipe (`devbox.json`, Compose files and a
  `t3.json` with T3 actions for the devbox commands).
- `t3.json`: T3 actions for working on the kit itself, and its project icon.
- `tests/`: unit tests, plus a Docker runtime acceptance check.

## Running things

- Tests: `go test ./...` (`gofmt -l .` and `go vet ./...` should be clean).
  Docker check: `DEVBOX_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`; it
  drives `bin/devbox` from outside, so build first.
- `~/.local/bin/devbox` is a wrapper that sets DEV_DOMAIN and runs `bin/devbox`
  from this checkout, so a rebuild takes effect immediately.
- The dashboard runs as the user service `devbox-web` from `bin/devbox`. After
  changing Go code or `web/`, rebuild and run `systemctl --user restart devbox-web`
  (the page is embedded, so page edits need a rebuild too).
- UI checks: jsdom in a scratch directory for behaviour, headless Firefox for
  screenshots. Firefox is a snap, so write screenshots to a non-hidden folder
  under `~`, not `/tmp` or a dot-directory.

## Conventions

- Commit straight to `main` and push. No PRs. Short imperative commit subjects.
- The dashboard is read-only. Lifecycle actions stay in the CLI unless the user
  asks otherwise. Mask secret-looking values when showing container environment.
- Project hooks live in `devbox.json` under `"hooks"` (`prepare`, `check`,
  `build`, `publish`) and run with `sh -eu` in the project root. `build` is
  required for review and `publish` for publishing; the others are optional.
  There is no justfile any more.
- devbox never creates or deletes worktrees, and never commits, pushes or merges.
- Update README.md when you change commands or behaviour.

## Gotchas

- `devbox live` does not re-read Compose files for an existing preview. It reuses
  the rendered `<git-common-dir>/devbox/runs/<preview>/compose.json`. To pick up
  Compose changes, edit that file or destroy and recreate the preview (which
  deletes its data).
- Do not re-enable T3's Tailscale Serve on 443. Traefik owns that listener.
- Credentials (`box/traefik/.env`, `dns-provider.env`) are private host files and
  must never be committed.
