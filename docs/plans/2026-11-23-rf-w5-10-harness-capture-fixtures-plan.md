# rf-w5-10 harness capture fixture consolidation (TC-M, D10) — plan

Contract: `goal-rf-w5-10-harness-capture-fixtures.md` (GDD state root, external to the repository),
with its program-wide conductor rules, the D10/D1 rulings, the 2026-11-21 lesson-32 ruling, the
2026-11-22 comment policy, the 2026-11-23 lessons 55-56 amendment and the 2026-11-23 ruling
(Option A: the two stale claude READMEs). Base revision:
`2c8c419e886df1407436647fd53296eb79cba1e7` (W4-13 / PR #267 merged; the contract's only open
dependency). Branch: `refactor/rf-w5-10-harness-capture-fixtures`. Host: Windows x64, `go1.27.4`;
Linux rows run Ubuntu-24.04 over the same worktree (`wsl -d Ubuntu-24.04 -u accept`, `go1.27.0`).
Windows and WSL suites never run concurrently.

## 1. Scope

Owned (STRICT): `daemon/internal/harness/testdata/**` (the retirement of 32 files plus one new
`README.md` recipe file); the nine reader test files the contract lists, edited only where a reader
repoint is needed — none is needed in this slice, so all nine stay byte-identical; `docs/TODO.md`
and `docs/archive/CLOSED-TODO.md` for the one closed item; this plan. Out of scope (untouched):
production harness/adapter code, `docs/runbooks/**`, `docs/design/harness.md`, `daemon/internal/app/**`,
`testdata/opencode/**` and `opencode/*_test.go` (D1), app-side version strings, any capture
collection.

## 2. What the contract leaves unresolved, and the decisions taken

P1. **Landing shape — retirement-only, no delta.** The measured delta candidates are the three
non-manifest older kept directories: claude `2.1.322` (4 files / 4,131 bytes, read by
`claude/stream_test.go:13`), codex `0.167.2` (9 / 20,509, unread) and pi `0.87.1` (5 / 8,726,
unread). Per the R0 deletion test a per-directory delta that is pure data restores a plain read and
re-spreads no invariant, so no delta helper is admitted and no delta lands; conditional mutant
row 9 is recorded inapplicable with the inventory showing no delta files. The failure-control
sentence "a delta applied to a digest-bound directory" has no subject in this landing.

P2. **Where the judging artifact's tables live.** The contract names `answer.md` (the GDD stage
work dir, outside the repository). The plan file is in the owned set and carries the same inventory,
pin, reader, census and mutant tables so the record is verifiable from the tree; `answer.md` mirrors
them with the gate transcripts.

P3. **Recipe home and content.** `daemon/internal/harness/testdata/README.md` (the one new file the
contract names): layout, retention rule (two most recent per harness, plus digest-bound manifest
directories, design-pinned directories and unique test-read captures), the add-a-version recipe
(copy the canonical `TestedVersion` directory; keep its `version.txt`, README and manifest; update
the adapter `TestedVersion` constant and the `docs/design/harness.md` pin; run the package tests;
record the accepted capture), the optional per-directory delta admission (recorded R0 justification,
non-manifest and non-frozen directories only, reader repointed in the same change, decoded-frame
equality re-proven), and the frozen set (opencode `2.3.0`, claude `2.1.281` captures, every
`fixture-manifest.json` / kept `schema-manifest.json` directory).

P4. **Claude README prose edits** (conductor ruling 2026-11-23, Option A). Prose-only edits to
exactly `daemon/internal/harness/testdata/claude/2.1.281/README.md` and
`daemon/internal/harness/testdata/claude/2.1.323/README.md`, correcting the two stale identifiers
(`claudeCandidateStructuredOutputSchema` -> `candidateStructuredOutputSchema`,
`ClaudeTestedExecutableHash` -> `testedExecutableHash`) and no other paragraph. Every capture byte,
`version.txt` and `fixture-manifest.json` in both directories stays byte-identical; the diff gate is
`git diff --name-only <base>...HEAD -- daemon/internal/harness/testdata/claude/2.1.281
daemon/internal/harness/testdata/claude/2.1.323` showing only the two READMEs. The staging
directories' own READMEs say "Historical: superseded by" and "stays in the tree as historical
evidence" — retiring `2.1.259` with its own README and keeping `2.1.322` leaves those sentences
true; the `2.1.281` README's "moved from `../2.1.259/`" provenance sentence stayed in the
implement stage and the 2026-11-23 round-1 ruling replaced the retired path with the bare
version (§8.4).

