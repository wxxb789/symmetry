# rf-w5-05 error-reason coverage plan — every `control/lib` reason pinned, one counter on the unmapped fallback

Contract: the GDD goal contract `goal-rf-w5-05-error-reason-coverage.md` (2026-11-21, GDD state
root) including its binding conductor decisions: the Wave-5 draft round (D1 byte-identical
surfaces, D2 report-only dLOC, CW-9 step 2 explicitly not authorized), the W5 review reconcile
round (Decision I — reuse `:telemetry`; P2 fixes — unique probe reason, whole-tree
over-approximation recorded, controller suites are oracles) and the lesson-32 control-flow
decision. Baseline for every coordinate: this worktree's HEAD `e84973a7` = `origin/main`
(`refactor/rf-w5-05-error-reason-coverage`). The contract's dispatch gates are satisfied in this
base: #163 (W2-03), #192 (W3-03) and #211 (W3-14) are all ancestry of `e84973a7`.

Shape reference: `8fc5e338` (rf-w5-06, #199) plus `cca92284` (rf-w5-01, #219) — the W5-06 plan is
the in-repo record for a behaviour-preserving control-web slice, and the W5-01/W3-09/W4-17b
slices add the `docs/evidence/<slice>-<date>.md` record this contract's clause 2 and its lesson-32
decision demand. Both records are written here; the GDD contract and its index live outside this
repository and this stage does not move them.

Fix stage (review findings R1–R6, brief
`<HOST-PATH>/.local/state/gdd/findings/rf-w5-05-error-reason-coverage-fix1.md`): the evidence
record's byte-identity dump was re-run with a corrected CRLF-safe, fail-loud tool (`fix1/dump.exs`);
the coverage test gained the tuple-clause no-emission pin, the rendered-path exactly-once pin and
whole-map event comparisons; the lesson-32 line coordinates and the evidence record's
inventory-identity claim are corrected.
`protocol.ex` bytes are unchanged (`sha256 d2968799…`); the delivered test file is `sha256
d6833f89…` (evidence record header).

## Measured basis (dispatch HEAD `e84973a7`)

Scanner: `<HOST-PATH>/.local/state/gdd/scratch/w5-05/scan.py` — for every `control/lib/**/*.ex`
file read whole, `re.finditer(r"\{:error,\s*:([a-z][a-zA-Z0-9_]*)\}")`; the table is read from
`@error_statuses %{` through its closing brace in `control/lib/symmetry_control_web/protocol.ex`.
Raw inventory: `<HOST-PATH>/.local/state/gdd/scratch/w5-05/inventory-head.json`.

| measure | contract (re-verified at `c9ed23b4`) | HEAD `e84973a7` | delta |
| --- | ---: | ---: | --- |
| distinct `{:error, :atom}` reasons in `control/lib` | 86 | 86 | none |
| present in `@error_statuses` | 23 | 23 | none |
| absent from `@error_statuses` | 63 | 63 | none |
| `@error_statuses` entries | 64 | 64 | none |
| `@error_statuses` span | `:16-110` | `:18-112` | +2 lines (block moved down; same 64 entries) |
| fallback `Map.get` clause | `:132-133` | `:151-152` | +19 lines (W2-03/#163 region above it) |
| `error_details/1` consumers | `portal_api_controller.ex:141,460,513,518` | `:152,446,499,504` | controller lines shifted, same 4 call sites |
| | `portal_json.ex:138` | `:138` | none |
| `error_details/1` arity | 1 | 1 | none (the draft's `/2` was wrong) |

Files scanned: 72. Table keys not returned by any `{:error, :atom}` site in `control/lib`: 41
(e.g. `terminal_grace_expired`, `invalid_transition`, `handoff_*`, `requested_session_*` — reached
through other error shapes). Those 41 keys are pinned by the test's table pin even though the scan
does not derive them.

## Detection mechanism (contract Change (1) choice, recorded)

**Test-side whole-file scan plus doubly pinned lists.** The scan is the discovery mechanism; the
pins are the freeze. Concretely, `control/test/symmetry_control_web/protocol_reason_coverage_test.exs`
holds:

- `@table` — all 64 `@error_statuses` entries as `reason => {status, code, message}` literals,
  generated from the source at `e84973a7` (not transcribed by hand);
- `@scanned_mapped` — the 23 scanned reasons that are table keys;
- `@scanned_unmapped` — the 63 scanned reasons that are not;

and asserts (i) the scan's sorted string set equals `@scanned_mapped ++ @scanned_unmapped`
(sorted) — a new `{:error, :new_atom}` return, or a removed/renamed one, reddens; (ii) every
`@scanned_mapped` reason returns exactly its `@table` tuple; (iii) every `@scanned_unmapped`
reason returns exactly the `invalid_request` tuple; (iv) every one of the 64 `@table` keys returns
its pinned tuple — the 41 keys no scan site returns included.

The whole-tree scan **over-approximates reachability**: a reason returned on a path that never
reaches `Protocol.error/1` is asserted against the same tuples as a wire-reachable one, and a
string or doc-comment occurrence of `{:error, :atom}` inside `control/lib` would be scanned too.
That cost is the conductor's accepted option (b) (restricting to reachable call paths needs
call-graph work this slice cannot pin). The pins are what keep the over-approximation honest: a
scanned non-reason must be deliberately added to `@scanned_unmapped` (and then asserted against
the fallback tuple), so no scanned atom can pass unnoticed.

## Public / API shape after the increment

`SymmetryControlWeb.Protocol` keeps its full surface; `error_details/1` keeps the same contract:
`{:invalid_contract, _}` → `{422, "invalid_contract", "goal contract is invalid"}`;
`{:stale, _}` → `{409, "stale", "resource changed since it was loaded"}`; any other key in the
table → its entry; any other key → `{400, "invalid_request", "request payload is invalid"}`. The
fallback clause's `Map.get/3` becomes a `Map.fetch/2` case whose miss branch emits one event
before returning the same default tuple:

```elixir
:telemetry.execute(
  [:symmetry_control_web, :protocol, :unmapped_reason],
  %{count: 1},
  %{reason: reason}
)
```

Event name `[:symmetry_control_web, :protocol, :unmapped_reason]`, measurements `%{count: 1}`,
metadata `%{reason: reason}` — exactly Decision I. No new dependency (`:telemetry` 1.4.3 is in
`mix.lock` and already emitted by `orchestration.ex:4711`; `mix.exs`/`mix.lock` untouched), no
public/private function added or removed, no response byte changed.

## Edge-case semantics the contract demands

- **Emission exactly on the fallback branch.** A table hit returns without emitting; the two
  tuple-shaped clauses return without emitting (they are matched before the fallback); a reason
  that is not a table key emits once and returns the `invalid_request` tuple. A non-atom reason
  (e.g. an unexpected tuple) also takes the fallback and emits with that term as metadata —
  behaviour-identical to today's `Map.get/3` default.
- **No wire delta.** Status, code and message are unchanged for every reason. The rendered bytes
  are frozen by: the base-vs-HEAD dump over all 127 reasons (64 table keys ∪ 86 scanned) through
  both `error_details/1` and `Protocol.error/2`, with its fail-loud per-key guard (fix r1); the
  counter test's byte-exact assertion on the probe reason's rendered body; and the unmodified
  controller oracles.
- **The counter cannot fail the response.** `:telemetry.execute/3` isolates handler exceptions in
  1.4.3 (`telemetry.erl:221-238`: catch, detach the handler, emit `[telemetry, handler, failure]`,
  log) — a faulty handler cannot turn the 400 into a 500.
- **The counter is the only observable delta** (D1). Test-visible: one event; the run's report and
  the evidence record carry the observed measurement/metadata.

## Base-vs-HEAD control-flow audit (lesson-32 decision)

| Path | Base `e84973a7` | HEAD | Preserved |
| --- | --- | --- | --- |
| `error_details({:invalid_contract, _})` | `:148` → `@error_statuses.invalid_contract` | same clause, same line | yes |
| `error_details({:stale, _})` | `:149` → `@error_statuses.stale` | same clause, same line | yes |
| `error_details(reason)`, key present | `:151-152` `Map.get` hit → entry | `Map.fetch` hit → entry, no emission | yes |
| `error_details(reason)`, key absent | `:151-152` `Map.get` default → `invalid_request` | `:error` branch → emit, then `invalid_request` | yes (tuple identical) |
| emission order vs status/JSON | n/a (no emission) | emission happens inside `error_details/1`, before `put_status`/`json` in the callers | n/a; no response interaction |
| `error/2`, `{:invalid_contract, _}` | `:116` → `error(conn, :invalid_contract)` | same line, byte-identical | yes |
| `error/2`, `{reason, fields}` when map | `:118-124` `put_status` → `json` with `goal_error_fields/1` | same lines, byte-identical | yes |
| `error/2`, scalar/other | `:126-129` `put_status` → `json` | same lines, byte-identical | yes |
| `validation_error/2` | `:136-146` 422 `validation_failed` | same lines, byte-identical | yes |

`protocol.ex` lines 1–147 are byte-identical between the base and the delivered file (blobs
`3283e6c9` → `d119987b`; the only differing region is the fallback clause, HEAD `:151-164`), so the
four `error/2`/`validation_error/2` coordinates above are the same at both revisions. A green
controller suite does not prove the 63 unmapped reasons still hit the same tuple. Since fix r1 the
127-reason base-vs-HEAD dump is that direct proof *and* provably exercises the table: its fail-loud
guard requires 64/64 `@error_statuses` keys to resolve to their source entries, and the recorded
artifact shows 64 mapped inputs hitting entries (63 with non-fallback tuples; `invalid_request` is
the 64th). The coverage test keeps the mapping true afterwards.

## Evidence plan (exact commands)

All gates foreground through a host-local scratch runner
(`<HOST-PATH>/.local/state/gdd/scratch/rf-w5-05-mix.sh`, pattern of `rf-w5-06-mix.sh`: portable
Elixir 1.20.4 / OTP 29.0.6 via `erl.exe`, `MIX_DEPS_PATH` at the main checkout's `deps/` —
`mix.lock` sha256 verified identical to this worktree — own `MIX_TEST_PARTITION=rf-w5-05`, local
PostgreSQL on 5432; no repository file is added for tooling).

| Gate | Command | Expected |
| --- | --- | --- |
| format | `mix format --check-formatted` | exit 0 |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0 |
| new tests | `mix test test/symmetry_control_web/protocol_reason_coverage_test.exs` | exit 0, all new cases |
| oracles | `mix test test/symmetry_control_web/controllers/protocol_controller_test.exs test/symmetry_control_web/controllers/goal_controller_test.exs` | exit 0, unchanged names |
| web tree | `mix test test/symmetry_control_web/` | exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host-path scan | `git diff origin/main` piped through the program rule's case-insensitive profile-directory grep | 0 matches |

Sequencing: the measurement/dump scripts and the focused test/mutation runs are never concurrent
with a full suite (shared PostgreSQL cap); full `mix test` is the conductor's gate-runner, not run
here.

Base-vs-HEAD dump (outside the repository, `<HOST-PATH>/.local/state/gdd/scratch/w5-05/fix1/`):
`dump.exs` — CRLF-safe input split plus the fail-loud guard that requires every parsed
`@error_statuses` key to resolve to its source entry — run on the base `e84973a7` bytes
(`base.txt`) and on the delivered bytes (`head.txt`), input the frozen 127-reason list, then
`cmp base.txt head.txt` → identical (`sha256 5aa77e8e…`). Emitted per reason: `error_details/1`
tuple, and `Protocol.error/2` status + `resp_body` bytes; the guard summary on stderr is
`inputs=127 mapped_inputs=64 unmapped_inputs=63 table_keys=64 table_hits=64`.

Mutation matrix (judging; each mutant applied in place, focused test run, file restored from a
scratch copy with sha256 re-verified):

| Mutation | Reddens |
| --- | --- |
| (a) add `{:error, :brand_new_reason}` return in a controller | scan set-equality assertion |
| (b) map one `@scanned_unmapped` reason in `@error_statuses` | that reason's `invalid_request`-tuple assertion |
| (c) delete the counter emission | counter test's `assert_receive` |
| (d) emit the counter for every reason (not just the fallback) | counter test's `refute_receive` for a mapped reason |
| (e2) emit inside the `{:invalid_contract, _}` clause (a counter outside the fallback branch) | tuple-clause test's `refute_receive` |
| (e4) emit a second time in `error/2`'s scalar clause (wire path) | the rendered-path `refute_receive` in the unmapped case |
| (e6) add an extra metadata key (`status: 400`) to the fallback emission | the whole-map `metadata`/`measurements` comparisons |

## Design trade-offs

- **`:telemetry` over `Logger`** — Decision I; `config/test.exs:27` sets `:warning`, so a Logger
  witness would need level juggling, and `:telemetry` is the local production pattern.
- **Pinned table over AST-derived table** — deriving `@error_statuses` from the source would avoid
  64 duplicated lines but would also make an accidental status/code/message edit invisible to the
  test; the contract freezes those bytes (D1) and the pin is the only in-tree guard that reddens
  on a table edit.
- **Scan in the test, not a committed manifest** — the scan is the discovery half (new reasons
  redden without a manifest edit) and costs ~10 lines; a manifest would be a second artifact to
  keep honest.
- **One test file, not two** — the counter and the inventory are the same contract (the fallback
  branch's behaviour); splitting would duplicate the probe-reason setup.
- **`Map.fetch` case over `Map.get_lazy/3`** — `Map.get_lazy` would be 2 lines shorter, but the
  emission is a named decision point that reads better as an explicit miss branch, and the
  `Map.fetch` form keeps "emit only when the table misses" visible at the clause level.

## Out of scope (with reasons)

- The default flip to `:internal_error` and any client-error allowlist — wire-changing, needs its
  own owner-approved slice (contract Change (3), CW-9 step 2).
- The controllers' own reason atoms (`goal_controller.ex`, `portal_api_controller.ex`, …) — a
  re-measured inventory gap is an escalation, not an edit; no gap exists at `e84973a7`.
- `portal_json.ex`'s `error_details/1` use and `GoalAttentionFeed.unavailable` — W6-09 terrain.
- Served-document gates (W1-13 merged), telemetry dependency re-adoption (W1-02 removed inert
  scaffolding only; `:telemetry` is reused).
- `mix.exs`/`mix.lock`, the two controller suites (oracles owned by W2-03/W3-03/W3-14).

## Concurrency note

`refactor/comment-policy-sweep` (in flight, unmerged) rewrites the `validation_error/2` comment at
`protocol.ex:131-135`; this slice's only hunk starts at `:151`. Different lines, same file: a
textual conflict is possible but line-disjoint, and the sweep's change is comment-only.
