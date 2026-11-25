# rf-acc-owner-loss-semantics — evidence record

Goal: `rf-acc-owner-loss-semantics` (contract `goal-rf-acc-owner-loss-semantics.md`, GDD state root).
Branch `refactor/rf-acc-owner-loss-semantics`, base revision `6b2d4f8f` (#285 supplies this goal's
Q2(a) gate, #284 the refusal vocabulary, #280 the containment-witness legs). Conductor rulings:
Q1 = (a) — the containment half stays as implemented; Q2 = B1 (d1b) with (c2), (d2) and (b)
rejected; Q3 = (a); Q4 = (a). Consults: `findings/rf-acc-owner-loss-consult-{sol,ds}.md`,
`findings/rf-acc-owner-loss-d1d2-{sol,ds}.md`, `findings/rf-acc-owner-loss-d1b-{sol,ds}.md`. Plan:
`docs/plans/2026-11-25-rf-acc-owner-loss-semantics-plan.md`.

Line numbers below are at the delivered bytes unless a section says otherwise; the run tables name
their own head revisions. Records cite final bytes by sha256 only (never commit/head provenance
claims); docs bytes are never sha256'd.

## 1. Clause census at the delivered bytes (re-located by content)

| Contract clause | Delivered bytes | Disposition |
| --- | --- | --- |
| Change 1 consult + guard table | `findings/rf-acc-owner-loss-*`; §3a below | done before dispatch; guard table completed here |
| Change 2/3/4 ruled as d1b | §2.1-§2.6 | implemented |
| Change 5 observability | #285's refusal vocabulary unchanged; the d1b refusal is replaced by the named retention outcome (§2.3) | satisfied |
| Change 6 records | this record, the plan, `docs/runbooks/daemon-process-stop.md:99-133`, `docs/TODO.md:1979-1992` | implemented |
| Carried #284 items (Sec F1, DS F5, Sol P3) | §2.7 | implemented |
| Gates: four `stale_journal_*` sets on a hosted runner | §7 | run, see §7 |
| Owned set (amended by the d1b ruling) | `daemon/internal/app/linux_owner_witness_linux.go`, `daemon/internal/state/linux_clone_validation.go`, `daemon/internal/state/linux_cgroup_journal.go`, their tests, `daemon/e2e/production_linux_v2_{native_witness,owner_cli,owner_identity}_test.go`, the runbook, TODO | matches; nothing outside it edited |

## 2. Changes

1. **d1b shape predicate** (state, new): `linuxV2ReleasedCloneRetainedAfterRelease`
   `daemon/internal/state/linux_clone_validation.go:53-67`, called as an added arm of
   `linuxV2ReleasedCloneStateSafe` (`:44-46`). Every term is required — valid intent
   (`ValidateForJournal`), seed receipt, no checkout/promotion/abort/cleanup field, no reuse grant,
   no native-usage recovery, `WorkspacePath == intent.Target`, `WorkspaceBindingKey ==
   intent.BindingKey`, `WorkspaceRecoveryRequired` — so any other shape keeps blocking foreign
   admission. The caller (`linuxV2ForeignJournalIsSafelyRetired`, release branch) already supplies
   the release-proof, leaf-disposition, `ContainmentCgroup == nil` and no-live-authority terms.
2. **C3 cgroup leaf closure** (state, new): `linuxV2RetainedReleaseNamesLeaf`
   `daemon/internal/state/linux_cgroup_journal.go:423-431`, called from the admission scan's
   `reservation != nil` branch (`:394-402`) for exactly the d1b shape; the existing
   reservation-leaf refusal is unchanged and the seed/target alias block above it still runs
   (`:384-393`, no early `continue`).
3. **Witness retention outcome** (app): `linuxV2NativeWitnessClone.Retention`
   `daemon/internal/app/linux_owner_witness_linux.go:209`; the third branch at `:521-535` always
   calls `cleanupRecoveredWorkspace` and classifies its refusal only when the reloaded row is
   byte-identical and is the retained pre-checkout shape; classifier `:3082-3109`; closed
   vocabulary + summary validation `:3106-3119` and `:744-747`. No journal field is added: the
   durable retention reason is the row's own shape.
4. **Q3(a) pin discipline** (e2e): the crash-replay runner keeps pinning the resident anchor live
   after the kill (`daemon/e2e/production_linux_v2_owner_cli_test.go:1952-1956`) and hands the helper
   PIDFD to the leg (`:1920`, `:1959`); the `afterStageProcessKilled` contract now carries both
   PIDFDs (`production_linux_v2_native_witness_test.go:991-994`); the fresh-request leg pins the
   anchor and asserts the helper's owner-loss exit before and after the denial (`:437-444`,
   `:520-522`), waiting 90 s — above the helper's own 30 s `linuxOwnerHelperV2StopTimeout`
   (`internal/platform/helper_v2_protocol.go:33`) so the fixture cannot race the fail-safe (§7.2).
