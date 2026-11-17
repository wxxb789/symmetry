# TODO

Manual tasks and deferred maintenance discovered during scoped changes.

## Model-routing evaluation records

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

- [ ] Decide whether the 0008c evaluation records keep their raw host paths. The four committed
      receipts (`docs/evaluations/model-routing/0008c-{a-mixed,a-strong,b-mixed,b-strong}.json`,
      `environment.measured.mix_build_path` and `mix_deps_path`) and the committed copy of
      `docs/evidence/goal-0008c-elixir-model-routing-2026-11-17.md` state the operator's absolute
      temp root (the `symmetry-0008c-eval` run store under the local user profile). They are produced,
      committed evidence bound by digests other records name, so the 0008c audit recorded them as
      an open item instead of rewriting history. The runner's sanitisation extension
      (`validate_measured_mapping(..., redact=True)`) now records every host-specific absolute path
      in a measured value as a `sha256:` token, so run records and receipts written from now on
      cannot republish a disposable root or the operator profile. Revalidate before any scrub, and
      never edit a receipt whose digests other records bind.

## SEO / repository metadata

- [ ] Upload `docs/assets/symmetry-social-preview.png` as the GitHub social
      preview. GitHub has no REST/GraphQL API for this: open
      **Settings → Social preview → Edit → Upload an image**, then confirm the
      card renders on the repository page. The image is 1280×640 and mirrors the
      README banner (`docs/assets/symmetry-banner.png`).

## Dead-code candidates

- [ ] Revalidate and remove the unused `JsonDocument` alias in
      `control/lib/symmetry_control/goals/schemas.ex:1431`
      (`SymmetryControl.Goals.GoalExternalWait`). The 0006h focused Control run
      reported the unused alias; a scoped search of that module found only its
      alias declaration. Leave removal and verification to separate maintenance.

- [ ] Revalidate the unused `replay_task` binding at
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:1059`
      and `task` bindings at lines `2079` and `2170`. The 0007a `mix test` run
      reported these compiler warnings (`.tmp/0007a-session/control-test.log`).
      Preserve the fixture calls and leave any binding cleanup to separate
      maintenance.
      The 0009 full Control run reports the same bindings at lines `1351`,
      `2450` and `2541` in `.tmp/0009/control-full.log`; these are current
      coordinates, not additional cleanup work.

- [ ] Revalidate the unused default `kind \\ "omp"` on private
      `register_unverified_omp_runtime/2` in
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:3651`.
      The 0009 full Control run reports that the optional default is never used
      (`.tmp/0009/control-full.log`). Preserve the runtime fixture behavior and
      leave cleanup and verification to separate maintenance.
- `docs/goals/README.md` index row for 0006h targets `0006h-credentialed-provider-accounting.md` while the file lives at `docs/goals/archive/0006h-credentialed-provider-accounting.md` (pre-existing dead link; disclosed in the 0006-final series-review receipt F8). Revalidate and fix in a separate maintenance change.

- [ ] Revalidate and remove the unused `Connection.statuses/0` accessor at
      `control/lib/symmetry_control/integrations/schemas.ex:92`. A scoped search for
      `Connection.statuses` and `Integrations.Connection.statuses` in `control/lib` and
      `control/test` found no caller; the changesets read the `@statuses` module attribute
      directly. Observed while fixing goal 0007d. Leave removal and verification to separate
      maintenance.
- [ ] Investigate the intermittent Linux failure `TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`
      in `daemon/internal/platform` (`containment_supervisor_linux_test.go:1128: decode
      wrong-session helper witness: unexpected end of JSON input`). Observed in run
      43724904655 job 132844651104 on a workflow-only diff, then green on rerun job
      132846453648. Revalidate whether the helper witness can be decoded before it is fully
      written; keep the witness assertion itself and leave the fix to separate maintenance.
