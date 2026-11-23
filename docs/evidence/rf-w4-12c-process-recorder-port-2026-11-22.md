# rf-w4-12c process recorder port — evidence record (2026-11-22, implement stage)

Contract: `goal-rf-w4-12c-process-recorder-port.md` (GDD state root, external to the repository),
with the 2026-11-21 arch-decision-2 rulings (R1, R12, O1, O2, O3, O5), the verification amendments
(including the R2 supersession) and the 2026-11-22 comment policy binding over the earlier text.
Plan: `docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md`. Base revision:
`f04487736a30bccdc30ace6c40ecfd288fd45e05` (W4-15b/#243, W4-12a/#244, W4-12b/#257 merged). Branch:
`refactor/rf-w4-12c-process-recorder-port`. Every command ran in `daemon/` unless its row says
otherwise, on the Windows host with the Go toolchain of this checkout; the WSL row runs
Ubuntu-24.04 over the same worktree through its mount point (`<worktree>/daemon`). The fix-stage
amendments (§13) are committed as `bd819940`, the branch HEAD review 1 reviewed; §14 records the
review-round amendments, whose bytes §1 binds.

## 1. Subject bytes (sha256, final tree)

| File | sha256 |
| --- | --- |
| `daemon/internal/execution/recorder.go` (new) | `ee54071db3eecee2c4af534d2d8594340876bddfd230de5007802ee0815cf973` |
| `daemon/internal/execution/runner.go` | `eb848fc8a0245460510c009419762c25fda6ab6f2240214ce0fbe5cbec67f667` |
| `daemon/internal/execution/probe.go` | `72f520aaaeda0bb7f1df8b8aeb0ed05651546d51cc96dde3aaa9d39ae8ef617c` |
| `daemon/internal/execution/probe_test.go` | `6e63515653a03893394f98b01e634929d3ea4f35e61a7bcae0b6073c71c6d334` |
| `daemon/internal/execution/process_launcher_linux.go` | `72c58a553a5c9c795199eea74400e2a40f198760630d17d595062822ddb5124d` |
| `daemon/internal/execution/process_launcher_windows.go` | `8985bdf01f8d639b0afe693a1bfc819f934407e02e802066132056c1528767c9` |
| `daemon/internal/execution/recorder_test.go` (new) | `0dffd1a3626e2e9b634cbbc618c92e548ee0807e9b538ac15e1a2df969c7f156` |
| `daemon/internal/harness/types.go` | `9765d28b2922acaa39d30d6172d042d2791306e36a4e69b234b4966cf3e35144` |
| `daemon/internal/harness/staged_driver.go` | `fa69dd24210bfe09caa422291ced99192e0e712bf7ffa57b5197bacd12402b64` |
| `daemon/internal/harness/frozen_members_test.go` (new) | `e0659e6cfddedf75de2492ef501a0e85337ac62e516a301124e4bd81a9ad1722` |
| `daemon/internal/app/process_recorder.go` (new) | `e79090c8701d9cc26daaf6c04834cb03ed5462ae15863952c8181baa7dedf739` |
| `daemon/internal/app/process_recorder_test.go` (new) | `9d462f5af6c33b20b88f7281e21e212c31403862e26b8e6f1b62da26678857c4` |
| `daemon/internal/app/app.go` | `406ef06b9bfe720cee64e6a39333d76668a1d5a3a70607d458dbb6360129e67b` |
| `daemon/internal/app/goal_admission_tx.go` | `54a9c0bca1c5c0af7dc17a763b543773875a0c25b72bca3442e6afcd349a9576` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `3383fe973e1a41b15ea699eb7fa153b7b84ede16b5e7d814a2564ff9ae021442` |
| `scripts/ci/omo-control-native-evidence.py` | `b66b0d0c88c4201f7f0415571dc7a64a3ffcc7d2611b3a8df4a567f5d13d0803` |
| `docs/TODO.md` | `42b6b6d644632a1fc516bcc0115f4219a08fb4c393d1a96f5bd0266f33781e4c` |
| `docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md` | `f21ae0c6c2142dd1cba171f8581952275854003c9e2180f48fffb5fa32cef68b` |
| this record | the file you are reading (no self-referential hash) |

## 2. The port: one interface, ten writes, one capability query

Definition grep (paths, not bodies):

```
$ rg -n 'type Recorder |type RecorderCapabilities |type RecorderFuncs |func NewRecorder|func invocationWrites|func recorderWrites|func \(invocation \*Invocation\) MirrorRecorderMembers|func PopulateOpencodeRecorderMembers' daemon/internal/execution/recorder.go daemon/internal/harness/staged_driver.go
daemon/internal/execution/recorder.go:13:type RecorderCapabilities struct {
daemon/internal/execution/recorder.go:47:type Recorder interface {
daemon/internal/execution/recorder.go:87:type RecorderFuncs struct {
daemon/internal/execution/recorder.go:117:func NewRecorder(writes RecorderFuncs) Recorder {
daemon/internal/execution/recorder.go:198:func invocationWrites(invocation Invocation) RecorderFuncs {
daemon/internal/execution/recorder.go:219:func recorderWrites(recorder Recorder) RecorderFuncs {
daemon/internal/execution/recorder.go:262:func (invocation *Invocation) MirrorRecorderMembers() {
daemon/internal/harness/staged_driver.go:39:func PopulateOpencodeRecorderMembers(request StartRequest) StartRequest {
```

`Recorder` carries one method per durable write the runner performed at base
(`PrepareSupervisorHandoff`, `BindSupervisorHandoff`, `CommitSupervisorHandoff`,
`RecordSupervisorHandoffStopReceipt`, `ClearSupervisorHandoff`, `PersistProcess`,
`PersistProcessWithAuthority`, `PersistProcessAuthority`, `PersistContainmentStopReceipt`,
`PersistContainmentUnproven`) plus `Capabilities()`. `RecorderFuncs` is the functional form (ten
optional functions); `NewRecorder` adapts it into the port through the unexported `recorderFuncs`,
whose absent writes fail closed (`errRecorderWriteUnavailable`).

Two rules tie the port to the base surface:

- `invocationWrites(invocation)` returns the invocation's recorder writes when it has a recorder,
  otherwise the ten frozen `Invocation` mirrors. OpenCode rebuilds an invocation from
  `request.Invocation` members (`daemon/internal/harness/opencode/adapter.go:343-362`) and cannot
  carry a recorder, so its launch reaches the same recorder through the mirrors; every other path
  carries the recorder itself.
- `Invocation.MirrorRecorderMembers` sets the mirrors from the recorder's nil-gated write functions
  (`recorderWrites`; a false capability stays nil, exactly the legacy absent callback), and
  `harness.PopulateOpencodeRecorderMembers` copies those mirrors onto the three request-level
  members the frozen adapter reads; both are the single W6-04 deletion sites. Probes:
  `TestMirrorRecorderMembersKeepsAbsentWritesNil` and
  `TestPopulateOpencodeRecorderMembersKeepsAbsentRequestWritesNil` (§7.2).

## 3. Callback-name gate (harness, excluding OpenCode and tests)

Verbatim contract command; note that ripgrep matches `--glob '!opencode/**'` against the paths as
printed (`daemon/internal/harness/...`), so the exclusion only bites with `!**/opencode/**`; both
outputs are pasted.

```
$ rg -n 'Persist(Process|ProcessAuthority|ContainmentStopReceipt|ProcessWithAuthority|ContainmentUnproven)|(Prepare|Bind|Commit|Clear)SupervisorHandoff|RecordSupervisorHandoffStopReceipt' daemon/internal/harness --glob '!opencode/**' --glob '!*_test.go'
daemon/internal/harness/opencode/adapter.go:351:		PrepareSupervisorHandoff:           request.Invocation.PrepareSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:352:		BindSupervisorHandoff:              request.Invocation.BindSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:353:		CommitSupervisorHandoff:            request.Invocation.CommitSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:354:		RecordSupervisorHandoffStopReceipt: request.Invocation.RecordSupervisorHandoffStopReceipt,
daemon/internal/harness/opencode/adapter.go:355:		ClearSupervisorHandoff:             request.Invocation.ClearSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:356:		PersistProcessWithAuthority:        request.Invocation.PersistProcessWithAuthority,
daemon/internal/harness/opencode/adapter.go:357:		PersistProcess:                     request.PersistProcess,
daemon/internal/harness/opencode/adapter.go:358:		PersistProcessAuthority:            request.PersistProcessAuthority,
daemon/internal/harness/opencode/adapter.go:359:		PersistContainmentStopReceipt:      request.PersistContainmentStopReceipt,
daemon/internal/harness/opencode/adapter.go:360:		PersistContainmentUnproven:         request.Invocation.PersistContainmentUnproven,
daemon/internal/harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
daemon/internal/harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
daemon/internal/harness/staged_driver.go:45:	request.PersistProcess = request.Invocation.PersistProcess
daemon/internal/harness/staged_driver.go:46:	request.PersistProcessAuthority = request.Invocation.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:47:	request.PersistContainmentStopReceipt = request.Invocation.PersistContainmentStopReceipt
daemon/internal/harness/types.go:335:	PersistProcess func(pid int, identity string) error
daemon/internal/harness/types.go:338:	PersistProcessAuthority       func(pid int, identity string, value *authority.Supervisor) error
daemon/internal/harness/types.go:339:	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error

$ rg -n '<the same ten names>' daemon/internal/harness --glob '!**/opencode/**' --glob '!*_test.go'
daemon/internal/harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
daemon/internal/harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
daemon/internal/harness/staged_driver.go:45:	request.PersistProcess = request.Invocation.PersistProcess
daemon/internal/harness/staged_driver.go:46:	request.PersistProcessAuthority = request.Invocation.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:47:	request.PersistContainmentStopReceipt = request.Invocation.PersistContainmentStopReceipt
daemon/internal/harness/types.go:335:	PersistProcess func(pid int, identity string) error
daemon/internal/harness/types.go:338:	PersistProcessAuthority       func(pid int, identity string, value *authority.Supervisor) error
daemon/internal/harness/types.go:339:	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error
```

Reading: the ten handoff/authority names appear in `daemon/internal/harness` production code only
inside `opencode/adapter.go` (frozen reader). The three request-level names appear only as the
`StartRequest` declarations (types.go) and at the two population sites: `PopulateOpencodeRecorderMembers`
(staged_driver.go:45-47) and `grant()`'s no-grant fold (staged_driver.go:29-31, see §5.3). No
conforming adapter names any durable callback:

```
$ rg -n '<the same ten names>' daemon/internal/harness/claude daemon/internal/harness/codex daemon/internal/harness/omo daemon/internal/harness/omp daemon/internal/harness/pi daemon/internal/harness/generic.go --glob '!*_test.go'
(no output, exit 1)
```

## 4. Per-adapter effective writes, base vs HEAD

Completeness check for clause 1 (no production reader of the ten members outside the port surface
and its two write sites; `opencode/**` excluded because it is the frozen reader):

```
$ rg -n 'invocation\.(Persist|PrepareSupervisorHandoff|BindSupervisorHandoff|CommitSupervisorHandoff|RecordSupervisorHandoffStopReceipt|ClearSupervisorHandoff)' daemon/internal --glob '!*_test.go' --glob '!**/opencode/**' | sed 's|daemon/internal/||'
execution/recorder.go:203:		PrepareSupervisorHandoff:           invocation.PrepareSupervisorHandoff,
execution/recorder.go:204:		BindSupervisorHandoff:              invocation.BindSupervisorHandoff,
execution/recorder.go:205:		CommitSupervisorHandoff:            invocation.CommitSupervisorHandoff,
execution/recorder.go:206:		RecordSupervisorHandoffStopReceipt: invocation.RecordSupervisorHandoffStopReceipt,
execution/recorder.go:207:		ClearSupervisorHandoff:             invocation.ClearSupervisorHandoff,
execution/recorder.go:208:		PersistProcess:                     invocation.PersistProcess,
execution/recorder.go:209:		PersistProcessWithAuthority:        invocation.PersistProcessWithAuthority,
execution/recorder.go:210:		PersistProcessAuthority:            invocation.PersistProcessAuthority,
execution/recorder.go:211:		PersistContainmentStopReceipt:      invocation.PersistContainmentStopReceipt,
execution/recorder.go:212:		PersistContainmentUnproven:         invocation.PersistContainmentUnproven,
execution/recorder.go:267:	invocation.PrepareSupervisorHandoff = writes.PrepareSupervisorHandoff
execution/recorder.go:268:	invocation.BindSupervisorHandoff = writes.BindSupervisorHandoff
execution/recorder.go:269:	invocation.CommitSupervisorHandoff = writes.CommitSupervisorHandoff
execution/recorder.go:270:	invocation.RecordSupervisorHandoffStopReceipt = writes.RecordSupervisorHandoffStopReceipt
execution/recorder.go:271:	invocation.ClearSupervisorHandoff = writes.ClearSupervisorHandoff
execution/recorder.go:272:	invocation.PersistProcess = writes.PersistProcess
execution/recorder.go:273:	invocation.PersistProcessWithAuthority = writes.PersistProcessWithAuthority
execution/recorder.go:274:	invocation.PersistProcessAuthority = writes.PersistProcessAuthority
execution/recorder.go:275:	invocation.PersistContainmentStopReceipt = writes.PersistContainmentStopReceipt
execution/recorder.go:276:	invocation.PersistContainmentUnproven = writes.PersistContainmentUnproven
harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
```

| Adapter / path | Base write source | HEAD write source | Ordered writes + arguments |
| --- | --- | --- | --- |
| claude (`claude/adapter.go:167`) | grant invocation's ten members | `request.grant().merged(...)` (`staged_driver.go:102`) → the grant's `Recorder` | identical |
| codex (`codex/adapter.go:192`) | same | same | identical |
| pi (`pi/adapter.go:198`) | same | same | identical |
| omo (`omo/adapter.go:202`) | same | same | identical |
| omp (`omp/adapter.go:107`) | same | same | identical |
| generic (`generic.go:132-133`) | `request.grant().invocation`'s members | the same invocation's `Recorder` | identical |
| opencode (`opencode/adapter.go:343-362`, `:73`) | rebuilt invocation's ten members | rebuilt invocation (no recorder) → `invocationWrites` falls back to the same ten mirrors | identical |
| app direct (`app.go:1494-1509`) | ten inline callbacks | `daemon.processRecorder` (`process_recorder.go`) + mirrors | identical |
| goal admission (`goal_admission_tx.go:746-756`, `:900-919`) | seven invocation members + three grant members + three request members | one recorder in the invocation (`:754`) + mirrors + `PopulateOpencodeRecorderMembers` (`:918`) | identical |
| Linux v2 owner witness (`linux_owner_witness_linux.go:3109-3117`, `:3227-3232`) | seven members + three grant/request members each | one recorder (`:3113`) + mirrors (`:3117`) + populate (`:3232`) | identical |

The ordered writes themselves are unchanged because the runner's plumbing is unchanged: the same
store methods run in the same order with the same arguments, and the parity test
(`TestRecorderWritesLaunchExactlyLikeTheFrozenMembers`) pins the sequences (§7).

## 5. Nil-combination branches and the handoff truth table

### 5.1 Branches

| Base nil combination | Base branch | HEAD branch |
| --- | --- | --- |
| `PersistProcessWithAuthority` nil | legacy two-write path | `writes.PersistProcessWithAuthority == nil` → same |
| `PersistProcessWithAuthority` set + authority-capable containment | atomic write, two legacy writes skipped | same |
| `PersistProcess` nil | no marker write | same (`writes.PersistProcess == nil`) |
| `PersistProcessAuthority` nil | no authority write | same |
| `PersistContainmentStopReceipt` nil | no receipt write, `stopReceiptRequired=false` | same |
| `PersistContainmentUnproven` nil | no uncertainty observer installed | same |
| handoff members nil | `configureStartedHandoff` leaves the launcher's funcs | same (`writes.*`) |

The parity test's eight shapes (marker+authority, atomic, atomic failure, marker without authority,
no durable write, the production marker+atomic+authority+stop set, handoff commit-then-stop,
unproven-on-close-failure) each assert the write sequence for the frozen-member form and the
recorder form, plus the start error, the containment error and the containment cleanup counters.
The last three arrived in the review round (§14): the production shape pins the atomic write's
precedence over both legacy writes, the handoff shape pins commit-then-stop-receipt, and the
unproven shape pins one `PersistContainmentUnproven` after the failed close with no stop receipt.
The permanently failing atomic shape asserts the failing write's identity, the retry range [2,3]
and the stop invariant instead of a fixed length, because the write is retried once per
finalization call and a failed finalization stays retryable (§7.4).

