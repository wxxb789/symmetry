# Goal 0006b bounded iteration closure — 2026-10-10

## Accepted outcome and authority

The current owner explicitly requested reduced difficulty and completion of this
pre-0.0.1 iteration without solving every problem or guaranteeing project-wide
runnability or absence of bugs. The adopted contract is the owner-approved
iteration scope in [Goal 0006b](../goals/archive/0006b-crash-safe-process-containment.md).

The bounded iteration acceptance is satisfied: current-source journal/authority
and selected recovery/lease regressions passed, Windows and Linux-target static
analysis/build checks passed, a real daemon CLI help smoke ran, and an independent
bounded review has no remaining actionable finding. This is **local iteration
closure**, not full native, release, capability or whole-project certification.

The prior twenty unfinished acceptance items were explicitly deferred and removed
from this iteration's live task list, not marked passed. Their exact task labels
are preserved in the session artifact
`local://goal0006b-iteration-deferred-task-labels-20261010.json`; the scope
amendment is `local://goal0006b-owner-iteration-scope-amendment-20261010.json`.
The historical mapping and failure records remain retained.

## Current source and changes

- Checkout: `codex/goal-0006b-crash-safe-process-containment`.
- Observed HEAD: `d14debbf6ae3ded09e5013ef90952d347c01474d`.
- This is a dirty working-tree subject, **not** a new exact acceptance commit.
  The resumed inventory contained 68 modified tracked paths, 58 untracked entries
  and no staged changes. Those pre-existing changes were preserved.
- Added the bounded closure contract to the goal and precedence notice to the
  goal index. Marked the older convergence contract as historical/superseded.
- Removed only an unreachable three-line clone-cleanup timeout override in
  `daemon/internal/platform/linux_owner_anchor_linux.go`. The earlier dispatch
  already calls `serveCloneCleanup` and returns. The actual cleanup deadline in
  that handler is unchanged. No authority, persistence, provider or API behavior
  was intentionally changed.
- No test predicate, CI gate or deadline was removed or relaxed in code. No new
  permanent test was needed for deletion of an unreachable branch; existing
  selected regressions, builds, static analysis and independent source review
  supply the scoped verification.
- The existing operator-provided `gpt-6-luna` / `medium` provider choice remains
  unchanged. No renewed credential request, provider probe or provider call was
  performed in this closure pass. Authentic native Claim/provider acceptance is
  deferred, not substituted with provider availability.
- The two new WSL2/9P preflight scripts from the previous custody track remain
  **unexecuted**. The author acknowledged the scope change and stopped further
  work. They are not custody/native evidence or a closure prerequisite.

Changed-file SHA-256 bindings (not a full-source custody manifest):

These bindings record the closure pass before the owner's subsequent `git mv`
archive request. Archival preserves the goal bytes; index/reference edits after
that pass do not inherit the earlier document hashes as current-file evidence.
The archived goal is no longer an active iteration entry.

| File | SHA-256 |
| --- | --- |
| `daemon/internal/platform/linux_owner_anchor_linux.go` | `35207163f0d6e0e90602cde8a8a6f2d434530b6cf759a26783f9227cc79c21a4` |
| `docs/goals/0006b-crash-safe-process-containment.md` | `5858db0a716c146a1c6e4a44473d97e8002b70ff2749abbfe5f8a7faf3f26bd0` |
| `docs/goals/README.md` | `20ac253cabae30b5011de9eb27777ca56793b3f08641ee2b7886ca4f6c285e1c` |
| `docs/evidence/goal-0006b-convergence-goal-2026-09-29.md` | `b4d86d0017c366d2c2fee3320861c2fd70eac021efc1caf9e468fb090a448312` |

## Checks actually executed

Windows commands ran from `daemon/` using the explicit executable
`C:/Users/lhan/AppData/Local/mise/installs/go/1.27.2/bin/go.exe` (abbreviated
`GO_WIN` below). This avoids the observed PATH/GOROOT mismatch without changing
system configuration.

