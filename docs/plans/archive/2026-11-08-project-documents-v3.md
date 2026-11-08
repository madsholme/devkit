# Project documents — v3: pictures and flowcharts

Supersedes v2 (archived). Preserve v2 document discovery, filtering, links, edit guards,
and exact-checkout access. Extend the reader and editor preview with media.

- Render Markdown images and only picture/source/img HTML. All other raw HTML stays inert.
- Local image URLs resolve relative to the document within the exact selected checkout.
  Read PNG/JPEG/GIF/WebP/SVG only, at most 8 MiB, through pinned no-follow directory
  handles. Reject hidden/traversing paths, unregistered roots, links and special files.
  Sandbox SVG responses, disallow external resources, and set nosniff/no-store.
- Remote HTTP(S) images require explicit per-image loading, never server proxying.
  Preserve alt text/fallbacks, responsive sizing and picture media/density selection.
- Mermaid flowchart/graph fences render lazily with strict security, bounded source and
  edges, no author configuration or image/icon shapes. Sanitize SVG before insertion.
  Invalid/unsupported diagrams show an explanation and retain readable source.
- No arbitrary HTML, active SVG content, source-file serving, or new editing authority.

Implementation: (1) bounded image endpoint and access tests; (2) media token rendering,
checkout propagation and lazy flowcharts; (3) security/regression tests, desktop/mobile
browser checks, docs and ADR; (4) integrate and deploy following the user's established
request to update their fork and devkit server.

Acceptance: repository README pictures and its flowchart visibly render; relative
images honor checkout selection and preview; unsafe paths/HTML/diagram config fail
closed; external images make no request before a click; bad diagrams do not break reading.

## Verification checkpoint

Implemented media-only HTML reconstruction, relative checkout images, explicit remote
image loading, and lazy strict Mermaid flowcharts with sanitized SVG and source fallback.
Passed: full Go suite/vet; focused image race tests; 20 frontend unit tests; typecheck/build;
7 document Playwright scenarios, including real README pictures/flowchart and desktop/mobile
rendering. Screenshot inspection confirmed readable images/diagrams and source fallback;
wide mobile diagrams scroll within the reader. Build notes a large lazy Mermaid chunk;
the main dashboard bundle remains unchanged in size. No CDN or diagram rendering service.