### 5.2 Handoff truth table

`TestRecorderHandoffCapabilityMatchesPresenceGates` walks all 32 combinations of the five handoff
members and asserts `SupervisorHandoffRequested`/`SupervisorHandoffComplete` equal the base
any-of-five/prepare-and-bind-and-commit rules for the frozen members and for the recorder form:

```
$ go test -count=1 -v -run TestRecorderHandoffCapabilityMatchesPresenceGates ./internal/execution/
exit=0
mask 00000 requested=false complete=false
mask 00001 requested=true complete=false
mask 00010 requested=true complete=false
mask 00011 requested=true complete=false
mask 00100 requested=true complete=false
mask 00101 requested=true complete=false
mask 00110 requested=true complete=false
mask 00111 requested=true complete=true
mask 01000 requested=true complete=false
mask 01001 requested=true complete=false
mask 01010 requested=true complete=false
mask 01011 requested=true complete=false
mask 01100 requested=true complete=false
mask 01101 requested=true complete=false
mask 01110 requested=true complete=false
mask 01111 requested=true complete=true
mask 10000 requested=true complete=false
mask 10001 requested=true complete=false
mask 10010 requested=true complete=false
mask 10011 requested=true complete=false
mask 10100 requested=true complete=false
mask 10101 requested=true complete=false
mask 10110 requested=true complete=false
mask 10111 requested=true complete=true
mask 11000 requested=true complete=false
mask 11001 requested=true complete=false
mask 11010 requested=true complete=false
mask 11011 requested=true complete=false
mask 11100 requested=true complete=false
mask 11101 requested=true complete=false
mask 11110 requested=true complete=false
mask 11111 requested=true complete=true
```

