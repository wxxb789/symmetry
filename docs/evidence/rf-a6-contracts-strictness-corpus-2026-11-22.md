# rf-a6-contracts-strictness-corpus — evidence

Goal contract: `goal-rf-a6-contracts-strictness-corpus.md` (owner ruling 2026-11-22, L2=b: one Go
control-response decode entry with per-path strictness preserved; the tightening is deferred to
`docs/TODO.md`). Base revision `333c572c`. Plan:
`docs/plans/2026-11-22-rf-a6-contracts-strictness-corpus-plan.md`.

## 1. Raw corpus rows and the bytes under test

Files: `contracts/fixtures/raw/*.json`, 13 files, marked `-text` in `.gitattributes` (checked in §5).
Row members: `wire_error` (class), `wire_levels` (Go levels that reject today), `runtime_divergence`
(runtimes that accept today). Manifest row count 248 → 261.

| row id | class | bytes (hex where not printable) | TS `parseWireJson`/`parsePresentationJson` | Effect `decodeJson` | Go `DecodeSchema` |
|---|---|---|---|---|---|
| `goal-command.raw-duplicate-member-top-level` | `duplicate_json_member` | `"expected_revision": 1,` twice | `duplicate_json_member` | `duplicate_json_member` | `duplicate JSON object member "expected_revision"` |
| `evidence-batch.raw-duplicate-member-nested` | `duplicate_json_member` | `"evidence_key"` twice inside `items[0]` | `duplicate_json_member` | `duplicate_json_member` | `duplicate JSON object member "evidence_key"` |
| `goal-command.raw-number-fractional` | `noncanonical_number` | `1.0` | `noncanonical_number` | `noncanonical_number` | `fractional or exponent number is not canonical: 1.0` |
| `goal-command.raw-number-exponent` | `noncanonical_number` | `1e0` | `noncanonical_number` | `noncanonical_number` | `fractional or exponent number is not canonical: 1e0` |
| `goal-command.raw-number-negative-zero` | `negative_zero` | `-0` in `expected_version` | accepts | accepts | accepts (divergence) |
| `goal-command.raw-number-leading-zero` | `leading_zero` | `01` | `invalid_json` | `invalid_json` | `invalid character '1' after object key:value pair` |
| `goal-command.raw-number-unsafe-integer` | `unsafe_integer` | `9007199254740992` | `noncanonical_number` | `noncanonical_number` | `integer exceeds the JSON safe-integer range: 9007199254740992` |
| `workspace-read.raw-number-nonfinite` | `nonfinite_number` | `1e309` in additive metadata | `nonfinite_number` | `nonfinite_number` | `number is not finite: 1e309` |
| `workspace-read.raw-number-nonfinite-negative` | `nonfinite_number` | `-1e309` in additive metadata | `nonfinite_number` | `nonfinite_number` | `number is not finite: -1e309` |
| `goal-command.raw-invalid-utf8` | `invalid_utf8` | `0xff` inside the `pause` reason string | `invalid_utf8` | `invalid_utf8` | `JSON input is not valid UTF-8` |
| `goal-command.raw-bom-prefix` | `bom` | `ef bb bf` before the document | accepts (divergence) | accepts | `invalid character` (the BOM bytes) |
| `goal-command.raw-trailing-second-value` | `trailing_value` | `{}` after the document | `invalid_json` | `invalid_json` | `multiple JSON values` |
| `goal-command.raw-trailing-garbage` | `trailing_garbage` | ` trailing` after the document | `invalid_json` | `invalid_json` | `invalid character 'a' in literal true (expecting 'u')` |

Commands: `node contracts/scripts/zz-measure.mjs` (TS `parseWireJson`/`parsePresentationJson`) and
`node contracts/scripts/zz-measure3.mjs` (Effect `GoalCommand`/`EvidenceBatch`/`WorkspaceRead`
`decodeJson` + `Schema.is`), both run from the worktree root; the scripts and their TSV outputs are
archived at `<HOST-PATH>/.local/state/gdd/scratch/rf-a6/` (removed from the tree before the final
run). Go texts from the scratch program that ran `contracts.DecodeSchema` per row (§4 has the full
base-vs-head tables).

Escape-based classes were not authored (program-wide DLP rule); coverage stays: duplicate via an
escaped key alias — `contracts/scripts/boundary.test.mjs` and `daemon/internal/contracts/schema_test.go`
each reject a `duplicate_json_member` whose member name is written with a `ratio` alias escape;
unpaired surrogate escape — `contracts/fixtures/invalid/goal-command.unpaired-surrogate.json`,
plus the `unpaired_surrogate` cases in `boundary.test.mjs` and `schema_test.go`.

