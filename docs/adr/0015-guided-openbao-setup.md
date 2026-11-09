# ADR 0015: Continuous guided OpenBao setup

Accepted by user request, 2026-11-09. [Plan v9](../plans/2026-11-09-openbao-v9.md).

First-run `devkit setup` now owns the interactive init/unseal/provision sequence before DNS credentials are collected. Reuse the existing terminal-only vault commands directly, preserving their recovery-material isolation and hidden input. The user selects share settings, saves recovery material off-host and supplies the unseal shares and bootstrap token in the guided session. No recovery secret is retained automatically.

Live state determines resume steps. Existing initialized storage is never reinitialized; working backend authentication skips provisioning. Noninteractive setup refuses required human actions. Standalone recovery commands remain supported. This replaces the previous instruction to exit setup, run commands manually and restart setup; no storage/security boundary changes or live-host deployment are authorized by this change.
