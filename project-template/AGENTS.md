# Agent notes

This project runs on a devkit host: the app and its services run in Docker, one
preview per Git worktree, at a private HTTPS URL on the tailnet. The host may not
have the project's language or tools installed; the containers do.

Copy the "Previews with devkit" section into your project's own AGENTS.md (or
CLAUDE.md) and fill in "This project" for it.

## Previews with devkit

- Work in your own worktree and run every `devkit` command from it. devkit acts on
  the current worktree's preview; never act on another worktree's.
- Run project commands (tests, linters, migrations, package managers, scripts) in
  the container: `devkit exec <command...>`, or `devkit exec -s <service> ...` for
  another Compose service (e.g. the database). Files it writes belong to you.
- `devkit exec` needs this worktree's live preview. If it says there is none, run
  `devkit live`; it prints the preview URL. Starting a live preview is fine whenever
  you need one.
- The live preview mounts the worktree's files, so edits show up as the app's own
  reload allows. It does not rebuild images: after changing a Dockerfile, dependencies
  or Compose files, ask before running `devkit destroy` then `devkit live`, because
  destroy deletes the preview's data.
- Read logs with `devkit logs` (the last 200 lines). Never use `devkit logs -f`: it
  does not return.
- To hand work over for review: commit, then run `devkit review HEAD`. It builds from
  the committed files only, so uncommitted changes are not in it. Report the URL it
  prints and the full commit SHA.
- `devkit ls` lists previews and their URLs; `devkit history -n 10` shows what devkit
  did recently, including failures.

Ask first, and only do when asked:

- `devkit destroy`, `devkit gc --yes` and `devkit prune --yes`: they delete data or
  affect the whole host.
- `devkit share`: it makes a preview public on the internet.
- `devkit publish`: it deploys to the team.

Never:

- run `docker`, `docker compose` or the app directly on the host to get around devkit;
- publish ports, set `container_name` or `network_mode` in Compose files, or put
  host secrets into an image;
- create or remove worktrees, or commit, push or merge, unless asked (devkit never does);
- fix the host yourself (Traefik, DNS, certificates, `devkit setup`). Report what
  `devkit doctor` says instead.

When editing Compose files: every service needs a health check and named volumes; the
review file uses the built image (`${DEVKIT_IMAGE}`) with no `build:` and no source
mounts. `DEVKIT_URL` is the preview's URL; while shared, the web service also gets
`DEVKIT_PUBLIC_URL`. Hooks (`prepare`, `check`, `build`, `publish`) live in
`devkit.json`.

## This project

- Web service `web` on port 8000; config in `devkit.json`.
- `message.txt` is reread on every request. Python code changes need a restart:
  `devkit stop` then `devkit start` (data is kept).
- Checks: `devkit exec python3 -c 'import ast; ast.parse(open("app.py").read())'`.
  Review runs the same check through the `check` hook.
