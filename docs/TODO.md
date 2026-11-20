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

## Dead-code candidates

- [ ] Revalidate and remove two pre-existing dead surfaces found while applying the
      provider-action-chain-removal simplify findings (2026-11-21).
      (a) `Integrations.execute_accepted_resource_sync/2` and its catch-all
      (`control/lib/symmetry_control/integrations.ex:554`, `:579`) have zero callers tree-wide:
      `git grep -nw "execute_accepted_resource_sync" -- control/` matches only those two definition
      clauses (the unanchored form also matches the `_unprojected` sibling's name), while the live
      sibling `execute_accepted_resource_sync_unprojected/2` is called at
      `control/lib/symmetry_control/integrations/provider_access.ex:1569`, `:1808`.
      `record_provider_action_failure/2` (`integrations.ex:1111`) is reachable only from its two call
      sites `:572`, `:575` inside this function and is dead with it; removing (a) must remove it too
      (`compile --warnings-as-errors` will otherwise fail).
      (b) The `{:provider_action_failure, code, outcome}` 3-tuple clause of `provider_failure/1`
      (`control/lib/symmetry_control/integrations/provider_access.ex:2311`) has no producer: its only
      literal builder was the non-nil-outcome clause of `provider_action_failure_result/2`
      (`integrations.ex:1134-1135` before the apply stage removed it), fed by the then-existing
      `outcome` argument of `record_provider_action_failure/3`; it was unreachable once the
      callerless projected chain was deleted. The surviving builders emit only the 4-tuple form:
      the sole literal builder is `unprojected_provider_action_failure/1` (`integrations.ex:369`,
      pass-through at `:364-365`), while `:514` is the clause head of
      `project_provider_action_failure/2` matching the 4-tuple and `provider_access.ex:2315` the
      4-tuple clause head of `provider_failure/1`.
      Do not delete either surface here; revalidate and remove in a separate maintenance change.

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

- [ ] Give `stopDescendantMonitor` the post-timer recheck its sibling join already has.
      `daemon/internal/platform/process_unix.go:752-769` waits on the monitor against a timer
      (`:763-768`) and returns `ErrLinuxDescendantContainmentUnproven` ("descendant monitor did not stop
      before deadline") whenever the timer arm wins, without rechecking `monitorDone`. `monitorDone` is a
      close-only unbuffered channel (`:610` created, `:809` the only close), so it stays ready once
      closed: a monitor that exits as the deadline expires leaves both arms ready, Go picks uniformly
      between them, and a monitor whose published reason is already in hand can be reported as a
      deadline failure. PR #105 (`e64f8c0`) fixed this shape in the barrier join `:665-688`
      (function `waitForInitialDescendantScan` `:642-696`, the recheck at `:682-686`, pinned by
      the join probe `process_unix_linux_test.go:2078` and the expired-timer probe
      `:2136`, whose assertion at `:2157-2158` reddens when the recheck is removed) — and recorded
      this site as out of scope as work "to be recorded in `docs/TODO.md`"; the same shape also sits
      in the final-scan RPC's timer arms (`:1042-1043` request, `:1055` result). Fail-closed, so the
      cost is manufactured
      unproven-containment results for every caller
      that joins the monitor's own reason (`:1121`, `:1277`, `:1351`, `:1542`,
      `containment_supervisor_linux.go:1836`, `prepareDescendantMonitorForOwnerLoss` `:771-794`).
      Apply the same recheck and pin both directions (a monitor that exits by the deadline joins; one
      that never exits still fails) in a separate maintenance change.

- [ ] Decide whether the Goal surface's `resolve_decision` press presents the page's goal version or a
      submit-time served one. `approvePlan` builds its envelope from `query.data`
      (`frontend/src/features/goals/detail.tsx:124`), the page's last settled read, while its sibling
      senders re-read at submit through `servedEnvelope` (`:77`, used by the generic command `:86` and by
      the plan admission `:129`), so a Goal advance between the last settled read and the press makes
      `ensure_preconditions!/2` (`control/lib/symmetry_control/goals.ex:8571-8585`) answer 409 `stale`
      (`control/lib/symmetry_control_web/protocol.ex:33`) and the card renders "The goal changed since
      this page loaded" (`detail.tsx:187`) for an action the operator would simply repeat. The writer
      that advances the Goal row's `lock_version` is identified, which settles the reachability half of
      the original note: every accepted command appends an event through `append_event!/7`
      (`goals.ex:8308`, explicit `Changeset.optimistic_lock(:lock_version)` at `:8359`), the transition
      and reconcile writers go through `Goal.transition_changeset`
      (`control/lib/symmetry_control/goals/schemas.ex:65`), and the amend branch bumps explicitly
      (`goals.ex:6338`); no `update_all`/`inc` writer targets the `goals` table (the only such sites
      in `goals.ex` update `Command` rows at `:5309` and `GoalDecision` rows at `:6327-6333`; the
      seven other sites under `control/lib` update `ProjectResource`, `WorkItem`, `Run` and `Command`
      rows), so no writer bypasses the optimistic lock. An asynchronous reconcile advance
      alone can produce the refusal, which is what makes the page-held version worth revisiting; fix
      direction: build the press's envelope from a submit-time read the way the plan admission does,
      re-deriving the presented decision's own version from that same read, or keep the page version and
      record the refusal plus reload as the intended recovery.

## Retro review follow-ups (second pass, 2026-11-20)

- [ ] Align the control-character escape case in the canonical JSON bytes both languages hash.
      `protocol.CanonicalizeJSON` (`daemon/internal/protocol/goal_subject.go:102`) re-encodes every
      string through `encoding/json` (`writeCanonicalJSON` `:122`, its string arm `:132-137`;
      `marshalJSONString` `:186` with HTML escaping off), and Go spells every control character
      below U+0020 that has no shorthand escape as `\u00xx` with lowercase hex digits, while the
      Elixir side hashes `SymmetryControl.RequestHash.canonical/1`
      (`control/lib/symmetry_control/request_hash.ex:5`, `Jason.encode!/1`; Jason 1.4.5 per
      `control/mix.lock:10`) and Jason spells the same character `\u001F` with uppercase hex.
      Each language is self-consistent — in Go both
      `{"value":"\u001F"}` and `{"value":"\u001f"}` canonicalize to `{"value":"\u001f"}` — so the
      defect is the pair: one logical value carries two canonical digests, one per language.
      Reproduced at `978abba` with throwaway probes (not committed): a Go test over
      `CanonicalizeJSON` printed `{"value":"\u001f"}` with
      `sha256:b43e9944b5d28b7f3e6bad4b289642d1786aa5ba80658cf2ed8bf87ecd642e51`, and
      `Jason.encode!(%{"value" => <<0x1F>>})` printed `{"value":"\u001F"}` with
      `sha256:520bd0e56ee01fe3ec0b1c8d5ac7cfc0c2aea0999e9de295553abe6e45dcff63`. The Elixir reading is
      not a lone choice: the contracts layer already mirrors it deliberately — `encodeJsonString`
      (`contracts/ts/semantics.ts:69-80`) emits the shorthand escapes plus an uppercase `\u00XX`
      for every other C0 control, documented as "exactly as Elixir `Jason` encodes it", and the pin
      is the boundary test "canonical JSON escapes control characters exactly as the control
      plane's Jason does" (`contracts/scripts/boundary.test.mjs:59-71`, U+000B and U+001B inside a
      schema-valid LongText description), which names `RequestHash.canonical/1` as the hashed text;
      `contracts:check` verifies the canonical sorted JSON Subject hash through that same TypeScript
      encoder (`contracts/README.md:62-66`). So the Go canonicalizer is the side that disagrees
      with the other two, and it is unpinned: its escaping test pins `\u0000` and the shorthand
      escapes but no `\u00xx` escape carrying a hex letter
      (`daemon/internal/protocol/goal_subject_test.go:64-73`),
      and a case-sensitive search for an uppercase-hex control escape
      (`grep -rn 'u00[0-9]*[A-F]' daemon/ --include='*.go'`) returns 0 lines. The normalization the
      hash must follow is written at `docs/design/protocol.md:311-316` and does not name the C0
      escape case, so deciding it stays the open choice.
      Reachable through the context snapshot: Control builds it from free text — `approved_goal`
      `"objective"` (`control/lib/symmetry_control/goals.ex:2151`) and `work_contract`
      `"title"`/`"description"` (`:2156-2157`) — and stamps the snapshot's `content_hash` with
      Jason (`context_content_hash` `:2602-2606`, via `RequestHash.canonical`), which the daemon
      recomputes with Go and refuses on mismatch
      (`daemon/internal/control/validation.go:1744-1772`, "context.content_hash does not match
      canonical snapshot"; the same rule at `daemon/internal/protocol/goal_schema.go:48-70`).
      The contracts boundary test states U+000B/U+001B are schema-valid inside a LongText
      description (`contracts/scripts/boundary.test.mjs:59-71`), so a work item whose description
      carries U+001F yields a snapshot the daemon rejects — a source-traced consequence, not
      executed end to end in this record. The `Subject` the daemon binds cannot carry C0
      characters (its fields are a UUID, a git object id and a sha256, `Subject.Validate`
      `goal_subject.go:39-47`; `Subject.Hash` `:81-88`), and `goals.ex:2636` hashes that same
      restricted value, so neither is the reachable seam. Fix direction: decide the canonical
      case once — the spelling the other two implementations already share (uppercase, i.e. Jason's
      bytes) or a shortest-escape normalization written into the rule itself — then add the missing
      Go pin (and a shared fixture if the choice tightens the rule), and treat any recorded digest
      over a control-bearing value as invalidated by the change. Suggested owner area: daemon
      protocol plus the Control hash owners, with the contracts owner for the normalization text.

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

- [ ] Re-point the pre-#111 owner keys minted for recorded names that start with a colon. The
      current encoder escapes a recorded value that starts with `:` by prefixing one
      (`frontend/src/features/work/presentation.ts:94-97`), so a recorded name `:Ada` mints
      `human:::Ada` (`agent:::Ada` on the agent side), while the pre-#111 encoder spelled it
      unescaped — `human::Ada`. The reader committed with the legacy-sentinel fix
      (`ownerKeyFilter`, `:135-139`) maps only the two sentinel words, so a bookmark or hand-typed
      link carrying the pre-#111 spelling normalises to itself and matches no row, and the
      pre-#111 key for a recorded name `:unrecorded` (`human::unrecorded`) is byte-identical to
      the escape the current encoder mints for the recorded name `unrecorded`, so it silently
      selects that other row and cannot be disambiguated (the collision repeats for deeper colon
      prefixes: for a recorded name with `k >= 2` leading colons, the pre-#111 key equals the
      current mint for the same name with `k-1` leading colons, for example legacy `human:::Ada`
      (name `::Ada`) is the current mint for `:Ada`). Reproduced at `3c705d7` (production bytes
      identical to `7f62afe`) with a throwaway vitest probe over the repository's own modules
      (deleted after the run, no test file kept): rows named `:Ada`, `::Ada`, `:unrecorded` and
      `unrecorded` mint `human:::Ada`, `human::::Ada`, `human:::unrecorded` and
      `human::unrecorded`; the legacy key `human::Ada` kept its spelling and `filterWorkItems`
      selected `[]`; the legacy key `human::unrecorded` selected the row named `unrecorded`; the
      legacy key `human:::Ada` selected the row named `:Ada` instead (the deeper-prefix
      collision); and `ownerKeyLabel("human::Ada")` prints `Ada`, dropping the colon.
      Reachable: `assignee_name` and `agent_profile` are length-bounded only
      (`control/lib/symmetry_control/workspaces/schemas.ex:472-473`), so a colon-leading name can
      be served. The shape was recorded as an explicit residual of the legacy-sentinel increment
      (`docs/plans/2026-11-20-legacy-owner-bookmarks-plan.md`, "Out of scope"). Fix direction:
      decide the reading for the colon-leading pre-#111 keys (re-point them like the sentinel, or
      accept the collision and document it), then pin both shapes in
      `frontend/src/features/projects/filters.test.ts`. Suggested owner area: portal
      work-surface/frontend.

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
      A smaller residual from the same review lives here too: the rollout-off browser case went unrun in
      hosted CI for two reasons — before `52cb4bb` its spec file was not in the render step's spec list,
      and the step passed no `SYMMETRY_GOALS_OFF_URL` (both closed below).
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      The rollout-off browser-case residual is closed (2026-11-19, `fix/ci-render-gate-fixtures`, PR #98):
      the render step starts a rollout-off release instance and runs the attention spec's rollout-off case
      at both widths; its closure narrative and hosted evidence are in the same section there.
      Remaining open: the legacy surfaces that
      read workspace-read members still render their own fallbacks instead of refusing, which is
      per-surface work outside this class; and the captures for `accepted_outcomes`, `settlement_attention`
      and `external_waits` are empty arrays, so those members are pinned by the boundary test and the
      real-served controller gate rather than by a fixture-level rename probe.

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

## Record and documentation hygiene

- [ ] Add a path-and-symbol existence check for the backticked repo paths and
      identifiers cited in `docs/runbooks/**`, `docs/design/quality.md` and the
      CLOSED-TODO closures. Location: a new `scripts/ci/` check plus its fixture
      test, run for pull requests the way the record-cleanliness pair is
      (`scripts/ci/check-record-cleanliness.py` with
      `scripts/ci/test-check-record-cleanliness.py`,
      `.github/workflows/ci.yml:78,1218`): scoped to the changed files it audits
      but not hidden behind the documentation-only fast path, so a runbook-only
      pull request still runs it. Why: a cited `file:line` or symbol rots
      silently when its owner moves — the 2026-11-19 revalidation found the
      closed item's own `read_model.ex:471` health-signal citation stale
      (`portal.md` cites `health/4` with no line number), and PR #99's "Review
      fixes (round 2)" replaced an untracked
      `control/deps/ecto/...` citation (`control/.gitignore:8` ignores `deps/`)
      that no index contains. Check that a backticked repo-relative path exists
      and that the cited identifier occurs in the cited file; do not validate
      raw `file:line` values (any edit above them would redden the gate).
      Verification: the check fails on a citation whose path or symbol is absent
      and passes on the current tree; fixtures pin both outcomes.
- [ ] Commit a fixture test for the `changes` job's changed-path classifier
      (`.github/workflows/ci.yml:26-65`), whose proof today is only the removed
      throwaway harness plus hosted runs. Pattern:
      `scripts/ci/test-no-legacy-routes-audit.py` (fixture-driven, runs ungated
      in the `legacy-route-audit` job; `.github/workflows/ci.yml:1355`). Pin:
      `docs/**`/`*.md`-only diffs report `run-heavy=false`; `docs/evaluations/**`,
      `docs/evidence/**`, `scripts/ci/model-routing-record-sanitize.py` and
      `scripts/ci/check-record-cleanliness.py` changes report `run-records=true`;
      `legacy-route-audit` runs unconditionally; non-`pull_request` events
      report both `true`. Companion increment: its `ci.yml` wiring would move
      line numbers this branch just pinned. Verification: the fixture test
      fails when any of those routings changes.

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

- [ ] The chat surface's in-flight `Load older` guard is still keyed by cursor bytes:
      `loadingCursor` (`frontend/src/features/chat/chat.tsx:172-221`) admits a second load for the
      same boundary while the first is in flight once a re-serve has renewed `history.loadCursor`
      and re-rendered the button (`frontend/src/features/chat/chat.tsx:560`), and a refusal of the
      superseded request still posts its notice after the retry already loaded the page. (The rest
      of that damage is fixed — 2026-11-20, fix round 2: a refusal rewinds the chain pointer and
      re-reads the captured scope only while the press still owns the chain boundary it captured,
      so a superseded one is only this notice.) Pre-existing: base re-keyed the cursor on every
      refresh, so the race predates the boundary-id work. Repro on those bytes: press Load older,
      refresh while its page is in flight, press Load older again (admitted because the cursor
      bytes changed), and let the first request's refusal settle after the second landed. Fix
      direction: key pending loads by scope + boundary id (token bytes as the fallback) and
      suppress the superseded refusal's notice.

- [ ] An ended chat chain never accepts a later served token: a chain drained through its own loads
      (`nextCursor` null, the recorded `nextBoundaryId` naming an older message) or a read that
      served the whole history (no continuation was ever established) keeps reporting "oldest
      history loaded" while a later serve at the same boundary offers a token, so the rows below
      the served page stay unreachable until a reload or a boundary move. Repro (rows ordered 3–54,
      so the first page is 5–54 plus a token and the older read 3–4 ends `null`): rows 1–2 becoming
      visible late — a slow concurrent post commits an older-timestamped row, since `inserted_at`
      is set at insert inside the transaction and the advisory lock is per action, not per scope —
      make the next read still bound at 5 and offer a token for it, which the ended chain drops.
      Found by the r2 review of `fix/chat-history-boundary-id` (2026-11-20), the round that removed
      the `adopt` branch covering the never-continued shape (the drained shape was never covered by
      it). Fix direction: treat "ended" as no usable continuation and re-open from the served
      boundary when a serve offers a token — the offered token's boundary is the served page's
      oldest message, so the re-walk re-reads the drained region and dedupes against the retained
      pages.
