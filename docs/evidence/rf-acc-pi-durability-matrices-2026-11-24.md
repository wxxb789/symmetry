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
| `scripts/ci/linux-v2-native-acceptance.sh` | `99c0506947523053e85eef4a71beec422c5b023bb6c2ab53b3818792594983e2` |
| `scripts/ci/linux-v2-native-tools.sh` | `b590f9e577f8028c8f190971ec945be1f15f7622a94db752c14653eef49d813e` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `60bbaea2b699ad2651ac437bc7d21b3e25246a90f5ead47b5ed186d1dd3afb2f` |
| `scripts/ci/write-linux-v2-owner-request.py` | `9b5ce15b067bd22db50a800e005d3335e0f0bf94b9d91debf86e82e2aac44a42` |
| `scripts/ci/register-linux-v2-claim-machine.py` | `6fd8be109e4c4382186cef17f2c7dd9eb67a89c4890cd6cba6f4aa565d4372b7` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `a9da8a94f351df786fabef3e528503d7fb4511dcbb18bd105530e4f922946724` |
| `scripts/ci/test-linux-v2-evidence.py` | `6e7070210b2d72b490d8e4eaff319cd0510f00114bdadedfe484e102aa7f3dc8` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `550be6556da9bb6aa8a966e5c27dc76c75f694f906056ca5c168684fd5eb9874` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `5e3a4b29f5b996362a996fca3eab5d2306dfc3e1035fa6cd602075b3a39bb426` |
| `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` | `f11a77ad67a0eef7f14d52cece1592456fae71e548de2b435af2efa88b7492f1` |
| this record | no self-referential hash |

Finalization binding (2026-11-25, fix stage): the ten values above are the delivered tree's sha256
table, and no scratch run executed these bytes. Run `45489336388` (the last scratch run, §3 row
13) executed the implement stage's final bytes; the apply stage (§9) and the fix stage (§10)
changed nine of the ten files afterwards (`register-linux-v2-claim-machine.py` alone still equals
the bytes run 13 executed). The apply deltas carry the §8b probes P1/P2 for the
exporter membership table and the two line filters only; the F3 delta (where witnesses and the
diagnostic log are written) had no probe, review found it behavior-changing (F01, §10.1), and the
fix restores the configured directory. The literal all-12 re-run on the delivered bytes is carried
to the conductor (§10.11): both stages forbid a push, so no scratch run could be re-dispatched.

The admission campaign (2026-11-25) changed exactly, against the round-3 record's head
`f1f80fb6`: `scripts/ci/linux-v2-native-acceptance.sh` (+51),
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
   observation are part of the same retarget. One shared writer (`linuxV2WriteDurabilityWitness`,
   called once by each of the nine durability-witness runners) binds `native_binary`,
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
8. `scripts/ci/test-linux-v2-evidence.py`: pi fixture plus five cases (pinned-binary happy path,
   binding/digest/tree-digest refusals, non-retargeted case-set refusal, cleanup vehicle binding,
   the `shared` required-witness refusal).
9. `scripts/ci/linux-v2-native-acceptance.sh` (found by round-3 run `45460144698`, plan §3.7): the
   fixed-B account's provisioned HOME moves inside the isolated owner witness root
   (`B_HOME="$OWNER_TEST_ROOT/fixed-b-home"`, `:493`), because the App refuses any passwd HOME
   outside `/opt/symmetry-owner-test` (`linux_owner_witness_linux.go:2902-2905`). `machine_run_as`
   derives `HOME` per account (`:975-991`). The fixed-B XDG cache, `.pi/agent`, the Pi session
   directory and the A `--x` ACLs all resolve through `$B_HOME`, so one assignment moves them.
10. `daemon/e2e/production_linux_v2_owner_cli_test.go:53,277-282` + the acceptance script's failure
    context (`linux-v2-native-acceptance.sh:1705-1717`, printing at `:1716`): the witness command's
    stderr goes to a private 0600 `owner-witness-stderr.log` beside the witness documents instead
    of `/dev/null`, and the acceptance script prints a bounded tail when a leg fails. This is the
    instrument that named both refusals above.
11. `daemon/e2e/production_linux_v2_owner_cli_test.go:3783`: the migration denial's wait message no
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
15. `daemon/e2e/production_linux_v2_native_witness_test.go:1564-1572`: the old assertion that every
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
17. `daemon/e2e/production_linux_v2_native_witness_test.go:436-450,523-527`: the unresolved-journal
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
| 13 | `45489336388` | all twelve retargeted sets, literal, on the implement stage's final goal-tree bytes (the apply and fix stages changed nine of the ten subject files afterwards — §9, §10) | `f2682b71` | twelve exit-1 sets, every row reproducing runs 9/10/11 (§4e); ten manifests carrying the pinned pi `native_binary` block (`sha256:6a2ac1c5…`, `1.5.2`, subject commit `f2682b71`; artifact `linux-v2-native-evidence-45489336388-1`), the same two export refusals (`shared`, `synthetic_goal_cleanup`), and witness documents with `harness: "pi"` / `installed_owner_verified: true`. Job 16m42s. |

Every scratch run settled before its stage closed (no run pending). The scratch branch was deleted
after run 13 (`git push origin --delete scratch/rf-acc-pi-durability-matrices`) and
`git ls-remote origin scratch/rf-acc-pi-durability-matrices` prints nothing; earlier
deletions/recreations are recorded in rows 6 and 11. The last run (13) is the literal all-12 run on
the implement stage's final bytes; it does not bind the delivered bytes (§1, §10.11).

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
(`owner_cli_test.go:1686-1694`), so its failure means the anchor/helper process was already gone
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
| 1 | `stage_replay_create_started` reverted to the legacy `launch_one_shot_work_item!` route (`<scratch>/provision-mutant.exs`, sha256 `67e4b514996fe8ca4a58e083fd4a8a042193b24ad00ef1cef1d470505706d91e`) | the bytes run `45481221551` executed: the case is a goal case (`probe-fixture-final.json` sha256 `d0edafd065f86716223b4bc0e1d77e4c935809f26381ad556e2213d8298c7ddb`: `goal_cases` 39 / `work_items` 9), the writer accepts its admission (`probe-request-final.json` sha256 `13c4dc620c50c79b767fdf211c11a96fe36251822c91d3469264c9230430b861`: `work.input.schema_version == "symmetry.admission.v1"`, non-null `goal_id`, `subject` = the fixture Subject, `provider_scope: null`, no `provider_access`), and the set reaches the `create_started` hold | mutant bytes (`probe-fixture-mutant.json` sha256 `7d3bbed9bcae12daebbddbc9e502ed00570cd702b96b07f9cd70b90510d697d4`: `goal_cases` 38 / `work_items` 10 — exactly one case moved), `probe-run-mutant.json` sha256 `b9439d22bff1dd7fd789e3f26e47814bbc528882e7db86d0203ed4f8ed835464` (the legacy work item and task exist) and **no `probe-request-mutant.json`**. **On the delivered bytes the mutant stops at the writer**, not at the clone wall: the writer's pi-durability guard refuses the legacy ClaimResponse at `write-linux-v2-owner-request.py:193-196` with `durability ClaimResponse Work input is not a goal admission`, so no request document exists and the App never sees the claim. The clone-intent stop the ruling names (`Linux clone intent does not match the current admitted Subject`) is recorded from the *pre-admission* run `45461719002` (20 lines, §3 rows 4/6), whose bytes predate the writer guard. CI job: `linux-native-v2-acceptance` (non-root `runner`; the guest go-test runs as the fixture A account). |
| 2 | the goal-admission route without a native-shaped claim runtime (`register-linux-v2-claim-machine.py` adapter block removed) | final bytes: the sixth claim runtime registers with `harness_kind pi` / `1.5.2` / adapter operations and Control assigns the goal task (`probe-registration-final.json` sha256 `e2df9c51da6a4d04d6ba5bcdfa36f48989f0d796c9aa82827b688f2c9a51e609`) | probe-observed 2026-11-24 11:53 (session transcript; no artifact): Control refuses the goal assignment for the adapter-less runtime with `400` instead of assigning a run, so the fixture cannot claim it; `assignment.ex:332,:519-530` is the guard. |

