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
