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
| `scripts/ci/provision-linux-v2-control-claims.exs` | `82668512928ade9ab7d52199d8edd9f62af3bce4e4c4b547f9365c8a5ad0a5e3` | `:pi_provider` is goal-routed; the goal specs carry `provider_sync`; `resource.sync` lands in BOTH policies for those cases; the pi arms of `runtime_adapter_binding?` demand adapter kind `pi`; the legacy branch binds `:opencode` in its fn pattern instead of re-asserting the destructured role |
| `scripts/ci/test-linux-v2-owner-request-keys.py` | `d04666d234215cfaef9d6056101733ba91af549062722881c5c2a29d83a42b24` | N-role goal-route predicate, provider-sync role parse, writer profile rule, the provider-pair comparison, four refusal mutants, the fixture's both-policies `allowed_actions` refusal table; the writer's shared `writer_cases` accessor replaces the duplicated `CASES` preamble; round 2 (U2) trims the module docstring to one purpose line (comment-only) |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `d988ca3bf8a0364f7d2f1c9aabe4cef14b53216e501a6002c49950e18d508abf` | the three pi `(harness, request_case)` rows require the admission; the admission branch's dispatch is the pure predicate `linuxV2AdmissionProviderScopeRefusal` (exact composed scope for provider rows, any non-nil scope refused for provider-free ones, both directions table-tested); three row/table tests pin the map, the scope check and the dispatch; merged head after `origin/main` `b0206393` (#301), round-2 pre-merge `2dd84c37512d898dd6c65b8b4e25d546d00107bbc6a4a193a8826e344e60e89d` (the U2/U7 comment deletion plus the gofmt field alignment it forced), fix-stage `b4141da9face795573dc1d3d5619625197743575cf6f65e462fb372e8cd79d08` |
| `scripts/ci/register-linux-v2-claim-machine.py` | `f6029087c0acf8c42478018f0f9672fdf7a914a715d4e49d0eb3c3ee128ad773` | the pi-native runtime spec becomes a goal-assigned native spec (`pi`/`1.5.2` + adapter block) with `provider_access: true`; round 2 (U7) deletes the rationale comment (comment-only) |
| `docs/plans/2026-11-26-rf-acc-pi-provider-admission-plan.md` | `5a671160bbf8072214b28bc0afca44872c2156ea12a475913a03a2f31f1071d4` | the accepted plan in the repo's plan location (helper renamed to `linuxV2AdmissionProviderScopeIsExact`; five provider-free goal rows; §2.4 amended at the fix stage to the delivered dispatch predicate and its table tests) |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the closed residual moves out of `TODO`; this goal's bounded residual is appended |

The apply stage (`rf-acc-pi-provider-admission-apply`, 2026-11-26) delivered the four re-bound rows
above from the accepted simplify findings F1-F4 (and the D1/D2 record corrections); §5 holds its
re-runs and §6 its new mutants M9/M10. The fix stage (`rf-acc-pi-provider-admission-fix`,
2026-11-26) applied the review r1 findings REV-A01..A04: the doc comment above
`linuxV2AdmissionProviderScopeIsExact` is deleted (REV-A01); the admission branch's dispatch is
extracted into the pure predicate `linuxV2AdmissionProviderScopeRefusal`, pinned both directions by
the new table test `TestLinuxV2AdmissionProviderScopeRefusalRows` (REV-A02; the pre-fix gap is
characterized in §6 M11); §4.1 and §6 name their measured revisions (REV-A03); §4.3 and §5 state
the changed e2e file for the formatter gate (REV-A04). The writer row is byte-identical to the
implement stage; §5 quotes the fix-stage re-runs. PR #303 round 2 (the
conductor's round-1 ruling U1-U7, 2026-11-26) applied the U2/U7 comment-only code delta above and
the one gofmt field alignment the Go comment deletion forced, and re-bound the record rows: U1
re-ran the §4.1 probe on the delivered fixture `82668512…` (§4.1), U3 reworded §12.4, U5 extended
the §6 mutants and the `docs/TODO.md` residual, U6 folded the Bind (§13), U7 deleted the register
script's rationale comment. The register row therefore differs from the implement stage only by
that comment deletion. The branch then merged `origin/main` `b0206393` (#301); the merge's only
content conflict was `docs/archive/CLOSED-TODO.md` (both branches append at EOF), resolved by
keeping both appended sections — `rf-acc-claude-stage-replay` first, this goal's section last —
with no other hunk changed; §5.1 re-runs the gates on the merged tree. Bind303 (§13) remains the
BINDING acceptance for the `22d6c411` bytes, whose delta to the round-2 bytes is comment-only plus
whitespace, and the conductor re-Binds `shared` on the merged head.

Byte-identical to the dispatch head at `22d6c411` (declared, not changed by this goal):
`daemon/e2e/production_linux_v2_owner_cli_test.go`
(`d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` at `22d6c411`),
`daemon/internal/app/linux_owner_witness_linux.go`, `scripts/ci/linux-v2-native-acceptance.sh`,
`.github/workflows/linux-native-v2-acceptance.yml`, every `control/` byte. After the `origin/main`
`b0206393` (#301) merge, the owner-cli test and the witness seam carry #301's bytes (unchanged by
this goal; the witness file is `3bf974a170e1fe237c2eebe1428719fac4467705c7c1dfe7ac9d3e1945006dcd`).
The contract's owned set lists the owner-cli test and the witness seam; both were verified to need
no change from this goal: the witness request reader keys provider authority on the case name and
the ClaimResponse (`linux_owner_witness_linux.go:2622-2658` on the merged bytes), never on the
admission shape, and the owner-cli test carries no legacy/goal admission assertion (the only
admission branch in the e2e is `production_linux_v2_native_witness_test.go:1695-1705`).

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
`deps/` and `_build/prod` were reused) against an ephemeral local database on `127.0.0.1:5432`
(`symmetry_linux_v2_pi_probe` at the implement stage; `symmetry_linux_v2_pi_probe_r2` at the
round-2 re-run), with `GITHUB_ACTIONS=true`/`GITHUB_RUN_ID=local` so the fixture's
`require_ephemeral_ci!` gate was satisfied, and with the DELIVERED fixture bytes required (sha256
`82668512928ade9ab7d52199d8edd9f62af3bce4e4c4b547f9365c8a5ad0a5e3`, §1): the implement stage
measured the commit-`66e4bc00` fixture `ac1cfea8…` (superseded for revision binding; below), and
the round-2 re-run (U1) measured the delivered bytes themselves — its probe bytes are the
implement-stage probe `4165772b…` with only the scratch-directory constant retargeted (`fa2f505b…`,
`@fixture` unchanged, pointing at the delivered worktree file), its report is `32ae6c74…` and its
log `60ba9d0d…`. The probe evaluates the module definition only, because the file's own
`System.argv()` dispatch would otherwise consume the probe's arguments). The fixture's `prepare`
ran its own `@expected_fixture_runs` check (48 cases: 46 goal + 2 legacy) and created the three
resources; the probe registered the five runtimes from the session payload the delivered register
script emits (captured by the stub in §4.2 — the round-2 payloads are byte-identical to the
implement-stage pair, `2f99e82d…`/`341c043b…` — with the real resource ids substituted), enrolled
a machine through `Orchestration.enroll_machine/3` and registered through
`Orchestration.register_runtimes/3` — the same calls the daemon controller makes.

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

Revision binding (review r1, REV-A03; closed by U1): the implement-stage run and its mutant half
measured the implement-stage fixture bytes `ac1cfea88aa78b515f2e4b19e33e9ae28a9717531377c0fdcd9d8c8b17b07f97`,
not the delivered `82668512…`; the round-2 re-run above measures the delivered bytes themselves
(all five per-case rows and the mutant refusal reproduced; report `32ae6c74…`), so the
implement-stage row is superseded for revision binding only. The delta between the two fixture
revisions is exactly one hunk: `git diff --stat
66e4bc00..dadb474d -- scripts/ci/provision-linux-v2-control-claims.exs` reports `1 file changed, 1
insertion(+), 2 deletions(-)` — the legacy branch's `Map.new(legacy_cases, …)` moves the
`:opencode` role check from the fn body into the fn head pattern. Every P1 row is goal-routed
(`:pi_provider`/`:pi_durability`/`:opencode_goal`); no goal-routed row, `goal_case_specs` entry or
`provision_goal_admission!` byte is in the delta. The apply stage's F3 probe (§12.2) re-ran
`prepare` on the delivered bytes (`work_items=2 goal_cases=46`); the runtimes came from the
delivered register script, byte-identical to the implement stage.

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

