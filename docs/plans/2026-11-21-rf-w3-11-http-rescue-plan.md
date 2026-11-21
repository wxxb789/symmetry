# rf-w3-11 http rescue plan — narrow the integration transport rescue (CCS9b)

Contract: the GDD goal contract `goal-rf-w3-11-http-rescue.md` (2026-11-21, GDD state root)
including its conductor reconcile section (DS A10, Opus P1-1/P2-1/P2-2, D1, D6) and the program
plan row W3-11. Baseline for every coordinate below: this worktree's HEAD
`36f9a887635ec89d49cce2cd13b3915a8a875a83` (= `origin/main`, which already contains the
W1-14/#139 and W2-06/#154 increments). Branch: current worktree branch
`Q:/repos/wt-w3-11-http-rescue/symmetry`. Area: one file in
`control/lib/symmetry_control/integrations/` plus its new focused unit test.

`SymmetryControl.Integrations.HTTP` hand-rolls the `:httpc` request inside a whole-function
`rescue`: the request construction (URL/header charlist conversion, `Jason.encode!/1`) sits
*inside* the function the rescue covers, so a provider-side programming error (an un-encodable
body or header value) is classified as a transport failure. This increment keeps the
exception-to-transport mapping only around the socket call; construction raises.

## Current shape (measured on this tree)

`control/lib/symmetry_control/integrations/http.ex`, 63 lines:

| Lines | Content |
| --- | --- |
| `:6` | `def request(method, url, headers, body \\ nil) when method in [:get, :post, :patch, :put]` |
| `:7-20` | request tuple construction: nil-body branch `{String.to_charlist(url), encode_headers(headers)}`, value branch `{String.to_charlist(url), encode_headers(headers), ~c"application/json", Jason.encode!(value)}` |
| `:22-30` | `options` list (`autoredirect: false`, `timeout: @timeout` = 15_000, `connect_timeout: 5_000`, `ssl:` verify_peer + `:public_key.cacerts_get()` + hostname-check match_fun) |
| `:32-38` | `case :httpc.request(method, request, options, body_format: :binary)` with `{:ok, {{_version, status, _reason}, response_headers, response_body}}` and `{:error, reason} -> {:error, {:transport, reason}}` |
| `:39-40` | whole-function `rescue error -> {:error, {:transport, Exception.message(error)}}` |
| `:43-47` | `encode_headers/1` (`String.to_charlist(to_string(name))` / `(to_string(value))` — also raises `Protocol.UndefinedError` for a PID header value) |
| `:49-53` | `decode_headers/1` (downcased string keys) |
| `:55-62` | `decode_body/1` (`""` -> nil, JSON decode with string fallback) |

Callers that bind this seam: `Providers.GitHub`/`Providers.AzureDevOps` reach the module through
`Providers.Support.http().request/4` (default `SymmetryControl.Integrations.HTTP`,
`providers/support.ex:34-40`); `auth_test.exs`, `change_action_test.exs`, `command_test.exs`,
`syncer_test.exs`, `github_test.exs` and `azure_devops_test.exs` are the acceptance oracles.
The two provider suites substitute the whole client with `HTTPStub` through
`Application.put_env(:symmetry_control, :integration_http_client, ...)`, so they exercise the
provider → `Support.http()` seam, not this module; `request/4`'s name/arity/default is
nevertheless part of the contract (the seam a later Req migration needs).

## Public / API shape

Unchanged, and this is a hard acceptance clause:

```elixir
def request(method, url, headers, body \\ nil) when method in [:get, :post, :patch, :put]
# -> {:ok, status, decoded_headers_map, decoded_body}
#  | {:error, {:transport, reason}}          # socket failure, reason as :httpc returned it
#  | {:error, {:transport, Exception.message(error)}}   # exception raised by :httpc.request/4 itself
#  | raises                                   # un-encodable body / header value (NEW: no longer mapped)
```

New private helpers, in file order:

```elixir
defp encode_request(_method, url, headers, body)   # construction only; may raise
defp perform(method, request, options, body_format) # try/rescue around :httpc.request/4 only
```

The two helpers mirror the call shapes the contract names (`request/4`'s argument list and
`:httpc.request/4`); see "Ambiguities" 1 for the arity reading.

## Change (exact)

1. `encode_request/4` — one clause per body shape with the four `request/4` arguments, so the
   construction path is recognisably the first half of `request/4`. It returns
   `{url_charlist, encoded_headers}` for `nil` and
   `{url_charlist, encoded_headers, ~c"application/json", Jason.encode!(value)}` otherwise.
   It contains exactly the moved expressions (`String.to_charlist/1`, `encode_headers/1`,
   `Jason.encode!/1`); it may raise `Protocol.UndefinedError`, `Jason.EncodeError` and any other
   exception those functions raise, and nothing in this slice catches them.
2. `perform/4` — `try/rescue` around `:httpc.request(method, request, options, body_format: body_format)`
   only, returning the raw `:httpc` result or `{:rescue, error}`. It deliberately does **not**
   return a `{:transport, _}`-wrapped value: `request/4`'s existing `{:error, reason}` clause is
   byte-identical and would double-wrap it as `{:transport, {:transport, message}}`.
3. `request/4` — keeps the name, arity, default `body \\ nil`, the method guard, the `options`
   list (same keys, same order, same literals) and both existing result clauses byte-identical,
   and adds one arm: `{:rescue, error} -> {:error, {:transport, Exception.message(error)}}`.
4. `decode_headers/1` and `decode_body/1` unchanged, byte-for-byte.

## Edge-case semantics the contract demands

- **Un-encodable body value** (`%{pid: self()}` through `Jason.encode!/1`): raises
  `Protocol.UndefinedError` out of `request/4` — *not* `Jason.EncodeError`, because Jason reaches
  the value through the protocol dispatch and the PID has no implementation
  (conductor P1-1). Never `{:error, {:transport, _}}`.
- **Invalid UTF-8 body** (`<<0xFF>>`): raises `Jason.EncodeError`
  (`invalid byte 0xFF in <<255>>`). Never `{:error, {:transport, _}}`.
- **Un-encodable header value** (a PID): raises `Protocol.UndefinedError` from
  `encode_headers/1`'s `to_string/1`. Never `{:error, {:transport, _}}`.
- **Real socket failure**: `{:error, {:transport, reason}}` with `reason` exactly as `:httpc`
  returned it (e.g. `{:failed_connect, [{:to_address, {~c"127.0.0.1", 1}}, {:inet, [:inet], :econnrefused}]}`
  against `http://127.0.0.1:1/x`) — the narrow rescue must still map this, i.e. the rescue is
  narrowed, not deleted.
- **Exception raised by `:httpc.request/4` itself** (for example a malformed option): still
  mapped to `{:error, {:transport, Exception.message(error)}}` by the `{:rescue, error}` arm.
- **Successful responses**: unchanged tuples, unchanged header downcasing, unchanged body decode.

## Behaviour that must not change (byte-identical clauses)

The two `case` clauses, the guard, the default argument, the `options` list and the two decode
helpers are copied verbatim; the only authorised persisted-text difference is downstream (see
below). The construction expressions are moved, not rewritten: the same `String.to_charlist/1`
on the same values, the same `~c"application/json"`, the same `Jason.encode!/1`.

Downstream text change (conductor D1, explicitly authorised): with the raise leaving
`request/4`, the caller's rescue (`integrations.ex:1622-1630` `provider_call/1`, unowned)
classifies `:invalid_provider_response` -> `failure/1` -> "Provider response is invalid" for the
un-encodable-body path, where the pre-slice shape produced `{:transport, message}` ->
"Provider transport failed". Nothing parses the text, the trigger is a programming error
unreachable with the provider modules' own bodies, and the judging artifact records the
before/after observation. Every other message stays byte-identical: `failure/1`'s
`{:transport, _}` clause is untouched and still reachable through the real socket-failure path.

## Evidence plan (exact commands, run on this worktree)

Elixir runs use the portable-toolchain recipe (pattern of the scratch `retro-g2-mix.sh`) with
`MIX_TEST_PARTITION=rf-w3-11` (effective database `symmetry_control_testrf-w3-11`, PostgreSQL on
the local default instance) and `MIX_DEPS_PATH` at the main checkout's identical-lock `deps/`:
`<GDD-STATE>/scratch/rf-w3-11-mix.sh`. All FOREGROUND, one heavy run at a time.

| Gate / artifact | Command | Expected |
| --- | --- | --- |
| format | `bash rf-w3-11-mix.sh format --check-formatted` | exit 0, empty |
| compile | `MIX_ENV=test bash rf-w3-11-mix.sh compile --warnings-as-errors` | exit 0, 0 warnings |
| provider oracle (before) | `ERL_SCHEDULERS=4 SYMMETRY_EXUNIT_EVENTS_FILE=<base> bash rf-w3-11-mix.sh test --seed 0 --formatter ExUnit.CLIFormatter --formatter SymmetryControl.ExUnitJsonFormatter test/symmetry_control/integrations/` on the base bytes | exit 0, `59 passed`; event stream archived |
| provider oracle (after) | same command on the delivered bytes | exit 0, `63 passed` (59 + the 4 new cases); module/name/state sets diff against the base stream = exactly the new `HTTPTest` cases |
| focused unit run | `bash rf-w3-11-mix.sh test test/symmetry_control/integrations/http_test.exs` | exit 0, `4 passed` |
| mutation (judging) | widen the rescue back over the construction path (pre-slice shape) and re-run the focused file | exit 2, the three construction cases red (`{:returned, {:error, {:transport, _}}}` instead of a raise); `perform`/decode cases stay green; reverted, then focused run green again |
| downstream before/after | `MIX_ENV=test bash rf-w3-11-mix.sh run <GDD-STATE>/scratch/rf-w3-11-status-message-probe.exs`, once on the base bytes and once on the delivered bytes | base: `status_message: "Provider transport failed"`; delivered: `"Provider response is invalid"`; `check_connection/1` code `{:error, :provider_failure}` in both; fixture writes rolled back (`projects=0, resources=0`) |
| whitespace | `git diff --check` | exit 0 |
| profile-path scan | `git diff origin/main...HEAD \| grep -i "users/"` | no match (nothing is committed; the scan covers the working tree diff) |
| full `mix test` | conductor's gate-runner only, per the contract | not run here |

The provider oracle runs on a database the probe leaves clean: the probe's fixture writes are
rolled back explicitly, because leftover connected resources change the count
`syncer_test.exs ["periodic synchronization refreshes all connected resources"]` asserts
(`synced: 1`). The first baseline attempt inside this worktree ran with probe residue and
failed exactly that assertion (`synced: 3`) — that is the control that proves the rollback is
load-bearing, not cosmetic.

## Design trade-offs

- **`perform/4` returns `{:rescue, error}` instead of the wrapped tuple.** The alternative
  (mapping inside `perform`) would double-wrap through the byte-identical `{:error, reason}`
  clause (`{:transport, {:transport, message}}`); returning a tagged non-transport value keeps
  the existing clauses untouched and makes the new arm explicit (conductor P2-2).
- **Narrow the rescue, not delete it.** Deleting the rescue would turn a `:httpc`-internal
  exception into a caller-visible crash and would change the `{:error, {:transport, _}}` contract
  for genuine transport exceptions; the contract's failure control names this explicitly.
- **Preserve the message for the un-encodable path?** Rejected: it would require the module to
  catch construction errors again (forking the classification back inside `HTTP`) and undo the
  slice's acceptance criterion ("programming errors no longer classified `transport`"). D1
  authorises the single text change and the artifact pins it before/after.
- **No Req migration.** Out of scope by contract (F9 option (a), "optional/later"); the seam
  (`Support.http()` + `request/4`) stays call-shape compatible for it.
- **A new focused unit test file rather than extending a provider suite.** The provider suites
  stub the HTTP client, so they cannot observe this module's classification at all; the module
  boundary is the only place the raise-vs-transport distinction is observable.

## Ambiguities resolved (options and the chosen reading)

1. **Helper arities.** The contract's Change text names `encode_request/4` and `perform/4`; the
   natural signatures take three arguments each (`url, headers, body` and `method, request,
   options`) because the method plays no part in construction and the body format is a literal
   on the socket call. Options: (a) literal four-argument signatures with one parameter that
   only mirrors `request/4`'s argument list / hoists `body_format: :binary` to the call site;
   (b) three-argument signatures that take only what they use, keeping `perform`'s wrapped call
   textually identical to the contract's. Recommendation and choice: **(b)** — the arity labels
   describe the replaced call shapes (`request/4`'s argument list, `:httpc.request/4`), the
   acceptance clauses bind `request/4`'s exported shape and the moved semantics (not private
   helper arities), and a permanently unused parameter is dead weight in a 60-line module whose
   entire purpose is to stop disguising programming errors. The names, the construction
   boundary and `perform`'s `{:rescue, error}` return value follow the contract literally.
2. **The un-encodable-header case.** The contract names it in clause 1 but pins only the body
   classes in the mutation check (P1-1). Both are asserted: a PID header value raises
   `Protocol.UndefinedError`, and it is covered by the same re-widened-rescue mutation.
3. **`~c"application/json"`.** Kept as the charlist literal exactly as it is today; the contract
   repeats that value, and Elixir 1.20 warns on `'...'` single-quote charlists, so no rewrite.
4. **`@timeout`.** Kept as a module attribute in the same `options` list position; no new
   configurability.

## Out of scope (with reasons)

- The Req migration and any dependency change (`mix.exs` is not in the owned set; F9 option (a)
  is explicitly "optional/later").
- The provider modules' own URL/body shaping (`integrations/providers/*`), `integrations.ex`
  (including the caller's rescue and `failure/1`) and every other `integrations/*` file: not
  owned, and the downstream text change is recorded rather than implemented here.
- Any new abstraction (a transport behaviour, a response struct, a retry policy): no contract
  clause asks for it and the module is a thin `:httpc` adapter.

## Residual risks and known limits

- The downstream `status_message` for the un-encodable path changes text (authorised, recorded,
  unreachable in production with the provider modules' own bodies).
- No production path exercises the un-encodable construction path, so the raise is pinned by the
  new unit test at the module boundary plus the constructed downstream probe; the provider
  suites cannot cover it (they substitute the client module).
- The full `mix test` suite is the conductor's gate-runner, not this stage's; the provider oracle
  plus the focused file are the local evidence.
- The contract file, its index and the ledger live in the GDD state tree and are closed by the
  conductor (precedent: W1-04, W2-06 stayed `Status: active` under `contracts/` after merge).

## Implement-stage record (2026-11-21, this worktree, uncommitted)

Measured on the delivered bytes with the recipe above (`MIX_TEST_PARTITION=rf-w3-11`, database
`symmetry_control_testrf-w3-11`):

| Artifact | Result |
| --- | --- |
| `format --check-formatted` | exit 0, empty output |
| `compile --warnings-as-errors` (test env) | exit 0, 0 warnings, 85 project files on the first build |
| base provider oracle (6 files, seed 0, JSON events) | exit 0, `59 passed`, counts `{passed: 59, failed: 0}` |
| delivered provider oracle (7 files, seed 0, JSON events) | exit 0, `63 passed`; module/name/state diff against the base stream is exactly the 4 new `HTTPTest` cases, all `passed` — every pre-existing test name and state unchanged |
| focused `http_test.exs` | exit 0, `4 passed` |
| mutation: `rescue` widened back over construction (function-level rescue on `request/4`, pre-slice shape) | exit 2, `1/4 passed` — the three construction cases red with `{:returned, {:error, {:transport, _}}}` (PID body: `Jason.Encoder` protocol message; `<<0xFF>>` body: `invalid byte 0xFF in <<255>>`; PID header: `String.Chars` protocol message); the socket-failure case stayed green. Reverted, focused run green again (exit 0) and `format` exit 0 |
| downstream probe, base bytes | `HTTP.request/4` returns `{:error, {:transport, "protocol Jason.Encoder not implemented for PID ..."}}`; `check_connection/1` -> `{:error, :provider_failure}`; persisted `status: "degraded"`, `status_message: "Provider transport failed"` |
| downstream probe, delivered bytes | `HTTP.request/4` raises `Protocol.UndefinedError`; `check_connection/1` -> `{:error, :provider_failure}`; persisted `status: "degraded"`, `status_message: "Provider response is invalid"` |
| probe residue control | `committed residue after rollback: projects=0, resources=0` on both runs |

Two measurements corrected or added against the plan text above:

1. The new test file holds **4** cases (three construction classes plus the socket failure), so
   the delivered directory run is `63 passed`, not `66`.
2. The `{:rescue, error}` arm has no input reachable through `request/4` in this environment:
   ten probe inputs (empty, relative, scheme-only, space/control-character and unbalanced-IPv6
   URLs, newline/space/colon/over-long header names, newline header values) all returned
   `{:error, {:transport, _}}` tuples from `:httpc` instead of raising. The arm is therefore
   carried over as a narrow parity guard for exceptions raised by `:httpc.request/4` itself and
   is not covered by a test; the mutation check exercises its removal, not its firing.
