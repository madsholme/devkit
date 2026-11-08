# Codex and Claude Code discoverability

Devkit ships one canonical `devkit-secrets` skill (see `../../templates/devkit-secrets/SKILL.md`) and agent-specific installation adapters. At initial agent setup and upgrades, detect supported installation directories for installed Codex and Claude Code versions, install/update idempotently and record version. Preserve user files.

## Managed project instructions

- Codex: merge the bounded `../../templates/AGENTS.md.fragment` into an existing `AGENTS.md` without replacing other sections.
- Claude Code: merge `../../templates/CLAUDE.md.fragment` similarly into `CLAUDE.md`.
- Ensure imported projects, new projects and worktrees receive applicable instructions. Worktrees may share tracked instructions; do not duplicate managed blocks on every creation.
- Instructions should remain short; the skill provides scoped workflows. Do not embed credentials or per-session secret values.

## Skill activation

The frontmatter description must mention passwords, tokens, API keys, secret creation/retrieval, environment configuration and scoped project/branch/worktree tasks so both agents naturally discover it. On trigger: check `devkit secrets context`, list existing names, select owner scope, use Devkit CLI (not raw OpenBao admin APIs), prefer injection, permit explicit reveal where required, never paste values in answers or tracked files.

## Doctor and repair

`devkit doctor --agents` checks both agent executables, skill versions, current context, managed instruction blocks, CLI resolution and vault access. Provide a repair/install command and matching Web UI control. Tests should start fresh Codex and Claude sessions in new project/worktree contexts and confirm correct tool selection **without a user reminding them**. Runtime authorization must remain effective even if instructions are ignored.

An MCP integration can be considered later; when added it must call the same scoped Devkit backend.
