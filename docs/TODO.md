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

- [ ] Revalidate and remove the two remaining daemon surfaces the re-scoped dead-code sweep left
      in `internal/state` (2026-11-21, branch `refactor/rf-w1-06-daemon-deadcode`; plan
      `docs/plans/2026-11-21-w1-06-daemon-deadcode-plan.md`). `Store.AttachGoalSession`
      (`daemon/internal/state/goal_session.go:1026`) and `Store.FailUnresolvedProvideInput`
      (`daemon/internal/state/state.go:4241`) are
      unreachable from `deadcode -test ./cmd/...` on windows/amd64, linux/amd64 and linux/arm64
      alike, and at the baseline revision `a3039f0` `git grep -nw <symbol>` matches only each
      definition plus its doc comment — no call site, no test, no doc citation anywhere; this
      entry and the plan are the only matches added since. They were outside that sweep's owned
      file set; reproduction is the three `deadcode` runs plus the baseline-scoped grep, as
      recorded in the plan. The third candidate, `Store.EnsureGoalSessionLineageAvailable`, was
      revalidated and removed on the W3-07 follow-up branch
      `refactor/rf-w3-07b-state-error-followups` (moved to `docs/archive/CLOSED-TODO.md`).

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
      (`:273`) and `probeClaudeExecutable` (`:193`) are main-reachable through
      `NewCodexAdapter`/`NewClaudeAdapter` and appear in none of the three baseline `deadcode`
      reports; the probe/registry dedup (W3-04, #187) kept them (D4: the root stays fail-closed
      and undelegated — no `harness` → `harness/codex` import), so no deletion pass may remove
      them either.

      **Annotation (2026-11-22, rf-w4-15c):** the "no deletion pass may remove them either"
      sentence is superseded for the two named probe paths by the arch-decision-2 ruling O13
      (ACCEPT): the Claude candidate relocation moved the Claude adapter and its promotion probe
      out of the root package, deleted the Codex, Claude and Pi shadow constructors with their
      probe helpers, and left the registry registering only the generic and OpenCode adapters,
      while the daemon composition root keeps registering all five native kinds, so the
      registered-adapter set the daemon sees is unchanged. The removed W1-06 residual moved to
      `docs/archive/CLOSED-TODO.md` under `## Dead-code candidates (rf-w1-06 re-scoped sweep)`;
      the subject hashes, deleted-declaration list and mutant record are in
      `docs/evidence/rf-w4-15c-claude-candidate-relocation-2026-11-22.md`. The sentence above is
      kept as recorded.

- [ ] Reconcile the `harness/claude` package comment with the package's contents: the header of
      `daemon/internal/harness/claude/stream.go` still states that the package "intentionally does
      not expose a `harness.Adapter` until credentialed native lifecycle evidence proves the
      required session and control semantics", which the Claude candidate relocation made false —
      the package now owns the adapter. That file was outside the relocation's owned set (W4-15a's
      decoder, W4-15b's driver), so the sentence waits for the slice that next opens it. Owner
      area: daemon harness.

- [ ] Unify the `controlCapability` switch copy in `daemon/internal/harness/claude/adapter.go:576`
      with the root helper in `daemon/internal/harness/generic.go:255`: the two bodies are
      byte-identical outside the `harness.` qualifier, but the root helper is unexported and
      `generic.go` was outside the rf-w4-15c owned file set, so the relocation kept a package-local
      copy. Export the root helper and delete the copy in the slice that next owns `generic.go`
      (A3 adapter policy, W5). Owner area: daemon harness.

