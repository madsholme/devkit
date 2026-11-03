# Dashboard

https://devkit.<domain> is titled with the host's name (its hostname, or `name` in the host config)
and preview domain, and shows whether the harness (e.g. T3) and Traefik are up; the certificate
appears there only when it expires within three weeks. The gear menu holds sign-in and the theme;
the footer shows which devkit commit runs. Below is a CPU/memory/disk/containers summary bar (it
also mentions reclaimable Docker data from 5 GB up) that opens a system monitor: 10-minute history, swap,
per-container usage, top host processes and Docker disk use. Containers and processes
are sampled only while it is open.

Below it are three tabs, remembered per browser and linkable (`#worktrees`, `#history`):

- **Previews**: mode, health, branch/commit, uptime and URL. A live preview shows its
  worktree's current commit and uncommitted changes, a review how far its worktree has
  moved on. Expand one for all its containers with ports (a routed port lists where it
  answers: paths such as `/api` as links, subdomain routes as `*.<preview>`), a details view (health,
  networks, mounts, environment with secret-looking values masked, labels) and logs
  (recent lines, or followed live). Previews whose worktree is gone (what `devkit gc`
  would remove) are marked, with a notice above the list.
- **Worktrees**: each repository's worktrees with their last commit, uncommitted
  changes, upstream distance and preview.
- **History**: what devkit did, newest first and grouped by day (see below). The tab
  counts failures of the last 24 hours, and an expanded preview lists its last few events.

Everything is read live from Docker labels and Git; there is no database. It is dark
by default; the settings menu (gear icon) picks light or auto (follows the system),
remembered per browser.

Anyone on the tailnet can read it. To act from it, set a password on the host with
`devkit web password`; only a salted PBKDF2 hash is stored, in `~/.config/devkit/config.json`.
A browser that signs in can for 7 days start, stop, share, unshare and destroy previews,
start a live preview or review HEAD for a worktree, and clean up previews of removed
worktrees; changing the password signs every browser out. Each action runs the `devkit`
binary itself, as the CLI would, and the page shows its output live. The server only
accepts targets that exist on the host, requests from the dashboard's own page, and
an explicit confirmation for destroy and clean-up; five wrong passwords lock an address out for five
minutes. `exec` stays in the CLI.

With the T3 harness, a signed-in browser also gets **Pair device…** in T3 Code's menu: it
shows a QR code, link and code from `devkit pair` that connect one phone or computer to T3 Code
within 10 minutes. devkit asks the `t3` version the T3 service runs (`t3 auth pairing create`),
returns the link to that page only, without keeping it, and records only that a pairing happened.

## History

Every command that changes something records an event: setup, proxy up, a DNS record
setup created, live and review (created or resumed), start, stop, destroy (including `gc`), prune, publish, share, extend, unshare,
shares that expired and leftover tunnels or DNS records removed. A failed command is
recorded too, with its error. Each event says who caused it: `cli` (also T3 actions),
`dashboard`, or `auto` for what the web service does by itself, such as expiring shares or the daily prune.
The log is one JSON line per event in `~/.local/state/devkit/history.jsonl`
(`$XDG_STATE_HOME/devkit` if set). At 1 MB it moves to `history.jsonl.1`, replacing the
older one, so about the last two megabytes are kept. Read it with `devkit history`
or on the dashboard; deleting it loses nothing but the past.

`devkit web` binds to 127.0.0.1 and `web.port` (3780). `devkit setup` installs it as the user
service `devkit-web`, and Traefik publishes it at `devkit.<domain>` on the tailnet. Repositories
appear once they have at least one preview or a worktree matching the harness's `worktrees` glob.
