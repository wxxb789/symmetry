package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testUUID         = "11111111-1111-4111-8111-111111111111"
	testUUIDTwo      = "22222222-2222-4222-8222-222222222222"
	testCommit       = "0123456789abcdef0123456789abcdef01234567"
	testTreeDigest   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testHash         = "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	testEvidenceHash = "sha256:88c59dff7c5897758b4315961f687117819b744faf505eed29babc2437828dd0"
)

func TestCanonicalizeJSONSortsObjectsAndPreservesArrayOrder(t *testing.T) {
	first := []byte(`{"z":[{"b":2,"a":1},[3,2,1]],"a":"你好"}`)
	second := []byte(`{"a":"你好","z":[{"a":1,"b":2},[3,2,1]]}`)

	canonicalFirst, err := CanonicalizeJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSecond, err := CanonicalizeJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalFirst) != string(canonicalSecond) {
		t.Fatalf("canonical JSON differs:\n%s\n%s", canonicalFirst, canonicalSecond)
	}
	want := `{"a":"你好","z":[{"a":1,"b":2},[3,2,1]]}`
	if string(canonicalFirst) != want {
		t.Fatalf("canonical JSON = %s, want %s", canonicalFirst, want)
	}

	changedArray, err := CanonicalizeJSON([]byte(`{"a":"你好","z":[{"a":1,"b":2},[1,2,3]]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(changedArray) == string(canonicalFirst) {
		t.Fatal("canonicalization reordered or ignored array contents")
	}
}

func TestSubjectHashUsesCompleteCanonicalSubject(t *testing.T) {
	subject := Subject{ResourceID: testUUID, Commit: testCommit, TreeDigest: testTreeDigest}
	canonical, err := subject.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"commit":"`+testCommit+`","resource_id":"`+testUUID+`","tree_digest":"`+testTreeDigest+`"}` {
		t.Fatalf("canonical subject = %s", canonical)
	}
	first, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	second, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("subject hashes = %q and %q", first, second)
	}

	withoutTree := `{"resource_id":"` + testUUID + `","commit":"` + testCommit + `"}`
	if _, err := ParseSubject([]byte(withoutTree)); err == nil {
		t.Fatal("accepted Subject without tree_digest")
	}
}

