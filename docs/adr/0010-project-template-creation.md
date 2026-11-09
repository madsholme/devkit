# 10. Explicit project creation from the embedded template

Status: accepted, 2026-11-05. Extends ADRs 4 and 5 with a narrow new-repository operation.

The user requested Create Project in the CLI and dashboard, preserving devkit agent
guidance, and chose Git initialization with an initial commit. Embed the small bundled
project-template in the Go binary so installed hosts need no source checkout or network
clone. Copy every starter file and dotfile, substitute the project slug, and append
optional notes without replacing existing guidance. Optional origin configuration does
not fetch, push or create a remote repository.

Use an explicit `project create` command beneath the existing authenticated job API.
Creation accepts only a validated name beneath configured projects_dir, reserves the
destination exclusively and refuses all existing paths. Notes are bounded and passed
over stdin instead of logged arguments. Require global Git identity before reservation;
run Git without inherited repository overrides, templates, hooks or signing prompts.
Retain partial directories on failure to avoid deleting concurrent user work.

The initial main commit is an intentional exception to the no-commit policy for existing
projects. Discovery remains read-only, and no worktree lifecycle authority is added.

## Extension: GitHub creation (2026-11-09)

The user explicitly requested optional GitHub repository creation with owner, repository
name, private-by-default visibility and optional initial push. This supersedes the
hosted-repository exclusion only when these options are selected; --remote remains
local configuration only. The existing host GitHub integration supplies credentials,
keeping the CLI standard-library-only and avoiding a separate gh installation/login.
Only github.com is supported. API identity/type preflight distinguishes personal owners
from organizations; GitHub enforces access. Create an empty repository after local commit,
validate response identity and visibility, then configure HTTPS origin. Push is explicit
and never forced, with exact main refspec and upstream tracking.

Tokens are ephemeral process configuration for this push; no project token binding or
global Git changes are made. HTTP requests are bounded and do not follow redirects.
Never expose upstream error bodies or process output that could contain credentials.
Partial failures retain both local and remote state with recovery guidance. Ambiguous
creation results require inspection, never automatic retry or remote deletion.
