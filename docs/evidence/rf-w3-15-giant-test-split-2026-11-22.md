# rf-w3-15-giant-test-split — evidence record (2026-11-22)

Contract: `goal-rf-w3-15-giant-test-split.md` (GDD, 2026-11-21) with the conductor reconciliation
(D1–D3, A14/A15, P1-1–P1-3, P2-1–P2-3, P3, D6), the lesson-32 control-flow rule and the owner
comment policy (2026-11-22). Plan: `docs/plans/2026-11-22-rf-w3-15-giant-test-split-plan.md`.

Base revision `6d428d8f79af7c5367899404dc178e854bf628d2` (= `origin/main` at dispatch). Every
dependency the contract names is an ancestor of the base: #149 `b89e14d7`, W2-09 `52bdffd2`
(PR #165 — the contract's "branch `3d20b82a`, no PR" was stale), W3-08 `6d428d8f` (PR #214), W3-10
`dc0aa35d` (PR #202), W3-14 `79dc08ef` (PR #211). Nothing is committed: the tree is left for the
verification stage.

Scratch tooling (state dir, `gdd/refactor/scratch/`, read-only on the repo except for the patch
files it emits): `w3-15-measure.py` (unit spans: Go `^func `..`^}`, ExUnit `test "…"` blocks,
md5 `2543dea4684c323ba7edfc3b096af42e`), `w3-15-generate.py` (base→delivered transformation +
`apply_patch`-format patches, md5 `e295b2951b9f21fad20880203ec787ba` — F1 adjustment, §10),
`w3-15-verify.py` (25 printed move-only / structure checks, md5
`c56ec7831a0a5815b0e07f89aee77959` — same adjustment), `w3-15-capture-go.sh` /
`w3-15-capture-ex.sh` (name-set captures), `w3-15-mix.sh` (portable Elixir 1.20.4 / OTP 29.0.6,
`MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w3-15`, local PostgreSQL `127.0.0.1:5432`).

## 1. Deliverable

| file | diff | change |
| --- | --- | --- |
| `daemon/internal/app/app_test.go` | +142/−135 | the 5 single-use fixture constructs of `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` become file-level helpers (`claimAdmissionControlClient` 61 L, `claimAdmissionDaemon` 26 L, `startClaimAdmissionRun` 11 L, `assertClaimAdmissionNoEffects` 15 L, `claimAdmissionProviderAccess` 9 L); 4+5+3+2 call sites renamed; scenario bodies verbatim |
| `daemon/internal/state/state_test.go` | +450/−429 | the 48-case table of `TestJournalValidatorFirstErrorStringsArePinned`/`validateJournal` becomes `type pinnedJournalMutationCase` + 3 scenario-family funcs (ownership 204 L / scan+release 92 L / record 135 L) joined by `slices.Concat` in the original case order; `"slices"` import; two pinned message consts hoisted |
| `control/test/symmetry_control/goals_test.exs` | +386/−325 | the 328-L test body becomes 4 same-file `defp` phases (86/79/147/66 L) called from the same `test "…"` block in the original order |
| `docs/plans/2026-11-22-rf-w3-15-giant-test-split-plan.md` | new, 267 L | plan (owned set) |
| `docs/evidence/rf-w3-15-giant-test-split-2026-11-22.md` | new | this record |

Per-file counts are `git diff --numstat 6d428d8f -- <file>` against the delivered worktree
(additions/deletions; stable across `--minimal`/`--histogram`/`--patience`/`--no-indent-heuristic`);
`git diff --stat` on the same range: 3 files changed, 978 insertions(+), 889 deletions(-) (the plan
row's `dLOC ≈0` is report-only, conductor D2). The six unedited owned files are byte-identical to
the base:
`git diff --exit-code -- daemon/internal/harness/codex/adapter_test.go
daemon/internal/harness/opencode/adapter_native_work_test.go
daemon/internal/state/linux_cgroup_v2_test.go
control/test/symmetry_control/migrations/goal_0006_migration_test.exs
daemon/e2e/production_linux_v2_cleanup_witness_test.go
control/test/symmetry_control/pi_control_e2e_test.exs` → exit 0 (D3 measured-only pair included).
No production file, no new test file, no new package.

## 2. Unit-length table (the acceptance table)

Before = base `6d428d8f`; after = delivered tree; same tool both sides.

| owned file | before (max unit) | after (max unit) | action |
| --- | --- | --- | --- |
| `internal/app/app_test.go` | **321 @777** | **201 @777** (helpers 61/26/11/15/9; next 177 @6282 untouched) | split |
| `internal/state/state_test.go` | **595 @2457** | **204 @2637** (ownership; record 135, test func 171, scan 92; next 79 @1470 untouched) | split |
| `control/…/goals_test.exs` | **328 @2468** | **8** (the test block; new defps 86/79/147/66; next 207 @3173 untouched) | split |
| `internal/harness/codex/adapter_test.go` | 76 @28 | 76 @28, unedited | measured-closed |
| `internal/harness/opencode/adapter_native_work_test.go` | 176 @66 | 176 @66, unedited | measured-closed |
| `internal/state/linux_cgroup_v2_test.go` | 245 @667 | 245 @667, unedited | measured-closed |
| `migrations/goal_0006_migration_test.exs` | 295 @3440 | 295 @3440, unedited | measured-closed (no split, no edit) |
| `e2e/production_linux_v2_cleanup_witness_test.go` | 879 @170 (+300 helper @1050) | unchanged, unedited | D3 measured-only |
| `pi_control_e2e_test.exs` | 504 @1610, 339 @1266, 338 @702 | unchanged, unedited | D3 measured-only |

Repo-wide units >300 L: **17 at base → 14 delivered**; the three removed are exactly the split
targets. The remaining 14 = the 4 D3 measured-only units (the e2e witness test 879 + the three
pi-control blocks 504/339/338) + the 10 out-of-scope units the contract's P1-3 lists
(`e2e/production_linux_containment_witness_test.go`
632/329, `…containment_negative_witness_test.go` 507, `harness/omo/native_test.go` 469,
`native/linux-gate/protocol/launcher_integration_linux_test.go` 443,
`internal/state/linux_clone_journal_test.go` 395/336,
`e2e/production_linux_containment_replay_witness_test.go` 368,
`harness/opencode/adapter_native_terminal_candidate_test.go` 336,
`e2e/production_linux_v2_owner_cli_test.go` 329).

## 3. Move-only / structure proof

`python gdd/…/w3-15-verify.py <repo> 6d428d8f <candidates> --applied` → **ALL CHECKS PASS** (25
printed checks; base = `git show 6d428d8f:<file>`; re-run at the apply revision after F1 — §10):

- `app.prefix-identical`, `app.unit-top-level 201 L`, `app.call-site-renames
  {newClient 4, newDaemon 5, start 3, assertNoEffects 2}`, `app.test-body-identical-after-move`,
  `app.helper-move[newClient|newDaemon|start|assertNoEffects|providerAccess]` (whitespace-stripped
  body equality with the base closures; `newDaemon` excepts the one blank line F1 removed — §10).
- `state.entry-count 48`, `state.moved-entry-count 48`,
  `state.entries-byte-identical-modulo-indent` (each moved entry equals its base text after exactly
  one tab is removed), `state.case-order-preserved` (index-by-index `name:` equality),
  `state.unit-top-level 171 L`, `state.func-pretable-identical` (modulo the two hoisted consts),
  `state.func-posttable-identical`, `state.loop-header-replaced`,
  `state.import-only-slices-added`, `state.pinned-consts-hoisted`.
- `goals.moved-body-byte-identical` (the four phase bodies concatenated, in call order, are
  byte-identical to the base test body), `goals.phase-sizes 70 63 136 55`,
  `goals.block-size 8 L`, `goals.test-name-set-identical 147 vs 147`, `goals.tag-set-identical`.

Reproducibility: `w3-15-generate.py` regenerates all three delivered files from the base
byte-identically (`cmp` OK for each against the apply-stage worktree; the F1 blank-line
normalization is part of the generator — §10; the generated candidates under
`gdd/…/w3-15-runs/candidates/` equal the delivered tree). The bulk moves were applied with the
harness patch tool from the generated `apply_patch`-format patches (recorded under
`…/candidates/patches/`), each followed by `cmp` against the candidate.

## 4. Adjustments forced by the compiler/formatter (all recorded)

Each is a scope/PLUMBING change outside the moved text; none weakens an assertion.

1. `app_test.go`: the five declarations leave the test body, so the test rebinds the local name
   `providerAccess := claimAdmissionProviderAccess()` (the two scenario references stay verbatim).
2. `state_test.go`: `launchToken` (declared in the group closure) is passed as a parameter to
   `pinnedJournalOwnershipCases` and `pinnedJournalScanAndReleaseCases`; `pinnedJournalRecordCases`
   takes none (its cases never reference it). `go vet` red without this (`declared and not used`,
   `undefined`).
3. `state_test.go`: the two pinned message consts `invalidJournal` / `invalidProcessDetails` move
   from the group closure to the file's seed-const block — a function-scoped const is invisible to
   the new file-level funcs and duplicating the literals would fork the pin.
4. `goals_test.exs`: `validation_runtime` is threaded through the setup map and the
   `goal_loop_accept_validation` head (phase 2's `append_evidence` and closure calls need it); the
   missing key was caught by the ExUnit compile step (`undefined variable "validation_runtime"`).
5. `goals_test.exs`: `mix format` canonicalized the four new head/pattern lines (trailing commas
   dropped from the last map key, the three-argument head reflowed); no moved line changed
   (proof §3).
6. Placement: the type and the three case funcs sit between the test func and the `const (…)` block
   (i.e. immediately after their only caller); the four Elixir defps sit at the head of the file's
   helper section.

## 5. Control-flow audit (lesson 32)

- `app_test.go` (`gdd/…/w3-15-runs/control-flow-audit.txt`): every `return` / `t.Fatal*` /
  `t.Errorf` / `default:` / `WriteHeader` clause of each moved closure, in base order, maps to its
  HEAD line with the same clause kind — counts 10/10, 4/4, 2/2, 4/4 (order+count preserved, `True`
  for each). The handler's `404`-on-non-PUT branch, the `400`-on-bad-body branch and the
  `default:` switch clause are all inside the moved closure and are unchanged; `t.Cleanup(server.Close)`
  still registers on the same `*testing.T` passed by each caller, and the `now` value the helpers
  close over is the test's `now` parameter (same value at every call site).
- `state_test.go`: the 419 moved entry lines contain **no** `if` / `for` / `switch` / `case` /
  `return` / `else` clause (measured) — the entries are straight-line `mutate` closures. The
  drive loop (its `test.want == ""` guard and the `err == nil || err.Error() != test.want` guard)
  is byte-identical to the base (`state.func-posttable-identical`), and the concatenation order
  equals the base case order, so no early return, fall-through or default clause moved or changed.
- `goals_test.exs`: the 327 moved body lines contain **no** `if` / `case` / `cond` / `unless` /
  `else` / `return` clause; the phase boundaries fall between straight-line statements, and the four
  calls execute in base order (`goals.moved-body-byte-identical`).
- Caller packages: nothing outside the three files exercises the moved code (all moved symbols are
  test-only, same-file/package); the foreground gate still covered the edited packages' full suites
  and the two `//go:build linux || windows`-gated calibration packages.

## 6. Name-set pin (mechanical, contract D1)

Go (`go test -json -count=1`, before = base, after = delivered; `jq -r 'select(.Test)|.Test' |
sort -u`, `comm -23` / `comm -13`):

| package | before | after | lost | added |
| --- | --- | --- | --- | --- |
| `internal/app` (Windows) | 1173 | 1173 | **0** | **0** |
| `internal/harness/codex` (Windows) | 174 | 174 | **0** | **0** |
| `internal/harness/opencode` (Windows) | 140 | 140 | **0** | **0** |
| `internal/state` (Windows) | 462 | 462 | **0** | **0** |
| `internal/state` (WSL Ubuntu-24.04, linux-tagged files included) | 584 | 584 | **0** | **0** |

Per-package `pass`/`run`/`skip` counts are identical before/after (app 1172/1173/1, codex
171/174/3, opencode 134/140/6, state 462/462/0). ExUnit (`SYMMETRY_EXUNIT_EVENTS_FILE` +
`--formatter SymmetryControl.ExUnitJsonFormatter`, `goals_test.exs`): 147 rows before and after,
`comm` diffs 0 lost / 0 added, per-test states `147 passed` both sides, `suite_finished.counts`
identical (`invalid 0, total 147, failed 0, excluded 0, skipped 0, passed 147`). `goals.tag-set-identical`
and the file's 147 `test "…"` literals are unchanged.

The CI-gated names are untouched: the ten `go test -run` patterns and the 25 `--assert` names still
intersect the edited files' test-name sets empty (they live in `e2e/*`, `harness/opencode/peer_linux_test.go`,
`harness/omo`, `platform`, `internal/app/harness_test.go`), and the five verbatim ExUnit names are
in the unedited `pi_control_e2e_test.exs` — the D3 selection run (§7) shows all five still selected
under `--only pi_control_e2e_native_positive`.

## 7. Gates (real commands, exit codes, observed counts — implement revision; apply-stage re-runs in §10)

| command | exit | observed |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | no output |
| `go vet ./...` (`daemon/`) | 0 | no diagnostics (found and forced the §4.1/4.2 fixes) |
| `go test -count=1 ./internal/app/ ./internal/harness/codex/ ./internal/harness/opencode/ ./internal/state/` | 0 | `ok` app 112.3 s, codex 13.0 s, opencode 0.37 s, state 5.4 s |
| `go test -json -count=1` same 4 packages (name-set capture) | 0 | 1949 name rows |
| WSL `Ubuntu-24.04` `accept`: `cd …/daemon && go test -count=1 ./internal/state/` | 0 | `ok … 11.642s` (linux-tagged cgroup/clone suites) |
| WSL `-json -count=1 ./internal/state/` | 0 | 584 name rows |
| `git diff --check` | 0 | no output |
| `mix format --check-formatted` | 0 | no output |
| `MIX_ENV=test mix compile --warnings-as-errors` | 0 | `Generated symmetry_control app` |
| `mix test test/symmetry_control/goals_test.exs` (events capture) | 0 | `147 passed` (47.4 s) |
| `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs` (focused, unedited) | 0 | `60 passed` (117.1 s) |
| `mix test test/symmetry_control/pi_control_e2e_test.exs` (bare, module tag) | 0 | `2 passed, 6 skipped` (D3 measurement, unedited) |
| `mix test …pi_control_e2e_test.exs --only pi_control_e2e_native_positive --include skip:pi_control_e2e_native_positive` (CI-shaped selection, `SYMMETRY_PI_CONTROL_E2E=1`, events file) | 2 | selection resolves to the 5 CI-gated names (`excluded` for the other 3); all five fail at the host prerequisite `SYMMETRY_PI_CONTROL_E2E_EXECUTABLE must name the real Pi 1.5.2 executable` (`pi_control_e2e_test.exs:5075`). Recorded as the D3 measurement: the file is unedited and the selection is unchanged; the behavioural oracle stays the CI Pi jobs. |
| `git diff --exit-code` on the six unedited owned files | 0 | byte-identical to base |

Host-path scan: the diff scan for user-profile path fragments (`users/` and this host's profile
name) returns 0 hits, and neither new record cites an absolute host path. (`docs/plans/2026-11-21-rf-w3-08-…-plan.md`
was **not** edited — it is another slice's record; its absolute scratch path is pre-existing.)

## 8. Mutation probes (judging artifact)

Per split unit, two legs on the delivered tree; commands scoped to the unit
(`go test -run '^…$'` / `mix test <file>:<line>`).

| unit | leg A: flip a guarded value | leg B: flip + delete the covering assertion | restore |
| --- | --- | --- | --- |
| `app_test.go` table subtest | `got != 1` → `got != 2` → **exit 1**, `app_test.go:821: control requests = 1, want claim only` (both table subtests) | the 3-line `if got := len(requests)` block deleted, flip still in place → **exit 0** `ok … 0.573s` | byte-identical to the pre-probe copy |
| `state_test.go` case entry | `want:` of `containment uncertainty without evidence` → wrong string → **exit 1**, `validateJournal/containment_uncertainty_without_evidence: mutateJournal() error = run journal containment uncertainty is invalid, want "run journal containment uncertainty"` | that entry deleted (the flip goes with it) → **exit 0** `ok … 0.306s` | byte-identical |
| `goals_test.exs` phase 2 | `assert wakeup_job_count(goal.id) == 1` → `== 2` → **exit 2**, `0/1 passed`, failure stack `goals_test.exs:8103 → goal_loop_accept_validation/1 → goals_test.exs:2471` (the extracted defp is invoked from the test block) | that assert line deleted, flip still in place → **exit 0**, `1 passed` | byte-identical |

Leg B demonstrates the contract's failure mode directly: with the covering assertion removed the
suite goes green, so a split whose extracted code is not actually invoked cannot pass leg A. Leg A
also confirms the split kept each assertion inside the run path (the failing line numbers are inside
the moved/extracted text, and the reported subtest path is the original `Parent/Sub` name).
After restore, `cmp` against the pre-probe copies is OK for all three files, and the delivered tree
equals the regenerated candidates again.

Final-tree re-runs after the probes (the bytes are identical to the rows above, so these confirm the
probe restore, not a new state):
`go test -count=1 ./internal/app/ ./internal/harness/codex/ ./internal/harness/opencode/ ./internal/state/`
→ **exit 0** (`ok` app 105.6 s, codex 12.8 s, opencode 0.34 s, state 5.18 s);
`mix test test/symmetry_control/goals_test.exs` (events file) → **exit 0, 147 passed** (46.4 s,
`suite_finished` counts identical to the after-capture, name-set diff 0/0).

## 9. Residual risks and known limits

- CI-only oracles (unchanged by a test-only diff): the `daemon` job (Linux `-race`), the
  `daemon-windows` job (`-run` patterns / `--assert` groups), the `control` job's
  `pi-control-e2e-*` steps, and — for the unedited witness — the dispatch-only
  `linux-native-v2-acceptance` workflow. Conductor verifies these on the PR.
- The D3 `pi_control_e2e_test.exs` selection run cannot pass locally: no pinned `pi` 1.5.2
  executable and no e2e fixture daemon on this host. The recorded measurement is the selection +
  prerequisite failure; the file is byte-identical to the base.
- `-race` is unavailable locally (no cgo on this Windows host); a test-only move does not change
  concurrency structure, and W3-14 already records this host limit.
- The `internal/app` package is not run under WSL by this contract (the real Linux run is scoped to
  `./internal/state/`); `docs/evidence/rf-w4-05-linux-v2-finalize-2026-11-21.md` §12 records a
  pre-existing WSL loopback hang inside `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`
  (reproduced on the untouched base), so a local red there would not be evidence about this move.
  The moved handler/loopback lines are byte-identical modulo indentation (§3); CI's `daemon` job
  (Linux `-race`) remains the oracle.
- `docs/TODO.md:87-97` (the dead-store follow-up in `app_test.go:4881` and
  `stale_ack_settlement_test.go:32,125`) **stays open** (contract P2-3): this slice edits
  `app_test.go` but not at `:4881`, and `stale_ack_settlement_test.go` is not in the owned set.
  `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` are therefore untouched.
- Retained comments: the 5-line doc comment above `TestJournalValidatorFirstErrorStringsArePinned`
  (why the cases call the exported validator — outside the touched hunks) and the 2-line
  `launchToken` fixture note (a magic literal with its role). No comment was added; the touched
  hunks carry no banners/step labels.
- The plan row's `dLOC ≈0` is report-only (D2); this slice's diffstat is +978/−889 (test text
  re-indentation dominates).
- `linux_clone_journal_test.go` (395/336 L) is W2-09's file and is explicitly out of scope; the
  contract's P1-2 prediction for `state_test.go` (595 L at the rebased head) materialised and was
  split here.

## 10. Apply stage (2026-11-22 — accepted simplify finding F1)

F1 deleted the stray blank line at `app_test.go:1066` (between the returned `&daemon{}` literal and
`claimAdmissionDaemon`'s closing brace; the base closure carried the same line, so the move had
preserved it). Nothing else in the tree changed: no test, assertion, name, tag or count was touched.

- Re-derived at the apply revision (worktree): per-file `git diff --numstat 6d428d8f -- <file>` →
  app `142/135`, state `450/429`, goals `386/325`; `git diff --stat` total `978/889`;
  `claimAdmissionDaemon` helper `27 → 26 L`; app next-max coordinate `177 @6283 → 177 @6282`.
  Repo-wide sweep unchanged at 14 delivered (>300 L; `gdd/…/w3-15-runs/units-apply.txt`).
- Proof re-runs: `w3-15-verify.py` → **ALL CHECKS PASS** (25 printed checks; the
  `app.helper-move[newDaemon]` comparison encodes the accepted one-blank-line delta);
  `w3-15-generate.py` (same normalization added) regenerates all three candidates `cmp`-identical
  to the worktree. Script md5s: verify `c56ec7831a0a5815b0e07f89aee77959`, generate
  `e295b2951b9f21fad20880203ec787ba` (measure unchanged `2543dea4684c323ba7edfc3b096af42e`).
- Mutation probe re-run (§8, app leg only — F1's file): leg A (`got != 1 → != 2`) exit 1 with
  `app_test.go:821: control requests = 1, want claim only` (both table subtests); leg B (flip plus
  the 3-line covering `if got := len(requests)` block deleted) exit 0 (`ok … 0.635s`); restored
  `cmp`-identical to the pre-probe copy (`gdd/…/w3-15-runs/probes/app_test.go.apply-pre-mutation`,
  `md5 17060c6e41b278690dc4f94d7f59892a`) and the focused run green (`ok … 0.584s`). The
  state/goals probe legs were not re-run: those files' bytes are unchanged by the apply stage.
- Gates re-run at the apply revision (all foreground, serial; logs under `gdd/…/w3-15-runs/apply/`):
  `gofmt -l .` 0 (no output); `go vet ./...` 0; `go test -count=1 ./internal/app/
  ./internal/harness/codex/ ./internal/harness/opencode/ ./internal/state/` 0 (`ok` app 126.6s,
  codex 13.0s, opencode 0.37s, state 5.08s); app name-set re-capture (`go test -json -count=1
  ./internal/app/`) 0 → 1173 names, `comm -23`/`-13` 0/0 vs the implement capture and 0 lost vs the
  base capture, actions `run 1173 / pass 1172 / skip 1` identical to the recorded capture; WSL
  `Ubuntu-24.04` `go test -count=1 ./internal/state/` 0 (`ok … 11.839s`); `mix format
  --check-formatted` 0; `MIX_ENV=test mix compile --warnings-as-errors` 0 (forced clean rebuild:
  `Compiling 90 files (.ex)`, `Generated symmetry_control app`); `mix test
  test/symmetry_control/goals_test.exs` 0 → `147 passed` (51.5s; 147 event rows, `comm` 0/0 vs the
  implement capture, states `147 passed`, `suite_finished` counts identical); `mix test
  test/symmetry_control/migrations/goal_0006_migration_test.exs` 0 → `60 passed` (102.9s); the D3
  selection run (`--only pi_control_e2e_native_positive --include skip:…`,
  `SYMMETRY_PI_CONTROL_E2E=1`) exit 2 — identical to the recorded measurement (`0/5 passed, 3
  excluded`, all five red at the `SYMMETRY_PI_CONTROL_E2E_EXECUTABLE must name the real Pi 1.5.2
  executable` prerequisite; file unedited); `git diff --check` 0; `git diff --exit-code` on the six
  unedited owned files 0.
- Host-path scan at the apply revision: the working diff adds no host path and no profile-name text;
  the only pattern matches are the two pre-existing quoted scan texts (evidence §7, plan
  evidence-plan item 7) — no host path is written.
