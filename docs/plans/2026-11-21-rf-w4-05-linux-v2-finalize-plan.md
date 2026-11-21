# rf-w4-05 Linux v2 finalize pipeline plan — one close pipeline + one `retryMutation` + D6 single source

Contract: `goal-rf-w4-05-linux-v2-finalize.md` (GDD state root, 2026-11-21) with its three
conductor decisions (Wave-4 draft, W4 review reconcile, lesson-32 control-flow preservation) and
the program-wide rules (plan-file ownership, host-path hygiene, DLP escape-sequence rule).

Baseline: `daemon/internal/execution/linux_owner_v2_linux.go` at this worktree's base
(`refactor/rf-w4-03-runner-decomposition`, HEAD `376ff525`; contract re-derivation base
`origin/main 0fa9b705`). `git log 0fa9b705..HEAD -- daemon/internal/execution/linux_owner_v2_linux.go`
is empty and the file is **2,753 L**, sha256
`914dae6760b68b12481287bb7ecafdce75fadf4d08718b899e99085aaffc09e8`, so every contract line number
below is valid at both revisions. W4-03's runner decomposition is present in this base
(`runner.go` 2,756 L, `runner_pipes.go` added): the conditional `runner.go` owned region (the
ack-stage table) and the retry call sites are editable here.

Measured anchors at the baseline (re-measured, exact):

| anchor | line | anchor | line |
| --- | ---: | --- | ---: |
| `finalize` definition | 1417 | `finalizePreGate` definition | 2092 |
| gate dispatch (`owner.gate == nil`) | 1420-1448 | pre-gate preconditions guard | 2093-2095 |
| `ensureGateHandoff` / `ensureCommitReadback` | 1449-1454 | pre-gate `stop` | 2102-2107 |
| gate `stop` + pre-gate-evidence guard | 1455-1461 | pre-gate evidence guard + `ValidateFor` + equality | 2108-2118 |
| gate state read under `mu` | 1462-1472 | pre-gate candidate + `Validate()` | 2119-2128 |
| handoff/proof guard | 1473-1475 | pre-gate state read | 2129-2133 |
| receipt Equal-reconcile | 1476-1485 | receipt reconcile (`Equal(candidate)`) | 2134-2137 |
| `mirrorStop` | 1486 | persist pre-gate receipt | 2138-2146 |
| persist receipt | 1487-1500 | pre-gate ack | 2147-2163 |
| ack receipt | 1501-1516 | pre-gate leaf removal | 2164-2183 |
| leaf removal | 1517-1526 | pre-gate clear | 2184-2193 |
| release-capability snapshot + validation | 1527-1531 | pre-gate capability snapshot + validation | 2194-2197 |
| promotion hop | 1532-1535 | pre-gate release | 2198-2216 |
| clear | 1536-1543 | pre-gate retire | 2217-2224 |
| helper release (+ lost-ACK retirement) | 1544-1566 | no-checkout clone finalize | 2225-2228 |
| anchor retirement | 1567-1574 | `ownerReleased` | 2229-2231 |
| `ownerReleased` | 1575-1578 | | |

Retry family (definition + call sites inside this file): `runLinuxV2JournalMutation` `:2587`
(13 sites), `runLinuxV2MutationRetry` `:2591` (12 production references: 8 outer call sites + 4
inside the deleted wrappers), `retryLinuxV2` `:2603` (10),
`runLinuxV2ReceiptMutation` `:2605` (1), `runLinuxV2PreGateReceiptMutation` `:2609` (1); one
further call site in `linux_owner_v2_test.go:479` (the ack-lost oracle). Handler twins
`retrySupervisorHandoffReceipt`/`retrySupervisorHandoffClear` (base `0fa9b705`
`runner.go:2484-2504`, HEAD `runner.go:2522-2543`) stay byte-identical.

Ack-stage vocabulary: ten constants `runner.go:216-225`, consumed by
`app/linux_owner_witness_linux.go:2458-2469` (`linuxV2NativeWitnessAckLossSupported`). The
witness layer's own stage list (`:55-75`, `linuxV2NativeWitnessHoldSupported`) is a different
vocabulary (16 stage names) and is out of scope.