- [ ] Investigate the intermittent Windows failure `TestCloseStopsOwnedChildAfterLeaderExit`
      in `daemon/internal/platform` (`process_windows_test.go:1004`, "owned child <pid>
      survived Close()"). Observed in run 43720680428 job 132831780245 on a docs-only diff,
      green in run 43715830442 and on rerun job 132834151127. Revalidate the close/settle
      window; keep the survived-child assertion.
- [ ] Investigate the intermittent Control failure `reversed import and delivery updates
      lock WorkItems in UUID order without deadlocking` in
      `control/test/symmetry_control/integrations_test.exs:1161` (`assert
      String.contains?(query, "\"work_items\"")` inside `assert_sync_waiting_on_work_item/3`,
      with Postgrex connections disconnecting in the same log). Observed in run 43891279765
      job 133383740181 on the 0008a PR (`839/840 passed, 21 skipped`), green on rerun of the
      same job (that run then completed successfully). Revalidate whether the captured SQL can
      be asserted before the sync query is issued; keep the assertion itself and leave the fix
      to separate maintenance.
- [ ] Record the first hosted docs-only sample (run id, job count, wall-clock) in
      `docs/evidence/ci-process-trim-2026-11-15.md`, whose measurements section currently
      states the hosted after-run and the docs-only sample are unmeasured. Cross-check the
      classifier's skip list against the jobs that actually ran before treating the trim as
      fully accepted.

- [ ] Revalidate the unreachable resource synchronization states. `project_resources` has a
      `project_resources_sync_status_check` constraint admitting `syncing` and `stale`, and
      `SymmetryControl.Workspaces.ReadModel` maps them to `syncing`/`attention` labels, but no
      writer sets either value (`Integrations.persist_sync/4`,
      `Integrations.record_resource_failure/3` and the connection/capability resets in
      `Integrations.update_connection/2` and `Workspaces.update_resource/2` only write
      `unknown`, `synced` and `failed`). Observed while implementing goal 0007d; keep the
      constraint and labels, and decide in a separate maintenance change whether the states
      need a writer or the vocabulary needs a migration.

- [ ] Align the frontend `return_to` allowlist with the server's per-path query rules, or record
      the asymmetry deliberately. `frontend/src/app/application.ts` (`safeReturnTo`) accepts any
      query string on `/portal/attention|connections|projects/:id|work/:id` and any characters in
      the project id, while `control/lib/symmetry_control_web/portal_session.ex` validates each
      path's query against `projectId`, `connectionId` or the work-search patterns and narrows a
      project id to `[A-Za-z0-9_-]+`. The server stays the authority, so the worst case is a deep
      link falling back to `/portal` rather than an open redirect; observed while completing the
      0007d rebase union (0007b's client filter and 0007d's server rule met for the first time).
      Decide whether the client should narrow its filter or the server should accept the extra
      params, and if the client is narrowed, add a test that keeps the two lists aligned.

- [ ] Investigate the intermittent browser failure `project, resource, work-item,
      filtering, ordering, and persistence` in `browser/tests/portal.spec.mjs`: after the
      detach confirmation, `#toast` still showed "Resource updated" instead of "Resource
      detached" (desktop-chromium). Observed once while implementing goal 0007d
      (`portal.spec.mjs` + `workspace-shell.spec.mjs`, 40 cases, 30 passed / 1 failed /
      9 skipped) with two Control compiles running on the same host; the same test passed
      twice alone on the same revision (18.2 s and 17.3 s, exit 0). The classic bundle
      `control/priv/portal_assets/portal.js` and the resource routes are unmodified by
      0007d. Revalidate the classic toast/refresh ordering; keep the assertion.

- [ ] Investigate the environment-limited Windows command test. `CommandTest` "provider
      token commands can keep stderr diagnostics out of stdout" fails with
      `{:error, {:command_unavailable, "pwsh"}}` on hosts without a working `pwsh`
      (observed while implementing goal 0007d: the full control suite reported 828/829
      passed, 21 skipped, 1 failed on a clean dedicated test database; the current tree
      (control identical to `bbc9b6f`) reports 844/845 passed with the same single failure).
      `System.find_executable("pwsh")` returns `nil` here while `powershell.exe` resolves,
      and putting the `WindowsApps` store alias on PATH makes two cases in that file fail
      instead of one. `Command.run/3` and the test itself are untouched by 0007d.
      Revalidate the Windows shell resolution; keep the assertion.

- [ ] Keep `mix test` off the portal acceptance database. With `POSTGRES_DB` pointing at
      the 0007d fixture database, 10 control tests fail because committed fixture rows are
      visible to suite-level aggregates (`sync_all_resources/0`, workspace and portal
      controller reads): 819/829 passed / 10 failed there versus 828/829 passed / 1 failed
      on a clean dedicated test database. The browser harness recreates its own database;
      the suite should use the repository default (`symmetry_control_test`).

- [ ] Revalidate the load-sensitive `portal.spec.mjs` desktop cases. In the 0007d fix round's
      legacy run (two fixture servers on one host, 31 passed / 2 failed / 7 skipped), "detail
      output encodes untrusted statuses and rejects unsafe links" timed out on its project-create
      toast and "two browser contexts reject a stale project update" hit the 60 s test timeout;
      both pass alone on the same revision on a quiet host (the stale-update case `ok` in 5.0 s),
      and neither surface is touched by the fix diff. Keep the assertions; re-test on a quiet host
      before changing any budget.

- [ ] Investigate the mobile-chromium classic case `project, resource, work-item, filtering,
      ordering, and persistence` in `browser/tests/portal.spec.mjs`. After saving the work-item
      edit dialog, `#toast` stayed hidden with the stale text "Work item created" for the whole
      10 s expect window, while the update itself persisted: the failure snapshot's detail drawer
      shows the new pull request URL, CI `passed` and review `required` that the test set. Observed
      while completing 0007e in the first legacy run (`portal-work-views.spec.mjs` +
      `portal.spec.mjs` + `workspace-shell.spec.mjs` on both widths, 43 passed / 4 failed /
      5 skipped) and reproduced alone on the same revision with the desktop case passing (mobile
      `x` in 17.0 s, desktop `ok` in 12.1 s); it then passed in the second run of the same suites on
      the unchanged classic files (46 passed / 1 failed / 5 skipped), so the mobile case is
      intermittent. The classic route is
      unchanged by 0007e: `control/lib`, `control/priv/portal_assets`,
      `control/lib/symmetry_control_web/portal_html` and `portal_controller.ex` have no diff
      against the base revision. Revalidate the classic toast branch that reports an update when
      the edit dialog was opened from the detail drawer; keep the assertion.

- [ ] Make the Control suite's dependence on the staged portal assets explicit. Four
      `PortalControllerTest` cases ("a missing built entry returns an explicit uncacheable 503…",
      "the portal requires an operator session…", "every explicit shell deep link…", "built hashed
      assets are public and immutable…") fail when `control/priv/static/portal/index.html` is not
      staged, with `Workspace assets are unavailable. Build and deploy the frontend assets.` and a
      `File.RenameError` on the same path. Observed while completing 0007e: the 0007e full-suite run
      started before `pnpm --filter @symmetry/frontend build` staged the assets and reported
      851/856 passed with those four failures; the same suite after staging reports 855/856 (the one
      remaining failure is the environment-limited `CommandTest` above). The build output is
      gitignored, so a fresh worktree has it only after a build. Decide whether the suite should
      skip those cases with an explicit reason, stage a fixture bundle, or document the build as a
      suite prerequisite; keep the assertions.

- [ ] Revalidate the load-sensitive Go test
      `TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock` in
      `daemon/internal/harness/omp/adapter_final_message_test.go`. It failed with
      `adapter_final_message_test.go:44: context deadline exceeded` in the 0007e `go test ./...` run
      while 20 Go packages and the full Control suite ran concurrently, and passed alone on the same
      revision (`ok … 10.609s`, exit 0). The test itself sets a 1 s `context.WithTimeout` around
      `WaitTurn`, so host load alone can trip it. Keep the assertion; consider a load-tolerant
      deadline rather than a longer test timeout.

- [ ] Revalidate the `capability_not_granted` guidance entry on the resource surface. The new
      `refusalGuidance` key in `frontend/src/features/connections/resources.tsx` is only reachable if
      the server answers that code, but a sync refused for an ungranted capability maps to
      `failure(:capability_not_granted)` → `forbidden` on the wire, so the key is currently dead.
      Either the server should serve the more specific code or the client entry should be removed;
      decide in a separate maintenance change with a test that pins the served code.

- [ ] Investigate the intermittent Linux failure
      `TestProcessGroupLeaderExitAfterSuccessfulDescendantScanRemainsProven` in
      `daemon/internal/platform` (`process_unix_linux_test.go:2119`, `monitor terminal reason =
      owner_stop, want leader_absent`). Observed in run 43898893639 job 133409923040 under
      `go test -race ./...` on the 0007d head, green on rerun of the same job. Revalidate the
      leader-exit/owner-stop ordering window; keep the terminal-reason assertion.

- [ ] Investigate the intermittent `-race` package timeout in `daemon/internal/app`. Observed in run
      43905338877 job 133432721943 as `panic: test timed out after 10m0s` with
      `FAIL github.com/wxxb789/symmetry/daemon/internal/app 600.061s` on the 0008a head, where the
      same package had taken 365 s earlier on the same pull request and every other package passed;
      green on rerun. This is the same loaded-runner family as the Windows `internal/workspace`
      180 s package timeout: decide whether the package needs a longer explicit budget or fewer
      load-sensitive waits, and keep the tests themselves.

- [ ] Remove or justify the unreferenced CSS rules `.chip-failed` and `.chip-pending` in
      `frontend/src/styles.css:170-171`. A sweep of all 137 class selectors in that file against
      `frontend/src` found these two unreferenced already at `7a92ca4` (untouched by the 0007b,
      0007c and 0007d work), so they are pre-existing dead weight rather than a merge artifact.
      Revalidate before deleting and leave removal to separate maintenance.

## Deferred review findings (0007f)

- [ ] Give the focused render gate's `browser/tests/portal-goal-managed.spec.mjs` case its fixture
      environment in CI. The case writes its real Goal-managed row through
      `scripts/ci/portal-planning-fixture.exs`, so it needs `POSTGRES_*` naming the database the
      release serves plus the Elixir toolchain; `.github/workflows/ci.yml`'s render job has neither,
      so the case reports an environment-named skip there. Either add the setup-beam and
      `mix deps.get`/compile steps to that job, or run the case from a harness job that owns them.
      The local focused-gate run in the 0007f review round executes it for real.

- [ ] Revalidate the `/portal/connections?projectId=…` session return target.
      `control/lib/symmetry_control_web/portal_session.ex` accepts only `connectionId=…` for the
      connections shell path while the shell's own navigation writes `?projectId=…`, so a session
      expiry collapses that page to `/portal`. The identical gap on `/portal/runtimes?projectId=…`
      was fixed by the 0007f review round (`shell_query?("/portal/runtimes", …)`); extend the
      connections clause and its accept/reject tests in a separate change.

- [ ] Revalidate the pre-existing capability-vs-state cancel/retry policy in
      `control/lib/symmetry_control_web/portal_json.ex` (`execution_summary/1`): `can_cancel` and
      `can_retry` are derived from task state only, while adapters advertise per-operation support,
      so a runtime advertising `cancel: false` still shows a live Cancel control until the server
      refuses it. Either bind the projection to the advertised support or record the state-only
      policy as intended. Named as a limitation by the 0007f review round; out of that diff's
      scope.

## Deferred review findings (0007g)

- [ ] Give the work detail an entry point into a run's conversation. The classic workspace drawer
      offered "Open Chat" and "Review decision in Chat" for a work item whose execution carries a
      `run_id`; the migrated `/portal/chat` is reachable from the shell's Chat entry (whose run
      picker lists the served runs) but the React work detail added by 0007f has no such link, so a
      waiting decision is only reachable by picking the run in Chat. Adding the link is work-detail
      surface work (0007f's file) and was outside the 0007g diff; `browser/tests/portal.spec.mjs`
      keeps the classic drawer's own buttons until the 0007i cutover.

- [ ] Redirect the documented `#chat` hash location to `/portal/chat`. The frontend design fixes
      "old `/portal#chat` … redirect once to the new equivalent URL", and
      `frontend/src/app/legacy.ts` still maps the fragment to `/portal/classic#chat` (the classic
      surface's own in-page entry points write that hash). The one-time hash mapping belongs to the
      0007i cutover, which retires the classic writable UI; changing it earlier would split the
      classic page's own navigation from its hash.

## Deferred review findings (0007h)

- [ ] Harden the optional-but-consumed members of the served reads so a rename cannot pass every gate
      silently. The 0007h review confirmed these names are correct today but that the schemas leave them
      optional while the client reads them: `accounting.known_usage_cost_microusd`
      (`frontend/src/features/goals/detail.tsx:491`, served unconditionally at
      `control/lib/symmetry_control/goals/read_model.ex:2539`), `settlement_attention[].proposed_next_action`
      and `proposed_next_action_status` (`detail.tsx:245`), `external_waits[].external_ref`, `next_check_at`
      and `check_seq` (`detail.tsx:508`), the attention `attention_reasons`/`active_tasks`/`nonterminal_tasks`
      sets (`frontend/src/features/attention/attention.tsx:80`), and `accepted_outcomes[].candidate_subject`
      and `subject_hash` (`detail.tsx:417`). Marking them `required` — as `integration`, `satisfied?` and the
      unknown-cost member now are — would close the class; additive members staying unvalidated is intended.
      Two smaller residuals from the same review live here too: the rollout-off browser case skips in hosted
      CI because it needs `SYMMETRY_GOALS_OFF_URL`, and the retained `:goal_admission_disabled` inline branch
      is unreachable in production because the attention read is not admission-gated.

## CI consolidation owner decisions (2026-11-17)

- [ ] Decide whether `linux-native-v2-acceptance.yml` is dispatched against a current subject or its gap
      is recorded as permanent. The workflow (`419219121`) has **zero** recorded dispatches since it was
      split out of `ci.yml`: `gh api repos/wxxb789/symmetry/actions/workflows/419219121/runs` reports
      `total_count = 0`, while the in-CI job it replaced ran historically (for example run
      `43673581776`). A skipped qualification is never a passed predicate, so no native acceptance has
      executed since the split and none may be cited as evidence for a revision in that window. The owner
      must either dispatch it against a current subject or record that no acceptance has happened since
      the split.

- `scripts/audit-no-legacy-routes.sh` no longer enumerates its surfaces. It derives every audited file
  from `git ls-files` (binary and asset extensions excepted) and names its three intentional
  exclusions, so `.github/workflows/linux-native-v2-acceptance.yml` and the composite action
  `.github/actions/prepare-pi-release/action.yml` are covered without further maintenance.
  `scripts/ci/test-no-legacy-routes-audit.py` reproduces the placements the enumerated list missed.
  The job itself stays ungated, as the 2026-11-17 review decided.
- `scripts/ci/0007c-subject-digest.ps1` was deleted with the `chore/fence-replacements` audit: it
  prints a digest, compares nothing and cannot fail, and no job or script invoked it. The recorded
  digests in `docs/evidence/goal-0007c-safe-work-planning-2026-11-16.md` name it as the algorithm of
  record, so that receipt can no longer be recomputed from the tree; the file remains in git history.

## Browser acceptance under host load

- [ ] Re-measure the pre-existing browser suites on a quiesced host and take their verdicts off
      wall-clock budgets. Clause 8's re-run exposed the class: six full runs of the three
      pre-existing suites `tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs
      tests/portal-execution-control.spec.mjs` (~48 cases) produced `45 passed`, `3 skipped` twice (in
      4.7 m and 7.9 m) and `43`, `42`, `43` and `39` passed with 2–6 failures and the same
      `3 skipped` in 9.2 m, 10.0 m, 10.9 m and 21.9 m. The failing set is not stable either:
      the list/board case fails at desktop width in three runs and its mobile sibling in another, the
      two deep-link cases in others, and the five mobile failures of the 21.9 m run all pass
      `5 passed`, exit 0, in 45.5 s in isolation on those same bytes. A revision therefore has two
      available verdicts from this suite and which one a run produces tracks how saturated the host is
      (other sessions' servers, browsers and a concurrently running suite share it), not the
      revision — which is why this is a class and not a one-off: every one of these suites decides
      its verdict from wall-clock waits sized for an idle host (`browser/playwright.config.mjs`:
      `timeout: 60_000`, `expect.timeout: 30_000`), so any revision can be reported red without a
      code change, and neither a red full run nor a green isolation pass can be read as the surface's
      own property. The class is not confined to the pre-existing suites: a render wait in this
      delivery's own deterministic suite timed out once under load too, and the same load exposed a
      real defect in it — a restored `scrollTop` observed as 0 in its round-trip and scope-switch
      cases — which this delivery repairs (`frontend/src/features/chat/chat.tsx`: a browser pinning
      `scrollTop` at the pane's maximum under a replaced transcript is no longer recorded as the
      reader's own move), with the regression test, the falsification and the re-measured suite rows
      in the receipt; the timeout half is this class and stays open. Hosted CI does not reproduce it
      because the job is not saturated the same way — the runner gives the suite a dedicated machine —
      so a green hosted run neither explains nor retires a red local run, and a local failure must not
      be cited as a regression without the isolation pass beside it. The fix belongs to the
      observations (waits that end when the served response or the rendered state ends, instead of a
      load-sized maximum) or to a serialized suite on a freed host; either way it must be measured in
      both directions and name the bytes it was measured on.
