# Goal 0006c retained-session continuity evidence - 2026-11-11

## Status

**Locally complete under the owner-approved preserved-unsupported scope.**
Goal contract: [`archive/0006c-retained-session-continuity.md`](../goals/archive/0006c-retained-session-continuity.md),
including its 2026-11-11 scope clarification.

The owner selected the preserved-unsupported scope on 2026-11-11: Windows
retained resume and Linux/Windows compatible fresh handoff remain the continuity
paths for this goal; Linux v2 retained resume stays explicitly unsupported
until a separately fenced B-clone reuse grant exists. This receipt is
working-tree evidence, not exact-commit release acceptance or native capability
promotion.

Baseline `HEAD`: `a04c4ab` (`chore: ignore local agent artifacts`). No commit
was created. Goal 0006i still owns exact-Subject acceptance.

## Changed behavior

The existing Control reservation, local Goal-session journal, resume CAS,
handoff lineage, restart recovery, attach/stop receipts and fail-closed Linux
resume behavior were already present at the baseline. This change adds one
missing provenance guard:

- `daemon/internal/harness/pi/adapter.go` now requires a retained pi session
  file to resolve inside the profile-declared `--session-dir` when that option
  is present. Relative, malformed, duplicate or escaping session directories
  are rejected as `resume_rejected` before the native process starts.
- `daemon/internal/harness/pi/adapter_test.go` exercises the accepted in-directory
  resume and the rejection paths.

The guard is deliberately conditional: a profile without `--session-dir` keeps
pi's native default directory behavior, matching the baseline contract. Linux
v2 native profiles already require the exact provisioned B session directory.

## Requirement evidence

| Requirement | Evidence | Result |
| --- | --- | --- |
| Fresh start | Daemon fresh Goal admission and staged native lifecycle tests; Control goal admission tests | Pass |
| Retained resume, Windows | `TestPiResumeAdmissionReusesRetainedNativeSessionAndWorkspace`, `TestAdapterResumesExactPiSessionAtAnIdleBoundary`, resume rebind and recovery tests | Pass |
| Retained resume, Linux | `TestLinuxV2PiResumeRequiresSeparateCloneReuseGrant`; five Windows resume app tests skip with the exact B-clone reuse reason | Explicit unsupported; fails closed before workspace or native effects |
| Compatible fresh handoff | `TestNativeHandoffAdmissionsUseOnlyVerifiedNativeOwner` (Codex positive on Windows and Linux; pi/OpenCode positive on Windows and Linux-v2 owner preflight fail closed), Control handoff source/task/settlement tests, handoff replay and context-source assertions | Pass |
| Restart recovery | `TestGoalSessionJournalRoundTripAcrossRestart`, `TestRebindGoalSessionForResumeSurvivesStoreRestart`, app recovery tests for pre-start and post-start resume crashes | Pass |
| Duplicate delivery | Exact rebind replay, late predecessor stop receipt, attach/stop delivery replay, assignment replay without a second native start | Pass |
| Stale source rejection | `TestRebindGoalSessionForResumeRejectsStaleCertificateAndCompetingCAS`, Control stale-handoff and consumed-source tests | Pass |
| Session directory provenance | New in-directory resume test, outside/relative/duplicate/malformed/non-directory rejection subtests, symlink-escape rejection and in-directory alias acceptance; native pi test asserts the observed file under its isolated session directory | Pass for configured directories; native pi test skipped without credentials |
| Attachment and stop receipts | State attach/stop receipt persistence tests, app late-ack and predecessor-binding tests, Control machine receipt replay tests | Pass |
| Cleanup after partial failure | `TestRecoveryCompensatesReboundPiSessionBeforeNativeStart`, `TestPiResumeRejectedBeforeNativeStartReleasesExactNewBinding`, unready-attach retirement and Linux clone abort tests | Pass |

## Checks executed

