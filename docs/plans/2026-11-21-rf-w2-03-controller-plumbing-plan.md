# rf-w2-03 controller plumbing plan — one auth plug, one receipt, one envelope, one param family, one fence, one surface

Contract: `goal-rf-w2-03-controller-plumbing.md` (GDD, 2026-11-21), including the binding
conductor decision C1–C8. Coordinate baseline: this worktree's HEAD
`27486cbc5410bfa311b1646d1d67fad9abefaba2` (`27486cbc`), which already carries both dispatch
dependencies: #145 (`rf-w1-13-served-doc-gates`) and #147 (`rf-w1-03-web-credo-strict`, the hard
same-file gate — `Protocol.authenticate/2`, `created_or_ok/1`, `claim_in_transaction/3`,
`append_evidence_conflict/3`, `conflict_error/1`, `lease_duration_ms/0` are kept, not undone).
Every file, line and count below was read or measured at that revision.

Shape reference: `27486cbc` itself (#147) — a web-subtree refactor closed by a plan file under
`docs/plans/`, a host-local evidence manifest, and no in-repo goal archive. This slice adds no
in-repo evidence file either; the load-bearing numbers live in the run's final report and the
host-local scratch dir.

## Owned set and test partition (C5, C6)

Lib edits only in `control/lib/symmetry_control_web/{plugs/auth.ex, router.ex, query_params.ex,
protocol.ex}` and
`control/lib/symmetry_control_web/controllers/{goal_controller.ex, chat_controller.ex,
portal_api_controller.ex, task_controller.ex, daemon_controller.ex}`.

Test files this slice edits (all owned by W2-03, none by W2-04):

| file | why |
| --- | --- |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | C2 pins, CW-10 assertions, CW-6 after/limit table |
| `control/test/symmetry_control_web/controllers/protocol_controller_test.exs` | CW-6/CW-7 fence (body/query) and work-epoch tables |
| `control/test/symmetry_control_web/controllers/operator_read_controller_test.exs` | CW-6 `Protocol.history_limit/1` table through the task-history routes |
| `control/test/symmetry_control_web/controllers/chat_controller_test.exs` | the route-level CW-4 pin for a refused chat `start_work` changeset (fix round r1) |

Read-only oracles (run, never edited here; the four portal/attention files stay with W2-04):
`portal_controller_test.exs`, `portal_resource_identity_test.exs`, `goal_machine_controller_test.exs`,
`strict_evidence_json_test.exs`, `portal_resources_test.exs`, `portal_workflow_test.exs`,
`portal_integrations_test.exs`, `portal_goal_attention_test.exs`,
`router_invalidation_coverage_test.exs`.

No edits under `control/test/support/**` (W1-12 owns `test_fixtures.ex`). The only file this slice
adds is this plan; nothing under `control/test/symmetry_control_web/controllers/` moves to W2-04.

## The five consolidations, one per coordinate

Baseline duplicate count (jscpd, `--format elixir --min-lines 5 --min-tokens 50`, over
`control/lib`): re-measured at `27486cbc` for this slice (C8) — the finding's per-file figures are
stale after #147 and the numbers shipped are the re-baselined ones.

### CW-2 — one `CredentialAuth` plug for three pipelines

`plugs/auth.ex` holds `EnrollmentAuth`, `MachineAuth`, `OperatorAuth`: three modules with the same
shape (`init/1`, `Protocol.authenticate/2`, `credential_class/1` dispatch, 401 on `:unknown`, 403 on
a known-but-wrong class), differing only in the accepted class and the assign. They collapse into
one module:

```elixir
plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :enrollment # pipeline :enrollment
plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :machine    # pipeline :machine
plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :operator   # pipeline :operator
```

`classify/3` matches `{accepts, Protocol.credential_class(token)}`: the accepted class assigns
(`:enrollment_token`, `:machine`) or passes through, `:unknown` stays 401, any other known class
stays 403. `Protocol.credential_class/1` (precedence: enrollment → operator → machine) and
`Protocol.authenticate/2` are untouched; the three module names disappear. Wire effect: none —
401/403 bodies and statuses are produced by the same two `Protocol.error/2` calls.

### CW-3 — one `action_receipt/3` in the portal API

Five sites in `portal_api_controller.ex` repeat the same block (four work-item actions plus
`create_work_item`, which builds the same envelope with the `:created` literal variant);
`connection_receipt/4` is a sixth, status-parameterised copy of the same envelope. One builder:

```elixir
# {durable disposition, success status of a fresh `:created` receipt}
defp action_receipt(conn, {disposition, created_status}, fields) do
  status = if disposition == :created, do: created_status, else: :ok
  conn |> put_status(status) |> json(Map.put(fields, :disposition, Atom.to_string(disposition)))
end
```

Call sites pass their own members (`%{work_item: ...}`, `%{command: ..., execution: ...}`,
`%{connection: ...}`) and the one irreducible difference between a create and an asynchronous
action as an explicit pair: `{:created, :accepted}` (202) for run/cancel/retry/provide_input,
`{:created, :created}` (201) for `create_work_item` and `create_connection`, and
`{:replayed, _}` / `{:updated, _}` / `{:checked, _}` for the 200 sites. The served statuses and
`disposition` labels are unchanged; `connection_receipt/4` is deleted.

Decision recorded (contract-preserving ambiguity): status is not a function of `(disposition,
members)` alone — a fresh create is 201 and a fresh action is 202 — so the builder takes the
created-status from the call site. The alternative (two named entry points `action_receipt/3` +
`create_receipt/3` over a shared `receipt/4`) was rejected because clause 1 asks for one receipt
builder and one status policy; the pair keeps exactly one definition and one expression of the
policy.

Recorded, not a behaviour change (review STD-A03): the replayed `create_work_item` receipt now takes
a plain 200 from `action_receipt/3` where the baseline's `json(conn, …)` kept whatever status the
conn already carried. It was the baseline's only disposition receipt that inherited the conn's
status (the work-item actions computed `put_status(if disposition == :created …)` at the call site,
the connection receipts went through `connection_receipt/4`), the conn's default is 200, and no
mounted route pre-sets a status on that path — so every reachable response is unchanged. The
narrowing is kept rather than re-opening the builder to a `nil` status for a caller no route
produces.

### CW-4 — one validation-error envelope

`chat_controller.ex` and `portal_api_controller.ex` each carry the same 422
`%{error: %{code: "validation_failed", message: "request contains invalid fields", fields:
PortalJSON.changeset_errors/1}}` block. It moves to one public function,
`Protocol.validation_error/2`, which both controllers call from a two-clause `error/2`
(`%Ecto.Changeset{}` → `validation_error/2`, anything else → `Protocol.error/2`). Both copies are
reachable, so both must serve identical bytes: the portal copy through
`Workspaces.create_resource/2` / `delete_resource/2` (pinned by `portal_controller_test.exs:341`
and `portal_resource_identity_test.exs`), and the chat copy through `Chat`'s `start_work` path,
which rolls back `Workspaces.launch_work_item/3`'s `{:error, Ecto.Changeset.t()}` (spec at
`workspaces.ex:302-311`, `Repo.rollback(reason)` from `Repo.update(WorkItem.execution_changeset(…))`
at `:612-627`). An earlier draft of this plan called the chat copy unreachable; the independent
second opinion measured the rollback chain and disproved it, and the shared clause is byte-identical
to both deleted copies. `PortalJSON` is only called, never moved (C7). The chat copy is reachable
from the HTTP surface (a `start_work` message whose `work.title` trims to empty), and since fix
round r1 each controller's *delegation* to the shared function is pinned at the route level: the
portal by the two cases above, the chat by `chat_controller_test.exs` → "a refused chat work
changeset serves the validation envelope". Before that pin, replacing the chat clause with
`Protocol.error/2` (400 `invalid_request`) left every gate green while the helper-level test name
claimed the wider coverage.

