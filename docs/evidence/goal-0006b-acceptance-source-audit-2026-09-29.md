# Goal 0006b acceptance source integration audit

Date: 2026-09-29

Mode: read-only source/history investigation; this report is the only assigned write.
Task: `st_01a0ee08`.
Requested route: `deep-low`.
Actual route: provider/model `ghc/gpt-5.6-sol-fast`, reasoning level `medium`.

## Decision

Do not blanket-copy, stage, or commit the shared checkout. Create the goal
worktree from current committed `HEAD`, then transfer only the authorized path
set below. Four final source files contain both Goal 0006b and later
project-test-ablation work. They are authorized but ownership-ambiguous at
whole-file granularity; keep their final bytes in one source integration group
unless trusted pre-ablation objects are supplied.

The smallest dependency-complete transfer supported by current evidence is:

1. retain the existing 16 commits beyond PR #9's current remote tip;
2. one combined source/test/tooling commit containing the 114 authorized source
   paths below, and no unrelated or generated/private path;
3. one Goal convergence evidence commit (12 paths including all three audits); and
4. one project test-ablation evidence commit (14 paths), if those reports are
   intended for the branch.

This is not blanket inclusion. Exclude `docs/design/README.md`,
`NUL`, `.omo/**`, and `.plans/**`. Goal 0006b remains **ACTIVE / INCOMPLETE**.
This map is not acceptance evidence.

## Repository, history, and staged state

CLI observations from this task:

- Repository: `Q:/repos/symmetry`.
- Branch: `codex/goal-0006b-crash-safe-process-containment`.
- `HEAD`: `d14debbf6ae3ded09e5013ef90952d347c01474d`.
- PR #9 is open to `main`; its remote branch currently ends at
  `85a7968511f0b499ca205b361d11b5d29e380e62`.
- `git rev-list --left-right --count origin/codex/goal-0006b-crash-safe-process-containment...HEAD`
  returned `0 16`: local is exactly 16 commits ahead and not behind. The linear
  range `85a7968..d14debb` is a committed prerequisite chain.
- `git diff --cached --name-status` and an independent cached-path count both
  returned zero paths. **Nothing is staged.** All 67 tracked statuses are ` M`;
  there are no tracked adds, deletes, renames, or mode-only changes.
- Before this report, porcelain status had 806 records: 67 tracked changes, 72
  relevant untracked paths, 185 `.omo/**`, and 482 `.plans/**`. Thirty-five
  `.plans` paths match historical `state/runs/journal-*` material. Per scope,
  `.omo`, `.plans`, and journals are excluded. This report raises the relevant
  untracked count to 73 and is the only added path from this task.

During final verification, a concurrent child added
the CI and environment acceptance audits; after removal of an `apply_patch`
backup, status showed 75 relevant untracked paths and 193 `.omo` records. Those
audits change no source or index entry and do not alter the pre-write baseline.

The requested `goal-0006b-convergence-inventory-2026-09-29.md` does not exist in
the worktree and has no `git log --all` path history. No evidence is invented
from it. The current inventories are the daemon-test and fixture reports below.

Classifications mean: **Goal-owned** is identified by the active Goal,
convergence receipts, or implementation dependencies; **test-ablation-owned**
is identified by the project ablation receipts; **unrelated** is expressly
outside Goal/ablation scope; **generated/private** is session/runtime material;
and **ownership-ambiguous** contains separately authorized Goal and ablation
hunks in one whole current file.

## Complete tracked-path accounting (67)

### Goal-owned (62)

