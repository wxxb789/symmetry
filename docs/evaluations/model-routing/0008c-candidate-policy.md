# 0008c candidate routing policy for Elixir work

Status: **candidate, produced and awaiting the owner's verdict**. This record is the run's
produced result; it is not owner acceptance, grants no `achieve`, and promotes nothing by itself.
The binding policy remains [docs/design/quality.md](../../design/quality.md); the receipts behind
every row live beside this file, and the contract is
[docs/goals/archive/0008c-elixir-model-routing.md](../../goals/archive/0008c-elixir-model-routing.md).

## What was measured

Two Elixir task classes, fixed before either arm ran, each executed twice at the same frozen base
`5c2e0ada6a3fd669c5ed2d2bc4b50ff9fed3b12a` in its own isolated checkout, with identical frozen task
bytes, allowed/protected paths, budgets, gates, evidence shape and review route. Only the
implementer route differs between the arms of a class.

| Class | Work | Mixed arm implementer | Strong-only arm implementer |
| --- | --- | --- | --- |
| `elixir-established-pattern` | persist `stale` for a definite provider identity loss instead of `failed`, keep every other failure classification, surface attention in the read model | configured lightweight task route (`task.agentModelOverrides.sonic` -> `modelRoles.flash_max`) | configured strong route (`modelRoles.sol_high`, the configured default) |
| `elixir-db-concurrency-authority` | commit a non-authoritative `syncing` marker under the resource's optimistic lock before any provider call, finalize once, single winner under concurrency, notification after the run | same configured lightweight route | same configured strong route |

Both arms of a class ran in the same wave of the same host and the same execution surface; both
were sealed, gated, reviewed, receipted and accepted independently. The reviewer route is the
configured `reviewer` role (`modelRoles.flash_max`) in a separate invocation for every run.

| Run | Receipt | Subject | Verdict |
| --- | --- | --- | --- |
| class A mixed | `0008c-a-mixed.json` | `sha256:5c8b1a800a17acb7231545ce3168b04f84a3fc3d21f6608e6ea6077f105d2ddc` | accepted; review `pass`, four resolved `info` findings |
| class A strong | `0008c-a-strong.json` | `sha256:09dd5f8840f3d3fed81cc4c22e1fe7f9a981c78c941b27de8309f7b5643999af` | accepted; review `pass`, four resolved `info` findings |
| class B mixed | `0008c-b-mixed.json` | `sha256:986bbd6e8818d128a96dc84eb38f1267e134041e63dd5f562674e27217386864` | accepted; review `pass`, three resolved `info` findings |
| class B strong | `0008c-b-strong.json` | `sha256:2d98eaec565495be3bafc12559b99f9bc4beb51837fc8faf2ab3b0630bf3f2e1` | accepted; review `pass`, four resolved `info` findings |

## Per-class verdicts

Criteria, fixed before the results were known:

- **promote-lightweight** for a class only when the mixed arm's accepted revision passed every
  frozen gate with its named tests executed, its independent review recorded no open finding and no
  gate weakening, no acceptance regression appears against the strong-only arm on the same class,
  and the mixed arm's total accounting (tokens, wall time, review effort, repairs and failed
  attempts) is not worse than the strong-only arm's.
- **strong-only** for a class when the mixed arm failed acceptance, regressed acceptance, or was not
  cheaper by the same accounting, or when the strong-only arm's acceptance is the only accepted
  result for that class.
- **insufficient evidence** when either arm's acceptance, review or accounting is incomplete enough
  that the comparison cannot support a verdict, or when the frozen classes did not exercise the
  behaviour the promotion would cover.

A verdict never follows from model identity, effort labels or a single number: it follows from the
pair of accepted revisions with their executed gates, reviews and accounting rows.

### `elixir-established-pattern` — verdict: **strong-only**

Both arms were independently accepted on identical contracts, permissions, gates, evidence shapes
and review route: mixed `sha256:5c8b1a800a17acb7231545ce3168b04f84a3fc3d21f6608e6ea6077f105d2ddc`
(receipt `sha256:93050ced0667d012924564e58032f96fd3244214ac68871e3f552750cc5fa73c`, review `pass`,
four resolved `info` findings, zero repairs, one failed attempt) and strong
`sha256:09dd5f8840f3d3fed81cc4c22e1fe7f9a981c78c941b27de8309f7b5643999af` (receipt
`sha256:5ee9b87bd6d5b26adb4674b1024f8a212e48e6050223703c43e8e3c766f86a7f`, review `pass`, four
resolved `info` findings, zero repairs, one failed attempt). Every frozen gate passed in both arms
with its named tests executed and its trace recorded.

