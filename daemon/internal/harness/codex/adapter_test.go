package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestAdapterStagesNativeTransportAndRequiresMatchingStructuredCompletion(t *testing.T) {
	process := newFakeNativeProcess()
	var invocation execution.Invocation
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			invocation = got
			process.sink = sink
			return process, nil
		},
	}
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, adapter, sink)
	if got, want := invocation.Program, "codex-test"; got != want {
		t.Fatalf("program = %q, want %q", got, want)
	}
	if got, want := strings.Join(invocation.Args, " "), "app-server --stdio"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if invocation.InitialInput != nil || invocation.CloseInputAfterInitial {
		t.Fatalf("invocation starts native protocol input unexpectedly: %+v", invocation)
	}
	if pid, identity := session.ProcessDetails(); pid != 42 || identity != "test:42" {
		t.Fatalf("ProcessDetails() = (%d, %q)", pid, identity)
	}

	handle := completeOpen(t, process, session)
	if handle.ID != "thread-1" || handle.Filename != "" {
		t.Fatalf("Open() handle = %+v", handle)
	}
	if sink.has(harness.EventSessionStarted) {
		t.Fatal("Open() emitted session_started before the caller can persist the native handle")
	}
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Write the bounded change.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": "remoteControl/status/changed", "params": map[string]any{"status": "idle"},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1", "delta": "working"},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventThreadTokenUsage,
		"params": map[string]any{
			"threadId": "thread-1", "turnId": "turn-1",
			"tokenUsage": map[string]any{"last": map[string]any{"cachedInputTokens": 0, "inputTokens": 4, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 8}, "total": map[string]any{"cachedInputTokens": 0, "inputTokens": 4, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 8}},
		},
	})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})

	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Summary != "made bounded progress" {
		t.Fatalf("Wait() result = %+v", result)
	}
	if result.Usage.State != harness.UsageUnknown {
		t.Fatalf("usage state = %q, want unknown until billing semantics are verified", result.Usage.State)
	}
	if !sink.has(harness.EventSessionStarted) || !sink.has(harness.EventMessageDelta) || !sink.has(harness.EventUsageObserved) || !sink.has(harness.EventTaskResult) {
		t.Fatalf("normalized events = %#v", sink.events)
	}
	usagePayload := string(sink.payload(harness.EventUsageObserved))
	if !strings.Contains(usagePayload, `"cached_input_tokens":0`) || !strings.Contains(usagePayload, `"total_tokens":8`) || strings.Contains(usagePayload, "threadId") || strings.Contains(usagePayload, "turnId") {
		t.Fatalf("usage payload = %s, want normalized counters without native identity", usagePayload)
	}
	if !sink.hasDiagnostic("unknown_native_event") {
		t.Fatalf("unrelated notification was not retained as diagnostic: %#v", sink.events)
	}
	if payload := sink.payload(harness.EventTaskResult); string(payload) != validTaskResultJSON(t, "progress") {
		t.Fatalf("task-result payload = %s", payload)
	}
}

func TestUnsupportedServerRequestReplyCarriesJSONRPCVersion(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)

	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "server-request-1",
		"method":  "item/commandExecution/requestApproval",
		"params":  map[string]any{},
	}); err != nil {
		t.Fatalf("emit unsupported server request: %v", err)
	}
	response := process.nextServerRequestResponse(t)
	if response.ID != "server-request-1" || response.Error.Code != -32601 {
		t.Fatalf("unsupported server request response = %+v, want JSON-RPC method-not-found reply", response)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartAllowsAdmissionModelProfileAlias(t *testing.T) {
	adapter := fakeAdapter(newFakeNativeProcess())
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:    filepath.Join(t.TempDir(), "workspace"),
		ModelProfile: "implementation-default",
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() error = %v, want admission alias accepted", err)
	}
}

func TestStartRejectsResumeBeforeLaunchingFreshProcess(t *testing.T) {
	called := false
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			called = true
			return newFakeNativeProcess(), nil
		},
	}
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: filepath.Join(t.TempDir(), "workspace"),
		Resume:    &harness.ResumeHandle{NativeSessionID: "native-1"},
	}, &recordingHarnessSink{})
	var capabilityErr *harness.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != harness.CapabilityResume {
		t.Fatalf("Start() error = %v, want explicit unsupported resume", err)
	}
	if called {
		t.Fatal("Start() launched process for unsupported resume")
	}
}

func TestStartRejectsStrictCostCapBeforeLaunchingNativeTransport(t *testing.T) {
	called := false
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			called = true
			return newFakeNativeProcess(), nil
		},
	}
	cap := "250000"
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: t.TempDir(),
		Limits:    harness.Limits{MaxCostMicrousd: &cap},
	}, &recordingHarnessSink{})
	var capabilityErr *harness.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != harness.CapabilityHardCostLimit {
		t.Fatalf("Start() error = %v, want hard-cost capability rejection", err)
	}
	if called {
		t.Fatal("Start() launched native transport for strict cap")
	}
}

func TestStartRejectsProviderAccessBeforeLaunchingNativeTransport(t *testing.T) {
	called := false
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			called = true
			return newFakeNativeProcess(), nil
		},
	}
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: filepath.Join(t.TempDir(), "workspace"),
		ProviderAccess: &protocol.ProviderAccess{
			Path:  "https://control.example.test/api/v1/provider-actions",
			Token: "provider-token",
		},
	}, &recordingHarnessSink{})
	var capabilityErr *harness.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != harness.CapabilityProviderAccess {
		t.Fatalf("Start() error = %v, want provider-access capability rejection", err)
	}
	if called {
		t.Fatal("Start() launched native transport for unsupported provider access")
	}
}

func TestStartQueuesEarlyProcessOutputUntilProcessIsPublished(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			raw := []byte(`{"jsonrpc":"2.0","id":"early","method":"future/request","params":{}}` + "\n")
			if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: raw}); err != nil {
				return nil, err
			}
			return process, nil
		},
	}
	if _, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	response := process.nextServerRequestResponse(t)
	if response.ID != "early" || response.Error.Code != -32601 {
		t.Fatalf("early server request response = %+v, want unsupported response", response)
	}
	_ = process.Terminate(context.Background(), 0)
}

func TestStartPersistsProcessIdentityBeforeReturningSession(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, invocation execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			if invocation.PersistProcess != nil {
				pid, identity := process.ProcessDetails()
				if err := invocation.PersistProcess(pid, identity); err != nil {
					return nil, err
				}
			}
			return process, nil
		},
	}
	var gotPID int
	var gotIdentity string
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: t.TempDir(),
		PersistProcess: func(pid int, identity string) error {
			gotPID, gotIdentity = pid, identity
			return nil
		},
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if gotPID != 42 || gotIdentity != "test:42" {
		t.Fatalf("persisted process = (%d, %q), want (42, test:42)", gotPID, gotIdentity)
	}
	_ = process.Terminate(context.Background(), 0)
}

func TestStartRetainsProcessOwnerAfterRunnerError(t *testing.T) {
	process := newFakeNativeProcess()
	want := errors.New("persist process failed after commit")
	var processSink execution.Sink
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			processSink = sink
			raw := []byte(`{"jsonrpc":"2.0","id":"early","method":"future/request","params":{}}` + "\n")
			if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: raw}); err != nil {
				t.Fatalf("queue pre-ready output: %v", err)
			}
			return process, want
		},
	}
	sink := &recordingHarnessSink{}
	started, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, sink)
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want runner failure", err)
	}
	session, ok := started.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want retained *nativeSession", started)
	}
	if pid, identity := session.ProcessDetails(); pid != 42 || identity != "test:42" {
		t.Fatalf("ProcessDetails() = (%d, %q), want returned process identity", pid, identity)
	}
	if _, err := session.Open(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Open() error = %v, want retained start failure", err)
	}
	if err := session.StartTurn(context.Background(), harness.TurnRequest{
		Goal:    "must be rejected",
		Context: json.RawMessage(`{"snapshot":"canonical"}`),
	}); !errors.Is(err, want) {
		t.Fatalf("StartTurn() error = %v, want retained start failure", err)
	}
	if err := processSink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 2, Data: []byte(`{"jsonrpc":"2.0","id":"late","method":"future/request","params":{}}` + "\n")}); !errors.Is(err, want) {
		t.Fatalf("late process output error = %v, want retained start failure", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Summary != want.Error() {
		t.Fatalf("Wait() result = %+v, want failed result retaining start error", result)
	}
	if result.Reason == nil || *result.Reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("Wait() reason = %v, want unknown_outcome", result.Reason)
	}
	process.mutex.Lock()
	terminationCalls := process.termination
	process.mutex.Unlock()
	if terminationCalls != 1 {
		t.Fatalf("Terminate calls = %d, want one session-owned cleanup", terminationCalls)
	}
	if len(process.writes) != 0 || len(sink.events) != 0 {
		t.Fatalf("failed start emitted output or acknowledgement: writes=%d events=%#v", len(process.writes), sink.events)
	}
}

func TestStartQueuesEarlyStderrBehindEarlierStdout(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, output execution.Sink) (nativeProcess, error) {
			process.sink = output
			if err := output.Handle(context.Background(), execution.Event{
				Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(),
				Data: []byte(`{"jsonrpc":"2.0","id":"early","method":"future/request","params":{}}` + "\n"),
			}); err != nil {
				return nil, err
			}
			if err := output.Handle(context.Background(), execution.Event{
				Stream: execution.Stderr, Sequence: 2, At: time.Now().UTC(), Data: []byte("warning\n"),
			}); err != nil {
				return nil, err
			}
			return process, nil
		},
	}
	if _, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, sink); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	response := process.nextServerRequestResponse(t)
	if response.ID != "early" || response.Error.Code != -32601 {
		t.Fatalf("early server request response = %+v", response)
	}
	if !sink.hasDiagnostic("native_stderr") {
		t.Fatalf("queued stderr was not emitted: %#v", sink.events)
	}
	_ = process.Terminate(context.Background(), 0)
}

func TestStartRejectsNilNativeProcessAndCancels(t *testing.T) {
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			return nil, nil
		},
	}
	if _, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{}); !errors.Is(err, errNativeProcessNil) {
		t.Fatalf("Start() error = %v, want nil-process failure", err)
	}
}

func TestStartRejectsTypedNilNativeProcess(t *testing.T) {
	var process *fakeNativeProcess
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			return process, nil
		},
	}
	if _, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{}); !errors.Is(err, errNativeProcessNil) {
		t.Fatalf("Start() error = %v, want typed-nil process failure", err)
	}
}

