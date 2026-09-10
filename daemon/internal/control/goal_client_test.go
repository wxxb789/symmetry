package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	goalRunID                 = "33333333-3333-4333-8333-333333333333"
	goalTaskID                = "44444444-4444-4444-8444-444444444444"
	goalEvidenceID            = "55555555-5555-4555-8555-555555555555"
	goalUsageID               = "66666666-6666-4666-8666-666666666666"
	goalResourceID            = "77777777-7777-4777-8777-777777777777"
	goalHash                  = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	goalContextHash           = "sha256:3a2e4ea0b0246c9e536658dad28d643a2d6deba9fe9a7bd7a8ec4ba5154ff10f"
	goalValidationContextHash = "sha256:6c643937e09d39cf849cf31a6fe023a8edad4175da3fa399847a0db2c825295c"
	goalEvidenceHash          = "sha256:88c59dff7c5897758b4315961f687117819b744faf505eed29babc2437828dd0"
	goalCommit                = "0123456789abcdef0123456789abcdef01234567"
	goalRuntimeID             = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	goalClaimID               = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	goalLeaseToken            = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	goalHandleID              = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	goalSessionID             = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

func TestGoalMachineEndpointsPropagateFenceAndStrictlyDecodeReceipts(t *testing.T) {
	tests := []struct {
		name  string
		call  func(context.Context, *Client) error
		check func(*testing.T, *http.Request)
		body  string
	}{
		{
			name: "attach session",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.AttachHarnessSession(ctx, goalRunID, goalSessionRequest())
				return err
			},
			check: func(t *testing.T, request *http.Request) {
				if request.Method != http.MethodPut || request.URL.Path != "/api/v1/runs/"+goalRunID+"/session" {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				body := readRequestBody(t, request)
				assertJSONField(t, body, "runtime_id", goalRuntimeID)
				assertJSONField(t, body, "runtime_epoch", float64(3))
				assertJSONField(t, body, "generation", float64(2))
				assertJSONField(t, body, "claim_id", goalClaimID)
				assertJSONField(t, body, "lease_token", goalLeaseToken)
				assertJSONField(t, body, "local_handle_id", goalHandleID)
				if _, present := body["run_id"]; present {
					t.Fatal("attach request duplicated authoritative run_id in the body")
				}
			},
			body: `{"session":{"id":"` + goalSessionID + `","run_id":"` + goalRunID + `","state":"busy","runtime_id":"` + goalRuntimeID + `","active_run_id":"` + goalRunID + `","local_handle_id":"` + goalHandleID + `","harness_kind":"codex","harness_version":"1.2.3","adapter_version":"symmetry-adapter-1","workspace_fingerprint":"workspace-fingerprint-1","workspace":"primary"}}`,
		},
		{
			name: "evidence",
			call: func(ctx context.Context, client *Client) error {
				evidence := goalEvidence(t)
				_, err := client.AppendEvidence(ctx, goalRunID, goalFence(), evidence)
				return err
			},
			check: func(t *testing.T, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/v1/runs/"+goalRunID+"/evidence" {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				body := readRequestBody(t, request)
				assertJSONField(t, body, "runtime_id", goalRuntimeID)
				assertJSONField(t, body, "run_id", goalRunID)
				assertJSONField(t, body, "evidence_id", goalEvidenceID)
				assertJSONField(t, body, "subject_hash", goalEvidenceHash)
			},
			body: `{"evidence":{"id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed"}}`,
		},
		{
			name: "usage",
			call: func(ctx context.Context, client *Client) error {
				usage := goalUsage(t)
				_, err := client.RecordUsage(ctx, goalRunID, goalFence(), usage)
				return err
			},
			check: func(t *testing.T, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/v1/runs/"+goalRunID+"/usage" {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				body := readRequestBody(t, request)
				assertJSONField(t, body, "runtime_id", goalRuntimeID)
				assertJSONField(t, body, "run_id", goalRunID)
				assertJSONField(t, body, "usage_id", goalUsageID)
				assertJSONField(t, body, "cost_microusd", "9007199254740991")
			},
			body: `{"usage":{"id":"` + goalUsageID + `","run_id":"` + goalRunID + `","usage_key":"final","cost_microusd":"9007199254740991","cost_basis":"reported"}}`,
		},
		{
			name: "context",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.FetchRunContext(ctx, goalRunID, goalFence())
				return err
			},
			check: func(t *testing.T, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/runs/"+goalRunID+"/context" {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				for key, want := range map[string]string{
					"runtime_id": goalRuntimeID, "runtime_epoch": "3", "generation": "2",
					"claim_id": goalClaimID, "lease_token": goalLeaseToken,
				} {
					if got := request.URL.Query().Get(key); got != want {
						t.Fatalf("query %s = %q, want %q", key, got, want)
					}
				}
				if request.Body != nil {
					data, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					if len(data) != 0 {
						t.Fatalf("context GET body = %q", data)
					}
				}
			},
			body: goalContextJSON(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				test.check(t, request)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			client := mustMachineClient(t, server)
			if err := test.call(context.Background(), client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGoalUsageReceiptRejectsNumericMicrousd(t *testing.T) {
	for _, cost := range []string{"1", "1.5", "9007199254740991"} {
		t.Run(cost, func(t *testing.T) {
			var receipt GoalUsageReceipt
			err := json.Unmarshal([]byte(`{"id":"`+goalUsageID+`","run_id":"`+goalRunID+`","usage_key":"final","cost_microusd":`+cost+`,"cost_basis":"reported"}`), &receipt)
			if err == nil || !strings.Contains(err.Error(), "cost_microusd must be a decimal string or null") {
				t.Fatalf("numeric cost_microusd error = %v", err)
			}
		})
	}
}

func TestGoalMachineEndpointsRejectStrictUnknownAndPrivateFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(*Client) error
	}{
		{
			name: "session unknown response field",
			body: `{"session":{"id":"` + goalSessionID + `","run_id":"` + goalRunID + `","state":"busy","raw_private":"secret"}}`,
			call: func(client *Client) error {
				_, err := client.AttachHarnessSession(context.Background(), goalRunID, goalSessionRequest())
				return err
			},
		},
		{
			name: "evidence unknown response field",
			body: `{"evidence":{"id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed","raw_private":"secret"}}`,
			call: func(client *Client) error {
				_, err := client.AppendEvidence(context.Background(), goalRunID, goalFence(), goalEvidence(t))
				return err
			},
		},
		{
			name: "usage unknown response field",
			body: `{"usage":{"id":"` + goalUsageID + `","run_id":"` + goalRunID + `","usage_key":"final","cost_microusd":"9007199254740991","cost_basis":"reported","raw_private":"secret"}}`,
			call: func(client *Client) error {
				_, err := client.RecordUsage(context.Background(), goalRunID, goalFence(), goalUsage(t))
				return err
			},
		},
		{
			name: "context private payload field",
			body: goalContextWithExtraSourceField("prompt"),
			call: func(client *Client) error {
				_, err := client.FetchRunContext(context.Background(), goalRunID, goalFence())
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			if err := test.call(mustMachineClient(t, server)); err == nil || !strings.Contains(err.Error(), "decode strict goal response") && !strings.Contains(err.Error(), "private context field") {
				t.Fatalf("error = %v, want strict response rejection", err)
			}
		})
	}
}

func TestGoalContextRejectsNormalizedPrivateKeyVariants(t *testing.T) {
	for _, key := range []string{"rawTranscript", "session-filename", "local_handle"} {
		t.Run(key, func(t *testing.T) {
			body := goalContextWithExtraSourceField(key)
			server := jsonServer(t, http.StatusOK, body, nil)
			defer server.Close()
			_, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
			if err == nil || !strings.Contains(err.Error(), "decode strict goal response") {
				t.Fatalf("error = %v, want strict normalized private-key rejection", err)
			}
		})
	}
}

func TestGoalClientStrictlyValidatesResponseUUIDStateAndScope(t *testing.T) {
	cases := []struct {
		name string
		body string
		call func(*Client) error
	}{
		{
			name: "session state",
			body: `{"session":{"id":"` + goalSessionID + `","run_id":"` + goalRunID + `","state":"available"}}`,
			call: func(client *Client) error {
				_, err := client.AttachHarnessSession(context.Background(), goalRunID, goalSessionRequest())
				return err
			},
		},
		{
			name: "evidence UUID",
			body: `{"evidence":{"id":"not-a-uuid","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed"}}`,
			call: func(client *Client) error {
				_, err := client.AppendEvidence(context.Background(), goalRunID, goalFence(), goalEvidence(t))
				return err
			},
		},
		{
			name: "context generation scope",
			body: strings.Replace(goalContextJSON(), `"generation":2`, `"generation":3`, 1),
			call: func(client *Client) error {
				_, err := client.FetchRunContext(context.Background(), goalRunID, goalFence())
				return err
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			if err := test.call(mustMachineClient(t, server)); err == nil {
				t.Fatal("accepted response outside the requested UUID/state/scope")
			}
		})
	}
}

func TestGoalClientRejectsReceiptCorrelationMismatches(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(*Client) error
	}{
		{
			name: "session harness version",
			body: `{"session":{"id":"` + goalSessionID + `","run_id":"` + goalRunID + `","state":"busy","runtime_id":"` + goalRuntimeID + `","active_run_id":"` + goalRunID + `","local_handle_id":"` + goalHandleID + `","harness_kind":"codex","harness_version":"wrong","adapter_version":"symmetry-adapter-1","workspace_fingerprint":"workspace-fingerprint-1"}}`,
			call: func(client *Client) error {
				_, err := client.AttachHarnessSession(context.Background(), goalRunID, goalSessionRequest())
				return err
			},
		},
		{
			name: "evidence key",
			body: `{"evidence":{"id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"other","kind":"check","subject_hash":"` + goalEvidenceHash + `","verdict":"passed"}}`,
			call: func(client *Client) error {
				_, err := client.AppendEvidence(context.Background(), goalRunID, goalFence(), goalEvidence(t))
				return err
			},
		},
		{
			name: "usage cost",
			body: `{"usage":{"id":"` + goalUsageID + `","run_id":"` + goalRunID + `","usage_key":"final","cost_microusd":"1","cost_basis":"reported"}}`,
			call: func(client *Client) error {
				_, err := client.RecordUsage(context.Background(), goalRunID, goalFence(), goalUsage(t))
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			if err := test.call(mustMachineClient(t, server)); err == nil {
				t.Fatal("accepted receipt that did not correlate with the request")
			}
		})
	}
}

