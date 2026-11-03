# Project recipe

Copy/adapt `project-template` into ONE real project. Give `devkit.json` a unique
project slug per clone on a host. It identifies the web service, its internal port,
and separate live/review Compose files. Common configuration can be factored out
later; the tiny sample deliberately keeps each file self-contained.

devkit was called devbox before. Projects from then keep working: without a `devkit.json`
it reads `devbox.json`, and every `DEVKIT_*` variable (`DEVKIT_URL`, `DEVKIT_IMAGE`,
`DEVKIT_PUBLIC_URL`, ...) is also set as `DEVBOX_*`. Rename the file and variables when
convenient; the old names will go away in a later version.

`devkit.json` also holds the project's hooks: shell commands run with `sh -eu`
in the project root. `prepare` runs before live and review, `check` and `build`
during review, `publish` for `devkit publish`. All are optional except `build`
(review) and `publish` (publishing). Prepare/check must not modify tracked review
inputs. Build must produce `$DEVKIT_IMAGE`. Compose files must have health checks, isolated named volumes,
no fixed container names and no host port publishing in Compose. The explicit
`media_ports` exception below lets devkit allocate Tailscale-bound media ports. Review Compose uses the built
image and has no build entries or source mounts. `DEVKIT_URL` and `DEVKIT_COMMIT`
are available during rendering/building for project configuration.

The runner does not blindly copy `.env` from another checkout. Pass project-specific
development settings deliberately and generate per-preview connections/URLs. Never
include host secrets in the Docker build context. The supplied `.dockerignore` is
an allowlist for the sample. Rendered Compose config may contain environment secrets;
it is stored privately at `<git-common-dir>/devkit/runs/<preview>/compose.json`.
These are per-preview execution artifacts for restart/cleanup, not a task registry.

## Virtual machines

Use the same `project` slug as your Docker previews, and list the VMs that belong
to it in `devkit.json`:

```json
{
  "project": "swiftconfig",
  "vms": [
    {"name": "native-ubuntu22-task30"},
    {"uuid": "f845843c-ec18-4c42-8298-d8aec5c22286"}
  ]
}
```

Merge `vms` into your existing recipe; keep its Compose settings and hooks. A
VM-only project can use just `project` and `vms`. Each mapping has exactly one
`name` or `uuid`, with at most 128 mappings per recipe. Names are exact and
case-sensitive (no prefix/glob matching); UUIDs are case-insensitive. QEMU gets
its identity from `-name` / `-uuid`; libvirt provides its domain name and UUID.
Find these with `devkit vms --json`. Existing launch scripts need no changes
when they already give their guests stable names. UUIDs are preferable when
names may repeat or change.

UUID mappings take precedence over name mappings. If several active VMs share
a mapped identifier, or different projects claim the same VM at the same priority,
those VMs stay in **Other VMs** with an explanation. The same project can repeat
its mapping across worktrees without causing a conflict. Grouping is informational;
it does not grant access or change VM ownership, lifecycle or SSH authentication.

Recipe discovery uses Docker preview worktree labels, the harness's worktree glob,
the current CLI directory, and readable QEMU working directories. For nested
working directories it reads the nearest recipe. A working directory only helps
find a recipe: the VM still needs an explicit mapping. No disk-path inference or
recursive home-directory scan is performed.

For a VM-only project (particularly libvirt), add its directory to the host's
`~/.config/devkit/config.json` so the dashboard can find it independently of Docker:

```json
"vm_projects": ["/home/mads/Work/SwiftConfig", "/home/mads/Work/PlayWarden"]
```

These must be absolute paths or start with `~/`, with at most 128 directories.
Docker discovery is bounded and optional; errors leave other sources usable.
At most 128 unique recipes are read per inventory refresh. Recipes can use the
legacy `devbox.json` name. Configuration edits take effect on the next refresh
(the inventory cache lasts five seconds); there is no VM or dashboard restart.
Neither project discovery paths nor disk paths are returned in VM API metadata.

## Media ports

