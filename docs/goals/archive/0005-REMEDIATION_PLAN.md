# Symmetry Remediation Plan

Execution specification for a coding agent. Planning-only artifact: no application
code, tests, dependencies, or configuration were modified while producing it.

All paths are relative to the repository root unless prefixed with `control/`,
`daemon/`, `browser/`, or `docs/`. Line numbers refer to the archived tree
described in section A.2 and are guidance, not anchors — re-locate every symbol
before editing.

---

## A. Scope and baseline

### A.1 Goals, non-goals, intended behavior

**Goals**

1. Remove the confirmed correctness and robustness defects found in the code
   review of the control plane (`control/`) and execution daemon (`daemon/`):
   non-canonical idempotency hashing, client-controlled liveness interval,
   crash-prone scheduler wake, unvalidated runtime config, constant health
   endpoint, unbounded pending-output growth that fails runs, a stdin/stdout
   lock-ordering deadlock, a reconcile path that never retries, silent
   unsupported-platform degradation, an ineffective config normalization, and
   missing race detection in CI.
2. Reconcile `docs/protocol-v1.md` and `README.md` with actual behavior where
   the documents promise something the code does not do.
3. Add regression tests that fail on the current tree for every confirmed
   defect and pass after the fix, without weakening existing checks.

**Non-goals (explicitly out of scope for the worker)**

- Product, packaging, licensing, and positioning work (LICENSE, README
  marketing rewrite, releases, GitHub topics, demo media, agent-profile
  examples, governance files).
- Refactoring the large modules (`control/lib/symmetry_control/orchestration.ex`,
  `daemon/internal/app/app.go`), removing dead code, deduplicating helpers, or
  renaming modules (`Reconciler` → reaper).
- New dependencies (rate limiting, metrics reporters, linters) — each is a
  decision item in section E, not a task.
- Storage-format redesign of the daemon journal (segmented event log).
- Behavioral changes to the protocol lifecycle (e.g. reporting `failed` on
  daemon shutdown; see DEC-4 for why this would regress automatic requeue).

**Intended behavior after remediation** — the durable-execution invariants
already documented in `docs/protocol-v1.md` §"Recovery Invariants" hold, and:

- Idempotent replays are recognized independent of Erlang term encoding.
- Only the control plane decides how stale a runtime heartbeat may be.
- A heartbeat or transition request never returns 500 because the scheduler
  GenServer is restarting.
- A misconfigured interval fails at boot with a clear message, not at runtime.
- `/healthz` reports database and supervisor liveness.
- A run does not fail because the control plane was unreachable while the agent
  produced output; only raw output is discarded, with an explicit marker.
- A blocked stdin write cannot wedge a run indefinitely.
- A failed startup/reconnect reconcile is retried with backoff.
- A daemon on an unsupported OS refuses to start instead of failing every run.

### A.2 Revision and existing changes to preserve

- Source analyzed: `symmetry-main.zip` (GitHub "Download ZIP" of branch `main`,
  no `.git` directory). The archive contains `docs/goal-04-runbook.md` and
  migration `control/priv/repo/migrations/20260906070000_version_command_request_hashes.exs`,
  which places it at or after the Goal 4 completion commits (early September 2026).
- **Exact commit SHA: unknown from the archive.** Before starting, the worker
  must run `git rev-parse HEAD` and `git status --porcelain` in the working
  clone (`Q:\repos\symmetry`) and record both in the completion report. If the
  working tree has uncommitted changes, they must be preserved untouched.
- No pre-existing working-tree changes are known. Treat any that exist as
  user-owned.

### A.3 Constraints, assumptions, unresolved questions

Constraints

- Toolchain pinned by repo: Elixir 1.20.4 / OTP 29.0.6 / Go 1.27.0 / PostgreSQL
  18 (`.github/workflows/ci.yml`, `control/mix.exs`, `daemon/go.mod`,
  `docker/*.Dockerfile`). Do not change pins.
- Control-plane gates (from `docs/goal-02-runbook.md` and `ci.yml`):
  `mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test`,
  `MIX_ENV=prod mix release --overwrite`. Alias `mix precommit` runs
  `compile --warnings-as-errors`, `deps.unlock --unused`, `format`, `test`
  (`control/mix.exs` aliases).
- Daemon gates: `test -z "$(gofmt -l .)"`, `go vet ./...`,
  `go test -count=3 -timeout 300s ./...`, static build
  (`ci.yml` job `daemon`), plus Windows `go test ./...` (job `daemon-windows`).
- Route audit: `scripts/audit-no-legacy-routes.sh` must keep passing
  (it scans `docs/protocol-v1.md`, `README.md`, `ci.yml`, etc. for retired
  action-style routes).
- Repository conventions observed: conventional-commit style subjects with a
  space before the scope (`feat (daemon): …`, `fix (daemon): …`); hand-written
  `Repo.transaction(fn -> … Repo.rollback(reason) end)` style (no `Ecto.Multi`);
  structured `slog` JSON logging in Go with snake_case event names; migration
  `down/0` blocks fail closed when newer-version rows exist (see
  `20260906070000_version_command_request_hashes.exs`).

Assumptions

- The worker has network access to Hex and the Go module proxy (this planning
  environment did not), a PostgreSQL 18 instance reachable via `POSTGRES_*`
  variables (`control/config/test.exs` defaults: `postgres`/`postgres`,
  `localhost:5432`, db `symmetry_control_test`), Node/npm for `browser/`, and
  optionally Docker for the Compose scenarios.
- No behavior change is acceptable that requires a coordinated daemon +
  control deploy unless stated in the task (T01 is designed to be
  rolling-safe via versioned hash columns).

Unresolved questions (blockers; see section E)

- DEC-1 one-time enrollment codes, DEC-2 rate limiting dependency,
  DEC-3 metrics reporter dependency, DEC-4 shutdown termination grace,
  DEC-5 process-containment hardening, DEC-6 lint tooling, DEC-7 segmented
  event log, DEC-8 minimum machine-token length, DEC-9 product/packaging items.

### A.4 Diagnostics executed (this planning environment, read-only)

| Diagnostic | Result |
|---|---|
| Unpacked archive, enumerated tree | 224 files; ~30.2k lines Go, ~14.8k lines `.ex`, ~18.9k lines `.exs`, ~4.0k lines Playwright `.mjs`, 20 migrations, 1 CI workflow |
| `grep -rn "Logger\." control/lib` | **0** matches (no application logging in the control plane) |
| `grep -c "\.innerHTML\s*=" control/priv/portal_assets/portal.js` | 20 sites |
| `grep TODO\|FIXME` across `daemon/**/*.go`, `control/lib/**/*.ex` | 0 |
| `grep Pdeathsig\|InsecureSkipVerify daemon/` | 0 / 0 |
| `grep -n "race" .github/workflows/ci.yml` | 0 (no `-race` in any Go test step) |
| Read `control/config/runtime.exs`, `config/test.exs`, `config/config.exs` | Interval env vars parsed with `String.to_integer` without bounds (lines 57–66) |
| Read `docs/protocol-v1.md` §Authentication/§Claim/§Heartbeat | Three statements contradicted by code (see T08) |
| Traced daemon output path `runner.go` → `app.go` → `state.go` | Confirmed single-file journal with 4 MiB hard limit and per-chunk rewrite (T12) |
| Traced `handleCommand`/`supervisory.go` lock order vs sink | Confirmed `inputMu` held across blocking `WriteInput` while sink needs `inputMu` (T13) |

Not executed here (no Elixir/Go toolchains, no network, no PostgreSQL):
`mix test`, `mix compile`, `go test`, `go vet`, `gofmt`, Playwright, Compose.
The worker must establish the baseline in B.0 before any edit.

### A.5 Known baseline failures

None known. `docs/goal-04-runbook.md` §"Recorded evidence" reports the last
full runs: control suite 316 passed / 3 skipped; Linux daemon 378 top-level
tests passed / 11 skipped (with `-race`); Windows daemon 379 / 11; browser
45 passed / 17 skipped. Treat deviations found in B.0 as pre-existing and
report them separately.

---

## B. Coverage table

ID scheme: **R1-** round-1 (docs-only) recommendations; **R2-** round-2
strategic observations; **C-** control-plane review findings; **D-** daemon
review findings; **Q-** cross-cutting quality/CI. Status values: *confirmed*,
*needs investigation*, *duplicate*, *already resolved*, *not actionable*.

