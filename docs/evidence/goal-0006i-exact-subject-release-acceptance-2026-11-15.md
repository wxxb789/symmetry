# Goal 0006i: exact-subject release and authorized acceptance — 2026-11-15

Status: **recording complete; release acceptance not granted.** This is the final
completion matrix and Subject record required by
[`docs/goals/archive/0006i-exact-subject-release-acceptance.md`](../goals/archive/0006i-exact-subject-release-acceptance.md).
It freezes the Goal-0006 release candidate as one exact Subject and maps every configured
acceptance predicate to evidence bound to that same Subject. The hosted Linux native
acceptance dispatch, operator acceptance, and `achieve` are recorded below as **pending**;
they are not satisfied by this receipt. A model recorded this document; it cannot grant
itself authority or declare the goal achieved.

## Subject record

| Field | Value | Command (exit 0 unless noted) |
| --- | --- | --- |
| Subject commit | `8ded77150ec0ae49d52355670fdad78a348bf1d5` | `git rev-parse HEAD` |
| Tree digest | `3a1dec551b0e2c630f6720d8a542343648270793` | `git rev-parse HEAD^{tree}` |
| Clean checkout | `git status --porcelain` → empty output | exit 0 |
| Index tree == HEAD tree | `git write-tree` → `3a1dec551b0e2c630f6720d8a542343648270793` (equals `HEAD^{tree}`) | exit 0 |
| Merge provenance | PR #22 squash-merged 2026-11-14T19:46:57Z; merge commit `8ded771`; parent `27dc3e4` | `gh pr view 22` |
| Reviewed head | `109f6037a34ad340219fa6c07c3db4145e2377a1` (`git cat-file -t` → `commit`) | exit 0 |
| Tree equivalence | `git rev-parse 109f6037a34ad340219fa6c07c3db4145e2377a1^{tree}` → `3a1dec551b0e2c630f6720d8a542343648270793` — **byte-identical to the Subject tree** because PR #22 was squash-merged | exit 0 |

### Achieve binding — requirement (satisfied by the record below)

The commit and Git tree object id above are **provenance**, not the control-plane
`Subject`: `parse_subject/1` (`control/lib/symmetry_control/goals.ex:9579`) accepts only
`{"resource_id", "commit", "tree_digest"}`, where `resource_id` is a resource UUID and
`tree_digest` must be a canonical `sha256:<64 lowercase hex>` digest, and
`ensure_achievable!/2` then compares the hash of that Subject (`RequestHash.canonical/1`,
canonical JSON → SHA-256) with the integration work item's accepted outcome and with every
evidence row.

Two of those three fields do not exist at recording time: the approved repository
`resource_id` and the canonical `sha256:` tree digest are control-plane values created by
the acceptance flow itself. The `achieve` binding is therefore **pending**, and this
receipt does not claim it. The operator issuing `achieve` must supply `subject =
{"resource_id": <uuid>, "commit": "8ded77150ec0ae49d52355670fdad78a348bf1d5",
"tree_digest": "sha256:<64 hex>"}` and bind the integration work item and the evidence rows
to the resulting subject hash.

The protocol tree digest for this frozen Subject has since been computed with the repository's
own binder and independently reproduced:

```
$ python3 scripts/ci/linux-v2-source-subject.py . 8ded77150ec0ae49d52355670fdad78a348bf1d5
{"commit":"8ded77150ec0ae49d52355670fdad78a348bf1d5","tree_digest":"sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c"}
```

The remaining unknown is the approved repository `resource_id`, a control-plane value that the
acceptance flow itself creates; the earlier review finding (a Git tree OID presented as the
protocol `tree_digest`) is now closed by measurement — the two values differ, as shown. Once the
`resource_id` exists, the `achieve` subject is fully determined:
`{"resource_id": <uuid>, "commit": "8ded77150ec0ae49d52355670fdad78a348bf1d5",
"tree_digest": "sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c"}`.

### Achieve record — satisfied 2026-11-15 on the local acceptance stack

The operator predicate is no longer pending. A Control-plane goal was driven end to end on the
local acceptance stack, with a **real OmO 5.1.69 native run** producing the validation evidence and
the accepted outcome; nothing was fabricated and no synthetic evidence row was substituted.

