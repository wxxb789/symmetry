package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	processCleanupTimeout   = 5 * time.Second
	processTerminationGrace = 5 * time.Second
	nativeCancelTimeout     = 5 * time.Second
	maxPreReadyEvents       = 64
	maxPreReadyBytes        = 1 << 20
)

var (
	errNativeSessionClosed = errors.New("pi native session is closed")
	errNativeProcessNil    = errors.New("pi native process starter returned a nil process")
	// ErrResumeRejected identifies a local pi resume that cannot safely bind to
	// the retained native session selected by the daemon.
	ErrResumeRejected = errors.New("pi native resume rejected")
)

// ResumeRejectedError preserves the native or local validation cause while
// allowing the caller to classify the admission as resume_rejected.
type ResumeRejectedError struct {
	Cause error
}

func (err *ResumeRejectedError) Error() string {
	if err == nil || err.Cause == nil {
		return ErrResumeRejected.Error()
	}
	return fmt.Sprintf("%s: %v", ErrResumeRejected, err.Cause)
}

func (err *ResumeRejectedError) Unwrap() []error {
	if err == nil || err.Cause == nil {
		return []error{ErrResumeRejected}
	}
	return []error{ErrResumeRejected, err.Cause}
}

func resumeRejected(cause error) error {
	if cause == nil {
		return &ResumeRejectedError{}
	}
	var rejected *ResumeRejectedError
	if errors.As(cause, &rejected) {
		return cause
	}
	return &ResumeRejectedError{Cause: cause}
}

// nativeProcess is the test seam at the existing execution.Runner boundary.
// Production construction always uses execution.NewRunner.
type nativeProcess interface {
	WriteInputContext(context.Context, []byte) error
	Wait() execution.Result
	Terminate(context.Context, time.Duration) error
	ProcessDetails() (int, string)
}

type processStarter func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error)

func runnerProcessStarter(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (nativeProcess, error) {
	process, err := execution.NewRunner().Start(ctx, invocation, sink)
	if err != nil {
		return nil, err
	}
	return process, nil
}

// Adapter is private integration work for pi RPC. Probe remains fail-closed;
// registry integration must not expose this transport as a supported adapter
// until credentialed lifecycle evidence exists.
type Adapter struct {
	executable    string
	runner        CommandRunner
	startProcess  processStarter
	cancelTimeout time.Duration
}

// NewAdapter creates a pi --mode rpc transport adapter.
func NewAdapter(executables ...string) *Adapter {
	executable := DefaultExecutable
	if len(executables) > 0 && strings.TrimSpace(executables[0]) != "" {
		executable = executables[0]
	}
	return &Adapter{executable: executable, startProcess: runnerProcessStarter, cancelTimeout: nativeCancelTimeout}
}

// NewAdapterWithRunner creates an adapter with an injectable probe runner.
func NewAdapterWithRunner(executable string, runner CommandRunner) *Adapter {
	adapter := NewAdapter(executable)
	adapter.runner = runner
	return adapter
}

// Probe exposes conservative version/help evidence and always retains native
// lifecycle unverified status.
func (adapter *Adapter) Probe(ctx context.Context) (harness.Capabilities, error) {
	if adapter == nil {
		return harness.Capabilities{}, errors.New("pi adapter is nil")
	}
	var result ProbeResult
	var err error
	if adapter.runner == nil {
		result, err = Probe(ctx, adapter.executable)
	} else {
		result, err = Probe(ctx, adapter.executable, adapter.runner)
	}
	return result.Capabilities, err
}

// Start launches only pi's retained RPC transport. It neither creates a
// session nor sends a prompt, preserving the process and handle persistence
// barriers required by harness.StagedSession.
func (adapter *Adapter) Start(ctx context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	if adapter == nil {
		return nil, errors.New("pi adapter is nil")
	}
	if ctx == nil {
		return nil, errors.New("pi start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Workspace) == "" || !filepath.IsAbs(request.Workspace) {
		return nil, errors.New("pi workspace must be an absolute path")
	}
	if sink == nil {
		return nil, errors.New("pi event sink must not be nil")
	}
	if request.ProviderAccess != nil {
		return nil, unsupported(harness.CapabilityProviderAccess, "pi provider broker bridge is not verified")
	}
	if request.Limits.MaxCostMicrousd != nil {
		return nil, unsupported(harness.CapabilityHardCostLimit, "pi provider-enforced hard cost limits are not verified")
	}
	if len(request.Invocation.InitialInput) != 0 || request.Invocation.CloseInputAfterInitial {
		return nil, errors.New("pi native transport does not accept legacy initial input")
	}
	resumeState, err := piResumeState(request.Resume)
	if err != nil {
		return nil, err
	}
	args, err := piRPCArgs(request.Invocation.Args, resumeState)
	if err != nil {
		return nil, err
	}
	if adapter.startProcess == nil {
		adapter.startProcess = runnerProcessStarter
	}

	processContext, cancel := context.WithCancel(ctx)
	cancelTimeout := adapter.cancelTimeout
	if cancelTimeout <= 0 {
		cancelTimeout = nativeCancelTimeout
	}
	session := newNativeSession(processContext, cancel, sink, cancelTimeout, resumeState)
	invocation := execution.Invocation{
		Program:        adapter.executable,
		Args:           args,
		Dir:            request.Workspace,
		Env:            append([]string(nil), request.Invocation.Env...),
		PersistProcess: request.PersistProcess,
	}
	process, err := adapter.startProcess(processContext, invocation, execution.SinkFunc(session.handleProcessOutput))
	if err != nil {
		cancel()
		return nil, err
	}
	if isNilNativeProcess(process) {
		cancel()
		return nil, errNativeProcessNil
	}

	session.outputMutex.Lock()
	session.mutex.Lock()
	session.process = process
	session.processReady = true
	queued := append([]execution.Event(nil), session.preReadyEvents...)
	session.preReadyEvents = nil
	session.preReadyBytes = 0
	session.mutex.Unlock()
	for _, event := range queued {
		if err := session.handleProcessOutputLocked(processContext, event); err != nil {
			session.outputMutex.Unlock()
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), processCleanupTimeout)
			_ = process.Terminate(cleanupContext, 0)
			cleanupCancel()
			cancel()
			return nil, err
		}
	}
	session.outputMutex.Unlock()
	session.mutex.Lock()
	startFailure := session.failure
	session.mutex.Unlock()
	if startFailure != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), processCleanupTimeout)
		_ = process.Terminate(cleanupContext, 0)
		cleanupCancel()
		cancel()
		return nil, startFailure
	}
	go session.watchProcess()
	return session, nil
}

