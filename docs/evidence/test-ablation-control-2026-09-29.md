# Control test ablation inventory

Date: 2026-09-29  
Scope: `control/` tests, test support, Mix aliases, and linked validation/harness entrypoints  
Baseline: working tree based on `d14debbf6ae3ded09e5013ef90952d347c01474d`  
Actual served model: `gpt-5.6-sol-fast` (`PI_PROVIDER=ghc`, reasoning level `medium`)

## Constraints and method

This is source inventory and candidate discovery only. No test, production, fixture,
Git/index, database, service, or credential state was changed. The only write in this
wave is this report. Existing dirty state (65 modified and 40 untracked paths when
counted) was preserved.

The inventory was independently derived from `control/test/**/*_test.exs`, then
reconciled against `docs/evidence/test-ablation-manifest-2026-09-29.md`. Test counts
are static ExUnit `test` declaration counts, not run results. Guard summaries come
from every declaration name plus focused reads of boundary/support implementations.
The governing rules are `AGENTS.md`, `control/AGENTS.md`,
`docs/design/quality.md`, and `docs/evidence/project-test-ablation-2026-09-29.md`.

Disposition vocabulary:

- **retain**: distinct assertion, boundary, failure mode, or protected environment;
- **candidate Cn**: source-grounded overlap worth an isolated experiment, not an
  authorization to remove;
- **blocked retain**: required test whose prerequisite is unavailable or whose gate
  is absent; absence is not redundancy.

Every ordinary file can be selected with
`cd control && mix test <path>`. The Mix `test` alias first runs
`ecto.create --quiet` and `ecto.migrate --quiet`, then ExUnit. Thus even pure files
selected through `mix test` inherit a database-creation/migration entrypoint unless
Mix is invoked as `mix test` after alias expansion has already been handled. CI uses
the alias and PostgreSQL.

## Reconciliation and counts

- **57** `*_test.exs` files, exactly matching all 57 Control test paths in the parent
  manifest: no omissions, extras, missing paths, or false-positive test files.
- **802** static ExUnit test declarations.
- **6** non-suffixed Control support/entrypoint files in the manifest:
  `control/mix.exs`, four `control/test/support/*.ex` files, and
  `control/test/test_helper.exs`. All exist and are genuine linked inputs; none is an
  independently executable test suite.
- `control/test/symmetry_control/pi_control_e2e_test.exs` contains two modules: five
  literal-skipped native Pi harness tests and two ordinary
  `DataCaseSandboxOptionsTest` validation tests. Its file count is therefore seven;
  treating all seven as opt-in harness tests would be a manifest interpretation bug.
- Default-source filtering accounts for **20 opt-in/skipped declarations**: 10
  dispatch notification tests, five Pi native tests, four provider broker tests, and
  one real-listener Goal receipt test. Therefore the source predicts at most 782
  non-skipped declarations in an ordinary run (conditional dependency availability
  can add a skip); this is not a run result.
- Contract fixtures are Tooling-owned in the parent manifest, correctly so, but are
  linked inputs to Control validation. There are 184 payload files (63 valid, 121
  invalid) plus `manifest.json`; the manifest has 184 entries. They are fixtures,
  not 185 additional Control tests.
- `.github/workflows/ci.yml` is Tooling-owned and is an execution orchestrator, not a
  Control test. `control/config/{config,test,runtime}.exs` are production/test
  configuration inputs, not test harnesses. The contract scripts are cross-language
  validation entrypoints owned by Tooling, not aliases of the Elixir validators.

Category totals are intentionally not summed because files may have multiple
categories. "Unit" below includes focused context tests using the SQL sandbox;
"verification" marks durable SQL/cross-component behavior; "harness" marks a real
subprocess/listener/daemon boundary.

## Exhaustive file ledger

