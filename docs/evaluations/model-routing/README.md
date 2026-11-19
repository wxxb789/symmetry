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
The 0008 two-arm class evaluation adds [go-boundary-validator-coverage](tasks/0008b-go-boundary-validator-coverage.json) (established-pattern boundary-validator regression coverage) and [go-close-owned-settle](tasks/0008b-go-close-owned-settle.json) (Windows containment ownership settlement), each run once per workflow arm against the same frozen bytes.
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

A TypeScript run also records its resolved dependency tree once, after the shared hoisted install
exists and before the first gate runs: `bind-dependencies --run-dir "$RUN" --tree
"$STORE/runs/node_modules" --log "$INSTALL_LOG"` (the carrier section describes what it binds).
`check` and `receipt` refuse a TypeScript task, or one whose gates declare `vitest_tests`, until
that step is recorded.

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

`prepare --environment-json PATH` records the measured execution surface of the run (host, toolchain, PostgreSQL identity and route, database, bundle digest) under the run record's `environment.measured`, which every receipt row then states. Keys and values are validated, secret-shaped names are refused, and each recorded value has every host-specific absolute path these patterns recognise — drive-rooted and UNC Windows paths, and POSIX home, root and temp roots — replaced by a `sha256:` token, so from this revision onward a record does not republish a disposable root or the operator profile. The four committed `0008c` receipts predated the redaction; they were re-rendered on 2026-11-19 under the one-time owner-authorized exception below, with every digest that binds them recomputed and rebound. Recording a credential value is a policy prohibition on the caller: the guard refuses secret-shaped *names*, not arbitrary values, so an ordinary-looking secret cannot be detected by shape.

## TypeScript carrier

A task whose language is `typescript` declares the frontend's own commands, but as direct tool
invocations rather than pnpm script aliases, because the sealed checkout deliberately carries no
`node_modules`: the pnpm workspace's dependencies are installed once, hoisted, one level above
every run directory (`<store>/runs/node_modules` for runs at `<store>/runs/<name>`), and Node
resolves them by walking the ancestor `node_modules` chain. From a gate cwd of `frontend/` the
entries are `node ../../../node_modules/...`; from the repository root they are
`node ../../node_modules/...`. The measured entries (pnpm 12.17.0 workspace, Node v26.13.1,
vitest 5.1.0, vite 8.3.6, eslint 10.14.0 with typescript-eslint 8.72.1, TypeScript 7.0.2 through
the frontend's `@typescript/native` alias for `typecheck` and the unaliased root `typescript` for
`contracts/tsconfig.json`, `@playwright/test` 1.64.1) are:

- `contracts-typecheck`: `node ../../node_modules/typescript/bin/tsc --project contracts/tsconfig.json`
- `contracts-generated-drift` / `contracts-effect-drift`: `node contracts/scripts/check.mjs` and
  `node contracts/scripts/check-effect.mjs`
- `contracts-tests`: `node --test contracts/scripts/` (the repository's own
  `node --test contracts/scripts/*.test.mjs` with the same top-level file set)
