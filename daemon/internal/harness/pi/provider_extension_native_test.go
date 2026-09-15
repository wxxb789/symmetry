package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const nativeProviderBridgeActionSmokeEnv = "SYMMETRY_PI_PROVIDER_ACTION_SMOKE"

func TestNativePiProviderBridgeAction(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native Pi provider action smoke supports Linux and Windows only")
	}
	if os.Getenv(nativeProviderBridgeActionSmokeEnv) != "1" {
		t.Skip("set SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1 to run the native provider action smoke")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Pi 0.85.1 executable", nativeSmokeExecutableEnv)
	}

	gateway := &nativeProviderActionGateway{finalText: validTaskResultJSON(t)}
	server := httptest.NewServer(gateway)
	t.Cleanup(server.Close)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDirectory := filepath.Join(root, "sessions")
	for _, directory := range []string{workspace, sessionDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := nativePiRepositoryTaskLoopbackEnvironment(t, root, server.URL+"/v1")
	grants := testProviderGrants()
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(root, "extensions"), grants)
	if err != nil {
		t.Fatal(err)
	}

	var executeMutex sync.Mutex
	executeCalls := 0
	var executedActionID string
	var executedRequest ProviderBridgeRequest
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "native-provider-action-smoke",
		Grants:              grants,
		RequirePeerIdentity: true,
		Execute: func(_ context.Context, actionID string, request ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			executeMutex.Lock()
			defer executeMutex.Unlock()
			executeCalls++
			executedActionID = actionID
			executedRequest = request
			return ProviderBridgeResponse{
				Outcome: ProviderBridgeOutcomeSucceeded,
				Result:  json.RawMessage(`{"operation":"resource.sync","resource_id":"` + testProviderResourceID + `","projected":true,"resource":{"status":"healthy"}}`),
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	endpoint, err := bridge.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	started, err := NewAdapter(executable).Start(ctx, harness.StartRequest{
		Workspace: workspace,
		ProviderAccess: &protocol.ProviderAccess{
			Path:   "/api/v1/provider-actions",
			Token:  "native-provider-action-token-not-forwarded",
			Grants: grants,
		},
		ProviderBridge: &harness.ProviderBridgeLaunch{
			URL:             endpoint.URL,
			Nonce:           endpoint.Nonce,
			ExtensionPath:   extension.Path,
			ExtensionSHA256: extension.SHA256,
			Lifecycle:       bridge,
		},
		Invocation: execution.Invocation{
			Args: []string{
				"--no-skills",
				"--no-prompt-templates",
				"--no-themes",
				"--no-context-files",
				"--no-approve",
				"--no-builtin-tools",
				"--session-dir", sessionDirectory,
				"--provider", nativePiLoopbackProvider,
				"--model", nativePiLoopbackModel,
				"--thinking", nativePiLoopbackThinking,
			},
			Env: environment,
		},
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("start native Pi provider action transport: %v", err)
	}
	staged, ok := started.(harness.StagedSession)
	if !ok {
		t.Fatalf("session = %T, want staged session", started)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		_ = staged.Close(cleanupContext)
		_, _ = staged.Wait(cleanupContext)
	})
	if _, err := staged.Open(ctx); err != nil {
		t.Fatalf("open native Pi provider action transport: %v", err)
	}
	if err := staged.StartTurn(ctx, harness.TurnRequest{
		Goal:    "Synchronize the authorized connected repository once, then return the required task result.",
		Context: json.RawMessage(`{"task":"native_provider_action_smoke"}`),
	}); err != nil {
		t.Fatalf("start native Pi provider action turn: %v", err)
	}
	if err := staged.WaitTurn(ctx); err != nil {
		t.Fatalf("wait native Pi provider action turn: %v", err)
	}
	if err := staged.Close(ctx); err != nil {
		t.Fatalf("close native Pi provider action transport: %v", err)
	}
	result, err := staged.Wait(ctx)
	if err != nil {
		t.Fatalf("wait native Pi provider action transport: %v", err)
	}
	closed = true
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil {
		t.Fatalf("native Pi provider action result = %+v", result)
	}

	executeMutex.Lock()
	calls, actionID, request := executeCalls, executedActionID, executedRequest
	executeMutex.Unlock()
	wantActionKey := nativeProviderToolCallID + "|" + nativeProviderToolItemID
	if calls != 1 || request.Operation != "resource.sync" || request.ResourceID != testProviderResourceID || request.ActionKey != wantActionKey || string(request.Input) != `{}` {
		t.Fatalf("provider bridge execution = calls:%d action:%q request:%#v", calls, actionID, request)
	}
	if want := expectedProviderBridgeActionID("native-provider-action-smoke", request); actionID != want {
		t.Fatalf("provider action ID = %q, want %q", actionID, want)
	}
	requests, gatewayErr := gateway.state()
	if gatewayErr != nil || requests != 2 {
		t.Fatalf("provider action gateway = requests:%d error:%v", requests, gatewayErr)
	}
}

const (
	nativeProviderToolCallID = "call_symmetry_provider_sync"
	nativeProviderToolItemID = "fc_symmetry_provider_sync"
)

type nativeProviderActionGateway struct {
	mutex      sync.Mutex
	requests   int
	firstError error
	finalText  string
}

func (gateway *nativeProviderActionGateway) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v1/models":
		writeNativeProviderJSON(response, http.StatusOK, map[string]any{
			"object": "list",
			"data":   []any{map[string]any{"id": nativePiLoopbackModel, "object": "model", "owned_by": "symmetry-test"}},
		})
	case request.Method == http.MethodPost && request.URL.Path == "/v1/responses":
		gateway.handleResponse(response, request)
	default:
		gateway.fail(fmt.Errorf("unexpected route %s %s", request.Method, request.URL.Path))
		http.NotFound(response, request)
	}
}

