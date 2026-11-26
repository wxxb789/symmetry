# rf-acc-pi-provider-admission — evidence record (provider-scoped Goal admission for the three pi legs)

Contract: `goal-rf-acc-pi-provider-admission.md` (GDD state root, external to the repository); its
Ruling section is the conductor's own ruling of 2026-11-26 and it closes the
`docs/TODO.md` residual `## rf-acc-containment-goal-admission: residuals (2026-11-26)`. Branch
`refactor/rf-acc-pi-provider-admission`, base and dispatch head
`b27ea691dae8f2bbc462c200424acbfa6070452b` (#300 `rf-acc-shared-journal-release`). Plan:
`docs/plans/2026-11-26-rf-acc-pi-provider-admission-plan.md`. The acceptance clause is the
conductor's Bind `shared` run on the PR head; this stage records every local gate and leaves the
Bind row to the bind stage (§7).

## 1. What changed (delivered bytes)

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `scripts/ci/write-linux-v2-owner-request.py` | `a6d007374c7c4d64a96b0cc9a7fefc73d4120d4dba31f6ce796a30f5730d677a` | the three pi legs join `GOAL_ADMISSION_CASES`; the goal branch pins the exact provider scope for `provider_case` rows and `provider_scope: null` for the provider-free ones |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `ac1cfea88aa78b515f2e4b19e33e9ae28a9717531377c0fdcd9d8c8b17b07f97` | `:pi_provider` is goal-routed; the goal specs carry `provider_sync`; `resource.sync` lands in BOTH policies for those cases; the pi arms of `runtime_adapter_binding?` demand adapter kind `pi` |
| `scripts/ci/test-linux-v2-owner-request-keys.py` | `ecdb5e85edcf074c147868457e4cad466b85ea6728750c46973076857605a7cc` | N-role goal-route predicate, provider-sync role parse, writer profile rule, the provider-pair comparison, four new refusal mutants |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `df3b7ed32bcc2848a2b08e41b9f2ecfd5f67cdb601ead8438868c7bc88dccb46` | the three pi `(harness, request_case)` rows require the admission; the provider rows require the exact composed scope through the new pure helper; two row tests pin both |
| `scripts/ci/register-linux-v2-claim-machine.py` | `25b04fbcbe37cc9d678b7c07aa98690bd5cd276f4e08e1c89553eace166e3087` | the pi-native runtime spec becomes a goal-assigned native spec (`pi`/`1.5.2` + adapter block) with `provider_access: true` |
| `docs/plans/2026-11-26-rf-acc-pi-provider-admission-plan.md` | `38a205ee8bbcb117ed14f99bdcde10a62f98417e175c4c93de9ee9fc5fb2c292` | the accepted plan in the repo's plan location |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the closed residual moves out of `TODO`; this goal's bounded residual is appended |

Byte-identical to the dispatch head (declared, not changed): `daemon/e2e/production_linux_v2_owner_cli_test.go`
(`d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6`),
`daemon/internal/app/linux_owner_witness_linux.go`, `scripts/ci/linux-v2-native-acceptance.sh`,
`.github/workflows/linux-native-v2-acceptance.yml`, every `control/` byte. The contract's owned set
lists the owner-cli test and the witness seam; both were verified to need no change: the witness
request reader keys provider authority on the case name and the ClaimResponse
(`linux_owner_witness_linux.go:2616-2652`), never on the admission shape, and the owner-cli test
carries no legacy/goal admission assertion (the only admission branch in the e2e is
`production_linux_v2_native_witness_test.go:1682-1696`).

## 2. Clause census at the dispatch head

The plan §1 holds the byte-level census. Summary: contract Change 1/2 touch the writer
(`GOAL_ADMISSION_CASES` and the admission branch), the fixture (`@cases` roles, the split
predicate, `goal_case_specs`, `provision_goal_admission!`, `runtime_adapter_binding?`), the
key-agreement test and the e2e admission branch — all delivered here; Control's admission
composer (`control/lib/symmetry_control/goals/admission.ex:1218-1268`, `:1304-1307`, `:333-335`)
and the daemon's clone reserve (`daemon/internal/state/linux_clone_journal.go:864-878`) needed no
change, so the contract's `DECISION_NEEDED` clause is not triggered.

One scope amendment is carried and named: the register script (§8).

## 3. Stage-1 answers

At the dispatch head the three legs were the `:pi_provider` rows of the fixture's `@cases` table:
`legacy_cases` (the `Enum.split_with` predicate excluded only `:pi_durability`/`:opencode_goal`),
so they took `launch_one_shot_work_item!` (a goal-less WorkItem/Task, comment: "legacy (goal-less)
tasks must not carry goal-admission fields"), the writer's `else` branch required only
`work_input["repository_resource_id"]`, and the e2e required `admitted == false`
(`linuxV2WitnessRequiresGoalAdmission` false for `pi`+`pi_provider_action`). The App then refused
at its clone reserve: `ReserveLinuxCloneIntent` → `requireLinuxCloneJournalFence` refuses whenever
`protocol.ParseAdmissionInput(journal.Work.Input)` reports `present == false`, and the witness
persists the ClaimResponse Work verbatim (`linux_owner_witness_linux.go:4257-4290`). Verbatim from
run `45771518805` (`findings/300-binding-run.md` §3.1):

```
owner-witness stderr: -linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject
journal-ab37035b11d22622d0a2675198c4a187862b2d6ae4936c446a14b16c92aa019b.json {"local_state":"claimed","has_claim_grant":true,"has_clone_intent":false,"has_seed_receipt":false,"recovery_required":false}
```

Both the provider-access assertions and the clone reserve bind the same claim: the reserve reads
`journal.Work.Input`'s Subject, while Control's ClaimResponse `provider_access` is issued from the
frozen scope inside that same Work input (`ProviderAccess.issue/3` → `claim_scope_operations/2` →
`validate_goal_claim_authority/5`), so a provider-scoped admission satisfies both only when
Control composes the scope itself. A provider-scoped admission therefore needs, per file: the
fixture to commission a goal whose both policies list `resource.sync`; the writer to expect the
exact composed scope (not merely a non-null one); the e2e to accept the admission with the exact
scope for those legs; and a goal-assignable pi-native runtime for Control's assignment query.

## 4. Local probes

### 4.1 P1 — fixture and Control composition on an ephemeral local database

Probe `<scratch>/p1-probe.exs` (scratch only) ran `mix run` (prod) from the main checkout's
`control/` project (its `control/` bytes are byte-identical to the dispatch head; the checkout's
`deps/` and `_build/prod` were reused) against the ephemeral local database `symmetry_linux_v2_pi_probe` on
`127.0.0.1:5432`, with `GITHUB_ACTIONS=true`/`GITHUB_RUN_ID=local` so the delivered fixture's
`require_ephemeral_ci!` gate was satisfied, and with the delivered fixture bytes required
(sha256 `ac1cfea88aa78b515f2e4b19e33e9ae28a9717531377c0fdcd9d8c8b17b07f97`; the probe evaluates the
module definition only, because the file's own `System.argv()` dispatch would otherwise consume the
probe's arguments). The fixture's `prepare` ran its own `@expected_fixture_runs` check (48 cases:
46 goal + 2 legacy) and created the three resources; the probe registered the five runtimes from
the session payload the delivered register script emits (captured by the stub in §4.2, with the
real resource ids substituted), enrolled a machine through `Orchestration.enroll_machine/3` and
registered through `Orchestration.register_runtimes/3` — the same calls the daemon controller makes.

Per-case rows (each `schedule` + in-process `Claims.claim_run/3` serialized by the real
`SymmetryControlWeb.Protocol.claimed_run/4`):

| Case | goal-managed | admission | Subject = requested | `provider_scope` | `required_capabilities` | `provider_access` | runtime |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `pi_provider_action` | true | `symmetry.admission.v1`, purpose `implement` | true | exact: `resource_ids` `[resource]`, `operations_by_resource` `["resource.sync"]`, `change_target` nil | `{"provider_access": true}` | one github/repository grant, `["resource.sync"]`, exact resource | pi/1.5.2, adapter `pi`, provider capability true |
| `helper_sigkill_detached_child` | true | same | true | exact | `{"provider_access": true}` | same | same |
| `daemon_sigkill_helper_survives_detached_child` | true | same | true | exact | `{"provider_access": true}` | same | same |
| `codex_containment` | true | same | true | `null` | absent | absent | codex/0.168.0, adapter `codex` |
| `opencode_native_health` | false | legacy input (`repository_resource_id`, no schema) | n/a | n/a | absent | absent | adapter-free |

Every per-row check reported `true` (admission schema, purpose, exact Subject, exact provider
scope, the capability projection, the grant shape, the runtime binding and the provider-free
negative rows). The three pi admissions carry the exact scope Control composes for
`resource.sync` in both policies (`admission.ex:1218-1268`) and the claim gains
`provider_access` only for them (`:333-335`).

The mutant half ran the same fixture on the same database with the register script's *base* pi
spec (adapter-free, `provider_access` capability only, as at the dispatch head): `schedule
pi_provider_action` raised verbatim `** (RuntimeError) Control did not assign the exact isolated
owner-witness Task` (`await_assigned_run!/2` after its 30 s wait) — the register-script change is
load-bearing for goal assignment (`assignment.ex:322-341`, `:505-530`).

### 4.2 P2 — writer and register-script probes against a stub Control endpoint

Probe `<scratch>/p2-probe.py` (scratch only) served the enrollment, session-registration and claim
endpoints of Control's wire API and ran the delivered scripts against it.

Writer rows (delivered `write-linux-v2-owner-request.py`):

| Scenario | Exit | Request written | Message |
| --- | --- | --- | --- |
| `pi_provider_action` with the exact composed scope and the authentic grant | 0 | yes (`claim` preserved verbatim, Subject exact, barrier `native_turn_started`) | — |
| same, `provider_scope: null` | 1 | no | `provider-scoped ClaimResponse admission does not scope exactly resource.sync` |
| same, extra `change.upsert` operation | 1 | no | same |
| same, scope naming another resource | 1 | no | same |
| same, non-nil `change_target` | 1 | no | same |
| same, no ClaimResponse `provider_access` | 1 | no | `Pi ClaimResponse must carry authentic Control provider access` |
| `codex_containment` provider-free admission | 0 | yes | — |
| `codex_containment` with a provider scope | 1 | no | `provider-free ClaimResponse admission carries a provider scope` |
| `opencode_native_health` legacy claim | 0 | yes | — |

The last block of the probe replays the *authentic* Control claims from §4.1 through the delivered
writer: all five cases exit 0 and the written request preserves the authentic claim verbatim
(`pi_provider_action` `claim_sha256=ddd48d3801378cdf2bb3209c6b473a2ee4cb3066f441ce18716f4397eb358ac0`,
`helper_sigkill_detached_child` `f271583e194a73db44038fd40dc5e96df44cc41fb5fc171ac4dd5bc5ed662d30`,
`daemon_sigkill_helper_survives_detached_child`
`42998f7f9ee6a28e61dd2aad4cd3cf64fa3e9cccae61953a5e380b1e4950fce5`,
`codex_containment` `d55df3a651f98f7c478067baafd408c8d4ab2dc7146b9bb7403e8b8199610d40`,
`opencode_native_health` `e608e1f934fe8ea765569981a62b45e38815f565de17d98ec2f9c142a8dc77fb`), with the
admission's provider scope identical to the one Control sent. Transcript `<scratch>/p2-transcript.txt`
sha256 `8c6bc23246cc7879f02c3ff1c6b159712fd6546348a404f5f797bd0d490937b7`.

Register-script payload delta: running the base script
(`register-base.py` = `git show b27ea691:scripts/ci/register-linux-v2-claim-machine.py`, sha256
`94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8`) and the delivered script against
the same stub and diffing the captured session payloads yields exactly one changed runtime key,
`owner-witness-control-pi-generic`; the other four specs are byte-identical, so no downstream
consumer of the registration changed. The delivered pi spec is `harness_kind` `pi`,
`harness_version` `1.5.2`, `adapter_version` `symmetry-daemon:ci-linux-v2`,
`adapter_protocol_version` 1, `agent_profile` `pi-native`, adapter kind `pi` with the exact
operation block, `structured_input` true and `provider_access` true.

### 4.3 P3 — Go rows, formatting, vet

`gofmt -l` on both changed e2e files: 0 files (exit 0). The row tests run in the package's existing
CI job and locally on `go1.27.0 linux/amd64` (WSL, `GOTOOLCHAIN=auto`):

```
=== RUN   TestLinuxV2WitnessRequiresGoalAdmissionRows
--- PASS: TestLinuxV2WitnessRequiresGoalAdmissionRows (0.00s)
=== RUN   TestLinuxV2AdmissionAllowsProviderSyncRows
--- PASS: TestLinuxV2AdmissionAllowsProviderSyncRows (0.00s)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.023s
```

`GOOS=linux go vet ./...` from `daemon/` (WSL) exit 0; `go vet ./...` from `daemon/` (Windows)
exit 0.

The WSL full suite runs as the distro's root user, which one Linux-privilege fixture refuses before
its assertions: `internal/platform`'s anchor-shape harness prints `wire harness requires an
unprivileged A process: uid=0 euid=0 gid=0 egid=0` (13 occurrences) and the package exits 1; on
the same host `native/linux-gate/protocol` reddened on its first full-suite appearance
(`TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors`, an environment-sensitive in-process
FD-census probe) and is `ok` (8.520s, exit 0) on the isolated re-run. Deterministic per-package
exits in WSL: `go test ./e2e/ -count=1` → 0, `go test ./internal/platform/... -count=1` → 1,
`go test ./native/linux-gate/protocol/... -count=1` → 0. Attribution: the only changed Go file is
`daemon/e2e/production_linux_v2_native_witness_test.go` (package `e2e_test`); neither failing
package references `daemon/e2e` (`grep -rn "daemon/e2e" internal/platform native/linux-gate/protocol`
→ 0 matches), so their build closure is byte-identical at base and head, and the failures are
root-only refusals (proved by the unprivileged re-run, where `native/linux-gate/protocol` is `ok`
and `internal/platform` fails only on the ad-hoc probe environment — a group/other-writable
`GOTMPDIR` its witness-marker parent check refuses and `listen unixpacket … bind: invalid
argument` in that directory — never on the changed bytes).

### 4.4 Elixir formatter probe on the changed fixture

Under the repository's own formatter (`control/.formatter.exs`, which loads the `Styler` plugin;
`mix format --dot-formatter control/.formatter.exs` applied to copies of the base and delivered
fixture), the two rewrite diffs are *body-identical*: 387 diff lines each, `diff` of the two
bodies empty — no rewrite touches a delivered hunk, so the change adds zero formatter drift. The
first revision of the delivered file initially diverged (Styler prefers the two-line
`defp … when …, do: …` clause and rewrites a single-clause `case` into a match); both were
corrected and the probe re-run on the final bytes. `scripts/ci/*.exs` is outside every repo gate:
CI runs `mix format --check-formatted` inside `control/` whose `inputs` are `["*.{ex,exs}",
"{config,lib,test}/**/*.{ex,exs}", "priv/*/seeds.exs"]`, and `.credo.exs` includes only `lib/`
and `test/`, so the Credo strict ratchet is unaffected (no `control/` byte changed).

### 4.5 Probe artifacts (scratch, deleted after the stage)

`p1-probe.exs` `4165772bf58d74e80a9c40c5a91057188d594b3ec9e80966bab2c69d8ad54a7f`,
`run-p1.sh` `55833e436fa468c6c51862d32ac546ad9c7c74d1949b2cd3c737e5bf37036369`,
`p1-report.json` `772b929dc7dda1bf635aec5dcb095b003f1d1f574db97e634961cb2f5a7e8c5a`,
`p1-run.log` `4eaac293c5000087299cc1e8147d8db37878095ae06790f9115529b8436d8d77`,
`p2-probe.py` `e5a12121fbf542e9e5d13f93d24bcbe3c5846154448d961ed0c7b8a269e7e32d`,
`register-base.py` `94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8`,
`session-payload-head.json` `2f99e82daad7c3f62cb38fd90aa624b64f0571454e01c147b45e480f5549cac7`,
`session-payload-base.json` `341c043b62665df5e650018047c2008bca9aa5f8f82f5c1b12d2df39292b2631`,
`check-styler-both.sh` `1af321cf90521d3393b200c5e1e3190e64e06362850ee2092539b4506fcd3c4e`,
`do-format.exs` `8d0022035382fe382d02305203ab5f72a3288359fb110112e4eb8ee5b148ac27`.

## 5. Gates (command → exit code)

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `python scripts/ci/test-linux-v2-owner-request-keys.py` (root, Windows) | 0 | 9 tests, including the four new refusal mutants |
| `python3 scripts/ci/test-linux-v2-{control-receipt,evidence,go-events,host-prerequisite-probe,owner-request-keys,source-subject}.py` (root, WSL) | 0 each | all six suites `OK`; on Windows the evidence and host-prerequisite suites fail on Linux-only fixtures (systemd-unit path validation, `bash`-driven printer fixtures) with the same base bytes — my changed files are not inputs to the failing assertions, and the single head input the evidence suite reads (`linuxV2OwnerWitnessStderrLogLines`) is untouched |
| `python -m py_compile` on the three changed Python scripts | 0 | syntax |
| `gofmt -l` on both changed e2e files (WSL) | 0 | no files listed |
| `GOOS=linux go vet ./...` (`daemon/`, WSL) | 0 | linux/amd64 compile gate |
| `go vet ./...` (`daemon/`, Windows) | 0 | host compile gate |
| `go test ./...` (`daemon/`, Windows) | 0 | 27 `ok` packages, 0 `FAIL` |
| `go test ./...` (`daemon/`, WSL) | `e2e` 0 / `platform` 1 / `gate` 0 | linux/amd64: 29 `ok` packages including `daemon/e2e` (1.136s); `internal/platform` red only on the root-only privilege refusal, `native/linux-gate/protocol` red only on its first environment-sensitive census probe and `ok` on re-run (§4.3) |
| `go test ./e2e/ -run 'TestLinuxV2WitnessRequiresGoalAdmissionRows\|TestLinuxV2AdmissionAllowsProviderSyncRows' -count=1` (WSL) | 0 | §4.3 |
| Elixir: `mix format --dot-formatter` probe on base/delivered copies | 0 | body-identical rewrites, §4.4; `mix format --check-formatted` with the project config cannot run as a repo gate for `scripts/ci/*.exs` (not in the project's inputs) |
| `MIX_ENV=test mix test` over `test/symmetry_control/goals/commands_test.exs`, `test/symmetry_control/orchestration_assignment_test.exs`, `test/symmetry_control/orchestration_claims_test.exs`, `test/symmetry_control_web/controllers/provider_action_controller_test.exs` (WSL, `symmetry_control_test` created and migrated) | 0 | 129 tests, 0 failures (39.3s) |
| `MIX_ENV=prod mix compile --warnings-as-errors` (WSL) | 0 | the unchanged `control/` bytes compile clean |
| `MIX_ENV=dev mix credo --strict` + `python3 scripts/ci/assert-credo-strict-baseline.py --log …` (WSL) | 30 / 0 | the ratchet passes: 564 issues (5 warnings, 326 refactoring, 160 readability, 73 design) over 225 files at the baseline recorded from `95d1df45`; inputs byte-identical to base |
| `python3 scripts/ci/check-record-cleanliness.py --base b27ea691 --head <worktree snapshot of the final bytes>` | 0 | `1 changed record(s) pass the root-only check` |
| `python3 scripts/ci/check-citation-existence.py --base b27ea691 --head <same snapshot>` | 0 | `1 changed document(s) pass the path and symbol check (686 path citation(s))`; the three notes name pre-existing gitignore-covered citations, not additions |

Final-bytes binding: the delivered e2e file is `df3b7ed32bcc2848a2b08e41b9f2ecfd5f67cdb601ead8438868c7bc88dccb46`
at every gate above except the first Windows vet/test pair and the first WSL interaction, which
preceded one comment-line clarification inside the new row test; the affected package was re-run on
the final hash (row tests PASS, `go test ./e2e/ -count=1` exit 0, `GOOS=linux go vet ./e2e/` exit 0,
`gofmt -l` 0 files), and the file is Linux/amd64-tagged, so it is absent from every Windows build
the pre-clarification pair measured.

## 6. Mutants

| id | Mutation | Observed red |
| --- | --- | --- |
| M1 | delivered fixture's pi row reverted to the legacy role (`:pi_provider` → `:opencode`) | key-agreement test refuses, naming `pi_provider_action` |
| M2 | fixture's `provider_sync` derivation moved to another role | key-agreement test refuses: `provider-scoped rows disagree` |
| M3 | writer: one pi leg dropped from `GOAL_ADMISSION_CASES` | key-agreement test refuses, naming the leg |
| M4 | writer: one pi leg's profile changed to another profile | key-agreement test refuses, naming the leg |
| M5 | writer: exact-scope ClaimResponse mutated (null scope, extra operation, wrong resource, non-nil change target) | writer exits 1, no request written, exact message (§4.2) |
| M6 | register script: base adapter-free pi spec | Control refuses assignment: `Control did not assign the exact isolated owner-witness Task` (§4.1) |
| M7 | e2e: one goal-admission map row dropped (`helper_sigkill_detached_child`) | `TestLinuxV2WitnessRequiresGoalAdmissionRows` exit 1: `helper_sigkill_detached_child: linuxV2WitnessRequiresGoalAdmission = false, want true`; restored file sha256 back to `df3b7ed3…` |
| M8 | e2e: `linuxV2AdmissionAllowsProviderSync` relaxed to `return scope != nil` | `TestLinuxV2AdmissionAllowsProviderSyncRows` exit 1 on `two_resources`, `other_resource`, `extra_operation`, `wrong_operation` and `change_target` (`= true, want false`); restored file sha256 back to `df3b7ed3…` |

## 7. The conductor's Bind (acceptance clause)

The acceptance clause — "The conductor's Bind runs `shared` once on the PR head.
`TestProductionLinuxV2PiNativeProviderAction` no longer stops on the clone-intent/Subject refusal.
Record the next stop verbatim, or green; if it is reached, record `stop_readback_reader`'s Result
row" — is the bind stage's, on the delivered bytes: the witness's clone reserve now parses a
Control-composed admission whose Subject is exactly the intent's Subject (§4.1), which is the
precise refusal run `45771518805` hit. Its row is left for the bind stage.

## 8. Scope amendment carried by this goal (one CI script)

`scripts/ci/register-linux-v2-claim-machine.py` is not in the contract's owned-file list but is
required by its Change clause: Control's goal assignment admits only runtimes whose capabilities
contain both the adapter block and the task's required capabilities
(`control/lib/symmetry_control/orchestration/assignment.ex:322-341`, `:505-530`), and the three
goal-routed legs need `provider_access: true` on that same native runtime. The changed spec is the
one used by exactly these three legs; the other four registered runtimes are byte-identical (§4.2),
and the probe of §4.1 shows the base spec cannot be assigned at all. It is a CI fixture script —
neither Control admission policy nor daemon production code — so the contract's `DECISION_NEEDED`
clause does not apply; it is named here, in the plan §2.5 and in the report.

## 9. TODO disposition

- `docs/TODO.md` `## rf-acc-containment-goal-admission: residuals (2026-11-26)`: the
  `pi_provider_action`/crash-leg bullet moved verbatim to `docs/archive/CLOSED-TODO.md` under
  `## rf-acc-pi-provider-admission: the pi provider legs move to a provider-scoped Goal admission
  (2026-11-26)`; the section keeps a pointer line.
- `docs/TODO.md` gains `## rf-acc-pi-provider-admission: residuals (2026-11-26)` with the bounded
  residual of §10.

## 10. Residuals and known limits

1. The Bind acceptance row is unmeasured here (§7) — by design, per the contract.
2. The e2e's provider-scope expectation keys on the per-case `providerActionRequired` flag, which
   no local check pins against the writer's `PROVIDER_ACCESS_CASES` or the fixture's
   provider-sync role; a drift there is refused fail-closed by the witness request reader, but only
   on the credentialed dispatch. Recorded in `docs/TODO.md` with the conductor as owner.
3. The Windows runs of `test-linux-v2-evidence.py` and `test-linux-v2-host-prerequisite-probe.py`
   fail on their Linux-only fixtures at base and head alike (§5); the WSL runs are the recorded
   gate. `test-linux-v2-control-receipt.py` refuses without root (the WSL run had root). The WSL
   full Go suite carries two environment refusals (§4.3): `internal/platform`'s root-only
   privilege fixture (13 refusals, exit 1) and one environment-sensitive FD-census probe in
   `native/linux-gate/protocol` (`ok` on re-run); both packages' bytes and build closures are
   identical at base and head.
4. `mix format --check-formatted` with the project's Styler config cannot be run as a gate for
   `scripts/ci/*.exs` (outside the project's `inputs`); the body-identical rewrite probe (§4.4) is
   the replacement proof.
5. Full Control suite and the credentialed acceptance workflow are not run here: the contract
   routes the first to "never the full Control suite unserialized" (the four-file targeted run is
   recorded) and the second to the conductor's Bind.
