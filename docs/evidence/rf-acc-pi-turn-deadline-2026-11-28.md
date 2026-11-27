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
| `scripts/ci/linux-v2-native-acceptance.sh` | `0ab1545f4081b73b2114af350e8c2b5a16e2a2dd3e280f1c8480e1d17c2751b6` | The `shared` case set stages the authenticated Pi legs' task text into the fixed B home's Pi agent directory and installs it under the existing private-file discipline; the staged Pi home sets `defaultThinkingLevel` to `off` so the final assistant message carries the one `text` block the production decoder accepts. |
| `scripts/ci/test-linux-v2-evidence.py` | `9335f40c28901dd4664a8bb4abab8c448e53aed09c6c766f5b8262a6e0bb9210` | `authenticated_pi_task_failures` runs the fixture's own guard, home-writer call and install block over four case sets and pins the staged payload, the thinking level and both install-list directions; one pin and eleven exact-expectation mutant rows. |
| `docs/plans/2026-11-28-rf-acc-pi-turn-deadline-plan.md` | `1f91095bc677fada561b32c63c3520fb996a21de696773564259dc76d4e0561a` | This increment's plan (fix r1: the thinking level, the decoder row, the guest-scoped task-text statement). |
| `docs/TODO.md` | — (shared doc, cited by content) | Five `rf-acc-pi-turn-deadline: residuals` bullets: the credentialed re-dispatch, `stale_leaf`, the unobserved provider-free holds, the decoder's `subject_hash` stop and the production carriers. |

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

| Arm (staged profile) | Wall to `agent_settled` | Turns | Tools | Commit | Final stop |
| --- | --- | --- | --- | --- | --- |
| delivered bytes, no append, `minimal` (`exact`) | 100.2 s | 27 | `bash` 40, `read` 4, `symmetry_resource_sync` 1 | none | `stop` |
| delivered text, `minimal` (`instructed`, run 1) | 9.5 s | 6 | `symmetry_resource_sync` 1, `write` 1, `bash` 3 | 1 | `stop` |
| delivered text, `minimal` (`instructed`, run 2) | 39.2 s | 22 | `symmetry_resource_sync` 1, `write` 1, `read` 3, `bash` 16 | 1 | `stop` |
| delivered text, `minimal` (re-measured control) | 10.4 s | 4 | `symmetry_resource_sync` 1, `write` 1, `bash` 3 | 1 | `stop` |
| delivered text, `off` (the delivered bytes) | 7.3 s | 5 | `symmetry_resource_sync` 1, `bash` 3 | 1 | `stop` |
| delivered text + the exact contract values, `off` (not delivered) | 5.2 s | 3 | `symmetry_resource_sync` 1, `bash` 2 | 1 | `stop` |

The jail narrows what a model can explore, so its `exact` row (100.2 s) is a lower bound for the
guest's unspecified turn, which burned the full 424.04 s; the guest row of §3.1 is the binding one.
Every instructed row does the fixture's task end to end: one brokered `resource.sync` per run, one
commit and a normal stop. Against the unchanged 424.04 s bound that is a >10x margin, so the bound
needs no amendment (`plan §2.2`).

The pre-fix rows ran with the staged Pi home's `defaultThinkingLevel` `minimal`; the delivered bytes
set it to `off` (fix r1, RP-01), because the production decoder requires exactly one `text` block in
the final assistant message (`DecodeAssistantTaskResult`, `daemon/internal/harness/pi/rpc.go:901`;
the one-block rule is `:916`; the committed multi-block refusal is `adapter_test.go:657`). The final
assistant message of each captured arm and the decoder's verdict on it:

