# External secrets clients

Devkit exposes two APIs over its existing private HTTPS routes. Both are usable from another machine on your LAN or Tailscale when its DNS resolves to the appropriate private host address. Neither setup nor API-token creation opens a public listener, creates a public share, or changes firewall/DNS rules.

| Use case | Endpoint | Authentication | Behavior |
|---|---|---|---|
| Project secrets with Devkit inheritance and cleanup | `https://devkit.<domain>/api/secrets/v1` | Devkit bearer token | Same operations and registry as CLI/Secrets tab |
| App-owned native OpenBao storage | `https://vault.devkit.<domain>/v1/` | OpenBao token | Native KV semantics; no Devkit inheritance/cleanup |

For the example domain `sigvert.dev`, these are `https://devkit.sigvert.dev/api/secrets/v1` and `https://vault.devkit.sigvert.dev/v1/`. The native UI remains at `/ui/`. This documents external clients of the managed vault; connecting Devkit to a separately hosted OpenBao server is not supported.

## Issue a Devkit application token

Run on the Devkit host, from the registered checkout whose context the app should use:

```sh
cd /path/to/project-checkout
# Register explicitly first if this checkout is not already enrolled.
devkit secrets resources register

devkit secrets tokens create example-app --scope worktree \
  --permissions read,write,cleanup --ttl 720h \
  --output "$HOME/example-app.devkit-token"

devkit secrets tokens list --json
devkit secrets tokens revoke TOKEN_ID
```

`--output` must name a new file. Devkit creates it with mode 0600 and refuses existing files/symlinks. Without it, the credential is shown once on your controlling terminal. Ordinary command output contains only token metadata, including the ID used for revocation. Transfer the credential privately to the application; never place it in URLs, source control, chat, command arguments or application logs. The name is a non-secret label and must be unique among stored tokens.

Defaults: `--permissions read`, `--ttl 720h` (30 days). Duration syntax accepts hours/minutes/seconds; use `168h` for seven days. Explicit `--ttl never` disables expiry. There is no renewal API: create a replacement, update the application and revoke the old ID. Revocation applies to subsequent operations; an operation already authorized under the state lock finishes first. Expired tokens remain visible in the list until revoked.

The selected scope is fixed for that token:

- **project:** reads/writes shared project secrets. Cleanup applies to that project and its child secret scopes.
- **branch:** reads that branch plus inherited project secrets; writes/cleanup target that branch.
- **worktree:** reads that worktree plus branch/project inheritance; writes/cleanup target that worktree.

Tokens reference existing identities, not arbitrary client-supplied paths. Branch/worktree tokens stop working if the bound checkout changes branch or its identity no longer matches; create a token for the new context explicitly. Branch cleanup is the exception to the current-branch check: after Git switches away, it can retire/restore/purge the original bound branch if its recorded identity still matches. It never targets the newly checked-out branch. A moved, removed, ambiguous or destroyed context fails closed. Resource cleanup never deletes Git worktrees/files. A project token cannot select other branches/worktrees for ordinary reads/writes; issue separate tokens for those contexts.

| Permission | Allowed operations |
|---|---|
| `read` | `context`, `list`, `get`, `history`, `inject` |
| `write` | `set`, `update`, `generate`, `rotate`, `delete`, `restore` |
| `cleanup` | `cleanup-preview`, `cleanup-retire`, `cleanup-restore`, `cleanup-purge` |

Permissions are explicit and can be combined. Write permission alone does not allow secret reveal; responses may include the same metadata needed by the normal mutation workflow. Cleanup retains the existing preview/confirmation and active-branch protections. Host-wide cleanup configuration/resume/reconciliation, vault administration, integration credentials, agent installation, registration/binding, token management and arbitrary command execution are not available to application tokens. Read `inject` returns environment-name/value pairs; it does not run a process on the host.

## Request protocol

Use POST with JSON and `Authorization: Bearer <credential>`. Every response has `Cache-Control: no-store`. HTTPS is required for remote clients; direct loopback HTTP is allowed for local clients and the host's TLS proxy. There is no cross-origin browser API: requests carrying an `Origin` header are refused. Dashboard sessions continue to use the separate cookie-authenticated `/api/secrets` endpoint; this endpoint never falls back to dashboard cookies.

Omit `root`, `scope`, `project`, `branch` and `worktree`: the server supplies the granted context. An optional `scope`/`root` must exactly match the token; project/branch/worktree selectors are rejected. For cleanup, omit `resource` to target the bound scope; another resource ID is refused. The body is limited to 128 KiB; unknown fields and multiple/trailing JSON values are refused. Secret entry size limits still apply.

Examples of request bodies (values below are synthetic; real values belong in the request body, not URLs):

```json
{"operation":"list"}
{"operation":"get","key":"APP_PASSWORD","reveal":true}
{"operation":"generate","key":"APP_PASSWORD","length":32}
{"operation":"generate","key":"dev-admin","type":"login","username":"example-user","username_env":"APP_USER","password_env":"APP_PASS"}
{"operation":"get","key":"dev-admin","field":"password","reveal":true}
{"operation":"inject"}
```

Send each object as a separate request. Responses use the same `context`, `records`, `value`, `values`, `history`, `confirmation` and `message` fields as the CLI JSON protocol. List/context/history contain metadata only. Context resources/counts are limited to the token's ancestry. Revealing a login requires `field: "username"` or `"password"`; ordinary secrets use the default value field. One login stores both fields as one entry/version. `inject` respects explicit paired field mappings and refuses collisions/reserved destinations.

