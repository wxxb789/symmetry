package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func validateEnrollResponse(request protocol.EnrollRequest, response protocol.EnrollResponse) error {
	if response.MachineID == "" || response.MachineToken == "" {
		return invalidResponse("enroll", "machine_id and machine_token are required")
	}
	if err := validatePathID("machine ID", response.MachineID); err != nil {
		return invalidResponse("enroll", err.Error())
	}
	if response.MachineToken != request.MachineToken {
		return invalidResponse("enroll", "machine_token does not match the request")
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
	if response.HeartbeatIntervalMS <= 0 || response.PollIntervalMS <= 0 || response.WebSocketPath == "" {
		return invalidResponse("session registration", "intervals and websocket_path are required")
	}
	if response.LeaseDurationMS < protocol.MinimumLeaseDurationMS {
		return invalidResponse("session registration", "lease_duration_ms must be at least 30000")
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

func validateTransitionResponse(runID string, request protocol.StateTransitionRequest, response protocol.Run) error {
	for _, field := range []string{
		"run_id", "task_id", "runtime_id", "generation", "state", "claim_id", "lease_token", "lease_expires_at", "result", "failure",
	} {
		if !response.HasField(field) {
			return invalidResponse("transition", field+" is required")
		}
	}
	if response.RunID == "" || response.TaskID == "" || response.RuntimeID == "" || response.Generation <= 0 || response.State == "" || response.ClaimID == "" || response.LeaseToken == "" || response.LeaseExpiresAt.IsZero() {
		return invalidResponse("transition", "run identifiers, state, fence, and lease_expires_at must be non-null")
	}
	if response.RunID != runID || response.RuntimeID != request.RuntimeID || response.Generation != request.Generation || response.ClaimID != request.ClaimID || response.LeaseToken != request.LeaseToken {
		return invalidResponse("transition", "run_id or fence does not match the request")
	}
	if response.State != request.State {
		return invalidResponse("transition", "state does not match the request")
	}
	if !isTransitionState(response.State) {
		return invalidResponse("transition", "state is not recognized")
	}
	if err := validateNullableJSONObject(response.Result); err != nil {
		return invalidResponse("transition", "result "+err.Error())
	}
	if err := validateNullableJSONObject(response.Failure); err != nil {
		return invalidResponse("transition", "failure "+err.Error())
	}
	return validateTerminalPayloads("transition", response.State, response.Result, response.Failure)
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
	for _, field := range []string{"task_id", "state", "run_id", "generation", "work", "result", "failure", "waiting", "latest_command"} {
		if !response.HasField(field) {
			return invalidResponse(operation, field+" is required")
		}
	}
	if response.TaskID == "" || response.State == "" || response.Generation == nil || response.Work == nil {
		return invalidResponse(operation, "task_id, state, generation, and work must be non-null")
	}
	if expectedTaskID != "" && response.TaskID != expectedTaskID {
		return invalidResponse(operation, "task_id does not match the request")
	}
	if err := validateWork(*response.Work); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateNullableJSONObject(response.Result); err != nil {
		return invalidResponse(operation, "result "+err.Error())
	}
	if err := validateNullableJSONObject(response.Failure); err != nil {
		return invalidResponse(operation, "failure "+err.Error())
	}
	if !isTaskState(response.State) {
		return invalidResponse(operation, "state is not recognized")
	}
	if response.RunID == nil {
		if *response.Generation != 0 {
			return invalidResponse(operation, "runless task must have generation zero")
		}
	} else if *response.RunID == "" || *response.Generation <= 0 {
		return invalidResponse(operation, "run_id requires a positive generation")
	}
	if requiresTaskRun(response.State) && response.RunID == nil {
		return invalidResponse(operation, "state requires a run_id")
	}
	if err := validateTerminalPayloads(operation, response.State, response.Result, response.Failure); err != nil {
		return err
	}
	if response.Waiting == nil {
		if response.State == "waiting_for_input" {
			return invalidResponse(operation, "waiting_for_input state requires waiting")
		}
	} else {
		if response.State != "waiting_for_input" {
			return invalidResponse(operation, "only waiting_for_input state may include waiting")
		}
		if err := validateTaskWaiting(response, *response.Waiting); err != nil {
			return invalidResponse(operation, "waiting "+err.Error())
		}
	}
	if response.LatestCommand != nil {
		if err := validateTaskCommandResource(response.TaskID, *response.LatestCommand); err != nil {
			return invalidResponse(operation, "latest_command "+err.Error())
		}
	}
	return nil
}

func validateTerminalPayloads(operation, state string, result, failure json.RawMessage) error {
	if state == "completed" && isJSONNull(result) {
		return invalidResponse(operation, "completed state requires result")
	}
	if state == "failed" && isJSONNull(failure) {
		return invalidResponse(operation, "failed state requires failure")
	}
	if state != "completed" && !isJSONNull(result) {
		return invalidResponse(operation, "only completed state may include result")
	}
	if state != "failed" && !isJSONNull(failure) {
		return invalidResponse(operation, "only failed state may include failure")
	}
	return nil
}

func validateTaskCommandRequest(request protocol.TaskCommandRequest) error {
	switch request.Kind {
	case "cancel":
		if len(bytes.TrimSpace(request.Payload)) != 0 {
			return errors.New("cancel command must omit payload")
		}
	case "provide_input":
		if err := validateJSONObject(request.Payload); err != nil {
			return fmt.Errorf("provide_input payload %w", err)
		}
	default:
		return errors.New("task command kind is not recognized")
	}
	return nil
}

func validateTaskCommandResponse(operation, expectedTaskID string, request protocol.TaskCommandRequest, response protocol.TaskCommand) error {
	if err := validateTaskCommandResource(expectedTaskID, response); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if response.Kind != request.Kind || !sameCommandPayload(request, response.Payload) {
		return invalidResponse(operation, "kind or payload does not match the request")
	}
	return nil
}

func validateAcknowledgementResponse(commandID string, request protocol.CommandAcknowledgement, response protocol.TaskCommand) error {
	if err := validateTaskCommandResource("", response); err != nil {
		return invalidResponse("acknowledge command", err.Error())
	}
	if response.CommandID != commandID || response.RunID == nil || *response.RunID != request.RunID || response.Generation == nil || *response.Generation != request.Generation {
		return invalidResponse("acknowledge command", "command_id, run_id, or generation does not match the request")
	}
	if response.State != "acknowledged" {
		return invalidResponse("acknowledge command", "state must be acknowledged")
	}
	if response.AcknowledgementID == nil || *response.AcknowledgementID != request.AckID || response.AcknowledgementOutcome == nil || *response.AcknowledgementOutcome != request.Outcome {
		return invalidResponse("acknowledge command", "acknowledgement_id or outcome does not match the request")
	}
	return nil
}

func validateTaskWaiting(task protocol.Task, waiting protocol.TaskWaiting) error {
	for _, field := range []string{"run_id", "generation", "transition_id", "question", "payload", "recorded_at"} {
		if !waiting.HasField(field) {
			return errors.New(field + " is required")
		}
	}
	if waiting.RunID == "" || waiting.Generation <= 0 || waiting.TransitionID == "" || waiting.RecordedAt.IsZero() {
		return errors.New("run_id, generation, transition_id, and recorded_at must be non-null")
	}
	if task.RunID == nil || task.Generation == nil || waiting.RunID != *task.RunID || waiting.Generation != *task.Generation {
		return errors.New("run_id and generation must match the current run")
	}
	if err := validateJSONObject(waiting.Payload); err != nil {
		return fmt.Errorf("payload %w", err)
	}
	return nil
}

func validateTaskCommandResource(expectedTaskID string, response protocol.TaskCommand) error {
	for _, field := range []string{
		"command_id", "task_id", "run_id", "generation", "kind", "payload", "state", "issued_at", "applied_at",
		"acknowledgement_id", "acknowledgement_outcome", "acknowledged_at",
	} {
		if !response.HasField(field) {
			return errors.New(field + " is required")
		}
	}
	if response.CommandID == "" || response.TaskID == "" || response.IssuedAt.IsZero() {
		return errors.New("command_id, task_id, and issued_at must be non-null")
	}
	if expectedTaskID != "" && response.TaskID != expectedTaskID {
		return errors.New("task_id does not match expected task")
	}
	if (response.RunID == nil && response.Generation != nil) || (response.RunID != nil && response.Generation == nil) {
		return errors.New("run_id and generation must be null together")
	}
	runBound := response.RunID != nil
	if runBound && (*response.RunID == "" || *response.Generation <= 0) {
		return errors.New("run_id requires a positive generation")
	}
	if response.Kind != "cancel" && response.Kind != "provide_input" {
		return errors.New("kind is not recognized")
	}
	if err := validateJSONObject(response.Payload); err != nil {
		return fmt.Errorf("payload %w", err)
	}
	if response.Kind == "cancel" && !sameJSONObject(response.Payload, json.RawMessage(`{}`)) {
		return errors.New("cancel command payload must be an empty JSON object")
	}
	if response.State != "pending" && response.State != "applied" && response.State != "acknowledged" {
		return errors.New("state is not recognized")
	}
	acknowledgementCount := 0
	if response.AcknowledgementID != nil {
		acknowledgementCount++
	}
	if response.AcknowledgementOutcome != nil {
		acknowledgementCount++
	}
	if response.AcknowledgedAt != nil {
		acknowledgementCount++
	}
	if acknowledgementCount != 0 && acknowledgementCount != 3 {
		return errors.New("acknowledgement fields must be null together")
	}
	if response.AcknowledgementID != nil && (*response.AcknowledgementID == "" || *response.AcknowledgementOutcome == "" || response.AcknowledgedAt.IsZero()) {
		return errors.New("acknowledgement fields are invalid")
	}
	if response.AcknowledgementOutcome != nil && *response.AcknowledgementOutcome != "applied" && *response.AcknowledgementOutcome != "rejected" && *response.AcknowledgementOutcome != "failed" {
		return errors.New("acknowledgement outcome is not recognized")
	}
	if !runBound && (response.Kind != "cancel" || response.State != "applied") {
		return errors.New("runless command must be an applied cancel")
	}
	if response.Kind == "provide_input" && !runBound {
		return errors.New("provide_input command requires a run_id")
	}
	switch response.State {
	case "pending":
		if !runBound || response.AppliedAt != nil || acknowledgementCount != 0 {
			return errors.New("pending command must be run-bound without applied_at or acknowledgement")
		}
	case "applied":
		if response.Kind != "cancel" || response.AppliedAt == nil || response.AppliedAt.IsZero() || acknowledgementCount != 0 {
			return errors.New("applied command must be a cancel with applied_at and no acknowledgement")
		}
	case "acknowledged":
		if !runBound || response.AppliedAt != nil || acknowledgementCount != 3 {
			return errors.New("acknowledged command must be run-bound with acknowledgement and no applied_at")
		}
	}
	return nil
}

func sameCommandPayload(request protocol.TaskCommandRequest, response json.RawMessage) bool {
	if request.Kind == "cancel" {
		return sameJSONObject(json.RawMessage(`{}`), response)
	}
	return sameJSONObject(request.Payload, response)
}

func sameJSONObject(left, right json.RawMessage) bool {
	var leftValue, rightValue map[string]any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return false
	}
	return sameJSONValue(leftValue, rightValue)
}

func sameJSONValue(left, right any) bool {
	switch leftValue := left.(type) {
	case nil:
		return right == nil
	case bool:
		rightValue, ok := right.(bool)
		return ok && leftValue == rightValue
	case string:
		rightValue, ok := right.(string)
		return ok && leftValue == rightValue
	case json.Number:
		rightValue, ok := right.(json.Number)
		return ok && sameJSONNumber(leftValue, rightValue)
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index := range leftValue {
			if !sameJSONValue(leftValue[index], rightValue[index]) {
				return false
			}
		}
		return true
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, leftItem := range leftValue {
			rightItem, ok := rightValue[key]
			if !ok || !sameJSONValue(leftItem, rightItem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sameJSONNumber(left, right json.Number) bool {
	leftValue, leftOK := new(big.Rat).SetString(left.String())
	rightValue, rightOK := new(big.Rat).SetString(right.String())
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
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
		switch command.Kind {
		case "cancel":
			if !sameJSONObject(command.Payload, json.RawMessage(`{}`)) {
				return invalidResponse(operation, "cancel command payload must be an empty JSON object")
			}
		case "provide_input":
			if err := validateJSONObject(command.Payload); err != nil {
				return invalidResponse(operation, "provide_input command payload "+err.Error())
			}
		default:
			return invalidResponse(operation, "command kind is not recognized")
		}
	}
	return nil
}

func validateWork(work protocol.Work) error {
	if work.Goal == "" || work.AgentProfile == "" || work.Workspace == "" {
		return fmt.Errorf("work goal, agent_profile, and workspace are required")
	}
	if !work.HasField("input") {
		return errors.New("work input is required")
	}
	if err := validateNullableJSONObject(work.Input); err != nil {
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

func validateNullableJSONObject(value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return errors.New("must be present")
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	return validateJSONObject(trimmed)
}

func validateJSONObject(value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("must be a non-null JSON object")
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &decoded); err != nil || decoded == nil {
		return errors.New("must be a JSON object")
	}
	return nil
}

func isJSONNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func isTaskState(value string) bool {
	switch value {
	case "queued", "assigned", "claimed", "running", "waiting_for_input", "cancelling", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func isTransitionState(value string) bool {
	switch value {
	case "running", "waiting_for_input", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func requiresTaskRun(state string) bool {
	switch state {
	case "assigned", "claimed", "running", "waiting_for_input", "cancelling", "completed", "failed":
		return true
	default:
		return false
	}
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