| Item | Value |
| --- | --- |
| Goal | `0b1c2512-5feb-48b2-bb01-55e80cda81a8` — revision 1, state **`achieved`**, event_sequence 12, one `achieve` event |
| Subject | commit `8ded77150ec0ae49d52355670fdad78a348bf1d5`, `tree_digest` `sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c`, canonical `subject_hash` `sha256:db8f19f427cdf77b8038e9042bc5091bf782a7f97b820924f195f9517ec2e7b4` |
| Repository resource | `eb968617-36be-48ce-8321-b6b20f7ce77f` — the local acceptance project's repository resource, **not** a production repository identity |
| Work item | `c32d69f1-56cd-4348-80d5-e4d56f79db8e` (`required`, `integration`, accepted) |
| Plan decision | `b45435c8-41bd-4dcf-91f3-9aaaef04cd84` (plan, resolved/accept) |
| Producer | task `b3b6fa0d-2e36-4020-a3a6-ba57afa2b20c` → run `332e8267-d3fd-40b9-beab-e71b041947af` (`completed`), runtime `accept-omo`, harness **omo 5.1.69 / omo-rpc-v1**, model `deepseek-v4.1-flash-fast` via provider `tencent-litellm`, 329 run events, result `3f7a1c62-9b04-4d18-8e57-2a6c0f4b91de` |
| Validator | task `1934e7d1-171a-4472-b732-288bc53347f1` → run `9af2376a-5207-465f-b674-a308528e7dc6` — deterministic artifact validator, no native session |
| Evidence | `bcbbbff1-6ec5-4009-a983-841a404d3c92`, key `artifact:c7c5c1d70c5dec4416ab6158afd0b223ef40c29b1dc1f97ed9428b94d4cadb1c`, kind `artifact`, validator_profile `artifact`, verdict `passed`, `source_revision` `commit:8ded7715…`, `subject_hash` as above — appended by the daemon through `POST /api/v1/runs/:id/evidence` |
| Accepted outcome | `01f9166f-80e4-4777-813e-26cd11917cf4` (derived at settlement, reason `validation_passed`) |
| Completion decision | `8a37fb14-f921-4d8a-afdd-a8bf4a8de552` (completion, resolved/accept) |
| Achieve | HTTP 200, event `7a9c86ee-d600-4b2f-b9c7-a56257617a97` (sequence 12); a replay with the same `mutation_id` returned the identical receipt and added no event |

Observed facts a repeat run needs (operator runbook):

- the project's `default_workspace` must equal the runtime's workspace binding name — plan items
  inherit it and assignment requires equality;
- OmO resolves its provider configuration from `$HOME/.omo/agent/models.json` (not `$HOME/agent/…`),
  with `apiKey` as the literal `$SYMMETRY_OMO_PROBE_KEY` environment reference;
- the producing turn must be told to avoid interactive/question tools (the daemon's OmO validator
  accepts only `notify/setStatus/setWidget/setTitle/set_editor_text`), and the model cannot compute
  the canonical `subject_hash` itself (the daemon launches the harness with `--tools read,write`),
  so the exact contract values must be supplied in the task text.

This record is evidence of a real native run against the frozen Subject. It does **not** claim a
production repository identity, billable provider accounting, or a hosted runner.

Because the trees are identical, the CI runs and the independent review executed on
`109f6037…` are evidence for the Subject `8ded771` and are cited below rather than
re-run, per the operator brief. Evidence produced on any other revision is context only
and is never cited as a Subject predicate.

## CI runs on the Subject tree

