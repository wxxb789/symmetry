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
| `control/test/symmetry_control_web/protocol_reason_coverage_test.exs` | `d6833f8925ee1aef965091013f0f083b558fd62ab6f6d92d29079c4a9c0bde0a` |
| `docs/plans/2026-11-22-rf-w5-05-error-reason-coverage-plan.md` | `29f1c1b602cbce2e12a373a3a275b12ce04698b4d917446e368405aeff71147d` |

`control/mix.exs` and `control/mix.lock` are untouched (sha256 of `mix.lock`
`60e0d7fa420951e413f04b01bfccaa678689c552f4a742fa3d621a65b8813758`, identical to the main
checkout that supplied `MIX_DEPS_PATH`); the two controller suites
(`protocol_controller_test.exs`, `goal_controller_test.exs`) are oracles and are unmodified.
Apply stage (simplify findings 1–4, brief
`<HOST-PATH>/.local/state/gdd/findings/rf-w5-05-error-reason-coverage-apply.md`): the test file
gained the served-table key-set cross-check, the non-atom fallback probe and a raising-handler
response-safety case, and the two counter tests share `attach_counter!/1`; `protocol.ex`, the plan
and `mix.lock` are untouched, and the counts and mutation logs below are re-bound to the new test
bytes.
Fix stage (review findings R1–R6, brief
`<HOST-PATH>/.local/state/gdd/findings/rf-w5-05-error-reason-coverage-fix1.md`): R1 — the §3 dump
was re-run with a corrected, CRLF-safe and fail-loud tool (`fix1/dump.exs`), re-binding the
byte-identity evidence to the delivered bytes; R2/R4/R6 — the test file gained the tuple-clause
no-emission pin, the rendered-path exactly-once pin and whole-map event comparisons (test bytes
`d6833f89…`, mutation matrix (e2)/(e4)/(e6)); R3/R5 — the lesson-32 line coordinates and the
inventory-identity claim are corrected. `protocol.ex` bytes are unchanged
(`d2968799c0e3b59a44e826ce3633e49a6779e8e810c6049412dfb38f156c5976`); the implement-stage test
bytes `76143b9f…` are superseded.
Scratch tools and raw outputs (outside the repository, GDD state scratch
`<HOST-PATH>/.local/state/gdd/scratch/w5-05/`): `scan.py`, `inventory-head.json`,
`inventory-apply.json`, `reasons.txt` (127 reasons), `dump.exs`, `base.txt`, `head.txt`,
`head-apply.txt` / `head-apply-clean.txt`, `base-head.diff`, `observe.exs`,
`counter-observation.txt`, `muta.log`–`mutd.log` (pre-apply bytes) and
`muta-apply.log`–`mutd-apply.log`, `webtree-apply.log`, `webtree-apply-excl.log`,
`orig/` (pre-mutation file copies + `sha256.txt`); runner `../rf-w5-05-mix.sh`.
Fix-stage tools and raw outputs, in `…/w5-05/fix1/`: `dump.exs` (corrected: CRLF-safe split +
fail-loud source-table guard), `reasons.txt` (copy of the frozen list), `base.txt` / `head.txt`
(re-recorded dumps) with `base.err` / `head.err` (guard summaries), `orig-protocol.ex` (pre-swap
bytes) and `snapshot-sha256.txt`, `crosscheck.py` + `crosscheck.out` (independent re-derivation of
the dump), `guard-a/` (input-shape guard run), `guard/` (source-vs-served guard run + control),
`mut/` (`orig-*.ex` + `sha256-orig.txt`, `mut-a.log`–`mut-d.log`, `mut-e2.log`, `mut-e4.log`,
`mut-e6.log`), `webtree-fix1.log`, `webtree-fix1-excl.log`.

## 1. Re-measured inventory (HEAD `e84973a7`)