5. **Retention assertions** (e2e, new): `linuxV2RequireRetainedPreCheckoutClone`
   `production_linux_v2_owner_cli_test.go:2274-2291` (journal) and `linuxV2RequireRetainedCloneOutcome`
   `:2292-2303` (response), wired into the released stage-replay arm (`:1246-1248`) and the
   crash-replay runner for every released replay whose clone never checked out (`:1984-1986`).
6. **Admitted fresh leg** (e2e, new): after the setup recovery,
   `production_linux_v2_native_witness_test.go:537-632` asserts the retained setup row, then the same
   fresh request the unresolved row denied is admitted, reaches its own `handoff_committed` boundary,
   is killed and recovered to the same released retained shape, with the setup row byte-identical
   throughout. The aliasing denial is pinned at the state layer (§5) because a request cannot mint
   aliasing clone paths (they are RunKey-derived).
   The fixture builder's shared-State cleanup comparator (`production_linux_v2_native_witness_test.go:1933-1940`)
   takes the admitted run's key from a new `allowedNewRunKeys` field the leg fills only after it has
   proven that journal's exact boundary (`:600`), so exactly one additional new journal is
   tolerated while every previously retained row must still be byte-identical.
7. **Carried #284 items** (e2e): `linuxV2JournalBindsOwnerSlot` now projects PID and creation
   identity (`owner_cli_test.go:1639-1709`) and `linuxV2RequireSameOwnerIdentities` binds every
   journal-bound slot to that projection (`:1742-1744`), so a consistently swapped marker/response
   set reddens; new rows at `owner_identity_test.go:110-127` (swap, equal-PID/different-identity) and
   the phase-branch table at `:152-237`.
8. **Records**: runbook section `docs/runbooks/daemon-process-stop.md:99-133` (retention semantics
   plus the read-only jq locator), TODO item `docs/TODO.md:1979-1992` (authorized disposition
   command), plan `docs/plans/2026-11-25-rf-acc-owner-loss-semantics-plan.md`.

## 3. Censuses

### 3a. Guard table (contract Change 1(iii)) with each ruled disposition

| Guard (contract static trace) | Delivered bytes | Disposition |
| --- | --- | --- |
| 1. Exact-RunKey select + clone-intent identity | `linux_owner_witness_linux.go:2636-2723` | verified, no edit: satisfied by the same request/Subject |
| 2. `recoverLinuxV2OwnerJournal` anchor/lookup/helper-death guards | `linux_owner_recovery_linux.go:108-300` | verified, no edit: the dead-helper path (`helper.Control == nil` → `WaitReapRun` → `bindRecoveredV2AnchorGate`, `:1244-1257`) consumes the anchor-authenticated owner-loss stop evidence; no guard requires a live helper once `LookupHelper` reports `helper_exited` (Q1 consult, §1.2) |
| 3. Fresh-request pin | `linuxV2OpenStageOwnerPIDFD` `owner_cli_test.go:1797-1807` | converted: only the resident anchor is pinned live; the helper's owner-loss exit is asserted through its PIDFD |
| 4. Fresh-request denial | `native_run.go:204-207`, `linux_cgroup_journal.go:455-517` | verified, no edit: the denial is durable-only and survives owner loss; the old liveness pins were preconditions, not the assertion |
| 5. Recovery response read + exit status | `owner_cli_test.go:177-193,222-261` | satisfied for the ruled `released` outcome; the refusal path remains for every other shape |
| 6. Final journal check | `linuxV2JournalHasPhysicalRelease` `native_witness_test.go:2536-2542` | verified, no edit; the retained clone is additionally asserted (§2.5) |

### 3b. Live-pin census (every site that pins an owner process live after an owner kill)

Command: `grep -rn "linuxV2RequireLivePIDFD\|linuxV2OpenStageOwnerPIDFD\|linuxV2KillOwnerWitnessCommand" daemon/e2e/*.go`.

