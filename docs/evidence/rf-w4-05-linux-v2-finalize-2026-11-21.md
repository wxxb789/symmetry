# rf-w4-05 Linux v2 finalize pipeline — evidence record (2026-11-21)

Subject: `daemon/internal/execution/linux_owner_v2_linux.go` (pipeline + retry unification),
`daemon/internal/execution/runner.go` (the ack-stage single source, additive),
`daemon/internal/app/linux_owner_witness_linux.go` (`linuxV2NativeWitnessAckLossSupported`
derivation), the two new test files (`linux_owner_v2_finalize_pipeline_test.go`,
`linux_v2_ack_stages_test.go`) and one retargeted call in `linux_owner_v2_test.go:479`.
Uncommitted on this worktree's base `376ff525` (W4-03 branch head, which includes W4-03's
runner decomposition); the contract re-derivation base is `origin/main 0fa9b705`, and
`git log 0fa9b705..376ff525 -- daemon/internal/execution/linux_owner_v2_linux.go` is empty, so
every base line number below is valid at both revisions (base file sha256
`914dae6760b68b12481287bb7ecafdce75fadf4d08718b899e99085aaffc09e8`).

Baseline sizes: `linux_owner_v2_linux.go` **2,753 L**; `finalize` `:1417-1579` (**163 L**,
42 decision-point occurrences); `finalizePreGate` `:2092-2232` (**141 L**, 35); retry family
`runLinuxV2JournalMutation` `:2587` (3 L, 12 call sites), `runLinuxV2MutationRetry` `:2591`
(11 L, 11 call sites + 1 test call), `retryLinuxV2` `:2603` (2 L, 10 call sites),
`runLinuxV2ReceiptMutation` `:2605` (3 L, 1 call site), `runLinuxV2PreGateReceiptMutation`
`:2609` (3 L, 1 call site).

Contract: `goal-rf-w4-05-linux-v2-finalize.md` (GDD state root) with its three conductor
decisions (Wave-4 draft, W4 review reconcile, lesson-32 control-flow preservation). Plan:
`docs/plans/2026-11-21-rf-w4-05-linux-v2-finalize-plan.md`.

## 1. Shape delivered

| file | change |
| --- | --- |
| `linux_owner_v2_linux.go` | `finalize` 163 → 79 L and `finalizePreGate` 141 → 59 L; one `runClosePipeline` driver (34 L) + two step sets (`linuxV2CloseGateSteps` 10 methods, `linuxV2ClosePreGateSteps` 10 methods); `linuxV2HelperControl`, `linuxV2CloseSeams` + 3 accessors; `retryMutation` replaces the five retry names (11 L definition, 33 call sites + 1 test call) |
| `runner.go` | `LinuxV2AckStages` added below the ten constants (`:228-246`): one exported ten-name source, no name added, removed or renamed, table bytes unchanged |
| `linux_owner_witness_linux.go` | `linuxV2NativeWitnessAckLossSupported` `:2458-2463` derives membership from `execution.LinuxV2AckStages` with `slices.Contains`; the witness stage vocabulary `:55-75` is untouched |
| `linux_owner_v2_finalize_pipeline_test.go` (new, 756 L) | the CI characterization tests of the close fences (12 tests: 11 fences + the pre-gate capability-claim position pin) |
| `linux_v2_ack_stages_test.go` (new, no build tag) | the ten-name source completeness test |
| `linux_owner_v2_test.go` | `TestLinuxV2MutationAckLostIsNotReplayed` calls `retryMutation` (same closure, same assertions, same test name) |

Line totals: `linux_owner_v2_linux.go` 2,753 → 3,075; `runner.go` 2,756 → 2,776 (W4-03 base
plus the 20-line source); the two test files add 756 + 22 L; against the 0fa9b705 base the
numstat is `543 +/221 −` for `linux_owner_v2_linux.go` and `1 +/1 −` for the test call site. The
plan row's −150…−260 dLOC is not met (net +322 L in the source file: the ten step methods carry
their own doc comments and signatures) and is report-only per contract D2.

Function sizes (delivered tree, measured with a brace-matched awk over both revisions):

| function | base L / decisions | head L / decisions |
| --- | --- | --- |
| `finalize` | 163 / 42 | **79** / 21 (wrapper) + driver 34 / 15 + gate steps 20 |
| `finalizePreGate` | 141 / 35 | **59** / 16 (wrapper) + pre-gate steps 20 |
| `runClosePipeline` (new) | — | 34 / 15 |
| gate steps (10 methods) | — | 5+19+24+14+8+3+12+27+12+6 L |
| pre-gate steps (10 methods) | — | 4+15+24+20+14+8+5+24+12+9 L |
| retry family | 22 L across 5 names | `retryMutation` 11 L |

