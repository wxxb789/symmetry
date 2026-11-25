# rf-acc-containment-goal-admission — evidence record (Control-composed Goal admission for the four containment cases)

Goal `rf-acc-containment-goal-admission` (contract in the GDD state root; conductor ruling
`refactor/decision-witness-admission-2026-11-25.md`, both consults
`findings/admission-contradiction-consult-{sol,ds}.md`). Plan
`docs/plans/2026-11-25-rf-acc-containment-goal-admission-plan.md`. Branch
`refactor/rf-acc-containment-goal-admission`; dispatch head `4440c5c0`
(#290 `rf-acc-owner-loss-semantics`, the gate-dispatch PR). This record cites files by sha256 only;
host locations read `<worktree>` / `<scratch>`.

## 1. What changed (delivered bytes)

| File | sha256 | Change |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `0eb1e20f05a1cc84a5b034980e58ba7f43ece4b09de701e248cd037c7f7552a9` | the admission branch keys on the extracted `linuxV2WitnessRequiresGoalAdmission` (pi arm byte-identical; `codex`/`claude_code` rows join it); the refusal message names the harness (`%s owner-witness claim must carry the Control admission for the exact Subject`); post-run-4 (S1/S4): the helper comment names the excluded provider-free opencode rows and `TestLinuxV2WitnessRequiresGoalAdmissionRows` pins the 11-row set in-repo |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | the four containment rows move to the goal route via the `:opencode_goal` repository role (open repository resource); `runtime_adapter_binding?/3` asserts `adapter.kind` = the case's harness for that role; `@expected_fixture_runs` stays 48 (43 goal: 39 pi-durability + 4 containment; 5 legacy) |
| `scripts/ci/register-linux-v2-claim-machine.py` | `94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8` | the codex/claude claim runtimes gain `harness_kind`/`harness_version` (`0.168.0`/`2.1.323`, the pins at `linux-v2-native-acceptance.sh:1135-1136`)/`adapter_version`/`adapter_protocol_version` and the adapter operations block, mirroring the pi-durability spec; post-run-4 (S2): the three goal-assigned specs share one `goal_assigned_runtime_spec` builder and the emitted session payload is byte-identical (§4) |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` | the admission branch keys on `GOAL_ADMISSION_CASES` (the five goal-routed writer cases) instead of `profile_name == "pi-durability"`; its three checks and the legacy repository check are unchanged |
| `scripts/ci/linux-v2-native-acceptance.sh` | `84ecd4777367b7c3622940d31a5e705134da4269cc342326d18c92777759020e` | one line (conductor ruling 2026-11-26, W1): `production` joins the `stage-replay|negative` UMask arm — the fresh containment legs stand in for the installed daemon, whose units set UMask=0077 |
| `docs/plans/2026-11-25-rf-acc-containment-goal-admission-plan.md` | `51cd3361787bcda9d4480c923a75409159c802ca2eb37f85e01ee6e29fe0035a` | implementation plan (this goal, incl. §6's post-B4 ownership record, §7's W1 amendment execution and §8's post-run-4 S1/S2/S4 record) |

Not changed (byte-verified): `daemon/internal/state/linux_clone_journal.go`
`5a8455b80ad1b2f53678fab1904056f6739357aa984fce8105340f98a2018ede` (the reserve stays the wall it
was), `daemon/internal/execution/linux_owner_v2_linux.go`
`c5da521dda3c0d7f05fb008f9a5761d33a4cdb8a917d354d3b6930b031a36099` (stage-1(a) guard; untouched),
the real workflow, the exporter.

Mechanism: the fixture commissions a real Control Goal per case (create → plan decision →
accept_plan → activate → `admit_task`, `purpose: "implement"`, `allowed_actions: []`), so Control
composes the `symmetry.admission.v1` document with Subject equal to the fixture Subject and
`provider_scope: null`; neither the App nor the fixture authors an admission field. The e2e's
admission branch now proves, for the four rows, the same shape the pi-durability rows already
proved. **Substitution (per the contract's acceptance 5):** the harness can no longer distinguish
a Control-composed admission from a fixture-synthesized one; authenticity rests on the fixture
using Control's own Goal flow plus the run log.

## 2. Base census (run `45661224561`, head `1a5600f7` = `4440c5c0` + the branch-only scratch workflow and shim)

Scratch branch `scratch/rf-acc-containment-goal-admission`; throwaway workflow
`.github/workflows/scratch-rf-acc-containment-goal-admission.yml` (copy of the real workflow without
the Environment and without `secrets.*`/`vars.*`, dummy pins, both sets in one job) and the
ratified `gh` shim `scripts/ci/scratch-gh-shim` (sha256
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`, branch-only). Job
`linux-native-v2-acceptance` failed 17:18:16Z; the census reds, verbatim:

