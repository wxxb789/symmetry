# rf-w4-01 goal-admission transaction — evidence record (2026-11-22)

Contract: `goal-rf-w4-01-goal-admission-split.md` (GDD), binding with its `## Conductor decision`
sections. Plan: `docs/plans/2026-11-22-rf-w4-01-goal-admission-tx-plan.md`. Base revision for every
base coordinate: `b25e34ae` (the dispatch HEAD). Shape reference: `docs/evidence/rf-w4-17b-run-lifecycle-2026-11-22.md`.

## 1. Deliverable shape (files, counts)

| file | change | measure |
|---|---|---|
| `daemon/internal/app/goal_admission_tx.go` | new | 1,460 L: transaction object, 67 methods + the constructor, compensation stack, phases |
| `daemon/internal/app/goal_admission.go` | `startGoalAdmission` region replaced by the thin entry | 1,593 -> 612 lines; entry at `:249-255` (7 lines); `abandonGoalSession` and everything from `:256` code-identical (checked below) |
| `daemon/internal/app/goal_admission_tx_test.go` | new | 319 L, 6 seam tests (2 order/join, 4 driven through `tx.run`); each has a red mutant (§7) |
| `scripts/ci/omo-control-native-evidence.py` | `SOURCE_PATHS` line only (R9) | +`daemon/internal/app/goal_admission_tx.go` |
| `docs/design/harness.md` | citation-only (O23) | `goal_admission.go:263` -> `:258`; -> `:249` at the apply revision (post-merge #223 comment trims) |
| `docs/plans/…-rf-w4-01-goal-admission-tx-plan.md` | new plan + delivered-revision section | |
| `docs/TODO.md` | unchanged | no slice TODO entry existed; none closed |

`app.go` is **untouched** (`git diff --stat` shows no `app.go` entry): the entry keeps its
signature and the caller at `app.go:1443` compiles unchanged.

Byte-identity of the untouched region: the base file from line 1223 on (371 lines, starting at
`attachPartialNativeSession`) is code-identical to the delivered file from line 256 on (357 lines;
`diff` empty after stripping comment and blank lines — the 14-line difference is the comment trims
that #223 brought in on main). The head region differs by those same trims plus five imports that
`goimports` removed because only the moved code used them (`reflect`, `execution`, `harness/omo`,
`harness/omp`, `harness/pi`); the entry moved `:263` -> `:258` (imports) -> `:249` (the merge).

### 1.1 Native-evidence source mapping (arch-decision-2 R9)

`daemon/internal/app/goal_admission_tx.go` is appended to `SOURCE_PATHS` of
`scripts/ci/omo-control-native-evidence.py`, so the native-evidence identity set covers the file
this slice's behaviour moved into. Moved declarations → their listed base home:

| moved declaration (now in `goal_admission_tx.go`) | base home (listed source path) |
|---|---|
| `goalAdmissionTx` + `newGoalAdmissionTx` + `abort` | `goal_admission.go:263-307` (the subscriber locals + the three anonymous defers) |
| the five compensation entry methods | `goal_admission.go:272-274`, `:277-288`, `:289-300`, `:301-306`, `:442-450` |
| the phase methods (plan seam rows 1-61) | `goal_admission.go:308-1222` |
| the former closures (`abortFreshLaunchIfCancelled`, `queueNativeEventSink`, `persistNativeProcess`, `persistNativeProcessAuthority`, `settlePublishedCancellation`, `settleExpiredNativeStartFailure`) | `goal_admission.go:462-476`, `:665`, `:817`, `:824`, `:849`, `:862` |
| `startGoalAdmission` (entry) | stays at `goal_admission.go` (listed source path) |

Evidence tooling (all outside the repository, `<HOST-PATH>/.local/state/gdd/scratch/w4-01/`):
`measure` (go/ast: per-function lines/statements/gocyclo, `flow` control-flow points, `seq` call
paths, `probe` range metrics), `audit/flow_audit.py` (seam alignment, closure rows, call
multiset), `audit/classify2.py` (transformation classes), `audit/guards2.py` (dropped
error/settle signal audit). The scanner is calibrated against the pinned linter at base: 8/8
gocyclo values (277, 38, 33, 28, 33, 141, 119, 117) and 2/2 funlen statement counts (69, 62)
reproduced exactly.

## 2. Per-method bound (clause: funlen 120/60, gocyclo 25)

Scanner over the delivered tx file plus the entry: **67 tx methods + `newGoalAdmissionTx`** (68
`func` declarations in the tx file) plus the `startGoalAdmission` entry;
max gocyclo **19** (`startGoalNativeSession` 40/29/19, `attachPublishedNativeSession` 34/23/19),
mean 6.1; max 42 lines (`launchNativeProcess`); max 29 statements; **0 over any bound**
(`l/s/c` = lines / funlen statements / gocyclo). Entry: 7 lines, 4 statements, gocyclo 1.
The phase methods: `validateGoalAdmissionInputs` 22/15/9, `prepareGoalSessionForLaunch` 19/12/8,
`startGoalNativeSession` 40/29/19, `attachGoalNativeSession` 15/12/5 + `openGoalNativeSession`
36/28/18 (the F2 attach+open phase split at the `operationCancel` seam),
`openGoalTurnAndSettleTerminalOwnership` 5/4/3 + `attachPublishedNativeSession` 34/23/19 +
`runGoalTurn` 17/12/9 (the turn phase split once the `err != nil || settled` guards pushed the
single method to 28 gocyclo, over the bound).

Merged-config finding set (`GOOS=linux golangci-lint run --config .golangci.yml
--max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...`):

| revision | all findings | funlen+gocyclo | delta |
|---|---:|---:|---|
| base `b25e34ae` | 147 | 66 (64 gocyclo + 2 funlen) | — |
| head | 146 | 65 | `-1`: the `startGoalAdmission` gocyclo 277 finding is gone; the normalised per-linter diff (message + file) adds **no** finding — the only head-only row is base's own `gocritic ifElseChain`, moved with its code into the tx file; `unparam` 19/19 and `errorlint` 6/6 unchanged (§8 lists every linter) |
| apply revision (`a5d9bd88` + the accepted simplify findings) | 147 | 65 (63 gocyclo + 2 funlen) | the 277 row stays resolved; the +1 is one post-merge `whitespace` finding at `process_persistence_test.go:775` (brought in on main by #223, outside this slice); the normalised diff adds no other row |

## 3. Seam table (base range -> HEAD method, measured)

`l/s/c` = lines / funlen statements / gocyclo of the base range (`probe`) and of the delivered
method (`funcs`); every delivered value is inside 120/60/25.

| base range | HEAD method(s) | base l/s/c | head l/s/c |
|---|---|---|---|
| 263-307 (locals + 3 defers) | `newGoalAdmissionTx` + `abort` + 5 entry methods | n/a (composite) | 13/7/1 + 13/5/4 |
| 308-340 | `validateAdmissionEnvelope` | 33/22/10 | 35/24/10 |
| 341-358 | `validateConfiguredHarness` | 18/13/6 | 23/18/6 |
| 364-383 | `validateAdapterInvocationArgs` | 20/12/9 | 21/13/9 |
| 359-363 + 384-403 | `acquireNativeLaunchPhase` | 5/1/1 + 20/12/9 | 28/19/10 |
| 404-425 | `resolveGoalAdapter` | 22/16/8 | 26/20/8 |
| 426-435 | `buildNativeEnvironment` | 10/7/4 | 12/9/4 |
| 436-439 | `prepareProviderBridge` | 4/3/2 | 9/7/3 |
| 477-553 | `prepareGoalSessionForLaunch` + `prepareRetainedGoalSessionForLaunch` | n/a (composite) | 17/11/7 + 11/8/4 |
| 479-495 | `recoverRetainedGoalSession` | 17/13/5 | 23/18/6 |
| 497-507 | `markRetainedWorkspaceRecovery` | 11/6/4 | 12/7/4 |
| 509-521 | `queueRetainedSessionAttach` | 13/3/2 | 13/4/2 |
| 522-552 | `rebindRetainedGoalSession` | 31/15/16 | 32/16/16 |
| 554-638 | `prepareFreshGoalSessionForLaunch` | 85/54/23 | 30/19/13 |
| 554-558 / 559-561 / 630-638 | (inline: owner recheck, recovery-required write, two abort checks) | 5/3/3 + 3/2/2 + 2x3/2/2 | — |
| 563-577 | `prepareLinuxCloneWorkspace` | 15/11/5 | 21/16/6 |
| 579-607 | `prepareGoalSubjectWorkspace` | 29/21/9 | 30/22/9 |
| 609-629 | `recordGoalSessionLaunchIntent` | 21/10/4 | 23/12/4 |
| 640-642 | (inline in `prepareGoalSessionForLaunch`; `publishPreparedWorkspace` inlined at the apply revision) | 3/2/2 | 3/2/2 |
| 643-661 | `queueGoalSessionAttach` | 19/7/5 | 21/9/5 |
| 662-664 | (inline abort check) | 3/2/2 | — |
| 665-667 | `queueNativeEventSink` (former `sink` closure) | 3/1/1 | 1/1/1 |
| 668-669 / 670-680 / 681-683 | (capture) + `revalidateStartLease` + (inline abort check) | 2/2/1 + 11/8/5 + 3/2/2 | 12/9/5 |
| 684-699 + 700-756 | `buildNativeInvocation` + `requireLinuxV2CloneGrant` + `applyLinuxV2Invocation` + `attachLinuxCloneGrant` | n/a (composite) | 26/8/4 |
| 701-708 | `requireLinuxV2CloneGrant` | 8/6/5 | 9/7/5 |
| 709-740 | `applyLinuxV2Invocation` | 32/24/14 | 34/26/14 |
| 741-755 | `attachLinuxCloneGrant` | 15/12/6 | 16/13/6 |
| 757-771 | `publishRetainedNativeStartBoundary` | 15/8/4 | 15/10/4 |
| 772-780 | `armNativeLeaseSequence` | 9/5/3 | 10/6/3 |
| 781-799 | `acquireNativeStartOwnership` | 19/14/7 | 20/15/7 |
| 800-801 | `handOffLaunchToNativeOwner` | 2/2/1 | 6/4/3 |
| 802-848 | `launchNativeProcess` + `persistNativeProcess` + `persistNativeProcessAuthority` | 47/13/10 | 42/16/8 |
| 850-860 | `settlePublishedCancellation` (former closure) | 11/8/5 | 11/8/5 |
| 863-895 | `settleExpiredNativeStartFailure` (former closure) | 33/25/10 | 33/25/10 |
| 897-917 | `settleStartCancelledAfterLaunch` (renamed at the apply revision) + `settleCancelledProcessStart` + (inline expired window) | n/a (composite) | 8/6/4 |
| 918-958 | `failNativeStart` | 41/22/12 | 35/21/11 |
| 959-975 | `settleLeaseExpiredProcessStart` | 17/13/6 | 19/15/6 |
| 976-987 | `requireStagedSession` | 12/10/3 | 14/12/3 |
| 988-1001 | `attachGoalNativeSession` | 14/11/5 | 15/12/5 |
| 1003-1036 | `openGoalNativeSession` | 34/26/18 | 36/28/18 |
| 1041-1222 | `openGoalTurnAndSettleTerminalOwnership` + `attachPublishedNativeSession` + `runGoalTurn` + 14 sub-methods | n/a (composite) | 5/4/3 + 34/23/19 + 17/12/9 |
| 462-476 | `abortFreshLaunchIfCancelled` (former closure) | 15/11/5 | 13/10/5 |
| 272-274 / 277-288 / 289-300 / 301-306 / 442-450 | the five compensation entry methods | 3/2/2, 12/9/5, 12/9/5, 6/4/4, 9/7/4 | 4/3/2, 11/9/3, 11/9/3, 7/5/3, 7/6/2 |

Range coverage is monotone and non-overlapping over `263-1222`; rows that name "inline" are base
statements that stayed in their phase method (the plan's rows 13/14/18/21/41/61 record the same
choice). `openGoalTurnAndSettleTerminalOwnership`'s sub-methods are
`settleCancelledBeforeTurnStart`, `settleLeaseExpiredAttachedStart`, `persistNativeSessionHandle`,
`markNativeSessionAttached`, `loadNativeRunJournal`, `deliverNativeSessionAttach`,
`fetchGoalRunContextJSON`, `markNativeRunRunning`, `settleCancelledAttachedStart`,
`abandonCancelledOperationContext`, `settleLeaseExpiredTurn`,
`settleObservedTerminalCancellation`, `settleCancelledTurnStart`, `settleNativeTurnFailure`
(measured shapes in §2's scanner run, all <= bounds).

## 4. Compensation stack (base defers -> ordered entries)

Base: three anonymous defers (registration order `:271`, `:276`, `:441`) execute LIFO —
bridge close, clone abort, reuse retire, unknown-outcome wrap, launch-phase release. HEAD: one
`abort(ctx, returnErr)` in the entry, iterating five entries in exactly that order (`abort`'s
slice literal), each `armed` at the point its base handle became live and `released` where base set
a transfer flag:

| base defer / body | HEAD entry | armed (base) | released (base) |
|---|---|---|---|
| `:442-450` (defer 3) | `bridgeCompensation` | `:436` `providerBridge != nil` | `:835-837` `session != nil` |
| `:277-288` (defer 2, clone) | `cloneCompensation` | `:563` `linuxClone != nil` | `:800` `linuxClone != nil` |
| `:289-300` (defer 2, reuse) | `reuseCompensation` | `:489` (`bindLinuxCloneReuse` succeeded) | `:801` `linuxCloneReuse != nil` |
| `:301-306` (defer 2, wrap) | `nativeOutcomeCompensation` | `:829` `nativeLaunchAttempted` | never (condition, not a transfer) |
| `:272-274` (defer 1) | `launchPhaseCompensation` | `:387` release func non-nil | `:846-847` confirm path released it |

The three former transfer locals `providerBridgeTransferred`, `cloneLaunchStarted`,
`reuseLaunchStarted` and the condition local `nativeLaunchAttempted` are represented by the
entries' `released`/`armed` markers (plan D3); no other base local changed representation.

Per-exit-path participation (which entries run at HEAD, same order):

| exit path class | base defers that ran | HEAD entries that run |
|---|---|---|
| pre-acquisition failures (`:308-371`) | none | none |
| after launch-phase acquire, before bridge (`:394-431`) | release | launch-phase |
| after bridge prep, before clone/reuse (`:439-476`) | bridge close, release | bridge, launch-phase |
| fresh clone / retained reuse live, before handover (`:563-798`) | bridge close, clone or reuse abort, release | bridge, clone or reuse, launch-phase |
| native start attempted (`:829-`), any later exit | previous + outcome wrap | previous + native-outcome |
| after handover with a returned session (`:835-`) | bridge skipped, clone/reuse skipped, wrap, release | bridge skipped, clone/reuse skipped, native-outcome, launch-phase |
| confirm path (`:846-847`) | release skipped inline | launch-phase released |
| success (final `return nil`) | release (or released inline), wraps skipped | same |

`abort` runs the armed-and-unreleased entries in the fixed order and threads `returnErr` through
them, so `errors.Join` argument order is the base order (`settle` test
`TestGoalAdmissionTxCompensationErrorsJoinInExecutionOrder` pins the order textually).

## 5. Durable writes, calls, locks

- **Call multiset, base main body vs HEAD phase concatenation**: base 414 calls, HEAD 469 calls;
  **missing_terms = 0, missing_instances = 0** — every `daemon.store.*`, `daemon.control`/`goalAPI`,
  `adapter.Start`/`staged.Open`/`staged.StartTurn`, `fmt.Errorf`, `errors.Join` call of the base
  region appears the same number of times at HEAD. The 55 added terms (55 instances) are all
  `*goalAdmissionTx` method calls (dispatch/phase/closure-method calls), i.e. no new external call.
- **Durable-write order** persist -> commit -> lease-arm -> resume -> input: unchanged, per path —
  `daemon.store.*` sequence base vs HEAD differs only by the phase dispatch (the call multiset has
  zero missing store calls; the ordered `seq` diff lists the HEAD-only phase calls by name).
- **Locks**: five `daemon.mu.Lock()` sites in the base region (`:773` lease-sequence arm, `:1132`
  running publication, `:1162` lease-expired turn, `:1191`/`:1195` terminal-observed cancellation)
  and five at HEAD (`armNativeLeaseSequence:868`, `markNativeRunRunning:1365`,
  `settleLeaseExpiredTurn:1408`, `settleObservedTerminalCancellation:1439`/`:1445`), same order,
  same critical sections' content. No other mutex appears (0 occurrences of
  `eventReceiptMu`/`commandReceiptMu`/`inputMu` in the tx file, as at base).
- **Goroutines/channels/timers**: none added; the only concurrency objects in the moved code are
  the two `daemon.mu` critical sections above and the `daemon.running` map accesses inside them.

## 6. Control-flow audit (lesson 32)

Base main body (263-1222, closure bodies excluded by the scanner, as at base): **322** flow points.
HEAD: the 56-method execution-order concatenation (phases, sub-methods, spines, `run`), closure
methods excluded: **473** flow points. Alignment (`audit/align-apply.txt` at the apply revision;
the pre-apply run measured 114/265): `base-only = 115`, `head-only = 266` — the +1/+1 is the apply
revision's `publishPreparedWorkspace` inline, which moved the `if workspacePersisted != nil` term into `prepareGoalSessionForLaunch` at
its true execution position (the flow totals 322/473 are unchanged); every difference belongs to
one of six transformation classes, verified by
relaxed-multiset comparison (`audit/flow-classify-v2.txt`): unexplained terms = 15 (21
instances), each mapped by inspection to a class below; addition classes: dispatch and moved guard
forms 15 terms / 60 instances, phase return adaptations 19 / 63, settled returns 1 / 25,
fall-through markers 1 / 24 (all enumerated in the artifact).

| # | transformation class | count | example |
|---|---|---:|---|
| T1 | settle-signal adapter: base `return nil` (settled) -> `return true, nil` / `return settled, nil`; base implicit fall-through -> `return false, nil` | 25 + 24 | `startGoalNativeSession:712` |
| T2 | guard inversion for the false-return convention (`if X { ... }` -> `if !X { return false, nil }; ...`) | 15 terms (with T4) | `settleCancelledProcessStart:1032` (De Morgan of base `:906`) |
| T3 | phase dispatch: `if err := tx.<phase>(ctx); err != nil \|\| settled { return err }` | 60 instances | `run:121-141` |
| T4 | return adaptation for the settled/error signatures (`return X` -> `return false, X`, `return X` -> `return nil, false, X`) | 63 instances | `fetchGoalRunContextJSON:1347` |
| T5 | entry guard replacement: the three defer-body guards (`linuxClone != nil && !cloneLaunchStarted`, `providerBridge == nil \|\| providerBridgeTransferred`, `releaseLinuxV2LaunchPhase != nil`) moved to the entries' `armed`/`released` markers; entries return `returnErr` | 5 entries | `abortPreCheckoutLinuxClone` |
| T6 | bool-only settle method: `settleObservedTerminalCancellation` returns `bool` (base's block has no error path), so its call site is `if tx.settleObservedTerminalCancellation() { return nil }` — base's settled `return nil` is preserved, and there is no error left to drop | 1 site | `runGoalTurn:1230` |

The 15 unexplained terms are each an instance of T1/T2/T4 with the scanner's polarity
canonicalization not applying (e.g. base `if ctx.Err() != nil` vs HEAD `if ctx.Err() == nil`; base
`if linuxClone == nil && linuxCloneReuse == nil` vs HEAD `if linuxClone != nil || linuxCloneReuse != nil`;
the six base `if settled {` short-circuits converted into the helpers' returned `settled` values);
the list is `audit/flow-classify-v2.txt`.

Former-closure rows (`audit/closures-apply.txt`; the rows are identical to the implement run): `settlePublishedCancellation` and
`settleExpiredNativeStartFailure` map **IDENTICAL** (7 and 12 flow points); the four entry-guard
rows differ by exactly the guard replacement (T5) plus `return returnErr`; `abortFreshLaunchIfCancelled`
differs by the `(bool, error)` returns and the tx field substitutions, with the same three
conditions and the same error text.

**Settled-signal audit (the defect class the Windows suite found).** Two delivered-revision defects
were both dropped settle/error signals, fixed before the final revision:

| # | defect | how it was found | fix |
|---|---|---|---|
| D-a | `startGoalNativeSession`/`attachGoalNativeSession`/`openGoalNativeSession` returned a nil error for a settled cancel, so `run` continued into the next phase with `staged == nil` | Windows suite panic `goal_admission_tx.go:1136` (first revision) | the three phases and `markNativeSessionAttached`/`deliverNativeSessionAttach`/`markNativeRunRunning`/`fetchGoalRunContextJSON` now return `(settled bool, err error)`; `run` checks `err != nil \|\| settled` |
| D-b | the `openGoalTurnAndSettleTerminalOwnership` sub-method call sites checked only `settled`, dropping `(false, err)` results | Windows suite panic `goal_admission_tx.go:1330` (second revision, `tx.receipt` nil) | every sub-method call site checks `err != nil \|\| settled` |

The mechanical re-check is `audit/guards2.py`: 12 methods whose body returns
`(false, <non-nil error>)`, 63 call sites of error-returning tx methods, **0 violations** (every
risky call site guards with `err != nil || settled|handled|cancelled`, every settle-only call site
guards with its settled signal and returns the result). Residual limit: the audit is
shape-based (regex over the delivered file), not a type-level guarantee.

## 7. Red mutants

Each mutant applied alone to a copy of `goal_admission_tx.go` through `go test -overlay` (the
overlay maps the module path to a mutant file outside the repository, so no repository file is
modified and nothing needs reverting), the owning test run, then the same command without the
overlay to show green. Mutant files and outputs: `<HOST-PATH>/.local/state/gdd/scratch/w4-01/mutants/`.
Control: `go test -count=1 -run '^TestGoalAdmissionTx' ./internal/app/` without an overlay —
`ok github.com/wxxb789/symmetry/daemon/internal/app 0.980s`, exit 0.

| # | test | mutant | observed failure (exit 1) |
|---|---|---|---|
| M1 | `…CompensationOrderMatchesTheBaseDeferOrder` | swap the first two entries in `abort`'s slice literal | `:92: compensation order = "clone-refusal,provider-bridge,clone-reuse-refusal,native-start-outcome,linux-v2-launch-phase", want "provider-bridge,clone-refusal,…"` |
| M2 | same | drop `!entry.released` from `abort`'s skip condition | `:102: compensation order after transfers = "provider-bridge,clone-refusal,clone-reuse-refusal,…", want "clone-reuse-refusal,native-start-outcome,linux-v2-launch-phase"` |
| M3 | `…CompensationErrorsJoinInExecutionOrder` | move the bridge entry behind the launch-phase entry | `:139: compensation join order = [… "compensation linux-v2-launch-phase failed" "compensation provider-bridge failed"], want [… "compensation linux-v2-launch-phase failed"]` |
| M4 | `…RunsOnlyTheOutcomeCompensationAfterNativeHandover` | drop `tx.bridgeCompensation.released = true` in `launchNativeProcess` | `:277: the session handover did not release the provider-bridge compensation` |
| M5 | same | drop `tx.nativeOutcomeCompensation.armed = true` in `launchNativeProcess` | `:274: the native start attempt did not arm the outcome compensation` |
| M6 | `…KeepsKnownPreNativeFailureUnwrapped` | arm the outcome entry in `newGoalAdmissionTx` | `:237: a pre-native failure armed the native-start outcome compensation` |
| M7 | `…SettlesFreshCancellationWithoutCompensation` | drop the `abortGoalSessionLaunchBeforeNativeStart` call from `abortFreshLaunchIfCancelled` | `:180: cancelled fresh launch compensation = []state.GoalSessionJournal{{…LocalHandleID:"0ac92bbe-…"…}}` (the session never closed) |
| M8 | `…AbortsQueuedAttachBeforeNativeStart` | drop the `discardUnreadyGoalSessionAttachDelivery` call | `:196: cancelled launch compensation = abort 1, discard 0; want 1 and 1` |

M7's first candidate — return the unsettled signal (`return false, err`) at the pre-native abort
check — **survived** the test: it is behaviourally equivalent in this path, because the following
phase-level abort check re-detects the same cancellation and settles identically, and the mutation
returns before the launch-started write. The candidate was replaced by M7 above; while
investigating, the test gained the launch-started-marker assertion (a cancelled pre-native launch
must publish no `"launch_state":"launching"` write), and the test now flips the cancellation during
the launch-intent write (`SetAtomicWriterForTesting`) so the pre-native abort check — not the
workspace short-circuit — is the settling site.

## 8. Gates

| gate | command (cwd) | result |
|---|---|---|
| gofmt | `gofmt -l internal/app/` (`daemon/`) | no output, exit 0 |
| vet | `go vet ./...` (`daemon/`) | exit 0 |
| build windows | `GOOS=windows go build ./...` (`daemon/`) | exit 0 |
| build linux | `GOOS=linux go build ./...` (`daemon/`) | exit 0 |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` (`daemon/`) | exit 0 |
| whitespace | `git diff --check` (repo root) | exit 0 |
| lint primary (before) | `cd daemon && GOOS=linux golangci-lint run --config .golangci.yml --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...` (pinned 2.15.0) at `b25e34ae` | 147 findings: 64 gocyclo, 22 gocritic, 19 unparam, 15 staticcheck, 9 exhaustive, 6 errorlint, 4 nilerr, 3 ineffassign, 2 funlen, 2 copyloopvar, 1 unconvert; exit 1 (findings exist); `<HOST-PATH>/…/scratch/w4-01/repo-scratch/lint-base-primary.txt` |
| lint primary (after) | same at the frozen revision | 146 findings; normalised per-linter diff vs base (message + file, line numbers ignored): only-in-head = 1 `gocritic ifElseChain` in the tx file, which is base's identical finding **moved** with its code; only-in-base = that finding plus the resolved `goal_admission.go` gocyclo 277. Per linter: gocyclo 64→63, gocritic 22→22 (moved), unparam 19→19, errorlint 6→6, staticcheck 15→15, exhaustive 9→9, nilerr 4→4, ineffassign 3→3, funlen 2→2, copyloopvar 2→2, unconvert 1→1. **No new production finding**, exit 1 as at base; `<HOST-PATH>/…/scratch/w4-01/final-lint-primary.txt` |
| lint secondary | `GOOS=linux golangci-lint run --no-config --default=none --enable=funlen,gocyclo,ineffassign --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...` | 0 findings in the new files (default 60/40/25 limits), exit 1 (other pre-existing files have findings); `<HOST-PATH>/…/scratch/w4-01/final-lint-secondary.txt` |
| per-method bound | calibrated scanner (`<HOST-PATH>/…/scratch/w4-01/measure`) | 68 tx methods + constructor + entry; max 19 gocyclo / 42 lines / 29 statements, 0 over bound |
| focused (contract's six) | `go test -count=1 ./internal/app/ -run 'TestStartGoalAdmissionCompletesArtifactOnlyValidationWithoutNativeLaunch\|TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure\|TestFreshCodexNativeStartFailureReportsUncertainCompensationFailure\|TestFreshCodexPreNativeLaunchWriteFailuresReadBackAndCompensate\|TestNativeHandlePersistenceAndDiscardFailureStillClosesOwner\|TestLeaseOwnedNativeStartErrorsCleanupAndAcknowledgeBeforeReturn'` | `ok …`, exit 0 |
| focused (new seam tests) | `go test -count=1 -v -run '^TestGoalAdmissionTx' ./internal/app/` | `ok … 1.096s`, exit 0, 6 tests pass (order, join order, fresh cancellation, queued-attach abort, pre-native unwrapped, post-handover outcome) |
| full package suite (Windows) | `go test -count=1 -timeout 20m ./internal/app/...` (`daemon/`) | `ok github.com/wxxb789/symmetry/daemon/internal/app 189.154s` (`real 3m11.6s`), exit 0 — **the frozen revision**, after the review-driven edits; the same suite on earlier revisions caught the two settle-signal defects (§6). Earlier full runs: `280.707s` exit 0 (pre-review revision), log `<HOST-PATH>/…/scratch/w4-01/final-app-suite.txt` |
| `-race` (Windows) | `go test -count=1 -race ./internal/app/...` | refused: `-race requires cgo` on this host, as recorded at W4-17a/W4-17b |
| WSL race oracle (full package) | `wsl -d Ubuntu-24.04 -u accept` -> `cd /mnt/q/repos/wt-w4-01-goal-admission-split/symmetry/daemon && go test -count=1 -race -timeout 30m -v ./internal/app/...` | **no result**: the first attempt (no `-v`, `-timeout 25m`) produced nothing in 95 minutes (the `-race` build of the full test binary is itself long under WSL) and was cancelled with an empty log. The `-v` retry streamed **18 passing tests, 0 failures, 0 `DATA RACE`** and then stopped progressing for 10+ minutes at `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart/mixed_null_marker_survives_replay_and_restart` — the exact W4-17a/W4-17b-documented pre-existing WSL loopback-`httptest` stall. Log `<HOST-PATH>/…/scratch/w4-01/wsl-race-full-v2.txt` (69 lines, mtime frozen); the same test passes in the Windows suite (exit 0, 189.154s) and exercises no goal-admission path. |
| WSL race oracle (touched path) | `wsl -d Ubuntu-24.04 -u accept` -> `cd /mnt/q/repos/wt-w4-01-goal-admission-split/symmetry/daemon && go test -count=1 -race -timeout 12m -v -run 'TestGoalAdmissionTx\|TestStartGoalAdmissionCompletesArtifactOnly\|TestFreshCodex\|TestNativeHandlePersistenceAndDiscardFailureStillClosesOwner\|TestLeaseOwnedNativeStartErrorsCleanupAndAcknowledgeBeforeReturn' ./internal/app/` | **PASS**: 22 tests + 7 subtests, including all six seam tests and the fresh-Codex native-settlement family; `ok github.com/wxxb789/symmetry/daemon/internal/app 6.705s`, `real 1m4.8s`, exit 0, no `DATA RACE` line (go1.27.0 linux/amd64) |
| W4-17b write-site scan | `TestRunLifecycleWritesStayInTheModule` (part of the suite) | pass: 0 lifecycle writes outside `run_lifecycle.go`; the tx writes no lifecycle field |
| control-flow audit | `<HOST-PATH>/…/audit/flow_audit.py` (`align`, `closures`) + `classify2.py` | base 322 / head 473 points; `base-only=114`, `head-only=265`; every difference in T1-T6; `audit/align-final.txt`, `audit/closures.txt`, `audit/flow-classify-v2-final.txt` |
| compensation order | `abort`'s fixed entry order + `TestGoalAdmissionTx…Order…` + M1/M2/M3 | base LIFO order bridge -> clone -> reuse -> wrap -> release preserved |
| durable writes / calls | `audit/flow_audit.py seq` + multiset check | base 414 calls / head 469; `missing_terms = 0`; added terms are tx methods only |
| lock sites | `grep -c 'mu.Lock()'` both revisions | 5 <-> 5, same order, no other mutex |
| settled/error propagation | `audit/guards2.py` | 12 risky methods, 63 call sites, 0 violations |
| file sizes | `wc -l` | `goal_admission.go` 635, `goal_admission_tx.go` 1,473, `goal_admission_tx_test.go` 313 — all <= 1,500 |
| §11 citation gate | `python scripts/ci/check-citation-existence.py --base b25e34ae --head HEAD` | exit 0 (`no audited document added or modified …; nothing to check` — the deliverable is uncommitted, so the audited-corpus diff is empty; `harness.md` is not in the audited corpus and the gate never validates line suffixes) |
| R9 native-evidence gate | `python scripts/ci/test-omo-control-native-evidence.py` | `Ran 10 tests … OK`, exit 0 |

### Independent read-only review (2026-11-22, before completion)

One independent reviewer (Sol, read-only, no commands, 6m56s) audited the frozen tree against the
base revision and the contract: compensation registration/release points for every exit class,
the settle/error propagation of all 28 `(bool, error)` methods and 33 call sites, lock and
durable-write order, the entry/caller contract, and the comment multiset. Result: **no P0/P1/P2
finding**; the two settled-signal fix classes were confirmed complete at every sub-method
boundary (not only the two known sites), and "the central claim — byte-identical observable
behaviour, only the function split — is supported by the source as delivered".

Two P3 findings were fixed in response:

1. the record claimed 292 L / 5 seam tests and 69 methods — corrected to 313 L / 6 tests /
   68 methods + constructor (the plan's numbers likewise);
2. `failNativeStart` carried an unused `ctx`, and the merged-config `unparam` run showed **seven**
   newly extracted phases with an unused `ctx` plus one unused test-fixture result (an
   `errorlint` finding additionally came from the test's `err != want` comparison). All were
   fixed — the phase methods take `_ context.Context` (the same pattern as
   `typeNativeStartOutcome`/`releaseNativeLaunchPhase`, keeping the dispatch shape the T3 audit
   reads), the fixture drops its unused result, and the assertion uses `errors.Is` plus
   `Error()`-text equality. The re-run below records the resulting per-linter diff.

### Apply-stage re-verification (2026-11-22, apply revision on top of `a5d9bd88`)

The gate table above was produced before the `origin/main` merge; the apply stage re-ran the
contract gates on the merged tree plus the accepted simplify findings (§1-§3 and §6 carry the
republished numbers). Commands are the rows above unless stated.

| gate | result |
|---|---|
| gofmt / `go vet ./...` / `git diff --check` | empty, exit 0 / exit 0 / exit 0 |
| builds: windows, linux, linux/arm64 | exit 0, exit 0, exit 0 |
| lint primary (merged config) | 147 findings, exit 1: gocyclo 63, gocritic 22, unparam 19, staticcheck 15, exhaustive 9, errorlint 6, nilerr 4, ineffassign 3, funlen 2, copyloopvar 2, unconvert 1, whitespace 1; normalised diff vs base = the resolved 277 row plus one added post-merge `whitespace` row (`process_persistence_test.go:775`, main's #223 — not this slice's file) |
| lint secondary (`--no-config --default=none --enable=funlen,gocyclo,ineffassign`) | exit 1; 0 findings in `goal_admission.go`, `goal_admission_tx.go` or the seam test (240 package-wide rows at the default 60/40/25 bounds) |
| per-method scanner | 67 tx methods + constructor: max 19 gocyclo / 42 lines / 29 statements, mean 6.1, 0 over bound; entry 7 lines, 4 statements, 1 gocyclo |
| focused tests (six contract + six seam + `TestRunLifecycleWritesStayInTheModule`) | exit 0, `ok … 2.096s`, 12 funcs + 7 subtests PASS; the W4-17b write-site scan is part of this run |
| full package suite (Windows) | exit 0, `ok … 225.655s` |
| `-race` (Windows) | exit 2, refused (`-race requires cgo`), as recorded |
| WSL race oracle (touched path, the same `-run` set as the pre-merge run) | exit 0, 22 PASS / 0 FAIL / 0 `DATA RACE`, `ok … 5.678s` |
| R9 native-evidence gate | exit 0, `Ran 10 tests … OK` |
| §11 citation gate | exit 0 (`no audited document added or modified …; nothing to check` — the work is uncommitted) |
| audits re-run (`flow` / classifiers / `guards2.py`) | align 322/473 points, base-only 115 / head-only 266, unexplained 15 (21 instances); guards 12 methods / 63 call sites / 0 violations |
| red mutants rebuilt from the apply file (M1-M8, `-overlay`) | control exit 0; M1-M8 each exit 1 with the §7 failure classes (seam-test line numbers shifted by the probe edit) |

The diff carries no user-profile path and no backslash-u sequence (2 scans, 0 hits). The full WSL
race oracle (all tests) was not re-attempted: the pre-existing stall at
`TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` recorded above blocks it; the
touched-path run is the race evidence.

## 9. Scoped simplification review (repo AGENTS.md, before completion)

Candidates from the delivered diff, each with the ablation tried or the reason it was retained:

1. **The five-entry compensation chain** (`armed`/`released` markers + `abort`'s ordered slice
   literal) — retained. The ablation (a plain LIFO by acquisition time) changes the base execution
   order for the bridge-vs-clone pair and therefore the `errors.Join` order; a single boolean
   "compensate" flag loses the four distinct lifetimes. The chain is what the contract asks the tx
   to own; the deletion test fails (removing it re-spreads the guards into the phases).
2. **The `(settled bool, err error)` convention** — retained. The alternative (a tx `settled`
   field that `run` inspects once) is ~10 lines shorter but hides the base `return nil` sites
   inside state instead of the call graph, which is exactly what the lesson-32 audit and the
   `guards2.py` check read; with the delivered form every base settled return is a visible
   `return` in its phase.
3. **`applyLinuxV2Invocation`'s four near-identical error funnels** (`:712-720`, `:723-731`,
   `:734-739`, `:745-750`) — retained undeduplicated (plan D5). A shared
   `compensatePreNativeInvocationBuild(ctx, buildErr)` helper was written and then removed during
   implementation: it is provably equivalent for three of the four funnels, but the fourth has no
   `resume` branch, and the slice's clause-3 freeze on join order makes a silent collapse a
   failure mode; dedup belongs to a later slice that can add its own mutants.
4. **`typeNativeStartOutcome(_ context.Context, ...)` and `releaseNativeLaunchPhase(_ ...)`** —
   retained blank parameters. Per-entry signatures would need a type switch in `abort`; the
   uniform `func(context.Context, error) error` keeps one loop and the order auditable.
5. **`linuxV2Environment`** — the field kept the base local's name rather than a shorter
   `linuxV2Env` (ablation tried and reverted for audit fidelity; the plan's field map names it).
6. **No diagnostic trace field on the tx** — the seam tests wrap `entry.run` from the same package
   instead, so no production-only test state exists; `go vet`/`unused` are clean and no `_ = x`
   remains.
7. **`requireStagedSession`/`persistNativeSessionHandle`/`loadNativeRunJournal`** stayed separate
   rather than inlined into their single call sites: each carries its own base error join, and
   inlining would push their callers over the gocyclo bound.

No dead code was introduced: every tx field is read (the two entry-run parameters that some entries
ignore are blank by design), every method has a caller, and the removed region left no unreachable
statements.

## 10. Known limits

- The flow/call audits are shape-level (go/ast + regex) with the closure bodies compared per
  method, not per call site; they prove the statement/call moves, not dynamic equivalence. The
  dynamic oracle is the existing suites plus the new seam tests.
- The clone/reuse compensation entries are exercised dynamically only by the Linux v2 suites
  (WSL); on Windows they are proven by the arming/release mapping and the order/guards audits. A
  live Pi provider-bridge close (`bridgeCompensation`'s body) is likewise not driven on this host.
- The WSL `-race` oracle inherits the pre-existing loopback-`httptest` stall recorded at W4-17a and
  W4-17b for `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`. It was reproduced
  exactly: the `-v` full-package run passes 18 tests with no failure and no `DATA RACE`, then
  freezes at that test's `mixed_null_marker_survives_replay_and_restart` subtest (§8), so no
  full-suite green claim is made for Linux. That test is unchanged by this slice and exercises no
  goal-admission path (it passes in the Windows suite); the recorded Linux oracle is the
  touched-path `-race` run, 22 tests + 7 subtests green, which excludes it.
- One defect class (dropped settle/error signal) was found twice by the Windows suite rather than
  by the audits; `guards2.py` was written in response and is shape-based, so an unusual call shape
  could still slip through it.
- One settle-signal mutation is not observable at all: returning the unsettled signal at the
  pre-native abort check is masked, because the next phase-level abort check re-detects the same
  cancellation and settles identically (§7, M7's first candidate). No test therefore pins *which*
  of the two adjacent sites settles a fresh cancellation — only that the session ends closed,
  unstarted and fully compensated.
