# rf-acc-owner-loss-semantics — evidence record

Goal: `rf-acc-owner-loss-semantics` (contract `goal-rf-acc-owner-loss-semantics.md`, GDD state root).
Branch `refactor/rf-acc-owner-loss-semantics`, base revision `6b2d4f8f` (#285 supplies this goal's
Q2(a) gate, #284 the refusal vocabulary, #280 the containment-witness legs). Conductor rulings:
Q1 = (a) — the containment half stays as implemented; Q2 = B1 (d1b) with (c2), (d2) and (b)
rejected; Q3 = (a); Q4 = (a). Consults: `findings/rf-acc-owner-loss-consult-{sol,ds}.md`,
`findings/rf-acc-owner-loss-d1d2-{sol,ds}.md`, `findings/rf-acc-owner-loss-d1b-{sol,ds}.md`. Plan:
`docs/plans/2026-11-25-rf-acc-owner-loss-semantics-plan.md`.

Line numbers below are at the fix-round delivered bytes (§7.3) unless a section says otherwise; the
run tables name their own head revisions. Records cite final bytes by sha256 only (never
commit/head provenance claims); docs bytes are never sha256'd.
Apply round 2026-11-25 (simplify findings S1-S4, PD-A, PD-B; S4's B-target element is kept
unapplied — §7.5): every line anchor below is re-measured at the apply-round bytes, and §7.5 pins
the eight files again.
Fix round 2026-11-25 (review findings F1-F8, §11): F1/F7/F8 change the two e2e test files — the
retention helper now delegates to the exported state predicate, the `afterStageProcessKilled` struct
comment block is compressed to two lines, and the crash-replay runner asserts the helper's
owner-loss exit for every caller; F2-F6 are record fixes. Every line anchor is re-measured at the
fix-round bytes, and §7.3 re-pins the eight files.

## 1. Clause census at the delivered bytes (re-located by content)

| Contract clause | Delivered bytes | Disposition |
| --- | --- | --- |
| Change 1 consult + guard table | `findings/rf-acc-owner-loss-*`; §3a below | done before dispatch; guard table completed here |
| Change 2/3/4 ruled as d1b | §2.1-§2.6 | implemented |
| Change 5 observability | #285's refusal vocabulary unchanged; the d1b refusal is replaced by the named retention outcome (§2.3) | satisfied |
| Change 6 records | this record, the plan, `docs/runbooks/daemon-process-stop.md:99-155`, `docs/TODO.md:1987-2003` | implemented |
| Carried #284 items (Sec F1, DS F5, Sol P3) | §2.7 | implemented |
| Gates: four `stale_journal_*` sets on a hosted runner | §7 | run, see §7 |
| Owned set (amended by the d1b ruling) | `daemon/internal/app/linux_owner_witness_linux.go`, `daemon/internal/state/linux_clone_validation.go`, `daemon/internal/state/linux_cgroup_journal.go`, their tests, `daemon/e2e/production_linux_v2_{native_witness,owner_cli,owner_identity}_test.go`, the runbook, TODO | matches; nothing outside it edited |

## 2. Changes

1. **d1b shape predicate** (state, new): `linuxV2ReleasedCloneRetainedAfterRelease`
   `daemon/internal/state/linux_clone_validation.go:53-65`, called as an added arm of
   `linuxV2ReleasedCloneStateSafe` (`:44-46`). Every term is required — valid intent
   (`ValidateForJournal`), seed receipt, no checkout/promotion/abort/cleanup field, no reuse grant,
   no native-usage recovery, `WorkspacePath == intent.Target`, `WorkspaceBindingKey ==
   intent.BindingKey`, `WorkspaceRecoveryRequired` — so any other shape keeps blocking foreign
   admission. The caller (`linuxV2ForeignJournalIsSafelyRetired`, release branch) already supplies
   the release-proof, leaf-disposition, `ContainmentCgroup == nil` and no-live-authority terms.
2. **C3 cgroup leaf closure** (state, new): `linuxV2RetainedReleaseNamesLeaf`
   `daemon/internal/state/linux_cgroup_journal.go:423-430`, called from the admission scan's
   `reservation != nil` branch (`:394-402`) for exactly the d1b shape; the existing
   reservation-leaf refusal is unchanged and the seed/target alias block above it still runs
   (`:384-393`, no early `continue`).
3. **Witness retention outcome** (app): `linuxV2NativeWitnessClone.Retention`
   `daemon/internal/app/linux_owner_witness_linux.go:209`; the third branch at `:521-535` always
   calls `cleanupRecoveredWorkspace` and classifies its refusal only when the reloaded row is
   byte-identical and is the retained pre-checkout shape; classifier `:3082-3097` (apply round:
   `linuxV2NativeWitnessCloneRetainedAfterRelease` delegates to item 1's exported
   `state.LinuxV2ReleasedCloneRetainedAfterRelease` and adds the physical-release term; the
   dedupe round in §7.6 exported that predicate); closed vocabulary + summary validation
   `:3099-3112` and `:745-747`. No journal field is added: the
   durable retention reason is the row's own shape.
4. **Q3(a) pin discipline** (e2e): the crash-replay runner keeps pinning the resident anchor live
   after the kill (`daemon/e2e/production_linux_v2_owner_cli_test.go:1953-1957`) and requires the
   helper's owner-loss exit itself (`:1958-1964`; fix round: the assertion now covers the three
   `stale_journal_*` variants and every other crash-replay caller, not only the fresh-request leg);
   it then hands both PIDFDs to the leg (`:1920`, `:1965-1967`); the `afterStageProcessKilled`
   contract carries both PIDFDs (`production_linux_v2_native_witness_test.go:1071-1073`); the
   fresh-request leg pins the anchor and asserts the helper's owner-loss exit before and after the
   denial (`:420-427`, `:488-491`), waiting 90 s — above the helper's own 30 s
   `linuxOwnerHelperV2StopTimeout` (`internal/platform/helper_v2_protocol.go:33`) so the fixture
   cannot race the fail-safe (§7.2).
5. **Retention assertions** (e2e, new): `linuxV2RequireRetainedPreCheckoutClone`
   `production_linux_v2_owner_cli_test.go:2282-2292` (journal; fix round: it delegates to the
   exported `state.LinuxV2ReleasedCloneRetainedAfterRelease`, so the three later state terms cannot
   drift out of the assertion, and keeps the validated-seed check as its one extra term, pinned by
   its own row),
   `linuxV2RequireRetainedCloneArtifacts` `:2294-2314` (disk, apply round) and
   `linuxV2RequireRetainedCloneOutcome` `:2316-2325` (response), wired into the released
   stage-replay arm (`:1246-1248`) and the crash-replay runner for every released replay whose
   clone never checked out (`:1990-1997`).
6. **Admitted fresh leg** (e2e, new): after the setup recovery,
   `production_linux_v2_native_witness_test.go:511-583` asserts the retained setup row, then the same
   fresh request the unresolved row denied is admitted, reaches its own `handoff_committed` boundary,
   is killed and recovered to the same released retained shape, with the setup row byte-identical
   throughout. The aliasing denial is pinned at the state layer (§5) because a request cannot mint
   aliasing clone paths (they are RunKey-derived).
   The fixture builder's shared-State cleanup comparator (`production_linux_v2_native_witness_test.go:1917-1925`)
   takes the admitted run's key from a new `allowedNewRunKeys` field the leg fills only after it has
   proven that journal's exact boundary (`:546`), so exactly one additional new journal is
   tolerated while every previously retained row must still be byte-identical.
7. **Carried #284 items** (e2e): `linuxV2JournalBindsOwnerSlot` now projects PID and creation
   identity (`owner_cli_test.go:1639-1707`) and `linuxV2RequireSameOwnerIdentities` binds every
   journal-bound slot to that projection (`:1742-1744`), so a consistently swapped marker/response
   set reddens; new rows at `owner_identity_test.go:110-127` (swap, equal-PID/different-identity) and
   the phase-branch table at `:152-237`.
8. **Records**: runbook section `docs/runbooks/daemon-process-stop.md:99-155` (retention semantics
   plus the read-only jq locator), TODO item `docs/TODO.md:1987-2003` (authorized disposition
   command), plan `docs/plans/2026-11-25-rf-acc-owner-loss-semantics-plan.md`.
