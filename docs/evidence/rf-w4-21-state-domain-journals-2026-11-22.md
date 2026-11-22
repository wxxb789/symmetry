# rf-w4-21 `state.go` domain sub-ledgers — evidence record (2026-11-22)

Slice: W4-21 of `goal-rf-w4-21-state-domain-journals.md` (conductor decision
arch-decision-2, R0/R2/R8/R9; its open-item rulings O27/O28; the arch-decision-2
verification amendments; the owner comment policy 2026-11-22). Plan:
`docs/plans/2026-11-22-rf-w4-21-state-domain-journals-plan.md`.

Base revision of the working tree: `2db5ed0c` (`refactor(app): put the journal store behind a
narrow interface (#244)`), which the slice branch `refactor/rf-w4-21-state-domain-journals` holds.
Dependencies re-checked as ancestors of base (`git merge-base --is-ancestor`): W2-09
`#165`, W3-07 `#200`, W3-08 `#214`, W3-15 `#217`, W4-12a `#244`; arch-decision-2 is BOUND
(`docs/goals/0010-refactor-program.md`).

Change shape (arch-decision-2): four embedded value sub-ledgers on a kept `RunJournal` and
`Store` core, one shared fenced-CAS helper, and same-package domain files split out of the
6,532-line `state.go`. Not a move-only slice: predicate receivers and the folded CAS sites
change; every changed declaration is audited in §3 and §4.

## 1. Deliverable shape

`state.go` keeps its path and holds only the kernel (`SOURCE_PATHS` line unchanged). Sizes are
`wc -l` at the delivered tree.

| File | Kind | Size |
| --- | --- | --- |
| `daemon/internal/state/state.go` | kernel: `Store`, `New`/`Close`, atomic seam, load/save/mutate/CAS, JSON IO, path helpers, shared scalar validators | 621 |
| `daemon/internal/state/identity.go` | machine identity, enrollment, claim intent/grant | 257 |
| `daemon/internal/state/run_journal.go` | `RunJournal` value, predicates, save/delete, markers, lease, provide-input intent | 696 |
| `daemon/internal/state/journal_validation.go` | `validateJournal` and its section validators | 226 |
| `daemon/internal/state/run_outbox.go` | events, transitions, acknowledgements, terminal queueing | 910 |
| `daemon/internal/state/run_goal_delivery_journal.go` | `RunGoalDeliveryState` block and its predicate | 37 |
| `daemon/internal/state/linux_cgroup_journal.go` | cgroup-v2 reservation, admission scan, `Validate` section helpers | 1128 |
| `daemon/internal/state/linux_clone_journal.go` | `RunCloneState` block, clone journal mutations | 957 |
| `daemon/internal/state/linux_clone_validation.go` | clone journal validators | 425 |
| `daemon/internal/state/containment_journal.go` | `RunContainmentState` block, containment validators | 703 |
| `daemon/internal/state/supervisor_handoff_journal.go` | v1/v2 handoff prepare/bind/commit/stop-receipt/clear | 718 |
| `daemon/internal/state/store_test.go` | kernel/IO tests | 370 |
| `daemon/internal/state/identity_test.go` | identity tests | 266 |
| `daemon/internal/state/run_journal_test.go` | journal lifecycle, terminal audit, cleanup, lease | 638 |
| `daemon/internal/state/run_outbox_test.go` | outbox/transition/acknowledgement tests | 1012 |
| `daemon/internal/state/containment_test.go` | process-marker tests | 108 |
| `daemon/internal/state/journal_validation_test.go` | validator pin suite and its fixtures | 890 |
| `daemon/internal/state/state_helpers_test.go` | helpers used by ≥2 test files (`mustStore`, `testJournal`, `stoppedTestJournal`, three `pinnedJournal*` fixtures) | 106 |
| `daemon/internal/state/state_fence_test.go` | fence-pin tests for the folded sites (`//go:build linux`) | 95 |
| `daemon/internal/state/golden_test.go` | persisted-journal golden suite | 469 |
| `daemon/internal/state/testdata/goldens/*.json` | 9 base-generated goldens | 9 files |
| `scripts/ci/omo-control-native-evidence.py` | `SOURCE_PATHS` additions (R9) | 10 lines |
| `docs/TODO.md` | refreshed moved citations (§10) | 25 lines |
| `docs/plans/2026-11-22-rf-w4-21-state-domain-journals-plan.md` | this slice's plan | 190 |