| Set | First failing check (verbatim) | Consequence |
| --- | --- | --- |
| `codex_containment` | `witness output: production_linux_v2_native_witness_test.go:101: owner-witness stderr: -linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` | the witness writes no document; the export refuses `redacted evidence export failed: containment witness native_binary does not match --native-binary`; set exit 1 |
| `claude_containment` | same refusal at `production_linux_v2_native_witness_test.go:109` | same export refusal; set exit 1 |

Both sets' journal projection at the stop: `{"local_state":"claimed","has_claim_grant":true,
"has_clone_intent":false,"has_seed_receipt":false,"recovery_required":false}` — the claim seed
happened and the clone reserve refused (stop B4 of the contract's run-path trace). The
`stage_replay_native_release_<kind>` legs never ran (the set stops on the first failing leg). No
red source other than the expected reserve refusal and its export consequence appeared; the
`contracts` job passed (17:06:51–17:08:04Z).

## 3. Scratch runs

| # | Run | Case sets | Head | Outcome |
| --- | --- | --- | --- | --- |
| 1 | `45661224561` | both | `1a5600f7` (= `4440c5c0` + the scratch workflow and shim) | base census (§2): both sets exit 1 at the clone reserve |
| 2 | `45665341553` | both | `9b8e1572` | **both sets pass the clone reserve**; zero `reserve owner witness clone` / `clone intent does not match` lines in the full log (5 280 lines); first failing check per set = the fresh-leg Git-metadata revalidation below; per-set gate reds `TestProductionLinuxV2CodexNativeContainment=run:1,pass:0,skip:0` and `TestProductionLinuxV2ClaudeNativeContainment=run:1,pass:0,skip:0`; each set's `stage_replay_native_release_<kind>` leg is not reached (the set stops at its first failing leg) |
| 3 | `45668197894` | both | `180c0a21` (= `9b8e1572` + an empty commit; identical tree `887ace7c649a224d0a8a66332a53b852b124e2ee`) | binding run on the frozen bytes: reproduces run 2 row for row — zero reserve refusals, the same four Git-metadata refusal lines, the same two gate reds and test names |

Run 2/3 stop, verbatim (`codex_containment` at `production_linux_v2_native_witness_test.go:101`,
`claude_containment` at `:109`; both sets identical):

```
witness output: production_linux_v2_native_witness_test.go:101: owner-witness command exit status: 1
witness output: production_linux_v2_native_witness_test.go:101: owner-witness stderr: -linux-v2-native-owner-witness: build explicit App-owned Linux v2 witness invocation: revalidate Linux owner before native invocation: verify A-owned Git metadata tree /opt/symmetry-owner-test/production/source/.git: Git metadata entry /opt/symmetry-owner-test/production/source/.git/logs/refs/heads/symmetry is accessible to B: effective permissions for UID 52001 GID 52001 are 05, require 0 and forbid 07
witness output: production_linux_v2_native_witness_test.go:101: wait for requested production owner-witness barrier: owner-witness command did not reach its requested durable stage
```

This is a post-B4 stop (past the clone reserve, before `adapter.Start`), and it is the class the
pi campaign fixed for its bundle witness modes (`UMask=0077`, pi evidence §2b item 16; the
mechanism comment at `scripts/ci/linux-v2-native-acceptance.sh:1856-1860`): the fresh
`production`-mode legs stand in for the installed daemon, whose units set `UMask=0077`, and the
clone seed's new A-owned Git metadata is B-accessible without it. **No existing goal owns the fix**
— it is one line in `scripts/ci/linux-v2-native-acceptance.sh`, which this contract excludes
(`DECISION_NEEDED`); `rf-acc-guest-native-exec` (merged as #289) excluded `scripts/ci/**` and
owned the RELEASE-read class, and `rf-acc-apparmor-userns-profile` owns the installer profile and
the fixture exception. Per
the acceptance clause ("a wall with no owner is `DECISION_NEEDED`") the stage verdict carries the
question; plan §6 states both options with the recommendation, and the named residual is recorded
in `docs/TODO.md` under `## rf-acc-containment-goal-admission: residuals (2026-11-26)` with the
one-line candidate fix (`case "$test_mode" in stage-replay|negative|production)`).

### W1 amendment (conductor ruling 2026-11-26) and run 4

Proof that `production` is the exact mode of the two containment sets' fresh legs:

```
$ grep -n "run_native_owner_witness codex_containment\|run_native_owner_witness claude_containment" scripts/ci/linux-v2-native-acceptance.sh
3043:    run_native_owner_witness codex_containment "$v2_codex_containment_events"
3047:    run_native_owner_witness claude_containment "$v2_claude_containment_events"
$ grep -n "production" scripts/ci/linux-v2-native-acceptance.sh | grep run_go_test
2285:      if run_go_test "$event_file" ./e2e "^${test_name}$" production \
2325:    if run_go_test "$event_file" ./e2e "^${test_name}$" production \
```

`run_native_owner_witness` is defined at `:2211`; both containment calls above are inside it, so
their fresh legs run `production`. The same sets' `stage_replay_native_release_<kind>` legs run
`stage-replay` (`run_kind_stage_replay_case` `:2384-2390` → `run_stage_replay_leg` → `:2435`),
already covered by the pre-amendment arm. The authorized one line makes `:1859`
`stage-replay|negative|production) command+=(--property=UMask=0077) ;;`; the amended file's
sha256 is in §1. Other modes that also stand in for the installed daemon and still lack the
UMask — listed, not added, per the ruling: `synthetic-cleanup` (`:1950-1956`; call
`:3060-3062` — the synthetic goal clone-cleanup witness seeds a clone at
`daemon/e2e/production_linux_v2_cleanup_witness_test.go:383`, and its own preflight call is
before the seed at `:214`; no observed umask dependency) and `marker-reader` (`:1969-1974`; call
`:2993-2994` — the hostile-marker-entry test, no clone seed).

Run 4 (`45671784251`, head `b0ba2694` = `180c0a21`'s tree + the one line) result from the
4952-line run log:

| Check | `codex_containment` | `claude_containment` |
| --- | --- | --- |
| `reserve owner witness clone` / `clone intent does not match` lines | 0 | 0 |
| `… are 05, require 0 and forbid 07` (umask wall) lines | 0 | 0 |
| first failing check | `production_linux_v2_native_witness_test.go:101` (the containment witness) | `production_linux_v2_native_witness_test.go:109` |
| `stage_replay_native_release_<kind>` leg | not reached (the set stops at its first failing leg) | not reached |
| gate red | `bad=TestProductionLinuxV2CodexNativeContainment=run:1,pass:0,skip:0, skipped=[], fail_events=2, invalid_json_lines=0` | `bad=TestProductionLinuxV2ClaudeNativeContainment=run:1,pass:0,skip:0, …` |
| export | `redacted evidence export failed: containment witness native_binary does not match --native-binary` | same |
| case set | exited 1; `Failed case sets: codex_containment claude_containment` | exited 1 |
| journal at the stop | `{"local_state":"claimed","has_claim_grant":true,"has_clone_intent":true,"has_seed_receipt":true,"recovery_required":true}` | same shape |

The two sets' first failing checks, verbatim (App stderr tail, bounded last 20 lines):

```
codex_containment:
-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/bin/codex": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other guest_stage=gate_drop_bounding_set errno=1)
claude_containment:
-linux-v2-native-owner-witness: start native owner witness transport: start "/usr/local/bin/claude": validate native command and three Runner pipes before cgroup reservation: Linux owner native argv contains an empty, oversized, or NUL value
```

Both are post-B4 and post-B5 (the journal shows the clone intent and seed receipt present),
before `adapter.Start`:

- **codex — the AppArmor userns wall, owned by `rf-acc-apparmor-userns-profile` (in flight;
  conductor ruling 2026-11-26 — not `rf-acc-guest-native-exec`, which merged as #289).** The
  decoded v3 clone-bootstrap failure frame carries `guest_stage=gate_drop_bounding_set errno=1`:
  the gate's `drop_capability_bounding_set` (`daemon/native/linux-gate/c-gate/gate.c:917`, called
  from `drop_to_b` at `:1010`) failed with EPERM, and the release read reports
  `release_outcome_unknown` (`gate_lifecycle_linux.go:696`, `:796-797`). That is the wall the
  `docs/TODO.md` entry `## Linux v2 owner hosts under the Ubuntu AppArmor userns restriction`
  records verbatim (`PR_CAPBSET_DROP` returns `EPERM` while the caller sits in the
  capability-denying `unprivileged_userns` profile), and the in-flight goal owns its fix (the
  per-program profile in `scripts/install-linux-owner.sh` plus the acceptance fixture exception).
- **claude — the fixed-argv bound wall, owned by `rf-claude-schema-argv` (new conductor goal,
  design pending; conductor ruling 2026-11-26).** The Claude adapter's fixed
  argv includes `--json-schema <compact task-result schema>`
  (`daemon/internal/harness/claude/adapter.go:160-180`); the exact Go-marshaled argument is
  23 881 bytes (probe below), over the 16 KiB `maxCommandString` bound
  (`daemon/native/linux-gate/protocol/wire.go:53`), so `NativeCommand.Validate` refuses at
  `wire_linux.go:450-452`, wrapped at `daemon/internal/execution/linux_owner_v2_linux.go:182`,
  before the gate starts. The codex adapter's fixed argv (`app-server --stdio`) passes the same
  bound. The owning goal's design is pending: raise the argv bound, shrink the schema argument,
  or move the schema off argv. Probe (a throwaway `main` against the daemon module, run in WSL,
  exit 0) printed `schema_arg_bytes=23881 limit=16384 oversized=true`.

Conductor ruling 2026-11-26 (after run 4): the goal's own deliverable is met on these bytes; no
further scratch run; the two stops above are this goal's residuals, carried with the owners
named here (both fixes stay outside this goal's owned set).

Scratch hygiene: only the branch `scratch/rf-acc-containment-goal-admission` was pushed; it was
deleted at the end of run 4 with `git push origin --delete` and `git ls-remote origin
scratch/rf-acc-containment-goal-admission` prints nothing (exit 0); the local worktree was
removed and the local branch (`b0ba2694`) deleted. The throwaway workflow and the ratified shim
existed only on that branch.

## 4. Local probes

### P1/P2 — fixture Goal admission and writer, on an ephemeral local database

Probe script `<scratch>/p1p2-probe.sh` sha256
`bbf7136885200cd00372c27d871aa885094790087ac19af326ed0ba77591dca1`; transcript
`<scratch>/p1p2-transcript.txt` sha256
`a49a35f051e6b2333da34dfb89f98e9909af4d8cd0725d3929b7d0fe7fe2094b`; the probe refuses any
database not named `symmetry_linux_v2_containment_probe{,_neg}` and ran `mix run` (prod) against
a local Postgres, with Control served from `control/` on the same database. Subject used by the
probe: commit `4440c5c0712040ad9d1b87cafdbe66403f3115f1`, tree_digest
`sha256:c4963a86fe5197a1fcfb8307688e5a243f9bdc0d582afe7724465425fc99e372`.

P1 positive, all four cases (fixture `prepare` summary: 43 goal cases (39 pi-durability + 4 containment) / 5 legacy work items;
`schedule` assigned a run on the codex/claude-native runtime at the OpenCode repository; the
writer accepted the Control ClaimResponse):

| Case | Admission schema | Subject = request Subject | `provider_scope` | `provider_access` | Capabilities |
| --- | --- | --- | --- | --- | --- |
| `codex_containment` | `symmetry.admission.v1` | true | null | absent | absent (treated empty) |
| `claude_containment` | `symmetry.admission.v1` | true | null | absent | absent (treated empty) |
| `stage_replay_native_release_codex` | `symmetry.admission.v1` | true | null | absent | absent (treated empty) |
| `stage_replay_native_release_claude` | `symmetry.admission.v1` | true | null | absent | absent (treated empty) |

`admission_purpose` was `implement` for all four; the admission document carried
`admission_id`/`goal_id`/`goal_revision`/`limits`/`model_profile` composed by Control. Control
omits `required_capabilities` when it is empty; the e2e helper (`len(actual) == 0`) and the
writer (`get(..., {}) == {}`) treat absent as empty, so the absence is the expected shape.

P1 negative (M4) — mutant registration `<scratch>/register-mutant.py` sha256
`c02360b861b2df811313d95375b9295e9da8a2e189c357432970593e5e10980c` (codex metadata block removed
mechanically by `<scratch>/make-register-mutant.py`, sha256
`e5e96b4c034ddb1b888540e9473870d5b3c6cf373f66728468cdde4c860f7d00`, delta −1159 bytes): `schedule
codex_containment` raised `** (RuntimeError) Control did not assign the exact isolated
owner-witness Task` from `await_task_run!/2` (`provision…exs:700`) after its 30 s wait; exit 1.

P2 negatives — the writer run against a one-shot stub Claim endpoint
(`<scratch>/stub-claim.py` sha256
`45111b72809d78186527dba69f41fa0ccd4dc1eca82f709675ead04bb834595f`); observed verbatim:

```
stub-scope exit=1 request_present=no stderr=goal-routed ClaimResponse admission carries a provider scope
stub-legacy exit=1 request_present=no stderr=goal-routed ClaimResponse Work input is not a goal admission
```

### P3 — extracted predicate, WSL `go test`

`<scratch>/p3-extracted.go` sha256
`ebb846af82debf35700aa4ac62b7d7c1be23a147ce79a088ecf6444f470deb93` (the function text extracted by
`awk` plus a `package probe` header) and `<scratch>/p3-probe_test.go` sha256
`23ce499c139b394c8da81a9cb7d1132f8d8645a07281487f7244df96f0a52ae4`. Observed on `go1.26.8
linux/amd64`: `--- PASS: TestGoalAdmissionInScope (0.00s)`. Positive rows: `stage_replay` (pi),
`codex_containment`, `claude_containment`, `stage_replay_native_release_codex`,
`stage_replay_native_release_claude`. Negative rows: `pi_provider_action`,
`helper_sigkill_detached_child`, `daemon_sigkill_helper_survives_detached_child`,
`opencode_native_health`, `stage_replay_unprovisioned_refusal` (harness `opencode`), `wsl2_denial`.

The S4 post-run-4 amendment pins the same row set in-repo:
`TestLinuxV2WitnessRequiresGoalAdmissionRows` (11 rows, `go test -count=1 -run
"^TestLinuxV2WitnessRequiresGoalAdmissionRows$" ./e2e/` PASS on `go1.27.0 linux/amd64`, exit 0)
runs in the package's existing CI job. The `awk` extraction of the delivered predicate re-hashes
to `ebb846af82debf35700aa4ac62b7d7c1be23a147ce79a088ecf6444f470deb93` (`cmp` clean against the
probe above), so this evidence keeps binding the delivered predicate text.

### S2 payload equivalence (post-run-4)

`<scratch>/s2-payload-check.py` sha256
`dd317d8b3dce29c621f995550b92baeaeae322a060fd4ce74f8af92163e1513c` runs the pre-S2 register bytes
(`06ef1f31a34252c7f2b5917a343aa85f3ba95c32ee3dd29de3e4d33eb7656ae7`, run 4's file) and the
delivered bytes
(`94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8`) against an in-process stub
Control endpoint and captures each session `PUT` body: both captures are 2993 bytes with sha256
`47bfd1c082a15f4a7771e0518e1ee9b18f1c9ac3730c0ea466775f1d76f18c23` (`cmp` clean, payload
`PAYLOAD_EQUAL`), so Control registers byte-identical runtimes for the delivered script and the
recorded P1/P2 runs stand.

### P4 — static

| Check | Result |
| --- | --- |
| `gofmt -l .` in `daemon/` (WSL) | 0 files listed |
| `GOOS=linux go vet ./...` in `daemon/` (WSL, `GOTOOLCHAIN=auto`) | exit 0 |
| `python3 -m py_compile` on both touched scripts | OK |
| `python3 scripts/ci/test-linux-v2-{control-receipt,evidence,go-events,host-prerequisite-probe,source-subject}.py` (WSL, root) | five suites `OK`, exit 0 each |
| `bash -n` + `shellcheck --severity=warning` on the branch-only shim | exit 0 |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` (WSL, amended) | exit 0 |
| `shellcheck --severity=warning` base (`cf5dd5cd…`) vs amended (`84ecd477…`) arm of that script | 64 output lines each, identical but for the file argument (exit 1 both, pre-existing findings) → delta 0; the added line is clean |
| `mix format --check-formatted` on the exs | no repo gate covers `scripts/ci/*.exs` (`control/.formatter.exs` is the only project config; CI's `mix format --check-formatted` runs inside `control/`); the local check reports the same pre-existing drift message for base and delivered alike (Elixir 1.19.6, exit codes masked by this WSL wrapper); hunk-scoped delta below |

Hunk-scoped formatter check (`<scratch>/p4-format-probe.py` sha256
`1cc471efd5634d2664daf999677c24f40db13198da7c5d7fe75328700ac156ee`) on
`fmt-base-raw.exs` (`e997de02…`, the base file) and `fmt-head-raw.exs` (`f982ef7a…`, the
delivered file): `base_rejected 106 / head_rejected 106 / goal_touched 22 / goal-touched rejected
lines [50,52,96,98] all with rejected base counterparts / new_drift_lines []` →
`PROBE_RESULT: CLEAN`. The four goal lines sit inside the `@cases` block whose re-indentation is
the pre-existing drift; this goal adds no new rejected line.

Fresh same-toolchain re-run in this stage (WSL Elixir 1.19.6/OTP 27, `mix format` applied to
copies of both raw files): `base_rejected 81 / head_rejected 80 / goal_touched 22 / goal-touched
rejected [50,52,96,98] / new_drift_lines []` → `PROBE_RESULT: CLEAN`.

Fresh re-run on the record-corrected bytes (2026-11-26, delivered five-file set): `gofmt -l .` 0
files (exit 0); `GOOS=linux go vet ./...` exit 0 (WSL, `GOTOOLCHAIN=auto`); `python3 -m py_compile`
both scripts exit 0; the five `test-linux-v2-*.py` suites exit 0 each (`OK`); `bash -n` exit 0;
`shellcheck --severity=warning` 64 output lines on both the base
(`cf5dd5cd749782d019732871563fba4d5db8279f0718ea2764b18f258ad906b9`) and the delivered
(`84ecd4777367b7c3622940d31a5e705134da4269cc342326d18c92777759020e`) script, identical but for the
file argument (exit 1 both, pre-existing findings) → delta 0; the P3 extraction from the delivered
Go file re-hashes to
`ebb846af82debf35700aa4ac62b7d7c1be23a147ce79a088ecf6444f470deb93` and `go test -count=1` PASS
(exit 0), with the M2/M5 flips of the extracted text reddening exactly the recorded rows (exit 1
each) and the restored run green. P1/P2's original runs stand for the four code files whose bytes
are unchanged (the §6 equality table).

Apply-resume re-run on the post-run-4 delivered bytes (2026-11-26, S1/S2/S4): `gofmt -l daemon/` 0
files (exit 0); `GOOS=linux go vet ./e2e/` exit 0 and the S4 row test PASS (exit 0) on `go1.27.0
linux/amd64` (WSL, `GOTOOLCHAIN=auto`); `python -m py_compile` on the delivered register script
exit 0; `ruff check` and `ruff format --check` (0.17.5, the repo `pyproject.toml`) on the delivered
register script exit 0 each; `jscpd-changed --base 9eb874bf --head <snapshot of the delivered
worktree>` exit 0 (no clone overlaps an added line); `check-record-cleanliness` and
`check-citation-existence` over the same range exit 0.

## 5. Mutants

| id | mutation | observed red | restored |
| --- | --- | --- | --- |
| M1 | one containment case reverted to the legacy route (the pre-fix exs state, caught by the P1 probe before the fix existed): `stage_replay_native_release_codex` still legacy while its runtime carries adapter metadata | `** (RuntimeError) generic Control claim runtime did not match the exact Work profile/resource` (`provision…exs:262`) — the route assertion is load-bearing; the writer half: `stub-legacy` refused the legacy ClaimResponse with no request document | delivered exs restores the goal route; probe rows green |
| M2 | e2e branch revert for one leg — extracted text `claude_code` → `claude_codex` | the two claude rows FAIL: `claude_containment: got false want true`, `stage_replay_native_release_claude: got false want true` | extracted file restored byte-identically (`ebb846af…` re-read after restore); probe passes again |
| M3 | tamper the produced request's `provider_scope` | writer refuses (`stub-scope`: `goal-routed ClaimResponse admission carries a provider scope`), no request document | stub only; the loader's equality/scope assertions are byte-identical to the pi-proven branch, and no local guest fixture exists to re-drive them — the e2e half is covered by that precedent and the hosted mutant was not budgeted |
| M4 | adapter metadata dropped from the codex registration | `** (RuntimeError) Control did not assign the exact isolated owner-witness Task` (30 s wait, `await_task_run!/2`) | delivered registration keeps the metadata; positive rows green |
| M5 | over-broad relaxation — extracted predicate `return true` for all provider-free rows | the three opencode rows FAIL: `opencode_native_health`, `stage_replay_unprovisioned_refusal`, `wsl2_denial` | extracted file restored byte-identically; probe passes again |

## 6. Binding equality table

Run 4 (`45671784251`, head `b0ba269457057427d82c9d6c308b81f369a21563` = `180c0a21`'s tree + the
authorized one-line UMask amendment) binds the walls on its own head bytes: zero reserve
refusals, zero umask lines, both sets red on their containment witness. The conductor ruling
2026-11-26 (apply S1-S6) then authorized the S1/S2/S4 amendments below; both deltas are off the
witness run path, and the conductor's Bind run on the PR head (`codex_containment
claude_containment`, one scratch run, expecting the same walls as run 4) binds the delivered
bytes. Its row is left empty here:

| Bind run | Head | Case sets | Result |
| --- | --- | --- | --- |
|  |  |  |  |

| File | Run-4 head bytes | Delivered bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `83c436c5c1656747285f66b1c90136b5e0d9dbf8b8e02993a21554559a90c5c4` | `0eb1e20f05a1cc84a5b034980e58ba7f43ece4b09de701e248cd037c7f7552a9` | no — post-run-4 S1/S4 delta (comment reword + row test); the predicate text re-extracts to `ebb846af…` (§4) |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | yes |
| `scripts/ci/register-linux-v2-claim-machine.py` | `06ef1f31a34252c7f2b5917a343aa85f3ba95c32ee3dd29de3e4d33eb7656ae7` | `94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8` | no — post-run-4 S2 delta (shared builder); emitted session payload byte-identical (`47bfd1c0…`, §4) |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `84ecd4777367b7c3622940d31a5e705134da4269cc342326d18c92777759020e` | `84ecd4777367b7c3622940d31a5e705134da4269cc342326d18c92777759020e` | yes |

Run 3 (`45668197894`, head `180c0a21fdc6813e7517ef7d7e4f8dd903c20db8`, tree
`887ace7c649a224d0a8a66332a53b852b124e2ee` = run 2's) carried the same four code-file hashes, so
the P1/P2/P3 probes remain bound to the delivered bytes. The plan/evidence/TODO/CLOSED-TODO files
are not run inputs (documentation only) and are therefore excluded from this table; the
branch-only scratch workflow and shim were deleted with the branch.

## 7. Stage-1 answers

### (a) `synthetic_goal_cleanup`'s release refusal class — different class, residual stays with the release-wall rows

On the dispatch head the guard that emitted `Linux v2 native release requires an App admission
check` is `validateLinuxV2Invocation` (`daemon/internal/execution/linux_owner_v2_linux.go`, sha256
`c5da521dda3c0d7f05fb008f9a5761d33a4cdb8a917d354d3b6930b031a36099`, `:339-341`): it refuses an
invocation whose `Callbacks.CheckNativeAdmission` is nil. That is not this goal's admission class —
this goal's wall is the clone reserve parsing the Claim's Work input
(`daemon/internal/state/linux_clone_journal.go`, sha256
`5a8455b80ad1b2f53678fab1904056f6739357aa984fce8105340f98a2018ede`, `:864-878`), a Control-composed
Subject equality; the release guard is a callback-presence check on the release path.

The refusal no longer reproduces because #280 wired the callback into the cleanup witness
(`daemon/e2e/production_linux_v2_cleanup_witness_test.go`, sha256
`d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6`, `:1054`, `:1079-1082`
`CheckNativeAdmission: store.CheckLinuxV2NativeAdmission(…)`), and `docs/TODO.md` records the
set's current stop as the release class (credentialed `main` run `45573253632`):
`production_linux_v2_cleanup_witness_test.go:499: start controlled B Git worker through the real
Linux v2 launcher/anchor/helper: resume process "/bin/sh": one-use Linux v2 RELEASE result is
unresolved: Linux owner helper rejected operation (release_outcome_unknown)`. Per the conductor's
Q3 ruling the residual stays under the existing release-wall rows (`rf-acc-guest-native-exec` /
release-recovery); no new goal, and this goal changes no byte outside its owned set.

### (b) How Control composes `provider_scope` for a `pi_provider_action` admission — recorded, not widened

At `4440c5c0` (`control/lib/symmetry_control/goals/admission.ex`, sha256
`24ee858e96e739c0f54e534269fc79df93830d0ef0e8e8a486b81853ddd02770`): for `purpose == "implement"`
and `item.change_target == nil`, `admission_provider_scope!/4` (`:1218-1262`) composes
`%{"resource_ids" => [resource_id], "operations_by_resource" => %{resource_id => ["resource.sync"]},
"change_target" => nil}` (`provider_scope/3`, `:1263-1268`) iff `"resource.sync"` is in both
`authority_policy.allowed_actions` and `execution_policy.allowed_actions`
(`provider_operation_allowed?/2`, `:1304-1307`); `required_capabilities` then gains
`provider_access: true` (`:333-335`); branch and pull-request change targets compose
`change.upsert`/`change.update` scopes. The fixture's goals set `allowed_actions: []`
(`scripts/ci/provision-linux-v2-control-claims.exs` goal attrs), so the four containment
admissions arrive `provider_scope: null` — the shape the e2e requires. A `pi_provider_action`
admission (and the two crash legs') would arrive provider-scoped, which is why those stay latent
behind the `shared` set's stops; the bounded `docs/TODO.md` entry opened by this goal records the
finding and the three latent cases.

## 8. TODO disposition and residuals

- `docs/TODO.md` `## rf-acc-native-burndown`: the clone-admission-contradiction bullet is gone,
  replaced by a closure note pointing at `docs/archive/CLOSED-TODO.md`.
- `docs/archive/CLOSED-TODO.md` `## rf-acc-containment-goal-admission: the clone-admission
  contradiction (2026-11-26)`: the moved text verbatim plus the closure statement (branch, base,
  plan, evidence record, the runs, and the residual pointer).
- `docs/TODO.md` `## rf-acc-containment-goal-admission: residuals (2026-11-26)`, three bounded
  entries: (1) the sets' post-W1 stops — the codex release class (`gate_drop_bounding_set`
  EPERM, owned by `rf-acc-apparmor-userns-profile`) and the Claude fixed-argv bound wall
  (owned by `rf-claude-schema-argv`); (2) the provider-scoped
  admission for `pi_provider_action` plus the two crash legs (conductor ruling Q2);
  (3) the writer/fixture key agreement has no permanent check (apply S3; owner: a follow-up goal
  after PR #292 reshapes `ci.yml`).
- `scripts/ci/check-record-cleanliness.py` and `scripts/ci/check-citation-existence.py` were run
  over the changed documents against worktree snapshot commits of the corrected records
  (`--base 4440c5c0`, head = a dangling `commit-tree` of the worktree state): both exit 0
  (1 record; 1 changed document, 648 path citations; the three notes name pre-existing
  gitignore-covered citations — portal assets and control deps — not additions); the earlier
  `4a3ec76170d7` snapshot carried the pre-correction text.

Residuals and known limits:

1. Both sets pass B4/B5 and stop at their next class (§3, run 4): codex at the AppArmor userns
   wall (owner `rf-acc-apparmor-userns-profile`), claude at the argv bound (owner
   `rf-claude-schema-argv`); the
   two `stage_replay_native_release_<kind>` legs and every B7-B8 assertion are still unexecuted.
2. Hosted mutant probes (M1's legacy-route hosted half, M2's on CI, M3's loader half) were not run
   — the budget went to the base census, the first fixed bytes, the run-3 binding run and the
   authorized W1 run 4; they stay an oracle gap with the local halves (§4/§5) as the substitute
   (conductor ruling 2026-11-26).
3. `mix format --check-formatted` on the exs reports the same pre-existing drift for base and
   head alike under the local Elixir 1.19.6 toolchain; no repo gate covers `scripts/ci/*.exs`,
   and the hunk-scoped delta is CLEAN (§4, P4).
4. `pi_provider_action` and the two crash legs stay latent with the legacy claim shape;
   `synthetic_goal_cleanup`'s current stop is the release class owned by the release-wall rows
   (stage-1(a), §7(a)).
5. The W1 UMask arm (`linux-v2-native-acceptance.sh:1859`) covers all six `production`-mode
   owner-witness legs (`:2285`/`:2325`), but run 4 exercised only the two containment legs: the
   `shared` set's four legs — `pi_provider_action` (`:3003`), `helper_sigkill_detached_child`
   (`:3007`), `daemon_sigkill_helper_survives_detached_child` (`:3008`), `opencode_native_health`
   (`:3021`; `shared` scheduled at `.github/workflows/linux-native-v2-acceptance.yml:122`) — are
   unobserved under `UMask=0077`; their coverage is the conductor-owned credentialed all-14
   dispatch (oracle gap).
6. The delivered bytes carry the post-run-4 S1/S2/S4 amendments (§6): run 4 binds the walls, and
   the conductor's Bind run on the PR head binds the delivered bytes; the Bind row slot in §6
   stays empty until that run.