9. **Apply round** (simplify findings S1-S4, PD-A; 2026-11-25): the two fresh-request legs share
   `linuxV2FreshRequestFixture` (`production_linux_v2_native_witness_test.go:586-618`); four new
   comment blocks compressed to the contract's two-line policy (`linux_clone_validation.go:53-54`,
   `linux_cgroup_journal.go:423-424`, `linux_cgroup_v2_test.go:971-972`,
   `production_linux_v2_native_witness_test.go:418-419`); `preCleanup` is captured above the
   `cleanupRecoveredWorkspace` call (`linux_owner_witness_linux.go:523-524`); the app retention
   classifier gained the five state-arm terms (`intent.ValidateForJournal() == nil`, no reuse
   grant, `!NativeUsageRecoveryRequired`, `WorkspaceBindingKey == intent.BindingKey`,
   `WorkspaceRecoveryRequired`) with five pinning rows
   (`linux_owner_witness_linux_test.go:1667-1725`); the retained-clone disk assertions (item 5)
   were added for the durable A-side artifacts only — the A seed bundle and the per-run base ref.
   The B target path is not asserted: the diagnostic run `45619184193` shows it ENOENT for this
   crash point on all four sets (`target=ENOENT len=111`, `seed=file len=117`,
   `baseref=present len=98`), because the clone bootstrap creates the target after the
   `handoff_committed` handoff the four sets crash at. Evidence: §6 M13/M14, §7.5.
   A dedupe round driven by the repository's `jscpd` gate (§7.6) then exported item 1's state
   predicate so the app classifier delegates to it, and hoisted the witness-command preamble the
   ACK-loss and helper response-loss tests shared; no term and no row changed, and M13/M14 were
   re-pinned in the dedupe round.
10. **Fix round** (review findings F1-F8; 2026-11-25): items 4 and 5 above now describe the
   fix-round bytes of the two e2e files; §11 carries the per-finding dispositions, the two scratch
   probes, the F1 mutant row and the re-measured gates.

## 3. Censuses

### 3a. Guard table (contract Change 1(iii)) with each ruled disposition

| Guard (contract static trace) | Delivered bytes | Disposition |
| --- | --- | --- |
| 1. Exact-RunKey select + clone-intent identity | `linux_owner_witness_linux.go:2763-2844` (`recoverLinuxV2NativeWitnessRunKey`, `selectLinuxV2NativeWitnessJournal`, `verifyLinuxV2NativeWitnessSelectedJournal`) | verified, no edit: satisfied by the same request/Subject |
| 2. `recoverLinuxV2OwnerJournal` anchor/lookup/helper-death guards | `linux_owner_recovery_linux.go:108-300` | verified, no edit: the dead-helper path (`helper.Control == nil` → `WaitReapRun` → `bindRecoveredV2AnchorGate`, `:1244-1257`) consumes the anchor-authenticated owner-loss stop evidence; no guard requires a live helper once `LookupHelper` reports `helper_exited` (Q1 consult, §1.2) |
| 3. Fresh-request pin | `linuxV2OpenStageOwnerPIDFD` `owner_cli_test.go:1798-1807` | converted: only the resident anchor is pinned live; the helper's owner-loss exit is asserted through its PIDFD |
| 4. Fresh-request denial | `native_run.go:204-207`, `linux_cgroup_journal.go:454-516` | verified, no edit: the denial is durable-only and survives owner loss; the old liveness pins were preconditions, not the assertion |
| 5. Recovery response read + exit status | `owner_cli_test.go:177-193,222-261` | satisfied for the ruled `released` outcome; the refusal path remains for every other shape |
| 6. Final journal check | `linuxV2JournalHasPhysicalRelease` `native_witness_test.go:2531-2537` (call `:500`) | verified, no edit; the retained clone is additionally asserted (§2.5) |

### 3b. Live-pin census (every site that pins an owner process live after an owner kill)

Command: `grep -rn "linuxV2RequireLivePIDFD\|linuxV2OpenStageOwnerPIDFD\|linuxV2KillOwnerWitnessCommand" daemon/e2e/*.go`
(29 matching lines on the final bytes). `linuxV2RequireLivePIDFD` alone is 21 lines — definition
`native_witness_test.go:3324` plus 20 call sites (`native_witness_test.go` 10, `owner_cli_test.go` 9,
`helper_exit_witness_test.go` 1); the three converted rows below leave 17 remaining, one of which is
named explicitly.

| Site | Disposition |
| --- | --- |
| `native_witness_test.go:420`, `:488` (fresh-request leg) | converted: anchor live only; the helper's owner-loss exit is asserted (`:425-427`, a 90 s bound above the helper's own 30 s stop timeout, and `:491`) |
| `owner_cli_test.go:1954` (crash-replay runner) | converted: anchor live only; the runner now asserts the helper's exit itself (`:1958-1964`, fix round — before, only the fresh-request leg asserted it through `native_witness_test.go:426`, so the three `stale_journal_*` variant legs had no helper-exit assertion) |
| `native_witness_test.go:2472` | exempt, named explicitly: pins the A helper live immediately after the production daemon SIGKILL; the daemon-crash witness needs the helper to outlive the daemon to run the fail-safe stop this same check guards (`linuxV2WaitPIDFDExited(live.pidfd, 30s)` and the detached child), so it is not an owner-kill helper pin |
| `linuxV2RequireLivePIDFD` other 16 sites | exempt: pre-kill liveness, detached-child/replacement/watcher identity, or the target stop the recovery itself must prove; none of them pins a helper after an owner kill |
| `linuxV2OpenStageOwnerPIDFD` (definition `owner_cli_test.go:1798`; call sites `:1913`, `:1920`) | exempt: the pre-kill stage-entry pin (only the helper one is now consumed by the exit assertion) |
| `linuxV2KillOwnerWitnessCommand` (definition `owner_cli_test.go:202`; call sites `:1950`, `:4234`, `:4377`, `native_witness_test.go:547`) | exempt: the kill and the two unrelated runners' own kills |

### 3c. Assertion census

The only removed assertion is the helper-liveness pin the ruling names (pre-change
`owner_cli_test.go:1945-1949`, `native_witness_test.go:443-451`, `:520-527`); it is replaced by the
helper's explicit owner-loss exit. `requireLinuxV2RecoveryExitStatus`, the denial assertions and the
`linuxV2JournalHasPhysicalRelease` assertions are unchanged. Nothing was weakened: the added
assertions are §2.5-§2.7 plus the 9 unit rows, all additive. The apply round adds five classifier
rows and one disk subtest with five checks (§2.9); the dedupe round (§7.6) removes no assertion —
the classifier keeps every term by delegating to the exported state predicate, and the hoisted
witness-command helper is the same five statements the two tests ran inline.
The fix round (§11) removes no assertion either: the retention helper replaces its hand-copied term
list with the production predicate — adding `LinuxCloneReuseGrant == nil`,
`!NativeUsageRecoveryRequired` and `WorkspaceBindingKey == intent.BindingKey`, and keeping the
validated-seed check as its one extra term with a row of its own — and the crash-replay runner adds
the helper-exit assertion every caller now inherits (§2.4).

## 4. Local gates

| Gate | Command (working directory `daemon/`) | Exit | Result |
| --- | --- | --- | --- |
| formatting | `gofmt -l .` (re-run at the implement round as `accept`) | 0 | empty list |
| vet | `GOOS=linux go vet ./...` (node 1.27.0, WSL Ubuntu-24.04; re-run at the implement round as `accept`) | 0 | no findings |
| focused suites, implement round | `go test -count=1 -timeout 3600s ./internal/app/ ./internal/platform/ ./internal/state/` as user `accept` with `umask 022` | 0 | `ok app 215.391s`, `ok platform 44.544s`, `ok state 24.133s` (state re-run after the last test-row edit) |
| e2e unit pins, implement round | `go test -count=1 -timeout 1800s ./e2e/` as `accept`, `umask 022` (re-run after the wait-bound fix, at the implement round) | 0 | `ok e2e 0.738s` |
| whitespace | `git diff --check` | 0 | clean |

Apply round (2026-11-25, apply-round bytes, as `accept` with `umask 022`): `gofmt -l .` empty;
`GOOS=linux go vet ./...` clean; focused suites `ok app 214.610s`, `ok platform 36.605s`,
`ok state 12.723s`; `go test -count=1 -timeout 1800s ./e2e/` `ok e2e 1.478s`; `git diff --check`
clean under native git (WSL git cannot resolve this worktree's gitdir pointer, so the whitespace
gate ran on the Windows checkout of the same bytes).
Dedupe round (2026-11-25, dedupe-round bytes, as `accept` with `umask 022`): `gofmt -l .` empty;
`GOOS=linux go vet ./...` clean; focused suites `ok app 161.697s`, `ok platform 37.316s`,
`ok state 13.092s`; `go test -count=1 -timeout 1800s ./e2e/` `ok e2e 0.879s`; `git diff --check`
clean under native git. The two re-pinned mutants (M13b, M14b) ran as focused `go test -overlay`
invocations on the same bytes (§10).

