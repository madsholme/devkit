# Public shares

Previews are tailnet-only. `devkit share` makes one public for a while, e.g. for a
coworker or to test SSO, through a Cloudflare Tunnel: no open ports, and nothing
reaches the harness or the dashboard. Shares live on their own domain on Cloudflare, not on
the preview domain. Cloudflare's free certificate only covers one level below a domain, and a
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
`preview-<preview>.<domain>`, or at a fixed name from `hosts` with `--host sso-1`. Change
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

## Checking shares

`devkit doctor` checks shares without changing anything. It fails when the token cannot reach the
share domain, list the account's tunnels (Account / Cloudflare Tunnel) or read its DNS records, when
a share's `cloudflared` container is not running, or when Cloudflare reports its tunnel down. It
warns about a share past its expiry that is still public (a sign `devkit-web` is not running) and
about this machine's leftover tunnels and DNS records older than ten minutes. `devkit unshare
--expired` removes both. Whether the token may also create tunnels and DNS records only shows when
you share.

While shared, the preview's web service gets `DEVKIT_PUBLIC_URL` (the web container is
recreated to pick it up, and again when the share ends). Let the app accept that host,
e.g. Django's `ALLOWED_HOSTS` and `CSRF_TRUSTED_ORIGINS`, and build absolute URLs
and OAuth callbacks from it. A share is public: anyone with the link can open it.
Prefer review previews; a live preview may run with debug pages on.

## Managed credentials

With OpenBao enabled, sharing requires an OpenBao reference for its Cloudflare
API token. Use the explicit [verified import](secrets.md#integration-credentials-and-existing-host-import)
for existing source files. The per-tunnel token is delivered through an owner-only
memory-backed runtime file, not Docker environment/arguments. Unseal and restart
the affected consumer after reboot; there is no persistent plaintext fallback.
