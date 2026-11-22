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
| 25 | `ValidateGoalEnvelope` `:208` | `goal_contract_fixture_test.go:70`, `goal_contract_semantics_test.go:49,51,54,61,66,72,89` (retargeted/dropped), `contracts/README.md:217` prose (rewritten by the fix stage under the REV-01 amendment, §12) | deleted |
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

Citation leg: `git grep -nw` over the audited corpus (`docs/runbooks/**`, `docs/design/quality.md`,
`docs/archive/CLOSED-TODO.md`) finds one hit, `docs/archive/CLOSED-TODO.md:3640` (§8). The gate reads
only documents a revision modifies (`scripts/ci/check-citation-existence.py` →
`changed_paths(..., AUDITED_PATHS)`), and this revision modifies no audited document, so no citation
gate reddens. Two prose citations of deleted symbols existed at the deletion: `contracts/README.md:217`
(`protocol.ValidateGoalEnvelope`, outside the corpus) and the `CLOSED-TODO.md:3640` closure (a
historical record of a past fix, in the corpus but unmodified, naming `goal_contract_semantics.go` +
`canonicalPlanProposalSHA256`). The fix stage rewrote the `contracts/README.md` sentence to name the
retained production entry points under the conductor's REV-01 amendment (§12); the archive closure
stays as the historical record it is.

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
were measured at `0fa9b705`; the re-measured base is 227/118. The apply stage re-measured the three
after counts at the delivered tree — 92 / 201 / 153, unchanged — and re-checked that none of the
deleted names appears in any of the findings lists (§11).

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

Deleted exported declarations with zero remaining references (conductor amendment for review
round 1, P3-3): `EvidenceBatchSchemaVersion` and `EvidenceBatch` (`goal_evidence_batch.go`), and
`EvidenceReceiptDisposition` with its constants `EvidenceReceiptCreated`/`EvidenceReceiptReplayed`,
`EvidenceBatchResponse`, `EvidenceBatchReceiptSet`, `EvidenceBatchReceipt`,
`EvidenceBatchConflictDetails` and `EvidenceBatchConflictItem` (`goal_evidence_batch_response.go`,
deleted whole). Zero-reference proof before deletion: `git grep -nw` over the whole repository hits
only the declaring files, the docs corrected here, and homonyms — the TS exports and
`contracts.EvidenceBatch*` generated DTOs, and the `EvidenceBatchConflictDetails` field in
`control`/`e2e` whose type is `*contracts.SymmetryEvidenceBatchConflictDetailsV1`; the qualified
grep for `protocol.EvidenceBatch` is empty. `go build` and `go vet` on windows, linux/amd64 and
linux/arm64 pass unchanged, and `deadcode` reports functions, not declarations, so these
declarations never appeared in its output. The live wire DTOs are `contracts.*` and
`control.GoalEvidenceBatch*`.

## 5. Base→HEAD audit per deleted function (conductor decision, lesson 32)

| deleted function(s) | base callers | rule enforced | production entry point with the same rule | HEAD equivalent |
| --- | --- | --- | --- | --- |
| `ValidateGoalEnvelope` | tests only (`goal_contract_fixture_test.go:70`, `goal_contract_semantics_test.go`) | envelope dispatch to the family members | n/a (it *was* the dispatch) | the retargeted fixture dispatch: `ParseAdmissionInput`, `ParseTaskResult`, `DecodeContextSnapshot`, `ValidatePlanProposal` (the plan-proposal envelope's retained live validator, restored by the apply stage's finding 1), the retained DTO decoders; per-envelope drops §7 |
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
| `ParseEvidenceBatchResponse`, `ParseEvidenceBatchConflictDetails` and the eight DTO methods | `ValidateGoalEnvelope`, tests | response/conflict schemas + receipt run-id/duplicate rules, index bounds, fixed `conflict` disposition | no daemon-side response-envelope validator; the request-correlated live counterpart `validateGoalEvidenceBatchReceipt` (receipt count, run-id vs path, disposition, observed_at, per-receipt identity against the submitted evidence) rejects duplicate receipt `id`/`evidence_key` transitively against the uniqueness-validated request — unchanged | rules dropped with rows (§7 D6-D9); the conflict-details duplicate rules have no remaining equivalent |

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
| `TestGoalTransportParsesContractFixtures` | `ValidateGoalEnvelope` | seven-oracle dispatch (below) | 85 + 6 semantic |
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
| plan-proposal | 6 | `ValidatePlanProposal` (restored by the apply stage, finding 1) |
| connections-read + the six presentation reads | 57 | unchanged `DecodeSchema` arm |
| decision, evidence-batch, evidence-batch-response, evidence-batch-conflict-details, goal-command, goal-create, goal-revision | 100 | none; schema arm only, 15 of them lose a semantic-only rule (§7 D11) |

