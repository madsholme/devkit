# Previews

## Three modes

- Live: source bind mount, independent task data. The sample rereads `message.txt`
  on every request. Python code edits need a restart; adapt the recipe to your
  real application's hot-reload server. Creating a live preview builds its Compose images from
  the current files; resuming one does not, so after changing a Dockerfile or dependencies,
  destroy and recreate it.
- Local review: a detached checkout of an exact commit supplies build inputs.
  The web image is resolved to a Docker image ID; no review service may bind-mount
  host files. The temporary checkout is removed after the build. Test data remains
  mutable. Repeating review for the same commit resumes the existing artifact.
- Team review: explicit deployment through the project's `publish` hook, e.g. to
  Hetzner/Dokploy. The adapter needs destination configuration; see [publishing](publishing.md).

Live and review services get `restart: unless-stopped`, so running previews come back after a
host reboot (with Traefik, which uses the same policy); stopped ones stay stopped. Previews saved
without this policy gain it the next time `devkit live`, `devkit review` or `devkit start` brings
them up.

Commit the project recipe before using `review`; it reads configuration from the
selected commit. Generated inputs should be deterministic and dependencies locked.
This v1 sample does not initialize submodules or Git LFS automatically.

## Lifecycle and clean-up

Run lifecycle commands inside the owning Git repository. `ls` works anywhere.
Without a preview ID, `stop`, `start` and `destroy` act on the preview that belongs
to the current worktree: its live preview, or a review started from it. If several
match (e.g. a live and a review preview), devkit lists them and asks for an ID.
Each preview records the worktree it was started from in its `devkit.worktree` label.
To remove a finished worktree, run `devkit destroy` in it and then remove it with
your tool or `git worktree remove <path>`; Git refuses removal of uncommitted work.
If a tool already deleted the worktree, `devkit gc` lists the orphaned previews and
`devkit gc --yes` destroys them (containers, networks, volumes and run dirs). It
only considers previews of the current repository and never touches a preview
whose worktree still exists. Review previews created before devkit recorded
`devkit.worktree` are never collected; destroy them by ID.
Branches are retained until you explicitly delete them.

Every review is a fresh build, so build cache and images pile up as previews come
and go. `devkit prune --yes` trims the Docker build cache to 30 GB and removes images
older than a week that no container, running or stopped, uses. It works host-wide,
not only on devkit images, and never touches volumes. The web service runs it once a
day; without it, run it yourself. `devkit prune` alone shows `docker system df`.
The limits and the daily run are set in the host config's `prune` section.

## References

- https://git-scm.com/docs/git-worktree
- https://docs.docker.com/compose/how-tos/project-name/
