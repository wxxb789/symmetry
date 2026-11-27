# rf-acc-shared-helper-bound-journal — evidence record (the retained abort tombstone the fresh-run guard refuses)

Goal `rf-acc-shared-helper-bound-journal` (contract under the GDD contracts root, ruled by the
conductor 2026-11-28; dispatch gate `3d377582` = #309). Branch
`refactor/rf-acc-shared-helper-bound-journal`, dispatch base
`3d377582065255b47531861701967a407581d74d`. Plan
`docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md`. Shape reference:
`docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md` (committed, tested,
allowlisted-evidence increment). The digests below bind the delivered bytes; the stage's own gate
receipts bind the base, the changed-file scope and the content digests (§8).

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `213afc1cc0efc896b95258e29af39c8e1836e231bf2703d731016fd5cb6b3334` | The fresh-run guard dispatches across the daemon's retirement shapes (fenced abort tombstone, pre-gate leaf release) before its unchanged v2-StopReceipt release arm; three production-driven tombstone fixtures (never-created, removed-empty, and the clone-abort tombstone beside its reservation that the shared arm's own legs leave), one on-disk pre-gate fixture, one acceptance test and one refusal test; the deferred no-target selector tidiness item taken here (§4.1); the simplify-apply round (§4.2); and the review-fix round (§4.3) that refreshed these bytes. |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | `da798121a2a42778e429b928428c931876d231411f87cb94a0a6d0a4da87c79f` | Stage-1 answers, the accepted fix, the arms deliberately not mirrored (three, §2), the selector tidiness item taken here, the tests and the evidence plan; the review-fix round corrected the anchors this plan measured on the pre-apply bytes (§4.3). |
| `docs/evidence/rf-acc-shared-helper-bound-journal-2026-11-28.md` | this record | Stage-1 answers, the fix, the receipts. |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | The closed stage-1/2 items and the open Bind/residual entries. |

Byte-identical to the dispatch base: `daemon/e2e/production_linux_v2_native_witness_test.go`
(`98d4771cf6a3efafc45abab7a8c954a07e58b853858979a5435259e2e4c05d7e`, the digest #309's record §11
already named), `scripts/ci/linux-v2-native-acceptance.sh`
(`42177bb8ef119545997dcf3ba274d2a67e1a97abe0741d724c13b9cefcc6b305`), the acceptance workflow, the
evidence exporter, and every production package (`daemon/internal/state/linux_cgroup_journal.go`
`9358c264623f719038ddcfecaab976f3f2e7fde82f28a6a9689662cd167d9b4d`,
`daemon/internal/state/run_journal.go`
`093d93d942b1132d6f9b38b962938df3c3388ac4d6db9dec65a689aaabae9316` — both the same digests #309's
record §11 names). The fix needs no fixture, pipeline or production change.

## 2. Contract clause 1 — new or long-standing? Long-standing since #9, and latent by construction

