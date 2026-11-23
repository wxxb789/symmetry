package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	appServerMethodInitialize    = "initialize"
	appServerMethodInitialized   = "initialized"
	appServerMethodThreadStart   = "thread/start"
	appServerMethodTurnStart     = "turn/start"
	appServerMethodTurnInterrupt = "turn/interrupt"

	appServerEventAgentMessageDelta = "item/agentMessage/delta"
	appServerEventThreadTokenUsage  = "thread/tokenUsage/updated"
	appServerEventTurnCompleted     = "turn/completed"
	appServerEventThreadStarted     = "thread/started"
	appServerEventTurnStarted       = "turn/started"
	appServerEventItemStarted       = "item/started"
	appServerEventItemCompleted     = "item/completed"
	appServerEventError             = "error"

	nativeEngineeringSandbox = "workspace-write"
	nativeApprovalOnRequest  = "on-request"

	nativeInterruptTimeout   = 5 * time.Second
	processCleanupTimeout    = 5 * time.Second
	processTerminationGrace  = 5 * time.Second
	maxPendingTurnEvents     = 128
	maxPendingTurnBytes      = 4 << 20
	maxPreReadyEvents        = 64
	maxPreReadyBytes         = 1 << 20
	maxSeenNotifications     = 4096
	maxSeenNotificationBytes = 1 << 20
	maxRetiredRPCIDs         = 4096
)

var errNativeSessionClosed = errors.New("codex native session is closed")
var errNativeThreadNotOpened = errors.New("codex native session has not opened a thread")

var errPendingTurnEventOverflow = errors.New("Codex pending turn notification buffer overflow")

var errNativeTurnTerminal = errors.New("Codex turn reached a terminal state before the pending RPC completed")
var errNotificationOverflow = errors.New("Codex native notification dedupe window overflow")
var errProcessOutputBeforeReady = errors.New("Codex process output arrived before process readiness")
var errNativeProcessNil = errors.New("Codex native process starter returned a nil process")
var errNativeTurnNotObserved = errors.New("Codex native turn did not reach a matching terminal event")
var errNativeWatcherTimeout = errors.New("Codex native process watcher did not settle after close")

// nativeProcess keeps the test seam at the existing process-runner boundary.
// Production construction below always starts it with execution.Runner.
type nativeProcess = harness.NativeProcess

type processStarter func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error)