| Arm (staged profile) | Final message blocks | `DecodeAssistantTaskResult` on the captured message |
| --- | --- | --- |
| delivered text, `minimal` (all three retained runs and the re-measured control) | `[thinking, text]` | rejected: `pi task result must be one complete JSON object` (two blocks) |
| delivered text, `off` (the delivered bytes) | `[text]` | the one-block gate passes; rejected on the model's own JSON: `convert task-result generated DTO: subject_hash does not match canonical subject` (the model wrote the subject's `tree_digest` in place of the canonical hash) |
| delivered text + the exact contract values, `off` (not delivered) | `[text]`, the text block carrying a prose preamble before the JSON | as delivered rejected (the whole text must be one JSON object); the JSON payload after the preamble decodes to `candidate_completion` |

The delivered bytes remove the block-shape rejection. The remaining decoder rejection is the model's
inability to derive the canonical `subject_hash` from what the product sends: the third row is the
counterfactual that isolates it (with the contract values supplied, the same payload decodes), and
§9 records it as the next credential-row stop.

### 3.3 The measured bytes are the delivered bytes

The `APPEND_SYSTEM.md` the two `instructed` runs staged under their jailed fixed-B home is
byte-equal to `authenticated_pi_task_text` + `"\n"` in `scripts/ci/linux-v2-native-acceptance.sh`
(303 bytes, compared after the runs by a `python3` comparison of the two files; both compared
equal). For the re-measured arms, the delivered script's own home-writer call, run with the arm's
model and base URL, emits a home whose `settings.json` and `models.json` are byte-equal to the
arm's jailed home (`cmp`, both files), so the profile bytes the arms measured are the delivered
writer's own bytes.

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

The same home writer sets the staged Pi profile's `defaultThinkingLevel` to `off` (fix r1, RP-01):
the pinned release's own `docs/settings.md` lists `off` among the level values, and the re-measured
arms (§3.2) show the resulting final assistant message without the `thinking` block. One writer
serves every pi case set; the placeholder-credential legs never reach the decoder, so §7's Bind rows
are unchanged.

Recorded consequence, not hidden: the model is *told* the task by the fixture rather than inferring
it from the Control goal. That is the relationship the WorkItem route had before `#303` and the one
the product has when Control's context envelope is fetched; the fixture's own description is the
text, so no leg asserts anything the fixture did not already admit.

## 5. Red/green controls

- No-text control (§3.2 `exact`): 40 `bash` calls, no commit. Guest control (§3.1): the full bound,
  no `resource.sync` receipt. The fix's green rows: two instructed runs, one receipt and one commit
  each. Decoder rows: the pre-fix profile's captured messages are rejected on the block shape
  (`[thinking, text]`), the delivered profile's message passes that gate, and the
  supplied-contract-values payload decodes to `candidate_completion` (§3.2).
- Mutation rows over `authenticated_pi_task_failures` (eleven mutations, each asserted against the
  exact failure list it must produce; the pristine pair returns `[]`). The rows run in the `daemon`
  job's Check Linux v2 fixture contracts step as the non-root runner user:

| Mutation | Failure list |
| --- | --- |
| the `shared` guard made unconditional (`if true; then`) | the three case-set rows (`stale_leaf`, `stage_replay_leaf_bound`, `codex_containment`) plus `the fixed B home install list stages the task text without it` |
| the staged text reworded (`for the configured GitHub repository` → `for the configured resource`) | `the shared case set does not stage the exact authenticated Pi task text` |
| the fixed-scope paragraph deleted from the staged text | `the shared case set does not stage the exact authenticated Pi task text` |
| the invocation's last argument (`"$authenticated_pi_task_text"`) blanked to `""` | `the shared case set does not stage the exact authenticated Pi task text` |
| the fixture home writer's `if authenticated_pi_task_text:` replaced by `if False:` | `the shared case set does not stage the exact authenticated Pi task text` |
| the install target renamed to `APPEND_SYSTEM.txt` | `the fixed B home install list omits the staged task text` |
| the install row removed from the acceptance script | `the fixed B home install list omits the staged task text` |
| the Control fixture's description reworded | `the shared case set does not stage the exact authenticated Pi task text` |
| the Control fixture's `pi_provider_action` row renamed | `the Control claim fixture no longer carries the pi_provider_action description` |
| the invocation's last argument dropped (argument count changed) | `the fixture staging script for {shared, stale_leaf, stage_replay_leaf_bound, codex_containment} exited 1` |
| the staged thinking level raised (`off` → `minimal`) | `the staged pi home no longer sets the thinking level off: 'minimal'` |

