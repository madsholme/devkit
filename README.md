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
not start services. The kit does not commit, push Git, merge, install system
software, or configure credentials automatically.

| Command | Behavior |
|---|---|
| `devbox live` | Start/resume current workspace's live preview |
| `devbox review HEAD` | Clean checkout, checks, image build, isolated review runtime |
| `devbox exec <cmd>` | Run a command in this worktree's live web container, as your user |
| `devbox logs [-f]` | Show (or follow) this worktree's live web log |
| `devbox web` | Serve the web overview (see below); `devbox web password` enables its actions |
| `devbox ls` | Preview IDs, modes, web health/status, source commits and URLs |
| `devbox stop [preview]` | Stop containers; retain data |
| `devbox start [preview]` | Resume saved configuration; retain data |
| `devbox destroy [preview]` | Remove containers/networks and disposable named volumes |
| `devbox gc [--yes]` | Destroy previews whose worktree no longer exists; dry run without `--yes` |
| `devbox publish HEAD` | Run the project's `publish` hook with the reviewed image ID |
| `devbox share [preview] [--host NAME] [--hours N]` | Make a running preview public until it expires (see below) |
| `devbox unshare [preview]` | End a public share; `--expired` removes all expired ones and leftovers |
| `devbox shares` | List public shares, their URLs and expiry |

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

## Three modes

- Live: source bind mount, independent task data. The sample rereads `message.txt`
  on every request. Python code edits need a restart; adapt the recipe to your
  real application's hot-reload server. Live services get `restart: unless-stopped`,
  so running live previews come back after a host reboot (with Traefik, which uses the
  same policy); stopped ones stay stopped. Previews saved before this policy existed
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

## Host prerequisites

Linux, Git, Docker Engine with Compose v2, Tailscale, and the agent runtime of
your choice. Building devbox needs Go (the version is pinned in `mise.toml`).
Python 3 is only used by the Docker acceptance check. Install these using their official instructions after
checking the target host. Do not pipe installers into a shell without inspecting
the host and choosing versions. The installation is intentionally separate.

devbox is a single Go binary with the web page built in. Build and install it on
the Linux host when ready:

```sh
go build -o bin/devbox ./cmd/devbox
install -d "$HOME/.local/bin"
printf '#!/bin/sh\nexport DEV_DOMAIN="${DEV_DOMAIN:-dev.your-real-domain.com}"\nexec %s/bin/devbox "$@"\n' "$PWD" \
  > "$HOME/.local/bin/devbox" && chmod 0755 "$HOME/.local/bin/devbox"
export PATH="$HOME/.local/bin:$PATH"
```

Rebuild after pulling changes; replacing the binary is safe while `devbox web` runs,
which then needs a restart. Persist PATH/DEV_DOMAIN in the environment used by your agent service as well as
interactive shells. A shell profile alone does not configure a systemd service.

## Private routing

Create one DNS-only public A record:

```dns
*.dev.your-real-domain.com. A <Linux-host-Tailscale-IPv4>
```

The LAN address `192.168.1.21` is not the Tailscale IP. Get the latter with
`tailscale ip -4`. Names are flat and readable: a live preview is
`<project>-<branch>` (e.g. `demo-fix-login.dev.example.com`), a review
`<project>-<branch>-<sha7>` (e.g. `demo-fix-login-3052f1e`), with the full branch
name kept (`t3code/x` becomes `t3code-x`). If a name is already taken, e.g. by
branches `try/a` and `try-a`, the newer preview gets a 4-character suffix. Previews
created before readable names keep their hashed IDs until destroyed.

In `box/traefik`:

1. Copy `.env.example` to `.env` and enter the real settings.
2. Copy `dns-provider.env.example` to `dns-provider.env`; add the selected provider's
   API environment variables. Keep this file private (`chmod 600`).
3. Create the Docker network once: `docker network create devbox-proxy`.
4. Run `docker compose config --quiet`, then `docker compose up -d`.
5. Start a sample preview and inspect issuance logs.
6. The template defaults to Let's Encrypt staging. After successful validation,
   change ACME_CA_SERVER to production and ACME_STORAGE to `/certs/acme.json`,
   then recreate Traefik for trusted HTTPS. Separate storage keeps staging
   certificates out of the production certificate store.