## Public / API shape

Nothing exported changes except one additive symbol for D6:

| addition | kind | home |
| --- | --- | --- |
| `LinuxV2AckStages` | exported `[...]string` value, the ten ack-stage names in declaration order | `runner.go`, immediately below the ten constants |
| `linuxV2CloseSteps` | unexported interface: one close path's typed steps | `linux_owner_v2_linux.go` |
| `linuxV2CloseGateSteps`, `linuxV2ClosePreGateSteps` | unexported step sets | `linux_owner_v2_linux.go` |
| `linuxV2HelperControl` | unexported interface over the four close-path control calls | `linux_owner_v2_linux.go` |
| `linuxV2CloseSeams` | unexported struct of three nil-default CI doubles (see "Test seams") | `linux_owner_v2_linux.go` |
| `retryMutation` | replaces the five retry names | `linux_owner_v2_linux.go` (retry region) |

Frozen byte-identical: every error string, wrap text and `errors.Join` order of the two close
paths; the ten stage-name strings; the persisted receipt/journal bytes; every CI-gated test name;
`promoteCloneAfterStop` internals; `liveOwnerStatus`; `finalizeNoPrepareAbort`; `stop`; the owner
struct fields other than the added seam field; `runner.go` outside the ack-stage table.

## The one close pipeline

`runClosePipeline(steps linuxV2CloseSteps) error` owns the sequence; the two wrappers keep only
their path-specific prologue (evidence proof, receipt candidate, owner-state read) and build their
step set:

```
persist receipt → acknowledge receipt → remove leaf →
claim release capability → [promotion hop] → clear journal →
claim release capability (pre-gate path) → release helper → retire anchor → settle
```

`linuxV2CloseSteps` is the path's half: `persistReceipt`, `acknowledgeReceipt`, `removeLeaf`,
`claimRelease`, `promoteClone`, `clearJournal`, `releaseHelper`, `retireAnchor`, `settle`, plus
`claimsCapabilityAfterClear() bool`. The gate path claims before the hop and promotes the clone
where base `finalize` did (`:1532-1535`, after the snapshot `:1527-1531`, before clear
`:1536-1543`); the pre-gate path claims after its clear (base `:2184-2197`), and `promoteClone` is
a no-op. Moving the hop after clear is a failure: it is pinned by a characterization test plus a
mutant, and by the step table below.

Step table (base lines → HEAD step). Gate path:

| step | base | HEAD step | guard/flag preserved |
| --- | ---: | --- | --- |
| persist receipt | 1476-1500 | `linuxV2CloseGateSteps.persistReceipt` | `Equal` reconcile, `ValidForHandoff`, `owner.handoff` update, callback wrap text |
| acknowledge receipt | 1501-1516 | `.acknowledgeReceipt` | `!mirrorStop && !receiptAcked`, first-ACK-error witness capture, `stopReceiptAcked` |
| remove leaf | 1517-1526 | `.removeLeaf` | `normalRelease == nil` guard, `removeAfterReceipt`, `owner.normalRelease` |
| claim capability | 1527-1531 | `.claimRelease` | `Snapshot()` + `ValidFor(*handoff)`, one error text |
| promotion hop | 1532-1535 | `.promoteClone` | `promoteCloneAfterStop(ctx, receipt, release)`, error returned unwrapped |
| clear | 1536-1543 | `.clearJournal` | `!clearDone`, `journalCommitted` branch, both wrap texts, `clearDone` |
| release helper | 1544-1566 | `.releaseHelper` | `!mirrorStop && !helperReleased`, `retryMutation`, lost-ACK retirement join, both flags |
| retire anchor | 1567-1574 | `.retireAnchor` | `!anchorRetired`, `retireAnchorRun`, `anchorRetired` |
| settle | 1575-1578 | `.settle` | `ownerReleased` |

Pre-gate path:

