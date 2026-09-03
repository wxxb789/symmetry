package control

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
				_, err := enrollment.Enroll(context.Background(), "enrollment-token", "enrollment-1", protocol.EnrollRequest{MachineToken: "machine-token"})
				return err
			},
		},
		{
			name: "session", body: `{"runtimes":[],"heartbeat_interval_ms":1,"poll_interval_ms":1,"lease_duration_ms":1,"websocket_path":"/socket"}`, wantError: "invalid session registration response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default"}}})
				return err
			},
		},
		{
			name: "snapshot", body: `{"assignments":[],"commands":[]}`, wantError: "invalid runtime snapshot response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				_, err := machine.Dispatch(context.Background(), "runtime-1", 3)
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
			name: "transition", body: `{}`, wantError: "invalid transition response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				return machine.Transition(context.Background(), "run-1", protocol.StateTransitionRequest{Fence: fence(), TransitionID: "transition-1", State: "running", Payload: json.RawMessage(`{}`)})
			},
		},
		{
			name: "acknowledgement", body: `{}`, wantError: "invalid acknowledge command response",
			invoke: func(machine *Client, enrollment *EnrollmentClient, operator *OperatorClient) error {
				return machine.AcknowledgeCommand(context.Background(), "command-1", protocol.CommandAcknowledgement{Fence: fence(), RunID: "run-1", CommandID: "command-1", Outcome: "applied", AckID: "ack-1"})
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

	response, err := mustMachineClient(t, server).RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{
		Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "primary"}, {RuntimeKey: "secondary"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Runtimes) != 2 {
		t.Fatalf("runtime count = %d, want 2", len(response.Runtimes))
	}
}

func TestRegisterSessionRejectsLeaseBelowSafetyMinimum(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{
		"runtimes":[{"runtime_key":"default","runtime_id":"runtime-1","runtime_epoch":1}],
		"heartbeat_interval_ms":5000,
		"poll_interval_ms":5000,
		"lease_duration_ms":29999,
		"websocket_path":"/socket"
	}`, nil)
	defer server.Close()
	client := mustMachineClient(t, server)

	_, err := client.RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{
		Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default"}},
	})
	if err == nil || !strings.Contains(err.Error(), "lease_duration_ms must be at least 30000") {
		t.Fatalf("error = %v, want minimum lease rejection", err)
	}
}

func TestRegisterSessionAcceptsLeaseAtSafetyMinimum(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{
		"runtimes":[{"runtime_key":"default","runtime_id":"runtime-1","runtime_epoch":1}],
		"heartbeat_interval_ms":5000,
		"poll_interval_ms":5000,
		"lease_duration_ms":30000,
		"websocket_path":"/socket"
	}`, nil)
	defer server.Close()
	client := mustMachineClient(t, server)

	_, err := client.RegisterSession(context.Background(), "machine-1", "daemon-1", protocol.SessionRegistrationRequest{
		Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "default"}},
	})
	if err != nil {
		t.Fatalf("RegisterSession error = %v, want exact minimum accepted", err)
	}
}

