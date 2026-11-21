# contracts-single-pass-decode plan — `DecodeSchema` validates the instance it decodes

Contract: `goal-rf-w1-09-contracts-decode.md` (GDD, 2026-11-21), the Go half of program-plan
§2 M11 / §4 W1-09, from finding F-08
(`<gdd-state>/refactor/findings/daemon-protocol-rest.md:361-372`) and the S2 entry
(`:632`). Baseline: this worktree's HEAD `a3039f0d17fc7c7905b63bdf247cac5bf339b213` — every
coordinate below was read at that revision.

## Pass inventory at the baseline (why 4-6 traversals)

`DecodeSchema` (`daemon/internal/contracts/schema.go:74-88`) runs three stages over the same
`data`: `decodeRawJSON`, `ValidateSchema`, `json.Unmarshal`. One call therefore performs:

| # | Pass | Carrier | Anchor | Disposition |
| --- | --- | --- | --- | --- |
| 1 | `utf8.Valid` | raw bytes | `schema.go:265` | keep (stdlib decode accepts invalid UTF-8) |
| 2 | `validateUnicodeEscapes` | raw bytes | `:268` | keep (stdlib decode accepts unpaired surrogates) |
| 3 | `validateJSONNumberLexemes` | raw bytes | `:275` | keep once |
| 4 | `validateJSONDuplicateKeys` | decoder tokens | `:278` | keep once |
| 5 | `decoder.Decode` into `any` | allocating parse | `:281-284` | keep once |
| 6 | `json.Unmarshal` into the typed DTO | allocating parse | `:84` | keep (contract keeps the typed unmarshal) |
| 7 | `schema.Validate(instance)` | decoded tree | `ValidateSchema:106` | keep |
| 8 | `validateCommitPaths(instance)` | decoded tree | `:112` | keep |
| 3' | `validateJSONNumberLexemes` (again, inside `ValidateSchema`'s own `decodeSchemaJSON`) | raw bytes | `:98` | **remove** |
| 4' | `validateJSONDuplicateKeys` (again) | decoder tokens | `:98` | **remove** |
| 5' | `decoder.Decode` into a discarded `any` (again) | allocating parse | `:98` | **remove** |

So a `DecodeSchema` call tunnels the bytes nine times, twice through an allocating `any` decode.
Passes 3/4/5' exist only because `ValidateSchema` cannot see the instance `DecodeSchema` already
decoded and drops unread at `contracts/schema.go:78`; `ValidateSchema` decodes the same bytes again
at `:98` and validates that instance.

## Deliverable shape

1. `daemon/internal/contracts/schema.go`:
   - new package-internal `validateInstance(envelope Envelope, instance any) error` — the body of
     today's `ValidateSchema` minus its decode (`ensureSchemas`, registry lookup, `schema.Validate`,
     the CommitPath switch, unchanged messages and order);
   - `DecodeSchema` decodes once (`decodeRawJSON`) and calls `validateInstance` on that instance;
   - `ValidateSchema` keeps its published raw-bytes semantics: `decodeSchemaJSON` (unchanged) +
     `validateInstance`. Public signatures and error strings are unchanged; error precedence is
     unchanged except for the corrupt embedded-bundle row in the verdict table below, where only
     `ValidateSchema`'s precedence changes;
   - `decodeSchemaJSON` increments a package-internal `decodeSchemaJSONCalls` counter (atomic) so a
     test can pin the single-pass property structurally (the traversal-count test below).