Fix round (2026-11-25, fix-round bytes, as `accept` with `umask 022`): `gofmt -l .` empty (exit 0);
`GOOS=linux go vet ./...` clean (exit 0); one combined suite run
`go test -count=1 -timeout 3600s ./internal/app/ ./internal/platform/ ./internal/state/ ./e2e/`
exited 0 — `ok app 216.476s`, `ok platform 46.931s`, `ok state 20.608s`, `ok e2e 1.548s`; the
package's rows under repeats, `GOMAXPROCS=8 go test -race -count=200 -run
'TestLinuxV2RetainedPreCheckoutClonePredicates|TestLinuxV2JournalBindsOwnerSlotProjectsEveryPhase|TestLinuxV2RequireSameOwnerIdentities'
./e2e/`, exited 0 (`ok e2e 43.015s`); `git diff --check` clean under native git. The fix round's two
scratch probes ran as focused `go test` invocations (§11), and the recipe-level gates (record
cleanliness, citation existence, host-path scan, `jscpd`) ran on a scratch commit of the fix-round
tree (§11.2).
Recipe gates on a scratch commit of these bytes (built over HEAD with a temporary index, no ref or
HEAD moved; range `691eb424…` to the scratch commit): `jscpd-changed.py --repo . --base 691eb424…`
exit 0 (`13 changed file(s), 1743 scanned source(s), 1261 clone(s) in the corpus, 0 tolerated among
the added lines, none new`, with a notice that `docs/archive/CLOSED-TODO.md`'s added lines are
outside jscpd's scan scope); `check-record-cleanliness.py` exit 0 (`1 changed record(s) pass the
root-only check`); `check-citation-existence.py` exit 0 (`2 changed document(s) pass the path and
symbol check (633 path citation(s))`, plus three pre-existing ignore-rule notes for CLOSED-TODO
lines 737, 752 and 2512); `model-routing-record-sanitize.py --check --root-only` exit 0 on this
record, the plan, the runbook and TODO.md, and exit 1 on `docs/archive/CLOSED-TODO.md` where the
merge-base bytes hold the flagged fragments (18 rendered lines, none added by this branch; the
branch's added doc lines pass the same check, exit 0). Host-path scan over the branch diff for the
user-profile prefix: no match.

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
| `linuxV2NativeWitnessCloneRetention*` (app, 2 positive checks + 12 mutation + 7 summary rows; the apply round added the five divergence rows) | `go test -count=1 -run 'TestLinuxV2NativeWitnessCloneRetention' ./internal/app/` | 0 | `ok app` (0.42s) |
| `linuxV2RequireRetainedPreCheckoutClone` + `linuxV2RequireRetainedCloneArtifacts` + `linuxV2RequireRetainedCloneOutcome` (e2e, 11+4 negative, 2 non-applicable rows, plus the apply round's disk subtest: positive + no-intent + 2 removal rows; the fix round added the three delegation rows and the seed-validation row) | `go test -count=1 -run 'TestLinuxV2RetainedPreCheckoutClonePredicates' ./e2e/` | 0 | `ok e2e` (0.38s) |
| `linuxV2JournalBindsOwnerSlot` phases + strengthened comparator | `go test -count=1 -run 'TestLinuxV2JournalBindsOwnerSlotProjectsEveryPhase\|TestLinuxV2RequireSameOwnerIdentities' ./e2e/` | 0 | `ok e2e` |
| Existing pins the change must not disturb | `go test -count=1 -run 'TestLinuxCloneAbortCannotUseNormalReleaseAsNoTargetProof\|TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners\|TestLinuxV2AdmissionRejectsTerminalUnremovedAndUnprovenForeignOwners\|TestLinuxV2AdmissionFailsClosedOnCorruptForeignJournal\|TestLinuxCloneIntentFreezesFenceAndReservesUniquePaths' ./internal/state/` | 0 | `ok state` |
| The authenticated receipt path is probed with `testStateLinuxCgroupV2ReleaseProofFixture` (its own `Validate`) rather than by run luck (§2.1 helper, §3a row 6) | included in the state probe above | 0 | `ok state` |
| Fix round: the retention helper against the three shapes it used to admit (reuse grant, native-usage recovery, binding-key mismatch) versus `state.LinuxV2ReleasedCloneRetainedAfterRelease` (F1), one scratch overlay, both bytes pinned in §11 | `go test -count=1 -v -overlay=<TMP>/owloss-f1/overlay.json -run 'TestF1CharacterizationRetentionHelperAgreement' ./e2e/` (WSL Ubuntu-24.04, `accept`, `umask 022`) | 0 | `ok e2e`; every row logs `helperRefused=true productionRetained=false` (§11); on the pre-fix bytes the same rows red with `helperRefused=false` |
| Fix round: both arms of the guard the runner now relies on (`linuxV2WaitPIDFDExited` observes an exited child; a live child stays red), same scratch overlay, 50 repeats under `-race` and `GOMAXPROCS=8` | `go test -race -count=50 -overlay=<TMP>/owloss-f1/overlay.json -run 'TestF8WaitPIDFDGuardArms' ./e2e/` | 0 | `ok e2e 32.175s`; the live arm logs `exact process pidfd remained live beyond the bounded wait` |
| Fix round: the runbook's locator program extracted verbatim from `docs/runbooks/daemon-process-stop.md:121-139` against 10 synthetic rows (1 candidate + 9 rejected shapes; F6) | host `jq 1.8.2` `-e -f <extracted program>` on the row files | candidate 0, nine refusals 1 | all nine rejected rows refused (reuse grant, cleanup pending, promotion intent, native-usage recovery, binding mismatch, unknown leaf disposition, live authority, unproven containment, missing release proof); the review-round program admitted five of them (§11) |

## 6. Mutants (M1-M15)

Every mutation was applied to a scratch copy of the delivered bytes — M13/M14 as `go test -overlay`
replacements, so the tree itself was never mutated — run as a focused `go test`, and reverted; the
last column proves the revert is byte-identical (`EQ` = the file's sha256 equals the delivered
bytes' sha256). Delivered sha256s (final pins, §7.3): `linux_clone_validation.go`
`9e622b03…`, `linux_cgroup_journal.go` `9358c264…`, `linux_cgroup_v2_test.go` `7353273d…`,
`linux_owner_witness_linux.go` `50aa3c05…`, `linux_owner_witness_linux_test.go` `fd6a7767…`,
`production_linux_v2_native_witness_test.go` `41b844e4…`, `production_linux_v2_owner_cli_test.go`
`3817826a…`, `production_linux_v2_owner_identity_test.go` `167308c0…` (the fix round changed the
native-witness and owner-cli files; the app-test, state-test and identity files are unchanged from
the implement round). The M1-M12 rows' `EQ` hashes name the bytes delivered at that time (the
implement round); the apply round and its dedupe round (§7.6) re-ran M13/M14 only, and the fix
round re-ran M15 (overlay, §11).

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
| M9 | a release tombstone used as abort/cleanup authority | the existing pins | no mutation needed: `TestLinuxCloneAbortCannotUseNormalReleaseAsNoTargetProof`, `TestLinuxCloneCleanupRequiresDeliveredCandidateArtifact` and `TestDeleteJournalKeepsCandidateCloneWithoutDeliveredArtifact` are green on the fix-round bytes (negative control) | n/a |
| M10 | `linuxV2RequireSameOwnerIdentities` reverted to the presence-only comparator | the swapped and equal-PID rows | `production_linux_v2_owner_identity_test.go:146`: `owner identity comparison error = <nil>, want "does not match its exact RunJournal PID/creation identity"` on `/a_consistently_swapped_bound_owner_set_fails_against_the_journal` and `/a_different_creation_identity_at_the_journal_PID_fails` | EQ `233d848d…` at the time; the file then took the §7.2 cleanup-allowance fix (`3e353cd3…`) |
| M11 | drop the admitted leg's `allowedNewRunKeys` registration (the pre-fix bytes of the cleanup comparator) | the run_key set reds at the fixture cleanup | run `45599614997`: `production witness changed a previously retained RunJournal: shared StateDir added unrelated RunJournal 43f0a37f-…/1` (§7.2) | n/a (the fix is run `45608856419`, all four sets exit 0; superseded by the binding run, §7.3) |
| M12 | leave the helper-exit wait at 30 s (equal to the helper's own stop timeout) | the run_key set reds intermittently in the `fresh_request` leg | run `45604636169`: `A helper did not exit with owner loss before the fresh request: exact process pidfd remained live beyond the bounded wait`, after the leg consumed its whole 30 s bound (0.23 s in run `45599614997`) — one red in two runs of the same leg | n/a (the bound is now 90 s) |
| M13 | drop the app classifier's delegation to the state predicate (the classifier reduced to the physical-release term alone; re-pinned in the dedupe round) | the five new classifier rows | all five red: `…/the_row_no_longer_requires_workspace_recovery`, `…/the_row's_workspace_binding_differs_from_its_clone_intent`, `…/the_row_carries_a_clone_reuse_grant`, `…/the_row_requires_native_usage_recovery`, `…/the_row's_clone_intent_is_no_longer_journal-valid` | n/a (overlay; delivered sha `50aa3c05…` unchanged) |
| M14 | `linuxV2RequireRetainedCloneArtifacts` neutered (`return nil` first; re-run in the dedupe round) | the disk subtest | `--- FAIL: TestLinuxV2RetainedPreCheckoutClonePredicates/retained_clone_artifacts_on_disk` | n/a (overlay; delivered sha `49d7f50c…` unchanged) |
| M15 | restore the pre-fix hand-copied body of `linuxV2RequireRetainedPreCheckoutClone` (F1's pre-fix bytes, §11) in an overlay copy (one hunk against the delivered file) | the three delegation rows and the F1 characterization | `production_linux_v2_owner_cli_test.go:2405: a non-retained shape passed the retained pre-checkout clone predicate` on `…/reuse_grant_present`, `…/native_usage_recovery_required`, `…/workspace_binding_key_differs_from_the_intent_binding` (the mutant's own line numbering; `:2397` in the delivered file); the characterization rows log `helperRefused=false productionRetained=false`; the seed-validation row stays green (it pins the extra term, not the delegation) | n/a (overlay; the target bytes are the binding run's head, sha `3817826a…`; the final bytes differ only by R5's comment deletion, §12.3) |

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

Apply round (2026-11-25): the branch was rebuilt from the implement-stage scratch head `9089efba`
with only the changed files replaced (the pushed tree is byte-identical to the worktree files),
keeping the same four-set workflow; runs 6-8 below, §7.5, and the dedupe round's run 9 and §7.6.
The binding round (run 10) rebuilt the scratch tree on the PR head `9bf8ed0e` with the same
scratch-only files and the five-set array (the four ruled sets plus `stage_replay_launcher_bound`);
it is the final-bytes acceptance (§7.3).

| # | Run | Case sets | Head | Outcome |
| --- | --- | --- | --- | --- |
| 0 | `45594766773` | all 14 | `a2ba9779` | workflow-only failure: `Install scratch-only gh credential-ingestion shim` failed because `scripts/ci/scratch-gh-shim` is a scratch-only file absent from the base; no case set ran and no evidence artifact was uploaded. Fixed by scratch commit `78daf41f`, which adds the shim verbatim. |
| 1 | `45595454970` | all 14 | `78daf41f` (base `6b2d4f8f` + the probe workflow + the shim) | base census: nine sets exit 1, five exit 0 (below) |
| 2 | `45599614997` | all 14 | implement-round bytes (commit `4fc35f42`) | `run_key_legacy`, `fence`, `subject` exit 0; `run_key` red at the fixture builder's own shared-State cleanup after the new admitted leg had passed (§7.2) |
| 3 | `45604636169` | all 14 | implement-round bytes + the scoped cleanup allowance (`a46e2e3e`) | `run_key_legacy`, `fence`, `subject` exit 0; `run_key` red at the fresh-request leg's helper-exit wait — a fixture bound of 30 s racing the helper's own 30 s stop timeout (§7.2) |
| 4 | `45608856419` | the four ruled sets | implement-round bytes + the corrected wait bound (`9089efba`; workflow array trimmed to the four sets) | **implement-round acceptance: all four sets exit 0**, `All 4 case sets exited 0.` (superseded round) |
| 5 | `45610526163` | the four ruled sets | witness third branch reverted (`c802fe27`) | M8: all four sets exit 1 with the pre-change refusal |
| 6 | `45616416727` | the four ruled sets | apply-round bytes, first attempt (`5fe6044d`) | red: the S4 disk twin asserted the B target path, which does not exist at this crash point; the failing message exceeded the probe's 400-byte print bound (§7.5) |
| 7 | `45619184193` | the four ruled sets | diagnostic build logging the artifact state before a forced failure (`b6cbed4b`) | all four sets red only at the forced assertion, with `target=ENOENT len=111`, `seed=file len=117`, `baseref=present len=98` — the artifact-state measurement that shaped the corrected predicate (§7.5) |
| 8 | `45621915266` | the four ruled sets | corrected apply-round bytes (`50d99ae2`) | **apply acceptance: all four sets exit 0** (§7.5), 13:16:33-13:23:55Z (superseded round) |
| 9 | `45628215703` | the four ruled sets | dedupe-round bytes (`d8723d74`, then the §7.3 pins) | **dedupe acceptance: all four sets exit 0** (§7.6), 13:53:38-14:01:21Z (superseded round) |
| 10 | `45650762686` | the four ruled sets + `stage_replay_launcher_bound` | PR head `9bf8ed0e` + the scratch-only files (five-set workflow; scratch commit `fab5242a`) | **binding acceptance: all five sets exit 0** (`All 5 case sets exited 0.`), conclusion `success`, 16:03:58-16:14:07Z |

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

### 7.2 Acceptance runs on the implement-round bytes (runs `45599614997`, `45604636169`, `45608856419`; superseded by §7.3)

Run `45599614997` (14 sets; the other ten sets' rows are identical to §7.1, five red for their own
pre-existing reasons):

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key_legacy` | 0 | the setup recovery returned `released` with its retained clone; the stale-request denial red at its intended identity mismatch |
| `stale_journal_fence` | 0 | same, at the persisted runtime-epoch mismatch |
| `stale_journal_subject` | 0 | same, at the changed Subject revision |
| `stale_journal_run_key` | 1 | `/fresh_request` PASS (0.23s); `/fresh_request_admitted` PASS (1.33s) — the admitted run reached its own `handoff_committed` boundary, was killed, recovered to the same released retained shape, and the retained setup row stayed byte-identical; then the fixture builder's `t.Cleanup` comparator (`production_linux_v2_native_witness_test.go:1917-1925`; `:1918-1926` at the run's bytes) failed with `production witness changed a previously retained RunJournal: shared StateDir added unrelated RunJournal 43f0a37f-…/1` |

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