| Site | Disposition |
| --- | --- |
| `native_witness_test.go:437`, `:520` (fresh-request leg) | converted: anchor live only; the helper's owner-loss exit is asserted (`:442`, a 90 s bound above the helper's own 30 s stop timeout, and `:520`) |
| `owner_cli_test.go:1953` (crash-replay runner) | converted: anchor live only; the helper's exit is asserted where its owner-loss premise is used (`native_witness_test.go:440`) |
| `linuxV2RequireLivePIDFD` other 18 sites | exempt: pre-kill liveness, detached-child/replacement/watcher identity, or the target stop the recovery itself must prove; none pins a helper after an owner kill |
| `linuxV2OpenStageOwnerPIDFD` (`owner_cli_test.go:1797`, `:1912`, `:1919`) | exempt: the pre-kill stage-entry pin (only the helper one is now consumed by the exit assertion) |
| `linuxV2KillOwnerWitnessCommand` (`owner_cli_test.go:201,1949,4126,4269`, `native_witness_test.go:592`) | exempt: the kill and the two unrelated runners' own kills |

### 3c. Assertion census

The only removed assertion is the helper-liveness pin the ruling names (pre-change
`owner_cli_test.go:1945-1949`, `native_witness_test.go:443-451`, `:520-527`); it is replaced by the
helper's explicit owner-loss exit. `requireLinuxV2RecoveryExitStatus`, the denial assertions and the
`linuxV2JournalHasPhysicalRelease` assertions are unchanged. Nothing was weakened: the added
assertions are §2.5-§2.7 plus the 9 unit rows, all additive.

## 4. Local gates

| Gate | Command (working directory `daemon/`) | Exit | Result |
| --- | --- | --- | --- |
| formatting | `gofmt -l .` (re-run on the final bytes as `accept`) | 0 | empty list |
| vet | `GOOS=linux go vet ./...` (node 1.27.0, WSL Ubuntu-24.04; re-run on the final bytes as `accept`) | 0 | no findings |
| focused suites, final bytes | `go test -count=1 -timeout 3600s ./internal/app/ ./internal/platform/ ./internal/state/` as user `accept` with `umask 022` | 0 | `ok app 215.391s`, `ok platform 44.544s`, `ok state 24.133s` (state re-run after the last test-row edit) |
| e2e unit pins, final bytes | `go test -count=1 -timeout 1800s ./e2e/` as `accept`, `umask 022` (re-run after the wait-bound fix, on the exact final bytes) | 0 | `ok e2e 0.738s` |
| whitespace | `git diff --check` | 0 | clean |

Environment fidelity: the suite must run as an unprivileged user with the CI runner's `umask`.
Two observed environment artifacts on this host, both pre-existing and both absent under the CI
invocation: as `root`, 13 platform wire-harness tests refuse with `wire harness requires an
unprivileged A process: uid=0 euid=0`; as `accept` with this shell's `umask 0002`, 4 supervisor
marker tests refuse a group-writable `t.TempDir()` (`… is group- or other-writable, want
published`) and the scan-failure injections miss their `fired` marker. The recorded gate above
uses `accept` + `umask 022`, which reproduces the runner.

## 5. Probes (lesson 74)

| Predicate | Command | Exit | Result |
| --- | --- | --- | --- |
| `linuxV2AdmissionRetainsPreCheckoutCloneAfterRelease` (state, 5 rows: admit, seed alias, cross alias, same leaf, blocking shapes) | `go test -count=1 -run 'TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease' ./internal/state/` | 0 | `ok state` (0.7s) |
| `linuxV2NativeWitnessCloneRetention*` (app, 8 negative + 7 summary rows) | `go test -count=1 -run 'TestLinuxV2NativeWitnessCloneRetention' ./internal/app/` | 0 | `ok app` |
| `linuxV2RequireRetainedPreCheckoutClone` + `linuxV2RequireRetainedCloneOutcome` (e2e, 7+4 negative, 2 non-applicable rows) | `go test -count=1 -run 'TestLinuxV2RetainedPreCheckoutClonePredicates' ./e2e/` | 0 | `ok e2e` |
| `linuxV2JournalBindsOwnerSlot` phases + strengthened comparator | `go test -count=1 -run 'TestLinuxV2JournalBindsOwnerSlotProjectsEveryPhase\|TestLinuxV2RequireSameOwnerIdentities' ./e2e/` | 0 | `ok e2e` |
| Existing pins the change must not disturb | `go test -count=1 -run 'TestLinuxCloneAbortCannotUseNormalReleaseAsNoTargetProof\|TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners\|TestLinuxV2AdmissionRejectsTerminalUnremovedAndUnprovenForeignOwners\|TestLinuxV2AdmissionFailsClosedOnCorruptForeignJournal\|TestLinuxCloneIntentFreezesFenceAndReservesUniquePaths' ./internal/state/` | 0 | `ok state` |
| The authenticated receipt path is probed with `testStateLinuxCgroupV2ReleaseProofFixture` (its own `Validate`) rather than by run luck (§2.1 helper, §3a row 6) | included in the state probe above | 0 | `ok state` |

