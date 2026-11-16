# Model-routing evaluation workflow

This directory records task evidence, not a model leaderboard or routing-policy promotion.
The binding policy is [quality.md](../../design/quality.md); the first runnable increment is [0008a](../../goals/archive/0008a-auditable-acceptance-workflow.md).
Use the [task instructions](task-instructions.md) and [independent review instructions](review-instructions.md) with the repository's configured worker/reviewer routes.

## Ownership and startup

The coordinator freezes the task before implementation and keeps run state outside the worker checkout.
Reuse existing configured routing and authorization; do not select a new model, profile, credential or spend cap here.
Resolve the effective role overrides, then prove both required routes with a real request before starting the task.
Record provider/model/effort, configuration provenance and credential-route metadata only, never credential values.
Missing required routing or authorization at startup stops the run immediately; a partially started or fixture-only substitute is not evidence.

The worker owns only the task's allowed files in a fresh isolated Git checkout.
The coordinator owns frozen contracts, verification results, usage ledgers, review import and receipt derivation.
The reviewer reads the requirement and actual diff in a separate invocation, rather than accepting the worker's summary.
Same-model separate contexts are recorded as such; they establish invocation separation, not model diversity or immunity to correlated mistakes.

This is process isolation and auditability, not an OS security sandbox.
A same-user adversary able to rewrite the coordinator's private evidence can forge its records; use an externally protected coordinator if that is in the threat model.
Checks and review imports are trusted coordinator observations, not worker authority.

## Frozen task and exact Subject

The [positive task](tasks/go-subject-receiver.json) and [escalation task](tasks/go-linux-final-scan.json) are runnable examples on real repository code.
Task JSON declares base commit, outcome, required explanations, allowed paths, protected paths and exact argv gates.
The runner uses no shell command interpolation.
Existing tests, acceptance assets and declared guards remain protected; a new task contract is required for a legitimate protected-asset change.
Sealing derives the change set from bytes instead of Git's filtered or cached view: base blobs are read from the object store, worktree files are hashed from disk, symlink targets are compared by object id, file modes are compared where the host and Git track them, and a checkout whose index carries assume-unchanged, skip-worktree or other nonstandard entries, or whose Git view carries checkout-local filter/attribute machinery (any `filter.*` configuration inside the checkout, a `.git/info/attributes` file, `core.attributesFile`, or a `filter`/`working-tree-encoding` attribute on a bound path), is refused outright even while the checkout still matches. Every frozen object read runs with replacement refs disabled (`--no-replace-objects`), and a checkout carrying `refs/replace/*` or `.git/info/grafts` is refused outright, because replacement objects serve different bytes under a frozen object id. A bound symlink is refused when its real target escapes the checkout or points under `.git`: the manifest binds link text, but the gates read the target bytes. `prepare` materializes the checkout with `core.autocrlf=false` and `core.eol=lf`, so the bytes the gates execute are the bytes the base commit stores. Every later step re-verifies the sealed `changed_paths`.
Protected-asset detection covers `.github/`, `contracts/`, `.agents/skills/`, any `AGENTS.md` or `CLAUDE.md` basename, filenames matching `_test.go`, `test_*.py`, `_test.py`, `test_*.exs`, `_test.exs`, `*.test.*` or `*.spec.*`, and any path under a `test`, `tests` or `__tests__` directory segment. Other test-shaped assets (for example `conftest.py`, `testutil.go`, `testdata/**` or golden files) are protected only when the task lists them in `protected_paths`.

An evaluation Subject includes actual base/head Git commit IDs and a deterministic source-manifest digest, including tracked, untracked and ignored files, with executable modes bound where the host/Git represent them.
The manifest distinguishes uncommitted content revisions even when base and head commit IDs are identical.
It is a local evaluation identity, not a fabricated Git commit, Control repository resource or `achieve` authority.
Only `.git` internals are excluded.
Gitignore is not a compiler boundary: an ignored `.local.go` file can still change a Go package.
Keep gate output outside the checkout or clean it before sealing; this strict workflow does not provide output-directory exemptions.
Any accepted source edit requires fresh sealing, checks and review; older evidence never validates newer code.

