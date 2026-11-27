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
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `09a3d4ca8296a8225e725eeb72cd8ec74eb54af38e6f8691e63c3a1aa518e0af` | The fresh-run guard dispatches across the daemon's retirement shapes (fenced abort tombstone, pre-gate leaf release) before its unchanged v2-StopReceipt release arm, and **every** successful arm additionally proves the exact leaf path is currently absent (`linuxV2RequireAbsentPriorLeaf` `:3170-3180`, a `Lstat` whose non-absence outcome and stat error are both refusals); the abort arm validates the enclosing clone abort proof whenever the selected tombstone is a clone's nested one (`linuxV2RequireExactCloneAbortEnclosure` `:3196-3206`); the shared StateDir snapshot reads fail-closed and applies the production Store's own journal read — including the entry-name/key check — to the exact bytes it classifies (`linuxV2SnapshotStoreJournals` `:2915-2941`, `linuxV2ReadPriorJournalFiles` `:2942-2975`, `linuxV2StoreJournalValidator` `:2976-3015`); five production-driven tombstone fixtures (never-created, removed-empty, the clone-abort tombstone beside its reservation, the clone-enclosed removed-empty tombstone, and the on-disk pre-gate fixture) drive one acceptance test, one refusal test, a present-leaf refusal test and a Store-invalid-row test; the deferred no-target selector tidiness item taken here (§4.1); the simplify-apply round (§4.2); the review-fix round (§4.3); and the panel round-2 fix round (§4.4) that refreshed these bytes. |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | `a1c5dce8fd70eb156046c387a41dca6a938be978978b2497a72c081f5bd14f08` | Stage-1 answers, the accepted fix, the arms deliberately not mirrored (three, §2), the selector tidiness item taken here, the tests and the evidence plan; the review-fix round corrected the anchors this plan measured on the pre-apply bytes (§4.3). |
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
(`linuxV2ExpectedStageReplayRecovery` `daemon/e2e/production_linux_v2_owner_cli_test.go:1155-1164`),
which requires the tombstone to be *retained* with no release proof, no process details, no authority
and no handoff (`linuxV2ValidateStageReplayJournal` `:1406-1496`, `aborted_no_target` arm `:1443-1474`)
and binds the `never_created` disposition to that hold
(`linuxV2ValidateHeldStageAbortDisposition` `:1509-1527`). Its producer is
`state.Store.AbortNeverCreatedLinuxCgroupReservation`
(`daemon/internal/state/linux_cgroup_journal.go:903-928`), whose policy comment at `:898-902` states
the tombstone "remains until journal deletion so an unknown write can be replayed with the same
proof". Retention is designed behaviour, not a leak.

### 3.2 The fixture's own setup (the contract's first alternative)

The fixture neither releases nor reuses the preceding leg's journal, and it may not:

- each leg gets a fresh RunKey from Control (`scripts/ci/provision-linux-v2-control-claims.exs`,
  schedule mode, `Ecto.UUID.generate()`), so no leg adopts another leg's journal;
- the two `aborted_no_target` legs *require* their retained tombstone
  (`daemon/e2e/production_linux_v2_owner_cli_test.go:1443-1474`);
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
(`daemon/e2e/production_linux_v2_owner_cli_test.go:3140-3169`) is a dispatcher across the
daemon's retirement arms; its release arm keeps every conjunct, and since the panel round-2 fix
(§4.4) every arm ends in one shared absence check, so no successful return exists without a current
filesystem observation of the exact leaf path:

1. owner-marker refusal at `:3142` — `HasProcessDetails`, `HasPendingContainment`,
   `ContainmentUnproven` — one literal message (`linuxV2PriorJournalOwnershipRefusal` `:3202`). The
   apply round (§4.2) dropped the two conjuncts `ContainmentAuthority != nil` and
   `ContainmentHandoff != nil`, which `HasPendingContainment`
   (`daemon/internal/state/run_journal.go:104-107`) already ORs; the two refusal rows added with that
   deletion keep the refusal guarantee pinned (M11, §5);
2. fenced abort tombstone at `:3145-3150` → `linuxV2RequireRetiredPriorAbortTombstone`
   (`:3181-3195`): `ContainmentCgroup` present with a proof equal to the extracted one — the
   preferred clone proof when the journal carries one, which is the shared StateDir's own shape
   (acceptance rows 4 and 5) — `reservation.Validate()` (which itself runs
   `linuxCgroupReservationAbortMatches`, `daemon/internal/state/linux_cgroup_journal.go:1094-1130`,
   through `validateProofs` `:157-172`), and no release and no pre-gate proof beside it. When the
   journal carries a clone abort proof at all, that enclosing proof must also validate, bind the
   journal's own clone intent and enclose exactly this nested tombstone
   (`linuxV2RequireExactCloneAbortEnclosure` `:3196-3206`, §4.4);
3. pre-gate release tombstone at `:3151-3156` → `linuxV2RequireRetiredPriorPreGateTombstone`
   (`:3207-3213`): `proof.Validate()` and no release proof beside it. `Validate()` implies the
   version and the `GateOutcome == never_created` conjuncts the daemon spells out at
   `linux_cgroup_journal.go:448-452`, and the dispatcher's earlier terms already exclude a live
   reservation and every active marker;
4. the release arm `:3157-3167`: v2 `StopReceipt`, disposition ∈ {removed, absent_after_receipt};
5. the shared absence check `linuxV2RequireAbsentPriorLeaf` (`:3170-3180`): `Lstat` of
   `mount/delegated/<leaf name>`, passing only on `os.ErrNotExist` — an entry at that exact path is
   the presence refusal, and any other stat error, an unresolvable path included, is the inspection
   refusal. The name is `authority.LinuxCgroupReservationAbortProof.LeafName` for the abort arms, the
   pre-gate receipt's `CgroupIdentity.LeafName` for the pre-gate arm and the release receipt's
   identity's for the release arm. `TestLinuxV2RetiredPriorJournalRefusesAPresentLeaf` (`:2109-2172`)
   pins both directions on all five tombstone shapes and M14 reddens it (§5, §4.4).

