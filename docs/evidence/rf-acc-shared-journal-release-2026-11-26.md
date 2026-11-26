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

## 3. Round-0 fix and order pin (superseded by §15)

**This section describes the round-0 (`84dc22c3`) bytes; round 1 replaced the ordering with the
leg's own case set (§15).** The `shared` arm ran the mount-refusal leg after every leg that
starts a fresh native-owner witness, and immediately before the frozen `opencode_native_health`
leg (`scripts/ci/linux-v2-native-acceptance.sh`):

| leg | line |
| --- | --- |
| pi provider action | `:3167` |
| helper sigkill detached child | `:3171` |
| daemon sigkill helper survives | `:3172` |
| stage-replay cases | `:3173` |
| ack-loss replay cases | `:3176` |
| helper exit after release before register | `:3179` |
| helper response loss | `:3180` |
| receipt persist failure | `:3182` |
| literal pid reuse | `:3183` |
| legacy v1 replay | `:3184` |
| **alternate-mount refusal** | **`:3187`** |
| frozen opencode native health | `:3191` |

No journal is deleted, no Store is reset, no `rm`/`truncate`/state reset enters the script diff,
and the moved leg keeps every one of its refusal assertions. The old placement carried the
fixture-inventory rule "intentionally first so another row cannot mask the proof"
(`docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82`); that rule is carried by an
explicit pin: the leg reads its own captured App stderr
(`production_linux_v2_owner_cli_test.go:4857`) and requires the mount-identity phrase on a line
that also carries the witness marker, while forbidding the foreign-ownership phrase
(`linuxV2RequireMountRefusalCause`, `:2314`), and it requires its own retained row to stay
claim-only (`linuxV2RequireClaimOnlyRetainedRow`, `:2396`; call site `:4875`). Round 0 also
guarded the order at runtime (`:3034-3039`, ten prior event files as arguments); round 1 deletes
that guard because the leg no longer shares the Store (§15).

Local order and guard proof (scratch harnesses, not committed; base bytes snapshotted with
`git show 7111828b:scripts/ci/linux-v2-native-acceptance.sh` to
`<HOST-PATH>/scratch-shared-journal-apply/base-7111828b-acceptance.sh`; re-run at the apply-round
bytes, and the guard rows re-run at the fix-round bytes):

| check | input | result |
| --- | --- | --- |
| `order-check.sh` | base `7111828b` bytes | `ORDERING-RED status=1` — mount leg at `:3160`, before all ten prior legs (`:3161-:3178`), guard absent |
| order re-measure (`grep -n`/`sed -n`) | delivered bytes | ten legs (`:3167-:3184`) before the mount call at `:3187-3190`, opencode after at `:3191`, and the call passes the ten prior event files |
| `guard-harness.sh` (a) | ten non-empty event files | `GUARD-OK`, exit 0 |
| `guard-harness.sh` (b) | `pi` event file empty | `fail` names the path, exit 1 |
| `guard-harness.sh` (c) | `v1-replay` event file missing | `fail` names the path, exit 1 |
| `guard-harness.sh` (d) | zero prior event files (empty argument list) | `fail` names the missing legs, exit 1; the pre-fix bytes exited 0 silently (§14) |

The guard block the harness executes is extracted verbatim from the delivered script
(`sed -n '3034,3039p'`).

**Corrected in round 1 (Sol P1):** the round-0 claim that the opencode leg was harmless was
false. The opencode leg's fixture reads the shared Store at
`production_linux_v2_native_witness_test.go:1968-1976` before the App's opencode version gate, so
with the mount leg in `shared` the opencode leg would have failed at `:1974` for the mount leg's
retained row — the same wall, re-labelled. Round 1 removes the coupling entirely by running the
mount leg in its own case set (§15); the opencode leg's pinned-version behavior is unrelated.

## 4. Compile and vet

| gate | command (from `daemon/`) | exit |
| --- | --- | --- |
| formatting | `gofmt -l e2e/` | 0, no output |
| Windows vet, full module | `go vet ./...` | 0 |
| Linux vet, changed package | `GOOS=linux go vet ./e2e/` | 0 |
| Linux vet, full module | `GOOS=linux go vet ./...` | 0 |
| script syntax (repo root) | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 |

All five commands were re-run at the apply-round bytes (2026-11-26) and exited 0.

## 5. One red mutant per new branch

Re-run at the apply-round bytes in a scratch copy (`<HOST-PATH>/tmp/shared-journal-apply-mutants`,
WSL) by `<HOST-PATH>/scratch-shared-journal-apply/mutants.sh`; each row replaces exactly one term
inside its owning function, runs the named test, and restores the pristine bytes. Baselines
(unmutated copy and post-restore copy): both `ec=0`, no `FAIL`.

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
| mount-check marker term `strings.Contains(line, …)` → `false` | refusal cause | 1 | `…/the_exact_mount_preflight_refusal_is_accepted` |
| mount-phrase scoping reverted to window-wide (`line` → `window`) | refusal cause | 1 | `…/the_mount_phrase_without_the_witness_marker_on_its_line_is_refused`, alone; restored copy `ec=0` |
| stderr read refusal (`return nil, nil`) | unreadable capture | 1 | top-level `TestLinuxV2AlternateMountRefusalCaptureReadRefused` at `:2390` |
| argument-count guard line deleted | order guard, zero prior legs | 0 (silent pass under mutation) | the zero-argument refusal row (harness (d)); oracle gap — the row lives in the scratch harness, so no CI job reds it (§14) |
| `!snapshot.owner.isStoppedRetained()` in `linuxV2RequireSafelyReleasedPriorJournal` | claim-only prior record | 1 | `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` at `:2302` (`lacks an exact v2 StopReceipt-backed leaf release, want the released-and-safe refusal`) |

