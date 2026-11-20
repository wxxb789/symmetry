# test-db-recovery-docs plan — document default test-database recovery, refresh two stale TODO notes

Contract: `goal-test-db-recovery-docs.md` (GDD, 2026-11-20). Baseline for every coordinate:
`origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, the worktree's HEAD),
branch `fix/test-db-recovery-docs`.

## Deliverable shape

1. `docs/design/quality.md` — the "Commands and gates" section gains the default test
   database's drop-and-recreate recovery, next to the commands it recovers. Documentation,
   not an alias change (contract: prefer documentation).
2. `frontend/src/features/connections/resources.tsx` — drop the `provider_owned` entry from
   the connections page's `refusalGuidance`; no connections-page command can produce that
   code. `frontend/src/features/connections/connections.test.tsx` gains the pinning case.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md` — the three quoted entries/notes move out
   with their closures: the test-database entry under `## Retro review follow-ups`, the two
   marker-less notes under `## Dead-code candidates` (the contract overrides the "a
   marker-less bullet stays where it is" rule for these two).
4. This plan file. No evidence file: the judging artifact is `answer.md`, as in the sibling
   docs increments (`978abba`, `fdf332d`).
5. No Control source, mix alias, test, CI, or contract edit beyond the above.

## Item map (verified at `fdf332d`)

| # | Entry / note (`docs/TODO.md`) | Claim | Verification | Verdict |
| --- | --- | --- | --- | --- |
| 1 | "Document how to recover the repository-default test database, or reset it from the test alias." | A pending migration whose object set already exists fails the `test` alias's migrate step (`42P07 duplicate_table`) and no doc names a recovery | reproduce on a throwaway database: create + migrate, delete the newest `schema_migrations` row of a table-creating migration, re-run `mix test` → `42P07`; then `MIX_ENV=test mix ecto.drop` + `mix test` → green | document the recovery in quality.md |
| 2 | 0006h index-row note | "index row for 0006h targets `0006h-credentialed-provider-accounting.md` while the file lives at `docs/goals/archive/…`" | `docs/goals/README.md:336` targets `archive/0006h-credentialed-provider-accounting.md` (fixed by `c514bd9`, #66); the archive file exists; the two prose references (`:178`, `:214`) also target it | note's premise false → remove with a CLOSED-TODO line |
| 3 | `refusalGuidance.provider_owned` note | no `refusalNotice` call site on `/portal/connections` can receive `provider_owned` | producer search: `:provider_owned` exists only at `workspaces.ex:1003` (`provider_owned_fields_editable/2`, called from `update_work_item/2` at `:203`); only caller `portal_api_controller.ex:265` serves `PATCH /portal/api/work-items/:work_item_id` (`router.ex:82`); the page's call sites call only resource/connection endpoints; the assignment surface keeps its own map (`planning/write-policy.ts:125`) | remove the entry + pin the rendered fallback |

## Exact semantics the entries state

- **(1)** The suite's database is `config/test.exs`'s `POSTGRES_DB` (default
  `symmetry_control_test`) with `MIX_TEST_PARTITION` appended as a literal suffix. The `test`
  alias runs `symmetry.check_test_db`, `ecto.create --quiet`, `ecto.migrate --quiet` first
  (`control/mix.exs:79`), so create+migrate heal a missing or behind database but never an
  object set ahead of `schema_migrations`. The documented recovery must (a) act on the
  database the test environment resolves, (b) not be able to touch a harness-owned database
  (`symmetry.check_test_db` refuses `symmetry_dev` and the `symmetry_0007c_/d_/e_` families;
  it is the first step of the `ecto.drop` alias too), and (c) keep `MIX_TEST_PARTITION` as
  the CI-parallel path, not the recovery recipe. `MIX_ENV=test` is load-bearing in the
  recipe: a bare `mix ecto.drop` resolves the development database.
- **(3)** The connections page's refusal fold (`refusalNotice`, shared by `resources.tsx`
  and `connections.tsx`) renders `server message (code)` plus the page's guidance step when
  the map owns the code. For an unowned code the honest fallback is the server's own
  message. The pin asserts both halves: the exact fallback text, and the absence of the
  removed provider guidance on that page.

## Evidence plan (commands run at the delivered revision, recorded in `answer.md`)

1. Reproduction on the throwaway database `symmetry_control_test_tdbrec` from `control/`, with
   `MIX_TEST_PARTITION` unset, `MIX_ENV=test` and
   `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5433 POSTGRES_DB=symmetry_control_test_tdbrec`, against a
   dedicated disposable PostgreSQL 18.6 instance, kept from the earlier attempt because the shared
   5432 server was saturated by concurrent agent suites then. `mix ecto.create --quiet` exit 0 and
   `mix ecto.migrate --quiet` exit 0 set the state up; `psql … -c "DELETE FROM schema_migrations
   WHERE version = 20260911000000"` printed `DELETE 1`; `mix test
   test/mix/tasks/symmetry_check_test_db_test.exs` exited 1 with `ERROR 42P07 (duplicate_table)
   relation "harness_session_attach_receipts" already exists` in the alias's migrate step, before
   any test ran. Recovery: `mix ecto.drop` exit 0 (`The database for SymmetryControl.Repo has been
   dropped`), then the same `mix test` recreated and migrated the database and reported
   `Result: 11 passed`, exit 0. The throwaway database was dropped again (exit 0) with `pg_database`
   residue `0`; the sequence ran clean on the first attempt.
2. Pinning case red then green over the two revisions of `resources.tsx`:
   `mise x pnpm@12.17.0 -- pnpm exec vitest run src/features/connections/connections.test.tsx
   -t "leaves a provider_owned refusal"` from `frontend/` exits 1 on the pre-fix map (with the
   `provider_owned:` entry temporarily restored) with `TestingLibraryElementError: Unable to find
   an element with the text: field is authoritative in the external provider (provider_owned)` —
   the fold had rendered the guidance suffix — and exits 0 on the delivered map (`1 passed | 55
   skipped`). The full suite (`pnpm --filter @symmetry/frontend test`) reports `20 passed (20)`,
   `494 passed (494)`, exit 0, with no flakes in this run.
3. Gates: touched package's full suite + format/lint/typecheck — frontend
   `typecheck`/`lint`/`test`/`build` (all exit 0, `mise x pnpm@12.17.0 -- pnpm --filter
   @symmetry/frontend …`), plus Control `mix format --check-formatted` exit 0 and
   `mix compile --warnings-as-errors` exit 0 through the recipe. The increment's Elixir evidence is
   the reproduction's focused `mix test` (`Result: 11 passed`); the Control full suite is outside
   the touched package because no `control/` source file changed; the earlier attempt recorded 97
   `53300` connection-storm failures against the saturated shared server and treated them as an
   invalid environment rather than a result.
4. Path-triggered render gate, run as CI runs it: `SYMMETRY_RENDER_GATE_REQUIRED=1`, the seven
   spec files, both projects, against three prod release instances (4000 primary with Goal rollout
   on, 4001 expiry with a 2 s session age, 4002 rollout off) serving the migrated gate database
   `symmetry_control_render_tdbrec` on the dedicated PostgreSQL instance. Host adaptation: the prod
   release binds `[::]` only on Windows (`IPV6_V6ONLY=1`), so a loopback TCP relay republishes
   `[::1]:40xx` on `127.0.0.1:40xx`, keeping the gate's documented origin and `PHX_HOST`. Result of
   the recorded run (`render-gate2.log`, 18.0 min): 91 passed, 1 by-design concurrency skip, and 2
   failures — the same fixture-backed case (`portal-goal-attention.spec.mjs:435`, the Goal context
   snapshot read) timed out at the 60 s cap in both projects (75.2 s desktop, 195.1 s mobile) while
   two other sessions' gates held the host at 100% CPU. A scoped re-run of exactly that case under
   the same gate environment passed in both projects (30.0 s / 22.3 s, exit 0), and the recorded
   run's own JSON report passes CI's executed-set assertion (`assert-playwright-json.py`: the seven
   files, both projects, 1 skip, the three environment-gated cases as plain passes). The failing
   case exercises Goal context snapshot reads and the fixture's second `mix run` app start; the
   increment touches neither.
5. `pnpm install --frozen-lockfile` under the pinned pnpm first.

## Design trade-offs and decisions

1. **Documentation over an alias change** (contract direction). A reset step inside the
   `test` alias would make `mix test` destructive by default; the documentation names the
   two verified commands instead.
2. **The recipe uses `MIX_ENV=test mix ecto.drop` + `mix test`, not `mix ecto.reset`.**
   `ecto.reset` expands to `ecto.setup` = create + migrate + `run priv/repo/seeds.exs`
   (`control/mix.exs:69-71`); today `seeds.exs` is a comment-only stub, but a reset that
   runs seeds couples the test database to seed content, and this repository already
   recorded the failure mode of fixture rows reaching suite-level aggregates. The recipe
   keeps the exact drop-and-recreate the suite needs.
3. **The 0006h note is removed, not rewritten.** Its premise is false at `fdf332d`; the
   closure line records where the link points now and what fixed it.
4. **`provider_owned` guidance is removed from the connections page, not reworded.** With
   no producing endpoint on the page, any wording stays dead; the server's own message is
   the correct fallback, which the pin forces.
5. **The pin lives in the rendered-surface test file, not a map unit test.** The repo's
   existing refusal cases (`connections.test.tsx:237`, `:1089`, `:270`) assert rendered
   notices; the new case follows them and fails on pre-fix bytes for a consumer-visible
   reason (the guidance text rendered where the server's message alone is correct).

## Out of scope (contract clause 4)

Other TODO entries (the `execute_provider_action/5` chain, the unused `conn` parameter,
and every other open item); the render-gate diagnostic item; refactors beyond the fix;
changing the test alias; adding a seed/reset alias; a gate that re-derives refusal-map
reachability.

## Residual risks

- The throwaway reproduction deletes a `schema_migrations` row by hand to make the stale
  state; the recorded occurrence's exact provenance (which migration) is not reconstructed,
  only the class (pending migration whose `CREATE TABLE` object exists).
- The render gate's recorded full run carries two load-induced timeouts on the one fixture-backed
  Goal-context case (both re-verified green in a scoped re-run of the same case under the same gate
  environment, with CI's executed-set assertion passing on the run's own JSON report); no clean
  93/93 full run was recorded, because two other sessions' gates held the host at 100% CPU for the
  whole window.
