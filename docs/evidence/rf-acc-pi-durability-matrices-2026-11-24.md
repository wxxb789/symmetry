# rf-acc-pi-durability-matrices — evidence record (2026-11-24, implement stage)

Contract: `goal-rf-acc-pi-durability-matrices.md` (GDD state root, external to the repository).
Plan: `docs/plans/2026-11-24-rf-acc-pi-durability-matrices-plan.md`. Base revision:
`477e4c62cbd14a671262da727954aa8e67fd0c40` (#277, the rf-acc-native-burndown merge). Branch:
`refactor/rf-acc-pi-durability-matrices`.

Runner: GitHub-hosted `ubuntu-latest`, `workflow`-dispatched throwaway copy of the real job on
`scratch/rf-acc-pi-durability-matrices` (push trigger; no `environment`, `secrets.*` or `vars.*`;
the contract's dummy credentials; the scratch-only `gh` shim ratified in rf-acc-native-hosted-run,
which answers only `gh auth login` and never reaches the PR branch). Every scratch run stops before
a real model turn: the pi turn legs are the credential-gated rows the owner dispatches after merge.

Placeholders: `<A_HOME>`/`<B_HOME>` = the fixture A/B user homes, `<RUNNER_TEMP>` = the hosted
runner's temp directory, `<STATE_DIR>` = the fixture-provided Linux v2 StateDir.

## 1. Subject bytes (final tree, sha256)

| File | sha256 |
| --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `143d78ea6b29e1cf8b76a16f0929634c11c932e396ab64120125dd36bf22961e` |
| `scripts/ci/linux-v2-native-tools.sh` | `185f4ff085f3a3a47e4f46d4727ded89fd78b14c0ba6c9ea822e413c42b5f1d2` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `18001a824062f94e470fae3655b00cbb85dc866048148a57ce8afb437f8d06fa` |
| `scripts/ci/write-linux-v2-owner-request.py` | `47cb012ba516dcf1f85fdd65b709d620ddbf77b7a4b398c4dfcbfeda831c03a9` |
| `scripts/ci/register-linux-v2-claim-machine.py` | `6fd8be109e4c4382186cef17f2c7dd9eb67a89c4890cd6cba6f4aa565d4372b7` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `a21a22d97f09d4afec2fee7cc6b035684c78479ecb3f37dc33d5822e82794d25` |
| `scripts/ci/test-linux-v2-evidence.py` | `afb9b7c2a4660645e2d1f6c24082d1c9c4d17f69641189f7007115e3c63bdcb9` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `bddf80d702e7ef033104b338bb2d18c01b0fed981ff10acbda256837ff4a2f8e` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `5b3bf2f3db0ba6352f1dca79a2bc845a20e88f56748b0636ac3657537cfd6d62` |
| `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` | `25d5ee972acf2e11994751526ed3dab8308d3488fe380bc29221bd35b04f96f5` |
| this record | no self-referential hash |

Finalization binding (2026-11-25): the ten values above are also the finalization sha256 equality
table — the goal worktree's source files are byte-identical to the scratch head `f2682b71` of run
`45489336388` (the last scratch run). The only source delta between the run-9/10 heads and the final
bytes is the §2.17 parked-gate hunk (`0003f62d..ab48f317` touches only
`TestProductionLinuxV2UnresolvedJournalBlocksFreshRun`); `aa58f43b..0003f62d` changed only the
scratch workflow's case-set list.

The five rows above the unchanged ones are the admission campaign: the diff against the round-3
record's head `f1f80fb6` is exactly `scripts/ci/linux-v2-native-acceptance.sh` (+51),
`scripts/ci/provision-linux-v2-control-claims.exs` (+231),
`scripts/ci/register-linux-v2-claim-machine.py` (+32),
`scripts/ci/write-linux-v2-owner-request.py` (+29) and
`daemon/e2e/production_linux_v2_native_witness_test.go` (20 changed lines: §2.15's admission
assertion plus §2.17's parked-gate fix).

Scratch-only files, deleted with the branch: `.github/workflows/scratch-pi-durability-matrices.yml`,
`scripts/ci/scratch-gh-shim` (branch `scratch/rf-acc-pi-durability-matrices` deleted a third time
after the finalization run 13; `git ls-remote origin scratch/rf-acc-pi-durability-matrices` prints
nothing — §3). The frozen OpenCode bytes
(`daemon/internal/harness/opencode/**`,
`daemon/internal/harness/testdata/opencode/**`, `.github/workflows/ci.yml`, `docs/design/harness.md`,
`docs/goals/README.md`, `docs/goals/0010-refactor-program.md`) are at base bytes, and the acceptance
script's frozen line regions are byte-identical to the base (verified by extracting those regions
from `9d113eb6`'s copy and matching them verbatim in the final file; §6).

## 2. The changes

1. `scripts/ci/linux-v2-native-acceptance.sh`: `config()` gained a `provider_access` keyword
   argument (default `kind == "pi"`, so every existing call line is unchanged); a new template
   `pi-durability.json` (kind `pi`, profile `pi-durability`, `provider_access: false`, bound to the
   new non-provider-bound durability resource). The stage-replay-hold, ack-loss, negative-root and
   `synthetic-goal-cleanup.json` generators and the `daemon_service_template` default now use that
   template; the unprovisioned bundle and the `opencode-health.json`/`opencode-wsl2.json` writes are
   untouched. The unprovisioned request now names its own case
   (`stage_replay_unprovisioned_refusal`) so its OpenCode harness/profile comes from the table
   instead of from the retargeted generic `stage_replay` entry. The staged pi executable is checked
   against the pinned `pi_executable_sha`. `export_native_evidence` passes the pi native arguments
   for the twelve retargeted sets. In `shared`, every pi leg now runs above the frozen
   `opencode_native_health` invocation, whose bytes and position relative to the OpenCode
   peer-negative leg are unchanged.
2. `scripts/ci/linux-v2-native-tools.sh`: `pi_archive_sha`, `pi_executable_sha` and a new
   `pi_version_line` are shell-visible; the pinned hashes and fetch rules are unchanged.
3. `scripts/ci/provision-linux-v2-control-claims.exs`: each `@cases` entry names its repository role
   (`:pi_provider`, `:pi_durability`, `:opencode`); the fixture creates the non-provider-bound
   `Linux v2 Pi durability witness repository`; the schedule assertion is now
   `provider_access? == (repository_role == :pi_provider)`; the retargeted entries run under the
   `pi-durability` profile with pi instructions; the unprovisioned and codex/claude entries are
   unchanged in profile/harness. `stage_replay_unprovisioned_refusal` keeps its own run case name so
   its request carries the generic `stage_replay` case with an OpenCode harness.
4. `scripts/ci/register-linux-v2-claim-machine.py`: a sixth generic runtime
   `owner-witness-control-pi-durability-generic` (`agent_profile` `pi-durability`, no
   `provider_access` capability) bound to the durability resource.
5. `scripts/ci/write-linux-v2-owner-request.py`: `CASES["stage_replay"] = ("pi", "pi-durability",
   None)`; provider access is required and validated only for the three pi provider cases, so
   durability requests require empty `work.required_capabilities` and no `claim.provider_access`.
6. `daemon/e2e/**`: the eight retargeted runners, the stage/ack/negative mappers, the
   `linuxV2AssertStaleRequestVariant` harness argument, the unresolved-journal kind checks and the
   helper-exit live-B command/executable check are pi; the helper-exit wrapper and the released-B
   observation are part of the same retarget. A shared helper binds `native_binary`,
   `native_binary_sha256` and `native_version_line` into every provider-free pi durability witness
   (the daemon's recovery path publishes no digest), and the exporter checks them against the
   CI-side pin.
7. `scripts/ci/export-linux-v2-redacted-evidence.py`: `--native-kind pi` is accepted for the twelve
   retargeted sets; each set's pi witness files are resolved from `MANUAL_CASE_SET_FILES` and must
   bind `harness == "pi"`, the staged binary path, the pinned sha and the `1.5.2` version line plus
   `subject.tree_digest`; `synthetic-goal-cleanup.json` is checked by its
   `fixture.runtime_kind == "pi"` vehicle field because that test never launches the harness. The
   manifest carries the same `native_binary` block as the containment kinds, without
   `manifest["containment"]`.
8. `scripts/ci/test-linux-v2-evidence.py`: pi fixture plus four cases (pinned-binary happy path,
   binding/digest/tree-digest refusals, non-retargeted case-set refusal, cleanup vehicle binding).
9. `scripts/ci/linux-v2-native-acceptance.sh` (found by round-3 run `45460144698`, plan §3.7): the
   fixed-B account's provisioned HOME moves inside the isolated owner witness root
   (`B_HOME="$OWNER_TEST_ROOT/fixed-b-home"`, `:468`), because the App refuses any passwd HOME
   outside `/opt/symmetry-owner-test` (`linux_owner_witness_linux.go:2902-2905`). `machine_run_as`
   derives `HOME` per account (`:955-961`). The fixed-B XDG cache, `.pi/agent`, the Pi session
   directory and the A `--x` ACLs all resolve through `$B_HOME`, so one assignment moves them.
10. `daemon/e2e/production_linux_v2_owner_cli_test.go:53,275-282` + the fixture's failure context
    (`:1684-1689`): the witness command's stderr goes to a private 0600 `owner-witness-stderr.log`
    beside the witness documents instead of `/dev/null`, and the fixture prints a bounded tail when
    a leg fails. This is the instrument that named both refusals above.
11. `daemon/e2e/production_linux_v2_owner_cli_test.go:3813`: the migration denial's wait message no
    longer says "OpenCode native_release boundary" for a case that is now pi (`wait for exact
    native_release boundary`), matching the neighbouring harness-neutral waits. Message only.

### 2b. Admission campaign (the conductor's ruling on implement `DECISION_NEEDED` F1, 2026-11-25)

12. `scripts/ci/provision-linux-v2-control-claims.exs`: `@cases` splits into `legacy_cases`
    (goal-less `launch_one_shot_work_item!`) and `goal_cases` (`repository_role == :pi_durability`);
    `prepare` publishes only the goal cases' repository/profile/description specs, and `schedule`
    provisions a real Control Goal per retargeted case: `goal_create` (`symmetry.goal_create.v1`;
    `symmetry.acceptance.v1` with the operator predicate; `publication_allowed: false`;
    `operator_required_for_completion: true`) → `request_decision`/`resolve_decision`/`accept_plan`
    → `activate` → `admit_task` with `purpose: "implement"` and the case's profile. Control
    composes the `symmetry.admission.v1` document itself (`subject` = the fixture Subject,
    `provider_scope` null because `purpose=implement` allows no provider action,
    `admission.ex:1304-1307`), so no fixture value is trusted as an admission field. Goal commands
    persist Oban wakeup jobs, so the fixture starts Oban with `queues: false` and no plugins.
13. `scripts/ci/register-linux-v2-claim-machine.py`: the sixth runtime
    `owner-witness-control-pi-durability-generic` is native-shaped (`harness_kind` `pi`, `1.5.2`, an
    adapter block with start/events/cancel), because Goal assignment admits only native-harness
    runtimes carrying adapter operations (`assignment.ex:332,:519-530`); the registration
    preflight's message no longer counts "four".
14. `scripts/ci/write-linux-v2-owner-request.py`: for `pi-durability` the Work input must parse as
    an admission — `schema_version == "symmetry.admission.v1"`, `subject` exactly the requested
    Subject (`resource_id`/`commit`/`tree_digest`), `provider_scope` present and null — and the
    legacy repository comparison moved to the non-durability branch. The writer refuses a
    legacy-shaped ClaimResponse before any witness starts.
15. `daemon/e2e/production_linux_v2_native_witness_test.go:1565-1573`: the old assertion that every
    claim's Work input is a real non-Goal Control task is inverted for pi durability cases — they
    must carry the Control admission with a valid `subject` equal to `request.Subject` and a null
    `provider_scope`; every other case keeps the non-Goal assertion and the no-provider-access
    assertions are untouched.
16. `scripts/ci/linux-v2-native-acceptance.sh`: the bundle-based witness go tests now run under
    `systemd-run --property=UMask=0077` for the `stage-replay` and `negative` modes, matching the
    installed daemon units; without it the clone seed's A-owned refs are B-accessible and the App's
    own revalidation refuses them (named by run 8). The failure context gained the bounded stall
    diagnostics that named that wall: `print_witness_stderr_tail` truncates to 400 bytes and
    redacts instead of dropping long lines, plus the RunJournal progress projection, the stage
    markers, the A-owned Git metadata modes and the owner-service journal.
17. `daemon/e2e/production_linux_v2_native_witness_test.go:437-451,524-528`: the unresolved-journal
    guard no longer requires the **parked gate** to be live after the setup's owner SIGKILL. The
    gate's `wait_for_release` polls the owner pidfd and exits with `ESRCH` as soon as the owner dies
    (`daemon/native/linux-gate/c-gate/gate.c:731-757`), and the un-released helper aborts it on
    owner loss (`daemon/internal/platform/helper_v2_recovery.go:223-225`), so the old expectation
    was unsatisfiable on any host; the gate's exact identity stays bound by the marker/RunJournal
    comparison at `:411-429`, and only the anchor/helper liveness remains required (both survive by
    design). Found by run 9, fixed here and re-run as run 11 (all four stale-journal sets) beside
    run 10's sibling batch (§3).

## 3. Scratch runs

Baseline for comparison: the burndown's literal all-14 run `45431551609` (head `7072891f`, pre-goal
bytes, OpenCode-vehicle sets) — every durability set stopped at the fixture's frozen OpenCode
version gate (`owner witness config native harness version differs from the tested release`), and
`shared` also stopped in its owner-leg scratch group. That is the "pre-retarget behaviour" the
mutant rows below revisit.

| # | Run | Case sets | Head | Outcome |
| --- | --- | --- | --- | --- |
| 1 | `45455364615` | 6 class sets | `8c2f1efc` | all six exit 1. Every witness leg stops at the barrier timeout with the App's refusal still invisible (stderr went to `/dev/null`); `shared` additionally stops in its owner-leg scratch group; `synthetic_goal_cleanup` in the cleanup witness path. |
| 2 | `45458275273` | `stage_replay_create_started` | `35fc35d4` | exit 1, and the new private stderr log names the refusal: `fixed-B native HOME is outside the isolated owner witness root`. |
| 3 | `45460144698` | `stage_replay_create_started` | `3478e780` | exit 1. The HOME refusal is gone (the fixture now stages `fixed-b-home/.pi/agent/*`, and the App's read-only pi preflight passes for every pi config); the next refusal is `reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` (plan §3.9). Control-side retarget visible in the same log: the work item insert carries `agent_profile "pi-durability"`, `workspace "native"`, the new durability `repository_resource_id` and the pi instruction. |
| 4 | `45461719002` | all twelve retargeted sets | `baed845c` | every set exit 1; rows in §4. Ten sets publish a witness document and a manifest carrying the pinned pi `native_binary` block; `synthetic_goal_cleanup` stops at its pre-existing release refusal and `shared` in its owner-leg scratch group, so their exports fail closed. |
| 5 | `45464759248` | `migration`, `stage_replay_create_started`, `stale_leaf` (three mutants, head `7a1da42f` = run 4's bytes with the three reversions) | `7a1da42f` | all three exit 1 with the mutant-specific refusals: `migration` → `per-harness daemon profile is absent` (the reverted OpenCode-vehicle config has no `pi-durability` profile for the Control run's profile), `stage_replay_create_started` → `production_linux_v2_native_witness_test.go:178: … runtime.harness_kind = "pi", want "opencode"`, `stale_leaf` → `per-harness daemon profile is absent` (the reverted Control case names `opencode-native` for a pi config). All three export nothing (`pi durability witness is unavailable: <file>`). |
| 6 | `45465722190` | all twelve retargeted sets (replay on the restored final bytes, head `f1f80fb6`; the worktree's ten changed files are byte-identical to the goal tree) | `f1f80fb6` | reproduces run 4 row for row: twelve exit-1 sets, 20 clone-wall lines, 10 witness rows, 10 manifests, the same two export refusals. The only visible difference is the migration wait's corrected harness-neutral label. After this run the scratch branch was deleted (`git push origin --delete`) and `git ls-remote origin scratch/rf-acc-pi-durability-matrices` printed nothing; the stage ended `DECISION_NEEDED` on the clone wall and the conductor ruled the admission shape (§2b), after which the branch was recreated with the admission campaign's rows 7–9. |
| 7 | `45476046261` | `stage_replay_create_started`, `migration`, `stale_journal_run_key` (admission fixture) | `73400558` | all three exit 1, each at a ~31 s barrier wait (`wait for exact durable stage create_started`), with no named cause: the failing line exceeded the old context filter's 400-byte bound and was dropped, and the surviving short stderr lines are the fixture's config-only preflight noise (`open root-controlled fixture file`, the App's deliberately detail-free `unix.Open` failure, `linux_owner_witness_linux.go:1199`). The admission path itself is visible in the Control log for every set: `goal_revisions` ("Isolated Linux v2 owner-witness admission", objective = the case's pi instruction), `goal_events` (`goal_created`), and the work item insert keeps `agent_profile "pi-durability"` with the durability `repository_resource_id`. No `clone intent does not match` line remains. |
| 8 | `45479093531` | same three sets (run 7's bytes + the stall diagnostics) | `5bb2e546` | all three exit 1, and the new truncate-instead-of-drop tail names the wall: `build explicit App-owned Linux v2 witness invocation: revalidate Linux owner before native invocation: verify A-owned Git metadata tree /opt/symmetry-owner-test/production/source/.git: Git metadata entry …/.git/logs/refs/heads/symmetry is accessible to B: effective permissions for UID 52001 GID 52001 are 05, require 0 and forbid 07`. The RunJournal projection shows `has_clone_intent` and `has_seed_receipt` set, so the clone reserve admits the goal-backed claim and the seed runs; the A-owned refs it creates inherit the fixture's `umask 022`, which the same binary's revalidation refuses. |
| 9 | `45481221551` | same three sets (run 8's bytes + `UMask=0077` for the bundle witnesses) | `aa58f43b` | all three exit 1; zero `clone intent does not match` and zero Git-metadata lines — the wall is cleared and each set now reaches its own boundary, stopping on the later checks of §4: `stage_replay_create_started` reaches the hold and its journal boundary validates, then fails the recovery exit contract; `migration` never reaches its marker (the App's own release step aborts it, stderr in §4); `stale_journal_run_key` fails a pre-fresh-request precondition (the pinned native process is already exited). |
| 10 | `45484692731` | `stage_replay_leaf_bound`, `stage_replay_launcher_bound`, `ack_loss_launcher_bound`, `stale_leaf`, `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | `0003f62d` | all seven exit 1, and every row confirms a predicted class: the three `unresolved`-expected sets stop on the recovery exit contract (`:178` twice, the ack runner's `:217` once, all `unresolved production recovery exit = <nil>, want deliberate nonzero process exit`); `stale_leaf` reaches its boundary — a **durable stop receipt on the pi vehicle with dummy credentials** (the failed turn stops the containment, the marker and `linuxV2ValidateStageJournalBoundary` both pass) — and its recovery leg then refuses with `stage replay unresolved outcome lacks retained journal/owner uncertainty` and writes no response (`:630`); the three variants stop at the recovery refusal of §4c (`:269`). |
| 11 | `45485437534` | the four stale-journal sets (run 9's bytes + §2.17's parked-gate fix) | `ab48f317` | all four exit 1, and the fix is visible: no leg stops at the old liveness message any more. The new first failing checks are `stale_journal_run_key` → `:532` `pin exact live A anchor/helper PIDFD before fresh request: stage owner does not have exact A credentials`, and the three `TestProductionLinuxV2StaleOwnerRequestDenials` variants → `:269` `read stage-replay recovery response: decode redacted production owner-witness response`. Every set's App stderr is the same refusal — `recover existing owner witness RunJournal without launch` — over a journal that still shows `has_clone_intent`/`has_seed_receipt`/`recovery_required` (§4c). |
| 12 | `45488371274` | `stale_journal_run_key` (F3 mutant: a mismatched marker gate identity injected before the guard) | `6763a2df` | exit 1 at 0.00 s with the guard refusing — `production_linux_v2_native_witness_test.go:533: setup RunJournal does not bind the exact live anchor/helper/gate identities`; run 11's unmutated bytes got past the same guard to the pidfd pin (§5 row 6). No manifest. |
| 13 | `45489336388` | all twelve retargeted sets, literal, on the final goal-tree bytes | `f2682b71` | twelve exit-1 sets, every row reproducing runs 9/10/11 (§4e); ten manifests carrying the pinned pi `native_binary` block (`sha256:6a2ac1c5…`, `1.5.2`, subject commit `f2682b71`; artifact `linux-v2-native-evidence-45489336388-1`), the same two export refusals (`shared`, `synthetic_goal_cleanup`), and witness documents with `harness: "pi"` / `installed_owner_verified: true`. Job 16m42s. |

Every scratch run settled before its stage closed (no run pending). The scratch branch was deleted
after run 13 (`git push origin --delete scratch/rf-acc-pi-durability-matrices`) and
`git ls-remote origin scratch/rf-acc-pi-durability-matrices` prints nothing; earlier
deletions/recreations are recorded in rows 6 and 11. The last run (13) is the literal all-12 run on
the final bytes, bound by the §1 equality table.

## 4. Per-set rows (final all-12 run)

Binding run: `45461719002` (all twelve retargeted sets, discovery order), then the identical
all-12 replay after the mutant probes. Retarget census: the Elixir `@cases` table has 26 source
entries — 48 cases after the two comprehensions expand (14 stage-replay holds, 10 ack-loss stages) —
of which 39 are `pi-durability`/`:pi`/`:pi_durability` (17 source entries), 3 stay the unchanged
pi-provider cases (`pi-native`/`:pi`/`:pi_provider`) and 6 are exempt by the contract
(`opencode_native_health`, `stage_replay_unprovisioned_refusal`, `codex_containment`,
`claude_containment`, `stage_replay_native_release_codex`, `stage_replay_native_release_claude`).
Every witness leg stops at the pre-existing clone wall of plan §3.9, so no row below reaches its
contract-expected pi boundary. The pi plumbing around the legs is green: each set's read-only pi
preflight accepts all three pi configs (the two OpenCode configs still hit the frozen version gate),
the witness documents carry `harness: "pi"` with `native_binary`,
`native_binary_sha256` = `sha256:6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`,
the pinned `pi_executable_sha` from `linux-v2-native-tools.sh:19-21`, and `native_version_line: "1.5.2"`,
and ten sets published a manifest carrying exactly that block
(artifact `linux-v2-native-evidence-45461719002-1`; `archive_sha256` `sha256:811c6f2c…` and
`kind: "pi"`, `status: "failed"`). The two sets with a different pre-existing stop publish no
manifest and their export fails closed (`pi durability witness is unavailable: <file>`).

| Set | Exit | First failing check |
| --- | --- | --- |
| `stage_replay_create_started` | 1 | clone wall; e2e `production_linux_v2_native_witness_test.go:178` `create_started` wait times out (30.9 s); manifest published |
| `stage_replay_leaf_bound` | 1 | clone wall; `:178` `leaf_bound` wait; manifest published |
| `stage_replay_launcher_bound` | 1 | clone wall; `:178` `launcher_bound` wait; manifest published |
| `ack_loss_launcher_bound` | 1 | clone wall; `:217` ack `launcher_bound` boundary wait; manifest published |
| `migration` | 1 | clone wall; denial wait (`:616`; that run's message said "OpenCode", made harness-neutral on the final bytes); manifest published |
| `stale_journal_run_key` | 1 | clone wall; `:534` unresolved-journal guard wait; manifest published |
| `stale_journal_run_key_legacy` | 1 | clone wall; `:269` stale-owner-request denial; manifest published |
| `stale_journal_fence` | 1 | clone wall; `:269` stale-owner-request denial; manifest published |
| `stale_journal_subject` | 1 | clone wall; `:269` stale-owner-request denial; manifest published |
| `stale_leaf` | 1 | clone wall; `:630` durable stop-receipt wait; manifest published |
| `synthetic_goal_cleanup` | 1 | pre-existing release refusal `Linux v2 native release requires an App admission check` (`production_linux_v2_cleanup_witness_test.go:499`); export refused (`synthetic-goal-cleanup.json`) |
| `shared` | 1 | owner-leg scratch group (`clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240`, `launcher_integration_linux_test.go:439`) before any pi witness runs; export refused (`ack-loss-replay-checkout_ready.json`). Its frozen OpenCode legs are therefore F2-excluded **and not reached on this head** (the set aborts above `opencode_native_health`; in the pre-retarget baseline they stopped at the version gate), and its pi legs have no rows here either — the pre-existing stop named in `docs/TODO.md` must land before either can be read. |

### 4b. Per-set rows (admission campaign)

Run `45481221551` (three sets, final admission bytes) — every set publishes a witness document
binding `harness: "pi"`, `native_version: "1.5.2"`, `installed_owner_verified: true` and the exact
`subject`, and a manifest whose `native_binary` block is the pinned pi executable
(`path /usr/local/lib/symmetry-pi/pi`, `sha256:6a2ac1c5…`, `version 1.5.2`,
`archive_sha256 sha256:811c6f2c…`; artifact `linux-v2-native-evidence-45481221551-1`). The clone
wall is gone from every leg (run 9, §3).

| Set | Exit | Boundary reached | First failing check | First check after it (not reached) |
| --- | --- | --- | --- | --- |
| `stage_replay_create_started` | 1 | the `create_started` stage hold: the marker exists, `linuxV2ValidateStageJournalBoundary` passes, the SIGKILL and the same-RunKey replay both run | the recovery leg reports `recovery_state=unresolved` / `containment_unproven` (the expected state for this boundary, `linuxV2ExpectedStageReplayRecovery`) and exits **0**; `requireLinuxV2RecoveryExitStatus` (`production_linux_v2_owner_cli_test.go:185`) demands a deliberate non-zero exit for `unresolved` | `linuxV2ValidateStageReplayResponse` (the unresolved-state validators) |
| `migration` | 1 | no boundary marker; the witness runs a real native leg (`journal` `claimed`, `has_claim_grant`, `has_clone_intent`, `has_seed_receipt`, `recovery_required`) | the App aborts before its `native_release` barrier: `start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown)` (`linux_owner_witness_linux.go:3279` ← `runner.go:864` ← `linux_owner_v2_linux.go:1066`), so the `native_release` wait (`:616`) times out | — |
| `stale_journal_run_key` | 1 | the setup leg ran (an unresolved-authority RunJournal with `has_clone_intent`, `has_seed_receipt`, `recovery_required` exists) | the pre-fresh-request precondition fails: `A anchor/helper or exact gate was not live before fresh request: exact native process pidfd reports an exited target` (`:534`); the same test then also fails to decode a recovery response | `run_stale_owner_request_denial`'s fresh-request denial legs |

The `stale_journal_run_key` row is a pre-existing **e2e** expectation defect, not a fixture or product
defect: the subtest runs after the setup's owner SIGKILL and required the *parked* gate to be live,
while the product deliberately ends a parked gate on owner loss (§2.17). Run 10 measures the three
`TestProductionLinuxV2StaleOwnerRequestDenials` variants on the *pre-fix* bytes (their own setup does
not pin the gate), and run 11 re-runs all four stale-journal sets with the fix.

Run 11 (post-fix) rows — all four sets, first failing checks only, with the same App stderr
(`recover existing owner witness RunJournal without launch`) and the same unresolved setup journal
(`has_clone_intent`, `has_seed_receipt`, `recovery_required`):

| Set | Exit | Boundary reached | First failing check |
| --- | --- | --- | --- |
| `stale_journal_run_key` | 1 | the setup hold + SIGKILL, the post-kill anchor/helper liveness check, the marker/journal identity binding | `:532` `pin exact live A anchor/helper PIDFD before fresh request: stage owner does not have exact A credentials` (the same pidfds passed the wrapper's liveness check moments earlier) |
| `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | 1 | the setup hold + SIGKILL and the post-kill liveness check | `:269` `read stage-replay recovery response: decode redacted production owner-witness response` — the recovery leg refused and wrote no response, so the denial legs never ran |

Run 10 (pre-fix siblings) rows, same per-set evidence shape (witness document with `harness: "pi"` and
the exact Subject, manifest with the pinned pi `native_binary` block):

| Set | Exit | Boundary reached | First failing check |
| --- | --- | --- | --- |
| `stage_replay_leaf_bound` | 1 | the `leaf_bound` hold, marker + journal boundary + SIGKILL + replay | `:178` `unresolved production recovery exit = <nil>, want deliberate nonzero process exit` (the expected `unresolved` response, but exit 0) |
| `stage_replay_launcher_bound` | 1 | the `launcher_bound` hold + SIGKILL + replay | the same exit-contract check (`:178`) |
| `ack_loss_launcher_bound` | 1 | the ack-loss `launcher_bound` boundary + replay | the same exit-contract check through the ack runner (`:217`) |
| `stale_leaf` | 1 | the `stop_receipt_durable` hold with a **durable stop receipt on the pi vehicle** (the failed dummy-credential turn stops the containment; marker, boundary validator and leaf checks pass), SIGKILL, replay | `:630` `read stale-leaf recovery response: decode redacted production owner-witness response`; the App's stderr is `stage replay unresolved outcome lacks retained journal/owner uncertainty` — its own final-summary validator refuses the unresolved outcome and no response is written |
| `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject` | 1 | the setup hold + SIGKILL and the post-kill anchor/helper liveness check (this head still requires the parked gate) | `:269` `read stage-replay recovery response: decode redacted production owner-witness response` — the same recovery refusal as run 11 |

### 4c. The containment-machinery class: the `migration` release fails, the stale-journal recovery refuses

`migration`'s expected stop is the `native_release` hold, and that hold is written **only after a
successful release** (`linux_owner_witness_linux.go:3207-3211` calls `witness.stage(...release...)`
after the process is live; the `native_release` stage-replay case itself captures the released
process via `linuxV2CaptureLiveProcess`). In run 9 the release never succeeds:

- App stderr: `start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi":
  one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved
  (release_outcome_unknown)` — the chain is `main.go:35` ← `linux_owner_witness_linux.go:3279` ←
  `runner.go:864` (run.fail) ← `linux_owner_v2_linux.go:1066` (`started.resume`) ← the helper's
  RELEASE response (`helper_v2_server.go:1055` `handleRelease` → `prepared.Release` `:1139`).
- The response's `release_outcome_unknown` is written by `helper_v2_server.go:1183-1189` for **any**
  release error (or a lost owner/lease/stop request), and the inner error is deliberately not carried
  into it, so the failing sub-step (gate authorisation, clone/seed bootstrap, native exec, …) is not
  observable in this environment.
- The same guest's product-side scratch legs fail the same class pre-existing:
  `launcher_integration_linux_test.go:439` `native exec failed after RELEASE: errno=1` and
  `clone_integration_linux_test.go:240` `clone status frame magic or version is invalid`
  (run `45465722190`; tracked in `docs/TODO.md:1877-1888`, "no fixture change is proposed without a
  named root").

Verdict: **product-rooted, inner sub-step undetermined**; no owned fixture input can reach the
`native_release` marker while the release fails. Smallest fix (diagnosability, unowned): carry the
failing sub-step and errno into the unresolved release response/error (`helper_v2_server.go:1178-1189`,
`linux_owner_helper_v2_linux.go:308-317`). Consequence for this goal: **every leg whose boundary is
strictly after `native_release` is blocked by this class** — `migration`, `stale_leaf` at
`stop_receipt_durable`, `shared`'s post-release stage/ack holds and the helper-response-loss legs —
so neither the dummy-credential rows nor the credentialed dispatch can green them before the release
defect is named and fixed.

The **pre-release half of the same machinery class** shows up in run 11's four stale-journal sets: the
recovery leg of a `handoff_committed` setup refuses with `recover existing owner witness RunJournal
without launch` (`linux_owner_witness_linux.go:531-534`) and writes **no** response — the refusal
replaces the recovery error, so the failing sub-step is unobservable for the same reason as the
release above — and `stale_journal_run_key` fails its fresh-request precondition one guard earlier
(`:532` `stage owner does not have exact A credentials`) although the wrapper's own post-SIGKILL
liveness check on the *same* anchor/helper pidfds had passed moments before. Neither symptom is
reachable from an owned file; both are pre-existing (these legs never ran this far before the
retarget). The pin guard reads `/proc/<pid>` credentials and requires **A's** effective UID/GID
(`owner_cli_test.go:1676-1684`), so its failure means the anchor/helper process was already gone
milliseconds after the SIGKILL — consistent with the helper's designed owner-loss fail-safe
(`markOwnerLost` → `stopOwned` → exit, `helper_v2_recovery.go:191-236`) resolving the containment
that the test wants to keep unresolved: the App's refusal branch is reached only when
`linuxV2NativeWitnessJournalHasUnresolvedAuthority` (`linux_owner_witness_linux.go:2801-2811`) is
false, i.e. a release proof exists or the containment fields were cleared. The conflict is
semantic — product fail-safe versus the test's "unresolved journal survives" premise — so the
smallest change on either side (product: keep a same-RunKey recovery able to consume the owner-loss
receipt; test: assert the resolved-by-owner-loss state instead) needs an owner ruling rather than an
edit inside this goal's owned set.

`stale_leaf` (run 10) is the third variant of the same class, one step later in the lifecycle: its
setup *does* produce the durable stop receipt on the pi vehicle with dummy credentials (the failed
turn stops the containment fail-closed, and every fixture-side check passes), but the recovery leg's
final summary is refused by the App's own validator (`stage replay unresolved outcome lacks retained
journal/owner uncertainty`) and no response is written. Class census: the recovery exit contract
blocks `stage_replay_create_started`, `stage_replay_leaf_bound`, `stage_replay_launcher_bound` and
`ack_loss_launcher_bound`; the recovery/release decision paths block `migration`, `stale_leaf` and the
four stale-journal sets — i.e. **every retargeted set whose leg survives to its own recovery**, while
the two remaining sets (`synthetic_goal_cleanup`, `shared`) keep the pre-existing stops the F1 ruling
already assigns to the parallel goal.

### 4d. Mutant rows (admission campaign)

The F1 ruling's mutant is the legacy-provisioning revert of one retargeted case, run locally
(`mix run` against the ephemeral probe database) on the same bytes as run 9; the App-level refusal it
names is the pre-admission run's own stop, so both stops are on record.

| # | Mutant | Before (run / observed) | After (probe / observed) |
| --- | --- | --- | --- |
| 1 | `stage_replay_create_started` reverted to the legacy `launch_one_shot_work_item!` route (`Q:/.tools/.temp/provision-mutant.exs`, sha256 `67e4b514996fe8ca4a58e083fd4a8a042193b24ad00ef1cef1d470505706d91e`) | final bytes, run `45481221551`: the case is a goal case (`probe-fixture-final.json` sha256 `d0edafd065f86716223b4bc0e1d77e4c935809f26381ad556e2213d8298c7ddb`: `goal_cases` 39 / `work_items` 9), the writer accepts its admission (`probe-request-final.json` sha256 `13c4dc620c50c79b767fdf211c11a96fe36251822c91d3469264c9230430b861`: `work.input.schema_version == "symmetry.admission.v1"`, non-null `goal_id`, `subject` = the fixture Subject, `provider_scope: null`, no `provider_access`), and the set reaches the `create_started` hold | mutant bytes (`probe-fixture-mutant.json` sha256 `7d3bbed9bcae12daebbddbc9e502ed00570cd702b96b07f9cd70b90510d697d4`: `goal_cases` 38 / `work_items` 10 — exactly one case moved), `probe-run-mutant.json` sha256 `b9439d22bff1dd7fd789e3f26e47814bbc528882e7db86d0203ed4f8ed835464` (the legacy work item and task exist) and **no `probe-request-mutant.json`**: the writer's pi-durability guard (`write-linux-v2-owner-request.py`; `durability ClaimResponse Work input is not a goal admission`) refuses the legacy ClaimResponse, so no request document exists. The App-level stop the ruling names (`Linux clone intent does not match the current admitted Subject`) is the *pre-admission* run `45461719002`'s wall (20 lines, §3 rows 4/6); on the current bytes the same mutant stops one step earlier, at the writer. |
| 2 | the goal-admission route without a native-shaped claim runtime (`register-linux-v2-claim-machine.py` adapter block removed) | final bytes: the sixth claim runtime registers with `harness_kind pi` / `1.5.2` / adapter operations and Control assigns the goal task (`probe-registration-final.json` sha256 `e2df9c51da6a4d04d6ba5bcdfa36f48989f0d796c9aa82827b688f2c9a51e609`) | probe-observed 2026-11-24 11:53 (session transcript; no artifact): Control refuses the goal assignment for the adapter-less runtime with `400` instead of assigning a run, so the fixture cannot claim it; `assignment.ex:332,:519-530` is the guard. |

### 4e. Per-set verdicts (finalizing ruling of 2026-11-25)

Binding run `45489336388` (all twelve retargeted sets, final bytes). Verdict vocabulary from the
conductor's ruling on implement `DECISION_NEEDED` #2: *expected stop proven*, *blocked by product
class* (F1-exit / F2-release / F4-semantics) with run id and the verbatim stop line, or *out of
scope* (owned by the parallel `rf-acc-native-containment-witness`). The classes are carried into the
three follow-up goals named in `docs/TODO.md`.

| Set | Verdict | Run | Verbatim stop line / check |
| --- | --- | --- | --- |
| `stage_replay_create_started` | expected stop proven (the `create_started` hold: marker written, journal boundary validates, SIGKILL + same-RunKey replay) → F1-exit | `45489336388` | `production_linux_v2_native_witness_test.go:178: unresolved production recovery exit = <nil>, want deliberate nonzero process exit` (1.88 s) |
| `stage_replay_leaf_bound` | expected stop proven (`leaf_bound` hold) → F1-exit | `45489336388` | same `:178` line (1.86 s) |
| `stage_replay_launcher_bound` | expected stop proven (`launcher_bound` hold) → F1-exit | `45489336388` | same `:178` line (1.84 s) |
| `ack_loss_launcher_bound` | expected stop proven (ack-loss `launcher_bound` boundary) → F1-exit | `45489336388` | `production_linux_v2_native_witness_test.go:217: unresolved production recovery exit = <nil>, want deliberate nonzero process exit` (1.84 s) |
| `migration` | F2-release (the App's own release aborts before its `native_release` marker) | `45489336388` | `-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown)` (31.02 s) |
| `stale_journal_run_key` | expected stop proven (setup hold, SIGKILL, marker/journal identity binding) → F4-semantics | `45489336388` | `production_linux_v2_native_witness_test.go:532: A anchor/helper was not live before fresh request: exact native process pidfd reports an exited target` (0.00 s); run 11 hit the credential variant of the same class |
| `stale_journal_run_key_legacy` | F4-semantics (the recovery refuses; the fresh-request denial legs never run) | `45489336388` | `production_linux_v2_native_witness_test.go:269: read stage-replay recovery response: decode redacted production owner-witness response`; App stderr `recover existing owner witness RunJournal without launch` |
| `stale_journal_fence` | F4-semantics | `45489336388` | same `:269` line |
| `stale_journal_subject` | F4-semantics | `45489336388` | same `:269` line |
| `stale_leaf` | expected stop proven at the pi turn: the `stop_receipt_durable` hold passes with a durable stop receipt on the pi vehicle (the failed dummy-credential turn stops the containment fail-closed) → F2-release in its recovery | `45489336388` | `production_linux_v2_native_witness_test.go:628: read stale-leaf recovery response: decode redacted production owner-witness response`; App stderr `stage replay unresolved outcome lacks retained journal/owner uncertainty` |
| `synthetic_goal_cleanup` | out of scope (containment-witness); pre-existing stop | `45489336388` | `production_linux_v2_cleanup_witness_test.go:499: start controlled B Git worker through the real Linux v2 launcher/anchor/helper: start "/bin/sh": Linux v2 native release requires an App admission check`; export refused (`pi durability witness is unavailable: synthetic-goal-cleanup.json`) |
| `shared` | out of scope (containment-witness); the pre-existing owner-leg stop fires before any pi witness | `45489336388` | `clone_cleanup_linux_test.go:259: A cleanup caller exited without the requested parent-death signal` + `clone_integration_linux_test.go:240` + `launcher_integration_linux_test.go:439`; export refused (`ack-loss-replay-checkout_ready.json`) |

## 5. Mutant rows (round-1..6 campaign)

Four mutants were probed; the three CI ones run in scratch run `45464759248` (three single-set
probes on deliberately reverted bytes, committed as `7a1da42f` and reverted again before the final
all-12 replay; each "before" column points at the same set's row in `45461719002`).

| # | Mutant | Before (run / observed) | After (run / observed) |
| --- | --- | --- | --- |
| 1 | negative-root generator reverted to `opencode-health.json` (`linux-v2-native-acceptance.sh:1293`) | `45461719002`, `migration`: manifest + witness document with the pinned pi block | `45464759248`, `migration`: `per-harness daemon profile is absent`; no witness document, no manifest |
| 2 | e2e stage-replay runner harness literal reverted to `"opencode"` (`production_linux_v2_owner_cli_test.go:1712`) | `45461719002`, `stage_replay_create_started`: the pi witness's clone wall after a 30.9 s wait | `45464759248`, `stage_replay_create_started`: `production_linux_v2_native_witness_test.go:178: … runtime.harness_kind = "pi", want "opencode"` at 0.00 s |
| 3 | `@cases` entry `stage_replay_stale_leaf` reverted to `opencode-native`/`:opencode` (`provision-linux-v2-control-claims.exs:75`) | `45461719002`, `stale_leaf`: manifest with the pinned pi block | `45464759248`, `stale_leaf`: `per-harness daemon profile is absent`; no witness document, no manifest |
| 4 | exporter's pi durability binding drops the `native_binary_sha256` comparison (`export-linux-v2-redacted-evidence.py:1486-1490`) | `python3 scripts/ci/test-linux-v2-evidence.py -k pi_durability` → `Ran 2 tests … OK` | same command → `FAILED (failures=2)`, `AssertionError: 0 == 0` in `test_manual_pi_durability_digest_or_binding_mismatch_refuses_publication` (mutation `witness digest`); file restored and re-run green |
| 5 | frozen bytes | `git diff 9d113eb6 -- <frozen paths>` empty; six base regions + three frozen OpenCode invocations verbatim | the same checks after every edit: still empty/verbatim (§6) |
| 6 | F3: a mismatched marker gate identity injected before the unresolved-journal guard (`marker.Owners.Gate.Identity = "mutant-mismatched-gate-identity"`, scratch commit `6763a2df`) | run `45485437534`, `stale_journal_run_key`: the guard passes and the leg proceeds to the pidfd pin (`:532` `pin exact live A anchor/helper PIDFD before fresh request: stage owner does not have exact A credentials`) | run `45488371274`: the guard fails at 0.00 s — `production_linux_v2_native_witness_test.go:533: setup RunJournal does not bind the exact live anchor/helper/gate identities`; no manifest. The injected line was removed before run `45489336388` (the §1 equality table). Design source for the relaxed liveness: `gate.c:731-757` (`wait_for_release` polls the owner pidfd and exits `ESRCH`) and `helper_v2_recovery.go:223-225` (`prepared.Abort()` on owner loss); the gate identity itself stays bound by the marker/RunJournal comparison above the liveness pins. |

## 6. Gates (local, final bytes)

Re-run 2026-11-25 on the final worktree bytes (the sha256 table of §1), except where a row says
otherwise. Toolchains: WSL Ubuntu-24.04 (go1.27.0, shellcheck 0.9.0, python 3.12) and WSL Alpine
`symmetry-race` (elixir 1.19.6 / OTP 27); rows whose result depends on the toolchain state both
values.

| Gate (working directory) | Command | Exit | Result |
| --- | --- | --- | --- |
| fixture syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | ok |
| shellcheck delta | `shellcheck -S warning -f gcc` (0.9.0) on `477e4c62` copies vs the final files | 0/0 | delta 0 on all five touched scripts: `linux-v2-native-acceptance.sh` 16 findings, per-code multiset identical (8×SC2154, 2×SC2276, SC1078, SC2024, SC2043, SC2174, SC2288, SC2289); `provision-linux-v2-control-claims.exs` 22/22; `write-linux-v2-owner-request.py` and `register-linux-v2-claim-machine.py` 1/1 each; `linux-v2-native-tools.sh` 0/0 |
| exporter unit tests | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL Ubuntu-24.04) | 0 | `Ran 46 tests … OK` |
| fixture unit tests | `python3 scripts/ci/test-linux-v2-{control-receipt,go-events,host-prerequisite-probe,source-subject}.py` (WSL) | 0 | `OK` |
| python lint/format | `python -m ruff check` / `ruff format --check` | — | not run on the finalization bytes: ruff is not installed in either WSL distro or on Windows. Recorded green on the same bytes in the previous session (`All checks passed!`; `4 files already formatted`); the contract's gate list does not name it. |
| gofmt | `gofmt -l .` in `daemon/` (WSL, go1.27.0) | 0 | no files listed |
| go vet | `GOOS=linux go vet ./...` in `daemon/` (WSL) | 0 | no findings |
| elixir formatter | `(cd control && mix format --check-formatted ../scripts/ci/provision-linux-v2-control-claims.exs)` | 0 (1.19.6) / 1 (1.20.4) | Alpine elixir 1.19.6, the closest available to CI's 1.20.4: rc 0, clean. The CI-parity 1.20.4 run (mise `erlang@29.1.2` `elixir@1.20.4-otp-29`, 2026-11-24) failed **at base and head alike**: copy-format-diff base 27 regions / 248 changed lines, head 26/246 — pre-existing upstream Styler drift, delta 0 for this goal. Ubuntu's elixir 1.14.0 cannot run the project at all (`mix.exs` declares `~> 1.17`), so its rc is not a result. |
| actionlint | not run | — | exempt: no workflow file changes in the goal tree (the throwaway workflow is scratch-only and deleted with the branch; the `git ls-remote` proof is in §3) |
| frozen paths | `git diff 9d113eb6 -- daemon/internal/harness/opencode daemon/internal/harness/testdata/opencode .github/workflows/ci.yml docs/design/harness.md docs/goals/README.md docs/goals/0010-refactor-program.md` and the same against `477e4c62` | 0/0 | both empty |
| frozen regions | `python3 Q:/.tools/.temp/frozen_regions.py` (base `9d113eb6` regions `:21`, `:665`, `:894-895`, `:1002-1003`, `:1081-1097`, `:1116-1125` + the three frozen OpenCode invocation blocks) | 0 | `MISSING= 0` on the final bytes |

## 7. Residual risks and limits

- **Final state of the rows (ruling of 2026-11-25).** The last scratch run `45489336388` (run 13)
  executed the literal all-12 case-set list on the final bytes; §4e maps every set to its verdict.
  No set is green at its contract-expected stop yet: four stop on F1-exit, two on F2-release
  (`migration`, `stale_leaf`), four on F4-semantics (the stale-journal sets), and two (`shared`,
  `synthetic_goal_cleanup`) keep pre-existing stops owned by the parallel
  `rf-acc-native-containment-witness`. The pi plumbing around the legs is green: ten manifests and
  ten witness documents bind `harness: "pi"`, `native_version: "1.5.2"`,
  `installed_owner_verified: true`, the pinned `native_binary` (`sha256:6a2ac1c5…`,
  `archive_sha256:811c6f2c…`) and the exact subject at commit `f2682b71`; each export that has a
  witness file succeeds and the two sets without one fail closed.
- **The clone wall is closed; three further classes bound the rows** (this session, plan §3.10):
  1. the **recovery exit contract** (§4b `stage_replay_create_started`): the App writes the expected
     `unresolved`/`containment_unproven` response but exits 0 at
     `linux_owner_witness_linux.go:501,:556,:596` (the legacy, unresolved-authority and
     `summarizeJournal` branches all `return writeLinuxV2NativeWitnessSummary(...)`), while the e2e's
     `requireLinuxV2RecoveryExitStatus` (`production_linux_v2_owner_cli_test.go:177-193`) requires a
     deliberate non-zero exit for `unresolved` — and the response-loss variant of the same outcome
     *does* join `execution.ErrLinuxV2MutationAckLost` (`:627`), i.e. the non-zero exit is the
     intended semantics. Product-rooted; smallest fix = return an error after writing the summary in
     those three branches. It blocks every set whose expected recovery is `unresolved`.
  2. the **recovery/release decision paths** (§4c): the helper answers `release_outcome_unknown` for
     any release error with the inner sub-step discarded (`migration`), the recovery of a
     `handoff_committed` setup refuses with `recover existing owner witness RunJournal without launch`
     and writes no response (the four stale-journal sets), and `stale_leaf`'s recovery refuses its own
     summary (`stage replay unresolved outcome lacks retained journal/owner uncertainty`) after its
     setup *did* produce the durable stop receipt on the pi vehicle. Together with class 1 this bounds
     every retargeted set whose leg survives to its recovery, and the credentialed dispatch cannot
     green the post-release rows first.
  3. the **parked-gate liveness expectation** (§2.17): fixed in this stage's owned e2e file; run 11
     re-ran the four stale-journal sets, where it proved necessary but not sufficient — the first
     failing checks moved forward to the anchor/helper pin and to the recovery refusal of §4c's
     pre-release paragraph, whose cause is a product-versus-test semantic conflict, not an owned-file
     defect. The relaxation stands under the ruling's condition: the design source proves the old
     expectation unsatisfiable by construction (`gate.c:731-757`, `helper_v2_recovery.go:223-225`),
     the gate identity stays bound by the marker/RunJournal comparison, and the F3 mutant row (run
     `45488371274`) shows a wrong gate identity still fails it.
- **Two sets keep their own pre-existing stops** (out of scope, owned by the parallel
  `rf-acc-native-containment-witness`): `shared`'s owner-leg scratch group (`daemon/native/linux-gate/**`,
  the same guest class as §4c) and `synthetic_goal_cleanup`'s release refusal
  (`Linux v2 native release requires an App admission check`,
  `daemon/internal/execution/linux_owner_v2_linux.go:340`); the F1 ruling already directs both to be
  recorded as unreadable rows.
- **The pi plumbing around the legs is green**: every admission-campaign set published a witness
  document carrying `harness: "pi"`, `native_version: "1.5.2"`, `installed_owner_verified: true` and
  the exact Subject, plus a manifest whose `native_binary` block equals the CI pin
  (`sha256:6a2ac1c5…`, `archive_sha256 sha256:811c6f2c…`; run 13 artifact
  `linux-v2-native-evidence-45489336388-1`, run 9's was `linux-v2-native-evidence-45481221551-1`),
  and the exporter's binding is covered by
  `test-linux-v2-evidence.py` (46 tests) and mutant row 4. The legs' *outcomes* remain bounded by the
  three classes above; `synthetic_goal_cleanup` and `shared` publish no manifest on this head and
  their export fails closed.
- **Credentialed rows are absent by design and additionally blocked**: every scratch run uses the
  contract's dummy credentials, and the post-release boundaries need a successful release that the
  current environment does not produce (§4c), so the Q3(a) turn counts and those rows can only come
  from the conductor-dispatched credentialed run after the release class is named and fixed.
- **The elixir formatter gate fails at base and head** (see §6); the file is not Styler-formatted
  upstream under the CI-parity toolchain and this goal's additions introduce no new formatter hunks;
  the closest available toolchain (elixir 1.19.6) passes clean (§6).
- **`owner-witness-stderr.log` is still never published**: the exporter reads only allowlisted
  sources and refuses unexpected files in its own output directory; the admission-campaign runs
  publish the witness documents and manifests (run 9 artifact above) and the export still fails
  closed for the two sets with no manifest, so the bounded stderr tail stays a fixture-side
  diagnostic only.
- **Scratch residue**: the remote branch is deleted with an empty `git ls-remote` (§3); the local
  scratch worktree with the throwaway workflow and the `gh` shim copies remains on this host only —
  they never entered the goal branch and are not part of the deliverable.

## 8. Appendix: scratch pins, mutants and probe artifacts (verbatim)

Scratch runner (deleted with the branch; values as they ran in every scratch job):

```
GH_TOKEN=gho_scratchdummy0000000000000000000000000000
SYMMETRY_OPENAI_API_KEY=sk-scratch-dummy
SYMMETRY_PI_MODEL=deepseek-v4.1-flash-fast
SYMMETRY_OPENCODE_MODEL=deepseek-v4.1-flash-fast
SYMMETRY_OPENAI_BASE_URL=https://litellm-proxy-hk.graydune-4a6be1e2.eastasia.azurecontainerapps.io/v1
GITHUB_EVENT_NAME=workflow_dispatch
```

The workflow (`.github/workflows/scratch-pi-durability-matrices.yml`, scratch-only) called
`bash scripts/ci/linux-v2-native-acceptance.sh` once per case set under
`SYMMETRY_LINUX_V2_NATIVE_CASE_SET`/`SYMMETRY_LINUX_V2_NATIVE_EVIDENCE_DIR`, and the scratch-only
`gh` shim (`scripts/ci/scratch-gh-shim`) answered only `gh auth login`. No `environment:`,
`secrets.*` or `vars.*` was used; every value above is a dummy.

Admission-campaign local probe (ephemeral WSL pg16 database `symmetry_linux_v2_probe` in the
`control/` app; the exact invocation and console output are in the session transcript, whose
`local://1.log` spill carries the probe's compile output):

| Artifact | sha256 | What it shows |
| --- | --- | --- |
| `pi-admission-probe.exs` | `0d294c08e6efd84b3fd0fac766ee9bc7f74f11107667b497d4f900d16e0a09c0` | the probe itself (refuses any non-`symmetry_linux_v2_probe` database) |
| `probe-fixture-final.json` | `d0edafd065f86716223b4bc0e1d77e4c935809f26381ad556e2213d8298c7ddb` | `prepare` output: 39 goal cases, 9 legacy work items, the durability repository id |
| `probe-request-final.json` | `13c4dc620c50c79b767fdf211c11a96fe36251822c91d3469264c9230430b861` | the writer's request for the claimed goal task: `claim.work.input` = `symmetry.admission.v1` (`admission_id` `2644c387-…`, `goal_id` `715312b6-…`, `goal_revision` 1, `purpose` `implement`, `session_mode` `fresh`, `model_profile` `pi-durability`, `provider_scope` null, `limits.max_turns` 1), `subject` `{resource_id b4fa8387-…, commit 477e4c62…, tree_digest sha256:c4ef64c9…}` equal to the fixture Subject, `claim.work` without `provider_access` |
| `probe-run-final.json` / `probe-machine-token-final` | (not hashed; superseded by the two rows above) | the claimed run and its machine token |
| `probe-registration-final.json` | `e2df9c51da6a4d04d6ba5bcdfa36f48989f0d796c9aa82827b688f2c9a51e609` | the six registered claim runtimes incl. `owner-witness-control-pi-durability-generic` |
| `provision-mutant.exs` | `67e4b514996fe8ca4a58e083fd4a8a042193b24ad00ef1cef1d470505706d91e` | the mutant fixture copy of §4d row 1 |
| `probe-fixture-mutant.json` | `7d3bbed9bcae12daebbddbc9e502ed00570cd702b96b07f9cd70b90510d697d4` | the mutant's `prepare`: 38 goal cases, 10 legacy work items (exactly one case moved) |
| `probe-run-mutant.json` | `b9439d22bff1dd7fd789e3f26e47814bbc528882e7db86d0203ed4f8ed835464` | the mutant's legacy work item + task |
| `probe-request-mutant.json` | (absent) | the writer refused the legacy ClaimResponse; no request document was produced |

Frozen-region checker: `Q:/.tools/.temp/frozen_regions.py` against the base copy
`Q:/.tools/.temp/base-fixture.sh` (= `9d113eb6:scripts/ci/linux-v2-native-acceptance.sh`) and the
final bytes; the six line regions plus the three frozen OpenCode invocations, `MISSING= 0`
(§6).
