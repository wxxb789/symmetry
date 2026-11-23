package codex

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type cancelAttemptState uint8

const (
	cancelAttemptIntent cancelAttemptState = iota + 1
	cancelAttemptWritten
	cancelAttemptAccepted
	cancelAttemptFailed
)

type cancelAttempt struct {
	id                  uint64
	state               cancelAttemptState
	done                chan struct{}
	err                 error
	rpcID               uint64
	terminalInterrupted bool
	responseObserved    bool
	doneClosed          bool
}

type pendingInterruptedTerminal struct {
	attemptID uint64
	frame     Frame
	completed turnCompletedParams
	result    protocol.TaskResult
	rawResult json.RawMessage
	parseErr  error
}

// cancelTransition names the tracker transitions cancelViolationHook reports on (nil outside tests).
type cancelTransition uint8

const (
	cancelMarkWritten cancelTransition = iota + 1
	cancelAccept
	cancelReject
	cancelFail
	cancelFinish
)

type interruptRPCOutcome uint8

const (
	interruptRPCFailed interruptRPCOutcome = iota
	interruptRPCSuspended
	interruptRPCApplied
)

var cancelViolationHook func(transition cancelTransition, attemptID uint64)

func cancelViolation(transition cancelTransition, attemptID uint64) {
	if cancelViolationHook != nil {
		cancelViolationHook(transition, attemptID)
	}
}

// cancelTracker's methods never lock: every caller holds nativeSession.mutex.
type cancelTracker struct {
	pendingCancelAttempts map[uint64]uint64
	cancelRequested       bool
	nextCancelAttempt     uint64
	cancelAttempts        map[uint64]*cancelAttempt
	cancelFlight          *cancelAttempt
	deferredTerminal      *pendingInterruptedTerminal
}

func newCancelTracker() cancelTracker {
	return cancelTracker{
		pendingCancelAttempts: make(map[uint64]uint64),
		cancelAttempts:        make(map[uint64]*cancelAttempt),
	}
}

func hasWrittenCancelAttempt(tracker *cancelTracker) bool {
	for _, attempt := range tracker.cancelAttempts {
		if attempt != nil && (attempt.state == cancelAttemptWritten || attempt.state == cancelAttemptAccepted) {
			return true
		}
	}
	return false
}

func newPendingInterruptedTerminal(frame Frame, completed turnCompletedParams, result protocol.TaskResult, rawResult json.RawMessage, parseErr error) *pendingInterruptedTerminal {
	return &pendingInterruptedTerminal{
		frame:     frame,
		completed: completed,
		result:    result,
		rawResult: rawResult,
		parseErr:  parseErr,
	}
}

func (tracker *cancelTracker) acquire() (uint64, bool, <-chan struct{}) {
	if tracker.cancelFlight != nil {
		return tracker.cancelFlight.id, false, tracker.cancelFlight.done
	}
	if tracker.cancelAttempts == nil {
		tracker.cancelAttempts = make(map[uint64]*cancelAttempt)
	}
	tracker.nextCancelAttempt++
	attempt := &cancelAttempt{id: tracker.nextCancelAttempt, done: make(chan struct{}), state: cancelAttemptIntent}
	// The attempt id is stored in the map key so the state remains stable even
	// after the shared flight has been cleared.
	tracker.cancelAttempts[attempt.id] = attempt
	tracker.cancelFlight = attempt
	return attempt.id, true, attempt.done
}

func (tracker *cancelTracker) bindRPC(rpcID uint64) {
	if tracker.cancelFlight == nil {
		return
	}
	tracker.pendingCancelAttempts[rpcID] = tracker.cancelFlight.id
	if attempt := tracker.cancelAttempts[tracker.pendingCancelAttempts[rpcID]]; attempt != nil {
		attempt.rpcID = rpcID
	}
}

func (tracker *cancelTracker) forgetRPC(rpcID uint64) uint64 {
	attemptID := tracker.pendingCancelAttempts[rpcID]
	delete(tracker.pendingCancelAttempts, rpcID)
	return attemptID
}

func (tracker *cancelTracker) observeResponse(attemptID uint64) {
	if attempt := tracker.cancelAttempts[attemptID]; attempt != nil {
		attempt.responseObserved = true
	}
}

func (tracker *cancelTracker) markWritten(id uint64) {
	attempt := tracker.cancelAttempts[id]
	if attempt == nil || attempt.state != cancelAttemptIntent {
		cancelViolation(cancelMarkWritten, id)
		return
	}
	attempt.state = cancelAttemptWritten
	tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
}

