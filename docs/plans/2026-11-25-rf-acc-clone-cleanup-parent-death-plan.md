# rf-acc-clone-cleanup-parent-death — implement plan

Contract: `goal-rf-acc-clone-cleanup-parent-death.md` (GDD state root, external to the repository).
Base revision: `6b2d4f8f195a2b2011d1fff4962d1e13acc74efd` (`origin/main` after `git fetch`; PR #285).
Branch: `refactor/rf-acc-clone-cleanup-parent-death`. Rulings carried: Q-A (a fence failure whose
only fix is `wrapper.c`/`launchercore` is `DECISION_NEEDED`, the owned set never widens), Q-B
(`PR_SET_CHILD_SUBREAPER` in the test process and the root-deleter observation before the unchanged
fatal, test-only), Q-C(a) (stage-2 asserts the worker `signaled && signal == SIGKILL` **and** the
root deleter `si_code=2 si_status=9`), Q-D (acceptance under a red `shared` leg: our test absent
from `bad=`, witness lines printed, the next stop named verbatim), Q-E (a 4th run only for a probe
defect).

The accepted plan is the contract; this file resolves only the choices it leaves open, the
per-clause baseline, and what is out of scope.

## 1. Clause census at the dispatch head (before any edit)

Contract addresses are `6b2d4f8f` by construction (the dispatch head is this base). Every cited
site re-located by content:

| Contract | Head | Content | Disposition |
| --- | --- | --- | --- |
| stop reader `:256-259` | `clone_cleanup_linux_test.go:256-259` | `_, waitErr := command.Process.Wait()` then `if waitErr == nil { t.Fatal("A cleanup caller exited without the requested parent-death signal") }` | converted in stage 2 only, after run 1 names the cause; the message text is kept as the predicate's leading clause |
| stop renderer (worker) `:298` (the contract text's `:289` is base `t.Fatal(err)`) | `clone_cleanup_linux_test.go:298` (base) → `:356` (delivered bytes, re-derived by content; `:353` at the stage-2 head `0e0b5486`) | `t.Fatalf("parent test killed A cleanup owner but root call returned: %v", err)` | exempt: the worker is the killed caller; the fatal is only reachable if the kill never happened, and its text is out of the observed stop class |
| root-deleter observation `:262-270` | `clone_cleanup_linux_test.go:261-271` | `waitForPIDFDExit` readiness plus the two preserved-entry `Lstat` checks | stage 1 observes the same pidfd with `waitid` before the unchanged fatal; stage 2 under Q-C(a) replaces readiness with the `si_code`/`si_status` assertion |
| sibling readers in the package | `clone_cleanup_linux.go:525,726,776` | `command.Process.Wait()` in the `Start` error path (`_` discard), `waitid(P_PIDFD)` in `waitReapCleanupChild`, and `reapCleanupChildWithoutPIDFD` (error join only) | exempt: none decides a pass condition, and none reads the worker's reaped status |
| other package readers | `gate_lifecycle_linux.go:459,1205,1214,1403,1408`; `proc_identity_linux.go:520` | launcher/gate wait paths (`stopLauncher`'s `waitProcessFD` and error-checked reap); the pidfd liveness probe (`WNOWAIT`, expects `ECHILD`) | exempt: different producer, no relation to this stop |

Characterization baseline (lesson 74, captured before any edit; WSL Ubuntu-24.04, Go 1.27.0,
throwaway program at a scratch path outside the repo): `/bin/sh -c 'kill -9 $$'` reaped through
`Process.Wait` yields `waitErr=<nil> signaled=true signal=9 exit=-1 core=false`; `/bin/sh -c 'exit
0'` yields `waitErr=<nil> signaled=false signal=-1 exit=0 core=false`. The first row is H1's
premise on the pinned toolchain: the wait error is nil for a successfully reaped signal death, so
the `:258` predicate can only be read from the status. No local run can reproduce the guest's
setuid fixture (no root fixture on this host); run 1 is the end-to-end observation.

Environment fact verified before coding: `golang.org/x/sys@v0.50.0/unix` defines no `CLD_*`
constants (grep over the module cache, zero hits), so the two used `si_code` values are local
constants carrying the uapi source in one comment.

## 2. Public/API shape (test-only; no product bytes in stage 1)

All additions live in `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go` (owned),
inside the existing `//go:build linux && amd64` file. No new file. Nothing is exported.

1. `func parseStatusSignalMasks(status []byte) (sigCgt string, sigBlk string, sigIgn string, err error)`
   — keys on the exact `SigCgt:`/`SigBlk:`/`SigIgn:` fields, requires 16 lowercase hex digits each
   (the amd64 kernel's `render_sigset_t` width, confirmed on the host), errors on a missing field.
2. `func cleanupWaitStatusCodes(state *os.ProcessState) (signaled bool, signalCode int, exitCode int, coreDump bool)`
   — decodes `syscall.WaitStatus` from `state.Sys()`; `nil` state (a failed reap) decodes to
   `false, 0, 0, false` and the log line disambiguates it with `wait_error`.
3. `type cleanupPidfdExit struct { Signo, Code, Status int32 }` and
   `func waitidCleanupPidfdExit(pidfd int, timeout time.Duration) (cleanupPidfdExit, error)` —
   waits on the existing 10 s pidfd bound, then reaps through `waitid(P_PIDFD)` (EINTR retried) and
   reuses the package's `cleanupSiginfoStatus` (`clone_cleanup_linux.go:748-753`, read-only).
4. `var errCleanupPIDFDExitTimeout` — `waitForPIDFDExit` returns it on the bound, so the caller can
   render `timeout=1` without matching error text.
5. `cleanupCLDExited = 1`, `cleanupCLDKilled = 2` — the two `si_code` values used, with one
   source comment (`include/uapi/asm-generic/siginfo.h`).
6. Three `t.Logf` call sites total (one per observation), closed values only, no path or PID:
   - `cleanup caller signal masks: <sigcgt=… sigblk=… sigign=…|read_error=1|parse_error=1>`
   - `cleanup caller wait status: signaled=%t signal=%d exit=%d core=%t wait_error=%t`
   - `root cleanup worker wait status: <si_signo=%d si_code=%d si_status=%d|timeout=1|waitid_error=1>`
   Worst-case length ≈100 bytes, far below the leg printer's 400-byte bound.

## 3. Stage 1 implementation choices

- **Subreaper placement.** `PR_SET_CHILD_SUBREAPER` is set once, after the fixture precondition
  checks and before the worker spawn, with one why-only comment (without it init adopts the orphan
  and the exit code is unrecoverable); a `t.Cleanup` resets it. The reset is registered before the
  worker-kill cleanup so LIFO runs the kill first: the orphan is already reparented to this process
  by reset time either way.
- **Mask snapshot placement.** Immediately before `command.Process.Kill()`, while the pidfd
  pre-checks just proved the worker was alive. A read or parse failure renders `read_error=1` /
  `parse_error=1` instead of a fatal: the observation never introduces a new failing check that
  could pre-empt the unchanged one.
- **Order of the after-kill block.** reap the caller → decode + log → `waitidCleanupPidfdExit` +
  log → render the unchanged `if waitErr == nil` fatal → keep the existing readiness-only
  `waitForPIDFDExit` + preserved-entry checks. The readiness call is only reachable when
  `waitErr != nil` (a failed reap), and the pidfd stays readable after the `waitid` reap, so the
  stage-1 tree keeps the post-fatal code byte-equivalent in behaviour to the base for every path
  run 1 can take.
- **Probe tests (green in the ordinary `coverage-daemon` package run).**
  `TestCleanupWaitStatusCodesClassifiesReapedChildren`: `kill -9 $$` row (also pins that the reap
  error is nil while the status is signaled), `exit 0` row, nil-state row.
  `TestWaitidCleanupPidfdExitReportsReapedChildren`: killed child CLD_KILLED/9/SIGCHLD, exited
  child CLD_EXITED/0/SIGCHLD, live child → `errCleanupPIDFDExitTimeout` (no fabricated exit).
  `TestParseStatusSignalMasksGoldenText`: golden text with a decoy `SigPnd` line, a missing-field
  row and a wrong-width row. Children are the tests' own (`/bin/sh`, `/bin/sleep`), no root
  fixture, no guest.
- **Mutant mapping (local, scratch copies, restored byte-identical).** M1 swap signal/exit in
  `cleanupWaitStatusCodes` → killed row red; M2 drop `si_code`/`si_status` (return zero
  `cleanupPidfdExit`) → killed/exited rows red; M4 return zero masks → golden row red; M5 treat the
  timeout as an exit (return nil error) → live row red. All four redden
  `go test ./native/linux-gate/protocol/` locally; M3 is the run-1 gate row (the unmutated stage-1
  tree already shows the test in `bad=`), quoted by run id.

## 4. Stage 2 contingency (no byte before run 1's named cause)

- **(a) caller `signaled=true signal=9` + root `si_code=2 si_status=9`** (predicate channel): keep
  the reap-error guard (`waitErr != nil` fatal, the check is never deleted), then
  `if !signaled || signalCode != int(unix.SIGKILL)` fatal with the original message text plus the
  four closed values appended; replace the readiness-only root check with the `waitid` values:
  `waitErr` timeout keeps the exact `root cleanup worker survived its exact A parent: %v` message,
  a reap failure gets its own message, and the values must be `si_signo=17 si_code=2 si_status=9`.
- **(b) caller `signaled=false exit=N`**: trace N through the caller's path; only
  `clone_cleanup_linux.go` or this test's own scaffolding may be edited.
- **(c) root `timeout=1`**: the fence failed; a fix whose only home is `wrapper.c` or
  `launchercore` stops at `DECISION_NEEDED` with the closed values.
- The three log lines stay in stage 2 (they are the run-2 printer evidence).

## 5. Evidence plan

Scratch branch `scratch/rf-acc-clone-cleanup-parent-death`, throwaway workflow
`.github/workflows/scratch-rf-acc-clone-cleanup-parent-death.yml` (copy of
`linux-native-v2-acceptance.yml`, sha256 `c0d18435…`: no `environment:`, no `secrets.*`/`vars.*`,
the ratified dummy pins, `GITHUB_EVENT_NAME=workflow_dispatch` in the loop env, the scratch-only
`gh` shim `507340af…`, `case_sets=(shared)`, push trigger on the scratch branch), deleted at the
end. Runs, foreground, ≤3: (1) stage-1 bytes names the cause; (2) stage-2 bytes shows our test
absent from `bad=`; (3) literal final run on the frozen bytes, its head compared per file against
the delivered worktree. Record `docs/evidence/2026-11-25-rf-acc-clone-cleanup-parent-death.md`
(per-run table with run id, first failing check verbatim, closed values, artifact ids, head sha;
the sha256 equality table; the census above; the mutant rows). `docs/TODO.md`: carve this stop's
clause only, the sibling's two stops verbatim; the closed clause moves to
`docs/archive/CLOSED-TODO.md` under its own heading (§11, R5).

## 6. Local gates

WSL Ubuntu-24.04, `GOTOOLCHAIN=local` (Go 1.27.0): `(cd daemon && gofmt -l . && GOOS=linux go vet
./... && go test -race -count=1 ./native/linux-gate/protocol/)`; host `go vet ./...`; the four
mutants as scratch copies. No scratch run is dispatched concurrently with a local Go suite (host
memory), and no measurement harness runs beside the suite.

## 7. Out of scope (with reasons)

- `daemon/cmd/symmetry-linux-launcher/**` and `daemon/internal/launchercore/**` (the fence arms):
  not in the owned set; branch (c) stops at `DECISION_NEEDED`.
- The sibling's two stops (`clone_integration_linux_test.go`, `launcher_integration_linux_test.go`)
  and the pi-leg `pi_provider_action` consequence: owned by `rf-acc-guest-native-exec`.
- `scripts/ci/**`, the real workflow, `daemon/e2e/**`, `daemon/internal/platform/**`,
  `daemon/internal/state/**`: not owned. `docs/archive/CLOSED-TODO.md` is not a subject file; it
  carries the closed clause's move (§11, R5) while the two sibling clauses stay open in
  `docs/TODO.md`.
- Any fixture change: none is proposed; stage 1 only observes.

## 8. Addendum (2026-11-25, after run 1) — the printer window and the parallel-phase amendment

Run 1 (`45595479247`, head `cc22c107`) carried one of the three closed-value lines into the run
log: `witness output:     clone_cleanup_linux_test.go:284: root cleanup worker wait status:
si_signo=17 si_code=2 si_status=9`, followed by the unchanged fatal at `:286`. The masks and
caller lines were displaced by the leg printer's `lines[-10:]` window: after this test's output
the stream carries the two sibling tests' seven failure lines plus this test's own fatal message
and `--- FAIL` line, so only the last observation line fits. Replaying the printer's exact python
filter over a synthetic `go test -json` stream of the four test names reproduces this (window
shows `root …` only) and shows the amendment's shape (window shows all three lines and the fatal
message, or all three and `--- PASS` when the stage-2 bytes pass).

Amendment: `t.Parallel()` after the skip guard runs this test in Go's parallel phase — after the
three sequential tests of the leg — so the printer tail is this test's own six lines. No
assertion, message, closed value, or bound changes; the leg's `bad=` counts and its first failing
check are order-independent. Options considered: (a) merge the three observations into one
printer-visible line (fits the window; deviates from the mandated three-line shape); (b) the
parallel phase (keeps the three lines exactly as mandated; changes only intra-binary scheduling).
Chosen: (b).

Run mapping after the amendment (run 1's dispatch is invalidated as the observation-complete
dispatch by this diagnostics-visibility defect in the new diagnostic's emission shape — not a probe
defect, so Q-E's "otherwise" branch applies and the final-literal requirement falls on the
stage-2 bytes):
run 1 = stage-1 dispatch 1 (partial lines; records the root values and the window arithmetic),
run 2 = stage-1 dispatch 2 (three lines verbatim; names the cause), run 3 = stage-2 (the binding
run, also the literal final run on the frozen subject bytes).

## 9. Addendum (2026-11-25, review round before the binding run) — findings and resolutions

Panel: Sol review (`ghc/gpt-6.2-sol:xhigh`, packet under the GDD reviews root), DS audit
(`reviewer`), security pass (`security-reviewer`). Reconciled dispositions:

- **Sol P2 — the parallel phase hides the shared set's first failing check.** With `t.Parallel()`
  this test's six-line tail occupies ten of the window's slots and run 3 would print only
  `launcher_integration_linux_test.go:439`, not `clone_integration_linux_test.go:240`. Resolution:
  revert the §8 amendment. The passing stage-2 shape is five events (three lines + `--- PASS` +
  `=== RUN`), so the natural order leaves the window = `caller`, `root`, `--- PASS` plus the two
  sibling stops' message/frame lines — both expected next stops named verbatim. The masks line is
  displaced in that shape (it is the non-classifying line and is present in run 2's log); this is
  the documented trade-off. The contract's three-line shape and the base execution order are kept.
- **Sol P2 — the 10 s bound could be bypassed.** `unix.Poll` returns `-1` on `EINTR`; the old
  `count != 0` treated that as readiness and a following `waitid` could block past the bound. Fixed:
  readiness requires `count > 0`, and the poll timeout is clamped to ≥1 ms so a computed ≤0
  remaining time cannot poll indefinitely. Same 10 s bound; no bound raised.
- **Sol P3 — the mask snapshot widened the pre-kill window.** The `/proc/<pid>/status` read and its
  log line sat between the deletion witness and the `SIGKILL`; both moved out (read immediately
  after the worker spawn, emission with the other two lines after the kill). The observation is
  still before the kill, as Change 1 requires.
- **Sol P3 — an adopted root child could stay unreaped.** The worker-kill cleanup now reaps an
  observed-but-unreaped root deleter with a bounded 2 s `waitid(P_ALL, WEXITED|WNOHANG)` loop
  (the kernel zeroes the siginfo when nothing is ready; `ECHILD` ends the loop), run only when the
  body did not reap it.
- **Sol P3 — probe child cleanup on failure paths.** The probe helper registers `Kill`+`Wait`
  immediately after `Start`, so an early `t.Fatal` no longer leaves its `/bin/sh` children running;
  the manually built live `/bin/sleep` child, which this bullet overstated, gained the same
  registration in the PR #287 round-1 fix (§11, R1).
- **DS P3 — readiness failures were rendered as reap failures.** `waitForPIDFDExit` now wraps
  non-EINTR poll errors (`wait for cleanup pidfd readiness: %w`) and the test renders the
  non-timeout failure as `observe the exact root cleanup worker exit after the parent-death fence`,
  so the attribution branches (timeout / readiness / reap) are distinct.
- **DS P3 (not fixed, recorded)** — the probes hard-require `pidfd_open`/`waitid(P_PIDFD)` and run
  in the ordinary package run; on a kernel < 5.4 or a seccomp-filtered container they would redden
  the package. The contract requires them green in the ordinary run and the pinned CI image is
  Ubuntu-latest; recorded as a residual, not gated.
- **DS residual (recorded)** — the masks line cannot discriminate a fence failure (SIGKILL is
  unimpedable); it is contract-mandated printer evidence. `si_code/si_status` is not unique to
  `PDEATHSIG`, but no other SIGKILL sender is reachable in this test.
- **Security pass** — `No findings.` on the pre-review bytes (byte equality of the privileged set by
  content comparison against a base-pinned checkout; the session had no shell tool, so the four
  digests were not recomputed) plus a requested re-verification on the final bytes.

## 10. Addendum (2026-11-25, fix stage) — second review round dispositions

The conductor ruling on the second review round (contract, "Conductor ruling (2026-11-25, review
MUST_FIX)") accepted six findings; all are applied in the fix stage, with no new scratch run — the
binding run moves to the conductor's Bind worker on the PR head.

- **SPEC-A01** — the census row paired base `clone_cleanup_linux_test.go:289` with the worker fatal;
  base `:289` is `t.Fatal(err)` and the fatal sits at base `:298` / delivered `:356` (`:353` at the
  stage-2 head `0e0b5486`). Re-addressed here (§1) and in the evidence record; re-derived from the
  delivered bytes in §11 (R4).
- **SPEC-A02** — the evidence record's per-run artifact/member cells named §3.1/§3.2 instead of an
  artifact id; filled from the retained artifacts (`linux-v2-native-evidence-<run>-1` carrying
  `shared/unprovisioned-owner-refusal.json`).
- **SPEC-A03** — the census omitted `gate_lifecycle_linux.go:1403,1408` and
  `proc_identity_linux.go:520`; added with the same exemption.
- **SPEC-A04** — run 1's re-dispatch was attributed to Q-E's probe-defect clause; re-cited as the
  diagnostics-visibility defect under Q-E's "otherwise" branch.
- **STD-A01** — the subreaper reset cleanup discarded its error; it now reports through `t.Errorf`,
  matching the five existing in-package sites.
- **STD-A02** — `rootChildReaped` started `true` and flipped only after a successful observation, so
  a failed observation left an adopted child unreaped; it now starts `false`.

The test file is otherwise byte-identical to run 3's head: three hygiene edits (STD-A01, STD-A02 and
one comment block shortened to the two-line cap); no assertion, message, closed value or bound moved.

## 11. Addendum (2026-11-25, PR #287 round 1) — findings and the re-bind

Panel: Sol review, DS audit, security pass (findings records `287-opus-review-1.md`,
`287-ds-audit-1.md`, `287-sec-review-1.md` under the GDD findings root) plus the conductor's Bind
run report `287-binding-run.md`. The Bind run `45611900578` (head `76a36d5f` = `ac0fa101` + the two
scratch files) printed `--- PASS` for this test, absent from `bad=`, and bound the `ac0fa101`
delivered bytes; its run-table row is in the evidence record §3.

- **R1** — the live timeout-probe child had no failure-path cleanup; the §9 bullet overstated the
  fix. The `/bin/sleep` child now registers `Kill`+`Wait` through `t.Cleanup` immediately after
  `Start` (mirroring the helper) and the trailing inline `Kill`/`Wait` pair is gone; §9's bullet is
  corrected. Re-opens the binding requirement (the bytes change).
- **R2** — `waitForDirectCleanupChild` tolerated `EINTR` at the poll and then read `count != 0`, so
  an interrupted poll (`count=-1`) was rendered as the caller's exit; it now requires `count > 0`,
  mirroring `waitForPIDFDExit`. The package's `unix.Poll` sites are censused with the grep command
  and hit count in evidence §5.1.
- **R3** — the dead store `rootChildReaped = false` (unobservable between the declaration and the
  cleanup closure) is deleted.
- **R4** — the worker-fatal census address is re-derived from the delivered bytes (§1): base
  `:298` → delivered `:356` (`:353` at the stage-2 head; the fix round's `:357` moved by the R3
  deletion above it).
- **R5** — the closed parent-death clause moves to `docs/archive/CLOSED-TODO.md` under its own
  heading; the two sibling clauses stay open in `docs/TODO.md`, which points at the archive entry.
  Supersedes §5's "no `CLOSED-TODO.md` entry" and §7's blanket archive exclusion.

R1/R2/R3 change the test bytes after the Bind run, so the conductor re-binds once on the new head
(one `shared` run); the fix stage runs no scratch run. Local gates and the changed-doc sanitizer run
on the frozen round-1 bytes (evidence record §12).
