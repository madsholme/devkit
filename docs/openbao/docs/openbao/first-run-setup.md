# First-run installation and vault-first credentials

**Default installer choice: Managed OpenBao (Recommended).** A clearly labeled optional local secrets provider is permitted. Opting out must be explicit. Once secrets are vault-backed, they cannot silently fall back to local storage.

## Installation sequence

1. Inspect existing Devkit and OpenBao installation, volumes and initialization state. Never reinitialize a populated vault.
2. Configure persistent OpenBao service and internal connection; start it in production mode.
3. Guide the human through initialization and unsealing; deliver root/recovery/unseal material via secure one-time human ceremony, never agent output/logs/config.
4. Establish a restricted Devkit service identity, project scoping, KV v2, audit configuration and readiness checks; retire bootstrap root access.
5. **Only after the vault is ready**, ask for persistent integrations such as Cloudflare API token, GitHub/GitLab tokens, DNS provider tokens, database credentials and other sensitive Devkit settings. Store each directly into `kv/devkit/integrations/` or `kv/devkit/system/`; persist only references/metadata in ordinary Devkit settings.
6. Configure existing Traefik hostname `vault.devkit.<configured-domain>`; validate internal and Tailscale DNS/TLS as far as available.
7. Test credential retrieval and actual integration operation across Devkit restart; show masked storage-source status in CLI/UI.
8. Detect legacy plaintext secrets and offer guided migration with readback verification and approval before retiring source values. Inspect backups and Compose overrides for leftover plaintext copies.

## Cloudflare ordering problem

If setting up Cloudflare DNS/TLS normally happens before OpenBao is ready, **reorder installation** so local vault bootstrap comes first. If an API token is momentarily needed before vault readiness, hold it only in memory for a narrowly scoped operation; never write it to `.env`, YAML, Compose environment fields or CLI arguments. Pause and report a recoverable setup state if it cannot be stored safely.

For CLI, accept tokens through protected prompt or stdin (e.g., `devkit integrations cloudflare configure --token-stdin`), not arguments or shell history. CLI equivalents must exist for all settings actions.

## Secrets vs ordinary settings

- **In OpenBao:** API tokens, credentials, signing keys, integration passwords and sensitive configuration.
- **In standard settings:** domain names, ports, feature flags, vault references and non-secret metadata.
- **Minimal bootstrapping exception:** Protected service identity material needed to authenticate to OpenBao itself. Never root token or recovery keys in Devkit-accessible settings.

## Acceptance tests

- Fresh install with managed OpenBao, Cloudflare and at least two other integrations: credentials work after restart and can be rotated via CLI and UI.
- Scan generated `.env`, YAML/JSON/TOML, Compose files, database settings, logs and agent transcripts for **test plaintext credentials**; none must appear. Avoid printing actual secrets during tests.
- Sealed/unavailable vault: integration access fails closed, with diagnostics, never stale file fallback.
- Legacy import is verified before old copies are removed and supports rollback; local-provider opt-out is tested separately.
