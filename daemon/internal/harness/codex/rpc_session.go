package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type retiredRPC struct {
	method          string
	cancelAttemptID uint64
}

// rpcResolution is the correlator's answer for one response id: the waiter still
// registered for it, or the retired record that keeps a late response classifiable.
type rpcResolution struct {
	waiter        chan rpcResponse
	method        string
	cancelAttempt uint64
	found         bool
	retired       bool
}

// rpcCorrelator owns JSON-RPC request ids, their waiters and the retired-id
// window. It never locks: every caller holds nativeSession.mutex.
type rpcCorrelator struct {
	pending        map[uint64]chan rpcResponse
	pendingMethods map[uint64]string
	retiredIDs     map[uint64]retiredRPC
	retiredOrder   []uint64
	nextID         uint64
}

func newRPCCorrelator() rpcCorrelator {
	return rpcCorrelator{
		pending:        make(map[uint64]chan rpcResponse),
		pendingMethods: make(map[uint64]string),
		retiredIDs:     make(map[uint64]retiredRPC),
	}
}

func (rpc *rpcCorrelator) nextRequestID() uint64 {
	return atomic.AddUint64(&rpc.nextID, 1)
}

func (rpc *rpcCorrelator) register(id uint64, method string, waiter chan rpcResponse) {
	rpc.pending[id] = waiter
	rpc.pendingMethods[id] = method
}

func (rpc *rpcCorrelator) resolve(id uint64) rpcResolution {
	if waiter, found := rpc.pending[id]; found {
		method := rpc.pendingMethods[id]
		delete(rpc.pending, id)
		delete(rpc.pendingMethods, id)
		return rpcResolution{waiter: waiter, method: method, found: true}
	}
	if retired, found := rpc.retiredIDs[id]; found {
		return rpcResolution{method: retired.method, cancelAttempt: retired.cancelAttemptID, retired: true}
	}
	return rpcResolution{}
}

func (rpc *rpcCorrelator) forget(id uint64) {
	delete(rpc.pending, id)
	delete(rpc.pendingMethods, id)
}

func (rpc *rpcCorrelator) retire(id uint64, metadata retiredRPC) {
	if id == 0 {
		return
	}
	if _, exists := rpc.retiredIDs[id]; exists {
		return
	}
	rpc.retiredIDs[id] = metadata
	rpc.retiredOrder = append(rpc.retiredOrder, id)
	if len(rpc.retiredOrder) <= maxRetiredRPCIDs {
		return
	}
	oldest := rpc.retiredOrder[0]
	rpc.retiredOrder[0] = 0
	rpc.retiredOrder = rpc.retiredOrder[1:]
	delete(rpc.retiredIDs, oldest)
}

func (rpc *rpcCorrelator) reset() {
	rpc.pending = make(map[uint64]chan rpcResponse)
	rpc.pendingMethods = make(map[uint64]string)
	rpc.retiredIDs = make(map[uint64]retiredRPC)
	rpc.retiredOrder = nil
}

type stagedNotification struct {
	frame     Frame
	message   rpcEnvelope
	dedupeKey string
}

func (session *nativeSession) handleFrame(ctx context.Context, frame Frame) error {
	if frame.Kind == FrameDiagnostic {
		if frame.IsFramingError() {
			cause := errors.New(frame.DiagnosticError)
			if frame.DiagnosticError == "" {
				cause = errors.New(frame.DiagnosticCode)
			}
			return session.failFraming(execution.Event{
				Stream:   execution.Stdout,
				Sequence: frame.Sequence,
				At:       time.Now().UTC(),
			}, cause)
		}
		return session.emit(frame.Event())
	}
	var message rpcEnvelope
	if err := json.Unmarshal(frame.Raw, &message); err != nil {
		return session.diagnostic(frame, "invalid_rpc_envelope", "native RPC envelope could not be decoded")
	}
	if message.Method != "" && message.ID != nil {
		return session.handleServerRequest(frame, message)
	}
	if message.Method != "" {
		return session.handleNotification(ctx, frame, message)
	}
	if message.ID != nil {
		return session.handleResponseWithOutputLock(frame, message, true)
	}
	return session.diagnostic(frame, "unknown_native_event", "native frame is neither a request, response, nor notification")
}

