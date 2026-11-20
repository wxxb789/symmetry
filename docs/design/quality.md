# Model-independent acceptance and development policy

Normative target. Cheap generation is useful only if accepted-change total cost
improves without reducing acceptance standards. No model or prompt guarantees
zero defects or absolute equivalence on unseen tasks.

## Routing decision

| Task | Default executor | Acceptance |
| --- | --- | --- |
| Locate code, summarize exact source, bounded established-pattern edit | Lightweight configured model | Relevant executable checks and evidence review |
| New domain invariant, DB concurrency, authority, recovery, protocol migration | Strong reasoning model designs; bounded implementation may use light model | Independent semantic review plus failure-oriented verification |
| CSS/component change within established design | Lightweight model | Rendered interaction/accessibility checks; owner visual review where qualitative |
| Ambiguous requirement or failed contract assumption | Strong model/owner resolves exact conflict | Contract amendment before affected implementation |

`gpt-5.6-luna` and `deepseek-v4-flash` are candidate profile labels supplied by
the owner, not guaranteed provider IDs, available endpoints or measured winners.
Resolve actual local IDs in configuration and record them in evaluation receipts.
Do not hardcode model names into domain policy or assume their relative quality.

Do not use model confidence as a routing signal. Escalate when a required
invariant cannot be explained/tested, the diff crosses its contract, failures
recur without a changed diagnosis, or the worker proposes weakening a guard.
Keep already-correct work; supply the escalation with failing evidence, not the
entire noisy transcript. No arbitrary retry-to-green loop.

## Before implementation

Record the intended behavior, exact relevant contract, affected state owner,
baseline revision and required evidence. Acceptance predicates come from the
owner/design before implementation. New regression tests may be written by the
worker, but they cannot be the only evidence for the behavior they invented.
For consequential changes, reviewer independently reads requirement and diff.
Independent review can be a separately invoked local Codex session; no automatic
multi-agent orchestration is required by these files.

## Commands and gates

The current CI has Elixir tests/release, Linux Go vet/tests/race tests, Windows
tests, the wire-contract gate and daemon/control restart E2E. Preserve those
gates. Frontend typecheck, lint, test and build exist since goal 0007 delivered
`frontend/`; run them when a change touches it.

Run CI for pull requests, pushes to `main`, tags, and manual dispatch; do not
duplicate the full matrix on feature-branch pushes. PR evidence binds the tested
merge revision, not the unmerged feature head. Branches without a PR and
conflicted PRs have no automatic feature-head run. The Control job owns the
canonical formatter gate. Platform-specific compilation and E2E are retained:
the Linux Pi acceptance steps run inside the Control job, which shares their
platform and Postgres service, while the Windows Pi acceptance keeps its own
job.

Amendment (2026-11-17, owner-directed CI consolidation). Two structural
statements above are superseded while every gate they protect is retained: the
Linux Pi acceptance steps moved from their own job into Control (same runner
image, same database service, one compilation of the test environment instead of
two), and the ordinary Linux Go pass is now scoped to the two packages that
carry non-race assertions while the `-race` pass remains the full-suite pass.
Both passes remain for an empirical reason, not a stylistic one: at `671525c`
the ordinary pass succeeded on a revision where the race pass failed in
`internal/harness/omp`, so neither shape may replace the other. Folding never
masks a gate: every folded step is `if: always()`, carries its own outcome, and a
final assertion step fails the job for any folded outcome that is not success.

Ordinary Linux user/PID namespace fixtures still run as the original
unprivileged runner UID. On disposable GitHub-hosted runners, preparation may
attach a fresh per-program AppArmor `userns` exception to a private copy of the
system `unshare`; it must not disable global restrictions or run Go/tests with
`sudo`. This exception is not a sandbox and does not qualify a native owner.
Installed-owner host prerequisites must be proved in the same protected manual
native-acceptance job as their consumers, not on a separate automatic PR runner.
A skipped qualification job is unavailable evidence, never a passed predicate.

Ordinary daemon Go test commands inherit only stdio from their command caller.
Establish that descriptor boundary before Go starts, not inside a native census fixture.
Descriptors subsequently owned by a test remain subject to the unchanged
production census, including unexpected FD rejection. This isolates descriptor
inheritance only; it does not qualify a host or identify an upstream producer.