## 6. Which userland each property needs

The new pin and its mutants are bash/python text-and-staging rows: they execute the fixture's own
guard, the fixture home writer's real shell call (its argument list included) and the install-list
block verbatim over four case sets, and assert the staged file bytes against the Control fixture's
description plus the fixed-scope paragraph. No permission, ACL, ownership or environment assertion is added, so
the runner-mirroring requirement (lesson 160) is not engaged; the rows run in the `daemon` job's
Check Linux v2 fixture contracts step on the runner image, and the local runs above are recorded as
this host's oracle (WSL `Ubuntu-24.04`, `VERSION_CODENAME=noble`, python 3.12.3). The guest-side
effect of the staged file is the credentialed dispatch's row.

## 7. Does the conductor's Bind execute it?

The placeholder `shared` Bind executes one gated leg, `pi_provider_action`, through
`run_credential_gated_pi_provider_leg` (`scripts/ci/linux-v2-native-acceptance.sh:2613-2631`, the
case name hard-coded at `:2615`), which drives the same Go test to the placeholder 401 stop; the two
crash legs are only registered by `record_credential_gated_leg` (`:3303-3306`), and the declaration
marks them `not_run` (`:2589`). The staged text cannot change a provider refusal, so every Bind row
is expected to be byte-identical to Bind 2 of `rf-acc-pi-authentic-turn`, and the Bind proves nothing
new for the two crash legs. The two Python suites (including the new pin and its mutants) run in the
`daemon` job on the PR head. The authenticated outcome is the owner's credentialed cadence, not the
Bind.

## 8. Gates

Every command below ran on the delivered bytes of §1 within the range `ac04f3e2..HEAD`; the
doc-scoped rows ran in a scratch clone holding exactly those bytes as one commit. The CI home of
the two Python suites is the `daemon` job's Check Linux v2 fixture contracts step (it runs them
from the `daemon` directory on the runner image).

| Command (working directory) | Exit | Result |
| --- | --- | --- |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` (repo root, WSL) | 0 | no output |
| `python3 -B scripts/ci/test-linux-v2-evidence.py` (repo root, WSL) | 0 | `Ran 75 tests … OK` (was 73 before this increment; fix r1 keeps 75) |
| `python3 -B scripts/ci/test-linux-v2-owner-request-keys.py` (repo root, WSL) | 0 | `Ran 10 tests … OK` |
| `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only <plan, record, TODO>` (repo root) | 0 / 0 / 0 | all three changed docs pass root-only |
| `python3 scripts/ci/check-record-cleanliness.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | `1 changed record(s) pass the root-only check` |
| `comment-cap.py <scratch clone> ac04f3e2` | 0 | no added comment block over 2 lines |
| `python3 scripts/ci/jscpd-changed.py --repo <scratch clone> --base ac04f3e2 --head HEAD` | 0 | `5 changed file(s), 1803 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new` |
| pinned `ruff` 0.17.5 `check scripts/ci` and `format --check scripts/ci`, in the delivered tree and in the dispatch base tree (WSL) | — | identical finding sets at both revisions: `30` findings with `25` E501 rows (none of them on a line this range adds) and `8` files-to-reformat; the advisory CI step is `continue-on-error`, and the range moves neither number |