Two review findings from the first mutant round were fixed rather than papered over: the initial
row predicate also named `journal.ContainmentAuthority != nil` and `journal.ContainmentHandoff
!= nil`, which no mutant could red because `HasPendingContainment()` already covers them (both
were deleted), and the missing-row assertion was rewritten to pass a claim-only-shaped journal
with `found=false` so the `!found` branch has a row that actually reds it. `ContainmentCgroup !=
nil` was kept beside `HasPendingContainment()` because an aborted reservation
(`AbortProof != nil`) escapes the latter; `an_aborted_cgroup_reservation_was_written` is the row
that reds only for that disjunct.

The last two rows (the deleted guard line and the zero-argument refusal) describe the round-0
`84dc22c3` bytes; round 1 deletes the order guard (§15), so those rows are historical.

## 6. Full suites

| gate | command | exit | result |
| --- | --- | --- | --- |
| Windows full module | `go test ./... -count=1` from `daemon/` (go1.27.4 windows/amd64) | 0 | `27 ok`, `0 FAIL`, `3` packages with no test files; `internal/app` 362.625s, `internal/workspace` 296.378s, `cmd/symmetry-fake-agent` 80.563s, `internal/state` 60.966s, `internal/platform` 39.273s; 416.6s wall |
| WSL full module, root | `go test ./... -count=1` from `<HOST-PATH>/mnt/q/repos/wt-acc-journal-release/symmetry/daemon` in `wsl -d Ubuntu-24.04` (go1.27.0 linux/amd64, uid 0) | 1 | `30 ok` incl. `daemon/e2e` (1.804s); `internal/platform` fails 13 wire-shape tests, each `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`; 422.7s wall |
| WSL full module, unprivileged | same command in `wsl -d Ubuntu-24.04 -u accept` (uid 1000) with `GOMODCACHE=<HOST-PATH>/tmp/accept-gomod GOCACHE=<HOST-PATH>/tmp/accept-home/.cache/go-build GOPROXY=off GOFLAGS=-mod=mod` | 0 | `31 ok`, 0 `FAIL`, incl. `daemon/e2e` (1.678s) and `internal/platform`; 282.3s wall |
| byte-identity attribution | `git diff --name-only 7111828b..HEAD` plus the apply-round worktree status | — | the range's files are exactly six (the e2e test file, the acceptance script, the plan, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, this record); `daemon/internal/platform`, `internal/launchercore`, `internal/linuxpreflight` and `native/linux-gate/**` are untouched, so their bytes are identical at base and at the delivered bytes and the root-identity failures cannot come from this change (the implement round's base-vs-head runs showed the same sets: root 13 = 13, unprivileged names identical) |
| new rows, verbose | `go test ./e2e/ -run '^TestLinuxV2(ClaimOnlyPriorJournalRequiresPhysicalRelease\|AlternateMountRefusalCauseRequiresThePreflightIdentityRefusal\|AlternateMountRefusalCaptureReadRefused\|AlternateMountRefusalRetainsTheClaimOnlyRow)$' -count=1 -v` (WSL, root) | 0 | 26 `RUN`, 26 `PASS`, 0 `FAIL` (4 top-level rows and 22 subtests: 7 cause-predicate rows and 15 row-predicate rows) |

On the apply-round bytes the unprivileged identity is fully green (`31 ok`, 0 `FAIL`), and the
root identity fails only the 13 `internal/platform` wire-shape tests, which refuse uid 0 by
construction; every other package passes as root, including `daemon/e2e`.

## 7. Comment cap, duplication and record cleanliness

`python <HOST-PATH>/.local/state/gdd/scratch/comment-cap.py
<HOST-PATH>/scratch-shared-journal-apply/clone 7111828b` → exit 0, no output: the scratch clone
carries a commit of the apply-round bytes on top of `7111828b`, so every added comment block in
the diff is ≤2 lines. Run before the final re-measurement of the digests and anchors in §10.

From the same clone: `python scripts/ci/jscpd-changed.py --base 7111828b --head HEAD` → exit 0
(6 changed files, 1775 scanned sources, 1254 corpus clones, 0 tolerated among the added lines,
none new; notices: the e2e test file is above the gate's line ceiling and
`docs/archive/CLOSED-TODO.md` lies outside the configured scope, so clones inside them are not
checked). `python scripts/ci/check-record-cleanliness.py --base 7111828b --head HEAD` → exit 0
(this record passes the root-only sanitize check).

The fix round re-ran all three gates on the same scratch-clone shape at the fix-round bytes
(`<scratch>/clone`, §14): comment-cap exit 0; jscpd exit 0; check-record-cleanliness exit 0. The
whole-document sanitize check is green on `docs/TODO.md`, the plan and this record, and on
`docs/archive/CLOSED-TODO.md` over the branch's added lines only: that file's whole-file flags are
pre-existing entries, identical at `7111828b` (§14).

## 8. Where the refused claim row lives, and how the product retires it

`seedAuthenticClaim` writes `claimed` before the native-owner preflight
(`linux_owner_witness_linux.go:3243` vs `:3253`), and the refusal exits without a release or
clear step. The row is retired only by the product's terminal/cleanup path
(`cleanupPending` `daemon/internal/app/cleanup.go:893` → `DeleteJournal`
`daemon/internal/state/run_journal.go:423`, single production caller at `cleanup.go:940`);
containment-only recovery no-ops on it (§2(b)). The guest's shared StateDir therefore keeps such a
row until teardown. Round 1 gives the leg its own case set and Store instead of touching durable
state (§15); a product-side retirement for a claim that provably never started native work is
recorded as the option-(a) residual in `docs/TODO.md`.

## 9. Nothing weakened

