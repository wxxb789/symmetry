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
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `9c36f90f198e27a5a380ad336cda80ab4f745379831772256abba8a951be32cc` | The `never_created` disposition switch is a named predicate; the pre-create clause demands `CreateStarted=false, LeafNeverAppeared=false`; two committed rows drive the production tombstone and the clause's observed-absence refusal. |
| `docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md` | `1f441b02a3030c7e9cc3d7de5f32fa543a68128872c5ec62a781b8909c62d5f4` | Stage-1 read and the accepted decisions. |
| `docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md` | this record | Stage-1 answers, the fix, the receipts. |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | The closed stage-1/2 items and the open Bind/residual entries. |

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
predicate at delivered `:1507`), the authority rule (then `authority.go:1251`, now
`internal/authority/cgroup.go:383`), the producer (then `state.go:2724`, now
`internal/state/linux_cgroup_journal.go:916`) and both pins are from the same commit.
`git log -S` over each text finds only the move-only refactors `9ac85ac0` (#158, authority split)
and `ff18add1` (#255, state split) after it; no commit changed any of the three tokens.

So the wall cannot be new in its cause: `linuxV2ValidateStageReplayJournal` checks
`abort.Validate()` first (`:1467`), which forces `LeafNeverAppeared == CreateStarted`, and the
clause then demands `CreateStarted == false && LeafNeverAppeared == true`. One conjunct always
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

The wrong layer is `daemon/e2e/production_linux_v2_owner_cli_test.go:1477` (the pre-fix clause),
which demands a proof shape the authority forbids. The two legal `never_created` shapes are pinned
by `internal/authority/authority_v2_test.go:236-243`:

- `CreateStarted=false, LeafNeverAppeared=false` — the pre-mkdir fence tombstone, refused there if
  `CreateStarted` is set without the observed-never-appeared flag ("post-create-intent proof
  inferred never-created from path absence alone");
- `CreateStarted=true, LeafNeverAppeared=true` — the accepted "exact never-appeared proof".

The demanded mix (`false, true`) fails `Validate` under both. The producer writes the first shape
(`internal/state/linux_cgroup_journal.go:903-916`, `CreateStarted: false, LeafNeverAppeared: false,
LeafAbsent: true`), pinned by
`internal/state/linux_clone_journal_test.go:256-260`, and the pre-mkdir policy is documented at
`internal/authority/cgroup.go:140-141` and `:898-902` of the state file: exclusive durable creator
fence, no filesystem observation, no stop receipt, no leaf-removal authority.

The recovery is not defective: the `reservation_durable` boundary requires
`reservation.CreateStarted == false` (`:1617-1620` at the dispatch base; delivered `:1712-1713`), so
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
  and still refuses a tombstone reporting the create CAS, an observed absence, a helper or a leaf.
  The observed-absence refusal is now pinned by its own committed row (delivered `:1585-1590`),
  red-capable per §5's deletion mutant.
- Row mapping for the clause's contract: the production pre-mkdir tombstone must be accepted → the
  green row of `TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape`; the create-CAS shape and a
  claimed observed absence must stay refused → its `createStarted` and `claimedAbsent` rows; the
  helper/leaf shapes and an unknown disposition → its `removedEmpty` and `unknown` rows; the old
  clause's contradiction of the producer → the §5 polarity mutant. No contract invariant of this
  change lacks an executable row.
- Unchanged: `authority.Validate`, the state producer, the tombstone semantics and json fields, the
  boundary validators, the replay stage list, the acceptance script and the exporter. The `shared`
  arm's credential-gated pi leg (1B1) is untouched, so the gated outcome is recorded exactly as
  before.
- Adversarial satisfaction of the new green row: it can be satisfied only by a tombstone whose
  disposition, flags and evidence all equal the persisted pre-mkdir proof; no other real event
  reaches it, because the clause reads the journal the daemon wrote under the `reservation_durable`
  boundary. The committed test drives the *production* producer, so a future producer change that
  moves the shape fails this row instead of the CI-only witness.

## 5. Red/green controls (command → exit code)

All rows ran in WSL `Ubuntu-24.04` (Noble, GNU coreutils; `go1.27.0 linux/amd64`) from the `daemon/`
directory of `path-sha256:2f67be0f6c89098f40f06106d9a81063985eef29086d1da95456bd4a318385f5`. WSL
ran the *working-tree* bytes; the mutant rows ran a clone of the primary checkout carrying §1's
bytes, each mutant identified by its own sha256 in the appendix below.

| Row | Command | Exit | Observed |
| --- | --- | --- | --- |
| Green, delivered bytes | `go test ./e2e -run '^TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape$' -count=1 -v` | 0 | `--- PASS: TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape (0.02s)` |
| Red, old clause on the same bytes | appendix mutant `64c007dc…` (delivered bytes with the clause's second conjunct's polarity restored): `go test ./e2e -run '^TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape$' -count=1` | 1 | `production_linux_v2_owner_cli_test.go:1572: the persisted pre-mkdir tombstone was refused: pre-create abort tombstone reports a target or leaf creation` |
| Red, clause conjunct deleted — the new row's mutation control | appendix mutant `3bcb1eca…` (delivered bytes with `abort.LeafNeverAppeared || ` deleted at `:1507`): the same command | 1 | `production_linux_v2_owner_cli_test.go:1589: a tombstone claiming an observed absence was not refused with the target/leaf-creation message: <nil>` |
| Neighbour predicates | `go test ./e2e -run 'TestLinuxV2' -count=1` | 0 | `ok … daemon/e2e 0.603s` |
| Race pass shape (CI home) | `go test -race ./e2e -run '^TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape$' -count=1` | 0 | `ok … 1.093s` |
| Producer-shape pin | `go test ./internal/state -run '^TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence$' -count=1` | 0 | `ok … 0.089s` |
| Authority-shape pin | `go test ./internal/authority -run '^TestLinuxCgroupReservationAbortProofRequiresExactPreEffectEvidence$' -count=1` | 0 | `ok … 0.011s` |
| Vet | `go vet ./e2e ./internal/state ./internal/authority` | 0 | no output |
| Format | `gofmt -l e2e/production_linux_v2_owner_cli_test.go` | 0 | no output |

The polarity row is the executed un-satisfiability proof for §2 and §3: the *old* clause refuses
the proof the *production* producer persists for a never-created reservation, which is exactly the
`:224` stop; the green row is the same bytes with the corrected expectation. The deletion row is
the mutation control for the row this stage adds: with the clause's second conjunct gone the
earlier rows stay green and the run fails at the new `:1589`, so that row — not `Validate`, not a
neighbour — pins the conjunct. Both mutants were written by copying §1's bytes over the clone file and
changing one line; the same copy restored the delivered bytes, re-verified at `9c36f90f…` before
each following step.

### Mutant appendix (red-capability controls)

Both mutants were run with `go test ./e2e -run '^TestLinuxV2PreEffectAbortTombstoneRequiresItsExactShape$' -count=1`
from the clone's `daemon/` directory, on a clone copy of §1's bytes:

- `64c007dc16818d814fa6d092baa53e10275f02715e64a1daf254270545680aca` — clause line `:1507` restored to
  `if abort.CreateStarted || !abort.LeafNeverAppeared || abort.Helper != nil || abort.HelperExit != nil {`
- `3bcb1ecda3c62bb43fcaff7da298a83ea7bd75f61bf2c38a6394cdfbce339698` — clause line `:1507` with
  `abort.LeafNeverAppeared || ` deleted:
  `if abort.CreateStarted || abort.Helper != nil || abort.HelperExit != nil {`

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
consumes only `authority.LinuxCgroupReservationAbortProof` bytes from the persisted journal, which
are identical on every userland.

## 7. Does the conductor's Bind execute it?

No. The new test lives in `daemon/e2e` and runs in the `daemon` job's race pass
(`go test -race -count=1 -timeout 600s -json ./...`, `.github/workflows/ci.yml:1055-1063`) and
locally; the `shared` Bind does not invoke `./e2e` with this test name. What the Bind *does*
execute is the fixed clause itself, through the `reservation_durable` leg of
`TestProductionLinuxV2StageCrashReplayMatrix`. The fix also reloads no fixture, so the Bind needs no
new step. The arm's later legs and the export/reader tail stay outside this goal's proof, as they
were before this increment.

## 8. Gates

| Gate | Command (working directory) | Exit | Counts |
| --- | --- | --- | --- |
| Go rows above | see §5 (`daemon/`, WSL Noble) | 0 each | as tabulated |
| Full Go suite, Windows | `go test -count=1 ./...` (`daemon/`) | 0 | 27 `ok` packages, 3 `[no test files]`, no failure; the changed file is `//go:build linux && amd64`, so Windows compiles none of it |
| Full Go suite, WSL Noble as root | `go test -count=1 ./...` (`daemon/`) | 1 | 30 `ok` packages including `daemon/e2e` (2.601s, the new row inside); `internal/platform` fails 13 tests with `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` — reproduced at the dispatch base, and those tests pass when run unprivileged |
| Full Go suite, WSL Noble as `accept` (unprivileged, a checkout it owns, private `TMPDIR`) | `go test -count=1 ./...` | 1 | 28 `ok` packages including `daemon/e2e` (2.592s); failures confined to `internal/linuxpreflight`, `internal/platform`, `internal/workspace` (AppArmor-parser behaviour, supervisor witness-directory writability, clone-seed source-ownership rules); all three are byte-identical to the dispatch base (the range touches five files, none of them) and are `ok` on Windows |
| Comment cap | the gdd scratch checker on the clone commit carrying the final bytes (`daemon/` in the clone; base = `git merge-base HEAD origin/main` = `ac04f3e2`) | 0 | no added comment block over 2 lines |
| Changed-file duplication, pinned ceilings | `python scripts/ci/jscpd-changed.py --base ac04f3e2 --head HEAD` (scratch clone) | 0 | 5 changed files, 1803 scanned sources, 1245 clones in the corpus, 0 tolerated among the added lines, none new |
| Changed-file duplication, raised ceilings | the same script with `--max-lines 30000 --max-size 8mb` (scratch copy in the clone) | 0 | 5 changed files, 1810 scanned sources, 1404 clones, 0 tolerated among the added lines, none new |
| Shell syntax | not applicable: no `sh`/`py`/workflow/exported file changed in this increment | — | — |

The duplication gate's pinned ceilings (10 lines, 50 tokens, 5000 lines, 1 mb) are why the raised
row exists: the pinned run reported notices for `daemon/e2e/production_linux_v2_owner_cli_test.go`
and `docs/archive/CLOSED-TODO.md` — both carry added lines but sit outside the pinned scan scope
(5679 and 5857 lines against `--max-lines 5000`), so clones inside them were not checked — while
the raised run scanned all five changed files and refused nothing.

All three checkers read the committed diff, so the receipts ran on a scratch clone of the dispatch
base carrying these five files, re-verified by sha256 immediately before each run (the Go file's
digest is §1's `9c36f90f…`, and the clone's `git diff --stat` list equals §1's file list). Their
counts are corpus-wide properties of the commit each ran on; no byte they gate — Go, shell, Python,
workflow or export — differs between those commits and the delivered bytes, only record and plan
prose added while writing this section. The pinned and raised rows were run on each scratch commit
that carried the refreshed record, with identical counts each time. The binding revision is
therefore §1's digest plus the file list — a commit sha cannot
contain its own hash, so none is restated here, and the scratch clone is removed after the stage.
The full-suite rows ran on the delivered Go bytes: Windows compiles the changed file out entirely,
and the WSL runs execute it inside `daemon/e2e` (whose `ok` row appears in all of them); the
platform, workspace and preflight failures are host/userland preconditions in packages this range
does not touch, named per row above.

## 9. Next Bind

Report the `shared` case-set exit status, the 22-row `verified …` sequence and its first
disagreement, the `reservation_durable` leg's outcome (expected: no `:224` stop), the credential-gated
pi outcome verbatim in log, declaration and exported member, the export status and the reader
Result — plus the first unexpected failure if the arm stops later in the replay matrix. The expected
green outcome of the fixed row is in §5; the Bind is the acceptance, not this record.

## 10. Residuals and known limits

- The leg is CI-only: nothing here proves the `reservation_durable` leg passes end to end, only that
  its last observed stop is gone and the clause is satisfiable. The next stop after it (a later
  stage-replay hold, the ACK-loss matrix, the export/reader tail) is unmeasured on these bytes.
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

Every line anchor this record cites, re-read on the delivered bytes and cited with its file's
sha256; the dispatch-base anchors of §1–§3 were re-read at `ac04f3e2` and are marked there. Three
anchors in the earlier text were stale and are corrected above: the pre-mkdir policy comment is
`cgroup.go:140-141` (not `:138-139`), the producer pin spans `linux_clone_journal_test.go:256-260`
(not `:255-258`), and the never-created recovery branch starts at
`linux_owner_recovery_linux.go:139` (not `:140`).

| File | sha256 (delivered) | Anchors checked |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `9c36f90f198e27a5a380ad336cda80ab4f745379831772256abba8a951be32cc` | predicate `:1504`, clause `:1507`, caller `:1476-1477`, green row `:1571-1572`, `claimedAbsent` row `:1585-1590`, boundary arm `:1712-1713`; dispatch base `:1467`, `:1477`, `:1617-1620` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `d988ca3bf8a0364f7d2f1c9aabe4cef14b53216e501a6002c49950e18d508abf` | the `:224` subtest invocation that reports the stop |
| `daemon/internal/authority/cgroup.go` | `74743fc7601c889f0f2c5a26798fcc5585044c41200a0c0d741a89f4fa0fad78` | `never_created` rule `:381-384`, policy comment `:140-141` |
| `daemon/internal/authority/authority_v2_test.go` | `a0890aa2c5a097fb6c0712853dc94238bce385219fe4ba3df8977c64c66c5029` | both shapes pinned `:236-243` |
| `daemon/internal/state/linux_cgroup_journal.go` | `9358c264623f719038ddcfecaab976f3f2e7fde82f28a6a9689662cd167d9b4d` | producer `:903-916`, policy comment `:898-902` |
| `daemon/internal/state/linux_clone_journal_test.go` | `dda264bcd004afbbed03801962bdbc79b6719660aa58bbc7c9def92301e84e2b` | producer pin `:256-260` |
| `daemon/internal/app/linux_owner_recovery_linux.go` | `6fb072a26495f1494c1bcc4bf199db95be41bed2a4eba0a2f4d9bcf28d92dfa5` | never-created branch `:139-146` |
| `.github/workflows/ci.yml` | `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7` | daemon race step `:1055-1063` |
