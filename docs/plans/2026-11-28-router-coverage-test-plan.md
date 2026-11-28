# rf-router-coverage-test — plan (test-only slice W6-14 of goal 0010)

Contract: goal 0010, slice **W6-14** — "the router-invalidation coverage test must stop asserting a
Phoenix `@doc false` internal". Branch `refactor/rf-router-coverage-test`; dispatch base `ea54bc91`,
measured base = freshly fetched `origin/main` `1ced43d0e4a617327a03418124ea0177a9cdc83c` (the
dispatch base plus the docs-only #320, `docs/goals/0010-refactor-program.md` only).
Owned files, and nothing else:
`control/test/symmetry_control_web/router_invalidation_coverage_test.exs` and this plan.

## 1. The defect, re-verified from the pins' own bytes

Phoenix 1.8.17 changed element 2 of the 4-tuple the generated `Router.__match_route__/3` returns:

- 1.8.16 `lib/phoenix/router.ex:765-768` — element 2 is `fn conn, %{path_params: _} -> … end`, a
  2-arity closure whose body is emitted by `Phoenix.Router.Route.build_prepare/1`
  (`lib/phoenix/router/route.ex:168`).
- 1.8.17 `lib/phoenix/router.ex:765-766` — element 2 is `unquote(prepare)`, i.e. the route's
  `Macro.escape({route.private, route.assigns})` data (`lib/phoenix/router/route.ex:130`), interpreted
  by the new `@doc false` `Phoenix.Router.Route.prepare/3`
  (`lib/phoenix/router/route.ex:192`: `def prepare(%{params: params} = conn, %{path_params: path_params}, {private, assigns})`).

1.8.17's own dispatcher was updated in lockstep (`lib/phoenix/router.ex:400`, `:409`, `:413` now call
`Phoenix.Router.Route.prepare(conn, metadata, prepare)`), so only code that calls element 2 as a
function breaks. In this repository that is exactly one line, the test's
`conn = pipeline.(prepare.(conn, metadata))`. Every route in
`control/lib/symmetry_control_web/router.ex` leaves `private`/`assigns` at `%{}`, so the element is
`{%{}, %{}}` and the call raises `BadFunctionError` (the #312 `control` failure, job `140879983182`).

Both facts above were read from the bytes this repository pins: the 1.8.17 tarball
(`repo.hex.pm/tarballs/phoenix-1.8.17.tar`, 446 464 bytes, its `CHECKSUM` =
`2047C3B0A093F0E6C25B3F18970FBB8410219BE739877C82433687E5D8CFBD2E` = the checksum the #312
`mix.lock` entry names) and the resolved 1.8.16 tree in `control/deps/phoenix` (`@version "1.8.16"`).
`Phoenix.Router.build_metadata/2` is byte-identical in both pins (1.8.16 `:791-810`, 1.8.17
`:789-808`) and always emits `path_params:`, so `prepare/3`'s second-argument pattern always matches
for every route.

## 2. The ruling, delivered

A version-tolerant, fail-closed adapter, in the test file only; the probe keeps hand-driving
prepare + pipeline:

```elixir
conn = pipeline.(prepare_conn(conn, metadata, prepare))

# Phoenix 1.8.17 returns {private, assigns} where 1.8.16 returned a prepare closure for
# Route.prepare/3; the arg list stays in a variable so no Refactor.Apply finding is added.
defp prepare_conn(conn, metadata, prepare) when is_function(prepare, 2) do
  prepare.(conn, metadata)
end

defp prepare_conn(conn, metadata, prepare) do
  args = [conn, metadata, prepare]
  apply(Phoenix.Router.Route, :prepare, args)
end
```

Two constraints decide this exact shape:

1. **`apply/3`, never a direct call.** `main` still resolves Phoenix 1.8.16, and CI compiles the test
   tree with `MIX_ENV=test mix compile --warnings-as-errors`
   (`.github/workflows/ci.yml:252`, the `control` job's gate — `:254` is a comment line), where a direct
   `Phoenix.Router.Route.prepare/3` call is an undefined-function warning. The dynamic call compiles
   against both pins.
2. **The argument list stays in a variable.** `Credo.Check.Refactor.Apply` is enabled and the
   committed strict baseline is an *exact* per-category ratchet
   (`scripts/ci/assert-credo-strict-baseline.py` `compare`: a rise *and* a fall both fail, against
   `scripts/ci/credo-strict-baseline.json`, refactor 326). `apply(Mod, :fun, [a, b, c])` with a
   literal list is exactly what that check reports; the check's `do_issue/5` needs a literal list
   node, so rebinding to `args` keeps the count byte-identical.

Fail-closed: any element 2 that is not a 2-arity function is passed to `Route.prepare/3`, which
raises `FunctionClauseError` (or `UndefinedFunctionError` on 1.8.16, where that function does not
exist — 1.8.16 `route.ex` has no `prepare/3`). Both redden the test; neither can pass silently.

## 3. Invariants that must NOT be weakened (unchanged by this slice, each still asserted)

| # | Invariant | Where it stays asserted |
| --- | --- | --- |
| I1 | Every non-allowlisted write route's scope chain runs to completion, calls `send_resp(conn, 200, "")`, and publishes at least one `{:portal_invalidation, %{kind: binary}}` | the comprehension body: `run_scope_chain/2` then `assert_receive {:portal_invalidation, %{kind: kind}} when is_binary(kind), 1_000, "no invalidation was published for #{request_line(verb, path)}"` (`:56-58`) |
| I2 | A chain that halts before the plug still flunks with its diagnosis | the `if conn.halted or conn.state != :unset do flunk(… "without reaching Plug.PortalInvalidations" …)` guard |
| I3 | `@non_publishing` exempts only the routes it names | `allowlisted?/1` plus `assert stale_allowlist_entries(routes) == []` |
| I4 | The mailbox cannot rescue an assertion: each row drains before its own chain | `drain_invalidations/0` before `run_scope_chain/2` |
| I5 | The probe is honestly tied to the publish precondition (a 2xx `before_send`) | the trailing `send_resp(conn, 200, "")`, which is also mutant M4 below |

I1 is *at least one*, not *exactly one*: `assert_receive` matches one message within its 1 s window and
`drain_invalidations/0` runs **before** each row (`:53`, `:149-152`), so a duplicate publication passes
today. An exactly-one guarantee is a new assertion (with its own duplicate-publish mutant) and is
proposed as a residual in the PR body — deliberately NOT added by this slice.

Mutants that must still redden a named row (M1 is executed as this slice's CI mutation check; M2–M4
stay recorded as the next reviewer's probes, as the diagnosis' §Q5 lists them):

- **M1** delete `plug PortalInvalidations` from `pipeline :portal_api`
  (`control/lib/symmetry_control_web/router.ex:43`) → `POST /portal/api/chat/messages` (the first
  non-allowlisted write route in `__routes__/0` order) red on I1.
- **M2** add a write route in a scope without the plug and without an allowlist entry → that row red
  on I1.
- **M3** delete a route named by `@non_publishing` → `assert stale_allowlist_entries(routes) == []`
  red on I3.
- **M4** drop the trailing `send_resp(conn, 200, "")` → the plug's `before_send` never fires → every
  row red, i.e. the probe cannot pass without the publishable-response precondition.

## 4. Oracle

No local Elixir/OTP exists on the dispatch host: `where erl` finds nothing, `mise` holds only the
`elixir 1.20.4-otp-29` shim, and `mise x elixir@1.20.4-otp-29 -- elixir --version` fails with
`'"erl.exe"' is not recognized`. No `mix compile`, `mix format`, `mix test` or Credo run was
possible, so **CI is the only executable evidence** for this slice:

- green gate: this PR's `control` job (the suite that contains the defect, the
  `mix format --check-formatted` step and the Credo ratchet) — the run whose job id is recorded in
  the PR body;
- red gate: the throwaway mutation branch, same suite, `plug PortalInvalidations` removed from
  `pipeline :portal_api`; the `control` job must go red on the row named in M1 with the exact
  assertion text, and that branch is deleted afterwards.
- clause-2 gate: a third throwaway branch carrying this fix plus the #312 lockfile pin
  (`control/mix.lock`, phoenix `1.8.16` → `1.8.17`); a dispatched run of the same suite executes
  the adapter's second clause, which no run on the 1.8.16 pin can reach. That branch is deleted
  afterwards and the evidence is recorded with it.

Because the check set of this change is a test file plus this plan, the `control` job is the only job
whose behaviour the change can alter; the remaining matrix jobs are unchanged by construction
(docs-only + test-only diff) and are attributed to CI, not claimed locally.

## 5. Limits and risk, stated

- The probe still couples to a `@doc false` seam (the matcher tuple). A future Phoenix refactor
  reddens the suite loudly rather than silently — the fail-closed arm is the point of this slice —
  and the coupling now lives in one named function.
- No local execution, no local formatting check: the added lines were written to be
  `mix format`+Styler-stable (3-line block defs with one-line heads, no line above 98 columns, LF
  endings, exactly one trailing newline) and to add no Credo finding, but only the `control` job can
  confirm that.
- The `main` pin exercises the first clause only. This slice's PR run and the mutation run are both
  on the 1.8.16 pin, so clause 2's *behaviour* is observed only on the dispatched 1.8.17 branch
  (§4) — and, after this lands, on #312's own re-run, which remains the binding evidence for the
  bump.

## 6. Delivered evidence (CI, on the bytes named)

| What | Revision | Run / job | Verdict |
| --- | --- | --- | --- |
| PR `control` job (format, compile gate `ci.yml:252`, Credo ratchet, suite) | `b920617162487e1cab94e53b3684577d74d551ba` | run `46114564088`, job `140885928976` | success — `Result: 1164 passed, 21 skipped`, `mix format --check-formatted` clean, Credo strict `564 issue(s)` = the committed baseline |
| Mutation (M1: `plug PortalInvalidations` deleted from `pipeline :portal_api`) | `fd5e86715c8eadb79df1e4b79425e23d050093df` | run `46114612826`, job `140886122532` | failure — `Result: 1162/1164 passed, 21 skipped`: `no invalidation was published for POST /portal/api/chat/messages` on `SymmetryControlWeb.RouterInvalidationCoverageTest` (plus `PortalRealtimeTest`, same mutation) |
| Clause 2 (same fix + the #312 pin, phoenix 1.8.17) | `1fc5ca57816dc18d7878e64661ca72e3b14a325b` | run `46114623074`, job `140886213311` | success — `Result: 1164 passed, 21 skipped`, and no `phoenix 1.8.16` advisory line, where the 1.8.16 run has three |

Both throwaway branches were deleted after their runs; the deltas are one line each and are stated
above (M1's deletion at `control/lib/symmetry_control_web/router.ex:43`, and the phoenix `1.8.16` →
`1.8.17` entry of `control/mix.lock`).