- [ ] Reconcile the two `testdata/claude/**` READMEs with the relocated identifiers: both files
      were frozen for rf-w4-15c (`harness/testdata/**` is W5-10's surface) and neither is in the
      citation gate's audited corpus, so the move left them stale —
      `daemon/internal/harness/testdata/claude/2.1.281/README.md:48` names
      `claudeCandidateStructuredOutputSchema` (now `candidateStructuredOutputSchema` in
      `claude/adapter.go`) and `daemon/internal/harness/testdata/claude/2.1.323/README.md:36`
      names `ClaudeTestedExecutableHash` (now `testedExecutableHash` in `claude/probe.go`). Owner
      area: W5-10 fixture pruning.

- [ ] Re-pin `codex.Probe`'s own nil-context guard: `daemon/internal/harness/codex/probe.go`
      rejects a nil context with `"codex probe context must not be nil"`, and after the rf-w4-15c
      relocation deleted the root registry-seam case that exercised a nil context end to end, no
      HEAD test covers that guard text — the shared entry guard stays pinned by
      `TestRunBoundedProbeRejectsNilContext`
      (`daemon/internal/harness/bounded_probe_test.go`). Owner: W5-16 (codex adapter concepts).

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
      remaining ones in `internal/app/`: in `daemon/internal/app/app_test.go`,
      `TestRecoveredCleanupRulesPreserveOrRemoveJournalSafely` subtest "empty path is no-op"
      (`journal, err := store.LoadJournal(key)`); and in
      `daemon/internal/app/stale_ack_settlement_test.go`
      (`journal, err := store.SetLocalState(key, "stale")`) in
      `TestStaleCleanupWaitsForCommandAcknowledgementWithoutSendingOrdinaryOutbox` and in
      `TestStaleAcknowledgementConclusiveErrorRetiresBeforeCleanup` (both of its table subtests).
      Each binds `journal` and overwrites it on the next statement
      (`journal = expireLeaseBeforeCleanup(t, store, key)`) with no read in between,
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
      (`frontend/src/features/goals/detail.tsx:413`) returns null unless `decision["open?"] === true`,
      the render (`:388`) mounts no other admission affordance, and the resolved card shows
      "Resolved: …" only. `NextAction` has no `accept_plan` payload path: its `accept_plan` branch
      renders a note only ("Approve the open plan decision above first …", `:539`), `actionPayload`
      returns null for every kind it does not build (`:632-633`), and the send button returns without
      sending anything. The dead end is reachable through the press's own two commands by two paths.
      (a) An answered admission refusal releases the whole attempt (`:224`), so a re-press re-resolves
      a decision the server serves `resolved` and is refused `invalid_decision` (`apply_command!`
      requires `decision.state == "open"`, `control/lib/symmetry_control/goals.ex:6491-6495`), while
      the server would accept a fresh `accept_plan` built from a current read (`goals.ex:6395-6414`).
      (b) A lost resolution acknowledgement keeps the attempt, but the goal invalidation the committed
      resolution published before its response
      (`control/lib/symmetry_control_web/plugs/portal_invalidations.ex:28-32`) refetches the card and
      the `open?` gate (`:414`) unmounts the control; the server would replay the frozen resolution
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
  (`docs/evidence/ci-process-trim-2026-11-15.md:228-234`). When this entry was written no CI job
  invoked `actionlint` (a repository search for the name matched `docs/` only); refactor slice
  W1-01 added the shellcheck-disabled shape to the `legacy-route-audit` job as the advisory step
  `Measure actionlint findings for the workflows (advisory)` (pinned upstream actionlint 1.7.12,
  `actionlint -shellcheck= -pyflakes=` over every `.github/workflows/*.yml`) beside
  `Measure shellcheck findings in scripts/ci (advisory)`
  (`shellcheck --severity=warning scripts/ci/*.sh`); both are `continue-on-error: true`. The
  shellcheck-*enabled* shape still runs nowhere: not on this host (the observed non-termination
  above) and not in CI, where the hosted step disables the integration deliberately so the
  verdict stays the shellcheck-disabled one. This entry therefore stays open as the local-lint
  gap it describes, with its substitute recipe unchanged. Owner area: CI tooling.

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
    systemd-container`); pinned tooling Go 1.27.4, Elixir 1.20.4/OTP 29.0.6 and the `postgres:18.6-alpine`
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
      `notification/client_test.go:285` and the descendant-monitor polls wait on conditions the product
      deliberately does not publish, so their windows stay wall-clock. (Coordinate re-pointed from
      `:283` by the W3-05 notification tests, whose two added imports shift every line of that file by
      two; the cited `time.Sleep(20 * time.Millisecond)` itself is unchanged.) Verification: `gofmt -l .` empty,
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

## Deferred review findings (rf-w2-16 clone-aliasing gate)

- [ ] Deep-copy the nested helper exits that the platform stop-evidence clones still share or
      truncate, then remove the matching per-case skip paths from the aliasing harness.
      `cloneLinuxCgroupStopReceipt`
      (`daemon/internal/platform/linux_cgroup_v2_linux.go:1443-1457`) copies `LinuxProof` by
      value (`:1449`) and deep-copies only the stored `LinuxProof.HelperExit` (`:1451-1452`), so
      `LinuxProof.GateExit.HelperExit` and `LinuxProof.LauncherExit.HelperExit` stay shared with
      the caller's receipt; that stored `HelperExit` copy shares its own nested exit in turn.
      The `GateExit`/`LauncherExit` alias is live on a valid shape: a mirror stop proof demands
      `proof.HelperExit != nil` and a `GateExit` anchored on that same exit
      (`daemon/internal/authority/authority.go:1470-1474` via `ValidForOwnedDescendant`,
      `:1174-1184`), every v2 removal receipt must carry such a proof
      (`StopReceipt.ValidForHandoff`, `authority.go:1822-1833`), and the platform constructs it
      (`daemon/internal/platform/anchor_run.go:1531`; validated at
      `daemon/internal/execution/linux_owner_v2_linux.go:1351` and sealed into the mirror proof
      at `:1373`); `authority.go:2027` also accepts a non-nil `LauncherExit.HelperExit`.
      `cloneLinuxOwnerHelperV2StopEvidence` (`helper_v2_stop.go:1540-1573`) instead drops every
      nested exit it copies through `cloneLinuxOwnerAnchorExitProof`
      (`anchor_server.go:44-49` nils `HelperExit`): `Proof.LauncherExit.HelperExit`,
      `Proof.GateExit.HelperExit`, `Proof.HelperExit.HelperExit`,
      `PreGate.LauncherExit.HelperExit`, `PreGate.Proof.HelperExit.HelperExit` and the
      `AnchorStop` helper exits are nil in the clone, and
      `PreGate.Proof.LauncherExit.HelperExit` stays shared through the `PreGate.Proof` struct
      copy (`:1563-1567`). The drop is latent for today's producers because both evidence
      producers build helper-kind proofs whose exits are witnessed by the helper, so the
      nested `HelperExit` is nil: the pre-gate proof at `:861-869` and the full stop proof at
      `:983-990`. The receipt-ack copies at `:1173-1179` and `:1282-1288` are a separate path
      that truncates a possibly mirror-kind `StopReceipt` the same way. The clone would
      silently lose data if a producer ever starts carrying the single-level nested exit that
      the receipt shape allows. The review of branch `refactor/rf-w2-16-clone-aliasing-test`
      (2026-11-21) confirmed the `GateExit`/`LauncherExit` alias with a probe against the
      shipped clone - mutating the original's `LinuxProof.GateExit.HelperExit` changed the
      clone while the stored `HelperExit` copy stayed independent - and that goal's contract
      forbids production changes. The gate files
      `daemon/internal/platform/clone_aliasing_linux_test.go` and
      `clone_aliasing_linux_amd64_test.go` list every path above as an explicit
      `skipExitProofs` entry next to its reason; the driver fails a case whose listed path
      population never reaches, and it re-runs each case with every skip removed, failing any
      skip that no longer changes the outcome. Removing one of those entries before the
      matching production fix turns the covering subtest red:
      `TestCloneAliasingLinuxAMD64Evidence` for the helper-v2 and anchor paths,
      `TestCloneAliasingLinuxCgroupEvidence` for the StopReceipt paths.
      `TestCloneAliasingWindowsContainmentSessionID` does not share the Linux harness and is
      unaffected.

## Deferred review findings (PR #161)

- [ ] Retire the `effective_delivery/6` references left after PR #161 deleted the helper, and
      revalidate two callerless `@evidence_keys` entries as dead code (2026-11-21, branch
      `refactor/rf-w2-04-portal-json`). PR #161 deleted `effective_delivery/6`,
      `effective_delivery/4` and `delivery_context/3` from
      `control/lib/symmetry_control_web/portal_json.ex` (the work they did is now the private
      `resolve_delivery/1`), but `git grep -n effective_delivery` at the reviewed revision
      `c459b6a0` still finds the symbol in three schema descriptions —
      `contracts/v1/workspace-read.schema.json:225`,
      `contracts/v1/work-detail-read.schema.json:86`, `contracts/v1/chat-read.schema.json:168` —
      in their generated copies (`contracts/generated/ts/workspace-read.ts:122`,
      `contracts/generated/ts/work-detail-read.ts:96`, `contracts/generated/ts/chat-read.ts:144`;
      `daemon/internal/contracts/generated.go:238`, `:1510`, `:1888`;
      `daemon/internal/contracts/schema_bundle.json:8389`, `:39339`, `:44347`), in two frontend
      comments (`frontend/src/features/planning/write-policy.ts`, above `recordedDeliveryValue`;
      `frontend/src/services/api.fixtures.ts:44`) and in two plan files
      (`docs/plans/2026-11-21-portal-json-plan.md`,
      `docs/plans/2026-11-21-served-doc-gates-plan.md`), which are historical and stay. Reword the
      three descriptions and the two comments to drop the deleted symbol (or to describe the
      projection without naming the private `resolve_delivery/1`), then regenerate
      the DTOs with `pnpm contracts:generate` (`contracts/README.md:208`) and verify with
      `pnpm contracts:check`. Separately, `message` and `question` in `@evidence_keys`
      (`control/lib/symmetry_control_web/portal_json.ex:722`) are callerless: the getter's only
      call sites ask for `summary` (`:470`), `findings` (`:471`),
      `changed_artifacts`/`changed_files` (`:476`) and `tests` (`:478`), and
      `git grep -n '"message"\|"question"'` over
      `control/lib/symmetry_control_web/portal_json.ex` finds nothing, so treat both as dead-code
      candidates for a separate authorized cleanup. RV-11 was judged valid but out of scope by the
      fix stage (`docs/plans/2026-11-21-portal-json-plan.md:373-374`); found by the adversarial
      review of PR #161 (P3-1).

## Tolerated duplication: URL-state allowlists (refactor findings, 2026-11-21)

- tolerated — no tracked work (decision note, 2026-11-21, `refactor/rf-w2-04-portal-json`): the
      portal shell's URL-state policy is written twice — the `safeReturnTo` open-redirect allowlist
      (`control/lib/symmetry_control_web/portal_session.ex:5-79`: the per-parameter regexes at
      `:21-27`, the per-path clauses at `:46-79`) and the frontend shell router
      (`frontend/src/app/application.ts:46` `shellPath`, `:75` `shellQuery`, used by
      `safeReturnTo/1` at `:108`). The two spellings are kept in step by the shared corpus
      `contracts/fixtures/shell-return-targets.json` (read by
      `control/test/symmetry_control_web/portal_session_test.exs:372` and
      `frontend/src/app/application.test.ts:4`, deliberately outside the fixture manifest,
      `contracts/README.md:80`). Refactor findings CW-15 (`findings/control-web.md:450-471`) proposed
      one data table `%{path_pattern => [param_regex]}`; the portal-json contract ruled it out of
      scope (`portal_session.ex` is not one of its owned files, and the finding itself says the table
      is worth doing only when the shared corpus is extended). Accepted as tolerated duplication
      rather than scheduled work: folding the allowlist into one table changes no wire behaviour and
      buys nothing until a new shell parameter needs the corpus extended, which is the condition that
      should trigger it.

## Deferred review findings (PR #163)

- [ ] Fix the query fence's `machine_id` crash: a run-context query carrying `machine_id` raises
      `WithClauseError` — an unhandled 500 in production — where an unknown query key answers 400.
      The raise site is the `with` clause in `query_fence/1`,
      `control/lib/symmetry_control_web/controllers/daemon_controller.ex:514-516` (the
      `false <- Map.has_key?(query, "machine_id")` member at `:515`); the crash is pinned by
      `control/test/symmetry_control_web/controllers/protocol_controller_test.exs:1233`
      (`assert_raise WithClauseError`). One-line fix: drop that redundant member — the subset check
      already refuses `machine_id` as an unknown key — and flip the pin to a 400
      `invalid_request`. Behaviour is unchanged from base; the fix belongs to its own increment
      (plan CW-7 in `docs/plans/2026-11-21-rf-w2-03-controller-plumbing-plan.md`). Found by the
      adversarial review of PR #163 (P3-1).

## Deferred review findings (PR #137)

- [ ] Close the default-scope non-fixpoint the round-4 review of PR #137 found on
      U+002F-escape plus escaped-backslash literals, disclosed rather than fixed in that PR's
      round 5: re-rendering a literal whose decoded text puts a separator directly in front of a
      rendered token reads the separator and the token's text as a POSIX path and binds the
      token's own text into a new digest. Input (Python, `BS = chr(0x5C)`):
      `'{"k":"' + BS + 'u002F' + BS*2 + 'opx' + BS*2 + BS + 'u002faop"}'` renders to the escape
      plus `path-sha256:ef6452<...>` on the first render and to `path-sha256:50df1a<...>` plus
      `:` plus the first token's own hex on the second; the first render's claim starts at the
      escaped backslash run, so the decoded `/` the escape spells stays outside it. Neither
      render leaks a host byte, and the `record-cleanliness` gate runs `--check --root-only`
      (a fixpoint over the committed corpus), so both effects fail closed; the boundary is
      disclosed in the sanitizer's docstrings and `docs/evaluations/model-routing/README.md`
      (Explicit local limits). Fix direction: a token-text guard on the POSIX families' leading
      positions (the raw look already keeps rendered token text out of its digests) or extending
      a claim to the separator a raw match's start left outside it - whichever closes the shape
      without narrowing a real path out of the patterns, pinned by a second-render fixpoint test.
- [ ] Close the second disclosed non-fixpoint the round-5 review of PR #137 found: a record
      that already spells a rendered token can bind that token's own text on a later render, in
      either scope, because a separator run directly in front of the token is a location's start
      and the union binds the token text the match covers. Shape (Python, `BS = chr(0x5C)`):
      `'{"k":"//<disposable-root>' + BS + 'n<disposable-root>' + BS*2 + '"}'` renders to
      `//<disposable-root>` plus the newline escape plus `<disposable-root>` plus
      `path-sha256:b11b4c<...>` and re-renders to `path-sha256:a2c233<...>` plus
      `<disposable-root>` plus `path-sha256:a80e51<...>` plus the rest. The input carries no host
      byte, so the render over-redacts a token rather than leaking; the boundary is disclosed in
      the sanitizer's docstrings and `docs/evaluations/model-routing/README.md` (Explicit local
      limits). The round-6 review found two further residual shapes of the same fail-closed
      kind, confined to a `--root` render on a record that carries no token text of its own. In
      one a `file:` scheme, a control escape and then the root
      (`'{"k":"file:' + BS + 'r' + root + BS + 'n"}'`, where `root` is the synthetic drive-root
      spelling `'C:' + BS*2 + 'W'`) render as `file:` + the escape + `<disposable-root>` + the
      newline escape. In the other a space-continued chunk reaches the root
      (`'{"k":"store' + root + 'x/ ' + root + BS + 'n"}'`). Both are the fail-closed direction:
      nothing leaks (the residue is scheme text, an escape or a name glued to a word), the
      gate's `--check --root-only` refuses that first render (exit 1), and a second render
      converges - the third render is byte-identical - and passes the gate. Masked spellings
      only: `root` is synthetic and no digest is reproduced. Fix direction: the raw look's
      token-text filter keeps that text out of its own
      digests, but a separator run in front of a token is still a location's start; closing the
      shape must not narrow a real path out of the patterns, pinned by a second-render fixpoint
      test. The same closing options cover the two `--root` residue shapes: an anchored match
      that crosses a claim (starts before it, ends after it), under the same no-narrowing rule
      and the same second-render pin.

## Linux supervisor: remaining sticky-refusal paths after the owner-loss latch