Complexity per function is under the advisory `funlen`/`gocyclo` thresholds; the advisory lint
distribution is reported in §7.

## 2. Pipeline step table (base → HEAD)

Both wrappers keep their path-specific prologue (evidence proof, receipt candidate, receipt
reconcile, owner-state read) and then call the one driver. `runClosePipeline` owns the shared
order; `promoteClone` is the injected hop (gate: the real hop; pre-gate: a no-op), and
`claimsCapabilityAfterClear` selects the path's base claim position.

Gate path:

| step | base | HEAD | notes |
| --- | ---: | --- | --- |
| receipt reconcile (cached `Equal` + `ValidForHandoff`) | 1476-1485 | `finalize` 1483-1500 | **stays byte-identical in the wrapper**, see §5 |
| persist receipt | 1486-1500 | `linuxV2CloseGateSteps.persistReceipt` `:1655-1673` | mirror-stop snapshot `:1501` stays with the wrapper state read |
| acknowledge receipt | 1501-1516 | `.acknowledgeReceipt` `:1678-1701` | `!mirrorStop && !receiptAcked`, witness capture, `stopReceiptAcked` |
| remove leaf | 1517-1526 | `.removeLeaf` `:1706-1719` | `normalRelease == nil`, `removeAfterReceipt`, owner field |
| claim capability | 1527-1531 | `.claimRelease` `:1723-1730` | snapshot seam + real `issued`/`ValidFor` guard |
| promotion hop | 1532-1535 | `.promoteClone` `:1734-1736` (driver call `:1599`) | injected after the claim, before the clear |
| clear | 1536-1543 | `.clearJournal` `:1741-1752` | `!clearDone`, `journalCommitted` branch, `clearDone` |
| release helper | 1544-1566 | `.releaseHelper` `:1757-1783` | `!mirrorStop && !helperReleased`, lost-ACK retirement join |
| retire anchor | 1567-1574 | `.retireAnchor` `:1787-1798` | `!anchorRetired` |
| settle | 1575-1578 | `.settle` `:1802-1806` | `ownerReleased` |

Pre-gate path:

| step | base | HEAD | notes |
| --- | ---: | --- | --- |
| receipt reconcile (cached `Equal`) | 2134-2137 | `finalizePreGate` 2363-2371 | stays in the wrapper, CI-executable here (fence 1) |
| persist receipt | 2138-2146 | `linuxV2ClosePreGateSteps.persistReceipt` `:2409-2423` | callback + reservation publish |
| acknowledge receipt | 2147-2163 | `.acknowledgeReceipt` `:2427-2448` | `!preGateReceiptAcked`, witness capture |
| remove leaf | 2164-2183 | `.removeLeaf` `:2456-2475` | pre-gate authorization, leaf then parent completion |
| clear | 2184-2193 | `.clearJournal` `:2479-2492` | `ClearReservationV2`, `clearDone` |
| claim capability | 2194-2197 | `.claimRelease` `:2496-2504` (driver call `:1611`) | after the clear, exactly as base |
| promotion hop | — | `.promoteClone` `:2508` | `return nil` (base had none) |
| release helper | 2198-2216 | `.releaseHelper` `:2513-2535` | `!helperReleased`, lost-ACK pre-gate retirement join |
| retire anchor | 2217-2224 | `.retireAnchor` `:2539-2550` | `!anchorRetired` |
| settle | 2225-2231 | `.settle` `:2554-2562` | `finalizeNoCheckoutCloneAfterRetirement`, `ownerReleased` |

Driver (`runClosePipeline` `:1588-1621`), the one order both paths share:

```
persistReceipt → acknowledgeReceipt → removeLeaf → [claim if the path claims before the hop]
→ promoteClone → clearJournal → [claim if the path claims after the clear] → releaseHelper
→ retireAnchor → settle
```

## 3. Retry-family call-site table

`retryMutation(callback func() error) error` has the exact `runLinuxV2MutationRetry` semantics
(same call count, same `errors.Is(first, ErrLinuxV2MutationAckLost)` short-circuit, same
`errors.Join(first, second)`), and every value-carrying call site keeps its clone *inside* the
attempt closure so each attempt builds its own clone.

