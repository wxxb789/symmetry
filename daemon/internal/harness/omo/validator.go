package omo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// TestedEngineVersion is the engineVersion observed in both pinned classic
// transports. It differs from the senpi build text printed by --version.
const TestedEngineVersion = "5.1.69"

type registeredRequest struct {
	request  Request
	answered bool
}

// Validator is a serial, independent OmO protocol and one-turn state machine.
// Its caller owns synchronization and must keep draining after a violation.
type Validator struct {
	requests          map[string]*registeredRequest
	protocolRequested bool
	protocolReady     bool
	state             *SessionState
	expected          *SessionState
	prompt            *Request
	promptAccepted    bool
	inputNotFound     bool
	running           bool
	ended             bool
	willRetry         bool
	settled           bool
	idle              bool
	finalAssistant    json.RawMessage
	endAssistant      json.RawMessage
	cancelEffect      string
	abortDrained      bool
	failure           error
}

func NewValidator(expected *SessionState) *Validator {
	validator := &Validator{requests: make(map[string]*registeredRequest)}
	if expected != nil {
		value := *expected
		validator.expected = &value
	}
	return validator
}

func (validator *Validator) Register(request Request) error {
	if validator.failure != nil {
		return validator.failure
	}
	if err := request.Validate(); err != nil {
		return validator.fail(err)
	}
	if _, exists := validator.requests[request.ID]; exists {
		return validator.fail(ErrCorrelation)
	}
	switch request.Type {
	case CommandProtocol:
		if validator.protocolRequested {
			return validator.fail(ErrProtocol)
		}
		validator.protocolRequested = true
	case CommandGetState:
		if !validator.protocolReady {
			return validator.fail(ErrProtocol)
		}
	case CommandPrompt:
		if !validator.protocolReady || validator.state == nil {
			return validator.fail(ErrProtocol)
		}
		if validator.prompt != nil {
			previous := *validator.prompt
			previous.ID = request.ID
			if previous != request || !validator.inputNotFound || validator.promptAccepted || validator.running {
				return validator.fail(ErrUnknownOutcome)
			}
		}
		copy := request
		validator.prompt = &copy
		validator.inputNotFound = false
	case CommandEffectStatus:
		if validator.prompt == nil || request.EffectID != validator.prompt.ClientMessageID {
			return validator.fail(ErrCorrelation)
		}
	case CommandAbort:
		if validator.prompt == nil {
			return validator.fail(ErrProtocol)
		}
		if validator.cancelEffect != "" && validator.cancelEffect != request.EffectID {
			return validator.fail(ErrCorrelation)
		}
		validator.cancelEffect = request.EffectID
	}
	validator.requests[request.ID] = &registeredRequest{request: request}
	return nil
}

// Expire preserves a prompt's correlation slot. A late acknowledgement can
// still prove its original admission, but can never acknowledge another input.
func (validator *Validator) Expire(id string) error {
	entry := validator.requests[id]
	if entry == nil || entry.answered {
		return ErrCorrelation
	}
	if entry.request.Type == CommandPrompt || entry.request.Type == CommandEffectStatus || entry.request.Type == CommandAbort {
		return ErrUnknownOutcome
	}
	return validator.fail(ErrUnknownOutcome)
}

func (validator *Validator) Observe(record Record) error {
	if validator.failure != nil {
		return validator.failure
	}
	if record.DecodeError != nil {
		return validator.fail(record.DecodeError)
	}
	var err error
	if record.Response != nil {
		err = validator.observeResponse(*record.Response)
	} else {
		err = validator.observeEvent(record)
	}
	if err != nil {
		return validator.fail(err)
	}
	return nil
}