### 5.3 The no-grant fold

`StartRequest.grant()` still folds `request.PersistProcess`/`PersistProcessAuthority`/
`PersistContainmentStopReceipt` onto the invocation's mirrors when no grant was passed. Production
always passes a grant (`goal_admission_tx.go:915`, `linux_owner_witness_linux.go:3230`), so the fold
only serves direct constructions (adapter tests, `staged_driver_test.go`), and it feeds exactly the
mirrors a recorder-less invocation already launches through — it is not a second path for a
conforming production adapter.

## 6. Port-path unit tests and app dispatch

`daemon/internal/app/process_recorder_test.go` drives one recorder through a real store wrapped in
`apptest.FaultStore` with one recording hook per write: the store dispatch sequence is asserted
(`SetProcessDetails`, `SetProcessDetailsWithAuthority` ×2, `RecordContainmentStopReceipt`,
`MarkContainmentUnproven`, `PrepareSupervisorHandoff`, `BindSupervisorHandoff`,
`RecordSupervisorHandoffStopReceipt`, `ClearSupervisorHandoff`, then the commit route's
`PrepareSupervisorHandoff`, `BindSupervisorHandoff`, `CommitSupervisorHandoff`) and the resulting
journal state is asserted after each write (marker, authority, receipt, unproven flag, bound
handoff, cleared handoff, promoted marker).

## 7. Red mutants

