# rf-w2-05 goal schema validators plan — one shared validator module for `goals/schemas.ex`

Contract: `goal-rf-w2-05-schema-validators.md` (GDD, 2026-11-21) with its binding
amendments and contract-review amendments: `refactor/w2-contract-review-opus-1.md`
P1-3, P2-1 and its three P3 notes; `refactor/w2-contract-review-ds-1.md` A4, A11;
the contract's own "Contract review amendments (binding)" section (pin tests first,
in a new test file, `goals_test.exs` read-only). Finding under implementation:
`refactor/findings/control-core.md` F2:195-221. Baseline for every coordinate and
count in this plan: this worktree's HEAD `4dd374bdcdbe09a05287e77ad90faaa949fcddcd`
(`4dd374b`, main). Branch: `refactor/rf-w2-05-schema-validators`.

The increment extracts the goal schema modules' copy-pasted changeset validators
into one shared module, byte-identically, and adds the message pins the reviews
found missing before touching the code. It changes no validation outcome, no
message byte, no cast list, no constraint and no caller outside
`control/lib/symmetry_control/goals/`.

## Public / API shape

New module `SymmetryControl.Goals.ChangesetValidators`
(`control/lib/symmetry_control/goals/changeset_validators.ex`), `@moduledoc false`,
three public functions, each byte-identical in behaviour to the copy it replaces:

| Function | Replaces | Message |
| --- | --- | --- |
| `validate_digest(changeset, field)` | `validate_digest/2` ×6, `validate_request_hash/1` ×2 | `"must be a 32-byte digest"` |
| `validate_map(changeset, field)` | `validate_map/2` ×4, `validate_response/1` ×2 | `"must be an object"` |
| `reject_changes(changeset, label)` | `reject_changes/1` ×7, `reject_changes/2` ×1 | `"#{label} is immutable"` |

`reject_changes/2` keeps the label-parameterized shape the existing
`GoalRevision` copy already uses (`schemas.ex:342`), per the opus P3 note; the
message template therefore lives in exactly one place and the eight call sites
pass the labels `"goal_revision"`, `"work dependency"`, `"context snapshot"`,
`"run evidence"`, `"work outcome"`, `"goal event"`, `"run usage"`,
`"external wait history"`.

Call form follows the repository's existing precedent
(`SymmetryControl.Workspaces.ChangesetValidators`, `workspaces/schemas.ex:1`,
called as `ChangesetValidators.validate_http_url(:url)`): each affected module
gains the module in its existing `alias SymmetryControl.Goals.{…}` group (or a
one-line alias where the module has no Goals alias yet) and calls
`ChangesetValidators.<fun>`. No `import`, so every call site names the owner and
the diff stays a deletion of the moved bodies plus in-place call renames.

No schema, migration, changeset function signature, cast list, constraint or
`goals.ex` caller changes. `changeset/2` (×14) and `immutable_changeset/2` (×8)
stay per module, as the contract requires.

## What moves (counted by the pinned instrument at base)

Instrument: `sh rf-w2-05-mix.sh run --no-start <scratch>/rf-w2-05-duplicate-defs.exs
lib/symmetry_control/goals/schemas.ex lib/symmetry_control/goals/changeset_validators.ex`
(AST count of `def`/`defp` clauses per name/arity; scratch copy outside the repo).
Base output over the owned lib set: `TRACKED-TOTAL 55` clauses, of which the F2
ten signatures are 53 (`changeset/2` 14, `immutable_changeset/2` 8,
`reject_changes/1` 7, `validate_digest/2` 6, `validate_map/2` 4,
`reject_terminal_authority_mutation/1` 2, `validate_request_hash/1` 2,
`validate_response/1` 2, `valid_uuid?/1` 4, `trim/1` 4) — the 53 reproduces the
ds-review baseline exactly — plus `reject_changes/2` 1 and
`validate_optional_digest/2` 1.

| Signature | Base (schemas.ex) | After | Where |
| --- | --- | --- | --- |
| `validate_digest/2` | 6 | 1 | shared module |
| `validate_map/2` | 4 | 1 | shared module |
| `reject_changes/1` | 7 | 0 | folded into `reject_changes/2` |
| `reject_changes/2` | 1 | 1 | shared module |
| `validate_request_hash/1` | 2 | 0 | call sites use `validate_digest/2` |
| `validate_response/1` | 2 | 0 | call sites use `validate_map/2` |
| `changeset/2` | 14 | 14 | unchanged |
| `immutable_changeset/2` | 8 | 8 | unchanged (cast lists differ) |
| `reject_terminal_authority_mutation/1` | 2 | 2 | unchanged (different message and field set per module) |
| `valid_uuid?/1` | 4 | 4 | unchanged (see boundary 3) |
| `trim/1` | 4 | 4 | unchanged (see boundary 3) |
| `validate_optional_digest/2` | 1 | 1 | unchanged (nil-tolerant, single copy) |
| **tracked total** | **55** | **36** | 22 definitions deleted, 3 created |

