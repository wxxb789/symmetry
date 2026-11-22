# rf-w4-01 goal-admission transaction plan — `goalAdmissionTx` owns its compensation

Contract: `goal-rf-w4-01-goal-admission-split.md` (GDD), binding with its `## Conductor decision`
sections (architecture reshape 2026-11-21, arch-decision-2 and its verification amendments, the
2026-11-22 comment policy) and the program-wide rules. Base revision for every coordinate below:
`b25e34ae` (the dispatch HEAD; the contract's `:263-1224` re-verified as `:263-1222` at this
revision). Shape reference: `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md` +
`docs/evidence/rf-w4-17b-run-lifecycle-2026-11-22.md` (same package, same wave, plan +
`docs/evidence/<slice>-<date>.md` record).

Preconditions re-verified at dispatch: arch-decision-2 recorded as bound (`e936065c`, #207);
W4-17b merged (`3f73cc73`, #213); W3-09 merged (`e3864c1e`, #216); W3-08 merged (`de526861`, #214);
#159 (W2-10) merged. No open PR touches the owned files at dispatch (`gh pr list`: #220 W4-11
frontend + #222 W4-15a harness; the W4-15b co-writer on `goal_admission.go:802-829` cannot
dispatch yet, so this slice lands first and W4-15b rebases).

## Measured basis (dispatch HEAD `b25e34ae`)

Scanner: `<HOST-PATH>/.local/state/gdd/scratch/w4-01/measure` (`go/ast`, stdlib only; modes
`funcs` per-function metrics, `probe` wraps a base line range in a synthetic function and reports
its line/statement/complexity shape, `top` statement map, `flow` control-flow points). It is
calibrated against the pinned linter's primary output at base: `startGoalAdmission` 277,
`recoverNativeGoalSessionForResume` 38, `abandonGoalSession` 33, `(*daemon).reconcile` 28,
`settleCancelledNativeStart` 33, `cleanupLinuxCloneWorkspace` 141, `handleCommand` 119,
`linuxV2LiveOwnerFences` 117 and the funlen statement counts `nativeTerminalPayload` 69,
`queueNativeEvent` 62 — 8/8 gocyclo and 2/2 funlen values reproduced exactly. The funlen
statement counter is a port of `github.com/ultraware/funlen@v0.2.0` `parseStmts`, the same code
the pinned golangci-lint 2.15.0 runs.

| measure | value | source |
|---|---:|---|
| `startGoalAdmission` span | `goal_admission.go:263-1222` | `funcs`; the contract's `:263-1224` end was off by two lines (the blank line and the next declaration) |
| `startGoalAdmission` funlen lines | 958 | `funcs` (`End.Line - Pos.Line - 1`) |
| `startGoalAdmission` funlen statements | 611 | `funcs`, funlen port |
| `startGoalAdmission` gocyclo | 277 | `funcs`; equal to the pinned linter's primary finding `goal_admission.go:263` |
| primary merged-config findings, `./internal/app/...` | 64 gocyclo + 2 funlen (453 output lines total) | `cd daemon && GOOS=linux golangci-lint run --config .golangci.yml --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...` |
| anonymous compensation defers | 3: `:271-275` launch-phase release, `:276-307` clone abort + reuse retire + unknown-outcome wrap, `:441-451` provider-bridge close | source |
| named defers | 2: `:1002` `operationCancel`, `:1040` `finishNativeStartPublication` | source |
| former locals captured by the defers | 9: `nativeLaunchAttempted :264`, `cloneLaunchStarted :265`, `reuseLaunchStarted :266`, `linuxClone :267`, `linuxCloneReuse :268`, `linuxV2Fence :269`, `releaseLinuxV2LaunchPhase :270`, `providerBridge :436`, `providerBridgeTransferred :440` | source |
| closures | 5: `abortFreshLaunchIfCancelled :462` (4 call sites `:630,:636,:662,:681`), `sink :665`, `PersistProcess :817` / `PersistProcessAuthority :824`, `settlePublishedCancellation :849` (6 call sites `:989,:1008,:1074,:1096,:1110,:1126`), `settleExpiredNativeStartFailure :862` (2 call sites `:915,:1005`) | source |
| `settleCancelledNativeStart` call sites | 6: `:853,:898,:907,:1042,:1147,:1204` | source |
| `finishNativeStartPublication` call sites | 7 inline (`:870,:919,:978,:992,:1011,:1029,:1178`) + the `:1040` defer | source |
| direct `startGoalAdmission` callers | production `app.go:1443`; tests `abandon_error_join_test.go:44` (added by W3-09, not in the contract's pre-W3-09 list), `deterministic_validation_test.go:572`, `harness_test.go:3828,:3865,:6002`, `native_process_stop_test.go:131`, `native_settlement_regression_test.go:700` | source |
| W4-17b lifecycle write-site gate | green at base (`TestRunLifecycleWritesStayInTheModule`) | `go test -run TestRunLifecycleWritesStayInTheModule` |

## Deliverable shape

1. **New `daemon/internal/app/goal_admission_tx.go`** — the transaction object and every phase:
   - `type goalAdmissionTx struct`: the five admission inputs (`daemon`, `key`, `claim`,
     `admission`, `workspacePersisted`), the nine former locals (five kept as fields, four
     transferred to compensation markers — D3), the former body locals that now cross a method
     boundary (phase-carried state; field map below), and the five ordered compensation entries.
   - `type goalAdmissionCompensation struct{ armed, released bool; run func(context.Context, error) error }`.
   - the phase methods and their sub-methods (seam table below), the five compensation entry
     methods, `abort`, the former closures as methods.
2. **`daemon/internal/app/goal_admission.go:263-1222`** — `startGoalAdmission` shrinks to:
   build the tx, defer `tx.abort(ctx, returnErr)` into the named return, return `tx.run(ctx)`.
   The declaration stays on line 263 (O23 does not trigger; the citation gate stays green).
   Nothing at `:1223+` changes; `abandonGoalSession` stays byte-identical.
3. **`daemon/internal/app/app.go:1443`** — untouched: the entry keeps its signature, so the caller
   is byte-identical (re-verified in the evidence by line comparison).
4. **New `daemon/internal/app/goal_admission_tx_test.go`** (package `app`) — the seam tests, each
   with its recorded red mutant. No existing test file changes.
5. **`scripts/ci/omo-control-native-evidence.py`** — one `SOURCE_PATHS` line (R9).
6. **This plan + `docs/evidence/rf-w4-01-goal-admission-tx-2026-11-22.md`** — the record.
7. **`docs/TODO.md`** — only if an entry is closed (none is expected).

### Transaction fields (former body locals that cross a phase boundary)

| field | base local | created at |
|---|---|---|
| `configuredKind`, `profile`, `providerAccess` | same names | `:344,:354,:355` |
| `linuxV2Required`, `linuxV2Owner`, `linuxV2Environment` | same names | `:359-363,:400` |
| `goalAPI`, `adapter`, `environment`, `deadline` | same names | `:404,:415,:426,:411` |
| `prepared`, `fingerprint`, `localHandleID`, `sessionKey`, `resume`, `attachQueued`, `attachReady` | same names | `:567/:587,:564,:609,:617,:494,:521,:767` |
| `sink`, `initialLeaseDeadlineAt`, `initialLeaseDeadline`, `nativeInvocation` | same names | `:665,:668,:669,:684` |
| `session`, `staged`, `handle`, `operationContext`, `operationCancel` | same names | `:802,:976,:1003,:1001` |
| `attached`, `runJournal`, `receipt`, `active`, `turnErr` | same names | `:1071,:1086,:1092,:1134,:1159` |
| `linuxClone`, `linuxCloneReuse`, `linuxV2Fence`, `releaseLinuxV2LaunchPhase`, `providerBridge` | same names | `:563,:489,:388,:387,:436` |
| `bridgeCompensation`, `cloneCompensation`, `reuseCompensation`, `nativeOutcomeCompensation`, `launchPhaseCompensation` | `providerBridgeTransferred`, `cloneLaunchStarted`, `reuseLaunchStarted`, `nativeLaunchAttempted`, the `:271` defer guard | D3 below |

None of these names is in the W4-17b `lifecycleFieldNames` set, and no tx method writes a lifecycle
field: the two `runningRun` write groups (`:1133-1141`, `:777-779`) stay at their base statements,
which the committed write-site scan keeps green.

### Compensation stack

Five entries, held as named fields, executed by `abort` in exactly the base body order of the
three anonymous defers (`abort` D2):

| # | entry | base defer/body | armed at | released at |
|---|---|---|---|---|
| 1 | `bridgeCompensation` | `:442-450` (defer 3) | `:436` (`providerBridge != nil`) | `:835-837` (`session != nil`) |
| 2 | `cloneCompensation` | `:277-288` (defer 2, first block) | `:563` (`linuxClone != nil`) | `:800` (`linuxClone != nil`) |
| 3 | `reuseCompensation` | `:289-300` (defer 2, second block) | `:489` | `:801` (`linuxCloneReuse != nil`) |
| 4 | `nativeOutcomeCompensation` | `:301-306` (defer 2, tail) | `:829` (`nativeLaunchAttempted`) | never (condition, not an ownership transfer) |
| 5 | `launchPhaseCompensation` | `:272-274` (defer 1) | `:387` (`releaseLinuxV2LaunchPhase != nil`) | `:846-847` (confirm path releases it) |

Base execution order at any exit: bridge close, clone abort, reuse abort, outcome wrap, launch-phase
release. `abort` runs the five entries in that order, threading `returnErr` (D1, D2).

### Seam table (base range → HEAD method; measured with the calibrated scanner)

Every closed range below is statement-aligned and was measured by `probe`; `l/s/c` = funlen
lines / funlen statements / gocyclo of the range. The bound is 120 / 60 / 25.

| # | base range | HEAD method | l/s/c | notes |
|---|---|---|---:|---|
| 0 | `:263-307` | `startGoalAdmission` (residual) + `newGoalAdmissionTx` + `abort` | new code, ~8 methods-lines | entry: build tx, defer abort, `run` |
| 1 | `:308-340` | `validateAdmissionEnvelope(ctx) (bool, error)` | 33/22/10 | deterministic-validation branch returns `(true, nil)` |
| 2 | `:341-358` | `validateConfiguredHarness()` | 18/13/8 | sets `configuredKind`, `profile`, `providerAccess` |
| 3 | `:359-363` + `:384-403` | `acquireNativeLaunchPhase(ctx)` | 5/1/1 + 20/12/9 | declaration hoisted; arms entry 5 |
| 4 | `:364-383` | `validateAdapterInvocationArgs()` | 20/15/7 | pi/OMP/OmO arg validation |
| 5 | `:404-425` | `resolveGoalAdapter(ctx)` | 22/16/8 | `goalAPI`, `deadline`, `adapter` |
| 6 | `:426-435` | `buildNativeEnvironment()` | 10/7/4 | |
| 7 | `:436-439` | `prepareProviderBridge(ctx)` | 4/3/2 | arms entry 1 |
| 8 | `:477-553` | `recoverRetainedGoalSession(ctx)` | dispatch, ~6 lines | resume branch |
| 9 | `:479-490` | `bindRetainedLinuxCloneReuse(ctx)` | 12/8/5 | arms entry 3 |
| 10 | `:491-521` | `queueRetainedSessionAttach(ctx)` | 31/14/5 | |
| 11 | `:522-552` | `rebindRetainedGoalSession(ctx)` | 31/15/16 | |
| 12 | `:554-638` | `prepareFreshGoalSession(ctx)` | dispatch, ~18 lines | fresh branch |
| 13 | `:554-558` | `requireLinuxV2OwnerBeforeCloneEffects(ctx)` | 5/3/2 | |
| 14 | `:559-561` | (inline in 12) | 3/2/2 | |
| 15 | `:562-578` | `prepareLinuxCloneWorkspace(ctx)` | 17/11/6 | arms entry 2 |
| 16 | `:579-607` | `prepareSubjectWorkspace(ctx)` | 29/21/9 | |
| 17 | `:609-629` | `recordGoalSessionLaunchIntent()` | 21/10/4 | |
| 18 | `:630-638` | (inline in 12) | 2x(3/2/2) | two `abortFreshLaunchIfCancelled` sites |
| 19 | `:640-642` | (inline in `prepareGoalSessionForLaunch` at the apply revision; `publishPreparedWorkspace()` before it) | 3/2/2 | |
| 20 | `:643-661` | `queueGoalSessionAttach(ctx)` | 19/7/5 | |
| 21 | `:662-664` | (inline in P2) | 3/2/2 | |
| 22 | `:665-667` | `queueNativeEventSink` method + field | ~3 | former `sink` closure |
| 23 | `:668-680` | `captureInitialLeaseDeadlines()` + `revalidateStartLease()` | 2/2/1 + 11/8/5 | |
| 24 | `:681-683` | (inline in P3) | 3/2/2 | |
| 25 | `:684-699` + dispatch | `buildNativeInvocation(ctx)` | 16/1/1 + ~4 | |
| 26 | `:701-708` | `requireLinuxV2CloneGrant()` | 8/6/3 | |
| 27 | `:709-740` | `applyLinuxV2Invocation(ctx)` | 32/24/14 | 4 verbatim error funnels |
| 28 | `:741-755` | `attachLinuxCloneGrant(ctx)` | 15/12/6 | |
| 29 | `:757-771` | `publishRetainedNativeStartBoundary(ctx)` | 15/11/5 | |
| 30 | `:772-780` | `armNativeLeaseSequence()` | 9/5/3 | |
| 31 | `:781-799` | `acquireNativeStartOwnership(ctx) (bool, error)` | 19/14/7 | `(true, nil)` = settled retained resume |
| 32 | `:800-801` | `handOffLaunchToNativeOwner()` | ~4 | releases entries 2, 3 |
| 33 | `:802-816,:826-848` | `launchNativeProcess(ctx) error` | 47/13/10 incl. closures; ~7 without | former `PersistProcess`/`PersistProcessAuthority` closures → methods; releases entry 1 |
| 34 | `:818-822` | `persistNativeProcess(pid, identity) error` | ~6/4/3 | former closure |
| 35 | `:825` | `persistNativeProcessAuthority(pid, identity, value) error` | ~3/1/1 | former closure |
| 36 | `:829` | (inline in 33) | 1 | arms entry 4 |
| 37 | `:850-860` | `settlePublishedCancellation(ctx, stage, cause, discardAttach) (bool, error)` | 11/8/5 | former closure, 6 call sites |
| 38 | `:863-895` | `settleExpiredNativeStartFailure(ctx, stage, stageErr, attached, discardAttach) (bool, error)` | 33/25/10 | former closure |
| 39 | `:897-905` | `settleStartCancelledAfterLaunch(ctx) (bool, error)` (renamed at the apply revision) | 9/7/5 | |
| 40 | `:906-914` | `settleCancelledProcessStart(ctx, startErr) (bool, error)` | 9/7/5 | |
| 41 | `:915-917` | (inline in P3) | 3/2/2 | expired window |
| 42 | `:918-958` | `failNativeStart(ctx, startErr) error` | 41/22/12 | |
| 43 | `:959-975` | `settleLeaseExpiredProcessStart(ctx) (bool, error)` | 17/13/6 | |
| 44 | `:976-987` | `requireStagedSession() error` | 12/10/3 | sets `staged` |
| 45 | `:988-1001` | `attachGoalNativeSession(ctx) error` | 14/11/5 | creates `operationContext` |
| 46 | `:1003-1036` | `openGoalNativeSession(ctx) error` | 34/26/18 | `Open` funnel + handle check |
| 47 | `:1041-1049` | `settleCancelledBeforeTurnStart(ctx) (bool, error)` | 9/6/5 | |
| 48 | `:1050-1062` | `settleLeaseExpiredAttachedStart(ctx) (bool, error)` | 13/10/5 | |
| 49 | `:1063-1070` | `persistNativeSessionHandle()` | 8/6/3 | |
| 50 | `:1071-1084` | `markNativeSessionAttached(ctx) error` | 14/10/5 | |
| 51 | `:1086-1090` | `loadNativeRunJournal() error` | 5/4/2 | |
| 52 | `:1091-1105` | `deliverNativeSessionAttach(ctx) error` | 15/10/4 | |
| 53 | `:1106-1131` | `fetchGoalRunContextJSON(ctx) ([]byte, error)` | 26/20/7 | fetch + validate + marshal |
| 54 | `:1132-1145` | `markNativeRunRunning(ctx) error` | 14/12/3 | |
| 55 | `:1146-1154` | `settleCancelledAttachedStart(ctx) (bool, error)` | 13/9/5 | |
| 56 | `:1155-1158` | `abandonCancelledOperationContext(ctx) error` | 4/3/2 | |
| 57 | `:1160-1190` | `settleLeaseExpiredTurn(ctx) (bool, error)` | 31/24/9 | |
| 58 | `:1191-1202` | `settleObservedTerminalCancellation() bool` | 13/11/7 | base's block has no error path |
| 59 | `:1203-1211` | `settleCancelledTurnStart(ctx) (bool, error)` | 15/11/6 | |
| 60 | `:1212-1217` | `settleNativeTurnFailure() (bool, error)` | 6/4/3 | |
| 61 | `:1218-1220` | (inline in P5) | 3/2/2 | |
| 62 | `:462-476` | `abortFreshLaunchIfCancelled(ctx) (bool, error)` | 15/11/5 | former closure |
| 63 | `:272-274` | `releaseNativeLaunchPhaseEntry` (entry 5 run) | 3/2/2 | |
| 64 | `:277-288` | `abortPreCheckoutLinuxClone` (entry 2 run) | 12/9/5 | |
| 65 | `:289-300` | `abortPreNativeLinuxCloneReuse` (entry 3 run) | 12/9/5 | |
| 66 | `:301-306` | `typeNativeStartOutcome` (entry 4 run) | 6/4/4 | |
| 67 | `:442-450` | `closeUnownedProviderBridge` (entry 1 run) | 9/7/4 | |

The five phase methods (`validateGoalAdmissionInputs`, `prepareGoalSessionForLaunch`,
`startGoalNativeSession`, `attachGoalNativeSession`, `openGoalNativeSession`,
`openGoalTurnAndSettleTerminalOwnership`) are thin orchestrators over rows 1-61 in base order;
their own probe: ≤ 45 lines, ≤ 30 statements, ≤ 16 gocyclo each. The residual entry + `run` +
`abort` are ≤ 20 lines each. The "attach + open" F2 phase is split into two methods at the
`operationCancel` seam (D4). Rows 63-67 are the five compensation entry bodies; each is also a
`*goalAdmissionTx` method, so the per-method bound covers the whole file.

Range coverage is monotone and non-overlapping except: row 3's `:359-363` sits inside row 2's
statement neighbourhood (the declaration group is hoisted into the linux-v2 method; it has no side
effect), row 25's dispatch adds the `if !linuxV2Required` branch around rows 26-28, and rows 33-36
interleave (33 is the surrounding method, 34-36 its former closures/inline statements).

## Contract-preserving decisions (recorded, not guessed)

**D1 — the compensation chain keeps the base execution order of the three anonymous defers.**
Options measured: (a) a pure acquisition-order LIFO stack — the base code makes the resources live
in the order launch phase `:387`, bridge `:436`, reuse `:489`, clone `:563`, so LIFO by acquisition
runs clone, reuse, bridge, launch phase; base runs **bridge, clone, reuse, wrap, release**. The
difference is observable: with a live bridge and a live clone the abort errors join in a different
order, and clause R2/4 freezes `errors.Join` order and error text. (b) an ordered five-entry chain
executed in the base body order. **Recommendation adopted: (b).** The evidence records the
base-vs-HEAD execution order per exit path.

**D2 — the defers-vs-entries choice for `operationCancel` and `finishNativeStartPublication`.**
The contract allows either "as defers inside the run method or as stack entries — choice and proof
recorded". **Adopted: defers inside `run`.** Base registration order `:271`, `:276`, `:441`, `:1002`,
`:1040` executed LIFO gives: `finishNativeStartPublication`, `operationCancel`, bridge close, clone
abort, reuse abort, wrap, release. `run` registers `defer tx.operationCancel` after
`attachGoalNativeSession` (base `:1002`) and `defer tx.daemon.finishNativeStartPublication(tx.key)`
after `openGoalNativeSession` (base `:1040`); `startGoalAdmission`'s abort defer is the outer frame,
so it runs after both. Proved identical; also keeps the success path free of compensation entries,
which the seam tests assert. The alternative would put two non-compensation entries on the stack
and make "no compensation on success" untestable without a second notion of entry kind.

**D3 — the nine former locals: five fields + four compensation markers.** The contract lists the
nine locals as fields and then requires ownership transfers to mark the entry released "instead of
setting a captured flag". Adopted: `linuxClone`, `linuxCloneReuse`, `linuxV2Fence`,
`releaseLinuxV2LaunchPhase`, `providerBridge` stay fields (they hold live handles);
`cloneLaunchStarted`, `reuseLaunchStarted`, `providerBridgeTransferred` become `cloneCompensation.released`,
`reuseCompensation.released`, `bridgeCompensation.released`; `nativeLaunchAttempted` becomes
`nativeOutcomeCompensation.armed` (the outcome wrap applies exactly when the native start was
attempted, which is what `:301` tests). Arming happens where the base code made the handle live and
releasing where the base code set the flag, so every exit-time guard of the base defers evaluates
the same value; none of the four markers is ever set back.

**D4 — `attach + open` is one F2 phase split into two methods at the `operationCancel` seam.**
`context.WithDeadline` is created at base `:1001` (after `attachNativeSession`, before `Open`) and
its `defer` `:1002` runs at function exit, i.e. after the turn phase. A single method covering both
statements would run `operationCancel` when the phase returned — before `StartTurn` — cancelling
the operation context the turn still uses. Adopted: `attachGoalNativeSession` creates the context
and stores it on the tx; `run` registers the defer between the two calls, at the base position.
The F2 phase is therefore delivered as two methods with one seam, not a reordering.

**D5 — no settle-call or error-funnel deduplication.** The superseded Change allowed deduplicating
the six `settleCancelledNativeStart` sites and the closure families "where the extracted control
flow is provably identical"; the binding reshape does not require it, and clause 3 makes any
changed join order a failure. Adopted: every site moves verbatim; the four near-identical Linux v2
invocation error funnels (`:712-720`, `:723-731`, `:734-739`, `:745-750`) stay four sites. Recorded
as a candidate for a later dedup slice, not for this one.

**D6 — `abort` receives the named return.** `startGoalAdmission` keeps `(returnErr error)`;
`defer func() { returnErr = tx.abort(ctx, returnErr) }()` is the single outer defer, so the
compensation chain reads and extends the same value the base defers captured, and a panic unwind
still runs it with a nil error exactly as base did.

**D7 — phase outputs are explicit in the signature only where a phase settles the admission.**
`validateAdmissionEnvelope` returns `(handled bool, err error)` and the start/ownership helpers
return `(settled bool, err error)`; every other phase returns `error` and carries its output on the
tx (the transaction's own working state). This keeps each base `return` representable without a
sentinel, and the seam table names the settled row per helper. **Delivered extension (2026-11-22):
the convention covers every method whose body can short-circuit a settle**, because a base
`return nil` there ends the whole admission while a nil error return means "continue" for a phase:
`startGoalNativeSession`, `attachGoalNativeSession`, `openGoalNativeSession`, and the
`openGoalTurnAndSettleTerminalOwnership` sub-methods `markNativeSessionAttached`,
`deliverNativeSessionAttach`, `markNativeRunRunning` return `(settled bool, err error)`, and
`fetchGoalRunContextJSON` returns `(canonicalJSON []byte, settled bool, err error)`. The first
delivered revision missed this in three phases and the Windows suite caught it as a nil
`tx.staged` panic in `openGoalNativeSession` (a settle returned nil, the run continued, and the
staged session had never been assigned); the fix is recorded in the evidence record.

**D8 — method granularity is bound-driven, not stylistic.** 277 base decision points at a 25
per-method cap force at least 12 methods; the delivered set has 67 tx methods plus the constructor
and the residual entry, measured max 19 gocyclo (mean 6.1), max 42 lines, max 29 statements (evidence §2). The
alternative (fewer, larger methods near 25) reduces the audit margin without reducing owned
complexity, and the contract asks for the bound with no new finding.

## Preserved semantics (byte-identical surfaces)

1. **Durable-write order** persist -> commit -> lease-arm -> resume -> input, every
   `daemon.store.*`, `daemon.control`/`goalAPI.*`, `adapter.Start`/`staged.Open`/`staged.StartTurn`
   call and every `errors.Join` argument order are unchanged; the evidence records the per-path
   call-sequence table base vs HEAD.
2. **Locks.** No new mutex; `daemon.mu` is taken at `:777-779` and `:1133-1141` only, at their base
   statements, in the base order; no lock is taken inside a compensation entry.
3. **Goroutines, channels, timers.** None added, none moved: `tx.abort` runs the same calls the base
   defers ran; the operation context is created and cancelled at the base positions.
4. **Text.** No error string, join order, log key, journal/receipt field or test name changes; the
   entry signature is unchanged, so `app.go:1443` and the seven test callers compile unchanged.
5. **Lifecycle.** The tx writes no `runLifecycle` field; the W4-17b write-site scan and its field
   set stay green.
6. **Behaviour on unmodelled states.** No new refusal: a state the base has no branch for takes the
   same base branch; the phases add only the `if err := tx.phase(); err != nil { return err }`
   dispatch, which is the base return itself.

## Evidence plan (all foreground; heavy suites serialized)

1. `gofmt -l ./internal/app/` (empty); `go vet ./...`; `GOOS=windows go build ./...`;
   `GOOS=linux go build ./...`; `GOOS=linux GOARCH=arm64 go build ./...` (exit 0 each).
2. Primary metric: `cd daemon && GOOS=linux golangci-lint run --config .golangci.yml
   --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...` before (recorded above) and
   after; the after set must add no production funlen/gocyclo finding. Secondary metric:
   `--no-config --default=none --enable=funlen,gocyclo,ineffassign`.
3. Per-method bounds: the calibrated scanner over the new file (every method l/s/c) and the
   residual entry; every value inside 120/60/25.
4. `go test -count=1 ./internal/app/...` (Windows; ~150-500 s). The six focused test functions of
   the contract plus the new seam tests, `-run` recorded. `-race` is refused on this host (no cgo).
5. WSL oracle: `wsl -d Ubuntu-24.04 -u accept` -> `cd /mnt/q/repos/wt-w4-01-goal-admission-split/symmetry/daemon && go test -count=1 -race ./internal/app/...`
   (the contract's race gate; the pre-existing `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`
   loopback stall recorded at W4-17a/W4-17b is attributed if it reappears, with the touched-path
   `-race` subset as the complementary run).
6. Control-flow audit (lesson 32): the scanner's `flow` mode over the base range and over the new
   file, mapped row by row in the evidence (every early return, fall-through, error short-circuit
   and default clause -> its HEAD equivalent), including the seven rows the orchestration adds
   (`run`/phase dispatch).
7. Compensation-order table: per exit path, which base defers ran (in order) and which HEAD entries
   run (D1 order). Durable-write, call-sequence and lock-site tables: mechanical extraction, base
   vs HEAD, 0 mismatches expected.
8. Red mutants: one per new test, applied alone, failing output recorded, reverted. Named mutants
   include: reorder two stack entries; drop a transfer mark (`:800`); run a compensation after
   transfer; drop the `:829` arming.
9. `python3 scripts/ci/test-omo-control-native-evidence.py` (R9) and
   `python3 scripts/ci/check-citation-existence.py` (§11) at the slice head; `git diff --check`.
10. `no file > 1500 L`: line counts of every changed file; `goal_admission.go` shrinks under
    1,000 L, `goal_admission_tx.go` (+ its test) stay under 1,500.
11. Host-path and DLP scans over the diff (user-profile path, backslash-u), `git status` clean of
    scratch tools.

## Out of scope (with reasons)

- The six `settleCancelledNativeStart` sites and the four invocation error funnels as a dedup
  family (D5); the `handleCommand` cancel branch (F3); `writeInputBounded` (F15); the native-run
  phase enum (S14); test-suite splits (W4-12a/b/c, W4-13).
- `app.go` beyond the untouched caller, `linux_owner_witness_linux.go`, `go.mod`/`go.sum`.
- `daemon/internal/app/goal_admission_phases.go` / `goal_admission_phases_test.go`: dropped by the
  reshape; the delivered files are `goal_admission_tx.go` / `goal_admission_tx_test.go`.
- Any behaviour change on a path the base does not reach (no new refusal, D7/§6).

## Delivered revision (2026-11-22)

Measured at the working tree (`goal_admission_tx.go` 1,460 lines, `goal_admission.go` 612 lines at
the apply revision on top of `a5d9bd88`, post-merge and after the accepted simplify findings):

- **Per-method bound.** 67 `*goalAdmissionTx` methods + the `newGoalAdmissionTx` constructor + the
  7-line residual entry: max gocyclo 19
  (`startGoalNativeSession` 40/29/19, `attachPublishedNativeSession` 34/23/19), max 42 lines
  (`launchNativeProcess`), max 29 statements; mean gocyclo 6.1. No method over 120 lines / 60
  statements / 25 gocyclo (scanner, evidence §2-§3). `openGoalNativeSession` (36/28/18) stayed one
  method; the invocation build is three methods (`requireLinuxV2CloneGrant`,
  `applyLinuxV2Invocation`, `attachLinuxCloneGrant`), as the plan's rows 26-28 anticipated. The
  turn phase is three methods (`openGoalTurnAndSettleTerminalOwnership` 5/4/3 +
  `attachPublishedNativeSession` 34/23/19 + `runGoalTurn` 17/12/9): the `err != nil || settled`
  guards pushed the single method to 28 gocyclo, over the bound, so the post-Open publication
  prefix was split from the turn-and-settlement half.
- **Seam deviations from the plan's table.** The resume branch is four methods
  (`prepareRetainedGoalSessionForLaunch` → `recoverRetainedGoalSession` (returns the
  `resumedGoalSession`), `markRetainedWorkspaceRecovery`, `queueRetainedSessionAttach`,
  `rebindRetainedGoalSession(ctx, resumed)`); the fresh branch keeps `requireLinuxV2OwnerBeforeCloneEffects`
  and the `MarkWorkspaceRecoveryRequired` write inline in `prepareFreshGoalSessionForLaunch`; the
  plan's rows 13/14/18/21/41/61 are inline statements of their phase, not methods. Range coverage
  is still monotone and non-overlapping.
- **Field map additions.** `bindingID` (base `:323`), `linuxV2Environment` (base `:400`),
  `linuxV2Owner`, and the `resumed` value threaded as an explicit parameter from
  `recoverRetainedGoalSession` to `rebindRetainedGoalSession`.
- **Settled propagation.** D7's delivered extension (three phases + four sub-methods); the first
  revision's dropped settle was caught by the Windows suite as a nil-`tx.staged` panic.
- **Citation.** `goimports` removed five imports that only the moved code used, so the entry
  declaration moved from `:263` to **`:258`**; `docs/design/harness.md:288` was refreshed
  citation-only under O23 (it was not stale at the pre-goimports revision). The `origin/main` merge
  then brought #223's comment trims in, moving the declaration to **`:249`**; the citation was
  refreshed to `:249` at the apply revision. The `startGoalAdmission` span is now `:249-255`.
- **Settled-return convention names.** `typeNativeStartOutcome` ignores the context (blank
  parameter) so no new `unparam` finding appears. Independent review then found the same class in
  seven extracted phases whose bodies make no context-dependent call
  (`queueRetainedSessionAttach`, `rebindRetainedGoalSession`, `queueGoalSessionAttach`,
  `attachLinuxCloneGrant`, `publishRetainedNativeStartBoundary`, `failNativeStart`; a seventh,
  `settleObservedTerminalCancellation`, dropped the unused context parameter along with its
  always-nil error result): the six keep `_ context.Context` for the same reason —
  the dispatch shape stays uniform, the T3 call-shape audit stays valid, and `unparam` stays
  silent (a dropped parameter would have changed the audited call shapes instead).
- **File sizes (contract gate "no file > 1,500 L").** `goal_admission.go` 1,593 -> 612 L;
  `goal_admission_tx.go` 1,460 L; `goal_admission_tx_test.go` 319 L (6 tests).
- **Lint (primary/secondary).** Primary merged-config run: base 147 findings -> 147 at the apply
  revision; the `startGoalAdmission` gocyclo 277 row is resolved (`gocyclo` 64 -> 63), base's own
  `gocritic ifElseChain` moved with its code from `goal_admission.go` into the tx file, and the
  only added row is the post-merge `whitespace` finding at `process_persistence_test.go:775`
  (brought in on main by #223, outside this slice). Secondary `--no-config
  --default=none --enable=funlen,gocyclo,ineffassign` at the default 60/40/25 bounds: 0 findings in
  the new files. Both commands exit 1 (pre-existing findings exist), as at base; evidence §8.
- **Apply-stage simplification (2026-11-22).** Accepted simplify findings applied: the
  `publishPreparedWorkspace` pass-through body inlined back at its single call site
  (`prepareGoalSessionForLaunch` 17/11/7 -> 19/12/8, inside the bound); `settleCancelledPublishedStart`
  renamed `settleStartCancelledAfterLaunch`; the seam-test probe's name/entry pairing built from one
  constructor literal (no test assertion changed). Finding 2 (clone-abort cleanup-context dedup)
  kept unapplied — D5 defers dedup to a later slice; the rename landed together with the record
  updates. Evidence §8, apply subsection.