Five edits, each applied alone to the final tree, run against the named tests, then reverted
(`sha256sum -c` over the pre-mutant hashes of `recorder.go` and `runner.go` says `OK`).

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| m1 reorder two writes | `persistLegacyIdentity`: authority write before the marker write | new parity subtest `process_marker_and_legacy_authority` and pre-existing `TestAuthorityProviderWithoutAtomicCallbackPreservesLegacyCallbacks` | `write sequence = ["persist-process-authority(4242,fake:4242)","persist-process(4242,fake:4242)",...], want ["persist-process(...)","persist-process-authority(...)",...]`; `legacy persistence callbacks = [authority marker], want [marker authority]` |
| m2 drop `PersistContainmentUnproven` | `persist()` sets `started.persistContainmentUnproven = nil` | pre-existing `TestRunnerPersistsObservedContainmentUncertaintyAfterMarkerBeforeClose`, `TestStartPersistsMarkerBeforeCleaningRetainedInitialScanFailure` | `containment uncertainty callback was not installed before marker persistence` |
| m3 two-callback path while `PersistProcessWithAuthority` is supplied | `persist()` branch drops the write-presence check | new parity subtest `process_marker_and_legacy_authority` | `panic: runtime error: invalid memory address or nil pointer dereference` (nil atomic write reached) |
| m4a wrong-method populate (invocation) | `MirrorRecorderMembers`: `PersistProcessAuthority = recorder.PersistProcessWithAuthority` | new `TestMirrorRecorderMembersCallsTheSameRecorderMethods` | `frozen member authority reached ["atomic"], want the same recorder method` |
| m4b wrong-method populate (request) | `PopulateOpencodeRecorderMembers`: `PersistProcessAuthority = recorder.PersistProcessWithAuthority` | new `TestPopulateOpencodeRecorderMembersReachesTheSameRecorderMethod` | `frozen member PersistProcessAuthority reached ["PersistProcessWithAuthority"], want the same recorder method` |
| m5 drop `ClearSupervisorHandoff` from the handoff decision | `SupervisorHandoffRequested` loses the `Clear` term | new `TestRecorderHandoffCapabilityMatchesPresenceGates` | `mask 10000: frozen members decided requested=false complete=false, want requested=true complete=false`; then `mask 10000: recorder decided requested=false complete=false, want requested=true complete=false` |

m1, m2 and m3 are killed by tests that predate the slice as well, satisfying the R2 amendment.

### 7.1 Apply-stage re-checks (2026-11-23)