| Issue/finding ID | Status | Root cause or uncertainty | Task ID or disposition |
|---|---|---|---|
| C-01 Idempotency hashes use `:erlang.term_to_binary` (orchestration.ex:2675, chat.ex:72, provider_access.ex:1466) | confirmed (mechanism); cross-OTP divergence not reproduced here | Durable replay comparison depends on Erlang external-term encoding of atom-keyed maps and `DateTime` structs, which is not specified as stable across releases; repo already needed `request_hash_version` once | **T01** |
| C-02 Daemon may set its own `heartbeat_interval_ms` (daemon_controller.ex:57 `Map.put_new`) | confirmed | Client-supplied runtime field flows into offline detection (orchestration.ex:1960) and scheduler freshness (1037–1042); only `> 0` validated (schemas.ex:80) | **T02** |
| C-03 Enrollment token is static, doc says "one-time" (protocol-v1.md:98, runtime.exs:35) | confirmed doc/code mismatch; redesign is a product decision | Shared secret with no expiry/rotation | **T08** (doc) + **DEC-1** |
| C-04 No minimum machine-token entropy (orchestration.ex:64) | confirmed, low severity | Only non-empty check; shipped daemon generates 256-bit tokens (state.go `NewMachineToken`) so exposure is limited to third-party daemons | **DEC-8** |
| C-05 No rate limiting on login/enroll/provider-actions | confirmed | No plug or dependency present | **DEC-2** |
| C-06a `/healthz` returns constant `ok` (health_controller.ex:5) | confirmed | No dependency probe | **T05** |
| C-06b No telemetry reporter; zero `Logger` calls; no domain events | confirmed | `telemetry.ex:15` reporter commented out; scheduler swallows `{:error, _}` (scheduler.ex:53) | **T16** (events + logs) + **DEC-3** (reporter) |
| C-07 `Scheduler.wake/0` uses `send(@name, :wake)` (scheduler.ex:12) | confirmed | `send/2` to an unregistered atom raises `ArgumentError`; callers are request paths (`register_session`, `Reconciler.run_once`) | **T03** |
| C-08 Interval env vars unvalidated (runtime.exs:57–66) | confirmed | `String.to_integer` raises on malformed input and accepts `0`/negatives, which break `Process.send_after` / tickers | **T04** |
| C-09 Protocol drift: claim "verifies runtime capacity" (protocol-v1.md:320) but `claim/3` does not; heartbeat `active_runs` validated then discarded (orchestration.ex:223–228); claim replay only while run is `claimed`/`cancelling` (orchestration.ex:1096) vs unconditional wording (protocol-v1.md:352) | confirmed | Documentation written ahead of/apart from implementation | **T08** |
| C-10a Raw EEx templates without escaping (portal_html.ex:11, login.html.eex:18) | confirmed, latent (only literal values today) | `EEx.function_from_file` performs no HTML escaping | **T07** |
| C-10b `portal.js` interpolates `outcome.phase`, `item.ci_status`, `item.review_status`, `formatLabel(...)` unescaped (≈lines 773–781); detail `href`s lack the scheme check used in `chatDelivery` (≈1744) | confirmed, defensive (server validates enums/URLs) | Inconsistent output encoding discipline | **T07** |
| C-11 Single shared operator token / no per-user identity | not actionable (product) | Identity model decision | **DEC-9** |
| C-12 `Repo` `ssl:` commented out in prod (runtime.exs) | not actionable (ops) | Deployment topology decision (private network in `compose.production.yaml`) | **DEC-9** |
| C-13 No Content-Security-Policy header | needs investigation | Must inventory inline scripts/styles in `index.html.eex`/`portal.js` first | deferred diagnostic (record in report) |
| C-14 Module size / duplication / naming (`Reconciler`) / manual transactions | not actionable in this plan | Refactor, not defect | deferred |
| C-15 `Protocol.error_details` maps unknown reasons to 400 | needs investigation | Whether any caller returns a non-atom reason | deferred diagnostic |
| C-16 Lock hot spots (runtime row / project row per mutation) | not actionable | Throughput design | deferred |
| C-17 No scheduler concurrency stress test | not actionable | Test-suite enhancement | deferred |
| C-18 `heartbeat.active_runs` dead input | duplicate of C-09 | — | **T08** |
| C-19 `claim/3` no capacity re-check | duplicate of C-09 | — | **T08** |
| C-20 `:httpc` instead of pooled client | not actionable | Dependency decision | deferred |
| C-21 `Release.migrate/0` only, no rollback helper | not actionable | Ops convenience | deferred |
| C-22 Possible missing `runs(state, lease_expires_at)` index | needs investigation | Requires `EXPLAIN` on `expire/1` query with data | deferred diagnostic |
| C-23 Portal polls full workspace every 5 s | not actionable | Scaling design | deferred |
| C-24 Portal accessibility | not actionable | UX work | deferred |
| D-01 Outbox embedded in 4 MiB journal; overflow fails run; O(n²) rewrites (state.go:28,1199–1203; app.go:2191–2209, 2403–2437) | confirmed | Every output chunk is a full journal rewrite; `writeJSONWithLimit` returns error → sink error → `failed` terminal | **T12** (bounded output) + **DEC-7** (segmented log) |
| D-02 `inputMu` held across blocking `WriteInput`; sink also needs `inputMu` (app.go:2542–2582, 2211–2216; supervisory.go:48–81, 120) | confirmed lock-ordering hazard; deadlock requires full stdin pipe | Bidirectional pipe backpressure with a shared mutex | **T13** |
| D-03 `reconcile()` failure never retried (app.go:311, 343–346, 1072–1076) | confirmed | Only `"connected"` hint or lease expiry recovers | **T09** |
| D-04 Lease expiry vs local clock, 5 s margin (app.go:2760, 2818, 2862) | needs investigation — reviewer's "6 s skew kills runs" claim is **incorrect**: renewal begins at ¾ lease remaining (≈90 s for 120 s lease), so tolerance ≈ lease − threshold − margin | Skew only matters near the renewal window with short leases | **T14** (skew warning, no behavior change) |
| D-05 Shutdown terminates with grace 0 and reports nothing (app.go:337–342, 3656–3672, 2422) | confirmed behavior; **reporting `failed` would regress** (lease expiry → reaper requeues automatically; `failed` requires manual retry) | Zero grace is the only questionable part | **DEC-4** |
| D-06 Prior-instance orphans run until reconcile completes (app.go:886–920) | confirmed, by protocol design (`stale_stop` after reconcile) | Gap is the missing reconcile retry | mitigated by **T09**; early-kill deferred |
| D-07 Containment gaps: Linux `Setpgid` only; Windows Job attached after `Start`; `taskkill` soft kill | confirmed | Platform primitives with trade-offs (`Pdeathsig` thread semantics in Go; `CREATE_SUSPENDED` requires replacing `exec.Cmd.Start`) | **DEC-5** |
| D-08 Renewal retry every 1 s without backoff; `ListJournals` 3×/s | confirmed, performance only | Bounded by runtime capacity; renewal window only when ≤ ¾ lease remains | deferred with **DEC-7** |
| D-09 Non-Linux/Windows daemon starts then fails every run (`identity_unsupported.go`, runner.go:214–219); `terminatePersistedProcessWithRetry` loops forever on permanent error | confirmed | No startup capability check | **T10** |
| D-10a `config.go:233` assigns `EventFormat` on a range copy | confirmed, no observable effect today | Normalization not written back | **T11** |
| D-10b Test-only production functions; 8 test hooks in `options` | not actionable | Refactor | deferred |
| D-11 No `-race` in CI (ci.yml:69, 89) | confirmed | Missing flag | **T15** |
| D-12 `state` package `errors.New("read "+resource)` drops causes | not actionable | Diagnostics quality | deferred |
| D-13 Token redactor matches raw string only | needs investigation | Encoded-variant leakage design | deferred |
| D-14 `daemon.log != nil` guards | not actionable | Style | deferred |
| D-15 No lease renewal during workspace `Prepare` | not actionable | Intentional (`TestLeaseExpiryCancelsBlockedPrepareWithoutProcess`) | none |
| D-16 Windows `syncDirectory` no-op | not actionable | Platform | none |
| D-17 `syscall.NewLazyDLL` vs `x/sys/windows` | not actionable | Dependency decision | deferred |
| Q-01 No linters (Credo/Dialyxir/Sobelow/golangci-lint) | not actionable without approval | Tooling additions | **DEC-6** |
| Q-02 Duplicate helpers (`valid_uuid?`, `request_hash`, `secure_compare`) | partially addressed | T01 centralizes hashing only | **T01** (hash) / deferred (rest) |
| R1-01 LICENSE, description, topics | not actionable (legal/repo settings) | Owner decision | **DEC-9** |
| R1-02 Release with prebuilt binaries/images | not actionable | Tooling/owner | **DEC-9** |
| R1-03/04 README pitch, GIF, positioning doc | not actionable | Owner content | **DEC-9** |
| R1-05 Lower toolchain floor | needs investigation | Requires compile attempts on older versions (e.g. `for range 16` needs Go ≥1.22) | **DEC-9** |
| R1-06 `examples/agent-profiles/` | not actionable | Needs real agents to validate | **DEC-9** |
| R1-07 `.ps1`-only live test runners | not actionable | Tooling decision | **DEC-9** |
| R1-08 Docs restructure / OpenAPI / diagrams | partially | Only factual drift is in scope | **T08**; rest **DEC-9** |
| R1-09 Governance files | not actionable | Owner | **DEC-9** |
| R1-10 Per-runtime lease/heartbeat tuning | not actionable | Protocol design | **DEC-9** |
| R1-11 `waiting_for_input` holds capacity | not actionable | Documented intent (protocol-v1.md:79) | none |
| R1-12 Multi-user identity | duplicate of C-11 | — | **DEC-9** |
| R1-13 Runtime scoping per project | needs investigation | Model review | **DEC-9** |
| R1-14 Workspace isolation / threat-model doc | not actionable in plan | Doc backlog | **DEC-9** |
| R1-15 Observability | partially | Events/health in scope; reporter/dashboards not | **T05**, **T16**, **DEC-3** |
| R1-16 Cost/usage tracking | not actionable | Product | **DEC-9** |
| R1-17 Demo/blog | not actionable | Owner | **DEC-9** |
| R2-a God files / lint gates | partially | Lint = DEC-6; refactor deferred | **DEC-6** |
| R2-b README "Chat" wording vs templated replies | confirmed doc clarity | README omits that replies are evidence summaries without an LLM (stated only in goal-04 runbook) | **T08** |
| R2-c `gh`/`az` CLI credential model | not actionable | Product/architecture | **DEC-9** |
| R2-d Container workspace policy | not actionable | Product | **DEC-9** |
| R2-e Test story / badges in README | not actionable | Owner | **DEC-9** |

---

## B.0 Mandatory baseline (before any edit)

1. Record `git rev-parse HEAD`, `git status --porcelain`, `git stash list`.
2. Control plane, from `control/`: `mix deps.get`, `mix format --check-formatted`,
   `mix compile --warnings-as-errors`, `mix test`. Save the summary line
   (N tests, F failures, S skipped).
3. Daemon, from `daemon/`: `test -z "$(gofmt -l .)"`, `go vet ./...`,
   `go test -count=1 -timeout 300s ./...`, and once
   `go test -race -count=1 -timeout 600s ./...`. Save summaries.
4. From repo root: `scripts/audit-no-legacy-routes.sh`.
5. Optional but recommended: `docker compose config` (validates `compose.yaml`).
6. Any failure here is pre-existing; list it in the completion report and do not
   attempt to fix it unless a task below names it.

---

## C. Ordered implementation tasks

Phase A (control plane, sequential — several tasks touch `orchestration.ex`
and `scheduler.ex`): T01 → T02 → T03 → T04 → T05 → T16 → T07 → T08.
Phase B (daemon, sequential — all but T11 touch `app.go`): T09 → T10 → T11 →
T12 → T13 → T14. Phase C: T15. Phases A and B are independent of each other;
a single worker should finish Phase A before Phase B. Do not interleave edits
to `app.go` across tasks; complete and verify each before starting the next.

---

### Task T01: Canonical, versioned request hashing for idempotent replays

1. **Coverage and prerequisites**
   - Addresses C-01, Q-02 (hashing portion).
   - No dependencies. Requires PostgreSQL for tests. Decision made in this plan:
     add version columns (repository pattern) rather than dual-accept without
     versions.

2. **Root cause**
   - Verified: `control/lib/symmetry_control/orchestration.ex:2675`
     `defp request_hash(value), do: value |> :erlang.term_to_binary() |> digest()`
     is used for `machines.enrollment_request_hash` (lines 68/82/106/120),
     `tasks.request_hash` (263/278/307/316), `run_events.request_hash`
     (1198/1207/1218 via `event_body/1` at 2641–2648, which includes a
     `DateTime` in `occurred_at`), `run_transitions.request_hash`
     (1245 `%{state: …, payload: …}`, 1252), and `commands.request_hash`
     (2547–2562, already versioned 1/2). `chat.ex:71–72` and
     `provider_access.ex:1463–1471` do the same for `chat_actions.request_hash`
     and `provider_action_intents.request_hash`.
   - Verified: inputs contain atom keys and a `%DateTime{}` struct; the
     external term format of maps is implementation-ordered for >32 keys and
     the format itself is not guaranteed stable across OTP releases.
   - Trigger → path → effect: a daemon (or operator) retries a request after an
     OTP upgrade or against a node on a different OTP version → stored hash ≠
     recomputed hash → `Repo.rollback(:idempotency_conflict)` → `409` → the
     daemon's terminal/transition delivery stalls until the 8-minute terminal
     grace (`ensure_terminal_fence!`) expires, or the operator's task submit is
     rejected.
   - Hypothesis (not reproduced): which OTP pairs actually diverge. The fix does
     not depend on this.

