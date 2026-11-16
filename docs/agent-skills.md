# Bundled agent skills

Devkit installs versioned guidance for Codex and Claude Code. Skills teach agents
how to use the existing commands; they do not grant new permissions or automatically
change previews, VMs, credentials, routing or host services.

| Skill | Use it for |
|---|---|
| `devkit-projects` | New-project creation, existing-app onboarding, live/review recipes and existing Compose registration without recreation. |
| `devkit-previews` | Checkout/preview ownership, development and tests, live versus review, rebuild decisions, data retention, sharing and publishing. |
| `devkit-troubleshooting` | Bounded diagnostics and distinguishing application/container failures from DNS, TLS, proxy and client LAN/VPN failures. |
| `devkit-vms` | QEMU/libvirt connection and lifecycle work, SPICE defaults, direct LAN/Tailscale access and optional tunnels. New SPICE launchers default to no display password unless requested; existing passwords are preserved. |
| `devkit-secrets` | Context-bound credentials, login entries, safe generation/injection, explicit cleanup, external API tokens and app-owned native OpenBao storage. |

`devkit setup` installs all five, including on hosts without a configured vault.
The dashboard's agent install/repair actions use the same catalog. Install them on
each machine where the agents run; installing on a remote devkit host does not
update the agent client's local skill directory.

```sh
devkit agents skills install
devkit agents skills install --project /absolute/path/to/checkout
devkit agents skills status --json
devkit agents skills repair
devkit doctor --agents
```

Codex assets go under `~/.agents/skills/`; Claude Code assets go under
`~/.claude/skills/`. Use `--agent codex` or `--agent claude` to select one. The default
is both. The existing JSON status map remains keyed by agent; `current` means every
bundled skill is current. `doctor --agents` reports each asset separately and checks
project guidance/context. Missing credential registration is distinct from skill
installation; installing instructions does not enroll a project in the vault.

`--project` installs a separately managed block for each skill in the verified
checkout's AGENTS.md and CLAUDE.md. New project templates include identical blocks.
For existing projects, run installation explicitly for each intended checkout;
ordinary discovery does not edit repositories. Repeat installation after an upgrade
to refresh unedited blocks. Unrelated project instructions and permissions remain
intact, and the exact `CLAUDE.md -> AGENTS.md` sibling alias is preserved.

Edited/unmanaged skill files, edited managed blocks and unsafe aliases are not
overwritten. Inspect and reconcile custom guidance deliberately; repair is not a
force-overwrite operation. Keep project-specific preview commands and data-retention
requirements rather than replacing them with generic defaults.

Start new agent sessions after installation to refresh discovery. Installed-file
checks prove distribution, not that an already-running session has loaded new skills.
The managed project blocks direct relevant tasks to the corresponding skill.

Project onboarding covers both isolated previews and registering an existing stack;
these have different lifecycle/data permissions. External-secret guidance covers
Devkit tokens and native OpenBao without giving applications host administration.
See [practical scenario checks](agent-scenarios.md) for repeatable requests,
acceptance criteria and the distinction between distribution and live agent checks.

See [Previews](previews.md), [Project recipes](projects.md), [Host setup](setup.md),
and [Development secrets](secrets.md) for the command and configuration contracts.