func (session *nativeSession) handleServerRequest(frame Frame, message rpcEnvelope) error {
	if !validServerRequestID(message.ID) {
		return session.diagnostic(frame, "invalid_server_request_id", "server request id is not a supported JSON-RPC identifier")
	}
	reply, err := json.Marshal(rpcErrorResponse{
		JSONRPC: jsonRPCVersion,
		ID:      append(json.RawMessage(nil), message.ID...),
		Error: rpcError{
			Code:    -32601,
			Message: "unsupported by Symmetry Codex adapter",
		},
	})
	if err != nil {
		return fmt.Errorf("encode unsupported Codex server request response: %w", err)
	}
	if err := session.writeInput(session.context, append(reply, '\n')); err != nil {
		diagnosticErr := session.diagnostic(frame, "server_request_reply_failed", "could not reject unsupported Codex server request")
		return errors.Join(fmt.Errorf("reply to unsupported Codex server request: %w", err), diagnosticErr)
	}
	return session.diagnostic(frame, "unsupported_server_request", "Codex server request is unsupported and was rejected")
}

func (session *nativeSession) handleResponseWithOutputLock(frame Frame, message rpcEnvelope, outputLocked bool) error {
	hasResult := hasRPCValue(message.Result)
	hasError := hasRPCValue(message.Error)
	if hasResult == hasError {
		return session.failFraming(execution.Event{
			Stream:   execution.Stdout,
			Sequence: frame.Sequence,
			At:       time.Now().UTC(),
		}, errors.New("native JSON-RPC response must contain exactly one of result or error"))
	}
	id, ok := parseRPCID(message.ID)
	if !ok {
		return session.failFraming(execution.Event{
			Stream:   execution.Stdout,
			Sequence: frame.Sequence,
			At:       time.Now().UTC(),
		}, errors.New("native response id is not a positive integer"))
	}
	var rpcErrorErr error
	if hasError {
		_, rpcErrorErr = parseNativeRPCError(message.Error)
	}
	session.mutex.Lock()
	resolution := session.rpc.resolve(id)
	var deferredTerminal *pendingInterruptedTerminal
	var deferredKind harness.ResultKind
	attemptID := resolution.cancelAttempt
	settle := resolution.found && resolution.method == appServerMethodTurnInterrupt
	if resolution.found {
		attemptID = session.cancelTracker.forgetRPC(id)
	} else if resolution.retired && resolution.method == appServerMethodTurnInterrupt && attemptID != 0 {
		settle = true
	}
	if settle {
		session.cancelTracker.observeResponse(attemptID)
		if hasError && rpcErrorErr == nil {
			deferredTerminal, deferredKind = session.cancelTracker.reject(attemptID)
		} else if hasError {
			deferredTerminal, deferredKind = session.cancelTracker.takeDeferredTerminal(attemptID)
		} else if hasResult && validateRPCResult(resolution.method, message.Result) == nil {
			session.cancelTracker.accept(attemptID)
			deferredTerminal, deferredKind = session.cancelTracker.takeDeferredTerminal(attemptID)
		}
	}
	session.mutex.Unlock()
	if deferredTerminal != nil {
		if outputLocked {
			session.settlePendingInterruptedTerminalLocked(deferredTerminal, deferredKind)
		} else {
			session.settlePendingInterruptedTerminal(deferredTerminal, deferredKind)
		}
	}
	if !resolution.found {
		if resolution.retired {
			return session.diagnostic(frame, "late_response", "native response arrived after its request was terminally resolved")
		}
		return session.failFraming(execution.Event{
			Stream:   execution.Stdout,
			Sequence: frame.Sequence,
			At:       time.Now().UTC(),
		}, errors.New("native response does not match an outstanding request"))
	}
	response := rpcResponse{result: append(json.RawMessage(nil), message.Result...), err: append(json.RawMessage(nil), message.Error...)}
	if hasResult {
		if resultErr := validateRPCResult(resolution.method, message.Result); resultErr != nil {
			response = rpcResponse{failure: resultErr}
		}
	}
	select {
	case resolution.waiter <- response:
	default:
		return session.failFraming(execution.Event{
			Stream:   execution.Stdout,
			Sequence: frame.Sequence,
			At:       time.Now().UTC(),
		}, errors.New("native response was already delivered"))
	}
	return nil
}