## 6. Mutants (M1-M12)

Every mutation was applied to a scratch copy of the final bytes, run as a focused `go test`, and
reverted; the last column proves the revert is byte-identical (`EQ` = the file's sha256 equals the
delivered bytes' sha256). Delivered sha256s: `linux_clone_validation.go` `098a47d5…`,
`linux_cgroup_journal.go` `63bc4706…`, `linux_cgroup_v2_test.go` `cb7e26cb…`,
`linux_owner_witness_linux.go` `6a6f07ad…`, `linux_owner_witness_linux_test.go` `94699059…`,
`production_linux_v2_native_witness_test.go` `659febb5…`, `production_linux_v2_owner_cli_test.go`
`3e353cd3…`, `production_linux_v2_owner_identity_test.go` `167308c0…`.

| id | mutation | expected red | observed red (verbatim) | restored |
| --- | --- | --- | --- | --- |
| M1 | remove the d1b arm from `linuxV2ReleasedCloneStateSafe` | the admit row | `--- FAIL: TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease/non-aliasing_intent_and_fresh_leaf_are_admitted` (plus `same_leaf_name_is_refused` and `other_shapes_stay_blocking` by cascade) | EQ `098a47d5…` |
| M2 | skip the seed/target alias block for a d1b foreign row | the two alias rows | `--- FAIL: …/seed_path_alias_is_refused`, `--- FAIL: …/cross_seed/target_alias_is_refused` | EQ `63bc4706…` |
| M3 | remove `!LinuxCloneAbortPending` from the arm only | the pending-abort row | no red: the pre-existing first check (`linux_clone_validation.go:10`) already blocks it. Two-site mutant (also that first check): `linux_cgroup_v2_test.go:1107: pending clone abort shape was admitted as a safely retired foreign owner` | EQ (both files) |
| M4 | remove `LinuxCloneCheckoutReceipt == nil` from the arm | the checkout-receipt row | `linux_cgroup_v2_test.go:1107: checkout receipt present shape was admitted as a safely retired foreign owner` | EQ `098a47d5…` |
| M5a | remove the caller's `release.StopReceipt != nil` and leaf-disposition terms | the two proof rows | no red: `SupervisorHandoffReleaseProof.Validate` already refuses both, so the guard is upstream of the owned files | EQ `63bc4706…` |
| M5b | M5a plus weakening `Validate`'s v2 stop-receipt and disposition terms | the two proof rows | `linux_cgroup_v2_test.go:1107: release proof without a stop receipt shape was admitted as a safely retired foreign owner` (both terms were removed; the loop reports the first matching row) | EQ (`authority/supervisor.go` restored byte-identically) |
| M6 | remove the retained-leaf check call | the same-leaf row | `linux_cgroup_v2_test.go:1043: retained pre-checkout release did not keep its cgroup leaf name reserved` | EQ `63bc4706…` |
| M7 | hoist the arm above every caller term in `linuxV2ForeignJournalIsSafelyRetired` | the unproven row | `linux_cgroup_v2_test.go:1107: unproven containment shape was admitted as a safely retired foreign owner`; the existing pins (`TestLinuxV2AdmissionRejectsTerminalUnremovedAndUnprovenForeignOwners`, `TestLinuxV2AdmissionFailsClosedOnCorruptForeignJournal`, `TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners`) stayed green because their rows are blocked by terms that mutation does not touch (leaf still bound, unparseable journal, live owner) | EQ `63bc4706…` |
| M8 | revert the witness third branch to `recoveryErr = cleanupErr` | the four ruled sets red with the pre-change refusal | run `45610526163` (§7.4): all four exit 1 at `production recovery exit = exit status 1, want exit 0` with `refusal=journal_without_unresolved_authority reason=release_proof_present recovery_error_class=recovery_failed` | EQ `6a6f07ad…` in the goal tree; the scratch revert was the mutant itself |
| M9 | a release tombstone used as abort/cleanup authority | the existing pins | no mutation needed: `TestLinuxCloneAbortCannotUseNormalReleaseAsNoTargetProof`, `TestLinuxCloneCleanupRequiresDeliveredCandidateArtifact` and `TestDeleteJournalKeepsCandidateCloneWithoutDeliveredArtifact` are green on the final bytes (negative control) | n/a |
| M10 | `linuxV2RequireSameOwnerIdentities` reverted to the presence-only comparator | the swapped and equal-PID rows | `production_linux_v2_owner_identity_test.go:146`: `owner identity comparison error = <nil>, want "does not match its exact RunJournal PID/creation identity"` on `/a_consistently_swapped_bound_owner_set_fails_against_the_journal` and `/a_different_creation_identity_at_the_journal_PID_fails` | EQ `233d848d…` at the time; the file then took the §7.2 cleanup-allowance fix (`3e353cd3…`) |
| M11 | drop the admitted leg's `allowedNewRunKeys` registration (the pre-fix bytes of the cleanup comparator) | the run_key set reds at the fixture cleanup | run `45599614997`: `production witness changed a previously retained RunJournal: shared StateDir added unrelated RunJournal 43f0a37f-…/1` (§7.2) | n/a (the fix is run `45608856419`, all four sets exit 0) |
| M12 | leave the helper-exit wait at 30 s (equal to the helper's own stop timeout) | the run_key set reds intermittently in the `fresh_request` leg | run `45604636169`: `A helper did not exit with owner loss before the fresh request: exact process pidfd remained live beyond the bounded wait`, after the leg consumed its whole 30 s bound (0.23 s in run `45599614997`) — one red in two runs of the same leg | n/a (the bound is now 90 s) |

## 7. Scratch runs

Scratch branch `scratch/rf-acc-owner-loss-semantics` with
`.github/workflows/scratch-owner-loss.yml` — a copy of `linux-native-v2-acceptance.yml` with the
`environment:` gate removed, the probe's dummy credentials (`GH_TOKEN=gho_scratchdummy…`,
`SYMMETRY_OPENAI_API_KEY=sk-scratch-dummy`), a push trigger on that branch only, and all fourteen
case sets in workflow order inside one job (the M8 commit trims the array to the four ruled sets,
so the M8 run costs one focused pass) — plus the scratch-only `scripts/ci/scratch-gh-shim`
(byte-identical to the parent probe's file, git blob `6c2598c1…`). Every run was watched to
settlement; none was left pending. Each set runs
`SYMMETRY_LINUX_V2_NATIVE_CASE_SET=<set> SYMMETRY_LINUX_V2_NATIVE_EVIDENCE_DIR=<dir>
GITHUB_EVENT_NAME=workflow_dispatch bash scripts/ci/linux-v2-native-acceptance.sh`, which gates on
`scripts/ci/assert-go-test-json.py` (each expected test run=1 pass=1 skip=0, no `fail` events, no
invalid JSON lines).

| # | Run | Case sets | Head | Outcome |
| --- | --- | --- | --- | --- |
| 0 | `45594766773` | all 14 | `a2ba9779` | workflow-only failure: `Install scratch-only gh credential-ingestion shim` failed because `scripts/ci/scratch-gh-shim` is a scratch-only file absent from the base; no case set ran and no evidence artifact was uploaded. Fixed by scratch commit `78daf41f`, which adds the shim verbatim. |
| 1 | `45595454970` | all 14 | `78daf41f` (base `6b2d4f8f` + the probe workflow + the shim) | base census: nine sets exit 1, five exit 0 (below) |
| 2 | `45599614997` | all 14 | final bytes (commit `4fc35f42`) | `run_key_legacy`, `fence`, `subject` exit 0; `run_key` red at the fixture builder's own shared-State cleanup after the new admitted leg had passed (§7.2) |
| 3 | `45604636169` | all 14 | final bytes + the scoped cleanup allowance (`a46e2e3e`) | `run_key_legacy`, `fence`, `subject` exit 0; `run_key` red at the fresh-request leg's helper-exit wait — a fixture bound of 30 s racing the helper's own 30 s stop timeout (§7.2) |
| 4 | `45608856419` | the four ruled sets | final bytes + the corrected wait bound (`9089efba`; workflow array trimmed to the four sets) | **acceptance: all four sets exit 0**, `All 4 case sets exited 0.` |
| 5 | `45610526163` | the four ruled sets | witness third branch reverted (`c802fe27`) | M8: all four sets exit 1 with the pre-change refusal |

### 7.1 Base census (run `45595454970`)

Every set's exit status, and the first check each red set stops at (verbatim). Green sets are named
by the test names their event gates verified.

| Set | Exit | First check / verified test |
| --- | --- | --- |
| `codex_containment` | 1 | `TestProductionLinuxV2CodexNativeContainment` `native_witness_test.go:101`: `reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` (pre-existing; Containment-witness goal) |
| `claude_containment` | 1 | `TestProductionLinuxV2ClaudeNativeContainment` `native_witness_test.go:109`: same refusal text (pre-existing) |
| `shared` | 1 | `TestInstalledLinuxOwnerCloneCleanupParentDeathScratch` `clone_cleanup_linux_test.go:259`: `A cleanup caller exited without the requested parent-death signal`; also `TestInstalledLinuxOwnerCloneCheckoutScratch` `clone_integration_linux_test.go:240` (`clone status frame magic or version is invalid`) and `TestInstalledLinuxOwnerBoundaryScratch` `launcher_integration_linux_test.go:439` (`native exec failed after RELEASE: errno=1`) — the three product-test stops the burndown already records for this set |
| `synthetic_goal_cleanup` | 1 | `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` `production_linux_v2_cleanup_witness_test.go:499`: `one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; …)` (pre-existing) |
| `stage_replay_create_started` | 0 | `verified TestProductionLinuxV2StageCrashReplayMatrix` + `/create_started` run=1 pass=1 skip=0 |
| `stage_replay_leaf_bound` | 0 | `/leaf_bound` run=1 pass=1 skip=0 |
| `stage_replay_launcher_bound` | 0 | `/launcher_bound` run=1 pass=1 skip=0 |
| `ack_loss_launcher_bound` | 0 | `verified TestProductionLinuxV2AckLossReplayMatrix` + `/launcher_bound` run=1 pass=1 skip=0 |
| `stale_leaf` | 0 | `verified TestProductionLinuxV2StaleLeafIdentityRetainsJournal` run=1 pass=1 skip=0 |
| `migration` | 1 | `TestProductionLinuxV2MigrationDenialRetainsJournal` `native_witness_test.go:613`: `resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved …` then `wait for exact native_release boundary: owner-witness command did not reach its requested durable stage` (pre-existing, the release-recovery-diagnostics residual) |
| `stale_journal_run_key` | 1 | `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun`: `/fresh_request` PASSes (0.15s), then `native_witness_test.go:531: production recovery exit = exit status 1, want exit 0` |
| `stale_journal_run_key_legacy` | 1 | `TestProductionLinuxV2StaleOwnerRequestDenials/stale_journal_run_key`: `native_witness_test.go:269: production recovery exit = exit status 1, want exit 0` |
| `stale_journal_fence` | 1 | `/stale_journal_fence`: same `:269` check |
| `stale_journal_subject` | 1 | `/stale_journal_subject`: same `:269` check |

All four ruled sets' red is one class: the same-RunKey recovery exits 1 before the response is
written, and the App's own stderr names it —
`recover existing owner witness RunJournal without launch: refusal=journal_without_unresolved_authority
reason=release_proof_present recovery_error_class=recovery_failed: Linux clone cleanup requires its
exact workspace adapter and complete durable receipts`. Each set's `fail_events=2` (the test's own
`Errorf` plus the `--- FAIL` line); no `invalid_json_lines`.

This differs from the parent's run `45485437534` rows in one way and agrees in another: the parent's
sets did not reach an exit-status check at all (the response decode failed first, no response being
written), while both runs stop at the same product refusal — whose text now carries the sub-step
(`#285`). The base census confirms every red source outside this goal's four sets before the fix:
none of the five other red sets names this goal's owned files.

### 7.2 Acceptance runs on the delivered bytes (runs `45599614997`, `45604636169`, `45608856419`)

Run `45599614997` (14 sets; the other ten sets' rows are identical to §7.1, five red for their own
pre-existing reasons):

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key_legacy` | 0 | the setup recovery returned `released` with its retained clone; the stale-request denial red at its intended identity mismatch |
| `stale_journal_fence` | 0 | same, at the persisted runtime-epoch mismatch |
| `stale_journal_subject` | 0 | same, at the changed Subject revision |
| `stale_journal_run_key` | 1 | `/fresh_request` PASS (0.23s); `/fresh_request_admitted` PASS (1.33s) — the admitted run reached its own `handoff_committed` boundary, was killed, recovered to the same released retained shape, and the retained setup row stayed byte-identical; then `t.Cleanup` at `production_linux_v2_native_witness_test.go:1933-1940` failed with `production witness changed a previously retained RunJournal: shared StateDir added unrelated RunJournal 43f0a37f-…/1` |

The run_key red is exactly the lesson-83 class: the admitted fresh request's own new RunJournal was
unknown to the fixture builder's cleanup comparator, which until then had only ever seen tests that
deny. The scoped fix (§2 item 6) lets a test register a new key it has proven; the run then exited
1 on that one `Errorf` alone (`fail_events=2`), so no other run_key assertion red.

Run `45604636169` on the corrected bytes: `run_key_legacy`, `fence`, `subject` exit 0 again (the
cleanup allowance held; their rows are unchanged from run `45599614997`). `stale_journal_run_key`
reddened in the `fresh_request` leg — this time on the new Q3(a) pin itself:

```
production_linux_v2_native_witness_test.go:524: A helper did not exit with owner loss before the
fresh request: exact process pidfd remained live beyond the bounded wait
--- FAIL: TestProductionLinuxV2UnresolvedJournalBlocksFreshRun/fresh_request (30.05s)
```

the log's `:524` is the runner's call site in the test file, the attribution Go gives a fatal raised
inside the runner's callback — the same attribution the base census shows for the exit-status check
(`:269`, `:531`); the failing assertion itself is the unique helper-exit wait, `:441` on the run's
bytes and `:442` after the fix). The subtest consumed its whole 30 s bound; the same leg passed in 0.23 s in run
`45599614997`. The helper's own owner-loss fail-safe is bounded at
`linuxOwnerHelperV2StopTimeout = 30 s` (`internal/platform/helper_v2_protocol.go:33`), so a fixture
bound of exactly 30 s is a race by construction; this run is the losing side of it. That the helper
did exit is proven inside the same run: the later setup recovery consumed the helper's own
self-reap stop evidence (`stop/proof_kind=helper`, `stop/issuer/pid = reaper/pid`, witness document
`unresolved-journal-guard.json` in the run's artifact, whose `source_commit` is `a46e2e3e`), and the
retention it reports is `clone/retention = retained_after_release`. The `fresh_request_admitted`
leg passed again (1.18 s) and the `t.Cleanup` comparator stayed green, so the cleanup allowance from
run `45599614997` is fixed. The bound is now 90 s (§2 item 4); the M12 row (§6) names this run as
the mutant's red.

Run `45608856419` on the final bytes (`9089efba`), the four ruled sets:

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key` | 0 | `verified TestProductionLinuxV2UnresolvedJournalBlocksFreshRun: run=1 pass=1 skip=0`, then `Case set stale_journal_run_key exited 0` (11:59:52Z); the test's `/fresh_request`, `/fresh_request_admitted` legs and the fixture cleanup all pass in one run |
| `stale_journal_run_key_legacy` | 0 | `verified TestProductionLinuxV2StaleOwnerRequestDenials` + `/stale_journal_run_key` run=1 pass=1 skip=0 |
| `stale_journal_fence` | 0 | `…/stale_journal_fence` run=1 pass=1 skip=0 |
| `stale_journal_subject` | 0 | `…/stale_journal_subject` run=1 pass=1 skip=0 |

Final line: `All 4 case sets exited 0.` and the run's conclusion is `success` (the workflow job
fails when any set does). Each set's event gate reports `fail_events` absent, no skipped test and no
invalid JSON line, so the four sets' boundaries are reached on the delivered bytes — the same eight
product/e2e file bytes as the goal tree (§7.3).

