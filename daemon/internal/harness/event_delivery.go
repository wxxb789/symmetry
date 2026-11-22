package harness

import (
	"context"
	"time"
)

// Stamps a missing event time on a copy, leaving the caller's copy untouched; the sink
// error is returned unwrapped so each adapter's "persist <adapter> native event: %w" text stays byte-identical.
func DeliverEvent(ctx context.Context, sink EventSink, event Event) error {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	return sink.Handle(ctx, event)
}
