package operator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func validateTaskResponse(operation, expectedTaskID string, response Task) error {
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

func validateTaskWaiting(task Task, waiting TaskWaiting) error {
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
