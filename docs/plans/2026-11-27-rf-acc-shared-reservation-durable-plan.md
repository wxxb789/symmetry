# rf-acc-shared-reservation-durable — plan (stage-1 read and the stage-2 fix)

Contract: `goal-rf-acc-shared-reservation-durable.md` (GDD contracts root, ruled 2026-11-27;
dispatch gate `ac04f3e2` = #308). Branch `refactor/rf-acc-shared-reservation-durable`, dispatch
base `ac04f3e2ec8b48c9f28a6f7ed66f9abc87a3882a`. Shape reference:
`docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md` with its stage-2 plan. This plan fixes only
what the accepted plan leaves open: which layer the wall belongs to, the exact shape the
`reservation_durable` leg must demand, the committed test that pins it, and the evidence plan.

## 1. Stage 1 — the wall, from evidence

### 1.1 The three bytes that disagree

The stop is
`production_linux_v2_native_witness_test.go:224: stage reservation_durable recovery did not preserve
the correct physical Journal state: pre-create abort tombstone reports a target or leaf creation`
(raised from `production_linux_v2_owner_cli_test.go:1477`). Three committed bytes decide it:

| Layer | Byte | Dispatch base (`ac04f3e2`) |
| --- | --- | --- |
| Witness expectation | `abort.CreateStarted \|\| !abort.LeafNeverAppeared \|\| abort.Helper != nil \|\| abort.HelperExit != nil` | `production_linux_v2_owner_cli_test.go:1477` |
| Authority validator | `never_created` requires `proof.LeafNeverAppeared == proof.CreateStarted` | `internal/authority/cgroup.go:383` |
| State producer | `CreateStarted: false`, `LeafNeverAppeared: false`, `LeafAbsent: true`, `Validated` | `internal/state/linux_cgroup_journal.go:903-916` |

`linuxV2ValidateStageReplayJournal` calls `abort.Validate()` first (dispatch base `:1467`); a proof
that fails it returns the other message. So the switch at `:1475` (dispatch base) only sees proofs with
`LeafNeverAppeared == CreateStarted`, and every such proof fails the clause:

- `CreateStarted=false` ⇒ `LeafNeverAppeared=false` ⇒ `!abort.LeafNeverAppeared` is true;
- `CreateStarted=true` ⇒ `abort.CreateStarted` is true.

The demanded combination `CreateStarted=false && LeafNeverAppeared=true` is not one of the two
legal `never_created` shapes; `internal/authority/authority_v2_test.go:231-244` pins them (the
positive pre-mkdir `Validate`/`ValidFor` check at `:231-233`; "post-create-intent proof inferred
never-created from path absence alone" refused at `:235-237`; `CreateStarted=true,
LeafNeverAppeared=true` accepted as the "exact never-appeared proof" at `:239-241`, and its
`ValidFor` refusal for another creation-intent state at `:242-244`). The
producer's pre-mkdir shape is itself pinned by
`internal/state/linux_clone_journal_test.go:256-260`. The clause can therefore never pass: it is a
dead assertion that reddens any journal reaching it.

### 1.2 The `reservation_durable` boundary requires the pre-mkdir shape

`linuxV2ValidateStageJournalBoundary` (dispatch base `:1617-1620`; delivered `:1871-1874`) refuses the held marker unless
`reservation.CreateStarted == false`, no leaf identity and no helper. Recovery then takes
`linux_owner_recovery_linux.go:139-146` (`!reservation.CreateStarted` →
`abortNeverCreatedLinuxV2Reservation`) and persists the fence-inferred pre-mkdir tombstone whose
policy is stated at `internal/authority/cgroup.go:140-141` and `internal/state/linux_cgroup_journal.go:898-902`:
exclusive durable creator fence, no filesystem observation, no stop receipt, no leaf removal
authority. The recovery is exactly the designed behaviour; the witness clause contradicts it.

### 1.3 New or long-standing

Long-standing, and unsatisfiable since it was written. `git grep` at `41e64851` (#9,
2026-10-13) shows the same three bytes already in one commit — clause
`production_linux_v2_owner_cli_test.go:1315`, rule `internal/authority/authority.go:1251`,
producer `internal/state/state.go:2724`, plus the two pins (`authority_v2_test.go:237`,
`linux_clone_journal_test.go:265`). `git log -S` finds only the move-only refactors after it
(`9ac85ac0` #158 authority split, `ff18add1` #255 state split); no commit changed any of the three
tokens. So no revision since #9 could pass the leg, and no earlier run ever did.

#308 contributed nothing to the Journal state: its diff (`git show --stat ac04f3e2`) touches only
`internal/harness/pi`, `scripts/ci/*`, docs and the acceptance script. Its pi-leg deferral changed
*when* the `shared` arm reaches the replay matrix, not what the daemon persists; the earlier stop
happened at the pi provider leg, before the replay invocation. The wall is the witness's own
contradiction, exposed by the deferral.

Replacement proof for the leg runs the contract suggests (a replay-only or pre-#308 run): the leg
executes only inside the CI guest fixture (`scripts/ci/linux-v2-native-acceptance.sh` `shared` arm
under `linux-native-v2-acceptance.yml`; root, systemd-nspawn, a debootstrapped guest, a Control
release), which this workstation cannot synthesize. The un-satisfiability above is a static
property of the tree, and §4 executes it: the new unit row runs the *production* tombstone through
the *old* clause and reddens, then through the fixed clause and passes.

### 1.4 Ruling

The wrong layer is the witness's expectation
(`daemon/e2e/production_linux_v2_owner_cli_test.go:1477`). No production daemon/platform defect is
claimed, so no `DECISION_NEEDED`: the authority validator, the state producer and their two pins
agree with each other, with the boundary, and with the pre-mkdir recovery; only the witness asks
for a proof the contract forbids.

## 2. Stage 2 — the fix

- Extract the disposition switch into `linuxV2ValidatePreEffectAbortTombstone`
  (`production_linux_v2_owner_cli_test.go`) so the two pre-effect tombstone shapes are testable as a
  predicate; `linuxV2ValidateStageReplayJournal` keeps its exact error text and message identity.
- `never_created` must be the pre-mkdir fence shape: `!CreateStarted && !LeafNeverAppeared` and no
  helper/helper-exit. The two conjuncts that `Validate` already implies stay explicit, matching the
  sibling `removed_empty` clause's style; the one that contradicts `Validate` is the defect.
- The predicate alone accepted either pre-effect disposition at any `aborted_no_target` hold, so
  `linuxV2ValidateStageReplayJournal` takes the held `stage` and binds the disposition to it
  (`linuxV2ValidateHeldStageAbortDisposition`): `reservation_durable` may present only the
  `never_created` fence shape, `helper_bound` may present only the `removed_empty`/
  `absent_after_rmdir` shape, and any other held stage presenting a no-target tombstone is refused.
  The call sites pass the held barrier stage they already validate against
  `linuxV2ExpectedStageReplayRecovery`.
- Nothing else changes: `Validate`, the producer, the tombstone semantics, the boundary validators,
  the replay matrix's stage list and the acceptance script stay byte-identical. The `shared` arm's
  pi-leg credential gate (ruling 1B1) is untouched, so the gated outcome is recorded exactly as
  before.

## 3. Edge cases the contract demands

- `linuxV2ExpectedStageReplayRecovery` maps exactly two holds to `aborted_no_target`:
  `reservation_durable` (only the `never_created` fence shape may satisfy it) and `helper_bound`
  (only the `removed_empty`/`absent_after_rmdir` shape with its helper reap evidence may satisfy
  it). `create_started`, `leaf_bound` and `launcher_bound` recover `unresolved`; `gate_bound` and
  later recover `released`. No other leg consumes the changed conjunct or the new binding.
- A tombstone that claims the create CAS advanced (`CreateStarted=true`) or a helper/helper exit
  stays refused by the clause; the accepted `CreateStarted=true, LeafNeverAppeared=true` shape is
  not the pre-mkdir fence and is refused here too. A valid `removed_empty` proof is refused at
  `reservation_durable` and a valid pre-mkdir proof at `helper_bound`, each with its own message.
- The leg's remaining assertions (clone-abort proof, RunKey/Subject binding, `ValidFor` against the
  durable reservation) are unchanged; the fix removes the only stop the leg was observed to raise
  and cannot make an earlier one pass.

## 4. Tests and evidence plan

| New behaviour | Committed red-capable test | Runs where |
| --- | --- | --- |
| The pre-mkdir tombstone the state producer writes for a never-created reservation satisfies the stage-replay clause | `TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape` in `daemon/e2e`, driving `state.Store.AbortNeverCreatedLinuxCgroupReservation` | `daemon` job race pass (`go test -race ./...`), local WSL |
| The clause still refuses a target/leaf-creation tombstone (`CreateStarted`, helper, helper exit, claimed absence: one field per row) | same test, red rows over a mutated copy | same |
| Each hold presents only its own valid disposition, and no other hold may present a no-target tombstone | `TestLinuxV2HeldStageBindsThePreEffectAbortDisposition` (predicate) and `TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage` (the replay validator the leg calls) | same |
| The old clause text rejects that same production tombstone | scratch-clone revert of the one token, same test | local WSL, recorded; not committed |

Evidence plan: run the new row and the reverted-clause row in WSL with exact exit codes; run the
neighbor predicate rows, `go vet`, `gofmt`, the comment cap and the changed-file duplication gate;
record the residue: the leg itself is CI-only, so the record names the conductor's next `shared`
Bind as the acceptance and lists the residual risk if a later assertion in the leg stops it.

## 5. Out of scope, with reasons

- `internal/authority`, `internal/state`, `internal/app`: no defect found there; changing the
  `LeafNeverAppeared` rule would rewrite a contract the state and authority pins fix and would
  invalidate committed tests.
- The acceptance script's arms and the pi credential gate: the fix needs no fixture change, and a
  script edit would add a pipeline step that must be proven red/green in the CI runner's userland
  before the PR opens.
- The other replay holds, the ACK-loss matrix and the negative matrices: untouched; none of them
  reaches the `never_created` clause.
