# rf-acc-pi-turn-deadline — plan (stage 1 root cause, stage 2 fixture fix)

Contract: the goal file under the GDD contracts root (`goal-rf-acc-pi-turn-deadline.md`), ruled by
the conductor 2026-11-27; dispatch gate: the `rf-acc-pi-authentic-turn` PR (`#308`, merged as
`ac04f3e2`). Branch `refactor/rf-acc-pi-turn-deadline`, dispatch base `ac04f3e2`. Failure of record:
the credentialed dispatch `45974323458` (`main` `d45e3298`), `shared` exit 1, gate row
`bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0`. Shape reference for the
record: `docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md` (the merged predecessor increment).
This plan fixes only what the accepted plan leaves open: the exact task-text carrier, the
observation bound, the fixture pin, and what stays out of scope.

## 1. What this increment delivers

1. **Stage 1 root cause, measured.** The credentialed Pi turn does not run a bounded task: the
   goal-routed Control admission puts only the Goal *title* into the task's `goal`
   (`control/lib/symmetry_control/goals/admission.ex:418,573` → `Protocol.work/1` `goal: task.goal`,
   `control/lib/symmetry_control_web/protocol.ex:446`), and the witness replaces the Control context
   with a subject/run-key stub (`linux_owner_witness_linux.go:3948-3955`), so the model's
   `<symmetry_goal>` is `Linux v2 pi_provider_action owner witness` and no other field carries the
   task. #303 moved the three authenticated Pi legs onto that route (`write-linux-v2-owner-request.py`
   `GOAL_ADMISSION_CASES`); before it they were WorkItem-launched tasks whose `goal` was
   `execution_goal/1` = title + `\n\n` + description (`control/lib/symmetry_control/workspaces.ex:684,765-770`).
   With the ratified placeholder the turn died at the provider in under a second, so the missing
   text was invisible; with a real credential the model explores the repository instead of calling
   the brokered tool and the leg misses the observation bound. Measurements, the guest evidence
   (191 594 B Pi bash log, no `provider_action_intents` receipt, stage markers stopping at
   `native_turn_started`, `provider_action_succeeded: false`, 424.04 s failure) and the
   reproduction rows are in `docs/evidence/rf-acc-pi-turn-deadline-2026-11-28.md`.
2. **Stage 2 fixture fix.** The acceptance fixture delivers its own admitted task text to the
   authenticated Pi legs through Pi's agent-directory system-prompt append
   (`$B_HOME/.pi/agent/APPEND_SYSTEM.md`, `docs/configuration.md` in the pinned 1.5.2 release), so
   the model receives the same task the Control fixture admits. Nothing else changes for those legs:
   the same request, the same profile args, the same tools, the same assertions.
3. **The observation bound stays as it is, with the measurement recorded.** The bound is not the
   named side: measured sum in a guest-shaped jail (same model, same pinned Pi release, same argv and
   environment shape, same subject clone, same prompt bytes) an instructed turn settles in 9.5 s and
   39.2 s and commits its file, while the stalling guest turn ran for the full 424.04 s bound. A
   longer bound would spend the same wall time on the *unspecified* legs that produced the stall and
   would not make any clause true; the record carries the numbers instead (§2.2).
4. Committed test pins, the plan, the evidence record, `docs/TODO.md`/`docs/archive/CLOSED-TODO.md`.

## 2. Decisions this plan fixes

### 2.1 The task text is staged by the fixture, not by production code

Three carriers were available: (a) `control/lib/…/goals/admission.ex` composing the task goal the
way `execution_goal/1` does, (b) the Control *fixture* putting the description into the Goal title
(`provision-linux-v2-control-claims.exs`), (c) the acceptance fixture staging Pi's own
agent-directory append. (a) and (b) are production Control and a non-owned fixture file; the
contract's owned set is the witness test, its helpers and the acceptance script's `shared` arm, and
it routes production changes to `DECISION_NEEDED`. (c) is inside the owned set, is scoped to the
authenticated legs (only the `shared` case set stages it, so the provider-free `pi-durability`
vehicles keep the turn their cases were designed around), and restores exactly the text the
fixture's Control table already carries (`provision-linux-v2-control-claims.exs` `@cases`
descriptions). Recorded consequence, not hidden: the model is *told* the task by the fixture
instead of inferring it from the Control goal — the same relationship the product has, where the
task reaches the model through the admitted goal text/context, and the same relationship the
WorkItem-launched legs had before #303.

