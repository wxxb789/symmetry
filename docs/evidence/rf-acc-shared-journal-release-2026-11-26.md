# rf-acc-shared-journal-release — evidence record

Goal `rf-acc-shared-journal-release` (contract: `goal-rf-acc-shared-journal-release.md` under the
GDD contracts root; dispatch gate main `7111828b`). Branch
`refactor/rf-acc-shared-journal-release`, base `7111828b902ecfb8885cbd3dd972c6fc0c28918c`. Plan:
`docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md`. Bind evidence for the stop:
`findings/296-binding-run-2.md` §3.4 (run `45737002125`; log retained as
`scratch/bind296r2/run-45737002125.log` under the GDD state root, sha256
`bf00da4fc22b7ea862e017e473637bd6b3482c41dc5c22db951c4692c7db5d95`). Local rows ran on Windows
(`go1.27.4 windows/amd64`) and in WSL Ubuntu-24.04 (kernel `6.18.58.1-microsoft-standard-WSL2`,
`go1.27.0 linux/amd64`). No production byte changed: the diff touches one test file, one CI script,
the plan and the two record files; production code would need DECISION_NEEDED (§8).

## 1. Root cause, named verbatim

The stop (Bind 2, `run-45737002125.log:3226`) is verbatim:

```
production_linux_v2_native_witness_test.go:91: pre-existing RunJournal c869cf0f-884e-4160-b99a-aedd1dd6d659/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf
```

The blocking record is a **claim-only RunJournal**, not a released or unresolved containment: the
journal was inserted at `01:41:01.8457627Z` (`:3146`) by the immediately preceding green
`TestProductionLinuxV2AlternateCgroupMountRefusal` leg (its own verification row is at `:3156`),
and the pi leg's post-failure projection (`:3229`) prints exactly one retained row:
`journal-9c6629….json {"local_state":"claimed","has_claim_grant":true,"has_clone_intent":false,
"has_seed_receipt":false,"recovery_required":false}`.

Why the refusal leg leaves it:

1. The witness's fresh path seeds the authentic claim durably before it touches the native owner:
   `checkLinuxV2NativeAdmissionBeforeClaim` (`daemon/internal/app/linux_owner_witness_linux.go:3240`)
   → `seedAuthenticClaim` (`:3243`) → `prepareLinuxV2NativeOwner` (`:3253`).
   `seedAuthenticClaim` (`:4257`) writes the intent with `LocalState: "claiming"` (`:4262`), the
   `claiming` state barrier (`:4273`), and then the claim grant (`SaveClaimGrant`, `:4284`), which
   readback-verifies `journal.LocalState != "claimed"` (`:4291-4293`, the check at `:4293`).
2. The alternate mount is refused inside the native-owner preflight, after that claim:
   `prepareLinuxV2NativeOwner` builds the factory owner (`daemon/internal/app/native_run.go:169`),
   then `owner.Preflight` (`:197`) and wraps its error as `Linux v2 native owner preflight: %w`
   (`:199`); the owner's preflight calls `linuxpreflight.VerifyLinuxOwnerInstallation`
   (`daemon/internal/app/linux_owner_v2_linux.go:91`), which reaches
   `openConfiguredLinuxOwnerParent` (`daemon/internal/linuxpreflight/cgroup_linux.go:80`) and
   `validateLinuxOwnerParentIdentity`, whose error literal is at `:93`:
   `configured delegated parent identity differs from the live owner anchor's exact manager
   subgroup`. The alternate bind mount's measured parent identity differs from the live anchor's
   manager subgroup by `MountID` (`daemon/internal/platform/linux_cgroup_v2_linux.go:244`;
   `authority.LinuxCgroupParentIdentity{MountID}` `daemon/internal/authority/cgroup.go:25-31`).
   Guest-observed text (fixture probe output, `native/run-45384436642.log:1227`):
   `-verify-linux-owner-install: configured delegated parent identity differs from the live owner
   anchor's exact manager subgroup`.
