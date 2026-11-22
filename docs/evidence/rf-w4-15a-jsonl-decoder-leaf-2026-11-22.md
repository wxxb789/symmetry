# rf-w4-15a JSONL decoder leaf — evidence record (2026-11-22, r1 fix + simplify apply, r2 review fix, r3 review-round-2 fix)

Contract: `goal-rf-w4-15a-jsonl-decoder-leaf.md` (GDD state root). Plan:
`docs/plans/2026-11-22-rf-w4-15a-jsonl-decoder-leaf-plan.md`.
Baseline revision: `de526861313b05908c95d21dc1902a516c7ef15a` (origin/main after #217 W3-15; the
#187 W3-04 gate is merged too, so no rebase was needed). The slice is committed on
`refactor/rf-w4-15a-jsonl-decoder-leaf`: apply `7f825a3f`, main merge `d81d6e80`, DLP scrub
`5a67e341`; the r3 fix round is the `fix(harness): keep the pending chunk on decoder overflow`
revision on top of `5a67e341` (this revision, committed). The differential baseline is origin/main
`e84973a7`; the four wrapper files are byte-identical at `de526861`, `b25e34ae` and `e84973a7`.

An independent read-only review ran at the first freeze; its P1-1 (bound-before-copy ordering),
P2-1 (missing row for records returned before a bound failure) and P3-1 (`Fatal` without `Latch`
forgot a failure) findings are fixed in this revision, and the review's missing-evidence findings
(no WSL/mutation record, stale hashes) are closed by this file. A report-only simplify review then
ran on `cc0469df`; its accepted findings are applied in this revision (two comment deletions in
the leaf, the `omo` bound-text const, the `Fatal`/`Latch` row) and the record rows were re-measured
at these bytes. A second report-only review then ran on the frozen `cca92284..4443791d` increment;
its four findings (REV-01 count scoping, REV-02 zero-value parity, REV-03 aggregate-hash
reproducibility, REV-04 anchor numbers) are fixed in the r2 bytes below, the zero-value arm was
added to the differential harness (`<HOST-PATH>/tmp/w4-15a-fix2/zerodiff_test.go`, dumps beside it), and
every measured row was re-measured here. Every number below was produced on the revision these
hashes bind (the §9 rows (a1)–(i) name their earlier revision); the r1/r2 throwaway harnesses were
kept at `<HOST-PATH>/tmp/w4-15a-diff/decoderdiff-dump_test.go` and
`<HOST-PATH>/tmp/w4-15a-fix2/zerodiff_test.go` with their dumps beside them.

A second independent review ran on `5a67e341` (range `origin/main...5a67e341`); its findings are
fixed in this r3 revision. P2-1: the non-latch buffer-arm overflow dropped the pending chunk
(observable with `ChunkFirst: false`, and no test pinned the branch) — the arm is now
`decoder.buffer = append(decoder.buffer[lineEnd+1:], chunk...)` and the interim `ChunkFirst` axis
is deleted, with the 60 000-case differential byte-identical (§6) and two new adapter rows
(mutations (j)/(k)). P3-1: the package header, `Policy`/`Decoder`/`New`/`Feed`/`Close` doc prose,
the four `readerOrZeroBound` comments and the "want the base …" test messages are removed or
reworded to the behaviour (only the `Decode` buffer-view hazard line remains). P3-2: the
revision-state and §1 anchor claims are corrected (symbol citations). P3-3: `pi` and `claude`
gain the terminated-line bound rows (mutation (l)). P3-4: the lint findings are fixed, including
the `linejson` `err !=` comparison the `--new-from-rev` run flags, and the advisory run reports
0 issues (§7).

Subject hashes (this record binds exactly these bytes; every mutation run was reverted and the
hashes re-verified afterwards):

| File | sha256 |
| --- | --- |
| `daemon/internal/harness/linejson/decoder.go` | `611a65ca21242ca419887df3650699be097217103e82380fdbb118d14767a859` |
| `daemon/internal/harness/linejson/decoder_test.go` | `865d11a28e89a6a6715336271e9ed859e14bbcb6f8ec78ee5aa5d1f9d7d8c0d7` |
| `daemon/internal/harness/pi/rpc.go` | `c9236dac36a9bcc19bf8cd3742bce068589bcee4c5cd62c80cfbb8a05df60066` |
| `daemon/internal/harness/omo/rpc.go` | `1e706bb27c87a3dd424eed0473518806d5a6d1f001747a238ea8b539ad0d45c1` |
| `daemon/internal/harness/claude/stream.go` | `8c3267ec8ecf1d1eaf74a016c6849abd51748827a3388ef791caf4de6602145a` |
| `daemon/internal/harness/codex/framing.go` | `1dbd89868fc7c4ef9e2d44fc192a536e2f4f4bc904e70af4c4b0584854d10b0e` |
| `daemon/internal/harness/pi/rpc_test.go` | `af4fb8efd2416350f854c8665e41d0e6b95aeae770952a6f7b67a14a7ecdbb94` |
| `daemon/internal/harness/omo/rpc_test.go` | `67f20a7538dc49cf6bf34fdd01889d1866bdc5bc9d5c9de64d4b62ff24ff231f` |
| `daemon/internal/harness/claude/stream_test.go` | `9c2ec2fcfa5814ad147e13438f8d423b2891bd2b4a3d222d7299535fe67f5616` |
| `daemon/internal/harness/codex/framing_test.go` | `4ac13587e07d7fb3a5acbbb81d609c59b6502c992714d855e93af7c16edefac4` |

The subject bytes are the table above. The committed apply-stage Go delta over `166e3a05`
(`git diff 166e3a05 4443791d -- daemon | sha256sum` =
`f3dc3c0f5dbfadfbb996188327bb3b40eb74067012433366f189ede8fbc3c4cf`) and the r1/r2 revisions are
history; the r2 fix-round delta was committed as `4443791d`. The r3 fix delta on top of `5a67e341`
is reproducible at this revision as `git diff 5a67e341 HEAD -- daemon | sha256sum` =
`a22c773881035af6bf049f51c501f9307d42818b9186e22817e4481c5232a0d1` (commit-to-commit, `a/`/`b/`
prefixes). The worktree comparison `git diff 5a67e341 -- daemon` prints `c/`/`w/` prefixes under a
`diff.mnemonicPrefix` config and hashes
`32baa5e2b893426f7a99c36e7a02b0b187a57da05d86ad25faf437ee18ce1ba8`; the subject bytes of both
comparisons are this table.

## 1. Definition counts (clause 1)

Commands run at the base revision (`git show de526861:<path> | grep -c <pattern>`) and on the
working tree (`grep -c <pattern> <path>`), non-test sources only:

| grep | pi/rpc.go | omo/rpc.go | claude/stream.go | codex/framing.go | linejson/decoder.go |
| --- | --- | --- | --- | --- | --- |
| `IndexByte` (the LF scan) | 1 → 0 | 1 → 0 | 1 → 0 | 1 → 0 | 0 → **2** |
| `buffer = append` (buffering) | 2 → 0 | 1 → 0 | 1 → 0 | 1 → 0 | 0 → **4** |
| `> (decoder.max\|decoder.limit\|framer.max)` (bound checks) | 2 → 0 | 1 → 0 | 2 → 0 | 2 → 0 | — |
| bound decisions in the leaf | — | — | — | — | `Feed`'s no-LF guard (`len(buffer)+len(chunk) > MaxBytes`), the two `overBound` calls in `Feed`, and the `overBound` predicate |

The leaf's two `IndexByte` sites scan the buffer and the incoming chunk; its four
`buffer = append` sites are the unterminated tail, the two non-latching suffix retentions (the
incoming-chunk arm and the buffered-line arm, the latter added by the r3 fix) and the pull-arm
line copy.

Files containing a bounded-line framing loop (LF scan + bound + blank policy + close): **4 → 1**
within the contract's scope — the four wrapper decoders plus the leaf (`linejson/decoder.go`).
Tree-wide that shape survives outside the scope and the count is not 1: `omp/protocol.go`'s
`frameDecoder.feed` (:116) scans LF (:118), bounds on `maxFrameBytes` (:120/:126), rejects an empty
frame (:133) and has a `close` (:226); `opencode/sse.go` (:32 `Feed`, LF scan :41, bound :43/:49,
`Close` :71) is the same shape. `omp/**` is outside this slice's owned set and `opencode/**` is
E3/D1-excluded, so neither is in the clause-1 count — residual, not claimed. The four wrappers
keep `NewDecoder`/`NewFramer`/`Feed`/`Close` and the same record types, and the `decode*` record
functions are untouched (`decodeRecord` pi:288→246, omo:158→128; `decodeEvent` claude:151→134;
`decodeFrame` codex:120→101 — identical bodies).

## 2. Per-adapter behaviour table (clause 2)

| | `pi` | `omo` | `claude` | `codex` |
| --- | --- | --- | --- | --- |
| duplicate-key policy | explicit walk, `ErrDuplicateField` + key | explicit walk, `invalid OmO RPC record: duplicate JSON member` | none of its own; `jsontext.Value.IsValid` rejects duplicates (`ErrMalformedJSON`) | none of its own; `jsontext.Value.IsValid` (`malformed_json` diagnostic) |
| non-finite numbers | accepted | accepted | accepted (`usage` stays raw) | accepted |
| bound timing | pending length compared before its bytes are copied | same | same (the base copied the whole chunk first; r3 keeps the pending chunk on overflow) | same |
| CR | `CRAllowance` (MaxBytes+1 when the byte before LF is CR) + `TrimCR` | ordinary byte, counts toward the bound, reaches `decodeRecord` | ordinary byte | ordinary byte |
| blank lines | `SkipBlank`: skipped, no sequence | delivered (a blank line is `ErrInvalidRecord` and fails the feed) | skipped | skipped |
| record-decode failure | in-band `Record.DecodeError` | `Fatal`: the bad record is returned and the feed fails permanently | in-band `Event.DecodeError` | in-band diagnostic frame |
| latch | `Latch`: the bound failure is reported from every later call | latched | none | none |
| overflow state | latched, suffix dropped | latched, suffix dropped | post-LF suffix and pending chunk retained for the next call | post-LF suffix and pending chunk retained |
| no-LF overflow | buffer dropped | buffer dropped | buffer dropped | buffer dropped |
| close shape | `([]Record, error)`: one `Record{DecodeError: ErrIncompleteRecord}` + error | `error` only | `([]Event, error)`: one `invalidEvent` + error | `([]Frame, error)`: one `incomplete_frame` diagnostic + error |
| close blank rule | whitespace-only suffix empty | any suffix byte is unterminated | whitespace-only suffix empty | whitespace-only suffix empty |
| sentinels | `ErrRecordTooLarge`, `ErrIncompleteRecord` | wrapped `ErrInvalidRecord` (2 texts) | `ErrRecordTooLarge`, `ErrIncompleteRecord` | `ErrFrameTooLarge`, `ErrIncompleteFrame` |
| nil receiver | `pi rpc decoder is nil` | panics (nil deref) | `Claude stream-json decoder is nil` | `codex framer is nil` |
| zero-value construction | `Feed` → the zero-bound error, `Close` → the same latched error; same at HEAD | `Feed` → `invalid OmO RPC record: JSONL record exceeds limit` (latched through `Close`); same at HEAD | `Feed` → `Claude stream-json record exceeds limit`, `Close` → nil; same at HEAD | `Feed` → `codex app-server frame exceeds limit`, `Close` → nil; same at HEAD |

A zero-value wrapper is not constructed in the tree (no composite-literal or zero-value variable
site), and its base framed with a zero bound (errors above). The r2 fix keeps that behaviour: the
wrapper builds the leaf on demand with `MaxBytes` 0, so the zero value frames exactly as its base
did instead of panicking on the nil `reader`. Proof: the zero-value dump of §6 is byte-identical
base vs HEAD (the same harness panicked at `4443791d` before the fix), and mutation (i) reddens the
four new adapter rows.

Parameter that reproduces each base difference: CR → `CRAllowance`/`TrimCR`; blank policy (lines
and close) → `SkipBlank`; failure policy → `Latch` + `Fatal`; bound value → `MaxBytes`; error
identity → the `TooLarge`/`Incomplete` constructors; close record → built by each wrapper from
`Close`'s `(sequence, pending, err)`. The `claude`/`codex` bases copied the whole chunk before the
bound decided; the leaf compares each line's length before its bytes are copied and keeps the
pending chunk on overflow (r3), which the 60 000-case differential cannot distinguish from the
base (§6); the r1/r2 interim `ChunkFirst` axis was deleted in r3 (§9 (j)/(k), §11). The
duplicate-key and non-finite policies remain inside each adapter's `Decode` callback (`decode*`
bodies unchanged); the leaf adds neither.