| step | base | HEAD step | guard/flag preserved |
| --- | ---: | --- | --- |
| persist receipt | 2134-2146 | `linuxV2ClosePreGateSteps.persistReceipt` | `Equal(candidate)` guard, callback wrap text, `reservation.PreGateStopReceipt` |
| acknowledge receipt | 2147-2163 | `.acknowledgeReceipt` | `!preGateReceiptAcked`, witness capture, wrap text, `preGateReceiptAcked` |
| remove leaf | 2164-2183 | `.removeLeaf` | `preGateRelease == nil`, leaf then parent completion, wrap text, `preGateRelease` |
| clear | 2184-2193 | `.clearJournal` | `!clearDone`, `ClearReservationV2`, wrap text, `clearDone` |
| claim capability | 2194-2197 | `.claimRelease` | `SnapshotPreGate()` + `ValidFor(receipt)`, one error text |
| release helper | 2198-2216 | `.releaseHelper` | `!helperReleased`, `retryMutation`, lost-ACK retirement join, both flags |
| retire anchor | 2217-2224 | `.retireAnchor` | `!anchorRetired`, `retireAnchorPreGate` |
| settle | 2225-2231 | `.settle` | `finalizeNoCheckoutCloneAfterRetirement`, `ownerReleased` |

`promoteClone` exists only on the gate set; the pre-gate set implements it as `return nil` so the
driver keeps one sequence. Wrapper sizes: `finalize` 163 → ≤60 L (dispatch + evidence proof +
candidate + state read); measured 163 → **79 L** (the gate reconcile block stays byte-identical
in the wrapper, see §A/evidence §5) and `finalizePreGate` 141 → **59 L**.

## `retryMutation`

```go
// retryMutation runs one idempotent journal mutation at most twice, with the
// clone of every attempt built inside callback. A first attempt that reports
// ErrLinuxV2MutationAckLost already took durable effect and its response was
// lost, so it is never replayed; any other failure is retried once and joined.
func retryMutation(callback func() error) error {
	first := callback()
	if first == nil || errors.Is(first, ErrLinuxV2MutationAckLost) {
		return first
	}
	second := callback()
	if second == nil {
		return nil
	}
	return errors.Join(first, second)
}
```

Byte-identical semantics to `runLinuxV2MutationRetry` (same call count, same sentinel short
circuit, same join). Call-site migration:

| old | new | count |
| --- | --- | ---: |
| `runLinuxV2MutationRetry(cb)` | `retryMutation(cb)` | 8 |
| `retryLinuxV2(cb)` | `retryMutation(cb)` | 10 |
| `runLinuxV2JournalMutation(cb, value)` | `retryMutation(func() error { return cb(value.Clone()) })` | 13 |
| `runLinuxV2ReceiptMutation(cb, handoff, receipt)` | `retryMutation(func() error { return cb(handoff.Clone(), cloneLinuxV2StopReceiptValue(receipt)) })` | 1 |
| `runLinuxV2PreGateReceiptMutation(cb, reservation, receipt)` | `retryMutation(func() error { return cb(reservation.Clone(), receipt.Clone()) })` | 1 |
| `runLinuxV2MutationRetry` in `linux_owner_v2_test.go:479` | `retryMutation` (same closure, same assertions) | 1 |

The clone stays inside the attempt closure at every value-carrying site, so attempt 2 sees an
unmutated input (base behaviour, now pinned by a counting fake; the mutant hoists the clone above
`retryMutation`). The five names are deleted. The ack-lost oracle keeps its name and assertions;
only the identifier of the function it exercises changes, because the contract replaces that
function.

## D6 — one ten-name source

`runner.go` gains `LinuxV2AckStages = [...]string{…}` listing the ten constants in declaration
order. `linuxV2NativeWitnessAckLossSupported` becomes
`slices.Contains(execution.LinuxV2AckStages[:], stage)` (`slices` is already imported in that
file). No name is added, removed or renamed; all ten strings stay byte-identical; the witness
layer's separate vocabulary is untouched. The completeness test asserts the exact ten-name,
ten-string source and that every name is accepted by the consumer's contract (membership over the
source); the mutant deletes one name from the source and must turn it red.

## Test seams (recorded, minimal, nil-default)