func runnerProcessStarter(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (nativeProcess, error) {
	process, err := execution.NewRunner().Start(ctx, invocation, sink)
	// Check the concrete pointer before converting it to nativeProcess. A nil
	// *execution.Process stored in the interface would otherwise look non-nil
	// to callers and lose the launch failure's cleanup boundary.
	if process == nil {
		return nil, err
	}
	return process, err
}

// NewAdapter creates the Codex app-server adapter. Probe intentionally stays
// fail-closed until the installed version has platform-specific live evidence;
// Start is nevertheless a real native transport implementation for the daemon
// to use after its independent admission policy permits it.
func NewAdapter(executables ...string) *Adapter {
	executable := DefaultExecutable
	if len(executables) > 0 && strings.TrimSpace(executables[0]) != "" {
		executable = executables[0]
	}
	return &Adapter{executable: executable, startProcess: runnerProcessStarter}
}

// NewAdapterWithRunner creates an adapter with a deterministic command probe.
// Native process startup still uses the existing execution.Runner.
func NewAdapterWithRunner(executable string, runner CommandRunner) *Adapter {
	if strings.TrimSpace(executable) == "" {
		executable = DefaultExecutable
	}
	return &Adapter{executable: executable, runner: runner, startProcess: runnerProcessStarter}
}

// Start launches only the Codex app-server transport. It deliberately does not
// create a native thread or begin a turn: callers persist the process identity
// before Open, and persist the returned opaque handle before StartTurn.
func (adapter *Adapter) Start(ctx context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	if adapter == nil {
		return nil, errors.New("codex adapter is nil")
	}
	if ctx == nil {
		return nil, errors.New("codex start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ProviderAccess != nil {
		return nil, &harness.CapabilityError{
			Kind:       harness.KindCodex,
			Capability: harness.CapabilityProviderAccess,
			Reason:     "Codex native provider broker bridge is not verified",
		}
	}
	if request.Limits.MaxCostMicrousd != nil {
		return nil, &harness.CapabilityError{
			Kind:       harness.KindCodex,
			Capability: harness.CapabilityHardCostLimit,
			Reason:     "Codex app-server provider-enforced hard cost limits are not verified",
		}
	}
	if strings.TrimSpace(request.Workspace) == "" || !filepath.IsAbs(request.Workspace) {
		return nil, errors.New("codex workspace must be an absolute path")
	}
	if request.Resume != nil {
		return nil, &harness.CapabilityError{
			Kind:       harness.KindCodex,
			Capability: harness.CapabilityResume,
			Reason:     "Codex native resume is not verified",
		}
	}
	if sink == nil {
		return nil, errors.New("codex event sink must not be nil")
	}
	if len(request.Invocation.InitialInput) != 0 || request.Invocation.CloseInputAfterInitial {
		return nil, errors.New("codex native transport does not accept legacy initial input")
	}
	if adapter.startProcess == nil {
		adapter.startProcess = runnerProcessStarter
	}

	processContext, cancel := context.WithCancel(ctx)
	session := newNativeSession(processContext, cancel, sink, request.Workspace)
	launchProcess := request.StartProcess
	if launchProcess == nil {
		launchProcess = adapter.startProcess
	}
	session.launchProcess = launchProcess
	session.configuredNativeModel = strings.TrimSpace(adapter.configuredNativeModel)
	session.configuredNativeProvider = strings.TrimSpace(adapter.configuredNativeProvider)
	launchErr := session.staged.Launch(processContext, request, harness.ProcessDescription{
		Program: adapter.executable,
		Args:    []string{"app-server", "--stdio"},
		Dir:     request.Workspace,
		Env:     append([]string(nil), request.Invocation.Env...),
	})
	session.mutex.Lock()
	installed := !harness.IsNilInterface(session.process)
	session.mutex.Unlock()
	if launchErr != nil && !installed {
		return nil, launchErr
	}
	return session, launchErr
}

type nativeSession struct {
	context       context.Context
	cancel        context.CancelFunc
	process       nativeProcess
	forwarder     harness.ProcessForwarder[nativeProcess]
	staged        *harness.StagedDriver
	launchProcess func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error)
	sink          harness.EventSink
	workspace     string
	framer        *Framer

	mutex                    sync.Mutex
	outputMutex              sync.Mutex
	emitMutex                sync.Mutex
	closed                   bool
	threadID                 string
	sessionStartedSent       bool
	turnID                   string
	configuredNativeModel    string
	configuredNativeProvider string
	nativeModel              string
	nativeModelProvider      string
	turnStarted              bool
	turnStarting             bool
	turnReplaying            bool
	turnReplayDone           chan struct{}
	replayOwnedByWatcher     bool
	terminalSeen             bool
	turnObserved             bool
	turnWaitErr              error
	turnDone                 chan struct{}
	overflowReported         bool
	result                   *harness.TaskResult
	terminalResult           *harness.TaskResult
	processResult            *execution.Result
	resultDone               chan struct{}
	resultOnce               sync.Once
	watchDone                chan struct{}
	watchOnce                sync.Once
	watchStarted             bool
	closeNormalCandidate     bool
	sinkErr                  error
	nativeFailureReason      *protocol.TaskResultReason
	pendingThreadStarts      map[string]json.RawMessage
	seenNotifications        map[string]struct{}
	seenNotificationBytes    int
	preReadyEvents           []execution.Event
	preReadyBytes            int
	processReady             bool
	outputFailure            error
	startFailed              bool
	startError               error
	pendingTurnEvents        []stagedNotification
	pendingTurnBytes         int
	eventCount               uint64
	lastSeq                  uint64
	cancelTracker            cancelTracker
	rpc                      rpcCorrelator
}

func newNativeSession(ctx context.Context, cancel context.CancelFunc, sink harness.EventSink, workspace string) *nativeSession {
	session := &nativeSession{
		context:             ctx,
		cancel:              cancel,
		sink:                sink,
		workspace:           workspace,
		framer:              NewFramer(defaultMaxFrameBytes),
		resultDone:          make(chan struct{}),
		turnDone:            make(chan struct{}),
		watchDone:           make(chan struct{}),
		pendingThreadStarts: make(map[string]json.RawMessage),
		seenNotifications:   make(map[string]struct{}),
		cancelTracker:       newCancelTracker(),
		rpc:                 newRPCCorrelator(),
	}
	session.forwarder = harness.NewProcessForwarder(&session.mutex, &session.process)
	session.staged = harness.NewStagedDriver(session, &session.mutex)
	return session
}

func (session *nativeSession) LaunchTransport(ctx context.Context, invocation execution.Invocation) error {
	process, err := session.launchProcess(ctx, invocation, execution.SinkFunc(session.handleProcessOutput))
	if harness.IsNilInterface(process) {
		if err != nil {
			session.cancel()
			return err
		}
		session.outputMutex.Lock()
		failure := session.failProcessOutputBeforeReady(execution.Event{
			Stream:   execution.Stdout,
			Sequence: atomic.AddUint64(&session.lastSeq, 1),
			At:       time.Now().UTC(),
		}, errNativeProcessNil)
		session.outputMutex.Unlock()
		session.cancel()
		return failure
	}
	if err != nil {
		session.retainStartFailure(process, err)
		return err
	}

	session.outputMutex.Lock()
	session.mutex.Lock()
	session.process = process
	preReadyEvents := append([]execution.Event(nil), session.preReadyEvents...)
	session.preReadyEvents = nil
	session.preReadyBytes = 0
	outputFailure := session.outputFailure
	session.processReady = outputFailure == nil
	session.mutex.Unlock()
	var outputErr error
	if outputFailure == nil {
		for _, queued := range preReadyEvents {
			if err := session.handleProcessOutputLocked(ctx, queued); err != nil {
				outputErr = err
				break
			}
		}
	}
	if outputErr != nil {
		session.retainStartFailureLocked(process, outputErr)
		session.outputMutex.Unlock()
		session.cancel()
		session.launchWatcher()
		return outputErr
	}
	session.outputMutex.Unlock()
	session.mutex.Lock()
	outputFailure = session.outputFailure
	session.mutex.Unlock()
	if outputFailure != nil {
		session.retainStartFailure(process, outputFailure)
		return outputFailure
	}
	session.launchWatcher()
	return nil
}

func (session *nativeSession) ProcessDetails() (int, string) {
	if session == nil {
		return 0, ""
	}
	return session.forwarder.Details()
}

func (session *nativeSession) processDetailsLocked() (int, string) {
	return session.forwarder.DetailsLocked()
}

func (session *nativeSession) LeaseRenewalAvailable() bool {
	if session == nil {
		return false
	}
	return session.forwarder.LeaseRenewalAvailable()
}

func (session *nativeSession) RenewLease(deadline time.Duration, sequence uint64) error {
	if session == nil {
		return errors.ErrUnsupported
	}
	return session.forwarder.RenewLease(deadline, sequence)
}

// Open performs the complete app-server initialization and opens an idle,
// retained engineering thread. It never sends turn/start. The requested
// workspace-write policy is checked against the response; a native runtime
// that downgrades it to read-only is rejected rather than silently accepting a
// non-writing session for an engineering Goal.
func (session *nativeSession) Open(ctx context.Context) (harness.NativeSessionHandle, error) {
	if session == nil {
		return harness.NativeSessionHandle{}, errors.New("codex native session is nil")
	}
	if ctx == nil {
		return harness.NativeSessionHandle{}, errors.New("codex open context must not be nil")
	}
	if err := session.ensureUsable(false); err != nil {
		return harness.NativeSessionHandle{}, err
	}
	handle, err := session.staged.Open(ctx)
	if errors.Is(err, harness.ErrStagedSessionOpening) {
		return harness.NativeSessionHandle{}, errors.New("codex native session is already opening")
	}
	return handle, err
}

func (session *nativeSession) OpenNativeSession(ctx context.Context) (harness.NativeSessionHandle, error) {
	opened := false
	defer func() {
		if opened {
			return
		}
		// A failed native open cannot be retried safely: the process may have
		// accepted thread/start and then returned an incompatible identity or
		// policy. Stop it before the caller can accidentally create a second
		// native thread against the same launch journal entry.
		session.cancel()
	}()

	var initialized initializeResponse
	if err := session.call(ctx, appServerMethodInitialize, initializeParams{
		ClientInfo: clientInfo{Name: "symmetry", Version: "1"},
		Capabilities: &initializeCapabilities{
			OptOutNotificationMethods: []string{},
		},
	}, &initialized); err != nil {
		return harness.NativeSessionHandle{}, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if strings.TrimSpace(initialized.UserAgent) == "" || strings.TrimSpace(initialized.PlatformFamily) == "" || strings.TrimSpace(initialized.PlatformOS) == "" {
		return harness.NativeSessionHandle{}, errors.New("initialize Codex app-server: incomplete response")
	}
	if err := session.notify(ctx, appServerMethodInitialized); err != nil {
		return harness.NativeSessionHandle{}, fmt.Errorf("notify Codex app-server initialized: %w", err)
	}

	var started threadStartResponse
	if err := session.call(ctx, appServerMethodThreadStart, threadStartParams{
		ApprovalPolicy:    nativeApprovalOnRequest,
		ApprovalsReviewer: "user",
		CWD:               session.workspace,
		Ephemeral:         boolPointer(false),
		Model:             session.configuredNativeModel,
		Sandbox:           nativeEngineeringSandbox,
		Config: map[string]any{
			"sandbox_workspace_write.writable_roots":         []string{},
			"sandbox_workspace_write.network_access":         false,
			"sandbox_workspace_write.exclude_tmpdir_env_var": true,
			"sandbox_workspace_write.exclude_slash_tmp":      true,
			// Codex 0.167.2/0.168.0 starts its shell snapshot in a detached session.
			// Symmetry proves containment with the OS process group, so that
			// detached helper makes every Linux close unproven. The daemon
			// already owns the child environment; disabling the snapshot keeps
			// containment provable without widening permissions.
			"features.shell_snapshot": false,
		},
	}, &started); err != nil {
		return harness.NativeSessionHandle{}, fmt.Errorf("start Codex thread: %w", err)
	}
	if err := started.validate(session.workspace); err != nil {
		return harness.NativeSessionHandle{}, fmt.Errorf("validate Codex thread/start response: %w", err)
	}
	if started.Sandbox.Type != "workspaceWrite" {
		return harness.NativeSessionHandle{}, fmt.Errorf("%w: Codex thread/start granted sandbox %q, want workspaceWrite", harness.ErrUnsupportedCapability, started.Sandbox.Type)
	}
	if configured := strings.TrimSpace(session.configuredNativeModel); configured != "" && strings.TrimSpace(started.Model) != configured {
		return harness.NativeSessionHandle{}, fmt.Errorf("%w: Codex thread/start returned model %q, want configured native model %q", harness.ErrUnsupportedCapability, started.Model, configured)
	}
	if configured := strings.TrimSpace(session.configuredNativeProvider); configured != "" && strings.TrimSpace(started.ModelProvider) != configured {
		return harness.NativeSessionHandle{}, fmt.Errorf("%w: Codex thread/start returned provider %q, want configured native provider %q", harness.ErrUnsupportedCapability, started.ModelProvider, configured)
	}

	session.mutex.Lock()
	if session.closed {
		session.mutex.Unlock()
		return harness.NativeSessionHandle{}, errNativeSessionClosed
	}
	if session.sinkErr != nil {
		err := session.sinkErr
		session.mutex.Unlock()
		return harness.NativeSessionHandle{}, err
	}
	if session.terminalSeen || session.context.Err() != nil {
		session.mutex.Unlock()
		return harness.NativeSessionHandle{}, session.sessionError()
	}
	session.threadID = started.Thread.ID
	session.nativeModel = strings.TrimSpace(started.Model)
	session.nativeModelProvider = strings.TrimSpace(started.ModelProvider)
	delete(session.pendingThreadStarts, started.Thread.ID)
	opened = true
	session.mutex.Unlock()
	return harness.NativeSessionHandle{ID: started.Thread.ID}, nil
}

// StartTurn sends the exact v2 turn/start shape. The Goal and canonical Context
// are serialized as separate labelled data sections so context cannot silently
// replace the admission-owned instruction.
func (session *nativeSession) StartTurn(ctx context.Context, request harness.TurnRequest) error {
	if session == nil {
		return errors.New("codex native session is nil")
	}
	if ctx == nil {
		return errors.New("codex start turn context must not be nil")
	}
	if strings.TrimSpace(request.Goal) == "" {
		return errors.New("codex turn goal must not be empty")
	}
	if !validJSONObject(request.Context) {
		return errors.New("codex turn context must be a JSON object")
	}
	if err := session.ensureUsable(false); err != nil {
		return err
	}
	err := session.staged.Admit(ctx, request)
	if errors.Is(err, harness.ErrStagedSessionNotOpened) {
		return errNativeThreadNotOpened
	}
	return err
}

func (session *nativeSession) AdmitNativeTurn(ctx context.Context, request harness.TurnRequest) error {
	outputSchema, err := nativeTaskResultSchema()
	if err != nil {
		return fmt.Errorf("load canonical Codex task-result schema: %w", err)
	}

	session.mutex.Lock()
	if session.threadID == "" {
		session.mutex.Unlock()
		return errNativeThreadNotOpened
	}
	if session.turnStarted {
		session.mutex.Unlock()
		return errors.New("codex native session already owns a turn")
	}
	if session.turnStarting {
		session.mutex.Unlock()
		return errors.New("codex native session is already starting a turn")
	}
	session.turnStarting = true
	threadID := session.threadID
	publishSessionStarted := !session.sessionStartedSent
	if publishSessionStarted {
		session.sessionStartedSent = true
	}
	session.mutex.Unlock()
	startedTurn := false
	defer func() {
		if startedTurn {
			return
		}
		session.mutex.Lock()
		if session.replayOwnedByWatcher {
			session.turnStarting = false
			session.mutex.Unlock()
			return
		}
		session.turnStarting = false
		session.turnReplaying = false
		closeReplayBarrierLocked(session)
		session.pendingTurnEvents = nil
		session.pendingTurnBytes = 0
		session.mutex.Unlock()
	}()
	if publishSessionStarted {
		// Open returns the opaque native ID for the local Goal session journal.
		// It must not be forwarded through the generic event/outbox path.
		if err := session.emit(harness.Event{Kind: harness.EventSessionStarted}); err != nil {
			return err
		}
	}

	var started turnStartResponse
	params := turnStartParams{
		ThreadID: threadID,
		Input: []userInput{{
			Type: "text",
			Text: buildTurnPrompt(request.Goal, request.Context),
		}},
		CWD:          session.workspace,
		OutputSchema: outputSchema,
	}
	if err := session.call(ctx, appServerMethodTurnStart, params, &started); err != nil {
		// After turn/start has been written, a missing response is an unknown
		// native outcome. Do not reuse this session for another start attempt.
		session.cancel()
		return fmt.Errorf("start Codex turn: %w", err)
	}
	if strings.TrimSpace(started.Turn.ID) == "" || started.Turn.Status != "inProgress" {
		return errors.New("start Codex turn: invalid turn response")
	}

	session.mutex.Lock()
	if session.closed {
		session.mutex.Unlock()
		return errNativeSessionClosed
	}
	if session.sinkErr != nil {
		err := session.sinkErr
		session.mutex.Unlock()
		return err
	}
	if session.turnStarted {
		if session.turnID != started.Turn.ID {
			session.mutex.Unlock()
			return errors.New("codex native session received a mismatched replayed turn")
		}
		replayDone := session.turnReplayDone
		watcherOwned := session.replayOwnedByWatcher
		hasResult := session.result != nil
		session.turnStarting = false
		session.mutex.Unlock()
		startedTurn = true
		if hasResult {
			return nil
		}
		if watcherOwned && replayDone != nil {
			return session.waitForTurnReplay(ctx, replayDone)
		}
		return session.drainPendingTurnNotifications(ctx)
	}
	if session.terminalSeen || session.context.Err() != nil {
		session.mutex.Unlock()
		return session.sessionError()
	}
	if session.turnStarted {
		session.mutex.Unlock()
		return errors.New("codex native session already owns a turn")
	}
	session.turnStarting = false
	session.turnStarted = true
	session.turnID = started.Turn.ID
	session.turnReplaying = true
	session.replayOwnedByWatcher = false
	session.turnReplayDone = make(chan struct{})
	session.mutex.Unlock()
	startedTurn = true
	if err := session.drainPendingTurnNotifications(ctx); err != nil {
		return err
	}
	return nil
}

func (session *nativeSession) Control(ctx context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	if session == nil {
		return harness.ControlReceipt{}, errors.New("codex native session is nil")
	}
	if ctx == nil {
		return harness.ControlReceipt{}, errors.New("codex control context must not be nil")
	}
	if request.Kind != harness.ControlCancel {
		return harness.ControlReceipt{
			CommandID:  request.CommandID,
			Kind:       request.Kind,
			Outcome:    harness.ControlUnsupported,
			Capability: capabilityForControl(request.Kind),
			Message:    "Codex native control has not been verified for this operation",
			AppliedAt:  time.Now().UTC(),
		}, nil
	}
	if err := session.ensureUsable(true); err != nil {
		return harness.ControlReceipt{}, err
	}

	session.mutex.Lock()
	threadID, turnID := session.threadID, session.turnID
	session.mutex.Unlock()
	attemptID, owner, done := session.acquireCancelAttempt()
	var interruptErr error
	if owner {
		interruptErr = session.executeCancelAttempt(ctx, attemptID, threadID, turnID)
		if interruptErr != nil {
			if isExplicitCancelRejection(interruptErr) {
				session.rejectCancelAttempt(attemptID)
			} else {
				session.failCancelAttempt(attemptID)
			}
		} else {
			session.acceptCancelAttempt(attemptID)
		}
		session.finishCancelAttempt(attemptID, interruptErr)
	} else {
		interruptErr = session.waitCancelAttempt(ctx, attemptID, done)
	}
	if interruptErr != nil {
		return harness.ControlReceipt{
			CommandID:  request.CommandID,
			Kind:       request.Kind,
			Outcome:    harness.ControlFailed,
			Capability: harness.CapabilityCancel,
			Message:    interruptErr.Error(),
			AppliedAt:  time.Now().UTC(),
		}, interruptErr
	}
	return harness.ControlReceipt{
		CommandID:  request.CommandID,
		Kind:       request.Kind,
		Outcome:    harness.ControlApplied,
		Capability: harness.CapabilityCancel,
		Message:    "Codex turn interruption accepted; awaiting turn/completed",
		AppliedAt:  time.Now().UTC(),
	}, nil
}

// Wait returns an immutable result only after the matching turn/completed event
// and the supervised app-server process have both reached a terminal state. A
// successful turn/start response is not terminal, and process exit without a
// structured result becomes missing_result rather than success.
func (session *nativeSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	if session == nil {
		return harness.TaskResult{}, errors.New("codex native session is nil")
	}
	if ctx == nil {
		return harness.TaskResult{}, errors.New("codex wait context must not be nil")
	}
	return harness.WaitResult(ctx, session.resultDone, session.currentResult, harness.TaskResult{Kind: harness.ResultCancelled, Summary: "Codex wait cancelled"})
}

// WaitTurn waits only until the matching native turn has been observed as
// terminal. It intentionally does not expose the semantic result: the final
// TaskResult remains behind Wait, which is released only after the app-server
// watcher has joined the stopped process.
func (session *nativeSession) WaitTurn(ctx context.Context) error {
	if session == nil {
		return errors.New("codex native session is nil")
	}
	if ctx == nil {
		return errors.New("codex wait-turn context must not be nil")
	}
	session.mutex.Lock()
	turnDone := session.turnDone
	turnWaitErr := session.turnWaitErr
	observed := session.turnObserved
	session.mutex.Unlock()
	if observed {
		return turnWaitErr
	}
	if turnDone == nil {
		return errors.New("codex native turn wait barrier is unavailable")
	}
	select {
	case <-turnDone:
		session.mutex.Lock()
		err := session.turnWaitErr
		session.mutex.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-session.context.Done():
		return session.sessionError()
	}
}

// Close attempts native interruption when a turn is active, then relies on the
// existing process-tree termination as the bounded fallback. Concurrent callers
// share one in-flight close operation. Failed attempts remain retryable, while a
// confirmed successful close stays stable for subsequent callers.
func (session *nativeSession) Close(ctx context.Context) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("codex close context must not be nil")
	}
	return session.staged.Close(ctx)
}