// piRPCArgs preserves profile flags while preventing a transport override or a
// positional startup prompt before Open establishes a native session handle.
// A resume session path is daemon-owned and cannot be supplied by a profile.
// execution.Runner launches argv directly, never through a command shell.
func piRPCArgs(profileArgs []string, resumeState *SessionState) ([]string, error) {
	args := make([]string, 0, len(profileArgs)+4)
	args = append(args, "--mode", "rpc")
	if resumeState != nil {
		args = append(args, "--session", resumeState.SessionFile)
	}
	for _, argument := range profileArgs {
		if strings.IndexByte(argument, 0) >= 0 {
			return nil, errors.New("pi invocation argument contains NUL")
		}
		if argument == "--" {
			return nil, errors.New("pi invocation argument separator is not allowed for RPC transport")
		}
		if argument == "--mode" || strings.HasPrefix(argument, "--mode=") {
			return nil, errors.New("pi invocation must not override required --mode rpc transport")
		}
		if forbiddenFreshSessionArgument(argument) {
			return nil, fmt.Errorf("pi invocation argument %q is not allowed for a fresh or handoff RPC session", argument)
		}
		args = append(args, argument)
	}
	return args, nil
}

// piResumeState validates the daemon-local handle before pi is launched. pi
// remains responsible for loading the session, and Open then binds the exact
// returned session ID and file through a correlated get_state response.
func piResumeState(resume *harness.ResumeHandle) (*SessionState, error) {
	if resume == nil {
		return nil, nil
	}
	if strings.TrimSpace(resume.LocalHandleID) == "" {
		return nil, resumeRejected(errors.New("local handle ID is missing"))
	}
	if strings.TrimSpace(resume.NativeSessionID) == "" {
		return nil, resumeRejected(errors.New("native session ID is missing"))
	}
	if strings.TrimSpace(resume.NativeSessionFilename) == "" {
		return nil, resumeRejected(errors.New("native session filename is missing"))
	}
	if !filepath.IsAbs(resume.NativeSessionFilename) {
		return nil, resumeRejected(errors.New("native session filename must be absolute"))
	}
	if strings.IndexByte(resume.NativeSessionFilename, 0) >= 0 {
		return nil, resumeRejected(errors.New("native session filename contains NUL"))
	}
	info, err := os.Stat(resume.NativeSessionFilename)
	if err != nil {
		return nil, resumeRejected(fmt.Errorf("native session file is unavailable: %w", err))
	}
	if !info.Mode().IsRegular() {
		return nil, resumeRejected(errors.New("native session filename is not a regular file"))
	}
	if strings.TrimSpace(resume.WorkspaceFingerprint) == "" {
		return nil, resumeRejected(errors.New("workspace fingerprint is missing"))
	}
	if resume.NativeVersion != TestedVersion {
		return nil, resumeRejected(fmt.Errorf("native version %q is not compatible with pi %s transport", resume.NativeVersion, TestedVersion))
	}
	return &SessionState{SessionID: resume.NativeSessionID, SessionFile: resume.NativeSessionFilename}, nil
}

