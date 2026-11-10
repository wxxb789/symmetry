# Goal 0006b resumed convergence-test safety review

Date: 2026-09-29  
Served route: `deep-low`  
Scope: read-only review of exactly the four named repaired tests. `linux_owner_v2_app_test.go` is excluded.

## Review boundary and subject

I read the four tests, their doubles, production callers, root/daemon `AGENTS.md`, `docs/design/quality.md`, the active original Goal 0006b, the convergence goal, and both prior worker receipts. Receipts were treated only as delta pointers; conclusions below come from current source.

The checkout is dirty at HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`. These SHA-256 values bind this review snapshot, not an accepted commit:

| Path | SHA-256 |
|---|---|
| `AGENTS.md` | `e62bc2a572b16ea70a2c4b538c70e8c20a0b6697336b534935c0c39b006c2951` |
| `daemon/AGENTS.md` | `0ba047bd763b1c94651aedaab2970fababd8b33bc30f86c2b182b1681130c971` |
| `docs/design/quality.md` | `96a89469161dc0cb8e9e8e718d2c5731f5abd5b08395ec1bff0f8073dc14aa6a` |
| `docs/goals/0006b-crash-safe-process-containment.md` | `d8871190ee18d6639b599a1cdcda2b09bf171f4d335919550f96a1ab34418054` |
| `docs/evidence/goal-0006b-convergence-goal-2026-09-29.md` | `fc8f4e96a0d28e38bbde2ba5ad69b4b305c8077a044d0e31cf577f610d556a5b` |
| `daemon/internal/app/app.go` | `4345733d131a3a3e6aa161d931461988dab7cec8168b824aace68acd6fedd7f6` |
| `daemon/internal/app/cleanup.go` | `043b98247f4822c07eefea2c2858a4b3295cda858b897d9130c7f1d598d2f9ba` |
| `daemon/internal/app/app_test.go` | `c7b34a0216dce620bf97ba76b30e5828bd925a4f7d1bd02f044a1d97349cc1c2` |
| `daemon/internal/app/cleanup_test.go` | `08ece6ca9b5b6da5853c571a5a4b412c28af0b2a2ecdb17bb5a249df73ce9b37` |
| `daemon/internal/app/recovery_isolation_test.go` | `52729b7e5d015abeec6d71c5ec35cb30fdbc64c5f66b05c7742e96eaa60340d4` |
| `daemon/internal/platform/containment_supervisor_linux.go` | `cb9884c9c828b0b3d53f28d4d4c7410ca72e92f9aa240d5e6d25afbcd880c849` |
| `daemon/internal/platform/process_unix.go` | `3402f4fc74b09a69073557440c3ae4f17926a8a1ed5cf6b8a5e3a52ab1e77a38` |
| `daemon/internal/platform/containment_supervisor_linux_test.go` | `3739b0afbb0536c9aa91ca6093cc8f516cb383d8109fefca31b97eeddaf051c2` |

## Findings, ordered by severity

### P0/P1 - none

No repair weakens process authority, creates a receipt from PID absence, clears uncertainty, serializes independent work behind a blocked operation, or uses a sleep/poll race to manufacture success.

### P2 - snapshot test does not identify the reconcile-backoff timer

The registration channel correctly separates initialization from the first exact `Reconcile` observation (`recovery_isolation_test.go:551-646`). However, `timers.await(t, minimumInterval)` matches any unconsumed timer of that duration. `startBackground` starts liveness and outbox workers before initial reconcile, and both also create `minimumInterval` timers (`app.go:669-878`). `recordingTimerFactory.await` does not record timer ownership (`app_test.go:7452-7505`). A background timer can therefore satisfy the alleged backoff observation.

The test still proves registration before reconcile, snapshot survival despite pending recovery, and durable cancel acknowledgement before the same-RunKey assignment starts. It does not prove the narrower ordering that snapshot scheduling precedes creation of the specific reconcile retry timer; asynchronous scheduling after backoff arm could pass. Production is currently correct: `reconcile` schedules the snapshot before returning `!recoveryPending` (`app.go:2764-2847`), then `run` arms retry (`app.go:669-715`). This is a P2 evidence gap, not a production defect. Use a source-specific backoff observation before citing the test as full snapshot-before-backoff proof.

### P3 - none

No other concrete defect or misleading assertion was found in this slice.

## Safety and synchronization analysis

### Blocked input versus another RunKey

The repair replaces a 500 ms completion proxy with an exact `WriteInput` entry signal established before enqueue (`app_test.go:5469-5520`). The first run is already blocked. Production gives non-cancel commands per-`RunKey` lanes and separate goroutines (`app.go:927-1030`). Receiving `otherEntered` while the first remains blocked directly proves cross-run progress. Timeout fixes the result as failure before releasing the first run, so a late write cannot become success. The final count retains the exactly-once check.

### Blocked cleanup versus unrelated outbox

`cleaner.entered` establishes the cleanup block before `flushAll`; `eventDeliveryControl.AppendEvents` emits the exact pre-subscribed delivery event (`cleanup_test.go:150-199,890-898`). Cleanup and outbox use separate paths (`cleanup.go:20-34`; `app.go:10191-10258`). The three-second timer is only a guard: cleanup remains blocked until cancellation and only actual `AppendEvents` is success. The retained-journal assertion confirms cancellation did not turn the block into deletion.

### Pending recovery snapshot

Registration, reconcile invocation, and acknowledgement are exact channel events, not elapsed-time proxies. `scheduleSnapshotForRequest` enqueues commands and gates same-RunKey assignment on completion (`app.go:3298-3405`), so rejected cancel acknowledgement before assignment start remains meaningful. The P2 gap concerns only identification of the retry timer, not snapshot survival or command-before-assignment ordering.

### Linux mirror scan failure

`pauseDescendantMonitoring: true` reserves scripted procfs calls for explicit scans but does not suppress the initial or close-requested scan (`containment_supervisor_linux_test.go:1917-2013`). The monitor performs its initial scan before deciding whether to create a periodic ticker; while paused it still services explicit requests (`process_unix.go:619-698,946-1006`). Helper-death takeover still calls pidfd-bound termination and `closeUntil`, whose post-kill scan makes scan loss sticky (`containment_supervisor_linux.go:1514-1589`; `process_unix.go:1128-1321`). Pausing periodic monitoring therefore does not bypass receipt authority.

Retained pidfd and sticky scan-loss assertions are meaningful. Failed `closeUntil` copies `mirror.fd` back to `supervisor.pidfd`; retryability requires no receipt plus retained pidfd/final proof (`containment_supervisor_linux.go:1223-1230,1545-1557`). The test checks both exact descriptor references, sticky `descendantScanLost`, incomplete close, retryability, no receipt, and `[SIGKILL, 0]` kill/probe sequencing. Receipt creation is after successful close, absence proof, drained scan events, and durable seal; this path exits earlier.

## Scoped simplification result

No worthwhile simplification was found.

- `otherEntered` reuses `recordingProcess` and is the smallest exact cross-run event.
- Cleanup reuses `eventDeliveryControl`; another wrapper would add indirection.
- The small registration override observes a real boundary. Folding it into the broad retry fake would couple unrelated tests. The timer ambiguity needs a precise observation, not a generic timer abstraction.
- Existing `pauseDescendantMonitoring` accurately removes only the fixture's periodic scheduler race; a bespoke monitor fixture would be less faithful.
- Repeated claimed-run setup is local and explicit; this slice does not justify another helper.

## Evidence gaps

No test, build, binary, witness, child task, or provisioner ran in this review. Parent-supplied prior raw events report:

- Windows App/full: exit 0, 898 passes, 1 platform skip, 186.949 s.
- Linux platform race: exit 0, 308 passes, 2 kernel-dependent skips.

These are working-tree evidence, were not rerun here, and are not exact-commit acceptance. This report excludes concurrently repaired `linux_owner_v2_app_test.go`. It establishes no qualified A/B host, protected Pi/OpenCode inputs, authentic provider action, clean commit, or final CI artifact.

## Conclusion

Three repairs are safety-preserving and discriminating for their core claims: blocked input does not block another RunKey, blocked cleanup does not block unrelated outbox, and mirror scan loss cannot mint a StopReceipt while exact pidfd authority remains retryable. The recovery test remains strong for registration, snapshot survival, durable command-before-assignment ordering, and retry scheduling, but has the P2 gap for ordering against the specific backoff timer.

**Verdict: approve this four-test repair slice with the P2 evidence limitation; do not cite the current timer assertion alone as snapshot-before-backoff proof.** Original Goal 0006b remains active and requires a clean exact commit plus qualified-host evidence.


---

## Bounded addendum: Linux App v2 fixture

Addendum date: 2026-09-29  
Scope: `daemon/internal/app/linux_owner_v2_app_test.go` changed fixture regions only.  
Current source SHA-256: `4fbb4d5b0dfb7f28e1e515fae80aa7d03d38a9deefed3ae9b416396b93122a77`.

### Addendum findings, ordered by severity

#### Resolved - prior blocking P2 capacity-wait finding

Every main-test exact-event wait now has a local 10-second failure guard: first native Start and first-owner readiness (`:2475-2490`), launch-phase timer creation (`:2492-2499`), first live turn (`:2521-2525`), second native Start (`:2531-2539`), second owner binding (`:2540-2547`), and second live turn (`:2548-2552`). The rejected scenarios now await the existing `firstWorkerDone` signal with the same bound (`:2564-2568`). A regression therefore produces a fixture-specific failure instead of waiting for the package timeout.

Deferred teardown releases a still-held first-owner gate, cancels the run context, and joins workers through a `stopped` channel with its own 10-second guard (`:2340-2355`). `fakeNativeGoalSession.WaitTurn` selects on that context (`harness_test.go:4398-4414`), so the intentionally unclosed `runWaitRelease` channel cannot strand ordinary failure cleanup. If cancellation itself regresses, teardown reports the separate worker-cleanup failure without hanging the package indefinitely.

The supplied overlay mutation is discriminating evidence for the exact repaired boundary: replacing only the first session's `waitTurnEntered` channel removes the expected `firstTurnEntered` event. Before the patch it reached the package panic timeout at the old raw receive. After the patch it failed in 11.192 seconds with `first capacity-two Run did not enter its live turn after RELEASE`, without a package timeout or worker-cleanup timeout. This directly demonstrates both local event failure and successful deferred cancellation/join for that path.

#### Blocking findings - none

#### Nonblocking findings - none

No residual concrete in-scope safety, coverage, or bounded-teardown flaw was found. The following assertions remain discriminating.

### Callback and preclaim ordering

- Wrong-fence and wrong-RunKey callback attempts assert rejection before any Store write and then read back unchanged containment state (`:31-134`).
- The foreign unresolved owner is inserted before the target claim in the RELEASE-admission regression (`:136-183`). That ordering exercises the real pre-existing-owner scan rather than creating the conflict after target setup. The foreign journal is byte-compared before/after refusal, so admission cannot pass by mutating away the blocker.
- Local cancellation and lease-safety-margin refusal are separately checked (`:185-210`). Production snapshots live owners and applies the Store check under `linuxV2AdmissionMu`, rechecking the target around the snapshot (`app.go:1720-1799`); the fixtures exercise those boundaries rather than substituting a second authority.

### Cleanup candidate, retention, and exact artifact evidence

`newLinuxCloneAppCleanupFixtureMode` builds the exact clone grant, checkout, stop receipt, and version-matched release proof. In Goal-managed mode it records the promotion receipt, creates a fully validated artifact `Evidence` whose subject/resource/commit/tree digest and receipt payload agree, queues it, and marks that exact delivery delivered before cleanup eligibility (`:735-954`). This discriminates artifact-backed candidate cleanup from terminal-state-only cleanup.

The non-Goal mode deliberately records `TaskResultProgress`, `RetainWorkspace`, exact checkout identity, physical release, and no cleanup intent/proof (`:844-909`). It therefore keeps physical release separate from task completion and from destructive checkout cleanup. `linuxCloneAppReleaseProof` validates the synthetic unit-fixture proof against the exact handoff and embedded StopReceipt (`:957-980`). Direct journal serialization clears live containment only after those exact validated objects exist; it models an already-completed release boundary for State/App replay tests and is not physical-stop evidence or a production effect.

The two cleanup replay matrices remain meaningfully distinct. `TestLinuxCloneCleanupRecoversAcrossAAndCompleteFaults` covers pre-A failure, post-A lost response, and post-rename Complete acknowledgement loss, including exact write-count replay (`:1281-1352`). `TestLinuxCloneCleanupReplaysAcrossAAndCompleteFaults` additionally pins the intermediate A-material/effect state on both sides of the external effect (`:1427-1476`). Keeping both preserves durable convergence and effect-boundary discrimination; this is not duplicated fixture logic worth removing.

### Capacity-two authority discrimination

`suppressTaskResult: true` prevents an early semantic result from terminalizing the first run while `WaitTurn` remains live (`:2362-2376`). Distinct buffered `firstTurnEntered`/`secondTurnEntered` channels and per-RunKey owner identities prevent one run's signal from satisfying the other. The successful scenario observes the first exact live-owner fence after RELEASE but before task completion, surfaces the second callback-chain result through buffered `secondOwnerReady`, then requires both journals to remain active with clone intent and containment authority (`:2517-2562`). This catches the previously observed premature-TaskResult failure where the first live-owner set was empty.

The blocked scenarios preserve both failure modes: unresolved pre-RELEASE ownership, and the same ownership with stale local state. They require no second native start, no second clone/cgroup effect, and only one owner preflight/invocation (`:2563-2578`). Production releases only the serialized pre-RELEASE launch phase after `confirmLinuxV2LiveRelease`; it does not equate RELEASE with task completion (`app.go:4488-4494,5024-5033`).

### Bound handoff readback

`bindAppLinuxV2OwnerWithCallbacks` reloads the persisted bound handoff after `BindHandoffV2` and commits that exact clone (`:3180-3193`). This is material: bind may add the exact helper/mirror owner identity, so committing the pre-bind local value would weaken the callback chain. The subsequent journal read requires committed authority, cgroup state, and process details before publishing the fake session's live-owner status (`:3194-3207`).

### Runtime evidence and limits

No command was run by this reviewer for the addendum or re-review. Parent-supplied working-tree evidence is:

- The original regression was red when premature `TaskResult` made the first live-owner set empty.
- The settled focused 14 scenarios passed normally (exit 0, 9.598 s) and under race (exit 0, 42.340 s).
- The targeted missing-first-turn overlay changed from a 15-second package panic timeout before repair to the named local failure in 11.192 seconds after repair, with no cleanup timeout.
- Post-patch `go test -p 2 -count=1 -timeout 180s -run '^TestLinuxV2CapacityTwoWaitsForReleaseThenRechecksOwnerState$' ./internal/app` passed all three table scenarios (exit 0, 3.122 s).
- The same focused command with `-race` passed all three scenarios (exit 0, 8.088 s).
- `gofmt -l daemon/internal/app/linux_owner_v2_app_test.go` produced no paths and exited 0.
- The temporary mutation overlay and source were deleted after their exact owned contents were checked; they are not part of the reviewed source snapshot.

These are not exact-commit acceptance. Full Linux normal/race sequences remain pending and were not duplicated or inferred green. The unit fixture creates no process, cgroup, Git, provider, or release effect and grants no capability claim.

### Addendum verdict

The previous four-test verdict is unchanged. For this Linux App fixture addendum, authority, artifact, retention, replay, capacity, and bound-handoff assertions are safety-preserving and materially discriminating. **The prior blocking capacity-wait finding is resolved by source inspection, the supplied failure-oriented mutation proof, and focused ordinary/race passes of all three scenarios. No blocking production/test finding and no nonblocking finding remain in this bounded addendum.** Goal 0006b remains active and unaccepted; full Linux normal/race results remain pending evidence, not inferred passes.