func (session *nativeSession) handleNotification(ctx context.Context, frame Frame, message rpcEnvelope) error {
	return session.handleNotificationWithStaging(ctx, frame, message, true, "")
}

func (session *nativeSession) handleNotificationWithStaging(ctx context.Context, frame Frame, message rpcEnvelope, allowStaging bool, preDedupeKey string) error {
	if allowStaging {
		staged, err := session.stageTurnNotification(frame, message)
		if err != nil {
			if errors.Is(err, errNotificationOverflow) {
				return session.failNotificationOverflow(frame)
			}
			return session.failPendingTurnNotificationOverflow(frame)
		}
		if staged {
			return nil
		}
	}
	if message.Method != appServerEventTurnCompleted && session.hasTerminalResult() {
		return session.diagnostic(frame, "late_native_event", "native notification arrived after the terminal Codex turn event")
	}
	switch message.Method {
	case appServerEventThreadStarted:
		var params threadStartedParams
		if err := json.Unmarshal(message.Params, &params); err != nil || params.Thread.ID == "" {
			return session.diagnostic(frame, "unrelated_native_event", "thread/started has no usable native thread id")
		}
		if session.stageThreadStarted(params.Thread.ID, message.Params) {
			// A native thread can announce itself before thread/start returns the
			// opaque handle. It stays local until Open has crossed that barrier.
			return nil
		}
		if !session.matchesThread(params.Thread.ID) {
			return session.diagnostic(frame, "unrelated_native_event", "thread/started does not match the active Codex thread")
		}
		return nil
	case appServerEventAgentMessageDelta:
		var params agentMessageDeltaParams
		if err := json.Unmarshal(message.Params, &params); err != nil || !session.matches(params.ThreadID, params.TurnID) {
			return session.diagnostic(frame, "unrelated_native_event", "agent message delta does not match the active Codex turn")
		}
		// Delta notifications have no stable native event ID. Identical text can
		// legitimately occur consecutively, so content-based deduplication would
		// silently corrupt the emitted transcript.
		return session.emit(harness.Event{Kind: harness.EventMessageDelta, Sequence: frame.Sequence, Data: []byte(params.Delta)})
	case appServerEventThreadTokenUsage:
		var params threadTokenUsageParams
		if err := json.Unmarshal(message.Params, &params); err != nil || !session.matches(params.ThreadID, params.TurnID) {
			return session.diagnostic(frame, "unrelated_native_event", "token usage update does not match the active Codex turn")
		}
		usagePayload, err := normalizeTokenUsage(params.TokenUsage)
		if err != nil {
			return session.diagnostic(frame, "unverified_usage", "Codex token usage counters are not verified: "+err.Error())
		}
		seen, markErr := session.markNotificationDeduplicated(frame, preDedupeKey, notificationDigestKey(message.Method, params.ThreadID, params.TurnID, message.Params))
		if markErr != nil {
			return markErr
		}
		if !seen {
			return nil
		}
		// Counters are normalized without native identity; billing semantics remain
		// unknown until independently verified.
		return session.emit(harness.Event{Kind: harness.EventUsageObserved, Sequence: frame.Sequence, Payload: usagePayload})
	case appServerEventTurnStarted:
		var params turnStartedParams
		if err := json.Unmarshal(message.Params, &params); err != nil || !session.matches(params.ThreadID, params.Turn.ID) {
			return session.diagnostic(frame, "unrelated_native_event", "turn/started does not match the active Codex turn")
		}
		seen, markErr := session.markNotificationDeduplicated(frame, preDedupeKey, turnStartedDedupeKey(params.ThreadID, params.Turn.ID))
		if markErr != nil {
			return markErr
		}
		if !seen {
			return nil
		}
		return session.emit(harness.Event{Kind: harness.EventNativeFrame, Sequence: frame.Sequence})
	case appServerEventItemStarted, appServerEventItemCompleted:
		threadID, turnID, item, ok := itemNotificationIdentity(message.Params)
		if !ok || !session.matches(threadID, turnID) {
			return session.diagnostic(frame, "unrelated_native_event", "native lifecycle event does not match the active Codex turn")
		}
		itemID, validItem := nativeItemID(item)
		if !validItem {
			return session.diagnostic(frame, "malformed_native_item", "native item lifecycle event has no item id")
		}
		seen, markErr := session.markNotificationDeduplicated(frame, preDedupeKey, itemLifecycleDedupeKey(message.Method, threadID, turnID, itemID))
		if markErr != nil {
			return markErr
		}
		if !seen {
			return nil
		}
		return session.emit(harness.Event{Kind: itemLifecycleEventKind(message.Method, item), Sequence: frame.Sequence})
	case appServerEventError:
		var params nativeErrorNotificationParams
		if err := json.Unmarshal(message.Params, &params); err != nil || !session.matches(params.ThreadID, params.TurnID) {
			return session.diagnostic(frame, "unrelated_native_event", "native error does not match the active Codex turn")
		}
		seen, markErr := session.markNotificationDeduplicated(frame, preDedupeKey, notificationDigestKey(message.Method, params.ThreadID, params.TurnID, message.Params))
		if markErr != nil {
			return markErr
		}
		if !seen {
			return nil
		}
		session.rememberNativeFailureReason(classifyNativeError(params.Error.CodexErrorInfo))
		return session.emit(harness.Event{Kind: harness.EventDiagnostic, Sequence: frame.Sequence, Diagnostic: true, Code: "native_error", Message: "Codex app-server reported a native error"})
	case appServerEventTurnCompleted:
		return session.handleTurnCompleted(frame, message.Params, preDedupeKey)
	default:
		return session.diagnostic(frame, "unknown_native_event", "native notification method is not supported by this Codex adapter")
	}
}

