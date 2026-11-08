# Project documents — v5: wheel and touch gestures

Extends archived v4; preserve viewer controls, keyboard access, media boundaries,
focus restoration, and cleanup. The user requests unmodified mouse-wheel zoom and
mobile pinch zoom. Replace native touch scrolling inside the media canvas with
pointer-based one-finger panning and two-finger pinch zoom. Outside the canvas,
normal browser gestures remain available.

Zoom around the mouse position or pinch midpoint. Track pointer capture/cancellation,
rebase when switching between one and two fingers, retain scale bounds, and reset on
close/media changes. Do not let pinch zoom the whole page. Existing buttons, Fit and
keyboard shortcuts continue working. Update instructions to describe the new gestures.

Verify actual wheel events without modifiers plus browser-emulated multi-touch pinch
out/in, transition to single-finger pan, cancellation and browser viewport stability.
Run existing viewer/document checks, rebuild embedded assets, then push/deploy through
the established fork/server workflow. No new dependency or backend change.

## Verification

Passed typecheck/build, 20 frontend unit tests, all 9 document browser tests and the
Go suite. Real browser wheel and emulated multi-touch events verify zoom without Ctrl,
pinch out/in, transition to one-finger pan, cancellation and unchanged page scale.
Existing dialog, keyboard, source-selection and document flows remain covered.
