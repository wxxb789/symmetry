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
| `daemon/internal/platform/containment_supervisor_linux_test.go` | `3dfb41eb0e2ee10643d5cabc99d1e9e676d44d7a3abf714c34a9e7c368a94e05` |
| `daemon/internal/execution/recorder.go` | `10833a31fbfa02f4b36ef2f8783a9cea1c4d158caf2e2621fc115a9f0bb90114` |
| `daemon/internal/execution/recorder_test.go` | `b5fb2866b8c8afe15229b8d2493ac4934d190aaf4401e05f4277e91181811e9f` |
| `daemon/internal/execution/process_launcher_linux.go` | `77ecbf4c8453f052446e824a06dfb809771039563def8e77aff8fc56a601a540` |
| `daemon/internal/execution/process_launcher_linux_test.go` (new) | `17cfd115e3c971229c3168658455682a68101b905de997bb74c46f2aa1674faa` |
| `daemon/internal/app/process_recorder.go` | `b2ab5e7d6ca781ea4bc4f9c98d2a5bc6d77d616fbe726b27a4ae179e580374e3` |
| `daemon/internal/app/process_recorder_test.go` | `824cc4828443b513513c63b359cb1c4a3b016e7f5ca48819b08c8d808acce982` |
| `daemon/internal/app/apptest/faultstore.go` | `aeb257808710dab11486154c9817671822376c0ebe3a0bccc48be783846742a2` |
| `docs/TODO.md` | `bb383285cdf0eb76fd3f43cec67a1f88e0256e4a5f603bab3414d6388db8da23` |
| `docs/archive/CLOSED-TODO.md` | `11e4320d4fed65a4fe571e271f9af18c11b62b5e82d1ec1de28012e004b4b93c` |
| `docs/plans/2026-11-23-rf-w4-22-response-loss-seal-consumer-plan.md` | `ccc761540b261551a5f0a70afee35aaab3015d6be6b17cd585297cd728a0b9fe` |
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
"write the seal after the receipt/mirror freeze" mutant. Recorded in the plan §3.1 and ratified by
the conductor (2026-11-23, PR #266 dual audit round 1): the as-built position is binding, and the
literal "before the mirror freeze" is superseded as infeasible.

Failure semantics: the write error is returned (the caller keeps its existing
`response-loss mirror freeze: %w` wrapper and fires the containment-unproven callback once), so the
stop stays fail-closed with the same sentinel class, no adopted receipt and no durable advance. The
alternative — swallowing the write error and letting the assertion refuse — was rejected because it
misattributes the cause; for the *absent* capability the byte-identical base refusal is preserved
automatically. Recorded in the plan §3.3 and pinned by the pin's failure arm. Ratified by the
conductor (2026-11-23, PR #266 dual audit round 1): the unchanged wrapper, sentinel pair, single
unproven mark and durable order are the binding reading of "same error text"; the inner cause names
the failed write and no consumer keys on it.

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
| after the recovery seal, before the receipt | mirror `2/2`, helper `2/2`, no receipt | a restarted daemon takes the plain `terminatePersistAuthority` route (`RecoveryHelperScanSealEligible` is false because the helper is sealed) and the receipt write passes `requireLinuxScansSealedForAuthority`; a dead helper with no persisted receipt refuses exactly as at base |
| after the receipt adoption | receipt durable | unchanged base behaviour |

A failed seal write is a different window, not a crash point: the attempt fails in-process and
persists the containment-unproven mark before returning, so the durable state is mirror `2/2`,
helper `1/0`, unproven, no receipt. An in-process retry re-attempts the same endpoint proof and the
store refuses the rule itself (`containment_journal.go:413-415`); the modelled store refuses at that
same recovery-seal precondition, while the real store also gates scan updates on unproven (§11). The
pin's failure arm re-runs `Terminate` with the injected write error cleared and pins that refusal
and the re-attempt (§13, probe F2).

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
the plan (145 lines). `docs/TODO.md` loses 20 lines because the closed bullet moves here; the
remaining modified files are the port member and its enumerations, the launcher spec extraction, the
platform write and its pin family, and the apptest hook. Every path is in the contract's owned set
or is one of the re-derivations recorded in §9; no helper, state, harness, Windows-client, control
plane or harness-adapter file is touched, and no path is under `daemon/internal/harness/opencode/`.
R9: `daemon/internal/app/process_recorder.go` is the only `SOURCE_PATHS` file among them and it
gains code (13 added, 0 removed), so no listed path loses code.

Result-bearing hygiene, on the final tree: scanning the line ranges the branch adds, plus the
untracked files, case-insensitively for the account-name fragment and for path shapes (drive
letters, the Linux mount, scratch and home prefixes) described here in prose finds no host path;
the only whole-file hits are pre-existing text — an identifier that spells the account fragment
inside a longer name, and a historic escaped profile-path quote inside `CLOSED-TODO.md`. §13
records the two self-referential added-line hits the review found at `b3ca0367` and the rewrite
that removed them. A byte scan for the backslash-u escape pair finds none in any added line or
untracked file (pre-existing historic entries elsewhere in `CLOSED-TODO.md` carry such escapes).

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
  rules that matter here (authority CAS via `handoff.ToSupervisor()`, prior durable clean mirror
  seal with no unproven observation, exact `Completed+1`, idempotent replay, no advance on a failed
  write, the assert-update sequence CAS at `state/supervisor_handoff_journal.go:269`/`:277`, and the
  containment-unproven refusal at `state/containment_journal.go:412-415` that the fix stage
  re-pinned). It does not model the journal write barrier or the store's broader unproven gate on
  scan updates (`recordSupervisorScanUpdate`'s unproven gate, `:165-167`). The store's own suite pins
  the mirror-seal precondition and the idempotent replay; the unproven refusal is pinned by the
  pin's own arms because the store suite never marks a journal unproven before its recovery-seal
  calls (§13, probe F2).
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

- Finding 1: this record's §8 plan line count corrected 128 → 140 (the plan file was 140 lines at
  that revision; the round-1 revision in §14 grew it to 145, and §8 carries the refreshed count).
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
| added-line scan: profile-path shapes (prose) and the backslash-u escape pair | 0 | no host path, no escape pair in the added lines; the raw fragment scan read this record's own two restatements — §13 rewrites both lines and restates the scan in prose |

§7's table binds the implement-stage committed tree; the rows above re-validate the applied tree.
No measurement harness applies: this slice carries no consumer metric beyond §4's durable-write
order table, which the pin's ordered-tail assertion re-verifies (green). The two long suites ran
serialized, Windows first.

## 13. Review-fix stage amendments (2026-11-23, fix r1)

Review report: the review stage's `answer.md` (verdict `MUST_FIX (4)`, findings F1-F5). This stage
changed the pin's journal model, the failure arm's retry setup, the plan's §3.5 numbers and this
record; no production byte, contract clause, existing assertion or durable order moved, and the
conductor committed the reviewed bytes as `a7f4cbbb` (the real fix-revision head) and pushed them.
The model now carries the store's unproven precondition and both assert-CAS rules,
so the pin's durable-evidence oracle matches `state` rather than approximating it.

| Finding | Disposition | Change and probe |
| --- | --- | --- |
| F1 (P2) | Fixed | Two added lines spelled the account-name fragment (the §8 hygiene sentence and the §12 scan row), so the contract's raw fragment scan over the committed range read 2 added hits while the record claimed 0. Both are rewritten in prose; the §12 row now states the path-shape scan it measured, and §8 names the probe. Fix-revision probes: the added lines of `git diff origin/main` (filtered for `^+`) hold zero account-name fragment matches, zero path shapes (drive letters and the Linux mount, scratch, home, root, private and var prefixes) and zero backslash-u pairs; `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record exits 0 (it exited 1 before the rewrite); `python scripts/ci/check-record-cleanliness.py --base cafd5488 --head b3ca0367` still exits 1 because it reads the committed blob of `b3ca0367`, where this uncommitted rewrite is not visible. |
| F2 (P3) | Fixed | The failure arm's retry was refused by the injected write error, not by the durable unproven mark its comment and §11 claimed, and the model had no unproven state, while the real store refuses an unproven journal (`state/containment_journal.go:412-415`) and its own suite never covers that refusal (probe: `grep -rn RecordSupervisorRecoveryScanSeal internal/state/*_test.go` returns only `supervisor_handoff_test.go:1156`/`:1176`/`:1191`, none reached with an unproven journal). The double now records the mark (`markContainmentUnproven`, called from the pin's unproven callback exactly where production persists it) and `recordRecoveryScanSeal` refuses on it with the store's precondition; the retry clears the injected error first, so the refusal is the mark's. Red-first probe: with only the clearing line added and the model unchanged, the retry adopted the receipt (`response-loss stop retry = <nil>`); with the fix, the four arms and the new arm pass under `-race`. |
| F3 (P3) | Fixed; the review's mutant claim did not reproduce | The double's assert branches did not model the store's `update.Sequence` CAS (`state/supervisor_handoff_journal.go:269`/`:277`). The fix adds the CAS to both branches plus a direct arm (`TestLinuxSupervisorScanJournalModelRefusesStaleAssertSequences`: the owner's durable sequence is accepted, the stale one refused). Probe: a mutant of `assertDurableScanStateSealed` sending `sequence - 1` does not survive at `b3ca0367` — it reddens `RecordsSeal` and `RecordsTheHelperReturnedSequence` at the ordered-tail assertion (`mirror:assert_all_sealed:1` against the wanted `:2`), so the review's "all four arms stay green" is refuted; the CAS is applied as model hardening with two killing mutants (m9, m10) rather than as the only oracle. |
| F4 (P3) | Fixed | Plan §3.5 wrote mirror `1/1` and the tail `mirror:seal:1`, `recovery-scan-seal:2`, `mirror:assert_all_sealed:1`; the measured durable state is mirror `2/2` with the `:2` tail (§4's table; the response-loss request opens a second mirror sequence before the latch). §3.5 now carries the measured values and notes that the pin reads the mirror sequence from the model. Probe: the pin's ordered-tail assertion at the fix revision (green) plus §4's measured table. |
| F5 (P3, advisory) | Recorded, no code change (as its own disposition says); conductor-ratified | The inner cause of a seal-write failure differs from base's assertion-refusal text; the wrapper, the sentinel pair, the single unproven mark and the durable order are unchanged, and no consumer keys on the inner text. The finding defers to conductor ratification rather than asking for an edit; the rejected alternative (revert to the assertion refusal) would misattribute the cause. Ratified by the conductor (2026-11-23, PR #266 dual audit round 1): the inner-cause difference is accepted, and the unchanged wrapper, sentinel pair, single mark and durable order are the binding surface. |

New mutants at the fix revision (each applied alone to the fixed tree, run against the pin family
and the new arm, then reverted; the restored tree is byte-identical in the modified files):

| Mutant | Edit | Killer (exit 1) |
| --- | --- | --- |
| m7 | drop `model.markContainmentUnproven()` from the pin's unproven callback | failure arm: `response-loss stop retry = <nil>, want the fail-closed unproven class` |
| m8 | drop `model.unproven ||` from the model's recovery-seal precondition | same failure-arm line |
| m9 | drop `owner.completed != update.Sequence ||` from the assert-clean branch (with its now-unused read) | new arm: `assert_clean accepted a stale sequence` |
| m10 | drop `owner.sealed != update.Sequence ||` from the assert-all-sealed branch (with its read) | new arm: `assert_all_sealed accepted a stale sequence` |

Re-run of the implement/apply mutants at the fix revision (the pin family changed, so all six were
re-run rather than inherited from §6/§12): m1 red on the success, failure and resumed arms (`Linux
containment scan seals are unresolved` against `want a completed stop` / `want the freeze wrapper
unchanged`); m2 the same three lines; m3 the same three plus the failure arm's tail
`recovery-scan-seal:3` against `:2`; m4 red on the failure arm only; m5 red on the resumed-helper
arm only; m6 red on `TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite` (`start spec dropped a
recorder write`). Restored tree: the focused family, the new arm and the carrier test re-ran green,
and `git status` showed only the one subject test file modified.

§1 rows refreshed: `containment_supervisor_linux_test.go` and the plan moved (the stage's only
subject edits); the other ten rows still match, so `sha256sum -c` over §1 reads 12/12 OK.

Fix-revision gates (working directory `daemon/` unless a row says otherwise; WSL rows run
Ubuntu-24.04 over the same worktree as `wsl -d Ubuntu-24.04 -u accept`; the stage's subject delta is
the platform test file, the plan and this record, so the full `app` suites stay conductor-owned and
were not re-run):

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | no file listed |
| `git diff --check` (git root) | 0 | no whitespace error |
| `go vet ./...` (`daemon/`) | 0 | empty log |
| `GOOS=linux`/`windows`/`darwin` `go build ./...` (`daemon/`) | 0 | 3/3 ok |
| WSL `go test -count=1 -race -timeout 3000s ./internal/platform/... ./internal/execution/...` | 0 | platform 62.626s, execution 22.512s; no race report |
| focused family WSL: `-race -run 'TestLinuxSupervisorLiveOwnerControlCloseInProcessStop\|TestLinuxSupervisorScanJournalModelRefusesStaleAssertSequences\|TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite' ./internal/platform/... ./internal/execution/...` | 0 | 4 arms + the new model arm + the carrier test; platform 6.019s, execution 1.215s |
| load run WSL: `GOMAXPROCS=8 go test -count=200 -race -timeout 3000s -run 'TestLinuxSupervisorLiveOwnerControlCloseInProcessStop\|TestLinuxSupervisorScanJournalModelRefusesStaleAssertSequences' ./internal/platform/...` | 0 (`tail` end of a pipe; the run's own verdict is its summary line) | `ok platform 930.620s`; 200 repetitions of the four arms and the model arm, no failure line |
| Windows `go test -count=1 ./internal/platform/... ./internal/execution/...` | 0 | platform 7.754s, execution 15.585s |
| `python scripts/ci/check-citation-existence.py --base cafd5488 --head b3ca0367` (git root) | 0 | 527 path citations, 1 changed document (3 pre-existing ignore-rule notes) |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record and the plan (git root) | 0 | no host absolute path in either (this record read exit 1 before the rewrite) |
| `python scripts/ci/check-record-cleanliness.py --base cafd5488 --head a7f4cbbb` (git root) | 0 | 1 changed record passes the root-only check; the fix revision is committed at its real head, so the gate reads the final bytes (the same command against `b3ca0367` read exit 1 only because the rewrite was then uncommitted) |
| added-line scans: account-name fragment, path shapes, backslash-u pair (`git diff origin/main`, `^+` filter) | 0 | 0 hits each |
| `sha256sum -c` (§1, 12 rows) | 0 | 12/12 OK |
| platform package test functions (`grep -h '^func Test' internal/platform/*_test.go \| wc -l`): HEAD `b3ca0367` vs working tree | - | 332 → 333 (the new model arm); the changed file 58 → 59 |

## 14. Conductor rulings and revisions (2026-11-23, PR #266 dual audit rounds 1 and 2)

Conductor ruling (dual audit round 1: Sol reviewer + DeepSeek auditor, reconciled); the fix list is
records and comments only, no production code change:

- Ratified: the write position — the helper owner-loss seal is written after the mirror's durable
  seal and before the all-sealed assertion and the receipt (§3, plan §3.1); Design 2's literal
  "before the mirror freeze" is superseded as infeasible.
- Ratified: review F5 — on a seal-write failure the inner cause differs from base's assertion
  refusal, while the wrapper, sentinel pair, single unproven mark and durable order are unchanged
  (§3, §13, plan §3.3).
- Seven added test intent comments stripped from
  `daemon/internal/platform/containment_supervisor_linux_test.go`; the four hazard-why comments
  (resumed monitor interval, endpoint replay safety, post-failure unproven refusal, endpoint seal
  availability) stay.
- §5 separates the crash-after-seal window from the failed-write/unproven retry paragraph; §13
  states the fix revision committed at its real head and the cleanliness row at that head; plan §2
  names `recoveryScanSealWrite`; §8 carries the plan's refreshed line count.
- No action: the Codex bot marker (usage-limit failed run, 0 threads); #265 landed with the same
  marker.

Round-1 verification on the final tree (git root unless a row says otherwise; the WSL row runs
Ubuntu-24.04 over the same worktree as `wsl -d Ubuntu-24.04 -u accept`):

| Check | Exit | Output |
| --- | --- | --- |
| `gofmt -l daemon/internal/platform` | 0 | no file listed |
| WSL `-race -count=1 -run 'TestLinuxSupervisorLiveOwnerControlCloseInProcessStop\|TestLinuxSupervisorScanJournalModelRefusesStaleAssertSequences\|TestLinuxSupervisorStartSpecCarriesEveryLaunchWrite' ./internal/platform/... ./internal/execution/...` (working directory `daemon/`) | 0 | 4 arms + the model arm + the carrier test; `ok platform 5.707s`, `ok execution 1.110s` (go1.27.0) |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record and on the plan (one FILE per run) | 0 | no host absolute path in either |
| `git diff --check` | 0 | no whitespace error |
| `python scripts/ci/check-record-cleanliness.py --base cafd5488 --head a7f4cbbb` | 0 | `check-record-cleanliness: 1 changed record(s) pass the root-only check` |
| `sha256sum -c` (§1, 12 rows) | 0 | 12/12 OK |
| `git diff --name-only` vs `a7f4cbbb` | - | only the platform test file, the plan and this record; the test file's 14 changed lines are all removals (comments), so every production Go byte is unchanged |
| added-line scans over this revision's diff (`git diff HEAD`, `^+` filter): account-name fragment, path shapes (drive letters and the Linux mount, scratch, home, root, private and var prefixes), backslash-u escape pair | 0 | 0 hits each |

§1 rows for `containment_supervisor_linux_test.go` and the plan are refreshed from the final bytes;
the plan is now 145 lines (was 140), the test file 4150 (14 comment lines removed), and the platform
test-function counts still read 333 total / 59 in the changed file. The conductor commits this
round-1 revision as the branch head after `a7f4cbbb`.

### Round 2: both auditors ship; two P3 records items

The round-2 conductor ruling (2026-11-23, PR #266) closed with both auditors shipping and two
records items, both applied in this revision:

- The rf-w4-22 closure entry in `docs/archive/CLOSED-TODO.md` moved back to the end of the
  `## Comment-policy sweep: debt the deleted comments recorded (2026-11-22)` section, ahead of the
  `## Codex adapter giant-file split (rf-w5-16, 2026-11-23)` heading. The entry's bytes are
  unchanged — only its position moved, proved by `git diff --stat` reading 15 insertions and 15
  deletions over the single file and by the sorted line lists of `HEAD` and the working tree being
  identical (4645 lines each).
- §1's `docs/TODO.md` and `docs/archive/CLOSED-TODO.md` rows recomputed from the final bytes. The
  merge named: `f037f918` + main `126b435e` → `b25fe1f4` — bringing #265, whose changes are why
  the pre-merge `f037f918` rows no longer matched — and the conductor commits this round-2 records
  revision as the records commit on top of that merge; every round-2 row below reads the tree this
  records commit carries. The PR body's `sha256sum -c … 12/12` row is rebound to this final head
  and reproduces.

Round-2 verification (git root; no code file changes; the changed docs are this record and
`docs/archive/CLOSED-TODO.md`):

| Check | Exit | Output |
| --- | --- | --- |
| `sha256sum -c` (§1, 12 rows, from a checklist of §1's refreshed rows) | 0 | 12/12 OK |
| `git diff --stat` over `docs/archive/CLOSED-TODO.md`, plus the sorted line-multiset comparison (`git show HEAD:… \| sort` vs `sort …`) | 0 | 15 insertions, 15 deletions; the two sorted lists byte-identical |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record | 0 | no host absolute path |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on `docs/archive/CLOSED-TODO.md` | 1 | pre-existing and unchanged: the archive quotes the sanitizer's own specimen paths (first refusal line `:373`). The revision's added lines carry no host path — the same tool over the added lines (`git diff -U0`, `^+` filter, fed via stdin) reads exit 0; the committed-record gate scopes records to `docs/evaluations` + `docs/evidence` |
| `git diff --check` | 0 | no whitespace error |
| `git diff --name-only` | - | exactly the two docs above; no code file |