func TestGoalContextRejectsLegacyProjectionAndIncompleteCanonicalSnapshots(t *testing.T) {
	missingSources := strings.Replace(
		goalContextJSON(),
		`"sources":[{"resource_id":"22222222-2222-4222-8222-222222222222","source_kind":"approved_goal","source_revision":"goal-revision:1","content_hash":"`+goalHash+`","observed_at":"2026-09-09T12:00:00Z","trust":"trusted_policy","required":true,"content":{"kind":"excerpt","value":"The approved objective and authority policy."}}],`,
		``,
		1,
	)
	for _, test := range []struct {
		name string
		body string
	}{
		{
			name: "legacy projection",
			body: `{"goal_id":"11111111-1111-4111-8111-111111111111","task_id":"` + goalTaskID + `","run_id":"` + goalRunID + `","generation":2,"session_id":"` + goalSessionID + `","context":{"id":"88888888-8888-4888-8888-888888888888","goal_id":"11111111-1111-4111-8111-111111111111","work_item_id":"99999999-9999-4999-8999-999999999999","goal_revision":1,"schema_version":1,"content_hash":"` + goalHash + `","payload":{},"inserted_at":"2026-09-09T12:00:00Z"}}`,
		},
		{
			name: "wrong schema version",
			body: strings.Replace(goalContextJSON(), "symmetry.context_snapshot.v1", "symmetry.context_snapshot.v2", 1),
		},
		{
			name: "missing mandatory source section",
			body: missingSources,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			if _, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence()); err == nil {
				t.Fatal("accepted a non-canonical or incomplete context snapshot")
			}
		})
	}
}

