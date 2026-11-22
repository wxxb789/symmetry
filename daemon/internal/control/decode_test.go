package control

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
)

type rawWireFixture struct {
	ID                string            `json:"id"`
	Schema            string            `json:"schema"`
	Path              string            `json:"path"`
	WireError         string            `json:"wire_error"`
	WireLevels        []string          `json:"wire_levels"`
	RuntimeDivergence map[string]string `json:"runtime_divergence"`
}

type rawWireManifest struct {
	Fixtures []rawWireFixture `json:"fixtures"`
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

// The corpus vocabulary; a row accepted at every level never consults
// rawWireErrorTexts, so an unknown class must fail before the level loop.
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

var rawWireLevels = map[string]responseDecodeLevel{
	"legacy": responseDecodeLegacy,
	"typed":  responseDecodeTyped,
	"schema": responseDecodeSchema,
}

var responseDecodeLevelNames = [responseDecodeLevelCount]string{
	responseDecodeLegacy: "legacy",
	responseDecodeTyped:  "typed",
	responseDecodeSchema: "schema",
}

func TestResponseDecodeLevelsMatchTheRawCorpus(t *testing.T) {
	root := controlRepositoryRoot(t)
	manifestData, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest rawWireManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}

	seen := 0
	for _, fixture := range manifest.Fixtures {
		if fixture.WireError == "" {
			continue
		}
		seen++
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if _, known := rawWireErrorClasses[fixture.WireError]; !known {
				t.Fatalf("unknown wire_error class %q", fixture.WireError)
			}
			data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", filepath.FromSlash(fixture.Path)))
			if err != nil {
				t.Fatal(err)
			}
			rejecting := map[string]bool{}
			for _, level := range fixture.WireLevels {
				if _, known := rawWireLevels[level]; !known {
					t.Fatalf("unknown wire level %q", level)
				}
				rejecting[level] = true
			}
			goAccepts := fixture.RuntimeDivergence["go"] == "accepts"

			for name, level := range rawWireLevels {
				var decoded map[string]any
				err := decodeResponseJSON(level, contracts.Envelope(fixture.Schema), data, &decoded)
				if got, want := err != nil, rejecting[name]; got != want {
					t.Fatalf("%s level rejected = %t, want %t: %v", name, got, want, err)
				}
				if name != "schema" || err == nil {
					continue
				}
				want, known := rawWireErrorTexts[fixture.WireError]
				if !known {
					t.Fatalf("wire_error class %q was rejected but this reader has no native text", fixture.WireError)
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("schema rejection %q does not carry %q", err, want)
				}
			}
			if goAccepts == rejecting["schema"] {
				t.Fatalf("go accepts = %t contradicts schema-level rejection = %t", goAccepts, rejecting["schema"])
			}
		})
	}
	if seen == 0 {
		t.Fatal("manifest carries no raw wire fixtures")
	}
}

func TestResponseDecodeRoutesEveryPathThroughTheEntry(t *testing.T) {
	assertEntryLevel := func(t *testing.T, expected responseDecodeLevel, run func() error) {
		t.Helper()
		var before [responseDecodeLevelCount]int64
		for level := range before {
			before[level] = responseDecodeCalls[level].Load()
		}
		if err := run(); err != nil {
			t.Fatalf("decode path failed: %v", err)
		}
		for level := range before {
			delta := responseDecodeCalls[level].Load() - before[level]
			want := int64(0)
			if responseDecodeLevel(level) == expected {
				want = 1
			}
			if delta != want {
				t.Fatalf("%s entry calls = %d, want %d", responseDecodeLevelNames[level], delta, want)
			}
		}
	}
	machineResponse := func(t *testing.T, body string) *Client {
		t.Helper()
		server := jsonServer(t, http.StatusOK, body, nil)
		t.Cleanup(server.Close)
		return mustMachineClient(t, server)
	}

	t.Run("requestGoalWithStatus", func(t *testing.T) {
		client := machineResponse(t, `{"value":1}`)
		var target struct {
			Value int `json:"value"`
		}
		assertEntryLevel(t, responseDecodeTyped, func() error {
			_, err := client.requestGoalWithStatus(context.Background(), http.MethodGet, "v1/goal", nil, "", nil, &target)
			return err
		})
	})

	t.Run("requestGoalContractWithStatus", func(t *testing.T) {
		body, err := os.ReadFile(filepath.Join(controlRepositoryRoot(t), "contracts", "fixtures", "valid", "evidence-batch-response.basic.json"))
		if err != nil {
			t.Fatal(err)
		}
		client := machineResponse(t, string(body))
		var target contracts.SymmetryEvidenceBatchResponseV1
		assertEntryLevel(t, responseDecodeSchema, func() error {
			_, err := client.requestGoalContractWithStatus(context.Background(), http.MethodPost, "v1/evidence", nil, "", nil, contracts.EnvelopeEvidenceBatchResponse, &target)
			return err
		})
	})

	t.Run("requestWithStatus", func(t *testing.T) {
		client := machineResponse(t, `{"value":1}`)
		var target struct {
			Value int `json:"value"`
		}
		assertEntryLevel(t, responseDecodeLegacy, func() error {
			_, err := client.requestWithStatus(context.Background(), http.MethodGet, "v1/enroll", nil, "token", "", nil, &target)
			return err
		})
	})

	t.Run("requestWithStatusNilResponse", func(t *testing.T) {
		client := machineResponse(t, `{"value":1}`)
		assertEntryLevel(t, responseDecodeLegacy, func() error {
			_, err := client.requestWithStatus(context.Background(), http.MethodGet, "v1/enroll", nil, "token", "", nil, nil)
			return err
		})
	})

	t.Run("decodeStrictObjectJSON", func(t *testing.T) {
		var target struct {
			GoalID string `json:"goal_id"`
		}
		assertEntryLevel(t, responseDecodeTyped, func() error {
			return decodeStrictObjectJSON([]byte(`{"goal_id":"goal-1"}`), &target, "goal_id")
		})
	})

	t.Run("decodeProviderActionObjectJSON", func(t *testing.T) {
		var target struct {
			Operation string `json:"operation"`
		}
		assertEntryLevel(t, responseDecodeLegacy, func() error {
			return decodeProviderActionObjectJSON([]byte(`{"operation":"probe"}`), &target, "operation")
		})
	})

	t.Run("decodeAPIError", func(t *testing.T) {
		assertEntryLevel(t, responseDecodeLegacy, func() error {
			_ = decodeAPIError(http.StatusBadRequest, http.Header{}, []byte(`{"error":{"code":"invalid_request","message":"bad"}}`))
			return nil
		})
	})

	t.Run("decodeEvidenceBatchConflictDetails", func(t *testing.T) {
		body, err := os.ReadFile(filepath.Join(controlRepositoryRoot(t), "contracts", "fixtures", "valid", "evidence-batch-conflict-details.basic.json"))
		if err != nil {
			t.Fatal(err)
		}
		assertEntryLevel(t, responseDecodeSchema, func() error {
			_, err := decodeEvidenceBatchConflictDetails(json.RawMessage(body))
			return err
		})
	})
}

func controlRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}
