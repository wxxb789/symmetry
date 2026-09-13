package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestProbeRecordsExactTransportButRemainsUnverified(t *testing.T) {
	adapter := NewAdapterWithRunner("opencode", fixtureCommandRunner{responses: map[string][]byte{
		"--version":    []byte("1.18.30\n"),
		"serve --help": []byte("opencode serve --hostname --port --pure"),
	}})
	capabilities, err := adapter.Probe(context.Background())
	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("Probe() error = %v, want ErrNativeUnverified", err)
	}
	if err := capabilities.Validate(); err != nil {
		t.Fatalf("capabilities invalid: %v", err)
	}
	if !capabilities.VersionKnown || capabilities.NativeVersion != TestedVersion || !capabilities.TransportVerified || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Usage != harness.UsageUnknown {
		t.Fatalf("capabilities = %+v, want known but completely unverified projection", capabilities)
	}
}

func TestProbeRejectsWrongVersionAndIncompleteHelp(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		help    string
		want    error
	}{
		{name: "wrong version", version: "1.18.29", want: harness.ErrUnsupportedVersion},
		{name: "bad help", version: TestedVersion, help: "opencode serve --port", want: harness.ErrNativeUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewAdapterWithRunner("opencode", fixtureCommandRunner{responses: map[string][]byte{
				"--version":    []byte(test.version),
				"serve --help": []byte(test.help),
			}})
			_, err := adapter.Probe(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("Probe() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStartUsesOwnedServeInvocationPersistsBeforeOutputAndWithholdsSecrets(t *testing.T) {
	process := newFakeProcess()
	api := &fakeAPI{}
	var invocation execution.Invocation
	persisted := false
	starter := func(ctx context.Context, got execution.Invocation, sink execution.Sink) (nativeProcess, error) {
		invocation = got
		if got.PersistProcess == nil {
			t.Fatal("PersistProcess is nil")
		}
		if err := got.PersistProcess(123, "created:123"); err != nil {
			t.Fatalf("PersistProcess() error = %v", err)
		}
		persisted = true
		if err := sink.Handle(ctx, execution.Event{Stream: execution.Stdout, Sequence: 1, Data: []byte("password=secret")}); err != nil {
			t.Fatalf("sink error = %v", err)
		}
		return process, nil
	}
	adapter := newTestAdapter(starter, api)
	sink := &recordingSink{}
	request := startRequest(t)
	request.Invocation.Env = []string{"PATH=C:\\tools", "OPENCODE_SERVER_PASSWORD=caller-secret", "OPENCODE_SERVER_USERNAME=caller"}
	request.PersistProcess = func(pid int, identity string) error {
		if pid != 123 || identity != "created:123" {
			t.Fatalf("PersistProcess(%d, %q)", pid, identity)
		}
		return nil
	}
	session, err := adapter.Start(context.Background(), request, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid, identity := session.(harness.StagedSession).ProcessDetails()
	if !persisted || pid != 123 || identity != "created:123" {
		t.Fatalf("ProcessDetails() = %d, %q; persisted=%v", pid, identity, persisted)
	}
	wantArgs := []string{"serve", "--hostname", "127.0.0.1", "--port", "0", "--pure"}
	if strings.Join(invocation.Args, "|") != strings.Join(wantArgs, "|") || invocation.Program != "opencode-test" || invocation.Dir != request.Workspace {
		t.Fatalf("invocation = %+v, want owned explicit serve", invocation)
	}
	if strings.Contains(strings.Join(invocation.Env, "\n"), "caller-secret") || strings.Contains(strings.Join(invocation.Env, "\n"), "caller=") || !strings.Contains(strings.Join(invocation.Env, "\n"), "OPENCODE_SERVER_PASSWORD=test-secret") {
		t.Fatalf("invocation env did not replace caller credentials: %#v", invocation.Env)
	}
	if sink.containsData("secret") {
		t.Fatal("process output bytes leaked into harness event data")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartRetainsProcessOwnerWhenRunnerReturnsError(t *testing.T) {
	process := newFakeProcess()
	want := errors.New("persist process failed after commit")
	adapter := newAdapter(
		"opencode-test",
		nil,
		func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
			return process, want
		},
		func(Config) (api, error) { return &fakeAPI{}, nil },
		func() (string, string, error) { return "opencode", "test-secret", nil },
		func(int, string) ConnectionVerifier { return func(context.Context, net.Conn) error { return nil } },
	)
	started, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want runner failure", err)
	}
	session, ok := started.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want retained *nativeSession", started)
	}
	if pid, identity := session.ProcessDetails(); pid != 123 || identity != "created:123" {
		t.Fatalf("ProcessDetails() = (%d, %q), want returned process identity", pid, identity)
	}
	if _, err := session.Open(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Open() error = %v, want retained start failure", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if process.terminateCalls != 1 {
		t.Fatalf("Terminate calls = %d, want one session-owned cleanup", process.terminateCalls)
	}
}

func TestOpenStartTurnAndWaitStayFailClosed(t *testing.T) {
	process := newFakeProcess()
	stream := newBlockingReadCloser()
	api := &fakeAPI{stream: stream}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	sink := &recordingSink{}
	session, err := adapter.Start(context.Background(), startRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	handle, err := staged.Open(context.Background())
	if err != nil || !isID(handle.ID, "ses_") || api.healthCalls != 1 || api.createCalls != 1 {
		t.Fatalf("Open() = %+v, %v; health=%d create=%d", handle, err, api.healthCalls, api.createCalls)
	}
	turn := harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{"key":"value"}`)}
	if err := staged.StartTurn(context.Background(), turn); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if api.openEventCalls != 1 || api.promptCalls != 1 || !sink.hasKind(harness.EventSessionStarted) {
		t.Fatalf("start turn calls: events=%d prompt=%d sink=%+v", api.openEventCalls, api.promptCalls, sink.events)
	}
	if err := staged.WaitTurn(context.Background()); !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("WaitTurn() error = %v, want ErrNativeUnverified", err)
	}
	process.finish(execution.Result{PID: 123, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultUnknown || result.Semantic != nil {
		t.Fatalf("Wait() = %+v, %v; want unknown without semantic result", result, err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartTurnAdmitsPromptBeforeOpeningDurableReplay(t *testing.T) {
	process := newFakeProcess()
	stream := newBlockingReadCloser()
	api := &fakeAPI{stream: stream}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if prompt := api.lastPromptRequest(); !prompt.Resume {
		t.Fatalf("StartTurn() prompt = %+v, want explicit Resume:true work request", prompt)
	}
	if got, want := api.callOrder(), []string{"prompt", "events"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	if after := api.eventCursors(); len(after) != 1 || after[0] != 0 {
		t.Fatalf("event cursors = %#v, want [0]", after)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartTurnKeepsDurableReplayOwnedBySession(t *testing.T) {
	process := newFakeProcess()
	var stream *contextBlockingReadCloser
	api := &fakeAPI{streamFactory: func(ctx context.Context, _ string, _ uint64) io.ReadCloser {
		stream = newContextBlockingReadCloser(ctx)
		return stream
	}}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	callerContext, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	if err := staged.StartTurn(callerContext, harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	<-stream.readStarted
	if stream.contextCancelled() {
		t.Fatal("StartTurn() cancelled the successful durable replay stream")
	}
	cancelCaller()
	if stream.contextCancelled() {
		t.Fatal("caller turn context cancelled the session-owned durable replay stream")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartTurnCancelsInFlightPromptWhenCallerCancels(t *testing.T) {
	process := newFakeProcess()
	promptStarted := make(chan struct{})
	api := &fakeAPI{promptStarted: promptStarted}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	callerContext, cancelCaller := context.WithCancel(context.Background())
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- staged.StartTurn(callerContext, harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)})
	}()
	<-promptStarted
	cancelCaller()
	if err := <-turnDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("StartTurn() error = %v, want caller cancellation", err)
	}
	if api.openEventCalls != 0 || process.terminateCalls != 1 {
		t.Fatalf("events=%d terminate=%d, want no replay and one cleanup", api.openEventCalls, process.terminateCalls)
	}
}

func TestStartTurnKeepsReplayOpenWhenCallerCancelsAfterAdmission(t *testing.T) {
	process := newFakeProcess()
	openStarted := make(chan struct{})
	openRelease := make(chan struct{})
	stream := newBlockingReadCloser()
	api := &fakeAPI{stream: stream, openEventStarted: openStarted, openEventRelease: openRelease}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	callerContext, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- staged.StartTurn(callerContext, harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)})
	}()
	<-openStarted
	cancelCaller()
	if api.openEventContextCancelled() {
		t.Fatal("caller cancellation interrupted durable replay opening after admission")
	}
	close(openRelease)
	if err := <-turnDone; err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartTurnObservesPromptAdmissionFromDurableReplay(t *testing.T) {
	process := newFakeProcess()
	api := &fakeAPI{replayPromptAdmission: true}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	sink := &recordingSink{expectedCode: "opencode_session_event", expectedCodeSeen: make(chan struct{})}
	session, err := adapter.Start(context.Background(), startRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	<-sink.expectedCodeSeen
	if !sink.hasCode("opencode_session_event") {
		t.Fatalf("events = %+v, want replayed prompt admission native frame", sink.events)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartTurnClosesCommittedPromptWhenReplayCannotOpen(t *testing.T) {
	process := newFakeProcess()
	openErr := errors.New("durable replay unavailable")
	api := &fakeAPI{openEventErr: openErr}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	first := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)})
	if !errors.Is(first, openErr) {
		t.Fatalf("StartTurn() error = %v, want durable replay failure", first)
	}
	second := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "again", Context: json.RawMessage(`{}`)})
	if !errors.Is(second, openErr) || api.promptCalls != 1 || process.terminateCalls != 1 {
		t.Fatalf("second StartTurn() error = %v promptCalls=%d terminate=%d; want sticky replay failure and one cleanup", second, api.promptCalls, process.terminateCalls)
	}
}

func TestCloseCancelsInFlightDurableReplayOpenAfterPromptAdmission(t *testing.T) {
	process := newFakeProcess()
	openStarted := make(chan struct{})
	api := &fakeAPI{openEventStarted: openStarted}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)})
	}()
	<-openStarted
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-turnDone; err == nil || api.promptCalls != 1 || process.terminateCalls != 1 {
		t.Fatalf("StartTurn() error = %v promptCalls=%d terminate=%d; want cancelled committed turn cleanup", err, api.promptCalls, process.terminateCalls)
	}
}

func TestOpenFailureClosesAndBecomesSticky(t *testing.T) {
	process := newFakeProcess()
	api := &fakeAPI{healthErr: errors.New("not listening")}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	adapter.healthRetryWait = time.Millisecond
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, first := session.(harness.StagedSession).Open(timeoutContext)
	if first == nil || process.terminateCalls != 1 {
		t.Fatalf("Open() error = %v, terminate=%d", first, process.terminateCalls)
	}
	_, second := session.(harness.StagedSession).Open(context.Background())
	if second == nil || api.createCalls != 0 || process.terminateCalls != 1 {
		t.Fatalf("second Open() error = %v create=%d terminate=%d", second, api.createCalls, process.terminateCalls)
	}
}

func TestOpenPeerOwnershipFailureDoesNotRetryHealth(t *testing.T) {
	process := newFakeProcess()
	api := &fakeAPI{healthErr: fmt.Errorf("verify socket: %w", ErrPeerOwnership)}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = session.(harness.StagedSession).Open(ctx)
	if !errors.Is(err, ErrPeerOwnership) || ctx.Err() != nil || api.healthCalls != 1 || api.createCalls != 0 {
		t.Fatalf("Open() = %v; context=%v health=%d create=%d", err, ctx.Err(), api.healthCalls, api.createCalls)
	}
	if process.terminateCalls != 1 {
		t.Fatalf("ownership rejection terminated process %d times, want 1", process.terminateCalls)
	}
}

func TestCloseAndCancelShareOneInFlightInterruptAndTerminate(t *testing.T) {
	process := newFakeProcess()
	stream := newBlockingReadCloser()
	started := make(chan struct{})
	release := make(chan struct{})
	api := &fakeAPI{stream: stream, interruptStarted: started, interruptRelease: release}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cmd_1", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close(context.Background()) }()
	close(release)
	if err := <-controlDone; err != nil {
		t.Fatalf("Control() error = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if api.interruptCalls != 1 || process.terminateCalls != 1 || !stream.closed() {
		t.Fatalf("interrupt=%d terminate=%d streamClosed=%v", api.interruptCalls, process.terminateCalls, stream.closed())
	}
}

func TestCloseCancelsInFlightPromptAndPreventsSecondAdmission(t *testing.T) {
	process := newFakeProcess()
	stream := newBlockingReadCloser()
	promptStarted := make(chan struct{})
	api := &fakeAPI{stream: stream, promptStarted: promptStarted}
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		return process, nil
	}, api)
	session, err := adapter.Start(context.Background(), startRequest(t), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	turnDone := make(chan error, 1)
	go func() {
		turnDone <- staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "finish", Context: json.RawMessage(`{}`)})
	}()
	<-promptStarted
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-turnDone; err == nil {
		t.Fatal("StartTurn() succeeded after Close cancelled its in-flight prompt")
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "again", Context: json.RawMessage(`{}`)}); err == nil || api.promptCalls != 1 {
		t.Fatalf("second StartTurn() error = %v promptCalls=%d; want no retry", err, api.promptCalls)
	}
}

func TestSessionWatcherRejectsUnexpectedAdmissionIdentityBeforePublishingFrame(t *testing.T) {
	sessionContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &recordingSink{}
	eventContext, cancelEvents := context.WithCancel(sessionContext)
	defer cancelEvents()
	session := newNativeSession(sessionContext, cancel, eventContext, cancelEvents, sink, t.TempDir(), func(Config) (api, error) { return &fakeAPI{}, nil }, "opencode", "secret", func(int, string) ConnectionVerifier { return func(context.Context, net.Conn) error { return nil } }, time.Millisecond, time.Second)
	stream := &streamHandle{body: io.NopCloser(strings.NewReader("data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":1,\"version\":1},\"data\":{\"timestamp\":1,\"sessionID\":\"ses_native_1\",\"messageID\":\"msg_other\",\"prompt\":{\"text\":\"other\"},\"delivery\":\"steer\"}}\n\n"))}
	done := make(chan struct{})
	go session.watchSessionEvents(stream, done, "ses_native_1", "msg_expected")
	<-done
	sink.mu.Lock()
	events := append([]harness.Event(nil), sink.events...)
	sink.mu.Unlock()
	if !sink.hasDiagnostic("opencode_stream_error") || sink.hasKind(harness.EventNativeFrame) {
		t.Fatalf("events = %+v; want only stream failure diagnostic", sink.events)
	}
	for _, event := range events {
		if event.Code == "opencode_stream_error" {
			if event.Message != "OpenCode durable event stream failed; native identifiers withheld" {
				t.Fatalf("stream diagnostic message = %q, want fixed redacted message", event.Message)
			}
			if strings.Contains(event.Message, "ses_native_1") || strings.Contains(event.Message, "msg_other") || strings.Contains(event.Message, "msg_expected") {
				t.Fatalf("stream diagnostic leaked native identifier: %q", event.Message)
			}
		}
	}
}

func TestSessionWatcherPublishesKnownLifecycleAndDiagnosesUnknownEvents(t *testing.T) {
	sessionContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &recordingSink{}
	eventContext, cancelEvents := context.WithCancel(sessionContext)
	defer cancelEvents()
	session := newNativeSession(sessionContext, cancel, eventContext, cancelEvents, sink, t.TempDir(), func(Config) (api, error) { return &fakeAPI{}, nil }, "opencode", "secret", func(int, string) ConnectionVerifier { return func(context.Context, net.Conn) error { return nil } }, time.Millisecond, time.Second)
	stream := &streamHandle{body: io.NopCloser(strings.NewReader(strings.Join([]string{
		"data: {\"id\":\"evt_admission\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":1,\"version\":1},\"data\":{\"timestamp\":1,\"sessionID\":\"ses_native_1\",\"messageID\":\"msg_expected\",\"prompt\":{\"text\":\"one\"},\"delivery\":\"steer\"}}\n\n",
		"data: {\"id\":\"evt_step\",\"type\":\"session.next.step.started\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":2,\"version\":1},\"data\":{\"timestamp\":2,\"sessionID\":\"ses_native_1\",\"assistantMessageID\":\"msg_assistant\",\"agent\":\"build\",\"model\":{\"id\":\"model\",\"providerID\":\"provider\"}}}\n\n",
		"data: {\"id\":\"evt_tool\",\"type\":\"session.next.tool.called\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":3,\"version\":1},\"data\":{\"timestamp\":3,\"sessionID\":\"ses_native_1\",\"assistantMessageID\":\"msg_assistant\",\"callID\":\"call_1\",\"tool\":\"read\",\"input\":{\"path\":\"README.md\"},\"provider\":{\"executed\":true}}}\n\n",
		"data: {\"id\":\"evt_step_ended\",\"type\":\"session.next.step.ended\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":4,\"version\":2},\"data\":{\"timestamp\":4,\"sessionID\":\"ses_native_1\",\"assistantMessageID\":\"msg_assistant\",\"finish\":\"stop\",\"cost\":0,\"tokens\":{\"input\":1,\"output\":1,\"reasoning\":0,\"cache\":{\"read\":0,\"write\":0}}}}\n\n",
		"data: {\"id\":\"evt_unknown\",\"type\":\"session.next.future\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":5,\"version\":9},\"data\":{\"sessionID\":\"ses_native_1\"}}\n\n",
	}, "")))}
	done := make(chan struct{})
	go session.watchSessionEvents(stream, done, "ses_native_1", "msg_expected")
	<-done
	sink.mu.Lock()
	events := append([]harness.Event(nil), sink.events...)
	sink.mu.Unlock()
	var lifecycle []harness.Event
	var unknown []harness.Event
	for _, event := range events {
		if event.Code == "opencode_session_event" {
			lifecycle = append(lifecycle, event)
		}
		if event.Code == "opencode_unknown_session_event" {
			unknown = append(unknown, event)
		}
	}
	if len(lifecycle) != 4 {
		t.Fatalf("lifecycle events = %+v; want all four known events retained", events)
	}
	for index, event := range lifecycle {
		if event.Kind != harness.EventNativeFrame || event.Diagnostic || event.Sequence != uint64(index+1) {
			t.Fatalf("lifecycle event[%d] = %+v; want native frame sequence %d", index, event, index+1)
		}
	}
	if len(unknown) != 1 || unknown[0].Kind != harness.EventDiagnostic || !unknown[0].Diagnostic {
		t.Fatalf("unknown events = %+v; want one diagnostic-only event", unknown)
	}
	session.mu.Lock()
	streamErr := session.streamErr
	session.mu.Unlock()
	if !errors.Is(streamErr, errStreamEndedUnknown) {
		t.Fatalf("stream error = %v, want only EOF without a terminal mapping", streamErr)
	}
}

func TestStartRejectsUnsupportedOrUnsafeInputsBeforeLaunch(t *testing.T) {
	processStarted := false
	adapter := newTestAdapter(func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error) {
		processStarted = true
		return newFakeProcess(), nil
	}, &fakeAPI{})
	request := startRequest(t)
	request.Resume = &harness.ResumeHandle{}
	if _, err := adapter.Start(context.Background(), request, &recordingSink{}); !errors.Is(err, harness.ErrUnsupportedCapability) || processStarted {
		t.Fatalf("resume Start() error = %v started=%v", err, processStarted)
	}
	cap := "100"
	request = startRequest(t)
	request.Limits.MaxCostMicrousd = &cap
	if _, err := adapter.Start(context.Background(), request, &recordingSink{}); !errors.Is(err, harness.ErrUnsupportedCapability) || processStarted {
		t.Fatalf("cost Start() error = %v started=%v", err, processStarted)
	}
	request = startRequest(t)
	request.ModelProfile = "  profile is not mapped  "
	if _, err := adapter.Start(context.Background(), request, &recordingSink{}); err == nil || processStarted {
		t.Fatalf("model profile Start() error = %v started=%v", err, processStarted)
	}
	request = startRequest(t)
	request.Invocation.Args = []string{"--unsafe-unknown-flag"}
	if _, err := adapter.Start(context.Background(), request, &recordingSink{}); err == nil || processStarted {
		t.Fatalf("args Start() error = %v started=%v", err, processStarted)
	}
}

func TestCloseCancelsBlockedSSEPersistenceBeforeWaitCompletes(t *testing.T) {
	process := newFakeProcess()
	processContext, cancelProcess := context.WithCancel(context.Background())
	defer cancelProcess()
	eventContext, cancelEvents := context.WithCancel(processContext)
	defer cancelEvents()
	sink := newBlockingEventSink()
	session := newNativeSession(processContext, cancelProcess, eventContext, cancelEvents, sink, t.TempDir(), func(Config) (api, error) { return &fakeAPI{}, nil }, "opencode", "secret", func(int, string) ConnectionVerifier { return func(context.Context, net.Conn) error { return nil } }, time.Millisecond, time.Second)
	stream := &streamHandle{body: io.NopCloser(strings.NewReader("data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":1,\"version\":1},\"data\":{\"timestamp\":1,\"sessionID\":\"ses_native_1\",\"messageID\":\"msg_expected\",\"prompt\":{\"text\":\"one\"},\"delivery\":\"steer\"}}\n\n"))}
	session.mu.Lock()
	session.process = process
	session.stream = stream
	session.streamDone = make(chan struct{})
	streamDone := session.streamDone
	session.mu.Unlock()
	go session.watchSessionEvents(stream, streamDone, "ses_native_1", "msg_expected")
	<-sink.entered
	go session.watchProcess(process)
	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close(context.Background()) }()
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-sink.returned:
	default:
		t.Fatal("Close() returned before the cancelled SSE persistence call returned")
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultUnknown {
		t.Fatalf("Wait() = %+v, %v", result, err)
	}
}

type fixtureCommandRunner struct{ responses map[string][]byte }

func (runner fixtureCommandRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	return runner.responses[strings.Join(args, " ")], nil
}

type fakeAPI struct {
	mu                    sync.Mutex
	healthCalls           int
	createCalls           int
	promptCalls           int
	openEventCalls        int
	interruptCalls        int
	callSequence          []string
	eventAfter            []uint64
	healthErr             error
	createErr             error
	promptErr             error
	promptStarted         chan struct{}
	promptRelease         <-chan struct{}
	lastPrompt            PromptRequest
	lastPromptSession     string
	replayPromptAdmission bool
	openEventErr          error
	openEventStarted      chan struct{}
	openEventRelease      <-chan struct{}
	openEventContext      context.Context
	streamFactory         func(context.Context, string, uint64) io.ReadCloser
	interruptErr          error
	stream                io.ReadCloser
	interruptStarted      chan struct{}
	interruptRelease      <-chan struct{}
}

func (api *fakeAPI) Health(context.Context) error {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.healthCalls++
	return api.healthErr
}

func (api *fakeAPI) CreateSession(_ context.Context, request CreateSessionRequest) (SessionInfo, error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.createCalls++
	if api.createErr != nil {
		return SessionInfo{}, api.createErr
	}
	return SessionInfo{ID: request.ID, ProjectID: "prj_1", Location: request.Location}, nil
}

func (api *fakeAPI) Prompt(ctx context.Context, sessionID string, request PromptRequest) (PromptAdmission, error) {
	api.mu.Lock()
	api.promptCalls++
	api.callSequence = append(api.callSequence, "prompt")
	started, release, err := api.promptStarted, api.promptRelease, api.promptErr
	api.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return PromptAdmission{}, ctx.Err()
		}
	} else if started != nil {
		<-ctx.Done()
		return PromptAdmission{}, ctx.Err()
	}
	if err != nil {
		return PromptAdmission{}, err
	}
	api.mu.Lock()
	api.lastPrompt = request
	api.lastPromptSession = sessionID
	api.mu.Unlock()
	return PromptAdmission{AdmittedSeq: 1, ID: request.ID, SessionID: sessionID, Text: request.Text, Delivery: request.Delivery, TimeCreated: 1}, nil
}

func (api *fakeAPI) Interrupt(context.Context, string) error {
	api.mu.Lock()
	api.interruptCalls++
	started, release, err := api.interruptStarted, api.interruptRelease, api.interruptErr
	api.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	return err
}

func (api *fakeAPI) OpenSessionEvents(ctx context.Context, sessionID string, after uint64) (io.ReadCloser, error) {
	api.mu.Lock()
	api.openEventCalls++
	api.callSequence = append(api.callSequence, "events")
	api.eventAfter = append(api.eventAfter, after)
	started, release, err := api.openEventStarted, api.openEventRelease, api.openEventErr
	stream, streamFactory := api.stream, api.streamFactory
	replayPromptAdmission, prompt, promptSession := api.replayPromptAdmission, api.lastPrompt, api.lastPromptSession
	api.openEventContext = ctx
	api.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else if started != nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if streamFactory != nil {
		return streamFactory(ctx, sessionID, after), nil
	}
	if replayPromptAdmission {
		if promptSession != sessionID {
			return nil, ErrIdentityMismatch
		}
		return promptAdmissionReplayStream(sessionID, prompt), nil
	}
	if stream == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return stream, nil
}

func (api *fakeAPI) openEventContextCancelled() bool {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.openEventContext == nil || api.openEventContext.Err() != nil
}

func (api *fakeAPI) lastPromptRequest() PromptRequest {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.lastPrompt
}

func (api *fakeAPI) callOrder() []string {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]string(nil), api.callSequence...)
}

func (api *fakeAPI) eventCursors() []uint64 {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]uint64(nil), api.eventAfter...)
}

func promptAdmissionReplayStream(sessionID string, prompt PromptRequest) *replayBlockingReadCloser {
	payload, err := json.Marshal(map[string]any{
		"id":      "evt_1",
		"type":    "session.next.prompt.admitted",
		"durable": map[string]any{"aggregateID": sessionID, "seq": 1, "version": 1},
		"data": map[string]any{
			"timestamp": 1,
			"sessionID": sessionID,
			"messageID": prompt.ID,
			"prompt":    map[string]string{"text": prompt.Text},
			"delivery":  prompt.Delivery,
		},
	})
	if err != nil {
		panic(err)
	}
	return newReplayBlockingReadCloser([]byte("data: " + string(payload) + "\n\n"))
}

type fakeProcess struct {
	mu             sync.Mutex
	done           chan struct{}
	result         execution.Result
	terminated     bool
	terminateCalls int
}

func newFakeProcess() *fakeProcess { return &fakeProcess{done: make(chan struct{})} }

func (process *fakeProcess) Wait() execution.Result {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result
}

func (process *fakeProcess) Terminate(context.Context, time.Duration) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.terminateCalls++
	if !process.terminated {
		process.terminated = true
		process.result = execution.Result{PID: 123, Terminated: true}
		close(process.done)
	}
	return nil
}

func (process *fakeProcess) ProcessDetails() (int, string) { return 123, "created:123" }

func (process *fakeProcess) finish(result execution.Result) {
	process.mu.Lock()
	defer process.mu.Unlock()
	if !process.terminated {
		process.terminated = true
		process.result = result
		close(process.done)
	}
}

type blockingReadCloser struct {
	done       chan struct{}
	closedOnce sync.Once
}

type contextBlockingReadCloser struct {
	context     context.Context
	readStarted chan struct{}
	done        chan struct{}
	closedOnce  sync.Once
}

func newContextBlockingReadCloser(ctx context.Context) *contextBlockingReadCloser {
	return &contextBlockingReadCloser{context: ctx, readStarted: make(chan struct{}), done: make(chan struct{})}
}

func (reader *contextBlockingReadCloser) Read([]byte) (int, error) {
	close(reader.readStarted)
	select {
	case <-reader.context.Done():
		return 0, reader.context.Err()
	case <-reader.done:
		return 0, io.EOF
	}
}

func (reader *contextBlockingReadCloser) Close() error {
	reader.closedOnce.Do(func() { close(reader.done) })
	return nil
}

func (reader *contextBlockingReadCloser) contextCancelled() bool { return reader.context.Err() != nil }

type replayBlockingReadCloser struct {
	mu         sync.Mutex
	payload    []byte
	delivered  bool
	done       chan struct{}
	closedOnce sync.Once
}

func newReplayBlockingReadCloser(payload []byte) *replayBlockingReadCloser {
	return &replayBlockingReadCloser{payload: payload, done: make(chan struct{})}
}

func (reader *replayBlockingReadCloser) Read(data []byte) (int, error) {
	reader.mu.Lock()
	if !reader.delivered {
		reader.delivered = true
		count := copy(data, reader.payload)
		reader.mu.Unlock()
		return count, nil
	}
	reader.mu.Unlock()
	<-reader.done
	return 0, io.EOF
}

func (reader *replayBlockingReadCloser) Close() error {
	reader.closedOnce.Do(func() { close(reader.done) })
	return nil
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{done: make(chan struct{})}
}

func (reader *blockingReadCloser) Read([]byte) (int, error) { <-reader.done; return 0, io.EOF }
func (reader *blockingReadCloser) Close() error {
	reader.closedOnce.Do(func() { close(reader.done) })
	return nil
}
func (reader *blockingReadCloser) closed() bool {
	select {
	case <-reader.done:
		return true
	default:
		return false
	}
}

type recordingSink struct {
	mu               sync.Mutex
	events           []harness.Event
	expectedCode     string
	expectedCodeSeen chan struct{}
	expectedCodeOnce sync.Once
}

type blockingEventSink struct {
	entered  chan struct{}
	returned chan struct{}
	once     sync.Once
}

func newBlockingEventSink() *blockingEventSink {
	return &blockingEventSink{entered: make(chan struct{}), returned: make(chan struct{})}
}

func (sink *blockingEventSink) Handle(ctx context.Context, event harness.Event) error {
	if event.Kind != harness.EventNativeFrame {
		return nil
	}
	sink.once.Do(func() { close(sink.entered) })
	<-ctx.Done()
	close(sink.returned)
	return ctx.Err()
}

func (sink *recordingSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	expectedCode, expectedCodeSeen := sink.expectedCode, sink.expectedCodeSeen
	sink.mu.Unlock()
	if expectedCodeSeen != nil && event.Code == expectedCode {
		sink.expectedCodeOnce.Do(func() { close(expectedCodeSeen) })
	}
	return nil
}

func (sink *recordingSink) containsData(value string) bool {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if strings.Contains(string(event.Data), value) || strings.Contains(string(event.Payload), value) || strings.Contains(event.Message, value) {
			return true
		}
	}
	return false
}

func (sink *recordingSink) hasKind(kind harness.EventKind) bool {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func (sink *recordingSink) hasDiagnostic(code string) bool {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Kind == harness.EventDiagnostic && event.Code == code {
			return true
		}
	}
	return false
}

func (sink *recordingSink) hasCode(code string) bool {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Code == code {
			return true
		}
	}
	return false
}

func newTestAdapter(start processStarter, fake api) *Adapter {
	readyStart := func(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (nativeProcess, error) {
		process, err := start(ctx, invocation, sink)
		if isNilNativeProcess(process) {
			return nil, err
		}
		if err != nil {
			return process, err
		}
		if err := sink.Handle(ctx, execution.Event{Stream: execution.Stdout, Sequence: 2, Data: []byte("opencode server listening on http://127.0.0.1:43123\n")}); err != nil {
			return process, err
		}
		return process, nil
	}
	adapter := newAdapter("opencode-test", nil, readyStart, func(Config) (api, error) { return fake, nil }, func() (string, string, error) { return "opencode", "test-secret", nil }, func(int, string) ConnectionVerifier { return func(context.Context, net.Conn) error { return nil } })
	adapter.healthRetryWait = time.Millisecond
	adapter.terminationWait = time.Second
	return adapter
}

func startRequest(t *testing.T) harness.StartRequest {
	t.Helper()
	workspace := t.TempDir()
	return harness.StartRequest{Workspace: workspace, Invocation: execution.Invocation{Env: []string{"PATH=C:\\tools"}}}
}
