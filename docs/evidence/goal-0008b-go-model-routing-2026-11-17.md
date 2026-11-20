# Goal 0008b: evidence-based Go model routing

## Contract and source provenance

The binding contract is [`docs/goals/archive/0008b-go-model-routing.md`](../goals/archive/0008b-go-model-routing.md),
including its standalone Done-when and its owner-acceptance paragraph.
The run started from commit `5c2e0ada6a3fd669c5ed2d2bc4b50ff9fed3b12a` on branch
`feat/0008b-go-model-routing` with a clean working tree; no commit, push or history rewrite was
performed, and no other worktree or `main` was touched.
Base tree digest for every isolated checkout: `sha256:1c6b5adddef45ef431b28f760bd8b1c0b3fa41e153034474cf618e3ccca70a21`
(`scripts/ci/linux-v2-source-subject.py` exit 0 on an isolated clone of that commit).

Shape reference: the accepted 0008a increment and its evidence record. This goal reuses that
increment's tooling unchanged — `scripts/ci/model-routing-evaluation.py`,
`scripts/ci/model-routing-omp-usage.py`, `scripts/ci/assert-go-test-json.py`,
`scripts/ci/linux-v2-source-subject.py` — with the receipt/manifest format, the source-subject seal
and the six-stage usage ledger, and defines no second pipeline, profile, spend cap or credential.

## Startup routing and preflight

Both required routes were proved with real requests before any task ran; the served identities below
come from the observed session records, not from a worker's self-description:

| Route | Configured role | Served identity | Effort | Preflight session (private probe record) |
| --- | --- | --- | --- | --- |
| Strong | `modelRoles.sol_high` = `ghc/gpt-6.1-sol:high` | provider `ghc`, model `gpt-6.1-sol`, api `openai-responses` | `high` | `01a1e5fb-97a6-7202-8bff-67e351602a0c` |
| Lightweight | `modelRoles.flash_max` = `tencent-litellm/deepseek-v4.1-flash-fast:max` | provider `tencent-litellm`, model `deepseek-v4.1-flash-fast`, api `openai-completions` | `max` | `01a1e5fb-b745-74d1-b2af-ef2f45bfec52` |

The two preflight probe records live in the coordinator's private store and are not published; the
run itself binds the same served identities in each receipt's `model_sessions` rows with their
`omp.headless.modelRole.*` provenance.

Every run reused the existing configured routing and authorization (`reuse_existing`): the mixed arm
used the configured lightweight role as worker and the configured strong role as the separate
verifier/reviewer; the strong-only arm used the configured strong role for both the worker and the
reviewer invocation. The reviewer is a separate invocation and context in both arms; in the
strong-only arm that is invocation separation inside one model family, not model diversity.
Credential routes are recorded as metadata only (`omp-ghc-configured`, `omp-tencent-configured`);
no credential value, prompt body or transcript is exported.

## Frozen Go classes and tasks

The class list and both task files were fixed before the first arm ran; their digests are recorded in
the receipts as `task_sha256` and published with the policy record.

| Class | Frozen task | Bytes / SHA-256 | Shape |
| --- | --- | --- | --- |
| `go-bounded-regression` | `docs/evaluations/model-routing/tasks/0008b-go-boundary-validator-coverage.json` | 1660 / `sha256:d3422209a7ece34e235e0d28a52d4dd0af17037b96513bd86d64385190d99d86` | Established-pattern regression coverage of documented production boundary validators in `daemon/internal/protocol`; production files protected, only a new test file is writable |
| `go-native-ownership-settlement` | `docs/evaluations/model-routing/tasks/0008b-go-close-owned-settle.json` | 2822 / `sha256:287d0ab9769001a058f599e0aeeef20e4fbb3bbe1ea0036e489d67938827773f` | Resource-ownership and recovery-sensitive production change in `daemon/internal/platform` (Windows containment settlement), with new deterministic coverage and unmodifiable baseline tests |

