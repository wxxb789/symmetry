# Elixir / Phoenix

Follow the root contract and `docs/design/{core,data,protocol}.md`.

- Keep public Phoenix contexts thin and stable. Internal modules own concrete
  transition/read responsibilities, not generic service/repository wrappers.
- PostgreSQL rows and constraints own invariants. GenServers coordinate wakeups
  and liveness only. Never make process memory the sole goal/run truth.
- Lock in the order specified in data.md. No provider, harness, or model network
  calls while a database transaction is open. Commit state and durable wakeup
  together; publish notifications after commit.
- Use Ecto changesets and named database constraints. Validate external enums
  explicitly; do not create atoms from untrusted strings. Use tagged results for
  expected failures; never rescue arbitrary exceptions into success.
- Preserve provider-owned WorkItem fields and existing task-generation fences.
  Extract orchestration internals with characterization parity, not a rewrite.
- Use Req for new outbound HTTP integration, with redirects disabled for
  credential-bearing requests and explicit bounded retry ownership. Migrate
  old HTTP code only with behavior parity. Oban is for short durable jobs, not
  holding an agent process for hours.
- Typespecs, compiler diagnostics, and Dialyzer help; none replaces transaction
  or concurrency tests. Add StreamData properties for state-machine invariants
  when the changed behavior needs them, not for ordinary CRUD.

Existing checks, from control/: `mix format --check-formatted`,
`mix compile --warnings-as-errors`, `mix test`. Required integration environment
and additional checks are in `docs/design/quality.md` and existing runbooks.