Two deliberate, unobservable differences:

- `Close` advances the shared sequence number whenever a pending suffix exists. `omo`'s base
  `Close` advanced nothing and returned no record; the difference is invisible (`omo` latches the
  failure and never emits that record or a later sequence), and the dump's `close`/`post` arms
  confirm it (both sides byte-identical).
- Copy ordering: the leaf checks each line's bound before copying its bytes, while the
  `claude`/`codex` bases buffered the whole chunk first. With the r3 overflow arm keeping the
  pending chunk, no returned record, error or state differs — the 60 000-case differential is
  byte-identical for all four adapters (§6) — and the rewritten arm is pinned by the two
  retained-overflow adapter rows (mutations (j)/(k), §9).

## 3. Base→HEAD control-flow audit (conductor decision 2026-11-21, lesson 32)

Every base branch of the four deleted framing blocks, against the statement that reproduces it.

`pi/rpc.go` (base lines):

| Base | Branch | HEAD | Preserved |
| --- | --- | --- | --- |
| 221-223 | nil receiver → `pi rpc decoder is nil` | `pi.Decoder.Feed` guard (unchanged text) | yes |
| 224-227 | latched failure → `nil, failed` | `linejson.Decoder.Feed` failed check | yes |
| 228-235 | loop guard + no-LF bound `len(buffer)+len(chunk) > max` → `fail(ErrRecordTooLarge)` **before** `append` | leaf pull arm: the same length is compared before the bytes are copied; `Latch: true` clears the buffer | yes |
| 237-240 | complete-line bound + CR allowance, before the line bytes are appended | leaf pull arm `overBound(lineBytes, trailerBeforeLF(buffer, chunk, chunkEnd))` | yes |
| 241-249 | append, split, `buffer = nil`, CR strip, blank skip | leaf line consumption + `TrimCR` + `SkipBlank` | yes |
| 250-253 | sequence++, `decodeRecord` | leaf `Decode: decodeRecord` | yes |
| 258-273 | close: nil guard, latched check, blank rule, sequence++, record + `fail` | leaf `Close` + wrapper record build + `Latch` | yes |
| 275-279 | `fail`: clear buffer, latch | leaf `fail` (`Latch`) + per-arm buffer handling | yes |
| 281-286 | `hasTrailingCRBeforeLF` | `trailerBeforeLF` + `overBound` | yes |