`state_test.go` (3,324 L at base) is deleted; its units moved verbatim (declaration-key, doc
hash and body hash equal for all 104 units, §8). `golden_gen_test.go` was the temporary
generator of §5 and is deleted with it.

Files this slice did **not** touch: `goal_session.go` (2,149 L), `goal_delivery.go` (1,185),
`provider_action.go` (651), `control.go` (230), `goal_usage.go` (70),
`cgroup_release_linux.go` (202), the readers, `state_unix.go`, `state_windows.go`, and every
test file other than `state_test.go`. Every file of the package this slice writes is ≤ 1,500 L;
the four files above 1,500 L in the package are pre-existing and unowned
(`goal_session.go` 2,149, `goal_session_test.go` 2,427, `linux_clone_journal_test.go` 2,057,
`provider_action_test.go` 1,541 — identical at base, `scratch/base-wc.txt`).

## 2. Sub-ledger membership

`scratch/membership-final.txt` (tool `scratch/membership.py`): 63 base `RunJournal` fields, 28
stay direct, 35 move into four embedded blocks. The head field sequence — RunJournal's own
fields with each embedded block expanded in place — equals the base sequence name-for-name and
tag-for-tag, in the same order:

```
base fields: 63   head RunJournal own: 28   head expanded: 63
OK: head expanded field sequence is identical (name + json tag, same order)
```

| Block | Fields | Base span (`state.go`) | Head file |
| --- | --- | --- | --- |
| `RunCloneState` | workspace binding + 14 Linux clone authority fields (18) | 680-722 | `linux_clone_journal.go` |
| `RunContainmentState` | `PID`, `ProcessIdentity`, `StartedAt`, supervisor authority/handoff, cgroup reservation + release tombstones, scan ledger, `ContainmentUnproven` (10) | 723-742 | `containment_journal.go` |
| `RunOutboxState` | event sequence + pending events, drop counters, transitions, attempted IDs, acknowledgements (7) | 743-749 | `run_outbox.go` |
| `RunGoalDeliveryState` | pending/delivered/retired deliveries + enable fence (4) | 750-753 | `run_goal_delivery_journal.go` |

Field order is load-bearing: `encoding/json` emits struct fields in index order, so the
embedding keeps every persisted key in its old position and the committed goldens byte-identical
(§5). The workspace binding joins `RunCloneState` because every clone-target validator reads
exactly that block (plan §1).

## 3. Predicate relocation and the receiver-ownership invariant

14 free functions became methods on their sub-ledger; the plan's rule is that a predicate moves
only when every field it reads belongs to that block, with remaining context as a parameter.

`scratch/predicate-audit-final.txt` diffs each base function against its head method. Every
diff is the receiver change plus one parameter substitution:
`linuxCloneStageCanAdvance(journal)` → `(journal RunCloneState) linuxCloneStageCanAdvance(localState string)`,
`validateContainmentCgroup/Handoff(journal)` → `(journal RunContainmentState) (…, granted bool)`,
`hasExactUsageDelivery(journal, usage)` → `(journal RunGoalDeliveryState) hasExactUsageDelivery(runID string, fence protocol.Fence, usage protocol.Usage)`;
the other ten diffs change only the receiver. No condition, branch or error string moves.