### 7.3 sha256 equality (delivered bytes = the acceptance run's product/e2e bytes)

| File | sha256 |
| --- | --- |
| `daemon/internal/state/linux_clone_validation.go` | `098a47d5885dfa40bd6c4c0cd3460937f63f9a07a0194bb40993d72fbc5fd198` |
| `daemon/internal/state/linux_cgroup_journal.go` | `63bc4706581e88a51cbf54481ea86aecb34ac2cf1739dee297675f06e52c93b9` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | `cb7e26cbc1dfdc3898320a457c2c986cc7199938a41518b53c9a29fa76fd1920` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `6a6f07ad341b992c4ea6c3fb0729b3f212b3c0c3088f965293387dfcdead4d88` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `94699059e70fa6f27bd296d300c3b8e8eb3a95de39e9085e957dc49565b36f90` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `659febb5328949504b2e40907a44bdaaf98bd8d3d52ed1318866490df8f73fa4` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `3e353cd3bbed71364cee2afa708f766bcb427a842742b19838bda58e32389a56` |
| `daemon/e2e/production_linux_v2_owner_identity_test.go` | `167308c0b8468a19f4c854d0a18196a443953f2df654225e507b9e1d5bebf1b2` |

Run `45608856419`'s head (`9089efba`) holds these eight files byte-identically (verified by copying
each file and re-hashing in both checkouts); the record, plan, runbook and TODO bytes were finalized
after the acceptance commit, so those four docs bytes are not part of the run's head (no test or
product behaviour depends on them).

