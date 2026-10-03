# Installation status

Installed and HTTPS commissioned on 2026-09-14.

## Host

- SSH: rene@192.168.1.21
- Ubuntu 26.04.1; Docker Engine 29.8.0; Compose 5.5.1
- Tailscale IPv4: 100.69.127.125
- Kit: /home/rene/devbox-kit
- CLI: /home/rene/.local/bin/devbox is a symlink to /home/rene/devbox-kit/bin/devbox
  (built from cmd/devbox since 2026-09-29; previously a Python script)
- Host config: /home/rene/.config/devbox/config.json (domain, harness T3, proxy, web, share);
  the DNS token is in /home/rene/.config/devbox/dns-token.
- 2026-10-03: moved to devbox setup. Traefik now runs from the generated
  ~/.config/devbox/proxy (same image digest and certificate); devbox-web.service
  is the unit setup writes, without DEV_DOMAIN. box/traefik and its private .env and
  dns-provider.env were removed. devbox doctor passes all checks. Later that day the
  command became `devbox proxy up` (Compose project devbox-proxy, config key "proxy");
  the certificates were copied into the devbox-proxy_certificates volume and the old
  devbox-edge volume and directory removed.
- Demo repository: /home/rene/work/devbox-demo
- No pre-existing Docker workloads were altered.
- 2026-10-02: installed opt-in `media_ports` support for per-preview TCP/UDP
  listeners bound only to the local Tailscale IPv4. HTTP-only projects retain
  the no-published-ports default. Allocations persist across stop/start.

## HTTPS and T3

- Existing wildcard DNS for *.dev.steenfeldt.net resolves to 100.69.127.125.
  No wildcard DNS changes were needed or made.
- Cloudflare token zone access verified after user corrected permissions.
- Let's Encrypt staging issuance passed; production issuance then passed.
- Certificate SAN: *.dev.steenfeldt.net only. Initial expiry: 2026-12-13.
- Production ACME state: /certs/acme.json in the devbox-proxy_certificates volume.
  Staging state remains separate at /certs/acme-staging.json.
- Traefik uses Linux host networking, bound to 100.69.127.125:443.
- The old Tailscale Serve 443 mapping was removed after trusted HTTPS worked.
- T3 backend remains localhost:3773. t3code.service is active, with user
  lingering enabled. No T3 sessions or data were moved or deleted.
- T3 pairing at the new address was confirmed working by Rene on 2026-09-14.

## Running endpoints

- T3: https://t3.dev.steenfeldt.net
- Overview: https://devbox.dev.steenfeldt.net (devbox-web.service, localhost:3780)
- Live: https://demo-main-live-9f7f4fac13.dev.steenfeldt.net
- Review: https://demo-40636547d6674a487f13-review-06a726b4b8.dev.steenfeldt.net
- Review commit: 40636547d6674a487f1335b3bf46aeaaa518149c

## Checks completed

- Nine automated checks on both Mac and Linux.
- Real Docker acceptance: independent live worktrees, slug collision avoidance,
  isolated mutable data, pinned review image, persistence through stop/start,
  Traefik hostname routing and cleanup of temporary resources.
- Production wildcard certificate verification.
- Final T3 and review URLs returned HTTP 200 with TLS verification 0 from the Mac.
- Both demo containers healthy on the server.
- Media-port checks (2026-10-02): Go race tests and vet; two concurrent temporary
  previews with four distinct TCP/UDP ports; echo traffic through the Tailscale
  address; stopped-preview reservation, separate review allocation and restart
  persistence. Temporary previews and leases were removed. Existing Docker
  runtime acceptance also passed. External-browser WebRTC remains a consuming
  project's acceptance test.

## Remaining work

- Check the second Mac and exercise an agent session against a real project.
- Adapt the recipe to the first real project.
- Configure registry/Dokploy publication to Hetzner and complete a team review.
- No host reboot test has been performed.

Credentials remain only in private host configuration, excluded from this kit.
Do not re-enable T3's Tailscale Serve on 443; Traefik owns that listener now.