### 4e. Per-set verdicts (finalizing ruling of 2026-11-25)

Binding run `45489336388` (all twelve retargeted sets, the implement stage's final bytes — §3 row
13; the delivered bytes of §1 have no run yet, §10.11). Verdict vocabulary from the
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
| 4 | exporter's pi durability binding drops the `native_binary_sha256` comparison (`export-linux-v2-redacted-evidence.py:1486-1490`) | `python3 scripts/ci/test-linux-v2-evidence.py -k pi_durability` → `Ran 2 tests … OK` (apply-stage bytes) | same command → `FAILED (failures=2)`, `AssertionError: 0 == 0` in `test_manual_pi_durability_digest_or_binding_mismatch_refuses_publication` (mutation `witness digest`); file restored and re-run green. At the delivered bytes the selector matches 5 cases and stays green (§10.9) |
| 5 | frozen bytes | `git diff 9d113eb6 -- <frozen paths>` empty; six base regions + three frozen OpenCode invocations verbatim | the same checks after every edit: still empty/verbatim (§6) |
| 6 | F3: a mismatched marker gate identity injected before the unresolved-journal guard (`marker.Owners.Gate.Identity = "mutant-mismatched-gate-identity"`, scratch commit `6763a2df`) | run `45485437534`, `stale_journal_run_key`: the guard passes and the leg proceeds to the pidfd pin (`:532` `pin exact live A anchor/helper PIDFD before fresh request: stage owner does not have exact A credentials`) | run `45488371274`: the guard fails at 0.00 s — `production_linux_v2_native_witness_test.go:533: setup RunJournal does not bind the exact live anchor/helper/gate identities`; no manifest. The injected line was removed before run `45489336388` (the §1 equality table). Design source for the relaxed liveness: `gate.c:731-757` (`wait_for_release` polls the owner pidfd and exits `ESRCH`) and `helper_v2_recovery.go:223-225` (`prepared.Abort()` on owner loss); the gate identity itself stays bound by the marker/RunJournal comparison above the liveness pins. |

## 6. Gates (local, delivered bytes)

Re-run 2026-11-25 by the fix stage on the delivered worktree bytes (the sha256 table of §1).
Toolchains: WSL Ubuntu-24.04 (go1.27.0, shellcheck 0.9.0, python 3.12.3), WSL Alpine
`symmetry-race` (elixir 1.19.6 / OTP 28) and, for the CI-parity formatter, Windows mise
`elixir@1.20.4-otp-29` / `erlang@29.1.2`; rows whose result depends on the toolchain state both
values.

| Gate (working directory) | Command | Exit | Result |
| --- | --- | --- | --- |
| fixture syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh`; `bash -n scripts/ci/linux-v2-native-tools.sh` (WSL) | 0/0 | ok |
| shellcheck delta | `shellcheck -S warning -f gcc` (0.9.0, WSL) on the `cc725d50` copies vs the delivered bytes; six scripts | 0/0 | delta 0: `linux-v2-native-acceptance.sh` 16/16 (per-code histogram identical: 8×SC2154, 2×SC2276, SC1078, SC2024, SC2043, SC2174, SC2288, SC2289); `linux-v2-native-tools.sh` 0/0; `provision-linux-v2-control-claims.exs` 22/22; `write-linux-v2-owner-request.py`, `register-linux-v2-claim-machine.py`, `export-linux-v2-redacted-evidence.py` 1/1 each. Windows shellcheck 0.11.0 on the delivered acceptance script: 16 findings. |
| exporter unit tests | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL Ubuntu-24.04) | 0 | `Ran 49 tests … OK` (47 at the apply stage; the fix stage's checked-set and acceptance-list cases are the additions) |
| fixture unit tests | `python3 scripts/ci/test-linux-v2-{control-receipt,go-events,host-prerequisite-probe,source-subject}.py` (WSL) | 0 | `OK` (four suites) |
| python compile | `python3 -m py_compile` on `export-linux-v2-redacted-evidence.py`, `write-linux-v2-owner-request.py`, `test-linux-v2-evidence.py` (WSL) | 0 | clean |
| python lint/format | `ruff check` / `ruff format --check` | — | not run at the fix stage either: ruff is not installed in either WSL distro or on Windows; the contract's gate list does not name it |
| gofmt | `gofmt -l .` in `daemon/` (WSL, go1.27.0) | 0 | no files listed |
| go vet | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` in `daemon/` (WSL) | 0 | clean |
| go build | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` in `daemon/` (WSL) | 0 | clean |
| elixir formatter | `(cd control && mix format --check-formatted ../scripts/ci/provision-linux-v2-control-claims.exs)` | 1 (1.20.4) / 0 (1.19.6) | **Amendment (F10(i), §10.9/§10.10)**: the CI-parity 1.20.4/OTP 29 run is red at base and delivered alike — pre-existing Styler drift on bytes this goal did not touch (base `33ad88a9` 110 removed/138 added lines, delivered 106/137; 240 at the `9d113eb6` copy). At every goal-touched rejected line the base counterpart is rejected too, and the one new-layout rejection the hunk-scoped check found was rewritten to the formatter's own layout. `symmetry-race` elixir 1.19.6: rc 0, clean. Ubuntu's elixir 1.14.0 cannot run the project (`mix.exs` declares `~> 1.17`). |
| actionlint | not run | — | exempt: no workflow file changes in the goal tree (the throwaway workflow is scratch-only and deleted with the branch; the `git ls-remote` proof is in §3) |
| frozen paths | `git diff 9d113eb6 -- daemon/internal/harness/opencode daemon/internal/harness/testdata/opencode .github/workflows/ci.yml docs/design/harness.md docs/goals/README.md docs/goals/0010-refactor-program.md` | 0 | empty (0 bytes) at the delivered bytes |
| frozen regions | a region checker over the `9d113eb6` copy (regions `:21`, `:665`, `:894-895`, `:1002-1003`, `:1081-1097`, `:1116-1125`) that matches each verbatim in the delivered file, plus the 20 frozen OpenCode-pattern lines extracted from the `cc725d50` copy | 0 | `MISSING_TOTAL= 0` |

## 7. Residual risks and limits

- **Final state of the rows (ruling of 2026-11-25).** The last scratch run `45489336388` (run 13)
  executed the literal all-12 case-set list on the implement stage's final bytes (§3 row 13); §4e
  maps every set to its verdict. The delivered bytes have no run — the literal all-12 re-run on
  them is carried to the conductor (§10.11).
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
- **The apply-stage witness-output regression (F01) is fixed** (§10.1): the apply stage's F3
  refactor made `linuxV2NativeWitnessOutputDirectory` return `fixture.artifactDir` instead of the
  configured `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR`, so on those bytes every
  witness document and the diagnostic log landed in the per-test `os.MkdirTemp` directory the
  fixture cleanup removes and no set could publish a manifest. The delivered bytes return the
  configured directory; the extracted-function probe (§10.10) observed the misrouting at the
  pre-fix bytes and the configured path at the delivered bytes, and the mutant row waits on the
  conductor's all-12 run (§10.9).
- **The pi plumbing around the legs is green**: every admission-campaign set published a witness
  document carrying `harness: "pi"`, `native_version: "1.5.2"`, `installed_owner_verified: true` and
  the exact Subject, plus a manifest whose `native_binary` block equals the CI pin
  (`sha256:6a2ac1c5…`, `archive_sha256 sha256:811c6f2c…`; run 13 artifact
  `linux-v2-native-evidence-45489336388-1`, run 9's was `linux-v2-native-evidence-45481221551-1`),
  and the exporter's binding is covered by
  `test-linux-v2-evidence.py` (49 tests) and mutant row 4. The legs' *outcomes* remain bounded by the
  three classes above; `synthetic_goal_cleanup` and `shared` publish no manifest on this head and
  their export fails closed.
- **Credentialed rows are absent by design and additionally blocked**: every scratch run uses the
  contract's dummy credentials, and the post-release boundaries need a successful release that the
  current environment does not produce (§4c), so the Q3(a) turn counts and those rows can only come
  from the conductor-dispatched credentialed run after the release class is named and fixed.
- **The elixir formatter gate fails at base and delivered bytes alike** (amendment F10(i), §6/§10.9):
  the file is not Styler-formatted upstream under the CI-parity toolchain, so the gate is recorded
  as an amendment with a `docs/TODO.md` entry; the hunk-scoped check (§10.10) shows every
  goal-touched rejected line has a rejected base counterpart and the one new-layout rejection it
  found was rewritten to the formatter's own layout. The closest available toolchain (elixir
  1.19.6) passes clean (§6).
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

Frozen-region checker: a scratch python probe (`<scratch>/frozen_regions.py`) against a scratch copy
of the base file (`<scratch>/base-fixture.sh` = `9d113eb6:scripts/ci/linux-v2-native-acceptance.sh`)
and the final bytes; the six line regions plus the three frozen OpenCode invocations, `MISSING= 0`
(§6). The fix stage re-ran the equivalent content check at the delivered bytes (§6,
`MISSING_TOTAL= 0`).

### 8b. Apply-stage probes (2026-11-25, verbatim)

Host locations inside the pasted probe code read `<worktree>` for the checkout and `<scratch>` for
the session scratch directory; the recorded sha256 values anchor the files as they ran.

Probe P1 (`sha256 b307ddab88afab8930a76cf01df04e205a768023c058951d8340b8d726b1a75c`) — exporter
membership: loads the pre-apply exporter (`sha256 a21a22d9…`, §9) and the post-apply one, compares
the new `PI_DURABILITY_REQUIRED.get(case_set, MANUAL_CASE_SET_FILES[case_set])` against the
pre-apply `PI_DURABILITY_WITNESSES` for all 12 case sets, and re-asserts the `shared`
strict-subset invariant. Ran as `python3 <source>` in the worktree.

```python
import importlib.util, json, pathlib, sys