func TestGoalContextAcceptsDeterministicCanonicalContentHash(t *testing.T) {
	server := jsonServer(t, http.StatusOK, goalContextJSON(), nil)
	defer server.Close()

	response, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
	if err != nil {
		t.Fatalf("valid canonical context rejected: %v", err)
	}
	bindings := response.Context.WorkContract.ValidationBindings
	if len(bindings) != 1 {
		t.Fatalf("validation bindings = %#v, want one frozen binding", bindings)
	}
	binding := bindings[0]
	if binding.ProfileName != "default-checks" || binding.Kind != "check" || binding.ProfileDigest != goalHash ||
		len(binding.AllowedRuntimeIDs) != 1 || binding.AllowedRuntimeIDs[0] != goalRuntimeID {
		t.Fatalf("validation binding = %#v, want canonical frozen binding", binding)
	}
}

func TestGoalContextStrictlyDecodesChangeTarget(t *testing.T) {
	validTarget := `{"kind":"branches","source_branch":"codex/goal-0006","target_branch":"main"}`
	malformedTarget := `{"kind":"branches","source_branch":"codex/goal-0006","target_branch":"main","pull_request_url":"https://example.test/pr/1"}`
	unknownFieldTarget := `{"kind":"branches","source_branch":"codex/goal-0006","target_branch":"main","extra":true}`

	for _, test := range []struct {
		name       string
		body       string
		wantTarget bool
		wantErr    bool
	}{
		{name: "null", body: goalContextJSON()},
		{name: "branches", body: goalContextWithChangeTarget(t, validTarget), wantTarget: true},
		{name: "malformed", body: goalContextWithChangeTarget(t, malformedTarget), wantErr: true},
		{name: "unknown field", body: goalContextWithChangeTarget(t, unknownFieldTarget), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()

			response, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
			if test.wantErr {
				if err == nil {
					t.Fatal("accepted malformed change_target")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.wantTarget {
				target := response.Context.WorkContract.ChangeTarget
				if target == nil || target.Kind != protocol.ProviderChangeTargetBranches ||
					target.SourceBranch == nil || *target.SourceBranch != "codex/goal-0006" ||
					target.TargetBranch == nil || *target.TargetBranch != "main" {
					t.Fatalf("change_target = %#v, want preserved branches target", target)
				}
				return
			}
			if response.Context.WorkContract.ChangeTarget != nil {
				t.Fatalf("change_target = %#v, want nil", response.Context.WorkContract.ChangeTarget)
			}
		})
	}
}

func TestGoalContextRejectsTamperedCanonicalContentHash(t *testing.T) {
	body := strings.Replace(
		goalContextJSON(),
		"Deliver a verified change to the repository.",
		"Deliver a tampered change to the repository.",
		1,
	)
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	_, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
	if err == nil || !strings.Contains(err.Error(), "content_hash does not match canonical snapshot") {
		t.Fatalf("error = %v, want canonical content hash mismatch", err)
	}
}

func TestGoalContextContentHashIncludesValidationBindings(t *testing.T) {
	body := strings.Replace(
		goalContextJSON(),
		`"profile_digest":"`+goalHash+`","allowed_runtime_ids"`,
		`"profile_digest":"sha256:5555555555555555555555555555555555555555555555555555555555555555","allowed_runtime_ids"`,
		1,
	)
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	_, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
	if err == nil || !strings.Contains(err.Error(), "content_hash does not match canonical snapshot") {
		t.Fatalf("error = %v, want canonical validation binding hash mismatch", err)
	}
}

func TestGoalContextRejectsAmbiguousNumericLiteral(t *testing.T) {
	body := strings.Replace(goalContextJSON(), `"goal_revision":1`, `"goal_revision":1.0`, 1)
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	if _, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence()); err == nil {
		t.Fatal("accepted a non-canonical numeric literal in the context snapshot")
	}
}