2. `daemon/internal/contracts/schema_test.go`:
   - the fixture reader splits into a non-`testing.T` loader (`readContractFixtureBytes`) so a
     benchmark can use it; `readContractFixture(t, …)` keeps its signature and its `t.Fatal`;
   - `BenchmarkDecodeSchema` over a representative valid-fixture table (envelope + generated DTO),
     `b.ReportAllocs()`, `b.SetBytes`, the payload read outside the timed loop, a fresh DTO per
     iteration — identical setup charging on the pre-change and post-change tree, because the
     benchmark only touches the exported `DecodeSchema`;
   - `BenchmarkDecodeSchemaBaselineComposition` runs the pre-change body
     (`decodeRawJSON` + `ValidateSchema` + `json.Unmarshal`) on the same table, so the removed
     traversal stays measurable against the delivered path inside the tree, in one run, instead of
     only across the stage's before/after logs. On the baseline revision the two sub-benchmarks
     measure the same body, which is itself the harness's fairness check;
   - `TestSchemaEntryPointsDecodePayloadOnce` (traversal-count test) asserts that one `DecodeSchema`
     call and one `ValidateSchema` call each perform exactly one payload decode, so a revert to the
     pre-refactor composition fails the suite instead of only moving the benchmark's allocation
     column;
   - no test deleted, renamed or weakened; no assertion changes.
