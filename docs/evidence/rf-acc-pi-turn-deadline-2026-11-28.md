# rf-acc-pi-turn-deadline — evidence record (stage 1 root cause; stage 2 fixture fix)

Goal `rf-acc-pi-turn-deadline` (contract under the GDD contracts root, ruled by the conductor
2026-11-27; dispatch gate #308, merged as `ac04f3e2`). Branch `refactor/rf-acc-pi-turn-deadline`,
dispatch base `ac04f3e2`. Failure of record: the credentialed dispatch `45974323458` (`main`
`d45e3298`), `shared` exit 1, gate row
`bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0, skipped=[], fail_events=2`.
Plan: `docs/plans/2026-11-28-rf-acc-pi-turn-deadline-plan.md`. Shape reference:
`docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md`.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `a971ab561de05de300142f7cffab23d5642de5da562dfb6fd5572eaf8696c0ec` | The `shared` case set stages the authenticated Pi legs' task text into the fixed B home's Pi agent directory and installs it under the existing private-file discipline; the same home writer sets `defaultThinkingLevel` to `off` for `shared` and keeps the pre-r1 `minimal` for every other Pi case set (round 2, R29). |
| `scripts/ci/test-linux-v2-evidence.py` | `e5118f435268adc275b68955ba5a306c2c1a60af42e3fc5ad3f14b2a52beb356` | `parse_case_arms` normalises each arm's head — surrounding whitespace, an optional leading `(` or trailing `)` and any quoting — before matching it, so an ordinary Bash spelling such as `(review_probe) ;;` is recognised rather than silently dropped. `authenticated_pi_task_failures` derives its case-set list from the acceptance script's own allowlist (no hand-copied list) and runs the fixture's own guard, home-writer call and install block over every derived set: `shared`'s exact payload and `off` level, every other Pi case set's absent file, pre-r1 `minimal` level and absent install row, and the two containment sets' explicit exemption (no Pi home, no install row). One committed derivation test binds the derived list to the script's dispatch block, to every case-set arm the script carries in any `case "$fixture_case_set" in` block and to the CI workflow matrix; seventeen staging mutants and five case-set-list mutants each assert their own exact failure list. |
| `docs/plans/2026-11-28-rf-acc-pi-turn-deadline-plan.md` | `bf561a3adb9eaa9ec0bbb699487bdf08f6710328689dc0cfa24d144a1aa8ee68` | This increment's plan (fix r1: the thinking level, the decoder row, the guest-scoped task-text statement; round 2: the R28–R34 wording and scope; round 3: the derived pin, the explicit containment exemption and the 420 s bound label). |
| `docs/TODO.md` | — (shared doc, cited by content) | Six `rf-acc-pi-turn-deadline: residuals` bullets: the credentialed re-dispatch, `stale_leaf`, the unobserved provider-free turns and holds, the decoder's `subject_hash` stop, the production carriers, and F1 (the unprojected provider `stopReason`). |

`docs/archive/CLOSED-TODO.md` is untouched: this increment closes no tracked item. No Go source is
changed, so no Go gate binds a delivered byte here (§8).

## 2. Stage 1 — the root cause, named

The authenticated Pi turn runs with no task text. The chain, every cite at the dispatch base:

1. A goal-admitted task stores the Goal *title* as its `goal`
   (`control/lib/symmetry_control/goals/admission.ex:418,573`), and the claim response copies the
   stored field verbatim (`control/lib/symmetry_control_web/protocol.ex:444-450`,
   `work.goal = task.goal`). The WorkItem route instead composes
   `execution_goal/1` = `title <> "\n\n" <> description`
   (`control/lib/symmetry_control/workspaces.ex:684,765-770`).
2. The witness hands the model that field and nothing else: `harness.TurnRequest{Goal:
   request.Claim.Work.Goal, Context: witness.nativeContext()}` and `nativeContext()` is a
   `version`/`subject`/`run_key` stub (`daemon/internal/app/linux_owner_witness_linux.go:3948-3955`).
   The Goal admission's `input` (which carries `limits`, `provider_scope` and the context snapshot
   id) never reaches the prompt, and Control's context envelope is not fetched on this path.
3. `#303` (`07be175e`) moved the three authenticated Pi legs onto the goal-admitted route
   (`scripts/ci/write-linux-v2-owner-request.py` `GOAL_ADMISSION_CASES`), so from that commit the
   model sees exactly `Linux v2 pi_provider_action owner witness` in `<symmetry_goal>`. The task
   row of the credentialed run shows the same value (`INSERT INTO "tasks"` goal column, run log
   5628). Before `#303` those legs were WorkItem-launched and their goal carried the description.
4. With the ratified placeholder the turn died at the provider in under a second (HTTP 401 →
   `stopReason:"error"`), so the missing text cost nothing. With a real credential it costs the
   whole bound.

Candidate discrimination the contract asked for:

| Candidate | Verdict |
| --- | --- |
| (a) the real turn legitimately needs more than the bound | refuted — a *specified* turn settles in seconds (§3), and the failing turn had no completion criterion at all |
| (b) the leg hangs after the turn | refuted — the stage markers stop at `native_turn_started`, and the RunJournal never reaches a stop receipt, a leaf removal or a clear; the App was still inside the turn, not inside stop/clear |
| (c) an agent/tool-call loop never terminates | it is an *unbounded exploration loop*, and its driver is the missing task text: the model probes the repository and the filesystem for what the run wants (guest row: 191 594 B of Pi bash output in under 420 s; §3.2 local row: 40 `bash` calls and no commit without the text, 3-16 with it) |

Named layer: **the fixture's task specification for the authenticated legs** — the goal-admitted
task text (`(1)`) as delivered through the witness's stub context (`(2)`). Not the harness, not the
stop path, and not the seven-minute bound.

## 3. Measurements

### 3.1 The guest baseline (the failure of record)

