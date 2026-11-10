# Final independent local test-ablation review

Date: 2026-09-29  
Task: `st_01a0ed96` / `final-local-ablation-review`  
Baseline HEAD: `d14debbf6ae3ded09e5013ef90952d347c01474d`  
Verdict: **PASS for this local source/evidence slice; no remaining findings**

## Review identity and constraints

Actual served route was `deep-low`. The task environment reported provider/model
`ghc/gpt-5.6-sol-fast` with reasoning level `medium`.

This was an independent read-only review of requirements, current source, Git
diffs, and experiment reports. I did not run tests, start services, use
credentials, mutate fixtures/source/index, or promote native, Goal, merge, or
pending acceptance. The only repository write was this assigned report.

The review is limited to:

- `daemon/internal/app/app_test.go`: the retained startup test and
  `terminalRecoveryControl`;
- `control/test/symmetry_control/goals/contract_validation_test.exs`;
- `control/test/symmetry_control_web/controllers/goal_controller_test.exs`;
- `scripts/ci/assert-go-test-json.py` and its regression test;
- `.github/workflows/ci.yml`: the C5 ordinary-stream peer gates and the four C4
  parser replacements.

## Findings, severity ranked

**Remaining findings: none in the reviewed source slice.**

### RESOLVED - Exact Control public error classification coverage restored

**Paths:**

- `control/test/symmetry_control/goals/contract_validation_test.exs:64-107`
- `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs:47-52`
- `contracts/fixtures/manifest.json:365,388,453,462,558,581,597,975,1026,1035`

The prior HIGH finding was valid because those manifest records establish only
invalidity unless semantic-error metadata is present. The current dedicated
table test now independently preserves every removed public result contract:

- tuple tag families for ContextSnapshot `content_hash_mismatch`, TaskResult and
  Evidence `subject_hash_mismatch`, and Evidence
  `evidence_identity_mismatch`;
- exact `{:duplicate_predicate_id, "tests"}` tuples through GoalRevision,
  ContextSnapshot, and PlanProposal public validators;
- exact atom reasons for GoalCreate
  `deterministic_acceptance_contract`, Admission
  `provider_scope_resource_operations_mismatch`, and Admission
  `provider_change_target_branches_must_differ`.

The table invokes each affected public validator with the original canonical
invalid fixture, so a wrong-tag remapping can no longer hide behind the
manifest's generic non-`:ok` comparison. It restores the tag/tuple shapes and
the only exact detail previously asserted (`"tests"`) without changing the
manifest or production validation. No lost error shape/detail contract remains
from the reviewed removal.

Executable verification now discriminates the correction. With the GoalCreate
wrong-tag mutant compiled only inside each isolated BEAM process, the exact
prior `596086...` test snapshot plus manifest remained green (12 tests, 0
failures, exit 0), while the current table caught
`{:error, :invalid_goal_create}` instead of
`:deterministic_acceptance_contract` (13 tests, 1 failure, exit 2, expected-red).
A fresh unmutated BEAM then passed the full current suite. The finding is
resolved in both source and failure-oriented evidence.

## Source-grounded checks with no additional finding

### Startup consolidation

Current `daemon/internal/app/app_test.go:4099-4123` uses a ten-second context
bound, registers once, proves registration preceded flushing, requires exactly
one terminal transition attempt, and reloads the retained journal after the
injected failure. `terminalRecoveryControl` at `:8439-8467` cancels only from the
post-registration completed transition when `cancelAfterFlush` is set. Thus the
survivor cannot succeed merely because cancellation prevented the flush, and a
missing transition fails at the bounded context rather than hanging forever.
The experiment's ordering, duplicate-registration, and failed-deletion mutants
exercise the three merged obligations. No additional startup finding was found.

The current App test hash differs from the startup experiment's original final
hash because the parent subsequently added the bounded context guard. Final
reported executions now bind the reviewed hash directly: native Windows App
exit 0 with 897 pass events and 1 skip in 193.001s, and Linux App race exit 0
with 1,041 pass events and 8 skips in 812.237s.

### Direct Plug and routed HTTP duplicate-member boundaries

The direct Plug suite retains recursive escaped duplicate detection, escaped
route-prefix decoding, protocol and portal prefixes, and safe 400 envelopes.
The routed tests at
`control/test/symmetry_control_web/controllers/goal_controller_test.exs:51-69`
and `:216-234` use otherwise-valid Goal-create JSON on canonical protocol and
portal routes; the portal case includes `"title"` plus escaped
`"\u0074itle"`. This closes the unrelated-400 weakness documented by the
experiment. The removed command-route examples used the same parser/classifier
boundary, while direct Plug tests still cover escaped route segments. No
additional local boundary or authentication loss was found.

### C5 ordinary-stream peer gates

`.github/workflows/ci.yml:1316-1350` captures the one ordinary `go test -json
./...` stream, prints it, applies package-scoped gates for the Pi and OpenCode
parents/subtests, preserves the Go process exit, and leaves the race and build
gates present. `scripts/ci/assert-go-test-json.py:30-64` requires each selected
name to have exactly one run and one pass with no skip, rejects descendant
skips, rejects malformed lines, and makes all fail events globally fatal even
when package filtering is enabled. The six-method regression suite covers
missing/duplicate/pass/skip, foreign-only/collision, foreign failure, and
malformed input. No additional local C5 defect was found.

### C4 shared-parser replacements

