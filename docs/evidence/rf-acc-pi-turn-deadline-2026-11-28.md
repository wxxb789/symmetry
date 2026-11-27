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
| `scripts/ci/linux-v2-native-acceptance.sh` | `ee018988b6057676a4fb00f5e03a7f1dfb3fd7d9cced5aa96e32917623a11b31` | The `shared` case set stages the authenticated Pi legs' task text into the fixed B home's Pi agent directory and installs it under the existing private-file discipline. |
| `scripts/ci/test-linux-v2-evidence.py` | `d1b764a45a605f60754c2ab5fa4799fe3e809f21512f973bcdaf8789d688c852` | `authenticated_pi_task_failures` executes the staging, its Control-fixture agreement and its fixed-B-home install row; one pin and one five-mutant row. |
| `docs/plans/2026-11-28-rf-acc-pi-turn-deadline-plan.md` | `a6a6a912501812cf78102d49faa7439a8e561c903ccb14a18c3748fea3a20549` | This increment's plan (re-hashed after the apply stage's §4 wording fix). |
| `docs/TODO.md` | `51bc4d610a67b863f86f821a5c4c8e727adf38cf91cb385e47081ae50b6ebe29` | The credentialed re-dispatch, the provider-free vehicle stop and the production carriers. |

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

| Arm | Wall to `agent_settled` | Turns | Tools | Commit | Final stop |
| --- | --- | --- | --- | --- | --- |
| delivered bytes (`exact`) | 100.2 s | 27 | `bash` 40, `read` 4, `symmetry_resource_sync` 1 | none | `stop` |
| delivered bytes + the staged text (`instructed`, run 1) | 9.5 s | 6 | `symmetry_resource_sync` 1, `write` 1, `bash` 3 | 1 | `stop` |
| delivered bytes + the staged text (`instructed`, run 2) | 39.2 s | 22 | `symmetry_resource_sync` 1, `write` 1, `read` 3, `bash` 16 | 1 | `stop` |

The jail narrows what a model can explore, so its `exact` row (100.2 s) is a lower bound for the
guest's unspecified turn, which burned the full 424.04 s; the guest row of §3.1 is the binding one.
The instructed rows do the fixture's task end to end: one brokered `resource.sync` per run, one
written file, one commit, and a normal stop. Against the unchanged 424.04 s bound that is a >10x
margin, so the bound needs no amendment (`plan §2.2`).

### 3.3 The measured bytes are the delivered bytes

The `APPEND_SYSTEM.md` the two `instructed` runs staged under their jailed fixed-B home is
byte-equal to `authenticated_pi_task_text` + `"\n"` in `scripts/ci/linux-v2-native-acceptance.sh`
(303 bytes, compared after the runs by a `python3` comparison of the two files; both compared
equal).

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

Recorded consequence, not hidden: the model is *told* the task by the fixture rather than inferring
it from the Control goal. That is the relationship the WorkItem route had before `#303` and the one
the product has when Control's context envelope is fetched; the fixture's own description is the
text, so no leg asserts anything the fixture did not already admit.

## 5. Red/green controls

- No-text control (§3.2 `exact`): 40 `bash` calls, no commit. Guest control (§3.1): the full bound,
  no `resource.sync` receipt. The fix's green rows: two instructed runs, one receipt and one commit
  each.
- Mutation rows over `authenticated_pi_task_failures` (each reddens for its own reason, verified by
  its failure string; the pristine pair returns `[]`):