func (session *nativeSession) closeOnce(ctx context.Context) error {
	session.mutex.Lock()
	threadID, turnID := session.threadID, session.turnID
	active := session.turnStarted && !session.terminalSeen && session.result == nil && !session.cancelTracker.requested()
	process := session.process
	watchStarted := session.watchStarted
	// This candidate is deliberately narrower than session.closed: only a close
	// begun after a matching terminal turn and before process exit may exempt the
	// expected process termination marker from semantic success.
	session.closeNormalCandidate = session.terminalSeen && !active && session.processResult == nil
	session.mutex.Unlock()

	var interruptErr error
	if active && strings.TrimSpace(threadID) != "" && strings.TrimSpace(turnID) != "" {
		interruptContext, cancel := context.WithTimeout(ctx, nativeInterruptTimeout)
		attemptID, owner, done := session.acquireCancelAttempt()
		if owner {
			interruptErr = session.executeCancelAttempt(interruptContext, attemptID, threadID, turnID)
			if interruptErr != nil {
				if isExplicitCancelRejection(interruptErr) {
					session.rejectCancelAttempt(attemptID)
				} else {
					session.failCancelAttempt(attemptID)
				}
			} else {
				session.acceptCancelAttempt(attemptID)
			}
			session.finishCancelAttempt(attemptID, interruptErr)
		} else {
			interruptErr = session.waitCancelAttempt(interruptContext, attemptID, done)
		}
		cancel()
	}
	session.mutex.Lock()
	session.closed = true
	session.mutex.Unlock()
	session.cancel()
	if process == nil {
		return interruptErr
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), processCleanupTimeout+processTerminationGrace)
	terminateErr := process.Terminate(cleanupContext, processTerminationGrace)
	cancel()
	var watcherErr error
	if watchStarted && session.watchDone != nil {
		watchContext, watchCancel := context.WithTimeout(context.Background(), processCleanupTimeout)
		select {
		case <-session.watchDone:
		case <-watchContext.Done():
			watcherErr = errNativeWatcherTimeout
		}
		watchCancel()
	}
	if terminateErr == nil && watcherErr == nil {
		// A successful process-tree termination plus a joined watcher proves the
		// native session is stopped even when best-effort interrupt RPC timed out.
		return nil
	}
	return errors.Join(interruptErr, terminateErr, watcherErr)
}

