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
(`docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md:82`); that rule is now carried by an
explicit pin: the leg reads its own captured App stderr
(`production_linux_v2_owner_cli_test.go:4857`) and requires the mount-identity phrase on a line
that also carries the witness marker, while forbidding the foreign-ownership phrase
(`linuxV2RequireMountRefusalCause`, `:2314`), and it requires its own retained row to stay
claim-only (`linuxV2RequireClaimOnlyRetainedRow`, `:2396`; call site `:4875`). The leg also guards
its order at runtime: it refuses an empty argument list and fails unless all ten prior event files
are already non-empty, and the call site (`:3187-3190`) passes those ten files to the guard as its
arguments (`for prior in "$@"`, `:3034-3039`), so a future reorder cannot silently reintroduce the
wall.

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
row until teardown. This goal orders around it instead of touching durable state; a product-side
retirement for a claim that provably never started native work is a separate, unowned decision
(contract class DECISION_NEEDED).

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

Delivered files and their sha256 at the fix-round delivered bytes (measured over the delivered
bytes, the same bytes the §7 clone gates ran on; `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`
and this record are deliberately not hashed, and
`daemon/e2e/production_linux_v2_native_witness_test.go` is unchanged by design):

| file | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `abdb383be31d70cd5b591cc8f927c2c2ec0e4fab196041b0a31ab91bd5e37012` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `3254deef0bfdc16c741542623c293012aa1e1c265e6f881d74792121280f7bcb` |
| `docs/plans/2026-11-26-rf-acc-shared-journal-release-plan.md` | `5dadbcbaaacab9e09254a44bbfb9392d5cca08f2f1b956cf63067d5769295237` |

Every anchor cited above and in the plan, measured with `grep -n`/`sed -n` on the delivered
bytes (the Bind log's retained copy still hashes to `bf00da4f…` at
`scratch/bind296r2/run-45737002125.log` under the GDD state root, and the probe line cited in §1
is `native/run-45384436642.log:1227`):

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
| `scripts/ci/linux-v2-native-acceptance.sh:3034-3039,3167-3184,3187-3190,3191` | guard block over its arguments plus the empty-argument refusal, ten prior legs, the mount leg with its ten arguments, opencode leg |
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
  loud and diagnosable rather than silent — but the Bind is what proves the guest text. The pin
  also requires the witness marker and the mount phrase on the same line, which matches the App's
  single print (`cmd/symmetry-daemon/main.go:35` prints one `%v` line through the single-line
  wrappers at `linux_owner_witness_linux.go:3258` and `native_run.go:199`); a guest that wrapped
  them onto separate lines would red the leg rather than silently pass it.
- The opencode ordering coupling of §3 is a residual in `docs/TODO.md`; if that leg's
  pinned-version gate is fixed without moving the mount leg, the next `shared` run stops at the
  opencode leg for this same wall.
- The unmeasured `stop_readback_reader` row stays unmeasured until a `shared` run passes the
  opencode leg (`docs/TODO.md`, readback residual).
- This stage pushes nothing and commits nothing: the delivered bytes are the worktree files, and
  no PR body exists to rewrite yet.

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
