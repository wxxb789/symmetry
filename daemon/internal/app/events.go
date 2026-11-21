package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	redactedProviderToken            = "*"
	maxProviderRedactorPendingBytes  = 1 << 20
	maxProviderRedactorPendingEvents = 128
)

type agentOutput struct {
	executionContext context.Context
	format           config.EventFormat
	parser           *jsonlParser
	redactor         *streamingTokenRedactor
	stdoutAt         time.Time
	flushed          bool
}

func newAgentOutput(format config.EventFormat, token string) *agentOutput {
	output := &agentOutput{format: format, parser: &jsonlParser{}}
	if token != "" {
		output.redactor = newStreamingTokenRedactor(token)
	}
	return output
}

func (output *agentOutput) handle(daemon *daemon, key state.RunKey, event execution.Event) error {
	if event.Stream == execution.Stdout {
		output.stdoutAt = event.At
	}
	if output.redactor != nil {
		for _, ready := range output.redactor.push(event) {
			if err := daemon.queueOutput(key, output.format, output.parser, ready); err != nil {
				return err
			}
		}
		return nil
	}
	return daemon.queueOutput(key, output.format, output.parser, event)
}

func (output *agentOutput) flush(daemon *daemon, key state.RunKey, at time.Time) error {
	if output.flushed {
		return nil
	}
	output.flushed = true
	if output.redactor != nil {
		for _, event := range output.redactor.flush() {
			if err := daemon.queueOutput(key, output.format, output.parser, event); err != nil {
				return err
			}
		}
	}
	if output.format != config.EventFormatJSONL {
		return nil
	}
	if output.stdoutAt.IsZero() {
		output.stdoutAt = at
	}
	for _, record := range output.parser.flush() {
		if err := daemon.queueRawEvent(key, execution.Event{Stream: execution.Stdout, At: output.stdoutAt, Data: record.data}); err != nil {
			return err
		}
	}
	return nil
}

type redactorStream struct {
	candidate []redactorByte
}

type redactorByte struct {
	value byte
	owner *redactorEvent
}

type redactorEvent struct {
	event       execution.Event
	output      []byte
	remaining   int
	sourceBytes int
}

type streamingTokenRedactor struct {
	token        []byte
	replacement  byte
	streams      map[execution.Stream]*redactorStream
	pending      []*redactorEvent
	pendingBytes int
}

func newStreamingTokenRedactor(token string) *streamingTokenRedactor {
	encoded := []byte(token)
	return &streamingTokenRedactor{
		token:       encoded,
		replacement: redactionMarker(encoded),
		streams: map[execution.Stream]*redactorStream{
			execution.Stdout: {},
			execution.Stderr: {},
		},
	}
}

func redactionMarker(token []byte) byte {
	for _, candidate := range []byte(redactedProviderToken + "~!@#$%^&()-_=+[]{};:,.?/ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 ") {
		if !bytes.Contains(token, []byte{candidate}) {
			return candidate
		}
	}
	return 0xff
}

func (redactor *streamingTokenRedactor) push(event execution.Event) []execution.Event {
	entry := &redactorEvent{event: event, remaining: len(event.Data), sourceBytes: len(event.Data)}
	entry.event.Data = nil
	redactor.pending = append(redactor.pending, entry)
	redactor.pendingBytes += entry.sourceBytes
	state := redactor.streams[event.Stream]
	if state == nil {
		state = &redactorStream{}
		redactor.streams[event.Stream] = state
	}
	for _, value := range event.Data {
		state.candidate = append(state.candidate, redactorByte{value: value, owner: entry})
	}
	redactor.resolve(state)
	ready := redactor.drain()
	for redactor.overLimit() {
		if !redactor.redactOldestCandidate() {
			break
		}
		ready = append(ready, redactor.drain()...)
	}
	return ready
}

func (redactor *streamingTokenRedactor) flush() []execution.Event {
	for _, state := range redactor.streams {
		for _, pending := range state.candidate {
			pending.owner.output = append(pending.owner.output, pending.value)
			pending.owner.remaining--
		}
		clear(state.candidate)
		state.candidate = nil
	}
	events := redactor.drain()
	clear(redactor.token)
	redactor.token = nil
	return events
}

