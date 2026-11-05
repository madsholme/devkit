# SwiftDesign package snapshots

These are unmodified `pnpm pack` snapshots of `@madsholme/ui` and
`@madsholme/themes` 0.1.0 from the clean SwiftDesign checkout at commit
`df82eb31847bad4f65202302e3882edab368b54c`, captured 2026-11-05.
The UI archive includes its upstream shadcn-vue license. SwiftDesign package
manifests retain their own license declarations. Do not republish these packages.

They make this branch reproducible without an absolute developer path or registry
credentials. `pnpm.overrides` resolves the UI package's theme dependency to the same
snapshot. Update both archives and the lockfile together when adopting a new release:

```sh
pnpm --dir "$SWIFT_DESIGN_DIR/packages/themes" pack --pack-destination "$PWD/frontend/vendor"
pnpm --dir "$SWIFT_DESIGN_DIR/packages/ui" pack --pack-destination "$PWD/frontend/vendor"
pnpm --dir frontend install
pnpm --dir frontend build
```

Keep this provenance current and review the shared component changes before upgrading.
