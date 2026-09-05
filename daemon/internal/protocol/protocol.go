// Package protocol defines the version 1 JSON contract used by the daemon.
package protocol

import (
	"encoding/json"
	"time"
)

// MinimumLeaseDurationMS leaves enough time for the renewal cadence, request
// deadline, and expiry safety margin used by the daemon.
const MinimumLeaseDurationMS int64 = 30_000

// AgentInputRecordType identifies a newline-delimited message sent to an agent.
type AgentInputRecordType string

const (
	// AgentInputRecordTaskInput starts an agent execution.
	AgentInputRecordTaskInput AgentInputRecordType = "task_input"
	// AgentInputRecordProvideInput continues an interactive agent execution.
	AgentInputRecordProvideInput AgentInputRecordType = "provide_input"
)

// AgentInputRecord is the JSON envelope sent to an agent over standard input.
type AgentInputRecord struct {
	Type  AgentInputRecordType `json:"type"`
	Goal  string               `json:"goal"`
	Input json.RawMessage      `json:"input"`
}

// MachineEnrollment identifies a machine during one-time enrollment.
type MachineEnrollment struct {
	Name string `json:"name"`
}

// EnrollRequest creates a durable machine identity.
type EnrollRequest struct {
	Machine      MachineEnrollment `json:"machine"`
	MachineToken string            `json:"machine_token"`
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
	Runtimes []RuntimeRegistration `json:"runtimes"`
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
	Input        json.RawMessage `json:"input,omitempty"`
	present      map[string]struct{}
}

// UnmarshalJSON retains whether optional-looking protocol fields were sent.
// Control-plane response validation distinguishes an omitted field from JSON null.
func (work *Work) UnmarshalJSON(value []byte) error {
	type wire Work
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*work = Work(decoded)
	work.present = present
	return nil
}

// HasField reports whether a field was present when this value was decoded.
func (work Work) HasField(name string) bool {
	_, ok := work.present[name]
	return ok
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

// Run is the durable run resource returned after a state transition.
type Run struct {
	RunID          string          `json:"run_id"`
	TaskID         string          `json:"task_id"`
	RuntimeID      string          `json:"runtime_id"`
	Generation     int64           `json:"generation"`
	State          string          `json:"state"`
	ClaimID        string          `json:"claim_id"`
	LeaseToken     string          `json:"lease_token"`
	LeaseExpiresAt time.Time       `json:"lease_expires_at"`
	Result         json.RawMessage `json:"result"`
	Failure        json.RawMessage `json:"failure"`
	present        map[string]struct{}
}

// UnmarshalJSON retains response-field presence for strict protocol validation.
func (run *Run) UnmarshalJSON(value []byte) error {
	type wire Run
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*run = Run(decoded)
	run.present = present
	return nil
}

// HasField reports whether a field was present when this value was decoded.
func (run Run) HasField(name string) bool {
	_, ok := run.present[name]
	return ok
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

// ReconcileDecisionKind is a control-plane decision for one local execution.
type ReconcileDecisionKind string

const (
	ReconcileContinue    ReconcileDecisionKind = "continue"
	ReconcileCancel      ReconcileDecisionKind = "cancel"
	ReconcileStaleStop   ReconcileDecisionKind = "stale_stop"
	ReconcileTerminal    ReconcileDecisionKind = "terminal"
	ReconcileUnknownStop ReconcileDecisionKind = "unknown_stop"
)

// ReconcileDecision tells the daemon whether it may retain a local execution.
type ReconcileDecision struct {
	RunID          string                `json:"run_id"`
	Generation     int64                 `json:"generation"`
	Decision       ReconcileDecisionKind `json:"decision"`
	LeaseExpiresAt *time.Time            `json:"lease_expires_at"`
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

// TaskCommandRequest creates an operator command under an HTTP idempotency key.
// Cancel requests omit Payload; provide_input requests require an object Payload.
type TaskCommandRequest struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// TaskCommand is an operator-facing command resource. Unlike daemon dispatch
// commands, historical cancel or retry commands may not be associated with a run.
type TaskCommand struct {
	CommandID              string          `json:"command_id"`
	TaskID                 string          `json:"task_id"`
	RunID                  *string         `json:"run_id"`
	Generation             *int64          `json:"generation"`
	Kind                   string          `json:"kind"`
	Payload                json.RawMessage `json:"payload"`
	State                  string          `json:"state"`
	IssuedAt               time.Time       `json:"issued_at"`
	AppliedAt              *time.Time      `json:"applied_at"`
	AcknowledgementID      *string         `json:"acknowledgement_id"`
	AcknowledgementOutcome *string         `json:"acknowledgement_outcome"`
	AcknowledgedAt         *time.Time      `json:"acknowledged_at"`
	present                map[string]struct{}
}

// UnmarshalJSON retains response-field presence for strict protocol validation.
func (command *TaskCommand) UnmarshalJSON(value []byte) error {
	type wire TaskCommand
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*command = TaskCommand(decoded)
	command.present = present
	return nil
}

// HasField reports whether a field was present when this value was decoded.
func (command TaskCommand) HasField(name string) bool {
	_, ok := command.present[name]
	return ok
}

// TaskWaiting is the current run's waiting-for-input projection.
type TaskWaiting struct {
	RunID        string          `json:"run_id"`
	Generation   int64           `json:"generation"`
	TransitionID string          `json:"transition_id"`
	Question     *string         `json:"question"`
	Payload      json.RawMessage `json:"payload"`
	RecordedAt   time.Time       `json:"recorded_at"`
	present      map[string]struct{}
}

// UnmarshalJSON retains response-field presence for strict protocol validation.
func (waiting *TaskWaiting) UnmarshalJSON(value []byte) error {
	type wire TaskWaiting
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*waiting = TaskWaiting(decoded)
	waiting.present = present
	return nil
}

// HasField reports whether a field was present when this value was decoded.
func (waiting TaskWaiting) HasField(name string) bool {
	_, ok := waiting.present[name]
	return ok
}

// Task is the stable task-control response projection. Generation identifies
// the current attempt even before its run has been materialized.
type Task struct {
	TaskID        string          `json:"task_id"`
	State         string          `json:"state"`
	RunID         *string         `json:"run_id"`
	Generation    *int64          `json:"generation"`
	Work          *Work           `json:"work"`
	Result        json.RawMessage `json:"result"`
	Failure       json.RawMessage `json:"failure"`
	Waiting       *TaskWaiting    `json:"waiting"`
	LatestCommand *TaskCommand    `json:"latest_command"`
	present       map[string]struct{}
}

// UnmarshalJSON retains response-field presence for strict protocol validation.
func (task *Task) UnmarshalJSON(value []byte) error {
	type wire Task
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*task = Task(decoded)
	task.present = present
	return nil
}

// HasField reports whether a field was present when this value was decoded.
func (task Task) HasField(name string) bool {
	_, ok := task.present[name]
	return ok
}

func presentFields(value []byte) (map[string]struct{}, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(value, &fields); err != nil {
		return nil, err
	}
	present := make(map[string]struct{}, len(fields))
	for name := range fields {
		present[name] = struct{}{}
	}
	return present, nil
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