### 7.4 M8 (run `45610526163`)

The same four sets with the witness's third branch reverted (`c802fe27`; that commit carries the
`-1 +6` revert and nothing else). All four exit 1 (`Failed case sets: stale_journal_run_key
stale_journal_run_key_legacy stale_journal_fence stale_journal_subject`), each at the exit-status
check with the pre-change refusal — the class run `45595454970` recorded on the base bytes:

| Set | Exit | Verbatim |
| --- | --- | --- |
| `stale_journal_run_key` | 1 | `/fresh_request` PASS (0.23s), then `production_linux_v2_native_witness_test.go:526: production recovery exit = exit status 1, want exit 0`; App stderr `…refusal=journal_without_unresolved_authority reason=release_proof_present recovery_error_class=recovery_failed…` |
| `stale_journal_run_key_legacy` | 1 | `:269: production recovery exit = exit status 1, want exit 0` + the same refusal |
| `stale_journal_fence` | 1 | same |
| `stale_journal_subject` | 1 | same |

So the third branch (and nothing else in the diff) is what carries the four sets from the refusal to
their boundaries: run `45608856419` is the same four sets, same workflow, byte-identical except that
one branch, and exits 0. The `:526` attribution is the runner's call site, matching §8's note
(`:524` at `a46e2e3e` before the two comment lines were added).

