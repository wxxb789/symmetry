# control-test-pins plan — drop the attention test's unused `conn` and pin the pinned historical entry's `can_retry`

Contract: `goal-control-test-pins.md` (GDD, 2026-11-20). Baseline for every coordinate in this
plan and in the delivered entries: `origin/main` at
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, the worktree's HEAD). Test-only
increment inside `control/test/`: `portal_goal_attention_test.exs`, `chat_test.exs`, plus the
TODO archive move and this plan file.

## Deliverable shape

1. `control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs`: drop the
   unused `conn` parameter from `portal_command!/4` and the pass-through in its two helpers
   (`approve_plan/4` → `approve_plan/3`, `activate/2` → `activate/1`) and the one call site
   pair — the fix direction the entry states. No test case, assertion or fixture changes.
2. `control/test/symmetry_control/chat_test.exs`: pin `refute detail.work_item.execution.can_retry`
   at `:838` in "historical run chat remains pinned after retry and cannot control a newer
   attempt", beside the case's existing `refute …can_cancel`, keeping every other assertion.
   Mutation checks (temporary edits, reverted before delivery), all run against the pin and
   recorded in the stage report: (a) remove the `can_retry: false` member of the historical
   forcing; (b) remove the whole forcing block; (c) serve retry eligibility from the pinned
   projection with the member removed (the rule violation the pin guards). Section "Mutation
   results" below states the outcome of each.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: both entries move verbatim (with their
   section placement), each with a closure statement naming the branch
   (`fix/control-test-pins`), the decision and the pinning test names; the CLOSED-TODO preamble
   gains the 2026-11-20 move sentence, matching the file's existing move records.
4. No source change outside those two test files, no contract/schema change (the rule is already
   stated in `contracts/v1/chat-read.schema.json:224` and served), no new gate, no new file
   outside `docs/plans/`.

## Exact edge-case semantics

- **Entry 1** is a warning-only change: `portal_command!/4` builds its own `api_conn()` and
  never reads the `conn` its callers pass; dropping the parameter and the pass-through leaves
  every request byte-identical. The acceptance is negative and per-run: the
  `warning: variable "conn" is unused` line at the `portal_command!` definition must be absent
  from a test-file compile of the module. No behaviour assertion is added because there is no
  behaviour to assert; the file's existing 12 cases are the regression net (a changed request
  path would fail them), and the entry's own evidence is a compiler warning — no product
  decision is open.
- **Entry 2** pins the contract sentence "A superseded run's pinned historical entry always
  answers false" (`contracts/v1/chat-read.schema.json:224`, repeated in
  `work-detail-read.schema.json:142`, `workspace-read.schema.json:281`) at the one surface the
  repository says exercises the shape: the run-scope chat read of a retried run, whose
  `run_projection/2` pinned branch serves `historical: true`
  (`control/lib/symmetry_control/chat/read_model.ex:236-266`) and whose `runs/2` historical
  forcing forces `can_retry: false` among the other controls
  (`control/lib/symmetry_control/chat/read_model.ex:51-64`). The served value must stay
  `false`; the assertion is `refute`, matching the sibling `refute …can_cancel`.
- Mutation-check semantics: the pinned projection (`run_projection/2` else-branch, `:262-266`)
  carries no `retry_runtime_eligible`, while the served execution derives the state gate from
  the pinned task (`execution_summary/1`, `portal_json.ex:404-405`:
  `Orchestration.retryable_attempt?(task_record) and Map.get(snapshot, :retry_runtime_eligible, false)`),
  so a removal of the forcing member alone cannot change the served value, and a removal of the
  whole forcing block changes the entry at its `historical` flag instead. The pin's failure
  control is therefore the rule violation it guards — a pinned projection that starts serving
  retry eligibility — which is executed and recorded as mutation (c).

## Mutation results (measured at the delivered test files)

All three runs are the stage runner's `test test/symmetry_control/chat_test.exs:746` with the
pin at `chat_test.exs:838`; (a) and (b) are each applied alone to `chat/read_model.ex` and
reverted after, and (c) combines (a) with the eligibility-serving pinned projection that names
the rule violation.