func TestPreReadyOutputOverflowFailsClosed(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, output execution.Sink) (nativeProcess, error) {
			process.sink = output
			for index := 0; index <= maxPreReadyEvents; index++ {
				if err := output.Handle(context.Background(), execution.Event{
					Stream: execution.Stdout, Sequence: uint64(index + 1), At: time.Now().UTC(), Data: []byte("{}\n"),
				}); err != nil && index < maxPreReadyEvents {
					return nil, err
				}
			}
			return process, nil
		},
	}
	started, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	if !errors.Is(err, errProcessOutputBeforeReady) {
		t.Fatalf("Start() error = %v, want pre-ready overflow", err)
	}
	session, ok := started.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want retained *nativeSession", started)
	}
	if _, openErr := session.Open(context.Background()); !errors.Is(openErr, errProcessOutputBeforeReady) {
		t.Fatalf("Open() error = %v, want retained pre-ready failure", openErr)
	}
	if turnErr := session.StartTurn(context.Background(), harness.TurnRequest{
		Goal:    "must be rejected",
		Context: json.RawMessage(`{"snapshot":"canonical"}`),
	}); !errors.Is(turnErr, errProcessOutputBeforeReady) {
		t.Fatalf("StartTurn() error = %v, want retained pre-ready failure", turnErr)
	}
	if closeErr := session.Close(context.Background()); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	result, waitErr := session.Wait(context.Background())
	if waitErr != nil || result.Kind != harness.ResultFailed || result.Summary != errProcessOutputBeforeReady.Error() {
		t.Fatalf("Wait() = %+v, %v, want retained pre-ready failure", result, waitErr)
	}
	process.mutex.Lock()
	terminated := process.terminated
	process.mutex.Unlock()
	if !terminated {
		t.Fatal("pre-ready overflow cleanup did not terminate the process")
	}
}

func TestStartRetainsProcessOwnerAfterPreReadyReplayFailure(t *testing.T) {
	process := newFakeNativeProcess()
	var processSink execution.Sink
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			processSink = sink
			if err := sink.Handle(context.Background(), execution.Event{
				Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte("{\n"),
			}); err != nil {
				t.Fatalf("queue malformed pre-ready output: %v", err)
			}
			return process, nil
		},
	}
	sink := &recordingHarnessSink{}
	started, startErr := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, sink)
	if startErr == nil || !strings.Contains(startErr.Error(), "message is not valid JSON") {
		t.Fatalf("Start() error = %v, want original replay framing failure", startErr)
	}
	session, ok := started.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want retained *nativeSession", started)
	}
	if _, openErr := session.Open(context.Background()); !errors.Is(openErr, startErr) {
		t.Fatalf("Open() error = %v, want retained replay failure", openErr)
	}
	if turnErr := session.StartTurn(context.Background(), harness.TurnRequest{
		Goal:    "must be rejected",
		Context: json.RawMessage(`{"snapshot":"canonical"}`),
	}); !errors.Is(turnErr, startErr) {
		t.Fatalf("StartTurn() error = %v, want retained replay failure", turnErr)
	}
	if err := processSink.Handle(context.Background(), execution.Event{
		Stream: execution.Stdout, Sequence: 2, Data: []byte(`{"jsonrpc":"2.0","id":"late","method":"future/request","params":{}}` + "\n"),
	}); !errors.Is(err, startErr) {
		t.Fatalf("late process output error = %v, want retained replay failure", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultFailed || result.Summary != startErr.Error() {
		t.Fatalf("Wait() = %+v, %v, want retained replay failure", result, err)
	}
	process.mutex.Lock()
	terminationCalls := process.termination
	process.mutex.Unlock()
	if terminationCalls != 1 {
		t.Fatalf("Terminate calls = %d, want one session-owned cleanup", terminationCalls)
	}
	if len(process.writes) != 0 || len(sink.events) != 0 {
		t.Fatalf("failed replay emitted output or acknowledgement: writes=%d events=%#v", len(process.writes), sink.events)
	}
}

func TestOpenUsesOnlyExplicitNativeModelAndChecksReturnedIdentity(t *testing.T) {
	tests := []struct {
		name               string
		configured         string
		configuredProvider string
		returned           string
		returnedProvider   string
		wantOpenErr        bool
	}{
		{name: "matching configured model", configured: "gpt-6-astra", returned: "gpt-6-astra", returnedProvider: "openai"},
		{name: "mismatched configured model", configured: "gpt-5", returned: "gpt-6-astra", returnedProvider: "openai", wantOpenErr: true},
		{name: "mismatched configured provider", configured: "gpt-6-astra", configuredProvider: "azure", returned: "gpt-6-astra", returnedProvider: "openai", wantOpenErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := newFakeNativeProcess()
			adapter := NewAdapterWithNativeIdentity("codex-test", test.configured, test.configuredProvider)
			adapter.startProcess = func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
				process.sink = sink
				return process, nil
			}
			session := startNativeSession(t, adapter, &recordingHarnessSink{})
			opened := make(chan error, 1)
			go func() {
				_, err := session.Open(context.Background())
				opened <- err
			}()
			initialize := process.nextRequest(t)
			process.reply(t, initialize.ID, map[string]any{"userAgent": "codex-test", "codexHome": "C:/redacted", "platformFamily": "windows", "platformOs": "windows"})
			_ = process.nextNotification(t)
			thread := process.nextRequest(t)
			if thread.Params.Model != test.configured {
				t.Fatalf("thread/start model = %q, want configured native model %q", thread.Params.Model, test.configured)
			}
			process.reply(t, thread.ID, map[string]any{
				"approvalPolicy":    nativeApprovalOnRequest,
				"approvalsReviewer": "user",
				"cwd":               thread.Params.CWD,
				"model":             test.returned,
				"modelProvider":     test.returnedProvider,
				"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{thread.Params.CWD}},
				"thread":            map[string]any{"id": "thread-1", "cwd": thread.Params.CWD, "ephemeral": false},
			})
			err := <-opened
			if test.wantOpenErr {
				if !errors.Is(err, harness.ErrUnsupportedCapability) {
					t.Fatalf("Open() error = %v, want configured model mismatch", err)
				}
			} else if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			_ = session.Close(context.Background())
		})
	}
}

func TestOpenRejectsSandboxDowngrade(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(context.Background())
		opened <- err
	}()
	initialize := process.nextRequest(t)
	process.reply(t, initialize.ID, map[string]any{
		"userAgent":      "codex-test",
		"codexHome":      "C:/redacted",
		"platformFamily": "windows",
		"platformOs":     "windows",
	})
	_ = process.nextNotification(t)
	thread := process.nextRequest(t)
	process.reply(t, thread.ID, map[string]any{
		"approvalPolicy":    nativeApprovalOnRequest,
		"approvalsReviewer": "user",
		"cwd":               thread.Params.CWD,
		"model":             "gpt-6-astra",
		"modelProvider":     "openai",
		"sandbox":           map[string]any{"type": "readOnly", "networkAccess": false},
		"thread":            map[string]any{"id": "thread-1", "cwd": thread.Params.CWD, "ephemeral": false},
	})
	err := <-opened
	if !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("Open() error = %v, want sandbox-downgrade unsupported error", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() after rejected Open() = %v", err)
	}
}

func TestThreadStartWorkspacePolicyCannotEscapeAdmission(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	ephemeral := false
	base := threadStartResponse{
		ApprovalPolicy:    json.RawMessage(`"on-request"`),
		ApprovalsReviewer: json.RawMessage(`"user"`),
		CWD:               workspace,
		Model:             "gpt-6-astra",
		ModelProvider:     "openai",
		Sandbox: nativeSandboxPolicy{
			Type:          "workspaceWrite",
			NetworkAccess: json.RawMessage(`false`),
			WritableRoots: []string{workspace},
		},
		Thread:                nativeThread{ID: "thread-1", CWD: workspace, Ephemeral: &ephemeral},
		RuntimeWorkspaceRoots: []string{workspace},
	}
	tests := []struct {
		name   string
		mutate func(*threadStartResponse)
	}{
		{name: "valid workspace root", mutate: func(_ *threadStartResponse) {}},
		{name: "root outside workspace", mutate: func(response *threadStartResponse) { response.Sandbox.WritableRoots = []string{outside} }},
		{name: "network enabled", mutate: func(response *threadStartResponse) { response.Sandbox.NetworkAccess = json.RawMessage(`true`) }},
		{name: "network wrong type", mutate: func(response *threadStartResponse) { response.Sandbox.NetworkAccess = json.RawMessage(`"true"`) }},
		{name: "network missing", mutate: func(response *threadStartResponse) { response.Sandbox.NetworkAccess = nil }},
		{name: "missing writable roots", mutate: func(response *threadStartResponse) { response.Sandbox.WritableRoots = nil }},
		{name: "runtime root outside workspace", mutate: func(response *threadStartResponse) { response.RuntimeWorkspaceRoots = []string{outside} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := base
			response.Sandbox.WritableRoots = append([]string(nil), base.Sandbox.WritableRoots...)
			test.mutate(&response)
			err := response.validate(workspace)
			if (test.name == "valid workspace root") != (err == nil) {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
}

func TestControlCancelAwaitsTerminalInterruptedTurn(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakeAdapter(process)
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Stop safely.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	receiptResult := make(chan harness.ControlReceipt, 1)
	errResult := make(chan error, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-1", Kind: harness.ControlCancel})
		receiptResult <- receipt
		errResult <- err
	}()
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt || interrupt.Params.ThreadID != "thread-1" || interrupt.Params.TurnID != "turn-1" {
		t.Fatalf("interrupt request = %+v", interrupt)
	}
	process.reply(t, interrupt.ID, map[string]any{})
	if err := <-errResult; err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt := <-receiptResult; receipt.Outcome != harness.ControlApplied || !strings.Contains(receipt.Message, "awaiting") {
		t.Fatalf("cancel receipt = %+v", receipt)
	}

	// Interrupt acceptance is not terminal. Only the matching completed event
	// proves that the native turn entered its interrupted terminal state.
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted locally cancelled result = %+v, want cancelled", result)
	}
}

func TestControlCancelMarksCancellationBeforeInterruptResponse(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Cancel before response.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	receiptResult := make(chan harness.ControlReceipt, 1)
	errResult := make(chan error, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-race", Kind: harness.ControlCancel})
		receiptResult <- receipt
		errResult <- err
	}()
	interrupt := process.nextRequest(t)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	native := session.(*nativeSession)
	native.mutex.Lock()
	deferred := native.deferredTerminal != nil
	completed := native.result != nil
	native.mutex.Unlock()
	if !deferred || completed {
		t.Fatalf("terminal state before interrupt response = deferred:%t completed:%t, want deferred and incomplete", deferred, completed)
	}
	process.reply(t, interrupt.ID, map[string]any{})
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted result = %+v, want cancelled after interrupt response", result)
	}
	if err := <-errResult; err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt := <-receiptResult; receipt.Outcome != harness.ControlApplied {
		t.Fatalf("cancel receipt = %+v, want applied", receipt)
	}
}

func TestControlCancelDefersInterruptedTerminalUntilRejection(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Defer terminal until rejection.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	type controlResult struct {
		receipt harness.ControlReceipt
		err     error
	}
	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "deferred-rejection", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	if err := process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress"))); err != nil {
		t.Fatalf("emit interrupted terminal: %v", err)
	}
	native := session.(*nativeSession)
	native.mutex.Lock()
	deferred := native.deferredTerminal != nil
	completed := native.result != nil
	native.mutex.Unlock()
	if !deferred || completed {
		t.Fatalf("terminal state after interrupt-before-rejection = deferred:%t completed:%t, want deferred and incomplete", deferred, completed)
	}
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"code": -32001, "message": "turn is no longer interruptible"},
	}); err != nil {
		t.Fatalf("emit late interrupt rejection: %v", err)
	}
	control := <-controlDone
	if control.err == nil || control.receipt.Outcome == harness.ControlApplied {
		t.Fatalf("rejected cancel result = %+v, want failed non-applied control", control)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("terminal result after late rejection = %+v, want failed", result)
	}
}

