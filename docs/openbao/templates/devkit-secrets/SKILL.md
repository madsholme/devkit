---
name: devkit-secrets
description: Use when working in Devkit-managed projects, branches or worktrees and needing to create, generate, locate, retrieve, configure, inject, update, rotate or troubleshoot passwords, tokens, API keys, signing keys, database credentials or other development secrets. Use this for environment configuration and credential issues instead of writing plaintext .env files.
---

# Devkit secrets workflow

This is a **template**. Devkit's installer must ship and maintain a versioned canonical copy for both Codex and Claude Code after checking installed agent versions and skill paths.

1. Check `devkit secrets context --json`. Confirm the verified project, branch and/or worktree scope. If context is ambiguous, stop and request selection. Never infer authorization from a folder name alone.
2. Check `devkit secrets list --json` before creating/replacing a credential. Lists show names, ownership, provenance and effective source, not values.
3. Choose ownership deliberately: project-shared, branch or worktree. Inherited values may be viewed, but don't edit a parent accidentally. Worktree precedence is worktree > linked branch > project.
4. Generate with `devkit secrets generate KEY --length 32 --scope ...` or set via protected prompt/stdin. Do not put secrets in CLI arguments, Git-tracked `.env` files, commits or responses.
5. Retrieve when needed with `devkit secrets get KEY --reveal` in the appropriate authorized context. **Retrieval is allowed**, but plaintext may enter tool transcripts; prefer `devkit secrets run -- <command>` to inject into a process without printing a value when possible.
6. Use `devkit secrets rotate`, `history`, and explicit scope copy/promote operations when appropriate. Report names and status, not secret values.
7. Never read or alter `devkit/system` or another project's secrets. Never use raw root OpenBao tokens, seal keys or administrative APIs. Use Devkit's backend-authorized CLI.
8. Let Devkit manage cleanup. Branch and worktree secrets are retained 7 days after deletion, project secrets 30 days, then permanently destroyed. Do not manually delete OpenBao vault paths.
9. If OpenBao is sealed/unavailable, report that state and use `devkit doctor --vault`; don't use old local plaintext copies or generate replacement credentials without direction.

Use `devkit secrets --help`, `devkit vault status --json`, and `devkit doctor --agents` for discoverability and diagnostics. For detailed ownership rules, consult the Devkit docs installed with the project. This skill grants no permissions by itself; Devkit backend and OpenBao policies enforce access.