func (tracker *cancelTracker) accept(id uint64) {
	attempt := tracker.cancelAttempts[id]
	if attempt == nil || attempt.state != cancelAttemptWritten {
		cancelViolation(cancelAccept, id)
	}
	if attempt != nil && attempt.state == cancelAttemptWritten {
		attempt.state = cancelAttemptAccepted
	}
	tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
}

func (tracker *cancelTracker) reject(id uint64) (*pendingInterruptedTerminal, harness.ResultKind) {
	attempt := tracker.cancelAttempts[id]
	if attempt == nil || (attempt.state != cancelAttemptIntent && attempt.state != cancelAttemptWritten) {
		cancelViolation(cancelReject, id)
		tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
		return nil, harness.ResultFailed
	}
	attempt.state = cancelAttemptFailed
	tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
	return tracker.takeDeferredTerminal(id)
}

func (tracker *cancelTracker) fail(id uint64) (*pendingInterruptedTerminal, harness.ResultKind) {
	attempt := tracker.cancelAttempts[id]
	if attempt == nil {
		cancelViolation(cancelFail, id)
		return nil, harness.ResultFailed
	}
	if attempt.state != cancelAttemptIntent {
		if attempt.state != cancelAttemptWritten {
			cancelViolation(cancelFail, id)
		}
		return nil, harness.ResultFailed
	}
	attempt.state = cancelAttemptFailed
	tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
	return tracker.takeDeferredTerminal(id)
}

func (tracker *cancelTracker) finish(id uint64, err error) {
	attempt := tracker.cancelAttempts[id]
	if attempt == nil {
		cancelViolation(cancelFinish, id)
		return
	}
	if attempt.doneClosed {
		return
	}
	if err != nil && attempt.state == cancelAttemptIntent {
		attempt.state = cancelAttemptFailed
	}
	tracker.cancelRequested = hasWrittenCancelAttempt(tracker)
	attempt.err = err
	if tracker.cancelFlight == attempt {
		tracker.cancelFlight = nil
	}
	attempt.doneClosed = true
	close(attempt.done)
}

func (tracker *cancelTracker) deferTerminal(terminal *pendingInterruptedTerminal) bool {
	var attempt *cancelAttempt
	if tracker.cancelFlight != nil {
		candidate := tracker.cancelAttempts[tracker.cancelFlight.id]
		if candidate != nil && candidate.rpcID != 0 && !candidate.responseObserved && (candidate.state == cancelAttemptIntent || candidate.state == cancelAttemptWritten) {
			attempt = candidate
		}
	}
	if attempt == nil {
		for _, candidate := range tracker.cancelAttempts {
			if candidate == nil || candidate.rpcID == 0 || candidate.responseObserved || (candidate.state != cancelAttemptIntent && candidate.state != cancelAttemptWritten) {
				continue
			}
			if attempt == nil || candidate.id > attempt.id {
				attempt = candidate
			}
		}
	}
	if attempt == nil {
		return false
	}
	attempt.terminalInterrupted = true
	terminal.attemptID = attempt.id
	tracker.deferredTerminal = terminal
	return true
}

func (tracker *cancelTracker) takeDeferredTerminal(id uint64) (*pendingInterruptedTerminal, harness.ResultKind) {
	if tracker.deferredTerminal == nil || tracker.deferredTerminal.attemptID != id {
		return nil, harness.ResultFailed
	}
	terminal := tracker.deferredTerminal
	tracker.deferredTerminal = nil
	if hasWrittenCancelAttempt(tracker) {
		return terminal, harness.ResultCancelled
	}
	return terminal, harness.ResultFailed
}

func (tracker *cancelTracker) reset() {
	tracker.pendingCancelAttempts = make(map[uint64]uint64)
	tracker.deferredTerminal = nil
}

func (tracker *cancelTracker) requested() bool {
	return tracker.cancelRequested
}

func (tracker *cancelTracker) resolvePendingInterrupt(rpcID uint64, status string) (uint64, interruptRPCOutcome) {
	attemptID := tracker.pendingCancelAttempts[rpcID]
	attempt := tracker.cancelAttempts[attemptID]
	if status == "interrupted" && attempt != nil && (attempt.state == cancelAttemptIntent || attempt.state == cancelAttemptWritten) {
		attempt.terminalInterrupted = true
		return attemptID, interruptRPCSuspended
	}
	if status == "interrupted" && attempt != nil && (attempt.state == cancelAttemptWritten || attempt.state == cancelAttemptAccepted) {
		return attemptID, interruptRPCApplied
	}
	return attemptID, interruptRPCFailed
}

func (session *nativeSession) acquireCancelAttempt() (uint64, bool, <-chan struct{}) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.cancelTracker.acquire()
}

