# rf-w3-15-giant-test-split plan — split the >300-line test units, names preserved (TC-H)

Contract: `goal-rf-w3-15-giant-test-split.md` (GDD, 2026-11-21) including the conductor
reconciliation (D1/D2/D3, A14/A15, P1-1–P1-3, P2-1–P2-3, P3, D6) and the program-wide "lesson 32"
control-flow rule. Owner comment policy (2026-11-22) applies to every hunk.

Diff base revision: `6d428d8f79af7c5367899404dc178e854bf628d2` = `origin/main` at dispatch; working
tree clean. Every dependency named in the contract is an ancestor of the base: #149
(`b89e14d7`), W2-09 (`52bdffd2`, PR #165 — it carried a PR after all, so "branch `3d20b82a`, no
PR" was stale), W3-08 (`6d428d8f`, PR #214), W3-10 (`dc0aa35d`, PR #202), W3-14 (`79dc08ef`,
PR #211). All `<file>:<line>` citations below resolve at the base revision.

## Re-derivation at the dispatch head

Measurement tool: `gdd/refactor/scratch/w3-15-measure.py` — Go unit = `^func ` .. the line that is
exactly `}` (the contract's awk rule); ExUnit unit = `test "…"` block from the `test` line to the
first line equal to its indent + `end`. Before−after tables are produced with the same tool.

| owned file | L | max unit at HEAD | decision |
| --- | --- | --- | --- |
| `daemon/internal/app/app_test.go` | 10,078 | **321 @777** (`TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`); next 177 @6275 | **split** |
| `daemon/internal/state/state_test.go` | 2,898 | **595 @2457** (`TestJournalValidatorFirstErrorStringsArePinned`); next 79 @1469 | **split** |
| `control/test/symmetry_control/goals_test.exs` | 9,868 | **328 @2468** ("terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop"); next 207 @3494 | **split** |
| `daemon/internal/harness/codex/adapter_test.go` | 3,144 | 76 @28 | measured-closed, not edited |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | 2,487 | 176 @66 (`//go:build linux \|\| windows`) | measured-closed, not edited |
| `daemon/internal/state/linux_cgroup_v2_test.go` | 1,486 | 245 @667 (`//go:build linux`) | measured-closed, not edited |
| `control/test/symmetry_control/migrations/goal_0006_migration_test.exs` | 4,999 | 295 @3440 | measured-closed, not edited |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | 1,866 | 879 @170; helper 300 @1050 (`//go:build linux && amd64`) | D3 measured-only, not edited |
| `control/test/symmetry_control/pi_control_e2e_test.exs` | 5,183 | 504 @1610, 339 @1266, 338 @702 | D3 measured-only, not edited |

P1-2's prediction held with a correction: W2-09 landed as a PR and left the 595-L unit (the review
predicted "502 L at `:2457`" from the branch snapshot; the merged text is 595 L), so `state_test.go`
is a real split target at the dispatch head.

Repo-wide sweep at HEAD: **17 units >300 L** (test files only). The three split targets above; the
three D3 measured-only units above; the remaining **11 are outside the owned set** and stay
out-of-scope/measured (contract P1-3):
`e2e/production_linux_containment_witness_test.go:512` 632 and `:2296` 329,
`e2e/production_linux_containment_negative_witness_test.go:467` 507,
`internal/harness/omo/native_test.go:113` 469,
`native/linux-gate/protocol/launcher_integration_linux_test.go:305` 443,
`internal/state/linux_clone_journal_test.go:1655` 395 and `:713` 336 (W2-09's file, never owned),
`e2e/production_linux_containment_replay_witness_test.go:859` 368,
`internal/harness/opencode/adapter_native_terminal_candidate_test.go:167` 336,
`e2e/production_linux_v2_owner_cli_test.go:2232` 329.

## Deliverable shape

| file | change |
| --- | --- |
| `daemon/internal/app/app_test.go` | 5 single-test closures/vars become file-level helpers (`claimAdmission*`); call sites renamed; both `t.Run` tables and all 3 named `t.Run` blocks stay verbatim |
| `daemon/internal/state/state_test.go` | the 48-case table of `TestJournalValidatorFirstErrorStringsArePinned`/`validateJournal` becomes a named case type + 3 scenario-family funcs joined by `slices.Concat` in the same order; adds `"slices"` import |
| `control/test/symmetry_control/goals_test.exs` | the 328-L test body becomes 4 same-file `defp` phases (byte-identical moved bodies, 4-space indent preserved) called in the original order |
| `docs/plans/2026-11-22-rf-w3-15-giant-test-split-plan.md` | this plan |
| `docs/evidence/rf-w3-15-giant-test-split-2026-11-22.md` | evidence record (tables, diffs, control-flow audit, gate output, mutation red) |

No production code, no new file other than the two records, no test renamed, added, removed or
retagged, no `describe` in either ExUnit file, no new package.

## Split 1 — `app_test.go` (321 L → ≤300 top-level)

The 321 L are dominated by five single-use fixture constructs declared *inside* the test
(`newClient` 60 L, `newDaemon` 26 L, `start` 10 L, `assertNoEffects` 14 L, `providerAccess` 7 L)
plus the 5 already-`t.Run`-separated scenarios (largest 60 L). Move the five constructs to
file-level helpers placed immediately after the test; the scenario bodies and every subtest name
stay byte-identical.

| base construct (line) | delivered helper | signature |
| --- | --- | --- |
| `newClient := func(t, template)` :779 | `claimAdmissionControlClient` | `(t *testing.T, now time.Time, template protocol.ClaimResponse) (*control.Client, <-chan string, <-chan protocol.StateTransitionRequest)` |
| `newDaemon := func(t, store, client, stateDirectory, workspaceService, startCalls)` :840 | `claimAdmissionDaemon` | same args `+ now time.Time` (the closure captured `now` for its clocks) |
| `start := func(t, daemon, work)` :867 | `startClaimAdmissionRun` | `(t, app *daemon, work protocol.Work) state.RunJournal` |
| `assertNoEffects := func(...)` :878 | `assertClaimAdmissionNoEffects` | `(t, store *state.Store, journal state.RunJournal, workspaceService *countingWorkspace, startCalls int, stateDirectory string)` |
| `providerAccess := &protocol.ProviderAccess{…}` :893 | `claimAdmissionProviderAccess` | `() *protocol.ProviderAccess` |

All five names verified free in package `app` (`git grep -w -c` = 0). Call-site edits: `newClient` 5×,
`newDaemon` 5× (incl. `restarted :=`), `start` 3×, `assertNoEffects` 2×; `providerAccess` keeps its
local name, rebound in the test as `providerAccess := claimAdmissionProviderAccess()`. Extracted
bodies keep `t.Helper()` and their statements in order; the only textual change besides the move is
the helper name and the added `now` parameter.

Exactly the two motions the review pre-authorised ("dominated by an inline fake-control closure
plus … `t.Run`s, so helper extraction is mechanical"); no scenario is reordered, no `t.Run` name
changes, so the Windows/daemon jobs' `-run` patterns and `--assert` groups are untouched.

## Split 2 — `state_test.go` (595 L → top-level ~180 L, max table func ~210 L)

`TestJournalValidatorFirstErrorStringsArePinned` = `t.Parallel()` + two `t.Run` groups:
`validateJournal` (459 L: fixture `Validate()` checks + a 48-case mutation table at :2479-2897 +
a 20-L drive loop) and `linuxCgroupReservation` (129 L, stays inline — under the rule).

The 419-L table is chunked into three scenario-family funcs of one new named type; the drive loop
is unchanged and iterates `slices.Concat(...)` so the **case order is byte-for-byte the original
order** (no case moves between families' concatenation):

| func | cases (original indexes) | entries at :2479-2897 | size |
| --- | --- | --- | --- |
| `pinnedJournalOwnershipCases()` | 0-23: valid controls, base journal fields, containment authority/handoff, cgroup reservation | 2479-2678 | ~208 L |
| `pinnedJournalScanAndReleaseCases()` | 24-33: containment scans, cgroup release proofs | 2679-2766 | ~88 L |
| `pinnedJournalRecordCases()` | 34-47: lease/terminal/pending records, command acknowledgements, native usage, input commands | 2767-2897 | ~147 L |

```go
	for _, test := range slices.Concat(
		pinnedJournalOwnershipCases(),
		pinnedJournalScanAndReleaseCases(),
		pinnedJournalRecordCases(),
	) {
```

`type pinnedJournalMutationCase struct { name string; seed string; mutate func(journal *RunJournal); want string }`
replaces the anonymous struct; field set, tags and defaults identical. Entry text moves with one
tab removed (table depth 3 → 2); the move-only proof compares whitespace-stripped declaration text
plus `git diff --color-moved=dimmed-zebra --color-moved-ws=allow-indentation-change`. `slices` is
already imported elsewhere in `daemon/` (`internal/app/events.go`, `internal/app/harness_test.go`),
so no new dependency; stdlib `slices.Concat` preserves the concat order by construction.

Three delivered adjustments, forced by the Go compiler and recorded as such (each is a scope change
outside the table, no entry line is touched):

- `pinnedJournalOwnershipCases` / `pinnedJournalScanAndReleaseCases` take the group's `launchToken`
  fixture as a parameter (`pinnedJournalRecordCases` takes none — its cases never use it); the
  declaration stays in the test func, and leaving it out of the extracted funcs is a compile error
  (`declared and not used` / `undefined`), caught by the `go vet ./...` gate.
- the two pinned message consts the moved entries reference (`invalidJournal`,
  `invalidProcessDetails`) move from the `validateJournal` closure to the file's seed-const block:
  a function-scoped const is invisible to the new file-level funcs, and duplicating the literals
  per family would fork the pin.
- the type and the three case funcs are declared between the test func and the `const (…)` seed
  block, i.e. immediately after their only caller.

Subtests: every case keeps its `t.Run(test.name, …)` under the same `validateJournal` group, so the
`go test -json` name set (`Parent/Sub`) must be identical before/after.

## Split 3 — `goals_test.exs` (328 L test block → ~15 L; 4 defps ≤145 L)

ExUnit 1.20.4 composes names as `test <describe> <name>`; `ci.yml:1355-1364` passes five
`pi_control_e2e_test.exs` names verbatim to `assert-exunit-json.py` ("run and pass exactly once"),
and the ExUnit JSON gates read the event stream. The only conforming split is same-file `defp`
extraction (conductor P2-1): same single `test "…"` string, same tag set, no added/removed/renamed
test. A module-level `defp` body sits at 4-space indent — identical to a `test` body — so each
moved phase is a **byte-identical** line block.

New defps (placed at the head of the file's helper section, before `defp goal_with_items_fixture`
at :8319; all four names verified free):

| defp | moved base lines | size | returns |
| --- | --- | --- | --- |
| `goal_loop_validation_setup/0` | :2469-2537 (admit producer/validation, evidence, reject old-subject evidence) | 70 L (incl. the 13-line `%{}`) | setup map |
| `goal_loop_accept_validation/1` | :2539-2602 (settle accepted, wakeup job, evidence replay/closure, accepted outcome, `accept_outcome` refusal) | 63 L | `accepted_outcome` |
| `goal_loop_resolve_achieve_decisions/1` | :2604-2738 (pre-achieve predicates, completion/review decisions, weaker evidence, optional dependency) | 136 L | `decision_id` |
| `goal_loop_achieve_and_replay/3` | :2740-2794 (achieve, event count, replay, idempotency conflict) | 55 L | — |

The delivered test block calls the four phases in exactly the original order:

```elixir
  test "terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop" do
    setup = goal_loop_validation_setup()

    accepted_outcome = goal_loop_accept_validation(setup)
    decision_id = goal_loop_resolve_achieve_decisions(setup)

    goal_loop_achieve_and_replay(setup, accepted_outcome, decision_id)
  end
```

Data flow: phases 2-4 pattern-match the keys they use in the function head, so each phase's inputs
are explicit and the moved bodies keep every variable reference verbatim. The setup map carries
`goal, item, producer, producer_run, candidate_subject, validation, validation_run,
validation_fence, validation_runtime, evidence, stored_evidence` — all computed inside the moved
phase 1 (`validation_runtime` is required by phase 2's `append_evidence`/closure calls; the missing
key is a compile error caught by the ExUnit compile step). Decision: a
map instead of the file's positional 9-tuple convention (`settled_validation_attempt!` returns one)
because 10 positional values across three call sites is the readability problem this slice exists
to remove; the keys are the same identifiers the body already uses.

## Edge-case semantics the contract demands

- **D1 name pin.** Go: two `go test -json` streams (`internal/app`, `internal/state`, Windows and
  WSL) — `jq -r 'select(.Test)|.Test' | sort -u`, `comm -23 before after` must be empty, and
  `comm -13` (added) must be empty too (no subtest invented). Elixir: the
  `SYMMETRY_EXUNIT_EVENTS_FILE` + `--formatter SymmetryControl.ExUnitJsonFormatter` stream, sorted
  `module<TAB>name` per `test_finished`, `comm -23`/`comm -13` empty; per-test `state` and the
  `suite_finished` counts must be equal, not merely non-decreasing.
- **No assert/phase reorder.** Go: moved declaration text is compared whitespace-stripped, and the
  `slices.Concat` order equals the original case index order (checked positionally in the diff).
  Elixir: each moved block is one contiguous base range and the four calls are in base order.
- **Lesson 32 control-flow audit.** The moved Go closures are single-scope helpers (no early
  returns that differ by caller); the audit enumerates each base early-return/`t.Fatal`
  short-circuit and its HEAD equivalent. The ExUnit phases have no `case`/`cond` branches; the
  audit lists the four phase boundaries and confirms statement-level identity. Recorded in the
  evidence file.
- **A split that only moves code into an unrun helper = not achieved.** All helpers are called from
  the same top-level test (Go) / same `test` block (ExUnit); the mutation check deletes one
  assertion inside each split unit's run path and requires red.
- **Comment policy.** The moved hunks carry no runtime comments; the two existing comments inside
  the moved regions (the `launchToken` 2-line note and the `// strings.`-adjacent banner near
  `:2456` if it lands in a touched hunk) survive only where they state a non-obvious fixture rule.
  No new comments are added.

## Evidence plan (contract clause 5; all foreground, strictly serial)

1. Before capture at the base revision: the 4-package Windows `go test -json -count=1` run, the
   WSL `./internal/state/` `-json` run, the `goals_test.exs` events stream, saved under
   `gdd/refactor/scratch/w3-15-runs/before/`.
2. After capture: the identical commands into `…/after/`; the unit-length table; the `comm -23`/
   `comm -13` diffs; pass/skip/fail count comparison.
3. Windows gate: `gofmt -l .`, `go vet ./...`, `go test -count=1 ./internal/app/
   ./internal/harness/codex/ ./internal/harness/opencode/ ./internal/state/`, `git diff --check`.
4. WSL gate (`wsl -d Ubuntu-24.04 -u accept`): `cd /mnt/q/repos/wt-w3-15-giant-test-split/symmetry/daemon && go test -count=1 ./internal/state/`
   (the linux-only cgroup file's oracle; nothing else in the owned set is Linux-tagged).
5. Elixir gates via `gdd/refactor/scratch/w3-15-mix.sh` (portable 1.20.4/OTP 29, `MIX_ENV=test`,
   `MIX_TEST_PARTITION=rf-w3-15`, local PostgreSQL `127.0.0.1:5432`, deps from the worktree-local
   `control/deps`): `mix format --check-formatted`; `MIX_ENV=test mix compile --warnings-as-errors`;
   `mix test test/symmetry_control/goals_test.exs` (before and after);
   one recorded `mix test test/symmetry_control/pi_control_e2e_test.exs --only pi_control_e2e_native_positive
   --include skip:pi_control_e2e_native_positive` event run as the D3 measurement (no edits);
   one focused `goal_0006_migration_test.exs` run.
6. Mutation check: delete one assertion inside each split unit (an `if`-fatal in the app helper's
   run path is not assert-bearing — the probe uses one subtest assertion in `app_test.go`, one case
   entry's `want` in `state_test.go`, one assertion in a `goals_test.exs` phase), run the owning
   file, record red + the exact message, restore, re-run green.
7. `git diff origin/main...HEAD -- … | grep -i "users/"` host-path scan (no host path is written
   into the repo; scratch scripts live under the state dir).

## Out of scope (with reasons)

- The 11 non-owned >300-L units listed above (other slices own them: W5-01 witness scaffolding,
  the `docs/TODO.md:69-85` cloner-file move).
- `goal_0006_migration_test.exs` (295 L max — the contract's D3/out-of-scope text: no split, no
  edit; W3-14 already folded its scaffolding in PR #211).
- The D3 measured-only pair (`e2e/production_linux_v2_cleanup_witness_test.go`,
  `pi_control_e2e_test.exs`): measured and recorded, never edited.
- Fixture extraction (W3-03), deadline rework (W4-13), Go-double extraction (W4-12/W4-14),
  `harness_test.go`, production code, CI config.
- `docs/TODO.md:87-97` (pre-existing dead stores in `app_test.go:4881`,
  `stale_ack_settlement_test.go:32,125`): the entry stays open (P2-3). This slice edits
  `app_test.go` but not at `:4881`, and `stale_ack_settlement_test.go` is not owned; the entry is
  recorded, not closed, so `docs/TODO.md`/`CLOSED-TODO.md` are untouched.

## Design trade-offs

- **Go: helper extraction over `t.Run` re-grouping.** Adding `t.Run` blocks would invent new subtest
  names and change the JSON stream (`comm -13` non-empty); the contract asks for identical top-level
  names, and the cheap way to shrink `app_test.go` is moving the single-use fixture closures, which
  the review already called mechanical.
- **Go: named case type + 3 table funcs over one giant table.** The alternative at the same
  threshold would have been a single ~420-L func (still >300) or splitting cases into separate
  `t.Run` groups (renames). Concat order = original order removes any execution-order question.
- **Elixir: map threading.** See Split 3; the alternative (10-element tuple matching the local
  9-tuple convention) was rejected for readability — the point of the split.
- **Elixir: 4 phases, not 2.** A 2-phase split (setup + rest) would satisfy the ≤300 rule with less
  churn, but the test is a linear scenario with four natural phases and the owner mandate is a
  thorough uplift; the risk is mitigated by byte-identical bodies and the name-set/count proof.

## Risks and known limits

- `-race` is unavailable locally (no cgo on this Windows host); CI (`daemon` job) remains the race
  oracle, and a test-only move does not change concurrency structure.
- The opener of `state_test.go`'s `validateJournal` group asserts fixture `Validate()` results
  before the table; those two `if err := …Validate(); err != nil` checks stay in place and are not
  part of any >300-L unit.
- The pi_control D3 measurement run needs `SYMMETRY_PI_CONTROL_E2E`/the pinned `pi` binary for
  *pass*; the environment may only produce the selection/skip stream. The accepted measurement is
  the recorded event stream (names + states) for the selected tag, and the file is unedited — the
  CI jobs remain the behavioural oracle (conductor verifies on the PR).
- Elixir compile/test timing on this shared host is noisy (W3-14 F4); no wall-time criterion is
  claimed here.