| Check | Result |
| --- | --- |
| Windows `go test ./internal/state ./internal/protocol ./internal/harness/pi ./internal/app -count=1` | Pass: state 43.0s, protocol 3.0s, pi 9.3s, app 148.2s |
| Linux `symmetry-race` WSL, same affected packages | Pass: state 66.9s, protocol 5.8s, pi 3.6s, app 134.5s |
| Linux `go test -race` on the same affected packages | Pass: state 163.7s, protocol 9.8s, pi 8.6s, app 505.9s |
| Linux focused `go test -race ./internal/harness/pi -run 'Resume|Symlink'` | Pass: changed provenance guard and symlink cases |
| Windows `go vet ./...` | Pass |
| Linux WSL `go vet ./...` | Pass |
| Focused pi resume/provenance tests, Windows and Linux | Pass: `TestAdapterResumesExactPiSessionAtAnIdleBoundary`, 12 rejection subtests, `TestAdapterRejectsPiResumeSymlinkOutsideSessionDirectory`, and `TestAdapterAcceptsPiResumeSymlinkInsideSessionDirectory` |
| Focused native handoff test, Windows and Linux | Pass: `TestNativeHandoffAdmissionsUseOnlyVerifiedNativeOwner` with Codex positive on both OSes; pi/OpenCode positive on Windows and fail closed at the Linux v2 owner preflight |
| Control focused tests: `orchestration_goal_admission_test.exs`, `goal_machine_controller_test.exs`, `goal_controller_test.exs`, `goals_test.exs` | Pass: 221 tests, 0 failures, 1 external skip (`committed harness session receipts recover through real Go HTTP after listener restarts`, gated by `SYMMETRY_GOAL_RECEIPT_E2E=1`) |
| `mix compile --warnings-as-errors` in `control/` with `MIX_ENV=test` | Pass |
| `mix format --check-formatted` in `control/` | Pass |
| Contract typecheck `tsc --project contracts/tsconfig.json` | Pass |
| Contract script tests `node --test contracts/scripts/*.test.mjs` | Pass: 27 tests |
| `node contracts/scripts/check.mjs` | Pass: 14 schemas, 184 fixtures |
| `node contracts/scripts/check-effect.mjs` | Pass: 184 fixtures decoded |
| `pnpm contracts:check` wrapper | Not run: installed pnpm 12.18.0 conflicts with the project pin 12.17.0; the four constituent commands above were run directly |

## Not run or explicitly unsupported

- **Linux v2 retained resume.** Every Linux pi/OpenCode native run owns a fresh
  B clone per RunKey. Reusing that clone requires a separately fenced B-clone
  reuse grant covering the predecessor stop receipt, target inode/device,
  A/B authority, new RunKey and workspace fingerprint. That grant is not
  implemented here; resume admissions return `resume_rejected` before
  workspace, clone, Control attach or native start. Goal 0006f cannot claim
  Linux retained-resume evidence until this prerequisite is implemented and
  verified on a qualified host.
- **Real native pi retained resume.** The opt-in
  `TestNativeRepositoryTaskRetainedResume` was skipped because the native
  executable, credentials and provider route were not configured. The recorded
  0.87.1 Windows native attempt failed at the shared runner
  `OutputTruncated` termination-barrier assertion after semantic settlement;
  the same failure also occurred with 0.85.1 on that working tree. The earlier
  native retained-resume receipt is for pi 0.85.1. The owner directive of
  2026-11-11 now pins 0006f to Pi 1.5.2, so neither the 0.85.1 nor 0.87.1
  observation satisfies the current goal. This known failure remains a
  native-adapter prerequisite, not merely unavailable credentials.
- **Windows race instrumentation.** `go test -race` requires cgo and a Windows C
  toolchain; this machine has neither. The Linux race run is the concurrency
  evidence for the changed adapter path.
- **Installed A/B Linux v2 handoff positive.** No qualified systemd A/B host or
  delegated cgroup-v2 environment was available. Control scheduler tests and
  daemon fail-closed tests cover the mechanism; they do not substitute for an
  installed-owner witness.
- **Exact-subject acceptance.** No commit or clean checkout exists. This receipt
  cannot be promoted to release evidence; Goal 0006i must bind every predicate
  to one exact revision.
- **Concurrent path replacement after validation: not closed.** The guard is a
  point-in-time containment check. Pi 1.5.2 accepts only a session path,
  re-resolves it lexically, and later reopens it for append; it exposes no
  fd/import API. A writer with access to the session directory can replace the
  leaf, an ancestor, or the content between validation and Pi's open. On Linux
  v2 that writer must be the fixed-B principal (A is denied read/write on the B
  session directory); on Windows it is the daemon user. `get_state` checks only
  the retained ID/path string, and Pi's session header supplies its effective
  context/cwd. This is an intra-principal session-provenance exposure, not a
  cross-principal file read, and the daemon has not proven a no-writer
  invariant. A staging copy would change native path/session semantics; an fd
  handoff would require a Pi API that does not exist. The decision is therefore
  to keep the bounded check and state the limitation rather than claim closure.

## Review disposition

An independent read-only review found no demonstrated containment bypass and no
weakening of a fixed fence or capability contract. It required two evidence
fixes:

1. Add symlink coverage for the provenance guard. Done: escape rejection and
   in-directory alias acceptance both pass on Windows and Linux.
2. Record the final Linux race outcome instead of a placeholder. Done in the
   checks table above.

The reviewer also identified the concurrent path-replacement window as a
speculative residual risk; it is documented above rather than closed. The
change remains a narrow provenance guard plus scope/evidence updates. It does
not add a harness capability, change provider policy, transfer a native handle
across a handoff, weaken a fence, or authorize Linux clone reuse.