func (session *nativeSession) stageTurnNotification(frame Frame, message rpcEnvelope) (bool, error) {
	if !isTurnNotification(message.Method) {
		return false, nil
	}
	threadID, ok := notificationThreadID(message.Params)
	if !ok {
		return false, nil
	}
	dedupeKey := notificationDedupeKey(message)
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.terminalSeen || (!session.turnStarting && !session.turnReplaying) || threadID != session.threadID {
		return false, nil
	}
	if dedupeKey != "" {
		seen, err := session.markNotificationLocked(dedupeKey)
		if err != nil {
			return true, err
		}
		if !seen {
			return true, nil
		}
	}
	if len(session.pendingTurnEvents) >= maxPendingTurnEvents || session.pendingTurnBytes+len(frame.Raw) > maxPendingTurnBytes {
		session.terminalSeen = true
		session.pendingTurnEvents = nil
		session.pendingTurnBytes = 0
		session.turnReplaying = false
		return true, errPendingTurnEventOverflow
	}
	stored := Frame{
		Kind:            frame.Kind,
		Sequence:        frame.Sequence,
		Raw:             append(json.RawMessage(nil), frame.Raw...),
		Method:          frame.Method,
		ID:              append(json.RawMessage(nil), frame.ID...),
		Diagnostic:      frame.Diagnostic,
		DiagnosticCode:  frame.DiagnosticCode,
		DiagnosticError: frame.DiagnosticError,
	}
	session.pendingTurnEvents = append(session.pendingTurnEvents, stagedNotification{
		frame:     stored,
		dedupeKey: dedupeKey,
		message: rpcEnvelope{
			JSONRPC: message.JSONRPC,
			Method:  message.Method,
			Params:  append(json.RawMessage(nil), message.Params...),
		},
	})
	session.pendingTurnBytes += len(stored.Raw)
	return true, nil
}