```text
daemon/cmd/symmetry-daemon/main.go
daemon/cmd/symmetry-linux-launcher/c-wrapper/wrapper.c
daemon/cmd/symmetry-linux-launcher/main.go
daemon/e2e/core_test.go
daemon/e2e/production_daemon_crash_witness_test.go
daemon/e2e/production_linux_containment_negative_witness_test.go
daemon/e2e/production_linux_containment_replay_witness_test.go
daemon/e2e/production_linux_containment_witness_test.go
daemon/e2e/production_pre_authority_crash_witness_test.go
daemon/internal/app/app.go
daemon/internal/app/cleanup.go
daemon/internal/app/cleanup_test.go
daemon/internal/app/harness_test.go
daemon/internal/app/output_settlement_test.go
daemon/internal/app/pi_provider_bridge.go
daemon/internal/app/pi_provider_bridge_test.go
daemon/internal/app/process_persistence_test.go
daemon/internal/app/recovery.go
daemon/internal/app/recovery_containment_linux.go
daemon/internal/app/recovery_isolation_test.go
daemon/internal/app/recovery_linux_test.go
daemon/internal/app/terminate_linux.go
daemon/internal/authority/authority.go
daemon/internal/authority/authority_linux_clone_test.go
daemon/internal/execution/linux_owner_v2_linux.go
daemon/internal/execution/linux_owner_v2_test.go
daemon/internal/execution/runner.go
daemon/internal/harness/harness_test.go
daemon/internal/harness/opencode/adapter.go
daemon/internal/harness/opencode/adapter_test.go
daemon/internal/harness/opencode/peer_test.go
daemon/internal/harness/pi/adapter.go
daemon/internal/harness/pi/adapter_test.go
daemon/internal/harness/pi/provider_bridge.go
daemon/internal/harness/pi/provider_bridge_peer_test.go
daemon/internal/harness/pi/provider_extension.go
daemon/internal/harness/pi/provider_extension_test.go
daemon/internal/platform/containment_supervisor_linux.go
daemon/internal/platform/containment_supervisor_linux_test.go
daemon/internal/platform/containment_supervisor_production_windows_test.go
daemon/internal/platform/containment_supervisor_windows.go
daemon/internal/platform/containment_supervisor_windows_test.go
daemon/internal/platform/linux_owner_anchor_linux.go
daemon/internal/platform/linux_owner_anchor_linux_test.go
daemon/internal/platform/linux_owner_anchor_unsupported.go
daemon/internal/platform/linux_owner_helper_v2_linux.go
daemon/internal/platform/process_unix.go
daemon/internal/platform/process_unix_linux_test.go
daemon/internal/platform/tcp_peer.go
daemon/internal/platform/tcp_peer_linux.go
daemon/internal/state/control.go
daemon/internal/state/linux_cgroup_v2_test.go
daemon/internal/state/linux_clone_journal_test.go
daemon/internal/state/state.go
daemon/internal/workspace/linux_clone_fs_linux.go
daemon/internal/workspace/linux_clone_linux.go
daemon/internal/workspace/linux_clone_linux_test.go
daemon/internal/workspace/workspace.go
daemon/native/linux-gate/c-gate/gate.c
daemon/native/linux-gate/protocol/launcher_integration_linux_test.go
daemon/native/linux-gate/protocol/launcher_linux.go
scripts/install-linux-owner.sh
```

The Goal requires these App, state, workspace, execution, launcher, platform,
harness, native, legacy-Linux, and Windows-preservation layers. The inventories
say similarly named unit, installed, and production witnesses are not substitutes.

### Test-ablation-owned (2)

```text
control/test/symmetry_control/goals/contract_validation_test.exs
control/test/symmetry_control_web/controllers/goal_controller_test.exs
```

The Control experiment and final review bind these files. They retain exact
public errors and direct Plug/canonical HTTP boundaries after consolidation.

### Ownership-ambiguous (2)

```text
.github/workflows/ci.yml
daemon/internal/app/app_test.go
```

Both had substantial Goal changes before ablation. Ablation later removed two
repeated workflow Go processes, replaced four inline parsers, and consolidated
the App startup test. The same files still contain Goal-only workflow and Linux
v2/recovery fixture work. Do not attribute either whole file to only one track.

### Unrelated (1)

```text
docs/design/README.md
```

Its sole diff adds "Apple Silicon arm64 when implemented" to deferred macOS.
No Goal or ablation receipt claims this separate design change. Exclude it.

### Generated/private

No tracked paths.

