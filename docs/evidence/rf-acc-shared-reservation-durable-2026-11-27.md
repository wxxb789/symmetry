# rf-acc-shared-reservation-durable — evidence record (the pre-create tombstone the `reservation_durable` leg refuses)

Goal `rf-acc-shared-reservation-durable` (contract under the GDD contracts root, ruled by the
conductor 2026-11-27; dispatch gate #308 at `ac04f3e2`). Branch
`refactor/rf-acc-shared-reservation-durable`, dispatch base
`ac04f3e2ec8b48c9f28a6f7ed66f9abc87a3882a`. Plan
`docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md`. Shape reference:
`docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md` (committed, tested, allowlisted-evidence
increment). The digests below bind the delivered bytes; the stage's own gate receipts bind the base,
the changed-file scope and the content digests (§8) rather than a scratch commit sha.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `96306f493b8a55a6fddcaa5052679733a3e9fd0e8be2e10e36110a40b4bd4c56` | The `never_created` disposition switch is a named predicate demanding `CreateStarted=false, LeafNeverAppeared=false`; the held stage binds the disposition (`linuxV2ValidateHeldStageAbortDisposition`); three committed tests drive the production tombstone, the one-field clause mutants, the authority-valid post-create row and both wrong-stage directions. |
| `docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md` | `ea3f176f15908bc515fb12666173429ab366afd354dcba56f7cdeb9d2ba9397c` | Stage-1 read, the accepted decisions, the held-stage binding and the live consumers. |
| `docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md` | this record | Stage-1 answers, the fix, the receipts. |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | The closed stage-1/2 items and the open Bind/residual entries. |

This fix round supersedes the earlier stage-2 and round-2 bytes: the Go blobs
`9c36f90f198e27a5a380ad336cda80ab4f745379831772256abba8a951be32cc` (stage 2) and
`dba2daec309c4ee01eda195ce0539d2c59e67b718a93e847f0dc12826d258ebd` (round 2), and every receipt,
digest and line anchor in this record that named them, are PARTIAL and SUPERSEDED by the bytes
above. §5 labels each earlier receipt; §11 re-measures every anchor on the delivered bytes.

The table above lists four data rows naming five of the six changed paths
(`git diff --name-status ac04f3e2..c9a4bbf9`); the sixth, the native witness test, is not
byte-pinned here — its delivered digest is in §11 (`98d4771c…`).

The scope is the live call chain of the named witness: `production_linux_v2_native_witness_test.go`
owns the leg and calls `runProductionLinuxV2StageCrashReplay` → `linuxV2ValidateStageReplayJournal`
in `production_linux_v2_owner_cli_test.go`, which holds the stop, the predicate and its helpers.
`scripts/ci/linux-v2-native-acceptance.sh` (the `shared` replay arm) needs no change and is
byte-identical to the dispatch base: the fix needs no fixture, pipeline or workflow change.

## 2. Contract clause 1 — new or long-standing? Long-standing, and unsatisfiable since it was written

`git grep -n "LeafNeverAppeared" 41e64851 -- daemon` (#9, 2026-10-13) returns the contradiction
already assembled in one commit:

```text
41e64851:daemon/e2e/production_linux_v2_owner_cli_test.go:1315:  if abort.CreateStarted || !abort.LeafNeverAppeared || abort.Helper != nil || abort.HelperExit != nil {
41e64851:daemon/internal/authority/authority.go:1251:  if proof.LeafIdentity != nil || !proof.LeafAbsent || proof.LeafEmpty || proof.LeafNeverAppeared != proof.CreateStarted ||
41e64851:daemon/internal/state/state.go:2724:  LeafNeverAppeared: false,
41e64851:daemon/internal/authority/authority_v2_test.go:237:  proof.LeafNeverAppeared = true
41e64851:daemon/internal/state/linux_clone_journal_test.go:265:  cleared.ContainmentCgroup.AbortProof.LeafNeverAppeared ||
```

The clause (then `:1315` at #9, `:1477` at the dispatch base — this goal's fix moves it into the
predicate, at delivered `:1530`), the authority rule (then `authority.go:1251`, now
`internal/authority/cgroup.go:383`), the producer (then `state.go:2724`, now
`internal/state/linux_cgroup_journal.go:916`) and both pins are from the same commit.
`git log -S` over each text finds only the move-only refactors `9ac85ac0` (#158, authority split)
and `ff18add1` (#255, state split) after it; no commit changed any of the three tokens.

So the wall cannot be new in its cause: `linuxV2ValidateStageReplayJournal` checked
`abort.Validate()` first (dispatch base `:1467`), which forces `LeafNeverAppeared == CreateStarted`,
and the pre-fix clause then demanded `CreateStarted == false && LeafNeverAppeared == true`. One conjunct always
fires, at every revision since #9, for every tombstone that reaches the switch. No run on any
revision could pass the leg, and the #308 record's Bind 1 (`46020141750`) shows that run's first
failing witness line as this same `:224` stop, newly reachable once the pi leg was deferred: its
first 21 green rows match Bind #303's order, and the record states there was no prior replay pass.

#308 contributed nothing to the Journal state: `git show --stat ac04f3e2` lists only
`daemon/internal/harness/pi/rpc.go` and its test, five `scripts/ci/*` files and that goal's own
plan, record and TODO documents — no cgroup, state or recovery file. Its credential gate changed
*when* the `shared` arm reaches the replay invocation (the pi leg no longer aborts the arm), not
what the daemon persists.

## 3. Contract clause 2 — which layer is wrong: the witness's expectation

The wrong layer is `daemon/e2e/production_linux_v2_owner_cli_test.go:1477` (the pre-fix clause at
the dispatch base), which demands a proof shape the authority forbids. The two legal
`never_created` shapes are pinned by `internal/authority/authority_v2_test.go:231-244`, the
positive pre-mkdir `Validate`/`ValidFor` check at `:231-233`:

- `CreateStarted=false, LeafNeverAppeared=false` — the pre-mkdir fence tombstone, pinned at
  `:231-233` and refused at `:235-237` if `CreateStarted` is set without the observed-never-appeared
  flag ("post-create-intent proof inferred never-created from path absence alone");
- `CreateStarted=true, LeafNeverAppeared=true` — the accepted "exact never-appeared proof".

The demanded mix (`false, true`) fails `Validate` under both. The producer writes the first shape
(`internal/state/linux_cgroup_journal.go:903-916`, `CreateStarted: false, LeafNeverAppeared: false,
LeafAbsent: true`), pinned by
`internal/state/linux_clone_journal_test.go:256-260`, and the pre-mkdir policy is documented at
`internal/authority/cgroup.go:140-141` and `:898-902` of the state file: exclusive durable creator
fence, no filesystem observation, no stop receipt, no leaf-removal authority.

The recovery is not defective: the `reservation_durable` boundary requires
`reservation.CreateStarted == false` (`:1617-1620` at the dispatch base; delivered `:1877-1879`), so
`internal/app/linux_owner_recovery_linux.go:139-146` takes the never-created branch and persists
exactly the fence shape. No fixture tombstone is staged (the arm writes only the barrier request and
the control claims), and `scripts/ci/*` holds no copy of this expectation. **No `DECISION_NEEDED`**:
the defect is in the test expectation, not in the daemon or platform, and the fix touches no
production file.

## 4. Contract clause 3 — the fix, and what it does not weaken

- `linuxV2ValidatePreEffectAbortTombstone` now holds the two pre-effect shapes; the caller keeps the
  exact error text and message identity (`linuxV2ValidateStageReplayJournal` still reports the
  string verbatim).
- `never_created` demands `!CreateStarted && !LeafNeverAppeared` with no helper/helper-exit. The two
  conjuncts `Validate` already implies stay explicit, matching the sibling `removed_empty` clause;
  the conjunct that contradicted `Validate` is the one removed. The old clause proved nothing (it
  refused every legal proof), so nothing of equal strength is lost; the new clause is satisfiable
  and still refuses a tombstone reporting the create CAS, an observed absence, a helper or a helper
  exit. Each of those four refusals has its own one-field row over the *production* tombstone
  (`:1608-1624`), red-capable per §5's M1-M4 mutants; the `removedEmpty` row (`:1625-1628`) walks
  the *sibling* `removed_empty` arm and is not evidence for the never-created conjuncts, and the
  `unknown` row (`:1629-1632`) pins the default arm. The M1-M4 single-field rows and the
  disposition-only `removedEmpty` row are DELIBERATELY INVALID direct predicates — each fails
  `authority.Validate` at `daemon/internal/authority/cgroup.go:381-384` — and are driven only as
  negative inputs to the switch, not as valid-proof controls. A fifth row, `postCreateClaimedAbsence`
  (`:1642-1651`), sets `CreateStarted` and `LeafNeverAppeared` together — the authority's own
  accepted "exact never-appeared proof" — asserts `Validate()` accepts it, and only then demands the
  same target/leaf refusal. It and the removed-empty fixture that drives the stage/replay rows
  (`:1654-1676`, `:1712-1740`) are the VALID controls. It is the sole row pinning that
  authority-valid shape: the row was absent in round 2, so M8 was green in round 2 and reds only the
  restored row in round 3.
- The stage binding: the predicate alone accepted either pre-effect disposition at whichever
  `aborted_no_target` hold reached it, and `linuxV2ExpectedStageReplayRecovery` maps two holds there
  (`reservation_durable`, `helper_bound`). `linuxV2ValidateStageReplayJournal` now takes the held
  `stage` and refuses any proof whose disposition is not that hold's
  (`linuxV2ValidateHeldStageAbortDisposition`, delivered `:1508-1525`, called at `:1448-1451`);
  `reservation_durable` requires `never_created`, `helper_bound` requires a leaf-bound disposition,
  and any other held stage presenting a no-target tombstone is refused. The abort proof is read by
  the same extraction (`linuxV2StageReplayNoTargetAbort`, `:1495-1506`) that the identity, fence,
  `ValidFor` and helper-reap checks already used; those checks are unchanged and still run.
- Row mapping: the production pre-mkdir tombstone must be accepted → the green row of
  `TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape`; the create-CAS, helper, helper-exit,
  claimed-absence and authority-valid post-create shapes must stay refused → its `createStarted`,
  `helper`, `helperExit`, `claimedAbsent` and `postCreateClaimedAbsence` rows; the wrong-stage
  directions → `TestLinuxV2HeldStageBindsThePreEffectAbortDisposition`
  (a *valid* `removed_empty` proof refused at `reservation_durable`, a *valid* pre-mkdir proof
  refused at `helper_bound`, and both accepted at their own hold) and
  `TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage`, which drives the same
  four directions through the replay validator the leg calls; the old clause's contradiction of the
  producer → the §5 M5 polarity mutant. No contract invariant of this change lacks an executable
  row.
- Live consumers of the changed conjunct and the new binding, kept apart by clause reach and binding
  reach and by the kind of coverage they carry. The ordinary stage replay's `reservation_durable`
  leg consumes BOTH: the changed `never_created` conjunct (clause reach) and the new stage binding
  (binding reach); it is observed green in all three scratch receipts (`46037316453` on the
  clause-only pre-fix blob `9c36f90f…`; `46041720955` and `46045331565` on bytes that also carry the
  binding). The ordinary and ACK-loss `helper_bound` legs, *if reached*, consume the BINDING and then
  the UNCHANGED `removed_empty`/`absent_after_rmdir` arm — a `never_created` proof at `helper_bound`
  returns at witness `:1449-1451,1517-1520`, before the changed conjunct at `:1530` — so those legs
  are binding consumers, not clause consumers. (a) Observed: the `reservation_durable` leg above,
  whose clause observation is round 1 and whose binding observation is rounds 2-3. (b) Intended live
  consumers *if reached* on the final head: the ordinary stage replay's `helper_bound` leg (the
  binding's second accept arm) and the ACK-loss replay's second response for `helper_bound`
  (`run_ack_loss_replay_cases ... helper_bound`, `scripts/ci/linux-v2-native-acceptance.sh:3301-3303`,
  whose second response is validated at delivered `daemon/e2e/production_linux_v2_owner_cli_test.go:4689`;
  `helper_bound` also maps to `aborted_no_target`, so it reaches the binding before the unchanged
  sibling clause). No final-head observation is claimed for (b). (c) New unit controls: the three
  `daemon/e2e` tests of §5, which run in the `daemon` job's race pass and locally, never in the Bind.
  (d) Scheduled final-head coverage for (a)'s binding and (b): the conductor's protected credentialed
  dispatch (§9) is its only acceptance.
- Unchanged: `authority.Validate`, the state producer, the tombstone semantics and json fields, the
  boundary validators, the replay stage list, the acceptance script and the exporter. The `shared`
  arm's credential-gated pi leg (1B1) is untouched, so the gated outcome is recorded exactly as
  before.
- Adversarial satisfaction, clause: the `reservation_durable` acceptance needs a tombstone that is
  `never_created`, `!CreateStarted`, `!LeafNeverAppeared` and carries no leaf identity, helper,
  helper exit, launcher or launcher exit, and it must pass `authority.Validate`. The only real
  producer of that shape is `state.Store.AbortNeverCreatedLinuxCgroupReservation` under the fence
  the boundary validator already requires (`:1877-1879`); the accept arm is driven by that production
  producer rather than a literal, and the one refusal row that must be a valid proof
  (`postCreateClaimedAbsence`) asserts `Validate()` accepts it before the refusal. The M1-M4 and
  disposition-only `removedEmpty` rows are deliberately invalid predicates (they fail `Validate` at
  `daemon/internal/authority/cgroup.go:381-384`), so they cannot satisfy the accept arm.
- Adversarial satisfaction, binding: without the stage binding an `aborted_no_target` hold could be
  satisfied by the *other* hold's valid disposition. The extraction prefers the clone tombstone, the
  `ValidFor` binding is skipped whenever `ContainmentCgroup` (or its mirror) is absent (`:1448-1467`),
  and a bare `LinuxCgroupReservationAbortProof.Validate()` requires no reservation at all, so the
  real event that could satisfy the unbound direction is a MALFORMED raw journal — a hand-edited
  `LinuxCloneAbortProof.CgroupAbortProof`, or a regression that broke the Store's history invariant —
  not a normal recovery. Normal Store completion and read-back exclude it:
  `CompleteLinuxCloneAbort` requires `linuxCloneAbortHistoryMatches`
  (`internal/state/linux_clone_journal.go:691-693`), which for a cgroup-backed clone proof requires
  `journal.ContainmentCgroup != nil` AND a matching reservation tombstone
  (`internal/state/linux_clone_validation.go:396-401`), and the witness reader itself deliberately
  raw-`Unmarshal`s the journal, so the raw hole is the witness's, not the Store's. The binding
  refuses the wrong-disposition direction for both holds before the clone-abort, identity and
  `ValidFor` checks run. §5's M6/M7 mutants attribute those refusals to it: they bind that
  attribution on authority-valid NESTED cgroup proofs (`LinuxCloneAbortProof.CgroupAbortProof`), not
  on a live false-accept run, and the third and fourth replay rows show the admitted direction
  reaching the next check instead. Those mutant inputs pass `Validate` and are distinct from the
  deliberately invalid one-field rows the clause mutants use.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`, the toolchain the
module cache pins) from the `daemon/` directory of the delivered worktree, on the *delivered* bytes.
Each mutant is the delivered file with one line changed in a scratch copy of `daemon/`, identified
by its own sha256 in the appendix below; the delivered file's digest is §1's `96306f49…` and was
re-verified by `sha256sum` after every mutant run. The earlier stage-2 table for `9c36f90f…` is
SUPERSEDED (§1).

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Green, all three tests, delivered bytes | `go test ./e2e -run 'TestLinuxV2HeldStageBindsThePreEffectAbortDisposition\|TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape\|TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage' -count=1 -v` | 0 | 3 parents and all 16 subtests PASS, including `postCreateClaimedAbsence`, `reservation_durable accepts its own pre-mkdir tombstone`, `helper_bound accepts its own removed-empty tombstone` and the two replay rows that reach the next check |
| Red `createStarted` | mutant M1 (§ appendix) | 1 | `:1638: mutated pre-mkdir tombstone error = <nil>, want "pre-create abort tombstone reports a target or leaf creation"` |
| Red `helper` | mutant M2 | 1 | the same `:1638` line, red at the `helper` row |
| Red `helperExit` | mutant M3 | 1 | the same `:1638` line, red at the `helperExit` row |
| Red `claimedAbsent` | mutant M4 | 1 | the same `:1638` line, red at the `claimedAbsent` row |
| Red, old clause polarity | mutant M5 | 1 | `:1595: the persisted pre-mkdir tombstone was refused: pre-create abort tombstone reports a target or leaf creation` |
| Red, binding not called | mutant M6 | 1 | the two replay rows, `helper_bound replay carrying a valid pre-mkdir tombstone` and `reservation_durable replay carrying a valid removed-empty tombstone`, each with the clone-abort message (`:1762` in the mutant) |
| Red, binding neutered | mutant M7 | 1 | the three held-stage refusal rows plus those two replay rows (`:1766` in the mutant) |
| Red, authority-valid post-create | mutant M8 | 1 | `postCreateClaimedAbsence` alone, `:1649: a create-started tombstone was not refused with the target/leaf-creation message: <nil>` |
| Neighbour predicates | `go test ./e2e -run 'TestLinuxV2' -count=1` | 0 | `ok … daemon/e2e 0.747s` |
| Whole `e2e` package | `go test ./e2e -count=1` | 0 | `ok … daemon/e2e 1.422s` |
| Related packages | `go test ./internal/state ./internal/authority ./internal/app -count=1` | 0 | `ok … 12.915s`, `ok … 0.032s`, `ok … 158.297s` |
| Race pass (CI home) | `go test -race ./e2e -run 'TestLinuxV2HeldStageBindsThePreEffectAbortDisposition\|TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape\|TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage' -count=1` | 0 | `ok … daemon/e2e 1.351s` |
| Producer-shape pin | `go test ./internal/state -run '^TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence$' -count=1` | 0 | `ok … 0.206s` |
| Authority-shape pin | `go test ./internal/authority -run '^TestLinuxCgroupReservationAbortProofRequiresExactPreEffectEvidence$' -count=1` | 0 | `ok … 0.008s` |
| Vet (WSL) | `go vet ./e2e` | 0 | no output |
| Vet (Windows) | `go vet ./...` (`daemon/`) | 0 | no output |
| Format | `gofmt -l e2e/production_linux_v2_owner_cli_test.go e2e/production_linux_v2_native_witness_test.go` | 0 | no output |

The mutant rows are the executed control for every refusal this round claims. M5 is the
un-satisfiability proof for §2 and §3 (the *old* clause refuses the proof the *production* producer
persists, which is exactly the `:224` stop). M1-M4 show each never-created conjunct is pinned by its
own row: removing one conjunct reddens only that row, and the other rows stay green. M7 (the binding
function returns nil) reddens the three held-stage refusal rows, and M6 (the binding call deleted)
reddens the two replay rows — so the wrong-stage refusals come from the binding and not from a
blanket rejection; in both mutants the two *accept* directions keep running to the next check. M8
(`abort.CreateStarted || abort.LeafNeverAppeared` → `abort.CreateStarted != abort.LeafNeverAppeared`)
reddens only the restored `postCreateClaimedAbsence` row: M8 was green in round 2 (the round-2 bytes
carried no row over the authority-valid `(true,true)` shape) and reds only the restored row in round
3.

### Independent reproduction (not this round's receipt)

An independent panelist (its transcript's first `"model"` entry is
`tencent-litellm/deepseek-v4.1-flash-fast`) reproduced, in WSL Noble on the *pre-fix* delivered
bytes (`9c36f90f…`, before this round): the delivered bytes PASS; the polarity-restoring mutant
`64c007dc16818d814fa6d092baa53e10275f02715e64a1daf254270545680aca` red at its `:1572` (exit 1); the
conjunct-deletion mutant `3bcb1ecda3c62bb43fcaff7da298a83ea7bd75f61bf2c38a6394cdfbce339698` red at
its `:1589` (exit 1); `GOOS=linux GOARCH=amd64 go vet ./e2e` exit 0; gofmt clean. Those digests and
line anchors belong to the pre-fix bytes and are SUPERSEDED here: M5 and M4 repeat those two
mutations on the delivered bytes, with this round's line anchors.

### Mutant appendix (red-capability controls)

Each mutant was run with `go test ./e2e -run 'TestLinuxV2HeldStageBindsThePreEffectAbortDisposition\|TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape\|TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage' -count=1`
from the scratch copy's `daemon/` directory, on a copy of the delivered file; `cp` of the delivered
file restored it between runs. Parenthesised line numbers are the delivered file's (`:1530` clause,
`:1508-1525` binding, `:1765` replay assertion). Every digest below is the round-3 bytes; the
round-2 appendix's M1-M7 digests gated `dba2daec…` and are superseded (the same one-line mutations,
new context).

- `81771c47d550b635d749e15c1106610d50c01c53b18e8758b8b2ff68d85db678` — M1, clause `:1530` without
  `abort.CreateStarted || `; reddens the `createStarted` row (exit 1).
- `6b3d6a10f1c2920df9af1dce7c96622c0c076678c5b2ec0acb17f872c9293710` — M2, clause `:1530` without
  `abort.Helper != nil || `; reddens the `helper` row (exit 1).
- `364dc13e6eea97189a63148ebd25f0c6a2cbfd34e8357585ce3ecdde9041a4e5` — M3, clause `:1530` without
  ` || abort.HelperExit != nil`; reddens the `helperExit` row (exit 1).
- `f42fe9609dfe1cdc4d14d240a56d3d787a3f9310ba9c2e5124747f4e2b804c2a` — M4, clause `:1530` without
  `abort.LeafNeverAppeared || `; reddens the `claimedAbsent` row (exit 1).
- `692567c695d89a511ece3eead6eae517b726845980bba25143eea3bc3f0285e4` — M5, clause `:1530` with the
  old polarity `!abort.LeafNeverAppeared`; reddens the green baseline row (exit 1).
- `19165c8ed742150812aba619c76cb84e2bd6b764a8a978fd0165a894dd7995d5` — M6, the
  `linuxV2ValidateHeldStageAbortDisposition` call deleted from `:1448-1451`; reddens the two replay
  rows (exit 1).
- `46dc473937c238ba7f1ad8d1bfbf5a82bc9c9c7dfd16edfaa60135d5e1e41826` — M7, `:1508` returning nil
  before the switch; reddens the three held-stage refusal rows and the two replay rows (exit 1).
- `1c5c642e07ad6088a5c543b42faa68422e6b8aebc8b348d3d7e7ebf1d38281d2` — M8, clause `:1530` with
  `abort.CreateStarted != abort.LeafNeverAppeared` replacing the first two conjuncts; reddens
  `postCreateClaimedAbsence` alone (exit 1).

The command suggestions in the contract (a replay-only run, or a pre-#308 run, with the pi deferral
removed) could not be executed here: the leg runs only inside the CI guest fixture of
`linux-native-v2-acceptance.yml` (root, systemd-nspawn, a debootstrapped guest, a built Control
release, users A/B), which this workstation cannot synthesize; and the deferral is a fixture
behaviour that cannot change a journal the daemon writes afterwards. The replacement proof is §2's
static contradiction plus this table's executed rows.

## 6. Which userland each property needed (standing rule)

The executed rows are pure Go logic over exported fixtures: no permission, ACL, mount or image
resolution is involved, so Noble is a sufficient userland for them and the runner-mirroring
requirement (rule 17) does not apply to a row whose outcome does not depend on the environment. The
leg itself keeps its unchanged userland: `linux-native-v2-acceptance.yml` runs on `ubuntu-latest`
with the Resolute guest rootfs the fixture builds; no new assertion, pipeline step or exported file
was added there, so no new runner-userland red/green was required before the PR. The changed clause
and the held-stage binding consume only `authority.LinuxCgroupReservationAbortProof` bytes and the
held stage name from the persisted journal, which are identical on every userland; the new rows in
§5 are pure Go logic over those bytes and a probe-free fixture, with no permission, mount or image
resolution.

## 7. Does the conductor's Bind execute it?

No. The three new tests live in `daemon/e2e` and run in the `daemon` job's race pass
(`go test -race -count=1 -timeout 600s -json ./...`, `.github/workflows/ci.yml:1055-1063`) and
locally; the `shared` Bind does not invoke `./e2e` with these test names. What the Bind *does*
execute is the fixed clause through `TestProductionLinuxV2StageCrashReplayMatrix`, and — from the
round-2 head onward, the first head that carries `linuxV2ValidateHeldStageAbortDisposition` — that
leg's binding accept direction as well. Round 1 (`46037316453`, parent `f84332e4`) is the
POLARITY-only observation: it gated the pre-fix blob `9c36f90f…`, which has the corrected clause but
not the binding symbol, so its `reservation_durable` pass (`verified
TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`) demonstrates
the clause, not the binding. The BINDING was demonstrated from round 2 (`46041720955`, parent
`db8f6047`) and round 3 (`46045331565`, parent `c9a4bbf9`) onward, where the same row is green on
bytes that carry the binding and its call. The final head stays PENDING until the conductor's
protected credentialed dispatch reports (§9); the round-3 scratch Bind is a scratch receipt, not that
dispatch. The `helper_bound` accept arm was NOT reached in either historical run: its stage row stops
at the retained-journal fixture guard (`linuxV2RequireSafelyReleasedPriorJournal`,
`production_linux_v2_native_witness_test.go:2099`, reported at `:224`), and neither the round-1 nor
the round-2 log carries a `verified` row for any `helper_bound` case, so a repeat of that guard on the
final head is an expectation until the conductor's Bind reports it (§9). The binding's two refusal
arms need a journal whose disposition contradicts the held stage; no case in the Bind's set produces
one, so those rows are attributed to the `daemon` job's race pass (and to §5's local rows), not to the
Bind. The fix also reloads no fixture, so the Bind needs no new step. The arm's later legs and the
export/reader tail stay outside this goal's proof, as they were before this increment.

## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above | see §5 (`daemon/`, WSL Noble) | per §5's Exit column | as tabulated: the eight mutant rows exit 1, the rest exit 0 |
| Full Go suite, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./...` (`daemon/`) | 0 | 30 rows: 27 `ok`, 3 `[no test files]`, no `FAIL`; the changed Go files are `//go:build linux && amd64`, so Windows compiles none of them |
| Full Go suite, WSL Noble as root (`go1.27.0 linux/amd64`) | `go test -count=1 ./...` (`daemon/`) | 1 | 34 rows: 30 `ok` including `daemon/e2e` (2.696s) and `native/linux-gate/protocol` (13.754s), 3 `[no test files]`, 1 `FAIL` — `internal/platform`, 13 rows of `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`. The round-2 second failure (`native/linux-gate/protocol`, missing `linux/capability.h`) did not reproduce in this run |
| Attribution of the WSL failure (base) | the same package in an extracted `ac04f3e2` `daemon/` tree (its `internal/platform/anchor_shape_table_test.go` sha256 `1f6b070a…` equals the head's) | 1 | identical: 13 rows of the same root-uid message at `anchor_shape_table_test.go:251`. Neither `internal/platform` nor `native/linux-gate/protocol` is inside the six-path changed scope |
| Comment cap | the coordinator's owner comment-policy checker (`comment-cap.py`, its own scratch install) on the scratch commit carrying the delivered bytes, base `ac04f3e2` | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 6 changed files, 1803 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new; notices that the 5845-line Go test file and the 5857-line `CLOSED-TODO.md` carry added lines jscpd did not scan, both above the pinned `--max-lines 5000` ceiling |
| Changed-file duplication, raised ceilings | the same script with `--max-lines 30000 --max-size 8mb` (scratch copy inside the clone) | 0 | 6 changed files, 1810 scanned sources, 1404 clones, 0 tolerated among the added lines, none new, no notice — every changed file was scanned |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 1 changed record passes the root-only check |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 1 changed document passes the path and symbol check (739 path citations; the notes are pre-existing ignore-rule notes in `CLOSED-TODO.md`) |
| Shell syntax | not applicable: no `sh`/`py`/workflow/exported file changed in this round | — | — |

The comment cap and the duplication gate read the *committed* diff, so both ran on a scratch clone of
this branch carrying the delivered bytes, with the pinned jscpd install symlinked in (no install was
run). Their gate scope is the six paths of `git diff --name-status ac04f3e2..c9a4bbf9` — the owner CLI
test, the native witness test, the plan, this record, `docs/TODO.md` and `docs/archive/CLOSED-TODO.md`
— of which §1's table lists four rows naming five paths and omits the native witness path, whose
delivered digest is in §11 (`98d4771c…`). Every round-3 gate that reads the committed diff ran on a
scratch clone commit of this branch's delivered bytes, not on the PR commit: the predecessor scratch
commit `2809fb63ca55…` carried the delivered `daemon/e2e/production_linux_v2_owner_cli_test.go` blob
(§1 `96306f49…`) and plan blob (§1 `ea3f176f…`) with the round-3 record text *preceding this final
append*, so it is NOT the final gate. The commands, exit status and counts in the table above are the
receipt for the record's final bytes; the exact scratch revision that carried those final bytes is the
PR #309 head, reported in the fix-commit report outside this record — a commit cannot name its own
hash, and this record does not claim that final-byte revision as verified inside itself. No earlier
round's receipt named a scratch revision for these gates, so such a revision is never inferred from a
matching base or from content digests — those rows are labelled base-plus-digests evidence only. The
gate therefore binds the base `ac04f3e2`, that six-path scope, the predecessor scratch commit
`2809fb63ca55…` for the Go and plan blobs, and the §1/§11 content digests. The full-suite rows ran on
the delivered Go bytes: Windows compiles the changed files out entirely, and the WSL runs execute them
inside `daemon/e2e` (whose `ok` row appears in both). The one WSL failure is a host precondition (a
root uid) in a package this range does not touch, reproduced at the dispatch base in the same
environment.

## 9. Next Bind

The binding receipt must be the conductor's re-Bind on the post-fix bytes: this round changes the
`daemon/e2e/production_linux_v2_owner_cli_test.go` blob (§1 `96306f49…`) and the plan, so no run that
gated `9c36f90f…` or `dba2daec…` can bind it.

- Hosted run `46020141750` (head `ad8f6557ee75`) is BASELINE ONLY: the witness blob at that run was
  `751eaf4d5e4d29852b4fa23e4cec2be85a5b524ad5705f40e3cd2fc322a749a7` against `9c36f90f…` at the
  pre-fix head and `dba2daec…` at the round-2 head. It is history for the predecessor's failure, not acceptance
  evidence for these bytes.
- Hosted run `46024288348` (head `b46b8d89`) is BASELINE ONLY for the same reason: it carries the
  same stale witness blob.
- The scratch Bind `46037316453` (run head `8cee777f`, sole parent `f84332e4`) is PARTIAL and
  SUPERSEDED by this fix for the same reason: it gated the pre-fix Go blob. Its `Bind` section stays
  as the receipt for the pre-fix bytes and as the observation that the `:224` stop is cleared there.
- The round-2 scratch Bind `46041720955` (run head `c45a05bea0`, sole parent `db8f6047`) is PARTIAL
  and SUPERSEDED for the same reason: it gated the round-2 blob `dba2daec…`. Its section stays as
  the round-2 receipt and as the second observation that the `:224` stop is cleared there, with the
  same `helper_bound` fixture guard as its next stop.
- The round-3 scratch Bind `46045331565` (scratch head `7865bc17`, sole parent `c9a4bbf9`) is PARTIAL
  and SUPERSEDED as acceptance: it gated the delivered blob `96306f49…` on scratch bytes and still
  exits 1. Its `Bind (round 3)` section is the final SCRATCH receipt — the observation that the `:224`
  stop is cleared and `verified
  TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0` on the
  delivered bytes — not the acceptance.

ONLY the conductor's protected credentialed dispatch on the PR head is the acceptance for these
bytes: every run above is `shared` with the ratified placeholder credential, so none of them
substitutes for it, and the code bytes those dispatches gate are unchanged by this records-only
delta.

What the re-Bind must report: the `shared` case-set exit status, the `verified …` sequence and its
first disagreement, the `reservation_durable` leg's outcome, the credential-gated pi outcome verbatim
in log, declaration and exported member, the export status and the reader Result — plus the first
unexpected failure if the arm stops later in the replay matrix. The scratch Bind's first
disagreement is already recorded above (the `helper_bound` fixture guard at
`linuxV2RequireSafelyReleasedPriorJournal`, `production_linux_v2_native_witness_test.go:2099`,
reported at `:224` — not the fixed clause). The expected green outcome
of the fixed rows is in §5; the Bind is the acceptance, not this record.

## 10. Residuals and known limits

- The leg is CI-only: nothing here proves the `reservation_durable` leg passes on the delivered
  bytes. The clause's leg-level clearance is OBSERVED in all three scratch receipts — round 1
  (`46037316453`) on the pre-fix bytes for the clause alone, and rounds 2-3 (`46041720955`,
  `46045331565`) on bytes carrying the binding (`verified
  TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0` in each) — and on
  the final head it stays PENDING until the conductor's protected credentialed dispatch reports it.
  The next stop in the round-2 and round-3 receipts is the `helper_bound` fixture guard
  (`linuxV2RequireSafelyReleasedPriorJournal`, `production_linux_v2_native_witness_test.go:2099`,
  reported at `:224`: "pre-existing RunJournal … is not physically released and safe for a fresh
  run"), which this round does not touch and which that dispatch is expected to re-encounter; the
  ACK-loss matrix and the export/reader tail are unmeasured on any revision here.
- The binding refuses a wrong-stage disposition; it does not restore history a recovery lost. If a
  future recovery dropped the durable reservation while keeping a clone tombstone of the *other*
  hold's disposition, the binding refuses the pair rather than proving the recovery correct, and the
  missing history still has to be read from the retained journal.
- A tombstone that satisfied the *old* clause cannot exist, so the old clause's removal cannot hide
  a regression in the producer: the producer's shape is separately pinned by the state row and by
  the new e2e row, and both must move together with any future change of the fence semantics.
- If the `LeafNeverAppeared` rule in `internal/authority` is ever amended (it is not here), the
  `observed` row (`observed.LeafNeverAppeared = true` must fail `Validate`) reddens first, and the
  clause's own refusal of the same shape stays pinned independently by the `claimedAbsent` row —
  red-capable on its own per §5's deletion mutant, no longer only implied by `Validate`. The other
  authority-valid shape, `CreateStarted=true, LeafNeverAppeared=true`, is refused by the clause and
  pinned by `postCreateClaimedAbsence` (§5 M8): it is not the pre-mkdir fence and cannot satisfy the
  `reservation_durable` accept arm.
- The pi leg's credential-gated outcome is not proven by this goal and must not be reported greener:
  the `shared` arm still exits 1 and names the gated legs under the placeholder credential.

## 11. Anchor re-measurement (delivered bytes)

Every line anchor this record cites, re-read on the delivered bytes at the end of this fix round and
cited with that file's sha256; digests already in §1 are referenced there, not repeated. The
dispatch-base anchors of §1–§3 were re-read at `ac04f3e2` and are marked as such in place. Three
anchors of the earlier text were stale and are corrected above: the pre-mkdir policy comment is
`cgroup.go:140-141` (not `:138-139`), the producer pin spans `linux_clone_journal_test.go:256-260`
(not `:255-258`), and the never-created recovery branch starts at
`linux_owner_recovery_linux.go:139` (not `:140`). This round also re-measured every anchor it added
or moved, including the authority test's positive pre-mkdir check (`:231-233`, previously cited as
`:236-243`) and the producer pin including its observed-absence and no-authority lines (`:256-260`,
previously `:255-258`).

This round re-measured every anchor again on the round-3 bytes; the only Go edits are the single
proof selection in `linuxV2ValidateStageReplayJournal` (the clone re-selection deleted, `:1455-1460`
kept as validation) and the new `postCreateClaimedAbsence` row (`:1642-1651`).

The round-4 records round changed no Go byte and re-measured the anchors it added or touched on the
same delivered file (`96306f49…`): the clause/binding-reach anchors `:1448-1451`, `:1517-1520` and
`:1530`, the invalid-predicate rows `:1608-1632`, the valid controls `:1642-1651`, `:1654-1676` and
`:1712-1740`, and the extraction `:1495-1506` named in the new `docs/TODO.md` residual. The plan's
§3 anchors (`scripts/ci/linux-v2-native-acceptance.sh:3298-3300`, `:3301-3303`, owner CLI `:4689`)
were re-read unchanged.

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | §1 `96306f49…` | caller `:1404-1493`, binding call `:1448-1451`, extraction `:1495-1506`, binding `:1508-1525`, `helper_bound` refusal return `:1517-1520`, predicate `:1527-1542`, clause `:1530`, green baseline `:1594-1596`, clause rows `:1603-1633`, post-create row `:1642-1651`, removed-empty fixture `:1654-1676`, held-stage test `:1678-1710`, replay fixture `:1712-1740`, replay test `:1742-1769`, boundary arm `:1877-1879`; dispatch base `:1467`, `:1477`, `:1617-1620` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `98d4771cf6a3efafc45abab7a8c954a07e58b853858979a5435259e2e4c05d7e` | the `:224` subtest invocation that reports both stops, the retained-journal guard message `:2099`, the admitted-fresh call site `:599` |
| `daemon/internal/authority/cgroup.go` | `74743fc7601c889f0f2c5a26798fcc5585044c41200a0c0d741a89f4fa0fad78` | `never_created` rule `:381-384`, policy comment `:140-141` |
| `daemon/internal/authority/authority_v2_test.go` | `a0890aa2c5a097fb6c0712853dc94238bce385219fe4ba3df8977c64c66c5029` | positive pre-mkdir check `:231-233`, post-create-intent refusal `:235-237`, accepted never-appeared shape `:239-241`, other-intent `ValidFor` refusal `:242-244` |
| `daemon/internal/state/linux_cgroup_journal.go` | `9358c264623f719038ddcfecaab976f3f2e7fde82f28a6a9689662cd167d9b4d` | producer `:903-916`, policy comment `:898-902` |
| `daemon/internal/state/linux_clone_journal_test.go` | `dda264bcd004afbbed03801962bdbc79b6719660aa58bbc7c9def92301e84e2b` | producer pin `:256-260` |
| `daemon/internal/app/linux_owner_recovery_linux.go` | `6fb072a26495f1494c1bcc4bf199db95be41bed2a4eba0a2f4d9bcf28d92dfa5` | never-created branch `:139-146` |
| `.github/workflows/ci.yml` | `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7` | daemon race step `:1055-1063` |
| `docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md` | §1 `ea3f176f…` | §1.1 authority ranges `:231-233`/`:235-237`/`:239-241`/`:242-244`, producer pin `:256-260`, dispatch-base caller `:1467` and switch `:1475`; §1.2 dispatch-base boundary `:1617-1620` and delivered `:1877-1879`, recovery `:139-146`, policy `:140-141`; §2 the binding; §3 the delivered recovery mapping and the live consumers; §4 the rows |
| `docs/TODO.md` | — | the open Bind/residual entries; not byte-pinned (a later unrelated `main` merge moves it) |
| `docs/archive/CLOSED-TODO.md` | — | the closed stage items; not byte-pinned (entries older than six months may be deleted) |

## Bind

**PARTIAL and SUPERSEDED by the fix round in §1:** this receipt gated the pre-fix witness blob
`9c36f90f…`; the delivered blob is `96306f49…`. It remains the receipt for the pre-fix bytes and the
one observation that the `:224` stop is cleared there — the binding receipt for the delivered bytes
must be the conductor's re-Bind (§9).

[Run 46037316453](https://github.com/wxxb789/symmetry/actions/runs/46037316453) tested `shared` once with the ratified placeholder credential. The run head `8cee777f3f1ba18860d49a062b7816a7494717f1` has sole parent `f84332e4a90a15c61906fc20117d81c71b22e646` (PR #309). The scratch branch was deleted and its worktree removed; the head-filtered Actions response still reported `total_count=1`, attempt 1. The [evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46037316453/artifacts/15218976179) has SHA-256 `b6ddf5ab375748b37e05ffa54f4904e97deb9f2cda3847be0428c28dd3448296`, verified against the Actions digest.

| Run input | Run `8cee777f3` | PR `f84332e4` |
| --- | --- | --- |
| All PR-tracked paths | Identical tree entries, modes and blobs | PR tree |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | Added blob `d637df5de6d05b62c2668e8b6a47b107a7e3e71a`: `shared` only; push filter `scratch/bind-309` because the shared scratch ref was absent | Absent |
| `scripts/ci/scratch-gh-shim` | Added original blob `6c2598c146e85875c1868515280fcaee8bec110d`, unchanged from Bind #308 | Absent |

1. **Exit (log 4553):** `Case set shared exited 1`. There were 24 `verified … run=1 pass=1 skip=0` rows, the previous 22 plus the replay parent and `reservation_durable` subtest.
2. **Replay (3686–3687):** `verified TestProductionLinuxV2StageCrashReplayMatrix: run=1 pass=1 skip=0` and `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`. The entire log contains zero occurrences of `stage reservation_durable recovery did not preserve the correct physical Journal state`.
3. **Credential gate and export (3109, 4550):** `credential-gated: pi_provider_action (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.` The partial export has the same four members as Bind #308: `shared/credential-gated-legs.json` (807 B), `shared/pi-native-provider-action.json` (3224 B), `shared/stage-crash-replay-reservation_durable.json` (6644 B), `shared/unprovisioned-owner-refusal.json` (691 B). Full export still fails: `redacted evidence export failed: pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`.
4. **First failing witness (4265, `helper_bound`):** `witness output:     production_linux_v2_native_witness_test.go:224: pre-existing RunJournal bad57e1c-12a1-4ca8-922b-56f0e2f27334/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf`
5. **Manifest:** `shared/manifest.json` was not published. The reader started (2515); the `helper_bound` abort prevented the reader stop and Result check from running.

## Bind (round 2)

**ROUND 2 / ACT SUPERSEDED for protected acceptance, and PARTIAL/SUPERSEDED for the round-3 delivered bytes** (it gated the round-2 blob `dba2daec…`, which round 3 replaces with `96306f49…`).
[Run 46041720955](https://github.com/wxxb789/symmetry/actions/runs/46041720955) is the `shared` replay receipt on the round-2 PR bytes, not a protected credentialed dispatch or a pass for all of `shared`. [Run 46037316453](https://github.com/wxxb789/symmetry/actions/runs/46037316453) (`8cee777f`, parent `f84332e4`) is **ROUND 1 / PARTIAL / SUPERSEDED**, pre-fix evidence only. Both pushes used the ratified placeholder credential.

The round-2 [evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46041720955/artifacts/15220621570) (4625 B) has SHA-256 `27c89655a98e3ebd573313edfa3820eee40a8e99d0ec00f33a00689b31fd4e94`, verified against the Actions digest. Scratch run head `c45a05bea0dcc2b5e190946c336886063d1a97aa` has sole parent `db8f60472eff779bfc863cea25129dd1a37ad6fc`. The delivered head descends from the round-1 PR base `f84332e4` through `70d387df`.

Equality deltas since round 1 only: the four PR paths below changed between the two PR heads, but all delivered PR entries are byte-identical in round 2. The original shim addition (same mode and blob as above) is the only unchanged scratch-only row. Round 2 used the available shared scratch ref rather than round 1's fallback.

| Input | Pre-fix PR `f84332e4` | Delivered PR `db8f6047` | Run `c45a05bea0` |
| --- | --- | --- | --- |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | Absent | Absent | `3e85d193704205f9e1ba1736b13867f6727939cc` (blob); `shared` only, push filter `scratch/rf-acc-containment-goal-admission` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | SHA-256 `9c36f90f…` | SHA-256 `dba2daec…` (round 2) | Equals the round-2 PR |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | SHA-256 `d988ca3b…` | SHA-256 `98d4771c…` (§11) | Equals delivered PR |
| This evidence record and the goal plan | Pre-fix text | Fix-round text | Equals delivered PR |

The [acceptance job](https://github.com/wxxb789/symmetry/actions/runs/46041720955/job/140651188401) reported:

1. **Exit (4553):** `Case set shared exited 1`. All 24 green witness rows are identical and in the same order as in round 1.
2. **Replay (3687):** `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`. The old physical Journal state diagnostic did not appear.
3. **Next stop (`helper_bound`, 4263–4267):**

   ```text
   witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix
   witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix/helper_bound
   witness output:     production_linux_v2_native_witness_test.go:224: pre-existing RunJournal 0c9048d6-3150-4e1c-9ba8-7f3b1dad7b1c/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf
   witness output: --- FAIL: TestProductionLinuxV2StageCrashReplayMatrix/helper_bound (0.37s)
   witness output: --- FAIL: TestProductionLinuxV2StageCrashReplayMatrix (0.37s)
   ```

4. **Pi and export (3109, 4550):** `credential-gated: pi_provider_action (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.` Full export stopped at `redacted evidence export failed: pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`. The partial ZIP has the same members and sizes as round 1: `shared/credential-gated-legs.json` (807 B), `shared/pi-native-provider-action.json` (3224 B), `shared/stage-crash-replay-reservation_durable.json` (6644 B), and `shared/unprovisioned-owner-refusal.json` (691 B).
5. **First failure and manifest:** the first failing witness is the `:224` line above (4265), preceded at 4261 by `Go native acceptance event gate failed: bad=TestProductionLinuxV2StageCrashReplayMatrix=run:1,pass:0,skip:0; TestProductionLinuxV2StageCrashReplayMatrix/helper_bound=run:1,pass:0,skip:0, skipped=[], fail_events=3, invalid_json_lines=0`. No `shared/manifest.json` was published; the reader started, but its stop and Result were not measured.

## Bind (round 3)

**FINAL SCRATCH receipt, not the protected credentialed dispatch.** [Run 46045331565](https://github.com/wxxb789/symmetry/actions/runs/46045331565) ran `shared` once with the placeholder credential. The [evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46045331565/artifacts/15223010430) (4637 B) has download-verified SHA-256 `12a5c153bbd16b1dab4fd338bdcbcd3ca9810508bf364fcefcb497d58ac4495b`. Runs `46037316453` (`8cee777f`, round 1) and `46041720955` (`c45a05bea0`, round 2) are **PARTIAL/SUPERSEDED** pre-final receipts.

Scratch head `7865bc1706545a977c4c4208ff3d70553eb3b27a` has sole parent `c9a4bbf97d467d09c259ee23e481027ae9840e71` (PR #309). All PR entries match by blob and mode; the two scratch-only additions, including their modes, match round 2. Only PR inputs that changed since round 2 are listed (SHA-256):

| Input | Round-2 PR `db8f6047` | Final PR `c9a4bbf9` | Run `7865bc17` |
| --- | --- | --- | --- |
| `production_linux_v2_owner_cli_test.go` | `dba2daec…` | `96306f49…` (§1) | Same as final PR |
| This record (before this append) | `221153b6…` | `17d8bd5e…` | Same as final PR |
| Goal plan | `c73df5b3…` | `ea3f176f…` (§1) | Same as final PR |

[Acceptance job](https://github.com/wxxb789/symmetry/actions/runs/46045331565/job/140663396378), raw log lines:

1. **4517:** `Case set shared exited 1`. 24 green witness rows (`run=1 pass=1 skip=0`), same rows and order as round 2.
2. **3651:** `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`. The old physical Journal state diagnostic did not recur.
3. **4227–4231, next stop:**

   ```text
   witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix
   witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix/helper_bound
   witness output:     production_linux_v2_native_witness_test.go:224: pre-existing RunJournal 9588eb0e-8ab6-4570-9341-494cc59e9218/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf
   witness output: --- FAIL: TestProductionLinuxV2StageCrashReplayMatrix/helper_bound (0.50s)
   witness output: --- FAIL: TestProductionLinuxV2StageCrashReplayMatrix (0.50s)
   ```

4. **3079:** `credential-gated: pi_provider_action (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.` **4514:** `redacted evidence export failed: pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`. The ZIP contains `shared/credential-gated-legs.json` (807 B), `shared/pi-native-provider-action.json` (3223 B), `shared/stage-crash-replay-reservation_durable.json` (6644 B), and `shared/unprovisioned-owner-refusal.json` (691 B).
5. **4225:** `Go native acceptance event gate failed: bad=TestProductionLinuxV2StageCrashReplayMatrix=run:1,pass:0,skip:0; TestProductionLinuxV2StageCrashReplayMatrix/helper_bound=run:1,pass:0,skip:0, skipped=[], fail_events=3, invalid_json_lines=0`. The first failing witness is line 4229 above. The ZIP has no `shared/manifest.json`; the reader started at 2515, but its stop and Result remain unmeasured.
