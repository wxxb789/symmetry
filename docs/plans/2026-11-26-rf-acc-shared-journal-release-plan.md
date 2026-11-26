# rf-acc-shared-journal-release — implement plan

Contract: `goal-rf-acc-shared-journal-release.md` (GDD state root, external to the repository).
Base revision: `7111828b902ecfb8885cbd3dd972c6fc0c28918c` (#296 on main).
Branch: `refactor/rf-acc-shared-journal-release`. Bind evidence: `findings/296-binding-run-2.md`
§3.4 (run `45737002125`, log retained at the GDD state root, sha256 `bf00da4f…`).

The accepted plan is the contract plus the Bind evidence; this file resolves the Stage-1
discrimination the contract demands, the fix choice, the proof pins, and what stays out of scope.

## 1. Stage 1 — root cause, named verbatim

The blocking prior record is a **claim-only RunJournal**, not a released or unresolved
containment: `docs/evidence/rf-acc-b-process-readback-2026-11-26.md` and the run log's own
secret-free projection — the journal is inserted at `run-45737002125.log:3146`, the pi leg stops
at `:3226`, and its post-failure projection at `:3229` prints exactly one retained row —
`journal-9c6629….json {"local_state":"claimed","has_claim_grant":true,
"has_clone_intent":false,"has_seed_receipt":false,"recovery_required":false}`.

Why the preceding `alternate_mount` leg leaves it:

1. The witness's fresh path seeds the authentic claim durably before it touches the native owner:
   `checkLinuxV2NativeAdmissionBeforeClaim` → `seedAuthenticClaim` → `prepareLinuxV2NativeOwner`
   (`daemon/internal/app/linux_owner_witness_linux.go:3240,3243,3253`). `seedAuthenticClaim`
   writes `claiming` then `claimed` with the claim grant (`:4257-4292`).
2. The alternate mount is then refused inside the native-owner preflight, after that claim:
   `prepareLinuxV2NativeOwner` builds the factory owner, then `owner.Preflight`
   (`daemon/internal/app/native_run.go:169,197-199`) → `linuxpreflight.VerifyLinuxOwnerInstallation`
   (`daemon/internal/app/linux_owner_v2_linux.go:91`) → `openConfiguredLinuxOwnerParent` →
   `validateLinuxOwnerParentIdentity` (`daemon/internal/linuxpreflight/cgroup_linux.go:80,92-96`;
   the error literal at `:93`). The alternate bind mount's
   measured parent identity differs from the live anchor's manager subgroup by `MountID`
   (`daemon/internal/platform/linux_cgroup_v2_linux.go:239-247`;
   `authority.LinuxCgroupParentIdentity{MountID}` at `daemon/internal/authority/cgroup.go:25-31`).
   Verbatim guest text (probe output, `gdd/native/run-45384436642.log:1231`):
   `-verify-linux-owner-install: configured delegated parent identity differs from the live owner
   anchor's exact manager subgroup`.
3. The refusal path has no release or clear step: the process exits 1 with the claim row, which
   is by design (the journal is written before every effect and is retired only by the product's
   terminal/cleanup path: `cleanupPending` `daemon/internal/app/cleanup.go:893` is the caller of
   `state.DeleteJournal` `daemon/internal/state/run_journal.go:423`; the witness writes no
   terminal transition).

Discrimination against the contract's three candidates:

- (a) **confirmed**: the refusal path legitimately retains an unresolved-by-predicate row; the
  product itself classifies it as blocking — `linuxV2ForeignJournalIsSafelyRetired`
  (`daemon/internal/state/linux_cgroup_journal.go:432-475`) accepts only release/pre-gate/abort/
  reuse-retired/v1-receipt shapes, so `scanLinuxV2NativeAdmissionLocked` refuses a fresh native
  claim with `foreign RunJournal … has unresolved native ownership` (`:382`). The store test
  `TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease` pins "unreleased row" as not retired
  (`daemon/internal/state/linux_cgroup_v2_test.go:973`, row at `:1095`).
- (b) rejected as the fix: making the refusal leg release or retire the row is a production
  change (DECISION_NEEDED, not owned here), and no product command retires a claim-only row in
  bounded time (containment-only recovery no-ops on it: `recoverContainmentOnlyJournals`
  `daemon/internal/app/linux_owner_recovery_linux.go:1622-1650`).
- (c) rejected: relaxing the pi leg's precondition would need to match a product gate that also
  refuses, and the contract forbids weakening it.

## 2. The fix (Stage 2, wrong side)

The refusal leg's roster row is retained by design, so the shared set must not let it precede a
leg that requires a fresh or physically released Store. The delivered order runs
`run_alternate_mount_refusal_witness "$v2_alternate_mount_events"` at
`scripts/ci/linux-v2-native-acceptance.sh:3190`, after every leg that starts a fresh native-owner
witness (pi `:3170`, crash `:3174`, daemon-crash `:3175`, stage-replay `:3176`, ack-loss `:3179`,
helper-exit `:3182`, response-loss `:3183`, receipt-persist `:3185`, pid-reuse `:3186`, v1-replay
`:3187`) and immediately before the frozen `opencode_native_health` leg (`:3191`). No journal is
deleted, no Store is reset, and no leg's assertions are weakened.

The old placement carried the fixture-inventory rule "intentionally first so another row cannot
mask the proof" (`docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82`). That rule is
preserved by an explicit, stronger pin: the leg reads the App stderr it already captures and
requires the mount-identity preflight refusal verbatim, and forbids the foreign-ownership
refusal. Position no longer has to carry the anti-masking proof.

The pinned text is grounded in the App's own print path, not in prose: `main.go:35` prints
`-linux-v2-native-owner-witness: <err>`; `linux_owner_witness_linux.go:3258` wraps the preflight
as `preflight installed Linux v2 owner before clone/native work: %w`; `native_run.go:199` wraps it
as `Linux v2 native owner preflight: %w`; `cgroup_linux.go:93` is the bare cause phrase. The leg
also guards its order at runtime: it refuses to start unless all ten prior legs' event files are
already non-empty.

## 3. Public/API shape

1. `scripts/ci/linux-v2-native-acceptance.sh` — the leg move plus a 2-line comment naming the
   retained-claim-only row as the reason for the order, and a runtime guard inside
   `run_alternate_mount_refusal_witness` that fails unless the ten prior legs' event files are
   non-empty; no other helper changes.
2. `daemon/e2e/production_linux_v2_owner_cli_test.go`
   - `runProductionLinuxV2AlternateMountRefusal`: require the exact refusal cause from
     `linuxV2OwnerWitnessStderrTail` (`daemon/e2e/production_linux_v2_native_witness_test.go:2216`)
     and require the leg's own retained row to be the claim-only shape: found, `claimed`, claim id
     and unexpired claim grant present, no clone intent, no process details, no pending
     containment (authority, handoff, scan ledger or abort-less reservation), no unproven stop, no
     cgroup reservation even when aborted, and no release tombstone.
   - New pure predicates `linuxV2RequireMountRefusalCause(lines)` and
     `linuxV2RequireClaimOnlyRetainedRow(journal)`, each with a unit table row per branch: 3 for
     the cause predicate (witness marker present; foreign-ownership phrase absent; mount phrase
     present) and 12 for the row predicate (missing row plus 11 field predicates).
   - One row pinning the capture read refusal
     (`TestLinuxV2AlternateMountRefusalCauseRefusesAnUnreadableCapture`).
   - One unit row pinning the unchanged precondition's treatment of a claim-only prior record
     (`linuxV2RequireSafelyReleasedPriorJournal` refuses it) as the ordering rationale.
3. `daemon/e2e/production_linux_v2_native_witness_test.go` — no change.
4. Records: this plan, an evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`.

## 4. Choices the accepted plan left open

- **Position**: immediately before `opencode_native_health`, not at the very end: the opencode
  leg aborts on its own pinned-version gate before any Store read (its config gate runs at
  `production_linux_v2_native_witness_test.go:1901`, the prior-journal precondition only at
  `:1968-1975`), so the mount leg still produces its E3 negative evidence while nothing that needs
  a released Store follows it.
- **Cause pin by text, not by position**: the mount phrase is guest-verified; the assertion also
  forbids `unresolved native ownership` (the foreign-row refusal) so a masked cause is a red leg.
- **No new precondition on the moved leg**: requiring the fixture's stricter released-row
  predicate for all prior rows would red the leg on rows the product accepts (pre-gate/abort
  retirements). The moved leg asserts only its own cause and its own retained row.
- **Subsumed disjuncts dropped**: `ContainmentAuthority != nil` and `ContainmentHandoff != nil`
  were removed from the row predicate after the mutant run showed `HasPendingContainment()` alone
  reds their rows; keeping them would have shipped branches no mutant can distinguish.
- **Aborted reservations stay refused**: `ContainmentCgroup != nil` is kept beside
  `HasPendingContainment()` because an aborted reservation (`AbortProof != nil`) escapes the
  latter; the "an aborted cgroup reservation was written" row is the only one that reds when the
  disjunct goes.
- **Comment wording** stays within the 2-line cap; the script comment names the reason, not the
  whole history.

## 5. Per-clause baseline and proof

| Contract clause | Baseline (before) | Proof |
| --- | --- | --- |
| 1 root cause named verbatim | `:91` stop, run `45737002125`; row shape from the log projection | record §1-§2 with the measured log lines (`:3146`, `:3226`, `:3229`) and the file:line anchors of record §10 |
| 2 fix on the wrong side, no ad-hoc deletion | leg first, blocking row before the pi leg | record §3: the script diff (order only), the ordering checker RED on the pre-change bytes (`ORDERING-RED status=1`) and GREEN on the delivered bytes, the guard harness red/green, no `rm`/Store reset in the diff |
| 2 no weakened precondition or refusal assertion | — | record §9: `git diff` shows no change to `linuxV2RequireSafelyReleasedPriorJournal`; the new row `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` pins its claim-only refusal |
| Invariants: ≤2-line comments, `go vet`, records rule | — | record §4-§6: comment-cap before the re-measure, `go vet` rows, one red mutant per new branch (record §5) |
| Acceptance: `:91` stop gone in the Bind | — | carried to the conductor's Bind; local rows recorded (record §11) |

## 6. Local gates

From `daemon/`: `gofmt -l e2e/`; `go vet ./...` (Windows, pinned go1.27.4) and
`GOOS=linux go vet ./e2e/`; the three new rows plus the `./e2e/` package; then the full
`go test ./...` on Windows and in WSL (`wsl -d Ubuntu-24.04` as root from
`/mnt/q/repos/wt-acc-journal-release/symmetry/daemon`, go1.27.0), never beside a measurement
harness. From the repo root: `bash -n scripts/ci/linux-v2-native-acceptance.sh`. Scratch
harnesses, not committed: `check-order.sh` on the pre-change bytes (RED) and on the delivered
bytes (GREEN); `guard-harness.sh` with all ten event files (green), one empty, and one missing
(red); `mutants.sh` in a scratch clone for one red row per new branch. Comment cap:
`python C:/Users/lhan/.local/state/gdd/scratch/comment-cap.py <worktree>` before the final
re-measure of digests and anchors.

## 7. Out of scope

- Any production change (claim seeding before the preflight, foreign-claim retirement): contract
  requires DECISION_NEEDED instead; recorded as a residual.
- The opencode leg's pinned-version gate and the unmeasured `stop_readback_reader` row: their own
  tracked entries.
- Weakening, deleting or renaming any refusal assertion; deleting journals; splitting the shared
  case set into another guest (needs the workflow file, not owned).