def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod

old = load(r"<scratch>/pre-apply-exporter.py", "old_exporter")
new = load(r"<worktree>/scripts/ci/export-linux-v2-redacted-evidence.py", "new_exporter")

def new_expected(case_set):
    return new.PI_DURABILITY_REQUIRED.get(case_set, new.MANUAL_CASE_SET_FILES[case_set])

mismatches = []
for case_set, required in old.PI_DURABILITY_WITNESSES.items():
    if new_expected(case_set) != required:
        mismatches.append(case_set)
keysets_equal = set(old.PI_DURABILITY_WITNESSES) == set(new.PI_DURABILITY_CASE_SETS)
shared_subset = new.PI_DURABILITY_REQUIRED["shared"] < new.MANUAL_CASE_SET_FILES["shared"]
shared_counts = (len(new.PI_DURABILITY_REQUIRED["shared"]), len(new.MANUAL_CASE_SET_FILES["shared"]))
cleanup = new.CLEANUP_VEHICLE_WITNESS == "synthetic-goal-cleanup.json"
print(json.dumps({
    "case_set_keys_equal": keysets_equal,
    "expected_set_mismatches": mismatches,
    "shared_strict_subset": shared_subset,
    "shared_required_of_allowlist": shared_counts,
    "cleanup_constant": cleanup,
}, sort_keys=True))
print("PROBE_RESULT:", "IDENTICAL" if (keysets_equal and not mismatches and shared_subset and cleanup) else "MISMATCH")
```

Result: `{"case_set_keys_equal": true, "cleanup_constant": true, "expected_set_mismatches": [], "shared_required_of_allowlist": [30, 35], "shared_strict_subset": true}` → `PROBE_RESULT: IDENTICAL`.

Probe P2 (`sha256 7fffecdfbeeb0810e6e998d0d22bd375231079bcae113c7afebb04c758abf5d4`) — F4
equivalence: extracts the two pre-apply line-filter python bodies from the pre-apply script
(`sha256 143d78ea…`, §9) and the post-apply `redact_evidence_lines` body, both LF-canonicalized;
feeds an identical 120-line corpus (`sha256 a1a0d0a34aad459a2c387a66c979496cef6b5071ab739730688298fa1de1fdbd`;
secrets, CRLF, 400/401/500-byte lines, an empty line, a final line without newline) to both.

```python
import pathlib, re, subprocess, sys, hashlib

repo = pathlib.Path(r"<worktree>")
probe = pathlib.Path(r"<scratch>")

def extract(path_bytes, start_marker):
    text = path_bytes.decode("utf-8").replace("\r\n", "\n")
    start = text.index(start_marker)
    return re.search(r"python3 -c '(.*?)'", text[start:], re.S).group(1)

pre = (probe / "pre-apply-acceptance.sh").read_bytes()
work = (repo / "scripts/ci/linux-v2-native-acceptance.sh").read_bytes()
old_drop = extract(pre, "print_filtered_output_lines() {")
old_trunc = extract(pre, "print_witness_stderr_tail() {")
new_all = extract(work, "redact_evidence_lines() {")

for name, body in (("old_drop_lf.py", old_drop), ("old_trunc_lf.py", old_trunc), ("new_redact_lf.py", new_all)):
    (probe / name).write_bytes(body.encode())
    print(name, "sha256:", hashlib.sha256(body.encode()).hexdigest())

secrets = probe / "secrets.bin"
secrets.write_bytes(b"SECRETVALUE\0OTHER-SECRET\0\0")
corpus = []
for i in range(120):
    k = (1, 399, 400, 401, 500)[i % 5]
    body = f"line-{i:03d}-".encode() + b"x" * k
    if i % 7 == 3:
        body += b"SECRETVALUE"
    if i % 11 == 5:
        body += b"OTHER-SECRET"
    ending = b"" if i == 119 else (b"\r\n" if i % 3 == 1 else b"\n")
    corpus.append(body + ending)
corpus.append(b"")
corpus.append(b"tail-no-newline")
data = b"".join(corpus)
(probe / "corpus.bin").write_bytes(data)
print("corpus sha256:", hashlib.sha256(data).hexdigest())

def run(path):
    return subprocess.run([sys.executable, str(path), str(secrets)], input=data, capture_output=True)

def run_new(mode, bound, keep):
    return subprocess.run([sys.executable, str(probe / "new_redact_lf.py"), mode, str(bound), str(keep), str(secrets)],
                          input=data, capture_output=True)

failures = 0
for name, old, new in (
    ("drop 400 40", run(probe / "old_drop_lf.py"), run_new("drop", 400, 40)),
    ("truncate 400 0", run(probe / "old_trunc_lf.py"), run_new("truncate", 400, 0)),
):
    same = (old.stdout == new.stdout, old.returncode == new.returncode, old.stderr == new.stderr)
    print(f"{name}: stdout_equal={same[0]} rc_equal={same[1]}({old.returncode}/{new.returncode}) stderr_equal={same[2]} bytes={len(old.stdout)}/{len(new.stdout)}")
    failures += (not all(same))
print("PROBE_RESULT:", "IDENTICAL" if failures == 0 else f"{failures} MISMATCH")
```

Extracted bodies (LF-canonical; a reader re-derives them from the two sha256-pinned scripts):
pre-apply drop filter `sha256 cc72ded0611ce4bdf4275bcb1472b943d29059f76c8d1fc45493c0310380f70a`:

```python
import pathlib, sys

secrets_path, = sys.argv[1:]
try:
    secrets = [value for value in pathlib.Path(secrets_path).read_bytes().split(b"\0") if value]
except OSError:
    secrets = []
lines = []
for line in sys.stdin.buffer:
    if len(line.rstrip(b"\r\n")) > 400 or any(secret in line for secret in secrets):
        continue
    lines.append(line)
for line in lines[-40:]:
    sys.stdout.buffer.write(line)
```

pre-apply truncate filter `sha256 49409d2fc7ec43ede230a58b8f5d75738c8a5d7d7fd911c9950716ac1335e526`:

```python
import pathlib, sys

secrets_path, = sys.argv[1:]
try:
    secrets = [value for value in pathlib.Path(secrets_path).read_bytes().split(b"\0") if value]
except OSError:
    secrets = []
for line in sys.stdin.buffer:
    for secret in secrets:
        line = line.replace(secret, b"<redacted>")
    sys.stdout.buffer.write(line[:400].rstrip(b"\r\n") + b"\n")
```

post-apply stage `sha256 5213d37e9208e667f91bc6b1249393870f242d7202696e1c53226d1cde08bdda` (the body
embedded in `redact_evidence_lines`):

```python
import pathlib, sys

