package control

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

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
	if response.HasField("provider_access") && response.ProviderAccess == nil {
		return invalidResponse("claim", "provider_access must be non-null when present")
	}
	if response.ProviderAccess != nil {
		if err := validateProviderAccess(*response.ProviderAccess); err != nil {
			return invalidResponse("claim", "provider_access "+err.Error())
		}
	}
	if response.HasField("harness_session_id") && response.HarnessSessionID != nil {
		if err := validateGoalUUID(*response.HarnessSessionID, "harness_session_id"); err != nil {
			return invalidResponse("claim", err.Error())
		}
	}
	if response.HasField("harness_binding_id") && response.HarnessBindingID != nil {
		if err := validateGoalUUID(*response.HarnessBindingID, "harness_binding_id"); err != nil {
			return invalidResponse("claim", err.Error())
		}
	}
	if mode, goalAdmission := claimGoalAdmissionSessionMode(response.Work); goalAdmission {
		switch mode {
		case protocol.SessionModeResume:
			if !response.HasField("harness_session_id") || !response.HasField("harness_binding_id") || response.HarnessSessionID == nil || response.HarnessBindingID == nil {
				return invalidResponse("claim", "resume Goal claim requires harness_session_id and harness_binding_id")
			}
		case protocol.SessionModeFresh, protocol.SessionModeHandoff:
			if response.HarnessSessionID != nil || response.HarnessBindingID != nil {
				return invalidResponse("claim", "fresh and handoff Goal claims must not reserve a harness session")
			}
		}
	}
	return nil
}

