# OpenCode 1.18.30 private protocol fixtures

These synthetic fixtures model the locally inspected OpenCode `serve` API at
version `1.18.30` (upstream commit `3104c1428ec91f809e5ab86631300de41eb6952e`).
They exercise HTTP identity and SSE framing/cursor validation only.

No provider credential, model prompt, native lifecycle, terminal result, usage,
resume, permission, artifact-recovery, or shutdown behavior was captured. The
fixtures do not establish a supported Symmetry OpenCode adapter or capability.