func TestEnrollRejectsUnexpectedSuccessStatus(t *testing.T) {
	server := jsonServer(t, http.StatusAccepted, `{"machine_id":"machine-1","machine_token":"machine-token"}`, nil)
	defer server.Close()
	client, err := NewEnrollmentClient(server.URL+"/api", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Enroll(context.Background(), "enrollment-token", "enrollment-1", protocol.EnrollRequest{
		Machine:      protocol.MachineEnrollment{Name: "builder"},
		MachineToken: "machine-token",
	})
	if err == nil || !strings.Contains(err.Error(), "expected HTTP 200 or 201") {
		t.Fatalf("Enroll error = %v, want unexpected success status rejection", err)
	}
}

func TestEnrollRejectsMismatchedResponseToken(t *testing.T) {
	server := jsonServer(t, http.StatusOK, `{"machine_id":"machine-1","machine_token":"different-token"}`, nil)
	defer server.Close()
	client, err := NewEnrollmentClient(server.URL+"/api", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Enroll(context.Background(), "enrollment-token", "enrollment-1", protocol.EnrollRequest{
		Machine:      protocol.MachineEnrollment{Name: "builder"},
		MachineToken: "machine-token",
	})
	if err == nil || !strings.Contains(err.Error(), "machine_token does not match the request") {
		t.Fatalf("Enroll error = %v, want token mismatch rejection", err)
	}
}

func TestEnrollRejectsUnsafeMachineID(t *testing.T) {
	server := jsonServer(t, http.StatusCreated, `{"machine_id":"..","machine_token":"machine-token"}`, nil)
	defer server.Close()
	client, err := NewEnrollmentClient(server.URL+"/api", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Enroll(context.Background(), "enrollment-token", "enrollment-1", protocol.EnrollRequest{
		Machine:      protocol.MachineEnrollment{Name: "builder"},
		MachineToken: "machine-token",
	})
	if err == nil || !strings.Contains(err.Error(), "machine ID must be a non-empty safe path segment") {
		t.Fatalf("Enroll error = %v, want unsafe machine ID rejection", err)
	}
}

func TestClientRejectsUnsafePathIDsBeforeRequest(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*Client) error
	}{
		{name: "empty runtime", invoke: func(client *Client) error { _, err := client.Dispatch(context.Background(), "", 3); return err }},
		{name: "runtime dot segment", invoke: func(client *Client) error { _, err := client.Dispatch(context.Background(), "..", 3); return err }},
		{name: "runtime slash", invoke: func(client *Client) error {
			_, err := client.Dispatch(context.Background(), "runtime/1", 3)
			return err
		}},
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
	server := jsonServer(t, http.StatusOK, taskJSON(), func(request *http.Request) {
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

func TestTaskResponseRequiresPresenceAndStateInvariants(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "missing result", body: strings.Replace(taskJSON(), `,"result":null`, "", 1), wantError: "result is required"},
		{name: "missing waiting", body: strings.Replace(taskJSON(), `,"waiting":null`, "", 1), wantError: "waiting is required"},
		{name: "missing latest command", body: strings.Replace(taskJSON(), `,"latest_command":null`, "", 1), wantError: "latest_command is required"},
		{name: "null work", body: strings.Replace(taskJSON(), `"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}`, `"work":null`, 1), wantError: "work must be non-null"},
		{name: "missing work input", body: strings.Replace(taskJSON(), `,"input":{}`, "", 1), wantError: "work input is required"},
		{name: "non-object work input", body: strings.Replace(taskJSON(), `"input":{}`, `"input":[]`, 1), wantError: "work input must be a JSON object"},
		{name: "runless task has generation", body: strings.Replace(taskJSON(), `"generation":0`, `"generation":1`, 1), wantError: "runless task must have generation zero"},
		{name: "active task without run", body: strings.Replace(taskJSON(), `"state":"queued"`, `"state":"running"`, 1), wantError: "state requires a run_id"},
		{name: "completed task without result", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":0`, `"state":"completed","run_id":"run-1","generation":1`, 1), wantError: "completed state requires result"},
		{name: "failed task without failure", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":0`, `"state":"failed","run_id":"run-1","generation":1`, 1), wantError: "failed state requires failure"},
		{name: "waiting state without waiting projection", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":0`, `"state":"waiting_for_input","run_id":"run-1","generation":1`, 1), wantError: "waiting_for_input state requires waiting"},
		{name: "waiting projection outside waiting state", body: strings.Replace(taskJSON(), `"waiting":null`, `"waiting":{"run_id":"run-1","generation":1,"transition_id":"transition-1","question":"Choose","payload":{},"recorded_at":"2026-09-03T00:00:00Z"}`, 1), wantError: "only waiting_for_input state may include waiting"},
		{name: "waiting run differs from current run", body: strings.Replace(waitingTaskJSON(), `"run_id":"run-current","generation":2,"transition_id"`, `"run_id":"run-other","generation":2,"transition_id"`, 1), wantError: "waiting run_id and generation must match the current run"},
		{name: "waiting misses required field", body: strings.Replace(waitingTaskJSON(), `,"question":"Choose the target branch"`, "", 1), wantError: "waiting question is required"},
		{name: "waiting payload is not object", body: strings.Replace(waitingTaskJSON(), `"payload":{"question":"Choose the target branch"}`, `"payload":[]`, 1), wantError: "waiting payload must be a JSON object"},
		{name: "latest command task differs", body: strings.Replace(waitingTaskJSON(), `"task_id":"task-1","run_id":"run-earlier"`, `"task_id":"task-other","run_id":"run-earlier"`, 1), wantError: "latest_command task_id does not match expected task"},
		{name: "latest command violates resource invariant", body: strings.Replace(waitingTaskJSON(), `"state":"applied","issued_at"`, `"state":"pending","issued_at"`, 1), wantError: "latest_command pending command must be run-bound without applied_at or acknowledgement"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			_, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestTaskResponseAcceptsNullWaitingQuestion(t *testing.T) {
	body := strings.Replace(waitingTaskJSON(), `"question":"Choose the target branch"`, `"question":null`, 1)
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	task, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Waiting == nil || task.Waiting.Question != nil {
		t.Fatalf("waiting question = %#v, want explicit null", task.Waiting)
	}
}

func TestTaskResponsePreservesExplicitNullWorkInputAndUnknownFields(t *testing.T) {
	body := strings.Replace(taskJSONWithUnknownField(), `"input":{}`, `"input":null`, 1)
	server := jsonServer(t, http.StatusOK, body, nil)
	defer server.Close()

	task, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Work == nil || string(task.Work.Input) != "null" {
		t.Fatalf("work input = %#v, want explicit null", task.Work)
	}
}

func TestCreateTaskCommandValidatesRequestBeforeTransport(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		request   protocol.TaskCommandRequest
		wantError string
	}{
		{name: "missing idempotency key", request: protocol.TaskCommandRequest{Kind: "cancel"}, wantError: "idempotency key must not be empty"},
		{name: "cancel with payload", key: "command-1", request: protocol.TaskCommandRequest{Kind: "cancel", Payload: json.RawMessage(`{}`)}, wantError: "cancel command must omit payload"},
		{name: "unknown kind", key: "command-1", request: protocol.TaskCommandRequest{Kind: "pause"}, wantError: "kind is not recognized"},
		{name: "provide input missing payload", key: "command-1", request: protocol.TaskCommandRequest{Kind: "provide_input"}, wantError: "must be a non-null JSON object"},
		{name: "provide input null payload", key: "command-1", request: protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`null`)}, wantError: "must be a non-null JSON object"},
		{name: "provide input array payload", key: "command-1", request: protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`[]`)}, wantError: "must be a JSON object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", test.key, test.request)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("calls = %d, want 0", got)
			}
		})
	}
}

func TestCreateTaskCommandAcceptsEmptyObjectAndReplay(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := jsonServer(t, status, strings.Replace(commandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1), func(request *http.Request) {
				if got := request.Header.Get("Idempotency-Key"); got != "input-1" {
					t.Errorf("Idempotency-Key = %q", got)
				}
			})
			defer server.Close()
			command, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "input-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			if command.CommandID != "command-1" {
				t.Fatalf("command = %#v", command)
			}
		})
	}
}

