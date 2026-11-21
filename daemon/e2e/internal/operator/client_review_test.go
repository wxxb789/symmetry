package operator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestOperatorClientRejectsIncompleteSuccessResponses(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
		invoke    func(*OperatorClient) error
	}{
		{
			name: "task", body: `{"state":"queued"}`, wantError: "invalid get task response",
			invoke: func(operator *OperatorClient) error {
				_, err := operator.GetTask(context.Background(), "task-1")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := jsonServer(t, http.StatusOK, test.body, nil)
			defer server.Close()
			operator, err := NewOperatorClient(server.URL+"/api", "operator-token", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			err = test.invoke(operator)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
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
	if _, ok := reflect.TypeOf(machine).MethodByName("GetTask"); ok {
		t.Fatal("machine Client must not implement task APIs")
	}
}

func TestAuthenticatedRequestsRejectRedirectsBeforeDowngrade(t *testing.T) {
	var sourceRequests atomic.Int32
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetRequests.Add(1)
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect target Authorization = %q, want empty", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, taskJSON())
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sourceRequests.Add(1)
		if got, want := request.Header.Get("Authorization"), "Bearer operator-token"; got != want {
			t.Errorf("source Authorization = %q, want %q", got, want)
		}
		http.Redirect(writer, request, target.URL+"/api/v1/tasks/task-1", http.StatusFound)
	}))
	defer source.Close()

	permissive := source.Client()
	var permissiveRedirects atomic.Int32
	permissive.CheckRedirect = func(*http.Request, []*http.Request) error {
		permissiveRedirects.Add(1)
		return nil
	}
	client, err := NewOperatorClient(source.URL+"/api", "operator-token", permissive)
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient == permissive {
		t.Fatal("NewOperatorClient reused the caller-owned HTTP client")
	}

	_, err = client.GetTask(context.Background(), "task-1")
	if !errors.Is(err, errRedirectNotAllowed) {
		t.Fatalf("error = %v, want redirect policy error", err)
	}
	if delay, retry := RetryDelay(context.Background(), err, time.Second); retry || delay != 0 {
		t.Fatalf("RetryDelay() = (%s, %t), want (0s, false)", delay, retry)
	}
	if got := sourceRequests.Load(); got != 1 {
		t.Fatalf("source requests = %d, want 1", got)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("target requests = %d, want 0", got)
	}
	if got := permissiveRedirects.Load(); got != 0 {
		t.Fatalf("caller CheckRedirect calls = %d, want 0", got)
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
		{name: "task generation is not positive", body: strings.Replace(taskJSON(), `"generation":1`, `"generation":0`, 1), wantError: "task generation must be positive"},
		{name: "active task without run", body: strings.Replace(taskJSON(), `"state":"queued"`, `"state":"running"`, 1), wantError: "state requires a run_id"},
		{name: "completed task without result", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":1`, `"state":"completed","run_id":"run-1","generation":1`, 1), wantError: "completed state requires result"},
		{name: "failed task without failure", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":1`, `"state":"failed","run_id":"run-1","generation":1`, 1), wantError: "failed state requires failure"},
		{name: "waiting state without waiting projection", body: strings.Replace(taskJSON(), `"state":"queued","run_id":null,"generation":1`, `"state":"waiting_for_input","run_id":"run-1","generation":1`, 1), wantError: "waiting_for_input state requires waiting"},
		{name: "waiting projection outside waiting state", body: strings.Replace(taskJSON(), `"waiting":null`, `"waiting":{"run_id":"run-1","generation":1,"transition_id":"transition-1","question":"Choose","payload":{},"recorded_at":"2026-09-03T00:00:00Z"}`, 1), wantError: "only waiting_for_input state may include waiting"},
		{name: "waiting run differs from current run", body: strings.Replace(waitingTaskJSON(), `"run_id":"run-current","generation":2,"transition_id"`, `"run_id":"run-other","generation":2,"transition_id"`, 1), wantError: "waiting run_id and generation must match the current run"},
		{name: "waiting misses required field", body: strings.Replace(waitingTaskJSON(), `,"question":"Choose the target branch"`, "", 1), wantError: "waiting question is required"},
		{name: "waiting payload is not object", body: strings.Replace(waitingTaskJSON(), `"payload":{"question":"Choose the target branch"}`, `"payload":[]`, 1), wantError: "waiting payload must be a JSON object"},
		{name: "latest command task differs", body: strings.Replace(waitingTaskJSON(), `"task_id":"task-1","run_id":"run-earlier"`, `"task_id":"task-other","run_id":"run-earlier"`, 1), wantError: "latest_command task_id does not match expected task"},
		{name: "latest command violates resource invariant", body: strings.Replace(waitingTaskJSON(), `"state":"applied","issued_at"`, `"state":"pending","issued_at"`, 1), wantError: "latest_command pending command must be run-bound without applied_at or acknowledgement"},
		{name: "retry command cannot remain pending", body: strings.Replace(strings.Replace(retriedTaskJSON(), `"state":"applied"`, `"state":"pending"`, 1), `"applied_at":"2026-09-03T00:00:01Z"`, `"applied_at":null`, 1), wantError: "latest_command pending command must be run-bound without applied_at or acknowledgement"},
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
		{name: "unknown kind", key: "command-1", request: protocol.TaskCommandRequest{Kind: "teleport"}, wantError: "kind is not recognized"},
		{name: "retry is read only", key: "command-1", request: protocol.TaskCommandRequest{Kind: "retry", Payload: json.RawMessage(`{"work":{}}`)}, wantError: "kind is not recognized"},
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
		{name: "applied provide input without timestamp", body: strings.Replace(strings.Replace(commandJSON(), `"kind":"cancel"`, `"kind":"provide_input"`, 1), `"state":"pending"`, `"state":"applied"`, 1)},
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

func mustMachineClient(t *testing.T, server *httptest.Server) *control.Client {
	t.Helper()
	client, err := control.NewClient(server.URL+"/api", machineToken, server.Client())
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