3. The refusal path has no release or clear step: the App exits 1 with the claim row and the
   witness writes no terminal transition. `cleanupPending` (`daemon/internal/app/cleanup.go:893`)
   is the only production caller of `store.DeleteJournal` (`:940`), and `state.DeleteJournal` is
   defined at `daemon/internal/state/run_journal.go:423`; `cleanupPending` returns immediately
   unless `LocalState` is `cleanup_pending` or `stale` (`:894-896`), which a claim row is not.

The App prints this refusal through its own error path, which is what the delivered pin reads:
`main.go:35` prints `-linux-v2-native-owner-witness: <err>`; `linux_owner_witness_linux.go:3258`
wraps the preflight as `preflight installed Linux v2 owner before clone/native work: %w`;
therefore the captured stderr's final line is
`-linux-v2-native-owner-witness: preflight installed Linux v2 owner before clone/native work:
Linux v2 native owner preflight: configured delegated parent identity differs from the live owner
anchor's exact manager subgroup`.

## 2. Stage-1 discrimination

- **(a) confirmed**: the refusal path legitimately retains an unresolved-by-predicate row, and the
  product itself classifies it as blocking. `linuxV2ForeignJournalIsSafelyRetired`
  (`daemon/internal/state/linux_cgroup_journal.go:432-475`) accepts only reuse-retired,
  release-proof, pre-gate-proof, aborted-reservation, clone-abort-proof, or v1-stop-receipt
  shapes, so `scanLinuxV2NativeAdmissionLocked` refuses a fresh native claim with
  `foreign RunJournal … has unresolved native ownership` (`:382`). The store test
  `TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease`
  (`daemon/internal/state/linux_cgroup_v2_test.go:973`) pins "unreleased row" as not retired
  (`:1095`).
- **(b) rejected as the fix**: making the refusal leg release or retire the row is a production
  change (contract class DECISION_NEEDED, not owned here), and no product command retires a
  claim-only row in bounded time: containment-only recovery no-ops on it —
  `recoverContainmentOnlyJournals` (`daemon/internal/app/linux_owner_recovery_linux.go:1622`)
  switches on `hasLinuxV2ContainmentState` (`daemon/internal/app/cleanup_glue.go:414-419`),
  `ContainmentHandoff != nil`, `HasProcessDetails()`, and `HasPendingContainment() ||
  ContainmentUnproven`; all four are false for a claim-only row, so the loop body does nothing.
- **(c) rejected**: relaxing the pi leg's precondition would need to match a product gate that
  also refuses, and the contract forbids weakening it.

File mislabel in the contract: the pi precondition is cited there as
`production_linux_v2_owner_cli_test.go:~1968-1976`; it actually lives in
`daemon/e2e/production_linux_v2_native_witness_test.go` — snapshot at `:1968`, loop at `:1972`,
check at `:1973`, fatal at `:1974` (identical to `2f7a38c6`/`origin/main` per Bind §3.4). The
helper it calls, `linuxV2RequireSafelyReleasedPriorJournal`, is in
`production_linux_v2_owner_cli_test.go:2259` and is unchanged (§9).

## 3. The fix and the order pin

The `shared` arm now runs the mount-refusal leg after every leg that starts a fresh native-owner
witness, and immediately before the frozen `opencode_native_health` leg
(`scripts/ci/linux-v2-native-acceptance.sh`):

| leg | line |
| --- | --- |
| pi provider action | `:3170` |
| helper sigkill detached child | `:3174` |
| daemon sigkill helper survives | `:3175` |
| stage-replay cases | `:3176` |
| ack-loss replay cases | `:3179` |
| helper exit after release before register | `:3182` |
| helper response loss | `:3183` |
| receipt persist failure | `:3185` |
| literal pid reuse | `:3186` |
| legacy v1 replay | `:3187` |
| **alternate-mount refusal** | **`:3190`** |
| frozen opencode native health | `:3191` |