func claimGoalAdmissionSessionMode(work protocol.Work) (protocol.SessionMode, bool) {
	input := bytes.TrimSpace(work.Input)
	if len(input) == 0 || input[0] != '{' {
		return "", false
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(input, &envelope); err != nil {
		return "", false
	}
	if nested, present := envelope["goal_admission"]; present {
		input = bytes.TrimSpace(nested)
	}
	admission, err := protocol.ParseAdmission(input)
	if err != nil {
		return "", false
	}
	return admission.SessionMode, true
}

func validateProviderAccess(access protocol.ProviderAccess) error {
	if access.Path != "/api/v1/provider-actions" {
		return errors.New("path is invalid")
	}
	if strings.TrimSpace(access.Token) == "" || len(access.Token) > 65536 {
		return errors.New("token is invalid")
	}
	if len(access.Grants) == 0 {
		return errors.New("grants must not be empty")
	}
	for _, grant := range access.Grants {
		if strings.TrimSpace(grant.ResourceID) == "" || len(grant.ResourceID) > 4096 {
			return errors.New("grant resource_id is invalid")
		}
		switch grant.Provider {
		case "github", "azure_devops":
		default:
			return errors.New("grant provider is invalid")
		}
		switch grant.Kind {
		case "repository", "work_tracking", "ci":
		default:
			return errors.New("grant kind is invalid")
		}
		if len(grant.Operations) == 0 {
			return errors.New("grant operations must not be empty")
		}
		seen := make(map[string]struct{}, len(grant.Operations))
		for _, operation := range grant.Operations {
			switch operation {
			case "resource.sync", "change.upsert", "change.update":
			default:
				return errors.New("grant operation is invalid")
			}
			if _, duplicate := seen[operation]; duplicate {
				return errors.New("grant operations contain a duplicate")
			}
			if operation != "resource.sync" && grant.Kind != "repository" {
				return errors.New("change grant operations require repository kind")
			}
			seen[operation] = struct{}{}
		}
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
	if *response.Generation <= 0 {
		return invalidResponse(operation, "task generation must be positive")
	}
	if response.RunID != nil && *response.RunID == "" {
		return invalidResponse(operation, "run_id must be non-empty when present")
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
	case "guidance", "pause", "resume":
		if request.Generation <= 0 {
			return errors.New("supervisory command requires a positive generation")
		}
		return ValidateSupervisoryPayload(request.Kind, request.Payload)
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
	// Cancelling queued work applies to the task without creating a run. The
	// server validates its generation fence, while the command retains nil run IDs.
	runlessCancellation := response.Kind == "cancel" && response.State == "applied" && response.RunID == nil && response.Generation == nil
	if request.Generation > 0 && !runlessCancellation && (response.Generation == nil || *response.Generation != request.Generation) {
		return invalidResponse(operation, "generation does not match the request")
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
	supervisory := isSupervisoryCommand(response.Kind)
	if response.Kind != "cancel" && response.Kind != "provide_input" && response.Kind != "retry" && !supervisory {
		return errors.New("kind is not recognized")
	}
	if err := validateJSONObject(response.Payload); err != nil {
		return fmt.Errorf("payload %w", err)
	}
	if response.Kind == "cancel" && !sameJSONObject(response.Payload, json.RawMessage(`{}`)) {
		return errors.New("cancel command payload must be an empty JSON object")
	}
	if supervisory {
		if err := ValidateSupervisoryPayload(response.Kind, response.Payload); err != nil {
			return err
		}
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
	// Cancellation can settle outstanding input/control intents before a daemon
	// receipt exists. This is historical state, never proof of a matching ack.
	serverRejected := (supervisory || response.Kind == "provide_input") && response.State == "acknowledged" &&
		response.AcknowledgementID == nil && response.AcknowledgementOutcome != nil && *response.AcknowledgementOutcome == "rejected" &&
		response.AcknowledgedAt != nil && !response.AcknowledgedAt.IsZero() && response.AppliedAt == nil
	if acknowledgementCount != 0 && acknowledgementCount != 3 && !serverRejected {
		return errors.New("acknowledgement fields must be null together")
	}
	if response.AcknowledgementID != nil && (*response.AcknowledgementID == "" || *response.AcknowledgementOutcome == "" || response.AcknowledgedAt.IsZero()) {
		return errors.New("acknowledgement fields are invalid")
	}
	if response.AcknowledgementOutcome != nil && *response.AcknowledgementOutcome != "applied" && *response.AcknowledgementOutcome != "rejected" && *response.AcknowledgementOutcome != "failed" {
		return errors.New("acknowledgement outcome is not recognized")
	}
	controlApplied := response.Kind == "cancel" || response.Kind == "retry"
	if !runBound && (!controlApplied || response.State != "applied") {
		return errors.New("runless command must be an applied cancel or retry")
	}
	if response.Kind == "provide_input" && !runBound {
		return errors.New("provide_input command requires a run_id")
	}
	switch response.State {
	case "pending":
		if response.Kind == "retry" || !runBound || response.AppliedAt != nil || acknowledgementCount != 0 {
			return errors.New("pending command must be run-bound without applied_at or acknowledgement")
		}
	case "applied":
		if (!controlApplied && !supervisory && response.Kind != "provide_input") || response.AppliedAt == nil || response.AppliedAt.IsZero() || acknowledgementCount != 0 {
			return errors.New("applied command requires a supported kind, applied_at, and no acknowledgement")
		}
	case "acknowledged":
		retainedApplication := (supervisory || response.Kind == "provide_input") && response.AcknowledgementOutcome != nil && *response.AcknowledgementOutcome == "applied" && response.AppliedAt != nil && !response.AppliedAt.IsZero()
		if response.Kind == "retry" || !runBound || (response.AppliedAt != nil && !retainedApplication) || (acknowledgementCount != 3 && !serverRejected) {
			return errors.New("acknowledged command must be run-bound with acknowledgement and no unrelated applied_at")
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
		case "guidance", "pause", "resume":
			if err := ValidateSupervisoryPayload(command.Kind, command.Payload); err != nil {
				return invalidResponse(operation, err.Error())
			}
		default:
			return invalidResponse(operation, "command kind is not recognized")
		}
	}
	return nil
}

func isSupervisoryCommand(kind string) bool {
	return kind == "guidance" || kind == "pause" || kind == "resume"
}

// ValidateSupervisoryPayload validates the payload of a recognized supervisory command.
func ValidateSupervisoryPayload(kind string, payload json.RawMessage) error {
	if err := validateJSONObject(payload); err != nil {
		return fmt.Errorf("%s command payload %w", kind, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return err
	}
	if kind == "guidance" {
		var message string
		if len(object) != 1 || json.Unmarshal(object["message"], &message) != nil || strings.TrimSpace(message) == "" || len(message) > 32768 {
			return errors.New("guidance payload must contain only a non-empty message of at most 32768 bytes")
		}
	} else if len(object) != 0 {
		return fmt.Errorf("%s command payload must be an empty JSON object", kind)
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
	if work.HasField("required_capabilities") && work.RequiredCapabilities == nil {
		return errors.New("work required_capabilities must be a non-null boolean map")
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
	case "queued", "assigned", "claimed", "running", "paused", "waiting_for_input", "cancelling", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func isTransitionState(value string) bool {
	switch value {
	case "running", "paused", "waiting_for_input", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func requiresTaskRun(state string) bool {
	switch state {
	case "assigned", "claimed", "running", "paused", "waiting_for_input", "cancelling", "completed", "failed":
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

// decodeStrictJSON is reserved for the additive Goal machine endpoints. The
// legacy decodeJSON helper intentionally remains tolerant for protocol-v1
// response evolution.
func decodeStrictJSON(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("response must contain one JSON value")
		}
		return err
	}
	return nil
}

func decodeStrictObjectJSON(value []byte, target any, required ...string) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("response must be a JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return err
	}
	for _, field := range required {
		if _, present := fields[field]; !present {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return decodeStrictJSON(trimmed, target)
}

func validateGoalFence(runID string, fence protocol.Fence) error {
	if err := validateGoalUUID(runID, "run ID"); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"runtime ID":  fence.RuntimeID,
		"claim ID":    fence.ClaimID,
		"lease token": fence.LeaseToken,
	} {
		if err := validateGoalUUID(value, field); err != nil {
			return err
		}
	}
	if fence.RuntimeEpoch <= 0 || fence.Generation <= 0 {
		return errors.New("fence runtime_epoch and generation must be positive")
	}
	return nil
}

func validateGoalSessionAttach(runID string, request GoalSessionAttachRequest) error {
	if err := validateGoalFence(runID, request.Fence); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"local handle ID":       request.LocalHandleID,
		"harness version":       request.HarnessVersion,
		"adapter version":       request.AdapterVersion,
		"workspace fingerprint": request.WorkspaceFingerprint,
		"workspace":             request.Workspace,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be empty", field)
		}
	}
	switch request.HarnessKind {
	case "codex", "claude_code", "pi", "opencode":
	default:
		return fmt.Errorf("harness_kind %q is invalid", request.HarnessKind)
	}
	if request.RepositoryResourceID != nil {
		if err := validateGoalUUID(*request.RepositoryResourceID, "repository resource ID"); err != nil {
			return err
		}
	}
	if err := validateGoalUUID(request.LocalHandleID, "local handle ID"); err != nil {
		return err
	}
	if request.BindingID != nil {
		if err := validateGoalUUID(*request.BindingID, "binding ID"); err != nil {
			return err
		}
	}
	return nil
}

func validateGoalSessionStopped(runID string, request GoalSessionStoppedRequest) error {
	if err := validateGoalFence(runID, request.Fence); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"session ID":      request.SessionID,
		"local handle ID": request.LocalHandleID,
		"binding ID":      request.BindingID,
	} {
		if err := validateGoalUUID(value, field); err != nil {
			return err
		}
	}
	return nil
}

func validateGoalEvidence(runID string, fence protocol.Fence, evidence protocol.Evidence) error {
	if err := validateGoalFence(runID, fence); err != nil {
		return err
	}
	if evidence.RunID != runID {
		return errors.New("evidence run_id does not match the path run ID")
	}
	if err := evidence.Validate(); err != nil {
		return fmt.Errorf("invalid evidence request: %w", err)
	}
	return nil
}

func validateGoalUsage(runID string, fence protocol.Fence, usage protocol.Usage) error {
	if err := validateGoalFence(runID, fence); err != nil {
		return err
	}
	if usage.RunID != runID {
		return errors.New("usage run_id does not match the path run ID")
	}
	if err := usage.Validate(); err != nil {
		return fmt.Errorf("invalid usage request: %w", err)
	}
	return nil
}

func validateGoalSessionReceipt(runID string, request GoalSessionAttachRequest, receipt GoalSessionReceipt) error {
	if err := validateGoalFence(runID, request.Fence); err != nil {
		return invalidResponse("attach session", err.Error())
	}
	if err := validateGoalSessionAttachmentReceipt("attach session", runID, request.Fence, receipt); err != nil {
		return err
	}
	if receipt.LocalHandleID != request.LocalHandleID {
		return invalidResponse("attach session", "session local_handle_id does not match the request")
	}
	if request.BindingID != nil && receipt.BindingID != *request.BindingID {
		return invalidResponse("attach session", "session binding_id does not match the request")
	}
	if receipt.HarnessKind != request.HarnessKind || receipt.HarnessVersion != request.HarnessVersion ||
		receipt.AdapterVersion != request.AdapterVersion || receipt.WorkspaceFingerprint != request.WorkspaceFingerprint {
		return invalidResponse("attach session", "session harness or workspace identity does not match the request")
	}
	if receipt.Workspace != "" && receipt.Workspace != request.Workspace {
		return invalidResponse("attach session", "session workspace does not match the request")
	}
	if request.RepositoryResourceID != nil && receipt.RepositoryResourceID != *request.RepositoryResourceID {
		return invalidResponse("attach session", "session repository_resource_id does not match the request")
	}
	return nil
}

func validateGoalSessionAttachmentReadback(runID string, fence protocol.Fence, receipt GoalSessionReceipt) error {
	return validateGoalSessionAttachmentReceipt("fetch session attachment", runID, fence, receipt)
}

func validateGoalSessionAttachmentReceipt(operation, runID string, fence protocol.Fence, receipt GoalSessionReceipt) error {
	if err := validateGoalFence(runID, fence); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateGoalUUID(receipt.ID, "session.id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateGoalUUID(receipt.AttachmentReceiptID, "session.attachment_receipt_id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateGoalUUID(receipt.RunID, "session.run_id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if receipt.RunID != runID {
		return invalidResponse(operation, "session run_id does not match the path run ID")
	}
	if receipt.State != "busy" {
		return invalidResponse(operation, "session state must be busy")
	}
	if receipt.SessionID == "" || receipt.GoalID == "" || receipt.TaskID == "" || receipt.MachineID == "" || receipt.RepositoryResourceID == "" ||
		receipt.RuntimeID == "" || receipt.ActiveRunID == "" || receipt.LocalHandleID == "" ||
		receipt.HarnessKind == "" || receipt.HarnessVersion == "" || receipt.AdapterVersion == "" ||
		receipt.WorkspaceFingerprint == "" {
		return invalidResponse(operation, "session receipt is missing immutable identity fields")
	}
	for field, value := range map[string]string{
		"session.goal_id":                receipt.GoalID,
		"session.task_id":                receipt.TaskID,
		"session.machine_id":             receipt.MachineID,
		"session.repository_resource_id": receipt.RepositoryResourceID,
		"session.local_handle_id":        receipt.LocalHandleID,
	} {
		if err := validateGoalUUID(value, field); err != nil {
			return invalidResponse(operation, err.Error())
		}
	}
	if err := validateGoalUUID(receipt.RuntimeID, "session.runtime_id"); err != nil || receipt.RuntimeID != fence.RuntimeID {
		return invalidResponse(operation, "session runtime_id does not match the fence")
	}
	if err := validateGoalUUID(receipt.ActiveRunID, "session.active_run_id"); err != nil || receipt.ActiveRunID != runID {
		return invalidResponse(operation, "session active_run_id does not match the run")
	}
	if err := validateGoalUUID(receipt.LocalHandleID, "session.local_handle_id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateGoalUUID(receipt.BindingID, "session.binding_id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if err := validateGoalUUID(receipt.SessionID, "session.session_id"); err != nil {
		return invalidResponse(operation, err.Error())
	}
	if receipt.SessionID != receipt.ID {
		return invalidResponse(operation, "session session_id does not match id")
	}
	if receipt.LockVersion < 0 {
		return invalidResponse(operation, "session lock_version is invalid")
	}
	for field, value := range map[string]string{
		"session.inserted_at": receipt.InsertedAt,
		"session.updated_at":  receipt.UpdatedAt,
	} {
		if value != "" {
			if err := validateGoalUTCTimestamp(value, field); err != nil {
				return invalidResponse(operation, err.Error())
			}
		}
	}
	return nil
}

func validateGoalSessionStoppedReceipt(runID string, request GoalSessionStoppedRequest, receipt GoalSessionStoppedReceipt) error {
	if err := validateGoalSessionStopped(runID, request); err != nil {
		return invalidResponse("session stopped", err.Error())
	}
	for field, value := range map[string]string{
		"session_stopped.receipt_id":      receipt.ReceiptID,
		"session_stopped.run_id":          receipt.RunID,
		"session_stopped.session_id":      receipt.SessionID,
		"session_stopped.local_handle_id": receipt.LocalHandleID,
		"session_stopped.binding_id":      receipt.BindingID,
	} {
		if err := validateGoalUUID(value, field); err != nil {
			return invalidResponse("session stopped", err.Error())
		}
	}
	if receipt.RunID != runID || receipt.SessionID != request.SessionID || receipt.LocalHandleID != request.LocalHandleID || receipt.BindingID != request.BindingID {
		return invalidResponse("session stopped", "receipt identity does not match the request")
	}
	if receipt.State != "available" || receipt.ActiveRunID != nil || receipt.LockVersion <= 0 {
		return invalidResponse("session stopped", "receipt must release the session to available")
	}
	return nil
}

func validateGoalEvidenceReceipt(runID string, evidence protocol.Evidence, receipt GoalEvidenceReceipt) error {
	if err := validateGoalUUID(receipt.ID, "evidence.id"); err != nil || receipt.ID != evidence.EvidenceID || receipt.RunID != runID {
		return invalidResponse("evidence", "id and run_id must match the request and path run ID")
	}
	if err := validateGoalUUID(receipt.RunID, "evidence.run_id"); err != nil || receipt.RunID != evidence.RunID {
		return invalidResponse("evidence", "run_id must match the request and path run ID")
	}
	if receipt.EvidenceKey != evidence.EvidenceKey {
		return invalidResponse("evidence", "evidence_key does not match the request")
	}
	if err := validateGoalShortIdentifier(receipt.EvidenceKey, "evidence.evidence_key"); err != nil {
		return invalidResponse("evidence", err.Error())
	}
	if receipt.Kind != string(evidence.Kind) {
		return invalidResponse("evidence", "kind does not match the request")
	}
	if receipt.Verdict != string(evidence.Verdict) {
		return invalidResponse("evidence", "verdict does not match the request")
	}
	if receipt.SubjectHash != evidence.SubjectHash {
		return invalidResponse("evidence", "subject_hash does not match the request")
	}
	if !validGoalDigest(receipt.SubjectHash) {
		return invalidResponse("evidence", "subject_hash is invalid")
	}
	return nil
}

func validateGoalUsageReceipt(runID string, usage protocol.Usage, receipt GoalUsageReceipt) error {
	if err := validateGoalUUID(receipt.ID, "usage.id"); err != nil || receipt.ID != usage.UsageID || receipt.RunID != runID {
		return invalidResponse("usage", "id and run_id must match the request and path run ID")
	}
	if err := validateGoalUUID(receipt.RunID, "usage.run_id"); err != nil || receipt.RunID != usage.RunID {
		return invalidResponse("usage", "run_id must match the request and path run ID")
	}
	if receipt.UsageKey != usage.UsageKey {
		return invalidResponse("usage", "usage_key does not match the request")
	}
	if err := validateGoalShortIdentifier(receipt.UsageKey, "usage.usage_key"); err != nil {
		return invalidResponse("usage", err.Error())
	}
	if receipt.CostBasis != string(usage.CostBasis) {
		return invalidResponse("usage", "cost_basis does not match the request")
	}
	if !equalGoalOptionalString(receipt.CostMicrousd, usage.CostMicrousd) {
		return invalidResponse("usage", "cost_microusd does not match the request")
	}
	switch receipt.CostBasis {
	case "reported", "estimated":
		if receipt.CostMicrousd == nil {
			return invalidResponse("usage", "reported or estimated cost requires cost_microusd")
		}
	case "unknown":
		if receipt.CostMicrousd != nil {
			if err := protocol.ValidateMicroUSD(*receipt.CostMicrousd); err != nil {
				return invalidResponse("usage", err.Error())
			}
		}
	default:
		return invalidResponse("usage", "cost_basis is not recognized")
	}
	if receipt.CostMicrousd != nil {
		if err := protocol.ValidateMicroUSD(*receipt.CostMicrousd); err != nil {
			return invalidResponse("usage", err.Error())
		}
	}
	return nil
}

func equalGoalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateGoalRunContext(runID string, fence protocol.Fence, response GoalRunContext) error {
	for field, value := range map[string]string{
		"goal_id": response.GoalID,
		"task_id": response.TaskID,
		"run_id":  response.RunID,
	} {
		if err := validateGoalUUID(value, field); err != nil {
			return invalidResponse("run context", err.Error())
		}
	}
	if response.SessionID == nil {
		if response.Context.WorkContract.Purpose != "validate" {
			return invalidResponse("run context", "session_id may be null only for validation context")
		}
	} else if err := validateGoalUUID(*response.SessionID, "session_id"); err != nil {
		return invalidResponse("run context", err.Error())
	}
	if response.RunID != runID || response.Context.GoalID != response.GoalID || response.Generation != fence.Generation {
		return invalidResponse("run context", "run ownership identifiers do not match the request")
	}
	if err := validateGoalSafePositiveInteger(response.Generation, "generation"); err != nil {
		return invalidResponse("run context", err.Error())
	}
	if err := validateGoalFence(runID, fence); err != nil {
		return invalidResponse("run context", err.Error())
	}
	if response.Context.SchemaVersion != goalContextSnapshotSchemaVersion {
		return invalidResponse("run context", "context schema_version is not canonical")
	}
	if response.Context.ApprovedGoal.GoalID != response.Context.GoalID ||
		response.Context.ApprovedGoal.Revision != response.Context.GoalRevision {
		return invalidResponse("run context", "approved_goal identity does not match the snapshot")
	}
	if response.Context.GoalID != response.GoalID {
		return invalidResponse("run context", "context goal_id does not match the run goal_id")
	}
	return nil
}

func validGoalDigest(value string) bool {
	if strings.HasPrefix(value, "sha256:") {
		return len(value) == len("sha256:")+64 && strings.Trim(value[len("sha256:"):], "0123456789abcdef") == ""
	}
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validateGoalUUID(value, field string) error {
	if len(value) != 36 {
		return fmt.Errorf("%s must be a canonical UUID", field)
	}
	for index := 0; index < len(value); index++ {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return fmt.Errorf("%s must be a canonical UUID", field)
			}
			continue
		}
		character := value[index]
		if character >= 'A' && character <= 'F' || character < '0' || character > 'f' || character > '9' && character < 'a' {
			return fmt.Errorf("%s must use lowercase hexadecimal UUID digits", field)
		}
	}
	if value[14] < '1' || value[14] > '5' || !strings.ContainsRune("89ab", rune(value[19])) {
		return fmt.Errorf("%s must use a supported UUID version and variant", field)
	}
	return nil
}

const (
	goalContextSnapshotSchemaVersion = "symmetry.context_snapshot.v1"
	goalAcceptanceSchemaVersion      = "symmetry.acceptance.v1"
)

func validateGoalSafePositiveInteger(value int64, field string) error {
	if value <= 0 || value > protocol.MaxJSONSafeInteger {
		return fmt.Errorf("%s must be a positive JSON-safe integer", field)
	}
	return nil
}

func validateGoalSafeNonNegativeInteger(value int64, field string) error {
	if value < 0 || value > protocol.MaxJSONSafeInteger {
		return fmt.Errorf("%s must be a non-negative JSON-safe integer", field)
	}
	return nil
}

func validateGoalShortIdentifier(value, field string) error {
	if len(value) < 1 || len(value) > 128 {
		return fmt.Errorf("%s must contain 1..128 characters", field)
	}
	for index, character := range value {
		if index == 0 {
			if !goalASCIIAlphaNumeric(character) {
				return fmt.Errorf("%s has an invalid identifier", field)
			}
			continue
		}
		if !goalASCIIAlphaNumeric(character) && !strings.ContainsRune("._:-", character) {
			return fmt.Errorf("%s has an invalid identifier", field)
		}
	}
	return nil
}

func validateGoalLongText(value, field string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must contain valid UTF-8", field)
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 16_384 {
		return fmt.Errorf("%s must contain 1..16384 characters", field)
	}
	return nil
}

func validateGoalUTCTimestamp(value, field string) error {
	if strings.TrimSpace(value) == "" || !strings.HasSuffix(value, "Z") {
		return fmt.Errorf("%s must be an RFC3339 UTC timestamp", field)
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be an RFC3339 UTC timestamp: %w", field, err)
	}
	return nil
}

func validateGoalSHA256(value, field string) error {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") ||
		strings.Trim(value[len("sha256:"):], "0123456789abcdef") != "" {
		return fmt.Errorf("%s must be sha256:<64 lowercase hex characters>", field)
	}
	return nil
}

func validateGoalCommit(value, field string) error {
	if len(value) != 40 && len(value) != 64 {
		return fmt.Errorf("%s must be a full Git object ID", field)
	}
	if strings.Trim(value, "0123456789abcdef") != "" {
		return fmt.Errorf("%s must be a full Git object ID", field)
	}
	return nil
}

func validateGoalCommitPath(value, field string) error {
	if len(value) < 1 || len(value) > 1024 || value[0] == '/' || strings.ContainsRune(value, '\x00') ||
		strings.ContainsRune(value, '\\') || strings.Contains(value, "//") {
		return fmt.Errorf("%s is not a normalized relative commit path", field)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("%s must not traverse parent directories", field)
		}
	}
	return nil
}

func goalASCIIAlphaNumeric(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func validateGoalUniqueShortIdentifiers(values []string, field string) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateGoalShortIdentifier(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s must not contain duplicates", field)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func goalObjectFieldSet(data []byte) map[string]struct{} {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &fields); err != nil {
		return nil
	}
	present := make(map[string]struct{}, len(fields))
	for field := range fields {
		present[field] = struct{}{}
	}
	return present
}

func goalFieldIsNull(data []byte, field string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &fields); err != nil {
		return false
	}
	raw, ok := fields[field]
	return ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func goalFieldPresent(present map[string]struct{}, field string) bool {
	if present == nil {
		return false
	}
	_, ok := present[field]
	return ok
}

func goalAnyFieldPresent(present map[string]struct{}, fields ...string) bool {
	for _, field := range fields {
		if goalFieldPresent(present, field) {
			return true
		}
	}
	return false
}

func validateAuthorityPolicy(value AuthorityPolicy) error {
	if value.AllowedActions == nil || len(value.AllowedActions) > 64 {
		return errors.New("authority_policy.allowed_actions must be an array of at most 64 items")
	}
	return validateGoalUniqueShortIdentifiers(value.AllowedActions, "authority_policy.allowed_actions")
}

func validateApprovedGoal(value ApprovedGoal) error {
	if err := validateGoalUUID(value.GoalID, "approved_goal.goal_id"); err != nil {
		return err
	}
	if err := validateGoalSafePositiveInteger(value.Revision, "approved_goal.revision"); err != nil {
		return err
	}
	if err := validateGoalLongText(value.Objective, "approved_goal.objective"); err != nil {
		return err
	}
	return validateAuthorityPolicy(value.AuthorityPolicy)
}

func validateAcceptancePredicate(value AcceptancePredicate) error {
	if err := validateGoalShortIdentifier(value.ID, "acceptance.predicate.id"); err != nil {
		return err
	}
	switch value.Kind {
	case "check":
		if err := validateGoalShortIdentifier(value.ValidatorProfile, "acceptance.predicate.validator_profile"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "resource_id", "path", "reviewer_profile") || value.ResourceID != "" || value.Path != "" || value.ReviewerProfile != "" {
			return errors.New("check predicate contains fields for another kind")
		}
	case "artifact":
		if err := validateGoalUUID(value.ResourceID, "acceptance.predicate.resource_id"); err != nil {
			return err
		}
		if err := validateGoalCommitPath(value.Path, "acceptance.predicate.path"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "validator_profile", "reviewer_profile") || value.ValidatorProfile != "" || value.ReviewerProfile != "" {
			return errors.New("artifact predicate contains fields for another kind")
		}
	case "review":
		if err := validateGoalShortIdentifier(value.ReviewerProfile, "acceptance.predicate.reviewer_profile"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "validator_profile", "resource_id", "path") || value.ValidatorProfile != "" || value.ResourceID != "" || value.Path != "" {
			return errors.New("review predicate contains fields for another kind")
		}
	case "operator_acceptance":
		if goalAnyFieldPresent(value.present, "validator_profile", "resource_id", "path", "reviewer_profile") || value.ValidatorProfile != "" || value.ResourceID != "" || value.Path != "" || value.ReviewerProfile != "" {
			return errors.New("operator_acceptance predicate contains fields for another kind")
		}
	default:
		return fmt.Errorf("acceptance.predicate.kind %q is invalid", value.Kind)
	}
	return nil
}

func validateAcceptanceContract(value AcceptanceContract) error {
	if value.SchemaVersion != goalAcceptanceSchemaVersion {
		return fmt.Errorf("acceptance.schema_version must be %q", goalAcceptanceSchemaVersion)
	}
	if err := validateGoalLongText(value.Description, "acceptance.description"); err != nil {
		return err
	}
	if value.Predicates == nil || len(value.Predicates) < 1 || len(value.Predicates) > 256 {
		return errors.New("acceptance.predicates must contain 1..256 items")
	}
	for _, predicate := range value.Predicates {
		if err := validateAcceptancePredicate(predicate); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkContract(value WorkContract) error {
	if err := validateGoalLongText(value.Title, "work_contract.title"); err != nil {
		return err
	}
	if err := validateGoalLongText(value.Description, "work_contract.description"); err != nil {
		return err
	}
	switch value.Purpose {
	case "implement", "validate", "plan", "observe", "chat":
	default:
		return fmt.Errorf("work_contract.purpose %q is invalid", value.Purpose)
	}
	if err := validateAcceptanceContract(value.Acceptance); err != nil {
		return err
	}
	if value.ValidationBindings == nil || len(value.ValidationBindings) > 256 {
		return errors.New("work_contract.validation_bindings must be an array of at most 256 items")
	}
	requiredProfiles := make(map[[2]string]struct{})
	for _, predicate := range value.Acceptance.Predicates {
		switch predicate.Kind {
		case "check":
			requiredProfiles[[2]string{"check", predicate.ValidatorProfile}] = struct{}{}
		case "review":
			requiredProfiles[[2]string{"review", predicate.ReviewerProfile}] = struct{}{}
		}
	}
	boundProfiles := make(map[[2]string]struct{}, len(value.ValidationBindings))
	for index, binding := range value.ValidationBindings {
		if err := validateValidationBinding(binding); err != nil {
			return fmt.Errorf("work_contract.validation_bindings[%d]: %w", index, err)
		}
		key := [2]string{binding.Kind, binding.ProfileName}
		if _, exists := boundProfiles[key]; exists {
			return fmt.Errorf("work_contract.validation_bindings[%d]: duplicate profile binding", index)
		}
		if _, required := requiredProfiles[key]; !required {
			return fmt.Errorf("work_contract.validation_bindings[%d]: profile does not match an acceptance predicate", index)
		}
		boundProfiles[key] = struct{}{}
	}
	for key := range requiredProfiles {
		if _, bound := boundProfiles[key]; !bound {
			return fmt.Errorf("work_contract.validation_bindings is missing %s profile %q", key[0], key[1])
		}
	}
	return nil
}

func validateValidationBinding(value ValidationBinding) error {
	if err := validateGoalShortIdentifier(value.ProfileName, "profile_name"); err != nil {
		return err
	}
	switch value.Kind {
	case "check", "review":
	default:
		return fmt.Errorf("kind %q is invalid", value.Kind)
	}
	if err := validateGoalSHA256(value.ProfileDigest, "profile_digest"); err != nil {
		return err
	}
	if value.AllowedRuntimeIDs == nil || len(value.AllowedRuntimeIDs) < 1 || len(value.AllowedRuntimeIDs) > 256 {
		return errors.New("allowed_runtime_ids must contain 1..256 items")
	}
	seen := make(map[string]struct{}, len(value.AllowedRuntimeIDs))
	for index, runtimeID := range value.AllowedRuntimeIDs {
		if err := validateGoalUUID(runtimeID, fmt.Sprintf("allowed_runtime_ids[%d]", index)); err != nil {
			return err
		}
		if _, exists := seen[runtimeID]; exists {
			return errors.New("allowed_runtime_ids must not contain duplicates")
		}
		seen[runtimeID] = struct{}{}
	}
	return nil
}

func validateContextContent(value ContextContent) error {
	switch value.Kind {
	case "excerpt", "path", "evidence", "pointer":
	default:
		return fmt.Errorf("context source content.kind %q is invalid", value.Kind)
	}
	return validateGoalLongText(value.Value, "context source content.value")
}

func validateContextSource(value ContextSource) error {
	if err := validateGoalUUID(value.ResourceID, "context source.resource_id"); err != nil {
		return err
	}
	if err := validateGoalShortIdentifier(value.SourceKind, "context source.source_kind"); err != nil {
		return err
	}
	if err := validateGoalShortIdentifier(value.SourceRevision, "context source.source_revision"); err != nil {
		return err
	}
	if err := validateGoalSHA256(value.ContentHash, "context source.content_hash"); err != nil {
		return err
	}
	if err := validateGoalUTCTimestamp(value.ObservedAt, "context source.observed_at"); err != nil {
		return err
	}
	switch value.Trust {
	case "trusted_policy", "validated_evidence", "repository_untrusted", "advisory":
	default:
		return fmt.Errorf("context source.trust %q is invalid", value.Trust)
	}
	if goalFieldPresent(value.present, "stale") && value.Stale == nil {
		return errors.New("context source.stale must be a boolean when present")
	}
	return validateContextContent(value.Content)
}

func validateDecisionReference(value DecisionReference) error {
	if err := validateGoalUUID(value.DecisionID, "decision.decision_id"); err != nil {
		return err
	}
	if err := validateGoalSHA256(value.ActionHash, "decision.action_hash"); err != nil {
		return err
	}
	switch value.State {
	case "open", "resolved", "superseded":
		return nil
	default:
		return fmt.Errorf("decision.state %q is invalid", value.State)
	}
}

func validateEvidenceReference(value EvidenceReference) error {
	if err := validateGoalUUID(value.EvidenceID, "evidence.evidence_id"); err != nil {
		return err
	}
	if err := validateGoalShortIdentifier(value.PredicateID, "evidence.predicate_id"); err != nil {
		return err
	}
	if err := validateGoalSHA256(value.SubjectHash, "evidence.subject_hash"); err != nil {
		return err
	}
	switch value.Verdict {
	case "passed", "failed", "unknown", "not_applicable":
		return nil
	default:
		return fmt.Errorf("evidence.verdict %q is invalid", value.Verdict)
	}
}

func validateFailedAttempt(value FailedAttempt) error {
	if err := validateGoalUUID(value.TaskID, "failed_attempt.task_id"); err != nil {
		return err
	}
	if value.RunID != nil {
		if err := validateGoalUUID(*value.RunID, "failed_attempt.run_id"); err != nil {
			return err
		}
	}
	if err := validateGoalLongText(value.Reason, "failed_attempt.reason"); err != nil {
		return err
	}
	return validateGoalUTCTimestamp(value.ObservedAt, "failed_attempt.observed_at")
}

func validateBlocker(value Blocker) error {
	switch value.Kind {
	case "decision":
		if err := validateGoalUUID(value.DecisionID, "blocker.decision_id"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "resource_id", "external_ref", "next_check_at", "work_item_ids", "code", "detail") || value.ResourceID != "" || value.ExternalRef != "" || value.NextCheckAt != "" || len(value.WorkItemIDs) != 0 || value.Code != "" || value.Detail != "" {
			return errors.New("decision blocker contains fields for another kind")
		}
	case "external":
		if err := validateGoalUUID(value.ResourceID, "blocker.resource_id"); err != nil {
			return err
		}
		if err := validateGoalLongText(value.ExternalRef, "blocker.external_ref"); err != nil {
			return err
		}
		if err := validateGoalUTCTimestamp(value.NextCheckAt, "blocker.next_check_at"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "decision_id", "work_item_ids", "code", "detail") || value.DecisionID != "" || len(value.WorkItemIDs) != 0 || value.Code != "" || value.Detail != "" {
			return errors.New("external blocker contains fields for another kind")
		}
	case "dependency":
		if len(value.WorkItemIDs) < 1 || len(value.WorkItemIDs) > 256 {
			return errors.New("blocker.work_item_ids must contain 1..256 items")
		}
		seen := make(map[string]struct{}, len(value.WorkItemIDs))
		for _, id := range value.WorkItemIDs {
			if err := validateGoalUUID(id, "blocker.work_item_ids"); err != nil {
				return err
			}
			if _, exists := seen[id]; exists {
				return errors.New("blocker.work_item_ids must not contain duplicates")
			}
			seen[id] = struct{}{}
		}
		if goalAnyFieldPresent(value.present, "decision_id", "resource_id", "external_ref", "next_check_at", "code", "detail") || value.DecisionID != "" || value.ResourceID != "" || value.ExternalRef != "" || value.NextCheckAt != "" || value.Code != "" || value.Detail != "" {
			return errors.New("dependency blocker contains fields for another kind")
		}
	case "environment":
		if err := validateGoalShortIdentifier(value.Code, "blocker.code"); err != nil {
			return err
		}
		if err := validateGoalLongText(value.Detail, "blocker.detail"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "decision_id", "resource_id", "external_ref", "next_check_at", "work_item_ids") || value.DecisionID != "" || value.ResourceID != "" || value.ExternalRef != "" || value.NextCheckAt != "" || len(value.WorkItemIDs) != 0 {
			return errors.New("environment blocker contains fields for another kind")
		}
	default:
		return fmt.Errorf("blocker.kind %q is invalid", value.Kind)
	}
	return nil
}

func validateNextAction(value NextAction) error {
	switch value.Kind {
	case "validate":
		if err := validateGoalUUID(value.ProducingTaskID, "next_action.producing_task_id"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "work_item_id", "reason", "resource_id", "external_ref", "blocker") || value.WorkItemID != "" || value.Reason != "" || value.ResourceID != "" || value.ExternalRef != "" || value.Blocker != nil {
			return errors.New("validate next action contains fields for another kind")
		}
	case "repair":
		if err := validateGoalUUID(value.WorkItemID, "next_action.work_item_id"); err != nil {
			return err
		}
		if err := validateGoalLongText(value.Reason, "next_action.reason"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "producing_task_id", "resource_id", "external_ref", "blocker") || value.ProducingTaskID != "" || value.ResourceID != "" || value.ExternalRef != "" || value.Blocker != nil {
			return errors.New("repair next action contains fields for another kind")
		}
	case "observe":
		if err := validateGoalUUID(value.ResourceID, "next_action.resource_id"); err != nil {
			return err
		}
		if err := validateGoalLongText(value.ExternalRef, "next_action.external_ref"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "producing_task_id", "work_item_id", "reason", "blocker") || value.ProducingTaskID != "" || value.WorkItemID != "" || value.Reason != "" || value.Blocker != nil {
			return errors.New("observe next action contains fields for another kind")
		}
	case "replan":
		if err := validateGoalLongText(value.Reason, "next_action.reason"); err != nil {
			return err
		}
		if goalAnyFieldPresent(value.present, "producing_task_id", "work_item_id", "resource_id", "external_ref", "blocker") || value.ProducingTaskID != "" || value.WorkItemID != "" || value.ResourceID != "" || value.ExternalRef != "" || value.Blocker != nil {
			return errors.New("replan next action contains fields for another kind")
		}
	case "wait":
		if value.Blocker == nil {
			return errors.New("next_action.blocker is required")
		}
		if err := validateBlocker(*value.Blocker); err != nil {
			return fmt.Errorf("next_action.blocker: %w", err)
		}
		if goalAnyFieldPresent(value.present, "producing_task_id", "work_item_id", "reason", "resource_id", "external_ref") || value.ProducingTaskID != "" || value.WorkItemID != "" || value.Reason != "" || value.ResourceID != "" || value.ExternalRef != "" {
			return errors.New("wait next action contains fields for another kind")
		}
	default:
		return fmt.Errorf("next_action.kind %q is invalid", value.Kind)
	}
	return nil
}

func validateContextSize(value ContextSize) error {
	for field, number := range map[string]int64{
		"size.mandatory_bytes": value.MandatoryBytes,
		"size.optional_bytes":  value.OptionalBytes,
		"size.total_bytes":     value.TotalBytes,
	} {
		if err := validateGoalSafeNonNegativeInteger(number, field); err != nil {
			return err
		}
	}
	if err := validateGoalSafePositiveInteger(value.ByteBudget, "size.byte_budget"); err != nil {
		return err
	}
	if value.TokenEstimate != nil {
		if err := validateGoalSafeNonNegativeInteger(*value.TokenEstimate, "size.token_estimate"); err != nil {
			return err
		}
	}
	return nil
}

func validateGoalContextSnapshot(value GoalContextSnapshot) error {
	if value.SchemaVersion != goalContextSnapshotSchemaVersion {
		return fmt.Errorf("context schema_version must be %q", goalContextSnapshotSchemaVersion)
	}
	if err := validateGoalUUID(value.SnapshotID, "context.snapshot_id"); err != nil {
		return err
	}
	if err := validateGoalUUID(value.GoalID, "context.goal_id"); err != nil {
		return err
	}
	if err := validateGoalSafePositiveInteger(value.GoalRevision, "context.goal_revision"); err != nil {
		return err
	}
	if err := validateGoalSHA256(value.ContentHash, "context.content_hash"); err != nil {
		return err
	}
	if err := validateGoalUTCTimestamp(value.CreatedAt, "context.created_at"); err != nil {
		return err
	}
	if err := validateApprovedGoal(value.ApprovedGoal); err != nil {
		return err
	}
	if value.ApprovedGoal.GoalID != value.GoalID || value.ApprovedGoal.Revision != value.GoalRevision {
		return errors.New("context approved_goal identity does not match the snapshot")
	}
	if err := validateWorkContract(value.WorkContract); err != nil {
		return err
	}
	if value.WorkContract.Purpose == "plan" {
		if value.WorkItemID != nil {
			return errors.New("context.work_item_id must be null for plan purpose")
		}
	} else {
		if value.WorkItemID == nil {
			return fmt.Errorf("context.work_item_id is required for purpose %q", value.WorkContract.Purpose)
		}
		if err := validateGoalUUID(*value.WorkItemID, "context.work_item_id"); err != nil {
			return err
		}
	}
	if err := value.Subject.Validate(); err != nil {
		return fmt.Errorf("context subject: %w", err)
	}
	if value.Sources == nil || len(value.Sources) < 1 || len(value.Sources) > 512 {
		return errors.New("context sources must contain 1..512 items")
	}
	for _, source := range value.Sources {
		if err := validateContextSource(source); err != nil {
			return err
		}
	}
	if value.CurrentDecisions == nil || len(value.CurrentDecisions) > 256 {
		return errors.New("context current_decisions must be an array of at most 256 items")
	}
	for _, decision := range value.CurrentDecisions {
		if err := validateDecisionReference(decision); err != nil {
			return err
		}
	}
	if value.ValidatedEvidence == nil || len(value.ValidatedEvidence) > 1024 {
		return errors.New("context validated_evidence must be an array of at most 1024 items")
	}
	for _, evidence := range value.ValidatedEvidence {
		if err := validateEvidenceReference(evidence); err != nil {
			return err
		}
	}
	if value.FailedAttempts == nil || len(value.FailedAttempts) > 256 {
		return errors.New("context failed_attempts must be an array of at most 256 items")
	}
	for _, attempt := range value.FailedAttempts {
		if err := validateFailedAttempt(attempt); err != nil {
			return err
		}
	}
	if value.AdvisoryRecall == nil || len(value.AdvisoryRecall) > 256 {
		return errors.New("context advisory_recall must be an array of at most 256 items")
	}
	for _, source := range value.AdvisoryRecall {
		if err := validateContextSource(source); err != nil {
			return err
		}
	}
	if value.NextAction != nil {
		if err := validateNextAction(*value.NextAction); err != nil {
			return err
		}
	}
	if err := validateContextSize(value.Size); err != nil {
		return err
	}
	return validateGoalContextContentHash(value)
}

func validateGoalContextContentHash(value GoalContextSnapshot) error {
	// Hash the complete canonical context envelope except for its self-referential
	// content_hash field. The protocol package owns canonical JSON ordering and
	// number handling; this boundary only supplies the SHA-256 digest format.
	type wire GoalContextSnapshot
	encoded, err := json.Marshal(wire(value))
	if err != nil {
		return fmt.Errorf("marshal context for content_hash: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return fmt.Errorf("decode context for content_hash: %w", err)
	}
	if _, present := envelope["content_hash"]; !present {
		return errors.New("context.content_hash is missing from the canonical envelope")
	}
	delete(envelope, "content_hash")
	withoutHash, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal context content envelope: %w", err)
	}
	canonical, err := protocol.CanonicalizeJSON(withoutHash)
	if err != nil {
		return fmt.Errorf("canonicalize context content envelope: %w", err)
	}
	digest := sha256.Sum256(canonical)
	expected := "sha256:" + hex.EncodeToString(digest[:])
	if value.ContentHash != expected {
		return fmt.Errorf("context.content_hash does not match canonical snapshot (expected %s)", expected)
	}
	return nil
}

func (policy *AuthorityPolicy) UnmarshalJSON(data []byte) error {
	type wire AuthorityPolicy
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded,
		"operator_required_for_scope_change", "operator_required_for_completion", "publication_allowed", "allowed_actions"); err != nil {
		return err
	}
	if goalFieldIsNull(data, "operator_required_for_scope_change") || goalFieldIsNull(data, "operator_required_for_completion") || goalFieldIsNull(data, "publication_allowed") {
		return errors.New("authority_policy boolean fields must not be null")
	}
	value := AuthorityPolicy(decoded)
	if err := validateAuthorityPolicy(value); err != nil {
		return err
	}
	*policy = value
	return nil
}

func (goal *ApprovedGoal) UnmarshalJSON(data []byte) error {
	type wire ApprovedGoal
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "goal_id", "revision", "objective", "authority_policy"); err != nil {
		return err
	}
	value := ApprovedGoal(decoded)
	if err := validateApprovedGoal(value); err != nil {
		return err
	}
	*goal = value
	return nil
}

func (predicate *AcceptancePredicate) UnmarshalJSON(data []byte) error {
	type wire AcceptancePredicate
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "id", "kind"); err != nil {
		return err
	}
	value := AcceptancePredicate(decoded)
	value.present = goalObjectFieldSet(data)
	if err := validateAcceptancePredicate(value); err != nil {
		return err
	}
	*predicate = value
	return nil
}

func (contract *AcceptanceContract) UnmarshalJSON(data []byte) error {
	type wire AcceptanceContract
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "schema_version", "description", "predicates"); err != nil {
		return err
	}
	value := AcceptanceContract(decoded)
	if err := validateAcceptanceContract(value); err != nil {
		return err
	}
	*contract = value
	return nil
}

func (contract *WorkContract) UnmarshalJSON(data []byte) error {
	type wire WorkContract
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "title", "description", "purpose", "change_target", "acceptance", "validation_bindings"); err != nil {
		return err
	}
	value := WorkContract(decoded)
	if err := validateWorkContract(value); err != nil {
		return err
	}
	*contract = value
	return nil
}

func (binding *ValidationBinding) UnmarshalJSON(data []byte) error {
	type wire ValidationBinding
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "profile_name", "kind", "profile_digest", "allowed_runtime_ids"); err != nil {
		return err
	}
	value := ValidationBinding(decoded)
	if err := validateValidationBinding(value); err != nil {
		return err
	}
	*binding = value
	return nil
}

func (content *ContextContent) UnmarshalJSON(data []byte) error {
	type wire ContextContent
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "kind", "value"); err != nil {
		return err
	}
	value := ContextContent(decoded)
	if err := validateContextContent(value); err != nil {
		return err
	}
	*content = value
	return nil
}

func (source *ContextSource) UnmarshalJSON(data []byte) error {
	type wire ContextSource
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded,
		"resource_id", "source_kind", "source_revision", "content_hash", "observed_at", "trust", "required", "content"); err != nil {
		return err
	}
	if goalFieldIsNull(data, "required") {
		return errors.New("context source.required must not be null")
	}
	value := ContextSource(decoded)
	value.present = goalObjectFieldSet(data)
	if err := validateContextSource(value); err != nil {
		return err
	}
	*source = value
	return nil
}

func (reference *DecisionReference) UnmarshalJSON(data []byte) error {
	type wire DecisionReference
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "decision_id", "action_hash", "state"); err != nil {
		return err
	}
	value := DecisionReference(decoded)
	if err := validateDecisionReference(value); err != nil {
		return err
	}
	*reference = value
	return nil
}

func (reference *EvidenceReference) UnmarshalJSON(data []byte) error {
	type wire EvidenceReference
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "evidence_id", "predicate_id", "subject_hash", "verdict"); err != nil {
		return err
	}
	value := EvidenceReference(decoded)
	if err := validateEvidenceReference(value); err != nil {
		return err
	}
	*reference = value
	return nil
}