| Test file | Count | Categories | Guarded behavior / boundary | Entrypoint and environment | Disposition |
|---|---:|---|---|---|---|
| `symmetry_control/chat_test.exs` | 15 | unit, verification | Atomic conversation/work creation, replay, guidance/decision fencing, durable evidence, pagination | ordinary focused Mix test; PostgreSQL | retain |
| `symmetry_control/config/runtime_test.exs` | 22 | unit, validation | Runtime env parsing, secret separation, absolute contracts path, rollout, lease/hash/sync interval defaults and rejection | ordinary; reads config and process env; PostgreSQL inherited through alias | retain |
| `symmetry_control/goals/contract_fixture_manifest_test.exs` | 1 | validation, verification | Every manifest fixture is dispatched to its public Elixir validator and agrees on semantic validity/error code | ordinary; contracts tree and `ExJsonSchema` dependency | retain; candidate C1 survivor |
| `symmetry_control/goals/contract_validation_test.exs` | 13 | unit, validation | Public schema mapping, UTF-8/path bounds, strict enums/fields, semantic Goal/decision rules, root/cache behavior | ordinary; contracts tree; module conditionally skips if `ExJsonSchema.Schema` unavailable | retain core synthesized cases; candidate C1 overlap |
| `symmetry_control/goals/dispatch_notification_test.exs` | 10 | verification, smoke | Commit-after-notify ordering for scheduler/PubSub/Oban; rollback and replay suppress hints/jobs | `SYMMETRY_GOAL_DISPATCH_E2E=1 mix test ...`; PostgreSQL and supervised processes | **blocked retain**: no tracked CI/script sets this variable |
| `symmetry_control/goals/validation_profiles_test.exs` | 6 | unit, validation | Frozen profile registry, profile kind/digest/runtime safety and fail-closed lookup | ordinary | retain |
| `symmetry_control/goals/workers_test.exs` | 6 | unit, verification | Oban worker malformed/no-op handling and unique fenced job identities/retries | ordinary; PostgreSQL/Oban manual mode | retain |
| `symmetry_control/goals_external_wait_schema_test.exs` | 8 | unit, validation | Immutable external-wait source/result, terminality, schedule rules and state partitions | ordinary; PostgreSQL | retain |
| `symmetry_control/goals_read_model_test.exs` | 46 | unit, verification | Goal projection, typed evidence/attention, current revision authority, dependency completion, accounting and sanitization | ordinary; PostgreSQL | retain |
| `symmetry_control/goals_schema_test.exs` | 16 | unit, validation | Ecto changeset invariants for Goal membership, append-only state, usage, decisions, terminal authority/policy | ordinary; PostgreSQL | retain; changesets do not replace SQL constraints |
| `symmetry_control/goals_test.exs` | 131 | unit, verification | Goal authority lifecycle, atomic admission/settlement, replay, validation evidence, budgets, sessions/receipts, handoff/resume and recovery | ordinary; PostgreSQL/Oban | retain; protected authority/receipt/transaction coverage |
| `symmetry_control/health_test.exs` | 3 | unit | Health aggregation and safe handling of error/raise/throw/exit | ordinary | retain; distinct injected failure boundary from HTTP smoke |
| `symmetry_control/integrations/auth_test.exs` | 4 | unit, validation | CLI-derived GitHub/Azure auth, HTTPS requirement and PAT rejection | ordinary; command stub, no real credential | retain |
| `symmetry_control/integrations/azure_devops_test.exs` | 19 | unit, verification | Azure URL identity, pagination/state normalization, Pipeline binding, PR create race/replay/update and invalid input | ordinary; HTTP/auth stubs, PostgreSQL | retain; provider-specific protocol |
| `symmetry_control/integrations/change_action_test.exs` | 2 | unit, validation | Shared title/body normalization and forbidden Git ref components | ordinary | retain |
| `symmetry_control/integrations/command_test.exs` | 3 | unit, validation | CLI timeout, missing executable, and stderr/stdout credential-command separation | ordinary; OS subprocess | retain |
| `symmetry_control/integrations/github_test.exs` | 17 | unit, verification | GitHub identity, pagination/state normalization, Actions binding, PR create race/replay/update and invalid input | ordinary; HTTP/auth stubs, PostgreSQL | retain; not equivalent to Azure API/platform |
| `symmetry_control/integrations/syncer_test.exs` | 1 | unit, verification | Periodic sync traverses all connected resources | ordinary; PostgreSQL/supervised syncer | retain |
| `symmetry_control/integrations_test.exs` | 28 | unit, verification | Stale-write races, credential rejection, provider-owned field ordering, rollback, identity immutability and sanitized failures | ordinary; PostgreSQL/provider stub | retain; protected provider-field and transaction coverage |
| `symmetry_control/migrations/add_history_lookup_indexes_test.exs` | 1 | validation, verification | Migration rerun leaves valid current/history lookup indexes | ordinary; PostgreSQL DDL | retain |
| `symmetry_control/migrations/add_retry_commands_test.exs` | 2 | validation, verification | Safe rollback without history and refusal preserving durable retry history | ordinary; PostgreSQL DDL | retain |
| `symmetry_control/migrations/add_task_command_ownership_test.exs` | 1 | validation, verification | Backfill and nullable command binding constraints | ordinary; PostgreSQL DDL | retain |
| `symmetry_control/migrations/goal_0006_migration_test.exs` | 59 | validation, verification | Goal migration ordering, clean rollback/reapply, legacy backfill, SQL authority/identity/receipt/policy/lineage constraints and destructive rollback refusal | ordinary; disposable PostgreSQL schema required | retain; SQL boundary cannot be replaced by changesets |
| `symmetry_control/migrations/goal_external_wait_migration_test.exs` | 4 | validation, verification | External-wait DDL/down path, unsupported terminal transition and immutable receipt | ordinary; PostgreSQL DDL | retain |
| `symmetry_control/migrations/version_request_hashes_test.exs` | 2 | validation, verification | Hash-version rollback safety with/without canonical history | ordinary; PostgreSQL DDL | retain |
| `symmetry_control/orchestration/capability_scheduling_test.exs` | 7 | unit, verification | Frozen connected scope, runtime compatibility, assignment/claim recheck, submission/retry idempotency | ordinary; PostgreSQL | retain |
| `symmetry_control/orchestration/scheduler_test.exs` | 2 | unit, verification | Expiry requeue generation and wake while scheduler registration changes | ordinary; PostgreSQL/supervised scheduler | retain |
| `symmetry_control/orchestration_goal_admission_test.exs` | 71 | unit, verification | Goal-vs-legacy routing, runtime policy/affinity, transaction rollback, control replay, retained sessions, fences, attempts and after-commit notifications | ordinary; PostgreSQL | retain; protected authorization/transaction/replay surface |
| `symmetry_control/orchestration_request_hash_test.exs` | 5 | unit, verification | Persisted legacy/canonical versions, task replay/conflict, invalid-input translation, event/transition compatibility | ordinary; PostgreSQL | retain; persistence boundary distinct from pure hash unit tests |
| `symmetry_control/orchestration_retry_test.exs` | 2 | unit, verification | Retry idempotency/intent refresh and terminal eligibility | ordinary; PostgreSQL | retain |
| `symmetry_control/orchestration_review_test.exs` | 18 | unit, verification | Cancellation/ack/input delivery, protocol errors, concurrent enrollment/submission and terminal generation serialization | ordinary; PostgreSQL | retain |
| `symmetry_control/orchestration_supervision_test.exs` | 21 | unit, verification | UTF-8 guidance bound, versioned command hashes, pause/resume/cancel priority, decision packet durability and fencing | ordinary; PostgreSQL | retain |
| `symmetry_control/orchestration_telemetry_test.exs` | 2 | verification | Committed lifecycle telemetry and suppression on outer rollback | ordinary; PostgreSQL/telemetry | retain; distinct emitted-event boundary |
| `symmetry_control/orchestration_test.exs` | 39 | unit, verification | Full enrollment-to-completion lifecycle, leases/fences/grace, replay, JSONB NUL rejection, cancellation/input races and paged read models | ordinary; PostgreSQL | retain; core transaction/replay coverage |
| `symmetry_control/pi_control_e2e_test.exs` | 7 | harness, verification, validation | Five real Pi 0.87.1 Control/daemon tests for crash recovery, accepted candidate, handoff/resume and process identity; two ordinary sandbox-timeout option tests | harness: `SYMMETRY_PI_CONTROL_E2E=1 SYMMETRY_PI_CONTROL_E2E_EXECUTABLE=<absolute pinned binary> mix test ... --include skip:true --only pi_control_e2e`; support tests run ordinarily | retain; native Windows/Linux identity and receipts are protected |
| `symmetry_control/provider_broker_e2e_test.exs` | 4 | harness, verification | Fake-agent/real-daemon broker paths for GitHub/Azure and fixed-artifact validation, with HTTP providers stubbed | `SYMMETRY_PROVIDER_BROKER_E2E=1 mix test ...`; Go toolchain, daemon source, loopback listener, PostgreSQL | retain; cross-process boundary |
| `symmetry_control/request_hash_test.exs` | 6 | unit, validation | Canonical ordering/key representation, DateTime precision, versions/write modes, literal distinctions and unsupported types | ordinary | retain; pure primitive assertions survive persisted caller tests |
| `symmetry_control/workspaces_completion_test.exs` | 11 | unit, verification | Optimistic versions, binding/affinity constraints, active intent freeze, archive/launch and concurrent Kanban ordering | ordinary; PostgreSQL | retain |
| `symmetry_control/workspaces_read_model_test.exs` | 4 | unit, verification | Runtime/evidence/health joins, generation isolation and stable timing projection | ordinary; PostgreSQL | retain |
| `symmetry_control/workspaces_test.exs` | 7 | unit, verification | Workspace aggregate validation, repository/CI identity, launch linkage, Goal guard and not-found atomicity | ordinary; PostgreSQL | retain |
| `symmetry_control_web/channels/daemon_channel_test.exs` | 2 | unit, smoke | Machine-authenticated hint topic and rejection of Authorization socket auth | ordinary; Phoenix channel/PubSub, PostgreSQL | retain |
| `symmetry_control_web/controllers/chat_controller_test.exs` | 3 | unit, smoke, verification | Operator/CSRF boundary, durable chat work/replay and explicit mutation intent | ordinary; Phoenix conn, PostgreSQL | retain |
| `symmetry_control_web/controllers/error_json_test.exs` | 4 | unit, smoke | Safe 4xx/5xx envelopes and actual router 404 fallback | ordinary; Phoenix conn | retain |
| `symmetry_control_web/controllers/goal_controller_test.exs` | 9 | unit, smoke, verification | Operator/portal Goal HTTP auth, replay/stale behavior, reads, duplicate JSON and typed/sanitized errors | ordinary; Phoenix conn, PostgreSQL | retain core; candidate C2 has two local overlaps |
| `symmetry_control_web/controllers/goal_machine_controller_test.exs` | 11 | unit, harness, verification | HTTP runtime affinity, session attach/stop receipts, evidence/usage replay, authorization/sanitization; final test verifies Go HTTP recovery across listener restart | first 10 ordinary; final: `SYMMETRY_GOAL_RECEIPT_E2E=1 mix test ... --only goal_receipt_e2e`; Go toolchain, disposable dedicated PostgreSQL DB | retain; receipts and ownership are protected |
| `symmetry_control_web/controllers/health_controller_test.exs` | 1 | smoke, verification | Routed `/healthz` invokes real database and registered-process checks and returns exact body | ordinary; PostgreSQL and app supervisors | retain; distinct from injected Health unit tests |
| `symmetry_control_web/controllers/operator_read_controller_test.exs` | 6 | unit, smoke, verification | Runtime/task/history/timeline HTTP projections and cursor tamper/scope rejection | ordinary; Phoenix conn, PostgreSQL | retain |
| `symmetry_control_web/controllers/portal_controller_test.exs` | 7 | unit, smoke, verification | Portal session/assets/workspace API/onboarding/launch/errors/cancel route | ordinary; Phoenix conn, PostgreSQL | retain |
| `symmetry_control_web/controllers/portal_integrations_test.exs` | 9 | unit, smoke, verification | Provider connect/bind/sync HTTP workflow, credential non-disclosure, stale/error classifications and mixed providers | ordinary; Phoenix conn, stubs, PostgreSQL | retain; HTTP aggregate boundary differs from context tests |
| `symmetry_control_web/controllers/portal_resource_identity_test.exs` | 2 | unit, smoke, validation | Case-insensitive GitHub create and Azure update identity through portal API | ordinary; Phoenix conn, PostgreSQL | retain; provider/update boundaries differ |
| `symmetry_control_web/controllers/portal_workflow_test.exs` | 10 | unit, smoke, verification | Browser-facing stale writes, runtime identity, archive restore, Kanban, CSRF, cancel/retry/input replay and timeline generation | ordinary; Phoenix conn, PostgreSQL | retain |
| `symmetry_control_web/controllers/protocol_controller_test.exs` | 26 | unit, smoke, verification | Machine protocol route shape, body-vs-query ownership, auth classes, leases/fences, commands, guidance/input and no legacy routes | ordinary; Phoenix conn, PostgreSQL | retain |
| `symmetry_control_web/controllers/provider_action_controller_test.exs` | 67 | unit, smoke, verification | Goal-scoped grants, signed capability/fence auth, durable intent replay/recovery, ambiguous outcomes, dispatch locks/timers, sanitization and provider-owned delivery | ordinary; Phoenix conn, PostgreSQL/provider stubs | retain; protected authorization/provider-field/unknown-outcome boundary |
| `symmetry_control_web/controllers/task_goal_guard_controller_test.exs` | 2 | unit, smoke, verification | Legacy HTTP command blocked for Goal Task while goal-less idempotency remains | ordinary; Phoenix conn, PostgreSQL | retain; routed authorization boundary |
| `symmetry_control_web/plugs/strict_evidence_json_test.exs` | 12 | unit, validation | Raw evidence capture, decoded duplicate detection, route/method/media classification, safe malformed errors and parser limits | ordinary; Plug test conn, no routed controller | retain; candidate C2 survivor |
| `symmetry_control_web/portal_html_test.exs` | 2 | unit, validation | Escaping login output and CSRF token interpolation | ordinary | retain |
| `symmetry_control_web/portal_session_test.exs` | 3 | unit, validation | Session expiry/token binding, renewal/logout and token rotation clearing | ordinary | retain |

