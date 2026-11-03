# Host setup

Linux, Git, Docker Engine with Compose v2 (usable without sudo), Tailscale, systemd
with lingering on (so user services run without a login); the agent tool is your choice.
devkit itself installs no system software. On Ubuntu, Debian (and derivatives such as Mint
or Pop!_OS) or Fedora, `./bootstrap.sh` installs and sets up all of these: run it from a
checkout as the user who will run devkit (not root; it uses sudo), and again at any time,
since it skips what is done. It uses Docker's package repository and Tailscale's installer,
adds you to the docker group (log in again after), runs `tailscale up` if the host is not on
a tailnet yet and enables lingering. If firewalld (Fedora's default) or ufw is active, it lets
traffic on `tailscale0` in, so the tailnet reaches Traefik and media ports; your tailnet's
access controls decide who can. Elsewhere, install them with their official instructions;
devkit checks for them. On Fedora, SELinux may stay enforcing: Docker's own packages run
containers without SELinux labels, so live previews can read the mounted worktree.

Building devkit needs Go (the version is pinned in `mise.toml`; `mise install` gets it).
Python 3 is only used by the Docker acceptance checks. You need a domain whose DNS provider
Traefik supports (Cloudflare gets the guided path) for the wildcard certificate.

devkit is a single Go binary with the dashboard and the Traefik configuration built in.
Build it, put it on PATH and run setup:

```sh
./bootstrap.sh    # Ubuntu/Debian/Fedora, once per host
go build -o bin/devkit ./cmd/devkit
install -d "$HOME/.local/bin" && ln -sf "$PWD/bin/devkit" "$HOME/.local/bin/devkit"
devkit setup
```

Releases on GitHub carry these bundles for amd64 and arm64: pushing a version tag
(`git tag v0.1.0 && git push origin v0.1.0`) runs `.github/workflows/release.yml`, which checks the
code, builds both and publishes them with checksums. For a host without Go, `./bundle.sh` (or `./bundle.sh arm64`) builds
`bin/devkit-linux-<arch>.tar.gz` here: the binary, `bootstrap.sh`, `project-template`, README, docs and
LICENSE. Copy it over, unpack it, run `./bootstrap.sh`, then `install -D devkit ~/.local/bin/devkit`
and `devkit setup`.

`devkit setup` asks for each setting, offering the current one as default, and can be run
again at any time to change something or repair the host. It:

1. Checks Docker, Compose, Tailscale and systemd, and notes if lingering is off (`sudo loginctl
   enable-linger $USER`, or `./bootstrap.sh`, keeps user services running after you log out).
2. Asks for the preview domain (`dev.example.com`), the agent harness (`t3`, `none` or
   `custom`), the email for the Let's Encrypt account, the DNS provider, and any addresses
   besides the Tailscale IP Traefik should listen on (see [Beyond the tailnet](#beyond-the-tailnet)).
3. Asks where each DNS credential comes from (see [Secrets](#secrets)) and checks it; a
   Cloudflare token needs Zone / DNS / Edit and Zone / Zone / Read for the domain's zone.
4. Checks that `*.<domain>` resolves to the Tailscale IP. On Cloudflare it offers to create the
   DNS-only record `*.<domain> A <tailscale ip>`; elsewhere create it yourself.
5. Starts Traefik (`devkit proxy up`). Without a trusted certificate yet, it first gets one from
   Let's Encrypt's staging service, whose rate limits forgive mistakes, then switches to production.
6. Installs and starts the dashboard as the user service `devkit-web`, running this binary.
7. With the `t3` harness (the default), makes sure T3 Code answers on `127.0.0.1:3773`. If it is
   missing, it offers T3's official installer (`curl -fsSL https://t3.codes/install.sh | sh`, a
   self-contained `t3` in `~/.local/bin`) and `t3 service install` (the user service `t3code`),
   or starts the service if it is only stopped. Pair a device with `devkit pair` or the dashboard. Don't
   run `t3 up`: Traefik already serves T3 at `t3.<domain>`, and Tailscale Serve would take
   port 443. T3 Code wants about 1.5 GB of memory; give a T3 host at least 2 GB.
8. Runs `devkit doctor`.

`devkit setup --yes` takes every default without asking (`--domain`, `--email`, `--harness`
set them) and fails where there is none, e.g. a credential that is not configured yet.
`devkit doctor` checks the same things at any time: config, Docker, Tailscale, DNS, the proxy
network, where Traefik listens, Traefik, the certificate (trusted, days left), the dashboard service, lingering, the
harness and the Cloudflare tokens. Each line ends with how to fix it; it exits non-zero when a
check fails.

Rebuild after pulling changes; replacing the binary is safe while `devkit web` runs, which
then needs `systemctl --user restart devkit-web`. If you move the binary, run setup again so
the service follows it.

## Host config

Everything lives in `~/.config/devkit/config.json` (`DEVKIT_CONFIG_DIR` moves the directory),
which setup writes and which you may edit:

```json
{
  "domain": "dev.example.com",
  "harness": {"name": "T3 Code", "host": "t3", "url": "http://127.0.0.1:3773",
              "worktrees": "~/.t3/worktrees/*/*"},
  "proxy": {"email": "you@example.com", "dns_provider": "cloudflare",
            "dns_env": {"CF_DNS_API_TOKEN": {"file": "~/.config/devkit/dns-token"}},
            "listen": []},
  "web": {"port": 3780},
  "prune": {"build_cache_gb": 30, "image_age_days": 7, "daily": true},
  "share": {"…": "see Public shares"}
}
```

- `name`: optional; the dashboard's title, the hostname by default.
- `domain`: previews are `<name>.<domain>`, the dashboard `devkit.<domain>`. `DEV_DOMAIN`
  overrides it, e.g. for tests.
- `tailscale_ip`: optional; read from `tailscale ip -4` when empty.
- `harness`: the agent tool. `name` is shown on the dashboard with whether `url` answers,
  Traefik routes `<host>.<domain>` to `url`, and repositories with worktrees matching the
  `worktrees` glob appear on the dashboard. `null` for none.
- `proxy`: `dns_provider` is a [lego provider](https://go-acme.github.io/lego/dns/) and `dns_env`
  its credentials by variable name. Optional: `image` (a pinned Traefik), `staging`, and
  `listen`, addresses Traefik takes HTTPS on besides the Tailscale IP (see
  [Beyond the tailnet](#beyond-the-tailnet)).
- `web.port`: where `devkit web` listens on localhost.
- `prune`: optional; what `devkit prune` keeps. `build_cache_gb` of Docker build cache
  (default 30), unused images newer than `image_age_days` (default 7), and `daily: false`
  stops the web service pruning by itself (default true; read each day, no restart needed).

After editing `domain`, `harness`, `proxy` or `web.port`, run `devkit proxy up`; doctor says
when Traefik is out of date.

## Secrets

Every credential setting (`proxy.dns_env.*`, `share.token`) takes exactly one source:

- `{"file": "~/.config/devkit/dns-token"}`: a file only you can read (`chmod 600`). This is
  what setup writes when you type a secret.
- `{"env": "CF_DNS_API_TOKEN"}`: an environment variable, so a secret manager can inject it:
  `op run --env-file=devkit.env -- devkit proxy up` (1Password), `bws run`, `doppler run`, ...
- `{"command": "op read op://dev/cloudflare/token"}`: a command that prints it, run each time
  it is needed (`sh -c`), so nothing is written to disk.

Traefik needs the DNS credentials for as long as it renews certificates, so `devkit proxy up`
hands them to its container through the environment of `docker compose`. They are not written
to the proxy directory, but Docker keeps them in the container's configuration.

## Private routing

Traefik (`devkit proxy up`, Compose project `devkit-proxy`) runs with host networking and binds
HTTPS only to the Tailscale IP, unless `proxy.listen` adds addresses (see below). Its files are generated into `~/.config/devkit/proxy/`; edit the
host config rather than these. It requests one wildcard certificate for `*.<domain>` by DNS-01,
so no public inbound port is needed, routes `devkit.<domain>` to the dashboard and the harness
host to the harness, and takes preview routes from Docker labels on the `devkit-proxy` network.
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
development host, not hostile tenants. Only expose the application frontend to `devkit-proxy`;
keep databases on their Compose-scoped networks. Do not publish database ports. Host
firewall/Tailscale policy should grant the intended users access to TCP 443 and SSH.

Traefik's image is pinned (`proxy.image` overrides it). Do not run automatic image updates.

## Beyond the tailnet

For devices that can't join the tailnet, such as a TV or a test phone, Traefik can also listen on
other addresses of the host. `proxy.listen` (or setup's question) lists them:

```json
"proxy": {"listen": ["192.168.1.20"]}
```

- An address of this host, IPv4 or IPv6, such as its LAN address. Several are fine; each gets
  its own Traefik entrypoint. `devkit proxy up` and doctor fail if the host doesn't have one,
  e.g. after DHCP handed out another, so give the host a fixed address.
- `0.0.0.0` (or `::`, IPv4 and IPv6) for every interface. It covers the others, so Traefik binds
  only that. On a host with a public address, this puts the dashboard and every preview on the
  internet; `devkit proxy up` and doctor say so. Let a firewall decide who gets in.

The Tailscale IP is always included; the public DNS record keeps pointing at it. Devices off the
tailnet need DNS that answers `*.<domain>` with the other address, typically a local DNS
override (split DNS) in your router, Pi-hole or AdGuard Home. The certificate is the same
wildcard on every address. `bootstrap.sh` only opens the firewall on `tailscale0`, so open
port 443 for the network yourself, e.g. `sudo firewall-cmd --permanent --add-service=https` and
`sudo firewall-cmd --reload`, or `sudo ufw allow from 192.168.1.0/24 to any port 443 proto tcp`.

Everyone who reaches one of these addresses gets what the tailnet gets: the dashboard (its
actions still need the password), every preview, and the harness at `<host>.<domain>` (T3 Code
behind its own pairing). Media ports stay on the Tailscale IP. After changing `listen`, run
`devkit proxy up`. Previews started by an older devkit answer on the new addresses once they are
next started (`devkit start`, `live` or `review`).

## References

- https://doc.traefik.io/traefik/reference/install-configuration/tls/certificate-resolvers/acme/
- https://letsencrypt.org/docs/challenge-types/
- https://github.com/pingdotgg/t3code/blob/main/docs/user/remote-access.md