The closed witness-stall entry (`docs/archive/CLOSED-TODO.md`, "Production Linux containment
witness: stop-receipt barrier stall awaiting a diagnosable occurrence", closed 2026-11-21 by commit
`e9c457d8`) latched owner loss at every `enterRecovery` entry; the review of that fix (PR #185
round 1) left two paths open here.

- [ ] Close the same sticky-refusal class for the lease-expiry arm. `ownerLostStop` in
      `(*linuxSupervisorSession).runStop` is still computed from `ownerLostLatched ||
      channelClosedV2(watchdog.ownerLost())` (`supervisor_session.go`); the lease-expiry arm of
      `(*linuxSupervisorSession).runHelperLoop` calls `session.stop()` without `enterRecovery`. If
      the owner dies or wedges as the lease expires and that timer arm wins the `select` before the
      control-EOF event, the final
      descendant scan persists over the dead metadata transport, the failure latches the sticky
      `descendantScanLost` (`process_unix.go`, the final-scan block and the monitor's
      `pre_scan_ack_failure` path), and every later stop is refused exactly as in CI 44876100236.
      Direction (root-cause §5 P2, kept in the local flake analysis `flake/root-cause-1.md`): do
      not latch sticky `descendantScanLost` on a final-scan *persistence* failure — return a
      retryable stop error with the authority retained so a later attempt (after the watchdog
      latch) seals through the owner-loss branch — and clear `descendantSealPending` when the
      observer seal write fails in `sealDescendantScanAfterMonitorStop`. This changes retry
      semantics and needs its own increment; the plan of the closed fix records it as out of scope.
- [ ] Give the in-process `recoverResponseLossStop` consumer the helper's owner-loss terminal seal.
      The live-owner entries of the latch (a daemon-side control close on response-transport loss,
      and response writes that fail before any pipe I/O) make the helper seal locally with no
      observer writes, so the durable helper progress keeps its pre-latch sequence; the restarted
      daemon records the seal returned over the recovery endpoint
      (`RecordSupervisorRecoveryScanSeal`, exact `Completed+1`) before the receipt, but the
      in-process consumer does not, `assert_all_sealed` refuses, and that stop fails closed into a
      durable containment-unproven mark. Pinned by
      `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed`; the amendment (record the
      returned seal before the mirror freeze, added once where the run-journal callback is plumbed)
      is deferred to the RunJournalRecorder lane — the callback is added once there instead of being
      hand-copied into every adapter — and the interim state stays fail-closed (PR #185 review
      round 1, finding F1; conductor decision 2026-11-21).

## Daemon: state validator dead branches, a nil-deref panic and the `validGoalSessionUUID` follow-up (rf-w2-09 survey)