Run `45608856419` on the implement-round bytes (`9089efba`), the four ruled sets (superseded by
§7.3):

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key` | 0 | `verified TestProductionLinuxV2UnresolvedJournalBlocksFreshRun: run=1 pass=1 skip=0`, then `Case set stale_journal_run_key exited 0` (11:59:52Z); the test's `/fresh_request`, `/fresh_request_admitted` legs and the fixture cleanup all pass in one run |
| `stale_journal_run_key_legacy` | 0 | `verified TestProductionLinuxV2StaleOwnerRequestDenials` + `/stale_journal_run_key` run=1 pass=1 skip=0 |
| `stale_journal_fence` | 0 | `…/stale_journal_fence` run=1 pass=1 skip=0 |
| `stale_journal_subject` | 0 | `…/stale_journal_subject` run=1 pass=1 skip=0 |

Final line: `All 4 case sets exited 0.` and the run's conclusion is `success` (the workflow job
fails when any set does). Each set's event gate reports `fail_events` absent, no skipped test and no
invalid JSON line, so the four sets' boundaries are reached on the delivered bytes — the same eight
product/e2e file bytes as the implement-round goal tree, superseded by the apply round's and the
fix round's pins in §7.3.

### 7.3 sha256 equality (final bytes; binding run `45650762686`)

| File | sha256 |
| --- | --- |
| `daemon/internal/state/linux_clone_validation.go` | `9e622b03b86031387443e3a3e7423e0336c0b4c5993afd4c3f2c5e1c16e1dc6a` |
| `daemon/internal/state/linux_cgroup_journal.go` | `9358c264623f719038ddcfecaab976f3f2e7fde82f28a6a9689662cd167d9b4d` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | `7353273d61e13beae90e2db5866f3767ec71537e67a009174be2b404d66a0d35` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `50aa3c05ac5bc384dc1018b77c14c9135dc9df25688c9df1d8786baaaa68cbbc` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `fd6a77674ad85379ce8f83fa221abc1992d25609ad8567aff4a6152cb4d14346` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `41b844e4f892a4d04e5adf6050541c8fb4f12633b8234fea0b63bbd7de4a7826` (fix round; the dedupe-round bytes were `5523352b94ee642e03f451fcaf0c4ea2039d613dc0fb22bf763412c45f8888e0`) |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `c7cce96c3d0bb265f77b1a0a3bd30b5dfcb07800ed7dc371aa10cad4a78c6dc9` (final bytes; the binding run's head held `3817826a40664a96565df5431d5553d63b0d00823a58bf9477c14e3030dcdf99` and the two are go/parser-equal without comments, §12.3; the dedupe-round bytes were `49d7f50c7a587c19cb515eb79c2cd058f6ff41727e894e17e24a6b3472bb7e55`) |
| `daemon/e2e/production_linux_v2_owner_identity_test.go` | `167308c0b8468a19f4c854d0a18196a443953f2df654225e507b9e1d5bebf1b2` |

Six of the eight files are byte-identical to run `45628215703`'s head (`d8723d74`, the §7.6 dedupe
round): run 45628215703 held them byte-identically (verified at the time by re-hashing the copied
files in the scratch worktree — the sha256 lines the push printed are the table's `EQ` values), and
the fix round touched only the two e2e files. The fix round changed those two (F7's comment
compression in the native-witness file; F1's delegation and F8's runner assertion in the owner-cli
file) and re-computed their pins with `sha256sum` on the delivered worktree bytes. The fix-round
acceptance is the binding run `45650762686` on the PR-head bytes: the PR head `9bf8ed0e` plus the
scratch-only five-set workflow (scratch commit `fab5242a`), the four `stale_journal_*` sets and
`stage_replay_launcher_bound`, all five exit 0 (`All 5 case sets exited 0.`, conclusion `success`),
with the per-set verbatim lines and artifact digests in `findings/290-binding-run.md`. The final
bytes differ from that run's head only by R5's two deleted doc comments in the owner-cli file
(§12.3); the table pins the final bytes and notes the run head's value. `45608856419` (implement
round), `45621915266` (apply round) and `45628215703` (dedupe round) are superseded rounds. The
run-8 apply-round head (`50d99ae2`) held the pre-dedupe pins (`8e5613e4…`, `ae3307c5…`,
`a5cdde02…`, `7d6b7b82…`, and `88ad36cb…` for the owner-cli file); the implement-round run
`45608856419` held the implement-round pins (`098a47d5…`, `63bc4706…`, `cb7e26cb…`, `6a6f07ad…`,
`94699059…`, `659febb5…`, `3e353cd3…`), all superseded here. The record, plan, runbook and TODO
bytes are not part of any run's head (no test or product behaviour depends on them).

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

### 7.5 Apply-round acceptance (runs `45616416727`, `45619184193`, `45621915266`; superseded by §7.3)

Run `45616416727` (first attempt, head `5fe6044d`) reddened all four sets before either the
`journal` or the `clone` witness snapshot was taken, so the stop sat between the response read and
`witness.Journal` — the window that holds the new S4 disk predicate; its message was not printed
because the probe echoes only output events ≤400 bytes and the message carried two absolute
artifact paths. Run `45619184193` (diagnostic build, head `b6cbed4b`: the predicate replaced by a
logger plus one forced failure) measured the artifact state on all four sets and failed only at
the forced assertion:

```
APPLYDIAG target=ENOENT len=111
APPLYDIAG seed=file len=117
APPLYDIAG baseref=present len=98
```

So the B target directory does not exist at this crash point — it is created by the clone
bootstrap after the `handoff_committed` handoff (`createFreshCloneTarget`), and the
`create_started` journal bit is the cgroup mkdir intent, not the clone target — while the A seed
bundle and the per-run base ref do exist. The predicate now asserts exactly the durable A-side
artifacts (§2.9, §9); the S4 B-target element stays unapplied because it conflicts with the
delivered crash point (the attempt and the diagnostic are its evidence).

Run `45621915266` (corrected bytes, head `50d99ae2`, 13:16:33-13:23:55Z, conclusion `success`) —
the four ruled sets; the only body difference from the red attempt is the predicate's target-path
element:

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key` | 0 | `verified TestProductionLinuxV2UnresolvedJournalBlocksFreshRun: run=1 pass=1 skip=0`; `/fresh_request`, `/fresh_request_admitted` and the fixture cleanup pass in one run; the added assertion sites at `production_linux_v2_native_witness_test.go:506-508`, `:569-571` and `owner_cli_test.go:1987-1989` all execute |
| `stale_journal_run_key_legacy` | 0 | `verified TestProductionLinuxV2StaleOwnerRequestDenials/stale_journal_run_key: run=1 pass=1 skip=0` |
| `stale_journal_fence` | 0 | `…/stale_journal_fence: run=1 pass=1 skip=0` |
| `stale_journal_subject` | 0 | `…/stale_journal_subject: run=1 pass=1 skip=0` |