Each arm received the same frozen bytes, the same allowed and protected paths, the same gate argv,
the same approval mode, the same 40-minute worker and 25-minute reviewer wall-clock budgets, and the
same worker packet text; the packets differ only in the workspace path line they must name. These
coordinator-private inputs (packets, budgets, approval mode) are not exported with this record, so
the freeze is a coordinator observation rather than an artifact a reader can re-inspect.

The host surface actually used is Windows: win32 10.0.26300, amd64, `go version go1.27.4 windows/amd64`,
`omp/18.20.3`, `git version 2.56.0.windows.4`, Python 3.12.10. Both classes' gates ran on that
surface; no Linux-surface claim is made.

Before the runs, the acceptance toolchain was proved to execute in an isolated checkout on this host:

| Proof | Command | Observed |
| --- | --- | --- |
| Base qualification | `python scripts/ci/linux-v2-source-subject.py . 5c2e0ada…` | exit 0, tree digest `1c6b5add…` |
| Vet in isolated clone | `go vet ./...` (from `daemon/`) | exit 0, 28 s |
| Class A gate package | `go test -json -count=1 ./internal/protocol` | exit 0, 12 s, 57 top-level tests run / 58 pass events / 0 skip / 0 fail |
| Class B gate package | `go test -json -count=1 ./internal/platform` | exit 0, 17 s, 91 run / 91 pass / 1 skip / 0 fail |
| Recorded intermittent test | `go test -json -count=10 ./internal/platform -run '^TestCloseStopsOwnedChildAfterLeaderExit$'` | exit 0, 22 s, 10 tests, all pass |

The preflight observations above are the coordinator's private gate logs for this run (raw
`go test -json` streams under the run's `gates-proof-*.jsonl`/`gates/` files, not published); the
commands themselves are re-runnable on this host surface.

## Run results

Both arms per class ran sequentially on the same host, one at a time; no measurement run overlapped
another run or a full test suite.

### `go-bounded-regression`

| Item | Mixed arm | Strong-only arm |
| --- | --- | --- |
| Worker route | `deepseek-v4.1-flash-fast` (`@flash_max`) | `gpt-6.1-sol` (`@sol_high`) |
| Reviewer route | `gpt-6.1-sol` (`@sol_high`), separate invocation | `gpt-6.1-sol` (`@sol_high`), separate invocation |
| Delivered change | new `daemon/internal/protocol/goal_boundary_validator_test.go` | new `daemon/internal/protocol/goal_boundary_validator_test.go` |
| Sealed subject | `sha256:b10945d7026f081b4aa32df12d6b738f1e95fba87291c82001740734cd5b811f` | `sha256:9301f14f2a6f1f51541a4bd3f730eed4a5773a5b22d02a2d9130a1052f278824` |
| Checkpoint | `attempt-0001` `sha256:d3609e30…`, complete and passed | `attempt-0001` `sha256:7c818050…`, complete and passed |
| Gates | `protocol-tests` exit 0 with validator exit 0; `protocol-vet` exit 0 | same argv, both exit 0 |
| Independent review | verdict `pass`, no findings, reviewer session `01a1e609-3fce-7508-b9a0-f58599717e66` | verdict `pass`, no findings, reviewer session `01a1e611-3cdf-7460-8213-d55370063a4e` |
| Accepted | yes (`accept` exit 0) | yes (`accept` exit 0) |
| Receipt SHA-256 | `sha256:c5b01b39f01c50c9e4f95d635aee22fd54f06576ab7e499d0c1fd70ee704f53c` | `sha256:8ad6c2d277dfac86af23fe6e858d7c8eedb9ab9e5961c10ddb8c6934e824332c` |
| Repair rounds | 1 (coordinator-caused review redo, see negative findings; the discarded first reviewer invocation is published in `0008b-go-bounded-regression-mixed.review-attempts.json`) | 0 |

The frozen named tests `TestValidateMicroUSDEnforcesWireMonetaryContract` and
`TestValidateCommitPathRejectsUnnormalizedPaths` ran once and passed once in both arms, verified
through the event validator (`validator_exit_code` 0) and the runner's derived `go test -list`
cross-check.

### `go-native-ownership-settlement`