func (session *nativeSession) CloseNativeSession(ctx context.Context) error {
	return session.closeOnce(ctx)
}

func (session *nativeSession) watchProcess() {
	defer session.watchOnce.Do(func() {
		if session.watchDone != nil {
			close(session.watchDone)
		}
	})
	session.mutex.Lock()
	process := session.process
	session.mutex.Unlock()
	if process == nil {
		session.outputMutex.Lock()
		_ = session.failProcessOutputBeforeReady(execution.Event{
			Stream:   execution.Stdout,
			Sequence: atomic.AddUint64(&session.lastSeq, 1),
			At:       time.Now().UTC(),
		}, errProcessOutputBeforeReady)
		session.outputMutex.Unlock()
		session.cancel()
		return
	}
	result := process.Wait()
	session.mutex.Lock()
	processResult := result
	session.processResult = &processResult
	startError := session.startError
	session.mutex.Unlock()
	if startError != nil {
		reason := protocol.TaskResultReasonUnknownOutcome
		failure := harness.TaskResult{
			Kind:         harness.ResultFailed,
			Summary:      startError.Error(),
			Reason:       &reason,
			Process:      result,
			Usage:        harness.Usage{State: harness.UsageUnknown},
			EventCount:   atomic.LoadUint64(&session.eventCount),
			LastSequence: atomic.LoadUint64(&session.lastSeq),
		}
		session.mutex.Lock()
		if session.result == nil {
			session.completeLocked(failure)
		} else {
			stored := harness.CloneTaskResult(*session.result)
			stored.Kind = failure.Kind
			stored.Summary = failure.Summary
			stored.Reason = failure.Reason
			stored.Process = result
			session.result = &stored
		}
		session.mutex.Unlock()
		session.cancel()
		return
	}
	session.outputMutex.Lock()
	frames, closeErr := session.framer.Close()
	for _, frame := range frames {
		if err := session.handleFrame(context.Background(), frame); err != nil {
			break
		}
	}
	if closeErr != nil {
		_ = session.failFraming(execution.Event{
			Stream:   execution.Stdout,
			Sequence: atomic.AddUint64(&session.lastSeq, 1),
			At:       time.Now().UTC(),
		}, closeErr)
	}
	session.outputMutex.Unlock()
	if session.preparePendingTurnReplayAfterProcessExit() {
		_ = session.drainPendingTurnNotifications(context.Background())
	}
	session.failDeferredCancelOnProcessExit()
	session.mutex.Lock()
	replayDone := session.turnReplayDone
	replaying := session.turnReplaying
	session.mutex.Unlock()
	if replaying && replayDone != nil {
		if err := session.waitForTurnReplay(context.Background(), replayDone); err != nil {
			_ = session.failFraming(execution.Event{
				Stream:   execution.Stdout,
				Sequence: atomic.AddUint64(&session.lastSeq, 1),
				At:       time.Now().UTC(),
			}, err)
		}
	}
	session.mutex.Lock()
	if session.result != nil {
		session.mutex.Unlock()
		session.cancel()
		return
	}
	if session.terminalResult != nil {
		terminal := session.finalizeTerminalResultLocked(*session.terminalResult)
		session.terminalResult = nil
		session.completeLocked(terminal)
		session.mutex.Unlock()
		session.cancel()
		return
	}
	resultKind, summary := harness.ResultFailed, "Codex app-server exited without a structured task result (missing_result)"
	reason := protocol.TaskResultReasonMissingResult
	if result.Terminated {
		resultKind, summary = harness.ResultCancelled, "Codex app-server process was terminated"
	}
	session.mutex.Unlock()
	session.complete(harness.TaskResult{
		Kind:         resultKind,
		Summary:      summary,
		Reason:       &reason,
		Process:      result,
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: atomic.LoadUint64(&session.lastSeq),
	})
	session.cancel()
}