One refusal row is deliberately stricter than the daemon. For a journal that carries both the
reservation tombstone and a clone-abort proof that differs from it, `linuxV2ForeignJournalIsSafelyRetired`
answers *safely retired* at its reservation arm (`linux_cgroup_journal.go:453-456`), whose conjuncts
all hold, before its clone arm (`:457-459`) is read; the guard refuses because its selector prefers
the clone proof (§4.1) and, since §4.4, requires the enclosing proof to enclose exactly the tombstone
it carries. The shape is unproducible: the Store's own journal validation refuses it with
and without a clone intent (§5's probes), so only the fixture's in-memory copy has that shape. The row
is therefore guard hardening, not a daemon-agreement row, and its name now says so; M3 reddens it and
the plan's §3 carries the design note.

Three further daemon arms are deliberately **not** mirrored:

- the clone-reuse-retired arm (`linux_cgroup_journal.go:435-441`): its shape needs a
  `LinuxCloneReuseGrant` retired before native start with no cgroup reservation, written only by the
  reuse-abort path (`daemon/internal/state/linux_clone_journal.go:517-518`); the shared arm's case
  sets drive no clone-reuse flow, but unlike the two arms below that exclusion is not derived from a
  producer constraint, so the apply round (§4.2) added this arm to the inventory and to the residual;
- the `LinuxCloneAbortProof`-only arm (`linux_cgroup_journal.go:457-459`): its cgroup-backed
  sub-shape requires the reservation tombstone beside it (`linuxCloneAbortHistoryMatches`,
  `daemon/internal/state/linux_clone_validation.go:388-409,396-401`), which is why the guard handles
  it through the reservation arm above; the sub-shapes with no `CgroupAbortProof` are **not**
  v1-era-only — the App's post-preparation failure path calls `abortLinuxV2CloneBeforeCheckoutLocked`
  (`daemon/internal/app/linux_owner_v2_linux.go:785-803`) and the workspace's builder sets
  `CgroupAbortProof` only when its caller supplies one
  (`daemon/internal/workspace/linux_clone_linux.go:1051-1069`), both current v2 code. What keeps the
  arm off the shared StateDir is the arm's own leg order (§3.1), never measured here: an earlier
  "pre-v2-era row" claim for it was wrong and the round-2 fix corrected it (R311-9);
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
- the tombstone arms are now *stricter* than the daemon's predicate in the two directions §4.4
  added: current leaf absence and the enclosing clone proof. Neither relaxes anything, and both
  refuse rather than pass when the evidence is missing;
- the leg-level validators that check each leg's *own* journal
  (`linuxV2ValidateStageReplayJournal` `:1406-1496`, `linuxV2ValidateStageJournalBoundary`,
  `linuxV2VerifyNoTargetCloneAbort` `:2223-2251`) are untouched, so the replay matrix's per-leg
  coverage is unchanged; and
- the acceptance script, the workflow, the exporter and every production package are byte-identical
  to the dispatch base (§1), so the `shared` arm and its credential-gated pi leg (1B1) are untouched.

### 4.1 The fixtures, and why the pre-gate one is a file

`linuxV2NeverCreatedTombstoneJournal` (`:1597-1609`), `linuxV2RemovedEmptyTombstoneJournal`
(`:1610-1618`), `linuxV2CloneTombstoneJournal` (`:1792-1796`) and
`linuxV2CloneRemovedEmptyTombstoneJournal` (`:1797-1801`) drive the production CAS chain —
`SaveJournal`, `ReserveLinuxCgroup`, then `AbortNeverCreatedLinuxCgroupReservation`, or
`MarkLinuxCgroupCreateStarted` → `BindLinuxCgroupLeaf` → `AbortLinuxCgroupReservation` with a
`removed_empty` proof; the two clone fixtures additionally drive `ReserveLinuxCloneIntent` →
`FenceLinuxCloneBeforeCheckout` → `CompleteLinuxCloneAbort` over the same tombstone — so each accepted
shape is written by the daemon's own producer, not by a literal. The shared steps live in three
helpers the four builders call — `linuxV2NeverCreatedAbortJournal` (`:1779-1791`),
`linuxV2RemovedEmptyAbortJournal` (`:1619-1653`) and the clone scaffolding
`linuxV2CloneAbortJournal` (`:1716-1778`) — so adding the round-2 row did not copy the clone chain:
the pair differs only in the callback it hands over, `linuxV2CloneTombstoneJournal` fencing before any
leaf exists (the shape the `reservation_durable` leg leaves) while
`linuxV2CloneRemovedEmptyTombstoneJournal` binds a leaf and retires it empty
(`LinuxCgroupAbortLeafRemovedEmpty`), the only fixture-covered clone-backed *post-create* shape. Both
carry no helper or helper-exit evidence, so the shared arm's own helper-bound tombstone, which the leg
validator requires to be helper-reaped (`linuxV2ValidatePreEffectAbortTombstone` `:1528-1544`), has no
committed positive fixture (§10, R311-8). `linuxV2PreMkdirAbortTombstone` (`:1802-1806`) now delegates
to the first builder, so #309's three tests keep the identical producer path.

`linuxV2PreGateReleaseTombstoneJournal` (`:1671-1715`) is a literal, because the platform is the only
issuer of the opaque pre-gate release (`platform.LinuxCgroupV2Release.SnapshotPreGate`, unexported
fields) and the Store refuses to import that tombstone outside its dedicated clear
(`daemon/internal/state/run_journal.go:275`, `:343`); the fixture is therefore written as the
daemon's own on-disk journal file (`linuxV2WritePriorJournalFile` `:1654-1670`, whose name comes from
`linuxV2StoreJournalFileName` `:3017-3020`, the digest the Store derives itself at
`daemon/internal/state/run_journal.go:544-556`) and then
re-read through the production Store (`LoadJournal`) so its `validateJournal` pass is production
evidence, not the fixture's claim. Its receipt shape is the state package's own pre-gate fixture
(`daemon/internal/state/linux_cgroup_v2_test.go:254-329`), with the authenticator produced by
`linuxgate.SignGateNeverProducedAuthenticator`. The held-stage fixture now writes through the same
`linuxV2WritePriorJournalFile` (apply round, §4.2), so one writer derives the name for both.