Final line `All 4 case sets exited 0.`; each set's event gate reports no fail events, no skipped
test and no invalid JSON line. The pushed tree equals the apply-round worktree files
byte-for-byte (its seven file bytes are the pre-dedupe pins §7.3 lists).

### 7.6 jscpd dedupe round (dedupe-round bytes, run `45628215703`; superseded by §7.3)

The repository's duplicate gate (`python scripts/ci/jscpd-changed.py --base 30ed5ada…`, the stage's
merge base `git merge-base HEAD origin/main`) refused the first apply-round bytes twice (exit 1):
the app classifier's ten-term predicate was a verbatim clone of the state predicate
(`linux_clone_validation.go:57-67` in the pre-dedupe bytes), and the ACK-loss and helper
response-loss witnesses shared their five-line build/verify/start preamble
(`owner_cli_test.go:3619-3623` and `:3793-3797` in the pre-dedupe bytes, the pair the gate's
same-file report pointed at). Both were removed without weakening a term or a row: the state predicate is
exported as `LinuxV2ReleasedCloneRetainedAfterRelease` (`linux_clone_validation.go:53-65`) and the
app classifier `linuxV2NativeWitnessCloneRetainedAfterRelease`
(`linux_owner_witness_linux.go:3093-3097`) delegates to it plus the physical-release term; the
shared preamble is hoisted into `linuxV2StartProductionWitnessCommand`
(`owner_cli_test.go:3687-3696`, call sites `:3723` and `:3893`). The gate reports exit 0,
`0 tolerated among the added lines, none new`, on the frozen bytes of this section.

The round re-ran the four ruled sets on those bytes (run `45628215703`, head `d8723d74` =
the §7.3 pins, 13:53:38-14:01:21Z, conclusion `success`):

| Set | Exit | Rows |
| --- | --- | --- |
| `stale_journal_run_key` | 0 | `verified TestProductionLinuxV2UnresolvedJournalBlocksFreshRun: run=1 pass=1 skip=0` (13:58:07Z), then `Case set stale_journal_run_key exited 0` (13:58:09Z) |
| `stale_journal_run_key_legacy` | 0 | `verified TestProductionLinuxV2StaleOwnerRequestDenials/stale_journal_run_key: run=1 pass=1 skip=0`, `exited 0` (13:59:11Z) |
| `stale_journal_fence` | 0 | `…/stale_journal_fence: run=1 pass=1 skip=0`, `exited 0` (14:00:13Z) |
| `stale_journal_subject` | 0 | `…/stale_journal_subject: run=1 pass=1 skip=0`, `exited 0` (14:01:15Z) |

Final line `All 4 case sets exited 0.` (14:01:15Z); no `Failed case sets` line. The re-pinned
mutants hold the same red rows as before the round: M13b reds the five classifier rows, M14b the
disk subtest (§10).

## 8. Lesson-83 assertion trace

For the four `stale_journal_*` sets the pre-change runs stopped at the same two points
(`45485437534`, `45489336388`): the recovery leg died at the refusal, so every assertion *after*
the response read had never executed in any run. On the implement-round bytes the following execute for the
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
(`production_linux_v2_native_witness_test.go:1917-1925`), which fires only after a test body has
finished and had never seen a test whose fresh request is admitted. The trace in §2 covered the test
body's assertions and the new leg; it did not enumerate teardown-time comparisons. The next wall
behind it is empty: no other `t.Cleanup` in the file compares the shared StateDir, the three variant
sets pass, and the run_key body ends at the admitted leg (`:580-584`).

Run `45604636169` then found a second wall, this one of a different species: not an assertion that
had never executed, but a **fixture bound that races the product's own bound**. The Q3(a) pin waited
30 s (`:441` on those bytes) for the helper's owner-loss exit while the helper's own fail-safe is
bounded at `linuxOwnerHelperV2StopTimeout = 30 s` — equal, so the race is by construction; the same
leg passed in 0.23 s one run earlier. The fix moves the fixture bound to 90 s, past the product
bound with margin, above which only a helper that never exits stays red (M12). This is the staffed
lesson for bound-carrying pins: a fixture wait must be *strictly above* the product bound it wraps,
never equal to it.