func (session *nativeSession) handleProcessOutput(ctx context.Context, event execution.Event) error {
	if session == nil {
		return errors.New("codex native session is nil")
	}
	session.outputMutex.Lock()
	defer session.outputMutex.Unlock()
	return session.handleProcessOutputLocked(ctx, event)
}

func (session *nativeSession) handleProcessOutputLocked(ctx context.Context, event execution.Event) error {
	if event.Stream == execution.Stderr {
		if !session.isProcessReady() {
			return session.queuePreReadyOutput(ctx, event)
		}
		return session.emit(harness.Event{Kind: harness.EventDiagnostic, Stream: string(event.Stream), Sequence: event.Sequence, At: event.At, Diagnostic: true, Code: "native_stderr", Message: "Codex app-server emitted stderr"})
	}
	if !session.isProcessReady() {
		return session.queuePreReadyOutput(ctx, event)
	}
	frames, err := session.framer.Feed(event.Data)
	if err != nil {
		return session.failFraming(event, err)
	}
	for _, frame := range frames {
		if frameErr := session.handleFrame(ctx, frame); frameErr != nil {
			return frameErr
		}
	}
	return nil
}

func (session *nativeSession) launchWatcher() {
	session.mutex.Lock()
	if session.watchStarted {
		session.mutex.Unlock()
		return
	}
	session.watchStarted = true
	session.mutex.Unlock()
	go session.watchProcess()
}

