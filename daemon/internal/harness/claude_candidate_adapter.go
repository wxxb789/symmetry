package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	claudeprotocol "github.com/wxxb789/symmetry/daemon/internal/harness/claude"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	claudeCandidateDefaultExecutable = "claude"
	claudeCandidateMaxRecordBytes    = 1 << 20
	claudeCandidateMaxPreReadyEvents = 64
	claudeCandidateMaxPreReadyBytes  = 1 << 20
	claudeCandidateTerminationGrace  = 5 * time.Second
	claudeCandidateCleanupTimeout    = 5 * time.Second
)

var (
	errClaudeCandidateSessionClosed    = errors.New("Claude staged candidate session is closed")
	errClaudeCandidateProcessNil       = errors.New("Claude staged candidate process starter returned a nil process")
	errClaudeCandidateMissingInit      = errors.New("Claude staged candidate did not observe a matching system/init session identity")
	errClaudeCandidateResultBeforeTurn = errors.New("Claude staged candidate observed a terminal result before StartTurn")
	errClaudeCandidateMissingResult    = errors.New("Claude staged candidate process ended without a verified result")
	errClaudeCandidateNoSemanticResult = errors.New("Claude staged candidate terminal result was not a valid Symmetry TaskResult")
)

// claudeCandidateProcess is the existing execution.Process seam required by
// the staged adapter. Keeping it local makes the candidate deterministic in
// tests without adding another process abstraction to the daemon.
type claudeCandidateProcess interface {
	WriteInputContext(context.Context, []byte) error
	Wait() execution.Result
	Terminate(context.Context, time.Duration) error
	ProcessDetails() (int, string)
}

type claudeCandidateProcessStarter func(context.Context, execution.Invocation, execution.Sink) (claudeCandidateProcess, error)