func (session *nativeSession) drainPendingTurnNotifications(ctx context.Context) error {
	for {
		session.mutex.Lock()
		if len(session.pendingTurnEvents) == 0 {
			session.turnReplaying = false
			session.replayOwnedByWatcher = false
			closeReplayBarrierLocked(session)
			session.mutex.Unlock()
			return nil
		}
		notification := session.pendingTurnEvents[0]
		session.pendingTurnBytes -= len(notification.frame.Raw)
		session.pendingTurnEvents[0] = stagedNotification{}
		session.pendingTurnEvents = session.pendingTurnEvents[1:]
		session.mutex.Unlock()

		if err := session.handleNotificationWithStaging(ctx, notification.frame, notification.message, false, notification.dedupeKey); err != nil {
			session.mutex.Lock()
			session.pendingTurnEvents = nil
			session.pendingTurnBytes = 0
			session.turnReplaying = false
			session.replayOwnedByWatcher = false
			closeReplayBarrierLocked(session)
			session.mutex.Unlock()
			return err
		}
	}
}

func closeReplayBarrierLocked(session *nativeSession) {
	if session.turnReplayDone != nil {
		select {
		case <-session.turnReplayDone:
		default:
			close(session.turnReplayDone)
		}
	}
}

func (session *nativeSession) waitForTurnReplay(ctx context.Context, replayDone <-chan struct{}) error {
	timer := time.NewTimer(processCleanupTimeout)
	defer timer.Stop()
	select {
	case <-replayDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-session.context.Done():
		return session.sessionError()
	case <-timer.C:
		return errors.New("Codex turn replay did not settle before process cleanup deadline")
	}
}

// preparePendingTurnReplayAfterProcessExit adopts a terminal notification that
// arrived before turn/start's response. The response waiter will still observe
// the process shutdown, but a valid buffered terminal event is authoritative
// enough to settle the native turn before missing_result is synthesized.
func (session *nativeSession) preparePendingTurnReplayAfterProcessExit() bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.closed || !session.staged.OpenedLocked() || !session.turnStarting || len(session.pendingTurnEvents) == 0 {
		return false
	}
	turnID := ""
	for _, pending := range session.pendingTurnEvents {
		var identity struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(pending.message.Params, &identity); err != nil || identity.ThreadID != session.threadID {
			return false
		}
		candidate := identity.TurnID
		if candidate == "" {
			candidate = identity.Turn.ID
		}
		if candidate == "" {
			return false
		}
		if turnID == "" {
			turnID = candidate
		} else if turnID != candidate {
			return false
		}
		if pending.message.Method == appServerEventTurnCompleted {
			var completed turnCompletedParams
			if err := json.Unmarshal(pending.message.Params, &completed); err != nil || completed.Turn.ID != turnID || (completed.Turn.Status != "completed" && completed.Turn.Status != "interrupted" && completed.Turn.Status != "failed") {
				return false
			}
		}
	}
	if turnID == "" {
		return false
	}
	session.turnStarting = false
	session.turnStarted = true
	session.turnID = turnID
	session.turnReplaying = true
	session.replayOwnedByWatcher = true
	session.turnReplayDone = make(chan struct{})
	return true
}

func (session *nativeSession) failPendingTurnNotificationOverflow(frame Frame) error {
	session.mutex.Lock()
	if session.overflowReported {
		session.mutex.Unlock()
		return errPendingTurnEventOverflow
	}
	session.overflowReported = true
	session.terminalSeen = true
	session.pendingTurnEvents = nil
	session.pendingTurnBytes = 0
	session.turnReplaying = false
	session.mutex.Unlock()
	reason := protocol.TaskResultReasonUnknownOutcome
	session.complete(harness.TaskResult{
		Kind:         harness.ResultFailed,
		Summary:      errPendingTurnEventOverflow.Error(),
		Reason:       &reason,
		Process:      session.partialProcessResult(),
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: frame.Sequence,
	})
	session.cancel()
	return errPendingTurnEventOverflow
}