| Item | Mixed arm | Strong-only arm |
| --- | --- | --- |
| Worker route | `deepseek-v4.1-flash-fast` (`@flash_max`) | `gpt-6.1-sol` (`@sol_high`) |
| Delivered change | `daemon/internal/platform/process_windows.go` (+162/−1; the repaired revision +290/−1) plus new `process_owned_settle_windows_test.go` (259 lines; the repaired revision 604 lines); per-attempt diffstats are recorded in the arm's `.review-attempts.json` | `process_windows.go` (+206/−8; the repaired revision +212/−8) plus new `process_owned_settle_windows_test.go` (604 lines; the repaired revision 680 lines); per-attempt diffstats are recorded in the arm's `.review-attempts.json`. This arm retains opened process handles and gates observation on the named `jobID` instead of treating every attachment alike |
| First sealed subject | `sha256:630c227594e6704c18c5a31a6bd7e451d9a034e70590e546c8ca4e9fdac464a7` (`attempt-0001` check evidence `sha256:586952da7072bd41471c83aaef4befad430b64ac093f083f434bd35484a51de7`, gates exit 0, recorded gate wall 44 s) | `sha256:aeafa56bc1d5a4129d0a9947adf02b556f552fcd400480c119c957c2ad5e4ad1` (`attempt-0001` check evidence `sha256:82108781e207adf4e98e6af85635676de56dd835b9d0396adfab3a825803f91b`, gates exit 0, recorded gate wall 30 s) |
| First independent review | verdict `fail`, 3 open findings (two high: unreadable owned processes treated as settled, enumeration failure silently disabling the proof; one medium: durable retry discarding observed unsettled identities) | verdict `fail`, 1 open medium finding (the process census grew its buffer after a successful call whose assigned count exceeded the listed count, citing Microsoft BuildXL's requirement of `ERROR_MORE_DATA` before retrying) |
| Repair round | 1: the findings were returned to the arm's worker route; production file repaired, re-sealed as `sha256:1ce76d473dc5c7e0f9eb63d0786fa25140c1a8b0426041a5963e039ec9f6c23f`, re-checked as `attempt-0002` (check evidence `sha256:1420a8dd23039c5a56ebc86df7bcf9948dab455f4fe840cd89b999e34f867dd2`, gates exit 0, recorded gate wall 50 s) | 1: the finding was returned to the arm's worker route; the census now grows only after a failed call reporting `ERROR_MORE_DATA`, re-sealed as `sha256:d44e99a19199073398dc4c388a2401e1049e9c6f55133075262890a194178065`, re-checked as `attempt-0002` (check evidence `sha256:86bc36650dfe47c6e4dba04ab1f18e28f7fb829bd4b1e062a2fc47e0f65887f9`, gates exit 0, recorded gate wall 28 s) |
| Second independent review | verdict `fail`; the three previous findings are `resolved`, one new medium finding is `open` (the non-durable supervisor branch caches a nil close error after a settlement failure, so a repeated `Close` can return nil without the proof) | verdict `fail`; the previous finding is `resolved`, one new medium finding is `open` in the same non-durable supervisor `Close` branch (`containmentSupervisorStopLease` without durable authority), reachable through the native Go-test-binary `AttachProcess` route and not through the production daemon's durable authority |
| Accepted | no: no passing review record exists, so the derived receipt carries `accepted=false` with blockers "no independent review record was imported" and "task explanations are not attested by an independent review" (`receipt` exit 0, receipt SHA-256 `sha256:7d075a2de40ef3153b680f603ff644a430505621ea1bb2820c46d6b50789aae2`) | no: same two blockers, `receipt` exit 0, receipt SHA-256 `sha256:51ec30dc1f1f2b2b19ef413709290f9a56a29d843a6780f5e1bdae29417deb5e` |

The one-round repair bound is deliberate and symmetric with the other arm: after a failed review the
findings go back once with failing evidence, and a rejected revision is never retried to green
without a changed diagnosis (`docs/design/quality.md`).

## Cost and effort accounting

All numbers below are observed values from the accepted per-run usage ledgers; they are harness cost
estimates summed from the sessions' own recorded usage, **not** provider bills. No price version was
reported, so every row stays `accounting_complete=false` and no provider-reported cost exists.
Missing values are null with a reason, never zero. One model session appears exactly once per run;
the coordinator, gate execution and non-model work are recorded as explicit non-model phases or
charged through the session that actually did the work.

Totals bind the recorded per-run measurement windows, not the sums of the session rows: each total's
`wall` is the receipt's `wall_time` run window (1090/470/2734/2154 s), which also covers coordinator
and gate intervals; the phase rows sum below it (970.2/386.4/2609.2/2084.2 s). Displayed USD values round half-up at six
decimals from the unrounded recorded sums (for example 1.3038045 → 1.303805); the class B totals
therefore differ by 1e-6 from the sum of their displayed, individually rounded phase rows. USD
totals are quoted from the candidate policy's `cost_estimated_usd`, which matches each receipt's
`cost.known_estimated_cost_subtotal` and its `cost.estimated_cost` except for strong-only, where the
receipt's separately accumulated float stores 2.0244584999999997 against the recorded 2.0244585
total — one ulp below the same half boundary, which would round to 2.024458 on its own; the
displayed 2.024459 rounds half-up from the 2.0244585 total. Costs on the two routes were produced by
different harness price tables, so every margin is read together with the cache-neutral sensitivity
below and the price-table limitation.

### `go-bounded-regression`

| Phase (session) | Model | Input | Output | Cached | Estimated USD | Wall s |
| --- | --- | --- | --- | --- | --- | --- |
| Mixed implementation (`01a1e5fc…`) | `deepseek-v4.1-flash-fast` | 16,322 | 27,936 | 1,118,336 | 0.038420 | 190.9 |
| Mixed review (`01a1e609…`) | `gpt-6.1-sol` | 36 | 8,202 | 583,049 | 0.272246 | 241.9 |
| Mixed failed attempt (`01a1e601…`) | `gpt-6.1-sol` | 39 | 7,253 | 634,535 | 0.277918 | 513.4 |
| Strong implementation (`01a1e60e…`) | `gpt-6.1-sol` | 39 | 5,983 | 484,760 | 0.224566 | 138.6 |
| Strong review (`01a1e611…`) | `gpt-6.1-sol` | 36 | 7,811 | 615,740 | 0.276626 | 224.9 |

Task totals: mixed — estimated `0.588584` USD, wall 1090 s, review time 241.9 s, repairs 1;
strong-only — estimated `0.501191` USD, wall 470 s, review time 224.9 s, repairs 0.
The mixed total includes the coordinator-caused review redo (0.277918 USD); excluding it, the mixed
arm's accepted work is `0.310666` USD (0.0384198 + 0.2722457, rounded half-up).
Cache-neutral sensitivity, recomputed from the two receipts by summing only
`known_subtotals.cost.input.sum` and `known_subtotals.cost.output.sum` per phase (dropping
cached-token charges): implementation 0.038420 USD versus 0.059908 USD; review 0.082092 USD versus
0.078182 USD, inverting the near-equal as-run review row (mixed 0.272246 USD versus strong 0.276626
USD, mixed cheaper by 0.004380 USD; cache-neutral strong-only cheaper by 0.003910 USD, mixed/strong
review ratio 1.050012); equal-treatment total 0.120512 USD versus 0.138090 USD. The mixed arm stays
lower on the class totals, with a margin of 0.017578 USD instead of the as-run 0.190526 USD, so no
decision changes; that margin's bulk is the strong route's billed cache-read/cache-write against a
lightweight route that prices cached tokens at zero, and the as-run review row is itself a
cache-pricing artifact.

### `go-native-ownership-settlement`

| Phase (session) | Model | Input | Output | Cached | Estimated USD | Wall s |
| --- | --- | --- | --- | --- | --- | --- |
| Mixed implementation (`01a1e615…`) | `deepseek-v4.1-flash-fast` | 419,277 | 153,835 | 9,606,656 | 0.310385 | 1151.0 |
| Mixed repair (`01a1e62f…`) | `deepseek-v4.1-flash-fast` | 241,205 | 100,077 | 2,182,208 | 0.192454 | 537.9 |
| Mixed first review (`01a1e628…`) | `gpt-6.1-sol` | 39 | 9,456 | 811,946 | 0.380958 | 459.5 |
| Mixed second review (`01a1e639…`) | `gpt-6.1-sol` | 45 | 11,173 | 1,052,385 | 0.420007 | 366.8 |
| Strong implementation (`01a1e640…`) | `gpt-6.1-sol` | 84 | 29,131 | 2,279,462 | 0.791140 | 746.0 |
| Strong repair (`01a1e655…`) | `gpt-6.1-sol` | 39 | 8,357 | 705,831 | 0.288571 | 194.4 |
| Strong first review (`01a1e64c…`) | `gpt-6.1-sol` | 57 | 15,526 | 1,478,518 | 0.555487 | 580.8 |
| Strong second review (`01a1e658…`) | `gpt-6.1-sol` | 54 | 8,978 | 1,196,646 | 0.389260 | 505.1 |

Mixed-arm totals: estimated `1.303805` USD, wall 2734 s, review time 826.4 s, repairs 1, accepted
`false`. Strong-only totals: estimated `2.024459` USD, wall 2154 s, review time 1085.8 s, repairs 1,
accepted `false`. Neither figure buys an accepted result, so neither supports a cost or routing claim.

## Candidate policy record

The machine-readable candidate record is
[`docs/evaluations/model-routing/0008b-policy.json`](../evaluations/model-routing/0008b-policy.json);
it carries the per-class decision, the evidence behind it, every negative finding, the scope
statement and the limitations. This record is **produced and awaiting the owner's verdict**; no model
issues owner acceptance, and no routing policy is promoted here.

Per tested class:

| Class | Decision | Basis |
| --- | --- | --- |
| `go-bounded-regression` | `promote-lightweight` | Both arms accepted with the same gates on the first execution and a passing independent review; on equal treatment the mixed arm's accepted work was cheaper (0.310666 USD versus 0.501191 USD; cache-neutral 0.120512 USD versus 0.138090 USD), with the as-run not-cheaper total carried as a negative finding |
| `go-native-ownership-settlement` | `insufficient-evidence` | Neither arm reached an accepted result within its single bounded repair round; the lightweight route's first revision drew two high-severity findings and the strong route's first revision one medium finding, and both repaired revisions ended on one open medium finding in the same non-durable supervisor `Close` branch |

The record also carries the unknown-billing limitation, the single-sample limitation, the
Windows-only surface and the explicit statement that no claim is made for untested Go work, Elixir or
TypeScript work.

Artifact digests over the repository's LF line endings: candidate policy
`sha256:82852890944b405b99da0c95b2404547be6fea994301c631b65b5145bf3f767d`, controls record
`sha256:4ab181863fbe4a2ddec4cdb7270ffb220e10e5c9ee930d90e1a63f51ca5407aa`. The four receipts are
bound by the policy record's per-arm `receipt_sha256`; the class A review records by
`evidence.frozen_review_sha256` inside each receipt and by the policy record's
`frozen_input_digests.review_sha256`; and the review records published as attempt histories (the
class A mixed arm and both class B arms) by the policy record's
`review_attempts_ref`/`review_attempts_sha256` pairs. Each digest is recomputable with
`sha256sum <file>` over the file's LF bytes. The controls record's internal `output_sha256` digests
bind the recorded output text with CRLF line endings, and its `record_rendering` block names the
renderer and token used for host paths.

## Closure and simplification

The goal file moved to [`docs/goals/archive/0008b-go-model-routing.md`](../goals/archive/0008b-go-model-routing.md)
without a text change: 5359 bytes, SHA-256 `d9ed376ff86d952fe487898163de023ed5c3d564b0637661c5729504799d3399`,
byte-identical to the pre-archival file, and it contains no relative link that archival would break.
The active goal path is absent and the goals index carries the closure paragraph.

Scoped simplification removed nothing from the reused 0008a tooling: this goal added two frozen task
specifications, one machine-readable policy record, one controls record and this evidence record, and
changed no executable source. The one-writable-mechanism rule was preserved: the coordinator used the
existing runner, exporter, source binder and Go event validator unchanged, and the two arms are
ordinary invocations of the already-configured routes rather than a new scheduler or profile.

## Observed refusal controls

[`0008b-controls.json`](../evaluations/model-routing/0008b-controls.json) records the argv, exit
code, output text and output digest of nine controls, all run on disposable copies; the unmodified
copy control proves the copies are faithful. The argv and outputs name the disposable root as the
`<disposable-root>` token (rendered by `scripts/ci/model-routing-record-sanitize.py`; see the
record's `record_rendering`), and each output digest binds the recorded, rendered text. Eight
returned exit 1:

| Control | Mutation | Observed |
| --- | --- | --- |
| Baseline class A revision | none (frozen base revision) | `check` exit 1: the Go run exits 0 but the validator refuses both absent required names |
| Baseline class B revision | none (frozen base revision) | `check` exit 1: the platform package passes but the validator refuses both absent required names |
| Stale content revision | append one newline to the produced test | `accept` exit 1: current source does not match the sealed subject |
| Missing required test | delete the produced test file | `accept` exit 1: current source does not match the sealed subject |
| Fabricated billing | replace null `reported_cost` with `0` | `accept` exit 1: the submitted receipt does not match derived evidence |
| Weakened frozen contract | remove one required name from the frozen task | `accept` exit 1: frozen task bytes changed after prepare |
| Weakened baseline test | insert `t.Skip` into an existing test | `seal` exit 1: a change to an existing baseline test is refused |
| Skipped required test | insert `t.Skip` into a required new test | `check` exit 1: validator reports `run:1,pass:0,skip:1` |

## Verification boundaries and limitations

- Provider billing is unverified everywhere: no provider-reported cost and no price version were
  observed, so every receipt stays `accounting_complete=false`, and total and per-phase costs are
  harness estimates over observed usage, not bills and not measured savings. An unversioned harness
  estimate is not a financial lower bound.
- The two routes were priced by different harness price tables, recovered from the recorded
  per-category cost subtotals: the lightweight route at 0.30 USD/M input, 1.20 USD/M output and
  0.00 USD/M cached tokens, the strong route at 2.00 USD/M input, 10.00 USD/M output, 0.10 USD/M
  cache-read and 2.50 USD/M cache-write. As-run margins therefore include a cache-pricing
  asymmetry; the cache-neutral sensitivity above preserves the direction with a smaller magnitude,
  and no saving percentage is claimed either way.
- Prompt identity is bound per arm as observed hashes; the reviewer prompts differ between arms
  because they embed each arm's own sealed subject, checkpoint and gate evidence, and the worker
  packets differ only in the workspace path line.
- Class B's mixed arm required one repair round and still ended without a passing review, so its
  receipt is a nonaccepted row; nothing in this record converts it into either a pass or a defect
  count for the workflow.
- The recorded intermittent `TestCloseStopsOwnedChildAfterLeaderExit` failure was not reproduced
  locally (10 consecutive passes); its CI observation remains in `docs/TODO.md` as the motivating
  evidence for class B, and neither arm claims to have fixed that flake.
- Both classes ran on the Windows surface only. Linux-specific, race, Control, wire and browser
  gates were not run here and remain pending with their missing environments; no cross-platform
  extrapolation is made.
- Each class/arm is a single sample. No significance threshold is claimed, and repeated runs would
  be needed to separate model variability from run conditions.
- The local coordinator store is not an adversarial append-only audit store; same-user tampering is
  outside the mechanism's guarantee, exactly as recorded for 0008a.

## Repository gates after the runs

No measurement or evaluation run overlapped these; the last model run had already stopped when they
started, so nothing here competes with the measurement harness on this host.
The results below are the coordinator's observations of the 0008b revision; the raw logs stay in the
coordinator store and are not published with this record.

| Gate (from the repository root unless noted) | Observed result |
| --- | --- |
| `python scripts/ci/test-model-routing-evaluation.py` | exit 0, `Ran 39 tests … OK (skipped=2)`, 587 s |
| `python scripts/ci/test-model-routing-omp-usage.py` | exit 0, `Ran 31 tests … OK`, 13 s |
| `python scripts/ci/test-linux-v2-source-subject.py` | exit 0, `Ran 4 tests … OK`, 5 s |
| `go vet ./...` (from `daemon/`) | exit 0, 10 s |
| `go test -json -count=1 -timeout 900s ./...` (from `daemon/`) | exit 1: 1,797 top-level tests, 1,740 pass, 56 skip, 1 fail; the failing test is `TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock` in `daemon/internal/harness/omp` (`adapter_final_message_test.go:44: context deadline exceeded`, 1.38 s) |
| `go test -count=1 ./internal/harness/omp` (from `daemon/`) | exit 0, `ok … 4.631 s` |

The two skips in the runner suite are platform-conditional cases, identified by running exactly those
methods: `test_file_modes_are_bound_when_git_tracks_them` ("executable file modes are not tracked on
this host") and `test_non_utf8_checkout_paths_are_refused` ("non-UTF-8 file names are only
constructible on POSIX hosts"). No assertion was weakened, skipped or made conditional by this goal.

The single suite failure was investigated instead of rerun to green: it does not reproduce in
isolation on either revision (three consecutive `-count=3` runs exit 0 on the unchanged base clone
`5c2e0ada` and on the working tree), the package itself passes alone, and this goal changed no Go
source, so the failure is load/timing-sensitive and not attributable to this increment. It is recorded
as a new intermittent candidate in `docs/TODO.md` with its coordinates and evidence.

## Standalone Done-when mapping

| Required witness | Evidence and observed result |
| --- | --- |
| Mixed-model and strong-only runs over the frozen Go classes, with identical contracts, permissions, budgets, evidence, checks and review per class | Four prepared runs from one base tree digest (`1c6b5add…`) over two frozen tasks (`d3422209…`, `287d0ab9…`); identical worker/reviewer wall-clock budgets, approval mode and packet text; each arm sealed, checked with the same gate argv, and reviewed by a separate strong-route invocation; see the two per-class tables above |
| Actual model identities, provenance, prompt/skill identities, credential route metadata and token/cost rows per accepted change | Observed served identities (`ghc`/`gpt-6.1-sol` `openai-responses`; `tencent-litellm`/`deepseek-v4.1-flash-fast` `openai-completions`), route provenance from `modelRoles` and the headless invocation labels, per-session prompt hashes, credential-route metadata labels, and nullable token/cost rows in the receipts; billing unknown with no price version |
| Total cost covering exploration, implementation, verification, review, repair and failed attempts, plus effort, repairs, failures and limitations per accepted change | Six-stage usage ledgers (model rows for implementation/repair/review sessions, explicit non-model rows for exploration/verification/failed attempts) with per-phase wall times, review time, repair counts and per-receipt acceptance blockers; all limits recorded above |
| Every accepted result passes the applicable Go gates and has an independent semantic review for that exact revision | Class A: both arms `accept` exit 0 with validator exit 0 and pass verdicts bound to `b10945d7…`/`9301f14f…`. Class B: mixed arm nonaccepted after one repair round; strong-only arm see above |
| Candidate policy per tested class: promote-lightweight / strong-only / insufficient evidence, with the evidence behind it | [`0008b-policy.json`](../evaluations/model-routing/0008b-policy.json): per-class decision, decision basis, evidence references and the frozen class list |
| Any class whose acceptance regressed, is not cheaper, or lacks adequate evidence is carried explicitly | The policy record's negative findings carry the class A as-run total that is not cheaper, the class B mixed-arm rejection with its open finding, and the unknown-billing limitation |
| No claim for untested Go work, Elixir or TypeScript | The policy record's scope statement; both classes' tested surface is named and no cross-surface inference is made |
