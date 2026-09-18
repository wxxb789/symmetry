package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

func TestGoalRevisionContractSemanticsStayAtProtocolBoundary(t *testing.T) {
	for name, test := range map[string]struct {
		envelope contractdto.Envelope
		fixture  string
		wantErr  bool
	}{
		"goal revision permits review when completion requires an operator": {
			envelope: contractdto.EnvelopeGoalRevision,
			fixture:  "valid/goal-revision.deterministic-operator-override.json",
		},
		"goal create permits review when completion requires an operator": {
			envelope: contractdto.EnvelopeGoalCreate,
			fixture:  "valid/goal-create.deterministic-operator-override.json",
		},
		"goal amendment permits review when completion requires an operator": {
			envelope: contractdto.EnvelopeGoalCommand,
			fixture:  "valid/goal-command.amend-deterministic-operator-override.json",
		},
		"deterministic revision still rejects review without an operator fence": {
			envelope: contractdto.EnvelopeGoalRevision,
			fixture:  "invalid/goal-revision.deterministic-review.json",
			wantErr:  true,
		},
		"goal create still rejects review without an operator fence": {
			envelope: contractdto.EnvelopeGoalCreate,
			fixture:  "invalid/goal-create.deterministic-review.json",
			wantErr:  true,
		},
		"goal amendment still rejects review without an operator fence": {
			envelope: contractdto.EnvelopeGoalCommand,
			fixture:  "invalid/goal-command.amend-deterministic-review.json",
			wantErr:  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateGoalEnvelope(test.envelope, readGoalSemanticFixture(t, test.fixture))
			if test.wantErr && err == nil {
				t.Fatal("ValidateGoalEnvelope accepted a non-machine deterministic acceptance contract")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidateGoalEnvelope rejected an operator-fenced contract: %v", err)
			}
		})
	}
}

func TestGoalEnvelopeRejectsDuplicateContextPredicateIDs(t *testing.T) {
	err := ValidateGoalEnvelope(
		contractdto.EnvelopeContextSnapshot,
		readGoalSemanticFixture(t, "invalid/context-snapshot.duplicate-predicate-id.json"),
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate predicate id") {
		t.Fatalf("ValidateGoalEnvelope accepted duplicate ContextSnapshot predicate IDs: %v", err)
	}
}

func TestAdmissionProviderScopeBindsOperationsToItsResources(t *testing.T) {
	valid := readGoalSemanticFixture(t, "valid/admission.provider-scope.json")
	if err := ValidateGoalEnvelope(contractdto.EnvelopeAdmission, valid); err != nil {
		t.Fatalf("valid provider scope rejected: %v", err)
	}

	var admission map[string]any
	if err := json.Unmarshal(valid, &admission); err != nil {
		t.Fatal(err)
	}
	scope := admission["provider_scope"].(map[string]any)
	operations := scope["operations_by_resource"].(map[string]any)
	operations["99999999-9999-4999-8999-999999999999"] =
		operations["22222222-2222-4222-8222-222222222222"]
	delete(operations, "22222222-2222-4222-8222-222222222222")
	data, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGoalEnvelope(contractdto.EnvelopeAdmission, data); err == nil || !strings.Contains(err.Error(), "is not a scoped resource") {
		t.Fatalf("provider scope with an unbound resource was accepted: %v", err)
	}
}