No journal is deleted, no Store is reset, no `rm`/`truncate`/state reset enters the script diff,
and the moved leg keeps every one of its refusal assertions. The old placement carried the
fixture-inventory rule "intentionally first so another row cannot mask the proof"
(`docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82`); that rule is now carried by an
explicit pin: the leg reads its own captured App stderr
(`production_linux_v2_owner_cli_test.go:4839`) and requires the mount-identity phrase while
forbidding the foreign-ownership phrase (`linuxV2RequireMountRefusalCause`, `:2304`), and it
requires its own retained row to stay claim-only (`linuxV2RequireClaimOnlyRetainedRow`, `:2375`;
call site `:4859`). A runtime guard inside `run_alternate_mount_refusal_witness` (`:3033-3042`)
fails the leg unless all ten prior event files are already non-empty (`:3041`), so a future
reorder cannot silently reintroduce the wall.

Local order and guard proof (scratch harnesses, not committed; pre-change bytes snapshotted from HEAD
to `Q:/scratch-shared-journal/pre-change-acceptance.sh`):

| check | input | result |
| --- | --- | --- |
| `check-order.sh` | pre-change bytes (`git show HEAD:…`) | `ORDERING-RED status=1` — mount leg at `:3160` before all ten prior legs, guard absent |
| `check-order.sh` | delivered bytes | `ORDERING-GREEN status=0` — ten legs before `:3190`, opencode after, guard present |
| `guard-harness.sh` (a) | ten non-empty event files | `GUARD-OK`, exit 0 |
| `guard-harness.sh` (b) | `pi` event file empty (the pre-change ordering) | `fail` names the path, exit 1 |
| `guard-harness.sh` (c) | `v1-replay` event file missing | `fail` names the path, exit 1 |

The guard block the harness executes is extracted verbatim from the delivered script
(`sed -n '3033,3042p'`).

**Coupling recorded as a residual** (`docs/TODO.md`): the mount leg stays before the opencode leg
only because that leg aborts at its pinned-version config gate
(`production_linux_v2_native_witness_test.go:1901`) before the prior-journal precondition
(`:1968-1975`). If that version gate is fixed, the opencode leg would read the mount leg's
claim-only row; the mount leg must then move after the opencode leg in the same change (or the
opencode precondition needs its own released-store guarantee).

## 4. Compile and vet

| gate | command (from `daemon/`) | exit |
| --- | --- | --- |
| formatting | `gofmt -l e2e/` | 0, no output |
| Windows vet, full module | `go vet ./...` | 0 |
| Linux vet, changed package | `GOOS=linux go vet ./e2e/` | 0 |
| Linux vet, full module | `GOOS=linux go vet ./...` | 0 |
| script syntax (repo root) | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 |

## 5. One red mutant per new branch

Run in a scratch clone (`/tmp/shared-journal-mutants`, WSL) by
`Q:/scratch-shared-journal/mutants.sh`; each row replaces exactly one term inside its owning
function, runs the named test, and restores the pristine bytes. Baselines (unmutated clone and
post-restore clone): both `ec=0`, no `FAIL`.

