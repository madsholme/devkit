# Project recipe

Use `devkit project create` for a new project, or copy/adapt `project-template` into ONE existing project. Give `devkit.json` a unique
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

## Create a project

Configure `projects_dir` in the host config and create that parent directory first.
Configure `git config --global user.name` and `git config --global user.email` for the
host account running devkit. Then use **Projects → Create Project** after signing in,
or run from any directory:

```sh
devkit project create my-app
devkit project create another-app --remote git@github.com:you/another-app.git --agents-file notes.md
# Read additional instructions from stdin:
cat notes.md | devkit project create third-app --agents-file -
```

The name is 1–32 lowercase letters, digits or hyphens, starting with a letter or digit.
Creation copies the built-in `project-template` into `projects_dir/NAME`: the sample Python
app, message, Dockerfile, live/review Compose files, `devkit.json`, T3 actions, AGENTS.md,
CLAUDE.md and both dotfiles. It sets the recipe's project slug, initializes Git on `main`
and commits all starter files once. The template is embedded in the binary; it does not
clone the devkit repository or its history. No files need to be removed during creation.
Adapt the sample app, Dockerfile and recipes together afterward for your own stack.

The optional remote accepts HTTPS or SSH (including `git@host:owner/repo.git`) without
embedded credentials, query strings or fragments. It only sets `origin`: it does not
create a hosted repository, verify remote access, fetch or push. Use an empty hosted
repository when you later publish this new history; importing an existing repository
is a separate workflow.

To create a new **github.com** repository at the same time, select **Create GitHub
repository** in the form or use:

```sh
devkit project create my-app --github-owner YOUR_USER_OR_ORG --github-repo my-app
# Explicitly publish the initial commit and track origin/main:
devkit project create another-app --github-owner YOUR_USER_OR_ORG --github-repo another-app --github-push
```

Repositories are **private by default**. `--github-public` (the form's **Public
repository** checkbox) makes one public. Initial push is off by default; select
**Push initial commit** or add `--github-push` to upload the template and appended
agent instructions. The dashboard repository name defaults to the project name.
CLI owner and repository name are both required. GitHub options cannot be combined
with `--remote`; GitHub Enterprise is not supported by this flow.