The four fence invariants and the pre-gate fences can only be exercised in CI if the close
pipeline's two hard machine-local dependencies can be doubled. At the baseline the close paths
cannot run at all in a test process: `helper.Control` is a concrete
`*platform.LinuxOwnerHelperV2Client` (a live socket client), and the opaque
`platform.LinuxCgroupV2Release` capability can only be minted by a real cgroup removal inside
package `platform` (its `issued` field is unexported, and package `platform` is out of scope). The
old tests therefore inject a fake `closeLinuxV2Containment` (`linux_owner_v2_linux.go:374`
(base) → `owner.finalize`) and the real `finalize` never executes.

Seams (all on the owner, all nil in production, each with a fallback to the exact platform call):

| seam | doubles | production fallback |
| --- | --- | --- |
| `linuxV2CloseSeams.helperControl linuxV2HelperControl` | the four close-path control calls (`AcknowledgeStopReceipt`, `ReleaseAfterClear`, `AcknowledgePreGateStopReceipt`, `ReleaseAfterPreGateClear`) | `helper.Control` on the bound helper |
| `linuxV2CloseSeams.ownerReleaseSnapshot` | `platform.LinuxCgroupV2Release` → `authority.SupervisorHandoffReleaseProof` | `release.Snapshot()` |
| `linuxV2CloseSeams.preGateReleaseSnapshot` | `platform.LinuxCgroupV2Release` → `authority.LinuxCgroupPreGateReleaseProof` | `release.SnapshotPreGate()` |

The two snapshot seams replace only the *snapshot*; the pipeline still performs the real
`issued`/`ValidFor` guards, so a double that reports an unfaithful proof fails the real guard. What
stays outside CI execution and therefore byte-identical: the leaf/parent removal calls
(`removeAfterReceipt`, pre-gate removal — the tests preset the capability, exercising the
"already removed" branch), anchor retirement (`RetireHelper`/`RetirePreGateHelper` — the tests
preset `anchorRetired`), and `promoteCloneAfterStop` internals (untouched, out of scope). `stop`
keeps `helper.Control` and is byte-identical; the tests preset `stopEvidence`, which is the
persisted-evidence branch of the same function.

## Characterization tests (commit-1 order: tests first, each red on its fence mutant)

Delivery note: the frozen history does not show the tests red before the unification - the tests
and the refactor landed in one commit and the pre-gate capability-claim position fence arrived at
apply; the deviation is recorded in
`docs/evidence/rf-w4-05-linux-v2-finalize-2026-11-21.md` §12.

New file `daemon/internal/execution/linux_owner_v2_finalize_pipeline_test.go` (`//go:build linux &&
amd64`, so it runs on the CI Linux daemon job and in WSL), plus one cross-platform test file for
the vocabulary source, plus one retargeted call in `linux_owner_v2_test.go`. Every test calls the
real `finalize()` / `finalizePreGate()` on a hand-built `linuxV2ProcessOwner`.

| # | test | fence | mutant that must turn it red |
| --- | --- | --- | --- |
| 1 | `TestLinuxV2FinalizeReconcilesCachedReceiptBeforeAnyMutation` | receipt Equal-reconcile | delete the `Equal` guard (persist/clear then run) |
| 2 | `TestLinuxV2FinalizeMirrorStopSkipsStopReceiptAck` | mirror-stop ack skip | drop `!mirrorStop` from the ack guard |
| 3 | `TestLinuxV2FinalizePromotionHopStaysBeforeClear` | promotion-hop position | move the hop after clear; move the hop before the capability claim |
| 4 | `TestLinuxV2FinalizeClearFollowsDurableReceiptAndAck` | clear-after-receipt | clear before the receipt callback / before the ack |
| 5 | `TestLinuxV2FinalizeReleasesHelperOnlyAfterClear` | never release before clear | release before clear |
| 6 | `TestLinuxV2FinalizePreGateReleasesHelperOnlyAfterClear` | pre-gate clear + pre-gate release | release before clear; clear before ack |
| 7 | `TestLinuxV2FinalizeMutationAttemptsSeeFreshClones` | fresh clone per attempt | hoist the clone out of the attempt closure |
| 8 | `TestLinuxV2MutationAckLostIsNotReplayed` (existing) | ack-lost no-replay | replay after `ErrLinuxV2MutationAckLost` |
| 9 | `TestLinuxV2AckStageVocabularyIsComplete` (new, cross-platform) | D6 ten-name source | drop one name from `LinuxV2AckStages` |