func claudeCandidateRunnerProcessStarter(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (claudeCandidateProcess, error) {
	process, err := execution.NewRunner().Start(ctx, invocation, sink)
	if process == nil {
		return nil, err
	}
	return process, err
}

// ClaudeCandidateAdapter is a deliberately unregistered, fail-closed Claude
// Code lifecycle candidate. It is not capability evidence and must not be
// composed into the default registry until credentialed native evidence exists.
type ClaudeCandidateAdapter struct {
	executable   string
	startProcess claudeCandidateProcessStarter
	newSessionID func() (string, error)
}

var _ Adapter = (*ClaudeCandidateAdapter)(nil)

// NewClaudeCandidateAdapter constructs the isolated Claude staged candidate.
// The constructor does not register or advertise the candidate anywhere.
func NewClaudeCandidateAdapter(executables ...string) *ClaudeCandidateAdapter {
	executable := claudeCandidateDefaultExecutable
	if len(executables) > 0 && strings.TrimSpace(executables[0]) != "" {
		executable = executables[0]
	}
	return &ClaudeCandidateAdapter{
		executable:   executable,
		startProcess: claudeCandidateRunnerProcessStarter,
		newSessionID: newClaudeCandidateSessionID,
	}
}

// Probe remains unverified by design. A concrete candidate is useful for
// deterministic lifecycle review without upgrading runtime admission.
func (adapter *ClaudeCandidateAdapter) Probe(ctx context.Context) (Capabilities, error) {
	if adapter == nil {
		return Capabilities{}, errors.New("Claude staged candidate adapter is nil")
	}
	if ctx == nil {
		return Capabilities{}, errors.New("Claude staged candidate probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	return UnsupportedCapabilities(KindClaude, "Claude staged candidate is not behaviorally verified or registered"), ErrNativeUnverified
}

// Start launches only the fresh stream-json process. It does not send a
// prompt, and it retains the process owner when execution reports a post-start
// error so the caller can still close and reap it.
func (adapter *ClaudeCandidateAdapter) Start(ctx context.Context, request StartRequest, sink EventSink) (Session, error) {
	if adapter == nil {
		return nil, errors.New("Claude staged candidate adapter is nil")
	}
	if ctx == nil {
		return nil, errors.New("Claude staged candidate start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Resume != nil {
		return nil, &CapabilityError{Kind: KindClaude, Capability: CapabilityResume, Reason: "Claude fresh staged candidate has no verified resume entry"}
	}
	if request.ProviderAccess != nil {
		return nil, &CapabilityError{Kind: KindClaude, Capability: CapabilityProviderAccess, Reason: "Claude staged candidate has no verified provider broker bridge"}
	}
	if request.Limits.MaxCostMicrousd != nil {
		return nil, &CapabilityError{Kind: KindClaude, Capability: CapabilityHardCostLimit, Reason: "Claude staged candidate has no verified provider-enforced hard cost limit"}
	}
	if strings.TrimSpace(request.Workspace) == "" || !filepath.IsAbs(request.Workspace) {
		return nil, errors.New("Claude staged candidate workspace must be an absolute path")
	}
	if sink == nil {
		return nil, errors.New("Claude staged candidate event sink must not be nil")
	}
	if len(request.Invocation.Args) != 0 {
		return nil, errors.New("Claude staged candidate does not accept profile arguments; its fresh transport argv is fixed")
	}
	if len(request.Invocation.InitialInput) != 0 || request.Invocation.CloseInputAfterInitial {
		return nil, errors.New("Claude staged candidate does not accept legacy initial input")
	}

	startProcess := adapter.startProcess
	if startProcess == nil {
		startProcess = claudeCandidateRunnerProcessStarter
	}
	newSessionID := adapter.newSessionID
	if newSessionID == nil {
		newSessionID = newClaudeCandidateSessionID
	}
	sessionID, err := newSessionID()
	if err != nil {
		return nil, fmt.Errorf("create fresh Claude session ID: %w", err)
	}
	if !isCanonicalClaudeSessionID(sessionID) {
		return nil, errors.New("Claude staged candidate session ID generator returned a non-canonical UUID")
	}

	processContext, cancel := context.WithCancel(ctx)
	session := newClaudeCandidateSession(processContext, cancel, sink, sessionID)
	invocation := execution.Invocation{
		Program: adapter.executable,
		Args: []string{
			"--print",
			"--verbose",
			"--input-format",
			"stream-json",
			"--output-format",
			"stream-json",
			"--session-id",
			sessionID,
		},
		Dir:            request.Workspace,
		Env:            append([]string(nil), request.Invocation.Env...),
		PersistProcess: request.PersistProcess,
	}
	process, startErr := startProcess(processContext, invocation, execution.SinkFunc(session.handleProcessOutput))
	if isNilClaudeCandidateProcess(process) {
		cancel()
		if startErr != nil {
			return nil, startErr
		}
		return nil, errClaudeCandidateProcessNil
	}
	if startErr != nil {
		// A non-nil process plus an error is a cleanup-owner transfer. The
		// process may have emitted bytes before the runner reported the error,
		// but those bytes are not admissible lifecycle evidence for this launch.
		session.fail(startErr)
	}

	// The runner may begin delivering output before Start returns. Buffer it
	// until the process owner is installed so Open cannot race process setup.
	session.outputMutex.Lock()
	session.mutex.Lock()
	session.process = process
	session.processReady = true
	queued := append([]execution.Event(nil), session.preReadyEvents...)
	session.preReadyEvents = nil
	session.preReadyBytes = 0
	outputFailure := session.failure
	session.mutex.Unlock()
	var outputErr error
	if startErr == nil && outputFailure == nil {
		for _, event := range queued {
			if err := session.handleProcessOutputLocked(processContext, event); err != nil && outputErr == nil {
				outputErr = err
			}
		}
	}
	session.outputMutex.Unlock()

	if outputErr != nil {
		session.fail(outputErr)
	}
	session.mutex.Lock()
	outputFailure = session.failure
	session.mutex.Unlock()
	go session.watchProcess()
	if startErr != nil {
		return session, startErr
	}
	if outputErr != nil {
		return session, outputErr
	}
	if outputFailure != nil {
		return session, outputFailure
	}
	return session, nil
}

func isNilClaudeCandidateProcess(process claudeCandidateProcess) bool {
	if process == nil {
		return true
	}
	value := reflect.ValueOf(process)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type claudeCandidateSession struct {
	context           context.Context
	cancel            context.CancelFunc
	sink              EventSink
	expectedSessionID string
	decoder           *claudeprotocol.Decoder
	validator         *claudeprotocol.TerminalValidator

	mutex       sync.Mutex
	outputMutex sync.Mutex
	emitMutex   sync.Mutex
	closeMutex  sync.Mutex
	writeMutex  sync.Mutex

	process        claudeCandidateProcess
	processReady   bool
	preReadyEvents []execution.Event
	preReadyBytes  int
	failure        error
	processResult  *execution.Result

	identityObserved     bool
	opened               bool
	opening              bool
	turnStarted          bool
	turnFinal            bool
	turnErr              error
	turnResult           *TaskResult
	terminal             *claudeprotocol.Terminal
	lastSequence         uint64
	eventCount           uint64
	cancelRequested      bool
	closing              bool
	closeNormalCandidate bool
	turnWriteCancel      context.CancelFunc

	openDone   chan struct{}
	openOnce   sync.Once
	turnDone   chan struct{}
	turnOnce   sync.Once
	resultDone chan struct{}
	resultOnce sync.Once
	result     *TaskResult
	watchDone  chan struct{}
	watchOnce  sync.Once

	closeAttempt   *claudeCandidateCloseAttempt
	closeSucceeded bool
	closeError     error
}

type claudeCandidateCloseAttempt struct {
	done chan struct{}
	err  error
}

func newClaudeCandidateSession(ctx context.Context, cancel context.CancelFunc, sink EventSink, sessionID string) *claudeCandidateSession {
	return &claudeCandidateSession{
		context:           ctx,
		cancel:            cancel,
		sink:              sink,
		expectedSessionID: sessionID,
		decoder:           claudeprotocol.NewDecoder(claudeCandidateMaxRecordBytes),
		validator:         claudeprotocol.NewTerminalValidator(sessionID),
		openDone:          make(chan struct{}),
		turnDone:          make(chan struct{}),
		resultDone:        make(chan struct{}),
		watchDone:         make(chan struct{}),
	}
}

func (session *claudeCandidateSession) ProcessDetails() (int, string) {
	if session == nil {
		return 0, ""
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.process == nil {
		return 0, ""
	}
	return session.process.ProcessDetails()
}

// Open never writes to stdin. It waits for the native system/init record to
// report the exact UUID supplied on the fresh argv.
func (session *claudeCandidateSession) Open(ctx context.Context) (NativeSessionHandle, error) {
	if session == nil {
		return NativeSessionHandle{}, errors.New("Claude staged candidate session is nil")
	}
	if ctx == nil {
		return NativeSessionHandle{}, errors.New("Claude staged candidate open context must not be nil")
	}
	session.mutex.Lock()
	if session.opened {
		handle := NativeSessionHandle{ID: session.expectedSessionID}
		session.mutex.Unlock()
		return handle, nil
	}
	if session.opening {
		session.mutex.Unlock()
		return NativeSessionHandle{}, errors.New("Claude staged candidate session is already opening")
	}
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		return NativeSessionHandle{}, err
	}
	session.opening = true
	session.mutex.Unlock()

	opened := false
	defer func() {
		if opened {
			return
		}
		session.mutex.Lock()
		session.opening = false
		session.mutex.Unlock()
	}()

	for {
		session.mutex.Lock()
		if err := session.usableLocked(); err != nil {
			session.mutex.Unlock()
			return NativeSessionHandle{}, err
		}
		if session.identityObserved {
			session.opened = true
			session.opening = false
			opened = true
			session.mutex.Unlock()
			return NativeSessionHandle{ID: session.expectedSessionID}, nil
		}
		session.mutex.Unlock()

		select {
		case <-session.openDone:
		case <-ctx.Done():
			return NativeSessionHandle{}, session.stopAfterOpenFailure(ctx.Err())
		case <-session.context.Done():
			session.mutex.Lock()
			err := session.sessionErrorLocked()
			session.mutex.Unlock()
			return NativeSessionHandle{}, session.stopAfterOpenFailure(err)
		}
	}
}

func (session *claudeCandidateSession) stopAfterOpenFailure(cause error) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), claudeCandidateCleanupTimeout)
	closeErr := session.Close(cleanupContext)
	cancel()
	if closeErr != nil {
		return errors.Join(cause, fmt.Errorf("stop Claude process after Open failure: %w", closeErr))
	}
	return cause
}

// StartTurn sends one exact Claude stream-json user envelope after Open. The
// prompt requires a canonical TaskResult, so a prose result can never become
// a successful Symmetry result.
func (session *claudeCandidateSession) StartTurn(ctx context.Context, request TurnRequest) error {
	if session == nil {
		return errors.New("Claude staged candidate session is nil")
	}
	if ctx == nil {
		return errors.New("Claude staged candidate start turn context must not be nil")
	}
	if strings.TrimSpace(request.Goal) == "" {
		return errors.New("Claude staged candidate turn goal must not be empty")
	}
	if !isClaudeCandidateJSONObject(request.Context) {
		return errors.New("Claude staged candidate turn context must be a JSON object")
	}
	prompt, err := buildClaudeCandidatePrompt(request.Goal, request.Context)
	if err != nil {
		return err
	}
	frame, err := json.Marshal(claudeCandidateUserFrame{
		Type: "user",
		Message: claudeCandidateUserMessage{
			Role:    "user",
			Content: prompt,
		},
	})
	if err != nil {
		return fmt.Errorf("encode Claude stream-json user frame: %w", err)
	}
	frame = append(frame, '\n')

	session.writeMutex.Lock()
	session.mutex.Lock()
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		session.writeMutex.Unlock()
		return err
	}
	if !session.opened {
		session.mutex.Unlock()
		session.writeMutex.Unlock()
		return errors.New("Claude staged candidate session has not opened")
	}
	if session.turnStarted {
		session.mutex.Unlock()
		session.writeMutex.Unlock()
		return errors.New("Claude staged candidate session already owns a turn")
	}
	session.turnStarted = true
	process := session.process
	writeContext, cancelWrite := context.WithCancel(ctx)
	session.turnWriteCancel = cancelWrite
	session.mutex.Unlock()
	session.writeMutex.Unlock()

	if err := session.emit(Event{Kind: EventSessionStarted}); err != nil {
		cancelWrite()
		session.clearTurnWriteCancel()
		session.fail(err)
		return err
	}

	// Close sets the fence and cancels writeContext before waiting on this
	// mutex. Rechecking the fence here prevents a close that raced the event
	// sink from allowing a prompt write after shutdown began.
	session.writeMutex.Lock()
	session.mutex.Lock()
	closing := session.closing || session.cancelRequested || session.failure != nil
	if process == nil {
		closing = true
	}
	if closing {
		err := session.sessionErrorLocked()
		session.mutex.Unlock()
		session.writeMutex.Unlock()
		cancelWrite()
		session.clearTurnWriteCancel()
		if err == nil {
			err = errClaudeCandidateSessionClosed
		}
		session.fail(err)
		return err
	}
	session.mutex.Unlock()

	writeErr := process.WriteInputContext(writeContext, frame)
	cancelWrite()
	session.clearTurnWriteCancel()
	session.writeMutex.Unlock()
	if writeErr != nil {
		session.fail(writeErr)
		return fmt.Errorf("write Claude stream-json user frame: %w", writeErr)
	}
	return nil
}

func (session *claudeCandidateSession) clearTurnWriteCancel() {
	session.mutex.Lock()
	session.turnWriteCancel = nil
	session.mutex.Unlock()
}

type claudeCandidateUserFrame struct {
	Type    string                     `json:"type"`
	Message claudeCandidateUserMessage `json:"message"`
}

type claudeCandidateUserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func buildClaudeCandidatePrompt(goal string, contextJSON json.RawMessage) (string, error) {
	schema, err := contracts.TaskResultSchema()
	if err != nil {
		return "", fmt.Errorf("load task result schema: %w", err)
	}
	encodedSchema, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("encode task result schema: %w", err)
	}
	return "Complete the engineering task. Return exactly one JSON object conforming to the supplied Symmetry TaskResult schema as the entire final assistant text. Do not use Markdown, prose, or code fences.\n\n" +
		"<symmetry_goal>\n" + goal + "\n</symmetry_goal>\n\n" +
		"<canonical_context_json>\n" + string(contextJSON) + "\n</canonical_context_json>\n\n" +
		"<symmetry_task_result_schema>\n" + string(encodedSchema) + "\n</symmetry_task_result_schema>", nil
}