Follow [GitHub token setup](secrets.md#github-token-for-project-creation) before using
this option. Create a fine-grained token for the intended owner with **All repositories**
access, **Repository creation: Read and write** (or **Administration: Read and write**)
and **Contents: Read and write** for the initial push. Save it under **Secrets →
Integrations**, service `github`, key `token`, or run `devkit integrations github configure`.
The owner and push option are per-project choices, not global integration settings.

Creation reads `integrations.github.token` from host config using the existing secret
sources (OpenBao, private file, environment or command). A sealed/unavailable source
fails closed before local reservation. `devkit integrations github status --verify`
checks credential acceptance, not the exact creation/write grants; only a successful
create-and-push operation verifies the full workflow. GitHub enforces organization
policies and token permissions.

Creation never adopts an existing repository. It creates an empty remote only after
the local initial commit succeeds, then sets an HTTPS origin. Initial push uses the
host credential for this one operation, with hooks disabled, redirects refused, and
no force push. The token is never saved in the project, Git config, URLs, arguments,
job output or history. Later Git operations require separately configured credentials
(for example an [exact project credential binding](secrets.md#integration-credentials-and-existing-host-import));
the host integration token is not automatically shared with projects.

If GitHub rejects creation or its result is uncertain, the local project is retained.
Inspect the reported GitHub URL before retrying; devkit never retries creation or
deletes remote repositories automatically. If setting origin or pushing fails, the
created remote is retained too. For a failed push, inspect both sides, configure Git
credentials, and run `git push -u origin main` inside the retained project. Do not
rerun project creation against an existing local folder.

Additional instructions are UTF-8 text up to 16 KiB, appended under **Project-specific
instructions** in AGENTS.md. Existing devkit guidance stays intact; CLAUDE.md references
AGENTS.md. The modal lets you review that guidance. Instructions travel to the CLI over
stdin and are excluded from job output and history, but are committed in the new project:
do not put secrets in them.

Existing destinations are refused, including empty folders, files and symlinks. Concurrent
attempts cannot overwrite one another. Missing Git identity is reported before creating
anything. If copying or Git fails afterward, the partial folder is retained and its path
is reported; inspect it before manually removing it or choosing another name. Creation
runs no template hooks and starts no containers or VMs. The initial commit is the sole
exception to devkit's rule against committing changes in an existing project.

## Discovering projects before launch

Set `"projects_dir": "~/Work"` in the host's `~/.config/devkit/config.json`, using
whatever directory contains your main project folders. Every immediate non-hidden
folder appears in **Projects**, including folders without a recipe. Git supplies the
main checkout and linked worktrees, even when T3 or another tool places them elsewhere.
The main checkout is Git's original working directory, not necessarily the `main` branch.
Independent clones remain separate; repeated discovery of one repository is deduplicated.
Child symlinks and nested folders are not scanned. Keep explicit `vm_projects` for
VM recipes outside that parent.

Sign in to the dashboard and use **Launch containers** on the main checkout or a
worktree to run `devkit up` there. It needs a Git checkout and a valid recipe whose
`compose.live` file exists (or the default `compose.live.yaml`); it must stay inside
the checkout. A running preview has **Open app** and **Stop containers**;
a stopped one has **Start containers**. Reading the tree never executes hooks.

Associated VMs show their existing **Start**, **Stop** and **Restart** controls;
**Details & connect** opens their inventory and private connection instructions.
Each VM still needs an explicit mapping and lifecycle adapter below. Launching
containers never implicitly starts VMs, and launching a VM never starts containers.

## Read and edit documents

The dashboard's **Documents** tab reads Markdown from the selected project's main
checkout or linked worktree. Root `README.md` and `AGENTS.md`/`AGENT.md` are view-only (case-insensitive names); Markdown under
`docs/` can be created or edited after sign-in. To use the same behavior from the CLI,
run in the root of a registered project checkout:

The reader also displays relative project images and Mermaid flowcharts; see
[document media](dashboard.md#project-documents) for supported formats and limits.

```sh
devkit docs list
devkit docs read docs/plans/release.md
cat plan.md | devkit docs save docs/plans/release.md --expected new --file -
```

List and read produce JSON. `read` includes the exact content and its SHA-256 `version`.
For an edit, pass that version to `--expected VERSION` with the updated content supplied
through `--file FILE` or `--file -` (stdin). `new` only creates a nonexistent destination.
Paths must be non-hidden `.md` files under docs/; nested folders are created for new files.
The current folder must still be registered through project discovery when the save runs.

Saves preserve existing permission bits, use a sibling temporary file and atomic
publication, and serialize devkit saves per checkout. Version checks reject stale edits
and observed external changes. External editors do not share the lock: avoid editing the
same document simultaneously because a change after the final check can still race the
atomic replacement. No existing file is removed on failure, and failed drafts remain in
the dashboard editor. No commits, hooks, fetches or pushes occur. New files and edited
plans appear as ordinary Git working-tree changes.

The individual file limit is 256 KiB of UTF-8 text without NUL bytes. Symbolic/hard links
and special files are refused. Document contents travel to CLI jobs over stdin and are
excluded from job output/history; successful saves record the document path only.

## Existing Compose stacks

To show an existing stack under its project without recreating containers or moving
its data, add explicit registrations to the project's `devkit.json` (preserving any
existing VM entries):

```json
"existing_compose": [
  {"project": "shop-restored", "working_dir": ".validation/restored"}
]
```

Both values must match Docker's `com.docker.compose.project` and
`com.docker.compose.project.working_dir` labels. The directory is relative to this
recipe and must resolve within it. No container-name prefix matching is used.
Discover the project through `projects_dir`, `vm_projects`, or the harness worktree
glob; linked Git worktrees are included. Conflicting claims or incomplete discovery
block registration authority and show a warning. Removing the entry revokes access
on the next request. Registration itself makes no Docker changes.

The Projects and Containers tabs offer Details, Logs, and signed-in Start or
confirmed graceful Turn off. Start uses the existing full container ID: it does
not build, pull, create containers, or run Compose hooks. Start dependencies first
(for example PostgreSQL before an API); these are individual-container controls.
The existing container, mounts, certificates, ports and image remain in place.

```sh
devkit start --container EXACT_NAME_OR_FULL_ID
devkit stop --container EXACT_NAME_OR_FULL_ID
```

These are registered existing containers, not disposable devkit previews. They do
not acquire destroy, GC, review, or public-share actions. Registration does not
create a new URL or expose ports: use the original endpoints. Do not launch a
second writer against the same database to turn an active stack into a preview.

Only registered existing containers gain start/detail/log authority; unrelated
containers retain the earlier stop-only behavior. Details use the same environment
masking as previews. Logs and details become readable to dashboard viewers on the
private network, as for previews; register only stacks intended for that audience.

## Syncing a project with origin

Each Git checkout in Projects has signed-in controls for **Fetch origin**,
**Sync from origin**, and **Push to origin**. They run the same CLI commands:

```sh
devkit git fetch   # update origin tracking refs; keep working files as they are
devkit git sync    # fetch, then fast-forward the current branch from its origin upstream
devkit git push    # fetch/check, then push this commit to that exact origin upstream branch
```

Sync and Push require an existing commit, an attached branch tracking one branch on
`origin`, and a clean checkout (including staged, untracked and submodule changes).
Finish any merge, rebase, cherry-pick, revert or bisect first. If the branch is only
ahead, Sync retains those commits; use the separate Push action to publish them.
Diverged history is refused. Ignored local files are also protected from overwrite
when origin introduces a conflicting path. A behind branch must Sync before Push. No stashing,
rebasing, resetting, automatic commits, force pushes or upstream creation occurs.
Fetch remains available with local changes. Branch counts describe the last fetch;
opening/refreshing the dashboard never contacts Git remotes.

Configure credentials and the origin/upstream using Git on the host. Git controls
use existing Git/SSH configuration and hooks, do not prompt for passwords, and time
out after two minutes. Errors deliberately omit remote diagnostics that may contain
credentials. Commands serialize across a repository's linked worktrees; avoid
simultaneous manual Git edits while an action runs. Dashboard jobs revalidate that
the checkout still belongs to a discovered repository. Git actions for registered
projects remain available if Docker is unavailable. Successful actions and failures
appear in History. Syncing source does not rebuild or restart its containers or VMs.

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

Recipe discovery uses the host projects_dir folders, Docker preview worktree labels, the harness's worktree glob,
the current CLI directory, and readable QEMU working directories. For nested
working directories it reads the nearest recipe. A working directory only helps
find a recipe: the VM still needs an explicit mapping. No disk-path inference or
recursive home-directory scan is performed.

For a VM-only project outside projects_dir (particularly libvirt), add its directory to the host's
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

### Start, stop and restart VMs

Mappings do not authorize launcher commands. Native stop-only controls may be discovered independently. To enable start/restart and adapter lifecycle control, add `control` to
one unique VM definition. For standalone QEMU or a native engine, wrap your
existing launcher with start and stop commands:

```json
{
  "project": "swiftconfig",
  "vms": [{
    "name": "native-ubuntu22-task30",
    "control": {
      "start": "./scripts/native-vm start",
      "stop": "./scripts/native-vm stop",
      "timeout_seconds": 120
    }
  }]
}
```

The commands above illustrate a project adapter you supply; devkit does not ship
`native-vm` or change SwiftConfig's engine. Commands run as the devkit user with
`sh -eu -c` in that recipe's directory, like project hooks. They receive
`DEVKIT_PROJECT`, `DEVKIT_VM_NAME`, `DEVKIT_VM_UUID` (empty when not selected), and
the corresponding `DEVBOX_*` variables. Both commands are required. The launcher
must give the guest that exact stable QEMU `-name` or `-uuid`, detach it (for
example QEMU `-daemonize`), redirect its standard streams and return. The stop
adapter must request graceful shutdown of only that guest, preserve its disks,
and return. Use your engine's existing shutdown operation; do not use broad
process-name killing. These commands are trusted project code: devkit cannot
make an arbitrary script safe or prevent it from deleting files.

For an already defined persistent libvirt guest, use its UUID and local connection URI:

```json
{
  "uuid": "f845843c-ec18-4c42-8298-d8aec5c22286",
  "control": {"libvirt_uri": "qemu:///session", "timeout_seconds": 120}
}
```

`qemu:///system` is also supported with the devkit user's existing libvirt
permissions. No sudo, remote libvirt URI, VM provisioning or XML changes are
performed. Do not combine `libvirt_uri` with shell commands. Devkit uses native
`virsh start` / `shutdown`, and polls state because shutdown is asynchronous.
See the [libvirt command reference](https://www.libvirt.org/manpages/virsh.html).

```sh
devkit up --vm native-ubuntu22-task30      # start via the configured launcher
devkit start --vm native-ubuntu22-task30   # equivalent; already running is a no-op
devkit stop --vm native-ubuntu22-task30    # wait for shutdown; retain disks
devkit restart --vm native-ubuntu22-task30 # stop, wait, then start
devkit vms --json                         # includes stopped managed guests/control_id
```

Names and UUIDs resolve within the current project's recipe when present;
otherwise they must uniquely identify a discovered definition. The opaque
`control_id` shown by `vms` works anywhere and is what dashboard buttons use.
Use `projects_dir` or `vm_projects` above so the dashboard can still discover a VM's recipe after
its last guest stops. Settings are read again when an action runs; removing
`control` revokes it. No VM actions are implicit: `devkit up` without `--vm` is
an alias for the existing Docker `live` command. Docker `start`/`stop` keep their
existing meanings, and `destroy --vm` is unsupported.

Each start or stop step has a timeout (default 120 seconds, maximum 600), including
its command and state polling. A successful start means the process/domain is
running; it does not promise the guest OS, SSH or applications are ready. A restart
never launches the replacement until stop succeeds. A timeout reports failure
without escalating to a forced shutdown of a detached guest. Timed-out launcher
process groups are cancelled, including their shell children; a guest incorrectly
left in that group may be terminated too. Launchers must detach guests before
waiting for readiness. Check guest state before retrying. Paused guests must first
be resumed through their existing manager before a restart. Stop is available to
request graceful shutdown, which a paused guest may not be able to complete.

Only one devkit VM lifecycle operation runs at a time on a host, across both CLI
and dashboard. Conflicting definitions, duplicate live identities, provider
mismatches and incomplete process visibility disable control rather than guessing.
Unlike display-only grouping, duplicate control claims across worktrees are
ambiguous even with the same project slug: put control in one discovered recipe
and give worktree-specific guests unique identities. At most 128 discovered
control definitions are supported. Unavailable/missing libvirt domains remain
unavailable, not stopped. Inventory and private connection discovery still work
for guests without lifecycle configuration.

### Clean up standalone QEMU VMs

Cleanup stops a registered guest through its configured stop adapter and removes its
entry from the project recipe. Disks and other guest files are retained by default.

```sh
devkit cleanup --vm native-ubuntu22-task30                # dry run
devkit cleanup --vm native-ubuntu22-task30 --yes          # stop and unregister
devkit cleanup --project swiftconfig                     # list the whole batch
devkit cleanup --project swiftconfig --yes               # clean all registered QEMU VMs in that project
devkit cleanup --project swiftconfig --purge              # preview explicit purge adapters
devkit cleanup --project swiftconfig --purge --yes        # apply those adapters, then unregister
```

Choose exactly one `--vm` or `--project`. VM selection accepts an exact configured
name, UUID, or control ID, using the same current-recipe scope as lifecycle commands.
Project selection uses the exact recipe `project` slug across discovered recipes;
it includes stopped registrations. No name prefixes, arbitrary PIDs or guessed disk
paths are accepted. An unregistered guest must first have an explicit project mapping.

A stopped plain mapping can be removed without lifecycle commands. A running guest
needs its own configured stop adapter; plain mappings alone never permit cleanup to
terminate it. If any selected guest has missing authority, conflicting definitions,
unreadable inventory or an unsafe recipe, the whole batch is blocked before changes.
Libvirt-controlled/observed guests are not supported by cleanup; use their existing manager.

For optional data removal, add `purge` to the existing standalone control block:

```json
{
  "name": "native-ubuntu22-task30",
  "control": {
    "start": "./scripts/native-vm start",
    "stop": "./scripts/native-vm stop",
    "purge": "./scripts/native-vm purge",
    "timeout_seconds": 120
  }
}
```

This is an adapter you provide, not a devkit-supplied script. It must remove only this
guest's disposable resources and return a failure if cleanup is incomplete. Devkit
does not infer deletion paths or check that arbitrary trusted shell code preserves
shared images; the adapter owns that responsibility. Start and stop remain required.
The purge command receives the same working directory, `DEVKIT_*`/`DEVBOX_*` identity
variables and bounded process-group execution as the other lifecycle adapters. It
runs only with `--purge --yes`, after the guest is confirmed stopped. Libvirt control
blocks cannot include it.

Cleanup shares the lifecycle lock, checks fresh process identities and registrations,
and preserves unrelated recipe fields, VM entries and file permissions when writing.
Symlink recipes are refused; the legacy `devbox.json` filename is retained. JSON
formatting/key order may change. After registration removal the stopped VM disappears
from inventory; starting it through devkit again requires restoring its mapping and
control settings. The disks retained by default remain available for reuse.

A stop timeout never escalates to forced shutdown. Stop/purge failure halts the batch
and retains that guest's registration and all unattempted entries; already completed
targets stay removed and are reported. A successful stop or purge may have happened
before a later failure, so inspect the output before retrying. Successful stop, purge
and unregister steps are recorded separately in history. Observed concurrent recipe
edits abort the operation rather than overwrite them. The lock coordinates devkit
commands; pause external launchers/editors while cleanup runs.

This is a CLI command; the dashboard has no cleanup button. Its next inventory refresh
reflects removed registrations.

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


### Turn off an individual VM or container

Signed-in dashboard users can choose **Turn off** in Virtual Machines, in a project's
VM list, or in Containers. The confirmation identifies the resource. Existing VM
adapters still take precedence; other discovered VMs get stop-only native controls:
local libvirt shutdown by UUID, or QMP `system_powerdown` for standalone QEMU with
an absolute local Unix server socket (`-qmp unix:/path/to/qmp,server=on,wait=off`).
QMP must belong to the current user and the observed process. The guest must handle
its virtual power button. PID/start-time checks reject stale instances; devkit never
sends `quit`, `destroy`, or a kill signal to an observed VM. A VM without QMP or an
adapter explains why Turn off is unavailable. Resume paused resources first.

```sh
devkit vms --json                  # obtain the current control_id
devkit stop --vm live-...          # exact observed instance, stop only
devkit stop --container api        # exact name, resolved to a full Docker ID
devkit stop --container FULL_ID
```

Container stops address only that container, including containers outside devkit
projects. They use Docker's configured stop signal and `--timeout -1` to disable
the daemon's forced-kill fallback. Devkit reports a timeout after two minutes;
Docker may still be awaiting a later graceful exit. Auto-remove containers are
refused because stopping them could remove their data. A configured SIGKILL stop signal is also refused. VM disks, recipes, container
filesystems and volumes are retained. No native restart/start or cleanup authority
is implied by a discovered stop capability. Stopping a proxy/dependency may disconnect
the dashboard. Stopped plain-mapped VMs disappear from live inventory; configured
managed VMs remain listed and can use their existing start adapter.

### VNC and SPICE viewers

Both protocols are supported, independently or together. **Connect privately** shows
only the listeners actually discovered on that VM, with a separate SSH tunnel and
viewer command for each. A dual-protocol VM is labeled **VNC + SPICE** regardless of
argument order. For example, after opening its displayed tunnel:

```sh
remote-viewer spice://127.0.0.1:5931
remote-viewer vnc://127.0.0.1:5944
```

These are examples, not interchangeable endpoints. A VNC-only VM needs SPICE enabled
in its existing launcher before a SPICE connection can appear. Devkit does not add
listeners or restart guests to change their display protocol. Existing TLS/socket
configurations keep their documented client-configuration fallback.

## Project credentials

Project and worktree detail pages have a **Secrets** panel supporting single values and paired username/password login entries, with a branch selector
for branch-owned overrides. Enroll the intended checkout explicitly with
`devkit secrets resources register`. Worktree values override branch values, then
project values. Cleanup changes secret state only; existing tools continue to own
Git/worktree lifecycle. See [Development secrets](secrets.md) for CLI use, immutable
context, retirement/recovery and configurable retention, including automatic off.
