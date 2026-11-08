# Contributing

Thanks for taking a look. devkit is a side project, so replies may take a while, but issues and
pull requests are welcome.

## Issues

Bugs, questions and ideas are all fine. For a bug, include what you ran, what happened, your
distribution, and the output of `devkit version` and `devkit doctor` (remove anything private,
such as your domain or tokens).

## Pull requests

Small, focused pull requests are easiest to review. For a larger change, such as a new command,
a new DNS or share provider, or a change to how previews are named or stored, open an issue first
so we can agree on the approach before you spend time on it.

Before you open one:

- `gofmt -l .` prints nothing, and `go vet ./...` and `go test ./...` pass. The Go version is
  pinned in `mise.toml`; `mise install` gets it.
- If you changed how previews run, also run the Docker check:
  `go build -o bin/devkit ./cmd/devkit && DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`.
- Update `README.md` or the page in `docs/` that describes what you changed.
- For dashboard changes, run `pnpm --dir frontend test`, `pnpm --dir frontend build`
  and `pnpm --dir frontend test:ui`. Commit regenerated `web/dist/` assets and add a
  synthetic-data screenshot to the pull request. See [frontend checks](docs/testing.md#frontend-development-and-checks).

## How the code is kept

[AGENTS.md](AGENTS.md) describes the layout and conventions; it's written for coding agents, but
it's the best map for people too. The main rules:

- The CLI is Go with the standard library only. The Vue dashboard in `frontend/` uses
  SwiftDesign components and themes; reuse those components and avoid unrelated dependencies.
  Its compiled assets remain embedded in the single Go binary.
- Tests replace package variables such as `runCommand` and `dockerContainers` instead of running
  Docker or Git for real, and never read the real host config.
- Dashboard actions run the `devkit` binary itself, so new actions start as a CLI command.
- devkit never creates or deletes worktrees, commits, pushes or merges, and never uses sudo.
- Credentials never go into the repository, logs, history or test fixtures.

By contributing you agree that your contribution is licensed under the [MIT license](LICENSE).