func TestGoalContextRejectsRawSchemaViolationBeforeSemanticValidation(t *testing.T) {
	body := goalContextJSON()
	sourceStart := strings.Index(body, `"sources":[`)
	sourceEnd := strings.Index(body[sourceStart:], `],`)
	if sourceStart < 0 || sourceEnd < 0 {
		t.Fatal("test context fixture does not contain a sources array")
	}
	body = body[:sourceStart] + `"sources":null` + body[sourceStart+sourceEnd+1:]
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	_, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
	if err == nil || !strings.Contains(err.Error(), "decode context snapshot schema") || !strings.Contains(err.Error(), "validate context-snapshot schema") {
		t.Fatalf("error = %v, want generated context schema validation error", err)
	}
}

func TestGoalContextRejectsInvalidValidationBindings(t *testing.T) {
	bindings := `"validation_bindings":[{"profile_name":"default-checks","kind":"check","profile_digest":"` + goalHash + `","allowed_runtime_ids":["` + goalRuntimeID + `"]}]`
	for _, test := range []struct {
		name        string
		replacement string
	}{
		{name: "missing", replacement: ``},
		{name: "invalid profile", replacement: strings.Replace(bindings, `"profile_name":"default-checks"`, `"profile_name":"invalid profile"`, 1)},
		{name: "invalid kind", replacement: strings.Replace(bindings, `"kind":"check"`, `"kind":"artifact"`, 1)},
		{name: "invalid digest", replacement: strings.Replace(bindings, goalHash, "sha256:invalid", 1)},
		{name: "missing required profile binding", replacement: `"validation_bindings":[]`},
		{name: "empty runtime IDs", replacement: strings.Replace(bindings, `"allowed_runtime_ids":["`+goalRuntimeID+`"]`, `"allowed_runtime_ids":[]`, 1)},
		{name: "duplicate runtime IDs", replacement: strings.Replace(bindings, `"allowed_runtime_ids":["`+goalRuntimeID+`"]`, `"allowed_runtime_ids":["`+goalRuntimeID+`","`+goalRuntimeID+`"]`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(goalContextJSON(), bindings, test.replacement, 1)
			server := jsonServer(t, http.StatusOK, body, nil)
			defer server.Close()
			if _, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence()); err == nil {
				t.Fatal("accepted invalid validation bindings")
			}
		})
	}
}