Measured on the delivered tree: 22 clause definitions deleted (21 bodies plus
the `reject_changes/2` body, which moves), 3 created; `schemas.ex` 1622 → 1476
lines (`git diff --stat` −184/+38), the new module 25 lines, net dLOC **−121**
on the owned lib set — inside the plan row's −120..−150. The tracked total is
`55 → 36` because `changeset/2` (14), `immutable_changeset/2` (8),
`reject_terminal_authority_mutation/1` (2), `valid_uuid?/1` (4) and `trim/1` (4)
stay by contract or by the boundary reasons below.

Post-change census (`rf-w2-05-duplicate-census.exs`, same instrument family,
counts signatures defined in more than one module): the only remaining
multi-module signatures are exactly those five, with the module lists recorded
in the stage report. Every shared validator now has one definition inside the
owned set.

## Exact edge-case semantics the contract demands

1. **Byte-identical messages** (clause 3: any changed message = not achieved).
   The moved bodies are byte-identical copies of the base bodies; the only text
   that changes place is the message literal, which for `reject_changes` becomes
   the single `"#{label} is immutable"` template with the eight base labels
   verified against `schemas.ex:228`, `:384`, `:461`, `:799`, `:928`, `:1241`,
   `:1394`, `:1558`. Pinned by
   `control/test/symmetry_control/goals/changeset_validators_test.exs`
   (32 invalid-value drives across nine digest, fifteen object and eight
   immutability fields, plus the valid-digest acceptance and the struct-path
   `"is invalid"` pins; added and green on base bytes first, per the contract's
   binding amendment).
2. **Accept/reject outcome unchanged**: each pinned drive asserts
   `refute changeset.valid?` plus the exact returned message; the valid-digest
   drive asserts the message list stays empty. Beyond the pins, the unchanged
   call sites are exercised by `goals_schema_test.exs`, `goals_test.exs` and
   `goals_external_wait_schema_test.exs`.
3. **Message reachability (measured at base, reported, not silently papered
   over)**: the digest fields accept any binary in `cast/3`, so
   `"must be a 32-byte digest"` is reachable through the plain struct call
   (`ContextSnapshot.changeset(%ContextSnapshot{}, %{content_hash: "short"})`).
   The `:map`-typed fields are *rejected by `cast/3` before the validator runs*:
   a non-map value yields Ecto's own `"is invalid"`, and the shared validator's
   `"must be an object"` fires only for a caller that pre-seeds the change
   (`Changeset.change/2` + `put_change/3` on the changeset passed as the
   changeset's `data`). The pin file drives that path and additionally pins the
   `"is invalid"` outcome of the struct path, so both observable behaviours are
   fixed. `grep -rn "put_change(:(acceptance_contract|payload|content_hash|subject_hash|request_hash|response)" control/lib control/test`
   finds no production caller (and no pre-existing test) seeding those fields at
   the base revision; the new pin file's drives are the only seeds, and they
   exist to keep the message reachable and byte-identical. The validator is a
   defence-in-depth guard, and this plan does not claim it is exercised by any
   production caller today.
4. **`reject_changes` order and shape**: the shared body keeps
   `Enum.reduce(changeset.changes, changeset, …)` with `add_error/3` per changed
   field, so error order per changeset is unchanged; the `_value` vs `_` pattern
   difference between the base copies has no observable effect and the shared
   body uses one form.
5. **Receipt pair folding**: `HarnessSessionStopReceipt` /
   `HarnessSessionAttachReceipt` `validate_request_hash/1` and
   `validate_response/1` are not distinct validators — they are
   `validate_digest/2` and `validate_map/2` with the field fixed — and their two
   messages are the same two literals. Keeping them would leave both message
   bytes duplicated inside the owned set, which is exactly the drift hazard
   clause 1 removes, so their call sites become
   `ChangesetValidators.validate_digest(:request_hash)` /
   `…validate_map(:response)` and both bodies are deleted (4 clauses).

## Decisions and boundaries

1. **New file (contract-named), not an inline module.** The contract's own
   owned-file set names `goals/changeset_validators.ex` (new); the opus P3 note
   allows either and asks for a recorded reason if the file is separate. Reason:
   `goals/schemas.ex` is 1622 lines / 15 modules (the `workspaces/schemas.ex`
   precedent is 788 / 4), the `goals/` area already keeps its shared pieces in
   sibling files (`contract_validation.ex`, `validation_profiles.ex`,
   `read_model.ex`), and a separate file is what makes "one definition per
   shared validator inside the owned set" countable by the pinned instrument
   over two named paths. The *call* convention still follows the workspaces
   precedent (alias + remote call), so no second call convention is created.
2. **`valid_uuid?/1` and `trim/1` stay module-local** (2 module copies each).
   They are not named by the contract's Change clause (`validate_digest`,
   `validate_map`, parameterized `reject_changes`), they carry no user-facing
   message (so no message-drift risk, which is the clause-1 hazard), and folding
   them would widen the shared module into unrelated predicates
   (`uuid_array?/1`, `evidence_ids`, title/objective trimming). They are listed
   as residual duplicates rather than silently mixed into the shared set.