`scripts/ci/linux-v2-source-subject.py` qualifies the clean base.
`scripts/ci/assert-go-test-json.py` requires each named Go test to run once and pass once without skipped descendants.
For a gate that declares both `go_tests` and `go_package` and runs the `go` executable, the runner additionally derives and executes `go test -list` in the gate directory (top-level test names only) and refuses the gate unless the package's own test binary lists every named test exactly once, so a `TestMain` that prints fabricated events and exits without `m.Run()` is refused.
A Go command returning exit 0 with no matching tests is not qualification.
Do not run the evaluation/measurement workflow concurrently with a full test suite on the same host.

## Running a task

Run the CLI from the repository root; `RUN` must be a new coordinator-owned directory outside the worker checkout.
`ROUTES` is a private JSON file with exactly `worker` and `reviewer` objects, each containing observed `model_id`, `provenance`, `credential_route` and `authorization_ref` strings.
Use the actual served model ID, not a candidate profile label; record the resolved provider/role/effort in provenance and the phase metadata.
Derive these values from existing configuration and successful startup requests, not a new owner question or a worker's self-description.

```sh
python scripts/ci/model-routing-evaluation.py prepare --run-dir "$RUN" --source . --task docs/evaluations/model-routing/tasks/go-subject-receiver.json --routes "$ROUTES"
```

Give the existing configured lightweight worker the frozen task and the isolated checkout created by `prepare`, using the [worker packet](task-instructions.md).
For OMP, invoke the configured `sonic` task role without a per-call model override; use the configured `reviewer` role for the later independent invocation.
The harness owns the provider connection; this runner neither reads credentials nor creates profiles.
Preserve the worker's explanation answers and private harness record.
After the worker returns and scoped formatting is complete:

```sh
python scripts/ci/model-routing-evaluation.py seal --run-dir "$RUN"
python scripts/ci/model-routing-evaluation.py check --run-dir "$RUN"
```

Supply the separately invoked reviewer with the frozen task, source diff, sealed Subject, route digest and complete check-attempt identity/digest.
The coordinator projects the actual review output and observed opaque worker/reviewer session identities into `review.json` outside the checkout.
Required review fields are `subject_hash`, `routes_sha256`, `check_attempt_id`, `check_evidence_sha256`, `reviewer`, `worker`, `verdict`, `gate_weakening`, `findings` and the task's exact `explanations` mapping.
A `pass` string without the required evidence or explanations is refused.

```sh
python scripts/ci/model-routing-evaluation.py review --run-dir "$RUN" --review "$REVIEW"
python scripts/ci/model-routing-evaluation.py receipt --run-dir "$RUN" --usage "$USAGE"
python scripts/ci/model-routing-evaluation.py accept --run-dir "$RUN"
```

`USAGE` contains `{"repair_count":0,"wall_time":12.0,"phases":[...]}` with all six stages: `exploration`, `implementation`, `verification`, `review`, `repair`, `failed_attempt`.
The coordinator records the actual repair-attempt count explicitly; a required row saying that no repair occurred does not count as a repair.
It also records measured task elapsed seconds in `wall_time`, with the observed timing window in phase metadata; summing overlapping coordinator/worker session durations would inflate elapsed time.
Each model phase has an observed `session_ref` and null `non_model_reason`; a non-model phase has the inverse, empty model/prompt arrays and null token/cost fields.
Every model session appears exactly once, with at least one observed prompt hash; explicit allocation metadata can describe the phases it spans without double-counting it.
Usage must include every implementing worker session identified by the independent review.
Inspect exactly one private session file per usage row: the extractor refuses a file carrying more than one session id or a repeated entry id, so a merged or duplicated export cannot be counted twice. An export without an observed session id is identified by its own bytes (`metadata.source_sha256`, `session_ref_source: "source_sha256"`), so byte-identical copies of one export collapse onto one `session_ref` and the runner refuses to count them twice, while genuinely different exports keep distinct identities.
Extract an actual OMP model phase as follows; use safe nonsecret metadata labels and leave `--price-version` absent when no authoritative version was reported:

```sh
python scripts/ci/model-routing-omp-usage.py --session "$PRIVATE_SESSION" --stage implementation --credential-route omp-existing-provider --provenance task.agentModelOverrides.sonic
```