func TestRejectedCancelDoesNotClassifyIndependentInterruptedTurnAsCancelled(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject cancellation explicitly.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	type controlResult struct {
		receipt harness.ControlReceipt
		err     error
	}
	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "rejected-cancel", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"code": -32001, "message": "turn is no longer interruptible"},
	}); err != nil {
		t.Fatalf("emit interrupt rejection: %v", err)
	}
	control := <-controlDone
	if control.err == nil || control.receipt.Outcome == harness.ControlApplied {
		t.Fatalf("rejected cancel result = %+v, want failed non-applied control", control)
	}

	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("independently interrupted result after rejection = %+v, want failed", result)
	}
}

func TestInterruptRejectionBeforeSameChunkInterruptedTerminalWinsClassification(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Order rejection before terminal.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	type controlResult struct {
		receipt harness.ControlReceipt
		err     error
	}
	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "same-chunk-rejection", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	errorRaw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"code": -32001, "message": "turn is no longer interruptible"},
	})
	if err != nil {
		t.Fatalf("encode interrupt rejection: %v", err)
	}
	terminalRaw, err := json.Marshal(completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err != nil {
		t.Fatalf("encode terminal notification: %v", err)
	}
	chunk := append(append(errorRaw, '\n'), append(terminalRaw, '\n')...)
	if err := process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: chunk}); err != nil {
		t.Fatalf("emit same-chunk rejection and terminal: %v", err)
	}
	control := <-controlDone
	if control.err == nil || control.receipt.Outcome == harness.ControlApplied {
		t.Fatalf("same-chunk rejected cancel result = %+v, want failed non-applied control", control)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("same-chunk interrupted result = %+v, want failed", result)
	}
}

func TestDeferredInterruptedTerminalUsesOlderValidCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Retain older cancellation evidence.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	native := session.(*nativeSession)
	olderAttempt := native.beginCancelAttempt()
	native.markCancelAttemptWritten(olderAttempt)
	native.finishCancelAttempt(olderAttempt, context.DeadlineExceeded) // written, but native response is unknown
	newerAttempt := native.beginCancelAttempt()
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	native.failCancelAttempt(newerAttempt) // the newer write failed; older evidence remains effective
	native.finishCancelAttempt(newerAttempt, context.Canceled)
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("deferred terminal result = %+v, want cancelled from older attempt", result)
	}
}

func TestConcurrentControlCancelSharesOneInterruptAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Share cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	type controlResult struct {
		receipt harness.ControlReceipt
		err     error
	}
	results := make(chan controlResult, 2)
	for _, commandID := range []string{"cancel-a", "cancel-b"} {
		go func(commandID string) {
			receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: commandID, Kind: harness.ControlCancel})
			results <- controlResult{receipt: receipt, err: err}
		}(commandID)
	}
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt {
		t.Fatalf("interrupt request = %+v", interrupt)
	}
	select {
	case raw := <-process.writes:
		t.Fatalf("second interrupt request = %s", raw)
	case <-time.After(50 * time.Millisecond):
	}
	process.reply(t, interrupt.ID, map[string]any{})
	for range 2 {
		result := <-results
		if result.err != nil || result.receipt.Outcome != harness.ControlApplied {
			t.Fatalf("shared cancel result = %+v", result)
		}
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	if result, err := session.Wait(context.Background()); err != nil || result.Kind != harness.ResultCancelled {
		t.Fatalf("cancelled turn = %+v, err=%v", result, err)
	}
	_ = session.Close(context.Background())
}

func TestInterruptedTerminalWaitsForBlockedCancelWrite(t *testing.T) {
	process := newBlockingFakeNativeProcess()
	process.blockAfter = 5 // initialize, initialized, thread/start, turn/start, then interrupt
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process.fakeNativeProcess, session)
	completeTurnStart(t, process.fakeNativeProcess, session, harness.TurnRequest{Goal: "Classify blocked cancel.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "blocked-cancel", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	select {
	case <-process.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("cancel write did not block")
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its write failed")
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("blocked cancel terminal result = %+v, want failed", result)
	}
	_ = session.Close(context.Background())
}

func TestTerminalNonInterruptedTurnDoesNotSynthesizeAppliedCancel(t *testing.T) {
	for _, status := range []string{"completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			process := newFakeNativeProcess()
			session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
			_ = completeOpen(t, process, session)
			completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject stale cancel.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
			errResult := make(chan error, 1)
			receiptResult := make(chan harness.ControlReceipt, 1)
			go func() {
				receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-stale", Kind: harness.ControlCancel})
				receiptResult <- receipt
				errResult <- err
			}()
			_ = process.nextRequest(t)
			process.emitJSON(t, completedNotification("thread-1", "turn-1", status, validTaskResultJSON(t, "progress")))
			if err := <-errResult; err == nil {
				t.Fatal("Control(cancel) succeeded for non-interrupted terminal turn")
			}
			if receipt := <-receiptResult; receipt.Outcome == harness.ControlApplied {
				t.Fatalf("cancel receipt = %+v, want failed", receipt)
			}
		})
	}
}

func TestInterruptedTerminalUsesIssuedCancelIntentBeforeWriteHook(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Classify interrupt race.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	native := session.(*nativeSession)
	attemptID := native.beginCancelAttempt()
	native.markCancelAttemptWritten(attemptID)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted result = %+v, want cancellation from issued intent", result)
	}
}

func TestInterruptedTurnWithoutLocalCancelIsNotCancellation(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakeAdapter(process)
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Observe interruption.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("externally interrupted result = %+v, want failed", result)
	}
}

func TestSinkFailureCannotProduceNativeSuccess(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{failKind: harness.EventMessageDelta, failure: errors.New("journal unavailable")}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Persist events.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1", "delta": "cannot persist"},
	})
	if err == nil || !strings.Contains(err.Error(), "journal unavailable") {
		t.Fatalf("sink failure = %v", err)
	}
	result, waitErr := session.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	if result.Kind != harness.ResultFailed || !strings.Contains(result.Summary, "journal unavailable") {
		t.Fatalf("sink failure result = %+v", result)
	}
}

func TestAdapterRejectsLegacyInputBeforeLaunchingNativeTransport(t *testing.T) {
	adapter := fakeAdapter(newFakeNativeProcess())
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:  t.TempDir(),
		Invocation: execution.Invocation{InitialInput: []byte(`{"legacy":true}`)},
	}, &recordingHarnessSink{})
	if err == nil || !strings.Contains(err.Error(), "legacy initial input") {
		t.Fatalf("Start() error = %v, want legacy input rejection", err)
	}
}

func TestMismatchedTerminalAndProcessExitBecomeFailure(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject replay.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("other-thread", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if !sink.hasDiagnostic("unrelated_native_event") {
		t.Fatalf("mismatched terminal was not diagnostic: %#v", sink.events)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || !strings.Contains(result.Summary, "missing_result") {
		t.Fatalf("process exit result = %+v", result)
	}
}

func TestProcessExitWithoutTerminalResultPreservesMissingResultReason(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Classify EOF.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	process.finish(execution.Result{PID: 42, ExitCode: 0})
	waitTurnErr := session.WaitTurn(context.Background())
	if waitTurnErr == nil || !strings.Contains(waitTurnErr.Error(), "missing_result") {
		t.Fatalf("WaitTurn() error = %v, want typed missing_result classification", waitTurnErr)
	}
	result, waitErr := session.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	if result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult {
		t.Fatalf("process EOF result = %+v, want missing_result reason", result)
	}
}

func TestThreadStartedBeforeStartResponseStaysBehindJournalBarrier(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	handleResult := make(chan harness.NativeSessionHandle, 1)
	errResult := make(chan error, 1)
	go func() {
		handle, err := session.Open(context.Background())
		handleResult <- handle
		errResult <- err
	}()
	initialize := process.nextRequest(t)
	process.reply(t, initialize.ID, map[string]any{"userAgent": "codex-test", "codexHome": "C:/redacted", "platformFamily": "windows", "platformOs": "windows"})
	_ = process.nextNotification(t)
	thread := process.nextRequest(t)
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventThreadStarted,
		"params": map[string]any{"thread": map[string]any{"id": "thread-1"}},
	}); err != nil {
		t.Fatalf("emit early thread/started: %v", err)
	}
	if sink.has(harness.EventSessionStarted) || sink.hasDiagnostic("unrelated_native_event") {
		t.Fatalf("early thread metadata escaped its durable barrier: %#v", sink.events)
	}
	process.reply(t, thread.ID, map[string]any{
		"approvalPolicy":    nativeApprovalOnRequest,
		"approvalsReviewer": "user",
		"cwd":               thread.Params.CWD,
		"model":             "gpt-6-astra",
		"modelProvider":     "openai",
		"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{thread.Params.CWD}},
		"thread":            map[string]any{"id": "thread-1", "cwd": thread.Params.CWD, "ephemeral": false},
	})
	if err := <-errResult; err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if handle := <-handleResult; handle.ID != "thread-1" {
		t.Fatalf("Open() handle = %+v", handle)
	}
	if sink.has(harness.EventSessionStarted) {
		t.Fatal("Open() emitted session_started before StartTurn")
	}
}