## 8. Lesson-83 assertion trace

For the four `stale_journal_*` sets the pre-change runs stopped at the same two points
(`45485437534`, `45489336388`): the recovery leg died at the refusal, so every assertion *after*
the response read had never executed in any run. On the final bytes the following execute for the
first time: the response decode, `requireLinuxV2RecoveryExitStatus`, the released arm of
`linuxV2ValidateStageReplayResponse` (including the new retention outcome check), the released case
of `linuxV2ValidateStageReplayJournal`, and — for `stale_journal_run_key` — the entire
`fresh_request_admitted` leg (fresh stage marker, `linuxV2ValidateStageJournalBoundary`, the
SIGKILL, the same-RunKey recovery, the retention assertions and the two shared-State comparisons).
The variants' post-refusal assertions (the stale-request identity rejection, the byte-equality
snapshots and the empty-marker check) also execute for the first time. Section §7's per-set rows
name where each set stops now, i.e. the first assertion wall after the refusal that used to hide
them.

Run `45599614997` then produced exactly the wall the discipline predicts, and it was one the trace
did not cover: the fixture builder's `t.Cleanup` shared-State comparator
(`production_linux_v2_native_witness_test.go:1933-1940`), which fires only after a test body has
finished and had never seen a test whose fresh request is admitted. The trace in §2 covered the test
body's assertions and the new leg; it did not enumerate teardown-time comparisons. The next wall
behind it is empty: no other `t.Cleanup` in the file compares the shared StateDir, the three variant
sets pass, and the run_key body ends at the admitted leg (`:631`).