## Support and entrypoint ledger

| Path | Responsibility and consumers | Disposition |
|---|---|---|
| `control/mix.exs` | Build definition and alias entrypoint. `test` creates/migrates the configured test DB; `precommit` compiles, unlocks unused deps, formats, and tests. It is operational configuration, not a test. | retain; changing alias semantics requires separate gate review |
| `control/test/test_helper.exs` | Starts ExUnit and places `Repo` SQL Sandbox in manual mode. Loaded by Mix for all test files. | retain |
| `control/test/support/data_case.ex` | Compiled only in test env. Owns SQL Sandbox owner lifecycle, bounded ownership-timeout option validation, and changeset error formatting. Used by DB context tests. Its option helper is tested by the second module in `pi_control_e2e_test.exs`. | retain; possible file-placement refactor only, no ablation |
| `control/test/support/conn_case.ex` | Test-env Phoenix case template; starts SQL Sandbox owner and a fresh conn. Used by channel/controller and E2E files. | retain |
| `control/test/support/integration_auth_stub.ex` | Deterministic credential-shaped headers plus process-local CLI expectation queue. Used by provider unit/E2E tests; validates request behavior without real credentials. | retain; a stub does not replace credentialed acceptance |
| `control/test/support/integration_http_stub.ex` | Process-local or shared concurrent HTTP expectation queue; asserts method, URL, selected headers/body and rejects unconsumed calls. Used by provider tests. | retain |
| `control/test/support/integration_provider_stub.ex` | Provider behavior fake supporting malformed data, delayed race synchronization, provider-owned snapshots and action delivery. Used by context/controller tests. | retain; preserves behavior under assertion rather than masking it |
| `.github/workflows/ci.yml` | Tooling-owned external orchestrator. Runs ordinary Control format/compile/test/release, provider broker opt-in, dedicated Goal receipt DB test, and Linux/Windows pinned Pi E2E jobs. Not an extra Control test. | retain; cross-owner gate |
| `control/config/test.exs` | Test DB defaults (`postgres/postgres`, localhost:5432, DB `symmetry_control_test` plus partition), SQL Sandbox, disabled schedulers, manual Oban, contract root. Configuration input, not a harness. | retain |
| `contracts/fixtures/**` + manifest | Tooling-owned shared protocol fixtures consumed by Elixir fixture validation and other language gates. They are shipped test data, not independent tests. | retain shared fixtures; candidate C1 concerns duplicated Elixir assertions only |

