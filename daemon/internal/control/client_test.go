package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const machineToken = "machine-token"

func TestClientProtocolRequests(t *testing.T) {
	now := time.Date(2026, time.September, 2, 0, 0, 30, 0, time.UTC)
	var enrollmentClient *EnrollmentClient
	var operatorClient *OperatorClient
	tests := []struct {
		name       string
		invoke     func(context.Context, *Client) error
		method     string
		path       string
		wantAuth   string
		wantHeader string
		wantBody   string
		response   string
	}{
		{
			name: "enroll", method: http.MethodPost, path: "/api/v1/daemon/enroll", wantAuth: "Bearer enrollment-token",
			wantBody: `{"machine":{"name":"builder-01"}}`, response: `{"machine_id":"machine-1","machine_token":"issued-token"}`,
			invoke: func(ctx context.Context, client *Client) error {
				response, err := enrollmentClient.Enroll(ctx, "enrollment-token", protocol.EnrollRequest{Machine: protocol.MachineEnrollment{Name: "builder-01"}})
				if err == nil && (response.MachineID != "machine-1" || response.MachineToken != "issued-token") {
					return fmt.Errorf("unexpected enrollment response: %#v", response)
				}
				return err
			},
		},
		{
			name: "register session", method: http.MethodPost, path: "/api/v1/daemon/sessions", wantAuth: "Bearer " + machineToken,
			wantBody: `{"daemon_instance_id":"daemon-1","runtimes":[{"runtime_key":"default","name":"Local Codex","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{}}]}`,
			response: `{"runtimes":[{"runtime_key":"default","runtime_id":"runtime-1","runtime_epoch":3}],"heartbeat_interval_ms":5000,"poll_interval_ms":5000,"lease_duration_ms":30000,"websocket_path":"/socket/websocket?vsn=2.0.0"}`,
			invoke: func(ctx context.Context, client *Client) error {
				response, err := client.RegisterSession(ctx, protocol.SessionRegistrationRequest{DaemonInstanceID: "daemon-1", Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default", Name: "Local Codex", Capacity: 1, AgentProfile: "codex", Workspace: "primary", Capabilities: json.RawMessage(`{}`)}}})
				if err == nil && (len(response.Runtimes) != 1 || response.Runtimes[0].RuntimeEpoch != 3) {
					return fmt.Errorf("unexpected session response: %#v", response)
				}
				return err
			},
		},
		{
			name: "runtime heartbeat", method: http.MethodPost, path: "/api/v1/runtimes/runtime-1/heartbeat", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_epoch":3,"active_runs":[]}`, response: snapshotJSON(now),
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Heartbeat(ctx, "runtime-1", protocol.RuntimeHeartbeatRequest{RuntimeEpoch: 3, ActiveRuns: []protocol.ActiveRun{}})
				return err
			},
		},
		{
			name: "work", method: http.MethodGet, path: "/api/v1/runtimes/runtime-1/work?runtime_epoch=3", wantAuth: "Bearer " + machineToken,
			response: snapshotJSON(now),
			invoke: func(ctx context.Context, client *Client) error {
				response, err := client.Work(ctx, "runtime-1", 3)
				if err == nil && len(response.Assignments) != 1 {
					return fmt.Errorf("assignments = %d", len(response.Assignments))
				}
				return err
			},
		},
		{
			name: "claim", method: http.MethodPost, path: "/api/v1/runs/run-1/claim", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1"}`,
			response: `{"run_id":"run-1","task_id":"task-1","generation":2,"claim_id":"claim-1","lease_token":"lease-1","lease_expires_at":"2026-09-02T00:00:30Z","work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Claim(ctx, "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"})
				return err
			},
		},
		{
			name: "renew lease", method: http.MethodPost, path: "/api/v1/runs/run-1/heartbeat", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1"}`,
			response: `{"lease_expires_at":"2026-09-02T00:00:45Z","commands":[]}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.RenewLease(ctx, "run-1", protocol.LeaseHeartbeatRequest{Fence: protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1", LeaseToken: "lease-1"}})
				return err
			},
		},
		{
			name: "append events", method: http.MethodPost, path: "/api/v1/runs/run-1/events", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","events":[{"event_id":"event-1","sequence":4,"kind":"progress","occurred_at":"2026-09-02T00:00:10Z","payload":{"message":"running"}}]}`,
			response: `{}`,
			invoke: func(ctx context.Context, client *Client) error {
				return client.AppendEvents(ctx, "run-1", protocol.AppendEventsRequest{Fence: fence(), Events: []protocol.RunEvent{{EventID: "event-1", Sequence: 4, Kind: "progress", OccurredAt: time.Date(2026, time.September, 2, 0, 0, 10, 0, time.UTC), Payload: json.RawMessage(`{"message":"running"}`)}}})
			},
		},
		{
			name: "transition", method: http.MethodPost, path: "/api/v1/runs/run-1/state", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","transition_id":"transition-1","state":"waiting_for_input","payload":{"question":"branch"}}`,
			response: `{}`,
			invoke: func(ctx context.Context, client *Client) error {
				return client.Transition(ctx, "run-1", protocol.StateTransitionRequest{Fence: fence(), TransitionID: "transition-1", State: "waiting_for_input", Payload: json.RawMessage(`{"question":"branch"}`)})
			},
		},
		{
			name: "reconcile", method: http.MethodPost, path: "/api/v1/runtimes/runtime-1/reconcile", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_epoch":3,"runs":[]}`, response: `{"decisions":[],"assignments":[],"commands":[]}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Reconcile(ctx, "runtime-1", protocol.ReconcileRequest{RuntimeEpoch: 3, Runs: []protocol.ReconcileRun{}})
				return err
			},
		},
		{
			name: "acknowledge command", method: http.MethodPost, path: "/api/v1/commands/command-1/ack", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","run_id":"run-1","command_id":"command-1","outcome":"applied","ack_id":"ack-1"}`,
			response: `{}`,
			invoke: func(ctx context.Context, client *Client) error {
				return client.AcknowledgeCommand(ctx, "command-1", protocol.CommandAcknowledgement{Fence: fence(), RunID: "run-1", CommandID: "command-1", Outcome: "applied", AckID: "ack-1"})
			},
		},
		{
			name: "submit task", method: http.MethodPost, path: "/api/v1/tasks", wantAuth: "Bearer operator-token", wantHeader: "submit-1",
			wantBody: `{"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}`, response: `{"task_id":"task-1","state":"queued"}`,
			invoke: func(ctx context.Context, client *Client) error {
				response, err := operatorClient.SubmitTask(ctx, "submit-1", protocol.TaskSubmitRequest{Work: work()})
				if err == nil && response.TaskID != "task-1" {
					return fmt.Errorf("task id = %q", response.TaskID)
				}
				return err
			},
		},
		{
			name: "get task", method: http.MethodGet, path: "/api/v1/tasks/task-1", wantAuth: "Bearer operator-token",
			response: `{"task_id":"task-1","state":"queued","future_field":{"kept":"compatible"}}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := operatorClient.GetTask(ctx, "task-1")
				return err
			},
		},
		{
			name: "cancel task", method: http.MethodPost, path: "/api/v1/tasks/task-1/cancel", wantAuth: "Bearer operator-token",
			wantBody: `{}`, response: `{"task_id":"task-1","state":"cancelled"}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := operatorClient.CancelTask(ctx, "task-1")
				return err
			},
		},
		{
			name: "submit input", method: http.MethodPost, path: "/api/v1/tasks/task-1/input", wantAuth: "Bearer operator-token", wantHeader: "input-1",
			wantBody: `{"input":{"answer":"main"}}`, response: `{"task_id":"task-1","state":"waiting_for_input"}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := operatorClient.SubmitInput(ctx, "task-1", "input-1", protocol.TaskInputRequest{Input: json.RawMessage(`{"answer":"main"}`)})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != test.method {
					t.Errorf("method = %s, want %s", request.Method, test.method)
				}
				path := request.URL.RequestURI()
				if path != test.path {
					t.Errorf("path = %q, want %q", path, test.path)
				}
				if got := request.Header.Get("Authorization"); got != test.wantAuth {
					t.Errorf("Authorization = %q, want %q", got, test.wantAuth)
				}
				if test.wantHeader != "" && request.Header.Get("Idempotency-Key") != test.wantHeader {
					t.Errorf("Idempotency-Key = %q, want %q", request.Header.Get("Idempotency-Key"), test.wantHeader)
				}
				if test.wantBody != "" {
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					assertJSONEqual(t, test.wantBody, string(body))
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.response)
			}))
			defer server.Close()

			client, err := NewClient(server.URL+"/api", machineToken, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			enrollmentClient, err = NewEnrollmentClient(server.URL+"/api", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			operatorClient, err = NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := test.invoke(context.Background(), client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClientTypedErrorsAndResponseValidation(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		limit     int64
		wantCode  ErrorCode
		wantError string
	}{
		{name: "ownership lost", status: http.StatusConflict, body: `{"error":{"code":"ownership_lost","message":"lease lost"}}`, wantCode: OwnershipLost, wantError: "ownership_lost"},
		{name: "standard error", status: http.StatusTooManyRequests, body: `{"error":{"code":"rate_limited","message":"slow down"}}`, wantCode: RateLimited, wantError: "rate_limited"},
		{name: "malformed success", status: http.StatusOK, body: `{`, wantError: "decode response"},
		{name: "trailing success", status: http.StatusOK, body: `{} {}`, wantError: "one JSON value"},
		{name: "oversized success", status: http.StatusOK, body: `{"task_id":"task-1","padding":"01234567890123456789"}`, limit: 8, wantError: "response body exceeds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client(), WithMaxResponseBytes(test.limit))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetTask(context.Background(), "task-1")
			if test.wantCode != "" {
				var apiError *APIError
				if !errors.As(err, &apiError) {
					t.Fatalf("error = %T %v, want APIError", err, err)
				}
				if apiError.Code != test.wantCode {
					t.Errorf("code = %q, want %q", apiError.Code, test.wantCode)
				}
				if test.wantCode == OwnershipLost && !IsOwnershipLost(err) {
					t.Error("IsOwnershipLost() = false")
				}
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestClientHonorsRequestCancellationAndDoesNotRetryMutation(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)

	client, err := NewClient(server.URL+"/api", machineToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, claimErr := client.Claim(requestContext, "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"})
		result <- claimErr
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach server")
	}
	cancel()

	select {
	case err = <-result:
	case <-time.After(time.Second):
		t.Fatal("Claim() did not return after context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestClientDoesNotRetryMutation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, `{"error":{"code":"service_unavailable","message":"try later"}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api", machineToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Claim(context.Background(), "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"})
	if err == nil {
		t.Fatal("Claim() succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestClientUsesProtocolAPIPrefixForOriginBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/api/v1/tasks/task-1"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_, _ = io.WriteString(writer, `{"task_id":"task-1","state":"queued"}`)
	}))
	defer server.Close()

	for _, baseURL := range []string{server.URL, server.URL + "/"} {
		client, err := NewOperatorClient(baseURL, "operator-token", server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.GetTask(context.Background(), "task-1"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewClientRejectsInvalidConfiguration(t *testing.T) {
	for _, baseURL := range []string{"", "ftp://control.example.test", "https://user:pass@control.example.test", "https://control.example.test?x=1", "https://control.example.test?", "https://control.example.test#fragment"} {
		if _, err := NewClient(baseURL, machineToken, nil); err == nil {
			t.Errorf("NewClient(%q) succeeded", baseURL)
		}
	}
	if _, err := NewClient("https://control.example.test/api", "", nil); err == nil {
		t.Error("NewClient accepted empty machine token")
	}
	if _, err := NewOperatorClient("https://control.example.test/api", "", nil); err == nil {
		t.Error("NewOperatorClient accepted empty operator token")
	}
}

func assertJSONEqual(t *testing.T, want, got string) {
	t.Helper()
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("invalid expected JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("invalid actual JSON: %v", err)
	}
	if !jsonEqual(wantValue, gotValue) {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

func jsonEqual(left, right any) bool { return reflect.DeepEqual(left, right) }

func fence() protocol.Fence {
	return protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1", LeaseToken: "lease-1"}
}

func work() protocol.Work {
	return protocol.Work{Goal: "work", AgentProfile: "codex", Workspace: "primary", Input: json.RawMessage(`{}`)}
}

func snapshotJSON(now time.Time) string {
	return fmt.Sprintf(`{"assignments":[{"run_id":"run-1","task_id":"task-1","generation":2,"assignment_expires_at":%q,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}],"commands":[{"command_id":"command-1","run_id":"run-1","generation":2,"kind":"cancel","payload":{},"issued_at":%q}],"server_time":%q}`, now.Format(time.RFC3339), now.Add(-9*time.Second).Format(time.RFC3339), now.Add(-8*time.Second).Format(time.RFC3339))
}