mode, bound, keep, secrets_path = sys.argv[1:5]
bound = int(bound)
keep = int(keep)
try:
    secrets = [value for value in pathlib.Path(secrets_path).read_bytes().split(b"\0") if value]
except OSError:
    secrets = []
lines = []
for line in sys.stdin.buffer:
    if mode == "drop":
        if len(line.rstrip(b"\r\n")) > bound or any(secret in line for secret in secrets):
            continue
        lines.append(line)
    else:
        for secret in secrets:
            line = line.replace(secret, b"<redacted>")
        sys.stdout.buffer.write(line[:bound].rstrip(b"\r\n") + b"\n")
if mode == "drop":
    for line in lines[-keep:]:
        sys.stdout.buffer.write(line)
```

Result on both rows: `stdout_equal=True rc_equal=True(0/0) stderr_equal=True`, bytes `204/204` and
`38830/38830`; `PROBE_RESULT: IDENTICAL`.

## 9. Simplification apply (2026-11-25)

The simplify review (report-only) raised seven tidy-up findings plus two record/rule-vs-code items;
the apply stage accepted all nine and rejected none. Six files changed; the columns are the
pre-apply sha256 — the bytes run `45489336388` executed on — and the post-apply sha256 now in §1.

| File | pre-apply sha256 | post-apply sha256 |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `143d78ea6b29e1cf8b76a16f0929634c11c932e396ab64120125dd36bf22961e` | `d1bca15691216c6ed703348c8d9d08736d35a26c4f4f674f364d2a9fea771ec1` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `a21a22d97f09d4afec2fee7cc6b035684c78479ecb3f37dc33d5822e82794d25` | `d246887f8c9fae2f1a68eaee9bc8e38b6a5d72bd3ce537d0451d5493243ffecd` |
| `scripts/ci/test-linux-v2-evidence.py` | `afb9b7c2a4660645e2d1f6c24082d1c9c4d17f69641189f7007115e3c63bdcb9` | `1efb4850e5f49a2521b2dc125959a9887cab2d83b96e92f2b556de7d9bf730d2` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `bddf80d702e7ef033104b338bb2d18c01b0fed981ff10acbda256837ff4a2f8e` | `412e67a46fd03b047d1012761801feeaefdcecea7d5908d31998066635617170` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `5b3bf2f3db0ba6352f1dca79a2bc845a20e88f56748b0636ac3657537cfd6d62` | `f71556913ff515843cb8555b66baa6811af862df06de2e7d8104181a75b89c51` |
| `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` | `25d5ee972acf2e11994751526ed3dab8308d3488fe380bc29221bd35b04f96f5` | `f11a77ad67a0eef7f14d52cece1592456fae71e548de2b435af2efa88b7492f1` |

Applied: F1 exporter table dedup (`PI_DURABILITY_WITNESSES` + `PI_VEHICLE_ONLY_WITNESSES` →
`PI_DURABILITY_CASE_SETS` + a `shared`-only `PI_DURABILITY_REQUIRED` + one `CLEANUP_VEHICLE_WITNESS`
constant; membership unchanged, subset guard kept, probe P1). F2 one `linuxV2WriteDurabilityWitness`
replacing nine bind+write defers (eight matrix/negative runners in
`production_linux_v2_owner_cli_test.go`, one in the helper-exit wrapper — the plan §3.2
declared-and-accepted scope addition already inside the increment, so no new file enters the PR).
F3 `linuxV2NativeWitnessOutputDirectory` shared by the output-path and diagnostic-path helpers —
the refactor dropped the configured-directory return; §10.1 restores it.
F4 one `redact_evidence_lines` pipeline stage for both line filters (probe P2). F5 `machine_run_as`
fail-closed `case` over the two fixture accounts. F6 "is not a supported native kind" message +
pinned assertion. F7 the `shared` required-witness gate case (suite 46 → 47 tests). D1 two-line
parked-gate comment (both prior facts kept). D2 the diagnostic-path comment and §2.10 now name the
acceptance script as the printer.

"Every X" censuses: the writer has one definition and nine call sites (`linuxV2WriteDurabilityWitness`:
9 lines in `production_linux_v2_owner_cli_test.go` incl. the definition, 1 call in the helper-exit
wrapper); `linuxV2BindDurabilityNativeBinary` has no remaining direct call site outside the writer;
`linuxV2NativeWitnessOutputPath(t, fixture)` has 9 call sites and
`linuxV2OwnerWitnessDiagnosticPath(t, fixture)` one, both resolving through the new directory
helper; `redact_evidence_lines` has 2 call sites and one definition; `machine_run_as` has 40 call
sites (34 × `"$A_USER"`, 6 × `"$B_USER"`) and no other account form, so the new `fail` arm is
unreachable on the current fixture.

No new scratch run was dispatched (push is forbidden in this stage): run `45489336388`'s binding to
its own bytes stands and the apply deltas are the §6-re-run local gates plus probes P1/P2. The
apply stage's claim that package-level behavior differs only where a finding changed a guard (F5's
rejection arm) or a gate case set (F6/F7) is **falsified for the F3 delta** — the dropped
configured-directory return changed where every retargeted witness is written (§10.1) — so the
delivered bytes carry no behavior-neutrality claim (§1).

## 10. Fix stage (2026-11-25, DS review MUST_FIX F01–F10)

Eight of the ten subject files changed against the post-apply bytes (the apply stage's six and the
fix stage's eight overlap; `register-linux-v2-claim-machine.py` alone is untouched since run 13).
The stage cannot push, so the literal all-12 scratch run on the delivered bytes is carried to the
conductor (§10.11) and no probe below substitutes for it. Acceptance-script citations written for
earlier heads shift by +2 after `:459` (the fix's staging hunk); §10's own citations use the
delivered bytes.

| Finding | Sev | Where | Disposition |
| --- | --- | --- | --- |
| F01 | P0 | `daemon/e2e/production_linux_v2_owner_cli_test.go:255-269` | fixed — the configured output directory wins (§10.1) |
| F02 | P1 | record §1/§3 row 13/§4e/§9, plan §7 | fixed — the false behavior-neutrality claims are gone and the pre-apply binding is recorded (§10.2) |
| F03 | P2 | contract owned list | amendment recorded in the contract's DS ruling; plan §3.2 cites it (§10.3) |
| F04 | P3 | `daemon/e2e/production_linux_v2_native_witness_test.go:1578` | fixed — the refusal names the case's harness (§10.4) |
| F05 | P3 | `scripts/ci/export-linux-v2-redacted-evidence.py:1468` | fixed — `expected` derives from the allowlist minus five named non-pi names (§10.5) |
| F06 | P3 | `scripts/ci/linux-v2-native-acceptance.sh:260-284`, `test-linux-v2-evidence.py` | fixed — the export arm defaults to pi; a suite case pins the two lists equal (§10.6) |
| F07 | P3 | record §4d row 1 | fixed — CI job and the writer's stop are named (§10.7) |
| F08 | P3 | `scripts/ci/write-linux-v2-owner-request.py:29` | fixed — derived from `CASES` (§10.8) |
| F09 | P3 | record §2 item 7, plan §3.4 | record-only: the `synthetic_goal_cleanup` carve-out is named (§10.8) |
| F10 | P3 | contract gates; `tools.sh`; `acceptance.sh:459`; `provision-…-claims.exs:228-234` | (i) amendment recorded with a `docs/TODO.md` entry; (ii) fixed with the whole-tree digest; the one hunk-scoped formatter rejection found was rewritten to the formatter's own layout (§10.9/§10.10) |

### 10.1 F01 — the configured witness output directory

`linuxV2NativeWitnessOutputDirectory` validated and created the directory named by
`SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR` and then returned `fixture.artifactDir`;
the accept script sets that variable for every witness go-test
(`linux-v2-native-acceptance.sh:1596,1896,1916,1926,1933`) and the exporter reads witnesses from
the same root, so on the pre-fix bytes every witness document and `owner-witness-stderr.log`
landed in the per-test `os.MkdirTemp` directory the fixture cleanup deletes (`fixture.artifactDir`)
and no set could publish a manifest. The delivered bytes return the configured directory.

Census — 12 resolutions, all converted: the ten `fixture.outputPath` writes through
`linuxV2NativeWitnessOutputPath` (`production_linux_v2_owner_cli_test.go:1743,2300,3356,3529,3772,
3932,4085,4248,4392` and `production_linux_v2_helper_exit_witness_test.go:148`), the
unprovisioned-refusal document (`production_linux_v2_native_witness_test.go:824`, resolving to the
`unprovisioned` output directory the acceptance script sets for that test), and
`linuxV2OwnerWitnessDiagnosticPath` (`production_linux_v2_owner_cli_test.go:53`). Exempt: the
cleanup witness resolves the same environment name on its own
(`production_linux_v2_cleanup_witness_test.go:182-184`, unowned) and already requires the
configured directory when the witness is enabled.

Proof: the extracted-function probe (§10.10) observed the misrouting at the pre-fix bytes
(`configured output directory ignored: got <scratch>/artifact want <scratch>/witness-output`), the
configured path at the delivered bytes, the unset fallback, and the non-canonical refusal. The CI
consequence (no witness → the exporter refuses `pi durability witness is unavailable: <file>`) is
mutant row §10.9-1, whose CI half waits on §10.11's run.

### 10.2 F02 — record corrections

§1's finalization paragraph no longer claims the apply deltas are behavior-neutral: it states that
run 13 executed the implement stage's bytes, that only the exporter membership and the line filters
had probes (P1/P2), and that the carried run is §10.11. §3 row 13, §4e and plan §7 qualify the run
the same way, plan §7's "final bytes, bound by the §1 equality table" sentence is gone, and §9's
closing neutrality claim is annotated as falsified for the F3 delta.

### 10.3 F03 — the owned-set amendment

No tree change for the finding itself: the conductor's DS ruling (2026-11-25, F03) amends the
contract's owned list to include `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` for
the harness-literal retarget only, and plan §3.2 now cites that amendment, so the delivered change
ships inside the owned set.

### 10.4 F04 — the provider-access refusal names its harness

`production_linux_v2_native_witness_test.go:1577-1578` now builds the refusal from the case:
`fmt.Errorf("non-provider %s owner-witness claim unexpectedly carries provider access",
testCase.harness)`. The guard's behavior is unchanged (verified: the branch is the same
`else if request.Claim.ProviderAccess != nil`), and no test pins the old text (grep over the tree).
The branch is a fail-closed refusal that no passing run exercises → oracle note §10.9-2.

### 10.5 F05 — every allowlisted witness is checked

`validate_pi_durability_witnesses` derives `expected = MANUAL_CASE_SET_FILES[case_set] -
PI_VEHICLE_INDEPENDENT_WITNESSES`; the vacuous `unallowlisted` check is gone because the derivation
can no longer name a name outside the allowlist. The five exclusions are exactly the three pi
provider-action receipts (`control-provider-action-{pi,helper-crash,daemon-crash}.json`, published
from the receipt root, not pi-durability witnesses) and the two OpenCode-vehicle documents
(`opencode-native-health.json`, `unprovisioned-owner-refusal.json`). A missing document still
refuses (`pi durability witness is unavailable: <file>`).

Proof: the suite case
`test_pi_durability_checks_every_allowlisted_witness_except_the_named_non_pi_documents` pins the
exclusions to the receipt targets plus the two OpenCode documents and the non-`shared` sets'
checked set to their allowlist, and the mutant probe (§10.10) shows the pre-fix bytes publishing a
newly allowlisted witness unchecked while the delivered bytes refuse it.

### 10.6 F06 — one driving list, one equality pin

`export_native_evidence`'s case statement now has exactly the two containment arms and a `*)` arm
holding the pi block, so the export path cannot silently omit a retargeted set: any other set in
the validation list is routed to `--native-kind pi`, and the exporter refuses pi outside its own
`PI_DURABILITY_CASE_SETS`. The suite case
`test_acceptance_case_set_lists_match_the_exporter_pi_durability_sets` parses the acceptance
script's validation list and export switch and pins both (validator = the 12 retargeted sets plus
codex/claude; export arms = codex, claude, `*` with `native_kind=pi`). Mutants §10.9-4/-5.

### 10.7 F07 — the mutant row names its job and stop

§4d row 1 now carries `CI job: linux-native-v2-acceptance` (non-root `runner`; the guest go-test
runs as the fixture A account) and the writer's stop on the delivered bytes: the legacy
ClaimResponse is refused at `write-linux-v2-owner-request.py:193-196` with `durability
ClaimResponse Work input is not a goal admission`.

### 10.8 F08/F09

F08: `PROVIDER_ACCESS_CASES` is now `{name for name, (_, profile, _) in CASES.items() if profile
== "pi-native"}` — the same three names on these bytes, derived so the writer's two tables cannot
drift apart; the Control fixture's separate `repository_role` column still fails closed at claim
time (§4d row 1). F09 stays a documented carve-out: `synthetic_goal_cleanup`'s
`manifest["native_binary"]` block is argument-derived (`synthetic-goal-cleanup.json` records only
`fixture.runtime_kind`), so a consumer must not read that one block as witness-backed (§2 item 7,
plan §3.4).

### 10.9 Mutant rows (fix stage)

| # | Mutant | Pre-fix / base observation | Delivered observation | CI job and identity |
| --- | --- | --- | --- | --- |
| 1 | drop `return output` from `linuxV2NativeWitnessOutputDirectory` (the F01 revert) | extracted-function probe (§10.10): `FAIL … configured output directory ignored: got <temp>/artifact want <temp>/witness-output`; every witness write then resolves into the per-test temp directory the fixture cleanup deletes | the probe passes (configured directory, unset fallback, non-canonical refusal); in CI the export refuses `pi durability witness is unavailable: <file>` for every set | `linux-native-v2-acceptance` (non-root `runner`; the witness go-test runs as the fixture A account). Local probe observed both directions; the CI half waits on §10.11's run |
| 2 | revert the F04 message to the OpenCode literal | the branch never fires in a passing run (fail-closed refusal) | same | oracle gap: no run exercises the branch; the change is message-only, proven by the source trace (§10.4) |
| 3 | add `extra-durability-witness.json` to `shared`'s allowlist plus a `MANUAL_WITNESSES` source carrying a non-pi document | the `cc725d50` exporter (`<scratch>/old-mutant-with-source.py`, sha256 `469337a8140890de29c46b1dcff6654ef5a03b6e1b02dd8475d683db2e9d970f`) publishes it unchecked: `rc=0`, manifest lists the extra witness | delivered exporter (`<scratch>/new-mutant-with-source.py`, sha256 `884104d94a0126688b1d98aef5b4ca44a953590ad808b1752721412e3abc0870`): `rc=1`, `extra-durability-witness.json pi durability witness does not bind the staged pi executable`; the allowlist-only variant (`cc8247b36c57953dbe39df3f7c6b55f4b29fab1467da9f3c92d3379e0627de21`): `rc=1`, `pi durability witness is unavailable: extra-durability-witness.json` | `daemon` (the exporter suite, non-root `runner`) for the derivation; `linux-native-v2-acceptance` for the run path |
| 4 | drop `stale_leaf` from the acceptance script's validation list | — | `test_acceptance_case_set_lists_match_the_exporter_pi_durability_sets` FAILED, naming `'stale_leaf'`; restored → `OK` | `daemon` (non-root `runner`) |
| 5 | set the export default arm's `native_kind=pi` to `codex` | — | the same suite case FAILED; restored → `OK` | `daemon`; in the acceptance job the exporter would then refuse the set (`--native-kind codex requires --case-set codex_containment`) |
| 6 | tamper one file in the staged pi tree (`printf 'x' >> package.json` on a copy of the extraction) | release-tree digest `30ee2527343046840674893b3915a637e76ec2732dbf05551c49fc40c9eb9ab6` over the extraction of the pinned archive (archive sha `811c6f2c…` and executable sha `6a2ac1c5…` both reproduced); the untampered copy re-derives the same digest | tampered digest `d9faafd8259292dc70393a3d0b84af35c7dda9eb7256a8372c6575c438968656` ≠ the pin → the staging check `fail 'staged pi release tree differs from the pinned 1.5.2 archive'` (`linux-v2-native-acceptance.sh:459-460`); the live `linux_v2_fetch_pi` run (download + both digests + `--version`) exits 0 | `linux-native-v2-acceptance` (runner user; the staged tree is root-owned and read under `sudo`). Local probe observed; the CI half waits on §10.11's run |

The F08 derivation is a no-op on these bytes (the derived set equals the three explicit names); the
class is closed by construction.

### 10.10 Probes (verbatim, with sha256)

Host locations inside the pasted code read `<worktree>`/`<scratch>`/`<temp>`; the sha256 values
anchor the files as they ran.

**F01 extracted-function probe.** `harness_test.go` (sha256
`02ac98e9f5a5cf085575fd25911a8bff484f30f644ca8f19fa8c2e81264c5bbb`), `go.mod`
(`162da892adba48d5c2b90d296e491ca0c92dcae082369e4110fa259a78733b3b`), the pre-fix extraction
`extracted-prefix.go` (`a16d143d552326abdaf6c79373d3cc5d419e1c90a86ff669e69160e8bafca795`, from the
`412e67a4…` bytes) and the delivered extraction `extracted-fixed.go`
(`e57d656e4e89c129b1a5f70c23b0a8323fcc288655ae9a375450095def7cd599`, from the `550be655…` bytes).
The function text is extracted mechanically and the probe file is generated with a `package
probe`/imports header, so the tested body is the file's own bytes:

```bash
awk '/^func linuxV2NativeWitnessOutputDirectory/{f=1} f{print} f&&/^}$/{exit}' <file>
```

```go
package probe

