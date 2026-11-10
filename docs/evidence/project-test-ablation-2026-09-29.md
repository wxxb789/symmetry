# Project test ablation

Date: 2026-09-29. Status: **local ablation complete; independent review passed**.

Final executable results and source hashes are recorded in
[the final local verification receipt](test-ablation-final-gates-2026-09-29.md).
[Independent re-review](test-ablation-final-review-2026-09-29.md) has no remaining
finding. Unavailable native/browser environments remain explicit below; their
checks were retained, not silently removed or reported green.

## Authorized outcome

After the crash-recovery checklist, the user requested ablation across all
project tests, including unit, harness, smoke, validation and verification
harnesses, removal of proven redundancy, and refactoring opportunities revealed
by those experiments. This explicitly expands the next phase beyond Goal 0006b;
it does not change that Goal's authority or acceptance conditions.

The deliverable is an exhaustive test/entrypoint inventory, a disposition and
experiment ledger for each redundancy candidate, verified removals, and a
separate list of grounded refactoring opportunities. Similar names, successful
runs and line-count reductions alone do not prove redundancy.

## Baseline and preservation

- Branch: `codex/goal-0006b-crash-safe-process-containment`.
- HEAD: `d14debbf6ae3ded09e5013ef90952d347c01474d`; extensive existing dirty work
  remains uncommitted and must not be reverted or absorbed into an unauthorized
  commit. No push is authorized.
- Initial Git-visible inventory, excluding session artifacts `.omo/` and
  `.plans/`: 907 files, including 242 test files: 173 Go, 57 Elixir, seven browser,
  two contract-script and three Python test files. Test support, fixtures,
  workflow jobs and executable validation/smoke scripts are additional inventory
  subjects, not omitted because their names lack a test suffix.
- Reconciliation added the PowerShell regression suite: **243 runtime test
  files**, plus **290 additional fixture/entrypoint/provenance candidates** in
  [the complete manifest](test-ablation-manifest-2026-09-29.md). Compile-time
  TypeScript validation is listed separately; support and historical evidence
  are not counted as executable tests or automatic deletion candidates.
- Recovered Go baseline and its revision limits are in
  [the resumed gate report](goal-0006b-resumed-go-gates-2026-09-29.md).
  Linux ordinary and race suites each produced 3,984 test pass events and 95
  explicit skips. Windows and other prior evidence remain separately scoped.
- Preserve journals, physical StopReceipt proof, RunKey/fence boundaries,
  native A/B identity checks, protocol consumers and real-provider acceptance.
  A missing host, database, browser fixture or credential is not redundancy.

## Inventory evidence

- [Go core](test-ablation-go-core-2026-09-29.md): 59 files, 947 top-level tests;
  one actionable duplicate-startup merge and eight distinct-boundary comparisons.
- [Go surfaces](test-ablation-go-surfaces-2026-09-29.md): 114 files, 1,104 top-level
  tests; two platform shims contain support declarations rather than Test functions.
- [Control](test-ablation-control-2026-09-29.md): 57 files, 802 ExUnit declarations;
  two partial-assertion candidates, with direct Plug and routed HTTP boundaries
  explicitly distinguished.
- [Tooling](test-ablation-tooling-2026-09-29.md): 13 files, 76 JS/Python declarations
  plus one PowerShell suite; all 184 contract fixture records and validator,
  browser, CI and Compose entrypoints classified.

The Control inventory's environment probe covered Windows only. Parent probes
found Elixir 1.19.6/OTP 28 and PostgreSQL 17.11 in WSL `symmetry-race`, with existing
dependencies and no server listening on port 5432. A separately owned temporary
cluster can therefore support local experiments; those versions are not the newer
CI matrix. Neither an existing service nor the default database may be altered.

A fresh Docker API probe still failed because the Docker Desktop Linux engine
pipe was absent. Compose restart and live-versus-packaged comparisons retain their
checks; they cannot be declared redundant from unavailable runtime evidence.

## Settled experiment decisions

- [CI peer gates](test-ablation-ci-peer-experiment-2026-09-29.md): the recorded
  complete ordinary suite already ran every required Pi/OpenCode peer parent and
  subtest exactly once. Remove the two later Go invocations, but keep both strict
  gates over the preserved ordinary JSON stream with package identity. Missing,
  duplicate, skipped, malformed and foreign-only evidence remain rejected.
- [Go probe and commit-path boundaries](test-ablation-go-probe-experiment-2026-09-29.md):
  retain the direct bounded-probe test because it alone detects raw output returned
  with a deadline error; retain both commit-path tests because a control-package
  byte-length mutation leaves the protocol-package test green. No tests were
  removed from this experiment.
- [App startup](test-ablation-app-startup-experiment-2026-09-29.md): merge the two
  terminal-registration tests into one survivor with registration order/count,
  a real failed-flush attempt, retained journal, and a bounded context. Ordering,
  duplicate-registration and journal-deletion mutants were detected. Final parent
  package checks passed on the guarded source: Windows 897 pass events/one skip,
  Linux race 1,041 pass events/eight skips, both exit 0.
- [Contract boundaries](test-ablation-contract-experiment-2026-09-29.md): retain
  all direct parser/error tests. The fixture runners missed escaped and nested
  duplicates, malformed-byte classification and a semantic error-code mutation.
  The restored 27-test Node suite and complete contract gate passed.