P5. **TODO bookkeeping.** The item at `docs/TODO.md:95-104` (the claude README reconciliation,
parked on W5-10) moves to `docs/archive/CLOSED-TODO.md` per the repository contract: the item text
is reproduced with its marker, followed by a `**Closed (2026-11-23, branch
\`refactor/rf-w5-10-harness-capture-fixtures\` ...):**` paragraph naming this plan and the two
README blobs; a header note line records the move, matching the file's existing notes.

P6. **Citation gate before commit.** The stage must not commit, and
`scripts/ci/check-citation-existence.py` audits documents from a revision, not the working tree.
Method: stage the working tree into a temporary `GIT_INDEX_FILE` (never the real index), write the
tree, `git commit-tree` a dangling commit with parent `2c8c419e`, and run the gate
`--base 2c8c419e --head <dangling>`. No branch, index or working-tree state is disturbed; the
dangling objects are garbage. The gate audits the whole changed document, so the appended
CLOSED-TODO entry must cite only paths present at head.

## 3. Retirement set (re-derived at `2c8c419e`)

Base `git ls-tree -r --long HEAD -- daemon/internal/harness/testdata`: 114 files / 596,662 bytes,
17 version directories, no root files. Per-directory decision:

| directory | files | bytes | reader at `2c8c419e` | manifest | decision |
| --- | --- | --- | --- | --- | --- |
| codex/0.153.4 | 5 | 13,490 | none | schema-manifest.json | **retire** |
| codex/0.156.1 | 8 | 17,961 | none | schema-manifest.json | **retire** |
| codex/0.167.1 | 9 | 13,716 | none | schema-manifest.json | **retire** |
| codex/0.167.2 | 9 | 20,509 | none (comments in `codex/adapter.go:399`, `codex/probe.go:209`) | schema-manifest.json | keep (one of the two most recent) |
| codex/0.168.0 | 8 | 21,053 | 4 sites (below) | schema-manifest.json | keep (canonical `TestedVersion`) |
| claude/2.1.259 | 2 | 1,428 | none (probe strings `claude/probe_test.go:64,:72` are not reads) | none | **retire** |
| claude/2.1.281 | 7 | 37,368 | `claude/stream_test.go:181` table (3 captures) | none | keep (decision J; README prose edit) |
| claude/2.1.322 | 4 | 4,131 | `claude/stream_test.go:13` | none | keep (two most recent) |
| claude/2.1.323 | 7 | 56,962 | `claude/probe_test.go:418` manifest test | fixture-manifest.json | keep (decision J; README prose edit) |
| pi/0.85.1 | 8 | 30,497 | none | none | **retire** |
| pi/0.87.1 | 5 | 8,726 | none (probe string `pi/probe_promotion_test.go:236`) | none | keep (one of the two most recent; README prose repair) |
| pi/1.5.2 | 10 | 43,980 | `pi/rpc_test.go:16,:91`, `pi/probe_promotion_test.go:297` | fixture-manifest.json | keep (canonical `TestedVersion`; README prose repair) |
| omo/5.1.69 | 5 | 182,762 | `omo/transcript_test.go:59`; comment `omo/probe.go:58` | none | keep (only version; design-pinned) |
| omp/18.20.0 | 10 | 30,611 | `omp/adapter_test.go:961` | fixture-manifest.json | keep (decision J; unread in-repo) |
| opencode/1.18.30, 1.18.32, 2.3.0 | 17 | 113,468 | `opencode/sse_test.go:13,:410` (out of scope) | none | keep (D1, untouched) |

