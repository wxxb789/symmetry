# rf-opencode-phase1 — OpenCode V2 2.4.1 scoped Phase-1 promotion (Windows surface; Linux fail-closed)

Delivered 2026-11-26 on branch `refactor/rf-opencode-phase1`; dispatch-gate base revision
`9eb874bf22eace98399e54b25d00634b2bed5418` (`origin/main` at the merge of
`rf-opencode-230-hostname-redact` #291). Contract `goal-rf-opencode-phase1.md`; accepted plan
`docs/plans/2026-11-25-rf-opencode-phase1-plan.md`
(sha256 `300ed1a7e5647f284097faf89621a4b8d8e0c46b430b00fbd717cfdcf0a9de85`); proposal of
record `refactor/opencode-phase1-amendment.md` (GDD state root). The fix round below re-anchored
the client under the conductor ruling of 2026-11-26 (review R1-R9); its comparison base is
`550f96a3c128d3f3e397c6f03f6e003be6dfbd44` (`origin/main` #294).

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
(§4).

Row 3 (wiring): `TestNewHarnessRegistryBindsNativeAdapters` asserts the native adapter on
Windows and the unavailable seam on every other platform (M8 reddens the off-Windows branch);
`TestComposedOpenCodeProbeStaysFailClosedThroughTheRegistry` pins the composed native adapter's
tolerated refusal; `TestStartupProbeRegistersUnavailableNativeProjections/opencode` keeps the
unavailable seam pinned; `TestStartupProbeToleratesAnExpiredOpenCodeProbe` pins that a canceled
or expired probe is tolerated instead of aborting startup (M7 reddens it, §3);
`adapter_policy_truth_table_test.go` carries the promoted/refused/foreign-blob rows;
`inprocess_containment_test.go` installs and restores the in-process containment supervisor.

Row 4 (Linux containment; standing condition, **recorded unmet**): no dispatched
`linux-native-v2-acceptance` `opencode_native_health` run was attempted — the conductor ruled
"no dispatched Linux run in this goal; do not attempt it", and no qualifying-host decision
exists. No run id or subject sha256 is recorded; no Linux promotion is claimed.

Row 5 (pin drift): the carried pin literals are `2.4.1` —
`linux-v2-native-tools.sh` names `opencode-linux-x64.tar.gz` `9ddd18e3…` → ELF `ec93043f…` →
`opencode v2.4.1`; the acceptance fixture's `harness_version` map names `2.4.1`; the witness
tested-release check accepts `2.4.1` via `opencode.TestedVersion`. Census over the range's added
lines (`git diff <base>…` filtered to lines containing `1.18.32`) finds four, all recorded
historical references, none a carried pin: the V1 unsupported-version test row
(`daemon/internal/harness/opencode/adapter_test.go`), the historical V1 fixture constant
(`daemon/internal/harness/opencode/adapter_native_test.go`), a synthetic profile command string in
`daemon/internal/app/harness_test.go`, and the D1 row of `docs/goals/0010-refactor-program.md`.

Row 6 (revision binding): §5's delivered-element digests plus the amendment merge revision or a
post-merge re-run.

Row 7 (Windows live promotion proof; fix round R1): the opt-in env-gated
`TestNativePromotionProofOnThePinnedWindowsBlob` ran against the exact pinned Windows PE
(sha256 `f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e`, 278,383,496 bytes)
with the recorded relay provider and model on this Windows host: Open (readiness identity plus
session create), `StartTurn` with the prompt admission observed on the global event stream, an
applied cancel, a bounded clean close (`unknown`, process terminated) and the server-side turn
outcome `interrupted` read back through the retained client. A 2.4.1 interrupt that arrives
before the first generated token is refused rather than queued, so the test re-applies the
cancel until the server reports it applied — the same first-token ordering the live collector
records for its interruption class. The run also live-pins the identity check both ways: the
retained client accepts the launched pid and refuses `pid+1` with `ErrInvalidIdentity`. The
credential was read at run time from the operator's OMP models config and never entered argv, a
file, a log line, the journal or the receipt. Receipt:
`<STATE_DIR>/scratch/rf-opencode-phase1-fix1/promotion-receipt.json`, sha256
`24210df8459f7caf31cc49dfc711c3a86e79aba5c0f88b1e0388c410e1462b54`; it carries the
delivered-file digests of the six OpenCode rows in §5a, so the proof binds the delivered client
and adapter bytes.

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
| the contract's full-suite race form `go test -race -count=1 -timeout 600s -json ./...` (WSL) | `daemon/` | environment-limited: `internal/app` and `internal/harness/opencode` reach the 600 s per-package timeout before the named row runs (`run=0`), and `internal/platform`'s anchor-shape rows fail (environment-gated); the named row passes under the scoped form above |
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

### 5a. sha256 rows (computed at the delivered bytes; row 6 re-binds at the merge)

| File | sha256 |
| --- | --- |
| `.github/workflows/ci.yml` | `c3a88f8a8426c293761f91c9f9d541a326612f75901975293cf81d1c4438d82a` |
| `daemon/internal/app/adapter_policy_truth_table_test.go` | `788b6425dde10e9536b259d54d348d309fd3cfa489985c6ac228ae2e512d06c8` |
| `daemon/internal/app/harness_test.go` | `9c8c11b7d9910f193b089c6593bfc37231ab199ff2a45d93b73bebf7a5513dea` |
| `daemon/internal/app/probe.go` | `0ccbafd4f026472770e5f9e4a9da8146797a7049c62f95c1ba2d19a10e24ff49` |
| `daemon/internal/harness/harness_test.go` | `3cb64f1c20af0d5063272e63e1a4ae2f9751687826b67c25ff060ce55cc76759` |
| `daemon/internal/harness/opencode/adapter.go` | `651742055f9610b0a045f9d94257a5a0285d04c3893235aa63180fd2a42ac93a` |
| `daemon/internal/harness/opencode/adapter_native_promotion_test.go` (new) | `01fd371e57c93a53a9849a7dfde42696f4a74070a5e4565568f5c1a0f1dbf2a4` |
| `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go` | `acd4fe09ff550fc6af8761861e3aedb1d0695b2a108bf78917fea4fb1dc8494a` |
| `daemon/internal/harness/opencode/adapter_native_test.go` | `5ba4c1af2cd536e126d030e648f5567a9a3bc79acd8974c7e0573ff180b2edd5` |
| `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` | `b138d11c8a66b929dad80ccd9ace6e2e98cf3fdfff92669327e3875c612ba40d` |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | `ce4ea716e1d5698c68f5a5b9a95ca584ee7236d70c8daa11983018820be3dcdb` |
| `daemon/internal/harness/opencode/adapter_test.go` | `c02594032280825eb7dab00f25a1371927774d1382615a79b3ccfccfae1c3069` |
| `daemon/internal/harness/opencode/client.go` | `b9bb6894bef7dae7a1662b99cdd288bd043d6077e82adfbb9366703bf3d46c2c` |
| `daemon/internal/harness/opencode/client_test.go` | `20bd229f3a6863aee1b76d8b7cc3070c864f7c96b598a1cca72558c8be18a2de` |
| `daemon/internal/harness/opencode/inprocess_containment_test.go` (new) | `9f3bdff150af99fc3775468105ed4270261ced19ef0c20b6ba19382275ace7d7` |
| `daemon/internal/harness/opencode/peer_linux_test.go` | `e4b464c0d7adf47959012c8be803cdf31c03efe0b3c5dd94ff202d5369ada362` |
| `daemon/internal/harness/opencode/peer_test.go` | `1a267d6874a3c2fd067c6c90318db1fa934a30909c0dfb283af82d388606e7ff` |
| `daemon/internal/harness/opencode/probe_promotion_test.go` (new) | `ae9b0c550826f7f1be24c1c9a230fa93f29436278ecff59805020d9abb54b8c6` |
| `daemon/internal/harness/opencode/sse.go` | `7e6bcf7f0dbee159f026e377dc502465c41ca99f77e1c25ebfcc6d2318fb9f94` |
| `daemon/internal/harness/opencode/sse_test.go` | `0eb12d072a3d9448b501598dbf5a3ccb0b23f8ac62a337d97e17106ff0b39ecd` |
| `daemon/internal/harness/opencode/types.go` | `7c53e63fad3a7af2da4b8baab05ce6a09929fd41d78b02878095a5e65d84dbb4` |
| `daemon/internal/harness/registry.go` | `c8049e6eced97a11d325b78aff7ec9c6aa77102f1397f799164a8646d6e2ddcc` |
| `docs/goals/0010-refactor-program.md` | `49a9f0335020773716d2f884bd6f904fb78c81bfa2b63ba72ae3b1d24be22e37` |
| `docs/goals/README.md` | `5f2a6292408648d87d50f78a07d3c4c623bb3f87fa49ee22b4bf377e7bc6edd4` |
| `docs/plans/2026-11-25-rf-opencode-phase1-plan.md` (new) | `300ed1a7e5647f284097faf89621a4b8d8e0c46b430b00fbd717cfdcf0a9de85` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `dd5ffa474474ab87192c48b3a7519469e0c0748ec08d6b9a94e4a528aae65f66` |
| `scripts/ci/linux-v2-native-tools.sh` | `500ac7b47dd9a9b0f5aea2d67dafe4a0131dfd10726b0dc4b0215f4f1b410c69` |

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
   its scenarios were not re-run; `--port 0` is live-verified but absent from the committed
   receipts. None of these weakens a promoted operation.
7. Fenced, unchanged: truncated responses stay `unavailable` (no schema bound), usage stays
   unreconciled, and SSE reconnect/replay, retained-session recovery, resume, handoff,
   guidance/steer, safe pause, the approval bridge, hard cost limits and provider brokering stay
   unsupported; Linux containment is not promoted by any Windows or WSL2 receipt; V1.x stays
   skipped.

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

## 9. Apply sweep (2026-11-26)

The accepted simplification findings were applied on the delivered bytes, each with read-only
evidence in the review report: the blanket refusal sentence became operation-scoped; the
readiness-line comment cites the frozen fixtures' prefix spelling; the probe-cancellation guards
keep the seeded projection; three pins were added (composed promotion row with the literal
`opencode-http-v2` identity, the byte-equal refusal sentence row, the cancelled/expired probe
row); the duplicated version/help probe fixtures were hoisted; the Windows and WSL gate rows
were re-run on those bytes; M1-M3 were re-run and restored; the race row's local environment
limitations were recorded (now §4).