Delivered beyond the plan: `TestLinuxV2FinalizeDoesNotReplayLostReceiptAck` (the same policy
through the real `finalize`), `TestLinuxV2FinalizeStopsBeforeClearWhenReceiptAckFails` and
`TestLinuxV2FinalizeResumesFromAlreadyAppliedSteps`, plus the review-round
`TestLinuxV2FinalizeRejectsCapabilityBoundToAnotherReceipt` (the `ValidFor` half of both
capability-claim guards) and the pre-gate clear-input pin.

Tests 1-6 also assert the observed call order of the fakes (callback order + control-call order),
so the "clear-after-receipt" and "release-after-clear" fences are asserted positively, not only by
error-path negatives. Every mutation is run by the implementer, recorded with its red output, and
reverted (`docs/evidence/rf-w4-05-linux-v2-finalize-2026-11-21.md` §7).

## Control-flow audit (lesson 32)

The evidence record carries one row per base decision point per path: base line → base construct
(early return / fall-through / error short-circuit / branch) → HEAD step + line → preserved yes/no
→ how the step returns there. Rows include the mirror-stop branch, the ack-lost capture branch,
the `normalRelease == nil` / `preGateRelease == nil` reuse branches, the `clearDone`,
`helperReleased` and `anchorRetired` no-op branches, and the lost-ACK retirement join in both
paths. The audit is produced from the base and delivered revisions by the same extraction
commands, not from memory.

## Evidence plan (foreground, sequential — never a harness beside a suite)

1. `gofmt -l`, `go vet ./...`, `GOOS=windows go build ./...`, `GOOS=linux go build ./...`,
   `GOOS=linux GOARCH=arm64 go build ./...` (cwd `daemon`).
