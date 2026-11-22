# rf-w4-15a JSONL decoder leaf — implementation plan

Contract: `goal-rf-w4-15a-jsonl-decoder-leaf.md` (GDD state root). Baseline for every coordinate in
this plan: `de526861313b05908c95d21dc1902a516c7ef15a` (origin/main after #217, the worktree HEAD —
the W3-04/#187 and W3-15/#217 dispatch gates are satisfied, so no rebase is needed). Branch:
`refactor/rf-w4-15a-jsonl-decoder-leaf`. Area: `daemon/internal/harness/**` only.

The increment moves the bounded-line/JSONL framing loop of the four adapter decoders into one leaf
package, `daemon/internal/harness/linejson`, which imports nothing from `harness` (so the existing
root → `claude` edge cannot become a cycle). Each adapter keeps its record type, its `decode*`
function, its public method signatures, its sentinels, its error strings and its acceptance
behaviour; only the framing loop, the buffer and the bound move. No wrapper's acceptance
behaviour changes, and **no duplicate-key or non-finite rejection is added anywhere**.

## 1. Duplication measured at the baseline

Non-test sources only; coordinates are at `de526861`.

| Concern | Copies before | Where | After |
| --- | --- | --- | --- |
| bounded-line framing loop (chunk/buffer scan, LF split, byte bound, blank policy, sequencing, close) | 4 | `pi/rpc.go:204-286`, `omo/rpc.go:100-156`, `claude/stream.go:88-149`, `codex/framing.go:59-117` | 1 implementation (`linejson.Decoder`), 4 thin wrappers |
| `bytes.IndexByte(<bytes>, '\n')` line scan | 4 | same four blocks | 1 (`linejson/decoder.go`) |
| max-byte bound check | 4 | `pi:231,237-240`, `omo:125`, `claude:115,123`, `codex:84,92` | 1 (`linejson.Decoder.Feed`), parameterised |
| trailing-CR detection before LF | 1 | `pi:238,281-286` | 1 (`linejson`), parameter `CRAllowance` + `TrimCR` |
| close-with-buffer handling | 4 | `pi:258-273`, `omo:147-156`, `claude:137-149`, `codex:106-117` | 1 (`linejson.Decoder.Close`), the wrapper builds its own trailing record |

Grep counts at the baseline and on the final tree (re-run and recorded in the evidence file):
`grep -c "IndexByte"` → `pi/rpc.go` 1, `omo/rpc.go` 1, `claude/stream.go` 1, `codex/framing.go` 1,
`linejson` 0; after → 0/0/0/0/2 (`linejson`: the buffer scan and the incoming-chunk scan).
`grep -c "buffer = append"` → 2/1/1/1/0; after → 0/0/0/0/4 (`linejson`: the chunk-first copy, the
unterminated tail, the chunk-arm and buffer-arm retained suffixes, the pull-arm line copy).

## 2. Public / API shape

New package `daemon/internal/harness/linejson` (files `decoder.go`, `decoder_test.go`). It imports
`bytes` only. Source carries no explanatory comments (owner comment policy 2026-11-22, applied in
r3): only the `Decode` buffer-view hazard line remains; behaviours are pinned by the tests.

```go
type Policy[R any] struct {
	MaxBytes    int                                  // bound on one raw line, before any CR trim
	CRAllowance bool                                 // admit MaxBytes+1 bytes when the last byte is CR
	TrimCR      bool                                 // strip one trailing CR before Decode
	SkipBlank   bool                                 // drop whitespace-only lines and a whitespace-only close suffix
	Latch       bool                                 // report the first failure from every later call
	TooLarge    func() error                         // error for a line over the bound
	Incomplete  func() error                         // error for a non-blank unterminated suffix
	Decode      func(sequence uint64, line []byte) R // the adapter's decode* function; line is a buffer view
	Fatal       func(record R) error                 // record that must fail the stream; nil keeps records in-band
}

func New[R any](policy Policy[R]) *Decoder[R]
func (decoder *Decoder[R]) Feed(chunk []byte) ([]R, error)
func (decoder *Decoder[R]) Close() (sequence uint64, pending []byte, err error)
```

`Decode` receives a view into the decoder buffer and must not retain it; all four adapters' `decode*`
functions copy what they keep (pi `rpc.go:289`, omo `rpc.go:160` via `json.Unmarshal` into
`json.RawMessage`, claude `stream.go:152`, codex `framing.go:121`), so no adapter needs a new copy.

Consumers after the change — no public signature changes, `NewDecoder`/`Feed`/`Close`/`NewFramer`/
`decode*` keep their shapes:

| File | Edit |
| --- | --- |
| `pi/rpc.go` | `Decoder` holds `reader *linejson.Decoder[Record]`; `NewDecoder` builds the policy (`MaxBytes`, `CRAllowance: true`, `TrimCR: true`, `SkipBlank: true`, `Latch: true`, `TooLarge`/`Incomplete` → `ErrRecordTooLarge`/`ErrIncompleteRecord`, `Decode: decodeRecord`); `Feed` keeps its nil-receiver error and delegates; `Close` keeps its nil-receiver error, delegates, and builds the trailing `Record` from `pending`. Delete the local fields, the loop, `fail` and `hasTrailingCRBeforeLF`. `decodeRecord` unchanged. |
| `omo/rpc.go` | `Decoder` holds `reader *linejson.Decoder[Record]`; `NewDecoder` keeps the non-positive default; policy `Latch: true`, `Decode: decodeRecord`, `Fatal: record.DecodeError`, `TooLarge`/`Incomplete` construct the two existing wrapped `ErrInvalidRecord` errors; `Feed` and `Close` delegate (no nil-receiver guard, as today). Delete the local fields and the loop. `decodeRecord` unchanged. |
| `claude/stream.go` | `Decoder` holds `reader *linejson.Decoder[Event]`; `NewDecoder` keeps the non-positive default; policy `SkipBlank: true`, `Decode: decodeEvent`, `TooLarge`/`Incomplete` → `ErrRecordTooLarge`/`ErrIncompleteRecord`; `Feed`/`Close` keep their nil-receiver errors and delegate; `Close` builds the trailing `Event` with `invalidEvent`. Delete the local fields and the loop. `decodeEvent` unchanged. |
| `codex/framing.go` | `Framer` holds `reader *linejson.Decoder[Frame]`; `NewFramer` keeps the non-positive default; policy `SkipBlank: true`, `Decode: decodeFrame`, `TooLarge`/`Incomplete` → `ErrFrameTooLarge`/`ErrIncompleteFrame`; `Feed`/`Close` keep their nil-receiver errors and delegate; `Close` builds the trailing diagnostic (raw bytes passed through untrimmed, as today). Delete the local fields and the loop. `decodeFrame` unchanged. |

The leaf applies the bound to each line before its bytes are copied; the `claude`/`codex` bases
buffered the whole chunk first. The r1/r2 interim `ChunkFirst` axis was deleted in r3, once the
non-latch overflow arm kept the pending chunk (`append(buffer[lineEnd+1:], chunk...)`) and the
differential stayed byte-identical (§9.9).

The policy is built by a per-adapter `newReader(maxBytes)` helper that `NewDecoder`/`NewFramer`
call, and the `reader` field is built on demand: a zero-value wrapper (or framer) that never ran
its constructor gets that same policy with `MaxBytes` 0, so it frames exactly as its base did
instead of dereferencing a nil field (r2, §9.8).

Import edges: `linejson` → `bytes` only; `pi`/`omo`/`claude`/`codex` → `linejson`; `codex` keeps its
existing `harness` import (for `Frame.Event`). `claude` gains no import of the root package, and no
file under `linejson` imports `harness`, so no cycle exists by construction.

## 3. Exact edge-case semantics

Per-adapter table (binding, verified by reading the base code at `de526861`):

| | `pi` | `omo` | `claude` | `codex` |
| --- | --- | --- | --- | --- |
| duplicate-key policy | explicit walk, `ErrDuplicateField` with the key (`strictObject` → `rejectDuplicateFields`) | explicit walk, `%w: duplicate JSON member` on `ErrInvalidRecord` | none of its own; `jsontext.Value.IsValid` rejects duplicates as `ErrMalformedJSON` | none of its own; `jsontext.Value.IsValid` rejects duplicates as `malformed_json` |
| non-finite numbers | accepted (`json.Valid`, no number parsing) | accepted (`json.Valid`) | accepted (`jsontext` accepts `1e1000`; `usage`/`result` stay raw) | accepted (same) |
| bound timing | checked on each line before its bytes are copied | same | same (the base copied the whole chunk first; r3 keeps the pending chunk on overflow) | same |
| CR | LF-only framing: `MaxBytes+1` admitted when the byte before LF is CR; one trailing CR is stripped before `decodeRecord` | CR is an ordinary byte: it counts toward the bound and reaches `decodeRecord` | CR is an ordinary byte; the line is `TrimSpace`d by `decodeEvent` | CR is an ordinary byte; `decodeFrame` trims |
| blank lines | skipped (no record, no sequence) | delivered to `decodeRecord` (an empty/whitespace line is `ErrInvalidRecord` and fails the feed) | skipped | skipped |
| record-decode failure | in-band (`Record.DecodeError`), feed continues | fatal: the bad record is returned **and** the feed fails with it, latched | in-band (`Event.DecodeError`) | in-band (diagnostic frame) |
| failure latch | bound/close failures latched; later calls return the same error | same | none (later calls start from the retained suffix) | none |
| complete-line overflow state | buffer dropped, latched | buffer dropped, latched | post-LF suffix retained for the next call | post-LF suffix retained |
| no-LF overflow state | buffer dropped | buffer dropped | buffer dropped | buffer dropped |
| close shape | `([]Record, error)`: a non-blank suffix becomes one `Record` (`DecodeError = ErrIncompleteRecord`) plus that error | `error` only, no record | `([]Event, error)`: one `invalidEvent(ErrIncompleteRecord)` plus that error | `([]Frame, error)`: one `incomplete_frame` diagnostic plus `ErrIncompleteFrame` |
| close blank rule | whitespace-only suffix is empty | any suffix byte is an unterminated record | whitespace-only suffix is empty | whitespace-only suffix is empty |
| sentinels | `ErrRecordTooLarge`, `ErrIncompleteRecord` | wrapped `ErrInvalidRecord` | `ErrRecordTooLarge`, `ErrIncompleteRecord` | `ErrFrameTooLarge`, `ErrIncompleteFrame` |
| nil receiver | `pi rpc decoder is nil` | panics (reads a field) | `Claude stream-json decoder is nil` | `codex framer is nil` |

Closed forms used by the loop:

- line length = `len(buffer) + index('\n')`; the bound admits the line when `length <= MaxBytes`, or
  when `CRAllowance` and `length == MaxBytes+1` and the byte before the LF is CR; otherwise
  `TooLarge()` and the records so far are returned.
- no-LF remainder: `len(buffer)+len(chunk) > MaxBytes` → `TooLarge()` with the buffer cleared.
- `SkipBlank` compares `bytes.TrimSpace(line)`; a skipped line consumes no sequence number.
- `Fatal` is consulted after the record is emitted, so `omo`'s bad record is returned with the error.
- `Close` clears the buffer in every arm; the sequence number advances exactly when a pending
  suffix exists (unobservable for `omo`, which latches and returns no record).

### 3.1 Base→HEAD control-flow audit (conductor decision 2026-11-21, lesson 32)

Every base branch of the deleted blocks, against the parameter or statement that reproduces it.
G: guard, S: select/branch, L: loop, T: tail.

| Base branch (file:line at `de526861`) | Base behaviour | HEAD counterpart | Preserved |
| --- | --- | --- | --- |
| `pi:222` G nil receiver | `errors.New("pi rpc decoder is nil")` | `pi.Decoder.Feed` guard, unchanged text | yes |
| `pi:224-227` G latched failure | `nil, failed` | `linejson.Decoder.Feed` failed check | yes |
| `pi:228-235` L/S no-LF guard and bound | `for len(chunk) > 0`; `len(buffer)+len(chunk) > max` → `fail(ErrRecordTooLarge)` | leaf pull arm: the same length is compared before the bytes are copied; `Latch: true` clears the buffer | yes |
| `pi:237-240` S complete-line bound + CR allowance | `recordBytes > max+1 \|\| (recordBytes > max && !trailingCR)` | `CRAllowance` arm (`MaxBytes+1`, last byte CR) | yes |
| `pi:241-244` S append/split/CR strip | `append`, slice, drop one trailing CR | `TrimCR: true` | yes |
| `pi:246-248` S blank line | `TrimSpace` empty → continue, no sequence | `SkipBlank: true` | yes |
| `pi:250-253` L emit | sequence++, `decodeRecord` | `Decode: decodeRecord` | yes |
| `pi:258-273` T close | nil guard, latched check, blank rule, sequence++, record + `fail` | `Close` + wrapper record build, `Latch: true` | yes |
| `pi:275-279` — `fail` | clears buffer, latches | `linejson` failure arm (`Latch`) + per-arm buffer handling | yes |
| `pi:281-286` — `hasTrailingCRBeforeLF` | byte before LF, buffer tail when `lineEnd == 0` | `CRAllowance` predicate | yes |
| `omo:114-145` L feed | bound before append, `Latch`, first-decode-error fatal, `buffer[:0]` reuse | loop + `Latch: true` + `Fatal` + bound before copy | yes |
| `omo:147-156` T close | latched check, any non-empty buffer is incomplete, no record | `Close`, `SkipBlank: false` | yes |
| `claude:106-135` L feed | append whole chunk first, no-LF bound clears buffer, complete-line bound keeps the suffix, blank skip, no latch | loop, `Latch: false` (suffix and pending chunk retained), `SkipBlank: true` | yes |
| `claude:137-149` T close | nil guard, blank rule, `invalidEvent` + error, no latch | `Close` + wrapper record build | yes |
| `codex:75-104` L feed | as claude; diagnostics in-band | loop, `Decode: decodeFrame` | yes |
| `codex:106-117` T close | nil guard, blank rule, `decodeDiagnosticFrame(..., "incomplete_frame", ...)` | `Close` + wrapper diagnostic build | yes |

No deleted block contains a `switch` or a `default` clause; the only `switch` in the touched region
is `claude`'s `supportedEventType`/`decodeEvent` record switch and `codex`'s `Frame.IsFramingError`
code switch, all untouched.

## 4. Design trade-offs

- **One generic decoder instead of four wrappers around free functions.** The framing loop is
  stateful (buffer, sequence, latch), so it must be a type; the record type differs per adapter, so
  the type carries `R` and the record builder is a policy field. The alternative — a non-generic
  decoder with a per-call emit closure — was rejected: it forces every wrapper to keep its own
  record slice and pass a fresh closure per chunk, which is more wrapper code and one more
  allocation than the base per chunk.
- **`Decode: decodeRecord` by direct reference.** Three of the four adapters pass their existing
  `decode*` function unchanged, exactly the "record decode callback" the contract names; `omo` adds
  the two-line `Fatal` extractor instead of changing `decodeRecord`'s shape.
- **Error constructors, not error values.** `TooLarge`/`Incomplete` are built at failure time, so
  `omo`'s two wrapped `ErrInvalidRecord` errors keep the base lifetime (one value per failure, then
  latched) instead of being shared across decoder instances.
