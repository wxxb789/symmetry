package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
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

	process.emitJSON(t, statusChangedNotification("idle"))
	process.emitJSON(t, deltaNotification("working"))
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

func TestAdapterStartUsesOwnerSuppliedProcessSeam(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
			t.Fatal("adapter default process starter was used despite an owner-supplied seam")
			return nil, nil
		},
	}
	called := false
	raw, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: t.TempDir(),
		Invocation: execution.Invocation{
			Env: []string{"PATH=test"},
		},
		StartProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
			called = true
			process.sink = sink
			return process, nil
		},
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() with owner process seam: %v", err)
	}
	if !called {
		t.Fatal("adapter did not use the owner-supplied process seam")
	}
	session, ok := raw.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want *nativeSession", raw)
	}
	session.cancel()
}

func TestAdapterForwardsLinuxV2Invocation(t *testing.T) {
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
	v2 := &execution.LinuxV2Invocation{}
	raw, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:  t.TempDir(),
		Invocation: execution.Invocation{LinuxV2Required: true, LinuxV2: v2},
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !invocation.LinuxV2Required || invocation.LinuxV2 != v2 {
		t.Fatalf("Linux v2 invocation = required:%t v2:%p, want true and %p", invocation.LinuxV2Required, invocation.LinuxV2, v2)
	}
	if session, ok := raw.(harness.Session); ok {
		_ = session.Close(context.Background())
	}
}

