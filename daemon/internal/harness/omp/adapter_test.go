package omp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type scriptedProcess struct {
	commands             chan map[string]any
	eof, terminate, done chan struct{}
	eofOnce, stopOnce    sync.Once
	mu                   sync.Mutex
	result               execution.Result
	terminateFailures    int
	promptSeen           chan struct{}
	filename, sessionID  string
	frames               func(map[string]any) []any
}

func newScriptedProcess(filename string, frames func(map[string]any) []any) *scriptedProcess {
	return &scriptedProcess{commands: make(chan map[string]any), eof: make(chan struct{}), terminate: make(chan struct{}), done: make(chan struct{}), promptSeen: make(chan struct{}), filename: filename, sessionID: "native-session", frames: frames}
}

func (process *scriptedProcess) run(ctx context.Context, sink execution.Sink) {
	defer close(process.done)
	emit := func(value any) bool {
		data, _ := json.Marshal(value)
		err := sink.Handle(ctx, execution.Event{Stream: execution.Stdout, Data: append(data, '\n')})
		if err != nil {
			process.result.SinkError = err
			return false
		}
		return true
	}
	if !emit(map[string]any{"type": "ready", "protocolVersion": 1, "supportedProtocolVersions": []int{1, 2}, "maxFrameBytes": maxFrameBytes, "maxReassembledFrameBytes": maxLogicalBytes}) {
		return
	}
	for {
		select {
		case request := <-process.commands:
			id, kind := request["id"], request["type"]
			var data any
			switch kind {
			case "negotiate_protocol":
				data = map[string]any{"protocolVersion": 2}
			case "get_state":
				data = map[string]any{"sessionId": process.sessionID, "sessionFile": process.filename, "isStreaming": false, "isCompacting": false, "queuedMessageCount": 0, "hasPendingAsyncWork": false, "isSettled": true}
			case "prompt":
				close(process.promptSeen)
			case "abort_and_restore_queue":
				data = map[string]any{"steering": []any{}, "followUp": []any{}}
			}
			response := map[string]any{"type": "response", "id": id, "command": kind, "success": true}
			if data != nil {
				response["data"] = data
			}
			values := []any{response}
			if process.frames != nil {
				if custom := process.frames(request); custom != nil {
					values = custom
				}
			}
			for _, value := range values {
				if !emit(value) {
					return
				}
			}
		case <-process.eof:
			return
		case <-process.terminate:
			process.result.Terminated = true
			return
		case <-ctx.Done():
			process.result.Terminated = true
			process.result.WaitError = ctx.Err()
			return
		}
	}
}