func TestGoalContextAllowsNullSessionOnlyForValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "validation", body: strings.Replace(strings.Replace(strings.Replace(goalContextJSON(), `"purpose":"implement"`, `"purpose":"validate"`, 1), goalContextHash, goalValidationContextHash, 1), `"session_id":"`+goalSessionID+`"`, `"session_id":null`, 1)},
		{name: "implementation", body: strings.Replace(goalContextJSON(), `"session_id":"`+goalSessionID+`"`, `"session_id":null`, 1), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			response, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
			if test.wantErr {
				if err == nil {
					t.Fatal("accepted null session outside validation context")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if response.SessionID != nil {
				t.Fatalf("session_id = %q, want nil", *response.SessionID)
			}
		})
	}
}

func TestGoalContextPlanningUsesNullWorkItemIdentity(t *testing.T) {
	planBody := goalContextWithPurposeAndWorkItem(t, "plan", []byte("null"))
	server := jsonServer(t, http.StatusOK, planBody, nil)
	response, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence())
	server.Close()
	if err != nil {
		t.Fatalf("valid planning context rejected: %v", err)
	}
	if response.Context.WorkContract.Purpose != "plan" || response.Context.WorkItemID != nil {
		t.Fatalf("planning context = %#v, want plan with null work item", response.Context)
	}

	for name, body := range map[string]string{
		"plan with work item":           goalContextWithPurposeAndWorkItem(t, "plan", []byte(`"99999999-9999-4999-8999-999999999999"`)),
		"implement with null work item": goalContextWithPurposeAndWorkItem(t, "implement", []byte("null")),
	} {
		t.Run(name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, body, nil)
			defer server.Close()
			if _, err := mustMachineClient(t, server).FetchRunContext(context.Background(), goalRunID, goalFence()); err == nil {
				t.Fatal("accepted context with an invalid purpose/work-item identity pair")
			}
		})
	}
}

