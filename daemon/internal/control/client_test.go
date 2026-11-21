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

// testHangGuard bounds how long a test waits for a scripted in-process control
// server or client to publish an observable. It only turns a hung harness into
// a failure; it is never a statement about how fast the product should be, so a
// loaded host cannot turn a slow round trip into a verdict about the client.
const testHangGuard = 30 * time.Second

func TestOmOReviewRegressionClientRegistersNativeRuntime(t *testing.T) {
	repositoryResourceID := "77777777-7777-4777-8777-777777777777"
	for _, kind := range []string{"omo", "unknown_native"} {
		t.Run(kind, func(t *testing.T) {
			registration := protocol.RuntimeRegistration{
				RuntimeKey: "omo", Name: "OmO", Capacity: 1, AgentProfile: "omo", Workspace: "primary",
				RepositoryResourceID: &repositoryResourceID,
				HarnessKind:          kind, HarnessVersion: "5.1.69", AdapterVersion: "omo-rpc-v1", AdapterProtocolVersion: 1,
				Capabilities: protocol.RuntimeCapabilities{
					StructuredInput: true,
					Adapter: &protocol.Adapter{
						Kind: "omo", NativeVersion: "5.1.69", ImplementationVersion: "omo-rpc-v1", ProtocolVersion: 1,
						Operations: protocol.AdapterOperations{
							Start: true, Events: true, Cancel: true, Resume: true, Handoff: true,
							Guidance: protocol.GuidanceUnsupported, Pause: protocol.PauseUnsupported, Usage: protocol.UsageUnknown,
						},
					},
				},
			}
			if kind != "omo" {
				registration.Capabilities.Adapter = nil
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodPut || request.URL.RequestURI() != "/api/v1/machines/machine-1/sessions/daemon-1" {
					t.Errorf("request = %s %s", request.Method, request.URL.RequestURI())
				}
				if request.Header.Get("Authorization") != "Bearer "+machineToken {
					t.Error("machine authentication missing")
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				var received struct {
					Runtimes []struct {
						RuntimeKey             string `json:"runtime_key"`
						AgentProfile           string `json:"agent_profile"`
						Workspace              string `json:"workspace"`
						RepositoryResourceID   string `json:"repository_resource_id"`
						HarnessKind            string `json:"harness_kind"`
						HarnessVersion         string `json:"harness_version"`
						AdapterVersion         string `json:"adapter_version"`
						AdapterProtocolVersion int    `json:"adapter_protocol_version"`
						Capabilities           struct {
							StructuredInput bool              `json:"structured_input"`
							ProviderAccess  *bool             `json:"provider_access"`
							Adapter         *protocol.Adapter `json:"adapter"`
						} `json:"capabilities"`
					} `json:"runtimes"`
				}
				if err := json.Unmarshal(body, &received); err != nil || len(received.Runtimes) != 1 {
					t.Errorf("registration request cannot be consumed: runtimes=%d error=%v", len(received.Runtimes), err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				actual := received.Runtimes[0]
				if actual.RuntimeKey != "omo" || actual.AgentProfile != "omo" || actual.Workspace != "primary" || actual.RepositoryResourceID != repositoryResourceID || actual.HarnessKind != "omo" || actual.HarnessVersion != "5.1.69" || actual.AdapterVersion != "omo-rpc-v1" || actual.AdapterProtocolVersion != 1 {
					t.Errorf("registration native identity = %+v", actual)
				}
				if !actual.Capabilities.StructuredInput || actual.Capabilities.ProviderAccess == nil || *actual.Capabilities.ProviderAccess {
					t.Errorf("registration must support structured input without provider broker access: %+v", actual.Capabilities)
				}
				adapter := actual.Capabilities.Adapter
				wantOperations := protocol.AdapterOperations{
					Start: true, Events: true, Cancel: true, Resume: true, Handoff: true,
					Guidance: protocol.GuidanceUnsupported, Pause: protocol.PauseUnsupported, Usage: protocol.UsageUnknown,
				}
				if adapter == nil || adapter.Kind != "omo" || adapter.NativeVersion != "5.1.69" || adapter.ImplementationVersion != "omo-rpc-v1" || adapter.ProtocolVersion != 1 || adapter.Operations != wantOperations {
					t.Errorf("registration native operation projection = %+v", adapter)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"runtimes":[{"runtime_key":"omo","runtime_id":"runtime-omo","runtime_epoch":7}],"heartbeat_interval_ms":5000,"poll_interval_ms":4000,"lease_duration_ms":30000,"websocket_path":"/socket/websocket?vsn=2.0.0"}`)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, machineToken, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{registration}})
			if kind != "omo" {
				if err == nil || !strings.Contains(err.Error(), "harness_kind") || requests.Load() != 0 {
					t.Fatalf("unknown kind crossed HTTP boundary: requests=%d error=%v", requests.Load(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("OmO registration rejected: %v", err)
			}
			if requests.Load() != 1 || len(response.Runtimes) != 1 || response.Runtimes[0].RuntimeKey != "omo" || response.Runtimes[0].RuntimeID != "runtime-omo" || response.Runtimes[0].RuntimeEpoch != 7 || response.HeartbeatIntervalMS != 5000 || response.PollIntervalMS != 4000 || response.LeaseDurationMS != 30000 || response.WebSocketPath != "/socket/websocket?vsn=2.0.0" {
				t.Fatalf("OmO registration receipt = %+v, requests=%d", response, requests.Load())
			}
		})
	}
}

func TestClientProtocolRequests(t *testing.T) {
	now := time.Date(2026, time.September, 2, 0, 0, 30, 0, time.UTC)
	var enrollmentClient *EnrollmentClient
	tests := []struct {
		name       string
		invoke     func(context.Context, *Client) error
		method     string
		path       string
		wantAuth   string
		wantHeader string
		wantBody   string
		status     int
		response   string
	}{
		{
			name: "enroll", method: http.MethodPost, path: "/api/v1/machines", wantAuth: "Bearer enrollment-token", wantHeader: "enrollment-1",
			wantBody: `{"machine":{"name":"builder-01"},"machine_token":"issued-token"}`, response: `{"machine_id":"machine-1","machine_token":"issued-token"}`,
			invoke: func(ctx context.Context, client *Client) error {
				response, err := enrollmentClient.Enroll(ctx, "enrollment-token", "enrollment-1", protocol.EnrollRequest{Machine: protocol.MachineEnrollment{Name: "builder-01"}, MachineToken: "issued-token"})
				if err == nil && (response.MachineID != "machine-1" || response.MachineToken != "issued-token") {
					return fmt.Errorf("unexpected enrollment response: %#v", response)
				}
				return err
			},
		},
		{
			name: "register session", method: http.MethodPut, path: "/api/v1/machines/machine-1/sessions/daemon-1", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtimes":[{"runtime_key":"default","name":"Local Codex","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{"structured_input":true,"provider_access":true}}]}`,
			response: `{"runtimes":[{"runtime_key":"default","runtime_id":"runtime-1","runtime_epoch":3}],"heartbeat_interval_ms":5000,"poll_interval_ms":5000,"lease_duration_ms":30000,"websocket_path":"/socket/websocket?vsn=2.0.0"}`,
			invoke: func(ctx context.Context, client *Client) error {
				response, err := client.RegisterSession(ctx, "machine-1", "daemon-1", protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default", Name: "Local Codex", Capacity: 1, AgentProfile: "codex", Workspace: "primary", Capabilities: protocol.RuntimeCapabilities{StructuredInput: true, ProviderAccess: true}}}})
				if err == nil && (len(response.Runtimes) != 1 || response.Runtimes[0].RuntimeEpoch != 3) {
					return fmt.Errorf("unexpected session response: %#v", response)
				}
				return err
			},
		},
		{
			name: "runtime heartbeat", method: http.MethodPatch, path: "/api/v1/runtimes/runtime-1", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_epoch":3,"active_runs":[]}`, response: snapshotJSON(now),
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Heartbeat(ctx, "runtime-1", protocol.RuntimeHeartbeatRequest{RuntimeEpoch: 3, ActiveRuns: []protocol.ActiveRun{}})
				return err
			},
		},
		{
			name: "dispatch", method: http.MethodGet, path: "/api/v1/runtimes/runtime-1/dispatch?runtime_epoch=3", wantAuth: "Bearer " + machineToken,
			response: snapshotJSON(now),
			invoke: func(ctx context.Context, client *Client) error {
				response, err := client.Dispatch(ctx, "runtime-1", 3)
				if err == nil && len(response.Assignments) != 1 {
					return fmt.Errorf("assignments = %d", len(response.Assignments))
				}
				return err
			},
		},
		{
			name: "claim", method: http.MethodPut, path: "/api/v1/runs/run-1/claims/claim-1", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2}`,
			response: `{"run_id":"run-1","task_id":"task-1","generation":2,"claim_id":"claim-1","lease_token":"lease-1","lease_expires_at":"2026-09-02T00:00:30Z","lease_remaining_ms":30000,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Claim(ctx, "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"})
				return err
			},
		},
		{
			name: "renew lease", method: http.MethodPatch, path: "/api/v1/runs/run-1/lease", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1"}`,
			response: `{"lease_expires_at":"2026-09-02T00:00:45Z","lease_remaining_ms":30000,"commands":[]}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.RenewLease(ctx, "run-1", protocol.LeaseHeartbeatRequest{Fence: protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1", LeaseToken: "lease-1"}})
				return err
			},
		},
		{
			name: "append events", method: http.MethodPost, path: "/api/v1/runs/run-1/events", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","events":[{"event_id":"event-1","sequence":4,"kind":"progress","occurred_at":"2026-09-02T00:00:10Z","payload":{"message":"running"}}]}`,
			status:   http.StatusNoContent,
			invoke: func(ctx context.Context, client *Client) error {
				return client.AppendEvents(ctx, "run-1", protocol.AppendEventsRequest{Fence: fence(), Events: []protocol.RunEvent{{EventID: "event-1", Sequence: 4, Kind: "progress", OccurredAt: time.Date(2026, time.September, 2, 0, 0, 10, 0, time.UTC), Payload: json.RawMessage(`{"message":"running"}`)}}})
			},
		},
		{
			name: "transition", method: http.MethodPut, path: "/api/v1/runs/run-1/transitions/transition-1", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","state":"waiting_for_input","payload":{"question":"branch"}}`,
			response: runJSON("waiting_for_input", `null`, `null`),
			invoke: func(ctx context.Context, client *Client) error {
				return client.Transition(ctx, "run-1", protocol.StateTransitionRequest{Fence: fence(), TransitionID: "transition-1", State: "waiting_for_input", Payload: json.RawMessage(`{"question":"branch"}`)})
			},
		},
		{
			name: "reconcile", method: http.MethodPut, path: "/api/v1/runtimes/runtime-1/reconciliation", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_epoch":3,"runs":[]}`, response: `{"decisions":[],"assignments":[],"commands":[]}`,
			invoke: func(ctx context.Context, client *Client) error {
				_, err := client.Reconcile(ctx, "runtime-1", protocol.ReconcileRequest{RuntimeEpoch: 3, Runs: []protocol.ReconcileRun{}})
				return err
			},
		},
		{
			name: "acknowledge command", method: http.MethodPut, path: "/api/v1/commands/command-1/acknowledgements/ack-1", wantAuth: "Bearer " + machineToken,
			wantBody: `{"runtime_id":"runtime-1","runtime_epoch":3,"generation":2,"claim_id":"claim-1","lease_token":"lease-1","run_id":"run-1","outcome":"applied"}`,
			response: acknowledgedCommandForGeneration(2),
			invoke: func(ctx context.Context, client *Client) error {
				return client.AcknowledgeCommand(ctx, "command-1", protocol.CommandAcknowledgement{Fence: fence(), RunID: "run-1", CommandID: "command-1", Outcome: "applied", AckID: "ack-1"})
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
				if test.status != 0 {
					writer.WriteHeader(test.status)
				}
				if test.response != "" {
					writer.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(writer, test.response)
				}
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
			if err := test.invoke(context.Background(), client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegisterSessionUnpromotedOMPBoundary(t *testing.T) {
	for _, kind := range []string{"omp", "future_harness"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != http.MethodPut || request.URL.RequestURI() != "/api/v1/machines/machine-1/sessions/daemon-1" {
					t.Errorf("registration route = %s %s", request.Method, request.URL.RequestURI())
				}
				if got := request.Header.Get("Authorization"); got != "Bearer "+machineToken {
					t.Errorf("Authorization = %q, want machine credential", got)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read registration body: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				want := `{"runtimes":[{"runtime_key":"omp","name":"OMP","capacity":1,"agent_profile":"omp","workspace":"primary","harness_kind":"omp","harness_version":"18.20.0","adapter_version":"omp-rpc-v2","adapter_protocol_version":2,"capabilities":{"structured_input":false,"provider_access":false,"adapter":{"kind":"omp","native_version":"18.20.0","implementation_version":"omp-rpc-v2","protocol_version":2,"operations":{"start":false,"events":false,"cancel":false,"resume":false,"handoff":false,"guidance":"unsupported","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}}]}`
				assertJSONEqual(t, want, string(body))
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"runtimes":[{"runtime_key":"omp","runtime_id":"runtime-1","runtime_epoch":3}],"heartbeat_interval_ms":5000,"poll_interval_ms":5000,"lease_duration_ms":30000,"websocket_path":"/socket"}`)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+"/api", machineToken, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			registration := protocol.RuntimeRegistration{
				RuntimeKey:             "omp",
				Name:                   "OMP",
				Capacity:               1,
				AgentProfile:           "omp",
				Workspace:              "primary",
				HarnessKind:            kind,
				HarnessVersion:         "18.20.0",
				AdapterVersion:         "omp-rpc-v2",
				AdapterProtocolVersion: 2,
				Capabilities: protocol.RuntimeCapabilities{Adapter: &protocol.Adapter{
					Kind:                  kind,
					NativeVersion:         "18.20.0",
					ImplementationVersion: "omp-rpc-v2",
					ProtocolVersion:       2,
					Operations: protocol.AdapterOperations{
						Start: false, Events: false, Cancel: false, Resume: false, Handoff: false,
						Guidance: protocol.GuidanceUnsupported, Pause: protocol.PauseUnsupported,
						ApprovalResponse: false, Usage: protocol.UsageUnknown, HardCostLimit: false,
					},
				}},
			}
			response, err := client.RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{registration}})
			if kind != "omp" {
				if err == nil || !strings.Contains(err.Error(), `harness_kind "future_harness" is invalid`) {
					t.Fatalf("RegisterSession error = %v, want unknown harness rejection", err)
				}
				if requests.Load() != 0 {
					t.Fatalf("unknown harness made %d HTTP requests", requests.Load())
				}
				return
			}
			if err != nil {
				t.Fatalf("register unpromoted OMP runtime: %v", err)
			}
			if requests.Load() != 1 {
				t.Fatalf("OMP registration made %d HTTP requests, want one", requests.Load())
			}
			if len(response.Runtimes) != 1 || response.Runtimes[0] != (protocol.RegisteredRuntime{RuntimeKey: "omp", RuntimeID: "runtime-1", RuntimeEpoch: 3}) {
				t.Fatalf("registered OMP runtime = %#v", response.Runtimes)
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
	case <-time.After(testHangGuard):
		t.Fatal("request did not reach server")
	}
	cancel()

	select {
	case err = <-result:
	case <-time.After(testHangGuard):
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

func TestNewClientRejectsInvalidConfiguration(t *testing.T) {
	for _, baseURL := range []string{"", "ftp://control.example.test", "https://user:pass@control.example.test", "https://control.example.test?x=1", "https://control.example.test?", "https://control.example.test#fragment"} {
		if _, err := NewClient(baseURL, machineToken, nil); err == nil {
			t.Errorf("NewClient(%q) succeeded", baseURL)
		}
	}
	if _, err := NewClient("https://control.example.test/api", "", nil); err == nil {
		t.Error("NewClient accepted empty machine token")
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

func snapshotJSON(now time.Time) string {
	return fmt.Sprintf(`{"assignments":[{"run_id":"run-1","task_id":"task-1","generation":2,"assignment_expires_at":%q,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}],"commands":[{"command_id":"command-1","run_id":"run-1","generation":2,"kind":"cancel","payload":{},"issued_at":%q}],"server_time":%q}`, now.Format(time.RFC3339), now.Add(-9*time.Second).Format(time.RFC3339), now.Add(-8*time.Second).Format(time.RFC3339))
}

func TestWorkValidationRetainsBooleanCapabilityRequirements(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		valid bool
	}{
		{"legacy omitted", "", true},
		{"empty map", `,"required_capabilities":{}`, true},
		{"forward compatible boolean map", `,"required_capabilities":{"supervisory_control":true,"future_capability":false}`, true},
		{"null map", `,"required_capabilities":null`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var work protocol.Work
			if err := json.Unmarshal([]byte(`{"goal":"goal","agent_profile":"agent","workspace":"local","input":{}`+test.field+`}`), &work); err != nil {
				t.Fatal(err)
			}
			if err := validateWork(work); (err == nil) != test.valid {
				t.Fatalf("validation = %v, want valid %v", err, test.valid)
			}
		})
	}
}

func commandJSON() string {
	return `{"command_id":"command-1","task_id":"task-1","run_id":"run-1","generation":1,"kind":"cancel","payload":{},"state":"pending","issued_at":"2026-09-03T00:00:00Z","applied_at":null,"acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null}`
}

func acknowledgedCommandForGeneration(generation int64) string {
	return fmt.Sprintf(`{"command_id":"command-1","task_id":"task-1","run_id":"run-1","generation":%d,"kind":"cancel","payload":{},"state":"acknowledged","issued_at":"2026-09-03T00:00:00Z","applied_at":null,"acknowledgement_id":"ack-1","acknowledgement_outcome":"applied","acknowledged_at":"2026-09-03T00:00:01Z"}`, generation)
}

func runJSON(state, result, failure string) string {
	return fmt.Sprintf(`{"run_id":"run-1","task_id":"task-1","runtime_id":"runtime-1","generation":2,"state":%q,"claim_id":"claim-1","lease_token":"lease-1","lease_expires_at":"2026-09-03T00:00:30Z","result":%s,"failure":%s}`, state, result, failure)
}

// TestValidateProviderActionUUIDPinsRejectionMessages pins the exact rejection
// message for the provider-action action_id classes this boundary already
// rejects. Version and variant rows are deliberately absent: the base accepts
// them here, and the tightening is covered by its own test.
func TestValidateProviderActionUUIDPinsRejectionMessages(t *testing.T) {
	const field = "provider action action_id"
	const uuid = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	canonical := fmt.Sprintf("%s must be a canonical UUID", field)
	lowerHex := fmt.Sprintf("%s must use lowercase hexadecimal UUID digits", field)

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: uuid},
		{name: "too short", value: uuid[:35], want: canonical},
		{name: "36 runes 37 bytes", value: uuid[:35] + "\u00e9", want: canonical},
		{name: "misplaced dash", value: uuid[:8] + "0" + uuid[9:], want: canonical},
		{name: "uppercase hex digit", value: uuid[:15] + "A" + uuid[16:], want: lowerHex},
		{name: "non-hex lowercase", value: uuid[:16] + "g" + uuid[17:], want: lowerHex},
		{name: "non-hex uppercase outside A-F", value: uuid[:16] + "Z" + uuid[17:], want: lowerHex},
		{name: "length beats uppercase", value: uuid[:15] + "A" + uuid[16:35], want: canonical},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateProviderActionUUID(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateProviderActionUUID(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateProviderActionUUID(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateProviderActionUUID(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}

// The tolerant legacy path (decodeJSON) already rejected duplicate members
// before this change; these rows pin that rejection so a later tolerance
// relaxation cannot silently reopen the gap.
func TestDecodeJSONRejectsDuplicateMembers(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "duplicate top-level member",
			body: `{"a":1,"a":2}`,
			want: `duplicate JSON object member "a" at $`,
		},
		{name: "accepted single value", body: `{"a":1}`},
		{name: "malformed body keeps its message", body: `{"a":`, want: "unexpected EOF"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var decoded map[string]any
			err := decodeJSON([]byte(test.body), &decoded)
			if test.want == "" {
				if err != nil {
					t.Fatalf("decodeJSON(%q) error = %v, want accept", test.body, err)
				}
				if decoded["a"] != float64(1) {
					t.Fatalf("decodeJSON(%q) decoded = %#v, want a=1", test.body, decoded)
				}
				return
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("decodeJSON(%q) error = %v, want %q", test.body, err, test.want)
			}
		})
	}
}
