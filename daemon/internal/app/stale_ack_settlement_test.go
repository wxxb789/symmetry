package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestStaleCleanupWaitsForCommandAcknowledgementWithoutSendingOrdinaryOutbox(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	if _, err := store.SetWorkspacePath(key, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "00000000-0000-4000-8000-000000000010", Sequence: 1, Kind: "progress", OccurredAt: time.Now().UTC(), Payload: []byte(`{"text":"discard"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTransition(key, protocol.StateTransitionRequest{TransitionID: "00000000-0000-4000-8000-000000000011", State: "running", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: "cancel-1", Outcome: "failed", AckID: "00000000-0000-4000-8000-000000000012"}
	if _, err := store.QueueCommandAcknowledgement(key, acknowledgement); err != nil {
		t.Fatal(err)
	}
	journal, err := store.SetLocalState(key, "stale")
	if err != nil {
		t.Fatal(err)
	}
	journal = expireLeaseBeforeCleanup(t, store, key)

	var calls []string
	record := func(call string) {
		calls = append(calls, call)
	}
	controlClient := &staleAcknowledgementControl{fakeControl: &fakeControl{}, record: record}
	workspaceService := &staleAcknowledgementWorkspace{fakeWorkspace: &fakeWorkspace{}, record: record}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	active := &runningRun{nativeStartInFlight: true, slotHeld: true}
	app := &daemon{
		config:      testConfig(t),
		store:       store,
		control:     controlClient,
		workspace:   workspaceService,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       slots,
		outboxWake:  make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		options:     options{newID: ids(), clock: time.Now},
	}

	if err := app.scheduleCleanup(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if app.runningRun(key) != active {
		t.Fatal("cleanup released the stale run before its acknowledgement was delivered")
	}
	if len(slots) != 1 || !active.slotHeld {
		t.Fatalf("cleanup released capacity through the native start barrier: slots=%d active=%#v", len(slots), active)
	}
	app.mu.Lock()
	active.nativeStartInFlight = false
	app.mu.Unlock()
	if err := app.scheduleCleanup(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if len(slots) != 0 || active.slotHeld || app.runningRun(key) != active {
		t.Fatalf("stale acknowledgement did not release only the slot: slots=%d active=%#v", len(slots), active)
	}
	retained, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("cleanup lost stale acknowledgement: %#v", retained)
	}
	select {
	case <-app.outboxWake:
	default:
		t.Fatal("cleanup did not wake the stale acknowledgement outbox")
	}
	if len(calls) != 0 {
		t.Fatalf("cleanup performed effects before acknowledgement flush: %#v", calls)
	}

	if err := app.flushRun(context.Background(), retained); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("stale journal remained after acknowledgement and cleanup: %v", err)
	}
	if len(calls) != 2 || calls[0] != "ack" || calls[1] != "cleanup" {
		t.Fatalf("stale settlement order = %#v, want acknowledgement before cleanup", calls)
	}
	if controlClient.eventCalls != 0 || controlClient.transitionCalls != 0 {
		t.Fatalf("stale outbox sent ordinary traffic: events=%d transitions=%d", controlClient.eventCalls, controlClient.transitionCalls)
	}
}

func TestStaleAcknowledgementConclusiveErrorRetiresBeforeCleanup(t *testing.T) {
	for _, test := range []struct {
		name string
		code control.ErrorCode
	}{
		{name: "ownership lost", code: control.OwnershipLost},
		{name: "terminal grace expired", code: control.TerminalGraceExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			if _, err := store.SetWorkspacePath(key, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: "cancel-1", Outcome: "failed", AckID: "00000000-0000-4000-8000-000000000012"}
			if _, err := store.QueueCommandAcknowledgement(key, acknowledgement); err != nil {
				t.Fatal(err)
			}
			journal, err := store.SetLocalState(key, "stale")
			if err != nil {
				t.Fatal(err)
			}
			journal = expireLeaseBeforeCleanup(t, store, key)
			var calls []string
			record := func(call string) { calls = append(calls, call) }
			app := &daemon{
				config: testConfig(t),
				store:  store,
				control: &staleAcknowledgementControl{
					fakeControl: &fakeControl{},
					record:      record,
					ackErr: &control.APIError{
						StatusCode: http.StatusConflict,
						Code:       test.code,
						Message:    test.name,
					},
				},
				workspace: &staleAcknowledgementWorkspace{fakeWorkspace: &fakeWorkspace{}, record: record},
				log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running:   make(map[state.RunKey]*runningRun),
				options:   options{newID: ids(), clock: time.Now},
			}

			if err := app.flushRun(context.Background(), journal); err != nil {
				t.Fatalf("flush stale acknowledgement: %v", err)
			}
			if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
				t.Fatalf("conclusively retired stale acknowledgement blocked cleanup: %v", err)
			}
			if len(calls) != 2 || calls[0] != "ack" || calls[1] != "cleanup" {
				t.Fatalf("conclusive stale acknowledgement order = %#v", calls)
			}
		})
	}
}

type staleAcknowledgementControl struct {
	*fakeControl
	record          func(string)
	ackErr          error
	eventCalls      int
	transitionCalls int
}

func (client *staleAcknowledgementControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	client.eventCalls++
	client.record("events")
	return nil
}

func (client *staleAcknowledgementControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	client.transitionCalls++
	client.record("transition")
	return nil
}

func (client *staleAcknowledgementControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	client.record("ack")
	return client.ackErr
}

type staleAcknowledgementWorkspace struct {
	*fakeWorkspace
	record func(string)
}

func (service *staleAcknowledgementWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	service.record("cleanup")
	return nil
}
