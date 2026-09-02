package control

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func validateEnrollResponse(response protocol.EnrollResponse) error {
	if response.MachineID == "" || response.MachineToken == "" {
		return invalidResponse("enroll", "machine_id and machine_token are required")
	}
	return nil
}

func validateSessionResponse(request protocol.SessionRegistrationRequest, response protocol.SessionRegistrationResponse) error {
	if response.Runtimes == nil || len(response.Runtimes) != len(request.Runtimes) {
		return invalidResponse("session registration", "runtimes must match the request")
	}
	expected := make(map[string]struct{}, len(request.Runtimes))
	for _, runtime := range request.Runtimes {
		if runtime.RuntimeKey == "" {
			return invalidResponse("session registration", "request runtime_key must not be empty")
		}
		if _, exists := expected[runtime.RuntimeKey]; exists {
			return invalidResponse("session registration", "request runtime_key is duplicated")
		}
		expected[runtime.RuntimeKey] = struct{}{}
	}
	for _, runtime := range response.Runtimes {
		if runtime.RuntimeKey == "" || runtime.RuntimeID == "" || runtime.RuntimeEpoch <= 0 {
			return invalidResponse("session registration", "each runtime requires runtime_key, runtime_id, and positive runtime_epoch")
		}
		if _, exists := expected[runtime.RuntimeKey]; !exists {
			return invalidResponse("session registration", "runtime_key does not match the request")
		}
		delete(expected, runtime.RuntimeKey)
	}
	if len(expected) != 0 {
		return invalidResponse("session registration", "runtime_key does not match the request")
	}
	if response.HeartbeatIntervalMS <= 0 || response.PollIntervalMS <= 0 || response.LeaseDurationMS <= 0 || response.WebSocketPath == "" {
		return invalidResponse("session registration", "intervals and websocket_path are required")
	}
	return nil
}

func validateRuntimeSnapshot(response protocol.RuntimeSnapshot) error {
	if response.Assignments == nil || response.Commands == nil || response.ServerTime.IsZero() {
		return invalidResponse("runtime snapshot", "assignments, commands, and server_time are required")
	}
	return validateSnapshotParts("runtime snapshot", response.Assignments, response.Commands)
}

func validateClaimResponse(runID string, request protocol.ClaimRequest, response protocol.ClaimResponse) error {
	if response.RunID != runID || response.Generation != request.Generation || response.ClaimID != request.ClaimID {
		return invalidResponse("claim", "run_id, generation, or claim_id does not match the request")
	}
	if response.TaskID == "" || response.LeaseToken == "" || response.LeaseExpiresAt.IsZero() {
		return invalidResponse("claim", "task_id, lease_token, and lease_expires_at are required")
	}
	if err := validateWork(response.Work); err != nil {
		return invalidResponse("claim", err.Error())
	}
	return nil
}

func validateLeaseHeartbeatResponse(response protocol.LeaseHeartbeatResponse) error {
	if response.LeaseExpiresAt.IsZero() || response.Commands == nil {
		return invalidResponse("lease heartbeat", "lease_expires_at and commands are required")
	}
	return validateCommands("lease heartbeat", response.Commands)
}

func validateReconcileResponse(response protocol.ReconcileResponse) error {
	if response.Decisions == nil || response.Assignments == nil || response.Commands == nil {
		return invalidResponse("reconcile", "decisions, assignments, and commands are required")
	}
	for _, decision := range response.Decisions {
		if decision.RunID == "" || decision.Generation <= 0 || !isReconcileDecision(decision.Decision) {
			return invalidResponse("reconcile", "each decision requires run_id, generation, and a known decision")
		}
		if decision.Decision == protocol.ReconcileContinue && (decision.LeaseExpiresAt == nil || decision.LeaseExpiresAt.IsZero()) {
			return invalidResponse("reconcile", "continue decisions require lease_expires_at")
		}
	}
	return validateSnapshotParts("reconcile", response.Assignments, response.Commands)
}

func validateTaskResponse(operation, expectedTaskID string, response protocol.Task) error {
	if response.TaskID == "" || response.State == "" {
		return invalidResponse(operation, "task_id and state are required")
	}
	if expectedTaskID != "" && response.TaskID != expectedTaskID {
		return invalidResponse(operation, "task_id does not match the request")
	}
	if response.Work != nil {
		if err := validateWork(*response.Work); err != nil {
			return invalidResponse(operation, err.Error())
		}
	}
	return nil
}

func validateSnapshotParts(operation string, assignments []protocol.Assignment, commands []protocol.Command) error {
	for _, assignment := range assignments {
		if assignment.RunID == "" || assignment.TaskID == "" || assignment.Generation <= 0 || assignment.AssignmentExpiresAt.IsZero() {
			return invalidResponse(operation, "each assignment requires identifiers, generation, and expiry")
		}
		if err := validateWork(assignment.Work); err != nil {
			return invalidResponse(operation, err.Error())
		}
	}
	return validateCommands(operation, commands)
}

func validateCommands(operation string, commands []protocol.Command) error {
	for _, command := range commands {
		if command.CommandID == "" || command.RunID == "" || command.Generation <= 0 || command.Kind == "" || command.IssuedAt.IsZero() {
			return invalidResponse(operation, "each command requires identifiers, generation, kind, and issued_at")
		}
		if err := validateJSONValue(command.Payload); err != nil {
			return invalidResponse(operation, "command payload "+err.Error())
		}
	}
	return nil
}

func validateWork(work protocol.Work) error {
	if work.Goal == "" || work.AgentProfile == "" || work.Workspace == "" {
		return fmt.Errorf("work goal, agent_profile, and workspace are required")
	}
	if err := validateJSONValue(work.Input); err != nil {
		return fmt.Errorf("work input %w", err)
	}
	return nil
}

func validateJSONValue(value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("must be non-null JSON")
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return fmt.Errorf("must be valid JSON: %w", err)
	}
	return nil
}

func invalidResponse(operation, reason string) error {
	return fmt.Errorf("invalid %s response: %s", operation, reason)
}

func isReconcileDecision(value protocol.ReconcileDecisionKind) bool {
	switch value {
	case protocol.ReconcileContinue,
		protocol.ReconcileCancel,
		protocol.ReconcileStaleStop,
		protocol.ReconcileTerminal,
		protocol.ReconcileUnknownStop:
		return true
	default:
		return false
	}
}