| old shape | new shape | sites | base lines → HEAD lines |
| --- | --- | ---: | --- |
| `runLinuxV2MutationRetry(cb)` | `retryMutation(cb)` | 8 outer | `1143→1156, 1723→1951, 1814→2042, 2022→2250, 2067→2295, 2074→2302, 2185→2483, 2290→2620` |
| `retryLinuxV2(cb)` | `retryMutation(cb)` | 10 | `503→512, 802→815, 1503→1684, 1546→1762, 1741→1969, 1964→2192, 2086→2314, 2149→2433, 2199→2518, 2239→2569` |
| `runLinuxV2JournalMutation(cb, value)` | `retryMutation(func() error { return cb(value.Clone()) })` | 13 | `429→434, 433→440, 446→455, 497→506, 636→645, 643→652, 664→675, 689→700, 725→736, 733→746, 760→773, 1342→1355, 1763→1991` |
| `runLinuxV2ReceiptMutation(cb, handoff, receipt)` | `retryMutation(func() error { return cb(handoff.Clone(), cloneLinuxV2StopReceiptValue(receipt)) })` | 1 | `1488→1657` (gate `persistReceipt`) |
| `runLinuxV2PreGateReceiptMutation(cb, reservation, receipt)` | `retryMutation(func() error { return cb(reservation.Clone(), receipt.Clone()) })` | 1 | `2140→2413` (pre-gate `persistReceipt`) |
| `runLinuxV2MutationRetry` inside the deleted definitions | — | 3 | `2588, 2606, 2610` (no HEAD counterpart) |
| `runLinuxV2MutationRetry` in the ack-lost oracle | `retryMutation` | 1 | `linux_owner_v2_test.go:479` (same closure, same assertions) |

Clone-less sites keep no clone (base semantics); the two base names were the same policy
(`retryLinuxV2` was a one-line forwarder), and the 33 delivered `retryMutation` call sites are
exactly the 8 + 10 + 13 + 1 + 1 migrated sites.

`retrySupervisorHandoffReceipt`/`retrySupervisorHandoffClear` (`runner.go:2484-2504`) are
byte-identical and were not touched.

The value sites where the base callback received a *typed closure parameter* now pass the clone
expression directly (`Callbacks.BindLeaf(owner.reservation.Clone(), …)`), and the value is read at
attempt time instead of once before the retry: `retryMutation` can only re-invoke its callback
after a callback that receives clones, and no concurrent writer mutates the owner's reservation or
handoff during a close sequence, so both evaluations observe the same value. Recorded as a
non-behavioural translation in §4.

## 4. Control-flow audit (lesson 32)

Base decision points (base revision `0fa9b705`) → HEAD equivalent. "unchanged bytes" means the
base line's text survives verbatim at the HEAD line.

### `finalize` (base 1417-1579)

| base | construct | head | preserved | note |
| --- | --- | --- | --- | --- |
| 1420-1441 | no-gate dispatch: pre-gate, no-prepare abort, unresolved-producer exits | 1433-1454 | yes | unchanged bytes |
| 1443-1454 | ensure gate handoff / commit readback exits | 1455-1461 | yes | unchanged bytes |
| 1455-1461 | stop + pre-gate-evidence-for-gate-journal exit | 1462-1470 | yes | unchanged bytes |
| 1462-1472 | owner-state read under `mu`; `helper` local removed | 1471-1479 | yes | the `helper` snapshot is replaced by the `helperControl()` seam accessor (production: the same client) |
| 1473-1475 | handoff/proof exit | 1480-1482 | yes | unchanged bytes |
| 1477 | cached-receipt `Equal` reconcile exit | 1492-1494 | yes | unchanged bytes; unreachable from CI, see §5 |
| 1480,1483 | candidate adoption + `ValidForHandoff` exit | 1495-1500 | yes | unchanged bytes |
| 1486 | `mirrorStop` derivation | 1501 | yes | unchanged bytes (pure read) |
| 1487 | `!receiptPersisted` write branch | `persistReceipt` 1656 | yes | unchanged predicate |
| 1491-1492 | write failure exit | 1660-1662 | yes | same wrap text |
| 1493-1499 | receipt-bound handoff publish + owner update | 1662-1669 | yes | same order, same clone shape |
| 1499 | cached-receipt else branch | 1671-1672 | yes | same assignment |
| 1502 | `!mirrorStop && !receiptAcked` | `acknowledgeReceipt` 1679 | yes | short-circuit inverted to an early return |
| 1505-1507 | first-ACK-error capture | 1684-1690 | yes | unchanged text |
| 1510-1512 | witness drop capture | 1692-1694 | yes | unchanged |
| 1513-1515 | ack failure exit | 1695-1697 | yes | same wrap text |
| 1520 | `normalRelease == nil` | `removeLeaf` 1694-1696 | yes | same predicate |
| 1521-1525 | removal failure + owner field | 1697-1702 | yes | same call, same field |
| 1530-1532 | `!issued || !ValidFor` exit | `claimRelease` 1711-1715 | yes | same text, real guard on the seam's snapshot |
| 1534-1536 | hop failure exit | `promoteClone` 1722 (driver 1599) | yes | hop injected at the same position |
| 1537 | `!clearDone` | `clearJournal` 1730-1732 | yes | same predicate |
| 1545-1546 | `!mirrorStop && !helperReleased` + release retry | `releaseHelper` 1746-1750 | yes | same predicate, same `retryMutation` |
| 1550-1556 | lost-ACK retirement join | 1754-1764 | yes | same joined texts and order |
| 1557-1563 | success flag updates | 1766-1770 | yes | same fields |
| 1567-1573 | `!anchorRetired` retirement exit | `retireAnchor` 1776-1785 | yes | unchanged |
| 1575-1577 | `ownerReleased` | `settle` 1791-1793 | yes | unchanged |

