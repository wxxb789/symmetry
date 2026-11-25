# rf-acc-owner-loss-semantics — implementation plan (d1b clone retention)

Contract: `goal-rf-acc-owner-loss-semantics.md` (GDD state root, external to the repository). Base
revision `6b2d4f8f` (#285 `rf-acc-release-recovery-diagnostics`, this goal's Q2(a) gate; #284 and
#280 supply the refusal vocabulary and the containment-witness legs). Branch
`refactor/rf-acc-owner-loss-semantics`.

The accepted plan is the contract; this file resolves only the implementation choices it leaves
open, the clause census at the dispatch head, the edge-case semantics, the evidence plan, and what
is out of scope.

## 1. Clause census at the dispatch head (re-located by content)

| Contract clause | Dispatch head (`6b2d4f8f`) | Disposition |
| --- | --- | --- |
| Change 1 consult (Q1/Q2/Q3/Q4 rulings) | `findings/rf-acc-owner-loss-consult-{sol,ds}.md`, `…-d1d2-{sol,ds}.md`, `…-d1b-{sol,ds}.md`; candidate (B) confirmed by run `45557621798` | done before dispatch, no code |
| Change 2/3/4 ruled as d1b (witness + admission + e2e) | witness branch `daemon/internal/app/linux_owner_witness_linux.go:511-527`; admission `daemon/internal/state/linux_clone_validation.go:9-46` + `daemon/internal/state/linux_cgroup_journal.go:379-398`; e2e `daemon/e2e/production_linux_v2_owner_cli_test.go:1833-2021`, `production_linux_v2_native_witness_test.go:338-538,246-336` | implement here |
| Change 5 observability | #285 already names the guard/reason/class and writes the identity-only refusal document (`linux_owner_witness_linux.go:2916-3060`) | satisfied; the d1b refusal is replaced by the named retention outcome |
| Change 6 records | plan/evidence/TODO do not exist yet | this plan, `docs/evidence/rf-acc-owner-loss-semantics-2026-11-25.md` |
| Owned set (amended by the d1b ruling) | witness, `state/linux_clone_validation.go`, `state/linux_cgroup_journal.go`, their tests, the e2e files, the runbook, TODO | matches §2-§4 below |
| Carried #284 items | `daemon/e2e/production_linux_v2_owner_cli_test.go:1639-1753`, `production_linux_v2_owner_identity_test.go:46-132`, `docs/archive/CLOSED-TODO.md` (§rf-acc-owner-loss-semantics: owner-identity polish) | implemented here (see §3.4); the entry is closed |
| Gates | four `stale_journal_*` sets red at run `45485437534`; scratch workflow shape on `scratch/rf-acc-native-burndown` | §5 |

## 2. Public/API shape (choices the contract leaves open)

1. **Named retention outcome** (app): the witness summary's clone document gains one bounded field
   `linuxV2NativeWitnessClone.Retention string \`json:"retention,omitempty"\`` with the single value
   `linuxV2NativeWitnessCloneRetentionRetainedAfterRelease = "retained_after_release"`. No journal
   field is added (conductor clarification 2026-11-25: the durable retention reason is the row's own
   shape). The value is emitted only from the classification in §3.1 and validated in
   `validateFinalSummary` against `case == stage_replay`, `recovery_state == released`,
   `clone.checkout_ready == false`, non-empty `clone.intent_sha256`/`seed_digest`, and
   `journal_unproven == false`.
2. **The d1b shape predicate** (state): `LinuxV2ReleasedCloneRetainedAfterRelease(journal RunJournal) bool`
   in `daemon/internal/state/linux_clone_validation.go`, next to `linuxV2ReleasedCloneStateSafe`,
   which calls it as one more arm. Exact terms (all of them; anything else stays blocking): valid
   `LinuxCloneIntent` (`ValidateForJournal`), non-nil seed receipt, `LinuxCloneCheckoutReceipt == nil`
   (the discriminator), `LinuxClonePromotionIntent == nil`, `LinuxClonePromotionReceipt == nil`,
   `!LinuxCloneAbortPending`, `LinuxCloneAbortProof == nil`, `!LinuxCloneCleanupPending`,
   `LinuxCloneCleanupIntent == nil`, `LinuxCloneTargetRemovalProof == nil`,
   `LinuxCloneCleanupProof == nil`, `LinuxCloneReuseGrant == nil`, `!NativeUsageRecoveryRequired`,
   `WorkspacePath == intent.Target`, `WorkspaceBindingKey == intent.BindingKey`,
   `WorkspaceRecoveryRequired`. The caller
   (`linuxV2ForeignJournalIsSafelyRetired`, release branch) already supplies the release-proof,
   leaf-disposition, `ContainmentCgroup == nil` and no-live-authority terms.
3. **The witness-side classifier** (app): `linuxV2NativeWitnessCloneRetentionAfterRelease(before, after state.RunJournal) bool`
   takes the pre-attempt and post-attempt journals. It requires the post-attempt shape — the exact
   pre-checkout shape of §2.2, delegated through the exported
   `state.LinuxV2ReleasedCloneRetainedAfterRelease` (apply round: the five state-arm terms were the
   missing half; dedupe round: §2.2's predicate is exported so the two arms cannot drift apart) —
   plus `linuxV2NativeWitnessJournalPhysicallyReleased` — plus identity equality between the two (intent,
   seed, release proof, workspace path/binding, `RetainWorkspace`) so a cleanup that mutated
   anything can never be classified as retention.
4. **Admission leaf closure** (state): inside the existing per-foreign-row loop
   (`scanLinuxV2NativeAdmissionLocked`), the `reservation != nil` branch gains an `else if` arm:
   when the foreign row is the d1b shape and its release proof carries `CgroupIdentity`, refuse a
   reservation whose `Parent.Path` and `LeafName` equal the proof's. Same error string as the
   existing leaf refusal. The alias block above it is not skipped, and no `continue` is added.
5. **e2e pin discipline** (owner, Q3(a)): `runProductionLinuxV2StageCrashReplayCase` pins the anchor
   PIDFD (survives by design) and requires the helper PIDFD to signal exit (`linuxV2WaitPIDFDExited`,
   90 s) instead of requiring it live. The fresh-request leg of
   `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun` pins only the anchor and asserts the
   helper's owner-loss exit the same way.

## 3. Edge-case semantics the contract demands

1. **Never skip or fake the cleanup**: the witness always calls
   `cleanupRecoveredWorkspace`; the retention outcome is emitted only after the call failed *and*
   the reloaded row is byte-for-byte the same clone shape. A cleanup that succeeded (abort/pre-gate
   recovery) keeps today's path; a cleanup that failed on any other shape keeps today's refusal.
2. **Fail-closed default**: any d1b term missing (abort pending, checkout receipt present, cleanup
   fields, native-usage recovery, cgroup identity of the reservation present) leaves the row
   blocking admission and the recovery error a `recovery_failed` refusal — unchanged behaviour.
3. **Unchanged byte-frozen surfaces**: `LinuxCloneAbortProof.Validate`, `ClearLinuxCloneNoTargetRetention`,
   `LinuxCloneCleanupEligible`, `DeleteJournal`, the workspace rename guard and `daemon/internal/app/cleanup.go`
   are not edited (M9's pins `linux_clone_journal_test.go:423-424,454` stay green).
4. **Carried #284 items**: `linuxV2JournalBindsOwnerSlot` becomes an identity projection
   (the projection inline in `linuxV2JournalBindsOwnerSlot(journal, slot) (int, string, bool)`) and
   `linuxV2RequireSameOwnerIdentities` additionally requires every actual bound slot to equal the
   journal's projection — so a consistently swapped marker/response set reddens. Table rows are
   added for the handoff, authority, release-proof and pre-gate journal branches (DS F5) and for an
   equal-PID/different-identity journal comparison (Sol P3). Both live in
   `daemon/e2e/production_linux_v2_owner_identity_test.go`, the unit-test file for the named helper;
   the d1b ruling's "their tests, the e2e files" owns it, and no production file changes for it.
5. **e2e acceptance semantics** (ruling §4): the four `stale_journal_*` sets assert, on the recovered
   row, the `released` outcome, the physical release proof, the retained pre-checkout clone (intent +
   seed, no checkout/promotion/abort/cleanup) with its durable A-side disk artifacts asserted (A
   seed bundle, per-run base ref; the B target is created post-handoff, so it is not asserted;
   apply round) and exit 0. `stale_journal_run_key` additionally runs
   the same, already-staged fresh request twice: before the setup recovery it must be denied on the
   durable unresolved row (existing leg), after the recovery it must be admitted and reach its own
   `handoff_committed` boundary, then be killed and recovered to the same released retained shape
   (this is the non-aliasing admission proof; the aliasing denial is pinned at the state layer,
   because a request cannot mint aliasing paths — seed/target are RunKey-derived,
   `internal/workspace/linux_clone_linux.go:90-101`). "Every foreign admission denied" is not
   encoded anywhere.

## 4. Stage sequencing

1. State change + unit rows (M1/M2/M6/M7 arms) → focused `go test ./internal/state/`.
2. App change + unit rows (retention classifier positive/negative, summary validation, the refusal
   table stays green) → focused `go test ./internal/app/`.
3. e2e changes (pins, retention assertions, admitted leg, CAF carried items) → `GOOS=linux go vet`.
4. Lesson-74 probes (§5.1) on the two new predicates before any scratch run.
5. Scratch run 1 = base census on the dispatch head + the scratch workflow only (per lesson 72-73;
   #284/#285 changed the refusal path since the contract's run `45485437534`, so the current reds
   must be observed, not assumed). Scratch run 2 = the same workflow on the final bytes. Cap 6.
6. Records, TODO/CLOSED-TODO, runbook, contract archive.

## 5. Local probes (lesson 74) and gates

1. Probes: `go test -count=1 -run 'TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease' ./internal/state/`
   and `go test -count=1 -run 'TestLinuxV2NativeWitnessCloneRetention' ./internal/app/`, each with
   one positive and one negative case per arm; the auth-receipt path (release proof + leaf
   disposition) is probed with the existing `testStateLinuxCgroupV2ReleaseProofFixture`, not by run
   luck.
2. Local gates in WSL Ubuntu-24.04: `gofmt -l .`, `GOOS=linux go vet ./...`, focused
   `go test ./internal/app/ ./internal/platform/ ./internal/state/`, `git diff --check`.
3. Census: grep census of every site pinning an owner live after an owner kill
   (`linuxV2RequireLivePIDFD`, `linuxV2OpenStageOwnerPIDFD`, `linuxV2KillOwnerWitnessCommand`) and of
   every assertion removed (`none`, except the helper-liveness pin the ruling names).
4. Mutants (scratch copy, reverted byte-identically): the contract's M1-M9 rows; each row names the
   job that must redden.

## 6. Out of scope

- F1 exit contract (`rf-acc-witness-unresolved-exit`) and F2 diagnostics
  (`rf-acc-release-recovery-diagnostics`), including any line step 1 merely cites.
- `shared`/`synthetic_goal_cleanup`/`migration`/`stale_leaf` residuals and the other case sets'
  pre-existing reds; they are reported, never fixed here.
- `daemon/native/linux-gate/**`, `daemon/internal/execution/**`, `daemon/internal/authority/**`,
  `daemon/internal/workspace/**`, `daemon/internal/app/cleanup.go`, `control/**`, `docs/design/**` and
  `.github/workflows/linux-native-v2-acceptance.yml` — read-only.
- The clone-row disposition command: only a TODO item and a read-only runbook locator are added.