## Candidate ledger

### C1 - Manifest-backed fixture assertions duplicated in the broad contract unit file

**Compared surfaces.**

`contract_fixture_manifest_test.exs` loads all 184 manifest rows, dispatches each
schema name to the matching public `ContractValidation.validate_*` function,
asserts semantic validity, and checks declared semantic error codes. The first and
third/fourth/seventh/eighth tests in `contract_validation_test.exs` also load named
valid/invalid fixture files and assert many of the same validator outcomes. Setup is
nearly identical (same `contracts/v1`, valid/invalid fixture roots and public
validator module); both are in-process, platform-neutral, credential-free validation.
For fixture-backed accept/reject outcomes, the failure mode is equivalent: validator
mapping/schema/semantic drift causes an assertion failure.

They are **not wholly duplicates**. The broad unit file additionally constructs
values not represented by fixtures (UTF-8 code-point limits, malformed maps,
recursive atom normalization), checks pure decision/final-authority helpers,
requires an explicit root, and verifies resolved-document cache behavior. Those
assertions must survive. The shared manifest also verifies fixture enumeration and
cross-language expectations and must survive.

**Smallest experiment.** In an isolated checkout with a known-green DB baseline:

```sh
cd control
mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs \
  test/symmetry_control/goals/contract_validation_test.exs
```