// Control supports only bounded process cancellation. Native guidance,
// pause/resume, approval replies, and usage controls remain unsupported.
func (session *claudeCandidateSession) Control(ctx context.Context, request ControlRequest) (ControlReceipt, error) {
	if session == nil {
		return ControlReceipt{}, errors.New("Claude staged candidate session is nil")
	}
	if ctx == nil {
		return ControlReceipt{}, errors.New("Claude staged candidate control context must not be nil")
	}
	if request.Kind != ControlCancel {
		return ControlReceipt{
			CommandID:  request.CommandID,
			Kind:       request.Kind,
			Outcome:    ControlUnsupported,
			Capability: controlCapability(request.Kind),
			Message:    "Claude staged candidate native control is unverified for this operation",
			AppliedAt:  time.Now().UTC(),
		}, nil
	}
	session.mutex.Lock()
	turnStarted := session.turnStarted
	turnFinal := session.turnFinal
	cancelRequested := session.cancelRequested
	session.mutex.Unlock()
	if !turnStarted || (turnFinal && !cancelRequested) {
		return ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: ControlRejected, Capability: CapabilityCancel, Message: "Claude turn is not active", AppliedAt: time.Now().UTC()}, nil
	}
	if err := session.Close(ctx); err != nil {
		return ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: ControlFailed, Capability: CapabilityCancel, Message: err.Error(), AppliedAt: time.Now().UTC()}, err
	}
	return ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: ControlApplied, Capability: CapabilityCancel, Message: "Claude process termination completed", AppliedAt: time.Now().UTC()}, nil
}