func TestProviderScopeValidateMatchesChangeTargetOperationContract(t *testing.T) {
	resourceID := "22222222-2222-4222-8222-222222222222"
	sourceBranch := "codex/goal-0006"
	targetBranch := "main"
	pullRequestURL := "https://example.test/change/1"
	branches := &ProviderChangeTarget{Kind: ProviderChangeTargetBranches, SourceBranch: &sourceBranch, TargetBranch: &targetBranch}
	pullRequest := &ProviderChangeTarget{Kind: ProviderChangeTargetPullRequest, PullRequestURL: &pullRequestURL}

	for _, test := range []struct {
		name       string
		operations []ProviderOperation
		target     *ProviderChangeTarget
		wantErr    bool
	}{
		{name: "read only", operations: []ProviderOperation{ProviderOperationResourceSync}},
		{name: "branches upsert", operations: []ProviderOperation{ProviderOperationChangeUpsert}, target: branches},
		{name: "branches upsert and update", operations: []ProviderOperation{ProviderOperationChangeUpsert, ProviderOperationChangeUpdate}, target: branches},
		{name: "pull request update", operations: []ProviderOperation{ProviderOperationChangeUpdate}, target: pullRequest},
		{name: "read only target with change", operations: []ProviderOperation{ProviderOperationChangeUpsert}, wantErr: true},
		{name: "branches with resource sync", operations: []ProviderOperation{ProviderOperationResourceSync}, target: branches, wantErr: true},
		{name: "branches update without upsert", operations: []ProviderOperation{ProviderOperationChangeUpdate}, target: branches, wantErr: true},
		{name: "pull request upsert", operations: []ProviderOperation{ProviderOperationChangeUpsert}, target: pullRequest, wantErr: true},
		{name: "pull request mixed changes", operations: []ProviderOperation{ProviderOperationChangeUpsert, ProviderOperationChangeUpdate}, target: pullRequest, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := ProviderScope{
				ResourceIDs:          []string{resourceID},
				OperationsByResource: map[string][]ProviderOperation{resourceID: test.operations},
				ChangeTarget:         test.target,
			}
			err := scope.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("ProviderScope.Validate() error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestProviderChangeTargetValidateMatchesSchemaTextRules(t *testing.T) {
	validSource := "codex/goal-0006"
	validTarget := "main"
	validURL := "https://example.test/change/1"

	for _, test := range []struct {
		name   string
		target ProviderChangeTarget
	}{
		{
			name: "source branch whitespace",
			target: ProviderChangeTarget{
				Kind:         ProviderChangeTargetBranches,
				SourceBranch: pointerTo("codex/goal 0006"),
				TargetBranch: &validTarget,
			},
		},
		{
			name: "target branch NUL",
			target: ProviderChangeTarget{
				Kind:         ProviderChangeTargetBranches,
				SourceBranch: &validSource,
				TargetBranch: pointerTo("ma\x00in"),
			},
		},
		{
			name: "pull request leading whitespace",
			target: ProviderChangeTarget{
				Kind:           ProviderChangeTargetPullRequest,
				PullRequestURL: pointerTo(" " + validURL),
			},
		},
		{
			name: "pull request trailing whitespace",
			target: ProviderChangeTarget{
				Kind:           ProviderChangeTargetPullRequest,
				PullRequestURL: pointerTo(validURL + "\t"),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.target.Validate(); err == nil {
				t.Fatalf("ProviderChangeTarget.Validate accepted %#v", test.target)
			}
		})
	}
}

func TestProviderChangeTargetMatchesECMAScriptWhitespaceAndLineTerminators(t *testing.T) {
	source := "feature\u0085name"
	target := "main"
	if err := (ProviderChangeTarget{
		Kind:         ProviderChangeTargetBranches,
		SourceBranch: &source,
		TargetBranch: &target,
	}).Validate(); err != nil {
		t.Fatalf("ECMAScript-schema-valid U+0085 branch rejected: %v", err)
	}

	url := "\u0085https://example.test/change/1"
	if err := (ProviderChangeTarget{
		Kind:           ProviderChangeTargetPullRequest,
		PullRequestURL: &url,
	}).Validate(); err != nil {
		t.Fatalf("ECMAScript-schema-valid U+0085 pull_request_url rejected: %v", err)
	}

	for _, lineTerminator := range []string{"\n", "\r", "\u2028", "\u2029"} {
		t.Run(fmt.Sprintf("internal line terminator U+%04X", []rune(lineTerminator)[0]), func(t *testing.T) {
			value := "https://example.test/change/" + lineTerminator + "1"
			if err := (ProviderChangeTarget{
				Kind:           ProviderChangeTargetPullRequest,
				PullRequestURL: &value,
			}).Validate(); err == nil {
				t.Fatalf("ECMAScript-pattern-invalid pull_request_url was accepted: %q", value)
			}
		})
	}
}

func pointerTo(value string) *string {
	return &value
}

func readGoalSemanticFixture(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractRepositoryRoot(t), "contracts", "fixtures", filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