Scan method: for every `control/lib/**/*.ex` file read whole,
`re.finditer(r"\{:error,\s*:([a-z][a-zA-Z0-9_]*)\}")`; table read from `@error_statuses %{`
through its closing brace in `protocol.ex`. Raw: `scan.py` output `inventory-head.json`;
re-run at the applied bytes → `inventory-apply.json`: sets, counts and span identical (72 files,
86/23/63/64, span `:18-112`), and the only difference is that the per-site `protocol.ex` lines of
two reasons shift +13 (`invalid_request`: 10 sites; `unauthenticated`: 3 sites) because the fallback
hunk grows `protocol.ex` below the table; all other site lines, sets, counts and span match.

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

Apply-stage additions (same contract, findings 1–4): `served_table_keys/0` reads
`control/lib/symmetry_control_web/protocol.ex`, extracts the `@error_statuses` key lines
(wrap-safe) and asserts key-set equality with `@table` — an extra or stray served key now reddens
(mutation (b)); the unmapped counter case also probes a non-atom reason
(`{:rf_w5_05_unmapped_probe, %{}}`; `Map.fetch/2` misses for any term); and a raising-handler
case attaches a handler that raises for its unique probe reason, with the response read under
`ExUnit.CaptureLog.with_log/1` (the telemetry failure report is absorbed and the 400 body stays
byte-identical; the handler-invoked signal is asserted directly, so the raise path cannot pass
unexercised).

Fix-stage additions (review findings R2/R4/R6, test bytes `d6833f89…`): the tuple-clause case
attaches `attach_counter!/1` and refutes an event whose metadata carries `{:invalid_contract, _}`
or `{:stale, _}` (an emission placed in those clauses now reddens — mutation (e2)); the counter
case asserts exactly one event after the rendered `Protocol.error/2` call as well (a duplicate
emission added on the wire path reddens — mutation (e4)); and each event is compared as whole
measurement/metadata maps through `assert_unmapped_event!/1`, so an additive payload key reddens
(mutation (e6)). The probe reason stays pinned inside every receive pattern, so the cases remain
race-free under `async: true`.

## 3. Byte-unchanged response evidence

`fix1/dump.exs` (fix r1) prints, for each of the 127 input reasons (64 table keys ∪ 86 scanned;
`reasons.txt`), the `error_details/1` tuple and the `Protocol.error/2`-rendered status + `resp_body`,
plus the two tuple-shaped clause forms. Run on the base `e84973a7` bytes and on the delivered bytes,
with the table parsed from the source file that was compiled (`PROTOCOL_SRC`):

| run | `protocol.ex` bytes | command | output | sha256 |
| --- | --- | --- | ---: | --- |
| base | blob `3283e6c9` (sha256 `b4bed534…`) | `mix run --no-start …/fix1/dump.exs` | `fix1/base.txt` (256 lines: 127 details + 127 render + 2 tuple) | `5aa77e8ef466c6c39eb2e95416b259f2c20fdd4448c90da715c1601a846e278b` |
| HEAD | delivered bytes (sha256 `d2968799…`) | same command | `fix1/head.txt` (256 lines) | `5aa77e8ef466c6c39eb2e95416b259f2c20fdd4448c90da715c1601a846e278b` |

The base bytes were materialized with `git show e84973a7:control/lib/symmetry_control_web/protocol.ex`
(blob hash verified) and the delivered bytes were restored afterwards from `fix1/orig-protocol.ex`
(sha256 `d2968799…` re-verified; `git status` clean for the file).
`cmp fix1/base.txt fix1/head.txt` → exit 0, 0 bytes. Every reason's status, code, message and
rendered JSON body is byte-identical; the only observable delta in the slice is the telemetry event.
The counter test also pins the probe reason's rendered body byte-exactly
(`{"error":{"code":"invalid_request","message":"request payload is invalid"}}`).

The tool is fail-loud: it shape-checks every input line, requires 127 distinct reasons covering all
64 table keys, and requires every parsed `@error_statuses` key to resolve to its source entry
through `error_details/1`. Both guard paths are exercised: a junk input line exits 1
(`fix1/guard-a/guard-a.out`), and a source copy whose `internal_error` entry reads `{400, …}` while
the compiled table serves `{500, …}` exits 1 (`fix1/guard/guard-b.out`); the same command against
the real source exits 0 (`fix1/guard/guard-b-control.out`). The stderr guard summary on both
recorded runs is `check inputs=127 mapped_inputs=64 unmapped_inputs=63 table_keys=64 table_hits=64`
(`fix1/base.err`, `fix1/head.err`).

