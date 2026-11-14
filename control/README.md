# Symmetry Control

`control/` is the Phoenix control plane for Symmetry. It owns durable
orchestration state and exposes the versioned API consumed by operators and
execution daemons.

Build the authenticated React workspace from the repository root before starting
Phoenix or assembling a release. The pinned package manager, not an ambient
pnpm installation, owns dependency resolution:

```sh
mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend typecheck
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend lint
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend test
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build
```

The build stages hashed assets and `index.html` in
`control/priv/static/portal/`. `mix release` includes those files; the Docker
build performs the same frontend build in a disposable Node stage. Production
needs no Node server or package manager. Missing build output returns 503 rather
than an alternate workspace. HTML/session bootstrap are no-store; hashed assets
are immutable. Unknown API and asset paths remain 404.

Typecheck uses TypeScript 7's native `tsc`. Type-aware ESLint uses the supported
TypeScript 6 compiler API alias alongside it; neither strict compiler options
nor lint rules are disabled to accommodate the API transition.

For local development, install Elixir dependencies and start the endpoint from
`control/`:

```sh
mix setup
mix phx.server
```

The local endpoint is available at `http://127.0.0.1:4000`. Open
`http://127.0.0.1:4000/portal` for the engineering workspace and sign in with
the configured operator token. `/portal/attention`, `/portal/projects` and
`/portal/projects/:projectId` are read-only workspace routes. The URL owns
project selection, including refresh/back/forward recovery. Only the server's
selected project contains hydrated work items; unloaded project work is not
reported as zero. Attention selection is carried by `?projectId=...`, including
navigation from project details and authentication recovery. Clearing it restores
the server-selected project rather than implying a global blocker summary.
On scoped pages, the header reuses that read's project directory; a failed scoped
read loads the default directory so project navigation remains usable.

Existing writable project, board, connection and Chat flows remain at
`/portal/classic` until their React replacements reach parity. Switching uses a
full document navigation, never two simultaneous workspace runtimes or shared
client stores. Existing hash links retain their project query and route to the
classic document. Both entrypoints use the same session and CSRF protection.
Unauthenticated legacy links retain only validated classic query parameters.
The login form submits to its current document URL so the browser retains the
legacy fragment across the login redirect. API expiry
cancels reads and clears private Query state before redirecting to sign in.
Local HTTP is only for an explicitly trusted development or container network.

For Vite development, start Phoenix first, then run
`mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend dev` from the root.
Open `http://127.0.0.1:5173/portal/`; session/login/logout and socket requests
proxy to Phoenix, as do `/portal/classic` and its `/assets/portal.js` and
`/assets/portal.css` assets. `SYMMETRY_PHOENIX_ORIGIN` overrides the upstream origin. CSRF
remains enabled. Use the same hostname for both origins.

In production, do not publish Phoenix directly. It must receive private HTTP
only from the trusted TLS edge; the public boundary terminates HTTPS before
requests reach this service. See the repository [README](../README.md) for the
topology and [`docs/goal-01-runbook.md`](../docs/goal-01-runbook.md) for
deployment and operational steps. The API contract is
[`docs/protocol-v1.md`](../docs/protocol-v1.md).

Portal project, Kanban, health, and execution workflows are documented in
[`docs/goal-02-runbook.md`](../docs/goal-02-runbook.md). Assigning a work item
to an agent records its execution settings; `Start run` explicitly creates the
durable task, after which the portal supports input, cancellation, and legal
failed/cancelled retries.

GitHub and Azure DevOps connection setup, credential handling, resource binding,
external ownership rules, and synchronization are documented in
[`docs/goal-03-runbook.md`](../docs/goal-03-runbook.md).

## Subject-bound GitHub Goal effects

New Goal-managed GitHub changes freeze the complete Subject, action/run/task
identity, repository ID/node ID, approved selector and explicit title/body before
dispatch. Synchronize the repository first to obtain its stable provider IDs.
Supply a body string, including an explicit empty string when intended; an
omitted body is refused rather than clearing provider-owned content. The PR body
includes a hidden marker binding the complete frozen action receipt.

An initial upsert selects open PRs only; a historical closed PR does not replace
creating an open PR. Unknown readback searches all states and refuses ambiguous
selectors. Concurrent import and delivery sync lock their existing WorkItems in
ascending UUID order before either projection writes.

Unknown outcomes use authenticated, read-only exact-object reconciliation.
Repository, head commit, selector, title, body and marker must match before an
action can settle successfully. Projection still requires current authority and
execution fences; confirmation alone does not grant projection. A completed
action replays its stored result without a provider request, including when it
completes while another caller waits for the dispatch advisory lock. Historical intents
without a binding are not reconstructed from current task state; old accepted
GitHub Goal changes fail closed. Azure and non-Goal operation semantics remain
unchanged. The binding migration refuses rollback once bindings exist.

The opt-in credentialed witness is restricted to the operator-approved private
`wxxb789/symmetry-0006h-provider-fixture` repository and isolated PostgreSQL port
`55436`, database `symmetry_0006h_test`. It requires production `gh` keyring Auth,
prepared real Subjects/branches and a manifest of the exact dirty source bytes.
Run from the repository root to inspect its complete prerequisites and flags:

```sh
python3 scripts/ci/credentialed-provider-witness.py --help
```

The launcher force-compiles the validated checkout before running a phase and
rechecks the source manifest before execution. Before broker recovery starts,
the witness refuses unrelated accepted intents without modifying them. Outbound
writes must match this invocation's exact action, resource and Subject; fixture
repository membership alone does not authorize a write.

The witness exercises the Control broker in separate Linux BEAM processes, not a
daemon/native harness. Its timeout fault withholds an actual provider response
after acknowledgement; it does not claim a naturally occurring GitHub timeout.
Monetary values remain `null`/`unknown` without provider-owned billing evidence;
rate-limit counters do not establish money or zero billing. See the
[0006h completion contract](../docs/goals/0006h-credentialed-provider-accounting.md).

Chat, durable instructions, decision packets, and cooperative worker controls
are documented in [`docs/goal-04-runbook.md`](../docs/goal-04-runbook.md).
