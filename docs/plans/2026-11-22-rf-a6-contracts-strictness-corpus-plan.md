# rf-a6-contracts-strictness-corpus plan — raw-byte fixtures in the shared manifest + one Go control-response decode entry

Goal contract: `goal-rf-a6-contracts-strictness-corpus.md` (owner ruling 2026-11-22, L2=b,
superseding arch-decision-2 R12's HOLD). Base revision `333c572c` (`refactor/rf-a6-contracts-strictness-corpus`).
This plan records only the choices the accepted plan left unresolved: the manifest row shape, the
per-runtime expectations, the Go entry's exact levels, and the evidence plan. It does not reopen the
decided design.

## 1. Dispatch gates and re-derived citations

- #178 (W2-12b) and #140 (W1-09) are merged; the tree already carries their result. `daemon/internal/control/client.go`
  is 1771 lines, `validation.go` 1881; the contract's citations (`client.go:1101/1115/1311/1240`, `validation.go:655-698`)
  are pre-#178 and are re-derived here as: `decodeStrictJSON` `validation.go:522` (now replaced by the entry),
  `contracts.DecodeSchema` call sites `client.go:1044` (was 1043 before this slice) and `client.go:1273`,
  `decodeJSON` `client.go:1160` (replaced), `decodeStrictObjectJSON` `validation.go:519` with 18 production callers.
- `contracts/fixtures/manifest.json` holds 248 rows before the slice (`manifest_version` 1) and 261 after
  (13 raw rows). Row members in use: `id, schema, path, valid, tags` plus optional `schema_valid, semantic_valid,
  semantic_error`; this slice adds `wire_error`, `wire_levels`, `runtime_divergence`.
- `pnpm contracts:check` = typecheck + `node --test contracts/scripts/*.test.mjs` + `check.mjs` + `check-effect.mjs`
  (`package.json:8-10`); the Go reader runs in the daemon jobs; the Elixir reader in `mix test` (`ci.yml:327`).

## 2. Corpus design (manifest row shape)

New directory `contracts/fixtures/raw/`, marked `-text` in `.gitattributes` so the bytes judged are the bytes
committed. 13 rows, one file each, every file schema-valid apart from the raw property (the only exception is
the unsafe-integer row, where the raw check runs before schema validation by construction). Wrapping schemas
are chosen so the class's rule is the one that fires: canonical command envelopes (`goal-command`) for lexeme,
duplicate, encoding and trailing classes; `evidence-batch` for the nested duplicate so the Elixir reader's
machine-request strict decoder is exercised; `workspace-read` for the two non-finite rows, where the
presentation number policy is the one that judges finiteness.

| row id | class (`wire_error`) | bytes | Go levels rejecting (`wire_levels`) |
|---|---|---|---|
| `goal-command.raw-duplicate-member-top-level` | `duplicate_json_member` | duplicate `expected_revision` member | legacy, typed, schema |
| `evidence-batch.raw-duplicate-member-nested` | `duplicate_json_member` | duplicate `evidence_key` inside `items[0]` | legacy, typed, schema |
| `goal-command.raw-number-fractional` | `noncanonical_number` | `1.0` | schema |
| `goal-command.raw-number-exponent` | `noncanonical_number` | `1e0` | schema |
| `goal-command.raw-number-negative-zero` | `negative_zero` | `-0` | (none) |
| `goal-command.raw-number-leading-zero` | `leading_zero` | `01` | legacy, typed, schema |
| `goal-command.raw-number-unsafe-integer` | `unsafe_integer` | `9007199254740992` | schema |
| `workspace-read.raw-number-nonfinite` | `nonfinite_number` | `1e309` in additive metadata | legacy, typed, schema |
| `workspace-read.raw-number-nonfinite-negative` | `nonfinite_number` | `-1e309` in additive metadata | legacy, typed, schema |
| `goal-command.raw-invalid-utf8` | `invalid_utf8` | `0xff` byte inside a LongText reason | schema |
| `goal-command.raw-bom-prefix` | `bom` | `ef bb bf` prefix | legacy, typed, schema |
| `goal-command.raw-trailing-second-value` | `trailing_value` | `{}` after the document | legacy, typed, schema |
| `goal-command.raw-trailing-garbage` | `trailing_garbage` | ` trailing` after the document | legacy, typed, schema |

Row members added by this slice:

- `wire_error`: the corpus class the bytes violate. Shared vocabulary, not a native code; each reader maps it
  to its own rejection, and the evidence record carries the native text per runtime.
- `wire_levels`: the Go control-response decode levels that reject the bytes today, measured against
  `map[string]any` as the decode target (the level's checks are target-independent; a typed DTO can add its
  own rejections after the entry, which is not the entry's business).
- `runtime_divergence`: `{"<runtime>": "accepts"}` for a runtime that accepts the bytes today. Default is
  rejection; the divergence is a measured fact, not an expectation to change. `valid` stays `false` for every
  raw row (the row documents a banned wire form); `schema_valid`/`semantic_valid` stay the TS policy's answer
  and are only set where TS accepts.

Escape-based classes (duplicate via an escaped key alias, unpaired surrogate escape) are deliberately not
authored as new files: the program-wide DLP rule forbids writing the two-byte escape in committed content.
They stay covered by `contracts/fixtures/invalid/goal-command.unpaired-surrogate.json` and the inline tests
(`daemon/internal/contracts/schema_test.go`, `contracts/scripts/boundary.test.mjs`); the evidence record lists
which existing test covers each.

## 3. Runtime adapter shape

- TS `check.mjs`: `readFixtureJson` takes the row's `wire_error`; a `WireJsonError` for a raw row becomes an
  issue carrying the native code. A raw row whose class is in the code map must reject with that code;
  a raw row that declares `runtime_divergence.ts == "accepts"` must parse, and a row that parses without
  declaring the divergence fails the gate. `raw/` joins the exactly-one-row directory gate, `raw-bytes`
  joins the required tags, and a `wire_error` row must live under `raw/`.
- TS `check-effect.mjs`: same code map; for every raw row the Effect `decodeJson` outcome must equal the
  declared TS acceptance, and a rejected row's `WireJsonError` code must equal the mapped code.
- Go `daemon/internal/protocol/goal_contract_fixture_test.go`: a row with `wire_error` decodes through
  `contracts.DecodeSchema` (not `ValidateSchema`, which by design skips the UTF-8 check); it must be rejected
  with the class's text, or accepted when `runtime_divergence.go == "accepts"`.
- Go `daemon/internal/control/decode_test.go` (new): one subtest per raw row runs the row through all three
  entry levels and asserts the `wire_levels` membership plus the schema-level class text; six routing subtests
  pin that each decode path increments the entry's call counter exactly once.
- Elixir `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs`: a `wire_error` row is read
  with `SymmetryControlWeb.StrictJSONDecoder.decode!/1` when its schema is a machine-request document
  (`evidence`, `evidence-batch`) and with `Jason.decode/1` otherwise; the decoded document still runs through
  the schema's validator, and acceptance must equal `runtime_divergence.elixir == "accepts"`.

## 4. Go decode entry

One function in `daemon/internal/control`, `decodeResponseJSON(level, envelope, value, target)`, in
`client.go`; the level is explicit and each caller keeps the strictness it had:

| level | checks | former code | call sites |
|---|---|---|---|
| `responseDecodeLegacy` | duplicate members (filtered to the duplicate message), permissive typed decode, single top-level value | `decodeJSON` | `transport.requestWithStatus` (2 calls), `decodeAPIError`, `decodeProviderActionObjectJSON` |
| `responseDecodeTyped` | `DisallowUnknownFields`, single top-level value, duplicate members post-parse | `decodeStrictJSON` | `requestGoalWithStatus`, `decodeStrictObjectJSON` (18 callers) |
| `responseDecodeSchema` | all of `contracts.DecodeSchema`: UTF-8, escaped surrogates, number lexemes for the envelope class, duplicate members, v1 schema, DTO unmarshal | `contracts.DecodeSchema` | `requestGoalContractWithStatus`, `decodeEvidenceBatchConflictDetails` |

`envelope` is read only at `responseDecodeSchema`. The outer `responseErrorf` prefixes are untouched
(byte-compared base vs head in the evidence). `decodeJSON`, `decodeJSONUnchecked` and `decodeStrictJSON` are
deleted; `client_test.go`'s `TestDecodeJSONRejectsDuplicateMembers` keeps its name and its three pinned
outcomes and now calls the legacy level. A counter (`responseDecodeCalls`) makes the routing observable; a
path that stops calling the entry fails its routing subtest (the red mutant).

The entry's existence does not change acceptance: unifying the legacy and typed callers on the schema level is
out of scope (owner ruling) and is recorded in `docs/TODO.md` with the rows that would change.

## 5. Measurements taken before the change

- Base corpus run (real paths at `333c572c`: `decodeJSON`, `decodeStrictJSON`, `contracts.DecodeSchema`) and
  head corpus run (the entry at the three levels), 13 rows × 3 levels, identical accept/reject tables; the
  table is in the evidence record. Consequence: the enumerated accepted→rejected list is empty.
- TS: `parseWireJson`/`parsePresentationJson` outcomes per row (`check.mjs` path) and the Effect `decodeJson`
  outcomes (`check-effect.mjs` path); TS accepts the BOM (the JS `JSON.parse` strips it) and `-0`.
- Go: `ValidateSchema` accepts the invalid-UTF-8 row while `DecodeSchema` rejects it, which is why the raw Go
  reader uses `DecodeSchema`; `-0` in `expected_version` is accepted at all levels (in `expected_revision` it
  is rejected by the schema's `minimum`, which would make the row a schema fixture rather than a raw one).
- Elixir: measured with `mix run --no-start` over the same bytes (Jason vs the strict decoder, then each
  schema's validator); the outcomes fill the rows' `runtime_divergence`.

## 6. Edge-case semantics the contract demands

- Every raw file is schema-valid apart from the raw property; the `-0` row uses `expected_version` (value 0)
  rather than `expected_revision` (minimum 1) for exactly that reason.
- The non-finite rows use the presentation policy (a read projection) because a canonical envelope classifies
  `1e309` as a noncanonical lexeme instead; the class then names what actually fires per runtime.
- `trailing_value` (a second JSON value) and `trailing_garbage` (a non-JSON tail) are separate classes because
  the decoders report different native errors.
- No committed file this slice writes contains the two-byte escape the DLP rule forbids; the raw corpus
  deliberately excludes escape-based classes.
- Existing fixture bytes and rows are untouched; the manifest gains rows only.

## 7. Evidence plan

1. `pnpm contracts:check` (root): typecheck, `node --test`, `check.mjs`, `check-effect.mjs`; manifest row count
   248 → 261.
2. `daemon/`: `gofmt -l`, `go vet ./...`, `GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
   `go test -count=1 ./internal/protocol/... ./internal/control/... ./internal/contracts/...`; the same
   packages via WSL with `-race` (`wsl -d Ubuntu-24.04 -u accept`). The contract names
   `/mnt/q/repos/symmetry/daemon`, which is the main clone; the Linux lane therefore uses this worktree's
   path, `/mnt/q/repos/wt-a6-contracts-strictness/symmetry/daemon`, and the evidence record says so.
3. Elixir via the portable-toolchain recipe (`<HOST-PATH>/.codex/tools/symmetry/elixir-1.20.4` +
   `erlang-29.0.6`, `MIX_TEST_PARTITION=rf-a6`, deps from the main checkout, `+S 2`): the fixture-manifest
   test and the strict-decoder plug test.
4. Base-vs-head: corpus accept/reject tables, outer error-prefix byte compare, control-flow audit of each
   rerouted call site (early returns, error wraps), `git check-attr -a contracts/fixtures/raw/*`,
   `git hash-object` with and without filters (no normalization), `git diff --check`,
   `scripts/ci/check-citation-existence.py` over a synthetic snapshot commit built from the final tree.
5. Mutants: bypass the entry inside one path (routing subtest reddens); swap the Elixir decoder for Jason on
   the nested-duplicate row (its subtest reddens); flip one `wire_levels` entry. Each reverted hash-verified.

## 8. Out of scope (with reasons)

- Unifying the Go levels (owner ruling: owner sign-off required; recorded in `docs/TODO.md`).
- Elixir/TS strictness alignment (only recorded as divergences).
- Machine-endpoint request schemas vs fence-stripped bodies (K4/C05, W5-17), third-party native stream
  decoders, `contracts/ts/wire-json.ts` and `daemon/internal/contracts/schema.go` internals (not owned).
- `contracts/v1/**`, generated code, Elixir plugs/production code, harness decoders (not owned).

## 9. Residual risks and known limits

- `wire_levels` is defined against a `map[string]any` decode target; a production typed DTO can add rejections
  after the entry (documented, not asserted).
- The BOM and `-0` rows are accepted by at least one runtime, so they pin today's leniency rather than a
  rejection; the TODO entry names them.
- The Elixir reader asserts acceptance end to end (decode plus the schema's validator), so a post-decode
  rejection and a raw rejection are not distinguished in the row; the evidence record separates them.
- The `-race` pass runs in WSL against this worktree's Linux path (`/mnt/q/repos/wt-a6-contracts-strictness/symmetry/daemon`);
  the contract's literal `/mnt/q/repos/symmetry/daemon` is the main clone and is not tested.