func forbiddenFreshSessionArgument(argument string) bool {
	switch argument {
	case "--continue", "-c", "--resume", "-r", "--session", "--session-id", "--fork", "--no-session":
		return true
	}
	return strings.HasPrefix(argument, "--continue=") || strings.HasPrefix(argument, "--resume=") ||
		strings.HasPrefix(argument, "--session=") || strings.HasPrefix(argument, "--session-id=") ||
		strings.HasPrefix(argument, "--fork=")
}

func isNilNativeProcess(process nativeProcess) bool {
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

type nativeSession struct {
	context context.Context
	cancel  context.CancelFunc
	sink    harness.EventSink
	decoder *Decoder

	mutex       sync.Mutex
	outputMutex sync.Mutex
	emitMutex   sync.Mutex
	closeMutex  sync.Mutex
	cancelMutex sync.Mutex

	process              nativeProcess
	cancelTimeout        time.Duration
	processReady         bool
	preReadyEvents       []execution.Event
	preReadyBytes        int
	validator            *Validator
	pending              map[string]chan Response
	nextRequestID        uint64
	opened               bool
	opening              bool
	turnStarted          bool
	turnFinal            bool
	turnResult           *harness.TaskResult
	turnErr              error
	cancelApplied        bool
	cancelInFlight       bool
	closing              bool
	failure              error
	processResult        *execution.Result
	closeNormalCandidate bool
	resumeState          *SessionState

	turnDone   chan struct{}
	turnOnce   sync.Once
	resultDone chan struct{}
	resultOnce sync.Once
	result     *harness.TaskResult
	watchDone  chan struct{}
	watchOnce  sync.Once

	closeAttempt   *closeAttempt
	closeSucceeded bool
	eventCount     uint64
	lastSequence   uint64
}

type closeAttempt struct {
	done chan struct{}
	err  error
}

func newNativeSession(ctx context.Context, cancel context.CancelFunc, sink harness.EventSink, cancelTimeout time.Duration, resumeState *SessionState) *nativeSession {
	var expected *SessionState
	if resumeState != nil {
		copied := *resumeState
		expected = &copied
	}
	return &nativeSession{
		context:       ctx,
		cancel:        cancel,
		sink:          sink,
		cancelTimeout: cancelTimeout,
		decoder:       NewDecoder(defaultMaxRecordBytes),
		validator:     NewValidator(expected),
		resumeState:   expected,
		pending:       make(map[string]chan Response),
		turnDone:      make(chan struct{}),
		resultDone:    make(chan struct{}),
		watchDone:     make(chan struct{}),
	}
}

// ProcessDetails exposes the exact process identity persisted by Start.
func (session *nativeSession) ProcessDetails() (int, string) {
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

// Open establishes a retained native session by matching the get_state
// response. It creates no prompt and publishes no generic session event.
func (session *nativeSession) Open(ctx context.Context) (harness.NativeSessionHandle, error) {
	if session == nil {
		return harness.NativeSessionHandle{}, errors.New("pi native session is nil")
	}
	if ctx == nil {
		return harness.NativeSessionHandle{}, errors.New("pi open context must not be nil")
	}
	session.mutex.Lock()
	if session.opened {
		state, ok := session.validator.SessionState()
		session.mutex.Unlock()
		if !ok {
			return harness.NativeSessionHandle{}, ErrMissingSessionState
		}
		return harness.NativeSessionHandle{ID: state.SessionID, Filename: state.SessionFile}, nil
	}
	if session.opening {
		session.mutex.Unlock()
		return harness.NativeSessionHandle{}, errors.New("pi native session is already opening")
	}
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		return harness.NativeSessionHandle{}, err
	}
	session.opening = true
	id := session.requestIDLocked("state")
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
	request, err := GetStateRequest(id)
	if err != nil {
		return harness.NativeSessionHandle{}, err
	}
	if _, err := session.call(ctx, request); err != nil {
		if session.resumeState != nil {
			return harness.NativeSessionHandle{}, resumeRejected(fmt.Errorf("get resumed pi session state: %w", err))
		}
		return harness.NativeSessionHandle{}, fmt.Errorf("get pi session state: %w", err)
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if err := session.usableLocked(); err != nil {
		return harness.NativeSessionHandle{}, err
	}
	state, ok := session.validator.SessionState()
	if !ok {
		return harness.NativeSessionHandle{}, ErrMissingSessionState
	}
	if session.resumeState != nil {
		if err := state.ValidateResumeReady(); err != nil {
			return harness.NativeSessionHandle{}, resumeRejected(err)
		}
	}
	session.opened = true
	session.opening = false
	opened = true
	return harness.NativeSessionHandle{ID: state.SessionID, Filename: state.SessionFile}, nil
}

// StartTurn sends one prompt after Open and after the caller has persisted the
// opaque native handle. pi has no output-schema parameter, so the exact output
// convention is constrained locally and decoded only from a full final message.
func (session *nativeSession) StartTurn(ctx context.Context, request harness.TurnRequest) error {
	if session == nil {
		return errors.New("pi native session is nil")
	}
	if ctx == nil {
		return errors.New("pi start turn context must not be nil")
	}
	if strings.TrimSpace(request.Goal) == "" || !isJSONObject(request.Context) {
		return errors.New("pi turn requires a non-empty goal and JSON object context")
	}
	prompt, err := buildTurnPrompt(request.Goal, request.Context)
	if err != nil {
		return err
	}
	session.mutex.Lock()
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		return err
	}
	if !session.opened {
		session.mutex.Unlock()
		return errors.New("pi native session has not opened")
	}
	if session.turnStarted {
		session.mutex.Unlock()
		return errors.New("pi native session already owns a turn")
	}
	session.turnStarted = true
	id := session.requestIDLocked("prompt")
	session.mutex.Unlock()
	if err := session.emit(harness.Event{Kind: harness.EventSessionStarted}); err != nil {
		session.fail(err)
		return err
	}
	nativeRequest, err := PromptRequest(id, prompt, "")
	if err != nil {
		session.fail(err)
		return err
	}
	if _, err := session.call(ctx, nativeRequest); err != nil {
		session.fail(err)
		return fmt.Errorf("start pi prompt: %w", err)
	}
	session.tryFinalizeTurn()
	return nil
}

func buildTurnPrompt(goal string, contextJSON json.RawMessage) (string, error) {
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

// Control supports only documented cancellation. Guidance, pause, resume,
// approval, usage, and hard-cost controls remain explicitly unavailable.
func (session *nativeSession) Control(ctx context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	if session == nil {
		return harness.ControlReceipt{}, errors.New("pi native session is nil")
	}
	if ctx == nil {
		return harness.ControlReceipt{}, errors.New("pi control context must not be nil")
	}
	if request.Kind != harness.ControlCancel {
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlUnsupported, Capability: capabilityForControl(request.Kind), Message: "pi native control is unverified for this operation", AppliedAt: time.Now().UTC()}, nil
	}
	session.cancelMutex.Lock()
	defer session.cancelMutex.Unlock()
	session.mutex.Lock()
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		return harness.ControlReceipt{}, err
	}
	if session.cancelApplied {
		session.mutex.Unlock()
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlApplied, Capability: harness.CapabilityCancel, Message: "pi cancellation is already accepted; awaiting agent_settled", AppliedAt: time.Now().UTC()}, nil
	}
	if !session.turnStarted || session.turnFinal || session.validator.Settled() {
		session.mutex.Unlock()
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlRejected, Capability: harness.CapabilityCancel, Message: "pi turn is already settled or not active", AppliedAt: time.Now().UTC()}, nil
	}
	clearID := session.requestIDLocked("clear")
	abortID := session.requestIDLocked("abort")
	session.mutex.Unlock()
	commands, err := ClearThenAbortRequests(clearID, abortID)
	if err != nil {
		return harness.ControlReceipt{}, err
	}
	session.mutex.Lock()
	session.cancelInFlight = true
	session.mutex.Unlock()
	if _, err := session.call(ctx, commands[0]); err != nil {
		session.finishCancelAttempt()
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlFailed, Capability: harness.CapabilityCancel, Message: err.Error(), AppliedAt: time.Now().UTC()}, err
	}
	if _, err := session.call(ctx, commands[1]); err != nil {
		session.finishCancelAttempt()
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlFailed, Capability: harness.CapabilityCancel, Message: err.Error(), AppliedAt: time.Now().UTC()}, err
	}
	session.mutex.Lock()
	session.cancelInFlight = false
	session.cancelApplied = true
	session.mutex.Unlock()
	session.tryFinalizeTurn()
	return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlApplied, Capability: harness.CapabilityCancel, Message: "pi clear_queue and abort were accepted; awaiting agent_settled", AppliedAt: time.Now().UTC()}, nil
}