func (redactor *streamingTokenRedactor) resolve(state *redactorStream) {
	for len(state.candidate) > 0 {
		if len(state.candidate) >= len(redactor.token) && candidateMatches(state.candidate[:len(redactor.token)], redactor.token) {
			owner := state.candidate[len(redactor.token)-1].owner
			owner.output = append(owner.output, redactor.replacement)
			for _, matched := range state.candidate[:len(redactor.token)] {
				matched.owner.remaining--
			}
			state.candidate = state.candidate[len(redactor.token):]
			continue
		}
		if candidateIsTokenPrefix(state.candidate, redactor.token) {
			return
		}
		first := state.candidate[0]
		first.owner.output = append(first.owner.output, first.value)
		first.owner.remaining--
		state.candidate = state.candidate[1:]
	}
}

func (redactor *streamingTokenRedactor) drain() []execution.Event {
	ready := make([]execution.Event, 0, len(redactor.pending))
	for len(redactor.pending) > 0 && redactor.pending[0].remaining == 0 {
		entry := redactor.pending[0]
		redactor.pending[0] = nil
		redactor.pending = redactor.pending[1:]
		redactor.pendingBytes -= entry.sourceBytes
		if len(entry.output) != 0 {
			entry.event.Data = entry.output
			ready = append(ready, entry.event)
		}
	}
	if len(redactor.pending) == 0 {
		redactor.pending = nil
	}
	return ready
}

func (redactor *streamingTokenRedactor) overLimit() bool {
	return len(redactor.pending) > maxProviderRedactorPendingEvents || redactor.pendingBytes > maxProviderRedactorPendingBytes
}

func (redactor *streamingTokenRedactor) redactOldestCandidate() bool {
	if len(redactor.pending) == 0 {
		return false
	}
	head := redactor.pending[0]
	state := redactor.streams[head.event.Stream]
	if state == nil || len(state.candidate) == 0 || state.candidate[0].owner != head {
		return false
	}
	state.candidate[0].owner.output = append(state.candidate[0].owner.output, redactor.replacement)
	for _, pending := range state.candidate {
		pending.owner.remaining--
	}
	clear(state.candidate)
	state.candidate = nil
	return true
}

func candidateMatches(candidate []redactorByte, token []byte) bool {
	if len(candidate) != len(token) {
		return false
	}
	for index, value := range candidate {
		if value.value != token[index] {
			return false
		}
	}
	return true
}

func candidateIsTokenPrefix(candidate []redactorByte, token []byte) bool {
	if len(candidate) >= len(token) {
		return false
	}
	for index, value := range candidate {
		if value.value != token[index] {
			return false
		}
	}
	return true
}

func (daemon *daemon) queueOutput(key state.RunKey, format config.EventFormat, parser *jsonlParser, event execution.Event) error {
	if event.Stream == execution.Stderr || format != config.EventFormatJSONL {
		return daemon.queueRawEvent(key, event)
	}
	for _, record := range parser.push(event.Data) {
		if record.raw || !json.Valid(record.data) {
			if err := daemon.queueRawEvent(key, execution.Event{Stream: event.Stream, At: event.At, Data: record.data}); err != nil {
				return err
			}
			continue
		}
		var decoded any
		if err := json.Unmarshal(record.data, &decoded); err != nil || containsNUL(decoded) {
			if err := daemon.queueRawEvent(key, execution.Event{Stream: event.Stream, At: event.At, Data: record.data}); err != nil {
				return err
			}
			continue
		}
		payload, ok := decoded.(map[string]any)
		if !ok {
			if err := daemon.queueEvent(key, "agent_event", wrapAgentEventValue(record.data), event.At); err != nil {
				return err
			}
			continue
		}
		kind := semanticEventKind(payload, len(record.data))
		if kind == "command_applied" {
			matched, err := daemon.queueCommandApplied(key, payload, event.At)
			if err != nil {
				return err
			}
			if matched {
				continue
			}
			kind = "agent_event"
		}
		if kind == "waiting_for_input" {
			if err := daemon.validateWaitingPacket(key, payload); err != nil {
				return err
			}
			if err := daemon.queueWaitingForInput(key, json.RawMessage(record.data), event.At); err != nil {
				return err
			}
			continue
		}
		if err := daemon.queueEvent(key, kind, json.RawMessage(record.data), event.At); err != nil {
			return err
		}
	}
	return nil
}