import (
	"os"
	"path/filepath"
	"testing"
)

type linuxV2NativeFixture struct{ artifactDir string }

const linuxV2NativeWitnessOutputEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR"

func probeFixture(t *testing.T) (linuxV2NativeFixture, string) {
	t.Helper()
	base := t.TempDir()
	return linuxV2NativeFixture{artifactDir: filepath.Join(base, "artifact")}, filepath.Join(base, "witness-output")
}

func TestConfiguredDirectoryWins(t *testing.T) {
	fixture, env := probeFixture(t)
	t.Setenv(linuxV2NativeWitnessOutputEnvironment, env)
	if got := linuxV2NativeWitnessOutputDirectory(t, fixture); got != env {
		t.Fatalf("configured output directory ignored: got %s want %s", got, env)
	}
	os.Unsetenv(linuxV2NativeWitnessOutputEnvironment)
	if got := linuxV2NativeWitnessOutputDirectory(t, fixture); got != fixture.artifactDir {
		t.Fatalf("unset environment must fall back to artifactDir: got %s", got)
	}
}

func TestNonCanonicalDirectoryRefused(t *testing.T) {
	fixture, _ := probeFixture(t)
	t.Setenv(linuxV2NativeWitnessOutputEnvironment, "relative/witness-output")
	linuxV2NativeWitnessOutputDirectory(t, fixture)
}
```

Observed (pre-fix bytes): `TestConfiguredDirectoryWins` FAIL —
`configured output directory ignored: got <temp>/artifact want <temp>/witness-output`;
`TestNonCanonicalDirectoryRefused` FAIL with `… must be a canonical absolute path`. Observed
(delivered bytes): `TestConfiguredDirectoryWins` PASS (`ok probe 0.075s`);
`TestNonCanonicalDirectoryRefused` still FAILs with the same refusal (exit 1).

**F05 mutant probe.** `<scratch>/probe-f05.py` (sha256
`37ec67e59f5fb30c8c93fa5c61881fef77cdf67bc7b739de9310a97c7856cddd`); the mutation it applies, verbatim:

```python
ALLOW_MARKER = '        "unprovisioned-owner-refusal.json",\n'
SOURCE_ANCHOR = "MANUAL_WITNESSES = (\n"