The envelope is deliberately **not** a `Protocol.error/2` clause (the first draft made it one, and
the gpt-6.2-sol audit rejected that draft): `ProviderAccess.record_intent/1` rolls back a
non-`resource_id` `ProviderActionIntent.accept_changeset` error verbatim
(`provider_access.ex:1092-1097`), `ProviderActionController.create/2` passes that reason to
`Protocol.error/2` (`provider_action_controller.ex:13`), and at the baseline that call answers
`400 invalid_request`. A changeset clause inside `error/2` would have turned that public provider
path into `422 validation_failed` with `fields` — an error-envelope change the contract's failure
control forbids. With the named function, every existing `Protocol.error/2` caller keeps its exact
bytes and only the two controllers that owned the envelope map changesets to 422.

### CW-5 — `QueryParams.body_params/1` + `QueryParams.idempotency_key/1`

`body_params/1` is the same two-clause helper in `goal_controller.ex`, `task_controller.ex`,
`daemon_controller.ex`; `idempotency_key/1` (exactly one non-empty `idempotency-key` header) is the
same helper in `task_controller.ex` and `daemon_controller.ex`. Both move into `query_params.ex`,
which already owns this kind of wire helper and is imported by `goal_controller.ex` and
`portal_api_controller.ex`; `daemon_controller.ex` and `task_controller.ex` gain the import. The
`Plug.Conn` import in `query_params.ex` is new (`get_req_header/2`).