func wrapAgentEventValue(value []byte) json.RawMessage {
	payload := make([]byte, 0, len(value)+len(`{"value":}`))
	payload = append(payload, `{"value":`...)
	payload = append(payload, value...)
	return append(payload, '}')
}

func semanticEventKind(payload map[string]any, encodedBytes int) string {
	if encodedBytes > maxSemanticEventBytes || !semanticVersionSupported(payload) {
		return "agent_event"
	}

	kind, _ := payload["type"].(string)
	switch kind {
	case "command_applied":
		if nonEmptyString(payload["command_id"]) && stringIn(payload["kind"], "guidance", "pause", "resume") && stringIn(payload["outcome"], "applied", "rejected", "failed") {
			return kind
		}
	case "progress", "waiting_for_input":
		return kind
	case "summary":
		if nonEmptyString(payload["summary"]) || nonEmptyString(payload["message"]) {
			return kind
		}
	case "finding":
		if nonEmptyString(payload["message"]) || nonEmptyString(payload["title"]) {
			return kind
		}
	case "artifact":
		if nonEmptyString(payload["path"]) || httpURL(payload["url"]) {
			return kind
		}
	case "test":
		if nonEmptyString(payload["name"]) && stringIn(payload["status"], "running", "passed", "failed", "skipped") {
			return kind
		}
	case "pull_request":
		if httpURL(payload["url"]) {
			return kind
		}
	case "ci":
		if stringIn(payload["status"], "unknown", "pending", "passed", "failed") {
			return kind
		}
	case "review":
		if stringIn(payload["status"], "none", "required", "changes_requested", "approved") {
			return kind
		}
	}

	return "agent_event"
}

func semanticVersionSupported(payload map[string]any) bool {
	version, ok := payload["schema_version"]
	if !ok {
		return true
	}
	number, ok := version.(float64)
	return ok && number == 1
}

func nonEmptyString(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func stringIn(value any, allowed ...string) bool {
	text, ok := value.(string)
	return ok && slices.Contains(allowed, text)
}

func httpURL(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(text))
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "https" || scheme == "http"
}