On Linux, Traefik uses host networking and binds HTTPS specifically to the
Tailscale IP. This also lets it reach the T3 backend on localhost:3773. Its
file-provider configuration requests one wildcard certificate via the default TLS
store and routes t3.DEV_DOMAIN to T3; application routes come from Docker labels.
No public inbound challenge port is required. The wildcard certificate covers no
base URL, as agreed. Browser DNS
must resolve the public record; rebinding filters may require a domain exception
or split DNS. The shared proxy network is not a security boundary between apps.
The Docker socket gives Traefik powerful Docker access even when mounted `:ro`.
This v1 assumes your trusted development host, not hostile tenants.

Only expose the application frontend to `devbox-proxy`; keep databases on their
Compose-scoped networks. Do not publish database ports. Host firewall/Tailscale
policy should grant the intended users access to TCP 443 and SSH.

The Traefik `v3` and Python image tags are bootstrap defaults. Pin tested versions
or digests when commissioning the host. Do not run automatic image updates.

## Public shares

Previews are tailnet-only. `devbox share` makes one public for a while, e.g. for a
coworker or to test SSO, through a Cloudflare Tunnel: no open ports, and nothing
reaches T3 or the dashboard. Shares live on their own domain on Cloudflare, not on
DEV_DOMAIN. Cloudflare's free certificate only covers one level below a domain, and a
separate domain keeps test apps away from your main domain's cookies.

Configure the host once in `~/.config/devbox/config.json`:

```json
{"share": {"domain": "example-share.com", "account_id": "<Cloudflare account ID>",
           "hosts": ["sso-1", "sso-2"], "default_hours": 8, "max_hours": 72}}
```

and put an API token with **Tunnel Write** and **DNS Write** for that domain in
`~/.config/devbox/cloudflare-token` (`chmod 600`). Optional keys: `zone_id` (looked up
from the domain otherwise), `token_file`, `prefix`, and `image` (the pinned `cloudflared`).

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

https://devbox.DEV_DOMAIN shows whether T3 and Traefik are up, days until the
certificate expires, a CPU/memory/disk summary bar that opens a system
monitor (10-minute history, swap, per-container usage and top host processes; containers
and processes are sampled only while it is open), previews (mode, health, branch/commit, uptime and
URL; a live preview shows its worktree's current commit and uncommitted changes, a
review how far its worktree has moved on; expand one for all its containers with ports and a details view: health,
networks, mounts, environment with secret-looking values masked, and labels), logs for any container (recent lines, or
followed live), each repository's worktrees with their last commit,
uncommitted changes, upstream distance and preview, clean-up candidates (what `devbox gc` would remove), certificate expiry,
and Docker disk use. Everything is read live from Docker labels and Git; there is
no database. It is dark by default; a header switch picks light or auto (follows the
system), remembered per browser.

Anyone on the tailnet can read it. To act from it, set a password on the host with
`devbox web password`; only a salted PBKDF2 hash is stored, in `~/.config/devbox/config.json`.
A browser that signs in can for 7 days start, stop, share, unshare and destroy previews,
start a live preview or review HEAD for a worktree, and clean up previews of removed
worktrees; changing the password signs every browser out. Each action runs the `devbox`
binary itself, as the CLI would, and the page shows its output live. The server only
accepts targets that exist on the host, requests from the dashboard's own page, and
typed confirmation for destroy; five wrong passwords lock an address out for five
minutes. `exec` stays in the CLI.

`devbox web` binds to 127.0.0.1:3780. Install `box/systemd/devbox-web.service` as a
user service (instructions in the file); the `devbox` router in
`box/traefik/dynamic.yaml` publishes it on the tailnet. Repositories appear once
they have at least one preview or a T3 worktree (under `~/.t3/worktrees`).

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
no fixed container names and no host port publishing. Review Compose uses the built
image and has no build entries or source mounts. `DEVBOX_URL` and `DEVBOX_COMMIT`
are available during rendering/building for project configuration.

The runner does not blindly copy `.env` from another checkout. Pass project-specific
development settings deliberately and generate per-preview connections/URLs. Never
include host secrets in the Docker build context. The supplied `.dockerignore` is
an allowlist for the sample. Rendered Compose config may contain environment secrets;
it is stored privately at `<git-common-dir>/devbox/runs/<preview>/compose.json`.
These are per-preview execution artifacts for restart/cleanup, not a task registry.

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

## References

- https://git-scm.com/docs/git-worktree
- https://docs.docker.com/compose/how-tos/project-name/
- https://doc.traefik.io/traefik/reference/install-configuration/tls/certificate-resolvers/acme/
- https://letsencrypt.org/docs/challenge-types/
- https://github.com/pingdotgg/t3code/blob/main/docs/user/remote-access.md