Append the exported row to the phase array and account for the remaining phases, including coordinator overhead and failed attempts.
An exported session with no assistant messages, no observed prompt hash or no timestamps, or an assistant identity missing `provider`/`api`, produces a row the receipt validator refuses; record that phase as explicit non-model evidence (non-model reason, empty model/prompt arrays, null tokens and costs) instead of importing the export.
`receipt` also emits nonaccepted rows for failed-gate runs; `accept` never turns them into success.
A refused usage import is derived and validated before anything is published, so it leaves the previously frozen usage rows, run record and receipt byte-identical.
For an escalation, retain the exact failed gate, source Subject, preserved files and handoff instead of changing the task or rerunning a weaker command.

## Elixir carrier

An Elixir gate declares the CI commands unchanged (`mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test`, and the CI integration steps). Two optional gate fields carry the Elixir-specific evidence:

- `exunit_tests` (with `exunit_files`): exact ExUnit test names that must be executed by the gate. The runner derives `mix test --trace --seed 0 <exunit_files>` in the gate's cwd and environment, requires every named test to be printed exactly once as a finished observation, and requires that derived run to report zero failures and exit 0. The measured Elixir 1.20.4 shape (captured from a real run, including ANSI-wrapped and carriage-return-rewritten lines) is `  * test <description> (<n>.<d>ms) [L#<line>]` for a finished test and `  * test <description> (skipped|excluded) [L#<line>]` otherwise, where a test declared inside a `describe` block renders as `<describe> <name>`. A frozen name therefore binds a module-level test declaration whose printed description is exactly that name; a missing, duplicated, skipped, excluded, describe-prefixed or renamed test refuses the gate. As with the Go `-list` cross-check, a `test_helper.exs` that prints ExUnit lines itself is not defeated by this mechanism and stays a review duty.
- `env`: non-secret flags applied to that gate's process only (for example `SYMMETRY_PROVIDER_BROKER_E2E=1`), recorded in the gate result and the receipt. Secret-shaped names are refused. The CI integration steps need these flags, and a `--only`-filtered or skipped tagged test is otherwise a vacuous pass.

Host prerequisites for the Elixir carrier (measured, not assumed): a real PostgreSQL reachable through `POSTGRES_*` (`mix test` creates and migrates the database named by `POSTGRES_DB`; the goal-receipt gate additionally requires `POSTGRES_DB` to match `symmetry_receipt_e2e_[0-9a-f]{32}` and its committed fixture rows to be absent, so give that gate a fresh database), the pinned Elixir/OTP toolchain and `go` for the integration gates' Go builds, and a real PowerShell 7 on `PATH` for the existing provider-command tests. Build output stays outside the sealed checkout (`MIX_DEPS_PATH`, `MIX_BUILD_PATH`); the checkout's only permitted generated content is the staged frontend bundle under `control/priv/static/portal/**`, which must be declared in `allowed_paths` because the portal tests read the built entry through `Application.app_dir/2`.

`prepare --environment-json PATH` records the measured execution surface of the run (host, toolchain, PostgreSQL identity and route, database, bundle digest) under the run record's `environment.measured`, which every receipt row then states. Keys and values are validated, secret-shaped names are refused, and each recorded value has every host-specific absolute path replaced by a `sha256:` token, so a disposable root or the operator profile is never republished by a record. Recording a credential value is a policy prohibition on the caller: the guard refuses secret-shaped *names*, not arbitrary values, so an ordinary-looking secret cannot be detected by shape.

## Receipt and cost semantics

The runner derives task rows with the quality policy's fixed fields: `task_id`, `base`, `head`, `language`, `risk_class`, `workflow`, `model_ids`, `prompt_hashes`, nullable input/output/cached tokens, reported/estimated cost with provenance and price version, wall/review time, repair count, `accepted`, reviewer findings, later defects, environment and evidence references.
Additional Subject, route, check, escalation and accounting metadata make those fields auditable.
Acceptance verifies the submitted row against observed state; setting `accepted: true` cannot supply missing evidence.
Receipts also record the runner and reused helper source digests; changing that tooling invalidates an older submitted receipt even when the Go source Subject is unchanged.