func containsNUL(value any) bool {
	switch value := value.(type) {
	case string:
		return strings.IndexByte(value, 0) >= 0
	case []any:
		for _, item := range value {
			if containsNUL(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range value {
			if strings.IndexByte(key, 0) >= 0 || containsNUL(item) {
				return true
			}
		}
	}
	return false
}

func (daemon *daemon) queueRawEvent(key state.RunKey, event execution.Event) error {
	payload, err := json.Marshal(map[string]string{"stream": string(event.Stream), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(event.Data)})
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	_, dropped, err := daemon.store.QueueOutputEvent(key, protocol.RunEvent{EventID: id, Kind: "output", OccurredAt: event.At, Payload: payload}, maxPendingOutputBytes)
	if err != nil {
		return err
	}
	if dropped {
		daemon.warnOutputDropOnce(key, len(event.Data))
		return nil
	}
	daemon.signalOutboxFor(key)
	return nil
}

func (daemon *daemon) warnOutputDropOnce(key state.RunKey, rawBytes int) {
	daemon.mu.Lock()
	active := daemon.running[key]
	warn := active != nil && !active.outputDropWarned
	if warn {
		active.outputDropWarned = true
	}
	daemon.mu.Unlock()
	if warn && daemon.log != nil {
		daemon.log.Warn("output_dropped_pending_budget", "run_id", key.RunID, "generation", key.Generation, "bytes", rawBytes)
	}
}

func (daemon *daemon) queueEvent(key state.RunKey, kind string, payload json.RawMessage, at time.Time) error {
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	event := protocol.RunEvent{EventID: id, Kind: kind, OccurredAt: at, Payload: payload}
	daemon.eventReceiptMu.Lock()
	_, err = daemon.store.QueueNextEvent(key, event)
	if err != nil {
		if journal, loadErr := daemon.store.LoadJournal(key); loadErr == nil && hasExactPendingEvent(journal, event) {
			err = nil
		}
	}
	var renewCancel context.CancelFunc
	if err == nil && kind == string(harness.EventTaskResult) {
		daemon.mu.Lock()
		if active := daemon.running[key]; active != nil {
			if active.nativeStartInFlight {
				active.nativeStartTerminalObserved = true
			}
			if active.nativeSession != nil && !active.nativeTerminalOwner && !active.cancelled && !active.stale && !active.terminal && active.terminalizing == 0 {
				claimNativeTerminalOwner(active, true)
				active.nativeStartTerminalObserved = true
				active.nativeUsageRenewalBlocked = true
				renewCancel = active.renewCancel
				active.renewCancel = nil
				active.renewCancelID++
			}
		}
		daemon.mu.Unlock()
	}
	daemon.eventReceiptMu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
}

func hasExactPendingEvent(journal state.RunJournal, expected protocol.RunEvent) bool {
	for _, event := range journal.PendingEvents {
		if event.EventID == expected.EventID && event.Kind == expected.Kind && event.OccurredAt.Equal(expected.OccurredAt) && bytes.Equal(event.Payload, expected.Payload) {
			return true
		}
	}
	return false
}

func (daemon *daemon) queueWaitingForInput(key state.RunKey, payload json.RawMessage, at time.Time) error {
	active := daemon.runningRun(key)
	if active != nil {
		active.inputMu.Lock()
		defer active.inputMu.Unlock()
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	_, err = daemon.store.QueueWaitingForInput(key,
		protocol.RunEvent{EventID: id, Kind: "waiting_for_input", OccurredAt: at, Payload: payload},
		protocol.StateTransitionRequest{TransitionID: id, State: "waiting_for_input", Payload: payload},
	)
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
}

func (daemon *daemon) queueTransition(key state.RunKey, stateName string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	_, err = daemon.store.QueueTransition(key, protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded})
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
}

func (daemon *daemon) queueFailure(ctx context.Context, key state.RunKey, stage string, cause error) {
	payload := map[string]string{
		"stage":   stage,
		"reason":  string(canonicalTaskResultReason(cause, nil, protocol.TaskResultReasonUnknownOutcome)),
		"summary": "daemon failed during " + stage,
		"error":   cause.Error(),
	}
	if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", payload); err != nil {
		daemon.log.Error("queue_failed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "stage", stage, "error", err)
	}
}

type jsonlRecord struct {
	data []byte
	raw  bool
}

type jsonlParser struct {
	pending  []byte
	overflow bool
}

func (parser *jsonlParser) push(value []byte) []jsonlRecord {
	parser.pending = append(parser.pending, value...)
	records := make([]jsonlRecord, 0, 1)
	for len(parser.pending) > 0 {
		newline := bytes.IndexByte(parser.pending, '\n')
		if newline >= 0 {
			line := parser.pending[:newline]
			parser.pending = parser.pending[newline+1:]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if parser.overflow || len(line) > maxJSONLRecordBytes {
				records = appendRawRecords(records, line)
				parser.overflow = false
				continue
			}
			records = append(records, jsonlRecord{data: append([]byte(nil), line...)})
			continue
		}
		if parser.overflow || len(parser.pending) > maxJSONLRecordBytes {
			parser.overflow = true
			records = appendRawRecords(records, parser.pending)
			parser.pending = nil
		}
		break
	}
	return records
}

func (parser *jsonlParser) flush() []jsonlRecord {
	if len(parser.pending) == 0 {
		parser.overflow = false
		return nil
	}
	records := appendRawRecords(nil, parser.pending)
	clear(parser.pending)
	parser.pending = nil
	parser.overflow = false
	return records
}

func appendRawRecords(records []jsonlRecord, value []byte) []jsonlRecord {
	for len(value) > 0 {
		length := len(value)
		if length > rawOutputChunkBytes {
			length = rawOutputChunkBytes
		}
		records = append(records, jsonlRecord{data: append([]byte(nil), value[:length]...), raw: true})
		value = value[length:]
	}
	return records
}