func (session *nativeSession) handleTurnCompleted(frame Frame, raw json.RawMessage, preDedupeKey string) error {
	var completed turnCompletedParams
	if err := json.Unmarshal(raw, &completed); err != nil || !session.matches(completed.ThreadID, completed.Turn.ID) {
		return session.diagnostic(frame, "unrelated_native_event", "turn/completed does not match the active Codex turn")
	}
	seen, markErr := session.markNotificationDeduplicated(frame, preDedupeKey, terminalDedupeKey(completed.ThreadID, completed.Turn.ID))
	if markErr != nil {
		return markErr
	}
	if !seen {
		return session.diagnostic(frame, "duplicate_terminal_event", "Codex turn already has a terminal result")
	}
	if !session.markTerminalSeen() {
		return session.diagnostic(frame, "duplicate_terminal_event", "Codex turn already has a terminal result")
	}
	if completed.Turn.Error != nil {
		session.rememberNativeFailureReason(classifyNativeError(completed.Turn.Error.CodexErrorInfo))
	}
	session.resolvePendingCallsAfterTerminal(completed.Turn.Status)

	result, rawResult, err := completed.taskResult()
	terminal := newPendingInterruptedTerminal(frame, completed, result, append(json.RawMessage(nil), rawResult...), err)
	if completed.Turn.Status == "interrupted" && session.deferInterruptedTerminal(terminal) {
		// The terminal event itself is enough to permit the daemon's bounded
		// close. Cancel acknowledgement classification remains deferred and is
		// resolved by the final physical Wait after process shutdown.
		session.signalTurnObserved(nil)
		return nil
	}
	err = session.publishTurnTerminal(terminal, nil)
	if err != nil {
		session.signalTurnObserved(err)
	}
	return err
}

func (session *nativeSession) settlePendingInterruptedTerminal(terminal *pendingInterruptedTerminal, kind harness.ResultKind) {
	session.outputMutex.Lock()
	session.settlePendingInterruptedTerminalLocked(terminal, kind)
	session.outputMutex.Unlock()
}

func (session *nativeSession) settlePendingInterruptedTerminalLocked(terminal *pendingInterruptedTerminal, kind harness.ResultKind) {
	_ = session.publishTurnTerminal(terminal, &kind)

	session.mutex.Lock()
	attempt := session.cancelTracker.cancelAttempts[terminal.attemptID]
	var waiter chan rpcResponse
	if attempt != nil && attempt.rpcID != 0 {
		if pending, ok := session.rpc.pending[attempt.rpcID]; ok {
			waiter = pending
			session.rpc.forget(attempt.rpcID)
			session.cancelTracker.forgetRPC(attempt.rpcID)
			session.rpc.retire(attempt.rpcID, retiredRPC{method: appServerMethodTurnInterrupt, cancelAttemptID: terminal.attemptID})
		}
	}
	session.mutex.Unlock()
	if waiter != nil {
		response := rpcResponse{failure: errNativeTurnTerminal}
		if kind == harness.ResultCancelled {
			response = rpcResponse{result: json.RawMessage(`{}`)}
		}
		select {
		case waiter <- response:
		default:
		}
	}
}

func (session *nativeSession) publishTurnTerminal(terminal *pendingInterruptedTerminal, forcedKind *harness.ResultKind) error {
	frame := terminal.frame
	completed := terminal.completed
	if terminal.parseErr != nil {
		kind := session.resultKindForTurn(completed.Turn.Status)
		if forcedKind != nil {
			kind = *forcedKind
		}
		failure := harness.TaskResult{
			Kind:         completedTerminalKind(completed.Turn.Status, kind),
			Summary:      missingResultSummary(completed.Turn.Status, terminal.parseErr),
			Process:      session.partialProcessResult(),
			Usage:        harness.Usage{State: harness.UsageUnknown},
			EventCount:   atomic.LoadUint64(&session.eventCount),
			LastSequence: frame.Sequence,
		}
		if failure.Kind == harness.ResultFailed {
			failure.Reason = session.failureReasonOr(protocol.TaskResultReasonMissingResult)
		} else if failure.Kind == harness.ResultCancelled {
			reason := protocol.TaskResultReasonCancelled
			failure.Reason = &reason
		}
		session.completeTerminal(failure)
		return session.emit(harness.Event{Kind: harness.EventDiagnostic, Sequence: frame.Sequence, Diagnostic: true, Code: "missing_result", Message: terminal.parseErr.Error()})
	}

	semantic := session.resultKindForTurn(completed.Turn.Status)
	if forcedKind != nil {
		semantic = *forcedKind
	}
	if completed.Turn.Status == "completed" && terminal.result.Kind == protocol.TaskResultFailed {
		semantic = harness.ResultFailed
	}
	taskResult := harness.TaskResult{
		Kind:         semantic,
		Summary:      terminal.result.Summary,
		Semantic:     &terminal.result,
		Reason:       terminal.result.Reason,
		Process:      session.partialProcessResult(),
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: frame.Sequence,
	}
	if forcedKind != nil && *forcedKind == harness.ResultFailed && taskResult.Reason == nil {
		reason := protocol.TaskResultReasonUnknownOutcome
		taskResult.Reason = &reason
	}
	session.emitMutex.Lock()
	defer session.emitMutex.Unlock()
	if err := session.emitLocked(harness.Event{Kind: harness.EventTaskResult, Sequence: frame.Sequence, Payload: terminal.rawResult}); err != nil {
		return err
	}
	session.completeTerminal(taskResult)
	return nil
}