| mutant (term removed/inverted) | test | exit | red row |
| --- | --- | --- | --- |
| `journal.LocalState != "claimed"` | claim-only row | 1 | `…/local_state_is_not_claimed` |
| `journal.ClaimID == ""` | claim-only row | 1 | `…/the_claim_id_is_missing` |
| `journal.LeaseToken == ""` | claim-only row | 1 | `…/claim_grant_is_incomplete` |
| `journal.LeaseExpiresAt.IsZero()` | claim-only row | 1 | `…/the_claim_grant_has_no_expiry` |
| `journal.LinuxCloneIntent != nil` | claim-only row | 1 | `…/a_clone_intent_was_reserved` |
| `journal.HasProcessDetails()` | claim-only row | 1 | `…/process_details_were_recorded` |
| `journal.HasPendingContainment()` | claim-only row | 1 | `…/a_scan_ledger_was_recorded` (+ `…/process_authority_was_recorded`, `…/a_launch_handoff_fence_was_written`) |
| `journal.ContainmentUnproven` | claim-only row | 1 | `…/an_unproven_stop_was_recorded` |
| `journal.ContainmentCgroup != nil` | claim-only row | 1 | `…/an_aborted_cgroup_reservation_was_written` |
| `journal.ContainmentCgroupReleaseProof != nil` | claim-only row | 1 | `…/a_release_proof_was_written` |
| `journal.ContainmentCgroupPreGateReleaseProof != nil` | claim-only row | 1 | `…/a_pre-gate_release_proof_was_written` |
| `!found` (missing-row refusal) | claim-only row | 1 | top-level `TestLinuxV2AlternateMountRefusalRetainsTheClaimOnlyRow` |
| witness-marker `strings.Contains` | refusal cause | 1 | `…/an_empty_capture_is_refused` (+ `…/the_bare_preflight_probe_without_the_witness_is_refused`) |
| foreign-phrase `strings.Contains` | refusal cause | 1 | `…/a_foreign_RunJournal_refusal_is_refused` (+ `…/a_capture_naming_both_causes_is_refused`) |
| mount-phrase `strings.Contains` | refusal cause | 1 | `…/an_unrelated_witness_refusal_is_refused` |
| stderr read refusal (`return nil, nil`) | unreadable capture | 1 | top-level `TestLinuxV2AlternateMountRefusalCauseRefusesAnUnreadableCapture` |
| `!snapshot.owner.isStoppedRetained()` in `linuxV2RequireSafelyReleasedPriorJournal` | claim-only prior record | 1 | `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` at `:2292` (`lacks an exact v2 StopReceipt-backed leaf release, want the released-and-safe refusal`) |

Two review findings from the first mutant round were fixed rather than papered over: the initial
row predicate also named `journal.ContainmentAuthority != nil` and `journal.ContainmentHandoff
!= nil`, which no mutant could red because `HasPendingContainment()` already covers them (both
were deleted), and the missing-row assertion was rewritten to pass a claim-only-shaped journal
with `found=false` so the `!found` branch has a row that actually reds it. `ContainmentCgroup !=
nil` was kept beside `HasPendingContainment()` because an aborted reservation
(`AbortProof != nil`) escapes the latter; `an_aborted_cgroup_reservation_was_written` is the row
that reds only for that disjunct.

## 6. Full suites

| gate | command | exit | result |
| --- | --- | --- | --- |
| Windows full module | `go test ./...` from `daemon/` (go1.27.4 windows/amd64) | 0 | `27 ok`, `0 FAIL`, `3` packages with no test files; `internal/app` 113.250s, `internal/workspace` 164.285s, `cmd/symmetry-fake-agent` 10.179s, rest cached or seconds |
| WSL full module, root | `go test ./...` from `/mnt/q/repos/wt-acc-journal-release/symmetry/daemon` in `wsl -d Ubuntu-24.04` (go1.27.0 linux/amd64, uid 0) | 1 | `30 ok` incl. `daemon/e2e`; `internal/platform` fails 13 wire-shape tests, each `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` |
| WSL full module, unprivileged | same command in `wsl -d Ubuntu-24.04 -u accept` (uid 1000) with `GOMODCACHE=/tmp/accept-gomod GOCACHE=/tmp/accept-home/.cache/go-build GOPROXY=off GOFLAGS=-mod=mod` | 1 | `27 ok` incl. `daemon/e2e`; `internal/launchercore` 3 + `internal/linuxpreflight` 1 (both: a `/tmp` ancestor "has unsafe owner or mode"), `internal/platform` 5 (`Linux supervisor recovery endpoint is invalid`), `native/linux-gate/protocol` 1 FD census |
| base-vs-head attribution | the failing tests above re-run at `7111828b` (detached worktree `/tmp/base-attrib`) and at the delivered bytes | — | failure sets identical at both revisions: root 13 = 13 (`internal/platform`); unprivileged 3 + 1 + 5 identical names, and the protocol FD-census test passes isolated at both. Two further one-off flakes (`TestLinuxSupervisorOwnerLossProofBarrierIsPostReapAndOptIn` twice under load; the FD census inside the accept full run) pass 3/3 isolated at head and base and at head in the root full run |
| new rows, verbose | `go test ./e2e/ -run '^TestLinuxV2(ClaimOnlyPriorJournalRequiresPhysicalRelease\|AlternateMountRefusalCauseRequiresThePreflightIdentityRefusal\|AlternateMountRefusalCauseRefusesAnUnreadableCapture\|AlternateMountRefusalRetainsTheClaimOnlyRow)$' -count=1 -v` (WSL, root) | 0 | 25 `RUN`, 25 `PASS`, 0 `FAIL` (4 top-level rows and 21 subtests) |