`scratch/recvfields-final.txt` (mode `recvfields` of the scratch AST tool) proves the
invariant mechanically: for every method whose receiver is one of the four blocks, it lists
every selector taken off that receiver and flags any name that is not a field of the block or a
method on it. Output: 17 methods, `0 OUT-OF-OWNER` selectors. The 24 declarations whose body
hash changed are exactly the seven folded CAS sites of §4, nine `Store` methods whose only edit
is a predicate call site, the five free functions whose callees became methods
(`linuxV1StopReceiptSafelyRetires`, `validateJournal`, `validateLinuxCloneJournal`,
`validateLinuxCloneCleanup`, `validatePendingJournalEntriesAndDeliveries`),
`validAcknowledgement` (the whole-journal argument became `runID` + `fence`), `RunJournal`
(embedded block) and `RunJournal.HasProcessDetails` (delegates to
`RunContainmentState.HasProcessDetails`, whose body hash equals the base method's). Nothing else
changed: the 14 relocated predicates appear as removed+added pairs (receiver renamed) and 342
declarations moved file with identical doc and body hashes (`scratch/declaration-map-summary.md`;
`scratch/inspect-final.txt`: 24 changed, 14 removed, 40 added, 0 unexplained).

## 4. Folded fenced-CAS sites

`mutateJournalCAS(key, fence, stale, mutate)` runs the site's key/fence comparison under the
Store lock after `loadJournalLocked` (which already rejects a journal whose key differs from
the requested key: "run journal does not match requested key") and before `mutate`; the
capacity-guarded write is unchanged. Each folded site passes its own base error text as
`stale`, so no error string, check order or write order changes.

| # | Site | Base check | Head fold | Base mutant (red) | HEAD mutant (red) |
| --- | --- | --- | --- | --- | --- |
| 1 | `AbortLinuxCloneReuseBeforeNativeStart` | `state.go:1710` | helper + grant check in closure | new `TestLinuxCloneReuseAbortRejectsStaleFence` | same test |
| 2 | `BeginLinuxCloneCleanup` | `state.go:1948` (+ `:956`) | helper only | new `TestLinuxCloneCleanupBeginRejectsStaleFence` | same test |
| 3 | `RecordLinuxCloneTargetRemoval` | `state.go:2017` (+ `:956`) | helper only | `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks` | same test |
| 4 | `CompleteLinuxCloneCleanup` | `state.go:2048` | helper only | new `TestLinuxCloneCleanupCompleteReplayRejectsStaleFence` | same test |
| 5 | `ReserveLinuxCgroup` | `state.go:2703` (+ `:415`, `:464`) | **retained inline**: its `:2700` claim-grant check runs before the fence check, so the helper (whose comparand always runs first) would turn `journal has no claim grant` into the stale-fence error for a grantless journal — banned by the frozen error-identity/order set. Line `linux_cgroup_journal.go:562` keeps the comparison; the admission-scan duplicates `:317`/`:366` are untouched by the fold | `TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners` | same test, own comparand mutated |
| 6 | `MarkLinuxCgroupCreateStarted` | `state.go:2749` (+ `:415`, `:464`) | helper + `validLinuxV2AdmissionFence` in closure (no grant check to reorder) | same test | same test |
| 7 | `ArmLinuxCgroupLauncherGrant` | `state.go:2883` (+ `:415`, `:464`) | helper + `validLinuxV2AdmissionFence` in closure (no grant check to reorder) | new `TestLinuxCgroupLauncherGrantRejectsStaleFence` | same test |
| 8 | `AbortNeverCreatedLinuxCgroupReservation` | `state.go:3049` | helper + `hasClaimGrant` in closure | `TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence` | same test |

Sites 2/3/6/7 carried a second copy of the same comparand on the same path
(`LinuxCloneCleanupBeginEligible`'s fence comparison; the admission scan's
`target.Fence() != fence` and `targetFound` proof); the base mutant removes both copies, and
the fold leaves exactly one copy, in the helper. Seven of the eight tabled sites fold; site 5 is
retained inline with the reason above, so the site table is 7 folded + 1 retained and the
retained site keeps its own mutant evidence. Values live in `scratch/mutants-base.txt`,
`scratch/mutants-base-cgroup.txt` and `scratch/mutants-head.txt`.

Mutant verdicts, all eight sites red at base and at HEAD (`scratch/mutants-base.txt`,
`scratch/mutants-base-cgroup.txt`, `scratch/mutants-head.txt`; every touched file restored and
sha256-verified: `state.go 87f914a9…`, `linux_cgroup_journal.go 1796b34f…`,
`run_journal.go ad4443f5…`):

| Site | Base | HEAD |
| --- | --- | --- |
| 1 | red (new test) | red |
| 2 | red | red (helper comparand + `LinuxCloneCleanupBeginEligible`'s comparand `run_journal.go:148`) |
| 3 | red | red (same pair) |
| 4 | red (new test) | red |
| 5 | red | red (site's own comparand `linux_cgroup_journal.go:562` + scan duplicates `:317`/`:366`) |
| 6 | red | red (helper + scan duplicates) |
| 7 | red (new test) | red |
| 8 | red | red |

Sites 2 and 3 also consult `LinuxCloneCleanupBeginEligible`, which keeps the same premise
(`linux_clone_journal.go:756,762`); the HEAD mutant must remove that copy too, exactly as the base
mutant removed `state.go:956`. With only the helper's comparand removed those two tests survive —
recorded because it defines the per-site mutant's required shape, not because a mutant survived.

The four new fence tests live in `state_fence_test.go` (`//go:build linux`, so the Windows list
is unchanged by them); the three pre-existing tests are named above. Sites whose fence proof is
not a bare key/fence comparison (`requireLinuxCloneJournalFence`: intent, binding key, admitted
subject) keep their own proof and are not folded.

## 5. Golden JSON byte-identity

Nine goldens under `daemon/internal/state/testdata/goldens/` cover every persisted journal
family the package writes: five `RunJournal` shapes (minimal, clone, containment, outbox,
goal-delivery) through `decodeJSONWithLimitAndTags` + `serializeRunJournal`, and
`goal-session-journal`, `late-goal-usage`, `machine-identity`, `enrollment-intent` through the
`writeJSON` path.

Base generation: the committed goldens were produced by base code — the head golden test files
were compiled into a pristine base checkout (`git archive 2db5ed0c daemon`, scratch
`base-tree/`) and `TestWriteGoldens` regenerated all nine files there; `diff -r` against the
committed goldens is empty and the sha256 lists match (`scratch/goldens-base.sha256`,
`scratch/goldens-head.sha256`). The head suite then asserts, for every golden, both directions:
committed bytes decode and re-encode byte-identically, and the code-built value encodes to the
same bytes (`TestRunJournalGoldensDecodeAndReencodeIdentically`,
`TestPersistedJournalFamilyGoldensRoundTrip`), plus a per-field key test that mutates each moved
field and requires the old key to appear, move, or disappear exactly as before
(`TestRunJournalGoldenFieldKeys`, 40 cases across the four blocks).

Red mutant: renaming one moved field's json tag (`workspace_path` → `workspace_path_x` in
`RunCloneState`) turns `TestRunJournalGoldensDecodeAndReencodeIdentically` and
`TestRunJournalGoldenFieldKeys` red — the production decoder rejects the golden's old key
(`decode run journal: json: unknown field "workspace_path"`), so the committed bytes no longer
decode at all — and the key-lookup test fails with it; `TestPersistedJournalFamilyGoldensRoundTrip`
is unaffected because it covers the non-`RunJournal` families. Output in
`scratch/golden-mutant.txt`, restore verified by sha256 (`restored: True`).

## 6. Composite-literal proof (O28)

`scratch/lits-base-now.tsv` / `scratch/lits-head-now.tsv` (mode `lits` of the scratch tool)
dump every `RunJournal` composite literal with its field→value entries, flattening embedded
sub-ledger literals. Base and HEAD both hold 498 literal rows, 80 of them with assignments; the
multiset of assignment rows is identical (`0` only-base, `0` only-head). 15 sites set a moved
field — 12 outside `daemon/internal/state` (`cmd/symmetry-daemon/preauthority_witness_command_windows.go`,
`e2e/core_test.go`, `e2e/production_linux_containment_witness_test.go`, `internal/app/app_test.go`,
`internal/app/journal_store_linux_test.go`, `internal/app/process_persistence_test.go`,
`internal/app/recovery_handoff_test.go`, `internal/app/recovery_linux_test.go`,
`internal/execution/linux_owner_v2_test.go`, `internal/workspace/linux_clone_linux_test.go`) and 3
inside it — and **no literal was edited**: Go 1.27.4 resolves a promoted field key in a literal
to the embedded field (probe `scratch/embtest`), so the O28 mechanical-edit allowance is unused and
no non-owned file is touched.

## 7. Exported surface, formatting, builds

`go doc -all ./internal/state` base vs head (`scratch/godoc-final.diff`, 129 lines) contains
only the intended additions: `type RunCloneState`, `type RunContainmentState`,
`func (journal RunContainmentState) HasProcessDetails() bool`, `type RunGoalDeliveryState`,
`type RunOutboxState`, and `RunJournal`'s struct block with the four embedded fields in place of
the moved fields (every remaining field line and comment identical). No exported declaration is
removed or renamed.

```
gofmt -l internal/state                 # empty
go vet ./...                            # exit 0
GOOS=linux go build ./...               # exit 0
GOOS=windows go build ./...             # exit 0
GOOS=darwin go build ./...              # exit 0
GOOS=linux GOARCH=arm64 go build ./...  # exit 0
```

## 8. Tests

Test units moved verbatim: `scratch/inspect-final.txt` compares every base declaration key
against HEAD by doc hash and printed-body hash. All 104 `state_test.go` units (70 tests, 31
helpers, one type, one const block) and its import decl match a head declaration; the 24
changed declarations are production-only (§3) and the 8 new test declarations are
`state_fence_test.go`'s four fence tests plus the golden suite's helpers and tests.

Test-name sets (`go test -list '.*' ./internal/state/`, sorted):

* Windows (`scratch/bn.txt` vs `scratch/hn.txt`): base 229 names ⊆ head 232 names; the three
  additions are the golden tests; zero removals.
* WSL (`scratch/bw.txt` vs `scratch/hw.txt`): base 249 names ⊆ head 256 names; the seven
  additions are the three golden tests plus the four linux-only fence tests; zero removals.
* SKIP list: 0 skips in the Windows state+app run (`scratch/head-win-state-app.txt`), 0 in the
  WSL run (`scratch/head-wsl-race.txt`), matching base.

Suites (all `-count=1`):

```
go test ./internal/state/... ./internal/app/...        # Windows: ok state 41.3s, ok app 420.0s, exit 0
wsl -d Ubuntu-24.04 -u accept bash -lc "… go test -race -timeout 30m \
  -skip TestOutputBackpressureRecoversWithOneTruncationMarker \
  ./internal/state/... ./internal/app/..."             # WSL race: ok state 78.0s, ok app 766.5s, exit 0
```

One package-timeout limit is recorded rather than hidden: the WSL `-race` run of `internal/app`
stops at Go's default 10-minute package budget inside
`TestOutputBackpressureRecoversWithOneTruncationMarker` (`panic: test timed out after 10m0s`,
that test running 4m6s). The test is this repository's documented slowest
(`docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md:309`: 170.05s against a 30s cap),
and it reproduces identically on a pristine base checkout under the same flags:
`base-tree/daemon` `go test -race -timeout 8m -run TestOutputBackpressureRecoversWithOneTruncationMarker ./internal/app/`
fails at `480.152s`, HEAD at `480.146s` (`scratch/base-single-race.txt`, `scratch/head-wsl-race.txt`).
The gate therefore reruns the same two packages with the same flags, an explicit 30-minute budget
and that one documented slow test skipped. CI runs the same race step with
`go test -race -count=1 -timeout 600s ./...` (`.github/workflows/ci.yml:1121`) on native Linux
runners; `internal/state` alone is green under `-race` in WSL at 86.97s.

The app package is the lesson-32 gate carrier: the moved code paths are exercised end to end
through `internal/app` (journal store interface, recovery, cleanup glue), not only in the state
package.

## 9. R9 source-subject mapping

`scripts/ci/omo-control-native-evidence.py` `SOURCE_PATHS` gains the ten new files that receive
`state.go` code, and `state.go` keeps its path:

| New path | Base `state.go` spans |
| --- | --- |
| `containment_journal.go` | 66-113 partial, 723-742, 3790-4000, 5960-6010, 6100-6200 |
| `identity.go` | 1146-1336, 1420-1560 (identity/enrollment/claim) |
| `journal_validation.go` | 5817-6018, 6214-6266 |
| `linux_cgroup_journal.go` | 115-273, 339-575, 2580-3024 |
| `linux_clone_journal.go` | 759-968, 1354-2013 |
| `linux_clone_validation.go` | 4816-5119 |
| `run_goal_delivery_journal.go` | 750-753, 2234-2260 |
| `run_journal.go` | 577-756, 808-968, 972-1066, 2015-2258, 2343-2578 |
| `run_outbox.go` | 3026-3300, 3302-4494, 4543-4790, 6187-6266 |
| `supervisor_handoff_journal.go` | 3302-4494 (handoff half) |

Gate: `python scripts/ci/test-omo-control-native-evidence.py` — 15 tests, exit 0 (it pins the
declared-set verification the parent attestation uses).

## 10. Citation refresh

Living document refreshed: `docs/TODO.md`, 25 citations of code this slice moved, each to its
new file:line — `run_journal.go:674`, `containment_journal.go:614`, `:617`, `:626`, `:597`,
`:574`, `:553`, `:600`, `:604`, `:601`, `:605`, `:623`, `linux_cgroup_journal.go:165`, `:169`,
`:178`, `:1082`, `journal_validation.go:128`, `:73`, `:53`, `run_outbox.go:840`,
`linux_clone_validation.go:250`, `:55`, and `state.go:236`, `:240`, `:254` for the kernel
functions whose line numbers moved inside the kept path.

`docs/archive/CLOSED-TODO.md` cites only `provider_action.go` and `goal_session.go`; they and
`docs/design/**`, `docs/runbooks/**` contain no `state.go` citation. Historical, revision-bound
records (`docs/plans/**`, `docs/evidence/**`, `docs/goals/**`) keep their own-revision citations
— rewriting them would falsify the revision each binds — and the arch-decision-2 owned set plus
O28 exclude those files. `scratch/plans-citations-map.tsv` maps all 328 of their
`state.go:<line>` citations to the declaration each line held at this slice's base and to that
declaration's head file:line (86 distinct declarations; 48 citations already fell outside every
declaration at base, i.e. they were stale before this slice). The gate itself
(`scripts/ci/check-citation-existence.py --base origin/main --head HEAD`) exits 0; it audits only
documents the range changes, and it strips line suffixes, so `state.go:<line>` citations keep
resolving to the file that still exists.

## 11. Interpretations, residual risks

* Host paths and DLP: `git diff origin/main...HEAD` and the working-tree diff carry no path
  containing a user profile name (`grep -ic 'users/'` = 0), and no file this slice writes
  introduces a backslash-escape sequence. The two such sequences the split test files carry are
  JSON duplicate-member fixtures that escape a leading ASCII letter of a key (`store_test.go`,
  `run_outbox_test.go`); both are verbatim relocations of lines that already exist at HEAD in
  `state_test.go`, so the change set adds no new occurrence.
* Two observations outside this slice's owned set, recorded for a follow-up rather than edited:
  `scripts/ci/export-linux-v2-redacted-evidence.py:536` still names `state.go` as the owner of
  `isTerminalTransitionState`, which this slice moved to `journal_validation.go`; and
  `scripts/ci/test-linux-v2-evidence.py` fails identically on a pristine base checkout and at HEAD
  (55 failures, 5 errors in both — `scratch/base-tree/scripts/ci`, environment-bound, not a gate of
  this contract).
* "No file > 1,500 L" is read as every file this slice writes (the Design-1 family files, the
  kernel, and the split test files). Four pre-existing files above that size (§1) are outside
  the slice's owned set and are unchanged; `goal_session.go` is explicitly excluded by the
  contract's root-cause section.
* The docs/plans citation-refresh gate sentence conflicts with the arch-decision-2 owned set
  (which lists only this slice's plan) and with O28's "any other edit outside
  `daemon/internal/state` = not achieved"; the conflict is resolved in favour of not rewriting
  revision-bound historical records, with the full mapping recorded (§10).
* Goldens are encoder-level round-trips over representative values, not validation fixtures:
  they pin the persisted byte contract of every journal family, not every validation branch.
* `RunContainmentState.HasProcessDetails` is exported without a doc comment (the base comment
  stays on the delegating `RunJournal.HasProcessDetails`); the owner comment policy prefers no
  comments over restated ones.
* Residual risk: the four sub-ledger blocks are embedded, so a future field added to a block is
  silently persisted at that block's position; the golden key test is the detector — it fails
  when a moved field's key appears outside its golden position.