### CW-6 — one integer family with explicit bounds per call site

Seven parsers exist today (`Protocol.history_limit/1` 1..500 with a 100 default,
`daemon_controller` `positive_integer/2` body numbers, `query_positive_integer/2` query text,
`integer/1` either, `portal_api_controller.version_param/1` a params map, `goal_controller`
`nonnegative_integer/1` 0..bigint max, `query_params.optional_positive_integer/1`). The measured
baseline (probe run in the scratch tree, recorded below) shows three carriers and per-site bounds;
both stay visible at the call site:

* carrier — a JSON body carries a JSON number (`json_integer/2`), a query string carries decimal
  text (`query_integer/2` accepts the `"+1"` sign, as every baseline query parser did, and refuses
  the `" 1"`/`"1.5"`/`""` forms), and the two
  sites that already accepted either form keep `wire_integer/2`;
* bounds — `{min, max}` with `:infinity` for the sites that never had an upper bound today.

```elixir
QueryParams.json_integer(value, {1, :infinity})            # body fence epoch/generation
QueryParams.query_integer(value, {1, :infinity})           # query fence epoch/generation
QueryParams.query_integer(value, {1, @max_history_limit})  # history limit (Protocol)
QueryParams.wire_integer(value, {1, :infinity})            # work epoch, version param
QueryParams.optional_wire_integer(value, {0, max_integer()})  # goal event `after`
QueryParams.optional_wire_integer(value, {1, :infinity})   # event/attention `limit`
```

`optional_wire_integer/2` is the nil-passthrough member; `max_integer/0` publishes the bigint
maximum the durable goal columns accept (`@max_bigint` moves out of `goal_controller.ex`). Measured
accept sets, pinned green at the baseline and unchanged after (the C4 tables below plus the
existing portal oracle suites that drive the version parameter), stated per carrier: the text
carriers — `query_integer/2`, `optional_wire_integer/2`, and `wire_integer/2` where its value comes
from the query string — accept `"+1"` (`Integer.parse/1` keeps the sign) and refuse `" 1"`,
`"1.5"`, `""`; the JSON-body carrier (`json_integer/2`) refuses the string form, so a body fence
accepts a JSON number only; `"0"`/`"-1"` are refused wherever the site is positive-only; the bigint
maximum is accepted by the unbounded sites and refused by `after` only at max+1; the history limit
refuses 501 and up; and a JSON number reaches the version parameter exactly as its text does
(`conn.params` at `portal_api_controller.ex:520`), while the work epoch is read from the query
string alone (`daemon_controller.ex:92` `query_value/1`), so no body spelling reaches it.
`history_limit/1` keeps its own default clause and its 1..500 bound.

### CW-7 — one fence spec

`daemon_controller.ex` writes the same five-step fence rule twice (`fence/1` for bodies,
`query_fence_values/1` for queries) plus two key-list variants (`@fence_fields`,
`MapSet.new(@fence_fields)`). One ordered specification drives both carriers:

```elixir
@fence_spec [
  {"runtime_id", :string}, {"runtime_epoch", :integer}, {"generation", :integer},
  {"claim_id", :string}, {"lease_token", :string}
]
```