`git show 41e64851` (#9, 2026-10-13) carries both sides of the contradiction in one commit:

- the guard, at that revision `daemon/e2e/production_linux_v2_owner_cli_test.go:2085-2106`, with
  today's conjuncts (`isStoppedRetained`, `ContainmentCgroup != nil`, the v2 StopReceipt demand) and
  today's message;
- the daemon's admission predicate `linuxV2ForeignJournalIsSafelyRetired` at
  `daemon/internal/state/state.go:450-475`, **already** carrying the abort arm at `:464-467`
  (`journal.ContainmentCgroup != nil && reservation.AbortProof != nil` →
  `reservation.Validate() && linuxCgroupReservationAbortMatches`) that retires exactly what the
  guard refuses.

No commit has changed either side since: `git log -S "may still own or require recovery of a process
or leaf" -- daemon/e2e` returns only `41e64851`, and
`git log -S "reservation_durable helper_bound" -- scripts/ci/linux-v2-native-acceptance.sh` returns
only `41e64851`, so the shared arm's replay order
(`scripts/ci/linux-v2-native-acceptance.sh:3298-3300`) is unchanged too.

The wall is nevertheless *new as an observation*, and the contract's own framing says why: no earlier
run ever measured a PASS at the preceding hold. Until #309 the `reservation_durable` leg was
unsatisfiable (its clause contradicted `authority.Validate`; #309's record §2), so no leg ever left a
tombstone behind *and* succeeded, and the guard's tombstone refusal was unreachable. The three #309
scratch Binds (`8cee777f`, `c45a05bea0`, `7865bc17`; #309's record §7, §9, §10) each report the same
`:224` guard message at the `helper_bound` subtest with the same three green `reservation_durable`
rows, naming a different RunID each time (`bad57e1c…`, `0c9048d6…`, `9588eb0e…`) — Control issues a
fresh RunKey per leg.

The contract's suggested observation — run only the replay legs (or the `helper_bound` hold) with the
preceding hold's fix in place, and on a pre-#309 tree — is not available on this workstation: those
legs run only inside the CI guest fixture (`linux-native-v2-acceptance.yml` → root, systemd-nspawn,
a debootstrapped guest, a built Control release). The replacement proof is the `git show`/`git log -S`
structure above plus §5's executed red rows: `M9` restores the pre-fix guard shape on the delivered
fixtures and reproduces the wall verbatim.

## 3. Contract clause 2 — which layer is wrong: the witness's expectation

Three bytes decide it; two of them agree with each other:

| Layer | Byte | Verdict on the retained pre-effect tombstone |
| --- | --- | --- |
| Daemon State admission | `daemon/internal/state/linux_cgroup_journal.go:432-464`, abort arm `:453-456`, pre-gate arm `:448-452` | **retired**: `noActiveMarkers` and no release/pre-gate proof and `reservation.Validate()` — a fresh run may proceed beside it |
| Daemon State vocabulary | `daemon/internal/state/run_journal.go:104-107`, `HasPendingContainment` | not a pending owner: the reservation term is `ContainmentCgroup != nil && AbortProof == nil` |
| Witness fresh-run guard | `daemon/e2e/production_linux_v2_owner_cli_test.go:2585-2606` (dispatch base) | **refuses**: `journal.ContainmentCgroup != nil` at `:2587-2590` plus the v2 StopReceipt release demand at `:2592-2598` |

The guard asks the daemon's own question — can a prior RunJournal still own a process or leaf? — and
answers it from one arm of the daemon's rule. A pre-effect abort has no target and no stop, so no
`StopReceipt`-backed release can ever exist for it, and the guard refuses a state the product retires
as safely retired. **The wrong layer is the witness's expectation,
`daemon/e2e/production_linux_v2_owner_cli_test.go:2585-2606`** — not the daemon (no
`DECISION_NEEDED`; `authority`, the state producer and the admission scan agree with each other and
with the pre-mkdir policy at `daemon/internal/state/linux_cgroup_journal.go:898-902`), and not the
fixture (§3.2).

### 3.1 The stop, and the one journal it names

`newLinuxV2NativeFixture` (`daemon/e2e/production_linux_v2_native_witness_test.go:1989`) snapshots
every journal in the shared StateDir at `:2093-2101` and raises the message at `:2099`; the reported
line `:224` is the `t.Run` closure of `TestProductionLinuxV2StageCrashReplayMatrix`
(`:218-227`), the innermost frame that is not a `t.Helper`. The guard runs before the leg starts any
daemon work, and the `reservation_durable` leg passed the identical guard in all three #309 runs, so
the only journal it can refuse at `helper_bound` is the tombstone the preceding leg left.

That leg's retained shape is fixed by its own validators: recovery is `aborted_no_target`
(`linuxV2ExpectedStageReplayRecovery` `daemon/e2e/production_linux_v2_owner_cli_test.go:1153-1161`),
which requires the tombstone to be *retained* with no release proof, no process details, no authority
and no handoff (`linuxV2ValidateStageReplayJournal` `:1404-1493`, `aborted_no_target` arm `:1441-1472`)
and binds the `never_created` disposition to that hold
(`linuxV2ValidateHeldStageAbortDisposition` `:1508-1525`). Its producer is
`state.Store.AbortNeverCreatedLinuxCgroupReservation`
(`daemon/internal/state/linux_cgroup_journal.go:903-928`), whose policy comment at `:898-902` states
the tombstone "remains until journal deletion so an unknown write can be replayed with the same
proof". Retention is designed behaviour, not a leak.

### 3.2 The fixture's own setup (the contract's first alternative)

The fixture neither releases nor reuses the preceding leg's journal, and it may not:

- each leg gets a fresh RunKey from Control (`scripts/ci/provision-linux-v2-control-claims.exs`,
  schedule mode, `Ecto.UUID.generate()`), so no leg adopts another leg's journal;
- the two `aborted_no_target` legs *require* their retained tombstone
  (`daemon/e2e/production_linux_v2_owner_cli_test.go:1441-1472`);
- the arm deletes only `request.json`/`replay_request.json` between legs
  (`scripts/ci/linux-v2-native-acceptance.sh:2647`, `:2717`), never a journal;
- the StateDir is fixed by the root-installed OwnerConfig and asserted equal to it
  (`newLinuxV2NativeFixture` `:2078-2091` fails unless `stateDir == owner.StateDir`; the arm pins the
  installed path through its own `DAEMON_SERVICE_STATE_DIR`,
  `scripts/ci/linux-v2-native-acceptance.sh:520`), so a per-leg StateDir is not available to this
  arm, and deleting the tombstone in the fixture would delete durable control state the product keeps
  for replay.

The precedent for the other direction does not apply: #300 moved the alternate-mount refusal into its
own case set because its leftover claim-only row is one "no product path retires"
(`scripts/ci/linux-v2-native-acceptance.sh:3150`); here a product path does retire the tombstone, and
moving the two abort legs out of the shared arm would drop their sequence coverage, which the
contract forbids.

## 4. Contract clause 3 — the fix, and what it does not weaken

`linuxV2RequireSafelyReleasedPriorJournal`
(`daemon/e2e/production_linux_v2_owner_cli_test.go:2887-2917`) is now a dispatcher across the
daemon's retirement arms, with its physical-release arm byte-identical:

1. owner-marker refusal at `:2889` — `HasProcessDetails`, `HasPendingContainment`,
   `ContainmentUnproven` — one literal message (`linuxV2PriorJournalOwnershipRefusal` `:2885`). The
   apply round (§4.2) dropped the two conjuncts `ContainmentAuthority != nil` and
   `ContainmentHandoff != nil`, which `HasPendingContainment`
   (`daemon/internal/state/run_journal.go:104-107`) already ORs; the two refusal rows added with that
   deletion keep the refusal guarantee pinned (M11, §5);
2. fenced abort tombstone at `:2894-2896` → `linuxV2RequireRetiredPriorAbortTombstone`
   (`:2919-2927`): `ContainmentCgroup` present with a proof equal to the extracted one — the
   preferred clone proof when the journal carries one, which is the shared StateDir's own shape
   (acceptance row 4) — `reservation.Validate()` (which itself runs
   `linuxCgroupReservationAbortMatches`, `daemon/internal/state/linux_cgroup_journal.go:1094-1130`,
   through `validateProofs` `:157-172`), and no release and no pre-gate proof beside it;
3. pre-gate release tombstone at `:2897-2899` → `linuxV2RequireRetiredPriorPreGateTombstone`
   (`:2929-2934`): `proof.Validate()` and no release proof beside it. `Validate()` implies the
   version and the `GateOutcome == never_created` conjuncts the daemon spells out at
   `linux_cgroup_journal.go:448-452`, and the dispatcher's earlier terms already exclude a live
   reservation and every active marker;
4. the unchanged release arm `:2900-2915`: v2 `StopReceipt`, disposition ∈ {removed,
   absent_after_receipt} and the exact leaf-absence `Lstat`.

One refusal row is deliberately stricter than the daemon. For a journal that carries both the
reservation tombstone and a clone-abort proof that differs from it, `linuxV2ForeignJournalIsSafelyRetired`
answers *safely retired* at its reservation arm (`linux_cgroup_journal.go:453-456`), whose conjuncts
all hold, before its clone arm (`:457-459`) is read; the guard refuses because its selector prefers
the clone proof (§4.1). The shape is unproducible: the Store's own journal validation refuses it with
and without a clone intent (§5's probes), so only the fixture's in-memory copy has that shape. The row
is therefore guard hardening, not a daemon-agreement row, and its name now says so; the plan's §3
carries the design note.

Three further daemon arms are deliberately **not** mirrored:

- the clone-reuse-retired arm (`linux_cgroup_journal.go:435-441`): its shape needs a
  `LinuxCloneReuseGrant` retired before native start with no cgroup reservation, written only by the
  reuse-abort path (`daemon/internal/state/linux_clone_journal.go:517-518`); the shared arm's case
  sets drive no clone-reuse flow, but unlike the two arms below that exclusion is not derived from a
  producer constraint, so the apply round (§4.2) added this arm to the inventory and to the residual;
- the `LinuxCloneAbortProof`-only arm (`linux_cgroup_journal.go:457-459`): a cgroup-backed clone
  abort requires the reservation tombstone beside it (`linuxCloneAbortHistoryMatches`,
  `daemon/internal/state/linux_clone_validation.go:388-409`), and the bare legacy shape is a
  pre-v2-era row;
- the legacy v1 `ContainmentAuthority` + StopReceipt arm (`linux_cgroup_journal.go:460-462`): the
  only v1 leg (`run_legacy_v1_replay_witness`, `scripts/ci/linux-v2-native-acceptance.sh:3212-3217`)
  drives `TestProductionLinuxV2LegacyV1ReplayNoV2Receipt`, whose `runLinuxStopResponseLostCase` uses
  its own witness state root, not the shared StateDir.

All three stay refused with the existing ownership message, so a row of any of those shapes stalls the
arm with the same diagnostic as before rather than passing silently; §10 names them.

Not weakened:

- the release arm keeps every conjunct and the leaf `Lstat`; no release requirement is relaxed;
- the tombstone arms demand the daemon's own evidence and refuse any active-owner marker, so a
  tombstone that still owns a process, authority, handoff or scan is refused exactly as before (§5's
  refusal rows, M1);
- the leg-level validators that check each leg's *own* journal
  (`linuxV2ValidateStageReplayJournal` `:1404-1493`, `linuxV2ValidateStageJournalBoundary`,
  `linuxV2VerifyNoTargetCloneAbort` `:2071-2098`) are untouched, so the replay matrix's per-leg
  coverage is unchanged; and
- the acceptance script, the workflow, the exporter and every production package are byte-identical
  to the dispatch base (§1), so the `shared` arm and its credential-gated pi leg (1B1) are untouched.

### 4.1 The fixtures, and why the pre-gate one is a file

`linuxV2NeverCreatedTombstoneJournal` (`:1596-1614`), `linuxV2RemovedEmptyTombstoneJournal`
(`:1616-1653`) and `linuxV2CloneTombstoneJournal` (`:1717-1781`) drive the production CAS chain —
`SaveJournal`, `ReserveLinuxCgroup`, then `AbortNeverCreatedLinuxCgroupReservation`, or
`MarkLinuxCgroupCreateStarted` → `BindLinuxCgroupLeaf` → `AbortLinuxCgroupReservation` with a
`removed_empty` proof; the clone fixture additionally drives `ReserveLinuxCloneIntent` →
`FenceLinuxCloneBeforeCheckout` → `CompleteLinuxCloneAbort` over the same tombstone — so each accepted
shape is written by the daemon's own producer, not by a literal. `linuxV2PreMkdirAbortTombstone`
(`:1783-1786`) now delegates to the first builder, so #309's three tests keep the identical producer
path.

`linuxV2PreGateReleaseTombstoneJournal` (`:1672-1715`) is a literal, because the platform is the only
issuer of the opaque pre-gate release (`platform.LinuxCgroupV2Release.SnapshotPreGate`, unexported
fields) and the Store refuses to import that tombstone outside its dedicated clear
(`daemon/internal/state/run_journal.go:275`, `:343`); the fixture is therefore written as the
daemon's own on-disk journal file (`linuxV2WritePriorJournalFile` `:1655-1670`, the same
name↔key derivation the Store enforces at `daemon/internal/state/run_journal.go:544-556`) and then
re-read through the production Store (`LoadJournal`) so its `validateJournal` pass is production
evidence, not the fixture's claim. Its receipt shape is the state package's own pre-gate fixture
(`daemon/internal/state/linux_cgroup_v2_test.go:254-329`), with the authenticator produced by
`linuxgate.SignGateNeverProducedAuthenticator`. The held-stage fixture now writes through the same
`linuxV2WritePriorJournalFile` (apply round, §4.2), so one writer derives the name for both.

`linuxV2StageReplayNoTargetAbort` (`:1495-1506`) also takes the deferred tidiness item #309 left in
`docs/TODO.md` with "the next slice that already touches this file" as its owner, which this slice
is: it no longer clones the reservation proof before discarding it when the nested clone proof is
preferred, but returns the preferred clone proof first, else the reservation proof, else nil,
cloning only the selected proof. The precedence and every result are unchanged (the mandatory
pre-mkdir path and every `aborted_no_target` leg keep the same selected proof), the one-line
precedence note beside the selector records why the clone proof wins, and §5's reordered-selector
mutant reddens the clone-vs-reservation mismatch row alone — the row that exists because the
preference is load-bearing.

### 4.2 The simplify-apply round (2026-11-28)

Seven accepted tidy-up findings plus one record-accuracy defect were applied to the delivered bytes
after the simplify review; the fourth finding (hoisting the two refusal messages to package consts)
was skipped, because a literal shared between producer and assertion makes that message pin hollow.
What changed:

- the guard's first condition lost the two conjuncts `HasPendingContainment` already covers, and the
  refusal test gained one row per dropped conjunct (`process authority on the fence tombstone`,
  `launch handoff fence on the fence tombstone`) so the surviving term is pinned by M11 (§5);
- `linuxV2CloneTombstoneJournal` and acceptance row 4 (`clone abort tombstone beside its
  reservation`) pin the accept direction of the shape the shared StateDir's own journal has: a
  reservation tombstone beside a clone-abort proof that equals it. M12 is the counterfactual (§5);
- `linuxV2PriorLeafIdentity` (`:1580-1585`) replaced three hand-written four-line leaf literals; the
  single-caller store helper was inlined into `linuxV2NewPriorJournalFixtures` (`:1549-1562`); the
  held-stage fixture calls `linuxV2WritePriorJournalFile` instead of its own write block; and the
  acceptance loop's snapshot lookup now names a missing key instead of asserting through a zero
  snapshot;
- the record inventory of unmirrored daemon arms now names three arms instead of two (§4, §10,
  `docs/TODO.md`): the clone-reuse-retired arm (`linux_cgroup_journal.go:435-441`) was missing from
  the earlier count.

All of it is inside the fixture's own file: no production package, no script, no workflow and no
leg-level validator changed, and the pre-apply digests (`812593b9…` for the Go file) and every receipt
bound to them are PARTIAL and SUPERSEDED by §1, §5 and §8.

### 4.3 The review-fix round (2026-11-28)

Five review findings are applied to the delivered bytes; no production file, script or workflow
changes. F4: the three owner-marker refusal rows now carry the message literal inline instead of the
package `linuxV2PriorJournalOwnershipRefusal`, so a producer-message drift reddens them — M13 proves
the change is load-bearing, because the same drift left the pre-fix rows green (§5). F5: the mismatch
row's name states that the guard is stricter than the daemon, and §4's arm-order sentence is scoped to
the arms the dispatcher mirrors, with the probe-backed note above. F1–F3 are record- and
plan-accuracy fixes: the archived closeout's row count (`docs/archive/CLOSED-TODO.md`), the plan's
red-capable test name, and ten wrong line ranges re-measured on the delivered bytes — the nine the
review named (eight in the plan, one in this record) plus the claim-only test's stale anchor, which
the same sweep found; nine were corrected here (the eight plan ranges the review named and the
claim-only sibling) and this record's own range stayed stale until the docs-only follow-up that
closed the panel's round-1 audit (§11); the two pre-fix guard anchors that keep dispatch-base numbers
are now labelled as such (§11).
The pre-fix bytes (`6ddddb8c…` for the Go file, `52f5e13c…` for the plan) and every receipt bound to
them are PARTIAL and SUPERSEDED by §1, §5 and §8.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`) from the `daemon/`
directory of the delivered worktree, on the *delivered* bytes `213afc1c…` (§1). The mutant rows ran in
a scratch copy of `daemon/` with one exact substring replaced in the delivered file; each mutant's own
sha256 is in the table, and the copy was re-created from the delivered file between runs (the restored
row re-measures to the delivered digest and exits 0). The replacement pairs are not delivered (the
previous round's review recorded the same limit; a re-derivation names each mutant's dropped conjunct
in the row below). The pre-apply rows, measured on `812593b9…`, and the pre-fix-round rows, measured
on `6ddddb8c…`, are PARTIAL and SUPERSEDED by this table.

Command for every row below:
`go test ./e2e -run 'TestLinuxV2(RetiredPriorJournalAllowsAFreshRun|PriorJournalRetirementRefusesActiveOwners)' -count=1`

| Row | sha256 (mutant) | Exit | Observed red row / message |
| --- | --- | --- | --- |
| Green, delivered bytes | §1 `213afc1c…` | 0 | 4 acceptance rows and 10 refusal rows PASS (16 subtests); each acceptance row also asserts the daemon's own `CheckLinuxV2NativeAdmissionBeforeClaim` accepts the same journal |
| Red M1, dispatcher drops the process marker | `bad5b5b5a6e7abbaabdc90b736cdad27891cde861391177897ef66631fcf4d37` | 1 | `process_marker_on_the_fence_tombstone`: `prior RunJournal admission = <nil>, want a refusal naming "prior RunJournal may still own or require recovery …"` |
| Red M2, abort arm drops the nil-reservation guard | `3b4a17349a55fe62ddd0908d9a2c4dc687c0ad28b1e18cdd6490093f77d31a77` | 1 | `clone_abort_proof_with_no_reservation`, with a nil-pointer panic in the guard |
| Red M3, abort arm drops the proof-equality guard | `34c599226ab5f12456069d59df016edf273dafe7337ae607e272c7bb3606f923` | 1 | `clone_tombstone_that_differs_from_its_reservation_(guard_stricter_than_the_daemon)`: `admission = <nil>, want a refusal naming "prior RunJournal's abort tombstone is not an exact fenced pre-effect retirement"` |
| Red M4, abort arm drops `reservation.Validate()` | `1ecf8f744ee03816a5cff2dd3b4a164f81e75e6fd7daf9ca0d633fafc24f067d` | 1 | `reservation_that_no_longer_validates`: the same abort-tombstone refusal |
| Red M5, abort arm drops the release-proof absence | `812578ea432a12a9f3faa12d892df3935c4aa6aaae15a90163e161e9850f25c6` | 1 | `release_proof_beside_the_fence_tombstone`: the same abort-tombstone refusal |
| Red M6, abort arm drops the pre-gate-proof absence | `b0962cd484416467bfeded454e3c76c7cf0fe98beec5e080edd0488984dd0027` | 1 | `pre-gate_proof_beside_the_fence_tombstone`: the same abort-tombstone refusal |
| Red M7, pre-gate arm drops the release-proof absence | `e1bf70680b370e70460ea60eabd5825c497a50edac4fea2c90bad0cf7b720801` | 1 | `release_proof_beside_the_pre-gate_tombstone`: the pre-gate refusal |
| Red M8, pre-gate arm drops `proof.Validate()` | `b4f655f5c7504923fa8db63daca670038309684c3a6c0b043022e6d77519305c` | 1 | `pre-gate_proof_without_its_removal_disposition`: the pre-gate refusal |
| Red M9, the pre-fix guard (both dispatch arms deleted) | `65b2dbcbff83e2824d5a93be00e911351761bf72546603095f5f939dc8e34746` | 1 | all four acceptance rows (`retired prior RunJournal was refused as an owner: prior RunJournal may still own or require recovery of a process or leaf` — the wall, reproduced on the delivered fixtures) and seven of the ten refusal rows: both clone-proof rows, `reservation_that_no_longer_validates`, `release_proof_beside_the_fence_tombstone`, `pre-gate_proof_beside_the_fence_tombstone`, `release_proof_beside_the_pre-gate_tombstone` and `pre-gate_proof_without_its_removal_disposition` |
| Red M10, the selector's two branches reordered | `e672858c77f0d30583239460de0f4561c1bbc885766be494cd77e107eceaf122` | 1 | `clone_tombstone_that_differs_from_its_reservation_(guard_stricter_than_the_daemon)` alone: the reservation proof would be selected instead of the preferred clone proof, so the mismatch row's journal is admitted. The clone-tombstone acceptance row stays green, because its two proofs are equal |
| Red M11, guard drops the pending-containment term | `02fb0cace9b56ba5cb3f906abb47879f022d4295914e283345b0f84e4a64a237` | 1 | `process_authority_on_the_fence_tombstone` and `launch_handoff_fence_on_the_fence_tombstone` alone: with the term gone the abort arm admits both journals |
| Red M12, abort arm refuses a clone proof beside the tombstone | `4ff2700579fc44da408c8db8787554183ecbf75ef555d053f395c456f1336eb2` | 1 | `clone_abort_tombstone_beside_its_reservation` alone: the added conjunct refuses the shape the shared StateDir's own journal has |
| Red M13, the producer's ownership message drifts | `5d9ae262e42e1f52bb0626e8adb42da9498b5a162bd286924363bfc900dd3d33` | 1 | `process_authority_on_the_fence_tombstone`, `launch_handoff_fence_on_the_fence_tombstone` and `process_marker_on_the_fence_tombstone` alone: each row's message pin is now independent of the producer's literal (F4). The same drift on the pre-fix bytes exits 0 with no failing row |

Neighbour rows, same environment:

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Neighbour predicates | `go test ./e2e -run '^TestLinuxV2' -count=1` (WSL Noble) | 0 | `ok … daemon/e2e 1.002s` |
| Whole `e2e` package | `go test ./e2e -count=1` (WSL Noble) | 0 | `ok … daemon/e2e 1.758s` |
| Whole `e2e` package | `go test -count=1 ./e2e` (`daemon/`, Windows `go1.27.4`) | 0 | `ok … daemon/e2e 0.283s` |
| Vet | `go vet ./e2e` (both userlands) | 0 | no output |
| Format | `gofmt -l e2e` (both userlands) | 0 | no output |
| Daemon-retires probe (F5) | scratch copy of the delivered `daemon/`, `go test ./internal/state -run 'TestFix1ProbeForeignCrossProofShape' -count=1 -v` | 0 | `PROBE producible=no validateJournal=run journal has Linux clone evidence without an intent`; `PROBE daemon_retires=yes` — the predicate answers *safely retired* on the mismatch shape, and the Store refuses to load it |
| Store-refuses probe (F5) | the same copy with the probe file in `e2e`, `go test ./e2e -run 'TestFix1ProbeStoreRefusesMismatchedCloneProofBesideItsReservation' -count=1 -v` | 0 | `PROBE store_refuses=run journal Linux clone abort proof lacks exact no-gate history` — with an intent the equality demand (`linuxCloneAbortHistoryMatches`) is the refusal |
| Message-drift baseline (F4) | the M13 replacement applied to the pre-fix file `6ddddb8c…`, same targeted run | 0 | no failing row: before the inline literals the drift was invisible to those three rows |

Every refusal this round claims names the mutant that reddens its own row: M1 pins the dispatcher's
process-marker conjunct, M2–M8 pin the seven conjunct removals of the two tombstone arms, M11 pins the
`HasPendingContainment` term that carries the authority and handoff refusals, M13 pins the three
owner-marker rows' message text, and M10 pins the selector's precedence. The acceptance rows are pinned
from the other side: M9 (the pre-fix guard) reddens all four, M12 reddens the clone-tombstone row
alone, and each row's second assertion — the daemon's own admission scan — fails if the guard ever
admits more than the daemon retires. The two probes show both sides of the one row the guard hardens
past the daemon (§4): the daemon's predicate retires it and the Store refuses to carry it.

## 6. Which userland each property needed (standing rule)

The new rows are pure Go logic over fixtures in `t.TempDir()`: no permission, ACL, mount, cgroup or
image resolution decides their outcome, and the only filesystem semantics they touch are ordinary
directory creation and file read-back, which no userland in the matrix treats differently. Noble is
therefore a sufficient userland for them, and the runner-mirroring requirement (rule 17) does not
apply. The leg itself keeps its unchanged userland: `linux-native-v2-acceptance.yml` runs on
`ubuntu-latest` with the Resolute guest rootfs the fixture builds, and this increment adds no new
assertion, pipeline step or exported file there. The two new arms add no filesystem observation to
the guard — in particular no `Lstat` — so no new environment-dependent assertion enters the guest;
the release arm's existing leaf `Lstat` is unchanged and was already exercised by the earlier legs'
retained release journals in the three #309 runs.

## 7. Does the conductor's Bind execute it?

Both new tests live in `daemon/e2e` and run in the `daemon` job's race pass (the `daemon_race_tests`
step, `.github/workflows/ci.yml:1055-1063`, running `test -race -count=1 -timeout 600s -json ./...`
through `scripts/ci/go-test-json-gate.py`) and locally; the `shared` Bind does not invoke `./e2e`
with these test names, so it does not execute them. What the Bind *does* execute is the fixed guard
itself, twice per `shared` run: at the `helper_bound` fixture creation over the `reservation_durable`
tombstone — the clone-tombstone shape acceptance row 4 pins, which is why that row was added — and, if
the arm reaches it, at the `receipt_persist_failure`/`literal_pid_reuse` fixture creation over the
`helper_bound` and ACK-loss `helper_bound` tombstones (the `removed_empty` shape, acceptance row 2).
The pre-gate arm is executed only if the arm reaches
`helper_launcher_response_loss` and its final journal carries the pre-gate tombstone; no run has been
measured there. The refusal rows need a journal whose shape contradicts the guard; no Bind case
produces one, so they are attributed to the `daemon` job's race pass and to §5.

## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above (fix round) | see §5 (`daemon/`, WSL Noble) | per §5 | 4 acceptance rows and 10 refusal rows green; thirteen mutants red, each at its own named row; both F5 probes green; neighbour rows, `gofmt` and `vet` green in both userlands |
| Whole `e2e` package, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./e2e` (`daemon/`) | 0 | `ok … daemon/e2e 0.283s` |
| Whole `e2e` package, WSL Noble (`go1.27.0 linux/amd64`) | `go test -count=1 ./e2e` (`daemon/`) | 0 | `ok … daemon/e2e 1.758s` |
| Vet and format | `go vet ./e2e`; `gofmt -l e2e` (both userlands) | 0 / 0 | no output in either |
| Comment cap | the coordinator's owner comment-policy checker on the scratch commit carrying these bytes | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 5 changed files, 1805 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new; the notices name the 6173-line Go test file and the 5896-line `docs/archive/CLOSED-TODO.md` as outside the configured scope (both above the pinned `--max-lines 5000` ceiling) |
| Changed-file duplication, raised ceilings | the same script with `JSCPD_LIMITS` raised to `--max-lines 30000 --max-size 8mb` | 0 | 5 changed files, 1812 scanned sources, 1404 clones in the corpus, 0 tolerated among the added lines, none new, no notice — every changed file was scanned |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed record passes the root-only check |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed document passes the path and symbol check (the notices are pre-existing ignore-rule notes for the portal and `deps` paths) |
| Model-routing record sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | 0 | three changed documents pass (the plan, this record, `docs/archive/CLOSED-TODO.md`); the apply round rendered that file's 18 host-shaped spellings as `<HOST-PATH>`/`<TMP>` and this round adds none |
| Apply-round full Go suite, Windows (superseded for the fix bytes) | `go test -count=1 ./...` (`daemon/`, the pre-fix bytes `6ddddb8c…`) | 0 | 30 rows: 27 `ok`, 3 `[no test files]`, no `FAIL` |
| Apply-round full Go suite, WSL Noble as root (superseded for the fix bytes) | `go test -count=1 ./...` (`daemon/`, the pre-fix bytes) | 1 | 32 rows: 29 `ok` including `ok … daemon/e2e 2.633s`, 3 `[no test files]`, two failing packages (`internal/platform`, `native/linux-gate/protocol`) with 14 failing test rows |
| Attribution, `internal/platform` | the same package at the head and in a `git archive 3d377582 daemon` tree, same environment | 1 and 1 | both: 13 rows of `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`; `daemon/internal/platform/anchor_shape_table_test.go` sha256 `1f6b070ac0ce9a2a8e036c11fa6212a1c98051cebd5b31e97d593f1163ff0841` identical on both sides |
| Attribution, `native/linux-gate/protocol` | the same package at the head and in the base tree | 1 and 1 | both: `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` alone, the same `phase=fd … closedir … errno=1` census rows |

The comment cap, both duplication runs, the record check, the citation check and the sanitize sweep
read the *committed* diff, so both rounds ran them on a scratch clone carrying the round's delivered
bytes of every changed file, base `3d377582` (the fix round's run carries §1's `213afc1c…` and
`da798121…`); the record's own §8 text follows that clone commit, so those numbers bind the
changed-file scope and the content digests rather than a revision that cannot name itself — the next
commit that touches this record is the deliverable's own, and the docs-only follow-up that closed the
panel's round-1 audit (§4.3) touches this record only, so the changed-file scope and those content
digests are unchanged. The sanitize sweep's one document-level finding was the apply round's:
`docs/archive/CLOSED-TODO.md` already refused `--check --root-only` at
the dispatch base (five drive/UNC/POSIX-mount spellings plus thirteen host-shaped regression specimens
quoted in the sanitizer's own closed record), so those spellings were rendered with this step's
sanctioned `<HOST-PATH>`/`<TMP>` tokens, keeping each sentence's shape and counts and losing only the
literal root; this round edits that file's goal entry only. The full-suite rows ran on the pre-fix
bytes: the round's Go edit is inside the same `//go:build linux && amd64` test file, so Windows
compiles it out entirely and the WSL run executes it inside `daemon/e2e`; per the standing fix-round
scope the fix round gates that package whole in both userlands and attributes head-wide full suites to
CI and to the conductor's Bind. Neither WSL failure is in the changed scope, and both reproduce at the
dispatch base in the same environment (`internal/platform` 13 uid-refusal rows,
`native/linux-gate/protocol`'s fd-census pin alone, `e2e` green).

## 9. Next Bind

The binding receipt must be the conductor's re-Bind on the post-fix bytes: this round changes the
`daemon/e2e/production_linux_v2_owner_cli_test.go` blob (§1 `213afc1c…`) and the plan, so neither a
run that gated #309's `96306f49…` blob nor any run on the pre-fix bytes `6ddddb8c…` can bind it. The
three #309 scratch Binds (`46037316453`,
`46041720955`, `46045331565`; #309 record §7, §9, §10) are BASELINE ONLY: each gated a predecessor
witness blob, and each records the wall's `:224` guard stop.

What the re-Bind must report: the `shared` case-set exit status, the `verified …` sequence and its
first disagreement, the `reservation_durable` row (expected green, unchanged), the `helper_bound` leg
(expected: no `:224` guard stop — the guard now retires the retained tombstone), the first unexpected
failure if the arm stops later in the replay matrix, the credential-gated pi outcome verbatim in log,
declaration and exported member, the export status and the reader Result. The expected green outcome
of the fixed rows is §5's; the Bind is the acceptance, not this record.

## 10. Residuals and known limits

- The leg is CI-only: nothing here proves the `helper_bound` leg passes on the delivered bytes. The
  guard's accept direction is executed locally, and M9 reproduces the wall on the delivered fixtures,
  but the leg itself waits for the conductor's dispatch. In all three #309 runs the tombstone was the
  only pre-existing journal the guard could name, which is what §3.1's ordering argument rests on.
- If the arm stops later, the next stop is most likely a shape this fix did not mirror: the
  clone-reuse-retired arm, the bare `LinuxCloneAbortProof` row or the legacy v1
  `ContainmentAuthority` + StopReceipt row (§4). All three refuse with the existing ownership
  message, so the arm fails closed rather than passing silently; `docs/TODO.md` carries the residual
  item, and the apply round added the third arm to it.
- The clone-tombstone acceptance row (row 4) is inferred-reachable in the same sense as the pre-gate
  one: the guard's selector prefers the clone proof, so the `helper_bound` fixture creation exercises
  this row's shape only if the preceding leg's journal carries the clone proof
  `linuxV2VerifyNoTargetCloneAbort` demands; a leg reaching the arm without one selects the
  reservation proof instead, and row 1's shape is exercised there.
- The pre-gate acceptance arm is inferred-reachable, not observed.
  `linuxV2ValidateLauncherReadyLostAckSummary` (`:2428-2467`) permits `released` or
  `pre_gate_released` for `helper_launcher_response_loss`; only the second shape reaches the new arm,
  and no run has been measured at that leg.
- The pre-gate fixture is a literal imported as a journal file (§4.1): the platform is the only issuer
  of that proof, so its fidelity rests on the Store's own read validation at construction, the
  daemon's admission scan in the acceptance row, and the state package's receipt fixture. A drifted
  platform shape reddens the fixture construction or the acceptance row rather than passing silently.
- The guard still refuses a prior journal that carries a release proof *and* a tombstone (§5's
  refusal rows), matching the daemon's arm order; such a row cannot be produced by the Store, so the
  refusal is a malformed-input guard, not a live-path behaviour claim.
- The mismatch row is guard hardening in the same sense (§4, §5's probes): the daemon's predicate
  retires that shape at its reservation arm while the Store refuses to carry it, so the row documents
  the guard's stricter selector preference rather than a live divergence. The daemon side of that
  statement rests on the scratch probes, not on a committed row: a shape the Store cannot carry has
  no product path to pin.
- The two WSL suite failures (§8) are environmental and pre-existing; neither package is in the
  changed scope, and both were reproduced at the base revision in the same environment.

## 11. Anchor re-measurement (delivered bytes)

Every line anchor this record cites was re-read on the delivered bytes after the last edit and is
cited with that file's sha256; digests already in §1 are referenced there, not repeated. The fix
round's sweep re-measured every `:start-end` citation of this record and the plan against the
delivered files, printing each resolved citation's first and last line, and corrected nine wrong
ranges (the eight plan ranges the review named and the claim-only sibling, §4.3); it left this
record's §4 range stale, which the panel's round-1 audit found and the docs-only follow-up commit
closed after re-reading every anchor of both documents on these bytes. Each corrected range now starts
at the symbol, list or comment the sentence around it names.

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | §1 `213afc1c…` | selector `:1495-1506` and its precedence note `:1496`; fixture store and constructor `:1549-1562`; tombstone reservation `:1564-1578`; leaf-identity builder `:1580-1585`; tombstone journal `:1587-1594`; never-created builder `:1596-1614`; removed-empty builder `:1616-1653`; journal-file writer `:1655-1670`; pre-gate builder `:1672-1715`; clone-tombstone builder `:1717-1781`; #309 helper `:1783-1786`; #309 tests `:1788-1849`, `:1851-1871`, `:1873-1905`, `:1907-1926`, `:1928-1955`; acceptance test `:1957-1987` with its new row `:1964`, snapshot lookup `:1978-1981` and guard call `:1982`; refusal test `:1989-2069` with its two apply-round rows `:2002-2009`, the three inline-literal rows `:2002-2014` and assertion `:2063-2066`; `linuxV2VerifyNoTargetCloneAbort` `:2071-2098`; leg validator gate `:2147-2248`; launcher-ready summary `:2428-2467`; `case "released":` `:1414` and `case "pre_gate_released":` `:1429`; recovery map `:1153-1161`; journal validator `:1404-1493` with `aborted_no_target` `:1441-1472`; held-stage binding `:1508-1525`; snapshot predicate test `:2827-2883`; guard `:2887-2917` (markers `:2889`, abort dispatch `:2894-2896`, pre-gate dispatch `:2897-2899`, release arm `:2900-2915`); abort helper `:2919-2927`; pre-gate helper `:2929-2934`; claim-only test `:2943-2952`; pid-reuse leg `:3813-4137`; ack-loss legs `:4879-5043`; helper-loss legs `:5045-5292`; receipt leg `:5614-5748` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | §1 `98d4771c…` (dispatch base) | fixture `:1989`; retained-journal snapshot `:2093-2101` with the guard call `:2098` and the message `:2099`; the `:224` subtest invocation; StateDir assertion `:2078-2091`; leaf path `:3601-3603`; physical-release helper `:2738` |
| `daemon/internal/state/linux_cgroup_journal.go` | §1 `9358c264…` (dispatch base) | retirement predicate `:432-464` (pre-gate arm `:448-452`, abort arm `:453-456`, clone arm `:457-459`, v1 arm `:460-462`); reservation `Validate` `:50-79`; `validateProofs` `:157-172`; abort match `:1094-1130`; never-created producer `:903-928` with its policy comment `:898-902`; preclaim scan `:514-527` |
| `daemon/internal/state/run_journal.go` | §1 `093d93d9…` (dispatch base) | `HasPendingContainment` `:104-107`; `SaveJournal` `:254-360` with the pre-gate import refusal `:275`, `:343`; journal-file naming `:544-556` |
| `daemon/internal/state/linux_clone_validation.go` | — | clone-abort history `:388-409`; `linuxCloneAbortNoActiveOwner` `:439-442` |
| `daemon/internal/state/cgroup_release_linux.go` | — | pre-gate receipt `:17-52`; pre-gate clear `:149-191` with the tombstone write at `:188` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | — | the pre-gate receipt fixture `:254-329` with its receipt shape inside it |
| `scripts/ci/linux-v2-native-acceptance.sh` | §1 `42177bb8…` (dispatch base) | stage-replay leg runner `:2619-2651` with the request cleanup `:2647`; ACK-loss runner `:2677-2721` with its cleanup `:2717`; the #300 isolation note `:3148-3151`; legacy v1 leg `:3212-3217`; shared arm lists `:3298-3303`, `:3305-3306`, `:3309`; `DAEMON_SERVICE_STATE_DIR` `:520` |
| `.github/workflows/ci.yml` | — | the `daemon_race_tests` step `:1055-1063` |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | §1 `da798121…` | §1.4 fixture facts, §2 the dispatcher, the clone-proof-equal abort shape and the selector item, §3 the refusal rows, §4 the test plan, §5 the out-of-scope residuals — all re-read after the fix round; the plan's own anchors point at this table's ranges, and the two it keeps at their dispatch-base values (the pre-fix guard and the claim-only test) say so |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the open Bind and residual entries and the closed stage items; not byte-pinned, because a later unrelated merge moves them |