func TestMatchingNativeErrorProvidesTypedFailureReason(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Observe error.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventError,
		"params": map[string]any{
			"threadId": "thread-1", "turnId": "turn-1", "willRetry": false,
			"error": map[string]any{"message": "window exhausted", "codexErrorInfo": "contextWindowExceeded"},
		},
	})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "failed", `{}`))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Reason == nil || *result.Reason != "context_overflow" {
		t.Fatalf("native error reason = %#v, want context_overflow", result.Reason)
	}
}

func TestTurnErrorDecodesCodexErrorInfo(t *testing.T) {
	var completed turnCompletedParams
	if err := json.Unmarshal([]byte(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed","items":[],"error":{"message":"window exhausted","codexErrorInfo":"contextWindowExceeded"}}}`), &completed); err != nil {
		t.Fatalf("decode TurnError: %v", err)
	}
	if completed.Turn.Error == nil || string(completed.Turn.Error.CodexErrorInfo) != `"contextWindowExceeded"` {
		t.Fatalf("TurnError = %+v, want codexErrorInfo contextWindowExceeded", completed.Turn.Error)
	}
	reason := classifyNativeError(completed.Turn.Error.CodexErrorInfo)
	if reason == nil || *reason != protocol.TaskResultReasonContextOverflow {
		t.Fatalf("TurnError reason = %v, want context_overflow", reason)
	}
}

func TestNormalizeTokenUsageAcceptsOptionalCodexCounters(t *testing.T) {
	raw := json.RawMessage(`{"last":{"cacheWriteInputTokens":3,"cachedInputTokens":1,"inputTokens":4,"outputTokens":5,"reasoningOutputTokens":2,"totalTokens":12},"modelContextWindow":131072,"total":{"cacheWriteInputTokens":7,"cachedInputTokens":8,"inputTokens":9,"outputTokens":10,"reasoningOutputTokens":11,"totalTokens":38}}`)
	normalized, err := normalizeTokenUsage(raw)
	if err != nil {
		t.Fatalf("normalizeTokenUsage() error = %v", err)
	}
	var got normalizedTokenUsage
	if err := json.Unmarshal(normalized, &got); err != nil {
		t.Fatalf("decode normalized usage: %v", err)
	}
	if got.ModelContextWindow == nil || *got.ModelContextWindow != 131072 {
		t.Fatalf("model context window = %v, want 131072", got.ModelContextWindow)
	}
	if got.Last.CacheWriteInputTokens == nil || *got.Last.CacheWriteInputTokens != 3 {
		t.Fatalf("last cache write tokens = %v, want 3", got.Last.CacheWriteInputTokens)
	}
	if got.Total.CacheWriteInputTokens == nil || *got.Total.CacheWriteInputTokens != 7 {
		t.Fatalf("total cache write tokens = %v, want 7", got.Total.CacheWriteInputTokens)
	}
}

func TestTerminalSuccessIsOverriddenByNonzeroProcessExit(t *testing.T) {
	process := newFakeNativeProcess()
	native := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	native.process = process
	native.processReady = true
	native.opened = true
	native.turnStarted = true
	native.threadID = "thread-1"
	native.turnID = "turn-1"

	raw, err := json.Marshal(completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err != nil {
		t.Fatalf("encode terminal notification: %v", err)
	}
	var message rpcEnvelope
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("decode terminal notification: %v", err)
	}
	if err := native.handleTurnCompleted(Frame{Kind: FrameNotification, Sequence: 1, Raw: raw, Method: appServerEventTurnCompleted}, message.Params, ""); err != nil {
		t.Fatalf("handleTurnCompleted() error = %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 23, Terminated: true, WaitError: errors.New("exit status 23")})
	native.watchProcess()
	result, err := native.currentResult()
	if err != nil {
		t.Fatalf("currentResult() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != protocol.TaskResultReasonProcessFailure {
		t.Fatalf("result after nonzero process exit = %+v, want process_failure", result)
	}
	if result.Process.ExitCode != 23 || result.Process.WaitError == nil {
		t.Fatalf("process result = %+v, want exit code 23 and wait error", result.Process)
	}
}

func TestWaitReturnsStableProcessFinalResult(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		waitErr    error
		wantKind   harness.ResultKind
		wantReason *protocol.TaskResultReason
	}{
		{name: "clean exit", exitCode: 0, wantKind: harness.ResultSucceeded},
		{name: "nonzero exit", exitCode: 23, waitErr: errors.New("exit status 23"), wantKind: harness.ResultFailed, wantReason: reasonPointer(protocol.TaskResultReasonProcessFailure)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := newFakeNativeProcess()
			session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
			_ = completeOpen(t, process, session)
			completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Freeze process result.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
			process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
			process.finish(execution.Result{PID: 42, ExitCode: test.exitCode, WaitError: test.waitErr})

			first, err := session.Wait(context.Background())
			if err != nil {
				t.Fatalf("first Wait() error = %v", err)
			}
			second, err := session.Wait(context.Background())
			if err != nil {
				t.Fatalf("second Wait() error = %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("repeated Wait() changed result: first=%+v second=%+v", first, second)
			}
			if first.Kind != test.wantKind || first.Process.ExitCode != test.exitCode {
				t.Fatalf("final result = %+v, want kind=%q exit=%d", first, test.wantKind, test.exitCode)
			}
			if test.wantReason == nil {
				if first.Reason != nil {
					t.Fatalf("final reason = %v, want nil", first.Reason)
				}
			} else if first.Reason == nil || *first.Reason != *test.wantReason {
				t.Fatalf("final reason = %v, want %q", first.Reason, *test.wantReason)
			}
		})
	}
}

func reasonPointer(reason protocol.TaskResultReason) *protocol.TaskResultReason {
	return &reason
}

func TestCurrentResultDeepCopiesSemanticPayload(t *testing.T) {
	reason := protocol.TaskResultReasonQuota
	semanticReason := protocol.TaskResultReasonRateLimit
	semantic := &protocol.TaskResult{
		EvidenceRefs: []string{"evidence-1"},
		Blocker:      &protocol.Blocker{WorkItemIDs: []string{"work-1"}},
		ProposedNextAction: &protocol.NextAction{
			Blocker: &protocol.Blocker{WorkItemIDs: []string{"next-work-1"}},
		},
		Reason:      &semanticReason,
		Diagnostics: []protocol.Diagnostic{{Code: "native", Message: "diagnostic"}},
	}
	native := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	native.complete(harness.TaskResult{Kind: harness.ResultFailed, Reason: &reason, Semantic: semantic})
	first, err := native.currentResult()
	if err != nil {
		t.Fatalf("first currentResult() error = %v", err)
	}
	first.Semantic.EvidenceRefs[0] = "mutated"
	first.Semantic.Blocker.WorkItemIDs[0] = "mutated"
	first.Semantic.ProposedNextAction.Blocker.WorkItemIDs[0] = "mutated"
	first.Semantic.Diagnostics[0].Message = "mutated"
	*first.Semantic.Reason = protocol.TaskResultReasonAuth
	*first.Reason = protocol.TaskResultReasonNetwork

	second, err := native.currentResult()
	if err != nil {
		t.Fatalf("second currentResult() error = %v", err)
	}
	if second.Semantic.EvidenceRefs[0] != "evidence-1" || second.Semantic.Blocker.WorkItemIDs[0] != "work-1" || second.Semantic.ProposedNextAction.Blocker.WorkItemIDs[0] != "next-work-1" || second.Semantic.Diagnostics[0].Message != "diagnostic" {
		t.Fatalf("semantic result was mutated through caller copy: %+v", second.Semantic)
	}
	if *second.Semantic.Reason != protocol.TaskResultReasonRateLimit || *second.Reason != protocol.TaskResultReasonQuota {
		t.Fatalf("reason pointers were not isolated: semantic=%q outer=%q", *second.Semantic.Reason, *second.Reason)
	}
}

func TestDuplicateTerminalCompletionCannotReplayTaskResult(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Accept once.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	terminal := completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress"))
	process.emitJSON(t, terminal)
	process.emitJSON(t, terminal)
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultSucceeded {
		t.Fatalf("Wait() = (%+v, %v)", result, err)
	}
	if got := sink.count(harness.EventTaskResult); got != 1 {
		t.Fatalf("task_result events = %d, want 1", got)
	}
	if !sink.hasDiagnostic("duplicate_terminal_event") {
		t.Fatalf("duplicate terminal was not retained as diagnostic: %#v", sink.events)
	}
}

func TestDuplicateItemAndUsageSnapshotsDoNotRepeatSemanticEvents(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Deduplicate events.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	item := map[string]any{
		"jsonrpc": "2.0", "method": appServerEventItemStarted,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "startedAtMs": 1, "item": map[string]any{"id": "tool-1", "type": "commandExecution"}},
	}
	usage := map[string]any{
		"jsonrpc": "2.0", "method": appServerEventThreadTokenUsage,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"last": map[string]any{"cachedInputTokens": 0, "inputTokens": 1, "outputTokens": 1, "reasoningOutputTokens": 0, "totalTokens": 2}, "total": map[string]any{"cachedInputTokens": 0, "inputTokens": 1, "outputTokens": 1, "reasoningOutputTokens": 0, "totalTokens": 2}}},
	}
	process.emitJSON(t, item)
	process.emitJSON(t, item)
	process.emitJSON(t, usage)
	process.emitJSON(t, usage)
	if got := sink.count(harness.EventToolStarted); got != 1 {
		t.Fatalf("tool_started events = %d, want 1", got)
	}
	if got := sink.count(harness.EventUsageObserved); got != 1 {
		t.Fatalf("usage_observed events = %d, want 1", got)
	}
}

func TestNotificationDedupeWindowBoundsKeyBytesAfterDuplicateCheck(t *testing.T) {
	session := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	key := strings.Repeat("k", maxSeenNotificationBytes)
	seen, err := session.markNotification(key)
	if err != nil || !seen {
		t.Fatalf("first max-sized notification key = (%v, %v), want accepted", seen, err)
	}
	seen, err = session.markNotification(key)
	if err != nil || seen {
		t.Fatalf("duplicate max-sized notification key = (%v, %v), want duplicate without overflow", seen, err)
	}
	if _, err := session.markNotification("new"); !errors.Is(err, errNotificationOverflow) {
		t.Fatalf("new notification after byte budget = %v, want overflow", err)
	}
}