Then temporarily ablate only the named fixture assertions from the broad unit tests,
leaving synthesized/helper/root/cache assertions. Run the same command once.
For a meaningful expected-red mutation, change one isolated public validator mapping
in the manifest test (for example map `admission` to a validator for another schema),
or mutate one fixture-covered schema/semantic branch; the surviving manifest test
must fail. Restore experiment-owned edits, then repeat focused green verification.
No fixture or production mutation belongs in this inventory wave.

**Exact surviving coverage required.** All 184 manifest entries; unique fixture IDs;
manifest version/nonempty checks; every schema-to-public-validator mapping; declared
semantic error code checks; every synthesized UTF-8, malformed-map, normalization,
decision/authority, explicit-root, and cache assertion in the unit file.

**Disposition:** actionable partial-ablation candidate, pending baseline/mutation
experiment by the parent. No complete file is removable.

### C2 - Repeated duplicate-JSON assertions at direct Plug and routed Goal endpoints

**Compared surfaces.**

`strict_evidence_json_test.exs` directly proves decoded duplicate detection and that
all decoded `POST|PUT|PATCH|DELETE` paths beginning `api/v1` or `portal/api` are
strictly parsed, including escaped path segments and structured JSON media types.
Production `StrictEvidenceJSON.json_mutation_path?/1` classifies the whole prefix,
not individual Goal routes. `goal_controller_test.exs` repeats this behavior through:

