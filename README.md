# devbox

A small, single-host kit for remote agent development and private previews.
The Macs connect to one Linux host over Tailscale. Git owns workspace metadata;
Docker labels identify previews. No central task database.

Target supplied for the first installation: `rene@192.168.1.21`.
Configured domain: `dev.steenfeldt.net`, Cloudflare DNS, Tailscale IP `100.69.127.125`.
Wildcard HTTPS is commissioned. T3 is at https://t3.dev.steenfeldt.net.
The Hetzner/Dokploy destination is pending.
See [installation status](STATUS.md) for completed runtime checks.

## Workflow

Worktree → agent work → commit → local review → explicit team deployment → merge.
Start a live preview whenever you want a longer back-and-forth session.

devbox does not create or delete worktrees. Your tools (T3 Code, Orca, or plain
`git worktree add`) own them; devbox only manages the environment (containers,
volumes, preview URL) of whichever worktree it runs in. Creating a worktree does
not start services. The kit does not commit, push Git, merge or install system
software; `devbox setup` configures the rest of the host from your answers.

| Command | Behavior |
|---|---|
| `devbox setup` | Configure this host step by step; safe to run again (see [Host setup](#host-setup)) |
| `devbox doctor` | Check the host and say how to fix what is missing |
| `devbox proxy up` | Write Traefik's configuration from the host config and start or update it |
| `devbox live` | Start/resume current workspace's live preview |
| `devbox review HEAD` | Clean checkout, checks, image build, isolated review runtime |
| `devbox exec [-s service] <cmd>` | Run a command in this worktree's live web container (or another service's), as your user |
| `devbox logs [-s service] [-f]` | Show (or follow) this worktree's live web log (or another service's) |
| `devbox web` | Serve the web overview (see below); `devbox web password` enables its actions |
| `devbox ls` | Preview IDs, modes, web health/status, source commits and URLs |
| `devbox stop [preview]` | Stop containers; retain data |
| `devbox start [preview]` | Resume saved configuration; retain data |
| `devbox destroy [preview]` | Remove containers/networks and disposable named volumes |
| `devbox gc [--yes]` | Destroy previews whose worktree no longer exists; dry run without `--yes` |
| `devbox prune [--yes]` | Trim Docker build cache and remove old unused images, host-wide (limits in the host config); dry run without `--yes` |
| `devbox publish HEAD` | Run the project's `publish` hook with the reviewed image ID |
| `devbox share [preview] [--host NAME] [--hours N]` | Make a running preview public until it expires (see below) |
| `devbox unshare [preview]` | End a public share; `--expired` removes all expired ones and leftovers |
| `devbox shares` | List public shares, their URLs and expiry |
| `devbox history [preview] [-n N]` | What devbox did on this host and who did it, newest last (default 30 events) |

Run lifecycle commands inside the owning Git repository. `ls` works anywhere.
Without a preview ID, `stop`, `start` and `destroy` act on the preview that belongs
to the current worktree: its live preview, or a review started from it. If several
match (e.g. a live and a review preview), devbox lists them and asks for an ID.
Each preview records the worktree it was started from in its `devbox.worktree` label.
To remove a finished worktree, run `devbox destroy` in it and then remove it with
your tool or `git worktree remove <path>`; Git refuses removal of uncommitted work.
If a tool already deleted the worktree, `devbox gc` lists the orphaned previews and
`devbox gc --yes` destroys them (containers, networks, volumes and run dirs). It
only considers previews of the current repository and never touches a preview
whose worktree still exists. Review previews created before devbox recorded
`devbox.worktree` are never collected; destroy them by ID.
Branches are retained until you explicitly delete them.

Every review is a fresh build, so build cache and images pile up as previews come
and go. `devbox prune --yes` trims the Docker build cache to 30 GB and removes images
older than a week that no container, running or stopped, uses. It works host-wide,
not only on devbox images, and never touches volumes. The web service runs it once a
day; without it, run it yourself. `devbox prune` alone shows `docker system df`.
The limits and the daily run are set in the host config's `prune` section.

## Three modes

- Live: source bind mount, independent task data. The sample rereads `message.txt`
  on every request. Python code edits need a restart; adapt the recipe to your
  real application's hot-reload server. Live services get `restart: unless-stopped`,
  so running live previews come back after a host reboot (with Traefik, which uses the
  same policy); stopped ones stay stopped. Creating a live preview builds its Compose images from
  the current files; resuming one does not, so after changing a Dockerfile or dependencies,
  destroy and recreate it. Previews saved before this policy existed
  gain it the next time `devbox live` or `devbox start` brings them up.
