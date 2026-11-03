# Plan: a self-hosted public edge for shares

Status: idea, not started (written 2026-10-03). Nothing here is implemented.

`devkit share` publishes a preview through a Cloudflare Tunnel today. This plan adds a
self-hosted alternative: a small public "edge" that several devkites share, so public
shares no longer need a Cloudflare account, token or tunnel per share. Cloudflare stays
available; a host picks one in its config.

"Edge" means this public piece only. The private Traefik on each devkit is the proxy
(`devkit proxy up`).

## Goals

- **Dumb edge.** It routes by hostname and forwards encrypted bytes. It holds no
  certificates for shares, never decrypts share traffic and knows nothing about
  individual shares.
- **Several devkites on one edge**, each with its own names and certificate.
- **No VPN requirement.** Works for hosts without Tailscale and for teams that won't let
  a public machine join their private network.
- **Nothing private is reachable.** A compromised edge can reach only what a box
  currently shares; T3, the dashboard and the host network stay out of reach.
- **Standard library only**, like the rest of devkit.

## Design

```
browser ──TLS──▶ edge (public VPS, :443)
                   │ reads the SNI hostname, picks the box, splices
                   ▼
           idle connection that box1 opened to the edge
                   │
                   ▼
box1: devkit-web ──▶ Traefik public entrypoint 127.0.0.1:8443 ──▶ preview
```

### Boxes connect out

Each devkit keeps a small pool (e.g. 4) of idle, authenticated TLS connections to
`edge.<share-domain>:443`. When a browser connects to `x.box1.<share-domain>`, the edge
reads the server name from the TLS ClientHello (sent in the clear), takes an idle
connection from box1's pool, sends a short "go" frame, replays the ClientHello bytes and
splices the two streams. The box connects that stream to its local share entrypoint and
opens a fresh idle connection to refill the pool.

One connection carries exactly one browser connection, so there is no multiplexing
protocol to write: plain `net`, `crypto/tls` and `io.Copy`. This works behind NAT and
firewalls with only outbound 443, and the same mode serves Tailscale users, so there is
one path to build and test.

### What the edge knows

- Its own certificate for `edge.<share-domain>` (ACME, HTTP-01 or DNS-01), so boxes can
  verify it. That is the only certificate it holds.
- A static list of boxes: name and a hash of the box's key (or a client certificate
  fingerprint). Adding a box is one line.
- Nothing per share. Unknown names, or a box with no idle connection, get the TCP
  connection closed (or a tiny fixed TLS alert).

### What each devkit does

- **Public-only entrypoint.** The proxy (`devkit proxy up`) gets a second Traefik
  entrypoint, e.g. `127.0.0.1:8443`, that only share routers use. T3 and the dashboard
  stay on the Tailscale entrypoint, so a forged hostname cannot reach them via the edge.
- **Its own certificate** for `*.box1.<share-domain>`, by DNS-01 like the preview
  certificate. A box can only serve its own names.
- **A share is a route.** `devkit share` adds a router for the share host on the public
  entrypoint; unshare and expiry remove it. Prefer a file-provider entry per share over
  container labels so routing changes don't recreate containers (the web container is
  still recreated for `DEVKIT_PUBLIC_URL`, as today).
- **The connector** runs inside `devkit-web`, which is always on and already expires shares.

### Names

- Shares: `<prefix><preview>.box1.<share-domain>`; fixed OAuth hosts become per box,
  `sso-1.box1.<share-domain>`, registered once per box with Google, GitHub, etc.
- DNS: one record per box, `*.box1.<share-domain> → edge IP`, created once at setup, plus
  `edge.<share-domain> → edge IP`.
- Box names are DNS labels; default to `machine()` (already used in Cloudflare names).

## Config sketch

On a devkit (`~/.config/devkit/config.json`):

```json
"share": {
  "via": "edge",
  "domain": "share.example.com",
  "box": "box1",
  "edge": {"address": "edge.share.example.com:443", "key": {"file": "~/.config/devkit/edge-key"}},
  "hosts": ["sso-1"], "default_hours": 8, "max_hours": 72
}
```

`"via": "cloudflare"` (the default, today's behaviour) keeps the current fields. The key
is a [secret source](setup.md#secrets).

On the edge (`devkit edge serve --config edge.json`):

```json
{"domain": "share.example.com", "listen": ":443",
 "boxes": {"box1": {"key_sha256": "..."}, "box2": {"key_sha256": "..."}}}
```

## Commands

- `devkit edge serve`: run the edge (on the VPS, as a systemd service).
- `devkit edge add-box NAME`: on the edge, generate a key, print it once, store its hash.
- `devkit setup`: on a box, ask Cloudflare or edge for shares; for edge, take the address
  and key, create the `*.box.<share-domain>` record if the DNS provider allows, get the
  certificate and add the public entrypoint.
- `devkit doctor`: check the edge connection (pool connected, key accepted), the share
  certificate and that `*.box.<share-domain>` resolves to the edge.

## Steps

1. Proxy: add the public entrypoint and per-share file-provider routes; keep Cloudflare
   working on top of it.
2. Edge server: SNI peek, box registry, idle pools, splice; tests with in-memory
   listeners. Size target: a few hundred lines.
3. Box connector in `devkit-web`: pool, reconnect with backoff, auth, local dial.
4. `devkit share` with `"via": "edge"`: names, certificate, routes, expiry; the existing
   lifecycle (stop/destroy/gc end shares) stays as is.
5. Setup and doctor for both sides; README section; history events (`edge connected`,
   `edge lost` maybe only as doctor/dashboard state, not history).
6. Try it with two boxes on one edge, including an OAuth login through a fixed host.

## Open questions

- Auth: shared key over TLS (simplest) or client certificates (no secret on the wire)?
- Edge certificate: HTTP-01 needs port 80 on the edge; DNS-01 needs a token there.
- Pool size and idle timeouts; how fast a burst of connections must be served.
- Should the edge rate-limit per box or per client IP?
- HTTP/3 (UDP) is out of scope; browsers fall back to TCP.
- Edge-side access logs: keep minimal (time, host, box, bytes), no paths, since the edge
  cannot see them anyway.
- A separate, related step: private routing without Tailscale (a setting for the private
  interface: NetBird, ZeroTier, WireGuard or LAN), so devkit works for teams without a tailnet.

## Compared with Cloudflare

Lose: Cloudflare's DDoS protection, origin hiding (only the VPS address is public),
Access and other add-ons. Gain: no Cloudflare account or token on boxes, near-instant
shares (no tunnel to create), any DNS provider, one design for every box. Cost: a small
VPS and one more machine to keep updated.
