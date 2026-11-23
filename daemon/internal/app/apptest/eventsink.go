package apptest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

// EventSink records every delivered event copy and lets OnHandle fail
// delivery. The recorded copy is detached from the caller's buffers so a
// recycled read buffer cannot corrupt a recorded event.
type EventSink struct {
	OnHandle func(context.Context, harness.Event) error

	mu     sync.Mutex
	events []harness.Event
}

func (sink *EventSink) Handle(ctx context.Context, event harness.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, copyEvent(event))
	hook := sink.OnHandle
	sink.mu.Unlock()
	if hook != nil {
		return hook(ctx, event)
	}
	return nil
}

func copyEvent(event harness.Event) harness.Event {
	event.Data = append([]byte(nil), event.Data...)
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	return event
}

func (sink *EventSink) Events() []harness.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	events := make([]harness.Event, 0, len(sink.events))
	for _, event := range sink.events {
		events = append(events, copyEvent(event))
	}
	return events
}

func (sink *EventSink) Count() int {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return len(sink.events)
}