func TestAdapterStartForwardsPersistProcessWithAuthority(t *testing.T) {
	process := newFakeNativeProcess()
	var invocation execution.Invocation
	var called bool
	var gotPID int
	var gotIdentity string
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			invocation = got
			process.sink = sink
			return process, nil
		},
	}
	request := harness.StartRequest{
		Workspace: t.TempDir(),
		Invocation: execution.Invocation{
			PersistProcessWithAuthority: func(pid int, identity string, value *authority.Supervisor) error {
				if value != nil {
					t.Fatalf("authority = %p, want nil", value)
				}
				called = true
				gotPID = pid
				gotIdentity = identity
				return nil
			},
			PrepareSupervisorHandoff:           func(authority.SupervisorHandoff) error { return nil },
			BindSupervisorHandoff:              func(authority.SupervisorHandoff, int, string) error { return nil },
			CommitSupervisorHandoff:            func(authority.SupervisorHandoff, time.Time) error { return nil },
			RecordSupervisorHandoffStopReceipt: func(authority.SupervisorHandoff, authority.StopReceipt) error { return nil },
			ClearSupervisorHandoff:             func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error { return nil },
		},
	}
	started, err := adapter.Start(context.Background(), request, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = process.Terminate(context.Background(), 0)
		_, _ = started.Wait(context.Background())
	})
	if invocation.PersistProcessWithAuthority == nil {
		t.Fatal("Start() did not forward PersistProcessWithAuthority")
	}
	if invocation.PrepareSupervisorHandoff == nil || invocation.BindSupervisorHandoff == nil || invocation.CommitSupervisorHandoff == nil || invocation.RecordSupervisorHandoffStopReceipt == nil || invocation.ClearSupervisorHandoff == nil {
		t.Fatal("Start() did not forward complete supervisor handoff callbacks")
	}
	if err := invocation.PersistProcessWithAuthority(42, "test:42", nil); err != nil {
		t.Fatalf("forwarded PersistProcessWithAuthority() error = %v", err)
	}
	if !called || gotPID != 42 || gotIdentity != "test:42" {
		t.Fatalf("callback = called:%t pid:%d identity:%q, want called with (42, test:42)", called, gotPID, gotIdentity)
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
			process.reply(t, initialize.ID, initializeReply())
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
				"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{thread.Params.CWD}, "excludeTmpdirEnvVar": true, "excludeSlashTmp": true},
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

func TestOpenRefusesAConcurrentOpenWithTheFrozenText(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	opened := make(chan error, 1)
	go func() {
		_, err := session.Open(context.Background())
		opened <- err
	}()
	initialize := process.nextRequest(t)
	if initialize.Method != appServerMethodInitialize {
		t.Fatalf("first request = %+v, want initialize", initialize)
	}
	if _, err := session.Open(context.Background()); err == nil || err.Error() != "codex native session is already opening" {
		t.Fatalf("concurrent Open() error = %v, want the frozen already-opening text", err)
	}
	process.emitJSON(t, statusChangedNotification("idle"))
	process.reply(t, initialize.ID, initializeReply())
	_ = process.nextNotification(t)
	thread := process.nextRequest(t)
	process.reply(t, thread.ID, map[string]any{
		"approvalPolicy":    nativeApprovalOnRequest,
		"approvalsReviewer": "user",
		"cwd":               thread.Params.CWD,
		"model":             "gpt-6-astra",
		"modelProvider":     "openai",
		"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{}, "excludeTmpdirEnvVar": true, "excludeSlashTmp": true},
		"thread":            map[string]any{"id": "thread-1", "cwd": thread.Params.CWD, "ephemeral": false},
	})
	if err := <-opened; err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	_ = session.Close(context.Background())
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
	process.reply(t, initialize.ID, initializeReply())
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
			Type:                "workspaceWrite",
			NetworkAccess:       json.RawMessage(`false`),
			WritableRoots:       []string{workspace},
			ExcludeTmpdirEnvVar: boolPointer(true),
			ExcludeSlashTmp:     boolPointer(true),
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

func TestSinkFailureCannotProduceNativeSuccess(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{failKind: harness.EventMessageDelta, failure: errors.New("journal unavailable")}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Persist events.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	err := process.emitJSON(t, deltaNotification("cannot persist"))
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

func TestTerminalSuccessIsOverriddenByNonzeroProcessExit(t *testing.T) {
	process := newFakeNativeProcess()
	native := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
	native.process = process
	native.processReady = true
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

func TestCloneTaskResultPreservesSchemaValidEmptyArrays(t *testing.T) {
	semantic, err := protocol.ParseTaskResult([]byte(validTaskResultJSON(t, "progress")))
	if err != nil {
		t.Fatal(err)
	}
	cloned := harness.CloneTaskResult(harness.TaskResult{Kind: harness.ResultSucceeded, Semantic: &semantic})
	encoded, err := json.Marshal(cloned.Semantic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.ParseTaskResult(encoded); err != nil {
		t.Fatalf("cloning a validated task result changed its wire validity: %v", err)
	}
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
	case <-time.After(testHangGuard):
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
	case <-time.After(testHangGuard):
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
	case <-time.After(testHangGuard):
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

func TestNativeTaskResultSchemaIsAnIndependentProjection(t *testing.T) {
	want, err := contracts.TaskResultSchema()
	if err != nil {
		t.Fatalf("contracts.TaskResultSchema() error = %v", err)
	}
	got, err := nativeTaskResultSchema()
	if err != nil {
		t.Fatalf("nativeTaskResultSchema() error = %v", err)
	}
	if reflect.DeepEqual(got, want) {
		t.Fatal("native task-result schema must not be the canonical document")
	}
	if _, present := want["$defs"]; present {
		t.Fatal("canonical schema was mutated with native $defs")
	}
	if definitions, _ := got["$defs"].(map[string]any); len(definitions) == 0 {
		t.Fatal("native schema has no reachable definitions")
	}
	got["title"] = "mutated"
	again, err := nativeTaskResultSchema()
	if err != nil {
		t.Fatalf("nativeTaskResultSchema() second call error = %v", err)
	}
	if again["title"] == "mutated" {
		t.Fatal("native task-result schema returned shared mutable state")
	}
	canonicalAgain, err := contracts.TaskResultSchema()
	if err != nil {
		t.Fatalf("contracts.TaskResultSchema() second call error = %v", err)
	}
	if canonicalAgain["title"] == "mutated" {
		t.Fatal("native projection mutated the shared canonical document")
	}
}

func TestStartTurnBeforeOpenRefusesWithTheCodexThreadBarrierText(t *testing.T) {
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
	started, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:      t.TempDir(),
		PersistProcess: func(int, string) error { return nil },
	}, &recordingHarnessSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	session := started.(harness.StagedSession)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "before open", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err == nil || err.Error() != "codex native session has not opened a thread" {
		t.Fatalf("StartTurn before Open = %v, want the thread barrier text", err)
	}
	_ = process.Terminate(context.Background(), 0)
}