func TestRetiredRPCIDsEvictOldestAndKeepLateResponsesClassifiable(t *testing.T) {
	session := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	for id := uint64(1); id <= maxRetiredRPCIDs+1; id++ {
		session.mutex.Lock()
		session.retireRPCIDLocked(id)
		session.mutex.Unlock()
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if len(session.retiredRPCIDs) != maxRetiredRPCIDs {
		t.Fatalf("retired RPC IDs = %d, want bound %d", len(session.retiredRPCIDs), maxRetiredRPCIDs)
	}
	if _, retained := session.retiredRPCIDs[1]; retained {
		t.Fatal("oldest retired RPC ID was not evicted")
	}
	if _, retained := session.retiredRPCIDs[maxRetiredRPCIDs+1]; !retained {
		t.Fatal("newest retired RPC ID was evicted")
	}
}

func TestConcurrentStartTurnSendsOnlyOneNativeRequest(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	request := harness.TurnRequest{Goal: "Serialize turn starts.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}
	first := make(chan error, 1)
	go func() { first <- session.StartTurn(context.Background(), request) }()
	turn := process.nextRequest(t)
	if turn.Method != appServerMethodTurnStart {
		t.Fatalf("first native request = %+v", turn)
	}
	if err := session.StartTurn(context.Background(), request); err == nil || !strings.Contains(err.Error(), "already starting") {
		t.Fatalf("concurrent StartTurn() error = %v, want active start rejection", err)
	}
	process.reply(t, turn.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}})
	if err := <-first; err != nil {
		t.Fatalf("first StartTurn() error = %v", err)
	}
	select {
	case raw := <-process.writes:
		t.Fatalf("concurrent StartTurn wrote a second native request: %s", raw)
	default:
	}
}

func TestTurnNotificationsBeforeStartResponseReplayAfterTurnIDIsKnown(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	request := harness.TurnRequest{Goal: "Accept ordered native replay.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}
	started := make(chan error, 1)
	go func() { started <- session.StartTurn(context.Background(), request) }()
	turn := process.nextRequest(t)
	if turn.Method != appServerMethodTurnStart {
		t.Fatalf("turn request = %+v", turn)
	}
	process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "method": appServerEventTurnStarted, "params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}}})
	process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta, "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1", "delta": "finalizing"}})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if sink.has(harness.EventTaskResult) {
		t.Fatal("pre-response terminal escaped before turn/start established the native turn id")
	}
	process.reply(t, turn.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}})
	if err := <-started; err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultSucceeded {
		t.Fatalf("Wait() = (%+v, %v), want semantic success", result, err)
	}
	if got := sink.count(harness.EventNativeFrame); got != 1 {
		t.Fatalf("replayed turn/started events = %d, want 1", got)
	}
	if got := sink.count(harness.EventMessageDelta); got != 1 {
		t.Fatalf("replayed deltas = %d, want 1", got)
	}
	if got := sink.count(harness.EventTaskResult); got != 1 {
		t.Fatalf("replayed terminal task results = %d, want 1", got)
	}
}

func TestDuplicatePreTurnNotificationsDeduplicateBeforeStageCapacity(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	started := make(chan error, 1)
	go func() {
		started <- session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Deduplicate before staging capacity.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	}()
	turn := process.nextRequest(t)
	turnStarted := map[string]any{
		"jsonrpc": "2.0", "method": appServerEventTurnStarted,
		"params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}},
	}
	usage := map[string]any{
		"jsonrpc": "2.0", "method": appServerEventThreadTokenUsage,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{
			"last":  map[string]any{"cachedInputTokens": 0, "inputTokens": 1, "outputTokens": 1, "reasoningOutputTokens": 0, "totalTokens": 2},
			"total": map[string]any{"cachedInputTokens": 0, "inputTokens": 1, "outputTokens": 1, "reasoningOutputTokens": 0, "totalTokens": 2},
		}},
	}
	for index := 0; index < maxPendingTurnEvents+1; index++ {
		if err := process.emitJSON(t, turnStarted); err != nil {
			t.Fatalf("duplicate turn/started %d: %v", index, err)
		}
		if err := process.emitJSON(t, usage); err != nil {
			t.Fatalf("duplicate usage %d: %v", index, err)
		}
	}
	process.reply(t, turn.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}})
	if err := <-started; err != nil {
		t.Fatalf("StartTurn() error = %v, want duplicate replay accepted", err)
	}
	if got := sink.count(harness.EventNativeFrame); got != 1 {
		t.Fatalf("turn/started events = %d, want one deduplicated event", got)
	}
	if got := sink.count(harness.EventUsageObserved); got != 1 {
		t.Fatalf("usage events = %d, want one deduplicated event", got)
	}
	_ = session.Close(context.Background())
}

func TestBufferedTerminalBeforeStartResponseSettlesBeforeProcessEOF(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	started := make(chan error, 1)
	go func() {
		started <- session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Replay terminal before EOF.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	}()
	turn := process.nextRequest(t)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Summary != "made bounded progress" {
		t.Fatalf("EOF replay result = %+v, want buffered terminal success", result)
	}
	if err := <-started; err != nil {
		t.Fatalf("StartTurn() error = %v, want buffered terminal replay to settle turn", err)
	}
	_ = turn
}

func TestTurnReplayBarrierPreservesPreResponseNotificationOrder(t *testing.T) {
	process := newFakeNativeProcess()
	sink := newBlockingOrderedSink(harness.EventMessageDelta)
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	request := harness.TurnRequest{Goal: "Preserve notification order.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}
	started := make(chan error, 1)
	go func() { started <- session.StartTurn(context.Background(), request) }()
	turn := process.nextRequest(t)
	process.emitJSON(t, deltaNotification("A"))
	process.emitJSON(t, deltaNotification("B"))
	replyDone := make(chan error, 1)
	go func() {
		replyDone <- process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}}})
	}()
	select {
	case <-sink.blocked:
	case <-time.After(time.Second):
		t.Fatal("first staged notification did not enter replay")
	}
	// The response is already accepted, but replay has not drained A. C must
	// queue behind A and B instead of bypassing the pre-response notifications.
	process.emitJSON(t, deltaNotification("C"))
	close(sink.release)
	if err := <-replyDone; err != nil {
		t.Fatalf("emit turn/start response: %v", err)
	}
	if err := <-started; err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if got := sink.deltaData(); strings.Join(got, ",") != "A,B,C" {
		t.Fatalf("replayed delta order = %#v, want A,B,C", got)
	}
}

func TestPendingTurnNotificationOverflowFailsClosed(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	started := make(chan error, 1)
	go func() {
		started <- session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Bound pending events.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	}()
	_ = process.nextRequest(t)
	for index := 0; index < maxPendingTurnEvents; index++ {
		if err := process.emitJSON(t, deltaNotification(fmt.Sprintf("%d", index))); err != nil {
			t.Fatalf("stage notification %d: %v", index, err)
		}
	}
	if err := process.emitJSON(t, deltaNotification("overflow")); !errors.Is(err, errPendingTurnEventOverflow) {
		t.Fatalf("overflow error = %v, want %v", err, errPendingTurnEventOverflow)
	}
	if err := <-started; err == nil {
		t.Fatal("StartTurn() succeeded after pending notification overflow")
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("overflow result = %+v, want unknown_outcome failure", result)
	}
	if !strings.Contains(result.Summary, errPendingTurnEventOverflow.Error()) {
		t.Fatalf("overflow summary = %q, want fatal cause", result.Summary)
	}
	if sink.hasDiagnostic("pending_turn_event_overflow") {
		t.Fatal("fatal overflow synchronously emitted a diagnostic event")
	}
	native := session.(*nativeSession)
	native.mutex.Lock()
	pending, pendingBytes := len(native.pendingTurnEvents), native.pendingTurnBytes
	native.mutex.Unlock()
	if pending != 0 || pendingBytes != 0 {
		t.Fatalf("overflow retained pending turn data: events=%d bytes=%d", pending, pendingBytes)
	}
}

func TestNotificationDedupeWindowFailsClosedAtBound(t *testing.T) {
	session := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	for index := 0; index < maxSeenNotifications; index++ {
		seen, err := session.markNotification(fmt.Sprintf("notification-%d", index))
		if err != nil || !seen {
			t.Fatalf("markNotification(%d) = (%t, %v), want admitted", index, seen, err)
		}
	}
	if _, err := session.markNotification("notification-overflow"); !errors.Is(err, errNotificationOverflow) {
		t.Fatalf("overflow markNotification error = %v, want %v", err, errNotificationOverflow)
	}
	if err := session.failNotificationOverflow(Frame{Sequence: 1}); !errors.Is(err, errNotificationOverflow) {
		t.Fatalf("failNotificationOverflow() error = %v, want %v", err, errNotificationOverflow)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("overflow result = (%+v, %v), want unknown_outcome failure", result, err)
	}
}

func TestLateNotificationsAfterTerminalAreDiagnosticOnly(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Protect terminal truth.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.emitJSON(t, deltaNotification("late"))
	if got := sink.count(harness.EventMessageDelta); got != 0 {
		t.Fatalf("late semantic delta count = %d, want 0", got)
	}
	if !sink.hasDiagnostic("late_native_event") {
		t.Fatalf("late native event was not retained as a diagnostic: %#v", sink.events)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultSucceeded {
		t.Fatalf("terminal truth changed by late event: (%+v, %v)", result, err)
	}
}

func TestFramingErrorReplacesEarlierTerminalSuccess(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject malformed trailing protocol.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.mutex.Lock()
	nativeSink := process.sink
	process.mutex.Unlock()
	if nativeSink == nil {
		t.Fatal("native process has no output sink")
	}
	framingErr := nativeSink.Handle(context.Background(), execution.Event{
		Stream:   execution.Stdout,
		Sequence: 99,
		At:       time.Now().UTC(),
		Data:     []byte("{malformed}\n"),
	})
	if framingErr == nil {
		t.Fatal("malformed native frame returned nil error")
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("Wait() = %+v, want unknown framing failure", result)
	}
	if sink.hasDiagnostic("framing_error") {
		t.Fatal("fatal framing error synchronously emitted a diagnostic event")
	}
}

func TestIncompleteTrailingFrameReplacesEarlierTerminalSuccess(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject incomplete trailing protocol.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.mutex.Lock()
	nativeSink := process.sink
	process.mutex.Unlock()
	if nativeSink == nil {
		t.Fatal("native process has no output sink")
	}
	if err := nativeSink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 99, At: time.Now().UTC(), Data: []byte(`{"jsonrpc":"2.0"`)}); err != nil {
		t.Fatalf("partial trailing frame returned error = %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("Wait() = %+v, want unknown trailing-frame failure", result)
	}
}

func TestFramingFailureSettlesWaitWithoutDiagnosticSink(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	native := session.(*nativeSession)
	failed := make(chan error, 1)
	go func() {
		failed <- native.failFraming(execution.Event{Stream: execution.Stdout, Sequence: 99, At: time.Now().UTC()}, errors.New("replay timeout"))
	}()
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("Wait() = %+v, want settled unknown_outcome failure", result)
	}
	if err := <-failed; err == nil {
		t.Fatal("failFraming() returned nil")
	}
}

