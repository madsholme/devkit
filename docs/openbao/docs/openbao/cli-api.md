# CLI and backend API contract

**Every management action in the Devkit Web UI must have a documented CLI equivalent.** Exact syntax may be adjusted to match the repository's established CLI conventions. All commands support helpful `--help`, stable exit codes, `--json` for machine use, and explicit project/branch/worktree selectors where relevant. The backend—not the CLI—authorizes context.

## Managed vault lifecycle

```bash
devkit vault status --json
devkit vault setup
devkit vault unseal               # secure prompt/stdin, never key in argv
devkit vault health
devkit vault ui                   # open/print native URL
devkit vault config show --json
devkit vault config set <setting> <value>  # never pass sensitive values as argv
devkit vault backup create
devkit vault backup list
devkit vault backup verify <backup-id>
devkit vault backup restore <backup-id> --dry-run
devkit vault backup restore <backup-id>  # explicit human approval
devkit vault upgrade --check
devkit vault upgrade              # backed up, gated
devkit doctor --vault
```

## Secrets

```bash
devkit secrets context --json
devkit secrets list --project <id> --json
devkit secrets list --branch <id> --json
devkit secrets list --worktree <id> --json
devkit secrets generate DATABASE_PASSWORD --length 32 --scope worktree
devkit secrets set API_TOKEN --scope project       # prompt/stdin
devkit secrets get DATABASE_PASSWORD              # masked/metadata
devkit secrets get DATABASE_PASSWORD --reveal     # explicit plaintext
devkit secrets rotate DATABASE_PASSWORD
devkit secrets history DATABASE_PASSWORD
devkit secrets copy DATABASE_PASSWORD --from branch --to project
devkit secrets run -- npm run dev
devkit secrets cleanup preview <resource-id> --json
devkit secrets cleanup schedule <resource-id>
devkit secrets cleanup status --json
devkit secrets cleanup cancel <resource-id>
devkit secrets cleanup reconcile --dry-run
devkit doctor --agents
```

## Devkit integration settings

```bash
devkit integrations cloudflare configure --token-stdin
devkit integrations cloudflare status --json
devkit integrations cloudflare rotate-token --token-stdin
```

Provide equivalent commands for all configured integration types and agent installation/repair, plus project, branch and worktree create/list/switch/delete. No root OpenBao API operations may be reachable through an unprivileged project/agent identity.

## Backend behavior

- Authenticate caller and resolve verified Devkit project/branch/worktree context; never trust arbitrary user-supplied paths.
- Return names, scope, provenance, version and status to list calls; **never plaintext in ordinary listing**.
- Use explicit authorized secret-value retrieval for reveal, not a cached value exposed through metadata endpoints.
- Ensure CLI and UI share API, validation, audit trail, scope logic and deletion state machine.
- Token/key input from secure prompt/stdin; never required as shell argument or URL query.
- Use consistent typed errors: forbidden, not found, sealed, unavailable, conflict, pending deletion, expired and invalid scope.
- Support noninteractive scripts without reducing authorization or leaking credential values into stdout by default.