func TestAdmissionAndAdapterCapabilitiesRejectUnknownFieldsAndEnums(t *testing.T) {
	validAdmission := `{"schema_version":"symmetry.admission.v1","admission_id":"` + testUUID + `","goal_id":"` + testUUIDTwo + `","goal_revision":1,"work_item_id":"` + testUUID + `","purpose":"implement","context_snapshot_id":"` + testUUIDTwo + `","context_hash":"` + testTreeDigest + `","model_profile":"implementation-default","session_mode":"fresh","requested_session_id":null,"subject":{"resource_id":"` + testUUID + `","commit":"` + testCommit + `","tree_digest":"` + testTreeDigest + `"},"limits":{"max_turns":1,"deadline_at":"2026-09-09T12:00:00Z","max_cost_microusd":"250000"},"validation_of_task_id":null,"provider_scope":null}`
	if _, err := ParseAdmission([]byte(validAdmission)); err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"missing tree digest":     strings.Replace(validAdmission, `,"tree_digest":"`+testTreeDigest+`"`, "", 1),
		"unknown admission field": strings.Replace(validAdmission, `,"limits":`, `,"extra":true,"limits":`, 1),
		"unknown purpose":         strings.Replace(validAdmission, `"purpose":"implement"`, `"purpose":"unknown"`, 1),
		"unsafe goal revision":    strings.Replace(validAdmission, `"goal_revision":1`, `"goal_revision":9007199254740992`, 1),
		"missing maximum cost":    strings.Replace(validAdmission, `,"max_cost_microusd":"250000"`, "", 1),
		"invalid maximum cost":    strings.Replace(validAdmission, `"max_cost_microusd":"250000"`, `"max_cost_microusd":"0250000"`, 1),
		"missing provider scope":  strings.Replace(validAdmission, `,"provider_scope":null`, "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAdmission([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid admission %s", mutated)
			}
		})
	}

	validAdapter := `{"structured_input":true,"provider_access":true,"interactive":false,"supervisory_control":false,"adapter":{"kind":"codex","native_version":"1.2.3","implementation_version":"symmetry-codex-1","protocol_version":1,"operations":{"start":true,"events":true,"cancel":true,"resume":false,"handoff":true,"guidance":"next_turn","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}`
	if _, err := ParseAdapterCapabilities([]byte(validAdapter)); err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"unknown guidance":         strings.Replace(validAdapter, `"guidance":"next_turn"`, `"guidance":"steer"`, 1),
		"unknown capability field": strings.Replace(validAdapter, `{"structured_input"`, `{"unsupported":true,"structured_input"`, 1),
		"unknown operation field":  strings.Replace(validAdapter, `"hard_cost_limit":false`, `"hard_cost_limit":false,"extra":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAdapterCapabilities([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid adapter capabilities %s", mutated)
			}
		})
	}
}

func TestPlanningAdmissionUsesNullableWorkItemIdentity(t *testing.T) {
	base := `{"schema_version":"symmetry.admission.v1","admission_id":"` + testUUID + `","goal_id":"` + testUUIDTwo + `","goal_revision":1,"work_item_id":"` + testUUID + `","purpose":"implement","context_snapshot_id":"` + testUUIDTwo + `","context_hash":"` + testTreeDigest + `","model_profile":"implementation-default","session_mode":"fresh","requested_session_id":null,"subject":{"resource_id":"` + testUUID + `","commit":"` + testCommit + `","tree_digest":"` + testTreeDigest + `"},"limits":{"max_turns":1,"deadline_at":"2026-09-09T12:00:00Z","max_cost_microusd":null},"validation_of_task_id":null,"provider_scope":null}`

	planJSON := strings.Replace(strings.Replace(base, `"work_item_id":"`+testUUID+`"`, `"work_item_id":null`, 1), `"purpose":"implement"`, `"purpose":"plan"`, 1)
	plan, err := ParseAdmission([]byte(planJSON))
	if err != nil {
		t.Fatalf("plan admission rejected: %v", err)
	}
	if plan.Purpose != AdmissionPurposePlan || plan.WorkItemID != nil || plan.ValidationOfTaskID != nil || plan.ProviderScope != nil {
		t.Fatalf("plan admission = %+v, want null work item, validation source, and provider scope", plan)
	}

	for name, mutated := range map[string]string{
		"plan with work item":         strings.Replace(planJSON, `"work_item_id":null`, `"work_item_id":"`+testUUID+`"`, 1),
		"implement without work item": strings.Replace(base, `"work_item_id":"`+testUUID+`"`, `"work_item_id":null`, 1),
		"plan with validation source": strings.Replace(planJSON, `"validation_of_task_id":null`, `"validation_of_task_id":"`+testUUIDTwo+`"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAdmission([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid planning admission: %s", mutated)
			}
		})
	}
}

func TestTaskResultKindMustMatchAdmissionPurpose(t *testing.T) {
	planAdmission := Admission{Purpose: AdmissionPurposePlan}
	ordinaryAdmission := Admission{Purpose: AdmissionPurposeImplement}
	planResult := TaskResult{Kind: TaskResultPlanProposed}
	ordinaryResult := TaskResult{Kind: TaskResultProgress}

	if err := ValidateTaskResultForAdmission(planAdmission, planResult); err != nil {
		t.Fatalf("plan result rejected for plan admission: %v", err)
	}
	if err := ValidateTaskResultForAdmission(ordinaryAdmission, ordinaryResult); err != nil {
		t.Fatalf("ordinary result rejected for ordinary admission: %v", err)
	}
	if err := ValidateTaskResultForAdmission(planAdmission, ordinaryResult); err == nil {
		t.Fatal("ordinary semantic result accepted for plan admission")
	}
	if err := ValidateTaskResultForAdmission(ordinaryAdmission, planResult); err == nil {
		t.Fatal("plan_proposed accepted for ordinary admission")
	}
}

