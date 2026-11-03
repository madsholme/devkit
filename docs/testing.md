# Acceptance check

1. Create a Git repository from the sample and commit it.
2. Create worktrees for branches `try/a` and `try-a` (e.g. `git worktree add -b try/a ../try-a`);
   verify distinct names/URLs.
3. Start both live previews. Change each `message.txt`; verify different responses.
4. Increment one preview's counter; the other must stay unchanged.
5. Commit one task; run `devkit review HEAD`. Change its live message again; the
   review must keep the committed message.
6. Increment the review counter, stop/start it, and verify the count persists.
7. Open the same URLs from another computer on the tailnet. Disconnect the first client; agents and
   previews should continue on the Linux host.
8. Configure the publishing adapter and complete one team review on your destination.

Local validation commands: `go test ./...` and `bin/devkit --help`. Full runtime acceptance requires a Docker daemon,
the Linux host, real domain/DNS settings and a configured publishing destination.

The Docker-only check can run with `DEVKIT_RUNTIME_TEST=1 python3 tests/runtime_acceptance.py`.
It uses temporary repositories and containers with loopback-only test HTTPS,
does not issue certificates or alter DNS, and removes its test containers/volumes.
The media check is `DEVKIT_RUNTIME_TEST=1 python3 tests/media_acceptance.py`; it
requires local Tailscale, creates temporary UDP/TCP echo previews, checks distinct
ports and restart persistence through the tailnet IP, and cleans them up.