The apply round re-ran the same four sets on the corrected bytes (run `45621915266`): the three
added assertion sites — `native_witness_test.go:506-508`, `:569-571` and `owner_cli_test.go:1990-1997`
— executed green for the first time there, and no new wall appeared (§7.5).
The dedupe round re-ran the same four sets on the dedupe-round bytes (run `45628215703`): the same
rows verified, the three sites sit on the success paths those rows cover, and no new wall appeared
(§7.6). The fix round keeps both e2e edits on those same success paths — the helper delegates to the
production predicate and the runner adds the helper-exit assertion before it hands the PIDFDs to the
leg (§11).

## 9. Residual risks and limits

- The binding acceptance is run `45650762686` on the final bytes — the four ruled sets plus
  `stage_replay_launcher_bound`, all five green (§7.3); the implement-round (`45608856419`),
  apply-round (`45621915266`) and dedupe-round (`45628215703`) runs are superseded rounds, each on
  its own bytes. The other case sets' pre-existing reds
  (`shared` owner-leg scratch group, `synthetic_goal_cleanup` release refusal, `migration`/`stale_leaf`
  release machinery, `stage_replay_*` exit contract) are reported, never fixed here.
- The fix round changes the two e2e files (§11); binding run `45650762686` is their end-to-end check
  on the fix-round bytes (§7.3), with `stage_replay_launcher_bound` giving F8's 90 s helper-exit wait
  its first coverage on that set. The two changed assertions are on paths those sets already reach
  green (F1's rows are unit-level; F8's assertion waits on the same helper exit the fresh-request
  leg observed in every green run).
- The exported `LinuxV2ReleasedCloneRetainedAfterRelease`'s exact scope is the place a later
  refactor can silently widen; the mutant rows M1/M3/M4/M7 (state arm) and M13 (app classifier) in
  §6 are the guard.
- The disk-side retention predicate asserts the durable A-side artifacts: the A seed bundle
  (`os.Lstat`) and the per-run base ref (`git show-ref` in the A repository). The B target path is
  not asserted — the crash point precedes the post-handoff clone bootstrap that creates it
  (`target=ENOENT` on all four sets in run `45619184193`), so a deletion of a not-yet-created
  directory is not detectable there; the predicate catches a deprovision change that deletes the
  seed bundle or the base ref while leaving the row untouched. Its absence-mirror is
  `linuxV2VerifyNoTargetCloneAbort` (`production_linux_v2_owner_cli_test.go:1406`).
- The retained row has no deletion path by design; the disposition command is a TODO item, not built.
- The witness retention classification is shape-driven: a future cleanup failure that leaves the
  exact retained shape would be reported as retention, but the e2e asserts the durable row through
  the production predicate plus the seed receipt's `Validate()` — strictly stronger than the
  predicate, which requires only a non-nil receipt — so a masked mutation is visible there. The
  production arm needs no validation term of its own: journal load validates the seed receipt and
  fails closed (`run_journal.go:544-549` → `validateJournal`, `journal_validation.go:13` →
  `validateLinuxCloneSeedReceipt`, `linux_clone_validation.go:211`, called at `:120`).
- The run-set census and the per-set rows are §7; a set reddening for a reason outside this goal's
  owned files is reported as its own row and not repaired here.
- The admitted-leg shared-State allowance is the one place the fixture could over-tolerate: it is
  populated by `production_linux_v2_native_witness_test.go:546` only, after the admitted run's
  journal boundary has been validated, and the comparator still requires every previously retained
  row to be byte-identical (M11).

## 10. Apply-round mutants (verbatim pins)

- M13 target `daemon/internal/app/linux_owner_witness_linux.go`, delivered sha256
  `50aa3c05ac5bc384dc1018b77c14c9135dc9df25688c9df1d8786baaaa68cbbc`; mutant file (built in
  `<TMP>/apply-m13b`, never written into the tree; consumed via `go test -overlay`) sha256
  `db6b7eb3ff2c141ebcdce9d92ecd49960e4637c5172f84941103851284033ea8`; delta: the delegation to
  `state.LinuxV2ReleasedCloneRetainedAfterRelease` dropped from
  `linuxV2NativeWitnessCloneRetainedAfterRelease`, which is then the physical-release term alone.