3. **Change specification**
   - New module (proposed) `control/lib/symmetry_control/request_hash.ex`,
     `SymmetryControl.RequestHash`:
     - `@spec canonical(term) :: <<_::256>>` — SHA-256 of `canonical_json/1`.
     - `canonical_json/1` rules: maps → `Jason.OrderedObject` with keys
       stringified (`Atom.to_string/1` for atoms, binaries unchanged; any other
       key type → `raise ArgumentError`) and sorted by binary comparison;
       lists → arrays; `%DateTime{}` → `DateTime.to_unix(dt, :microsecond)`
       (integer); other structs → `raise ArgumentError`; `true/false/nil`
       literals; other atoms → strings; integers/floats/binaries unchanged.
       Encode with `Jason.encode!/1` (Jason 1.4.5 is already a dependency).
     - `@spec legacy(term) :: <<_::256>>` — the existing
       `:erlang.term_to_binary/1 |> :crypto.hash(:sha256, …)`; retained only
       for version-1 rows.
     - `@spec matches?(stored :: binary, version :: pos_integer, body :: term) :: boolean`
       — `1 → legacy(body) == stored`, `2 → canonical(body) == stored`
       (commands: `1`/`2` legacy shapes as today, `3` canonical).
   - Migration (proposed) `control/priv/repo/migrations/20260907000000_version_request_hashes.exs`:
     - `add :request_hash_version, :integer, null: false, default: 1` to
       `tasks`, `run_events`, `run_transitions`, `chat_actions`,
       `provider_action_intents`; `add :enrollment_request_hash_version, …` to
       `machines`; check constraints `IN (1, 2)` for each.
     - `commands`: replace `commands_request_hash_version_check` with
       `IN (1, 2, 3)`.
     - `down/0`: fail closed with `RAISE EXCEPTION` if any row has version 2
       (or 3 for commands), mirroring `20260906070000_version_command_request_hashes.exs`;
       then drop constraints/columns and restore the `IN (1, 2)` check.
   - Schemas (`orchestration/schemas.ex`, `chat/schemas.ex`,
     `integrations/schemas.ex`): add the version fields with `default:` set to
     the new current version (2; commands 3), `validate_inclusion`, and
     `check_constraint` matching the migration names.
   - Call sites: replace each `request_hash(...)`/inline hash with
     `RequestHash.canonical(body)` when **writing**, and replace each
     pattern-match comparison (`%Task{request_hash: ^request_hash}`,
     `task.request_hash == request_hash`, `%RunEvent{request_hash: ^event_hash}`,
     `%RunTransition{request_hash: ^body_hash}`, `%Machine{enrollment_request_hash: ^request_hash}`,
     `%Action{request_hash: ^request_hash}`, `intent.request_hash != request_hash`)
     with `RequestHash.matches?(row.request_hash, row.request_hash_version, body)`.
     Keep the body shapes exactly as they are today (same keys) so version-1
     rows still verify with `legacy/1`.
   - `ensure_command_replay!/4` (orchestration.ex ≈2554): add
     `3 -> RequestHash.canonical(v2_body)`; write new commands with
     `request_hash_version: 3`.
   - Remove the now-unused private `request_hash/1` in `orchestration.ex`,
     the inline hash in `chat.ex`, and `request_hash/3` in `provider_access.ex`
     (keep `digest/1` if still used elsewhere).
   - Out of scope: changing what is hashed, hashing other tables, dual-accept.

4. **Why this fixes it**
   - Replay identity becomes a function of the JSON-visible request content
     with a fully specified encoding (sorted string keys, integer timestamps),
     independent of the BEAM's term encoding. Existing rows keep verifying via
     their recorded version, so a rolling deploy never converts a legitimate
     replay into `idempotency_conflict`.
   - Unchanged contracts: `409 idempotency_conflict` for a different body under
     the same key; `(run_id, transition_id)` / `(run_id, event_id)` /
     `(task_id, idempotency_key)` uniqueness; response payloads.

5. **Verification**
   - New unit test `control/test/symmetry_control/request_hash_test.exs`:
     - same body with atom vs string keys and different insertion order →
       identical `canonical/1`;
     - `%DateTime{}` values for the same instant with `{0, 0}` and `{0, 3}`
       microsecond precision → identical hash; two different instants → differ;
     - `canonical/1 != legacy/1` for a representative body;
     - `matches?/3` uses `legacy/1` for version 1 and `canonical/1` for 2;
     - unsupported struct key/value raises `ArgumentError`.
   - Replay compatibility test (new, in `orchestration_test.exs`): insert a
     `Task` row directly with `request_hash_version: 1` and
     `request_hash: RequestHash.legacy(normalized_attrs)`, then call
     `Orchestration.submit_task/3` with the same attrs and key → `{:ok, _, :replayed}`;
     a different body → `{:error, :idempotency_conflict}`. Same shape for one
     `RunTransition` and one `RunEvent`. Before the fix these tests cannot
     compile (module absent) — the defect-detecting test is the canonical
     equality test above, which fails today because hashing is not canonical.
   - Migration test (new) `control/test/symmetry_control/migrations/version_request_hashes_test.exs`
     following `add_retry_commands_test.exs`: `down` succeeds with only
     version-1 rows; `down` raises when a version-2 task row exists.
   - Existing suites that must still pass: every test referencing
     `idempotency_conflict` (9 files, 27 sites), `protocol_controller_test.exs`,
     `chat_controller_test.exs`, `provider_action_controller_test.exs`,
     `provider_broker_e2e_test.exs` (when enabled).
   - Commands (from `control/`): `mix test test/symmetry_control/request_hash_test.exs`,
     `mix test test/symmetry_control/migrations`, then `mix test`.

6. **Implementation pitfalls**
   - `Jason.encode!/1` on a plain map does **not** sort keys; you must build
     `Jason.OrderedObject` (or an explicit keyword list) after sorting.
   - `DateTime.to_iso8601/1` is not canonical across precisions
     (`…10Z` vs `…10.000Z`); use `DateTime.to_unix(dt, :microsecond)`.
   - `event_body/1` today includes `occurred_at` as whatever `Protocol` parsed
     (a `DateTime`); do not switch it to the raw string or existing version-1
     rows will stop matching.
   - Commands already have versions 1 and 2 with *different body shapes*
     (`ensure_command_replay!`); version 3 must hash the **version-2 body
     shape** (with `context`) canonically. Do not renumber 1/2.
   - Run `mix format` — the migration `.formatter.exs` in `priv/repo/migrations`
     applies.
   - Do not "fix" unrelated `valid_uuid?`/`secure_compare` duplicates.

7. **Acceptance and stop conditions**
   - All new tests pass; full `mix test` count ≥ baseline with 0 failures;
     `mix compile --warnings-as-errors` clean; migration up/down tested.
   - Stop if any table stores hashes computed from bodies that cannot be
     canonicalized (e.g. binary keys that are not valid UTF-8) — report and
     propose base64 wrapping rather than guessing.
   - Rollback: revert the task's commits; the migration `down/0` is safe only
     before any version-2 rows exist (by design).

---

### Task T02: Control plane owns `heartbeat_interval_ms`

1. **Coverage and prerequisites**
   - Addresses C-02. No dependencies (can precede T01; both touch different
     files).

2. **Root cause**
   - Verified: `control/lib/symmetry_control_web/controllers/daemon_controller.ex:49–58`
     merges the configured interval with `Map.put_new("heartbeat_interval_ms", configured_heartbeat)`,
     so a client-supplied value wins. `Orchestration.register_runtimes`
     (orchestration.ex:182) stores it; `Runtime.changeset` only validates
     `greater_than: 0` (schemas.ex:80). `expire_offline_runtimes`
     (orchestration.ex:1960) and `next_assignable_task_and_runtime`
     (1037–1042) use `3 × heartbeat_interval_ms` as the liveness window.
   - Effect: a daemon sending `"heartbeat_interval_ms": 10_000_000_000` is
     never marked offline and always passes scheduler freshness, so work is
     assigned to a dead runtime until its assignment expires.
   - `docs/protocol-v1.md` lists the field only in the **response**
     (line 191); `daemon/internal/protocol/protocol.go:61–68`
     `RuntimeRegistration` has no such field. The field is not part of the
     request contract.

3. **Change specification**
   - File: `daemon_controller.ex`, `register_session/2`. Replace
     `Map.put_new("heartbeat_interval_ms", configured_heartbeat)` with
     `Map.put("heartbeat_interval_ms", configured_heartbeat)` so the server
     value always overrides.
   - Leave `Orchestration.register_runtimes/3` accepting the key (tests call it
     directly with `heartbeat_interval_ms: 60_000`).
   - Add one sentence to `docs/protocol-v1.md` §"Register A Daemon Session And
     Runtime" (near line 200): the heartbeat interval is server-configured and
     any client-supplied `heartbeat_interval_ms` in a runtime specification is
     ignored.
   - Out of scope: rejecting the request (would be a contract change with no
     benefit), validating other runtime fields.

4. **Why this fixes it**
   - Liveness windows are derived exclusively from
     `config :symmetry_control, :orchestration, heartbeat_interval_ms`, so a
     misbehaving or malicious daemon cannot extend its own online status.
   - Unchanged: response shape, epoch semantics, `register_runtimes/3` API.

5. **Verification**
   - New test in `control/test/symmetry_control_web/controllers/protocol_controller_test.exs`
     (model on "session response advertises the configured lease duration",
     lines 310–331): `PUT /api/v1/machines/:machine_id/sessions/:uuid` with a
     runtime spec containing `"heartbeat_interval_ms" => 999_999_999`; assert
     200, then `Repo.get!(SymmetryControl.Orchestration.Runtime, runtime_id).heartbeat_interval_ms ==
     Application.fetch_env!(:symmetry_control, :orchestration)[:heartbeat_interval_ms]`
     (5_000 in test env). Fails before the fix (stores 999_999_999).
   - Existing: `scheduler_test.exs`, `capability_scheduling_test.exs`,
     `orchestration_test.exs` (~2091) still pass because they bypass the
     controller.
   - Command: `mix test test/symmetry_control_web/controllers/protocol_controller_test.exs`.

6. **Implementation pitfalls**
   - Do not move the override into `register_runtimes/3`; direct callers in
     tests rely on passing the value.
   - `Protocol.normalize_map/1` runs before the put; keep the order (normalize,
     then put) so the string key is what `value/3` reads.

7. **Acceptance and stop conditions**
   - New test passes; protocol controller suite green; doc sentence added.
   - Rollback: single-line revert.

---

### Task T03: `Scheduler.wake/0` must not raise while the GenServer is down

1. **Coverage and prerequisites**
   - Addresses C-07. Independent.

