# rf-k4 schema-stripped map — plan

Contract: `goal-rf-k4-schema-stripped-map.md` (GDD, dispatched 2026-11-22; owner ruling L1=b; the
contract's `## Conductor decision` sections bind this stage). Baseline: this worktree's HEAD
`b6c7c38397f41d99a0a1e4956e9fb8aaa8977a98` (= `origin/main`, #243). Every coordinate below is
re-derived at that revision; the contract's own coordinates were taken at `333c572c` and were
re-checked, not copied.

Docs-plus-tests slice: schema bytes, fixture/manifest bytes, generated artifacts, wire bytes and
persisted bytes do not change. No production code edit.

## What the base tree holds (re-derived)

| fact | coordinate at `b6c7c383` |
| --- | --- |
| fence definition (five members, one order) | `daemon/internal/protocol/protocol.go:348-354` (`type Fence struct`), members `runtime_id`, `runtime_epoch`, `generation`, `claim_id`, `lease_token` |
| Control's one fence rule | `control/lib/symmetry_control_web/controllers/daemon_controller.ex:482-489` (`@fence_spec`, `@fence_keys`), `:491-500` (`fenced_body/3` → `body \|> Map.drop(@fence_keys)`) |
| fenced endpoints in that controller | `def` at `:302` (attach) / `:328` (session stopped) / `:341` (evidence) / `:421` (usage); the matching `fenced_body/3` calls at `:306` / `:332` / `:346` / `:425` |
| stripped batch admission | `control/lib/symmetry_control/goals/run_fence.ex:719-724` (`only_known_keys?(batch, ["schema_version", "run_id", "items"])`), `:293-321` (`append_evidence_batch/5`) |
| Stripped-map validators | `control/lib/symmetry_control/goals/contract_validation.ex:98` (`validate_evidence/2`), `:101-113` (`validate_evidence_batch/2`), `:127` (`validate_usage/2`); schema failure shape `{:error, {:validation_failed, errors}}` at `:454-466` |
| Go request producers | `daemon/internal/control/client.go:757-780` (`AppendEvidence`, embeds `protocol.Fence` + `protocol.Evidence`), `:785-819` (`AppendEvidenceBatch`, embeds `GoalEvidenceBatch`), `:843-867` (`RecordUsage`, embeds `protocol.Fence` + `protocol.Usage`) |
| Go request path applies no schema | `client.go:1035-1044` (`requestGoalContractWithStatus` → `contracts.DecodeSchema` on the **response**), `:1231` (conflict details); the request body is plain `json.Marshal` in `transport.perform` |
| schemas | `contracts/v1/evidence.schema.json` (4 top-level `oneOf` branches, 13 properties each, each `additionalProperties: false`, plus one nested 2-branch `oneOf` under the observation branch's `validator_profile`), `contracts/v1/evidence-batch.schema.json:6`, `contracts/v1/usage.schema.json:6` |
| manifest | `contracts/fixtures/manifest.json` — `manifest_version` 1, **248** rows (`jq '.fixtures \| length'`) |
| valid request fixtures (fence-free) | `valid/evidence.artifact.json` 13 members, `valid/evidence-batch.basic.json` 3, `valid/usage.nullable.json` 14 |
| existing pins | `daemon/internal/control/goal_client_test.go:105-200` (per-endpoint fence spot fields), `:244-272` (batch spot fields); `control/test/symmetry_control_web/controllers/goal_machine_controller_test.exs` (controller-test coordinates at HEAD): `:448` created → 201, `:477` replayed → 200, `:661` refused → 400 `invalid_request`, `:672` created → 201 (four batch posts of `Map.merge(fence, batch)`; at `b6c7c383` the last two are `:621`/`:632`), `:421` fence-merged usage → 201; `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs:41` and `contract_validation_test.exs` validate fence-free fixtures |
| nothing pins the relationship | no test asserts the wire member set, that Control's tolerated top-level extras are exactly the five fence members, or that the fenced body fails the schema by design |

Wire member sets this slice pins (measured in K4's capture, re-derived here from the three call
sites and the three schemas):

| endpoint | wire top-level members | fence | document |
| --- | --- | --- | --- |
| `POST /api/v1/runs/:run_id/evidence` (single) | 18 | 5 | `evidence.schema.json` 13 |
| `POST /api/v1/runs/:run_id/evidence` (batch) | 8 | 5 | `evidence-batch.schema.json` 3 |
| `POST /api/v1/runs/:run_id/usage` | 19 | 5 | `usage.schema.json` 14 |

## Change 1 — `contracts/README.md`, one added paragraph

Inserted as one new paragraph at `contracts/README.md:55-67`, after the `ContextSnapshot.content_hash`
paragraph (`:51-53`). It qualifies the file's opening statement that `v1/*.schema.json` is "the
canonical JSON Schema Draft 7 source for the new control envelopes and operator requests" (`:3-6`).
Text added (one blank-line-separated paragraph, no other edit in the file):

> `evidence.schema.json`, `evidence-batch.schema.json` and `usage.schema.json`
> describe the fence-stripped machine request document, not the HTTP body a daemon
> sends. The daemon flattens the five fence members — `runtime_id`,
> `runtime_epoch`, `generation`, `claim_id` and `lease_token` — beside that
> document at the top level by embedding `protocol.Fence` at the `AppendEvidence`,
> `AppendEvidenceBatch` and `RecordUsage` call sites, and keeps `run_id`
> path-authoritative rather than body-authoritative. Control validates and drops
> exactly those five members in one place, the `DaemonController` fence rule
> (`@fence_spec` / `fenced_body/3`), before `ContractValidation` sees the
> document; the fence members and their stripping are specified and tested at that
> controller boundary. A body that still carries the fence is therefore expected
> to fail these schemas' `additionalProperties: false`, and the document validated
> on its own is the same document Control validates after stripping.

## Change 2 — `docs/design/protocol.md`, one qualifying sentence

Appended to the "Contract source and evolution" paragraph, directly after "These are the single wire
authority, separate from Ecto persistence structs." Sentence added:

> For the fenced machine
> requests the daemon sends (`evidence`, `evidence-batch` and `usage`), that
> authority covers the fence-stripped request document: the five-member fence is
> carried beside it, enforced and stripped by the controller boundary before
> validation, and the fence itself remains owned by
> [protocol v1](../protocol-v1.md).

No other protocol text changes; the sentence adds no protocol meaning.

## Change 3 — Elixir schema pin (`contract_validation_test.exs`, one new test)

New test `"the request schemas describe the fence-stripped evidence, batch and usage documents"`
placed beside the existing manifest/fixture tests. One table of
`{fixture filename, &validate_*/2}` over the three valid fixtures; for each: the fence-free document
validates, and `Map.merge(document, fence)` with the five members of `@fence_spec` (string
`runtime_id`/`claim_id`/`lease_token`, integer `runtime_epoch`/`generation` ≥ 1) returns
`{:error, {:validation_failed, …}}`. Assertions:
`:ok == validate.(document, opts)`; `{:error, {:validation_failed, errors}} = validate.(Map.merge(document, fence), opts)`
and, to pin the failure **class** per member rather than one error string, that `errors` carries one
`AdditionalProperties` refusal for each of the five fence members — an error of that class whose path
ends in `/<member>`, recursing through `oneOf` branch errors so the evidence row's refusals are
reached. No fixture bytes change: the fenced document is built in the test.

Failure control for this test: adding the five fence members to any one of the three schemas'
`properties` (or relaxing `additionalProperties`) reddens it; the run is recorded in the report.

## Change 4 — Elixir boundary pin (`goal_machine_controller_test.exs`, one new test)

New test `"the evidence endpoint accepts exactly the fence beside the batch document"`, real claimed
run via the existing `claimed_goal_run_fixture/1` and `evidence_attrs/2` helpers:

1. `Map.merge(fence, batch)` (the real wire shape: five fence members + `schema_version`/`run_id`/`items`)
   → `201` with the created receipt;
2. for each of the five fence keys, the same request with that member dropped → `400 invalid_request`
   (before any admission, so no identity replay is consumed);
3. the same request plus one non-fence top-level member → `400 invalid_request`.

(1) pins that the wire shape is accepted, (2) that the fence is mandatory as a set, (3) that the
tolerated top-level extras are exactly the five fence members — i.e. the map Control validates is the
stripped document. Failure control: with only four of the five keys in `fenced_body/3`'s drop set,
(1) reddens (the surviving member reaches batch admission and is refused); recorded in the report.

## Change 5 — Go producer pin (new `daemon/internal/control/fenced_request_document_test.go`)

One table-driven test `TestGoalMachineRequestBodiesAreFenceBesideTheSchemaDocument` with three rows
(single evidence, evidence batch, usage). Each row calls the real client method against an
`httptest` server that captures the exact request bytes, and then asserts, from those bytes:

- the top-level member set equals the five fence members ∪ the member set of `json.Marshal` of that
  endpoint's document value (so the count is derived, not hard-coded, and the K4 counts above are
  the cross-check; the batch row's value is the reconstruction trade-off 3 describes, not the
  `[]protocol.Evidence` handed to `AppendEvidenceBatch`);
- no top-level `fence` member (the wire is flattened, not nested);
- `contracts.DecodeSchema(envelope, documentBytes, &dto)` accepts the document bytes — the
  reconstructed document value and the captured wire bytes with the five fence members removed;
- `contracts.DecodeSchema(envelope, wireBytes, &dto)` refuses the captured wire bytes with an
  `additionalProperties` error naming the fence members.

Reuses the existing helpers (`goalFence`, `goalEvidence`, `goalUsage`, `mustMachineClient`,
`goalRunID`, `jsonServer` with a request-capturing callback, response-stub shapes from
`goal_client_test.go`). Failure controls, both recorded in the report: removing one fence member
from the embedded struct at the batch call site reddens the member-set assertion; a document value
the schemas refuse (a batch `schema_version` other than the v1 const) reddens the fence-stripped
acceptance at that row — recorded with the client-side `schema_version` guard relaxed, since in
production that guard would reject the value before it reaches the wire.

## Evidence plan (all foreground, one heavy job at a time)

Order (this host: never a measurement harness beside the full suite; the Elixir suite is a single
mix run at a time):

1. `pnpm contracts:check` at base and at HEAD (root; `mise exec pnpm@12.17.0 -- pnpm contracts:check`),
   manifest row count 248 before and after, `git status`/`git diff` showing no schema, fixture,
   generated or bundle byte change.
2. Go: `gofmt -l .`, `go vet ./...`, `GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
   `go test -count=1 ./internal/control/... ./internal/contracts/... ./internal/protocol/...` in
   `daemon/`; then WSL `wsl -d Ubuntu-24.04 -u accept` →
   `cd <worktree>/daemon && go test -race -count=1 ./internal/control/...`
   (this slice's worktree, never the main checkout).
3. Elixir via the portable-toolchain shim (erl.exe + `-elixir_root`, `MIX_ENV=test`,
   `MIX_TEST_PARTITION=rf-k4`, `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset):
   `mix format --check-formatted`, `mix compile --warnings-as-errors`, then
   `mix test test/symmetry_control/goals/contract_validation_test.exs test/symmetry_control_web/controllers/goal_machine_controller_test.exs`.
   `control/deps` is populated from the local checkout by copy (gitignored; not part of the diff).
4. Mutants, each in a scratch copy outside the worktree (never in the delivered tree):
   Elixir drop-set mutant (Change 4), Go embedded-struct mutant (Change 5), schema mutant
   (`additionalProperties` relaxed on one request schema) for Change 3.
5. `git diff --check`; `git status --porcelain -uall` scoped to the owned files; the program's
   profile-path grep over the branch diff empty (no host path carrying a user profile name) and no
   backslash-`u` sequence in any written file.

## Design trade-offs

1. **Assert the failure class, not one error string** (clause 3's failure control). The Elixir tests
   assert the `{:validation_failed, …}` tuple plus, per fence member, an `AdditionalProperties`
   refusal whose error path ends in that member; the Go test asserts the `additionalProperties` class
   plus the named members. Wording drift then cannot silently weaken the pin, and a re-worded error
   keeps the test red only if the class changes.
2. **Derive the Go member set from the marshaled document** instead of hard-coding 13/3/14: a
   hard-coded list would pass while the DTO quietly disagrees with the schema, which is the property
   under test. The K4 counts stay in the plan and the report as the independent cross-check.
3. **Batch document value reconstructed in the test the way `AppendEvidenceBatch` builds it**
   (`schema_version` const, path `run_id`, the items passed in): the client has no exported accessor
   for the document half, and adding one would be a production-code change this slice may not make.
   The member-set assertion pins the wire's member set; the schema acceptance of the fence-stripped
   captured bytes pins the document the client actually sent, so the reconstruction supplies the
   expected member set and the envelope rather than the validation target.
4. **`Map.merge(document, fence)` for the Elixir schema pin, not the controller mutant**: the
   contract asks for the fence-free/`fenced` pair at the validator boundary; the controller boundary
   is pinned separately by Change 4, so the two tests fail for distinguishable reasons.
5. **No edit to `client.go`'s stale `GoalEvidenceBatch` comment**: the conductor routed it to A6
   (one-writer rule, 2026-11-22 conductor decision); this slice records it in the report instead.
6. **No new fixture bytes.** The correct fence-free corpus already exists in the manifest; a
   flattened-fence corpus is clause 4's out-of-scope list.

## Out of scope (with reason)

- Amending the schemas to describe the fenced wire body (declined by owner ruling L1=b) and any
  schema `description` annotation or generated/bundle change (clause 4).
- Request-side schema validation in the Go client (clause 4): no production behaviour change here.
- `contracts/v1/**`, `contracts/fixtures/**`, `contracts/generated/**`, `daemon/internal/contracts/**`
  (byte-identical), `daemon/internal/control/client.go` (A6's file).
- Session/stopped/ack/heartbeat/events/transition bodies (no request schema exists) and the
  `TestEvidenceBatchDecodesNestedEvidenceUnderFence` rename (conflict 2: CI-pinned name, no rename).
- A6's raw-byte strictness corpus and the deferred tightening (owner L2=b).

## Residual risks and limits

- Windows host: no `-race` locally; the WSL leg runs Go 1.27.0 (host 1.27.4) and is the only local
  race evidence for `internal/control`.
- The Elixir gates need the local PostgreSQL; if it is unavailable the suite is reported missing,
  never assumed passed.
- The Go request path still applies no schema to requests; this slice pins the relationship, it does
  not add a validator (clause 4).
- `docs/evidence/**` is not in the owned set: the judging artifact for this stage is the GDD
  `answer.md` (contract clause 2), not a new repository evidence record.

## Environment addendum (implement run, 2026-11-22)

What the plan's evidence steps did not know when it was written, recorded here so the plan matches
what ran:

1. Elixir gates ran against a slice-owned disposable PostgreSQL 18.6 cluster
   (`<scratch>/k4-pg/data`, port 55433, superuser role `postgres` created) because the shared local
   instance on 5432 was rejecting connections while six sibling slices were live.
   The runner is `<scratch>/rf-k4-mix-pg55433.sh` — the program's `retro-k4-mix.sh` with
   `POSTGRES_PORT` kept, so the suite uses `symmetry_control_testrf-k4` on that cluster.
2. `pnpm install` cannot run on this host (npm-registry handshake failures; the pnpm offline mirror
   lacks metadata: `ERR_PNPM_NO_OFFLINE_META`), so the contracts gate needs a `node_modules` mirror.
   It was mirrored from the main checkout's own worktree (identical `pnpm-lock.yaml`) by a
   junction-preserving script: pnpm's symlinks become directory junctions that Node resolves;
   plain `rsync`/`robocopy` copies do not preserve them and leave Node unable to resolve.
3. Mutants ran in this worktree and were reverted with sha256 verification, not in a scratch copy:
   the mutated files are production files at base, and an offline scratch copy of the Elixir tree
   (`deps`, `_build`) is not reproducible here. Each restore was confirmed against the pre-mutant
   hash and `git status`.
