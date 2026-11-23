package apptest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestControlAPIDefaultsAnswerTheMajorityBehaviour(t *testing.T) {
	ctx := context.Background()

	t.Run("RegisterSession", func(t *testing.T) {
		response, err := (&ControlAPI{}).RegisterSession(ctx, "machine-1", "daemon-1", protocol.SessionRegistrationRequest{})
		if err != nil {
			t.Fatalf("RegisterSession() error = %v", err)
		}
		want := []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}
		if !reflect.DeepEqual(response.Runtimes, want) {
			t.Fatalf("RegisterSession() runtimes = %#v, want %#v", response.Runtimes, want)
		}
		if response.LeaseDurationMS != 120_000 {
			t.Fatalf("RegisterSession() lease duration = %d, want 120000", response.LeaseDurationMS)
		}
	})

	t.Run("Heartbeat", func(t *testing.T) {
		snapshot, err := (&ControlAPI{}).Heartbeat(ctx, "runtime-1", protocol.RuntimeHeartbeatRequest{})
		if err != nil {
			t.Fatalf("Heartbeat() error = %v", err)
		}
		if !reflect.DeepEqual(snapshot, protocol.RuntimeSnapshot{}) {
			t.Fatalf("Heartbeat() snapshot = %#v, want the zero value", snapshot)
		}
	})

	t.Run("Dispatch", func(t *testing.T) {
		snapshot, err := (&ControlAPI{}).Dispatch(ctx, "runtime-1", 1)
		if err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
		if !reflect.DeepEqual(snapshot, protocol.RuntimeSnapshot{}) {
			t.Fatalf("Dispatch() snapshot = %#v, want the zero value", snapshot)
		}
	})

	t.Run("Claim", func(t *testing.T) {
		response, err := (&ControlAPI{}).Claim(ctx, "run-1", protocol.ClaimRequest{Generation: 7, ClaimID: "claim-1"})
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if response.RunID != "run-1" {
			t.Fatalf("Claim() run id = %q, want %q", response.RunID, "run-1")
		}
		if response.Generation != 7 {
			t.Fatalf("Claim() generation = %d, want 7", response.Generation)
		}
		if response.ClaimID != "claim-1" {
			t.Fatalf("Claim() claim id = %q, want %q", response.ClaimID, "claim-1")
		}
		if response.LeaseToken != "lease" {
			t.Fatalf("Claim() lease token = %q, want %q", response.LeaseToken, "lease")
		}
		if response.LeaseExpiresAt.IsZero() {
			t.Fatal("Claim() lease expiry is the zero time")
		}
		if response.Work.Goal != "work" {
			t.Fatalf("Claim() work goal = %q, want %q", response.Work.Goal, "work")
		}
	})

	t.Run("RenewLease", func(t *testing.T) {
		response, err := (&ControlAPI{}).RenewLease(ctx, "run-1", protocol.LeaseHeartbeatRequest{})
		if err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		if response.LeaseExpiresAt.IsZero() {
			t.Fatal("RenewLease() lease expiry is the zero time")
		}
	})

	t.Run("AppendEvents", func(t *testing.T) {
		if err := (&ControlAPI{}).AppendEvents(ctx, "run-1", protocol.AppendEventsRequest{}); err != nil {
			t.Fatalf("AppendEvents() error = %v", err)
		}
	})

	t.Run("Transition", func(t *testing.T) {
		if err := (&ControlAPI{}).Transition(ctx, "run-1", protocol.StateTransitionRequest{}); err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
	})

	t.Run("Reconcile", func(t *testing.T) {
		response, err := (&ControlAPI{}).Reconcile(ctx, "runtime-1", protocol.ReconcileRequest{})
		if err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
		if !reflect.DeepEqual(response, protocol.ReconcileResponse{}) {
			t.Fatalf("Reconcile() response = %#v, want the zero value", response)
		}
	})

	t.Run("AcknowledgeCommand", func(t *testing.T) {
		if err := (&ControlAPI{}).AcknowledgeCommand(ctx, "run-1", protocol.CommandAcknowledgement{}); err != nil {
			t.Fatalf("AcknowledgeCommand() error = %v", err)
		}
	})
}