This host yields no single-identity green WSL suite: the wire-shape harness refuses uid 0 and the
privileged gate/supervisor tests need root or a root-owned TMPDIR; every failing package is
outside this change's footprint (one `e2e` test file and one CI script), and the two identities
agree on the failure sets at `7111828b` and at the delivered bytes.

## 7. Comment cap

`python C:/Users/lhan/.local/state/gdd/scratch/comment-cap.py Q:/scratch-shared-journal/clone
7111828b` → exit 0, no output: the clone carries a commit of the delivered bytes on top of
`7111828b`, so every added comment block in the diff is ≤2 lines. Run before the final
re-measurement of the digests and anchors in §10.

## 8. Where the refused claim row lives, and how the product retires it

`seedAuthenticClaim` writes `claimed` before the native-owner preflight
(`linux_owner_witness_linux.go:3243` vs `:3253`), and the refusal exits without a release or
clear step. The row is retired only by the product's terminal/cleanup path
(`cleanupPending` `daemon/internal/app/cleanup.go:893` → `DeleteJournal`
`daemon/internal/state/run_journal.go:423`, single production caller at `cleanup.go:940`);
containment-only recovery no-ops on it (§2(b)). The guest's shared StateDir therefore keeps such a
row until teardown. This goal orders around it instead of touching durable state; a product-side
retirement for a claim that provably never started native work is a separate, unowned decision
(contract class DECISION_NEEDED).

## 9. Nothing weakened

- The Go diff deletes exactly 8 lines, all inside `runProductionLinuxV2AlternateMountRefusal`
  (the old `if found { … }` block); no assertion is deleted or renamed.
- `linuxV2RequireSafelyReleasedPriorJournal` (`production_linux_v2_owner_cli_test.go:2259`) is
  byte-unchanged: `git diff` touches no line of it. The new row
  `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` (`:2282`) pins that a claim-only prior
  record is refused with `may still own or require recovery`; its red row is the mutant that
  removes `!snapshot.owner.isStoppedRetained()` from the helper's disjunction — the row then fails
  on the changed refusal text (recorded in §5).
- `daemon/e2e/production_linux_v2_native_witness_test.go` (the `:91`/`:1973` precondition and its
  wording) is untouched: it is absent from `git diff --name-only`.
- The moved leg keeps every prior assertion: process failure, non-zero exit, empty response, no
  reservation marker, no hook marker, and the journal-history-preservation check; the new checks
  are additive.

## 10. Anchor and digest re-measurement at the delivered bytes