func (session *nativeSession) resolvePendingCallsAfterTerminal(status string) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	for id, waiter := range session.rpc.pending {
		method := session.rpc.pendingMethods[id]
		response := rpcResponse{failure: errNativeTurnTerminal}
		switch method {
		case appServerMethodTurnInterrupt:
			attemptID, outcome := session.cancelTracker.resolvePendingInterrupt(id, status)
			if outcome == interruptRPCSuspended {
				continue
			}
			if outcome == interruptRPCApplied {
				response = rpcResponse{result: json.RawMessage(`{}`)}
			}
			session.rpc.retire(id, retiredRPC{method: method, cancelAttemptID: attemptID})
		case appServerMethodTurnStart:
			response = rpcResponse{result: json.RawMessage(fmt.Sprintf(`{"turn":{"id":%q,"status":"inProgress"}}`, session.turnID))}
			session.rpc.retire(id, retiredRPC{method: method})
		default:
			session.rpc.retire(id, retiredRPC{method: method})
		}
		session.rpc.forget(id)
		session.cancelTracker.forgetRPC(id)
		select {
		case waiter <- response:
		default:
		}
	}
}

func (session *nativeSession) call(ctx context.Context, method string, params any, destination any) error {
	return session.callWithHooks(ctx, method, params, destination, nil, nil)
}

func (session *nativeSession) callWithHooks(ctx context.Context, method string, params any, destination any, beforeWrite func() error, afterWrite func()) error {
	if ctx == nil {
		return errors.New("Codex RPC context must not be nil")
	}
	if err := session.ensureUsable(false); err != nil {
		return err
	}
	id := session.rpc.nextRequestID()
	waiter := make(chan rpcResponse, 1)
	session.mutex.Lock()
	if session.closed {
		session.mutex.Unlock()
		return errNativeSessionClosed
	}
	session.rpc.register(id, method, waiter)
	if method == appServerMethodTurnInterrupt {
		session.cancelTracker.bindRPC(id)
	}
	session.mutex.Unlock()

	request, err := json.Marshal(rpcRequest{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: params})
	if err != nil {
		session.removePending(id)
		return fmt.Errorf("encode %s: %w", method, err)
	}
	if beforeWrite != nil {
		if err := beforeWrite(); err != nil {
			session.removePending(id)
			return err
		}
	}
	if err := session.writeInput(ctx, append(request, '\n')); err != nil {
		session.retirePending(id)
		return fmt.Errorf("write %s: %w", method, err)
	}
	if afterWrite != nil {
		afterWrite()
	}
	select {
	case response := <-waiter:
		return session.finishRPCResponse(method, destination, response)
	default:
	}
	select {
	case response := <-waiter:
		return session.finishRPCResponse(method, destination, response)
	case <-ctx.Done():
		session.retirePending(id)
		return ctx.Err()
	case <-session.context.Done():
		session.retirePending(id)
		return session.sessionError()
	}
}

