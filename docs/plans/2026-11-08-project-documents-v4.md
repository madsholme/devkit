# Project documents — v4: media viewer

Supersedes archived v3, retaining all media/document access and rendering boundaries.
Click or keyboard-activate any rendered image or flowchart to open a near-fullscreen
SwiftDesign dialog. Reuse the selected picture source and sanitized flowchart SVG;
never render raw document HTML or bypass explicit external-image consent.

Provide zoom in/out, Fit to window, Actual size (100%), percentage, Escape/close,
keyboard controls (+/-/0/F), focus restoration and a scrollable/pannable canvas.
Mouse drag and native touch scrolling move enlarged media. Ctrl-wheel zooms within
the canvas. Initial fit respects viewport size; resize refits only while in fit mode.
Close/reset on document/checkout changes and release temporary SVG blob URLs.

Implementation: reuse existing dialog/button components; add one media viewer;
make rendered media accessible triggers; verify image/diagram opening, picture source,
zoom/reset, focus/Escape, scrolling and mobile layout; update dashboard docs; integrate,
push to the user fork and deploy the verified update following the ongoing workflow.
No new dependency, backend endpoint or external requests beyond already-loaded images.

## Verification checkpoint

Viewer implemented with existing SwiftDesign components; no dependency or backend change.
Passed 20 frontend unit tests, typecheck/build and all 8 document browser scenarios.
New coverage verifies image and diagram opening, zoom controls, Fit/100%, mouse drag,
active picture source, Escape/focus return, global shortcut isolation and mobile layout.
Desktop/mobile screenshots inspected. Diagram blobs are revoked on close and document
change/unmount. The existing lazy Mermaid chunk-size build warning is unchanged.
