# Goal 0009: achieve-contract amendments A, B and C

## Scope and evidence boundaries

The binding contract is
[`docs/goals/archive/0009-achieve-contract-amendments.md`](../goals/archive/0009-achieve-contract-amendments.md),
archived by a content-preserving move after the standalone evidence was produced.
The implementation plan is
[`2026-11-15-2235-feat-achieve-contract-amendments-plan.md`](../plans/2026-11-15-2235-feat-achieve-contract-amendments-plan.md).
The supplied baseline is `343dea6a62484d73e7b577a0be2683ca233510f6`.
This increment is an uncommitted working-tree change; no commit, push or history
rewrite is authorized or performed.

The separate
[`independent amendment review`](goal-0009-amendment-review-2026-11-16.md)
records reviewer scope, findings, repairs and retained trade-offs. It is not the
owner's verdict.

The approved local project is `139a5747-5878-4503-9972-1bd3a46b9400` and its
repository resource is `eb968617-36be-48ce-8321-b6b20f7ce77f`. The acceptance
stack uses a run-owned database, `symmetry_0009_dev`, cloned from the existing
local stack, plus a separate test database. Existing goals and the original
database are not acceptance targets. These identities were available before
implementation; no synthetic replacement was substituted for the prerequisite.

The repository used as a read-only execution and measurement carrier is commit
`8ded77150ec0ae49d52355670fdad78a348bf1d5`, with 1,091 blobs and protocol digest
`sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c`.
That older commit is a carrier, not the implementation revision. It does not
validate the amended source by itself, and old completion receipts are not
evidence for the amended achieve ceremony.

## Preserving clauses

- A changes only eligible operator Goal acceptance and its derived readiness.
  A supplied decision remains subject to its existing exact authority binding;
  an exact current Goal/revision/Subject completion rejection remains a refusal.
  WorkItem acceptance, non-operator authority, required outcomes, evidence,
  dependencies, unresolved decisions and nonterminal tasks retain their guards.
- B counts required items, not all items. Exactly one required item supplies the
  integration role; a conflicting stored or explicit designation is refused,
  never ignored or rewritten. Other cardinalities retain explicit designation
  and payload obligations. Original canonical proposal/hash/approval validation
  precedes persistence of the derived role; replay and plan immutability remain.
- C retains the canonical bytes and `sha256:<64 hex>` carrier. Both consumers
  use one persistent batch process, retain refusal and cancellation/reaping
  behavior, and preserve the Linux clone's existing five-minute command budget
  once for the stream. Thirty seconds is the existing Subject-observation
  context from 0006j, not a new daemon timeout.

## Regression evidence

Raw command output is retained under `.tmp/0009/` in this checkout. Exit codes
below describe the observed invocation, not an inferred CI result.

| Check before implementation | Observed result |
| --- | --- |
| Focused workspace Subject regression checks | Exit 1: 5 passed, 2 failed. The seven-blob public materialization used seven `cat-file` processes. The Linux 250-blob fixture sampled 205 distinct PIDs; this is not an exact process count. |
| Selected Control A/B regressions | Exit 2: 1/14 passed, 13 failed, 174 excluded. |
| `node contracts/scripts/check.mjs` with new wire cases | Exit 1: the omitted-integration command expected to be valid was rejected for the old required payload field. |

The corresponding files are `subject-red.log`, `control-red-prepared.log` and
`wire-red-prepared.log`. Earlier missing-Hex and missing-Ajv attempts are
prerequisite failures, not red regression evidence.

The focused workspace command after implementation was
`go test ./internal/workspace -run 'Test.*Subject|TestGitCommandOutputLimited|TestLinuxCloneTreeDigest' -count=1 -v`,
under the unprivileged WSL `accept` user with a stdio-only inherited descriptor
boundary, `GOMAXPROCS=4` and `GOFLAGS=-p=1`. It exited 0; the package reported
14.746 seconds. Its opt-in measurement case was skipped in this invocation.
Missing-object, oversized-object, unsupported-tree, cancellation and child-reap
checks ran rather than being replaced by the measurement witness.

