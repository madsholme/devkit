# Secrets ownership and contextual views

Each project, branch and worktree has an independent scope with immutable Devkit IDs. Never authorize or clean up solely by mutable branch names or paths.

| View | Display | Owns |
|---|---|---|
| Project → Secrets | Project shared secrets | Project-shared records |
| Branch → Secrets | Branch records + inherited project records | Branch records only |
| Worktree → Secrets | Worktree + linked branch + project records | Worktree records only |

For a worktree, effective-value precedence is **worktree > branch > project**. A detached worktree doesn't inherit a branch without an explicit Devkit association. UI and CLI list operations show source (`project`, `branch`, `worktree`), inherited/owned state, and which value is effective. **Listing does not reveal values.** Reveal must be explicit; the ordinary frontend stays masked.

## Write behavior

- On a new secret, explicitly choose project/branch/worktree ownership; default to the current verified Devkit context, never an arbitrary shell path.
- Existing inherited entries cannot accidentally be modified through a child context: require an explicit scope or create an override.
- Provide explicit copy/promote actions to move a secret between scopes, with collision preview and ownership confirmation.
- Before creating a replacement, agents check whether a secret exists. Rotations must be distinguishable from ordinary updates.
- Avoid secrets in tracked files, process arguments, logs, errors or metrics. Plaintext `--reveal` is supported, but runtime injection is preferred when practical.
- Keep Devkit system integration secrets entirely outside project scopes.

## Shared-reference protection

Deleting a branch does **not** delete project-shared records or worktree-owned overrides. Worktrees actively referencing a branch must be reconciled before branch-owned secret deletion. Deleting a worktree does not erase branch or project data. Project deletion cascades into its owned scopes under the agreed 30-day retention policy.