func (session *nativeSession) beginCancelAttempt() uint64 {
	id, _, _ := session.acquireCancelAttempt()
	return id
}

func (session *nativeSession) markCancelAttemptWritten(id uint64) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	session.cancelTracker.markWritten(id)
}

func (session *nativeSession) failCancelAttempt(id uint64) {
	session.mutex.Lock()
	terminal, kind := session.cancelTracker.fail(id)
	session.mutex.Unlock()
	if terminal != nil {
		session.settlePendingInterruptedTerminal(terminal, kind)
	}
}

func (session *nativeSession) acceptCancelAttempt(id uint64) {
	session.mutex.Lock()
	session.cancelTracker.accept(id)
	session.mutex.Unlock()
}

func (session *nativeSession) rejectCancelAttempt(id uint64) {
	session.mutex.Lock()
	terminal, kind := session.cancelTracker.reject(id)
	session.mutex.Unlock()
	if terminal != nil {
		session.settlePendingInterruptedTerminal(terminal, kind)
	}
}

func (session *nativeSession) finishCancelAttempt(id uint64, err error) {
	session.mutex.Lock()
	session.cancelTracker.finish(id, err)
	session.mutex.Unlock()
}

func (session *nativeSession) executeCancelAttempt(ctx context.Context, attemptID uint64, threadID, turnID string) error {
	beforeInterrupt := func() error {
		session.mutex.Lock()
		defer session.mutex.Unlock()
		if session.terminalSeen || session.result != nil {
			return errNativeTurnTerminal
		}
		return nil
	}
	afterInterrupt := func() {
		session.markCancelAttemptWritten(attemptID)
	}
	return session.callWithHooks(ctx, appServerMethodTurnInterrupt, turnInterruptParams{ThreadID: threadID, TurnID: turnID}, nil, beforeInterrupt, afterInterrupt)
}

func isExplicitCancelRejection(err error) bool {
	var rpcErr nativeRPCError
	return errors.As(err, &rpcErr)
}

func (session *nativeSession) waitCancelAttempt(ctx context.Context, attemptID uint64, done <-chan struct{}) error {
	if ctx == nil {
		return errors.New("Codex cancel context must not be nil")
	}
	select {
	case <-done:
		session.mutex.Lock()
		attempt := session.cancelTracker.cancelAttempts[attemptID]
		var err error
		if attempt != nil {
			err = attempt.err
		}
		session.mutex.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-session.context.Done():
		return session.sessionError()
	}
}

func (session *nativeSession) performCancelAttempt(ctx context.Context, threadID, turnID string) error {
	attemptID, owner, done := session.acquireCancelAttempt()
	var interruptErr error
	if owner {
		interruptErr = session.executeCancelAttempt(ctx, attemptID, threadID, turnID)
		if interruptErr != nil {
			if isExplicitCancelRejection(interruptErr) {
				session.rejectCancelAttempt(attemptID)
			} else {
				session.failCancelAttempt(attemptID)
			}
		} else {
			session.acceptCancelAttempt(attemptID)
		}
		session.finishCancelAttempt(attemptID, interruptErr)
	} else {
		interruptErr = session.waitCancelAttempt(ctx, attemptID, done)
	}
	return interruptErr
}

func (session *nativeSession) deferInterruptedTerminal(terminal *pendingInterruptedTerminal) bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.cancelTracker.deferTerminal(terminal)
}

func (session *nativeSession) failDeferredCancelOnProcessExit() {
	session.mutex.Lock()
	if session.cancelTracker.deferredTerminal == nil {
		session.mutex.Unlock()
		return
	}
	terminal := session.cancelTracker.deferredTerminal
	attempt := session.cancelTracker.cancelAttempts[terminal.attemptID]
	if attempt == nil || (attempt.state != cancelAttemptIntent && attempt.state != cancelAttemptWritten) {
		session.mutex.Unlock()
		return
	}
	if attempt.state == cancelAttemptIntent {
		attempt.state = cancelAttemptFailed
		attempt.err = errors.New("Codex process exited before the cancel request write completed")
	} else if attempt.err == nil {
		attempt.err = errors.New("Codex process exited before the cancel response completed")
	}
	session.cancelTracker.cancelRequested = hasWrittenCancelAttempt(&session.cancelTracker)
	if session.cancelTracker.cancelFlight == attempt {
		session.cancelTracker.cancelFlight = nil
	}
	terminal, kind := session.cancelTracker.takeDeferredTerminal(attempt.id)
	if !attempt.doneClosed {
		attempt.doneClosed = true
		close(attempt.done)
	}
	session.mutex.Unlock()
	if terminal != nil {
		session.settlePendingInterruptedTerminal(terminal, kind)
	}
}