- `frontend-typecheck`: `node ../../../node_modules/@typescript/native/bin/tsc --noEmit`
- `frontend-lint`: `node ../../../node_modules/eslint/bin/eslint.js . --max-warnings 0`
- `frontend-tests`: `node ../../../node_modules/vitest/vitest.mjs run`
- `frontend-build`, `frontend-stage-assets`: `node ../../../node_modules/vite/bin/vite.js build`
  and then `node scripts/stage-assets.mjs` (the two steps of the frontend's own `build` script)
- `browser-acceptance`:
  `node ../../../node_modules/@playwright/test/cli.js test tests/workspace-shell.spec.mjs
  tests/portal-execution-control.spec.mjs`, with the frozen `env` naming the run's own Control
  dev server (`SYMMETRY_PORTAL_URL`) and its expiry instance (`SYMMETRY_SHELL_EXPIRY_URL`) so the
  two session-expiry cases run instead of skipping

One optional gate field carries the TypeScript-specific evidence:

- `vitest_tests` (with `vitest_files`): exact module-level `it(...)` names. After a passing gate
  command the runner derives
  `vitest run --reporter=json --outputFile <gate dir>/vitest-report.json <vitest_files>` in the
  gate's cwd and environment and parses that structured document instead of printed text. Every
  named test must appear exactly once with `status: "passed"` and an empty `ancestorTitles` array
  (so a frozen name binds one top-level declaration), the report's `numTotalTests` must be at
  least the named-test count, `numFailedTests` must be 0 and `success` must be true; a missing,
  empty, unparsable, summary-less or failing report fails closed, and a named test reported twice,
  renamed, or hidden inside a `describe` block is refused. The measured shape is vitest's own JSON
  reporter document: `numTotalTestSuites`, `numTotalTests`, `numPassedTests`, `numFailedTests`,
  `success` and `testResults[].assertionResults[]` entries carrying `fullName`, `title`, `status`
  and `ancestorTitles` (a module-level test has `fullName == title` and no ancestor titles). The
  gate command itself must run the vitest entry point (`node .../vitest.mjs`), so the derived run
  reuses the frozen program rather than an arbitrary one.

Host prerequisites for the TypeScript carrier (measured, not assumed): the shared hoisted install
above, whose provisioning lockfile copy substitutes the unreachable `registry.npmjs.org` host with
the configured package feed while every package is still verified against the frozen lockfile's
own integrity value; the tree is bound per run: `bind-dependencies --run-dir "$RUN" --tree "$STORE/runs/node_modules"
--log "$INSTALL_LOG"` copies the captured install log into the run store as `provisioning/install.log`,
records the run-relative tree route and a deterministic digest over every file and link of the
resolved tree - a link whose target resolves outside the tree (pnpm's workspace links such as
`@symmetry/contracts -> ../../contracts`) contributes that target's own content digest, because the
gates execute it - and every receipt derived from the run states the block under
`environment.provisioning`, which `--check` and every later load recompute and refuse on drift; the
log's source route keeps the measured surface's host-path redaction, while the tree route stays
run-relative so the digest remains recomputable from the stored run directory. `check` and `receipt`
refuse a `typescript` task, or one whose gates declare `vitest_tests`, until that step is recorded.
The coordinator runs the step after provisioning and before the gates; a real PostgreSQL instance and a Phoenix dev server started from the run's
own sealed checkout (ports 4040/4041) with `MIX_DEPS_PATH`/`MIX_BUILD_PATH` outside the checkout;
Chromium through Playwright's own browser cache; and the pre-staged generated trees. Because the
seal refuses generated content that appears after it, the task must list both generated trees in
`allowed_paths` (`frontend/dist/**` and `control/priv/static/portal/**`) and the coordinator stages
them before the seal; the frozen build gates then reproduce them byte for byte (measured:
`vite build` plus `stage-assets.mjs` produced the same three-entry tree digest
`sha256:ca2cd8873b4acd89cef75c3400e95d96eb8070a4c55e15167540d78dd6b4c2e6` on two consecutive runs,
so the per-gate subject check still holds). The html reporter would write `playwright-report/` into the gate's cwd, which would
change the sealed revision during the gate; the coordinator exports `PLAYWRIGHT_HTML_OUTPUT_DIR`
outside the checkout while running `check`, so the repository's own command and test selection
stay unchanged and the report set lands next to the run as `rendered-report/`. Playwright's
per-test captures and attachments land under `SYMMETRY_PLAYWRIGHT_OUTPUT` (or the platform temp
directory by default) and are copied into the run store as `rendered/` after the check.

Tool run state is redirected, not excluded from the manifest: `frontend/vite.config.ts` and
`frontend/vitest.config.ts` resolve `SYMMETRY_TOOL_CACHE_DIR` as Vite's `cacheDir` and vitest's
`fsModuleCachePath`, `check` exports that variable for every gate as
`<run>/tool-state/<attempt>/<gate id>` (a fresh directory per attempt), and the coordinator exports
it for the worker's own runs (for example as `<run>/tool-state/worker`).
`prepare` gives the run store an empty `node_modules` directory, so Vite's config-loader temp files
(`.vite-temp`, written next to the nearest existing `node_modules`) land in the run store rather
than inside the checkout or the shared dependency tree; an empty directory changes no module
resolution because Node keeps walking ancestors past it. Redirection alone does not close the
boundary: vitest's results cache and transformed-module cache execute code written by an earlier
invocation, so a gate run also exports `SYMMETRY_DISABLE_TOOL_CACHES=1`, which
`frontend/vitest.config.ts` reads as its test-level `cache: false` and as `fsModuleCache: false`, and
the fresh per-attempt directory means nothing a previous attempt wrote can be read. Worker runs keep
their caches (the variable is the harness's to set), and run state a worker leaves inside the
checkout is bound by the seal. A gate that still writes
`node_modules/.vite*` run state into the checkout changes the sealed revision and fails closed,
which is what replaced the old `node_modules/.vite`, `.vite-temp` and `.vitest-cache` manifest
exclusions.

## Two-arm class evaluation (0008b)

A class evaluation runs one frozen task under two workflow arms and derives one receipt per arm. The arms are intended to differ only in which configured route does the work; as run, the 0008b mixed arm additionally carries one coordinator-caused reviewer invocation that was discarded and charged as its `failed_attempt` phase (published with an explicit verdict and reason in `0008b-go-bounded-regression-mixed.review-attempts.json`):

- **mixed arm**: the configured lightweight worker route implements; the configured strong route verifies and reviews in a separate invocation.
- **strong-only arm**: the configured strong route implements, and the same configured strong route reviews in a separate invocation. That is invocation separation with one model family, not model diversity.

Both arms get the same frozen task bytes, the same allowed and protected paths, the same gate argv, the same approval mode and wall-clock budget, and the same worker packet text; the packet differs only in the workspace path it names. Packets and budgets are coordinator-private and not exported, so the freeze is a coordinator observation, not a published artifact. Reviewer packets are necessarily not identical across arms: each embeds that arm's own sealed subject, checkpoint and gate evidence, and the candidate policy's `limitations` states that prompt hashes are not comparable across arms. External route labels and served model identities come from configuration and observed sessions, never from a worker's self-description.

A rejected review is not acceptance evidence: when the independent reviewer returns `fail` with open findings, the arm keeps the failing revision and its review in the private store, the findings go back to the implementing route as failing evidence in one bounded repair round, and the repaired revision needs a fresh seal, gate execution and a fresh reviewer invocation. The failing review session is charged in the run's usage ledger, and the repair round is the run's recorded `repair_count`.

Every discarded or superseded reviewer invocation is published as an attempt record with an explicit verdict and reason, not left to coordinator prose: the class B arms publish `<class>-<arm>.review-attempts.json`, and the class A mixed arm publishes its discarded first review the same way. The candidate policy binds each published attempt record with `review_attempts_ref` and `review_attempts_sha256`.

Seal the class list and both task files before the first arm starts, and publish the frozen digests with the receipts. Per-class results live in this directory as `<class>-<arm>.json` receipts, their review records, the derived `0008b-policy.json` candidate record and `0008b-controls.json`; the narrative closure record stays under `docs/evidence/`.

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
python scripts/ci/test-model-routing-record-sanitize.py
python scripts/ci/test-check-record-cleanliness.py
python scripts/ci/test-model-routing-source-subject.py
python scripts/ci/test-linux-v2-source-subject.py
```

From `daemon/`, after the evaluation workflow has finished:

```sh
go vet ./...
go test ./...
```

The focused Go task also requires exact named-test execution through the frozen task gate.
CI's existing daemon job runs the Python regression suites (evaluation runner, OMP usage extraction, record path sanitisation, Linux v2 contracts) sequentially before the ordinary Go suite; these deterministic parser/runner controls need no model credentials and do not replace the live evaluation records.
A separate `record-cleanliness` CI job runs `scripts/ci/check-record-cleanliness.py` whenever a change adds, modifies or retypes a file under `docs/evaluations/**` or `docs/evidence/**`, or changes the sanitize tool itself: the script selects that change's records against the pull-request base commit (or the pushed `before` commit; an all-zero base falls back to the `origin/main` merge base) and runs `model-routing-record-sanitize.py --check --root-only` on each one, so the guard executes the tool's own check instead of re-implementing its patterns. The job is independent of the documentation-only fast path - a records-only pull request is exactly the change it must see - and unchanged committed records are deliberately out of scope while their own TODO entries remain open.
Linux native/race, Control, wire and browser gates are neither removed nor reinterpreted by this tooling.
Record unavailable or inapplicable gates rather than claiming they passed.

## Evidence scope

Each run needs a receipt plus the exact checks, independent review and any escalation handoff.
Keep sanitized task rows here and narrative closure records under `docs/evidence/`, following the existing executable-witness convention.
When capturing argv or output into a published record, render host-specific absolute paths with `scripts/ci/model-routing-record-sanitize.py` (a disposable root becomes the `<disposable-root>` token, other absolute paths become `path-sha256:<digest>`), and record the rendering in the record as `0008b-controls.json` does in its `record_rendering` block. For the 2026-11-19 one-time re-publication of the older 0008 records, the series-level record of the rendering is the [record rendering exception](#record-rendering-exception-2026-11-19) section below, not new `record_rendering` blocks in those records.
The renderer's default scope is every absolute path, which its docstring names as the argv/output boundary. `--root-only` narrows rendering and `--check` to host absolute paths - Windows drive and UNC roots and the POSIX `/home`, `/Users`, `/root`, `/tmp`, `/var`, `/mnt` and `/private` roots - plus every `--root` disposable root, so a product route such as `/portal/runtimes` stays literal: `--check --root-only` exits 0 for every committed `0008d-*` record, while a host path in recorded prose is still caught. Records published from argv/output capture use the default scope; prose fixtures that are meant to keep a literal route use the root-only scope.
Rejection controls may deliberately corrupt a disposable copy of a real run; they are controls, not additional live-model samples.
Parser regression fixtures establish parser behavior only and never replace the real Go/Elixir work required by the goal.
The 0008b class evidence is [go-bounded-regression mixed](0008b-go-bounded-regression-mixed.json) and [strong-only](0008b-go-bounded-regression-strong.json), [go-native-ownership-settlement mixed](0008b-go-native-ownership-settlement-mixed.json) and [strong-only](0008b-go-native-ownership-settlement-strong.json), their review records, the [candidate policy record](0008b-policy.json) and the [refusal controls](0008b-controls.json); the narrative closure record is [`docs/evidence/goal-0008b-go-model-routing-2026-11-17.md`](../../evidence/goal-0008b-go-model-routing-2026-11-17.md).
The 0008d class evidence is [ts-runtime-capability-rows mixed](0008d-a-mixed.json) and [strong-only](0008d-a-strong.json), [ts-command-receipt-truth mixed](0008d-b-mixed.json) and [strong-only](0008d-b-strong.json), their four review records, the [candidate policy record](0008d-candidate-policy.json) with its [human rendering](0008d-candidate-policy.md); the narrative closure record is [`docs/evidence/goal-0008d-typescript-model-routing-2026-11-17.md`](../../evidence/goal-0008d-typescript-model-routing-2026-11-17.md) and the series-closing [`docs/evidence/goal-0008d-series-release-receipt-2026-11-17.md`](../../evidence/goal-0008d-series-release-receipt-2026-11-17.md) binds the letter's Subject and maps every clause of the 0008a acceptance contract to its evidence with the revision each row came from.

## Record rendering exception (2026-11-19)

The owner authorized, on 2026-11-19, a one-time and recorded exception to the rule that published records are not rewritten; this section records the exception, the rebound digests and the one-time reference-binding audit that followed it. The host absolute paths still carried by the published 0008 records were rendered with `scripts/ci/model-routing-record-sanitize.py` at its origin/main bytes (`sha256:b8b252f3c7491b5c69317e2f42f1a0772ce5e8a965477964bfd06693e7d2c973`, the same bytes `0008b-controls.json` records) in its argv/output scope, and every digest that binds a changed file was recomputed and rebound. The operator's disposable run store renders as `<disposable-root>` with its trailing structure kept; other host paths render as deterministic `path-sha256:<digest>` tokens. Only host paths were rendered: the sanitizer's patterns also match repo-relative prose fixtures (a `.gitignore` pattern, model route names, fragments such as `/ExUnit`), and those were deliberately left unrendered and are listed below.

| File changed | Host-path substitutions | `sha256` before | `sha256` after |
| --- | --- | --- | --- |
| `docs/evaluations/model-routing/0008a-controls.json` | 32 — 14 disposable-root argv/output strings to `<disposable-root>`; 9 toolchain interpreter argv entries (rendered to `path-sha256:4d144641…`) and 9 operator-worktree argv entries to `path-sha256:`; plus one rebound rendered-output digest below | `171f1959d56489b632885671315d5a543fb4301d6eaa3dd40fbbd4e3a6f87b5d` | `8be656d18b35f0b4267aad5ae1045ee2b7bff41920b0fc984b58ef275a9f97d5` |
| `docs/evaluations/model-routing/0008a-audit.json` | 5 — 3 disposable-root argv values; 2 toolchain argv entries | `dc87809933912acd1ca5c11bed741cb3f0245058cd6c2f17222f0f313a4cf040` | `832117d0828032ebf1dc731d156bc7ed378ac16e61d41a741b8d4db12b5fa318` |
| `docs/evidence/goal-0008a-auditable-acceptance-workflow-2026-11-15.md` | 1 — `EVIDENCE_ROOT`; plus the quoted audit digest below | `d123975190cb5729d4f39bbe516abbf9934279078d50bda27e1f8325722e046b` | `534e325d8532473c7e22bf9487cc19ca21b460603ea4358118ba704e9813c57b` |
| `docs/evaluations/model-routing/0008c-a-mixed.json` | 2 — `mix_build_path`, `mix_deps_path` | `9432545d54eca14346e24a42c37821915dc663e0bd5c6b89e36571e242a01e31` | `93050ced0667d012924564e58032f96fd3244214ac68871e3f552750cc5fa73c` |
| `docs/evaluations/model-routing/0008c-a-strong.json` | 2 | `26d8e003419e3e985e29b5a065b8ce4d178a8402c276855a39fdab6998040d7b` | `5ee9b87bd6d5b26adb4674b1024f8a212e48e6050223703c43e8e3c766f86a7f` |
| `docs/evaluations/model-routing/0008c-b-mixed.json` | 2 | `e79fe17327fe756f2af5580e61a64404c8a6a1b3d8bac4ed3bf15db48a078e40` | `ceae34ce3507075c4fe26dc719fa8d3caff8b98b4817b576a4f63798a6e6f74a` |
| `docs/evaluations/model-routing/0008c-b-strong.json` | 2 | `1be7b41985540705653c763f57cfe98f31b219a077f4812a1afe04747b863bf7` | `af12819d6806509f78c0379f561fd3d8c01e5f0a3116335cd362d9c55918f34a` |
| `docs/evidence/goal-0008c-elixir-model-routing-2026-11-17.md` | 1 — the private evidence root; plus 4 quoted receipt digests below | `84f822ed7358918c9d99b7e69c58d12e38180fd2dc7ec9baa70a1e8b8c8fc8ba` | `21bf406fc7375e9b6934885ca851375de147c406650a31804b40a898675bfb1f` |
| `docs/evaluations/model-routing/0008c-candidate-policy.md` | 4 quoted receipt digests below | `46d97e02bbce74e3edc05788206ad8ee145c12bd7f06f9e196c6e274a4bd3759` | `f7fb3835a5e74b4418224aae580052a1bd05ea9abdc7a9f7f7aaf129432654e6` |

Digest cascade: the four 0008c receipt digests are quoted twice each — in `0008c-candidate-policy.md` and in the 0008c evidence record (`9432545d…`/`26d8e003…`/`e79fe173…`/`1be7b419…` → `93050ced…`/`5ee9b87b…`/`ceae34ce…`/`af12819d…`) — and the 0008a audit digest quoted by the 0008a evidence record was rebound (`dc878099…` → `832117d0…`). One rendered-output digest needed a second-generation rebind: `0008a-controls.json`'s `missing-gate` control had its `stderr` rendered while `stderr_sha256` still bound the pre-render text, so the digest was rebound to the rendered text it now describes (`c12881ae…` → `8503bfa2…`) and that file's `sha256` above is its post-rebind value. A companion check walked every embedded output/digest pair (`X` beside `X_sha256`) in the touched records — 18 pairs, 0 mismatches — and across the other model-routing records only `0008b-controls.json` differs, by line-ending representation (its 9 `output_sha256` values bound the captured CRLF bytes while the embedded `output` text uses LF; untouched by this exception). A scan of every tracked file under `docs/`, `scripts/`, `contracts/` and `.github/` finds no full-length occurrence of any replaced digest outside this exception record (which carries the full before/after values by design); the truncated `…` prefixes that remain are the ones in this section and in the CLOSED-TODO closures (`9432545d…`/`26d8e003…`/`e79fe173…`/`1be7b419…`/`dc878099…`).

Store-copy note: the published `0008c` receipts diverge from their run-store copies at the two rendered `environment.measured` fields, so `accept --receipt <published>` refuses against the unchanged store copy; re-verification is against that store copy.

Left unrendered by design (prose, not host paths): in `0008c-a-mixed.json` the fragments `/\` (three prose separators between quoted statuses), `/control/priv/static/portal/` and `/portal/assets/\` (a reviewer's `.gitignore:77` reference); in `0008c-b-strong.json` `/sync_status`, `/unchanged` and `/control/priv/static/portal/.`; in the 0008c evidence record `/ExUnit` (in "fake-`mix`/ExUnit tests") and the route names `/tencent-litellm/openai-completions` and `/ghc/openai-responses`. Rendering these would corrupt the prose they belong to. Three records that carry no host path were left untouched and still trip `--check` on such fragments only: `0008a-escalation.json` (`//go`), `0008c-a-mixed-review.json`, `0008c-b-strong-review.json`. `--check` exits 0 on `0008a-controls.json`, `0008a-audit.json`, the 0008a evidence record, `0008c-a-strong.json` and `0008c-b-mixed.json` (and on the re-bound candidate policy, which needed no rendering).

Historical quotes deliberately not rewritten (their meaning depends on the revision or state they describe): the 0008a audit's F12 block and the 0008a evidence record's rebinding narrative quote pre-rebase digests and `git show c45db2c`/`1baf76c`/`e4a91c1`/`bdbcdc7`/`abf8210`/`993870e`/`4644fd7` references; `docs/archive/CLOSED-TODO.md`'s F12 closure keeps the truncated `sha256:dc878099…` it recorded at that closure; and `docs/evidence/goal-0008d-series-release-receipt-2026-11-17.md` records that raw host paths shipped as of 2026-11-17 in two places — its numbered item 3 and its 0008c summary table row ("Unresolved: the four committed receipts carry raw host paths (TODO: re-publish through `model-routing-record-sanitize.py`)").

### One-time reference-binding audit (2026-11-19)

The scrub was followed by a one-time audit (owner decision: one-time audit, no CI gate). A throwaway script parsed every model-routing record and every 0008-series evidence document for quoted `git show <commit>:<path>`/`<commit>:<path>` bindings and manifest digests, and verified them against the revisions they name: four verifiable commit:path bindings (all resolve, are reachable from `origin/main`, and exist at the named commit — `343dea6a` and `1c6c715` for the 0008a audit and `dacec45` for the two 0008d bindings), plus method/placeholder and sealed-checkout `HEAD:` references classified unverifiable; the 0008a source manifest re-derives in full at `1c6c715` (17/17 entries, bound tree `54fde273…`, canonical scoped digest `9dc95d2f…`); the 0008d landing-tip source subject recomputes at `73e083f` (1141 paths, `70e03853…`) with the delivered `scripts/ci/model-routing-source-subject.py`; 42 tooling-source digests re-derive at their letters' tagged/delivered revisions (`253a701` for 0008b, `cab222b` for 0008c, `73e083f` for 0008d, and the unreachable-but-GitHub-served `4644fd7` for 0008a); every receipt `frozen_review_sha256`/`evidence_sha256`/`frozen_task_sha256` field matches its sibling review or task file; a companion check walked every embedded output/digest pair (`X` beside `X_sha256`) in the touched records — 18 pairs, 0 mismatches after the `missing-gate` rebind above; and 175 `sha256:` tokens in scope match a current tracked-file digest. Final run: 61 bindings passed, 0 mismatches, 0 unresolved; 28 values classified unverifiable (sealed private revisions, sealed-checkout `HEAD:`/placeholder references, and the unreachable-from-refs commit inventory below).

Unreachable-from-refs commits referenced by the records (47 merged branches were deleted on 2026-11-17/18): `c45db2c` and `1baf76c` are GitHub-absent — their pre-rebase bytes exist only as digest history in F12, and the delivered equivalent (`1c6c715`) is named there, so no rebinding to a main commit is possible; `4644fd7`, `993870e`, `abf8210`, `bdbcdc7` and `a9f4d93` are unreachable from local refs but still served by `gh api repos/wxxb789/symmetry/commits/<sha>`; `cab222b` is preserved via the tag `0008c-produced-tooling`. No live binding was broken, so no binding needed rebinding beyond the digest cascade above.

## Explicit local limits

The coordinator retains all attempts and raw evidence outside Git; this is not an adversarial append-only audit store.
Deleting coordinator-owned history or editing its metadata is outside the same-user trust boundary.
A process interruption before `attempt.json` is written leaves preserved but unreadable partial evidence: the runner refuses review/acceptance rather than silently discarding it or retrying to green.
Model usage can still be exported and charged to a subsequent coordinator ledger; the interrupted store is not advertised as automatically resumable.
A run directory written by a harness revision before the 0008c gate/result extension is refused, not upgraded: the strict result and gate key checks require the extended key sets in full (11 result keys and 3 gate keys were added for the Elixir carrier), so a pre-extension `result.json` fails validation and such a run is no longer re-derivable with the current bytes. This is fail-closed; no shipped receipt is otherwise affected, because each receipt is a self-contained document byte-identical to its store copy — except the four `0008c` receipts re-rendered on 2026-11-19 (see the exception section), which diverge from their store copies at the two rendered `environment.measured` fields, so `accept --receipt <published>` refuses for them and re-verification is against the store copy.
Abnormal validator-process termination may also make its record unreadable instead of producing a negative receipt; no validator-crash qualification is claimed.
Source binding observes the checkout before and after each gate, not transient edits restored inside one gate; stop the worker before handing off to coordinator verification.
The derived `go test -list` cross-check raises the cost of silent test forgery but cannot defeat a test binary that controls its own stdout and branches on `-test.list`; named-test evidence still depends on the independent review reading the diff of changed test files.
A base tree whose tracked attributes pin CRLF (`eol=crlf`) cannot pass `prepare`: the forced `core.eol=lf` conflicts with the attribute and Git refuses the checkout (`fatal: LF would be replaced by CRLF`), so the run stops at a refusal instead of producing a checkout whose bytes differ from the stored blobs.
Session metadata exports safe-token identifiers verbatim unless they are path-like, in which case they are hashed; an export with no session id at all gets a `sha256:` reference derived from the exported bytes instead of from its path. A secret shaped like an ordinary token cannot be detected by shape, so callers must never pass credential values, prompts or transcripts through these arguments.