func TestControlAPIHooksReplaceMethods(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("sentinel")

	t.Run("RegisterSession", func(t *testing.T) {
		control := &ControlAPI{}
		var machineID, daemonInstanceID string
		var received protocol.SessionRegistrationRequest
		want := protocol.SessionRegistrationResponse{LeaseDurationMS: 7, WebSocketPath: "/hook"}
		control.OnRegisterSession = func(_ context.Context, machine, daemon string, request protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
			machineID, daemonInstanceID, received = machine, daemon, request
			return want, sentinel
		}
		request := protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "hook-runtime"}}}
		response, err := control.RegisterSession(ctx, "machine-1", "daemon-1", request)
		if !errors.Is(err, sentinel) {
			t.Fatalf("RegisterSession() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(response, want) {
			t.Fatalf("RegisterSession() response = %#v, want the hook response %#v", response, want)
		}
		if machineID != "machine-1" || daemonInstanceID != "daemon-1" {
			t.Fatalf("RegisterSession() forwarded machine %q and daemon %q", machineID, daemonInstanceID)
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("RegisterSession() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("Heartbeat", func(t *testing.T) {
		control := &ControlAPI{}
		var runtimeID string
		var received protocol.RuntimeHeartbeatRequest
		want := protocol.RuntimeSnapshot{ServerTime: time.Unix(7, 0)}
		control.OnHeartbeat = func(_ context.Context, runtime string, request protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
			runtimeID, received = runtime, request
			return want, sentinel
		}
		request := protocol.RuntimeHeartbeatRequest{RuntimeEpoch: 3, ActiveRuns: []protocol.ActiveRun{{RunID: "run-1"}}}
		snapshot, err := control.Heartbeat(ctx, "runtime-1", request)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Heartbeat() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(snapshot, want) {
			t.Fatalf("Heartbeat() snapshot = %#v, want the hook snapshot %#v", snapshot, want)
		}
		if runtimeID != "runtime-1" {
			t.Fatalf("Heartbeat() forwarded runtime id %q, want %q", runtimeID, "runtime-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("Heartbeat() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("Dispatch", func(t *testing.T) {
		control := &ControlAPI{}
		var runtimeID string
		var epoch int64
		want := protocol.RuntimeSnapshot{ServerTime: time.Unix(7, 0)}
		control.OnDispatch = func(_ context.Context, runtime string, requestedEpoch int64) (protocol.RuntimeSnapshot, error) {
			runtimeID, epoch = runtime, requestedEpoch
			return want, sentinel
		}
		snapshot, err := control.Dispatch(ctx, "runtime-1", 3)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Dispatch() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(snapshot, want) {
			t.Fatalf("Dispatch() snapshot = %#v, want the hook snapshot %#v", snapshot, want)
		}
		if runtimeID != "runtime-1" || epoch != 3 {
			t.Fatalf("Dispatch() forwarded runtime %q and epoch %d, want %q and 3", runtimeID, epoch, "runtime-1")
		}
	})

	t.Run("Claim", func(t *testing.T) {
		control := &ControlAPI{}
		var runID string
		var received protocol.ClaimRequest
		want := protocol.ClaimResponse{RunID: "hook-run", ClaimID: "hook-claim", LeaseToken: "hook-lease"}
		control.OnClaim = func(_ context.Context, run string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
			runID, received = run, request
			return want, sentinel
		}
		request := protocol.ClaimRequest{RuntimeID: "runtime-1", Generation: 3, ClaimID: "claim-1"}
		response, err := control.Claim(ctx, "run-1", request)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Claim() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(response, want) {
			t.Fatalf("Claim() response = %#v, want the hook response %#v", response, want)
		}
		if runID != "run-1" {
			t.Fatalf("Claim() forwarded run id %q, want %q", runID, "run-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("Claim() forwarded request %#v, want %#v", received, request)
		}
		if got := control.Claims(); len(got) != 1 || !reflect.DeepEqual(got[0], request) {
			t.Fatalf("Claim() with a hook set recorded %#v, want the request %#v", got, request)
		}
	})

	t.Run("RenewLease", func(t *testing.T) {
		control := &ControlAPI{}
		var runID string
		var received protocol.LeaseHeartbeatRequest
		want := protocol.LeaseHeartbeatResponse{LeaseRemainingMS: 7}
		control.OnRenewLease = func(_ context.Context, run string, request protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
			runID, received = run, request
			return want, sentinel
		}
		request := protocol.LeaseHeartbeatRequest{Fence: protocol.Fence{RuntimeID: "runtime-1", Generation: 3}}
		response, err := control.RenewLease(ctx, "run-1", request)
		if !errors.Is(err, sentinel) {
			t.Fatalf("RenewLease() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(response, want) {
			t.Fatalf("RenewLease() response = %#v, want the hook response %#v", response, want)
		}
		if runID != "run-1" {
			t.Fatalf("RenewLease() forwarded run id %q, want %q", runID, "run-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("RenewLease() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("AppendEvents", func(t *testing.T) {
		control := &ControlAPI{}
		var runID string
		var received protocol.AppendEventsRequest
		control.OnAppendEvents = func(_ context.Context, run string, request protocol.AppendEventsRequest) error {
			runID, received = run, request
			return sentinel
		}
		request := protocol.AppendEventsRequest{Events: []protocol.RunEvent{{EventID: "event-1", Sequence: 1, Kind: "output"}}}
		if err := control.AppendEvents(ctx, "run-1", request); !errors.Is(err, sentinel) {
			t.Fatalf("AppendEvents() error = %v, want the hook sentinel", err)
		}
		if runID != "run-1" {
			t.Fatalf("AppendEvents() forwarded run id %q, want %q", runID, "run-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("AppendEvents() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("Transition", func(t *testing.T) {
		control := &ControlAPI{}
		var runID string
		var received protocol.StateTransitionRequest
		control.OnTransition = func(_ context.Context, run string, request protocol.StateTransitionRequest) error {
			runID, received = run, request
			return sentinel
		}
		request := protocol.StateTransitionRequest{TransitionID: "transition-1", State: "running"}
		if err := control.Transition(ctx, "run-1", request); !errors.Is(err, sentinel) {
			t.Fatalf("Transition() error = %v, want the hook sentinel", err)
		}
		if runID != "run-1" {
			t.Fatalf("Transition() forwarded run id %q, want %q", runID, "run-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("Transition() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("Reconcile", func(t *testing.T) {
		control := &ControlAPI{}
		var runtimeID string
		var received protocol.ReconcileRequest
		want := protocol.ReconcileResponse{Decisions: []protocol.ReconcileDecision{{RunID: "run-1", Decision: protocol.ReconcileContinue}}}
		control.OnReconcile = func(_ context.Context, runtime string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
			runtimeID, received = runtime, request
			return want, sentinel
		}
		request := protocol.ReconcileRequest{RuntimeEpoch: 3, Runs: []protocol.ReconcileRun{{RunID: "run-1", Generation: 3}}}
		response, err := control.Reconcile(ctx, "runtime-1", request)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Reconcile() error = %v, want the hook sentinel", err)
		}
		if !reflect.DeepEqual(response, want) {
			t.Fatalf("Reconcile() response = %#v, want the hook response %#v", response, want)
		}
		if runtimeID != "runtime-1" {
			t.Fatalf("Reconcile() forwarded runtime id %q, want %q", runtimeID, "runtime-1")
		}
		if !reflect.DeepEqual(received, request) {
			t.Fatalf("Reconcile() forwarded request %#v, want %#v", received, request)
		}
	})

	t.Run("AcknowledgeCommand", func(t *testing.T) {
		control := &ControlAPI{}
		var runID string
		var received protocol.CommandAcknowledgement
		control.OnAcknowledgeCommand = func(_ context.Context, run string, acknowledgement protocol.CommandAcknowledgement) error {
			runID, received = run, acknowledgement
			return sentinel
		}
		acknowledgement := protocol.CommandAcknowledgement{RunID: "run-1", CommandID: "command-1", Outcome: "delivered", AckID: "ack-1"}
		if err := control.AcknowledgeCommand(ctx, "run-1", acknowledgement); !errors.Is(err, sentinel) {
			t.Fatalf("AcknowledgeCommand() error = %v, want the hook sentinel", err)
		}
		if runID != "run-1" {
			t.Fatalf("AcknowledgeCommand() forwarded run id %q, want %q", runID, "run-1")
		}
		if !reflect.DeepEqual(received, acknowledgement) {
			t.Fatalf("AcknowledgeCommand() forwarded acknowledgement %#v, want %#v", received, acknowledgement)
		}
	})

	t.Run("hooks run outside the lock", func(t *testing.T) {
		control := &ControlAPI{}
		observed := make(chan int, 1)
		control.OnClaim = func(_ context.Context, _ string, _ protocol.ClaimRequest) (protocol.ClaimResponse, error) {
			if err := control.AppendEvents(ctx, "run-1", protocol.AppendEventsRequest{}); err != nil {
				observed <- -1
				return protocol.ClaimResponse{}, err
			}
			observed <- len(control.AppendedEvents())
			return protocol.ClaimResponse{}, sentinel
		}
		go func() {
			_, _ = control.Claim(ctx, "run-1", protocol.ClaimRequest{})
		}()
		select {
		case count := <-observed:
			if count != 1 {
				t.Fatalf("hook observed %d recorded appended events, want 1", count)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("hook did not run outside the double's lock")
		}
	})
}

func TestControlAPIRecordedRequestsAreDetachedCopies(t *testing.T) {
	ctx := context.Background()

	t.Run("Registrations", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "runtime-1", Name: "name-1"}}}
		if _, err := control.RegisterSession(ctx, "machine-1", "daemon-1", request); err != nil {
			t.Fatalf("RegisterSession() error = %v", err)
		}
		assertDetachedRecord(t, control.Registrations, request)
	})

	t.Run("Heartbeats", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.RuntimeHeartbeatRequest{RuntimeEpoch: 3, ActiveRuns: []protocol.ActiveRun{{RunID: "run-1"}}}
		if _, err := control.Heartbeat(ctx, "runtime-1", request); err != nil {
			t.Fatalf("Heartbeat() error = %v", err)
		}
		assertDetachedRecord(t, control.Heartbeats, request)
	})

	t.Run("Claims", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: 7, ClaimID: "claim-1"}
		if _, err := control.Claim(ctx, "run-1", request); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		assertDetachedRecord(t, control.Claims, request)
	})

	t.Run("Renewals", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.LeaseHeartbeatRequest{Fence: protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: 7, ClaimID: "claim-1", LeaseToken: "lease-1"}}
		if _, err := control.RenewLease(ctx, "run-1", request); err != nil {
			t.Fatalf("RenewLease() error = %v", err)
		}
		assertDetachedRecord(t, control.Renewals, request)
	})

	t.Run("AppendedEvents", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.AppendEventsRequest{Fence: protocol.Fence{RuntimeID: "runtime-1"}, Events: []protocol.RunEvent{{EventID: "event-1", Sequence: 1, Kind: "output"}}}
		if err := control.AppendEvents(ctx, "run-1", request); err != nil {
			t.Fatalf("AppendEvents() error = %v", err)
		}
		assertDetachedRecord(t, control.AppendedEvents, request)
	})

	t.Run("Transitions", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.StateTransitionRequest{Fence: protocol.Fence{RuntimeID: "runtime-1"}, TransitionID: "transition-1", State: "running"}
		if err := control.Transition(ctx, "run-1", request); err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		assertDetachedRecord(t, control.Transitions, request)
	})

	t.Run("Reconciliations", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.ReconcileRequest{RuntimeEpoch: 3, Runs: []protocol.ReconcileRun{{RunID: "run-1", Generation: 7}}}
		if _, err := control.Reconcile(ctx, "runtime-1", request); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
		assertDetachedRecord(t, control.Reconciliations, request)
	})

	t.Run("Acknowledgements", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.CommandAcknowledgement{Fence: protocol.Fence{RuntimeID: "runtime-1"}, RunID: "run-1", CommandID: "command-1", Outcome: "delivered", AckID: "ack-1"}
		if err := control.AcknowledgeCommand(ctx, "run-1", request); err != nil {
			t.Fatalf("AcknowledgeCommand() error = %v", err)
		}
		assertDetachedRecord(t, control.Acknowledgements, request)
	})
}

func TestControlAPINestedRecordsAreDetachedCopies(t *testing.T) {
	ctx := context.Background()

	t.Run("Registrations", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{{RuntimeKey: "runtime-1"}}}
		if _, err := control.RegisterSession(ctx, "machine-1", "daemon-1", request); err != nil {
			t.Fatalf("RegisterSession() error = %v", err)
		}
		request.Runtimes[0].RuntimeKey = "mutated-caller"
		recorded := control.Registrations()
		if len(recorded) != 1 || recorded[0].Runtimes[0].RuntimeKey != "runtime-1" {
			t.Fatalf("record after mutating the caller's request = %#v, want runtime-1", recorded)
		}
		recorded[0].Runtimes[0].RuntimeKey = "mutated-reader"
		if again := control.Registrations(); len(again) != 1 || again[0].Runtimes[0].RuntimeKey != "runtime-1" {
			t.Fatalf("record after mutating the returned copy = %#v, want runtime-1", again)
		}
	})

	t.Run("Heartbeats", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.RuntimeHeartbeatRequest{RuntimeEpoch: 3, ActiveRuns: []protocol.ActiveRun{{RunID: "run-1"}}}
		if _, err := control.Heartbeat(ctx, "runtime-1", request); err != nil {
			t.Fatalf("Heartbeat() error = %v", err)
		}
		request.ActiveRuns[0].RunID = "mutated-caller"
		recorded := control.Heartbeats()
		if len(recorded) != 1 || recorded[0].ActiveRuns[0].RunID != "run-1" {
			t.Fatalf("record after mutating the caller's request = %#v, want run-1", recorded)
		}
		recorded[0].ActiveRuns[0].RunID = "mutated-reader"
		if again := control.Heartbeats(); len(again) != 1 || again[0].ActiveRuns[0].RunID != "run-1" {
			t.Fatalf("record after mutating the returned copy = %#v, want run-1", again)
		}
	})

	t.Run("AppendedEvents", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.AppendEventsRequest{Fence: protocol.Fence{RuntimeID: "runtime-1"}, Events: []protocol.RunEvent{{EventID: "event-1", Sequence: 1, Kind: "output", Payload: json.RawMessage(`{"payload":true}`)}}}
		if err := control.AppendEvents(ctx, "run-1", request); err != nil {
			t.Fatalf("AppendEvents() error = %v", err)
		}
		request.Events[0].EventID = "mutated-caller"
		request.Events[0].Payload[0] = 'X'
		recorded := control.AppendedEvents()
		if len(recorded) != 1 || recorded[0].Events[0].EventID != "event-1" || string(recorded[0].Events[0].Payload) != `{"payload":true}` {
			t.Fatalf("record after mutating the caller's request = %#v, want event-1 with the original payload", recorded)
		}
		recorded[0].Events[0].EventID = "mutated-reader"
		recorded[0].Events[0].Payload[0] = 'Y'
		if again := control.AppendedEvents(); len(again) != 1 || again[0].Events[0].EventID != "event-1" || string(again[0].Events[0].Payload) != `{"payload":true}` {
			t.Fatalf("record after mutating the returned copy = %#v, want event-1 with the original payload", again)
		}
	})

	t.Run("Transitions", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.StateTransitionRequest{Fence: protocol.Fence{RuntimeID: "runtime-1"}, TransitionID: "transition-1", State: "running", Payload: json.RawMessage(`{"payload":true}`)}
		if err := control.Transition(ctx, "run-1", request); err != nil {
			t.Fatalf("Transition() error = %v", err)
		}
		request.Payload[0] = 'X'
		recorded := control.Transitions()
		if len(recorded) != 1 || string(recorded[0].Payload) != `{"payload":true}` {
			t.Fatalf("record after mutating the caller's payload = %#v, want the original payload", recorded)
		}
		recorded[0].Payload[0] = 'Y'
		if again := control.Transitions(); len(again) != 1 || string(again[0].Payload) != `{"payload":true}` {
			t.Fatalf("record after mutating the returned payload = %#v, want the original payload", again)
		}
	})

	t.Run("Reconciliations", func(t *testing.T) {
		control := &ControlAPI{}
		request := protocol.ReconcileRequest{RuntimeEpoch: 3, Runs: []protocol.ReconcileRun{{RunID: "run-1", Generation: 7}}}
		if _, err := control.Reconcile(ctx, "runtime-1", request); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
		request.Runs[0].RunID = "mutated-caller"
		recorded := control.Reconciliations()
		if len(recorded) != 1 || recorded[0].Runs[0].RunID != "run-1" {
			t.Fatalf("record after mutating the caller's request = %#v, want run-1", recorded)
		}
		recorded[0].Runs[0].RunID = "mutated-reader"
		if again := control.Reconciliations(); len(again) != 1 || again[0].Runs[0].RunID != "run-1" {
			t.Fatalf("record after mutating the returned copy = %#v, want run-1", again)
		}
	})
}

func assertDetachedRecord[T any](t *testing.T, read func() []T, want T) {
	t.Helper()
	recorded := read()
	if len(recorded) != 1 || !reflect.DeepEqual(recorded[0], want) {
		t.Fatalf("recorded requests = %#v, want exactly one entry equal to %#v", recorded, want)
	}
	recorded[0] = *new(T)
	if got := read(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("mutating the returned slice changed the record: %#v", got)
	}
}