2. **Root cause**
   - Verified: `control/lib/symmetry_control/orchestration/scheduler.ex:12–15`
     `def wake do if enabled?(), do: send(@name, :wake); :ok end`. `send/2` with
     an unregistered atom name raises `ArgumentError`. Callers on request paths:
     `DaemonController.register_session/2` (after successful registration),
     `Reconciler.run_once/1`, and (via `Workspaces`/`TaskController`) task
     submission. During a supervisor restart of `Scheduler` (crash in
     `assign_all/1`), these requests return 500.

3. **Change specification**
   - `scheduler.ex`:
     ```elixir
     def wake do
       if enabled?() do
         case Process.whereis(@name) do
           pid when is_pid(pid) -> send(pid, :wake)
           nil -> :ok
         end
       end
       :ok
     end
     ```
   - Out of scope: `drain/1` (uses `GenServer.call`, which already returns an
     `exit` the tests control), retry/backoff semantics.

4. **Why this fixes it**
   - `send/2` to a pid never raises; the restarted GenServer's `init/1` already
     self-sends `:wake` when enabled, so a wake lost during the restart window
     is re-issued by the new process.
   - Unchanged: wake coalescing (`scheduled?` flag), `drain/1`.

5. **Verification**
   - New test `control/test/symmetry_control/orchestration/scheduler_test.exs`
     (async: false): capture `pid = Process.whereis(SymmetryControl.Orchestration.Scheduler)`,
     temporarily `Application.put_env(:symmetry_control, :orchestration, Keyword.put(cfg, :scheduler_enabled, true))`,
     `Process.unregister(SymmetryControl.Orchestration.Scheduler)`, assert
     `Scheduler.wake() == :ok` (before fix: raises `ArgumentError`), then in
     `on_exit`/`after` re-register the name (`Process.register(pid, …)`) and
     restore config. Guard with `try/after` so a failing assertion cannot leave
     the name unregistered.
   - Command: `mix test test/symmetry_control/orchestration/scheduler_test.exs`.

6. **Implementation pitfalls**
   - Test env has `scheduler_enabled: false`; without the temporary config the
     old code never reaches `send/2` and the test would pass vacuously.
   - Never `GenServer.stop` the supervised scheduler in the test — the
     supervisor restart races with re-registration.

7. **Acceptance and stop conditions**
   - New test fails on old code and passes on new; suite green.

---

### Task T04: Validate orchestration interval environment variables at boot

1. **Coverage and prerequisites**
   - Addresses C-08. Independent.

2. **Root cause**
   - Verified: `control/config/runtime.exs:57–66` reads
     `SYMMETRY_HEARTBEAT_INTERVAL_MS`, `SYMMETRY_POLL_INTERVAL_MS`,
     `SYMMETRY_ASSIGNMENT_DURATION_MS`, `SYMMETRY_REAPER_INTERVAL_MS`,
     `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS` with `String.to_integer/1`
     (raises a bare `ArgumentError` on `"abc"`, accepts `0` and negatives).
     `Reconciler.handle_info(:reap)` calls `Process.send_after(self(), :reap, interval())`
     which raises on negative values; `0` produces a busy loop; the scheduler's
     freshness fragment uses `heartbeat_interval_ms` as a multiplier.
   - Contrast: `SYMMETRY_LEASE_DURATION_MS` and
     `SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS` already use `Integer.parse` with a
     lower bound and a descriptive `raise`.

3. **Change specification**
   - `runtime.exs` (inside the existing `if config_env() != :test` block):
     introduce one local helper used for all five variables, mirroring the
     existing pattern:
     ```elixir
     positive_integer = fn variable, default ->
       case Integer.parse(System.get_env(variable, default)) do
         {value, ""} when value >= 1 -> value
         _ -> raise "#{variable} must be a positive integer"
       end
     end
     ```
     and use it for `heartbeat_interval_ms`, `poll_interval_ms`,
     `assignment_duration_ms`, `reaper_interval_ms`,
     `portal_session_max_age_seconds`. Keep defaults identical
     (`"5000"`, `"5000"`, `"30000"`, `"5000"`, `"28800"`).
   - Out of scope: `PORT`, `POOL_SIZE` (standard Phoenix generator code),
     cross-variable constraints.

4. **Why this fixes it**
   - Malformed or non-positive values are rejected at configuration load with
     an actionable message instead of crashing a GenServer or spinning.
   - Unchanged: defaults, variable names, `config/test.exs` (untouched by
     `runtime.exs` for `:test`).

5. **Verification**
   - Extend `control/test/symmetry_control/config/runtime_test.exs`: add the
     five variables to `@variables`; for each, `"0"`, `"-1"`, `"abc"` →
     `assert_raise RuntimeError, ~r/must be a positive integer/`; a valid
     override (e.g. `"1500"`) is reflected in the read config. Before the fix,
     `"0"`/`"-1"` are accepted (assertion fails) and `"abc"` raises
     `ArgumentError` (wrong exception type).
   - Command: `mix test test/symmetry_control/config/runtime_test.exs`.

6. **Implementation pitfalls**
   - The runtime config test reads `runtime.exs` with `Config.Reader.read!(…, env: :dev)`;
     ensure the helper is defined before first use inside the same `if` block.
   - Do not change `String.to_integer` for `PORT` in the same commit
     (unrelated).

7. **Acceptance and stop conditions**
   - Tests pass; `mix phx.server` boots with default env; CI job
     `integration` (which sets `SYMMETRY_HEARTBEAT_INTERVAL_MS: "1000"`,
     `SYMMETRY_REAPER_INTERVAL_MS: "500"`) still starts the release.

---

### Task T05: Dependency-aware `/healthz`

1. **Coverage and prerequisites**
   - Addresses C-06a, part of R1-15. Independent of T01–T04.

2. **Root cause**
   - Verified: `control/lib/symmetry_control_web/controllers/health_controller.ex:5`
     returns `%{status: "ok"}` unconditionally. `ci.yml` and operators use it as
     readiness (`curl --fail … /healthz`). A control node with a lost database
     connection or a crashed `ProviderAccess`/`Scheduler`/`Reconciler` process
     reports healthy.

3. **Change specification**
   - New module (proposed) `control/lib/symmetry_control/health.ex`,
     `SymmetryControl.Health`:
     - `@spec check([{atom, (-> :ok | {:error, term})}]) :: {:ok, map} | {:error, map}`
       runs each check, builds `%{status: "ok" | "unavailable", checks: %{name => "ok" | "error"}}`.
     - `default_checks/0` returns:
       `database` → `Ecto.Adapters.SQL.query(SymmetryControl.Repo, "SELECT 1", [], timeout: 2_000)`
       mapped to `:ok`/`{:error, reason}` (rescue `DBConnection.ConnectionError`);
       `scheduler`, `reaper`, `provider_access` → `Process.whereis/1` non-nil for
       `SymmetryControl.Orchestration.Scheduler`, `SymmetryControl.Orchestration.Reconciler`,
       `SymmetryControl.Integrations.ProviderAccess`.
   - `HealthController.show/2`: call `Health.check(Health.default_checks())`;
     `{:ok, body}` → 200; `{:error, body}` → 503 with the same body shape.
     Never include exception text in the response (log it in T16).
   - Out of scope: separate liveness vs readiness endpoints, `compose.production.yaml`
     healthchecks (may be added later).

4. **Why this fixes it**
   - Readiness now reflects the two things the control plane cannot operate
     without (database, orchestration supervisors), so load balancers and CI
     stop routing to a node that would fail every request.
   - Unchanged: path `/healthz`, unauthenticated, JSON.

5. **Verification**
   - Convert `control/test/symmetry_control_web/controllers/health_controller_test.exs`
     to `use SymmetryControlWeb.ConnCase` (needs the sandbox for `SELECT 1`);
     assert 200 and `%{"status" => "ok", "checks" => %{"database" => "ok", …}}`.
   - New `control/test/symmetry_control/health_test.exs`: `Health.check/1` with
     an injected failing check returns `{:error, %{status: "unavailable", checks: %{db: "error"}}}`;
     all-ok returns `{:ok, …}`. This exercises the aggregation logic without
     mocking the database.
   - Before the fix the new assertion on `"checks"` fails (key absent).
   - Commands: `mix test test/symmetry_control_web/controllers/health_controller_test.exs test/symmetry_control/health_test.exs`.

6. **Implementation pitfalls**
   - The existing test uses plain `ExUnit.Case`; without `ConnCase` the DB
     probe runs outside the sandbox and fails with an ownership error.
   - Keep the query timeout short (2 s) so a hung database does not tie up
     Bandit acceptors.
   - `ProviderAccess` is always started (`application.ex`), `Syncer` is
     conditional — do not include `Syncer` in default checks.

7. **Acceptance and stop conditions**
   - Tests pass; `curl -sf http://127.0.0.1:4000/healthz` returns 200 against a
     migrated dev database; CI `integration` job readiness loop still succeeds.

---

### Task T16: Domain telemetry events and failure logging in orchestration

1. **Coverage and prerequisites**
   - Addresses C-06b (events/logging half), part of R1-15.
   - **After T01, T03, T05** (touches `orchestration.ex`, `scheduler.ex`,
     `reconciler.ex`, and the health controller).

2. **Root cause**
   - Verified: zero `Logger` calls in `control/lib`; `scheduler.ex:53`
     `{:error, _reason} -> :ok` discards assignment failures;
     `Reconciler.handle_info/2` ignores `run_once/1` results;
     `telemetry.ex` defines only Phoenix/Ecto/VM metrics. Operators cannot see
     lease expiries, offline transitions, or scheduler errors.

3. **Change specification**
   - `orchestration.ex`: emit `:telemetry.execute/3` **after** the enclosing
     `Repo.transaction` returns `{:ok, _}` (never inside the transaction fun):
     - `[:symmetry_control, :orchestration, :run, :assigned]` in `assign_one/1`
       (metadata: `task_id`, `run_id`, `runtime_id`, `generation`);
     - `[:symmetry_control, :orchestration, :run, :claimed]` in `claim/3`;
     - `[:symmetry_control, :orchestration, :run, :transition]` in
       `transition/6` (metadata includes `state`);
     - `[:symmetry_control, :orchestration, :run, :expired]` per run finalized by
       `expire/1` (metadata: `run_id`, `generation`, resulting `state`);
     - `[:symmetry_control, :orchestration, :runtime, :offline]` per runtime
       marked offline by `expire_offline_runtimes` (metadata `runtime_id`).
     Measurements: `%{count: 1}`.
   - `scheduler.ex`: `require Logger`; in `assign_all/1`
     `{:error, reason} -> Logger.warning("scheduler assignment failed", reason: inspect(reason))`.
   - `reconciler.ex`: `require Logger`; log a warning when `Orchestration.expire/1`
     returns an error tuple (inspect the actual return shape first).
   - `health_controller.ex`/`health.ex` (from T05): `Logger.error` with the
     failing check name when returning 503.
   - `telemetry.ex` `metrics/0`: add `counter("symmetry_control.orchestration.run.assigned.count")`
     etc. for the five events (inert without a reporter; see DEC-3).
   - Out of scope: request-level logging (Phoenix already logs), integrations
     module logging, metrics reporter dependency.

