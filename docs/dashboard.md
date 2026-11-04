# Dashboard

https://devkit.<domain> is titled with the host's name (its hostname, or `name` in the host config)
and preview domain, and shows whether the harness (e.g. T3) and Traefik are up; the certificate
appears there only when it expires within three weeks. The gear menu holds sign-in and the theme;
the footer shows which devkit commit runs. Below is a CPU/memory/disk/containers summary bar (it
also mentions reclaimable Docker data from 5 GB up) that opens a system monitor: 10-minute history, swap,
per-container usage, top host processes and Docker disk use. Containers and processes
are sampled only while it is open.

Below it are six tabs, remembered per browser and linkable (`#projects`, `#previews`, `#containers`, `#vms`, `#worktrees`, `#history`):

- **Projects** (default for new browsers): each main checkout with its linked Git worktrees,
  container previews and explicitly associated VMs. Configure `projects_dir` in the host
  config to discover projects before anything runs. Expand/collapse projects or filter by
  name, path, branch or VM; expansion is preserved on refresh. Signed-in users can launch
  containers, control configured VMs, and fetch/sync/push each Git checkout directly here.
  Sync only fast-forwards a clean branch tracking origin; Push is a separate action.
  Git, VM and container icons distinguish the resources while retaining text labels. Folders needing recipes stay visible.
  The read-only `/api/projects` catalog remains available with a warning if Docker fails;
  VM inventory and controls work independently. Paths and Git metadata are readable just
  like the Worktrees tab; recipe commands and VM disk paths are never returned.

- **Previews**: mode, health, branch/commit, uptime and URL. A live preview shows its
  worktree's current commit and uncommitted changes, a review how far its worktree has
  moved on. Expand one for all its containers with ports (a routed port lists where it
  answers: paths such as `/api` as links, subdomain routes as `*.<preview>`), a details view (health,
  networks, mounts, environment with secret-looking values masked, labels) and logs
  (recent lines, or followed live). Previews whose worktree is gone (what `devkit gc`
  would remove) are marked, with a notice above the list.
- **Containers**: all running and stopped Docker containers on this host, including
  containers outside devkit. Filter by name, project, image, ID or status. Rows show
  state, image, project and configured host-to-container port mappings (mappings may
  be inactive while stopped). Managed preview containers retain Details/Logs links;
  other containers are read-only. `/api/containers` exposes only this inventory metadata,
  never raw labels, environment, mount paths or commands. Probes are bounded to five
  seconds, 512 containers and 2 MiB per batch. Errors retain the last result with a
  visible warning and Refresh button. The tab refreshes while visible.
- **Virtual machines**: active standalone QEMU and libvirt VMs grouped by project, with display/headless icons,
  allocated RAM/vCPUs, disk capacity, graphics and host CPU/resident-memory usage. Expand
  **Connect privately** for SSH tunnels and remote-viewer or guest SSH commands. See below.
- **Worktrees**: each repository's worktrees with their last commit, uncommitted
  changes, upstream distance and preview.
- **History**: what devkit did, newest first and grouped by day (see below). The tab
  counts failures of the last 24 hours, and an expanded preview lists its last few events.

Everything is read live from Docker labels, Git, QEMU processes and libvirt; there is no database. It is dark
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
shares that expired and leftover tunnels or DNS records removed. QEMU cleanup records
successful stop, purge and registration removal separately so partial work remains visible. A failed command is
recorded too, with its error. Each event says who caused it: `cli` (also T3 actions),
`dashboard`, or `auto` for what the web service does by itself, such as expiring shares or the daily prune.
The log is one JSON line per event in `~/.local/state/devkit/history.jsonl`
(`$XDG_STATE_HOME/devkit` if set). At 1 MB it moves to `history.jsonl.1`, replacing the
older one, so about the last two megabytes are kept. Read it with `devkit history`
or on the dashboard; deleting it loses nothing but the past.

