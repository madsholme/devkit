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
| `devbox ls` | Preview IDs, modes, web health/status, source commits and URLs |
| `devbox stop <preview>` | Stop containers; retain data |
| `devbox start <preview>` | Resume saved configuration; retain data |
| `devbox destroy <preview>` | Remove containers/networks and disposable named volumes |
| `devbox publish HEAD` | Call the project's publishing adapter with reviewed image ID |

Run lifecycle commands inside the owning Git repository. `ls` works anywhere.
To remove a finished worktree, use normal `git worktree remove <path>` after
stopping/destroying its live preview; Git refuses removal of uncommitted work.
Branches are retained until you explicitly delete them.

## Three modes

- Live: source bind mount, independent task data. The sample rereads `message.txt`
  on every request. Python code edits need a restart; adapt the recipe to your
  real application's hot-reload server.
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

Linux, Git, Python 3.10+, Docker Engine with Compose v2, `just`, Tailscale, and the
agent runtime of your choice. Install these using their official instructions after
checking the target host. Do not pipe installers into a shell without inspecting
the host and choosing versions. The installation is intentionally separate.

Install this CLI on the Linux host when ready:

```sh
install -d "$HOME/.local/bin"
install -m 0755 bin/devbox "$HOME/.local/bin/devbox"
export PATH="$HOME/.local/bin:$PATH"
export DEV_DOMAIN=dev.your-real-domain.com
```

Persist PATH/DEV_DOMAIN in the environment used by your agent service as well as
interactive shells. A shell profile alone does not configure a systemd service.

## Private routing

Create one DNS-only public A record:

```dns
*.dev.your-real-domain.com. A <Linux-host-Tailscale-IPv4>
```

The LAN address `192.168.1.21` is not the Tailscale IP. Get the latter with
`tailscale ip -4`. Names are flat, e.g. `demo-fix-login-live-<hash>.dev.example.com`.
The bounded readable name plus hash avoids branch-slug collisions.

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

## Project recipe

Copy/adapt `project-template` into ONE real project. Give `devbox.json` a unique
project slug per clone on a host. It identifies the web service, its internal port,
and separate live/review Compose files. Common configuration can be factored out
later; the tiny sample deliberately keeps each file self-contained.

The `justfile` contract is prepare/check/live/build/seed plus a publish adapter.
Prepare/check must not modify tracked review inputs. Build must produce
`$DEVBOX_IMAGE`. Compose files must have health checks, isolated named volumes,
no fixed container names and no host port publishing. Review Compose uses the built
image and has no build entries or source mounts. `DEVBOX_URL` and `DEVBOX_COMMIT`
are available during rendering/building for project configuration.

The runner does not blindly copy `.env` from another checkout. Pass project-specific
development settings deliberately and generate per-preview connections/URLs. Never
include host secrets in the Docker build context. The supplied `.dockerignore` is
an allowlist for the sample. Rendered Compose config may contain environment secrets;
it is stored privately at `<git-common-dir>/devbox/runs/<preview>/compose.json`.
These are per-preview execution artifacts for restart/cleanup, not a task registry.

Seed data must be idempotent and wait for database readiness. The sample does this
on startup without resetting existing data. For a separate seed service, gate app
startup on its successful completion and make it safe to rerun.

## Agent integration

Give your agent the following project instruction:

> Work in a separate worktree. Use just check for checks. Start devbox live only
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

Local validation commands: `python3 -m unittest discover -s tests -v` and
`python3 bin/devbox --help`. Full runtime acceptance requires a Docker daemon,
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