`omo/rpc.go`: 114-125 bound before append + latch; 126-139 decode, record append, first decode
error fatal; 147-156 close (any non-empty buffer unterminated, no record). HEAD: leaf loop with
`Latch: true`, `Fatal: record.DecodeError`, `SkipBlank: false` and the bound compared before the
copy; wrapper `Close` returns the leaf's error only.

`claude/stream.go`: 106-135 (`buffer = append` first, no-LF bound clears the buffer, complete-line
bound keeps the suffix, blank skip, no latch); 137-149 close builds `invalidEvent`. HEAD: leaf loop
with `SkipBlank: true` and no latch; the non-latch overflow arms keep the post-LF suffix and the
pending chunk (r3 — the base kept them by appending the whole chunk first); wrapper builds the
same `invalidEvent`.

`codex/framing.go`: 75-104 as claude, diagnostics in-band; 106-117 close builds
`decodeDiagnosticFrame(..., "incomplete_frame", "stream ended before newline")`. HEAD: leaf loop
as claude with `Decode: decodeFrame`; wrapper builds the same diagnostic from `pending`.

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
`git diff`). The r2 fix adds two unexported helpers to each wrapper (`newReader`, which builds the
policy, and `readerOrZeroBound`); no exported declaration or signature changed. The r3 fix changes
the leaf's non-latch overflow arm and deletes the `ChunkFirst` policy field; no wrapper surface
changes.

