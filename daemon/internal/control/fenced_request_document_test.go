package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestGoalMachineRequestBodiesAreFenceBesideTheSchemaDocument(t *testing.T) {
	fence := goalFence()
	evidence := goalEvidence(t)
	batch := GoalEvidenceBatch{
		SchemaVersion: "symmetry.evidence_batch.v1",
		RunID:         goalRunID,
		Items:         []protocol.Evidence{evidence},
	}
	usage := goalUsage(t)

	tests := []struct {
		name     string
		path     string
		envelope contracts.Envelope
		document any
		response string
		call     func(context.Context, *Client) error
	}{
		{
			name:     "single evidence",
			path:     "/api/v1/runs/" + goalRunID + "/evidence",
			envelope: contracts.EnvelopeEvidence,
			document: evidence,
			response: `{"evidence":{"id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed"}}`,
			call: func(ctx context.Context, client *Client) error {
				_, err := client.AppendEvidence(ctx, goalRunID, fence, evidence)
				return err
			},
		},
		{
			name:     "evidence batch",
			path:     "/api/v1/runs/" + goalRunID + "/evidence",
			envelope: contracts.EnvelopeEvidenceBatch,
			document: batch,
			response: `{"evidence_batch":{"run_id":"` + goalRunID + `","receipts":[{"id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed","observed_at":"2026-09-11T12:00:00Z","disposition":"created"}]}}`,
			call: func(ctx context.Context, client *Client) error {
				_, err := client.AppendEvidenceBatch(ctx, goalRunID, fence, []protocol.Evidence{evidence})
				return err
			},
		},
		{
			name:     "usage",
			path:     "/api/v1/runs/" + goalRunID + "/usage",
			envelope: contracts.EnvelopeUsage,
			document: usage,
			response: `{"usage":{"id":"` + goalUsageID + `","run_id":"` + goalRunID + `","usage_key":"final","cost_microusd":"9007199254740991","cost_basis":"reported"}}`,
			call: func(ctx context.Context, client *Client) error {
				_, err := client.RecordUsage(ctx, goalRunID, fence, usage)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var wire []byte

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != test.path {
					t.Errorf("request = %s %s, want POST %s", request.Method, request.URL.Path, test.path)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				wire = body
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(writer, test.response)
			}))
			defer server.Close()

			if err := test.call(context.Background(), mustMachineClient(t, server)); err != nil {
				t.Fatalf("request failed: %v", err)
			}

			fenceMembers := objectMembers(t, mustMarshal(t, fence))
			want := append(slices.Clone(fenceMembers), objectMembers(t, mustMarshal(t, test.document))...)
			slices.Sort(want)

			if got := objectMembers(t, wire); !slices.Equal(got, want) {
				t.Fatalf("wire body members = %v, want the fence beside the document = %v", got, want)
			}

			var decoded any
			if err := contracts.DecodeSchema(test.envelope, mustMarshal(t, test.document), &decoded); err != nil {
				t.Fatalf("the fence-stripped document must validate: %v", err)
			}

			err := contracts.DecodeSchema(test.envelope, wire, &decoded)
			if err == nil {
				t.Fatal("the fenced wire body must not validate as its request document")
			}
			if !strings.Contains(err.Error(), "additionalProperties") {
				t.Fatalf("wire body refusal = %v, want an additionalProperties error", err)
			}
			for _, member := range fenceMembers {
				if !strings.Contains(err.Error(), member) {
					t.Fatalf("wire body refusal = %v, want the fence member %q named", err, member)
				}
			}
		})
	}
}

func objectMembers(t *testing.T, data []byte) []string {
	t.Helper()

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}

	members := make([]string, 0, len(object))
	for member := range object {
		members = append(members, member)
	}
	slices.Sort(members)
	return members
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
