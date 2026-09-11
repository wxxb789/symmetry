package protocol

import (
	"encoding/json"
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

func readGoalSemanticFixture(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractRepositoryRoot(t), "contracts", "fixtures", filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
