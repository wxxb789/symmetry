package protocol

import (
	"encoding/json"
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

			var semanticErr error
			switch contractdto.Envelope(fixture.Schema) {
			case contractdto.EnvelopeConnectionsRead:
				// Decode into the generated DTO, not a map: this is the only
				// place the daemon decodes the connections read projection, and
				// a map would let a broken nullable field pass unnoticed.
				var connections contractdto.SymmetryConnectionsReadV1
				semanticErr = contractdto.DecodeSchema(
					contractdto.EnvelopeConnectionsRead,
					data,
					&connections,
				)
			case contractdto.EnvelopeWorkspaceRead, contractdto.EnvelopeWorkDetailRead, contractdto.EnvelopeWorkTimelineRead:
				var presentation map[string]any
				semanticErr = contractdto.DecodeSchema(contractdto.Envelope(fixture.Schema), data, &presentation)
			default:
				semanticErr = ValidateGoalEnvelope(contractdto.Envelope(fixture.Schema), data)
			}
			if got, want := semanticErr == nil, fixture.expectedSemanticValidity(); got != want {
				t.Fatalf("semantic validity = %t, want %t: %v", got, want, semanticErr)
			}
		})
	}
}

func TestConnectionsReadDTOFillsNullableFields(t *testing.T) {
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "valid", "connections-read.unknown-values.json"))
	if err != nil {
		t.Fatal(err)
	}

	var decoded contractdto.SymmetryConnectionsReadV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeConnectionsRead, data, &decoded); err != nil {
		t.Fatalf("valid connections read rejected: %v", err)
	}
	if len(decoded.Connections) != 1 {
		t.Fatalf("decoded %d connections, want 1", len(decoded.Connections))
	}
	connection := decoded.Connections[0]
	if connection.Status != "degraded" || connection.StatusMessage == nil || *connection.StatusMessage != "Provider authentication failed" {
		t.Fatalf("nullable status fields did not decode: status=%q message=%v", connection.Status, connection.StatusMessage)
	}
	if connection.LastCheckedAt == nil {
		t.Fatalf("nullable last_checked_at did not decode")
	}
	if connection.Version != 2 {
		t.Fatalf("version = %d, want 2", connection.Version)
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

func contractRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}