Oracle equivalence was measured **before** the retarget: a throwaway in-package probe ran the six
oracles over `contracts/fixtures/manifest.json` and reproduced `semantic_valid` for 85/85 fixtures of
those envelopes with zero mismatches (`go test -run TestZZProbeRetargetOracles ./internal/protocol/`,
`ok ... 0.633s`; probe deleted afterwards). The `plan-proposal` arm is outside that probe: the
deleted `ValidateGoalEnvelope` dispatched this envelope to exactly `return ValidatePlanProposal(data)`
(base `goal_schema.go`), the function is retained and cmd-reachable through `TaskResult.Validate`
(`goal_results.go:343`), and both restored semantic-only rows (`duplicate-predicate-id`,
`same-change-branch`) are mutation-checked (§9 Dm7, Dm12); the pristine dispatch left them silently
unasserted (measured, §9 Dm7).

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
| D6 | `TestParseEvidenceBatchResponseValidatesReceipts` | ≥1 receipt, `created`/`replayed` dispositions, ordered receipts | `validate_evidence_batch_response/2` (manifest-pinned; the response schema pins `minItems` and the disposition enum); request-correlated live path `control/validation.go:852` (unchanged) | Dm3 |
| D7 | `TestParseEvidenceBatchResponseRejectsMismatchedRunAndDuplicateIdentities` — 3 subtests | receipt `run_id` equality, duplicate receipt `id`/`evidence_key` | receipt `run_id` equality: `validate_evidence_batch_response_semantics` (Elixir) and the live by-index path `validateGoalEvidenceBatchReceipt`; duplicate receipt `id`/`evidence_key`: rejected transitively on the daemon's only consuming path — `validateGoalEvidenceBatch` rejects duplicate request `evidence_id`/`evidence_key` (its `seenIDs`/`seenKeys` maps) and `validateGoalEvidenceReceipt` compares each receipt `id`/`evidence_key` per index against that uniqueness-validated request, so a duplicate can only match a request the daemon refuses. No producer-side or schema enforcement (the Elixir response clause checks only `run_id`; no `uniqueItems` in the response schema); recorded in `docs/TODO.md` | Dm3 |
| D8 | `TestParseEvidenceBatchConflictDetailsValidatesItems` | index `0..255`, fixed `conflict` disposition | `validate_evidence_batch_conflict_details/2` (schema-only, manifest-pinned: index bounds, `const conflict`, item count) | Dm4 |
| D9 | `TestParseEvidenceBatchConflictDetailsRejectsDuplicateIndexesAndKeys` — 2 subtests | duplicate index / duplicate evidence_key | **no remaining enforcement** — the conflict-details schema carries no `uniqueItems` and has no semantic clause behind it (`validate_semantics/2` falls through to `:ok`), and no validator on any plane checks the duplicates; recorded in `docs/TODO.md` | Dm4 |
| D10 | `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` — second half | the daemon recomputed `proposal_hash` with the `normalizePlanProposal` default before admitting an `accept_plan` command | deleted with `validateGoalCommandPredicateIDs`; TS `validateGoalCommandSemantics` + `validate_goal_command/2` enforce it; the first half (canonical bytes + Jason digest) stays | Dm5 |
| D11 | `TestGoalTransportParsesContractFixtures` — the semantic assertion for the 15 fixtures whose manifest `semantic_valid` ≠ `schema_valid` | `decision` 5 (`duplicate-option`, `missing-resolution`, `open-with-resolution`, `unknown-option`, `superseded-with-resolution`), `goal-command` 6 (`amend-deterministic-review`, `plan-same-change-branch`, `decision-plan-same-change-branch`, `proposal-hash-mismatch`, `request-plan-subject-resource-mismatch`, `unpaired-surrogate`), `goal-revision` 2 (`duplicate-predicate-id`, `deterministic-review`), `goal-create` 1 (`deterministic-review`), `evidence-batch` 1 (`duplicate-key`) | schema arm unchanged for all 248; Elixir `contract_fixture_manifest_test.exs` ("public Elixir validators agree with every manifest semantic expectation") pins the semantic column; the proposal rules stay live for a proposal embedded in a TaskResult (`validatePlanProposalPredicateIDs` via `TaskResult.Validate`) | Dm6/Dm7 |

