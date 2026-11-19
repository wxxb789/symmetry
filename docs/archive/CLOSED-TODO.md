# CLOSED TODO

Done items moved out of `docs/TODO.md`, each kept with the closure statement its own text records —
that the thing is fixed, enforced, closed or resolved — and a named commit, PR or branch where one
is recorded. `docs/TODO.md` holds only undone or in-progress items; where a closed part and an open
residual share an item, the closed part is here and the residual stays in `docs/TODO.md` with a line
saying where it went. This file is separate from `docs/goals/archive/`, which holds archived
contracts. Section names of `docs/TODO.md` are preserved below so context survives the move, and
fragments of items keep their original indentation. Each entry keeps its closure statement and any
closure date it records. Entries in this file older than six months may be deleted.

The blocks below were moved on 2026-11-18. Markers (`- [ ]`, `- [x]`) are reproduced as they were,
not a claim that work remains.

The five dead-code closures appended to `## Dead-code candidates` below were moved on 2026-11-19
from branch `chore/deadcode-revalidation`.

## Model-routing evaluation records

- [x] Fixed the half-applied usage-receipt import (serialization gap) reported by the cancelled 0008a round-2
      review. `receipt` in `scripts/ci/model-routing-evaluation.py` derived the candidate usage and published
      `frozen/usage.json` and `run.json` before `write_json(receipt.json)` serialized the receipt, so a valid
      JSON usage row carrying a lone surrogate (`\ud800` in the opaque `metadata` passthrough: `strict_json` and
      `validate_usage` accept it, `canonical_json` refuses it at `encode("utf-8")`) left the frozen usage bytes
      and the run record at the candidate revision while `receipt.json` stayed at the previous one; `accept`
      then refused at `$.evidence.frozen_usage_sha256` and the run could not be re-derived. Fix:
      `command_receipt` serializes the derived receipt and the run record (`run_record_bytes`, shared with
      `save_run`) before the first write, so no document is published unless the receipt for the candidate has
      been serialized; on any refusal the three files stay byte-identical. Reproduction:
      `python3 scripts/ci/test-model-routing-evaluation.py -v EvaluationTests.test_serialization_refusal_publishes_nothing`
      (red before the fix on the `frozen/usage.json` byte equality, green after; re-broken on demand by moving
      `canonical_json(receipt)` back after the writes, which reproduces the same failure). Validation was not
      relaxed: the surrogate document is still refused.

- [x] Rebound the 0008a audit's source manifest to the delivered revision `1c6c715` (audit finding F12) after
      the final rebase left its previous binding unreachable. The manifest and the repair list named the
      pre-rebase feature tip `c45db2c` and the pre-rebase repair commit `1baf76c`; once the branch was rebased
      onto `7a92ca4` and delivered as `1c6c715`, `git show c45db2c:<path>` failed for a reader and eight entries
      described no published revision - including `docs/goals/README.md` (16713 bytes `sha256:aac4ca76…` ->
      17578 bytes `sha256:b764dabf…`) and `.github/workflows/ci.yml` (103330 `sha256:a0ad0020…` -> 103379
      `sha256:4e73a080…`). Rebinding was chosen over marking history because the manifest's own method is
      `git show <bound commit>:<path>`, and only main's history is durable for a reader. All 17 entries now
      re-derive with `git show 1c6c715:<path>`; the scoped digest is `sha256:9dc95d2f…` (was `sha256:34cc3198…`);
      the pre-rebase values, the moved list and the delivered commit mapping (`e4a91c1` F1-F3, `bdbcdc7` F4-F9,
      `abf8210` F10-F11, `993870e` post-audit repairs, squashed as `1c6c715`) are retained in F12; the evidence
      record's quoted audit digest (`sha256:dc878099…`) and manifest digest are updated, and its delivery
      narrative now names the delivered commits. Run-time observations (gate results, link checks, receipt
      re-derivations recorded in the audit) remain bound to the pre-rebase reviewed revision and are labelled as
      history.

## Dead-code candidates