The `shared` StateDir snapshot is itself Store-validated since §4.4:
`linuxV2SnapshotStoreJournals` (`:2915-2941`) reads its candidates fail-closed
(`linuxV2ReadPriorJournalFiles` `:2942-2975`) and re-reads each one through a private production
Store (`linuxV2StoreJournalValidator` `:2976-3015`) — the live StateDir's own Store is locked by the
daemon or by the fixture's Store — so a journal the Store would refuse, or one whose file name does
not match its key, is a named snapshot failure instead of an invisible row.

`linuxV2StageReplayNoTargetAbort` (`:1497-1508`) also takes the deferred tidiness item #309 left in
`docs/TODO.md` with "the next slice that already touches this file" as its owner, which this slice
is: it no longer clones the reservation proof before discarding it when the nested clone proof is
preferred, but returns the preferred clone proof first, else the reservation proof, else nil,
cloning only the selected proof. The precedence and every result are unchanged (the mandatory
pre-mkdir path and every `aborted_no_target` leg keep the same selected proof) — the precedence note
that recorded it was itself dropped by §4.4 as restated implementation, and the plan's §2 keeps the
rationale — and §5's reordered-selector mutant reddens the clone-vs-reservation mismatch row alone,
the row that exists because the preference is load-bearing.

### 4.2 The simplify-apply round (2026-11-28)

Eight findings were accepted from the simplify review; six tidy-up findings and one record-accuracy
defect (PD-1) were applied to the delivered bytes, and the fourth finding (hoisting the two refusal
messages to package consts) was skipped, because a literal shared between producer and assertion makes
that message pin hollow. What changed:

- the guard's first condition lost the two conjuncts `HasPendingContainment` already covers, and the
  refusal test gained one row per dropped conjunct (`process authority on the fence tombstone`,
  `launch handoff fence on the fence tombstone`) so the surviving term is pinned by M11 (§5);
- `linuxV2CloneTombstoneJournal` and acceptance row 4 (`clone abort tombstone beside its
  reservation`) pin the accept direction of the shape the shared StateDir's own journal has: a
  reservation tombstone beside a clone-abort proof that equals it. §4.4's enclosing-proof check is the
  counterfactual that keeps that accept from admitting a clone proof which does not enclose the
  tombstone it carries (M18, §5);
- `linuxV2PriorLeafIdentity` (`:1581-1587`) replaced three hand-written four-line leaf literals; the
  single-caller store helper was inlined into `linuxV2NewPriorJournalFixtures` (`:1550-1564`); the
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

### 4.4 The panel round-2 fix round (2026-11-28)

The independent panel (Sol review, DeepSeek audit) confirmed the fix's direction and found two P1
defects on its delivered path, four P2 items (one fixture pin, two records corrections, one archival label), one P3
comment item and the PR-body refresh; all eight ruling items (R311-5…R311-12) were applied to the
delivered bytes. No production file, script, workflow or exporter changed,
and the plan's §5 rejection of exporting `linuxV2ForeignJournalIsSafelyRetired` still holds — the
snapshot instead re-reads the exact bytes through a private production Store. What changed:

- **current leaf absence on every arm (P1, R311-5).** The two tombstone arms returned before the
  release arm's `Lstat`, so the guard accepted a journal whose exact leaf path was currently occupied
  — the review's probe installed an entry at that path and the guard still returned nil. Every arm now
  ends in `linuxV2RequireAbsentPriorLeaf` (`:3170-3180`), which passes only on `os.ErrNotExist` and
  refuses on both a present entry and any other stat error; the leaf name is the tombstone's or the
  receipt identity's own (`:3149`, `:3155`, `:3167`). This is a new filesystem observation inside the guest's
  guard (§6, §9);
- **the snapshot applies the Store's own read, fail-closed (P1, R311-6a/b).** `readLinuxWitnessJournals`
  skipped undecodable rows and never validated decoded ones, and the previous snapshot classified
  whatever it decoded. `linuxV2ReadPriorJournalFiles` (`:2942-2975`) now fails on any undecodable
  journal-shaped file and on any read error other than absence, the file name must equal the digest
  the Store derives from the decoded key (`linuxV2StoreJournalFileName` `:3017-3020`), and each row is
  re-read through a private production `Store` (`linuxV2NewStoreJournalValidator` `:2983-2995` and
  its `load` `:3001-3015`), so a Store-refused journal is a named failure. Polling readers keep their
  tolerance: only the snapshot path is strict;
- **the enclosing clone proof is validated (P1, R311-6c).** The selector read a clone's nested
  tombstone without validating the proof that encloses it, so an equal nested proof was enough.
  `linuxV2RequireExactCloneAbortEnclosure` (`:3196-3206`) now requires `proof.Validate()`, the
  journal's own clone intent, no pending abort, no checkout/promotion receipt, no workspace path or
  recovery barrier, and the nested tombstone's equality with the reservation's stored one;
- **the clone-backed post-create row and its pin (P2, R311-7).** A mutant that rejects only the
  clone-backed *post-create* tombstone left both suites green, because the only clone-backed fixture
  fenced before the leaf existed. `linuxV2CloneRemovedEmptyTombstoneJournal` (`:1779-1801`) closes the
  fixture gap, `TestLinuxV2RetiredPriorJournalAllowsAFreshRun` gained that row (`:2052`) and
  `TestLinuxV2RetiredPriorJournalRefusesAPresentLeaf` (`:2109-2172`) plus
  `TestLinuxV2PriorJournalSnapshotRefusesStoreInvalidRows` (`:2173-2222`) add the committed
  present-leaf and Store-invalid rows; §5's M19 demonstrates the gap on the pre-fix bytes and the
  named red row on these;
- **records (P2, R311-8/9/10).** §7's account of the live helper-bound prior shape now names what the
  fixtures actually produce, §4's clone-abort-only bullet no longer calls a current v2 producer
  v1-era-only, §10 scopes the parity claim, and `docs/archive/CLOSED-TODO.md`'s rendered specimens are
  labelled where a sentence claims an exit code for them (§4.5);