4. **Why this fixes it**
   - Failure paths become observable through standard OTP mechanisms without
     new dependencies; a reporter can be attached later (DEC-3) without
     touching orchestration again.
   - Unchanged: return values, transaction boundaries (events emitted after
     commit only).

5. **Verification**
   - New `control/test/symmetry_control/orchestration_telemetry_test.exs`
     (DataCase, async: false): `ref = :telemetry_test.attach_event_handlers(self(), [events…])`
     (telemetry 1.4.2 ships `:telemetry_test`), drive an
     enroll → register → submit → `assign_one` → `claim` → `transition`
     sequence using the helpers already present in `orchestration_test.exs`,
     and `assert_received {[:symmetry_control, :orchestration, :run, :assigned], ^ref, %{count: 1}, %{run_id: _}}`
     etc. For `expire/1`, reuse the existing expiry fixture pattern from
     `orchestration_test.exs` (search "expire") with a `now:` option past the
     lease. Fails before the fix (no messages).
   - Scheduler warning: first check whether `Orchestration.assign_all/1` has a
     deterministic error return reachable from a public option (read
     `assign_all/1` and `assign_one/1`, orchestration.ex:994–1068). If it
     does, add an `ExUnit.CaptureLog` test asserting the
     "scheduler assignment failed" line; if it does not, do not fabricate one —
     record the log line as manually verified in the completion report.
   - Commands: `mix test test/symmetry_control/orchestration_telemetry_test.exs`,
     then full `mix test`.

6. **Implementation pitfalls**
   - Emitting inside the transaction closure would fire events for rolled-back
     work; emit only after `{:ok, …}`.
   - `config :logger, level: :warning` in test — use `Logger.warning`, not
     `info`, for anything you assert on with `CaptureLog`.
   - Do not log task goals, inputs, payloads, or tokens; metadata is IDs and
     states only (`filter_parameters` does not apply to Logger).

7. **Acceptance and stop conditions**
   - Telemetry test passes; no new compiler warnings; full suite green.

---

### Task T07: Portal output-encoding hardening (EEx and portal.js)

1. **Coverage and prerequisites**
   - Addresses C-10a, C-10b. Independent; requires a running control plane for
     the browser suite.

2. **Root cause**
   - Verified: `control/lib/symmetry_control_web/portal_html.ex:11–12` compiles
     templates with `EEx.function_from_file/5` (no HTML escaping);
     `portal_html/login.html.eex:18` renders `<%= assigns.error %>`. Values
     today are literals from `portal_session_controller.ex:17,22`, so not
     exploitable, but any future dynamic message would be injected raw.
   - Verified: `portal.js` `escapeHtml` (98–105) is applied to most fields, but
     the detail drawer (≈773–781) interpolates `outcome.phase`, `item.ci_status`,
     `item.review_status` into `class` attributes and `formatLabel(...)` output
     as text without escaping; `href="${escapeHtml(item.external.url)}"` and
     `item.pull_request_url` (≈769, 779) rely solely on server-side
     `validate_http_url`, whereas `chatDelivery` (≈1744) also checks
     `/^https?:\/\//i` client-side.

3. **Change specification**
   - `portal_html.ex` / templates: escape every interpolated assign with
     `Plug.HTML.html_escape/1` (Plug 1.20.3 is a dependency; no new dep):
     `<%= Plug.HTML.html_escape(assigns.error) %>` and the two
     `csrf_token` sites. Alternatively wrap in the controller; template-side is
     preferred so the template is safe by construction.
   - `portal.js`: add `function safeHref(url) { return url && /^https?:\/\//i.test(String(url)) ? escapeHtml(url) : null; }`
     next to `escapeHtml`; use it for `item.external.url` and
     `item.pull_request_url` in the detail drawer (render the "Not created" /
     plain text fallback when null); wrap `outcome.phase`, `item.ci_status`,
     `item.review_status`, and every `formatLabel(...)` in the detail drawer
     with `escapeHtml(...)`. Refactor `chatDelivery` to use `safeHref` so both
     views share one rule.
   - Out of scope: CSP (C-13), framework migration, other `innerHTML` sites
     already escaped.

4. **Why this fixes it**
   - Output encoding no longer depends on server-side enum validation or on a
     future developer remembering to escape; both surfaces use the same helper.
   - Unchanged: markup, class names for valid enum values (escaping does not
     alter `[a-z_]` strings), Playwright selectors.

5. **Verification**
   - Elixir: new test in `portal_session_test.exs` or a new
     `portal_html_test.exs`: `PortalHTML.login(%{csrf_token: "t", error: "<b>x</b>"})`
     contains `&lt;b&gt;x&lt;/b&gt;` and not `<b>x</b>`. Fails before the fix.
   - JS: `node --check control/priv/portal_assets/portal.js`; then run the
     browser suite against a running control plane (`cd browser && npm ci && npx playwright install chromium && npm test`) —
     `portal.spec.mjs` and `portal-connections.spec.mjs` cover the detail drawer
     and PR/CI badges.
   - Manual: with a work item whose `pull_request_url` is set to
     `javascript:alert(1)` via direct DB update (test DB only), the drawer must
     render the fallback text instead of a link.

6. **Implementation pitfalls**
   - `Plug.HTML.html_escape/1` returns a binary; do not use
     `Phoenix.HTML.html_escape/1` — `phoenix_html` is not a direct dependency
     (only optional via `phoenix_ecto`).
   - The compiled release copies `priv/portal_assets`; the goal-04 runbook notes
     `mix compile --force` may be needed for the browser to receive updated
     `portal.js` in an existing build.
   - Escaping a class attribute value must not change legitimate values —
     verify a badge still receives class `state-badge failed` etc.

7. **Acceptance and stop conditions**
   - Elixir test passes; `npm test` results ≥ baseline (45 passed / 17 skipped
     recorded in goal-04 runbook) with no new failures; syntax check clean.

---

### Task T08: Reconcile protocol and README with implemented behavior

1. **Coverage and prerequisites**
   - Addresses C-03 (documentation part), C-09, C-18, C-19, R2-b. Coordinate
     with T02 (adds one sentence to the same document). Documentation only.

2. **Root cause**
   - Verified mismatches:
     1. `docs/protocol-v1.md:98` "one-time enrollment bearer token" — the token
        is the static `SYMMETRY_ENROLLMENT_TOKEN` (runtime.exs:35) with no
        single-use semantics.
     2. `docs/protocol-v1.md:320–321` "Claim … verifies assignment ownership,
        runtime capacity, and expiry" — `Orchestration.claim/3`
        (orchestration.ex:1072–1136) checks ownership, epoch, generation,
        assignment expiry, and claim identity; capacity is enforced at
        assignment (`next_assignable_task_and_runtime`, 1027–1050, plus the
        `runs_one_capacity_bearing_run_per_task` index), not at claim.
     3. `docs/protocol-v1.md:242–262` heartbeat `active_runs` — validated by
        `valid_active_run?/1` and then discarded (`heartbeat/4` → `heartbeat_runtime/3`, 223–235).
     4. `docs/protocol-v1.md:352` "Repeating the same `claim_id` returns the
        same lease" — only while the run is `claimed` or `cancelling`
        (orchestration.ex:1096); after `running` the same claim returns
        `409 ownership_lost`.
     5. `README.md` "Chat" paragraph implies conversational AI; the goal-04
        runbook states replies are evidence summaries without an LLM call.

3. **Change specification**
   - `docs/protocol-v1.md`: (1) replace "one-time" with "configured shared
     enrollment bearer token (`SYMMETRY_ENROLLMENT_TOKEN`); it is not single-use
     and must be protected and rotated like any deployment secret"; (2) rewrite
     the claim sentence to list what is actually verified and state that
     capacity is reserved at assignment; (3) after the `active_runs` example add
     "The control plane validates `active_runs` structurally; it does not
     currently reconcile them and they are reserved for drift diagnostics";
     (4) qualify the replay sentence with the `claimed`/`cancelling` condition
     and the `ownership_lost` outcome afterwards.
   - `README.md`: in the Chat paragraph add one sentence: "Discussion and status
     replies are deterministic summaries of recorded execution evidence; the
     control plane does not call a language model."
   - Out of scope: implementing `active_runs` reconciliation or claim capacity
     checks (decisions recorded: documentation is the correction because the
     implemented behavior is sound), restructuring docs.

4. **Why this fixes it**
   - Removes promises the code does not keep, so third-party daemon authors
     and operators build against real semantics.

5. **Verification**
   - `scripts/audit-no-legacy-routes.sh` still passes (it scans both files).
   - `grep -n "one-time" docs/protocol-v1.md` returns nothing.
   - Reviewer reads the four edited passages against the cited code.

6. **Implementation pitfalls**
   - The audit script's regex forbids action-style route literals in these
     files; do not introduce example paths like `/runs/{id}/claim`.
   - Keep the 80-column wrapping style of the existing prose.

7. **Acceptance and stop conditions**
   - Audit passes; diffs limited to the five passages (+T02 sentence).

---

### Task T09: Retry a failed reconcile with backoff

1. **Coverage and prerequisites**
   - Addresses D-03; mitigates D-06. First daemon task (touches the reactor
     loop in `app.go`).

2. **Root cause**
   - Verified: `daemon/internal/app/app.go:311` calls `daemon.reconcile(ctx)`
     once after registration; `reconcile` (1045–1097) returns silently on
     `recoverUnresolvedInputIntents` error (1046–1049), `ListJournals` error
     (1050–1054), or `control.Reconcile` error (1072–1076, `runtime_reconcile_failed`).
     The only other trigger is the notification hint `"connected"` (343–346).
     With notifications disabled or the socket already connected, a transient
     control-plane error during startup leaves recovered journals unresolved
     (no `stale_stop`, no `continue` lease advance) until lease expiry kills
     them via `terminateForLease`.

3. **Change specification**
   - Change `reconcile(ctx)` to return `bool` (true on a fully processed
     response). Keep all existing side effects.
   - In `run()` (296–356): add reactor state `reconcileRetry deadlineTimer`
     (from `daemon.timer`) and `reconcileBackoff time.Duration`. After the
     initial `reconcile` and after each hint-triggered `reconcile`, if it
     returned false arm the timer with `reconcileBackoff` (start at the
     existing `minimumInterval` = 1 s, app.go:32; double on each failure up to
     a new named constant `reconcileRetryMax = 30 * time.Second` declared next
     to the other timing constants at app.go:31–41). Add a
     `case <-reconcileRetryChan:` branch that calls `reconcile` again, resets
     backoff to `minimumInterval` on success, re-arms on failure. A successful `"connected"`-hint
     reconcile stops any pending retry timer.
   - Use `daemon.timer(...)` (injectable `newTimer`) for testability; nil-safe
     channel pattern (`var reconcileRetryChan <-chan time.Time`) so the select
     branch is inert when no retry is pending.
   - Out of scope: changing reconcile decisions, `recoverUnresolvedInputIntents`
     semantics, early orphan termination (D-06).

