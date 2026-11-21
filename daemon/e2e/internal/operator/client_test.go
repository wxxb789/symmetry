package operator

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
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const machineToken = "machine-token"

func TestOperatorClientProtocolRequests(t *testing.T) {
	var operatorClient *OperatorClient
	tests := []struct {
		name       string
		invoke     func(context.Context) error
		method     string
		path       string
		wantAuth   string
		wantHeader string
		wantBody   string
		status     int
		response   string
	}{
		{
			name: "submit task", method: http.MethodPost, path: "/api/v1/tasks", wantAuth: "Bearer operator-token", wantHeader: "submit-1",
			wantBody: `{"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}`, response: taskJSON(),
			invoke: func(ctx context.Context) error {
				response, err := operatorClient.SubmitTask(ctx, "submit-1", protocol.TaskSubmitRequest{Work: work()})
				if err == nil && response.TaskID != "task-1" {
					return fmt.Errorf("task id = %q", response.TaskID)
				}
				return err
			},
		},
		{
			name: "get task", method: http.MethodGet, path: "/api/v1/tasks/task-1", wantAuth: "Bearer operator-token",
			response: taskJSONWithUnknownField(),
			invoke: func(ctx context.Context) error {
				_, err := operatorClient.GetTask(ctx, "task-1")
				return err
			},
		},
		{
			name: "create cancel command", method: http.MethodPost, path: "/api/v1/tasks/task-1/commands", wantAuth: "Bearer operator-token", wantHeader: "cancel-1",
			wantBody: `{"kind":"cancel"}`, response: commandJSON(),
			invoke: func(ctx context.Context) error {
				_, err := operatorClient.CreateTaskCommand(ctx, "task-1", "cancel-1", protocol.TaskCommandRequest{Kind: "cancel"})
				return err
			},
		},
		{
			name: "create provide input command", method: http.MethodPost, path: "/api/v1/tasks/task-1/commands", wantAuth: "Bearer operator-token", wantHeader: "input-1",
			wantBody: `{"kind":"provide_input","payload":{"answer":"main"}}`, response: provideInputCommandJSON(`{"answer":"main"}`),
			invoke: func(ctx context.Context) error {
				_, err := operatorClient.CreateTaskCommand(ctx, "task-1", "input-1", protocol.TaskCommandRequest{Kind: "provide_input", Payload: json.RawMessage(`{"answer":"main"}`)})
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
				if test.status != 0 {
					writer.WriteHeader(test.status)
				}
				if test.response != "" {
					writer.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(writer, test.response)
				}
			}))
			defer server.Close()

			var err error
			operatorClient, err = NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := test.invoke(context.Background()); err != nil {
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
		{name: "terminal grace expired", status: http.StatusConflict, body: `{"error":{"code":"terminal_grace_expired","message":"terminal delivery expired"}}`, wantCode: TerminalGraceExpired, wantError: "terminal_grace_expired"},
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
				if test.wantCode == TerminalGraceExpired && !IsTerminalGraceExpired(err) {
					t.Error("IsTerminalGraceExpired() = false")
				}
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestClientUsesProtocolAPIPrefixForOriginBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/api/v1/tasks/task-1"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_, _ = io.WriteString(writer, taskJSON())
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

func TestNewOperatorClientRejectsInvalidConfiguration(t *testing.T) {
	for _, baseURL := range []string{"", "ftp://control.example.test", "https://user:pass@control.example.test", "https://control.example.test?x=1", "https://control.example.test?", "https://control.example.test#fragment"} {
		if _, err := NewOperatorClient(baseURL, "operator-token", nil); err == nil {
			t.Errorf("NewOperatorClient(%q) succeeded", baseURL)
		}
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

func work() protocol.Work {
	return protocol.Work{Goal: "work", AgentProfile: "codex", Workspace: "primary", Input: json.RawMessage(`{}`)}
}

func TestU2CreateTaskCommandUsesTaskOwnedRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := request.URL.Path, "/api/v1/tasks/task-1/commands"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("Idempotency-Key"), "command-1"; got != want {
			t.Errorf("Idempotency-Key = %q, want %q", got, want)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, `{"kind":"cancel"}`, string(body))
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, commandJSON())
	}))
	defer server.Close()

	client, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateTaskCommand(context.Background(), "task-1", "command-1", protocol.TaskCommandRequest{Kind: "cancel"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateFencedCancelPreservesGenerationWithoutPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/tasks/task-1/commands" {
			t.Errorf("unexpected route: %s %s", request.Method, request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, `{"kind":"cancel","generation":2}`, string(body))
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, strings.Replace(commandJSON(), `"generation":1`, `"generation":2`, 1))
	}))
	defer server.Close()
	_, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "cancel-generation-2", protocol.TaskCommandRequest{Kind: "cancel", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateFencedCancelAcceptsOnlyValidRunlessCancellationOrMatchingGeneration(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"queued cancellation has no run", runlessAppliedCommandJSON(), true},
		{"assigned generation mismatch", commandJSON(), false},
		{"runless pending is invalid", strings.Replace(runlessAppliedCommandJSON(), `"state":"applied"`, `"state":"pending"`, 1), false},
		{"runless wrong task is invalid", strings.Replace(runlessAppliedCommandJSON(), `"task_id":"task-1"`, `"task_id":"task-2"`, 1), false},
		{"runless other kind is invalid", strings.Replace(runlessAppliedCommandJSON(), `"kind":"cancel"`, `"kind":"retry"`, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEqual(t, `{"kind":"cancel","generation":2}`, string(body))
				writer.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			command, err := newOperatorClient(t, server).CreateTaskCommand(context.Background(), "task-1", "fenced-queued-cancel", protocol.TaskCommandRequest{Kind: "cancel", Generation: 2})
			if (err == nil) != test.valid {
				t.Fatalf("fenced cancel response = %#v, %v; want valid %t", command, err, test.valid)
			}
			if test.valid && (command.RunID != nil || command.Generation != nil || command.State != "applied") {
				t.Fatalf("queued cancellation invented a run: %#v", command)
			}
		})
	}
}