`fence(params, :body | :query)` reduces the spec in order, using `json_integer/2` for the body
carrier and `query_integer/2` for the query carrier; `fenced_body/3` keeps its three post-conditions in the same
order (fence → no `machine_id` → `run_id` mode) and still returns the attributes with the five
fence keys dropped; `query_fence/1` keeps its two post-conditions (no `machine_id` → only fence
keys) so a query fence still accepts nothing else. `positive_integer/2` and
`query_positive_integer/2` disappear; `integer/1` (work epoch) is served by `QueryParams.wire_integer/2`.

Decision recorded — the query fence's redundant `machine_id` branch is preserved, not fixed. With
the five-key subset check in place, the explicit `Map.has_key?(query, "machine_id")` clause cannot
refuse anything the subset check would not refuse; its failure path is a `with`-clause error (the
regression probe measures it: `GET /api/v1/runs/:run_id/context?...&machine_id=<uuid>` raises
`WithClauseError`, i.e. an unhandled 500, where an unknown key answers 400 `invalid_request`). The
options were (a) delete the redundant clause so the subset check answers the standard 400, or
(b) keep the branch shape and preserve the crash. (b) is what this slice ships: the contract's
failure control forbids error-envelope differences and the acceptance sentence fixes fence
semantics, so a served 400 where none exists today is a wire change outside this slice's authority.
The branch keeps its pre-merge shape with a comment naming this record; the defect and its one-line
fix (delete the clause, so the subset check answers the standard 400) belong to their own increment.
The repo's `docs/TODO.md` tracker is outside this slice's owned set (its entry is "docs/TODO.md only
if a TODO entry is closed", and this slice closes none), so the record ships here and in the run's
final report instead of in the tracker. The accept/reject decision is identical either way: a query
carrying `machine_id` was never accepted.

### CW-10 — explicit `:surface` for Goal authority and projection

`goal_controller.ex` derives both the durable actor (`"portal_operator"` vs `"operator"`) and
whether the portal projection is served from `String.starts_with?(conn.request_path, "/portal/")` —
a second face for the same operation has to keep the path shape in sync. The surface is assigned
once per pipeline instead:

```elixir
defp put_surface(conn, options), do: assign(conn, :surface, Keyword.fetch!(options, :surface))
# pipeline :operator   -> plug :put_surface, surface: :operator
# pipeline :portal_api -> plug :put_surface, surface: :portal
```

`actor_ref/1` and `portal_goal/2` pattern-match `conn.assigns.surface` in two clauses
(`:portal` / `:operator`) and fail loudly (500) if a Goal route is ever mounted without one, rather
than silently falling back to the operator identity. The router-local function plug follows the
existing `put_portal_no_store/2` precedent; the `:surface` vocabulary lives in the router that
decides it. Served actor strings and projections are unchanged for every spelling the router
matches to the same pipeline (C2 pins them), with one deliberate exception recorded below, and no
controller outside `goal_controller.ex` reads the assign.

Decision recorded — one deliberate, authority-positive delta outside the suites. The router matches
*decoded* path segments (`Phoenix.Router.call/2` maps `path_info` through `URI.decode/1`,
`phoenix/router.ex:469-470`) while `conn.request_path` keeps the raw text
(`Plug.Conn.Adapter.conn/4` sets `path_info`/`request_path` from the same raw path,
`plug/conn/adapter.ex:45-49`). So `GET /%70ortal/api/goals/:id` routes to the portal pipeline in
both revisions, but the baseline's `String.starts_with?(conn.request_path, "/portal/")` was false
there and it served the *operator* projection and recorded the *operator* actor on a portal route;
the pipeline assign serves the portal projection and `"portal_operator"`. The gpt-6.2-sol audit
measured the divergence by reading both dependencies. It is accepted, named and pinned rather than
reverted: the contract's byte-identity object is the controller suites **as they stood before this
increment** (those pre-increment tests never sent an encoded path, so the differential oracle is
empty over them; the encoded-path case added here is one of the two CW-10 records the capture does
show, below), failure control names 401/403 and error envelopes (neither changes), and CW-10's
mandate is exactly that authority follow the matched pipeline instead of the path text. The delta
removes a wrong-authority attribution; the new
assertion `goal_controller_test.exs` → "an encoded portal path keeps the portal surface the router
matched" pins it, and it is red on the baseline (the path check fails there).

## Baseline and evidence plan

* Differential oracle (C3): a scratch copy of `control/` (`<HOST-PATH>/refactor/scratch/w2-03/tree`,
  deps and `_build` copied from a sibling worktree at the same `mix.lock`) carries one throwaway,
  uncommitted recorder — a plug inserted before the router plug, `register_before_send/2`, and a
  `ConnCase` setup line naming the running test. Every answered request appends
  `test · method · path · status · content-type · body skeleton · normalised body`. The normalisation
  masks only per-run randomness whose value cannot agree between two runs of identical code: UUIDs
  (in paths and bodies), ISO-8601 timestamps, ≥32-char base64/hex tokens, digit runs inside string
  leaves and paths (DB sequences and `System.unique_integer/1` fixture names), identifier-shaped
  short strings carrying a digit (project and work-item keys cut from UUID slices), integer leaves
  ≥ 1e6, and the one wall-clock member (`lease_remaining_ms`); member names, member paths, array
  lengths, statuses, content types, JSON numbers and all other string content are compared as
  served. The
  masks are applied by the recorder itself, identically to every capture.
* Capture protocol (as executed): the scratch tree at partition `rf-w2-03d`, always
  `--seed 1234 --exclude portal_assets` over the contract's six files plus the C5 oracle extension
  (14 files). Each load-bearing capture binds to a named revision: every `base*` run served the
  `65869a78` baseline lib, and the two head runs served the twelve blobs the *reviewer verdict
  condition 2* row below pins as unchanged at the head captures — the apply-stage and fix-round r1
  rows list the blobs that changed afterwards, and in both rows the lib changes are comment/doc
  text only (no code path, no served byte). The captures are `base7`/`base8` (baseline lib,
  pre-review tests; 225/226 passed — the one failure is the CW-4 head-only pin),
  `base10`/`base11`/`base12` (baseline lib, final tests; 225/227 — the two head-only pins red),
  `head3`/`head4` (the head blobs at capture time; 227 passed each), and eight `iso-base-*` runs of
  `portal_workflow_test.exs` alone. Earlier
  captures (`base1`–`base6`, `base9`, `head2`) were superseded by a mask-version or a test-set
  change and are kept as history only. Noise floor `base10`↔`base11`: 44 unmatched records over 37
  tests; `head3`↔`head4` (identical code): 40/39 over 35 tests. The
  instrument's first version (no path masking, 6+-digit strings) produced a 2 108-line
  base↔base diff — proof that a same-code rerun is not byte-stable without the masks above. The
  second version's base↔base diff is the *noise floor*: it is the irreducible churn of the
  provider-action suite (async broker timers and stub state across tests), measured, not assumed.
  The load-bearing comparison is therefore made over the **stable corpus** — the 914 keys (1 213
  records) whose value multiset is identical in both base runs — and the result is: exactly four
  differing records there, each named: two are the CW-10 encoded-path delta below, one is a
  timeline ordering that eight isolated baseline runs flip on (three of them serve the other
  order — `Orchestration.timeline_precedes?/2` breaks `inserted_at` ties by random UUID), and one
  is a per-run provider capability token that also differs between the two identical-code head
  runs. The full-corpus base↔head diff (45 vs 43 unmatched over the same 37 churn tests plus the
  encoded-path test) introduces no difference class the floor does not already contain.
  The claim that carries is therefore a normalised one, and the masks above are its boundary: over
  the stable corpus the served bytes agree except for the two CW-10 encoded-path records, which are
  named and pinned; the remaining two records are noise classes the identical-code runs produce on
  their own. Nothing here claims literal byte equality of raw captures — identical code does not
  reproduce them without the masks, measured above.
* C2 pins, green at the baseline: durable `goal_revisions`/`goal_events` `actor_ref` for the portal
  and operator create and command routes; all 13 Goal routes on their own pipeline resolving a
  surface without a 500; the operator read never carrying the portal projection members.
* C4 tables (green at baseline, unchanged after): `after`/`limit` on goal events, attention
  `limit` (operator), task-history `limit` through `Protocol.history_limit/1`, the daemon body fence
  and query fence integer fields, and the work-epoch query parameter.
* Gates, all foreground: `mix format --check-formatted`, `mix compile --warnings-as-errors`, the
  contract's six targeted files plus the C5 oracle extension, `git diff --check`. The full `mix test`
  is the conductor's gate-runner only: the contract forbids two full suites on this host, and the
  scratch captures are serialized against the worktree runs.
* Duplicate count for the report: jscpd at the same flags over `control/lib`, base vs head, plus the
  same-tree drop in the five owned lib files; Credo stays clean in the touched files (no new
  `credo:disable`).
* CW-10 and the whole slice get an independent read-only review (authority-adjacent) before the
  report; the reviewer sees the diff, not the plan's conclusions.

## Independent reviews and their reconciliation

Three read-only reviews ran in parallel against the change set (no edits, no builds, no test runs by
the reviewers): `reviewer` (adversarial audit, frozen at `git diff` b2sum
`5c4aba437adf1063a796d62aece62ac20a25fd919b8575d5b600fd4623252d00e6e5f5cd80c78c06fe81b890ddafec24ff8019f518b2c7f654e04f5383b3dc96`),
`consultant` (second opinion on the two recorded decisions and CW-10), and a `gpt-6.2-sol` pass at
`xhigh` effort (`codex exec --sandbox read-only`). Their findings and the resolution of each:

| finding | resolution |
| --- | --- |
| gpt-6.2-sol #2 / reviewer P3b — a changeset clause inside `Protocol.error/2` turns the public provider-actions path (400 `invalid_request` today) into 422 `validation_failed` | **fixed before the freeze**: the envelope moved to `Protocol.validation_error/2`, called only by the two controllers that owned it; the reviewer verified the reachable rollback chain |
| gpt-6.2-sol #1 / reviewer P2 — percent-encoded portal spellings now get the portal projection and actor where the baseline served the operator's | **accepted, documented and pinned** (CW-10 below); the plan sentence that claimed "unchanged" for all spellings was corrected |
| reviewer P3a — an intermediate state had the portal `error/2` catch-all before the changeset clause (a live 422→400 regression) | **fixed before the freeze** (captured by `compile --warnings-as-errors`); the reviewer re-verified the clause order in the frozen snapshot |
| gpt-6.2-sol #3 — the duplicate-`after` assertion only proved a 200 | **fixed**: it now asserts the page equals the `after=1` page and refutes the `after=0` page |
| gpt-6.2-sol #4 / consultant F2 — the reviewers' diff snapshot was stale (it cited an obsolete `docs/TODO.md` comment) | **fixed**: the live code points at this plan, the snapshot was regenerated at the freeze, and both later reviewers pinned blob hashes instead |
| consultant F1 — the plan called the chat changeset copy unreachable | **fixed**: the plan now carries the measured rollback chain (`workspaces.ex:302-311`, `:612-627`) |
| consultant F3 — `chat_controller.ex` keeps its own inline 201/200 status expression | **recorded, not changed**: it is a create status, not a receipt envelope, and CW-3 names the portal sites; the chat envelope's disposition member is served unchanged |
| consultant F4 — `optional_wire_integer/2` is a union carrier | **recorded**: all four present sites (`goal_controller.ex:114`, `:115`, `:128`, `portal_api_controller.ex:72`) are query-only; a future optional *body* site must add the matching carrier wrapper instead of reusing the union |
| consultant F5 / reviewer P3 — `Keyword.fetch!(options, …)` and the fail-loud clauses raise generic exceptions | **accepted**: all mounts pass their options (proven by the 13-route test) and a missing surface is a programming error; a named raise was rejected as error handling for an unreachable scenario |
| reviewer verdict condition 2 — gates must run at a frozen revision | **met**: the 12 code/test blob hashes in the audit (chat `120654a3`, daemon `151517dc`, goal `b81f05e6`, portal_api `7f87d198`, task `af06ef97`, auth `789e2174`, protocol `3283e6c9`, query_params `9f500a3f`, router `a43257d4`, goal test `31776283`, operator_read test `2f439d03`, protocol test `0f132ed2`) are unchanged at the gate runs and at the head captures |
| apply stage (simplify F1–F4, 2026-11-21) — fence comment reword, `idempotency_key/1` docstring reword, one `goal_command/5` test envelope (`post_goal_command/4` and `amend_goal/4` stay as wrappers), one `machine_id` query-fence pin | **appended, review result unchanged**: four of the twelve audited blobs re-hash (daemon `1dd0d6a1`, query_params `07f9af15`, goal test `70dcab76`, protocol test `e361c004`); the two lib blobs differ in comment/doc text only (no code path, no served byte), the other eight are byte-identical to the row above. The new pin freezes the disclosed CW-7 `WithClauseError` until the one-line fix increment (it is that increment's red→green anchor). Gates at this head: format, compile `--warnings-as-errors`, targeted 136 passed, oracle extension 91 passed/1 skipped/4 excluded, `git diff --check` — all exit 0. Credo default set + `--strict` with the parse timeout raised (the bare 5 s default silently drops files on this host: three bare runs gave 197/313/460 over 33/41/42 files) → 460 project-wide over 42 files, 0 in the four edited files, 0 `DuplicatedCode` |
| fix round r1 (review findings, 2026-11-21) — route-level chat CW-4 pin (`chat_controller_test.exs`), goal-test rename and `after` comment, `action_receipt/3` comment, plan corrections (STD-A01, A02, A04, A05, A06; STD-A03 recorded under CW-3) | **appended**: two of the twelve audited blobs re-hash — portal_api `52dc8ca1` (comment text only: the `git diff` is one hunk with no code line) and goal test `3683634f` (one test renamed, one comment) — and the remaining ten are byte-identical to the apply row. The chat test file, outside the audited twelve and byte-identical to the baseline until now, is `277b63b9`. Gates at this head, all exit 0: `mix format --check-formatted`, `mix compile --warnings-as-errors`, `git diff --check` over `65869a78..HEAD` and `origin/main...HEAD`, targeted **137 passed** (the +1 over the apply row's 136 is the added route-level chat case), oracle extension **91 passed/1 skipped/4 excluded**. Two host prerequisites for those counts: the oracle brace list must spell `goal_machine_controller` (`goal_machine` alone expands to a path that does not exist, and mix then silently runs the other seven files — 81 passed), and `POSTGRES_PORT` must be unset because this host's exported 55442 has no server while the local PostgreSQL listens on 5432; `SYMMETRY_CONTRACTS_DIR=<repo>/contracts` is required. Flip check on the new pin: replacing `chat_controller.ex`'s changeset clause with `Protocol.error/2` answers 400 `invalid_request` and turns that case red. This round changes no served byte (the only lib edit is comment text), and the differential instrument's scratch tree no longer exists on this host, so the capture rows above were not re-run and still bind to the blobs they name. |
| reviewer verdict condition 3 — the plan file is untracked | **reported**: the commit must include `docs/plans/2026-11-21-rf-w2-03-controller-plumbing-plan.md` (this stage does not commit) |

## Out of scope, with reasons

* CW-9 (unmapped-reason coverage and the fallback counter) is W5-05; CW-8 (`portal_json.ex`) is
  W2-04; test-only seam removal is W5-06.
* `control/test/support/**` stays untouched; the differential recorder lives in the scratch copy,
  never in this tree.
* No `docs/design/*.md` edit: the router/pipeline structure, the 401/403 mapping and the actor
  strings those documents describe are unchanged; the owned set does not include design docs.
* No `docs/TODO.md` move: no entry names this slice or the CW-2..CW-10 items (checked at
  `27486cbc`).
* The frontend bundle (`priv/static/portal/`) is not built here; the four `:portal_assets` cases in
  `portal_controller_test.exs` are excluded with the repo's documented
  `mix test --exclude portal_assets` (no Node/pnpm staging on this host) and that exclusion is
  reported as a limit, not as a pass.

## Residual risks

* CW-3's status pair makes the create/action distinction explicit at each call site; a wrong pair
  changes a wire status, which the differential oracle and the existing portal suites both pin.
* CW-6/CW-7 change parsing internals that only HTTP-level tests can see (the parsers are
  `QueryParams` functions, not a controller-facing surface); the C4 tables plus the differential
  capture are the acceptance evidence, and the unbounded sites keep `:infinity` so no accepted
  input narrows.
* CW-10 is authority-adjacent: a missing `:surface` assign now fails loudly instead of guessing;
  create, show and command pin the surface on both pipelines, and the 13-route test proves no Goal
  route 500s on its pipeline (`/events`, `/graph`, `/contexts/:id` and `/attention` never read
  `assigns.surface`, so they cannot pin it).
* The scratch noise floor is proven by the base↔base capture; if that diff is not empty, the
  instrument is reported as unstable instead of claiming byte-identity from a noisy diff.