### 2.2 The observation bound stays at seven minutes

`observeLinuxV2OwnerWitnessRun` observes for `linuxV2NativeTaskTimeout` (7 minutes, PR #9 vintage,
tuned while every Pi turn failed at the provider in well under a second). Candidates were: leave it,
derive it from the claim's admission deadline (`claim.work.input.limits.deadline_at`, Control's
default admission + 15 minutes), or extend it by a measured margin. The measurements decide against
extending it: the specified turn is *short* (9.5 s and 39.2 s locally, against 100.2 s for the same
bytes without the task text and 424.04 s in the guest), so the seven-minute window already carries a
>10x margin, and every second added to it is spent only by the *unspecified* legs that stalled —
exactly the failure this goal removes. Raising a bound that is not the cause would also be the
"raise it until it passes" move the contract forbids. The record states the measured margin so a
later amendment has a number to argue with.

### 2.3 What the fixture pin has to prove

`scripts/ci/test-linux-v2-evidence.py` (the suite that already pins the acceptance script and its
`shared` arm) gains one row: the staged append file carries the Control fixture's own admitted task
description for the authenticated legs and is installed into the fixed B home's Pi agent directory
with the same private-file discipline as the other staged Pi files. Mutants: dropping the staging,
re-pointing it at another directory, dropping it from the credential-file list, or rewriting the
text so it no longer carries the admitted description each redden the row.

## 3. Evidence plan

- Local, same carrier: the pinned `pi-linux-x64.tar.gz` 1.5.2 executable
  (`sha256:6a2ac1c5…`, tree `f5fc36f7…`), the fixture's own `settings.json`/`models.json` bytes, the
  fixture's generated provider-bridge extension (`sha256:315a32c0…`, the guest's staged digest), the
  fixture's `--provider openai --model deepseek-v4.1-flash-fast --mode rpc --no-extensions
  --extension <path>` argv, a clone of the subject commit, and the harness's exact
  `buildTurnPrompt` bytes (title-only goal + the witness's stub context + the embedded TaskResult
  schema). Arm A: the delivered bytes (no append). Arm B: the delivered bytes plus the staged
  append. Rows: wall time to `agent_settled`, tool calls, the bridge stub's request log, the commit
  count, the Pi bash-log bytes, the final assistant `stopReason`.
- Guest: the conductor's Bind re-runs `shared` on the PR head (placeholder credential; the
  credential-gated named outcome is the expected row) and the owner's credentialed cadence is the
  acceptance row for the authenticated turn.
- Gates: the `daemon` package's targeted rows, the Pi harness rows, the two Python fixture suites,
  `gofmt`/`go vet`, `bash -n`, and the acceptance script's static checks, each with its exit code.

## 4. Out of scope, with reasons

- **Production daemon/Control changes.** The root cause's production-side fix (compose the
  goal-admitted task's `goal` from the item description, or forward the admission through the
  witness context) is named in the record for `DECISION_NEEDED`; this increment does not touch
  `daemon/internal/**` sources.
- **The `stale_leaf` case set.** Its red (`wait for exact durable stop receipt`, command live,
  journal at `claimed`) shares the layer — a provider-free `pi-durability` vehicle whose turn was
  designed to fail instantly now receives the real credential — but it is a different case set with
  its own fixture decision (stage the ratified placeholder for provider-free vehicles, or bound its
  stop-receipt hold). The record names it as the next verbatim stop; the fix is a separate goal.
- **Raising the seven-minute default.** The default stays; only an admitted claim's own deadline
  extends the window, and only by measurement.
- **`docs/archive/CLOSED-TODO.md` moves that no closed item owns.**
