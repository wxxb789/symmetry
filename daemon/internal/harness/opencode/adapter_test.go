package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	wantArgs := []string{"serve", "--hostname", "127.0.0.1", "--port", "43123", "--pure"}
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
	context, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &recordingSink{}
	session := newNativeSession(context, cancel, sink, t.TempDir(), &fakeAPI{}, time.Millisecond, time.Second)
	stream := &streamHandle{body: io.NopCloser(strings.NewReader("data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_native_1\",\"seq\":1,\"version\":1},\"data\":{\"timestamp\":1,\"sessionID\":\"ses_native_1\",\"messageID\":\"msg_other\",\"prompt\":{\"text\":\"other\"},\"delivery\":\"steer\"}}\n\n"))}
	done := make(chan struct{})
	go session.watchSessionEvents(stream, done, "ses_native_1", "msg_expected")
	<-done
	if !sink.hasDiagnostic("opencode_stream_error") || sink.hasKind(harness.EventNativeFrame) {
		t.Fatalf("events = %+v; want only stream failure diagnostic", sink.events)
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
}

type fixtureCommandRunner struct{ responses map[string][]byte }

func (runner fixtureCommandRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	return runner.responses[strings.Join(args, " ")], nil
}

type fakeAPI struct {
	mu               sync.Mutex
	healthCalls      int
	createCalls      int
	promptCalls      int
	openEventCalls   int
	interruptCalls   int
	healthErr        error
	createErr        error
	promptErr        error
	promptStarted    chan struct{}
	promptRelease    <-chan struct{}
	interruptErr     error
	stream           io.ReadCloser
	interruptStarted chan struct{}
	interruptRelease <-chan struct{}
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

func (api *fakeAPI) OpenSessionEvents(context.Context, string, uint64) (io.ReadCloser, error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.openEventCalls++
	if api.stream == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return api.stream, nil
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
	mu     sync.Mutex
	events []harness.Event
}

func (sink *recordingSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.events = append(sink.events, event)
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

func newTestAdapter(start processStarter, fake api) *Adapter {
	adapter := newAdapter("opencode-test", nil, start, func(Config) (api, error) { return fake, nil }, func() (int, error) { return 43123, nil }, func() (string, string, error) { return "opencode", "test-secret", nil })
	adapter.healthRetryWait = time.Millisecond
	adapter.terminationWait = time.Second
	return adapter
}

func startRequest(t *testing.T) harness.StartRequest {
	t.Helper()
	workspace := t.TempDir()
	return harness.StartRequest{Workspace: workspace, Invocation: execution.Invocation{Env: []string{"PATH=C:\\tools"}}}
}
