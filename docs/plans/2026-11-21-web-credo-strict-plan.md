# web-credo-strict plan — the ten owned control-web lib files strict-Credo clean, no behaviour change

Contract: `goal-rf-w1-03-web-credo-strict.md` (GDD, 2026-11-21). Coordinate baseline: this
worktree's HEAD `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (`a3039f0`); every file, line and count in
the baseline sections below was read or measured at that revision, and the fix-stage amendment
points at the host-local evidence manifest for the re-measured fix bytes. Shape reference:
`7ea9a9d` (refactor slice closed by a plan file under `docs/plans/` plus the TODO bookkeeping it
actually closed; no in-repo goal archive participates — `docs/goals/archive/` holds the 000x
lettered contracts only).

## The strict-Credo state being burned down

`mix credo --strict` with Credo **1.7.19** and the default check set (no repo `.credo.exs` exists at
`a3039f0`; `control/.credo.exs` is W1-01's artifact and lands later) reports **474 issues**
project-wide over 154 files — the exact count and split recorded in
`findings/control-web.md` §1.4 (387 lib / 87 test). Reproduced for this slice in a throwaway copy of
`control/` (see *Measurement protocol*): 474, byte-comparable check histogram.

Restricted to the ten contract-owned files, the baseline is **15 issues** (control-web.md §5 S0,
:504):

| # | file:line:col | check | message | trigger |
| --- | --- | --- | --- | --- |
| 1 | `controllers/daemon_controller.ex:4:9` | Readability.AliasOrder | not alphabetically ordered among its group | `SymmetryControl.Integrations.ProviderAccess` |
| 2 | `controllers/daemon_controller.ex:25:25` | Refactor.Nesting | nested too deep (max 2, was 3) | `if` |
| 3 | `controllers/daemon_controller.ex:145:9` | Refactor.Nesting | nested too deep (max 2, was 3) | `with` |
| 4 | `controllers/daemon_controller.ex:357:21` | Refactor.Nesting | nested too deep (max 2, was 4) | `if` |
| 5 | `controllers/goal_controller.ex:10:9` | Readability.AliasOrder | not alphabetically ordered among its group | `SymmetryControlWeb.Protocol` |
| 6 | `controllers/health_controller.ex:16:65` | Warning.MissedMetadataKeyInLoggerConfig | Logger metadata key `check` not found in Logger config | `check` |
| 7 | `controllers/portal_session_controller.ex:53:18` | Design.AliasUsage | nested modules could be aliased at the top of the invoking module | `Plug.Conn.Status` |
| 8 | `controllers/task_controller.ex:20:20` | Refactor.Nesting | nested too deep (max 2, was 3) | `if` |
| 9 | `plugs/auth.ex:10:5` | Readability.WithSingleClause | `with` has one `<-` clause and an `else`, use `case` | `with` |
| 10 | `plugs/auth.ex:31:5` | Readability.WithSingleClause | same | `with` |
| 11 | `plugs/auth.ex:57:5` | Readability.WithSingleClause | same | `with` |
| 12 | `plugs/portal_auth.ex:7:9` | Readability.AliasOrder | not alphabetically ordered among its group | `SymmetryControlWeb.Protocol` |
| 13 | `protocol.ex:403:13` | Refactor.Nesting | nested too deep (max 2, was 3) | `case` |
| 14 | `user_socket.ex:25:14` | Refactor.Nesting | nested too deep (max 2, was 3) | `if` |
| 15 | `channels/daemon_channel.ex:1:11` | Readability.ModuleDoc | module has no `@moduledoc` tag | `SymmetryControlWeb.DaemonChannel` |

The plan-review amendment (P2-4) binds the slice to the §10.1 check set instead of the bare default
set: `mix credo gen.config` output plus `Credo.Check.Design.DuplicatedCode` (mass 40) and
`Credo.Check.Readability.MaxLineLength` (98), never a `checks: %{}` key. Measured on the same
throwaway copy, that set reports **528** project-wide (474 + 32 `MaxLineLength` + 22
`DuplicatedCode`, default-check histogram unchanged — the `extra:` form does not disable anything)
and **17** in the ten files: the 15 above plus

| 16 | `controllers/daemon_controller.ex:199:99` | Readability.MaxLineLength | line is too long (max 98, was 99) | `,` |
| 17 | `controllers/task_controller.ex:85:99` | Readability.MaxLineLength | line is too long (max 98, was 101) | `and` |

Both are in owned files, so acceptance under the amended set requires fixing them too. They are not
contract issues 16/17 — the contract fixes 15 — but "zero strict-Credo issues in the 10 files" under
the amended instrument is not reachable without them.

## Decisions, one per coordinate

Notation: *preserve* means the exact values, statuses and error envelopes observed before the edit
must not change.

1–2–3–4–8–13–14 — **Nesting by extraction** (Credo's own guidance: extract, never relax
`max_nesting`). `Refactor.Nesting` reports the single deepest leaf per `def` (one issue per
function, depth = the count of `if/unless/case/cond/for/fn/with` ancestors of the deepest leaf,
`deps/credo/lib/credo/check/refactor/nesting.ex:96-115`), so every function named here must end
**≤ 2** on *all* paths, not only at the reported line — after a partial fix the issue simply
re-appears at the next-deepest leaf:
- `daemon_controller.ex:25` (`enroll/2`: `with` → `case` → `if`): bind the created/ok status through
  the `created_or_ok/1` helper (`:created` → `:created`, `_` → `:ok`) — the same name
  `task_controller.ex` uses — no `if` inside the `case`. Status mapping `:created → 201`, otherwise
  `200` preserved.
- `daemon_controller.ex:145` (`claim_with_provider_access/2`: `with` → `fn` → `with`): move the
  `Repo.transaction(fn -> … end)` body into `claim_in_transaction/3`; lock order
  (`Orchestration.claim_owner` → transaction → `ProviderAccess.lock_claim_scope`), the
  `Repo.rollback(reason)` branch, the returned `{run, task, provider_access, disposition}` tuple and
  `claim_provider_access_result/1` (emit only on `:created`, replay unchanged) are preserved.
- `daemon_controller.ex:357` (`append_evidence/2`: `with` → `case` → `if` → `if`): route the
  `{:error, {:idempotency_conflict, fields}}` branch through `append_evidence_conflict/3`, whose
  non-batch fallback passes the original `{:idempotency_conflict, fields}` tuple to
  `Protocol.error/2`; the bare-atom branch goes through the shared `conflict_error/1`. The
  three-way envelope policy (batch + valid details → `:idempotency_conflict` with fields; batch +
  invalid details → `:invalid_request`; non-batch → the original `{:idempotency_conflict, fields}`
  tuple) is preserved verbatim.
- `task_controller.ex:20` (`create/2`: `case` → `with` → `if`): bind the status through
  `created_or_ok/1`, used by `create/2` and the identical `command/2` status line so the file keeps
  one convention (`201` on `:created`, `200` otherwise).
- `protocol.ex:403` (`parse_event_times/1`: `fn` → `case` → `case`): extract the per-event parse into
  `parse_event_time/1` (`with` `Map.fetch/2` binary guard + `DateTime.from_iso8601/1`); the function
  keeps returning `{:ok, Enum.reverse(parsed)}` on success and `{:error, :invalid_request}` for a
  non-list input, a non-map event, a missing/non-binary `occurred_at` and an unparseable timestamp.
- `user_socket.ex:25` (`bearer/1`: `case` → `fn` → `if`): extract the header predicate into
  `token_header/1`; the case-sensitive header-name comparison is not what is matched (the original
  lower-cases the name) and the returned token shape (`{:ok, binary}`, non-empty) is preserved.

5–12–1 — **AliasOrder**: sort each contiguous alias group case-insensitively, full name, prefix first
(`deps/credo/lib/credo/check/readability/alias_order.ex:5-7`). `daemon_controller.ex`: `Goals`,
`Goals.ContractValidation`, `Integrations.ProviderAccess`, `Orchestration`,
`Orchestration.Scheduler`, `Repo`, `SymmetryControlWeb.Plugs.StrictEvidenceJSON`,
`SymmetryControlWeb.Protocol`. `goal_controller.ex`: `…PortalJSON` before `…Protocol`.
`portal_auth.ex`: `…PortalSession` before `…Protocol`. No call site changes.

6 — **MissedMetadataKeyInLoggerConfig**: the check requires every metadata key passed to `Logger.*`
to be listed in the current environment's `config :logger, :default_formatter, metadata: […]`
(`…/warning/missed_metadata_key_in_logger_config.ex:124-137`). This tree declares
`metadata: [:request_id]` only (`control/config/config.exs:59-61`), so `check: name`
(`health_controller.ex:16`) is not in any formatter's metadata list — and `config/dev.exs:57`
overrides the format without `$metadata`, so the key is never rendered. `config/**` is outside the
slice's owned set, so the call site is the only place the issue can be fixed: the shipped call is
`Logger.error("health check failed")`, which keeps the rendered line byte-identical in every
environment and drops the unconfigured `:check` key. Folding the name into the message was rejected
in the R1 adjudication (contract clauses 1/3: no behaviour change); registering `:check` in
`config/**` would change test/prod rendering, where `$metadata` is part of the format string. HTTP
behaviour (`200` body with the four checks, `503` body on error) is untouched and the failing check's
name stays in the `503` body; no test asserts the log line (`health_controller_test.exs` asserts the
200 body only).

7 — **AliasUsage**: `alias Plug.Conn.Status` at the top (alphabetically before the
`SymmetryControlWeb.{…}` group) and call `Status.code(status)` at
`portal_session_controller.ex:53`. The status-code value passed to `send_resp/3` is unchanged.

9–10–11 — **WithSingleClause**: three single-clause `with`/`else` blocks in `plugs/auth.ex` become
a call to the new `Protocol.authenticate/2` (`protocol.ex:153-158`), which wraps
`Protocol.machine_token/1` plus the shared 401/halt policy; each plug's `call/2` keeps only its
credential-class dispatch. `Protocol.machine_token/1` returns exactly
`{:ok, binary} | {:error, :unauthenticated}` and `authenticate/2` maps the error to the plug's 401,
so the 401 (unknown/absent) / 403 (known-but-wrong class) mapping and the assigns
(`:enrollment_token`, `:machine`) are preserved. The credential precedence stays in
`Protocol.credential_class/1`; it is not touched.

15 — **ModuleDoc**: `@moduledoc false` on `DaemonChannel`. In the ten files only 6 of the 12 modules
carry `@moduledoc` (auth.ex ×3, portal_auth, protocol, daemon_channel) — the `*Controller`s and
`UserSocket` are skipped by Credo's default `ignore_names` — and `@moduledoc false` is the repo's
convention for undocumented private web modules.

16 — `daemon_controller.ex:199`: bind `lease_duration_ms/0` (a one-line wrapper over
`config(:lease_duration_ms)`) so the `renew_lease` clause fits 98 columns without touching the value
or the call.

17 — `task_controller.ex:85`: split the four-field `provide_input` clause's guard into a map+size
wrapper clause and a `provide_input_command/3` guard (`is_map/1`, `is_integer/1`, `> 0`,
`is_binary/1`), preserving both the accept and the `{:error, :invalid_request}` fall-through. Final
shape is confirmed against `mix format` output before the gates run.

## Measurement protocol (no repo `mix.exs`/`mix.lock` edit)

The credo dep lands in W1-02, which has not merged here (this worktree's `control/mix.exs` has no
`credo` entry; `control/.credo.exs` does not exist). Per the contract amendment, credo is injected
**only** into a throwaway `git archive HEAD control` copy under
the host scratch directory `<HOST-PATH>/refactor/scratch/w1-03/`:
- `control/mix.exs` in the copy gains `{:credo, "~> 1.7", only: [:dev, :test], runtime: false}`;
  `deps/credo`, `deps/bunt`, `deps/file_system` are copied from the W1-02 worktree, which already
  resolved credo **1.7.19** (the version of the finding's baseline).
- Run 1 = default check set (no `.credo.exs` in the copy).
- Run 2 = §10.1 instrument: `checks: %{extra: [{Credo.Check.Design.DuplicatedCode,
  [mass_threshold: 40]}, {Credo.Check.Readability.MaxLineLength, [max_length: 98]}]}`,
  `files.included: ["lib/", "test/"]`, no `checks: %{}`. The exact file is kept at
  `…/scratch/w1-03/credo-10-1.exs` and its text is quoted in the evidence record.
- "Before" = the copy's archived HEAD bytes. "After" = the worktree's edited copies of the ten files
  dropped into the same tree, so both runs share one dep set, one credo version and one project.
- Both runs use `--strict --format json`; the ten-file subset is selected by filename, project counts
  are totals. Excluding files from `files.included` to lower the count is explicitly *not* used
  (§10.1 includes `lib/` and `test/`).

## Evidence plan (all foreground)

| # | check | command |
| --- | --- | --- |
| E1 | baseline + after, default set | scratch `mix credo --strict --format json` (runs 1/1′) |
| E2 | baseline + after, §10.1 set | scratch `mix credo --strict --format json` with `credo-10-1.exs` (runs 2/2′) |
| E3 | formatter | `mix format --check-formatted` (portable toolchain, MIX_ENV=test) |
| E4 | compile | `mix compile --warnings-as-errors` |
| E5 | targeted suites | `mix test test/symmetry_control_web/` (MIX_TEST_PARTITION=rf-w1-03) |
| E6 | whitespace | `git diff --check` |
| E7 | full suite | left to the conductor gate-runner (never two full suites on this host) |

Portable-toolchain recipe for this worktree:
`<HOST-PATH>/refactor/scratch/w1-03-mix.sh` (adapted from
`scratch/retro-g2-mix.sh`: `erl.exe` invoked directly, `MIX_TEST_PARTITION=rf-w1-03`,
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset).

## Behaviour-preservation oracle

Until the fix stage no test file was edited (test lint belongs to W1-13/W3); the fix stage adds four
discriminating tests under the conductor's R4/R5 extension (see the fix-stage amendment below). The
oracle is the `test/symmetry_control_web/` suite — 22 `.exs` files, including the route-level suites
that exercise every pipeline the three auth plugs guard (`protocol_controller_test.exs`,
`operator_read_controller_test.exs`, `provider_action_controller_test.exs`), the daemon
fence/evidence receipts (`protocol_controller_test.exs`, `goal_machine_controller_test.exs`), the
portal projection mounts (`goal_controller_test.exs`, `portal_session_test.exs`,
`portal_workflow_test.exs`), the channel mount (`daemon_channel_test.exs`) and health
(`health_controller_test.exs`). The two structural extractions with no direct unit test
(`parse_event_time/1`, `token_header/1`) are covered through `append_events`
(`protocol_controller_test.exs`) and the socket connect path (`daemon_channel_test.exs`), plus the
fix-stage tests.

## Out of scope, with reasons

- **Test-file lint issues** (31 in the subtree: 19 AliasUsage, of which 11 sit in
  `provider_action_controller_test.exs`, whose 15 issues in total include 4 other checks): owned by
  W1-13 and the W3 batches; the contract excludes them. Fixing them would collide with their
  writers, and the fix stage's own tests are written against the §10.1 instrument so they add none.
- **`control/.credo.exs`**: W1-01 owns it; this slice only *uses* the §10.1 content as a local
  instrument. `--files-included`/positional narrowing does not work in Credo 1.7.19 (finding §5),
  hence the throwaway copy.
- **`control/mix.exs` / `mix.lock`**: W1-02 owns them; forbidden by the amendment.
- **Other lib files** and **Styler** (D12/W6-11): contract excludes; Styler would mechanically
  recreate the alias-order/alias-usage families later, and re-doing them here would collide with it.
- **`config/**`** (the logger metadata list): no wave-1 owner; adding `:check` to
  `metadata: [:request_id]` would change formatter output in test/prod, where `$metadata` is part of
  the format string, which is a wider change than this slice's contract.
- **`docs/TODO.md`**: no entry in it names credo/web-lint at `a3039f0` (checked), so this increment
  closes none; no TODO move is invented.

## Residual risks

- `health_controller.ex`: the rendered log line is byte-identical to `a3039f0` in all three
  environments; the never-rendered `:check` metadata key is dropped (MissedMetadataKeyInLoggerConfig
  requires configured keys and `config/**` is outside the slice). No consumer; nothing asserts the
  line.
- Nesting depth is a whole-function property: a wrong extraction could leave a different leaf at
  depth 3; the after-run (E1/E2) is the oracle for the count, and E5 for the behaviour.
- The §10.1 instrument is *my* reconstruction of W1-01's config (W1-01 has not written it yet). It
  uses the documented `checks.extra` form, preserves the default histogram exactly (474 → 528 = +32
  +22), and the ten-file result is recorded under both instruments so the re-measurement after
  W1-01/W1-02 merge is a diff of configs, not a new investigation.
- The scratch tree holds `a3039f0`'s other lib/test files (the fix-stage runs additionally copy in
  the ten worktree lib files and the two edited test files), so the project counts here are not the
  counts CI will see after W1-02 (which deletes `telemetry.ex` and adds credo) and W1-13 merge; the
  load-bearing numbers are the ten-file subset (0) and the same-tree drop (≥15).

## Fix-stage amendment (2026-11-21, review R1–R5)

The review of `a3039f0..77fd035` returned `DECISION_NEEDED`; the conductor adjudicated the five
findings and extended the owned set to this plan file and to
`control/test/symmetry_control_web/**` (except the three W1-13 portal files). Resolutions, all
shipped by the fix stage:

- **R1 (health log)** — decision 6 above rewritten: the shipped call is the bare
  `Logger.error("health check failed")`. Rendering is byte-identical in dev/test/prod; the
  unconfigured `:check` metadata key is gone, which is the only shape strict Credo accepts. Folding
  the name into the message was reverted.
- **R2 (decision drift)** — decisions 2/4/9–11 now name the shipped helpers (`created_or_ok/1`,
  `append_evidence_conflict/3` + `conflict_error/1`, `Protocol.authenticate/2`).
- **R3 (statistics)** — the module-count, suite-size, socket-connect and AliasUsage statements are
  corrected to the verified numbers.
- **R4/R5 (coverage)** — four discriminating tests added: `protocol_controller_test.exs` ("task
  creation replays the stored task…", "provide_input accepts the four-key waiting_transition_id
  fence", "events with invalid occurred_at values…") and `daemon_channel_test.exs` ("accepts a
  mixed-case X-Symmetry-Token header…"). Each was shown red under its named swap:
  `generation >= 0`; `is_binary(waiting_id)` dropped; the accept clause flipped to reject;
  `parse_event_time/1` returning `:stale`; case-sensitive header matching; the header-name filter
  dropped; `create/2` pinned to `:created`.

Re-measured on the fix bytes: both instruments still report **0** issues in the ten files; the
project totals and the mutation-run logs live in the slice's host-local evidence manifest
(`<HOST-PATH>/refactor/scratch/w1-03/EVIDENCE-MANIFEST.md`), their single source.
