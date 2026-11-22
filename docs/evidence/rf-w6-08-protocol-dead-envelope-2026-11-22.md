# rf-w6-08 protocol dead envelope family — evidence

Contract: `goal-rf-w6-08-protocol-dead-envelope-family.md` (GDD). Plan:
`docs/plans/2026-11-22-rf-w6-08-protocol-dead-envelope-plan.md`. Baseline:
`e84973a76abd46125cad08ed76710cec446388f4` (this worktree's HEAD, origin/main). Every coordinate
below was measured at that revision or at the change recorded beside it.

## 1. Tooling and method

```
deadcode: the mise-managed Go 1.27.4 toolchain's bin directory (`deadcode.exe`)
          golang.org/x/tools v0.53.0, built by go1.27.4 windows/amd64 (go version -m)
runs (from daemon/):
  deadcode ./cmd/...                          # GOOS=windows GOARCH=amd64 (host)
  GOOS=linux  GOARCH=amd64 deadcode ./cmd/...
  GOOS=linux  GOARCH=arm64 deadcode ./cmd/...
  deadcode -test ./...                        # test binaries as roots (classification leg)
```

Second strategy: `git grep -nw <symbol>` over the whole repository (production, tests, `daemon/e2e`,
`daemon/native`, witness files, docs, `contracts/`). Citation leg: `scripts/ci/
check-citation-existence.py`'s audited corpus (`docs/runbooks/**`, `docs/design/quality.md`,
`docs/archive/CLOSED-TODO.md`) scanned for all 26 names.

## 2. Two proofs per symbol (both held before any deletion)

