# rf-w4-22 response-loss seal consumer — evidence record (2026-11-23, implement stage)

Contract: `goal-rf-w4-22-response-loss-seal-consumer.md` (GDD state root, external to the
repository), with the 2026-11-21 arch-decision-2 rulings (R1, R14), the verification amendments, the
program-wide conductor rules and the 2026-11-22 comment policy binding over the earlier text. Plan:
`docs/plans/2026-11-23-rf-w4-22-response-loss-seal-consumer-plan.md`. Base revision:
`e13450bc03499a62e77b125ac02596cdb433ce05` (#185 `4b034f50` and W4-12c `e13450bc`/`#263` merged).
Branch: `refactor/rf-w4-22-response-loss-seal-consumer`. Host: Windows, Go 1.27.4; the Linux-only
rows run Ubuntu-24.04 over the same worktree through its mount point (`<worktree>/daemon`, Go
1.27.0) as `wsl -d Ubuntu-24.04 -u accept`. The Windows and WSL suites never ran concurrently.

## 1. Subject bytes (sha256, final tree)

| File | sha256 |
| --- | --- |
| `daemon/internal/platform/supervisor_linux_client.go` | `0b5642affc09cd6470b57bb173cc17c4544ebf7adffbb9745102c3a63a6b1821` |
| `daemon/internal/platform/containment_supervisor_linux_test.go` | `780b20731bc3e625f28f46ce85e820e106045318a9a4918c47595e4515c14597` |
| `daemon/internal/execution/recorder.go` | `10833a31fbfa02f4b36ef2f8783a9cea1c4d158caf2e2621fc115a9f0bb90114` |
| `daemon/internal/execution/recorder_test.go` | `b5fb2866b8c8afe15229b8d2493ac4934d190aaf4401e05f4277e91181811e9f` |
| `daemon/internal/execution/process_launcher_linux.go` | `77ecbf4c8453f052446e824a06dfb809771039563def8e77aff8fc56a601a540` |
| `daemon/internal/execution/process_launcher_linux_test.go` (new) | `17cfd115e3c971229c3168658455682a68101b905de997bb74c46f2aa1674faa` |
| `daemon/internal/app/process_recorder.go` | `b2ab5e7d6ca781ea4bc4f9c98d2a5bc6d77d616fbe726b27a4ae179e580374e3` |
| `daemon/internal/app/process_recorder_test.go` | `824cc4828443b513513c63b359cb1c4a3b016e7f5ca48819b08c8d808acce982` |
| `daemon/internal/app/apptest/faultstore.go` | `aeb257808710dab11486154c9817671822376c0ebe3a0bccc48be783846742a2` |
| `docs/TODO.md` | `84d3c7d618fcf5e94ec9b3228153e4d842db6df6120361f956a29b5d34d54a36` |
| `docs/archive/CLOSED-TODO.md` | `e91f81ede69711e3b8a70611822090d990e9f83f5525e8fafa45a114b546cc41` |
| `docs/plans/2026-11-23-rf-w4-22-response-loss-seal-consumer-plan.md` | `c23a191504e5b2857ba2227064e120ee5117c66fa5108dcb7d904220d96f6972` |
| this record | the file you are reading (no self-referential hash) |

## 2. The one new write

The port (`daemon/internal/execution/recorder.go`) gains an eleventh member:

```
type RecorderCapabilities struct { …, RecordSupervisorRecoveryScanSeal bool }
type Recorder interface { …, RecordSupervisorRecoveryScanSeal(authority.Supervisor, uint64) error }
type RecorderFuncs struct { …, RecordSupervisorRecoveryScanSeal func(authority.Supervisor, uint64) error }
```

- `recorderFuncs.RecordSupervisorRecoveryScanSeal` fails closed with `errRecorderWriteUnavailable`
  when the function is absent, exactly like the other ten.
- `recorderWrites` maps the capability to its own method (the non-`recorderFuncs` fallback path);
  `RecorderFuncs.Capabilities` reports presence.
- `invocationWrites`' frozen-member fallback and `MirrorRecorderMembers` deliberately do not carry
  it: it has no legacy `Invocation` member to keep byte-identical, so there is nothing to mirror and
  no hand copy in an adapter (contract failure control).
- App implementation (`daemon/internal/app/process_recorder.go`): the recorder factory binds the run
  key, `recordSupervisorRecoveryScanSealCallback(key)` is a thin factory over
  `daemon.persistSupervisorRecoveryScanSeal` — the restart consumer's one-exact-replay
  implementation (`daemon/internal/app/recovery.go:618-631`), unchanged — holding the
  store-lifetime read lock and refusing with `errDaemonStoreClosing` while the store is closing or
  absent, the discipline every neighbouring launch-path CAS follows
  (`cleanup_glue.go` `persistProcessMarker`/`selectStoreWrite`); the implementation function itself
  is untouched so the restart consumer's path and error surface stay exactly as at base.
- Launch plumbing: `execution/process_launcher_linux.go` passes
  `writes.RecordSupervisorRecoveryScanSeal` into `platform.LinuxSupervisorStartSpec.RecordRecoveryScanSeal`,
  `StartLinuxSupervisor` stores it as `linuxSupervisor.recoveryScanSealWrite`, and
  `recordRecoveryScanSeal(sequence)` calls it with `*supervisor.ContainmentAuthority()`; a nil write
  is skipped so the all-sealed assertion refuses exactly as at base (the OpenCode
  recorder-less launch path). The spec construction is the small `linuxSupervisorStartSpec`
  function so the carrier line is directly testable
  (`TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite`); the production branch itself stays
  unreachable under `go test` because `platform.DurableSupervisorHandoffAvailable()` is false in a
  test binary.

## 3. Position and the as-built amendment

Base order on the in-process response-loss path (`recoverResponseLossStop` success arm):
`verifyMirrorObservation` (durable `assert_clean`) → `freezeMirrorObservation` (durable mirror
`seal` → local assert → durable `assert_all_sealed`), then receipt adoption.

HEAD splits `freezeMirrorObservation` into `sealMirrorObservation` (monitor stop, barrier restore,
durable mirror seal, local assert) and the existing `assertDurableScanStateSealed`, and
`freezeMirrorObservation` is their composition, so `stop()`'s live sequence is unchanged. The
recovery arm calls: verify → mirror seal → **`recordRecoveryScanSeal(sequence)`** → all-sealed
assertion → receipt adoption.

The contract's Design 2 phrase "before the mirror freeze" cannot be satisfied literally:
`state.Store.RecordSupervisorRecoveryScanSeal` refuses without a durable mirror seal
(`daemon/internal/state/containment_journal.go:413-415`, pinned by
`TestLinuxRecoverySealRequiresCleanHelperProofAndPriorMirrorSeal`), and the mirror's durable seal is
written by the freeze itself. The smallest contract-preserving amendment is the position above: one
new durable write, after the mirror's durable seal and before the durable all-sealed assertion, and
strictly before the receipt adoption — which is clause 1's binding requirement and the target of the
"write the seal after the receipt/mirror freeze" mutant. Recorded in the plan §3.1.

Failure semantics: the write error is returned (the caller keeps its existing
`response-loss mirror freeze: %w` wrapper and fires the containment-unproven callback once), so the
stop stays fail-closed with the same sentinel class, no adopted receipt and no durable advance. The
alternative — swallowing the write error and letting the assertion refuse — was rejected because it
misattributes the cause; for the *absent* capability the byte-identical base refusal is preserved
automatically. Recorded in the plan §3.3 and pinned by the pin's failure arm.

## 4. Durable-write order, base vs HEAD (measured)

The pin's journal model logs every durable scan write attempt in order. Base column: the base
revision's own pin (`TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed`), run in a
throwaway worktree at `e13450bc` with a temporary order log (worktree removed afterwards); HEAD
column: the rewritten pin. Both on WSL, same scenario (live-owner control close, long lease,
`Terminate(true)`):

| Tree | Ordered durable writes | Durable end state |
| --- | --- | --- |
| base | `mirror:begin:1, mirror:clean:1, mirror:retire:1, helper:begin:1, helper:clean:1, helper:retire:1, mirror:begin:2, mirror:clean:2, mirror:retire:2, mirror:assert_clean:2, mirror:seal:2, mirror:assert_all_sealed:2, mirror:assert_all_sealed:2` | helper `1/0` (unsealed), mirror `2/2`, all-sealed assertions attempted 2 / refused 2, no receipt, one containment-unproven mark, stop = `ErrLinuxSupervisorStopUnproven` + `ErrLinuxSupervisorResponseLost` |
| HEAD | the same prefix, then `mirror:seal:2, recovery-scan-seal:2, mirror:assert_all_sealed:2` | helper `2/2` (the recorded previous clean 1 + 1), mirror `2/2`, assertions attempted 1 / refused 0, receipt adopted, stop = nil |

Reading: exactly one new durable write (`recovery-scan-seal:2`) at the stated position; the refused
assertion's second attempt disappears because it now succeeds on the first. `sealMirrorObservation`
still runs the same monitor-stop/barrier/seal/local-assert sequence the base freeze ran, and the
live stop path calls the same composed function.

## 5. Crash windows and durable states

| Crash point (in-process response-loss stop) | Durable state | Consumer that resolves it |
| --- | --- | --- |
| before the mirror seal | authority committed, mirror unsealed, helper `1/0` | base behaviour: a restarted daemon's plain route writes the receipt only if `requireLinuxScansSealedForAuthority` holds; otherwise the stop stays unproven (unchanged by this slice) |
| after the mirror seal, before the recovery seal | mirror `2/2`, helper `1/0` → `RecoveryHelperScanSealEligible()` true | today's restart path: `terminatePersistAuthorityWithScanSeal` → `RecordSupervisorRecoveryScanSeal` (`Completed+1`) → receipt (`daemon/internal/app/recovery.go:449-470`) |
| after the recovery seal, before the receipt | mirror `2/2`, helper `2/2`, no receipt | a restarted daemon takes the plain `terminatePersistAuthority` route (`RecoveryHelperScanSealEligible` is false because the helper is sealed) and the receipt write passes `requireLinuxScansSealedForAuthority`; a dead helper with no persisted receipt refuses exactly as at base; an in-process retry re-attempts the same endpoint proof, and because this window's failure callback already wrote the durable containment-unproven mark the store refuses the rule itself (`containment_journal.go:413-415`) — the pin's failure arm re-runs `Terminate` and pins that refusal and the re-attempt |
| after the receipt adoption | receipt durable | unchanged base behaviour |

Store-level idempotency and the one-exact-replay barrier are pinned by the state suite
(`TestLinuxRecoverySealRequiresCleanHelperProofAndPriorMirrorSeal`), paragraph "Replaying the exact
proof performs a fresh journal write barrier" in `state.RecordSupervisorRecoveryScanSeal`; the pin
also observes the helper's endpoint returning the same `sequence 2` and an equal receipt after the
in-process consumer consumed it.

## 6. Red mutants

Each edit applied alone to the final tree, run against the two pin tests on WSL, then reverted;
`sha256sum -c` over the pre-mutant hashes of the six changed source files says `OK` afterwards.

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| m1 drop the seal write (= base behaviour, m2/m3/m4 isolated from it) | `recordRecoveryScanSeal` body reduced to `return nil` | both tests | `live-owner response-loss stop = Linux supervisor stop is unproven … want a completed stop` (success arm); `seal write failure = Linux supervisor stop is unproven … want the freeze wrapper unchanged` (failure arm) |
| m2 write after the mirror freeze's assertion | recovery arm order: all-sealed assertion, then the seal write | both tests | same two lines: the assertion refuses before the write is reached |
| m3 record `Completed+2` | `recordRecoveryScanSeal(sequence + 1)` in a correctly ordered arm | both tests | success arm: `… want a completed stop`; failure arm: `durable write order tail = []string{"mirror:assert_clean:2", "mirror:seal:2", "recovery-scan-seal:3"}, want […, "recovery-scan-seal:2"]` |
| m4 swallow the write error (the rejected alternative) | `_ = supervisor.recoveryScanSealWrite(…)` then `return nil` | failure arm only (the success arm stays green, as designed) | `seal write failure = Linux supervisor stop is unproven … want the freeze wrapper unchanged` |
| m5 record a constant sequence | `recordRecoveryScanSeal` passes `2` instead of the helper's returned sequence | the resumed-helper arm only (the other three arms stay green, as the adversarial review predicted) | `live-owner response-loss stop = Linux supervisor stop is unproven … want a completed stop`, i.e. the store's exact `Completed+1` rule refused the forged sequence |
| m6 drop the launcher carrier | `linuxSupervisorStartSpec`: `RecordRecoveryScanSeal: nil` | new `TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite` | `start spec dropped a recorder write` |

m1 is also the base-behaviour red for the rewritten pin's first assertion: the base tree has no
write, so the base's own refusal (measured in §4) is exactly what m1 reproduces with every other
byte of the slice final.

The pin's four arms and the carrier test together close the two control gaps the independent
adversarial review found (a constant sequence surviving every other test, and the production
carrier line being untested); each mutant above was applied alone to the final tree and reverted.

## 7. Gates

Working directory `daemon/` unless a row says otherwise; WSL rows run Ubuntu-24.04 over the same
worktree as `wsl -d Ubuntu-24.04 -u accept`. The two long suites never ran concurrently with each
other or with a measurement harness. Rows marked "final tree" are the binding evidence; the
pre-review rows are recorded because the reviewer-driven edits (guard, pin arms, carrier test)
changed test files after them.

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | no file listed |
| `git diff --check` (git root) | 0 | no whitespace error |
| `go vet ./...` (`daemon/`) | 0 | empty log (0 lines, `<scratch>/vet.log`) |
| `GOOS=linux go build ./...`, `GOOS=windows`, `GOOS=darwin` (`daemon/`) | 0 | 3/3 `ok` |
| `go test -count=1 ./internal/execution/... ./internal/platform/...` (Windows; pre-review tree) | 0 | execution 20.864s, platform 14.592s |
| `go test -count=1 ./internal/app/...` (Windows; pre-review tree) | 0 | app 373.651s, apptest 12.224s |
| focused pin family, WSL (`-run 'TestLinuxSupervisorLiveOwnerControlCloseInProcessStop' -count=5`) | 0 | 2.055s, all four arms green in every repetition |
| focused carrier test, WSL (`-run TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite`) | 0 | green |
| `go test -count=1 ./internal/execution/... ./internal/platform/... ./internal/app/...` (Windows, final tree) | see the re-run row | first final-tree run failed one subtest — the pre-existing documented flake below — and the focused revalidation passed 3/3 |
| `go test -count=1 ./internal/execution/... ./internal/platform/... ./internal/app/...` (Windows, final tree, re-run) | 0 | execution 14.903s, platform 11.127s, app 505.849s, apptest 10.165s |
| WSL `go test -count=1 -race ./internal/platform/... ./internal/app/... ./internal/execution/...` | 0 | platform 63.285s, app 830.848s, apptest 3.618s, execution 22.999s; no race report |
| `python3 scripts/ci/test-omo-control-native-evidence.py` (git root) | 0 | `Ran 15 tests in 15.821s` / `OK` |

Flake note: the first final-tree Windows row failed exactly one subtest,
`TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline`
(`omo_attach_ack_test.go:629: attachment requests=[], want [GET PUT] … context deadline exceeded`,
`native_calls=[start details open details close wait]`), the pre-existing flake already tracked in
`docs/TODO.md` ("Daemon: the `admission_deadline` attach-ack subtest flakes on its wall-clock
deadline (PR #217 review)") whose recorded first failure shape is the same; the arm sets a 2 s
wall-clock admission deadline and requires a loopback GET+PUT inside it, so it reddens when the host
starves the suite (the failing row ran ~30 % slower than the pre-review row: 486 s vs 373 s for the
app package). Focused revalidation on the final tree: `-count=3` of that test → exit 0 (77.458s).

## 8. Scope

```
$ git diff --name-only
daemon/internal/app/apptest/faultstore.go
daemon/internal/app/process_recorder.go
daemon/internal/app/process_recorder_test.go
daemon/internal/execution/process_launcher_linux.go
daemon/internal/execution/recorder.go
daemon/internal/execution/recorder_test.go
daemon/internal/platform/containment_supervisor_linux_test.go
daemon/internal/platform/supervisor_linux_client.go
docs/TODO.md
docs/archive/CLOSED-TODO.md

$ git status --porcelain -uall --no-renames
 M daemon/internal/app/apptest/faultstore.go
 M daemon/internal/app/process_recorder.go
 M daemon/internal/app/process_recorder_test.go
 M daemon/internal/execution/process_launcher_linux.go
 M daemon/internal/execution/recorder.go
 M daemon/internal/execution/recorder_test.go
 M daemon/internal/platform/containment_supervisor_linux_test.go
 M daemon/internal/platform/supervisor_linux_client.go
 M docs/TODO.md
 M docs/archive/CLOSED-TODO.md
?? daemon/internal/execution/process_launcher_linux_test.go
?? docs/evidence/rf-w4-22-response-loss-seal-consumer-2026-11-23.md
?? docs/plans/2026-11-23-rf-w4-22-response-loss-seal-consumer-plan.md

$ git diff --numstat
8	0	daemon/internal/app/apptest/faultstore.go
13	0	daemon/internal/app/process_recorder.go
49	0	daemon/internal/app/process_recorder_test.go
11	6	daemon/internal/execution/process_launcher_linux.go
16	0	daemon/internal/execution/recorder.go
16	2	daemon/internal/execution/recorder_test.go
318	55	daemon/internal/platform/containment_supervisor_linux_test.go
39	11	daemon/internal/platform/supervisor_linux_client.go
6	20	docs/TODO.md
65	0	docs/archive/CLOSED-TODO.md
```

New files: `daemon/internal/execution/process_launcher_linux_test.go` 46 lines, this record, and
the plan (140 lines). `docs/TODO.md` loses 20 lines because the closed bullet moves here; the
remaining modified files are the port member and its enumerations, the launcher spec extraction, the
platform write and its pin family, and the apptest hook. Every path is in the contract's owned set
or is one of the re-derivations recorded in §9; no helper, state, harness, Windows-client, control
plane or harness-adapter file is touched, and no path is under `daemon/internal/harness/opencode/`.
R9: `daemon/internal/app/process_recorder.go` is the only `SOURCE_PATHS` file among them and it
gains code (13 added, 0 removed), so no listed path loses code.

Result-bearing hygiene, on the final tree: a case-insensitive scan of every added line plus the
untracked files for a workstation profile-path fragment finds no match (the only whole-file hits are
pre-existing text: `localHandleID` matching the name fragment, and a historic `C:/Users/...` quote
inside `CLOSED-TODO.md`); a byte scan for the backslash-u escape pair finds none in any added line
or untracked file (pre-existing historic entries elsewhere in `CLOSED-TODO.md` carry such escapes).

## 9. As-built deviations from the plan/contract

- The contract's helper-side "`:940-951` comment refresh" has no target at HEAD: the helper loop and
  `enterRecovery` moved to `daemon/internal/platform/supervisor_session.go:375-383`, and the long
  #185 comment that named the deferred amendment was shortened by the 2026-11-22 comment-policy
  sweep before this slice. The surviving two lines describe the latch's own invariant (a live-owner
  stop persists its fence over the dead transport) and stayed true and untouched;
  `daemon/internal/platform/supervisor_linux_helper.go` has no edit.
- The platform test file carries the pin's failing-seal-write arm as a second test
  (`…StopSealWriteFailureFailsClosed`) sharing a scenario helper, because clause 1 requires the
  seal-write-failure path pinned and the file was the only owned home; the contract's "the pin only"
  is read as "do not touch the file's other tests" (none was touched).
- The pin is four arms over one scenario helper (the rewritten pin, the failing seal write, the
  absent write, and a resumed helper's non-2 terminal seal) plus the new Linux-only
  `daemon/internal/execution/process_launcher_linux_test.go`. The last three arms and the launcher
  spec extraction (`linuxSupervisorStartSpec`) answer the independent adversarial review's control
  findings: a constant sequence and a deleted launch carrier both survived every other test.
- Two test-enumeration consequences of the eleventh port member are in the owned set by necessity:
  `daemon/internal/execution/recorder_test.go` (capability literals, the fail-closed table, the
  capability-to-method map, the `capabilityRecorder` double) and
  `daemon/internal/app/process_recorder_test.go` (the capability literal plus a new test that pins
  the bound key, the exact authority/sequence and the one-exact-replay against the store seam).
- `daemon/internal/app/apptest/faultstore.go` gains the `OnRecordSupervisorRecoveryScanSeal` hook so
  the app-level recorder test can observe the new write; the double is the established seam for the
  app's store writes and a missing hook would make the new write untestable at that level.
- The #185 closure paragraph in `docs/archive/CLOSED-TODO.md` is left as written history (it
  describes the pre-slice interim state and names the old pin); the slice's own closure entry in the
  same file records the rename and the closure.
- `docs/TODO.md`'s W6-04 entry gains three lines recording that the eleventh write is deliberately
  not mirrored, so a recorder-less OpenCode launch keeps today's fail-closed response-loss stop
  until W6-04 lets the adapter carry the recorder.

## 10. Review records

Two independent read-only reviews ran in parallel on the working tree (no builds, tests, vet or
linters; both reported that limit themselves): a durable-transition review by the `consultant`
agent and an adversarial review by the `reviewer` agent, both given the contract, this plan, the
diff and an attack list, and both forbidden to write. Route note: the owner's routing exception
asks an independent audit to include `ghc/gpt-6.2-sol` at xhigh alongside a
`deepseek-v4.1-flash-fast` auditor; this runtime exposes no per-task model parameter, so both
reviews ran on the configured agents for those roles. Recorded as a limit, not silently
substituted.

Durable-transition review (verdict: keeps the invariants at the level that matters; no P0/P1):
confirmed the store's rule chain, the authority value (it proved the equality of the platform
authority with the committed `handoff.ToSupervisor()` and noted the preceding scan-event CAS
enforces the same equality at runtime before the new write), exactly one platform-level call per
attempt with the app's one-replay beneath it, the fail-closed surface, and the crash walk. Accepted
and applied: F1 (the new write was the only launch-path CAS outside the store-lifetime guard → the
app callback now holds `storeLifetimeMu.RLock()` and refuses with `errDaemonStoreClosing`, the
implementation function itself untouched), F2 (the owned TODO closure → §9/`docs/archive/CLOSED-TODO.md`
and the section's "two paths" wording corrected), F3 (an arm with the write absent → the new
`…WithoutTheSealWriteFailsClosed`), F4 (the replay wording → §5 corrected, plus the failure arm's
second `Terminate`). Recorded, not fixed (out of scope, fail-closed): F6 — if the seal lands and a
later durable step in the same attempt fails, the unproven mark blocks restart recovery until the
explicit clear path; the class pre-exists at base (mirror-sealed + unproven) and the delta is only
that the helper seal is durable too. F5 (a real launch's `ContainmentAuthority()` never fed to the
real store CAS in a test) is a recorded evidence-depth limit; §11.

Adversarial review (verdict: fix first; no redesign): confirmed one new write at one position, the
preserved error surface and the byte-equivalent live path, the absent-write chain's fail-closed
result, no second carriage of the write, and no consumer assuming exactly ten capabilities.
Accepted and applied: its F1 (the production carrier line untested → the `linuxSupervisorStartSpec`
extraction plus `TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite`, mutant m6), F2 (a constant
sequence surviving every test → the resumed-helper arm, mutant m5), F3 (the absent-capability arm:
the same gap the durable-transition review's F3 found; one arm covers both), F4 (the owned TODO
closure, above), F5 (a pin comment naming the infeasible order → reworded). Recorded, not fixed:
its F6 (the test double omits store preconditions; the fresh-advance branch has no real-store
coverage — the opt-in witness lane would not exercise it either, because the witness waits for both
scanners to be durably sealed before its in-process stop, so the recovery write there is the
store's idempotent no-op branch) and its F7 (deleting `mirror.assertDescendantScanSealedLocally()`
inside `sealMirrorObservation` survives the pin; pre-existing defense-in-depth with a sibling at
`supervisor_linux_client.go:1484`, not introduced here). Its F8 scope note matches §9.

The two reports agreed on the amendment (§3), the failure semantics (§3), the crash walk (§5) and
the residual classes (§11); no finding was rejected as invalid.

## 11. Residual risks and known limits

- The platform pin uses a journal model narrower than `state.Store`; it models the recovery-seal
  rules that matter here (authority CAS via `handoff.ToSupervisor()`, prior durable mirror seal,
  exact `Completed+1`, idempotent replay, no advance on a failed write) and does not model the
  store's `ContainmentUnproven` flag or the journal write barrier. The store's own suite pins those.
- A response-loss stop whose helper response is refused by another store rule (for example a
  stale authority binding) is now reported through the same fail-closed wrapper as before; no path
  reaches the receipt without the durable all-sealed assertion passing.
- Evidence depth accepted by both reviews: no test feeds a live launch's `ContainmentAuthority()`
  into the real store CAS (the store suite uses the journal's own committed authority; the pin's
  model derives the expected value from the bind handoff via `ToSupervisor()`), and the
  fresh-advance branch is exercised against the pin's double rather than the real store. The
  runtime guarantee rests on the preceding `assert_clean`/`seal` scan-update CAS, which enforces
  the same value equality field by field before the new write, so a divergence is loud and
  fail-closed.
- The resumed-helper arm measures the durable helper seal as "whatever the helper retired + 1"
  (`completed == sealed >= 3`), not a literal, because the resumed monitor's 250 ms interval makes
  the exact count timing-dependent; the model's exact-next rule and the arm's tail equality still
  bind the recorded sequence to the helper's attested one, and the constant-sequence mutant dies on
  it.
- Deleting `mirror.assertDescendantScanSealedLocally()` inside `sealMirrorObservation` survives the
  pin (the local state is sealed in every pinned scenario, and
  `sealDescendantScanAfterMonitorStop` re-checks the same invariants under its own lock). The line
  is pre-existing defense-in-depth with a sibling at `supervisor_linux_client.go:1484`; this slice
  neither introduced nor weakened it, and pinning it is a separate coverage increment.
- If the recovery seal lands and a later durable step of the same attempt fails, the attempt's
  containment-unproven mark makes a restarted daemon refuse the run until the explicit
  `ClearContainmentUnproven` path; that class pre-exists at base for mirror-sealed + unproven
  journals, and changing it would be an unauthorized recovery-path change here.
- The Windows supervisor client is untouched (contract out of scope), and `supervisor_linux_client.go`
  is not split (R14).
- The lease-expiry sticky-refusal arm and `sealDescendantScanAfterMonitorStop`'s retry semantics
  stay open in `docs/TODO.md` (contract out of scope).
- The pin uses the `max` builtin and `slices.Clone`/`slices.Equal`; both need the Go toolchain the
  repository already pins (go1.27).

## 12. Apply-stage amendments (2026-11-23)

The simplify stage (`…-si/answer.md`, report-only) found no structural simplification and returned
five P3 findings (no P0-P2). The apply stage applied all five on top of the committed slice
`59f13815` as working-tree edits; it committed, pushed and reverted nothing, and it changed no file
outside the accepted findings.

- Finding 1: this record's §8 plan line count corrected 128 → 140 (the plan file is 140 lines; its
  §1 sha256 row already matched that file, so no other number moved).