## Complete relevant untracked accounting before this report (72)

### Goal-owned daemon source/tests (26)

```text
daemon/cmd/symmetry-daemon/main_test.go
daemon/cmd/symmetry-linux-launcher/main_test.go
daemon/e2e/production_linux_v2_cleanup_witness_test.go
daemon/e2e/production_linux_v2_helper_exit_witness_test.go
daemon/e2e/production_linux_v2_native_witness_test.go
daemon/e2e/production_linux_v2_owner_cli_test.go
daemon/e2e/production_linux_v2_rootless_compose_witness_test.go

daemon/internal/app/linux_owner_preflight_linux.go
daemon/internal/app/linux_owner_preflight_linux_test.go
daemon/internal/app/linux_owner_preflight_unsupported.go
daemon/internal/app/linux_owner_recovery_linux.go
daemon/internal/app/linux_owner_v2_app_test.go
daemon/internal/app/linux_owner_v2_linux.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/app/linux_owner_witness_linux_test.go
daemon/internal/app/linux_owner_witness_unsupported.go
daemon/internal/app/linux_owner_witness_unsupported_test.go
daemon/internal/harness/opencode/peer_linux_test.go
daemon/internal/harness/pi/provider_bridge_peer_linux_test.go
daemon/internal/platform/linux_owner_helper_v2_linux_test.go
daemon/internal/platform/tcp_peer_linux_v2_test.go
daemon/internal/platform/tcp_peer_v2_unsupported.go
daemon/internal/state/linux_clone_cleanup_reader_linux_amd64.go
daemon/internal/state/linux_clone_cleanup_reader_unsupported.go
daemon/native/linux-gate/protocol/clone_cleanup_linux.go
daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go
```

Linux and unsupported build-tag counterparts are one dependency unit.

### Goal-owned CI/fixture tooling (20)

```text
scripts/ci/export-linux-v2-redacted-evidence.py
scripts/ci/linux-v2-host-peer-probe.go
scripts/ci/linux-v2-host-prerequisite-probe.sh
scripts/ci/linux-v2-native-acceptance.sh
scripts/ci/linux-v2-native-tools.sh
scripts/ci/linux-v2-source-subject.py
scripts/ci/literal-pid-reuse-helper.c
scripts/ci/prepare-rootless-compose-owner-claim.py
scripts/ci/provision-linux-v2-control-claims.exs
scripts/ci/publish-linux-v2-control-receipt.py
scripts/ci/register-linux-v2-claim-machine.py
scripts/ci/rootless-compose-native-acceptance.sh
scripts/ci/rootless-compose-native-daemon.Dockerfile
scripts/ci/rootless-compose-transition-proxy.py
scripts/ci/test-linux-v2-evidence.py
scripts/ci/test-linux-v2-source-subject.py
scripts/ci/verify-linux-v2-helper-exit-checkpoint.py
scripts/ci/write-linux-v2-owner-request.py
scripts/ci/wsl2-v2-denial-witness.sh
scripts/cleanup-linux-pi-processes.sh
```

The fixture inventory assigns each a source-binding, acquisition, claim,
exporter, refusal, checkpoint, or installed-witness responsibility. They are
source inputs, not generated artifacts. The concurrent CI audit shows the
cleanup script is referenced twice by the workflow and was absent from the
historical PR tree, causing fixture exit 127 before Pi Control E2E could run.

### Goal-owned convergence evidence (9 before, 12 after concurrent audits)

```text
docs/evidence/goal-0006b-convergence-goal-2026-09-29.md
docs/evidence/goal-0006b-convergence-receipt-2026-09-29.md
docs/evidence/goal-0006b-daemon-test-inventory-2026-09-29.md
docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md
docs/evidence/goal-0006b-resumed-go-gates-2026-09-29.md
docs/evidence/goal-0006b-resumed-test-review-2026-09-29.md
docs/evidence/goal-0006b-safety-review-2026-09-29.md
docs/evidence/goal-0006b-simplification-review-2026-09-29.md
docs/evidence/goal-0006b-surface-qa-2026-09-29.md
docs/evidence/goal-0006b-acceptance-source-audit-2026-09-29.md
docs/evidence/goal-0006b-acceptance-ci-audit-2026-09-29.md
docs/evidence/goal-0006b-acceptance-environment-audit-2026-09-29.md
```

