# rf-acc-containment-goal-admission — implementation plan (Control-composed Goal admission for the four containment cases)

Contract: `goal-rf-acc-containment-goal-admission.md` (GDD state root, external to the repository);
conductor ruling `refactor/decision-witness-admission-2026-11-25.md` and both consults
`findings/admission-contradiction-consult-{sol,ds}.md`. Dispatch head `4440c5c0`
(#290 `rf-acc-owner-loss-semantics`, the gate-dispatch PR; #285/#287/#289 are the same-file
predecessors the contract cites at `6b2d4f8f`). Branch `refactor/rf-acc-containment-goal-admission`.

The accepted plan is the contract; this file resolves only the implementation choices it leaves
open, the clause census at the dispatch head, the exact edge-case semantics, the evidence plan, and
what is out of scope.

## 1. Clause census at the dispatch head (re-located by content)

| Contract clause | Dispatch head (`4440c5c0`) | Disposition |
| --- | --- | --- |
| Change 1 fixture routing | `scripts/ci/provision-linux-v2-control-claims.exs`: `@cases` rows, split `Enum.split_with(... repository_role != :pi_durability)`, `goal_case_specs`, route `Map.fetch!(fixture["goal_cases"], …)`, `runtime_adapter_binding?/2` | implement here (§2.1, §3.1-3.3) |
| Change 2 runtimes | `scripts/ci/register-linux-v2-claim-machine.py`: `owner-witness-control-codex-generic` / `-claude-generic` adapter-free; the pi-durability spec at `:103-137` is the exact shape to mirror | implement here (§2.2) |
| Change 3 writer | `scripts/ci/write-linux-v2-owner-request.py`: `if profile_name == "pi-durability":` admission branch; legacy repository check in the `else` | implement here (§2.3) |
| Change 4 e2e | `daemon/e2e/production_linux_v2_native_witness_test.go`: `if testCase.harness == "pi" && !testCase.providerActionRequired { … "pi durability owner-witness claim must carry the Control admission for the exact Subject" } else if err != nil \|\| admitted { "owner-witness claim must be a real non-Goal Control task" }` (#290 moved the block to `:1646-1653`; byte-content unchanged from `6b2d4f8f :1562-1569`) | implement here (§2.4) |
| Change 5 records | plan/evidence do not exist yet; `docs/TODO.md` clone-admission entry at the `## rf-acc-native-burndown` section | this plan, `docs/evidence/rf-acc-containment-goal-admission-2026-11-25.md`, TODO/CLOSED-TODO |
| Owned set | four code files + plan + evidence + TODO/CLOSED-TODO + scratch branch; `linux_clone_journal.go` / `linux_owner_witness_linux.go` byte-identical; `linux-v2-native-acceptance.sh` needs no change (the two sets already schedule containment + `stage_replay_native_release_<kind>`, `:3042-3048`) | §2, §3 |
| Gates | base census run `45661224561` (scratch workflow, both sets, dispatch head); recorded pre-fix reds `45452357027`/`45455325059`/`45458181149` | §5 |

## 2. Public/API shape (choices the contract leaves open)

1. **Fixture routing flag.** The four containment rows get a distinct repository role
   `:opencode_goal` (they already bind the OpenCode repository resource). The split predicate
   becomes `repository_role in [:pi_durability, :opencode_goal]` → goal cases; every legacy case
   (including `:pi_provider`, `:opencode`, unprovisioned) keeps `launch_one_shot_work_item!`.
   `goal_case_specs` binds `:pi_durability` → `pi_durability_repository.id` and `:opencode_goal`
   → `opencode_repository.id`. `@expected_fixture_runs` stays 48
   (43 goal: 39 pi-durability + 4 containment; 5 legacy).
2. **Adapter assertion.** `runtime_adapter_binding?/2` becomes `runtime_adapter_binding?/3`
   (`runtime, repository_role, harness`): the `:pi_durability` arm stays
   `["adapter", "kind"] == "pi"`; the `:opencode_goal` arm is
   `["adapter", "kind"] == Atom.to_string(harness)` — the pinned wire strings are `codex` and
   `claude_code` (registry.ex `@native_harness_kinds`); every other role keeps the adapter-free
   arm. The schedule shape check (`:252-257`) is untouched.
3. **Writer key.** A module-level set `GOAL_ADMISSION_CASES = {"stage_replay",
   "codex_containment", "claude_containment", "stage_replay_native_release_codex",
   "stage_replay_native_release_claude"}` replaces `profile_name == "pi-durability"` as the
   admission branch key. `stage_replay_unprovisioned_refusal` stays out (legacy refusal target).
   The branch's three checks are unchanged; the legacy repository check stays for every other case.
4. **e2e predicate.** One extracted function
   `linuxV2WitnessRequiresGoalAdmission(testCase linuxV2NativeWitnessCase) bool` keys on an
   explicit map of the goal-routed (`harness`, `request_case`) pairs: `pi`+`stage_replay`,
   `codex`/`claude_code`+`stage_replay` (the native-release kind legs), `codex`+
   `codex_containment`, `claude_code`+`claude_containment`. Rows outside the map — the
   opencode/generic/legacy carve-out and any new provider-free codex/claude row — take the
   non-Goal refusal, so extending the goal route to a new case is an explicit edit (review fix
   r1; §9). The branch calls the predicate; the refusal message becomes
   `fmt.Errorf("%s owner-witness claim must carry the Control admission for the exact Subject",
   testCase.harness)` (the F04 pattern beside it names the harness; the old text said
   `pi durability`).
5. **Table probe target.** The predicate region (map, key type and function) is pure and
   text-extractable by
   `awk '/^\/\/ Goal-routed request cases/{f=1} f{print} f && /^func linuxV2WitnessRequiresGoalAdmission/{g=1} g && /^}$/{exit}'`,
   so P3 pins the in-scope set with a generated `package probe` file, exactly like the pi
   campaign's extracted-function probe.

## 3. Exact edge-case semantics the contract demands

1. **Pi stays identical.** The twelve pi rows of this predicate (`stage_replay` + the eleven
   pi-durability replay/ack-loss rows, all `harness == "pi"` and provider-free) keep exactly the
   admission acceptance; the pi provider/crash rows (`pi_provider_action`,
   `helper_sigkill_detached_child`, `daemon_sigkill_helper_survives_detached_child`) keep the
   legacy `else` arm as today.
2. **The carve-out is deliberate.** `opencode_native_health`, `stage_replay_unprovisioned_refusal`
   (fixture case with harness `opencode`) and `wsl2_denial` keep the `err != nil || admitted`
   refusal; M5's over-broad mutant must redden P3's negative rows.
3. **Provider scope / capabilities unchanged.** The fixture's goals set `allowed_actions: []`
   (`provision-linux-v2-control-claims.exs` goal attrs), so Control composes `provider_scope: null`
   and no `provider_access` (`admission.ex:1218-1262,:1304-1307`); the writer requires exactly
   that, and the e2e keeps `linuxV2WitnessClaimRequirementsMatch` and the provider-access
   assertions byte-identical.
4. **Two-sided key agreement.** The writer's `GOAL_ADMISSION_CASES` and the fixture's goal-routed
   rows must agree exactly: a writer-keyed case still on the legacy route gets a legacy
   ClaimResponse and the writer refuses it (M1's local half); a fixture goal case missing from the
   writer key would hit the legacy repository check on admission input.
5. **Runtime metadata is load-bearing.** Control's goal assignment requires
   `runtime.harness_kind in native_harness_kinds`, `capabilities @> {"adapter": {"operations":
   {"start": true, "events": true, "cancel": true, "pause": "unsupported"}}}`, and
   `adapter_matches_runtime_metadata?/2` equality of the adapter block against
   `harness_kind`/`harness_version`/`adapter_version`/`adapter_protocol_version`
   (`assignment.ex:332-341,:519-530`; `registry.ex:475-520`). The codex/claude specs mirror the
   pi-durability spec with `harness_version` `0.168.0` / `2.1.323` (the pins at
   `linux-v2-native-acceptance.sh:1135-1136`) and `adapter_version` `symmetry-daemon:ci-linux-v2`,
   `adapter_protocol_version` 1.
6. **No product byte changes.** `linux_clone_journal.go` and `linux_owner_witness_linux.go` stay
   byte-identical (the reserve keeps demanding a real admitted Subject).

## 4. Evidence plan

1. Base census: scratch run on the dispatch head + the branch-only scratch workflow and shim
   (`scratch/rf-acc-containment-goal-admission`); census every red source of both sets.
2. P1: WSL `mix run` probe against an ephemeral local Postgres database whose name starts
   `symmetry_linux_v2` (fixture `prepare` + `schedule` per case, registration through the
   register script against a local Control endpoint); positive rows assert the produced
   `symmetry.admission.v1` Work input, the fixture Subject, null `provider_scope`, no
   `provider_access`, and the assigned run on the codex/claude runtime. Negative row: adapter
   metadata dropped from the codex registration → no assignment (fixture raises).
3. P2: the same endpoint, the four cases through the writer; plus a local stub-claim probe for
   the two refusal rows the writer owns (non-null `provider_scope`; legacy goal-less input).
4. P3: extracted-predicate `go test` table (positive rows: the four cases; negative rows:
   `pi_provider_action`, `opencode_native_health`, `stage_replay`+`opencode`, `wsl2_denial`),
   plus M2/M5 mutant flips of the extracted text.
5. P4 static: `gofmt -l .`, `GOOS=linux go vet ./...` (daemon, WSL), `mix format
   --check-formatted`, `python3 -m py_compile`, the five `scripts/ci/test-linux-v2-*.py` suites,
   `bash -n` on the shim.
6. Scratch runs: base census (1) → first fixed-bytes run (2) → binding run on the final bytes
   (3, with the sha256 equality table). Hosted mutant probes only if the budget allows.

## 5. Out of scope (contract-bounded, with reason)

- `pi_provider_action`'s provider-scoped admission and both crash legs: latent, stage-1(b)
  finding recorded; no branch widening.
- `synthetic_goal_cleanup`: stage-1(a) determination only; its release-class residual stays with
  the release-wall tracking (`rf-acc-guest-native-exec`).
- The frozen OpenCode legs, the `shared` owner-leg stops, the credentialed all-14 dispatch.
- `scripts/ci/linux-v2-native-acceptance.sh` and the real workflow: no change expected; a needed
  change there is `DECISION_NEEDED`.

## 6. Post-B4 stop observed (run 2 `45665341553`) — an ownership decision, not a code decision

Run 2 put both sets past the clone reserve (the goal's pass condition) and stopped at the next
class, verbatim: `build explicit App-owned Linux v2 witness invocation: revalidate Linux owner
before native invocation: verify A-owned Git metadata tree
/opt/symmetry-owner-test/production/source/.git: Git metadata entry
…/.git/logs/refs/heads/symmetry is accessible to B: effective permissions for UID 52001 GID 52001
are 05, require 0 and forbid 07`. This is the same class the pi campaign fixed for its bundle
witness modes (`UMask=0077` at `linux-v2-native-acceptance.sh:1858-1860`, pi evidence §2b item 16):
the fresh `production`-mode legs stand in for the installed daemon, whose units set `UMask=0077`,
and without it the clone seed's new A-owned Git metadata is B-accessible and the App's own
pre-invocation revalidation refuses.

Options and recommendation, per the contract's acceptance clause (a post-B4 stop is an accepted
outcome only when carried with a named owner; "a wall with no owner is `DECISION_NEEDED`", and a
needed change in the acceptance script is `DECISION_NEEDED`):

1. **Recommended — amend the owned set by one line:** add `production` to the `case "$test_mode"`
   arm at `linux-v2-native-acceptance.sh:1858-1860`, then spend the reserved run 3 as the binding
   run. Same-class continuation of the pi precedent; no App, Control or assertion change; the two
   sets would then run their containment and `stage_replay_native_release_<kind>` witnesses to
   their own first real check.
2. **Fallback — assign the residual to a follow-up goal that owns the acceptance script.** The
   admission contradiction itself stays closed; the containment legs stay blocked on the fixture
   umask.

No bytes were changed on the strength of option 1 in this stage: the owned set is strict, so the
stage carries the verbatim stop and asks.

## 7. W1 amendment executed (conductor ruling 2026-11-26)

Option 1 was authorized and executed as one line: `production` joins the UMask arm at
`scripts/ci/linux-v2-native-acceptance.sh:1859` (`stage-replay|negative|production`). Proof that
`production` is the containment sets' fresh-leg mode: `:3043`/`:3047` call `run_native_owner_witness`,
which drives `run_go_test … production` at `:2285`/`:2325`; their
`stage_replay_native_release_<kind>` legs run `stage-replay` (`:2384-2390` → `:2435`), already in
the arm. Modes that also stand in for the installed daemon without the UMask are listed, not
added: `synthetic-cleanup` (`:1950-1956`; call `:3060-3062`) and `marker-reader` (`:1969-1974`;
call `:2993-2994`).

Binding run 4 (`45671784251`, head `b0ba2694` = the run-3 tree + the one line, scratch branch
deleted afterwards): both sets pass the clone reserve (zero `reserve owner witness clone` /
`clone intent does not match` lines) and the umask wall (zero `… are 05` lines), and stop at the
sets' next stops — codex: the AppArmor userns wall (`guest_stage=gate_drop_bounding_set errno=1`,
owned by `rf-acc-apparmor-userns-profile`, in flight); claude: the fixed `--json-schema` argv
element is 23 881 bytes, over the 16 KiB `maxCommandString` bound (`wire.go:53`, refused at
`wire_linux.go:450-452` via `linux_owner_v2_linux.go:182`), owned by the new conductor goal
`rf-claude-schema-argv` (design pending). Conductor ruling 2026-11-26 (after run 4): the goal's
own deliverable is met on these bytes, no further scratch run, and these two stops are the
residuals. Details, verbatim stops and the sha256 equality table: evidence record §3 and §6.

## 8. Post-run-4 amendments executed (conductor ruling 2026-11-26, apply S1-S6)

The apply stage (resumed 2026-11-26) executed the accepted simplify findings exactly as the ruling
specified; all three land after run 4 and off the witness run path, so run 4 still binds the walls
on its head bytes and the conductor's Bind run on the PR head binds the delivered bytes (evidence
§6; its row is left empty there). The §5 *Final bytes (lesson 86)* clause is superseded for these
three deltas by the ruling's first bullet.

