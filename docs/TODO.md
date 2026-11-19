# TODO

Manual tasks and deferred maintenance discovered during scoped changes.

## Rules

- Done items move to `docs/archive/CLOSED-TODO.md`; this file holds only undone or in-progress items.
- When a closed part and an open residual share an item, the closed part moves to the archive and the residual stays here with a line saying where it went.
- Entries in `docs/archive/CLOSED-TODO.md` older than six months may be deleted.

A bullet that carries no `- [ ]`/`- [x]` marker is a decision note, not a tracked item: it stays
where it is and is neither moved to `docs/archive/CLOSED-TODO.md` nor deleted with it.

## Model-routing evaluation records

- [ ] Bound the Windows lingering reap in `scripts/ci/model-routing-evaluation.py`. In
      `run_owned_command` (~1738-1768) a timed-out gate is tree-killed and then reaped with
      `process.communicate(timeout=PROCESS_TREE_REAP_SECONDS)`, which on Windows can keep waiting
      on the gate's stdout/stderr pipes while a descendant the snapshot tree kill missed still
      holds the inherited write ends - past the documented reap window and past any caller that
      expects `check` to return once the kill finished (the timeout-tree fixture's survivor
      witness was masked this way until the fixture spawned its descendant with `DEVNULL`).
      Fix direction: close the read ends from a daemon thread, or leave the final handle close to
      process exit, so a pipe-holding survivor can never extend the reap; add a regression case
      with a pipe-holding survivor. Not edited here because PR #79 rewrites this file.

- [ ] Own the record-cleanliness ratchet baseline: 26 unchanged committed records under
      `docs/evaluations/**` and `docs/evidence/**` still carry a host absolute path, so the
      `record-cleanliness` CI job - which checks only the records a revision range adds or
      modifies - fails the moment a later change touches one of them. Enumerated at `5d6c858`
      with `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` over
      `git ls-files docs/evaluations docs/evidence`: the four
      `docs/evaluations/model-routing/0008b-go-bounded-regression-{mixed,strong}{,.review}.json`
      receipts; `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`,
      `goal-0006-final-series-review-2026-11-15.md`,
      `goal-0006i-exact-subject-release-acceptance-2026-11-15.md`,
      `goal-0006j-omp-native-capability-2026-11-14.md`, `goal-0007b-readable-work-views-2026-11-15.md`,
      `goal-0007i-portal-cutover-series-receipt-2026-11-17.md`,
      `goal-0009-source-identity-2026-11-16.json`; the seven `goal-0006b-*` files
      (`acceptance-environment-audit`, `acceptance-source-audit`, `convergence-receipt`,
      `fixture-inventory`, `resumed-go-gates` and `surface-qa` of 2026-09-29, and
      `iteration-closure-2026-10-10`); the two `pi-control-e2e-*-2026-09-13.md` files, the five
      `test-ablation-*-2026-09-29.md` files and `windows-opencode-ci-gate-2026-09-14.md`. Each one
      needs either an explicit decision to leave it as produced or the render-and-rebind pass
      (re-render the text and keep every digest another record quotes consistent - of this list,
      `test-ablation-go-probe-experiment-2026-09-29.md` is the one quoted by another record). Until
      that decision, a change that merely touches one of these records fails the job by design, and
      the remedy is that render pass, never an edit that breaks a binding.

## SEO / repository metadata

- [ ] Upload `docs/assets/symmetry-social-preview.png` as the GitHub social
      preview. GitHub has no REST/GraphQL API for this: open
      **Settings → Social preview → Edit → Upload an image**, then confirm the
      card renders on the repository page. The image is 1280×640 and mirrors the
      README banner (`docs/assets/symmetry-banner.png`).

## Dead-code candidates

- `docs/goals/README.md` index row for 0006h targets `0006h-credentialed-provider-accounting.md` while the file lives at `docs/goals/archive/0006h-credentialed-provider-accounting.md` (pre-existing dead link; disclosed in the 0006-final series-review receipt F8). Revalidate and fix in a separate maintenance change.