func (session *claudeCandidateSession) WaitTurn(ctx context.Context) error {
	if session == nil {
		return errors.New("Claude staged candidate session is nil")
	}
	if ctx == nil {
		return errors.New("Claude staged candidate wait-turn context must not be nil")
	}
	session.mutex.Lock()
	if !session.opened {
		session.mutex.Unlock()
		return errors.New("Claude staged candidate session has not opened")
	}
	if !session.turnStarted {
		session.mutex.Unlock()
		return errors.New("Claude staged candidate turn has not started")
	}
	session.mutex.Unlock()
	select {
	case <-session.turnDone:
		session.mutex.Lock()
		err := session.turnErr
		session.mutex.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-session.context.Done():
		select {
		case <-session.turnDone:
			session.mutex.Lock()
			err := session.turnErr
			session.mutex.Unlock()
			return err
		default:
		}
		session.mutex.Lock()
		err := session.sessionErrorLocked()
		session.mutex.Unlock()
		return err
	}
}

func (session *claudeCandidateSession) Wait(ctx context.Context) (TaskResult, error) {
	if session == nil {
		return TaskResult{}, errors.New("Claude staged candidate session is nil")
	}
	if ctx == nil {
		return TaskResult{}, errors.New("Claude staged candidate wait context must not be nil")
	}
	select {
	case <-session.resultDone:
		session.mutex.Lock()
		defer session.mutex.Unlock()
		if session.result == nil {
			return TaskResult{}, errors.New("Claude staged candidate final result is unavailable")
		}
		return cloneClaudeCandidateTaskResult(*session.result), nil
	case <-ctx.Done():
		return TaskResult{Kind: ResultCancelled, Summary: "Claude staged candidate wait cancelled", Usage: Usage{State: UsageUnknown}}, ctx.Err()
	}
}