func (process *scriptedProcess) WriteInputContext(ctx context.Context, data []byte) error {
	var request map[string]any
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	select {
	case process.commands <- request:
		return nil
	case <-process.done:
		return execution.ErrInputClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (process *scriptedProcess) Wait() execution.Result        { <-process.done; return process.result }
func (process *scriptedProcess) ProcessDetails() (int, string) { return 4123, "owned-process" }
func (process *scriptedProcess) CloseInput() error {
	process.eofOnce.Do(func() { close(process.eof) })
	return nil
}
func (process *scriptedProcess) Terminate(ctx context.Context, _ time.Duration) error {
	process.mu.Lock()
	if process.terminateFailures > 0 {
		process.terminateFailures--
		process.mu.Unlock()
		return errors.New("physical stop unavailable")
	}
	process.mu.Unlock()
	process.stopOnce.Do(func() { close(process.terminate) })
	select {
	case <-process.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func testSession(t *testing.T, frames func(map[string]any) []any, sink harness.EventSink, resume *harness.ResumeHandle) (*nativeSession, *scriptedProcess) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(directory, "native-session.jsonl")
	if resume != nil {
		resume.NativeSessionFilename = filename
		if err := os.WriteFile(filename, []byte("retained session"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	process := newScriptedProcess(filename, frames)
	adapter := NewAdapter("candidate-omp")
	adapter.probe = func(context.Context) (harness.Capabilities, error) {
		return observedCapabilities(), harness.ErrNativeUnverified
	}
	persisted := false
	adapter.start = func(ctx context.Context, invocation execution.Invocation, output execution.Sink) (harness.NativeProcess, error) {
		if invocation.PersistProcess != nil {
			if err := invocation.PersistProcess(4123, "owned-process"); err != nil {
				return nil, err
			}
		}
		go process.run(ctx, output)
		return process, nil
	}
	if sink == nil {
		sink = harness.EventSinkFunc(func(context.Context, harness.Event) error {
			if !persisted {
				return errors.New("output preceded process persistence")
			}
			return nil
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	value, err := adapter.Start(ctx, harness.StartRequest{Workspace: directory, Invocation: execution.Invocation{Args: []string{"--session-dir", directory}}, Resume: resume, PersistProcess: func(int, string) error { persisted = true; return nil }}, sink)
	if err != nil {
		t.Fatal(err)
	}
	session := value.(*nativeSession)
	t.Cleanup(func() {
		bounded, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := session.Close(bounded); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return session, process
}

func responseFor(request map[string]any) map[string]any {
	return map[string]any{"type": "response", "id": request["id"], "command": request["type"], "success": true}
}

func terminalFrames(request map[string]any, text, status string) []any {
	stop := "stop"
	if status == "error" {
		stop = "error"
	}
	return []any{responseFor(request), map[string]any{"type": "agent_start"}, map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": stop, "content": []any{map[string]any{"type": "text", "text": text}}}}, map[string]any{"type": "agent_end", "messages": []any{}}, map[string]any{"type": "prompt_result", "id": request["id"], "status": status, "agentInvoked": true, "sessionSettled": true}, map[string]any{"type": "session_settled"}}
}

func validTaskText(t *testing.T) string {
	t.Helper()
	subject := protocol.Subject{ResourceID: "11111111-1111-4111-8111-111111111111", Commit: "1111111111111111111111111111111111111111", TreeDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}
	hash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	result := protocol.TaskResult{SchemaVersion: protocol.TaskResultSchemaVersion, ResultID: "22222222-2222-4222-8222-222222222222", Kind: protocol.TaskResultProgress, Summary: "Observed repository progress", Subject: subject, SubjectHash: hash, EvidenceRefs: []string{}, Diagnostics: []protocol.Diagnostic{}}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func startTestTurn(t *testing.T, session *nativeSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect admitted repository", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestStagedPromptAndPhysicalResultBarriers(t *testing.T) {
	text := validTaskText(t)
	var eventsMutex sync.Mutex
	startedEvents := 0
	session, process := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			return terminalFrames(request, text, "completed")
		}
		return nil
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		eventsMutex.Lock()
		defer eventsMutex.Unlock()
		if event.Kind == harness.EventSessionStarted {
			startedEvents++
		}
		return nil
	}), nil)
	select {
	case <-process.promptSeen:
		t.Fatal("Start sent a prompt")
	default:
	}
	// This bounds the fixture, not native latency; race instrumentation and
	// first-use schema validation still get the adapter's operation budget.
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	handle, err := session.Open(ctx)
	if err != nil || handle.ID != "native-session" {
		t.Fatalf("Open: %+v, %v", handle, err)
	}
	if _, err := os.Stat(handle.Filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fresh handle should allow lazy retained file, got %v", err)
	}
	select {
	case <-process.promptSeen:
		t.Fatal("Open sent a prompt")
	default:
	}
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitTurn(ctx); err != nil {
		t.Fatal(err)
	}
	eventsMutex.Lock()
	starts := startedEvents
	eventsMutex.Unlock()
	if starts != 1 {
		t.Fatalf("admitted turn emitted %d normalized session-start events, want one", starts)
	}
	short, stop := context.WithTimeout(ctx, 10*time.Millisecond)
	if _, err := session.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait bypassed physical barrier: %v", err)
	}
	stop()
	short, stop = context.WithTimeout(ctx, 10*time.Millisecond)
	if _, err := session.WaitStopped(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitStopped bypassed physical barrier: %v", err)
	}
	stop()
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Second", Context: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("second prompt admitted")
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil || result.Usage.State != harness.UsageUnknown {
		t.Fatalf("Wait=%+v, %v", result, err)
	}
}

func TestProtocolFailuresCannotBecomeSuccess(t *testing.T) {
	text := validTaskText(t)
	for _, scenario := range []string{"unknown-negotiation", "wrong-response-id", "duplicate-response", "settled-before-result", "missing-final", "fenced-json", "embedded-json", "duplicate-prompt-result", "schema-invalid-summary"} {
		t.Run(scenario, func(t *testing.T) {
			session, _ := testSession(t, func(request map[string]any) []any {
				if request["type"] == "negotiate_protocol" && scenario == "unknown-negotiation" {
					response := responseFor(request)
					response["data"] = map[string]any{"protocolVersion": 3}
					return []any{response}
				}
				if request["type"] != "prompt" {
					return nil
				}
				values := terminalFrames(request, text, "completed")
				switch scenario {
				case "wrong-response-id":
					values[0].(map[string]any)["id"] = "unrelated"
				case "duplicate-response":
					values = append(values[:1], append([]any{responseFor(request)}, values[1:]...)...)
				case "settled-before-result":
					values[4], values[5] = values[5], values[4]
				case "missing-final":
					values = append(values[:2], values[3:]...)
				case "fenced-json":
					values = terminalFrames(request, "```json\n"+text+"\n```", "completed")
				case "embedded-json":
					values = terminalFrames(request, "Here is the result: "+text, "completed")
				case "duplicate-prompt-result":
					values = append(values[:5], append([]any{values[4]}, values[5:]...)...)
				case "schema-invalid-summary":
					values = terminalFrames(request, strings.Replace(text, `"Observed repository progress"`, `"\u0000"`, 1), "completed")
				}
				return values
			}, nil, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := session.Open(ctx); err == nil {
				_ = session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)})
				if err := session.WaitTurn(ctx); err == nil {
					t.Fatal("malformed native lifecycle succeeded")
				}
			}
			_ = session.Close(ctx)
			result, _ := session.Wait(ctx)
			if result.Kind == harness.ResultSucceeded || result.Semantic != nil {
				t.Fatalf("malformed result escaped: %+v", result)
			}
		})
	}
}

func assertFinalAssistantMessageRejected(t *testing.T, message json.RawMessage) {
	t.Helper()
	var eventsMutex sync.Mutex
	taskResults := 0
	text := validTaskText(t)
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] != "prompt" {
			return nil
		}
		values := terminalFrames(request, text, "completed")
		values[2].(map[string]any)["message"] = message
		return values
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		eventsMutex.Lock()
		defer eventsMutex.Unlock()
		if event.Kind == harness.EventTaskResult {
			taskResults++
		}
		return nil
	}), nil)
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	_ = session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)})
	if err := session.WaitTurn(ctx); err == nil {
		t.Error("conflicting final assistant message completed successfully")
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err == nil || result.Kind != harness.ResultFailed || result.Semantic != nil || result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult || result.Usage.State != harness.UsageUnknown {
		t.Errorf("Wait=%+v, %v; want failed/missing_result without semantic output", result, err)
	}
	eventsMutex.Lock()
	defer eventsMutex.Unlock()
	if taskResults != 0 {
		t.Errorf("conflicting final assistant message emitted %d TaskResult events", taskResults)
	}
}