func TestGoalMachineEndpointsPreserveTypedHTTPErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		code   ErrorCode
	}{
		{name: "unauthenticated", status: http.StatusUnauthorized, code: Unauthenticated},
		{name: "forbidden", status: http.StatusForbidden, code: Forbidden},
		{name: "conflict", status: http.StatusConflict, code: StateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, test.status, `{"error":{"code":"`+string(test.code)+`","message":"rejected"}}`, nil)
			defer server.Close()
			_, err := mustMachineClient(t, server).AttachHarnessSession(context.Background(), goalRunID, goalSessionRequest())
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != test.status || apiError.Code != test.code {
				t.Fatalf("error = %#v, want APIError %d/%s", err, test.status, test.code)
			}
		})
	}
}

func goalFence() protocol.Fence {
	return protocol.Fence{RuntimeID: goalRuntimeID, RuntimeEpoch: 3, Generation: 2, ClaimID: goalClaimID, LeaseToken: goalLeaseToken}
}

func goalSessionRequest() GoalSessionAttachRequest {
	return GoalSessionAttachRequest{
		Fence:                goalFence(),
		LocalHandleID:        goalHandleID,
		HarnessKind:          "codex",
		HarnessVersion:       "1.2.3",
		AdapterVersion:       "symmetry-adapter-1",
		WorkspaceFingerprint: "workspace-fingerprint-1",
		Workspace:            "primary",
	}
}