// retainStartFailure binds the process owner before exposing a failed start.
// Cleanup remains session-owned, while all later output is rejected without
// interpreting or acknowledging a partially started native transport.
func (session *nativeSession) retainStartFailure(process nativeProcess, cause error) {
	if session == nil || cause == nil {
		return
	}
	session.outputMutex.Lock()
	session.retainStartFailureLocked(process, cause)
	session.outputMutex.Unlock()
	session.cancel()
	session.launchWatcher()
}

// retainStartFailureLocked is called while outputMutex is held, preventing a
// concurrent reader from processing another frame before failure is visible.
func (session *nativeSession) retainStartFailureLocked(process nativeProcess, cause error) {
	session.mutex.Lock()
	if process != nil {
		session.process = process
	}
	session.processReady = false
	session.startFailed = true
	if session.startError == nil {
		session.startError = cause
	}
	session.preReadyEvents = nil
	session.preReadyBytes = 0
	session.mutex.Unlock()
}

func (session *nativeSession) isProcessReady() bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.processReady
}

func (session *nativeSession) queuePreReadyOutput(ctx context.Context, event execution.Event) error {
	session.mutex.Lock()
	if session.startFailed {
		err := session.sessionErrorLocked()
		if err == nil {
			err = session.outputFailure
		}
		session.mutex.Unlock()
		return err
	}
	if session.processReady {
		session.mutex.Unlock()
		return session.handleProcessOutputLocked(ctx, event)
	}
	if len(session.preReadyEvents) >= maxPreReadyEvents || session.preReadyBytes+len(event.Data) > maxPreReadyBytes {
		session.mutex.Unlock()
		return session.failProcessOutputBeforeReady(event, errProcessOutputBeforeReady)
	}
	queued := execution.Event{
		Stream:   event.Stream,
		Sequence: event.Sequence,
		At:       event.At,
		Data:     append([]byte(nil), event.Data...),
	}
	session.preReadyEvents = append(session.preReadyEvents, queued)
	session.preReadyBytes += len(queued.Data)
	session.mutex.Unlock()
	return nil
}