func TestFinalAssistantStopReasonAliasCannotBecomeSuccess(t *testing.T) {
	text, err := json.Marshal(validTaskText(t))
	if err != nil {
		t.Fatal(err)
	}
	// RawMessage preserves the review's ordered conflicting keys on the wire.
	message := json.RawMessage(`{"role":"assistant","stopReason":"error","stopreason":"stop","content":[{"type":"text","text":` + string(text) + `}]}`)
	assertFinalAssistantMessageRejected(t, message)
}

func TestMessageEndRoleAliasOnlyIsFailed(t *testing.T) {
	text, err := json.Marshal(validTaskText(t))
	if err != nil {
		t.Fatal(err)
	}
	message := json.RawMessage(`{"ROLE":"assistant","stopReason":"stop","content":[{"type":"text","text":` + string(text) + `}]}`)
	assertFinalAssistantMessageRejected(t, message)
}

func TestMessageEndReverseRoleAliasCannotReusePriorAssistantResult(t *testing.T) {
	text := validTaskText(t)
	encodedText, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	message := json.RawMessage(`{"role":"user","ROLE":"assistant","stopReason":"stop","content":[{"type":"text","text":` + string(encodedText) + `}]}`)
	var eventsMutex sync.Mutex
	taskResults := 0
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] != "prompt" {
			return nil
		}
		values := terminalFrames(request, text, "completed")
		conflicting := map[string]any{"type": "message_end", "message": message}
		return append(values[:3], append([]any{conflicting}, values[3:]...)...)
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		eventsMutex.Lock()
		defer eventsMutex.Unlock()
		if event.Kind == harness.EventTaskResult {
			taskResults++
		}
		return nil
	}), nil)
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	_ = session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)})
	if err := session.WaitTurn(ctx); !errors.Is(err, errProtocol) {
		t.Errorf("WaitTurn=%v; want the role-alias protocol failure", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if !errors.Is(err, errProtocol) || result.Kind != harness.ResultFailed || result.Semantic != nil || result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult || result.Usage.State != harness.UsageUnknown {
		t.Errorf("Wait=%+v, %v; want failed/missing_result without the prior assistant result", result, err)
	}
	eventsMutex.Lock()
	defer eventsMutex.Unlock()
	if taskResults != 0 {
		t.Errorf("role-alias routing conflict emitted %d TaskResult events", taskResults)
	}
}

func TestFinalAssistantMessageRejectsFieldCaseAliases(t *testing.T) {
	text, err := json.Marshal(validTaskText(t))
	if err != nil {
		t.Fatal(err)
	}
	content := `"content":[{"type":"text","text":` + string(text) + `}]`
	for _, test := range []struct {
		name    string
		message string
	}{
		{"role-alias", `{"role":"assistant","ROLE":"user","stopReason":"stop",` + content + `}`},
		{"stop-reason-alias-after-valid", `{"role":"assistant","stopReason":"stop","stopreason":"error",` + content + `}`},
		{"stop-reason-alias-before-valid", `{"role":"assistant","stopreason":"error","stopReason":"stop",` + content + `}`},
		{"stop-reason-wrong-case-only", `{"role":"assistant","StopReason":"stop",` + content + `}`},
		{"content-alias", `{"role":"assistant","stopReason":"stop",` + content + `,"CONTENT":[]}`},
		{"content-wrong-case-only", `{"role":"assistant","stopReason":"stop","Content":[{"type":"text","text":` + string(text) + `}]}`},
		{"block-type-alias", `{"role":"assistant","stopReason":"stop","content":[{"type":"text","TYPE":"image","text":` + string(text) + `}]}`},
		{"block-type-wrong-case-only", `{"role":"assistant","stopReason":"stop","content":[{"Type":"text","text":` + string(text) + `}]}`},
		{"block-text-alias", `{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":` + string(text) + `,"TEXT":"not a TaskResult"}]}`},
		{"block-text-wrong-case-only", `{"role":"assistant","stopReason":"stop","content":[{"type":"text","Text":` + string(text) + `}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFinalAssistantMessageRejected(t, json.RawMessage(test.message))
		})
	}
}

func TestFinalAssistantMessageAllowsExtraMetadata(t *testing.T) {
	text := validTaskText(t)
	var eventsMutex sync.Mutex
	taskResults := 0
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] != "prompt" {
			return nil
		}
		values := terminalFrames(request, text, "completed")
		message := values[2].(map[string]any)["message"].(map[string]any)
		message["model"] = "candidate-model"
		message["usage"] = map[string]any{"inputTokens": 1}
		message["content"].([]any)[0].(map[string]any)["signature"] = "native-metadata"
		return values
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		eventsMutex.Lock()
		defer eventsMutex.Unlock()
		if event.Kind == harness.EventTaskResult {
			taskResults++
		}
		return nil
	}), nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if err := session.WaitTurn(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil {
		t.Fatalf("Wait=%+v, %v; unrelated native metadata must remain accepted", result, err)
	}
	eventsMutex.Lock()
	defer eventsMutex.Unlock()
	if taskResults != 1 {
		t.Fatalf("canonical final assistant message emitted %d TaskResult events, want one", taskResults)
	}
}

func TestProviderErrorIsFailedWithUnknownUsage(t *testing.T) {
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] != "prompt" {
			return nil
		}
		values := terminalFrames(request, "", "error")
		values[4].(map[string]any)["error"] = map[string]any{"httpStatus": 401, "retryable": false, "message": "authentication rejected"}
		return values
	}, nil, nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitTurn(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultFailed || result.Semantic != nil || result.Reason == nil || *result.Reason != protocol.TaskResultReasonAuth || result.Usage.State != harness.UsageUnknown {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

func TestResumeRejectsReturnedIdentityMismatch(t *testing.T) {
	resume := &harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "retained-other-id"}
	session, _ := testSession(t, nil, nil, resume)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.Open(ctx); !errors.Is(err, ErrResumeRejected) {
		t.Fatalf("Open returned %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.WaitStopped(ctx); err != nil {
		t.Fatalf("retained identity rejection prevented physical stop proof: %v", err)
	}
	if _, err := session.Wait(ctx); !errors.Is(err, ErrResumeRejected) {
		t.Fatalf("physical cleanup erased retained identity rejection: %v", err)
	}
}

func TestRetainedResumeBindsExactIdleHandle(t *testing.T) {
	resume := &harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "native-session"}
	session, process := testSession(t, nil, nil, resume)
	canonical, err := filepath.EvalSymlinks(resume.NativeSessionFilename)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	handle, err := session.Open(ctx)
	if err != nil || handle.ID != resume.NativeSessionID || handle.Filename != canonical {
		t.Fatalf("Open=%+v, %v", handle, err)
	}
	select {
	case <-process.promptSeen:
		t.Fatal("resume invoked an unadmitted prompt")
	default:
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultUnknown || result.Semantic != nil {
		t.Fatalf("idle retained transport cleanup returned %+v, %v", result, err)
	}
}

func TestRetainedResumeRevalidatesBothIdentityPathsAtOpen(t *testing.T) {
	for _, scenario := range []string{"same-file", "different-file", "different-id", "missing-returned", "missing-retained", "outside-returned", "outside-retained", "directory-returned", "directory-retained"} {
		t.Run(scenario, func(t *testing.T) {
			resume := &harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "native-session"}
			session, process := testSession(t, nil, nil, resume)
			canonical, err := filepath.EvalSymlinks(resume.NativeSessionFilename)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "different-file":
				process.filename = filepath.Join(session.directory, "other-session.jsonl")
				if err := os.WriteFile(process.filename, []byte("other session"), 0600); err != nil {
					t.Fatal(err)
				}
			case "different-id":
				process.sessionID = "other-session"
			case "missing-returned":
				process.filename = filepath.Join(session.directory, "missing.jsonl")
			case "missing-retained":
				resume.NativeSessionFilename = filepath.Join(session.directory, "missing.jsonl")
			case "outside-returned", "outside-retained":
				outside := filepath.Join(t.TempDir(), "outside.jsonl")
				if err := os.WriteFile(outside, []byte("outside session"), 0600); err != nil {
					t.Fatal(err)
				}
				if scenario == "outside-returned" {
					process.filename = outside
				} else {
					resume.NativeSessionFilename = outside
				}
			case "directory-returned":
				process.filename = session.directory
			case "directory-retained":
				resume.NativeSessionFilename = session.directory
			}
			ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
			defer cancel()
			handle, err := session.Open(ctx)
			if scenario == "same-file" {
				if err != nil || handle.ID != resume.NativeSessionID || handle.Filename != canonical {
					t.Fatalf("Open=%+v, %v; want the canonical retained identity", handle, err)
				}
			} else if !errors.Is(err, ErrResumeRejected) || handle.ID != "" || handle.Filename != "" {
				t.Fatalf("Open=%+v, %v; want retained identity rejection without a handle", handle, err)
			}
			select {
			case <-process.promptSeen:
				t.Fatal("identity validation submitted an unadmitted prompt")
			default:
			}
		})
	}
}

func TestLaunchPersistenceFailureNeverExposesOutputOrPrompt(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("process marker could not be persisted")
	adapter := NewAdapter("candidate-omp")
	adapter.probe = func(context.Context) (harness.Capabilities, error) {
		return observedCapabilities(), harness.ErrNativeUnverified
	}
	adapter.start = func(_ context.Context, invocation execution.Invocation, _ execution.Sink) (harness.NativeProcess, error) {
		return nil, invocation.PersistProcess(4123, "owned-process")
	}
	value, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: directory, Invocation: execution.Invocation{Args: []string{"--session-dir", directory}}, PersistProcess: func(int, string) error { return sentinel }}, harness.EventSinkFunc(func(context.Context, harness.Event) error {
		t.Fatal("output exposed after persistence failure")
		return nil
	}))
	if value != nil || !errors.Is(err, sentinel) {
		t.Fatalf("Start=%v, %v", value, err)
	}
}

func TestSessionStartSinkFailureNeverSubmitsPrompt(t *testing.T) {
	sentinel := errors.New("session-start journal storage unavailable")
	session, process := testSession(t, nil, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		if event.Kind == harness.EventSessionStarted {
			return sentinel
		}
		return nil
	}), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)}); !errors.Is(err, sentinel) {
		t.Fatalf("StartTurn=%v", err)
	}
	select {
	case <-process.promptSeen:
		t.Fatal("prompt escaped after session-start persistence failed")
	default:
	}
	_ = session.Close(ctx)
	result, err := session.Wait(ctx)
	if !errors.Is(err, sentinel) || result.Kind == harness.ResultSucceeded || result.Semantic != nil {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

func TestSinkFailureInvalidatesSuccessfulNativeResult(t *testing.T) {
	text := validTaskText(t)
	sentinel := errors.New("journal storage unavailable")
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			return terminalFrames(request, text, "completed")
		}
		return nil
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		if event.Kind == harness.EventTaskResult {
			return sentinel
		}
		return nil
	}), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)}); err != nil && !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := session.WaitTurn(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("WaitTurn=%v", err)
	}
	_ = session.Close(ctx)
	result, err := session.Wait(ctx)
	if !errors.Is(err, sentinel) || result.Kind == harness.ResultSucceeded || result.Semantic != nil {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

func TestCancelWaitsForNativeSettlementAndPhysicalStop(t *testing.T) {
	var promptID any
	session, process := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			promptID = request["id"]
			return []any{responseFor(request), map[string]any{"type": "agent_start"}}
		}
		if request["type"] == "abort_and_restore_queue" {
			response := responseFor(request)
			response["data"] = map[string]any{"steering": []any{}, "followUp": []any{}}
			return []any{response, map[string]any{"type": "agent_end", "messages": []any{}}, map[string]any{"type": "prompt_result", "id": promptID, "status": "aborted", "agentInvoked": true, "sessionSettled": true}, map[string]any{"type": "session_settled"}}
		}
		return nil
	}, nil, nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	receipt, err := session.Control(ctx, harness.ControlRequest{CommandID: "cancel-once", Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("cancel=%+v, %v", receipt, err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("cancel acknowledged before physical exit")
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultCancelled {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

func TestCancelAfterCompletedTurnDoesNotRewriteSuccess(t *testing.T) {
	text := validTaskText(t)
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			return terminalFrames(request, text, "completed")
		}
		return nil
	}, nil, nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitTurn(ctx); err != nil {
		t.Fatal(err)
	}
	receipt, err := session.Control(ctx, harness.ControlRequest{Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlRejected {
		t.Fatalf("cancel=%+v, %v", receipt, err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := session.Wait(ctx)
	if err != nil || result.Kind != harness.ResultSucceeded {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

func TestUnknownBlobCannotLaunch(t *testing.T) {
	program := filepath.Join(t.TempDir(), "not-pinned-omp.exe")
	if err := os.WriteFile(program, []byte("not a pinned executable"), 0700); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(program)
	adapter.start = func(context.Context, execution.Invocation, execution.Sink) (harness.NativeProcess, error) {
		t.Fatal("unknown blob launched")
		return nil, nil
	}
	value, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: directory, Invocation: execution.Invocation{Args: []string{"--session-dir", directory}}}, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }))
	if value != nil || !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("Start=%v, %v", value, err)
	}
}

func TestPendingPromptTimeoutIsTerminalAndNeverResent(t *testing.T) {
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			return []any{}
		}
		return nil
	}, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.Open(ctx); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	err := session.StartTurn(short, harness.TurnRequest{Goal: "Inspect", Context: json.RawMessage(`{}`)})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StartTurn=%v", err)
	}
	if err := session.StartTurn(ctx, harness.TurnRequest{Goal: "Do not resend", Context: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("uncertain prompt was replayed")
	}
	_ = session.Close(ctx)
	result, err := session.Wait(ctx)
	if err == nil || result.Kind == harness.ResultSucceeded {
		t.Fatalf("result=%+v, %v", result, err)
	}
}

type processWithoutEOF struct{ harness.NativeProcess }

func TestCloseRetainsOwnerAfterFailedPhysicalStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process := newScriptedProcess(filepath.Join(t.TempDir(), "session.jsonl"), nil)
	process.terminateFailures = 1
	session := newSession(ctx, cancel, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }), t.TempDir(), nil)
	session.process = processWithoutEOF{process}
	close(session.launchReady)
	go process.run(ctx, execution.SinkFunc(session.output))
	go session.watchProcess()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := session.Close(bounded); err == nil {
		t.Fatal("failed physical stop acknowledged")
	}
	select {
	case <-process.done:
		t.Fatal("first failed stop lost physical owner")
	default:
	}
	if err := session.Close(bounded); err != nil {
		t.Fatalf("retry physical stop: %v", err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("close returned before process joined")
	}
}

func TestCancelFallsBackToPhysicalStopAfterUncertainAbort(t *testing.T) {
	session, process := testSession(t, func(request map[string]any) []any {
		if request["type"] == "prompt" {
			return []any{responseFor(request), map[string]any{"type": "agent_start"}}
		}
		if request["type"] == "abort_and_restore_queue" {
			return []any{}
		}
		return nil
	}, nil, nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	receipt, err := session.Control(ctx, harness.ControlRequest{Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("cancel=%+v, %v", receipt, err)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("fallback acknowledged before physical exit")
	}
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	result, err := session.Wait(bounded)
	if result.Kind != harness.ResultCancelled || err == nil {
		t.Fatalf("uncertain RPC failure must remain visible alongside physical cancellation: %+v, %v", result, err)
	}
	if _, err := session.WaitStopped(bounded); err != nil {
		t.Fatalf("sticky abort failure prevented physical stop proof: %v", err)
	}
	if _, err := session.Wait(bounded); err == nil {
		t.Fatal("physical stop proof erased the uncertain abort failure")
	}
}

func nativeActivityFixture(t *testing.T, name string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "omp", TestedVersion, name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var activity []map[string]any
	active := false
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		if value["type"] == "response" && value["command"] == "prompt" {
			active = true
		}
		if active {
			activity = append(activity, value)
		}
	}
	if len(activity) == 0 {
		t.Fatal("native fixture contains no admitted prompt activity")
	}
	return activity
}

func TestObservedWindowsAndLinuxFailedAndAbortedTurns(t *testing.T) {
	for _, platform := range []string{"windows", "linux"} {
		for _, mode := range []string{"live-protocol", "cancel"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				activity := nativeActivityFixture(t, platform+"-"+mode)
				var promptID any
				session, _ := testSession(t, func(request map[string]any) []any {
					var values []any
					if request["type"] == "prompt" {
						promptID = request["id"]
						for _, value := range activity {
							if mode == "cancel" && value["type"] == "queue_update" {
								break
							}
							if value["type"] == "prompt_result" || (value["type"] == "response" && value["command"] == "prompt") {
								value["id"] = promptID
							}
							values = append(values, value)
						}
						return values
					}
					if request["type"] == "abort_and_restore_queue" {
						remaining := false
						for _, value := range activity {
							if value["type"] == "queue_update" {
								remaining = true
							}
							if !remaining {
								continue
							}
							if value["type"] == "prompt_result" {
								value["id"] = promptID
							}
							if value["type"] == "response" && value["command"] == "abort_and_restore_queue" {
								value["id"] = request["id"]
							}
							values = append(values, value)
						}
						return values
					}
					return nil
				}, nil, nil)
				startTestTurn(t, session)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				expected := harness.ResultFailed
				if mode == "cancel" {
					expected = harness.ResultCancelled
					receipt, err := session.Control(ctx, harness.ControlRequest{Kind: harness.ControlCancel})
					if err != nil || receipt.Outcome != harness.ControlApplied {
						t.Fatalf("cancel=%+v, %v", receipt, err)
					}
				}
				if err := session.WaitTurn(ctx); err != nil {
					t.Fatal(err)
				}
				if err := session.Close(ctx); err != nil {
					t.Fatal(err)
				}
				result, err := session.Wait(ctx)
				if err != nil || result.Kind != expected || result.Semantic != nil || result.Usage.State != harness.UsageUnknown {
					t.Fatalf("result=%+v, %v", result, err)
				}
				if mode == "live-protocol" && (result.Reason == nil || *result.Reason != protocol.TaskResultReasonAuth) {
					t.Fatalf("HTTP401 lost auth classification: %+v", result)
				}
			})
		}
	}
}
