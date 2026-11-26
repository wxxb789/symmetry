# rf-acc-pi-provider-admission — implementation plan (provider-scoped Goal admission for the three pi legs)

Contract: `goal-rf-acc-pi-provider-admission.md` (GDD state root, external to the repository); the
conductor's ruling of 2026-11-26 is the contract's own "Ruling" section, and the residual it closes
is `docs/TODO.md` `## rf-acc-containment-goal-admission: residuals (2026-11-26)`. Dispatch head
`b27ea691` (#300 `rf-acc-shared-journal-release`). Branch `refactor/rf-acc-pi-provider-admission`.

The accepted plan is the contract; this file resolves only the implementation choices it leaves
open, the clause census at the dispatch head, the exact edge-case semantics, the evidence plan, and
what is out of scope.

## 1. Clause census at the dispatch head (re-located by content)

| Contract clause | Dispatch head (`b27ea691`) | Disposition |
| --- | --- | --- |
| Stage 1 routing map | writer `:29-38` (`PROVIDER_ACCESS_CASES`, `GOAL_ADMISSION_CASES`), writer `:199-217` (goal/legacy admission branch); fixture `:41-140` (`@cases` with roles `:pi_provider`/`:opencode`/`:pi_durability`/`:opencode_goal`), `:152-154` split, `:186-256` `goal_case_specs`/schedule, `:447-570` `provision_goal_admission!`, `:603-610` `runtime_adapter_binding?/3`; e2e `production_linux_v2_native_witness_test.go:1682-1690` admission branch, `:1780-1793` predicate map, `:1796-1822` row test; register script `:129-160` runtime specs | §3.1-§3.4 |
| Stage 2 route the three legs | the same files; no Control lib change (`admission.ex` already composes the scope at `:1218-1268`/`:1304-1307`/`:333-335`) and no daemon production change (the clone reserve `linux_clone_journal.go:864-878` already demands the admitted Subject) | implement here |
| Key agreement stays green | `scripts/ci/test-linux-v2-owner-request-keys.py` (`GOAL_ROUTE` `:27-30` pins the two-role predicate; `check_agreement` `:189-215` compares writer goal set to fixture goal-routed rows) | §3.3 extends it to the provider pair |
| Owned set | six code files + plan + evidence + TODO/CLOSED-TODO; `linux_owner_witness_linux.go` byte-identical (its witness request reader keys provider authority on the case name, `:2616-2652`, not on the admission shape); `linux-v2-native-acceptance.sh` byte-identical | §2.5 (one-file amendment) |
| Acceptance | the conductor's Bind `shared` run; local gates are the key-agreement test, unit/e2e compile gates, mutants | §4 |
| Dispatch-head failure | run `45771518805` on `84dc22c3`: `pi` witness `reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` (`findings/300-binding-run.md` §3.1/§3.4) | §3.5 |

## 2. Choices the contract leaves open

1. **Fixture routing flag.** The three pi rows keep the literal role `:pi_provider` (their
   repository is `pi_repository`, the only one with a GitHub connection) and join the goal split:
   `repository_role in [:pi_provider, :pi_durability, :opencode_goal]` → goal cases; `:opencode`
   stays the only legacy role. `goal_case_specs` binds `:pi_provider` → `pi_repository.id`, so
   `@expected_fixture_runs` stays 48 (46 goal: 39 pi-durability + 4 opencode_goal + 3 pi_provider;
   2 legacy). The schedule shape check keeps `provider_access? == (repository_role == :pi_provider)`
   unchanged; `runtime_adapter_binding?/3` gains the `:pi_provider` → adapter kind `"pi"` arm.
2. **Provider-scope fact.** `goal_case_specs` carries `"provider_sync" => repository_role == :pi_provider`;
   `provision_goal_admission!/5` writes `["resource.sync"]` into BOTH `authority_policy.allowed_actions`
   and `execution_policy.allowed_actions` iff that flag is set, else `[]` (the containment shape).
   Control then composes `provider_scope` (`resource_ids` `[pi_repository.id]`,
   `operations_by_resource` `["resource.sync"]`, `change_target` nil) and adds
   `required_capabilities["provider_access"]` for exactly those cases.
3. **Writer key.** `GOAL_ADMISSION_CASES` gains the three legs; the goal branch splits on
   `provider_case` (`PROVIDER_ACCESS_CASES`, profile `pi-native`): provider-scoped legs must carry the
   exact composed `provider_scope`, provider-free goal legs must carry `provider_scope: null` (the
   existing clause, reworded). `expected_capabilities` is unchanged and now holds on both routes
   (`{"provider_access": true}` for the three; `{}` for every provider-free goal leg).
4. **e2e predicate and scope check.** `linuxV2GoalAdmissionRequestCases` gains the three
   `{harness: "pi", requestCase: …}` rows; the admission branch's dispatch is the pure predicate
   `linuxV2AdmissionProviderScopeRefusal(providerActionRequired, scope, resourceID)`:
   `providerActionRequired` rows require the exact Control-composed scope via
   `linuxV2AdmissionProviderScopeIsExact` (nil scope, wrong resource, extra or wrong operation
   and non-nil change target all false), and any non-nil scope is refused otherwise. The row
   test flips the three pi rows to `true`; `TestLinuxV2AdmissionAllowsProviderSyncRows` and
   `TestLinuxV2AdmissionProviderScopeRefusalRows` pin the scope check and both dispatch
   directions with the verbatim refusals (review r1, REV-A02).
5. **Scope amendment (one file): `scripts/ci/register-linux-v2-claim-machine.py`.** Control's goal
   assignment admits only runtimes whose `capabilities` contain both the adapter block and
   `task.required_capabilities` (`assignment.ex:322-341`, `:505-530`), so the three goal-routed legs
   need a pi-native runtime that is adapter-capable and provider-capable. The generic
   `owner-witness-control-pi-generic` spec — used by exactly these three legs — becomes a
   `goal_assigned_runtime_spec(..., provider_access=True)`; the non-pi specs, their emitted payloads
   and the daemon-side configs are untouched. This is a CI fixture script (the class the owned set
   already lists), not Control admission policy and not daemon production code, so the contract's
   `DECISION_NEEDED` clause does not apply; the contract's "add them to the fixture's goal routing"
   clause cannot be satisfied without it. Recorded in the evidence record §8 and the report.
6. **`linux_owner_witness_linux.go` stays byte-identical.** Its only admission-adjacent check keys
   on the case name (`:2616-2626`) and already requires exactly `{"provider_access": true}` plus an
   authentic ClaimResponse `provider_access`; the witness seeds the ClaimResponse Work verbatim
   (`:4257-4290`), so the state-layer clone reserve is the only consumer of the admission shape on
   this path. No witness seam byte changes.

## 3. Exact edge-case semantics the contract demands

1. **Provider-free goals stay provider-free.** The 43 existing goal rows keep
   `allowed_actions: []` (`provider_scope: null`, no `provider_access`); the writer, e2e branch and
   the pi durability/containment legs keep their current assertions byte-for-byte where the shape
   is unchanged.
2. **No weakened guards.** The clone reserve, `Subject` equality, `Validate()`, the provider-access
   verdict (`linuxV2ClaimAllowsProviderSync`, `linuxV2WitnessClaimRequirementsMatch`,
   `validateLinuxV2NativeWitnessRequest`) and the legacy refusals (`else if err != nil || admitted`,
   the legacy repository check, the fixture's provider-free `required_capabilities == {}`) are
   untouched. The provider-scoped branch is strictly stronger than the old `provider_scope == nil`
   refusal for these three legs: it pins the exact composed scope, not merely a non-null one.
3. **Two-sided key agreement.** `test-linux-v2-owner-request-keys.py` now also pins: fixture rows
   whose role is the provider-sync role are goal-routed; the writer's provider set (its
   `PROVIDER_ACCESS_CASES` profile rule) is a subset of its goal set; and the two provider sets are
   equal. Drift in either direction is refused with the offending case names.
4. **Assertion relaxation accounting (brief-rules).** The single relaxation is the e2e branch's
   `admission.ProviderScope != nil` refusal, which applied uniformly to all five goal-routed rows.
   Guarantees it proved and their surviving/new checks: (a) a provider-free leg cannot hide a
   provider scope → kept verbatim for the five provider-free goal rows (mutant: scope on a
   `stage_replay` row); (b) a goal row must carry the Control admission for the exact Subject →
   unchanged (`admission.Subject != request.Subject`); (c) the provider legs' authority must be the
   exact Control composition → new, stronger check (scope equality on resource ids, operations and
   change target) with row test `TestLinuxV2AdmissionAllowsProviderSyncRows`.
5. **The observed failure.** `requireLinuxCloneJournalFence` (`linux_clone_journal.go:864-878`)
   refuses when `protocol.ParseAdmissionInput(journal.Work.Input)` reports `present == false`; the
   journal's Work is the request Claim's Work (`linux_owner_witness_linux.go:4257-4290`), which for
   a legacy pi claim carries `repository_resource_id` and no admission envelope. Routing the legs
   through the provider-scoped admission makes `present == true` with the same
   `Subject.ResourceID/Commit/TreeDigest` the witness planned its intent from
   (`linux_owner_witness_linux.go:3340-3362`).

## 4. Evidence plan

1. **P1 static/unit (Windows host).** `python3 scripts/ci/test-linux-v2-owner-request-keys.py`
   (committed pair + every mutant), `python3 -m py_compile` on the three python scripts, the
   `scripts/ci/test-linux-v2-*.py` suites, `bash -n` where the touched shell is not edited.
2. **P2 writer probes (stub Control endpoint, Windows host, scratch only).** Parse-identical
   `RUN.json`/`CONFIG.json` fixtures plus a stub HTTP claim endpoint: exact-scope ClaimResponse →
   writer exit 0 and the written request's `claim`/`subject`/admission checks; provider-scope
   mutants (null scope, extra operation, wrong resource, non-nil change target, missing
   `provider_access`) → the writer refuses with exit 1 and no output file. The stub is deleted
   afterwards (scratch directory only).
3. **P3 extracted-predicate and helper rows (WSL).** `go vet ./...` from `daemon/` (linux/amd64,
   WSL) plus `go test ./e2e/ -run
   'TestLinuxV2WitnessRequiresGoalAdmissionRows|TestLinuxV2AdmissionAllowsProviderSyncRows'`; the
   helper mutant (`return scope != nil`) must redden the new row test.
4. **P4 compile gates.** `gofmt -l` (changed files), `go vet ./...` from `daemon/` on Windows and
   in WSL; the Elixir side: `mix format --check-formatted` hunk-scoped on the fixture, `mix
   compile` with warnings-as-errors and the Credo strict ratchet per the contract, plus the
   relevant Control tests that own `admission_provider_scope!`/`validate_goal_claim_authority` if a
   serialized local database is available (else recorded as unrun).
5. **Mutants** are the key test's own mutation cases (P1), the stub-probe refusals (P2) and the two
   Go row-test mutants (P3); each records the command and the refused/rejecting output.
6. **Records.** This plan, `docs/evidence/rf-acc-pi-provider-admission-2026-11-26.md`,
   `docs/TODO.md` and `docs/archive/CLOSED-TODO.md`; the Conductor's Bind `shared` run binds the
   delivered bytes (its row is left for the bind stage).

## 5. Out of scope (contract-bounded, with reason)

- Any Control lib or daemon production change: the contract routes those to `DECISION_NEEDED`; none
  is needed (the reserve and the admission composer already implement the target shape).
- The `stage_replay_native_release_<kind>` legs, the containment sets' post-wall stops, the
  readback reader's `shared`-scoped stop row and the other `docs/TODO.md` residuals of the
  preceding goals — they belong to the conductor's dispatch order.
- `scripts/ci/linux-v2-native-acceptance.sh` (its `production` mode already supplies the owner
  witness request, the UMask arm and the provider-receipt readback for these three legs), the
  workflow files and `daemon/internal/app/linux_owner_witness_linux.go`.

## 6. Gate set (contract "Acceptance", plus `refactor/brief-rules.md`)

Behavior changes run the FULL `go test ./...` from `daemon/` on Windows and in WSL; a green
compile/e2e-rows run is not a substitute where the full suite is runnable, and any failure is
attributed base-vs-head. The Elixir contract gates (mix format/compile/Credo strict, relevant
tests) are recorded with their commands and exit codes. The `-` gate placeholder carried no gate
command, so every gate below is run explicitly and its exit code recorded.