func (validator *Validator) observeResponse(response Response) error {
	entry := validator.requests[response.ID]
	if entry == nil || entry.answered || entry.request.Type != response.Command {
		return ErrCorrelation
	}
	entry.answered = true
	if !response.Success {
		return fmt.Errorf("OmO native %s command was rejected", entry.request.Type)
	}
	switch response.Command {
	case CommandProtocol:
		object, err := strictObject(response.Data)
		if err != nil {
			return err
		}
		var version int
		var engine, server, mode string
		if json.Unmarshal(object["protocolVersion"], &version) != nil || version != 1 || !nonEmptyString(object["engineVersion"], &engine) || engine != TestedEngineVersion || !nonEmptyString(object["serverVersion"], &server) || server != TestedVersion || !nonEmptyString(object["mode"], &mode) || mode != "classic" {
			return ErrProtocol
		}
		capabilities, err := requiredArray(object["capabilities"])
		if err != nil {
			return ErrProtocol
		}
		seen := make(map[string]bool)
		for _, value := range capabilities {
			var capability string
			if !nonEmptyString(value, &capability) || seen[capability] {
				return ErrProtocol
			}
			seen[capability] = true
		}
		if !seen["durable_client_message_id"] || !seen["effect_ledger"] {
			return ErrProtocol
		}
		validator.protocolReady = true
	case CommandGetState:
		state, err := decodeSessionState(response.Data)
		if err != nil {
			return err
		}
		if validator.expected != nil && (validator.expected.SessionID != state.SessionID || validator.expected.SessionFile != state.SessionFile) {
			return ErrCorrelation
		}
		if validator.state != nil && (validator.state.SessionID != state.SessionID || validator.state.SessionFile != state.SessionFile) {
			return ErrCorrelation
		}
		if err := state.ValidateIdle(); err != nil {
			return err
		}
		validator.state = &state
	case CommandPrompt:
		if err := validator.acceptPrompt(response.Data, entry.request); err != nil {
			return err
		}
	case CommandEffectStatus:
		object, err := strictObject(response.Data)
		if err != nil {
			return err
		}
		var found bool
		if requiredBool(object["found"], &found) != nil {
			return ErrInvalidRecord
		}
		if !found {
			var searched string
			if !nonEmptyString(object["searched"], &searched) || searched != "complete" || validator.promptAccepted || validator.running {
				return ErrUnknownOutcome
			}
			validator.inputNotFound = true
			return nil
		}
		if err := validator.validateEffect(object["effect"], entry.request.EffectID, "input", false); err != nil {
			return err
		}
		validator.promptAccepted = true
	case CommandAbort:
		object, err := strictObject(response.Data)
		if err != nil {
			return err
		}
		var idle, stopped bool
		if requiredBool(object["idle"], &idle) != nil || requiredBool(object["stopped"], &stopped) != nil {
			return ErrInvalidRecord
		}
		var effectID, state string
		if !nonEmptyString(object["effectId"], &effectID) || effectID != entry.request.EffectID || !nonEmptyString(object["state"], &state) {
			return ErrCorrelation
		}
		if err := validator.validateEffect(object["effect"], effectID, "abort", idle && stopped); err != nil {
			return err
		}
		if idle != stopped || (idle && state != "done") {
			return ErrInvalidRecord
		}
		validator.abortDrained = idle && stopped
	}
	return nil
}

func (validator *Validator) acceptPrompt(raw json.RawMessage, request Request) error {
	object, err := strictObject(raw)
	if err != nil {
		return err
	}
	var disposition, messageID, turnID string
	if !nonEmptyString(object["disposition"], &disposition) || disposition != "started" || !nonEmptyString(object["clientMessageId"], &messageID) || messageID != request.ClientMessageID || !nonEmptyString(object["clientTurnId"], &turnID) || turnID != request.ClientTurnID {
		return ErrCorrelation
	}
	admission, err := strictObject(object["admission"])
	if err != nil {
		return err
	}
	var durableID, admittedMessage, admittedTurn, state, admittedDisposition string
	if !nonEmptyString(admission["durableSessionId"], &durableID) || validator.state == nil || durableID != validator.state.SessionID || !nonEmptyString(admission["clientMessageId"], &admittedMessage) || admittedMessage != messageID || !nonEmptyString(admission["clientTurnId"], &admittedTurn) || admittedTurn != turnID || !nonEmptyString(admission["state"], &state) || (state != "running" && state != "completed") || !nonEmptyString(admission["disposition"], &admittedDisposition) || admittedDisposition != disposition {
		return ErrCorrelation
	}
	validator.promptAccepted = true
	return nil
}