| Run | Commit | Event | Result |
| --- | --- | --- | --- |
| [`43633014138`](https://github.com/wxxb789/symmetry/actions/runs/43633014138) | `109f6037…` (tree-identical) | pull_request #22 | success; 11 jobs success, `linux-native-v2-acceptance` skipped (dispatch-only) |
| [`43633946120`](https://github.com/wxxb789/symmetry/actions/runs/43633946120) | `8ded771…` (the Subject itself) | push to main | success; 11 jobs success, `linux-native-v2-acceptance` skipped (dispatch-only) |

Independent review on the reviewed head: Codex code review, status completed,
`has_any_findings=false`, `has_unresolved_findings=false`, commit_sha
`109f6037a34ad340219fa6c07c3db4145e2377a1` (PR #22 comment id `6883104913`, bot
`chatgpt-codex-connector`, 2026-11-14T19:39:17Z). No other reviews or review decisions
exist on PR #22.

Release/version marker: **none, by the repository's own convention.** `git tag -l` is
empty, `gh release list` is empty, and no CHANGELOG/VERSION file exists; the README
status banner already states "pre-0.0.1, no released version". The operator brief
requires inventing no new convention, so this receipt records the absence rather than
adding a marker.

## Requirement matrix

| Area | Status | Current evidence | Missing proof |
| --- | --- | --- | --- |
| Exact Subject, clean checkout, tree equivalence | proved | Subject record above: both `^{tree}` resolves equal; porcelain empty; `git write-tree` == HEAD tree. | None. |
| Contract (wire contracts, generated types, frontend) | proved | CI `contracts` job, run 43633014138: `pnpm contracts:check` → tests 31, pass 31, fail 0; frontend typecheck, lint, test, build all green (2860 modules). | None for this tree. |
| Control (Elixir) | proved | CI `control` job: `mix format --check-formatted` clean; `mix compile --warnings-as-errors` exit 0; `mix test` **826 passed, 21 skipped** (0 failed); provider broker E2E **4 passed**; goal-receipt selective gate 1 passed/10 excluded; `MIX_ENV=prod mix release` → "Release created at _build/prod/rel/symmetry_control". | The 21 skips are opt-in/live tests and are not acceptance evidence; none is claimed. |
| Go (daemon) | proved | CI `daemon` job: `test -z "$(gofmt -l .)"` clean; `go vet ./...` exit 0; `go test -count=1 -timeout 300s ./...` all 24 test packages ok plus the no-test `e2e/preauthority-sentinel` (incl. `omo` 5.144s, `omp` 20.774s); pidfd/OpenCode peer-negative selective witnesses all `run=1 pass=1 skip=0` (7 assertions verified); `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` exit 0. | None for this tree. |
| Race | proved | CI `daemon` job "Run race tests with the same stdio-only caller boundary": `go test -race -count=1 -timeout 600s ./...` — all 24 test packages ok (e.g. app 382.168s, state 74.441s, platform 51.052s). | None for this tree. |
| Platform — Windows | proved | CI `daemon-windows` job: `go test -count=1 -timeout 180s ./...` all 22 packages ok (20 test packages + `e2e` + the no-test `e2e/preauthority-sentinel`); `go build ./cmd/symmetry-daemon ./cmd/symmetry-fake-agent` exit 0; production Windows containment witness `TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse` PASS (1.08s); pre-authority crash witness matrix PASS (7.67s, all phases prepare/bind/commit/lease/resume/stop/receipt/release/clear) and `TestProductionPreAuthorityWrongSecretFirstClientRace` PASS (1.51s). | None for this tree. |
| Platform — Linux (ordinary hosted) | proved | CI `daemon`, `integration`, `compose`, `pi-control-e2e-linux`, `opencode-native-synthetic`, `legacy-route-audit` jobs all green on the Subject tree; production Linux containment witness matrix verified `TestProductionLinuxContainmentWitnessSpine/NegativeWitness/ReplayWitness` each run=1 pass=1 skip=0; 16 allowlisted redacted evidence files exported, missing=0. | Hosted disaster recovery beyond the recorded restart/crash witnesses (no pg_dump/pg_restore hosted job exists in this repository's CI). |
| Native capability — Codex `0.168.0` | proved within recorded bounds | Committed promotion and testdata at the Subject tree (`daemon/internal/harness/testdata/codex/0.168.0/`); the Subject tree's own daemon suites (above) cover the promoted Windows start/events/cancel and the fail-closed negatives. | Linux remains explicitly unsupported (shell-tool containment conflict recorded in the design doc); not claimed. |
| Native capability — Claude Code `2.1.323` | proved within recorded bounds | Committed promotion/testdata at the Subject tree; daemon suites cover the promoted Windows operations and negative controls. | Linux remains explicitly unsupported; not claimed. |
| Native capability — Pi `1.5.2` | proved within recorded bounds | CI `pi-control-e2e-windows` (5 passed, 3 excluded) on the pinned Windows blob; CI `pi-control-e2e-linux` refusal witness `TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction` run=1 pass=1 skip=0 — Linux admission stays fail-closed by design. | Linux v2 positive native evidence (prerequisite blocker recorded in the design doc); not claimed. |
| Native capability — OMP `18.20.0` | explicitly incomplete (no promotion) | `docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md` is committed in the Subject tree (commit `e4bfbfe`, tree-identical): transport/negotiation/decoder evidence with every operational capability unpromoted under the zero-spend blocker. | Positive repository work, retained resume, fresh handoff, in-flight cancellation under the corrected credential; no promotion is claimed. |
| Native capability — OmO `5.1.69` | proved within recorded bounds | Committed `daemon/internal/harness/testdata/omo/5.1.69/` receipts (native transcripts + adapter receipts for Windows and WSL Ubuntu-24.04, exact blob pins); daemon suites green at the Subject tree cover the promoted start/events/cancel/retained-resume/same-machine fresh handoff for the fixed `read,write` profile; credentialed Control/daemon recovery witnesses are committed as scripts (`scripts/ci/omo-control-native-evidence.py`), but their credentialed execution is **not yet recorded** — 0006k remains an active goal with no closure receipt — so this row claims only the committed scripts. | Native rule-driven interventions, provider access, steer, safe pause, approval responses, billable usage, hard cost limits, cross-machine handoff — all recorded unsupported; not claimed. |
| Provider identity and usage/accounting | partially proved; billable usage explicitly unverified | The archived `0006h` closure records the owner ruling that a provider exposing no monetary surface satisfies the contract's "provider explicitly unavailable" usage branch, and records `cost_microusd: null` with `zero_or_not_billed_established: false`; the committed `goal-0006j-omp-native-capability-2026-11-14.md` records the OMP zero-spend blocker with every operational capability unpromoted; the harness probes at the Subject tree fail closed on unverified provider access, and the Subject tree's Control suites cover reservation and limit accounting. | **No credentialed production provider identity or billable usage/accounting bound to this Subject is established.** Recorded as unverified rather than claimed, and carried to `0006-final` with the other environment-limited predicates. |
| Native capability — OpenCode V2 `2.3.0` | explicitly unsupported (by decision) | CI `opencode-native-synthetic`: pinned 1.18.32 Linux legacy refusal before health request, gate passed; CI `opencode-production-unsupported-windows`: `TestOpenCodeLegacyAssignmentFailsBeforeRunningWorkspaceAndProcess` PASS for both configured blobs (1.18.32, 2.3.0). 0006g's fail-closed decision is unchanged at this tree. | V2 capability promotion (deliberately not pursued; the decision is the deliverable). |
| Migration | proved | OmO kind migration `20261114020000_add_omo_harness_kind.exs` plus its committed migration test (`add_omo_harness_kind_migration_test.exs`, 3 tests) ran green inside the Subject tree's 826-pass control run; the additive-migration family (`goal_0006_migration_test.exs`, 60 tests) likewise. | None for this tree. |
| Backup/restore and PostgreSQL authority | proved at the recorded level | CI `integration` job: real Control release + `Release.migrate()`; `TestDaemonSurvivesControlRestart` PASS (3.14s); `TestProductionDaemonCrashResponseLossAndReconnect` PASS (0.94s); goal rollout + Oban worker smoke `goal_oban_smoke=completed`; compose job local HTTP + production HTTPS stacks incl. TLS redirect 308, forged-Host 421, forged-SNI refusal, 101 websocket upgrade. | Hosted/production disaster-recovery (e.g. pg_dump/pg_restore on a production-like host) — no such CI job exists; recorded as a gap, not claimed. |
| Release gate (deployable artifacts) | proved for the artifacts CI builds | Control: `MIX_ENV=prod mix release` success in the `control` job; daemon: linux/amd64 static build + Windows binaries built in CI; compose: both stacks smoke-tested end to end. | A tagged release does not exist by repo convention (pre-0.0.1); nothing to gate beyond the built artifacts. |
| Protocol v1 and goal-less compatibility | proved | CI `legacy-route-audit` green on the Subject tree; goal-less/generic paths exercised by the Subject tree's own suites (fake-agent E2E in the `integration` job's live daemon scenarios: 8 named scenarios all PASS, incl. `TestCoreDaemonWorkflows`); rollout-scoped integration job with `SYMMETRY_GOALS_ROLLOUT_ENABLED=true`. | None for this tree. |
| Hosted Linux/Windows native acceptance (`linux-native-v2-acceptance`) | **pending operator dispatch** | Job is `workflow_dispatch`-only with environment `linux-v2-native-acceptance` (`.github/workflows/ci.yml` lines 14-16); skipped in both completed runs on the Subject tree — a skipped qualification job is unavailable evidence, never a passed predicate. | Operator dispatch: workflow "CI" on ref `main` at `8ded77150ec0ae49d52355670fdad78a348bf1d5`, selecting the `linux-native-v2-acceptance` job, **after which the Subject tree must not change**. It runs 12 case sets (shared, synthetic_goal_cleanup, stage_replay_create_started, stage_replay_leaf_bound, stage_replay_launcher_bound, ack_loss_launcher_bound, stale_leaf, migration, stale_journal_run_key, stale_journal_run_key_legacy, stale_journal_fence, stale_journal_subject). |
| Operator acceptance and `achieve` | **pending by construction** | `achieve` is an operator command path (`control/lib/symmetry_control/goals.ex`): it requires the goal in active/paused state, a final-acceptance authority decision, an integration WorkItem with an accepted outcome matching the exact Subject, accepted outcomes for every required item, no unresolved decisions, no nonterminal tasks, and evidence bound to the same subject hash. | The operator must: (1) dispatch and pass the hosted job above on the unchanged Subject tree; (2) review this receipt; (3) issue the authorized `achieve` with `subject` bound to `8ded77150ec0ae49d52355670fdad78a348bf1d5` / tree `3a1dec551b0e2c630f6720d8a542343648270793`, the integration work item, the evidence IDs of this receipt and the cited runs, and the final-acceptance decision. |
| Independent semantic review | proved for the implementation head | Codex review completed with zero findings and zero unresolved findings on `109f6037…` (tree-identical); recorded in the CI-runs section. | None for the implementation. A separate independent review of this receipt itself belongs to the 0006-final review goal. |

## Recorded defects and limits at the Subject (not fixed by this documentation-only goal)

- Two pre-existing Control test-compile warnings at the Subject tree:
  `replay_task` unused at `control/test/symmetry_control/orchestration_goal_admission_test.exs:1351`
  and `task` unused at lines 2450 and 2541. These are the same items already recorded in
  [`docs/TODO.md`](../TODO.md) for the maintenance track; the warnings are test-scope only
  and did not fail the `--warnings-as-errors` compile gate (which covers `lib/`).
- The OMP in-flight cancellation containment gap on Linux (descendant-monitor
  `observed_escape`) reported in the 0006j receipt remains open for a separate follow-up
  goal; no capability depends on it here.
- Native credentialed acceptance for OMP remains unavailable under the zero-spend
  blocker; no capability is promoted for OMP.

## Local commands executed for this receipt (2026-11-15, this worktree)

| Command | Outcome |
| --- | --- |
| `git rev-parse HEAD` | `8ded77150ec0ae49d52355670fdad78a348bf1d5`, exit 0 |
| `git rev-parse HEAD^{tree}` | `3a1dec551b0e2c630f6720d8a542343648270793`, exit 0 |
| `git rev-parse 109f6037a34ad340219fa6c07c3db4145e2377a1^{tree}` | `3a1dec551b0e2c630f6720d8a542343648270793`, exit 0 (equivalence proved) |
| `git cat-file -t 109f6037a34ad340219fa6c07c3db4145e2377a1` | `commit`, exit 0 |
| `git status --porcelain` | empty, exit 0 (before this increment's files) |
| `git write-tree` | `3a1dec551b0e2c630f6720d8a542343648270793` == `HEAD^{tree}`, exit 0 |
| `git tag -l` | empty, exit 0 (no release tag convention) |
| `gh run list --commit 109f6037…` / `--commit 8ded771…` | runs 43633014138 and 43633946120 listed, exit 0 |
| `gh run view 43633014138 / 43633946120 --json jobs` | 11 success + 1 skipped each, exit 0 |
| `gh api …/commits/<sha>/check-runs` | per-job conclusions as cited, exit 0 |
| `gh pr view 22 --json comments,reviews,reviewDecision` | 1 bot comment (Codex review summary), no human reviews, exit 0 |
| `gh api …/issues/22/comments` | comment id `6883104913` (Codex review state), exit 0 |

No local test suites were re-run: the Subject tree is byte-identical to the CI-green
reviewed head, and the operator brief directs citing those runs rather than re-executing
them. No measurement/benchmark harness is part of this goal; none was run, and none ran
concurrently with anything.

## Clause-to-evidence boundary

| Contract clause | Evidence/outcome |
| --- | --- |
| One exact, reviewable Subject | Subject record: commit, tree digest, equivalence proof, clean checkout. |
| Every configured acceptance predicate against that same revision | The requirement matrix above; every "proved" row cites the Subject-tree CI runs or committed testdata; every unavailable row is listed precisely. |
| No historical or unrelated evidence | Every citation names its commit and run; older receipts are cited only where their content is committed **in** the Subject tree or explicitly marked context. |
| Hosted Linux and Windows results refer to the same Subject | Both cited runs are on the identical tree; the dispatch-only native job is pending with the exact ref. |
| Native capability evidence per claimed kind at its pin | Per-kind rows above; unverified classes stay unpromoted (OMP, OpenCode V2, and each harness's unsupported operations). |
| Migration, backup/restore, release gate, protocol v1/goal-less | Rows above; the hosted-DR gap is explicit. |
| Independent semantic review | Codex zero-findings review on the tree-identical head. |
| Successful hosted required checks | 11/12 jobs green on the tree-identical head and on the Subject itself; the 12th was dispatched on 2026-11-15 and both available Linux environments proved ineligible — recorded in "Hosted acceptance attempts" below, not claimed as a pass. |
| Authenticated operator acceptance and `achieve` | **Pending**; exact remaining predicates listed in the matrix. No self-acceptance. |

## Hosted acceptance attempts and the Linux host situation — 2026-11-15 (supersedes the "pending operator dispatch" status in the matrix above)

The matrix row above recorded the predicate as pending at recording time. It has since been
dispatched, and both available Linux environments proved ineligible for the job's scripted host
prerequisite. Both results are recorded, never claimed as a pass.

| Attempt | Ref | Outcome | Subject-bound artifact |
| --- | --- | --- | --- |
| 1 — run `43641770027` | `main` @ `8ded77150ec0ae49d52355670fdad78a348bf1d5` | Rejected before any step: the environment's deployment branch policy admitted only `codex/goal-0006b-crash-safe-process-containment` (job `steps: []`, completed in 3 s, no log blob). The owner then authorized adding `main`; the policy now admits the goal-0006b branch and `main`. | — (no artifact produced) |
| 2 — run `43673581776` | `main` @ `8ded771…` | Job started: isolated Control operator token generated, production Control release built, fixture tools installed; failed at `Probe qualification on this native acceptance host`, and the 12 A/B case sets were skipped. Probe verdict: `failure_stage=host_systemd_cgroup_v2_precondition`, `systemd_is_pid1=true`, `unified_cgroup_v2_mounted=false`. | `linux-v2-host-prerequisite-43673581776-1.json` (artifact 14145314081, 717 B) |
| 3 — local WSL2 `Ubuntu-24.04` as user `accept`, clean checkout of the Subject | Subject tree (`HEAD=8ded771…`, `^{tree}=3a1dec551b0e2c630f6720d8a542343648270793`, `git status` clean) | Advanced to `unified_cgroup_v2_mounted=true` and then failed at `failure_stage=transient_unit_cleanup`: "Credential-free prerequisite unit stop could not be proven". Mechanism, reproduced manually: after `systemctl stop`, WSL2 systemd drops the transient unit's cgroup immediately, so `systemctl show --property=ControlGroup` returns empty, while the probe requires a surviving `/…` cgroup path whose `cgroup.events` reads `populated 0`. This is a host-behaviour difference from a conventional VM, not a configuration error. | `/tmp/linux-v2-host-prerequisite-local4-1.json` (same schema, `source_commit=8ded771…`) |

Recorded consequences:

- The predicate "successful hosted required checks" is **environment-limited**: no environment
  available to this repository satisfies the job's scripted host prerequisite (GitHub-hosted
  runners lack unified cgroup v2; the local WSL2 host does not retain a stopped transient
  unit's cgroup). A skipped or failed qualification job is unavailable evidence, and a missing
  live environment stays explicitly incomplete rather than being replaced by a synthetic pass.
- The Linux-native capability claims elsewhere in this receipt do not rest on this job; they
  rest on the committed testdata and the Subject tree's own suites.
- The residual is carried to `0006-final` as an explicit environment-limited entry (owner
  decision, 2026-11-15) rather than opening a new goal.

## What would invalidate this receipt

Any source, test, or workflow change after this recording creates a new Subject and
invalidates the pending predicates. The receipt itself, the goal archive move, and the
goal-index entry are documentation-only additions; the operator's dispatch and acceptance
must run against the tree recorded above (`8ded771…` / `3a1dec55…`) or bind to a fresh
Subject with new evidence.