func capabilityForControl(kind harness.ControlKind) harness.Capability {
	switch kind {
	case harness.ControlCancel:
		return harness.CapabilityCancel
	case harness.ControlGuidance:
		return harness.CapabilityGuidance
	case harness.ControlPause:
		return harness.CapabilityPause
	case harness.ControlResume:
		return harness.CapabilityResume
	case harness.ControlApprovalResponse:
		return harness.CapabilityApprovalResponse
	default:
		return harness.CapabilityCancel
	}
}

func unsupported(capability harness.Capability, reason string) error {
	return &harness.CapabilityError{Kind: harness.KindPi, Capability: capability, Reason: reason}
}

// WaitTurn waits for native settlement and explicit complete structured output;
// it never returns a TaskResult before the physical process final barrier.
func (session *nativeSession) WaitTurn(ctx context.Context) error {
	if session == nil {
		return errors.New("pi native session is nil")
	}
	if ctx == nil {
		return errors.New("pi wait-turn context must not be nil")
	}
	select {
	case <-session.turnDone:
		session.mutex.Lock()
		err := session.turnErr
		session.mutex.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-session.context.Done():
		session.mutex.Lock()
		err := session.sessionErrorLocked()
		session.mutex.Unlock()
		return err
	}
}

// Wait joins process exit, all bounded output decoding, and event-sink delivery.
func (session *nativeSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	if session == nil {
		return harness.TaskResult{}, errors.New("pi native session is nil")
	}
	if ctx == nil {
		return harness.TaskResult{}, errors.New("pi wait context must not be nil")
	}
	select {
	case <-session.resultDone:
		session.mutex.Lock()
		defer session.mutex.Unlock()
		if session.result == nil {
			return harness.TaskResult{}, errors.New("pi final result is unavailable")
		}
		return cloneTaskResult(*session.result), nil
	case <-ctx.Done():
		return harness.TaskResult{Kind: harness.ResultCancelled, Summary: "pi wait cancelled"}, ctx.Err()
	}
}

