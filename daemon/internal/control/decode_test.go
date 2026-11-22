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

var rawWireLevels = map[string]responseDecodeLevel{
	"legacy": responseDecodeLegacy,
	"typed":  responseDecodeTyped,
	"schema": responseDecodeSchema,
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
					t.Fatalf("unknown wire_error class %q", fixture.WireError)
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
	entryDelta := func(t *testing.T, run func() error) int64 {
		t.Helper()
		before := responseDecodeCalls.Load()
		if err := run(); err != nil {
			t.Fatalf("decode path failed: %v", err)
		}
		return responseDecodeCalls.Load() - before
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
		delta := entryDelta(t, func() error {
			_, err := client.requestGoalWithStatus(context.Background(), http.MethodGet, "v1/goal", nil, "", nil, &target)
			return err
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
	})

	t.Run("requestGoalContractWithStatus", func(t *testing.T) {
		body, err := os.ReadFile(filepath.Join(controlRepositoryRoot(t), "contracts", "fixtures", "valid", "evidence-batch-response.basic.json"))
		if err != nil {
			t.Fatal(err)
		}
		client := machineResponse(t, string(body))
		var target contracts.SymmetryEvidenceBatchResponseV1
		delta := entryDelta(t, func() error {
			_, err := client.requestGoalContractWithStatus(context.Background(), http.MethodPost, "v1/evidence", nil, "", nil, contracts.EnvelopeEvidenceBatchResponse, &target)
			return err
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
	})

	t.Run("requestWithStatus", func(t *testing.T) {
		client := machineResponse(t, `{"value":1}`)
		var target struct {
			Value int `json:"value"`
		}
		delta := entryDelta(t, func() error {
			_, err := client.requestWithStatus(context.Background(), http.MethodGet, "v1/enroll", nil, "token", "", nil, &target)
			return err
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
	})

	t.Run("decodeStrictObjectJSON", func(t *testing.T) {
		var target struct {
			GoalID string `json:"goal_id"`
		}
		delta := entryDelta(t, func() error {
			return decodeStrictObjectJSON([]byte(`{"goal_id":"goal-1"}`), &target, "goal_id")
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
	})

	t.Run("decodeProviderActionObjectJSON", func(t *testing.T) {
		var target struct {
			Operation string `json:"operation"`
		}
		delta := entryDelta(t, func() error {
			return decodeProviderActionObjectJSON([]byte(`{"operation":"probe"}`), &target, "operation")
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
	})

	t.Run("decodeAPIError", func(t *testing.T) {
		delta := entryDelta(t, func() error {
			_ = decodeAPIError(http.StatusBadRequest, http.Header{}, []byte(`{"error":{"code":"invalid_request","message":"bad"}}`))
			return nil
		})
		if delta != 1 {
			t.Fatalf("entry calls = %d, want 1", delta)
		}
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