Retired total: codex `0.153.4` + `0.156.1` + `0.167.1` + claude `2.1.259` + pi `0.85.1` =
**32 files / 77,092 bytes** (28.1 % of files, 12.9 % of bytes). Post-state: 82 files / 519,570 bytes
plus the recipe file. The `-40..-60 %` estimate is reported, never a gate (D2-adjacent).
The ruled claude README prose edits remove 12 further bytes from that total (two identifiers
shortened by six characters each); the apply-stage prose repairs change `pi/0.87.1/README.md`
(+174 B), `pi/1.5.2/README.md` (-60 B) and the recipe (+51 B), so the settled retained-tree
total is 82 fixtures / 519,672 bytes + recipe 2,286 = 83 / 521,958 (§8).

Runbook/design-pin check (re-derived at `2c8c419e`): no file under `docs/runbooks/` names any of the
five retired version strings (grep = 0); `docs/design/harness.md` pins codex `0.167.2` (`:31`, kept
by the two-most-recent floor), claude `2.1.323` (`:11,:39,:55`), pi `1.5.2` (`:12,:63,:66`), omp
`18.20.0` (`:13,:124,:142`), omo `5.1.69` (`:14,:77,:78,:81`) and opencode `2.3.0` (`:15,:247`) —
none retired, so no pin blocks the slice and neither document is edited.

## 4. Reader table and the decoded-frame equality method

Base `os.ReadFile(testdata/<harness>/<version>/<name>)` sites and their HEAD equivalent (all stay
plain kept directories; no reader repoint in this slice):

| reader (file:line) | test | version dir | mechanism entry |
| --- | --- | --- | --- |
| `claude/stream_test.go:13` | `TestDecoderReadsFixtureAcrossCRLFAndChunkBoundaries` | claude/2.1.322 | plain directory, kept |
| `claude/stream_test.go:181` | `TestTerminalValidatorReplaysNativeStructuredOutputCaptures` | claude/2.1.281 | plain directory, kept, byte-identical |
| `claude/probe_test.go:418` | `TestClaudeTestdataManifestBindsRecordedDigests` | claude/2.1.323 | plain directory + manifest, kept |
| `codex/framing_test.go:206` | `TestFramerReadsSanitizedVersionFixture` | codex/0.168.0 | canonical `TestedVersion` |
| `codex/framing_test.go:232` | `TestFramerAcceptsRawCodexStartSmokeWithoutJSONRPCHeader` | codex/0.168.0 | canonical `TestedVersion` |
| `codex/probe_test.go:20` | `TestProbeKnownVersionAndHelpProjectsPlatformCapability` | codex/0.168.0 | canonical `TestedVersion` |
| `codex/rpc_session_test.go:820` | `TestSanitizedLifecycleFixturePreservesNativeInterleaving` | codex/0.168.0 | canonical `TestedVersion` |
| `omo/transcript_test.go:59` | `TestPinnedNativeTranscript` | omo/5.1.69 | plain directory, kept |
| `omp/adapter_test.go:961` | `TestObservedWindowsAndLinuxFailedAndAbortedTurns` (helper `nativeActivityFixture`) | omp/18.20.0 | plain directory, kept |
| `pi/rpc_test.go:16` | `TestDecoderReadsVersionedLifecycleFixtureAcrossArbitraryBoundaries` | pi/1.5.2 | plain directory + manifest, kept |
| `pi/rpc_test.go:91` | `TestValidatorAcceptsDocumentedCompactionContinuationAndRejectsInvalidLifecycle` | pi/1.5.2 | plain directory + manifest, kept |
| `pi/probe_promotion_test.go:297` | `TestPiTestdataManifestBindsRecordedDigests` | pi/1.5.2 | plain directory + manifest, kept |

Method (lesson 32): each site's target file is hashed (SHA-256) at base and at HEAD; the nine reader
files are byte-identical base vs HEAD; no reader target lies inside a retired directory (grep over
all `testdata/` reads); the decode tests run green at HEAD; mutants (1)-(5) and (8) prove the reads
are load-bearing. No reader path can decode fewer records because its input bytes are unchanged and
no reader source changed — the failure this gate exists to catch (a repointed reader silently
decoding fewer records) has no carrier in a retirement-only landing, and the mutant rows show each
reader fails when its directory is disturbed.

## 5. Gates (all foreground; Windows and WSL never concurrent)