- **The duplicate-key policy stays with the `Decode` callback, not as a leaf flag.** Duplicate
  rejection is part of each adapter's object decoding: `pi`/`omo` walk the members because they
  validate with `json.Valid`, which accepts duplicates; `claude`/`codex` get rejection from
  `jsontext.Value.IsValid`. A leaf-level flag would either duplicate the walk or change which
  validator decides, and it cannot sit behind `decode*`'s unchanged shapes (contract Change 3). The
  leaf adds no duplicate-key and no non-finite rejection; §3 records the per-adapter policy and the
  mutation plan proves `claude`/`codex` stay sensitive to a non-finite rejection (mutation (c)).
- **`SkipBlank` also carries the close rule.** Whitespace-only suffixes are empty for the three
  adapters that skip blank lines and are unterminated records for `omo`; one flag keeps those two
  facts equal to the base, which is exactly how the base composes them.
- **One loop, bound before copy.** `Feed` drains complete buffered lines and, when none is
  buffered, pulls the next line's bytes from the chunk: the length is compared before the bytes
  are copied, exactly as `pi`/`omo` did, and a bound failure keeps the post-LF suffix and the
  pending chunk, exactly as the `claude`/`codex` bases did by copying the whole chunk first (r3).
  The single ordering is observationally identical on the 60 000-case differential for all four
  adapters (evidence §6.1). The r1/r2 interim `ChunkFirst` axis was removed in r3; the branch it
  guarded is pinned by the two retained-overflow adapter rows and mutations m5/m6 (evidence §9
  (j)/(k), §11).
