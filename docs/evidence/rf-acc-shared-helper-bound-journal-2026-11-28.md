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
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `812593b9afbb865bff6e9dcf39ac03068b6019db3c2a2957eccb3c20bdf888a8` | The fresh-run guard dispatches on the daemon's own retirement shapes (fenced abort tombstone, pre-gate leaf release) before its unchanged v2-StopReceipt release arm; two production-driven tombstone fixtures, one on-disk pre-gate fixture, one acceptance test and one refusal test; and the deferred no-target selector tidiness item taken here (§4.1). |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | `816a0cdeb0e60f1c3fe2f149eaf374a227a8c2fd744f94eec391d43c84292f06` | Stage-1 answers, the accepted fix, the arms deliberately not mirrored, the selector tidiness item taken here, the tests and the evidence plan. |
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
| Daemon State vocabulary | `daemon/internal/state/run_journal.go:101-107`, `HasPendingContainment` | not a pending owner: the reservation term is `ContainmentCgroup != nil && AbortProof == nil` |
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
(`daemon/internal/state/linux_cgroup_journal.go:903-916`), whose policy comment at `:898-902` states
the tombstone "remains until journal deletion so an unknown write can be replayed with the same
proof". Retention is designed behaviour, not a leak.

### 3.2 The fixture's own setup (the contract's first alternative)

The fixture neither releases nor reuses the preceding leg's journal, and it may not:

- each leg gets a fresh RunKey from Control (`scripts/ci/provision-linux-v2-control-claims.exs`,
  schedule mode, `Ecto.UUID.generate()`), so no leg adopts another leg's journal;
- the two `aborted_no_target` legs *require* their retained tombstone (`:1441-1472`);
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
(`daemon/e2e/production_linux_v2_owner_cli_test.go:2823-2854`) is now a dispatcher in the daemon's
own arm order, with its physical-release arm byte-identical:

1. owner-marker refusal at `:2825-2828` — `HasProcessDetails`, `HasPendingContainment`,
   `ContainmentUnproven`, `ContainmentAuthority`, `ContainmentHandoff` — one literal message
   (`linuxV2PriorJournalOwnershipRefusal` `:2821`);
2. fenced abort tombstone at `:2831-2833` → `linuxV2RequireRetiredPriorAbortTombstone`
   (`:2856-2864`): `ContainmentCgroup` present with a proof equal to the extracted one,
   `reservation.Validate()` (which itself runs `linuxCgroupReservationAbortMatches`,
   `daemon/internal/state/linux_cgroup_journal.go:1094-1131`, through `validateProofs` `:157-176`),
   and no release and no pre-gate proof beside it;
3. pre-gate release tombstone at `:2834-2836` → `linuxV2RequireRetiredPriorPreGateTombstone`
   (`:2866-2871`): `proof.Validate()` and no release proof beside it. `Validate()` implies the
   version and the `GateOutcome == never_created` conjuncts the daemon spells out at
   `linux_cgroup_journal.go:448-452`, and the dispatcher's earlier terms already exclude a live
   reservation and every active marker;
4. the unchanged release arm `:2837-2854`: v2 `StopReceipt`, disposition ∈ {removed,
   absent_after_receipt} and the exact leaf-absence `Lstat`.

Two further daemon arms are deliberately **not** mirrored, because no leg of any case set leaves that
shape in the shared StateDir:

- the `LinuxCloneAbortProof`-only arm (`linux_cgroup_journal.go:457-459`): a cgroup-backed clone
  abort requires the reservation tombstone beside it (`linuxCloneAbortHistoryMatches`,
  `daemon/internal/state/linux_clone_validation.go:388-408`), and the bare legacy shape is a
  pre-v2-era row;
- the legacy v1 `ContainmentAuthority` + StopReceipt arm (`linux_cgroup_journal.go:460-462`): the
  only v1 leg (`run_legacy_v1_replay_witness`, `scripts/ci/linux-v2-native-acceptance.sh:3212-3217`)
  drives `TestProductionLinuxV2LegacyV1ReplayNoV2Receipt`, whose `runLinuxStopResponseLostCase` uses
  its own witness state root, not the shared StateDir.

Both stay refused with the existing ownership message, so a row of either shape stalls the arm with
the same diagnostic as before rather than passing silently; §10 names them.