| Row (dispatch `45974323458`) | Value |
| --- | --- |
| Event gate | `bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0, skipped=[], fail_events=2` |
| Test stop | `production_linux_v2_native_witness_test.go:91: observe isolated production owner-witness RunKey: isolated production owner witness did not reach stop/clear before its bounded deadline`; `--- FAIL: TestProductionLinuxV2PiNativeProviderAction (424.04s)` |
| Journal projection | `{"local_state":"claimed","has_claim_grant":true,"has_clone_intent":true,"has_seed_receipt":true,"recovery_required":true}` |
| Stage markers reached | `reservation_durable … handoff_committed, lease_armed, checkout_ready, native_release, native_turn_started` plus the test's `release` (8 B); no `stop_receipt_durable`, `leaf_removed` or `journal_cleared` |
| Exported witness | `provider_action_succeeded: false`, `post_commit_marker_observed: false`, `leaf_absent_after_receipt: false`, `harness: pi`, `native_version: 1.5.2` |
| Fixture inventory | `fixed-b-home/.cache/pi-bash-0ca3729b4f6c4f05.log 191594`; `fixed-b-home/.cache/jiti/pi-provider-share-…mjs 5854` (the bridge extension was loaded); `fixed-b-home/.pi/agent/models-store.json 56560` |
| Control readback | `Control did not persist a succeeded provider_action_intents receipt for the exact Pi Claim/Run/resource/operation (case=pi_provider_action).` |

### 3.2 The local arms (same carrier, guest-shaped jail)

Carrier: the pinned `pi-linux-x64.tar.gz` 1.5.2 release (archive
`811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5`, executable
`6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`); the fixture's own
`settings.json`/`models.json` bytes; the fixture's generated provider-bridge extension
(`sha256:315a32c053c19256b876eae045338e3e1ab9a8f0bc70d5fe11d3f0268d6191cb`, the guest's staged
digest); argv `--provider openai --model deepseek-v4.1-flash-fast --mode rpc --no-extensions
--extension <path>` with `HOME` and the `SYMMETRY_PI_PROVIDER_BRIDGE_*` variables; the harness's
exact `buildTurnPrompt` bytes (title-only goal, the witness's subject/run-key context, the embedded
TaskResult schema); the subject clone at `d45e3298`; a local bridge stub answering one `succeeded`
action. The jail (`bwrap`) exposes only read-only system directories, an empty temporary
directory, the clone at
the guest's workspace path and the home at the guest's fixed-B-home path, with the host network
shared (the guest keeps the host netns).

| Arm | Staged profile | Wall to `agent_settled` | Turns | Tools | Commit | Final stop |
| --- | --- | --- | --- | --- | --- | --- |
| `c2` | the carrier, no staged append, `minimal` | 100.2 s | 27 | `bash` 40, `read` 4, `symmetry_resource_sync` 1 | none | `stop` |
| `c1` | delivered text, `minimal` | 9.5 s | 6 | `symmetry_resource_sync` 1, `write` 1, `bash` 3 | 1 | `stop` |
| `c3` | delivered text, `minimal` | 39.2 s | 22 | `symmetry_resource_sync` 1, `write` 1, `read` 3, `bash` 16 | 1 | `stop` |
| `min1` | delivered text, the pre-fix level `minimal` | 10.4 s | 4 | `symmetry_resource_sync` 1, `write` 1, `bash` 3 | 1 | `stop` |
| `off1` | delivered text, the delivered level `off` | 7.3 s | 5 | `symmetry_resource_sync` 1, `bash` 3 | 1 | `stop` |
| `sup1` | delivered text plus the exact contract values, `off` (not delivered) | 5.2 s | 3 | `symmetry_resource_sync` 1, `bash` 2 | 1 | `stop` |

The jail narrows what a model can explore, so its `c2` row (100.2 s) is a no-append timing
observation, not a proof about guest behaviour; the guest row of §3.1 is the binding one. The
observation bound is `linuxV2NativeTaskTimeout` = 420 s
(`daemon/e2e/production_linux_v2_native_witness_test.go:71`, applied at
`daemon/e2e/production_linux_v2_owner_cli_test.go:933`); the 424.04 s figure is that failed test's
own reported duration — a measurement artifact, four seconds above the bound — not the bound.
Every text-staged arm settled with one brokered `resource.sync` call, one commit and a normal stop.
The slowest instructed arm (39.2 s) is still more than ten times inside the 420 s window, so the
bound needs no amendment (`plan §2.2`). No arm's original final message was accepted by the
production decoder; §3.2's decoder table is the per-arm verdict.

The pre-fix arms ran with the staged Pi home's `defaultThinkingLevel` `minimal`; the delivered bytes
set it to `off` for the `shared` case set only, because the authenticated Pi legs must return a
final assistant message the production decoder accepts, and `DecodeAssistantTaskResult` requires
exactly one `text` block (`daemon/internal/harness/pi/rpc.go:901`; the one-block rule is `:916`; the
committed multi-block refusal is `adapter_test.go:657`). The level is staged per guest, not per leg:
one Pi staging home serves the whole `shared` guest, so the provider-free `pi-durability`
stage-replay and ack-loss legs inherit the same `off` level and the same staged append (§9), and no
code restricts the decoder to the authenticated legs. Every other Pi case set keeps the level it had
before r1 (round-2 scope, §4). The final assistant message of each captured arm and the decoder's
verdict on it, quoted as the whole `ErrInvalidTaskResult` string:

| Arm | Final message blocks | `DecodeAssistantTaskResult` on the captured message |
| --- | --- | --- |
| `c1`, `c3`, `min1` — delivered text, `minimal` | `[thinking, text]` | rejected: `pi task result must be one complete JSON object` |
| `c2` — the carrier with no staged append, `minimal` | `[thinking, text]` | rejected: the same two-block string |
| `off1` — the delivered writer's settings, model and text, `off` | `[text]` | the one-block gate passes; the whole rejection is `pi task result must be one complete JSON object: convert task-result generated DTO: subject_hash does not match canonical subject` (the model wrote the subject's `tree_digest` in place of the canonical hash) |
| `sup1` — delivered settings and model, extended task text, `off` (not delivered) | `[text]`, the text block carrying a prose preamble before the JSON | as delivered rejected: `pi task result must be one complete JSON object`; decoding the JSON payload alone, after the preamble, returns `candidate_completion` — a diagnostic on an extracted payload, not a delivered-arm result |

The delivered bytes remove the block-shape rejection. The remaining decoder rejection is the model's
inability to derive the canonical `subject_hash` from what the product sends: the `sup1` row is the
counterfactual that isolates it (with the contract values supplied, the same payload decodes), and
§9 records it as the next credential-row stop.

### 3.3 Per arm: reproduced byte for byte, re-measured, or only captured