An independent source review subsequently found a real teardown defect: a
non-`ErrProcessDone` failure from the batch command's `Cancel` callback did not
fall back to killing its retained child. The added
`TestSubjectBatchStreamCancelFailureFallsBackToKill` failed before the repair
after 5.19 seconds; the test's explicit escape cancellation was required.
`subject-review-red.log` exited 1. The ten-row malformed-response/control table
already passed in that red invocation.

The repair retains the original cancellation error, invokes the retained
`Process.Kill`, joins a second failure, and leaves the single `Wait` and
unresolved-process fence intact. The broad Subject command, now with
`-timeout 120s`, exited 0 in `subject-review-green.log`: **33 top-level passed,
1 opt-in measurement skipped**, package time 23.423 seconds. The new teardown
regression passed in 0.12 seconds, without cancelling its own context; all ten
framing rows passed. The independent reviewer closed both the teardown and
framing-coverage findings after reading the repair and tests. The earlier full
Go runs and initial measurements predate this correction; they are not claimed
as executions of the corrected source. The final measurements below were rerun.

## Subject measurement and independent equality

The following table observes the final production source after the correction.
No measurement invocation overlapped a full test suite. Each public
`Manager.MaterializeSubject` observation used the existing thirty-second context
and the same clean carrier commit and resource. The opt-in witness is
`TestSubjectMeasurementWitness`, enabled by
`SYMMETRY_SUBJECT_WITNESS_REPOSITORY`, `SYMMETRY_SUBJECT_WITNESS_COMMIT` and
`SYMMETRY_SUBJECT_WITNESS_RESOURCE_ID`.

| Platform | Blobs | `cat-file` children | All Git children | Materialization wall time | Exit |
| --- | ---: | ---: | ---: | ---: | ---: |
| Linux, WSL unprivileged user | 1,091 | 1 | 5 | 377 ms | 0 |
| Windows | 1,091 | 1 | 5 | 1,649 ms | 0 |

Both returned
`sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c`.
The unchanged `scripts/ci/linux-v2-source-subject.py` ran separately on each
checkout and exited 0 with the identical commit and digest. Parent-side machine
assertions compared both production observations with their independent JSON
outputs. The independent script is a digest oracle, not a timing baseline. No
speedup ratio or comparison with the differently sized historical checkout is
claimed. These are elapsed production observations, not end-to-end
checkout/bootstrap timings.

The machine-readable
[`final measurements`](goal-0009-subject-measurements-2026-11-16.json) bind these
observations and both fresh independent-oracle invocations to the
[`final source identity`](goal-0009-source-identity-2026-11-16.json): 500 source,
test, dependency-pin, schema and generated files, plus both built daemon hashes.
The installed Linux daemon hash is
`c8f30088c10ad1ba1513f4afdfaf98da3bbf9b51b7bd90cbf1bd2de9cefabf68`, matching
the built binary. Only the run-owned `/out-0009` daemon was stopped/replaced;
its replacement registered the same runtime at epoch 2 before dispatch. The
initial pre-correction observations were 498/1,114 ms and remain historical
logs, not final-source evidence or a speed baseline.

The Windows-owned copy initially exposed a checkout-mode difference. Setting
that copy's `core.fileMode=false`, the native Windows default, removed the
mode-only discrepancy before either oracle or production measurement; no tracked
content or repository history was changed. Neither this measurement nor a
generic fixture establishes installed Linux native-owner qualification.

## Observed gate limitations

- On the corrected production C source, the scoped Linux workspace `go vet`,
  `go test -count=1 -timeout=300s -json ./internal/workspace` and
  `go test -race -count=1 -timeout=600s -json ./internal/workspace` each exited
  0. Both test invocations recorded **133 passing, 0 failing, 3 skipped test
  events**, with package times 74.107 and 76.900 seconds. The serial scoped
  checks took 181.14 seconds. Their files are
  `subject-review-workspace-linux-{gates,vet,test,race}.log`.
