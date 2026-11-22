package harness

import (
	"context"
	"sync"
	"time"
)

// PermanentCloseFailure builds the bare marker a close attempt returns when its
// failure must stay sticky. A wrapped marker stays retryable.
func PermanentCloseFailure(err error) error { return permanentCloseFailure{Err: err} }

// Classified as permanent only when returned bare: a wrapped or joined marker stays a
// retryable failure, and a nil Err is retryable too.
type permanentCloseFailure struct{ Err error }

func (failure permanentCloseFailure) Error() string {
	if failure.Err == nil {
		return ""
	}
	return failure.Err.Error()
}

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

// CloseGate runs at most one in-flight shutdown: the first caller runs it, and a
// successful or permanentCloseFailure attempt is permanent; plain failures stay retryable.
type CloseGate struct {
	mutex     sync.Mutex
	attempt   *closeAttempt
	succeeded bool
	permanent error
}

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

// Cleanup runs on its own goroutine under context.WithoutCancel plus budget, so a caller
// that leaves cannot abandon it; start runs on the caller before that goroutine starts.
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

// Only the bare marker is classified as permanent; the attempt slot is cleared only when
// it still holds this attempt, so a retry after a failure is never cleared by the earlier one.
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
