package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

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
		"sandbox":           map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{thread.Params.CWD}, "excludeTmpdirEnvVar": true, "excludeSlashTmp": true},
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
		session.rpc.retire(id, retiredRPC{})
		session.mutex.Unlock()
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if len(session.rpc.retiredIDs) != maxRetiredRPCIDs {
		t.Fatalf("retired RPC IDs = %d, want bound %d", len(session.rpc.retiredIDs), maxRetiredRPCIDs)
	}
	if _, retained := session.rpc.retiredIDs[1]; retained {
		t.Fatal("oldest retired RPC ID was not evicted")
	}
	if _, retained := session.rpc.retiredIDs[maxRetiredRPCIDs+1]; !retained {
		t.Fatal("newest retired RPC ID was evicted")
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
	case <-time.After(testHangGuard):
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
	pending := len(native.rpc.pending)
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
	pending := len(native.rpc.pending)
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
	case <-time.After(testHangGuard):
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
	case <-time.After(testHangGuard):
		t.Fatal("initialized notification did not enter blocked write")
	}
	if err := <-opened; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want Open deadline", err)
	}
	_ = session.Close(context.Background())
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

func TestSanitizedLifecycleFixturePreservesNativeInterleaving(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "codex", TestedVersion, "app-server-lifecycle.jsonl"))
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