func (session *nativeSession) finishRPCResponse(method string, destination any, response rpcResponse) error {
	if response.failure != nil {
		return response.failure
	}
	if hasRPCValue(response.err) {
		return decodeRPCError(method, response.err)
	}
	if !hasRPCValue(response.result) {
		return fmt.Errorf("%s response is missing result", method)
	}
	if destination != nil {
		if err := json.Unmarshal(response.result, destination); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
	}
	return nil
}

func (session *nativeSession) notify(ctx context.Context, method string) error {
	if err := session.ensureUsable(false); err != nil {
		return err
	}
	request, err := json.Marshal(rpcNotification{JSONRPC: jsonRPCVersion, Method: method})
	if err != nil {
		return err
	}
	if err := session.writeInput(ctx, append(request, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", method, err)
	}
	return nil
}

func (session *nativeSession) writeInput(ctx context.Context, input []byte) error {
	if ctx == nil {
		return errors.New("Codex RPC write context must not be nil")
	}
	session.mutex.Lock()
	process := session.process
	session.mutex.Unlock()
	if process == nil {
		return errProcessOutputBeforeReady
	}
	return process.WriteInputContext(ctx, input)
}

func (session *nativeSession) stageThreadStarted(threadID string, raw json.RawMessage) bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if !session.staged.OpeningLocked() || session.staged.OpenedLocked() {
		return false
	}
	if _, duplicate := session.pendingThreadStarts[threadID]; duplicate {
		return true
	}
	// Only one thread/start is active. Keep a small bound for unsolicited
	// pre-response metadata so it cannot grow without limit.
	if len(session.pendingThreadStarts) >= 4 {
		return true
	}
	session.pendingThreadStarts[threadID] = append(json.RawMessage(nil), raw...)
	return true
}

func (session *nativeSession) markNotification(key string) (bool, error) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.markNotificationLocked(key)
}

func (session *nativeSession) markNotificationLocked(key string) (bool, error) {
	if _, seen := session.seenNotifications[key]; seen {
		return false, nil
	}
	keyBytes := len([]byte(key))
	if len(session.seenNotifications) >= maxSeenNotifications || keyBytes > maxSeenNotificationBytes || session.seenNotificationBytes+keyBytes > maxSeenNotificationBytes {
		return false, errNotificationOverflow
	}
	session.seenNotifications[key] = struct{}{}
	session.seenNotificationBytes += keyBytes
	return true, nil
}

func (session *nativeSession) markNotificationForReplay(preDedupeKey, key string) (bool, error) {
	if preDedupeKey != "" && preDedupeKey == key {
		return true, nil
	}
	return session.markNotification(key)
}

func (session *nativeSession) markNotificationDeduplicated(frame Frame, preDedupeKey, key string) (bool, error) {
	seen, err := session.markNotificationForReplay(preDedupeKey, key)
	if err != nil {
		return false, session.failNotificationOverflow(frame)
	}
	return seen, nil
}

func (session *nativeSession) failNotificationOverflow(frame Frame) error {
	session.mutex.Lock()
	session.terminalSeen = true
	session.pendingTurnEvents = nil
	session.pendingTurnBytes = 0
	session.turnReplaying = false
	session.replayOwnedByWatcher = false
	closeReplayBarrierLocked(session)
	session.mutex.Unlock()
	reason := protocol.TaskResultReasonUnknownOutcome
	session.complete(harness.TaskResult{
		Kind:         harness.ResultFailed,
		Summary:      errNotificationOverflow.Error(),
		Reason:       &reason,
		Process:      session.partialProcessResult(),
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: frame.Sequence,
	})
	session.cancel()
	return errNotificationOverflow
}

func (session *nativeSession) removePending(id uint64) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	session.rpc.forget(id)
	session.cancelTracker.forgetRPC(id)
}

func (session *nativeSession) retirePending(id uint64) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	method := session.rpc.pendingMethods[id]
	session.rpc.forget(id)
	attemptID := session.cancelTracker.forgetRPC(id)
	session.rpc.retire(id, retiredRPC{method: method, cancelAttemptID: attemptID})
}