The 2 `plan-proposal` rows left this row when the apply stage restored their oracle (finding 1), so
D11 now covers 15 fixtures, and both restored rows redden under their mutants (§9 Dm7, Dm12). The
other 85 fixtures of the seven remaining no-oracle envelopes keep their coverage (100 fixtures in
those envelopes − the 15 semantic-only rows): those 85 rows carry neither `semantic_valid` nor
`schema_valid`, so both expectations fall back to `valid` (measured over the manifest: all 33 rows
that carry `semantic_valid` also carry `schema_valid`, and 32 of those 33 differ; the 15 no-oracle
rows are a subset of the 33), which the unchanged schema assertion pins. Test names were kept
byte-identical, including `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` whose name now
outlives the parser it was named after (contract clause "no test name changes"); the retarget is
visible in its body (`RejectDuplicateJSONMembers`).

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
| M7 `TaskResult.Validate` subject-hash equality | fixture subtest `task-result.subject-hash-mismatch` (`goal_contract_fixture_test.go:96`; was `:94` before the apply stage's finding-1 arm) |

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
| Dm7 `validateProviderChangeTarget` equal-branch rule | pre-deletion tree: 5 fixture rows red. Retargeted tree at `7f259378` with the pristine dispatch (re-measured by the apply stage in a scratch worktree): `task-result.proposal-same-change-branch` and `context-snapshot.same-change-branch` red while `plan-proposal.same-change-branch`, `goal-command.plan-same-change-branch` and `goal-command.decision-plan-same-change-branch` stayed **green** — the D11 coverage loss, measured | D11 (change targets) |
| Dm7 (re-run with finding 1 applied; scratch worktree at `7f259378` plus the edited dispatch) | 3 fixture rows red: `plan-proposal.same-change-branch` (restored), `task-result.proposal-same-change-branch` and `context-snapshot.same-change-branch`; `goal-command.plan-same-change-branch` and `goal-command.decision-plan-same-change-branch` stay green — the residual D11 loss | D11 (change targets), finding 1 |
| Dm12 `acceptanceContract.validatePredicateIDs` duplicate id (retargeted-tree re-run of the M3 mutant, same scratch worktree) | `plan-proposal.duplicate-predicate-id` (restored), `context-snapshot.duplicate-predicate-id` and `task-result.proposal-duplicate-predicate` red; `goal-revision.duplicate-predicate-id` stays green — the residual D11 loss | D11 (predicate ids), finding 1 |
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

`git diff --shortstat origin/main...HEAD -- daemon` (re-measured at the review-round-1 fix head):
19 files changed, 88 insertions(+), 1081 deletions(-) — net **−993**. Per file (`git diff --numstat
origin/main...HEAD -- daemon`): `goal_evidence_batch_response.go` 0/263 (deleted whole by the
review-round-1 fix), `goal_evidence_batch_response_test.go` 0/169, `goal_schema.go` 0/148,
`goal_contract_semantics.go` 2/150 (0/148 at the deletion; the apply stage's finding-3 comment
rewrite replaces two lines), `goal_evidence_batch_test.go` 1/83, `goal_evidence_batch.go` 2/88
(0/74 at the deletion; the fix stages' comment rewrites and the review-round-1 declaration
deletion), `goal_contract_semantics_test.go` 5/56, `goal_subject_test.go` 3/31, `goal_results.go`
0/20, `goal_admission.go` 0/12, `goal_subject.go` 0/9, `goal_transport_test.go` 33/33 (oracle
swaps), `goal_contract_fixture_test.go` 26/3 (24/3 at the deletion; the finding-1 restored arm adds
a case), and the six cross-package fixture builders 2-4 lines each on both sides. The contract's
−400..−900 estimate was report-only (D2); the measured net delta is −993, of which the
review-round-1 deletion contributes 57 deleted lines (10 in `goal_evidence_batch.go`, 47 in
`goal_evidence_batch_response.go`).

Residual risks and known limits:

- The daemon no longer decodes a goal-command, goal-create, goal-revision, decision or evidence-batch
  envelope at all; a plan-proposal envelope stays validated by the retained `ValidatePlanProposal`
  (live through `TaskResult.Validate`). Those wire properties stay pinned on the control side
  (`control/lib/symmetry_control/goals/contract_validation.ex` + the Elixir manifest test over all
  248 fixtures, and TS `contracts/ts/semantics.ts`); a future daemon consumer calls the production
  parsers (D5) or re-adds a dropped rule with its own tests.
- The `contracts/README.md` sentence that named `protocol.ValidateGoalEnvelope` was rewritten to the
  retained production entry points by the fix stage under the conductor's REV-01 amendment (§12);
  `docs/archive/CLOSED-TODO.md:3640` still names `goal_contract_semantics.go` +
  `canonicalPlanProposalSHA256` — a historical record of a past fix, in a document this revision does
  not modify. The citation gate audits only documents a revision modifies, so it never reads that
  closure; carried in the PR body by design.
- `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` keeps the name of the parser it was
  named after (the contract forbids test-name changes); its body now calls
  `RejectDuplicateJSONMembers`.
- `EvidenceBatchSchemaVersion` and the batch response DTOs had no in-tree consumer; the
  review-round-1 fix deleted them with the zero-reference proof in §4.
- `deadcode` reachability is RTA from `./cmd/...` with the tool's documented caveats (no
  `//go:linkname` aliasing, one GOOS/GOARCH per run, generated files excluded); the whole-repo grep
  and the `-test ./...` classification leg are the independent second strategy.
