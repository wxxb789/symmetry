# 0008 series release receipt

The single release receipt for the `0008` model-routing evaluation series: `0008a` (the
auditable acceptance workflow), `0008b` (the Go carrier), `0008c` (the Elixir carrier) and
`0008d` (the TypeScript carrier, this increment). It binds one exact Subject, maps every
clause of the series acceptance contract (`docs/goals/archive/0008a-auditable-acceptance-workflow.md`,
section *Done when*) to the record that carries it, and states for each row which revision the
row's evidence came from. Nothing below is signed for the owner: the series is produced and
awaiting the owner's verdict.

## Subject this receipt binds

| Item | Value |
| --- | --- |
| Repository | `symmetry` |
| Evaluated base commit (the runs' frozen base; the revision every 0008d run receipt binds, and the meaning of "base `dacec450…`" wherever this receipt names one) | `dacec45034abc667ae65d41e1f778a896ec340c7` |
| Evaluated base tree | `33a36d9f753d6d9d85a8608b08aa1bd22123923c` |
| Landing base commit (what this receipt's branch is rebased onto for landing; `0007g`, `0007i` and one platform fix landed on `main` after the evaluation) | `ca1bdad981711d52bcf53377436d2b697aaf3930`, tree `3e04ba767c9f0735cc12a2e47017f843dac63301` |
| Delivered source subject of the evaluated revision (1120 source paths, re-derived by the delivered tool `python scripts/ci/model-routing-source-subject.py`: every regular file in the working tree except `.git` in either form, `__pycache__`, the declared build/report output directories (`dist`, `playwright-report`, `test-results`), the whole `node_modules` tree, the `.vite`/`.vite-temp`/`.vitest-cache` tool caches, `.tmp/` and everything under `docs/`; entry = mode + file digest + path, NUL-separated; digest = sha256 over the entries sorted by the UTF-8 bytes of the path) | `sha256:e333bb857811bf7d996e05320be7ab2b2680a6757395ef41fbfbce360d823ae4` - the subject of the tree the evaluation closed on (evaluated base plus this delivery's own source paths). The landing tip carries a different subject; it is the next row rather than a replacement, because a reader reaching the branch after the rebase no longer has this tree |
| Delivered source subject at the landing tip (landing base `ca1bdad…` plus this delivery, re-derived with the same tool after the landing rebase) | `sha256:70e03853e69239146d1f1669de22007c62a61bc379e36595722958d0cd093f69` over 1141 paths: the 21 extra paths are `0007g`/`0007i`'s contracts, browser and frontend additions, none of which any evaluated claim, receipt binding or check evidence in this series depends on |
| Delivered evaluation runner | `scripts/ci/model-routing-evaluation.py` `sha256:8150471596dee8cf88cedd2778f77b8d20fc37633972dc3d7b59df50cf123ee5` |
| Delivered runner regression suite | `scripts/ci/test-model-routing-evaluation.py` `sha256:dbe1a0fe27d3b1c119d15b4dfb408a38e591cf9fb1749ca3a9ce4fbcbc1f5fec` (superseded by the series-closing repair, which added the refusal-coverage derivation the S5 row names; history: `sha256:76f09f73…`) |
| Private evidence root (store, outside Git) | `<store>`, a disposable coordinator root outside Git that no published record names |
| Delivered task contracts | `docs/evaluations/model-routing/tasks/0008d-ts-runtime-capability-rows.json` `sha256:7733759022c4c874468f51106bdca6a6176e959a3c7c528dcf706146b394b5cf`; `docs/evaluations/model-routing/tasks/0008d-ts-command-receipt-truth.json` `sha256:480eef71bb356133fb004bd97b30bad67df89b38be1202618e70a20cc8529809` |
| Per-run Subjects (TypeScript carrier) | `03309e00…` (a-mixed), `6519aefe…` (a-strong), `d2c533c1…` (b-mixed), `d7dbee10…` (b-strong) — full digests in each receipt's `subject_hash` |

The 0008a, 0008b and 0008c receipts bind their own Subject each; this receipt does not restate
them, it names them in the clause map below. A row produced on an older revision is valid for
that older revision only (series constraint: evidence binds its revision).

Two subject details decide a re-derivation, both stated in the delivered tool: the walk excludes the
whole `node_modules` tree (a provisioned dependency tree is not delivered source - the *manifest*,
not this subject, is what polices package files inside a sealed checkout), and the entry mode is the
measured tree's own owner-execute bit, so this Windows worktree hashes every entry `100644` while the
tool's `executable_paths` line (empty here) names exactly the paths whose bit differs in another
checkout.

Between the evaluation and landing, `main` moved twice and this branch was rebased onto it (`0007g`,
`0007i` and one platform fix; landing base `ca1bdad…`). That replay changed no byte of this delivery:
the two replayed commits are patch-identical, the runner and suite bytes are unchanged
(`81504715…`, `dbe1a0fe…`), and the whole non-documentation difference the rebase introduced is
`main`'s own (98 paths, the same file list and blob pairs as `dacec450…`..`ca1bdad…` outside `docs/`,
with `.github/workflows/ci.yml` differing only by this delivery's one added workflow line). What the
rebase does change is *which tree a reader has*: the branch tip now carries `main`'s source, so the
tip's subject is `70e03853…` over 1141 paths while the evaluated revision's subject stays
`e333bb85…` over 1120 - both rows above, neither presented as the other.

The runner changed twice inside this letter and only the digests above are current. First: the
receipts were derived with `sha256:d80e49ca…` (source subject `sha256:5abd13ec…`, computed before
the coordinator's subject scope was narrowed to the declared set in the table above) until the first
full regression-suite pass failed two cases of
`EvaluationTests.test_vitest_report_gate_refuses_missing_failing_or_nested_names`, because the
derived vitest report's summary check pre-empted the more specific refusals (`missing <name>` and
`carries no test results`); the refusal order in `vitest_report_detail` was corrected with no branch
removed. Second: an independent review then showed that the manifest's cache predicate
`is_tool_cache_path` was reachable only from tests while the walk carried a second inline condition,
so the walk now asks `is_tool_cache_directory`, one line layered on that predicate, and the same
review's boundary note about unbound files inside a tool cache is recorded in the runner docstring,
in the closure record and in `docs/TODO.md`. Both changes were followed by re-deriving all four
receipts against the new bytes with no gate re-executed, no review re-imported and no check evidence
changed: the Subjects, check-evidence digests and review bindings in the table above are the same
rows throughout. Superseded values are labelled history: runner `sha256:d80e49ca…` then
`sha256:3f3b0975…` then `sha256:619f0d37…` (its last edit before the delivered bytes is docstring-only),
source subject `sha256:5abd13ec…` (1121 paths, first scope) then `sha256:5e1e4e6c…` (with the
`sha256:3f3b0975…` runner bytes) then `sha256:e0976262…` (1123 paths, `sha256:619f0d37…` runner) then
`sha256:4db67385…` (1118 paths, same runner; the scope narrowing above is what removed the bytecode
caches and the worktree `.git` pointer file from the binding), and each remains recorded in
`docs/TODO.md`.

A pre-publication audit then showed that the *last* of those values was itself stale: `4db67385…` was
computed before the runner's final docstring edit, so quoting it beside the delivered runner
`81504715…` broke the series constraint above (S13, evidence binds its revision). The pre-repair tree
this receipt actually describes hashed to `sha256:245edc15…` (1118 paths, delivered runner) - a value
that appears in none of the run's records or in its session transcript, which is why the receipt paired
a delivered runner with a superseded subject. This series-closing repair recomputed the subject with a *delivered* tool and republished it
as `sha256:e333bb85…` over 1120 paths: the two extra entries are the derivation test added to the
suite and the subject tool with its regression test, and the runner is untouched at `81504715…`, so
the four receipts, their check-evidence digests and their review bindings still re-derive byte for
byte. The earlier value stays here and in `docs/TODO.md` as labelled history rather than being
rewritten.

## Clause map of the full-series acceptance contract

The full-series contract is the *Full-series acceptance contract* section of
`docs/goals/archive/0008d-typescript-model-routing.md`, preserved unchanged from the former series
review. Its owner acceptance is the post-run verdict, not a condition this run can satisfy.

| Clause | Requirement | Evidence | Evidence revision named in the row |
| --- | --- | --- | --- |
| F1 | A practical development workflow in which lightweight models perform suitable Go, Elixir and TypeScript work without lowering the code acceptance standard (per `docs/design/quality.md` and the language contributor contracts) | All four letters: `0008a-{positive,negative}.json`, `0008b-*`, `0008c-*`, `0008d-*` receipts, each accepted arm passing its class's frozen gates with an independent review bound to its own revision. The gate kinds differ per letter and are named rather than folded into one list: `0008a` and the 0008b regression class run `protocol-tests` + `protocol-vet`, the 0008b ownership class `platform-tests` + `platform-vet`, `0008c` runs `goal-receipt-e2e`, `format`, `compile`, `focused-tests`, `provider-broker-e2e` and `full-suite`, and only `0008d` carries all eleven gates, including the strict type, lint, build, staged-bundle and browser gates | each letter's own base/head: 0008a `343dea6a…`, 0008b/0008c `5c2e0ada…`, 0008d `dacec450…` |
| F2 | Usable task instructions, scoped repository skills, executable verification and independent review behavior that prevent silent weakening; real tasks demonstrate escalation when design or verification exceeds the worker's scope | `docs/evaluations/model-routing/{task,review}-instructions.md`, the `.agents/skills/symmetry-*` scoped skills, `0008a-controls.json` (weakened-test and missing-gate refusals), `0008a-escalation.json` (recorded escalation with failing evidence), plus each letter's per-run review records and the 0008d carrier-level refusals | 0008a commit `343dea6a62484d73e7b577a0be2683ca233510f6` (base and head of `0008a-positive.json`/`0008a-negative.json`) for controls/escalation; instructions and skills as delivered at base `dacec450…` |
| F3 | Compare mixed-model and strong-model workflows on frozen representative tasks with the same acceptance conditions; record actual total cost per accepted change, verification and review effort, repair, observed failures and limitations; the owner can inspect the underlying evidence and accept a task-class routing policy | Paired receipts per class in every letter (`0008b-go-*`, `0008c-{a,b}-*`, `0008d-{a,b}-*`), each pair sharing identical frozen task/packet bytes, gates, permissions and budgets; token/cost rows with `cost_provenance`, price version and `accounting_complete`; wall/review effort and failed attempts per run | each pair's own revision; 0008d receipts and review records above |
| F4 | Recommend lightweight routing only where measured results justify it; a not-cheaper or insufficient-evidence finding stays visible; no arbitrary savings target, invented benchmark or fabricated result; missing provider access, costs or independent review remain missing evidence; writing AGENTS.md and skills alone does not complete the goal | `0008b-policy.json` (`promote-lightweight` for `go-bounded-regression`, `insufficient-evidence` for `go-native-ownership-settlement`), `0008c-candidate-policy.md` (both classes `strong-only`), `0008d-candidate-policy.md`/`0008d-candidate-policy.json` (both classes `strong-only`, with the class-B asymmetry published), plus the unresolved list below | each policy record's own revision |
| F5 | Owner acceptance after the run: the owner reviews the TypeScript receipts, the paired rendered capture set for the two workflows and the series receipt against `docs/design/quality.md`, records their qualitative visual judgment, and accepts or rejects the TypeScript, Go and Elixir policies together | Not satisfied by this run and not claimed: this receipt carries the evidence for that verdict, names the rendered capture sets (`<store>/runs/<run>/rendered`, 44 images per accepted arm at desktop-chromium and mobile-chromium widths) and states the result as produced and awaiting the owner's verdict. The model never rates its own rendering and never signs for the owner | 0008d base `dacec450…` |

## Clause map of the 0008a Done-when (the workflow contract the series reuses)

| Clause | Requirement (0008a *Done when*) | Evidence | Evidence revision named in the row |
| --- | --- | --- | --- |
| S1 | A frozen contract runs end to end in isolation and the workflow's guards are demonstrated | `docs/evaluations/model-routing/0008a-audit.json`, `0008a-controls.json`, `0008a-positive.json`; isolation and sealing re-demonstrated by `0008b`/`0008c`/`0008d` runs (sealed manifest per run, e.g. `docs/evaluations/model-routing/0008b-go-bounded-regression-mixed.json`) | 0008a commit `343dea6a62484d73e7b577a0be2683ca233510f6`, re-demonstrated on each letter's own base revision (0008b/0008c `5c2e0ada…`); 0008d evidenced on base `dacec450…` |
| S2 | Positive and negative runs on real current Go or Elixir work → receipts with base/head, model identities, provenance, credential route metadata, checks, evidence, exact Subject | `0008a-positive.json` (Go), `0008a-negative.json` (refusal); `0008b-go-bounded-regression-{mixed,strong}.json`, `0008b-go-native-ownership-settlement-{mixed,strong}.json` (Go); `0008c-{a,b}-{mixed,strong}.json` (Elixir) | each receipt's own base/head and `subject_hash` |
| S3 | One bounded task completed by the lightweight worker → its receipt with the explanations the task required | `0008a-positive.json` (lightweight worker route, required explanations in `reviewed_explanations`); repeated per letter — for 0008d see the a-mixed and b-mixed rows | 0008a commit `343dea6a62484d73e7b577a0be2683ca233510f6`; 0008d base `dacec450…` |
| S4 | One escalation triggered by a real boundary → the escalation handoff carrying failing evidence and preserved correct work | `0008a-escalation.json` (recorded handoff with failing evidence and preserved work) | 0008a commit `343dea6a62484d73e7b577a0be2683ca233510f6` |
| S5 | Rejection controls: stale revision, missing gate, weakened test or guard, incomplete or fabricated receipt are all refused → observed refusals | `0008a-controls.json` (the four controls); 0008d re-observes the class on the TypeScript carrier: a frozen task refused at `prepare` for a protected-path contradiction (recorded from the command's own message in the closure record; the pre-correction task bytes were not preserved, so that refusal is documented rather than re-runnable), every derived-vitest-report defect refused fail-closed by the runner and pinned by `scripts/ci/test-model-routing-evaluation.py` - of the 17 refusal shapes in `vitest_report_detail`, 15 are reached by the suite's malformed-report cases (the fifteen cases include a missing named test, an empty result list, a non-zero failure count, a malformed file result, a file result without assertions and an assertion without its fields) and exactly 2 are not: the vitest run's own timeout and an assertion that is not an object. That classification is derived rather than listed: `python scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_vitest_report_refusal_coverage_is_derived` parses the inventory out of the function's syntax tree, measures coverage by executing the very documents those cases feed to the gate, prints the classification and fails when it changes - a revision change refused during a gate (`source changed while executing this gate`), a submitted receipt refused by `accept` because it did not match the re-derived evidence at `$.evidence.frozen_review_sha256`, and a review refused when its worker session did not match the usage phases (`usage phases omit an implementing worker session required by the review`). The four live refusals, with their messages, are recorded in the closure record's *Refusals observed live in this run*, and the same refusal classes are pinned for 0008a in `0008a-controls.json` | 0008a commit `343dea6a…` for the controls; 0008d observations on base `dacec450…` and the four TypeScript attempts; the coverage classification on the delivered suite `dbe1a0fe…` |
| S6 | Total-cost discipline: exploration, implementation, verification, review, repair and failed attempts counted; unknown billing recorded as unknown with token counts and a price version, never zero | Per-run token and cost rows with the fixed phase list in every letter's receipts; 0008a's positive run carries the token fields with null values and non-null known subtotals, its negative run carries concrete token and cost rows; the 0008b-0008d receipts carry `accounting_complete: false`, an empty `price_versions` list and null per-phase price versions - the field is present with unknown values, never zero | 0008a commit `343dea6a…`; 0008b/0008c receipts on `5c2e0ada…`; 0008d on `dacec450…` |
| S7 | Task instructions and review behavior demonstrably prevent silent weakening → negative controls plus an independent review record on the produced revision | `docs/evaluations/model-routing/{task,review}-instructions.md`; per-run review records (`<letter>-<class>-<arm>.review.json`), each bound to its check attempt and evidence digest; 0008a negative controls; 0008d's four review records, including reviewers that executed mutations and reverts in throwaway copies to prove named tests bind production behavior | 0008a commit `343dea6a…` for the controls; each review record names its own subject, attempt and evidence digest (0008d: `03309e00…`, `6519aefe…`, `d2c533c1…`, `d7dbee10…` on base `dacec450…`) |
| S8 | Task-level receipt fields (task_id, base/head, language, risk_class, workflow, model_ids, prompt hashes, tokens, cost with provenance and price version, wall_time, review_time, repair_count, accepted, reviewer_findings, later_defects, environment, evidence refs) | Enforced by the runner's receipt/review/accept path and its regression suite; present in every 0008a–0008d receipt (`docs/evaluations/model-routing/*.json`) | runner bytes of the letter that produced the receipt; for 0008d the delivered `81504715…`, with every superseded runner and subject digest kept as history above and in `docs/TODO.md` |
| S9 | Raw sensitive transcripts stay outside Git | All sessions, packets and transcripts live in the private evidence root (`<store>`); Git carries receipts, review records and narrative records only | 0008a commit `343dea6a…`; 0008d store `<store>` |
| S10 | Reuse existing configured worker/reviewer routing and authorization; no new profiles, caps or credentials | Credential route recorded as metadata (`omp-existing-provider`) in every receipt; no profile, cap or credential added by the workflow (docs/design unchanged in this respect) | each letter |
| S11 | Series operational assets: usable task instructions and scoped repository skills for the evaluated classes | `docs/evaluations/model-routing/task-instructions.md`; `.agents/skills/symmetry-durable-transition`, `symmetry-harness-adapter`, `symmetry-frontend-contract`, `symmetry-simplification-review`, `symmetry-acceptance-workflow` (frontend contract and acceptance workflow skills added by this increment's series) | 0008a commit `343dea6a…`; skills as delivered at base `dacec450…` |
| S12 | No wire contract, gate, permission, authority or recovery rule weakened; repository stays runnable; fixed-design changes need a documented amendment | `0008a-controls.json` refusal of a weakened gate; 0008d adds gate fields without weakening the frozen list and its suite proves fail-closed behavior; `docs/design/README.md` decisions preserved | 0008a commit `343dea6a…`; 0008d runner `81504715…` and suite `dbe1a0fe…` |
| S13 | Evidence binds its revision; older evidence never validates newer code | Every receipt binds subject, check evidence digest and tooling digests; `accept` re-derives against the recorded bytes and refuses mismatches (observed in 0008d for a stale submitted receipt). This receipt's own subject row was itself the counter-example and is the reason for the series-closing repair: it paired the delivered runner `81504715…` with a subject computed before that runner's last edit (`4db67385…`), caught by a pre-publication audit and returned here rather than left standing; the row now carries a subject recomputed from the delivered tree | all letters; the corrected subject row on the 0008d tree above |
| S14 | If configured routing or authorization is missing at run start, stop and report; no synthetic substitution, no narrowed scope | Routing and authorization were available for every letter; no run started partially. 0008d froze its two-class list before the first arm, both classes carried by accepted 0007 surfaces with executable acceptance commands, and a class without them would have been reported untested rather than trimmed | 0008d base `dacec450…` |
| S15 | One independently mergeable PR per goal (repository convention) | 0008a, 0008b and 0008c each landed as their own increments; 0008d is the series-closing increment and is produced for review as one change set | each letter's merge; 0008d pending owner |

## The letters

| Letter | Carrier / class | Receipts | Accepted | Observed outcome and unresolved items |
| --- | --- | --- | --- | --- |
| 0008a | Workflow itself, real Go work | `0008a-{positive,negative}.json`, `{audit,controls,escalation,review}.json` | 1 of 2 | Positive run accepted with one finding resolved; negative run refused (intended). Token rows: the positive run carries null token fields with non-null known subtotals, the negative run carries concrete rows (21,814 / 12,965 / 483,008, est USD 0.0221022) — unknown fields stay unknown, nothing is zero-filled. |
| 0008b | Go carrier, two classes | `0008b-go-bounded-regression-{mixed,strong}.json`, `0008b-go-native-ownership-settlement-{mixed,strong}.json`, `0008b-{controls,policy}.json` | 2 of 4 | `go-bounded-regression`: both arms accepted (mixed 16,397/43,391/2,335,920 tokens, est USD 0.5885838; strong 75/13,794/1,100,500, est USD 0.5011912). `go-native-ownership-settlement`: **neither arm accepted** (mixed est USD 1.3038045, strong est USD 2.0244585) — an unresolved class outcome, kept unmet here rather than presented as a pass. |
| 0008c | Elixir carrier, two classes | `0008c-{a,b}-{mixed,strong}.json` and their `.review.json` records | 4 of 4 | All four accepted; findings all resolved; accounting incomplete (`accounting_complete: false`) in every receipt; est USD 0.2002575 / 0.6404529 / 0.3790065 / 0.8367779. Unresolved: the four committed receipts carry raw host paths (TODO: re-publish through `model-routing-record-sanitize.py`), and receipts produced by the 0008c-era runner bytes no longer re-derive under later tooling (TODO entry). |
| 0008d | TypeScript carrier, two classes | `0008d-{a,b}-{mixed,strong}.json` and their `.review.json` records | 4 of 4 | See the TypeScript section below. |

## 0008d — TypeScript carrier

Two frozen classes, each run twice with identical task bytes, gates, permissions, budgets,
evidence shape and review route; only the implementer route differs (`deepseek-v4.1-flash-fast`
versus the owner-routed strong route). Class A's packet bytes are identical across its arms, and so
are class B's, so the route is the only difference between arms. Every arm passed all eleven frozen
gates (contracts typecheck/drift/effect/tests, frontend typecheck/lint, focused vitest suite with a
parsed structured report, frontend suite, frontend build, staged-bundle check, browser acceptance at
two widths) and carries an independent review record bound to that attempt.

The receipts' `prompt_hashes` lists are not identical between the arms of a class even though the
packet and the user prompt are: the a-mixed implementation phase's session carries two
`custom_message` entries and one `ttsr_injection` (`mid-run-todo-nudge`, an `async-result` notice and
the `ts-no-tiny-functions` rule injection), and its phase row binds that session file's digest
`sha256:d3116a8d…`, while the a-strong phase records none. Those are the worker's own runtime
notices, not coordinator steering, and every other prompt-identity row is equal per class.

| Run | Subject | Check attempt | Gates | Tokens in/out/cached | Est. USD | Wall / review (s) | Reviewer verdict | Observed repo hash |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| a-mixed | `sha256:03309e00…` | attempt-0004 (3 earlier attempts refused) | 11/11 | 178,066 / 99,237 / 4,950,400 | 0.1725042 | 1,166.1 / 168.3 | pass, 4 resolved `info` | `ee4bf9a4…` |
| a-strong | `sha256:6519aefe…` | attempt-0001 | 11/11 | 101,204 / 44,968 / 2,888,136 | 0.6868291 | 1,024.4 / 127.7 | pass, 3 resolved `info` | `dc03503a…` |
| b-mixed | `sha256:d2c533c1…` | attempt-0001 | 11/11 | 190,067 / 122,546 / 6,968,256 | 0.2040753 | 1,358.6 / 223.4 | pass, 5 resolved `info` | `0a25c6d8…` |
| b-strong | `sha256:d7dbee10…` | attempt-0001 | 11/11 | 101,752 / 125,689 / 8,876,103 | 0.8930115 | 1,575.8 / 462.9 | pass, 9 resolved `info` | `23ac1dd1…` |

Candidate policy (`docs/evaluations/model-routing/0008d-candidate-policy.md` and the machine-readable
`0008d-candidate-policy.json`, derived from the four receipts and their review records by coordinator
tooling under criteria fixed before the results): both tested classes are recorded as
**strong-only** - class A because the mixed arm is worse in every token row, in wall time and in
review effort; class B because the mixed arm, although better on output, cached tokens, wall time and
review effort at a 4.4x lower estimate, is worse in input tokens (190,067 against 101,752), so the
fixed "not worse in every measured dimension" precondition fails. Every acceptance regression,
not-cheaper class and evidence gap is carried explicitly rather than recommended away; the record
recommends no routing change and makes no claim for untested TypeScript work, Go or Elixir.

## Routing decisions on the table for owner acceptance

| Class | Letter | Decision | Basis recorded in that letter |
| --- | --- | --- | --- |
| `go-bounded-regression` | 0008b | **promote-lightweight** (the only promotion recommended) | Both arms accepted the same frozen task with no findings; on equal treatment the mixed arm's accepted work was cheaper (implementation USD 0.038420 against 0.224566, total 0.310666 against 0.501191) with near-equal review cost. **As run the mixed arm was the more expensive arm** and that stays visible: its total including the charged failed attempt (USD 0.277918) is 0.5885838 against the strong arm's 0.5011912, which `0008b-policy.json` states and the 0008b letters row above prints; the promotion rests on the equal-treatment comparison, not on the as-run total |
| `go-native-ownership-settlement` | 0008b | **insufficient evidence** | Neither arm produced an accepted result: the mixed arm ended its bounded repair round with one open medium finding, and the strong arm's review also returned fail (est. USD 1.3038045 / 2.0244585) |
| `elixir-established-pattern`, `elixir-db-concurrency-authority` | 0008c | **strong-only** | Mixed arms 2.2x / 3.2x cheaper by estimate but worse in input, output and cached tokens and in wall time |
| `ts-runtime-capability-rows` | 0008d | **strong-only** | Mixed arm worse in every token row, in accounted wall time and in review effort |
| `ts-command-receipt-truth` | 0008d | **strong-only** | Mixed arm better on output, cached tokens, wall time and review effort at a 4.4x lower estimate, but worse in input tokens |

So one class is recommended for lightweight routing, three tested classes require the strong route,
and one class has insufficient evidence; every one of those outcomes is a single sample per arm and
every cost figure is a harness estimate, because no provider-reported cost or price version exists on
these routes. No model adopts any of this: the owner accepts or rejects the TypeScript, Go and Elixir
policies together, and until then the result stays produced and awaiting the owner's verdict.

## Unresolved findings and reused evidence, stated plainly

Unresolved in this series:

1. `go-native-ownership-settlement` (0008b) has no accepted arm — the class outcome stays unmet.
2. Billing is incomplete in every 0008b–0008d receipt: no provider-reported cost and an empty
   `price_versions` list, so `accounting_complete: false` and the harness estimate is labelled as an
   estimate. 0008a's positive run carries the token fields with null values and non-null known
   subtotals, its negative run concrete rows, and neither invents a price.
3. Raw host paths ship in the committed 0008c receipts and evidence record, and also in
   `0008a-audit.json` and `0008a-controls.json` (the latter cited as evidence by this receipt); the
   release notes in `docs/TODO.md` ask for a re-publish through the sanitiser. In 0008d no record
   carries a host-specific absolute path: the receipts are published as the runner's derived bytes
   (`accept` re-derives each one byte for byte), the narrative records name the store only as
   `<store>`, and the session text the runner read had already been rendered, which is why the class-A
   records still carry `path-sha256:` tokens (including one for the fragment `/||`). The sanitiser
   also hashes non-host absolute paths, so `--check` still exits 1 for the records that keep the
   literal route `/portal/runtimes` and for the derived receipts; recorded in the same TODO entry.
4. The manifest's tool-cache exclusion is a declared boundary rather than a proof of harmlessness:
   files under `node_modules/.vite`, `.vite-temp` and `.vitest-cache` are not bound by the Source
   manifest, so in principle a worker could place unbound code there. No run observed such a file,
   the runner docstring states the boundary, and `docs/TODO.md` carries the narrowing proposal.
5. Receipts produced by earlier tooling bytes refuse re-derivation after a tooling change (fail
   closed, TODO entry). The 0008d receipts bind the delivered bytes only.
6. The TypeScript carrier's rendered claim for the new capability rows rests on the focused
   component test (three named cases, three independent reviewer-checked mutations) and the frozen
   browser gate's existing runtimes case, not on a new browser case; both class-A reviewers and this
   record state that limit rather than claiming browser-level coverage.
7. `go-bounded-regression` (0008b) stays visible as a *not-cheaper as run* result: the mixed arm's
   total including its charged failed attempt is 0.5885838 against the strong arm's 0.5011912, and the
   promotion rests on the equal-treatment comparison. The routing row above states it in full; it is
   not a demonstration of savings.
8. The 0008d increment's own closeout discloses a duplicate full-suite pass: two passes of
   `scripts/ci/test-model-routing-evaluation.py` on the same superseded revision (1127.054s and
   1212.410s, both `OK (skipped=2)`), the second started because the first was believed dead. Both are
   labelled history in the closure record and neither converted a failure.
   The repair's own first pass on the repaired revision also failed one case -
   `test_timed_out_gate_terminates_its_descendants` ("a gate descendant survived the per-gate
   timeout"), a load-sensitive fixture - while four other suites and the record re-derivations ran
   beside it, the concurrent-host combination this workflow's runbook forbids. The closure record
   carries that failed row rather than a tidied one, the assertion is untouched, and `docs/TODO.md`
   carries the fixture's timing and the snapshot-kill path for separate maintenance. One quiet pass
   on the same revision then ran green (`Ran 56 tests in 1026.271s`, `OK (skipped=2)`, exit 0,
   including the case that failed under load), and both passes are rows in the closure record.
9. The TypeScript gates resolved their toolchain through a shared hoisted `node_modules` one level
   above the run directories. That tree is outside every sealed checkout and outside this subject; the
   run's `environment.json` records the route and asserts that integrity values are the frozen
   lockfile's own, but no provisioning log, install output or tree digest was stored, so that
   assertion cannot be re-verified by an auditor and an unbound file placed there would not show in
   any binding. `docs/TODO.md` carries the proposed fix (a recorded provisioning step that binds the
   resolved tree).
10. The audit that produced this repair could not verify five things from artifacts, and they stay
   unverified here rather than being claimed: the surface-proof command table in the goal receipt is
   narrative-only (the store's `surface/` directory is empty and no command log of it exists); the
   policy's "criteria fixed before the results" wording is not timestamped by anything; the two
   pre-publication review rounds' methods were not audited (only their findings and the artifacts they
   cite); the pre-correction 0008d task bytes that a `prepare` refusal rejected were not preserved;
   and the two 0008a-era "completed/stopped" first-attempt sessions were not inspected in detail.
11. Independent review is not comparable across letters: 0008b's reviewer ran on `gpt-6.1-sol` while
   0008c and 0008d used `deepseek-v4.1-flash-fast`. Within-letter comparisons are unaffected, but the
   series-level claim is only "each accepted revision had an independent review", not "the same
   reviewer route reviewed every letter".

Reused evidence (deliberately, not as a substitute for missing runs):

1. The 0008a controls, audit and escalation records remain the series' evidence for S4 and S5;
   0008d adds its own carrier-level refusals but does not re-audit the workflow logic.
2. The 0008b carrier shape and the 0008c arm structure are the model for the 0008d runs: same
   runner, same receipt fields, same fixed criteria; 0008d extended the runner in place (gate fields
   and the structured-report cross-check) instead of adding a second mechanism.
3. The frontend build assets are produced by each arm and verified by the frozen gates; the 0008d
   receipts name the exact staged files and hashes rather than inheriting them from another arm.

Claims this receipt does not make: no promotion verdict is adopted as repository policy, no
untested TypeScript class is characterised, no Go or Elixir behaviour was re-measured in this
increment, the series is not declared accepted, and nothing here signs for the owner.