def mutate(text, *, with_source):
    index = text.index("MANUAL_CASE_SET_FILES = {")
    head, tail = text[:index], text[index:]
    assert tail.count(ALLOW_MARKER) >= 1
    tail = tail.replace(ALLOW_MARKER, ALLOW_MARKER + f'        "{EXTRA_TARGET}",\n', 1)
    text = head + tail
    if with_source:
        anchor = text.index(SOURCE_ANCHOR) + len(SOURCE_ANCHOR)
        text = text[:anchor] + f'    ("{EXTRA_SOURCE}", "{EXTRA_TARGET}"),\n' + text[anchor:]
    return text
```

with `EXTRA_SOURCE = "extra-durability/TestExtra.json"`, `EXTRA_TARGET =
"extra-durability-witness.json"`; the assertion set, verbatim:

```python
    expectations = {
        "old-mutant-with-source.py": (0, EXTRA_TARGET),
        "new-mutant-with-source.py": (1, "does not bind the staged pi executable"),
        "new-mutant-allowlist-only.py": (1, "pi durability witness is unavailable"),
    }
```

Observed output (verbatim):

```
old-mutant-with-source.py: rc=0 stderr='' manifest_published=True extra_published=True named=True
new-mutant-with-source.py: rc=1 stderr='redacted evidence export failed: extra-durability-witness.json pi durability witness does not bind the staged pi executable' manifest_published=False extra_published=None named=True
new-mutant-allowlist-only.py: rc=1 stderr='redacted evidence export failed: pi durability witness is unavailable: extra-durability-witness.json' manifest_published=False extra_published=None named=True
PROBE_RESULT: MATCH
```

The probe's witness root holds the 33 documents the mutated `shared` allowlist sources (the pi
durability witnesses, the two OpenCode-vehicle documents, the cleanup vehicle document and the
extra non-pi document) and the three receipt documents; the pre-fix exporter publishes all of
them unchecked for the extra name, the delivered exporter refuses it.

**F06 mutants.** The two edits, verbatim: `…|ack_loss_launcher_bound|migration|…` (the validation
list with `stale_leaf` removed) and `*)` + `native_kind=codex` (the export default arm). Observed:
`FAILED (failures=1)` for each, naming `'stale_leaf'` for the first and the default-arm assertion
for the second; restored bytes → `Ran 1 test … OK` (twice).

**F10(ii) whole-tree digest.** The helper as it sits in `linux-v2-native-tools.sh`, verbatim:

```bash
linux_v2_pi_tree_digest() {
  local tree=$1 runner=${2:-}
  $runner bash -c 'cd "$1" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum' _ "$tree" |
    sha256sum | cut -d' ' -f1
}
```

Fetch-time check (`linux_v2_fetch_pi`): `test "$(linux_v2_pi_tree_digest "$pi_unpack/pi")" =
"$pi_tree_sha"` beside the executable and archive checks. Staging check
(`linux-v2-native-acceptance.sh:459-460`): `[[ "$(linux_v2_pi_tree_digest
"$rootfs/usr/local/lib/symmetry-pi" sudo)" == "${pi_tree_sha:-}" ]] || fail 'staged pi release
tree differs from the pinned 1.5.2 archive'`. The tamper command, verbatim: `cp -a unpack/pi
tampered && printf 'x' >> tampered/package.json`.

Observed: archive `811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5`, executable
`6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`, tree
`30ee2527343046840674893b3915a637e76ec2732dbf05551c49fc40c9eb9ab6` (208 files, no symlinks);
the helper returns the same digest with and without the `sudo` argument and for a plain copy, and
`d9faafd8259292dc70393a3d0b84af35c7dda9eb7256a8372c6575c438968656` for the tampered copy; the
independent Python re-derivation (`<scratch>/probe-f10-python.py`, sha256
`2f5c54ea8a67712148b89ac5b0ea72237ddcf47e456dbd1111eade60109a7bb6`) prints the same
`30ee2527…`; a live `linux_v2_fetch_pi` against the pinned URL exits 0 with the new check in
place.

**F10(i) hunk-scoped formatter check.** `<scratch>/probe-f10-hunkscope.py` (sha256
`e215d72d00bba26fd6c0a9556c3e923ee940fa243ff4150f00bc5bf700b3cd19`) parses the CI-parity
`mix format` diff views, reconstructs the formatter-rejected line numbers on both sides, aligns
base (`33ad88a9`) to the delivered file with `difflib`, and reports the goal-touched lines the
formatter rejects (`goal_lines & head_rejected`) plus those without a rejected base counterpart.
Observed output (verbatim):

```
base: rejected=110 removed=110 added=138 changed=248
head: rejected=106 removed=106 added=137 changed=243
goal-touched lines=278 rejected-and-goal-touched=43
goal-touched rejections without a rejected base counterpart=0 []
PROBE_RESULT: CLEAN
```

The check found one new-layout rejection at its first run (the goal's `cond` clause head `] ->
case_name`, head line 232, absent from the base's one-line form); the delivered bytes carry
Styler's own layout (`] ->` + the result on its own line, `provision-…-claims.exs:228-234`), so
every remaining goal-touched rejection is pre-existing (the `@cases` list indentation, the
`unless` → `if !(...)` bodies, the alias block). Logs: base
`04eb44e6c70ef6226e444efde566e9e8e17876f62b94e545ab79058bc1fed541`, delivered
`b70302de2564b90e260b02dd59e07b31925d196542e1c8a889e072dee3fdf2e3`; the first-run head log
(with the rejected `cond` head) is superseded.

### 10.11 Delivered-bytes binding (the carried all-12 run)

Run 13 (`45489336388`) executed the implement stage's bytes; the delivered bytes (§1) differ in
nine of the ten subject files, so the equality table between the run head and the delivery is:

| File | Run 13 bytes (implement stage) | Post-apply (`cc725d50`) | Delivered (§1) | Equal to run 13? |
| --- | --- | --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `143d78ea6b29e1cf8b76a16f0929634c11c932e396ab64120125dd36bf22961e` | `d1bca15691216c6ed703348c8d9d08736d35a26c4f4f674f364d2a9fea771ec1` | `92c795f6…` (§1) | no |
| `scripts/ci/linux-v2-native-tools.sh` | `185f4ff085f3a3a47e4f46d4727ded89fd78b14c0ba6c9ea822e413c42b5f1d2` | `185f4ff085f3a3a47e4f46d4727ded89fd78b14c0ba6c9ea822e413c42b5f1d2` | `b590f9e5…` (§1) | no |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `18001a824062f94e470fae3655b00cbb85dc866048148a57ce8afb437f8d06fa` | `18001a824062f94e470fae3655b00cbb85dc866048148a57ce8afb437f8d06fa` | `60bbaea2…` (§1) | no |
| `scripts/ci/write-linux-v2-owner-request.py` | `47cb012ba516dcf1f85fdd65b709d620ddbf77b7a4b398c4dfcbfeda831c03a9` | `47cb012ba516dcf1f85fdd65b709d620ddbf77b7a4b398c4dfcbfeda831c03a9` | `9b5ce15b…` (§1) | no |
| `scripts/ci/register-linux-v2-claim-machine.py` | `6fd8be109e4c4382186cef17f2c7dd9eb67a89c4890cd6cba6f4aa565d4372b7` | `6fd8be109e4c4382186cef17f2c7dd9eb67a89c4890cd6cba6f4aa565d4372b7` | `6fd8be10…` (§1) | yes |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `a21a22d97f09d4afec2fee7cc6b035684c78479ecb3f37dc33d5822e82794d25` | `d246887f8c9fae2f1a68eaee9bc8e38b6a5d72bd3ce537d0451d5493243ffecd` | `a9da8a94…` (§1) | no |
| `scripts/ci/test-linux-v2-evidence.py` | `afb9b7c2a4660645e2d1f6c24082d1c9c4d17f69641189f7007115e3c63bdcb9` | `1efb4850e5f49a2521b2dc125959a9887cab2d83b96e92f2b556de7d9bf730d2` | `6e707021…` (§1) | no |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `bddf80d702e7ef033104b338bb2d18c01b0fed981ff10acbda256837ff4a2f8e` | `412e67a46fd03b047d1012761801feeaefdcecea7d5908d31998066635617170` | `550be655…` (§1) | no |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `5b3bf2f3db0ba6352f1dca79a2bc845a20e88f56748b0636ac3657537cfd6d62` | `f71556913ff515843cb8555b66baa6811af862df06de2e7d8104181a75b89c51` | `5e3a4b29…` (§1) | no |
| `daemon/e2e/production_linux_v2_helper_exit_witness_test.go` | `25d5ee972acf2e11994751526ed3dab8308d3488fe380bc29221bd35b04f96f5` | `f11a77ad67a0eef7f14d52cece1592456fae71e548de2b435af2efa88b7492f1` | `f11a77ad…` (§1) | no |

The literal all-12 run on the delivered bytes is therefore **not yet dispatched**: this stage
forbids a push, and the conductor's DS ruling schedules it as the last code-affecting action
(`docs/TODO.md`, "The literal all-12 scratch run on the delivered bytes"). Its rows supersede §4e
for the delivered bytes; until then §4e's verdicts bind run 13's own bytes only, and the delivered
bytes' behavioral evidence is the local gates (§6), the probes above and the mutant rows (§10.9).

## 11. Round-1 review fixes (2026-11-25, PR #281 Sol + DS + Sec reconciled)

The Conductor's binding ruling of 2026-11-25, items 1-5 (item 6 is the carried all-12 run, §11.6),
on `findings/281-opus-review-1.md`, `findings/281-ds-audit-1.md` and `agent://Sec281`. Changed
files: `scripts/ci/rootless-compose-native-acceptance.sh`,
`scripts/ci/prepare-rootless-compose-owner-claim.py` (the ruling's owned-set amendment, this
migration only), `scripts/ci/provision-linux-v2-control-claims.exs`,
`scripts/ci/linux-v2-native-tools.sh`, the plan and this record. **No Go file changed**, so
`gofmt`/`go vet` are not applicable to this round. Supersessions: §10.11 is rewritten by §11.6;
§10.10's F10(ii) helper quote and pin, and §10.9-6's values, describe the fix-stage bytes and are
superseded by §11.3 for the delivered tree; the stop-line citations in §3 row 11, §4b, §4c, §4e
and §5 row 6 were corrected in place (`:531` → `:532`, `:627` → `:628`; §11.4).