func (session *nativeSession) failProcessOutputBeforeReady(event execution.Event, cause error) error {
	if cause == nil {
		cause = errProcessOutputBeforeReady
	}
	session.mutex.Lock()
	if session.outputFailure != nil {
		cause = session.outputFailure
	} else {
		session.outputFailure = cause
	}
	session.startFailed = true
	session.terminalSeen = true
	session.preReadyEvents = nil
	session.preReadyBytes = 0
	session.rpc.reset()
	session.cancelTracker.reset()
	session.pendingTurnEvents = nil
	session.pendingTurnBytes = 0
	session.turnReplaying = false
	session.replayOwnedByWatcher = false
	closeReplayBarrierLocked(session)
	reason := protocol.TaskResultReasonUnknownOutcome
	session.completeLocked(harness.TaskResult{
		Kind:         harness.ResultFailed,
		Summary:      cause.Error(),
		Reason:       &reason,
		Process:      session.partialProcessResultLocked(),
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: event.Sequence,
	})
	session.mutex.Unlock()
	session.cancel()
	return cause
}

func (session *nativeSession) failFraming(event execution.Event, cause error) error {
	if cause == nil {
		return nil
	}
	session.mutex.Lock()
	session.terminalSeen = true
	session.rpc.reset()
	session.cancelTracker.reset()
	session.pendingTurnEvents = nil
	session.pendingTurnBytes = 0
	session.turnReplaying = false
	session.replayOwnedByWatcher = false
	closeReplayBarrierLocked(session)
	reason := protocol.TaskResultReasonUnknownOutcome
	failure := harness.TaskResult{
		Kind:         harness.ResultFailed,
		Summary:      "Codex app-server framing failed: " + cause.Error(),
		Reason:       &reason,
		Process:      session.partialProcessResultLocked(),
		Usage:        harness.Usage{State: harness.UsageUnknown},
		EventCount:   atomic.LoadUint64(&session.eventCount),
		LastSequence: event.Sequence,
	}
	session.terminalResult = nil
	if session.result == nil {
		session.completeLocked(failure)
	} else if session.result.Kind == harness.ResultSucceeded {
		stored := harness.CloneTaskResult(failure)
		session.result = &stored
	}
	session.mutex.Unlock()
	session.cancel()
	return cause
}

func (session *nativeSession) ensureUsable(requireTurn bool) error {
	if session == nil {
		return errors.New("codex native session has not started")
	}
	if session.context.Err() != nil {
		return session.sessionError()
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.process == nil {
		return errors.New("codex native session has not started")
	}
	if session.closed {
		return errNativeSessionClosed
	}
	if session.sinkErr != nil {
		return session.sinkErr
	}
	if requireTurn && (!session.staged.OpenedLocked() || !session.turnStarted || session.threadID == "" || session.turnID == "") {
		return errors.New("codex native turn has not started")
	}
	return nil
}

func (session *nativeSession) matches(threadID, turnID string) bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.turnStarted && threadID == session.threadID && turnID == session.turnID
}

func (session *nativeSession) matchesThread(threadID string) bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.staged.OpenedLocked() && threadID != "" && threadID == session.threadID
}

func (session *nativeSession) hasTerminalResult() bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.terminalSeen
}

func (session *nativeSession) markTerminalSeen() bool {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.terminalSeen {
		return false
	}
	session.terminalSeen = true
	return true
}

func (session *nativeSession) emit(event harness.Event) error {
	session.emitMutex.Lock()
	defer session.emitMutex.Unlock()
	return session.emitLocked(event)
}

