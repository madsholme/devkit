# Project creation — v1

## Intent and accepted behavior

Extend the existing codex/swiftdesign-ui branch with Create Project in Projects and
`devkit project create NAME [--remote URL] [--agents-file FILE|-]`. Use the configured
host `projects_dir` only. The user chose Git initialization on main with an initial
commit. A remote is optional: configure origin locally, never fetch, push or create a
hosted repository. Copy only the bundled project-template, including dotfiles; no
parent repository history or implementation files. Append notes to AGENTS.md under
Project-specific instructions without replacing the template text. CLAUDE.md continues
to reference AGENTS.md. No template hooks, containers or VMs start during creation.

## Safety and failure behavior

Validate a lowercase project slug (1–32 letters/digits/hyphens, existing recipe rules),
remote syntax (HTTPS or SSH; no embedded passwords/tokens, query strings, local paths
or external command transports), and UTF-8 notes up to 16 KiB. Refuse existing files,
directories and symlinks even if empty. An exclusive directory creation arbitrates
concurrent CLI/dashboard attempts. Parent-relative filesystem operations stay rooted
under projects_dir; exclusive file writes never overwrite. Git runs in the new pinned
directory with inherited Git repository/index overrides cleared, template hooks disabled,
no signing prompts, and a bounded timeout. Require configured global Git name/email
before creating the directory. Initialize only this new repository and make one initial
commit with the supplied template and appended notes. Never modify global Git settings.

Failures before creation leave no folder. If copying or Git fails after reservation,
preserve the partial folder and clearly report it; never recursively delete a potentially
user-modified directory. A retry then refuses that existing path. The authenticated
same-origin dashboard action invokes the same CLI, with notes passed over stdin, never
in command arguments, job output or history. Revalidate the configured parent at execution.
The action is an explicit exception to the existing rule that mutations only target
existing projects. It grants no arbitrary-path or arbitrary-command authority.

## Implementation and verification

1. Embed the source template and implement bounded, non-overwriting creation. Tests cover
   complete template/dotfile copying, recipe naming, retained agent text, main/initial commit,
   optional origin, existing paths/symlinks/traversal, concurrent attempts, missing identity,
   private notes and safe handling of Git failure. Keep Go standard-library-only.
2. Add CLI parsing, action validation/stdin transport and the shared-component modal.
   Show destination and preserved-template guidance, validate inputs, retain values on
   validation failure, refresh the catalog after success and keep normal job output.
   Sign-in and same-origin checks remain unchanged. Existing project files are not editable
   through this action. No arbitrary repository URL is cloned.
3. Verify real CLI creation in an isolated directory, authenticated job execution using
   the actual binary, browser success/duplicate/cancel flows and existing regressions.
   Update command/template/dashboard docs, reconcile the narrow Git-init/commit exception,
   rebuild embedded UI assets and commit on the branch. No new live deployment is implied.

## Checkpoint

Implemented on codex/swiftdesign-ui. CLI and authenticated dashboard use the embedded
11-file starter template, optional origin and appended notes with one initial main commit.
Documentation and ADR 10 record the explicit exception for new repositories. No scope deviations.

Verified: full Go suite and vet; project-creation tests with the race detector; frontend
contracts (9 tests), typecheck/build and Playwright (10 tests), followed by the focused
creation flow after removing a redundant modal Close button. Real HTTP → CLI → Git acceptance
passed with disposable projects and stubbed runtime tools, including stdin notes, origin,
initial commit, discovery refresh, duplicate rejection and private job/history output.
Desktop/mobile screenshots inspected. Static amd64 bundle built; dotfiles retained and
embed.go excluded from the exported template. Generated web/dist assets refreshed.
The live server is unchanged; review this feature through the branch preview.
