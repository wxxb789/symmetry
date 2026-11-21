package harness

import (
	"context"
	"sync"
	"time"
)

// permanentCloseFailure marks a close attempt whose failure must stay sticky:
// the session stores the inner error and answers every later Close with it
// because the underlying cleanup is not retryable. CloseGate is its only
// interpreter, so the marker stays unexported and no import site can wrap or
// fabricate permanence. An attempt signals the marker by returning it bare;
// the gate classifies only that object, so a wrapped or joined marker is an
// ordinary retryable failure. Err must be non-nil; a marker with a nil error
// counts as a retryable failure.
type permanentCloseFailure struct{ Err error }

// Error reports the wrapped attempt failure.
func (failure permanentCloseFailure) Error() string {
	if failure.Err == nil {
		return ""
	}
	return failure.Err.Error()
}

// Unwrap exposes the wrapped attempt failure.
func (failure permanentCloseFailure) Unwrap() error { return failure.Err }

type closeAttempt struct {
	done chan struct{}
	err  error
}

type closeGateMode int

const (
	closeGateRun closeGateMode = iota
	closeGateJoin
	closeGateClosed
)

// CloseGate owns one session's "at most one in-flight shutdown" protocol. The
// first caller runs the attempt, concurrent callers join it, a successful
// attempt is permanent, a plain failure stays retryable, and a
// permanentCloseFailure failure is permanent.
type CloseGate struct {
	mutex     sync.Mutex
	attempt   *closeAttempt
	succeeded bool
	permanent error
}

// Run runs attempt as the gate's single in-flight shutdown on the calling
// goroutine with the caller's context.
func (gate *CloseGate) Run(ctx context.Context, attempt func(context.Context) error) error {
	entry, mode, closedErr := gate.begin()
	switch mode {
	case closeGateClosed:
		return closedErr
	case closeGateJoin:
		return waitCloseAttempt(ctx, entry)
	}
	err := attempt(ctx)
	gate.finish(entry, err)
	return entry.err
}

// RunDetached runs attempt on its own goroutine under a cleanup context derived
// from the caller's context (context.WithoutCancel plus budget), so a caller
// that leaves on cancellation cannot abandon bounded cleanup. It returns the
// attempt's error, or the caller's context error when the caller leaves first.
// start, when non-nil, runs on the calling goroutine after the gate decided
// that this caller owns the attempt and before the cleanup goroutine starts, so
// a caller-side close fence is still set when RunDetached returns.
func (gate *CloseGate) RunDetached(ctx context.Context, budget time.Duration, start func(), attempt func(context.Context) error) error {
	entry, mode, closedErr := gate.begin()
	switch mode {
	case closeGateClosed:
		return closedErr
	case closeGateJoin:
		return waitCloseAttempt(ctx, entry)
	}
	if start != nil {
		start()
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	go func() {
		err := attempt(cleanupContext)
		cleanupCancel()
		gate.finish(entry, err)
	}()
	return waitCloseAttempt(ctx, entry)
}

// begin decides who runs the shutdown: the caller (closeGateRun), an attempt to
// join (closeGateJoin), or nothing further because the session is already
// closed (closeGateClosed, with the error to return).
func (gate *CloseGate) begin() (*closeAttempt, closeGateMode, error) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.succeeded {
		return nil, closeGateClosed, nil
	}
	if gate.permanent != nil {
		return nil, closeGateClosed, gate.permanent
	}
	if attempt := gate.attempt; attempt != nil {
		return attempt, closeGateJoin, nil
	}
	entry := &closeAttempt{done: make(chan struct{})}
	gate.attempt = entry
	return entry, closeGateRun, nil
}

// finish publishes one attempt's outcome to every waiting caller. Only the
// bare marker is classified as permanent, so a wrapped marker keeps its full
// error and stays retryable. The attempt slot is cleared only when it still
// holds this attempt, so a retry that began after a failure is never cleared
// by the earlier one.
func (gate *CloseGate) finish(entry *closeAttempt, err error) {
	permanent := false
	if failure, ok := err.(permanentCloseFailure); ok && failure.Err != nil {
		err, permanent = failure.Err, true
	}
	gate.mutex.Lock()
	entry.err = err
	switch {
	case err == nil:
		gate.succeeded = true
	case permanent:
		gate.permanent = err
	}
	if gate.attempt == entry {
		gate.attempt = nil
	}
	close(entry.done)
	gate.mutex.Unlock()
}

func waitCloseAttempt(ctx context.Context, entry *closeAttempt) error {
	select {
	case <-entry.done:
		return entry.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
