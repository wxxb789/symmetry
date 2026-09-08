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

The current CI already has Elixir tests/release, Linux Go tests/race tests,
Windows tests and daemon/control restart E2E. Preserve those gates. Do not pretend
future frontend/contract commands already exist in this documentation PR.

| Surface | Actual baseline commands / target additions |
| --- | --- |
| Elixir (`control/`) | `mix format --check-formatted`; `mix compile --warnings-as-errors`; `mix test`; release/E2E per current CI |
| Go (`daemon/`) | `go test ./...`; concurrency changes: `go test -race ./...`; add `go vet ./...` to gate if absent |
| Browser (`browser/`, before migration) | `npm ci`; `npm test`; configured control/daemon fixtures required |
| TS (goal 0007 target) | frontend typecheck/lint/test/build commands from frontend.md; migrate browser runner to pnpm |
| Wire contract (goal 0006 target) | generate/check DTO drift; positive/negative fixtures through Elixir/Go/Effect decoders |

Run affected focused checks during development; required CI gates remain required
before merge. Native credentialed harness tests and Windows behavior cannot be
inferred from fake-agent tests or Linux compilation. Mark unavailable checks
pending with the missing environment; do not remove them to claim completion.

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