// Close performs at most one in-flight shutdown. Failed termination attempts
// remain retryable; only a joined successful stop becomes permanently closed.
func (session *nativeSession) Close(ctx context.Context) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("pi close context must not be nil")
	}
	session.closeMutex.Lock()
	if session.closeSucceeded {
		session.closeMutex.Unlock()
		return nil
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
	attempt := &closeAttempt{done: make(chan struct{})}
	session.closeAttempt = attempt
	session.closeMutex.Unlock()
	// The initiating caller may leave at any time, but a close that has begun
	// must still attempt bounded process-tree cleanup. WithoutCancel preserves
	// request values without deriving an unbounded Background operation.
	cleanupContext, cleanupCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		session.cancelTimeout+processCleanupTimeout+processTerminationGrace,
	)
	go func() {
		err := session.closeOnce(cleanupContext)
		cleanupCancel()
		session.closeMutex.Lock()
		attempt.err = err
		if err == nil {
			session.closeSucceeded = true
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

func (session *nativeSession) closeOnce(ctx context.Context) error {
	session.mutex.Lock()
	process := session.process
	active := session.turnStarted && !session.turnFinal && !session.validator.Settled()
	session.mutex.Unlock()
	if active {
		// A failed native cancel must remain visible in the turn result, but a
		// later successful process-tree cleanup still satisfies Close itself.
		cancelContext, cancel := context.WithTimeout(ctx, session.cancelTimeout)
		_, _ = session.Control(cancelContext, harness.ControlRequest{CommandID: "close", Kind: harness.ControlCancel})
		cancel()
	}
	session.mutex.Lock()
	session.closeNormalCandidate = session.turnFinal && session.failure == nil
	session.closing = true
	session.mutex.Unlock()
	if process == nil {
		return nil
	}
	// The Runner stops output delivery on termination, but its sink context is
	// distinct from the session-owned EventSink context. Cancel this context
	// before cleanup so a blocked sink cannot indefinitely delay Close.
	session.cancel()
	terminateErr := process.Terminate(ctx, processTerminationGrace)
	if terminateErr != nil {
		return terminateErr
	}
	select {
	case <-session.watchDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *nativeSession) call(ctx context.Context, request Request) (Response, error) {
	if ctx == nil {
		return Response{}, errors.New("pi request context must not be nil")
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("encode pi request: %w", err)
	}
	encoded = append(encoded, '\n')
	response := make(chan Response, 1)
	session.mutex.Lock()
	if err := session.usableLocked(); err != nil {
		session.mutex.Unlock()
		return Response{}, err
	}
	if err := session.validator.Register(request); err != nil {
		session.mutex.Unlock()
		session.fail(err)
		return Response{}, err
	}
	session.pending[request.ID] = response
	process := session.process
	session.mutex.Unlock()
	if process == nil {
		err := errors.New("pi native process is unavailable")
		session.expireRequest(request.ID, err)
		return Response{}, err
	}
	if err := process.WriteInputContext(ctx, encoded); err != nil {
		session.expireRequest(request.ID, err)
		return Response{}, err
	}
	select {
	case value := <-response:
		return value, nil
	case <-ctx.Done():
		session.expireRequest(request.ID, ctx.Err())
		return Response{}, ctx.Err()
	case <-session.context.Done():
		session.mutex.Lock()
		err := session.sessionErrorLocked()
		session.mutex.Unlock()
		return Response{}, err
	}
}

func (session *nativeSession) expireRequest(id string, cause error) {
	session.mutex.Lock()
	delete(session.pending, id)
	err := session.validator.Expire(id)
	if err == nil {
		err = cause
	}
	session.mutex.Unlock()
	session.fail(err)
}

func (session *nativeSession) requestIDLocked(prefix string) string {
	session.nextRequestID++
	return fmt.Sprintf("symmetry-%s-%d", prefix, session.nextRequestID)
}

func (session *nativeSession) handleProcessOutput(ctx context.Context, event execution.Event) error {
	session.outputMutex.Lock()
	defer session.outputMutex.Unlock()
	session.mutex.Lock()
	if !session.processReady {
		if len(session.preReadyEvents) >= maxPreReadyEvents || session.preReadyBytes+len(event.Data) > maxPreReadyBytes {
			session.mutex.Unlock()
			err := errors.New("pi process output arrived before process readiness")
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

func (session *nativeSession) handleProcessOutputLocked(ctx context.Context, event execution.Event) error {
	if event.Stream == execution.Stderr {
		err := session.emit(harness.Event{Kind: harness.EventDiagnostic, Stream: string(event.Stream), Sequence: event.Sequence, At: event.At, Diagnostic: true, Code: "native_stderr", Message: "pi emitted stderr"})
		if err != nil {
			session.fail(err)
		}
		return err
	}
	if event.Stream != execution.Stdout {
		return nil
	}
	records, err := session.decoder.Feed(event.Data)
	for _, record := range records {
		session.noteSequence(record.Sequence)
		if handleErr := session.handleRecord(ctx, event, record); handleErr != nil && err == nil {
			err = handleErr
		}
	}
	if err != nil {
		session.fail(err)
	}
	return err
}

func (session *nativeSession) handleRecord(ctx context.Context, processEvent execution.Event, record Record) error {
	if record.DecodeError != nil {
		_ = session.emit(harness.Event{Kind: harness.EventDiagnostic, Stream: string(processEvent.Stream), Sequence: record.Sequence, At: processEvent.At, Diagnostic: true, Code: "invalid_native_record", Message: record.DecodeError.Error()})
		return record.DecodeError
	}
	if err := session.emit(harness.Event{Kind: harness.EventNativeFrame, Stream: string(processEvent.Stream), Sequence: record.Sequence, At: processEvent.At}); err != nil {
		return err
	}
	session.mutex.Lock()
	err := session.validator.Observe(record)
	var waiter chan Response
	if err == nil && record.Response != nil {
		if record.Response.Command == CommandAbort && record.Response.Success {
			// Record cancellation at the correlated native acknowledgement, before
			// waking the Control caller. A process exit can otherwise race that
			// goroutine and be incorrectly reported as a process failure.
			session.cancelInFlight = false
			session.cancelApplied = true
		}
		waiter = session.pending[record.Response.ID]
		delete(session.pending, record.Response.ID)
	}
	session.mutex.Unlock()
	if err != nil {
		return err
	}
	if waiter != nil {
		waiter <- *record.Response
	}
	if record.Event != nil && record.Event.Type == EventMessageUpdate {
		// Deltas are journaled as native frames only. They are never assembled or
		// searched for a semantic TaskResult.
	}
	session.tryFinalizeTurn()
	return nil
}

func (session *nativeSession) tryFinalizeTurn() {
	session.mutex.Lock()
	if !session.turnStarted || session.turnFinal || session.failure != nil {
		session.mutex.Unlock()
		return
	}
	completion, err := session.validator.Finish()
	cancelApplied := session.cancelApplied
	cancelInFlight := session.cancelInFlight
	settled := session.validator.Settled()
	session.mutex.Unlock()
	if settled && cancelInFlight {
		return
	}
	if err != nil {
		if isTemporaryFinishError(err) {
			return
		}
		if settled && cancelApplied && !errors.Is(err, ErrMissingSessionState) {
			session.completeTurn(harness.TaskResult{Kind: harness.ResultCancelled, Summary: "pi abort was accepted and the native agent settled", Usage: harness.Usage{State: harness.UsageUnknown}}, nil, false)
			return
		}
		if settled {
			reason := protocol.TaskResultReasonMissingResult
			session.completeTurn(harness.TaskResult{Kind: harness.ResultFailed, Summary: err.Error(), Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}, err, false)
		}
		return
	}
	if cancelApplied {
		session.completeTurn(harness.TaskResult{Kind: harness.ResultCancelled, Summary: "pi abort was accepted and the native agent settled", Usage: harness.Usage{State: harness.UsageUnknown}}, nil, false)
		return
	}
	semantic, err := DecodeAssistantTaskResult(completion.FinalAssistant)
	if err != nil {
		reason := protocol.TaskResultReasonMissingResult
		session.completeTurn(harness.TaskResult{Kind: harness.ResultFailed, Summary: err.Error(), Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}, err, false)
		return
	}
	resultKind := harness.ResultSucceeded
	if semantic.Kind == protocol.TaskResultFailed {
		resultKind = harness.ResultFailed
	}
	result := harness.TaskResult{Kind: resultKind, Summary: semantic.Summary, Semantic: &semantic, Reason: semantic.Reason, Usage: harness.Usage{State: harness.UsageUnknown}}
	session.completeTurn(result, nil, true)
}

func (session *nativeSession) finishCancelAttempt() {
	session.mutex.Lock()
	session.cancelInFlight = false
	session.mutex.Unlock()
	session.tryFinalizeTurn()
}

func isTemporaryFinishError(err error) bool {
	return errors.Is(err, ErrNotSettled) || errors.Is(err, ErrPromptNotAccepted) || errors.Is(err, ErrPendingResponse)
}

func (session *nativeSession) completeTurn(result harness.TaskResult, turnErr error, emitTaskResult bool) {
	session.mutex.Lock()
	if session.turnFinal {
		session.mutex.Unlock()
		return
	}
	session.turnFinal = true
	sequence := session.lastSequence
	session.mutex.Unlock()
	if emitTaskResult {
		if err := session.emit(harness.Event{Kind: harness.EventTaskResult, Sequence: sequence, Payload: mustMarshalTaskResult(result.Semantic)}); err != nil {
			turnErr = err
			reason := protocol.TaskResultReasonUnknownOutcome
			result = harness.TaskResult{Kind: harness.ResultFailed, Summary: err.Error(), Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}
			session.fail(err)
		}
	}
	session.mutex.Lock()
	stored := cloneTaskResult(result)
	session.turnResult = &stored
	session.turnErr = turnErr
	session.mutex.Unlock()
	session.turnOnce.Do(func() { close(session.turnDone) })
}

func (session *nativeSession) noteSequence(sequence uint64) {
	session.mutex.Lock()
	if sequence > session.lastSequence {
		session.lastSequence = sequence
	}
	session.mutex.Unlock()
}

func mustMarshalTaskResult(result *protocol.TaskResult) json.RawMessage {
	if result == nil {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	return encoded
}

func (session *nativeSession) emit(event harness.Event) error {
	session.emitMutex.Lock()
	defer session.emitMutex.Unlock()
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if err := session.sink.Handle(session.context, event); err != nil {
		return fmt.Errorf("persist pi native event: %w", err)
	}
	session.mutex.Lock()
	session.eventCount++
	if event.Sequence > session.lastSequence {
		session.lastSequence = event.Sequence
	}
	session.mutex.Unlock()
	return nil
}

func (session *nativeSession) fail(cause error) {
	if cause == nil {
		return
	}
	session.mutex.Lock()
	if session.failure != nil {
		session.mutex.Unlock()
		return
	}
	session.failure = cause
	started := session.turnStarted
	session.mutex.Unlock()
	if started {
		reason := protocol.TaskResultReasonUnknownOutcome
		session.completeTurn(harness.TaskResult{Kind: harness.ResultFailed, Summary: cause.Error(), Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}, cause, false)
	}
	session.cancel()
}

func (session *nativeSession) usableLocked() error {
	if session.failure != nil {
		return session.failure
	}
	if session.closing {
		return errNativeSessionClosed
	}
	if session.context.Err() != nil {
		return session.sessionErrorLocked()
	}
	if session.process == nil {
		return errors.New("pi native process is unavailable")
	}
	return nil
}

func (session *nativeSession) sessionErrorLocked() error {
	if session.failure != nil {
		return session.failure
	}
	if err := session.context.Err(); err != nil {
		return err
	}
	return errNativeSessionClosed
}

func (session *nativeSession) watchProcess() {
	defer session.watchOnce.Do(func() { close(session.watchDone) })
	session.mutex.Lock()
	process := session.process
	session.mutex.Unlock()
	if process == nil {
		session.fail(errNativeProcessNil)
		session.completeFinal(harness.TaskResult{Kind: harness.ResultFailed, Summary: errNativeProcessNil.Error(), Usage: harness.Usage{State: harness.UsageUnknown}})
		return
	}
	processResult := process.Wait()
	session.outputMutex.Lock()
	trailing, closeErr := session.decoder.Close()
	for _, record := range trailing {
		if err := session.handleRecord(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: record.Sequence, At: time.Now().UTC()}, record); err != nil {
			session.fail(err)
		}
	}
	session.outputMutex.Unlock()
	if closeErr != nil {
		session.fail(closeErr)
	}
	session.mutex.Lock()
	session.processResult = &processResult
	turnFinal := session.turnFinal
	session.mutex.Unlock()
	if turnFinal {
		<-session.turnDone
	}
	session.mutex.Lock()
	turnFinal = session.turnFinal
	turnResult := cloneTaskResultPointer(session.turnResult)
	turnErr := session.turnErr
	failure := session.failure
	normalClose := session.closeNormalCandidate
	cancelApplied := session.cancelApplied
	session.mutex.Unlock()
	if !turnFinal {
		if cancelApplied {
			turnResult = &harness.TaskResult{Kind: harness.ResultCancelled, Summary: "pi abort was accepted before the native process stopped", Usage: harness.Usage{State: harness.UsageUnknown}}
			turnErr = nil
		} else {
			reason := protocol.TaskResultReasonMissingResult
			if !processResult.Success() {
				reason = protocol.TaskResultReasonProcessFailure
			}
			cause := failure
			if cause == nil {
				cause = errors.New("pi process ended before a settled structured task result")
			}
			turnResult = &harness.TaskResult{Kind: harness.ResultFailed, Summary: cause.Error(), Reason: &reason, Usage: harness.Usage{State: harness.UsageUnknown}}
			turnErr = cause
		}
		session.completeTurn(*turnResult, turnErr, false)
	}
	if turnResult == nil {
		turnResult = &harness.TaskResult{Kind: harness.ResultFailed, Summary: "pi turn result is unavailable", Usage: harness.Usage{State: harness.UsageUnknown}}
	}
	final := cloneTaskResult(*turnResult)
	final.Process = processResult
	if !processResult.Success() && !expectedNormalClose(processResult, normalClose) && !expectedCancelledExit(processResult, cancelApplied, final) {
		reason := protocol.TaskResultReasonProcessFailure
		final.Kind = harness.ResultFailed
		final.Reason = &reason
		final.Semantic = nil
		final.Summary = "pi process failed after native turn: " + summarizeProcessFailure(processResult)
	}
	if failure != nil {
		reason := protocol.TaskResultReasonUnknownOutcome
		final.Kind = harness.ResultFailed
		final.Reason = &reason
		final.Semantic = nil
		final.Summary = failure.Error()
	}
	if turnErr != nil && final.Kind == harness.ResultSucceeded {
		reason := protocol.TaskResultReasonUnknownOutcome
		final.Kind = harness.ResultFailed
		final.Reason = &reason
		final.Semantic = nil
		final.Summary = turnErr.Error()
	}
	session.completeFinal(final)
}

func expectedNormalClose(result execution.Result, candidate bool) bool {
	return candidate && result.Terminated && result.SinkError == nil && result.OutputError == nil &&
		result.TerminationError == nil && result.ContainmentError == nil
}

func expectedCancelledExit(result execution.Result, cancelApplied bool, final harness.TaskResult) bool {
	return cancelApplied && final.Kind == harness.ResultCancelled && result.Terminated &&
		result.SinkError == nil && result.OutputError == nil &&
		result.TerminationError == nil && result.ContainmentError == nil
}

func (session *nativeSession) completeFinal(result harness.TaskResult) {
	session.resultOnce.Do(func() {
		stored := cloneTaskResult(result)
		session.mutex.Lock()
		session.result = &stored
		session.mutex.Unlock()
		close(session.resultDone)
	})
}

func summarizeProcessFailure(result execution.Result) string {
	if result.WaitError != nil {
		return result.WaitError.Error()
	}
	if result.SinkError != nil {
		return result.SinkError.Error()
	}
	if result.OutputError != nil {
		return result.OutputError.Error()
	}
	if result.TerminationError != nil {
		return result.TerminationError.Error()
	}
	if result.ContainmentError != nil {
		return result.ContainmentError.Error()
	}
	if result.OutputTruncated {
		return "process output was truncated"
	}
	return fmt.Sprintf("process exited with code %d", result.ExitCode)
}

func cloneTaskResultPointer(result *harness.TaskResult) *harness.TaskResult {
	if result == nil {
		return nil
	}
	cloned := cloneTaskResult(*result)
	return &cloned
}

func cloneTaskResult(result harness.TaskResult) harness.TaskResult {
	cloned := result
	if result.Semantic != nil {
		semantic := *result.Semantic
		semantic.EvidenceRefs = append([]string(nil), semantic.EvidenceRefs...)
		semantic.Diagnostics = append([]protocol.Diagnostic(nil), semantic.Diagnostics...)
		cloned.Semantic = &semantic
	}
	if result.Reason != nil {
		reason := *result.Reason
		cloned.Reason = &reason
	}
	return cloned
}