- Versus base `7111828b` the Go file is +201/−11 (`git diff --numstat` at the apply-round bytes):
  the 11 deleted lines are the 8-line old `if found { … }` block inside
  `runProductionLinuxV2AlternateMountRefusal` and the 3-line duplicated stderr loop now behind
  `linuxV2LogOwnerWitnessStderr` (`production_linux_v2_owner_cli_test.go:1614`). One table row's
  name changed in the apply round (`the row is gone` → `a zero-value row is refused`, `:2421`):
  the row mutates the journal to its zero value and reds the same `LocalState != "claimed"` term,
  so the rename makes the name true and no assertion is deleted, dropped or weakened.
- `linuxV2RequireSafelyReleasedPriorJournal` (`production_linux_v2_owner_cli_test.go:2265`) is
  byte-unchanged: `git diff` touches no line of it. The new row
  `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` (`:2295`) pins that a claim-only prior
  record is refused with `may still own or require recovery`; its red row is the mutant that
  removes `!snapshot.owner.isStoppedRetained()` from the helper's disjunction — the row then fails
  on the changed refusal text (recorded in §5).
- `daemon/e2e/production_linux_v2_native_witness_test.go` (the `:91`/`:1973` precondition and its
  wording) is untouched: it is absent from `git diff --name-only`.
- The moved leg keeps every prior assertion: process failure, non-zero exit, empty response, no
  reservation marker, no hook marker, and the journal-history-preservation check; the new checks
  are additive. The apply round moved the leg's stderr-tail logging and the shared claim-only
  journal literal into helpers (`:1614`, `:2288`) without changing what any row asserts.

## 10. Anchor and digest re-measurement at the delivered bytes

Delivered files and their sha256 at the round-0 fix-round delivered bytes (`84dc22c3`; measured
over the delivered bytes, the same bytes the §7 clone gates ran on; `docs/TODO.md`,
`docs/archive/CLOSED-TODO.md` and this record are deliberately not hashed, and
`daemon/e2e/production_linux_v2_native_witness_test.go` is unchanged by design). Round 1
re-measured the script's anchors (§15.1); the script row below carries the head anchors:

| file | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `abdb383be31d70cd5b591cc8f927c2c2ec0e4fab196041b0a31ab91bd5e37012` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `3254deef0bfdc16c741542623c293012aa1e1c265e6f881d74792121280f7bcb` |
| `docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md` | `5dadbcbaaacab9e09254a44bbfb9392d5cca08f2f1b956cf63067d5769295237` |

Every anchor cited above and in the plan, measured with `grep -n`/`sed -n` on the round-0
delivered bytes (`84dc22c3`), except the script row, which is re-measured at the head `66b27e63`
(round 1 moved the guard and the mount call; §15.1). The Bind log's retained copy still hashes
to `bf00da4f…` at `scratch/bind296r2/run-45737002125.log` under the GDD state root, and the
probe line cited in §1 is `native/run-45384436642.log:1227`:

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
| `e2e/production_linux_v2_owner_cli_test.go:54,1614,1640,2265,2288,2295,2308,2312,2314,2358,2387,2396,2421,4857,4861,4862,4875` | App stderr FD, the stderr-tail logger, the pre-existing report's call, the precondition helper, the shared claim-only literal, the new rows and predicates, the renamed capture-read row, the zero-value row, the leg's read/cause/log and row call |
| `scripts/ci/linux-v2-native-acceptance.sh:33,3031-3032,3213-3215` | at the head: the case-set allowlist (with `alternate_mount_refusal`), the mount leg's 2-line why comment at the deleted guard's site, its dispatch arm; round-0's guard and call anchors (`:3034-3039,3167-3184,3187-3190,3191`) are historical (§3, §15.1) |
| `docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82` | "intentionally first … cannot mask the proof" |
| `run-45737002125.log:3146,3156,3226,3229` | journal insert, prior leg green, the stop, the retained-row projection |

## 11. Acceptance carried to the conductor's Bind

**Met by Bind 2** (run `45778210044` on the head `66b27e63`; §Bind 2 below). Under the round-1
acceptance amendment the Bind ran `shared` **and** `alternate_mount_refusal` once each on the PR
head: `shared` keeps the `:91` pre-existing-RunJournal stop gone (its next stop, the pi
provider-scoped-admission residual, recorded verbatim), and the new set passed its mount-refusal
leg and exported `alternate-mount-refusal.json`. Two limbs cannot execute outside the guest; the
Bind's passing leg is their guest proof, and the local rows above and in §15 stand behind them:
(i) the leg's stderr/cause assertions, whose text is grounded in the App's print chain (§1) and
whose predicates are unit-pinned (§5); (ii) the leg's own compliance with the fresh/released
precondition. `docs/archive/CLOSED-TODO.md` carries the closure citing the run.

## Bind (diagnostic) — run on the fixed head 84dc22c3 (shared)

*Diagnostic, not the binding record: round 1 (§15) moved the mount-refusal leg to its own case
set (`alternate_mount_refusal`), and the conductor's binding run is Bind 2 on `66b27e63`
(below).*

One `shared` run on the exact PR-head bytes plus the two scratch-only files, watched once, then
the scratch branch deleted and the temporary worktree removed. Full report:
`findings/300-binding-run.md`; the run log is retained at `scratch/bind300/run-45771518805.log`
under the GDD state root (sha256
`ca02616835c34affb7ee80f19cc309eec0d34bb261b12acc7d368e08cac2eef6`, 488559 B, 3096 lines).

- Run `45771518805` (event `push`, `run_attempt=1`, head
  `2dfde5e4d03d204c7499eac2a08006ff43e2295e` = `84dc22c3` + the two scratch-only files, created
  06:54:59Z, conclusion `failure`); `total_count=1` on every read once the run row was indexed
  (06:55:32Z, 07:00:49Z, 07:04:04Z, and after the branch deletion 07:04:16Z).