These are convergence/history evidence only. Dirty-snapshot results must not be
relabeled exact-commit acceptance.

### Test-ablation-owned evidence (14)

```text
docs/evidence/project-test-ablation-2026-09-29.md
docs/evidence/test-ablation-app-startup-experiment-2026-09-29.md
docs/evidence/test-ablation-ci-peer-experiment-2026-09-29.md
docs/evidence/test-ablation-contract-experiment-2026-09-29.md
docs/evidence/test-ablation-control-2026-09-29.md
docs/evidence/test-ablation-control-experiment-2026-09-29.md
docs/evidence/test-ablation-final-gates-2026-09-29.md
docs/evidence/test-ablation-final-review-2026-09-29.md
docs/evidence/test-ablation-go-core-2026-09-29.md
docs/evidence/test-ablation-go-probe-experiment-2026-09-29.md
docs/evidence/test-ablation-go-surfaces-2026-09-29.md
docs/evidence/test-ablation-inline-parser-experiment-2026-09-29.md
docs/evidence/test-ablation-manifest-2026-09-29.md
docs/evidence/test-ablation-tooling-2026-09-29.md
```

The project report records explicit authorization beyond Goal 0006b; it does not
change Goal authority or acceptance conditions.

### Ownership-ambiguous (2)

```text
scripts/ci/assert-go-test-json.py
scripts/ci/test-linux-v2-go-events.py
```

The Goal inventory identifies both as convergence helpers. C5 later added
package scoping and expanded tests from two to six methods; C4 reused the final
helper. Because the files are untracked, Git has no pre-ablation index object.
Keep their final bytes in the mixed source group.

### Generated/private (1 relevant-name path, plus excluded trees)

```text
NUL
```

`NUL` is a Windows device-name artifact with no source consumer. Never add it.
Also exclude all 185 `.omo/**`, 482 `.plans/**`, and 35 identified historical
journal records. Do not delete them or treat deletion as containment proof.

## Authorized source group and atomic boundaries

Boundary 0 is committed base `d14debb`, preserving all 16 local commits. Starting
at `85a7968` and copying dirty files would omit committed dependencies.

Boundary 1 is one final source/test/tooling commit:

- 62 Goal-owned tracked paths;
- 26 Goal-owned untracked daemon paths;
- 20 Goal-owned untracked CI/fixture paths;
- 2 ablation-owned tracked Control tests; and
- 4 mixed final-source paths (`.github/workflows/ci.yml`,
  `daemon/internal/app/app_test.go`, `scripts/ci/assert-go-test-json.py`, and
  `scripts/ci/test-linux-v2-go-events.py`).

Total: **114 paths**. Evidence, unrelated paths, and private/generated material
are not in this commit.

This is the only dependency-complete byte-exact source boundary currently
supported: App/harness consume new execution/workspace/authority interfaces;
state journals serialize platform/native proofs; launcher, installer, and config
assumptions must agree; witnesses/scripts consume hidden CLI and StateDir
contracts; workflow jobs consume the untracked helpers; and Linux/unsupported
pairs preserve cross-platform builds.

This is the candidate **input**, not a verified commit-ready snapshot. The
concurrent environment audit found `scripts/ci/wsl2-v2-denial-witness.sh` uses
`-d /sys/fs/cgroup/cgroup.controllers` although that cgroup-v2 marker is a
regular file. The isolated worktree must receive the focused `-d` to `-f` fix
and verification before Boundary 1 can be called verified or committed.


Strict concern-separated commits require trusted pre-ablation objects matching
the hashes in experiment receipts. The scratch snapshots were reported removed;
hashes alone cannot reconstruct bytes. Do not reverse-engineer them from prose.