// Close owns bounded process-tree termination. A failed termination remains a
// stable observation because execution.Process.Terminate is idempotent and
// retains its first error; it never treats a notification as stop proof.
func (session *claudeCandidateSession) Close(ctx context.Context) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("Claude staged candidate close context must not be nil")
	}
	session.closeMutex.Lock()
	if session.closeSucceeded {
		session.closeMutex.Unlock()
		return nil
	}
	if session.closeError != nil {
		err := session.closeError
		session.closeMutex.Unlock()
		return err
	}
	if attempt := session.closeAttempt; attempt != nil {
		session.closeMutex.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &claudeCandidateCloseAttempt{done: make(chan struct{})}
	session.closeAttempt = attempt
	session.closeMutex.Unlock()
	session.requestClose()

	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), claudeCandidateCleanupTimeout)
	go func() {
		err := session.closeOnce(cleanupContext)
		cleanupCancel()
		session.closeMutex.Lock()
		attempt.err = err
		if err == nil {
			session.closeSucceeded = true
		} else {
			// Process.Terminate is idempotent and retains its first failure.
			// Preserve that observation instead of issuing another terminate
			// call that could be mistaken for a fresh cleanup attempt.
			session.closeError = err
		}
		if session.closeAttempt == attempt {
			session.closeAttempt = nil
		}
		close(attempt.done)
		session.closeMutex.Unlock()
	}()
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *claudeCandidateSession) requestClose() {
	session.mutex.Lock()
	session.closing = true
	if session.turnStarted && !session.turnFinal {
		session.cancelRequested = true
	}
	writeCancel := session.turnWriteCancel
	session.mutex.Unlock()
	if writeCancel != nil {
		writeCancel()
	}
}