Delivered files and their sha256 at the delivered bytes (measured after the §7 comment-cap run;
`docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record are deliberately not hashed, and
`daemon/e2e/production_linux_v2_native_witness_test.go` is unchanged by design):

| file | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `9540ffa1c82602f7a8a7c8062069a112dc5372a116616f344c8f4c8e8656ebe8` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `4b7412f441d2e58073bd8e00600bd5ca09886c0a6a901987ddde753c3d4c684d` |
| `docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md` | `9b22377ef77af919f4e476ae132945c4264f6aad8c875c4e12c5d19eea5510b9` |

Every anchor cited above and in the plan, measured with `grep -n`/`sed -n` on the delivered
bytes:

| anchor | cited construct |
| --- | --- |
| `daemon/cmd/symmetry-daemon/main.go:35` | `-linux-v2-native-owner-witness: %v` stderr print |
| `linux_owner_witness_linux.go:3240,3243,3253` | admission check, claim seed, owner preparation |
| `linux_owner_witness_linux.go:3258` | `preflight installed Linux v2 owner before clone/native work` |
| `linux_owner_witness_linux.go:4257,4262,4273,4284,4292-4295` | `seedAuthenticClaim`: intent, `claiming`, grant, `claimed` readback |
| `native_run.go:169,197,199` | factory owner, `owner.Preflight`, `Linux v2 native owner preflight` wrap |
| `linux_owner_v2_linux.go:91` | `VerifyLinuxOwnerInstallation` call inside `Preflight` |
| `linuxpreflight/cgroup_linux.go:80,93` | `openConfiguredLinuxOwnerParent`, the mount-identity error |
| `platform/linux_cgroup_v2_linux.go:244` | parent identity carries `MountID` |
| `authority/cgroup.go:25-31` | `LinuxCgroupParentIdentity` incl. `MountID` |
| `app/cleanup.go:893,894-896,940` | `cleanupPending`, its state gate, the `DeleteJournal` call |
| `state/run_journal.go:423` | `DeleteJournal` definition |
| `app/linux_owner_recovery_linux.go:1622` | `recoverContainmentOnlyJournals` |
| `app/cleanup_glue.go:414-419` | `hasLinuxV2ContainmentState` (false for a claim-only row) |
| `state/linux_cgroup_journal.go:382,432-475` | foreign-ownership refusal, `linuxV2ForeignJournalIsSafelyRetired` |
| `state/linux_cgroup_v2_test.go:973,1095` | `TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease`, "unreleased row" |
| `e2e/production_linux_v2_native_witness_test.go:1901,1968,1972,1973,1974,2183,2216` | pinned-CLI gate, precondition snapshot/loop/check/fatal, stderr path and tail helpers |
| `e2e/production_linux_v2_owner_cli_test.go:54,1627,2259,2282,2298,2302,2304,2375,4837,4839,4843,4859` | App stderr FD, pre-existing log-only read, precondition helper, new rows and predicates, leg assertions |
| `scripts/ci/linux-v2-native-acceptance.sh:3033-3042,3170-3187,3190,3191` | guard block, ten prior legs, mount leg, opencode leg |
| `docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82` | "intentionally first … cannot mask the proof" |
| `run-45737002125.log:3146,3156,3226,3229` | journal insert, prior leg green, the stop, the retained-row projection |

## 11. Acceptance carried to the conductor's Bind

Delivered, pending the conductor's Bind. The Bind must run `shared` once on the PR head and
observe that the `:91` pre-existing-RunJournal stop is gone; the next stop (or green) is recorded
verbatim, and if the run reaches `stop_readback_reader` its Result row is recorded. Two limbs
cannot execute outside the guest and are proven locally by the rows above instead: (i) the
moved leg's new stderr/cause assertions, whose text is grounded in the App's print chain (§1) and
whose predicates are unit-pinned (§5); (ii) the ten legs' own compliance with the fresh/released
precondition, which is the Bind's subject. `docs/archive/CLOSED-TODO.md` carries the closure
statement in the "delivered, pending the conductor's Bind" form.

## 12. Limits and residual risk

- The new cause assertion is exact-text: if the guest's App prints the mount refusal through a
  different wrapper, the leg reds and prints the bounded captured stderr tail, so the failure is
  loud and diagnosable rather than silent — but the Bind is what proves the guest text.
- The opencode ordering coupling of §3 is a residual in `docs/TODO.md`; if that leg's
  pinned-version gate is fixed without moving the mount leg, the next `shared` run stops at the
  opencode leg for this same wall.
- The unmeasured `stop_readback_reader` row stays unmeasured until a `shared` run passes the
  opencode leg (`docs/TODO.md`, readback residual).
- This stage pushes nothing and commits nothing: the delivered bytes are the worktree files, and
  no PR body exists to rewrite yet.