Boundary 2 is the twelve Goal evidence reports, including all three audits. Boundary 3 is
the fourteen ablation reports as one experiment ledger. Do not commit only the
favorable final report without its inventory, mutations, limits, and review.

## Non-destructive transfer procedure

1. Re-record shared `HEAD`, empty index, and NUL-delimited status. Abort if
   `HEAD` differs from `d14debb...`, the index is nonempty, or authorized file
   hashes changed; concurrent work has changed the subject.
2. From outside the shared checkout, create an isolated worktree from exact
   `d14debb`, for example:
   `git -C Q:/repos/symmetry worktree add -b <new-goal-branch> <new-path> d14debbf6ae3ded09e5013ef90952d347c01474d`.
   This neither switches nor cleans the shared checkout.
3. Generate a binary patch for authorized tracked paths, excluding
   `docs/design/README.md`. Generate add-file patches for each explicitly listed
   untracked source path (`git diff --no-index --binary -- /dev/null <path>` in
   Bash). Do not recurse from repository root and do not use `git add -A`.
4. Apply exactly Boundary 1 in the isolated worktree. Confirm
   `NUL`, `.omo`, and `.plans` are absent
   from candidate additions. Stage explicit pathspecs, inspect
   `git diff --cached --name-status` and `git diff --cached --check`, verify the
   candidate, then commit.
5. Transfer Goal and ablation evidence as Boundaries 2 and 3 with explicit
   pathspecs. Do not stage, clean, remove, rename, or switch this checkout.

If add-file patching is awkward on Windows, a script may read each explicit
allowlisted file and write only to the isolated worktree, then compare SHA-256.
It must reject paths outside this manifest and never copy directory roots.

## Verification and dependency risks

Checks actually run here: branch/HEAD/recent history, remote divergence, PR #9
metadata and commit list, porcelain-v2 and NUL-delimited porcelain-v1 status,
cached-state checks, tracked name/num/stat diffs, `git diff --check`, touched-path
history, root/daemon AGENTS, active Goal, convergence receipts/inventories/review,
surface QA, project ablation, experiments, final gates, and final independent
review. `git diff --check` exited 0 before this report.

No suite, build, service, installer, witness, or credentialed command ran;
whole-suite reruns were barred. No existing source, index entry, branch,
worktree, journal, or runtime artifact was changed.

Candidate risks:

1. Existing broad passes bind dirty snapshots, not the future commit. Re-run
   required gates on the clean exact candidate; do not say they carry over.
2. Omitting mixed-file Goal hunks breaks witness/interface wiring; omitting
   ablation hunks invalidates final hashes and review.
3. A tracked-only patch omits most Linux v2 tests, shims, and scripts.
4. Missing one build-tag counterpart can break another platform.
5. Missing any workflow helper makes the candidate CI non-runnable.
6. Legacy Linux and Windows paths remain required; v2 does not replace them.
7. Raw witness/journal trees are protected recovery material, not source or
   public artifacts.
8. Clean integration does not close missing qualified A/B, Pi/OpenCode,
   authentic WSL/rootless, remote CI, or durable raw-storage evidence.
9. The current workflow requires `scripts/cleanup-linux-pi-processes.sh`;
   omitting it reproduces the historical exit-127 packaging failure before Pi
   Control E2E can run.

## Acceptance and ownership conclusion

The convergence receipt and project-ablation report agree on the central limit:
there is no clean exact tested commit. Local complete Linux ordinary/race and
focused Windows/Control results are useful dirty-tree evidence only. Qualified
amd64 systemd A/B Pi/OpenCode positives, authentic WSL denial, rootless runtime
refusal, final remote CI, protected raw archival, and exact-subject review remain
missing. The source integration commit enables those gates; it does not satisfy
them.

No ownership question blocks candidate construction. Receipts resolve the
114-path source group and both evidence groups. Exclude the macOS wording,
`NUL`, `.omo`, and `.plans`. The only optional future decision is whether the
unrelated macOS wording belongs on another branch or PR; it is not a Goal 0006b
candidate input.