- **comments (P3, R311-11).** The two added comments — the selector's precedence note (pre-§4.4 `:1496`) and
  the guard's daemon-mirroring note (pre-§4.4 `:2892-2893`) — were dropped as restated implementation and
  design rationale; the pre-gate fixture's adjacent `SaveJournal` refusal note stays, because it is a
  real test-construction trap. The plan's §2 keeps the selector rationale the first note carried;
- **the PR body (R311-12).** It is rewritten from the delivered record (§1's digests) rather than the
  pre-round record blob it quoted, and its own counts and anchors are §5's, §7's and §11's. Every
  anchor this round touched was re-measured after the last edit, which is what §11's "every anchor"
  claim rests on.

The pre-§4.4 bytes of the Go file (`213afc1c…`) and every receipt bound to them — including the
M1–M13 digests §5 recorded against them — are PARTIAL and SUPERSEDED by §1, §5 and §8: §5 re-measures
every surviving conjunct mutant against these bytes.

### 4.5 The archival label in `docs/archive/CLOSED-TODO.md` (P2, R311-10)

The record-sanitizer goal's closed entry quotes runnable specimens, and the apply round rendered their
host-shaped roots to the sanctioned `<HOST-PATH>`/`<TMP>` tokens, which the file-level
`model-routing-record-sanitize.py --check --root-only` gate requires. One quoted command then no
longer exits the way its sentence says, because the rendered spelling is not the recorded one. The
bytes stay rendered — restoring them would redden that gate — and the measurement is labelled instead:
the entry states that its host-shaped specimens are shown as the sanitizer renders them and that every
exit code, refusal and measurement in it is that of the recorded spelling, which a reader reproduces by
substituting the recorded root for the token. No claim about the original bytes is left unsupported.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`) from the `daemon/`
directory of the delivered worktree, on the *delivered* bytes §1 pins. Each mutant row is the
delivered file with one exact substring replaced, applied through `go test -overlay`, so the worktree
itself stayed clean; the mutant's own sha256 is in the table and the row names the conjunct the
replacement drops (the pairs themselves are not delivered, as the previous round's review recorded).
The pre-apply rows (`812593b9…`), the pre-fix-round rows (`6ddddb8c…`) and the whole M1–M12 table
§4.3 recorded (`213afc1c…`) are PARTIAL and SUPERSEDED by this table, because §4.4 changed the file
those digests were taken from.

Command for every row below (mutant rows add `-overlay <that row's overlay.json>`):
`go test ./e2e -run 'TestLinuxV2(RetiredPriorJournalAllowsAFreshRun|PriorJournalRetirementRefusesActiveOwners|RetiredPriorJournalRefusesAPresentLeaf|PriorJournalSnapshotRefusesStoreInvalidRows|ClaimOnlyPriorJournalRequiresPhysicalRelease)' -count=1`

| Row | sha256 (mutant) | Exit | Observed red row / message |
| --- | --- | --- | --- |
| Green, delivered bytes | §1 `09a3d4ca…` | 0 | 5 acceptance rows, 13 refusal rows, 5 present-leaf rows and 3 snapshot rows PASS (26 subtests over 5 top-level tests); each acceptance row also asserts the daemon's own `CheckLinuxV2NativeAdmissionBeforeClaim` accepts the same journal |
| Red M1, dispatcher drops the process marker | `c93c7bd54537874bcbf4a8c81df6560f619653f1070f9c1b984abe6592e06342` | 1 | `process_marker_on_the_fence_tombstone` alone: `prior RunJournal admission = <nil>, want a refusal naming "prior RunJournal may still own or require recovery of a process or leaf"` |
| Red M2, abort arm drops the nil-reservation guard | `5fc64ee7f03037836c6cdbfdc067c3ba58efe6a1d1ab8e3cef03388ef3a7b89f` | 1 | `clone_abort_proof_with_no_reservation` alone, with a nil-pointer panic in the guard |
| Red M3, abort arm drops the proof-equality guard | `6be4455d76985f25c4719b47d8d6dce7da074b570f7474ed321cb046d92fd784` | 1 | `clone_tombstone_that_differs_from_its_reservation_(guard_stricter_than_the_daemon)` alone: with the equality gone the row now reddens through §4.4's enclosure message (`prior RunJournal's clone abort does not enclose its exact cgroup tombstone`), not the abort-tombstone message it asserts |
| Red M4, abort arm drops `reservation.Validate()` | `64a6dcb3bb7dd16a42ba52877fef5a890110e0d85e35b16a9e899626bcf2dee3` | 1 | `reservation_that_no_longer_validates` alone: the abort-tombstone refusal |
| Red M5, abort arm drops the release-proof absence | `4bec2870b8083756f0a64bb17d32634b0c8df9295fb89bf080baec9c96e8a3b6` | 1 | `release_proof_beside_the_fence_tombstone` alone: the abort-tombstone refusal |
| Red M6, abort arm drops the pre-gate-proof absence | `87fe6880770aa7f8055ba44a079f19adeb0a7e1c045c52f6429b2b3da09552e4` | 1 | `pre-gate_proof_beside_the_fence_tombstone` alone: the abort-tombstone refusal |
| Red M7, pre-gate arm drops the release-proof absence | `26629e554704f5074a677cbe952ea8100556179e8c2bc32679fee14301286d6f` | 1 | `release_proof_beside_the_pre-gate_tombstone` alone: the pre-gate refusal |
| Red M8, pre-gate arm drops `proof.Validate()` | `39edba7e98937e265596f5b2871aa7ade3335282d73e6c0fa3b230b7ad1e2027` | 1 | `pre-gate_proof_without_its_removal_disposition` alone: the pre-gate refusal |
| Red M9, the pre-fix guard (both dispatch arms deleted) | `10ebb5e6b2b49a539aced6c657dc9019c45623785bad824bce1852ef8389d1e7` | 1 | all five acceptance rows and all five present-leaf rows (`retired prior RunJournal was refused as an owner: prior RunJournal may still own or require recovery of a process or leaf`, or `retired prior RunJournal whose exact leaf is absent was refused: …` — the wall, reproduced on the delivered fixtures) and ten of the thirteen refusal rows: both clone-proof rows, `reservation_that_no_longer_validates`, `release_proof_beside_the_fence_tombstone`, `pre-gate_proof_beside_the_fence_tombstone`, `release_proof_beside_the_pre-gate_tombstone`, `pre-gate_proof_without_its_removal_disposition` and the three enclosure rows. Only the three first-condition rows (authority, handoff, process marker) stay green |
| Red M10, the selector's two branches reordered | `0b69b85ab49dac95f393e1f0b371cd7e895915796200f3ce41831e575b129c22` | 1 | `clone_tombstone_that_differs_from_its_reservation_(guard_stricter_than_the_daemon)` alone: the reservation proof is selected instead of the preferred clone proof, so the section-4.4 enclosure check refuses instead and the row's abort-tombstone message assertion fails. Both clone acceptance rows stay green, because their two proofs are equal |
| Red M11, guard drops the pending-containment term | `3cde9da36af752bf51849af29784cc7b8caa1922a0ef60cff1891771fda2bd24` | 1 | `process_authority_on_the_fence_tombstone` and `launch_handoff_fence_on_the_fence_tombstone` alone: with the term gone the abort arm admits both journals |
| Red M13, the producer's ownership message drifts | `98de518a7047f5b66cc580ac03ec3dd9a48a8c5526cede2d80283401addbb340` | 1 | `TestLinuxV2ClaimOnlyPriorJournalRequiresPhysicalRelease` plus `process_authority_on_the_fence_tombstone`, `launch_handoff_fence_on_the_fence_tombstone` and `process_marker_on_the_fence_tombstone`: each row's message pin is independent of the producer's literal (F4), and the claim-only test's own inline `may still own or require recovery` substring is independent too |
| Red M14, every arm drops the current-absence check | `58773d95ad8b0e586fd0bee3e1f1981fd1d2add861f7f2b5b8651eaa62a29165` | 1 | all five `TestLinuxV2RetiredPriorJournalRefusesAPresentLeaf` rows — one per tombstone arm — each with `retired prior RunJournal with an entry at its exact leaf = <nil>, want the presence refusal`. This is the P1 probe: with an entry installed at the exact leaf path, every acceptance arm returns nil on the pre-§4.4 bytes' return discipline |
| Red M15, the snapshot skips an undecodable row | `6f51a6e1f0426071d93c40d45aa3bcc93650f990d96f81df48d83c30eff23a81` | 1 | `TestLinuxV2PriorJournalSnapshotRefusesStoreInvalidRows/undecodable_journal_file` alone: `snapshot of an undecodable prior RunJournal = rows=0 error=<nil>, want a named decode refusal` (the pre-§4.4 `continue`) |
| Red M16, the snapshot skips the Store read | `bb13633fde4427acd29b7559e01b97da7045a3c74cce8ee44abe15643ff0638b` | 1 | `TestLinuxV2PriorJournalSnapshotRefusesStoreInvalidRows/clone_evidence_the_Store_refuses_without_a_clone_intent` alone: `snapshot of a Store-refused prior RunJournal = rows=1 error=<nil>, want the Store refusal` — the review's own probe shape (an equal nested clone tombstone with no clone intent) walks straight past the guard on those bytes |
| Red M17, the snapshot drops the entry-name/key check | `cd2d6ea6e816e6ce38b196f2919624f6ff8cd29c8cfaa9324a64f5f6d749eafb` | 1 | `TestLinuxV2PriorJournalSnapshotRefusesStoreInvalidRows/journal_file_whose_name_does_not_match_its_key` alone: the refusal becomes the Store's own `run journal not found` for the key-derived name instead of the entry-name refusal the row asserts |
| Red M18, the abort arm drops the enclosing-proof check | `1e756740ae9f90dbb23f2e72949b717e83780373cdd85eac2e5d21c2de2d9368` | 1 | `clone_abort_tombstone_without_its_clone_intent`, `clone_abort_tombstone_with_a_foreign_enclosing_proof` and `clone_abort_tombstone_whose_enclosing_proof_lost_its_intent_binding` alone: `prior RunJournal admission = <nil>, want a refusal naming "prior RunJournal's clone abort does not enclose its exact cgroup tombstone"` |
| Red M19, the guard rejects only the clone-backed post-create tombstone | `9c7578aeb19e011bf7f6a5991f78119f43c01ec8c9e22ef10e1c6d903787899c` | 1 | `TestLinuxV2RetiredPriorJournalAllowsAFreshRun/clone_abort_tombstone_over_its_removed-empty_leaf` and `TestLinuxV2RetiredPriorJournalRefusesAPresentLeaf/clone_abort_tombstone_over_its_removed-empty_leaf` (the two rows §4.4 added for exactly this shape) plus the three enclosure rows, whose message assertion the mutant's own refusal pre-empts |
| Red M19 on the pre-§4.4 bytes (the fixture gap) | `d65525fa6427d084d33a5f086573e9e26a72966712b433f91def07ca6fcd290d` | 0 | **no failing row**: the same conjunct added to the `213afc1c…` guard leaves every row of that revision's two suites green, because no fixture there is a clone-backed post-create tombstone. This is R311-7's finding and why §4.4 added the fixture and the two rows above |

The M12 row §4.3 recorded ("abort arm refuses a clone proof beside the tombstone", reddening the
clone-tombstone acceptance row) is **retired, not re-measured**: the conjunct it named was a rejection
probe, and the delivered file replaces that rejection with §4.4's positive enclosure check, which M18
pins. Its digest bound `213afc1c…` and is PARTIAL and SUPERSEDED.

Neighbour rows, same environment:

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Targeted suite | the command above (WSL Noble) | 0 | `ok … daemon/e2e 0.651s`, 26 subtests PASS |
| Neighbour predicates | `go test ./e2e -run '^TestLinuxV2' -count=1` (WSL Noble) | 0 | `ok … daemon/e2e 1.443s` |
| Whole `e2e` package | `go test ./e2e -count=1` (WSL Noble) | 0 | `ok … daemon/e2e 2.071s` |
| Whole `e2e` package | `go test -count=1 ./e2e` (`daemon/`, Windows `go1.27.4`) | 0 | `ok … daemon/e2e 0.274s`; the changed file is `//go:build linux && amd64`, so Windows compiles it out and this row does not execute the new assertions |
| Vet | `go vet ./e2e` (both userlands) | 0 | no output |
| Format | `gofmt -l e2e` (both userlands) | 0 | no output |
| Daemon-retires probe (F5, unchanged, scratch) | scratch copy of the delivered `daemon/`, `go test ./internal/state -run 'TestFix1ProbeForeignCrossProofShape' -count=1 -v` | 0 | `PROBE producible=no validateJournal=run journal has Linux clone evidence without an intent`; `PROBE daemon_retires=yes` — the predicate answers *safely retired* on the mismatch shape, and the Store refuses to load it |
| Store-refuses probe (F5, unchanged, scratch) | the same copy with the probe file in `e2e`, `go test ./e2e -run 'TestFix1ProbeStoreRefusesMismatchedCloneProofBesideItsReservation' -count=1 -v` | 0 | `PROBE store_refuses=run journal Linux clone abort proof lacks exact no-gate history` — with an intent the equality demand (`linuxCloneAbortHistoryMatches`) is the refusal |

Every refusal this round claims names the mutant that reddens its own row: M1 pins the dispatcher's
process-marker conjunct, M2–M8 pin the seven conjunct removals of the two tombstone arms — the same
conjuncts §4.3's M-series pinned, re-measured here against these bytes — M11 pins the
`HasPendingContainment` term that carries the authority and handoff refusals, M13 pins the four
message pins, M14 pins the shared absence check on all five arms, M15–M17 pin the snapshot's three
fail-closed properties, M18 pins the enclosing-proof check and M10 pins the selector's precedence. The
acceptance rows are pinned from the other side: M9 reddens all five plus all five present-leaf rows,
M19 reddens the clone-backed post-create row alone (and is green on the pre-§4.4 bytes, §4.4's fixture
gap). What the rows pin is parity **for each shape they carry**, not for the daemon's whole predicate:
the guard is deliberately stricter than `linuxV2ForeignJournalIsSafelyRetired` in the two §4.4
directions, and three daemon arms have no row at all (§4, §10).

## 6. Which userland each property needed (standing rule)

The new rows are Go logic over fixtures in `t.TempDir()`: no permission, ACL, mount, cgroup or image
resolution decides their outcome, and the filesystem semantics they touch are ordinary directory
creation, file read-back and `Lstat` of a path the test itself creates (or of a path under a parent
that is itself a regular file), which no userland in the matrix treats differently. Noble is therefore
a sufficient userland for the rows themselves, and the runner-mirroring requirement (rule 17) does not
apply to them.

§4.4's absence check *is* a new filesystem observation inside the guard, and §4.4's review probe
recorded exactly that. In the guest it runs against the same `mount/delegated` tree the release arm
already `Lstat`s (`linuxV2LeafPath`), as the same A-owner, and its outcomes there are only two: the
tombstone arms are reached with a never-created or already-retired leaf name, whose `Lstat` is
`ENOENT` in every userland — including when the delegated parent itself does not exist yet — and the
release arm's existing `Lstat` was already exercised by the earlier legs' retained release journals in
the three #309 runs. No permission, ACL or mount semantic decides either outcome, so the Resolute
guest requirement does not attach to this assertion; what the guest *does* need to re-establish is that
the `helper_bound` leg now passes with the same leaf absent, which is the conductor's re-Bind (§9) and
is not claimed here. The leg's own userland is unchanged: `linux-native-v2-acceptance.yml` runs on
`ubuntu-latest` with the Resolute guest rootfs the fixture builds, and this increment adds no new
pipeline step or exported file there.

## 7. Does the conductor's Bind execute it?

All four new tests live in `daemon/e2e` and run in the `daemon` job's race pass (the `daemon_race_tests`
step, `.github/workflows/ci.yml:1055-1063`, running `test -race -count=1 -timeout 600s -json ./...`
through `scripts/ci/go-test-json-gate.py`) and locally; the `shared` Bind does not invoke `./e2e`
with these test names, so it does not execute them. What the Bind *does* execute is the fixed guard
itself, twice per `shared` run: at the `helper_bound` fixture creation over the `reservation_durable`
tombstone — the clone-tombstone shape acceptance rows 4 and 5 pin, which is why those rows exist — and,
if the arm reaches it, at the `receipt_persist_failure`/`literal_pid_reuse` fixture creation over the
`helper_bound` and ACK-loss `helper_bound` tombstones. Those two later fixture creations see a
leaf-bound retired reservation written by the `helper_bound` recovery itself; the committed
`removed_empty` row (row 2) is the same *post-create, no-helper* shape that the Store's own
`AbortLinuxCgroupReservation` CAS writes for a reservation with no helper, while the shared arm's
`helper_bound` tombstone additionally carries helper reaped/waited evidence
(`linuxV2ValidatePreEffectAbortTombstone` `:1528-1544`) that no committed positive fixture reproduces —
§10 records that as this round's residual, and the guard's acceptance of it rests on the store-valid
`reservation.Validate()`/`AbortProof.Equal` conjuncts both the row and the judge carry, not on a
fixture claim. The pre-gate arm is executed only if the arm reaches
`helper_launcher_response_loss` and its final journal carries the pre-gate tombstone; no run has been
measured there. The present-leaf rows and the refusal rows need a journal or a filesystem state no
Bind case produces, so they are attributed to the `daemon` job's race pass and to §5; the Store-invalid
snapshot rows are attributed the same way, because a Store-invalid journal in the shared StateDir
would stop the daemon's own admission scan first.


## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above (round-2 fix) | see §5 (`daemon/`, WSL Noble) | per §5 | 5 acceptance, 13 refusal, 5 present-leaf and 3 snapshot rows green (26 subtests); eighteen mutant rows red at their own named rows, and M19 green on the pre-§4.4 bytes; both F5 probes green; neighbour rows, `gofmt` and `vet` green in both userlands |
| Whole `e2e` package, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./e2e` (`daemon/`) | 0 | `ok … daemon/e2e 0.274s` (the changed file is compiled out there) |
| Whole `e2e` package, WSL Noble (`go1.27.0 linux/amd64`) | `go test -count=1 ./e2e` (`daemon/`) | 0 | `ok … daemon/e2e 2.071s` |
| Vet and format | `go vet ./e2e`; `gofmt -l e2e` (both userlands) | 0 / 0 | no output in either |
| Comment cap | the coordinator's owner comment-policy checker on the scratch commit carrying these bytes | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 5 changed files, 1805 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new; the notices name the 6451-line Go test file and the 5911-line `docs/archive/CLOSED-TODO.md` as outside the configured scope (both above the pinned `--max-lines 5000` ceiling) |
| Changed-file duplication, raised ceilings | the same script with `JSCPD_LIMITS` raised to `--max-lines 30000 --max-size 8mb` | 0 | 5 changed files, 1812 scanned sources, 1404 clones in the corpus, 0 tolerated among the added lines, none new, no notice — every changed file was scanned |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed record passes the root-only check |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed document passes the path and symbol check (746 path citations; the notices are pre-existing ignore-rule notes for the portal and `deps` paths) |
| Model-routing record sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | 0 | the four changed documents pass (the plan, this record, `docs/archive/CLOSED-TODO.md`, `docs/TODO.md`) on the delivered bytes; the apply round rendered `CLOSED-TODO.md`'s host-shaped spellings as `<HOST-PATH>`/`<TMP>` and this round adds none, dropping the one round-2 row that quoted a host command path (§4.5) |
| Exporter unit suite | `python3 -B scripts/ci/test-linux-v2-evidence.py` (`scripts/ci/`, this host) | 1 | 73 tests, 146 failures + 7 errors — and the *identical* failing-name set at the dispatch base (`git archive 3d377582 scripts`, both runs' sorted `FAIL:`/`ERROR:` lines hash to `d536ac9f…`). The exporter, the acceptance script and the workflow are byte-identical to the base, so this is an environmental failure of this host, not a regression from this change; CI runs the suite in its own job |
| Round-2 full Go suite, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./...` (`daemon/`, these bytes) | 0 | every package `ok` or `[no test files]`, no `FAIL`; the changed file is `//go:build linux && amd64`, so this run does not execute it |
| Apply-round full Go suite, Windows (superseded for the fix bytes) | `go test -count=1 ./...` (`daemon/`, the pre-fix bytes `6ddddb8c…`) | 0 | 30 rows: 27 `ok`, 3 `[no test files]`, no `FAIL` |
| Apply-round full Go suite, WSL Noble as root (superseded for the fix bytes) | `go test -count=1 ./...` (`daemon/`, the pre-fix bytes) | 1 | 32 rows: 29 `ok` including `ok … daemon/e2e 2.633s`, 3 `[no test files]`, two failing packages (`internal/platform`, `native/linux-gate/protocol`) with 14 failing test rows |
| Attribution, `internal/platform` | the same package at the head and in a `git archive 3d377582 daemon` tree, same environment | 1 and 1 | both: 13 rows of `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`; `daemon/internal/platform/anchor_shape_table_test.go` sha256 `1f6b070ac0ce9a2a8e036c11fa6212a1c98051cebd5b31e97d593f1163ff0841` identical on both sides |
| Attribution, `native/linux-gate/protocol` | the same package at the head and in the base tree | 1 and 1 | both: `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` alone, the same `phase=fd … closedir … errno=1` census rows. Round 2's non-root WSL `go test ./...` over these bytes reproduces that one package failure and nothing else |