## 2. Divergence table (recorded in the rows and `docs/TODO.md`, not fixed)

| row | runtime | today's outcome | cause |
|---|---|---|---|
| `goal-command.raw-duplicate-member-top-level` | Elixir | accepts | `Jason` keeps the last member; `validate_goal_command/2` then sees the valid document |
| `goal-command.raw-number-fractional` | Elixir | accepts | `Jason` decodes `1.0` as a float and `validate_goal_command/2` accepts the value |
| `goal-command.raw-number-exponent` | Elixir | accepts | `Jason` decodes `1e0` as a float and `validate_goal_command/2` accepts the value |
| `goal-command.raw-bom-prefix` | JS | accepts | `JSON.parse` strips a leading BOM |
| `goal-command.raw-number-negative-zero` | Go, JS, Elixir | accepts | no runtime enforces the canonical `-0` to `0` form |

No row adds a rejection in Elixir or JS; every reader asserts the table above.

Elixir outcomes were measured with `mix run --no-start` over the same bytes (strict decoder for the
machine-request schema, `Jason.decode/1` otherwise, then each schema's validator): 9 rows reject
(`DuplicateJSONKeyError` for the nested duplicate; `Jason.DecodeError` for the BOM, invalid UTF-8,
leading zero, trailing value/garbage and both non-finite presentation numbers; a post-decode
`oneOf` failure for the unsafe integer), and the four rows above accept.

## 3. Go path routing table

Single entry: `decodeResponseJSON(level, envelope, value, target)` (`daemon/internal/control/client.go`).

| path (pre-slice) | level | checks | post-slice call |
|---|---|---|---|
| `requestGoalWithStatus` (`decodeStrictJSON`) | typed | unknown fields, single value, duplicate members | `decodeResponseJSON(responseDecodeTyped, "", …)` |
| `requestGoalContractWithStatus` (`contracts.DecodeSchema`) | schema | raw checks + v1 schema + DTO | `decodeResponseJSON(responseDecodeSchema, envelope, …)` |
| `decodeEvidenceBatchConflictDetails` (`contracts.DecodeSchema`) | schema | same | `decodeResponseJSON(responseDecodeSchema, EnvelopeEvidenceBatchConflictDetails, …)` |
| `transport.requestWithStatus` (`decodeJSON`, 2 calls) | legacy | duplicate members, permissive decode | `decodeResponseJSON(responseDecodeLegacy, "", …)` |
| `decodeAPIError` (`decodeJSON`) | legacy | same | `decodeResponseJSON(responseDecodeLegacy, "", …)` |
| `decodeProviderActionObjectJSON` (`decodeJSONUnchecked` after its own duplicate gate) | legacy | duplicate members, permissive decode | `decodeResponseJSON(responseDecodeLegacy, "", …)` |
| `decodeStrictObjectJSON` (18 callers, `decodeStrictJSON`) | typed | unknown fields, single value, duplicate members | `decodeResponseJSON(responseDecodeTyped, "", …)` |

Deleted: `decodeJSON`, `decodeJSONUnchecked`, `decodeStrictJSON`. `rejectDuplicateJSONMembersOnly`
remains (provider-action alias comparison). Control-flow audit: every removed body's checks are the
entry's per-level bodies verbatim (single-value message, duplicate-after-parse precedence, legacy
duplicate filter); no early return or error wrap changed; the outer `responseErrorf` prefixes are
byte-compared in §5.

Routing proof: `TestResponseDecodeRoutesEveryPathThroughTheEntry`
(`daemon/internal/control/decode_test.go`) asserts the entry counter grows by exactly 1 for
`requestGoalWithStatus`, `requestGoalContractWithStatus`, `requestWithStatus`,
`decodeStrictObjectJSON`, `decodeProviderActionObjectJSON` and `decodeAPIError`.

## 4. Base-vs-head corpus enumeration (Go decode paths)

Both revisions run the same 13 rows through their real paths with `map[string]any` targets:
base `decodeJSON` / `decodeStrictJSON` / `contracts.DecodeSchema` (the probe `TestZZA6CorpusProbe`
in a detached worktree at `333c572c`, `Q:/repos/tmp-a6-base`, driving the real base functions), head
the entry at the three levels (the same probe shape in this worktree). Re-run at the final tree on
2026-11-22; 39 lines, `diff` empty:

```
$ diff <HOST-PATH>/.local/state/gdd/scratch/rf-a6/{base,head}-corpus.tsv && echo IDENTICAL
IDENTICAL
```