3. **`reject_terminal_authority_mutation/1` stays duplicated**: its two copies
   differ in both the message (`"cannot be changed after Goal terminalization"`
   vs `"… after decision terminalization"`) and the guarded field list, so
   sharing it would change bytes or add a parameter that hides two different
   policies. Out of the shared set by both the finding and the contract.
4. **Single-copy validators and inline call-site lambdas keep their literals.**
   They are not duplicate definitions by the finding's own criterion (a
   signature defined in more than one module), so folding them would rewrite
   changeset bodies with zero duplicate-definition reduction. All of them stay
   byte-identical and are pinned by the new table test: the
   `GoalRevision.execution_policy` non-map fallback (`schemas.ex:314`), the
   `WorkOutcome` `candidate_subject` lambda (`:802`, reachable — `JsonDocument`
   casts lists), the `GoalDecision.resolve_changeset` `resolution` lambda
   (`:959`), the `GoalExternalWait` `validate_source_ref/1` (`:1431`) and
   `validate_result/1` (`:1437`) helpers, and the nil-tolerant
   `validate_optional_digest/2` (`:976`). The residual literal census of the
   owned set is `"must be an object"` 5 + 1 (shared), `"must be a 32-byte
   digest"` 1 + 1 (shared) and `"is immutable"` 1 + 1 (shared, and that one
   remaining module literal is the different `"terminal external wait is
   immutable"` message), all enumerated here and recorded in the stage report
   against the base counts 11 / 9 / 9.
5. **Test placement and `goals_test.exs`**: `goals_test.exs` is read-only (opus
   P2-1 + ds A4 + the contract's binding amendment name open W1-12 as its
   writer). The opus P1-3 amendment asks for the pin table; the contract's own
   binding amendment (later, and specific) says to add the pins first "in a new
   test file (e.g. `control/test/symmetry_control/goals/changeset_validators_test.exs`)".
   The substance of P1-3 is satisfied in full at that location (every touched
   schema module's immutability message, every digest/map field's message, exact
   bytes, green on base, mutation-checked), and `goals_schema_test.exs` stays
   untouched so no other open slice's fixture work is disturbed. The contract's
   targeted gate is extended with `goals_external_wait_schema_test.exs` (P1-3)
   and the new pin file.
6. **Mutation check** (program rule: converted assertions need a red-on-flip
   demonstration): the *new* pins are the tests this slice adds, and the code
   they guard is moved. Red-on-flip is demonstrated twice on the delivered
   bytes — flip one message literal in the shared module (expect the digest and
   map table tests red), flip one label argument at a call site (expect the
   immutability table red) — each reverted and re-run green. Output recorded in
   the stage report.

## Evidence plan

All gates foreground, in the contract's order, on the final bytes, with
`MIX_TEST_PARTITION=rf-w2-05`, `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`
unset, through the worktree-adapted portable recipe
(`rf-w2-05-mix.sh`: erl.exe invoked directly, `MIX_DEPS_PATH` pointing at a copy
of the main checkout's deps taken before any gate ran — identical `mix.lock`
md5 `99cf712fe3eba0bc263701095548bf31`; build output stays in this worktree):

1. `mix format --check-formatted`
2. `mix compile --warnings-as-errors`
3. `mix test test/symmetry_control/goals_schema_test.exs test/symmetry_control/goals_test.exs
   test/symmetry_control/goals_external_wait_schema_test.exs
   test/symmetry_control/goals/changeset_validators_test.exs`
4. Pinned duplicate-def instrument, base vs head (the clause-2 judging artifact)
5. Byte-level message-literal census inside the owned set (`grep -n` counts per
   message at base vs head)
6. `git diff --check`; the program rule's profile-path grep over the delivered
   diff before finishing (no host profile path appears in any delivered file)
7. Migration tests and the full `mix test` are the conductor's gate-runner job
   (never concurrently with another full suite on this host); CI's `control` job
   stays the CI-only oracle.

## Out of scope

`goals.ex` regions (W2-07/W3-10/W4-07), `goals/read_model.ex` (W2-08),
control-web, `goals_test.exs` (read-only), migrations, contracts/schemas,
daemon, frontend. No `credo:disable`/suppression, no new dependency, no
behaviour change beyond the extraction, no documentation change outside
`docs/plans/` (no TODO entry names this work; nothing is closed).

## Residual risks

- The extraction's only real risk is a silently changed message or ordering; it
  is bounded by the new 32-drive pin table (green on base first) plus the
  existing suites, and by the two mutation runs.
- dLOC lands slightly above the plan row's estimate (the shared module head and
  alias lines are new lines). Reported as measured, not adjusted.
- `validate_map`'s message is reachable mainly through a seeded changeset today
  (boundary 3). That is a pre-existing reachability property of the base code;
  this slice preserves it instead of "fixing" it, because changing the cast
  behaviour or the field types would change validation outcomes (clause 3) and
  belongs to a contract change, not to this refactor.
