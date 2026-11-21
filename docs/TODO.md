# TODO

Manual tasks and deferred maintenance discovered during scoped changes.

## Rules

- Done items move to `docs/archive/CLOSED-TODO.md`; this file holds only undone or in-progress items.
- When a closed part and an open residual share an item, the closed part moves to the archive and the residual stays here with a line saying where it went.
- Entries in `docs/archive/CLOSED-TODO.md` older than six months may be deleted.

A bullet that carries no `- [ ]`/`- [x]` marker is a decision note, not a tracked item: it stays
where it is and is neither moved to `docs/archive/CLOSED-TODO.md` nor deleted with it.

## SEO / repository metadata

- [ ] Upload `docs/assets/symmetry-social-preview.png` as the GitHub social
      preview. GitHub has no REST/GraphQL API for this: open
      **Settings → Social preview → Edit → Upload an image**, then confirm the
      card renders on the repository page. The image is 1280×640 and mirrors the
      README banner (`docs/assets/symmetry-banner.png`).

- [ ] Revalidate and remove three daemon surfaces the re-scoped dead-code sweep left in
      `internal/state` (2026-11-21, branch `refactor/rf-w1-06-daemon-deadcode`; plan
      `docs/plans/2026-11-21-w1-06-daemon-deadcode-plan.md`). `Store.AttachGoalSession`
      (`daemon/internal/state/goal_session.go:1044`), `Store.EnsureGoalSessionLineageAvailable`
      (`:592`) and `Store.FailUnresolvedProvideInput` (`daemon/internal/state/state.go:4175`) are
      unreachable from `deadcode -test ./cmd/...` on windows/amd64, linux/amd64 and linux/arm64
      alike, and at the baseline revision `a3039f0` `git grep -nw <symbol>` matches only each
      definition plus its doc comment — no call site, no test, no doc citation anywhere; this
      entry and the plan are the only matches added since. They were outside that sweep's owned
      file set; reproduction is the three `deadcode` runs plus the baseline-scoped grep, as
      recorded in the plan.

- [ ] Revalidate and remove the daemon surfaces that are unreachable from both mains but still
      referenced, so no deletion is possible without retargeting a caller (2026-11-21, same
      branch and plan). Three groups were surveyed; only the first, below, is dead-code material
      — the other two are live — do not remove (decision note below).
      **In the three-target `deadcode -test` intersection and referenced:**
      `control.NewOperatorClient` (`client.go:485`) with `SubmitTask`/`GetTask`/
      `CreateTaskCommand`/`operatorRequest`/`operatorRequestWithStatus` and
      `control.WithMaxResponseBytes` (`:99`) — callers `daemon/e2e/core_test.go:696`,
      `e2e/production_linux_containment_witness_test.go:2046`, the control tests, and
      `app/pi_provider_bridge_test.go:409` (the oversized-response case of the live `NewClient`);
      the seven legacy task validators in `daemon/internal/control/validation.go`
      (`:209/:281/:302/:334/:441/:611/:629`) — reached from
      `daemon/internal/control/client.go:992,:1007,:1022,:1034` (inside the dead operator client),
      from sibling validators (`validation.go:230,:239,:253,:306`), and from
      `daemon/internal/control/supervisory_validation_test.go:39,:48,:55,:125,:129,:148,:152`;
      the `protocol.Task`/`TaskWaiting` v1 decoders (65 and 8 references, 37 in
      `e2e/core_test.go`); and `harness.NewClaudeAdapterWithRunner` (`registry.go:245`, three test
      files). Removing any of them needs an owner for `daemon/e2e/**` and the app/control test
      seams — the W2-12 follow-up is that owner (program plan §4).