The comment cap, both duplication runs, the record check, the citation check and the sanitize sweep
read the *committed* diff, so both rounds ran them on a scratch clone carrying the round's delivered
bytes of every changed file, base `3d377582` (the round-2 run carries §1's `09a3d4ca…` and `a1c5dce8…`); the record's own §8 text follows that clone commit, so those numbers bind the
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

The binding receipt must be the conductor's re-Bind on the delivered bytes: this round changes the
`daemon/e2e/production_linux_v2_owner_cli_test.go` blob (§1 `09a3d4ca…`) and every doc, so neither a
run that gated #309's `96306f49…` blob nor any run on the pre-fix bytes (`6ddddb8c…`) or on the
§4.3 bytes (`213afc1c…`) can bind it. The
three #309 scratch Binds (`46037316453`,
`46041720955`, `46045331565`; #309 record §7, §9, §10) are BASELINE ONLY: each gated a predecessor
witness blob, and each records the wall's `:224` guard stop.

What the re-Bind must report: the `shared` case-set exit status, the `verified …` sequence and its
first disagreement, the `reservation_durable` row (expected green, unchanged), the `helper_bound` leg
(expected: no `:224` guard stop — the guard retires the retained tombstone *and* now proves the exact
leaf absent there, §4.4), the first unexpected failure if the arm stops later in the replay matrix,
the credential-gated pi outcome verbatim in log, declaration and exported member, the export status
and the reader Result. Two of this round's changes are guest-visible and only the Bind can execute
them: the absence `Lstat` every arm now ends in, and the Store-validated snapshot read over the live
StateDir (§4.4, §6). Both fail closed — a present leaf or a Store-refused journal stops the leg with a
named guard message rather than passing it — so a green `helper_bound` leg is the evidence that the
shared StateDir's retained tombstone also satisfies them. The expected green outcome of the fixed rows
is §5's; the Bind is the acceptance, not this record.

## 10. Residuals and known limits

- The leg is CI-only: nothing here proves the `helper_bound` leg passes on the delivered bytes. The
  guard's accept direction is executed locally, and M9 reproduces the wall on the delivered fixtures,
  but the leg itself waits for the conductor's dispatch. In all three #309 runs the tombstone was the
  only pre-existing journal the guard could name, which is what §3.1's ordering argument rests on.
- If the arm stops later, the next stop is most likely a shape this fix did not mirror: the
  clone-reuse-retired arm, the `LinuxCloneAbortProof`-only arm or the legacy v1
  `ContainmentAuthority` + StopReceipt row (§4). None of the three is v1-era-only any more than the
  others are — the round-2 fix corrected that wording for the clone arm (§4) — and all three refuse
  with the existing ownership message, so the arm fails closed rather than passing silently;
  `docs/TODO.md` carries the residual item, and the apply round added the third arm to it.
- The shared arm's own `helper_bound` tombstone — a leaf-bound retirement whose abort proof carries
  helper reaped/waited evidence (`linuxV2ValidatePreEffectAbortTombstone` `:1528-1544`) — has **no
  committed positive fixture**: `linuxV2RemovedEmptyTombstoneJournal` (`:1610-1653`) and
  `linuxV2CloneRemovedEmptyTombstoneJournal` (`:1779-1801`) both retire a leaf with no helper and no
  helper exit (`linuxCgroupReservationAbortMatches`'s no-helper branch,
  `daemon/internal/state/linux_cgroup_journal.go:1121-1126`), which is the shape the Store itself
  writes for such a reservation. §7 no longer claims the live shape is the fixture; extending the
  fixtures to the helper-present variant is this round's residual (R311-8), owned by the next slice
  that needs that evidence.
- The clone-tombstone acceptance row (row 4) is inferred-reachable in the same sense as the pre-gate
  one: the guard's selector prefers the clone proof, so the `helper_bound` fixture creation exercises
  this row's shape only if the preceding leg's journal carries the clone proof
  `linuxV2VerifyNoTargetCloneAbort` demands; a leg reaching the arm without one selects the
  reservation proof instead, and row 1's shape is exercised there.
- The pre-gate acceptance arm is inferred-reachable, not observed.
  `linuxV2ValidateLauncherReadyLostAckSummary` (`:2580-2620`) permits `released` or
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
cited with that file's sha256; digests already in §1 are referenced there, not repeated. The panel
round-2 fix re-read every anchor of this record and the plan against the delivered files, because
§4.4 moved the whole Go file: each range below starts at the symbol, arm, row or comment the sentence
around it names, the two anchors that describe *pre-§4.4* lines say so in place (the two dropped
comments, §4.4), and every anchor into an unchanged file was re-checked against the same digest §1
names for it.

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | §1 `09a3d4ca…` | selector `linuxV2StageReplayNoTargetAbort` `:1497-1508`; held-stage disposition validator `:1509-1527`; pre-effect tombstone validator `:1528-1544`; prior-journal fixture type and store `:1545-1564`; tombstone reservation `:1565-1580`; leaf-identity builder `:1581-1587`; tombstone journal `:1588-1596`; never-created builder `:1597-1609`; removed-empty builder `:1610-1653`; journal-file writer `:1654-1670`; pre-gate builder `:1671-1715`; clone-tombstone builder `:1716-1778`; clone removed-empty builder `:1779-1801`; #309 helper `:1802-1806`; #309 tests and helpers `:1807-1869`, `:1870-1891`, `:1892-1925`, `:1926-1946`, `:1947-1975`; acceptance test `:1976-2008` with its clone rows `:1983-1984`, snapshot lookup `:1994-1997` and guard call `:2002`; refusal test `:2009-2108` with its three inline-literal owner rows `:2024-2033` and the three enclosure rows `:2077-2092`; present-leaf test `:2109-2172`; Store-invalid-row test `:2173-2222`; snapshot map type `linuxV2StoreJournalSnapshot` `:2909-2914`; snapshot `:2915-2941`; strict reader `:2942-2975`; Store validator type, constructor, close and load `:2976-3015`; journal-file name `:3017-3019`; journal-file predicate `:3021-3032`; owner-state builder `:3034-3068`; ownership const `:3138-3139`; guard `:3140-3169` with the owner-marker refusal `:3142`, the abort arm `:3145-3150`, the pre-gate arm `:3151-3156` and the release arm `:3157-3167`; absent-leaf check `:3170-3180`; abort tombstone `:3181-3195`; enclosure check `:3196-3206`; pre-gate tombstone `:3207-3213`; claim-only journal `:3214-3220` and its test `:3221-3233`; and the two anchors that name pre-§4.4 lines: the dropped selector note `:1496` and the dropped guard note `:2892-2893` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | §1 `98d4771c…` (dispatch base) | fixture `:1989`; retained-journal snapshot `:2093-2101` with the guard call `:2098` and the message `:2099`; the `:224` subtest invocation; StateDir assertion `:2078-2091`; leaf path `:3601-3603`; physical-release helper `:2738` |
| `daemon/internal/state/linux_cgroup_journal.go` | §1 `9358c264…` (dispatch base) | retirement predicate `:432-464` (pre-gate arm `:448-452`, abort arm `:453-456`, clone arm `:457-459`, v1 arm `:460-462`); reservation `Validate` `:50-79`; `validateProofs` `:157-172`; abort match `:1094-1130`; never-created producer `:903-928` with its policy comment `:898-902`; preclaim scan `:514-527` |
| `daemon/internal/state/run_journal.go` | §1 `093d93d9…` (dispatch base) | `HasPendingContainment` `:104-107`; `SaveJournal` `:254-360` with the pre-gate import refusal `:275`, `:343`; journal-file naming `:544-556` |
| `daemon/internal/state/linux_clone_validation.go` | — | clone-abort history `:388-409`; `linuxCloneAbortNoActiveOwner` `:439-442` |
| `daemon/internal/state/cgroup_release_linux.go` | — | pre-gate receipt `:17-52`; pre-gate clear `:149-191` with the tombstone write at `:188` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | — | the pre-gate receipt fixture `:254-329` with its receipt shape inside it |
| `scripts/ci/linux-v2-native-acceptance.sh` | §1 `42177bb8…` (dispatch base) | stage-replay leg runner `:2619-2651` with the request cleanup `:2647`; ACK-loss runner `:2677-2721` with its cleanup `:2717`; the #300 isolation note `:3148-3151`; legacy v1 leg `:3212-3217`; shared arm lists `:3298-3303`, `:3305-3306`, `:3309`; `DAEMON_SERVICE_STATE_DIR` `:520` |
| `.github/workflows/ci.yml` | — | the `daemon_race_tests` step `:1055-1063` |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | §1 `a1c5dce8…` | §1.4 fixture facts, §2 the dispatcher, the clone-proof-equal abort shape and the selector item, §3 the refusal rows, §4 the test plan, §5 the out-of-scope residuals and the round-2 correction note — all re-read on these bytes; the plan's Go-file anchors name this table's ranges, and the two it keeps at their dispatch-base values (the pre-fix guard and the claim-only test) say so |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the open Bind and residual entries and the closed stage items; not byte-pinned, because a later unrelated merge moves them |
