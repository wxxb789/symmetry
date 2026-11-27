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
      (`daemon/internal/state/run_journal.go:673`) are
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

- Dead-surface candidates (decision note, 2026-11-22, rf-w4-21 state domain journals): the split
      left two exported `*Store` methods with no non-test caller — `QueueEvent`
      (`daemon/internal/state/run_outbox.go:58`) and
      `QueueCancelledTransitionAndAcknowledgementAt`
      (`daemon/internal/state/run_outbox.go:288`). The app-side cancel path that would call the
      second one sits behind `options.queueCancelledTransitionAndAcknowledgement`
      (`daemon/internal/app/outbox.go:214-215`), and that field is assigned only in tests
      (`daemon/internal/app/app_test.go:2474,2653,4374,5723`,
      `daemon/internal/app/retention_failure_test.go:148`); production falls back to
      `QueueTerminalTransitionAndAcknowledgementAt`. Both methods predate this slice and are
      exported, so unexporting them belongs to a W3-08-style surface slice rather than to the
      apply stage of a same-package split; revalidate with `deadcode -test ./cmd/...` on the
      three CI platforms plus a repo-wide `git grep -nw <symbol>` before deleting. Owner area:
      daemon state unexport sweep.

- [ ] Reconcile the `harness/claude` package comment with the package's contents: the header of
      `daemon/internal/harness/claude/stream.go` still states that the package "intentionally does
      not expose a `harness.Adapter` until credentialed native lifecycle evidence proves the
      required session and control semantics", which the Claude candidate relocation made false —
      the package now owns the adapter. That file was outside the relocation's owned set (W4-15a's
      decoder, W4-15b's driver), so the sentence waits for the slice that next opens it. Owner
      area: daemon harness.

- [ ] Re-pin `codex.Probe`'s own nil-context guard: `daemon/internal/harness/codex/probe.go`
      rejects a nil context with `"codex probe context must not be nil"`, and after the rf-w4-15c
      relocation deleted the root registry-seam case that exercised a nil context end to end, no
      HEAD test covers that guard text — the shared entry guard stays pinned by
      `TestRunBoundedProbeRejectsNilContext`
      (`daemon/internal/harness/bounded_probe_test.go`). Owner: unassigned — rf-w5-16 (codex
      adapter concepts) could not cover it: its strict owned set excludes `codex/probe.go` and
      `codex/probe_test.go`, so the re-pin needs the slice that next owns the probe's tests.