The apply stage changed `recorder.go`, `probe.go`, `runner.go` (one comment hunk) and the two
execution test files, so every mutant whose edit text or killer sits on those bytes was re-run
alone on the applied tree and then reverted (`sha256sum` of `recorder.go` and `probe.go` restored
to the §1 values). m1–m3 mutate statements in `persist()`/`persistLegacyIdentity()` and m5 mutates
`SupervisorHandoffRequested`; those functions are byte-identical to the implement-stage tree, and
m4b's subject `PopulateOpencodeRecorderMembers` was not touched by apply — those rows still bind
the same bytes.

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| mA1a recorder left attached | `ephemeralInvocation` drops `invocation.Recorder = noDurableWrites` | `TestRunBoundedCommandDoesNotInvokeRecorderWrites`, `TestEphemeralInvocationRemovesEveryDurableWrite` | `RunBoundedCommand() invoked 1 durable writes`; `ephemeral strip left durable writes: {Prepare…Commit:true, PersistProcess:true, PersistProcessWithAuthority:true, PersistContainmentStopReceipt:true, PersistContainmentUnproven:true}` |
| mA1b pre-D2 strip | `ephemeralInvocation` restores `Recorder = nil` plus the three legacy clears | `TestEphemeralInvocationRemovesEveryDurableWrite` (the recorder-run test passes, since the recorder itself is nil) | `ephemeral strip left durable writes: {Prepare:true Bind:true Commit:true RecordSupervisorHandoffStopReceipt:true Clear:true PersistProcessWithAuthority:true PersistContainmentUnproven:true}` (7 writes alive) |
| mA2 pre-D1 mirror | `MirrorRecorderMembers` restores the unconditional method-value body | `TestMirrorRecorderMembersKeepsAbsentWritesNil` (m4a's killer still passes) | `an absent write was mirrored as present` |
| mA3 wrong-method fallback | `recorderWrites`: `writes.PersistProcessAuthority = recorder.PersistProcessWithAuthority` | `TestRecorderWritesMapsEveryCapabilityToItsMethod` | `fallback write authority reached []string{"atomic"}, want its own method` |
| m4a re-expressed | `MirrorRecorderMembers`: `invocation.PersistProcessAuthority = writes.PersistProcessWithAuthority` | `TestMirrorRecorderMembersCallsTheSameRecorderMethods` | `frozen member authority reached []string{"atomic"}, want the same recorder method` |

### 7.2 Fix-stage re-checks (2026-11-23)

The fix stage (STD-02, §13) changed `PopulateOpencodeRecorderMembers` and added
`TestPopulateOpencodeRecorderMembersKeepsAbsentRequestWritesNil`, so every mutant whose edit text
or killer sits on those bytes was re-run alone on the fixed tree and then reverted (`sha256sum -c`
over the fix-stage hashes of `staged_driver.go` and `frozen_members_test.go` says `OK`). m5's
subject bytes are unchanged; it was re-run to capture the true output order for its §7 row.

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| m4b re-expressed | `PopulateOpencodeRecorderMembers`: `request.PersistProcessAuthority = request.Invocation.PersistProcessWithAuthority` (the wrong mirror) | new `TestPopulateOpencodeRecorderMembersReachesTheSameRecorderMethod` | `frozen member PersistProcessAuthority reached []string{"PersistProcessWithAuthority"}, want the same recorder method` |
| mF1 absent write populated as present | `PopulateOpencodeRecorderMembers` restores the method-value body (`request.PersistProcess = recorder.PersistProcess`, …) | new `TestPopulateOpencodeRecorderMembersKeepsAbsentRequestWritesNil` | `an absent write was populated onto the request as present` |
| m5 re-run | `SupervisorHandoffRequested` loses the `Clear` term (unchanged bytes; the §7 row's first red line is corrected) | new `TestRecorderHandoffCapabilityMatchesPresenceGates` | `mask 10000: frozen members decided requested=false complete=false, want requested=true complete=false`; then `mask 10000: recorder decided requested=false complete=false, want requested=true complete=false` |

### 7.3 Review-round re-checks (2026-11-23)

The review round (§14) added the parity shape that kills m6 and the absent-write table test that
kills m7; both edits were applied alone to the final tree, run against their killer (exit 1), then
reverted (`sha256sum -c` over the pre-mutant hashes of `recorder.go` and `runner.go` says `OK`).

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| m6 two-callback path while a marker write exists (the review's M2) | `persist()`: the atomic branch additionally requires `run.writes.PersistProcess == nil` | new parity subtest `atomic_write_wins_over_the_production_marker_and_authority_pair` | `write sequence = ["persist-process(4242,fake:4242)","persist-process-authority(4242,fake:4242)","persist-containment-stop-receipt(4242,fake:4242)"], want ["persist-process-with-authority(4242,fake:4242)","persist-containment-stop-receipt(4242,fake:4242)"]` |
| m7 absent write fails open | `recorderFuncs.PrepareSupervisorHandoff`: the nil branch returns nil instead of `errRecorderWriteUnavailable` | new `TestRecorderFuncsEveryAbsentWriteFailsClosed` | `absent PrepareSupervisorHandoff error = <nil>, want errRecorderWriteUnavailable` |

### 7.4 Retry-count determination (2026-11-23)

The `atomic_write_failure_stops_the_launch` row once observed a third
`PersistProcessWithAuthority` attempt under load. Two scratch copies (base `764c653e`; head
`bd819940` plus this round's test file) ran the same shape — an always-failing authority write
plus a stop receipt over `p1AuthorityContainment` — in loops with the worker count pinned to
GOMAXPROCS; the head copy also counted finalization calls per run (scratch-only probe).

| Tree / representation | Setting | Runs | attempts=2 | attempts=3 | violations |
| --- | --- | --- | --- | --- | --- |
| head members | GOMAXPROCS=1 | 2000 | 2000 | 0 | 0 |
| head recorder | GOMAXPROCS=1 | 2000 | 2000 | 0 | 0 |
| head members | GOMAXPROCS=8 | 4000 | 3985 | 15 | 0 |
| head recorder | GOMAXPROCS=8 | 4000 | 3981 | 19 | 0 |
| base members | GOMAXPROCS=1 | 2000 | 2000 | 0 | 0 |
| base members | GOMAXPROCS=8 | 4000 | 3992 | 8 | 0 |
| head members | WSL `-race`, 8 workers | 2400 | 2302 | 98 | 0, no race report |
| head recorder | WSL `-race`, 8 workers | 2400 | 2318 | 82 | 0, no race report |
| base members | WSL `-race`, 8 workers | 2400 | 2319 | 81 | 0, no race report |

Every run kept the stop invariant: every event was the same `persist-process-with-authority` call,
no stop receipt was written, `success=false`, both the start and containment errors carried the
write failure, and the cleanup stayed at one close with no release or abort. The instrumented head
copy showed `finalize calls == runs + attempts=3 runs` (8035 = 8000 + 35 for members, 8025 = 8000
+ 25 for the recorder form); `runtime.Caller` attributed the calls to `waitForCompletion`'s two
finalize sites and `Terminate`'s result-done site — an extra legitimate finalization call adds
exactly one retry, and the retry never multiplies inside one finalization. The base copy behaves
identically, and the base→head diff touches none of `closeContainment`/`waitForCompletion`/
`Terminate` (no hunks), so the count is a scheduling artifact of the shared runner retry semantics,
not a port behaviour: the permanently failing write stays retryable by design, and the row now
asserts the failing write's identity, the [2,3] retry range and the stop invariant instead of a
fixed length and the interleaving-dependent joined error text. The final tree passes the
sequential 500/500 re-run and 8000 additional loaded runs (16-process batches; 8 attempts=3 runs),
all exit 0. Under the same load, the round before the retry/error-text adjustments reproduced both
rejected artifacts: `frozen=2 recorder=3` sequence logs (four in 1000 runs, rejected by the
original exact-sequence comparison) and one batch failure on `start errors differ`, whose texts
differed only in which finalization call Terminate joined.

## 8. Gates

Every row ran in the foreground on this host except the WSL and coverage rows, which ran alone (no
other suite on the host while either ran). The fix stage (2026-11-23, §13) re-ran every row
against the fixed tree; the numbers below are the fix-stage observations (the callback-name greps'
populate-site lines are the §3 fixed-tree output).

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| callback-name grep (contract command, `git root`) | 0 | §3 |
| callback-name grep with `!**/opencode/**` (`git root`) | 0 | §3 |
| conforming-adapter grep (`git root`) | 1 | no output: no durable callback named |
| `gofmt -l .` (`daemon/`) | 0 | no file listed |
| `git diff --check` (`git root`) | 0 | no whitespace error |
| `go vet ./...` (`daemon/`) | 0 | empty log (`<scratch>/gate-vet.log`) |
| `GOOS=linux go build ./...` (`daemon/`) | 0 | — |
| `GOOS=windows go build ./...` (`daemon/`) | 0 | — |
| `GOOS=darwin go build ./...` (`daemon/`) | 0 | — |
| `GOOS=linux GOARCH=arm64 go build ./...` (`daemon/`) | 0 | — |
| `go test -count=1 -timeout 1800s ./internal/execution/... ./internal/harness/... ./internal/app/...` (Windows, `daemon/`) | 0 | 12 packages `ok`: execution 12.188s, harness 3.378s, claude 16.041s, codex 15.639s, linejson 0.724s, omo 5.955s, omp 6.830s, opencode 2.278s, pi 7.292s, probeconformance 9.176s, app 202.702s, apptest 3.924s |
| `python3 scripts/ci/test-omo-control-native-evidence.py` (R9, `git root`) | 0 | `Ran 15 tests in 21.865s / OK` |
| `git diff --name-only` (scope, `git root`) | 0 | §9 |
| `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race -timeout 3000s ./internal/execution/... ./internal/harness/... ./internal/app/... ./internal/platform/...` (`<worktree>/daemon`) | 0 | 13 packages `ok`: execution 23.314s, harness 1.182s, claude 20.430s, codex 26.961s, linejson 1.032s, omo 9.886s, omp 23.020s, opencode 2.650s, pi 6.132s, probeconformance 8.082s, app 571.827s, apptest 4.082s, platform 58.537s; no race report |
| coverage pass (`go test -covermode=count -coverprofile=…` for `./internal/execution/`, `./internal/harness/`, `./internal/app/`, Windows) | 0 | execution 54.3% (base 53.0%), harness 71.1% (base 70.7%), app 75.2% (base 75.2%); per-file deltas below |

Coverage delta for every touched file (statements covered/total; the base side comes from a
throwaway worktree at `f0448773`, so the base bytes are the code the contract was authored against;
that worktree was removed afterwards):

| File | Base | Implement stage | Applied (2026-11-23) |
| --- | --- | --- | --- |
| `daemon/internal/execution/runner.go` | 629/1169 (53.8%) | 635/1167 (54.4%) | 636/1167 (54.5%) |
| `daemon/internal/execution/probe.go` | 74/86 (86.0%) | 75/87 (86.2%) | 74/86 (86.0%) |
| `daemon/internal/execution/process_launcher_windows.go` | 16/102 (15.7%) | 18/104 (17.3%) | 18/104 (17.3%) |
| `daemon/internal/execution/recorder.go` (new) | no profile | 43/76 (56.6%) | 46/75 (61.3%) |
| `daemon/internal/harness/types.go` | 44/72 (61.1%) | 44/72 (61.1%) | 44/72 (61.1%) |
| `daemon/internal/harness/staged_driver.go` | 47/47 (100.0%) | 55/55 (100.0%) | 55/55 (100.0%) |
| `daemon/internal/app/app.go` | 829/1148 (72.2%) | 833/1149 (72.5%) | 831/1149 (72.3%) |
| `daemon/internal/app/goal_admission_tx.go` | 582/816 (71.3%) | 580/813 (71.3%) | 579/812 (71.3%) |
| `daemon/internal/app/process_recorder.go` (new) | no profile | 2/2 (100.0%) | 2/2 (100.0%) |

Apply-stage reading: `recorder.go`'s statement count drops by one (the deleted `recorderFuncs.Capabilities`
forwarder) and its covered count rises by three — the new apply tests cover the whole
`recorderWrites` fallback path (`go tool cover -func`: `recorderWrites` 8.0% at implement stage →
100.0% applied). `probe.go` loses one statement and one covered statement: the D2 strip rewrite
replaces eight assignments with the `ephemeralInvocation` call (`100.0%`) plus the helper's five
assignments and return. `app.go`'s bytes are unchanged from the implement-stage row (same §1
sha256); its 833→831 covered difference is run-to-run variation in timing-dependent paths, not a
code change.

`process_launcher_linux.go` and `linux_owner_witness_linux.go` are Linux-only files with no Windows
profile rows at base or HEAD (their behaviour is exercised by the WSL row above); the new test files
carry no profile rows by construction, and the applied-stage movements are the statement-shape
changes above, not uncovered additions.

Fix-stage re-measure (2026-11-23, §13): the three profiles re-ran on the fixed tree; every
per-file row above is unchanged except execution's package total (54.1% → 54.3%) and `runner.go`'s
covered count (636 → 639), both run-to-run variation on a file whose §1 sha256 is unchanged, like
the apply-stage `app.go` movement. `staged_driver.go` stays 55/55 (100.0%): the three mirror-copy
statements keep the tested shape, and the new `TestPopulateOpencodeRecorderMembersKeepsAbsentRequestWritesNil`
adds no uncovered production statement.

Review-round re-run (2026-11-23, §14, final tree after §7.4): `gofmt -l .` → no file listed;
`go vet ./...` and `GOOS=linux go vet ./...` → empty; `GOOS=linux go build ./...` and
`GOOS=windows go build ./...` → exit 0; `go test -count=1 -timeout 40m ./internal/execution/...
./internal/harness/...` → exit 0, ten packages `ok` (execution 11.670s, harness 2.137s, claude
14.277s, codex 16.007s, linejson 0.750s, omo 5.253s, omp 6.260s, opencode 0.504s, pi 5.511s,
probeconformance 10.288s); the targeted app pattern → exit 0 (app 75.141s, apptest 2.604s no tests
to run); the WSL `-race` execution row → exit 0 (22.314s, no race report); R9 → exit 0 (`Ran 15
tests in 10.909s / OK`); the permanently failing parity row → exit 0 in the sequential 500/500
re-run and in 8000 additional loaded runs (§7.4). Not re-run: the darwin/arm64 builds and the full
WSL harness/app/platform suites, whose inputs a test-only round does not touch. The R1 coverage
profile for `./internal/execution/` moves the package total from 54.3% to 58.2% (841/1446
statements) and `recorder.go` from 46/75 (61.3%) to 64/75 (85.3%): every `recorderFuncs` wrapper
method leaves 0% for 66.7% (its nil branch; the delegate branch runs only from the app package),
and `runner.go` reads 677/1167 (58.0%, fix-stage row 639) because the three new parity shapes
exercise its handoff and unproven paths. The unskipped coverage pass trips the pre-existing
`TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB` under coverage instrumentation on this host
(stderr length +577 bytes; reproduced with this round's two tests skipped and again on the base
revision, 1 of 3 runs; the test passes focused and in the plain suite row above), so the profile
above comes from a pass that skips that one test.

## 9. Scope

```
$ git diff --name-only
daemon/internal/app/app.go
daemon/internal/app/goal_admission_tx.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/execution/probe.go
daemon/internal/execution/process_launcher_linux.go
daemon/internal/execution/process_launcher_windows.go
daemon/internal/execution/runner.go
daemon/internal/harness/staged_driver.go
daemon/internal/harness/types.go
docs/TODO.md
scripts/ci/omo-control-native-evidence.py

$ git status --porcelain -uall --no-renames
 M daemon/internal/app/app.go
 M daemon/internal/app/goal_admission_tx.go
 M daemon/internal/app/linux_owner_witness_linux.go
 M daemon/internal/execution/probe.go
 M daemon/internal/execution/process_launcher_linux.go
 M daemon/internal/execution/process_launcher_windows.go
 M daemon/internal/execution/runner.go
 M daemon/internal/harness/staged_driver.go
 M daemon/internal/harness/types.go
 M docs/TODO.md
 M scripts/ci/omo-control-native-evidence.py
?? daemon/internal/app/process_recorder.go
?? daemon/internal/app/process_recorder_test.go
?? daemon/internal/execution/recorder.go
?? daemon/internal/execution/recorder_test.go
?? daemon/internal/harness/frozen_members_test.go
?? docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md
?? docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md
```

Line counts (`git diff --numstat` for the modified files, `wc -l` for the new ones): modified
`app.go` +15/−24, `goal_admission_tx.go` +17/−32, `linux_owner_witness_linux.go` +6/−20, `probe.go`
+1/−0, `process_launcher_linux.go` +8/−6, `process_launcher_windows.go` +10/−8, `runner.go`
+35/−62, `staged_driver.go` +19/−0, `types.go` +6/−10, `docs/TODO.md` +17/−16, the evidence script
+2/−0 (net −53 source lines over the ten modified files); new `execution/recorder.go` 281,
`app/process_recorder.go` 26 (production, +307), `execution/recorder_test.go` 390,
`harness/frozen_members_test.go` 112, `app/process_recorder_test.go` 209 (tests, +711),
`docs/plans/...` 201, this record.

Apply stage (2026-11-23, working tree against the committed slice; §12): the same owned set stays,
plus `daemon/internal/execution/probe_test.go` (the F6 test lives in the existing probe test file).

```
$ git diff --name-only
daemon/internal/app/goal_admission_tx.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/app/process_recorder.go
daemon/internal/execution/probe.go
daemon/internal/execution/probe_test.go
daemon/internal/execution/recorder.go
daemon/internal/execution/recorder_test.go
daemon/internal/execution/runner.go

$ git diff --numstat
0	1	daemon/internal/app/goal_admission_tx.go
0	1	daemon/internal/app/linux_owner_witness_linux.go
2	3	daemon/internal/app/process_recorder.go
12	8	daemon/internal/execution/probe.go
54	0	daemon/internal/execution/probe_test.go
15	19	daemon/internal/execution/recorder.go
145	0	daemon/internal/execution/recorder_test.go
3	3	daemon/internal/execution/runner.go
```

Post-apply line counts: `execution/recorder.go` 277, `execution/recorder_test.go` 535,
`execution/probe.go` 183, `execution/probe_test.go` 260, `app/process_recorder.go` 25,
`app/goal_admission_tx.go` 1455, `app/linux_owner_witness_linux.go` 3897, `execution/runner.go`
2672.

Fix stage (2026-11-23, committed as `bd819940` on top of `5945f5d4`; §13): the same owned set, no new files;
the five findings fix the harness port helper and its test, the R9 script, and this record's three
documents.

```
$ git status --porcelain -uall --no-renames
 M daemon/internal/harness/frozen_members_test.go
 M daemon/internal/harness/staged_driver.go
 M docs/TODO.md
 M docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md
 M docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md
 M scripts/ci/omo-control-native-evidence.py

$ git diff --numstat
26	0	daemon/internal/harness/frozen_members_test.go
3	3	daemon/internal/harness/staged_driver.go
2	2	docs/TODO.md
5	3	docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md
0	1	scripts/ci/omo-control-native-evidence.py
```

(the `docs/evidence/...` row is omitted: this record's own count changes with each edit of the
fix-stage sections).

Post-fix line counts: unchanged from the post-apply list except `harness/staged_driver.go` 151 and
`harness/frozen_members_test.go` 138 (was 112); `SOURCE_PATHS` holds 62 entries covering 62
distinct paths.

Review round (2026-11-23, working tree against the committed `bd819940`; §14): the same owned set;
`daemon/internal/execution/recorder_test.go` gains the three parity shapes, the absent-write table
test and the invariant-based assertions for the permanently failing shape (§7.4), and this record
and the plan carry the review-round text.

```
$ git diff --name-only
daemon/internal/execution/recorder_test.go
docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md
docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md

$ git diff --numstat
224	29	daemon/internal/execution/recorder_test.go
3	0	docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md
```

(the `docs/evidence/...` row is omitted: this record's own count changes with each edit.)

Review-round line counts: `execution/recorder_test.go` 730, `docs/plans/...` 206.

No path is under `daemon/internal/harness/opencode/`; a case-insensitive scan of the final
working-tree branch diff for the workstation profile directory fragment has no match (the
implement stage's own quoted check command was the only match; the fix stage rewrote these
sentences in prose, §13), and a byte scan for the backslash-u escape pair over every changed and
new file finds no byte (the implement stage's own quoted scan pattern was the only match; the fix
stage removed it, §13). The new files are LF (`file` reports `ASCII text`).

## 10. As-built deviations from the plan

- The port's functional form and the port itself are two types (`RecorderFuncs` fields +
  unexported `recorderFuncs` methods) because Go forbids a field and a method with the same name;
  plan §9 records it.
- The harness populate test file is `frozen_members_test.go` (not `opencode_members_test.go`) so the
  `opencode/**` scope check cannot be confused by a file name.
- R9 adds one `SOURCE_PATHS` entry, `daemon/internal/app/process_recorder.go` (the new receiver);
  `daemon/internal/app/goal_admission_tx.go` was already declared by W4-01 — its record
  (`docs/evidence/rf-w4-01-goal-admission-tx-2026-11-22.md` §1.1) states the append, and
  `git blame` at the base revision attributes the existing entry to `adfe7d6a` (PR #234). The
  implement stage's duplicate entry and its "left out" justification were removed in the fix
  stage (STD-01, §13).
- The Windows launcher evaluates the handoff capability once instead of twice: the invocation cannot
  change between the two base checks, and the reused capability set is the same value (the Linux
  launcher's condition order — query first, `DurableSupervisorHandoffAvailable` second — is
  unchanged).

## 11. Residual risks and known limits

- The recorder-less fallback stays for the frozen adapter; it is the only way an OpenCode launch can
  reach its durable writes without editing `opencode/**` (O2). W6-04 deletes it with the members.
- The supervisor-handoff **positive** path (the two launchers' `Prepare`/`Bind` closures and
  `commitSupervisorHandoff`) stays unreachable under `go test` on both OSes, because
  `platform.DurableSupervisorHandoffAvailable()` is false in a test binary; it is provable only by
  the native/e2e witness runs the contract's gate list names. The unit-side evidence is the
  mechanical retarget (one identifier per call) and the 32-mask capability test (§5.2).
- `Recorder` writes on a non-`RecorderFuncs` implementations are taken as method values once per
  launch (`recorderWrites`); the app and both construction paths use `NewRecorder`, which avoids it.
- The base coverage profile was taken from a throwaway worktree at `f0448773` (removed after the
  measurement); no repository file depends on it.

## 12. Apply-stage amendments (2026-11-23)

The accepted findings of the simplify stage
(`state/gdd/runs/2026-11-23/symmetry-rf-w4-12c-process-recorder-port-simplif/answer.md`) were
applied to the merged slice (`51178508`, uncommitted working tree). Every finding was accepted;
only F8's option (b) was not applied, because the finding itself records it as conflicting with the
plan §8 decision (no method value per write) and requiring conductor sign-off — option (a), the
fallback-mapping test, is in.

- F1: the `Invocation` mirror comment names every reader (opencode, grant-less direct constructions,
  test doubles replacing the launch seam); same 4 lines, no code change.
- F2/F3: the `recorderFuncs.Capabilities` forwarder and the duplicated method comment are deleted;
  the promoted `RecorderFuncs.Capabilities` keeps `recorderFuncs` satisfying `Recorder`
  (`NewRecorder(writes).Capabilities()` in `recorder_test.go` still pins the value).
- F4: `processRecorder`'s doc comment is three lines.
- F5: the duplicate `MirrorRecorderMembers()` calls in `goal_admission_tx.go` and the Linux-only
  witness are deleted; between them and `PopulateOpencodeRecorderMembers` no reader exists, and the
  populate mirrors the request copy the adapters read.
- F6: `probe_test.go` adds `TestRunBoundedCommandDoesNotInvokeRecorderWrites` (an attached recorder
  must stay at zero calls) and `TestEphemeralInvocationRemovesEveryDurableWrite` (a fully mirrored
  invocation strips to zero capabilities).
- F7/D1: `MirrorRecorderMembers` mirrors `recorderWrites(recorder)`, so a false capability stays
  nil like the legacy absent callback and `recorderFuncs` mirrors allocate no method value;
  `TestMirrorRecorderMembersKeepsAbsentWritesNil` pins it.
- F8(a): `TestRecorderWritesMapsEveryCapabilityToItsMethod` pins the `recorderWrites` fallback
  mapping (all ten present → each field calls its own method; sparse capabilities → only the true
  ones are mapped).
- D2: `RunBoundedCommand` strips through `ephemeralInvocation` and the package-level
  `noDurableWrites = NewRecorder(RecorderFuncs{})`; the recorder is replaced, not merely nil'd, so a
  mirrored invocation can no longer keep a live write. The three legacy member clears are gone; the
  §4 grep shows `probe.go` no longer appears there.
- D3: the §2 grep output replaced the `PENDING` placeholder.

Refreshed at their single source by this stage: §1 hashes, §2 grep, §4 grep, §7.1 mutants, §8 gate
rows and coverage table, §9 scope and line counts. Not re-run: the base-side coverage worktree
(removed at implement stage; the base column is unchanged by construction). The advisory
`golangci-lint run ./internal/execution/... ./internal/harness/...` stays at the recorded 105
issues.

## 13. Fix-stage amendments (2026-11-23)

The accepted findings of the review stage
(`state/gdd/runs/2026-11-23/symmetry-rf-w4-12c-process-recorder-port-review/answer.md`,
STD-01…STD-05) were applied on top of `5945f5d4` and are committed as `bd819940`, the branch HEAD
review 1 reviewed (no push). Every finding was accepted; none was judged invalid.

- STD-01: `SOURCE_PATHS` carried `daemon/internal/app/goal_admission_tx.go` twice — the implement
  stage added it at the sorted position while W4-01's append already held the tail entry
  (`git blame -L 128,134` at the base revision attributes the existing entry to `adfe7d6a`, PR
  #234; the W4-01 record §1.1 states the append). The added duplicate is deleted, and the §10
  bullet and the plan §9 bullet no longer claim the path was missing. Re-run: `python3
  scripts/ci/test-omo-control-native-evidence.py` exit 0 (`Ran 15 tests … OK`); `SOURCE_PATHS`
  holds 62 entries covering 62 distinct paths.
- STD-02: `PopulateOpencodeRecorderMembers` now copies the three request-level members from the
  invocation mirrors (`request.PersistProcess = request.Invocation.PersistProcess`, …) after
  `MirrorRecorderMembers`, so all thirteen frozen members share one absence semantics — a false
  capability stays nil, exactly the legacy absent callback, where the previous recorder method
  values stayed non-nil and failed closed with `errRecorderWriteUnavailable`. Probe: the new
  `TestPopulateOpencodeRecorderMembersKeepsAbsentRequestWritesNil` goes red when this change alone
  is reverted (`go test -count=1 -run TestPopulateOpencodeRecorderMembers ./internal/harness/`,
  exit 1, `an absent write was populated onto the request as present`) and green on the committed
  tree; §7.2 re-runs the
  previous method-value body as mutant mF1 and re-expresses m4b against the new line.
- STD-03: the m5 row's first red line is the frozen-side `mask 10000` line, with the port-side line
  second (§7, §7.2): `SupervisorHandoffRequested` has one implementation that both assertions call,
  and the frozen assertion at `recorder_test.go:57` executes before the port assertion at `:61` in
  the same mask iteration.
- STD-04: §9's post-apply line counts for `app/goal_admission_tx.go` and
  `app/linux_owner_witness_linux.go` are corrected to 1455 and 3897 (`wc -l` and
  `awk 'END{print NR}'` agree per file; both files are newline-terminated).
- STD-05: the W6-04 entry in `docs/TODO.md` and §2's rule sentence now read that the mirrors are
  populated from the recorder's nil-gated write functions (`recorderWrites`), not method values.
- Host-path hygiene: the WSL mount path in the header and §8 and the vet log path are written as
  `<worktree>`/`<scratch>`; §9's two scan sentences are prose (no pattern bytes), and a byte scan
  for the backslash-u escape pair over the changed and new files finds none.

Refreshed at their single source by this stage: §1 hashes, §3 grep outputs, §7 m5 row, §7.2, §8
gate rows and coverage note, §9 scope and line counts.

## 14. Review-round amendments (2026-11-23)

Review 1 (the P3-only report beside this record's evidence bundle) found no code defect; the
accepted findings changed three things inside the owned set and two record documents, and the one
advisory record note (P3-04) is now in §11.

- P3-03: `recorderParityCases()` gains three shapes — the production marker+atomic+authority+stop
  set (expected atomic, stop: the atomic write skips both legacy writes), handoff commit-then-stop
  (expected `commit-supervisor-handoff` with the launcher's frozen `startedAt`, then the stop
  receipt at finalize) and unproven-on-close-failure (expected marker, authority, then one
  `persist-containment-unproven` after the failed close, with no stop receipt). The production
  shape kills the review's M2 edit (m6, §7.3), so the mutant no longer survives the slice's own
  parity test.
- P3-05: `TestRecorderFuncsEveryAbsentWriteFailsClosed` drives all ten `recorderFuncs` methods
  from a zero `RecorderFuncs` and asserts `errors.Is(err, errRecorderWriteUnavailable)`; the nine
  methods that sat at 0% statement coverage now show 66.7% in-package (the nil branch; the
  delegate branch runs only from the app package), and m7 (§7.3) shows a wrapper failing open is
  red.
- P3-01: the header, §9's fix-stage line, §13's lead sentence, this record's §7.3/§8/§14 and the
  plan's header and §9 name `bd819940` as the committed fix-stage revision (review 1's slice); §1
  refreshed the two changed rows, §5.1 the shape list and §9 the review-round line counts.
- P3-04 (record note): §11 states that the launcher's handoff positive path is provable only by
  the native/e2e witness runs, not by unit tests.
- Review-round self-finding (retry-count flake, §7.4): the permanently failing parity shape's exact
  attempt count and joined error text are scheduling artifacts of the runner's retry-by-design
  finalization (base and head behave identically; the process-completion path and `Terminate`'s
  result-done path may both finalize). The row now asserts the failing write's identity, the [2,3]
  retry range and the stop invariant.

Probes on the final tree (working directory `daemon/` unless a row says otherwise): the two new
test runs are green (10 absent-write subtests, 8 parity subtests, and the failing row's 500/500);
m6 and m7 are red then reverted with the pre-mutant hashes restored (§7.3); the gate rows in §8
re-ran with exit 0, and §7.4 re-ran the failing row 8000 times under load.