The promote-lightweight precondition fixed before the runs ("the mixed arm's total accounting —
tokens, wall time, review effort, repairs and failed attempts — is not worse") fails on the measured
numbers, so this class gets **strong-only** and no promotion: the mixed arm is worse in input
(260,429 vs 58,320), output (101,774 vs 56,229) and cached tokens (5,104,704 vs 2,957,741) and in
wall time (1,453.6 s vs 1,422.4 s), better only in review effort (175.5 s vs 252.9 s, both reviews
ran on the same route) and equal on repairs and failed attempts. Its harness *estimate* is lower
(USD 0.2002575 vs 0.6404529, both including review and the failed attempt) — that is the one
dimension the mixed arm wins, and it is an estimate, not a bill, so it cannot carry a promotion on
its own. Class A produced two different accepted implementations of the same contract (a
`failure_sync_status/1` helper versus an inlined decision), which is the intended freedom, not a
regression.

Evidence gaps that keep this verdict from being stronger: one sample per arm (no variance), no
provider-reported cost or price version anywhere (`accounting_complete: false` in both receipts),
and a read-model observation from the reviews that no consumer currently distinguishes `stale` from
`failed` in the attention summary.

### `elixir-db-concurrency-authority` — verdict: **strong-only**

Both arms were independently accepted on the amended frozen bytes and identical contracts,
permissions, gates, evidence shapes and review route: mixed
`sha256:986bbd6e8818d128a96dc84eb38f1267e134041e63dd5f562674e27217386864` (receipt
`sha256:ceae34ce3507075c4fe26dc719fa8d3caff8b98b4817b576a4f63798a6e6f74a`, review `pass`, three
resolved `info` findings, zero repairs, one failed attempt) and strong
`sha256:2d98eaec565495be3bafc12559b99f9bc4beb51837fc8faf2ab3b0630bf3f2e1` (receipt
`sha256:af12819d6806509f78c0379f561fd3d8c01e5f0a3116335cd362d9c55918f34a`, review `pass`, four
resolved `info` findings, zero repairs, one failed attempt). Every frozen gate passed in both arms
with its named tests executed — five focused names plus the four provider-broker names per arm.

The same promote-lightweight precondition fails here, so this class also gets **strong-only** and no
promotion: the mixed arm is worse in input (433,043 vs 58,763), output (207,578 vs 70,858) and
cached tokens (7,424,896 vs 4,466,218) and in wall time (1,772.4 s vs 1,611.4 s), better in review
effort (253.5 s vs 339.1 s) and equal on repairs and failed attempts. Its harness estimate is lower
(USD 0.3790065 vs 0.8367779) and, as in class A, that single estimated dimension cannot carry a
promotion. The class also cost more on both arms than class A in absolute terms because the frozen
contract is larger (transaction, lock, rollback and notification ordering).

Two class-specific gaps belong to this verdict: the class's first frozen contract was unsatisfiable
against a protected baseline test and was corrected (with one added rule and its own named test)
before any class-B acceptance, so every class-B number on both arms comes from the corrected bytes
and the original formulation has no accepted result; and only one failed attempt per arm was
observed, the strong one being a correct escalation rather than a defective implementation.

## Negative findings, not-cheaper classes and evidence gaps

Every acceptance regression, every class the mixed workflow did not make cheaper, and every evidence
gap are recorded here rather than in the verdicts, so a promotion cannot be read without its
negatives. Acceptance regressions and review findings are appended per run as the receipts are
accepted.

Measured asymmetry that every verdict must carry, from the worker rows alone (harness estimates, not
bills):

| Class | Mixed arm tokens (in/out/cached) | Mixed harness estimate | Mixed wall | Strong arm tokens | Strong harness estimate | Strong wall |
| --- | --- | --- | --- | --- | --- | --- |
| A | 108,909 / 47,162 / 2,981,760 | USD `0.0892671` | 438.6 s | 48 / 9,891 / 848,804 | USD `0.321512` | 293.2 s |
| B | 268,410 / 95,175 / 3,320,128 | USD `0.194733` | 525.7 s | 54 / 17,325 / 1,130,699 | USD `0.4618031` | 428.6 s |

So on this host the lightweight implementer was 2.4–3.6× cheaper by harness estimate but 1.2–1.5×
slower in wall time, and it consumed far more cached tokens. Neither number is a bill, neither is a
saving, and both arms' verification and review costs are counted separately in their receipts.
The token columns mix two providers whose cache accounting differs, so token totals are comparable
only within one provider; the harness USD estimate is the one common unit across providers, and it
is an estimate with no provider bill and no price version.

The verdicts above use the receipts' run totals, not the worker rows, so review and failed-attempt
work is charged to the arm that caused it:

| Class and arm | Tokens in / out / cached | Harness estimate | Accounted wall time | Review effort | Repairs / failed attempts |
| --- | --- | --- | --- | --- | --- |
| A mixed | 260,429 / 101,774 / 5,104,704 | USD `0.2002575` | 1,453.6 s | 175.5 s | 0 / 1 |
| A strong | 58,320 / 56,229 / 2,957,741 | USD `0.6404529` | 1,422.4 s | 252.9 s | 0 / 1 |
| B mixed | 433,043 / 207,578 / 7,424,896 | USD `0.3790065` | 1,772.4 s | 253.5 s | 0 / 1 |
| B strong | 58,763 / 70,858 / 4,466,218 | USD `0.8367779` | 1,611.4 s | 339.1 s | 0 / 1 |

Per-run review findings, by arm (all `resolved`; none is a gate weakening):

- class A mixed (review `sha256:2ef575e2…`, reviewer `deepseek-v4.1-flash-fast`):
  `named-test-evidence-integrity`, `portal-bundle-untracked-in-change-set`,
  `read-model-attention-not-discriminating` — no consumer distinguishes `stale` from `failed` in the
  read model's attention summary — and `provider-action-projection-widened` — the same failure write
  also serves provider-action 404s, so a bound resource flips to `stale` there too.
- class A strong (review `sha256:9a712738…`, reviewer `deepseek-v4.1-flash-fast`):
  `focused-gate-re-executed-by-reviewer`, `independent-negative-control-revert` (the reviewer
  reverted the changed line in a scratch copy and two of three named tests failed),
  `not-found-classification-is-not-sync-path-gated` (the same widened projection as above) and
  `portal-bundle-is-provisioned-not-authored`.
- class B mixed (review `sha256:1c9c68cd…`, reviewer `deepseek-v4.1-flash-fast`):
  `marker-non-authority-by-design` (the marker never consumes the optimistic-lock version and no
  reaper or timeout was added), `rollback-write-scope` (a refused finalization restores only the
  marker's own two fields) and `portal-bundle-provenance`.
- class B strong (review `sha256:2918a91d…`, reviewer `deepseek-v4.1-flash-fast`):
  `accepted-sync-boundary`, `marker-rollback-cas`, `raw-update-all-bypass` (the marker bypasses the
  changeset path on purpose, which is what keeps it from consuming the lock version) and
  `subject-scope`.

Class B carries process negatives that no verdict may hide. First, the class-B contract had to be
corrected before any class-B acceptance: the first strong-arm attempt stopped and escalated a real
conflict between the frozen requirement and a protected baseline test — the archive race asserts the
resource is left at `"unknown"`, while a marker committed before the provider call leaves
`"syncing"` — and the corrected task adds one restore rule with its own named test. That attempt
touched no file and is charged as the strong arm's failed attempt; the mixed arm's first attempt was
aborted by the coordinator when the task changed, produced no answer, and is charged as the mixed
arm's failed attempt — an abort, not a behavioural finding about that model. Second, the two arms
independently converged on the same non-authority design (a compare-and-set marker that does not
consume the lock version), so class B measures how each workflow executed a design both reached
rather than two rival designs.

Evidence gaps that no verdict may paper over: no provider-reported cost or price version exists for
any row (accounting is explicitly incomplete); cross-provider token totals are not comparable
(cache accounting is per provider and `cached` counts cache read plus cache write there), so only
the harness estimate is a common unit — and it is an estimate, with no provider bill and no price
version; one run per arm per class gives no variability estimate; the classes exercise only the
resource-synchronization path (not goals/orchestration
settlement, protocol, chat or portal surfaces); the "notification" element is the syncer's
process-local completion message only, not a durable wakeup; the review route is the configured
reviewer role (invocation-separated, not a different model family), so correlated mistakes are not
excluded; and the whole sample is one Windows host with one PostgreSQL 18.6 container.

## Scope statement

- No claim is made for **untested Elixir work**: the verdicts cover only the two frozen classes and
  the revisions named in their receipts, on the execution surface recorded in each row's
  `environment` field. Other Elixir areas (goals/orchestration settlement, protocol/model-facing
  paths, chat, portal controllers, numeric or algorithmic work) were not tested and get no verdict.
- No claim is made for **Go** or **TypeScript** work, and none for any other repository surface.
- No claim of general quality equality between the workflows is made; a clean sample supports only
  the tested scope.
- No savings percentage, measured saving or invented cost figure is stated anywhere: costs appear
  only as observed token rows with their provenance, unknown billing is `null` with a price-version
  gap, and harness estimates are labelled as estimates, never as provider bills.
- No model name is hardcoded into domain policy, configuration default, profile or credential: this
  record names the routes it observed as evidence, and the production routing stays whatever the
  existing configuration says until the owner decides otherwise.
- The integration rule fixed before the runs: the strong-only arm's accepted artifact is the merge
  candidate for its class, and a mixed arm's accepted artifact stays sealed evaluation evidence with
  its own Subject and receipt. Any deviation is recorded explicitly below.