## 6. Differential corpus dump (base vs head)

### 6.1 r3 random differential (this revision)

Throwaway harness `<HOST-PATH>/tmp/w4-15a-r3/harness/main.go` (outside the repo), compiled into a
scratch copy of the differential baseline (`git archive origin/main daemon` → `e84973a7`) and one
of the r3 worktree (`git ls-files -z daemon | tar -T -`), each with the harness under
`daemon/cmd/w4decoderdiff`. `DIFF_OUT=<dir> go run ./cmd/w4decoderdiff` writes one dump per
adapter; `cmp` byte-compares base against head.

Corpus: 60 000 deterministic cases per adapter (`math/rand/v2` PCG seed `0x20261122`/`0x15a`; 19
bounds — 0, 1…17, 32, 64, 1024, 1 MiB, -1, -7 — plus a 1-in-16 zero-value-wrapper arm; 1–8 pieces
per case from an alphabet including CR/LF/CRLF, blank/whitespace lines, JSON envelopes, duplicate
keys, `1e1000`, invalid UTF-8, U+2028 (LINE SEPARATOR), bare `{`/`}`/`"`/`\`; random splits into 1–4 chunks; a
1-in-5 nil chunk; a 1-in-3 mid-stream `Close`), then `Close`, post-close `Feed("z\n")` and
post-close `Close`. Every call dumps each returned record field-wise (pointers dereferenced — no
addresses) plus the `%T`, text, `errors.Is` and `==` of every returned error and `DecodeError`
against all exported sentinels of the adapter.

| Adapter | Cases | Dump bytes | Dump lines | sha256 (base = head) |
| --- | --- | --- | --- | --- |
| `pi` | 60 000 | 62 687 083 | 505 160 | `c675aedda32866c746dd00c57926ebda6f4fba36fc2963601b6a7c85b1f913dd` |
| `omo` | 60 000 | 52 883 936 | 466 474 | `cdffce581a0957db23cd0e4b198c68a120f4ec7bf3589fe3bc804764fd7838de` |
| `claude` | 60 000 | 69 399 593 | 579 676 | `084a5e4f5d1709f318886715c6f047ebbbbaa58828c5791b3d20091ed158fa51` |
| `codex` | 60 000 | 49 837 805 | 579 300 | `a0055ff14ab7ef3b6b9b19aa32594e18db200c2eed89c2f0aeb8f5404f9aa41c` |

`cmp` reports no difference for any adapter. The same harness on a scratch tree with the r3 mutant
m5 (non-latch overflow arm → `buffer = nil`) changes only the `claude`/`codex` hashes
(`6fda750fc7fb408eb6df57b46c0f3bc0229b0325b57cb2ee2401d7b635603bed` /
`95f404841c2e450a1e3a83aaeb9f5abe32cae2df8f00f354b631b99647b8752e`), and with the fix reverted
(m6, arm → `buffer = buffer[lineEnd+1:]`) only those two change
(`c7ad084d3c728dd3669d8dd8c7b560b88c476d58d56f504075a2a275a66f9c0c` /
`fbe20d1690017f204d11ccfa4e66b85fb0f565480c06e960376d19b917f56836`); `pi`/`omo` latch and drop
the suffix, so the corpus exercises the rewritten arm and notices when it is wrong.

### 6.2 r2 dumps (history; bind the r2 revision)

The r2 harness `daemon/internal/harness/decoderdiff` (deleted from the tree after the run; was kept
at `<HOST-PATH>/tmp/w4-15a-diff/decoderdiff-dump_test.go`, no longer present on the host at r3) ran
against a `git worktree` of `de526861`: for each of 864 cases it feeds one or more chunks to all
four decoders, then `Close`, then a post-close `Feed("z\n")`, and writes every record (sequence,
raw bytes, type/envelope fields, decode error text) and every error text. The rows below bind the
r2 bytes.

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

`cmp` reports no difference, and the working-tree dump was re-run at the simplify-apply revision
with the same harness and corpus: same sha256, still no difference. Three arms worth naming
explicitly, all byte-identical on both sides:

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

A third dump feeds the same 864-case corpus to zero-value decoders (`var d pi.Decoder`,
`omo.Decoder`, `claude.Decoder`, `codex.Framer`; arms labelled `pi-zero`/`omo-zero`/`claude-zero`/
`codex-zero`) and is byte-identical base vs HEAD: sha256
`fbf7e887f287d8e00d6ee4b5639c19dc691e6154425c64632e60c5459998bfab`, 1 933 622 bytes. The same run
panicked at `4443791d` (nil-pointer dereference in `linejson.(*Decoder).Feed`), which is the
behaviour the r2 fix removes.

The longest fixture line is 3 734 bytes (`testdata/pi/1.5.2/lifecycle.jsonl`) against the 1 MiB
default bound, so the fixture replays cannot exercise the bound arms; the synthetic corpus above
does.

## 7. Gates (all foreground; exit codes as printed; re-run at the r3 fix revision)

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l` over the ten changed Go files | empty, exit 0 |
| vet | `cd daemon && go vet ./...` | exit 0 |
| build windows | `GOOS=windows go build ./...` | exit 0 |
| build linux | `GOOS=linux go build ./...` | exit 0 |
| cross vet | `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...` | exit 0 |
| cycle proof | `go build ./internal/harness/...` | exit 0 |
| import invariant | `go list -deps ./internal/harness/claude \| grep internal/harness` | `…/harness/linejson`, `…/harness/claude` only — no root `harness` |
| windows suite | `cd daemon && go test -count=1 -timeout 900s ./internal/harness/...` | 9/9 packages `ok` (root 13.498s, claude 0.108s, codex 13.434s, linejson 0.089s, omo 1.990s, omp 2.747s, opencode 0.499s, pi 4.751s, probeconformance 7.169s), exit 0 |
| lint (advisory) | `GOOS=linux golangci-lint run --new-from-rev=origin/main ./internal/harness/...` | `0 issues.`, exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host path | the r3 diff (`5a67e341..HEAD`, code + docs) filtered for a profile-directory separator | 0 hits |
| suppressions | new `//nolint`/`credo:disable` directives in the r3 Go diff | 0 |

## 8. WSL Linux run

Re-run at the r3 bytes (`wsl -d Ubuntu-24.04`, tree under test =
`<WORKTREE>/daemon`): `go test -count=1
./internal/harness/linejson ./internal/harness/omo ./internal/harness/claude
./internal/harness/codex ./internal/harness/probeconformance` → 5/5 `ok` (0.016s / 1.203s /
0.139s / 13.089s / 7.047s), `CORE5_OK`; `go test -count=1 -skip "TestProviderBridge"
./internal/harness/pi` → `ok 0.944s`, `PI_OK`. The whole-tree invocation below was run at the
r1/r2 revisions; its loopback failures are distro-level (no failing test touches a decoder) and
were not re-run at r3.

`wsl -d Ubuntu-24.04 -u accept`, tree under test = this worktree
(`<WORKTREE>/daemon`), re-run at the simplify-apply
revision. Whole-tree invocation `go test -count=1 -timeout 150s ./internal/harness/...` (log at
`<HOST-PATH>/tmp/w4-15a-apply/scratch/wsl-tree-150.txt`; the 150 s package timeout bounds the loopback
stalls):

| Package | Result |
| --- | --- |
| root `harness` | `ok 11.724s` |
| `claude` | `ok 0.254s` |
| `codex` | `ok 13.815s` |
| `linejson` | `ok 0.035s` |
| `omo` | `ok 1.805s` |
| `probeconformance` | `ok 7.045s` |
| `omp` | `FAIL 33.215s` — `TestNativeAcceptanceCancelToolOracle` |
| `opencode` | `FAIL 150.081s` — `TestNativeOpenCodeRealConfigUsesLoopbackObserverEndpoint` and nine `TestClient*` tests, then the package timeout |
| `pi` | `FAIL 150.047s` — `TestProviderBridgeRejectsMissingOrWrongNonceBeforeExecutor` and the remaining `TestProviderBridge*` family at the package timeout |

12 `--- FAIL` test lines, 2 package-timeout panics, 4 visible `dial tcp 127.0.0.1:<port>: i/o
timeout` lines (the r1 default-timeout run showed the same structure at `opencode 1200.061s` /
`pi 427.482s`). Observed exit status: **1** (`tree_rc=1`, captured outside the distro).

Every failing test is loopback/IPC-dependent and cannot be caused by this slice: the distro cannot
complete a loopback TCP connect at all (a `python3` bind/listen/connect probe on `127.0.0.1` times
out on `connect`, `<HOST-PATH>/tmp/w4-15a-apply/scratch/loopback_probe.py`), the failing tests are HTTP
tests that never touch a decoder (`pi/provider_bridge_test.go`'s only `.Close()` /
`json.NewDecoder` matches are HTTP bodies), and a representative failing pi test fails identically
at the base revision in a `de526861` worktree in the same distro (`FAIL …/pi 150.227s`, the same
30 s dial timeouts; r1 run).

Decoder-relevant WSL surface with an exact exit code (re-run at the r2 fix revision; the
whole-tree loopback failures above were not re-run — they are environment-level and no failing
test touches a decoder): `go test -count=1 ./internal/harness
./internal/harness/claude ./internal/harness/codex ./internal/harness/linejson ./internal/harness/omo
./internal/harness/probeconformance` → 6/6 `ok` (10.999s / 0.046s / 12.672s / 0.010s / 0.918s /
7.031s), `core_rc=0`. `pi` without its provider-bridge tests (`-skip "TestProviderBridge"`) →
`ok 0.864s`, `pi_rc=0` (the package line itself is the decoder suite plus the validator/state
tests).

`omp`, `opencode` and `pi` with the loopback families skipped (`-skip
"TestProviderBridge|TestNativeAcceptanceCancelToolOracle|TestClient|TestNativeOpenCode"`):
`omp ok 1.803s`, `pi ok 0.954s`, `opencode FAIL 150.042s` —
`TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest` plus
`TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest` at the package timeout. `opencode` is
outside this slice (E3/D1) and its failure is the same dial stall.

## 9. Mutation runs

Each mutant was applied to the frozen tree, the named suites were run, and the mutant was reverted
(`sha256sum -c` against the §0 table shows only the files that legitimately changed afterwards).
Mutants (a1), (a2) and (b) were re-applied and re-run after the final comment-only clarification in
`linejson/decoder.go`, so every leaf mutation is bound to the §0 bytes; (c), (d) and (e) ran before
the latch rows were appended to the adapter suites, and each reddened a row that is byte-identical
in the frozen files. Rows (a1)–(g) ran at the pre-apply revision (the fix-round-r1 bytes) and
remain red rows; the leaf was rewritten again in r3 but the rows they reddened are unchanged.
Mutant (h) ran at the apply revision and (i) at the r2 fix revision; rows (j)–(l) ran at the r3
bytes, and every mutant was reverted before the §0 hashes were re-verified.

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
| (g) copy-ordering axis flipped (r1/r2) | `pi` `ChunkFirst: true` | **green** at the r1/r2 bytes, as predicted and documented: the axis had no behavioural row. The axis was deleted in r3; the branch it guarded is now pinned by (j)/(k) |
| (h) `Fatal` latch re-gated on `Latch` | `decoder.failed = err` moved under `if decoder.policy.Latch` in the `Fatal` arm | red: `linejson TestFeedFatalRecordIsReturnedAndFailsTheStream/latch=false` (the new arm); `omo` stays green because it sets `Latch: true`, and (e)/(f1) do not cover this case |
| (i) drop the zero-value fallback | each wrapper's `readerOrZeroBound` returns the nil `reader` without building the zero-bound leaf | red: the four new rows — `pi`/`omo`/`claude` `TestDecoderZeroValueKeepsTheZeroBound` and `codex` `TestFramerZeroValueKeepsTheZeroBound` (nil-pointer panic in `linejson.(*Decoder).Feed`); the leaf's `TestFeedZeroBoundAdmitsOnlyAnEmptyLine` stays green (no wrapper involved) |
| (j) r3: drop the retained suffix on overflow | buffer-arm non-latch `append(decoder.buffer[lineEnd+1:], chunk...)` → `decoder.buffer = nil` | red: `claude TestDecoderKeepsThePendingChunkOnRetainedOverflow` and `codex TestFramerKeepsThePendingChunkOnRetainedOverflow` (only these two); the §6.1 dump changes for `claude`/`codex` only |
| (k) r3: drop the pending chunk (revert of the fix) | buffer-arm non-latch → `decoder.buffer = decoder.buffer[lineEnd+1:]` | red: the same two rows only; the §6.1 dump changes for `claude`/`codex` only |
| (l) r3: disable the complete-line length check | `overBound` → `return false`, the unterminated `len(buffer)+len(chunk) > MaxBytes` guard kept | red: `pi TestDecoderTreatsCRLFAsTheSameBoundedRecordAndRejectsNestedDuplicateFields` (the new CR-over row), `claude TestDecoderRejectsOversizeAndTruncatedRecords` (the new complete-line row), both r3 retained-overflow rows, and the pre-existing `linejson`/`omo`/`claude`/`codex` bound rows |
| duplicate-walk row (base-pinned, unchanged code) | — | `pi TestDecoderTreatsCRLFAs…` already reddens when the walk is dropped (reviewer-verified); `omo TestDecoderRejectsAmbiguousOrUnboundedJSONL` likewise |

## 10. LOC delta (report-only, D2)

`git diff --numstat de526861 -- daemon/internal/harness` at the r3 bytes: non-test sources 229
insertions / 165 deletions — the four wrappers at 99/165 (`pi` 25/56, `omo` 23/42, `claude`
26/33, `codex` 25/34) plus the new `linejson/decoder.go` (130 lines) = **net +64 non-test lines**.
Tests: 439 added lines, 0 deleted — the four adapter suites (`pi` 34, `omo` 58, `claude` 51,
`codex` 45) + `linejson/decoder_test.go` (251 lines). Docs: the plan (336 lines) and this record
(443 lines). The contract's `−600…−800` figure covers the whole 5b (driver half included) and is
report-only (D2).

## 11. Residual risks and known limits

- `pi`'s `strictObject`/`omo`'s `scanJSONValue` (and their non-decoder call sites) stay separate:
  the two walkers carry distinct error identities and are not part of the framing duplication.
- The leaf's `Decode` callback receives a view into the decoder buffer and must not retain it; all
  four adapters copy what they keep (verified by reading `decodeRecord`, `decodeEvent`,
  `decodeFrame`, and `omo`'s `json.Unmarshal` into `json.RawMessage`).
- `omo`'s `Close` never exposes the sequence number: the leaf advances it whenever a pending suffix
  exists, but `omo` latches the failure and returns no record, so the difference is unobservable,
  recorded in §2.
- The `claude`/`codex` bases copied the whole chunk before the bound decided; the leaf checks each
  line before copying and keeps the pending chunk on overflow (r3). That branch is pinned by the
  two retained-overflow adapter rows (mutations (j)/(k) redden exactly those and nothing else) and
  by the §6.1 differential, which changes only for `claude`/`codex` under those mutants. The
  r1/r2 interim `ChunkFirst` axis had no behavioural row (mutation (g) was green) and was deleted
  in r3.
- The differential dump compares the decoder surface, not the adapters above it; the adapter
  suites and the fixture replays are the oracle for those layers.
- Native acceptance suites that `t.Skip` without credentials are reported as skipped in the run
  output, not as failures.