- live — do not remove (decision note, 2026-11-21, same branch and plan): the two remaining
      groups of that survey are production-live surfaces that no deletion pass may touch.
      **In the same `validation.go:209-629` range but in no target's `deadcode` findings (live
      code, not candidates):** `validateTerminalPayloads` (`:265`),
      `validateAcknowledgementResponse` (`:318`, caller `client.go:980`),
      `validateTaskCommandResource` (`:352`), `sameJSONObject/Value/Number` (`:448/:460/:501`),
      `validateSnapshotParts` (`:507`), `validateCommands` (`:519`), `isSupervisoryCommand`
      (`:544`), `ValidateSupervisoryPayload` (`:549`), `validateWork` (`:568`),
      `validateNullableJSONObject` (`:584`), `validateJSONObject` (`:595`), `isJSONNull` (`:607`)
      and `isTransitionState` (`:620`).
      **Not dead-code at all:** the shadowed root probe paths `probeCodexExecutableWithRunner`
      (`:379`) and `probeClaudeExecutable` (`:299`) are main-reachable through
      `NewCodexAdapter`/`NewClaudeAdapter` and appear in none of the three baseline `deadcode`
      reports; they belong to the probe/registry dedup (W3-04), not to a deletion pass.

## Build-tag parity (arm64 oracle)

- [ ] `GOOS=linux GOARCH=arm64 go vet ./...` cannot pass until
      `daemon/internal/state/linux_clone_journal_test.go` stops calling an amd64-only method
      (found 2026-11-21 while running the re-scoped W1-06 oracle). The test file is
      `//go:build linux` and calls `reader.verify(...)` at `:1077` and `:1091`, whose only
      definition is `daemon/internal/state/linux_clone_cleanup_reader_linux_amd64.go:154` under
      `//go:build linux && amd64`; the arm64 vet therefore reports
      `reader.verify undefined (type *LinuxCloneCleanupReader has no field or method verify, but
      does have method Verify)`. Pre-existing: both files are at the same lines in the baseline
      revision `a3039f0`, and CI's `go vet ./...` runs on the amd64 host, so nothing in the
      pipeline sees it. Fix by moving `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks`
      (`:723-1150`) into an amd64-tagged test file, keeping both assertions; do not delete them,
      do not narrow the whole test file's tag (its other 13 tests would leave the arm64 oracle),
      and do not widen only the reader's tag — the reader's file name pins GOOS/GOARCH, so a tag
      widening there needs a rename plus an arm64-compilability proof. The four packages W1-06
      touched vet clean on all three targets.

- [ ] Revalidate and remove three pre-existing dead stores of the class the daemon-app/state hygiene
      slice fixed, found while applying it. Evidence: the slice's `golangci-lint v2.15.0` run
      (`GOOS=linux`, `--default=none -E staticcheck,ineffassign,gocritic`, `./internal/app/...`) at
      `a3039f0` reports them among its `ineffassign` findings; after this slice they are the only
      remaining ones in `internal/app/`:
      `daemon/internal/app/app_test.go:4881` (`journal, err := store.LoadJournal(key)`),
      `daemon/internal/app/stale_ack_settlement_test.go:32` and `:125`
      (`journal, err := store.SetLocalState(key, "stale")`). Each binds `journal` and overwrites it on
      the next statement (`journal = expireLeaseBeforeCleanup(t, store, key)`) with no read in between,
      so the first value is dead; each needs its own review before removal, because the intermediate
      journal may be the value an intended assertion would read, and the daemon test split (W3-15)
      owns those files.

## Retro review follow-ups