func (session *nativeSession) emitLocked(event harness.Event) error {
	if err := harness.DeliverEvent(session.context, session.sink, event); err != nil {
		session.mutex.Lock()
		if session.sinkErr == nil {
			session.sinkErr = fmt.Errorf("persist Codex native event: %w", err)
		}
		sinkErr := session.sinkErr
		result := harness.TaskResult{
			Kind:         harness.ResultFailed,
			Summary:      sinkErr.Error(),
			Process:      session.partialProcessResultLocked(),
			Usage:        harness.Usage{State: harness.UsageUnknown},
			EventCount:   atomic.LoadUint64(&session.eventCount),
			LastSequence: event.Sequence,
		}
		session.completeLocked(result)
		session.mutex.Unlock()
		session.cancel()
		return sinkErr
	}
	atomic.AddUint64(&session.eventCount, 1)
	if event.Sequence > atomic.LoadUint64(&session.lastSeq) {
		atomic.StoreUint64(&session.lastSeq, event.Sequence)
	}
	return nil
}

func (session *nativeSession) diagnostic(frame Frame, code, message string) error {
	return session.emit(harness.Event{Kind: harness.EventDiagnostic, Sequence: frame.Sequence, Diagnostic: true, Code: code, Message: message})
}

func (session *nativeSession) complete(result harness.TaskResult) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	session.completeLocked(result)
}

func (session *nativeSession) completeLocked(result harness.TaskResult) {
	if session.result != nil || session.terminalResult != nil {
		return
	}
	if session.processResult != nil {
		result.Process = *session.processResult
	}
	session.resultOnce.Do(func() {
		stored := harness.CloneTaskResult(result)
		session.result = &stored
		close(session.resultDone)
	})
	if !session.turnObserved {
		failure := errNativeTurnNotObserved
		if result.Reason != nil && *result.Reason == protocol.TaskResultReasonMissingResult {
			failure = fmt.Errorf("%w: missing_result", failure)
		}
		if strings.TrimSpace(result.Summary) != "" {
			failure = fmt.Errorf("%w: %s", failure, result.Summary)
		}
		session.signalTurnObservedLocked(failure)
	}
}

func (session *nativeSession) completeTerminal(result harness.TaskResult) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.result != nil || session.terminalResult != nil {
		return
	}
	stored := harness.CloneTaskResult(result)
	session.terminalResult = &stored
	session.signalTurnObservedLocked(nil)
	if session.processResult != nil {
		session.terminalResult = nil
		session.completeLocked(session.finalizeTerminalResultLocked(stored))
	}
}

func (session *nativeSession) finalizeTerminalResultLocked(result harness.TaskResult) harness.TaskResult {
	final := harness.CloneTaskResult(result)
	if session.processResult != nil {
		final.Process = *session.processResult
		if !session.processResult.Success() && !session.expectedNormalCloseLocked(*session.processResult) && final.Kind == harness.ResultSucceeded {
			reason := protocol.TaskResultReasonProcessFailure
			final.Kind = harness.ResultFailed
			final.Reason = &reason
			final.Summary = "Codex app-server process failed after the terminal turn event: " + summarizeProcessFailure(*session.processResult)
		}
	}
	return final
}

func (session *nativeSession) expectedNormalCloseLocked(result execution.Result) bool {
	// Process.Terminate marks the result as Terminated before sending the soft
	// signal. A forceful fallback can therefore legitimately report a non-zero
	// exit code and WaitError (for example exit code -1 after SIGKILL). Only the
	// explicit termination marker is allowed to account for those two fields;
	// transport, output, containment, and termination failures remain fatal.
	return session.closeNormalCandidate && result.Terminated &&
		result.SinkError == nil && result.OutputError == nil &&
		result.TerminationError == nil && result.ContainmentError == nil
}

func (session *nativeSession) signalTurnObserved(err error) {
	session.mutex.Lock()
	session.signalTurnObservedLocked(err)
	session.mutex.Unlock()
}

func (session *nativeSession) signalTurnObservedLocked(err error) {
	if session.turnObserved {
		return
	}
	session.turnObserved = true
	session.turnWaitErr = err
	if session.turnDone != nil {
		close(session.turnDone)
	}
}

func (session *nativeSession) currentResult() (harness.TaskResult, error) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.result == nil {
		return harness.TaskResult{}, errors.New("Codex task result is unavailable")
	}
	return harness.CloneTaskResult(*session.result), nil
}

func (session *nativeSession) partialProcessResult() execution.Result {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.partialProcessResultLocked()
}

func (session *nativeSession) partialProcessResultLocked() execution.Result {
	pid, _ := session.processDetailsLocked()
	return execution.Result{PID: pid}
}

func (session *nativeSession) sessionError() error {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.sessionErrorLocked()
}

func (session *nativeSession) sessionErrorLocked() error {
	if session.startError != nil {
		return session.startError
	}
	if session.outputFailure != nil {
		return session.outputFailure
	}
	if session.sinkErr != nil {
		return session.sinkErr
	}
	if session.closed {
		return errNativeSessionClosed
	}
	return context.Canceled
}

func (session *nativeSession) rememberNativeFailureReason(reason *protocol.TaskResultReason) {
	if reason == nil {
		return
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.nativeFailureReason == nil {
		copied := *reason
		session.nativeFailureReason = &copied
	}
}

func (session *nativeSession) failureReasonOr(fallback protocol.TaskResultReason) *protocol.TaskResultReason {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.nativeFailureReason != nil {
		copied := *session.nativeFailureReason
		return &copied
	}
	return &fallback
}

func (session *nativeSession) resultKindForTurn(status string) harness.ResultKind {
	if status != "interrupted" {
		return resultKindForTurn(status)
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.cancelTracker.requested() {
		return harness.ResultCancelled
	}
	return harness.ResultFailed
}

func boolPointer(value bool) *bool { return &value }
