# Goal 0006b convergence and acceptance

Historical acceptance contract: the owner's 2026-10-10 bounded-iteration
amendment in [Goal 0006b](../goals/archive/0006b-crash-safe-process-containment.md)
supersedes this document's full-matrix/native/CI prerequisites for local Goal
closure. This record is retained for provenance, not an instruction to restore
those prerequisites or a claim that the missing evidence passed.

## Observable outcome

The existing Linux v2 containment implementation is buildable, runnable,
independently reviewable and eligible for acceptance at one exact Git commit.
Every in-scope test, fixture and installation/CI entry point has an explicit
behavioral responsibility. Duplicate, non-runnable or non-discriminating checks
are removed only after equivalent behavior coverage exists. This is one goal:
passing compilation alone does not establish containment acceptance.

## Scope and non-goals

Only code, fixtures, installation/CI scripts and tests belonging to
[0006b](../goals/archive/0006b-crash-safe-process-containment.md) are in scope. No product
capability expansion, harness capability promotion, new scheduler, whole-repo
refactor, production-branch push or early Goal archival is permitted.

## Immutable safety boundaries

Preserve RunKey/fence authority, journal compatibility, exact version-matched
physical StopReceipt evidence, crash/lost-response replay, distinct A/B identity
and permissions, Windows behavior and real Pi/OpenCode positive acceptance.
The surviving A anchor remains the only v2 cgroup.kill writer. Unknown stop,
identity, kill acknowledgement, reaping or receipt outcomes remain fail closed.
Never delete or fabricate unresolved journals, physical-stop proof or user work.
A physically released run retaining its checkout is not unresolved containment
and is not completed checkout cleanup. Unproven stop is never physical release.
Each unresolved acceptance scenario has an independent guest; erasing its journal
to admit the next scenario is prohibited. Evidence exports contain no credentials.

## Completion evidence

- One exact tested commit binds the source, production binaries, fixture
  contract, commands, results, platform observations and independent review.
  A dirty working tree or evidence from an older commit is not that subject.
- Installed StateDir and App-owned RunJournal agree with CI fixtures. Retained
  released records and unresolved stop records remain observably distinct.
- The executable matrix includes quick builds/unit tests, Linux and Windows
  regressions, WSL2 rejection, rootless Compose rejection, and qualified Linux
  A/B Pi/OpenCode positive and crash/replay witnesses. Tests can detect real
  regressions; ordinary Runner v2 peer units do not certify native acceptance.
- Production witnesses cover prepare, bind, commit, lease arm, resume, receipt,
  release and clear, including lost responses and hard crashes; detached native
  descendants stop within the existing contract. Windows retains its first-client
  wrong-secret rejection and legitimate hello within the same deadline.
- Pi provider-action evidence uses an authentic Control-issued claim-scoped token
  and isolated Integration. Synthetic success and unverified capabilities do not
  substitute. Independent safety/recovery and scoped simplification review have
  no unresolved blocking findings on the tested subject.
- The completion report identifies every removal and its replacement coverage,
  actual commands/results, exact revision, real runtime evidence and missing
  host, credentials or CI evidence. Verified logical increments are the only
  eligible commits; unrelated pre-existing work stays intact.

## External conditions and scope stop

The current WSL2 host supplies rejection evidence only. Positive acceptance
requires the approved amd64 systemd host, exact A/B peer checks, operator-owned
provisioning and protected native/provider inputs. Missing external conditions
mean positive acceptance has not passed: finish unaffected work, report the
blocker and keep original Goal 0006b active. No model or test count can waive it.
Stop expanding scope when remaining work would require a new capability, changed
authority/acceptance contract, unrelated cleanup or unavailable external input.
Do not weaken a gate to manufacture a pass; report any required contract decision.

Baseline inspection: branch `codex/goal-0006b-crash-safe-process-containment`,
HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`; 64 tracked paths were already
modified and 526 untracked paths existed. That dirty baseline is not an accepted
test subject. Historical evidence and raw journals are not cleanup candidates.