**Superseded artifact (fix r1).** The implement-stage dump was invalid as byte-identity evidence:
`reasons.txt` is CRLF and the old `dump.exs` split it on `"\n"`, so every input atom was
`:"<name>\r"` and all 127 rows fell through to `400 invalid_request` — no row of the dump exercised
a table hit (only the two appended tuple-shaped lines reached an entry), so the recorded hash
`e7f3b7e9…` stayed identical under any mapped-reason regression. Independent re-derivation with a
different parser and row model (`fix1/crosscheck.py`,
exit 0; `fix1/crosscheck.out`) confirms the corrected artifact: 63 of the 64 mapped rows serve a
non-fallback tuple (`invalid_request` is the 64th, itself the fallback), the mapped rows hit status
400/401/403/404/409/410/422/500/502/503, every unmapped row returns the fallback tuple in both the
`details` and the `render` section, and 63 mapped rows differ from the old artifact — e.g.
`internal_error` was recorded `(400, invalid_request)` and is now `(500, internal_error)`. The
corrected `fix1/base.txt` / `fix1/head.txt` supersede `base.txt` / `head.txt` /
`head-apply-clean.txt` in the scratch root; the mapped path is now covered by the dump itself
(64/64 table hits) in addition to the frozen 64-key pin, the untouched table literal and the
unmodified controller oracles.

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
detached in `on_exit`, asserting exactly one event for the probe reason (`:rf_w5_05_unmapped_probe`,
unique to the test file, race-free under `async: true`) after each of the three emitting calls —
the direct `error_details/1` call, the non-atom tuple probe and the rendered `Protocol.error/2`
call (fix r1) — and no event for `:not_found`. Since fix r1 each event is compared as whole
measurement/metadata maps (`assert_unmapped_event!/1`), and the tuple-clause case refutes events
whose metadata carries `{:invalid_contract, _}` or `{:stale, _}`.

## 5. Mutation matrix (judging; each mutant restored, sha re-verified)

Re-run at the fix-stage test bytes `d6833f89…` (`fix1/mut/`): each mutant applied in place, focused
`mix test test/symmetry_control_web/protocol_reason_coverage_test.exs`, file restored from the
`fix1/mut/orig-*.ex` copies.

| # | mutant | exit | red |
| --- | --- | ---: | --- |
| a | `defp rf_w5_05_mutant, do: {:error, :brand_new_reason}` appended to `portal_api_controller.ex` | 2 (8/9) | "every closed {:error, :atom} reason in control/lib is pinned as mapped or known-unmapped" — the scan set gained `"brand_new_reason"` (`fix1/mut/mut-a.log:6`) |
| b | `timeout: {504, "timeout", "request timeout"},` added to `@error_statuses` | 2 (7/9) | "a scanned reason without a table entry returns the invalid_request fallback" — `left: {504, "timeout", "request timeout"}`; and "the served table has exactly the pinned keys" — the key set gained `"timeout"` (`fix1/mut/mut-b.log:6,15`) |
| c | counter emission deleted from the `:error` branch | 2 (7/9) | "an unmapped reason emits the fallback counter once and keeps the response bytes" (`assert_receive`: no matching message, mailbox empty) and "a raising counter handler cannot change the response bytes" (the raising handler was never invoked) (`fix1/mut/mut-c.log:6,20`) |
| d | emission moved to the clause head (fires for every reason; the fallback emission stayed, so an unmapped reason emits twice) | 2 (7/9) | "an unmapped reason emits the fallback counter once and keeps the response bytes" — the second emission was caught; and "a mapped reason does not emit the fallback counter" — unexpected `%{reason: :not_found}` message (`fix1/mut/mut-d.log:6,18`) |
| e2 | emission added to `error_details({:invalid_contract, _})` — a counter outside the fallback branch | 2 (8/9) | "the tuple-shaped contract and stale clauses keep their entries" — `refute_receive … %{reason: {:invalid_contract, _}}` at test `:237` (`fix1/mut/mut-e2.log:6`) |
| e4 | duplicate emission added to `error/2`'s scalar clause (the wire path) | 2 (8/9) | the rendered-path `refute_receive` at test `:260` caught the second event (`fix1/mut/mut-e4.log:6`) |
| e6 | `%{reason: reason}` → `%{reason: reason, status: 400}` in the fallback emission | 2 (8/9) | `assert metadata == %{reason: reason}` — `left: %{reason: :rf_w5_05_unmapped_probe, status: 400}` (`fix1/mut/mut-e6.log:6`) |

