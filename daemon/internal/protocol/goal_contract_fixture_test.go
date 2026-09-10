package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

type goalContractFixtureManifest struct {
	Fixtures []goalContractFixture `json:"fixtures"`
}

type goalContractFixture struct {
	ID            string `json:"id"`
	Schema        string `json:"schema"`
	Path          string `json:"path"`
	Valid         bool   `json:"valid"`
	SchemaValid   *bool  `json:"schema_valid"`
	SemanticValid *bool  `json:"semantic_valid"`
}

func TestGoalTransportParsesContractFixtures(t *testing.T) {
	root := contractRepositoryRoot(t)
	manifestData, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest goalContractFixtureManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fixtures) == 0 {
		t.Fatal("fixture manifest is empty")
	}

	for _, fixture := range manifest.Fixtures {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", filepath.FromSlash(fixture.Path)))
			if err != nil {
				t.Fatal(err)
			}

			schemaErr := validateFixtureSchema(fixture.Schema, data)
			if got, want := schemaErr == nil, fixture.expectedSchemaValidity(); got != want {
				t.Fatalf("schema validity = %t, want %t: %v", got, want, schemaErr)
			}

			semanticErr := parseGoalContractFixture(fixture.Schema, data)
			if got, want := semanticErr == nil, fixture.expectedSemanticValidity(); got != want {
				t.Fatalf("semantic validity = %t, want %t: %v", got, want, semanticErr)
			}
		})
	}
}

func TestEvidenceReviewPayloadVerdictMustMatchEvidenceVerdict(t *testing.T) {
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "valid", "evidence.review.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEvidence(data); err != nil {
		t.Fatalf("valid review evidence rejected: %v", err)
	}
	mismatched := strings.Replace(string(data), `"verdict": "passed"`, `"verdict": "failed"`, 1)
	if _, err := ParseEvidence([]byte(mismatched)); err == nil {
		t.Fatal("accepted review evidence with a payload verdict different from evidence verdict")
	}
}

func (fixture goalContractFixture) expectedSchemaValidity() bool {
	if fixture.SchemaValid != nil {
		return *fixture.SchemaValid
	}
	return fixture.Valid
}

func (fixture goalContractFixture) expectedSemanticValidity() bool {
	if fixture.SemanticValid != nil {
		return *fixture.SemanticValid
	}
	return fixture.Valid
}

func validateFixtureSchema(schema string, data []byte) error {
	return contractdto.ValidateSchema(contractdto.Envelope(schema), data)
}

func parseGoalContractFixture(schema string, data []byte) error {
	switch contractdto.Envelope(schema) {
	case contractdto.EnvelopeAdapterCapabilities:
		_, err := ParseAdapterCapabilities(data)
		return err
	case contractdto.EnvelopeAdmission:
		_, err := ParseAdmission(data)
		return err
	case contractdto.EnvelopeContextSnapshot:
		_, err := DecodeContextSnapshot(data)
		return err
	case contractdto.EnvelopeDecision:
		_, err := DecodeDecision(data)
		return err
	case contractdto.EnvelopeEvidence:
		_, err := ParseEvidence(data)
		return err
	case contractdto.EnvelopeGoalCommand:
		return ValidateGoalCommand(data)
	case contractdto.EnvelopeGoalCreate, contractdto.EnvelopePlanProposal:
		return contractdto.Validate(contractdto.Envelope(schema), data)
	case contractdto.EnvelopeGoalRevision:
		_, err := DecodeGoalRevision(data)
		return err
	case contractdto.EnvelopeTaskResult:
		_, err := ParseTaskResult(data)
		return err
	case contractdto.EnvelopeUsage:
		_, err := ParseUsage(data)
		return err
	default:
		return fmt.Errorf("unknown fixture schema %q", schema)
	}
}

func contractRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}