| Check | Result | Observed scope |
| --- | --- | --- |
| State and authority package tests | PASS | Both complete package invocations; package times 28.217 s and 1.475 s |
| Selected Windows App/platform regressions | PASS | 10 named top-level tests; package times 2.503 s and 2.106 s |
| Windows `go vet ./...` | PASS | Exit success, no diagnostics; 18.56 s wall time |
| Windows daemon/fake-agent build | PASS | Exit success, no diagnostics; 5.81 s wall time |
| Real Windows daemon `-h` smoke | PASS | Printed `Usage` and the `-config` option; 10.21 s wall time |
| Linux amd64 `CGO_ENABLED=0 go build ./...` | PASS | Cross-build after the dead-branch removal; exit 0, 27.230 s |
| Linux amd64 `CGO_ENABLED=0 go vet ./...` | PASS | Target-specific static analysis after removal; exit 0, 9.749 s |
| Selected Linux platform/App regressions in existing WSL2 | PASS | 5 top-level tests plus 2 nested cases; package times 0.020 s and 0.246 s |

Exact Windows test selector:

```text
WIN_TESTS=^(TestStopPersistedProcess(DurableReceiptPrecedesHelperRelease|ReplaysReceiptWriteBarrierAfterPostRenameUnknown)|TestRecoverPersistedSupervisorHandoff(StopsAndReleasesBoundLaunch|RetainsUnresolvedBoundLaunch)|TestContainmentSupervisor(LeaseStateRejectsLateRenewal|LeaseStateIgnoresStaleTimerAfterRenewal|StopStateReplaysExactReceipt|ReleaseRequiresStopProofAndDoesNotRearmLease)|TestRecoverPersistedContainmentReplaysDurableReceiptWithoutContactingHelper|TestReleasePersistedContainmentRequiresDurableReceiptAcknowledgement)$
```

Executed Windows commands:

```text
GO_WIN test -count=1 -p=1 -timeout=180s ./internal/state ./internal/authority
GO_WIN test -v -count=1 -p=1 -timeout=180s ./internal/app ./internal/platform -run WIN_TESTS
GO_WIN vet ./...
GO_WIN build ./cmd/symmetry-daemon ./cmd/symmetry-fake-agent
GO_WIN run ./cmd/symmetry-daemon -h
```

The Linux cross-build and vet used the same explicit Windows Go executable with
process-local `GOOS=linux`, `GOARCH=amd64`, `CGO_ENABLED=0`, and
`-C Q:/repos/symmetry/daemon`. No global environment setting changed.

The actual Linux test command used the existing selected WSL2 guest and current
live checkout, not an old frozen source root:

```text
C:/Windows/System32/wsl.exe -d Symmetry-Goal0006b-Local-c06a7a09 --user symmetry --cd Q:/repos/symmetry/daemon --exec /opt/symmetry-goal0006b-bootstrap/c06a7a09/go/bin/go test -v -count=1 -p=1 -timeout=180s ./internal/platform ./internal/app -run "^(TestLinuxSupervisorLeaseState(ReplaysAndFencesGenerations|LatchesDeadlineBeforeTimerCallback|OwnerLossLinearizesWithResume)|TestRestartStopPersistsAuthenticatedHelperSealOnlyWithPriorMirrorSeal|TestLinuxOwnerAnchor.*(Stop|Kill|Cleanup))$"
```

Observed Linux tests were the three lease-state cases,
`TestLinuxOwnerAnchorStateReadbackDenialPreventsRootCleanup`, and the restart seal
case with its `unsealed_mirror_remains_unresolved` and
`authenticated_helper_terminal_seal_precedes_receipt_release_and_clear` cases.
They establish scoped unit/injected recovery behavior, not installed native
execution or real descendant termination.

The CLI smoke establishes execution of the real help path, not service startup,
Control connectivity or native launch. No browser surface changed in this pass.
No full-suite/race/Control/browser/release/remote-CI run was claimed by that
closure pass; later publishing checks are recorded separately below.

### Preserved failed setup attempt

The first PATH-selected Windows Go invocation failed before tests:
`compile: version "go1.27.2" does not match go tool version "go1.27.0"`.
`where.exe go` and `go env GOROOT GOTOOLDIR GOTOOLCHAIN` located the conflicting
executable and mise Go root. The successful checks used the matching existing
explicit executable. This was an execution-environment correction, not a product
patch, test failure suppression or global installation change. The failed
attempt remains recorded in `artifact://10133`.