2. `go test -count=1 ./internal/execution/...` (Windows subset) and the Linux set via
   `wsl -d Ubuntu-24.04 -u accept` at this worktree's path; `go test -count=1
   ./internal/execution/ -run 'TestLinuxV2|TestRunnerStopReceipt'` focused; `go test -count=1
   ./internal/app/...` (Windows ≈150 s, plus the WSL run for the witness files).
3. `python scripts/ci/check-citation-existence.py` at the slice head; `git diff --check`;
   host-path hygiene (no user-profile or drive path in any added line, checked with the
   program's case-insensitive pattern); DLP (no added line carries a backslash immediately
   followed by the letter `u`, checked with a two-character grep pattern).
4. Mutation battery (tests 1-9 above), each recorded red and reverted.
5. LOC / function-length table, report-only (contract D2).

## Ambiguities and decisions taken

### A. Two extra seams beyond `helper.Control` (contract asked for the minimal `helper.Control` seam)

The contract authorizes "the minimal test seam needed over `helper.Control`" and, separately, says
a fence whose path cannot be executed in CI is excluded and stays byte-identical. Taken
literally, only the `helper.Control` seam is authorized — but the close paths cannot pass their
own release-capability guard in CI without a way to obtain an issued capability, and every fence
after that guard (hop position, clear-after-receipt, release-after-clear, both pre-gate fences)
would then be unexecutable, which would leave almost nothing to unify and contradict the slice.
Option chosen: two additional nil-default snapshot seams that double only the *snapshot*, keeping
the real `issued`/`ValidFor` guard in the pipeline. Option rejected: changing the owner's release
fields to a new interface type (fabricating an issued capability outside `platform` would move the
guard behind the double), and rejecting the fences (defeats the contract). Recorded here and in
the evidence file, with the uncovered surface named.

### B. Promotion-hop observability

The hop is not directly observable in CI: `promoteCloneAfterStop` (out of scope, byte-identical)
validates the opaque capability itself before any clone callback, and a test fixture can only
carry an unissued value. The fence is therefore pinned by two assertions on the same fixture set:
(a) with a valid claim the pipeline stops on the hop's own error and has not cleared; (b) with an
unissued claim the pipeline stops on the claim error before the hop. Mutant (hop after clear)
fails (a); mutant (hop before claim) fails (b).

### C. `stop`, `promoteCloneAfterStop`, `finalizeNoPrepareAbort`, `liveOwnerStatus`

All stay byte-identical (contract's out-of-scope list). The retry-name migration inside
`finalizeNoPrepareAbort` and `stopAsMirror` is syntax-only (same closure, same call count) and is
recorded in the control-flow audit as a non-behavioural rewrite; no guard, text or ordering there
changes.

### D. Ack-lost oracle call site

`linux_owner_v2_test.go:479` calls one of the five replaced names. Its test name, closure and
assertions stay byte-identical; only the identifier changes, because the contract deletes that
name. Recorded; the mutant run for fence 8 proves the policy is still pinned.

## Out of scope

- `finalizeNoPrepareAbort` restructuring (recorded: it shares `retireAnchorRun`,
  `finalizeNoCheckoutCloneAfterRetirement` and the retry family but not the pipeline sequence).
- `liveOwnerStatus` / `captureWitnessDropStatus` split (F-15 remainder); `promoteCloneAfterStop`
  internals; `runner.go` outside the ack-stage table; package `platform`; app witness logic beyond
  the one validation function; W4-03's decomposition; the witness layer's own 16-stage vocabulary.

## Measured results (filled at delivery)

- `finalize` **163 → 79 L** (advisory `gocyclo` 43 → 22) and `finalizePreGate` **141 → 59 L**
  (36 → 17); the shared driver is 34 L with ten methods per path (gate 130 L, pre-gate 135 L).
- Retry family: five names (22 L, 33 call sites + 1 test call) → `retryMutation` (11 L, 33 call
  sites + 1 test call); the clone stayed inside every value-carrying attempt closure.
- `runner.go` +20 L (one additive hunk: the ten-name source); `linux_owner_witness_linux.go`
  −6 L net (the ten-arm switch → one derivation); `linux_owner_v2_linux.go` +323 L net
  (544 +/221 − against the 0fa9b705 base). The plan row's −150…−260 dLOC is not met and is
  report-only per D2.
- Tests: 14 new tests in `linux_owner_v2_finalize_pipeline_test.go` (the seven planned
  pipeline-file fences — rows 1-7 — plus the reused-durable-receipt, ack-lost-through-`finalize`,
  failed-acknowledgement, resume-from-applied-steps, pre-gate capability-claim position and
  lost-release-ACK readback cases, and the review-round capability-bound-to-another-receipt case),
  the retargeted ack-lost oracle and the cross-platform
  vocabulary test; 13 mutants (M1–M12 including M3b, of which M9 was added at the apply stage and
  M10–M12 by the fix pass for the whole-increment review) each observed red and reverted,
  including the consumer-side app red for the dropped stage name, plus the review-round mutants
  re-run after the P3 fixes (N1/N2/N4 red, N5 green — the entry-snapshot equivalence).
- Gates: `gofmt -l`, `go vet ./...`, three builds, Windows execution + app, WSL focused + WSL
  execution, the WSL witness subset and the citation gate all exit 0; the full WSL app suite does
  not complete on this host (nondeterministic admission/httptest HTTP-dial hangs that the
  untouched base revision shows in the same family and at the same first test — evidence
  §11/§12); `git diff --check`, host-path and DLP checks clean.
- Advisory lint: no new finding; the Windows pass is identical to W4-03's 9 findings and the
  Linux pass adds only the linux file's pre-existing findings.

Full evidence — the filled step table, the retry call-site table with base→HEAD line pairs, the
per-path control-flow audit, the mutation battery with observed output, the ten-name consumer
census, every gate with its exit code, and the residual limits — is in
`docs/evidence/rf-w4-05-linux-v2-finalize-2026-11-21.md`.
