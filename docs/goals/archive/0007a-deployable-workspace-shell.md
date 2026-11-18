# 0007a - Deployable authenticated workspace shell

Provide a production-built React and TypeScript workspace served by the Phoenix
release, with authenticated entry, stable navigation, project selection, deep
links, refresh recovery, and typed access to existing read APIs. The first
screen is the usable workspace shell rather than a marketing page.

Completion evidence includes deterministic install, typecheck, lint, test and
build commands; Phoenix release asset delivery; direct deep-link and expired-auth
tests; and rendered desktop/mobile verification of navigation, focus, loading,
empty, error, and signed-out states.

This goal includes no work-item mutation, provider management, execution
control, Chat, or Goal-specific UI. It creates no production Node server, mock
backend, second server-state cache, or frontend policy engine. It is one
independently mergeable PR and must fit one agent run of at most 12 hours.

Operator note 2026-11-14 (this goal has no owner-decision block; the dispatch
brief carries the resolved host prerequisites):

- **Frontend toolchain.** The repository pins `pnpm@12.17.0` in the root
  `package.json` (`packageManager`). The ambient `pnpm` on this host is a newer
  install and refuses this repository with `ERR_PNPM_BAD_PM_VERSION`, so every
  install/typecheck/lint/test/build command must run through the pinned version,
  e.g. `mise exec pnpm@12.17.0 -- pnpm …` (verified on 2026-11-14: that reports
  `12.17.0`). Do not change the repository's pin to match an ambient tool; make
  the toolchain match the repository. Node `v26.13.1` is on `PATH`.
- **`frontend/` currently contains only `AGENTS.md`.** Read it; this goal creates
  the workspace. `control/` (Phoenix) and `contracts/` (typed read APIs) are the
  existing surfaces the workspace consumes; no second server-state cache or mock
  backend may be introduced.
- **Local Linux surface**, if any Linux-side observation is needed, is
  `wsl.exe -d Ubuntu-24.04` (it has no Node/pnpm installed; the host's default
  WSL distribution is an Alpine image and is not a surface). This contract does
  not require Linux evidence.
- **Rendered verification.** Rendered desktop/mobile verification means an actual
  render in a browser with recorded, reviewable evidence; a headless smoke that
  only echoes markup does not satisfy it. Record the exact command, the artifact
  path, and what was observed.