4. **Why this fixes it**
   - Reconcile becomes eventually consistent with the control plane regardless
     of notification availability, closing the window in which stale local
     processes and un-advanced leases persist.
   - Unchanged: at most one reconcile in flight (reactor is single-threaded),
     `terminal_pending`/`cleanup_pending` skipping, decision handling.

5. **Verification**
   - New test in `daemon/internal/app/app_test.go` (pattern: fakes such as
     `restartRecoveryControl`, manual timers via `manualDeadlineTimer` /
     `blockedTimerFactory`): a control fake whose `Reconcile` returns a
     transport error for the first two calls and then a response with one
     `stale_stop` decision for a persisted `running` journal; notifications nil.
     Assert: `Reconcile` is called three times after firing the manual timer
     twice; the journal transitions to `stale`; backoff durations passed to
     `newTimer` are 1 s then 2 s. Before the fix: `Reconcile` is called once and
     the journal stays `running` until the test times out — a failure caused by
     the missing retry, not by setup.
   - Existing: `TestReconcileRetainsTerminalPendingJournal`,
     `TestReconcileCancelPreservesActiveExecution`,
     `TestReconcileFiltersIneligibleJournalStates`, `TestDelayedReconcile*`
     must pass unchanged. E2E `TestDaemonReconnectReclaimsExpiredRunWithNewGeneration`
     (requires control plane) must pass.
   - Commands (from `daemon/`): `go test -run 'Reconcile' -count=1 ./internal/app/`,
     then `go test -race -count=1 ./internal/app/`, `go vet ./...`.

6. **Implementation pitfalls**
   - `reconcile` currently calls `daemon.beginCommandRequest()` before the RPC
     and `finishCommandRequest` on error; keep that pairing on every early
     return you add, or the command-request barrier leaks
     (see `TestRenewLeaseErrorClearsCommandRequestBarrier` for the invariant).
   - Do not retry from inside `reconcile` with a blocking loop — that would
     stall the reactor (`TestStartAssignmentDoesNotBlockReactor` guards this).
   - Do not treat a *successful* reconcile whose decisions are all `unknown_stop`
     as failure.

7. **Acceptance and stop conditions**
   - New and existing reconcile tests pass with and without `-race`.
   - Stop if the reactor restructuring requires changing `sync`/`heartbeat`
     ordering — report instead.

---

### Task T10: Fail fast on platforms without process-identity support

1. **Coverage and prerequisites**
   - Addresses D-09. After T09 (same file).

2. **Root cause**
   - Verified: `daemon/internal/platform/identity_unsupported.go` (`!windows && !linux`)
     returns an error from `ProcessIdentity`; `execution.Runner.Start`
     (runner.go:214–219) then fails every launch with "capture process
     creation identity". `config.Load` and `app.Run` succeed, so the daemon
     registers as online and every assignment fails at `start_agent`.
     `terminate_unsupported.go` always errors, and
     `terminatePersistedProcessWithRetry` (app.go:2605–2629) retries forever on
     any error, so `recoverUnresolvedInputIntents` never returns on such a
     platform when a journal has `PID > 0`.

3. **Change specification**
   - In `app.go` `initialize` (or at the top of `daemon.run`, before enrollment
     and before `recoverUnresolvedInputIntents`):
     `if _, err := platform.ProcessIdentity(os.Getpid()); err != nil { return fmt.Errorf("platform does not support restart-safe process identity: %w", err) }`.
     `Run` already propagates `initialize` errors to `main.go`.
   - Out of scope: making `terminatePersistedProcessWithRetry` distinguish
     permanent errors (unreachable once startup fails), adding macOS support.

4. **Why this fixes it**
   - The daemon refuses to register a runtime it cannot safely supervise, so
     the scheduler never assigns work to it.
   - Unchanged: Linux/Windows behavior (`ProcessIdentity(os.Getpid())` succeeds
     via `/proc/self/stat` and `OpenProcess` respectively).

5. **Verification**
   - Cross-compile check: from `daemon/`, `GOOS=darwin GOARCH=arm64 go vet ./...`
     and `GOOS=freebsd go build ./...` must compile.
   - New build-tagged test `daemon/internal/app/app_unsupported_test.go`
     (`//go:build !linux && !windows`): `Run` with a valid config and fake
     enrollment returns an error containing "process identity" and never calls
     `Enroll`. It compiles under `GOOS=darwin go vet ./...`; it cannot execute
     in CI (Linux/Windows only) — record as manually unverified unless a macOS
     machine is available.
   - Linux/Windows regression: full `go test ./...` unchanged (the new check
     passes on both).

6. **Implementation pitfalls**
   - Place the check **before** enrollment so a misconfigured host does not
     consume an enrollment and leave a dead machine record.
   - Do not put the check in `config.Load` — tests load configs on all
     platforms and would break.

7. **Acceptance and stop conditions**
   - Vet/build pass for darwin/freebsd targets; Linux/Windows suites unchanged.

---

### Task T11: Make agent-profile `event_format` normalization effective

1. **Coverage and prerequisites**
   - Addresses D-10a. Independent (config package).

2. **Root cause**
   - Verified: `daemon/internal/config/config.go:230–236` normalizes
     `profile.EventFormat = EventFormatRaw` on the `range` value copy and never
     writes it back to `profiles[key]`, so loaded profiles keep `""`. Consumers
     compare against `EventFormatJSONL` only (app.go:1851, 2035), so there is no
     behavioral defect today; a future `== EventFormatRaw` comparison would
     silently misbehave.

3. **Change specification**
   - In `validateAgentProfiles`, after the `switch profile.EventFormat` block
     (and after all validations for that profile), write back
     `profiles[key] = profile` (assignment to an existing key during `range` is
     permitted). Alternatively iterate by key and mutate through the map.
   - Out of scope: any change to `EventFormat` semantics.

4. **Why this fixes it**
   - Loaded configuration matches the documented default (`raw`), removing the
     divergence between validation and stored state.

5. **Verification**
   - Extend `daemon/internal/config/config_test.go` (`TestLoadAcceptsValidConfiguration`
     or a new `TestLoadDefaultsEventFormatToRaw`): a profile without
     `event_format` loads with `EventFormat == EventFormatRaw`. Fails before the
     fix (`""`).
   - Command: `go test -run 'TestLoad' ./internal/config/`.

6. **Implementation pitfalls**
   - The function receives `profiles map[string]AgentProfile` by value (map
     header); mutation is visible to the caller — confirm the caller uses the
     same map (`config.Load`).

7. **Acceptance and stop conditions**
   - Test passes; `go vet` clean.

---

### Task T12: Bound pending raw output in the run journal instead of failing the run

1. **Coverage and prerequisites**
   - Addresses D-01 (correctness half); DEC-7 records the storage redesign.
     After T09/T10 (touches `app.go` queue paths and `state.go`).

2. **Root cause**
   - Verified chain: `execution.Process.readOutput` (runner.go:360–378) reads
     32 KiB chunks → `agentOutput.handle` (app.go:1824–1837) → `queueOutput`
     (2034–) → `queueRawEvent` (2191–2197) base64-encodes each chunk →
     `queueEvent` → `state.Store.QueueNextEvent` (state.go:588–593) →
     `mutateJournal` (973–993) loads, appends to `PendingEvents`, and rewrites
     the **entire** journal via `writeJSONWithLimit` (1199–1208), which returns
     `errors.New("encode run journal")` when the encoded journal exceeds
     `maxJournalFileBytes = 4 << 20` (state.go:28). The sink error is recorded
     (runner.go:411–413) and `waitForRunWithContext` (2430–2437) queues a
     `failed` transition with that error as the cause.
   - Trigger: control plane unreachable or slow while the agent emits roughly
     >3 MiB of stdout/stderr (base64 inflates 4/3). Effect: the run is reported
     `failed` although the agent was healthy; all later output is discarded.
     Secondary: each chunk rewrites and fsyncs the growing file (O(n²)).

3. **Change specification**
   - `state.go`:
     - Add journal fields `DroppedOutputChunks int64 json:"dropped_output_chunks,omitempty"`
       and `DroppedOutputBytes int64 json:"dropped_output_bytes,omitempty"`.
     - Add (proposed) `func (store *Store) QueueOutputEvent(key RunKey, event protocol.RunEvent, budget int) (journal RunJournal, dropped bool, err error)`:
       under `mutateJournal`, compute `pending := Σ len(e.Payload)` over
       `PendingEvents` with `Kind == "output"`; if `pending + len(event.Payload) > budget`
       increment the dropped counters (bytes = `len(event.Payload)`) and return
       `dropped = true` without appending or consuming a sequence number;
       otherwise behave exactly like `QueueNextEvent`.
     - Add (proposed) `func (store *Store) QueueOutputTruncatedMarker(key RunKey, eventID string, at time.Time) (RunJournal, bool, error)`:
       under one `mutateJournal`, if `DroppedOutputChunks > 0` append an event
       `Kind: "output_truncated"`, `Payload: {"dropped_chunks": N, "dropped_bytes": M}`
       with the next sequence, reset both counters, return `true`; else return
       `false`. Requires `hasClaimGrant()` like `appendEvent`.
   - `app.go`:
     - New constant `maxPendingOutputBytes = 2 << 20` (2 MiB of encoded
       `output` payload pending per run; leaves headroom under the 4 MiB
       journal limit for semantic events and metadata).
     - `queueRawEvent`: call `QueueOutputEvent(..., maxPendingOutputBytes)`;
       when `dropped`, log `output_dropped_pending_budget` at Warn **once per
       run** (track on `runningRun`, e.g. `outputDropWarned bool`) and return
       `nil` (not an error).
     - `deliverEvents` (3393–3422): after a successful `MarkEventsDelivered`,
       call `QueueOutputTruncatedMarker(key, newID, now)`; if it returns
       `true`, `signalOutboxFor(key)`.
   - Semantic events (`agent_event`, `waiting_for_input`, `command_applied`,
     transitions, acks) keep using the unbudgeted paths; the 4 MiB hard limit
     remains as the final backstop.
   - Out of scope: changing the journal file layout, server-side rendering of
     `output_truncated` (server accepts any `kind`; `RunEvent.changeset`
     schemas.ex:247–262 has no kind inclusion), redactor behavior, chunk size.

4. **Why this fixes it**
   - The failure mechanism (journal encode error → sink error → `failed`) can
     no longer be reached by raw output volume; the loss is made explicit and
     bounded (an `output_truncated` event with exact counts) instead of failing
     healthy work. Bounding pending output also bounds the per-chunk rewrite
     cost.
   - Unchanged: durable-before-ack ordering, contiguous `sequence` numbering
     (dropped chunks never receive a sequence), delivery order, terminal
     handling.