- Local review: a detached checkout of an exact commit supplies build inputs.
  The web image is resolved to a Docker image ID; no review service may bind-mount
  host files. The temporary checkout is removed after the build. Test data remains
  mutable. Repeating review for the same commit resumes the existing artifact.
- Team review: explicit deployment to Hetzner/Dokploy. The adapter needs destination
  configuration; see [publishing](docs/publishing.md).

Commit the project recipe before using `review`; it reads configuration from the
selected commit. Generated inputs should be deterministic and dependencies locked.
This v1 sample does not initialize submodules or Git LFS automatically.

## Host setup

Linux, Git, Docker Engine with Compose v2 (usable without sudo), Tailscale and a
systemd user session; the agent tool is your choice. Install these with their official
instructions; devbox checks for them but installs no system software. Building devbox
needs Go (the version is pinned in `mise.toml`). Python 3 is only used by the Docker
acceptance checks. You need a domain whose DNS provider Traefik supports (Cloudflare gets
the guided path) for the wildcard certificate.

devbox is a single Go binary with the dashboard and the Traefik configuration built in.
Build it, put it on PATH and run setup:

```sh
go build -o bin/devbox ./cmd/devbox
install -d "$HOME/.local/bin" && ln -sf "$PWD/bin/devbox" "$HOME/.local/bin/devbox"
devbox setup
```

`devbox setup` asks for each setting, offering the current one as default, and can be run
again at any time to change something or repair the host. It:

1. Checks Docker, Compose, Tailscale and systemd, and notes if lingering is off (`sudo loginctl
   enable-linger $USER` keeps user services running after you log out).
2. Asks for the preview domain (`dev.example.com`), the agent harness (`t3`, `none` or
   `custom`), the email for the Let's Encrypt account and the DNS provider.