### 11.1 Item 1 — the Compose caller migrates to the OpenCode-preserving refusal case

The red: `compose` job `138836746719` in run `45504671691` (PR head `0557536f`) aborted in
`Run rootless Compose native refusal witnesses` with `per-harness daemon profile is absent` — the
Compose fixture had kept the generic `stage_replay` writer case, which the retarget turned into the
pi-durability vehicle, while its config carries only `opencode-native`
(`rootless-compose-native-acceptance.sh:380-418`). The regression direction is proven by the base
run `45457018959` (`33ad88a9`): `compose` green.

The migration (mirrors `linux-v2-native-acceptance.sh:1559-1561`): the Compose caller now passes
`stage_replay_unprovisioned_refusal` and drops its explicit `native_release` argument (that case's
fixed hold is `native_release`, and the writer requires exactly 8 argv for it), and
`prepare-rootless-compose-owner-claim.py:306` names the same case in the run metadata the writer
cross-checks (`run.get("case") == case`). No compat branch was added to the writer. The request
document is unchanged: for that case the writer still writes `request_case = "stage_replay"`
(`STAGE_REPLAY_NATIVE_RELEASE_CASES`), so
`TestProductionLinuxV2UnprovisionedHostRefusal`'s contract assertion (`request.Case ==
"stage_replay"`, harness `opencode`, `Barrier.HoldAfter == "native_release"`,
`production_linux_v2_native_witness_test.go:727-731`) still holds byte-for-byte.

Caller census (`grep -rn "write-linux-v2-owner-request"` over `scripts/`, `daemon/`, `.github/`):

| Caller | Case argument | Disposition |
| --- | --- | --- |
| `linux-v2-native-acceptance.sh:1559-1561` | `stage_replay_unprovisioned_refusal` (OpenCode family) | unchanged (the OpenCode-preserving case) |
| `linux-v2-native-acceptance.sh:2084` in `prepare_owner_request` (17 call sites) | the fixture case name and hold: the retargeted sets pass `stage_replay` (pi-durability); `stage_replay_native_release_{codex,claude}` stay containment; the frozen OpenCode legs pass their own cases | unchanged (the case table retarget is this goal's core) |
| `rootless-compose-native-acceptance.sh:421-425` | `stage_replay` + `native_release` → `stage_replay_unprovisioned_refusal` | **migrated here**, §11.1 |

`stage_replay` case-name census in executable files: the writer's `CASES` holds the four
`stage_replay*` names and maps the three `STAGE_REPLAY_NATIVE_RELEASE_CASES` to the generic
`stage_replay` request case; the acceptance script passes the `stage_replay` request case from
`run_stage_replay_leg` (14 holds), the ack-loss wrapper (`drop_ack_after=<stage>`), the
stale-journal/helper/migration/stale-leaf/alternate-mount/pid-reuse negative wrappers, and the
helper-exit selector check (`:2666`); the Elixir fixture names `stage_replay_*` cases in `@cases`
(§4's 26-source-entry census; `stage_replay_unprovisioned_refusal` keeps `:opencode`); the e2e
binds `linuxV2NativeWitnessCaseMatches("stage_replay", harnessKind)`; `test-linux-v2-evidence.py`
uses `"case": "stage_replay"` in its document fixtures only (not the writer).

Local probe (`<scratch>/probe-writer.py`, sha256
`7400af11b4ce65b6b257a11d0e787a679ccbe4f17a963f3641283468d6263bd5`; stub Control claim endpoint,
Compose-shaped config, the writer under test at sha256
`9b5ce15b067bd22db50a800e005d3335e0f0bf94b9d91debf86e82e2aac44a42`). Positive (new argv):

```
positive rc: 0
positive stderr:
positive request: {"case": "stage_replay", "claim_preserved": true, "harness": "opencode", "hold_after": "native_release"}
positive output mode: 0o600
negative rc: 1
negative stderr: per-harness daemon profile is absent
PROBE_RESULT: MATCH
```

Negative (the old 9-argv call, same config): rc 1, `per-harness daemon profile is absent`, no
request document written. The full Compose job is green only on the PR's own CI event (the scratch
workflow carries its copy, §11.7).

### 11.2 Item 2 — the goal-create literal stops being a clone

The red: `contracts` job `138836746612` (run `45504671691`): `jscpd-changed refused:
scripts/ci/provision-linux-v2-control-claims.exs [471-488] duplicates
control/test/symmetry_control_web/controllers/portal_workflow_test.exs [1546-1563] (18 lines, 127
tokens) ... (new content)`. Fix: `provision_goal_admission!/5` now composes `execution_policy` by
`Map.merge` of two partial maps (disjoint key sets) instead of the single literal, so no ≥10-line
token run repeats the test's `goal_create_attrs`; the composed map is value-identical (probe
below). Nothing under `control/**` changed, and `.jscpd.json` / `JSCPD_LIMITS` are untouched
(`git diff 0557536f` shows no change to either).

Equality probe (`<scratch>/probe-goal-policy.exs`, sha256
`4e28aae1239435f800edd200cb73e532aefa16da298e5fc6fd6ad9b5e772597c`, elixir 1.19.6/OTP 28):
`MAP_EQUAL`, `keys=13`, the same 13 key/value pairs as the base literal (`max_parallel_tasks` 2,
`max_task_admissions` 2, `max_run_attempts_per_task` 2, budget fields nil/`soft`, no allowed
actions/resource ids).

Gate evidence: `python3 scripts/ci/jscpd-changed.py --base 3d994870 --head <round-1 commit>` and
the clean-tree runs with `--base 0557536f` / `--base 33ad88a9` (the PR push and merge-base event
shapes) — exit 0 each, no refused clone; the direct pair scan of the two files still reports the
test's four *internal* duplicates but none between the fixture and the test (§11.7).

### 11.3 Item 3 — the pi tree digest binds the whole normalized tree

Sec F1/F2 widened `linux_v2_pi_tree_digest` from a content-only file list to a sorted metadata
manifest (entry type, mode, owner, path, link target) plus file content hashes, verbatim:

```bash
linux_v2_pi_tree_digest() {
  local tree=$1 runner=${2:-}
  # Binds entry type, mode, ownership, path, link target and content, so an added link,
  # a chmod or a rewritten file cannot match the pinned normalized release tree.
  $runner bash -c '
    set -o pipefail
    cd "$1" || exit 1
    {
      find . -mindepth 1 -printf "%y %m %U:%G %P\t%l\0"
      find . -mindepth 1 -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | tr "\n" "\0"
    } | LC_ALL=C sort -z | sha256sum | cut -d" " -f1
  ' _ "$tree"
}
```

The pin is recomputed for the tree the guest actually receives — the acceptance staging
normalization (root:root; dirs 0755; files 0644 / u+x 0755) — `pi_tree_sha =
f5fc36f79aa55b685295b4c8c251ed4da485e565181c22dcb16a1676d2396a68` (supersedes
`30ee2527…`). `linux_v2_fetch_pi` no longer compares the raw extraction against the pin: raw
extraction ownership and modes are caller-dependent (the Compose fixture bind-mounts its
user-owned tree and re-chmods it, `rootless-compose-native-acceptance.sh:116-117`), so the pin is
checked where the tree is canonicalized — the staging site
(`linux-v2-native-acceptance.sh:459-460`, `sudo`, before first use), which is host-side, pre-boot,
and equivalent to in-guest verification because `systemd-nspawn --directory` shares the same
root-owned tree (this records F10(ii)'s verification honestly and amends its wording). The fetch
still pins the archive sha (`811c6f2c…`), the executable sha (`6a2ac1c5…`) and the `1.5.2` version
line; `export pi_tree_sha` keeps the global visible to the acceptance script (shellcheck delta 0).

Probe (`<scratch>/probe-pi-digest.sh`, sha256
`1b28e316b8231ed562d20bbccef2ceaeb4e279ec94a957d58b81654a7b937a79`, root, WSL Ubuntu-24.04):
the edited `linux_v2_fetch_pi` exits 0 (archive/executable/version checks); the staging
simulation yields 25 dirs 0755, 204 files 0644, 4 files 0755 (`pi`, `examples/extensions/doom-
overlay/doom/build.sh`, `…/doom.wasm`, `native/linux/prebuilds/linux-x64/linux-platform-x11.node`),
233 manifest records; the pin is deterministic across runs and with the `sudo` runner argument,
and the staging check compares equal:

```
fetch rc=0 pins: archive=811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5 tree=f5fc36f79aa55b685295b4c8c251ed4da485e565181c22dcb16a1676d2396a68
staging check: MATCH (f5fc36f79aa55b685295b4c8c251ed4da485e565181c22dcb16a1676d2396a68)
```

Mutants on the staged copy (each must change the digest; all do):

| Mutant | Digest | vs pin |
| --- | --- | --- |
| added symlink `evil-link -> /etc/passwd` | `c60e5af039bf3bd4cccd88dafa0411ad93000f3997132ec2b76c0f535bfe3efe` | changed |
| `chmod 0700 package.json` | `0e11ef78aca4a91c2bfe5fe46d14afd7928947a545829d601c08c3f5dffddc60` | changed |
| `chown 1001:1001 package.json` | `8cabe46a63946bdae2bc5e0edda0ec3dd328065189b4361f2141771150bf6319` | changed |
| append `x` to `package.json` (the F10(ii) tamper) | `54c78146839f7f1d32df134073586d189b63610571ce3420bfaf7928446ac301` | changed |

The content-only digest of the same staged tree is still `30ee2527…`, showing the new binding is
the metadata, not a content re-derivation.

### 11.4 Item 4 — record corrections

§10.11 is rewritten below (§11.6); the three off-by-one stop-line citations were corrected against
the run logs: `stale_journal_run_key` `:531` → `:532` (§3 row 11, §4b, §4c, §4e, §5 row 6) and
`stale_leaf` `:627` → `:628` (§4e) — verified by `gh run view 45485437534 --log-failed` and
`gh run view 45489336388 --log-failed` (the Go `file:line` is the first non-`t.Helper` frame). §4d
row 1 now states plainly that on the delivered bytes the mutant stops at the writer
(`durability ClaimResponse Work input is not a goal admission`) and cites the clone-intent stop
from the pre-admission run `45461719002`.

### 11.5 Item 5 — the plan artifact is owned

The Conductor's item-5 ratification makes `docs/plans/2026-11-24-rf-acc-pi-durability-matrices-plan.md`
a standard GDD artifact of the owned set; plan §7c records this round. No other `docs/**` file
enters the owned set.