- [ ] Investigate the intermittent Windows failure `TestCloseStopsOwnedChildAfterLeaderExit`
      in `daemon/internal/platform` (`process_windows_test.go:1004`, "owned child <pid>
      survived Close()"). Observed in run 43720680428 job 132831780245 on a docs-only diff,
      green in run 43715830442 and on rerun job 132834151127. Revalidate the close/settle
      window; keep the survived-child assertion.
      Closed by the identity-bound close/settle proof
      recorded in the `daemon/internal/platform` flake-class entry below (the second occurrence,
      run 44055523619 job 133943809688, is classified there); the observation above stays as the
      record, and only the wider 0008b settlement contract recorded there remains open.

- [ ] Investigate the load-sensitive omp harness failure
      `TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock` in
      `daemon/internal/harness/omp` (`adapter_final_message_test.go:44`,
      `context deadline exceeded` after 1.38 s). Observed once during goal 0008b's
      repository-wide `go test ./...` run (1,797 top-level tests: 1,740 pass,
      56 skip, 1 fail) while that package alone then reported `ok` at 4.631 s,
      and not reproduced in isolation: `-count=3` passed on the unchanged base
      clone `5c2e0ada` and on the working tree. Revalidate the deadline budget
      under load; keep the assertion. Closed on `main` by `8082317` (#44): 89 wall-clock budget
      sites across 16 test files now point at one documented `testHangGuard` per package
      (`const testHangGuard = 30 * time.Second`, "turns a hung harness into a failure; never a
      statement about how fast the adapter should be"), no assertion was removed or weakened, and in
      `omp` a failing wait now names the outstanding step (`mustSettle(t, "WaitTurn", err)`) so a
      guard expiry can no longer be reported as this test's behavioural failure. The observation
      above stays as the record; the open action does not.

*Closed part of the item “Close the `daemon/internal/platform` flake class …” in `docs/TODO.md`; the open residual stays there.*

      **Linux terminal reason — fixed at the cause; verification is CI on the new revision, not a
      local green.** `TestProcessGroupLeaderExitAfterSuccessfulDescendantScanRemainsProven` failed
      twice in the same source family with `process_unix_linux_test.go:2119: monitor terminal reason
      = owner_stop, want leader_absent`: run 43898893639 job 133409923040 (`go test -race ./...` on
      the 0007d head, green on rerun of the same job) and run 44027453497 job `daemon` (push of main
      `dacec450`), while the **same tree** was green on the `pull_request` run 44026216492 (merged
      head of #41, `cf9e4a1`). That job runs, from `daemon/`:
      `python3 ../scripts/ci/stdio-only-caller-boundary.py "$(command -v go)" test -race -count=1 -timeout 600s ./...`
      (step "Run race tests with the same stdio-only caller boundary", `.github/workflows/ci.yml`,
      after that job's `gofmt -l .` and `go vet ./...`). Cause: one first-writer-wins field had two
      writers — the stop requester wrote `owner_stop` *before* it signalled `monitorStop`, and the
      monitor wrote `leader_absent` only after a clean scan observed the leader gone — so a caller
      asking whether the leader was proven absent received whichever path arrived first. Now the
      monitor goroutine is the only writer and publishes the reason before it closes `monitorDone`
      (`process_unix.go`: `recordDescendantMonitorTerminalReason`, `stopDescendantMonitor`,
      `monitorDescendants`); the shipped assertion is unchanged.
      `TestProcessGroupStopRequestKeepsCleanLeaderAbsentTerminalReason` forces the two paths into
      that race's order and fails with the same `owner_stop` text against the unpatched revision. On
      this host's Linux (WSL, go1.27.0) the shipped test itself hit `owner_stop` in 2 of 800 `-race`
      runs before the fix and 0 of 800 after; that still only judges a gate once CI runs it on a
      pushed revision.
      **Windows owned-child — untouched.** `TestCloseStopsOwnedChildAfterLeaderExit`
      (`process_windows_test.go:1004`, "owned child <pid> survived Close()") was observed in run
      43720680428 job 132831780245 on a docs-only diff, green in run 43715830442 and on rerun job
      132834151127; this change makes no claim about it, so this entry stays open. The Linux
      supervisor helper-witness flake
      (`TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`) keeps its own entry in `docs/TODO.md`.
      **Windows owned-child — fixed at the cause; verification is CI on the new revision, not a
      local green.** `TestCloseStopsOwnedChildAfterLeaderExit` failed again in run 44055523619 job
      133943809688 (push of main `ca1bdad`) with `process_windows_test.go:1004: owned child 7824
      survived Close(); tasklist output: "\"cmd.exe\",\"7824\",\"Console\",\"2\",\"4,540 K\""`
      (0.19 s), on a tree whose `daemon-windows` job passed minutes earlier in run 44054200088
      (PR #48, head `95b9fa1`); the earlier occurrence was run 43720680428 job 132831780245
      (docs-only diff), green in run 43715830442 and on rerun job 132834151127. Discriminator: the
      shipped assertion observed `tasklist /FI "PID eq <pid>"` membership, which is neither the
      owned process's creation-time identity nor its termination state — a same-PID process with a
      new creation time satisfies it, and a process already dropped from the process snapshot
      satisfies its negation. On this host (Windows 10.0.26300) the shipped test with the *old*
      assertion passed while `OpenProcess`+`WaitForSingleObject(0)` on the same PID returned `0x102`
      (WAIT_TIMEOUT): the child was still running when `Close()` returned success, so the old
      assertion could not fail for its claimed reason. Cause (product): `jobContainment.Close`
      published a stop proof built from `JobObjectBasicAccountingInformation.ActiveProcesses == 0`
      alone, and that accounting reaches zero when the Job starts terminating its members, not when
      they have stopped. `terminateJobAndProveStop` (`process_windows.go`) now samples the processes
      the Job owned through `JobObjectBasicProcessIdList`, binds each to `ProcessIdentity`
      (pid + creation FILETIME), and reports success only after every sampled process object
      signals within the existing `containmentCloseDeadline`, failing closed otherwise;
      `terminateAndObserveSupervisorJob` shares that proof, so the supervisor's recovery stop is no
      longer accounting-only. In the test, `windowsOwnedProcessRunning` binds the same identity and
      waits on the same process object. Evidence: with the identity-bound assertion and the
      unpatched product the shipped test failed `owned child 47564 survived Close(); the recorded
      process is still running (wait status 0x102)`; with the proof in place the shipped test, the
      whole `internal/platform` package on this host (`go test -count=1 ./internal/platform`,
      exit 0, 9.5 s) and the new `TestOwnedChildObservationBindsCreationTimeIdentity` pass, and that
      new test fails `recycled identity windows:<pid>:<creation+1> was accepted as the live owned
      child` when the observer ignores the creation time (PID-only matching, as shipped). Open: a
      repeat is now a hard, correctly-attributed failure — `Close() error = owned process <pid>
      (<identity>) was still running after the containment close deadline` (the product refusing an
      unproven stop) or the same text from the test's observation — and would mean a process that
      really outlived its Job rather than a listing or recycled-PID artifact. The wider contract
      (`docs/evaluations/model-routing/tasks/0008b-go-close-owned-settle.json`) still owns the
      supervisor protocol's escaped-descendant semantics; this change settles the daemon-side and
      supervisor-side stop proofs only. The same PID-only membership observation still verifies
      child liveness in `internal/execution` (`runner_windows_test.go`, its own
      `windowsProcessExists`); it produced none of today's failures and was not touched here.

- [ ] Revalidate and remove the unused `JsonDocument` alias in
      `control/lib/symmetry_control/goals/schemas.ex:1431`
      (`SymmetryControl.Goals.GoalExternalWait`). The 0006h focused Control run
      reported the unused alias; a scoped search of that module found only its
      alias declaration. Leave removal and verification to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): `JsonDocument`
      was removed from the alias line, which now reads
      `alias SymmetryControl.Goals.{Goal, GoalEvent}`; the file's other modules
      (`GoalRevision`, `WorkOutcome`, `GoalDecision`) keep their own aliases and
      uses. Revalidated with a repository-wide `JsonDocument` token search over
      every file type including generated artifacts (`rg -uuu`: the source
      matches are only `schemas.ex` and docs, and the only BEAM artifacts that
      mention it are the definition and the three modules that use it —
      `Elixir.SymmetryControl.Goals.GoalExternalWait.beam` does not), a
      module-span search of `GoalExternalWait` (lines 1427-end of file: no token,
      and no re-export or alias in that module wraps the type), and a
      fully-qualified `Goals.JsonDocument` search.
      `MIX_ENV=test mix compile --force --warnings-as-errors` exits 0 and the goal
      schema/context suites pass under `--warnings-as-errors`. Elixir 1.20.4 no
      longer emits the unused-alias warning (probed with a minimal unused alias),
      so the searches are the primary record.

- [ ] Revalidate the unused `replay_task` binding at
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:1059`
      and `task` bindings at lines `2079` and `2170`. The 0007a `mix test` run
      reported these compiler warnings (`.tmp/0007a-session/control-test.log`).
      Preserve the fixture calls and leave any binding cleanup to separate
      maintenance.
      The 0009 full Control run reports the same bindings at lines `1351`,
      `2450` and `2541` in `.tmp/0009/control-full.log`; these are current
      coordinates, not additional cleanup work.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): the three
      destructuring bindings were renamed `_replay_task` (line 1351) and `_task`
      (lines 2450, 2541), so the side-effecting `insert_goal_task/1` fixture calls
      stay and only the dead names go. Revalidated with a repository-wide
      `replay_task` token search (only the binding and docs match), searches for
      `:replay_task`/`"replay_task"` atoms and strings, a
      `String.to_atom`/`Task.Supervisor`/`start_child` search in the file, and the
      compiler itself: with the bindings restored, `mix test --warnings-as-errors`
      fails on exactly the three unused-variable warnings at 1351/2450/2541; with
      the renames the file reports 77 passed, 0 warnings.

- [ ] Revalidate the unused default `kind \\ "omp"` on private
      `register_unverified_omp_runtime/2` in
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:3651`.
      The 0009 full Control run reports that the optional default is never used
      (`.tmp/0009/control-full.log`). Preserve the runtime fixture behavior and
      leave cleanup and verification to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): the default was
      removed (`defp register_unverified_omp_runtime(label, kind)`); the function
      is private, both call sites (lines 109 and 128) pass `kind` explicitly, and
      an `apply(`/capture search in the file found no dynamic dispatch, so no `/1`
      call can exist. The compiler confirms: restored,
      `mix test --warnings-as-errors` fails with `default values for the optional
      arguments in the private function register_unverified_omp_runtime/2 are
      never used`; removed, the same command reports 77 passed, 0 warnings.

- [ ] Revalidate and remove the unused `Connection.statuses/0` accessor at
      `control/lib/symmetry_control/integrations/schemas.ex:92`. A scoped search for
      `Connection.statuses` and `Integrations.Connection.statuses` in `control/lib` and
      `control/test` found no caller; the changesets read the `@statuses` module attribute
      directly. Observed while fixing goal 0007d. Leave removal and verification to separate
      maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): `def statuses, do:
      @statuses` was removed; `@statuses` stays (the three changesets read it at
      lines 48/66/76) and the module's public surface keeps `providers/0`,
      `capabilities/0` and `auth_types/1`. Revalidated with a repository-wide
      `statuses` word search (no caller of the accessor anywhere; no `defdelegate`
      exists in `control/lib`; no `one_of`, OpenAPI/spec or frontend type
      generation derives from it), a check of every `Connection.` call site
      (`integrations.ex:43-49` uses `providers/0`, `capabilities/0` and
      `auth_types/1`; `portal_integrations_test.exs` uses `Connection.changeset/2`,
      Ecto queries and the portal API only), and
      `MIX_ENV=test mix compile --force --warnings-as-errors` exiting 0 — any
      remaining Elixir caller of the removed function would fail that gate.
      `integrations_test.exs` and `portal_integrations_test.exs` pass under
      `--warnings-as-errors`.

- [ ] Remove or justify the unreferenced CSS rules `.chip-failed` and `.chip-pending` in
      `frontend/src/styles.css:170-171`. A sweep of all 137 class selectors in that file against
      `frontend/src` found these two unreferenced already at `7a92ca4` (untouched by the 0007b,
      0007c and 0007d work), so they are pre-existing dead weight rather than a merge artifact.
      Revalidate before deleting and leave removal to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): kept, because both rules are
      reached through constructed class names — `work-surface.tsx:330` and `detail.tsx:303` render
      `chip chip-${ci.tone}` and `runtimes.tsx:59` renders `chip chip-${row.tone}`, with `failed`
      and `pending` among the tones (`frontend/src/features/work/presentation.ts:247`, and
      `runtimes/presentation.ts` for the not-supported, next-turn and estimated capability
      presentations; the rules now sit at `styles.css:216-217`) — so the literal-selector sweep's
      "unreferenced" premise was wrong and no rule was removed.

## Deferred review findings (0007h)

*Closed part of the item “Harden the optional-but-consumed members of the served reads …” in `docs/TODO.md`; the open residual stays there.*

      Resolved in `fix/served-read-required-members` (2026-11-18). Every member a surface consumes that
      the projection always emits is now `required` in the schema that describes it, so a rename on the
      serving side fails the bundle gate, the Elixir served-document test and the Effect boundary instead
      of decoding into a silent fallback: `goal-read.schema.json` gained 46 required members — root
      `project_key`, `revision`, `settlement_attention`, `accepted_outcomes`, `external_waits`; revision
      `objective`, `non_goals`, `acceptance_contract`, `authority_policy`, `execution_policy`,
      `context_manifest`, `inserted_at`; work item `number`, `accepted_outcome`, `execution`, `blockers`;
      graph node `number`, `blockers`, `accepted?`; graph edge `blocker`; decision `goal_revision`,
      `question`, `options`, `resolution`; outcome `work_item_id`, `candidate_subject`, `subject_hash`,
      `evidence_ids`, `inserted_at`; accounting `settled_reservation_count`, `released_reservation_count`,
      `known_usage_cost_microusd`, `usage_cost_microusd`; external wait `reason`, `resource_id`,
      `external_ref`, `next_check_at`, `check_seq`; settlement attention `event_id`, `task_id`,
      `goal_revision`, `reason`, `proposed_next_action`, `proposed_next_action_status`; blocker `ids`,
      `details` — and `attention-read.schema.json` gained 19 — root `selected_project_id`, feed
      `next_cursor`, entry `project_id`, `attention_reasons`, `active_tasks`, `nonterminal_tasks`, blocked
      row `project_key`, `project_name`, `project_status`, `work_item_key`, `blocker`, plus the same
      `GoalBlocker` (`ids`, `details`) and `SettlementAttention` (`event_id`, `task_id`, `goal_revision`,
      `reason`, `proposed_next_action`, `proposed_next_action_status`) members, because the Effect
      generator keeps one graph per shared definition name and refuses two spellings of it. Every newly required
      member stays nullable where the projection can serve `null`; no scalar constraint moved.
      `pnpm contracts:generate` regenerated the TypeScript, Effect, Go DTOs and the daemon schema bundle
      (21 schemas, 219 fixtures, both unchanged), and `contracts/scripts/boundary.test.mjs` pins the class:
      a renamed consumed member of the served Goal or attention read is refused, a renamed unconsumed
      member still decodes.
      Still optional, by design rather than by omission: the workspace-read row's consumed members
      (`resources[].id`/`status`/`sync_status`, project `version`/`default_agent_profile`/
      `default_workspace`, the runtime rows) stay optional under `contracts/README.md`'s "keeps accepting
      older partial projections" rule, and the work-detail/chat-read `WorkItemWaitingDecision` packet stays
      optional because it is carried verbatim from the agent's `waiting_for_input` payload and its options
      are advisory display data. Unconsumed members stay optional and additive: goal-read root
      `project_name`, `updated_at`, `lifecycle`, `history`, `execution`, `planning`, `attention_reasons`;
      the goal work item's planning/assignee fields, which the work surfaces read from their own read's
      definition; and the attention read's entry `blockers`/`settlement_attention`/`updated_at` plus its
      `GoalBlocker` and `SettlementAttention` members, which no surface consumes there.

## Browser acceptance under host load

*Closed part of the item “Re-measure the pre-existing browser suites on a quiesced host …” in `docs/TODO.md`; the open residual stays there.*

      **Fixed product half** (branch `fix/channel-join-read-epoch`). The join is now a read
      epoch: the client's `channel.join()` carries an `ok` hook that performs exactly the read
      an invalidation takes — the unclassified payload's own pass (`realtime.ts:294-296`, the
      `invalidateReads` call `flush` makes for an unclassified frame at `:168`) — issued once
      per completed join. It fires on every rejoin too, because the Phoenix client resends the
      same join push (`Channel.rejoin` -> `joinPush.resend`, whose `reset` keeps the receive
      hooks), which is the same window on a reconnect: the frames published while the channel is
      errored are dropped the same way. The socket-open snapshot read (`:243-250`) stays, polling
      is not reinstated (`syncPolling` still stops on `connected`, `:189-193`), no timer is
      added, and the reconcile is unconditional because nothing on this side can decide it: the
      payload's `cursor` has no store here by design and the join reply carries no position, so
      the epoch event itself justifies the read.
      Evidence on this host, frontend vitest (jsdom), with the new cases run against the
      unpatched bytes and the delivered ones: `src/app/router.test.tsx`'s `channel-join read
      epoch` case drives the real shell and the real `/portal/work/:workItemId` surface over a
      scripted socket whose `publish` drops anything sent before the join (there is no
      subscription to address, as on the server); it opens the transport, posts the run and
      publishes its `run` invalidation inside the window, observes that no read follows (the
      pre-run document still renders, no `Queued` chip), then completes the join. Red on the
      unpatched bytes for exactly that reason (`2 failed | 53 passed (55)`, exit 1: the case
      stops at `router.test.tsx:1173`'s `findByText("Queued")` with the pre-run control bar in
      the DOM dump, and its client-side sibling in `src/services/realtime.test.ts` reports
      `expected "invalidateQueries" to be called 2 times, but got 1 times`); green on the
      delivered bytes. Sabotage: making that reconcile conditional on a received frame
      (`if (pending.length > 0) invalidateReads()`) re-breaks both cases for the same reasons
      (`TestingLibraryElementError: Unable to find an element with the text: Queued`;
      `expected "invalidateQueries" to be called 2 times, but got 1 times`, exit 1), which is
      the point: the read cannot be decided by a frame the client never received, so a
      payload-driven repair is not a repair.
      The same window was checked in the other direction and no second defect was found: while
      the client is not connected the fallback is armed (`syncPolling` stops it only for
      `closed` or `connected`, and `connected` is set in the socket-open hook alone, `:245`), so
      a frame published while the socket is down is covered by a poll tick; a transport
      reconnect always reads (`:248`); and the channel's own rejoin now reconciles through the
      same hook.
      What remains, still not converted: `portal-cutover.spec.mjs:377`'s polling-fallback case,
      whose subject is the fallback itself — its reader's every socket attempt is closed
      (`established === 0`) and it waits `FALLBACK_CADENCE_GUARD_MS` (20 s, four of the client's
      5 s ticks) on the surface. The join epoch does not touch it: a reader that never joins has
      no join to complete. What would show the conversion is an observable mark of a fallback
      pass: the client publishing the read it issued on a tick, the way it already publishes
      its connection status. The case would then wait on that mark plus the read it caused,
      dropping the elapsed bound rather than lengthening it.
      The case that exposed the window (`portal-work-views.spec.mjs:1110`, converted by #58 to
      drive the surface's own refresh control and to wait on the read it serves) is not converted
      back to waiting on the invalidation channel. A browser case does not hold either end of that
      window as a product path: the suite's routed websocket can withhold the join frames, but
      that would make the case's precondition a property of the harness's own timing again, and
      its frame counters (`pushed`, `pushedAt`) can only observe a delivery that happened, never
      the interval between the transport opening and the subscription existing. What would
      justify waiting on the channel there is a page-visible mark of the join epoch — the client
      publishing `subscribed from here`, the way it publishes connection status — after which
      the case could wait on that mark plus the read it causes. The window's deterministic proof
      stays the frontend case above, where both the drop and the join are held.
      **Both remaining cases converted** (branch `feat/visible-refresh-marks`). The product now
      publishes the one fact both cases needed, and it is state an operator reads: the invalidation
      client records every every-read pass it issues — which of its own events caused it, when it
      issued its reads and when those reads finished — and the shell states that record beside its
      connection status (`frontend/src/services/realtime.ts`: `RefreshPass`, `lastRefresh()`, and
      `invalidateReads(source)` writing the record at the same call that issues the reads;
      `frontend/src/app/shell.tsx`: `.reads-status` in the topbar with `data-connection`,
      `data-refresh-source`, `data-refresh-issued-at` and `data-refresh-settled-at`, rendering e.g.
      `Offline, polling · refreshed 14:03:12 (the polling fallback)`). The record is written where
      the work happens — the fallback's own tick, the join's `ok` hook, the socket-open snapshot,
      the tab's return, and the every-read pass an unclassified invalidation takes — so no mark can
      exist without the pass that issues the reads, and only an every-read pass is recorded: a
      change classified to specific keys refreshes those reads alone and cannot speak for the reads
      on screen as a whole. Its completion is the promise `invalidateQueries` returns, so no timer
      was added, and a pass a newer one superseded never records a completion; polling is not
      reinstated (`syncPolling` still stops on `connected`). Frontend coverage: the record's
      sources, its completion, the superseded-pass guard and the classified-change exclusion in
      `src/services/realtime.test.ts`, and the shell's `.reads-status` attributes asserted in
      `src/app/router.test.tsx`'s `channel-join read epoch` case.
      `portal-cutover.spec.mjs`'s polling-fallback case (read at `:338` on this branch) now routes
      the **reader's own** socket (`controlSocket(reader)` with every attempt closed) instead of one
      page's socket standing in for it, and decides on the shell's record plus the state that pass
      served: a fallback pass whose reads finished after the write was requested
      (`data-refresh-source="fallback"`, `settledAt >= createdAt`, both in the reader's own clock)
      and then the new state rendered — no elapsed budget anywhere in the decision.
      `FALLBACK_CADENCE_GUARD_MS` (20 s) is renamed `FALLBACK_READ_GUARD_MS`, its value unchanged
      and its comment now says it is a backstop over a missing pass, never a verdict. The old case
      routed the *writer's* page, so its `established === 0` never spoke for the reader: with the
      reader's socket live the convergence it observed was the push path rather than the fallback
      its note claimed.
      `portal-work-views.spec.mjs`'s disconnected-read case (read at `:1119`) now exercises the
      product's join epoch instead of its own refresh control. The harness holds the browser's own
      connection (`controlSocket(page)` with `hold` set: the page's WebSocket is opened as a mock
      and **no upstream connection is made**, `withheld` counts the bytes that stay with the
      harness, and `release()` connects and replays them in order — the bytes are never parsed, and
      a Phoenix 1.8 frame is a JSON array on the wire, which is why an object-shaped join matcher
      forwarded the join in the first attempt at this hold). The browser therefore is `connected`
      with the server's join clause unrun: the run posts through the API, the server pushes its
      invalidation down a socket with no subscription to address (`pushed === 0`,
      `established === 0` at that moment, `withheld > 0`), and the surface still renders the pre-run
      document — the hosted failure's own observation, now the case's deterministic precondition.
      Releasing the connection then decides on two product facts in order: the shell's record that
      the pass which refreshed the reads is `join` and issued them after the run was published
      (`issuedAt > publishedAt`), and the read that pass caused, served by the server (the detail
      read the case armed before the release). Nothing else can reconcile that surface: the
      fallback is stopped while the transport is open, the frame is gone, and the case dispatches no
      tab return and presses no refresh control. `JOIN_EPOCH_GUARD_MS` (20 s) is a new named
      backstop over the client's ten-second join timeout and its rejoin backoff; it decides nothing.
      The harness hold answers this entry's own earlier objection — that a routed websocket
      withholding join frames only re-makes the precondition timing-dependent — in the only way that
      holds: the hold is set before the shell starts and released by the case after it has observed
      the product's own states, and a hold that failed to apply turns the case red (the surface
      renders the run early), never green.
      Affected suites on the delivered bytes, the render step's own command form
      (`pnpm --filter symmetry-browser-acceptance test tests/portal-cutover.spec.mjs
      tests/portal-work-views.spec.mjs`, both projects, against a `mix phx.server` of this revision
      on a `postgres:18.6` container): the two files, both projects,
      `33 passed`, `1 skipped`, exit 0 (2.9 m), the converted cases green on both: the cutover
      fallback case (desktop 9.6 s, mobile 10.7 s) and the disconnected-read case (desktop 8.0 s,
      mobile 5.2 s). This host cannot boot the packaged release (recorded above), so the runs used
      `mix phx.server` of this revision against a `postgres:18.6` container with
      `SYMMETRY_PORTAL_URL` pointed at it and a private `SYMMETRY_PLAYWRIGHT_OUTPUT` directory —
      the shared results directory is what wedged two earlier attempts at the rehearsal, and the
      private one is what let the suite report at all.
      Gates on the delivered bytes, exit codes: `pnpm --filter @symmetry/frontend typecheck` 0,
      `lint` 0, `test` 0 (18 files, 322 tests), `build` 0.
      Sabotage evidence: first, the fallback tick was made to refresh
      nothing at all (`syncPolling`'s tick body emptied): the converted cutover case failed for its
      own stated reason on both projects (`2 failed`, exit 1: `the fallback pass completed and
      refreshed the reads after the write`, `Received: false`, `Timeout 20000ms exceeded`), while
      the pre-change version of the same case — which routed the *writer's* page and waited on the
      surface — passed the same sabotaged bytes (`2 passed`, exit 0), because its reader's live
      socket delivered the frame. Second, the join's `ok` hook was made to issue nothing
      (`onJoined` emptied): the converted disconnected-read case failed for its own stated reason on
      both projects (`2 failed`, exit 1: `the completed join's pass refreshed the reads`,
      `Received has value: null`, `Timeout 20000ms exceeded`), while the pre-change (#58) version of
      the case passed the same sabotaged bytes (`2 passed`, exit 0) through its own refresh control.
      Both sabotages were reverted and the bundle rebuilt to the delivered asset name
      (`index-BNKze5Le.js`), so the green suite verdict above stands for the restored bytes.

## Open findings from the 0008b model-routing review

*Closed part of the item “Revalidate the open class B finding against `daemon/internal/platform/process_windows.go` …” in `docs/TODO.md`; the open residual stays there.*

      **Fixed** (branch `fix/non-durable-close-proof`). `jobContainment.Close`
      now latches the failed physical stop proof into `job.closeErr` in the
      non-durable `containmentSupervisorStopLease` branch
      (`process_windows.go:554-568`; the `errors.Join(stopErr, ...)` at `:563`)
      instead of caching only the daemon-side handle release, so a repeated
      `Close` through the `!durableAuthority` retry branch (`:497-500`) reports
      the failure instead of returning nil after releasing the Job handle. A
      lease with stop/release capability but no authority keeps no receipt that
      could answer for the proof on a later call, and the proof cannot be
      re-derived once that handle is closed, so latching follows the fail-closed
      convention the sibling `!durableAuthority` branch (`:577-579`) already
      applies to the no-supervisor and non-stop-lease paths. The
      durable-authority path (`:531-537`, `:581-591`) and the partial/legacy
      paths (`:602-613`) are unchanged. A scan of every `job.closeErr`
      assignment in the file found this as its only instance of the class: the
      other sites either join `stopErr` (`:577-579`), latch a supervisor failure
      for a Job that has no handle to prove (`:526-542`), or release the handle
      only with / after a durable receipt (`:581-591`).
      Reproduced through the route this entry names — the native Go-test-binary
      `AttachProcess` route, whose supervisor is the stop/release-capable
      `noopContainmentSupervisorLease` with no durable authority — by
      `TestNonDurableStopLeaseCloseRetainsFailedStopProof`
      (`process_windows_test.go:870-946`): it asserts the no-durable-handoff
      precondition and the capability shape, forces the settlement failure
      through the `waitForEmptyJob` seam, then requires the first, second and
      third `Close()` to report it. Red on the unpatched product (`go test
      -count=1 -run '^TestNonDurableStopLeaseCloseRetainsFailedStopProof$'
      ./internal/platform` → exit 1, `repeated Close() reported success without
      the containment stop proof`, `process_windows_test.go:937`), green with
      the fix (exit 0), and the deliberately re-broken branch fails the same
      assertion at the same line. Final-revision gates: `gofmt -l .` → no
      output (exit 0), `go vet ./...` → exit 0, `go test -count=1
      ./internal/platform` → exit 0 (`ok ... 10.606s`).

## Record and documentation hygiene

*Closed part of the item “Revalidate the runbook claims that are true on this tree but rot …” in `docs/TODO.md`; the open residual stays there.*

      Third pass (the `gpt-6.2-sol` findings, each re-verified against its
      artifact). Closed by restating them against their owners rather than
      re-asserting them: the receipt recipe's cleanup now owns the generated
      database name and refuses an inherited `MIX_TEST_PARTITION` when the
      effective name is derived (`config/test.exs`), and says the guard covers
      the fixture writes only, not create/migrate/drop; the live-E2E recipe names
      `SYMMETRY_LEASE_DURATION_MS=30000` as the terminal-retry case's prerequisite
      and CI's per-case assertion; the request-hash recipe distinguishes the
      release's environment from the production Compose service, which forwards
      neither `SYMMETRY_REQUEST_HASH_WRITE_MODE` nor
      `SYMMETRY_GOALS_ROLLOUT_ENABLED`; the broker paragraph scopes stdin delivery
      to the legacy task path and says native provider access needs a verified
      adapter bridge; the OpenCode recipe labels the Linux-v2 verifier as the
      installed-owner verification path while production registers the
      unavailable adapter; `portal.md` no longer implies a Goal-side board move
      and states the native run's supervisory flags as `false` rather than
      absent; `connections.md` scope its live smoke to synchronization reads; the
      index rule cites the contract's record-the-conflict requirement; and the
      rot-prone restatements (pnpm pin, Claude guard, CI step inventory, cadence
      and poll defaults, GitHub API version, health fields) now name the file that
      owns each value (`package.json`, the smoke script, `ci.yml`,
      `control/config/runtime.exs`, `frontend/src/services/realtime.ts`,
      `integrations/auth.ex`, `workspaces/read_model.ex`). The `README.md`
      documentation index carries the one convention line for the whole `docs/`
      tree.

- [ ] Verified, no change needed: the 0007b repair receipt's source subject
      `sha256:4a28f41446d2a057e355fffcf2f4c06e8c49475867e26ce66038f66a6502256f` (393 source entries) has not
      silently drifted. Recomputing the receipt's own documented algorithm (recursive roots plus the direct
      files in `frontend/`, `browser/` and `contracts/` plus the explicit file list, every `docs/**` path
      excluded; entry `sha256 + two spaces + path + LF`, PowerShell invariant-culture sort) reproduces it
      exactly on the delivered 0007b merge revision `7a92ca4` and on the published branch commit `efe0fdf`, and
      the same script reproduces the receipt's own historical anchors (`01eb134` full-tree `sha256:1e345643…`,
      `efe0fdf` full-tree `sha256:705e559d…`). The tree recomputed at this change's revision differs
      (`sha256:fdf75910…`, 466 entries; 83 moved + 75 added + 2 removed scope paths contributed by later goals),
      which no `docs/**`-only edit can cause and which no receipt claim asserts as current. The command the
      receipt names (`.tmp/0007b-subject-digest.ps1`) was a throwaway, so a delivered tool for that scope would
      need its own increment if a future submission must re-derive it without re-implementing the documented
      algorithm.
      Corroboration recorded without action: an independent cancelled review parsed the browser HTML
      report's embedded results and confirmed the focused 35/1/0 and broad 79/15/0 rows with no flaky cases -
      evidence for the 0007b receipts' browser rows, not a defect.