- **Rejected: a `Policy` of named presets (`Pi()`, `Omo()`, …).** It would put adapter names into
  the leaf package and hide the per-adapter table behind constructors; the evidence file has to show
  the parameter for every base branch, so the table stays explicit at the call site.
- **Rejected: leaving `omo` local (contract Change 4).** Its behaviour is expressible (`Latch`,
  `Fatal`, `SkipBlank: false`) with no base branch lost, so the narrower outcome is not needed.

## 5. Baseline and delta (report-only, D2)

Single source of the delta: `git diff --stat de526861` on the final tree, reproduced in the evidence
file with `git diff --numstat`. Report-only: the contract's dLOC figure is not a gate (D2).

## 6. Evidence plan (all foreground, on the final tree)

| Gate | Command |
| --- | --- |
| format | `gofmt -l` over the changed Go files (expect empty) |
| vet | `cd daemon && go vet ./...` (host windows/amd64) |
| builds | `GOOS=windows go build ./...`, `GOOS=linux go build ./...` |
| cross vet | `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...` |
| import invariant | `go list -deps ./internal/harness/claude` lists no root-`harness` package; `go build ./internal/harness/...` succeeds (no cycle) |
| windows suite | `cd daemon && go test -count=1 ./internal/harness/...` |
| linux suite | `wsl -d Ubuntu-24.04 -u accept` → `cd /mnt/q/repos/wt-w4-15a-jsonl-decoder-leaf/symmetry/daemon && go test -count=1 ./internal/harness/...` |
| whitespace | `git diff --check` |
| host path | diff filtered for a profile-directory separator (expect none) |
| definition counts | the greps of §1, before (recorded above) and after |
| declaration set | per file: the deleted framing declarations against the wrapper + leaf declarations, and `git diff --color-moved=plain` as an aid only |
| differential corpus | throwaway harness over the four wrappers, dumped at origin/main `e84973a7` and at the final tree, byte-compared: 60 000 random cases per adapter in r3 (evidence §6.1; the r2 864-case corpus is historical §6.2) |
| mutation (a) | drop the leaf's CR allowance → `pi`'s CRLF row reddens; add the allowance to `omo` → `omo`'s CR-bound row reddens |
| mutation (b) | remove the bound check → the four oversize rows and the affected fixture replays redden |
| mutation (c) | add non-finite rejection to `claude`/`codex` → their new acceptance rows redden |
| mutation (d) | turn `codex`'s malformed-line diagnostic into an error → `codex/framing_test.go` reddens |
| mutation (e) | drop `omo`'s `Fatal` extractor → `omo`'s malformed rows redden |
| mutation (f) | drop `pi`'s duplicate walk (`pi` side) → the duplicate row reddens |
| mutation (f1) | `pi` `Latch: false` → `TestDecoderRetainsTheBoundFailureAcrossCalls` reddens |
| mutation (f2) | `omo` `Latch: false`, `claude`/`codex` `Latch: true` → each new resume/retain row reddens |
| mutation (g) (r1/r2) | flip `pi`'s interim `ChunkFirst` axis → reported green; the axis was deleted in r3 (see §4) |
| mutation (j)/(k) (r3) | r3 overflow arm → `buffer = nil` / reverted `buffer[lineEnd+1:]` → the two retained-overflow adapter rows redden and the differential changes for `claude`/`codex` only |
| mutation (l) (r3) | disable the complete-line bound predicate (`overBound` → false), keep the unterminated guard → the new `pi`/`claude` complete-line rows redden (plus the pre-existing bound rows) |
| WSL pre-existence | for every WSL failure, the same test at the base revision in a `de526861` worktree, plus a loopback connect probe (bind/listen/connect on 127.0.0.1) as the distro-level cause |

