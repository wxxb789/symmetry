package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestAtomicTerminalAcknowledgementUnknownWriteSurvivesDeliveredTransition(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	controlClient := &terminalAcknowledgementInterleavingControl{fakeControl: &fakeControl{}, acknowledgementAccepted: make(chan struct{})}
	result := harness.TaskResult{Kind: harness.ResultFailed, Summary: "native terminal"}
	active := &runningRun{
		nativeFinalResult:            &result,
		nativeCancelCommandID:        "cancel-1",
		nativeCancelTerminalObserved: true,
		nativeTerminalOwner:          true,
		terminalizing:                1,
		cleanupBlocked:               true,
	}
	flushDone := make(chan error, 1)
	var app *daemon
	app = &daemon{
		config:  testConfig(t),
		store:   store,
		control: controlClient,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			queueTerminalTransitionAndAcknowledgement: func(runKey state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, at time.Time) (state.RunJournal, error) {
				journal, err := store.QueueTerminalTransitionAndAcknowledgementAt(runKey, transition, acknowledgement, at)
				if err != nil {
					return state.RunJournal{}, err
				}
				go func() { flushDone <- app.flushRun(context.Background(), journal) }()
				select {
				case <-controlClient.acknowledgementAccepted:
				case <-time.After(2 * time.Second):
					return state.RunJournal{}, errors.New("outbox did not accept the terminal before unknown write return")
				}
				return state.RunJournal{}, errors.New("injected post-write unknown terminal pair")
			},
		},
	}

	app.completeNativeRunAfterUsage(context.Background(), key, active, result, nil, nil, false)
	if active.nativeTerminalOwner || active.terminalizing != 0 {
		t.Fatalf("unknown atomic pair retained terminal owner: %#v", active)
	}
	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatalf("interleaved outbox flush error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interleaved outbox flush did not finish")
	}
	if controlClient.transitionCalls != 1 || controlClient.acknowledgementCalls != 1 {
		t.Fatalf("interleaved remote calls = transition:%d acknowledgement:%d", controlClient.transitionCalls, controlClient.acknowledgementCalls)
	}
}

func TestAtomicTerminalAcknowledgementUnknownWriteRejectsConflicts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*protocol.StateTransitionRequest, *protocol.CommandAcknowledgement)
	}{
		{
			name: "payload",
			mutate: func(transition *protocol.StateTransitionRequest, _ *protocol.CommandAcknowledgement) {
				transition.Payload = []byte(`{"different":true}`)
			},
		},
		{
			name: "outcome",
			mutate: func(_ *protocol.StateTransitionRequest, acknowledgement *protocol.CommandAcknowledgement) {
				acknowledgement.Outcome = "rejected"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			app := &daemon{
				config:  testConfig(t),
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: make(map[state.RunKey]*runningRun),
				options: options{
					newID: ids(),
					clock: time.Now,
					queueTerminalTransitionAndAcknowledgement: func(runKey state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, at time.Time) (state.RunJournal, error) {
						test.mutate(&transition, &acknowledgement)
						if _, err := store.QueueTerminalTransitionAndAcknowledgementAt(runKey, transition, acknowledgement, at); err != nil {
							return state.RunJournal{}, err
						}
						return state.RunJournal{}, errors.New("injected conflicting unknown write")
					},
				},
			}

			err := app.queueTerminalTransitionAndAcknowledgementWithContext(context.Background(), key, "failed", map[string]any{"reason": "expected"}, "cancel-1", "failed")
			if err == nil || !errors.Is(err, errAuthoritativeTerminal) {
				t.Fatalf("conflicting %s pair error = %v", test.name, err)
			}
		})
	}
}

type terminalAcknowledgementInterleavingControl struct {
	*fakeControl
	acknowledgementAccepted chan struct{}
	acknowledgementOnce     sync.Once
	transitionCalls         int
	acknowledgementCalls    int
}

func (client *terminalAcknowledgementInterleavingControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	client.transitionCalls++
	return nil
}

func (client *terminalAcknowledgementInterleavingControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	client.acknowledgementCalls++
	client.acknowledgementOnce.Do(func() { close(client.acknowledgementAccepted) })
	return nil
}