- M14 target `daemon/e2e/production_linux_v2_owner_cli_test.go`, delivered sha256
  `49d7f50c7a587c19cb515eb79c2cd058f6ff41727e894e17e24a6b3472bb7e55` (the apply round's bytes);
  mutant file (built in
  `<TMP>/apply-m14b`) sha256
  `61e486d62eacbf27e855ab906803a3a243273c0d7927be45866ef9779b032b90`; delta: one statement,
  `return nil` inserted first in `linuxV2RequireRetainedCloneArtifacts`.
- Commands: `go test -count=1 -overlay=<TMP>/apply-m13b/overlay.json -run
  'TestLinuxV2NativeWitnessCloneRetentionNamesOnlyTheRetainedRelease' ./internal/app/` (exit 1, the
  five rows §6 names red) and `-overlay=<TMP>/apply-m14b/overlay.json -run
  'TestLinuxV2RetainedPreCheckoutClonePredicates' ./e2e/` (exit 1, the disk subtest red). The
  delivered files were never mutated, so no revert step applies; the worktree sha256s stayed at the
  apply-round pins throughout.

## 11. Fix round (review findings F1-F8, 2026-11-25)

Every finding is FIX; none was judged invalid. F1/F7/F8 change the two e2e files and F2-F6 change
the records; no production file changes, so the accepted product surfaces (§7.5-§7.6) keep their
bytes.

| Finding | Site (fix-round bytes) | What changed | Observed before-state | Proof (command, exit) |
| --- | --- | --- | --- | --- |
| F1 (P2) | `daemon/e2e/production_linux_v2_owner_cli_test.go:2282-2292`; four new rows `:2378-2391` | The retention helper delegates to the exported `state.LinuxV2ReleasedCloneRetainedAfterRelease`, so `LinuxCloneReuseGrant == nil`, `!NativeUsageRecoveryRequired` and `WorkspaceBindingKey == intent.BindingKey` are asserted instead of hand-copied, and the validated-seed check stays as its one extra term with its own row. | The scratch characterization logged `helperRefused=false productionRetained=false` for all three shapes: the pre-fix helper admitted rows the production predicate rejects. | `go test -count=1 -run 'TestLinuxV2RetainedPreCheckoutClonePredicates' ./e2e/` exit 0 (§5); M15 reds the three delegation rows under the pre-fix body; the characterization run (§11.1) exits 0 on the delivered bytes |
| F2 (P3) | this record §7.2/§7.3; `docs/TODO.md:1976-1980`; `docs/archive/CLOSED-TODO.md:5228-5232` | The three acceptance rounds are named per round, §7.2 is titled "on the implement-round bytes", the ledger entries name the implement/apply/dedupe runs, and §7.3 re-pins the fix-round bytes and names the two changed files plus the bind-run gap. | The ledger cited `45608856419` "on the final bytes" while §7.3 pinned `45628215703` and marked the earlier pins superseded; §7.2's title said "on the delivered bytes". | The three files' own text now matches the runs they cite; the doc gates (§11.2) cover them |
| F3 (P3) | `docs/plans/2026-11-25-rf-acc-owner-loss-semantics-plan.md:42-43` | `WorkspaceRecoveryRequired` appended to the plan's exact-term list, which now matches the delivered predicate term-for-term (17/17 against `linux_clone_validation.go:57-64`). | The list stopped at `WorkspaceBindingKey == intent.BindingKey`; the predicate requires `WorkspaceRecoveryRequired` (`linux_clone_validation.go:64`). | term-by-term read of `:57-64` against the plan's list |
| F4 (P3) | `docs/plans/…plan.md:55,61,78,112` | The plan's identifiers are the delivered ones: `scanLinuxV2NativeAdmissionLocked`, the 90 s wait, `linuxV2JournalBindsOwnerSlot`, `TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease`. | `scaPlayerLinuxV2NativeAdmissionLocked`, `linuxV2JournalOwnerSlot`, `TestLinuxV2ReleasedCloneRetainedShape` and a 30 s wait existed nowhere in the delivered bytes (whole-repo grep: no hit). | `grep -rn` over `daemon/` finds each new identifier; §5's probe command names the delivered state test |
| F5 (P3) | `docs/runbooks/daemon-process-stop.md:102-107`; `docs/TODO.md:1997-2001` | Both texts now say the retention keeps the clone row, the A seed bundle and the base ref, and that the B target exists only once the clone bootstrap has created it, citing the artifact-state measurement. | Both said the retained row keeps "the B checkout"/"the B clone … on disk" while the goal's own diagnostic (run `45619184193`, §7.5) measured `target=ENOENT` at the crash point. | §7.5's `target=ENOENT` measurement, named in the corrected texts |
| F6 (P3) | `docs/runbooks/daemon-process-stop.md:122-140,149-153` | The locator gained every expressible missing term (seed receipt, promotion intent, cleanup pending, target removal proof, reuse grant, native-usage recovery, workspace path/binding/recovery, release-proof version/stop-receipt/leaf-disposition, pre-gate proof, containment terms) and names its residual approximation. | The review-round program admitted five rejected shapes when probed with the same ten fixtures (reuse grant, cleanup pending, promotion intent, native-usage recovery, binding mismatch). | §5's jq row: the extracted program admits the candidate (exit 0) and refuses all nine rejected shapes (exit 1 each); the before-state is the review-round program |
| F7 (P3) | `daemon/e2e/production_linux_v2_native_witness_test.go:1071-1073` | The `afterStageProcessKilled` comment block is two lines (1071-1072), the field at 1073. | The block ran three lines (1071-1073), one over the contract's two-line cap for a comment block this diff extended. | `gofmt -l .` exit 0 (§4); the block is 1071-1072 |
| F8 (P3) | `daemon/e2e/production_linux_v2_owner_cli_test.go:1958-1964` | The crash-replay runner asserts the helper's owner-loss exit (`linuxV2WaitPIDFDExited`, 90 s) for every caller before it hands the PIDFDs to the leg, so the three `stale_journal_*` variant legs inherit the Q3(a) assertion. | Only the fresh-request leg asserted the exit (`native_witness_test.go:424-426`, `:491`); the three variants ran with `afterStageProcessKilled == nil` and the runner pinned only the anchor, so a fail-safe regression would not have reddened them. | §5's F8 probe row (both arms of the relied-on wait, 50 repeats under `-race`, exit 0); the same assertion text has one observed CI red (M12, run `45604636169`); its own red needs a helper that never exits — recorded as an oracle gap below |

### 11.1 Scratch pins and mutants (verbatim)

- Overlay test file `<scratch>/owloss-f1/f1_characterization_test.go`, sha256
  `6db75aa1bcdfb7fc302034d7ef90894b425c784d634fe6777c6d07794540b9ad`, mapped by
  `<scratch>/owloss-f1/overlay.json` (sha256
  `557612e1f31c60616524fdb9fcce98489b64d0ff7721187728c15d4b989de41b`) to the in-package path
  `<worktree>/daemon/e2e/zz_f1_characterization_overlay_test.go`; the tree itself was never
  written. It carries the F1 characterization (one positive row per shape) and the F8 guard probe
  (one positive and one negative case for the wait the runner relies on), both verbatim:

```go
//go:build linux && amd64

package e2e_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// F1: the e2e retention helper must admit exactly what the production predicate admits; these three
// shapes were the drift between the hand-copied term list and state.LinuxV2ReleasedCloneRetainedAfterRelease.
func TestF1CharacterizationRetentionHelperAgreement(t *testing.T) {
	target := "/srv/symmetry-fixture/workspaces/binding-b9/run-fix1-run/generation-7"
	retained := state.RunJournal{RunID: "fix1-run", Generation: 7, RunCloneState: state.RunCloneState{
		WorkspacePath: target, WorkspaceBindingKey: "b9", WorkspaceRecoveryRequired: true,
		LinuxCloneSeedReceipt: &authority.LinuxCloneSeedReceipt{Version: authority.LinuxCloneVersion,
			BaseCommit: strings.Repeat("1", 40), Digest: "sha256:" + strings.Repeat("2", 64), Size: 4096, Device: 9, Inode: 13},
	}}
	intent := authority.LinuxCloneIntent{Version: authority.LinuxCloneVersion, RunID: "fix1-run", Generation: 7,
		BindingKey: "b9", ResourceID: "11111111-2222-4333-8444-555555555555", BaseCommit: strings.Repeat("1", 40),
		BaseTreeDigest: "sha256:" + strings.Repeat("2", 64), Nonce: strings.Repeat("3", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/srv/symmetry-fixture/seeds/binding-b9/run-fix1-run/generation-7.bundle", Target: target}
	retained.LinuxCloneIntent = &intent
	if err := retained.LinuxCloneIntent.Validate(); err != nil {
		t.Fatalf("intent fixture: %v", err)
	}
	if !state.LinuxV2ReleasedCloneRetainedAfterRelease(retained) {
		t.Fatal("the fixture row is not the retained shape the predicate reports")
	}
	for _, shape := range []struct {
		name   string
		change func(*state.RunJournal)
	}{
		{"reuse grant present", func(journal *state.RunJournal) {
			journal.LinuxCloneReuseGrant = &authority.LinuxCloneReuseGrant{}
		}},
		{"native usage recovery required", func(journal *state.RunJournal) {
			journal.NativeUsageRecoveryRequired = true
		}},
		{"workspace binding key differs from the intent binding", func(journal *state.RunJournal) {
			journal.WorkspaceBindingKey = "secondary"
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			journal := retained
			shape.change(&journal)
			helperErr := linuxV2RequireRetainedPreCheckoutClone(journal)
			production := state.LinuxV2ReleasedCloneRetainedAfterRelease(journal)
			t.Logf("F1CHAR %s: helperRefused=%t productionRetained=%t", shape.name, helperErr != nil, production)
			if (helperErr == nil) != production {
				t.Errorf("F1CHAR disagreement: helperRefused=%t productionRetained=%t", helperErr != nil, production)
			}
		})
	}
}

// F8: the crash-replay runner's new assertion relies on linuxV2WaitPIDFDExited; one positive case
// (an exited child is observed) and one negative case (a live child stays red) pin both of its arms.
func TestF8WaitPIDFDGuardArms(t *testing.T) {
	startChild := func(t *testing.T, script string) (*os.File, *exec.Cmd) {
		t.Helper()
		command := exec.Command("/bin/sh", "-c", script)
		if err := command.Start(); err != nil {
			t.Fatalf("spawn probe child: %v", err)
		}
		identity, err := platform.ProcessIdentity(command.Process.Pid)
		if err != nil {
			t.Fatalf("read probe child identity: %v", err)
		}
		pidfd, err := linuxV2OpenExactPIDFD(command.Process.Pid, identity)
		if err != nil {
			t.Fatalf("open exact probe child pidfd: %v", err)
		}
		return pidfd, command
	}
	t.Run("exited child is observed", func(t *testing.T) {
		pidfd, command := startChild(t, "sleep 0.3")
		defer func() {
			_ = pidfd.Close()
			_ = command.Wait()
		}()
		if err := linuxV2WaitPIDFDExited(int(pidfd.Fd()), 10*time.Second); err != nil {
			t.Fatalf("F8PROBE exited child: %v", err)
		}
	})
	t.Run("live child stays red", func(t *testing.T) {
		pidfd, command := startChild(t, "sleep 30")
		defer func() {
			_ = pidfd.Close()
			_ = command.Process.Kill()
			_ = command.Wait()
		}()
		err := linuxV2WaitPIDFDExited(int(pidfd.Fd()), 300*time.Millisecond)
		t.Logf("F8PROBE live child: %v", err)
		if err == nil {
			t.Fatal("F8PROBE live child passed the bounded exit wait")
		}
	})
}
```

- M15 mutant `<scratch>/owloss-f1/mutant1/production_linux_v2_owner_cli_test.go`, sha256
  `cee4c6001924296b11c50dd3263e376ac351989c2258f80401db0681098fe3a7`, mapped by
  `<scratch>/owloss-f1/mutant-overlay.json` (sha256
  `437e79e1faf99c8a5d9b35d1537146559bebd1661208e1bc0a49c6da0828832e`). Delta against the delivered
  file: exactly one hunk — the helper body and its comment reverted to the pre-fix bytes (the
  hand-copied term list exactly as the review round had it, 17 lines against the delegate's 11) —
  with F8's runner assertion and all four new rows kept, so the mutation isolates the delegation.
- Commands (WSL Ubuntu-24.04, user `accept`, `umask 022`, Go 1.27.0, working directory `daemon/`):
  - `go test -count=1 -v -overlay=<scratch>/owloss-f1/overlay.json -run 'TestF1CharacterizationRetentionHelperAgreement|TestF8WaitPIDFDGuardArms|TestLinuxV2RetainedPreCheckoutClonePredicates' ./e2e/` — exit 0 on the delivered bytes (`ok e2e 0.382s`); every shape logs `helperRefused=true productionRetained=false`.
  - the same command on `<scratch>/owloss-f1/mutant-overlay.json` — exit 1: the three new rows red with `a non-retained shape passed the retained pre-checkout clone predicate` and each shape logs `helperRefused=false productionRetained=false`.
  - `GOMAXPROCS=8 go test -race -count=50 -overlay=<scratch>/owloss-f1/overlay.json -run 'TestF1CharacterizationRetentionHelperAgreement|TestF8WaitPIDFDGuardArms' ./e2e/` — exit 0 (the §5 row carries the observed timing); the live arm logs `exact process pidfd remained live beyond the bounded wait`.
  - F6: a read-only script extracted the runbook's program into `<scratch>/owloss-f1/locator.jq` (sha256 `9f88ec5f2d24f6517ddec5f3be72a5bf3d71a009c64882da2ac41b4c8f39342d`, so the probe binds the delivered program text), and `jq -e -f locator.jq row-*.json` (host jq 1.8.2) over ten fixtures admitted the candidate and refused the nine rejected shapes (§5).
- F8 oracle gap: the runner's assertion can only redden when a helper does not exit on owner loss,
  and the helper's fail-safe lives outside this goal's owned set
  (`daemon/internal/platform/helper_v2_recovery.go`); no mutant inside the owned set produces that
  red. Its two arms are probed locally instead, and the assertion's class has the M12 CI red (a
  fixture bound racing the product bound) as its closest observed failure.

### 11.2 Fix-round recipe gates

Run on a scratch commit of the fix-round tree built over HEAD with a temporary index (no branch,
remote ref or HEAD moved), each with `--repo .` and `--base 691eb424…`:

- `python scripts/ci/jscpd-changed.py` — exit 0: `13 changed file(s), 1743 scanned source(s), 1261
  clone(s) in the corpus, 0 tolerated among the added lines, none new`; a notice records that
  `docs/archive/CLOSED-TODO.md`'s added lines are outside jscpd's scan scope.
- `python scripts/ci/check-record-cleanliness.py` — exit 0: `1 changed record(s) pass the root-only
  check`.
- `python scripts/ci/check-citation-existence.py` — exit 0: `2 changed document(s) pass the path and
  symbol check (633 path citation(s))`, plus three pre-existing ignore-rule notes (CLOSED-TODO lines
  737, 752, 2512).
- `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc — exit 0
  for this record, the plan, the runbook and TODO.md; exit 1 for `docs/archive/CLOSED-TODO.md`,
  whose flagged fragments sit in the merge-base bytes (18 rendered lines, none added by this branch;
  the branch's added doc lines pass the same check, exit 0).
- Host-path scan over the branch diff for the user-profile prefix: no match (`grep` exit 1).

## 12. Round-1 review fixes (R1-R6, 2026-11-26)

The PR #290 round-1 reviews (`findings/290-opus-review-1.md`, `findings/290-ds-audit-1.md`,
`findings/290-sec-review-1.md`) agreed the product is sound and fail-closed and found one binding gap
(closed by the binding run) plus record defects; the conductor ruled six record-level fixes (R1-R6).
No product or test code changes: R5 deletes two doc comments and carries the no-comments equality
proof in §12.3.

### 12.1 Dispositions

| Item | Action |
| --- | --- |
| R1 | the run-id census (§12.2) over this record, the plan, TODO, CLOSED-TODO, the runbook and the PR body names `45650762686` as the binding run and labels `45608856419` (implement), `45621915266` (apply) and `45628215703` (dedupe) superseded |
| R2 | `docs/archive/CLOSED-TODO.md` gives each of the two closures its own heading (`…: the F4 design question` / `…: owner-identity polish`) |
| R3 | every cross-record section citation on the changed lines carries the source record's path: CLOSED-TODO's `§4c` (`docs/evidence/rf-acc-pi-durability-matrices-2026-11-24.md`), `§9 items 8-10` and `§12` (`docs/evidence/rf-acc-witness-unresolved-exit-2026-11-25.md`), both `§6` mutant citations (this record); TODO's and the runbook's `§7.5` (this record); the plan's carried-item heading |
| R4 | §9's "cannot drift" clause corrected: the e2e helper is strictly stronger than the predicate (its extra seed `Validate()`), and the production arm's presence-only seed term is safe because journal load validates the receipt and fails closed |
| R5 | the two restating doc comments deleted from `daemon/e2e/production_linux_v2_owner_cli_test.go`; go/parser no-comments equality proof in §12.3 |
| R6 | §3b corrected to 17 remaining sites and `native_witness_test.go:2472` named with its reason; recorded grep in §12.4 |

### 12.2 Run-id census (R1)

Command: `grep -c -E '45608856419|45621915266|45628215703|45650762686'` over the five changed records
(this record, the plan, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the runbook) and the PR body
(`290-body.md`, the conductor's findings store, line hits): final bytes — this record 29, the plan
0, `docs/TODO.md` 2, `docs/archive/CLOSED-TODO.md` 2, the runbook 0, the PR body 5. The binding run
`45650762686` is named as such in §7.3/§9 here, in TODO, in CLOSED-TODO and in the PR body's
Acceptance section; the three earlier acceptance rounds are labelled superseded in this record's
run-table rows 4, 8 and 9 and in §7.3/§9, in TODO, in CLOSED-TODO and in the PR body. The records'
other run ids are the rounds' own diagnostics (censuses, mutants, red probes) or other goals' runs,
each labelled by role in the §7 run table.

### 12.3 R5 comment deletion (go/parser no-comments equality)

Deleted: the two doc comments above `linuxV2RequireRetainedCloneOutcome` and
`linuxV2StartProductionWitnessCommand` — the only delta from the binding run's head.

- Binding run head bytes (`9bf8ed0e:daemon/e2e/production_linux_v2_owner_cli_test.go`; byte-identical
  to the scratch commit `fab5242a`, §2.1 of `findings/290-binding-run.md`): sha256
  `3817826a40664a96565df5431d5553d63b0d00823a58bf9477c14e3030dcdf99`.
- Final bytes: sha256 `c7cce96c3d0bb265f77b1a0a3bd30b5dfcb07800ed7dc371aa10cad4a78c6dc9`.
- A throwaway Go program (sha256 `f8f9cac6f7ebfdaa847332f88126768c23d379966a25969bf2e5722f82a2f011`,
  deleted afterwards) parsed both revisions with `go/parser` (mode 0, comments discarded), rendered
  each with `go/printer` and compared: both print 241110 bytes, byte-equal, common printed sha256
  `ad157a915c88784382efe0def4668343697710003a5716ac54483f01ca798083`; a negative control (an
  identifier renamed in a copy) printed unequal and exited 1. Every comment-free AST is therefore
  identical, and the binding run still binds the final bytes.
- `gofmt -l daemon/e2e/production_linux_v2_owner_cli_test.go` exit 0 (unchanged from the fix round);
  `GOOS=linux GOARCH=amd64 go vet ./e2e/` exit 0 and `GOOS=linux GOARCH=amd64 go test -c -o /dev/null
  ./e2e/` exit 0 (the touched package's compile check).

### 12.4 §3b census re-run (R6)

Recorded grep on the final bytes: `grep -rn
"linuxV2RequireLivePIDFD\|linuxV2OpenStageOwnerPIDFD\|linuxV2KillOwnerWitnessCommand" daemon/e2e/*.go`
→ 29 lines; `grep -n "linuxV2RequireLivePIDFD" daemon/e2e/*.go` → 21 lines (1 definition +
20 call sites); minus the three converted sites (`native_witness_test.go:420`, `:488`,
`owner_cli_test.go:1954`) → 17 remaining, one of them (`native_witness_test.go:2472`) now named
explicitly in §3b; the `linuxV2OpenStageOwnerPIDFD`/`linuxV2KillOwnerWitnessCommand` definition
sites are relabelled there.

### 12.5 Final-byte recipe gates

Run on a scratch commit of the final bytes built over HEAD with a temporary index (no branch, remote
ref or HEAD moved; range `61399984…` to the scratch commit):

- `python scripts/ci/jscpd-changed.py --base 61399984…` — exit 0: `13 changed file(s), 1748 scanned
  source(s), 1261 clone(s) in the corpus, 0 tolerated among the added lines, none new`; a notice
  records that `docs/archive/CLOSED-TODO.md`'s added lines are outside jscpd's scan scope.
- `python scripts/ci/check-record-cleanliness.py --base 61399984…` — exit 0: `1 changed record(s)
  pass the root-only check`.
- `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc — exit 0
  for this record, the plan, the runbook and TODO.md; exit 1 for `docs/archive/CLOSED-TODO.md`,
  whose 18 flagged fragments are byte-identical to the merge base's (the `diff` of the two rendered
  fragments is empty, exit 0; 18 changed lines each), and every added line of the branch passes the
  same check (exit 0, one stdin run per record).
- Host-path scan over the branch diff for a user-profile prefix, drive roots or the local user name:
  no match (`grep` exit 1).