3. Asks where each DNS credential comes from (see [Secrets](#secrets)) and checks it; a
   Cloudflare token needs Zone / DNS / Edit and Zone / Zone / Read for the domain's zone.
4. Checks that `*.<domain>` resolves to the Tailscale IP. On Cloudflare it offers to create the
   DNS-only record `*.<domain> A <tailscale ip>`; elsewhere create it yourself.
5. Starts Traefik (`devbox proxy up`). Without a trusted certificate yet, it first gets one from
   Let's Encrypt's staging service, whose rate limits forgive mistakes, then switches to production.
6. Installs and starts the dashboard as the user service `devbox-web`, running this binary.
7. Runs `devbox doctor`.

`devbox setup --yes` takes every default without asking (`--domain`, `--email`, `--harness`
set them) and fails where there is none, e.g. a credential that is not configured yet.
`devbox doctor` checks the same things at any time: config, Docker, Tailscale, DNS, the proxy
network, Traefik, the certificate (trusted, days left), the dashboard service, lingering, the
harness and the Cloudflare tokens. Each line ends with how to fix it; it exits non-zero when a
check fails.

Rebuild after pulling changes; replacing the binary is safe while `devbox web` runs, which
then needs `systemctl --user restart devbox-web`. If you move the binary, run setup again so
the service follows it.

### Host config

Everything lives in `~/.config/devbox/config.json` (`DEVBOX_CONFIG_DIR` moves the directory),
which setup writes and which you may edit:

```json
{
  "domain": "dev.example.com",
  "harness": {"name": "T3 Code", "host": "t3", "url": "http://127.0.0.1:3773",
              "worktrees": "~/.t3/worktrees/*/*"},
  "proxy": {"email": "you@example.com", "dns_provider": "cloudflare",
            "dns_env": {"CF_DNS_API_TOKEN": {"file": "~/.config/devbox/dns-token"}}},
  "web": {"port": 3780},
  "prune": {"build_cache_gb": 30, "image_age_days": 7, "daily": true},
  "share": {"…": "see Public shares"}
}
```

- `domain`: previews are `<name>.<domain>`, the dashboard `devbox.<domain>`. `DEV_DOMAIN`
  overrides it, e.g. for tests.
- `tailscale_ip`: optional; read from `tailscale ip -4` when empty.
- `harness`: the agent tool. `name` is shown on the dashboard with whether `url` answers,
  Traefik routes `<host>.<domain>` to `url`, and repositories with worktrees matching the
  `worktrees` glob appear on the dashboard. `null` for none.
- `proxy`: `dns_provider` is a [lego provider](https://go-acme.github.io/lego/dns/) and `dns_env`
  its credentials by variable name. Optional: `image` (a pinned Traefik), `staging`.
- `web.port`: where `devbox web` listens on localhost.
- `prune`: optional; what `devbox prune` keeps. `build_cache_gb` of Docker build cache
  (default 30), unused images newer than `image_age_days` (default 7), and `daily: false`
  stops the web service pruning by itself (default true; read each day, no restart needed).

After editing `domain`, `harness`, `proxy` or `web.port`, run `devbox proxy up`; doctor says
when Traefik is out of date.

### Secrets

Every credential setting (`proxy.dns_env.*`, `share.token`) takes exactly one source:

- `{"file": "~/.config/devbox/dns-token"}`: a file only you can read (`chmod 600`). This is
  what setup writes when you type a secret.
- `{"env": "CF_DNS_API_TOKEN"}`: an environment variable, so a secret manager can inject it:
  `op run --env-file=devbox.env -- devbox proxy up` (1Password), `bws run`, `doppler run`, ...
- `{"command": "op read op://dev/cloudflare/token"}`: a command that prints it, run each time
  it is needed (`sh -c`), so nothing is written to disk.

Traefik needs the DNS credentials for as long as it renews certificates, so `devbox proxy up`
hands them to its container through the environment of `docker compose`. They are not written
to the proxy directory, but Docker keeps them in the container's configuration.

### Private routing

Traefik (`devbox proxy up`, Compose project `devbox-proxy`) runs with host networking and binds
HTTPS only to the Tailscale IP. Its files are generated into `~/.config/devbox/proxy/`; edit the
host config rather than these. It requests one wildcard certificate for `*.<domain>` by DNS-01,
so no public inbound port is needed, routes `devbox.<domain>` to the dashboard and the harness
host to the harness, and takes preview routes from Docker labels on the `devbox-proxy` network.
The wildcard certificate does not cover the bare domain.

The wildcard DNS record must point at the Tailscale IP (`tailscale ip -4`), not the LAN
address. Browser DNS must resolve the public record; rebinding filters may need a domain
exception or split DNS. Names are flat and readable: a live preview is `<project>-<branch>`
(e.g. `demo-fix-login.dev.example.com`), a review `<project>-<branch>-<sha7>` (e.g.
`demo-fix-login-3052f1e`), with the full branch name kept (`t3code/x` becomes `t3code-x`). If a
name is already taken, e.g. by branches `try/a` and `try-a`, the newer preview gets a
4-character suffix. Previews created before readable names keep their hashed IDs until destroyed.

The shared proxy network is not a security boundary between apps. The Docker socket gives
Traefik powerful Docker access even when mounted `:ro`. This v1 assumes your trusted
development host, not hostile tenants. Only expose the application frontend to `devbox-proxy`;
keep databases on their Compose-scoped networks. Do not publish database ports. Host
firewall/Tailscale policy should grant the intended users access to TCP 443 and SSH.

Traefik's image is pinned (`proxy.image` overrides it). Do not run automatic image updates.

## Public shares

Previews are tailnet-only. `devbox share` makes one public for a while, e.g. for a
coworker or to test SSO, through a Cloudflare Tunnel: no open ports, and nothing
reaches the harness or the dashboard. Shares live on their own domain on Cloudflare, not on
the preview domain. Cloudflare's free certificate only covers one level below a domain, and a
separate domain keeps test apps away from your main domain's cookies.
A self-hosted alternative to Cloudflare is planned, not built: see [docs/edge.md](docs/edge.md).

Configure the host once in `~/.config/devbox/config.json`:

```json
{"share": {"domain": "example-share.com", "account_id": "<Cloudflare account ID>",
           "hosts": ["sso-1", "sso-2"], "default_hours": 8, "max_hours": 72}}
```

and put an API token with **Tunnel Write** and **DNS Write** for that domain in
`~/.config/devbox/cloudflare-token` (`chmod 600`), or name another [source](#secrets) with
`"token"`. Optional keys: `zone_id` (looked up from the domain otherwise), `prefix`, and
`image` (the pinned `cloudflared`).

`devbox share` publishes the current worktree's preview (or the one named) at
`preview-<preview>.<domain>`, or at a fixed name from `hosts` with `--host sso-1`. Change
the prefix with `"prefix"` (`""` for none); names over the 63-character DNS limit are
shortened with a short hash. Register
fixed names once as OAuth callback hosts with Google, GitHub and so on; Google does
not accept wildcards. Each share is its own tunnel and a `cloudflared` container next to the
preview, labelled with it, so the web overview shows it in the preview's group, marked
public with its URL and time left; `devbox shares` lists them. Running `devbox share`
again extends the expiry.

A share ends when it expires, on `devbox unshare`, and before `stop`, `destroy` or
`gc`: devbox removes the container, the DNS record and the tunnel. The web service
(`devbox web`) expires shares every minute; without it, run `devbox unshare --expired`.
It also removes this machine's leftover tunnels and DNS records, e.g. after a crash
mid-share. Tunnels are named `devbox-<machine>-<preview>` and DNS records carry the
comment `devbox share <machine>`; devbox never touches anything else in the account.

While shared, the preview's web service gets `DEVBOX_PUBLIC_URL` (the web container is
recreated to pick it up, and again when the share ends). Let the app accept that host,
e.g. Django's `ALLOWED_HOSTS` and `CSRF_TRUSTED_ORIGINS`, and build absolute URLs
and OAuth callbacks from it. A share is public: anyone with the link can open it.
Prefer review previews; a live preview may run with debug pages on.

## Web overview

https://devbox.<domain> shows whether the harness (e.g. T3) and Traefik are up and days until the
certificate expires, then a CPU/memory/disk summary bar (it also mentions reclaimable
Docker data from 5 GB up) that opens a system monitor: 10-minute history, swap,
per-container usage, top host processes and Docker disk use. Containers and processes
are sampled only while it is open.

Below it are three tabs, remembered per browser and linkable (`#worktrees`, `#history`):

- **Previews**: mode, health, branch/commit, uptime and URL. A live preview shows its
  worktree's current commit and uncommitted changes, a review how far its worktree has
  moved on. Expand one for all its containers with ports (a routed port lists where it
  answers: paths such as `/api` as links, subdomain routes as `*.<preview>`), a details view (health,
  networks, mounts, environment with secret-looking values masked, labels) and logs
  (recent lines, or followed live). Previews whose worktree is gone (what `devbox gc`
  would remove) are marked, with a notice above the list.
- **Worktrees**: each repository's worktrees with their last commit, uncommitted
  changes, upstream distance and preview.
- **History**: what devbox did, newest first and grouped by day (see below). The tab
  counts failures of the last 24 hours, and an expanded preview lists its last few events.

Everything is read live from Docker labels and Git; there is no database. It is dark
by default; the settings menu (gear icon) picks light or auto (follows the system),
remembered per browser.

Anyone on the tailnet can read it. To act from it, set a password on the host with
`devbox web password`; only a salted PBKDF2 hash is stored, in `~/.config/devbox/config.json`.
A browser that signs in can for 7 days start, stop, share, unshare and destroy previews,
start a live preview or review HEAD for a worktree, and clean up previews of removed
worktrees; changing the password signs every browser out. Each action runs the `devbox`
binary itself, as the CLI would, and the page shows its output live. The server only
accepts targets that exist on the host, requests from the dashboard's own page, and
an explicit confirmation for destroy and clean-up; five wrong passwords lock an address out for five
minutes. `exec` stays in the CLI.

### History

Every command that changes something records an event: setup, proxy up, a DNS record
setup created, live and review (created or resumed), start, stop, destroy (including `gc`), prune, publish, share, extend, unshare,
shares that expired and leftover tunnels or DNS records removed. A failed command is
recorded too, with its error. Each event says who caused it: `cli` (also T3 actions),
`dashboard`, or `auto` for what the web service does by itself, such as expiring shares or the daily prune.
The log is one JSON line per event in `~/.local/state/devbox/history.jsonl`
(`$XDG_STATE_HOME/devbox` if set). At 1 MB it moves to `history.jsonl.1`, replacing the
older one, so about the last two megabytes are kept. Read it with `devbox history`
or on the dashboard; deleting it loses nothing but the past.

`devbox web` binds to 127.0.0.1 and `web.port` (3780). `devbox setup` installs it as the user
service `devbox-web`, and Traefik publishes it at `devbox.<domain>` on the tailnet. Repositories
appear once they have at least one preview or a worktree matching the harness's `worktrees` glob.

## Project recipe

Copy/adapt `project-template` into ONE real project. Give `devbox.json` a unique
project slug per clone on a host. It identifies the web service, its internal port,
and separate live/review Compose files. Common configuration can be factored out
later; the tiny sample deliberately keeps each file self-contained.

`devbox.json` also holds the project's hooks: shell commands run with `sh -eu`
in the project root. `prepare` runs before live and review, `check` and `build`
during review, `publish` for `devbox publish`. All are optional except `build`
(review) and `publish` (publishing). Prepare/check must not modify tracked review
inputs. Build must produce `$DEVBOX_IMAGE`. Compose files must have health checks, isolated named volumes,
no fixed container names and no host port publishing in Compose. The explicit
`media_ports` exception below lets devbox allocate Tailscale-bound media ports. Review Compose uses the built
image and has no build entries or source mounts. `DEVBOX_URL` and `DEVBOX_COMMIT`
are available during rendering/building for project configuration.

The runner does not blindly copy `.env` from another checkout. Pass project-specific
development settings deliberately and generate per-preview connections/URLs. Never
include host secrets in the Docker build context. The supplied `.dockerignore` is
an allowlist for the sample. Rendered Compose config may contain environment secrets;
it is stored privately at `<git-common-dir>/devbox/runs/<preview>/compose.json`.
These are per-preview execution artifacts for restart/cleanup, not a task registry.

### Media ports

WebRTC and other media protocols cannot travel through an HTTP preview route.
Opt a project into direct tailnet media by declaring named UDP/TCP listeners in
`devbox.json`:

```json
"media_ports": [
  {"name": "sfu_udp", "service": "web", "protocol": "udp"},
  {"name": "ice_tcp", "service": "web", "protocol": "tcp"},
  {"name": "turn_tls", "service": "turn", "protocol": "tcp", "target": 5349}
]
```

Names must be unique lowercase letters, digits and underscores, starting with a
letter (at most 32 characters). At most 16 mappings are allowed. `target` is an
optional fixed container port; when omitted, the container listens on the same
port devbox allocated on the host. This is useful for an SFU that advertises its
own listener port. Each mapping gets a distinct port number in 20000–29999.

During **Compose rendering**, devbox supplies `DEVBOX_MEDIA_IP` (the local
`tailscale ip -4` address) and `DEVBOX_MEDIA_<UPPERCASE_NAME>_PORT` for each mapping:

```yaml
services:
  web:
    environment:
      PLENUM_ICE_PUBLIC_IPS: ${DEVBOX_MEDIA_IP:?}
      PLENUM_UDP_PORT: ${DEVBOX_MEDIA_SFU_UDP_PORT:?}
      PLENUM_ICE_TCP_PORT: ${DEVBOX_MEDIA_ICE_TCP_PORT:?}
```

Do **not** put `ports` in the Compose file: devbox inserts only the mappings
declared above, bound to its local Tailscale IPv4. Raw Compose port publishing,
`network_mode`, and fixed container names remain rejected. HTTP/WSS still use
the normal preview URL. Media variables are not available to prepare/check/build
hooks; use them for runtime configuration in Compose.

Allocation is serialized across CLI processes and repositories, checks listening
host sockets and Docker bindings, and reserves stopped previews' ports. Private
port lease files live in `~/.local/state/devbox/media-ports/` (under
`DEVBOX_STATE_DIR` or `XDG_STATE_HOME` when configured). Saved Compose retains the
chosen address/ports across stop/start. Destroy releases the lease; failed or
interrupted creation without saved Compose is reclaimed on the next allocation.
An unrelated host process can still claim a port before Docker binds it; Docker
then fails startup rather than changing or sharing the mapping. A changed
Tailscale address requires recreating the preview.

Tailnet/firewall policy must permit the selected media ports. These are private
tailnet listeners: a public `devbox share` only shares HTTP, and does not expose
media. TURN-TLS services must provide their own TLS certificate/configuration;
Traefik's HTTP routing does not terminate these raw TCP connections.

### Routes

The web service answers on the preview's host. A project with more than one public service lists
the others under `"routes"`; each route sends part of the preview's traffic to a Compose service:

```json
"routes": [
  {"service": "api", "port": 80, "paths": ["/api", "/admin"]},
  {"service": "core", "port": 8002, "subdomains": true}
]
```

- `paths`: requests to the preview's host whose path starts with one of these prefixes.
- `subdomains`: every host one level below the preview's host, e.g. `acme.<preview>.<domain>`, for
  apps that tell tenants apart by host name. The wildcard DNS record already resolves them; Traefik
  requests a certificate for `<preview>` and `*.<preview>` from its `dns` resolver, one per preview
  (Let's Encrypt allows 50 a week per registered domain). Combined with `paths`, both must match.

`"wait_timeout"` (seconds, default 120) is how long `live`, `review` and `start` wait for every
health check; raise it for stacks that migrate, seed or compile on start.

Routed services join the proxy network like the web service. The web service stays the one that
speaks for the preview: its health, `devbox exec`/`logs` without `-s`, and shares, which publish the
preview's own host only.

Seed data must be idempotent and wait for database readiness. A one-shot service
(such as a separate seed job) must set `restart: "no"` in the live Compose file, or
the live restart policy would rerun it whenever it exits. The sample does this
on startup without resetting existing data. For a separate seed service, gate app
startup on its successful completion and make it safe to rerun.

## T3 actions

`project-template/t3.json` declares T3 Code scripts for the devbox commands: live
preview, review of HEAD, logs and stop. T3 reads `t3.json` from the repository root
and offers its scripts for import in the project's scripts menu; import them once
per project. They run in a T3 terminal in the thread's worktree, so each thread
drives its own preview. Add project-specific ones there too, e.g. tests through
`devbox exec`. T3 ignores the whole file if any field is invalid; check it against
https://t3.codes/schema/t3.json. This repository's own `t3.json` has test and
dashboard scripts and sets the CRT logo as the project icon.

## Agent integration

Give your agent the following project instruction:

> Work in a separate worktree. Run tests with devbox exec. Start devbox live only
> when a live preview is useful or requested. After committing, use devbox review
> with the exact commit for review and report its URL and SHA. Preserve stopped
> preview data. Publish to the team only when requested.

Use Git worktree commands to discover existing workspaces, regardless of which
harness created them. T3 stays on localhost:3773 behind Traefik at
https://t3.dev.steenfeldt.net. Remove the old Tailscale Serve 443 mapping only after
the new HTTPS endpoint is ready. Clients then reconnect using the new address.
Do not enable T3's Tailscale Serve 443 mapping again: it would conflict with
Traefik. No unverified automatic worktree hook is required.

## Acceptance check

1. Create a Git repository from the sample and commit it.
2. Create worktrees for branches `try/a` and `try-a` (e.g. `git worktree add -b try/a ../try-a`);
   verify distinct names/URLs.
3. Start both live previews. Change each `message.txt`; verify different responses.
4. Increment one preview's counter; the other must stay unchanged.
5. Commit one task; run `devbox review HEAD`. Change its live message again; the
   review must keep the committed message.
6. Increment the review counter, stop/start it, and verify the count persists.
7. Open the same URLs from the other Mac. Disconnect the first client; agents and
   previews should continue on the Linux host.
8. Configure the publishing adapter and complete one team review on Hetzner.

Local validation commands: `go test ./...` and `bin/devbox --help`. Full runtime acceptance requires a Docker daemon,
the Linux host, real domain/DNS settings and a configured publishing destination.

The Docker-only check can run with `DEVBOX_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`.
It uses temporary repositories and containers with loopback-only test HTTPS,
does not issue certificates or alter DNS, and removes its test containers/volumes.
The media check is `DEVBOX_RUNTIME_TEST=1 python3 tests/media_acceptance.py`; it
requires local Tailscale, creates temporary UDP/TCP echo previews, checks distinct
ports and restart persistence through the tailnet IP, and cleans them up.

## References

- https://git-scm.com/docs/git-worktree
- https://docs.docker.com/compose/how-tos/project-name/
- https://doc.traefik.io/traefik/reference/install-configuration/tls/certificate-resolvers/acme/
- https://letsencrypt.org/docs/challenge-types/
- https://github.com/pingdotgg/t3code/blob/main/docs/user/remote-access.md