Restores, verified by sha256 after every mutant: `protocol.ex` back to
`d2968799c0e3b59a44e826ce3633e49a6779e8e810c6049412dfb38f156c5976` (b, c, d, e2, e4, e6);
`portal_api_controller.ex` back to
`13b9676a1cf46b73ce91d561e2fb4fba02984ed163df0dd247d161b816f18772` (a). The implement/apply-stage
logs `muta*.log`–`mutd*.log` and `muta-apply.log`–`mutd-apply.log` bind superseded test bytes.

## 6. Base-vs-HEAD control-flow audit (lesson-32 decision)

Base lines are `e84973a7`; HEAD lines are the delivered file.

| Path | Base | HEAD | Preserved |
| --- | --- | --- | --- |
| `error_details({:invalid_contract, _})` | `:148` → `invalid_contract` entry | `:148` identical | yes |
| `error_details({:stale, _})` | `:149` → `stale` entry | `:149` identical | yes |
| `error_details(reason)`, key present | `:151-152` `Map.get` hit → entry | `:151-154` `Map.fetch` hit → entry, no emission | yes |
| `error_details(reason)`, key absent | `:151-152` `Map.get` default → `invalid_request` | `:156-163` miss branch → emit, then the same `invalid_request` tuple | yes (tuple identical, counter added) |
| emission placement | n/a | inside `error_details/1`, before the caller's `put_status`/`json`; `:telemetry.execute/3` isolates handler exceptions in telemetry 1.4.3 (`do_execute/4` catches, detaches, emits `[telemetry, handler, failure]`) | n/a; no response interaction |
| `error/2`, `{:invalid_contract, _}` | `:116` → `error(conn, :invalid_contract)` | same line, byte-identical | yes |
| `error/2`, `{reason, fields}` when map | `:118-124` `put_status` → `json` merge `goal_error_fields/1` | same lines, byte-identical | yes |
| `error/2`, scalar/other | `:126-129` `put_status` → `json` | same lines, byte-identical | yes |
| `validation_error/2` | `:136-146` 422 `validation_failed` | same lines, byte-identical | yes |

The four `error/2`/`validation_error/2` coordinates are the same at both revisions: `protocol.ex`
lines 1–147 are byte-identical (blobs `3283e6c9` → `d119987b`), and the only differing region is
the fallback clause (HEAD `:151-164`). Fix r1 corrected the four rows of this table that cited
lines existing at neither revision (`:114`, `:116-123`, `:125-128`, `:138-146`).

## 7. Gates (final tree, fix r1)

Runner: `sh <HOST-PATH>/.local/state/gdd/scratch/rf-w5-05-mix.sh <mix args>` — portable Elixir
1.20.4 / OTP 29.0.6 via `erl.exe`, `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w5-05` (suite DB
`symmetry_control_testrf-w5-05`), `MIX_DEPS_PATH` at the main checkout's `deps/` (mix.lock
identical), shared PostgreSQL on 5432.