func (session *claudeCandidateSession) closeOnce(ctx context.Context) error {
	// Wait for an in-flight prompt write after the close fence has cancelled it.
	// StartTurn cannot acquire this mutex and then successfully write once
	// requestClose has marked the session closing.
	session.writeMutex.Lock()
	session.mutex.Lock()
	process := session.process
	session.closeNormalCandidate = session.turnFinal && session.failure == nil
	session.mutex.Unlock()
	session.writeMutex.Unlock()
	if process == nil {
		return errors.New("Claude staged candidate process is unavailable")
	}
	session.cancel()
	if err := process.Terminate(ctx, claudeCandidateTerminationGrace); err != nil {
		return err
	}
	select {
	case <-session.watchDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *claudeCandidateSession) handleProcessOutput(ctx context.Context, event execution.Event) error {
	session.outputMutex.Lock()
	defer session.outputMutex.Unlock()
	session.mutex.Lock()
	if session.failure != nil {
		err := session.failure
		session.mutex.Unlock()
		return err
	}
	if session.closing {
		session.mutex.Unlock()
		return errClaudeCandidateSessionClosed
	}
	if !session.processReady {
		if len(session.preReadyEvents) >= claudeCandidateMaxPreReadyEvents || session.preReadyBytes+len(event.Data) > claudeCandidateMaxPreReadyBytes {
			session.mutex.Unlock()
			err := errors.New("Claude staged candidate output arrived before process readiness")
			session.fail(err)
			return err
		}
		copyEvent := execution.Event{Stream: event.Stream, Sequence: event.Sequence, At: event.At, Data: append([]byte(nil), event.Data...)}
		session.preReadyEvents = append(session.preReadyEvents, copyEvent)
		session.preReadyBytes += len(copyEvent.Data)
		session.mutex.Unlock()
		return nil
	}
	session.mutex.Unlock()
	return session.handleProcessOutputLocked(ctx, event)
}

func (session *claudeCandidateSession) handleProcessOutputLocked(ctx context.Context, event execution.Event) error {
	if event.Stream == execution.Stderr {
		if err := session.emit(Event{Kind: EventDiagnostic, Stream: string(event.Stream), Sequence: event.Sequence, At: event.At, Diagnostic: true, Code: "native_stderr", Message: "Claude emitted stderr"}); err != nil {
			session.fail(err)
			return err
		}
		return nil
	}
	if event.Stream != execution.Stdout {
		return nil
	}
	records, feedErr := session.decoder.Feed(event.Data)
	var firstErr error
	for _, record := range records {
		if err := session.handleClaudeRecord(ctx, event, record); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if feedErr != nil && firstErr == nil {
		firstErr = feedErr
	}
	if firstErr != nil {
		session.fail(firstErr)
	}
	return firstErr
}

func (session *claudeCandidateSession) handleClaudeRecord(_ context.Context, processEvent execution.Event, record claudeprotocol.Event) error {
	if record.DecodeError != nil {
		_ = session.emit(Event{Kind: EventDiagnostic, Stream: string(processEvent.Stream), Sequence: record.Sequence, At: processEvent.At, Payload: append(json.RawMessage(nil), record.Raw...), Diagnostic: true, Code: "invalid_native_record", Message: record.DecodeError.Error()})
		return record.DecodeError
	}
	if err := session.emit(Event{Kind: EventNativeFrame, Stream: string(processEvent.Stream), Sequence: record.Sequence, At: processEvent.At, Payload: append(json.RawMessage(nil), record.Raw...)}); err != nil {
		return err
	}

	session.mutex.Lock()
	observeErr := session.validator.Observe(record)
	identityObserved := false
	var terminal *claudeprotocol.Terminal
	if observeErr == nil && record.Type == claudeprotocol.EventSystem && record.Subtype == "init" {
		identityObserved = record.SessionID == session.expectedSessionID
		if identityObserved {
			session.identityObserved = true
		}
	}
	if observeErr == nil && record.Type == claudeprotocol.EventResult {
		if !session.turnStarted {
			observeErr = errClaudeCandidateResultBeforeTurn
		} else {
			observed, finishErr := session.validator.Finish()
			if finishErr != nil {
				observeErr = finishErr
			} else {
				copyOfTerminal := observed
				terminal = &copyOfTerminal
				session.terminal = &copyOfTerminal
			}
		}
	}
	session.lastSequence = maxUint64(session.lastSequence, record.Sequence)
	session.mutex.Unlock()
	if identityObserved {
		session.openOnce.Do(func() { close(session.openDone) })
	}
	if observeErr != nil {
		return observeErr
	}
	if terminal != nil {
		session.completeClaudeCandidateTerminal(*terminal)
	}
	return nil
}

func (session *claudeCandidateSession) completeClaudeCandidateTerminal(terminal claudeprotocol.Terminal) {
	var output string
	if err := json.Unmarshal(terminal.Result.Output, &output); err != nil || strings.TrimSpace(output) == "" {
		session.completeTurn(TaskResult{Kind: ResultUnknown, Summary: errClaudeCandidateNoSemanticResult.Error(), Usage: Usage{State: UsageUnknown}}, errClaudeCandidateNoSemanticResult, false)
		return
	}
	semantic, err := protocol.ParseTaskResult([]byte(output))
	if err != nil {
		session.completeTurn(TaskResult{Kind: ResultUnknown, Summary: errClaudeCandidateNoSemanticResult.Error(), Usage: Usage{State: UsageUnknown}}, fmt.Errorf("parse Claude TaskResult: %w", err), false)
		return
	}
	resultKind := ResultSucceeded
	if semantic.Kind == protocol.TaskResultFailed {
		resultKind = ResultFailed
	}
	session.completeTurn(TaskResult{Kind: resultKind, Summary: semantic.Summary, Semantic: &semantic, Reason: semantic.Reason, Usage: Usage{State: UsageUnknown}}, nil, true)
}

func (session *claudeCandidateSession) completeTurn(result TaskResult, turnErr error, emitTaskResult bool) {
	session.mutex.Lock()
	if session.turnFinal {
		session.mutex.Unlock()
		return
	}
	session.turnFinal = true
	sequence := session.lastSequence
	session.mutex.Unlock()
	if emitTaskResult {
		if err := session.emit(Event{Kind: EventTaskResult, Sequence: sequence, Payload: marshalClaudeCandidateTaskResult(result.Semantic)}); err != nil {
			turnErr = err
			reason := protocol.TaskResultReasonUnknownOutcome
			result = TaskResult{Kind: ResultFailed, Summary: err.Error(), Reason: &reason, Usage: Usage{State: UsageUnknown}}
			session.fail(err)
		}
	}
	session.mutex.Lock()
	stored := cloneClaudeCandidateTaskResult(result)
	session.turnResult = &stored
	session.turnErr = turnErr
	session.mutex.Unlock()
	session.turnOnce.Do(func() { close(session.turnDone) })
}

func (session *claudeCandidateSession) fail(cause error) {
	if cause == nil {
		return
	}
	session.mutex.Lock()
	if session.failure == nil {
		session.failure = cause
	}
	started := session.turnStarted
	final := session.turnFinal
	writeCancel := session.turnWriteCancel
	if final {
		session.turnErr = cause
		if session.turnResult != nil {
			reason := protocol.TaskResultReasonUnknownOutcome
			session.turnResult.Kind = ResultFailed
			session.turnResult.Reason = &reason
			session.turnResult.Semantic = nil
			session.turnResult.Summary = cause.Error()
		}
	}
	session.mutex.Unlock()
	if writeCancel != nil {
		writeCancel()
	}
	session.openOnce.Do(func() { close(session.openDone) })
	if started && !final {
		reason := protocol.TaskResultReasonUnknownOutcome
		session.completeTurn(TaskResult{Kind: ResultFailed, Summary: cause.Error(), Reason: &reason, Usage: Usage{State: UsageUnknown}}, cause, false)
	}
	session.cancel()
}

func (session *claudeCandidateSession) watchProcess() {
	session.watchOnce.Do(func() {
		defer close(session.watchDone)
		session.mutex.Lock()
		process := session.process
		session.mutex.Unlock()
		if process == nil {
			session.fail(errClaudeCandidateProcessNil)
			session.completeFinal(TaskResult{Kind: ResultFailed, Summary: errClaudeCandidateProcessNil.Error(), Usage: Usage{State: UsageUnknown}})
			return
		}
		processResult := process.Wait()
		session.outputMutex.Lock()
		trailing, closeErr := session.decoder.Close()
		for _, record := range trailing {
			_ = session.handleClaudeRecord(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: record.Sequence, At: time.Now().UTC()}, record)
		}
		session.outputMutex.Unlock()
		if closeErr != nil {
			session.fail(closeErr)
		}

		session.mutex.Lock()
		session.processResult = &processResult
		identityObserved := session.identityObserved
		turnStarted := session.turnStarted
		turnFinal := session.turnFinal
		cancelRequested := session.cancelRequested
		failure := session.failure
		normalClose := session.closeNormalCandidate
		turnResult := cloneClaudeCandidateTaskResultPointer(session.turnResult)
		turnErr := session.turnErr
		session.mutex.Unlock()
		if !identityObserved {
			if failure == nil {
				session.fail(errClaudeCandidateMissingInit)
			}
			session.openOnce.Do(func() { close(session.openDone) })
		}
		if turnStarted && !turnFinal {
			if cancelRequested {
				turnResult = &TaskResult{Kind: ResultCancelled, Summary: "Claude process was cancelled", Usage: Usage{State: UsageUnknown}}
				turnErr = nil
			} else {
				cause := failure
				if cause == nil {
					cause = errClaudeCandidateMissingResult
				}
				reason := protocol.TaskResultReasonMissingResult
				if !processResult.Success() {
					reason = protocol.TaskResultReasonProcessFailure
				}
				turnResult = &TaskResult{Kind: ResultFailed, Summary: cause.Error(), Reason: &reason, Usage: Usage{State: UsageUnknown}}
				turnErr = cause
			}
			session.completeTurn(*turnResult, turnErr, false)
		}
		if turnResult == nil {
			turnResult = &TaskResult{Kind: ResultFailed, Summary: errClaudeCandidateMissingResult.Error(), Usage: Usage{State: UsageUnknown}}
		}
		final := cloneClaudeCandidateTaskResult(*turnResult)
		final.Process = processResult
		session.mutex.Lock()
		final.EventCount = session.eventCount
		final.LastSequence = session.lastSequence
		session.mutex.Unlock()
		expectedCancelled := expectedClaudeCandidateCancelled(processResult, cancelRequested)
		if (!processResult.Success() && !expectedClaudeCandidateNormalClose(processResult, normalClose) && !expectedCancelled) ||
			(final.Kind == ResultCancelled && !expectedCancelled) {
			reason := protocol.TaskResultReasonProcessFailure
			final.Kind = ResultFailed
			final.Reason = &reason
			final.Semantic = nil
			final.Summary = "Claude process failed: " + summarizeClaudeCandidateProcessFailure(processResult)
		}
		if failure != nil {
			reason := protocol.TaskResultReasonUnknownOutcome
			final.Kind = ResultFailed
			final.Reason = &reason
			final.Semantic = nil
			final.Summary = failure.Error()
		}
		if turnErr != nil && final.Kind == ResultSucceeded {
			reason := protocol.TaskResultReasonUnknownOutcome
			final.Kind = ResultFailed
			final.Reason = &reason
			final.Semantic = nil
			final.Summary = turnErr.Error()
		}
		session.completeFinal(final)
	})
}

func expectedClaudeCandidateNormalClose(result execution.Result, candidate bool) bool {
	return candidate && result.Terminated && result.SinkError == nil && result.OutputError == nil && result.TerminationError == nil && result.ContainmentError == nil
}

func expectedClaudeCandidateCancelled(result execution.Result, requested bool) bool {
	return requested && result.Terminated && result.WaitError == nil && result.SinkError == nil && result.OutputError == nil && result.TerminationError == nil && result.ContainmentError == nil
}

func summarizeClaudeCandidateProcessFailure(result execution.Result) string {
	for _, err := range []error{result.WaitError, result.SinkError, result.OutputError, result.TerminationError, result.ContainmentError} {
		if err != nil {
			return err.Error()
		}
	}
	return "process ended without a successful execution result"
}

func (session *claudeCandidateSession) completeFinal(result TaskResult) {
	session.resultOnce.Do(func() {
		stored := cloneClaudeCandidateTaskResult(result)
		session.mutex.Lock()
		session.result = &stored
		session.mutex.Unlock()
		close(session.resultDone)
	})
}

func (session *claudeCandidateSession) emit(event Event) error {
	session.emitMutex.Lock()
	defer session.emitMutex.Unlock()
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if err := session.sink.Handle(session.context, event); err != nil {
		return fmt.Errorf("persist Claude native event: %w", err)
	}
	session.mutex.Lock()
	session.eventCount++
	if event.Sequence > session.lastSequence {
		session.lastSequence = event.Sequence
	}
	session.mutex.Unlock()
	return nil
}

func (session *claudeCandidateSession) usableLocked() error {
	if session.failure != nil {
		return session.failure
	}
	if session.result != nil {
		return errors.New("Claude staged candidate process has completed")
	}
	if session.closing {
		return errClaudeCandidateSessionClosed
	}
	if err := session.context.Err(); err != nil {
		return err
	}
	if session.process == nil {
		return errors.New("Claude staged candidate process is unavailable")
	}
	return nil
}

func (session *claudeCandidateSession) sessionErrorLocked() error {
	if session.failure != nil {
		return session.failure
	}
	if err := session.context.Err(); err != nil {
		return err
	}
	return errClaudeCandidateSessionClosed
}

func isClaudeCandidateJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func newClaudeCandidateSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func isCanonicalClaudeSessionID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return value[14] == '4' && strings.ContainsRune("89ab", rune(value[19]))
}