Not weakened:

- the release arm keeps every conjunct and the leaf `Lstat`; no release requirement is relaxed;
- the tombstone arms demand the daemon's own evidence and refuse any active-owner marker, so a
  tombstone that still owns a process, authority, handoff or scan is refused exactly as before (§5's
  refusal rows, M1);
- the leg-level validators that check each leg's *own* journal
  (`linuxV2ValidateStageReplayJournal` `:1404-1493`, `linuxV2ValidateStageJournalBoundary`,
  `linuxV2VerifyNoTargetCloneAbort` `:2007-2034`) are untouched, so the replay matrix's per-leg
  coverage is unchanged; and
- the acceptance script, the workflow, the exporter and every production package are byte-identical
  to the dispatch base (§1), so the `shared` arm and its credential-gated pi leg (1B1) are untouched.

### 4.1 The fixtures, and why the pre-gate one is a file

`linuxV2NeverCreatedTombstoneJournal` (`:1594-1612`) and `linuxV2RemovedEmptyTombstoneJournal`
(`:1614-1654`) drive the production CAS chain — `SaveJournal`, `ReserveLinuxCgroup`, then
`AbortNeverCreatedLinuxCgroupReservation`, or `MarkLinuxCgroupCreateStarted` → `BindLinuxCgroupLeaf`
→ `AbortLinuxCgroupReservation` with a `removed_empty` proof — so each accepted shape is written by
the daemon's own producer, not by a literal. `linuxV2PreMkdirAbortTombstone` (`:1721-1724`) now
delegates to the first builder, so #309's three tests keep the identical producer path.

`linuxV2PreGateReleaseTombstoneJournal` (`:1673-1719`) is a literal, because the platform is the only
issuer of the opaque pre-gate release (`platform.LinuxCgroupV2Release.SnapshotPreGate`, unexported
fields) and the Store refuses to import that tombstone outside its dedicated clear
(`daemon/internal/state/run_journal.go:275`, `:343`); the fixture is therefore written as the
daemon's own on-disk journal file (`linuxV2WritePriorJournalFile` `:1656-1671`, the same
name↔key derivation the Store enforces at `daemon/internal/state/run_journal.go:544-555`) and then
re-read through the production Store (`LoadJournal`) so its `validateJournal` pass is production
evidence, not the fixture's claim. Its receipt shape is the state package's own pre-gate fixture
(`daemon/internal/state/linux_cgroup_v2_test.go:268-292`), with the authenticator produced by
`linuxgate.SignGateNeverProducedAuthenticator`.

