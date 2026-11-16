# TODO

Manual tasks and deferred maintenance discovered during scoped changes.

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
      passed, 21 skipped, 1 failed on a clean dedicated test database).
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
