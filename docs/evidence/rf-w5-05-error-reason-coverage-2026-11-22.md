# rf-w5-05 error-reason coverage — evidence record (2026-11-22)

Subject: `SymmetryControlWeb.Protocol.error_details/1`'s unmapped-reason fallback now emits one
`:telemetry` event and is covered by a new inventory test that pins every `{:error, :atom}` reason
returned anywhere in `control/lib` to its served `{status, code, message}` tuple. Contract:
`goal-rf-w5-05-error-reason-coverage.md` (GDD, 2026-11-21) including its binding conductor
decisions (D1 byte-identical surfaces, Decision I `:telemetry`, P2 fixes, lesson-32 control-flow
audit). Base revision `e84973a7` (= `origin/main` = this worktree's HEAD before the edit); branch
`refactor/rf-w5-05-error-reason-coverage`. Plan:
`docs/plans/2026-11-22-rf-w5-05-error-reason-coverage-plan.md`.

Delivered files (sha256 of the delivered bytes):

| file | sha256 |
| --- | --- |
| `control/lib/symmetry_control_web/protocol.ex` | `d2968799c0e3b59a44e826ce3633e49a6779e8e810c6049412dfb38f156c5976` |
| `control/test/symmetry_control_web/protocol_reason_coverage_test.exs` | `87c8690fe2e0507599e1944b24116f697ce441c2fb1114e490fdf31cca15b8d1` |
| `docs/plans/2026-11-22-rf-w5-05-error-reason-coverage-plan.md` | `38bb0948c8089b480a7ad443b138d24c3ec59cd7f83bf9dbea6c78ddcd4ad4ce` |

`control/mix.exs` and `control/mix.lock` are untouched (sha256 of `mix.lock`
`60e0d7fa420951e413f04b01bfccaa678689c552f4a742fa3d621a65b8813758`, identical to the main
checkout that supplied `MIX_DEPS_PATH`); the two controller suites
(`protocol_controller_test.exs`, `goal_controller_test.exs`) are oracles and are unmodified.
Scratch tools and raw outputs (outside the repository, GDD state scratch
`<HOST-PATH>/.local/state/gdd/scratch/w5-05/`): `scan.py`, `inventory-head.json`,
`reasons.txt` (127 reasons), `dump.exs`, `base.txt`, `head.txt`, `base-head.diff`,
`observe.exs`, `counter-observation.txt`, `muta.log`, `mutb.log`, `mutc.log`, `mutd.log`,
`orig/` (pre-mutation file copies + `sha256.txt`); runner `../rf-w5-05-mix.sh`.

## 1. Re-measured inventory (HEAD `e84973a7`)

Scan method: for every `control/lib/**/*.ex` file read whole,
`re.finditer(r"\{:error,\s*:([a-z][a-zA-Z0-9_]*)\}")`; table read from `@error_statuses %{`
through its closing brace in `protocol.ex`. Raw: `scan.py` output `inventory-head.json`.

| measure | contract (re-verified at `c9ed23b4`) | HEAD | delta |
| --- | ---: | ---: | --- |
| distinct `{:error, :atom}` reasons in `control/lib` | 86 | 86 | none |
| present in `@error_statuses` (mapped) | 23 | 23 | none |
| absent (unmapped, pinned allowlist) | 63 | 63 | none |
| `@error_statuses` entries | 64 | 64 | none |
| `@error_statuses` span | `:16-110` | `:18-112` | +2 lines |
| fallback clause | `:132-133` | `:151-152` | +19 lines (W2-03/#163) |
| `error_details/1` consumers | `portal_api_controller.ex:141,460,513,518` | `:152,446,499,504` | same 4 call sites, shifted |
| | `portal_json.ex:138` | `:138` | none |
| `error_details/1` arity | 1 | 1 | none |

Files scanned: 72. Table keys with no `{:error, :atom}` site in `control/lib`: 41 — pinned by the
test's table pin although the scan does not derive them.

## 2. Detection mechanism (recorded choice)

Scan + double pin: the test's `scanned_reasons/0` re-runs the scan above at test time and asserts
sorted string set equality with `@scanned_mapped` (23 atoms) ++ `@scanned_unmapped` (63 atoms), so
a new/removed/renamed `{:error, atom}` site reddens without a manifest. Per-reason assertions:
mapped → exactly `@table[reason]`; unmapped → exactly `@table.invalid_request`; all 64 `@table`
entries asserted against `error_details/1` (the 41 non-scanned keys included); the two tuple-shaped
clauses pinned in their own case.

Accepted cost (conductor option (b), recorded): the whole-tree scan **over-approximates
reachability** — a reason returned on a path that never reaches `Protocol.error/1`, or a matching
string inside a comment, is scanned and asserted against the same tuples. No call-graph
restriction in this slice; the pins, not the scan, are the freeze.

## 3. Byte-unchanged response evidence

`dump.exs` prints, for each of the 127 input reasons (64 table keys ∪ 86 scanned; `reasons.txt`),
the `error_details/1` tuple and the `Protocol.error/2`-rendered status + `resp_body`, plus the two
tuple-shaped clause forms. Run before the `protocol.ex` edit and after it:

| run | command | output | sha256 |
| --- | --- | ---: | --- |
| base (pre-edit) | `mix run --no-start …/dump.exs` | `base.txt` (256 lines: 127 details + 127 render + 2 tuple) | `e7f3b7e96edf7bfa92adb0557adc85b452010cbf393ed67b131f18cfd0c9e7c3` |
| HEAD | same command | `head.txt` (256 lines) | `e7f3b7e96edf7bfa92adb0557adc85b452010cbf393ed67b131f18cfd0c9e7c3` |

`diff base.txt head.txt` → exit 0, 0 bytes (`base-head.diff`). Every reason's status, code, message
and rendered JSON body is byte-identical; the only observable delta in the slice is the telemetry
event. The counter test also pins the probe reason's rendered body byte-exactly
(`{"error":{"code":"invalid_request","message":"request payload is invalid"}}`).

## 4. Counter observation (raw output)

`observe.exs` attaches a handler and prints the received event for one unmapped probe reason and
for one mapped reason (`counter-observation.txt`):

```
unmapped error_details/1: {400, "invalid_request", "request payload is invalid"}
unmapped event: {[:symmetry_control_web, :protocol, :unmapped_reason], %{count: 1},
 %{reason: :rf_w5_05_observation_probe}}
mapped error_details/1: {404, "not_found", "resource was not found"}
mapped event: :no_event
```

The event name, `%{count: 1}` measurements and `%{reason: reason}` metadata are Decision I's exact
form; a table hit emits nothing. The test side observes the same event through a handler id of
`{SymmetryControlWeb.ProtocolReasonCoverageTest, :unmapped_probe, make_ref()}` (unique per run),
detached in `on_exit`, asserting one event for the probe reason (`:rf_w5_05_unmapped_probe`,
unique to the test file, race-free under `async: true`) and no event for `:not_found`.

## 5. Mutation matrix (judging; each mutant restored, sha re-verified)

| # | mutant | command | exit | red |
| --- | --- | ---: | ---: | --- |
| a | `defp rf_w5_05_mutant, do: {:error, :brand_new_reason}` appended to `portal_api_controller.ex` | `mix test test/symmetry_control_web/protocol_reason_coverage_test.exs` | 2 (6/7) | "every control/lib error reason is pinned as mapped or known-unmapped" — actual set gained `"brand_new_reason"` (`muta.log:19`) |
| b | `timeout: {504, "timeout", "request timeout"},` added to `@error_statuses` | same | 2 (6/7) | "a scanned reason without a table entry returns the invalid_request fallback" — `left: {504, "timeout", "request timeout"}` vs `right: {400, "invalid_request", …}` (`mutb.log`) |
| c | counter emission deleted from the `:error` branch | same | 2 (6/7) | "an unmapped reason emits the fallback counter once and keeps the response bytes" — `assert_receive`: no matching message after 100ms, mailbox empty (`mutc.log`) |
| d | emission moved to the clause head (fires for every reason; the fallback emission also stayed, so an unmapped reason emits twice) | same | 2 (5/7) | "a mapped reason does not emit the fallback counter" — unexpected `%{reason: :not_found}` message; and the unmapped case's "exactly once" `refute_receive` caught the second emission (`mutd.log`) |

Restores, verified by sha256: `protocol.ex` back to
`d2968799c0e3b59a44e826ce3633e49a6779e8e810c6049412dfb38f156c5976` after mutations b, c and d;
`portal_api_controller.ex` back to
`13b9676a1cf46b73ce91d561e2fb4fba02984ed163df0dd247d161b816f18772` after mutation a.

## 6. Base-vs-HEAD control-flow audit (lesson-32 decision)

Base lines are `e84973a7`; HEAD lines are the delivered file.

| Path | Base | HEAD | Preserved |
| --- | --- | --- | --- |
| `error_details({:invalid_contract, _})` | `:148` → `invalid_contract` entry | `:148` identical | yes |
| `error_details({:stale, _})` | `:149` → `stale` entry | `:149` identical | yes |
| `error_details(reason)`, key present | `:151-152` `Map.get` hit → entry | `:151-154` `Map.fetch` hit → entry, no emission | yes |
| `error_details(reason)`, key absent | `:151-152` `Map.get` default → `invalid_request` | `:156-163` miss branch → emit, then the same `invalid_request` tuple | yes (tuple identical, counter added) |
| emission placement | n/a | inside `error_details/1`, before the caller's `put_status`/`json`; `:telemetry.execute/3` isolates handler exceptions in telemetry 1.4.3 (`do_execute/4` catches, detaches, emits `[telemetry, handler, failure]`) | n/a; no response interaction |
| `error/2`, `{:invalid_contract, _}` | `:114` → `error(conn, :invalid_contract)` | identical | yes |
| `error/2`, `{reason, fields}` when map | `:116-123` `put_status` → `json` merge `goal_error_fields/1` | identical | yes |
| `error/2`, scalar/other | `:125-128` `put_status` → `json` | identical | yes |
| `validation_error/2` | `:138-146` 422 `validation_failed` | identical | yes |

## 7. Gates (final tree)

Runner: `sh <HOST-PATH>/.local/state/gdd/scratch/rf-w5-05-mix.sh <mix args>` — portable Elixir
1.20.4 / OTP 29.0.6 via `erl.exe`, `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w5-05` (suite DB
`symmetry_control_testrf-w5-05`), `MIX_DEPS_PATH` at the main checkout's `deps/` (mix.lock
identical), shared PostgreSQL on 5432.

| Gate | Command | Exit | Counts |
| --- | --- | ---: | --- |
| format | `mix format --check-formatted` | 0 | whole project |
| compile | `mix compile --warnings-as-errors` | 0 | 1 file |
| new tests | `mix test test/symmetry_control_web/protocol_reason_coverage_test.exs` | 0 | 7 passed |
| oracles | `mix test test/…/protocol_controller_test.exs test/…/goal_controller_test.exs` | 0 | 45 passed |
| web tree | `mix test test/symmetry_control_web/` | 2 | 282/286 passed, 1 skipped; 4 failed — all four are `PortalControllerTest`'s `:portal_assets` cases, each failing in `ConnCase` setup at `PortalBundle.require_staged!/1` |
| web tree, bundle-dependent cases excluded | `mix test test/symmetry_control_web/ --exclude portal_assets` | 0 | 282 passed, 1 skipped, 4 excluded |

Full `mix test` is the conductor's gate-runner (contract), not run here.

## 8. Residual risks and known limits

- **The four `:portal_assets` cases are unavailable evidence on this host.**
  `control/priv/static/portal/` is gitignored frontend build output; a fresh worktree has it only
  after `pnpm --filter @symmetry/frontend build`, and CI stages the `contracts` job's artifact
  instead. The staging was attempted here: `mise exec pnpm@12.17.0 -- pnpm install
  --frozen-lockfile` ended after 6m0.6s with
  `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` (`registry.npmjs.org/@jridgewell%2Fremapping`:
  `received fatal alert: HandshakeFailure`) and left `frontend/node_modules` empty. The four cases
  therefore fail at the setup prerequisite in both runs; the base-revision control run of
  `test/symmetry_control_web/controllers/portal_controller_test.exs` with the pre-edit
  `protocol.ex` fails the same four cases (9/13 passed, `base-portal-controller.log`), so they are
  environmental and pre-date the slice. `test/support/portal_bundle.ex`'s own moduledoc sanctions
  `mix test --exclude portal_assets` for a host without a usable staging; the excluded run is the
  substantive web-tree gate here.
- The scan over-approximates reachability (option (b)); a scanned atom that never reaches
  `Protocol.error/1` is pinned to the fallback tuple like a wire-reachable one.
- The test pins the served table from the outside (each of the 64 keys → its tuple); an extra
  `@error_statuses` entry for an atom no `control/lib` site returns is not observable from outside
  and changes no wire byte for any scanned reason.
- `refactor/comment-policy-sweep` (unmerged) rewrites the `validation_error/2` comment at
  `protocol.ex:131-135`; this slice's only hunk is `:151-165` — line-disjoint, same file.
- No `docs/TODO.md` entry is added or closed: CW-9's second step (the `:internal_error` default
  flip with a client-error allowlist) stays out of scope and is tracked by the program plan row
  W5-05 and the contract's out-of-scope clause.