`linuxV2StageReplayNoTargetAbort` (`:1495-1506`) also takes the deferred tidiness item #309 left in
`docs/TODO.md` with "the next slice that already touches this file" as its owner, which this slice
is: it no longer clones the reservation proof before discarding it when the nested clone proof is
preferred, but returns the preferred clone proof first, else the reservation proof, else nil,
cloning only the selected proof. The precedence and every result are unchanged (the mandatory
pre-mkdir path and every `aborted_no_target` leg keep the same selected proof), the one-line
precedence note beside the selector records why the clone proof wins, and §5's reordered-selector
mutant reddens the clone-vs-reservation mismatch row alone — the row that exists because the
preference is load-bearing.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`) from the `daemon/`
directory of the delivered worktree, on the *delivered* bytes. The mutant rows ran in a scratch copy
of `daemon/` with one string replaced in the delivered file; each mutant's own sha256 is in the
table, the delivered file's digest is §1's `812593b9…`, and `cp` of the delivered file restored it
between runs (the restored row re-measures to the delivered digest and exit 0).

Command for every row below:
`go test ./e2e -run 'TestLinuxV2(RetiredPriorJournalAllowsAFreshRun|PriorJournalRetirementRefusesActiveOwners)' -count=1`

| Row | sha256 (mutant) | Exit | Observed red row / message |
| --- | --- | --- | --- |
| Green, delivered bytes | §1 `812593b9…` | 0 | 3 acceptance rows and 8 refusal rows PASS; each acceptance row also asserts the daemon's own `CheckLinuxV2NativeAdmissionBeforeClaim` accepts the same journal |
| Red M1, dispatcher drops the process marker | `f3c5cbbf8a9e3c36543ff40ef700760c9b8482d053a12c16e70483dfddf1f293` | 1 | `process_marker_on_the_fence_tombstone`: `prior RunJournal admission = <nil>, want a refusal naming "prior RunJournal may still own or require recovery …"` |
| Red M2, abort arm drops the nil-reservation guard | `69e11c0c1a49ddd30967d15b85562e562b4892809486bf66433130b3aced81a3` | 1 | `clone_abort_proof_with_no_reservation`, with a nil-pointer panic in the guard |
| Red M3, abort arm drops the proof-equality guard | `99ec092648d6b4bc2a1310aba2189c06fbe01908b64ea27e2a989a9c204c2d19` | 1 | `clone_tombstone_that_differs_from_its_reservation`: `admission = <nil>, want a refusal naming "prior RunJournal's abort tombstone is not an exact fenced pre-effect retirement"` |
| Red M4, abort arm drops `reservation.Validate()` | `74754a4f39aa281de0a5b4ecb7bcb475c206ddae32f9c78c785e8ac704710c37` | 1 | `reservation_that_no_longer_validates`: the same abort-tombstone refusal |
| Red M5, abort arm drops the release-proof absence | `b731e579a2af3981987cda03656dd52f038276294fef4d1bbc5a58827e704c4c` | 1 | `release_proof_beside_the_fence_tombstone`: the same abort-tombstone refusal |
| Red M6, abort arm drops the pre-gate-proof absence | `e77d97a2793f0917cf9151a3121321101cd428a5fc329bb156c93fbfcddcde27` | 1 | `pre-gate_proof_beside_the_fence_tombstone`: the same abort-tombstone refusal |
| Red M7, pre-gate arm drops the release-proof absence | `21c17e8448c1811d251a256c8e37ef34afbdc260a156fd8c1433a2556b6cba4f` | 1 | `release_proof_beside_the_pre-gate_tombstone`: the pre-gate refusal |
| Red M8, pre-gate arm drops `proof.Validate()` | `f4705b4510b4eef723733ead0afcfcb2feddbe48e264926706e0c0eb76f45f4e` | 1 | `pre-gate_proof_without_its_removal_disposition`: the pre-gate refusal |
| Red M9, the pre-fix guard (both dispatch arms deleted) | `8c0920cd719536e7c04c48c83a5e046430f11ed4e712a6438345551daac81a48` | 1 | all three acceptance rows: `retired prior RunJournal was refused as an owner: prior RunJournal may still own or require recovery of a process or leaf` — the wall, reproduced on the delivered fixtures |
| Red M10, the selector's two branches reordered | `147fc246cae277f40ae55a034f276e81094e840e750e030747a5a2e684e9dfb0` | 1 | `clone_tombstone_that_differs_from_its_reservation` alone: the reservation proof would be selected instead of the preferred clone proof, so the mismatch row's journal is admitted |

Neighbour rows, same environment:

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Neighbour predicates | `go test ./e2e -run '^TestLinuxV2' -count=1` | 0 | `ok … daemon/e2e 0.829s` |
| Whole `e2e` package | `go test ./e2e -count=1` | 0 | `ok … daemon/e2e 1.438s` |
| Vet | `go vet ./e2e` | 0 | no output |
| Format | `gofmt -l e2e` | 0 | no output |

Every refusal this round claims names the mutant that reddens its own row: M1 pins the dispatcher's
process-marker conjunct, M2-M8 pin the eight conjuncts of the two new arms, and M10 pins the
selector's precedence. The acceptance rows are pinned from the other side: M9 (the pre-fix guard)
reddens all three, and each row's second assertion — the daemon's own admission scan — fails if the
guard ever admits more than the daemon retires.

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
itself, twice per `shared` run: at the
`helper_bound` fixture creation over the `reservation_durable` tombstone (acceptance rows' shape,
`M9`'s counterfactual) and, if the arm reaches it, at the `receipt_persist_failure`/`literal_pid_reuse`
fixture creation over the `helper_bound` and ACK-loss `helper_bound` tombstones (the
`removed_empty` shape, acceptance row 2). The pre-gate arm is executed only if the arm reaches
`helper_launcher_response_loss` and its final journal carries the pre-gate tombstone; no run has been
measured there. The refusal rows need a journal whose shape contradicts the guard; no Bind case
produces one, so they are attributed to the `daemon` job's race pass and to §5.

## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above | see §5 (`daemon/`, WSL Noble) | per §5 | 3 acceptance rows and 8 refusal rows green; ten mutants red, each at its own named row; neighbour rows, `gofmt` and `vet` green |
| Full Go suite, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./...` (`daemon/`) | 0 | 30 rows: 27 `ok`, 3 `[no test files]`, no `FAIL` |
| Full Go suite, WSL Noble as root (`go1.27.0 linux/amd64`) | `go test -count=1 ./...` (`daemon/`) | 1 | 32 rows: 29 `ok` including `ok … daemon/e2e 2.113s`, 3 `[no test files]`, two failing packages (`internal/platform`, `native/linux-gate/protocol`) with 14 failing test rows |
| Attribution, `internal/platform` | the same package at the head and in a `git archive 3d377582 daemon` tree, same environment | 1 and 1 | both: 13 rows of `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`; `daemon/internal/platform/anchor_shape_table_test.go` sha256 `1f6b070ac0ce9a2a8e036c11fa6212a1c98051cebd5b31e97d593f1163ff0841` identical on both sides |
| Attribution, `native/linux-gate/protocol` | the same package at the head and in the base tree | 1 and 1 | both: `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` alone, the same `phase=fd … closedir … errno=1` census rows |
| Comment cap | the coordinator's owner comment-policy checker on the scratch commit carrying these bytes | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 5 changed files, 1805 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new; notices that the 6110-line Go test file and the 5895-line `docs/archive/CLOSED-TODO.md` carry added lines jscpd did not scan, both above the pinned `--max-lines 5000` ceiling |
| Changed-file duplication, raised ceilings | the same script with `JSCPD_LIMITS` raised to `--max-lines 30000 --max-size 8mb` | 0 | 5 changed files, 1812 scanned sources, 1404 clones, 0 tolerated among the added lines, none new, no notice — every changed file was scanned |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed record passes the root-only check |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo <scratch clone> --base 3d377582 --head HEAD` | 0 | 1 changed document passes the path and symbol check (744 path citations; the notices are pre-existing ignore-rule notes for the portal and `deps` paths) |

The comment cap, both duplication runs, the record check and the citation check read the *committed*
diff, so they ran on a scratch clone of this branch carrying the delivered bytes of every changed
file, base `3d377582`; the record's own final append follows that clone commit, so those numbers bind
the changed-file scope and the §1 content digests rather than a revision that cannot name itself —
the next commit that touches this record is the deliverable's own. The full-suite rows ran on the
delivered Go bytes: the changed Go file is `//go:build linux && amd64`, so Windows compiles it out
entirely and the WSL runs execute it inside `daemon/e2e`, whose `ok` row appears in both runs. Neither
WSL failure is in the changed scope, and both reproduce at the dispatch base in the same environment.