- [ ] Investigate the intermittent Control failure class behind the provider-action cases at
      `control/test/symmetry_control_web/controllers/provider_action_controller_test.exs:1906-1933`
      ("a broker restart automatically recovers accepted work after lease expiry", with its timer
      assertion `:1929`) and `:1935-1962` ("the broker worker completes after the request process
      exits", carrying the same assertion at `:1961`). Observed intermittently in
      local runs — the observation is the 2026-11-20 retro review's — and no occurrence is recorded in
      the repository for either case. Most probable mechanism (inference, not observed): both cases
      release the worker with `send(provider_pid, :continue)` and then read the broker's
      own timer — `assert_timer_cancelled(readiness.timeout_ref)`
      (`:2764-2766`, `Process.read_timer(timeout_ref) == false`) — without broker synchronization
      (`:1906` reads it directly after the worker's `:DOWN`; `:1935` joins the worker and asserts two
      persisted fields first), while that timer is cancelled only inside the broker's own message
      handling — when it handles the worker's completion
      (`control/lib/symmetry_control/integrations/provider_access.ex:95` → `pop_job/2` `:232` →
      `cancel_timer/1` `:238`), when a worker `:EXIT` pops its job (`:113`/`:116` → the same
      `pop_job/2` → `cancel_timer/1`), and in `terminate/2`'s bulk cancel (`:135`) — so a test that
      observes the worker's `:DOWN` first
      can still read a live timer. Secondary candidates: the 5 s `assert_receive` bound under load, and
      the unbounded `receive`s that turn a missed message into a hang. The assertion appears in eleven
      call sites across ten cases of this file (`:1826`, `:1929`, `:1961`, `:1990`, `:2028`, `:2060`,
      `:2094`, `:2201`, `:2345`, `:2375`, `:2649`; the two in the broker-restart case declared at
      `:2035`), so treat it as a class. Revalidate against a recovered failure before changing anything
      (rerun the two cases, then check whether the failing read is the timer or the intent state), and
      keep both assertions.

- [ ] The Goal surface offers no admission path for a resolved plan decision whose admission was
      never accepted.
      `PlanApproval` renders only for an open plan decision: `planApprovalOption`
      (`frontend/src/features/goals/detail.tsx:408`) returns null unless `decision["open?"] === true`,
      the render (`:381-382`) mounts no other admission affordance, and the resolved card shows
      "Resolved: …" only. `NextAction` has no `accept_plan` payload path: its `accept_plan` branch
      renders a note only ("Approve the open plan decision above first …", `:533`), `actionPayload`
      returns null for every kind it does not build (`:626-627`), and the send button returns without
      sending anything. The dead end is reachable through the press's own two commands by two paths.
      (a) An answered admission refusal releases the whole attempt (`:218`), so a re-press re-resolves
      a decision the server serves `resolved` and is refused `invalid_decision` (`apply_command!`
      requires `decision.state == "open"`, `control/lib/symmetry_control/goals.ex:6491-6495`), while
      the server would accept a fresh `accept_plan` built from a current read (`goals.ex:6395-6414`).
      (b) A lost resolution acknowledgement keeps the attempt, but the goal invalidation the committed
      resolution published before its response
      (`control/lib/symmetry_control_web/plugs/portal_invalidations.ex:28-32`) refetches the card and
      the `open?` gate (`:408`) unmounts the control; the server would replay the frozen resolution
      bytes, and the admission would still need a fresh read. Pre-existing (the pre-fix press also
      applied the resolution and could not admit) and outside `fix/resolve-decision-submit-read`'s
      contract scope; that branch records the frozen attempt's release rule and the precondition of
      the lost-acknowledgement replay, not this dead end. Fix direction: keep a retry control while
      `sentApproval` holds a frozen attempt for that decision id (its frozen resolution replays
      through `replay_event` and the admission is built from a fresh read), which covers only (b), or
      serve `accept_plan` as an allowed action for a resolved plan decision that is not yet admitted,
      so `NextAction` can present it through a payload path, which covers both paths.

- [ ] Give the final-scan RPC's timer arms the post-timer recheck the barrier join and
      `stopDescendantMonitor` now carry. `requestDescendantMonitorScan`
      (`daemon/internal/platform/process_unix.go:1029-1066`) waits on a timer twice and returns a
      timeout whenever the timer arm wins, without rechecking the channel whose result is already in
      hand. First select `:1046-1052`: the request arm's timer `:1050-1051` returns "descendant monitor
      final scan request timed out" without rechecking `monitorDone` (close-only, created `:610`,
      closed `:817`, so it stays ready once closed). Second select `:1053-1065`: the `monitorDone` arm
      `:1056-1062` does recheck the buffered `resultChannel` (created `:1045`, written by the monitor
      at `:1002` before it can return), but the timer arm `:1063-1064` returns "descendant monitor final
      scan timed out" without rechecking either channel. A monitor that exits as the deadline expires,
      or a result already published there, therefore leaves both arms ready; Go picks uniformly
      between them, and the timeout is reported for a result that is in hand. Fail-closed, so the cost
      is manufactured unproven-containment results: both errors reach `closeUntil` (`:1196` on the
      leader-absent path, `:1237` on the post-readback final scan), where they become sticky
      `descendantScanLost` (`:1248-1252`) or an unproven containment return (`:1205-1207`), and the
      same manufactured errors also reach the supervisor mirror path —
      `supervisor_linux_client.go:1223` (`verifyMirrorObservation`, returned as-is) and `:1448`
      (`sealInitialMirrorScanForWitness`, whose pre-resume witness scan wraps the error). Same shape
      as the barrier join `:665-688` (fixed, with pins, by PR #105 `e64f8c0`) and
      `stopDescendantMonitor` (fixed, with pins, on branch `fix/stop-descendant-recheck`). Apply the
      same recheck here and pin both directions (an arm that becomes ready at the deadline is honored;
      a monitor that never answers still fails closed), in a separate maintenance change. This entry's
      inventory is scoped to the RPC arms: two further pre-existing arms of the same class —
      `supervisor_linux_client.go`'s `sealInitialMirrorScanForWitness` (`:1468-1470`, timer arm on
      `mirror.monitorDone`) and `waitForScanEvents` (`:654-657`, timer arm on `done`, the channel
      `serveScanEvents` closes at `:506`) — are recorded for completeness but not part of this fix
      direction.
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      Coordinates re-pointed to `supervisor_linux_client.go` by the move-only platform split
      (`docs/plans/2026-11-21-rf-w1-04-platform-split-plan.md`); the four declarations were moved
      unchanged, no line above them in that file was edited.

