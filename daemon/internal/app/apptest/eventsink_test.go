package apptest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestEventSinkHandleRecordsDetachedCopies(t *testing.T) {
	ctx := context.Background()
	sink := &EventSink{}
	data := []byte("event-data")
	payload := json.RawMessage(`{"payload":true}`)
	event := harness.Event{
		Kind:       harness.EventOutput,
		Stream:     "stream-1",
		Sequence:   3,
		Data:       data,
		Payload:    payload,
		Diagnostic: true,
		Code:       "code-1",
		Message:    "message-1",
	}
	if err := sink.Handle(ctx, event); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := sink.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	data[0] = 'X'
	payload[0] = 'X'
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("Events() length = %d, want 1", len(events))
	}
	if string(events[0].Data) != "event-data" {
		t.Fatalf("Events()[0].Data = %q, want %q", events[0].Data, "event-data")
	}
	if string(events[0].Payload) != `{"payload":true}` {
		t.Fatalf("Events()[0].Payload = %q, want %q", events[0].Payload, `{"payload":true}`)
	}
	if events[0].Kind != harness.EventOutput || events[0].Stream != "stream-1" || events[0].Sequence != 3 || !events[0].Diagnostic || events[0].Code != "code-1" || events[0].Message != "message-1" {
		t.Fatalf("Events()[0] = %#v, want the delivered event", events[0])
	}
	events[0].Data[0] = 'Y'
	events[0].Payload[0] = 'Y'
	events[0].Kind = harness.EventDiagnostic
	events[0] = harness.Event{}
	again := sink.Events()
	if len(again) != 1 {
		t.Fatalf("Events() length = %d, want 1 after mutating a previous copy", len(again))
	}
	if string(again[0].Data) != "event-data" || string(again[0].Payload) != `{"payload":true}` || again[0].Kind != harness.EventOutput {
		t.Fatalf("Events()[0] = %#v after mutating a previous copy, want the delivered event", again[0])
	}
}

func TestEventSinkCountCountsHandledEvents(t *testing.T) {
	ctx := context.Background()
	sink := &EventSink{}
	if got := sink.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
	if err := sink.Handle(ctx, harness.Event{Kind: harness.EventOutput}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if err := sink.Handle(ctx, harness.Event{Kind: harness.EventDiagnostic}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := sink.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
}

func TestEventSinkOnHandleReplacesDeliveryError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("sentinel")
	sink := &EventSink{}
	hookContext, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	var receivedContext context.Context
	var receivedEvent harness.Event
	sink.OnHandle = func(hookCtx context.Context, event harness.Event) error {
		receivedContext, receivedEvent = hookCtx, event
		return sentinel
	}
	event := harness.Event{Kind: harness.EventOutput, Data: []byte("hook-data")}
	if err := sink.Handle(hookContext, event); !errors.Is(err, sentinel) {
		t.Fatalf("Handle() error = %v, want the hook sentinel", err)
	}
	if receivedContext != hookContext {
		t.Fatal("Handle() did not forward the context to the hook")
	}
	if receivedEvent.Kind != harness.EventOutput || string(receivedEvent.Data) != "hook-data" {
		t.Fatalf("OnHandle received %#v, want the delivered event", receivedEvent)
	}
}