- [ ] Consolidate `validGoalSessionUUID` (`daemon/internal/state/goal_session.go:2044`) into the
      exported wire-primitive validator family. It is a hand-rolled 36-character UUID check that
      stays local to `internal/state`; the W2-12a strict-decode/wire-primitive slice (program plan §5
      W2-12, "one validator family, exported/delegated; fix `validateProviderActionUUID`
      (version/variant)") owns that consolidation, so refactor slice rf-w2-09 (contract clause C7)
      left it unedited while decomposing the four validators around it. Reproduction:
      `git grep -n validGoalSessionUUID` matches the definition and call sites inside
      `goal_session.go` only; neither `authority` nor `protocol` exposes a UUID validator to delegate
      to yet. W2-12a also owns the `validateProviderActionUUID` version/variant bug named by the same
      row.

- [ ] Revalidate and remove six unreachable validation conditions (found 2026-11-21, branch
      `refactor/rf-w2-09-state-validators`; rf-w2-09 is move-only under contract clause C5, so every
      condition below was relocated byte-identically and is recorded here instead of being fixed in
      that slice). Reachability was probed by mutating one condition at a time against the rf-w2-09
      pin fixtures (probe code not committed); the affected strings are listed in
      `docs/evidence/rf-w2-09-state-validators-2026-11-21.md` §5.
      (a) `v2 cgroup reservation cannot accompany a v1 authority` (`state.go:6120`) and
      `v2 cgroup reservation cannot accompany a v1 handoff` (`state.go:6123`): the authority and
      handoff blocks (`validateContainmentAuthority`, `validateContainmentHandoff`) already return
      `v1 … cannot use a v2 cgroup reservation` whenever a non-v2 owner is combined with a cgroup
      reservation, so the later checks can never fire.
      (b) `Linux cgroup reservation cannot have both abort and physical-stop proof` (`state.go:259`)
      and `Linux cgroup aborted reservation lost producer-exit evidence` (`state.go:263`):
      `linuxCgroupReservationAbortMatches` (`state.go:5885`) returns false for every proof that
      carries a pre-gate receipt or lacks producer-exit parity, so the earlier abort-proof error
      fires first. Probes: aborted reservation + pre-gate receipt → `Linux cgroup reservation abort
      proof is invalid`; aborted reservation + helper exit proof → `Linux cgroup helper exit proof
      is invalid`.
      (c) `Linux cgroup reservation has effects before creation intent` (`state.go:272`): with
      `CreateStarted == false` every listed effect is shadowed by an earlier block
      (leaf→leaf identity, helper→helper identity, launcher→launcher identity,
      launcher exit→launcher exit proof, gate→gate identity, helper exit→helper exit proof,
      abort with a disposition other than `neverCreated`→abort proof). Probe: unstarted reservation
      + leaf identity → `Linux cgroup reservation leaf identity is invalid`.
      (d) `unresolved input command has an acknowledgement` (`state.go:6261`): it needs `pending`
      with `intent.Outcome == ""`, but `pending` compares against `acknowledgement.Outcome` and
      `validAcknowledgement` (`state.go:6427`) requires a non-blank `Outcome`, so an empty-outcome
      acknowledgement is rejected earlier with `run journal is invalid`. Probe: empty-outcome
      acknowledgement → `run journal is invalid`; an applied acknowledgement leaves the fixture
      valid.
      Fix direction: delete each condition (and its now-dead operand list) only after re-running
      these probes, since two of them (b, c) also decide which earlier block owns the first-error
      string; keep the surrounding block's other conditions.

- [ ] Revalidate and remove seven shadowed operands inside reachable validation conditions (found
      2026-11-21 by the rf-w2-09 fix review's operand-isolation sweep, branch
      `refactor/rf-w2-09-state-validators`). Each operand sits in a condition that does fire, but an
      earlier block or an earlier operand of the same condition already covers every journal that
      could satisfy it, so deleting it changes no verdict — a pin case cannot isolate it. Recorded
      instead of edited for the same move-only reason as the entry above.
      (a) `journal.ContainmentCgroup != nil` in `validateContainmentScans` (`state.go:6132`): a
      journal carrying both a cgroup reservation and containment scans is rejected earlier by
      `validateContainmentCgroup` (`state.go:6103` `journal.ContainmentScans != nil`).
      (b) `handoff.SupervisorPID <= 0` in `validateContainmentScans` (`state.go:6141`): the earlier
      scan block rejects `scans.SupervisorPID <= 0`, and `scans.matchesHandoff` equates the two
      supervisor PIDs, so a non-positive handoff supervisor PID can never be the deciding operand.
      (c) `journal.ContainmentAuthority != nil` in `validateContainmentHandoff` (`state.go:6080`):
      a valid authority always carries `TargetPID > 0` (`state.go:6059`) equal to the journal PID,
      so `journal.HasProcessDetails()` is already true in the same condition whenever the authority
      operand could matter.
      (d) `journal.ContainmentAuthority != nil` in the aborted / pre-gate cgroup preconditions
      (`state.go:6106`, `state.go:6110`): `validateContainmentAuthority` rejects a v1 authority
      beside a cgroup reservation, and a v2 authority whose cgroup carries an abort or pre-gate
      proof fails `linuxCgroupReservationMatchesAuthority` (it requires both to be nil), so the
      authority block always errors first.
      (e) `journal.ContainmentHandoff != nil` in the same two preconditions (`state.go:6107`,
      `state.go:6111`): a v2 handoff passes only when its cgroup matches it with a gate, a bound
      launcher grant and no pre-gate receipt, while an aborted cgroup must have no gate and no
      launcher grant (`linuxCgroupReservationAbortMatches`) and a pre-gate receipt requires no abort
      proof; a v1 handoff beside any cgroup always errors in the handoff block.
      Fix direction: re-run each argument as a probe before deleting; keep the surrounding
      condition's live operands.

- [ ] Fix the nil-pointer panic in `validateLinuxCloneCheckout`
      (`daemon/internal/state/state.go:5113`). It dereferences `*seed`
      (`linuxCloneCheckoutMatches(*intent, *seed, *checkout)`, `state.go:5124`) although
      `journal.LinuxCloneSeedReceipt` is only tested as `seed != nil && …` earlier in the same
      validator, so a journal carrying `linux_clone_checkout_receipt` without
      `linux_clone_seed_receipt` panics instead of failing validation. Verified 2026-11-21 with a
      temporary probe test on WSL (`PANIC_RECOVERED: runtime error: invalid memory address or nil
      pointer dereference`; probe removed afterwards). Because `readJournalEntryLocked` wraps
      `validateJournal`, a hand-crafted journal file can panic a `ListJournals`/`LoadJournal`
      caller. Fix direction: give the checkout stage its own requirement that a seed receipt exists
      (with its own first-error string) or make `linuxCloneCheckoutMatches` take pointers; both
      change a validation verdict, so they need their own regression test and cannot be folded into
      a move-only slice. Do not "fix" it by reordering existing conditions: that relocates
      first-error strings.

Decision note: rf-w2-09 left `validateJournalLeaseTerminalAndGrantInvariants`
(`state.go:6184`) at cyclomatic complexity 25 and `validateJournalKeyScalarsAndProcessDetails`
(`state.go:6036`) and `validateContainmentScans` (`state.go:6129`) at 24, against the wave's ≤25
acceptance. The next condition added to any of those three blocks must be split into a new helper
instead of appended.

## Daemon: `writeInputBounded` can strand its writer goroutine (audit F15)

- [ ] Give `writeInputBounded` (`daemon/internal/app/app.go:7240-7272`) an owned exit path for the
      writer goroutine it starts at `:7242` (`go func() { result <- process.WriteInput(input) }()`):
      the `ctx.Done` and timer arms return while the write can still be blocked in a stopped
      process's pipe, so the goroutine outlives the call, belongs to no WaitGroup, and contradicts
      `daemon/AGENTS.md` ("Every goroutine has an owner and exit path"). Found by the 2026-11-21
      daemon audit (finding F15). Fix direction: prefer the context-aware write path the runner
      already implements -- `internal/execution/runner.go:1400` `WriteInputContext` closes the stdin
      write end when its context is cancelled (`:1430-1434`) -- which `internal/app` cannot reach
      today only because its `Process` interface (`app.go:232-233`) declares `WriteInput([]byte)
      error` alone; otherwise close the pipe's write end on timeout (the runner owns the pipe), or
      track the goroutine in `daemon.workers` with a documented bound and assert its exit at
      shutdown. Pin it with a test that a blocked writer returns after timeout + terminate.
      Consequence carried meanwhile by refactor slice rf-w1-08: `internal/app` verifies goroutines
      per-test (`verifyNoLeakedGoroutines` in `daemon/internal/app/leak_check_test.go`) instead of a
      package-wide `goleak.TestMain`, because this path would force wildcard ignore lists that would
      hide real leaks.

## scripts/ci ruff baseline: drop the #137 exclude and keep the gate's ruff version pinned

- [ ] Delete the `extend-exclude` block in `pyproject.toml` once open PR #137
      (`fix/sanitizer-json-escape`, still open on 2026-11-21) is merged or abandoned. The excluded
      paths (`scripts/ci/model-routing-record-sanitize.py`,
      `scripts/ci/test-model-routing-record-sanitize.py`, and the sibling
      `scripts/ci/test-model-routing-record-sanitize-json.py` that #137 adds, named in the config
      ahead of its arrival) are the files #137 rewrites or adds, and the 2026-11-21 ruff baseline
      (`rf-w1-11`) excluded them so the reformat could not conflict with that branch. Measured
      pre-state (2026-11-21, ruff 0.17.5, `a3039f0` plus the committed config; ruff run on the
      explicit paths so the exclude cannot hide them): the two shipped files carry **5 findings**
      under the committed config (`E731` ×2, `E501`, `UP012`, `I001` — all in
      `scripts/ci/test-model-routing-record-sanitize.py`; `model-routing-record-sanitize.py`
      itself is clean) and `ruff format --check` reports `2 files would be reformatted`. The
      block may therefore only be deleted once #137 (or the follow-up that lands its content) has
      brought every then-present file in the excluded set to this baseline. After deleting the
      block, run `python3 -m ruff format scripts/ci` and `python3 -m ruff check scripts/ci` and
      expect both clean over all Python files under `scripts/ci` (52 once #137 lands, 51 today);
      until then `scripts/ci` is lint-clean over 49 of its 51 Python files. The exclude applies
      only to ruff's own file discovery: `ruff check scripts/ci` skips the excluded files, while
      `ruff check scripts/ci/model-routing-record-sanitize.py` (an explicitly named path) checks
      them anyway unless `--force-exclude` is passed.
The ruff half of this entry, the `ci.yml` ruff step installing ruff 0.17.5 exactly, is closed; the
closure record is the `## scripts/ci ruff baseline: drop the #137 exclude and keep the gate's ruff
version pinned` section of `docs/archive/CLOSED-TODO.md`.

## Deferred review findings (PR #154)

- [ ] Revalidate and remove nine test-only provider entry points (2026-11-21, branch
      `refactor/rf-w2-06-provider-support`; plan
      `docs/plans/2026-11-21-rf-w2-06-provider-support-plan.md`; found by the Opus review of
      PR #154 (P3-2)). `GitHub.check/1`, `GitHub.sync_resource/2`, `GitHub.sync_delivery/3`
      (`control/lib/symmetry_control/integrations/providers/github.ex:41,58,453`), the
      `AzureDevOps` twins (`azure_devops.ex:63,78,204`) and their new homes `Support.check/2`,
      `Support.sync_resource/3`, `Support.sync_delivery/4` (`support.ex:12,18,27`) have no
      production caller in `control/lib`: only the two provider suites call them. Evidence, run at
      `e4cac8d5`:
      `rg -n "provider\.(check|sync_resource|sync_delivery)\(" control/lib/symmetry_control/integrations.ex`
      matches only the auth-bearing callbacks — `:224` `provider.check(connection, auth)`,
      `:480`/`:619` `provider.sync_resource(connection, resource, auth)`, `:700`
      `provider.sync_delivery(connection, resource, item, auth)`;
      `rg -n "\b(GitHub|AzureDevOps)\.(check|sync_resource|sync_delivery)\(" control/lib` has no
      matches (exit 1); `rg -n "\bSupport\.(check|sync_resource|sync_delivery)\(" control/lib`
      matches only the six provider wrapper bodies themselves; and
      `rg -n "\b(GitHub|AzureDevOps|Support)\.(check|sync_resource|sync_delivery)\(" control/test`
      yields 17 call sites, 9 in `github_test.exs` and 8 in `azure_devops_test.exs`. Dynamic
      `apply/3` dispatch is not exhaustively ruled out, but every production dispatch goes through
      the `provider_call` closures above [inference]. Cleanup direction: migrate the tests to
      `authenticate/1` plus the auth-bearing callback arity, then delete the six provider wrappers
      and the three `Support` delegators, as a separately authorized change; `integrations.ex:690`
      already short-circuits a nil `pull_request_url`.

## Deferred review findings (PR #198)

- [ ] Cover the linux-gate wire validations no test kills (2026-11-21, branch
      `refactor/rf-w4-20-linux-gate-protocol-split`; found by the Opus review of PR #198, which is a
      pure move and kept them byte-identical). Single-point mutants of
      `daemon/native/linux-gate/protocol/wire.go` survive the whole package suite at base and HEAD
      for: `DecodeLaunchResult`'s `OwnerPID <= 1` and `UserNSInode == 0` checks, `readWireString`'s
      NUL check and `maxCommandString` bound, the receipt generation upper bound and minimum length,
      and `encodeGuardedRelease`'s body and grant bounds. The generation and minimum-length checks may
      be shadowed by later reads or `receipt.Validate`; the rest are real gaps. Add one rejection test
      per check in a separate maintenance slice.
- [ ] `scripts/ci/linux-v2-native-acceptance.sh:1490` pins
      `TestInstalledLinuxOwnerRejectsUnexpectedCgroupKillFD`, which no Go file defines (base and
      HEAD); owner: the acceptance-script maintainer — restore the test or drop the pin.

## Deferred review findings (PR #205)

- [ ] STD-A09 from the Opus review of PR #205 (2026-11-21, branch
      `refactor/rf-w4-14-browser-fixtures`): the merged #188 test-observability code loses the
      `(partial)` label on a timeout-truncated package once any test in that package failed.
      `scripts/ci/go-test-durations.py:213` computes
      `unexplained = [package for package in failed_packages if not any(key[0] == package for key in failed)]`,
      so a package that has a failed test counts as fully explained even when its stream is cut
      off by a timeout before the remaining tests ran. The step is `continue-on-error` today, so
      there is no gate impact, but the W2-01/W6-12 follow-up that flips the thresholds to blocking
      must fix the partial-run detection first.
- [ ] The merged W2-01 plan (`docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md:363`)
      misstates the run: it records the checks as run "with the pinned ruff 0.17.6", which
      `pyproject.toml:6` `required-version = "==0.17.5"` makes impossible (the config refuses any
      other version, exit 2). The real conflict (`pyproject.toml` `==0.17.5` vs `ci.yml`'s
      `ruff_version`) was resolved by rf-w6-01 (PR #237), which pinned the step to 0.17.5; that
      tracked entry now lives under
      `## scripts/ci ruff baseline: drop the #137 exclude and keep the gate's ruff version pinned`
      in `docs/archive/CLOSED-TODO.md`. This entry only records the misstatement in the merged
      evidence plan (found by the Opus review of PR #205).

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
      the `state` copy returns a bool and accepts only versions 1–5 and RFC 4122 variants
      (`8`/`9`/`a`/`b`), while
      `control.validateProviderActionUUID` and `protocol.validateUUID` use other strings. Pin
      every current rejection string per call site before delegating, so the unification changes
      no message; `pi/provider_bridge.go:1102-1120` stamps v5 UUIDs, so the stricter check is
      safe for journaled action IDs.

- [ ] Consolidate the frozen `opencode` duplicates onto the shared harness helpers when the D1
      freeze lifts with opencode capability work (W6-04) (2026-11-21, same branch and plan, §8).
      `opencode/**` stayed read-only (P2-3/D1: its six CI assertions and the release-acceptance
      case stay green), so every surface this slice consolidated elsewhere still has an
      `opencode` copy: process forwarding with its own guards
      (`daemon/internal/harness/opencode/adapter.go:522` ProcessDetails, `:538`
      LinuxV2LiveOwnerStatus, `:557` LeaseRenewalAvailable, `:568` RenewLease),
      `isNilNativeProcess` (`:400`, `reflect.Pointer`-only), the Close/join state machine
      (`:1042`), `emit` (`:1323`, stamps `At` and calls `sink.Handle` directly instead of
      `harness.DeliverEvent`) and `Wait` (`:954`). Consolidating them is part of opencode
      capability work, not this increment, and must keep those CI oracles green.

## pi provider-bridge close-retry timing flake under host load (PR #155 review)

- [ ] Revalidate the pre-existing host-load flake in
      `TestProviderBridgeCloseRetryClearsUnprovenAfterExecutorExits`
      (`daemon/internal/harness/pi/provider_bridge_test.go:941`): it failed once in 26
      executions during the PR #155 round-2 review — 1 of 3 full parallel
      `./internal/harness/...` runs, 0 of 20 isolated `-count=20 -run` runs and 0 of 3
      package-only `./internal/harness/pi/` runs, so reproduction needs full-suite load
      (2026-11-21, branch `refactor/rf-w2-13-harness-scaffolding-5a`; plan
      `docs/plans/2026-11-21-rf-w2-13-harness-scaffolding-5a-plan.md`). The test's first `Close`
      runs under a 25 ms `context.WithTimeout` (`:968-969`) and must return
      `errProviderBridgeExecutionUnproven` (`:971`); the failure is consistent with the deadline
      winning under load before that verdict is stored, but the cause is inferred, not proven,
      because the failure message was not captured. The flake is pre-existing: the test and
      `pi/provider_bridge.go` are outside this PR's diff (the test file was last changed in
      `8082317b`, #44), and the sibling
      `TestProviderBridgeCloseReportsIgnoringExecutorAsUnproven` (`:904`) uses the same 25 ms
      first-`Close` window (`:924`). Revalidate against a recovered failure before changing the
      window or the assertion.

## Daemon platform: anchor socket-error sanitation keeps its outermost-only type switch

- [ ] Follow-up error-sanitation slice: resolve the remaining `errorlint` finding in
      `daemon/internal/platform/anchor_wire.go:925` (`:346` at the merge base; the coordinate
      moved with `rf-w3-02`'s request-shape table in the same file)
      (`sanitizeLinuxOwnerAnchorSocketError`'s `switch typed := err.(type)`). The slice
      `rf-w2-11-typed-errors` fixed the other owned-file finding
      (`daemon/internal/platform/anchor_server.go:305`, the direct `AcceptUnix` error) by
      converting it to `errors.As` — behaviour-neutral because `net.(*UnixListener).AcceptUnix`
      returns either a bare `syscall.EINVAL` (`!l.ok()`, `Timeout() == false`) or an
      `&net.OpError{...}` that is already the outermost `net.Error` — but deliberately left this
      one. The sanitizer rebuilds only the **outermost**
      `*net.OpError`/`*os.PathError`/`*os.SyscallError` and returns a nested or joined error
      untouched, which `errors.As` cannot express without an identity check (`asErr == err`)
      that obscures the sanitizer's shape; a `//nolint` is forbidden by the refactor program's
      rules. Measured with `GOOS=linux GOARCH=amd64 golangci-lint run --no-config --default=none
      --enable=errorlint ./internal/platform/...` (v2.15.0) on 2026-11-21: 8 findings at the
      merge base `c87c9918` (this one and `anchor_server.go:305:27`, which the slice then
      fixed, plus six outside the owned set), and 7 after the fix, of which exactly this one is
      in the owned files (`:925:18`). The follow-up should either restructure the
      sanitizer so the "outermost only" rule is explicit in a form errorlint accepts and get the
      count to zero, or record the accepted shape in the plan that owns error sanitation.

## Daemon platform: mismatch-sentinel granularity and anonymous synonym sites

- [ ] W3-02 (or a dedicated follow-up over the same files): collapse the near-synonym sentinel
      families and adopt the anonymous synonyms left by `rf-w2-11-typed-errors`. At the 2026-11-21
      revision the slice had to keep every `Error()` byte identical (C3), so one predicate is now
      split four ways for process identity (`ErrLinuxOwnerCreationIdentityChanged`,
      `ErrLinuxOwnerCreationIdentityDiffers`, `ErrLinuxOwnerIdentityChanged`,
      `ErrLinuxOwnerIdentityDiffers`), two ways for nsfs device/inode
      (`ErrLinuxOwnerDeviceOrInodeChanged`, `ErrLinuxOwnerDeviceOrInodeDiffers`), and two ways
      for the parent check (`ErrLinuxOwnerParentPIDDiffers`,
      `ErrLinuxOwnerParentDiffersFromHelper`); a caller that wants "process identity changed,
      not transport loss" must OR four values and still cannot see the anonymous sites. Those
      synonym sites, verified at this revision: `linux_owner_helper_v2_linux.go:785`
      (`daemon creation identity changed`), `:799` (`helper identity differs`), `:887`
      (`namespace identity differs`), `helper_v2_protocol.go:361`
      (`PID or creation identity differs`); the parent-class synonyms are
      `anchor_gate.go:380`, `:417`, `anchor_run.go:278`, `helper_v2_run.go:122`, and
      `helper_v2_recovery.go:878` (`:830` at the merge base; the coordinate moved with
      `rf-w3-02`'s call-site codemod in the same file). The PR #160 review's suggested fix keeps every byte: one
      exported class sentinel per semantic class, backed by an unexported
      `type linuxOwnerMismatch struct { text string; class error }` whose `Error()` returns
      `text` and whose `Is(target)` reports `target == class`, so each phrasing keeps its exact
      wire text and joins its class.

## Deferred review findings (PR #193)

- [ ] Pin two containment close-path behaviours that the rf-w4-03 runner decomposition left
      untested (2026-11-21, branch `refactor/rf-w4-03-runner-decomposition`; plan
      `docs/plans/2026-11-21-rf-w4-03-runner-decomposition-plan.md`). Two mutants on a scratch
      copy over `./internal/execution/` stayed green under WSL: flipping the `closeOwnerOnce(false)`
      argument in `closeContainmentOwnerLocked` (`daemon/internal/execution/runner.go:2413`) to
      `true` — no test distinguishes the flag, and the pending-handoff owner does not implement
      `containmentUnprovenCallbackSetter`, so a future edit could flip it silently — and swapping
      the `containmentAuthorityUncertain` / `containmentAuthority != nil` guards in
      `persistReceiptOrAbort` (`:2163`, `:2166`), whose check order is the base order
      (pre-existing gap: base had the same untested order). Behaviour is correct at HEAD; this is
      test coverage only, no code change. Found by the opus round-1 review of PR #193. Owner:
      W4-12/W4-13 test work.

## Deferred review findings (PR #189, provider access lock order)

- [ ] Pin `IntentContext`'s intent-context lock order with an oracle (2026-11-21, branch
      `refactor/rf-w4-06-provider-access-intent`; plan
      `docs/plans/2026-11-21-rf-w4-06-provider-access-intent-plan.md`; found by the Opus review of
      PR #189 (P3-4) as review STD-A02, report-only per conductor decision E8).
      `IntentContext.legacy_lock/1`
      (`control/lib/symmetry_control/integrations/provider_access.ex:1147-1150` with
      `lock_scope_rows/2` `:1168-1184`) and `IntentContext.goal_lock/2` (`:1152-1164`) hold the
      merged lock order, but no test drives them: the only order assertion is the controller case
      "Goal provider claim scope locks Project before Goal"
      (`control/test/symmetry_control_web/controllers/provider_action_controller_test.exs:400`, its
      `FOR SHARE` telemetry probe at `:431`), and it drives `ProviderAccess.lock_claim_scope/1`
      (`:442`) instead. Measured during the slice: swapping Project and Goal in `goal_lock/2`, or
      reversing `lock_scope_rows/2`'s rows, keeps the 13 targeted tests and the 79 controller tests
      green. Add a controller or `ProviderAccess` test that pins the `goal_lock/2` and
      `legacy_lock/1` order with telemetry, mirroring `:400`, and record its red mutation.

- [ ] Fold `lock_goal_claim_scope/1`'s third Goal-order encoding into `IntentContext` (2026-11-21,
      same branch and plan; the plan's "Out of scope" section records it).
      `lock_goal_claim_scope/1`
      (`control/lib/symmetry_control/integrations/provider_access.ex:379-400`, comment `:375-377`)
      walks Project `FOR SHARE` -> Goal -> resources/connections (`lock_claim_resources/1`
      `:412-437`) -> WorkItem -> Task -> revision inline — the same order
      `IntentContext.goal_lock/2` (`:1152-1164`) now encodes once — but returns the claim-scope
      shape rather than the intent-context map, so the slice left it; folding it must keep the
      lock-order case `:400` and the advisory-lock case `:2092` green.

## Deferred review findings (PR #195)

- [ ] Drop the host path from `TestPinnedNativeTranscript`'s fixture comparison before the next OmO
      fixture re-record (2026-11-21, PR #195, branch `fix/ci-evidence-host-paths`). PR #195 removed
      the committed executable path from `scripts/ci/omo-native-evidence.py`: `PINS[platform]` keeps
      only `size` and `sha256`, and the operator names the file in `SYMMETRY_OMO_BINARY`, so a
      receipt written by the updated witness carries `binary.independentPlatformPin` **without** a
      `path` member. The fixture consumer still requires all three: `transcriptBinary` is
      `{Path, Size, SHA256}` (`daemon/internal/harness/omo/transcript_test.go:16-20`), embedded as
      `IndependentPlatformPin` at `:26` and compared whole against the per-GOOS expectations at
      `:46`/`:52`, so `:73` answers `fixture does not bind the independent <goos> executable pin`
      for any fixture re-recorded from the new receipt. The committed fixtures
      (`daemon/internal/harness/testdata/omo/5.1.69/{windows,linux}.json`) are unchanged and CI is
      unaffected today. The Go change — compare the pin's size and digest, not its `path` — is
      outside PR #195's scope, whose "Left unchanged" note treats the recorded pins as provenance.
      Owner: whoever re-records the OmO fixture. The opt-in native acceptance run pins the same
      host path separately (`daemon/internal/harness/omo/native_test.go:190-200`, resolved at
      `:219`) and does not read the fixture, so a re-record does not touch it.

## Control full-suite wall-time target and the D9 migration-file pin

- [ ] Own the Control `mix test` wall-time target (~102 s → ≤65 s, program-plan row W3-14)
      (2026-11-22, branch `refactor/rf-w3-14-exunit-async`, plan
      `docs/plans/2026-11-21-rf-w3-14-exunit-async-plan.md`; PR #211 review P2-1 under conductor
      amendment 2). The rf-w3-14 conversion could not carry it: the nine D9-pinned sync migration
      files carry 58.4 % of the suite's work (282.0 s of 482.6 s of per-test work at `d0761739`,
      with `Goal0006MigrationTest` 180.8 s bounding any async phase), and ExUnit concurrency is
      module-granular. CI `coverage-control` on the branch measured 989 tests in 140.2 s =
      18.1 s async + 122.0 s sync on the 4-scheduler runner, and main's `control` job spans
      73.0-159.4 s sync against 14.7-22.9 s async. Needs a conductor D9 re-evaluation of the
      migration-file `async: false` pin (their DDL already runs through a private schema, so the
      pin, not the fixture shape, now bounds the async phase) before any slice can own the target.
      W3-15 cannot: its contract has no wall-time clause and `goals_test.exs` stays `async: false`
      as an app-env mutator.

## Deferred review findings (PR #197)

- [ ] Unify the three copies of the runtime harness-kind vocabulary left by the rf-w4-19
      `linuxpreflight` extraction (2026-11-21, branch `refactor/rf-w4-19-linuxpreflight-package`;
      found by the round-1 review of PR #197 (P3-2)). The extraction could not move the vocabulary
      out of `internal/app`: `configuredHarnessKind` and `nativeGoalHarnessKind` are copied from
      `internal/app/probe.go:123,156` into `daemon/internal/linuxpreflight/cgroup_linux.go:31-63`,
      and the same seven-kind table exists a third time in the config loader
      (`internal/config/config.go:321-323`, message at `:323`, byte-identical to
      `internal/app/probe.go:140`). Only the "new kind" direction fails closed: a kind the copy does
      not know is rejected by `verifyLinuxNativeRuntimeSelection`, never admitted; if `app` dropped a
      kind from its native-goal set, preflight would still accept it — though the runtime would still
      refuse it, so this is not an authority gap. The pin test
      `TestNativeRuntimeSelectionVocabularyMatchesTheConfiguredKinds`
      (`daemon/internal/linuxpreflight/preflight_linux_test.go:326`) compares the copy against a
      hard-coded literal table, not against `app`'s copy, so drift between the two copies goes
      undetected. Fix direction: one `internal/harness` vocabulary — `ParseKind` plus the native-goal
      predicate — consumed by `internal/config`, `internal/app/probe.go` and
      `validateConfiguredHarness` (`internal/app/goal_admission_tx.go`) and `linuxpreflight`, with
      the pin test reading that shared table instead of a literal. That touches `internal/harness`,
      `internal/app/probe.go` and `internal/app/goal_admission_tx.go`, all
      outside the rf-w4-19 owned set (plan
      `docs/plans/2026-11-21-rf-w4-19-linuxpreflight-package-plan.md`, "The runtime-selection
      vocabulary"), so the slice kept the copy and recorded this entry instead.

## Deferred review findings (PR #200)

- [ ] Harden the goal-session uncertain-reason budget against invalid UTF-8 at the store
      boundary (`daemon/internal/state/goal_session.go:1034` write check vs `:1670` read
      validation; 2026-11-21, branch `refactor/rf-w3-07-state-error-causes`, PR #200 review
      P2-1). A within-budget reason containing invalid UTF-8 bytes passes the write check
      (`validRequiredString` is length-only), but the journal's JSON round trip replaces each
      invalid byte with U+FFFD (3 bytes), so the reloaded `UncertainReason` can exceed the
      4096-byte budget and `LoadGoalSession` then rejects the whole journal
      (`invalid goal session journal`), making the session unreadable after a mark that was
      accepted — e.g. a 4096-byte reason with one invalid byte reloads at 4098 bytes. The same
      hazard exists on base `52bdffd2`; PR #200 fixed only the reason strings the app composes
      (`boundedGoalSessionUncertainReason` now runs `strings.ToValidUTF8` over the over-budget
      prefix), because the W307-R1 decision forbids changing the sink. Owner: state store. Fix
      direction: reject or normalize non-UTF-8 reasons in
      `MarkGoalSessionUncertain`/`validateGoalSessionStates` the way the app bound does.
      Reproduction: `MarkGoalSessionUncertain(key, strings.Repeat("r", 4095)+"\xff")` succeeds,
      then `LoadGoalSession(key)` returns `invalid goal session journal`; the app-side callers
      are pinned by `daemon/internal/app/goal_delivery_test.go`:
      `TestAbandonGoalSessionBoundsOversizedUncertainReason` (the interior-invalid-byte case)
      and `TestBoundedGoalSessionUncertainReason` (the within-budget invalid-byte case).

## Daemon state: test-only `ControlProjection` surface (rf-w3-08 survey)

- [ ] Revalidate and remove `GoalSessionJournal.ControlProjection`
      (`daemon/internal/state/goal_session.go:412`) together with its
      `goalSessionControlProjection` type (`:308`, unexported by rf-w3-08). `git grep -nw
      ControlProjection` matches only the method definition, the doc comment at `:411`, the doc
      comment at `:185` that promises callers ("callers must use ControlProjection when building a
      control request") which do not exist, and two in-package test call sites
      (`goal_session_test.go:707`, `:1340`); there is no production consumer at this branch's head
      or at its base. It was not deleted in the rf-w3-08 apply stage because the test that pins it,
      `TestGoalSessionControlProjectionOmitsRawNativeIdentity`, is a contract-pinning proof that
      the projection carries no raw native identity — deleting the surface would retire that test.
      A follow-up owner must decide between wiring the intended production consumer and retiring
      the surface together with its test under an explicit contract amendment.
- [ ] The same retention question covers the other exported state methods whose signatures name
      now-unexported types: `Store.CheckGoalSessionLineage`
      (`daemon/internal/state/goal_session.go:576`, `goalSessionLineageStatusValue`) and
      `Store.QueueLateGoalUsage` (`daemon/internal/state/goal_delivery.go:829`,
      `lateGoalUsageLedger`). Both stay legal Go through inference (`status, err :=
      store.CheckGoalSessionLineage(key)`), neither has an out-of-package caller at this branch's
      head, and rf-w3-08 kept them rather than widening its slice; this entry tracks the retained
      surface so a follow-up owner can decide unexport/retire/wire for both together.
      `Store.EnsureGoalSessionLineageAvailable`, the same shape, was removed by PR #215 (the
      rf-w1-06 dead-code candidate above).

## Deferred findings (rf-a4 core work-item eligibility, 2026-11-21)

- [ ] Unify the allowed-actions predicate the Goal authority policy is read through: the two
      spellings are `provider_operation_allowed?/2`
      (`control/lib/symmetry_control/goals.ex:9982-9985`, `operation in
      allowed_values(revision.authority_policy, "allowed_actions") and operation in
      allowed_values(revision.execution_policy, "allowed_actions")`) and `goal_action_allowed?/2`
      (`control/lib/symmetry_control/integrations/provider_access.ex:607-609`, the same conjunction
      through `goal_allowed_actions/1`). Out of scope for rf-a4 (2026-11-21, branch
      `refactor/rf-a4-core-work-item-eligibility`): both files are owned by other chains — the
      `goals.ex` region by W4-07/W5-14 and `provider_access.ex` by W4-06 (#189) — and the
      slice's failure control forbids touching them. The unification is one predicate over
      `(revision, operation)` with one `allowed_actions` reader, whichever chain lands last owns
      it.

- [ ] Move the chat work-item projection into core (W5-15, control half): the chat read model
      reaches web-side code instead of the shared core verdicts —
      `control/lib/symmetry_control/chat/read_model.ex:10,46` calls
      `SymmetryControlWeb.PortalJSON.work_item_detail/2` and then overrides the five capability
      members (`:51-65`: `can_cancel`, `can_retry`, `can_guide`, `can_pause`, `can_resume`, with
      `historical: true`), and `control/lib/symmetry_control/chat.ex:19` aliases
      `SymmetryControlWeb.Protocol` for `normalize_map/1` (`:26`, `:68`). Deferred by rf-a4
      (2026-11-21, same branch): the slice's owned set excludes `chat/**`, and W5-15 owns the
      inversion plus `decision.lock_version`, consuming the new
      `SymmetryControl.Workspaces.WorkItemEligibility` for the chat override. The served chat
      document is unchanged by rf-a4.

## Deferred review findings (PR #213)

- [ ] Rename the cleanup memo `runningRun.cleanupBlocked` and move it into `runLifecycle` under a
      distinct name (PR #213 review r1, P3-1; plan
      `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md`, D1). The memo is an independent
      field that `markCleanupBlocked` and `clearCleanupBlock` set directly, separate from the
      derived `cleanupBlocked()` predicate; a field cannot share the method's name on one type,
      so the memo kept its base name outside the module. D1 measured option (a) at 86 test-file
      lines in 17 files at base (42 fixture-literal keys and 44 assertion/read lines), so the
      rename needs that mechanical rewrite plus the write-site scan's field-list update.

## Daemon: the durable terminal-transition payload has no capacity policy (rf-w3-09 review W309-R2)

- [ ] Decide and implement a capacity policy for the durable terminal-transition payload. Chain:
      `queueFailure` (`daemon/internal/app/events.go:530-541`) puts `cause.Error()` into
      `payload["error"]`; `queueTerminalTransitionWithRetry` (`daemon/internal/app/outbox.go:124-168`)
      retries `state.ErrProviderActionCapacity` forever (`minimumInterval`, log
      `queue_terminal_transition_failed`); the capacity error comes from
      `saveJournalWithCapacityGuardLocked` (`daemon/internal/state/state.go:5326`) →
      `validateJournalSaveCapacity` (`daemon/internal/state/provider_action.go:594-606`, 4 MiB check
      at `:599`) and `saveJournalLocked` (`state.go:5305`, size check at `:5311`;
      `maxJournalFileBytes = 4<<20`). rf-w3-09 (2026-11-22, drop-errcheck slice) joined the abandon
      result into the nine `startGoalAdmission` returns, and the `goal_admission` `queueFailure`
      call in `daemon/internal/app/app.go` passes that value through unchanged (read from source,
      not driven): a run whose pre-change
      payload fit by fewer bytes than the joined tail can now exceed the guard and never persist its
      terminal state, where base `261e84a4` persisted the same input. Review probe at the state
      boundary: a 4,194,116-byte journal accepts the transition and a +298-byte tail returns
      `ErrProviderActionCapacity`; re-verified here: `json.Marshal`'s HTML escaping makes each `<` in
      an error string cost 5 payload bytes (`json.Marshal(map[string]string{"error": strings.Repeat("<", 1000)})`
      = 6,012 B vs 1,012 B for `a`), and `APIError.Error()` embeds the Control message up to the 1 MiB
      read cap (`daemon/internal/control/client.go:22` `defaultMaxResponseBytes = 1 << 20`). Fix
      direction: either a capacity policy for `payload["error"]` (bounded durable copy with a marker,
      returned chain keeps the full text) or an explicit degraded outcome for capacity errors instead
      of the unbounded retry. The sink files are outside the rf-w3-09 owned set (`events.go`,
      `outbox.go`, `state.go`), which is why that slice recorded this entry instead of changing them;
      owner: daemon outbox/state.

## Daemon platform: the Windows witness hooks stay outside the Linux witness config (rf-w5-01)

- [ ] Consolidate the Windows pre-authority witness hooks the way rf-w5-01 (2026-11-22, branch
      `refactor/rf-w5-01-witness-converge-in-place`) consolidated the Linux surface. The Windows
      side still reads raw literals with no witness gate —
      `supervisor_windows_helper.go:554-555` (`SYMMETRY_PREAUTHORITY_DROP_FIRST_HELLO_RESPONSE`,
      `SYMMETRY_PREAUTHORITY_HELLO_RETRY_MARKER`) and `supervisor_windows_client.go:1080`
      (`SYMMETRY_PREAUTHORITY_WRONG_SECRET_MARKER`) — and
      `cmd/symmetry-daemon/preauthority_witness_command_windows.go` enters witness mode from argv
      (`init()` `:26-36`) while setting the same three names for the helper child (`:142-148`).
      Conductor decision A (2026-11-21) scoped rf-w5-01 to the Linux family and left these bytes
      for W6-03, which already owns the same files and dispatches after rf-w5-01; the Linux half
      now has one per-use witness-config parser
      (`daemon/internal/platform/supervisor_linux_witness.go`) behind
      `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` plus marker-parent validation, so the two
      conventions coexist until W6-03 lands.

## Daemon platform: two daemon-side barrier markers lack witness parent validation (rf-w5-01 residual)

- [ ] Validate the marker parent directory for the two daemon-side witness writers that still
      publish env-supplied marker paths unvalidated: the stop-receipt barrier reached marker
      (`daemon/internal/app/cleanup_glue.go:232-233` reads
      `SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_{REACHED,RELEASE}_FILE`, written at `:259`)
      and the shared resume-barrier writer (`daemon/internal/execution/runner.go:1077`
      `waitForResumeBarrier`, publishing the reached marker at `:1114`), which serves both the
      commit-resume family (`SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_{REACHED,RELEASE}_FILE`, caller
      `:1012`) and the lease-armed family
      (`SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_{REACHED,RELEASE}_FILE`, callers `:909,919` via
      `waitForLeaseArmedResumeBarrier`).
      Both are pre-existing and outside rf-w5-01's owned set (rf-w5-01 review CR-06); the platform
      helper funnels are covered by `validateLinuxSupervisorWitnessMarkerParent`.
      owner: daemon app/execution.

## Daemon: the `admission_deadline` attach-ack subtest flakes on its wall-clock deadline (PR #217 review)

- [ ] Revalidate and diagnose the pre-existing flake in
      `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline`
      (`daemon/internal/app/omo_attach_ack_test.go`; the `admission_deadline` arm sets a 2 s
      wall-clock admission `DeadlineAt` and then requires a loopback GET+PUT inside it). The PR #217
      review observed it fail twice on `394859a9` (once under concurrent load, once alone) with
      `assertRequests`'s `attachment requests=[] … context deadline exceeded`, then pass 2/2 alone
      and 3/3 on the PR head. Failures take one of two shapes: the attach GET leg hits the deadline
      (`native_calls=[start details open details close wait]`), or the admission check fires first
      (`Goal admission deadline has elapsed before native launch`, `resolveGoalAdapter` in
      `goal_admission_tx.go`); both are recorded under load in
      `docs/evidence/rf-w4-03-runner-decomposition-2026-11-21.md`
      (the app-suite failure paragraph). The failure is consistent with the deadline expiring when
      the suite is starved, but the cause is unconfirmed. The arm's duration is bounded by a
      production limit, so a fix needs an owner decision on the deadline or the assertion;
      revalidate against a recovered failure before changing either.

## Frontend test double: the WebSocket interception trap (rf-w4-11)

- [ ] A suite that seeds a `socket_token` in the shared portal double (`frontend/src/app/portal-double.ts`)
      makes the shell's invalidation client open a real socket, which MSW's node server intercepts as an
      unhandled **WebSocket** frame: under `listen({ onUnhandledFrame: "error" })` every incidental connect
      prints an `intercepted a WebSocket connection without a matching event handler` block and can leak the
      app's token-refresh reads into the next case. `frontend/src/app/router.test.tsx` solves it locally by
      passing an inert `socketFactory` to `createApplication` by default (`renderWorkspace`). If a later
      suite needs a token without exercising the socket, do the same, or extend the double's listen options
      so WebSocket frames stay quiet while HTTP paths keep failing loudly. This is deferred, not out of
      scope: both files a fix touches (`frontend/src/test-setup.ts`, `frontend/src/app/portal-double.ts`)
      are in rf-w4-11's owned set; no suite seeds a token yet, and an unexercised `ws` path would be
      unproven behaviour in a slice whose executed-case count is pinned.

## Comment-policy sweep: debt the deleted comments recorded (2026-11-22)

The comment-policy sweep (plan `docs/plans/2026-11-22-comment-policy-sweep-plan.md`, evidence
`docs/evidence/comment-policy-sweep-2026-11-22.md`) deleted program-added comments that were the
only in-source record of the following out-of-scope debt, per the owner policy's rule 2. None of it
is fixed here; each bullet names what a follow-up has to re-derive.

- [ ] `daemon/.golangci.yml`: the deferred lowercasing of the genuinely capitalized `ST1005` error
      strings, and the `dupl` threshold pinned at 150 with the intent to drop toward 100 once the
      dedup slices land (4 module-wide findings motivated the pin). Only the exclusion rationale
      survives compressed in the file.

- [ ] `frontend/eslint.config.js` / `frontend/vite.config.ts`: the two remaining
      `@typescript-eslint/dot-notation` findings are `process.env["NAME"]` accesses in
      `vite.config.ts`; the `warn` entry and its `// 2` note feed the ratchet, so fixing the accesses
      and dropping the entry must happen together.

- [ ] `pyproject.toml`: the three `scripts/ci/*model-routing-record-sanitize*` files stay in
      `extend-exclude`; the entry's premise (see "scripts/ci ruff baseline: drop the #137 exclude")
      no longer holds after #137 — the landed files carry 2 `PERF401` findings and 67 `ruff format`
      hunks across the three (`model-routing-record-sanitize.py` 18,
      `test-model-routing-record-sanitize.py` 24, `test-model-routing-record-sanitize-json.py` 25;
      re-measured with ruff 0.17.5), so dropping the exclude now reformats them rather than leaving
      them clean.

- [ ] `.github/workflows/ci.yml`: shellcheck-enabled `actionlint` still does not terminate on the
      Windows development host (reproduced while running this sweep's gate), and Credo still prints
      the same numbered list for files skipped over `parse_timeout` and for unparsable files, so the
      strict baseline can deflate even with the `credo_strict_unparsed_files` guard printing.

- [ ] `daemon/internal/linuxpreflight/cgroup_linux.go`: `configuredHarnessKind` is a verbatim copy
      of the app-side vocabulary owner (`internal/app/probe.go`) and the config loader's set; the
      package test pins a literal table, not the app-side copy, so drift is undetected. The package
      cannot import `internal/app`.

- [ ] `daemon/cmd/symmetry-fake-agent/autonomous.go`: on a carrier whose `Close` cannot unblock a
      parked read (the inherited stdio pipe), the reader goroutine is not joined before the run
      returns; the bounded join caps the wait and the goroutine exits with the process.

- [ ] `daemon/internal/platform/supervisor_linux_helper.go`: the in-process consumer of
      `recoverResponseLossStop` does not record the helper's authenticated recovery scan seal, so
      such a stop fails closed into a durable containment-unproven mark; only the restarted-daemon
      path records it (`TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed` pins the
      behaviour, not the missing seal record).

- [ ] `daemon/internal/app/leak_check_test.go`: there is deliberately no package-wide goleak
      `TestMain` while the "`writeInputBounded` can strand its writer goroutine (audit F15)" entry
      above stands; per-test leak verification is the interim oracle.

- [ ] `daemon/native/linux-gate/protocol/wire_test.go`: the dedicated "start time is truncated"
      decode guard is unreachable (the size guard already refuses those packets), so no test can
      cover that branch.

## Contracts: the evidence-batch response and conflict-details duplicate identities need independent enforcement (rf-w6-08 review REV-02/P3-1)

- [ ] Restore cross-item uniqueness for the evidence-batch response and conflict-details envelopes
      on top of the request correlation. The daemon-side parsers that rejected duplicate receipt
      `id`/`evidence_key` and duplicate conflict index/`evidence_key` were deleted with the dead
      goal-envelope family (rf-w6-08, 2026-11-22). The daemon's only consuming path still rejects
      duplicate receipt identity transitively: `validateGoalEvidenceBatch`
      (`daemon/internal/control/validation.go`) rejects duplicate request `evidence_id`/`evidence_key`,
      and `validateGoalEvidenceBatchReceipt` compares each receipt's `id`/`evidence_key` per index
      against that uniqueness-validated request, so a duplicate response can only match a request the
      daemon already refused. What is missing is enforcement independent of the request: neither
      `evidence-batch-response.schema.json` nor `evidence-batch-conflict-details.schema.json` carries
      `uniqueItems` (which would express whole-item, not per-field, uniqueness); the Elixir semantic
      clause for a response checks only receipt `run_id` equality
      (`validate_evidence_batch_response_semantics`,
      `control/lib/symmetry_control/goals/contract_validation.ex`) and `validate_semantics/2` falls
      through to `:ok` for conflict details, whose decoded items no production consumer reads
      (decoded only by `contracts.DecodeSchema` in `daemon/internal/control/client.go`);
      `contracts/ts/semantics.ts` has no response or conflict-details validator. The gap is latent,
      so the fix is a strictness restore, not an incident: add `uniqueItems` where it can express the
      rule, or a semantic clause on the Elixir side. Owner: contracts strictness (A6).

## Observed flakes, cause unknown: three observations (2026-11-22 CI runs and PR #220 review)

- [ ] `TestAutonomousReturnReleasesItsInputReader` (`daemon/cmd/symmetry-fake-agent/autonomous_test.go`;
      package `daemon/cmd/symmetry-fake-agent`): failed once in the hosted `daemon` job's race step
      (`go test -race -count=1 -timeout 600s -json ./...` through `scripts/ci/stdio-only-caller-boundary.py`),
      run `44976804508`, on PR #225 head `d45dc45f` — a Control-only code change
      (`control/lib/symmetry_control_web/protocol.ex` plus its reason-coverage test and two docs records;
      `git diff --name-only origin/main...d45dc45f`), so the daemon tree is untouched by that PR. Observed
      failure text: the test's own `t.Fatalf("reader goroutine is still alive when runAutonomous returned")`
      with the goroutine dump it printed — `goroutine 22 [runnable]` in `runAutonomous.func1` (the reader
      closure in `autonomous.go`, reported at the function's tail, with no `readLine` or select frame),
      `created by ... runAutonomous in goroutine 50`. Reproduction
      attempts and outcomes: the same test passed on the PR's previous and next heads (CI reruns), and no
      other occurrence is recorded in the repository; added 2026-11-22 — Linux (WSL, go1.27.0) `go test
      -race -count=20 -timeout 600s -v -run '^TestAutonomousReturnReleasesItsInputReader$'
      ./cmd/symmetry-fake-agent/` → 20/20 `--- PASS`, exit 0. Cause unknown. Most probable mechanism
      (inference, re-derived from this run's own `-json` record): the input is a closable `io.Pipe` whose
      `Close` does unblock the parked `readLine`, so the reader's exit is reachable; the record's
      `"Elapsed":0.01` (`--- FAIL: ... (0.01s)`) is below `runAutonomous`'s `readerJoinTimeout = 50 ms` at
      that head, so the join's `time.After(readerJoinTimeout)` arm cannot have fired — the run returned
      through `<-readerDone`, so the reader had already run its deferred `close(readerDone)` and was in its
      exit epilogue when the scan ran, and `bytes.Contains(stacks, "runAutonomous.func1")` reads that
      exiting goroutine as a live leak. Next diagnostic step: repeat the `-race -count=N` run under CPU
      contention, and on a reproduction capture whether the reader's `close(readerDone)` had already run
      when the scan executed (the goleak oracle in the same file is the existing alternative oracle) rather
      than touching the assertion or the bound, which the sibling
      `TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` pins (`--- PASS: ... (0.05s)` in the same
      run). Proximity, not duplication: the comment-policy-sweep entry above for
      `daemon/cmd/symmetry-fake-agent/autonomous.go` records the *unblockable* carrier, whose reader is
      never joined — a different property, not this test's failure.

- [ ] `TestLinuxOwnerAnchorWire*` — the five tests
      `TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts`, `TestLinuxOwnerAnchorWireCloneCleanupDescriptorGate`,
      `TestLinuxOwnerAnchorWireStoppingAnswerPrecedesShape`,
      `TestLinuxOwnerAnchorWireExclusiveFieldsRefusedBeforeStoppingAnswer` and
      `TestLinuxOwnerAnchorWireCodecRejectsMalformedFrames` (`daemon/internal/platform/anchor_wire_shape_test.go`,
      `//go:build linux && amd64`; package `daemon/internal/platform`): failed once in the hosted
      `coverage-daemon` job (advisory, `continue-on-error: true`; `go test -count=1 -timeout 1200s
      -coverprofile=coverage.out ./...` through `scripts/ci/stdio-only-caller-boundary.py`), run
      `44973284689` attempt 1, on PR #223 head `ca173870` — whose own change is records-only
      (`docs/evidence/comment-policy-sweep-2026-11-22.md` and its plan) over a PR whose code hunks are
      comment-only, so no `daemon/` behavior changed anywhere in that range. The group drives the real
      `serveConnection` over a real unixpacket socket, and all five tests failed in the same step of that
      attempt, each at the wire harness's client read (`linuxOwnerAnchorWirePair` →
      `linuxOwnerAnchorWireReadFrame` → `ReadMsgUnix`, the stdlib `(*net.UnixConn).ReadMsgUnix` behind the
      helper's `t.Fatalf("read wire frame: %v", err)`), with the full recorded lines (the `/tmp/Test…`
      segment elides the run's temp directory, which is named after the test):
      `--- FAIL: TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts (0.00s)`
      `anchor_wire_shape_test.go:295: read wire frame: read unixpacket @->/tmp/Test…/owner.sock: recvmsg: connection reset by peer`
      `--- FAIL: TestLinuxOwnerAnchorWireCloneCleanupDescriptorGate (0.00s)`
      `anchor_wire_shape_test.go:308: read wire frame: read unixpacket @->/tmp/Test…/owner.sock: recvmsg: connection reset by peer`
      `--- FAIL: TestLinuxOwnerAnchorWireStoppingAnswerPrecedesShape (0.00s)`
      `anchor_wire_shape_test.go:351: read wire frame: read unixpacket @->/tmp/Test…/owner.sock: recvmsg: connection reset by peer`
      `--- FAIL: TestLinuxOwnerAnchorWireExclusiveFieldsRefusedBeforeStoppingAnswer (0.00s)`
      `anchor_wire_shape_test.go:392: read wire frame: read unixpacket @->/tmp/Test…/owner.sock: recvmsg: connection reset by peer`
      `--- FAIL: TestLinuxOwnerAnchorWireCodecRejectsMalformedFrames (0.00s)`
      `anchor_wire_shape_test.go:434: read wire frame: read unixpacket @->/tmp/Test…/owner.sock: recvmsg: connection reset by peer`
      In the same attempt the `daemon` job's race step passed the same five tests about four minutes later
      (`--- PASS` at 01:56:04 against this failure at 01:51:50), and attempt 2 of that run is green at the
      same head. Reproduction attempts and outcomes: main's last four `coverage-daemon`
      runs passed; five local WSL `-cover` runs at head and base passed; added 2026-11-22 — WSL Ubuntu-24.04
      as an unprivileged user, go1.27.0, `go test -count=5 -timeout 1200s -cover -v -run
      '^TestLinuxOwnerAnchorWire' ./internal/platform/` → 25/25 `--- PASS`, exit 0, `coverage: 2.1% of
      statements`; the same command with `GOTOOLCHAIN=go1.27.0` in this host's default (root) WSL
      session refuses all five with
      `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`, so a revalidation must
      run as an unprivileged user (the refusal is a host property, not a test failure). Cause unknown. Next
      diagnostic step: re-run the group well above `-count=5` under CPU contention in the job's own shape
      (`-coverprofile` through `stdio-only-caller-boundary.py`) and, on a reproduction, capture whether the
      peer side had already closed the socket at that read. Fresh group, not the closed
      `daemon/internal/platform` flake class (helper witness / descendant monitor,
      `docs/archive/CLOSED-TODO.md`), and the file's own cgroup-`/` host trap is a different condition.

- [ ] `frontend/src/features/goals/goal-attention.test.tsx`, the plan-approval replay case: failed once
      among 13 shuffled vitest runs of a 13-file subset (`--sequence.shuffle --sequence.seed=3`) during the
      PR #220 review, recorded in that PR's `machine-review-state:v1` review record (comment
      `#issuecomment-7071789916`), whose reruns are at head `9715ce32` — the failing round's head is not
      recorded there; reruns with the same seed passed, so the observation is load-dependent, not
      order-dependent. The PR #220 review record names the case: "Round 1 saw one red run of `replays the
      plan approval's decision resolution byte for byte after a lost acknowledgement`", and records "1
      failure in 13 shuffled runs that include this case, and 0 in the last 11. That points to a rare
      load-sensitive flake in that held-promise case, not an order dependence". The sibling `replays the
      plan approval's admission byte for byte when its acknowledgement is lost` shares the contract's
      truncated prefix and is kept here only as that sibling. The case ends in a bounded wait
      (`findByText("The server admitted the plan.", {}, { timeout: 5_000 })` under a 15 s case timeout),
      so a starved bounded wait is plausible (inference, not observed; the review record carries no
      assertion text). Cause unknown. Next diagnostic step: repeat the shuffled 13-file
      subset with the same seed under CPU load — the rerun that passed is not recorded as loaded, so load is
      the untested variable — and, on a reproduction, capture the assertion text and whether the failure is
      the bounded wait or a recorded-request mismatch. The subset's file list is not recoverable from the
      repository (PR #220's plan records shuffle runs over the full 21-file suite at seed `20261122`).

## Goals state-set duplication after the rf-w4-07 facade split (2026-11-22)

- [ ] Consolidate the duplicated Task/Run state sets behind one compile-time owner. After
      `refactor/rf-w4-07-goals-facade-phase1` (PR #233) the same literals are re-declared per
      module: `@nonterminal_task_states` in `SymmetryControl.Goals` (`goals.ex`),
      `SymmetryControl.Goals.RunFence`, `SymmetryControl.Goals.Internal` and
      `SymmetryControl.Goals.ReadModel`; `@terminal_run_states` in `SymmetryControl.Goals` and
      `SymmetryControl.Goals.HarnessSessions`; `@accounting_terminal_run_states` in those two plus
      `SymmetryControl.Goals.ReadModel`, which derives it from that module's own
      `@terminal_task_states`. The split mandated per-module copies ("module attributes travel
      with their users") because the values feed compile-time query literals, so the SQL text
      stays frozen; the drift risk is real: a state added to one copy only silently splits
      transition policy from the read model. Next step (W5-14, the following goals.ex phase): a
      `@moduledoc false` owner module exposing the sets as functions the others read at compile
      time (`@x Owner.state_set()`), keeping every SQL literal byte-identical, plus one pinning
      test that fails on divergence.