| Mutation | Failure |
| --- | --- |
| the `shared` guard made unconditional (`if true; then`) | `the stale_leaf case set stages the authenticated Pi task text` (+ `stage_replay_leaf_bound`, `codex_containment`) |
| the staged text reworded (`for the configured GitHub repository` → `for the configured resource`) | `the staged task text does not carry the Control fixture's admitted description` |
| the fixture home writer's `if authenticated_pi_task_text:` replaced by `if False:` | `the fixture home writer did not stage the exact task text` |
| the install target renamed to `APPEND_SYSTEM.txt` | `the fixed B home install list does not follow the staged task text` |
| the Control fixture's description reworded | `the staged task text does not carry the Control fixture's admitted description` |

## 6. Which userland each property needs

The new pin and its mutants are bash/python text-and-staging rows: they execute the fixture's own
guard, the fixture's home writer and the install-list block, and assert the staged bytes against the
Control fixture's description. No permission, ACL, ownership or environment assertion is added, so
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

| Command (working directory) | Exit | Result |
| --- | --- | --- |
| `python3 -B scripts/ci/test-linux-v2-evidence.py` (repo root) | 0 | `Ran 75 tests … OK` (was 73 before this increment) |
| `python3 -B scripts/ci/test-linux-v2-owner-request-keys.py` (repo root) | 0 | `Ran 10 tests … OK` |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | no output |
| `comment-cap.py <scratch clone commit> ac04f3e2` | 0 | no block over 2 lines added |
| `python3 scripts/ci/check-record-cleanliness.py --base ac04f3e2 --head HEAD` (scratch clone of the final bytes) | 0 | `1 changed record(s) pass the root-only check` |
| `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only <changed doc>` | 0 / 0 / 0 | plan, TODO and record pass root-only |
| `python3 scripts/ci/jscpd-changed.py --base ac04f3e2 --head HEAD` (Windows clone of the final bytes) | 0 | `5 changed file(s), 1803 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new` |
| pinned `ruff check` / `ruff format --check`, `scripts/ci/test-linux-v2-evidence.py` (advisory step) | — | base and head identical (`13` E501 rows, none on an added line; the file already fails `format --check` at the base), so no regression |

All rows above ran on the delivered bytes in this host's WSL `Ubuntu-24.04` userland except
`jscpd-changed.py` (Windows, because the pinned jscpd needs `node`); the CI home of the two Python
suites is the `daemon` job's Check Linux v2 fixture contracts step, which runs on the runner image.

No Go source changes, so no `gofmt`/`go vet`/Go test row is claimed here; the `daemon` job's fixture
step and suites are the CI home of the added rows.

Apply stage (the D1/D2/D3 wording fixes): the code-scoped rows above re-ran unchanged (`Ran 75
tests`, `Ran 10 tests`, `bash -n`: exit 0), and the four doc-scoped rows re-ran at exit 0 on the
final bytes of this record and of the plan
(`a6a6a912501812cf78102d49faa7439a8e561c903ccb14a18c3748fea3a20549`) — `comment-cap.py`,
`check-record-cleanliness.py`, both sanitizers and `jscpd-changed.py` (counts as its row above,
nothing new); the implement-stage receipts for those two docs are superseded by these bytes. The
stage's full `go test ./...` runs on the same tree: Windows exit 0, and WSL exit 1 with only the
`internal/platform` uid=0 refusal rows; that package's bytes are identical to the dispatch base.

## 9. Residual risks and known limits

- **The next verbatim stop is named, not fixed.** `stale_leaf` (red in the same credentialed run:
  `wait for exact durable stop receipt: owner-witness command did not reach its requested durable
  stage`, command live, journal at `claimed`) shares the layer: the fixture stages the real provider
  credential into `pi-durability` vehicles whose cases were designed around an instant provider
  failure, so their turns now run. One B home serves a whole guest, so the fix is per case set
  (stage the ratified placeholder for the provider-free sets); it is a separate goal and is recorded
  in `docs/TODO.md`.
- The `shared` set's own provider-free `pi-durability` stage-replay legs (holds
  `stop_receipt_durable`, `leaf_removed`, `journal_cleared`) have never been reached in a
  credentialed run — the `shared` arm aborted at the first authenticated leg. With the staged text
  their turns settle quickly, but whether their post-turn assertions hold on a real provider turn is
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

- Plan and record; `docs/TODO.md` entries for the three open items.
- The measurement harness and its raw arm logs (`c1`, `c2`, `c3`: `summary.json`,
  `bridge-requests.jsonl`, `pi.stdout.jsonl`) under the GDD state root's `scratch/pi-turn-deadline/`;
  the same harness's scripts (`run-turn3.sh`, `bridge-stub.py`, the generated bridge extension).
- The failure-of-record material: the credentialed run log and evidence archive of `45974323458`
  under the GDD state root's `scratch/cred-45974323458/`.