- The corresponding Windows workspace vet exited 0, but its complete
  `go test -count=1 -timeout=180s -json ./internal/workspace` exited 1 after
  181.630 seconds: **73 passing, 0 terminal failing, 2 skipped test events**,
  then a package timeout. At the deadline,
  `TestPrepareReleasesCreatedReservationAfterFailedAdd` had run for one second.
  The new framing table and cancellation-fallback test had already passed.
  This is a second failed Windows aggregate gate, not a passing suite; its
  timeout was not extended and no test was skipped to make it pass.
- The subsequent change to the new regression's diagnostic counter was
  test-only: `atomic.Int32` removes a possible failure-path race. On that exact
  final test source, the two new regression functions passed again on Linux
  with `-race` (exit 0; package 6.231 seconds) and Windows (exit 0; package
  8.014 seconds), including all ten framing rows. Both final daemon builds
  with `-buildvcs=false` exited 0; both changed Go files were gofmt-canonical.
  These invocations are `subject-review-final-{linux,windows}.log`.
- `mix format --check-formatted` and `mix compile --warnings-as-errors`, with
  `MIX_ENV=test` in the isolated Control container, each exited 0 before the
  scoped simplification pass and again on the final Control source. The final
  focused Goal, read-model and contract-validation tests exited 0 with
  **202 passed**, recorded in `control-simplified-green.log`;
  `control-final-static.log` records both final static checks exiting 0.
- The pre-teardown-correction Linux matrix completed with `go vet ./...`,
  `go test -count=1 -timeout 300s -json ./...`,
  `go test -race -count=1 -timeout 600s -json ./...` and `go build ./...` all
  exiting 0, under the unprivileged stdio-only boundary described above. Each
  test invocation recorded **4,656 passing, 0 failing, 102 skipped test
  events**, and **24 passing, 0 failing, 1 skipped package events**. The serial
  matrix took 1,576.89 seconds; its logs are `go-linux-*.log`.
- The first full `MIX_ENV=test mix test --seed 0` exited 2, reporting
  **832/836 passed, 21 skipped**. All four failures were in
  `SymmetryControlWeb.PortalControllerTest`: the generated
  `priv/static/portal/index.html` was unavailable. No fallback asset or stub was
  created to make those tests pass.
- The initial provider E2E invocation exited 2 with **0 tests, 4 invalid** because
  the isolated container lacked Go. A real Go 1.27.0 toolchain and its existing
  module cache were subsequently copied into that owned container; this setup
  action itself exited 0. The initial Goal-receipt E2E invocation exited 2 with
  **0/1 passed, 10 excluded** because its database name did not satisfy the
  required dedicated ephemeral database naming rule. Neither attempt is a pass.
  After those prerequisites were corrected, the final-source provider E2E
  suite exited 0 with **4 passed**; the HTTP Goal-receipt E2E exited 0 with
  **1 passed, 10 excluded**, using its correctly named isolated database.
  `MIX_ENV=prod mix release --overwrite` then exited 0. These three invocations
  are recorded in `control-final-e2e-release.log`.
- Windows `go vet ./...` and
  `go build ./cmd/symmetry-daemon ./cmd/symmetry-fake-agent` exited 0. The full
  `go test -count=1 -timeout 180s -json ./...` exited 1: **3,758 passing test
  events, 0 failing test events, 55 skipped test events**, with **20 passing,
  1 failing and 1 skipped package events**. The failing package is
  `internal/app`; its process timed out at three minutes while
  `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/request_deadline`
  waited in the attach-ack fixture. A package timeout is a failed gate despite
  the absence of a terminal failing-test event.
- Fresh frozen pnpm installation failed with
  `ERR_SSL_SSL/TLS_ALERT_HANDSHAKE_FAILURE` for the npm registry. The same TLS
  failure occurred with Windows Node 26 and the CI-major Node 24 Docker image;
  offline installation lacked required package metadata. No mirror, insecure
  TLS option, lockfile change or dependency-verification bypass was used.
  Existing matching-manifest contract dependencies enabled the exact package
  scripts to run directly: contract TypeScript compilation, 31 Node tests,
  15 Draft-7 schemas and 196 fixtures, including 192 parsed Effect cases, all
  exited 0. `node contracts/scripts/generate.mjs` also exited 0. The pnpm wrapper,
  fresh dependency install and frontend build are not thereby proved.