Every row below is present, byte-identically, in the windows/amd64, linux/amd64 and linux/arm64
`deadcode ./cmd/...` findings at `e84973a7` (line numbers as printed by the tool at the base
revision; they equal the contract's draft coordinates). "References beyond" lists every file with a
`git grep -nw` match other than the defining file.

| # | symbol (file:line) | references beyond the definition | disposition |
| --- | --- | --- | --- |
| 1 | `ParseAdapterCapabilities` `goal_admission.go:782` | `goal_schema.go:211` (deleted caller), `goal_transport_test.go:98,107` | deleted |
| 2 | `validateGoalCreatePredicateIDs` `goal_contract_semantics.go:25` | `goal_schema.go:182` (deleted caller) | deleted |
| 3 | `validateGoalCommandPredicateIDs` `:52` | `goal_schema.go:102` (deleted), same-file deleted callers | deleted |
| 4 | `validateGoalRevisionContractSemantics` `:144` | `goal_schema.go:82` (deleted), same-file deleted callers; the `contracts/ts/semantics.ts:186` match is a distinct TS symbol | deleted |
| 5 | `canonicalSHA256` `:184` | same-file deleted caller only | deleted |
| 6 | `canonicalPlanProposalSHA256` `:197` | `goal_subject_test.go:207,220` (retargeted); `docs/archive/CLOSED-TODO.md:3640` prose | deleted |
| 7 | `normalizePlanProposal` `:205` | same-file deleted caller; `goal_subject_test.go:204` comment | deleted |
| 8 | `EvidenceBatch.UnmarshalJSON` `goal_evidence_batch.go:31` | `goal_evidence_batch_test.go` (dropped cases), `goal_schema.go:226` (deleted) | deleted |
| 9 | `ParseEvidenceBatch` `:82` | `goal_evidence_batch_test.go:14,31,60,77,88` (retargeted at `:88`, dropped elsewhere), `goal_schema.go:226` | deleted |
| 10 | `EvidenceBatchResponse.UnmarshalJSON` `goal_evidence_batch_response.go:72` | `goal_evidence_batch_response_test.go` (dropped), `goal_schema.go:229` (deleted) | deleted |
| 11 | `EvidenceBatchReceiptSet.UnmarshalJSON` `:81` | same-file dropped suite only | deleted |
| 12 | `EvidenceBatchReceipt.UnmarshalJSON` `:124` | same-file dropped suite only | deleted |
| 13 | `EvidenceBatchReceipt.Validate` `:139` | same-file deleted caller (`:103`), dropped suite | deleted |
| 14 | `EvidenceBatchConflictDetails.UnmarshalJSON` `:171` | dropped suite, `goal_schema.go:232` (deleted) | deleted |
| 15 | `EvidenceBatchConflictItem.UnmarshalJSON` `:207` | dropped suite only | deleted |
| 16 | `EvidenceBatchConflictItem.Validate` `:221` | same-file deleted callers (`:189`, `:213`), dropped suite | deleted |
| 17 | `ParseEvidenceBatchResponse` `:239` | dropped suite, `goal_schema.go:229` | deleted |
| 18 | `ParseEvidenceBatchConflictDetails` `:253` | dropped suite, `goal_schema.go:232` | deleted |
| 19 | `ParseEvidence` `goal_results.go:1010` | `goal_evidence_batch.go:58` (deleted), `goal_schema.go:223` (deleted), `goal_contract_fixture_test.go:111,115`, `goal_transport_test.go:454,457,523,533`, `control/goal_client_test.go:1062`, `app/cleanup_test.go:892`, `app/goal_delivery_test.go:2097`, `state/goal_delivery_test.go:1088`, `state/linux_clone_journal_test.go:1307`, `workspace/linux_clone_linux_test.go:1357` (all retargeted) | deleted |
| 20 | `ParseUsage` `goal_results.go:1124` | `goal_schema.go:247` (deleted), `goal_transport_test.go:382,386,438`, `control/goal_client_test.go:1071` (retargeted) | deleted |
| 21 | `DecodeGoalRevision` `goal_schema.go:73` | `goal_schema.go:239` (deleted caller) | deleted |
| 22 | `ValidateGoalCommand` `:93` | `goal_schema.go:235` (deleted caller) | deleted |
| 23 | `DecodeDecision` `:146` | `goal_schema.go:220` (deleted caller) | deleted |
| 24 | `ValidateGoalCreate` `:173` | `goal_schema.go:237` (deleted caller) | deleted |
| 25 | `ValidateGoalEnvelope` `:208` | `goal_contract_fixture_test.go:70`, `goal_contract_semantics_test.go:49,51,54,61,66,72,89` (retargeted/dropped), `contracts/README.md:217` prose | deleted |
| 26 | `ParseSubject` `goal_subject.go:91` | `goal_transport_test.go:71` (retargeted) | deleted |

Zero production callers outside `daemon/internal/protocol/` for all 26 (`git grep -nw`, non-test: the
only non-test matches are the defining files). Zero `daemon/e2e/**`, `daemon/native/**` or witness
matches (the one apparent e2e hit, `e2e/internal/operator/client.go:60,426`, names the *contracts*
DTO `contracts.SymmetryEvidenceBatchConflictDetailsV1`).

Test-only classification (the contract's `-test` leg): `deadcode -test ./...` — with test binaries
as roots the reference count for every candidate disappears (windows 23 findings / linux 0 findings,
**none** of the 26), i.e. every reference to the family was test-only or inside a deleted caller,
and `deadcode -test ./cmd/...` is byte-identical to the plain run because `./cmd/...` has no test
files.

Citation leg: no audited-corpus document names any of the 26 (`git grep -nw` over
`docs/runbooks/**`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`). Two out-of-corpus
prose citations exist and are recorded, not repaired (clause 4): `contracts/README.md:217`
(`protocol.ValidateGoalEnvelope`) and `docs/archive/CLOSED-TODO.md:3640` (a historical closure naming
`goal_contract_semantics.go` + `canonicalPlanProposalSHA256`).

## 3. Deadcode before/after (measured, not asserted)

| target | before | after | delta |
| --- | --- | --- | --- |
| windows/amd64 `deadcode ./cmd/...` | 118 | 92 | −26 |
| linux/amd64 `deadcode ./cmd/...` | 227 | 201 | −26 |
| linux/arm64 `deadcode ./cmd/...` | 179 | 153 | −26 |
| windows/amd64 `deadcode -test ./...` | 23 | 23 | 0 (classification leg, not a count gate) |
| linux/amd64 `deadcode -test ./...` | 0 | 0 | 0 |

Line-diff of the before/after outputs: **26 removed findings, 0 added** on both GOOS, and the removed
set is exactly the 26 names of §2 (`diff before after | grep '^<' | sed 's/.*unreachable func: //'`).
No new unreachable function appeared anywhere; the family's post-deletion fixpoint is the 26 itself,
so no additional helper function was deleted. The contract's draft counts (250 linux / 140 windows)
were measured at `0fa9b705`; the re-measured base is 227/118.

## 4. Deleted set and orphaned declarations

Deleted (26 functions): the five envelope decoders/validators (`DecodeGoalRevision`,
`ValidateGoalCommand`, `DecodeDecision`, `ValidateGoalCreate`, `ValidateGoalEnvelope`), the six
private contract-semantics helpers of `goal_contract_semantics.go`, the two normalised-payload
parsers (`ParseEvidence`, `ParseUsage`), `ParseSubject`, `ParseAdapterCapabilities`, the
evidence-batch family (`ParseEvidenceBatch`, `ParseEvidenceBatchResponse`,
`ParseEvidenceBatchConflictDetails` and the eight `UnmarshalJSON`/`Validate` methods of the batch
response DTOs).

Orphaned private declarations deleted with their functions (each verified at zero remaining
references by `git grep`): `evidenceBatchWire`, `evidenceBatchMaxItems`,
`evidenceBatchResponseWire`, `evidenceBatchReceiptSetWire`, `evidenceBatchReceiptWire`,
`evidenceBatchConflictDetailsWire`, `evidenceBatchConflictItemWire`. Dead imports removed:
`contractdto` (batch, batch response), `crypto/sha256` (contract semantics).

Retained on purpose (recorded, not residue to hide): `EvidenceBatchSchemaVersion`, the batch
response DTO structs and `EvidenceReceiptDisposition` with its constants. They are the exported wire
surface clause 3 freezes; `deadcode` reports functions, not declarations, and the owned-set rule is
"the func and its now-orphaned private helpers, nothing else". `EvidenceBatchSchemaVersion` now has
no in-tree consumer — a future consumer of the retained `EvidenceBatch` DTO is the caller the
contract's D5 anticipates.

## 5. Base→HEAD audit per deleted function (conductor decision, lesson 32)

| deleted function(s) | base callers | rule enforced | production entry point with the same rule | HEAD equivalent |
| --- | --- | --- | --- | --- |
| `ValidateGoalEnvelope` | tests only (`goal_contract_fixture_test.go:70`, `goal_contract_semantics_test.go`) | envelope dispatch to the family members | n/a (it *was* the dispatch) | the retargeted fixture dispatch: `ParseAdmissionInput`, `ParseTaskResult`, `DecodeContextSnapshot`, the retained DTO decoders; per-envelope drops §7 |
| `DecodeGoalRevision` | `ValidateGoalEnvelope` | goal-revision schema + revision-contract semantics (duplicate predicate ids; deterministic acceptance rejects non-machine predicates unless the operator fence is set) | no daemon path decodes a goal-revision envelope | rule deleted daemon-side; `ContractValidation.validate_goal_revision/2` (`control/lib/symmetry_control/goals/contract_validation.ex:77`) + `contracts/ts/semantics.ts:186` enforce it against the same manifest |
| `ValidateGoalCreate` | `ValidateGoalEnvelope` | goal-create schema + the same revision-contract rule on `initial_revision` | none | deleted daemon-side; `validate_goal_create/2` (`:80`) + TS `validateGoalCreateSemantics` |
| `ValidateGoalCommand` | `ValidateGoalEnvelope` | goal-command schema + predicate ids + `request_plan` resource/subject identity + `accept_plan` `proposal_hash` recompute with the omitted-`integration` default | none (the operator client that would have called it moved to `e2e/internal/operator` by #178 and never called it) | deleted daemon-side; `validate_goal_command/2` (`:83`) + TS `validateGoalCommandSemantics`; the `request_plan` identity rule also survives for admissions via `subject.resource_id` validation |
| `DecodeDecision` | `ValidateGoalEnvelope` | decision schema + duplicate option ids + resolution/state coupling | none | deleted daemon-side; `validate_decision/2` + TS `validateDecisionSemantics` |
| `validateGoal*PredicateIDs`, `validateGoalRevisionContractSemantics`, `canonicalSHA256`, `canonicalPlanProposalSHA256`, `normalizePlanProposal` | the four validators above | the listed rules | none | deleted with their only callers (§2 rows 2-7) |
| `ParseEvidence` | tests only (all retargeted, §2 row 19) | evidence schema + `Evidence.Validate` (subject hash, source-ref kind/hash binding, payload-kind consistency, verdict vocabulary) | `Evidence.UnmarshalJSON` (retained, cmd-reachable: the journal reader decodes `protocol.Evidence` fields) — the same semantic layer minus the schema pre-check | retargeted cases call it directly; the schema arm is asserted separately in the fixture suite |
| `ParseUsage` | tests only | usage schema + `Usage.Validate` (cost_basis/cost_microusd coupling, `ValidateMicroUSD`, JSON-safe token bounds, UTC observed_at) | `Usage.UnmarshalJSON` (retained, cmd-reachable) | as above |
| `ParseAdapterCapabilities` | `ValidateGoalEnvelope`, tests | adapter-capabilities schema + `RuntimeCapabilities.Validate` (adapter cross-field checks) | `RuntimeCapabilities.UnmarshalJSON` (retained, cmd-reachable: the registration decode path, `protocol.go:191`) | as above |
| `ParseSubject` | tests only | `Subject.UnmarshalJSON` (strict object, required `resource_id`/`commit`/`tree_digest`, `Validate`) | `Subject.UnmarshalJSON` itself | the case calls the retained decoder |
| `ParseEvidenceBatch` | `ValidateGoalEnvelope`, tests | duplicate-member scan + batch schema + item run-identity/duplicate rules | `RejectDuplicateJSONMembers` (retained, production-used by `ParseAdmissionInput` and `control/validation.go`) for the duplicate-member rule only; the identity rules had no production caller | duplicate-member case retargeted; identity rules dropped with rows (§7 D3-D5) |
| `ParseEvidenceBatchResponse`, `ParseEvidenceBatchConflictDetails` and the eight DTO methods | `ValidateGoalEnvelope`, tests | response/conflict schemas + receipt run-id/duplicate rules, index bounds, fixed `conflict` disposition | none; the request-correlated live counterpart is `control/validation.go:852 validateGoalEvidenceBatchReceipt` (receipt count, run-id vs path, disposition, observed_at, per-receipt identity against the submitted evidence) — unchanged | rules dropped with rows (§7 D6-D9) |

No deleted function had a caller that silently takes another path afterwards: every non-test caller
was inside this slice's deleted set, and the build plus the `deadcode` re-run confirm it.

## 6. Retarget table (cases preserved, oracle swapped)

| case (name unchanged) | base oracle | HEAD oracle | cases |
| --- | --- | --- | --- |
| `TestSubjectHashUsesCompleteCanonicalSubject` | `ParseSubject` | `json.Unmarshal(&Subject{})` | 1 |
| `TestAdmissionAndAdapterCapabilitiesRejectUnknownFieldsAndEnums` | `ParseAdmission`, `ParseAdapterCapabilities` | `ParseAdmissionInput`, `json.Unmarshal(&RuntimeCapabilities{})` | 12 |
| `TestPlanningAdmissionUsesNullableWorkItemIdentity` | `ParseAdmission` | `ParseAdmissionInput` | 4 |
| `TestAdmissionSessionModeBindsRequestedSessionID` | `ParseAdmission` | `ParseAdmissionInput` | 8 |
| `TestAdmissionProviderScopeIsFailClosedAndPreservesCanonicalGrants` | `ParseAdmission` | `ParseAdmissionInput` | 8 |
| `TestNormalizedResultsRejectUnknownEnumsAndValidateSafeUsage` | `ParseUsage` | `json.Unmarshal(&Usage{})` | 11 |
| `TestEvidenceCanonicalSubjectSourceAndPredicateConsistency` | `ParseEvidence` | `json.Unmarshal(&Evidence{})` | 5 |
| `TestGoalTransportParsesContractFixtures` | `ValidateGoalEnvelope` | six-oracle dispatch (below) | 85 semantic |
| `TestEvidenceReviewPayloadVerdictMustMatchEvidenceVerdict` | `ParseEvidence` | `json.Unmarshal(&Evidence{})` | 2 |
| `TestGoalEnvelopeRejectsDuplicateContextPredicateIDs` | `ValidateGoalEnvelope` | `DecodeContextSnapshot` | 1 |
| `TestAdmissionProviderScopeBindsOperationsToItsResources` | `ValidateGoalEnvelope` | `ParseAdmissionInput` | 2 |
| `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` | `ParseEvidenceBatch` | `RejectDuplicateJSONMembers` | 2 |
| `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` | `CanonicalizeJSON` + `canonicalPlanProposalSHA256` | `CanonicalizeJSON` only (second half dropped, §7 D10) | 1 kept |
| `goalEvidence`/`goalUsage` builders (`control/goal_client_test.go`) | `ParseEvidence`, `ParseUsage` | `json.Unmarshal` into `protocol.Evidence`/`protocol.Usage` | 2 |
| `app/cleanup_test.go`, `app/goal_delivery_test.go`, `state/goal_delivery_test.go`, `state/linux_clone_journal_test.go`, `workspace/linux_clone_linux_test.go` | `ParseEvidence` | `json.Unmarshal` into `protocol.Evidence` | 5 |

Fixture-suite dispatch (schema arm unchanged for all 248 fixtures; semantic arm per envelope):

| envelope | fixtures | HEAD oracle |
| --- | --- | --- |
| admission | 34 | `ParseAdmissionInput` |
| task-result | 18 | `ParseTaskResult` |
| context-snapshot | 8 | `DecodeContextSnapshot` |
| adapter-capabilities | 3 | `json.Unmarshal(&RuntimeCapabilities{})` |
| evidence | 10 | `json.Unmarshal(&Evidence{})` |
| usage | 12 | `json.Unmarshal(&Usage{})` |
| connections-read + 5 presentation reads | 47 | unchanged `DecodeSchema` arm |
| decision, evidence-batch, evidence-batch-response, evidence-batch-conflict-details, goal-command, goal-create, goal-revision, plan-proposal | 163 | none; schema arm only, 17 of them lose a semantic-only rule (§7 D11) |

Oracle equivalence was measured **before** the retarget: a throwaway in-package probe ran the six
oracles over `contracts/fixtures/manifest.json` and reproduced `semantic_valid` for 85/85 fixtures of
those envelopes with zero mismatches (`go test -run TestZZProbeRetargetOracles ./internal/protocol/`,
`ok ... 0.633s`; probe deleted afterwards).

Contract-enumeration corrections applied: `goal_subject_test.go` was added to the retarget set (it
carries the only test references to `canonicalPlanProposalSHA256`/`normalizePlanProposal`);
`goal_admission_input_test.go` carries no deleted reference and was left untouched; `protocol.go`
holds no legacy decoder at this head (#178 merged as `c171663b`) and leaves the owned set unedited.

## 7. Dropped cases (per-case rows)

Each row: base assertion → HEAD equivalent → why the wire property stays covered → mutation.

| # | dropped case | rule it pinned | HEAD equivalent | mutation |
| --- | --- | --- | --- | --- |
| D1 | `TestGoalRevisionContractSemanticsStayAtProtocolBoundary` — 3 operator-fence cases | deterministic acceptance accepts `review` only behind the operator fence; fixtures stay schema-valid | control/TS `validateGoalRevisionSemantics`; the 3 fixtures keep their schema-valid assertion in the manifest suite | see §9 Dm1 |
| D2 | same function — 3 `deterministic-review` rejections | deterministic acceptance rejects non-machine predicates without the fence | `ContractValidation.validate_goal_{revision,create,command}/2`, TS `semantics.ts:186`; Elixir manifest test pins all 248 | Dm1 |
| D3 | `TestParseEvidenceBatchValidatesBatchIdentityAndItemSemantics` | batch schema_version, one item minimum, item `run_id` equals batch `run_id` | `validate_evidence_batch/2` (manifest-pinned) | Dm2 |
| D4 | `TestParseEvidenceBatchRejectsDuplicateKeysAndIDs` | duplicate `evidence_key`/`evidence_id` within a batch | as D3 | Dm2 |
| D5 | `TestParseEvidenceBatchRejectsItemRunIDMismatch` | item/batch run identity | as D3 | Dm2 |
| D6 | `TestParseEvidenceBatchResponseValidatesReceipts` | ≥1 receipt, `created`/`replayed` dispositions, ordered receipts | `validate_evidence_batch_response/2` + TS; request-correlated live path `control/validation.go:852` (unchanged) | Dm3 |
| D7 | `TestParseEvidenceBatchResponseRejectsMismatchedRunAndDuplicateIdentities` — 3 subtests | receipt `run_id` equality, duplicate receipt `id`/`evidence_key` | as D6 | Dm3 |
| D8 | `TestParseEvidenceBatchConflictDetailsValidatesItems` | index `0..255`, fixed `conflict` disposition | `validate_evidence_batch_conflict_details/2` + TS | Dm4 |
| D9 | `TestParseEvidenceBatchConflictDetailsRejectsDuplicateIndexesAndKeys` — 2 subtests | duplicate index / duplicate evidence_key | as D8 | Dm4 |
| D10 | `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` — second half | the daemon recomputed `proposal_hash` with the `normalizePlanProposal` default before admitting an `accept_plan` command | deleted with `validateGoalCommandPredicateIDs`; TS `validateGoalCommandSemantics` + `validate_goal_command/2` enforce it; the first half (canonical bytes + Jason digest) stays | Dm5 |
| D11 | `TestGoalTransportParsesContractFixtures` — the semantic assertion for the 17 fixtures whose manifest `semantic_valid` ≠ `schema_valid` | `decision` 5 (`duplicate-option`, `missing-resolution`, `open-with-resolution`, `unknown-option`, `superseded-with-resolution`), `goal-command` 6 (`amend-deterministic-review`, `plan-same-change-branch`, `decision-plan-same-change-branch`, `proposal-hash-mismatch`, `request-plan-subject-resource-mismatch`, `unpaired-surrogate`), `goal-revision` 2 (`duplicate-predicate-id`, `deterministic-review`), `plan-proposal` 2 (`duplicate-predicate-id`, `same-change-branch`), `goal-create` 1 (`deterministic-review`), `evidence-batch` 1 (`duplicate-key`) | schema arm unchanged for all 248; Elixir `contract_fixture_manifest_test.exs` ("public Elixir validators agree with every manifest semantic expectation") pins the semantic column; the proposal rules stay live for a proposal embedded in a TaskResult (`validatePlanProposalPredicateIDs` via `TaskResult.Validate`) | Dm6/Dm7 |

The other 146 no-oracle fixtures keep their coverage: their `semantic_valid` equals `schema_valid`,
which the unchanged schema assertion pins. Test names were kept byte-identical, including
`TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` whose name now outlives the parser it was
named after (contract clause "no test name changes"); the retarget is visible in its body
(`RejectDuplicateJSONMembers`).

Test-name inventory (base vs HEAD, `^func Test…` sets per retargeted file): no name was renamed or
added in any file; the only removals are the fully dropped case sets —
`TestGoalRevisionContractSemanticsStayAtProtocolBoundary`,
`TestParseEvidenceBatchValidatesBatchIdentityAndItemSemantics`,
`TestParseEvidenceBatchRejectsDuplicateKeysAndIDs`,
`TestParseEvidenceBatchRejectsItemRunIDMismatch`, and the four functions of the deleted
`goal_evidence_batch_response_test.go`. Every other retargeted file's test-name set hashes
identically to the base.

## 8. Local gate results (all foreground, exit codes as observed)

| gate (from `daemon/` unless noted) | exit | output |
| --- | --- | --- |
| `gofmt -l .` | 0 | no files listed |
| `go vet ./...` (host windows/amd64) | 0 | no output |
| `go build ./...` | 0 | |
| `GOOS=windows go build ./...` | 0 | |
| `GOOS=linux GOARCH=amd64 go build ./...` | 0 | |
| `GOOS=linux GOARCH=arm64 go build ./...` | 0 | |
| `go test -count=1 ./internal/protocol/ ./internal/app/ ./internal/state/ ./internal/control/ ./internal/harness/... ./internal/workspace/` | 0 | 13 packages `ok` (protocol 2.230s, app 325.745s, state 9.961s, control 1.137s, harness 14.441s + 7 sub-packages, workspace 227.404s) |
| `deadcode ./cmd/...` windows/amd64 before → after | 0 | 118 → 92 |
| `GOOS=linux GOARCH=amd64 deadcode ./cmd/...` before → after | 0 | 227 → 201 |
| `GOOS=linux GOARCH=arm64 deadcode ./cmd/...` before → after | 0 | 179 → 153 |
| `deadcode -test ./...` both GOOS | 0 | 23/0 before and after (no candidate) |
| deleted-name `git grep -nw` over `daemon/**` | n/a | no reference left except the retained test name `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` |
| production-entry-point caller sets (base ⊆ HEAD, `git grep -lw`) | n/a | no live caller lost; the only removed references are inside deleted functions (`goal_schema.go`) or retargeted tests |
| `python scripts/ci/test-check-citation-existence.py` (the gate's own suite) | 0 | `Ran 49 tests … OK` (202.6 s) |
| citation scan over the audited corpus for the 26 names | n/a | one hit, `docs/archive/CLOSED-TODO.md:3640` — a document this revision does not modify, so the gate's range selection never reads it; carried in the PR body (clause 4) |
| `git diff --check` | 0 | clean |
| WSL `Ubuntu-24.04` `go test -count=1 ./internal/state/ ./internal/workspace/` (go1.27.0 linux/amd64) | 0 | `ok state 14.665s`, `ok workspace 58.894s` (the linux-tagged retargeted files run here) |

Local environment: Windows host, no cgo → no local `-race`; the CI race job is the conductor's slow
oracle and is not run here.

## 9. Mutation runs (per mutated guard: the case it must redden)

Retargeted cases, mutants applied to a scratch `git worktree` at `e84973a7` carrying the retargeted
test files; every row exited 1 with the named case red:

| mutant (flipped guard) | red case(s) observed |
| --- | --- |
| M1 `ProviderScope.Validate` scoped-resource membership pair | `TestAdmissionProviderScopeIsFailClosedAndPreservesCanonicalGrants/unscoped_operation_key` (`goal_transport_test.go:287`) |
| M2 `Usage.Validate` reported/estimated cost coupling | `TestNormalizedResultsRejectUnknownEnumsAndValidateSafeUsage/reported_cost_null` and `/estimated_cost_null` (retargeted loop) plus the retained `validate_*` siblings |
| M3 `acceptanceContract.validatePredicateIDs` duplicate id | `TestGoalEnvelopeRejectsDuplicateContextPredicateIDs` (`goal_contract_semantics_test.go:15`) |
| M4 `Evidence.Validate` `source_ref.subject_hash` binding | `TestEvidenceCanonicalSubjectSourceAndPredicateConsistency/source_hash_mismatch` (`goal_transport_test.go:534`) |
| M5 `Subject` required-member list + `Validate` `tree_digest` | `TestSubjectHashUsesCompleteCanonicalSubject` (`goal_transport_test.go:72`) |
| M6 `scanJSONValue` duplicate detection | `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` (both subtests) and `TestRejectDuplicateJSONMembersCanonicalizesEscapedNames` |
| M7 `TaskResult.Validate` subject-hash equality | fixture subtest `task-result.subject-hash-mismatch` (`goal_contract_fixture_test.go:94`) |

Dropped-case red baselines, mutants applied to a pristine pre-deletion worktree (base tests intact):

| mutant | red case(s) observed | covers |
| --- | --- | --- |
| Dm1 `validateGoalRevisionContractSemantics` non-machine predicate rule | the `goal-create`, `goal-command`, `goal-revision` `deterministic-review` fixture rows + the 3 rejection cases of `TestGoalRevisionContractSemanticsStayAtProtocolBoundary` (its 3 acceptance cases stayed green) | D1, D2 |
| Dm2 item `run_id` equality + duplicate `evidence_key`/`evidence_id` | `TestParseEvidenceBatchRejectsDuplicateKeysAndIDs`, `TestParseEvidenceBatchRejectsItemRunIDMismatch` | D4, D5 |
| Dm2b drop the item append | `TestParseEvidenceBatchValidatesBatchIdentityAndItemSemantics` (`items = 0, want 1`) | D3 |
| Dm3 receipt-set `Validate`/`run_id`/duplicate checks + append | `TestParseEvidenceBatchResponseValidatesReceipts` + all 3 subtests of `...RejectsMismatchedRunAndDuplicateIdentities` | D6, D7 |
| Dm4 conflict-details `Validate`/duplicate checks + append | `TestParseEvidenceBatchConflictDetailsValidatesItems` + both subtests of `...RejectsDuplicateIndexesAndKeys` | D8, D9 |
| Dm5 `normalizePlanProposal` `integration` default | the second half of `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` (`goal_subject_test.go:220`, digest mismatch) | D10 |
| Dm6 `DecodeDecision` semantic layer | all 5 decision fixture rows | D11 (decision) |
| Dm7 `validateProviderChangeTarget` equal-branch rule | pre-deletion tree: 5 fixture rows red; retargeted tree: `task-result.proposal-same-change-branch` and `context-snapshot.same-change-branch` red while `plan-proposal.same-change-branch`, `goal-command.plan-same-change-branch` and `goal-command.decision-plan-same-change-branch` stayed **green** — the D11 coverage loss, measured | D11 (change targets) |
| Dm8 `ValidateGoalCommand` `request_plan` identity rule | `goal-command.request-plan-subject-resource-mismatch` | D11 |
| Dm9 `contracts.DecodeSchema` unpaired-surrogate strictness | `goal-command.unpaired-surrogate` (that row only) | D11 |
| Dm10 `proposal_hash` equality | `goal-command.proposal-hash-mismatch` | D11 |
| Dm11 item-level duplicate `evidence_key` | `evidence-batch.duplicate-key` | D11 |

Rule attribution was measured before mutating: a throwaway probe (deleted afterwards) showed
`goal-command.unpaired-surrogate` is rejected by the *retained* `contracts.DecodeSchema` strict
raw-JSON check (`unpaired high surrogate in JSON string`; `ValidateSchema`,
`decodeGoalContractObject` and `validateGoalCommandPredicateIDs` all accepted it), while
`goal-command.proposal-hash-mismatch` is rejected by the deleted
`validateGoalCommandPredicateIDs` (`proposal_hash does not match canonical proposal`).

Retarget equivalence: the retargeted suite was also run unmodified against the pre-deletion bytes
(the five touched packages: `ok protocol 1.801s`, `ok control 1.373s`, `ok state 12.912s`,
`ok app 241.275s`, `ok workspace 186.956s`) — the cases assert properties the base source enforces.

## 10. LOC delta (report-only, D2) and residual risks

`git diff --shortstat`: 19 files changed, 82 insertions(+), 1018 deletions(-) — net **−936**. Per
file: `goal_evidence_batch_response.go −216`, `goal_evidence_batch_response_test.go −169`,
`goal_contract_semantics.go −148`, `goal_schema.go −148`, `goal_evidence_batch_test.go −82`,
`goal_evidence_batch.go −74`, `goal_contract_semantics_test.go −51`, `goal_subject_test.go −28`,
`goal_results.go −20`, `goal_admission.go −12`, `goal_subject.go −9`, `goal_transport_test.go`
±33 lines (oracle swaps), and the six cross-package fixture builders ±2-4 lines each. The
contract's −400..−900 estimate was report-only (D2); the measured net delta is −936.

Residual risks and known limits:

- The daemon no longer decodes a goal-command, goal-create, goal-revision, decision, plan-proposal or
  evidence-batch envelope at all. Those wire properties stay pinned on the control side
  (`control/lib/symmetry_control/goals/contract_validation.ex` + the Elixir manifest test over all
  248 fixtures, and TS `contracts/ts/semantics.ts`); a future daemon consumer calls the production
  parsers (D5) or re-adds a dropped rule with its own tests.
- `contracts/README.md:217` still names `protocol.ValidateGoalEnvelope`, and
  `docs/archive/CLOSED-TODO.md:3640` still names `goal_contract_semantics.go` +
  `canonicalPlanProposalSHA256`. Both documents are outside this slice's owned set; the citation
  gate audits only documents a revision modifies, and this revision modifies neither. Carried in the
  PR body instead of repaired outside the owned set.
- `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` keeps the name of the parser it was
  named after (the contract forbids test-name changes); its body now calls
  `RejectDuplicateJSONMembers`.
- `EvidenceBatchSchemaVersion` has no in-tree consumer; it is kept as part of the retained wire DTO
  surface (§4).
- `deadcode` reachability is RTA from `./cmd/...` with the tool's documented caveats (no
  `//go:linkname` aliasing, one GOOS/GOARCH per run, generated files excluded); the whole-repo grep
  and the `-test ./...` classification leg are the independent second strategy.
- Windows host: no local `-race`; Linux-only suites run under WSL (`Ubuntu-24.04`, go1.27.0) and the
  arm64 evidence is compile/analysis level only.
