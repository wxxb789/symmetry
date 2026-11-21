# core-dead-surface plan — delete the control-core dead surface named by F11 (rf-w1-14)

Contract: `goal-rf-w1-14-core-dead-surface.md` (GDD, 2026-11-21), including its binding review
amendment (P3-9). Baseline for every coordinate in this plan: `a3039f0` (the worktree's HEAD,
branch `refactor/rf-w1-14-core-dead-surface`, `origin/main`). Source entries:
`C:/Users/lhan/.local/state/gdd/refactor/findings/control-core.md` F11 and the open
`docs/TODO.md` entry under `## Dead-code candidates` ("Revalidate and remove two pre-existing dead
surfaces found while applying the provider-action-chain-removal simplify findings").

## Deliverable shape

1. `control/lib/symmetry_control/integrations.ex`
   - delete `execute_accepted_resource_sync/2` (clause `:554-577`, catch-all `:579`) — a public
     `def` with zero referents tree-wide.
   - delete `defp record_provider_action_failure/2` (`:1111-1130`) — called only from inside the
     deleted clauses (`:572`, `:575`); the compiler's unused-`defp` warning forces it out together
     with its only caller.
2. `control/lib/symmetry_control/goals/contract_validation.ex`: delete the `validate_adapter/2`
   alias and its `@spec` (`:142-143`) — a one-line delegate to `validate_adapter_capabilities/2`
   with zero callers; the bound surface (`contracts/README.md:302`,
   `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs:21`) names
   `validate_adapter_capabilities/2`, which stays.
3. `control/lib/symmetry_control/integrations/provider_access.ex`: delete the unreachable 3-tuple
   clause of `provider_failure/1` (`:2311-2313`) — no in-tree producer of
   `{:provider_action_failure, code, outcome}`.
4. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the whole entry (both surface (a) and surface
   (b)) is satisfied by this increment, so it moves with a closure paragraph naming the branch, the
   decision and the pinning evidence, plus the one-line header marker the archive file uses for
   every move (repo TODO rule in `AGENTS.md`, section `## Dead-code candidates` of the archive).
5. **No test file is edited, added or deleted.** The contract's binding amendment states
   "no test changes required", and the failure control in clause 3 forbids deleting tests: the dead
   surfaces have no test referent (strategy 1 below), so no assertion is removed or converted and
   no assertion-level mutation check is owed. The mechanical backstop is the compiler (item 1's
   unused-`defp` warning), demonstrated red in this increment's mutation check.

## Revalidation record (both strategies, at implementation bytes)

Baseline revision `a3039f0`; the worktree's only source state is that revision (clean at start).

**Strategy 1 — call-graph / text scan over every tracked file** (docs excluded so the entry's own
citation cannot manufacture a hit):

```
$ git grep -n "execute_accepted_resource_sync" -- . ':!docs'
control/lib/symmetry_control/integrations.ex:472:  def execute_accepted_resource_sync_unprojected(
control/lib/symmetry_control/integrations.ex:492:  def execute_accepted_resource_sync_unprojected(_, _), do: {:error, :invalid_request}
control/lib/symmetry_control/integrations.ex:554:  def execute_accepted_resource_sync(%Connection{} = connection, %ProjectResource{} = resource) do
control/lib/symmetry_control/integrations.ex:579:  def execute_accepted_resource_sync(_, _), do: {:error, :invalid_request}
control/lib/symmetry_control/integrations/provider_access.ex:1569:    Integrations.execute_accepted_resource_sync_unprojected(context.connection, context.resource)
control/lib/symmetry_control/integrations/provider_access.ex:1808:    case Integrations.execute_accepted_resource_sync_unprojected(

$ git grep -n "record_provider_action_failure" -- . ':!docs'
…integrations.ex:572 / :575 (calls inside the deleted clauses), :1111 (the defp)

$ git grep -n "validate_adapter\b" -- . ':!docs'
…contract_validation.ex:142-143 only (the @spec and def being deleted; the word boundary
excludes the live `validate_adapter_capabilities/2`)

$ git grep -n -e "apply(Integrations" -e "&Integrations\." -e "function_exported?(Integrations" a3039f0 -- . ':!docs'
  (only the pre-existing surface pin of the provider-action chain at integrations_test.exs:1782-1784;
   no dynamic-dispatch escape names any F11 surface. Without `':!docs'` the same command returns six
   more lines at docs/plans/2026-11-20-provider-action-chain-removal-plan.md:50-52,149-151, which
   quote this pattern as prose)
```

Reading: the unanchored form matches the live `_unprojected` sibling
(`execute_accepted_resource_sync_unprojected/2`, called at `provider_access.ex:1569`, `:1808`); the
word-boundary form matches only the dead function's two definition clauses. `record_provider_action_failure/2`
has exactly the two call sites the TODO entry names, both inside the dead function. The alias has no
caller.

**Strategy 2 — revision-scoped `git grep` at `a3039f0`** (the contract's named revision):

```
$ git grep -nw "execute_accepted_resource_sync" a3039f0 -- control/
  → only integrations.ex:554 and :579 (definition clauses)
$ git grep -n "validate_adapter\b" a3039f0 -- .
  → only contract_validation.ex:142-143 (spec + def)
$ git grep -nE "\{:provider_action_failure, [^,[:space:]]+, [^,[:space:]]+\}" a3039f0 -- .
  → only provider_access.ex:2311 (the clause head) and docs/TODO.md:35 (the entry's prose)
```

Reading of the 3-tuple clause: the only literal builders of `{:provider_action_failure, …}` in the
tree are the 4-tuple forms — `unprojected_provider_action_failure/1` (`integrations.ex:369`, with
its pass-through clause at `:364`), the clause head of `project_provider_action_failure/2`
(`integrations.ex:514`), the 4-tuple clause head of `provider_failure/1`
(`provider_access.ex:2315`), and the two test literals (`integrations_test.exs:1008`, `:1751`).
No 3-tuple producer exists: the clause can never match on any value the code gives `provider_failure/1`,
whose only caller is `finalize_failure/4` (`provider_access.ex:2186`).

**Strategy 3 — compiled call-site meter** (added because the contract's named compile gate is a
no-op on the pinned toolchain; see "Gate substitution" below). A scratch, uncommitted tracer
(`…/gdd/scratch/rf-w1-14-unreachable.exs`) compiles every `lib/**/*.ex` with the Elixir
`:tracers` option and records function-level call events (remote, imported, local):

```
SymmetryControl.Integrations:               execute_accepted_resource_sync/2 → exported, 0 call sites
SymmetryControl.Integrations:               record_provider_action_failure/2 → not exported (defp), 2 call sites
SymmetryControl.Goals.ContractValidation:   validate_adapter/2 (and the default-arg /1) → exported, 0 call sites
TARGET lines: execute_accepted_resource_sync/2 exported call_sites=0; record_provider_action_failure/2 not-exported-or-absent call_sites=2; validate_adapter/2 exported call_sites=0
```

## Deletion set, and why nothing else becomes dead

Deleted:

| Deleted | Evidence it is dead |
| --- | --- |
| `execute_accepted_resource_sync/2` (`integrations.ex:554-577`), catch-all (`:579`), public | zero referents in both text strategies and the compiled meter |
| `defp record_provider_action_failure/2` (`integrations.ex:1111-1130`) | its only two call sites (`:572`, `:575`) are inside the deleted clauses; `mix compile --warnings-as-errors` reddens if it survives (mutation check below) |
| `validate_adapter/2` + `@spec` (`contract_validation.ex:142-143`), public | zero referents; `validate_adapter_capabilities/2` is the name every binding document and test uses |
| 3-tuple clause of `provider_failure/1` (`provider_access.ex:2311-2313`) | no producer of that tuple shape (strategy 2) |

Kept, with the surviving reference that keeps each helper alive (all at `a3039f0`):

- `provider_module/1` (*integrations.ex:1680/1684*) — called at `:222`, `:302`, `:326`, `:476`,
  `:643`, `:670`.
- `provider_call/1` (*:1670*) — called at `:224`, `:332`, `:382`, `:480`, `:646`.
- `delivery_updates/4` (*:674*, `:700`, `:715`) — called at `:483`, `:649`.
- `delivery_resource/2` (*:661*, `:666`) — called at `:483`, `:649`.
- `persist_sync/4` (*:774*) — called at `:500`, `:651`.
- `accepted_sync_result/3` (*:1052*) — called at `:501-502`, `:508`.
- `provider_action_error/1` (*:1098-1109*) — still called at `:543` inside
  `mutation_failure_outcome/1`; only its `:1112` caller (inside the deleted `defp`) disappears.
- `record_resource_failure/2,3` (*:1557*, `:1562`) — called at `:517`, `:525`, `:656-657`.
- `mutation_failure_outcome/1` (*:540-551*) — still called at `:369`.
- The `_unprojected` sibling `execute_accepted_resource_sync_unprojected/2` and its catch-all
  (`:472-492`) — called from `provider_access.ex:1569`, `:1808`; untouched.
- The 4-tuple clause of `provider_failure/1` (`provider_access.ex:2315-2317`) and its remaining
  fallbacks — the live classification path for every in-tree reason.

The deletion is exactly the four surfaces the TODO entry and the contract name, nothing more.

## Exact edge-case semantics

- **The catch-all clause dies with its head.** `execute_accepted_resource_sync(_, _)` guarded
  invalid arguments for a function nobody calls; the live sibling keeps its own catch-all.
- **The alias's default-argument wrapper goes too.** `def validate_adapter(data, opts \\ [])`
  generates `validate_adapter/1` and `/2`; the meter shows both are caller-free, so the alias is
  removed whole (there is no surviving arity to keep).
- **The deleted `:capability_not_granted`-style classification list** inside
  `record_provider_action_failure/2` dies with its only caller; its collaborators
  (`provider_action_error/1`, `record_resource_failure/2,3`, `failure/1`) keep live callers (table
  above).
- **3-tuple clause removal is not behaviour-preserving for a producer that does not exist.** A
  hypothetical future 3-tuple would now fall through to
  `provider_failure(_reason) -> {:provider_failure, :ambiguous}` instead of `{code, outcome}`. The
  tree has no producer (strategy 2), the no-shipped-release boundary allows the removal, and the
  residual risk is recorded below rather than papered over with a dead clause kept "just in case".

## Gate substitution: `mix xref unreachable` is a no-op at the pinned toolchain

- The contract names `mix xref unreachable` with "was 26 lines". At the repo's pinned Elixir
  (`1.20.4`, the version CI installs at `.github/workflows/ci.yml:228`), the task is deprecated and
  inert: `elixir-1.20.4/lib/mix/lib/mix/tasks/xref.ex:479-483` prints
  `The unreachable check has been moved to the compiler and has no effect now` and exits 0.
  Executed at `a3039f0`: 1 line, exit 0 (`rf-w1-14-xref-before-test.log`). The "26 lines" figure is
  therefore not reproducible by that command on this toolchain, and this increment does not report
  it as a pass.
- The compiler replacement is the unused-definition warning path
  (`lib/elixir/lib/module/types.ex` `warn_unused_def`/unreachable set): it surfaces through
  `mix compile --warnings-as-errors`, which the contract already requires and which this increment
  runs before and after, plus as a red mutation check.
- For the public zero-caller surfaces the deprecated task used to list, the substitute meter is
  strategy 3 above: scoped to the three owned modules, it names
  `execute_accepted_resource_sync/2` and `validate_adapter/2` before the change; after the deletion
  those definitions no longer exist, so the entries are gone. The remaining entries in that scoped
  list (framework callbacks such as `init/1`/`handle_call/3`, default-argument wrappers such as
  `validate_workspace_read/1`) are the same false-positive class the deprecated check had and are
  out of scope per contract clause 4; they are reported unchanged, not "fixed".

## Failure controls (contract clause 3)

- Deleting a function with a live caller would fail compilation (`undefined function`); the two text
  strategies and the compiled meter establish there is none.
- Leaving `record_provider_action_failure/2` behind breaks the build under
  `MIX_ENV=test mix compile --warnings-as-errors`: executed as a real red mutation run
  (remove the two call sites, keep the `defp`) and recorded with its exit code before restoring.
- Deleting tests instead of the dead surface is excluded by construction: `git diff --stat` shows
  `control/test/**` untouched.

## Evidence plan (all commands foreground, Elixir via scratch `rf-w1-14-mix.sh`)

1. `sh rf-w1-14-mix.sh format --check-formatted` — before and after.
2. `MIX_ENV=test sh rf-w1-14-mix.sh compile --warnings-as-errors` — before, in the red mutation,
   and after (CI's control-job ordering: format, then test-env compile).
3. `sh rf-w1-14-mix.sh xref unreachable` — before/after, recording the inert output and exit code,
   plus the source citation above.
4. `sh rf-w1-14-mix.sh run --no-start …/rf-w1-14-unreachable.exs` — the substitute meter,
   before/after.
5. `sh rf-w1-14-mix.sh xref callers <owned module>` for the three owned modules, before/after.
6. `sh rf-w1-14-mix.sh test test/symmetry_control/integrations*` — the glob expands to
   `test/symmetry_control/integrations/` (6 files) and `test/symmetry_control/integrations_test.exs`.
7. `SYMMETRY_PROVIDER_BROKER_E2E=1 sh rf-w1-14-mix.sh test
   test/symmetry_control/provider_broker_e2e_test.exs` — CI runs this file with that environment
   variable and a Go toolchain on `PATH` (both available on this host).
8. `git diff --check`.
9. The full `mix test` suite is the conductor's gate-runner step, not this stage's (never two full
   suites at once; shared PostgreSQL has a 100-connection cap); the CI-only oracle stays CI.

## Out of scope (contract clause 4)

- Other `xref`/meter entries not named by F11 (framework callbacks, default-argument wrappers).
- W2-07 mutation-id helpers; any provider behaviour change.
- Anything else in `docs/TODO.md` (the two marker-less notes at `:320`/`:326` under `## CI consolidation owner decisions (2026-11-17)`, the `conn`
  parameter entry, archived at `docs/archive/CLOSED-TODO.md:1473`, the provider-action flake entry, the retro follow-ups).
- The frozen 0008c model-routing records that cite `Integrations.execute_accepted_resource_sync/2`
  as part of an evaluated subject (`docs/evaluations/model-routing/0008c-*.json`, e.g.
  `0008c-b-strong-review.json:16`): they are historical records of a frozen revision and are
  immutable; they are outside this increment's owned set and are not rewritten.

## Design trade-offs and decisions

1. **Delete, not annotate.** Zero referents plus the no-shipped-release boundary makes
  `@doc false` dead weight; a public `def` with no referent is exactly the hazard the entry
  records.
2. **No regression test is added, matching the binding review amendment.** The deletion's
  observable is the absence of a module surface; a `function_exported?` pin would be a test
  *addition* the amendment removed the need for, and the contract's judging artifact for this
  slice is the gate evidence (xref/meter/grep + suites), not a new assertion. The compiler is the
  mechanical backstop and is demonstrated red.
3. **The named-but-inert gate is reported as inert, with the substitute measured.** Reporting
  "xref unreachable passed" for a command that prints a deprecation notice would be a fabricated
  pass; the substitute (compiled meter + warnings-as-errors + red mutation) measures the same
  question with real numbers.
4. **No new committed harness.** The meter and the recipes live in the scratch tree; a committed
  script for a one-time deletion would be new dead weight.

## Residual risks

- The meter is a zero-inbound-call-site signal (the deprecated task's own granularity), not a
  type-aware analysis; it over-reports and its app-wide count (517 of 945 exports) is dominated by
  framework callbacks and default-argument wrappers. The three F11 targets are unambiguous
  (definitions present, zero call sites, no dynamic dispatch).
- The 3-tuple clause removal is safe only while no producer exists; a future producer of that shape
  would silently classify as `{:provider_failure, :ambiguous}`.
- `integrations*` and the provider-broker e2e suite cover the touched modules; the app-wide suite
  runs in the conductor's step, so a regression outside those files would surface there, not here.
- Line numbers cited above are at `a3039f0` and drift after the deletion; the closure names symbols.