- Windows host: no local `-race`; Linux-only suites run under WSL (`Ubuntu-24.04`, go1.27.0) and the
  arm64 evidence is compile/analysis level only.

## 11. Apply-stage re-verification (2026-11-22; simplify findings 1-3, PD-3/PD-4)

Findings applied without committing (the per-finding table is in the apply-stage report). All
foreground, one heavy job at a time; from `daemon/` unless noted.

| gate | exit | output |
| --- | --- | --- |
| `gofmt -l .` | 0 | no files listed |
| `go vet ./...` (windows/amd64) | 0 | no output |
| `go build ./...`, `GOOS=windows go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | 0 each | |
| `go test -count=1 ./internal/protocol/ ./internal/app/ ./internal/state/ ./internal/control/ ./internal/harness/... ./internal/workspace/` | 0 | 14 packages `ok` (protocol 2.166s, app 259.430s, state 12.690s, control 1.381s, harness 14.476s + 8 sub-packages, workspace 211.812s) |
| WSL `Ubuntu-24.04` (go1.27.0) `go test -count=1 ./internal/state/ ./internal/workspace/` | 0 | `ok state 12.579s`, `ok workspace 52.848s` |
| `deadcode ./cmd/...` windows/amd64 / linux-amd64 / linux-arm64 (after) | 0 | 92 / 201 / 153 — unchanged from §3 |
| deleted-name check over the three findings lists (and `git grep -nw` over `daemon/**`) | n/a | 0 of the 26 names in the lists; the tree grep finds only the retained `EvidenceBatchSchemaVersion` |
| `python scripts/ci/test-check-citation-existence.py` | 0 | `Ran 49 tests in 161.040s` … `OK` |
| citation scan over the audited corpus for the 26 names | n/a | 1 hit, `docs/archive/CLOSED-TODO.md:3640` (an unmodified document; the gate reads modified documents only) |
| `git diff --check` | 0 | clean |
| DLP probe (added lines, profile-path pattern and backslash-u pattern) | n/a | 0 host-path hits; 0 backslash-u hits |
| Dm7 and Dm12 mutants re-measured on the retargeted tree (§9) | 1 each | the restored `plan-proposal` rows are red (3 named rows per mutant) |
| LOC (`git diff --shortstat 4fdea989 -- daemon`, measured at the fix stage) | n/a | 19 files, 88 insertions(+), 1023 deletions(-), net −935; superseded by review round 1 (§10, §13) |

The implement gate recorded 13 packages `ok`; the package set now has 14 because `harness/linejson`
(#222, `4fdea989`) is part of the run.

## 12. Fix-stage re-verification (2026-11-22; review REV-01..REV-06)

All six review findings were fixed in place. No Go behaviour changed — the only source edit is a
comment coordinate in `goal_subject_test.go:179` — so the deletion's gates were re-run to confirm the
tree, not re-argued. All foreground, one heavy job at a time; from `daemon/` unless noted.

| gate | exit | output |
| --- | --- | --- |
| `gofmt -l .` | 0 | no files listed |
| `go vet ./...` (windows/amd64) | 0 | no output |
| `go build ./...`, `GOOS=windows go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | 0 each | |
| `go test -count=1 ./internal/protocol/ ./internal/app/ ./internal/state/ ./internal/control/ ./internal/harness/... ./internal/workspace/` | 0 | 14 packages `ok` (protocol 0.986s, app 192.328s, state 10.410s, control 1.331s, harness 14.618s + 8 sub-packages, workspace 172.769s); 3995 distinct test names executed — 3958 pass, 37 skip, 0 fail |
| WSL `Ubuntu-24.04` (go1.27.0) `go test -count=1 ./internal/state/ ./internal/workspace/` | 0 | `ok state 15.252s`, `ok workspace 65.516s` |
| `deadcode ./cmd/...` windows/amd64 / linux-amd64 / linux-arm64 (after) | 0 | 92 / 201 / 153 — unchanged from §3, and none of the deleted 26 names appears in either GOOS output |
| deleted-name `git grep -nw` over `daemon/**` | n/a | 0 matches; the plain grep matches only the retained test name `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers` (a compound identifier, not the deleted symbol) |
| production-entry-point caller sets (`git grep -lw`, pre-fix tree ↔ fix tree) | n/a | unchanged for all five entry points (12 / 6 / 18 / 4 / 7 files) |
| citation scan over the audited corpus for the 26 names | n/a | 1 hit, `docs/archive/CLOSED-TODO.md:3640` (an unmodified document; the gate reads modified documents only) |
| `python scripts/ci/test-check-citation-existence.py` | 0 | `Ran 49 tests in 136.602s` … `OK` |
| static test-function counts (`^func Test` over the six package trees, pre-fix tree ↔ fix tree) | n/a | 1712 ↔ 1712 — no test added, removed, renamed or re-pinned |
| `git diff --check` | 0 | clean |
| DLP probe (added lines: profile-path pattern, backslash-u pattern) | n/a | 0 host-path hits; 0 backslash-u hits |
| host-path scan over `git diff origin/main...HEAD` and the working diff | n/a | 0 hits |

What the fix round changed:

- REV-01 (P2): `contracts/README.md`'s boundary sentence named the deleted `protocol.ValidateGoalEnvelope`;
  under the conductor's owned-set amendment it now names the retained production entry points
  (`ParseAdmissionInput`, `ParseTaskResult`, `ValidateTaskResultForAdmission`, `DecodeContextSnapshot`;
  review round 1 extended the sentence with `ValidatePlanProposal`, the strict `Evidence`/`Usage`/
  `RuntimeCapabilities` decoders and the Control/TS semantics side — §13).
  No doc citation of a deleted symbol dangles in the delivered tree except the `CLOSED-TODO.md:3640`
  closure, a historical record this revision does not touch.
- REV-02 (P2, corrected in review round 1): the D7/D9 rows now state the truth — duplicate receipt
  `id`/`evidence_key` are rejected transitively on the daemon's only consuming path
  (`validateGoalEvidenceBatch`'s request-side uniqueness plus `validateGoalEvidenceReceipt`'s
  per-index identity), so the residual gap is producer-side/schema/TS response semantics and
  conflict-details; the conflict index/`evidence_key` rules have no remaining equivalent. The
  phantom `validate_evidence_batch_receipt_set` name is gone, and `docs/TODO.md` carries the
  residual-enforcement entry (owner: contracts strictness (A6)).
- REV-03 (P3): the D6/D8 rows no longer attribute their equivalent to TS (`contracts/ts/semantics.ts`
  holds no response or conflict-details validator; the schemas and the Elixir manifest test pin them).
- REV-04 (P3): the plan's `protocol.go` coordinate is 649 L at the re-derivation baseline (`719` was
  `0fa9b705`'s).
- REV-05 (P3): the other-85 sentence now states that those rows carry neither key and both
  expectations fall back to `valid`; the "17 rows" phrase is scoped to the D11 set (33 manifest rows
  carry `semantic_valid`, 32 of them differing).
- REV-06 (P3): the frontend pin coordinate is `goal-attention.test.tsx:1238`, where the digest actually
  lives (`751-790` was a decision-resolution block).

The residual-risk records in this file and in the plan were updated to match. Fix-stage delta
(`git diff --shortstat`, the reported source): 5 files, 128 insertions(+), 43 deletions(-) — three
documents, this evidence file, and the one test comment. The review-round-1 fix supersedes that
delta; its measurements are in §13.

## 13. Review-round-1 fix re-verification (2026-11-22; opus review P3-1..P3-4)

All four review findings were fixed. P3-1: the D7 rows (plan + evidence), the §12 REV-02 bullet and
the `docs/TODO.md` entry now record that duplicate receipt `id`/`evidence_key` are rejected
transitively on the daemon's only consuming path — `validateGoalEvidenceBatch`'s request-side
uniqueness plus `validateGoalEvidenceReceipt`'s per-index identity against that uniqueness-validated
request — and scope the residual gap to producer-side/schema/TS response semantics and
conflict-details; D9 keeps "no remaining enforcement" because it remains true there. P3-2:
`contracts/README.md`'s boundary sentence no longer claims the daemon owns full cross-field
validation of the deleted envelopes; it names the retained daemon entry points
(`ParseAdmissionInput`, `ParseTaskResult`, `ValidateTaskResultForAdmission`,
`DecodeContextSnapshot`, `ValidatePlanProposal`, the strict `Evidence`/`Usage`/`RuntimeCapabilities`
decoders) and the Control/TS semantics side. P3-3: the ten unreferenced exported declarations were
deleted (§4). P3-4: §10's LOC figures were re-measured against `origin/main`; §12's older
measurements are marked superseded. No wire, persisted or error-text change; no test added,
renamed or weakened. All commands foreground, from `daemon/` unless noted.

| gate | exit | output |
| --- | --- | --- |
| zero-reference proof before deletion (`git grep -nw` over the whole repository, ten names) | n/a | hits only in the declaring files, the docs corrected here, and the homonyms recorded in §4; `git grep -nE 'protocol\.EvidenceBatch\b'` → 0 |
| `gofmt -l .` before and after | 0 | no files listed |
| `go build ./...` (windows/amd64), `GOOS=windows go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` before and after | 0 each | |
| `go vet ./...` windows/amd64 and `GOOS=linux GOARCH=amd64 go vet ./...` before and after | 0 each | no output |
| `GOOS=linux GOARCH=arm64 go vet ./...` before and after | 1 | one pre-existing failure: `internal/state/linux_clone_journal_test.go` calls the unexported `LinuxCloneCleanupReader.verify`, defined only in the amd64-gated `linux_clone_cleanup_reader_linux_amd64.go`; both files are unchanged in the failing region (the branch edits the test file only at its evidence-builder retarget, ~`:1307`), so `origin/main` fails identically |
| `GOOS=linux GOARCH=arm64 go vet ./internal/protocol/ ./internal/control/ ./internal/app/` after | 0 | no output |
| `deadcode ./cmd/...` windows/amd64, `GOOS=linux GOARCH=amd64`, `GOOS=linux GOARCH=arm64` (the pinned tool on PATH) before → after | 0 each | 92 / 201 / 153 → 92 / 201 / 153; the deleted declarations are invisible to the function-level tool |
| `go test -count=1 ./internal/protocol/ ./internal/control/ ./internal/app/ ./internal/state/ ./internal/workspace/` | 0 | `ok protocol 1.376s`, `ok control 1.270s`, `ok app 231.510s`, `ok state 7.977s`, `ok workspace 200.960s` |
| deleted-name grep after deletion (ten names, whole repository) | n/a | no Go reference remains; only the docs corrected here and the homonyms above |
| `git diff --check` (HEAD↔worktree and merge-base↔worktree) | 0 | clean |
| DLP probe (added lines: profile-path pattern, backslash-u pattern) | n/a | 0 host-path hits; 0 backslash-u hits |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this file | 0 | clean |
| `git diff --numstat origin/main...HEAD -- daemon` at the final head | n/a | §10 (19 files, 88/1081, net −993) |

Not re-run this round: the WSL linux-only suites (`internal/state`, `internal/workspace`) and the
slow-oracle/race jobs — the round deletes unreferenced declarations, and the linux/amd64 and
linux/arm64 compile-level gates above are green apart from the pre-existing arm64 `internal/state`
vet failure.
