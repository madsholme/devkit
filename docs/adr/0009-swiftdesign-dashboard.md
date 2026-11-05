# ADR 0009: Compile SwiftDesign into the embedded dashboard

Status: accepted for the redesign branch, 2026-11-05.

The requested UI replacement uses SwiftDesign Vue components and SwiftConfig as the
visual template. The previous no-build vanilla page cannot consume those components.
Use Vue, Vite and Tailwind for build-time authoring, retaining the standard-library Go
server and single distributable binary. Authentication and actions remain Go APIs.

Use pinned package snapshots with provenance until a portable package distribution is
established. Commit production output under web/dist so Go-only builds still work;
frontend edits regenerate that output. The asset handler serves only known embedded
production files, with immutable caching for fingerprinted assets and no-store HTML.

A cosmetic CSS port would not use the requested components. A separately deployed
frontend would complicate host setup and the current same-origin authentication contract.
Neither is used. Shared presentation packages do not grant permissions or own host data.