func TestCreateTaskCommandRejectsUnexpectedSuccessStatus(t *testing.T) {
	server := jsonServer(t, http.StatusAccepted, commandJSON(), nil)
	defer server.Close()

	_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "cancel"})
	if err == nil || !strings.Contains(err.Error(), "expected HTTP 200 or 201") {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateTaskCommandAcceptsEquivalentProvideInputObject(t *testing.T) {
	server := jsonServer(t, http.StatusCreated, provideInputCommandJSON(`{"second":2,"first":1}`), nil)
	defer server.Close()

	_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "input-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"first":1,"second":2}`)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateTaskCommandDistinguishesLargeIntegerPayloads(t *testing.T) {
	server := jsonServer(t, http.StatusCreated, provideInputCommandJSON(`{"value":9007199254740993}`), nil)
	defer server.Close()

	_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "input-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"value":9007199254740992}`)})
	if err == nil || !strings.Contains(err.Error(), "does not match the request") {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskCommandResponseRequiresExplicitNullableFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing applied at", body: strings.Replace(commandJSON(), `,"applied_at":null`, "", 1)},
		{name: "unpaired run", body: strings.Replace(commandJSON(), `"generation":1`, `"generation":null`, 1)},
		{name: "partial acknowledgement", body: strings.Replace(commandJSON(), `"acknowledgement_id":null`, `"acknowledgement_id":"ack-1"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusCreated, test.body, nil)
			defer server.Close()
			_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "cancel"})
			if err == nil || !strings.Contains(err.Error(), "invalid create task command response") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTaskCommandResponseStateInvariants(t *testing.T) {
	valid := []string{
		commandJSON(),
		runlessAppliedCommandJSON(),
		acknowledgedCommandJSON(),
	}
	for _, body := range valid {
		server := jsonServer(t, http.StatusCreated, body, nil)
		_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "cancel"})
		server.Close()
		if err != nil {
			t.Fatalf("valid command response rejected: %v", err)
		}
	}

	acknowledged := acknowledgedCommandJSON()
	tests := []struct {
		name string
		body string
	}{
		{name: "runless pending", body: strings.Replace(commandJSON(), `"run_id":"run-1","generation":1`, `"run_id":null,"generation":null`, 1)},
		{name: "runless provide input", body: strings.Replace(runlessAppliedCommandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1)},
		{name: "pending applied at", body: strings.Replace(commandJSON(), `"applied_at":null`, `"applied_at":"2026-09-03T00:00:01Z"`, 1)},
		{name: "pending acknowledgement", body: strings.Replace(commandJSON(), `"acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null`, `"acknowledgement_id":"ack-1","acknowledgement_outcome":"applied","acknowledged_at":"2026-09-03T00:00:01Z"`, 1)},
		{name: "applied provide input", body: strings.Replace(strings.Replace(commandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1), `"state":"pending","issued_at":"2026-09-03T00:00:00Z","applied_at":null`, `"state":"applied","issued_at":"2026-09-03T00:00:00Z","applied_at":"2026-09-03T00:00:01Z"`, 1)},
		{name: "applied without timestamp", body: strings.Replace(commandJSON(), `"state":"pending"`, `"state":"applied"`, 1)},
		{name: "acknowledged runless", body: strings.Replace(acknowledged, `"run_id":"run-1","generation":1`, `"run_id":null,"generation":null`, 1)},
		{name: "acknowledged applied at", body: strings.Replace(acknowledged, `"applied_at":null`, `"applied_at":"2026-09-03T00:00:01Z"`, 1)},
		{name: "acknowledged without acknowledgement", body: strings.Replace(acknowledged, `"acknowledgement_id":"ack-1","acknowledgement_outcome":"applied","acknowledged_at":"2026-09-03T00:00:01Z"`, `"acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusCreated, test.body, nil)
			defer server.Close()
			_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "cancel"})
			if err == nil || !strings.Contains(err.Error(), "invalid create task command response") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCreateTaskCommandRejectsRequestResponseMismatch(t *testing.T) {
	tests := []struct {
		name    string
		request protocol.TaskCommandRequest
		body    string
	}{
		{name: "kind", request: protocol.TaskCommandRequest{Kind: "cancel"}, body: strings.Replace(commandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1)},
		{name: "provide input payload", request: protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"answer":"yes"}`)}, body: strings.Replace(commandJSON(), `"kind":"cancel","payload":{}`, `"kind":"provide_input","payload":{"answer":"no"}`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusCreated, test.body, nil)
			defer server.Close()
			_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", test.request)
			if err == nil || !strings.Contains(err.Error(), "does not match the request") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTransitionRequiresMatchingRunResource(t *testing.T) {
	request := protocol.StateTransitionRequest{
		Fence:        fence(),
		TransitionID: "transition-1",
		State:        "running",
		Payload:      json.RawMessage(`{}`),
	}
	valid := runJSON("running", `null`, `null`)
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "accepted", status: http.StatusAccepted, body: valid, wantErr: "expected HTTP 200"},
		{name: "no content", status: http.StatusNoContent, wantErr: "decode response"},
		{name: "empty object", status: http.StatusOK, body: `{}`, wantErr: "invalid transition response"},
		{name: "missing result", status: http.StatusOK, body: strings.Replace(valid, `,"result":null`, ``, 1), wantErr: "result is required"},
		{name: "mismatched run", status: http.StatusOK, body: strings.Replace(valid, `"run_id":"run-1"`, `"run_id":"run-2"`, 1), wantErr: "run_id or fence does not match the request"},
		{name: "mismatched fence", status: http.StatusOK, body: strings.Replace(valid, `"runtime_id":"runtime-1"`, `"runtime_id":"runtime-2"`, 1), wantErr: "run_id or fence does not match the request"},
		{name: "mismatched state", status: http.StatusOK, body: strings.Replace(valid, `"state":"running"`, `"state":"waiting_for_input"`, 1), wantErr: "state does not match the request"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, test.status, test.body, nil)
			defer server.Close()
			err := mustMachineClient(t, server).Transition(context.Background(), "run-1", request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
			var responseError *ResponseError
			if !errors.As(err, &responseError) {
				t.Fatalf("error = %T %v, want ResponseError", err, err)
			}
		})
	}

	completed := request
	completed.State = "completed"
	server := jsonServer(t, http.StatusOK, runJSON("completed", `null`, `null`), nil)
	defer server.Close()
	err := mustMachineClient(t, server).Transition(context.Background(), "run-1", completed)
	if err == nil || !strings.Contains(err.Error(), "completed state requires result") {
		t.Fatalf("completed response error = %v", err)
	}

	failed := request
	failed.State = "failed"
	server = jsonServer(t, http.StatusOK, runJSON("failed", `null`, `null`), nil)
	defer server.Close()
	err = mustMachineClient(t, server).Transition(context.Background(), "run-1", failed)
	if err == nil || !strings.Contains(err.Error(), "failed state requires failure") {
		t.Fatalf("failed response error = %v", err)
	}

	server = jsonServer(t, http.StatusOK, strings.TrimSuffix(valid, `}`)+`,"future_field":true}`, nil)
	defer server.Close()
	if err := mustMachineClient(t, server).Transition(context.Background(), "run-1", request); err != nil {
		t.Fatalf("unknown additive field: %v", err)
	}
}

func TestAcknowledgeCommandRequiresMatchingCommandResource(t *testing.T) {
	request := protocol.CommandAcknowledgement{
		Fence:     fence(),
		RunID:     "run-1",
		CommandID: "command-1",
		Outcome:   "applied",
		AckID:     "ack-1",
	}
	valid := acknowledgedCommandForGeneration(2)
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "accepted", status: http.StatusAccepted, body: valid, wantErr: "expected HTTP 200"},
		{name: "no content", status: http.StatusNoContent, wantErr: "decode response"},
		{name: "empty object", status: http.StatusOK, body: `{}`, wantErr: "invalid acknowledge command response"},
		{name: "missing task", status: http.StatusOK, body: strings.Replace(valid, `,"task_id":"task-1"`, ``, 1), wantErr: "task_id is required"},
		{name: "mismatched command", status: http.StatusOK, body: strings.Replace(valid, `"command_id":"command-1"`, `"command_id":"command-2"`, 1), wantErr: "command_id, run_id, or generation does not match the request"},
		{name: "mismatched run", status: http.StatusOK, body: strings.Replace(valid, `"run_id":"run-1"`, `"run_id":"run-2"`, 1), wantErr: "command_id, run_id, or generation does not match the request"},
		{name: "mismatched generation", status: http.StatusOK, body: strings.Replace(valid, `"generation":2`, `"generation":3`, 1), wantErr: "command_id, run_id, or generation does not match the request"},
		{name: "mismatched state", status: http.StatusOK, body: strings.Replace(valid, `"state":"acknowledged"`, `"state":"pending"`, 1), wantErr: "pending command must be run-bound without applied_at or acknowledgement"},
		{name: "mismatched acknowledgement", status: http.StatusOK, body: strings.Replace(valid, `"acknowledgement_id":"ack-1"`, `"acknowledgement_id":"ack-2"`, 1), wantErr: "acknowledgement_id or outcome does not match the request"},
		{name: "mismatched outcome", status: http.StatusOK, body: strings.Replace(valid, `"acknowledgement_outcome":"applied"`, `"acknowledgement_outcome":"rejected"`, 1), wantErr: "acknowledgement_id or outcome does not match the request"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, test.status, test.body, nil)
			defer server.Close()
			err := mustMachineClient(t, server).AcknowledgeCommand(context.Background(), "command-1", request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
			var responseError *ResponseError
			if !errors.As(err, &responseError) {
				t.Fatalf("error = %T %v, want ResponseError", err, err)
			}
		})
	}

	server := jsonServer(t, http.StatusOK, strings.TrimSuffix(valid, `}`)+`,"future_field":true}`, nil)
	defer server.Close()
	if err := mustMachineClient(t, server).AcknowledgeCommand(context.Background(), "command-1", request); err != nil {
		t.Fatalf("unknown additive field: %v", err)
	}
}

