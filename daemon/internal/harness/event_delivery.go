package harness

import (
	"context"
	"time"
)

// DeliverEvent stamps a missing event time on its copy of the event and
// forwards it to the session's sink, leaving the caller's copy untouched. The
// sink error is returned unwrapped: each adapter keeps its own
// "persist <adapter> native event: %w" literal at the call site, so the text
// stays byte-identical, and keeps its own counters, serialization lock and
// sink-failure state.
func DeliverEvent(ctx context.Context, sink EventSink, event Event) error {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	return sink.Handle(ctx, event)
}