## Paginated reads must be bound to the cursor that served their pages

- [ ] The work detail's execution history retained the older pages a reader had expanded without
      binding them to the served first-page cursor, so a read that re-served a different boundary
      rendered those pages beside the new first page: `WorkControlSurface` kept them as
      `{ workItemId, entries }` (`frontend/src/features/work/control.tsx`) and `WorkHistory` rendered
      `[...firstPage, ...retained]` (`frontend/src/features/work/history.tsx`), which presents the
      region between the two as contiguous history. A timeline cursor is signed afresh on every GET
      (`Plug.Crypto.encode/2` embeds `signed_at_ms` in the signed term), so the token cannot witness
      that two reads paginate from the same boundary; the entry the cursor's position names can, and
      it is the last entry of the page the read served (`Orchestration.page_with_next/3` takes
      `List.last(page) |> entry_position()`). The attention feed had the same shape: its loaded
      cursor list outlived the served first-page `next_cursor` the list chains from
      (`frontend/src/features/attention/attention.tsx`), so a re-read of the feed served older pages
      of a replaced cursor beside its new first page.

      **Fixed (2026-11-18, `fix/history-cache-cursor-reset`):** `timelineBoundary`
      (`frontend/src/features/work/presentation.ts`) derives that boundary from the entry's own
      `source` and `<source>_id` and returns null when the entry does not carry the id its source
      owns, so an unprovable boundary never binds pages; `WorkControlSurface` retains the pages under
      the boundary the read that fetched them served — or, when an empty first page proves no
      boundary, under the served cursor's own identity — and returns them only while the served read
      is still on that chain, and `WorkHistory` and `WorkReceipts` render that same bounded state;
      `AttentionPage` renders its loaded pages only while the served `next_cursor` equals the cursor
      its chain was expanded from. No timer, poll or automatic read was added: the pages a moved
      boundary retires are loaded again by the reader's own "Load older history" / "Load more Goal
      attention", which continues from the served boundary.