- [ ] `cancelAttempt.terminalInterrupted` (`daemon/internal/harness/codex/cancel_state.go:27`) is
      write-only in production: the write paths are `cancel_state.go:234` and `:265`, and the only
      readers are two test assertions (`cancel_state_test.go:183`, `:337`). Base
      `e13450bc:adapter.go` carried the same write-only field, so the rf-w5-16 split did not introduce
      it; removing it means rewriting the frozen tracker assertions, which is a contract change rather
      than a tidy-up (rf-w5-16 simplify finding 3, recorded by the apply stage 2026-11-23). Owner area:
      codex adapter.

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
    0006b branch"); and five configured inputs: the secrets `SYMMETRY_LINUX_V2_TEST_GITHUB_OAUTH_TOKEN`
    (must be `gho_*`; the hosted `GITHUB_TOKEN` is refused) and `SYMMETRY_LINUX_V2_TEST_OPENAI_API_KEY`,
    and the variables `SYMMETRY_LINUX_V2_TEST_PI_MODEL`, `SYMMETRY_LINUX_V2_TEST_OPENCODE_MODEL` and
    `SYMMETRY_LINUX_V2_TEST_OPENAI_BASE_URL` (the OpenAI-compatible provider base URL the native
    fixtures route their model calls through).
    Recorded status, still limiting (2026-11-23): the Environment policy read allowed branches `main` and
    `codex/goal-0006b-crash-safe-process-containment` with `prevent_self_review=false`. The hosted runner now
    passes the probe and the fixture's whole host preflight: an earlier attempt (run `43673581776`) failed the
    probe with `unified_cgroup_v2_mounted=false`, and run `45130117124` (2026-11-23, main `55ecd76a`) got all
    14 case sets past the probe into `scripts/ci/linux-v2-native-acceptance.sh`, where every one died at `:68`
    with `stat: cannot statx '/proc/1/ns/pid': Permission denied` — the fixture read the PID 1 namespace inode
    without the `sudo` it uses for its other privileged host reads. Goal `rf-acc-native-hosted-run` fixes each
    failure the scratch loop then reached: that read, the private Store StateDir ownership (its implicitly
    created parent came from `install -d` as root-owned), the guest A-search fence, the removed A-side `rmdir`,
    the fixture project key length and the debootstrap mirror source, verified by the loop's before/after runs
    `45132022431` through `45169868354`. The debootstrap source is now the image's own apt configuration (the
    first archive URI in `/etc/apt/apt-mirrors.txt`, else the first `URIs:` entry in
    `/etc/apt/sources.list.d/*.sources`), or the case set fails with a named message: the public-archive
    fallback was removed deliberately, because that archive dropped individual packages from the hosted runner
    network (runs `45154695682`, `45159670971`). The block is fail-closed and cannot be steered to an archive
    this record does not name. Two host-tooling facts this record must carry: the `ubuntu-latest`
    image is `resolute`, whose `coreutils` package is the uutils Rust build (`/usr/bin/test` resolves to
    `/usr/lib/cargo/bin/coreutils/test`, version `9.5-1ubuntu2+0.0.0~ubuntu25`; guest images debootstrap the
    same package), and uutils evaluates `test -x/-r/-w` from stat mode bits only, ignoring POSIX ACLs. Probe
    runs `45145656538`, `45146607068` and `45147275664` show the kernel itself enforcing the same ACL entries
    on the runner root filesystem, where the fixture's ACL paths live: raw `chdir`/`open` syscalls and
    `faccessat2(..., X_OK, AT_EACCESS)` agree, deny entries deny, and `mount -o remount,acl` changes nothing.
    The only disagreement on fresh tmpfs, fresh `mkfs.ext4 -q` loop mounts, xfs, btrfs and `/dev/shm` came
    from the uutils `/usr/bin/test` (`45147275664` straces that tool reading a mode-bit `statx`, while the
    bash builtin `test` and `python3 os.access` reach `faccessat2` and honour the ACL), so those `acl-ignored`
    verdicts are that tool's artifact; the fence's three probes now run through the bash builtin `test`, which
    issues that kernel call. With the scratch-only `gh` credential-ingestion shim on `PATH` (a scratch-branch
    workflow step, deleted with the branch; runs `45164286155` and `45169868354`), the loop reaches Control
    start, the production owner install and the installed daemon's own preflight, where it stops on a host
    property: systemd 259 (`259.9-0ubuntu0.1`) chowns the whole delegated `DelegateSubgroup=manager` subtree,
    `manager/cgroup.kill` included, to the service user (probe run `45169655828`: `55200:55200 200`, and that
    user can open it for writing; in-guest diagnostic in run `45169868354`: `52000:52000 200` while the unit's
    own `cgroup.kill` stays `0:0 200`), while `-verify-linux-owner-install` (run `45164286155`) requires that
    file to stay root-owned and unwritable by A. Making a credentialed run pass that step needs a
    production-side decision (installer re-chown once the subgroup exists, a reviewed preflight change, or a
    host whose systemd keeps the control root-owned); the fixture keeps the production sequence unchanged. The
    scratch runs carried only the contract's dummy credentials, so the delivered bytes' final all-14 run
    `45183708322` (scratch head `c514d3f6`, whose tree adds only the throwaway workflow to PR head `e329d8dd`;
    script sha256 `8cc4bd831e89085ddfcf3b669ebc6756808d955659a4791cc4bde6e07edae089`, fixture sha256
    `bf8f659b036fb32573b1f768db1dc8f1d913a1ada535835773233257864f10b5`) stops at that credential-dependent
    `gh auth login` (the fixture's `:292` refuses the dummy `gho_` token) and every provider turn stays unproven
    until the owner dispatches the workflow again. Run `45171498923` remains the historical all-14 run on the
    pre-mirror-fix bytes (script sha256 `592b98f8a0be53aaeab9022c5cc692320dc1be49a6e10bdab00a05c5664fab68`).
    Retired (2026-11-23): the `manager/cgroup.kill` ownership refusal on systemd ≥254 this record was to
    carry was retargeted by goal `rf-acc-delegated-cgroup-kill` to the boundary systemd keeps (the owning
    unit cgroup's own `cgroup.kill`); recorded in `docs/archive/CLOSED-TODO.md` and
    `docs/evidence/rf-acc-delegated-cgroup-kill-2026-11-23.md`.

    Missing CI oracle (2026-11-23, goal `rf-acc-delegated-cgroup-kill`): the `daemon` CI job runs as
    its runner's non-root user, so the row that pins the unit-kill write-probe wiring
    (`TestLinuxOwnerUnitKillControlReadsTheDerivedUnitPath`) only discriminates as root - non-root it
    refuses at the root:root metadata rule before the probe. A root-run (or WSL-root) pin of the probe
    wiring is the missing CI oracle; the metadata rule still catches an A-owned unit kill without root.

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

- [ ] Control review: goal-less assignment ignores the repository resource by design (C1 verdict);
      the one open slice is excluding Linux pi/opencode from legacy selection (2026-11-24, goal
      `rf-acc-native-burndown`; C1 verdict in `decision-native-opencode-2026-11-24.md`, GDD state
      root; evidence `docs/evidence/rf-acc-native-burndown-2026-11-24.md` §5.3 and §7, runs
      `45309039777`, `45315493474`, `45402706492`): repository affinity is Goal-scoped by the
      binding design (`docs/design/data.md:106-123`, `docs/design/protocol.md:194-207`; goal-less
      behaviour is frozen, `docs/design/data.md:12-14,397-398`). A goal-less Task has no WorkItem
      link, so the v1 selector has no resource to bind: it joins on
      `agent_profile`/`workspace`/`capabilities` and takes the oldest matching runtime
      (`control/lib/symmetry_control/orchestration/assignment.ex:260-306`,
      `runtime_matches_binding/0`), while the goal path requires the runtime's
      `repository_resource_id` to equal the goal's resource (`Assignment.goal_runtime_matches?/3`,
      `assignment.ex:505-512`, through `Eligibility.runtime_matches_goal_resource?/3`,
      `control/lib/symmetry_control/goals/eligibility.ex:209-224`). The two discriminating tests:
      `control/test/symmetry_control/orchestration/claims_test.exs` "claim_run refuses a goal claim
      whose runtime lost repository affinity" (`:89`) and
      `control/test/symmetry_control/orchestration/assignment_test.exs` "assign_one assigns a queued
      legacy task and moves it to assigned" (`:13`). Consequence (P3, routing only, no authority
      leak): a goal-less codex/claude/generic run may execute in another repository's machine-local
      workspace, and Linux pi/opencode refuse it with `linux_v2_required` only after the claim
      grant. No Control goal opens for this; the optional later Control slice retires Linux
      pi/opencode from legacy selection the way omp/omo already are. The fixture-side isolation
      (retiring earlier case sets' runtimes, `scripts/ci/provision-linux-v2-control-claims.exs:98-101,
      425-436`) lands with the exact-profile/resource guard (`:206`) unchanged, and the workflow's
      `SYMMETRY_HEARTBEAT_INTERVAL_MS=3600000` stays unchanged by decision. The closed half of the
      original item (the installed-daemon smoke's owner `Before` check) is in
      `docs/archive/CLOSED-TODO.md`.

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
round 1) left two paths open here — the in-process response-loss consumer path was closed
2026-11-23 (see `docs/archive/CLOSED-TODO.md`).

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
      (a) `v2 cgroup reservation cannot accompany a v1 authority` (`containment_journal.go:614`) and
      `v2 cgroup reservation cannot accompany a v1 handoff` (`containment_journal.go:617`): the authority and
      handoff blocks (`validateContainmentAuthority`, `validateContainmentHandoff`) already return
      `v1 … cannot use a v2 cgroup reservation` whenever a non-v2 owner is combined with a cgroup
      reservation, so the later checks can never fire.
      (b) `Linux cgroup reservation cannot have both abort and physical-stop proof` (`linux_cgroup_journal.go:165`)
      and `Linux cgroup aborted reservation lost producer-exit evidence` (`linux_cgroup_journal.go:169`):
      `linuxCgroupReservationAbortMatches` (`linux_cgroup_journal.go:1082`) returns false for every proof that
      carries a pre-gate receipt or lacks producer-exit parity, so the earlier abort-proof error
      fires first. Probes: aborted reservation + pre-gate receipt → `Linux cgroup reservation abort
      proof is invalid`; aborted reservation + helper exit proof → `Linux cgroup helper exit proof
      is invalid`.
      (c) `Linux cgroup reservation has effects before creation intent` (`linux_cgroup_journal.go:178`): with
      `CreateStarted == false` every listed effect is shadowed by an earlier block
      (leaf→leaf identity, helper→helper identity, launcher→launcher identity,
      launcher exit→launcher exit proof, gate→gate identity, helper exit→helper exit proof,
      abort with a disposition other than `neverCreated`→abort proof). Probe: unstarted reservation
      + leaf identity → `Linux cgroup reservation leaf identity is invalid`.
      (d) `unresolved input command has an acknowledgement` (`journal_validation.go:128`): it needs `pending`
      with `intent.Outcome == ""`, but `pending` compares against `acknowledgement.Outcome` and
      `validAcknowledgement` (`run_outbox.go:840`) requires a non-blank `Outcome`, so an empty-outcome
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
      (a) `journal.ContainmentCgroup != nil` in `validateContainmentScans` (`containment_journal.go:626`): a
      journal carrying both a cgroup reservation and containment scans is rejected earlier by
      `validateContainmentCgroup` (`containment_journal.go:597` `journal.ContainmentScans != nil`).
      (b) `handoff.SupervisorPID <= 0` in `validateContainmentScans` (`containment_journal.go:628`): the earlier
      scan block rejects `scans.SupervisorPID <= 0`, and `scans.matchesHandoff` equates the two
      supervisor PIDs, so a non-positive handoff supervisor PID can never be the deciding operand.
      (c) `journal.ContainmentAuthority != nil` in `validateContainmentHandoff` (`containment_journal.go:574`):
      a valid authority always carries `TargetPID > 0` (`containment_journal.go:553`) equal to the journal PID,
      so `journal.HasProcessDetails()` is already true in the same condition whenever the authority
      operand could matter.
      (d) `journal.ContainmentAuthority != nil` in the aborted / pre-gate cgroup preconditions
      (`containment_journal.go:600`, `containment_journal.go:604`): `validateContainmentAuthority` rejects a v1 authority
      beside a cgroup reservation, and a v2 authority whose cgroup carries an abort or pre-gate
      proof fails `linuxCgroupReservationMatchesAuthority` (it requires both to be nil), so the
      authority block always errors first.
      (e) `journal.ContainmentHandoff != nil` in the same two preconditions (`containment_journal.go:601`,
      `containment_journal.go:605`): a v2 handoff passes only when its cgroup matches it with a gate, a bound
      launcher grant and no pre-gate receipt, while an aborted cgroup must have no gate and no
      launcher grant (`linuxCgroupReservationAbortMatches`) and a pre-gate receipt requires no abort
      proof; a v1 handoff beside any cgroup always errors in the handoff block.
      Fix direction: re-run each argument as a probe before deleting; keep the surrounding
      condition's live operands.

- [ ] Fix the nil-pointer panic in `validateLinuxCloneCheckout`
      (`daemon/internal/state/linux_clone_validation.go:250`). It dereferences `*seed`
      (`linuxCloneCheckoutMatches(*intent, *seed, *checkout)`, `linux_clone_validation.go:55`) although
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
(`journal_validation.go:73`) at cyclomatic complexity 25 and `validateJournalKeyScalarsAndProcessDetails`
(`journal_validation.go:53`) and `validateContainmentScans` (`containment_journal.go:623`) at 24, against the wave's ≤25
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
      NUL check, the receipt generation upper bound and minimum length, and
      `encodeGuardedRelease`'s body and grant bounds. `readWireString`'s `maxCommandString` bound
      left this list on 2026-11-26: `rf-claude-schema-argv` added
      `TestWireLinuxCommandStringBoundary`, whose decode row refuses a `maxCommandString`+1 element
      (`length-prefixed command string is oversized or truncated`, evidence record §7); the NUL
      check stays open. The generation and minimum-length checks may be shadowed by later reads or
      `receipt.Validate`; the rest are real gaps. Add one rejection test per check in a separate
      maintenance slice.

## Deferred review findings (PR #205)

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

      **rf-w3-17 update (2026-11-22, branch `refactor/rf-w3-17-migration-tests-async`, plan
      `docs/plans/2026-11-22-rf-w3-17-migration-tests-async-plan.md`, conductor rulings F-7/F-8):**
      the D9 pin is removed — all nine files declare `async: true`, the test-time migration compile
      is replaced by a boot registry (`control/test/support/migration_modules.ex`), which also binds
      every test-time `{version, module}` source to the compiled files (`verified_pairs!/1`) and
      owns the one shared base-migration prefix, schema DDL runs on private single-connection Repos,
      and the two `public.oban_jobs` rollback tests moved to a new `async: false` module
      (`control/test/symmetry_control/migrations/add_oban_jobs_table_test.exs`) so their
      `LOCK TABLE … IN SHARE MODE` cannot queue behind an async Oban producer. Measured on this
      16-scheduler host, same command and partition (`w3-17-runs-pre/` five seeds,
      `w3-17-r2-runs/` three final-tree seeds): run 656.6 / 605.2 / 803.3 s → 342.7 / 336.0 / 294.0 s;
      sync phase 625.4 / 568.3 / 766.4 s → 123.1 / 137.8 / 120.1 s; async phase 31.2 / 36.9 / 36.9 s
      → 219.5 / 198.1 / 173.9 s, now bounded by `Goal0006MigrationTest` alone (its own work
      202.0 / 178.1 / 154.9 s). Counts and name sets equal the reviewed head `f6b5b176`
      (`only_left=0 only_right=0 state_diff=0` over `1181 = 1181`), as the attempt-2 streams did
      against the dispatch base before the branch merged origin/main (`1032 = 1007 + 21 + 4`,
      seeds 0/1/2, names identical with the F-8 move's two names remapped and the only state
      movement being two pre-side flakes passing on the post side); the merged tree runs
      `1181 = 1156 passed + 21 skipped + 4 excluded`. The ten-module set is green at seeds 0-4
      plus a 5× repeat,
      with the seven small files at 25× repeat each and `goal_external_wait` at 25×. Five
      same-workload A/B runs in both orders show the local wall difference is not stable (the
      path-identical nine-path pair put the post side 16 s wall below the pre side, the
      nine-pre→ten-post pair sat 40 s above it, and the reversed pair put the pre side 16 s above
      the preceding post run) while the level drifted inside fifteen minutes and nine-way
      concurrency inflates each module's own work (summed +16 % to +53 %), so the local number is
      report-only (W3-14 lesson F4). The review-stage fix revision re-ran the gates on the merged
      tree (ten-file set seeds 0-4, a 5× set repeat, the per-file sweeps at the accepted counts, one
      full seed; table in the plan's "Fix-stage verification"). **D9 ruling (2026-11-22, PR #262 CI
      run 45115861829 vs main run 45109921118):** the `coverage-control` test step moved 166 s →
      151 s (−9 %), ExUnit 133.9 s → 118.4 s (sync 116.0 → 47.3 s, async 17.9 → 71.0 s), and
      `redefining module` warnings 516 → 0. The ≤65 s target is NOT reached: the async phase
      (71.0 s) is now the floor. The slice lands as a measured improvement; this entry stays open
      with the next lever named by the conductor: profile the longest async module and split it,
      not more conversions. No further work in this slice.

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
      `saveJournalWithCapacityGuardLocked` (`daemon/internal/state/state.go:251`) →
      `validateJournalSaveCapacity` (`daemon/internal/state/provider_action.go:594-606`, 4 MiB check
      at `:599`) and `saveJournalLocked` (`state.go:237`, size check at `:241`;
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
      `supervisor_windows_helper.go:545-546` (`SYMMETRY_PREAUTHORITY_DROP_FIRST_HELLO_RESPONSE`,
      `SYMMETRY_PREAUTHORITY_HELLO_RETRY_MARKER`) and `supervisor_windows_client.go:1038`
      (`SYMMETRY_PREAUTHORITY_WRONG_SECRET_MARKER`) — and
      `cmd/symmetry-daemon/preauthority_witness_command_windows.go` enters witness mode from argv
      (`init()` `:26-36`) while setting the same three names for the helper child (`:142-148`).
      Conductor decision A (2026-11-21) scoped rf-w5-01 to the Linux family and left these bytes
      unassigned; W6-03 (the Go-test-binary fork removal) does not deliver this consolidation. The
      Linux half now has one per-use witness-config parser
      (`daemon/internal/platform/supervisor_linux_witness.go`) behind
      `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` plus marker-parent validation, so the two
      conventions coexist. Owner: unassigned.

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
      Windows development host (reproduced while running this sweep's gate). The Credo half of the
      item — that a source file skipped over `parse_timeout` or unparsable could deflate the strict
      baseline with only a guard printing — closed at rf-w6-10 and moved to
      `docs/archive/CLOSED-TODO.md`.

- [ ] `daemon/cmd/symmetry-fake-agent/autonomous.go`: on a carrier whose `Close` cannot unblock a
      parked read (the inherited stdio pipe), the reader goroutine is not joined before the run
      returns; the bounded join caps the wait and the goroutine exits with the process.

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

## Observed flakes, cause unknown: two observations (2026-11-22 CI runs and PR #220 review)

- [ ] `TestLinuxOwnerAnchorWire*` — the five tests
      `TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts`, `TestLinuxOwnerAnchorWireCloneCleanupDescriptorGate`,
      `TestLinuxOwnerAnchorWireStoppingAnswerPrecedesShape`,
      `TestLinuxOwnerAnchorWireExclusiveFieldsRefusedBeforeStoppingAnswer` and
      `TestLinuxOwnerAnchorWireCodecRejectsMalformedFrames` (`daemon/internal/platform/anchor_wire_shape_test.go`,
      `//go:build linux && amd64`; package `daemon/internal/platform`): failed once in the hosted
      `coverage-daemon` job (advisory at the time, `continue-on-error: true`; `go test -count=1 -timeout 1200s
      -coverprofile=coverage.out ./...` through `scripts/ci/stdio-only-caller-boundary.py`), run
      `44973284689` attempt 1, on PR #223 head `ca173870` — whose own change is records-only
      (`docs/evidence/comment-policy-sweep-2026-11-22.md` and its plan) over a PR whose code hunks are
      comment-only, so no `daemon/` behavior changed anywhere in that range. The group drives the real
      `serveConnection` over a real unixpacket socket, and all five tests failed in the same step of that
      attempt, each at the wire harness's client read (`linuxOwnerAnchorWirePair` →
      `linuxOwnerAnchorWireReadFrame` → `ReadMsgUnix`, the stdlib `(*net.UnixConn).ReadMsgUnix` behind the
      helper's `t.Fatalf("read wire frame: %v", err)`), with the full recorded lines (the `path-sha256:3ff0306ff829e21a5ae9bef2bfa3d222df279dffcc8af0fcb217845c5aabdb8a`
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
      module. `refactor/rf-w5-14-goals-concepts-phase2` (2026-11-22) moved the copies into the
      concept modules that use them and left the duplication in place, so the current homes are:
      `@nonterminal_task_states` in `Goals.Admission`, `Goals.Budget`, `Goals.ContextAssembly`,
      `Goals.Decisions`, `Goals.Internal`, `Goals.ReadModel`, `Goals.Reconciliation`,
      `Goals.Recovery` and `Goals.RunFence`; `@terminal_run_states` in `Goals.Budget`,
      `Goals.HarnessSessions`, `Goals.Recovery`, `Goals.Settlement` and `Goals.ValidationOutcome`;
      `@accounting_terminal_run_states` in `Goals.Budget`, `Goals.HarnessSessions` and
      `Goals.ReadModel` (the last deriving it from its own `@terminal_task_states`). The split
      mandated per-module copies ("module attributes travel with their users") because the values
      feed compile-time query literals, so the SQL text stays frozen; the drift risk is real: a
      state added to one copy only silently splits transition policy from the read model.
      Consolidation remains undone and is the next owner's item: a `@moduledoc false` owner module
      exposing the sets as functions the others read at compile time (`@x Owner.state_set()`),
      keeping every SQL literal byte-identical, plus one pinning test that fails on divergence.

## rf-w5-14 goals-concepts phase 2 — deferred simplify findings (2026-11-22)

- [ ] Apply the moved-lib simplifications the apply stage deferred (simplify report findings 7, 8,
      13-28). Deferred because each rewrites a moved function body and would invalidate the recorded
      per-function AST identity + string/atom control-flow move proof (contract clause 2) without a
      re-derivation; take them as a dedicated cleanup slice that re-runs the AST comparison. Locations:
      `goals/admission.ex:909-910` (`item_by_id` values never read; only `Map.keys` is used),
      `goals/settlement.ex:691` (identity `external_observation_terminal!/3`, called at `:146`),
      `goals/reconciliation.ex:308-339,345-367,369-389,511-548,644-668,693-702,1042-1190` (candidate
      queries, blocker `Repo.exists?`, locked current-run lookup, lock+revalidate preambles),
      `goals/admission.ex:279-304,347-459,474-476,509-613,812-825,1186-1189` (admission-limit gate,
      snapshot->admission->task->reservation pipeline, model-profile gate, validation-producer chain),
      `goals/handoff.ex:244-276,283-313`; `goals/budget.ex:73-89,299-308`;
      `goals/goal_control.ex:36-38,104-106,347-348`;
      `goals/commands.ex:162-167,226-233,278-283,293,374-379,404-529,900,1010-1030,1032-1037`;
      `goals/plan_contracts.ex:38-39,55-77,117-126,132-140,286-299,338`;
      `goals/settlement.ex:514-546,706-726,963,1135-1188`. Cross-module duplicates that would need a
      new `@doc false` edge (the contract forbids new edges): `goals/settlement.ex:82-111` vs
      `goals/validation_outcome.ex:50-77`; `goals/goal_control.ex:389-390` vs `goals/recovery.ex:440`
      (persisted idempotency-key wire format, parsed at `goal_control.ex:358-367`).
      Also not applied from the same report (F29 subset): the inline resource-keyed session fixture in
      `test/symmetry_control/goals/commands_flow_test.exs` (no WorkItem exists at that point), the six
      reconciliation policy maps (non-default `automatic_execution`/budget/parallel values), the run_fence
      evidence builders (distinct payloads per kind) and the full request→resolve scope pairs (two
      data-heavy call sites; their resolve halves now share `GoalsTestSupport.resolve_decision!/3`).
- [ ] Decide the `settle_unstarted_task` replay response shape: `goals/settlement.ex:1069-1071` returns
      `receipt.response` without `"event_sequence"` while the create path (`:1120`) and sibling replays
      (`:125`, `goals/validation_outcome.ex:485-487`) add it; the settlement interface test normalizes the
      difference away with `Map.drop`. Pre-existing asymmetry: either align the shape (observable change) or
      pin it explicitly.

## Daemon app tests: the retained-resume attach-ready mark is unpinned (PR #234 review round 2)

- [ ] Pin the `tx.attachReady = true` assignment in `publishRetainedNativeStartBoundary`
      (`daemon/internal/app/goal_admission_tx.go`): deleting it survives the tx seam matrix and the
      full app suite, so no test reaches the retained-resume path that reads the mark. It only
      matters when a retained resume is cancelled after launch or its lease expires after `Open`.
      The gap exists at base; owner: the next daemon app-test slice.

## Process recorder: frozen invocation and request members (rf-w4-12c, 2026-11-22)

- [ ] Remove the legacy `StartRequest.Invocation`, `StartRequest.PersistProcess`,
      `StartRequest.PersistProcessAuthority` and `StartRequest.PersistContainmentStopReceipt`
      members with the ten frozen `execution.Invocation` mirrors, `Invocation.MirrorRecorderMembers`
      (`daemon/internal/execution/recorder.go`),
      `harness.PopulateOpencodeRecorderMembers` and `StartRequest.grant()`'s no-grant fallback
      (`daemon/internal/harness/staged_driver.go`). Every durable write of a launch now goes through
      the run's `execution.Recorder`; the members are only mirrors, populated from that recorder's
      nil-gated write functions in the three app construction sites
      (`daemon/internal/app/app.go`, `goal_admission_tx.go`,
      `linux_owner_witness_linux.go`) and read only by
      `daemon/internal/harness/opencode/adapter.go`, which the slice (E3/D1) may not touch, by the
      runner's recorder-less fallback for an invocation OpenCode rebuilt from those members, and by
      app test doubles. The five conforming adapters (`pi`, `codex`, `claude`, `omo`, `omp`) still
      read `request.Invocation`'s process-description members (`Args`, `Env`, `InitialInput`,
      `CloseInputAfterInitial`), which must move onto a description input before `Invocation` can be
      deleted. Once OpenCode adopts the grant or is dropped (W6-04), delete the ten mirrors, the
      three request members, `MirrorRecorderMembers`, `PopulateOpencodeRecorderMembers`, the
      `grant()` fallback and their population in the three sites above.
      The port's eleventh write, `RecordSupervisorRecoveryScanSeal` (rf-w4-22), is deliberately not
      mirrored: it has no legacy member to keep byte-identical, so a launch whose invocation was
      rebuilt from the mirrors (OpenCode) keeps today's fail-closed response-loss stop until W6-04
      lets it carry the recorder.