func TestAppendEventsRequiresNoContentWithoutBody(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{name: "no content", status: http.StatusNoContent},
		{name: "ok empty", status: http.StatusOK, wantErr: true},
		{name: "created body", status: http.StatusCreated, body: `{}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, test.status, test.body, nil)
			defer server.Close()
			err := mustMachineClient(t, server).AppendEvents(context.Background(), "run-1", protocol.AppendEventsRequest{Fence: fence()})
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "invalid append events response")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}

	client, err := NewClient("https://control.example.test/api", machineToken, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	err = client.AppendEvents(context.Background(), "run-1", protocol.AppendEventsRequest{Fence: fence()})
	if err == nil || !strings.Contains(err.Error(), "invalid append events response") {
		t.Fatalf("204 body error = %v", err)
	}
}

func TestRetryDelayClassification(t *testing.T) {
	fallback := 3 * time.Second
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		context   context.Context
		err       error
		wantDelay time.Duration
		wantRetry bool
	}{
		{name: "ordinary error is permanent", context: context.Background(), err: errors.New("network unavailable"), wantRetry: false},
		{name: "URL policy error is permanent", context: context.Background(), err: &url.Error{Op: "Get", URL: "https://control.example.test", Err: errors.New("redirect policy rejected")}, wantRetry: false},
		{name: "URL TLS error is permanent", context: context.Background(), err: &url.Error{Op: "Get", URL: "https://control.example.test", Err: x509.UnknownAuthorityError{}}, wantRetry: false},
		{name: "URL network error", context: context.Background(), err: &url.Error{Op: "Get", URL: "https://control.example.test", Err: &net.DNSError{IsTimeout: true}}, wantDelay: fallback, wantRetry: true},
		{name: "network error", context: context.Background(), err: &net.DNSError{IsTimeout: true}, wantDelay: fallback, wantRetry: true},
		{name: "truncated transport response", context: context.Background(), err: io.ErrUnexpectedEOF, wantDelay: fallback, wantRetry: true},
		{name: "malformed response is permanent", context: context.Background(), err: responseErrorf("decode response: %w", io.ErrUnexpectedEOF), wantRetry: false},
		{name: "rate limited retry after", context: context.Background(), err: &APIError{StatusCode: http.StatusTooManyRequests, RetryAfter: 7 * time.Second}, wantDelay: 7 * time.Second, wantRetry: true},
		{name: "server error", context: context.Background(), err: &APIError{StatusCode: http.StatusBadGateway}, wantDelay: fallback, wantRetry: true},
		{name: "permanent client error", context: context.Background(), err: &APIError{StatusCode: http.StatusConflict}, wantRetry: false},
		{name: "context cancelled", context: cancelled, err: errors.New("network unavailable"), wantRetry: false},
		{name: "request cancelled", context: context.Background(), err: context.Canceled, wantRetry: false},
		{name: "request timeout", context: context.Background(), err: context.DeadlineExceeded, wantDelay: fallback, wantRetry: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delay, retry := RetryDelay(test.context, test.err, fallback)
			if delay != test.wantDelay || retry != test.wantRetry {
				t.Fatalf("RetryDelay() = (%s, %t), want (%s, %t)", delay, retry, test.wantDelay, test.wantRetry)
			}
		})
	}
}

func TestRetryAfterOverflowFallsBack(t *testing.T) {
	got, present := retryAfter("9223372036854775807")
	if present || got != 0 {
		t.Fatalf("retryAfter() = (%s, %t), want (0s, false)", got, present)
	}
	delay, retry := RetryDelay(context.Background(), &APIError{StatusCode: http.StatusTooManyRequests, RetryAfter: got, retryAfterSet: present}, 3*time.Second)
	if !retry || delay != 3*time.Second {
		t.Fatalf("RetryDelay() = (%s, %t), want (3s, true)", delay, retry)
	}
}

func TestRuntimeSnapshotCommandsRejectInvalidKindsAndPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown kind", body: strings.Replace(snapshotJSON(time.Now().UTC()), `"kind":"cancel"`, `"kind":"pause"`, 1)},
		{name: "cancel payload", body: strings.Replace(snapshotJSON(time.Now().UTC()), `"payload":{}`, `"payload":{"reason":"later"}`, 1)},
		{name: "provide input payload", body: strings.Replace(snapshotJSON(time.Now().UTC()), `"kind":"cancel","payload":{}`, `"kind":"provide_input","payload":[]`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			_, err := mustMachineClient(t, server).Dispatch(context.Background(), "runtime-1", 1)
			if err == nil || !strings.Contains(err.Error(), "invalid runtime snapshot response") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCreateTaskCommandComparesNumericPayloadsPrecisely(t *testing.T) {
	t.Run("equivalent number spellings", func(t *testing.T) {
		server := jsonServer(t, http.StatusCreated, provideInputCommandJSON(`{"value":1e0,"zero":-0}`), nil)
		defer server.Close()
		_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"zero":0.0,"value":1.0}`)})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("distinct large integers", func(t *testing.T) {
		server := jsonServer(t, http.StatusCreated, provideInputCommandJSON(`{"value":9007199254740993}`), nil)
		defer server.Close()
		_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"value":9007199254740992}`)})
		if err == nil || !strings.Contains(err.Error(), "does not match the request") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestRetryDelayHonorsExplicitZeroRetryAfter(t *testing.T) {
	server := jsonServer(t, http.StatusTooManyRequests, `{"error":{"code":"rate_limited"}}`, map[string]string{"Retry-After": "0"})
	defer server.Close()

	_, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
	delay, retry := RetryDelay(context.Background(), err, 3*time.Second)
	if delay != 0 || !retry {
		t.Fatalf("RetryDelay() = (%s, %t), want (0s, true)", delay, retry)
	}
}

func newOperatorClient(t *testing.T, server *httptest.Server) *OperatorClient {
	t.Helper()
	client, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
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
