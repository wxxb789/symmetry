package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

const (
	nativeClaudeSmokeEnv           = "SYMMETRY_CLAUDE_NATIVE_SMOKE"
	nativeClaudeSmokeExecutableEnv = "SYMMETRY_CLAUDE_NATIVE_SMOKE_EXECUTABLE"
)

// TestNativeClaudeCandidateStructuredOutput drives the production candidate
// against the real Claude Code binary and a loopback fake Messages API. It
// proves only native --json-schema delivery and structured_output decoding;
// no real model, credential, or capability promotion is involved.
func TestNativeClaudeCandidateStructuredOutput(t *testing.T) {
	if os.Getenv(nativeClaudeSmokeEnv) != "1" {
		t.Skip("set SYMMETRY_CLAUDE_NATIVE_SMOKE=1 to run the native Claude structured-output smoke")
	}
	executable := strings.TrimSpace(os.Getenv(nativeClaudeSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Claude Code %s executable", nativeClaudeSmokeExecutableEnv, testedClaudeVersion)
	}

	gateway := &nativeClaudeMessagesGateway{taskResult: json.RawMessage(validClaudeCandidateTaskResultJSON(t))}
	server := httptest.NewServer(gateway)
	t.Cleanup(server.Close)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	configDirectory := filepath.Join(root, "claude-config")
	for _, directory := range []string{workspace, home, configDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment, err := execution.BuildEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	isolated := environment[:0]
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH":
			continue
		}
		isolated = append(isolated, entry)
	}
	environment = append(isolated,
		"HOME="+home,
		"USERPROFILE="+home,
		"ANTHROPIC_BASE_URL="+server.URL,
		"ANTHROPIC_API_KEY=sk-ant-test-fake",
		"CLAUDE_CONFIG_DIR="+configDirectory,
		"DISABLE_TELEMETRY=1",
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sink := &recordingClaudeCandidateSink{}
	t.Cleanup(func() {
		if t.Failed() {
			for _, event := range sink.eventsSnapshot() {
				t.Logf("native event %s %s %s: %.300s", event.Kind, event.Code, event.Message, event.Payload)
			}
		}
	})
	started, err := NewClaudeCandidateAdapter(executable).Start(ctx, StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
	}, sink)
	if err != nil {
		t.Fatalf("start native Claude candidate: %v", err)
	}
	staged := started.(StagedSession)
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), claudeCandidateCleanupTimeout+claudeCandidateTerminationGrace)
		defer cleanupCancel()
		_ = staged.Close(cleanupContext)
		_, _ = staged.Wait(cleanupContext)
	})
	// Open waits for system/init before any prompt is written. Bound it so a
	// binary that defers init until the first user frame fails visibly.
	openContext, openCancel := context.WithTimeout(ctx, 30*time.Second)
	_, err = staged.Open(openContext)
	openCancel()
	if err != nil {
		t.Fatalf("open native Claude candidate (no system/init before the first user frame): %v", err)
	}
	if err := staged.StartTurn(ctx, TurnRequest{Goal: "Return the Symmetry task result.", Context: json.RawMessage(`{"task":"native_claude_structured_output_smoke"}`)}); err != nil {
		t.Fatalf("start native Claude turn: %v", err)
	}
	if err := staged.WaitTurn(ctx); err != nil {
		t.Fatalf("wait native Claude turn: %v", err)
	}
	if err := staged.Close(ctx); err != nil {
		t.Fatalf("close native Claude candidate: %v", err)
	}
	result, err := staged.Wait(ctx)
	if err != nil {
		t.Fatalf("wait native Claude candidate: %v", err)
	}
	closed = true
	if result.Kind != ResultSucceeded || result.Semantic == nil || result.Semantic.Summary != "made bounded progress" {
		t.Fatalf("native Claude result = %+v, want semantic TaskResult", result)
	}

	inputSchema, prompts, gatewayErr := gateway.state()
	if gatewayErr != nil {
		t.Fatalf("fake Messages API: %v", gatewayErr)
	}
	encodedSchema, err := claudeCandidateStructuredOutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(encodedSchema, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(inputSchema, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("StructuredOutput input_schema (%d bytes, %v) differs from the TaskResult schema", len(inputSchema), err)
	}
	if !strings.Contains(prompts, "Return the Symmetry task result.") || strings.Contains(prompts, "<symmetry_task_result_schema>") {
		t.Fatalf("model request user prompt = %q, want goal without inline schema", prompts)
	}
}

