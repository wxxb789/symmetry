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
	ID                string            `json:"id"`
	Schema            string            `json:"schema"`
	Path              string            `json:"path"`
	Valid             bool              `json:"valid"`
	WireError         string            `json:"wire_error"`
	RuntimeDivergence map[string]string `json:"runtime_divergence"`
	SchemaValid       *bool             `json:"schema_valid"`
	SemanticValid     *bool             `json:"semantic_valid"`
}

var rawWireErrorTexts = map[string]string{
	"duplicate_json_member": "duplicate JSON object member",
	"noncanonical_number":   "fractional or exponent number is not canonical",
	"unsafe_integer":        "integer exceeds the JSON safe-integer range",
	"leading_zero":          "invalid character",
	"nonfinite_number":      "number is not finite",
	"invalid_utf8":          "JSON input is not valid UTF-8",
	"bom":                   "invalid character",
	"trailing_value":        "multiple JSON values",
	"trailing_garbage":      "invalid character",
}

// The corpus vocabulary; an accepted row never consults rawWireErrorTexts, so
// an unknown class must fail before the divergence branch.
var rawWireErrorClasses = map[string]struct{}{
	"duplicate_json_member": {},
	"noncanonical_number":   {},
	"unsafe_integer":        {},
	"leading_zero":          {},
	"negative_zero":         {},
	"nonfinite_number":      {},
	"invalid_utf8":          {},
	"bom":                   {},
	"trailing_value":        {},
	"trailing_garbage":      {},
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

			if fixture.WireError != "" {
				assertRawWireFixture(t, fixture, data)
				return
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
			case contractdto.EnvelopeAttentionRead, contractdto.EnvelopeChatRead, contractdto.EnvelopeGoalRead, contractdto.EnvelopeWorkspaceRead, contractdto.EnvelopeWorkDetailRead, contractdto.EnvelopeWorkTimelineRead:
				var presentation map[string]any
				semanticErr = contractdto.DecodeSchema(contractdto.Envelope(fixture.Schema), data, &presentation)
			case contractdto.EnvelopeAdmission:
				_, _, semanticErr = ParseAdmissionInput(data)
			case contractdto.EnvelopeTaskResult:
				_, semanticErr = ParseTaskResult(data)
			case contractdto.EnvelopeContextSnapshot:
				_, semanticErr = DecodeContextSnapshot(data)
			case contractdto.EnvelopeAdapterCapabilities:
				var capabilities RuntimeCapabilities
				semanticErr = json.Unmarshal(data, &capabilities)
			case contractdto.EnvelopeEvidence:
				var evidence Evidence
				semanticErr = json.Unmarshal(data, &evidence)
			case contractdto.EnvelopeUsage:
				var usage Usage
				semanticErr = json.Unmarshal(data, &usage)
			case contractdto.EnvelopePlanProposal:
				semanticErr = ValidatePlanProposal(data)
			case contractdto.EnvelopeDecision, contractdto.EnvelopeEvidenceBatch, contractdto.EnvelopeEvidenceBatchConflictDetails,
				contractdto.EnvelopeEvidenceBatchResponse, contractdto.EnvelopeGoalCommand, contractdto.EnvelopeGoalCreate,
				contractdto.EnvelopeGoalRevision:
				// No daemon-side decoder remains for these envelopes; the control-plane
				// validators pin their fixtures (control/test/symmetry_control/goals/contract_fixture_manifest_test.exs).
				return
			default:
				t.Fatalf("no daemon-side oracle for fixture schema %q", fixture.Schema)
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
	if connection.Status != "future_status" || connection.StatusMessage == nil || *connection.StatusMessage != "Provider authentication failed" {
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
	if err := json.Unmarshal(data, &Evidence{}); err != nil {
		t.Fatalf("valid review evidence rejected: %v", err)
	}
	mismatched := strings.Replace(string(data), `"verdict": "passed"`, `"verdict": "failed"`, 1)
	if err := json.Unmarshal([]byte(mismatched), &Evidence{}); err == nil {
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

func assertRawWireFixture(t *testing.T, fixture goalContractFixture, data []byte) {
	t.Helper()
	if _, known := rawWireErrorClasses[fixture.WireError]; !known {
		t.Fatalf("unknown wire_error class %q", fixture.WireError)
	}
	var decoded map[string]any
	err := contractdto.DecodeSchema(contractdto.Envelope(fixture.Schema), data, &decoded)
	if fixture.RuntimeDivergence["go"] == "accepts" {
		if err != nil {
			t.Fatalf("raw fixture is accepted by Go today but was rejected: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("raw fixture class %s was accepted by DecodeSchema", fixture.WireError)
	}
	want, known := rawWireErrorTexts[fixture.WireError]
	if !known {
		t.Fatalf("wire_error class %q was rejected but this reader has no native text", fixture.WireError)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("rejection %q does not carry %q", err, want)
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