## Linux v2 owner hosts under the Ubuntu AppArmor userns restriction (2026-11-22)

- [ ] The owner-gated D3 `harness.md` amendment and the D3/W5-02 item still need the delivered host
      requirement applied; `docs/design/harness.md` and `docs/goals/0010-refactor-program.md` are
      owner-gated, so goal `rf-acc-apparmor-userns-profile` only proposes the amendment text in its
      plan §9. **Delivered (2026-11-26, goal `rf-acc-apparmor-userns-profile`, branch
      `refactor/rf-acc-apparmor-userns-profile`, base `61399984`; plan
      `docs/plans/2026-11-25-rf-acc-apparmor-userns-profile-plan.md`, record
      `docs/evidence/rf-acc-apparmor-userns-profile-2026-11-25.md`):** on a host with
      `kernel.apparmor_restrict_unprivileged_userns=1`, `scripts/install-linux-owner.sh` installs,
      loads and verifies the per-program AppArmor `userns` exception
      `/etc/apparmor.d/symmetry-linux-owner-userns` for the exact installed gate path
      `/usr/libexec/symmetry/symmetry-linux-gate`, fails closed when it cannot prove the loaded
      fact, and removes it through `-remove-userns-profile`; the installed
      `-verify-linux-owner-install` check refuses such a host unless the exact installed text is
      the loaded policy proven against that exact text, and records `apparmor_userns=not_needed|per_program` beside
      `LinuxOwnerPreflightRuntimeBoundary`; `scripts/ci/linux-v2-native-acceptance.sh` boots its
      guest with the default capability set plus `CAP_MAC_ADMIN`, provides the security filesystem before the in-guest
      installer run and authors no policy text of its own, while
      `scripts/ci/linux-v2-native-rootfs-base.sh` pins the `apparmor` package the in-guest load
      and readback need. The sysctl is never written, the two existing CI exceptions and the gate
      are unchanged, and the delivered run rows stand in the goal's evidence record. Run 3's head
      (`ce3c30b9`) clears the wall's rows (the installer and the preflight print
      `apparmor_userns=per_program`, the migration transport start resolves its one-use
      RELEASE, and `shared`'s `clone_integration_linux_test.go:240` /
      `launcher_integration_linux_test.go:439` checks pass); the runs stop beyond the wall at
      rows named verbatim in the record §8 (the migration turn wait and `shared`'s
      `/proc/<pid>/environ` identity rows) — next-goal territory, out of this goal's scope. The
      apply, fix and round-1 stages changed bytes after that head, so this entry binds the wall
      rows to `ce3c30b9` alone; the conductor's Bind run `45688173130` bound `2d423748`, the
      round-1 installer edits (R3/R4) postdate it, and the next Bind re-binds the delivered
      revisions (record §1 digests, §8 delta tables).
      The migration turn-wait row is owned by goal `rf-acc-migration-pi-hold` (2026-11-26; plan
      `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`, record
      `docs/evidence/rf-acc-migration-pi-hold-2026-11-26.md`; closed part in
      `docs/archive/CLOSED-TODO.md`): the pi runner path now stages its `native_release` hold, which
      is the interlock that kept the migration case out of the turn it cannot serve.