1. Windows `go test -count=1 ./internal/harness/...` (whole tree, one invocation).
2. Census: `go test -list '.*' ./internal/harness/...` at base and at HEAD — name sets equal
   (D1: test names frozen). Fixture census: `git ls-tree -r --long` base 114 / 596,662 -> HEAD
   82 / 519,672 + recipe 2,286 = 83 / 521,958 (§3/§8 arithmetic, apply-stage repairs included);
   any other delta is red.
3. Decoded-frame equality: the §4 hash table plus the reader audit.
4. `gofmt -l`; `go vet ./...`; `GOOS=windows go build ./...` and `GOOS=linux go build ./...`
   (test files compile for the host GOOS only); `git diff --check`.
5. Citation corpus run (`scripts/ci/check-citation-existence.py`, P6) — the delete slice leaves the
   audited corpus green: none of the five retired version strings occurs in `docs/runbooks`,
   `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md` at the base revision (grep = 0), and the
   appended CLOSED-TODO text cites only head-present paths (§7); historical citations of the retired
   paths live outside the audited corpus and are listed in the PR body.
6. WSL `go test -race -count=1 ./internal/harness/...` (Linux halves: `omo/linux.json`).
7. Windows run only: `claude/adapter_cmdline_windows_test.go` and `omp/adapter_windows_test.go`
   compile and run under the host GOOS — recorded from the §5.1 output.
8. Mutants M1-M8 (+M9 recorded inapplicable), each red then restored green with the blob verified.
9. DLP: no profile host path and no backslash-before-`u` in the diff.

## 6. Mutants

| id | mutation | expected red row |
| --- | --- | --- |
| M1 | remove `codex/0.168.0/frames.jsonl` | `TestFramerReadsSanitizedVersionFixture` |
| M2 | flip one byte in `claude/2.1.323/stream-json.jsonl` | `TestClaudeTestdataManifestBindsRecordedDigests` |
| M3 | flip one byte in `pi/1.5.2/lifecycle.jsonl` | `TestPiTestdataManifestBindsRecordedDigests` |
| M4 | delete `claude/2.1.281/structured-output-retry-exhausted.jsonl` | `TestTerminalValidatorReplaysNativeStructuredOutputCaptures` |
| M5 | point `claude/stream_test.go:13` at the retired `2.1.259` | `TestDecoderReadsFixtureAcrossCRLFAndChunkBoundaries` |
| M6 | rename one reader test | the census diff is non-empty |
| M7 | touch `testdata/opencode/**` or `opencode/*_test.go` | the freeze diff prints |
| M8 | drop a keep-floor directory (codex `0.167.2`) | the before/after inventory disagrees with the declared set |
| M9 | conditional delta rows | inapplicable: no delta lands; inventory shows no delta files |

Every restored file is verified against its base blob (`git rev-parse 2c8c419e:<path>`).

## 7. Risks and out-of-scope

- The citation gate audits the whole appended CLOSED-TODO document; the new text cites only
  head-present paths, and the gate is run before the stage ends (P6).
- `docs/design/harness.md` is read-only here; its `:31` codex `0.167.2` sentence and `:247`
  opencode README link stay true because both directories survive.