## Independent review and simplification

Actual routes: Main Sol owns integration and execution; `scout` (Luna medium)
retrieved current source/test entrypoints; `reviewer` (Sol high) performed the
independent bounded review. No Astra or external review route was started in
this pass. The earlier Sol xhigh preflight author performed no actual execution.

The reviewer read the amended documents and named cleanup/recovery/State/anchor
seams, checking receipt-before-release, UNKNOWN retention, v1/v2 isolation,
single kill authority, cleanup State readback and journal deletion barriers.
Coverage excludes untraced workspace/root-deleter internals, dedicated v2
callbacks outside those seams, installed service policy and runtime race/crash
certification.

One nonblocking P3 finding identified the unreachable timeout branch. After the
three-line deletion, the same reviewer confirmed that the earlier dispatch and
actual `linuxOwnerAnchorCleanupTimeout` deadline remain intact. Final verdict:
no remaining actionable finding within this bounded review. This is not a
whole-project security approval. No broader simplification or refactor was added.

## Deferred acceptance and unresolved limitations

Every row below is **DEFERRED / NOT PASSED** for this iteration. Related
requirements may be addressed by later native, harness or release iterations;
this receipt neither schedules them nor certifies them.

| Former acceptance item | Retained limitation |
| --- | --- |
| Native credentials/model-input/protected archive qualification | Complete installed-native input/archival acceptance unverified; do not reask for the already supplied provider |
| Installed Linux authority and exact A/B boundary | Exact A52000/B52001 installation unverified; earlier WSL component evidence is not this acceptance |
| Pi/OpenCode native execution and authentic actions | Full production-owner positives and authentic action evidence unverified |
| Crash/replay/cleanup/stale/retention case sets | Exhaustive original cross-platform sets incomplete |
| WSL-denial and Rootless rejection witnesses | Original installed fail-closed witnesses incomplete |
| Full sanitized/protected recovery evidence | Same-subject native/recovery/archive closure incomplete |
| Exact-source complete platform/Control/release matrix | Full matrix unpassed; only checks above are current execution evidence |
| Full release-subject safety/recovery/simplification review | Replaced for local closure by the bounded review, not a full release review PASS |
| Exact-commit remote CI | Not run |
| Verified worktree integration under former full contract | Not performed; unrelated WIP and local work preserved |
| Former full acceptance report | This bounded receipt replaces that local closure prerequisite, not its certification |
| Complete Control matrix | Original live waiting-input deadline failure remains unresolved |
| Original four Rootless Pi/OpenCode cases | Cases remain unpassed; original protection failure is not repaired |
| Complete SDK/compiled Pi publication and retention | Full SDK/native proof unverified |
| Seventeen namespace/CBOOT/enforced-disable/J/U families | Full family acceptance unverified |
| Nineteen-input custody and three changed-content experiments | Historical rules/complete handoff remain unresolved; ABI scripts unexecuted |
| Native base and closed-host closure | Complete qualification unverified |
| Installed owner preparation/preflight/stop/raw | Full installed native acceptance unverified |
| Authentic claim-scoped Control Integration | Not newly established by this closure pass |
| Equivalent Pi bridge native coverage | Full cross-platform native actions/action-ID evidence unverified |

Existing failure and UNKNOWN records remain intact. No frozen old input, helper,
raw file, native owner, descriptor, pointer or journal was reopened, replayed,
reconstructed, repaired or deleted in this closure pass. Product fences,
credential locality and fail-closed authorization remain in force. No provider
secret was requested or printed.

No commit, push, merge, installed-owner provisioning, capability promotion or
new remote CI was performed in the original closure pass. The owner subsequently
authorized commit/push/PR publication and explicitly requested archival. Those
effects are separate from this bounded closure.

## Subsequent publishing validation

The publishing scope contains 125 explicitly named paths, excluding unrelated
Control ablation tests, the macOS design edit, older unlisted evidence and local
runtime artifacts. These checks use current live source and new test-owned
fixtures; they establish no continuity with frozen historical subjects.