// nativeClaudeMessagesGateway is a zero-spend loopback Anthropic Messages API.
// Requests that offer the StructuredOutput tool get one tool_use carrying the
// configured TaskResult; any side request gets a short text reply.
type nativeClaudeMessagesGateway struct {
	taskResult json.RawMessage

	mutex       sync.Mutex
	requests    int
	inputSchema json.RawMessage
	prompts     string
	firstError  error
}

func (gateway *nativeClaudeMessagesGateway) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !strings.HasPrefix(request.URL.Path, "/v1/messages") {
		writeNativeClaudeJSON(response, map[string]any{})
		return
	}
	if strings.HasPrefix(request.URL.Path, "/v1/messages/count_tokens") {
		writeNativeClaudeJSON(response, map[string]any{"input_tokens": 10})
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 8<<20))
	var document struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
		Tools  []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err == nil {
		err = json.Unmarshal(body, &document)
	}
	if err != nil {
		gateway.fail(fmt.Errorf("decode Messages request: %w", err))
		http.Error(response, "invalid request", http.StatusBadRequest)
		return
	}
	var structured json.RawMessage
	for _, tool := range document.Tools {
		if tool.Name == "StructuredOutput" {
			structured = tool.InputSchema
		}
	}
	gateway.mutex.Lock()
	gateway.requests++
	sequence := gateway.requests
	if structured != nil && gateway.inputSchema == nil {
		gateway.inputSchema = append(json.RawMessage(nil), structured...)
		for _, message := range document.Messages {
			if message.Role == "user" {
				gateway.prompts += string(message.Content)
			}
		}
	}
	gateway.mutex.Unlock()

	last := json.RawMessage(nil)
	if len(document.Messages) > 0 {
		last = document.Messages[len(document.Messages)-1].Content
	}
	if structured == nil || strings.Contains(string(last), `"tool_result"`) {
		writeNativeClaudeMessage(response, document.Model, document.Stream, sequence, map[string]any{"type": "text", "text": "done"}, "end_turn")
		return
	}
	writeNativeClaudeMessage(response, document.Model, document.Stream, sequence, map[string]any{
		"type": "tool_use", "id": fmt.Sprintf("toolu_fake_%d", sequence), "name": "StructuredOutput", "input": gateway.taskResult,
	}, "tool_use")
}

func (gateway *nativeClaudeMessagesGateway) fail(err error) {
	gateway.mutex.Lock()
	defer gateway.mutex.Unlock()
	if gateway.firstError == nil {
		gateway.firstError = err
	}
}

func (gateway *nativeClaudeMessagesGateway) state() (json.RawMessage, string, error) {
	gateway.mutex.Lock()
	defer gateway.mutex.Unlock()
	if gateway.firstError == nil && gateway.inputSchema == nil {
		return nil, "", fmt.Errorf("no request offered the StructuredOutput tool in %d requests", gateway.requests)
	}
	return gateway.inputSchema, gateway.prompts, gateway.firstError
}

func writeNativeClaudeJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("request-id", "req_fake")
	_ = json.NewEncoder(response).Encode(value)
}

func writeNativeClaudeMessage(response http.ResponseWriter, model string, stream bool, sequence int, block map[string]any, stopReason string) {
	id := fmt.Sprintf("msg_fake_%d", sequence)
	usage := map[string]any{"input_tokens": 10, "output_tokens": 5, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	if !stream {
		writeNativeClaudeJSON(response, map[string]any{"id": id, "type": "message", "role": "assistant", "model": model, "content": []any{block}, "stop_reason": stopReason, "stop_sequence": nil, "usage": usage})
		return
	}
	start := map[string]any{"type": block["type"]}
	var delta map[string]any
	if block["type"] == "tool_use" {
		start["id"], start["name"], start["input"] = block["id"], block["name"], map[string]any{}
		input, _ := json.Marshal(block["input"])
		delta = map[string]any{"type": "input_json_delta", "partial_json": string(input)}
	} else {
		start["text"] = ""
		delta = map[string]any{"type": "text_delta", "text": block["text"]}
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("request-id", "req_fake")
	for _, event := range []struct {
		name    string
		payload map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": id, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": start}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": delta}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 5}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	} {
		encoded, _ := json.Marshal(event.payload)
		_, _ = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event.name, encoded)
	}
}