## Retro review follow-ups (second pass, 2026-11-20)

- [ ] Stop the Windows-family path tail from absorbing a JSON escape that follows the path.
      `_WINDOWS_TAIL` (`scripts/ci/model-routing-record-sanitize.py:110`) admits a bare backslash
      as a path byte — deliberate, per its comment at `:104-107`, because in raw recorded text the
      backslash is the separator — and the four Windows-family patterns listed here end in it (or
      in its require-a-byte variant `_WINDOWS_TAIL_PLUS` `:115`): `_DRIVE_PATH` `:133-135`,
      `_UNC_PATH` `:142-145` (`_PLUS`), `_SLASH_UNC_PATH` `:167-169` (`_PLUS`),
      `_FILE_URL_PATH` `:204-212`
      (the root-relative `_ROOT_RELATIVE_PATH` `:191-194` and the file-URL host-root arm `:209` do
      not), so a recorded `\n` escape that follows a Windows path is consumed into the rendered
      token together with the text behind it. The POSIX family already fixed exactly this
      shape (`_POSIX_BYTE` `:122`, whose comment at `:116-121` names the failure — "consuming it would
      swallow a JSON escape sequence together with the text behind it" — and
      `scripts/ci/test-model-routing-record-sanitize.py:282-295`,
      `test_a_json_escape_after_a_path_survives_the_render`, STD-A07); the Windows family has no
      such case. Reproduced at `978abba` with the tool on two scratch inputs of one shape (a JSON
      string recording a path, then the two-character `\n` escape, then the word `FAILED`; the
      Windows input's path is drive-rooted and carries the JSON-escaped doubled separators). The
      inputs, one JSONL line each, are built in Python with `BS = chr(0x5C)`: Windows
      `'{"output":"wrote C:' + BS*2 + 'Users' + BS*2 + 'op' + BS*2 + 'run' + BS + 'nFAILED"}'`,
      POSIX `'{"output":"wrote /' + 'tmp/run' + BS + 'nFAILED"}'`. The Windows input renders to a
      single token whose digest is
      `path-sha256:a3a19f2b70d9c277fa8b2ef0f62c217841c3f41ed7e6a78b689045338e4ad1e5`, and that
      digest is the recomputed sha256 of the raw span the token bound — the path text *plus* the
      escape *plus* `FAILED` — where a path-only render of the same input would carry
      `26e7b5b4…` (the sha256 of the path text alone); the POSIX input of the same shape keeps the
      escape and the word outside its token (`…path-sha256:91525b09…` followed by the escape and
      `FAILED`). Consequence in a record: an evidence line behind the escape disappears from the
      rendered bytes while the record stays valid JSON — the loss STD-A07 fixed on the POSIX side.
      Fix direction: the POSIX rule is not directly portable (a raw Windows spelling uses one
      backslash per separator, so a separator run can be shaped exactly like a JSON escape —
      `\run` starts with `\r`), so the reading must be decided — either a JSON-aware pass that
      respects string escapes (the tool's input is recorded JSON text), or a tail that stops
      before an escape-shaped backslash run once at least one separator has been consumed — and
      both the escaped-escape case and the raw single-backslash spelling must be pinned in the
      tester. Suggested owner area: `scripts/ci` record tooling.

- Repo-wide `actionlint` with ShellCheck enabled does not terminate on this Windows host, so the
  workflows have no local shellcheck-enabled `actionlint` verdict; the substitute is a
  shellcheck-disabled repo-wide `actionlint -shellcheck= -pyflakes=` run plus a direct
  `shellcheck --shell=bash` over each changed workflow `run:` block in full (read from the
  workflow file; the diff only selects which blocks), with every `${{ … }}` GitHub expression
  replaced by a placeholder word first, which is what actionlint does, because a raw expression is
  ShellCheck SC2296. The 2026-11-15 trim record used the same direct `shellcheck --shell=bash`
  over whole scripts (`docs/evidence/ci-process-trim-2026-11-15.md:231`); the placeholder step is
  added here for blocks that carry expressions.
  A shellcheck-enabled `actionlint` run is used only on a workflow where it has been observed to
  finish; for `.github/workflows/ci.yml` on this host it is not available. Observed
  2026-11-20 at `978abba` with the `mise`-installed upstream actionlint 1.7.12 invoked as
  `timeout -k 10 620 actionlint -color -shellcheck=shellcheck .github/workflows/ci.yml`: the bound
  killed it after 10 m 20 s wall time (`EXIT=124`, no output, no findings printed) — no completed
  run of that shape is recorded on this host — while the same binary over the same file with
  `-color -shellcheck= -pyflakes=` finished in 0.6 s at exit 0 with no findings, and the
  2026-11-15 trim record already carries a 120 s sample of the same shape
  (`docs/evidence/ci-process-trim-2026-11-15.md:228-234`). No CI job invokes `actionlint` (a
  repository search for the name matches `docs/` only), so this is a local-lint gap only: nothing
  in a hosted run is skipped by it. Owner area: CI tooling.

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
    its job carries no event `if:` guard (the `on:` block at `:3-6` is the only trigger), and
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

- [ ] Residual of the completed class scan (2026-11-19): (b) the live-daemon pagination cases run
      only under their own gates — `browser/tests/portal-chat-live.spec.mjs`
      (`SYMMETRY_CHAT_LIVE=1`, run by `browser/scripts/run-chat-daemon-tests.ps1`) and
      `browser/tests/portal-live-daemon.spec.mjs` (`SYMMETRY_LIVE_DAEMON=1`, run by
      `pnpm --filter symmetry-browser-acceptance test:live`) — and were not run on this host. CI's
      browser step names six specs and selects neither, so an expanded page disappearing when a
      live run moves the boundary is covered by the frontend suite only; running the live cases
      needs a provisioned live daemon on a quiet host.
      Closed part moved to `docs/archive/CLOSED-TODO.md`, recording the owner's 2026-11-19 keep
      decision — the chat conversation keeps its keep-and-state treatment rather than unifying with
      the work history's and the attention feed's retirement — and the fixed pre-existing
      `retainServedPage` note (a moved boundary is now judged by the boundary message's id, not the
      re-signed cursor's bytes); a cursor the reader has paged past is not renewed by later serves,
      but its refusal after the server's 24-hour `max_age` now restarts the chain from the served
      page and re-reads it, so the next press renews the token and the re-walk dedupes.

## Deferred review findings (PR #133)

- [ ] Align the TypeScript canonical encoders' object-key sort with the UTF-8 byte order Go and
      Elixir sort by. `contracts/ts/semantics.ts:37-47` sorts keys with `Object.keys(...).sort()`
      and the frontend `canonicalJson` (`frontend/src/services/commands.ts:53-58`, comparator at
      `:57`) compares UTF-16 code units, while Go `sort.Strings`
      (`daemon/internal/protocol/goal_subject.go` `writeCanonicalJSON`'s object arm) compares
      bytes and Elixir `Enum.sort_by(&elem(&1, 0))`
      (`control/lib/symmetry_control/request_hash.ex:31-36`) compares binaries byte-wise, so two
      keys that straddle the orders disagree — for example U+1F600 and U+E000: UTF-16 puts the
      emoji first, UTF-8 the U+E000 key. Hashed documents appear to carry only ASCII keys today
      (inference from the schema shapes, not exhaustively proved), so the class is not reachable
      through the current seams. Found by the round-1 review of PR #133, outside that PR's
      scope. Fix direction: sort by UTF-8 bytes (or code points) in both TypeScript encoders
      and pin a cross-language vector with a non-ASCII key in each layer's suite.

## Deferred review findings (PR #145)

- [ ] Consolidate the served-document gate helpers after the increment that owns
      `control/test/support/**` (W1-12) has landed. `assert_served_members!/3`, `assert_served_rows!/3`,
      `waiting_decision_payload/0`, the deterministic version-4 `uuid/1` and the bundle-derived
      `gate_members/3`/`declared_members/2` are duplicated between
      `control/test/symmetry_control_web/controllers/portal_resources_test.exs:1373-1426` and
      `control/test/symmetry_control_web/controllers/portal_workflow_test.exs:1891-1943`. Each file
      keeps its own copy because `control/test/support/**` was outside the gate increment's owned
      files and a module defined in one test file would break a single-file run of the others, but
      the copies can drift. Move them into `SymmetryControlWeb.ServedDocument` (the natural home for
      the bundle-derived pair too) and have both gate files import them. Found by the adversarial
      review of PR #145 (P3-2).

## Production Linux containment witness: stop-receipt barrier stall awaiting a diagnosable occurrence

- [ ] Diagnose the next occurrence of the `daemon_sigkill_after_lease_arm_before_resume` barrier
      stall from the restarted daemon's exported log tail; do not raise the 45 s bound in the
      meantime. **Occurrence:** CI run `44794227912` job `136491073605` (`integration`; PR #119 head
      `22ec193`, test merge `3b4a6ce` on base `adf6d38`) failed subtest
      `TestProductionLinuxContainmentWitnessSpine/daemon_sigkill_after_lease_arm_before_resume` after
      **110.19 s** with (the marker path's `$RUNNER_TEMP/symmetry-linux-containment-witness/<case>/`
      prefix elided here; the CI log spells it out),
      `production_linux_containment_witness_test.go:509: wait for durable stop-receipt-before-release barrier: marker "…/lease-arm-before-resume/stop-receipt-before-release.reached" did not appear within 45s`
      — the reported `:509` is the caller frame of the `t.Helper()` runner, whose `t.Fatalf` sits at
      `daemon/e2e/production_linux_containment_witness_test.go:1026` — followed by a failure cleanup
      that could not prove the stop: `:738: Linux production witness failure cleanup could not prove
      exact helper stop/release: bound recovery did not clear journal during failure cleanup: journal
      a2c5a551-2026-45d3-9eaf-467717f281e6/1 retained containment after 45s`, `containment journal
      remains unresolved after failure cleanup: … retained containment after 5s`, and
      `failure cleanup could not prove exact helper 21107/"linux:v2:9d556fd5-837f-4845-b407-4c6aa53983b4:4026531836:21107:19582"
      gone: process 21107 with identity "…" remained live`. **Classification:** flake of the witness's
      restart replay, not a product regression — every file under `daemon/e2e`, `daemon/internal/app`
      and `daemon/internal/platform` is byte-identical between the failing revision and current main
      (`git diff --name-only 22ec193 a7afdf4 -- daemon/e2e daemon/internal/app
      daemon/internal/platform` prints nothing), the same subtest is green in `0.56 s` on main
      (`44795802448` job `136495892251`: `PASS: …/daemon_sigkill_after_lease_arm_before_resume
      (0.56s)`), the run's own merge diff (`git diff adf6d38 3b4a6ce --stat`, 14 files) touches
      neither those trees nor `.github/workflows/ci.yml`, and the failing run's uploaded evidence
      (artifact `production-linux-containment-witness-44794227912-1`, 17 files) was complete: its
      `lease-arm-before-resume.json` stops at stage
      `daemon_crashed_after_lease_arm_with_durable_mirror_seal_before_resume` while the manifest
      reports `"complete": true` and `"incomplete_evidence_files": []`, i.e. the stall left no
      daemon-side trace in that evidence at all. **Recurrence (2026-11-21):** CI run `44803673577`
      attempt 1 (PR #126 head `17d838b`, the merge of `origin/main` into
      `fix/provider-action-chain-removal`) failed the same subtest in its `integration` job
      (`136524113298`) after **110.29 s**, with the identical
      `production_linux_containment_witness_test.go:509` 45 s barrier line and the same cleanup
      shape (journal `b2090059-1137-4b6b-a975-58f1e202834c/1` retained containment after 45s;
      helper 15580 remained live), while main's last six CI runs (through `44804764621` @
      `ea65742`) were all green. **Suspect path** for the restart pass:
      `daemon/internal/app/cleanup_glue.go:118-143` (record the durable receipt →
      `verifyDurableContainmentStopReceipt` at `:285` → `observeContainmentStop`) →
      `waitForLinuxStopReceiptBarrier` (`:203`), which publishes the reached marker at `:234` only
      after both marker paths pass `assertLinuxStopReceiptBarrierPathAbsent` (`:218`/`:221`), and
      whose own release bound is `linuxStopReceiptBarrierTimeout = 30s` (`:21`); the marker never
      appeared, so either the restart pass never reached the callback or it stalled before
      publishing. **Diagnosis rule:** read the restarted daemon's log tail now exported by
      `scripts/ci/export-linux-v2-redacted-evidence.py` (2026-11-21, `witness-stall-evidence`): a
      failed legacy witness run adds `<case>-second-daemon-stdout.json` and `-stderr.json` to the
      `production-linux-containment-witness-<run id>-<attempt>` artifact, carrying a ≤64 KiB
      sanitized tail of the `second-daemon.*.log` files written at
      `daemon/e2e/production_linux_containment_witness_test.go:1723-1754`; each record keeps
      `time`/`level` plus the allowlisted message/enum/integer fields, while identities, paths and
      `error` text are intentionally never exported and `dropped_lines` counts lines that could not
      be projected (a Go panic dump therefore appears only as a count). The 45 s harness wait and the
      30 s daemon bound are declared bounds, not the defect: no timeout changes until an occurrence
      has been diagnosed from that tail.

## Daemon: `writeInputBounded` can strand its writer goroutine (audit F15)

- [ ] Give `writeInputBounded` (`daemon/internal/app/app.go:7240-7272`) an owned exit path for the
      writer goroutine it starts at `:7242` (`go func() { result <- process.WriteInput(input) }()`):
      the `ctx.Done` and timer arms return while the write can still be blocked in a stopped
      process's pipe, so the goroutine outlives the call, belongs to no WaitGroup, and contradicts
      `daemon/AGENTS.md` ("Every goroutine has an owner and exit path"). Found by the 2026-11-21
      daemon audit (finding F15). Fix direction: prefer the context-aware write path the runner
      already implements -- `internal/execution/runner.go:1378` `WriteInputContext` closes the stdin
      write end when its context is cancelled (`:1408-1412`) -- which `internal/app` cannot reach
      today only because its `Process` interface (`app.go:232-233`) declares `WriteInput([]byte)
      error` alone; otherwise close the pipe's write end on timeout (the runner owns the pipe), or
      track the goroutine in `daemon.workers` with a documented bound and assert its exit at
      shutdown. Pin it with a test that a blocked writer returns after timeout + terminate.
      Consequence carried meanwhile by refactor slice rf-w1-08: `internal/app` verifies goroutines
      per-test (`verifyNoLeakedGoroutines` in `daemon/internal/app/leak_check_test.go`) instead of a
      package-wide `goleak.TestMain`, because this path would force wildcard ignore lists that would
      hide real leaks.

## Harness shared-scaffolding deferrals (rf-w2-13, PR #155)

- [ ] Delegate the two provider-action UUID validator copies the W2-12a unification left outside
      its owned set, behind the message-policy primitive that review P1-1 requires (2026-11-21,
      branch `refactor/rf-w2-13-harness-scaffolding-5a`; plan
      `docs/plans/2026-11-21-rf-w2-13-harness-scaffolding-5a-plan.md` §8). The copies are
      `validateProviderBridgeUUID` (`daemon/internal/harness/pi/provider_bridge.go:976`), whose
      follow-up owner is W2-13, and `validGoalSessionUUID`
      (`daemon/internal/state/goal_session.go:1988`), whose owner is W2-09. They differ in message
      text and strength from each other and from the canonical validators: the `pi` copy returns
      "UUID is invalid" / "UUID must use lowercase hexadecimal digits" (no version/variant check),
      the `state` copy returns a bool and rejects non-v5 versions and bad variants, while
      `control.validateProviderActionUUID` and `protocol.validateUUID` use other strings. Pin
      every current rejection string per call site before delegating, so the unification changes
      no message; `pi/provider_bridge.go:1102-1120` stamps v5 UUIDs, so the stricter check is
      safe for journaled action IDs.

- [ ] Consolidate the frozen `opencode` duplicates onto the shared harness helpers when the D1
      freeze lifts with opencode capability work (W6-04) (2026-11-21, same branch and plan, §8).
      `opencode/**` stayed read-only (P2-3/D1: its six CI assertions and the release-acceptance
      case stay green), so every surface this slice consolidated elsewhere still has an
      `opencode` copy: process forwarding with its own guards
      (`daemon/internal/harness/opencode/adapter.go:543` ProcessDetails, `:559`
      LinuxV2LiveOwnerStatus, `:578` LeaseRenewalAvailable, `:589` RenewLease),
      `isNilNativeProcess` (`:421`, `reflect.Pointer`-only), the Close/join state machine
      (`:1063`), `emit` (`:1344`, stamps `At` and calls `sink.Handle` directly instead of
      `harness.DeliverEvent`) and `Wait` (`:975`). Consolidating them is part of opencode
      capability work, not this increment, and must keep those CI oracles green.
