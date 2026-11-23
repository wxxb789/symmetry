package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// testHangGuard bounds how long a test waits for a scripted, in-process double
// to publish an observable. It only turns a hung harness into a failure; it is
// never a statement about how fast the adapter should be.
const testHangGuard = 30 * time.Second

func reasonPointer(reason protocol.TaskResultReason) *protocol.TaskResultReason {
	return &reason
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
	process.emitJSON(t, statusChangedNotification("idle"))
	process.reply(t, initialize.ID, initializeReply())
	initialized := process.nextNotification(t)
	if initialized.Method != appServerMethodInitialized || initialized.Params != nil {
		t.Fatalf("initialized notification = %+v", initialized)
	}
	thread := process.nextRequest(t)
	if thread.Method != appServerMethodThreadStart || thread.Params.CWD == "" || thread.Params.Ephemeral || thread.Params.Sandbox != nativeEngineeringSandbox || thread.Params.ApprovalPolicy != nativeApprovalOnRequest {
		t.Fatalf("thread/start request = %+v", thread)
	}
	if len(thread.Params.Config) != 5 ||
		string(thread.Params.Config["sandbox_workspace_write.writable_roots"]) != "[]" ||
		string(thread.Params.Config["sandbox_workspace_write.network_access"]) != "false" ||
		string(thread.Params.Config["sandbox_workspace_write.exclude_tmpdir_env_var"]) != "true" ||
		string(thread.Params.Config["sandbox_workspace_write.exclude_slash_tmp"]) != "true" ||
		string(thread.Params.Config["features.shell_snapshot"]) != "false" {
		t.Fatalf("thread/start did not request the exact workspace-only sandbox: %v", thread.Params.Config)
	}
	process.reply(t, thread.ID, map[string]any{
		"approvalPolicy":    nativeApprovalOnRequest,
		"approvalsReviewer": "user",
		"cwd":               thread.Params.CWD,
		"model":             "gpt-6-astra",
		"modelProvider":     "openai",
		"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{}, "excludeTmpdirEnvVar": true, "excludeSlashTmp": true},
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
	if !ok || subject["$ref"] != "#/$defs/Subject" {
		t.Fatalf("turn/start output schema must reference the canonical complete subject: %#v", turn.Params.OutputSchema)
	}
	required, ok := turn.Params.OutputSchema["required"].([]any)
	if !ok || !containsString(required, "subject") {
		t.Fatalf("turn/start output schema does not require subject: %#v", turn.Params.OutputSchema["required"])
	}
	process.reply(t, turn.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}})
	if err := <-errResult; err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
}

func fakeAdapter(process *fakeNativeProcess) *Adapter {
	return adapterFor(process, process)
}

func adapterFor(process nativeProcess, sinkTarget *fakeNativeProcess) *Adapter {
	return &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			sinkTarget.sink = sink
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

func initializeReply() map[string]any {
	return map[string]any{
		"userAgent":      "codex-test",
		"codexHome":      "C:/redacted",
		"platformFamily": "windows",
		"platformOs":     "windows",
	}
}

func turnStartedNotification() map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": appServerEventTurnStarted,
		"params": map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "items": []any{}, "status": "inProgress"}},
	}
}

func statusChangedNotification(status string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": "remoteControl/status/changed",
		"params": map[string]any{"status": status},
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
		Config         map[string]json.RawMessage
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
	case <-time.After(testHangGuard):
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
	case <-time.After(testHangGuard):
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
	case <-time.After(testHangGuard):
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
