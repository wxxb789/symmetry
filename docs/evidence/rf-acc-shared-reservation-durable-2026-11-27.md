# rf-acc-shared-reservation-durable — evidence record (the pre-create tombstone the `reservation_durable` leg refuses)

Goal `rf-acc-shared-reservation-durable` (contract under the GDD contracts root, ruled by the
conductor 2026-11-27; dispatch gate #308 at `ac04f3e2`). Branch
`refactor/rf-acc-shared-reservation-durable`, dispatch base
`ac04f3e2ec8b48c9f28a6f7ed66f9abc87a3882a`. Plan
`docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md`. Shape reference:
`docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md` (committed, tested, allowlisted-evidence
increment). The digests below bind the delivered bytes; the stage's own gate receipts name the
scratch clone whose changed files are byte-identical to them.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `dba2daec309c4ee01eda195ce0539d2c59e67b718a93e847f0dc12826d258ebd` | The `never_created` disposition switch is a named predicate demanding `CreateStarted=false, LeafNeverAppeared=false`; the held stage binds the disposition (`linuxV2ValidateHeldStageAbortDisposition`); three committed tests drive the production tombstone, the one-field clause mutants and both wrong-stage directions. |
| `docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md` | `c73df5b36065c975446ea7f1a25a15b4608bf64ddf7b99d35bf1651f6fd87974` | Stage-1 read, the accepted decisions and the held-stage binding. |
| `docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md` | this record | Stage-1 answers, the fix, the receipts. |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | The closed stage-1/2 items and the open Bind/residual entries. |

This fix round supersedes the earlier stage-2 bytes: the Go blob `9c36f90f198e27a5a380ad336cda80ab4f745379831772256abba8a951be32cc`, and
every receipt, digest and line anchor in this record that named it, is PARTIAL and SUPERSEDED by the
bytes above. §5 labels each earlier receipt; §11 re-measures every anchor on the delivered bytes.

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
predicate, at delivered `:1534`), the authority rule (then `authority.go:1251`, now
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
`reservation.CreateStarted == false` (`:1617-1620` at the dispatch base; delivered `:1871-1874`), so
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
  (`:1612-1628`), red-capable per §5's M1-M4 mutants; the `removedEmpty` row (`:1629-1632`) walks
  the *sibling* `removed_empty` arm and is not evidence for the never-created conjuncts, and the
  `unknown` row (`:1633-1636`) pins the default arm.
- The stage binding: the predicate alone accepted either pre-effect disposition at whichever
  `aborted_no_target` hold reached it, and `linuxV2ExpectedStageReplayRecovery` maps two holds there
  (`reservation_durable`, `helper_bound`). `linuxV2ValidateStageReplayJournal` now takes the held
  `stage` and refuses any proof whose disposition is not that hold's
  (`linuxV2ValidateHeldStageAbortDisposition`, delivered `:1512-1529`, called at `:1448-1451`);
  `reservation_durable` requires `never_created`, `helper_bound` requires a leaf-bound disposition,
  and any other held stage presenting a no-target tombstone is refused. The abort proof is read by
  the same extraction (`linuxV2StageReplayNoTargetAbort`, `:1499-1510`) that the identity, fence,
  `ValidFor` and helper-reap checks already used; those checks are unchanged and still run.
- Row mapping: the production pre-mkdir tombstone must be accepted → the green row of
  `TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape`; the create-CAS, helper, helper-exit and
  claimed-absence shapes must stay refused → its `createStarted`, `helper`, `helperExit` and
  `claimedAbsent` rows; the wrong-stage directions → `TestLinuxV2HeldStageBindsThePreEffectAbortDisposition`
  (a *valid* `removed_empty` proof refused at `reservation_durable`, a *valid* pre-mkdir proof
  refused at `helper_bound`, and both accepted at their own hold) and
  `TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage`, which drives the same
  four directions through the replay validator the leg calls; the old clause's contradiction of the
  producer → the §5 M5 polarity mutant. No contract invariant of this change lacks an executable
  row.
- Unchanged: `authority.Validate`, the state producer, the tombstone semantics and json fields, the
  boundary validators, the replay stage list, the acceptance script and the exporter. The `shared`
  arm's credential-gated pi leg (1B1) is untouched, so the gated outcome is recorded exactly as
  before.
- Adversarial satisfaction, clause: the `reservation_durable` acceptance needs a tombstone that is
  `never_created`, `!CreateStarted`, `!LeafNeverAppeared` and carries no leaf identity, helper,
  helper exit, launcher or launcher exit, and it must pass `authority.Validate`. The only real
  producer of that shape is `state.Store.AbortNeverCreatedLinuxCgroupReservation` under the fence
  the boundary validator already requires (`:1871-1874`); the committed rows drive that producer
  rather than a literal, and the refusal rows use proofs that pass `Validate` (the `removed_empty`
  fixture asserts it), so an invalid proof cannot satisfy them.
- Adversarial satisfaction, binding: without the stage binding an `aborted_no_target` hold could be
  satisfied by the *other* hold's valid disposition — the real event that could do it is a recovery
  that lost the durable reservation while a `LinuxCloneAbortProof` still carried a cgroup tombstone
  (the extraction prefers the clone tombstone and the `ValidFor` binding is skipped when
  `ContainmentCgroup` is absent, `:1455-1472`). The binding refuses that direction for both holds
  before the clone-abort, identity and `ValidFor` checks run; §5's M6/M7 mutants show
  the rows that go red when it is removed or neutered, and the third and fourth replay rows show the
  admitted direction reaching the next check instead, so the refusal message is attributable to the
  binding.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`, the toolchain the
module cache pins) from the `daemon/` directory of the delivered worktree, on the *delivered* bytes.
Each mutant is the delivered file with one line changed in a scratch copy of `daemon/`, identified
by its own sha256 in the appendix below; the delivered file's digest is §1's `dba2daec…` and was
re-verified by `sha256sum` after every mutant run. The earlier stage-2 table for `9c36f90f…` is
SUPERSEDED (§1).

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Green, all three tests, delivered bytes | `go test ./e2e -run 'TestLinuxV2HeldStageBindsThePreEffectAbortDisposition\|TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape\|TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage' -count=1 -v` | 0 | 3 parents and all 15 subtests PASS, including `reservation_durable accepts its own pre-mkdir tombstone`, `helper_bound accepts its own removed-empty tombstone` and the two replay rows that reach the next check |
| Red `createStarted` | mutant M1 (§ appendix) | 1 | `:1642: mutated pre-mkdir tombstone error = <nil>, want "pre-create abort tombstone reports a target or leaf creation"` |
| Red `helper` | mutant M2 | 1 | the same `:1642` line, red at the `helper` row |
| Red `helperExit` | mutant M3 | 1 | the same `:1642` line, red at the `helperExit` row |
| Red `claimedAbsent` | mutant M4 | 1 | the same `:1642` line, red at the `claimedAbsent` row |
| Red, old clause polarity | mutant M5 | 1 | `:1599: the persisted pre-mkdir tombstone was refused: pre-create abort tombstone reports a target or leaf creation` |
| Red, binding not called | mutant M6 | 1 | the two replay rows, `helper_bound replay carrying a valid pre-mkdir tombstone` and `reservation_durable replay carrying a valid removed-empty tombstone`, each with the clone-abort message (`:1756` in the mutant) |
| Red, binding neutered | mutant M7 | 1 | the three held-stage refusal rows plus those two replay rows (`:1760` in the mutant) |
| Neighbour predicates | `go test ./e2e -run 'TestLinuxV2' -count=1` | 0 | `ok … daemon/e2e 0.730s` |
| Whole `e2e` package | `go test ./e2e -count=1` | 0 | `ok … daemon/e2e 1.469s` |
| Related packages | `go test ./internal/state ./internal/authority ./internal/app -count=1` | 0 | `ok … 12.801s`, `ok … 0.030s`, `ok … 162.102s` |
| Race pass (CI home) | `go test -race ./e2e -run 'TestLinuxV2HeldStageBindsThePreEffectAbortDisposition\|TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape\|TestLinuxV2StageReplayJournalBindsTheAbortDispositionToTheHeldStage' -count=1` | 0 | `ok … daemon/e2e 1.247s` |
| Producer-shape pin | `go test ./internal/state -run '^TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence$' -count=1` | 0 | `ok … 0.307s` |
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
blanket rejection; in both mutants the two *accept* directions keep running to the next check.

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
file restored it between runs. Parenthesised line numbers are the delivered file's (`:1534` clause,
`:1512-1529` binding, `:1759` replay assertion).

- `3be5062aa56f160c4566cd75aade515786796e6e880beebe37753602a55328eb` — M1, clause `:1534` without
  `abort.CreateStarted || `; reddens the `createStarted` row (exit 1).
- `06cf0e20c724a302c4f5a642a753d529961dedef9eb58239993b90a35cd04a5e` — M2, clause `:1534` without
  `abort.Helper != nil || `; reddens the `helper` row (exit 1).
- `f2cc51778eb1bd42598a505db89b23e76c813077f5de92782181f8e2b0c0438f` — M3, clause `:1534` without
  ` || abort.HelperExit != nil`; reddens the `helperExit` row (exit 1).
- `41b132329a5bbace3a35c62cddfc85a7946f443adb98b35c7aad5ded4805f8c2` — M4, clause `:1534` without
  `abort.LeafNeverAppeared || `; reddens the `claimedAbsent` row (exit 1).
- `29f6274a7bc2d5b990feca8870225366220081a60778ff5340a43ccb5f4fa8c5` — M5, clause `:1534` with the
  old polarity `!abort.LeafNeverAppeared`; reddens the green baseline row (exit 1).
- `3bdec5f57372256a2a627b13f91d561652b69fd9778ee29eab89f42fc247ec4e` — M6, the
  `linuxV2ValidateHeldStageAbortDisposition` call deleted from `:1448-1451`; reddens the two replay
  rows (exit 1).
- `011fd14c3f0a79c163ad8ec42115082e3a34640cf0a2743cd0fd32e69a988140` — M7, `:1512` returning nil
  before the switch; reddens the three held-stage refusal rows and the two replay rows (exit 1).

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
execute is the fixed clause and the binding's accept direction, through
`TestProductionLinuxV2StageCrashReplayMatrix`: the `reservation_durable` leg presents a
`never_created` tombstone at `reservation_durable` (the binding's first accept arm) and the
`helper_bound` leg presents a leaf-bound tombstone at `helper_bound` (its second). The binding's
two refusal arms need a journal whose disposition contradicts the held stage; no case in the
Bind's set produces one, so those rows are attributed to the `daemon` job's race pass (and to §5's
local rows), not to the Bind. The fix also reloads no fixture, so the Bind needs no new step. The
arm's later legs and the export/reader tail stay outside this goal's proof, as they were before this
increment.

## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above | see §5 (`daemon/`, WSL Noble) | 0 each | as tabulated |
| Full Go suite, Windows (`go1.27.4 windows/amd64`) | `go test -count=1 ./...` (`daemon/`) | 0 | 30 rows: 27 `ok`, 3 `[no test files]`, no `FAIL`; the changed Go files are `//go:build linux && amd64`, so Windows compiles none of them |
| Full Go suite, WSL Noble as root (`go1.27.0 linux/amd64`) | `go test -count=1 ./...` (`daemon/`) | 1 | 34 rows: 29 `ok` including `daemon/e2e` (1.892s, the three tests inside), 3 `[no test files]`, 2 `FAIL` — `internal/platform` and `native/linux-gate/protocol` |
| Attribution of both WSL failures (base) | the same two packages in an extracted `ac04f3e2` `daemon/` tree (its `production_linux_v2_owner_cli_test.go` digest is `751eaf4d…`, the baseline runs' blob) | 1 | identical failures: 13 rows of `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` in `internal/platform`, and `gate.c:13:10: fatal error: linux/capability.h: No such file or directory` in `native/linux-gate/protocol`. This range's diff touches neither package |
| Comment cap | the coordinator's owner comment-policy checker (`comment-cap.py`, its own scratch install) on the clone commit carrying the delivered bytes, base `ac04f3e2` | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 6 changed files, 1803 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new; notices that the 5679-line Go test file and the 5857-line `CLOSED-TODO.md` are outside the pinned `--max-lines 5000` scope |
| Changed-file duplication, raised ceilings | the same script with `--max-lines 30000 --max-size 8mb` (scratch copy inside the clone) | 0 | 6 changed files, 1810 scanned sources, 1404 clones, 0 tolerated among the added lines, none new, no notice — every changed file was scanned |
| Record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 1 changed record passes the root-only check |
| Citation existence | `python scripts/ci/check-citation-existence.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | 1 changed document passes the path and symbol check (739 path citations; the notes are pre-existing ignore-rule notes in `CLOSED-TODO.md`) |
| Shell syntax | not applicable: no `sh`/`py`/workflow/exported file changed in this round | — | — |

The comment cap and the duplication gate read the *committed* diff, so both ran on a scratch clone of
this branch carrying the delivered bytes, with the pinned jscpd install symlinked in (no install was
run). Their counts are properties of that commit; the files they gated are §1's four. A commit sha
cannot contain its own hash, so the binding revision is §1's digest plus the file list, and the
scratch clone is removed after the stage. The full-suite rows ran on the delivered Go bytes: Windows
compiles the changed files out entirely, and the WSL runs execute them inside `daemon/e2e` (whose
`ok` row appears in both). The two WSL failures are host preconditions in packages this range does
not touch, reproduced at the dispatch base in the same environment.

## 9. Next Bind

The binding receipt must be the conductor's re-Bind on the post-fix bytes: this round changes the
`daemon/e2e/production_linux_v2_owner_cli_test.go` blob (§1 `dba2daec…`), so no run that gated
`9c36f90f…` can bind it.

- Hosted run `46020141750` (head `ad8f6557ee75`) is BASELINE ONLY: the witness blob at that run was
  `751eaf4d5e4d29852b4fa23e4cec2be85a5b524ad5705f40e3cd2fc322a749a7` against `9c36f90f…` at the
  pre-fix head and `dba2daec…` here. It is history for the predecessor's failure, not acceptance
  evidence for these bytes.
- Hosted run `46024288348` (head `b46b8d89`) is BASELINE ONLY for the same reason: it carries the
  same stale witness blob.
- The scratch Bind `46037316453` (run head `8cee777f`, sole parent `f84332e4`) is PARTIAL and
  SUPERSEDED by this fix for the same reason: it gated the pre-fix Go blob. Its `Bind` section stays
  as the receipt for the pre-fix bytes and as the observation that the `:224` stop is cleared there.

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
  bytes. The clause's leg-level clearance is OBSERVED once, in the scratch Bind `46037316453` on the
  pre-fix bytes (`verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1
  pass=1 skip=0`), and is PENDING on the delivered bytes until the conductor's re-Bind reports it.
  The next stop that run hit is the `helper_bound` fixture guard
  (`linuxV2RequireSafelyReleasedPriorJournal`, `production_linux_v2_native_witness_test.go:2099`,
  reported at `:224`: "pre-existing RunJournal … is not physically released and safe for a fresh
  run"), which this round does not touch and which the re-Bind will
  re-encounter; the ACK-loss matrix and the export/reader tail are unmeasured on any revision here.
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
  red-capable on its own per §5's deletion mutant, no longer only implied by `Validate`.
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

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | §1 `dba2daec…` | caller `:1404-1476`, binding call `:1448-1451`, extraction `:1499-1510`, binding `:1512-1529`, predicate `:1531-1546`, clause `:1534`, green baseline `:1598-1600`, clause rows `:1606-1637`, removed-empty fixture `:1648-1670`, held-stage test `:1672-1704`, replay fixture `:1706-1734`, replay test `:1736-1763`, boundary arm `:1871-1874`; dispatch base `:1467`, `:1477`, `:1617-1620` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `98d4771cf6a3efafc45abab7a8c954a07e58b853858979a5435259e2e4c05d7e` | the `:224` subtest invocation that reports both stops, the retained-journal guard message `:2099`, the admitted-fresh call site `:599` |
| `daemon/internal/authority/cgroup.go` | `74743fc7601c889f0f2c5a26798fcc5585044c41200a0c0d741a89f4fa0fad78` | `never_created` rule `:381-384`, policy comment `:140-141` |
| `daemon/internal/authority/authority_v2_test.go` | `a0890aa2c5a097fb6c0712853dc94238bce385219fe4ba3df8977c64c66c5029` | positive pre-mkdir check `:231-233`, post-create-intent refusal `:235-237`, accepted never-appeared shape `:239-241`, other-intent `ValidFor` refusal `:242-244` |
| `daemon/internal/state/linux_cgroup_journal.go` | `9358c264623f719038ddcfecaab976f3f2e7fde82f28a6a9689662cd167d9b4d` | producer `:903-916`, policy comment `:898-902` |
| `daemon/internal/state/linux_clone_journal_test.go` | `dda264bcd004afbbed03801962bdbc79b6719660aa58bbc7c9def92301e84e2b` | producer pin `:256-260` |
| `daemon/internal/app/linux_owner_recovery_linux.go` | `6fb072a26495f1494c1bcc4bf199db95be41bed2a4eba0a2f4d9bcf28d92dfa5` | never-created branch `:139-146` |
| `.github/workflows/ci.yml` | `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7` | daemon race step `:1055-1063` |
| `docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md` | §1 `c73df5b3…` | §1.1 authority ranges `:231-233`/`:235-237`/`:239-241`/`:242-244`, producer pin `:256-260`, dispatch-base caller `:1467` and switch `:1475`; §1.2 dispatch-base boundary `:1617-1620` and delivered `:1871-1874`, recovery `:139-146`, policy `:140-141`; §2 the binding; §3 the delivered recovery mapping; §4 the rows |
| `docs/TODO.md` | `4625d3d98195405dff98bfd4481bc371c65025b6c47fe7606a487085acbd96bf` | the open Bind/residual entries (not digest-pinned before) |
| `docs/archive/CLOSED-TODO.md` | `7a595f48aac86741814eb33eccee0b4e9e8b8284ba37dcf55e63c884c881589a` | the closed stage items (not digest-pinned before) |

## Bind

**PARTIAL and SUPERSEDED by the fix round in §1:** this receipt gated the pre-fix witness blob
`9c36f90f…`; the delivered blob is `dba2daec…`. It remains the receipt for the pre-fix bytes and the
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