func (attempt *FailedAttempt) UnmarshalJSON(data []byte) error {
	type wire FailedAttempt
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "task_id", "run_id", "reason", "observed_at"); err != nil {
		return err
	}
	value := FailedAttempt(decoded)
	if err := validateFailedAttempt(value); err != nil {
		return err
	}
	*attempt = value
	return nil
}

func (blocker *Blocker) UnmarshalJSON(data []byte) error {
	type wire Blocker
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "kind"); err != nil {
		return err
	}
	value := Blocker(decoded)
	value.present = goalObjectFieldSet(data)
	if err := validateBlocker(value); err != nil {
		return err
	}
	*blocker = value
	return nil
}

func (action *NextAction) UnmarshalJSON(data []byte) error {
	type wire NextAction
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "kind"); err != nil {
		return err
	}
	value := NextAction(decoded)
	value.present = goalObjectFieldSet(data)
	if err := validateNextAction(value); err != nil {
		return err
	}
	*action = value
	return nil
}

func (size *ContextSize) UnmarshalJSON(data []byte) error {
	type wire ContextSize
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded,
		"mandatory_bytes", "optional_bytes", "total_bytes", "byte_budget", "token_estimate"); err != nil {
		return err
	}
	if goalFieldIsNull(data, "mandatory_bytes") || goalFieldIsNull(data, "optional_bytes") || goalFieldIsNull(data, "total_bytes") || goalFieldIsNull(data, "byte_budget") {
		return errors.New("context size integer fields must not be null")
	}
	value := ContextSize(decoded)
	if err := validateContextSize(value); err != nil {
		return err
	}
	*size = value
	return nil
}