For creates, `expected` defaults to 0. For replacing an owned entry, set `expected` to its current record version; stale versions fail. `set`/`update` accept `value` for ordinary entries or `type: "login"`, `username`, `password` for pairs. `generate` generates a password; a login also needs its username. `rotate` changes only the password of an existing pair, preserving username/mappings. Explicit `clear_env: true` removes paired environment mappings during set/update. Rotation changes stored data, not an account in the consuming application.

Delete/restore and scope cleanup are two-step operations:

```json
{"operation":"delete","key":"APP_PASSWORD"}
{"operation":"delete","key":"APP_PASSWORD","confirm":"COPY_THE_RETURNED_CONFIRMATION"}
```

Inspect the first response before sending its exact confirmation. The same pattern applies to `restore`, `cleanup-retire`, `cleanup-restore`, and `cleanup-purge`. A separate `cleanup-preview` can inspect the bound resource, but its confirmation does not authorize a different operation. Confirmations include current registry/config state and become stale when relevant state changes. Purge permanently destroys the selected secrets. Inherited parent entries are not deleted by a child's delete/cleanup. The host's configured retention, `never`, automatic-off and overdue-resume barrier remain authoritative; API writes do not enable automatic cleanup.

HTTP status: 200 for success/preview, 400 for malformed requests, validation/CAS/context/lifecycle conflicts or a busy state lock, 401 for invalid/expired/revoked credentials, 403 for permission/context or transport restrictions, 405 for a non-POST request, and 503 when OpenBao is sealed/unavailable. Authorization is rechecked for every operation. Review state before retrying mutations; do not blindly repeat a possibly completed write after a network failure.

## Minimal external client

Store the issued credential in a private file on the application machine. This Python standard-library example uses it without placing the token in process arguments. It deliberately prints only a count:

```python
import json
from pathlib import Path
from urllib.request import Request, urlopen

endpoint = "https://devkit.sigvert.dev/api/secrets/v1"
token = (Path.home() / "example-app.devkit-token").read_text().strip()

def secrets_call(payload):
    request = Request(endpoint, method="POST", data=json.dumps(payload).encode(), headers={
        "Authorization": "Bearer " + token,
        "Content-Type": "application/json",
    })
    with urlopen(request, timeout=30) as response:
        return json.load(response)

result = secrets_call({"operation": "list"})
print("Effective secrets:", sum(row["effective"] for row in result["records"]))
# For application use, keep the returned value in memory; do not log it:
# password = secrets_call({"operation": "get", "key": "APP_PASSWORD", "reveal": True})["value"]
```

Devkit stores token hashes and scope metadata in `~/.config/devkit/secrets/api-tokens.json`, never plaintext credentials. The token store is excluded from Devkit vault backups/restores. On a replacement host, issue new tokens; vault snapshot recovery on the same host does not rewind token revocations. Manually restoring an old copy of the token file can reauthorize old credentials and is not the supported recovery procedure. API audit records identify the token ID, operation and resource/key, never request payloads or bearer values. Existing underlying operation audit events also remain present.

## Direct native OpenBao access

Use this route for storage managed by the external application itself. Do not write directly under `kv/data/projects/*` or `kv/data/devkit/*`: doing so bypasses Devkit's registry, version checks, inheritance and cleanup. Native entries outside those prefixes are not shown in Devkit's Secrets tab. They remain part of the vault's Raft storage/backups and follow native KV retention, not Devkit cleanup.

Native OpenBao requests use `X-Vault-Token`, not Devkit bearer tokens. Use a dedicated OpenBao identity/token restricted to the app's path. Do not distribute the human administrator or Devkit backend token. OpenBao documents the [HTTP API](https://openbao.org/api-docs/) and [KV v2 endpoints](https://openbao.org/api-docs/secret/kv/kv-v2/).

For example, a human administrator can provision an application policy for one app-owned entry. Save this non-secret policy as `external-example.hcl`:

```hcl
path "kv/data/external/example/login" {
  capabilities = ["create", "read", "update"]
}
path "kv/metadata/external/example/login" {
  capabilities = ["read"]
}
```

Authenticate with your existing human account using the native UI or `bao` CLI's hidden password prompt. Then install the policy and issue a short-lived, policy-limited token. With `bao` installed:

```sh
export BAO_ADDR=https://vault.devkit.sigvert.dev
bao login -method=userpass username=admin
bao policy write external-example external-example.hcl
# Choose a new private destination; do not overwrite an existing credential.
(umask 077; set -C; bao token create -policy=external-example \
  -no-default-policy -ttl=24h -explicit-max-ttl=24h -field=token > "$HOME/example.openbao-token")
```

Use your existing login name if it predates the `admin` default. The native child token also depends on its parent token's lifecycle; consult the [token creation reference](https://openbao.org/docs/commands/token/create/) for dedicated auth methods, renewal and parent/orphan choices. This example does not change Devkit's backend policy or expose its credentials.

The app reads `GET /v1/kv/data/external/example/login` with its `X-Vault-Token` header; protected fields are in `data.data`. Creating it uses POST to the same URL:

```json
{"options":{"cas":0},"data":{"username":"example-user","password":"example-password"}}
```

The managed `kv` mount requires CAS: use 0 only for a new entry and the observed current version for an update. Handle native KV errors and versions in the application. Keep headers/payloads out of logs and use normal TLS verification. A sealed vault rejects secret operations on both APIs. LAN/Tailscale access still needs working client DNS/firewall routing; no public-internet access is added by these instructions.
