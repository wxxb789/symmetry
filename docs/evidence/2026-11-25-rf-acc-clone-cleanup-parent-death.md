# rf-acc-clone-cleanup-parent-death — evidence record

Contract: `goal-rf-acc-clone-cleanup-parent-death.md` (GDD state root, external to the repository).
Base revision: `6b2d4f8f195a2b2011d1fff4962d1e13acc74efd` (`origin/main` after `git fetch`; the
dispatch head). Branch: `refactor/rf-acc-clone-cleanup-parent-death`. Plan:
`docs/plans/2026-11-25-rf-acc-clone-cleanup-parent-death-plan.md` (its §8 is the printer-window
amendment, §9 the review round).

## 1. Subject bytes

| File | Stage-1b sha256 | Stage-2 / final sha256 | Fix-round sha256 |
| --- | --- | --- | --- |
| `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go` | `133c5b7a69a379f354a9b33fe1a65829b27b7f6b1157c5641f3915fd1f4ed1a9` | `ab3c48ee8314f6028a65dc1793fd8457402749fd7afe9443889b8229f117a530` | `8a8fe30c0cbbce126a10e3adcebd0066b73d0c18f0892877947cc18884f8a622` |
| `daemon/native/linux-gate/protocol/clone_cleanup_linux.go` | `4fb65bb9874b1dfcd1beb3a16e18b4ff11fc3bb9a6910baa90c46c8e68d48dd7` (unchanged from base) | unchanged | unchanged |
| `daemon/cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | `709f7c18ef4174eb8e4b4e4b4c2e20975e064bb2c03409d40f85e473327ac8ab` (unchanged) | unchanged | unchanged |
| `daemon/internal/launchercore/main_linux_amd64.go` | `9a6d2cc67fc7ca52c5ec6400f59e0d3b6e2075f687241568f19932fc91e02e2c` (unchanged) | unchanged | unchanged |
| `daemon/cmd/symmetry-linux-launcher/main.go` | `05ce544b37f1094ab16beeab01a154a72ce718b9a4bbf8d7fb5bbefd41526a7e` (unchanged) | unchanged | unchanged |
| `docs/plans/2026-11-25-rf-acc-clone-cleanup-parent-death-plan.md` | `e47e4b3e…` (pre-§9) | `47418b0f8ddfff48ef8c042c2ffd628ebe5c96a263cad53da7ca8215bba0871a` | `8b3de2ae0e9b4a2960a88656870be66b109193aa6c69d78bcdbcf44e31218540` |

Scratch-only files (branch-only, deleted with the branch): `.github/workflows/scratch-rf-acc-clone-cleanup-parent-death.yml`, `scripts/ci/scratch-gh-shim` (sha256 `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`).

## 2. The changes

Stage 1 (test-only diagnostics; no product bytes):
- `PR_SET_CHILD_SUBREAPER` on the test process before the worker spawn, reset in cleanup, so the
  orphaned root deleter reparents to the test and `waitid(P_PIDFD)` can recover its exit.
- caller `/proc/<pid>/status` `SigCgt`/`SigBlk`/`SigIgn` snapshot (immediately after the worker
  spawn) through `parseStatusSignalMasks`.
- caller reap decoded through `cleanupWaitStatusCodes`; root deleter reaped through
  `waitidCleanupPidfdExit` (existing 10 s pidfd bound + `waitid(P_PIDFD)` siginfo).
- three `t.Logf` lines, closed values only; the unchanged `if waitErr == nil` fatal followed.
- probe tests `TestCleanupWaitStatusCodesClassifiesReapedChildren`,
  `TestWaitidCleanupPidfdExitReportsReapedChildren`, `TestParseStatusSignalMasksGoldenText`.

Stage 1b (after run 1): `t.Parallel()` after the skip guard — the printer-window amendment (§8 of
the plan), reverted in stage 2 after the review round (§10).

Stage 2 (after the named cause; run 3's head `0e0b5486`): the `waitErr == nil` predicate replaced
by the exact reaped status (`!signaled || signalCode != SIGKILL`, original message text kept plus
the closed values), the reap-error guard kept, and the root deleter asserted through
`si_signo=SIGCHLD && si_code=CLD_KILLED && si_status=SIGKILL` instead of readiness alone. Review
hardening: `waitForPIDFDExit` readiness requires `count > 0` (EINTR `-1` is not readiness) with a
≥1 ms poll floor and a wrapped non-EINTR error; the mask snapshot moved out of the pre-kill window;
the worker-kill cleanup bounds-reaps an adopted root child; probe children register `Kill`+`Wait`
cleanup after `Start`; `t.Parallel()` reverted (base execution order).

## 3. Scratch runs

Branch `scratch/rf-acc-clone-cleanup-parent-death`, throwaway workflow
`.github/workflows/scratch-rf-acc-clone-cleanup-parent-death.yml` (the real workflow minus
`environment:`, with the ratified dummy pins, `GITHUB_EVENT_NAME=workflow_dispatch`, the scratch `gh`
shim, `case_sets=(shared)`), `actionlint` v1.7.12 exit 0, deleted at the end (§3.4).

| run | head | set | first failing check (owner-leg event gate) | closed values observed | artifact / member |
| --- | --- | --- | --- | --- | --- |
| `45595479247` | `cc22c107` | `shared` | `bad=…CloneCleanupParentDeathScratch=run:1,pass:0,skip:0`, `fail_events=4` | root `si_signo=17 si_code=2 si_status=9` (masks/caller lines displaced) | `linux-v2-native-evidence-45595479247-1` / `shared/unprovisioned-owner-refusal.json` sha256 `bf0ab0834340eb0dcae083ea1ef44b7680da0b0b16043ce5bda5a9e3981861b6` |
| `45597242823` | `d64fe094` | `shared` | same `bad=` as run 1, `fail_events=4` | masks + caller `signaled=true signal=9 exit=-1 core=false wait_error=false` + root `si_signo=17 si_code=2 si_status=9` | `linux-v2-native-evidence-45597242823-1` / `shared/unprovisioned-owner-refusal.json` sha256 `36204b5e91b35301832ea67147e8e0b5571fb5cb5a2921842686fb0722787eb1` |
| `45600764607` | `0e0b5486` | `shared` | `bad=TestInstalledLinuxOwnerBoundaryScratch=…; TestInstalledLinuxOwnerCloneCheckoutScratch=…`, `fail_events=3` (this test absent) | caller + root as above; `--- PASS` for this test | `linux-v2-native-evidence-45600764607-1` / `shared/unprovisioned-owner-refusal.json` sha256 `af31e696653318a50151e2196a67c075bc96be7e4c66f6990f1f1125148312db` |

### 3.1 Run 1 — stage-1 dispatch (`45595479247`, head `cc22c107`)

Run 5m7s (job `contracts` 1m38s, job `linux-native-v2-acceptance` 3m25s); `shared` exited 1; the
owner-leg event gate:
`bad=TestInstalledLinuxOwnerBoundaryScratch=run:1,pass:0,skip:0; TestInstalledLinuxOwnerCloneCheckoutScratch=run:1,pass:0,skip:0; TestInstalledLinuxOwnerCloneCleanupParentDeathScratch=run:1,pass:0,skip:0, skipped=[], fail_events=4, invalid_json_lines=0`.

Owner-leg printer lines quoted verbatim from the run log:

```
witness output:     clone_cleanup_linux_test.go:284: root cleanup worker wait status: si_signo=17 si_code=2 si_status=9
witness output:     clone_cleanup_linux_test.go:286: A cleanup caller exited without the requested parent-death signal
witness output: --- FAIL: TestInstalledLinuxOwnerCloneCleanupParentDeathScratch (0.02s)
witness output: === RUN   TestInstalledLinuxOwnerCloneCheckoutScratch
witness output:     clone_integration_linux_test.go:240: one-use B clone RELEASE and durable checkout ACK/native exec: receive same-PID B bootstrap attempt: clone status frame magic or version is invalid
witness output:     clone_integration_linux_test.go:113: stop only descendants in the explicitly named clone scratch leaf: open exact scratch leaf cgroup.kill: bad file descriptor
witness output: --- FAIL: TestInstalledLinuxOwnerCloneCheckoutScratch (0.07s)
witness output: === RUN   TestInstalledLinuxOwnerBoundaryScratch
witness output:     launcher_integration_linux_test.go:439: one-use fixed-B RELEASE and same-PID exec: native exec failed after RELEASE: errno=1
witness output: --- FAIL: TestInstalledLinuxOwnerBoundaryScratch (0.01s)
```

Observations: the fence fires on the caller's death (`si_signo=17 si_code=2 si_status=9` =
SIGCHLD/CLD_KILLED/SIGKILL) and the unchanged fatal fires at its new line `:286` with its message
verbatim. The masks and caller lines are absent: the printer's `lines[-10:]` window starts at the
root line (plan §8). Disposition: run 1 is invalidated as the observation-complete dispatch by that
diagnostics-visibility defect in the new emission shape — not a probe defect, so Q-E's "otherwise"
branch applies (the three-run budget stands; the final-literal requirement falls on the stage-2
bytes); it records the root values and the window arithmetic. The owner leg stops at the same
three-test gate as the burndown's final run.

### 3.2 Run 2 — stage-1 corrected dispatch (`45597242823`, head `d64fe094`)

Run 5m3s (job `contracts` 1m18s, job `linux-native-v2-acceptance` 3m30s); `shared` exited 1; the
owner-leg event gate (byte-identical to run 1's):
`bad=TestInstalledLinuxOwnerBoundaryScratch=run:1,pass:0,skip:0; TestInstalledLinuxOwnerCloneCheckoutScratch=run:1,pass:0,skip:0; TestInstalledLinuxOwnerCloneCleanupParentDeathScratch=run:1,pass:0,skip:0, skipped=[], fail_events=4, invalid_json_lines=0`.

Owner-leg printer lines quoted verbatim from the run log (the three closed-value lines and the
unchanged fatal):

```
witness output: --- FAIL: TestInstalledLinuxOwnerCloneCheckoutScratch (0.05s)
witness output: === RUN   TestInstalledLinuxOwnerBoundaryScratch
witness output:     launcher_integration_linux_test.go:439: one-use fixed-B RELEASE and same-PID exec: native exec failed after RELEASE: errno=1
witness output: --- FAIL: TestInstalledLinuxOwnerBoundaryScratch (0.01s)
witness output: === CONT  TestInstalledLinuxOwnerCloneCleanupParentDeathScratch
witness output:     clone_cleanup_linux_test.go:271: cleanup caller signal masks: sigcgt=fffffffd7fc1feff sigblk=0000000000000000 sigign=0000000000000000
witness output:     clone_cleanup_linux_test.go:278: cleanup caller wait status: signaled=true signal=9 exit=-1 core=false wait_error=false
witness output:     clone_cleanup_linux_test.go:287: root cleanup worker wait status: si_signo=17 si_code=2 si_status=9
witness output:     clone_cleanup_linux_test.go:289: A cleanup caller exited without the requested parent-death signal
witness output: --- FAIL: TestInstalledLinuxOwnerCloneCleanupParentDeathScratch (0.03s)
```

**Observed cause: H1** (the predicate's channel). The caller was signaled by the requested SIGKILL
(`signaled=true signal=9 exit=-1 core=false wait_error=false`; `sigblk=0 sigign=0`), and the root
deleter died from the armed parent-death fence (`si_code=2 si_status=9`) while the unchanged fatal
fired on the nil wait error. The leg still stops at the same check with its message verbatim
(`clone_cleanup_linux_test.go:289` in the stage-1b tree — the pre-change address is `:259`; the line
moved only because the diagnostics add lines above it).

### 3.3 Run 3 — stage-2 dispatch (binding; `45600764607`, head `0e0b5486`)

Run 5m34s (job `contracts` 1m42s, job `linux-native-v2-acceptance` 3m47s); run conclusion
`failure` (the `shared` set's sibling stops, out of this goal's failure control); the case set
exited 1. The owner-leg event gate, verbatim:

```
Go native acceptance event gate failed: bad=TestInstalledLinuxOwnerBoundaryScratch=run:1,pass:0,skip:0; TestInstalledLinuxOwnerCloneCheckoutScratch=run:1,pass:0,skip:0, skipped=[], fail_events=3, invalid_json_lines=0
```

`TestInstalledLinuxOwnerCloneCleanupParentDeathScratch` is absent from `bad=` (`fail_events` fell
4 → 3). The owner-leg printer window, verbatim:

```
witness output:     clone_cleanup_linux_test.go:302: cleanup caller wait status: signaled=true signal=9 exit=-1 core=false wait_error=false
witness output:     clone_cleanup_linux_test.go:303: root cleanup worker wait status: si_signo=17 si_code=2 si_status=9
witness output: --- PASS: TestInstalledLinuxOwnerCloneCleanupParentDeathScratch (0.02s)
witness output: === RUN   TestInstalledLinuxOwnerCloneCheckoutScratch
witness output:     clone_integration_linux_test.go:240: one-use B clone RELEASE and durable checkout ACK/native exec: receive same-PID B bootstrap attempt: clone status frame magic or version is invalid
witness output:     clone_integration_linux_test.go:113: stop only descendants in the explicitly named clone scratch leaf: open exact scratch leaf cgroup.kill: bad file descriptor
witness output: --- FAIL: TestInstalledLinuxOwnerCloneCheckoutScratch (0.07s)
witness output: === RUN   TestInstalledLinuxOwnerBoundaryScratch
witness output:     launcher_integration_linux_test.go:439: one-use fixed-B RELEASE and same-PID exec: native exec failed after RELEASE: errno=1
witness output: --- FAIL: TestInstalledLinuxOwnerBoundaryScratch (0.01s)
```

Acceptance mapping: the test passes (`--- PASS`, 0.02 s); the shared set's first failing check is
named verbatim (`clone_integration_linux_test.go:240`, then `launcher_integration_linux_test.go:439`),
both owned by `rf-acc-guest-native-exec`; this test's classifying witness lines (caller, root) are
printed. The masks line is displaced in the passing shape (the window arithmetic in plan §8/§9); it
is present verbatim in run 2's log.

Export: `redacted evidence export failed: pi durability witness is unavailable:
ack-loss-replay-checkout_ready.json`, so the run retained its private fixture and uploaded
`linux-v2-native-evidence-45600764607-1` with the one allowlisted member
`shared/unprovisioned-owner-refusal.json` (sha256
`af31e696653318a50151e2196a67c075bc96be7e4c66f6990f1f1125148312db`; the 693-byte member inside the
607-byte artifact upload).

sha256 equality table — run 3's head vs the delivered tree (per file):

| Path in head `0e0b5486` | head blob sha256 | delivered (worktree) sha256 | equality |
| --- | --- | --- | --- |
| `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go` | `ab3c48ee8314f6028a65dc1793fd8457402749fd7afe9443889b8229f117a530` | `ab3c48ee8314f6028a65dc1793fd8457402749fd7afe9443889b8229f117a530` | equal |
| `.github/workflows/scratch-rf-acc-clone-cleanup-parent-death.yml` | branch-only | absent (deleted with the branch) | n/a |
| `scripts/ci/scratch-gh-shim` | branch-only | absent (deleted with the branch) | n/a |

`git diff --no-renames --name-status 6b2d4f8f 0e0b5486` prints exactly those three paths (one `M`,
two `A`); every other path in the head is byte-identical to the base (verified by construction: the
scratch branch was cut from `6b2d4f8f` and only the three paths were committed). The plan, this
record and `docs/TODO.md` were written after the run (docs only; no subject byte changed after
`0e0b5486`). The fix round (§11) then rewrote three hygiene lines of the test file: §1 carries the
post-fix sha256 and §11 the delta, so the table above binds run 3's head to the pre-fix delivered
bytes and the re-binding run for the delivered bytes is the conductor's Bind worker on the PR head.

### 3.4 Scratch branch deletion

`git push origin --delete scratch/rf-acc-clone-cleanup-parent-death` deleted the branch and
`git ls-remote origin scratch/rf-acc-clone-cleanup-parent-death` printed nothing (exit 0); the local
branch and the throwaway worktree with the workflow/shim were removed.

## 4. Local gates

| Gate | Command (WSL Ubuntu-24.04, `GOTOOLCHAIN=local`, Go 1.27.0; host Go 1.27.4) | Exit | Counts |
| --- | --- | --- | --- |
| gofmt | `(cd daemon && gofmt -l .)` | 0 | 0 files (stage 1, stage 1b, stage 2) |
| vet | `(cd daemon && GOOS=linux go vet ./...)` | 0 | no diagnostics (stage 1, stage 1b, stage 2) |
| package race | `(cd daemon && go test -race -count=1 ./native/linux-gate/protocol/)` | 0 | ok 4.349s (stage 1), ok 8.190s (stage 1b), ok 4.242s (stage 2 pre-review), ok 3.498s (stage 2 final) |
| host vet | `(cd daemon && go vet ./...)` (windows/amd64) | 0 | no diagnostics |
| actionlint | `actionlint .github/workflows/scratch-rf-acc-clone-cleanup-parent-death.yml` (v1.7.12) | 0 | 0 findings (throwaway copy) |
| YAML parse | `yaml.safe_load` on the scratch workflow | 0 | parsed |
| Changed-path clone gate | `python scripts/ci/jscpd-changed.py --repo . --base 6b2d4f8f --head HEAD` (gate commit of the four changed paths in a temporary detached worktree) | 0 | `4 changed file(s), 1739 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new` |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo . --base 6b2d4f8f --head HEAD` (same gate commit) | 0 | `1 changed record(s) pass the root-only check` |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo . --base 6b2d4f8f --head HEAD` (same gate commit) | 0 | `no audited document added or modified; nothing to check` |

The stage-2 gate triad was run on the final `ab3c48ee…` bytes.

## 5. Grep census — readers of the caller's reaped status and renderers of this stop (lesson 75-76)

| Site (base address) | Content | Disposition |
| --- | --- | --- |
| `clone_cleanup_linux_test.go:256-259` (base) | the caller reap and the pass predicate | converted in stage 2 (asserts `signaled && signal == SIGKILL`) |
| `clone_cleanup_linux_test.go:298` (base) → `:353` (head), the worker fatal | `parent test killed A cleanup owner but root call returned` | exempt: the worker is the killed caller; reachable only if the kill never happened |
| `clone_cleanup_linux.go:525` (base) | `command.Process.Wait()` in the `Start` error path, state discarded | exempt: reap-only, no pass condition |
| `clone_cleanup_linux.go:726` (base, `waitReapCleanupChild`) | `waitid(P_PIDFD, WEXITED)` reap of the caller's root child; status validated for the child's exit class | exempt: the child's status, not the caller's; error path only |
| `clone_cleanup_linux.go:776` (base, `reapCleanupChildWithoutPIDFD`) | `waitErr` joined into a terminal error | exempt: error path, not the caller's pass predicate |
| `clone_cleanup_linux_test.go:229` (base) → `:249` (head), the worker-kill cleanup | `Kill` + `Wait` of a caller that never reached its body reap; status discarded | exempt: cleanup path, no pass condition |
| `gate_lifecycle_linux.go:459,1205,1214,1403,1408` (base) | launcher/gate wait paths (`stopLauncher`'s `waitProcessFD` and error-checked reap) | exempt: different producer; no relation to this stop |
| `proc_identity_linux.go:520` (base) | pidfd liveness probe (`waitid(P_PIDFD\|WNOHANG\|WNOWAIT)`, expects `ECHILD`) | exempt: different producer; no relation to this stop |

## 6. Probes and mutants

Characterization baseline (before any edit; WSL, Go 1.27.0; throwaway program outside the repo):
`/bin/sh -c 'kill -9 $$'` reaped through `Process.Wait` → `waitErr=<nil> signaled=true signal=9
exit=-1 core=false`; `/bin/sh -c 'exit 0'` → `waitErr=<nil> signaled=false signal=-1 exit=0
core=false`. The first row is H1's premise: the wait error is nil for a signal death, so
`waitErr == nil` cannot be the fence predicate. `unix.Poll`-EINTR and `waitid(P_ALL)` semantics were
pinned by the same kind of local probe (`waitid` zeroes the siginfo when nothing is ready and sets
`si_pid` on a reap).

Probe rows (green in the ordinary package run; the local race run):

| Probe | Row | Result |
| --- | --- | --- |
| `TestCleanupWaitStatusCodesClassifiesReapedChildren` | `kill -9 $$` / `exit 0` / nil state | pass |
| `TestWaitidCleanupPidfdExitReportsReapedChildren` | CLD_KILLED/9/SIGCHLD, CLD_EXITED/0/SIGCHLD, live child → `errCleanupPIDFDExitTimeout` | pass |
| `TestParseStatusSignalMasksGoldenText` | golden text, missing-field, wrong-width | pass |

Mutant rows (scratch copy of the tree at hand, each restored byte-identical; the gate that must
redden):

| id | mutation | Observed red | Exit |
| --- | --- | --- | --- |
| M1 | swap signal/exit in `cleanupWaitStatusCodes` | `killed probe child decoded wrong: signaled=true signal=-1 exit=9 core=false` (stage-1b rows and again on the final bytes) | 1 |
| M2 | pidfd observation readiness-only (drop `si_code`/`si_status`) | `killed probe child waitid wrong: si_signo=17 si_code=0 si_status=0` (stage-1b and final) | 1 |
| M3 | restore `waitErr == nil` as the pass condition | run 1's and run 2's event gates list the test in `bad=` (§3.1/§3.2) | run 1, run 2 |
| M4 | `parseStatusSignalMasks` returns zeros | `golden status masks parsed wrong: sigcgt="0000000000000000" …` (stage-1b and final) | 1 |
| M5 | treat the `waitid`/poll timeout as an exit | `live probe child did not time out exactly: <nil>` (stage-1b and final) | 1 |

Each row was run in the ordinary `coverage-daemon` sense
(`go test -count=1 -run <probe> ./native/linux-gate/protocol/`) and the file restored to
`133c5b7a…` (stage-1b rows) or `ab3c48ee…` (final rows) after every row, re-checked with
`sha256sum`. The security re-verification observed one transient mutant revision on the deliverable
path during the final mutant round (its P2, §10); the freeze check after the round shows
`ab3c48ee…` with the intended `int(status.Signal()), status.ExitStatus()` order and the diff
byte-equal to the reviewed `diff-final.patch`.

## 7. The printer-window amendment (run-1 defect; contract §Change 1 vs the leg printer)

`print_witness_output_lines` keeps the last ten output events of the four named tests, and
`go test -json` emits one output event per line, so run 1's leg tail (this test's six events then
the two sibling stops' seven) cannot carry three observation lines. The synthetic replay of the
printer's filter showed both shapes (window starts at the root line without the amendment; all
three lines with the test in the parallel phase). Stage 1b used `t.Parallel()`; the review round
showed it displaces the first sibling stop's message from the binding run's window, so stage 2
reverted it — the passing shape is five events and the window then carries this test's
caller/root/`--- PASS` plus both sibling stops (run 3 above).

## 8. Scope

Owned files changed: `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go` (stages 1, 1b,
2), the plan, this record, `docs/TODO.md` (the carve). `clone_cleanup_linux.go` and the privileged
set (`daemon/cmd/symmetry-linux-launcher/**`, `daemon/internal/launchercore/**`),
`scripts/ci/**` and the real workflow are byte-identical to the base. Scratch-only:
`scratch/rf-acc-clone-cleanup-parent-death` with the throwaway workflow and the `gh` shim, deleted
at the end. No fixture change; no bound raised; no assertion deleted.

## 9. Residual risks and known limits

- **Printer-window arithmetic is a judging-surface dependency.** The leg printer keeps the last ten
  output lines across the four named tests. In the stage-2 (passing) shape the window carries this
  test's `caller`/`root`/`--- PASS` and both sibling stops' lines, so
  `clone_integration_linux_test.go:240` and `launcher_integration_linux_test.go:439` are named
  verbatim; the `masks` line is displaced in that shape (present in run 2's log, §3.2). Nothing
  asserts the window's content: if the sibling stops change their output volume or the leg gains a
  fifth named test, the window shifts.
- **Probe platform requirement (DS P3, recorded).** The three probes run in the ordinary package
  run and hard-require `pidfd_open`/`waitid(P_PIDFD)` (kernel ≥ 5.4) plus `/bin/sh`/`/bin/sleep`; a
  seccomp-filtered or older kernel would redden the protocol package for a platform reason. Not
  gated: the contract requires the probes green in the ordinary `coverage-daemon` run and the pinned
  image is Ubuntu-latest.
- **Fence-witness uniqueness.** `si_code=CLD_KILLED && si_status=SIGKILL` is not unique to
  `PDEATHSIG`; within this test no other SIGKILL sender is reachable, so a real fence failure cannot
  pass the assertion. The `masks` line cannot discriminate a fence failure at all (SIGKILL is
  unimpedable); it is contract-mandated printer evidence.
- **Cleanup reap boundary.** The bounded 2 s cleanup reap runs only on failing paths after the root
  child was observed; a child still dying when the bound expires remains a zombie until the test
  binary exits (no functional effect).
- **Pre-existing, re-touched only.** The parent-side copier fds of `io.Discard` stdout/stderr stay
  open until the worker exits because the test calls `Process.Wait()`; the `/proc` mask read may
  precede the worker's Go runtime fully starting (still a truthful bounded value, never an
  assertion).
- **The other two stops** (`clone_integration_linux_test.go:240`, `launcher_integration_linux_test.go:439`)
  and the legs behind the owner leg remain owned by `rf-acc-guest-native-exec`; this goal's run 3
  names them but does not fix them.

## 10. Review round (before the binding run; packets under the GDD reviews root)

Findings records (GDD findings root): `rf-acc-clone-cleanup-parent-death-sol-review-1.md`,
`rf-acc-clone-cleanup-parent-death-ds-audit-1.md`,
`rf-acc-clone-cleanup-parent-death-sec-review-1.md`.

- **Sol review** (`ghc/gpt-6.2-sol:xhigh`, read-only): two P2 and three P3 findings; dispositions in
  plan §9. P2-a (the parallel phase would hide the shared set's first failing check) resolved by
  reverting the parallel amendment; P2-b (the 10 s bound could be bypassed through an EINTR `-1`
  poll count or a ≤0 poll timeout) fixed in `waitForPIDFDExit`; P3s (mask snapshot in the pre-kill
  window; adopted-child zombie; probe child cleanup on failure paths) fixed.
- **DS audit** (`reviewer`, read-only): "Ship", no P1/P2; its two distinct P3s dispositioned:
  readiness failures rendered as reap failures (fixed with the wrapped
  `wait for cleanup pidfd readiness: %w` and the distinct `observe the exact root cleanup worker
  exit after the parent-death fence` message); probe env-gating (recorded, not fixed — the contract
  requires the probes green in the ordinary run).
- **Security pass** (`security-reviewer`, read-only): first pass `No findings.` on the pre-review
  bytes (no shell tool in that session, so byte equality was established by content comparison
  against a base-pinned checkout rather than recomputed digests). Re-verification on the final
  bytes: one P2 — the reviewer observed a transient mutant revision on the deliverable path while
  the final mutant round ran (the M1 form at `:617`) — with the action "freeze the bytes, recompute
  the digest, keep the mutant procedure off the deliverable path"; the freeze check after the round
  shows the file at `ab3c48ee…` with the intended field order and the diff byte-equal to the
  reviewed `diff-final.patch`. No other findings; the four privileged digests are recomputed in §1.

## 11. Fix round (2026-11-25, second review round)

The conductor ruling (contract, "Conductor ruling (2026-11-25, review MUST_FIX)") accepted six
findings and authorized no new scratch run — the binding run moves to the conductor's Bind worker on
the PR head. The test file's delta against run 3's head (`ab3c48ee…` → `8a8fe30c…`) is exactly three
hygiene hunks, 8 insertions and 5 deletions; no assertion, message, closed value or bound moved:

- **STD-A01** — the subreaper reset `t.Cleanup` now reports a failed reset through `t.Errorf`
  (`restore parent-death scratch subreaper state: %v`), matching the five existing in-package sites
  (`clone_integration_linux_test.go:72-76`); the discarded-error form is gone.
- **STD-A02** — `rootChildReaped` starts `false` after `command.Start()`, so the cleanup's bounded
  2 s `waitid(P_ALL, WEXITED|WNOHANG)` reap also covers a failed `waitForDirectCleanupChild` that
  leaves a live root deleter (previously that path returned early, leaving an adopted child
  unreaped until the binary exited).
- The `waitidCleanupPidfdExit` comment block is two lines (contract comment policy).

Records: SPEC-A01 (the census row re-addressed to base `:298` / head `:353`, here and in the plan
§10), SPEC-A02 (the per-run artifact/member cells filled from the retained artifacts), SPEC-A03 (the
omitted `gate_lifecycle_linux.go:1403,1408` and `proc_identity_linux.go:520` sites added with the
same exemption), SPEC-A04 (run 1's re-dispatch re-cited as the diagnostics-visibility defect under
Q-E's "otherwise" branch).

Fix-round gates, run on the frozen `8a8fe30c…` test bytes (WSL Ubuntu-24.04, `-u accept`,
`GOTOOLCHAIN=local`, Go 1.27.0, except the host row; `GOMAXPROCS=8` on the two load rows):

| Gate | Command as run | Exit | Counts |
| --- | --- | --- | --- |
| gofmt | `(cd daemon && gofmt -l .)` | 0 | 0 files |
| vet | `(cd daemon && GOOS=linux go vet ./...)` | 0 | no diagnostics |
| package race | `(cd daemon && go test -race -count=1 ./native/linux-gate/protocol/)` | 0 | `ok 3.500s` |
| host vet | `(cd daemon && go vet ./...)` windows/amd64 Go 1.27.4 | 0 | no diagnostics |
| probe load | `(cd daemon && go test -race -count=200 -run 'TestCleanupWaitStatusCodesClassifiesReapedChildren\|TestWaitidCleanupPidfdExitReportsReapedChildren\|TestParseStatusSignalMasksGoldenText' ./native/linux-gate/protocol/)` | 0 | `ok 25.325s` |
| package load | `(cd daemon && go test -race -count=20 ./native/linux-gate/protocol/)` | 0 | `ok 53.201s` |
| Changed-path clone gate | `python scripts/ci/jscpd-changed.py --repo . --base 6b2d4f8f --head <gate commit>` (the four changed paths committed for this run) | 0 | `4 changed file(s), 1739 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new` |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo . --base 6b2d4f8f --head <gate commit>` (same commit) | 0 | `1 changed record(s) pass the root-only check` |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo . --base 6b2d4f8f --head <gate commit>` (same commit) | 0 | no audited document added or modified; nothing to check |

The STD-A02 arm's mechanism was pinned by a throwaway WSL probe outside the repo (Go 1.27.0): under
`PR_SET_CHILD_SUBREAPER`, `/bin/sh -c '/bin/sleep 30 & exit 0'` leaves the sleep adopted by the
probe process (`/proc/self/task/<tid>/children` names it); `waitid(P_ALL, WEXITED|WNOHANG)` returns
`signo=0` while it lives (the cleanup loop's continue arm) and reaps it after `SIGKILL` as
`si_signo=17 si_code=2 si_status=9` — the loop's own call form. The STD-A01 `t.Errorf` arm has no
constructible pin (a `PR_SET_CHILD_SUBREAPER` reset cannot be made to fail from the test); the five
pre-existing sites carry the identical arm.

Delivered bytes: the §3.3 equality table binds run 3's head to the pre-fix delivered bytes; §1's
fix-round column carries the post-fix test and plan digests, and the conductor's Bind run on the PR
head re-binds the delivered bytes.