- Program-level follow-up (2026-11-23 round-1 ruling, P3-4; not this slice's gate): the
  profile-path DLP scan matches the forward-slash separator form only and needs the
  backslash-separated form as well. The branch diff's removed lines carry three profile paths in
  that form (added lines: none), so the forward-slash-only scan is a false negative for it.
- Out of scope: production harness/adapter code; `docs/runbooks/**`; `docs/design/harness.md`;
  app-side version strings; new capture collection; `testdata/opencode/**` and `opencode/*_test.go`.

## 8. Measured results at the settled tree (implement stage)

Base revision `2c8c419e`; working tree = base + the changes §3-§6 declare; nothing committed (the
stage leaves the tree for verification). All gates foreground; Windows and WSL never concurrent.

| gate | command (cwd `daemon/` unless noted) | exit | result |
| --- | --- | --- | --- |
| Windows harness suite, base | `go test -count=1 -json ./internal/harness/...` | 0 | 9 packages `ok`, 0 fail actions, 15 s |
| Windows harness suite, HEAD | `go test -count=1 -json ./internal/harness/...` | 0 | 9 packages `ok`, 0 fail actions, 13 s; 1352 unique passed test/subtest names |
| Settled re-run after mutants | `go test -count=1 ./internal/harness/...` | 0 | 9 `ok`, 0 `FAIL`, 18 s |
| WSL `-race`, HEAD | `go test -race -count=1 ./internal/harness/...` (Ubuntu-24.04, go1.27.0) | 0 | 9 packages `ok`, 68 s; Linux halves (`omo/linux.json`) |
| Test-name census | `go test -list '.*' ./internal/harness/...` base vs HEAD | 0/0 | 584 names, sets equal (`diff` empty) |
| Fixture census | `git ls-tree -r --long <settled tree>` | — | 114 / 596,662 -> 82 / 519,672 + recipe 2,286 = 83 / 521,958 (§3 arithmetic; apply-stage prose repairs, §8.3) |
| Reader-input equality | `sha256sum -c` over the 24 files the readers open | 0 | all identical base vs HEAD |
| Citation corpus | `python3 scripts/ci/check-citation-existence.py --base 2c8c419e --head <snapshot>` | 0 | 1 changed document, 537 path citations, 3 pre-existing ignore-covered notices |
| `gofmt -l .` | | 0 | no output |
| `go vet ./...` | | 0 | — |
| `go build ./...`; `GOOS=windows`; `GOOS=linux` | | 0/0/0 | — |
| `git diff --check` | repo root | 0 | — |
| App cross-package consumer | `go test -count=1 -run TestNewHarnessRegistryBindsNativeAdaptersAndUnsupportedOpenCode ./internal/app/` | 0 | 1.1 s |
| OpenCode freeze | `git diff --name-only -- daemon/internal/harness/testdata/opencode daemon/internal/harness/opencode` | — | empty |
| Inventory artifact | the declared after-set vs the working tree | — | ARTIFACT-AGREES (83 files) |
| DLP | the diff scanned for a user-profile path fragment and a backslash before `u`; new files grepped directly | — | 0 / 0 |

Windows platform files exercised by the host run: `TestClaudeCandidateWindowsCommandLineFitsCreateProcessLimit`
(`claude/adapter_cmdline_windows_test.go`) and `TestRetainedResumeAcceptsShortPathIdentity`
(`omp/adapter_windows_test.go`), both in the passed set with their subtests.

### 8.1 Reader-site audit (lesson 32)

All twelve base read sites (§4) keep their target path and mechanism entry: the reader test files
are byte-identical base vs HEAD (`git diff --name-only` over the nine files is empty), every target
file hash is identical (`sha256sum -c` over 24 inputs, exit 0), and each reader's decode assertions
are frozen by that byte-identity. No reader can decode fewer records: its input bytes and its
assertion text are unchanged, and mutant M5 shows the read is load-bearing (a repointed directory
reds the test).

### 8.2 Mutants (red observed, then restored green; restored blobs verified against base)

| id | mutation | observed red | restore |
| --- | --- | --- | --- |
| M1 | removed `codex/0.168.0/frames.jsonl` | `--- FAIL: TestFramerReadsSanitizedVersionFixture`, `framing_test.go:208: read framing fixture: open …: The system cannot find the file specified.`, exit 1 | blob `c06ebf43…` = base |
| M2 | flipped one byte in `claude/2.1.323/stream-json.jsonl` | `--- FAIL: TestClaudeTestdataManifestBindsRecordedDigests`, `probe_test.go:452: fixture stream-json.jsonl digest = "sha256:9929b378…", want "sha256:800c7a17…"`, exit 1 | blob `c3280f94…` = base |
| M3 | flipped one byte in `pi/1.5.2/lifecycle.jsonl` | `--- FAIL: TestPiTestdataManifestBindsRecordedDigests`, `probe_promotion_test.go:335: fixture lifecycle.jsonl digest = "sha256:18ec2458…", want "sha256:406fade8…"`, exit 1 | blob `180051a4…` = base |
| M4 | deleted `claude/2.1.281/structured-output-retry-exhausted.jsonl` | `--- FAIL: TestTerminalValidatorReplaysNativeStructuredOutputCaptures`, `stream_test.go:183: read capture: open …: The system cannot find the file specified.`, exit 1 | blob `b73e1b71…` = base |
| M5 | `claude/stream_test.go:13` pointed at the retired `2.1.259` | `--- FAIL: TestDecoderReadsFixtureAcrossCRLFAndChunkBoundaries`, `stream_test.go:15: read fixture: open …: The system cannot find the path specified.`, exit 1 | blob `f4ad93b6…` = base |
| M6 | renamed `TestFramerReadsSanitizedVersionFixture` | census `diff` non-empty: 2 lines (old name out, new name in) | blob `580588dc…` = base |
| M7 | appended a byte to `testdata/opencode/2.3.0/README.md` | the freeze diff prints that path; empty again after restore | blob `ac3f5740…` = base |
| M8 | moved the keep-floor `codex/0.167.2` directory away | the inventory artifact prints DISAGREES (9 paths); AGREES again after restore (83 files before and after) | 9 blobs unchanged |
| M9 | conditional delta rows | inapplicable: no delta lands; `find testdata -iname '*delta*' -o -iname '*patch*'` = 0, and the inventory lists no delta files | — |

### 8.3 Apply-stage prose repairs (2026-11-23; simplify findings F1-F4, D1)

Four README prose repairs landed on the settled tree; no capture, `version.txt`, manifest,
reader or test byte changed. Range gate `git diff --name-status 2c8c419e --
daemon/internal/harness/testdata` = 1 A (the recipe) + 32 D (the five retired directories) +
4 M (exactly the two ruled claude READMEs and the two pi READMEs); the nine reader files diff
empty; the opencode freeze diff empty.

| file | change | bytes |
| --- | --- | --- |
| `testdata/README.md` (recipe) | step 1 conditional on a `version.txt`; step 5 states the `daemon/` cwd | 2,235 -> 2,286 |
| `testdata/pi/0.87.1/README.md` | retired-`0.85.1` claims repointed to live content (`native-windows-2026-09-24.md`, `../1.5.2/README.md`) | 2,444 -> 2,618 |
| `testdata/pi/1.5.2/README.md` | dangling `../0.85.1/README.md` link replaced | 4,538 -> 4,478 |

Re-run gates on the settled tree: Windows `go test -count=1 ./internal/harness/...` exit 0
(9 packages `ok`, 0 `FAIL`, 23 s); WSL `go test -race -count=1 ./internal/harness/...` exit 0
(Ubuntu-24.04, go1.27.0; 9 `ok`, 0 `FAIL`, 57 s); fixture census `git ls-tree -r --long` =
82 / 519,672 + recipe 2,286 = 83 / 521,958; `git diff --check` exit 0.

### 8.4 Round-1 fix (2026-11-23; PR #271 review, rulings P1-1, P3-1 to P3-4)

Prose-only repairs; no capture, `version.txt` or manifest byte and no Go file changed; inside
every retained `testdata/` directory only `README.md` files differ from the base revision.

| file | change | ruling |
| --- | --- | --- |
| `testdata/README.md` | retention rule names the `testdata/opencode/` freeze (W6-04) as an explicit exception | P3-1 |
| `testdata/pi/1.5.2/README.md` | restored the one-line `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV` description; the retired-link removal stands | P1-1, P3-2 |
| `testdata/claude/2.1.281/README.md` | retired-directory paths replaced by the bare version | P3-3 |
| `testdata/pi/0.87.1/README.md` | retired-directory paths removed | P3-3 |
| `testdata/codex/0.167.2/README.md` | retired-directory path replaced by the bare version | P3-3 |

Fixture census after the round-1 repairs (`git ls-files --cached` with working-tree sizes over
`daemon/internal/harness/testdata`): **83 files / 522,147 bytes**; the §3 and §8.3 totals predate
these repairs. `git grep` for each retired version string under `testdata/` returns no path
pointer; the remaining mentions are recorded payload values inside JSONL captures,
manifest keys and digest comparisons, and bare historical version names (the hit table is in the
PR body). The profile-path DLP follow-up is in §7; P3-5 (the review bot's failed run) needs no
action.