- [Inline CI parsers](test-ablation-inline-parser-experiment-2026-09-29.md): replace
  four duplicate parsers with the existing strict helper. Controlled Bash and
  PowerShell runs preserved Go-exit precedence and rejected missing, skipped and
  malformed evidence. The real production Windows containment witness passed;
  no credentialed native result is inferred for the other three call sites.
- [Control](test-ablation-control-experiment-2026-09-29.md): the initial partial
  reduction passed 32 focused tests and a parent 799-test full suite, but
  [independent review](test-ablation-final-review-2026-09-29.md) correctly rejected
  its loss of ten exact public error contracts. Those tagged tuples/atoms and
  duplicate-predicate details have been restored in one table test. The wrong-tag
  mutant escaped the prior 12-test subset but failed the corrected 13-test subset.
  The final unmutated complete Control suite passed **800 tests, 0 failures,
  20 skipped**; independent re-review resolved the finding.

## Complete candidate disposition

The inventory reports contain each file's responsibility and each candidate's
specific survivor. These dispositions distinguish source comparisons from
executed experiments; they do not claim every native/browser environment ran.

| Inventory candidates | Disposition and basis |
|---|---|
| Go core C1 | Merge/remove one startup declaration; executed production-overlay counterexamples and final Windows/Linux package checks. |
| Go core C2-C5, C7-C9 | Retain independent retry lifetimes, verdicts, observation outcomes, admission fields, replay identities, Git-byte/path semantics and Prepare/Recover entrypoints. Similar setup is not equivalent detection. |
| Go core C6 | Retain both cleanup matrices: one covers durable Complete replay, the other intermediate A-owned material/effects. |
| Go surfaces C1, C4 | Retain after measured counterexamples: the proposed survivors missed raw-output leakage and an independent validator regression. |
| Go surfaces C2-C3, C5-C8 | Retain adapter, caller/DTO, OS, peer-identity, native-provider and production-replay boundaries. No lower-level fake replaces their contract. |
| Go surfaces C9 | Retain the non-executable sample config. No Go consumer was found, but test ablation did not establish a removable executable responsibility. Historical provenance is also retained. |
| Control C1 | Partial fixture-outcome reduction only; all ten exact public error classifications/details are retained. Wrong-tag counterexample, corrected full suite and independent re-review passed their required checks. |
| Control C2 | Remove repeated command-route parser examples; retain direct Plug coverage and otherwise-valid canonical protocol/portal requests, including escaped member aliases. |
| Tooling C1 | Retain every candidate boundary assertion after five meaningful mutation classes showed non-equivalent fixture coverage. |
| Tooling C2-C3 | Retain browser creation, restart-read and live-runtime boundaries. Docker is unavailable, so no runtime ablation/removal is claimed. |
| Tooling C4-C5 | Remove four parser implementations and two repeated Go processes, not their acceptance assertions or native commands. |

## Refactoring opportunities from the experiments

1. The shared event verifier is a completed refactor: one implementation now owns
   package identity and exact run/pass/skip/failure rules. Keep platform process
   and stdout/stderr handling in the platform-specific callers.
2. Express rejection, public error tags and tuple details separately when
   consolidating fixture coverage. The Control review demonstrated that a green
   boolean manifest is not enough; a validator or cross-language manifest redesign
   is unnecessary for this correction.
3. Codex/Pi TaskResult cloning and control/protocol CommitPath validation may be
   candidates for shared production implementations. Only after that shared
   implementation exists should their similar unit tests be reconsidered; the
   current independent implementations demonstrably need independent checks.
4. Reuse setup without merging distinct lifetimes or effects: claim retry versus
   new-assignment replay, cleanup state versus external material removal, and
   Compose creation versus restart-read remain separate scenarios.

These are bounded findings, not authorization inferred for a new production
refactor. No scheduler, domain authority, provider capability or protocol contract
was expanded.

## Remaining environment and pre-existing quality limits

- Docker engine absence prevented Compose/browser runtime comparisons. Qualified
  systemd A/B, protected native/provider inputs and final remote CI remain absent.
  Their tests/gates remain in place and no pass is claimed.
- The Control full run emitted existing unused-variable/helper and migration
  module-redefinition warnings from unchanged files. Production compilation with
  `--warnings-as-errors` passed; the warnings were neither suppressed nor repaired
  as unrelated cleanup.
- The inventory found ten dispatch-notification cases behind an opt-in not enabled
  by tracked CI, an unregistered PowerShell regression entrypoint, and fixed waits
  in browser tests. These are coverage/reliability findings, not grounds to delete
  the checks or to claim their runtime behavior was validated here.
- No commit or push was made. Goal 0006b remains active; this local ablation work
  does not supply its missing exact-commit/native acceptance evidence.

## Decision rule

Each candidate is retained, merged, removed, or left blocked with a concrete
reason. A removal needs both a known-green relevant baseline and a comparison
showing the surviving checks detect the same meaningful regression at the same
boundary. Use deterministic fault injection or controlled mutations when normal
green execution cannot distinguish coverage. Keep expected-red experimental
runs separate from passing verification. Restore only experiment-owned changes.

Do not replace a native/platform/integration assertion with a weaker unit mock,
remove a failing test to make a gate green, or equate race and ordinary execution.
Unavailable protected environments keep their checks in place. Evidence binds
the actual source snapshots used; later edits need affected verification.

Concurrent workers have disjoint inventory/report ownership. The parent owns
candidate admission, combined changes and final verification. No production
refactor is implied by an insight: report it separately unless it is the minimal
in-scope test/harness consolidation needed for an experimentally justified
removal.