The retained arms fall into two groups. The fix round's first captures — `c1`, `c2`, `c3`, logs
under the GDD state root's `scratch/pi-turn-deadline/runs/` — are **captured only**: their staged
homes were never compared with the delivered writer, and `c2` staged no append file at all (its
`exact`/no-text row, 40 `bash` calls and no commit). The fix-r1 re-measurement — `off1`, `min1`,
`sup1`, under the WSL scratch jail root, each run retaining `logs/final-message.json` and
`logs/summary.json`, with the jailed home left at `/opt/pi-run-<arm>/home/` — is **re-measured**.

For the re-measured group the delivered writer was re-run for the `shared` case set with the arm's
model and base URL, and the emitted home compared with the arm's jailed home. The exact rows
(`cmp`, re-executed on the delivered bytes, §8):

| Arm | `settings.json` | `models.json` | `APPEND_SYSTEM.md` |
| --- | --- | --- | --- |
| `off1` | equal (104 B, `off`) | equal (254 B) | equal (303 B) |
| `min1` | **differs** (108 B, `minimal`) | equal (254 B) | equal (303 B) |
| `sup1` | equal (104 B, `off`) | equal (254 B) | **differs** (648 B) |

So `off1` reproduced all three staged inputs byte for byte; `min1` reproduced the model config and
the task text but staged the pre-fix thinking level; `sup1` reproduced the settings and model but
staged the extended text with the contract values. No byte equality outside those rows is claimed,
and none of the three is a credentialed guest at the PR head.

## 4. The fix

`shared` is the only case set that runs authenticated Pi legs, so the fixture stages their task text
only there:

```text
Use the real symmetry_resource_sync tool for the configured GitHub repository, then create and commit one small file. Do not publish.

This leg is fixed-scope: do not explore the repository or run its test suites or gates, and return the required final TaskResult JSON object as your only final answer.
```

The first sentence is the Control fixture's own admitted description for `pi_provider_action`
(`scripts/ci/provision-linux-v2-control-claims.exs:42-43`), verbatim. One B home serves every leg
of a guest and `shared` admits all three authenticated legs, so the two crash legs receive this
text too, although their own admitted descriptions differ in wording (`:46-47` and `:48-49` carry
the detached-child helper and daemon crash witnesses); the placeholder credential registers both
`not_run` (§7), so the consequence of that wording mismatch is unobserved until a credentialed
cadence run. Pi loads `<agent-dir>/APPEND_SYSTEM.md` as system-prompt instructions
(documented in the pinned release and verified live: the system message's `addendum` section carries
the file), so `$B_HOME/.pi/agent/APPEND_SYSTEM.md` reaches the model without touching the request,
the profile arguments, the tools or any assertion. The file joins `b_native_credential_files`, so
the existing install and validation rows give it the same B-owned 0600 singly-linked discipline as
the other staged Pi files, and the containment kinds never see it (the text is empty for them).

The same home writer sets the staged Pi profile's `defaultThinkingLevel` to `off` for the `shared`
case set and keeps the pre-fix `minimal` for every other Pi case set (fix r1, RP-01; round 2, R29):
the pinned release's own `docs/settings.md` lists `off` among the level values, and the re-measured
arms (§3.2) show the resulting final assistant message without the `thinking` block. The census
behind that scope: the acceptance script's allowlist names 15 case sets
(`scripts/ci/linux-v2-native-acceptance.sh:33`) and the writer stages a Pi home for the 13 of them
that are neither `codex_containment` nor `claude_containment` — `shared`,
`stage_replay_create_started`, `stage_replay_leaf_bound`, `stage_replay_launcher_bound`,
`ack_loss_launcher_bound`, `stale_leaf`, `migration`, `alternate_mount_refusal`,
`stale_journal_run_key`, `stale_journal_run_key_legacy`, `stale_journal_fence`,
`stale_journal_subject`, `synthetic_goal_cleanup`. `off` reaches `shared` alone; the other twelve
keep `minimal`, their pre-r1 value. The second B home, `CLONE_TEST_B_HOME`
(`scripts/ci/linux-v2-native-acceptance.sh:538`), serves the clone/launcher scratch legs and stages
no Pi home at all (`:721,989-993`), so the Pi staging home stays the single Pi staging site. One
writer serves every pi case set; the placeholder-credential legs never reach the decoder, so §7's
Bind rows are unchanged.

Recorded consequence, not hidden: the model is *told* the task by the fixture rather than inferring
it from the Control goal. That is the relationship the WorkItem route had before `#303` and the one
the product has when Control's context envelope is fetched; the fixture's own description is the
text, so no leg asserts anything the fixture did not already admit.

## 5. Red/green controls

- No-text control (§3.2 `c2`): 40 `bash` calls, no commit. Guest control (§3.1): the bound elapsed,
  no `resource.sync` receipt. The fix's green rows: two instructed runs, one receipt and one commit
  each. Decoder rows: the pre-fix profile's captured messages are rejected on the block shape
  (`[thinking, text]`), the delivered profile's message passes that gate, and the
  supplied-contract-values payload decodes to `candidate_completion` (§3.2).
- Mutation rows over `authenticated_pi_task_failures` (seventeen staging mutations and five
  case-set-list mutations, each asserted against the exact failure list it must produce; the
  pristine pair returns `[]`). The pin derives its case-set list from the acceptance script's own
  allowlist, so every mutant runs it over every allowlisted set and a leak into any of them is a
  row. The rows run in the `daemon` job's Check Linux v2 fixture contracts step as the non-root
  runner user:

In the table, `<set>` ranges over the twelve non-containment case sets other than `shared`, in the
derived allowlist order: `stage_replay_create_started`, `stage_replay_leaf_bound`,
`stage_replay_launcher_bound`, `ack_loss_launcher_bound`, `stale_leaf`, `migration`,
`alternate_mount_refusal`, `stale_journal_run_key`, `stale_journal_run_key_legacy`,
`stale_journal_fence`, `stale_journal_subject`, `synthetic_goal_cleanup`. The shorthands used below
are verbatim rows:

