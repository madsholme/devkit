@AGENTS.md

<!-- devkit-secrets begin sha256:4756ed054083c0f35d012b2fde77fd4b669b7492abbbd6ef266d0f5b866db231 -->
## Development credentials with Devkit

Use the devkit-secrets skill whenever creating, generating, saving or using development usernames, passwords or API tokens. Store credentials through Devkit's integrated vault so the human can find and copy them in this project's Secrets tab.

- Run from the intended project checkout. Check `devkit secrets context --json` and `devkit secrets list --json` before creating credentials; reuse suitable existing entries instead of silently replacing them. Follow the skill for registration or context mismatches.
- Save a username and its password together as a login entry (`--type login`) with a descriptive key such as `dev-admin`, not the username. Standalone passwords and API tokens remain single-value secrets. Do not silently combine or delete existing entries.
- Choose an explicit scope. Use worktree scope for temporary checkout credentials; choose branch or project scope only when sharing is intended. Worktree overrides branch, which overrides project. Never modify an inherited parent merely to create a local override.
- Generate and store passwords with `devkit secrets generate`; send existing values through protected stdin or hidden prompts. Follow the skill for paired-login input and explicit username/password environment mappings.
- Use `devkit secrets run` for runtime injection. Never put credential values in chat, Git, recipes, command arguments, logs or permanent dotenv files. Report only the entry name, scope and location in the Secrets tab.
- Saving or rotating a vault entry does not create or update the application's account. Apply credentials to the application only within the authorized task, and verify without printing them.
- Preserve existing Git/T3/agent workflows. Cleanup retention is configurable, including never and automatic off; do not change cleanup settings or delete credentials as an incidental step.
- If the vault is sealed or unavailable, report it and ask the human to restore access. Never fall back to plaintext storage or read host backend tokens, root tokens or unseal material.
<!-- devkit-secrets end -->