- The `:91 pre-existing RunJournal … is not physically released` stop is **gone**: 0 occurrences
  of `pre-existing RunJournal`, `is not physically released` and `may still own or require
  recovery` in the whole log, and 0 of every earlier stop class
  (`clone_integration_linux_test.go:252`, the `launcher_integration_linux_test.go` rows,
  `open /proc/`, `/proc/`, `permission denied`, `stage leaf`, `not a member of the named leaf`).
- The set's first failing line is the pi leg's clone-reserve refusal — the pre-existing
  `pi_provider_action` provider-scoped-admission residual (`docs/TODO.md`, 2026-11-26, "still
  need a provider-scoped admission … needs its own ruling"), not this change; the moved
  alternate-mount leg never executed before the abort:

  ```
  Go native acceptance event gate failed: bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0, skipped=[], fail_events=2, invalid_json_lines=0
  witness output:     production_linux_v2_native_witness_test.go:91: owner-witness command exit status: 1
  witness output:     production_linux_v2_native_witness_test.go:91: owner-witness stderr: -linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject
  ```

  The retained row is the pi leg's own claim-only journal
  (`journal-ab37035b11d22622d0a2675198c4a187862b2d6ae4936c446a14b16c92aa019b.json
  {"local_state":"claimed","has_claim_grant":true,"has_clone_intent":false,
  "has_seed_receipt":false,"recovery_required":false}`).
- Every leg that ran verified green: all 21 case-set `verified … run=1 pass=1 skip=0` rows up to
  the stop (unprovisioned refusal; the seven AppArmor rows; the legacy Pi/OpenCode refusals; the
  B-caller and cross-mount cleanup denials; the unexpected-cgroup-procs-FD witness; the four
  installed-owner scratch legs including `TestInstalledLinuxOwnerBoundaryScratch`; the four
  marker-reader rows), then the pi leg is the first red. The readback reader start passed
  (`root readback reader ready: unit=symmetry-ci-readback-reader.service MainPID=7213`); the pi
  leg fails at its clone reserve, so `stop_readback_reader` (single call site `:3236`) did not
  run and no Result row exists — unmeasured, not failed.
- Artifacts (sha256 = zip bytes = API `digest`): `linux-v2-native-evidence-45771518805-1`
  id `15094073615` sha256
  `2f927ea232783189d8d560fec753509854d0be386e63c99890f5bbe5bbe9bf89` (members
  `shared/pi-native-provider-action.json`
  `c6338eacac6178c37f29dd376f2ff8dc8b74b2461a32b7e5c5d7a94db02cf550` and
  `shared/unprovisioned-owner-refusal.json`
  `676c81e374c86eb94fbd9d7a79a085ab18de51b06389b8e371a7565e8ab41a5f`);
  `linux-v2-host-prerequisite-45771518805-1` id `15093937858` sha256
  `2b4746da712d846aa299825f0b917491a23ddd178a6c5a554ab6fe372fa72bec` (`status: passed`,
  `source_commit` = the run head).
- Run-input sha256 equality (run head `2dfde5e4` vs PR head `84dc22c3`):

| file | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `abdb383be31d70cd5b591cc8f927c2c2ec0e4fab196041b0a31ab91bd5e37012` | `abdb383be31d70cd5b591cc8f927c2c2ec0e4fab196041b0a31ab91bd5e37012` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `3254deef0bfdc16c741542623c293012aa1e1c265e6f881d74792121280f7bcb` | `3254deef0bfdc16c741542623c293012aa1e1c265e6f881d74792121280f7bcb` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The four record-only docs (`docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the plan, this record)
are not run inputs; the full verified-row list and raw line numbers are in the findings file.

## 12. Limits and residual risk

- The new cause assertion is exact-text: if the guest's App printed the mount refusal through a
  different wrapper, the leg reds and prints the bounded captured stderr tail, so the failure is
  loud and diagnosable rather than silent. Bind 2's `alternate_mount_refusal` passed, which is
  the guest proof that the pinned text matches; the pin also requires the witness marker and the
  mount phrase on the same line, which matches the App's single print
  (`cmd/symmetry-daemon/main.go:35` prints one `%v` line through the single-line wrappers at
  `linux_owner_witness_linux.go:3258` and `native_run.go:199`).
- The round-0 opencode coupling of §3 no longer exists: round 1 runs the mount leg in its own
  case set (§15), so no `shared` leg can observe the retained row.
- The `stop_readback_reader` row was measured clean in Bind 2's `alternate_mount_refusal`
  (`readback-reader: stop signal requests=0`, 0 `did not stop cleanly`); `shared` still aborts
  before its single call site, so a `shared`-scoped stop row stays unmeasured (`docs/TODO.md`,
  readback residual).
- This stage pushes nothing and commits nothing: the delivered bytes are the worktree files; the
  PR body for the delivered state is `findings/300-body.md` (§15).

## 13. Apply round (2026-11-26, accepted simplify findings)

The conductor's simplify review produced ten findings against this increment: nine were accepted
(all applied, below) and one rejected because its fix needs a file outside the contract's owned
set. Only test, script and record bytes changed; no production byte and no acceptance condition
moved.

| finding | file | change |
| --- | --- | --- |
| F1 | `production_linux_v2_owner_cli_test.go` | the claim-only journal literal is now `linuxV2ClaimOnlyRefusalJournal` (`:2288`), used by both rows |
| F2 | same | the misnamed table row `the row is gone` renamed to `a zero-value row is refused` (`:2421`); the mutation is unchanged |
| F3 | `linux-v2-native-acceptance.sh` | the order guard takes the ten prior event files as arguments (`shift`, `for prior in "$@"`, `:3034-3039`) instead of reading ten globals; the call site passes them (`:3187-3190`); the fix round adds the empty-argument refusal (§14) |
| F4 | `production_linux_v2_owner_cli_test.go` | the bounded stderr tail logging is `linuxV2LogOwnerWitnessStderr` (`:1614`), used by the pre-existing refusal report and the mount leg |
| F5 | `linux-v2-native-acceptance.sh` | the call-site comment again carries the frozen-opencode ordering fact (still two lines) |
| F6+F7 | `production_linux_v2_owner_cli_test.go` | the mount phrase must sit on a line that also carries the witness marker (`:2322-2326`), and the masking capture gets its own table row (`:2358`) |
| F8 | `docs/TODO.md` | the `:91` stop reads "delivered, pending the conductor's Bind", matching `CLOSED-TODO` |
| F9 | `docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md` | predicate signature and row counts corrected (7 cause rows, 15 row rows, four new top-level rows) |
| F10 | (rejected) | the fixture-inventory row lives in another goal's record (`docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82`), outside this contract's owned files; §3 keeps the supersession in prose |

The two new pins carry their own red proof: with the pre-apply predicate the masking row fails
(`the mount phrase without the witness marker on its line is refused`, exit 1 — the row is new
coverage, not a tautology), and with the apply-round predicate the accepted row fails if the
fixture drops the witness-marked wrapper chain (`captured alternate-mount stderr carries no App
witness refusal`). The mutant table (§5) was re-run at these bytes.

The re-measure also caught one stale plan anchor: the probe-log line carrying the cited phrase is
`gdd/native/run-45384436642.log:1227`, not `:1231` (the plan now cites `:1227`).

## 14. Fix round (2026-11-26, review findings)

The review round (stage answer under the GDD state root) reported three P3 findings against
`7111828b..3fc9526b`; all three are inside this contract's owned set and are fixed at the
delivered bytes. No assertion, predicate, refusal condition or acceptance row moved.

| id | file | before → after | proof at the delivered bytes |
| --- | --- | --- | --- |
| 1 | `docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md` | "claim id and unexpired claim grant present" → "claim id and a claim grant carrying a recorded (non-zero) expiry" | source read: `linuxV2RequireClaimOnlyRetainedRow` (`production_linux_v2_owner_cli_test.go:2396`) requires only a non-zero `LeaseExpiresAt` (`:2401`), while its accepted fixture `linuxV2ClaimOnlyRefusalJournal` (`:2288`) carries the expired `time.Unix(1, 0)` lease (`:2291`) and passes — the predicate never required an unexpired grant |
| 2 | `daemon/e2e/production_linux_v2_owner_cli_test.go` | `TestLinuxV2AlternateMountRefusalCauseRefusesAnUnreadableCapture` → `TestLinuxV2AlternateMountRefusalCaptureReadRefused` (`:2387`); its fatal text no longer claims a "cause check" | characterization: with `linuxV2RequireMountRefusalCause` forced to fail unconditionally in a scratch copy, the old name still passed (`PASS`, exit 0) while `TestLinuxV2AlternateMountRefusalCauseRequiresThePreflightIdentityRefusal` reds; the renamed row passes at the delivered bytes (exit 0) and reds when the capture-read refusal is removed (`return nil, nil` mutant: `FAIL` at `:2390`, exit 1) |
| 3 | `scripts/ci/linux-v2-native-acceptance.sh` | the order guard now refuses an empty argument list: `(( $# > 0 )) || fail "alternate-mount refusal order guard received no prior legs"` (`:3035`) | pre-fix bytes, zero prior event files: `GUARD-baseline-zero status=0` (silent pass, the reviewed defect); delivered bytes: zero prior legs → `status=1` with the new text, ten non-empty files → `status=0`, one empty file → `status=1` |

Guard mutant (one red row per new branch; scratch harness only, so an oracle gap for CI):

| mutant | run | result |
| --- | --- | --- |
| the `(( $# > 0 ))` line deleted | order guard, zero prior legs | `GUARD-mutant-zero status=0` — the zero-argument refusal row reds under the mutation; the restored delivered bytes are `status=1` on the same row |

Fix-round gates at the delivered bytes: `gofmt -l e2e/` exit 0; `go vet ./...` (Windows) exit 0;
`GOOS=linux go vet ./...` exit 0; `bash -n scripts/ci/linux-v2-native-acceptance.sh` exit 0; WSL
root `go test ./e2e/ -count=1` ok and the §6 four-row set again 26 `RUN` / 26 `PASS` / 0 `FAIL`;
the §3 guard rows (a)-(d) and the §5 guard-mutant row above; the §7 clone gates re-ran at these
bytes (exit 0 each). The §6 full `go test ./...` suites were not re-run: the fix round changes a
test name, one shell guard line and the records, so their apply-round rows stay the conductor's.

Record sanitation at these bytes: `model-routing-record-sanitize.py --check --root-only` exits 0
over `docs/TODO.md`, the plan and this record. On `docs/archive/CLOSED-TODO.md` it exits 1 over
the whole file: the branch appends to that file only (`git diff --numstat`: 24 added, 0 deleted)
and the whole-file check exits 1 identically at `7111828b`, so every flagged entry is pre-existing
text the brief forbids rewriting. The same check over each changed doc's added lines exits 0 for
all four docs, and no added line of the six changed files carries a host absolute path: the only
added lines naming a host-shaped prefix are the four `<HOST-PATH>`-tokenized ones.

Round 1 (§15) deletes finding 3's order guard and its mutant row: with the mount leg in its own
case set there is no order left to guard. Every other finding of this section stands.

Appendix — scratch pins and mutants verbatim with their full sha256; the harness commands were
`bash <scratch>/guard-harness.sh <script> 3034 3039 <label> [event-file…]` for the delivered block
and `3034 3038` for the pre-fix and mutant blocks:

- guard-mutant script = the delivered script minus exactly the line
  `  (( $# > 0 )) || fail "alternate-mount refusal order guard received no prior legs"`; it is
  byte-identical to the pre-fix script, sha256
  `2e088b8b4ff88f16dc999564a911aace86e622823e2cfd8664b5681c03a8e8e2`.
- cause-predicate inversion overlay, inserted as the first statement of
  `linuxV2RequireMountRefusalCause`:
  	return errors.New("mutant: alternate-mount cause check disabled")
  file sha256 `830f34dbaf853cd2f1e9d6997abc11de7dfab6689b29139acf57b4284401b62c`.
- capture-read refusal overlay: the
  `return nil, fmt.Errorf("read captured owner-witness stderr: %w", err)` arm of
  `linuxV2OwnerWitnessStderrTail` replaced by `return nil, nil`; file sha256
  `5ed0042071d294b429af0a9f718088ca63d351e95d7dd3749c115ac73dd1f4cf`.

## 15. Round 1 (2026-11-26, conductor ruling option (b)): the refusal leg's own case set

The round-1 ruling replaced the round-0 ordering with isolation. The mount-refusal leg runs in
its own case set `alternate_mount_refusal` (a fresh guest, install and StateDir per set); its
call and the runtime order guard are deleted from `shared`, which keeps base leg order. No
journal is deleted, no Store is reset, `linuxV2RequireSafelyReleasedPriorJournal` is unchanged,
and the leg's assertions are byte-equivalent (only its two comment blocks were rewritten; W4).
The PR body for these bytes is `findings/300-body.md`.

### 15.1 What changed

| file | change |
| --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `alternate_mount_refusal` joins the allowlist (`:33`) and dispatch (`:3213-3215`); the mount call and its order comment are deleted from `shared`; the guard is deleted from `run_alternate_mount_refusal_witness`, which keeps a 2-line why comment |
| `.github/workflows/linux-native-v2-acceptance.yml` | `alternate_mount_refusal` joins `case_sets` (`:130`) |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | the mount witness moves from `shared` to `alternate_mount_refusal` (`:318`); the set joins `PI_DURABILITY_CASE_SETS` (`:341`) |
| `scripts/ci/test-linux-v2-evidence.py` | the case-set pin additionally checks the exporter catalog, allowlist disjointness, the dispatch arms and the mount-call/arm isolation, with two committed red mutations (`:435-490`, `:2014-2037`) |
| `scripts/ci/test-ci-structure-extraction.py` | new pin: the workflow `case_sets` array equals the script allowlist (`:796-822`, `:1093-1097`), with a committed red mutation (`:1148-1154`) |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | the `:1614` comment is why-only and the capture-read comment drops the ordering rationale; no assertion changed |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the plan, this record | corrected to the option-(b) design, including the round-0 "opencode leg is harmless" claim (§3, §12, plan §4, TODO, PR body) |

### 15.2 Why the guest cannot isolate one leg's Store (the ruling's evidence)

Every leg's config `state_dir` must equal the root-installed `OwnerConfig.StateDir`: the e2e
fixture fatals otherwise (`production_linux_v2_native_witness_test.go:1953-1956`) and so does the
production witness (`linux_owner_witness_linux.go:1217`, object-identity bound by
`linuxpreflight.VerifyOwnerStateDirBinding`, pinned by
`TestLinuxV2NativeWitnessStateDirExceptionIsExactAndInstalled`). The guest installs exactly one
such StateDir (`linux-v2-native-acceptance.sh:505,567-586`; the configs are generated with it at
`:1339-1356`), and each case set is a fresh guest (`:79`, `:963`; workflow `:119-140`), so a
separate case set is the only in-scope isolation. The new set runs one leg; its prerequisites
(negative-root bundle, barrier, alternate bind mount, owner install, in-leg Control claim
`stage_replay_alternate_mount` at `:3033-3037`, Control fixture case
`scripts/ci/provision-linux-v2-control-claims.exs:80`) are provisioned unconditionally, exactly
as for `migration`/`stale_leaf`. The workflow job ceiling stays `timeout-minutes: 180`; no
in-repo derivation of a timeout from the case-set list exists (grep), and the new set adds one
single-leg guest run of the same footprint as those sets.

### 15.3 Pins and mutants (one red proof per pin)

| pin | red proof |
| --- | --- |
| committed: workflow `case_sets` vs script allowlist (`test-ci-structure-extraction.py`) | `-k mutation_dropping_a_case_set` passes; the mutation drops the set from the workflow and the pin raises |
| committed: allowlist vs exporter catalog + pi set + disjointness + dispatch arms + mount arm (`test-linux-v2-evidence.py`) | `-k mutations_dropping_the_mount_case_set` passes (drop from the allowlist; rejoin the shared arm) |
| scratch: re-add `alternate-mount-refusal.json` to `shared`'s exporter allowlist | pin reds with `case-set witness allowlists are not disjoint` |
| scratch: delete the `alternate_mount_refusal)` dispatch arm | pin reds with `the dispatch arms and the case-set allowlist diverged` and `the mount-refusal leg must run only in its own case set` |
| scratch: `replica-export.py` export replica (WSL) | green export of `alternate-mount-refusal.json`; missing witness → `pi durability witness is unavailable: alternate-mount-refusal.json`; witness without the pi binding → `does not bind the staged pi executable`; `shared` with only the mount witness still demands its own first missing file |

### 15.4 Gates at the round-1 delivered bytes

Rows measured at the round-1 bytes (`8a1a482c`), as titled. The `origin/main` merge `ff0975a7`
(#298) re-based only `scripts/ci/linux-v2-native-acceptance.sh` among the run inputs (+4/−4,
OpenCode 2.4.1 staging), so the script-dependent rows were re-measured at the head `66b27e63`
in the records round (§16): `bash -n` 0; `actionlint` 0; `test-linux-v2-evidence.py` (WSL) 56
tests OK; `test-ci-structure-extraction.py` (Windows and WSL) 72 tests OK (WSL: 3 pwsh skips);
`test-ci-changed-path-classifier.py` (Windows and WSL) 37 tests OK. The clone-gate rows
(comment-cap, jscpd, check-record-cleanliness, sanitize) re-ran on a scratch-clone commit of
the final records bytes (§16). The Go rows stay attributed to `8a1a482c`: the range's only Go
file is byte-identical at the head (§15.5), and the rest of the module surface is the merge's,
exercised by the PR's `daemon` CI job.

| gate | command | exit | result |
| --- | --- | --- | --- |
| Windows full module | `go test ./... -count=1` from `daemon/` (go1.27.4 windows/amd64) | 0 | 27 ok / 0 FAIL / 3 no test files; 207.9s wall |
| WSL full module, root | `go test ./... -count=1` (go1.27.0 linux/amd64, uid 0) | 1 | 30 ok incl. `daemon/e2e`; only `internal/platform` fails, 13 `wire harness requires an unprivileged A process: uid=0` rows — the same pre-existing class §6 recorded, in a package the range does not touch |
| WSL full module, unprivileged | `wsl -d Ubuntu-24.04 -u accept` with the §6 env pins | 0 | 31 ok / 0 FAIL / 3 no test files |
| vet | `go vet ./...` (Windows) | 0 | — |
| vet | `GOOS=linux go vet ./...` (WSL root) | 0 | — |
| formatting | `gofmt -l e2e/` | 0 | no output |
| script syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | — |
| actionlint | `actionlint .github/workflows/linux-native-v2-acceptance.yml` | 0 | — |
| python CI suite | `python scripts/ci/test-linux-v2-evidence.py` (WSL) | 0 | 56 tests OK |
| python CI suite | `python scripts/ci/test-ci-structure-extraction.py` (Windows and WSL) | 0 | 72 tests OK (WSL: 3 pwsh skips) |
| python CI suite | `python scripts/ci/test-ci-changed-path-classifier.py` (Windows and WSL) | 0 | 37 tests OK |
| new Go rows | the four-row e2e set (WSL root) | 0 | 26 RUN / 26 PASS / 0 FAIL (4 top-level, 22 subtests) |
| duplication | `python scripts/ci/jscpd-changed.py --base 7111828b --head HEAD` (scratch clone with the existing jscpd install junctioned) | 0 | 10 changed files, 1775 scanned sources, 1254 corpus clones, none new |
| comment cap | `python <scratch>/comment-cap.py <clone> 7111828b` | 0 | no added comment block over 2 lines |
| record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <clone> --base 7111828b --head HEAD` | 0 | 1 changed record passes |
| record sanitize | `model-routing-record-sanitize.py --check --root-only` | 0 / 1 / 0 | TODO, plan and record whole-file 0; CLOSED-TODO whole-file 1 (pre-existing, identical at `7111828b`) and its branch-added lines 0 |

### 15.5 Delivered-byte digests (the guest/CI run inputs)

Re-measured at the head `66b27e63` (X1, §16); the round-1 column is the `8a1a482c` blob the
table previously carried:

| file | sha256 at the head `66b27e63` | round-1 `8a1a482c` |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` | identical |
| `scripts/ci/linux-v2-native-acceptance.sh` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` | `920a0a3ea715d4951d8f96de9336d4152528d9a3a0e044a7c2125bc164c6d0c9` |
| `.github/workflows/linux-native-v2-acceptance.yml` | `2d5500ce4b411e979cf974c19028514dceb5a729b3c73e8971add50f55bc27d8` | identical |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `bf53e78fa7fee74d98988c1a8c13cc910930aa08081b1adadd839973d09dd5c1` | identical |

Only the script differs: the `origin/main` merge `ff0975a7` (#298) brought the OpenCode 2.4.1
staging changes into it (+4/−4). The two CI test files are harness-side and the four records
are not run inputs; the branch's own range is 10 files, +1130/−37 (`origin/main...HEAD`,
`ff0975a7...66b27e63`). `git diff --check` is clean and no added line carries a host absolute
path. The Bind 2 run-input equality table below is authoritative for the run.

### 15.6 Acceptance carried to the conductor's Bind (amended)

Met by Bind 2 (§Bind 2 below): the Bind ran `shared` and `alternate_mount_refusal` once each on
the head; `shared` keeps the `:91` stop gone (its next stop, the pi provider-scoped-admission
residual, recorded verbatim), and the new set passed `TestProductionLinuxV2AlternateCgroupMountRefusal`,
exported `alternate-mount-refusal.json`, and completed its dispatch, so the
`stop_readback_reader` row is measured (clean).

## Bind 2 — binding run on the merge head 66b27e63 (shared + alternate_mount_refusal)

The conductor's Bind 2 ran `shared` and `alternate_mount_refusal` once each on the PR head
`66b27e630b904f41fc5a77c5f9b512f8962c0571` plus the two scratch-only files (run head
`11f5a9413639a929c64102a0ca35625a99c8cbed`): exactly one run, watched once; conclusion
`failure` (`shared` only). Run id `45778210044`, attempt 1, created 2026-11-26T07:49:37Z,
updated 07:56:13Z; `total_count=1` at every read of the runs list, including after the scratch
branch deletion; the scratch branch was deleted and the temporary worktree removed. Full report:
`findings/300-binding-run-2.md`.

- `shared` keeps the `:91` stop gone: 0 occurrences of `pre-existing RunJournal`,
  `is not physically released`, `may still own or require recovery` and of every earlier stop
  class (including bare `/proc/`); all 21 legs that ran verified green; the set's first failing
  line is the pre-existing, documented `pi_provider_action` provider-scoped-admission residual —
  `reserve owner witness clone before source Git effects: Linux clone intent does not match the
  current admitted Subject` (`production_linux_v2_native_witness_test.go:91`) — so it exits 1
  (`Case set shared exited 1`; `Failed case sets: shared`). `stop_readback_reader` did not run for
  `shared` (abort inside the `esac` at the pi leg `:3160`, before its single call site `:3226`),
  so its Result row stays unmeasured, not failed; the start proof passed (`MainPID=7252`).
- `alternate_mount_refusal` (its own guest/Store) verified
  `TestProductionLinuxV2AlternateCgroupMountRefusal: run=1 pass=1 skip=0`, exported
  `alternate_mount_refusal/alternate-mount-refusal.json` (2117 B, sha256
  `6ec6e5636a7353b3475984e92ec95210f5ad75957b57ba0063b308ef16851e6f`; its manifest is
  `status: passed` with `missing_allowlisted_files` empty), ran the reader stop proof cleanly
  (`readback-reader: stop signal requests=0`; 0 `did not stop cleanly`) and exited 0
  (`Case set alternate_mount_refusal exited 0`).

Artifacts: `linux-v2-native-evidence-45778210044-1` (id `15096983460`, 3295 B, sha256
`2cc49906b683fc4cf4b273075e1da9d9777a43d5b63121a5916b55fa5e845277`) and
`linux-v2-host-prerequisite-45778210044-1` (id `15097466296`, 676 B, sha256
`4e96cb3c01601eda20dc4796fffb0cc62e0c670a49564a2f9846945695653664`); both download-verified. The
shared export failed its durability gate as before (`pi durability witness is unavailable:
ack-loss-replay-checkout_ready.json`), so its members are the partial allowlist
(`pi-native-provider-action.json`, `unprovisioned-owner-refusal.json`).

### Run-input sha256 equality (run head `11f5a941` vs PR head `66b27e63`)

`git diff --name-status 66b27e63 11f5a941` = exactly the two scratch-only additions, so every
PR-head file executed by the run is byte-identical; the table pins the principal executed
inputs. The merge head's `scripts/ci/linux-v2-native-acceptance.sh` digest is `70163ae1…` — it
differs from §15.5's pre-merge digest (`920a0a3e…` at `8a1a482c`) because the `main` merge (#298)
brought the OpenCode 2.4.1 staging changes into the script; the run executed the merge-head
bytes. The other scripts the acceptance script invokes were measured equal too.

| File | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` | yes |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `bf53e78fa7fee74d98988c1a8c13cc910930aa08081b1adadd839973d09dd5c1` | `bf53e78fa7fee74d98988c1a8c13cc910930aa08081b1adadd839973d09dd5c1` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `abaa740e1ad0cbbd85899d9d78590e327f531767cb3ddf4134468915f9b3e63a` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

## 16. Round 2 (2026-11-26, conductor ruling X1-X5): records-only re-measure

No code, test or script byte changed in this round; every executed input of Bind 2 (head
`66b27e63`) stays byte-identical, so the binding run stands. Per item:

- X1: §15.5's four digests re-measured at the head — only the script differs from round 1
  (`70163ae1…` vs `920a0a3e…`, the merge's OpenCode 2.4.1 re-base); §15.4 carries the head
  re-run clause for the script-dependent rows; the PR body (`findings/300-body.md`) carries the
  same digests.
- X2: §10's script anchor row replaced with the head anchors `:33,3031-3032,3213-3215`.
- X3: the order-sensitive, duplicated allowlist detection (`test-linux-v2-evidence.py:469`,
  `test-ci-structure-extraction.py:814`) and `parse_case_arms`'s arm-id whitelist
  (`test-linux-v2-evidence.py:444`) are recorded as a `docs/TODO.md` residual; no code change,
  to keep the Bind binding.
- X4: Bind 2 (§Bind 2 above) is the binding row; the `84dc22c3` run is labelled diagnostic; the
  `CLOSED-TODO` closure cites run `45778210044`.
- X5: the readback-reader residual in `docs/TODO.md` now reads measured clean in run
  `45778210044` (`alternate_mount_refusal`), with `shared` still aborting before its call site.

Gates at the head `66b27e63` (all exit 0): `bash -n`; `actionlint`; `test-linux-v2-evidence.py`
(WSL) 56 tests OK; `test-ci-structure-extraction.py` (Windows and WSL) 72 tests OK (WSL: 3 pwsh
skips); `test-ci-changed-path-classifier.py` (Windows and WSL) 37 tests OK. Scratch-clone
commit gates (a commit of the final records bytes on `66b27e63`): comment-cap 0; jscpd 0 — 10
changed files, 1780 scanned sources, 1245 corpus clones, 0 tolerated among the added lines,
none new; check-record-cleanliness `--base ff0975a7` 1 changed record passes (`--base
7111828b`: 2 pass); sanitize `--check --root-only` whole-file 0 for `docs/TODO.md`, the plan
and this record, 1 for `docs/archive/CLOSED-TODO.md` (identical at `7111828b`), and 0 over
every changed doc's added lines.

Anchor sweep at the head: every anchor cited above and in the plan re-resolves — the script
(`:33,3031-3032,3213-3215`; `:79`, `:505`, `:963`, `:1339-1356`; the stop block `:2202-2226`;
the pi leg `:3160`; the stop call `:3226`), the workflow (`:119-140`, `:130`), the exporter
(`:318`, `:341`), the two CI test files (`:435-490`, `:2014-2037`; `:796-822`, `:1093-1097`,
`:1148-1154`), and the e2e/production rows of §10 (§15.5 proves the Go file's bytes).
