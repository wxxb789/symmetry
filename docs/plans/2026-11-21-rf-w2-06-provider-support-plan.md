# rf-w2-06 provider support plan — shared provider support module (CCS2)

Contract: the GDD goal contract `goal-rf-w2-06-provider-support.md` (2026-11-21, GDD state
root) plus its binding review amendments: the contract's own "Contract review amendments"
section and `refactor/w2-contract-review-opus-1.md` P2-2 / `refactor/w2-contract-review-ds-1.md`
A1 (where they differ, the stricter reading wins). Baseline for every coordinate in this plan:
`origin/main` at `4dd374bdcdbe09a05287e77ad90faaa949fcddcd` (this worktree's HEAD). Branch:
`refactor/rf-w2-06-provider-support`. Area: `control/lib/symmetry_control/integrations/providers`
plus the three integration suites.

The increment extracts the shared HTTP-backend provider plumbing out of the two parallel
provider implementations into one `SymmetryControl.Integrations.Providers.Support` module.
Provider-specific URL/body mapping, pagination, reference parsing and every error atom stay
where they are; both provider suites are the acceptance oracle.

## Derivation: what is actually shared (measured on this tree)

The contract's "19 byte-identical helpers" and "pagination/ref/URL shaping" do not reproduce
(F3:222-240 is the finding, not the measurement; the binding A1/P2-2 amendments both say the
count is wrong). The set below was re-derived mechanically by
`<GDD-STATE>/refactor/tools/w2-06-provider-support/provider_ast.exs`, which parses both
sources with `Code.string_to_quoted/2`, strips all metadata, groups `def`/`defp` clauses by
`(name, arity)` and compares the clause-group ASTs:

- **Strategy A** (definition-name/arity intersection): 26 `(name, arity)` pairs are defined in
  both providers (github 95 clauses, azure 88).
- **Strategy B** (clause-group AST equality of those pairs): **13 pairs are equal** —
  `auth/0`, `check/1`, `decoded_path_segments/1`, `http/0`, `latest_by/2`, `now/0`,
  `priority/1`, `required_datetime/1`, `same_reference?/2`, `segment/1`, `sync_delivery/3`,
  `sync_resource/2`, `sync_resource/3`.
- **Extraction set: 12 pairs.** `sync_resource/3` stays: its single call goes to the
  provider-private `sync_resource_with_headers/3` (verified by the tool's dependency column),
  so it cannot move without either exposing a private provider mapping function or moving the
  mapping itself — both forbidden by the contract's failure control.
  [Apply-stage correction, 2026-11-21, accepted finding F2: the pair is un-movable *into
  `Support`* as written, but it is removable in place — `sync_resource_with_headers/3` is
  inlined into the public `@impl sync_resource/3` in both providers, so no residual
  `sync_resource/3` duplicate pair remains (after-count 4 → 3, all pure `Support` delegators).
  See the apply-stage answer for the re-measured numbers.]

| Shared pair | github | azure | Move shape |
| --- | --- | --- | --- |
| `auth/0` | :964-970 | :899-905 | as-is (`defp` → `def`) |
| `http/0` | :956-962 | :891-897 | as-is |
| `segment/1` | :972 | :877 | as-is |
| `required_datetime/1` | :974-982 | :879-888 | as-is |
| `now/0` | :985 | :907 | as-is |
| `latest_by/2` | :701-706 | :724-729 | as-is |
| `priority/1` | :708-718 | :764-774 | as-is |
| `same_reference?/2` | :911-912 | :851-852 | as-is |
| `decoded_path_segments/1` | :903-909 | :840-849 | as-is |
| `check/1` | :40-42 | :62-64 | provider argument |
| `sync_resource/2` | :59-63 | :79-83 | provider argument |
| `sync_delivery/3` | :460-467 | :218-225 | provider argument (both clauses: the `%{pull_request_url: nil}` short-circuit travels with the delegator) |

Staying per provider (measured non-equal or explicitly excluded by the binding amendment):
`authenticate/1` (different header builder), `check/2`, `sync_resource/3`,
`sync_resource_with_headers/3` (URL/body mapping), `sync_delivery/4`, `sync_ci/4`,
`execute/6`, `execute_change/7`, `request/4` (per-provider base URL and response-headers
handling), `repository_parts/1` (owner/repo validation vs `parts: 2` split),
`pull_request_state/1` (merged-at vs status mapping), `ci_reference/1`, the pagination
(`issue_page/5`, `review_page/6` vs `build_pages/8` + `@max_build_pages`), and every
per-provider error atom.

`decoded_path_segments/1` reconciliation: DS A1's stay list names it, but DS's own text says
it "differs by formatting only" and the binding derivation rule is AST equality, which makes
it one of the 13 equal pairs; the contract's binding amendment names only `repository_parts/1`,
`pull_request_state/1` and pagination as staying. It is therefore extracted. Alternative if a
reviewer prefers DS's list verbatim: keep it local in both providers — a two-line revert with
no behaviour change.

## Public / API shape

New module `SymmetryControl.Integrations.Providers.Support` (`@moduledoc false`), twelve
functions:

```elixir
check/2, sync_resource/3, sync_delivery/4   # (provider_module, ...) authenticate-then-continue
http/0, auth/0                              # same :symmetry_control Application keys as before
segment/1, now/0, required_datetime/1       # as extracted
latest_by/2, priority/1, same_reference?/2, decoded_path_segments/1
```

The two provider modules keep their complete existing public surface: every `def` keeps its
name and arity, and both keep `@behaviour SymmetryControl.Integrations.Provider`. The three
`(provider_module, ...)` functions replace the local `with {:ok, headers} <- authenticate(...)`
wrappers; the providers call `Support.check(__MODULE__, connection)` etc. Callback dispatch
becomes dynamic (`provider.check(connection, headers)`); the compile-time protection moves to
`@behaviour` + the `mix compile --warnings-as-errors` gate, which fails if either provider
stops implementing a callback.

`Support.sync_delivery/4` carries two clauses: the `%{pull_request_url: nil}` short-circuit and
the authenticate-then-continue delegator, in the base clause order. The provider entry points
become one-line delegators, so the short-circuit matches in `Support` instead of in the
provider: the identical map pattern returns the same `{:ok, nil}` without touching the
connection (no authenticate call, no request).

## Edge-case semantics the contract demands

- **Per-provider mapping and error atoms untouched.** No function body that shapes a URL,
  query, request body, response map or error atom is edited, moved or re-typed. The suites
  assert request URLs/bodies and the atoms (`:invalid_request`, `:forbidden`,
  `:invalid_pull_request_url`, `:invalid_provider_response`, `:capability_not_granted`,
  `:unsupported_resource`, `:ambiguous_provider_objects`, `:missing_head_sha`,
  `:missing_commit_id`, `:missing_ci_reference`, `:ci_history_limit`, `{:transport, _}`
  passthrough).
- **Test stubs keep working.** `http/0`/`auth/0` move but keep reading
  `:symmetry_control, :integration_http_client` and `:integration_auth_provider`, which both
  suites stub (`github_test.exs:7-34`, `azure_devops_test.exs:7-34`); the module that reads
  the environment is not observable to the stub.
- **Order and error propagation in the wrappers.** `Support.check/2` performs
  `authenticate/1` first and only then the provider's headers-arity callback, exactly as the
  replaced `with` did, so an authenticate error short-circuits identically and no request is
  made on the failure path.
- **The nil-work-item short-circuit is unchanged.** `Support.sync_delivery/4` clause 1 matches
  the base provider pattern (`%{pull_request_url: nil}`, a map pattern, structs included) and
  returns `{:ok, nil}` before any authenticate call, exactly as the provider clause did; the
  4-arity callbacks keep their own nil guard in both providers untouched.
- **`sync_resource/3` stays.** Its body is AST-equal across providers but its callee is
  provider-private, so it remains a two-line delegator in each provider.
  [Apply-stage correction, 2026-11-21, accepted finding F2: superseded — the private callee is
  inlined into the public callback above the provider mapping, so neither provider keeps a
  delegator. No mapping or error atom changed.]
- **No suppressions, no behaviour flag.** No test, type, validation or permission is weakened;
  no `credo:disable` or `#[allow]` is added.

## Evidence plan (exact commands, run on this worktree)

| Gate / artifact | Command | Expected |
| --- | --- | --- |
| derivation (before) | `provider_ast.exs github.ex azure_devops.ex` at base blobs | 26 pairs / 13 equal |
| duplicate-helper count (before/after) | `provider_ast.exs ... --dups-only` at base blobs and at head | implement revision 13 → 4 (`support_delegate=true` for `check/1`, `sync_resource/2`, `sync_delivery/3`; `false` for `sync_resource/3`); apply revision (F2) 13 → 3, all `support_delegate=true` |
| extraction proof | `extract_proof.exs support.ex base-github.ex base-azure.ex` | 9 `as-is` pairs AST-identical to both base providers (modulo `defp` → `def`), 3 wrappers with `head_without_provider_equal=true`, 0 mismatch; re-run at the apply revision: byte-identical output (F1 kept unapplied) |
| targeted suites (before) | `mix test --seed 0 --formatter SymmetryControl.ExUnitJsonFormatter <3 files>` | green, events file recorded |
| targeted suites (after) | same command at head | same set of test names/states; apply revision (F3) 51 passed — base 49 plus the two new nil-URL short-circuit cases (names diff = exactly those two) |
| format | `mix format --check-formatted` | exit 0, empty |
| compile | `mix compile --warnings-as-errors` | exit 0, 0 warnings |
| whitespace | `git diff --check` | exit 0 |
| mutation checks | flip `Support.priority/1` (`"priority:urgent"/"priority:high"` → `"mutated"`), flip `Support.segment/1` (identity), flip `Support.check/2` (skip authenticate); apply revision (F3) adds a flip of `Support.sync_delivery/4` clause 1 (delete the `%{pull_request_url: nil}` short-circuit) | observed: exit 2 with 2 / 9 / 2 failing tests (both providers in every flip); apply-revision flip: exit 2, 2/2 new cases red (`{:error, :authenticate_must_not_be_called}` vs `{:ok, nil}`); reverted, then format/compile/suites green again |
| full suite | `ERL_SCHEDULERS=4 mix test --seed 0 --exclude portal_assets` (the conductor may re-run it on the PR; never two full suites at once on this host) | observed: exit 0, `938 passed, 21 skipped, 4 excluded` in 707.3 s; apply revision re-run: exit 0, `946 passed, 21 skipped, 4 excluded` in 712.0 s (938 plus origin/main's tests merged into `ef567a1c` plus the 2 new cases) |

## Design trade-offs

- **Module-parameterized wrappers vs. leaving three duplicated wrappers vs. `__using__`
  macro.** Chosen: module parameter. Leaving them keeps 4 duplicated pairs and contradicts
  the 12-pair acceptance; a `__using__` macro would inject the same code into each module
  (zero runtime indirection) but adds a code-generation layer over three 3-line functions and
  the repo has no provider-side `use` precedent — rejected as a wrapper-only magic layer.
- **The `%{pull_request_url: nil}` short-circuit moves with its pair.** Keeping the two
  nil-guard lines in the providers would leave one byte-identical *logic* clause outside
  `Support` and force the after-count to read "3 delegators + 1 pair with a logic clause".
  Moving it keeps clause order and the observable result identical and leaves every remaining
  duplicate pair a pure delegator. The production caller already short-circuits nil
  (`integrations.ex:690`), so this is a defensive guard on the provider's public entry point.
- **`alias` + qualified calls vs. `import`.** The provider call sites become
  `Support.segment(...)` etc. `import` would keep the call-site text literally unchanged, but
  this repo imports only DSL modules (`Ecto.Query`, `Ecto.Changeset`) and its one prior helper
  extraction (`Workspaces.ChangesetValidators`) is consumed by `alias` + qualified calls.
  Following the existing convention wins; the mechanical proof is the derivation tool, not
  diff minimality.
  [Apply-stage correction, 2026-11-21, accepted finding F5: "imports only DSL modules" is
  overstated — `SymmetryControlWeb.QueryParams` is a plain-function module imported by
  `goal_controller.ex:5` and `portal_api_controller.ex:4` (and `Ecto.Changeset` itself imports
  plain functions). The `alias` + qualified-call decision and its `Workspaces.ChangesetValidators`
  precedent stand; only this rationale sentence is corrected.]
- **One `Support` module, not three.** The pairs fall in three families (environment/HTTP,
  value helpers, authenticate wrappers) but the contract names one file and the module is
  small; splitting would be a second convention for no benefit.

## Out of scope (with reasons)

- `provider_access.ex` intent-context unification (W4-06) and `http.ex` rescue narrowing
  (W3-11): different modules, different owners; contract clause 4.
- The syncer (`integrations/syncer.ex`) and the provider behaviour module: untouched.
- Pagination, `repository_parts/1`, `pull_request_state/1`, `ci_reference/1`, `request/4`,
  `sync_ci/4`, `execute/6`, `execute_change/7`: measured non-equal (or excluded by name), and
  merging them would change provider semantics.
- `sync_resource/3`: private dependency, see above. [Apply revision, F2: its private callee is
  folded in place; the function still stays provider-specific (not moved into `Support`).]
- New tests: the owned set has no new test file and the contract's oracle is the three
  existing suites; the base-vs-head event-stream comparison plus the mutation checks carry the
  parity evidence. (A `support_test.exs` would need an owned-set extension; not requested.)
  [Apply revision, F3: still no new test file — one nil-URL short-circuit case is added to each
  of the two owned provider suites (49 → 51) and its guard mutation observed red.]

## Ambiguities resolved (options and the chosen reading)

1. **Owned set vs. required plan document.** The contract's owned set lists only the two
   providers, `support.ex` and three test files; the program-wide rule adds
   `docs/plans/<date>-<slice>-plan.md` to the owned set, so this plan file is legal. Nothing
   else outside the set is edited.
2. **"19 byte-identical" vs. the measured set.** The binding amendment (AST equality, recount
   before extracting) wins over the finding's prose; the count above is the answer.md artifact.
3. **`decoded_path_segments/1`.** Extracted, per the reasoning above; a two-line revert if a
   reviewer insists on DS's literal stay list.
4. **`sync_resource/3`.** Kept local; the "12 pairs" acceptance is met by the 12 extractable
   pairs and the remaining duplicate is named and justified rather than forced into `Support`.
   [Apply revision, F2: kept local, and the residual duplicate pair removed in place — no
   duplicate pair outside `Support` remains (13 → 3).]
5. **`sync_delivery/3`'s nil clause.** Moved into `Support` rather than left duplicated in the
   providers (see trade-offs); the alternative — keeping the guard local — is a two-line
   revert that would leave the pair with one duplicated logic clause.

## Comment-policy sweep (2026-11-22)

Module intent removed from `control/lib/symmetry_control/integrations/providers/support.ex` by the
2026-11-22 comment-policy sweep is recorded below (workstream record "ws8-control-lib").

- `control/.credo.exs` (header, lines 1-23): *Credo configuration for the Control
  application, added with the advisory `mix credo --strict` CI step (refactor slice
  W1-01); the step does not gate — the strict issue count is a metric and the count
  recorded on the PR is the ratchet baseline the burn-down slices measure against. The
  check list is `mix credo gen.config` output (Credo 1.7.19) with two contract changes;
  measured 459 strict issues with `checks: %{}` at 1.7.19 vs 403 from a Credo that dropped
  AliasOrder/AliasUsage/PreferImplicitTry/StringSigils/Apply (their findings sum to 67).*
  (The `%{}`-fallback why is retained in-file as a 2-line K3 note above `checks: %{`; the
  two per-check whys are retained beside their entries — see below.)
- `control/lib/symmetry_control/integrations/providers/support.ex` (4-9): module purpose
  and ownership boundary — *Shared plumbing for the HTTP-backed provider implementations.
  Provider-specific URL/body mapping, pagination and error atoms stay in the provider
  modules.* (The dropped provenance line: *Every function below was AST-identical across
  GitHub and AzureDevOps before this module existed (`decoded_path_segments/1` differed by
  whitespace only).*)

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
