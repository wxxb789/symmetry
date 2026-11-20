# goal-command-stale-race plan — an internal reconcile advance no longer refuses an operator's Goal command

Contract: `goal-goal-command-stale-race.md` (GDD, 2026-11-21). Coordinate baseline: this
worktree's HEAD `bba6d3a2be5ccb046404d861af1d6265e180deef` (`bba6d3a`), branch
`fix/goal-command-stale-race`; every `control/lib`, test, contract and docs coordinate below was
read at that revision.

## The race at its exact coordinates

A just-accepted operator command sets the Goal's `next_wake_at` to now
(`event_next_wake_at/3`, `control/lib/symmetry_control/goals.ex:8557-8575`, kinds
`activate`/`resume`/`admit_task`/`resolve_decision`/`task_unstarted_settled`), so the accepted
`resume` enqueues `WakeupWorker` (`enqueue_wakeup!/2`, `:8375-8390`). That worker calls
`Goals.reconcile_due_goals/1` (`control/lib/symmetry_control/goals/workers/wakeup_worker.ex:39-49`)
and, for a Goal without automatic execution, clears the due wake through
`Goal.transition_changeset/2` + `Repo.update!` (`goals.ex:6067-6074`; the automatic branch is
`:4108-4116`). `Goal.transition_changeset/2` carries
`Changeset.optimistic_lock(:lock_version)` (`control/lib/symmetry_control/goals/schemas.ex:65`), so
that internal bookkeeping write advances the Goal row's `lock_version` by one. The read model
serves that same `lock_version` as the Goal's `version`
(`control/lib/symmetry_control/goals/read_model.ex:192`), and `ensure_preconditions!/2`
(`goals.ex:8577-8590`) refuses any command whose `expected_version` does not equal it with
`{:stale, %{current_version: ...}}`. The Portal surface reads the served version at submit time
(`frontend/src/features/goals/detail.tsx:76-83`) and carries it verbatim
(`frontend/src/services/commands.ts:115-129`), so a reconcile advance landing in the window between
that read and the POST refuses an operator command whose semantics are still valid — the CI failure
this contract records (`portal-goal-attention.spec.mjs:220`, amend after resume at `:271`, 39 ms
apart, served 11 / posted 11 / server 12).

The same class covers every other internal advance: `append_event!/7` (`goals.ex:8314-8372`) writes
the Goal row with the same optimistic lock for internal events too — `task_settled` (`:2849`),
`task_unstarted_settled` (`:4031`), `automatic_task_admitted` (`:4206`),
`automatic_reconciliation_deferred` (`:4795`, and its existing-event branch `:4816`),
`automatic_admission_blocked` (`:5970`, `:6010`) and `validation_outcome_settled` (`:8071`).

## Decision: space option (A) — the command precondition binds an operator-authority version

Chosen: the Goal row gains `operator_version`, a monotonic counter advanced by exactly one for each
accepted operator Goal command event (including Goal creation) and never advanced by internal
reconciliation, settlement, automatic admission, receipt replay or a non-command row write. The
Portal/operator read serves it as `version`; `Goals.command/4` requires
`expected_version == operator_version` (and `expected_revision == current_revision`, unchanged);
the 409 payload reports `current_version: goal.operator_version`.

This is the contract's option (A) — the server does not count a system-internal reconcile advance
against an operator command whose semantic preconditions still hold — with the operator-visible
token made explicit instead of derived per write site. `lock_version` keeps its present meaning and
its present behavior (a row-level optimistic lock bumped by every writer, pinned by
`goals_test.exs:6889-6902`, where a replayed non-operator `record_usage` must leave it where the
accepted write put it).

Rejected, with evidence:

- **(C) "the reconcile advance does not bump `lock_version`"**, applied as written, is neither
  single-sited nor complete. The internal advances are eleven writes: the four bookkeeping updates
  (`goals.ex:1940`, `:4113`, `:4816`, `:6072`) and the seven system-event appends listed above, each
  of which writes the Goal row through `Changeset.optimistic_lock(:lock_version)` inside
  `append_event!/7` (`:8363-8371`). Suppressing the bump means forking that guard per site, deciding
  per site whether the write is operator-visible, and removing the row-version guard the other
  internal writers rely on. A future internal writer that bumps `lock_version` reintroduces the same
  flake; nothing in the code would fail.
- **(B) client re-read and resend on a 409** leaves the server refusing a valid operator command.
  The client cannot tell "internal advance" from "operator advance" from the refusal payload alone
  (both can carry an unchanged `current_revision`), and the surface's frozen-attempt semantics
  (`detail.tsx:85-114`; a frozen envelope must be resent byte-identically or the server answers
  `idempotency_conflict`) make a rebuild-for-the-same-identity path a new contract, not a retry. No
  automatic re-POST exists today (`frontend/src/app/application.ts:169-171`: `retry: false`; 409 is
  never retried), so (B) would add both the server blind spot and the client path.