func (gateway *nativeProviderActionGateway) handleResponse(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(io.LimitReader(request.Body, nativeCancellationMaxRequestBytes+1))
	if err != nil || len(body) == 0 || len(body) > nativeCancellationMaxRequestBytes {
		gateway.fail(errors.New("invalid Responses request body"))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		gateway.fail(fmt.Errorf("decode Responses request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	gateway.mutex.Lock()
	gateway.requests++
	requestNumber := gateway.requests
	gateway.mutex.Unlock()
	switch requestNumber {
	case 1:
		if !bytes.Contains(document["tools"], []byte(`"symmetry_resource_sync"`)) {
			gateway.fail(errors.New("generated provider tool was not advertised"))
		}
		writeNativeProviderSSE(response, nativeProviderToolEvents())
	case 2:
		if !bytes.Contains(document["input"], []byte(`"type":"function_call_output"`)) || !bytes.Contains(document["input"], []byte(nativeProviderToolCallID)) {
			gateway.fail(errors.New("provider tool result was not returned to the model"))
		}
		writeNativeProviderSSE(response, nativeProviderFinalEvents(gateway.finalText))
	default:
		gateway.fail(fmt.Errorf("unexpected Responses request count %d", requestNumber))
		http.Error(response, "unexpected request", http.StatusConflict)
	}
}

func nativeProviderToolEvents() []nativeProviderSSEEvent {
	arguments := `{"resource_id":"` + testProviderResourceID + `"}`
	item := map[string]any{
		"id": nativeProviderToolItemID, "type": "function_call", "status": "completed",
		"call_id": nativeProviderToolCallID, "name": "symmetry_resource_sync", "arguments": arguments,
	}
	added := cloneNativeProviderMap(item)
	added["status"] = "in_progress"
	response := nativeProviderResponse("resp_provider_tool", []any{item}, "completed")
	return []nativeProviderSSEEvent{
		{name: "response.created", payload: map[string]any{"type": "response.created", "sequence_number": 1, "response": nativeProviderResponse("resp_provider_tool", []any{item}, "in_progress")}},
		{name: "response.output_item.added", payload: map[string]any{"type": "response.output_item.added", "sequence_number": 2, "output_index": 0, "item": added}},
		{name: "response.function_call_arguments.delta", payload: map[string]any{"type": "response.function_call_arguments.delta", "sequence_number": 3, "item_id": nativeProviderToolItemID, "output_index": 0, "delta": arguments}},
		{name: "response.function_call_arguments.done", payload: map[string]any{"type": "response.function_call_arguments.done", "sequence_number": 4, "item_id": nativeProviderToolItemID, "output_index": 0, "arguments": arguments}},
		{name: "response.output_item.done", payload: map[string]any{"type": "response.output_item.done", "sequence_number": 5, "output_index": 0, "item": item}},
		{name: "response.completed", payload: map[string]any{"type": "response.completed", "sequence_number": 6, "response": response}},
	}
}

func nativeProviderFinalEvents(text string) []nativeProviderSSEEvent {
	content := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
	item := map[string]any{"id": "msg_provider_final", "type": "message", "status": "completed", "role": "assistant", "content": []any{content}}
	added := cloneNativeProviderMap(item)
	added["status"] = "in_progress"
	response := nativeProviderResponse("resp_provider_final", []any{item}, "completed")
	return []nativeProviderSSEEvent{
		{name: "response.created", payload: map[string]any{"type": "response.created", "sequence_number": 1, "response": nativeProviderResponse("resp_provider_final", []any{item}, "in_progress")}},
		{name: "response.output_item.added", payload: map[string]any{"type": "response.output_item.added", "sequence_number": 2, "output_index": 0, "item": added}},
		{name: "response.content_part.added", payload: map[string]any{"type": "response.content_part.added", "sequence_number": 3, "output_index": 0, "content_index": 0, "item_id": "msg_provider_final", "part": content}},
		{name: "response.output_text.delta", payload: map[string]any{"type": "response.output_text.delta", "sequence_number": 4, "output_index": 0, "content_index": 0, "item_id": "msg_provider_final", "delta": text}},
		{name: "response.output_text.done", payload: map[string]any{"type": "response.output_text.done", "sequence_number": 5, "output_index": 0, "content_index": 0, "item_id": "msg_provider_final", "text": text}},
		{name: "response.content_part.done", payload: map[string]any{"type": "response.content_part.done", "sequence_number": 6, "output_index": 0, "content_index": 0, "item_id": "msg_provider_final", "part": content}},
		{name: "response.output_item.done", payload: map[string]any{"type": "response.output_item.done", "sequence_number": 7, "output_index": 0, "item": item}},
		{name: "response.completed", payload: map[string]any{"type": "response.completed", "sequence_number": 8, "response": response}},
	}
}

func nativeProviderResponse(id string, output []any, status string) map[string]any {
	return map[string]any{
		"id": id, "object": "response", "created_at": time.Now().Unix(), "status": status,
		"model": nativePiLoopbackModel, "output": output,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
	}
}

type nativeProviderSSEEvent struct {
	name    string
	payload any
}

func writeNativeProviderSSE(response http.ResponseWriter, events []nativeProviderSSEEvent) {
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Content-Type", "text/event-stream")
	response.WriteHeader(http.StatusOK)
	for _, event := range events {
		encoded, _ := json.Marshal(event.payload)
		_, _ = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event.name, encoded)
	}
	_, _ = io.WriteString(response, "data: [DONE]\n\n")
}

func writeNativeProviderJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func cloneNativeProviderMap(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func (gateway *nativeProviderActionGateway) fail(err error) {
	gateway.mutex.Lock()
	defer gateway.mutex.Unlock()
	if gateway.firstError == nil {
		gateway.firstError = err
	}
}

func (gateway *nativeProviderActionGateway) state() (int, error) {
	gateway.mutex.Lock()
	defer gateway.mutex.Unlock()
	return gateway.requests, gateway.firstError
}
