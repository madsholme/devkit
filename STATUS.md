# Installation status

Installed and HTTPS commissioned on 2026-09-14.

## Host

- SSH: rene@192.168.1.21
- Ubuntu 26.04.1; Docker Engine 29.8.0; Compose 5.5.1
- Tailscale IPv4: 100.69.127.125
- Kit: /home/rene/devbox-kit
- CLI wrapper: /home/rene/.local/bin
- Demo repository: /home/rene/work/devbox-demo
- No pre-existing Docker workloads were altered.

## HTTPS and T3

- Existing wildcard DNS for *.dev.steenfeldt.net resolves to 100.69.127.125.
  No wildcard DNS changes were needed or made.
- Cloudflare token zone access verified after user corrected permissions.
- Let's Encrypt staging issuance passed; production issuance then passed.
- Certificate SAN: *.dev.steenfeldt.net only. Initial expiry: 2026-12-13.
- Production ACME state: /certs/acme.json in devbox-edge_certificates volume.
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

## Remaining work

- Check the second Mac and exercise an agent session against a real project.
- Adapt the recipe to the first real project.
- Configure registry/Dokploy publication to Hetzner and complete a team review.
- No host reboot test has been performed.

Credentials remain only in private host configuration, excluded from this kit.
Do not re-enable T3's Tailscale Serve on 443; Traefik owns that listener now.