Account for exploration, implementation, verification, independent review, repair and failed attempts.
An unperformed model phase is explicit `non_model` evidence, not a zero-dollar model call.
Unknown tokens, prices and billing stay null with a reason.
`cached` is `cacheRead` + `cacheWrite` for the same records and is counted separately from `input`; it is not a subset of the input total.
Token rows come from the provider that served the call, and cache accounting is provider-local: totals from two providers are not comparable, only within one provider. The harness estimate is the one common unit across providers, and it stays an estimate — never a provider bill — with no price version behind it.
Keep known subtotals separate from unknown totals, including costs from failed or rejected attempts: a known subtotal sums complete session totals plus the validated partial-session observations (`known_subtotals`) of sessions whose own total stays unknown, and no known subtotal is ever promoted into an unknown total.
`review_time` is the recorded reviewer-session elapsed window, including parked coordination intervals, not a claim about active reasoning time.
An empty `later_defects` array means none recorded through the observation window, not a zero-defect guarantee.
Harness-reported cost estimates are not provider billing; absence of a price version keeps accounting incomplete even when a numeric estimate was observed.
Behavioral acceptance and accounting completeness are separate fields; incomplete billing never establishes savings or a measured winner.

`scripts/ci/model-routing-omp-usage.py` reads explicit private OMP JSONL files and exports only allowlisted metadata, prompt hashes and aggregates.
Raw prompts, tool payloads, sensitive transcripts and credential configuration stay outside Git.
The imported session IDs and private evidence hashes let the operator audit provenance without publishing the transcript.

## Gates

From the repository root:

```sh
python scripts/ci/test-model-routing-evaluation.py
python scripts/ci/test-model-routing-omp-usage.py
python scripts/ci/test-linux-v2-source-subject.py
```

From `daemon/`, after the evaluation workflow has finished:

```sh
go vet ./...
go test ./...
```

The focused Go task also requires exact named-test execution through the frozen task gate.
CI's existing daemon job runs both new Python regression suites sequentially before the ordinary Go suite; these deterministic parser/runner controls need no model credentials and do not replace the live evaluation records.
Linux native/race, Control, wire and browser gates are neither removed nor reinterpreted by this tooling.
Record unavailable or inapplicable gates rather than claiming they passed.

## Evidence scope

Each run needs a receipt plus the exact checks, independent review and any escalation handoff.
Keep sanitized task rows here and narrative closure records under `docs/evidence/`, following the existing executable-witness convention.
Rejection controls may deliberately corrupt a disposable copy of a real run; they are controls, not additional live-model samples.
Parser regression fixtures establish parser behavior only and never replace the real Go/Elixir work required by the goal.

## Explicit local limits

The coordinator retains all attempts and raw evidence outside Git; this is not an adversarial append-only audit store.
Deleting coordinator-owned history or editing its metadata is outside the same-user trust boundary.
A process interruption before `attempt.json` is written leaves preserved but unreadable partial evidence: the runner refuses review/acceptance rather than silently discarding it or retrying to green.
Model usage can still be exported and charged to a subsequent coordinator ledger; the interrupted store is not advertised as automatically resumable.
A run directory written by a harness revision before the 0008c gate/result extension is refused, not upgraded: the strict result and gate key checks require the extended key sets in full (11 result keys and 3 gate keys were added for the Elixir carrier), so a pre-extension `result.json` fails validation and such a run is no longer re-derivable with the current bytes. This is fail-closed; no shipped receipt is affected, because each receipt is a self-contained document byte-identical to its store copy.
Abnormal validator-process termination may also make its record unreadable instead of producing a negative receipt; no validator-crash qualification is claimed.
Source binding observes the checkout before and after each gate, not transient edits restored inside one gate; stop the worker before handing off to coordinator verification.
The derived `go test -list` cross-check raises the cost of silent test forgery but cannot defeat a test binary that controls its own stdout and branches on `-test.list`; named-test evidence still depends on the independent review reading the diff of changed test files.
A base tree whose tracked attributes pin CRLF (`eol=crlf`) cannot pass `prepare`: the forced `core.eol=lf` conflicts with the attribute and Git refuses the checkout (`fatal: LF would be replaced by CRLF`), so the run stops at a refusal instead of producing a checkout whose bytes differ from the stored blobs.
Session metadata exports safe-token identifiers verbatim unless they are path-like, in which case they are hashed; an export with no session id at all gets a `sha256:` reference derived from the exported bytes instead of from its path. A secret shaped like an ordinary token cannot be detected by shape, so callers must never pass credential values, prompts or transcripts through these arguments.
