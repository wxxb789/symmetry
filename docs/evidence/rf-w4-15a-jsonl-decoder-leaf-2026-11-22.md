# rf-w4-15a JSONL decoder leaf — evidence record (2026-11-22, fix round r1)

Contract: `goal-rf-w4-15a-jsonl-decoder-leaf.md` (GDD state root). Plan:
`docs/plans/2026-11-22-rf-w4-15a-jsonl-decoder-leaf-plan.md`.
Base revision: `de526861313b05908c95d21dc1902a516c7ef15a` (origin/main after #217 W3-15; the
#187 W3-04 gate is merged too, so no rebase was needed). Nothing is committed: the tree stays
dirty for the verify stage.

An independent read-only review ran at the first freeze; its P1-1 (bound-before-copy ordering),
P2-1 (missing row for records returned before a bound failure) and P3-1 (`Fatal` without `Latch`
forgot a failure) findings are fixed in this revision, and the review's missing-evidence findings
(no WSL/mutation record, stale hashes) are closed by this file. Every number below was produced on
the revision these hashes bind; the throwaway differential harness is kept at
`Q:/tmp/w4-15a-diff/decoderdiff-dump_test.go` with its dumps beside it.

Subject hashes (this record binds exactly these bytes; every mutation run was reverted and the
hashes re-verified afterwards):

| File | sha256 |
| --- | --- |
| `daemon/internal/harness/linejson/decoder.go` | `8070def6478542b67a01bad8773a9c0e4855a95f78da9f5b1d7d1681b4a6a960` |
| `daemon/internal/harness/linejson/decoder_test.go` | `fe6d429e2802805208ca49a8078b713ec2e288494408613db8c6e3008c9712d5` |
| `daemon/internal/harness/pi/rpc.go` | `4c98a01f76db8cf0e4b6d1543471fe11dd63beb803e27d98168bc3a8755e7394` |
| `daemon/internal/harness/omo/rpc.go` | `0cba5a64c08d6609dfd6ce64d0ff50bd4608471828f38e10ddc61e97673cee1d` |
| `daemon/internal/harness/claude/stream.go` | `c4092e1c6023f1b5607fa3f48b2f273d1565ec9a85223d5287495e1d0dc5387a` |
| `daemon/internal/harness/codex/framing.go` | `5d60421b8fe5356a5770a9131fc8d0a25917b97b34048aa6f609f4a335ce550c` |
| `daemon/internal/harness/pi/rpc_test.go` | `8001d5945e87782c82800204c90ced3ef16178c836b329a84716e4a4856db5ed` |
| `daemon/internal/harness/omo/rpc_test.go` | `610218a812c998be74ca7e88bc4dc45db981632b2971cbdaade3fbdd778255ef` |
| `daemon/internal/harness/claude/stream_test.go` | `c994c5a76540edaf929ee8367ff871f0a1a2c41c5b537a3850e4ab429399b795` |
| `daemon/internal/harness/codex/framing_test.go` | `8148de13306ef47c97d1a836871776635927432f001f1cb0d6997fcdd89f3b52` |

`git diff | sha256sum` (tracked changes only; the two docs in this slice are untracked):
`7b6eb2f1d3c81dce3ead109658db165d15bf4e06a8f9ba874c3b3c8c978df33c`.

## 1. Definition counts (clause 1)

Commands run at the base revision (`git show de526861:<path> | grep -c <pattern>`) and on the
working tree (`grep -c <pattern> <path>`), non-test sources only:

| grep | pi/rpc.go | omo/rpc.go | claude/stream.go | codex/framing.go | linejson/decoder.go |
| --- | --- | --- | --- | --- | --- |
| `IndexByte` (the LF scan) | 1 → 0 | 1 → 0 | 1 → 0 | 1 → 0 | 0 → **1** |
| `buffer = append` (buffering) | 2 → 0 | 1 → 0 | 1 → 0 | 1 → 0 | 0 → **1** |
| `> (decoder.max\|decoder.limit\|framer.max)` (bound checks) | 2 → 0 | 1 → 0 | 2 → 0 | 2 → 0 | — |
| bound decisions in the leaf | — | — | — | — | `decoder.go:77` (`> MaxBytes`), `:85`/`:97` (`overBound` calls), predicate at `:159` |

Files containing a bounded-line framing loop (LF scan + bound + blank policy + close): **4 → 1**
(`linejson/decoder.go`). The four wrappers keep `NewDecoder`/`NewFramer`/`Feed`/`Close` and the
same record types, and the `decode*` record functions are untouched (`decodeRecord` pi:288→246,
omo:158→128; `decodeEvent` claude:151→133; `decodeFrame` codex:120→100 — identical bodies).

## 2. Per-adapter behaviour table (clause 2)

| | `pi` | `omo` | `claude` | `codex` |
| --- | --- | --- | --- | --- |
| duplicate-key policy | explicit walk, `ErrDuplicateField` + key | explicit walk, `invalid OmO RPC record: duplicate JSON member` | none of its own; `jsontext.Value.IsValid` rejects duplicates (`ErrMalformedJSON`) | none of its own; `jsontext.Value.IsValid` (`malformed_json` diagnostic) |
| non-finite numbers | accepted | accepted | accepted (`usage` stays raw) | accepted |
| bound timing | pending length compared before its bytes are copied (`ChunkFirst: false`) | same | whole chunk buffered first (`ChunkFirst: true`) | same as claude |
| CR | `CRAllowance` (MaxBytes+1 when the byte before LF is CR) + `TrimCR` | ordinary byte, counts toward the bound, reaches `decodeRecord` | ordinary byte | ordinary byte |
| blank lines | `SkipBlank`: skipped, no sequence | delivered (a blank line is `ErrInvalidRecord` and fails the feed) | skipped | skipped |
| record-decode failure | in-band `Record.DecodeError` | `Fatal`: the bad record is returned and the feed fails permanently | in-band `Event.DecodeError` | in-band diagnostic frame |
| latch | `Latch`: the bound failure is reported from every later call | latched | none | none |
| overflow state | latched, suffix dropped | latched, suffix dropped | post-LF suffix retained for the next call | post-LF suffix retained |
| no-LF overflow | buffer dropped | buffer dropped | buffer dropped | buffer dropped |
| close shape | `([]Record, error)`: one `Record{DecodeError: ErrIncompleteRecord}` + error | `error` only | `([]Event, error)`: one `invalidEvent` + error | `([]Frame, error)`: one `incomplete_frame` diagnostic + error |
| close blank rule | whitespace-only suffix empty | any suffix byte is unterminated | whitespace-only suffix empty | whitespace-only suffix empty |
| sentinels | `ErrRecordTooLarge`, `ErrIncompleteRecord` | wrapped `ErrInvalidRecord` (2 texts) | `ErrRecordTooLarge`, `ErrIncompleteRecord` | `ErrFrameTooLarge`, `ErrIncompleteFrame` |
| nil receiver | `pi rpc decoder is nil` | panics (nil deref) | `Claude stream-json decoder is nil` | `codex framer is nil` |

Parameter that reproduces each base difference: CR → `CRAllowance`/`TrimCR`; blank policy (lines
and close) → `SkipBlank`; copy ordering → `ChunkFirst`; failure policy → `Latch` + `Fatal`; bound
value → `MaxBytes`; error identity → the `TooLarge`/`Incomplete` constructors; close record → built
by each wrapper from `Close`'s `(sequence, pending, err)`. The duplicate-key and non-finite policies
remain inside each adapter's `Decode` callback (`decode*` bodies unchanged); the leaf adds neither.

Two deliberate, unobservable differences:

- `Close` advances the shared sequence number whenever a pending suffix exists. `omo`'s base
  `Close` advanced nothing and returned no record; the difference is invisible (`omo` latches the
  failure and never emits that record or a later sequence), and the dump's `close`/`post` arms
  confirm it (both sides byte-identical).
- `ChunkFirst` is a copy-ordering property, not an acceptance one: flipping it on `pi` leaves every
  suite green (mutation (g)), because no returned record, error or state differs. It is pinned by
  the §3 branch mapping and by the dump, not by a behavioural row.

## 3. Base→HEAD control-flow audit (conductor decision 2026-11-21, lesson 32)

Every base branch of the four deleted framing blocks, against the statement that reproduces it.

`pi/rpc.go` (base lines):

| Base | Branch | HEAD | Preserved |
| --- | --- | --- | --- |
| 221-223 | nil receiver → `pi rpc decoder is nil` | `pi.Decoder.Feed` guard (unchanged text) | yes |
| 224-227 | latched failure → `nil, failed` | `linejson.Decoder.Feed` failed check | yes |
| 228-235 | loop guard + no-LF bound `len(buffer)+len(chunk) > max` → `fail(ErrRecordTooLarge)` **before** `append` | leaf pull arm (`ChunkFirst: false`): the same length is compared before the bytes are copied; `Latch: true` clears the buffer | yes |
| 237-240 | complete-line bound + CR allowance, before the line bytes are appended | leaf pull arm `overBound(lineBytes, trailerBeforeLF(buffer, chunk, chunkEnd))` | yes |
| 241-249 | append, split, `buffer = nil`, CR strip, blank skip | leaf line consumption + `TrimCR` + `SkipBlank` | yes |
| 250-253 | sequence++, `decodeRecord` | leaf `Decode: decodeRecord` | yes |
| 258-273 | close: nil guard, latched check, blank rule, sequence++, record + `fail` | leaf `Close` + wrapper record build + `Latch` | yes |
| 275-279 | `fail`: clear buffer, latch | leaf `fail` (`Latch`) + per-arm buffer handling | yes |
| 281-286 | `hasTrailingCRBeforeLF` | `trailerBeforeLF` + `overBound` | yes |

`omo/rpc.go`: 114-125 bound before append + latch; 126-139 decode, record append, first decode
error fatal; 147-156 close (any non-empty buffer unterminated, no record). HEAD: leaf loop with
`ChunkFirst: false`, `Latch: true`, `Fatal: record.DecodeError`, `SkipBlank: false`; wrapper
`Close` returns the leaf's error only.

`claude/stream.go`: 106-135 (`buffer = append` first, no-LF bound clears the buffer, complete-line
bound keeps the suffix, blank skip, no latch); 137-149 close builds `invalidEvent`. HEAD: leaf loop
with `ChunkFirst: true` (drain arm, suffix retained), `SkipBlank: true`, no latch; wrapper builds
the same `invalidEvent`.

`codex/framing.go`: 75-104 as claude, diagnostics in-band; 106-117 close builds
`decodeDiagnosticFrame(..., "incomplete_frame", "stream ended before newline")`. HEAD: leaf loop
with `ChunkFirst: true` and `Decode: decodeFrame`; wrapper builds the same diagnostic from
`pending`.

No deleted block contains a `switch`/`default` clause. The `switch` statements in the touched
region (claude `supportedEventType`/`decodeEvent` record switch, codex `Frame.IsFramingError`
code switch) are untouched.

## 4. Pinned error strings

Sentinel literals extracted per file at the base and on the working tree are byte-identical:
`pi/rpc.go` 43 strings, `omo/rpc.go` 7, `claude/stream.go` 23, `codex/framing.go` 4
(`grep -oE 'errors\.New\("[^"]*"\)'` on both revisions, `diff` empty). The leaf package contains no
error text at all. The `omo` bound text (`invalid OmO RPC record: JSONL record exceeds limit`) is
asserted verbatim by the new `TestDecoderCountsTrailingCRTowardTheLimit` and
`TestDecoderRetainsTheBoundFailureAcrossCalls` rows; the unterminated text is asserted non-nil in
the suite and compared verbatim by the dump. Every other bound/incomplete text appears in the
dump's `err="..."` fields on both sides.

## 5. Move-only declaration comparison

Deleted declarations (base) against the working tree, from `git diff -U0` plus the declaration
lists (`grep -nE "^(func|type)"`):

| Base declarations | Working tree |
| --- | --- |
| `pi`: `type Decoder`, `NewDecoder`, `Feed`, `Close`, `fail`, `hasTrailingCRBeforeLF` (204-286) | same four exported declarations, bodies delegate; `fail`/`hasTrailingCRBeforeLF` live in `linejson` |
| `omo`: `type Decoder`, `NewDecoder`, `Feed`, `Close` (100-156) | same four, delegating |
| `claude`: `type Decoder`, `NewDecoder`, `Feed`, `Close` (88-149) | same four, delegating |
| `codex`: `type Framer`, `NewFramer`, `Feed`, `Close` (59-117) | same four, delegating |
| — | new `linejson`: `Policy[R]`, `Decoder[R]`, `New`, `Feed`, `Close`, `fail`, `overBound`, `trailerBeforeLF` |

`git diff --color-moved=plain` over the four files is the aid (the moved loop is not line-identical
because the four bases differ in the arms above); the acceptance is the per-branch table of §3 plus
the differential dump of §6. `decode*` bodies are byte-identical (no `-` line falls inside them in
`git diff`).

## 6. Differential corpus dump (base vs head)

Throwaway harness `daemon/internal/harness/decoderdiff` (deleted from the tree after the run, kept
at `Q:/tmp/w4-15a-diff/decoderdiff-dump_test.go`; a copy ran in a `git worktree` of `de526861`):
for each of 864 cases it feeds one or more chunks to all four decoders, then `Close`, then a
post-close `Feed("z\n")`, and writes every record (sequence, raw bytes, type/envelope fields,
decode error text) and every error text.

Corpus: 32 fixture files under `daemon/internal/harness/testdata/**/*.jsonl` (whole file plus four
sampled two-way splits each) plus 34 synthetic edge inputs in `-no-lf`/`-lf` variants (empty, blank
lines, CRLF at/over the bound, at-bound partials, over-bound partials, an over-bound line with a
retained full suffix and with a partial suffix, two over-bound lines, a good line followed by an
over-bound line in both framings, duplicate keys — top, escaped, nested, in arrays, in sibling
objects —, `1e1000`, invalid UTF-8, unpaired surrogate, non-object/scalar/null/string, malformed,
empty object, a response envelope, an unknown event) with every split point for inputs ≤ 24 bytes
and four samples otherwise; limits 8, 1024 and 1 MiB.

| Run | Cases | Dump bytes | Lines | sha256 |
| --- | --- | --- | --- | --- |
| base `de526861` | 864 | 3 755 579 | 26 588 | `e9342871a5c989395c20452c5929df86cca4d775bed6ee1692f3fa5011915cea` |
| working tree | 864 | 3 755 579 | 26 588 | `e9342871a5c989395c20452c5929df86cca4d775bed6ee1692f3fa5011915cea` |

`cmp` reports no difference. Three arms worth naming explicitly, all byte-identical on both sides:

- records completed before the bound failure are returned with the error
  (`pi|claude edge/good-then-over-bound-lf/split1`, limit 17: one record plus
  `pi rpc record exceeds limit` / `Claude stream-json record exceeds limit`; `pi` then latches the
  error into `close`/`post`, `claude` closes cleanly and decodes the post feed).
- the retained suffix after a non-latching bound failure
  (`claude edge/over-bound-with-suffix-lf/split12`: feed0 no events + error, feed1 emits sequence 1
  with raw `rest`, close empty, post feed emits `z`).
- `omo edge/record-at-bound-lf` (limit 17, record + CRLF) →
  `err="invalid OmO RPC record: JSONL record exceeds limit"` (the CR counts toward `omo`'s bound);
  `omo edge/lf-only-lf` → one record with `err="invalid OmO RPC record"` plus the latched error;
  `claude`/`codex edge/non-finite-lf` → accepted (`1e1000` is never rejected; the claude event
  carries only its missing-type decode error, the codex frame is an `unknown_native_event`
  diagnostic, not `malformed_json`).

A second dump covers the nil receivers and is also byte-identical (sha256
`2af5d0c3e5fafebdd0bb1b51cbd5ca4846b5fb2e54d1981acf8140903ed6365c`, 440 bytes):

```
pi.Feed	err="pi rpc decoder is nil"	n=0
pi.Close	err="pi rpc decoder is nil"	n=0
claude.Feed	err="Claude stream-json decoder is nil"	n=0
claude.Close	err="Claude stream-json decoder is nil"	n=0
codex.Feed	err="codex framer is nil"	n=0
codex.Close	err="codex framer is nil"	n=0
omo.Feed	panic: runtime error: invalid memory address or nil pointer dereference
omo.Close	panic: runtime error: invalid memory address or nil pointer dereference
```

The longest fixture line is 3 734 bytes (`testdata/pi/1.5.2/lifecycle.jsonl`) against the 1 MiB
default bound, so the fixture replays cannot exercise the bound arms; the synthetic corpus above
does.

## 7. Gates (all foreground; exit codes as printed)

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l` over the nine changed Go files | empty, exit 0 |
| vet | `cd daemon && go vet ./...` | exit 0 |
| build windows | `GOOS=windows go build ./...` | exit 0 |
| build linux | `GOOS=linux go build ./...` | exit 0 |
| cross vet | `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...` | exit 0 |
| cycle proof | `go build ./internal/harness/...` | exit 0 |
| import invariant | `go list -deps ./internal/harness/claude \| grep internal/harness` | `…/harness/linejson`, `…/harness/claude` only — no root `harness` |
| windows suite | `cd daemon && go test -count=1 -timeout 900s ./internal/harness/...` | 9/9 packages `ok` (root 13.752s, claude 0.144s, codex 13.765s, linejson 0.112s, omo 2.539s, omp 3.186s, opencode 0.448s, pi 4.438s, probeconformance 7.194s), exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host path | tracked diff + the four untracked docs/files filtered for a profile-directory separator | 0 hits |
| suppressions | new `//nolint`/`credo:disable` in the diff | 0 |

## 8. WSL Linux run

`wsl -d Ubuntu-24.04 -u accept`, tree under test = this worktree
(`/mnt/q/repos/wt-w4-15a-jsonl-decoder-leaf/symmetry/daemon`), one whole-tree invocation
`go test -count=1 -timeout 1200s ./internal/harness/...` (log at `Q:/tmp/w4-15a-diff/wsl-harness.txt`):

| Package | Result |
| --- | --- |
| root `harness` | `ok 12.125s` |
| `claude` | `ok 0.093s` |
| `codex` | `ok 13.822s` |
| `linejson` | `ok 0.014s` |
| `omo` | `ok 1.702s` |
| `probeconformance` | `ok 7.048s` |
| `omp` | `FAIL 33.200s` — one test, `TestNativeAcceptanceCancelToolOracle` |
| `opencode` | `FAIL 1200.061s` — the package hit the 20-minute Go test timeout (`panic: test timed out after 20m0s`) inside its `TestClient*`/`TestNativeOpenCode*` loopback families |
| `pi` | `FAIL 427.482s` — 11 `TestProviderBridge*` tests, every failure a `dial tcp 127.0.0.1:<port>: i/o timeout` |

Observed exit status for a whole-tree invocation: **1**. Re-run with `-timeout 150s` (so the
loopback stalls cannot consume 20 minutes) reproduces the structure —
`ok` root 11.351s / claude 0.181s / codex 13.150s / linejson 0.016s / omo 1.354s /
probeconformance 7.033s, `FAIL` omp 32.036s / opencode 150.067s / pi 150.043s, 12 `--- FAIL` test
lines, 2 package-timeout panics — and the status is captured in the WSL shell as `tree_rc=1`
(log `wsl-tree-150.txt`).

Every failing test is loopback/IPC-dependent and cannot be caused by this slice: the distro cannot
complete a loopback TCP connect at all (`python3` connect to `127.0.0.1` times out), the failing
tests are HTTP tests that never touch a decoder (`pi/provider_bridge_test.go`'s only `.Close()` /
`json.NewDecoder` matches are HTTP bodies), and a representative failing pi test fails identically
at the base revision in a `de526861` worktree in the same distro (`FAIL …/pi 150.227s`, the same
30 s dial timeouts).

Decoder-relevant WSL surface with an exact exit code: `go test -count=1 ./internal/harness
./internal/harness/claude ./internal/harness/codex ./internal/harness/linejson ./internal/harness/omo
./internal/harness/probeconformance` → 6/6 `ok` (11.009s / 0.076s / 12.770s / 0.015s / 0.948s /
7.046s), `core_rc=0` (captured inside the WSL shell as `echo "core_rc=\$?"` after writing the log).
`pi` without its provider-bridge tests (`-skip "TestProviderBridge"`) → `ok 0.784s`, `pi_rc=0`
(the package line itself is the decoder suite plus the validator/state tests).

`omp`, `opencode` and `pi` with the loopback families skipped (`-skip
"TestProviderBridge|TestNativeAcceptanceCancelToolOracle|TestClient|TestNativeOpenCode"`):
`omp ok 2.270s`, `pi ok 0.938s`, `opencode FAIL 600.051s` — one further loopback test
(`TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest`) stalls in
`net/http.(*Transport).startDialConnForLocked` and hits the default package timeout. `opencode` is
outside this slice (E3/D1) and its failure is the same dial stall.

## 9. Mutation runs

Each mutant was applied to the frozen tree, the named suites were run, and the mutant was reverted
(`sha256sum -c` against the §0 table shows only the files that legitimately changed afterwards).
Mutants (a1), (a2) and (b) were re-applied and re-run after the final comment-only clarification in
`linejson/decoder.go`, so every leaf mutation is bound to the §0 bytes; (c), (d) and (e) ran before
the latch rows were appended to the adapter suites, and each reddened a row that is byte-identical
in the frozen files.

| Mutant | Change | Result |
| --- | --- | --- |
| (a1) drop the CR handling entirely | `overBound` returns true for every over-bound line | red: `linejson TestFeedBoundAdmitsCRLFOnlyUnderTheAllowance` (3 subtests) + `pi TestDecoderTreatsCRLFAsTheSameBoundedRecordAndRejectsNestedDuplicateFields`; `omo`/`claude`/`codex` green (their suites have no CR-bounded row) |
| (a2) drop the CR policy axis | allowance applies to every policy (`CRAllowance &&` removed) | red: `linejson TestFeedBoundAdmitsCRLFOnlyUnderTheAllowance` + `omo TestDecoderCountsTrailingCRTowardTheLimit`; `pi` green (it loses nothing) |
| (b) remove the max-byte bound | pull arm `> MaxBytes` → `> 1<<30`, `overBound` → false | red in all five packages: `linejson` (bound + latch + pre-failure-record rows), `pi TestDecoderFailsClosedFor…`, `omo TestDecoderRejectsAmbiguousOrUnboundedJSONL` + CR row, `claude TestDecoderRejectsOversizeAndTruncatedRecords`, `codex TestFramerRejectsIncompleteAndOversizedFrames` + whitespace row |
| (c) add non-finite rejection | `nonFiniteMutant` check in `claude.decodeEvent` and `codex.decodeFrame` | red: `claude TestDecoderKeepsNumbersBeyondFloat64Range`, `codex TestFramerKeepsNumbersBeyondFloat64Range` (only those rows) |
| (d) turn `codex`'s malformed-line diagnostic into a failure | `Fatal` returns an error for `malformed_json` frames | red: `codex TestFramerMarksMalformedJSONWithoutPromotingItToSuccess`, `TestFramerRejectsAmbiguousOrInvalidUnicodeJSON` (6 subtests), `TestFramerReadsSanitizedVersionFixture` |
| (e) drop `omo`'s fatal record policy | remove `Fatal` from `omo`'s policy | red: `omo TestDecoderRejectsAmbiguousOrUnboundedJSONL` (5 subtests) + `TestDecoderTreatsBlankLinesAsRecords` |
| (f1) `pi` loses its latch | `pi` `Latch: false` | red: `pi TestDecoderRetainsTheBoundFailureAcrossCalls` (and *not* the pre-existing oversize row, whose second probe input is itself over-bound — recorded as the reason the new row exists) |
| (f2) latch wiring flipped | `omo` `Latch: false`; `claude`/`codex` `Latch: true` | red: `omo TestDecoderRetainsTheBoundFailureAcrossCalls`; `claude TestDecoderAcceptsTheNextRecordAfterABoundFailure`; `codex TestFramerAcceptsTheNextFrameAfterABoundFailure`; `pi` green (its latch unchanged) |
| (g) copy-ordering axis flipped | `pi` `ChunkFirst: true` | **green**, as predicted and documented: the axis is a copy-ordering property, not an acceptance one (plan §4, evidence §2) |
| duplicate-walk row (base-pinned, unchanged code) | — | `pi TestDecoderTreatsCRLFAs…` already reddens when the walk is dropped (reviewer-verified); `omo TestDecoderRejectsAmbiguousOrUnboundedJSONL` likewise |

## 10. LOC delta (report-only, D2)

`git diff --numstat` (tracked): non-test sources 61 insertions / 169 deletions across the four
wrappers (`pi` −42, `omo` −30, `claude` −17, `codex` −16 by net line count), plus the new
`linejson/decoder.go` (176 lines including the policy documentation) = **net +68 non-test lines**.
Tests: 89 added lines in the four adapter suites (0 deleted) + `linejson/decoder_test.go` (243
lines). Docs: the plan (304 lines) and this record. The contract's `−600…−800` figure covers the
whole 5b (driver half included) and is report-only (D2).

## 11. Residual risks and known limits

- `pi`'s `strictObject`/`omo`'s `scanJSONValue` (and their non-decoder call sites) stay separate:
  the two walkers carry distinct error identities and are not part of the framing duplication.
- The leaf's `Decode` callback receives a view into the decoder buffer and must not retain it; all
  four adapters copy what they keep (verified by reading `decodeRecord`, `decodeEvent`,
  `decodeFrame`, and `omo`'s `json.Unmarshal` into `json.RawMessage`).
- `omo`'s `Close` no longer advances the sequence number; unobservable (latched, no record
  returned), recorded in §2.
- `ChunkFirst` cannot be shown red by any behavioural row; its acceptance is the §3 branch mapping
  plus the dump (mutation (g) documents the green result and why).
- The differential dump compares the decoder surface, not the adapters above it; the adapter
  suites and the fixture replays are the oracle for those layers.
- Native acceptance suites that `t.Skip` without credentials are reported as skipped in the run
  output, not as failures.