func maxUint64(left, right uint64) uint64 {
	if left > right {
		return left
	}
	return right
}

func marshalClaudeCandidateTaskResult(result *protocol.TaskResult) json.RawMessage {
	if result == nil {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	return encoded
}

func cloneClaudeCandidateTaskResultPointer(result *TaskResult) *TaskResult {
	if result == nil {
		return nil
	}
	cloned := cloneClaudeCandidateTaskResult(*result)
	return &cloned
}

func cloneClaudeCandidateTaskResult(result TaskResult) TaskResult {
	cloned := result
	if result.Semantic != nil {
		semantic := cloneClaudeCandidateProtocolTaskResult(*result.Semantic)
		cloned.Semantic = &semantic
	}
	if result.Reason != nil {
		reason := *result.Reason
		cloned.Reason = &reason
	}
	return cloned
}

func cloneClaudeCandidateProtocolTaskResult(result protocol.TaskResult) protocol.TaskResult {
	cloned := result
	cloned.EvidenceRefs = slices.Clone(result.EvidenceRefs)
	if result.Blocker != nil {
		blocker := cloneClaudeCandidateBlocker(*result.Blocker)
		cloned.Blocker = &blocker
	}
	if result.ProposedNextAction != nil {
		action := *result.ProposedNextAction
		if result.ProposedNextAction.Blocker != nil {
			blocker := cloneClaudeCandidateBlocker(*result.ProposedNextAction.Blocker)
			action.Blocker = &blocker
		}
		cloned.ProposedNextAction = &action
	}
	if result.Proposal != nil {
		proposal := append(json.RawMessage(nil), (*result.Proposal)...)
		cloned.Proposal = &proposal
	}
	if result.Reason != nil {
		reason := *result.Reason
		cloned.Reason = &reason
	}
	if result.Diagnostics != nil {
		cloned.Diagnostics = make([]protocol.Diagnostic, len(result.Diagnostics))
		for index, diagnostic := range result.Diagnostics {
			cloned.Diagnostics[index] = cloneClaudeCandidateDiagnostic(diagnostic)
		}
	}
	return cloned
}

func cloneClaudeCandidateBlocker(blocker protocol.Blocker) protocol.Blocker {
	cloned := blocker
	cloned.WorkItemIDs = slices.Clone(blocker.WorkItemIDs)
	return cloned
}

func cloneClaudeCandidateDiagnostic(diagnostic protocol.Diagnostic) protocol.Diagnostic {
	// Diagnostic keeps a private parsed-field map. A JSON round trip rebuilds
	// that map without coupling this package to the protocol's private state.
	encoded, err := json.Marshal(diagnostic)
	if err == nil {
		var cloned protocol.Diagnostic
		if json.Unmarshal(encoded, &cloned) == nil {
			return cloned
		}
	}
	return diagnostic
}