func TestInvalidRPCResponseShapeReplacesEarlierTerminalSuccess(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject ambiguous response.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      99,
		"result":  map[string]any{},
		"error":   map[string]any{"code": -32000, "message": "ambiguous"},
	}); err == nil {
		t.Fatal("ambiguous response shape returned nil error")
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "unknown_outcome" {
		t.Fatalf("Wait() = %+v, want unknown framing failure", result)
	}
}

func TestNullRPCErrorCannotSatisfyOpenResponse(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(context.Background())
		opened <- err
	}()
	initialize := process.nextRequest(t)
	if err := process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": initialize.ID, "error": nil}); err == nil {
		t.Fatal("null error response returned nil framing error")
	}
	if err := <-opened; err == nil {
		t.Fatal("Open() succeeded with error:null and no result")
	}
	native := session.(*nativeSession)
	native.mutex.Lock()
	pending := len(native.pending)
	native.mutex.Unlock()
	if pending != 0 {
		t.Fatalf("pending RPC waiters = %d, want all released after null error", pending)
	}
}

func TestNullRPCErrorCannotSatisfyInterruptResponse(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject null interrupt response.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	errResult := make(chan error, 1)
	go func() {
		_, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-null", Kind: harness.ControlCancel})
		errResult <- err
	}()
	interrupt := process.nextRequest(t)
	if err := process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": interrupt.ID, "error": nil}); err == nil {
		t.Fatal("null interrupt error response returned nil framing error")
	}
	if err := <-errResult; err == nil {
		t.Fatal("Control(cancel) succeeded with error:null response")
	}
}

func TestUnmatchedRPCResponseFailsSessionAndReleasesPendingWaiters(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(context.Background())
		opened <- err
	}()
	_ = process.nextRequest(t)
	if err := process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": 999, "result": map[string]any{}}); err == nil {
		t.Fatal("unmatched response returned nil error")
	}
	if err := <-opened; err == nil {
		t.Fatal("Open() succeeded after unmatched response")
	}
	native := session.(*nativeSession)
	native.mutex.Lock()
	pending := len(native.pending)
	native.mutex.Unlock()
	if pending != 0 {
		t.Fatalf("pending RPC waiters = %d, want all released after unmatched response", pending)
	}
}

func TestTimedOutRPCResponseIDIsRetiredAndLateResponseIsDiagnostic(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(ctx)
		opened <- err
	}()
	initialize := process.nextRequest(t)
	if err := <-opened; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want deadline exceeded", err)
	}
	if err := process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": initialize.ID, "result": map[string]any{"userAgent": "late"}}); err != nil {
		t.Fatalf("late retired response error = %v", err)
	}
	if !sink.hasDiagnostic("late_response") {
		t.Fatalf("late retired response was not diagnostic: %#v", sink.events)
	}
}

func TestLateValidInterruptRejectionRetiresUnknownCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject a late cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "late-rejection", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its response timed out")
	}
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"code": -32001, "message": "turn is no longer interruptible"},
	}); err != nil {
		t.Fatalf("late valid rejection = %v", err)
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("late rejection terminal result = %+v, want failed", result)
	}
	if !sink.hasDiagnostic("late_response") {
		t.Fatalf("late rejection was not retained as diagnostic: %#v", sink.events)
	}
}

func TestMalformedLateInterruptErrorDoesNotRejectUnknownCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Preserve unknown cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "malformed-late-error", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its response timed out")
	}
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"message": "missing code"},
	}); err != nil {
		t.Fatalf("malformed late error = %v", err)
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("malformed late error terminal result = %+v, want unknown-written cancellation", result)
	}
}

func TestTerminalFinalMessageMayOmitPhaseButNotDeclareAnotherPhase(t *testing.T) {
	t.Run("omitted phase", func(t *testing.T) {
		process := newFakeNativeProcess()
		session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
		_ = completeOpen(t, process, session)
		completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Accept omitted phase.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
		process.emitJSON(t, completedNotificationWithPhase("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress"), nil))
		process.finish(execution.Result{PID: 42, ExitCode: 0})
		result, err := session.Wait(context.Background())
		if err != nil || result.Kind != harness.ResultSucceeded {
			t.Fatalf("Wait() = (%+v, %v)", result, err)
		}
	})
	t.Run("explicit non-final phase", func(t *testing.T) {
		process := newFakeNativeProcess()
		session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
		_ = completeOpen(t, process, session)
		completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject non-final phase.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
		process.emitJSON(t, completedNotificationWithPhase("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress"), "commentary"))
		process.finish(execution.Result{PID: 42, ExitCode: 0})
		result, err := session.Wait(context.Background())
		if err != nil || result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "missing_result" {
			t.Fatalf("Wait() = (%+v, %v), want missing_result failure", result, err)
		}
	})
}

func TestDuplicateMatchingNativeNotificationsDoNotRepeatIdentifiedEvents(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Deduplicate native replay.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	turnStarted := map[string]any{"jsonrpc": "2.0", "method": appServerEventTurnStarted, "params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}}}
	delta := map[string]any{"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta, "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1", "delta": "same"}}
	nativeError := map[string]any{"jsonrpc": "2.0", "method": appServerEventError, "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "willRetry": false, "error": map[string]any{"message": "limit", "codexErrorInfo": "rateLimitExceeded"}}}
	process.emitJSON(t, turnStarted)
	process.emitJSON(t, turnStarted)
	process.emitJSON(t, delta)
	process.emitJSON(t, delta)
	process.emitJSON(t, nativeError)
	process.emitJSON(t, nativeError)
	if got := sink.count(harness.EventNativeFrame); got != 1 {
		t.Fatalf("turn/started native events = %d, want 1", got)
	}
	if got := sink.count(harness.EventMessageDelta); got != 2 {
		t.Fatalf("message deltas = %d, want 2", got)
	}
	if got := sink.diagnosticCount("native_error"); got != 1 {
		t.Fatalf("native error diagnostics = %d, want 1", got)
	}
}

func TestNativeSinkEventsDoNotExposeRawProtocolIdentityOrFrames(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Sanitize native events.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventTurnStarted,
		"params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-secret", "delta": "visible"},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventThreadTokenUsage,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 1}}},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventItemStarted,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "item-secret", "type": "commandExecution", "command": "cat secret-file.txt"}},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": appServerEventError,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "error": map[string]any{"message": "secret-file.txt", "codexErrorInfo": "rateLimitExceeded"}},
	})
	process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "method": "future/native",
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-secret", "filename": "secret-file.txt"},
	})
	sink.mutex.Lock()
	events := append([]harness.Event(nil), sink.events...)
	sink.mutex.Unlock()
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal sink event: %v", err)
		}
		for _, secret := range []string{"thread-1", "turn-1", "item-secret", "secret-file.txt", `"jsonrpc"`, `"params"`} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("sink event leaked %q: %s", secret, encoded)
			}
		}
	}
	_ = session.Close(context.Background())
}

func TestBlockedRPCWriteIsCancelledAndDoesNotLeak(t *testing.T) {
	process := newBlockingFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(ctx)
		opened <- err
	}()
	select {
	case <-process.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("Open() did not enter blocked native write")
	}
	if err := <-opened; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want deadline exceeded", err)
	}
	process.mutex.Lock()
	terminations := process.termination
	process.mutex.Unlock()
	if terminations != 0 {
		t.Fatalf("blocked write termination calls = %d, want synchronous cancellation without adapter terminate", terminations)
	}
	_ = session.Close(context.Background())
}

func TestOpenInitializedNotificationUsesOpenContextDeadline(t *testing.T) {
	process := newBlockingFakeNativeProcess()
	process.blockAfter = 2
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(ctx)
		opened <- err
	}()
	initialize := process.nextRequest(t)
	process.reply(t, initialize.ID, map[string]any{"userAgent": "codex-test", "codexHome": "C:/redacted", "platformFamily": "windows", "platformOs": "windows"})
	select {
	case <-process.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("initialized notification did not enter blocked write")
	}
	if err := <-opened; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want Open deadline", err)
	}
	_ = session.Close(context.Background())
}

func TestCloseTimesOutNativeInterruptThenTerminatesProcess(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Bound close.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- session.Close(ctx) }()
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt {
		t.Fatalf("Close() native request = %+v", interrupt)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close() error = %v, want known stopped after successful termination", err)
	}
	process.mutex.Lock()
	terminations := process.termination
	process.mutex.Unlock()
	if terminations != 1 {
		t.Fatalf("Close() termination calls = %d, want 1", terminations)
	}
}

func TestUnsupportedServerRequestGetsErrorWithoutApproval(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject unsafe request.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": "approval-1", "method": "item/commandExecution/requestApproval",
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"},
	}); err != nil {
		t.Fatalf("emit server request: %v", err)
	}
	response := process.nextServerRequestResponse(t)
	if response.ID != "approval-1" || response.Error.Code != -32601 || response.Error.Message != "unsupported by Symmetry Codex adapter" {
		t.Fatalf("server request response = %+v", response)
	}
	if !sink.hasDiagnostic("unsupported_server_request") {
		t.Fatalf("unsupported server request did not emit diagnostic: %#v", sink.events)
	}
}

func TestCloseAttemptsNativeInterruptThenUsesProcessTermination(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Close cleanly.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	errResult := make(chan error, 1)
	go func() { errResult <- session.Close(context.Background()) }()
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt {
		t.Fatalf("Close() native request = %+v, want turn/interrupt", interrupt)
	}
	process.reply(t, interrupt.ID, map[string]any{})
	if err := <-errResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	process.mutex.Lock()
	terminations := process.termination
	process.mutex.Unlock()
	if terminations != 1 {
		t.Fatalf("Terminate calls = %d, want 1", terminations)
	}
}

func TestCloseAfterTerminalResultOnlyCleansUpAppServer(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Finish before close.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case raw := <-process.writes:
		t.Fatalf("Close() wrote unexpected post-terminal RPC: %s", raw)
	default:
	}
	process.mutex.Lock()
	terminations := process.termination
	process.mutex.Unlock()
	if terminations != 1 {
		t.Fatalf("Terminate calls = %d, want 1", terminations)
	}
}

