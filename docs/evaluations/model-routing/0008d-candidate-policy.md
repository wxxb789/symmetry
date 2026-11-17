# 0008d candidate policy - TypeScript carrier

Status: produced and awaiting the owner's verdict. This file is the human rendering of the
machine-readable record `0008d-candidate-policy.json`; the record's per-class verdicts are derived
from the four published receipts and their review records, not asserted by hand:

| Run | Receipt | Review record | Verdict |
| --- | --- | --- | --- |
| a-mixed | [0008d-a-mixed.json](0008d-a-mixed.json) | [0008d-a-mixed.review.json](0008d-a-mixed.review.json) | accepted, review pass |
| a-strong | [0008d-a-strong.json](0008d-a-strong.json) | [0008d-a-strong.review.json](0008d-a-strong.review.json) | accepted, review pass |
| b-mixed | [0008d-b-mixed.json](0008d-b-mixed.json) | [0008d-b-mixed.review.json](0008d-b-mixed.review.json) | accepted, review pass |
| b-strong | [0008d-b-strong.json](0008d-b-strong.json) | [0008d-b-strong.review.json](0008d-b-strong.review.json) | accepted, review pass |

## Criteria, fixed before the results

- **promote-lightweight** for a class only when the mixed arm's accepted revision passed every frozen
  gate with its named tests executed, its independent review recorded no open finding and no gate
  weakening, the strong-only arm was also accepted on the same class (so the comparison exists), and
  the mixed arm's total accounting (input/output/cached tokens, wall time, review effort, repairs and
  failed attempts) is not worse than the strong-only arm's.
- **strong-only** for a class when the mixed arm failed acceptance or was not cheaper by that same
  accounting, or when the strong-only arm's acceptance is the only accepted result for that class.
- **insufficient-evidence** when either arm's acceptance, review or accounting is incomplete enough
  that the comparison cannot support a verdict (the frozen wording carried by the JSON record).

The derivation compares every term that wording names, and each class's `compared_dimensions` list in
the JSON record is the derivation's own reading list: input, output and cached tokens, accounted wall
time, review effort, repairs and failed attempts. Repairs entered that comparison after an audit
showed the first version published the criterion's repairs term without evaluating it; both classes
carry zero repairs in both arms, so no verdict moved. A cheaper harness estimate is never on its own a
promotion, and a class that is only cheaper in the estimate while worse in the token rows is recorded
as not-cheaper, not as a near miss.

## Verdicts

### `ts-runtime-capability-rows` - **strong-only**

Both arms were independently accepted on identical frozen bytes, gates, permissions, budgets,
evidence shapes and review route: mixed `sha256:03309e00d13932f9ba1f06344a6f1ef5951ff272f2a757d9262459c3411723db`
(review `pass`, four resolved `info` findings, zero repairs, one charged failed attempt) and strong
`sha256:6519aefe65750295973a07e365ae9c41e83f3117bace5626e5f1a0de79a7217e` (review `pass`, three
resolved `info` findings, zero repairs, one charged failed attempt). Every one of the eleven frozen
gates passed in both arms, including the structured vitest report with all three named cases printed
once as passed and the browser gate at both widths.

The promote-lightweight precondition fixed before the runs fails on the measured numbers: the mixed
arm is worse in input (178,066 against 101,204), output (99,237 against 44,968) and cached tokens
(4,950,400 against 2,888,136), in accounted wall time (1,166.1 s against 1,024.4 s) and in review
effort (168.3 s against 127.7 s), and equal on repairs and failed attempts. Its harness estimate is
4.0x lower (USD 0.1725042 against USD 0.6868291, both including review and the charged failed
attempt) - the one dimension the mixed arm wins, an estimate rather than a bill, and not a
promotion on its own. The two arms built different accepted implementations of the same contract
(`runtimes.tsx` +46/-19 against +19/-1), which is the intended freedom.

### `ts-command-receipt-truth` - **strong-only**

Both arms were accepted: mixed
`sha256:d2c533c19e605f15546714083fbdec0de5e031138390fca0e5c83b8829a3e32a` (review `pass`, five
resolved `info` findings, zero repairs, one charged failed attempt) and strong
`sha256:d7dbee102448a942957280980f9d4eea2a70b2afad09fb7c998271b226db9f06` (review `pass`, nine
resolved `info` findings, zero repairs, one charged failed attempt), again with all eleven frozen
gates passing per arm including the parsed vitest report and both browser widths.

This class is the honest asymmetric case: the mixed arm is *better* on output (122,546 against
125,689), cached tokens (6,968,256 against 8,876,103), accounted wall time (1,358.6 s against
1,575.8 s) and review effort (223.4 s against 462.9 s), and its estimate is 4.4x lower (USD
0.2040753 against USD 0.8930115) - but it is worse in input (190,067 against 101,752), so the fixed
"not worse in every measured dimension" precondition fails and the verdict is strong-only. Recorded
as an asymmetry, not smoothed into either a promotion or a claim that the mixed route is worse
overall. The mixed arm's review was also the shorter one (five findings in 223.4 s against nine in
462.9 s), so review effort on this class tracked the reviewer's own investigation depth.

## Negative findings carried, not recommended away