The blocks below were moved on 2026-11-19.

## Model-routing evaluation records

- [ ] Decide whether the four 0008d receipts ever need re-production once the evaluation tooling
      changes again. The TypeScript carrier added gate fields `vitest_tests`/`vitest_files` and the
      derived structured-report cross-check, so `scripts/ci/model-routing-evaluation.py` and its
      regression suite changed after the 0008a/0008b/0008c receipts were produced. Those three
      letters' receipts already refused re-derivation under the 0008c-era bytes (recorded in the
      entry below); the 0008d receipts re-verify with the current bytes only, and any future tooling
      edit invalidates them for submission in exactly the same fail-closed way. Nothing must change
      unless a submission requires re-derivation, in which case the affected runs are re-executed,
      never re-derived by editing a receipt.
      The letter's own runner then changed once before publication (the `vitest_report_detail`
      refusal order that the first full suite pass rejected, described in the closure record), and
      the four receipts were re-derived to the corrected bytes without re-executing a gate or
      re-importing a review, three times: first for the refusal-order fix, then for the cache-predicate
      wiring that the independent reviews asked for, and a third time for a docstring-only change
      (the refusal-order paragraph now names the walk's structural branches). Superseded history:
      runner `sha256:d80e49ca…` then `sha256:3f3b0975…` then `sha256:619f0d37…`, source subject
      `sha256:5abd13ec…` then `sha256:5e1e4e6c…` (both before the subject scope was narrowed to a
      declared set that excludes generated bytecode caches and a worktree `.git` pointer file). The
      published receipts bind the delivered runner `sha256:81504715…` (and the auxiliary tool
      digests); the delivered suite `sha256:dbe1a0fe…` is named in the series receipt's subject
      table rather than bound by a receipt.
      The source subject is now computed by a delivered, reader-runnable tool
      (`python scripts/ci/model-routing-source-subject.py`, scope and entry formula in its
      docstring). Two values are current, each on its own revision: the *evaluated* revision
      (`dacec450` plus this delivery's source paths) is `sha256:e333bb85…` over 1120 source paths,
      and the landing tip after the rebase onto `ca1bdad` (`ca1bdad` plus this delivery, where `main`
      added 21 source paths under `contracts/`, `browser/` and `frontend/`) is
      `sha256:70e03853…` over 1141. History, in the order the run computed it: `sha256:5abd13ec…` (1121 paths, first scope), `sha256:5e1e4e6c…`
      (with the `sha256:3f3b0975…` runner bytes), `sha256:e0976262…` (1123 paths,
      `sha256:619f0d37…` runner) and `sha256:4db67385…` (1118 paths, same runner) - and it was the
      last of those that was published, which a pre-publication audit showed to be stale: it was
      computed before the runner's final docstring edit, so the delivered tree hashed to
      `sha256:245edc15…` (1118 paths, delivered runner `sha256:81504715…`; derived by that audit, and
      a value the run's own records and session transcript do not carry). The repair republished the subject and added the derivation
      test to the suite and the subject tool with its test, which is why the delivered count is two
      paths higher than the tree that value bound.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the decision is recorded in
      `docs/design/quality.md` (Model-cost evaluation, closing paragraph): a receipt is re-produced
      only by re-executing the evaluation under the current tooling and never by editing a receipt,
      an older receipt stays verifiable against the revision that produced it, and the current
      runner refuses it for submission - so the 0008d receipts need re-production only if a
      submission requires it, and then the affected runs are re-executed. This very change moves
      the evaluation runner again, which invalidates the four 0008d receipts for submission under
      the new bytes in exactly that fail-closed way.

- [ ] Decide whether the four 0008c receipts ever need re-production under the current evaluation
      tooling. They bind the produced revision, preserved as the annotated tag
      `0008c-produced-tooling` (commit `cab222b`): all four re-verify with that revision's
      bytes (`git archive 0008c-produced-tooling scripts/ci`, then `accept`, exit 0 each), while
      the post-audit tooling refuses them for submission (`accept` exit 1 at
      `$.evidence.tooling_sources.model-routing-evaluation.py`) — the documented fail-closed
      consequence of changing receipt-bound tooling, labelled in the evidence record with both
      digests. Nothing must change unless a future submission requires receipts produced by the
      current bytes, in which case the runs have to be re-executed, never re-derived by editing a
      receipt.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** same decision as the 0008d
      re-production entry above, now codified in `docs/design/quality.md`: the four 0008c receipts
      stay verifiable against the `0008c-produced-tooling` revision and are refused for submission
      by the current runner by design; if a submission requires current bytes, the runs are
      re-executed, never re-derived by editing a receipt.

- [ ] Decide whether the 0008b receipts need re-production under the current shared runner. They bind
      `scripts/ci/model-routing-evaluation.py` as produced —
      `sha256:9a9ff8eb65bda21e584c5fef9642f86569b46c8c8da16d3e216443e702425c56` — while `main`'s #42
      extended that runner for the Elixir carrier
      (`sha256:8ae5b2b6ba961924f7702aa4181e8488913b4d63171922bf1cf760e62142db50`), so the current bytes
      refuse the 0008b records for submission at `$.evidence.tooling_sources.model-routing-evaluation.py`,
      exactly as they refuse the committed 0008c receipts (see the model-routing entry above). The
      producing bytes stay reachable from the annotated tag `0008b-produced-tooling` (commit `253a701`,
      the head this branch was rebased from, tagged and pushed so the reference survives the rebase), and
      the post-rebase audit recomputation reports exactly those four
      tooling-digest mismatches and nothing else (`checks: 99, failures: 4`). Re-execute the runs if a
      future submission requires receipts produced by the current bytes; never re-derive by editing a
      receipt.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the same question as the other two
      re-production entries, answered by the same `docs/design/quality.md` paragraph: the 0008b
      receipts stay verifiable against the revision tagged `0008b-produced-tooling` and fail closed
      on submission under the current runner by design; a submission that requires current bytes
      re-executes the runs.