### `finalizePreGate` (base 2092-2232)

| base | construct | head | preserved | note |
| --- | --- | --- | --- | --- |
| 2093-2095 | armed-grant/never-created precondition exit | 2321-2323 | yes | unchanged bytes |
| 2102-2107 | stop + pre-gate evidence exits | 2324-2332 | yes | unchanged bytes |
| 2112-2118 | `ValidateFor` + outcome equality exits | 2333-2341 | yes | unchanged bytes |
| 2119-2128 | candidate construction + `Validate` exit | 2342-2351 | yes | unchanged bytes |
| 2134-2137 | cached-receipt `Equal` exit | 2363-2371 | yes | unchanged bytes, CI-executable (fence 1) |
| 2139-2141 | receipt write branch | `persistReceipt` 2410-2417 | yes | unchanged predicate and wrap text |
| 2143-2145 | reservation publish | 2418-2421 | yes | same clone, same order |
| 2147 | `!preGateAcked` | `acknowledgeReceipt` 2428 | yes | short-circuit inverted |
| 2151-2159 | first-ACK-error capture, witness drop, wrap text | 2430-2444 | yes | unchanged texts |
| 2171 | `preGateRelease == nil` | `removeLeaf` 2462-2464 | yes | same predicate |
| 2168-2170 | pre-gate authorization construction | 2464-2466 | yes | moved inside the guard (pure construction) |
| 2172-2179 | leaf removal, parent completion, failure exit | 2467-2474 | yes | same call order and text |
| 2180-2182 | capability publish | 2475-2477 | yes | same field |
| 2184 | `!clearDone` | `clearJournal` 2485-2487 | yes | same predicate |
| 2185-2192 | clear retry + wrap + flag | 2488-2491 | yes | same clone-less closure |
| 2194-2196 | `!issued || !ValidFor(receipt)` exit | `claimRelease` 2503-2506 | yes | same text, real guard |
| 2198-2216 | `!helperReleased`, release, lost-ACK join, flags | `releaseHelper` 2519-2535 | yes | same texts and order |
| 2217-2223 | `!anchorRetired` retirement | `retireAnchor` 2545-2552 | yes | unchanged |
| 2225-2230 | no-checkout finalize + `ownerReleased` | `settle` 2561-2567 | yes | unchanged texts |

No row is "changed": every base exit is reachable at the same predicate, records the same text
and returns the same error value. The only structural differences are the guard-inversion to early
returns, the explicit `linuxV2CloseSteps` dispatch, the `helperControl()` accessor, and the moved
`mirrorStop` and pre-gate authorization constructions (pure reads/constructions within the same
step).

## 5. Test seams and the excluded branch (recorded per contract)

Baseline fact: the two close paths cannot run in a CI process without doubles — `helper.Control`
is a concrete `*platform.LinuxOwnerHelperV2Client` (live socket client) and
`platform.LinuxCgroupV2Release` can only be minted by a real cgroup removal inside package
`platform` (unexported `issued` field; package `platform` is out of this slice's scope). The
baseline tests therefore inject a fake `closeLinuxV2Containment` (`runner.go:374` →
`owner.finalize`), which means the real `finalize` never executed.

Delivered seams (`linuxV2CloseSeams`, nil in production, one owner field, three accessors):

| seam | doubles | production fallback |
| --- | --- | --- |
| `helperControl` | the four close-path control calls | `owner.helper.Control` (the exact native client) |
| `ownerReleaseSnapshot` | `platform.LinuxCgroupV2Release` → `authority.SupervisorHandoffReleaseProof` | `release.Snapshot()` |
| `preGateReleaseSnapshot` | `platform.LinuxCgroupV2Release` → `authority.LinuxCgroupPreGateReleaseProof` | `release.SnapshotPreGate()` |

The snapshot seams replace only the snapshot: the pipeline still runs the real
`issued`/`ValidFor` guards on the returned value, and the mutants in §8 prove the fences are
sensitive. Outside CI execution and therefore byte-identical: leaf/parent removal (the tests
preset the capability, exercising the "already removed" branch), anchor retirement (tests preset
`anchorRetired`), `promoteCloneAfterStop` internals, and `stop` (tests preset `stopEvidence`).

Excluded branch: the gate path's cached-receipt `Equal` reconcile (`finalize` `:1492-1494`,
base `:1477-1479`) stays byte-identical in the wrapper and is **not** unified. It is unreachable
whenever the bound handoff validates: `SupervisorHandoff.Validate` requires a cached receipt to
pass `ValidForHandoff` (`supervisor.go:557-573`), and `LinuxCgroupStopProof.ValidateFor`
(`cgroup.go:518-552`) then determines every field the candidate can carry — `matchesStoppedReceipt`
pins all receipt identity fields to the handoff, and the kind branches pin `Issuer`, `Reaper`,
`HelperExit` and both exit proofs (including the anchor-adopted lineage) — so the cached receipt
and the candidate are `Equal` by construction. The reachable instance of the same policy is the
pre-gate reconcile, which the fence test executes on the real `finalizePreGate`.

