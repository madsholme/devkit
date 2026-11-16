---
name: devkit-troubleshooting
description: Use when a devkit preview, dashboard, service, private URL or agent integration is unavailable, unhealthy, stale or failing, including DNS, TLS, LAN, Tailscale and container connectivity symptoms.
---

# Devkit troubleshooting

Locate the failing layer with bounded evidence before choosing a repair. A diagnostic
request does not itself authorize restarts, data deletion or host configuration changes.
Continue repairs already authorized by the user without asking again unnecessarily.

## Collect a small baseline

Identify the client and host, intended checkout/preview, exact URL, expected behavior
and failure. Read project guidance and the host's `~/.config/devkit/STATUS.md` when
available. From the owning checkout:

```sh
devkit version
devkit context --json
devkit ls --json
devkit history -n 10
devkit doctor --json
devkit logs
```

Use `devkit logs -s SERVICE` for the implicated service; never leave `logs -f` or an
unbounded log stream running. `devkit doctor --agents --json` checks installed skills and
project guidance. New agent sessions may be needed after installing skills.
Inspect only relevant configuration/metadata and redact credentials, cookies and
tokens from evidence. Do not dump container environments or vault values.

## Separate the layers

| Evidence | Next check |
|---|---|
| App error or unhealthy service | Relevant service logs and project health check; use the owning preview's `devkit exec` for application checks. |
| Old code after source changes | Actual checkout/commit, live versus review mode, application reload behavior, and whether images or saved Compose need `devkit rebuild PREVIEW --json`. Use devkit-previews. |
| Name does not resolve or resolves to the wrong network | Compare DNS answers on the affected client and host with configured private listeners. Check split DNS/rebinding filters. |
| Timeout before HTTP | Client LAN/VPN route, host interface/listener and applicable firewall/tailnet policy. A host-local success does not prove client access. |
| TLS hostname/certificate error | Preserve the intended hostname/SNI and inspect the certificate, DNS and proxy listener. Do not disable certificate verification as the fix. |
| Proxy 502/503 | Check whether the backend is listening/healthy and the route targets the intended service; distinguish brief startup failure from persistent failure. |
| Sealed/missing credentials | Use devkit-secrets; never read backend/root/unseal material or weaken authentication. |
| VM display or guest SSH issue | Use devkit-vms and actual listener metadata; discovery does not authorize a VM restart. |

For example, after verifying the address belongs to the intended host, test an HTTPS
route while preserving hostname validation (substitute the real preview and private IP):

```sh
curl --connect-timeout 5 --max-time 15 \
  --resolve demo.dev.example.com:443:192.168.1.20 \
  https://demo.dev.example.com/
```

Success with `--resolve` but failure with normal DNS points toward name resolution.
It does not change DNS or prove remote VPN access. Check the affected client's route;
do not substitute a Docker bridge address or expose public ports to bypass a missing VPN.

JSON reports use `schema_version: 1`, `complete`, `data` and checks with stable codes.
Unavailable evidence is not a healthy/stopped resource. Keep saved metadata, source
HEAD and observed image identity separate. If a rebuild failed, inspect its
`--recover --json` plan; do not clear its record or restart stale saved configuration.

## Repair and verify

Choose the smallest repair supported by the evidence and the user's authority.
Inspect service status/bounded logs before restarting anything. Preserve volumes,
unrelated previews, VMs and host services. Do not run setup, proxy regeneration,
firewall changes, destroy/gc/prune or secret rotation as speculative troubleshooting.
For an authorized configuration/binary change, retain rollback information and
verify the actual served version and affected behavior, not just command exit status.

After two failed attempts on the same issue, reassess the failing layer and approach
before another change. Report the cause/evidence, exact change, successful checks,
unverified client routes and any remaining blocker. Never present a host-only probe
or healthy process as proof of a working end-to-end user session.
