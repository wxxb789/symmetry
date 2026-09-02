// Package protocol defines the version 1 JSON contract used by the daemon.
package protocol

import (
	"encoding/json"
	"time"
)

// MachineEnrollment identifies a machine during one-time enrollment.
type MachineEnrollment struct {
	Name string `json:"name"`
}

// EnrollRequest creates a durable machine identity.
type EnrollRequest struct {
	Machine MachineEnrollment `json:"machine"`
}

// EnrollResponse returns credentials for subsequent daemon requests.
type EnrollResponse struct {
	MachineID    string `json:"machine_id"`
	MachineToken string `json:"machine_token"`
}

// RuntimeRegistration declares one machine-local execution runtime.
type RuntimeRegistration struct {
	RuntimeKey   string          `json:"runtime_key"`
	Name         string          `json:"name"`
	Capacity     int             `json:"capacity"`
	AgentProfile string          `json:"agent_profile"`
	Workspace    string          `json:"workspace"`
	Capabilities json.RawMessage `json:"capabilities"`
}

// SessionRegistrationRequest registers a daemon process and its runtimes.
type SessionRegistrationRequest struct {
	DaemonInstanceID string                `json:"daemon_instance_id"`
	Runtimes         []RuntimeRegistration `json:"runtimes"`
}

// RegisteredRuntime identifies the current epoch for a registered runtime.
type RegisteredRuntime struct {
	RuntimeKey   string `json:"runtime_key"`
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
}

// SessionRegistrationResponse configures the daemon's HTTP and notification cadence.
type SessionRegistrationResponse struct {
	Runtimes            []RegisteredRuntime `json:"runtimes"`
	HeartbeatIntervalMS int64               `json:"heartbeat_interval_ms"`
	PollIntervalMS      int64               `json:"poll_interval_ms"`
	LeaseDurationMS     int64               `json:"lease_duration_ms"`
	WebSocketPath       string              `json:"websocket_path"`
}

// ActiveRun is a currently reserved run reported by a runtime heartbeat.
type ActiveRun struct {
	RunID               string `json:"run_id"`
	Generation          int64  `json:"generation"`
	ClaimedRuntimeEpoch int64  `json:"claimed_runtime_epoch"`
	ClaimID             string `json:"claim_id"`
	LeaseToken          string `json:"lease_token"`
	State               string `json:"state"`
}

// RuntimeHeartbeatRequest reports the complete set of locally active runs.
type RuntimeHeartbeatRequest struct {
	RuntimeEpoch int64       `json:"runtime_epoch"`
	ActiveRuns   []ActiveRun `json:"active_runs"`
}

// Work is an execution request. Input remains open for forward-compatible agent payloads.
type Work struct {
	Goal         string          `json:"goal"`
	AgentProfile string          `json:"agent_profile"`
	Workspace    string          `json:"workspace"`
	Input        json.RawMessage `json:"input"`
}

// Assignment is a non-destructive runtime work assignment.
type Assignment struct {
	RunID               string    `json:"run_id"`
	TaskID              string    `json:"task_id"`
	Generation          int64     `json:"generation"`
	AssignmentExpiresAt time.Time `json:"assignment_expires_at"`
	Work                Work      `json:"work"`
}

// Command is a durable instruction delivered in runtime snapshots.
type Command struct {
	CommandID  string          `json:"command_id"`
	RunID      string          `json:"run_id"`
	Generation int64           `json:"generation"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	IssuedAt   time.Time       `json:"issued_at"`
}

// RuntimeSnapshot is returned by heartbeat and work requests.
type RuntimeSnapshot struct {
	Assignments []Assignment `json:"assignments"`
	Commands    []Command    `json:"commands"`
	ServerTime  time.Time    `json:"server_time"`
}

// Fence identifies the sole authority allowed to mutate a claimed run.
type Fence struct {
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	Generation   int64  `json:"generation"`
	ClaimID      string `json:"claim_id"`
	LeaseToken   string `json:"lease_token"`
}

// ClaimRequest asks the control plane to issue a lease for an assignment.
type ClaimRequest struct {
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	Generation   int64  `json:"generation"`
	ClaimID      string `json:"claim_id"`
}

// ClaimResponse returns a durable lease and its assigned work.
type ClaimResponse struct {
	RunID          string    `json:"run_id"`
	TaskID         string    `json:"task_id"`
	Generation     int64     `json:"generation"`
	ClaimID        string    `json:"claim_id"`
	LeaseToken     string    `json:"lease_token"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	Work           Work      `json:"work"`
}