func TestPersistentTurnWaitTurnThenCloseResolvesFinalResult(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Close after terminal observation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))

	turnDone := make(chan error, 1)
	go func() { turnDone <- session.WaitTurn(context.Background()) }()
	select {
	case err := <-turnDone:
		if err != nil {
			t.Fatalf("WaitTurn() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitTurn() did not release after matching terminal event")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("final Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || result.Process.Terminated != true {
		t.Fatalf("final result = %+v, want semantic success with an expected terminated app-server", result)
	}
}

func TestPersistentTurnWaitTurnThenCloseAcceptsForcedTerminationResult(t *testing.T) {
	process := &resultOnTerminateNativeProcess{
		fakeNativeProcess: newFakeNativeProcess(),
		terminationResult: execution.Result{
			PID:        42,
			ExitCode:   -1,
			Terminated: true,
			WaitError:  errors.New("signal: killed"),
		},
	}
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process.fakeNativeProcess, session)
	completeTurnStart(t, process.fakeNativeProcess, session, harness.TurnRequest{Goal: "Close after forced stop.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || !result.Process.Terminated || result.Process.ExitCode != -1 || result.Process.WaitError == nil {
		t.Fatalf("final forced-close result = %+v, want semantic success with expected termination evidence", result)
	}
}

func TestTerminalObservationPrecedesNonzeroProcessFailure(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Preserve process failure.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 23, WaitError: errors.New("exit status 23")})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("final Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != protocol.TaskResultReasonProcessFailure {
		t.Fatalf("final result = %+v, want process_failure", result)
	}
}

func TestInterruptedTerminalReleasesWaitTurnBeforeCancelAcknowledgement(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Resolve cancel after terminal observation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want terminal observation before cancel acknowledgement", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("final Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled || !result.Process.Terminated {
		t.Fatalf("final interrupted result = %+v, want cancelled physical final result", result)
	}
	if err := <-controlDone; err == nil {
		t.Fatal("cancel control unexpectedly succeeded without an interrupt acknowledgement")
	}
	_ = interrupt
}

func TestRepeatedCloseWaitsForSharedOperationAndStabilizesSuccess(t *testing.T) {
	process := newDelayedTerminateNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process.fakeNativeProcess, session)
	completeTurnStart(t, process.fakeNativeProcess, session, harness.TurnRequest{Goal: "Share close.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- session.Close(context.Background()) }()
	select {
	case <-process.terminateStarted:
	case <-time.After(time.Second):
		t.Fatal("first Close() did not start process termination")
	}
	secondContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Close(secondContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Close() error = %v, want cancellation while first close is incomplete", err)
	}
	close(process.releaseTerminate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("repeated Close() error = %v, want stable success", err)
	}
}

func TestCloseRetriesFailedTerminationAndStabilizesSuccess(t *testing.T) {
	terminationErr := errors.New("transient termination failure")
	process := newRetryableTerminateNativeProcess(terminationErr)
	t.Cleanup(process.releaseFirstTermination)
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process.fakeNativeProcess, session)
	completeTurnStart(t, process.fakeNativeProcess, session, harness.TurnRequest{Goal: "Retry close.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "completed", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- session.Close(context.Background()) }()
	select {
	case <-process.firstTerminateStarted:
	case <-time.After(time.Second):
		t.Fatal("first Close() did not start process termination")
	}

	secondContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Close(secondContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Close() error = %v, want cancellation while first close is incomplete", err)
	}

	process.releaseFirstTermination()
	if err := <-firstDone; !errors.Is(err, terminationErr) {
		t.Fatalf("first Close() error = %v, want %v", err, terminationErr)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("retry Close() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("repeated successful Close() error = %v", err)
	}
	if calls := process.terminationCallCount(); calls != 2 {
		t.Fatalf("Terminate calls = %d, want one failed attempt and one retry", calls)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil {
		t.Fatalf("final result = %+v, want one preserved semantic success", result)
	}
}

func TestSanitizedLifecycleFixturePreservesNativeInterleaving(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "codex", "0.153.4", "app-server-lifecycle.jsonl"))
	if err != nil {
		t.Fatalf("read lifecycle fixture: %v", err)
	}
	frames, err := NewFramer(64 * 1024).Feed(fixture)
	if err != nil {
		t.Fatalf("frame lifecycle fixture: %v", err)
	}
	if len(frames) != 9 {
		t.Fatalf("fixture frame count = %d, want 9", len(frames))
	}
	if frames[0].Method != "remoteControl/status/changed" || frames[1].Kind != FrameResponse || frames[3].Kind != FrameResponse || frames[8].Method != appServerEventTurnCompleted {
		t.Fatalf("fixture ordering changed: %#v", frames)
	}
}

func startNativeSession(t *testing.T, adapter *Adapter, sink harness.EventSink) harness.StagedSession {
	t.Helper()
	raw, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir(), Invocation: execution.Invocation{Env: []string{"PATH=" + strings.TrimSpace("test")}}}, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged, ok := raw.(harness.StagedSession)
	if !ok {
		t.Fatalf("Start() session = %T, want StagedSession", raw)
	}
	return staged
}

func completeOpen(t *testing.T, process *fakeNativeProcess, session harness.StagedSession) harness.NativeSessionHandle {
	t.Helper()
	handleResult := make(chan harness.NativeSessionHandle, 1)
	errResult := make(chan error, 1)
	go func() {
		handle, err := session.Open(context.Background())
		handleResult <- handle
		errResult <- err
	}()
	initialize := process.nextRequest(t)
	if initialize.Method != appServerMethodInitialize || initialize.Params.ClientInfo.Name != "symmetry" || initialize.Params.ClientInfo.Version != "1" {
		t.Fatalf("initialize request = %+v", initialize)
	}
	process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "method": "remoteControl/status/changed", "params": map[string]any{"status": "idle"}})
	process.reply(t, initialize.ID, map[string]any{"userAgent": "codex-test", "codexHome": "C:/redacted", "platformFamily": "windows", "platformOs": "windows"})
	initialized := process.nextNotification(t)
	if initialized.Method != appServerMethodInitialized || initialized.Params != nil {
		t.Fatalf("initialized notification = %+v", initialized)
	}
	thread := process.nextRequest(t)
	if thread.Method != appServerMethodThreadStart || thread.Params.CWD == "" || thread.Params.Ephemeral || thread.Params.Sandbox != nativeEngineeringSandbox || thread.Params.ApprovalPolicy != nativeApprovalOnRequest {
		t.Fatalf("thread/start request = %+v", thread)
	}
	process.reply(t, thread.ID, map[string]any{
		"approvalPolicy":    nativeApprovalOnRequest,
		"approvalsReviewer": "user",
		"cwd":               thread.Params.CWD,
		"model":             "gpt-6-astra",
		"modelProvider":     "openai",
		"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{thread.Params.CWD}},
		"thread":            map[string]any{"id": "thread-1", "cwd": thread.Params.CWD, "ephemeral": false},
	})
	if err := <-errResult; err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return <-handleResult
}

func completeTurnStart(t *testing.T, process *fakeNativeProcess, session harness.StagedSession, request harness.TurnRequest) {
	t.Helper()
	errResult := make(chan error, 1)
	go func() { errResult <- session.StartTurn(context.Background(), request) }()
	turn := process.nextRequest(t)
	if turn.Method != appServerMethodTurnStart || turn.Params.ThreadID != "thread-1" || len(turn.Params.Input) != 1 || turn.Params.Input[0].Type != "text" {
		t.Fatalf("turn/start request = %+v", turn)
	}
	if !strings.Contains(turn.Params.Input[0].Text, request.Goal) || !strings.Contains(turn.Params.Input[0].Text, string(request.Context)) {
		t.Fatalf("turn prompt did not preserve separately labelled Goal and Context: %q", turn.Params.Input[0].Text)
	}
	if turn.Params.OutputSchema["type"] != "object" {
		t.Fatalf("turn/start output schema misses required root type: %#v", turn.Params.OutputSchema)
	}
	properties, ok := turn.Params.OutputSchema["properties"].(map[string]any)
	if !ok || properties["schema_version"].(map[string]any)["const"] != protocol.TaskResultSchemaVersion {
		t.Fatalf("turn/start schema_version must preserve the canonical const: %#v", turn.Params.OutputSchema)
	}
	subject, ok := properties["subject"].(map[string]any)
	if !ok || subject["$ref"] != "#/definitions/Subject" {
		t.Fatalf("turn/start output schema must reference the canonical complete subject: %#v", turn.Params.OutputSchema)
	}
	required, ok := turn.Params.OutputSchema["required"].([]any)
	if !ok {
		t.Fatalf("turn/start output schema required field has unexpected type: %#v", turn.Params.OutputSchema["required"])
	}
	if !containsString(required, "subject") {
		t.Fatalf("turn/start output schema does not require subject: %#v", required)
	}
	process.reply(t, turn.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}})
	if err := <-errResult; err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
}

func TestNativeTaskResultSchemaUsesIndependentCanonicalDocument(t *testing.T) {
	want, err := contracts.TaskResultSchema()
	if err != nil {
		t.Fatalf("contracts.TaskResultSchema() error = %v", err)
	}
	got, err := nativeTaskResultSchema()
	if err != nil {
		t.Fatalf("nativeTaskResultSchema() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native task-result schema diverges from canonical document")
	}
	got["title"] = "mutated"
	again, err := nativeTaskResultSchema()
	if err != nil {
		t.Fatalf("nativeTaskResultSchema() second call error = %v", err)
	}
	if again["title"] == "mutated" {
		t.Fatal("native task-result schema returned shared mutable state")
	}
}

func fakeAdapter(process *fakeNativeProcess) *Adapter {
	return &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
}

func validTaskResultJSON(t *testing.T, kind string) string {
	t.Helper()
	subject := protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000007",
		Commit:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TreeDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatalf("hash task result subject: %v", err)
	}
	result := map[string]any{
		"schema_version":       "symmetry.task_result.v1",
		"result_id":            "00000000-0000-4000-8000-000000000006",
		"kind":                 kind,
		"summary":              "made bounded progress",
		"subject":              subject,
		"subject_hash":         subjectHash,
		"evidence_refs":        []string{},
		"blocker":              nil,
		"proposed_next_action": nil,
		"proposal":             nil,
		"reason":               nil,
		"diagnostics":          []any{},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode task result: %v", err)
	}
	return string(encoded)
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func completedNotification(threadID, turnID, status, taskResult string) map[string]any {
	return completedNotificationWithPhase(threadID, turnID, status, taskResult, "final_answer")
}

func completedNotificationWithPhase(threadID, turnID, status, taskResult string, phase any) map[string]any {
	item := map[string]any{"id": "item-final", "type": "agentMessage", "text": taskResult}
	if phase != nil {
		item["phase"] = phase
	}
	return map[string]any{
		"jsonrpc": "2.0", "method": appServerEventTurnCompleted,
		"params": map[string]any{"threadId": threadID, "turn": map[string]any{
			"id": turnID, "status": status, "items": []any{item},
		}},
	}
}