- [ ] Investigate the intermittent Control failure `reversed import and delivery updates
      lock WorkItems in UUID order without deadlocking` in
      `control/test/symmetry_control/integrations_test.exs:1161` (`assert
      String.contains?(query, "\"work_items\"")` inside `assert_sync_waiting_on_work_item/3`,
      with Postgrex connections disconnecting in the same log). Observed in run 43891279765
      job 133383740181 on the 0008a PR (`839/840 passed, 21 skipped`), green on rerun of the
      same job (that run then completed successfully). Revalidate whether the captured SQL can
      be asserted before the sync query is issued; keep the assertion itself and leave the fix
      to separate maintenance.

      Update (2026-11-19, branch `fix/flake-control-reversed-import`): the helper now reads the
      wait from the lock manager instead of the statement text - the failed attempt-1 log names
      the repository lookup in `pg_stat_activity.query` while the import was in a
      delivery-attributed lock wait - accepts a sample only once the import's backend holds an
      ungranted lock and the delivery backend blocks it, and asserts on the accepted sample
      that the import's ungranted lock waits on the delivery backend's transaction
      (`pg_locks.transactionid = (SELECT backend_xid FROM pg_stat_activity WHERE pid = delivery)`)
      while the delivery backend holds a granted `work_items` lock; the raw sample (query, wait
      event, all three lock facts) is printed on deadline and in the accepted-path assertion.
      The waiter-bound xid is the discriminating fact: the `work_items` relation fact alone is
      invariant (a paused delivery always holds those locks), so it cannot separate the
      hypotheses - (a) `pg_stat_activity.query` is not the waiting statement for this backend at
      sample time (prepared-statement/activity reporting under load) while the lock wait is
      real, or (b) a transient pre-sync lock wait. Not reproduced locally (three pre-fix runs
      with seed 310068 left the target test green), so the entry stays open; a recurrence now
      shows the importer's wait event, whether it waited on the delivery transaction, and
      whether the delivery backend held `work_items`.

- [ ] Record the first hosted docs-only sample (run id, job count, wall-clock) in
      `docs/evidence/ci-process-trim-2026-11-15.md`, whose measurements section currently
      states the hosted after-run and the docs-only sample are unmeasured. Cross-check the
      classifier's skip list against the jobs that actually ran before treating the trim as
      fully accepted.

## Deferred review findings (0007f)

- [ ] Give the focused render gate's `browser/tests/portal-goal-managed.spec.mjs` case its fixture
      environment in CI. The case writes its real Goal-managed row through
      `scripts/ci/portal-planning-fixture.exs`, so it needs `POSTGRES_*` naming the database the
      release serves plus the Elixir toolchain; `.github/workflows/ci.yml`'s render job has neither,
      so the case reports an environment-named skip there. Either add the setup-beam and
      `mix deps.get`/compile steps to that job, or run the case from a harness job that owns them.
      The local focused-gate run in the 0007f review round executes it for real.

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
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      Remaining open: the two residuals above stay open; the legacy surfaces that read workspace-read
      members still render their own fallbacks instead of refusing, which is per-surface work outside this
      class; and the captures for `accepted_outcomes`, `settlement_attention` and `external_waits` are
      empty arrays, so those members are pinned by the boundary test and the real-served controller gate
      rather than by a fixture-level rename probe.

## CI consolidation owner decisions (2026-11-17)