- `<set> TEXT` = `the <set> case set stages the authenticated Pi task text`
- `<set> LEVEL` = `the <set> case set stages the authenticated thinking level 'off'`
- `<set> INSTALL` = `the <set> case set's fixed B home install list stages the authenticated Pi task text`
- `CONT` = `the <codex|claude>_containment case set's fixed B home install list stages the authenticated Pi task text without a Pi home`
- `CONT-HOME` = `the codex_containment case set is not the containment fixture: it staged a Pi home`
- `SHARED-TEXT` = `the shared case set does not stage the exact authenticated Pi task text`
- `SHARED-OMIT` = `the shared case set's fixed B home install list omits the staged task text`
- `PROVIDER-ROW` = `the Control claim fixture no longer carries the pi_provider_action description`
- `RC` = `the fixture staging script for <case set> exited 1`
- `UNRUN` = `a case-set arm outside the dispatch block admits a set the dispatcher does not run`

The multi-row `<set>` expectations are computed inside the test from the same two derived lists the
pin uses; the derivation itself carries its own committed test and its five mutants below, so a
drifted or truncated list reddens there rather than silently shortening a mutant's expectation.

| Mutation | Exact failure list |
| --- | --- |
| the `shared` text guard made unconditional (`if [[ "$fixture_case_set" == shared ]]` → `if true`) | `CONT` (both containment sets, allowlist order), then `<set> TEXT` followed by `<set> INSTALL` for each `<set>` in order: 26 rows |
| the home writer's `if authenticated_pi_task_text:` replaced by `if True:` | the twelve `<set> TEXT` rows in order, nothing else |
| the `shared` guard extended to `migration` | `the migration case set stages the authenticated Pi task text`, then `the migration case set's fixed B home install list stages the authenticated Pi task text` |
| the `shared` guard extended to `synthetic_goal_cleanup`, a set no earlier pin ran | `the synthetic_goal_cleanup case set stages the authenticated Pi task text`, then `the synthetic_goal_cleanup case set's fixed B home install list stages the authenticated Pi task text` |
| the `shared` guard extended to `codex_containment` | `CONT` for `codex_containment` alone: the round-2 leak that left the previous pin at `[]` |
| the fixture home writer's containment mapping loses `codex-containment` | `CONT-HOME` |
| the staged thinking level made unconditional (`… = "off" if fixture_identity_suffix == "shared" else "minimal"` → `= "off"`) | the twelve `<set> LEVEL` rows in order |
| the staged thinking level raised for `shared` (`… = "off" if … else "minimal"` → `"minimal"`) | `the staged pi home no longer sets the thinking level off: 'minimal'` |
| the staged text reworded (`for the configured GitHub repository` → `for the configured resource`) | `SHARED-TEXT` |
| the fixed-scope paragraph deleted from the staged text | `SHARED-TEXT` |
| the invocation's last argument (`"$authenticated_pi_task_text"`) blanked to `""` | `SHARED-TEXT` |
| the fixture home writer's `if authenticated_pi_task_text:` replaced by `if False:` | `SHARED-TEXT` |
| the install target renamed to `APPEND_SYSTEM.txt` | `SHARED-OMIT` |
| the install row removed from the acceptance script | `SHARED-OMIT` |
| the Control fixture's description reworded | `SHARED-TEXT` |
| the Control fixture's `pi_provider_action` row renamed | `PROVIDER-ROW` |
| the invocation's last argument dropped (argument count changed) | the fifteen `RC` rows in allowlist order |

The five case-set-list mutants assert the derivation directly:

| Mutation | Exact failure list |
| --- | --- |
| a name appended to the script's allowlist alternation arm (`|synthetic_goal_cleanup) ;;` → `|synthetic_goal_cleanup|review_probe) ;;`) | `the case-set allowlist and the script's own dispatch arms diverged`, then `UNRUN`, then `the case-set allowlist and the workflow case-set matrix diverged` |
| a standalone `  review_probe) ;;` arm inserted into the script's validation block before its rejecting `*)` arm | `UNRUN` alone |
| a parenthesised `  (review_probe) ;;` arm inserted into the script's validation block before its rejecting `*)` arm | `UNRUN` alone |
| a name added to the CI workflow's `case_sets` matrix | `the case-set allowlist and the workflow case-set matrix diverged` |
| the `native_containment_kind` arm removed for `codex_containment` | `the containment case sets and the writer's containment kinds diverged` |

## 6. Which userland each property needs