1. protocol Goal creation (`title` duplicate),
2. protocol Goal command (`kind` duplicate), and
3. portal Goal creation and command (two assertions in one test).

The routed tests have a stronger HTTP/endpoint boundary than direct Plug tests, so
removing all routed assertions would weaken coverage. Within each prefix, however,
creation and command use the same endpoint Plug before controller parsing, the same
method/content type, the same failure envelope, and the same failure mode. The
command instance is assertion-equivalent to the creation instance for parser
selection; it does not reach command authorization/domain code.

**Smallest experiment.** Establish a focused baseline:

```sh
cd control
mix test test/symmetry_control_web/plugs/strict_evidence_json_test.exs \
  test/symmetry_control_web/controllers/goal_controller_test.exs
```

Temporarily remove only protocol command duplicate-member test and the portal command
half of the combined portal duplicate test. Retain one routed protocol creation and
one routed portal creation assertion plus all direct Plug cases. Run the focused
command once. Then mutate `json_mutation_path?/1` independently for `api/v1` and
`portal/api` prefixes; each surviving routed case and its corresponding direct Plug
case must go red. A nested/escaped-key mutation must still be caught by the direct
recursive test. Restore and rerun green.

**Exact surviving coverage required.** Recursive escaped duplicate detection;
protocol and portal path prefixes; escaped path decoding; JSON and `+json`; exact
400 `invalid_request` envelope; one real endpoint traversal for each auth surface;
evidence raw-body deferred materialization; malformed/non-object safety; GET and
non-target bypass; request-size behavior. No evidence-batch HTTP case in
`goal_machine_controller_test.exs` is part of this ablation because it uniquely
proves authorization occurs before materialization and no write survives.