Control/Pi CI selects the production missing-owner refusal on Linux and retains
the five Windows positive integration scenarios. Test-only capability projection
for admission does not establish installed-owner qualification. Linux-specific
provider progress, crash/candidate handling, handoff and resume still need their
own installed-owner evidence; generic Control state tests cannot replace it.
Finite witness failure diagnostics are separate from acceptance summaries and
cannot satisfy missing receipts or turn a failed witness into a passing run.
Failed witness runs may print raw Go test events and upload a short-retention
diagnostic artifact; neither is acceptance evidence.
Runtime-registration observations retain the last successful Portal projection;
`portal_unavailable` means no valid snapshot was obtained, not runtime absence.
Helper-crash cancellation diagnostics classify one failed request without retry.
An HTTP 409 client fallback is not a decoded server-code proof.
`cancel_api_409_failed_run` additionally requires one bounded failed-Task readback
matching the original run/generation.
Legacy helper-scan crash phases identify only the entered assertion boundary;
mirror-wait labels identify the last entered predicate guard, not takeover proof.
Initialized evidence with absent snapshots remains incomplete.
Rootless native fixtures must load through the production config validator and
reuse a real repository resource binding. Public pinned assets are mounted
read-only and readable by the container UID; executables retain execute access.
Pi requires its complete release for relative version metadata and runtime assets,
not a standalone executable mount. Private extraction parents and state stay
private; archive extraction under umask 077 alone does not preserve cross-UID
access. Native service images explicitly provide a UID-private writable HOME;
OpenCode initializes XDG directories even for version and help probes.
Owner-request barrier paths bind the container-visible mount namespace, not the
host extraction path; fresh-directory ownership and mode guards remain.
The response-hold fixture decodes gzip session-registration observations without
changing the forwarded response status, headers or body.