## Contracts: A6 tightening — owner sign-off required (rf-a6-contracts-strictness-corpus, 2026-11-22)

- [ ] Unify the Control response decode levels on `responseDecodeSchema`
      (`daemon/internal/control/client.go`). The single entry `decodeResponseJSON` takes the level
      explicitly (owner ruling L2=b, `goal-rf-a6-contracts-strictness-corpus.md`): the legacy and
      typed levels reject duplicate members plus their typed checks, while the schema level also
      applies the contracts raw-byte checks (UTF-8, escaped surrogates, number lexemes for the
      envelope class) and the v1 envelope schema. Moving the legacy and typed callers to the schema
      level needs recorded owner sign-off because it changes acceptance, fail-closed, for these raw
      corpus rows (`contracts/fixtures/raw/`, `contracts/fixtures/manifest.json`):
      `goal-command.raw-number-fractional`, `goal-command.raw-number-exponent`,
      `goal-command.raw-number-unsafe-integer` and `goal-command.raw-invalid-utf8` are accepted by
      the legacy and typed levels today and rejected by the schema level. The tightening artifact
      must re-derive that set from a base-vs-head corpus run rather than from this list. Runtime
      divergences the corpus records and this slice deliberately does not fix:
      `goal-command.raw-duplicate-member-top-level` (Elixir's Jason keeps the first member),
      `goal-command.raw-number-fractional` and `goal-command.raw-number-exponent` (Elixir accepts the
      lexeme and its validator accepts the value), `goal-command.raw-bom-prefix` (the TS reader's
      UTF-8 `TextDecoder` strips the leading BOM before `JSON.parse`) and
      `goal-command.raw-number-negative-zero` (accepted by Go,
      JS and Elixir; no runtime enforces the canonical `-0` to `0` form).

## Linux v2 codex/claude containment case sets await their first dispatch (rf-w5-02a, 2026-11-22)

- [ ] `codex_containment` and `claude_containment` exist in
      `.github/workflows/linux-native-v2-acceptance.yml` and
      `scripts/ci/linux-v2-native-acceptance.sh` (fresh witness plus one same-RunKey `native_release`
      crash-replay leg per kind), but no dispatched run has exercised them yet: the only full proof
      is the dispatch-only workflow on a qualifying host, so this slice's off-host gates stay
      prerequisites and the D3 `harness.md` amendment cannot cite per-kind green runs for codex or
      claude until a run is green with each kind's `manifest.json` binding its executable digest,
      archive digest, version line and source tree digest plus the recorded
      `containment.native_open_reached`. The
      `shared` set's frozen opencode leg is excluded from the per-kind green gate while W6-04/D1
      keeps its bytes frozen (Owner ruling F2, 2026-11-22, binding): a red frozen leg in a dispatch
      is recorded as the F2-excluded leg, not edited. The fixture's AppArmor-userns question above
      is unverified for the guest launcher
      (its userns creation is a privileged root one, unlike the probe's ambient-cap worker): the
      first dispatch records the real behavior before any fixture work is scheduled there.
      Status update (2026-11-24, goal `rf-acc-native-burndown`): dispatched runs have now exercised
      both kinds and neither reached a witness leg: `45200662557` stopped the containment sets at the
      smoke config's OpenCode credential refusal, and the scratch narrow runs of that goal stop them
      at the installed-daemon smoke (the entry above); no per-kind green run and no
      `containment.native_open_reached` is recorded yet
      (`docs/evidence/rf-acc-native-burndown-2026-11-24.md`).

## Orchestration concept split (rf-w5-18, 2026-11-22)

- [ ] Remove the duplicated `ContextSnapshot` read in the goal runtime match. The split moved the
      read into `control/lib/symmetry_control/goals/eligibility.ex:128-135` (validation runtime ids
      from the admitted snapshot); the same row is loaded again by task id in
      `control/lib/symmetry_control/goals/settlement.ex:620` (`Repo.get/2`) and re-read with a lock
      in `control/lib/symmetry_control/goals/validation_outcome.ex:312-318`; Goal admission locks
      it through `goals/internal.ex:144-151`. The contract declared the removal out of scope, so
      the duplicate survives the split unchanged.

- [ ] Fold the three compile-time constant copies the split left, or assert them against each
      other. Attributes cannot be imported, so each module that reads one declares it:
      `@terminal_states` (`control/lib/symmetry_control/orchestration/registry.ex:6`,
      `.../transitions.ex:4`), `@supervisory_commands` (`.../registry.ex:5`,
      `.../commands.ex:4`, `.../transitions.ex:7`), `@capacity_bearing_states`
      (`.../assignment.ex:4`, `.../task_reads.ex:4`). A drift between two copies changes
      eligibility silently; a shared module or a compile-time assertion removes the risk.
      (`@native_harness_kinds` was folded in the apply stage: `Assignment.next_goal_runtime/3`
      consumes `Registry.native_harness_kinds/0`.)

- [ ] Fold the `retry_binding` type copy: `@type retry_binding` (with its `@typedoc`) is declared in
      `control/lib/symmetry_control/orchestration.ex:22-27` (typedoc at `:17`) for the facade's
      public specs and again in `control/lib/symmetry_control/orchestration/task_reads.ex:333-338`
      (typedoc at `:328`) for the moved specs. A drifted copy would mis-document the retry
      projection without failing a test.

## Credo strict ratchet: count-0 blocking (rf-w6-10, 2026-11-23)

- [ ] `.github/workflows/ci.yml` / `control/.credo.exs`: the Credo gate blocks against the committed
      ratchet (`scripts/ci/credo-strict-baseline.json`: 564 issues over 225 files at `95d1df45`),
      not at count 0: the 2026-11-21 reconcile replaced the zero target because zero is not reachable
      inside W6. The residual burn-down is the remaining 326 refactoring / 160 readability / 73
      design / 5 warning findings, after the alias-class share Styler delivered (rf-w6-11) and the
      web-tree clean-up (#147). Every decrease is recorded in the same change as the fix; a check is
      never disabled and no `credo:disable` is added.

## Claude Goal-session attach carries the internal harness-kind spelling (rf-a3 observation, 2026-11-23)

- [ ] Fix the claude harness-kind spelling leak across the Control boundary (observed 2026-11-23
      while auditing the rf-a3 kind vocabulary; the same-day conductor ruling Q2=a recorded it
      instead of folding a wire-value change into that refactor). The daemon's internal spelling
      `harness.KindClaude` is `"claude"` while the wire/config spelling is `"claude_code"`; the
      Goal-session launch intent and attach payload persist and send the internal value
      (`daemon/internal/app/goal_admission_tx.go:502,518,653,670` →
      `daemon/internal/app/goal_delivery.go:531`), while Control's attach inclusion list and DB
      constraint admit `"claude_code"` only
      (`control/lib/symmetry_control/goals/schemas.ex:509`,
      `control/priv/repo/migrations/20260909000000_add_goal_0006_control_plane.exs:517`). A claude
      native Goal-session attach would therefore be refused `:invalid_request` by Control. Nothing
      reaches it today because claude resolves to the unavailable adapter, so fixing it (map the
      attach request's kind through the wire spelling at that one boundary, plus a Control-side
      test) changes a wire field value and enables a previously refused flow — it needs its own
      authorization and independent review. Line numbers are the rf-a3 base `b9d4b4a4`; no test
      covers the cross-language boundary today (the app tests use a fake Control client).
      Owner area: daemon app + control goals.

## Retained-resume owner gate still answers by kind list (rf-a3 observation, 2026-11-23)

- [ ] Decide the retained-resume owner gate that still answers by kind list.
      `daemon/internal/app/native_run.go:2017` (`verifyNativeTaskResultSubject`) admits a
      retained-resume subject only while the running capability kind is pi/omp/omo. Since the
      rf-a3 Q1=a change, `admissionLaunchFailure` answers retained resume purely from the
      capability projection (probe: the truth table's `resume.advertised` rows pass for every
      kind), so a resume-advertising kind outside pi/omp/omo now passes admission and is then
      refused by this owner gate — a divergence no test covers (the table pins the admission
      answer only). Align the gate with the projection answer or record it as the deliberate
      retained-resume owner allowlist; do not delete the kind list silently. Owner area:
      daemon app.

## Duration ratchet: the adopted over-cap rows (rf-w6-12, 2026-11-24)

- [ ] Burn down the five rows the duration gate adopts instead of failing
      (`scripts/ci/go-test-durations.py` `ADOPTED_TEST_CEILINGS`, committed by the rf-w6-12 branch):
      `internal/app`'s `TestOutputBackpressureRecoversWithOneTruncationMarker` (167.20 s measured
      max on the race runner) and `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` (124.96 s),
      plus `internal/state`'s
      `TestProviderActionCompletionRejectsOverLimitWorstCaseUnknownWithoutMutation` (34.58 s),
      `TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded` (32.91 s) and
      `TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch` (33.88 s). The two
      `internal/app` rows do journal throughput work proportional to their 5 MiB fixture under
      `-race` (no timer in the body; the ten-run measurements live in
      `docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md`), so a smaller fixture at the same
      semantics or a production-side batching fix is the honest route; the three `internal/state`
      rows straddle the 30 s cap and are the first candidates. Owner area: `internal/app` and
      `internal/state` performance. Until then each row is ratcheted at 1.2x its measured max, and
      any test the roster does not name stays under the 30 s cap.

## Coverage-mode helper flake: the exit-hook diagnostic (resolved, decision note) (rf-flake-stdout-drain-64kib, 2026-11-24)

- note — resolved, decision only: `TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB` no longer fails
      when an instrumented helper child loses the shared `covmeta` rename race under
      `go test -coverprofile`. `daemon/internal/execution/runner_test.go` now strips a trailing block
      of exactly the three documented coverage-runtime exit-hook lines
      (`error: coverage meta-data emit failed: `, `error: coverage counter data emit failed: `,
      `warning: GOCOVERDIR not set, no coverage data emitted`) from the helper's stderr — only while
      the helper environment forwards `GOCOVERDIR`, only as a trailing block of documented lines
      matching those exact prefixes, each one logged — and the exact 196731-byte payload assertions
      plus `requireStrictSequence` are unchanged. This supersedes the rf-w4-12c §8 "the profile above comes from a pass that skips
      that one test" workaround (`docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md`) and
      the matching residual in `docs/plans/2026-11-21-rf-w2-17-coverage-plane-plan.md`. Measurements,
      the verbatim capture and the mutant rows:
      `docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md`.

      The race itself is untouched: concurrent instrumented helper children still rename
      `covmeta.<hash>` into the tool's shared merge directory, and Windows still loses the rename to
      `Access is denied`. The recorded alternative — one warm-up instrumented child per package, or a
      private `GOCOVERDIR` per helper — stays unapplied because it changes the shared helper every
      test in the package uses (the contract's rejected-as-larger option). A fourth coverage-runtime
      message, or a diagnostic that does not start at the payload end, still fails the test by design.

The descendant-window item the same goal's gate runs found (a fixed-sleep helper mode racing the
grandchild's stdout write) is fixed in that goal and archived in
`docs/archive/CLOSED-TODO.md`.

## rf-acc-native-burndown: Linux v2 native acceptance follow-ups (2026-11-24)

The capture half of this section's first item and the `synthetic_goal_cleanup` callback closed on
2026-11-24 (goal `rf-acc-native-containment-witness`, branch
`refactor/rf-acc-native-containment-witness`, base `477e4c62`); the closed text is in
`docs/archive/CLOSED-TODO.md` under `## rf-acc-native-containment-witness: the containment
witness's invisible App refusal (2026-11-24)`. What the capture named stays open as the item below.

The parent-death stop of this section's second item closed on 2026-11-25 (goal
`rf-acc-clone-cleanup-parent-death`, branch `refactor/rf-acc-clone-cleanup-parent-death`, base
`6b2d4f8f`); its text is in `docs/archive/CLOSED-TODO.md` under
`## rf-acc-clone-cleanup-parent-death: the installed-owner leg's parent-death stop (2026-11-25)`.

The clone-admission contradiction of this section's third item closed on 2026-11-26 (goal
`rf-acc-containment-goal-admission`, branch `refactor/rf-acc-containment-goal-admission`, base
`4440c5c0`); its text is in `docs/archive/CLOSED-TODO.md` under
`## rf-acc-containment-goal-admission: the clone-admission contradiction (2026-11-26)`, and the
goal's new residuals are in the `## rf-acc-containment-goal-admission: residuals (2026-11-26)`
section below.

The two remaining product-test stops of this section's first item are addressed as of 2026-11-26:
the AppArmor-userns fix (`rf-acc-apparmor-userns-profile`, run 3 `45659523339`) cleared
`clone_integration_linux_test.go:240` / `launcher_integration_linux_test.go:439` on its head, and
the root readback conversion (`rf-acc-b-process-readback`, this tree) replaces the `environ` rows
those runs then reached — bound by the conductor's Bind run `45737002125` on `57c8c775` (record
`docs/evidence/rf-acc-b-process-readback-2026-11-26.md` §7 "Bind 2"), which clears both rows and
records the next stop verbatim. The moved text and the closure statement are in
`docs/archive/CLOSED-TODO.md` under `## rf-acc-native-burndown: the installed-owner legs'
product-test stops (2026-11-26)`, and the conversion's residual is in
`## rf-acc-b-process-readback: residual same-class e2e reads (2026-11-26)` below.