- Finding 2: `recoverResponseLossStop`'s three freeze branches share one local `freezeUnproven`
  closure; the containment-unproven callback, the wrapper bytes
  (`response-loss mirror freeze: %w`) and the branch order are unchanged, and the
  `response-loss mirror proof` branch keeps its own text.
- Finding 3: `recordRecoveryScanSeal` now refuses an unavailable supervisor with the sibling's
  `errors.New("Linux supervisor is unavailable")` instead of folding it into the absent-write
  `return nil` (the finding's fail-closed option). The clause is unreachable at HEAD (the sole
  caller is nil-guarded), so no pinned behaviour moves; the absent-write skip, the documented
  fail-closed base path, is unchanged.
- Finding 4: the recovery-seal recorder test drops its dead `options{clock: …}`; the test never
  reaches `daemon.now()`, and `now` stays used by the `persistProcess` closure.
- Finding 5: `TestProcessRecorderDispatchesEveryWriteToItsLegacyStoreMethod` renamed to
  `TestProcessRecorderDispatchesEveryLegacyWriteToItsStoreMethod`; the eleventh port write stays
  pinned by the recovery-seal test and is not folded into the dispatch test.

Working-tree delta vs `59f13815`: `daemon/internal/platform/supervisor_linux_client.go` 13/13,
`daemon/internal/app/process_recorder_test.go` 2/2; the §1 rows for those two files are the refreshed
bytes and the other nine rows still match. Gate re-runs on the applied tree:

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | no file listed |
| `git diff --check` (git root) | 0 | no whitespace error |
| focused recorder tests, Windows (`-run TestProcessRecorder -v ./internal/app/`) | 0 | 2/2 PASS, `app 1.271s` |
| focused pin family, WSL (`-run TestLinuxSupervisorLiveOwnerControlCloseInProcessStop -count=5 ./internal/platform/...`) | 0 | 4 arms × 5 = 20/20 PASS, `platform 2.059s` |
| focused carrier test, WSL (`-run TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite ./internal/execution/...`) | 0 | PASS, `execution 0.015s` |
| `go test -count=1 -timeout 1800s ./internal/execution/... ./internal/platform/... ./internal/app/...` (Windows) | 0 | execution 10.971s, platform 8.245s, app 212.452s, apptest 5.030s |
| `go vet ./...` (`daemon/`) | 0 | empty log |
| `GOOS=linux`, `GOOS=windows`, `GOOS=darwin` `go build ./...` (`daemon/`) | 0 | 3/3 ok |
| WSL `go test -count=1 -race -timeout 3000s ./internal/platform/... ./internal/app/... ./internal/execution/...` | 0 | platform 62.790s, app 588.873s, apptest 3.358s, execution 22.907s; no race report |
| `python3 scripts/ci/test-omo-control-native-evidence.py` (git root) | 0 | `Ran 15 tests in 17.423s` / `OK` |
| apply-stage mutant re-runs m1, m2, m3, m4, m5 (each applied alone to the applied tree, run against the pin family, reverted; m5 is the function-body constant-sequence form, the call-site form does not build) | 1 | 5/5 red with the recorded first lines: m1 three arms (3541/3595/3691), m2 the same three ("the assertion refuses before the write"), m3 three arms including the recorded order tail, m4 the failure arm only, m5 the resumed-helper arm only; m6 not re-run — its subject (`linuxSupervisorStartSpec`) is outside the apply delta and the carrier test re-ran green |
| restored-tree pin family re-run after the reverts (`-run TestLinuxSupervisorLiveOwnerControlCloseInProcessStop ./internal/platform/...`) | 0 | `ok platform 0.429s` |
| `sha256sum -c` (§1, 12 rows, after the reverts) | 0 | 12/12 OK |
| added-line DLP scan (`users/`, backslash-u) | 0 | no match |

§7's table binds the implement-stage committed tree; the rows above re-validate the applied tree.
No measurement harness applies: this slice carries no consumer metric beyond §4's durable-write
order table, which the pin's ordered-tail assertion re-verifies (green). The two long suites ran
serialized, Windows first.