`devkit web` binds to 127.0.0.1 and `web.port` (3780). `devkit setup` installs it as the user
service `devkit-web`, and Traefik publishes it at `devkit.<domain>` on the tailnet. Repositories
appear from `projects_dir`, a preview, or a worktree matching the harness's `worktrees` glob.
The Projects tree also includes explicit `vm_projects` folders. See [project discovery](projects.md#discovering-projects-before-launch).

## Virtual machines

`devkit vms` lists the same inventory in a terminal; `devkit vms --json` returns the
read-only `/api/vms` response. Discovery needs no recipe, but project grouping uses
explicit `vms` name/UUID mappings in each project recipe (see [VM project mappings](projects.md#virtual-machines)).
Unmatched or ambiguous VMs appear under **Other VMs**, after the project groups.
Each VM includes a `project` slug (empty if unassigned) and, for conflicts, a
`project_note`; JSON remains a flat list. Filters match project names as well as VM
names. Group counts reflect the filtered rows.
The dashboard refreshes VMs while the Projects or VM tab is visible (every ten seconds, pausing
while a VM control has keyboard focus). It retains expanded connection sections.
A failed libvirt provider is a warning; other VM sources and Docker stay usable.

Discovery covers readable `qemu-system-*`/`qemu-kvm` processes and active domains
from `qemu:///session` and `qemu:///system` when `virsh` is installed. Session VMs
belong to the account running devkit. Guests with the same UUID deduplicate against
QEMU processes. Stopped guests with lifecycle settings remain listed; other stopped
libvirt definitions are not listed. Stopped standalone guests have no process to
inspect, so their allocation/display fields are unavailable until started. Standalone state means
host process state; it cannot tell whether the guest CPUs were paused via QMP.
Display type comes from configuration, not a check that a desktop has booted.

- A terminal icon means headless (`-display none`, `-nographic`, or no libvirt graphics).
  A monitor icon means a configured display. Unrecognized settings show **Display unknown**.
- RAM and vCPUs are configured allocations. Disk capacity is virtual capacity, excluding
  firmware and CD-ROMs, not host disk consumption. Simple local raw/qcow2 drives and
  libvirt block statistics are supported. Advanced standalone `-blockdev` graphs,
  network storage and unreadable disks may show **Unavailable**.
- CPU is sampled host usage: 100% means one core, and four busy vCPUs can reach 400%.
  The first sample is pending; the CLI's one-shot inventory has no CPU baseline.
  Resident memory is QEMU's host RSS, including overhead, not guest memory usage.
- Graphics lists configured virtual adapters. PCI passthrough is only called a GPU
  when host PCI class information confirms it. GPU utilization is not collected.

### Connecting from another device

Use the generated commands on your client computer, connected to the tailnet or
otherwise able to SSH to this host. They use the devkit user's account and configured
Tailscale IP (or detected tailnet IP, falling back to the hostname). Substitute your
SSH alias if you use a different destination/account. SSH authentication and host-key
verification remain your existing configuration; no private keys are read by devkit.

For SPICE on port 5931:

```sh
# Terminal 1 on your computer; keep this running:
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:5931:127.0.0.1:5931 mads@100.64.0.1
# Terminal 2 on your computer:
remote-viewer spice://127.0.0.1:5931
```

VNC uses `remote-viewer vnc://127.0.0.1:<port>`. Install the client with
`sudo dnf install virt-viewer` on Fedora or `sudo apt install virt-viewer` on Ubuntu.
If a local port is busy, change the first port in `-L` and the client command to the
same free port. End the tunnel with Ctrl+C. Display passwords are entered in the
viewer; devkit never reads or returns them.

Headless QEMU `-netdev user`, `-nic user`, and `-net user` TCP `hostfwd` entries to
guest port 22 get a private SSH tunnel and an SSH command that asks for the guest
username. Add your existing `-i` identity option if required. devkit does not infer
usernames, keys or bridged guest IPs. A headless libvirt guest also offers a serial
console through `ssh -t ... virsh --connect ... console <uuid>`; this requires a
configured guest serial console and libvirt permission. Ctrl+] leaves that console.

Only loopback or wildcard display/SSH listeners get automatic tunnel instructions.
Other addresses, Unix display sockets and TLS-enabled SPICE need the existing client
configuration. No display port is opened, no VM is modified, and VM connections
are not published through Cloudflare. Anyone who can read the dashboard can read
this sanitized inventory; connecting still requires SSH and guest authentication.

### VM lifecycle controls

VMs with [project lifecycle settings](projects.md#start-stop-and-restart-vms) show
Start when stopped, and Stop/Restart when running, after you sign in. Plain VM
mappings remain read-only. The page lists stopped managed guests too; register
VM-only projects through host `projects_dir` or `vm_projects` so their recipes remain discoverable.
Unavailable providers or conflicting definitions show an explanation instead of
action buttons. Paused guests offer Stop; resume with their existing manager
before restarting.

Buttons run the same CLI commands through the dashboard's authenticated job
system. The job window shows progress/errors, then refreshes VM state. A failed
stop prevents restart. There is no dashboard VM destroy action or public display tunnel.
The CLI [QEMU cleanup command](projects.md#clean-up-standalone-qemu-vms) can stop and
unregister guests by name or project, retaining files unless an explicit purge adapter
is requested. Removed registrations disappear on the next inventory refresh.
The API adds `control_id`, supported `actions`, and optional `control_note` to
managed rows, without exposing recipe paths or launcher command text.