## 6. Characterization tests and fences

| fence | test | executes |
| --- | --- | --- |
| receipt Equal-reconcile | `TestLinuxV2FinalizeReconcilesCachedReceiptBeforeAnyMutation` | real `finalizePreGate` (conflicting cached receipt → exact error, zero callbacks) |
| mirror-stop ack skip | `TestLinuxV2FinalizeMirrorStopSkipsStopReceiptAck` | real `finalize` (mirror kind: no ack, no helper release; helper kind: ack) |
| promotion-hop position | `TestLinuxV2FinalizePromotionHopStaysBeforeClear` | real `finalize` (valid claim stops on the hop before any clear; unissued claim stops before the hop) |
| clear-after-receipt | `TestLinuxV2FinalizeClearFollowsDurableReceiptAndAck` | real `finalize` (receipt < ack < clear, receipt published, owner settled) |
| release-after-clear | `TestLinuxV2FinalizeReleasesHelperOnlyAfterClear` | real `finalize` (clear < release, `helperReleased`) |
| pre-gate clear + release | `TestLinuxV2FinalizePreGateReleasesHelperOnlyAfterClear` | real `finalizePreGate` (receipt/ack/clear/release order, all four flags) |
| pre-gate capability-claim position | `TestLinuxV2FinalizePreGateClaimsCapabilityOnlyAfterClear` | real `finalizePreGate` (unissued capability + failing `ClearReservationV2`: the durable clear fails before any capability claim) |
| cached-receipt reuse | `TestLinuxV2FinalizeReusesDurableReceiptWithoutRewriting` | real `finalize` (persisted receipt: no rewrite, ack/clear/release) |
| fresh clone per attempt | `TestLinuxV2FinalizeMutationAttemptsSeeFreshClones` | real `finalize` (first attempt mutates its clone and fails; attempt 2 asserts pristine inputs) |
| ack-lost no-replay | `TestLinuxV2FinalizeDoesNotReplayLostReceiptAck` + `TestLinuxV2MutationAckLostIsNotReplayed` | real `finalize` (one attempt, sentinel propagated) and `retryMutation` directly |
| failed receipt acknowledgement | `TestLinuxV2FinalizeStopsBeforeClearWhenReceiptAckFails` | real `finalize` (both attempts fail; the wrapped error preserves the helper error; no clear or release) |
| resume from applied steps | `TestLinuxV2FinalizeResumesFromAlreadyAppliedSteps` | real `finalize` (preset `clearDone`/`helperReleased`: no repeated clear or release) |
| D6 ten-name source | `TestLinuxV2AckStageVocabularyIsComplete` | `LinuxV2AckStages` exact ten names/strings |

Fixtures: the harness builds every value through production predicates (`handoff.Validate`,
`proof.ValidateFor`, `receipt.ValidForHandoff`, `receipt.Validate`, `preGate.ValidateFor`,
anchored on a signed `SignGateNeverProducedAuthenticator` outcome), so a fixture drift fails the
tests instead of silently weakening them.

## 7. Mutation battery (each mutant applied, run red, reverted)

A mutant is accepted only with the observed red output of the test that owns the fence; every
mutant was reverted and the affected invariant re-verified afterwards (grep of the restored line,
`gofmt -l` clean, focused suite green in §11). M9 was added and run by the apply stage under the
same protocol (scratch-copy overlay, tree untouched).