func (validator *Validator) validateEffect(raw json.RawMessage, expectedID, expectedKind string, requireDone bool) error {
	object, err := strictObject(raw)
	if err != nil {
		return err
	}
	var id, kind, state, durableID string
	if !nonEmptyString(object["effectId"], &id) || id != expectedID || !nonEmptyString(object["kind"], &kind) || kind != expectedKind || !nonEmptyString(object["durableSessionId"], &durableID) || validator.state == nil || durableID != validator.state.SessionID || !nonEmptyString(object["state"], &state) {
		return ErrCorrelation
	}
	if state != "accepted" && state != "applied" && state != "done" && state != "failed" {
		return ErrInvalidRecord
	}
	if state == "failed" || (requireDone && state != "done") {
		return ErrUnknownOutcome
	}
	return nil
}

func (validator *Validator) observeEvent(record Record) error {
	if record.Type == "extension_ui_request" {
		var id, method string
		if !nonEmptyString(record.Fields["id"], &id) || !nonEmptyString(record.Fields["method"], &method) {
			return ErrInvalidRecord
		}
		switch method {
		case "notify", "setStatus", "setWidget", "setTitle", "set_editor_text":
			return nil
		default:
			return fmt.Errorf("%w: actionable or unknown native UI is unsupported", ErrProtocol)
		}
	}
	if record.Type == "transcript_write_failed" || record.Type == "session_recovered" || record.Type == "session_replaced" || record.Type == "session_closed" || record.Type == "session_released" || record.Type == "session_parked" {
		return fmt.Errorf("%w: native persistence or ownership changed", ErrProtocol)
	}
	switch record.Type {
	case "agent_start":
		if validator.prompt == nil || validator.running || validator.settled || (validator.ended && !validator.willRetry) {
			return ErrCorrelation
		}
		if raw, exists := record.Fields["runId"]; exists {
			var id string
			if !nonEmptyString(raw, &id) {
				return ErrInvalidRecord
			}
		}
		validator.running, validator.ended, validator.willRetry = true, false, false
		validator.finalAssistant, validator.endAssistant = nil, nil
	case "message_end":
		if !validator.running || validator.ended {
			return ErrCorrelation
		}
		if err := validator.checkClientIdentity(record.Fields); err != nil {
			return err
		}
		message, err := strictObject(record.Fields["message"])
		if err != nil {
			return err
		}
		var role string
		if !nonEmptyString(message["role"], &role) {
			return ErrInvalidRecord
		}
		if role != "assistant" {
			return nil
		}
		if validator.finalAssistant != nil {
			return ErrCorrelation
		}
		var stop string
		if !nonEmptyString(message["stopReason"], &stop) {
			return ErrInvalidRecord
		}
		if stop == "stop" {
			validator.finalAssistant = append(json.RawMessage(nil), record.Fields["message"]...)
		} else if stop != "toolUse" && stop != "error" && stop != "aborted" && stop != "length" {
			return ErrInvalidRecord
		}
	case "agent_end":
		if !validator.running || validator.ended || validator.settled {
			return ErrCorrelation
		}
		if err := validator.checkClientIdentity(record.Fields); err != nil {
			return err
		}
		messages, err := requiredArray(record.Fields["messages"])
		if err != nil {
			return err
		}
		if requiredBool(record.Fields["willRetry"], &validator.willRetry) != nil {
			return ErrInvalidRecord
		}
		if raw, exists := record.Fields["aborted"]; exists {
			var aborted bool
			if requiredBool(raw, &aborted) != nil {
				return ErrInvalidRecord
			}
			if aborted {
				validator.finalAssistant = nil
			}
		}
		if raw, exists := record.Fields["failure"]; exists {
			if _, err := strictObject(raw); err != nil {
				return err
			}
			validator.finalAssistant = nil
		}
		for _, message := range messages {
			object, err := strictObject(message)
			if err != nil {
				return err
			}
			var role string
			if !nonEmptyString(object["role"], &role) {
				return ErrInvalidRecord
			}
			if role == "assistant" {
				validator.endAssistant = append(json.RawMessage(nil), message...)
			}
		}
		validator.running, validator.ended = false, true
	case "agent_settled":
		if !validator.ended || validator.settled || validator.willRetry {
			return ErrCorrelation
		}
		if _, exists := record.Fields["reason"]; exists {
			return ErrProtocol
		}
		validator.settled = true
	case "agent_idle":
		if !validator.settled || validator.idle {
			return ErrCorrelation
		}
		validator.idle = true
	case "message_update":
		if !validator.running || validator.finalAssistant != nil {
			return ErrCorrelation
		}
		// OmO emits these diagnostic deltas without durable client identities.
		// Their nested assistantMessageEvent is never completion evidence.
	case "turn_start", "turn_end", "message_start", "tool_execution_start", "tool_execution_update", "tool_execution_end":
		if !validator.running {
			return ErrCorrelation
		}
		if err := validator.checkClientIdentity(record.Fields); err != nil {
			return err
		}
		if validator.finalAssistant != nil && record.Type != "turn_end" {
			return ErrCorrelation
		}
	}
	return nil
}

