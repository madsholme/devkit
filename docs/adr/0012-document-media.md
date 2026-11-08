# 12. Checkout images and Mermaid flowcharts

Accepted, 2026-11-08. Extends ADR 11's document renderer following the user's request
for README pictures and flowcharts. Root README remains read-only.

Replace image placeholders with checkout-scoped images. A read-only image endpoint
uses the document root registration and pinned directory helpers, validates image
extension/content, bounds reads to 8 MiB, and rejects hidden paths, traversal, symlinks,
hardlinks and special files. PNG/JPEG/GIF dimensions are additionally bounded to 32
million pixels. WebP is signature-checked; SVG is XML-checked and excludes DTDs and
stylesheet processing instructions. Serve SVG as an image with a restrictive sandbox
CSP, nosniff, same-origin resource policy and no-store, including direct navigation.
SVGs are never inserted into the dashboard DOM.

Parse HTML tokens but reconstruct only picture/source/img with allowlisted attributes;
escape all other HTML. Resolve local paths relative to the selected document. External
HTTP(S) images are browser-only, explicit-click requests with no referrer. No server
proxy, cookies/credentials in URLs, or automatic remote picture-source loads.

Lazy-load Mermaid for graph/flowchart fences with strict security, disabled HTML labels,
source/edge/count limits and no author directives or extended image/icon shapes. Serialize
rendering, ignore stale results after navigation, and sanitize generated SVG with DOMPurify
before insertion; remove foreignObject, image and link elements. Preserve source on errors.
Use pinned frontend dependencies; the Go backend stays standard-library-only. The existing
image-placeholder/no-diagram restrictions in ADR 11 are superseded only by this scope.

Verification covers renderer/path/HTML guards, HTTP headers, real browser rendering,
external-image opt-in, invalid diagrams and responsive layouts. Mermaid security API:
https://mermaid.js.org/config/usage and https://mermaid.js.org/config/schema-docs/config.