5. **Verification**
   - `daemon/internal/state/state_test.go` (new tests): `QueueOutputEvent`
     appends under budget; drops and counts when over budget without changing
     `LastEventSequence`; `QueueOutputTruncatedMarker` appends one event with
     the counts and resets them; returns `false` when nothing was dropped;
     journal round-trips through restart (`ListJournals` decodes the new fields).
   - `daemon/internal/app/app_test.go` (new test, modeled on
     `TestTerminalEventTransientFailureBlocksTerminalAndSurvivesRestart`): fake
     control whose `AppendEvents` returns a transport error until released; fake
     process emitting 5 MiB of stdout in 32 KiB chunks then a JSONL
     `waiting_for_input` record then exit 0. Assert: the run does **not** end
     `failed` with "encode run journal"; the `waiting_for_input` transition is
     journaled; after releasing the control fake, delivered events include
     exactly one `output_truncated` with `dropped_chunks > 0`, and the run
     completes. Before the fix this test fails with the encode error (defect,
     not setup — the same fixture with 1 MiB passes on the old code; include
     that as a control assertion).
   - Existing: `TestTerminalFlushDeliversQueuedEventsBeforeCompletion`,
     `TestJSONLWaitingInputTransitionsAndAcknowledges`, redactor tests, e2e
     `TestCoreDaemonWorkflows`.
   - Commands: `go test -run 'Output|Journal|Terminal' -count=1 ./internal/state/ ./internal/app/`,
     `go test -race -count=1 ./...`.

6. **Implementation pitfalls**
   - `readJSONWithLimit` uses `DisallowUnknownFields`: an **older** daemon
     binary cannot read journals written with the new fields. Add a note to
     `docs/goal-01-runbook.md` (daemon section) that daemon downgrades require
     draining journals; do not remove `DisallowUnknownFields`.
   - Never drop non-`output` kinds; never fail the run on a drop.
   - Counting `len(Payload)` (encoded JSON) rather than raw bytes is
     intentional — it is what the limit measures.
   - The marker must be appended in the **same** `mutateJournal` that resets
     the counters, or a crash between them double-reports.
   - `QueueOutputEvent` must not consume a sequence number when dropping, or
     the server-side contiguous-sequence check fails.

7. **Acceptance and stop conditions**
   - New tests pass; full daemon suite passes with `-race`; e2e core workflow
     passes against a control plane.
   - Stop and report if semantic events alone can plausibly exceed the 4 MiB
     limit in a supported scenario — that is DEC-7 territory.

---

### Task T13: Bounded stdin writes to break the `inputMu` ↔ pipe deadlock

1. **Coverage and prerequisites**
   - Addresses D-02. After T12 (both edit `app.go`; T12's queue changes are
     adjacent to the sink path).

2. **Root cause**
   - Verified: `handleCommand` provide_input branch (app.go:2542–2582) takes
     `active.inputMu` and, still holding it, calls `process.WriteInput(input)`
     (2579); `handleSupervisoryCommand` does the same (supervisory.go:48–49, 78).
     `execution.Process.WriteInput` (runner.go:276–295) loops on a blocking
     `stdin.Write`. The sink path taken by the output-delivery goroutine
     (`deliverOutput` runner.go:405–415 → `agentOutput.handle` →
     `queueWaitingForInput` app.go:2211–2216 / `queueCommandApplied`
     supervisory.go:120) also locks `inputMu`. If the sink blocks on `inputMu`,
     `process.events` (capacity 64, runner.go:23) fills, `enqueue` (396–402)
     blocks, `readOutput` stops draining, the agent blocks on a full stdout
     pipe, stops reading stdin, and the pending `WriteInput` never completes →
     permanent deadlock of that run (leases keep renewing; only `cancel`
     breaks it).
   - Preconditions: the stdin pipe must already be full (OS pipe buffer,
     typically 64 KiB on Linux) — reachable when multiple large `guidance`
     records (≤32 KiB each) stack up while the agent is busy. Low frequency,
     unbounded impact.
   - Design constraint (why not release the lock before writing): `inputMu`
     preserves the ordering invariant tested by
     `TestInputLifecycleOrdersInputBeforeWaitingOrExit` — a `waiting_for_input`
     observed after the write must be attributable to the delivered input.

3. **Change specification**
   - New constant `inputWriteTimeout = 60 * time.Second` in `app.go`.
   - New sentinel `errInputWriteTimeout = errors.New("agent did not consume standard input within the write timeout")`.
   - Add to `runningRun` a field `stopExecution context.CancelCauseFunc`
     (proposed) set in `startAssigned` where the process is recorded under
     `daemon.mu` (near 1457–1470; `stopExecution` is currently a local closure).
   - New helper (proposed) `func (daemon *daemon) writeInputBounded(ctx context.Context, active *runningRun, process Process, input []byte) error`:
     run `process.WriteInput(input)` in a goroutine sending its error on a
     buffered channel; `select` on that channel, `ctx.Done()`, and
     `daemon.timer(inputWriteTimeout).Chan()`. On timeout: log
     `input_write_timeout` (run id, generation, bytes), call
     `active.stopExecution(errInputWriteTimeout)` (this cancels the execution
     context → runner's `terminateWhenContextCancels` terminates the tree
     with `defaultTerminationGrace`), and return `errInputWriteTimeout`. The
     blocked goroutine ends when the child dies (write returns `EPIPE`/closed).
   - Replace the two direct `process.WriteInput(...)` calls (app.go:2579,
     supervisory.go:78) with the helper; existing failure handling
     (`completeProvideInputWithRetry(..., "failed")`,
     `completeControlWriteFailure`) stays as-is.
   - `waitForRunWithContext` (2398–2401): treat `errInputWriteTimeout` like
     `errRequiredDecisionPacket` when reading `context.Cause(output.executionContext)`
     so the terminal `failed` payload carries the timeout message.
   - Out of scope: changing the `Process` interface, changing `inputMu`
     semantics, pipe sizes, per-profile timeouts.

4. **Why this fixes it**
   - The cycle is broken at the only edge that can be bounded without breaking
     the ordering invariant: the stdin write. A wedged agent is terminated
     deterministically with an explicit cause and the run fails (operator can
     retry) instead of hanging until manual cancellation. At-most-once input
     semantics are preserved because the process is terminated whenever
     delivery is indeterminate.
   - Unchanged: intent-before-write persistence, `inputMu` ordering, successful
     write path latency (no extra lock), acknowledgement outcomes.

5. **Verification**
   - New `app_test.go` test using a fake process whose `WriteInput` blocks
     until a channel is closed (see `blockingInputProcess`, ≈5968) and a manual
     timer factory: issue a `provide_input` command; fire the timer; assert the
     command acknowledgement outcome is `failed`, `stopExecution` was invoked
     with `errInputWriteTimeout` (fake `Terminate` observed), and the final
     transition is `failed` whose payload `error` contains "standard input".
     Run the command handling in a goroutine and wait on a `context.WithTimeout`
     of a few seconds for the acknowledgement; on the old code the blocked
     write never returns, the wait expires, and the test fails — a failure
     caused by the unbounded write, not by setup (the same test with a
     non-blocking fake process passes on the old code; include that as a
     control case).
   - Deadlock reproduction (optional, Linux, runner-level, in
     `runner_unix_test.go`): a child that fills its stdin buffer without
     reading and floods stdout; with the old code the app-level write never
     returns. Keep it under `-short` skip if it takes >5 s.
   - Existing: `TestProvideInputPersistsIntentBeforeStdinWrite`,
     `TestInputLifecycleOrdersInputBeforeWaitingOrExit`,
     `TestCancelWinsInputLifecycleGateWithoutWritingStdin`,
     `TestShutdownCancelsInputCompletionRetryBeforeTerminating`, all
     supervisory tests, `-race` run.
   - Commands: `go test -run 'Input|Supervisory' -count=1 ./internal/app/`,
     `go test -race -count=1 ./internal/app/`.

6. **Implementation pitfalls**
   - The helper must be invoked **while still holding** `inputMu` (that is the
     point); do not restructure the lock.
   - Use `daemon.timer` (injectable) — a raw `time.After` makes the test
     non-deterministic.
   - `stopExecution` may be nil for a run whose process has not started; guard
     it.
   - Do not report `applied` after a timeout even if the goroutine later
     succeeds; the process is being terminated and the outcome is `failed`.
   - `terminateWhenContextCancels` uses `defaultTerminationGrace` (5 s); do
     not call `Terminate(ctx, 0)` here — graceful first.

7. **Acceptance and stop conditions**
   - New test passes deterministically 10× (`-count=10`); existing input tests
     unchanged; `-race` clean.
   - Stop if the ordering test suite requires holding `inputMu` for a path you
     cannot bound — report.

---

### Task T14: Log control-plane clock skew

1. **Coverage and prerequisites**
   - Addresses D-04 (as a diagnostic; no behavior change). After T13.

2. **Root cause / analysis**
   - Verified: `renewLeases` compares server-issued `LeaseExpiresAt` with
     `daemon.now()` (app.go:2760, 2818, 2862) and renews when remaining ≤
     `renewalThreshold()` (¾ of lease, capped 90 s; 2843–2852). Corrected
     analysis: with the default 120 s lease the daemon tolerates roughly ±85 s
     of skew before misjudging; with the 30 s lease used in CI/e2e the margin is
     ~17 s. Skew is therefore an operational hazard with short leases, not the
     immediate bug the review described. `RuntimeSnapshot.ServerTime`
     (protocol.go:187) is already delivered on every dispatch/heartbeat.

3. **Change specification**
   - In the snapshot-handling path of `sync`/`heartbeat` (where
     `protocol.RuntimeSnapshot` is received, ≈1022 and the dispatch
     equivalent): if `!snapshot.ServerTime.IsZero()`, compute
     `skew := snapshot.ServerTime.Sub(daemon.now())`; if `|skew| > leaseSafetyMargin`
     and the last logged state was "in tolerance", log
     `control_clock_skew_detected` (Warn) with `skew_ms` and
     `lease_safety_margin_ms`; when it returns within tolerance after a
     warning, log `control_clock_skew_cleared` (Info). Track state on `daemon`
     (`clockSkewWarned bool`) under `daemon.mu`.
   - Add one paragraph to `docs/goal-01-runbook.md` (runtime model / daemon
     configuration) stating that execution machines must run NTP and that the
     daemon warns when skew exceeds the lease safety margin.
   - Out of scope: skew compensation, lease TTL protocol change (DEC-9/R1-10).

4. **Why this is the right change**
   - Provides the evidence needed to decide whether compensation is worth a
     protocol change, without altering lease semantics.

5. **Verification**
   - New `app_test.go` test: fake control returns snapshots with
     `ServerTime = clock.Now().Add(30 * time.Second)`; assert the log writer
     (`options.logWriter`) receives one `control_clock_skew_detected` line
     across three heartbeats, then a `_cleared` line after `ServerTime` returns
     to `clock.Now()`. Fails before the fix (no line).
   - Command: `go test -run 'ClockSkew' -count=1 ./internal/app/`.

