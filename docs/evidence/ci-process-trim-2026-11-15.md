# CI process trim — 2026-11-15

Status: locally verified workflow implementation; not fully accepted. Hosted
after measurements and execution evidence are unavailable. Local checks bind
the uncommitted workflow files based on the commit below, not that unchanged
baseline commit as if it contained these edits.

## Subject and scope

- Baseline: `5c56c6098583677dabdf92681f7925a8e754b8cd`, merged PR
  [#26](https://github.com/wxxb789/symmetry/pull/26).
- Binding contract: the operator's `ci-process-trim-brief.md`. The session-local
  URI was unavailable; the original artifact was recovered from the prior
  session's `local/ci-process-trim-brief.md` and read in full.
- Allowed changes: `.github/workflows/**` and this evidence record only. No
  source, tests, lockfiles, assertion selections, secrets or remote state change.
- Shape reference: CI-only commit `52fbd1fcb0ca00565d5b5890dc789ce7c4b902fd`
  (`ci: capture witness failure events`), plus the source/verification/limits
  structure in `windows-opencode-ci-gate-2026-09-14.md`. That CI-only increment
  added workflow diagnostics without changing tests; it had no measurement
  harness or goal-contract archive to copy.

## Implementation plan and decisions

1. Keep the existing PR, `main`, tag and manual CI triggers. Add one lightweight
   path-classification job and gate the existing heavy automatic jobs on its
   output, retaining the cheap documentation-contract audit independently.
   GitHub has no job-level `paths-ignore`; using workflow-level filtering
   would omit the whole check suite and its run metadata. Apply the equivalent
   exclusion to individual jobs instead: only `docs/**` and `**/*.md` (including
   root Markdown and `AGENTS.md`) may bypass heavy jobs. Every other path runs
   the full automatic matrix. Non-PR events always run that matrix. The
   `legacy-route-audit` job also runs on documentation-only PRs because its
   explicit contract surfaces include Markdown.
2. Compare the PR's checked-out merge result with its first parent, with both
   parents fetched. Read NUL-delimited paths and disable rename detection so a
   move across the documentation boundary cannot hide a code deletion/addition.
   No file-count/API pagination limit, new action dependency or PR write token.
3. Move native acceptance unchanged into its own manual workflow, with its own
   existing contracts/frontend prerequisite. Retain the protected Environment,
   secrets, source-subject validation, case sets and host qualification. Do not
   add a schedule: `scripts/ci/linux-v2-native-acceptance.sh` explicitly refuses
   any event other than `workflow_dispatch`. Scheduling an always-refused job,
   spoofing `GITHUB_EVENT_NAME`, or changing that source guard would violate this
   increment's scope or acceptance boundary. The brief's dispatch/schedule-only
   allowed trigger set is implemented as dispatch-only, preserving actual policy.
4. Build the production Linux Control release once in the existing `contracts`
   prerequisite, alongside the already-shared frontend assets. Transfer a tar
   archive (including executable permissions) to `control` and `integration`.
   Keep independent Control tests and integration witnesses; do not make
   integration depend on Control test success. Preserve separate Windows,
   Docker-image and environment-gated native builds and all Go build assertions.
5. Audit the four named redundancy candidates and lockfile checks before changing
   either. Delete no job without a same-trigger owner for all its assertions.

## Evidence plan and completion limits

- Run `actionlint` if available. It was absent from PATH; the upstream v1.7.12
  Windows binary is available and has been downloaded outside the repository.
- Run a throwaway behavioral harness against the actual classifier and real Git
  merge fixtures: documentation-only, code/config/lock/script/workflow changes,
  mixed changes, renames/deletions, unusual filenames and large path sets. Check
  full non-PR routing and preservation of original test/witness commands.
- Exercise tar artifact transfer with executable files and symlinks. Local
  archive proof is not hosted Elixir release/browser/integration proof.
- Use GitHub Actions run metadata for the merged predecessor and the eventual
  current PR. Record attempts, executed versus skipped job counts, timestamp
  definitions and run IDs. Do not substitute predicted savings or a local test
  duration for an after run.
- The current branch has no PR. The operator forbids commit and push. Therefore
  this run cannot publish this workflow tree or obtain its hosted after run;
  that acceptance clause remains blocked, not passed. No PR body or goal-file
  archive/index is changed: the contract is external and the allowed diff does
  not include `docs/goals/**`.

## Redundant-job audit

No job is deleted. These owners retain their original PR/main/tag/manual
triggers whenever a non-documentation path changes; `main`, tags and manual CI
are deliberately not documentation-filtered. A job-wide ordinary suite is not
an equivalent owner for opt-in witnesses or a named no-skip JSON gate.

| Retained owner | Assertions and evidence it owns | Why no same-trigger replacement exists |
| --- | --- | --- |
| `opencode-production-unsupported-windows` | `TestOpenCodeLegacyAssignmentFailsBeforeRunningWorkspaceAndProcess` checks both configured 1.x/2.x commands: retained Claim/fence, exactly one durable failed transition, no running transition, workspace, process or event effects, and Windows `harness_capability` failure. The JSON gate requires the named test to pass, not skip. `test-opencode-v2-support-witness.py` rejects auth failure/idle/missing terminal as success, no-op interruption, continuation after the selected terminal, incomplete cleanup/tree-reader barriers, and decodes split/chunk-boundary SSE. | `daemon-windows` includes the Go test but not its named JSON/no-skip proof or the Python classifier suite. `opencode-native-synthetic` owns a different Linux refusal before health HTTP. |
| `daemon-windows` | Full Windows `go test ./...`, production daemon/fake-agent build, opt-in `TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse` plus its named JSON gate (supervisor authority, lost-response refusal, authority-bound replayed stop receipt and authenticated release); opt-in `TestProductionPreAuthorityCrashWitnessMatrix` and `TestProductionPreAuthorityWrongSecretFirstClientRace` (hard-crash boundaries, exact process/Job identity, wrong-secret rejection, legitimate retry/recovery and containment retirement). | Linux cannot replace Windows Job/supervisor/build behavior. No other Windows job enables these production witness flags. |
| `pi-control-e2e-linux` | Exact Pi release archive/executable digests and version; opt-in `TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction` plus named JSON gate; Control compilation with warnings as errors; pidfd process-group cleanup fixture proving matching identity signalling and wrong-start refusal; selected `pi_control_e2e_linux_refusal` ExUnit case and nonempty/zero-failure summary; orphan cleanup and required provenance artifacts. The Control case binds failed Task/Run/Claim/fence and no provider/native effects. | `daemon` does not enable the Pi smoke flag; ordinary `control` tests do not include the literally skipped Pi module. The Windows positives cannot establish Linux missing-owner refusal or pidfd cleanup. |
| `pi-control-e2e-windows` | Numeric loopback/ephemeral PostgreSQL isolation; exact Pi archive digest/version; opt-in provider bridge action plus named JSON gate; warnings-as-errors compilation; five selected real Pi/Control scenarios: repository work/receipts without Goal acceptance, hard-daemon-crash recovery without effect replay, independently validated candidate/achieve, fresh handoff lineage/one new native session, and retained resume across a fresh daemon with fencing. Nonempty/zero-failure ExUnit summary, prerequisite diagnostics, process cleanup markers/identity checks, PostgreSQL shutdown and required provenance artifacts remain. | `daemon-windows` does not enable the Pi action flag; ordinary `control` tests skip these five scenarios. Linux explicitly owns refusal, not these Windows positive scenarios. |

Source anchors: `daemon/internal/app/harness_test.go:790-860`;
`scripts/ci/test-opencode-v2-support-witness.py:31-166`;
`daemon/internal/platform/containment_supervisor_production_windows_test.go:23-116`;
`daemon/e2e/production_pre_authority_crash_witness_test.go:85-274`;
`control/test/symmetry_control/pi_control_e2e_test.exs:38-49,344-346,500-502,698-701,1041-1044,1261-1265,1606-1609`.
The final workflow diff and command-preservation check, not historical line
numbers in the old workflow, determine whether these owners were retained.

## Build and lockfile audit

- Frontend assets were already produced once by `contracts` and distributed as
  `portal-assets`; keep that build and its typecheck/lint/tests.
- The reusable Ubuntu production release was previously compiled independently
  by `control` and `integration`. The planned producer is their existing shared
  `contracts` prerequisite, so neither consumer acquires a dependency on the
  other's test result. The tar contains the release's bundled runtime and Portal
  assets; each consumer still owns its database, migrations and runtime checks.
- Native acceptance remains a separate protected workflow and builds its own
  release on the selected Subject. Container builds remain independent: they
  verify the committed Dockerfiles and production image, not a host release.
- Keep the Linux `CGO_ENABLED=0 go build ./...` assertion, the native Windows
  daemon/fake-agent build and integration's explicitly located fake-agent binary.
  The first is a package compilation check, not a transferable executable;
  Windows is a different target; E2E tests also build their own bound helpers.
- Searches of `.github/workflows/ci.yml` and `scripts/**` using registry/lockfile
  terms and a separate `grep`/`lock`/`registry` pattern found no lockfile registry
  URL grep to replace. Existing `pnpm install --frozen-lockfile` stays. The
  Windows Pi `HKLM`/`HKCU` registry lookup locates PostgreSQL binaries and is not
  a package-registry check. No registry policy or lockfile is changed.

## Hosted timing measurements

Read from `gh api repos/wxxb789/symmetry/actions/runs/<id>` and
`.../jobs?per_page=100`, with the per-attempt run/jobs endpoints used to separate
the predecessor's retry. These reads exited 0. There are fewer than 100 jobs,
so this query has no omitted jobs page. Wall-clock below is `updated_at` minus
`created_at` for a completed run, including queueing, retry gaps and reruns.
Job count means check records, not billed minutes or cumulative executions.

| Subject / event | Run ID | Attempts / conclusion | Job records | Wall-clock |
| --- | --- | --- | --- | --- |
| Merged predecessor PR #26 / `pull_request`, head `d881838b74d820e7ce158948c836321e443f957e` | [43720680428](https://github.com/wxxb789/symmetry/actions/runs/43720680428) | 2 / success | 12: 11 success, 1 skipped | 724 s (`13:45:46Z`–`13:57:50Z`) |
| Merged predecessor commit `5c56c6098583677dabdf92681f7925a8e754b8cd` / `push` to `main` | [43721800117](https://github.com/wxxb789/symmetry/actions/runs/43721800117) | 1 / success | 12: 11 success, 1 skipped | 562 s (`13:58:36Z`–`14:07:58Z`) |
| Current uncommitted workflow tree / PR | Missing: branch has 0 PRs | Not run | Not measured | Not measured |

All timestamps are 2026-11-15 UTC. The predecessor PR's first attempt ended in
failure after 478 s (`13:45:46Z`–`13:53:44Z`: 10 success, 1 failure, 1 skipped;
`daemon-windows` failed). Its successful retry interval was 195 s
(`13:54:35Z`–`13:57:50Z`). The latest jobs endpoint carries earlier successful
jobs forward; only the Windows daemon job has new execution timestamps in that
interval. Do not compare the 195 s retry alone against a full after workflow.
The skipped native job has inconsistent start/end ordering in GitHub metadata;
no per-job duration is derived from it.

No after run exists for this tree, so no measured reduction is claimed. A PR
containing workflow edits necessarily selects all automatic jobs, by design;
it is not a documentation-only sample. Hosted docs-only routing additionally
needs an actual docs-only PR after publication. Both observations are missing;
the local classifier scenarios below do not pretend to be GitHub run metadata.

## Current native dispatch entry point

For this workflow tree, use `Linux native v2 acceptance`
(`linux-native-v2-acceptance.yml`), not manual dispatch of `CI`. A historical
Subject requires an approved branch/tag whose tip matches that Subject and
whose historical workflow supports dispatch. The remote branch/tag listing
contains no ref at frozen commit `8ded77150ec0ae49d52355670fdad78a348bf1d5`;
its dispatchability is not established, before or after this change. Existing
historical receipts and `docs/goals/README.md` describe that old Subject and are
not rewritten by this workflow-only increment. No remote ref was created.

The live Environment read returned required reviewer `wxxb789`,
`prevent_self_review=false`, with allowed branch policies `main` and
`codex/goal-0006b-crash-safe-process-containment`. These settings were read, not
changed or approved. Same-host qualification and credentialed execution remain
environment-limited and are not established by this increment.

## Local verification

| Gate | Actual invocation / scope | Result |
| --- | --- | --- |
| Workflow linter | Upstream `actionlint` 1.7.12 Windows binary, `actionlint -color -shellcheck= .github/workflows/ci.yml .github/workflows/linux-native-v2-acceptance.yml` | Exit 0; 2 workflows; ShellCheck disabled in this invocation |
| Routing, unchanged coverage and artifact carrier | `python .tmp/ci-process-trim-check.py` (throwaway harness, removed after verification) | Exit 0; output below |
| Existing refusal-route audit | Git Bash `--noprofile --norc scripts/audit-no-legacy-routes.sh` | Exit 0; `No U2 legacy action endpoints found in production contract surfaces.` |
| Changed Bash blocks | Git Bash `-n` and installed ShellCheck 0.11.0 `--shell=bash --format=gcc -`, sequentially on classifier, producer and both consumer scripts | Exit 0; 4 syntax checks and 4 ShellCheck checks |
| Documentation-audit negative control | Existing audit script on copied exact targets, then a retired endpoint injected only into the copied protocol Markdown | Probe exit 0; underlying audit exits 0 then 1 as required |
| Merge-gate verdict compatibility | Three local calls to the existing `evaluate_checks` predicate, not a live PR verdict | Exit 0; classification failure rejected with either `allow_no_checks` value, docs-only success/skips accepted |
| Optional ShellCheck integration | `actionlint -color -shellcheck=<installed-shellcheck.exe> .github/workflows/ci.yml .github/workflows/linux-native-v2-acceptance.yml` | Timed out at 120 s; no process exit code reported; not passed |

Final focused harness output:

```text
Coverage preservation: 11 automatic owners, 8 unchanged job bodies, native job and prerequisite preserved; 1 shared automatic release build
Actual classifier / depth-2 Git merge checkouts: 23 scenarios passed
Actual classifier / NUL-framing and large-path transport cases: 5 scenarios passed
Classifier Git failure: exit 1, no bypass output
Official @actions/expressions 0.3.61: 34 routing scenarios, 374 expression evaluations passed
POSIX tar carrier: 2 consumers preserved executable mode 0751, symlink, executable output and Portal bytes
CI process-trim focused behavioral gate: PASS
```

The 23 real Git cases ran the workflow's actual classifier shell block in
fresh depth-2 checkouts of two-parent merges. They covered root/nested Markdown,
non-Markdown docs, goal archive, nested `AGENTS.md`, daemon/Control/frontend/
browser/contract changes, all three lockfiles, scripts/workflow/Compose/package
changes, mixed changes, both directions across the docs/code rename boundary,
code/doc deletions and Markdown rename. Five additional cases exercised the
same Python classifier with substituted NUL-delimited Git output: newline and
non-UTF-8 paths, 5,000 documentation files and code after those files. The
negative Git failure emitted no passing bypass output. The official Actions
expression library evaluated the actual output/job expressions for those 28
PR cases and six push/manual input combinations, across 10 gated job owners.
The final shape has those 10 heavy owners and one unconditional legacy-route
audit; the harness also compares that audit's complete job to the baseline.

The carrier proof ran the actual workflow tar creation/extraction commands in
Ubuntu-24.04 WSL, checking both consumer directories, executable mode `0751`,
symlink identity, executed output and Portal bytes. This proves the tar carrier,
not an actual Elixir release build, GitHub artifact service or hosted browser/
integration execution. `mix` is unavailable on this Windows host; no replacement
containers, full test suite or native credentialed acceptance were run.

Two preliminary combined-harness invocations exited 1 in temporary verification
code, not workflow code: first the expression-library `Dictionary` constructor
needed variadic pairs rather than the array shown in upstream README; then WSL
shell-command transport expanded variables before the inner shell. The final
invocation used the installed constructor signature and raw UTF-8 stdin to
`wsl --exec bash -s`. The workflow files did not change between these invocations.

## Independent review and disposition

The independent read-only review found that gating `legacy-route-audit` would
lose a documentation assertion: the audit explicitly includes README files,
`docs/goal-01-runbook.md` and `docs/protocol-v1.md`. A copied-target fixture ran
the actual audit script: original exit 0; adding the retired endpoint
`POST /api/v1/tasks/example/cancel` only to the copied protocol Markdown made it
exit 1. This is why the cheap audit must remain independent of heavy-job
classification, even for documentation-only PRs. The worker removed its two
added gating lines; final baseline comparison passed, and the independent
reviewer re-read the final block and confirmed this finding closed.

The proposed classifier-failure bypass was not reproduced under the binding
merge gate. Three direct calls to the existing `evaluate_checks` predicate
exited 0 as a verification probe: one failed classifier plus 11 skipped checks
is rejected both with and without `allow_no_checks`; two successful cheap jobs
plus 10 skipped heavy jobs is accepted. `--allow-no-checks` only applies to an
empty collection, not an existing failure. Do not add `always()` to consumers:
that would run them after failed prerequisites and change established failure
semantics. No live PR merge verdict was requested or issued.

Read-only remote checks returned HTTP 404 `Branch not protected` for `main`
and an empty repository-ruleset list. No required check context for the removed
in-CI native job was found in those settings. Remote protection was not edited.
The missing historical dispatch ref is recorded above rather than assuming an
old SHA is directly dispatchable. The inherited single-writer native policy
permits self-review; the recorded Environment policy is not independent approval
and no credentialed run or approval is claimed.
The moved job's inherited comment mentions only the reviewed 0006b branch,
whereas the live branch policy also permits `main`; that narrower comment is
not treated as a verified statement of current remote protection.

Scoped simplification retains one classifier/output and no new cache, wrapper
workflow or generic path-configuration layer. The duplicate production release
build is removed. The small native contracts job copy stays because its artifact
must be produced in the same independent workflow run; sharing across runs would
add state/permissions and weaken exact-Subject locality.

## Clause coverage and remaining limits

- Path routing: local executable proof covers documentation-only, every named
  code/config category, mixed changes, renames/deletions and NUL-framed paths
  without an API file-count cap. The cheap audit remains on all PRs. Hosted
  routing is not yet observed.
- Native isolation: independent dispatch-only workflow, original protected job
  and frontend/contracts prerequisite preserved. Schedule remains unsupported
  by the existing source guard, not silently enabled or simulated.
- Redundancy: 0 jobs deleted; four candidate owners and all opt-in/no-skip
  assertions remain. Their disposition is in this record because no PR exists.
- Release reuse: one automatic Linux production-release build; both archive
  consumers verified locally. Full hosted release startup, migration, browser
  rendering and integration witnesses for this tree remain unrun.
- Lockfiles: existing package-manager verification retained; no matching
  registry-URL grep found in the audited workflow/script scope.
- Measurements: predecessor PR and merged-commit run IDs/times are measured;
  current-tree after run and a hosted docs-only sample are missing. No speedup
  or cost reduction is claimed without them.
- Audit-surface limit: the existing legacy-route script explicitly enumerates
  `ci.yml`, not the newly separate native workflow. The moved native block has
  no retired-route literal; its future route edits would not be covered by that
  script. Expanding the script's target enumeration is outside this increment's
  exact workflow/evidence-only diff boundary; no duplicate regex or source-text
  rewriting workaround was introduced.

The increment is not fully accepted: hosted after measurements and execution
proof are absent. No commit, push, PR creation, history rewrite, remote policy
change or goal-contract archive was performed. The external contract artifact
was not moved; this record is the permitted implementation-stage handoff.