// LeaseHeartbeatRequest renews a current lease.
type LeaseHeartbeatRequest struct {
	Fence
}

// LeaseHeartbeatResponse returns the extended lease and pending commands.
type LeaseHeartbeatResponse struct {
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	Commands       []Command `json:"commands"`
}

// RunEvent is an append-only, idempotent execution event.
type RunEvent struct {
	EventID    string          `json:"event_id"`
	Sequence   int64           `json:"sequence"`
	Kind       string          `json:"kind"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// AppendEventsRequest appends one or more fenced events.
type AppendEventsRequest struct {
	Fence
	Events []RunEvent `json:"events"`
}

// StateTransitionRequest advances a fenced run lifecycle state.
type StateTransitionRequest struct {
	Fence
	TransitionID string          `json:"transition_id"`
	State        string          `json:"state"`
	Payload      json.RawMessage `json:"payload"`
}

// ReconcileRun describes one run retained in the daemon's local journal.
type ReconcileRun struct {
	RunID               string `json:"run_id"`
	Generation          int64  `json:"generation"`
	ClaimedRuntimeEpoch int64  `json:"claimed_runtime_epoch"`
	ClaimID             string `json:"claim_id"`
	LeaseToken          string `json:"lease_token"`
	LocalState          string `json:"local_state"`
	LastEventSequence   int64  `json:"last_event_sequence"`
}

// ReconcileRequest submits the daemon's complete local run journal for a runtime.
type ReconcileRequest struct {
	RuntimeEpoch int64          `json:"runtime_epoch"`
	Runs         []ReconcileRun `json:"runs"`
}

// ReconcileDecision tells the daemon whether it may retain a local execution.
type ReconcileDecision struct {
	RunID          string     `json:"run_id"`
	Generation     int64      `json:"generation"`
	Decision       string     `json:"decision"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
}

// ReconcileResponse combines recovery decisions with the current snapshot.
type ReconcileResponse struct {
	Decisions   []ReconcileDecision `json:"decisions"`
	Assignments []Assignment        `json:"assignments"`
	Commands    []Command           `json:"commands"`
}

// CommandAcknowledgement records command delivery without changing lifecycle state.
type CommandAcknowledgement struct {
	Fence
	RunID     string `json:"run_id"`
	CommandID string `json:"command_id"`
	Outcome   string `json:"outcome"`
	AckID     string `json:"ack_id"`
}

// TaskSubmitRequest creates a task using an explicit HTTP idempotency key.
type TaskSubmitRequest struct {
	Work Work `json:"work"`
}

// TaskInputRequest supplies structured human input using an idempotency key.
type TaskInputRequest struct {
	Input json.RawMessage `json:"input"`
}

// Task is the stable portion of a task-control response. Result fields are extensible.
type Task struct {
	TaskID     string          `json:"task_id"`
	State      string          `json:"state"`
	RunID      string          `json:"run_id"`
	Generation int64           `json:"generation"`
	Work       *Work           `json:"work"`
	Result     json.RawMessage `json:"result"`
	Failure    json.RawMessage `json:"failure"`
}

// ErrorEnvelope is the standard non-success response body.
type ErrorEnvelope struct {
	Error ProtocolError `json:"error"`
}

// ProtocolError gives the durable error code and human-readable message.
type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
