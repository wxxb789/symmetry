package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// ControlAPI is a configurable ControlAPI double. A nil hook answers with the
// permissive behaviour most app fakes use; all methods but Dispatch record the request.
type ControlAPI struct {
	OnRegisterSession    func(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error)
	OnHeartbeat          func(context.Context, string, protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error)
	OnDispatch           func(context.Context, string, int64) (protocol.RuntimeSnapshot, error)
	OnClaim              func(context.Context, string, protocol.ClaimRequest) (protocol.ClaimResponse, error)
	OnRenewLease         func(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error)
	OnAppendEvents       func(context.Context, string, protocol.AppendEventsRequest) error
	OnTransition         func(context.Context, string, protocol.StateTransitionRequest) error
	OnReconcile          func(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error)
	OnAcknowledgeCommand func(context.Context, string, protocol.CommandAcknowledgement) error

	mu               sync.Mutex
	registrations    []protocol.SessionRegistrationRequest
	heartbeats       []protocol.RuntimeHeartbeatRequest
	claims           []protocol.ClaimRequest
	renewals         []protocol.LeaseHeartbeatRequest
	appendedEvents   []protocol.AppendEventsRequest
	transitions      []protocol.StateTransitionRequest
	reconciliations  []protocol.ReconcileRequest
	acknowledgements []protocol.CommandAcknowledgement
}

func (control *ControlAPI) RegisterSession(ctx context.Context, machineID, daemonInstanceID string, request protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	control.mu.Lock()
	control.registrations = append(control.registrations, request)
	hook := control.OnRegisterSession
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, machineID, daemonInstanceID, request)
	}
	return protocol.SessionRegistrationResponse{
		Runtimes:        []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}},
		LeaseDurationMS: 120_000,
	}, nil
}

func (control *ControlAPI) Heartbeat(ctx context.Context, runtimeID string, request protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	control.mu.Lock()
	control.heartbeats = append(control.heartbeats, request)
	hook := control.OnHeartbeat
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runtimeID, request)
	}
	return protocol.RuntimeSnapshot{}, nil
}

func (control *ControlAPI) Dispatch(ctx context.Context, runtimeID string, epoch int64) (protocol.RuntimeSnapshot, error) {
	hook := control.OnDispatch
	if hook != nil {
		return hook(ctx, runtimeID, epoch)
	}
	return protocol.RuntimeSnapshot{}, nil
}

func (control *ControlAPI) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	control.mu.Lock()
	control.claims = append(control.claims, request)
	hook := control.OnClaim
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runID, request)
	}
	return protocol.ClaimResponse{
		RunID:          runID,
		Generation:     request.Generation,
		ClaimID:        request.ClaimID,
		LeaseToken:     "lease",
		LeaseExpiresAt: time.Now().Add(time.Minute),
		Work:           protocol.Work{Goal: "work"},
	}, nil
}

func (control *ControlAPI) RenewLease(ctx context.Context, runID string, request protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	control.mu.Lock()
	control.renewals = append(control.renewals, request)
	hook := control.OnRenewLease
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runID, request)
	}
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (control *ControlAPI) AppendEvents(ctx context.Context, runID string, request protocol.AppendEventsRequest) error {
	control.mu.Lock()
	control.appendedEvents = append(control.appendedEvents, request)
	hook := control.OnAppendEvents
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runID, request)
	}
	return nil
}

func (control *ControlAPI) Transition(ctx context.Context, runID string, request protocol.StateTransitionRequest) error {
	control.mu.Lock()
	control.transitions = append(control.transitions, request)
	hook := control.OnTransition
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runID, request)
	}
	return nil
}

func (control *ControlAPI) Reconcile(ctx context.Context, runtimeID string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	control.mu.Lock()
	control.reconciliations = append(control.reconciliations, request)
	hook := control.OnReconcile
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runtimeID, request)
	}
	return protocol.ReconcileResponse{}, nil
}

func (control *ControlAPI) AcknowledgeCommand(ctx context.Context, runID string, acknowledgement protocol.CommandAcknowledgement) error {
	control.mu.Lock()
	control.acknowledgements = append(control.acknowledgements, acknowledgement)
	hook := control.OnAcknowledgeCommand
	control.mu.Unlock()
	if hook != nil {
		return hook(ctx, runID, acknowledgement)
	}
	return nil
}

func (control *ControlAPI) Registrations() []protocol.SessionRegistrationRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.SessionRegistrationRequest(nil), control.registrations...)
}

func (control *ControlAPI) Heartbeats() []protocol.RuntimeHeartbeatRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.RuntimeHeartbeatRequest(nil), control.heartbeats...)
}

func (control *ControlAPI) Claims() []protocol.ClaimRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.ClaimRequest(nil), control.claims...)
}

func (control *ControlAPI) Renewals() []protocol.LeaseHeartbeatRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.LeaseHeartbeatRequest(nil), control.renewals...)
}

func (control *ControlAPI) AppendedEvents() []protocol.AppendEventsRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.AppendEventsRequest(nil), control.appendedEvents...)
}

func (control *ControlAPI) Transitions() []protocol.StateTransitionRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.StateTransitionRequest(nil), control.transitions...)
}

func (control *ControlAPI) Reconciliations() []protocol.ReconcileRequest {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.ReconcileRequest(nil), control.reconciliations...)
}

func (control *ControlAPI) Acknowledgements() []protocol.CommandAcknowledgement {
	control.mu.Lock()
	defer control.mu.Unlock()
	return append([]protocol.CommandAcknowledgement(nil), control.acknowledgements...)
}