- **S1** — the helper comment above `linuxV2WitnessRequiresGoalAdmission` now names the excluded
  provider-free opencode rows, so it no longer suggests the M5 over-broad reading.
- **S2** — the three goal-assigned runtime specs in `register-linux-v2-claim-machine.py` collapse
  into one `goal_assigned_runtime_spec` builder parameterised by kind/version/identity;
  `jscpd-changed` exits 0 on the delivered bytes, and the emitted session payload is
  byte-identical to run 4's (stub-captured before/after, evidence §4).
- **S4** — the predicate's row set is pinned in-repo by
  `TestLinuxV2WitnessRequiresGoalAdmissionRows` (5 positive / 6 negative rows) in the package's
  existing CI job; it supersedes the scratch extraction probe as the permanent pin while the
  extraction keeps binding the delivered predicate text (evidence §4, §6). The review fix r1
  derives the rows from the case constructors the witness tests dispatch (§9).
- **S3 is deferred** — the writer/fixture key-agreement check is recorded as a residual in
  `docs/TODO.md` (owner: a follow-up goal after PR #292 reshapes `ci.yml`), per the ruling.

## 9. Review fix r1 executed (conductor ruling 2026-11-26, review MUST_FIX accepted)

The review (`runs/2026-11-26/symmetry-rf-acc-containment-goal-admission-revie`) found one P1 and
three P3 findings; the conductor accepted all four. The fix stage executed them without touching
the run-input scripts:

| Finding | Location (review-time) | Disposition |
| --- | --- | --- |
| STD-A01 | `docs/archive/CLOSED-TODO.md` | the closure claim now reads "both containment cases past the clone reserve" and names both `stage_replay_native_release_<kind>` legs as unexecuted (each set stops at its first failing leg); census of the false-claim family over TODO/CLOSED-TODO/evidence/plan: one hit, converted |
| SPEC-A02 | this plan, §3.1 | the pi row count is twelve (`stage_replay` + the eleven pi-durability replay/ack-loss rows) |
| SPEC-A03 | evidence §3 | the `gate_lifecycle_linux.go:696`/`:796-797` anchors moved beside the `guest_stage=gate_drop_bounding_set` clause; `release_outcome_unknown` now cites `helper_v2_server.go:1200` and the `LinuxOwnerHelperV2ReleaseUnresolvedError` conversion at `helper_v2_protocol.go:675` |
| STD-A04 | the e2e predicate and its row test | the §2.4 map form plus constructor-derived rows: the row test builds every row from the same constructors the witness tests dispatch; the two rows without a single constructor (the matrix-wide `pi`+`stage_replay` shape and the writer-emitted `opencode`+`stage_replay` refusal shape) stay literal |

Re-collected on the final bytes (evidence §4/§5): the extracted predicate passes the 11-row
probe with the two hypothetical new provider-free codex/claude rows now `false`; M2's claude
mutation reddens both claude rows and M5's `return true` reddens six rows (the four false-want
rows plus the two hypotheticals); a harness change in the real codex constructor reddens the
in-repo row test (`codex_containment: … = false, want true`), which the pre-fix pin did not
observe. The predicate bytes are on the witness run path; its decision is unchanged for every
real case (the 11-row pin and the writer's case table, evidence §7(b)), so the conductor's Bind
run on the PR head binds the delivered bytes (evidence §6).