- [ ] State the owner `Before` residual in the D3 `harness.md` amendment (2026-11-24, PR #277
      round-1 security hardening; evidence §8 and §10.7): the daemon preflight now accepts an
      effective owner `Before` (systemd's reverse of the daemon's own required
      `After=symmetry-owner.service`), so the owner's effective `Before` set is no longer inspected
      at all. It is ordering-only — it pulls no unit into the transaction — but a root-installed
      third-party unit that declares `After=symmetry-owner.service` adds an entry to that set with
      no refusal. Declared fragment/drop-in `Before=` edges to the daemon stay refused
      (`daemon/internal/linuxpreflight/systemd_linux.go`); the D3 amendment should state the
      residual explicitly.

## rf-acc-b-process-readback: residual same-class e2e reads (2026-11-26)

- [ ] Convert the same-class ptrace-gated A→B `/proc` reads this goal's census deferred (plan
      §7.2 option (i), outside the converted capture-path surface;
      `docs/evidence/rf-acc-b-process-readback-2026-11-26.md` §3, plan §3.2 rows 9-12, branch
      `refactor/rf-acc-b-process-readback`, base `8453da12`): the e2e
      `linuxV2RequireNoWritableCgroupFDs*` fdinfo audit with its in-leaf descendant identity read
      (`/proc/<B>/ns/pid`, `daemon/e2e/production_linux_v2_native_witness_test.go:3277`),
      `linuxV2CaptureDetachedChild`'s child identity, `linuxV2OpenExactPIDFD` +
      `linuxV2ReadPIDNamespaceIdentity` on a released B (`daemon/e2e/production_linux_v2_owner_cli_test.go`,
      literal-PID-reuse leg; the ns/pid read is `:3603`), and the post-release helper-exit
      `platform.ProcessIdentity` / `platform.CaptureLinuxUserNamespaceIdentity` / exe readlink plus
      leaf audit (`daemon/e2e/production_linux_v2_helper_exit_witness_test.go:427,431,489,466`).
      Each is ptrace-gated (`fdinfo` measured plain-A denied, A+`CAP_SYS_PTRACE` allowed) and none
      is reached by any recorded run: the diagnostic `shared` run (`45717008719`) stops at
      `launcher_integration_linux_test.go:452` (`stage=leaf`, the goal's S5), and the binding
      Bind 2 run (`45737002125`) stops at the pi leg's fresh-run precondition
      (`daemon/e2e/production_linux_v2_native_witness_test.go:91`), both before the
      `run_literal_pid_reuse_witness` leg (`linux-v2-native-acceptance.sh:3177`); the ten
      witness sets stop before the live capture. Trigger:
      the first run that reaches one, plus the fixture's reader-step A capability readback row in
      that run — if it prints `CapEff 0`, the first stop in the helper-exit witness is its `:427`
      released-B identity read (before the `:466` leaf audit).
      Two further `production_linux_v2_owner_cli_test.go` sites join this census: the root
      PID-reuse replacement/watcher targets (`runProductionLinuxV2LiteralPIDReuse` reads
      `:2843,2855,2863,2894,2941`; `linuxV2OpenPIDReuseExchange` reads `:3008,3023/3026,3028/3029,3039`)
      are ptrace-gated but root-credentialed (`:3018-3021` requires real/effective uid 0), so
      converting them needs a second root-target reader contract rather than the B-only reader
      (`reader_linux.go:484-490` refuses any non-B credential vector); the migration witness
      `platform.ProcessIdentity(live.PID)` (`:4229`, a B target) is the reader's class but its
      leg's `native_release` hold is still unreached. Both are plan §3.2 rows 11-12.
      Conversion consumes the same `daemon/internal/linuxreadback` reader with an added per-FD
      `flags`/`mnt_id` return and a leaf-wide per-PID capture loop, preserving each audit's race
      tolerances. Owner: the next `rf-acc` acceptance increment that reaches the live e2e capture.

- [ ] Product constraint (2026-11-26, goal `rf-acc-b-process-readback`; plan §3.2 rows 1-6, the
      evidence record's §7 S5 section, `findings/296-s5.md` §1/§6): a native image that dispatches
      on `AT_EXECFN` (uutils multicall and similar) is unsupported under the gate's fd exec
      (`execveat(..., AT_EMPTY_PATH)`): `AT_EXECFN` becomes `/dev/fd/<n>`, and Ubuntu 26.04's
      uutils multicall exits 1 with `coreutils: unknown program '4'` before reading anything. Both
      released legs now release a staged single-purpose ELF (the test binary): the boundary leg
      (`launcher_integration_linux_test.go:436`, helper `:765`) and the owner-loss pair (`:1231`,
      helper `:781`); `trustedSleepPath()` remains only for the two legs that never execute their
      image (`:225`, `:522`). The constraint stays for any future leg whose subject is a distro
      multicall applet. Owner: conductor.

- [ ] The reader's ready-file publish is non-atomic while the fixture's readiness poll hard-asserts
      its stat (2026-11-26, goal `rf-acc-b-process-readback`; record §5, round-2 review):
      `publishReadyFile` runs `unlink ready` → `createExclusive("ready")` → write → `Chmod` →
      `Chown`; `start_readback_reader`'s loop (`scripts/ci/linux-v2-native-acceptance.sh:2181-2184`)
      checks `! -L`/`-f` in the `if` but hard-asserts `0:$A_GID:440:1` and the exact bytes inside
      the body under `set -Eeuo pipefail`, so a poll sampling the create→chown window reddens a
      valid run instead of retrying. The window is speculative (~2e-4 per case set) and fails
      closed; fix later with `ready.tmp` + `renameat` (the response path's pattern). Owner:
      conductor.

- [ ] The reader stop proof is measured clean in run `45778210044` (`alternate_mount_refusal`);
      `shared` still aborts before its call site (2026-11-26, goals `rf-acc-b-process-readback`
      and `rf-acc-shared-journal-release`; Bind 2 run `45737002125` start proof, PR #300 Bind 2
      run `45778210044` stop proof): the new set ran `stop_readback_reader` and its assertions
      held (`readback-reader: stop signal requests=0`, 0 `did not stop cleanly`,
      `ActiveState=inactive`/`Result=success`/`ExecMainStatus=0`, ready file removed); in
      `shared` the dispatch aborts at the pi leg, before the single call site
      (`scripts/ci/linux-v2-native-acceptance.sh:3226`), so a `shared`-scoped stop row stays
      unmeasured, not failed. Owner: conductor (the first `shared` run that reaches the stop).

## rf-acc-containment-goal-admission: residuals (2026-11-26)

The writer/fixture key-agreement residual closed on 2026-11-26 (goal `rf-ci-owner-request-key-check`,
branch `refactor/rf-ci-owner-request-key-check`, base `8453da12`); its text is in
`docs/archive/CLOSED-TODO.md` under
`## rf-ci-owner-request-key-check: the writer/fixture Goal-admission key agreement has a permanent check (2026-11-26)`.

- [ ] The containment sets' next stops after the W1 umask fix (2026-11-26, goal
      `rf-acc-containment-goal-admission`; evidence record
      `docs/evidence/rf-acc-containment-goal-admission-2026-11-25.md` §3, run `45671784251`):
      both sets now pass the clone reserve and the fresh-leg umask wall (the authorized one-line
      `production` UMask arm is in; runs `45665341553`/`45668197894` red at the wall, run 4 zero
      `… are 05` lines), and both walls the run then stopped at are closed, moved to
      `docs/archive/CLOSED-TODO.md`: `codex_containment`'s AppArmor userns wall by
      `rf-acc-apparmor-userns-profile` (#293; its record §8 names the rows that clear the wall
      and the runs' next stops — the migration turn wait and the `shared` `/proc/<pid>/environ`
      identity rows — and its §12 frames them as next-goal territory), and
      `claude_containment`'s fixed
      `--json-schema` argv wall by `rf-claude-schema-argv` (closure under
      `## rf-claude-schema-argv: the Claude fixed-argv bound stops the containment leg
      (2026-11-26)`; its Bind run `45710455914` on `72f8d01e` shows zero `oversized` refusals
      and the claude transport reaching its first native stage, and the set's next stop is
      recorded in the `## rf-claude-schema-argv: residuals` section below). Both sets'
      `stage_replay_native_release_<kind>` legs remain unexecuted (each set stops at its first
      failing leg).

The `pi_provider_action`/crash-leg provider-scoped-admission item moved to
`docs/archive/CLOSED-TODO.md` under
`## rf-acc-pi-provider-admission: the pi provider legs move to a provider-scoped Goal admission (2026-11-26)`
(goal `rf-acc-pi-provider-admission`).

- [ ] Bind-run observation: codex post-wall cleanup race (2026-11-26, goal
      `rf-acc-containment-goal-admission`; evidence record
      `docs/evidence/rf-acc-containment-goal-admission-2026-11-25.md` §6, Bind run `45689981952`):
      the codex set's stderr carries 12 `dial unixpacket: connect: connection refused` lines,
      6 verbatim as `Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket:
      connect: connection refused` (`daemon/internal/execution/linux_owner_v2_linux.go:1292`;
      file sha256 `c5da521d…`, identical at both heads); run 4 had 0 such lines. Post-wall
      cleanup racing the dead gate; not chased (conductor ruling 2026-11-26). Owner TBD by the
      conductor.

## rf-acc-claude-stage-replay: residuals (2026-11-26)

- [ ] `checkout_ready` helper-residency census (2026-11-26, goal `rf-acc-claude-stage-replay`;
      DS audit P3, `findings/301-ds-audit-1.md`): the census that keeps the 90 s fatal on every
      non-proven helper stage is established for `helper_bound`, `launcher_bound`, `gate_bound`,
      `handoff_prepared`, `handoff_committed` and `lease_armed`, but not for `checkout_ready`,
      whose release is in flight (`releaseAttempted == true`, `released == false`) and which no
      case set runs with a helper-exit assertion any record documents (the `shared` set schedules
      it, but that set has pre-existing reds and no RF-ACC evidence row for that leg). Static
      tracing found no term that would refuse a stop there; if the helper stop is provable at that
      boundary, the kept unconditional 90 s fatal is a latent false red. Owner: conductor (measure
      the leg in the guest and record the helper's behaviour, or extend the predicate with that
      stage's durable proof).

- [ ] Guest-only product-mutant row, fix round r3 (2026-11-26, goal `rf-acc-claude-stage-replay`;
      record §8.5): the mutant "`markOwnerLost` latches owner loss without calling `stopOwned`"
      must red `TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude` and
      `…/native_release_codex` through the recovery client's `owner_loss` snapshot (no cached
      proof before the recovery's first `Stop`, which would otherwise supply one) and the
      daemon-crash witness; the local overlay mutants pin only the decision layers (record
      §4/§8.4), so the privileged guest is that mutant's first end-to-end oracle. Owner:
      conductor (dispatch the mutant through the acceptance job, or record it unreached).

- [ ] Owner-loss snapshot trust and ordering (2026-11-26, goal `rf-acc-claude-stage-replay`;
      record §8.2): the `owner_loss` field is witness telemetry — its trust root is the recovery
      process's own response, and its "before the recovery's first stop" ordering is structural
      (the capture call precedes `recoverContainmentOnlyJournals`, pinned by the owned ordering
      row), not proved in-band by the helper, which cannot tell same-identity callers apart.
      Owner: conductor (accept the structural pin for this goal, or fund a helper-side ordering
      artifact as a new goal).

- Closed by goal `rf-acc-owner-loss-capture-exit` (2026-11-27): two items of this section — the
      stage-replay router stage→arm mapping pin (round-2 Y3 P3-3) and the owner-loss capture's 40 s
      re-dial of a terminal `helper_exited` helper (round-4 R2) — are archived under the separate
      [router wait](archive/CLOSED-TODO.md#stage-replay-wait-selection) and
      [terminal helper exit](archive/CLOSED-TODO.md#terminal-helper-exit-during-owner-loss-capture)
      closures. The remaining items here stay open.

- [ ] The owner-loss capture's anchor seam default has no local row (2026-11-27, goal
      `rf-acc-owner-loss-capture-exit`; record §2.3): the delivered
      `linuxV2NativeWitnessOwnerLossAnchor` returns the real `*platform.LinuxOwnerAnchorClient`
      (`owner.anchor`); the two local capture rows substitute a stub for that client.
      Constructing the real client needs the installed A/B owner configuration
      (`platform.NewLinuxOwnerAnchorClient` reads it and requires the A UID/GID), so the one-line
      default body is executed only by the credentialed acceptance legs
      (`claude_containment`, `codex_containment`), not by a local row. Owner: conductor (accept the
      acceptance leg as its oracle, or fund a platform-side injectable client).

- decision note (2026-11-27, goal `rf-acc-owner-loss-capture-exit`; plan §5): non-terminal lookup
      outcomes for the owner-loss snapshot — the anchor's `retired` status and every transient
      lookup error — deliberately keep the poll-to-the-40 s-bound behavior; the delivered early
      return is scoped to the proven `helper_exited` classification plus the exact held identity.

- [ ] Round-2 ruling Y3's second residual (P3-5, six 2-line "what" doc comments on unexported
      helpers) is superseded by round-4 Q4 and closed: the round-3-added comment lines were cut to
      why-only (the final diff adds 11 comment lines in seven ≤ 2-line blocks: 10 of the 25 r3
      lines kept — one trimmed — plus Q1's one line; record §10's census), the deleted comments are
      gone with their helpers, and the comment cap runs on the scratch-clone commit of the
      delivered bytes (record §6). No "what" doc-comment residual remains; the standing rule is
      the comment cap.

## rf-claude-schema-argv: residuals (2026-11-26)

- [ ] The legacy Linux supervisor route (`linuxSupervisorMaxFrame = 16 << 10`,
      `daemon/internal/platform/linux_supervisor_protocol_linux.go:19`) carries the command's
      Args/Env as JSON and would refuse a 23 881-byte element (`errLinuxSupervisorFrameTooLarge`,
      `supervisor_linux_client.go:79,219-224`). It is not on the linux-v2 path that
      `claude_containment` takes (`linuxV2Requested()` dispatches first). Owner: platform, if a
      claude-like leg ever takes that route. No code change.

## rf-acc-pi-durability-matrices: pi-evidence residual (2026-11-24)

- [ ] Record the credentialed `linux-native-v2-acceptance` rows for the retargeted durability sets
      (2026-11-24, goal `rf-acc-pi-durability-matrices`; owner rulings Q3(a)/A1; plan
      `docs/plans/2026-11-24-rf-acc-pi-durability-matrices-plan.md`, evidence record
      `docs/evidence/rf-acc-pi-durability-matrices-2026-11-24.md`): the conductor dispatches the
      credentialed run after merge (owner-approved Environment), and its run id plus per-set pi
      turn count are the proof of record for the legs that hold strictly after `native_release`
      (`shared`'s stage matrix at `stop_receipt_durable`/`leaf_removed`/`journal_cleared`, its ack
      matrix at `stop_receipt_durable`/`journal_cleared`, `literal_pid_reuse` at `leaf_removed`,
      `stale_leaf` at `stop_receipt_durable`, and the helper-response-loss legs whose run must
      complete). The scratch runs' dummy credentials stop exactly at the pi turn; ruling Q3(a)
      accepts those turns, one small turn per such leg.

- [ ] The `shared` OpenCode legs' disposition after the Q6=F2 promotion (2026-11-25, goal
      `rf-opencode-phase1`; 0010 W6-04 P4 row/bullet): the legs stay F2-excluded and non-evidence,
      and their exclusion is re-anchored with the conductor's D3 amendment
      (`refactor/d3-amendment-draft.md` §1 P7, whose "while frozen under D1/W6-04" clause the
      promotion makes stale). `opencode_native_health` keeps its bytes, its allowlist entry and
      its position; a non-zero `shared` exit caused only by that row is expected, and no report
      may read it as V2 promotion evidence. Linux admission stays fail-closed until one dispatched
      `linux-native-v2-acceptance` `opencode_native_health` run is green.

## rf-opencode-phase1: promoted 2.4.1 cross-harness residuals (2026-11-26)

- [ ] Launch-time exact-blob re-verification (gdd review 3 STD-A05, 2026-11-26, goal
      `rf-opencode-phase1`): the digest binds the executable at probe time only. `Start` hands the
      bare program name to the execution runner, which re-resolves it via its own `LookPath`, and
      `execution.Invocation` exposes no expected-digest field, so bytes swapped between the probe
      and a later assignment launch are executed with the session workspace as CWD and the session
      credential in the environment while start/events/cancel stay advertised. The shape is
      identical in pi, codex and opencode, so a one-harness fix would fork the convention: fix the
      shared `harness/execution` seam and re-pin all three. Owner: conductor (new goal).

- [ ] Pre-connect SSE replay assumption (gdd review 3 STD-A08, 2026-11-26, goal
      `rf-opencode-phase1`): the per-turn validator opens at the exclusive boundary `after=0`, so a
      watched frame carrying `durable.seq == 0` fails the turn closed (`ErrCursorRegression`). The
      session is created in `Open` and the global stream is opened later in `StartTurn`; the
      recorded `session.created` cursor is 0 and the watcher rows skip seq-0 frames because the
      stream opens after session creation. If `/api/event` ever replays a pre-connect frame for the
      watched session, a promoted turn fails closed. The live promotion proof shows no backlog and
      the guard stays as-is (fail-closed is the accepted direction); the item closes on a live
      replay observation. Owner: conductor (new goal).

- [ ] Dead-code candidate: stale `--pure` probe stub (PR #298 round-2 DS supplement V4,
      2026-11-26, goal `rf-opencode-phase1`; `298-ds-audit-2.md` P3-1):
      `daemon/internal/harness/probeconformance/cancellation_test.go:71` sets the opencode row's
      `otherOutput` to `"opencode serve --hostname --port --pure"` — a pre-2.4.1 CLI surface the
      pinned 2.4.1 build rejects — and no contract reaches it: the field is wired only into
      `cancellationSuccessRunner` (`:151`, returned only for a non-`--version` call, `:215`), both
      cancellation contracts assert the injected runner's only call is `--version` (`:172`), and
      the struct's own comment (`:46-48`) records that no contract here reaches it. Pre-existing
      since `5a9eae9e` (2026-11-17; `git show 7111828b:…` carries the same line). Keep-or-delete
      when the file is next touched; not removed in PR #298's records-only round (no code bytes
      change after `a3722677`).

- [ ] rf-acc-witness-unresolved-exit: the App exits 0 on the `unresolved` stage-replay recovery while
      its own e2e requires a deliberate non-zero exit (2026-11-25, goal `rf-acc-pi-durability-matrices`;
      conductor ruling on implement `DECISION_NEEDED` #2; evidence record §4b, plan §3.10 class 1):
      the App writes the expected `unresolved`/`containment_unproven` response and returns the summary
      (`daemon/internal/app/linux_owner_witness_linux.go:501,:556,:596`) while
      `requireLinuxV2RecoveryExitStatus` (`daemon/e2e/production_linux_v2_owner_cli_test.go:177-193`)
      demands a deliberate non-zero process exit; the response-loss variant of the same outcome
      already joins `execution.ErrLinuxV2MutationAckLost` (`:627`), so the App is the likely defect
      side. Root cause first: which side is the contract? Then the smallest fix (return an error
      after writing the summary in those three branches). Blocks the four `unresolved`-expected sets:
      run `45481221551` (`stage_replay_create_started`) and run `45484692731`
      (`stage_replay_leaf_bound`/`launcher_bound`/`ack_loss_launcher_bound`).
      **Fixed (2026-11-25, branch `refactor/rf-acc-witness-unresolved-exit`, base `3b23dead`;
      clause (1) of this residual)** (plan
      `docs/plans/2026-11-25-rf-acc-witness-unresolved-exit-plan.md`, evidence record
      `docs/evidence/rf-acc-witness-unresolved-exit-2026-11-25.md`, conductor ruling Q1(a)): the
      three writers return through `writeLinuxV2NativeWitnessSummaryAndExit`, whose sentinel
      `errLinuxV2NativeWitnessContainmentUnproven` the CLI maps to one stderr line plus exit 1, so
      all four unresolved sets complete at their expected recovery on the ruling-E bytes (run
      `45540942939`, exit 0 each, with their allowlisted witness document and `status=passed`
      manifest; the response-loss sibling keeps its deliberate exit 1 through
      `execution.ErrLinuxV2MutationAckLost`); the response bytes, the proven outcomes' exit 0 and
      the response-loss path are unchanged (mutant rows M1-M4 and the sha256 equality table are
      record §5/§6).
      `ack_loss_launcher_bound`'s second wall — `linuxV2RequireSameOwnerIdentities` rejecting the
      unbound owner placeholder every pre-gate replay carries by construction — is fixed under
      conductor ruling E (2026-11-25) by iterating the same four pairs in the completeness pass and
      skipping only pairs whose marker and response sides are both that placeholder and that the
      held journal has not bound (the 2026-11-25 R2 tightening; record §11), with the mandatory
      table test `daemon/e2e/production_linux_v2_owner_identity_test.go`; its M-E1 guest row is run
      `45536878631` (byte-exact: run 3's e2e file is the skip-reverted function bytes) and the
      four-set run `45540942939` is green (record §10). The R2 tightening changed the e2e bytes
      after run 4, so run 4 no longer binds the delivered bytes; the re-bind run is `45548810398`
      (scratch head `4e863a69` = PR head `1c89f257` plus the two pinned scratch files) — all four
      sets exit 0 on the delivered bytes with `status=passed` manifests and an all-EQ sha256 table
      (record §12). This bullet stays in `docs/TODO.md` while clause (2) and the F4 item are open;
      no CLOSED-TODO move.

      No clause (1) text above is deleted: the root-cause question it asks is answered by ruling Q1(a)
      and the fix is the behaviour it names.

- [ ] rf-acc-release-recovery-diagnostics residual: the `migration` release arm is observed
      (2026-11-25, goal `rf-acc-release-recovery-diagnostics`; evidence record
      `docs/evidence/rf-acc-release-recovery-diagnostics-2026-11-25.md` §9, plan
      `docs/plans/2026-11-25-rf-acc-release-recovery-diagnostics-plan.md` §3): the diagnostics, the
      `stale_leaf` validator alignment and the fix-stage F1-F10 changes are applied (CLOSED-TODO, its
      own heading), but the release that must reach `migration`'s `native_release` hold still fails, and
      the binding run `45579163968` (head `8da0f941`, on the delivered bytes) reads
      `(release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other)` — a
      different arm than the pre-F1 stage-3 reading
      `(release_outcome_unknown; stage=native_exec arm=await_result wait_class=other)` (run
      `45564071868`). The failing read is the unowned `wire_linux.go:559` bootstrap-attempt read
      wrapped at the owned `gate_lifecycle_linux.go:1054-1056`, and the expected frame's producer is
      the gate C code / reused guest (`gate.c:1168`). The aborted-release stop measurement is
      intermittent (four occurrences in run `45555828634`, zero in runs `45557621798` and
      `45564071868`, four again in the binding run's bounded App stderr). `DECISION_NEEDED`, and the
      new goal `rf-acc-guest-native-exec` (conductor 2026-11-25 ruling; per-arm entry above) routes
      from that arm. The four
      `stale_journal_*` sets' green rows belong to
      `rf-acc-owner-loss-semantics` and are delivered there (binding acceptance run `45650762686`
      on the PR-head bytes: the four ruled sets plus `stage_replay_launcher_bound`, all five sets
      exit 0; the implement, apply and dedupe rounds `45608856419`, `45621915266` and `45628215703`
      are superseded rounds): the guard this entry named
      (`refusal=journal_without_unresolved_authority reason=release_proof_present
      recovery_error_class=recovery_failed`) is consumed by the ruled d1b retention outcome. The two
      `rf-acc-owner-loss-semantics` entries this goal carried (the F4 design question and the
      owner-identity polish) are closed in `docs/archive/CLOSED-TODO.md`; the clone-row disposition
      entry below stays open as designed.
      The hold-wiring half of this row is delivered by goal `rf-acc-migration-pi-hold` (2026-11-26;
      plan `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`, record
      `docs/evidence/rf-acc-migration-pi-hold-2026-11-26.md`): the pi runner path now stages its
      `native_release` hold from the release ACK, so the next Bind reads this arm's own inner
      sub-step instead of a missing marker. The arm's inner sub-step measurement above stays open
      here.

- [ ] rf-acc-owner-loss-semantics residual: no product disposition deletes a retained
      pre-checkout clone row (2026-11-25, this goal's d1b ruling; evidence
      `docs/evidence/rf-acc-owner-loss-semantics-2026-11-25.md`, runbook
      `docs/runbooks/daemon-process-stop.md` §Retained pre-checkout clone after a normal release):
      the same-RunKey recovery of a `handoff_committed` crash consumes the anchor-authenticated
      owner-loss stop evidence, binds the ordinary v2 release proof, and leaves the row in its
      retained shape (intent, seed, release proof; no checkout, abort or cleanup field). Admission
      treats exactly that shape as retired for unrelated work while keeping the seed/target paths
      and the cgroup leaf name reserved, but `LinuxCloneCleanupEligible`,
      `ClearLinuxCloneNoTargetRetention` and `DeleteJournal` still refuse it by design: a normal
      release proves that containment stopped, not that B never ran. The row, the A
      seed bundle and the `refs/heads/symmetry/clone-v2/base/<hash>` ref stay on disk until an
      explicitly authorized disposition command exists (the B target exists only once the clone
      bootstrap has created it, and the pre-checkout crash point this retention is recovered from
      precedes it: evidence record `docs/evidence/rf-acc-owner-loss-semantics-2026-11-25.md`
      §7.5); building that command needs its own ruling (which
      authority authorizes deletion and what evidence the disposition records).

- [ ] One retargeted case set has an additional pre-existing stop of its own (2026-11-24, same
      goal; pre-retarget baseline = the burndown's literal all-14 run `45431551609`, §3.16 of its
      record; burndown entries above): `shared` aborts in its owner-leg
      scratch group (`daemon/native/linux-gate/**`, three product-test stops) before any witness
      runs. The `synthetic_goal_cleanup` half moved to `docs/archive/CLOSED-TODO.md` under
      `## rf-acc-synthetic-cleanup-release: the controlled B worker's one-use RELEASE resolves
      again (2026-11-26)`. The case set stays product-side and tracked; its pi rows stay
      unreadable while that stop remains — the two pre-clone-guard legs are residuals behind the
      AppArmor-userns entry above (follow-up goal `rf-acc-apparmor-userns-profile`), the
      `clone_cleanup_linux_test.go:259` parent-death stop closed 2026-11-25; see
      `docs/archive/CLOSED-TODO.md` under `## rf-acc-clone-cleanup-parent-death` (the clone reserve
      is no longer among them).

- [ ] The pi durability witnesses bind `native_binary`/`native_binary_sha256`/`native_version_line`
      from the e2e publisher, not from the daemon (2026-11-24, same goal): the daemon's
      same-RunKey recovery path publishes no executable digest
      (`daemon/internal/app/linux_owner_witness_linux.go:2906-2911` runs only in `runFresh`), and
      `daemon/internal/**` is out of that goal's scope, so each retargeted e2e runner hashes the
      staged executable (`linuxV2BindDurabilityNativeBinary`) and the exporter checks the value
      against the pinned `pi_executable_sha`/`1.5.2`. If the recovery path ever records the digest,
      drop the publisher-side hash and read the response fields instead.

- [ ] `mix format --check-formatted` on `scripts/ci/provision-linux-v2-control-claims.exs` is red
      under the CI-parity toolchain from pre-existing Styler drift (2026-11-25, same goal,
      amendment F10(i); evidence record §6/§10.10): the CI-parity run (elixir 1.20.4 / OTP 29) is
      red at the PR base `33ad88a9` (110 removed/138 added lines) and at the delivered bytes
      (106/137); the drift is upstream Styler style over bytes this goal did not touch (alias
      splitting, `unless` → `if !(...)`, `@cases` list re-indentation); the elixir 1.19.6/OTP 28
      run is red at base and delivered alike (359 rejected lines at the delivered bytes, none on a
      goal-hunk line — round-2 correction, record §12.3). Resolve in a separate maintenance change
      (reformat the file under the CI-parity toolchain, or pin the formatter version); the gate
      stays recorded as an amendment until then, and this goal's own hunks are formatter-clean
      (hunk-scoped check, record §10.10).

## rf-acc-shared-journal-release: residuals (2026-11-26)

The `shared` pi leg's `:91` pre-existing-RunJournal stop is bound by the conductor's Bind 2 (run
`45778210044` on the head `66b27e63`: `:91` stays gone, `alternate_mount_refusal` passes;
acceptance met) (goal `rf-acc-shared-journal-release`, branch
`refactor/rf-acc-shared-journal-release`, base `7111828b`); its text and closure statement are in
`docs/archive/CLOSED-TODO.md` under
`## rf-acc-shared-journal-release: the shared pi leg refuses the prior refusal leg's RunJournal (2026-11-26)`.

- [ ] A refused pre-containment claim row is retired only by the product's terminal/cleanup path
      (2026-11-26, same goal; plan §1(b), record §8): `seedAuthenticClaim` writes `claimed` before
      the native-owner preflight (`linux_owner_witness_linux.go:3243` vs `:3253`), the refusal
      exits without a release or clear step, `DeleteJournal` is reachable only through
      `cleanupPending` (`cleanup.go:889-945`), and containment-only recovery no-ops on a row with
      no containment effect (`linux_owner_recovery_linux.go:1622-1650`). The conductor's round-1
      ruling kept the product-side retirement out of PR #300 (option (a), production byte): the
      mount-refusal leg now runs in its own case set (`alternate_mount_refusal`, record §15), so
      no other set observes the row. A product-side retirement for a claim that provably never
      started native work stays a separate decision. Owner: conductor.

- [ ] The case-set pins' allowlist detection is order-sensitive and duplicated, and the dispatch
      reader whitelists arm ids (2026-11-26, same goal, PR #300 round-2 review P3-1/P3-2,
      `findings/300-opus-review-2.md`): `scripts/ci/test-linux-v2-evidence.py:469` and
      `scripts/ci/test-ci-structure-extraction.py:814` find the allowlist only when the line
      starts with `shared|codex_containment|`, so a semantics-free reorder reds with a message
      naming the wrong cause; `parse_case_arms` (`test-linux-v2-evidence.py:435`) matches arms
      with `[A-Za-z0-9_|*]+\)` (`:444`), so an arm id outside that alphabet is dropped from the
      reader (both fail closed). Fix in a CI-maintenance change (one terminator-based reader
      imported by both pins; a wider arm alphabet), not in PR #300: its code/script bytes stay
      identical to the bytes Bind 2 bound. Owner: conductor.

The pi `provider_action` leg's clone-reserve residual this goal unmasks stays under
`## rf-acc-containment-goal-admission: residuals (2026-11-26)`.

## rf-acc-native-pin-refusal-label: residuals (2026-11-27)

The unapproved-native-image residual closed: the pin refusal is labelled at its `PreparedGate.Release`
call site (`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:936-937`, stage `native_image`
arm `image_pin`), so the helper renders `release_outcome_unknown; stage=native_image arm=image_pin`
instead of `stage=none`. The refusal and its predicates are untouched, every previously rendered
detail string is byte-identical, and the new `PreparedGate.Release`-level row
(`daemon/native/linux-gate/protocol/release_pin_label_linux_test.go`) reddens when the wrap is
deleted or its vocabulary value is swapped. Goal `rf-acc-native-pin-refusal-label`, base
`d45e3298`; the closed item is archived under
[the pin-label closure](archive/CLOSED-TODO.md#rf-acc-native-pin-refusal-label-the-native-image-pin-refusal-is-labelled-at-release-2026-11-27);
plan `docs/plans/2026-11-27-rf-acc-native-pin-refusal-label-plan.md`, record
`docs/evidence/rf-acc-native-pin-refusal-label-2026-11-27.md`.

- [ ] Re-Bind the PR head: the `synthetic_goal_cleanup` set stays green (its leg releases a resolved
      symlink-free image, so the new label adds no stop to it) and every previously-labelled stop in
      that log renders byte-identically. Owner: conductor.

## rf-acc-pi-provider-admission: residuals (2026-11-26)

The `pi_provider_action`/crash-leg provider-scoped admission is delivered (2026-11-26, goal
`rf-acc-pi-provider-admission`, branch `refactor/rf-acc-pi-provider-admission`, base `b27ea691`);
its text and closure statement are in `docs/archive/CLOSED-TODO.md` under
`## rf-acc-pi-provider-admission: the pi provider legs move to a provider-scoped Goal admission (2026-11-26)`.
The conductor's Bind303 (`shared` on `22d6c411`, run `45811007804`) is the acceptance: the
clone-intent/Subject refusal occurs 0 times in the whole log, the retained journal reads claimed
with `has_claim_grant`/`has_clone_intent`/`has_seed_receipt` true, and all 21 verified rows are
green. The leg's next stop, verbatim: `production_linux_v2_native_witness_test.go:91: wait for
isolated production owner-witness command: hidden owner-witness command exited unsuccessfully:
exit status 1`, with owner-witness stderr `wait for authentic Pi owner witness turn: pi rpc final
assistant message did not stop normally: "error"` — routed to the new goal
`rf-acc-pi-authentic-turn`, gated on #303; `stop_readback_reader` did not run (its Result row is
unmeasured) and the redacted-evidence export was refused
(`pi durability witness is unavailable: ack-loss-replay-checkout_ready.json`).

- [ ] The e2e provider-scope expectation keys on each case constructor's `providerActionRequired`
      flag, which no local check pins against the writer's `PROVIDER_ACCESS_CASES` or the fixture's
      provider-sync role (2026-11-26, same goal; plan §3.3, record §10.2): a drift — a
      provider-scoped leg whose witness-case constructor loses the flag, or a provider-free leg
      that keeps it — is refused fail-closed by the witness request reader
      (`daemon/e2e/production_linux_v2_native_witness_test.go:1695-1705`), but only on the
      credentialed dispatch. The writer/fixture pair is pinned statically by
      `scripts/ci/test-linux-v2-owner-request-keys.py`; a generated row test over the Go case
      constructors would close the third side. The reader's dispatch call is pinned only up to the
      predicate: deleting the `if refusal := linuxV2AdmissionProviderScopeRefusal(...)` block at
      `:1700-1702` or inverting its first argument leaves the whole `daemon/e2e` package green
      (overlay mutants X1/X2, exit 0; record §6), so the call-site wiring is guarded only by the
      env-gated production witness and the credentialed dispatch; a committed pin needs a seam
      that runs in CI — an env-gated probe test was ruled out because it would not run in CI
      either. Owner: conductor.

## rf-acc-synthetic-cleanup-root-removal: residuals (2026-11-27)

The seed residual closed: the conductor's re-Bind (Bind 2, run `45938637085` on `f3dbae66`) cleared
the exact `:506 … guest_stage=seed errno=13` stop (0 occurrences of that frame in the 2,870-line
log) and the witness reached the authenticated root cleanup after its durable Begin. The next
failing line — `production_linux_v2_cleanup_witness_test.go:882: run exact root cleanup through the
authenticated installed Anchor: Linux owner anchor returned no exact root clone-removal proof` —
moved to
[the root-removal closure](archive/CLOSED-TODO.md#rf-acc-synthetic-cleanup-root-removal-the-authenticated-cleanup-requires-non-writable-b-created-entries-2026-11-27).
The seed residual item and this goal's fix are recorded in
[the root-removal evidence](evidence/rf-acc-synthetic-cleanup-root-removal-2026-11-27.md).

- [ ] Re-Bind the corrected PR head: #305 run `45956915653` was green on `dd20a485` only. Confirm
      the prior `:882` root-proof stop remains absent and the fixed head reaches removal, A-side
      cleanup, journal retirement and a passed export, or record the next failing line verbatim.
      Owner: conductor.

- [ ] The witness call site at `daemon/e2e/production_linux_v2_cleanup_witness_test.go:574` is
      exercised only inside the credentialed `synthetic_goal_cleanup` case set: deleting that line
      reddens no locally runnable test (the predicates it calls are committed-tested). Oracle gap,
      recorded in the evidence record §7; an env-gated probe was ruled out because it would not run
      in CI either. Owner: conductor.

- [ ] Oracle gap (§7): no fault fixture exercises the kernel `Fstat` error after the walk opens an
      `O_PATH` component. The outer boundary in the credentialed witness at `:574` treats any walk
      error as fatal before durable Begin or the Anchor call. The conductor ruled out adding a kernel
      fault fixture for this round. Owner: conductor.

- Production observation (decision note, 2026-11-27, goal `rf-acc-synthetic-cleanup-root-removal`):
      an install-accepted clone root whose default ACL mask grants write produces a checkout whose
      directories and files are group-writable, which the exact root-cleanup checks refuse. Today
      that is fail-closed. The writable defaults remain on the production, daemon-service-workspace
      and clone-test fixture roots. Shared cleanup does reach the clone-test root, but its targets
      are provisioned before that root's writable default mask is installed (evidence §11). This
      fix changes only the synthetic cleanup fixture. Candidates if the
      conductor wants a production hardening: reject a writable default mask in the install
      preflight, set the created modes explicitly in the clone bootstrap, or make the cleanup mode
      checks ACL-aware. Evidence: record §2.3 and §11.

## rf-acc-pi-authentic-turn: the credentialed proof of the authentic turn (2026-11-27)

Stage 1 (root cause) and stage 2 (slices 1B1 and 2C1 of the owner rulings, 2026-11-27) of goal
`rf-acc-pi-authentic-turn` are delivered: records `docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md`
and `docs/evidence/rf-acc-pi-authentic-turn-2026-11-27.md`, plans
`docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md` and
`docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md`. The stage-1 items are closed in
`docs/archive/CLOSED-TODO.md`; what stays open is the proof that only a real credential can give.

- [ ] Credentialed proof of the authentic pi turn and of the two detached-child crash legs.
      Owner: conductor and owner. Under the ratified placeholder credential the `shared` wall now
      completes with the three authentic-provider legs named `credential_gated_*` and never
      reported as passed (the fixture, the exported `credential-gated-legs.json` declaration and
      the manifest's `credential_gated`/`missing_allowlisted_files` rows all say so), so
      `stop_readback_reader` and the redacted export become measurable again; the only path that
      can prove the real turn is the owner-approved `main` dispatch (ruling A3; runbook
      `refactor/pi-authentic-turn-dispatch-runbook.md`), grouped per ruling 6G1. Expected on that
      run: no `credential_gated` manifest entry, all three Control provider receipts, and a green
      `TestProductionLinuxV2PiNativeProviderAction` row.

## rf-acc-shared-reservation-durable: the shared Bind on the PR head (2026-11-27)

The goal's stage 1 and stage 2 are delivered (plan
`docs/plans/2026-11-27-rf-acc-shared-reservation-durable-plan.md`, record
`docs/evidence/rf-acc-shared-reservation-durable-2026-11-27.md`). The closed part moved to
`docs/archive/CLOSED-TODO.md`.

- [ ] The conductor's `shared` Bind on the PR head (the contract's acceptance): report the case-set
      exit, the `verified …` sequence and its first disagreement, the `reservation_durable` leg's
      outcome (expected: no `:224` stop), the credential-gated pi outcome verbatim in the log, its
      declaration member and the export status, the reader Result, and the first unexpected failure
      if the arm stops later in the replay matrix. The leg is CI-only, so no local run substitutes
      for it (record §5, §7, §10). The authentic pi proof stays with
      `## rf-acc-pi-authentic-turn: the credentialed proof of the authentic turn` above.

## rf-acc-shared-helper-bound-journal: the fresh-run guard on the retained tombstone (2026-11-28)

The goal's stage 1 and stage 2 are delivered (plan
`docs/plans/2026-11-28-rf-acc-shared-helper-bound-journal-plan.md`, record
`docs/evidence/rf-acc-shared-helper-bound-journal-2026-11-28.md`). The closed part moved to
`docs/archive/CLOSED-TODO.md`.

- [ ] The conductor's `shared` Bind on the PR head (the contract's acceptance): report the case-set
      exit, the `verified …` sequence and its first disagreement, the `helper_bound` leg's outcome
      (expected: no `:224` guard stop, because the guard now retires the retained
      `reservation_durable` tombstone), the `reservation_durable` row's continued green, the
      credential-gated pi outcome verbatim in the log, its declaration member and the export status,
      the reader Result, and the first unexpected failure if the arm stops later in the replay
      matrix. The leg is CI-only, so no local run substitutes for it (record §2, §7, §10). The
      authentic pi proof stays with `## rf-acc-pi-authentic-turn: the credentialed proof of the
      authentic turn` above.

- [ ] Deferred retirement arms (2026-11-28, record §4, §10): the guard still refuses the three daemon
      arms no case-set leg leaves in the shared StateDir — the clone-reuse-retired arm
      (`daemon/internal/state/linux_cgroup_journal.go:435-441`), a bare `LinuxCloneAbortProof` row and
      a legacy v1 `ContainmentAuthority` + StopReceipt row (`:457-462`). Each stops a leg with the
      existing ownership message instead of passing silently. If a future leg reaches one, mirror
      that arm with its own red-capable row, as this slice did for the abort and pre-gate arms.
