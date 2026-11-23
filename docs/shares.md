# Public shares

Previews are tailnet-only. `devkit share` makes one public for a while, e.g. for a
coworker or to test SSO, through a Cloudflare Tunnel: no open ports, and nothing
reaches the harness or the dashboard. A separate Cloudflare domain is recommended for shares. Cloudflare's free certificate only covers one level below a domain, and a
separate domain keeps test apps away from your main domain's cookies.
A self-hosted alternative to Cloudflare is planned, not built: see [edge.md](edge.md).

Configure the host once in `~/.config/devkit/config.json`:

```json
{"share": {"domain": "example-share.com", "account_id": "<Cloudflare account ID>",
           "hosts": ["sso-1", "sso-2"], "default_hours": 8, "max_hours": 72}}
```

and put an API token with **Tunnel Write** and **DNS Write** for that domain in
`~/.config/devkit/cloudflare-token` (`chmod 600`), or name another [source](setup.md#secrets) with
`"token"`. Optional keys: `zone_id` (looked up from the domain otherwise), `prefix`, and
`image` (the pinned `cloudflared`).

`devkit share` publishes the current worktree's preview (or the one named) at
`preview-<preview>.<domain>`, or at a name you choose with `--host team-demo`. In the dashboard, select a
suggested address or choose **Custom hostname** and enter just the part before the
share domain. The full HTTPS address updates as you type. Use the copy icon beside it
to copy the address before sharing. Each suggested address also has a copy icon that works
regardless of which address is selected. Use 1–63 lowercase letters, digits or hyphens, starting and ending
with a letter or digit. The `hosts` list supplies the dashboard shortcuts. Change
the prefix with `"prefix"` (`""` for none); names over the 63-character DNS limit are
shortened with a short hash. Register
fixed names once as OAuth callback hosts with Google, GitHub and so on; Google does
not accept wildcards. Each share is its own tunnel and a `cloudflared` container next to the
preview, labelled with it, so the web overview shows it in the preview's group, marked
public with its URL and time left; `devkit shares` lists them. Running `devkit share`
again extends the expiry.

A share ends when it expires, on `devkit unshare`, and before `stop`, `destroy` or
`gc`: devkit removes the container, the DNS record and the tunnel. The web service
(`devkit web`) expires shares every minute; without it, run `devkit unshare --expired`.
It also removes this machine's leftover tunnels and DNS records, e.g. after a crash
mid-share. Tunnels are named `devkit-<machine>-<preview>` and DNS records carry the
comment `devkit share <machine>`; devkit never touches anything else in the account.

When the share and private preview are under the same domain, Devkit also routes
the exact share hostname on the private proxy for clients using split DNS. The
private preview URL stays active. Existing private routers take precedence, so pick
a share name distinct from other private hostnames. Removing the share removes
its private alias. Separate-domain shares continue to use public DNS.

## Checking shares

`devkit doctor` checks shares without changing anything. It fails when the token cannot reach the
share domain, list the account's tunnels (Account / Cloudflare Tunnel) or read its DNS records, when
a share's `cloudflared` container is not running, or when Cloudflare reports its tunnel down. It
warns about a share past its expiry that is still public (a sign `devkit-web` is not running) and
about this machine's leftover tunnels and DNS records older than ten minutes. `devkit unshare
--expired` removes both. Whether the token may also create tunnels and DNS records only shows when
you share.

While shared, the preview’s web service gets `DEVKIT_PUBLIC_URL`. A separate API or
authentication service can opt in by adding the Compose service label
`devkit.share.public-url: "true"` under `labels`. Devkit recreates participating
services with the selected HTTPS origin on share and removes it on unshare or expiry.
Read it at startup to configure exact allowed hosts and trusted origins. For an
existing saved preview, apply the label with the preview rebuild workflow first. Let the app accept that host,
e.g. Django's `ALLOWED_HOSTS` and `CSRF_TRUSTED_ORIGINS`, and build absolute URLs
and OAuth callbacks from it. A share is public: anyone with the link can open it.
Prefer review previews; a live preview may run with debug pages on.

## Managed credentials

With OpenBao enabled, sharing requires an OpenBao reference for its Cloudflare
API token. Use the explicit [verified import](secrets.md#integration-credentials-and-existing-host-import)
for existing source files. The per-tunnel token is delivered through an owner-only
memory-backed runtime file, not Docker environment/arguments. Unseal and restart
the affected consumer after reboot; there is no persistent plaintext fallback.

## Structured local inventory

`devkit shares --json` reports local connector metadata, its observation time and
UTC expiry. An invalid expiry is null with a warning; expired connectors remain
visible until explicit or scheduled cleanup. URLs must be valid credential-free
HTTPS origins matching their recorded host. Tunnel/DNS IDs are omitted. The command
does not contact Cloudflare, read its credentials, renew or remove anything; a local
record is not proof that its public endpoint is reachable.