| Surface | Actual commands / target additions |
| --- | --- |
| Elixir (`control/`) | `mix format --check-formatted`; `mix compile --warnings-as-errors`; `mix test` (four portal cases require the staged frontend bundle: `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build`, or CI's `portal-assets` artifact — see the [portal runbook](../runbooks/portal.md)); release/E2E per current CI |
| Go (`daemon/`) | `go vet ./...`; `go test ./...`; concurrency changes: `go test -race ./...` |
| Browser (`browser/`) | `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile` from the repository root; `mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test`; configured control/daemon fixtures required |
| TS (`frontend/`) | frontend typecheck/lint/test/build commands from frontend.md; CI runs them in the `contracts` job (`.github/workflows/ci.yml:107-110`); the browser runner is already pnpm (`.github/workflows/ci.yml:618`) |
| Wire contract | `pnpm contracts:check` (schemas, fixtures through Effect decoders, generated-file drift; regenerate with `pnpm contracts:generate`); Go and Elixir fixture decoders run in their test suites |

The Control suite runs against the database `control/config/test.exs` resolves:
`POSTGRES_DB` (repository default `symmetry_control_test`) with
`MIX_TEST_PARTITION` appended as a literal suffix. The `test` alias runs
`symmetry.check_test_db`, `ecto.create --quiet` and `ecto.migrate --quiet`
first, so create and migrate heal a missing or migration-behind database — but
nothing drops or truncates one. A database whose `schema_migrations` names a
revision older than its objects stops the alias's migrate step before any test
runs, with a duplicate-object error on the pending migration, for example
Postgrex `42P07` (`duplicate_table`) on a `CREATE TABLE`, or `42701`
(`duplicate_column`)/`42710` (`duplicate_object`) for a column or constraint.
That is stale local state, not a code failure; recover by dropping the
repository-default test database and re-running the suite, which recreates and
migrates it:

```sh
cd control
env -u POSTGRES_DB -u MIX_TEST_PARTITION MIX_ENV=test mix ecto.drop
env -u POSTGRES_DB -u MIX_TEST_PARTITION mix test
```

`MIX_ENV=test` is load-bearing: a bare `mix ecto.drop` resolves the development
database. Both commands unset `POSTGRES_DB` and `MIX_TEST_PARTITION`, so the
recipe always targets `symmetry_control_test` whatever the shell exported; to
recover a different dedicated test database, name it in place of the unset:
`env -u MIX_TEST_PARTITION POSTGRES_DB=<name> MIX_ENV=test mix ecto.drop`,
then `env -u MIX_TEST_PARTITION POSTGRES_DB=<name> mix test`.
`symmetry.check_test_db` is the first step of the `ecto.drop` alias
too, so the recipe is still refused for harness-owned databases only
(`symmetry_dev` and the `symmetry_0007c_/d_/e_` families). `MIX_TEST_PARTITION`
stays the CI-parallel path rather than the recovery recipe: exported, it names
that partition's own suffixed database, and the recipe's `env -u` unsets it, so
the partition database is left alone.

Run affected focused checks during development; required CI gates remain required
before merge. Native credentialed harness tests and Windows behavior cannot be
inferred from fake-agent tests or Linux compilation. Mark unavailable checks
pending with the missing environment; do not remove them to claim completion.

Amendment (2026-11-20, owner Q10C). Verification is selected by the paths a change
touches, not by the platform CI happens to use: a change touching `frontend/` or
`browser/` runs the browser render gate locally before its pull request opens (the
focused release-shell gate in the [browser README](../../browser/README.md), run
with `SYMMETRY_RENDER_GATE_REQUIRED=1` so a missing environment aborts instead of
skipping), and a change touching `daemon/`, platform-specific code, or `scripts/ci/`
runs the affected suites on the platform it targets before its pull request opens —
Linux via WSL or a Linux host; Windows-specific code natively on Windows. A
path-triggered run that cannot be executed is recorded as pending with its missing
environment, per the paragraph above, never as passed; documentation-only changes
under these paths are exempt, per the `AGENTS.md` sentence "Do not require every
expensive suite for a documentation-only or unrelated local change". CI remains the
floor, not the first execution of browser-rendered or Linux-only behavior: the local
run decides the change before review, and the hosted gates still decide the merge.

For state-changing code test the smallest set covering each changed invariant:
replay after commit, crash before acknowledgement, duplicate/out-of-order event,
stale revision/fence, cancel/completion race, provider unknown outcome, cost after
failure, and required evidence mismatch as applicable. No sleep-based race tests:
use synchronization/fault injection. Baseline flaky behavior is investigated,
not normalized through automatic reruns until green.

Go: resource ownership, cancellation, native platform and persistence failures.
Elixir: DB constraints, concurrent writers, rollback and notification-after-commit.
TS: decoder rejection, query races, cancellation, conflict recovery and UI receipt
truth. TypeScript strict checks and Dialyzer/typespecs do not prove domain policy.

## Completion receipt

For a change, report base/head commit, changed behavior, contract references,
checks actually executed with results, required checks not run, evidence subject
hash, reviewer findings and unresolved limitations. Never accept a review of an
older commit after new edits without checking their impact. CI/test-policy or
acceptance changes require a reviewer to inspect the gate diff itself.

## Task prompt

Use this as a compact task envelope; replace every bracketed field.

```text
Outcome: [observable required behavior]
Baseline and relevant code: [revision and paths]
Binding design: [specific sections, not all project history]
Scope and invariants: [ownership, compatibility, permitted effects]
Acceptance: [positive behavior and meaningful failure cases]

Implement the smallest complete change using the fixed design and local patterns.
Read the applicable AGENTS.md and only relevant repository skills.
Do not weaken checks or silently amend the contract. Report a concrete conflict
if a fixed assumption fails; continue unaffected authorized work.
Before finishing, simplify abstractions whose removal preserves requirements.
Return the actual checks, exact tested revision, result and unresolved limits.
```

Reviewer prompt:

```text
Independently assess this change against [contract] at [head revision].
Read the requirement and code before relying on the author's summary.
Look for violated ownership, missing failure behavior, stale evidence, weakened
gates and unnecessary abstractions. Use executable evidence where appropriate.
Report concrete findings with impact and reproduction; separate missing evidence
from demonstrated defects. Do not approve based on model reputation or confidence.
```

## Model-cost evaluation

Goal 0008 produces a small harness-neutral evaluation runner/data format using
the existing languages/tools, not a new benchmarking service. Tasks come from
real Go/Elixir/TS changes: ordinary edits plus recovery/authority/contract cases.
The [evaluation runbook](../evaluations/model-routing/README.md) provides the
repository-local runner, frozen task examples, worker instructions and independent
review procedure. Its receipts distinguish behavioral acceptance from incomplete
billing evidence; they do not promote a routing policy.
Freeze task specification, base commit and acceptance before either run.
Use isolated checkouts, the same tool permissions and budgets, and randomized
order where feasible. A strong-only baseline and mixed workflow both receive
the same evidence; record exact model/version, prompt/skill hashes and cache use.

Store task-level rows under `docs/evaluations/model-routing/` with:
task_id, base/head, language, risk_class, workflow, model_ids, prompt_hashes,
input/output/cached tokens (nullable), reported/estimated cost and price version,
wall_time, review_time, repair_count, accepted, reviewer_findings, later_defects,
environment and evidence refs. Keep raw sensitive transcripts outside Git.
The evaluator may use JSONL; do not invent result files with fabricated data.

Compare total cost of exploration + implementation + verification + review +
repair per accepted result. Repeated runs expose variability; choose sample
counts based on actual budget and report them, not an invented significance
threshold. Any observed acceptance regression prevents promotion for that task
class until resolved. A clean sample supports only its tested scope, never
"zero quality loss". Owner judges maintainability/UX and accepts routing policy.
Unavailable credentials/prices mean incomplete evidence, not estimated success.
A published receipt is re-produced only by re-executing the evaluation, never by
editing a receipt: when a submission needs rows exported under the current tooling,
the affected runs are executed again with those bytes. A receipt stays verifiable
against the tooling revision that produced it wherever a reader can reach those bytes:
the 0008b and 0008c runs' tooling is preserved by the annotated tags
`0008b-produced-tooling` (commit `253a701`) and `0008c-produced-tooling` (commit
`cab222b`), while the 0008d delivery's revision `73e083f` is untagged. The current
runner refuses a receipt whose tooling digests differ from the bytes that would
recompute it, so changing the evaluation tooling invalidates an older submitted
receipt by design (fail-closed) instead of re-deriving it into a new one.