No measurement or benchmark harness runs concurrently with the suite. Native acceptance suites that
`t.Skip` without credentials are reported as not run with their skip conditions named.

New test rows (contract-pinning, additive; no existing assertion is weakened or deleted):

- `omo/rpc_test.go`: a record of exactly the decoder limit plus CRLF must fail (the CR counts toward
  `omo`'s bound — the leaf must not give `omo` the `pi` allowance).
- `claude/stream_test.go`, `codex/framing_test.go`: a record carrying `1e1000` must be accepted
  (`claude`: no `Event.DecodeError`; `codex`: an unverified-response diagnostic, not
  `malformed_json`), so adding non-finite rejection reddens the stream suites.
- the r2 zero-value rows: `TestDecoderZeroValueKeepsTheZeroBound` in `pi`/`omo`/`claude`,
  `TestFramerZeroValueKeepsTheZeroBound` in `codex` and `TestFeedZeroBoundAdmitsOnlyAnEmptyLine` in
  the leaf pin the base's zero-bound framing for a zero-value decoder (mutation (i)).
- the r3 retained-overflow rows: `claude`/`codex` receive a retained over-bound suffix, then `{}`,
  then `Feed(nil)`, and must return exactly the `{}` record — the overflow arm that keeps the
  pending chunk (mutations (j)/(k)).
- the r3 terminated-line bound rows: `pi` `NewDecoder(4).Feed("12345\r\n")` and `claude`
  `NewDecoder(8).Feed("123456789\n")` must be rejected (mutation (l)).

## 7. Which moved strings are already asserted

| String from the shared code | Existing assertion | Compensating action |
| --- | --- | --- |
| `pi rpc decoder is nil`, `Claude stream-json decoder is nil`, `codex framer is nil` | none | The wrappers keep the literal text; the differential dump feeds nil receivers in a scratch copy and compares |
| `pi rpc record exceeds limit` / `pi rpc stream ended with an incomplete record` | `pi/rpc_test.go:271,277,281` | sentinel declarations untouched; the dump compares the strings |
| `invalid OmO RPC record: JSONL record exceeds limit` / `…: unterminated JSONL record` | `omo/rpc_test.go:110,117` assert non-nil only | The exact strings are asserted by the new `omo` bound row and compared by the dump |
| `Claude stream-json record exceeds limit` / `… incomplete record` | `claude/stream_test.go:309,314` | unchanged |
| `codex app-server frame exceeds limit` / `… incomplete frame` | `codex/framing_test.go:89,96` | unchanged |
| `codex` diagnostic codes/messages (`incomplete_frame`, `malformed_json`, `unknown_native_event`, …) | `codex/framing_test.go` (four suites' rows) | unchanged; mutation (d) proves the rows still pin the diagnostic path |

## 8. Out of scope (with reasons)

- The `stagedDriver` and the adapter session files and their suites (W4-15b): `staged_driver.go`,
  `pi/adapter.go`, `codex/adapter.go`, `claude_candidate_adapter.go`, `omo/adapter.go`, `omp/**`.
- The probe/registry layer (W3-04, merged as #187): `registry.go`, the probe files,
  `harness_test.go`, `bounded_probe.go`.
- `opencode/**` (E3/D1): not owned, not edited, not named in any shared policy.
- Capture-fixture pruning (W5-10): `harness/testdata/**` untouched.
- `internal/platform/**`, `internal/execution/**`, `internal/app/**`, `daemon/e2e/**`: no contract
  change reaches them; `NewDecoder`/`NewFramer` signatures are unchanged.
- Unifying `pi`'s and `omo`'s `strictObject`/`scanJSONValue` (and their non-decoder call sites such
  as `isJSONObject`, `DecodeAssistantTaskResult`): they carry distinct error identities and are not
  part of the framing duplication this slice owns. Recorded as a residual, not attempted.

## 9. Ambiguities resolved

1. **The duplicate-key policy is a callback property, not a leaf flag.** Contract Change 1 lists it
   among the reader's parameters, but Change 3 freezes `decode*` shapes and the four adapters'
   duplicate rejection lives inside those functions (`pi`/`omo` walk; `claude`/`codex` inherit
   strictness from `jsontext`). §3 records the per-adapter policy and §4 gives the reason; the leaf
   introduces no rejection of its own.
2. **Mutation (a) and `omo`.** The conductor's mutant "drop the CR handling" reddens `pi` directly
   (the CR allowance). `omo` has no CR branch at the baseline: a CR is an ordinary byte that counts
   toward the bound, so the `omo` suite is insensitive to *removing* CR handling but reddens when
   CR tolerance is *added* to `omo`'s path. The new `omo` CR-bound row pins that side, and both
   directions are run and recorded as (a1)/(a2).
3. **`Close` for `omo` and the sequence number.** The shared `Close` advances the sequence whenever
   a pending suffix exists; `omo`'s base `Close` does not advance it, and does not return it. The
   difference is unobservable (`omo` latches the failure and never emits that record or a later
   sequence), recorded in §3.1.
4. **Closing the increment.** Contracts, the run ledger and `answer.md` live in the GDD state tree
   and are operator-owned; this repository closes an increment with the plan file, the source, the
   tests and the evidence record (precedent: `docs/plans/2026-11-21-rf-w3-04-probe-registry-dedup-plan.md`
   §9.4). `docs/TODO.md` has no entry that names this slice or these files; nothing opens or closes.
5. **WSL gate path.** The contract names `/mnt/q/repos/symmetry/daemon`; that is the canonical
   checkout, not this worktree, so the Linux run uses
   `/mnt/q/repos/wt-w4-15a-jsonl-decoder-leaf/symmetry/daemon` and records that path.
6. **The evidence record's home.** The contract's clause 2 puts the judging artifact in the GDD
   run's `answer.md` (outside the repository, operator-owned). The two most recent merged
   increments of this program (`rf-w3-15`, `rf-w4-17b`) also wrote a repository record under
   `docs/evidence/`, which is the shape this slice matches with
   `docs/evidence/rf-w4-15a-jsonl-decoder-leaf-2026-11-22.md`; the report in `answer.md` carries
   the same numbers. Neither file edits existing repository code or docs, so the strict owned
   source set is unaffected.
7. **Fix round r1 (2026-11-22, independent review).** Three findings were fixed before the
   verification handoff: (a) `pi`/`omo` lost the base's bound-before-copy ordering — the leaf now
   has the `ChunkFirst` axis and a pull arm that compares the pending length before copying
   (P1-1); (b) no row pinned "records completed before a bound failure are returned" — the leaf
   gained `TestFeedReturnsRecordsCompletedBeforeTheBoundFailure` and the dump corpus gained the
   `good-then-over-bound` arms (P2-1); (c) `Fatal` without `Latch` used to drop the suffix and
   forget the failure — a fatal record now always fails the stream permanently, as its doc says
   (P3-1). The review also exposed that no row pinned the *latch wiring*: one new row per wrapper
   (`pi`/`omo` retain the bound error across later `Feed`/`Close`; `claude`/`codex` resume from a
   fresh record), each shown red by its own mutant (mutations f1/f2).

8. **Fix round r2 (2026-11-22, review reconcile).** The four findings of the frozen-increment
   review are closed: the §1 count sentence is scoped to the four contract decoders with the
   tree-wide residuals named (REV-01); a zero-value wrapper now frames with the base's zero bound
   via `newReader(0)` on demand, pinned by four new adapter rows and mutation (i) (REV-02); the
   evidence's aggregate hash is the reproducible committed delta
   `git diff 166e3a05 4443791d -- daemon` (REV-03); the `decode*` and bound-check coordinates are
   corrected (REV-04).

9. **Fix round r3 (2026-11-22, review round 2).** The second review's findings are closed. P2-1:
   the non-latch buffer-arm overflow dropped the pending chunk, and the interim `ChunkFirst` axis
   was the only thing hiding it (with `ChunkFirst: false` the chunk vanished); the arm now appends
   the pending chunk (`append(buffer[lineEnd+1:], chunk...)`), the axis is deleted from the policy
   and both wrappers stop setting it, and the branch is pinned by the new `claude`/`codex`
   retained-overflow rows (mutations (j)/(k); the §6.1 differential is byte-identical). P3-1: the
   comment-policy cleanup (leaf header/field/method prose, the four `readerOrZeroBound` comments,
   the "want the base …" messages — only the `Decode` buffer-view hazard line remains). P3-2: the
   plan and the evidence record are restated as committed revisions with symbol citations. P3-3:
   the `pi`/`claude` terminated-line bound rows (mutation (l)). P3-4: the advisory lint findings
   are fixed — the QF1001 de Morgan rewrite, the `pi` errorlint rows and the additional
   `linejson` `err !=` comparison the `--new-from-rev=origin/main` run flags become `errors.Is`
   checks (no coverage lost: the sentinels are returned by construction and the differential
   records `%T`/text/`errors.Is`/`==` for every error), and `GOOS=linux golangci-lint run
   --new-from-rev=origin/main ./internal/harness/...` reports `0 issues.` (evidence §7).

## 10. Files this slice writes

Source: `daemon/internal/harness/linejson/decoder.go` (new), `pi/rpc.go`, `omo/rpc.go`,
`claude/stream.go`, `codex/framing.go` (framing blocks only).
Tests: `daemon/internal/harness/linejson/decoder_test.go` (new), and the additive rows of §6 in
`pi/rpc_test.go` (latch retention, CR-over row), `omo/rpc_test.go` (CR bound, blank lines, latch
retention), `claude/stream_test.go` and `codex/framing_test.go` (non-finite acceptance, resume
after a bound failure, r3 retained-overflow row). No existing assertion is deleted; the r3 lint
fix converts four `err != sentinel` comparisons to `errors.Is` (identity is still pinned by the
sentinels' construction and the differential), and the pre-existing CR-allowance and
duplicate-walk rows are unchanged.
Docs: this plan, `docs/evidence/rf-w4-15a-jsonl-decoder-leaf-2026-11-22.md` (the judging artifact).
