# rf-acc-shared-helper-bound-journal — plan (stage-1 read and the stage-2 fix)

Contract: `goal-rf-acc-shared-helper-bound-journal.md` (GDD contracts root, ruled 2026-11-28;
dispatch gate `3d377582` = #309). Branch `refactor/rf-acc-shared-helper-bound-journal`, dispatch base
`3d377582065255b47531861701967a407581d74d`. Shape reference:
`docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md` with its plan. This plan fixes only
what the accepted plan leaves open: which layer the wall belongs to, the exact shape the freshness
guard must accept, the committed tests that pin it, and the evidence plan.

## 1. Stage 1 — the wall, from evidence

### 1.1 The stop, and the one journal it names

Three `shared` Binds of #309 report the same guard at the `helper_bound` subtest, on three different
RunIDs (`bad57e1c-…`, `0c9048d6-…`, `9588eb0e-…`, #309 record §7/§9/§10):

```text
production_linux_v2_native_witness_test.go:224: pre-existing RunJournal <uuid>/1 is not physically
released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf
```

`:224` is the `t.Run` closure of `TestProductionLinuxV2StageCrashReplayMatrix`
(`daemon/e2e/production_linux_v2_native_witness_test.go:218-227`); the message is raised one helper
deeper, in `newLinuxV2NativeFixture` (`:2093-2101`, message `:2099`), from
`linuxV2RequireSafelyReleasedPriorJournal` (`daemon/e2e/production_linux_v2_owner_cli_test.go:2585-2606`).
The guard runs *before* the leg touches the daemon: it is a precondition on the shared StateDir.

Control issues a fresh RunKey per leg (`scripts/ci/provision-linux-v2-control-claims.exs`, schedule
mode, `Ecto.UUID.generate()`), so no leg reuses another's journal, and the arm runs the legs in the
order `reservation_durable helper_bound gate_bound …`
(`scripts/ci/linux-v2-native-acceptance.sh:3296-3300`). The `reservation_durable` leg passed the same
guard in all three runs (24 green rows, #309 record §7), so the only journal the guard can be
refusing at `helper_bound` is the tombstone that `reservation_durable` left one leg earlier.

That leg's retained shape is fixed by its own validators: recovery is `aborted_no_target`
(`linuxV2ExpectedStageReplayRecovery` `:1153-1162`), and `linuxV2ValidateStageReplayJournal`
(`:1404-1493`, `aborted_no_target` arm `:1433-1464`) requires the tombstone to be *retained* with no
release proof, no process details and no authority, plus `linuxV2ValidateHeldStageAbortDisposition`
(`:1508-1525`) binding `reservation_durable` to the `never_created` fence shape. #309's producer is
`state.Store.AbortNeverCreatedLinuxCgroupReservation`
(`daemon/internal/state/linux_cgroup_journal.go:903-916`), whose policy is stated at `:898-902`: the
tombstone "remains until journal deletion so an unknown write can be replayed with the same proof".
Retention is designed behaviour, not a leak.

### 1.2 Which layer is wrong

Three bytes decide the question; two of them agree with each other:

| Layer | Byte | Verdict on the tombstone |
| --- | --- | --- |
| Daemon State admission | `linuxV2ForeignJournalIsSafelyRetired`, `daemon/internal/state/linux_cgroup_journal.go:432-465`, abort arm `:452-456` | **retired**: `noActiveMarkers && no release/pre-gate proof && reservation.Validate() == nil` — a fresh run may proceed beside it |
| Daemon State vocabulary | `HasPendingContainment`, `daemon/internal/state/run_journal.go:101-107` | not a pending owner: the reservation term is `ContainmentCgroup != nil && AbortProof == nil` |
| Witness freshness guard | `linuxV2RequireSafelyReleasedPriorJournal`, `daemon/e2e/production_linux_v2_owner_cli_test.go:2585-2606` | **refuses**: `journal.ContainmentCgroup != nil` (`:2587-2590`) plus the v2 StopReceipt release demand (`:2592-2598`) |

The guard asks the daemon's own question — "can a prior RunJournal still own a process or leaf?" —
but answers it from a *subset* of the daemon's arms: only the physical-release arm. A pre-effect
abort has no target and no stop, so it can never carry a `StopReceipt`; the guard therefore refuses
exactly the state the product retires as safely retired. The wrong layer is the witness's
expectation, **`daemon/e2e/production_linux_v2_owner_cli_test.go:2585-2606`**, contradicted by
`daemon/internal/state/linux_cgroup_journal.go:452-456`.

No production daemon/platform defect is claimed, so no `DECISION_NEEDED`: the authority
(`LinuxCgroupReservation.Validate` → `linuxCgroupReservationAbortMatches`,
`daemon/internal/state/linux_cgroup_journal.go:50-74`, `:157-176`, `:1094-1131`), the producer
(`:903-916`) and the admission scan (`:432-465`) agree; only the fixture's copy of the rule is
stale.

### 1.3 New or long-standing

Long-standing since #9 (`41e64851`, 2026-10-13), by construction, not by regression:

- `git show 41e64851:daemon/e2e/production_linux_v2_owner_cli_test.go` shows the guard with today's
  conjuncts and today's message (`:2085-2106` at that revision), and
  `git show 41e64851:daemon/internal/state/state.go` shows
  `linuxV2ForeignJournalIsSafelyRetired` (`:450-475` there) **already** carrying the
  `ContainmentCgroup.AbortProof` arm (`:464-467`);
- `git log -S "may still own or require recovery of a process or leaf" -- daemon/e2e` returns only
  `41e64851`: the guard's text has not changed since;
- `git log -S "reservation_durable helper_bound" -- scripts/ci/linux-v2-native-acceptance.sh`
  returns only `41e64851`: the shared arm's replay order has not changed either.

The contradiction was unobservable, not absent: until #309 the `reservation_durable` leg could not
pass at all (its own clause contradicted `authority.Validate` — #309's record §2), so no leg ever
left a tombstone behind *and* succeeded. The contracts' suggested observation (run only the replay
legs, or the `helper_bound` hold, on a pre-#309 tree) is not available on this workstation: the leg
runs only inside the CI guest fixture (`linux-native-v2-acceptance.yml` → a debootstrapped guest,
root, systemd-nspawn, a built Control release). The replacement proof is the structural one above
plus the three CI observations of the same guard on three heads recorded in #309's record §7/§10.

### 1.4 The fixture's own setup (the contract's first alternative)

The fixture neither releases nor reuses the preceding leg's journal, and it must not:

- each leg gets a fresh RunKey from Control (above), so no leg adopts another's journal;
- the two `aborted_no_target` legs *require* their retained tombstone
  (`linuxV2ValidateStageReplayJournal` `:1433-1464`);
- the acceptance script deletes only `request.json`/`replay_request.json` between legs (`:2648`,
  `:2717`) never a journal;
- the StateDir is fixed by the root-installed OwnerConfig and asserted equal to it
  (`newLinuxV2NativeFixture` `:2078-2091` fails unless `stateDir == owner.StateDir`; the arm pins the
  installed path through its own `DAEMON_SERVICE_STATE_DIR`,
  `scripts/ci/linux-v2-native-acceptance.sh:520`), so per-leg StateDir isolation is not available to
  this arm, and deleting the tombstone in the fixture would delete durable control state the product
  keeps for replay.

Precedent for the other direction exists and does not apply: #300 moved the alternate-mount refusal
into its own case set because its leftover claim-only row is one "**no product path retires**"
(`scripts/ci/linux-v2-native-acceptance.sh:3031-3033`). The abort tombstone *is* retired by a product
path, and moving the two abort legs out of the shared arm would drop their sequence coverage, which
the contract forbids.

### 1.5 Ruling

Fix the witness's expectation. Keep every physical-release requirement, keep the daemon's ownership
semantics untouched, and make the guard accept exactly the retirement shapes the shared arm's own
legs can leave, derived from the same evidence the daemon's admission scan uses.

## 2. Stage 2 — the fix

`linuxV2RequireSafelyReleasedPriorJournal` (`production_linux_v2_owner_cli_test.go`) becomes a
dispatcher in the daemon's own arm order, with its physical-release arm unchanged:

1. owner-marker refusal — `HasProcessDetails`, `HasPendingContainment`, `ContainmentUnproven`,
   `ContainmentAuthority`, `ContainmentHandoff`, one literal message
   (`linuxV2PriorJournalOwnershipRefusal`);
2. abort tombstone → `linuxV2RequireRetiredPriorAbortTombstone`: `ContainmentCgroup.AbortProof`
   present and equal to the extracted proof, `reservation.Validate()`
   (which itself runs `linuxCgroupReservationAbortMatches`, so the disposition/helper/launcher
   evidence production demands is included), no release and no pre-gate proof beside it;
3. pre-gate release tombstone → `linuxV2RequireRetiredPriorPreGateTombstone`: `ContainmentCgroup ==
   nil`, no release proof, `proof.Validate()`, `SupervisorHandoffVersionV2`,
   `Receipt.Proof.GateOutcome.Outcome == LinuxCgroupGateOutcomeNeverCreated`;
4. the unchanged release arm: v2 `StopReceipt`, disposition ∈ {removed, absent_after_receipt} and the
   exact leaf-absence `Lstat`.

Mirrored arms are arms 2 and 3 of `linuxV2ForeignJournalIsSafelyRetired` (`:448-456`, abort at
`:453-456`) plus the existing release arm (`:442-447`). Two further production arms are deliberately
**not** mirrored, because no leg of any case set can leave that shape in the shared StateDir:

- the `LinuxCloneAbortProof`-only arm (`:457-459`): a cgroup-backed clone abort requires the
  reservation tombstone beside it (`linuxCloneAbortHistoryMatches`,
  `daemon/internal/state/linux_clone_validation.go:388-408`), and the bare legacy shape is produced
  only by the v1-era paths;
- the legacy v1 `ContainmentAuthority` + StopReceipt arm (`:460-462`): the only v1 leg
  (`run_legacy_v1_replay_witness`, `scripts/ci/linux-v2-native-acceptance.sh:3212-3216`) drives
  `TestProductionLinuxV2LegacyV1ReplayNoV2Receipt`, whose `runLinuxStopResponseLostCase` uses its own
  witness state root, not the shared StateDir.

Both stay refused with the existing ownership message; §5 names them as residuals for the next goal
that needs them.

The guard's abort arm reads its proof through the existing selector `linuxV2StageReplayNoTargetAbort`
(`:1495-1506`). That selector also takes the deferred tidiness item #309 left in `docs/TODO.md`
whose owner clause names "the next slice that already touches this file": it returned the preferred
clone proof by overwriting a pointer it had already cloned from the reservation; it now returns the
preferred clone proof first, else the reservation proof, else nil, cloning only the selected proof.
The precedence and every result are unchanged, and §4's tests already pin both directions.

Not weakened, and why:

- the release arm keeps every conjunct and the leaf `Lstat` — no release requirement is relaxed;
- the tombstone arms demand the daemon's own evidence and refuse any active-owner marker, so a
  tombstone that still owns a process, authority, handoff or scan is refused exactly as before;
- the leg-level validators that check each leg's *own* journal
  (`linuxV2ValidateStageReplayJournal` `:1404-1493`, `linuxV2ValidateStageJournalBoundary`,
  `linuxV2VerifyNoTargetCloneAbort` `:1771-1798`) are untouched, so the replay matrix's per-leg
  coverage is unchanged; and
- the acceptance script, the workflow, the exporter and every production package stay
  byte-identical: the `shared` arm and its credential-gated pi leg (1B1) are untouched.

## 3. Edge cases the contract demands

- Acceptance: the never-created tombstone the state producer writes for a never-created reservation,
  and the removed-empty tombstone the state producer writes for a leaf-bound empty abort, are both
  accepted, each driven through the production CAS rather than a literal.
- Acceptance, pre-gate: a pre-gate release tombstone with the never-created gate outcome is accepted;
  it is a literal, because the platform is the only issuer of the opaque pre-gate release
  (`platform.LinuxCgroupV2Release.SnapshotPreGate`, unexported fields) and its shape is pinned by
  the state package's own receipt fixture (`linux_cgroup_v2_test.go:268-292`); the authenticator is
  produced by the exported `linuxgate.SignGateNeverProducedAuthenticator`, as that fixture does.
- Refusal, individually reddenable: a clone-only proof with no reservation; a reservation whose
  stored proof differs from the extracted one; a reservation that no longer validates (one field
  changed); a tombstone carrying a release proof or a pre-gate proof beside it; a tombstone carrying
  a process marker. Each row names the guard conjunct that must redden when it is deleted.
- Scope of the accepted pre-gate arm: `ContainmentCgroup != nil` or a release proof beside the
  pre-gate proof is refused, matching production's arm order (arm 2 requires both to be absent).
- No new filesystem observation: the tombstone arms add no `Lstat`, so no new environment-dependent
  assertion is introduced into the CI guest; the leaf absence they imply is the daemon's own
  documented trust (`:898-902`), and the release arm's existing `Lstat` is unchanged.
- Adversarial satisfaction: the only real producer of an accepted abort tombstone is the Store's own
  abort CAS under the exact claim fence; a journal that satisfied it without the guarded shape would
  have to carry a valid `authority`-level proof equal to its reservation's stored proof, which the
  Store's own `validateJournal`/`linuxCgroupReservationAbortMatches` chain refuses to persist. The
  pre-gate literal is likewise admitted only after `SaveJournal` runs the production journal
  validation, and the equivalence rows re-read both through the daemon's admission scan.

## 4. Tests and evidence plan

| New behaviour | Committed red-capable test | Runs where |
| --- | --- | --- |
| The never-created tombstone and the removed-empty tombstone are accepted by the guard, and the daemon's own admission scan accepts the same journals | `TestLinuxV2PriorAbortTombstoneRetiresTheFreshRunGuard` (`daemon/e2e`), driving `AbortNeverCreatedLinuxCgroupReservation`, `AbortLinuxCgroupReservation` and `CheckLinuxV2NativeAdmissionBeforeClaim` | `daemon` job race pass (`go test -race ./...`), local WSL Noble |
| Each refusal conjunct above is pinned by its own row, and each row reddens when its conjunct is deleted | same test's refusal rows + the mutant runs recorded in the evidence record | same |
| The pre-gate release tombstone is accepted and any marker/release-proof beside it is refused | same test's pre-gate rows | same |
| The guard still refuses a claim-only retained row (unchanged guarantee) | existing `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` (`:2615-2624`) | same |

Evidence plan: run the new rows and the mutants in WSL with exact exit codes; re-run the neighbour
rows (`TestLinuxV2`, the whole `e2e` package), `go vet`, `gofmt`, the comment cap, `jscpd-changed`,
record cleanliness and citation existence; run the full Windows and WSL `go test ./...` with
base-vs-head attribution for any failure; record the residue: the leg itself is CI-only, so the
conductor's `shared` Bind is the acceptance and the record names what it must report.

## 5. Out of scope, with reasons

- `daemon/internal/state`, `daemon/internal/authority`, `daemon/internal/app`: no defect found
  there. Exporting `linuxV2ForeignJournalIsSafelyRetired` so the guard could call it was considered
  and rejected: it widens the production package surface for a fixture precondition, and the
  contract's owned-file scope is the witness and its helpers; the mirror is instead pinned by the
  equivalence rows above so a future drift reddens a named test.
- The production arms deliberately not mirrored (bare clone abort, legacy v1 stop receipt): no leg of
  any case set leaves those shapes in the shared StateDir (§2). If a future leg does, it stops with
  the existing ownership message and the arm is a named residual, not a silent acceptance.
- The acceptance script, the workflow, the exporter and the replay stage lists: unchanged; the fix
  needs no fixture or pipeline change, and a script edit would require runner-userland red/green
  proof before the PR opens.
- The other legs' prior journals: unchanged; the guard still refuses everything that can own a
  process or leaf.