**Disposition:** actionable two-assertion partial-ablation candidate, pending parent
experiment. Keep HTTP context separate from direct Plug unit coverage.

## Examined similarities that are not redundancy

- `health_test.exs` uses injected checks to cover safe exception/throw/exit handling;
  `health_controller_test.exs` exercises real Repo/process checks through routing.
  Setup, boundary, and failure modes differ.
- `request_hash_test.exs` proves the pure canonicalization primitive;
  `orchestration_request_hash_test.exs` proves stored versions, replay and caller
  translation to `:invalid_request`. Removing either loses a boundary.
- GitHub and Azure tests have parallel names but distinct URL grammars, pagination,
  review/result vocabularies and provider APIs. Shared `ChangeAction` tests do not
  subsume them.
- Context tests and controller tests often share domain outcomes, but HTTP auth,
  CSRF, body-vs-query ownership, status/error serialization and credential
  non-disclosure are distinct assertions.
- Ecto changeset tests do not subsume PostgreSQL migration/constraint tests. In
  particular, Goal authority, immutable receipts, migration rollback refusal,
  provider-owned fields and transaction/replay checks remain retained.
- Fake provider/agent tests do not replace credentialed or native acceptance. The Pi
  tests preserve executable hash/version, OS process identity, real daemon restart,
  handoff/resume and receipt boundaries. Provider broker tests cross real daemon and
  loopback HTTP boundaries despite provider transport stubs.
- `dispatch_notification_test.exs`, `orchestration_telemetry_test.exs`, and Goal
  admission notification assertions concern different event consumers and commit
  paths. Similar "only after commit" language is not assertion equivalence.
- The two `DataCaseSandboxOptionsTest` declarations are ordinary support validation
  accidentally colocated in the Pi E2E file. Moving them could improve discoverability
  but removes no coverage and is not an ablation candidate.

## Environment probe, blockers, and exact prerequisites

Read-only local probe on this Windows workstation found:

- no `elixir` executable;
- no `mix` executable;
- no `psql` or `pg_isready` executable;
- no `POSTGRES_*` or `DATABASE_URL` environment configuration;
- TCP `127.0.0.1:5432` unreachable.

Therefore neither a focused baseline nor a disposable test database can run locally.
No language/runtime was installed, no service was started, and no database was
created or dropped.

The ordinary CI-equivalent prerequisite set is Elixir 1.20.4, OTP 29.0.6, existing
Hex dependencies, the repository contracts tree, and PostgreSQL 18.6 reachable with
`POSTGRES_HOST`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, and a disposable
`POSTGRES_DB`. `mix test` is mutating by design because its alias creates/migrates
that DB. Parent experiments should use an isolated checkout and dedicated disposable
DB, never the unknown local/default database.

Additional protected prerequisites:

- provider broker: Go 1.27, daemon source/build tools, loopback ports, and
  `SYMMETRY_PROVIDER_BROKER_E2E=1`; no provider credentials;
- Goal receipt listener: Go 1.27, a dedicated random PostgreSQL DB, loopback listener,
  and `SYMMETRY_GOAL_RECEIPT_E2E=1`;
- Pi Control: exact Pi 0.87.1 binary with source-pinned OS/architecture SHA-256,
  Go-built witness daemon, filesystem/process-control support, loopback synthetic
  upstream, `SYMMETRY_PI_CONTROL_E2E=1`, absolute
  `SYMMETRY_PI_CONTROL_E2E_EXECUTABLE`, `--include skip:true --only pi_control_e2e`;
  Windows and Linux executions are separate evidence;
- dispatch notification: `SYMMETRY_GOAL_DISPATCH_E2E=1`, PostgreSQL, scheduler/PubSub
  and manual Oban. No tracked workflow or script references this variable besides the
  test file, so these 10 declarations are currently a gate-coverage blocker, not an
  ablation opportunity.

No candidate above is admitted for removal until the parent records a green focused
baseline, performs the expected-red ablation/mutation comparison at the stated
boundary, restores experiment-owned changes, and verifies the exact survivor set.