## 9. Next Bind

The binding receipt must be the conductor's re-Bind on the post-fix bytes: this round changes the
`daemon/e2e/production_linux_v2_owner_cli_test.go` blob (§1 `812593b9…`) and the plan, so no run that
gated #309's `96306f49…` blob can bind it. The three #309 scratch Binds (`46037316453`,
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
- If the arm stops later, the next stop is most likely a shape this fix did not mirror: the bare
  `LinuxCloneAbortProof` row or the legacy v1 `ContainmentAuthority` + StopReceipt row (§4). Both
  refuse with the existing ownership message, so the arm fails closed rather than passing silently;
  `docs/TODO.md` carries the residual item.
- The pre-gate acceptance arm is inferred-reachable, not observed.
  `linuxV2ValidateLauncherReadyLostAckSummary` (`:2364-2403`) permits `released` or
  `pre_gate_released` for `helper_launcher_response_loss`; only the second shape reaches the new arm,
  and no run has been measured at that leg.
- The pre-gate fixture is a literal imported as a journal file (§4.1): the platform is the only issuer
  of that proof, so its fidelity rests on the Store's own read validation at construction, the
  daemon's admission scan in the acceptance row, and the state package's receipt fixture. A drifted
  platform shape reddens the fixture construction or the acceptance row rather than passing silently.
- The guard still refuses a prior journal that carries a release proof *and* a tombstone (§5's
  refusal rows), matching the daemon's arm order; such a row cannot be produced by the Store, so the
  refusal is a malformed-input guard, not a live-path behaviour claim.