| Gate | Command | Exit | Counts |
| --- | --- | ---: | --- |
| format | `mix format --check-formatted` | 0 | whole project |
| compile | `mix compile --warnings-as-errors` | 0 | 1 file |
| new tests | `mix test test/symmetry_control_web/protocol_reason_coverage_test.exs` | 0 | 9 passed |
| oracles | `mix test test/…/protocol_controller_test.exs test/…/goal_controller_test.exs` | 0 | 45 passed |
| web tree | `mix test test/symmetry_control_web/` | 2 | 284/288 passed, 1 skipped; 4 failed — all four are `PortalControllerTest`'s `:portal_assets` cases, each failing in `ConnCase` setup at `PortalBundle.require_staged!/1` |
| web tree, bundle-dependent cases excluded | `mix test test/symmetry_control_web/ --exclude portal_assets` | 0 | 284 passed, 1 skipped, 4 excluded |
| whitespace | `git diff --check` | 0 | no errors |
| host-path scan | `git diff origin/main...HEAD` piped through `grep -i users/` (the program rule's profile-directory check) | 0 | 0 matches |

Full `mix test` is the conductor's gate-runner (contract), not run here.
The fix stage re-ran every row against the delivered test bytes (`fix1/webtree-fix1.log`,
`fix1/webtree-fix1-excl.log`, plus the focused runs logged in `fix1/mut/`); the exit-2 web-tree row
is the recorded environmental `:portal_assets` limit, unchanged from the implement and apply runs.

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
- **The scan covers only closed `{:error, :atom}` returns.** A reason shaped `{:error, {:atom, fields}}`
  reaches `error_details/1` as a bare atom through `error/2`'s `{reason, fields}` clause but is not in
  the scanned inventory; a new reason of that shape is visible only through the runtime
  `[:symmetry_control_web, :protocol, :unmapped_reason]` counter (PR #225 review F1; an independent
  mutant `{:error, {:x, %{}}}` left the suite green). The test name states this scope.
- The scan over-approximates reachability (option (b)); a scanned atom that never reaches
  `Protocol.error/1` is pinned to the fallback tuple like a wire-reachable one.
- The test pins the served table's values from the outside (each of the 64 keys → its tuple) and
  now also cross-checks the served key set against the source `@error_statuses` block, so an
  extra or stray entry reddens (mutation (b)); an entry for an atom no `control/lib` site returns
  still changes no wire byte for any scanned reason (over-approximation caveat above).
- The raising-handler case pins telemetry 1.4.3's handler-failure isolation (catch → detach →
  `[telemetry, handler, failure]` → log) as a response-safety property; a substrate change that
  let handler exceptions reach the response would redden it — the contract's "counter with a
  response side effect" failure control.
- **The fix-r1 dump guard covers table resolution, not rendering.** It fails when a
  `@error_statuses` key does not resolve to its source entry or the input list is malformed
  (verified: `fix1/guard-a/`, `fix1/guard/`), but the `render` rows are compared base-vs-HEAD
  byte-wise, so a rendering regression present in both runs would not be caught by the dump alone;
  the unmodified controller oracles and the coverage test's byte-exact probe-reason body assertion
  are the rendering evidence. (The superseded implement-stage dump had a second failure mode: 0 of
  64 table keys exercised — see section 3.)
- The fallback event's measurements and metadata are pinned as whole maps (fix r1), so an additive
  payload key is now a deliberate, reviewed change that must update the pin (mutation (e6)).
- `refactor/comment-policy-sweep` (unmerged) rewrites the `validation_error/2` comment at
  `protocol.ex:131-135`; this slice's only hunk is `:151-164` — line-disjoint, same file.
- No `docs/TODO.md` entry is added or closed: CW-9's second step (the `:internal_error` default
  flip with a client-error allowlist) stays out of scope and is tracked by the program plan row
  W5-05 and the contract's out-of-scope clause.

## 9. Conductor round (PR #225 review round 1)

The test module renames `@table` to `@frozen_wire_table` (its comment deleted), keeps only the concurrency note on `assert_unmapped_event!/1`, and names the scan scope in the inventory test (review F1-F3); section 8 records the open-tuple blind spot. New `protocol_reason_coverage_test.exs` sha256 `f9965dbb0ac71ea51e998d32446cbc37102c89ae279ed991a47dfe74133d0b43`; `protocol.ex` is unchanged. The full Control suite re-runs on this head before landing.
