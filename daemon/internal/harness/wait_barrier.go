package harness

import "context"

// WaitResult waits for a session's terminal barrier and then returns the value
// that read produces; the adapter performs that read under its own lock. When
// ctx ends first, WaitResult returns cancelled plus ctx.Err(), so each adapter
// keeps the value it reports for its own cancellation.
func WaitResult(ctx context.Context, terminal <-chan struct{}, read func() (TaskResult, error), cancelled TaskResult) (TaskResult, error) {
	select {
	case <-terminal:
		return read()
	case <-ctx.Done():
		return cancelled, ctx.Err()
	}
}

// WaitTurnBarrier waits for a native turn's terminal barrier. A settled turn
// wins over an ended session context; otherwise sessionErr reports the
// session's own error. turnErr and sessionErr run under the adapter's lock.
func WaitTurnBarrier(ctx context.Context, sessionContext context.Context, turnDone <-chan struct{}, turnErr func() error, sessionErr func() error) error {
	select {
	case <-turnDone:
		return turnErr()
	case <-ctx.Done():
		return ctx.Err()
	case <-sessionContext.Done():
		select {
		case <-turnDone:
			return turnErr()
		default:
		}
		return sessionErr()
	}
}