The new pin and its mutants are bash/python text-and-staging rows: they execute the fixture's own
guard, the fixture home writer's real shell call (its argument list included) and the install-list
block verbatim over every case set the acceptance script allowlists, and assert the staged file
bytes against the Control fixture's description plus the fixed-scope paragraph. The coverage list
is not written down in the test: `acceptance_case_set_allowlist` reads the script's own allowlist
line, `acceptance_case_set_derivation_failures` binds that derived tuple to the script's dispatch
block (the one `case "$fixture_case_set" in` block that runs the native owner witnesses), to the
union of the arms of every such block, and to the CI workflow's `case_sets` matrix, and
`acceptance_containment_case_sets` reads the
script's `native_containment_kind` declaration, cross-checked against the fixture home writer's own
containment mapping. One uniform per-set assertion then runs over the derived list: `shared` must
stage the exact text at level `off` and carry the file in the fixed B home install list; every other
Pi case set must stage no `APPEND_SYSTEM.md`, keep the pre-r1 `minimal` level and keep the file out
of that install list — the empty-input negative restored in round 2 (R28); and the two containment
case sets are exempted explicitly, by asserting that they staged no Pi home at all *and* that the
install list does not carry the file, so a task-text guard leaked into a containment set reddens
(round 3, R37; §5's `CONT` and `CONT-HOME` rows). No permission, ACL, ownership or
environment assertion is added — the new rows are pure text parsing and subprocess staging — so the
runner-mirroring requirement (lesson 160) is not engaged; the rows run in the `daemon` job's Check
Linux v2 fixture contracts step on the runner image, and the local runs above are recorded as this
host's oracle (WSL `Ubuntu-24.04`, `VERSION_CODENAME=noble`, python 3.12.3). The guest-side effect
of the staged file is the credentialed dispatch's row.

## 7. Does the conductor's Bind execute it?

The placeholder Bind runs the `shared` case set alone, with the ratified placeholder credential,
and one gated leg, `pi_provider_action`, through `run_credential_gated_pi_provider_leg`
(`scripts/ci/linux-v2-native-acceptance.sh:2613-2631`, the case name hard-coded at `:2615`); the two
crash legs are only registered by `record_credential_gated_leg` (`:3303-3306`, declaration at
`:2589`). It does not execute the pin: the pin is a local Python row that runs in the `daemon` job
on the PR head (§8). What the Bind does establish, on the head it gated, is where `shared` stops.

Two scratch Binds ran; neither is the protected credentialed dispatch, and neither produced a green
`shared` row:

1. Round 1 ([run 46046284991](https://github.com/wxxb789/symmetry/actions/runs/46046284991)) on PR
   head `dd9da52f` exited 1 with **22** green event-gate rows and one red:
   `TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable` was `pass:0`, reported as
   `pre-create abort tombstone reports a target or leaf creation` (the section below quotes it in
   full); that subtest was the first non-gated failure.
2. Round 2 ([run 46049816624](https://github.com/wxxb789/symmetry/actions/runs/46049816624)) on the
   merge head `f6dfea80` is summarised below, not quoted verbatim: both Bind sections condense
   their receipt files, so a later change to a receipt file is not visible here.
   `shared` exits 1 again, but the
   round-1 red is gone: **24** green event-gate rows, including
   `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`
   (log line 3687), because `3d377582` (#309) is now an ancestor of the head — exactly the row
   round 1 lacked. The next replay leg, `helper_bound`, ran and was refused *before* reaching its
   hold (line 4265): `pre-existing RunJournal
   cf7ad503-93e5-4b19-9f85-5dec44c855f3/1 is not physically released and safe for a fresh run:
   prior RunJournal may still own or require recovery of a process or leaf`; the first non-gated
   failure is that `helper_bound` row (line 4261). §7 therefore no longer predicts Bind rows: the
   round-2 section is the observed truth for that head, and its next stop is a Go replay refusal,
   not the authenticated turn.
3. The authenticated Pi leg's progress and first failure were **not observed** in either run: the
   placeholder reached checkout readiness and completed no provider action, the two gated crash legs
   did not run, and the run declares `credential-gated: pi_provider_action
   (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider
   credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.`
   (line 3109). The `subject_hash` rejection of §3.2 and §9 is a local `off1` capture observation
   and is not a protected-run stop.
4. No `shared/manifest.json` was published in either run — the export fails on the missing ack-loss
   witness — so the Bind has no manifest status to report, and §9's credential-row outcomes stay
   unproven.

This round changes only the Python pin, the plan, the record and `docs/TODO.md`: the acceptance
script and every Go byte stay as they were. That is not outcome invariance. The fixture hashes the
full committed tree (`scripts/ci/source_subject.py:127-141,209-230`, `index_mode_digest`), passes
that commit and digest into Control provisioning and writes it into the exported credential-gated
declaration (`scripts/ci/linux-v2-native-acceptance.sh:26-29,1094-1102,2583-2610`,
`write_credential_gated_declaration`), so this round's changed docs and test file are inputs to that
digest. Run 46049816624 therefore establishes the **observed** behaviour at its own hashed subject,
the scratch commit `30f450a584f405409d3a4b3e71c13a26e590dc40`, whose sole parent is the PR head
`f6dfea80`; that receipt is observed for that subject only, and the run is neither outcome
invariance nor evidence equivalence for this head, which needs its own Bind. The authenticated
outcome remains the owner's credentialed cadence.

## 8. Gates

Every command below ran on the delivered bytes of §1. The code-scoped rows ran in the PR worktree
(WSL `Ubuntu-24.04`, `VERSION_CODENAME=noble`, python 3.12.3), the `node`-backed duplication gate on
Windows (node v26.13.1), and the doc-scoped rows in a scratch clone whose parent commit is exactly
`3d377582` and whose child commit carries the §1 bytes, so that clone's `HEAD~1..HEAD` is this
increment's own five-path delta. The CI home of the two Python suites is the `daemon`
job's Check Linux v2 fixture contracts step (it runs them from the `daemon` directory on the runner
image); the CI duplicates gate uses `HEAD^1` as its base, and the PR run's `contracts` job checked
out the synthetic merge `e81ed10c4b9000de1997e0a632fb5441bcc1b86b`, whose first parent is
`3d377582`, logging five changed files (job 140689285159 of run 46053161751) — so the PR gate
measures the five-path delta from the synthetic merge's main parent, not the four-path round-3 topic
increment `f6dfea80..HEAD`.

Range provenance, corrected in round 3: this increment's delta against the merged main tip is
`3d377582..HEAD`, the five paths of §1 — the acceptance script and the test file (the fix) plus the
plan, the record and `docs/TODO.md`. `ac04f3e2..HEAD` is *not* that delta on this head: it carries
the same five paths plus #309's merged paths (`daemon/e2e` ×2, `docs/archive/CLOSED-TODO.md` and that
goal's two documents) — ten paths in total, none of which any row below binds. The archived Bind
receipts were gated on their own heads, named in their own sections.

| Command (working directory) | Exit | Result |
| --- | --- | --- |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` (repo root, WSL) | 0 | no output |
| `python3 -B scripts/ci/test-linux-v2-evidence.py` (repo root, WSL) | 0 | `Ran 77 tests … OK` in 66.1 s (75 before round 3 added the derived-coverage test and the case-set-list mutation test; rounds 4 and 5 add no test method — they re-point the derivation and extend its mutant list) |
| `python3 -B scripts/ci/test-linux-v2-owner-request-keys.py` (repo root, WSL) | 0 | `Ran 10 tests … OK` in 0.3 s |
| `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only <plan, record, TODO>` (repo root) | 0 / 0 / 0 | all three changed docs pass root-only |
| `python3 scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base <clone base commit> --head HEAD` | 0 | `1 changed record(s) pass the root-only check` |
| `comment-cap.py <scratch clone> <clone base commit>` | 0 | no output: no added comment block over 2 lines (the pin's one new comment is a 2-line why block) |
| `python3 scripts/ci/jscpd-changed.py --repo <scratch clone> --base <clone base commit> --head HEAD` (Windows, node v26.13.1) | 0 | `5 changed file(s), 1805 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new` |
| pinned `ruff` 0.17.5 (`uv tool run ruff@0.17.5`) `check scripts/ci` and `format --check scripts/ci`, in the delivered tree and in the `3d377582` tree (Windows) | — | the same finding set at both revisions modulo line numbers (each finding's path, rule and offending line text matched): `30` findings with `25` E501 rows, none of them on a line this range adds, and `8` files-to-reformat; the advisory CI step is `continue-on-error` |
| the delivered `shared` home writer re-run against each retained arm's jailed home (`cmp`, WSL, §3.3) | 0 for every `equal` cell | round-2 row, still binding: the acceptance script is byte-identical to the round-2 bytes (`a971ab56…`); `off1` equal/equal/equal, `min1` settings differ by design (pre-fix level), `sup1` append differs by design (extended text) |
| `go test -mod=mod -count=1 -run 'TestScratchRP01' ./internal/harness/pi` in a scratch `daemon` copy whose `internal/harness/pi` and `internal/protocol` trees are byte-identical to the delivered ones (WSL, Go 1.27.0) | 0 | round-2 row, still binding: `ok … 0.203s`, the six captured messages reject or decode exactly as §3.2's table records, including `off1`'s full string |

The Go rows, declared once: `git diff --stat 3d377582..HEAD` lists only the five paths of §1 and
`git diff 3d377582..HEAD -- daemon` is empty, so no Go source is changed, no `gofmt`/`go vet` or Go
test row binds a delivered byte, and the `daemon` module's own suites were not re-run for this
range. The apply stage's `go test ./...` on the same Go bytes (Windows exit 0; WSL exit 1 with only
`internal/platform`'s uid=0 refusal rows) and `git diff ac04f3e2..HEAD -- daemon/internal/platform`
being empty are round-2 receipts for bytes this round does not touch, so those refusal rows are
pre-existing byte-identical rows, not a regression of this increment.

Fix r1 receipt: the code-scoped rows above (`bash -n`, the two Python suites) and the doc-scoped
rows (both sanitizers, `check-record-cleanliness.py`, `comment-cap.py`, `jscpd-changed.py`) ran on
the round-1 final bytes of §1 — the test file and the acceptance script after the RP-01/RP-03 rework and
the plan/record/TODO after their wording fixes — so every implement- and apply-stage receipt for
those files is superseded by these bytes.

Fix r3 receipt: the code-scoped rows (`bash -n`, the two Python suites) and the
doc-scoped rows (both sanitizers, `check-record-cleanliness.py`, `comment-cap.py`,
`jscpd-changed.py`) re-ran on the round-3 §1 bytes — the test file with the derived pin, the
explicit containment exemption and its twenty mutants, and the plan/record/TODO with the round-3
corrections (the gate range, the prompt-exposure distinction, the 420 s bound label and the F1
residual) — so the fix-r2 receipt is superseded for those four paths. It still binds the acceptance
script, which this round does not touch, and the two measurement rows are the round-2 ones named in
the table. Round 3 re-measured no arm.

Fix r4 receipt (round 4): the code-scoped rows (`bash -n`, the two Python suites) and the
doc-scoped rows (both sanitizers, `check-record-cleanliness.py`, `comment-cap.py`,
`jscpd-changed.py`) re-ran on the §1 bytes of this table — the test file with the anchored dispatch
block, the union-of-arms derivation and its four case-set-list mutants, and the record with the
round-4 corrections (the subject-scoped Bind claim, the synthetic-merge gate range and the two
provenance qualifiers) — so the fix-r3 receipt is superseded for the test file and the record. The
acceptance script is byte-identical to the round-2 bytes (`a971ab56…`), the plan is unchanged from
round 3 (`bf561a3adb9e…`), and the two measurement rows stay the round-2 ones named in the table.
This round re-measured no arm.

Fix r5 receipt (this round): the same code- and doc-scoped rows re-ran on the round-5 §1 bytes — the
test file with the normalised case-arm spelling and its fifth case-set-list mutant, and the record
with the round-5 corrections (the scratch-commit subject, the two-comparison archival claim and the
removal of the stale line counts) — so the fix-r4 receipt is superseded for the test file and the
record. This round re-measured no arm.

CI observation, not a gate of this increment: on the reviewed head `dd9da52f` the `compose` job
(`.github/workflows/ci.yml:1793-1812`) failed in 41 s inside its smoke step, at the Control image's
signing-key import — `gpg: no valid OpenPGP data found.` from the
`curl … microsoft.asc | gpg --dearmor` line (`docker/control.Dockerfile:46-47`). That Dockerfile is
unchanged by this diff, and the sibling PR #309's `compose` job passed six minutes earlier on the
same workflow, so the failure is an infrastructure flake and not attributable to this change. The
conductor attempted a rerun on the same head; it was refused while the run still had `control` in
progress. The later run on `f6dfea80` (`46049272397`) shows `compose` green in 5m52s — observed in
the round-2 adversarial audit's `gh pr checks` snapshot, not re-run by this round — so the flake did
not recur there. No `known-flakes` row is added: rule 63 needs a second PR to establish the pattern.

## 9. Residual risks and known limits

- **The next verbatim stops are named, not fixed.** In the local `shared`-shaped arms the first
  authenticated leg now fails at the production decoder rather than at the bound — a local
  reproduction on the captured messages, not a proved credentialed guest stop: the model writes the
  subject's `tree_digest` where the canonical `subject_hash` is required, and neither the goal title nor the
  witness's `nativeContext()` (`daemon/internal/app/linux_owner_witness_linux.go:3948`) carries a
  hash the model can copy (§3.2: with the exact subject and hash supplied, the same payload
  decodes; the same canonical-hash burden applies to every native harness's TaskResult, not only
  pi). The fix has two carriers — the fixture's staged task text (this increment's mechanism, values
  computed from the resolved subject) or a product-side carrier in the witness context — and needs a
  conductor ruling (`DECISION_NEEDED`); `docs/TODO.md` records it. `stale_leaf` (red in run
  `45974323458`: `wait for exact durable stop receipt: owner-witness command did not reach its
  requested durable stage`, log lines 9749 and 10031, command live, journal at `claimed`) shares
  the credential-staging layer: the fixture stages the real provider credential into a
  `pi-durability` vehicle whose case was designed around an instant provider failure. This
  increment's staged append does **not** reach that vehicle — the append is staged and installed for
  `shared` alone, and the round-3 pin asserts both its absence and its absence from the fixed B home
  install list for every other case set (§5, §6) — so what the real credential does there is
  unchanged by this increment and still unobserved: neither a turn nor a post-turn hold has been
  reached in a credentialed run. The Pi staging home serves a whole guest (`CLONE_TEST_B_HOME`
  serves only the clone/launcher scratch legs and stages no Pi home), so the fix is per case set
  (stage the ratified placeholder for the provider-free sets); it is a separate goal and is recorded
  in `docs/TODO.md`.
- **The model prefixed prose once.** In the `sup1` arm the final text block began with a prose
  sentence before the JSON; the decoder accepts only a whole-text JSON object, so that arm would
  fail as delivered even with the contract values supplied (§3.2, the `sup1` row). The
  other captured messages were pure JSON; the model-side risk is unmeasured and belongs to the
  credentialed row.
- The `shared` set's own provider-free `pi-durability` stage-replay and ack-loss legs receive the
  staged append text and the `off` thinking level too (one Pi staging home serves the whole guest)
  although their profile is `provider_access=False` and their cases were designed around an instant
  provider failure. The
  delivered script proves those legs would be *exposed* to the staged prompt text on a launch that
  reaches a turn; it does not show that any such turn ran. Their post-turn holds
  (`stop_receipt_durable`, `leaf_removed`, `journal_cleared`, the 30-second stop-receipt barrier)
  have never been reached in a credentialed run — the `shared` arm aborted at the first
  authenticated leg — so whether the turns or the holds are reached on a real provider turn is
  unobserved here and is the credential re-dispatch's third observation.
- The local arms are a jail, not the guest: no cgroup containment, no A/B ownership, no
  post-commit hook (the guest's hook can block its commit for up to 20 s) and no systemd. They
  support the *direction and size* of the effect; the credentialed dispatch remains the acceptance
  row.
- Production-side carriers for the same text remain available and are recorded for
  `DECISION_NEEDED`: the goal-admission goal composition (`admission.ex`) and the witness's stub
  context (`native_context()`), either of which would carry the text for every native harness
  instead of the fixture's Pi legs only.
- The staged text tells the model to skip repository exploration, which is the fixture's own scope
  statement for a leg whose claims are the brokered provider action, the native containment and the
  stop/clear path. It is not a claim that the model would have found the task unaided.

## 10. Retained artifacts

- Plan and record; `docs/TODO.md` entries for the six open items.
- The measurement harness and its raw arm logs (`c1`, `c2`, `c3`: `summary.json`,
  `bridge-requests.jsonl`, `pi.stdout.jsonl`) under the GDD state root's `scratch/pi-turn-deadline/`;
  the same harness's scripts (`run-turn3.sh`, `run-turn4.sh`, `bridge-stub.py`, the generated
  bridge extension) — retained as historical material only, see §10.1.
- The fix round's re-measured arms: `off1`, `min1`, `sup1`, each keeping its `logs/`
  `final-message.json` and `summary.json` under the WSL scratch jail root, with the arm's jailed
  home left at `/opt/pi-run-<arm>/home/`. The throwaway decoder check is a scratch copy of the
  daemon module holding one `_test.go` that calls `DecodeAssistantTaskResult` on those captured
  messages, never placed in the repository tree.
- The failure-of-record material: the credentialed run log and evidence archive of `45974323458`
  under the GDD state root's `scratch/cred-45974323458/`.

### 10.1 The retained measurement harness is unsafe to run as written (R31)

`run-turn3.sh` and `run-turn4.sh` are retained as the historical reproduction of §3.2's arms, not as
a probe a reader may run; both now open with a `# HISTORICAL — DO NOT RUN AS WRITTEN` banner naming
this section, so a reader who finds one before the record sees the label too. As written they read
the operator's configured provider key out of
`models.yml`, pass it as a **process argument** (`python3 - "$home" "$key" "$arm" "$level"`), write
`auth.json` before `chmod 0600` — a window at the umask default — and leave the credential-bearing
home behind under `/opt/pi-run-<tag>/home/`. An argument is readable by any same-user process
through `/proc/<pid>/cmdline`.

The corrected pattern for any re-measurement:

1. a dedicated, short-lived measurement key, never the operator's configured key;
2. the key supplied only through a private file or a file descriptor — never argv, never a logged
   command's environment;
3. the home created `0700` and `auth.json` created `0600` from the start, with no write-then-`chmod`
   window;
4. `auth.json` removed on exit, and the home directory removed with it.

The key that produced the retained arms must be treated as **exposed** and rotated by its owner.

Conductor remediation, 2026-11-28: the credential-bearing `auth.json` file in each of the three
retained arms' jail homes (`/opt/pi-run-<arm>/home/.pi/agent/` for `<arm>` in `off1`, `min1` and
`sup1`) was removed, and every remaining file in those homes was verified to contain zero
occurrences of the key.

## Bind (round 1)

**ROUND 1 / SCRATCH / ACT SUPERSEDED — not the protected credentialed dispatch.** [Run 46046284991](https://github.com/wxxb789/symmetry/actions/runs/46046284991), attempt 1, ran only `shared` with the ratified placeholder credential. The head-filtered Actions census was `total_count=1`. The [evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46046284991/artifacts/15222292091) is 4,449 bytes; downloaded SHA-256 `0c70c0d6f54aa9cf045ca1f4cedb630677c2c511314748b05061b0789dc318b0` matches the Actions digest.

**§1 cross-reference correction (round 3, requalified in rounds 4 and 5).** The "§1 bytes" in this receipt
are the round-1 `dd9da52f` blobs, not the §1 table above, which carries the round-5 bytes. At
`dd9da52f` the acceptance script's SHA-256 was
`0ab1545f4081b73b2114af350e8c2b5a16e2a2dd3e280f1c8480e1d17c2751b6` and the test file's was
`9335f40c2890…`, so no blob of the current §1 table matches that head. Round 3 compared the
**round-2 version** of the §1 table against the two historical commits in two separate steps. That
version's own three blobs all match those two heads: the acceptance script `a971ab561de05…`, the
test file `0b5f3dcd1890…` and the plan `5cb192855e63…` are identical in `e754b6da` and `f6dfea80`.
The §1 table above compared against the same two heads leaves only the acceptance script
(`a971ab561de05…`) equal, because the test file is now `e5118f435268…` and the plan
`bf561a3adb9e…`. The PR-head column below means the tree
the run commit descends from, and the run commit `a75ad21c` adds only the two scratch files to it,
exactly as the third table column records.

| Input | PR #310 `dd9da52f00e8d44cbc39a1590b307220a8567906` | Run `a75ad21c42145b083660501f7a4f350eed8e211f` |
| --- | --- | --- |
| Files already present at the PR head | Delivered tree, including the round-1 §1 bytes (`dd9da52f`) | Byte-identical: the PR head is the sole parent; the only tree changes are the two additions below |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | Absent | SHA-256 `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`; ratified workflow with only `case_sets=(shared)` changed; shared push filter unchanged |
| `scripts/ci/scratch-gh-shim` | Absent | SHA-256 `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`; unmodified ratified shim |

Observations from the [acceptance log](https://github.com/wxxb789/symmetry/actions/runs/46046284991/job/140666554575), using raw `gh run view --log` line numbers and omitting only the Actions prefix:

1. Line 3979: `Case set shared exited 1`. There were **22** `verified … run=1 pass=1 skip=0` rows; the credential-gated leg is not one of them.
2. `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0` is absent. Line 3689: `witness output:     production_linux_v2_native_witness_test.go:224: stage reservation_durable recovery did not preserve the correct physical Journal state: pre-create abort tombstone reports a target or leaf creation`. `helper_bound` was not reached. The two replay Go files are unchanged from `ac04f3e2`; the #309 round-2 head that verified `reservation_durable` is not an ancestor of this PR head.
3. **Authenticated Pi first failure: not observed.** The placeholder run has no `subject_hash` rejection line; the protected workflow returned zero dispatches at `dd9da52f` when queried. The `subject_hash` claim in §9 comes from the local decoder measurement in §3.2, not this run: retained `off1` RPC transcript line 189 has one final `text` block and sets `subject_hash` to `subject.tree_digest`, which differs from the recomputed canonical Subject hash. This confirms the field name for the local capture, not the next stop in a protected run.
4. Line 3107: `credential-gated TestProductionLinuxV2PiNativeProviderAction: run=1 pass=0 skip=0`. Line 3108: `credential-gated: pi_provider_action (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.` Line 3976: `redacted evidence export failed: pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`. The partial export contains `shared/credential-gated-legs.json` (807 B), `shared/pi-native-provider-action.json` (3223 B), `shared/stage-crash-replay-reservation_durable.json` (5728 B), and `shared/unprovisioned-owner-refusal.json` (691 B). The replay JSON is not a pass receipt.
5. No `shared/manifest.json` was published, so there is no manifest status. The first non-gated failure, line 3685, is `Go native acceptance event gate failed: bad=TestProductionLinuxV2StageCrashReplayMatrix=run:1,pass:0,skip:0; TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable=run:1,pass:0,skip:0, skipped=[], fail_events=3, invalid_json_lines=0`.

## Bind (round 2)

**FINAL SCRATCH receipt, not the protected credentialed dispatch.** [Run 46049816624](https://github.com/wxxb789/symmetry/actions/runs/46049816624), attempt 1, ran only `shared` with the placeholder credential (head-filtered census: one run). Round 1 ([run 46046284991](https://github.com/wxxb789/symmetry/actions/runs/46046284991) on PR head `dd9da52f`) is **ACT SUPERSEDED**. The [evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46049816624/artifacts/15224460938) is 4,632 B; downloaded SHA-256 `d0737a3ee54620a4357978c39bbaa588618242bf49e85fba6153917c9182e942` matches Actions.

The run's sole parent is PR head `f6dfea806ab0826810dfb76d8038eb54d9223d8a`, the merge of fix `e754b6da` and main `3d377582` (landed #309). All PR paths match by blob and mode; the two scratch-only additions are unchanged from round 1. Only changed input rows are repeated below (SHA-256 prefixes); the updated and added documentation also matches the PR tree.

| Input | Round 1 PR `dd9da52f` | Final PR `f6dfea80` | Run `30f450a584f405409d3a4b3e71c13a26e590dc40` |
| --- | --- | --- | --- |
| `production_linux_v2_native_witness_test.go` | `d988ca3bf8a0` | `98d4771cf6a3` | Equals PR |
| `production_linux_v2_owner_cli_test.go` | `751eaf4d5e4d` | `96306f493b8a` | Equals PR |
| `scripts/ci/linux-v2-native-acceptance.sh` | `0ab1545f4081` | `a971ab561de0` | Equals PR |
| `scripts/ci/test-linux-v2-evidence.py` | `9335f40c2890` | `0b5f3dcd1890` | Equals PR |

[Acceptance job](https://github.com/wxxb789/symmetry/actions/runs/46049816624/job/140678433314), raw log lines:

1. **4553:** `Case set shared exited 1`. **24** green event-gate rows: the original 22 plus the replay parent and `reservation_durable` subtest.
2. **3687:** `verified TestProductionLinuxV2StageCrashReplayMatrix/reservation_durable: run=1 pass=1 skip=0`. The next `helper_bound` witness was invoked but refused before reaching its hold (4265): `witness output:     production_linux_v2_native_witness_test.go:224: pre-existing RunJournal cf7ad503-93e5-4b19-9f85-5dec44c855f3/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf`.
3. **Authenticated Pi progress and first failure: not observed.** The placeholder reached checkout readiness (`clone.checkout_ready:true`) but not a successful provider action; the two gated crash legs did not run. The protected workflow query returned zero dispatches at `f6dfea80` before and after the scratch run. The whole scratch log has no canonical-hash rejection. The local `off1` decoder measurement (§3.2 and round 1) still names `subject_hash`: the payload at RPC line 189 copied `subject.tree_digest`, and the local test's line 40 says `pi task result must be one complete JSON object: convert task-result generated DTO: subject_hash does not match canonical subject`. That is not a protected-run first failure.
4. **3109:** `credential-gated: pi_provider_action (credential_gated_pi_provider_action) is NOT proven: the ratified placeholder provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as passed.` **4550:** `redacted evidence export failed: pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`. Partial ZIP: `shared/credential-gated-legs.json` (807 B), `shared/pi-native-provider-action.json` (3,224 B), `shared/stage-crash-replay-reservation_durable.json` (6,644 B), and `shared/unprovisioned-owner-refusal.json` (691 B).
5. No `shared/manifest.json`, so no manifest status. **4261**, first non-gated failure: `Go native acceptance event gate failed: bad=TestProductionLinuxV2StageCrashReplayMatrix=run:1,pass:0,skip:0; TestProductionLinuxV2StageCrashReplayMatrix/helper_bound=run:1,pass:0,skip:0, skipped=[], fail_events=3, invalid_json_lines=0`.