func (receipt *GoalSessionStoppedReceipt) UnmarshalJSON(data []byte) error {
	var wire struct {
		ReceiptID     string          `json:"receipt_id"`
		RunID         string          `json:"run_id"`
		SessionID     string          `json:"session_id"`
		LocalHandleID string          `json:"local_handle_id"`
		BindingID     string          `json:"binding_id"`
		State         string          `json:"state"`
		ActiveRunID   json.RawMessage `json:"active_run_id"`
		LockVersion   int64           `json:"lock_version"`
	}
	if err := decodeStrictObjectJSON(data, &wire,
		"receipt_id", "run_id", "session_id", "local_handle_id", "binding_id", "state", "active_run_id", "lock_version"); err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(wire.ActiveRunID), []byte("null")) {
		return errors.New("session_stopped.active_run_id must be explicitly null")
	}
	if wire.LockVersion <= 0 {
		return errors.New("session_stopped.lock_version must be positive")
	}
	*receipt = GoalSessionStoppedReceipt{
		ReceiptID:     wire.ReceiptID,
		RunID:         wire.RunID,
		SessionID:     wire.SessionID,
		LocalHandleID: wire.LocalHandleID,
		BindingID:     wire.BindingID,
		State:         wire.State,
		ActiveRunID:   nil,
		LockVersion:   wire.LockVersion,
	}
	return nil
}

func (snapshot *GoalContextSnapshot) UnmarshalJSON(data []byte) error {
	if _, err := protocol.DecodeContextSnapshot(data); err != nil {
		return fmt.Errorf("decode context snapshot schema: %w", err)
	}
	type wire GoalContextSnapshot
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded,
		"schema_version", "snapshot_id", "goal_id", "goal_revision", "work_item_id", "content_hash", "created_at",
		"approved_goal", "work_contract", "subject", "sources", "current_decisions", "validated_evidence",
		"failed_attempts", "advisory_recall", "next_action", "size"); err != nil {
		return err
	}
	value := GoalContextSnapshot(decoded)
	if err := validateGoalContextSnapshot(value); err != nil {
		return err
	}
	*snapshot = value
	return nil
}

func (context *GoalRunContext) UnmarshalJSON(data []byte) error {
	type wire GoalRunContext
	var decoded wire
	if err := decodeStrictObjectJSON(data, &decoded, "goal_id", "task_id", "run_id", "generation", "session_id", "context"); err != nil {
		return err
	}
	*context = GoalRunContext(decoded)
	return nil
}