func TestSubmitTaskPreservesOptionalWorkInputSerialization(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  string
	}{
		{name: "omitted", want: `{"work":{"goal":"work","agent_profile":"codex","workspace":"primary"}}`},
		{name: "explicit null", input: json.RawMessage(`null`), want: `{"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":null}}`},
		{name: "empty object", input: json.RawMessage(`{}`), want: `{"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEqual(t, test.want, string(body))
				_, _ = io.WriteString(writer, taskJSON())
			}))
			defer server.Close()

			_, err := newOperatorClient(t, server).SubmitTask(context.Background(), "task-1", protocol.TaskSubmitRequest{Work: protocol.Work{
				Goal: "work", AgentProfile: "codex", Workspace: "primary", Input: test.input,
			}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func commandJSON() string {
	return `{"command_id":"command-1","task_id":"task-1","run_id":"run-1","generation":1,"kind":"cancel","payload":{},"state":"pending","issued_at":"2026-09-03T00:00:00Z","applied_at":null,"acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null}`
}

func runlessAppliedCommandJSON() string {
	return `{"command_id":"command-1","task_id":"task-1","run_id":null,"generation":null,"kind":"cancel","payload":{},"state":"applied","issued_at":"2026-09-03T00:00:00Z","applied_at":"2026-09-03T00:00:01Z","acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null}`
}

func acknowledgedCommandJSON() string {
	return acknowledgedCommandForGeneration(1)
}

func acknowledgedCommandForGeneration(generation int64) string {
	return fmt.Sprintf(`{"command_id":"command-1","task_id":"task-1","run_id":"run-1","generation":%d,"kind":"cancel","payload":{},"state":"acknowledged","issued_at":"2026-09-03T00:00:00Z","applied_at":null,"acknowledgement_id":"ack-1","acknowledgement_outcome":"applied","acknowledged_at":"2026-09-03T00:00:01Z"}`, generation)
}

func provideInputCommandJSON(payload string) string {
	return strings.Replace(strings.Replace(commandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1), `"payload":{}`, `"payload":`+payload, 1)
}

func taskJSON() string {
	return `{"task_id":"task-1","state":"queued","run_id":null,"generation":1,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}},"result":null,"failure":null,"waiting":null,"latest_command":null}`
}

func taskJSONWithUnknownField() string {
	return `{"task_id":"task-1","state":"queued","run_id":null,"generation":1,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}},"result":null,"failure":null,"waiting":null,"latest_command":null,"future_field":{"kept":"compatible"}}`
}

func waitingTaskJSON() string {
	return `{"task_id":"task-1","state":"waiting_for_input","run_id":"run-current","generation":2,"work":{"goal":"work","agent_profile":"codex","workspace":"primary","input":{}},"result":null,"failure":null,"waiting":{"run_id":"run-current","generation":2,"transition_id":"transition-1","question":"Choose the target branch","payload":{"question":"Choose the target branch"},"recorded_at":"2026-09-03T00:00:00Z","future_waiting_field":true},"latest_command":{"command_id":"command-1","task_id":"task-1","run_id":"run-earlier","generation":1,"kind":"cancel","payload":{},"state":"applied","issued_at":"2026-09-03T00:00:00Z","applied_at":"2026-09-03T00:00:01Z","acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null,"future_command_field":true}}`
}

func retriedTaskJSON() string {
	return `{"task_id":"task-1","state":"queued","run_id":null,"generation":2,"work":{"goal":"work again","agent_profile":"codex","workspace":"primary","input":{}},"result":null,"failure":null,"waiting":null,"latest_command":{"command_id":"command-2","task_id":"task-1","run_id":"run-earlier","generation":1,"kind":"retry","payload":{"work":{"goal":"work again","agent_profile":"codex","workspace":"primary","input":{}}},"state":"applied","issued_at":"2026-09-03T00:00:00Z","applied_at":"2026-09-03T00:00:01Z","acknowledgement_id":null,"acknowledgement_outcome":null,"acknowledged_at":null}}`
}

func TestGetTaskProjectsWaitingAndHistoricalLatestCommand(t *testing.T) {
	server := jsonServer(t, http.StatusOK, waitingTaskJSON(), nil)
	defer server.Close()

	task, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Waiting == nil {
		t.Fatal("waiting = nil")
	}
	if task.Waiting.RunID != "run-current" || task.Waiting.Generation != 2 || task.Waiting.TransitionID != "transition-1" || task.Waiting.Question == nil || *task.Waiting.Question != "Choose the target branch" {
		t.Fatalf("waiting = %#v", task.Waiting)
	}
	if string(task.Waiting.Payload) != `{"question":"Choose the target branch"}` || task.Waiting.RecordedAt.IsZero() {
		t.Fatalf("waiting payload or recorded_at = %#v", task.Waiting)
	}
	if task.LatestCommand == nil {
		t.Fatal("latest_command = nil")
	}
	if task.LatestCommand.TaskID != task.TaskID || task.LatestCommand.RunID == nil || *task.LatestCommand.RunID != "run-earlier" || task.LatestCommand.Generation == nil || *task.LatestCommand.Generation != 1 {
		t.Fatalf("latest_command = %#v", task.LatestCommand)
	}
}

func TestGetTaskAcceptsQueuedRetryAttemptAndHistoricalRetryCommand(t *testing.T) {
	server := jsonServer(t, http.StatusOK, retriedTaskJSON(), nil)
	defer server.Close()

	task, err := newOperatorClient(t, server).GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.RunID != nil || task.Generation == nil || *task.Generation != 2 {
		t.Fatalf("task attempt = run %#v generation %#v", task.RunID, task.Generation)
	}
	if task.LatestCommand == nil || task.LatestCommand.Kind != "retry" || task.LatestCommand.State != "applied" {
		t.Fatalf("latest_command = %#v", task.LatestCommand)
	}
}