The round-2 regeneration of the payloads for the U1 re-run (same probe bytes, the delivered post-U7
register bytes) reproduced the writer rows and both session payloads byte-identically
(`2f99e82d…`/`341c043b…`); its own transcript is
`a466786b9603b6fac6d022690edbb2c2c846e847460514bb479ac763406b79b8` (per-run request hashes
differ).

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

`gofmt -l` on the changed e2e file (`production_linux_v2_native_witness_test.go`, the only e2e file
in the changed set): 0 files (exit 0; fix-stage re-run). The row tests run in the package's
existing CI job and locally on `go1.27.0 linux/amd64` (WSL, `GOTOOLCHAIN=auto`):

```
=== RUN   TestLinuxV2WitnessRequiresGoalAdmissionRows
--- PASS: TestLinuxV2WitnessRequiresGoalAdmissionRows (0.00s)
=== RUN   TestLinuxV2AdmissionAllowsProviderSyncRows
--- PASS: TestLinuxV2AdmissionAllowsProviderSyncRows (0.00s)
=== RUN   TestLinuxV2AdmissionProviderScopeRefusalRows
--- PASS: TestLinuxV2AdmissionProviderScopeRefusalRows (0.00s)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.016s
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
`go test ./native/linux-gate/protocol/... -count=1` → 0 at the implement stage (the apply stage
re-ran the suite after a host regression, below; the fix stage re-ran it on the final bytes).
Attribution: the only changed Go file is
`daemon/e2e/production_linux_v2_native_witness_test.go` (package `e2e_test`); neither failing
package references `daemon/e2e` (`grep -rn "daemon/e2e" internal/platform native/linux-gate/protocol`
→ 0 matches), so their build closure is byte-identical at base and head, and the failures are
root-only refusals (proved by the unprivileged re-run, where `native/linux-gate/protocol` is `ok`
and `internal/platform` fails only on the ad-hoc probe environment — a group/other-writable
`GOTMPDIR` its witness-marker parent check refuses and `listen unixpacket … bind: invalid
argument` in that directory — never on the changed bytes).

The apply stage re-ran the WSL full suite on the delivered bytes: `daemon/e2e` `ok` (2.585s),
`internal/platform` exit 1 on the same 13 root-only refusals, and `native/linux-gate/protocol`
now fails three tests (`TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors`,
`TestGateCommandStringBound`, `TestGateReleaseFenceOrdering`) because the WSL host no longer
carries `/usr/include/linux/capability.h` (`fatal error: linux/capability.h: No such file or
directory` in each probe's C compile); a `b27ea691` checkout of the package fails identically
(3 occurrences), so its bytes and build closure are unchanged and the failure is host-level.

The fix stage re-ran the WSL full suite on the final bytes: `daemon/e2e` `ok` (1.399s),
`internal/platform` exit 1 on the same 13 root-only refusals, and `native/linux-gate/protocol`
`ok` again (11.046s, 0 occurrences of the C-compile error) — the host carries
`/usr/include/linux/capability.h` (present at the fix-stage probe), so the apply stage's three
failures there recorded host state, not delivered bytes; the package's bytes and build closure
are unchanged at base and head.

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
The apply stage re-ran the same probe on the changed fixture: the rewrite diffs are again
body-identical (389 diff lines each; `diff` of the two diffs holds only the header and the hunk
offsets from the removed line), and no rewrite hunk names the changed region
(`grep -c "legacy_cases" ` on the delivered rewrite diff → 0).

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

Apply-stage artifacts (scratch, deleted after the stage; the two probes are carried verbatim in
§12): `p1-output.json` `f0b66e9ece5fcd9b19374abbbc433aa0e0f7161864aa6bcee911f0a79c7b3861`,
`p1-run2.log` `6073ce6662f680548a37f1e9e41c38c113592b4efcba7c43a18e9dd34733d630`,
`fmt-base.diff` `6d5ba1867a7a9da969f5ca35d8a4c10870f4621eb451bcf9e279f05d950aa2a2`,
`fmt-delivered.diff` `1f5df522bdc491fd325b77e970c10cece9df2447b6ec9f9c0e71fa127f352bdc`,
`jscpd-changed.log` `adef61e8a6b53610f7a7efde8e729a7b40f0afdac4926310a81a346f39183fd8`,
`credo.log` `51ac94b53febd7bab1b22965e6f494bd753dee8cb498057bc92236c07d6295b1`.

## 5. Gates (command → exit code)

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `python scripts/ci/test-linux-v2-owner-request-keys.py` (root, Windows) | 0 | 10 tests: the committed pair, four provider refusal mutants, the both-policies `allowed_actions` refusal table (apply and fix re-runs) |
| `python3 scripts/ci/test-linux-v2-{control-receipt,evidence,go-events,host-prerequisite-probe,owner-request-keys,source-subject}.py` (root, WSL) | 0 each | apply and fix re-runs on the delivered bytes: all six suites `OK`; on Windows the evidence and host-prerequisite suites fail on Linux-only fixtures (systemd-unit path validation, `bash`-driven printer fixtures) with the same base bytes — the changed files are not inputs to the failing assertions, and the single head input the evidence suite reads (`linuxV2OwnerWitnessStderrLogLines`) is untouched |
| `python -m py_compile` on the changed Python script | 0 | syntax (apply re-run; the fix stage changed no Python byte) |
| `gofmt -l` on the changed e2e file (WSL) | 0 | no files listed (apply and fix re-runs; the witness file is the only e2e file in the changed set) |
| `GOOS=linux go vet ./...` (`daemon/`, WSL) | 0 | linux/amd64 compile gate (apply and fix re-runs) |
| `go vet ./...` (`daemon/`, Windows) | 0 | host compile gate (apply and fix re-runs) |
| `go test ./...` (`daemon/`, Windows) | 0 | fix-stage re-run: 27 `ok` packages, 0 `FAIL` (the Linux/amd64-tagged e2e file is absent from the Windows build closure) |
| `go test ./...` (`daemon/`, WSL) | `e2e` 0 / `platform` 1 / rest 0 | fix-stage re-run: 30 `ok` packages including `daemon/e2e` (1.399s); `internal/platform` exit 1 on the 13 root-only refusals; `native/linux-gate/protocol` `ok` (11.046s) — the apply stage's three C-probe failures there were a host-state regression, the header is present again (§4.3) |
| `go test ./e2e/ -run 'TestLinuxV2WitnessRequiresGoalAdmissionRows\|TestLinuxV2AdmissionAllowsProviderSyncRows\|TestLinuxV2AdmissionProviderScopeRefusalRows' -count=1` (WSL) | 0 | fix-stage re-run: all three PASS, `ok … 0.016s` (§4.3) |
| Elixir: `mix format --dot-formatter` probe on base/delivered copies | 0 | apply re-run: body-identical rewrites (389 diff lines each), no hunk touches the changed region (§4.4); `mix format --check-formatted` with the project config cannot run as a repo gate for `scripts/ci/*.exs` (not in the project's inputs) |
| `MIX_ENV=test mix test` over `test/symmetry_control/goals/commands_test.exs`, `test/symmetry_control/orchestration_assignment_test.exs`, `test/symmetry_control/orchestration_claims_test.exs`, `test/symmetry_control_web/controllers/provider_action_controller_test.exs` (WSL, `symmetry_control_test` created and migrated) | 0 | 129 tests, 0 failures (39.3s) |
| `MIX_ENV=prod mix compile --warnings-as-errors` (WSL) | 0 | the unchanged `control/` bytes compile clean |
| `MIX_ENV=dev mix credo --strict` + `python3 scripts/ci/assert-credo-strict-baseline.py --log …` (WSL) | 30 / 0 | apply re-run: the ratchet passes, 564 issues (5 warnings, 326 refactoring, 160 readability, 73 design) over 225 files at the baseline recorded from `95d1df45`; inputs byte-identical to base |
| `python3 scripts/ci/check-record-cleanliness.py --base b27ea691 --head <scratch-clone commit>` | 0 | `1 changed record(s) pass the root-only check` (apply and fix re-runs; re-verified on the final record commit) |
| `python3 scripts/ci/check-citation-existence.py --base b27ea691 --head <scratch-clone commit>` | 0 | `1 changed document(s) pass the path and symbol check (686 path citation(s))`; the three notes name pre-existing gitignore-covered citations, not additions (apply and fix re-runs; re-verified on the final record commit) |
| `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` on the plan and this record | 0 / 0 | no host absolute path in the changed docs (apply and fix re-runs; re-verified on the final record commit) |
| `python <scratch>/comment-cap.py <scratch clone>` | 0 | no comment block longer than 2 lines among the added lines (apply and fix re-runs; re-verified on the final record commit) |
| `python3 scripts/ci/jscpd-changed.py --base b27ea691 --head <scratch-clone commit>` | 0 | apply and fix re-runs: `9 changed file(s), 1782 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new`; re-verified on the final record commit; the one notice is `docs/archive/CLOSED-TODO.md` (added lines outside the configured scan scope) |
| jscpd 4.3.0 census, `--min-tokens 25` over the five changed code files (the simplify stage's invocation) | 0 | 1 clone: the pre-existing `provision-linux-v2-control-claims.exs` self-clone (50 tokens, 8 lines), present at `b27ea691` too; the writer preamble duplication is gone (`grep -c writer_case_names` on the delivered file → 0; the shared `writer_cases` accessor replaced it) |
| F3 probe: `run!("prepare", …)` on the delivered fixture over the ephemeral DB (§12.2) | 0 | `work_items=2 goal_cases=46`, legacy profiles `["opencode-native", "opencode-native"]`, `P1-APPLY-PROBE OK` |
| M9 pin: `go test ./e2e/ -overlay <scratch overlay adding daemon/e2e/zz_apply_probe_test.go> -run TestProbeProviderFreeAdmissionCarriesProviderScopeRefusal -count=1 -v` (§12.1) | 0 | apply and fix re-runs: `--- PASS`; the provider-free control request returns `nil`, the mutant refused verbatim (`ok … 0.228s` at the fix stage) |

### 5.1 Round 2 (U1-U7) and the merged head (`origin/main` `b0206393`)

The round-2 code delta is comment-only in the three files above (U2/U7) plus the gofmt field
alignment the Go comment deletion forced; every row below ran on the MERGED tree (the round-2
scratch branch `22d6c411` + the round-2 commit, merged with `origin/main` `b0206393`;
`production_linux_v2_native_witness_test.go` `d988ca3b…`, §1) in WSL `go1.27.0 linux/amd64`.

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `gofmt -l daemon/e2e/production_linux_v2_native_witness_test.go` (`daemon/`, WSL) | 0 | 0 files listed — the round-2 comment deletion made the rows struct's fields contiguous, so the same change also aligns `name string` to `name  string` |
| `GOOS=linux go vet ./e2e/` (`daemon/`, WSL) | 0 | linux/amd64 compile gate on the merged bytes |
| `go test ./e2e/ -run 'TestLinuxV2WitnessRequiresGoalAdmissionRows\|TestLinuxV2AdmissionAllowsProviderSyncRows\|TestLinuxV2AdmissionProviderScopeRefusalRows' -count=1` (WSL) | 0 | all three PASS, `ok … 0.022s` |
| `go test ./e2e/ -count=1` (WSL, merged baseline) | 0 | whole package `ok … 1.134s` |
| X1 overlay (`!testCase.providerActionRequired`), whole package | 0 | SURVIVED by design: `ok … 1.177s` (§6 X1) |
| X2 overlay (the 3-line call block deleted), whole package | 0 | SURVIVED by design: `ok … 1.152s` (§6 X2) |
| `python3 -B scripts/ci/test-linux-v2-owner-request-keys.py` (WSL, merged tree) | 0 | `Ran 10 tests … OK` |
| U1: §4.1 probe re-run on the delivered fixture `82668512…` | 0 | all five per-case rows all-checks-true, the mutant refused verbatim; report `32ae6c74…`, log `60ba9d0d…` (§4.1) |
| §4.2 payload regeneration on the delivered register bytes | 0 | payloads byte-identical `2f99e82d…`/`341c043b…`; writer rows unchanged (§4.2) |
| `python <scratch>/comment-cap.py <scratch clone>` (merged-head commit) | 0 | no comment block longer than 2 lines among the added lines of the merged-head commit |

Final-bytes binding: the delivered bytes of §1 are the revision every gate above measured — the
fix-stage rows on the `b4141da9…` e2e file, the round-2 rows of §5.1 on the merged head
`d988ca3b…` (key test `d04666d2…`, fixture `82668512…`, register `f6029087…`, plan `5a671160…`) —
with one scope limit: the Windows `go vet`/`go test` pair cannot build the Linux/amd64-tagged e2e
file, so it binds it only through the unchanged closure. Every fix-stage WSL gate, the M9 probe
(§12.1) and the mutants (§6, §12.4) ran on the fix-stage e2e bytes; the round-2 WSL gates, the
X1/X2 overlays and the key test ran on the merged bytes (§5.1); the apply-stage Elixir gates keep
binding the unchanged fixture/key-test bytes; jscpd-changed, comment-cap and the record checks ran
on the delivered bytes (the scratch-clone commit for the last four) at the fix stage, and the
round-2 comment-cap re-ran on the merged-head commit (§5.1).

## 6. Mutants

| id | Mutation | Observed red |
| --- | --- | --- |
| M1 | delivered fixture's pi row reverted to the legacy role (`:pi_provider` → `:opencode`) | key-agreement test refuses, naming `pi_provider_action` |
| M2 | fixture's `provider_sync` derivation moved to another role | key-agreement test refuses: `provider-scoped rows disagree` |
| M3 | writer: one pi leg dropped from `GOAL_ADMISSION_CASES` | key-agreement test refuses, naming the leg |
| M4 | writer: one pi leg's profile changed to another profile | key-agreement test refuses, naming the leg |
| M5 | writer: exact-scope ClaimResponse mutated (null scope, extra operation, wrong resource, non-nil change target) | writer exits 1, no request written, exact message (§4.2) |
| M6 | register script: base adapter-free pi spec | Control refuses assignment: `Control did not assign the exact isolated owner-witness Task` (measured on the implement-stage fixture `ac1cfea8…`; the delivered delta is outside this row, §4.1) |
| M7 | e2e: one goal-admission map row dropped (`helper_sigkill_detached_child`, patch §12.4) | fix-stage re-run on the final bytes: `TestLinuxV2WitnessRequiresGoalAdmissionRows` exit 1: `helper_sigkill_detached_child: linuxV2WitnessRequiresGoalAdmission = false, want true`; restored file sha256 back to `b4141da9…` |
| M8 | e2e: `linuxV2AdmissionProviderScopeIsExact` relaxed to `return scope != nil` (patch §12.4) | fix-stage re-run: `TestLinuxV2AdmissionAllowsProviderSyncRows` exit 1 on `two_resources`, `other_resource`, `extra_operation`, `wrong_operation` and `change_target` (`= true, want false`) and `TestLinuxV2AdmissionProviderScopeRefusalRows` exit 1 on `provider_change_target`; restored file sha256 back to `b4141da9…` |
| M9 | e2e reader: a provider-free goal row (`pi`/`stage_replay`) request whose admission carries a fixture-side provider scope | fix-stage re-run on the final bytes (§12.1): `linuxV2ReadOwnerWitnessRequest` refuses verbatim `provider-free owner-witness admission carries a provider scope`; the same request with `provider_scope: null` passes the whole reader (exit 0) |
| M11 | e2e: the refusal predicate's dispatch flag inverted (`if !providerActionRequired`, patch §12.4); pre-fix characterization: the same arm swap on the inline branch left the whole `daemon/e2e` package green (`ok` 0.916s, REV-A02) | `TestLinuxV2AdmissionProviderScopeRefusalRows` exit 1 on `provider_exact_scope` (refused with the provider-free message), `provider_nil_scope`, `provider_change_target`, `provider_free_nil_scope` (refused with the provider message) and `provider_free_exact_scope`; reddens in CI's `daemon` job (`.github/workflows/ci.yml`, ubuntu-latest, non-root runner: the full race pass runs `go test -race -count=1 ./...` over the linux/amd64-tagged e2e package) |
| M12 | e2e: the refusal predicate's provider-free arm deleted (`_ = scope; return nil`, patch §12.4) | `TestLinuxV2AdmissionProviderScopeRefusalRows` exit 1: `provider_free_exact_scope: … = <nil>, want "provider-free owner-witness admission carries a provider scope"`; same CI job and non-root runner user as M11 |
| M10 | fixture: the `allowed_actions` binding replaced by a constant, one policy's consumption dropped, or a third consumer added outside both policies | the delivered `test_allowed_actions_policy_drift_is_refused` subtests refuse verbatim: `bound exactly once to the provider_sync conditional, got 0 matches`; `consumed exactly once per policy, got execution_policy=0, authority_policy=1` (mirror: `execution_policy=1, authority_policy=0`); `consumed exactly twice, got 3` |

X1/X2 are the call-site mutants the DS audit raised (its P3-1): they are *supposed* to stay green
— the reader's dispatch call site has no committed local pin — so they are recorded as survived
mutants feeding the `docs/TODO.md` residual, not as red rows. Both were re-run at round 2 on the
merged `d988ca3b…` bytes (whole `daemon/e2e` package, WSL):

| id | Mutation | Observed |
| --- | --- | --- |
| X1 | e2e reader: the dispatch call's first argument inverted (`!testCase.providerActionRequired`; overlay `x1.go`, sha256 `5ff17968bb2e94aef5b5113b62e1817c68f68fa507532f3f2cce7fa7e52fbd2b`) | SURVIVED: whole package green (`ok` 1.177s, exit 0) |
| X2 | e2e reader: the 3-line `if refusal := linuxV2AdmissionProviderScopeRefusal(…) { return …, refusal }` block deleted (overlay `x2.go`, sha256 `aa280207e1359178b3a5b74f9b9c6981d721f41bf73dc93cbe70ac7f6b4807b1`) | SURVIVED: whole package green (`ok` 1.152s, exit 0) |

## 7. The conductor's Bind (acceptance clause)

The acceptance clause — "The conductor's Bind runs `shared` once on the PR head.
`TestProductionLinuxV2PiNativeProviderAction` no longer stops on the clone-intent/Subject refusal.
Record the next stop verbatim, or green; if it is reached, record `stop_readback_reader`'s Result
row" — is the bind stage's, on the delivered bytes: the witness's clone reserve now parses a
Control-composed admission whose Subject is exactly the intent's Subject (§4.1), which is the
precise refusal run `45771518805` hit. Its row is Bind303 (§13): run `45811007804` on `22d6c411` is
BINDING — whole-log refusal count 0, journal claimed with
`has_claim_grant`/`has_clone_intent`/`has_seed_receipt` true, all 21 verified rows green. The
conductor re-Binds `shared` on the merged head (§1, §5.1).

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

1. The Bind acceptance row was unmeasured at the fix stage (§7); Bind303 (§13) now carries it and
   is BINDING, and the conductor re-Binds `shared` on the merged head.
2. The e2e's per-case `providerActionRequired` flag — the dispatcher input the fix stage pins both
   directions through `TestLinuxV2AdmissionProviderScopeRefusalRows` — is not pinned against the
   writer's `PROVIDER_ACCESS_CASES` or the fixture's provider-sync role; a drift in the flag's
   per-case values is refused fail-closed by the witness request reader, but only on the
   credentialed dispatch. The reader's dispatch call itself is pinned only up to the predicate:
   deleting or arg-inverting it leaves the whole `daemon/e2e` package green (X1/X2, §6), so that
   wiring is guarded only fail-closed at the same credentialed dispatch (an env-gated probe test
   was ruled out — it would not run in CI either). Recorded in `docs/TODO.md` with the conductor
   as owner.
3. The Windows runs of `test-linux-v2-evidence.py` and `test-linux-v2-host-prerequisite-probe.py`
   fail on their Linux-only fixtures at base and head alike (§5); the WSL runs are the recorded
   gate. `test-linux-v2-control-receipt.py` refuses without root (the WSL run had root). The WSL
   full Go suite carries known environment refusals (§4.3): `internal/platform`'s root-only
   privilege fixture (13 refusals, exit 1); the apply stage's `native/linux-gate/protocol`
   C-probe failures (host header absent) were host state — the fix-stage re-run has the package
   `ok` again with the header present (§4.3). Both packages' bytes and build closures are
   identical at base and head.
4. `mix format --check-formatted` with the project's Styler config cannot be run as a gate for
   `scripts/ci/*.exs` (outside the project's `inputs`); the body-identical rewrite probe (§4.4) is
   the replacement proof.
5. Full Control suite and the credentialed acceptance workflow are not run here: the contract
   routes the first to "never the full Control suite unserialized" (the four-file targeted run is
   recorded) and the second to the conductor's Bind.

## 11. Contract invariants → executable rows (delivered bytes)

| Contract clause | Executable row |
| --- | --- |
| `resource.sync` in BOTH `authority_policy.allowed_actions` and `execution_policy.allowed_actions` (Ruling) | key test `test_the_committed_pair_agrees` plus `test_allowed_actions_policy_drift_is_refused` (M10); the §4.1 per-case rows (round-2 re-run on the delivered fixture `82668512…`) |
| Provider-free goals stay provider-free (`provider_scope: null`, `allowed_actions: []`, no `provider_access`) | writer probe row `codex_containment` with a provider scope → exit 1 (§4.2); the key test's provider-pair equality; the e2e reader mutant M9; the refusal table's `provider_free_*` rows (`TestLinuxV2AdmissionProviderScopeRefusalRows`, §4.3) |
| No weakened provider-access assertion, clone reserve, or Subject binding | e2e row tests `TestLinuxV2WitnessRequiresGoalAdmissionRows`, `TestLinuxV2AdmissionAllowsProviderSyncRows`, `TestLinuxV2AdmissionProviderScopeRefusalRows` and the dispatch mutants M11/M12 (§4.3, §6, §12.4); the clone reserve and the witness reader are declared unchanged by this goal (§1) |
| No new credentials in CI | register-script payload delta (§4.2): exactly one runtime key changed, no credential field |
| Key agreement stays green | §5, first row (exit 0) |
| Comments at most 2 lines; Elixir changes run format, compile, Credo strict, relevant tests | comment-cap (§5); the §4.4 formatter probe; §5's Credo ratchet row; §12.2's compile-and-run probe |
| Acceptance: no clone-intent/Subject stop; next stop verbatim or green | the conductor's Bind303 row (§7, §13) — BINDING: whole-log refusal count 0, next stop the authentic Pi turn; the conductor re-Binds on the merged head |

## 12. Appendix — scratch probes and mutants (verbatim, full sha256)

The probes below ran only from the stage's scratch directory and were never committed; each
exercised the delivered bytes of §1. Each sha256 binds its probe's exact file bytes (LF). The fix
stage re-ran §12.1 on the final bytes and added no probe; its mutant patches are §12.4.

### 12.1 M9 pin — `zz_apply_probe_test.go` (sha256 `f4e499d87c6071bdced3e5103233f02c98e5d72307790e542fa97b002dbf2ace`, 110 lines)

Ran at the apply stage in a scratch clone of the delivered bytes as `daemon/e2e/zz_apply_probe_test.go`
(`//go:build linux && amd64`): `GOTOOLCHAIN=auto go test ./e2e/ -run
TestProbeProviderFreeAdmissionCarriesProviderScopeRefusal -count=1 -v` → `--- PASS`, `ok
github.com/wxxb789/symmetry/daemon/e2e 0.325s`, and the log line `mutant refused verbatim:
provider-free owner-witness admission carries a provider scope`. The provider-free control request
(`provider_scope: null`) returned `nil` from the whole reader. Re-ran at the fix stage against the
final e2e bytes through a `go test -overlay` added-file mapping for that package path (repo
untouched; the probe creates its own root-owned barrier directory): same command → `--- PASS`,
`ok … 0.228s`, same log line.

```go
//go:build linux && amd64

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

// Scratch probe (never committed): the D1 mutant. A provider-free goal row's request whose
// admission carries a fixture-side provider scope must be refused by the reader's provider-free
// branch; the same request with provider_scope null must pass the whole reader.
func TestProbeProviderFreeAdmissionCarriesProviderScopeRefusal(t *testing.T) {
	const (
		resourceID = "22222222-2222-4222-8222-222222222222"
		commit     = "0123456789abcdef0123456789abcdef01234567"
		treeDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	)
	barrierDirectory := "/opt/symmetry-owner-test/apply-probe-barrier"
	if err := os.MkdirAll(barrierDirectory, 0o700); err != nil {
		t.Fatalf("create probe barrier directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(barrierDirectory) })
	if err := os.Chmod(barrierDirectory, 0o700); err != nil {
		t.Fatalf("chmod probe barrier directory: %v", err)
	}
	identity, err := os.Lstat(barrierDirectory)
	if err != nil {
		t.Fatalf("stat probe barrier directory: %v", err)
	}
	stat, ok := identity.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		t.Fatalf("probe barrier directory must be root-owned")
	}

	testCase := linuxV2NativeWitnessCase{harness: "pi", requestCase: "stage_replay", barrierStage: "helper_bound"}
	value := config.Config{Runtime: config.Runtime{
		RepositoryResourceID: resourceID,
		AgentProfile:         "pi-native",
		Workspace:            "native",
	}}
	owner := linuxgate.OwnerConfig{AUID: 0, AGID: 0}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)

	request := func(scope string) string {
		admission := fmt.Sprintf(`{
  "schema_version": "symmetry.admission.v1",
  "admission_id": "55555555-5555-4555-8555-555555555555",
  "goal_id": "11111111-1111-4111-8111-111111111111",
  "goal_revision": 1,
  "work_item_id": "33333333-3333-4333-8333-333333333333",
  "purpose": "implement",
  "context_snapshot_id": "44444444-4444-4444-8444-444444444444",
  "context_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "model_profile": "implementation-default",
  "session_mode": "fresh",
  "requested_session_id": null,
  "subject": {"resource_id": %q, "commit": %q, "tree_digest": %q},
  "limits": {"max_turns": 1, "deadline_at": %q, "max_cost_microusd": "250000"},
  "validation_of_task_id": null,
  "provider_scope": %s
}`, resourceID, commit, treeDigest, deadline, scope)
		return fmt.Sprintf(`{
  "version": 1,
  "case": "stage_replay",
  "harness": "pi",
  "run_key": {"run_id": "apply-probe-run", "generation": 1},
  "fence": {"runtime_id": "apply-probe-runtime", "runtime_epoch": 1, "generation": 1, "claim_id": "apply-probe-claim", "lease_token": "apply-probe-token"},
  "claim": {"run_id": "apply-probe-run", "task_id": "apply-probe-task", "generation": 1,
    "claim_id": "apply-probe-claim", "lease_token": "apply-probe-token",
    "lease_expires_at": %q, "lease_remaining_ms": 3600000,
    "work": {"goal": "apply probe", "agent_profile": "pi-native", "workspace": "native", "input": %s, "required_capabilities": {}}},
  "subject": {"resource_id": %q, "commit": %q, "tree_digest": %q},
  "barrier": {"directory": %q, "timeout_seconds": 30, "hold_after": "helper_bound", "release_file": %q}
}`, deadline, admission, resourceID, commit, treeDigest, barrierDirectory, filepath.Join(barrierDirectory, "release"))
	}

	read := func(body string) error {
		path := filepath.Join(t.TempDir(), "request.json")
		if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
			t.Fatalf("write probe request: %v", err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatalf("chmod probe request: %v", err)
		}
		_, err := linuxV2ReadOwnerWitnessRequest(path, testCase, value, owner)
		return err
	}

	scope := fmt.Sprintf(`{"resource_ids": [%q], "operations_by_resource": {%q: ["resource.sync"]}, "change_target": null}`, resourceID, resourceID)
	if err := read(request("null")); err != nil {
		t.Fatalf("provider-free control request must pass the reader, got: %v", err)
	}
	err = read(request(scope))
	if err == nil {
		t.Fatal("provider-free request with a fixture-side provider scope must be refused")
	}
	if !strings.Contains(err.Error(), "provider-free owner-witness admission carries a provider scope") {
		t.Fatalf("refusal = %v, want the provider-free scope refusal", err)
	}
	t.Logf("mutant refused verbatim: %v", err)
}
```

### 12.2 F3 probe — `p1-apply-probe.exs` (sha256 `fbe92aa50b22369b72b728662e8ed4276fb77cebc08424dd982b67893c77268f`, 36 lines)

Ran from the main checkout's `control/` project (`MIX_ENV=prod`, `DATABASE_URL` naming the ephemeral
`symmetry_linux_v2_pi_apply_probe` on 127.0.0.1:5432, `GITHUB_ACTIONS=true`/`GITHUB_RUN_ID=local`);
it evaluates only the delivered fixture file's module definition (the file's own `System.argv`
dispatch is cut) and runs `run!("prepare", …)` on the delivered bytes. Result: exit 0,
`work_items=2 goal_cases=46`, legacy profiles `["opencode-native", "opencode-native"]`,
`P1-APPLY-PROBE OK` — the changed fn pattern matched the `:opencode` legacy rows and the fixture's
`@expected_fixture_runs` check (48) held; a violated role invariant would raise
`FunctionClauseError` (previously `MatchError`) instead.

```elixir
# Scratch probe (never committed): run the delivered fixture's `prepare` on an ephemeral local
# database, exercising the legacy (:opencode) branch with the delivered fn pattern, and assert
# the written fixture holds the expected case counts.
path = System.argv() |> Enum.at(0) || raise "fixture path required"
output_path = System.argv() |> Enum.at(1) || raise "output path required"

source = File.read!(path)
parts = String.split(source, "\ncase System.argv() do", parts: 2)
if length(parts) != 2, do: raise "fixture no longer ends in its System.argv dispatch"
[head, _tail] = parts

Code.eval_string(head, [], file: path)

LinuxV2ControlClaimFixture.run!("prepare", [
  output_path,
  "wxxb789/symmetry",
  "wxxb789",
  "0123456789abcdef0123456789abcdef01234567",
  "sha256:0000000000000000000000000000000000000000000000000000000000000000"
])

fixture = File.read!(output_path) |> Jason.decode!()
work_items = fixture["work_items"]
goal_cases = fixture["goal_cases"]
IO.puts("work_items=#{map_size(work_items)} goal_cases=#{map_size(goal_cases)}")
unless map_size(work_items) == 2 and map_size(goal_cases) == 46 do
  raise "unexpected fixture case counts"
end

legacy_profiles = work_items |> Map.values() |> Enum.map(& &1["agent_profile"]) |> Enum.sort()
IO.puts("legacy_profiles=#{inspect(legacy_profiles)}")
unless legacy_profiles == ["opencode-native", "opencode-native"] do
  raise "legacy branch did not route the :opencode rows"
end

IO.puts("P1-APPLY-PROBE OK")
```

### 12.3 M10 reproduction

The four M10 refusals are produced by the delivered test's own
`test_allowed_actions_policy_drift_is_refused` subtests (`binding`, `execution`, `authority`,
`outside`): the committed refusal table is the pin, so no scratch artifact is carried for M10.

### 12.4 Fix-stage mutant patches (verbatim, full sha256)

Each post-fix patch applies to the delivered bytes (`b4141da9…`); the baseline row applies to the
pre-fix bytes (`823681b3…`, the `dadb474d` blob). All five are change-line-only hunks (no context)
executed through a `go test -overlay` replacement mapping for that package path; the mutated bytes
were left only in scratch. The round-2 comment deletion and field alignment sit after every
post-fix patch anchor, so the four post-fix patches' change lines and line numbers are unchanged in
the round-2 bytes (the mutated-file hashes below are the fix-stage values); the X1/X2 overlays were
re-run on the merged bytes (§6). M11/M12 redden the new refusal table
(`TestLinuxV2AdmissionProviderScopeRefusalRows`); the baseline patch reproduces the pre-fix REV-A02
gap on the pre-fix bytes (whole `daemon/e2e` package green); M7/M8 re-bind the earlier rows to the
final bytes. The CI job/user that reddens them is in §6.

| Mutant | Patch sha256 | Mutated file sha256 |
| --- | --- | --- |
| baseline (pre-fix arm swap) | `06f9fa52821ef5cef15818388dafbc4f9c8c7b87e146ff646c86756d5bc01486` | `2edbdb9c02ca198a50183fa54a824a2e22a2c8fbe2b7e63b1352a2bd6cf571cb` |
| M7 | `5da65b176a2b902389fd91fd5a69d389df88243aff20814c706537aa0135e7b4` | `e4051d74a81b7ac45f82a1aa313e932c88b90c4b39362c365b6148190f5bc4e1` |
| M8 | `aa50e4674b5512d4165da037b90829eaa7f25502fbf663c11748caf2d265e4c2` | `b45110bde0454c9a0836075218ea28ac863f5012fa7b7989e954456147f1da6f` |
| M11 | `5ee5c625fef340491a57076cbe29ab729744cf394400f4dae1ae7c1d454e9f5e` | `27bbfbcdcb0dd55fc99064f873b7f58779780594f103a36b0d827d63a32bc53a` |
| M12 | `d23efef331d27d797d8d37f5a1752327472fb8cd85397ff4a2d547f33ed189dd` | `df7f6a0ab68a3d5ee8ba3ad3fc857ee19a65cc34696e8184d6fa593c387892ef` |

```diff
--- a/daemon/e2e/production_linux_v2_native_witness_test.go
+++ b/daemon/e2e/production_linux_v2_native_witness_test.go
@@ -1687 +1687 @@
-		if testCase.providerActionRequired {
+		if !testCase.providerActionRequired {
```

```diff
--- a/daemon/e2e/production_linux_v2_native_witness_test.go
+++ b/daemon/e2e/production_linux_v2_native_witness_test.go
@@ -1785 +1784,0 @@
-	{harness: "pi", requestCase: "helper_sigkill_detached_child"}:                 true,
```

```diff
--- a/daemon/e2e/production_linux_v2_native_witness_test.go
+++ b/daemon/e2e/production_linux_v2_native_witness_test.go
@@ -1870,0 +1871 @@
+	return scope != nil
```

```diff
--- a/daemon/e2e/production_linux_v2_native_witness_test.go
+++ b/daemon/e2e/production_linux_v2_native_witness_test.go
@@ -1880 +1880 @@
-	if providerActionRequired {
+	if !providerActionRequired {
```

```diff
--- a/daemon/e2e/production_linux_v2_native_witness_test.go
+++ b/daemon/e2e/production_linux_v2_native_witness_test.go
@@ -1886,3 +1886 @@
-	if scope != nil {
-		return errors.New("provider-free owner-witness admission carries a provider scope")
-	}
+	_ = scope
```

### 12.5 Line-anchor re-measurement at the fix stage

Every anchor in this record and the plan was re-measured at the fix stage against the revision it
names: the delivered changed files by their §1 hashes; the byte-identical files by the hashes below
(`sha256sum` on the worktree); the plan's dispatch-head anchors re-read from `b27ea691` with
`git show … | sed -n`.

| Anchor(s) | Measured file sha256 | Verified content |
| --- | --- | --- |
| e2e `production_linux_v2_native_witness_test.go:1682-1690` | `b4141da9…` | the goal-admission branch and the dispatch call |
| `control/lib/symmetry_control/goals/admission.ex:1218-1268`, `:1304-1307`, `:333-335` | `24ee858e96e739c0f54e534269fc79df93830d0ef0e8e8a486b81853ddd02770` | `admission_provider_scope!`; `provider_operation_allowed?` (both policies); `require_provider_access` |
| `control/lib/symmetry_control/orchestration/assignment.ex:322-341`, `:505-530` | `4b8c0a3f292177b06748fb29a22d97fb5ee63a50ca40ff6f299f2efa27048a3d` | the `@> required_capabilities` and `@> adapter_requirement` query; `goal_runtime_matches?` |
| `daemon/internal/state/linux_clone_journal.go:864-878` | `5a8455b80ad1b2f53678fab1904056f6739357aa984fce8105340f98a2018ede` | `requireLinuxCloneJournalFence` and its admitted-Subject match |
| `daemon/internal/app/linux_owner_witness_linux.go:2616-2652`, `:3340-3362`, `:4257-4290` | `1fd4c17f3f54d6b616097de1eb1832ee6d2b04ec67434e5e48fb607688ae0ce3` | the case-name provider keying; `prepareClone`; `seedAuthenticClaim` |
| plan §1 dispatch-head anchors: e2e `:1682-1690`, `:1780-1793`, `:1796-1822`; writer `:29-38`, `:199-217`; fixture `:41-140`, `:152-154`, `:186-256`, `:447-570`, `:603-610`; register `:129-160`; key test `:27-30`, `:189-215` | re-read from `b27ea691` | each range starts at the named construct (registered as dispatch-head readings) |
| round-2 merged head, e2e `:1695-1705` (the admission branch), `:1700-1702` (the dispatch call), `:1883`/`:1892` (the predicate pair), `:1952` (the refusal row test) | `d988ca3b…` | re-measured at round 2 on the merged bytes; the #301 merge shifted the fix-stage numbers by +13, and the fix-stage rows above keep their own revision |
| round-2 merged head, `daemon/internal/app/linux_owner_witness_linux.go:2563` (`validateLinuxV2NativeWitnessRequest`), `:2622-2626` (the case-name provider keying), `:3482` (`prepareClone`), `:4399` (`seedAuthenticClaim`) | `3bf974a170e1fe237c2eebe1428719fac4467705c7c1dfe7ac9d3e1945006dcd` | re-measured at round 2; the file carries #301's bytes |

### 12.6 Round-2 probe artifacts (scratch, deleted after the round)

The U1 re-run reconstructed the implement-stage probes byte-exact by replaying the implement run's
recorded write/edit calls (no file copy survived; all three recovered files match their §4.5
hashes), then retargeted only scratch constants:

| Artifact | sha256 | Delta from the implement stage |
| --- | --- | --- |
| `p1-probe.exs` (recovered) | `4165772bf58d74e80a9c40c5a91057188d594b3ec9e80966bab2c69d8ad54a7f` | = §4.5's implement-stage probe |
| `p1-probe.exs` (as run) | `fa2f505b31f664bc928c4fcf8edcd646b1a3a994fb1fa554f70241004b056184` | one line: the `@scratch` constant |
| `run-p1.sh` (recovered) | `55833e436fa468c6c51862d32ac546ad9c7c74d1949b2cd3c737e5bf37036369` | = §4.5's run script |
| `run-p1.sh` (as run) | `b0a25ed27323bed4ad93ac6e9b934fb57d6ab8846d23a2034de6ecaf6194c25b` | the `DATABASE_URL` database name and the scratch paths |
| `p2-probe.py` (recovered, as run) | `e5a12121fbf542e9e5d13f93d24bcbe3c5846154448d961ed0c7b8a269e7e32d` | = §4.5's stub probe |
| `p1-report.json` (the U1 run) | `32ae6c74441028975a10eee68679d85533f052b1a759d40660fa752587947516` | the U1 result surface |
| `p1-run.log` (the U1 run) | `60ba9d0d0a32281be5b951d0001bfd19627f6a91620854d25c6bb90470f6f084` | the U1 console log |
| `x1.go` / `x2.go` overlays of the merged bytes | `5ff17968bb2e94aef5b5113b62e1817c68f68fa507532f3f2cce7fa7e52fbd2b` / `aa280207e1359178b3a5b74f9b9c6981d721f41bf73dc93cbe70ac7f6b4807b1` | the X1/X2 mutants (§6) |

## 13. Bind — scratch run `45811007804` on `22d6c411` (case set `shared`) — BINDING

The §7 acceptance clause executed on the exact PR-head bytes
(`22d6c41167ebfb76ce474ed038c6e753ba72b85e`) plus the two scratch-only files, pushed once as
`19d8b81548ffa319c26dfaf97c80b8a3e48f1a02` (parent = the PR head; diff = the two files:
`.github/workflows/scratch-rf-acc-containment-goal-admission.yml` sha256
`bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`, `scripts/ci/scratch-gh-shim`
sha256 `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`; the only delta vs the
`b0ba2694`-recovered copy is `case_sets=(codex_containment claude_containment)` →
`case_sets=(shared)`). Run `45811007804` (workflow `Scratch containment goal-admission probe`,
attempt 1, created 11:29:12Z, conclusion `failure`), `total_count=1` at every list read (creation
11:29:12Z, pre-deletion 11:35:39Z, post-deletion 11:35:44Z, final 11:38:10Z; the settlement state
read from the run object); branch deleted, scratch worktree removed, PR refs untouched.

This row is the contract's BINDING acceptance (§7 and the conductor's PR #303 round-1 ruling): the
whole log contains 0 occurrences of the recorded refusal. The round-2 delta since is comment-only
in the three code files (§1) plus the `origin/main` `b0206393` merge; the conductor re-Binds
`shared` on the merged head, whose gate rows are §5.1.

- **The recorded stop is gone.** `Linux clone intent does not match the current admitted Subject`
  and `clone intent` occur **0** times in the whole 3,552-line log, together with 0 of
  `reserve owner witness clone before source Git effects`, `pre-existing RunJournal`,
  `is not physically released`, `may still own or require recovery` and every earlier stop class.
  The clone reserve now admits: the retained journal projects
  `{"local_state":"claimed","has_claim_grant":true,"has_clone_intent":true,"has_seed_receipt":true,"recovery_required":true}`
  (raw 3121; run 300 read `false/false/false` at the refusal), and the exported member records
  `clone.checkout_ready: true`.
- **First failing line** (gate raw 3111: `Go native acceptance event gate failed:
  bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0, skipped=[], fail_events=2`;
  raw 3114): `production_linux_v2_native_witness_test.go:91: wait for isolated production
  owner-witness command: hidden owner-witness command exited unsuccessfully: exit status 1`,
  with the owner-witness stderr `-linux-v2-native-owner-witness: wait for authentic Pi owner
  witness turn: pi rpc final assistant message did not stop normally: "error"` (raw 3119; the
  daemon's `ErrUnexpectedAssistantStop`, `daemon/internal/harness/pi/rpc.go:53`) and
  `--- FAIL: TestProductionLinuxV2PiNativeProviderAction (5.31s)`. The scratch workflow supplies
  dummy credentials (`SYMMETRY_OPENAI_API_KEY: sk-scratch-dummy`) where the acceptance script
  requires secret-backed ones (`:19` `: "${SYMMETRY_OPENAI_API_KEY:?…}"`, `:57` the `gho_*` check)
  and writes that key into the B-only native credential file (`:393`) the pi turn authenticates
  with.
- **All 21 verified rows that ran are green** (`run=1 pass=1 skip=0`): the unprovisioned refusal,
  the seven AppArmor rows, the legacy pi/OpenCode refusals, the B-caller / cross-mount /
  cgroup-procs-fd rows, the four `InstalledLinuxOwner*Scratch` rows and the four hostile-marker
  rows (raw 2495, 2505-2511, 2519-2531). The pi leg has no verified row.
- **`stop_readback_reader` did not run** — the abort is inside the `case` block at the pi leg
  (`:3160`), before its single call site (`:3226`): 0 `readback-reader:` rows, 0 `did not stop
  cleanly`, only the start row `root readback reader ready:
  unit=symmetry-ci-readback-reader.service MainPID=7245` (raw 2518). Its Result row is unmeasured,
  not failed (had it run, the row it prints is the reader's own exit line
  `readback-reader: stop signal requests=N`; the block asserts `ActiveState=inactive`,
  `Result=success`, `ExecMainStatus=0` and the removed ready file).
- **Export/manifest:** `redacted evidence export failed: pi durability witness is unavailable:
  ack-loss-replay-checkout_ready.json` (raw 3401); **no manifest**; `Case set shared exited 1`
  and `Failed case sets: shared` (raw 3404-3405).
- **Artifacts:** `linux-v2-native-evidence-45811007804-1` (id `15114046167`, 2011 B, sha256
  `cdc9a7e4dbd59c78aeb64207d8f045ed89150f68f1f9ed0608a81116f8d8c842`), members
  `shared/pi-native-provider-action.json` (3224 B, `2cc6ec1482f29bc53d34bc810e266fa9099afb7465dafa8f59a6124473728ebb`;
  `subject.commit` = the run head, `installed_owner_verified: true`, `clone.checkout_ready: true`,
  `provider_action_succeeded: false`) and `shared/unprovisioned-owner-refusal.json` (691 B,
  `c0bc11244999b3ba3337c3986128b7ba140a7b304856d678b5eae7c5ca221033`, `owner_witness_outcome:
  unprovisioned_refused`); `linux-v2-host-prerequisite-45811007804-1` (id `15113830851`, 675 B,
  sha256 `da9ef4a8f6537a5c1b6373a39c7c774fb39321ce3601e57f05705dd065a5ec81`, member
  `a26fd7b8736738a81f9899d3533bc03d4beebe673c492f0c5f868a4f70c8db50`, `status: passed`,
  `source_commit` = the run head).

Run-input sha256 equality (run head `19d8b81548` vs PR head `22d6c411`, `git cat-file -p … |
sha256sum` at both revisions; the pushed commit's parent is exactly the PR head and
`git diff --name-status 22d6c411 19d8b81548` is exactly the two scratch-only additions, so every
other tree file is byte-identical by construction):

| File | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `b4141da9face795573dc1d3d5619625197743575cf6f65e462fb372e8cd79d08` | same | yes |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` | same | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` | same | yes |
| `scripts/ci/linux-v2-source-subject.py` | `27fed36a71d46129ce2b713d55b6615573408a37c0dc8cceb9e5918dcaacd452` | same | yes |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `bf53e78fa7fee74d98988c1a8c13cc910930aa08081b1adadd839973d09dd5c1` | same | yes |
| `scripts/ci/linux-v2-host-prerequisite-probe.sh` | `b8b91f407dd1eb4f79c3a27ef03bb1c5ff2dad7ff5cc6911a1e1d2d7678e9fad` | same | yes |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` | same | yes |
| `scripts/ci/assert-go-test-json.py` | `bf4eac89a9d0fc89e380a8a579d9d4b7acb7e5c79bf49ca1369305baefe61582` | same | yes |
| `scripts/ci/linux-v2-native-tools.sh` | `500ac7b47dd9a9b0f5aea2d67dafe4a0131dfd10726b0dc4b0215f4f1b410c69` | same | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The run executed the `22d6c411` PR-head bytes listed above (e2e file `b4141da9…`, key test
`09ea677b…`, register `25b04fbc…`, fixture `82668512…`); the round-2 comment-only edits and the
gofmt alignment change no executed byte's behaviour, and every round-2/merged re-run is in §5.1.

Log: `gh run view 45811007804 --repo wxxb789/symmetry --log`, 3,552 lines, sha256
`264d792732a4feeec20c34987822754f76fe8e16d58db00303d79bcae4201970`; watched once
(`gh run watch 45811007804 --interval 60 --exit-status`, single invocation).

## 14. Bind 2 — scratch run `45817758808` on `5ae35230` (case set `shared`) — BINDING

Bind 2 is the BINDING run on `5ae35230` and supersedes §13, which judged the replaced
`22d6c411`. The binding head is `5ae35230a26a0caa1659941ea5207854a1a1ddc3` (round-1 `104c5fe9`
merged with `origin/main` `b0206393`), executed as the PR head plus the two scratch-only files in
one push: commit `0d503ebc72450436041341dbea352d20d53c1f9f`, parent exactly the PR head and diff
exactly those two files, whose bytes repeat the ratified pair recovered from `b0ba2694` (workflow
sha256 `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`, shim
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`). Run `45817758808`
(workflow `Scratch containment goal-admission probe`, attempt 1, created 12:14:11Z, settled
`completed`/`failure` 12:20:10Z; the only failing step is `Run isolated A/B Linux v2 acceptance
case sets`) was listed `total_count=1` at the creation read (12:14:24Z), before the branch
deletion (12:20:51Z), after it (12:21:27Z) and at the final read (12:21:29Z); the branch was
deleted on the first attempt, the temporary worktree removed, and `refs/pull/303/head` read
`5ae35230…` at every check.

- **The refusal the clause names stays absent.** `Linux clone intent does not match the current
  admitted Subject` and the substring `clone intent` occur **0** times in the full 3,549-line log
  (sha256 `1ba29255466b34603354e0c00aafcd93d2e3cf8c25b6eb749b15b1bf211c6479`); so do
  `reserve owner witness clone before source Git effects`, `pre-existing RunJournal`,
  `is not physically released` and `may still own or require recovery`. The retained journal again
  projects `{"local_state":"claimed","has_claim_grant":true,"has_clone_intent":true,"has_seed_receipt":true,"recovery_required":true}`
  (raw 3118), and the exported member carries `clone.checkout_ready: true`.
- **First failing line** (raw 3111, 12:19:59.7649572Z): `production_linux_v2_native_witness_test.go:91:
  wait for isolated production owner-witness command: hidden owner-witness command exited
  unsuccessfully: exit status 1` — the same stage §13 reached. Its stderr (raw 3116) reads
  `-linux-v2-native-owner-witness: wait for authentic Pi owner witness turn: pi rpc final
  assistant message did not stop normally: "error"`, and the test closes with
  `--- FAIL: TestProductionLinuxV2PiNativeProviderAction (5.46s)` (raw 3113); the gate line
  (raw 3108) is `Go native acceptance event gate failed:
  bad=TestProductionLinuxV2PiNativeProviderAction=run:1,pass:0,skip:0, skipped=[], fail_events=2,
  invalid_json_lines=0`.
- **Verified rows**: all 21 that ran are green (`run=1 pass=1 skip=0`), the same set as §13
  (raw 2492, 2502-2508, 2516-2528), timestamped 12:19:18–12:19:51Z; the pi leg and every later leg
  contribute none.
- **`stop_readback_reader` did not run**: the dispatch aborted inside the `case` block at the pi
  leg, before the stop call site `:3226` — 0 lines containing `readback-reader:`, 0
  `stop_readback_reader`; only the start proof, `root readback reader ready:
  unit=symmetry-ci-readback-reader.service MainPID=7199` (raw 2515). Unmeasured, not failed.
- **Export/manifest**: `redacted evidence export failed: pi durability witness is unavailable:
  ack-loss-replay-checkout_ready.json` (raw 3398); no manifest shipped; `Failed case sets: shared`
  and `Case set shared exited 1` (raw 3401-3402).
- **Artifacts**: `linux-v2-native-evidence-45817758808-1` (id `15117050850`, 2010 B, zip sha256
  `3e790df0650942ea635d71620e1428780508c5890508ace547e3d055589fb583`) holds
  `shared/pi-native-provider-action.json` (3223 B,
  `de57743a5ecf1c3d407632ee29ac0b40234937e59a9dc25f13dd6038a7923a20`; `subject.commit` = the run
  head, `installed_owner_verified: true`, `provider_action_succeeded: false`) and
  `shared/unprovisioned-owner-refusal.json` (691 B,
  `a46ab494c35862406e878c71d3e9658eed418ee69ec78bf20e965f59cd4b0f3b`, `owner_witness_outcome:
  unprovisioned_refused`); `linux-v2-host-prerequisite-45817758808-1` (id `15117195010`, 675 B,
  zip sha256 `e1d2289759c076ecb0fbcc113645c13abea9a67628b7078d3ed943e016b1caa8`, member
  `2f87f0537a3e664c9b3debb9bf52a131de7f3240322f54412986a9e40df13984`, `status: passed`,
  `source_commit` = the run head).

Changed rows only, against §13's table; the other nine of its eleven rows are byte-identical (the
seven scripts `linux-v2-native-acceptance.sh`, `linux-v2-source-subject.py`,
`export-linux-v2-redacted-evidence.py`, `linux-v2-host-prerequisite-probe.sh`,
`linux-v2-native-rootfs-base.sh`, `assert-go-test-json.py`, `linux-v2-native-tools.sh`, plus the
two scratch-only files repeating the pair above). Rows are hashed at the PR head, equal to the run
head modulo the two scratch-only files.

| File | §13 row (`22d6c411`) | Bind 2 row (`5ae35230`) |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `b4141da9face795573dc1d3d5619625197743575cf6f65e462fb372e8cd79d08` | `d988ca3bf8a0364f7d2f1c9aabe4cef14b53216e501a6002c49950e18d508abf` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` | `1bde11fbcfda1e7175a07f4c8407d69435658426e135b52c9bcc12d350a235f4` |

Two further run inputs changed where §13 had no row:
`daemon/internal/app/linux_owner_witness_linux.go`
(`1fd4c17f3f54d6b616097de1eb1832ee6d2b04ec67434e5e48fb607688ae0ce3` →
`3bf974a170e1fe237c2eebe1428719fac4467705c7c1dfe7ac9d3e1945006dcd`; compiled into the
`symmetry-daemon` binary whose hidden `-linux-v2-native-owner-witness` command the witness starts)
and `scripts/ci/register-linux-v2-claim-machine.py`
(`25b04fbcbe37cc9d678b7c07aa98690bd5cd276f4e08e1c89553eace166e3087` →
`f6029087c0acf8c42478018f0f9672fdf7a914a715d4e49d0eb3c3ee128ad773`; invoked by the acceptance
script at `:1534`).

## 15. Bind 3 — BINDING on `2627473c`

[Run `45820200923`](https://github.com/wxxb789/symmetry/actions/runs/45820200923) (`shared`, scratch
commit `7242480f`) supersedes §14. Clone-intent refusal count: 0. Pi reached the same RPC stop as
§14; the run failed, and the partial export has no manifest.

Zip SHA-256: native evidence `28ceccf27839c9eb4ad0fb4488bcd856848d8e04efbad1a2206828c667bc3d29`;
host prerequisite `7e487d575cb2b89acc77ef0de7c099f88b34282cd5e3ef57765cbb42ef631031`.

SHA-256 delta from Bind 2: all 13 previously recorded inputs are unchanged. #302 changed these
additional inputs compiled into the same e2e test binary:

| File | Bind 2 (`5ae35230`) | Bind 3 (`2627473c`) |
| --- | --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` |
| `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go` | Absent | `e9e95d049df3dab8e6f5a78a7d28fd2fbed04cef1291994fb67bc6d4f5d194d9` |