func TestAdmissionSessionModeBindsRequestedSessionID(t *testing.T) {
	base := `{"schema_version":"symmetry.admission.v1","admission_id":"` + testUUID + `","goal_id":"` + testUUIDTwo + `","goal_revision":1,"work_item_id":"` + testUUID + `","purpose":"implement","context_snapshot_id":"` + testUUIDTwo + `","context_hash":"` + testTreeDigest + `","model_profile":"implementation-default","session_mode":"fresh","requested_session_id":null,"subject":{"resource_id":"` + testUUID + `","commit":"` + testCommit + `","tree_digest":"` + testTreeDigest + `"},"limits":{"max_turns":1,"deadline_at":"2026-09-09T12:00:00Z","max_cost_microusd":null},"validation_of_task_id":null,"provider_scope":null}`

	handoffJSON := strings.Replace(
		strings.Replace(base, `"session_mode":"fresh"`, `"session_mode":"handoff"`, 1),
		`"requested_session_id":null`, `"requested_session_id":null,"handoff_source_run_id":"`+testUUIDTwo+`"`, 1)
	handoff, err := ParseAdmission([]byte(handoffJSON))
	if err != nil {
		t.Fatalf("handoff admission rejected: %v", err)
	}
	if handoff.SessionMode != SessionModeHandoff || handoff.RequestedSessionID != nil || handoff.HandoffSourceRunID == nil || *handoff.HandoffSourceRunID != testUUIDTwo {
		t.Fatalf("handoff admission = %+v, want null requested session", handoff)
	}
	encoded, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "native_session") || strings.Contains(string(encoded), "native_handle") {
		t.Fatalf("handoff admission leaked native session identity: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"handoff_source_run_id":"`+testUUIDTwo+`"`) {
		t.Fatalf("handoff admission lost source run identity: %s", encoded)
	}

	resumeWithoutSession := strings.Replace(base, `"session_mode":"fresh"`, `"session_mode":"resume"`, 1)
	if _, err := ParseAdmission([]byte(resumeWithoutSession)); err == nil {
		t.Fatal("resume admission without requested_session_id was accepted")
	}
	resumeJSON := strings.Replace(resumeWithoutSession, `"requested_session_id":null`, `"requested_session_id":"`+testUUIDTwo+`"`, 1)
	resume, err := ParseAdmission([]byte(resumeJSON))
	if err != nil {
		t.Fatalf("resume admission rejected: %v", err)
	}
	if resume.SessionMode != SessionModeResume || resume.RequestedSessionID == nil || *resume.RequestedSessionID != testUUIDTwo {
		t.Fatalf("resume admission = %+v, want requested session", resume)
	}

	handoffWithSession := strings.Replace(handoffJSON, `"requested_session_id":null`, `"requested_session_id":"`+testUUIDTwo+`"`, 1)
	if _, err := ParseAdmission([]byte(handoffWithSession)); err == nil {
		t.Fatal("handoff admission with requested_session_id was accepted")
	}

	handoffWithoutSource := strings.Replace(handoffJSON, `,"handoff_source_run_id":"`+testUUIDTwo+`"`, "", 1)
	if _, err := ParseAdmission([]byte(handoffWithoutSource)); err == nil {
		t.Fatal("handoff admission without handoff_source_run_id was accepted")
	}

	freshWithSource := strings.Replace(base, `"requested_session_id":null`, `"requested_session_id":null,"handoff_source_run_id":"`+testUUIDTwo+`"`, 1)
	if _, err := ParseAdmission([]byte(freshWithSource)); err == nil {
		t.Fatal("fresh admission with handoff_source_run_id was accepted")
	}

	resumeWithSource := strings.Replace(resumeJSON, `"requested_session_id":"`+testUUIDTwo+`"`, `"requested_session_id":"`+testUUIDTwo+`","handoff_source_run_id":"`+testUUID+`"`, 1)
	if _, err := ParseAdmission([]byte(resumeWithSource)); err == nil {
		t.Fatal("resume admission with handoff_source_run_id was accepted")
	}
}