| # | fence | mutant | test | observed |
| --- | --- | --- | --- | --- |
| M1 | receipt Equal-reconcile | delete the pre-gate `Equal(candidate)` guard (`finalizePreGate` `:2366-2368`) | `TestLinuxV2FinalizeReconcilesCachedReceiptBeforeAnyMutation` | FAIL: `finalizePreGate() error = Linux v2 pre-gate release capability differs from its receipt, want the conflicting cached receipt rejected` |
| M2 | mirror-stop ack skip | `if steps.mirrorStop \|\| steps.receiptAcked` → `if steps.receiptAcked` | `TestLinuxV2FinalizeMirrorStopSkipsStopReceiptAck` | FAIL: `mirror close order = []string{"receipt", "ack", "clear"}, want no acknowledgement without a bound helper` |
| M3 | hop after clear | swap the driver's `promoteClone` and `clearJournal` blocks | `TestLinuxV2FinalizePromotionHopStaysBeforeClear` | FAIL: `close order = []string{"receipt", "ack", "clear"}, want the hop before the durable clear and the helper release` |
| M3b | hop before the claim | move the pre-hop `claimRelease` block after `promoteClone` | `TestLinuxV2FinalizePromotionHopStaysBeforeClear` | FAIL: `finalize() error = Linux clone promotion requires the exact durable stop receipt and opaque removed-leaf release, want the unissued capability claim to stop before the hop` |
| M4 | clear-after-receipt | move the driver's `clearJournal` block to the front | `TestLinuxV2FinalizeClearFollowsDurableReceiptAndAck` | FAIL: `close order = []string{"clear", "receipt", "ack", "clear", "release"}, want the durable receipt and its acknowledgement before the Store clear` |
| M5 | never release before clear | move the driver's `releaseHelper` block before `clearJournal` | `TestLinuxV2FinalizeReleasesHelperOnlyAfterClear`, `TestLinuxV2FinalizePreGateReleasesHelperOnlyAfterClear` | FAIL both: `close order = []string{"receipt", "ack", "release", "clear"}` (gate) and `pre-gate close order = []string{"receipt", "ack", "release", "clear"}` |
| M6 | ack-lost no-replay | `if first == nil \|\| errors.Is(first, ErrLinuxV2MutationAckLost)` → `if first == nil` | `TestLinuxV2MutationAckLostIsNotReplayed`, `TestLinuxV2FinalizeDoesNotReplayLostReceiptAck` | FAIL both: `mutation retry error=… calls=2, want one non-replayable ACK-loss attempt` and `finalize() error = <nil>, want the response-loss sentinel` |
| M7 | fresh clone per attempt | hoist `cloneLinuxV2StopReceiptValue(steps.receipt)` above `retryMutation` in the gate `persistReceipt` | `TestLinuxV2FinalizeMutationAttemptsSeeFreshClones` | FAIL: `stop receipt attempt 2 observed a mutated receipt clone` |
| M8 | D6 ten-name source | drop `LinuxV2AckStageJournalCleared` from `LinuxV2AckStages` | `TestLinuxV2AckStageVocabularyIsComplete` (+ the app consumer oracle) | FAIL both: `LinuxV2AckStages = [9]string{…}, want the ten live ACK-loss stages […]` and `TestLinuxV2NativeWitnessAllowsReceiptBackedLostClearAck: receipt-backed lost-clear response rejected` |
| M9 | pre-gate capability-claim position | `linuxV2ClosePreGateSteps.claimsCapabilityAfterClear` `true` → `false` (claim before the durable clear) | `TestLinuxV2FinalizePreGateClaimsCapabilityOnlyAfterClear` | FAIL: `finalizePreGate() error = Linux v2 pre-gate release capability differs from its receipt, want the failed durable clear before any capability claim` |

M8's second failure is the consumer-side proof: the app derives its witness validation from the
single source, so a dropped name reddens an existing witness oracle, not only the new test.

## 8. Ten-name live-consumer census (D6)

All ten names stay; each has a live producer or consumer at the delivered revision:

| stage | live producer/consumer | evidence |
| --- | --- | --- |
| `lease_armed` | runner emits it | `linux_owner_v2_linux.go:841` (`DropAckAfter`) |
| `native_release` | runner emits it | `linux_owner_v2_linux.go:1077` |
| `helper_bound`, `launcher_bound`, `gate_bound`, `handoff_prepared`, `handoff_committed`, `lease_armed`, `checkout_ready`, `stop_receipt_durable`, `native_release`, `journal_cleared` | witness layer injects the acknowledgement loss and validates the stage | app `linux_owner_witness_linux.go:3516-3562` (stage presence), `:3604-3624` (clear stage), `:3664-3670` (runtime gate), `:2135` (marker validation), `:2365` (request validation), `:806`/`:1423` (summary validation) |
| all ten | witness request producers carry the literals | `scripts/ci/linux-v2-native-acceptance.sh:353-356` (`OWNER_ACK_LOSS_STAGES`), `daemon/e2e/production_linux_v2_native_witness_test.go:930-933` (`linuxV2AckLossReplayStages`), `daemon/internal/app/linux_owner_witness_linux_test.go` (`journal_cleared`, `stop_receipt_durable` cases) |
| all ten | single source + derived consumer | `runner.go:228-246` (`LinuxV2AckStages`), app `:2458-2462` (`slices.Contains(execution.LinuxV2AckStages[:], stage)`) |