func goalEvidence(t *testing.T) protocol.Evidence {
	t.Helper()
	value, err := protocol.ParseEvidence([]byte(`{"schema_version":"symmetry.evidence.v1","evidence_id":"` + goalEvidenceID + `","run_id":"` + goalRunID + `","evidence_key":"tests:default","kind":"check","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + goalCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"subject_hash":"` + goalEvidenceHash + `","source_ref":{"kind":"check","ref":"check-run-1","validator_profile":"default-checks","subject_hash":"` + goalEvidenceHash + `"},"source_revision":"profile:default-checks","validator_profile":"default-checks","verdict":"passed","payload":{"predicate_id":"tests","subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + goalCommit + `","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"profile_digest":"` + goalHash + `","command_argv_digest":"` + goalHash + `","exit_code":0,"subject_hash":"` + goalEvidenceHash + `","started_at":"2026-09-09T12:00:00Z","finished_at":"2026-09-09T12:01:00Z","output_ref":{"kind":"artifact","value":"artifact:check-output-1"}},"observed_at":"2026-09-09T12:01:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func goalUsage(t *testing.T) protocol.Usage {
	t.Helper()
	value, err := protocol.ParseUsage([]byte(`{"schema_version":"symmetry.usage.v1","usage_id":"` + goalUsageID + `","run_id":"` + goalRunID + `","usage_key":"final","provider":"codex","model":"implementation-default","input_tokens":128,"output_tokens":0,"cached_input_tokens":0,"cost_microusd":"9007199254740991","cost_basis":"reported","price_version":"price-1","supersedes_id":null,"observed_at":"2026-09-09T12:01:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func goalContextJSON() string {
	return `{"goal_id":"11111111-1111-4111-8111-111111111111","task_id":"` + goalTaskID + `","run_id":"` + goalRunID + `","generation":2,"session_id":"` + goalSessionID + `","context":{"schema_version":"symmetry.context_snapshot.v1","snapshot_id":"88888888-8888-4888-8888-888888888888","goal_id":"11111111-1111-4111-8111-111111111111","goal_revision":1,"work_item_id":"99999999-9999-4999-8999-999999999999","content_hash":"` + goalContextHash + `","created_at":"2026-09-09T12:00:00Z","approved_goal":{"goal_id":"11111111-1111-4111-8111-111111111111","revision":1,"objective":"Deliver a verified change to the repository.","authority_policy":{"operator_required_for_scope_change":true,"operator_required_for_completion":true,"publication_allowed":false,"allowed_actions":["activate","admit_task","achieve"]}},"work_contract":{"title":"Implement the bounded change","description":"Change only the approved repository scope and produce checkable evidence.","purpose":"implement","change_target":null,"acceptance":{"schema_version":"symmetry.acceptance.v1","description":"The required check passes.","predicates":[{"id":"tests","kind":"check","validator_profile":"default-checks"}]},"validation_bindings":[{"profile_name":"default-checks","kind":"check","profile_digest":"` + goalHash + `","allowed_runtime_ids":["` + goalRuntimeID + `"]}]},"subject":{"resource_id":"22222222-2222-4222-8222-222222222222","commit":"` + goalCommit + `","tree_digest":"` + goalHash + `"},"sources":[{"resource_id":"22222222-2222-4222-8222-222222222222","source_kind":"approved_goal","source_revision":"goal-revision:1","content_hash":"` + goalHash + `","observed_at":"2026-09-09T12:00:00Z","trust":"trusted_policy","required":true,"content":{"kind":"excerpt","value":"The approved objective and authority policy."}}],"current_decisions":[],"validated_evidence":[],"failed_attempts":[],"advisory_recall":[],"next_action":null,"size":{"mandatory_bytes":1024,"optional_bytes":0,"total_bytes":1024,"byte_budget":65536,"token_estimate":null}}}`
}

func goalContextWithChangeTarget(t *testing.T, target string) string {
	t.Helper()
	body := strings.Replace(goalContextJSON(), `"change_target":null`, `"change_target":`+target, 1)

	var response map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(response["context"], &snapshot); err != nil {
		t.Fatalf("decode test context: %v", err)
	}
	delete(snapshot, "content_hash")
	withoutHash, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode test context without hash: %v", err)
	}
	canonical, err := protocol.CanonicalizeJSON(withoutHash)
	if err != nil {
		t.Fatalf("canonicalize test context: %v", err)
	}
	snapshot["content_hash"] = json.RawMessage(fmt.Sprintf(`"sha256:%x"`, sha256.Sum256(canonical)))
	response["context"], err = json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode test context: %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode test response: %v", err)
	}
	return string(encoded)
}

func goalContextWithPurposeAndWorkItem(t *testing.T, purpose string, workItem json.RawMessage) string {
	t.Helper()
	var response map[string]json.RawMessage
	if err := json.Unmarshal([]byte(goalContextJSON()), &response); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(response["context"], &snapshot); err != nil {
		t.Fatalf("decode test context: %v", err)
	}
	snapshot["work_item_id"] = workItem
	var contract map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["work_contract"], &contract); err != nil {
		t.Fatalf("decode test work contract: %v", err)
	}
	contract["purpose"] = json.RawMessage(fmt.Sprintf(`%q`, purpose))
	encodedContract, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("encode test work contract: %v", err)
	}
	snapshot["work_contract"] = encodedContract
	delete(snapshot, "content_hash")
	withoutHash, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode test context without hash: %v", err)
	}
	canonical, err := protocol.CanonicalizeJSON(withoutHash)
	if err != nil {
		t.Fatalf("canonicalize test context: %v", err)
	}
	snapshot["content_hash"] = json.RawMessage(fmt.Sprintf(`"sha256:%x"`, sha256.Sum256(canonical)))
	response["context"], err = json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode test context: %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("encode test response: %v", err)
	}
	return string(encoded)
}

func goalContextWithExtraSourceField(key string) string {
	return strings.Replace(
		goalContextJSON(),
		`{"kind":"excerpt","value":"The approved objective and authority policy."}`,
		`{"kind":"excerpt","value":"The approved objective and authority policy.","`+key+`":"secret"}`,
		1,
	)
}

func readRequestBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	data, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func assertJSONField(t *testing.T, body map[string]any, key string, want any) {
	t.Helper()
	got, ok := body[key]
	if !ok || got != want {
		t.Fatalf("body[%q] = %#v, want %#v", key, got, want)
	}
}
