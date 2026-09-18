# Team publishing: adapter contract

The intended destination is Dokploy on Hetzner. Destination details are not yet
provided, so the sample `publish` recipe deliberately fails instead of pretending
to deploy. Local live/review previews do not require Git pushes.

`devbox publish <commit>` requires an existing local review container (running or
stopped). It obtains the reviewed web image ID from its Docker labels and calls
the current project's `just publish` with:

- `DEVBOX_COMMIT`: full source commit
- `DEVBOX_IMAGE_ID`: exact local image ID, not a mutable tag
- `DEVBOX_PREVIEW`: local review ID

Implement the project adapter to:

1. Check the reviewed image architecture against the Hetzner runtime.
2. Tag that image in the selected registry and push it. Do not rebuild implicitly.
3. Obtain its registry digest and deploy that digest through the configured
   Dokploy application/API.
4. Supply team-specific environment variables and isolated test data.
5. Wait for deployment health, then return its URL, commit and digest.

Registry upload is still needed even though a Git push is optional. Pinning an
image avoids a different rebuild being mistaken for the version reviewed locally.
If the architectures differ, build a target-compatible image and review that
artifact on a compatible runtime before calling it the same reviewed image.

The sample is a single-image application. A multi-service project needs an adapter
that records/publishes all application image digests, migrations and configuration
required by that version. Hostname-specific frontend build settings may require a
different artifact; prefer runtime configuration where the application supports it.

Do not automatically post PR comments unless that behavior has been enabled by
the user. Dokploy's built-in PR preview flow is an alternative trigger, not a
requirement of this kit.

References:
- https://docs.dokploy.com/docs/core/providers
- https://docs.dokploy.com/docs/core/auto-deploy
- https://docs.dokploy.com/docs/core/applications/preview-deployments