| Mutation | Result |
| --- | --- |
| (a) `can_retry: false` member of the forcing removed | `1 passed`, exit 0 — the pinned path answers false through the state gate without the member |
| (b) whole historical forcing block (`read_model.ex:51-64`) removed | `0/1 passed`, exit 2, failing at `chat_test.exs:836` `assert …historical` — the block's observable effect at this entry is the historical flag |
| (c) pinned projection serves `retry_runtime_eligible: true` + member (a) removed | `0/1 passed`, exit 2, failing exactly at `chat_test.exs:838` `refute …can_retry` (`Expected false or nil, got true`) |

Reading: the pin is not a tautology — it fails exactly when a pinned historical entry starts
answering true (c) — while (a) and (b) record that the forced member is defensive today: the
entry's premise that removing the member alone reddens the pin does not hold at this revision.

## Evidence plan (all commands at this worktree, `MIX_TEST_PARTITION=control-test-pins`)

1. Runner: the `retro-g2-mix.sh` recipe adapted to this worktree (portable Erlang/Elixir invoked
   directly), with the fetched deps of the main checkout reused through `MIX_DEPS_PATH` (the
   checkouts share `mix.lock`; build output stays in this worktree) and
   `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset as the contract requires. The
   partition suffix `control-test-pins` is unique to this goal (`symmetry_control_testcontrol-test-pins`).
2. Entry 1 before/after: `mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs`;
   capture the `variable "conn" is unused` warning line before the edit and its absence after.
3. Entry 2 before/after: `mix test test/symmetry_control/chat_test.exs:746` with the new
   assertion (green), then with the historical forcing mutated (red), then restored (green).
4. Gates: `mix format --check-formatted`; `mix compile --warnings-as-errors`;
   `mix test` (full suite) after
   `mise x pnpm@12.17.0 -- pnpm install --frozen-lockfile` and
   `mise x pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build` stage the frontend bundle the
   four `:portal_assets` cases require (`docs/runbooks/portal.md`). No frontend/daemon/browser
   path is touched, so no render gate or WSL run is triggered (quality.md path rule).
5. Sequencing: the Elixir compile/test runs and the pnpm build run serially, never beside each
   other, per the host-load rule quoted in the stage prompt.

## Out of scope (contract clause 4)

Other TODO entries (including the third retro entry beside these two), any `control/lib` change,
the projected provider-action chain, contracts/protocol edits, and the frontend/daemon suites.

## Design trade-offs and decisions

1. **Assertion placement inside the existing case, not a new case.** The entry names the case
   that already exercises the shape ("Add the assertion … and keep the case's other
   assertions"); a second case would duplicate the elaborate retry/supersede fixture for the
   same served entry. `refute` matches the sibling control's shape.
2. **No product-decision stop.** Both directions are settled by the entries: entry 1's fix is
   mechanical identically-shaped parameter removal, entry 2's assertion value is fixed by the
   contract sentence. The one disagreement discovered is factual (what the mutation can
   observe), not a product choice; it is resolved by measuring both mutation readings and
   reporting the result rather than choosing a reading silently.
3. **No docs/evidence record file.** The repo's shape for test-only maintenance closures is the
   CLOSED-TODO entry plus the stage report (#114, #117, #118 add none); the contract's judging
   artifact is this stage's `answer.md`, which carries the exact commands and outputs.

## Residual risks

- The unused-parameter warning is compiler output, not behaviour: the "before" evidence is a
  captured warning line, not a failing test.
- The `refute …can_retry` pin does not distinguish the `can_retry: false` member's removal alone
  while the pinned projection keeps carrying no retry eligibility (measured green in mutation
  (a)); the report and the closure record state the measured result and the pin's actual
  discriminating scope — the rule violation in mutation (c) — instead of claiming a reddening
  the code cannot produce.
- The full suite needs the staged frontend bundle; if the pnpm staging fails on this host, the
  four `:portal_assets` cases are recorded as pending with the missing environment, never as
  passed (quality.md).
