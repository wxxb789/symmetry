package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestClaimRejectsUntrustedResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "missing run ID", response: claimResponse(`"run_id":""`)},
		{name: "mismatched run ID", response: claimResponse(`"run_id":"other-run"`)},
		{name: "mismatched generation", response: claimResponse(`"generation":3`)},
		{name: "mismatched claim ID", response: claimResponse(`"claim_id":"other-claim"`)},
		{name: "missing task ID", response: claimResponse(`"task_id":""`)},
		{name: "missing lease token", response: claimResponse(`"lease_token":""`)},
		{name: "missing lease expiry", response: claimResponse(`"lease_expires_at":null`)},
		{name: "null work input", response: claimResponse(`"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":null}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.response, nil)
			defer server.Close()
			client := mustMachineClient(t, server)
			_, err := client.Claim(context.Background(), "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"})
			if err == nil || !strings.Contains(err.Error(), "invalid claim response") {
				t.Fatalf("error = %v, want invalid claim response", err)
			}
		})
	}
}

func TestClientRejectsIncompleteSuccessResponses(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
		invoke    func(*Client, *EnrollmentClient, *OperatorClient) error
	}{
		{
			name: "enroll", body: `{}`, wantError: "invalid enroll response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := enrollment.Enroll(context.Background(), "enrollment-token", protocol.EnrollRequest{})
				return err
			},
		},
		{
			name: "session", body: `{"runtimes":[],"heartbeat_interval_ms":1,"poll_interval_ms":1,"lease_duration_ms":1,"websocket_path":"/socket"}`, wantError: "invalid session registration response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.RegisterSession(context.Background(), protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default"}}})
				return err
			},
		},
		{
			name: "snapshot", body: `{"assignments":[],"commands":[]}`, wantError: "invalid runtime snapshot response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.Work(context.Background(), "runtime-1", 3)
				return err
			},
		},
		{
			name: "renew", body: `{"commands":[]}`, wantError: "invalid lease heartbeat response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.RenewLease(context.Background(), "run-1", protocol.LeaseHeartbeatRequest{})
				return err
			},
		},
		{
			name: "reconcile", body: `{"decisions":[],"assignments":[]}`, wantError: "invalid reconcile response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.Reconcile(context.Background(), "runtime-1", protocol.ReconcileRequest{})
				return err
			},
		},
		{
			name: "task", body: `{"state":"queued"}`, wantError: "invalid get task response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := operator.GetTask(context.Background(), "task-1")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			machine := mustMachineClient(t, server)
			enrollment, err := NewEnrollmentClient(server.URL+"/api", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			operator, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			err = test.invoke(machine, enrollment, operator)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestRegisterSessionAcceptsRuntimesInAnyOrder(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{"runtimes":[{"runtime_key":"secondary","runtime_id":"runtime-2","runtime_epoch":4},{"runtime_key":"primary","runtime_id":"runtime-1","runtime_epoch":3}],"heartbeat_interval_ms":5000,"poll_interval_ms":5000,"lease_duration_ms":30000,"websocket_path":"/socket"}`, nil)
	defer server.Close()

	response, err := mustMachineClient(t, server).RegisterSession(context.Background(), protocol.SessionRegistrationRequest{
		Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "primary"}, {RuntimeKey: "secondary"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Runtimes) != 2 {
		t.Fatalf("runtime count = %d, want 2", len(response.Runtimes))
	}
}

func TestClientRejectsUnsafePathIDsBeforeRequest(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*Client) error
	}{
		{name: "empty runtime", invoke: func(client *Client) error { _, err := client.Work(context.Background(), "", 3); return err }},
		{name: "runtime dot segment", invoke: func(client *Client) error { _, err := client.Work(context.Background(), "..", 3); return err }},
		{name: "runtime slash", invoke: func(client *Client) error { _, err := client.Work(context.Background(), "runtime/1", 3); return err }},
		{name: "run percent", invoke: func(client *Client) error {
			_, err := client.Claim(context.Background(), "run%2F1", protocol.ClaimRequest{})
			return err
		}},
		{name: "command slash", invoke: func(client *Client) error {
			return client.AcknowledgeCommand(context.Background(), "command/1", protocol.CommandAcknowledgement{})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { calls.Add(1) }))
			defer server.Close()
			err := test.invoke(mustMachineClient(t, server))
			if err == nil || !strings.Contains(err.Error(), "must be a non-empty safe path segment") {
				t.Fatalf("error = %v, want invalid ID", err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("calls = %d, want 0", got)
			}
		})
	}
}

func TestAcknowledgeCommandRejectsMismatchedIDBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { calls.Add(1) }))
	defer server.Close()

	err := mustMachineClient(t, server).AcknowledgeCommand(context.Background(), "command-1", protocol.CommandAcknowledgement{CommandID: "command-2"})
	if err == nil || !strings.Contains(err.Error(), "command ID does not match") {
		t.Fatalf("error = %v, want mismatched command ID", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestOversizedErrorResponseKeepsTypedFallback(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		retryAfter     string
		wantCode       ErrorCode
		wantRetryAfter time.Duration
	}{
		{name: "conflict", status: http.StatusConflict, wantCode: StateConflict},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "4", wantCode: RateLimited, wantRetryAfter: 4 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `{"error":{"code":"ownership_lost","message":"` + strings.Repeat("x", 256) + `"}}`
			server := jsonServer(t, test.status, body, map[string]string{"Retry-After": test.retryAfter})
			defer server.Close()
			client, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client(), WithMaxResponseBytes(32))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetTask(context.Background(), "task-1")
			var apiError *APIError
			if !errors.As(err, &apiError) {
				t.Fatalf("error = %T %v, want APIError", err, err)
			}
			if apiError.StatusCode != test.status || apiError.Code != test.wantCode || apiError.RetryAfter != test.wantRetryAfter {
				t.Fatalf("APIError = %#v", apiError)
			}
		})
	}
}

func TestOperatorClientUsesDedicatedToken(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{"task_id":"task-1","state":"queued"}`, func(request *http.Request) {
		if got, want := request.Header.Get("Authorization"), "Bearer operator-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
	})
	defer server.Close()

	operator, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operator.GetTask(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}

	machine := mustMachineClient(t, server)
	if _, ok := any(machine).(interface {
		GetTask(context.Context, string) (protocol.Task, error)
	}); ok {
		t.Fatal("machine Client must not implement task APIs")
	}
}

func mustMachineClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(server.URL+"/api", machineToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func jsonServer(t *testing.T, status int, body string, beforeWrite any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch value := beforeWrite.(type) {
		case func(*http.Request):
			value(request)
		case map[string]string:
			for key, headerValue := range value {
				if headerValue != "" {
					writer.Header().Set(key, headerValue)
				}
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
}

func claimResponse(replacement string) string {
	defaults := map[string]any{
		"run_id":           "run-1",
		"task_id":          "task-1",
		"generation":       2,
		"claim_id":         "claim-1",
		"lease_token":      "lease-1",
		"lease_expires_at": "2026-09-02T00:00:30Z",
		"work": map[string]any{
			"goal":          "work",
			"agent_profile": "codex",
			"workspace":     "primary",
			"input":         map[string]any{},
		},
	}
	var override map[string]json.RawMessage
	if err := json.Unmarshal([]byte("{"+replacement+"}"), &override); err != nil {
		panic(err)
	}
	for key, value := range override {
		defaults[key] = value
	}
	encoded, err := json.Marshal(defaults)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