## Deliverable shape

1. `control/priv/repo/migrations/20261121000000_add_goal_operator_version.exs` — `bigint`, not null,
   default 1, backfilled `operator_version = lock_version`, plus
   `goals_operator_version_positive` (`operator_version > 0`), mirroring
   `20260909000000_add_goal_0006_control_plane.exs:180,195`; `down/0` drops both.
2. `control/lib/symmetry_control/goals/schemas.ex` — `field(:operator_version, :integer,
   default: 1)` on `goals` (not `cast`-able: only the command path may advance it) and the
   `check_constraint` matching the migration.
3. `control/lib/symmetry_control/goals.ex`:
   - `append_event!/7` computes `next_operator_version = goal.operator_version + (operator_advance ?
     1 : 0)` from an explicit `:operator_advance` option (default false), writes it in the same
     `Changeset.change/2` as `event_sequence`/`next_wake_at`, and reports it in
     `immutable_receipt_snapshot/9` (`:8409-8427`);
   - `:operator_advance` is set at exactly two call sites: `create_new_goal/7` (`:6140`) and
     `execute_command/5` (`:6215`); the seven internal call sites are untouched;
   - `create_new_goal/7`'s response (`:6135`) and `response_for/2` (`:8544-8555`, operator-command
     callers only) predict `goal.operator_version + 1`;
   - `ensure_preconditions!/2` (`:8577-8590`) compares `operator_version` and reports it as
     `current_version`.
4. `control/lib/symmetry_control/goals/read_model.ex:192` — `version: map_value(goal,
   :operator_version)`.
5. `control/test/symmetry_control/goals_test.exs` — the new deterministic regression case (below),
   beside the existing precondition case (`:41`); plus `operator_version` for the preconditions
   built from a row snapshot in the sibling test files that bypass the command API to move a Goal
   row (`orchestration_goal_admission_test.exs:1863`, `:2559`, `:3941`;
   `pi_control_e2e_test.exs:2308`). Assertions and refusal expectations stay as they are.