The Go rows, declared once: `git diff --stat ac04f3e2..HEAD` lists only the five paths of §1, so no
Go source is changed, no `gofmt`/`go vet` or Go test row binds a delivered byte, and the `daemon`
module's own suites were not re-run for this range. The apply stage did run `go test ./...` on the
same Go bytes (Windows exit 0; WSL exit 1 with only `internal/platform`'s uid=0 refusal rows), and
`git diff ac04f3e2..HEAD -- daemon/internal/platform` is empty, so those refusal rows are
pre-existing byte-identical rows, not a regression of this increment.

Fix r1 receipt: the code-scoped rows above (`bash -n`, the two Python suites) and the doc-scoped
rows (both sanitizers, `check-record-cleanliness.py`, `comment-cap.py`, `jscpd-changed.py`) ran on
the final bytes of §1 — the test file and the acceptance script after the RP-01/RP-03 rework and
the plan/record/TODO after their wording fixes — so every implement- and apply-stage receipt for
those files is superseded by these bytes.

## 9. Residual risks and known limits

- **The next verbatim stops are named, not fixed.** In the `shared` arm the first authenticated leg
  now fails at the production decoder rather than at the bound: the model writes the subject's
  `tree_digest` where the canonical `subject_hash` is required, and neither the goal title nor the
  witness's `nativeContext()` (`daemon/internal/app/linux_owner_witness_linux.go:3948`) carries a
  hash the model can copy (§3.2: with the exact subject and hash supplied, the same payload
  decodes; the same canonical-hash burden applies to every native harness's TaskResult, not only
  pi). The fix has two carriers — the fixture's staged task text (this increment's mechanism, values
  computed from the resolved subject) or a product-side carrier in the witness context — and needs a
  conductor ruling (`DECISION_NEEDED`); `docs/TODO.md` records it. `stale_leaf` (red in run
  `45974323458`: `wait for exact durable stop receipt: owner-witness command did not reach its
  requested durable stage`, command live, journal at `claimed`) shares the credential-staging layer:
  the fixture stages the real provider credential into a `pi-durability` vehicle whose case was
  designed around an instant provider failure, so its turn now runs. One B home serves a whole
  guest, so the fix is per case set (stage the ratified placeholder for the provider-free sets); it
  is a separate goal and is recorded in `docs/TODO.md`.
- **The model prefixed prose once.** In one of the three `off` arms the final text block began with
  a prose sentence before the JSON; the decoder accepts only a whole-text JSON object, so that arm
  would fail as delivered even with the contract values supplied (§3.2, third decoder row). The
  other four captured messages were pure JSON; the model-side risk is unmeasured and belongs to the
  credentialed row.
- The `shared` set's own provider-free `pi-durability` stage-replay and ack-loss legs receive the
  staged append text too (one B home per guest) although their profile is `provider_access=False`
  and their cases were designed around an instant provider failure; their turns now run. Their
  post-turn holds (`stop_receipt_durable`, `leaf_removed`, `journal_cleared`, the 30-second
  stop-receipt barrier) have never been reached in a credentialed run — the `shared` arm aborted at
  the first authenticated leg — so whether they hold on a real provider turn is unobserved here and
  is the credential re-dispatch's third observation.
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

- Plan and record; `docs/TODO.md` entries for the five open items.
- The measurement harness and its raw arm logs (`c1`, `c2`, `c3`: `summary.json`,
  `bridge-requests.jsonl`, `pi.stdout.jsonl`) under the GDD state root's `scratch/pi-turn-deadline/`;
  the same harness's scripts (`run-turn3.sh`, `bridge-stub.py`, the generated bridge extension).
- The fix round's re-measured arm logs (`runs/off1`, `runs/min1`, `runs/sup1`, each with its
  captured `final-message.json` and `summary.json`) under the WSL `<scratch>` jail root, the
  retained runs' extracted final messages beside their `pi.stdout.jsonl`, and the throwaway decoder
  check — a scratch copy of the daemon module holding one `_test.go` that calls
  `DecodeAssistantTaskResult` on those captured messages, never placed in the repository tree.
- The failure-of-record material: the credentialed run log and evidence archive of `45974323458`
  under the GDD state root's `scratch/cred-45974323458/`.