The dedicated output files include `control-full.log`, `control-e2e.log`,
`go-windows-gates.log`, `go-windows-test.log`, `node24-registry-transport.log`,
`pnpm-install-resume.log`, `pnpm-install-offline.log`, and
`contracts-effect-check.log`. Missing checks and failed gates remain explicit;
they do not authorize a weaker contract or an owner verdict.

## Real local Goal, readiness and replay

The final Goal is `48bc1784-e6b8-49da-809a-db5d323783ba`, on the approved
project/resource and runtime `feef2f50-e4cb-4a1b-9300-e0d30c7f87ba`.
The [sanitized runtime receipt](goal-0009-runtime-receipt-2026-11-16.json)
contains 41 HTTP observations, the original proposal, admitted membership,
accepted outcome, pre-achieve projection, primary attention entry, exact
mutation requests/responses, replay, event readback and failed-attempt history.

The plan has two optional items and one required item. All three original
integration markers were omitted; canonical hashing materialized three false
defaults. Admission derived only `required-readme` as integration. The actual
approval identity was independently recomputed and read back from the final
database row after the run:

- Proposal: `e6be5eaa-4a99-49f7-afb1-dae6960632de`.
- Original canonical proposal hash:
  `sha256:86c49f60663f28340fe7a4a38587304ac210e2be7c363875b1805424a757dd08`.
- Plan decision: `48d19f23-6a33-4084-b102-6d1d9c70538f`.
- Expected and observed approval action hash:
  `cd97768eaa98084697c25da1c3dbf7ebd4e1c657b9a45bd6a010e50ed92f773d`.

The action hash uses the existing `kind`, `goal_id`, `revision` and unprefixed
proposal-hash fields. The original proposal/hash, not a proposal rewritten with
the derived true marker, authorized admission. Plan replay returned an identical
receipt. The same derivation also permits achieve with the integration payload
field omitted.

The required producer completed, a separate artifact validator produced one
accepted required outcome, and the optional nonterminal probe then completed.
These are three admitted tasks. The two actual OmO `5.1.69` native sessions used
the existing `tencent-litellm` / `deepseek-v4.1-flash-fast` route: the producer
made two successful README `read` calls and the optional task made one. Their
recorded tools contain no write, command or delegation call. The native binary
SHA-256 is `3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444`.
Raw native transcripts and resolved credential values are not published; the
receipt retains only session/run binding, tool arguments, successful-result
flags and output byte counts/hashes.

Both execution worktrees have no tracked changes. Their complete porcelain
output is `?? .symmetry-workspace.json`: the daemon's recovery journal, retained
rather than removed to obtain a clean claim. They are **not** described as
strictly Git-clean. This is separate from the clean measurement checkouts.
Existing native credentials remained local to the existing native host and
approved child environment. This run establishes neither installed Linux-owner
qualification nor a verified billable price or zero-charge claim.

Immediately before achieve, the observed projection reported one required item,
one accepted required item, no nonterminal tasks, no open decisions and
`ready?: true`. Its exact allowed actions were
`["achieve", "pause", "amend", "cancel"]`. There were zero completion decisions;
neither blockers nor the primary attention entry contained
`final_acceptance_required`, and attention reported `ready_to_achieve?: true`.

The operator achieve request contained `subject`, accepted `evidence_ids` and
`decision_id: null`, with no `integration_work_item_id`. It returned HTTP 200,
the Goal read back as `achieved`, the identical request/mutation replay returned
the identical receipt, and event readback contained exactly one operator
`achieve` event. Operational Goal acceptance is not the owner's verdict on this
implementation increment.

### Observed refusals on the amended implementation

