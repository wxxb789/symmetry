# rf-opencode-phase1 — OpenCode V2 2.4.1 scoped Phase-1 promotion (Windows surface; Linux fail-closed)

Delivered 2026-11-26 on branch `refactor/rf-opencode-phase1`; dispatch-gate base revision
`9eb874bf22eace98399e54b25d00634b2bed5418` (`origin/main` at the merge of
`rf-opencode-230-hostname-redact` #291). Contract `goal-rf-opencode-phase1.md`; accepted plan
`docs/plans/2026-11-25-rf-opencode-phase1-plan.md`
(sha256 `e6207d7a104ab8327bb2a3e65feb7b75a6dabba37d2d2392ab046d9d1984fb87`); proposal of
record `refactor/opencode-phase1-amendment.md` (GDD state root). The fix round below re-anchored
the client under the conductor ruling of 2026-11-26 (review R1-R9); its comparison base is
`550f96a3c128d3f3e397c6f03f6e003be6dfbd44` (`origin/main` #294).

Round 2 (2026-11-26) re-ran the implement stage under the conductor's second ruling (PR #298
round 1, MUST_FIX F1-F6); its comparison base is `6a0afd5f894468b148ba81209993ae05a3599e5f`.
Round-2 changes are the F1-F6 items mapped in §10; the live proof (§2 row 7) was re-run on the
round-2 bytes and is receipt-bound to them.

Round 3 (2026-11-26) applied the second simplification findings F1-F5/PD1-PD7 (§12); the live
proof was re-run on the round-3 bytes and §§2/4/5a/11 carry the round-3 re-measurements.

Round 4 (2026-11-26) applied the gdd review-3 findings STD-A01..A13 with the conductor's
dispositions (§13); its comparison base is `d63d6e0013673a937fd70a643b960f48d031f166`, and the
live proof (§2 row 7) was re-run on the round-4 bytes.

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
- P6.3 CI assertions: the six `--assert` rows and the race row stay named, with three test names
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
  readiness test renamed (`TestReadinessAcceptsTheRecordedLineAndTheFrozenFixturePrefix`, renamed
  again to `TestReadinessAcceptsTheRecordedLineAndTheInlinePrefixedForm` in round 4 under A04) and
  no longer claims a recorded prefixed 1.18.x line; the two over-cap comments in
  `daemon/internal/app/harness_test.go` are trimmed. The round-2 branch-wide claim was wrong: the
  conductor's comment-cap gate exits 1 on the round-2 bytes (three 3-line blocks); §12 trims them
  and the re-run exits 0.
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
  - F4: the decoder classifies every durable type in the committed 2.4.1 receipts (completed in
    fix round r4, STD-A03): `session.execution.*`, `session.inbox.enqueued`,
    `session.step.started|ended|failed` and `session.tool.called|success` decode to typed kinds,
    and the remaining recorded families (`session.step.first-token|streamed`, `session.tool.input.*`,
    `session.inbox.delivered`, `session.instructions.updated`, `session.reasoning.*`,
    `session.text.*`, `session.created|renamed|deleted`) are known-and-deliberately-uninterpreted,
    journaled with no unknown diagnostic; only types absent from the receipts stay
    `SessionEventUnknown`. `client.go` gained the recorded `GET /api/session/{id}` outcome
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
- **Fix round r4 (gdd review 3 on `d63d6e00`: MUST_FIX 4×P2 + 9×P3; per-finding dispositions in §13).**
  - A03: every durable type in the committed 2.4.1 receipts is classified — the 13 previously
    unknown recorded types are known-and-deliberately-uninterpreted
    (`recordedUninterpretedEventTypes`), journaled as native frames instead of
    `opencode_unknown_session_event`; the recorded success session produces zero unknown
    diagnostics (M18 reddens the rows). §7 residual 6's step/tool sentence is corrected.
  - A04: the `listeningLinePattern` comment and the readiness test name/doc now name the inline
    synthetic streams; the unverifiable frozen-fixture-prefix claim is dropped from production and
    test text.
  - A10: `testedLinuxExecutableHash` is receipt-backed by the committed `probe-linux.json`
    (`TestOpenCodeTestdataBindsTheRecordedLinuxDigest`, sharing the hoisted receipt helper with the
    Windows row; M19 reddens it).
  - A11: `TestWaitTurnSettlesTheRecordedProviderFailureTerminal` drives the committed
    provider-failure receipt's own frames through the watcher, so the recorded
    `session.execution.failed` payload settles the barrier against the readback and `currentResult`
    reports `failed` with `unknown_outcome` (M20 reddens it).
  - A09: the composed Goal-admission no-effect row runs on every platform (native refusal on
    Windows; the platform sentence and the unavailable seam off Windows) and the foreign-blob app
    row gained the wrong-version fixture case; each asserts zero workspace/process/session/event
    effects.
  - A06: the guest `opencode` is staged through `stage_guest_binary` with the pinned ELF digest, so
    the landed file is digest-checked like every sibling binary.
  - A01/A07: §5a digests recomputed at the delivered bytes with the delivered revision named; §5b
    reworded. A02: `local_goals_diff.txt` deleted (`git rm`). A05/A08: recorded as `docs/TODO.md`
    residuals per the conductor's disposition, with no code change. A12: the `probeconformance`
    opencode row's `probeTimeout` mirrors the 5 s package bound.

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
`TestRecordedOutcomeReadbackRefusesWhenTheSessionEndedFirst` (M14/M15). Round 4 adds:
`TestOpenCodeTestdataBindsTheRecordedLinuxDigest` (A10; M19 reddens it),
`TestWaitTurnSettlesTheRecordedProviderFailureTerminal` (A11; the committed provider-failure
receipt's own frames through watcher → settle → readback, red under M20), and the app-level
wrong-version composed row (A09, row 3).

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
AdmissionForAForeignInbox` pins the 2.4.1 inbox identity binding (M16). Round 4 (A09): the
composed Goal-admission no-effect row now runs on every platform — Windows pins the native
adapter and `opencode version probe failed`, off Windows the unavailable seam and the platform
sentence, both with zero workspace/process/session/event effects — and the foreign-blob app row
gained a wrong-version case that pins the startup `ErrUnsupportedVersion` refusal
(`OpenCode 2.3.0`) with zero claims, sessions and events.

Row 4 (Linux containment; standing condition, **recorded unmet**): no dispatched
`linux-native-v2-acceptance` `opencode_native_health` run was attempted — the conductor ruled
"no dispatched Linux run in this goal; do not attempt it", and no qualifying-host decision
exists. No run id or subject sha256 is recorded; no Linux promotion is claimed.

Row 5 (pin drift): the carried pin literals are `2.4.1` —
`linux-v2-native-tools.sh` names `opencode-linux-x64.tar.gz` `9ddd18e3…` → ELF `ec93043f…` →
`opencode v2.4.1`; the acceptance fixture's `harness_version` map names `2.4.1`; the witness
tested-release check accepts `2.4.1` via `opencode.TestedVersion`. Census, exact command:
`git diff 550f96a3c128d3f3e397c6f03f6e003be6dfbd44 -U0 -- <the 28 §5a table paths, this record
excluded> | grep -E '^\+' | grep -vE '^\+\+\+' | grep -cE '1\.18\.32'` → **11** (re-measured at
the round-4 bytes; the round-4 sse.go comment rewrite replaced two existing hits one-for-one),
and every hit is
a historical reference, not a carried pin: four comments
(`daemon/internal/harness/opencode/sse.go`), two witness messages
(`daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go`), one synthetic
profile command string (`daemon/internal/app/harness_test.go`), the D1 row
(`docs/goals/0010-refactor-program.md`) and three lines of the plan's round-2 section. The same
census over the full range (this record included) → **18** (re-measured at the round-4 bytes;
this record's round-4 prose adds one quoting line): this record's own prose quotes the
pin literal and makes up the difference. Pin-scope census, exact command: `grep -rn '1\.18\.32'
scripts/ci/linux-v2-native-tools.sh scripts/ci/linux-v2-native-acceptance.sh
.github/workflows/ci.yml daemon/internal/harness/opencode/adapter.go` → **0 hits**: no carried
pin names V1.

Row 6 (revision binding): §5's delivered-element digests plus the amendment merge revision or a
post-merge re-run.

Row 7 (Windows live promotion proof; re-run in round 4 on the delivered bytes): the opt-in
env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` ran against the exact pinned Windows
PE (sha256 `f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e`, 278,383,496
bytes, digest re-verified by the test before launch) with the recorded relay provider and model
on this Windows host, driving two admitted turns behind a non-empty `ModelProfile`
(`implementation-default`; the daemon-validated admission-alias shape F3 requires). No live
daemon `goal_admission_tx` run exists; as the conductor's F5 note accepts, the Goal-admission
evidence is the adapter-boundary proof with the daemon's admission alias plus the committed
admission semantics, and this record claims no more:

- succeeded operation: Open (the retained client accepts the launched pid and refuses `pid+1`
  with `ErrInvalidIdentity`, then creates the session), `StartTurn` with the recorded inbox
  admission observed on the global stream (15 native frames after the round-4 classification),
  `WaitTurn` returned the verified `session.execution.succeeded` terminal, the client's
  `GET /api/session/{id}` readback named `succeeded`, `Close` terminated the server cleanly and
  `Wait` returned `succeeded` with no semantic result and unknown usage;
- interrupted operation: the same open/admission path (6 native frames), the late-goal turn
  cancelled through `Control` (a 2.4.1 interrupt arriving before the first generated token is
  refused, not queued, so the proof re-applies until the server reports it applied), `WaitTurn`
  returned the verified `session.execution.interrupted` terminal, the readback named
  `interrupted`, and `Wait` returned `cancelled` with `TaskResultReasonCancelled` (round-4
  re-run, 17.06 s wall).

Receipt: `<STATE_DIR>/scratch/rf-opencode-phase1-fix1/promotion-receipt.json`, sha256
`399034a4c4bdf2eea365f5957e151bde9f46d88cd930ade1bd65582867267877`. Its `operations` block
carries `wait_turn: verified`, `readback_outcome`, `result_kind`, `control_applied`,
`native_frames`, the readiness booleans and `close_terminated: true` per operation, plus
`relay_host_recorded: false` and `model_profile_alias_used: true`. Its `delivered_sha256` block
equals the delivered bytes byte-for-byte: `adapter.go` `b12c3bab1337…e5e0`, `client.go`
`68de7e92df51…9da3`, `sse.go` `5a96edbb2ad9…76f7`, `types.go` `d1a5d42b33f9…a84d`,
`probe_promotion_test.go` `8319eacac420…6244`, `adapter_native_promotion_test.go`
`af362c247a2f…43cb` (§5a). The credential was read at run time from the operator's OMP models
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
skipped. Each mutant was run in a scratch copy of `daemon/`. The restores above name the bytes the
mutants actually ran on: `adapter.go` `65174205…2ac93a`, `client.go` `b9bb6894…d46c2c` and
`sse.go` `7e6bcf7f…8fb9f94` are the `5b716a61` intermediate round-2 bytes, one edit before the
round-2 delivery; §5a carries the delivered round-3 digests and §12 re-ran no mutant
M1-M16. Oracle note: neutering the post-open closed
re-check in `StartTurn` leaves the suite green — the cancelled stream context fails the open
before the re-check — and that predicate is carried from the comparison base, so it needs no
new pin; the concurrent-close row is pinned through the cancellation path
(`TestCloseCancelsInFlightGlobalStreamOpen`).

### 3a. Round-2 mutants (fresh scratch copies of the round-2 bytes; the delivered tree is untouched)

| Mutant | Mutation (scratch) | Check that reddened (observed) |
| --- | --- | --- |
| M1 re-run wrong blob digest | `TestedExecutableHash` ← the Linux hash | `TestOpenCodeTestdataBindsThePromotedWindowsDigest` (receipt blob `f3da11bf…` ≠ mutated constant) and `TestProbeRejectsWrongBlobDigest` |
| M2 re-run wrong `--version` | the `version != TestedVersion` branch deleted | `TestProbeRejectsWrongVersionAndIncompleteHelp` (`adapter_test.go:162` at the round-4 bytes, want `ErrUnsupportedVersion`) |
| M12 (F1) sentinel dropped | `openCodeProbeDidNotFinish` back to the fix-round `%v` shape (no typed error) | `probeconformance`'s opencode cancellation rows (`cancellation_test.go:166`, want `context.Canceled`) and `TestProbeKeepsSeededProjectionWhenTheProbeIsCancelledOrExpired` |
| M13 (F1) `Is` neutered | `probeNotFinishedError.Is` returns false | same `probeconformance` rows and the same adapter row: the `Is` method is what exposes the sentinel alongside the tolerated class |
| M14 (F4) readback verification skipped | `settleRecordedTerminal` treats every terminal as verified | `TestWaitTurnSettlesRecordedTerminalsAgainstTheOutcomeReadback/readback_disagrees` (WaitTurn returned nil for a disagreeing readback) |
| M15 (F4) verified-close guard removed | `verifiedTurn := false` in `closeOnce` | the same test's `interrupted` row: `Close() error = opencode interrupt did not apply to a live turn` |
| M16 (F4) inbox identity check removed | the `session.inbox.enqueued` identity branch deleted | `TestStartTurnRefusesARecordedAdmissionForAForeignInbox` (the foreign admission was journalled and the turn never refused) |
| M18 (round 4, A03) recorded type dropped | the `session.step.first-token` entry deleted from `recordedUninterpretedEventTypes` (`sse.go`; `go test -overlay` on a scratch copy) | `TestRecordedReceiptFramesDecode` (`windows-2026-11-25-r2.json` and `linux-2026-11-25.json`: kind `unknown`, want `recorded_uninterpreted`) and `TestRecordedSuccessSessionProducesNoUnknownDiagnostics` (12 native, 1 unknown) |
| M19 (round 4, A10) Linux digest zeroed | `testedLinuxExecutableHash` ← the all-zero digest (`adapter.go`; `go test -overlay`) | `TestOpenCodeTestdataBindsTheRecordedLinuxDigest` (receipt digest `ec93043f…`, want `sha256:000…`) |
| M20 (round 4, A11) failed terminal dropped | `executionOutcome` returns not-a-terminal for `session.execution.failed` (`sse.go`; `go test -overlay`) | `TestWaitTurnSettlesRecordedTerminalsAgainstTheOutcomeReadback/provider_failure` and `TestWaitTurnSettlesTheRecordedProviderFailureTerminal` (`currentResult()` unknown, want failed) |

Every mutated file lived in its own scratch copy (`<STATE_DIR>/scratch/rf-opencode-phase1-impl2/mut/<mutant>/`);
no delivered byte was ever mutated, so "restored byte-identical" is satisfied by construction and
the delivered digests are §5a's. M3 was re-run under WSL (Linux rows) — see §11.

M18-M20 (round 4) ran through `go test -overlay` against mutated copies under
`<STATE_DIR>/scratch/rf-opencode-phase1-fix1/mut/<mutant>/` (the overlay JSON maps the delivered
path to the mutated copy; `git status` stayed unchanged during the runs, so restored byte-identical
holds by construction). They redden in both the `daemon` job (ubuntu-latest, non-root) and the
`daemon-windows` job (windows-latest, non-root), which both run the opencode package suite. M8's
re-run under WSL (registry platform gate removed) reddens the round-4 off-Windows composed
Goal-admission row in the `daemon` job; M18-M20 commands and the M8 re-run are in §14.

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
| range added-line host-path scan (`git diff 550f96a3…`, added lines, shapes: account prefix, drive letters, `/mnt`, `/home`, `/var`, `/tmp`; the two untracked files scanned whole) | repo | 0 hits in this goal's paths; one added line in another goal's file (a runner home path in `daemon/internal/harness/claude/adapter_cmdline_linux_test.go`) disclosed in §11 |
| account-name scan over the range's added lines (shapes described in prose; the pattern is not quoted here) | repo | 0 hits after §12 replaces this record's one quoted state-dir path; the round-2 two hits were that path and this row's own quoted pattern |
| the V1-pin census over the range's added lines (exact commands and path set in §2 row 5) | repo | as recorded there; no carried pin |
| comment-block census over the range's added lines (cap: two lines) | repo | round-2 bytes: 1 — three 3-line blocks over the cap; §12 trims them and the re-run exits 0 |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | repo | 0 for the record and six other changed docs; `docs/archive/CLOSED-TODO.md` refuses at the comparison base and at the delivered bytes with the same pre-existing finding (specimen lines), and the range's added lines carry none |
| the R4 receipt-binding test (opencode package suite above) | `daemon/` | 0 |
| mutants M1-M11 per §3 | scratch `daemon/` | expected red; restores digest-verified |

## 5. Delivered-element digests

### 5a. sha256 rows (row 6 re-binds at the merge)

The revision column names the bytes each row was measured at: `r1` = untouched by round 2 and
byte-equal to the committed `6a0afd5f` blob (`git show 6a0afd5f:<path>`); `r2` = the round-2
delivered bytes; `r3` = the round-3 apply bytes (§12); `r4` = the round-4 delivered bytes
(§13). Every row was recomputed at the round-4 delivered bytes after the last code edit; an
`r1`/`r2`/`r3` label means those bytes are unchanged and byte-equal to that round's measurement.
The live proof's `delivered_sha256` block equals the six code files' delivered bytes below
(§2 row 7).

| File | Revision | sha256 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | r4 | `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7` |
| `daemon/internal/app/adapter_policy_truth_table_test.go` | r2 | `e220b844e75f32d46f13897b55bcb07f20246b5a2177eb9dcb2310548069476c` |
| `daemon/internal/app/harness_test.go` | r4 | `ec3b044990eef13a36e01363b71f52d54a3f0b36034aaae7ab911f3c6db9ca78` |
| `daemon/internal/app/probe.go` | r1 | `0ccbafd4f026472770e5f9e4a9da8146797a7049c62f95c1ba2d19a10e24ff49` |
| `daemon/internal/harness/harness_test.go` | r1 | `3cb64f1c20af0d5063272e63e1a4ae2f9751687826b67c25ff060ce55cc76759` |
| `daemon/internal/harness/probeconformance/cancellation_test.go` | r4 | `53af150adffe70eef750ecebbddd14189721910697d7a3dccea3f2bd768bd5bc` |
| `daemon/internal/harness/opencode/adapter.go` | r4 | `b12c3bab1337a291a391949304090965f724d1266f29ab1a2b4f2d2f9e23e5e0` |
| `daemon/internal/harness/opencode/adapter_native_promotion_test.go` | r3 | `af362c247a2f119cd651391c858d412a91f8549bad55d4e05d89c730700443cb` |
| `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go` | r3 | `e85f57e0565f77c6fa3e2c3afe39ec7580baffc926dff8519a5b91c0a25fc485` |
| `daemon/internal/harness/opencode/adapter_native_test.go` | r3 | `c84dc0bfe8985ad5d89c2879f2262ddea5d8cf9698d3e4d455cacfadbbc6e1ab` |
| `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` | r3 | `0b47483117477b7bcd5b9ff31a6213d7fd6b09da5b529f8ec58c170962a85512` |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | r3 | `2cc8d159e954125f8832e16810e8eed73bac03b86d4ab098e0d0a9c675b3cee9` |
| `daemon/internal/harness/opencode/adapter_test.go` | r4; re-measured after a comment-only reflow (3 docstrings deleted, one hazard line added; no code changed) | `8713e403c56dfe5e1be68d55ba3401a33ee583174cc8919cc0e451746c93eb23` |
| `daemon/internal/harness/opencode/client.go` | r3 | `68de7e92df5131bf4df1d156e80bea029e1538fac5a03ccdbc5276486f219da3` |
| `daemon/internal/harness/opencode/client_test.go` | r2 | `150ac7466a9d755f1c39671718ab2517bbf768a7cfda54fdc2f320169ef1b6d7` |
| `daemon/internal/harness/opencode/inprocess_containment_test.go` | r1 | `9f3bdff150af99fc3775468105ed4270261ced19ef0c20b6ba19382275ace7d7` |
| `daemon/internal/harness/opencode/peer_linux_test.go` | r1 | `e4b464c0d7adf47959012c8be803cdf31c03efe0b3c5dd94ff202d5369ada362` |
| `daemon/internal/harness/opencode/peer_test.go` | r1 | `1a267d6874a3c2fd067c6c90318db1fa934a30909c0dfb283af82d388606e7ff` |
| `daemon/internal/harness/opencode/probe_promotion_test.go` | r4 | `8319eacac4200380a78341bdcc79ab55b9366c45c6d7769446caeaf8e3eb6244` |
| `daemon/internal/harness/opencode/sse.go` | r4 | `5a96edbb2ad9eb344fe2eb892b7b60eb9769a60274c1521b31650792496676f7` |
| `daemon/internal/harness/opencode/sse_test.go` | r4 | `1c18d325c76e3a7fd11a6a91d11147c756b2f5e49193c53ae2f22448cc5cdd15` |
| `daemon/internal/harness/opencode/types.go` | r4 | `d1a5d42b33f94514ddadabf3d75c3a39afca308376ac7755c4fd5e66d605a84d` |
| `daemon/internal/harness/registry.go` | r1 | `c8049e6eced97a11d325b78aff7ec9c6aa77102f1397f799164a8646d6e2ddcc` |
| `docs/goals/0010-refactor-program.md` | r1 | `49a9f0335020773716d2f884bd6f904fb78c81bfa2b63ba72ae3b1d24be22e37` |
| `docs/goals/README.md` | r1 | `5f2a6292408648d87d50f78a07d3c4c623bb3f87fa49ee22b4bf377e7bc6edd4` |
| `docs/plans/2026-11-25-rf-opencode-phase1-plan.md` | r3 | `e6207d7a104ab8327bb2a3e65feb7b75a6dabba37d2d2392ab046d9d1984fb87` |
| `scripts/ci/linux-v2-native-acceptance.sh` | r4 | `6c7efcf062a3190a2559e35505c38cf036bd62a40f6f889bd6724733c33c973b` |
| `scripts/ci/linux-v2-native-tools.sh` | r1 | `500ac7b47dd9a9b0f5aea2d67dafe4a0131dfd10726b0dc4b0215f4f1b410c69` |

### 5b. Shared docs cited by content (main merges rebind them; never by sha256)

- `docs/TODO.md`: no longer carries the two removed consolidation entries (pure deletions; the
  entries and their closure notes are in `docs/archive/CLOSED-TODO.md`); it carries the re-anchored
  F2-exclusion text for the conductor's D3 amendment, the round-4 cross-harness residuals
  (launch-time exact-blob re-verification, the seq-0 replay assumption) and no OpenCode promotion
  item.
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
- Round-4 files: `daemon/internal/harness/opencode/{sse.go,sse_test.go,types.go,adapter.go,
  adapter_test.go,probe_promotion_test.go}`, `daemon/internal/app/harness_test.go` and
  `scripts/ci/linux-v2-native-acceptance.sh` are inside the contract's owned set or the
  conductor's per-finding extensions; `daemon/internal/harness/probeconformance/cancellation_test.go`
  is the conductor's A12 extension for that one fixture line, and `docs/TODO.md` carries the
  A05/A08 residuals (shared doc, cited by content). `local_goals_diff.txt` (a committed scratch
  `git diff` of `docs/goals/README.md`, added at `c4cbb8c6`, unreferenced) was deleted with
  `git rm` under the A02 disposition; it was never in the owned set or any record.
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
   a local writer could swap the blob between startup and assignment) — carried as a cross-harness
   `docs/TODO.md` residual (§5b); the readiness identity is checked at Open, not re-checked per
   turn; the terminal-candidate witness of §6 is dormant and its scenarios were not re-run on this
   host; `--port 0` is live-verified but absent from the committed receipts; the outcome readback
   is bounded at 30 s and an unsettled or disagreeing readback refuses the turn as unverified
   rather than guessing; the recorded frames outside the typed execution/step/tool kinds are
   journal-class evidence only (known and classified, never a capability or a settle). The seq-0
   pre-connect replay assumption is carried as a `docs/TODO.md` residual (§5b). None of these
   weakens a promoted operation.
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

All rows below ran on the round-2 delivered bytes (the §5a r2 rows) unless a row says otherwise.
Round 2 changes no shell script
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
| `python <STATE_DIR>/scratch/comment-cap.py <worktree>` (conductor's script) | repo | round-2 bytes: 1 — three 3-line blocks over the cap; §12 trims them and the re-run exits 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | repo | 0 (55 tests OK in 35.5 s) |
| `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"` (WSL) | repo | 0 |
| `git diff --check 550f96a3c128d3f3e397c6f03f6e003be6dfbd44` | repo | 0 |
| the V1-pin and pin-scope censuses (§2 row 5) | repo | as recorded there (re-measured in §12) |
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

## 12. Round-3 apply sweep (2026-11-26, second simplification findings)

Accepted findings F1-F5 and PD1-PD7 were applied on the round-2 delivered bytes; every number
below was re-measured on the round-3 bytes.

- F1 — deleted `Event.String()` (`sse.go`): it had no caller, and its value receiver was promoted
  into every type embedding `Event`, reducing `%v`/`%+v` failure output to the event type.
- F2 — dropped the vacuous session-identity clause of `TestRecordedReceiptFramesDecode`:
  `DecodeSessionEvent` returns only after `durableSessionIdentity` proves the id non-empty and
  equal to the durable aggregate, so the clause could not fire; the decode-error path and the
  mismatch semantics stay pinned one line above and in the mismatch row.
- F3 — the resume capture binds admission from the decoded frame (`SessionEventInboxEnqueued` +
  `InboxEnqueued.InboxID`) instead of re-parsing `data.inboxID`, which also matched
  `session.inbox.delivered` (the delivered frames of four receipts carry that field and decode to
  `SessionEventUnknown`); the lax helper is deleted and
  `TestNativeOpenCodeResumeCaptureBindsOnlyTheRecordedInboxAdmission` pins both arms.
- F4 — one version-check helper with an explicit timeout parameter returning the trimmed line;
  the remaining copies route through it with their original bounds preserved (`nativeSmokeTimeout`
  in `adapter_native_test.go`, `nativePromotionProofTimeout` in the promotion proof, 10 s in the
  terminal-candidate witness, `nativeRepositoryTaskTimeout` in the Linux work leg).
- F5 — the three 3-line comment blocks trimmed to two lines; the conductor's comment-cap script
  on the round-3 bytes exits 0 (gate row below).
- PD1/PD2 — this record's comment-cap claims are corrected in §1/§4/§11, and the one quoted
  state-dir path (the conductor's script invocation) is replaced with `<STATE_DIR>`.
- PD3 — the V1-pin census is re-measured with its exact path set and result (§2 row 5).
- PD4/PD6 — the cover re-cites the delivered plan digest; the re-anchored test-name count is
  corrected; the round-2 disposition pointer moves to §10.
- PD5 — §3's restore digests are labelled with the `5b716a61` intermediate revision they actually
  are, and the dangling §5 pointer is gone.
- PD7 — the plan's §2 and §8 carry appended superseded markers (the R9 platform gate; the R1
  re-anchor choice and the option-letter inversion).

| Command (as run) | cwd | Exit |
| --- | --- | --- |
| `gofmt -l .` | `daemon/` | 0 (no output) |
| `go vet ./...` | `daemon/` | 0 |
| `go test -count=1 -timeout 900s ./internal/harness/opencode/ ./internal/harness/ ./internal/app/` | `daemon/` | 0 (`opencode` 0.391 s, `harness` 0.207 s, `app` 91.236 s; one earlier run overlapped a 105 MB tree copy and saw `TestStagedDriverCloseDetachedRunsStartOnTheCaller` fail once — it passes `-count=3` isolated and the package passed three further unloaded runs) |
| `go vet ./...` and `go build ./...` (WSL `Ubuntu-24.04`, user `accept`, go1.27.0) | `daemon/` | 0 and 0 |
| `TMPDIR=/tmp go test -count=1 -timeout 600s ./internal/harness/opencode/ ./internal/harness/` (WSL) | `daemon/` | 0 (`opencode` 1.247 s, `harness` 0.042 s) |
| env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` (round-3 re-run; receipt §2 row 7) | `daemon/` | 0 (16.39 s) |
| `python <STATE_DIR>/scratch/comment-cap.py <round-3 tree>` (conductor's script; independent scratch commit of the round-3 tree) | scratch | 0 |
| `TestNativeOpenCodeResumeCaptureBindsOnlyTheRecordedInboxAdmission` on the M17 scratch copy | scratch `daemon/` | expected red, observed |
| `python scripts/ci/jscpd-changed.py --base 78166f2e --head HEAD` (scratch commit of the round-3 tree) | scratch repo | 0 — 32 changed files, 1771 scanned sources, 1253 corpus clones, 8 tolerated among the added lines, none new; the gate's own notices disclose two changed files carrying added lines outside its scope (`daemon/internal/app/harness_test.go`, `docs/archive/CLOSED-TODO.md`) |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only <file>` (this record; the plan) | repo | 0 and 0 |

M17 (red-first for F3): the strict admission binding replaced with the raw `data.inboxID` parse
in a scratch copy; the row reddens on its delivered-only case (the summary shows
`admission_observed=true` with the stream-ended stop reason, against the expected admission-missing
refusal). The mutated file is
`<STATE_DIR>/scratch/rf-opencode-phase1-apply3/mut/red-first/daemon/internal/harness/opencode/adapter_native_work_test.go`,
sha256 `da6b10a3db85d1b8dba726fb3895732e0ad5183c6a26c6416fb169bedd2d8f72`; the delivered tree was
never mutated, and no M1-M16 mutant was re-run in round 3.

## 13. Round-4 fix disposition (gdd review 3 on `d63d6e00`: MUST_FIX, 4×P2 + 9×P3)

Input: the review answer of `runs/2026-11-26/symmetry-rf-opencode-phase1-review` (findings
STD-A01..A13) and the conductor's dispositions of 2026-11-26 (contract `goal-rf-opencode-phase1.md`,
last ruling). Gate rows for this table are §14.

| id | disposition | delivered change and evidence |
| --- | --- | --- |
| A01 (P2) | fixed | §5a recomputed over the delivered bytes; the revision column gains `r4` and the cover names the fix round's comparison base `d63d6e00`. Evidence: the sha256 sweep of every §5a row against the working tree at the delivered bytes (§14), and the §5a equality note for the live proof's `delivered_sha256` block. |
| A02 (P2) | fixed | `git rm local_goals_diff.txt` (25-line committed `git diff` of `docs/goals/README.md`, added at `c4cbb8c6`). Evidence: `git ls-files local_goals_diff.txt` empty; the only remaining textual reference is this record's §6 sentence. |
| A03 (P2) | fixed | `recordedUninterpretedEventTypes` classifies the 13 recorded-but-uninterpreted types; `SessionEventRecordedUninterpreted` is a known kind and only types absent from the receipts stay `SessionEventUnknown`; the two untrue sentences are replaced (§1 F4, §7 residual 6). Evidence: red-first run before the production change (`TestRecordedReceiptFramesDecode/windows-2026-11-25-r2.json`: `session.created` kind `unknown`; `TestRecordedSuccessSessionProducesNoUnknownDiagnostics`: 5 native, 8 unknown), then green on the delivered bytes; M18 red (§3a). |
| A04 (P2) | fixed | The production comment and the test name/doc comment now name the inline synthetic streams; the frozen-fixture-prefix claim is gone. Evidence: `grep -rn` over `daemon/internal/harness/opencode` for the old claim phrases → 0 hits (§14); the readiness row keeps both forms green. |
| A05 (P3) | deferred by disposition | Recorded as a cross-harness `docs/TODO.md` residual (§5b) — launch-time exact-blob re-verification is missing identically in pi/codex/opencode, so a one-harness fix would fork the convention; owner conductor (new goal). No code change. |
| A06 (P3) | fixed | The guest `opencode` is staged through `stage_guest_binary` with the pinned ELF digest `ec93043f…`, so the landed file is digest-checked like every sibling binary. Evidence: `bash -n` exit 0 (§14); the guard itself runs only inside a dispatched `linux-v2-native-acceptance` run — oracle gap named in §14. |
| A07 (P3) | fixed | §5b now reads: `docs/TODO.md` no longer carries the two consolidation entries (pure deletions; `docs/archive/CLOSED-TODO.md` records them closed) and carries the round-4 residuals. Evidence: the §14 TODO grep rows. |
| A08 (P3) | deferred by disposition | Recorded as a `docs/TODO.md` residual (§5b): the seq-0 pre-connect replay would fail a promoted turn closed, it is speculative and fail-closed, and the cursor guard stays as-is. No code change. |
| A09 (P3) | fixed | The composed Goal-admission no-effect row runs on every platform (Windows native adapter + `opencode version probe failed`; off Windows the unavailable seam + the platform sentence; zero workspace/process/session/event effects), and the foreign-blob app row gained the wrong-version case pinning the startup `ErrUnsupportedVersion` refusal (`OpenCode 2.3.0`) with no journal and zero claims/effects. Evidence: Windows and WSL runs green (§14); M8's WSL re-run reddens the off-Windows arm (§3a/§14). |
| A10 (P3) | fixed | `TestOpenCodeTestdataBindsTheRecordedLinuxDigest` binds `testedLinuxExecutableHash` to the committed `probe-linux.json` digest through the helper hoisted for both platform receipts. Evidence: M19 red (§3a); green on the delivered bytes (§14). |
| A11 (P3) | fixed | `TestWaitTurnSettlesTheRecordedProviderFailureTerminal` renders the committed provider-failure receipt's durable frames into the watcher, so the recorded `session.execution.failed` payload settles the barrier against the readback and `currentResult` reports `failed` + `unknown_outcome`. Evidence: M20 red (§3a); green on the delivered bytes (§14). |
| A12 (P3) | fixed | The `probeconformance` opencode row's `probeTimeout` is `5 * time.Second`, matching `adapter.go`'s package bound. Evidence: the value equality is a read, not a runtime oracle — no mutation can redden a diagnostic string; the synchronised row stays green (§14). |
| A13 (P3) | fixed | The `sse.go` keep-justification now names the actual consumer set (the frozen 1.18.32 fixtures, the inline decode rows, the 1.18.32-pinned candidate witness) instead of claiming fixture coverage for every kept name. The two fixture-less names (`session.next.step.failed`, `session.next.tool.success`) are kept rather than deleted because both are replayed by in-tree rows (the candidate witness and the inline decode tables) whose deletion would remove pinned decode coverage; the conductor's disposition allows the reword. Evidence: the grep census of the kept-name consumers and the green suites (§14). |

## 14. Round-4 gates (exact commands; cwd; exit)

All rows ran on the round-4 delivered bytes (§5a) unless a row says otherwise; the fix round's
comparison base is `d63d6e0013673a937fd70a643b960f48d031f166`.

| Command (as run) | cwd | Exit |
| --- | --- | --- |
| `gofmt -l .` | `daemon/` | 0 (no output) |
| `go vet ./...` | `daemon/` | 0 |
| `go test -count=1 -timeout 900s ./internal/harness/opencode/ ./internal/harness/ ./internal/harness/probeconformance/ ./internal/app/` (Windows) | `daemon/` | 0 (`opencode` 0.810 s, `harness` 0.298 s, `probeconformance` 0.225 s, `app` 110.197 s) |
| `GOMAXPROCS=8 go test -count=200 -timeout 1800s -run 'TestRecorded(SuccessSessionProducesNoUnknownDiagnostics\|ReceiptFramesDecode)\|TestWaitTurnSettlesTheRecordedProviderFailureTerminal' ./internal/harness/opencode/` (Windows) | `daemon/` | 0 (5.200 s after the test-helper dedup; the first run on the pre-dedup bytes took 47.175 s including the build) |
| `go test -count=1 -timeout 600s ./internal/harness/opencode/ ./internal/harness/ ./internal/harness/probeconformance/` (WSL `Ubuntu-24.04`, go1.27.0, `TMPDIR=/tmp`) | `daemon/` | 0 (`opencode` 1.930 s, `harness` 0.055 s, `probeconformance` 0.027 s); `go vet ./...` and `go build ./...` → 0 and 0 |
| `TMPDIR=/tmp go test -count=1 -timeout 900s ./internal/app/` (WSL) | `daemon/` | 0 (149.235 s; the app bytes are `ec3b0449…`, unchanged by the round-4 test-helper dedup) |
| the seven `--assert` names through `go-test-json-gate.py --stdio-boundary` over `go test -count=1 -timeout 300s -json ./internal/harness/pi ./internal/harness/opencode` (WSL, CI form verbatim) | `daemon/` | 0 — every asserted name `run=1 pass=1 skip=0`; 550 `pass` actions in the events file |
| the scoped race row `go-test-json-gate.py --stdio-boundary --assert …/opencode TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./internal/harness/opencode -run '^TestOpenCodeV2VerifierRejectsReleasedPeer$'` (WSL) | `daemon/` | 0 — `run=1 pass=1 skip=0` |
| `go test -race -count=20 -timeout 900s -run '<the round-4 new rows>' ./internal/harness/opencode/` (WSL) | `daemon/` | 0 (20.066 s) |
| the CI-exact race form `go-test-json-gate.py --stdio-boundary --assert …/opencode TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./...` (WSL) | `daemon/` | 1 — `internal/app` reaches its 600 s per-package budget (`FAIL … 600.347 s`; the dump shows an app row still running at the deadline). Not diff-attributable: the package passes under `-race` with budget (row below), its race time is dominated by rows this increment does not touch (§11), and the added app rows measure ~1 s; the §11-disclosed host/budget pressure is unchanged |
| isolated probe `go test -race -count=1 -timeout 1500s ./internal/app/` (WSL; the timed-out package alone) | `daemon/` | 0 — `ok internal/app 1022.247 s` (1049 s wall of a 1500 s bound); the CI-exact form's 600 s per-package budget is host-dependent and round 2 measured 557.9 s on the bytes then (§11) |
| `python <STATE_DIR>/scratch/comment-cap.py <worktree>` (conductor's script) | repo | 0 |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only docs/evidence/…` and `docs/TODO.md` | repo | 0 and 0 |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | repo | 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | repo | 0 — 55 tests OK (48.98 s) |
| `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"` (WSL) | repo | 0 |
| `git diff --check 550f96a3c128d3f3e397c6f03f6e003be6dfbd44` | repo | 0 |
| env-gated `TestNativePromotionProofOnThePinnedWindowsBlob` (row 7) | `daemon/` | 0 — PASS in 17.06 s; receipt sha256 `399034a4c4bdf2eea365f5957e151bde9f46d88cd930ade1bd65582867267877`; its six `delivered_sha256` values equal the §5a r4 rows of `adapter.go`, `client.go`, `sse.go`, `types.go`, `probe_promotion_test.go`, `adapter_native_promotion_test.go` exactly (equality table re-run after the proof) |
| `python scripts/ci/jscpd-changed.py --base 7111828b --head <scratch head>` (scratch commit of the round-4 tree) | scratch repo | 0 — 32 changed files, 1779 scanned sources, 1252 corpus clones, 8 tolerated among the added lines, none new; the gate's own notices disclose two changed files carrying added lines outside its scope (`daemon/internal/app/harness_test.go`, `docs/archive/CLOSED-TODO.md`) |
| the A04 claim census over the opencode package and the readiness row (the dropped phrases) | repo | 0 hits |
| the V1-pin census and pin-scope census (§2 row 5; re-measured) | repo | 11 added historical references in the 28-path table; 18 over the full range; 0 carried pins |
| the A13 kept-name consumer census (`session.next.step.failed`: 1 constant + 1 inline row + 5 witness uses; `session.next.tool.success`: 1 constant + 6 inline + 3 witness) | repo | as counted |
| the A07 census (the two removed entry titles: 0 hits in `docs/TODO.md`; carried in `docs/archive/CLOSED-TODO.md`) | repo | 2 hits in the archive (the moved entries) |
| mutants M18-M20 (`go test -overlay`, scratch copies) | `daemon/` | expected red, observed (§3a) |
| the M8 re-run under WSL (`go test -overlay`, registry platform gate removed) | `daemon/` | expected red, observed — `m8_exit=1`; `TestOpenCodeGoalAssignmentFails…`: `OpenCode adapter = *opencode.Adapter, want the unavailable platform seam` |
| the added-line host-path scan: the fix's own lines, the worktree diff, and the range as a whole | repo | 0 in this goal's changed lines; the range carries two pre-existing other-goal absolute-path literals (a runner home path in the claude cmdline test, disclosed in §11; a temporary-directory leaf path in the linuxreadback local-repro test), both outside this goal's changed paths and untouched by it; the `users/` check over `git diff origin/main...HEAD` and over the worktree diff → 0 matches |
| the record's line anchors re-measured at the delivered bytes and cited with their file digests | repo | 3/3 — `adapter_test.go:162` is the version-refusal failure line (r4 `8713e403…eb23`, post-reflow), `cancellation_test.go:166` the cancellation failure line (r4 `53af150a…d5bc`), `linux-v2-native-tools.sh:76` the pinned-ELF digest check (r1 `500ac7b4…0c69`) |
| the A06 staged-guest digest guard (`stage_guest_binary` on the pinned ELF digest) | repo | **oracle gap**: the guard only executes inside a dispatched `linux-v2-native-acceptance` run and no qualified Linux host was used in this stage; the call and its digest were read against `linux-v2-native-tools.sh:76` and `probe-linux.json`, and `bash -n` covers the script's syntax only |
