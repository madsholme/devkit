---
name: devkit-projects
description: Use when creating a Devkit project, onboarding an existing app, adapting devkit.json or legacy devbox.json and live/review Compose recipes, or registering an existing Compose stack with Devkit.
---

# Devkit projects

Choose the workflow from the existing state: create a new project, adapt an existing
checkout for isolated previews, or register a running Compose stack in place.
Registration and preview creation have different storage and lifecycle contracts.

## Establish the project

Use `devkit projects --json` and `devkit containers --json` for local inventory,
including outside a checkout. Inspect `complete` and failed checks: Docker being
unavailable is not an empty or stopped environment. Match exact identities and
revalidate current registration before acting; listed capabilities and `live_recipe`
(declared recipe presence) never authorize launch or container recreation.

Read project guidance, Git state, Dockerfile, Compose files, hooks and the recipe.
Use `devkit.json` when present; otherwise `devbox.json` remains supported, as do
`DEVBOX_*` aliases of `DEVKIT_*`. Edit the effective recipe, preserving VM mappings,
routes, existing registrations and unknown fields. Do not introduce a partial
`devkit.json` that shadows a complete legacy recipe. Keep the project's slug stable
across linked worktrees; independent clones on the same host need distinct slugs.

For a new project, check configured `projects_dir`, Git identity and the unused
destination, then run `devkit project create NAME`. It copies the embedded Python
sample, initializes main and makes one initial commit; it starts no services.
Adapt the app, Dockerfile and recipes together for the requested stack. Existing
folders are refused: do not use creation to onboard a repository or delete a partial
result just to retry. Git and the agent's existing tools own later commits/worktrees.

`--remote URL` only sets origin. GitHub repository creation is separate, private by
default, and requires an explicitly selected owner/name; `--github-public` and
`--github-push` are explicit choices. After partial failure, inspect the retained
local folder and reported remote. For an uncertain remote result, establish whether
creation succeeded before retrying. After a failed initial push, verify origin and
the intended commit, configure Git credentials, then push the retained main branch
with `git push -u origin main` when authorized. The host creation token is not
automatically available to later Git commands. Never recreate or delete the remote
to recover a failed push.

## Adapt an existing app for previews

Reuse its stack and commands. For an API listening on internal port 3000, merge
these fields into the existing recipe, retaining the real project slug:

```json
{
  "project": "shop",
  "web_service": "api",
  "web_port": 3000,
  "compose": {"live": "compose.live.yaml", "review": "compose.review.yaml"},
  "hooks": {"build": "docker build -t \"$DEVKIT_IMAGE\" ."}
}
```

- Live Compose mounts the intended source and runs the app's development command.
  The web process must listen on the container interface, not just loopback.
- Give services health checks (for example the API's `/health` and PostgreSQL
  readiness). The health endpoint belongs in Compose, not a new recipe field.
- Persist preview data in Compose-scoped named volumes without fixed global names
  or external/shared database volumes. Remove fixed `container_name`, host `ports`
  and `network_mode` overrides from preview files. Explicit recipe `media_ports`
  is a separate supported exception for private media traffic.
- Keep existing data and deployment files intact. Do not attach previews to an
  active database's bind directory or start a second writer. Use empty isolated
  development data or an authorized backup/restore into a new preview volume.
- Review Compose uses `${DEVKIT_IMAGE}` for the built app, with no `build:` entries
  or source mounts; database services use their own appropriate images/volumes.
- Hooks run with `sh -eu` in the project root. `prepare` precedes live/review;
  `check` and required `build` run during review. Prepare/check must not modify
  tracked review inputs. Build must produce `$DEVKIT_IMAGE`; an optional publish
  workflow needs a `publish` hook. Exclude credentials and database files from the
  build context. Use devkit-secrets for credential storage/injection.

Run `devkit project validate --json` before launch; `--runtime containers` restricts
it to containers, and `--runtime vms` or `--vm TARGET` handles VM recipes. Inspect
`complete` and every failed check. Validation runs no hooks and does not prove
runtime health. Missing application interpolation inputs remain requirements;
never fabricate passwords to make validation pass. Compose input inspection accepts
plain-key YAML and literal short-form local includes/env files within the checkout;
unsupported notation must be simplified explicitly, not bypassed with a launch.
Use `devkit context --json` to match checkout and saved/runtime identities. Use
devkit-previews to start the owning live preview, run project checks via `devkit exec`,
and verify health and application behavior. When independent worktrees are
part of the task, verify distinct URLs, containers and data. Review the intended
committed revision under the user's Git authority; do not claim review success from
uncommitted changes or silently recreate a saved preview with valuable volumes.

### Custom share hostnames

The routed web service receives `DEVKIT_PUBLIC_URL` automatically while shared.
For a separate API or auth service that validates hosts or origins, merge these
keys into its service in **both** live and review Compose:

```yaml
labels:
  devkit.share.public-url: "true"
environment:
  DEVKIT_URL: ${DEVKIT_URL:?Run through devkit}
```

Derive exact allowed hostnames and trusted HTTPS origins from the private
`DEVKIT_URL` and optional `DEVKIT_PUBLIC_URL` at process startup. Keep necessary
loopback health checks. Avoid wildcard trust, build-time public URLs and
request headers as sources of trusted origins. Use relative browser API URLs
and proxy them through the web service. The label delivers an environment value;
the app still configures its framework and the web service routes API traffic.
Apply recipe changes to existing saved previews using the rebuild workflow.

## Register an existing Compose stack

For an observed Compose project `shop-restored` whose working-directory label is
`/work/shop/restored`, merge this into `/work/shop/devkit.json`:

```json
{"existing_compose": [{"project": "shop-restored", "working_dir": "restored"}]}
```

Match both `com.docker.compose.project` and
`com.docker.compose.project.working_dir` labels, not container-name prefixes.
The directory must resolve inside the recipe checkout. Ensure project discovery
includes that checkout; conflicting claims or incomplete discovery block authority.
Preserve existing entries and verify the exact container IDs, mounts and endpoints.

Registration does not launch/recreate containers, move data or add a preview URL.
It allows registered details/logs and start by exact existing container identity;
graceful stop retains data. Start dependencies first when requested. It grants no
review, share, destroy or GC actions. Details/logs become visible to dashboard
viewers on the private network, so register only the intended stack.

After authorized onboarding, install project guidance with
`devkit agents skills install --project /absolute/checkout`. Preserve custom
instructions; edited managed blocks are deliberately refused. Check
`devkit doctor --agents` and report the checkout, changes, verified runtime/URL and
any missing prerequisites. Host setup, public exposure and unrelated VM changes
are separate work.