| Observation label in the runtime receipt | HTTP | Exact code |
| --- | ---: | --- |
| `missing-required-outcome` | 422 | `incomplete_work` |
| `conflicting-integration-payload` | 422 | `integration_outcome_required` |
| `integration-subject-mismatch` | 422 | `integration_outcome_required` |
| `unresolved-decision` | 422 | `unresolved_decision` |
| `nonterminal-optional-task-resumed` | 422 | `nonterminal_task` |
| `multi-missing-designation-admission` | 422 | `invalid_plan` |
| `multi-missing-integration-payload` | 400 | `invalid_request` |
| `foreign-plan-authority-decision` | 409 | `invalid_decision` |
| `work-item-review-cannot-authorize-goal-achieve` | 409 | `invalid_decision` |
| `single-conflicting-designation-admission` | 422 | `invalid_plan` |

The WorkItem review decision was real, resolved, and bound to the same primary
Goal, revision and exact Subject; its different authority kind could not
authorize Goal achieve. No completion decision was created as a substitute.
The multi-required plan then admitted with an explicitly marked item and
replayed identically, but achieve without its explicit integration ID still
failed. The conflicting single-required marker was refused at admission, not
silently moved to the required item.

### Failed attempts and explicit recovery boundaries

- The first run-owned Goal, `614a9c2e-9232-4606-9ad5-8c2d6276aa18`, queued its
  producer while the gates ran. Its first dispatch failed before native launch
  with `Goal admission deadline has elapsed before native launch`; the
  `validate` driver exited 1. Its one allowed attempt was not retried. The Goal
  was cancelled through the real API (HTTP 200); its failure and cancellation
  remain in the receipt. The final Goal was created separately and dispatched
  immediately. No deadline or retry policy was enlarged.
- The first optional-task refusal request saw HTTP 409 `stale`, not
  `nonterminal_task`; that invocation exited 1 and was not counted as refusal
  proof. The driver resumed only the already admitted, still queued task's
  refusal and observed HTTP 422 `nonterminal_task`, without another admission.
  A decoded stale response permits one explicit version refresh only while the
  Goal revision stays identical; other errors are not retried or accepted.
- The first `finish` invocation exited 1 because the driver expected a missing
  integration marker to fail at proposal request. That request correctly
  returned HTTP 200: proposal request is not plan admission. The driver reused
  the recorded decision, resolved it and observed HTTP 422 `invalid_plan` at
  `accept_plan`. The final `finish` invocation exited 0. No native work was
  repeated and no production validation was relaxed.
- After recording the evidence, only the two run-owned refusal-probe Goals
  (`84bb42df-1d5f-4386-ab80-99e9c100a1b8` and
  `c0bbfe64-9499-4273-9087-e5c42f5beb9d`) were cancelled, each through HTTP 200.
  The primary remains achieved. The original stack/database and completed 0006
  Goal/receipts were not mutation targets.

## Additional exact commands and outcomes

Commands below ran in `control/` with `MIX_ENV=test` and the run-owned database,
or at repository root for contract/Python checks. Go commands above ran in
`daemon/`. `ERL_FLAGS=+S 4:4`, `GOMAXPROCS=4` and `GOFLAGS=-p=1` bounded local
parallelism. Receipt E2E used
`POSTGRES_DB=symmetry_receipt_e2e_3a80a6d9132548f783376ca63a237e07`.