No name was added, removed or renamed; the ten strings are byte-identical to the baseline table.

## 9. Simplification review (scoped)

Considered and rejected: (a) a shared `acknowledgeCloseReceipt`/`releaseCloseHelper` helper taking
the typed control call, the wrap text and the flag setter as closures — two call sites with
different receipt types, texts and owner flags would trade visible per-path steps for three
closures per call; (b) a single fixed driver order with the claim always before the hop — base
`finalizePreGate` clears before it consumes the pre-gate release proof, so the claim position is
genuinely per-path and `claimsCapabilityAfterClear` names that difference; (c) unifying the
pre-gate leaf removal with `removeAfterReceipt` — the authorizations and completion calls differ,
and the gate removal body is an existing helper kept byte-identical. Kept: the two state carriers
(`linuxV2CloseGateSteps`/`linuxV2ClosePreGateSteps`) with their own typed receipts, because the
gate path's handoff-bound receipt and the pre-gate path's reservation-bound receipt are different
types with different reconcile rules. No abstraction was added that has a single caller, and no
dead carrier field remains (`evidence` was removed from the gate carrier when the reconcile moved
back into the wrapper).

## 10. Witness-reviewer note (D1/D6)

Reviewer surface for the dispatch-only witness jobs: `runner.go:228-246` adds the exported
`LinuxV2AckStages` list **without** adding, removing or renaming a stage name and **without**
changing a stage string, and `linuxV2NativeWitnessAckLossSupported` (`:2458-2462`) now derives its
membership from that list instead of repeating the ten names in a switch. Every stage literal a
witness request, marker, or acceptance script carries is unchanged, so a dispatched
`linux-native-v2-acceptance` run or the env-gated v2 witness matrix sees identical vocabulary.
Those jobs were not dispatched here (zero dispatches at the baseline; contract E8 makes them
reviewer surfaces, not acceptance oracles); the acceptance evidence is the CI-run suites and the
mutation battery above.

## 11. Gates (all foreground, sequential; every command with its exit code)

All commands run in `daemon/` unless stated. "WSL" is
`wsl -d Ubuntu-24.04 -u accept` at this worktree's path
(`/mnt/q/repos/wt-w4-05-linux-v2-finalize/symmetry/daemon`, go1.27.0 linux/amd64).