Run `45604636169` then found a second wall, this one of a different species: not an assertion that
had never executed, but a **fixture bound that races the product's own bound**. The Q3(a) pin waited
30 s (`:441` on those bytes) for the helper's owner-loss exit while the helper's own fail-safe is
bounded at `linuxOwnerHelperV2StopTimeout = 30 s` — equal, so the race is by construction; the same
leg passed in 0.23 s one run earlier. The fix moves the fixture bound to 90 s, past the product
bound with margin, above which only a helper that never exits stays red (M12). This is the staffed
lesson for bound-carrying pins: a fixture wait must be *strictly above* the product bound it wraps,
never equal to it.

## 9. Residual risks and limits

- The four sets' green rows are the acceptance; the other case sets' pre-existing reds
  (`shared` owner-leg scratch group, `synthetic_goal_cleanup` release refusal, `migration`/`stale_leaf`
  release machinery, `stage_replay_*` exit contract) are reported, never fixed here.
- `linuxV2ReleasedCloneRetainedAfterRelease`'s exact scope is the place a later refactor can silently
  widen; the mutant rows M1/M3/M4/M7 in §6 are the guard.
- The retained row has no deletion path by design; the disposition command is a TODO item, not built.
- The witness retention classification is shape-driven: a future cleanup failure that leaves the
  exact retained shape would be reported as retention, but the e2e asserts the durable row itself, so
  a masked mutation is visible there.
- The run-set census and the per-set rows are §7; a set reddening for a reason outside this goal's
  owned files is reported as its own row and not repaired here.
- The admitted-leg shared-State allowance is the one place the fixture could over-tolerate: it is
  populated by `production_linux_v2_native_witness_test.go:596-598` only, after the admitted run's
  journal boundary has been validated, and the comparator still requires every previously retained
  row to be byte-identical (M11).