| Command | Observed outcome | Exit |
| --- | --- | ---: |
| `mix format --check-formatted` | Final project formatter gate | 0 |
| `mix compile --warnings-as-errors` | Final Control source | 0 |
| `mix test test/symmetry_control/goals_test.exs test/symmetry_control/goals_read_model_test.exs test/symmetry_control/goals/contract_validation_test.exs --seed 0` | 202 passed | 0 |
| `SYMMETRY_PROVIDER_BROKER_E2E=1 mix test test/symmetry_control/provider_broker_e2e_test.exs --seed 0` | 4 passed | 0 |
| `SYMMETRY_GOAL_RECEIPT_E2E=1 mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e --seed 0` | 1 passed, 10 excluded | 0 |
| `MIX_ENV=prod mix release --overwrite` | Production release assembled | 0 |
| `node contracts/scripts/generate.mjs` | Generated Go/TS/Effect artifacts | 0 |
| Local `tsc --project contracts/tsconfig.json` | Contract TypeScript check | 0 |
| `node --test contracts/scripts/*.test.mjs` | 31 passed, 0 failed/skipped | 0 |
| `node contracts/scripts/check.mjs` | 15 schemas, 196 fixtures | 0 |
| `node contracts/scripts/check-effect.mjs` | 196 raw fixtures, 192 parsed Effect cases; generated files current | 0 |
| `python3 scripts/ci/test-linux-v2-source-subject.py` | 4 passed, unprivileged user | 0 |
| `python3 scripts/ci/test-linux-v2-go-events.py` | 6 passed, unprivileged user | 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` | 27 passed, unprivileged user | 0 |
| `python3 scripts/ci/test-rootless-compose-owner-claim.py` | 1 passed, unprivileged user | 0 |
| `python3 scripts/ci/test-rootless-compose-transition-proxy.py` | 1 passed, unprivileged user | 0 |
| `python3 scripts/ci/test-linux-v2-control-receipt.py` | 9 passed, privileged permission fixtures as in CI; not native qualification | 0 |

The operator driver ran as `python /scripts/witness.py MODE` in the same Docker
network, using an environment-supplied local operator token. The final sequence
was `prepare` (0), immediate scheduler drain (0), `validate` (0), validator drain
(0), `probe` (1: stale), resumed `probe` (0), optional drain (0), `finish`
(1: wrong request/admission expectation), resumed `finish` (0). The final driver
source snapshot and the HTTP requests/responses are retained in the runtime
receipt; native session transcripts are not. Manual dispatch used the real
`SymmetryControl.Orchestration.Scheduler.drain/0` on the isolated Control node,
not inserted task/run/outcome rows.

## Standalone Done-when pairing

| Contract outcome | Evidence produced by this run |
| --- | --- |
| A/B fixed contracts and additive wire shape, nullable `decision_id` preserved | `docs/design/{core,data,protocol,contracts}.md`, `contracts/v1/goal-command.schema.json`, generated artifacts and six new fixture cases; direct wire gate above |
| Direct operator achieve, readiness/attention, achieved event and identical replay | Final primary Goal and exact observations in the runtime receipt; zero completion decisions, direct achieve offered |
| Required outcome, Subject, unresolved, nonterminal and other-authority refusals | Ten exact HTTP/code observations above, including the same-Goal/revision/Subject WorkItem-review authority check |
| Sole-required binding, conflicting marker/payload refusal, multi-required explicit obligations, immutable proposal/hash/replay | Original three-item proposal, derived persisted role, independently recomputed/read-back approval identity, admission refusals and plan replay in the runtime receipt |
| More than 1,000 blobs, one batch, existing 30-second deadline and independent equality | Final Linux 377 ms / Windows 1,649 ms observations for 1,091 blobs, one batch/five Git children each, both fresh independent results and final-source/binary fingerprints |
| Existing size, unsupported-entry and missing-object refusals | Current-production workspace checks and focused Subject checks, including Linux clone refusal/cancellation/reap paths; no substituted fake native carrier |
| Independent amendment review | Separate authority and Subject reviewer sessions and their findings/closure in the linked review record |

## Produced result and unresolved limits

Result: **produced and awaiting the owner's verdict**. There is no model-supplied
owner rating or signature. Required CI before merge remains unpassed wherever
the Windows aggregate timeouts, missing Portal asset/fresh frontend dependencies
or unavailable platform/native qualification checks are recorded above. The
browser/frontend suites and a hosted CI matrix were not run; direct wire checks
are not a pnpm-wrapper or frontend-build pass. The complete Go matrices preceded
the final teardown correction; the corrected production source was rechecked
with the workspace package's full test/race/vet scope and the final two focused
regressions/builds, not another complete matrix. No final-source whole-repository
Go matrix pass is claimed. No timeout, schema, test,
permission, authority, acceptance predicate or recovery fence was weakened to
change these outcomes.