func TestAdmissionProviderScopeIsFailClosedAndPreservesCanonicalGrants(t *testing.T) {
	validAdmission := `{"schema_version":"symmetry.admission.v1","admission_id":"` + testUUID + `","goal_id":"` + testUUIDTwo + `","goal_revision":1,"work_item_id":"` + testUUID + `","purpose":"implement","context_snapshot_id":"` + testUUIDTwo + `","context_hash":"` + testTreeDigest + `","model_profile":"implementation-default","session_mode":"fresh","requested_session_id":null,"subject":{"resource_id":"` + testUUID + `","commit":"` + testCommit + `","tree_digest":"` + testTreeDigest + `"},"limits":{"max_turns":1,"deadline_at":"2026-09-09T12:00:00Z","max_cost_microusd":null},"validation_of_task_id":null,"provider_scope":{"resource_ids":["` + testUUID + `","` + testUUIDTwo + `"],"operations_by_resource":{"` + testUUID + `":["change.upsert","change.update"],"` + testUUIDTwo + `":["change.upsert"]},"change_target":{"kind":"branches","source_branch":"codex/goal-0006","target_branch":"main"}}}`
	admission, err := ParseAdmission([]byte(validAdmission))
	if err != nil {
		t.Fatal(err)
	}
	if admission.Limits.MaxCostMicrousd != nil {
		t.Fatalf("max_cost_microusd = %q, want nil", *admission.Limits.MaxCostMicrousd)
	}
	if admission.ProviderScope == nil {
		t.Fatal("provider_scope was lost during admission conversion")
	}
	scope := admission.ProviderScope
	if len(scope.ResourceIDs) != 2 || scope.ChangeTarget == nil ||
		scope.ChangeTarget.Kind != ProviderChangeTargetBranches ||
		scope.ChangeTarget.SourceBranch == nil || *scope.ChangeTarget.SourceBranch != "codex/goal-0006" {
		t.Fatalf("provider_scope = %#v", scope)
	}
	if got := scope.OperationsByResource[testUUID]; len(got) != 2 || got[0] != ProviderOperationChangeUpsert || got[1] != ProviderOperationChangeUpdate {
		t.Fatalf("operations_by_resource[%q] = %#v", testUUID, got)
	}

	for name, mutated := range map[string]string{
		"unscoped operation key": strings.Replace(validAdmission, `"`+testUUIDTwo+`":["change.upsert"]`, `"33333333-3333-4333-8333-333333333333":["change.upsert"]`, 1),
		"duplicate operation":    strings.Replace(validAdmission, `"change.upsert","change.update"`, `"change.upsert","change.upsert"`, 1),
		"invalid operation":      strings.Replace(validAdmission, `"change.upsert","change.update"`, `"resource.delete"`, 1),
		"equal branch target":    strings.Replace(validAdmission, `"target_branch":"main"`, `"target_branch":"codex/goal-0006"`, 1),
		"invalid branch target":  strings.Replace(validAdmission, `"target_branch":"main"`, `"target_branch":"main","pull_request_url":"https://example.test/pr/1"`, 1),
		"unknown scope field":    strings.Replace(validAdmission, `"change_target":`, `"extra":true,"change_target":`, 1),
		"non-implement scope":    strings.Replace(validAdmission, `"purpose":"implement"`, `"purpose":"validate"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAdmission([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid provider_scope %s", mutated)
			}
		})
	}
}

func TestNormalizedResultsRejectUnknownEnumsAndValidateSafeUsage(t *testing.T) {
	resultSubject := Subject{ResourceID: testUUID, Commit: testCommit, TreeDigest: testTreeDigest}
	resultSubjectHash, err := resultSubject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	validResult := `{"schema_version":"symmetry.task_result.v1","result_id":"` + testUUID + `","kind":"progress","summary":"continued","subject":{"resource_id":"` + testUUID + `","commit":"` + testCommit + `","tree_digest":"` + testTreeDigest + `"},"subject_hash":"` + resultSubjectHash + `","evidence_refs":[],"blocker":null,"proposed_next_action":null,"proposal":null,"reason":null,"diagnostics":[]}`
	parsedResult, err := ParseTaskResult([]byte(validResult))
	if err != nil {
		t.Fatal(err)
	}
	if parsedResult.Subject != resultSubject || parsedResult.SubjectHash != resultSubjectHash {
		t.Fatalf("parsed TaskResult lost subject binding: %#v", parsedResult)
	}
	proposalData, err := os.ReadFile(filepath.Join(contractRepositoryRoot(t), "contracts", "fixtures", "valid", "plan-proposal.basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	planResultJSON := strings.Replace(validResult, `"kind":"progress"`, `"kind":"plan_proposed"`, 1)
	planResultJSON = strings.Replace(planResultJSON, `"proposal":null`, `"proposal":`+string(proposalData), 1)
	planResult, err := ParseTaskResult([]byte(planResultJSON))
	if err != nil {
		t.Fatalf("valid plan_proposed task result rejected: %v", err)
	}
	if planResult.Kind != TaskResultPlanProposed || planResult.Proposal == nil {
		t.Fatalf("parsed plan result = %#v, want non-null proposal", planResult)
	}
	if _, err := ParseTaskResult([]byte(strings.Replace(validResult, `"proposal":null`, `"proposal":`+string(proposalData), 1))); err == nil {
		t.Fatal("accepted non-plan task result with a non-null proposal")
	}
	if _, err := ParseTaskResult([]byte(strings.Replace(planResultJSON, `"proposal":`+string(proposalData), `"proposal":null`, 1))); err == nil {
		t.Fatal("accepted plan_proposed task result with a null proposal")
	}
	validBlocked := strings.Replace(
		strings.Replace(validResult, `"kind":"progress"`, `"kind":"blocked"`, 1),
		`"blocker":null`,
		`"blocker":{"kind":"environment","code":"missing_tool","detail":"required tool is unavailable"}`,
		1,
	)
	if _, err := ParseTaskResult([]byte(validBlocked)); err != nil {
		t.Fatalf("valid blocked task result rejected: %v", err)
	}
	validFailed := strings.Replace(
		strings.Replace(validResult, `"kind":"progress"`, `"kind":"failed"`, 1),
		`"reason":null`,
		`"reason":"process_failure"`,
		1,
	)
	if _, err := ParseTaskResult([]byte(validFailed)); err != nil {
		t.Fatalf("valid failed task result rejected: %v", err)
	}
	handoffUnsupported := strings.Replace(validFailed, `"reason":"process_failure"`, `"reason":"handoff_unsupported"`, 1)
	if parsed, err := ParseTaskResult([]byte(handoffUnsupported)); err != nil {
		t.Fatalf("handoff_unsupported task result rejected: %v", err)
	} else if parsed.Reason == nil || *parsed.Reason != TaskResultReasonHandoffUnsupported {
		t.Fatalf("handoff failure reason = %#v", parsed.Reason)
	}
	for name, mutated := range map[string]string{
		"blocked requires blocker":      strings.Replace(validResult, `"kind":"progress"`, `"kind":"blocked"`, 1),
		"nonblocked rejects blocker":    strings.Replace(validBlocked, `"kind":"blocked"`, `"kind":"progress"`, 1),
		"failed requires reason":        strings.Replace(validResult, `"kind":"progress"`, `"kind":"failed"`, 1),
		"failed terminal reason only":   strings.Replace(validFailed, `"reason":"process_failure"`, `"reason":"no_verified_progress"`, 1),
		"nonfailed reason must be null": strings.Replace(validResult, `"reason":null`, `"reason":"process_failure"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTaskResult([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid task result relation: %s", mutated)
			}
		})
	}
	if _, err := ParseTaskResult([]byte(strings.Replace(validResult, `"kind":"progress"`, `"kind":"unknown"`, 1))); err == nil {
		t.Fatal("accepted unknown task result kind")
	}
	if _, err := ParseTaskResult([]byte(strings.Replace(validResult, `,"proposed_next_action":null`, `,"proposed_next_action":null,"extra":true`, 1))); err == nil {
		t.Fatal("accepted unknown task result field")
	}
	for name, mutated := range map[string]string{
		"missing subject":             strings.Replace(validResult, `,"subject":{"resource_id":"`+testUUID+`","commit":"`+testCommit+`","tree_digest":"`+testTreeDigest+`"}`, "", 1),
		"missing subject tree digest": strings.Replace(validResult, `,"tree_digest":"`+testTreeDigest+`"`, "", 1),
		"subject hash mismatch":       strings.Replace(validResult, resultSubjectHash, testHash, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTaskResult([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid task result %s", mutated)
			}
		})
	}

	baseUsage := `{"schema_version":"symmetry.usage.v1","usage_id":"` + testUUID + `","run_id":"` + testUUIDTwo + `","usage_key":"turn-1","provider":"provider","model":"model","input_tokens":9007199254740991,"output_tokens":0,"cached_input_tokens":0,"cost_microusd":"9007199254740992","cost_basis":"reported","price_version":null,"supersedes_id":null,"observed_at":"2026-09-09T12:00:00Z"}`
	parsedUsage, err := ParseUsage([]byte(baseUsage))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseUsage([]byte(strings.Replace(baseUsage, `"cost_basis":"reported"`, `"cost_basis":"estimated"`, 1))); err != nil {
		t.Fatalf("estimated usage rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Usage){
		"reported cost null": func(usage *Usage) {
			usage.CostMicrousd = nil
		},
		"estimated cost null": func(usage *Usage) {
			usage.CostBasis = CostEstimated
			usage.CostMicrousd = nil
		},
		"unknown cost non-null": func(usage *Usage) {
			usage.CostBasis = CostUnknown
		},
	} {
		t.Run("validate "+name, func(t *testing.T) {
			usage := parsedUsage
			mutate(&usage)
			if err := usage.Validate(); err == nil {
				t.Fatalf("Usage.Validate accepted inconsistent %s", name)
			}
		})
	}
	for name, mutate := range map[string]func(string) string{
		"unsafe input tokens": func(usage string) string {
			return strings.Replace(usage, `9007199254740991`, `9007199254740992`, 1)
		},
		"negative output tokens": func(usage string) string {
			return strings.Replace(usage, `"output_tokens":0`, `"output_tokens":-1`, 1)
		},
		"numeric microusd": func(usage string) string {
			return strings.Replace(usage, `"cost_microusd":"9007199254740992"`, `"cost_microusd":1.5`, 1)
		},
		"microusd overflow": func(usage string) string {
			return strings.Replace(usage, `"cost_microusd":"9007199254740992"`, `"cost_microusd":"10000000000000000000"`, 1)
		},
		"reported cost null": func(usage string) string {
			return strings.Replace(usage, `"cost_microusd":"9007199254740992"`, `"cost_microusd":null`, 1)
		},
		"estimated cost null": func(usage string) string {
			usage = strings.Replace(usage, `"cost_basis":"reported"`, `"cost_basis":"estimated"`, 1)
			return strings.Replace(usage, `"cost_microusd":"9007199254740992"`, `"cost_microusd":null`, 1)
		},
		"unknown cost non-null": func(usage string) string {
			return strings.Replace(usage, `"cost_basis":"reported"`, `"cost_basis":"unknown"`, 1)
		},
		"unknown cost basis": func(usage string) string {
			return strings.Replace(usage, `"cost_basis":"reported"`, `"cost_basis":"actual"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := mutate(baseUsage)
			if _, err := ParseUsage([]byte(mutated)); err == nil {
				t.Fatalf("accepted invalid usage %s", mutated)
			}
		})
	}
	if err := ValidateMicroUSD("9007199254740992"); err != nil {
		t.Fatalf("safe-integer-sized microusd rejected: %v", err)
	}
	if err := ValidateMicroUSD("9223372036854775807"); err != nil {
		t.Fatalf("MaxInt64 microusd rejected: %v", err)
	}
	if err := ValidateMicroUSD("9223372036854775808"); err == nil {
		t.Fatal("accepted MaxInt64+1 microusd")
	}

	validEvidence := `{"schema_version":"symmetry.evidence.v1","evidence_id":"` + testUUID + `","run_id":"` + testUUIDTwo + `","evidence_key":"tests:default","kind":"check","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + testCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"subject_hash":"` + testEvidenceHash + `","source_ref":{"kind":"check","ref":"check-run-1","validator_profile":"default-checks","subject_hash":"` + testEvidenceHash + `"},"source_revision":"profile:default-checks","validator_profile":"default-checks","verdict":"passed","payload":{"predicate_id":"tests","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + testCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"profile_digest":"` + testHash + `","command_argv_digest":"` + testHash + `","exit_code":0,"subject_hash":"` + testEvidenceHash + `","started_at":"2026-09-09T12:00:00Z","finished_at":"2026-09-09T12:01:00Z","output_ref":{"kind":"artifact","value":"artifact:check-output-1"}},"observed_at":"2026-09-09T12:01:00Z"}`
	if _, err := ParseEvidence([]byte(validEvidence)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEvidence([]byte(strings.Replace(validEvidence, `"verdict":"passed"`, `"verdict":"maybe"`, 1))); err == nil {
		t.Fatal("accepted unknown evidence verdict")
	}
}

func TestTaskResultRejectsKernelDerivedAutomaticBaselineBlockers(t *testing.T) {
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "invalid", "task-result.automatic-baseline-blocker.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTaskResult(data); err == nil {
		t.Fatal("accepted kernel-derived automatic_baseline blocker in TaskResult")
	}

	valid := `{"schema_version":"symmetry.task_result.v1","result_id":"66666666-6666-4666-8666-666666666666","kind":"progress","summary":"continued","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"fedcba9876543210fedcba9876543210fedcba98","tree_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"subject_hash":"sha256:f347107963c353d0e1c0515dbbf147666cbaad469168ad0fc0cde534e25228af","evidence_refs":[],"blocker":null,"proposed_next_action":{"kind":"wait","blocker":{"kind":"automatic_baseline","work_item_id":"99999999-9999-4999-8999-999999999999","reason":"missing_explicit_baseline"}},"proposal":null,"reason":null,"diagnostics":[]}`
	if _, err := ParseTaskResult([]byte(valid)); err == nil {
		t.Fatal("accepted kernel-derived automatic_baseline blocker in proposed wait action")
	}
}

func TestTaskResultValidateRejectsOverlongBlockerAndProposalText(t *testing.T) {
	subject := Subject{ResourceID: testUUID, Commit: testCommit, TreeDigest: testTreeDigest}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	tooLong := strings.Repeat("x", 16_385)
	for name, mutate := range map[string]func(*TaskResult){
		"environment blocker detail": func(result *TaskResult) {
			result.Kind = TaskResultBlocked
			result.Blocker = &Blocker{Kind: BlockerEnvironment, Code: "missing_tool", Detail: tooLong}
		},
		"repair reason": func(result *TaskResult) {
			result.Kind = TaskResultRepairRequired
			result.ProposedNextAction = &NextAction{Kind: NextActionRepair, WorkItemID: testUUIDTwo, Reason: tooLong}
		},
		"observe external reference": func(result *TaskResult) {
			result.ProposedNextAction = &NextAction{Kind: NextActionObserve, ResourceID: testUUIDTwo, ExternalRef: tooLong}
		},
		"replan reason": func(result *TaskResult) {
			result.Kind = TaskResultReplanRequired
			result.ProposedNextAction = &NextAction{Kind: NextActionReplan, Reason: tooLong}
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := TaskResult{
				SchemaVersion: TaskResultSchemaVersion,
				ResultID:      testUUID,
				Kind:          TaskResultProgress,
				Summary:       "valid result envelope",
				Subject:       subject,
				SubjectHash:   subjectHash,
				EvidenceRefs:  []string{},
				Diagnostics:   []Diagnostic{},
			}
			mutate(&result)
			if err := result.Validate(); err == nil {
				t.Fatalf("TaskResult.Validate accepted overlong %s", name)
			}
		})
	}
}

func TestEvidenceCanonicalSubjectSourceAndPredicateConsistency(t *testing.T) {
	valid := `{"schema_version":"symmetry.evidence.v1","evidence_id":"` + testUUID + `","run_id":"` + testUUIDTwo + `","evidence_key":"tests:default","kind":"check","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + testCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"subject_hash":"` + testEvidenceHash + `","source_ref":{"kind":"check","ref":"check-run-1","validator_profile":"default-checks","subject_hash":"` + testEvidenceHash + `"},"source_revision":"profile:default-checks","validator_profile":"default-checks","verdict":"passed","payload":{"predicate_id":"tests","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + testCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"profile_digest":"` + testHash + `","command_argv_digest":"` + testHash + `","exit_code":0,"subject_hash":"` + testEvidenceHash + `","started_at":"2026-09-09T12:00:00Z","finished_at":"2026-09-09T12:01:00Z","output_ref":{"kind":"artifact","value":"artifact:check-output-1"}},"observed_at":"2026-09-09T12:01:00Z"}`
	if _, err := ParseEvidence([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"source kind mismatch":     strings.Replace(valid, `"kind":"check","ref":"check-run-1"`, `"kind":"review","ref":"check-run-1"`, 1),
		"source hash mismatch":     strings.Replace(valid, `"subject_hash":"`+testEvidenceHash+`"},"source_revision"`, `"subject_hash":"`+testHash+`"},"source_revision"`, 1),
		"payload subject mismatch": strings.Replace(valid, `"resource_id":"22222222-2222-4222-8222-222222222222","commit"`, `"resource_id":"33333333-3333-4333-8333-333333333333","commit"`, 1),
		"outer subject mismatch":   strings.Replace(valid, `"tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"subject_hash"`, `"tree_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"},"subject_hash"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEvidence([]byte(mutated)); err == nil {
				t.Fatalf("accepted inconsistent evidence: %s", mutated)
			}
		})
	}
}

func TestGoalDTOsDoNotChangeLegacyWorkDecodeShape(t *testing.T) {
	var work Work
	if err := json.Unmarshal([]byte(`{"goal":"legacy","agent_profile":"generic","workspace":"local","input":{"ok":true}}`), &work); err != nil {
		t.Fatal(err)
	}
	if work.Goal != "legacy" || string(work.Input) != `{"ok":true}` {
		t.Fatalf("legacy Work changed shape: %#v", work)
	}
}
