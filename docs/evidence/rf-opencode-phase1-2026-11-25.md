# rf-opencode-phase1 — OpenCode V2 2.4.1 scoped Phase-1 promotion (Windows surface; Linux fail-closed)

Delivered 2026-11-26 on branch `refactor/rf-opencode-phase1`; dispatch-gate base revision
`9eb874bf22eace98399e54b25d00634b2bed5418` (`origin/main` at the merge of
`rf-opencode-230-hostname-redact` #291). Contract `goal-rf-opencode-phase1.md`; accepted plan
`docs/plans/2026-11-25-rf-opencode-phase1-plan.md`
(sha256 `300ed1a7e5647f284097faf89621a4b8d8e0c46b430b00fbd717cfdcf0a9de85`); proposal of
record `refactor/opencode-phase1-amendment.md` (GDD state root). The fix round below re-anchored
the client under the conductor ruling of 2026-11-26 (review R1-R9); its comparison base is
`550f96a3c128d3f3e397c6f03f6e003be6dfbd44` (`origin/main` #294).

Round 2 (2026-11-26) re-ran the implement stage under the conductor's second ruling (PR #298
round 1, MUST_FIX F1-F6); its comparison base is `6a0afd5f894468b148ba81209993ae05a3599e5f`.
Round-2 changes are the F1-F6 items mapped in §8; the live proof (§2 row 7) was re-run on the
round-2 bytes and is receipt-bound to them.

No credential, relay host, machine hostname or host absolute path appears here. Receipts are cited
by repository path and sha256, never copied. The amendment **merge** revision is not knowable
before the merge: this record binds the delivered-file digests in §5; row 6 re-binds or re-runs
against the merge revision.

## 1. What was delivered

- Design text P1-P5, verbatim per the contract, with the `Before` spans checked against the base
  revision before editing and the `After` spans independently re-verified byte-exact
  (whitespace-normalized) at the delivered bytes: 7/7 `Before` spans, 7/7 `After` spans.
  `docs/design/harness.md` (OpenCode V2 row + the 2.3.0 paragraph → 2.4.1 evidence),
  `docs/goals/0010-refactor-program.md` (D1 row, W6-04 row/bullet), `docs/goals/README.md` (two
  appends), `docs/TODO.md` (two consolidations moved out; the F2-exclusion entry re-anchored for
  the conductor's D3 amendment).
- P6.1 pin + exact-blob gate (`daemon/internal/harness/opencode/adapter.go`): `TestedVersion =
  2.4.1`; `TestedExecutableHash` (Windows PE `f3da11bf…`), the evidence-only
  `testedLinuxExecutableHash` (Linux ELF `ec93043f…`), the platform+blob refusal branch on the pi
  shape (`rejectOpenCode` validates the refused projection); the version gate keeps
  `ErrUnsupportedVersion` **on Windows**. Launch corrections verified against the pinned bytes:
  `--pure` is gone, the readiness regex accepts the bare `server listening on
  http://127.0.0.1:PORT` line every 2.4.1 receipt records, and the probe bound follows pi
  (five seconds). The fix round (R9) moved the platform question to the composition root and
  keeps a canceled or expired probe inside the tolerated `ErrHarnessUnavailable` class
  (`openCodeProbeDidNotFinish`), so a slow probe cannot abort daemon startup.
- P6.2 registry wiring: `harness.NewRegistry()` no longer resolves OpenCode;
  `daemon/internal/app/probe.go` registers `opencode.NewAdapter(profile.Command)` **on Windows
  only** (fix round R9); every other platform registers `harness.NewOpenCodeAdapter()` — the
  byte-identical unavailable seam with **no native probe I/O** and the same
  `ErrHarnessUnavailable` classification and reason class the compose rows assert.
- P6.3 CI assertions: the six `--assert` rows and the race row stay named, with two test names
  re-anchored where the route they pin moved (`…RefusesBeforeReadinessRequest`,
  `…RejectsWrongLinuxV2PeerBindingsBeforeRequest`, `…FailsBeforeReadinessRequest`), updated in
  `.github/workflows/ci.yml` and `scripts/ci/linux-v2-native-acceptance.sh` in the same change.
- P6.4 V1 `1.18.32` drift fix: `scripts/ci/linux-v2-native-tools.sh` (URL →
  `opencode-linux-x64.tar.gz` v2.4.1, archive sha256 `9ddd18e3…`, extracted-ELF sha256
  `ec93043f…`, version line `opencode v2.4.1`), `scripts/ci/linux-v2-native-acceptance.sh`
  (`harness_version` map → `2.4.1`), `.github/workflows/ci.yml` (folded prepare step: URL,
  archive name, sha256, extracted-ELF assertion, `version_expected`/`version_actual`,
  executable path).
- P6.5 witness legs: `daemon/internal/app/linux_owner_witness_linux.go` is byte-unchanged; its
  tested-release check reads `opencode.TestedVersion` and accepts `2.4.1`; the health assertions
  row 4 binds and the no-native-turn-stage refusal are re-quoted unchanged.
- P6.6 TODO consolidations: `daemon/internal/harness/opencode/inprocess_containment_test.go`
  added on the pi shape (observable install/restore); the frozen OpenCode duplicates moved onto
  `harness.IsNilInterface`, `harness.ProcessForwarder`, `harness.DeliverEvent`, `harness.CloseGate`
  and `harness.WaitResult`.
- **Fix round R1 (re-anchor, conductor ruling option A).** The delivered adapter no longer speaks
  the pre-2.4 route set; every request form is the one the pinned bytes answer and the committed
  Phase-0 receipts record:
  - `daemon/internal/harness/opencode/client.go`: readiness is the `/api/info` identity boundary
    (`pid` equals the launched server pid and `version == "2.4.1"`; `ErrNotReady` marks the
    listening-but-not-ready `503`); `Prompt` sends the recorded flat body
    (`{id,text,delivery,resume}`) and decodes the recorded admission
    (`{data:{id,sessionID,time.created,type:"user",payload.text,delivery}}`); `Interrupt` sends
    `POST …/interrupt?resume=false` and requires `200 {"interrupted":true}` (`false` refuses as
    `ErrInterruptUnapplied`); `OpenSessionEvents` is deleted — the global `/api/event` stream is
    the only event surface, and the client-supplied session id is honoured by `POST /api/session`
    (live-characterized, see §2 row 7).
  - `daemon/internal/harness/opencode/sse.go`: the envelope accepts and validates the recorded
    `created` timestamp; `GlobalFrameSessionID` attributes each global frame to its durable
    session and fails closed on an internal identity conflict.
  - `daemon/internal/harness/opencode/adapter.go`: `StartTurn` opens the global stream before
    admission and starts the watcher with it, so no session event is missed and a pre-admission
    failure cannot strand a registered `streamDone` barrier; `observeSessionFrames` skips frames
    the shared stream carries for other sessions or non-session events; `Open` waits on the
    `/api/info` identity and fails before session create on a mismatch.
- **Fix round R3/R5/R8.** Stale comments now state the delivered wiring (adapter type comment,
  constructor phrase, package doc, `DecodedSessionEvent`/`DurableCursor`/validator wording); the
  readiness test is `TestReadinessAcceptsTheRecordedLineAndTheFrozenFixturePrefix` and no longer
  claims a recorded prefixed 1.18.x line; the two over-cap comments in
  `daemon/internal/app/harness_test.go` are trimmed, and a branch-wide scan of added comment
  blocks finds none longer than two lines.
- **Fix round R4.** `daemon/internal/harness/opencode/probe_promotion_test.go` parses the
  committed `testdata/opencode/2.4.1/probe-windows.json` receipt and README and asserts
  `binary.sha256 == TestedExecutableHash`, the Windows PE asset identity, and the parsed
  `opencode v2.4.1` version line — a wrong constant now reddens a suite instead of silently
  leaving Windows dark.
- **Fix round R2/R6/R7.** This record: §5 recomputes every delivered digest; shared docs are
  cited by content (§5b); the cover cites the real plan digest; §7.4 is reworded to the rows'
  actual pins; §7.1 records the resolved merge disposition.
- **Fix round R8 (duplication).** The changed test files hoist their repeated blocks into single
  helpers: the staged-session preamble (`daemon/internal/harness/opencode/adapter_test.go`), the
  native start preamble and the isolated config environment list (both in
  `daemon/internal/harness/opencode/adapter_native_test.go`, shared by the work tests and the
  promotion proof). Assertions, test names and expected values are unchanged; the changed-file
  duplication gate exits 0 with no new clone (§4).
- **Round 2 (F1-F6).**
  - F1: `openCodeProbeDidNotFinish` returns a typed `probeNotFinishedError` whose `Unwrap` is the
    tolerated `ErrHarnessUnavailable` and whose `Is` reaches the context sentinel, so
    `probeconformance`'s cancellation rows and the daemon's startup tolerance hold at once;
    `adapter_test.go`'s cancelled/expired row asserts the sentinel is reachable now.
  - F2: the composed Goal-assignment test skips off Windows (mirroring its foreign-blob sibling),
    and `adapter_policy_truth_table_test.go`'s production projection for opencode models the
    composition root per platform (native adapter on Windows, the unavailable seam elsewhere).
  - F3: `Adapter.Start` accepts the daemon-validated admission profile alias (the claude shape)
    and claims no native model identity; the pinning row now starts a turn with a non-empty
    alias instead of refusing it.
  - F4: the decoder's known set is re-anchored to the recorded 2.4.1 vocabulary
    (`session.execution.*`, `session.inbox.enqueued`, `session.step.*`, `session.tool.called`,
    `session.tool.success`); `client.go` gained the recorded `GET /api/session/{id}` outcome
    readback (`SessionOutcome`); `WaitTurn` blocks on a per-turn barrier that settles only from a
    recorded `session.execution.*` terminal whose readback names the same outcome, and
    `currentResult` maps the verified outcome to `succeeded`/`failed`/`cancelled` with no
    semantic result and unknown usage; `Close` no longer interrupts a turn that already settled
    verified (the pinned bytes refuse that interrupt rather than queuing it — the live proof
    caught this and the row pins it); `durable.seq` accepts the recorded `0` creation cursor.
  - F5: the live proof now drives two admitted turns behind a non-empty `ModelProfile` (a
    verified success and an applied cancel with a verified interrupted terminal) and records both
    operations in its receipt (§2 row 7).
  - F6: every operator input of the proof is an explicit absolute environment path; the CI
    folded-gate label and provenance sentence name the readiness route; the `1.18.32` and
    host-path censuses are recorded with their exact commands (§2 row 5, §4).

## 2. Acceptance rows

Row 1 (Windows receipts): unmodified Phase-0 bytes under
`daemon/internal/harness/testdata/opencode/2.4.1/` (9 files); their `binding_sha256` is the
collector `087e07a8acaab7c9d07c169acdbd70342e651112e66d0d2a4bdfb3d00c98b0f5` plus
`source_subject.py 8df46499b267f97b0e309f456bbbf2eec5c5ce98ceef92685d7a49bd798a2f93` and
`source_head 49a606dd346fa9ed0a5f1175c784934d11710223`. The route rows the re-anchored client
binds to are the README §4 table (`/api/info` 200/401, `/api/health` 404 still absent, session
create/prompt/permission/interrupt/delete, global `GET /api/event` 200, per-session
`/event?after=0` 404 still absent). Re-verified by content, not edited.

Row 2 (exact binary): `TestProbeRejectsWrongVersionAndIncompleteHelp` (a wrong `--version`
refuses; M2 reddens it), `TestProbeRejectsWrongBlobDigest` (a foreign blob refuses with
`ErrNativeUnverified`; M1 reddens it), the receipt-binding test (R4; M1 reddens it),
`TestOpenCodeForeignBlobGoalAssignmentFailsBeforeWorkspaceProcessSessionAndEventSideEffects`,
`TestProbePromotesOnlyTheExactTestedBlobOnWindows`, and the six CI assertions incl. the race row
(§4). Round 2 adds: `probeconformance`'s opencode cancellation rows and
`TestProbeKeepsSeededProjectionWhenTheProbeIsCancelledOrExpired` (F1; M12/M13),
`TestClientSessionOutcomeReadsTheRecordedReadback` (the recorded `GET /api/session/{id}`
readback incl. identity mismatch, missing session, unsettled `outcome` and the pre-request
session-id refusal), `TestRecordedReceiptFramesDecode` and
`TestRecordedReceiptTerminalsCarryTheRecordedPayload` (every durable frame of all eight
receipts, with the provider-failure error type and the interrupted reason),
`TestRecordedReceiptSessionFramesSatisfyTheValidator` (two recorded sessions replayed through
the watcher's validator),
`TestWaitTurnSettlesRecordedTerminalsAgainstTheOutcomeReadback` (success, provider failure,
interruption, and a readback that disagrees → unverified; each verified row also pins that
`Close` issues no interrupt for a finished turn), and
`TestRecordedOutcomeReadbackRefusesWhenTheSessionEndedFirst` (M14/M15).

Row 3 (wiring): `TestNewHarnessRegistryBindsNativeAdapters` asserts the native adapter on
Windows and the unavailable seam on every other platform (M8 reddens the off-Windows branch);
`TestComposedOpenCodeProbeStaysFailClosedThroughTheRegistry` pins the composed native adapter's
tolerated refusal; `TestStartupProbeRegistersUnavailableNativeProjections/opencode` keeps the
unavailable seam pinned; `TestStartupProbeToleratesAnExpiredOpenCodeProbe` pins that a canceled
or expired probe is tolerated instead of aborting startup (M7 reddens it, §3);
`adapter_policy_truth_table_test.go` carries the promoted/refused/foreign-blob rows;
`inprocess_containment_test.go` installs and restores the in-process containment supervisor.
Round 2: the truth table's opencode production projection selects the composition root's actual
wiring per platform (native adapter on Windows, the unavailable seam elsewhere) and its expected
row carries the platform sentence off Windows; the composed Goal-assignment row skips off
Windows, mirroring its foreign-blob sibling (`harness_test.go`). `TestStartTurnRefusesARecorded
AdmissionForAForeignInbox` pins the 2.4.1 inbox identity binding (M16).

Row 4 (Linux containment; standing condition, **recorded unmet**): no dispatched
`linux-native-v2-acceptance` `opencode_native_health` run was attempted — the conductor ruled
"no dispatched Linux run in this goal; do not attempt it", and no qualifying-host decision
exists. No run id or subject sha256 is recorded; no Linux promotion is claimed.

Row 5 (pin drift): the carried pin literals are `2.4.1` —
`linux-v2-native-tools.sh` names `opencode-linux-x64.tar.gz` `9ddd18e3…` → ELF `ec93043f…` →
`opencode v2.4.1`; the acceptance fixture's `harness_version` map names `2.4.1`; the witness
tested-release check accepts `2.4.1` via `opencode.TestedVersion`. Census, exact command:
`git diff 550f96a3c128d3f3e397c6f03f6e003be6dfbd44 -U0 -- <the §5a paths> | grep -E '^\+' |
grep -vE '^\+\+\+' | grep -cE '1\.18\.32'` → **14**, and every hit is a comment, a fixture
command name, a round-2 witness message or record/plan prose: one synthetic profile command
string (`daemon/internal/app/harness_test.go`), two witness messages
(`daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go`), four comments
(`daemon/internal/harness/opencode/sse.go`), the D1 row
(`docs/goals/0010-refactor-program.md`), three lines of this record and three lines of the plan's
round-2 section. Pin-scope census, exact command: `grep -rn '1\.18\.32'
scripts/ci/linux-v2-native-tools.sh scripts/ci/linux-v2-native-acceptance.sh
.github/workflows/ci.yml daemon/internal/harness/opencode/adapter.go` → **0 hits**: no carried
pin names V1.

Row 6 (revision binding): §5's delivered-element digests plus the amendment merge revision or a
post-merge re-run.

Row 7 (Windows live promotion proof; re-run in round 2 on the delivered bytes): the opt-in
env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` ran against the exact pinned Windows
PE (sha256 `f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e`, 278,383,496
bytes, digest re-verified by the test before launch) with the recorded relay provider and model
on this Windows host, driving two admitted turns behind a non-empty `ModelProfile`
(`implementation-default`; the daemon-validated admission-alias shape F3 requires):

- succeeded operation: Open (the retained client accepts the launched pid and refuses `pid+1`
  with `ErrInvalidIdentity`, then creates the session), `StartTurn` with the recorded inbox
  admission observed on the global stream (6 native frames), `WaitTurn` returned the verified
  `session.execution.succeeded` terminal, the client's `GET /api/session/{id}` readback named
  `succeeded`, `Close` terminated the server cleanly and `Wait` returned `succeeded` with no
  semantic result and unknown usage;
- interrupted operation: the same open/admission path (4 native frames), the late-goal turn
  cancelled through `Control` (a 2.4.1 interrupt arriving before the first generated token is
  refused, not queued, so the proof re-applies until the server reports it applied), `WaitTurn`
  returned the verified `session.execution.interrupted` terminal, the readback named
  `interrupted`, and `Wait` returned `cancelled` with `TaskResultReasonCancelled` (23.57 s).

Receipt: `<STATE_DIR>/scratch/rf-opencode-phase1-impl2/promotion-receipt.json`, sha256
`7bd9db34241339cc9b75ac2cad006b5ffdce4efedf072c72739e7a3a412d0655`. Its `operations` block
carries `wait_turn: verified`, `readback_outcome`, `result_kind`, `control_applied`,
`native_frames`, the readiness booleans and `close_terminated: true` per operation, plus
`relay_host_recorded: false` and `model_profile_alias_used: true`. Its `delivered_sha256` block
equals the delivered bytes byte-for-byte: `adapter.go` `ad572a922293…4df8`, `client.go`
`2f9a9af4c11e…2dbc`, `sse.go` `e4188206fdf8…4f04`, `types.go` `8df924a2c543…5779`,
`probe_promotion_test.go` `ae9b0c550826…b8c6`, `adapter_native_promotion_test.go`
`4bdf7c291c45…b692` (§5a). The credential was read at run time from the operator's OMP models
config, passed only through the child environment, and never entered argv, a file, a log line,
the journal or the receipt. The first round-2 attempt failed and is kept as evidence: `Close`
interrupted the already-succeeded turn and the server refused (`opencode interrupt did not
apply to a live turn`); the round-2 `closeOnce` fix and its interrupt-call row now pin that a
verified terminal is never interrupted.

## 3. Mutants (scratch copies, restored byte-identical)

| Mutant | Mutation (scratch) | Check that reddened | Restored |
| --- | --- | --- | --- |
| M1 wrong blob digest | `TestedExecutableHash` ← the Linux hash | `TestProbeRejectsWrongBlobDigest`; `TestOpenCodeTestdataBindsThePromotedWindowsDigest` | `adapter.go` `65174205…2ac93a` |
| M2 wrong `--version` | the `version != TestedVersion` branch deleted | `TestProbeRejectsWrongVersionAndIncompleteHelp` (V1_version, historical_V1_pin, superseded_V2_pin rows) | `adapter.go` `65174205…2ac93a` |
| M3 Linux admission forced to promote | platform-gate condition forced false | `TestProbeRecordsExactTransportButRemainsUnverified` (Linux branch), `TestProbePromotesOnlyTheExactTestedBlobOnWindows` (Linux branch) — run under WSL | `adapter.go` `65174205…2ac93a` |
| M4 readiness identity check neutered | `pid`/`version` equality removed from `Client.Readiness` | `TestClientReadinessRejectsIdentityRedirectAndOversizedResponse` (wrong_pid, wrong_version rows) | `client.go` `b9bb6894…d46c2c` |
| M5 global-frame filter removed | the ownership skip in `observeSessionFrames` removed | `TestStartTurnSkipsForeignSessionFramesOnTheGlobalStream` (cross-session frame failed the turn) | `adapter.go` `65174205…2ac93a` |
| M6 stream opened after admission | `OpenGlobalEvents` moved below `Prompt` | `TestStartTurnOpensTheGlobalStreamBeforeAdmission` (call order `["prompt","events"]`) | `adapter.go` `65174205…2ac93a` |
| M7 probe cancellation exposed raw | `openCodeProbeDidNotFinish` returns the raw context error | `TestProbeKeepsSeededProjectionWhenTheProbeIsCancelledOrExpired` (3 rows); app `TestStartupProbeToleratesAnExpiredOpenCodeProbe` (startup abort) | `adapter.go` `65174205…2ac93a` |
| M8 platform gate removed | `newHarnessRegistry` always registers the native adapter | `TestNewHarnessRegistryBindsNativeAdapters` off Windows (run under WSL) | `probe.go` `0ccbafd4…e24ff49` |
| M9 interrupt not-applied accepted | the `if !interrupted { return ErrInterruptUnapplied }` guard removed from `Client.Interrupt` | `TestClientInterruptRequiresTheRecordedInterruptedAnswer/not_applied` | `client.go` `b9bb6894…d46c2c` |
| M10 durable identity conflict ignored | `durableSessionIdentity` returns the data session id on a durable aggregate mismatch | `TestGlobalFrameSessionIDFiltersAndFailsClosed/identity_mismatch` | `sse.go` `7e6bcf7f…8fb9f94` |
| M11 stream-open failure ignored | an `OpenGlobalEvents` failure is replaced with an empty stream | `TestStartTurnFailsClosedWhenTheGlobalStreamCannotOpen` | `adapter.go` `65174205…2ac93a` |

M3 and M8 redden only in the Linux CI job (`daemon`, ubuntu-latest, non-root); M1/M2/M4-M7 and
M9-M11 redden in both `daemon` and `daemon-windows`. The compose refusal rows are constructor-seam
assertions, so a registry-gate mutation cannot redden them; they are recorded, not silently
skipped. Each mutant was run in a scratch copy of `daemon/`; the restored digests above are the
delivered bytes' digests (full values in §5). Oracle note: neutering the post-open closed
re-check in `StartTurn` leaves the suite green — the cancelled stream context fails the open
before the re-check — and that predicate is carried from the comparison base, so it needs no
new pin; the concurrent-close row is pinned through the cancellation path
(`TestCloseCancelsInFlightGlobalStreamOpen`).

### 3a. Round-2 mutants (fresh scratch copies of the round-2 bytes; the delivered tree is untouched)

| Mutant | Mutation (scratch) | Check that reddened (observed) |
| --- | --- | --- |
| M1 re-run wrong blob digest | `TestedExecutableHash` ← the Linux hash | `TestOpenCodeTestdataBindsThePromotedWindowsDigest` (receipt blob `f3da11bf…` ≠ mutated constant) and `TestProbeRejectsWrongBlobDigest` |
| M2 re-run wrong `--version` | the `version != TestedVersion` branch deleted | `TestProbeRejectsWrongVersionAndIncompleteHelp` (`adapter_test.go:162`, want `ErrUnsupportedVersion`) |
| M12 (F1) sentinel dropped | `openCodeProbeDidNotFinish` back to the fix-round `%v` shape (no typed error) | `probeconformance`'s opencode cancellation rows (`cancellation_test.go:166`, want `context.Canceled`) and `TestProbeKeepsSeededProjectionWhenTheProbeIsCancelledOrExpired` |
| M13 (F1) `Is` neutered | `probeNotFinishedError.Is` returns false | same `probeconformance` rows and the same adapter row: the `Is` method is what exposes the sentinel alongside the tolerated class |
| M14 (F4) readback verification skipped | `settleRecordedTerminal` treats every terminal as verified | `TestWaitTurnSettlesRecordedTerminalsAgainstTheOutcomeReadback/readback_disagrees` (WaitTurn returned nil for a disagreeing readback) |
| M15 (F4) verified-close guard removed | `verifiedTurn := false` in `closeOnce` | the same test's `interrupted` row: `Close() error = opencode interrupt did not apply to a live turn` |
| M16 (F4) inbox identity check removed | the `session.inbox.enqueued` identity branch deleted | `TestStartTurnRefusesARecordedAdmissionForAForeignInbox` (the foreign admission was journalled and the turn never refused) |

Every mutated file lived in its own scratch copy (`<STATE_DIR>/scratch/rf-opencode-phase1-impl2/mut/<mutant>/`);
no delivered byte was ever mutated, so "restored byte-identical" is satisfied by construction and
the delivered digests are §5a's. M3 was re-run under WSL (Linux rows) — see §11.

## 4. Gates (exact commands; cwd; exit)

All rows below ran on the delivered bytes (§5a); the range for every diff-scoped command is
`550f96a3c128d3f3e397c6f03f6e003be6dfbd44..<delivered>`.

| Command (as run) | cwd | Exit |
| --- | --- | --- |
| `gofmt -l .` | `daemon/` | 0 (no output) |
| `go vet ./...` | `daemon/` | 0 |
| `go test -count=1 -timeout 900s ./internal/harness/opencode/ ./internal/harness/ ./internal/app/` | `daemon/` | 0 (`app` 143.7 s) |
| `GOMAXPROCS=8 go test -count=200 -timeout 1800s -run 'Test(StartTurn\|CloseCancel\|CloseAndCancel\|Client\|GlobalFrameSessionID\|ProbeKeepsSeeded\|OpenRejectsReadiness\|ReadinessAccepts\|NativePromptAdmission)' ./internal/harness/opencode/` | `daemon/` | 0 (21.9 s) |
| `GOMAXPROCS=8 go test -count=200 -timeout 900s -run '^TestStartupProbeToleratesAnExpiredOpenCodeProbe$' ./internal/app/` | `daemon/` | 0 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (WSL) | `daemon/` | 0 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (WSL) | `daemon/` | 0 |
| `go test -count=1 -timeout 600s ./internal/harness/opencode/ ./internal/harness/` (WSL) | `daemon/` | 0 |
| the six `--assert` rows through `go-test-json-gate.py --stdio-boundary` over `go test -count=1 -timeout 300s -json ./internal/harness/pi ./internal/harness/opencode` (WSL) | `daemon/` | 0 — all seven asserted names `run=1 pass=1 skip=0` |
| the race row, named assertion scoped to its package: `go-test-json-gate.py --assert …/opencode TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./internal/harness/opencode -run '^TestOpenCodeV2VerifierRejectsReleasedPeer$'` (WSL) | `daemon/` | 0 |
| `go test -race -count=20 -timeout 1800s -run '<the repeat set above>' ./internal/harness/opencode/` (WSL) | `daemon/` | 0 (14.2 s) |
| the contract's full-suite race form `go test -race -count=1 -timeout 600s -json ./...` (WSL) | `daemon/` | environment-limited on those bytes: `internal/app` and `internal/harness/opencode` reach the 600 s per-package timeout before the named row runs (`run=0`), and `internal/platform`'s anchor-shape rows fail (root posture); superseded by §11's green CI-posture run |
| env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` (row 7; receipt sha256 in §2) | `daemon/` | 0 (9.6 s) |
| `python scripts/ci/jscpd-changed.py --repo <scratch> --base 550f96a3… --head HEAD` | repo | 0 — 31 changed files, 1759 scanned sources, 8 tolerated among added lines, none new |
| `bash -n scripts/ci/linux-v2-native-tools.sh scripts/ci/linux-v2-native-acceptance.sh` | repo | 0 |
| `python3 -c "import yaml; yaml.safe_load(…)ci.yml"` (WSL) | repo | 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | repo | 0 — 55 tests OK (the same script exits 1 on Windows: its exporter cases need POSIX process control) |
| `git diff --check 550f96a3…` | repo | 0 |
| range added-line host-path scan (`git diff 550f96a3…`, added lines, shapes: account prefix, drive letters, `/mnt`, `/home`, `/var`, `/tmp`; the two untracked files scanned whole) | repo | 0 hits |
| `git diff origin/main...HEAD \| grep -ci "users/"` | repo | 0 |
| the `1.18.32` census over the range's added lines (recorded in §2 row 5) | repo | 4 hits, all historical references, no carried pin |
| comment-block census over the range's added lines (cap: two lines) | repo | 0 blocks over two lines |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | repo | 0 for the record and six other changed docs; `docs/archive/CLOSED-TODO.md` refuses at the comparison base and at the delivered bytes with the same pre-existing finding (specimen lines), and the range's added lines carry none |
| the R4 receipt-binding test (opencode package suite above) | `daemon/` | 0 |
| mutants M1-M11 per §3 | scratch `daemon/` | expected red; restores digest-verified |

## 5. Delivered-element digests

### 5a. sha256 rows (computed at the round-2 delivered worktree; row 6 re-binds at the merge)

The revision column names the bytes each row was measured at: `r1` = untouched by round 2 and
byte-equal to the committed `6a0afd5f` blob (`git show 6a0afd5f:<path>`); `r2` = the round-2
delivered (uncommitted) bytes. Every r2 row was recomputed after the last source edit; the live
proof's `delivered_sha256` block equals the six OpenCode r2 rows below (§2 row 7).

| File | Revision | sha256 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | r2 | `a3326bbc7191822691bedd13d8763e91f57ea3789c1b69b8922a785569470143` |
| `daemon/internal/app/adapter_policy_truth_table_test.go` | r2 | `e220b844e75f32d46f13897b55bcb07f20246b5a2177eb9dcb2310548069476c` |
| `daemon/internal/app/harness_test.go` | r2 | `8ed45523af67c21b301c83dea2ffa39670ab43cedcc9627c35f7fdff7ed61880` |
| `daemon/internal/app/probe.go` | r1 | `0ccbafd4f026472770e5f9e4a9da8146797a7049c62f95c1ba2d19a10e24ff49` |
| `daemon/internal/harness/harness_test.go` | r1 | `3cb64f1c20af0d5063272e63e1a4ae2f9751687826b67c25ff060ce55cc76759` |
| `daemon/internal/harness/opencode/adapter.go` | r2 | `ad572a92229343fd2859118acc3476ed78e23ba20211d2b8da97e918adfa4df8` |
| `daemon/internal/harness/opencode/adapter_native_promotion_test.go` | r2 | `4bdf7c291c457004dbb93de7521c79f35a9cb99b194079d5750594d3e010b692` |
| `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go` | r2 | `cfde82059a96c4bc169a8393bb1e51c16190d97fa68f37141a9f4b6dfaa1b547` |
| `daemon/internal/harness/opencode/adapter_native_test.go` | r1 | `5ba4c1af2cd536e126d030e648f5567a9a3bc79acd8974c7e0573ff180b2edd5` |
| `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` | r1 | `b138d11c8a66b929dad80ccd9ace6e2e98cf3fdfff92669327e3875c612ba40d` |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | r1 | `c52c4a49ecc58cfee939db798be4a102b1046714ccfec69fd5dd6ce3e2176e78` |
| `daemon/internal/harness/opencode/adapter_test.go` | r2 | `12c6aa6f830cd17b86ba003e7f6f5b2452837b84a8fec8593a71805c3c169cbf` |
| `daemon/internal/harness/opencode/client.go` | r2 | `2f9a9af4c11e6891279a0fe6db25f918b684aeb2f84c80d7530862a1c0102dbc` |
| `daemon/internal/harness/opencode/client_test.go` | r2 | `150ac7466a9d755f1c39671718ab2517bbf768a7cfda54fdc2f320169ef1b6d7` |
| `daemon/internal/harness/opencode/inprocess_containment_test.go` | r1 | `9f3bdff150af99fc3775468105ed4270261ced19ef0c20b6ba19382275ace7d7` |
| `daemon/internal/harness/opencode/peer_linux_test.go` | r1 | `e4b464c0d7adf47959012c8be803cdf31c03efe0b3c5dd94ff202d5369ada362` |
| `daemon/internal/harness/opencode/peer_test.go` | r1 | `1a267d6874a3c2fd067c6c90318db1fa934a30909c0dfb283af82d388606e7ff` |
| `daemon/internal/harness/opencode/probe_promotion_test.go` | r1 | `ae9b0c550826f7f1be24c1c9a230fa93f29436278ecff59805020d9abb54b8c6` |
| `daemon/internal/harness/opencode/sse.go` | r2 | `e4188206fdf853c21ca59088a4eb224c894526863f53496c203b691ae9b84f04` |
| `daemon/internal/harness/opencode/sse_test.go` | r2 | `dfc7713993cccb57b34efe47162d97750724b7387363cb39166ccf9d89912761` |
| `daemon/internal/harness/opencode/types.go` | r2 | `8df924a2c5434ca8b4ce721401ad7e2543fdccbfe640afe2de1053d3406e5779` |
| `daemon/internal/harness/registry.go` | r1 | `c8049e6eced97a11d325b78aff7ec9c6aa77102f1397f799164a8646d6e2ddcc` |
| `docs/goals/0010-refactor-program.md` | r1 | `49a9f0335020773716d2f884bd6f904fb78c81bfa2b63ba72ae3b1d24be22e37` |
| `docs/goals/README.md` | r1 | `5f2a6292408648d87d50f78a07d3c4c623bb3f87fa49ee22b4bf377e7bc6edd4` |
| `docs/plans/2026-11-25-rf-opencode-phase1-plan.md` | r2 | `c151905bf57ed6613eb58bf2aacaf4183c7f1bc279ed1914f8142f62f28cbbe2` |
| `scripts/ci/linux-v2-native-acceptance.sh` | r1 | `002915569cc7aa3b9e553cf211d3f688fa961bcba1567f38d4cef2c777948034` |
| `scripts/ci/linux-v2-native-tools.sh` | r1 | `500ac7b47dd9a9b0f5aea2d67dafe4a0131dfd10726b0dc4b0215f4f1b410c69` |

### 5b. Shared docs cited by content (main merges rebind them; never by sha256)

- `docs/TODO.md`: carries the two removed consolidation entries as absorbed work, the re-anchored
  F2-exclusion text for the conductor's D3 amendment, and no OpenCode promotion item.
- `docs/archive/CLOSED-TODO.md`: records the moved consolidations as closed.
- `docs/design/harness.md`: OpenCode V2 row names `2.4.1` partial Windows promotion and the
  fail-closed Linux admission; the 2.3.0 paragraph now points at the 2.4.1 evidence directory.
- `docs/evidence/…`/`docs/plans/…`: not shared; §5a carries their digests where changed.

## 6. Scope notes (files outside the contract's owned list)

- `daemon/internal/harness/harness_test.go` — forced by P6.2's "stops resolving OpenCode"; the
  test registers `NewOpenCodeAdapter()` explicitly, no assertion changed.
- `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` +
  `adapter_native_work_test.go` — forced by P6.4 (the folded CI leg installs the 2.4.1 ELF) and
  by the R1 re-anchor (the opt-in admission observation now reads the recorded inbox frame off
  the global stream; the legacy refusal test and its renamed test target follow the `/api/info`
  readiness route).
- `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go` — compile-forced
  by the re-anchor: the witness keeps its pre-2.4 per-session replay open inside its own wrapper
  (the production client no longer speaks that route) and binds admission by message identity.
  It remains pinned to a pre-2.4 candidate blob at a host path absent from this host and was not
  re-run; oracle gap, named.
- `daemon/internal/harness/opencode/peer_test.go` + `peer_linux_test.go` — the connection-verifier
  rows now exercise `Client.Readiness` (the route the client actually issues) and two test names
  say so.
- `daemon/e2e/production_linux_v2_native_witness_test.go` — carry reverted (§6 note of the
  implement round); `daemon/internal/app/linux_owner_witness_linux.go`, the compose witness and
  `scripts/ci/rootless-compose-native-acceptance.sh` are byte-unchanged.
- Round-2 files: `daemon/internal/harness/opencode/{client.go,client_test.go,sse.go,sse_test.go,
  types.go,adapter.go,adapter_test.go,adapter_native_terminal_candidate_test.go}`,
  `daemon/internal/app/{adapter_policy_truth_table_test.go,harness_test.go}`,
  `.github/workflows/ci.yml` (labels), this record and the plan — all inside the contract's owned
  set or the R1 extension (`client.go`, its tests and fixtures); no new path outside it.
- Kept legacy decode paths (F4's "delete or justify"): the 1.18.32 `session.next.*` vocabulary
  stays in `sse.go` (9 references) because the frozen `testdata/opencode/1.18.32` replay rows are
  its only file-driven consumers (2 replay sites in `sse_test.go`) and 75 references across the
  four opencode test files build inline `session.next.*` frames; no committed 2.4.1 record
  contains a single `session.next.*` frame (`grep -c` over all eight receipts → 0 each), so no
  pinned-bytes frame can take those paths, and only `session.execution.*` settles a turn.
- The terminal-candidate witness (`adapter_native_terminal_candidate_test.go`) remains dormant
  (its pre-2.4 candidate blob is not on this host) and is re-anchored to the delivered semantics:
  `WaitTurn` must not return nil from a vocabulary with no `session.execution` terminal, and
  every scenario settles `ResultUnknown` with a non-nil post-shutdown `WaitTurn` sentinel.

## 7. Unclosed items and residual risk

1. **Resolved by the conductor ruling (was: the client speaks the pre-2.4 route set).** The
   ruling chose option A, re-anchor, and extended the owned set to `client.go`, its tests and its
   fixtures. That re-anchor is delivered in this revision (§1, row 7) and the registration flip
   merges with it in one PR; the advertised-but-dead Windows surface no longer exists. The
   fail-closed alternative (option B) is not taken. Verified by: the live characterization the
   fix round captured before the change (`/api/health` 404, nested prompt body 400, per-session
   event 404, interrupt 200), the reworked unit rows, the local env-gated inbox observation on
   the pinned blob, and the credentialed live proof (row 7).
2. Row 4 remains unmet: no dispatched `opencode_native_health` run, no run id, no subject sha256;
   no Linux promotion claimed; the e2e pinned-version carry of §6 belongs to that dispatch.
3. The D3 text re-anchor is the conductor's (`refactor/d3-amendment-draft.md` §1 P7); this goal
   owns only `docs/TODO.md`'s F2-exclusion text update.
4. The compose refusal rows (`daemon/e2e/production_linux_v2_rootless_compose_witness_test.go`
   and `scripts/ci/rootless-compose-native-acceptance.sh`) are byte-unchanged and stay green:
   they exercise the `harness.NewOpenCodeAdapter()` constructor seam, whose sentinel and
   projection shape are unchanged; the sentence now names `2.4.1` and is pinned by
   `TestComposedOpenCodeRefusalSentenceMatchesTheUnavailableSeam`, not by the compose rows.
5. **Version gate (recorded ruling).** P6.1 says the version gate keeps its
   `ErrUnsupportedVersion` contract; the conductor review says Linux keeps exactly today's
   `ErrHarnessUnavailable` classification and reason class. Resolved toward the conductor
   review, and since the R9 fix the composition root never probes the native adapter off
   Windows (the seam is registered instead), so the Linux registry path performs no native I/O
   at all; the adapter's off-Windows refusals keep the same class for direct construction.
6. Residuals: the digest binds the executable at probe time, not at launch (the pi/codex shape;
   a local writer could swap the blob between startup and assignment); the readiness identity is
   checked at Open, not re-checked per turn; the terminal-candidate witness of §6 is dormant and
   its scenarios were not re-run on this host; `--port 0` is live-verified but absent from the
   committed receipts; the outcome readback is bounded at 30 s and an unsettled or disagreeing
   readback refuses the turn as unverified rather than guessing; `session.step.*` and
   `session.tool.*` frames are journal-class evidence only. None of these weakens a promoted
   operation.
7. Fenced, unchanged: truncated responses stay `unavailable` (no schema bound), usage stays
   unreconciled, and SSE reconnect/replay, retained-session recovery, resume, handoff,
   guidance/steer, safe pause, the approval bridge, hard cost limits and provider brokering stay
   unsupported; Linux containment is not promoted by any Windows or WSL2 receipt; V1.x stays
   skipped. No OpenCode frame outside `session.execution.*` can settle a turn, and no decoded
   step/tool frame is a capability claim.

## 8. Review corrections

Implement round (before the first gate sweep): Sol review, security pass and model-diverse
audit; their findings changed the bytes (e2e pin reverted, promoted keys removed from
`Unsupported`, refusal paths validate their projection, Linux parity and registry pin added,
probe-deadline root cause fixed, comments trimmed).

Fix round (2026-11-26, review R1-R9 with the conductor ruling): R1 re-anchor delivered per §1
and row 7; R2 §5 recomputed with shared docs cited by content; R3 stale comments rewritten; R4
receipt-binding test added; R5 test renamed and the unverifiable provenance claim dropped; R6
cover digest corrected; R7 §7.4 reworded; R8 over-cap comments trimmed; R9 composition-root
platform gate and the tolerated probe-cancellation class added, each with a pin (M7/M8). The
conductor brief's own re-check then required the changed test files to be deduplicated for the
duplication gate (§1, §4), added pins for the remaining new guards (M9-M11), ran the load
repeats, and re-ran every row and the live proof on the deduplicated bytes.

Round 2 (2026-11-26, PR #298 round-1 review: Sol `298-opus-review-1.md`, DS `298-ds-audit-1.md`,
security `298-sec-review-1.md`, both Codex threads; conductor ruling MUST_FIX F1-F6): every item
is dispositioned in §10 with its delivered change, pin and mutant. The round also fixed the
defect the live proof itself caught (§2 row 7) and re-measured the censuses (§2 row 5, §4).

## 9. Apply sweep (2026-11-26)

The accepted simplification findings were applied on the delivered bytes, each with read-only
evidence in the review report: the blanket refusal sentence became operation-scoped; the
readiness-line comment cites the frozen fixtures' prefix spelling; the probe-cancellation guards
keep the seeded projection; three pins were added (composed promotion row with the literal
`opencode-http-v2` identity, the byte-equal refusal sentence row, the cancelled/expired probe
row); the duplicated version/help probe fixtures were hoisted; the Windows and WSL gate rows
were re-run on those bytes; M1-M3 were re-run and restored; the race row's local environment
limitations were recorded (now §4).

## 10. Round-2 review disposition (PR #298 round 1, MUST_FIX F1-F6)

Inputs: Sol review (`298-opus-review-1.md`), DS audit (`298-ds-audit-1.md`), security review
(`298-sec-review-1.md`), both Codex threads, and the conductor ruling of 2026-11-26. Every item
below names its delivered change, its pin and its mutant; the gate rows are §4.

- **F1 (P0, CI red)** — `openCodeProbeDidNotFinish` dropped the caller's context sentinel.
  Delivered: the typed `probeNotFinishedError` (Unwrap → tolerated class, Is → sentinel);
  `adapter_test.go`'s cancelled/expired row now asserts `errors.Is(err, context.Canceled|
  DeadlineExceeded)` instead of denying it. Pins: `probeconformance` (4 opencode rows), the
  inverted adapter row, `TestStartupProbeToleratesAnExpiredOpenCodeProbe` and
  `TestStartupProbeRejectsUnexpectedJoinedProbeError` (both unchanged and green: the join
  rejection contract survives because `probeErrorContainsOnly` is untouched). M12/M13.
- **F2 (P0, CI red on Linux)** — the composed Goal-assignment row asserted the Windows-only
  adapter and the truth table's production projection bypassed the composition root. Delivered:
  the `runtime.GOOS != "windows"` skip mirroring the foreign-blob sibling, and a per-platform
  production projection (native adapter on Windows, `harness.NewOpenCodeAdapter()` elsewhere)
  with the platform sentence in the expected row. Pin: the truth-table row itself, red on Linux
  before the change (DS reproduced it).
- **F3 (P1, both Codex threads)** — `Start` refused every valid Goal admission. Delivered: the
  admitted alias is accepted exactly as the Claude adapter accepts it (no native model claim;
  the daemon enforces `admission.model_profile == runtime.agent_profile` before `Start`). Pins:
  the reworked `TestStartRejectsUnsupportedOrUnsafeInputsBeforeLaunch` acceptance row and the
  live proof's non-empty `ModelProfile` turn (row 7).
- **F4 (P1, both Codex threads; security F1)** — the promoted event surface interpreted nothing
  from the pinned bytes and `WaitTurn` tore every turn down. Delivered: the recorded-vocabulary
  decode, the outcome readback, and the verified-terminal barrier described in §1; the two
  re-anchorings the evidence forced (`durable.seq` accepts the recorded 0; the 1.18.32
  terminal-candidate witness expects `ResultUnknown` for every scenario) and the 1.18.32
  decoder census are in §1 and §6. Pins: the receipt-frame decode/census rows, the terminal
  payload rows, the validator replay rows, the barrier rows, the readback client rows, and the
  foreign-inbox identity row. M14/M15/M16. Both Codex threads are closed by this item.
- **F5 (P1)** — the cited receipt did not bind the PR head. Delivered: the two-operation live
  proof re-run on the round-2 bytes; the receipt's `delivered_sha256` equals the six delivered
  files byte-for-byte (row 7, §5a). The run also caught a real defect (Close interrupting an
  already-succeeded turn), fixed and pinned in the same round.
- **F6 (P3)** — host-layout defaults in the proof, stale CI labels, stale `sse.go` comments, and
  non-reproducible censuses. Delivered: explicit absolute env paths for every operator input;
  the readiness-route wording in the folded-gate label and the provenance sentence; the
  re-anchored `sse.go` comments (part of F4's rewrite); and the exact-command censuses (§2 row
  5, §4). Race attribution: §4 records the round-2 full-suite results per platform.

## 11. Round-2 gates (exact commands; cwd; exit)

All rows below ran on the round-2 delivered bytes (§5a). Round 2 changes no shell script
(`git diff --name-only` over `scripts/` is empty), so the `bash -n` gate has no changed script to
cover; `.github/workflows/ci.yml` is YAML and the yaml-load row below covers it.

| Command (as run) | cwd | Exit |
| --- | --- | --- |
| `gofmt -l .` | `daemon/` | 0 (no output) |
| `go vet ./...` | `daemon/` | 0 |
| `go test -count=1 -timeout 1800s ./...` (Windows, full module) | `daemon/` | 0 (237.9 s) |
| `go test -count=1 -timeout 300s ./internal/harness/probeconformance/ ./internal/harness/opencode/` (Windows) | `daemon/` | 0 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` and `go build ./...` (WSL) | `daemon/` | 0 and 0 |
| `go test -count=1 -timeout 1800s ./...` (WSL, full module; CI posture: user `accept`, uid 1000, passwordless sudo, `TMPDIR=/tmp`) | `daemon/` | 0 — 30/30 packages ok, 169 s |
| the seven `--assert` names through `go-test-json-gate.py --stdio-boundary` over `go test -count=1 -timeout 300s -json ./internal/harness/pi ./internal/harness/opencode` (WSL, CI form verbatim) | `daemon/` | 0 — every name `run=1 pass=1 skip=0`, no other failing test or package |
| the race row, named assertion scoped to its package: `go-test-json-gate.py --assert …/opencode TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./internal/harness/opencode -run '^TestOpenCodeV2VerifierRejectsReleasedPeer$'` (WSL) | `daemon/` | 0 — `run=1 pass=1 skip=0` |
| the CI-exact race form: `go-test-json-gate.py --stdio-boundary --assert …/opencode TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./...` (WSL) | `daemon/` | 0 — 603 s total; `internal/app` 557.9 s of its 600 s budget; every package passes under `-race` |
| env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` (round 2, two operations) | `daemon/` | 0 (23.57 s; receipt §2 row 7) |
| `python C:/Users/lhan/.local/state/gdd/scratch/comment-cap.py <worktree>` (conductor's script, before the final re-measure) | repo | 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | repo | 0 (55 tests OK in 35.5 s) |
| `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"` (WSL) | repo | 0 |
| `git diff --check 550f96a3c128d3f3e397c6f03f6e003be6dfbd44` | repo | 0 |
| the `1.18.32` and pin-scope censuses (§2 row 5) | repo | as recorded there |
| the added-line host-path scan over the goal's paths (shapes: account prefix, drive letters, `/mnt`, `/home`, `/var`, `/tmp`) | repo | 0 hits; the range-wide scan over the merge-inclusive diff finds exactly one added line in another goal's file (`daemon/internal/harness/claude/adapter_cmdline_linux_test.go`), outside this goal's paths and untouched by it |
| mutants M1/M2/M12-M16 (§3a, Windows) | scratch `daemon/` copies | expected red, observed |
| mutant M3 (§3a, WSL) | scratch `daemon/` copy | expected red, observed |

WSL posture notes (three runs, same delivered bytes): as **root** the suite fails 13
`internal/platform` owner-anchor rows with `wire harness requires an unprivileged A process:
uid=0 euid=0 gid=0 egid=0` for every one of them; as an **unprivileged user without sudo** the
suite fails 6 supervisor rows (`Linux supervisor recovery endpoint is invalid`, they need the
privileged path) plus 2 rows whose fixture socket path exceeded `sun_path` under the nested
`TMPDIR` this measurement first used (`bind: invalid argument`, 109 > 108 bytes — a harness-setup
artifact, gone once `TMPDIR=/tmp`); as the **CI posture** (unprivileged user with passwordless
sudo, `/etc/sudoers.d/accept`) all 30 packages pass, which is the row above. The package suite is
untouched by this increment (`git status` lists no `internal/platform` file) and the root-posture
anchor failures reproduce the fix round's §4 classification.

Diagnostic (not a gate): a first WSL `go test ./...` attempt ran while a second agent was driving
its own full-module suite and build load on this host; its `opencode.test` binary was observed
parked in `futex_do_wait` at 0 % CPU past the point the isolated package needs, and the attempt was
aborted by the runner's own command deadline before the module finished (the suite row above is
measured by the clean CI-posture run, not by that attempt). The isolation check at the delivered
bytes: `go test -count=1 -timeout 300s -json ./internal/harness/opencode/` in WSL → exit 0, 409
JSON events, every `run` matched by a `pass`, ~30 s wall.

Race attribution (supersedes the fix-round §4 note, which recorded the race form as
environment-limited): the round-1 CI reds were the two deterministic defects F1
(`probeconformance` cancellation, red in the ordinary and race jobs) and F2 (the Linux app row),
not race-detector findings. Both are fixed, and with the two local environment confounds removed —
the root posture (the 13 `internal/platform` anchor rows demand an unprivileged A process) and a
concurrent agent running its own full module suite on this host — the CI-exact race form now runs
to completion locally green (row above). Residual risk recorded, not hidden: `internal/app`
measured 557.9 s of the job's 600 s per-package budget under `-race` on this host, so a slower CI
runner could time that package out on this budget; that pressure predates this increment (the
package's race time is dominated by the `LinuxV2NativeWitness*` rows, which this increment does
not touch).
