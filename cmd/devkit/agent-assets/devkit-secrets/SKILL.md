---
name: devkit-secrets
description: Use when generating or saving a development password, finding project credentials, running a command that needs secrets, or retiring a Devkit secret scope.
---

# Devkit secrets

Store development credentials in Devkit so the human can find and copy them in the project's Secrets tab. Never place values in chat, commits, recipes, command arguments, history, logs, or permanent dotenv files. Do not read Devkit's backend token, root/unseal material, or unrelated host credentials. This is a trusted single-user development host; the skill is workflow guidance, not OS isolation.

Run from the intended checkout:

```sh
devkit secrets context --json
devkit secrets list --json
```

If explicitly told the project is new, enroll it with `devkit secrets resources register`. Do not rebind a mismatched/moved context automatically. Explain the mismatch and request the intended identity. Git/T3/Codex/Claude own branches and worktrees; secret cleanup never removes them.

Choose the narrowest useful scope. Worktree values override linked branch values, which override project values. Detached checkouts inherit project values only. Saving an inherited name creates a child override; never rotate/delete its parent to work around a missing child.

To generate and save a temporary development password without exposing it:

```sh
devkit secrets generate DATABASE_PASSWORD --scope worktree --length 32 --json
```

To store an already generated value, send it through stdin using a pipe from the generating program (not a literal value in shell text), or use the hidden prompt:

```sh
devkit secrets set DATABASE_PASSWORD --scope worktree
# A generating program can pipe directly into:
# devkit secrets set DATABASE_PASSWORD --scope worktree --stdin
```

Use a **login entry** when a username and password belong together. Use a descriptive key such as `dev-admin`, not the username. Keep API tokens and unrelated values as ordinary single-value secrets. Do not silently combine or delete existing separate entries.

```sh
devkit secrets set dev-admin --type login --scope worktree
# Hidden prompts collect username and password.
devkit secrets generate dev-admin --type login --scope worktree --stdin
# A producer supplies JSON with username only; Devkit generates the password.
```

For paired input from a program, `set/update --type login --stdin` reads one JSON object with `username` and `password`; generation accepts only `username`. Pipe from the producer: do not embed either value in shell arguments, literal shell commands or chat. The entire entry inherits, versions, deletes and restores together. Type cannot change under an existing name.

To inject a login, explicitly set both `--username-env APP_USERNAME --password-env APP_PASSWORD` when saving/generating it. Unmapped logins are skipped by `secrets run`; do not assume their label is an environment variable. Mappings must be distinct and cannot collide with other effective secrets. `DEVKIT_`, `VAULT_` and `BAO_` names are reserved for injection. Ordinary single-value secret names are their environment names.

A requested field can be accessed with `get dev-admin --field username|password --reveal`; never paste the result into chat. A login's `rotate --scope ... --expected VERSION` generates only a new stored password and preserves the username/mappings. Updates require both fields through protected input; omitted mappings retain an owned login's existing mappings, and explicit `--no-env` removes them. Never change an application's account unless separately authorized.

Generation returns metadata only. For updates, use `--expected VERSION` from the owned record in `list`; a version conflict means reread metadata and reconsider, never force overwrite. `rotate` changes the saved password, not the application's account. Update the application separately when authorized.

Prefer runtime injection:

```sh
devkit secrets run --scope worktree -- your-command
```

The child receives effective values by environment. Do not use environment-dumping or tracing commands. Explicit `get KEY --reveal` is for a requested use; never copy its output into the conversation. Tell the human the key, scope, and where to reveal it in Devkit instead.

`history KEY` shows metadata. `delete KEY --scope worktree` and resource retirement return a preview and confirmation token. Show the affected scope/keys and obtain authorization for destructive changes unless already explicitly authorized. `restore` recovers stored values; it does not recreate Git resources or reset a remote password. Permanent purge cannot be undone from the live vault.

Retention is configurable: branch/worktree/project durations or `never`, with a global automatic-off option. Never assume a password expires after seven days. Do not change cleanup settings, enable overdue purging, manage OpenBao, or import operator credentials as part of a routine project task. When sealed/unavailable, report it and ask the human to unseal; never fall back to plaintext storage.