- [ ] Decide whether `linux-native-v2-acceptance.yml` is dispatched against a current subject or its gap
      is recorded as permanent. The workflow (`419219121`) has **zero** recorded dispatches since it was
      split out of `ci.yml`: `gh api repos/wxxb789/symmetry/actions/workflows/419219121/runs` reports
      `total_count = 0`, while the in-CI job it replaced ran historically (for example run
      `43673581776`). A skipped qualification is never a passed predicate, so no native acceptance has
      executed since the split and none may be cited as evidence for a revision in that window. The owner
      must either dispatch it against a current subject or record that no acceptance has happened since
      the split.

    **Operator instruction (dispatch branch, exact).** The trigger is dispatch-only by design: the
    workflow's only event is `workflow_dispatch` (`.github/workflows/linux-native-v2-acceptance.yml:3-6`),
    its job is guarded `if: github.event_name == 'workflow_dispatch'` (`:14`), and
    `scripts/ci/linux-v2-native-acceptance.sh:46` refuses every other event — `credentialed Linux
    acceptance requires explicit workflow_dispatch` — so nothing else can run it: the file declares no
    push, pull_request, schedule or `workflow_call` trigger, and `ci.yml` references it nowhere and holds
    no `actions: write`. The entry point is workflow **"Linux native v2 acceptance"**
    (`.github/workflows/linux-native-v2-acceptance.yml`, id `419219121`). Command, run with an
    authenticated `gh` — `gh workflow run linux-native-v2-acceptance.yml --repo wxxb789/symmetry --ref main`.
    `--ref` names a branch or tag, not a bare SHA: use `main`, or another ref the Environment admits whose
    tip is the exact subject; the harness requires `GITHUB_SHA` to be a full commit SHA and records
    `GITHUB_REF`/`GITHUB_SHA` in its output. The run then pauses in the `linux-v2-native-acceptance`
    Environment until its required reviewer (`@wxxb789`) approves, before the job's steps start.

    **Preconditions the steps assume** (recorded; the remote ones are not enforced by YAML): a GitHub-hosted
    `ubuntu-latest` x86_64 (amd64-only) runner — no self-hosted runners are registered — that has `systemd`
    as PID 1 and unified cgroup v2 (`/run/systemd/system`, `/sys/fs/cgroup/cgroup.controllers`), a
    stop-provable delegated transient unit, `sudo -n`, `systemd-nspawn`, `debootstrap`, `setfacl`, `gh`, a C
    toolchain and `jq` (the workflow installs `acl build-essential curl debootstrap gh jq python3 sudo
    systemd-container`); pinned tooling Go 1.27.0, Elixir 1.20.4/OTP 29.0.6 and the `postgres:18.6-alpine`
    service; the protected Environment `linux-v2-native-acceptance` with its required reviewer and branch
    policy, which the workflow comment requires verifying before dispatch ("Before dispatch, verify the
    Environment requires @wxxb789's separate approval, permits self-review, and admits only the reviewed
    0006b branch"); and four configured inputs: the secrets `SYMMETRY_LINUX_V2_TEST_GITHUB_OAUTH_TOKEN`
    (must be `gho_*`; the hosted `GITHUB_TOKEN` is refused) and `SYMMETRY_LINUX_V2_TEST_OPENAI_API_KEY`,
    and the variables `SYMMETRY_LINUX_V2_TEST_PI_MODEL` and `SYMMETRY_LINUX_V2_TEST_OPENCODE_MODEL`.
    Recorded status, still limiting (2026-11-15): the Environment policy read allowed branches `main` and
    `codex/goal-0006b-crash-safe-process-containment` with `prevent_self_review=false`; and no environment
    available here yet satisfies the host prerequisite — the last hosted attempt (run `43673581776`) failed
    the probe with `unified_cgroup_v2_mounted=false` and the local WSL2 attempt failed at
    `transient_unit_cleanup`, so a host that does not qualify fails at the probe before any case set runs.

    **When it must be dispatched.** At release acceptance, against the exact subject, and never cited as
    passed before it runs. The goals' pending predicate, quoted from `docs/goals/README.md`: "the operator
    must dispatch workflow "Linux native v2 acceptance"
    (`.github/workflows/linux-native-v2-acceptance.yml`) on `main` at `8ded771` to run the
    environment-gated `linux-native-v2-acceptance` job on the unchanged Subject tree"; the 0006i plan
    (`docs/plans/2026-11-15-0006i-exact-subject-release-acceptance-plan.md`) adds that it "must be
    dispatched by the operator on the RC head and is recorded as **pending operator dispatch** with the
    exact workflow and ref — never as passed." Caveat of record: no remote branch or tag points at
    the frozen subject `8ded771` and dispatching a ref runs that ref's tip and workflow
    bytes (`docs/evidence/ci-process-trim-2026-11-15.md`), so an exact-historical-subject dispatch needs
    an approved branch or tag whose tip is that subject.

    **Stale coordinates, documented rather than rewritten.** The 0006i plan and its receipt name the
    pre-split location — the `linux-native-v2-acceptance` job inside workflow "CI", cited at
    `.github/workflows/ci.yml:14-16` — and the goals row did too before this correction; after the
    2026-11-15 split `ci.yml` contains no such job. Those historical files keep their bytes; the live
    `docs/goals/README.md` row now names this workflow and file, and
    `docs/evidence/ci-process-trim-2026-11-15.md` already records the current entry point
    (`Linux native v2 acceptance`, `linux-native-v2-acceptance.yml`, not manual dispatch of `CI`).

    **What a dispatch alone produces.** The installed A/B native-owner witness case sets — `shared`,
    `synthetic_goal_cleanup`, `stage_replay_create_started`, `stage_replay_leaf_bound`,
    `stage_replay_launcher_bound`, `ack_loss_launcher_bound`, `stale_leaf`, `migration`,
    `stale_journal_run_key`, `stale_journal_run_key_legacy`, `stale_journal_fence`, `stale_journal_subject`
    — plus the host-prerequisite probe JSON artifact; in CI they run only through
    `scripts/ci/linux-v2-native-acceptance.sh`, which only this workflow invokes. Nothing in `ci.yml` sets
    the gating variables `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS`
    (`daemon/e2e/production_linux_v2_native_witness_test.go`) or `SYMMETRY_LINUX_OWNER_TEST_SCRATCH`
    (`daemon/native/linux-gate/protocol/`, e.g. `launcher_integration_linux_test.go`), so those witnesses
    skip in every automatic job and the installed-owner acceptance class stays unproven while no dispatch
    runs. A green aggregate elsewhere is not a substitute: the `integration` job's containment witness
    matrix (`SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_*`) and the `compose` job's rootless native refusal
    proofs are different surfaces, and the 0006i receipt
    (`docs/evidence/goal-0006i-exact-subject-release-acceptance-2026-11-15.md`) states "A skipped or failed
    qualification job is unavailable evidence, and a missing live environment stays explicitly incomplete
    rather than being
    replaced by a synthetic pass."

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
      Correction (2026-11-18), superseding the two claims above that hosted CI does not reproduce
      the class, and that saturation alone decides it: hosted CI *did* reproduce it. Run
      `44128369103` (PR #56, a daemon-only change), job `control`, step "Render authenticated release
      shell on desktop and mobile", case `tests/portal-work-views.spec.mjs:1110 › Readable work views
      › a disconnected read recovers and reconciles with the server's own snapshot`: `desktop-chromium`
      failed in 31.5 s while its mobile sibling passed in 2.8 s in the same run (72 passed, 1 failed,
      3 skipped). The uploaded report's `error-context.md` shows why, and it is not the revision: the
      drawer rendered the run's *pre-run* document for the whole 30 s `expect` budget (`Execution: No
      execution recorded`, `Start run` still offered), so the wait ended as `element(s) not found`
      exactly as the recorded local runs did. A dedicated machine is no protection against that,
      because the case posted its run through the API and then waited for the surface to *hear* about
      it: that happens only when the operator invalidation channel delivers the frame, and a client
      that believes it is connected does not poll (`frontend/src/services/realtime.ts`: `syncPolling`
      stops at `status === "connected"`), so a frame published in the window between the socket
      opening and its channel joining leaves the surface stale with nothing left to observe. Load
      widens that window; it does not create it.
      Inventory of the whole render step — the six spec files it runs plus their helpers — listed with
      `rg -n -e '<pattern>'` from `browser/` over
      `tests/{workspace-shell,portal-work-views,portal-execution-control,portal-goal-managed,portal-cutover,portal-chat}.spec.mjs`
      and `tests/fixtures.mjs`, `tests/readable-fixtures.mjs`, `tests/planning-fixtures.mjs`, where
      `<pattern>` is the alternation of the wall-clock forms `waitForTimeout(`, `.toPass(`,
      `waitForFunction(`, `Promise.race(`, `new Promise((resolve) => { setTimeout`, `test.setTimeout(`,
      `timeout: <value>`, `intervals:`, `Date.now()`, `performance.now()` and `_GUARD_MS = `
      (regex-escaped): 33 matching lines on `origin/main`'s
      bytes, 37 on the delivered bytes. The 33 classify into 28 sites — **4 verdicts decided by
      elapsed time**, 21 hang guards, 3 pacing/attribution budgets — because three matches are
      fixture/attribution `Date.now()` reads that decide nothing and two line pairs belong to one site
      each. The four verdict sites, quoted at `origin/main`'s coordinates, are converted, none of them
      by lengthening a timeout, adding a retry, weakening an assertion or making a failure
      conditional: `portal-work-views.spec.mjs:1140` (the line CI failed on) now drives the detail
      surface's own refresh control and waits on the read it serves (its 200 plus the served state it
      renders) instead of on the channel's delivery; `portal-execution-control.spec.mjs:191-209` (the
      clock at `:200-207`) holds the surface's own detail reads so the replay press lands on the state
      the case observed, and asserts the server's own `disposition: "replayed"` unconditionally —
      before the change the reload's own read had already served the settled execution and removed the
      control, so that assertion never ran at all (measured: 3 runs of 3) and the clock that guarded
      its branch decided nothing; `workspace-shell.spec.mjs:264` and `portal-cutover.spec.mjs:207` now
      count the application's own history writes that name a location (recorded by an `addInitScript`
      before the module runs) instead of sleeping 300 ms / 200 ms past the redirect they claim cannot
      happen. The 21 guards and 3 pacing budgets stay, each a named constant with its reason beside it
      (`EXPIRY_GUARD_MS`/`EXPIRY_POLL_INTERVALS`, `STATE_GUARD_MS`, `BACKGROUND_READ_GUARD_MS`,
      `RECONNECT_GUARD_MS`, `WALK_GUARD_MS`/`HISTORY_WALK_GUARD_MS`,
      `LOGIN_GUARD_MS`, `FIXTURE_GUARD_MS`); no guard value was changed. `portal-cutover.spec.mjs`'s
      `readAt - writeStarted < 3000` (`:263` before the change, `:293` after) keeps its elapsed budget
      because there the elapsed time *is* the contract (the read must land inside the batch window and
      well inside the 5 s fallback cadence), and it is measured between two product events, never
      waited on.
      Falsification, on this host, against a `mix phx.server` of the delivered revision plus a second
      instance started with `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2` (the render step's own
      configuration) on PostgreSQL 18.6: with the surface's read answered from the pre-run document the
      converted work-views wait fails for its stated reason (`1 failed`, 34.3 s, exit 1, the CI
      occurrence's own error) and passes on the delivered bytes (`1 passed`, exit 0); with the server
      answering `disposition: "applied"` the converted replay assertion fails (`1 failed`, exit 1) and
      the delivered file passes (`10 passed`, exit 0, both projects); a temporary second redirect in
      `frontend/src/main.tsx` that leaves the destination and returns to it failed both converted
      "exactly once" cases (exit 1 each) while the pre-change `waitForTimeout` versions of those same
      two cases *passed* it (exit 0 each) — the sleep could not fail for the claim it stood for — with
      the sabotage reverted afterwards (the rebuilt bundle hashed back to the delivered asset name).
      One of those budgets is not a backstop in practice and is reported as the class's live half:
      `portal-cutover.spec.mjs:377`'s polling-fallback case (`:341` before the change) sizes
      `FALLBACK_CADENCE_GUARD_MS` at four of the client's own 5 s poll ticks, and the delivered bytes
      hit it on this host — `mobile-chromium` failed that case inside the acceptance pass below while
      its desktop sibling passed in 1.6 s, which is the class's own signature (a red and a green
      verdict for the same bytes, decided by how saturated the host was). Its elapsed budget is the
      subject, not a standby for one: convergence through the fallback cadence can only be observed
      by waiting at least one tick, and the case already records the reader's own reads and asserts
      one followed the write, so what remains load-sized is the bound over the app's cadence. The
      conversion left for it is a product signal for "the fallback just ticked" (or a fallback that
      does not depend on wall-clock ticks), which the product does not publish today.
      Acceptance pass of the six render-step specs on the delivered bytes, the render step's own file
      list through the direct Playwright CLI (`pnpm --offline` refuses a junctioned `node_modules` on
      this host): the render step's own file list (`node node_modules/@playwright/test/cli.js test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs tests/portal-execution-control.spec.mjs tests/portal-goal-managed.spec.mjs tests/portal-cutover.spec.mjs tests/portal-chat.spec.mjs`) reported `74 passed`, `1 failed`, `1 skipped` in 9.4 m, exit 1: the failure is the unconverted `portal-cutover.spec.mjs:377` cadence case named above (`mobile-chromium`), every converted case passed on both projects, and the skip is the desktop-only concurrency case's mobile sibling.
      What could not run here: the packaged release, which cannot boot on this Windows host (the
      generated `erts-*/bin/erl` shim calls `erlexec` without the `.exe` name), so the render step's
      release instances stay CI's to run. `portal-goal-managed.spec.mjs` ran rather than skipping
      because all three `POSTGRES_*` coordinates were set for the pass, and the expiry case ran against
      the second instance. Remaining product half of the class, reported rather than papered over: a
      connected client has no recovery for an invalidation it never received — the polling fallback
      stops once the socket reports connected, and the channel's own join completion
      (`frontend/src/services/realtime.ts`, `channel.join()` in `connect()`) is not treated as a read
      epoch — so the repair belongs to that signal in the product, not to a longer wait.
      *(Superseded by the archived closure — the product half was fixed; see
      `docs/archive/CLOSED-TODO.md`.)*
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      Still not converted, and not part of this change: the four cases #58 moved onto product state
      stay as they are; the other guards in these suites stay as named backstops; the other four
      render-step specs were not re-run here, and this entry's first clause (re-measure the
      pre-existing suites on a quiesced host) stays open.

## Record and documentation hygiene

- [ ] Revalidate the runbook claims that are true on this tree but rot without any
      edit to `docs/runbooks/**` (found while independently resolving every
      checkable runbook claim on `632b369`). A CI job split or rename invalidates
      `daemon-process-stop.md`'s "the daemon job runs `go vet`, a scoped ordinary
      Go pass and the full `-race` suite ... and the `daemon-windows` job runs the
      full Windows suite"; a harness pin move invalidates
      `daemon-opencode-peer-ownership.md`'s "OpenCode 2.3.0 is explicitly
      unsupported" and `control-plane-and-daemon.md`'s "the script-local version
      guard is `-ExpectedClaudeVersion 2.1.281`, which is not the daemon's
      promoted Claude pin" (both follow `docs/design/harness.md`); a pnpm bump
      invalidates every `mise exec pnpm@12.17.0 -- …` command (they follow
      `package.json`'s `packageManager`); a change to the `development-*` fallbacks
      invalidates the sign-in and live-check examples (`control/config/runtime.exs`
      `:63-64`); a rename of the workspace read's health keys invalidates
      `portal.md`'s four signal names (`health/4` in
      `control/lib/symmetry_control/workspaces/read_model.ex:471`); and
      `session-receipt-recovery-e2e.md`'s `mix ecto.drop --force` uses the Ecto
      option meaning "do not ask for confirmation", not `--force-drop`, so an
      Ecto-side change to that option would change what the cleanup line does. The
      runbooks also cite *Commands and gates* in `docs/design/quality.md` as the
      checks CI actually runs while that section still carries the stale frontend
      rows tracked under *Deferred review findings (0007i)* below.

      Closed in the second pass (the review findings of `gpt-6.1-sol` on `#70`,
      each re-verified against its artifact): `connections.md` no longer asserts
      the `vso.code`/`vso.work`/`vso.build` scope mapping it could not support —
      the shipped code requests an Entra resource token
      (`control/lib/symmetry_control/integrations/auth.ex:41-46`) and names no
      scope — and instead states the read-versus-write requirement the code does
      establish (`change.upsert`/`change.update` create or update pull requests in
      `integrations/providers/{github,azure_devops}.ex`), with the scope and
      permission names explicitly left to the provider. Still rot-prone after that
      pass, with no artifact that can re-derive them: the runbooks index's
      precedence rule (it now mirrors `AGENTS.md:24`'s record-the-conflict
      requirement instead of "the code wins", so a change to that contract
      bullet invalidates it), `portal.md`'s board-move sentence (the keyboard
      bindings follow `frontend/src/features/projects/work-surface.tsx:115` and
      the Goal-managed refusal follows `ensure_legacy_work_item/1`), `portal.md`'s
      `playwright-report/` location (follows `browser/playwright.config.mjs` and
      `browser/README.md`), `portal.md`'s CSRF sentence (follows Plug's
      `@unprotected_methods ~w(HEAD GET OPTIONS)`), and
      `chat-and-supervisory-control.md`'s supervisory/intent scoping (follows
      `control_capabilities/4` in `control/lib/symmetry_control/orchestration.ex`
      and the daemon's autonomy envelope in `daemon/internal/app/app.go`).

      *(Partly superseded by the archived third-pass closure, which restated several of these
      against their owners; see `docs/archive/CLOSED-TODO.md`. The revalidatable list below stays
      open.)*
      Closed part moved to `docs/archive/CLOSED-TODO.md`.

      Still revalidatable, with no artifact that can pre-empt a future edit: the
      generated-name regex and the `MIX_TEST_PARTITION` behaviour (Ecto/Ecto
      test-config side), the 45-second skip bound in
      `daemon/e2e/core_test.go`, the list of variables the production Compose
      service forwards, the Pi/native provider-bridge gate's exact shape, the
      installed-owner vocabulary for the Linux-v2 verifier, and the docs-index
      convention paragraph itself (it restates the precedence the design and
      protocol documents already carry).

## Deferred review findings (0007i, rebased onto `ca29a8e`)

- [ ] Revalidate the pre-existing doc drift in `docs/design/quality.md`: line 41 still says the frontend
      commands "do not exist until goal 0007 builds `frontend/`" and line 116 lists them as a goal-0007
      target, while `.github/workflows/ci.yml:67-70` has run them since 0007a. Observed while recording
      the 0007i gate table; update the quality policy in a separate documentation change.

## Lockfiles must carry canonical registry URLs

- [ ] Regenerate `pnpm-lock.yaml` only with the repository-pinned pnpm and a canonical registry, and check the result before
      committing. On 2026-11-17 this host's user-level `~/.npmrc` pointed at a package-feed mirror, so a regenerated lockfile
      recorded 96 tarball URLs at the mirror's host and CI's supply-chain policy rejected the install with
      `ERR_PNPM_TARBALL_URL_MISMATCH` in the `contracts` and `compose` jobs of run `44050937524`; pnpm caches that policy
      verdict by lockfile hash, so the stale lockfile kept passing locally after the change. Before committing, compare the
      count of canonical-registry tarball URLs against the lockfile's entry count (or regenerate with
      `--registry=https://registry.npmjs.org/`) and state which registry the bytes came from.

      **Enforced (2026-11-17, `fix/ci-diagnosable-release-boot`):** the registry host of every recorded tarball URL is
      checked by `scripts/ci/check-lockfile-registry.py`, which parses `pnpm-lock.yaml` as a YAML stream and runs before the
      install in the `contracts`, `control` and `compose` jobs of `.github/workflows/ci.yml` and in the dispatch-only
      `contracts` job of `.github/workflows/linux-native-v2-acceptance.yml`. It fails closed (exit 2) on a missing, empty,
      unparseable or tarball-free lockfile and prints every entry whose URL is off-host (exit 1), so a mirror-hosted
      lockfile now fails in CI before anything installs from it; its unit suite runs in the `daemon` job's "Check CI gate
      helper scripts" step. Still open: regenerating the lockfile with the repository-pinned pnpm and a canonical registry
      remains a manual duty, and the checker covers recorded tarball URLs only — it does not verify integrity hashes, and
      entries that record no tarball URL still resolve through whatever registry the install is configured with.

## Release-shell render failures leave no diagnosable evidence

- [ ] The `control` job's step "Render authenticated release shell on desktop and mobile" starts the
      production release with
      `PORT=4000 RELEASE_NODE=symmetry_shell "$bin" start > "$RUNNER_TEMP/shell-release.log" 2>&1 &`
      (and a second one on 4001 for the expiry flow), waits
      `curl --fail --retry 30 --retry-delay 1 --retry-connrefused http://127.0.0.1:4000/healthz`, and never
      uploads either log file. On `44045766116` job `control` that wait exhausted with **curl exit 7**
      (no TCP listener — not a 503, which `--fail` would report as 22), and the run's artifacts contain
      nothing that says why the release did not boot. The step also runs immediately after the job's
      `mix test`, `MIX_ENV=test mix compile --warnings-as-errors` and the folded Linux Pi E2E gates all
      passed, and the `control-release-linux` artifact of that run is byte-comparable in size to the
      passing run's `44043789372` (main `0220f2a`, same step green 14 minutes earlier), while no file in
      #46's 52-file delta touches the release's boot path. Upload both `$RUNNER_TEMP/*.log` files as
      artifacts on failure and echo the tail of `shell-release.log` into the annotation, so the next
      occurrence is diagnosable instead of inferred; then fix whatever the log shows (candidate
      mechanisms: an eager Postgres connection in a boot child, or a first-bind window the 30 s retry
      cannot cover).

      **Enforced (2026-11-17, `fix/ci-diagnosable-release-boot`):** that step now carries `id: render_release_shell`, and on
      its own failure the `control` job runs "Report release shell boot log", which emits
      `::error title=Release shell boot log tail (last 8000 bytes of $RUNNER_TEMP/shell-release.log)::<escaped tail>` (with
      `%`, CR and LF escaped so release output cannot forge a workflow command), and "Upload release shell boot logs", which
      uploads `shell-release.log` and `shell-expiry.log` as the artifact `shell-release-boot-<run id>-<run attempt>`
      (`if-no-files-found: warn`). Both are guarded by `if: failure() && steps.render_release_shell.outcome == 'failure'`, so
      passing runs upload neither log and an earlier step's failure cannot produce a boot-log annotation. Still open: a boot
      that dies before the redirect creates no log at all — the annotation then reports that the log holds no bytes — and the
      log tail is a diagnostic, so whatever it shows (an eager Postgres connection in a boot child, or a first-bind window
      the 30 s retry cannot cover) still has to be fixed from the evidence it now leaves. The retry window, the timeouts and
      every assertion in the step are unchanged; the upload and annotation themselves are only exercised by a real job run.
- [ ] Apply the one-guard-per-package convention to the remaining non-harness wall-clock verdicts.
      The backlog item recorded in `8082317` (#44) said roughly 176 further non-harness select fallbacks
      still decided verdicts with wall-clock time; re-derived on `e72290a` that class is 249 lines
      (`case <-time.(After|Tick|NewTimer)(` in non-harness test files: 262 lines minus the 13 already on
      a guard), so the recorded figure predated the tree's growth rather than overstating it. This change
      puts 276 sites on the documented per-package `testHangGuard`: 207 select fallbacks retimed, 47
      context budgets, 7 raw timers, 11 poll-loop deadlines, 3 duplicate 30 s test constants folded
      (`processOwnershipTestTimeout`, `tcpPeerHelperTimeout`, the local `cleanupTimeout`) and 1 new guard
      for a readiness wait that replaced a 50 ms sleep with the fixture's own marker; the three failure
      messages that named the clock ("within the bound", "timed out waiting for helper child PID", "only
      by test timeout") now name the outstanding step. No assertion, budget, skip or product file was
      touched. 281 of the listed sites now sit on that guard (13 -> 224 select fallbacks, 3 -> 50 context
      budgets, 0 -> 7 raw timers). The lister, since `scripts/` holds no such helper: `rg -n --no-heading
      --sort path --glob '*_test.go' -g '!daemon/internal/harness/**' -e 'case
      <-time\.(After|Tick|NewTimer)\(' -e 'time\.NewTimer\(' -e 'time\.AfterFunc\(' -e
      'time\.Sleep\(' -e 'context\.WithTimeout\(' -e 'context\.WithDeadline\(' -e 'time\.Since\('
      daemon | wc -l` -> 458 lines on this revision, and `rg -c testHangGuard` over the same output is
      281. Classification of the 458: (a) 281 hang guards on the per-package guard; (b) 48 behavioural
      budgets kept because the elapsed time is the contract - 18 negative probes whose expiry is the
      expected outcome (a stale-frame window, a not-before-release probe, two 25 ms containment probes,
      the fixture's configured `sink.delay`), 8 fallbacks derived from the product's own deadline
      (`containmentCloseDeadline`, `time.Until(deadline)`), 13 injected product deadlines (25 ms
      `WaitTurn`, 50 ms `RunBoundedCommand`, 20 ms terminate, 150 ms `WaitContext`, 100 ms caller
      deadlines, a 130 ms busy-spin probe, a deliberately expired deadline), 8 elapsed-time assertions
      that prove the bound they assert (a 1 s ceiling over a deadline-respecting wait, a 75 ms floor for
      the soft-stop grace, one measured-and-reported witness value) and 1 timer derived from an absolute
      deadline; (c) 65 sleeps - 45 in the host-provisioned e2e witnesses, 12 poll intervals inside
      explicit bounded loops that already wait on the product's own observable, 6 fixture payloads, 2
      negative-probe observation windows (a loaded host can only make those pass); (d) 64 out of scope -
      63 guard-shaped decisions inside host-provisioned suites (`daemon/e2e` 35, `daemon/native/linux-gate`
      28) plus 1 fixture timer installed into the product's scheduler. Open remainder: those 63 e2e and
      linux-gate decisions cannot be exercised on this host and need a provisioned runner plus
      independent review; `scripts/ci/linux-v2-host-peer-probe.go` keeps its own tool timeouts
      (capability probes, not test verdicts). Findings, not fixed: the negative-reconnect window in
      `notification/client_test.go:283` and the descendant-monitor polls wait on conditions the product
      deliberately does not publish, so their windows stay wall-clock. Verification: `gofmt -l .` empty,
      `go vet ./...` rc 0 on windows, `GOOS=linux go vet ./...` rc 0, `go test -count=1` rc 0 over the six
      touched packages, and the guard was demonstrated to fire with its named step in a byte-identical
      scratch copy whose only difference was a fixture that never wrote its readiness marker.

## Paginated reads must be bound to the cursor that served their pages

- [ ] Residuals of the completed class scan (2026-11-19): (a) the owner's decision on unifying the
      chat conversation's keep-and-state treatment with the work history's and the attention feed's
      retirement — an expanded page the chat keeps renders beside a stated unloaded region, while
      the other two retire it, and unifying changes one surface's reader-visible behaviour; (b) the
      browser suites, which CI runs, carry the live-daemon pagination cases (an expanded page
      disappearing when a live run moves the boundary is covered by the frontend suite only) and
      were not run on this host. The scan and its fixes are archived in
      `docs/archive/CLOSED-TODO.md`. Pre-existing, left as-is: `retainServedPage` detects a moved
      boundary by comparing cursor token bytes, so a re-serve that only re-signs the same boundary
      position still restarts the chain pointer; deciding whether it should compare the boundary
      message's id instead belongs to (a).