6. **Implementation pitfalls**
   - Fakes may leave `ServerTime` zero — skip silently in that case.
   - Log at most once per state change; a per-heartbeat warning at 5 s cadence
     is noise.

7. **Acceptance and stop conditions**
   - Test passes; no other behavior change (`renewLeases` untouched).

---

### Task T15: Run the daemon test suite with the race detector in CI

1. **Coverage and prerequisites**
   - Addresses D-11. After Phase B (so the new tests are included).

2. **Root cause**
   - Verified: `.github/workflows/ci.yml:69` runs
     `go test -count=3 -timeout 300s ./...` and `:89` (Windows)
     `go test -count=1 -timeout 180s ./...`; neither passes `-race`. The
     goal-04 runbook records that `-race` passed locally, but nothing enforces
     it.

3. **Change specification**
   - In job `daemon` (Linux) add a step after the existing test step:
     `- run: go test -race -count=1 -timeout 600s ./...`. Keep the existing
     `-count=3` step (flake detection) unchanged. Do not add `-race` on Windows
     (requires CGO/toolchain considerations; out of scope).
   - Out of scope: linters (DEC-6), coverage upload.

4. **Why this fixes it**
   - Data races in the reactor/outbox/lease goroutines become CI failures
     rather than field incidents.

5. **Verification**
   - Local: `cd daemon && go test -race -count=1 -timeout 600s ./...` passes.
   - CI: the workflow file parses (`docker run --rm -v "$PWD":/repo rhysd/actionlint:latest`
     if available, otherwise a YAML lint) and the next push shows the new step
     green. `scripts/audit-no-legacy-routes.sh` scans `ci.yml` — re-run it.

6. **Implementation pitfalls**
   - The runbook notes the Unix termination test adjusts for the race
     runtime's exit delay; if any test is timing-sensitive under `-race`, fix
     the test's timing assumptions, not by skipping it.

7. **Acceptance and stop conditions**
   - Local `-race` run green; workflow valid.

---

## D. Integration gates and completion report

### D.1 Focused checks per task
Listed in each task's §5. Run them immediately after the task, before
starting the next.

### D.2 Broader checks

After Phase A (T01–T08, T16), from `control/`:
```sh
mix format --check-formatted
mix compile --warnings-as-errors
mix test
MIX_ENV=prod mix release --overwrite
```
From repo root: `scripts/audit-no-legacy-routes.sh`.
With a running control plane (`mix phx.server`) and `browser/` deps installed:
`cd browser && npm test`.

After Phase B (T09–T14), from `daemon/`:
```sh
test -z "$(gofmt -l .)"
go vet ./...
go test -count=3 -timeout 300s ./...
go test -race -count=1 -timeout 600s ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=windows go vet ./...
GOOS=darwin go vet ./...
```
Live daemon scenarios (require a running control plane; see `ci.yml` lines
138–149 for the exact environment): build `./cmd/symmetry-fake-agent`, set
`SYMMETRY_E2E=1`, `SYMMETRY_E2E_URL`, `SYMMETRY_E2E_AGENT`, and run
`go test -count=1 -timeout 180s -v ./e2e -run '^(TestCoreDaemonWorkflows|TestPollingFallbackDispatchesWithoutNotifications|TestStaleRuntimeEpochCannotOverwriteNewGeneration|TestDaemonReregistersAfterRestart|TestDaemonReconnectReclaimsExpiredRunWithNewGeneration|TestTaskInputDistinguishesOmissionFromEmptyObject|TestGitWorktreeCleanupRemovesOwnedArtifacts|TestTerminalTransitionRetriesAfterLeaseExpiry)$'`.
Note the e2e suite skips unless the lease is 30 000 ms (`e2e/core_test.go:241`);
start the control plane with `SYMMETRY_LEASE_DURATION_MS=30000`.

Optional (Docker): `docker compose config && docker compose up --build`, then
`cd browser && npm run test:compose`.

### D.3 Final whole-diff review
Before reporting, review `git diff <baseline>..HEAD` for: files outside the
task specifications; weakened or deleted assertions; changed test
expectations not tied to a contract change named above; new dependencies in
`mix.exs`/`go.mod`; formatting-only churn; leftover debug logging; any
tokens/secrets in tests or docs; `DisallowUnknownFields` still present in
`state.go`; protocol doc edits limited to T02/T08 passages.

### D.4 Remaining risks and limits of verification
- T01's cross-OTP divergence is not reproducible in a single-version CI;
  correctness rests on the canonicalization unit tests.
- T10's fail-fast path executes only on unsupported platforms; CI covers
  compilation, not execution.
- T12 changes the journal schema; downgrading the daemon binary with pending
  journals will fail to decode them (documented, not mitigated).
- T13's timeout value (60 s) is a judgment call; a cooperative agent that
  legitimately stops reading stdin for longer while its stdin buffer is full
  will be terminated with an explicit cause instead of hanging.
- Browser and e2e suites require live services; if unavailable, record them
  as not executed — do not mark them passed.

### D.5 Completion report (required format)
1. Baseline: commit SHA, working-tree status, baseline test summaries (control,
   daemon, race, audit).
2. Tasks completed, with the issue IDs each closes.
3. Files changed (path list) and new files (marked as new).
4. Checks executed with exact commands and result summaries (pass/fail
   counts), including which were run with live services.
5. Checks blocked or skipped and why (e.g. no macOS, no Docker).
6. Deviations from this plan and their justification.
7. Unresolved findings, residual risks, and any new sibling instances of a
   fixed defect found but not modified (report, do not fix).

---

## E. Decisions required (blockers — do not implement without approval)

| ID | Decision | Options and recommendation | Affected findings |
|---|---|---|---|
| DEC-1 | Replace the static enrollment secret with single-use, expiring enrollment codes (new table, operator API/portal to mint, hashed storage) | Recommend: yes, as a separate feature; interim = T08 doc correction + DEC-2 | C-03 |
| DEC-2 | Add rate limiting (`plug_attack` or `hammer`) on `POST /portal/login`, `POST /api/v1/machines`, `POST /api/v1/provider-actions` | Requires a dependency; recommend `plug_attack` with per-IP ETS storage; needs approval of the dependency and limits | C-05 |
| DEC-3 | Add a metrics reporter (`telemetry_metrics_prometheus_core` + `/metrics` on a private listener) | Requires dependency + exposure decision; T16 makes events available regardless | C-06b, R1-15 |
| DEC-4 | Daemon shutdown: use a termination grace (5 s, matching `defaultTerminationGrace`) instead of `Terminate(ctx, 0)` in `stopAll` (app.go:3670) | Recommend: yes; adds ≤5 s to stop time. **Do not** queue `failed` on shutdown — lease expiry → reaper requeue is automatic, whereas `failed` requires manual retry | D-05 |
| DEC-5 | Process containment hardening: Linux `Pdeathsig`/cgroup v2 subtree; Windows `CREATE_SUSPENDED` → assign job → resume, `taskkill /F` for soft stop | Trade-offs (Go `Pdeathsig` is per-thread; Windows requires custom process creation); recommend a design spike first | D-07 |
| DEC-6 | Add linters to CI (`credo`, `dialyxir`, `sobelow`; `golangci-lint`) | Tooling/dependency additions; recommend yes, in a dedicated PR with baseline suppressions reviewed | Q-01, R2-a |
| DEC-7 | Daemon journal redesign: append-only segmented event log with a sequence watermark in the journal | Removes O(n²) rewrites and the 4 MiB coupling; on-disk format migration required; recommend after T12 stabilizes | D-01 (perf half), D-08 |
| DEC-8 | Enforce minimum machine-token length (≥32 bytes) at enrollment | Low severity; touches ≥10 test fixtures (`"other-token"`, `"machine-token-…"`); approve or defer | C-04 |
| DEC-9 | Product/packaging/positioning backlog (LICENSE, release, README pitch, agent profiles, toolchain floor, runners, docs restructure, governance, identity model, DB TLS, cost tracking, provider-credential model, container workspaces) | Owner decisions; not for the coding agent | R1-*, R2-c/d/e, C-11, C-12 |

---

## F. Worker guardrails (mandatory)

- Read `README.md`, `control/README.md`, `docs/protocol-v1.md`,
  `docs/goal-01-runbook.md`, and the code each task cites before editing.
- Record the validation baseline (section B.0) before implementation; separate
  pre-existing failures from new regressions in the report.
- Preserve unrelated user changes. Do not use destructive Git operations
  (`reset --hard`, `checkout -- .`, `clean -fd`, force-push, history rewrite).
- Follow established architecture and verified project conventions
  (transaction style, `slog` event naming, migration fail-closed `down/0`,
  conventional-commit subjects with `feat (scope):`/`fix (scope):`). Do not
  blindly copy nearby code that may contain the same defect (e.g. other
  `term_to_binary` sites — T01 lists all of them; other `String.to_integer`
  env reads — T04 names the in-scope ones).
- Avoid unrelated refactors, renames, formatting churn, dependency upgrades,
  and speculative cleanup. `orchestration.ex`/`app.go` size is out of scope.
- Change public APIs, schemas, configuration semantics, or dependencies only
  where a task explicitly requires it (T01 migration/schemas; T12 journal
  fields; T02/T08 doc semantics). No new dependencies in any task.
- Reuse suitable abstractions (`daemon.timer`, `mutateJournal`,
  `Repo.transaction`/`rollback`, existing test fakes). Add the proposed helpers
  only as specified.
- Do not weaken types, disable checks, suppress errors, or delete assertions to
  obtain a passing result. Do not remove `DisallowUnknownFields`,
  `--warnings-as-errors`, or `-count=3`.
- Do not add arbitrary defaults, retries, sleeps, or broad catch-and-ignore
  behavior that conceals a defect. Timeouts/backoffs introduced here (T09,
  T13) are specified with named constants and tests.
- Do not change test expectations or snapshots merely to match the
  implementation. The only expected expectation changes are: T05 (health
  response gains `checks`), T12 (dropped output produces `output_truncated`),
  T13 (blocked input now fails with a cause).
- Tests must exercise the faulty layer (real journal store, real controller
  pipeline, real `WriteInput` blocking via fake process) — do not mock away
  the defect or compute expected values with the code under test.
- Inspect sibling paths for the same root cause but modify only approved
  in-scope instances; report other instances in §7 of the completion report.
- Check affected consumers for changes to return values, status codes, side
  effects, and resource lifecycle: `reconcile` return type (T09), health
  status code 503 (T05), `RunJournal` fields (T12), `runningRun` fields (T13).
- If verification fails, stop dependent work and diagnose. Revise or roll back
  only task-scoped changes; preserve unrelated work and completed
  prerequisites.
- Stop when a change exceeds approved scope, a material design decision
  (section E) is required, or no credible verification method exists.
- If automated verification is unavailable (no Docker, no macOS, no live
  control plane), define the reproducible manual steps you would run, record
  the observable outcomes expected, state the limitation, and report the check
  as **not executed** — never as passed.