WebRTC and other media protocols cannot travel through an HTTP preview route.
Opt a project into direct tailnet media by declaring named UDP/TCP listeners in
`devkit.json`:

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
port devkit allocated on the host. This is useful for an SFU that advertises its
own listener port. Each mapping gets a distinct port number in 20000–29999.

During **Compose rendering**, devkit supplies `DEVKIT_MEDIA_IP` (the local
`tailscale ip -4` address) and `DEVKIT_MEDIA_<UPPERCASE_NAME>_PORT` for each mapping:

```yaml
services:
  web:
    environment:
      PLENUM_ICE_PUBLIC_IPS: ${DEVKIT_MEDIA_IP:?}
      PLENUM_UDP_PORT: ${DEVKIT_MEDIA_SFU_UDP_PORT:?}
      PLENUM_ICE_TCP_PORT: ${DEVKIT_MEDIA_ICE_TCP_PORT:?}
```

Do **not** put `ports` in the Compose file: devkit inserts only the mappings
declared above, bound to its local Tailscale IPv4. Raw Compose port publishing,
`network_mode`, and fixed container names remain rejected. HTTP/WSS still use
the normal preview URL. Media variables are not available to prepare/check/build
hooks; use them for runtime configuration in Compose.

Allocation is serialized across CLI processes and repositories, checks listening
host sockets and Docker bindings, and reserves stopped previews' ports. Private
port lease files live in `~/.local/state/devkit/media-ports/` (under
`DEVKIT_STATE_DIR` or `XDG_STATE_HOME` when configured). Saved Compose retains the
chosen address/ports across stop/start. Destroy releases the lease; failed or
interrupted creation without saved Compose is reclaimed on the next allocation.
An unrelated host process can still claim a port before Docker binds it; Docker
then fails startup rather than changing or sharing the mapping. A changed
Tailscale address requires recreating the preview.

Tailnet/firewall policy must permit the selected media ports. These are private
tailnet listeners: a public `devkit share` only shares HTTP, and does not expose
media. TURN-TLS services must provide their own TLS certificate/configuration;
Traefik's HTTP routing does not terminate these raw TCP connections.

## Routes

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
speaks for the preview: its health, `devkit exec`/`logs` without `-s`, and shares, which publish the
preview's own host only.

Seed data must be idempotent and wait for database readiness. A one-shot service
(such as a separate seed job) must set `restart: "no"` in the live Compose file, or
the restart policy would rerun it whenever it exits. The sample does this
on startup without resetting existing data. For a separate seed service, gate app
startup on its successful completion and make it safe to rerun.

## T3 actions

`project-template/t3.json` declares T3 Code scripts for the devkit commands: live
preview, review of HEAD, logs and stop. T3 reads `t3.json` from the repository root
and offers its scripts for import in the project's scripts menu; import them once
per project. They run in a T3 terminal in the thread's worktree, so each thread
drives its own preview. Add project-specific ones there too, e.g. tests through
`devkit exec`. T3 ignores the whole file if any field is invalid; check it against
https://t3.codes/schema/t3.json. This repository's own `t3.json` has test and
dashboard scripts and sets the CRT logo as the project icon.

## Agent integration

`project-template/AGENTS.md` is an example of the instructions an agent needs: run
project commands with `devkit exec`, start `devkit live` when needed, commit and
`devkit review HEAD` to hand work over (reporting the URL and SHA), never `logs -f`, and
ask before anything that deletes data, goes public or deploys. Copy its "Previews with
devkit" section into your project's AGENTS.md (the template's `CLAUDE.md` imports it for
Claude Code) and fill in the project's own part.

Use Git worktree commands to discover existing workspaces, regardless of which
harness created them. The harness stays on localhost behind Traefik at
`<host>.<domain>` (T3: localhost:3773, https://t3.dev.example.com). If T3 used
Tailscale Serve on 443 before, remove that mapping only after the new HTTPS endpoint
is ready; clients then reconnect using the new address. Do not enable it again: it
would conflict with Traefik. No unverified automatic worktree hook is required.