| Check | Observed result |
| --- | --- |
| Windows `go test -json -count=1 -p=1 -timeout=180s ./...` | PASS; 1,583 top-level PASS, 41 SKIP, zero FAIL; 491.51 s |
| Linux `go test -json -count=1 -timeout=300s ./...`, final diagnostic fixture | PASS; 1,857 top-level PASS, 87 top-level SKIP, zero FAIL; 103.75 s |
| Linux `go test -race -json -count=1 -timeout=600s ./...` | PASS with the earlier readdir-only diagnostic fixture; production source matches the final state; 1,857 top-level PASS, 87 top-level SKIP, zero FAIL and no data-race report; 570.10 s |
| Final census fixture `go test -race -v -count=1 -timeout=60s ./native/linux-gate/protocol -run '^TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors$'` | PASS; parent and four subcases; 2.640 s package time |
| Windows/Linux `go vet ./...` and `go build ./...` | PASS; no diagnostics; production source unchanged afterward |
| C gate and launcher with installer production compile/link flags | PASS; both verified ELF64 x86-64 without an interpreter segment; binaries removed from new owned temporary storage |
| CI YAML parser | PASS; 13 jobs parsed |
| Scoped `gofmt -d` after the final diagnostic edit | PASS; zero formatting diff across all 94 owned Go files; the earlier formatting pass remains valid |
| Redacted evidence exporter and receipt publisher CLI | PASS; 13 exporter tests and nine publisher tests; new non-native fixtures only |
| Source-subject and strict Go-event helpers | PASS; four source-subject and six event-parser tests |
| Installer/cleanup shell syntax and disposer smoke | PASS; only new owned probe removed, state fields cleared, repeat disposal harmless and unrelated sentinel preserved; installer not executed |

Complete Go JSONL streams were parsed, not inferred from truncated previews.
Windows ordinary output is `artifact://10189`; Linux race output is
`artifact://10207`; final Linux ordinary output is `artifact://10211`.
Test-level PASS event counts, including nested subcases but excluding package
results, are 3,457 on Windows and 4,004 on Linux. Linux has one nested test SKIP
in addition to the 87 top-level test skips. Package results are separately
19 PASS/one no-test SKIP on Windows and 21 PASS/two no-test SKIPs on Linux.
Native opt-in skips remain unexecuted acceptance, not native capability evidence.

### Preserved unexplained census failure

The preceding Linux ordinary invocation failed with `EPERM` in
`TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors/allowed-fds`
(`artifact://10188`, 160.09 s). Its initial FD 0..8 shape check passed; the
failure output did not distinguish task rejection, a higher inherited FD or a
procfs operation failure. The other three subcases passed. This is a real,
**unexplained failure**, not a confirmed production fix or a waived assertion.

The test now retains fixed-buffer, failure-only diagnostics for the actual
`opendir`, `readdir` and `closedir` calls. Return values and immediate errno are
preserved; no descriptor or thread is added during the census. Production gate
checks and every original assertion remain unchanged. Diagnostic observations
may alter timing, so later passes do not prove the original cause disappeared.

An instrumented protocol-package run of 100 iterations passed, as did 2,000
independent child launches. A separate owned FD64 control demonstrated that a
non-CLOEXEC descriptor can produce the same allowed-FD rejection, while setting
CLOEXEC on that same owned descriptor allows the census. This establishes the
inheritance mechanism, **not** the original failure's source. No retry loop was
added to the product or test acceptance.

Sol owns implementation and verification. A Sol medium read-only consultant
reviewed diagnostic fidelity; an Astra xhigh read-only adviser narrowed the
unresolved hypotheses without executing tools or changing files. Neither
declared a root cause or granted release acceptance. All installed-native,
custody, Control/browser/release and exact-commit remote qualifications deferred
above remain unpassed by these local checks.

A final Sol high read-only publishing review found two evidence wording issues:
the earlier fixture used by the full race run was not explicit, and test-level
event counts did not exclude package results clearly. Both descriptions are
corrected above. That review found no new product blocker within this bounded
publication scope; the unexplained census failure remains a recorded risk.