| gate | command | result |
| --- | --- | --- |
| gofmt | `gofmt -l ./internal/` | no output, exit 0 |
| vet | `go vet ./...` | no output, exit 0 |
| build windows | `GOOS=windows go build ./...` | no output, exit 0 |
| build linux | `GOOS=linux go build ./...` | no output, exit 0 |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` | no output, exit 0 |
| windows execution | `go test -count=1 ./internal/execution/...` | `ok`, exit 0 (9.871 s) |
| windows app | `go test -count=1 ./internal/app/...` | `ok`, exit 0 (116.686 s) |
| WSL focused | `go test -count=1 -run 'TestLinuxV2\|TestRunnerStopReceipt' ./internal/execution/` | `ok`, exit 0 (0.035 s) |
| WSL execution | `go test -count=1 ./internal/execution/` | `ok`, exit 0 (2.359 s); 117 named tests listed (13 new: 12 in the pipeline file + 1 ack-stages; plus the retargeted ack-lost oracle among them) |
| WSL app (full) | `go test -count=1 ./internal/app/...` | exit 1: `FAIL … 600.058s`, test-binary timeout inside `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` (pre-existing; reproduced in the untouched base worktree, §12) |
| WSL app (witness subset) | `go test -count=1 -run 'TestLinuxV2NativeWitness' ./internal/app/` | `ok`, exit 0 (0.739 s) — the Linux-only oracle for the changed app function |
| WSL app (full, first hang skipped) | `go test -count=1 -timeout 900s -skip '^TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart$' ./internal/app/` | exit 1: `FAIL … 900.133s`, timeout inside `TestNativeResumeAdmissionRejectsIncompatibleOrUnavailableRetainedSessionBeforeNativeStart` (admission family, unrelated to the changed code; §12) |
| citation gate | `python scripts/ci/check-citation-existence.py --base 0fa9b705 --head HEAD` | exit 0: `no audited document added or modified between 0fa9b705cb6a and 376ff5254eac; nothing to check` (the slice adds only `docs/plans/` and `docs/evidence/`, outside the gate's audited corpus) |
| whitespace | `git diff --check` | no output, exit 0 |
| advisory lint (windows) | `golangci-lint run --config .golangci.yml ./internal/execution/...` (v2.15.0) | 9 issues, exit 0, byte-identical distribution to the W4-03 measurement: errcheck 2, funlen 1 (`finalizeSupervisorHandoffLocked`), gocritic 1, gocyclo 2 (`Start` 26, `closeContainmentAttempt` 41), unparam 2, unused 1 |
| advisory lint (linux) | `GOOS=linux golangci-lint run --config .golangci.yml ./internal/execution/...` | 18 issues, exit 0; the 9 linux-only additions are gocyclo on 8 pre-existing functions (`validateLinuxV2Invocation` 53, `prepareGate` 46, `readLiveOwnerHelperStatus` 30, `stopAsMirror` 28, `promoteCloneAfterStop` 57, `finalizeNoPrepareAbort` 32, `witnessDropStatus` 26, `liveOwnerStatus` 60) plus 3 gocritic (`linux_owner_v2_linux.go:614` ifElseChain) minus the linux-absent `unused`; **no finding names any function this slice adds or rewrites**, and the two functions it decomposes no longer appear (`finalize` CC 43 → 22, `finalizePreGate` CC 36 → 17, threshold 25) |

Acceptance-oracle note (contract E8): `linux-native-v2-acceptance` is `workflow_dispatch`-only
with zero dispatches and the v2 witness matrix is env-gated, so neither is an acceptance oracle
here; the fence evidence is the CI-run `internal/execution` characterization tests plus the
`internal/app` witness suites. The commit-1 tests do run in CI: the `daemon` job
(`.github/workflows/ci.yml`, ubuntu-latest) runs
`python3 ../scripts/ci/stdio-only-caller-boundary.py "$(command -v go)" test -race -count=1 -timeout 600s ./...`
at `:1530`, which builds and runs the `linux && amd64` test files, and the `daemon-windows` job
runs `go test -count=1 -timeout 600s ./...` at `:1834` for the cross-platform half
(`linux_v2_ack_stages_test.go`).

## 12. Residual limits and known drifts

- The race pass was not run locally (no cgo/race support on this host); CI's `daemon` race pass is
  the race oracle (same as the W4-03 slice).
- The full WSL `go test -count=1 ./internal/app/...` gate does not complete on this host. The
  slice worktree's full run hits the 600 s panic timeout inside
  `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` (subtest
  `mixed_null_marker_survives_replay_and_restart`), whose goroutine dump shows an HTTP transport
  dial (`net/http.(*Transport).startDialConnForLocked`); that single test then also times out in
  isolation; skipping it moves the timeout to
  `TestNativeResumeAdmissionRejectsIncompatibleOrUnavailableRetainedSessionBeforeNativeStart`.
  The untouched base worktree (`Q:/repos/wt-w4-03-runner-decomposition/symmetry` at `376ff525`)
  behaves the same way: its full app suite times out at the same
  `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`, its isolated run of that test
  times out too, and its suite with that test skipped times out at a third test,
  `TestExplicitZeroRetryAfterUsesMinimumRetryFloor` (goroutine dump in `net/http/httptest`
  `server.go:584`). The last hanging test differs between the two revisions and moves when one is
  skipped, so the hang is nondeterministic and environmental (HTTP dial/httptest in this WSL
  distro), not produced by this slice, and it sits in admission/httptest code, not in the close
  pipeline or the ack vocabulary. Linux coverage of the changed surfaces is recorded instead as:
  the WSL witness subset (`-run 'TestLinuxV2NativeWitness' ./internal/app/` → `ok`, 0.739 s) and
  the WSL execution package (§11).
- The dispatch-only v2 witness jobs were not dispatched; §10 is the reviewer surface.
- The `promoteCloneAfterStop` internals and the leaf/parent removal calls are not executed by the
  new CI tests (they need a live helper/cgroup); the tests preset the removed-leaf capability and
  the retired anchor, which is the persisted-state branch of the same steps, and those bodies are
  byte-identical (§4, §5).
- `docs/TODO.md` keeps a deferred rf-w2-16 finding that cites
  `daemon/internal/execution/linux_owner_v2_linux.go:1338` and `:1360`; the cited constructs moved
  to `:1351` (the mirror `GateExit` validation) and `:1375` (the mirror stop-evidence literal start)
  under this slice. The entry is not closed by this change and `docs/TODO.md` is outside the
  contract's owned set, so it was left untouched; a maintenance task should re-anchor the two
  citations.