6. Documentation of the changed precondition contract: `docs/design/protocol.md:258` (the
   "Expected version and revision are mandatory" sentence gains the operator-authority invariant),
   `docs/design/data.md:126-131` (the `goals` row gains `operator_version`),
   `contracts/v1/goal-read.schema.json` (`version`'s description) with regenerated
   `contracts/generated/**`.
7. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the `resolve_decision` page-version entry
   (`docs/TODO.md:94-113`) is closed — the server no longer refuses a page-held version for an
   asynchronous advance, so the entry's motivating hazard is gone and the page-held version is the
   operator-authority token; the residual ("a genuine operator conflict still shows the reload
   recovery") is what the surface already renders (`detail.tsx:217-226`).
8. This plan file. No evidence file: as in the sibling control increments (`#117`, `#118`, `#128`,
   `#129`), the judging artifact is the stage's `answer.md`, which carries the red/green runs and
   the render-gate runs.

## Exact semantics

- **Advance rule.** One operator command event ⇒ `operator_version + 1`, in the same transaction as
  the event insert and under the same `FOR UPDATE` Goal lock the command already takes
  (`lock_command_goal/2`). A replayed command (`replay_command/3`) returns the
  stored receipt and advances nothing; an `idempotency_conflict` advances nothing; a refused command
  (`:stale`, `:invalid_transition`, contract failures) advances nothing because it rolls back.
- **Creation.** A Goal row starts at 1 and its `goal_created` event advances it to 2, exactly what
  `lock_version` did; the creation receipt and the first read agree. The served sequence differs
  from the old `lock_version` series for the transition commands, which wrote the row twice per
  command (`transition_goal!/2` plus the event append: `lock_version + 2`) and now advance
  `operator_version` once — create-then-cancel served 2 → 4 before and 2 → 3 now.
- **Internal writes.** Reconciliation, settlement, automatic admission and every other system write
  keep bumping `lock_version` (unchanged behavior; the DB-level optimistic lock stays honest for
  every writer) and leave `operator_version` where it was. The receipt snapshots of those internal
  events report the unchanged token.
- **Precondition.** `expected_version == operator_version and expected_revision == current_revision`,
  evaluated after replay and contract validation, before `apply_command!/4` — the order is unchanged.
  A genuine concurrent operator change still advances the token, so the existing refusal tests
  (`goals_test.exs:155`, `goal_controller_test.exs:104-123`,
  `portal_goal_attention_test.exs:477-488`) and the browser stale case
  (`portal-goal-attention.spec.mjs:281-322`, a real concurrent operator `cancel`) keep their exact
  expectations. The record's own state and revision predicates in `apply_command!/4` remain the
  semantic preconditions; the token only answers "did an operator act on this Goal since your read".
- **Backfill.** Existing rows start at `lock_version`, i.e. at the highest version any client could
  already have read, so no post-migration read changes value and no pre-migration client read turns
  into an acceptance the operator could not see. From that point the counter only advances on
  operator commands.
- **Terminal Goals.** Every command kind refuses a terminal state in `apply_command!/4`
  (`:6434`, `:6485`, `:6550`, `:6595`), so no terminal row can advance the token; the
  `goals_terminal_immutability_guard` trigger (which fires on `state`, `current_revision`,
  `project_id`) is not involved.

## Regression tests (each fails on pre-fix bytes)

New case in `control/test/symmetry_control/goals_test.exs`, "an internal reconcile advance does not
refuse an operator command, while another operator's advance still does":

1. `admitted_task_fixture/0` gives an active Goal with a due wake (the fixture's `activate` +
   `admit_task` commands leave `next_wake_at = @now`, which is why
   `goals_test.exs:4261` already reconciles it to `[%{disposition: :noop}]`).
2. `before = fetch_goal!(goal.id)` is the operator's read; `lock_before = Repo.get!(Goal,
   goal.id).lock_version`.
3. `Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)` — the exact call the CI evidence
   names — and then `assert Repo.get!(Goal, goal.id).lock_version > lock_before` (the internal
   advance really wrote the row) and `assert fetch_goal!(goal.id).version == before.version` (the
   operator's token did not move).
4. `pause` built from `before` (through the file's `command/4`, `:9220-9228`, i.e. the pre-advance
   read) is accepted: `{:ok, receipt, :created}` with `receipt.goal.version == before.version + 1`.
   Pre-fix this assertion is unreachable: the same command answers `{:error, {:stale, ...}}`
   because `lock_version` moved.
5. `resume` built from the same `before` read is refused `{:error, {:stale, %{current_version: v}}}`
   with `v == receipt.goal.version` — the operator half of clause 1, in the same deterministic flow.

Assertion 3's second half fails on pre-fix bytes as well (the served version moved), so the case is
red for two independent reasons before the fix. Level: `Goals.command/4` + the read model, i.e. the
server contract both the browser spec and the frontend pass through; no timing, no sleeps, no
`Oban.drain_queue` (the case calls the reconcile function directly; the `WakeupWorker` also runs
`recover_terminal_settlements`, `recover_unstarted_cancellations` and `recover_goal_controls`
before it).

## Evidence plan (all commands FOREGROUND, serialized; never a suite beside a benchmark)

1. Runner: a worktree copy of the `retro-g2-mix.sh` recipe
   (`C:/Users/lhan/.local/state/gdd/scratch/retro-g2-mix.sh`) — portable Elixir 1.20.4 / OTP 29.0.6
   invoked directly, `cd` to this worktree's `control/`, `unset PHX_SERVER POSTGRES_PORT
   SYMMETRY_CONTRACTS_DIR`, `MIX_ENV=test`, an own `MIX_TEST_PARTITION=stale_race` (its own
   database), and `MIX_DEPS_PATH` pointing at the main checkout's `deps/` (identical `mix.lock`;
   build output stays in this worktree).
2. Red: the new case alone, on the pre-fix source bytes (the test file changes first, the
   implementation second), with the capture of the exact `{:stale, ...}` failure.
3. Green: the same selection after the implementation, plus the three affected files
   (`goals_test.exs`, `goal_controller_test.exs`, `portal_goal_attention_test.exs`) and the two
   helper files whose envelopes read the row (`orchestration_goal_admission_test.exs`,
   `pi_control_e2e_test.exs`).
4. `mix format --check-formatted`; `mix compile --warnings-as-errors`; then the full `mix test` in
   this partition (the stage's own membership of the `test` alias migrates the new column into a
   fresh database).
5. Contracts: `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile` then
   `pnpm contracts:check` after `pnpm contracts:generate` (the schema description change moves
   `contracts/generated/ts/goal-read.ts`, and the daemon-side generated bundle if the generator owns
   it), plus the frontend `typecheck`/`lint`/`test` for the touched generated TypeScript.
6. Browser render gate: `portal-goal-attention.spec.mjs` only, `--project=mobile-chromium`,
   `SYMMETRY_RENDER_GATE_REQUIRED=1`, 5 runs, against a `MIX_ENV=prod mix release` built from this
   tree with a fresh render database migrated through `SymmetryControl.Release.migrate` and the
   rollout-on/rollout-off instances the suite's gated cases require. Recorded carrier deviations on
   this host: the prod endpoint binds IPv6-any (`control/config/runtime.exs`) while this platform's
   IPv6 sockets are v6-only, so the suite's `127.0.0.1` targets are served by a raw TCP relay
   (`127.0.0.1:4100-4102 → [::1]:4000-4002`, no rewriting, no application change); the release is
   started through its Windows launcher (`bin/symmetry_control.bat`) because the POSIX launcher
   expects `erlexec`; and the context-snapshot fixture's own `mix run` gets `POSTGRES_HOST/PORT/DB`
   naming the release's database plus `MIX_DEPS_PATH` for the shared deps, exactly what the
   browser README means by "the `POSTGRES_*` variables must name the database the release serves".
   Host-load failures outside the changed path are recorded as pending with their missing
   environment, never as passed (`docs/design/quality.md`).
7. `git diff --check`.

## Out of scope (contract clause 4)

- The frontend: with the server split, a page-held `version` is exactly the token an operator
  command must carry, so neither `commands.ts` nor `detail.tsx` needs a change (in particular no
  resend-on-409 and no submit-time read for the plan-approval press). **Superseded on the merged
  tree (`655a5f9`, merge with `origin/main`):** #125's `decisionEnvelope` press reads at submit
  time, and this branch's own frontend delta is the comment-only `servedEnvelope` rewording.
- The decision-level optimistic lock (`expected_decision_version`, `goals.ex:6487-6517`) and
  `GoalDecision.lock_version`: decisions are only changed by operator commands and by the amend
  supersede, which is itself an operator command.
- The unrelated open flakes in `docs/TODO.md` (provider-action broker timers, the Windows/browser
  host-load classes) and every other TODO entry except the `resolve_decision` one above.
- Any change to `lock_version` semantics, to the `goals` trigger set, or to the wire shape of the
  command envelope (the field set stays exactly as `contracts/v1/goal-command.schema.json` pins it).

## Design trade-offs

1. **A new column over a per-write-site suppression rule.** One counter with one writer (the
   command path), one reader (the precondition), one comparison, versus eleven write sites that must
   each keep a hand-rolled guard. The counter also survives new internal writers by default: a write
   that does not advance it cannot reintroduce the flake.
2. **Column name `operator_version`, wire name `version`.** The wire member name, its type and the
   command envelope stay as published; only its documented meaning narrows from "the row's
   `lock_version`" to "the version an operator command must carry". Renaming the wire member would
   churn the frontend, the contracts package and both browser specs for no behavioral gain.
3. **Backfill from `lock_version`, not from an operator-event count.** The count would *lower* the
   served version for Goals with internal writes, so a client read before the deploy could match a
   value that never was the operator's view; backfilling the last served value keeps every
   pre-migration read's meaning and is monotone in the safe direction.
4. **Receipts keep the "+1 prediction" shape.** `immutable_receipt_snapshot/9` and `response_for/2`
   predict the post-append value; for an operator command that is `operator_version + 1` and for a
   system event the unchanged token, so a receipt still reports the Goal version its own event left
   behind (`portal-goal-attention.spec.mjs:364` pins the replay equality).
5. **No branch-guarded dual behavior.** The first release is not shipped, so the change is a clean
   cutover: no fallback that also accepts `lock_version`, no compatibility window.

## Residual risks

- The Python-era-style failure mode "an internal advance changes something the operator *acted on*
  but not the token": covered by `apply_command!/4`'s own predicates (state, revision, decisions,
  admitted work). Example: an internal settlement that would matter to `achieve` is re-checked
  against the durable state at command time, so a refusal is a semantic error —
  `:invalid_transition` (`422`), or `:invalid_decision` (`409`) when a decision predicate such as
  `ensure_completion_subject_not_rejected!` disagrees — never a version `409`. If a future internal
  path ever changes an operator-visible field that no predicate re-checks, the token would lag it;
  the reading is "internal advances never invalidate operator authority, predicates do".
- The Elixir suite's four `:portal_assets` cases need the staged frontend bundle; if the pnpm
  staging is unavailable on this host those cases are recorded as pending with the missing
  environment, never as passed (quality.md).
- The local render gate for `portal-goal-attention.spec.mjs` is a fixture-heavy suite whose failure
  classes under host load are tracked in `docs/TODO.md`; each run's outcome is reported as measured,
  and a failure outside the changed path is pending, not passed.