- **Attempt accounting.** Three check attempts were refused before the accepted ones and every
  charged session is recorded: class A's mixed arm consumed four check attempts — attempt-0001 lost
  its checkout source to the vitest run state (`source changed while executing this gate`); the
  interrupted attempt-0002 was quarantined with its own note after its `frontend-tests` gate failed
  with `EPERM … frontend/node_modules/.vite-temp/vitest.config.ts.timestamp-*.mjs`; attempt-0003
  failed the same way in `focused-tests`, `frontend-tests` and `frontend-build` (write denials inside
  a tool-cache directory the sealed manifest excludes, with the subject hashes unchanged) and failed
  the session-expiry browser case at `1 failed / 25 passed`; attempt-0004 accepted all eleven gates.
  The origin of those write denials is not established by any stored artifact (no probe, provisioning
  or ACL log was kept, and the accepted attempts' gate windows do not overlap); the earlier rendering
  of this finding attributed attempt-0003 to an "expiry-server TTL boundary", which the stored gate
  evidence does not carry and which is corrected here. a-strong, b-mixed and b-strong accepted on
  their first check attempt. The four first-attempt implementation sessions (the contradictory
  protected-path clause in the first frozen task bytes) are charged as `failed_attempt` phases with
  observed usage.
- **Not-cheaper classes.** Both classes are recorded as not-cheaper despite estimates 4.0x and 4.4x
  lower for the mixed arms, because the fixed accounting comparison fails on input tokens (and, for
  class A, on every token row and wall time). See the two verdict sections for the exact numbers.
- **Cost provenance.** No provider-reported cost and no price version exist on these routes; every
  receipt records `cost_provenance: "estimated"` with the harness version, tokens are
  provider-unreported for cached input, and both receipts set `accounting_complete: false`. Unknown
  billing is recorded as unknown with token counts, never as zero.
- **Coverage boundary.** The new capability rows are proved by the focused component test's three
  named cases - each verified non-vacuous by the reviewers through mutation and revert in throwaway
  copies - plus the existing frozen browser case for the runtimes page; no new browser case was added
  for the two rows, and all four review records carry that boundary as an explicit finding
  (`a-mixed` `browser-rows-coverage`; `a-strong`, `b-mixed` and `b-strong`
  `browser-coverage-boundary`). The b-strong review adds further boundary findings
  (`utf8-lossy-boundary`, `vitest-run-caches`), all resolved as recorded statements.
- **One sample per arm.** Nothing here estimates run-to-run variance; a single cheaper estimate
  cannot establish a promotion even where token rows favour the mixed arm.
- **Environment substitution.** The isolated checkouts omit the manifest's tool-cache paths
  (`.vite`, `.vite-temp`, `.vitest-cache`) so recorded run state cannot be mistaken for source; the
  package manifests stay bound. The browser gate's HTML report is redirected out of the checkout and
  the shared capture directory is cleared before each attempt - recorded rather than silent.
- **Record rendering.** No host-specific absolute path ships in any published 0008d record: the four
  receipts are published as the runner's derived bytes (`accept` re-derives each one byte for byte)
  and both narrative records name the private store root only as `<store>`.
  `scripts/ci/model-routing-record-sanitize.py --root <store> --check` still exits 1 on the records
  that deliberately keep non-host absolute text, because the tool renders every absolute path and not
  only host ones: the literal route `/portal/runtimes` in the a-* receipts' frozen `outcome` claim
  and in this record's prose, and the already-rendered `path-sha256:` tokens inside the class-A
  records (`019ba55f…` for the fragment `/||` in the a-mixed pair; `e11f3495…`, `810a366d…` and
  `523fbab2…` for the a-strong pair's quoted contract paths). Re-rendering a receipt would break
  byte-for-byte re-derivation, so the tokens stay as they are; `docs/TODO.md` records the correlation
  aid and this boundary.

## Evidence gaps and limitations

- The class-A task's first frozen bytes were internally contradictory (`control/**` protected while
  the gates rebuild `control/priv/static/portal/**`); the corrected bytes are the sealed ones, the
  contradiction and its charged cost are recorded, and no gate was weakened by the correction.
- Class A's arms were checked on four attempts, not one; the accepted attempts are the only ones
  cited as evidence.
- The two implementations inside a class differ in shape (class A +46/-19 against +19/-1 in
  `runtimes.tsx`; class B +37/-7 against +18/-4 in `api.ts`), so the comparison is of two accepted
  solutions of the same contract, not of one solution produced twice.
- One independent semantic review exists per accepted revision and there is no second reviewer or
  repeated run per arm.
- All cost figures are harness estimates; provider billing is unknown on these routes.
- Nothing in this increment re-measured Go or Elixir work, and no untested TypeScript class is
  characterised.

## Scope statement

This policy record covers exactly two TypeScript classes at base commit
`dacec45034abc667ae65d41e1f778a896ec340c7`: `ts-runtime-capability-rows`
(`frontend/src/features/runtimes/runtimes.tsx` plus its focused test) and `ts-command-receipt-truth`
(`frontend/src/services/api.ts` plus its focused test), each run twice with identical frozen task and
packet bytes, gates, permissions, budgets, evidence shapes and review route. It makes no claim about
any other TypeScript surface, about Go work (`0008b`) or about Elixir work (`0008c`); it adopts no
routing policy by itself, changes no configured route, cap or credential, and is produced and
awaiting the owner's verdict.

## What this record does not do

It recommends no routing change: neither class reaches a promote-lightweight verdict, so this letter
adds no new weight to the `strong-only` results of `0008b` and `0008c` and recommends no class for
cheapening. Adopting, rejecting or narrowing any policy remains the owner's decision, and nothing
here signs for the owner.