The four reviewed workflow call sites retain their native commands and opt-in
environment, capture the real Go exit, and invoke the package-scoped helper:
Linux Pi at `.github/workflows/ci.yml:389-424`, Windows Pi at `:1021-1050`,
Windows OpenCode at `:1543-1571`, and Windows containment at `:1614-1628`.
Linux continues to tee the merged stream to its log and provenance; Windows
separates stdout JSON from stderr diagnostics, emits both to the existing log
surface, and preserves provenance where that job already had it. Go exit takes
precedence over helper rejection. No native command, platform condition,
authentication condition, or receipt witness was removed by these four parser
replacements, and no wrapper-only abstraction was introduced beyond reuse of
the already shared CLI.

## Bound current source hashes

SHA-256, read from the current worktree rather than inherited from worker
snapshots:

| Path | SHA-256 |
| --- | --- |
| `daemon/internal/app/app_test.go` | `cc79ef49f5da5d056e980bdbeea3b01e866f609c5242b70824f0276e835b9c85` |
| `control/test/symmetry_control/goals/contract_validation_test.exs` | `3d71579ebda251a4f20b4902f63620f7dae8669293d4997f0bdef2ca175b9100` |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | `d81e5f32e0a7b5f3d51b2e574b4aa10e4260afb515fb265bc6564698a32f3c4e` |
| `scripts/ci/assert-go-test-json.py` | `14e658cab38af077348ff3c74f1786d38eb91b59dfee0f80e805ff51440a2e0a` |
| `scripts/ci/test-linux-v2-go-events.py` | `dc3f4fe338de0f7460693a9d7f0057e98d2484a9d33ff32f45fe70ba7672f4d0` |
| `.github/workflows/ci.yml` | `d549e76805589c93fc9d1f1fe5bc18f378861000c92f02ac4b2ba7778921a453` |

Evidence-report SHA-256 values reviewed:

| Report | SHA-256 |
| --- | --- |
| app startup | `326edbd8e3ea39569b6039c8b2b83f3ca877381a7341f14b7068ddbe959ca405` |
| Control | `e2672276b4f4171391a6df202dc01e9e66ccc705d72e4ee095db7471e7a1c6f9` |
| contracts | `73b76e1edd0360a26d5d2acf4a77b529b065e041136a85ab14e879d0f3bef8f7` |
| Go probe | `f7f04b84d20688f12c75ad17d4fed2975036827e1ceadaf378d8f713d405b1f0` |
| CI peer | `2df049683fce4256bc1f125fae33b6b4b793a838cefd129aa1b1725a1a3f8e5e` |
| inline parser | `8e81e32f72d2f9fb86954a65959ca104148d8f0c05c5a788cbbe62b58bd38aa2` |

## Evidence and coverage limits

I inspected, but did not re-execute, the reports' expected-red mutants and green
commands. `git diff --check` over the six scoped implementation/test files
returned exit 0 during this review; this is a whitespace check, not behavioral
verification.

Final Go evidence reported after the first review now covers the exact current
App hash `cc79ef49f5da5d056e980bdbeea3b01e866f609c5242b70824f0276e835b9c85`:

- native Windows App: exit 0, 897 pass events, 1 skip, 193.001s;
- Linux App race: exit 0, 1,041 pass events, 8 skips, 812.237s.

Corrected Control evidence reported for the exact current test SHA-256
`3d71579ebda251a4f20b4902f63620f7dae8669293d4997f0bdef2ca175b9100`:

- prior `596086...` snapshot plus manifest under the isolated GoalCreate
  wrong-tag mutant: 12 tests, 0 failures, exit 0;
- corrected current focused suite under the same isolated mutant: 13 tests,
  1 failure, exit 2 expected-red, observing `{:error, :invalid_goal_create}`
  instead of `:deterministic_acceptance_contract`;
- fresh unmutated `mix test --seed 0`: 800 tests, 0 failures, 20 skipped,
  366.0s, exit 0;
- `mix format --check-formatted`: passed;
- `mix compile --warnings-as-errors`: passed.

The mutant existed only in each isolated BEAM process. All production source
files remained unchanged; in particular,
`control/lib/symmetry_control/goals/contract_validation.ex` remains SHA-256
`5b5170dd95621b43c1eb0b3ddc36ab5bf8a65017af8747ebbffcbf4640e5e755`.
Existing unchanged-file test warnings and expected module-redefinition warnings
were visible rather than suppressed. The owned PostgreSQL cluster
`/tmp/symmetry-ablation-final.Cgnkmf` was stopped and removed.

Control evidence used Elixir 1.19.6, OTP 28, and PostgreSQL 17.11, not the newer
CI matrix. This environment limit remains explicit; it is not a pending local
check and does not claim CI execution.

The focused evidence does not promote native credentialed,
protected-environment, Goal acceptance, merge acceptance, CI-matrix acceptance,
or repository-wide acceptance. The real Windows containment witness reported
by the C4 experiment is limited to that witness and its then-bound
workflow/helper source.

## Scoped simplification result

- **Accept locally:** merge the two startup declarations into the bounded,
  discriminating survivor; remove the duplicate protocol command and portal
  command routed examples while retaining direct Plug plus otherwise-valid real
  protocol/portal create requests; remove the two repeated C5 Go processes while
  retaining package/name/event gates over the ordinary stream; replace the four
  inline parsers with the shared strict helper.
- **Accept locally:** the corrected Control fixture-assertion reduction. The
  dedicated table restores all removed public tag/tuple/detail contracts
  without production or manifest changes, the wrong-tag mutant is killed, and
  the fresh full Control suite is green.
- No further abstraction or refactor is justified in this slice.

**Local verdict:** PASS with no remaining finding for the reviewed ablation
source/evidence slice. All local checks named by this re-review are complete.
This is not native credentialed, Goal, merge, CI-matrix, broad-check, or
repository-wide acceptance.