- The two WSL suite failures (§8) are environmental and pre-existing; neither package is in the
  changed scope, and both were reproduced at the base revision in the same environment.

## 11. Anchor re-measurement (delivered bytes)

Every line anchor this record cites was re-read on the delivered bytes after the last edit and is
cited with that file's sha256; digests already in §1 are referenced there, not repeated.

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | §1 `812593b9…` | selector `:1495-1506` and its precedence note `:1496`; fixture store `:1544-1556`; fixtures `:1563-1567`; tombstone reservation `:1569-1583`; tombstone journal `:1585-1592`; never-created builder `:1594-1612`; removed-empty builder `:1614-1654`; journal-file writer `:1656-1671`; pre-gate builder `:1673-1719`; #309 helper `:1721-1724`; #309 tests `:1726-1787`, `:1813-1844`, `:1877-1904`; acceptance test `:1906-1931` with its guard call `:1926`; refusal test `:1933-2005` with its assertion `:1999-2002`; clone-abort validator `:2007-2034`; leg validator gate `:2083-2184`; launcher-ready summary `:2364-2403`; release arm of the leg validator `:1429-1432`; recovery map `:1153-1161`; journal validator `:1404-1493` with `aborted_no_target` `:1441-1472`; held-stage binding `:1508-1525`; snapshot predicate test `:2763-2819`; guard `:2823-2854` (markers `:2825-2828`, abort dispatch `:2831-2833`, pre-gate dispatch `:2834-2836`, release arm `:2837-2853`); abort helper `:2856-2864`; pre-gate helper `:2866-2871`; claim-only test `:2880-2889`; pid-reuse leg `:3750-4074`; ack-loss legs `:4816-4980`; helper-loss legs `:4982-5229`; receipt leg `:5551-5685` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | §1 `98d4771c…` (dispatch base) | fixture `:1989`; retained-journal snapshot `:2093-2101` with the guard call `:2098` and the message `:2099`; the `:224` subtest invocation; StateDir assertion `:2078-2091`; leaf path `:3601-3603`; physical-release helper `:2738` |
| `daemon/internal/state/linux_cgroup_journal.go` | §1 `9358c264…` (dispatch base) | retirement predicate `:432-464` (pre-gate arm `:448-452`, abort arm `:453-456`, clone arm `:457-459`, v1 arm `:460-462`); reservation `Validate` `:50-74`; `validateProofs` `:157-176`; abort match `:1094-1131`; never-created producer `:903-916` with its policy comment `:898-902`; preclaim scan `:514-529` |
| `daemon/internal/state/run_journal.go` | §1 `093d93d9…` (dispatch base) | `HasPendingContainment` `:101-107`; `SaveJournal` `:254-360` with the pre-gate import refusal `:275`, `:343`; journal-file naming `:544-556` |
| `daemon/internal/state/linux_clone_validation.go` | — | clone-abort history `:388-409`; `linuxCloneAbortNoActiveOwner` `:439-442` |
| `daemon/internal/state/cgroup_release_linux.go` | — | pre-gate receipt `:17-52`; pre-gate clear `:149-191` with the tombstone write at `:188` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | — | the pre-gate receipt fixture `:254-329` with its receipt shape inside it |
| `scripts/ci/linux-v2-native-acceptance.sh` | §1 `42177bb8…` (dispatch base) | stage-replay leg runner `:2619-2651` with the request cleanup `:2647`; ACK-loss runner `:2677-2721` with its cleanup `:2717`; the #300 isolation note `:3148-3151`; legacy v1 leg `:3212-3217`; shared arm lists `:3298-3303`, `:3305-3306`, `:3309`; `DAEMON_SERVICE_STATE_DIR` `:520` |
| `.github/workflows/ci.yml` | — | the `daemon_race_tests` step `:1055-1063` |
| `docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md` | §1 `816a0cde…` | §1.4 fixture facts, §2 the dispatcher and the selector item, §3 the refusal rows, §4 the test plan, §5 the out-of-scope residuals — all re-read; the anchors §2 cites in the witness and the daemon predicate are the ones measured in this table |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the open Bind and residual entries and the closed stage items; not byte-pinned, because a later unrelated merge moves them |
