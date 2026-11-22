# rf-w6-08 protocol dead envelope family — plan

Contract: `goal-rf-w6-08-protocol-dead-envelope-family.md` (GDD, dispatched 2026-11-22; binding
conductor decisions: D5 delete-and-retarget, D2 report-only LOC, lesson-32 control-flow audit,
comment policy 2026-11-22). Baseline: this worktree's HEAD `e84973a76abd46125cad08ed76710cec446388f4`
(origin/main, `#220`); every coordinate below was read at that revision. The contract's own
coordinates are from `0fa9b705` (2026-11-21) and are re-derived here as the contract requires.

## What the base tree holds at this revision

The contract's dependency gate is satisfied: `#178` (W2-12b) is merged as `c171663b`, the operator
client and the v1 `Task`/`TaskWaiting` decoders now live in `daemon/e2e/internal/operator/`, and
`daemon/internal/protocol/protocol.go` (649 L at this baseline; the contract's 719 L coordinate is
`0fa9b705`'s) holds no legacy decoder — its remaining
`UnmarshalJSON` set (`RuntimeRegistration`, `RuntimeCapabilities`, `Work`, `ClaimResponse`,
`LeaseHeartbeatResponse`, `Run`, `TaskCommand`) is unreported by `deadcode` on both GOOS, and the
file therefore drops out of the owned set entirely (recorded, not edited).

Tools and method (pinned, from `daemon/`):

```
deadcode.exe (the mise-managed Go 1.27.4 toolchain's bin directory)
  golang.org/x/tools v0.53.0, built by go1.27.4 windows/amd64
deadcode ./cmd/...                 # GOOS=windows GOARCH=amd64 (host)
GOOS=linux GOARCH=amd64 deadcode ./cmd/...
deadcode -test ./...               # test binaries as roots; classification leg
```

Measured before the change at `e84973a7` (findings are `file:line:col: unreachable func: name`):

| run | findings | note |
| --- | --- | --- |
| `deadcode ./cmd/...` (windows/amd64) | **118** | the before count |
| `GOOS=linux GOARCH=amd64 deadcode ./cmd/...` | **227** | the before count |
| `deadcode -test ./cmd/...` (both GOOS) | 118 / 227 | byte-identical to the plain run: `./cmd/...` has no test files, so `-test` adds no root |
| `deadcode -test ./...` (test binaries as roots) | 23 (windows) / **0** (linux) | none of the 26 candidates appear: **every reference to the family is test-only** |

The contract's draft counts (250 linux / 140 windows) are stale: wave-6 deletions from other slices
lowered the base (227/118 now). The delta this slice must show is the deleted set's exact size,
re-measured, not the contract's arithmetic.

## The candidate set — two proofs per symbol

All 26 rows below appear **identically** in the windows and linux `deadcode ./cmd/...` findings at
`e84973a7`, with the line numbers of the contract (the audit's coordinates reproduce). Strategy (b)
is `git grep -nw <symbol>` over the whole repository (production + tests + e2e + docs); the
"references beyond" column lists every file that holds a reference after the defining file, and how
each one is dispositioned by this slice.

| # | symbol (file:line) | references beyond the definition | disposition |
| --- | --- | --- | --- |
| 1 | `ParseAdapterCapabilities` (`goal_admission.go:782`) | `goal_schema.go` (deleted caller), `goal_transport_test.go:98,107` (retargeted) | deleted |
| 2 | `validateGoalCreatePredicateIDs` (`goal_contract_semantics.go:25`) | `goal_schema.go` (deleted caller) | deleted |
| 3 | `validateGoalCommandPredicateIDs` (`:52`) | `goal_schema.go` (deleted caller), same-file deleted callers | deleted |
| 4 | `validateGoalRevisionContractSemantics` (`:144`) | `goal_schema.go` (deleted), same-file deleted callers, `contracts/ts/semantics.ts:186` (a TS symbol of the same name, not this symbol) | deleted |
| 5 | `canonicalSHA256` (`:184`) | same-file deleted caller | deleted |
| 6 | `canonicalPlanProposalSHA256` (`:197`) | `goal_subject_test.go:207,220` (retargeted), `docs/archive/CLOSED-TODO.md:3640` (historical record, see residual risks) | deleted |
| 7 | `normalizePlanProposal` (`:205`) | same-file deleted caller, `goal_subject_test.go:204` (comment, retargeted) | deleted |
| 8 | `EvidenceBatch.UnmarshalJSON` (`goal_evidence_batch.go:31`) | `goal_evidence_batch_test.go` (dropped cases), `goal_schema.go` (deleted) | deleted |
| 9 | `ParseEvidenceBatch` (`:82`) | `goal_evidence_batch_test.go:14,31,60,77,88` (retargeted/dropped), `goal_schema.go` (deleted) | deleted |
| 10-16 | `EvidenceBatchResponse/ReceiptSet/Receipt/ConflictDetails/ConflictItem` `UnmarshalJSON` + `Validate` (`goal_evidence_batch_response.go:72,81,124,139,171,207,221`) | `goal_evidence_batch_response_test.go` (dropped cases), `goal_schema.go` (deleted) | deleted |
| 17-18 | `ParseEvidenceBatchResponse` (`:239`), `ParseEvidenceBatchConflictDetails` (`:253`) | `goal_evidence_batch_response_test.go` (dropped), `goal_schema.go` (deleted) | deleted |
| 19 | `ParseEvidence` (`goal_results.go:1010`) | `goal_evidence_batch.go` (deleted), `goal_schema.go` (deleted), `goal_contract_fixture_test.go:111,115`, `goal_transport_test.go` ×4, `control/goal_client_test.go:1062`, `app/cleanup_test.go:892`, `app/goal_delivery_test.go:2097`, `state/goal_delivery_test.go:1088`, `state/linux_clone_journal_test.go:1307`, `workspace/linux_clone_linux_test.go:1357` (all retargeted) | deleted |
| 20 | `ParseUsage` (`:1124`) | `goal_schema.go` (deleted), `goal_transport_test.go:382,386,438`, `control/goal_client_test.go:1071` (retargeted) | deleted |
| 21 | `DecodeGoalRevision` (`goal_schema.go:73`) | `goal_schema.go:239` (deleted caller, same file) | deleted |
| 22 | `ValidateGoalCommand` (`:93`) | `goal_schema.go:235` (deleted caller, same file) | deleted |
| 23 | `DecodeDecision` (`:146`) | `goal_schema.go:220` (deleted caller, same file) | deleted |
| 24 | `ValidateGoalCreate` (`:173`) | `goal_schema.go:237` (deleted caller, same file) | deleted |
| 25 | `ValidateGoalEnvelope` (`:208`) | `goal_contract_fixture_test.go:70`, `goal_contract_semantics_test.go` ×7 (retargeted/dropped), `contracts/README.md:217` (prose citation; rewritten by the fix stage under the conductor's REV-01 owned-set amendment) | deleted |
| 26 | `ParseSubject` (`goal_subject.go:91`) | `goal_transport_test.go:71` (retargeted) | deleted |

Every non-test reference of the 26 is either inside the defining file or inside a caller this slice
deletes (rows 1-18, 21-25) — measured: `git grep -nw` finds **0** production callers outside
`daemon/internal/protocol/` for all 26. Both proofs therefore hold for the whole set, and the
fixture/e2e/doc corpus holds no `daemon/e2e/**`, `daemon/native/**` or witness reference to any of
them (`git grep -nw` over those trees: 0 hits; the only apparent e2e hit, `EvidenceBatchConflictDetails`,
is the *contracts* DTO `contracts.SymmetryEvidenceBatchConflictDetailsV1`).

### Owned-set correction (contract clause "Owned files")

Two of the contract's file coordinates do not match `e84973a7`:

1. **`goal_subject_test.go` is a retargeted suite and is missing from the enumeration.** Rows 6/7's
   test references live there (`:189-222`), not in `goal_contract_semantics_test.go`. The clause's
   operative rule is "the test files carrying references to deleted symbols", so this file is added
   to the retarget set (6 references, one case).
2. **`goal_admission_input_test.go` carries no reference to a deleted symbol** (it exercises the live
   `ParseAdmissionInput`); nothing in it changes. It stays in the owned set unused.

Likewise `daemon/e2e/internal/operator/**` is untouched (grep above), and `goal_validation.go` is
expected to need no edit: every helper there is reached from a retained validator; the post-deletion
`deadcode` fixpoint run is the proof, and if it reports a newly dead helper it is deleted with its
own row.

## Resulting API shape (public surface)

Removed from package `protocol` (26 functions): the five envelope decoders/validators
(`DecodeGoalRevision`, `ValidateGoalCommand`, `DecodeDecision`, `ValidateGoalCreate`,
`ValidateGoalEnvelope`), the two normalised-payload parsers (`ParseEvidence`, `ParseUsage`), the
subject parser (`ParseSubject`), the adapter-capabilities parser (`ParseAdapterCapabilities`), the
evidence-batch family (`ParseEvidenceBatch`, `ParseEvidenceBatchResponse`,
`ParseEvidenceBatchConflictDetails` and the eight `UnmarshalJSON`/`Validate` methods of the batch
response DTOs), and the six private contract-semantics helpers of `goal_contract_semantics.go`.

Retained byte-identically (the contract's live list, re-verified): `TaskResult`, `Receipt`,
`Evidence`, `Admission`, `ParseAdmission`, `ParseAdmissionInput`, `DecodeContextSnapshot`,
`validateContextSnapshotPredicateIDs`, `validateProviderChangeTarget`,
`validateAcceptancePredicateIDs`, `ValidateMicroUSD`, every wire DTO struct of the seven files, and
every `UnmarshalJSON`/`Validate` method they own (`Admission`, `TaskResult`, `Blocker`, `NextAction`,
`Diagnostic`, `SourceRef`, `OutputRef`, `ReviewFinding`, `Receipt`, `Evidence`, `Usage`,
`RuntimeCapabilities`, `RuntimeRegistration`, `Subject`, …). `ValidatePlanProposal` also stays: it is
live through `TaskResult.Validate` (`goal_results.go:343`) — the contract's plan row never listed it
as dead, and `deadcode` agrees on both GOOS.

Also retained, with their liveness re-proven rather than assumed, because the retarget depends on
them:
- `RejectDuplicateJSONMembers` / `scanJSONValue` — live through `ParseAdmissionInput` and
  `control/validation.go` (W2-12a's strict-path scan).
- `Evidence.UnmarshalJSON` / `Usage.UnmarshalJSON` / `RuntimeCapabilities.UnmarshalJSON` /
  `Subject.UnmarshalJSON` — cmd-reachable at both GOOS (absent from both 118/227 findings), reached
  from the production journal/registration readers (`state.GoalDelivery.Evidence *protocol.Evidence`,
  the usage journal fields, the runtime-registration decode); they are the semantic layer the deleted
  `Parse*` wrappers delegated to, so a case retargeted onto them keeps its exact rule.

## Edge-case semantics the retarget must preserve

`ParseEvidence`, `ParseUsage`, `ParseSubject`, `ParseAdapterCapabilities` are two-line wrappers:
`decodeGoalEnvelope(data, envelope, &wire, &semantic)` = `contracts.DecodeSchema` (generated-DTO
schema) followed by `json.Unmarshal` into the semantic type, whose `UnmarshalJSON` runs
`decodeStrictObject` (duplicate-member scan, required members, unknown-field rejection) plus the
type's `Validate`. The retarget that keeps each case's property byte-for-byte is therefore:

| base call | HEAD oracle | why identical |
| --- | --- | --- |
| `ParseAdmission(x)` | `ParseAdmissionInput(x)` | `ParseAdmissionInput` dispatches a bare current-schema envelope to `ParseAdmission` and returns its error with `present=true` (`goal_admission.go:545-561`) |
| `ParseTaskResult(x)` | `ParseTaskResult(x)` | production entry point, unchanged |
| `ValidateGoalEnvelope(EnvelopeContextSnapshot, x)` | `DecodeContextSnapshot(x)` | `DecodeContextSnapshot` is the envelope's semantic boundary and the production entry point |
| `ParseSubject(x)` | `json.Unmarshal(x, &Subject{})` | `ParseSubject` *is* that call (`goal_subject.go:91-97`) |
| `ParseEvidence(x)` | `json.Unmarshal(x, &Evidence{})` | `ParseEvidence` is `DecodeSchema` + that call; the fixture suite asserts `DecodeSchema` separately for the same bytes |
| `ParseUsage(x)` | `json.Unmarshal(x, &Usage{})` | same decomposition |
| `ParseAdapterCapabilities(x)` | `json.Unmarshal(x, &RuntimeCapabilities{})` | same decomposition; `RuntimeCapabilities.UnmarshalJSON` runs the schema check itself (`protocol.go:191-207`) |
| `ParseEvidenceBatch(x)` for the duplicate-member cases | `RejectDuplicateJSONMembers(x)` | the batch wrapper's only retained persistent rule; the batch *identity* rules (`evidence_key`/`evidence_id` duplicates, item `run_id` equality) are deleted with the parser and have no live daemon equivalent |

Measured on the fixture corpus, to make the "no oracle" decision evidence-bound instead of assumed:
a throwaway in-package probe ran the mapping above over `contracts/fixtures/manifest.json` and
reproduced `semantic_valid` for **85 of 85** fixtures of the six oracle-bearing envelopes
(`admission` 34, `task-result` 18, `usage` 12, `evidence` 10, `context-snapshot` 8,
`adapter-capabilities` 3) with zero mismatches. (`goal_contract_fixture_test.go`,
`goal_transport_test.go` cases are the same bytes.)

Fixture-suite dispatch after the retarget (`TestGoalTransportParsesContractFixtures` keeps its name
and its schema-validity arm unchanged, `contracts.ValidateSchema`):
`admission` → `ParseAdmissionInput`; `task-result` → `ParseTaskResult`; `context-snapshot` →
`DecodeContextSnapshot`; `plan-proposal` → `ValidatePlanProposal` (restored by the apply stage,
finding 1: the deleted `ValidateGoalEnvelope` dispatched this envelope to exactly that call, and the
function is live through `TaskResult.Validate`); `adapter-capabilities`/`evidence`/`usage` → the
retained DTO decode; `connections-read` and the six presentation reads → unchanged `DecodeSchema`
arm; the seven envelopes with no daemon-side decoder (`decision`, `goal-command`, `goal-create`,
`goal-revision`, `evidence-batch`, `evidence-batch-response`, `evidence-batch-conflict-details`)
carry the schema arm only; any envelope in neither list fails the subtest closed.

## Case-by-case retarget table

Retargeted (property preserved; oracle swapped; test names byte-identical):

| case | base oracle | HEAD oracle | cases |
| --- | --- | --- | --- |
| `TestSubjectHashUsesCompleteCanonicalSubject` | `ParseSubject` | `json.Unmarshal(&Subject{})` | 1 (missing `tree_digest`) |
| `TestAdmissionAndAdapterCapabilitiesRejectUnknownFieldsAndEnums` | `ParseAdmission`, `ParseAdapterCapabilities` | `ParseAdmissionInput`, `json.Unmarshal(&RuntimeCapabilities{})` | 8 admission + 4 adapter |
| `TestPlanningAdmissionUsesNullableWorkItemIdentity` | `ParseAdmission` | `ParseAdmissionInput` | 4 |
| `TestAdmissionSessionModeBindsRequestedSessionID` | `ParseAdmission` | `ParseAdmissionInput` | 8 |
| `TestAdmissionProviderScopeIsFailClosedAndPreservesCanonicalGrants` | `ParseAdmission` | `ParseAdmissionInput` | 8 |
| `TestNormalizedResultsRejectUnknownEnumsAndValidateSafeUsage` | `ParseUsage` | `json.Unmarshal(&Usage{})` | 3 + 8 usage |
| `TestEvidenceCanonicalSubjectSourceAndPredicateConsistency` | `ParseEvidence` | `json.Unmarshal(&Evidence{})` | 5 |
| `TestGoalTransportParsesContractFixtures` | `ValidateGoalEnvelope` | seven-oracle dispatch | 85 + 6 semantic |
| `TestEvidenceReviewPayloadVerdictMustMatchEvidenceVerdict` | `ParseEvidence` | `json.Unmarshal(&Evidence{})` | 2 |
| `TestGoalEnvelopeRejectsDuplicateContextPredicateIDs` | `ValidateGoalEnvelope` | `DecodeContextSnapshot` | 1 |
| `TestAdmissionProviderScopeBindsOperationsToItsResources` | `ValidateGoalEnvelope` | `ParseAdmissionInput` | 2 |
| `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` | `ParseEvidenceBatch` | `RejectDuplicateJSONMembers` | 2 |
| `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` (first half) | `CanonicalizeJSON` (live) | unchanged | 1 |
| `goalEvidence`/`goalUsage` fixture builders (`control/goal_client_test.go:1062,1071`) | `ParseEvidence`, `ParseUsage` | `json.Unmarshal` into `protocol.Evidence`/`protocol.Usage` | 2 |
| `app/cleanup_test.go:892`, `app/goal_delivery_test.go:2097`, `state/goal_delivery_test.go:1088`, `state/linux_clone_journal_test.go:1307`, `workspace/linux_clone_linux_test.go:1357` | `ParseEvidence` | `json.Unmarshal` into `protocol.Evidence` | 5 |

Dropped with a per-case row and a mutation run (the base assertion, its HEAD equivalent, and why the
wire property stays covered). The recurring HEAD equivalent: the deleted rules were never on a
`cmd/...`-reachable path, and the control plane enforces the same properties —
`control/lib/symmetry_control/goals/contract_validation.ex`'s per-envelope validators (run against
the **same** manifest by `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs`,
"public Elixir validators agree with every manifest semantic expectation") and the mirrored
`contracts/ts/semantics.ts` validators behind `Contracts.<Envelope>`.

| # | dropped case (base) | rule | HEAD equivalent |
| --- | --- | --- | --- |
| D1 | `TestGoalRevisionContractSemanticsStayAtProtocolBoundary` — 3 operator-fence-accepted cases | deterministic acceptance may include `review` only behind the operator fence — acceptance side | control/TS `validateGoalRevisionSemantics`; the three fixtures stay schema-valid in the manifest suite |
| D2 | same function — 3 `deterministic-review` rejection cases | deterministic acceptance excludes non-machine predicates | deleted only daemon-side; `ControlValidation.validate_goal_{revision,create,command}` + `contracts/ts/semantics.ts:186` enforce it |
| D3 | `TestParseEvidenceBatchValidatesBatchIdentityAndItemSemantics` | item `run_id` equals batch `run_id`; one item minimum; ≤256 items | `ContractValidation.validate_evidence_batch` (manifest-pinned); `EvidenceBatchSchemaVersion`/count rules deletable daemon-side |
| D4 | `TestParseEvidenceBatchRejectsDuplicateKeysAndIDs` | duplicate `evidence_key`/`evidence_id` within a batch | same |
| D5 | `TestParseEvidenceBatchRejectsItemRunIDMismatch` | item/batch run identity (mutation row) | same |
| D6 | `TestParseEvidenceBatchResponseValidatesReceipts` | receipt dispositions `created`/`replayed`, ordered receipts | `validate_evidence_batch_response` (schema-only for `minItems`/disposition); the **request-correlated** live counterpart is `control/validation.go:852 validateGoalEvidenceBatchReceipt` (unchanged, receipt count/run-id/disposition/observed-at vs the submitted evidence) |
| D7 | `TestParseEvidenceBatchResponseRejectsMismatchedRunAndDuplicateIdentities` — 3 subtests | receipt `run_id` equality; duplicate receipt `id`/`evidence_key` | `run_id` equality: `validate_evidence_batch_response` + the live by-index path `control/validation.go:852`; duplicate `id`/`evidence_key`: **no remaining enforcement** — the response schema carries no `uniqueItems` and the Elixir semantic clause checks only `run_id`; recorded in `docs/TODO.md` |
| D8 | `TestParseEvidenceBatchConflictDetailsValidatesItems` | index `0..255`, fixed `conflict` disposition | `validate_evidence_batch_conflict_details` (schema-only: index bounds, `const conflict`, item count) |
| D9 | `TestParseEvidenceBatchConflictDetailsRejectsDuplicateIndexesAndKeys` — 2 subtests | duplicate index / duplicate evidence key | **no remaining enforcement** — the conflict-details schema carries no `uniqueItems` and the validator has no semantic clause behind it; recorded in `docs/TODO.md` |
| D10 | `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` second half | the daemon recomputes `proposal_hash` with the omitted-`integration` default before admitting an `accept_plan` command | deleted with the validator (`"proposal_hash does not match canonical proposal"`); control/TS `validateGoalCommandSemantics` enforces it; the first half (canonical bytes + Jason digest) stays and keeps its name |
| D11 | `TestGoalTransportParsesContractFixtures` — semantic assertion for the 15 fixtures whose manifest `semantic_valid` differs from `schema_valid` | `decision` 5, `goal-command` 6, `goal-revision` 2, `goal-create` 1, `evidence-batch` 1 | the schema assertion for the same 248 fixtures is unchanged; the Elixir manifest test pins the semantic column of all 248 |

For D11 the other 85 fixtures of the seven remaining no-oracle envelopes keep coverage (100 fixtures
in those envelopes − the 15 semantic-only rows): those 85 rows carry neither `semantic_valid` nor
`schema_valid`, so both expectations fall back to `valid`, which the unchanged schema assertion
already pins (recorded, not silently dropped). The 2 `plan-proposal` rows left D11 when the apply
stage restored their oracle (finding 1) and are mutation-checked there.

## Evidence plan (all foreground; one heavy job at a time)

Order matters on this host: the `deadcode` runs and the test suites never run concurrently.

1. Before/after `deadcode ./cmd/...` for windows/amd64 and linux/amd64 from `daemon/`, plus the
   `-test ./...` classification leg; before counts here, after counts recorded with the exact
   deleted-set delta and a no-new-finding check (`diff` of the after output against the before
   output minus the deleted rows).
2. `gofmt -l internal/protocol`; `go vet ./...` (host); `GOOS=windows GOOS=linux go build ./...`;
   `GOOS=linux GOARCH=amd64 go build ./...`; `GOOS=linux GOARCH=arm64 go build ./...`.
3. `go test -count=1 ./internal/protocol/ ./internal/app/ ./internal/state/ ./internal/control/
   ./internal/harness/... ./internal/workspace/` (Windows-local, no `-race`: no cgo on this host).
4. WSL `go test -count=1 ./internal/state/` for the linux-tagged retargeted file
   (`linux_clone_journal_test.go`); WSL `go test -count=1 ./internal/workspace/` likewise.
5. Deleted-name greps both ways: `git grep -nw <name>` for all 26 (no reference left outside this
   slice's own docs and the archived citation) and a caller-loss check for the five production entry
   points (their caller sets unchanged).
6. `git diff --check`; `git diff --numstat` for the report-only LOC delta.
7. Mutants in throwaway copies outside the worktree (`git worktree add`, removed afterwards): the
   retargeted suites are copied onto the **pre-deletion** bytes and must be green there (the
   property did not migrate), then each mutant flips the guard its case pins and the retargeted case
   must redden, with the exact failure line recorded: M1 `ProviderScope.Validate` scoped-resource
   check → admission provider-scope case; M2 `Usage.Validate` cost coupling → usage case;
   M3 `validateContextSnapshotPredicateIDs` → context-snapshot duplicate-predicate case;
   M4 `Evidence.Validate` source-hash check → evidence consistency case; M5 `Subject.UnmarshalJSON`
   required members → subject case; M6 `scanJSONValue` duplicate detection →
   `RejectDuplicateJSONMembers` case; M7 `TaskResult.Validate` subject-hash equality → the
   fixture-suite `task-result.subject-hash-mismatch` subtest (semantic arm only).
   Dropped-case mutations (D1-D11) are run on the same pre-deletion tree to record each case's red
   baseline before its oracle is deleted: batch identity/duplicate rules (one mutant of
   `EvidenceBatch.UnmarshalJSON`), receipt-set rules, conflict-item rules, the deterministic-acceptance
   predicate rule, the proposal-hash rule, and the manifest semantic validators for the D11 row set
   (17 rows at the base, 15 after the apply stage's finding 1 restored the `plan-proposal` arm; the
   manifest has 33 rows carrying `semantic_valid` in total, 32 of which differ from `schema_valid`).
8. `python scripts/ci/test-check-citation-existence.py` (the gate script's own unit suite) and a
   citation scan over the audited corpus for the 26 names (measured: one hit,
   `docs/archive/CLOSED-TODO.md:3640` — a document this PR does not modify, and the gate reads only
   modified documents; the corpus is `docs/runbooks/**`, `docs/design/quality.md`,
   `docs/archive/CLOSED-TODO.md`).

## Design trade-offs

1. **Retarget onto retained DTO decoders rather than dropping 85 fixture cases.** D5 prefers
   re-expressing a case over dropping it, and the retained `UnmarshalJSON` of `Subject`, `Evidence`,
   `Usage`, `RuntimeCapabilities` is the same enforcement point the deleted wrapper delegated to —
   proven live by `deadcode` and by the production readers that decode those DTOs from the journal
   and the registration response. The alternative (drop the semantic arm for those envelopes) would
   have discarded the only daemon-side pin of, for example, the evidence subject-hash rule while the
   rule itself stays live.
2. **Fixture dispatch fails closed on an unknown envelope** (`t.Fatalf`) instead of falling back to
   a schema-only arm: a new manifest envelope must be given an oracle or an explicit no-oracle row
   rather than silently losing its semantic arm.
3. **Drop the no-oracle envelopes' semantic arm, keep the schema arm — seven envelopes after the
   apply stage restored `plan-proposal` (finding 1).** The alternatives were to keep the deleted
   validators alive behind a test-only shim (forbidden: a deleted symbol with a surviving reference,
   and D5's whole point), or to stop asserting the semantic column for those fixtures (chosen, with
   per-fixture rows and the control-plane manifest test as the retained pin).
4. **Keep exported constants and DTO structs whose only *function* consumers are deleted**
   (`EvidenceBatchSchemaVersion`, the batch response DTOs, `EvidenceReceiptDisposition`): they are
   the wire surface clause 3 protects, and `deadcode` reports functions, not declarations. Private
   orphaned declarations (`evidenceBatchWire`, the five response wire types, `evidenceBatchMaxItems`)
   are deleted with their functions, applied only where grep shows zero remaining references.
5. **No new tests.** A deletion has no new behaviour to pin; the retargeted cases are the pins, and
   the mutation runs are the proof that they still catch their guards.
6. **`goal_subject_test.go` added to the retarget set** (contract enumeration gap, §"Owned-set
   correction"): leaving it out would make rows 6/7 undeletable under the two-proof rule, which
   contradicts D5 and the deadness criterion the same contract binds.

## Out of scope (with reason)

- **F-06's second half** (`decodeStrictObject` returning the member map once, killing the second
  parse): unchanged, recorded here as an unowned finding, not silently dropped.
- `contracts/**` and the fixture JSON corpus: clause 4. The conductor's REV-01 amendment extends the
  owned set by `contracts/README.md`'s one sentence that cited the deleted `ValidateGoalEnvelope`,
  rewritten by the fix stage toward the retained production entry points.
- `protocol.go`, `e2e/internal/operator/**`, the app/state/control/harness production files,
  `goal_validation.go`'s live validators, `decodeStrictObject` and the live `UnmarshalJSON` family:
  clause 4; `protocol.go` additionally verified to hold no legacy decoder (it leaves the owned set).
- Other deadcode families of the same report (`state/goal_session.go` `Store.*` rows,
  `control` items, `state/goal_delivery.go QueueGoalEvidence`, `platform` windows-only rows):
  other slices; `docs/TODO.md` already tracks the W1-06 residue, and this plan adds no entry
  because this slice closes none of those and creates none.
- The arm64 `go vet` failure in `state/linux_clone_journal_test.go` (pre-existing, tracked in
  `docs/TODO.md` "Build-tag parity (arm64 oracle)"): the retargeted line in that file is unrelated
  to it; the arm64 **build** gate is still run clean.

## Residual risks and limits

- **One prose citation of a deleted symbol outlives the deletion, by design.**
  `docs/archive/CLOSED-TODO.md:3640` names `goal_contract_semantics.go` +
  `canonicalPlanProposalSHA256` as the pre-admission hash recompute: a historical record of a past
  fix, in a document outside this slice's owned set (clause 4) that this revision does not modify.
  The citation gate audits only documents the revision modifies, so no gate reddens; the prose is
  carried in the PR body rather than rewritten. The other citation, `contracts/README.md:217`'s
  `protocol.ValidateGoalEnvelope`, was rewritten to the retained production entry points by the fix
  stage under the conductor's REV-01 amendment.
- **`deadcode` reachability is RTA from `./cmd/...`** with the tool's documented caveats (no
  `//go:linkname` aliasing, single GOOS/GOARCH per run, generated files excluded); the second
  strategy is the whole-repo grep, and the `-test ./...` leg classifies the residual references as
  test-only. Any disagreement kept the symbol (none occurred).
- **Windows host limits**: no local `-race` (no cgo) and no Linux-only suite execution outside WSL;
  the CI race job remains the conductor's oracle.
- **Linux-tagged retargeted files** (`state/linux_clone_journal_test.go`,
  `workspace/linux_clone_linux_test.go`) cannot run on the host: the WSL gate is the only local
  evidence for them, and if WSL's toolchain is unavailable the gate is reported missing, never
  assumed passed.

## Outcome on the final tree (2026-11-22)

- `deadcode` delta, measured after the change: windows/amd64 118 → 92, linux/amd64 227 → 201,
  linux/arm64 179 → 153 — exactly the 26 rows of §2 and nothing else (`diff` of the before/after
  outputs: 26 removed findings, 0 added, on both GOOS). The fixpoint produced no newly dead helper,
  so `goal_validation.go` needed no edit.
- Retargeted surfaces: the 248-fixture dispatch plus every retargeted case is green at HEAD and was
  run green against the pre-deletion bytes (five packages); 7 retargeted-case mutants and 12
  dropped-rule mutants reddened the named cases, with Dm7 measured in both trees (live-path rows red,
  dropped-envelope rows green — the D11 coverage loss, measured rather than asserted). The apply stage
  restored the `plan-proposal` semantic arm (simplify finding 1) and re-measured Dm7 in the delivered
  tree: `plan-proposal.same-change-branch` and `plan-proposal.duplicate-predicate-id` now redden with
  their mutants, leaving 15 semantic-only fixture rows dropped (D11).
- Gates: `gofmt -l .` clean, `go vet ./...` 0, the four builds 0, the foreground package gate 0
  (13 packages `ok` at the implement run, 14 at the apply-stage re-run — `harness/linejson`),
  WSL `./internal/state/ ./internal/workspace/` ok, `git diff --check` clean, deleted-name grep
  clean, no live caller lost. LOC delta −935 net (report-only).
- `goal_subject_test.go` joined the retarget set as the contract-enumeration correction predicted;
  `goal_admission_input_test.go` needed no edit; `protocol.go` stayed unedited and left the owned set.
- Fix round (2026-11-22, review REV-01..REV-06): the `contracts/README.md` sentence was rewritten to
  the retained production entry points (conductor-owned-set amendment); the D7/D9 drop rows now
  record that the duplicate receipt-id/evidence_key and conflict index/evidence_key rules have no
  remaining enforcement, with the new `docs/TODO.md` entry (owner: contracts strictness (A6)); the
  D6/D8 "+ TS" attribution was dropped; `protocol.go`'s 649 L baseline and the D11 row count are
  restated; the frontend pin coordinate is `goal-attention.test.tsx:1238`; the residual-risk record
  above holds. Gate re-run and fix-stage delta in
  `docs/evidence/rf-w6-08-protocol-dead-envelope-2026-11-22.md` §12.