Accepted→rejected and rejected→accepted changes: none. The contract's enumerated tightening list is
therefore empty; the rows that *would* change acceptance under unification are listed in
`docs/TODO.md` (`goal-command.raw-number-fractional`, `…-exponent`, `…-unsafe-integer`,
`…-invalid-utf8`: accepted by the legacy and typed levels, rejected by the schema level).

Level tags (`wire_levels`) are read from the same table, e.g.
`goal-command.raw-invalid-utf8` `["schema"]`, `…-negative-zero` `[]`,
`…-duplicate-member-top-level` `["legacy", "typed", "schema"]`.

Sign-off reference: the goal contract's "Owner ruling (2026-11-22, L2 = b)" — HOLD lifted, one entry
with each call site keeping its current strictness, no currently-accepted input rejected, and the
unified-stricter-level tightening recorded as one `docs/TODO.md` entry. The enumerated accepted to
rejected list for this slice is empty by that ruling; the rows that would change under the deferred
tightening are named in the TODO entry, not here.

## 5. Byte-level checks

`git check-attr -a contracts/fixtures/raw/*` (13 files, 26 lines, full output):

```
contracts/fixtures/raw/evidence-batch.duplicate-member-nested.json: eol: lf
contracts/fixtures/raw/evidence-batch.duplicate-member-nested.json: text: unset
contracts/fixtures/raw/goal-command.bom-prefix.json: eol: lf
contracts/fixtures/raw/goal-command.bom-prefix.json: text: unset
contracts/fixtures/raw/goal-command.duplicate-member-top-level.json: eol: lf
contracts/fixtures/raw/goal-command.duplicate-member-top-level.json: text: unset
contracts/fixtures/raw/goal-command.invalid-utf8.json: eol: lf
contracts/fixtures/raw/goal-command.invalid-utf8.json: text: unset
contracts/fixtures/raw/goal-command.number-exponent.json: eol: lf
contracts/fixtures/raw/goal-command.number-exponent.json: text: unset
contracts/fixtures/raw/goal-command.number-fractional.json: eol: lf
contracts/fixtures/raw/goal-command.number-fractional.json: text: unset
contracts/fixtures/raw/goal-command.number-leading-zero.json: eol: lf
contracts/fixtures/raw/goal-command.number-leading-zero.json: text: unset
contracts/fixtures/raw/goal-command.number-negative-zero.json: eol: lf
contracts/fixtures/raw/goal-command.number-negative-zero.json: text: unset
contracts/fixtures/raw/goal-command.number-unsafe-integer.json: eol: lf
contracts/fixtures/raw/goal-command.number-unsafe-integer.json: text: unset
contracts/fixtures/raw/goal-command.trailing-garbage.json: eol: lf
contracts/fixtures/raw/goal-command.trailing-garbage.json: text: unset
contracts/fixtures/raw/goal-command.trailing-second-value.json: eol: lf
contracts/fixtures/raw/goal-command.trailing-second-value.json: text: unset
contracts/fixtures/raw/workspace-read.number-nonfinite-negative.json: eol: lf
contracts/fixtures/raw/workspace-read.number-nonfinite-negative.json: text: unset
contracts/fixtures/raw/workspace-read.number-nonfinite.json: eol: lf
contracts/fixtures/raw/workspace-read.number-nonfinite.json: text: unset
```

No-normalization proof: `git hash-object --no-filters <f>` equals `git hash-object --path=<f> <f>`
(same blob id; the 13 ids are in the answer artifact). Existing fixtures and rows are untouched:
`git diff contracts/fixtures/valid contracts/fixtures/invalid` empty;
`git diff --numstat contracts/fixtures/manifest.json` = `126 0`.

Outer error-prefix byte compare: `responseErrorf("…%w")` literals extracted from `client.go` at
`333c572c` and at the final tree, `diff` empty:

```
responseErrorf("decode evidence batch conflict details: %w"
responseErrorf("decode response: %w"
responseErrorf("decode response: %w"
responseErrorf("decode strict goal response: %w"
responseErrorf("decode strict goal response: %w"
responseErrorf("decode strict provider action response: %w"
```

## 6. Gate results

All commands ran foreground and serialized at the final tree; no measurement harness ran beside the
suite. `pnpm` resolves through `mise x pnpm@12.17.0` (the workspace pins 12.17.0; the host's `latest`
shim is 12.21.0). Every row below was re-run after the last executable edit. The citation gate ran
over synthetic snapshots built from the tree with `GIT_INDEX_FILE` + `git commit-tree` (real refs
untouched) at several successive passes; each pass compared all 27 changed/added paths blob by blob
(`git hash-object --path`) against the working tree and found at most this record's own closing
lines as the difference, and the gate exited 0 on every pass (the pass ids are in the run's answer
artifact).