3. `docs/plans/2026-11-21-contracts-single-pass-decode-plan.md` (this file; `docs/plans/` is the stage
   template's designated plan location, where the sibling slices' plans live — #127, #132).

No `docs/TODO.md` entry names this slice (`grep -n 'W1-09' docs/TODO.md` → no hit), so nothing moves
to `docs/archive/CLOSED-TODO.md`. `contracts/README.md:212-217` already describes the delivered
behaviour ("`DecodeSchema` adds strict raw JSON checks before decoding a generated DTO"), so no
in-repo document is made stale; nothing outside the two owned files changes.

## Public/API shape and exact edge-case semantics

`DecodeSchema(envelope, data, target) error` and `ValidateSchema(envelope, data) error` keep their
signatures, so every caller compiles untouched — 13 `DecodeSchema` call sites outside
`daemon/internal/contracts` (24 in-tree call sites at the baseline: 25 `git grep` lines minus the
definition) and the 2 out-of-package `ValidateSchema` call sites
(`daemon/internal/protocol/protocol.go:193`, `daemon/internal/protocol/goal_contract_fixture_test.go:135`).
Verdicts and messages, by input class:

| Input | Baseline | Delivered |
| --- | --- | --- |
| invalid UTF-8 | `DecodeSchema`: `decode <envelope> JSON: JSON input is not valid UTF-8`; `ValidateSchema`: accept | unchanged |
| unpaired surrogate | `DecodeSchema`: `decode <envelope> JSON: unpaired high surrogate in JSON string`; `ValidateSchema`: accept | unchanged (pinned by `TestDecodeSchemaRejectsUnpairedSurrogatesAfterSchemaValidation`) |
| duplicate member (incl. `\uXXXX` alias) | `decode <envelope> JSON: duplicate JSON object member "…"` | unchanged (detected by the single decode's token scan) |
| non-canonical number (`1.0`, `1e0`, `9007199254740992`) | `decode <envelope> JSON: fractional or exponent number is not canonical: …` / `integer exceeds the JSON safe-integer range: …` | unchanged |
| presentation read with fractional metadata | accepted; `json.Number` values preserved | unchanged |
| non-finite presentation number (`1e309`) | `decode <envelope> JSON: number is not finite: 1e309` | unchanged |
| trailing JSON value | `decode <envelope> JSON: multiple JSON values` | unchanged |
| unknown envelope, valid JSON | `unsupported Goal envelope schema "<envelope>"` | unchanged |
| schema-invalid instance | `validate <envelope> schema: …` (jsonschema keyword path) | unchanged (same instance shape: `json.Number` + `any`) |
| CommitPath violation | `validate <envelope> CommitPath: path does not match the schema pattern` | unchanged |
| schema-valid but typed-DTO-illegal payload | `decode <envelope> generated DTO: …` | unchanged; reached after one `any` decode instead of two |
| corrupt embedded bundle (unreachable in a released tree: `pnpm contracts:check` gates the bundle) | `ValidateSchema` reports the compile error before any decode error; `DecodeSchema` already reported the decode error first | both report the decode error first: `validateInstance` is the single `ensureSchemas` site and runs after the decode. Verdicts unchanged (both reject) |

Ordering is preserved because the surviving passes run in the same sequence (raw checks, then
`ensureSchemas`, then `schema.Validate`, then the CommitPath walk, then the typed unmarshal).
`ValidateSchema`'s deliberate asymmetry — no UTF-8 or surrogate checks — is preserved by leaving it
on `decodeSchemaJSON`.

## Evidence plan (all gates FOREGROUND; never a benchmark beside a suite on this host)

The contract asks for a before/after benchmark and clause 3 refuses a win that only moves work
between passes, so the measurement carries three independent witnesses: the timed pair, the
deterministic allocation column, and the pre-change fairness check. Commands, in the order they ran:

1. Pre-change (benchmark added first, `schema.go` untouched):
   `cd daemon && go test -run '^$' -bench 'BenchmarkDecodeSchema' -benchmem -benchtime=300ms -count=6 -timeout 900s ./internal/contracts/`
   → `bench-before.txt`. On that revision the entry-point benchmark and the composition benchmark
   run the same three-pass body, so this file is the harness's fairness check: the two identical
   bodies must allocate the same (they do: index-paired runs agree to ≤9 allocations/op, any run
   pair to ≤14) and their ns/op should agree.
2. Post-change: the identical command on the delivered tree → `bench-after.txt`.
3. Paired runs, 8 each per fixture, because cross-run drift on this host is larger than the effect:
   both bodies in one process (`-bench '^BenchmarkDecodeSchema(BaselineComposition)?$/<fixture>$'`)
   plus a second campaign with one body per process (order bias removed) — `bench-pairs.txt`,
   `bench-separate.txt`, `-benchmem -benchtime=400ms -count=8`.
4. Deterministic witness: allocations/op and B/op, stable across the runs of a given body within a
   campaign but not bit-identical — largest observed spreads across the three six-run campaigns
   (the two saved ones and the apply-stage re-run): 19 allocations/op and 8.8 kB/op, both on
   `goal-read` composition. Settled samples only: a process's first calibration sample prints
   `b.N=1` and absorbs the one-time `ensureSchemas` compile (≈840 k allocations on `goal-read`).
   The removed second decode is the delta between the two columns (≈2 980 allocations/op on
   `goal-read`, ≈150× the largest observed spread).
5. Contract gates, serially after the benchmark runs:
   - `cd daemon && gofmt -l internal/contracts` → empty, exit 0
   - `cd daemon && go vet ./...` → exit 0
   - `cd daemon && GOOS=linux go build ./...` and `GOOS=windows go build ./...` → exit 0 each
   - `cd daemon && go test -count=1 -timeout 600s ./internal/contracts/` → exit 0
   - `cd daemon && go test -count=1 -timeout 600s ./internal/protocol/...` → exit 0
   - `git diff --check` → exit 0
6. CI-only oracle (contract): the daemon race job (`go test -race -count=1 ./...` through
   `scripts/ci/stdio-only-caller-boundary.py`) — not runnable on this host (no cgo); the conductor
   runs it on the PR.
7. Clause-3 mutation check in a throwaway copy outside the worktree (deleted afterwards):
   M1 drops `schema.Validate` from `validateInstance` → the schema-dependent cases go red while the
   raw-scan pins (duplicate member, non-canonical number, unpaired surrogate) stay green;
   M2 drops the CommitPath walk → `TestCommitPathUsesBoundedECMAScriptValidation` goes red while the
   rest of the package stays green. Both reds recorded with their exact failure lines.

## Design trade-offs

1. **Share the instance rather than fuse the token scan into the decode.** A token-driven
   `json.Decoder` walk that builds the `any` and detects duplicate members in one pass would remove
   one more raw traversal, but it replaces the library's decode with ~80 lines of hand-rolled value
   construction whose constant factor can easily exceed the traversal it saves; a win there would
   have to be shown, not assumed, and clause 3's failure control explicitly refuses "work moved
   between passes" as achievement. Rejected for this increment.
2. **The number-lexeme policy stays on raw bytes.** It could be re-expressed over the decoded
   instance's `json.Number` values (deleting pass 3), but that rewrites a strictness check on a
   different carrier — exactly the strictness-parity work clause 4 assigns to W2-12 — and would move
   error precedence after decode. Rejected.
3. **`ValidateSchema` keeps its own decode.** Routing it through `decodeRawJSON` would align both
   entry points but also start refusing invalid UTF-8 and unpaired surrogates there, which
   `TestDecodeSchemaRejectsUnpairedSurrogatesAfterSchemaValidation` and the fixture oracle depend on.
   Rejected as a strictness change.
4. **The typed `json.Unmarshal` stays.** The contract's change sentence keeps it ("unmarshal the
   typed DTO from the same bytes"); deriving the DTO from the `any` instance needs a marshal
   round-trip, i.e. more work and a second wire encoding. Rejected.
5. **LOC**: the program plan estimated −30…−60 LOC for this slice; the measured delta against the
   baseline is `schema.go` +24/−6 (net +18, `git diff --numstat`) and `schema_test.go` +116/−6
   (net +110: the benchmark harness and the traversal-count test). The estimate assumed the
   duplicate path was deletable code; it was *shared* code (`decodeSchemaJSON`, still the
   raw-bytes entry point `ValidateSchema` needs), so the change buys a pass per envelope instead of
   a deletion, and the new benchmark is the receipt. Recorded as a deviation from the plan's
   estimate, not as a contract miss: clause 1 measures the traversal.
6. **The benchmark measures the real entry point, not a synthetic harness.** Its setup charging is
   identical on both trees (fixture read outside the loop, DTO allocated inside), so the delta
   isolates the removed traversal. It stays in `schema_test.go` as this slice's oracle artifact.

## Out of scope (contract clause 4)

- Strictness-parity changes (W2-12) and the Elixir single-pass decoder (W2-02): no verdict, message
  or accepted-input set changes here.
- `daemon/internal/protocol/goal_subject.go` validators, `goal_validation.go`'s `decodeStrictObject`,
  and protocol's own envelope decoding (`goal_schema.go:18-95`): other files, other slices.
- `protocol.RuntimeCapabilities.Validate()` re-validating its marshalled copy
  (`protocol.go:193`, `:210`) and the control client's own re-decodes (`control/client.go:1147-1152`):
  different carriers on purpose, not the same-bytes duplication F-08 names.
- The pre-change composition as a *permanent alternate implementation*: the baseline numbers come
  from re-running the identical benchmark on the pre-change revision. Only the comparison
  sub-benchmark (`BenchmarkDecodeSchemaBaselineComposition`) stays in the tree — the historical
  `schema.go` bytes do not.

## Residual risks

- **This host cannot resolve the effect as a per-fixture time claim.** The pre-change fairness check
  (two byte-identical bodies, one process, 6 runs each) still diverged in ns/op: per-fixture
  minimum ratios 0.42×–1.45× and 0.14×–6.58× across every cross-body run pair (recomputed from the
  saved `bench-before.txt`), while index-paired allocations matched to ≤9 allocations/op. Of the
  three timing campaigns, the same-process pair favored the
  delivered path 6/6 (0.685×–0.885× of the composition minima), the repeat favored it 5/6
  (`goal-command` 1.196×) and the one-body-per-process campaign 4/6 (`goal-read` 1.136×,
  `workspace-read` 1.004×), with aggregate minima 0.727×, 0.715× and 0.873×. Individual fixtures
  therefore flip sign between campaigns and no per-fixture speed claim is made; the load-bearing
  witness is the deterministic allocation column — one `any` decode removed per envelope, 31560
  vs 38414 allocations/op and 1.64 MB vs 2.01 MB per corpus sweep (`goal-read` only: 12553→9558
  allocs, 717178→536074 B/op). A CI runner with a quieter timer should re-measure before any timing
  statement is published.
- The removed pass was the *second* decode; a future change that re-adds validation over raw bytes
  now fails `TestSchemaEntryPointsDecodePayloadOnce` (exactly one payload decode per entry-point
  call, counted by `decodeSchemaJSONCalls`), and the in-tree benchmark keeps the cost measurable in
  the allocation column.