func deltaNotification(delta string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": appServerEventAgentMessageDelta,
		"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "itemId": "item-1", "delta": delta},
	}
}

type parsedRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		ClientInfo struct {
			Name    string
			Version string
		}
		ApprovalPolicy string
		CWD            string
		Ephemeral      bool
		Sandbox        string
		ThreadID       string
		TurnID         string
		Input          []userInput
		Model          string
		OutputSchema   map[string]any
	}
}

type parsedNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type parsedServerRequestResponse struct {
	JSONRPC string   `json:"jsonrpc"`
	ID      string   `json:"id"`
	Error   rpcError `json:"error"`
}

type fakeNativeProcess struct {
	mutex       sync.Mutex
	sink        execution.Sink
	writes      chan []byte
	completed   chan execution.Result
	terminated  bool
	termination int
}

type resultOnTerminateNativeProcess struct {
	*fakeNativeProcess
	terminationResult execution.Result
	terminateOnce     sync.Once
}

func (process *resultOnTerminateNativeProcess) Terminate(_ context.Context, _ time.Duration) error {
	process.terminateOnce.Do(func() {
		process.mutex.Lock()
		process.terminated = true
		process.termination++
		process.mutex.Unlock()
		process.finish(process.terminationResult)
	})
	return nil
}

type delayedTerminateNativeProcess struct {
	*fakeNativeProcess
	terminateStarted chan struct{}
	releaseTerminate chan struct{}
	terminateOnce    sync.Once
}

func newDelayedTerminateNativeProcess() *delayedTerminateNativeProcess {
	return &delayedTerminateNativeProcess{
		fakeNativeProcess: newFakeNativeProcess(),
		terminateStarted:  make(chan struct{}),
		releaseTerminate:  make(chan struct{}),
	}
}

func (process *delayedTerminateNativeProcess) Terminate(ctx context.Context, grace time.Duration) error {
	process.terminateOnce.Do(func() { close(process.terminateStarted) })
	select {
	case <-process.releaseTerminate:
		return process.fakeNativeProcess.Terminate(ctx, grace)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type retryableTerminateNativeProcess struct {
	*fakeNativeProcess
	firstErr              error
	firstTerminateStarted chan struct{}
	releaseFirst          chan struct{}
	startOnce             sync.Once
	releaseOnce           sync.Once
	mutex                 sync.Mutex
	terminationCalls      int
}

func newRetryableTerminateNativeProcess(firstErr error) *retryableTerminateNativeProcess {
	return &retryableTerminateNativeProcess{
		fakeNativeProcess:     newFakeNativeProcess(),
		firstErr:              firstErr,
		firstTerminateStarted: make(chan struct{}),
		releaseFirst:          make(chan struct{}),
	}
}

func (process *retryableTerminateNativeProcess) Terminate(ctx context.Context, grace time.Duration) error {
	process.mutex.Lock()
	process.terminationCalls++
	call := process.terminationCalls
	process.mutex.Unlock()
	if call == 1 {
		process.startOnce.Do(func() { close(process.firstTerminateStarted) })
		select {
		case <-process.releaseFirst:
		case <-ctx.Done():
			return ctx.Err()
		}
		process.fakeNativeProcess.finish(execution.Result{PID: 42, Terminated: true})
		return process.firstErr
	}
	return process.fakeNativeProcess.Terminate(ctx, grace)
}

func (process *retryableTerminateNativeProcess) releaseFirstTermination() {
	process.releaseOnce.Do(func() { close(process.releaseFirst) })
}

func (process *retryableTerminateNativeProcess) terminationCallCount() int {
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.terminationCalls
}

type blockingFakeNativeProcess struct {
	*fakeNativeProcess
	writeStarted chan struct{}
	writeOnce    sync.Once
	unblockWrite chan struct{}
	writeCount   int
	blockAfter   int
}

func newFakeNativeProcess() *fakeNativeProcess {
	return &fakeNativeProcess{writes: make(chan []byte, 32), completed: make(chan execution.Result, 1)}
}

func newBlockingFakeNativeProcess() *blockingFakeNativeProcess {
	return &blockingFakeNativeProcess{
		fakeNativeProcess: newFakeNativeProcess(),
		writeStarted:      make(chan struct{}),
		unblockWrite:      make(chan struct{}),
		blockAfter:        1,
	}
}

func (process *fakeNativeProcess) WriteInput(input []byte) error {
	process.writes <- append([]byte(nil), input...)
	return nil
}

func (process *fakeNativeProcess) WriteInputContext(ctx context.Context, input []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return process.WriteInput(input)
}

func (process *blockingFakeNativeProcess) WriteInput(input []byte) error {
	process.mutex.Lock()
	process.writeCount++
	shouldBlock := process.writeCount >= process.blockAfter
	process.mutex.Unlock()
	if shouldBlock {
		process.writeOnce.Do(func() { close(process.writeStarted) })
		<-process.unblockWrite
	}
	return process.fakeNativeProcess.WriteInput(input)
}

func (process *blockingFakeNativeProcess) WriteInputContext(ctx context.Context, input []byte) error {
	process.mutex.Lock()
	process.writeCount++
	shouldBlock := process.writeCount >= process.blockAfter
	process.mutex.Unlock()
	if shouldBlock {
		process.writeOnce.Do(func() { close(process.writeStarted) })
		select {
		case <-process.unblockWrite:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return process.fakeNativeProcess.WriteInputContext(ctx, input)
}

func (process *fakeNativeProcess) Wait() execution.Result { return <-process.completed }

func (process *fakeNativeProcess) Terminate(_ context.Context, _ time.Duration) error {
	process.mutex.Lock()
	process.terminated = true
	process.termination++
	process.mutex.Unlock()
	process.finish(execution.Result{PID: 42, Terminated: true})
	return nil
}

func (process *blockingFakeNativeProcess) Terminate(ctx context.Context, grace time.Duration) error {
	select {
	case <-process.unblockWrite:
	default:
		close(process.unblockWrite)
	}
	return process.fakeNativeProcess.Terminate(ctx, grace)
}

func (process *fakeNativeProcess) ProcessDetails() (int, string) { return 42, "test:42" }

func (process *fakeNativeProcess) nextRequest(t *testing.T) parsedRequest {
	t.Helper()
	select {
	case raw := <-process.writes:
		var request parsedRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatalf("decode request %q: %v", raw, err)
		}
		if request.ID == 0 {
			t.Fatalf("request has no id: %s", raw)
		}
		if request.JSONRPC != jsonRPCVersion {
			t.Fatalf("request jsonrpc = %q, want %q: %s", request.JSONRPC, jsonRPCVersion, raw)
		}
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for native request")
		return parsedRequest{}
	}
}

func (process *fakeNativeProcess) nextNotification(t *testing.T) parsedNotification {
	t.Helper()
	select {
	case raw := <-process.writes:
		var notification parsedNotification
		if err := json.Unmarshal(raw, &notification); err != nil {
			t.Fatalf("decode notification %q: %v", raw, err)
		}
		if notification.JSONRPC != jsonRPCVersion {
			t.Fatalf("notification jsonrpc = %q, want %q: %s", notification.JSONRPC, jsonRPCVersion, raw)
		}
		return notification
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for native notification")
		return parsedNotification{}
	}
}

func (process *fakeNativeProcess) nextServerRequestResponse(t *testing.T) parsedServerRequestResponse {
	t.Helper()
	select {
	case raw := <-process.writes:
		var response parsedServerRequestResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("decode server request response %q: %v", raw, err)
		}
		if response.JSONRPC != jsonRPCVersion {
			t.Fatalf("server response jsonrpc = %q, want %q: %s", response.JSONRPC, jsonRPCVersion, raw)
		}
		return response
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for unsupported server request response")
		return parsedServerRequestResponse{}
	}
}

func (process *fakeNativeProcess) reply(t *testing.T, id uint64, result any) {
	t.Helper()
	if err := process.emitJSON(t, map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Fatalf("emit response: %v", err)
	}
}

func (process *fakeNativeProcess) emitJSON(t *testing.T, message any) error {
	t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("encode native output: %v", err)
	}
	process.mutex.Lock()
	sink := process.sink
	process.mutex.Unlock()
	if sink == nil {
		t.Fatal("native process has no output sink")
	}
	return sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: append(raw, '\n')})
}

func (process *fakeNativeProcess) finish(result execution.Result) {
	select {
	case process.completed <- result:
	default:
	}
}

type recordingHarnessSink struct {
	mutex    sync.Mutex
	events   []harness.Event
	failKind harness.EventKind
	failure  error
}

type blockingOrderedSink struct {
	*recordingHarnessSink
	blockKind harness.EventKind
	blocked   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newBlockingOrderedSink(blockKind harness.EventKind) *blockingOrderedSink {
	return &blockingOrderedSink{
		recordingHarnessSink: &recordingHarnessSink{},
		blockKind:            blockKind,
		blocked:              make(chan struct{}),
		release:              make(chan struct{}),
	}
}

func (sink *blockingOrderedSink) Handle(ctx context.Context, event harness.Event) error {
	if err := sink.recordingHarnessSink.Handle(ctx, event); err != nil {
		return err
	}
	if event.Kind == sink.blockKind {
		shouldBlock := false
		sink.once.Do(func() { shouldBlock = true; close(sink.blocked) })
		if shouldBlock {
			<-sink.release
		}
	}
	return nil
}

func (sink *blockingOrderedSink) deltaData() []string {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	var values []string
	for _, event := range sink.events {
		if event.Kind == harness.EventMessageDelta {
			values = append(values, string(event.Data))
		}
	}
	return values
}

func (sink *recordingHarnessSink) Handle(_ context.Context, event harness.Event) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.events = append(sink.events, event)
	if sink.failure != nil && event.Kind == sink.failKind {
		return sink.failure
	}
	return nil
}

func (sink *recordingHarnessSink) has(kind harness.EventKind) bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func (sink *recordingHarnessSink) count(kind harness.EventKind) int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	count := 0
	for _, event := range sink.events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func (sink *recordingHarnessSink) hasDiagnostic(code string) bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == harness.EventDiagnostic && event.Code == code {
			return true
		}
	}
	return false
}

func (sink *recordingHarnessSink) diagnosticCount(code string) int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	count := 0
	for _, event := range sink.events {
		if event.Kind == harness.EventDiagnostic && event.Code == code {
			count++
		}
	}
	return count
}

func (sink *recordingHarnessSink) payload(kind harness.EventKind) json.RawMessage {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return event.Payload
		}
	}
	return nil
}
