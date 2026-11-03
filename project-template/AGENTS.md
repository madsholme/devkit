# Agent notes

This project runs on a devbox host: the app and its services run in Docker, one
preview per Git worktree, at a private HTTPS URL on the tailnet. The host may not
have the project's language or tools installed; the containers do.

Copy the "Previews with devbox" section into your project's own AGENTS.md (or
CLAUDE.md) and fill in "This project" for it.

## Previews with devbox

- Work in your own worktree and run every `devbox` command from it. devbox acts on
  the current worktree's preview; never act on another worktree's.
- Run project commands (tests, linters, migrations, package managers, scripts) in
  the container: `devbox exec <command...>`, or `devbox exec -s <service> ...` for
  another Compose service (e.g. the database). Files it writes belong to you.
- `devbox exec` needs this worktree's live preview. If it says there is none, run
  `devbox live`; it prints the preview URL. Starting a live preview is fine whenever
  you need one.
- The live preview mounts the worktree's files, so edits show up as the app's own
  reload allows. It does not rebuild images: after changing a Dockerfile, dependencies
  or Compose files, ask before running `devbox destroy` then `devbox live`, because
  destroy deletes the preview's data.
- Read logs with `devbox logs` (the last 200 lines). Never use `devbox logs -f`: it
  does not return.
- To hand work over for review: commit, then run `devbox review HEAD`. It builds from
  the committed files only, so uncommitted changes are not in it. Report the URL it
  prints and the full commit SHA.
- `devbox ls` lists previews and their URLs; `devbox history -n 10` shows what devbox
  did recently, including failures.

Ask first, and only do when asked:

- `devbox destroy`, `devbox gc --yes` and `devbox prune --yes`: they delete data or
  affect the whole host.
- `devbox share`: it makes a preview public on the internet.
- `devbox publish`: it deploys to the team.

Never:

- run `docker`, `docker compose` or the app directly on the host to get around devbox;
- publish ports, set `container_name` or `network_mode` in Compose files, or put
  host secrets into an image;
- create or remove worktrees, or commit, push or merge, unless asked (devbox never does);
- fix the host yourself (Traefik, DNS, certificates, `devbox setup`). Report what
  `devbox doctor` says instead.

When editing Compose files: every service needs a health check and named volumes; the
review file uses the built image (`${DEVBOX_IMAGE}`) with no `build:` and no source
mounts. `DEVBOX_URL` is the preview's URL; while shared, the web service also gets
`DEVBOX_PUBLIC_URL`. Hooks (`prepare`, `check`, `build`, `publish`) live in
`devbox.json`.

## This project

- Web service `web` on port 8000; config in `devbox.json`.
- `message.txt` is reread on every request. Python code changes need a restart:
  `devbox stop` then `devbox start` (data is kept).
- Checks: `devbox exec python3 -c 'import ast; ast.parse(open("app.py").read())'`.
  Review runs the same check through the `check` hook.