func (validator *Validator) checkClientIdentity(object map[string]json.RawMessage) error {
	if validator.prompt == nil {
		return ErrCorrelation
	}
	var message, turn string
	if !nonEmptyString(object["clientMessageId"], &message) || message != validator.prompt.ClientMessageID || !nonEmptyString(object["clientTurnId"], &turn) || turn != validator.prompt.ClientTurnID {
		return ErrCorrelation
	}
	if raw, exists := object["clientMessages"]; exists {
		messages, err := requiredArray(raw)
		if err != nil || len(messages) != 1 {
			return ErrCorrelation
		}
		identity, err := strictObject(messages[0])
		if err != nil {
			return err
		}
		var nestedMessage, nestedTurn string
		if !nonEmptyString(identity["clientMessageId"], &nestedMessage) || nestedMessage != message || !nonEmptyString(identity["clientTurnId"], &nestedTurn) || nestedTurn != turn {
			return ErrCorrelation
		}
	}
	return nil
}

func (validator *Validator) Finish() (json.RawMessage, error) {
	if validator.failure != nil {
		return nil, validator.failure
	}
	if !validator.idle {
		return nil, ErrNotSettled
	}
	if !validator.promptAccepted {
		return nil, ErrPromptNotAccepted
	}
	if validator.finalAssistant == nil || validator.endAssistant == nil || !sameNativeMessage(validator.finalAssistant, validator.endAssistant) {
		return nil, ErrMissingResult
	}
	return append(json.RawMessage(nil), validator.finalAssistant...), nil
}

func sameNativeMessage(left, right json.RawMessage) bool {
	if bytes.Equal(left, right) {
		return true
	}
	// Identical native objects normally serialize identically. Object-member
	// order and insignificant JSON whitespace do not change message identity.
	var l, r any
	ld, rd := json.NewDecoder(bytes.NewReader(left)), json.NewDecoder(bytes.NewReader(right))
	ld.UseNumber()
	rd.UseNumber()
	return ld.Decode(&l) == nil && rd.Decode(&r) == nil && reflect.DeepEqual(l, r)
}

func (validator *Validator) SessionState() (SessionState, bool) {
	if validator.state == nil {
		return SessionState{}, false
	}
	return *validator.state, true
}

func (validator *Validator) fail(err error) error {
	if validator.failure == nil {
		validator.failure = err
	}
	return validator.failure
}

func temporaryCompletion(err error) bool {
	return errors.Is(err, ErrNotSettled) || errors.Is(err, ErrPromptNotAccepted)
}