| Gate | Command (cwd) | Exit | Output |
|---|---|---:|---|
| contracts:check | `mise x pnpm@12.17.0 -- pnpm --trust-lockfile --config.verify-deps-before-run=false contracts:check` (repo root) | 0 | typecheck pass; `node --test` 35/35 (0 fail); `Generated contract DTOs are up to date.`; `Validated 21 Draft 7 schemas and 261 fixtures.`; `Effect decoded all 261 raw fixtures; 246 parsed documents reached Schema validation` |
| gofmt | `gofmt -l .` (`daemon/`) | 0 | no output |
| vet | `go vet ./...` (`daemon/`) | 0 | no output |
| Linux build | `GOOS=linux go build ./...` (`daemon/`) | 0 | no output |
| Windows build | `GOOS=windows go build ./...` (`daemon/`) | 0 | no output |
| Go tests | `go test -count=1 ./internal/protocol/... ./internal/control/... ./internal/contracts/...` (`daemon/`) | 0 | `ok` ×3 (2.806s / 1.232s / 1.824s at the settled tree); `-v` run over the same sources: RUN=1100, top-level PASS=164, FAIL=0 |
| Go tests, WSL `-race` | `wsl -d Ubuntu-24.04 -u accept bash -c 'cd /mnt/q/repos/wt-a6-contracts-strictness/symmetry/daemon && go test -count=1 -race -timeout 25m ./internal/protocol/... ./internal/control/... ./internal/contracts/...'` | 0 | `ok` ×3 (10.545s / 10.221s / 5.164s), no `DATA RACE` |
| Elixir | `sh <HOST-PATH>/.local/state/gdd/scratch/rf-a6-mix.sh test test/symmetry_control/goals/contract_fixture_manifest_test.exs test/symmetry_control_web/plugs/strict_evidence_json_test.exs` (portable Elixir 1.20.4 / OTP 29.0.6, `MIX_TEST_PARTITION=rf-a6`, foreground, no concurrent mix run) | 0 | `Result: 25 passed`; re-run at the settled tree: 25 passed (2.4s) |
| citation existence | `python scripts/ci/check-citation-existence.py --repo . --base <snap>^ --head <snap>` over synthetic snapshots built with `GIT_INDEX_FILE` + `git commit-tree` (real refs untouched), last `9c9ce10c` | 0 | `no audited document added or modified between 333c572c1cd4 and 9c9ce10c4515; nothing to check` (same result for the earlier passes `b890ce6c`, `be12078f`) |
| whitespace | `git diff --check` (repo root) | 0 | no output |
| host-path scan | `git diff HEAD \| grep -ci "users/"` (repo root) | 0 | 0 matches |
| DLP escape scan | the two-character backslash+`u` escape grepped over `git diff HEAD` additions and the new files directly | 0 | 0 matches |

Row count: `contracts/fixtures/manifest.json` 248 → 261 (13 raw rows). The host's shared
PostgreSQL 18.6 instance needed crash recovery before the Elixir gate (a host-level kill at
`20:14 CST`/`04:14 PDT` terminated its workers); the Elixir row above records the run that
followed once `psql` accepted connections again.

## 7. Mutants (each applied alone, reverted)

- Bypass the entry inside `requestGoalWithStatus` (inline `json.Decoder` decode, no entry call) → red:
  `--- FAIL: TestResponseDecodeRoutesEveryPathThroughTheEntry/requestGoalWithStatus … decode_test.go:128: entry calls = 0, want 1`
  (observed at the final tree, then reverted).
- Flip one `wire_levels` entry (`goal-command.raw-invalid-utf8` to `["legacy", "typed", "schema"]`) → red:
  `--- FAIL: TestResponseDecodeLevelsMatchTheRawCorpus/goal-command.raw-invalid-utf8 … decode_test.go:83: legacy level rejected = false, want true: <nil>`
  (observed at the final tree, then reverted).
- Replace the Elixir machine-request decoder with `Jason.decode/1` → red (`mix` exit 2):
  `1) test public Elixir validators agree with every manifest semantic expectation …
  evidence-batch.raw-duplicate-member-nested expected elixir wire acceptance=false, got true`
  (observed at the final tree, then reverted; the test file's working blob equals its
  pre-mutant blob `152d477906dcf0b1cc5e21502a55aad34a54c2ce`, and the gate re-ran green).
