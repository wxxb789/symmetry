// Package app owns the daemon control loop.
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	minimumInterval        = time.Second
	maximumInterval        = time.Minute
	reconcileRetryMax      = 30 * time.Second
	leaseSafetyMargin      = 5 * time.Second
	retryMaximum           = 30 * time.Second
	terminalGrace          = 8 * time.Minute
	controlRequestLimit    = 15 * time.Second
	nativeUsageRetryLimit  = 5
	nativeUsageRetryWindow = 2 * time.Minute
	maxJSONLRecordBytes    = 256 * 1024
	maxSemanticEventBytes  = 64 * 1024
	rawOutputChunkBytes    = 32 * 1024
	maxPendingOutputBytes  = 2 << 20
	inputWriteTimeout      = 60 * time.Second
	shutdownTerminateLimit = 2 * time.Second
)

var (
	errAssignmentExpired    = errors.New("assignment expired")
	errLeaseDeadlineReached = errors.New("lease renewal deadline reached")
	errOutboxChanged        = errors.New("outbox changed during delivery")
	errInputWriteTimeout    = errors.New("agent did not consume standard input within the write timeout")
	errInvalidAdmission     = errors.New("invalid symmetry.admission.v1")
	errMissingResult        = errors.New("missing_result")
	workspaceFingerprint    = workspace.Fingerprint
)

// taskResultReasonError keeps a canonical terminal reason through local
// preflight failures without asking string matching to recover domain state.
type taskResultReasonError struct {
	reason protocol.TaskResultReason
	cause  error
}

func (err *taskResultReasonError) Error() string { return err.cause.Error() }
func (err *taskResultReasonError) Unwrap() error { return err.cause }

func taskResultFailure(reason protocol.TaskResultReason, cause error) error {
	if cause == nil {
		cause = errors.New(string(reason))
	}
	return &taskResultReasonError{reason: reason, cause: cause}
}

func taskResultFailureReason(cause error) (protocol.TaskResultReason, bool) {
	var typed *taskResultReasonError
	if errors.As(cause, &typed) {
		return typed.reason, true
	}
	return "", false
}

// restartOutboxRecoveryContextKey suppresses ordinary ownership-loss cleanup
// until restart recovery has converted the journal into a terminal fallback.
type restartOutboxRecoveryContextKey struct{}

// ControlAPI is the authenticated protocol boundary used by the loop.
type ControlAPI interface {
	RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error)
	Heartbeat(context.Context, string, protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error)
	Dispatch(context.Context, string, int64) (protocol.RuntimeSnapshot, error)
	Claim(context.Context, string, protocol.ClaimRequest) (protocol.ClaimResponse, error)
	RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error)
	AppendEvents(context.Context, string, protocol.AppendEventsRequest) error
	Transition(context.Context, string, protocol.StateTransitionRequest) error
	Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error)
	AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error
}

// goalControlAPI is deliberately a narrow optional extension to protocol v1.
// A non-Goal daemon remains compatible with an ordinary ControlAPI.
type goalControlAPI interface {
	AttachHarnessSession(context.Context, string, control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error)
	FetchRunContext(context.Context, string, protocol.Fence) (control.GoalRunContext, error)
	AppendEvidence(context.Context, string, protocol.Fence, protocol.Evidence) (control.GoalEvidenceReceipt, error)
	RecordUsage(context.Context, string, protocol.Fence, protocol.Usage) (control.GoalUsageReceipt, error)
}

// goalSubjectWorkspace is the immutable artifact boundary required only by
// Goal execution. Legacy work intentionally retains workspace.Service's
// configured-ref behavior.
type goalSubjectWorkspace interface {
	PrepareSubject(context.Context, string, workspace.RunRef, protocol.Subject) (workspace.SubjectWorkspace, error)
	DeriveSubject(context.Context, workspace.Prepared, string) (protocol.Subject, error)
}

// EnrollmentAPI is deliberately separate because it is authorized by the
// one-time enrollment token rather than a persisted machine credential.
type EnrollmentAPI interface {
	Enroll(context.Context, string, string, protocol.EnrollRequest) (protocol.EnrollResponse, error)
}

// Process is the part of a running agent the control loop needs.
type Process interface {
	WriteInput([]byte) error
	Terminate(context.Context, time.Duration) error
	Wait() execution.Result
	ProcessDetails() (int, string)
}

// StartProcess permits deterministic runner tests without exposing os/exec to
// the control loop's callers.
type StartProcess func(context.Context, execution.Invocation, execution.Sink) (Process, error)

// NotificationClient is the durable-notification wakeup boundary.
type NotificationClient interface {
	Run(context.Context, chan<- notification.Hint) error
}

// Options changes dependencies owned by Run. It is primarily intended for
// tests and embedded use; production callers need no options.
type Options func(*options)

type deadlineTimer interface {
	Chan() <-chan time.Time
	Stop()
}

type systemTimer struct{ timer *time.Timer }

func (timer systemTimer) Chan() <-chan time.Time { return timer.timer.C }
func (timer systemTimer) Stop()                  { timer.timer.Stop() }

type options struct {
	httpClient                                 *http.Client
	store                                      *state.Store
	control                                    ControlAPI
	enrollment                                 EnrollmentAPI
	workspace                                  workspace.Service
	start                                      StartProcess
	notifications                              NotificationClient
	logWriter                                  io.Writer
	clock                                      func() time.Time
	newTimer                                   func(time.Duration) deadlineTimer
	newID                                      func() (string, error)
	newMachineToken                            func() (string, error)
	terminatePersist                           func(pid int, identity string) error
	recordWorkspace                            func(state.RunKey, string) (state.RunJournal, error)
	recordProcess                              func(state.RunKey, int, string, time.Time) (state.RunJournal, error)
	queueTerminalTransition                    func(state.RunKey, protocol.StateTransitionRequest, time.Time) (state.RunJournal, error)
	queueGoalUsage                             func(state.RunKey, protocol.Usage) (state.RunJournal, error)
	markGoalSessionAttachDeliveryReady         func(state.RunKey, string) (state.RunJournal, error)
	discardUnreadyGoalSessionAttachDelivery    func(state.RunKey, string) (state.RunJournal, error)
	markCommandAcknowledgementsDelivered       func(state.RunKey, []string) (state.RunJournal, error)
	queueCancelledTransitionAndAcknowledgement func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
	retainWorkspace                            func(state.RunKey) (state.RunJournal, error)
}

// WithHTTPClient replaces the HTTP transport used for production clients.
func WithHTTPClient(client *http.Client) Options {
	return func(value *options) { value.httpClient = client }
}

// WithStore supplies an already-open state store. Run does not close it.
func WithStore(store *state.Store) Options { return func(value *options) { value.store = store } }

// WithControl supplies the authenticated control-plane client.
func WithControl(client ControlAPI) Options { return func(value *options) { value.control = client } }

// WithEnrollment supplies the enrollment client.
func WithEnrollment(client EnrollmentAPI) Options {
	return func(value *options) { value.enrollment = client }
}

// WithWorkspace supplies the local workspace service.
func WithWorkspace(service workspace.Service) Options {
	return func(value *options) { value.workspace = service }
}

// WithStartProcess supplies the local process launcher.
func WithStartProcess(start StartProcess) Options {
	return func(value *options) { value.start = start }
}

// WithNotificationClient supplies a notification source, normally for tests.
func WithNotificationClient(client NotificationClient) Options {
	return func(value *options) { value.notifications = client }
}

// WithLogWriter sends structured JSON logs to writer. The default is stderr.
func WithLogWriter(writer io.Writer) Options {
	return func(value *options) { value.logWriter = writer }
}

// Run enrolls or restores this machine identity, registers its one configured
// runtime, and keeps polling until ctx is cancelled. A failed request affects
// only that request: the daemon remains alive and retries on its next wakeup.
func Run(ctx context.Context, value config.Config, changes ...Options) error {
	if ctx == nil {
		return errors.New("daemon context must not be nil")
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	settings := options{
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		logWriter:        os.Stderr,
		clock:            func() time.Time { return time.Now().UTC() },
		newTimer:         func(delay time.Duration) deadlineTimer { return systemTimer{timer: time.NewTimer(delay)} },
		newID:            state.NewDaemonInstanceID,
		newMachineToken:  state.NewMachineToken,
		terminatePersist: terminatePersistedProcess,
	}
	for _, change := range changes {
		if change != nil {
			change(&settings)
		}
	}
	if settings.logWriter == nil {
		settings.logWriter = io.Discard
	}
	loop := &daemon{
		config: value, options: settings,
		log:     slog.New(slog.NewJSONHandler(settings.logWriter, &slog.HandlerOptions{Level: slog.LevelInfo})),
		running: make(map[state.RunKey]*runningRun),
	}
	return loop.run(ctx)
}

type daemon struct {
	config  config.Config
	options options
	log     *slog.Logger

	store               *state.Store
	control             ControlAPI
	workspace           workspace.Service
	start               StartProcess
	harnessRegistry     *harness.Registry
	harnessCapabilities harness.Capabilities
	runtimeID           string
	runtimeEpoch        int64
	leaseDuration       time.Duration
	pollEvery           time.Duration
	heartbeatEvery      time.Duration
	running             map[state.RunKey]*runningRun
	slots               chan struct{}
	mu                  sync.Mutex
	commandReceiptMu    sync.Mutex
	workers             sync.WaitGroup
	background          context.Context
	backgroundCancel    context.CancelFunc
	backgroundStop      bool
	backgroundWG        sync.WaitGroup
	terminalReleaseWG   sync.WaitGroup
	commandWG           sync.WaitGroup
	snapshotWG          sync.WaitGroup
	outboxWake          chan struct{}
	outboxRetry         map[state.RunKey]outboxRetry
	cleanupWake         chan struct{}
	cleanupQueued       map[state.RunKey]struct{}
	cleanupRetry        map[state.RunKey]time.Time
	retainedWorkspaces  map[state.RunKey]struct{}
	clockSkewWarned     bool
	commandWake         chan struct{}
	commandQueue        []*queuedCommand
	queuedCommands      map[commandKey]*queuedCommand
	commandLanes        map[state.RunKey]*commandLane
	commandRequests     map[uint64]map[commandKey]struct{}
	nextCommandRequest  uint64
}

type outboxRetry struct {
	fingerprint string
	retryAt     time.Time
	delay       time.Duration
	permanent   bool
}

type outboxFailure struct {
	journal state.RunJournal
	err     error
}

type goalDeliveryRejectedError struct{ err error }

func (err *goalDeliveryRejectedError) Error() string { return err.err.Error() }
func (err *goalDeliveryRejectedError) Unwrap() error { return err.err }

func (failure *outboxFailure) Error() string { return failure.err.Error() }
func (failure *outboxFailure) Unwrap() error { return failure.err }

type queuedCommand struct {
	command   protocol.Command
	key       commandKey
	done      chan struct{}
	completed bool
	delivered bool
}

type commandKey struct {
	run state.RunKey
	id  string
}

type commandCompletion struct {
	run  state.RunKey
	done <-chan struct{}
}

type commandLane struct {
	pending []*queuedCommand
	running bool
}

type runningRun struct {
	// inputMu serializes stdin delivery with input-related lifecycle mutations.
	// Never wait for it while holding daemon.mu.
	inputMu                  sync.Mutex
	process                  Process
	nativeSession            harness.Session
	goalSession              *state.GoalSessionKey
	goalAdmission            *protocol.Admission
	goalUsage                *harness.Usage
	goalUsageAt              time.Time
	goalCachedInputTokens    int64
	goalUsageHasCached       bool
	goalUsageObservation     *state.NativeUsageObservation
	nativeDeadline           time.Time
	prepared                 workspace.Prepared
	output                   *agentOutput
	starting                 bool
	claimed                  bool
	cancel                   context.CancelFunc
	cancelled                bool
	cancelCommandID          string
	stale                    bool
	terminal                 bool
	terminalizing            int
	slotHeld                 bool
	cleanupBlocked           bool
	startFailure             error
	nativeCloseRetryRequired bool
	nativeCloseRetrying      bool
	// Native completion is retained locally until accounting is durably queued.
	nativeFinalResult            *harness.TaskResult
	nativeFinalWaitErr           error
	nativeFinalCloseErr          error
	nativeFinalAdmission         *protocol.Admission
	nativeFinalUsage             *protocol.Usage
	nativeUsageRetryAt           time.Time
	nativeUsageRetryDeadline     time.Time
	nativeUsageRetryAttempts     int
	nativeUsageRetryPending      bool
	nativeUsageRetryInFlight     bool
	nativeUsageRetryNeedsPrepare bool
	nativeUsageRenewalBlocked    bool
	nativeUsageRetryExhausted    bool
	nativeUsageFinalized         bool
	outputDropWarned             bool
	stopExecution                context.CancelCauseFunc
	renewCancel                  context.CancelFunc
	renewCancelID                uint64
	terminalCancel               context.CancelFunc
}

func (daemon *daemon) run(ctx context.Context) error {
	if err := daemon.initialize(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	defer daemon.close()
	done := daemon.startBackground(ctx)
	defer func() {
		done()
	}()
	var reconcileRetry deadlineTimer
	var reconcileRetryChan <-chan time.Time
	reconcileBackoff := minimumInterval
	stopReconcileRetry := func() {
		if reconcileRetry != nil {
			reconcileRetry.Stop()
			reconcileRetry = nil
			reconcileRetryChan = nil
		}
		reconcileBackoff = minimumInterval
	}
	armReconcileRetry := func() {
		if reconcileRetry != nil {
			reconcileRetry.Stop()
		}
		reconcileRetry = daemon.timer(reconcileBackoff)
		reconcileRetryChan = reconcileRetry.Chan()
		if reconcileBackoff < reconcileRetryMax {
			reconcileBackoff *= 2
			if reconcileBackoff > reconcileRetryMax {
				reconcileBackoff = reconcileRetryMax
			}
		}
	}
	defer stopReconcileRetry()

	// Registration is complete before liveness starts, but reconciliation can
	// block on the control plane. Keep lease maintenance alive while it does.
	if !daemon.reconcile(ctx) {
		armReconcileRetry()
	}

	triggers := make(chan struct{}, 1)
	trigger := func() {
		select {
		case triggers <- struct{}{}:
		default:
		}
	}
	trigger()
	hints := make(chan notification.Hint, 16)
	if daemon.options.notifications != nil {
		go func() {
			if err := daemon.options.notifications.Run(ctx, hints); err != nil && !errors.Is(err, context.Canceled) {
				daemon.log.Warn("notification_client_stopped", "error", err)
			}
		}()
	}

	poll := time.NewTicker(daemon.interval(daemon.pollEvery))
	heartbeat := time.NewTicker(daemon.interval(daemon.heartbeatEvery))
	defer poll.Stop()
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			daemon.stopBackground()
			daemon.stopAll()
			done()
			daemon.workers.Wait()
			return nil
		case hint := <-hints:
			if hint.Type == "connected" {
				if daemon.reconcile(ctx) {
					stopReconcileRetry()
				} else {
					armReconcileRetry()
				}
			}
			trigger()
		case <-reconcileRetryChan:
			reconcileRetry = nil
			reconcileRetryChan = nil
			if daemon.reconcile(ctx) {
				reconcileBackoff = minimumInterval
			} else {
				armReconcileRetry()
			}
		case <-triggers:
			daemon.sync(ctx)
		case <-poll.C:
			daemon.sync(ctx)
		case <-heartbeat.C:
			daemon.heartbeat(ctx)
		}
	}
}

// startBackground separates deadline-sensitive lease work from the reactor.
// Its returned function is safe to call more than once and joins all workers.
func (daemon *daemon) startBackground(ctx context.Context) func() {
	background, cancel := context.WithCancel(ctx)
	daemon.mu.Lock()
	daemon.background = background
	daemon.backgroundCancel = cancel
	daemon.backgroundStop = false
	daemon.outboxWake = make(chan struct{}, 1)
	daemon.outboxRetry = make(map[state.RunKey]outboxRetry)
	daemon.cleanupWake = make(chan struct{}, 1)
	daemon.cleanupQueued = make(map[state.RunKey]struct{})
	daemon.cleanupRetry = make(map[state.RunKey]time.Time)
	daemon.commandWake = make(chan struct{}, 1)
	daemon.queuedCommands = make(map[commandKey]*queuedCommand)
	daemon.commandRequests = make(map[uint64]map[commandKey]struct{})
	daemon.mu.Unlock()

	livenessStarted := make(chan struct{})
	daemon.enqueueRecoveredCleanups()
	daemon.backgroundWG.Add(4)
	go func() {
		defer daemon.backgroundWG.Done()
		close(livenessStarted)
		daemon.runLiveness(background)
	}()
	go func() {
		defer daemon.backgroundWG.Done()
		daemon.runOutbox(background)
	}()
	go func() {
		defer daemon.backgroundWG.Done()
		daemon.runCleanup(background)
	}()
	go func() {
		defer daemon.backgroundWG.Done()
		daemon.runCommandExecutor(background)
	}()
	<-livenessStarted

	var once sync.Once
	return func() {
		once.Do(func() {
			daemon.stopBackground()
			daemon.backgroundWG.Wait()
			daemon.commandWG.Wait()
			daemon.snapshotWG.Wait()
			daemon.terminalReleaseWG.Wait()
		})
	}
}

func (daemon *daemon) stopBackground() {
	daemon.mu.Lock()
	daemon.backgroundStop = true
	cancel := daemon.backgroundCancel
	daemon.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (daemon *daemon) runLiveness(ctx context.Context) {
	for {
		daemon.renewLeases(ctx)
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) runOutbox(ctx context.Context) {
	for {
		daemon.flushAll(ctx)
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-daemon.outboxSignal():
			timer.Stop()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) outboxSignal() <-chan struct{} {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.outboxWake == nil {
		return nil
	}
	return daemon.outboxWake
}

func (daemon *daemon) signalOutbox() {
	daemon.mu.Lock()
	wake := daemon.outboxWake
	daemon.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (daemon *daemon) signalOutboxFor(key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	starting := active != nil && active.starting
	daemon.mu.Unlock()
	if !starting {
		daemon.signalOutbox()
	}
}

func (daemon *daemon) enqueueCommand(command protocol.Command) <-chan struct{} {
	daemon.mu.Lock()
	done, wake := daemon.enqueueCommandLocked(command)
	daemon.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return done
}

// enqueueCommandLocked must be called with daemon.mu held.
func (daemon *daemon) enqueueCommandLocked(command protocol.Command) (<-chan struct{}, chan<- struct{}) {
	if command.CommandID == "" {
		return nil, nil
	}
	key := commandKey{run: state.RunKey{RunID: command.RunID, Generation: command.Generation}, id: command.CommandID}
	if daemon.commandWake == nil || daemon.backgroundStop {
		return nil, nil
	}
	if daemon.queuedCommands == nil {
		daemon.queuedCommands = make(map[commandKey]*queuedCommand)
	}
	if queued, exists := daemon.queuedCommands[key]; exists {
		return queued.done, nil
	}
	queued := &queuedCommand{command: command, key: key, done: make(chan struct{})}
	daemon.queuedCommands[key] = queued
	daemon.commandQueue = append(daemon.commandQueue, queued)
	return queued.done, daemon.commandWake
}

func (daemon *daemon) runCommandExecutor(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		queued, ok := daemon.nextQueuedCommand()
		if ok {
			daemon.dispatchQueuedCommand(ctx, queued)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-daemon.commandSignal():
		}
	}
}

func (daemon *daemon) dispatchQueuedCommand(ctx context.Context, queued *queuedCommand) {
	key := state.RunKey{RunID: queued.command.RunID, Generation: queued.command.Generation}
	daemon.mu.Lock()
	if daemon.queuedCommands[queued.key] != queued {
		daemon.mu.Unlock()
		return
	}
	if daemon.backgroundStop || ctx.Err() != nil {
		daemon.mu.Unlock()
		daemon.finishQueuedCommand(queued, false)
		return
	}
	if queued.command.Kind == "cancel" {
		if active := daemon.running[key]; active != nil {
			active.cancelled = true
			if !active.claimed {
				active.cancelCommandID = queued.command.CommandID
				daemon.mu.Unlock()
				return
			}
		}
		daemon.commandWG.Add(1)
		daemon.mu.Unlock()
		go func() {
			defer daemon.commandWG.Done()
			daemon.executeQueuedCommand(ctx, queued)
		}()
		return
	}
	if daemon.commandLanes == nil {
		daemon.commandLanes = make(map[state.RunKey]*commandLane)
	}
	lane := daemon.commandLanes[key]
	if lane == nil {
		lane = &commandLane{}
		daemon.commandLanes[key] = lane
	}
	lane.pending = append(lane.pending, queued)
	if lane.running {
		daemon.mu.Unlock()
		return
	}
	lane.running = true
	daemon.commandWG.Add(1)
	daemon.mu.Unlock()
	go daemon.runCommandLane(ctx, key)
}

func (daemon *daemon) runCommandLane(ctx context.Context, key state.RunKey) {
	defer daemon.commandWG.Done()
	for {
		queued, ok := daemon.nextLaneCommand(key)
		if !ok {
			return
		}
		daemon.executeQueuedCommand(ctx, queued)
	}
}

func (daemon *daemon) nextLaneCommand(key state.RunKey) (*queuedCommand, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	lane := daemon.commandLanes[key]
	if lane == nil || len(lane.pending) == 0 {
		delete(daemon.commandLanes, key)
		return nil, false
	}
	queued := lane.pending[0]
	lane.pending[0] = nil
	lane.pending = lane.pending[1:]
	return queued, true
}

func (daemon *daemon) executeQueuedCommand(ctx context.Context, queued *queuedCommand) {
	daemon.mu.Lock()
	current := daemon.queuedCommands[queued.key] == queued
	daemon.mu.Unlock()
	if !current {
		return
	}
	if ctx.Err() != nil {
		daemon.finishQueuedCommand(queued, false)
		return
	}
	receipt := daemon.handleCommand(ctx, queued.command)
	daemon.finishQueuedCommand(queued, receipt)
}

func (daemon *daemon) nextQueuedCommand() (*queuedCommand, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if len(daemon.commandQueue) == 0 {
		return nil, false
	}
	command := daemon.commandQueue[0]
	daemon.commandQueue[0] = nil
	daemon.commandQueue = daemon.commandQueue[1:]
	return command, true
}

func (daemon *daemon) finishQueuedCommand(queued *queuedCommand, receipt bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	daemon.finishQueuedCommandLocked(queued, receipt)
}

func (daemon *daemon) finishDeferredCommand(key commandKey, receipt bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if queued := daemon.queuedCommands[key]; queued != nil {
		daemon.finishQueuedCommandLocked(queued, receipt)
	}
}

func (daemon *daemon) finishQueuedCommandLocked(queued *queuedCommand, receipt bool) {
	if daemon.queuedCommands[queued.key] == queued {
		if !queued.completed {
			close(queued.done)
		}
		queued.completed = true
		if !receipt || queued.delivered {
			delete(daemon.queuedCommands, queued.key)
		}
	}
}

func (daemon *daemon) commandSignal() <-chan struct{} {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.commandWake
}

func (daemon *daemon) ensureHarnessProbe(ctx context.Context) error {
	if daemon.harnessRegistry != nil && daemon.harnessCapabilities.Kind != "" {
		return nil
	}
	runtime, err := normalizeRuntimeForProbe(daemon.config.Runtime)
	if err != nil {
		return err
	}
	registry := daemon.harnessRegistry
	if registry == nil {
		registry = newHarnessRegistry(daemon.config.AgentProfiles[runtime.AgentProfile])
	}
	kind, err := configuredHarnessKind(runtime.HarnessKind)
	if err != nil {
		return err
	}
	capabilities, probeErr := registry.Probe(ctx, kind)
	unverified := errors.Is(probeErr, harness.ErrNativeUnverified)
	unavailable := errors.Is(probeErr, harness.ErrHarnessUnavailable) && unavailableRuntimeKind(runtime.HarnessKind)
	if probeErr != nil && !unverified && !unavailable {
		return fmt.Errorf("probe %s adapter: %w", runtime.HarnessKind, probeErr)
	}
	if probeErr == nil && !capabilities.Verified {
		return fmt.Errorf("probe %s adapter: %w", runtime.HarnessKind, harness.ErrNativeUnverified)
	}
	if !unavailable && runtime.HarnessVersion != "legacy" && runtime.HarnessVersion != capabilities.NativeVersion {
		return fmt.Errorf("runtime.harness_version %q does not match probed %q", runtime.HarnessVersion, capabilities.NativeVersion)
	}
	if !unverified && !unavailable && runtime.AdapterVersion != "legacy" && runtime.AdapterVersion != capabilities.ImplementationVersion {
		return fmt.Errorf("runtime.adapter_version %q does not match probed %q", runtime.AdapterVersion, capabilities.ImplementationVersion)
	}
	if runtime.AdapterProtocolVersion != capabilities.ProtocolVersion {
		return fmt.Errorf("runtime.adapter_protocol_version %d does not match probed %d", runtime.AdapterProtocolVersion, capabilities.ProtocolVersion)
	}
	daemon.config.Runtime = runtime
	daemon.harnessRegistry = registry
	daemon.harnessCapabilities = capabilities
	return nil
}

// newHarnessRegistry is the daemon composition root. Native adapter packages
// depend on harness interfaces, so they are wired here rather than from the
// harness package itself.
func newHarnessRegistry(profile config.AgentProfile) *harness.Registry {
	registry := harness.NewRegistry()
	_ = registry.Register(harness.KindCodex, codex.NewAdapterWithNativeIdentity(profile.Command, profile.NativeModel, profile.NativeModelProvider))
	_ = registry.Register(harness.KindClaude, harness.NewClaudeAdapterWithExecutable(profile.Command))
	return registry
}

func normalizeRuntimeForProbe(runtime config.Runtime) (config.Runtime, error) {
	if runtime.HarnessKind == "" && runtime.HarnessVersion == "" && runtime.AdapterVersion == "" && runtime.AdapterProtocolVersion == 0 {
		runtime.HarnessKind = config.RuntimeHarnessGeneric
		runtime.HarnessVersion = "legacy"
		runtime.AdapterVersion = "legacy"
		runtime.AdapterProtocolVersion = 1
	}
	if runtime.HarnessKind == "" || runtime.HarnessVersion == "" || runtime.AdapterVersion == "" || runtime.AdapterProtocolVersion <= 0 {
		return config.Runtime{}, errors.New("runtime adapter metadata must include harness_kind, harness_version, adapter_version, and adapter_protocol_version")
	}
	return runtime, nil
}

func configuredHarnessKind(value string) (harness.Kind, error) {
	switch value {
	case config.RuntimeHarnessGeneric:
		return harness.KindGeneric, nil
	case config.RuntimeHarnessCodex:
		return harness.KindCodex, nil
	case config.RuntimeHarnessClaudeCode:
		return harness.KindClaude, nil
	case config.RuntimeHarnessPi:
		return harness.KindPi, nil
	case config.RuntimeHarnessOpenCode:
		return harness.KindOpenCode, nil
	default:
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
}

func unavailableRuntimeKind(value string) bool {
	switch value {
	case config.RuntimeHarnessClaudeCode, config.RuntimeHarnessPi, config.RuntimeHarnessOpenCode:
		return true
	default:
		return false
	}
}

type runtimeRegistrationMetadata struct {
	HarnessKind            string
	HarnessVersion         string
	AdapterVersion         string
	AdapterProtocolVersion int
	Adapter                protocol.Adapter
}

func buildRuntimeRegistration(runtime config.Runtime, profile config.AgentProfile, capabilities harness.Capabilities) (protocol.RuntimeRegistration, runtimeRegistrationMetadata, error) {
	if err := capabilities.Validate(); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf("validate adapter capabilities: %w", err)
	}
	configuredKind, err := configuredHarnessKind(runtime.HarnessKind)
	if err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, err
	}
	if configuredKind != capabilities.Kind {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf(
			"runtime harness kind %q does not match probed adapter kind %q",
			runtime.HarnessKind,
			capabilities.Kind,
		)
	}
	registration := protocol.RuntimeRegistration{
		RuntimeKey:           runtime.RuntimeKey,
		Name:                 runtime.Name,
		Capacity:             runtime.Capacity,
		AgentProfile:         runtime.AgentProfile,
		Workspace:            runtime.Workspace,
		RepositoryResourceID: optionalRuntimeRepositoryResourceID(runtime.RepositoryResourceID),
		Capabilities: protocol.RuntimeCapabilities{
			StructuredInput:    structuredInputForProfile(profile),
			ProviderAccess:     legacyProviderAccessForRegistration(runtime, profile),
			Interactive:        profile.Interactive,
			SupervisoryControl: profile.SupervisoryControl,
		},
	}
	metadata := runtimeRegistrationMetadata{
		HarnessKind:            runtime.HarnessKind,
		HarnessVersion:         runtime.HarnessVersion,
		AdapterVersion:         runtime.AdapterVersion,
		AdapterProtocolVersion: runtime.AdapterProtocolVersion,
		Adapter: protocol.Adapter{
			Kind:                  runtime.HarnessKind,
			NativeVersion:         runtime.HarnessVersion,
			ImplementationVersion: runtime.AdapterVersion,
			ProtocolVersion:       int64(runtime.AdapterProtocolVersion),
			Operations: protocol.AdapterOperations{
				Start:            capabilities.Start,
				Events:           capabilities.Events,
				Cancel:           capabilities.Cancel,
				Resume:           capabilities.Resume,
				Guidance:         protocol.GuidanceCapability(capabilities.Guidance),
				Pause:            protocol.PauseCapability(capabilities.Pause),
				ApprovalResponse: capabilities.ApprovalResponse,
				Usage:            protocol.UsageCapability(capabilities.Usage),
				HardCostLimit:    capabilities.HardCostLimit,
			},
		},
	}
	if runtime.HarnessKind != config.RuntimeHarnessGeneric && capabilities.NativeVersion != "" {
		metadata.HarnessVersion = capabilities.NativeVersion
	}
	if runtime.HarnessKind != config.RuntimeHarnessGeneric && capabilities.ImplementationVersion != "" {
		metadata.AdapterVersion = capabilities.ImplementationVersion
	}
	metadata.Adapter.NativeVersion = metadata.HarnessVersion
	metadata.Adapter.ImplementationVersion = metadata.AdapterVersion
	if err := metadata.Adapter.Validate(); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf("validate adapter registration: %w", err)
	}
	if err := writeRuntimeRegistrationMetadata(&registration, metadata); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, err
	}
	return registration, metadata, nil
}

func optionalRuntimeRepositoryResourceID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func structuredInputForProfile(profile config.AgentProfile) bool {
	return profile.InputMode == config.InputModeJSON
}

// legacyProviderAccessForRegistration describes the existing stdin envelope
// contract only. Native harnesses must not advertise provider access until
// their broker bridge is independently verified.
func legacyProviderAccessForRegistration(runtime config.Runtime, profile config.AgentProfile) bool {
	return runtime.HarnessKind == config.RuntimeHarnessGeneric &&
		profile.InputMode == config.InputModeJSON && profile.ProviderAccess
}

func writeRuntimeRegistrationMetadata(registration *protocol.RuntimeRegistration, metadata runtimeRegistrationMetadata) error {
	if registration == nil {
		return errors.New("runtime registration must not be nil")
	}
	registration.HarnessKind = metadata.HarnessKind
	registration.HarnessVersion = metadata.HarnessVersion
	registration.AdapterVersion = metadata.AdapterVersion
	registration.AdapterProtocolVersion = metadata.AdapterProtocolVersion
	registration.Capabilities.Adapter = &metadata.Adapter
	return nil
}

func (daemon *daemon) initialize(ctx context.Context) error {
	if err := daemon.ensureHarnessProbe(ctx); err != nil {
		return fmt.Errorf("probe runtime harness: %w", err)
	}
	if _, err := platform.ProcessIdentity(os.Getpid()); err != nil {
		return fmt.Errorf("platform does not support restart-safe process identity: %w", err)
	}
	if daemon.options.store != nil {
		daemon.store = daemon.options.store
	} else {
		store, err := state.New(daemon.config.StateDir)
		if err != nil {
			return err
		}
		daemon.store = store
	}
	identity, err := daemon.store.LoadIdentity()
	if err != nil {
		if !state.IsNotFound(err) {
			return fmt.Errorf("load machine identity: %w", err)
		}
		intent, intentErr := daemon.loadOrCreateEnrollmentIntent()
		if intentErr != nil {
			return intentErr
		}
		token := os.Getenv("SYMMETRY_ENROLLMENT_TOKEN")
		if token == "" {
			return errors.New("SYMMETRY_ENROLLMENT_TOKEN is required for first enrollment")
		}
		enrollment := daemon.options.enrollment
		if enrollment == nil {
			client, buildErr := control.NewEnrollmentClient(daemon.config.ControlPlaneURL, daemon.options.httpClient)
			if buildErr != nil {
				return buildErr
			}
			enrollment = client
		}
		var response protocol.EnrollResponse
		enrollErr := retry(ctx, func() error {
			requestContext, cancel := daemon.controlContext(ctx)
			defer cancel()
			var requestErr error
			response, requestErr = enrollment.Enroll(requestContext, token, intent.IdempotencyKey, protocol.EnrollRequest{
				Machine:      protocol.MachineEnrollment{Name: intent.MachineName},
				MachineToken: intent.MachineToken,
			})
			return requestErr
		})
		if enrollErr != nil {
			return fmt.Errorf("enroll machine: %w", enrollErr)
		}
		if response.MachineToken != intent.MachineToken {
			return errors.New("enrollment response machine token does not match request")
		}
		identity = state.MachineIdentity{MachineID: response.MachineID, MachineToken: intent.MachineToken}
		if saveErr := daemon.store.SaveIdentity(identity); saveErr != nil {
			return fmt.Errorf("save machine identity: %w", saveErr)
		}
		if deleteErr := daemon.store.DeleteEnrollmentIntent(); deleteErr != nil {
			daemon.log.Warn("delete_enrollment_intent_failed", "error", deleteErr)
		}
		daemon.log.Info("machine_enrolled", "machine_id", identity.MachineID)
	} else if err := daemon.store.DeleteEnrollmentIntent(); err != nil {
		daemon.log.Warn("delete_stale_enrollment_intent_failed", "error", err)
	}
	client := daemon.options.control
	if client == nil {
		built, buildErr := control.NewClient(daemon.config.ControlPlaneURL, identity.MachineToken, daemon.options.httpClient)
		if buildErr != nil {
			return buildErr
		}
		client = built
	}
	daemon.control = client
	if daemon.options.workspace != nil {
		daemon.workspace = daemon.options.workspace
	} else {
		daemon.workspace = workspace.New(daemon.config.Workspaces)
	}
	if daemon.options.start != nil {
		daemon.start = daemon.options.start
	} else {
		runner := execution.NewRunner()
		daemon.start = func(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (Process, error) {
			return runner.Start(ctx, invocation, sink)
		}
	}
	if err := daemon.recoverUnresolvedInputIntents(ctx); err != nil {
		return fmt.Errorf("recover input command intents: %w", err)
	}
	if err := daemon.recoverUnclosedGoalSessions(ctx); err != nil {
		return fmt.Errorf("recover unclosed Goal sessions: %w", err)
	}
	daemon.slots = make(chan struct{}, daemon.config.Runtime.Capacity)
	instanceID, idErr := daemon.options.newID()
	if idErr != nil {
		return fmt.Errorf("generate daemon instance ID: %w", idErr)
	}
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	registeredRuntime, _, registrationMetadataErr := buildRuntimeRegistration(daemon.config.Runtime, profile, daemon.harnessCapabilities)
	if registrationMetadataErr != nil {
		return fmt.Errorf("build runtime registration: %w", registrationMetadataErr)
	}
	registrationRequest := protocol.SessionRegistrationRequest{Runtimes: []protocol.RuntimeRegistration{registeredRuntime}}
	var registration protocol.SessionRegistrationResponse
	registerErr := retry(ctx, func() error {
		requestContext, cancel := daemon.controlContext(ctx)
		defer cancel()
		var requestErr error
		registration, requestErr = daemon.control.RegisterSession(requestContext, identity.MachineID, instanceID, registrationRequest)
		return requestErr
	})
	if registerErr != nil {
		return fmt.Errorf("register daemon session: %w", registerErr)
	}
	for _, runtime := range registration.Runtimes {
		if runtime.RuntimeKey == daemon.config.Runtime.RuntimeKey {
			daemon.runtimeID, daemon.runtimeEpoch = runtime.RuntimeID, runtime.RuntimeEpoch
			break
		}
	}
	if daemon.runtimeID == "" || daemon.runtimeEpoch <= 0 {
		return errors.New("registration did not return configured runtime")
	}
	if registration.LeaseDurationMS < protocol.MinimumLeaseDurationMS {
		return fmt.Errorf("registration lease duration must be at least %dms", protocol.MinimumLeaseDurationMS)
	}
	daemon.leaseDuration = time.Duration(registration.LeaseDurationMS) * time.Millisecond
	daemon.pollEvery = time.Duration(registration.PollIntervalMS) * time.Millisecond
	daemon.heartbeatEvery = time.Duration(registration.HeartbeatIntervalMS) * time.Millisecond
	if daemon.options.notifications == nil && registration.WebSocketPath != "" {
		notifier, notifyErr := notification.New(daemon.config.ControlPlaneURL, registration.WebSocketPath, identity.MachineID, identity.MachineToken, daemon.options.httpClient)
		if notifyErr != nil {
			return fmt.Errorf("create notification client: %w", notifyErr)
		}
		daemon.options.notifications = notifier
	}
	daemon.log.Info("runtime_registered", "runtime_id", daemon.runtimeID, "runtime_epoch", daemon.runtimeEpoch)
	return nil
}

func (daemon *daemon) loadOrCreateEnrollmentIntent() (state.EnrollmentIntent, error) {
	intent, err := daemon.store.LoadEnrollmentIntent()
	if err == nil {
		return intent, nil
	}
	if !state.IsNotFound(err) {
		return state.EnrollmentIntent{}, fmt.Errorf("load enrollment intent: %w", err)
	}
	idempotencyKey, err := daemon.options.newID()
	if err != nil {
		return state.EnrollmentIntent{}, fmt.Errorf("generate enrollment idempotency key: %w", err)
	}
	newMachineToken := daemon.options.newMachineToken
	if newMachineToken == nil {
		newMachineToken = state.NewMachineToken
	}
	machineToken, err := newMachineToken()
	if err != nil {
		return state.EnrollmentIntent{}, err
	}
	intent = state.EnrollmentIntent{
		MachineName:    daemon.config.MachineName,
		MachineToken:   machineToken,
		IdempotencyKey: idempotencyKey,
	}
	if err := daemon.store.SaveEnrollmentIntent(intent); err != nil {
		return state.EnrollmentIntent{}, fmt.Errorf("save enrollment intent: %w", err)
	}
	return intent, nil
}

func (daemon *daemon) close() {
	if daemon.options.store == nil && daemon.store != nil {
		_ = daemon.store.Close()
	}
}

func (daemon *daemon) interval(value time.Duration) time.Duration {
	if value < minimumInterval {
		return minimumInterval
	}
	if value > maximumInterval {
		return maximumInterval
	}
	return value
}

func (daemon *daemon) now() time.Time {
	if daemon.options.clock != nil {
		return daemon.options.clock()
	}
	return time.Now().UTC()
}

func (daemon *daemon) timer(delay time.Duration) deadlineTimer {
	if delay < 0 {
		delay = 0
	}
	if daemon.options.newTimer != nil {
		return daemon.options.newTimer(delay)
	}
	return systemTimer{timer: time.NewTimer(delay)}
}

func (daemon *daemon) controlContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, controlRequestLimit)
}

func (daemon *daemon) rootContext(ctx context.Context) context.Context {
	root := ctx
	daemon.mu.Lock()
	if daemon.background != nil {
		root = daemon.background
	}
	daemon.mu.Unlock()
	if root == nil {
		return context.Background()
	}
	return root
}

func (daemon *daemon) recoverUnresolvedInputIntents(ctx context.Context) error {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	recoveries := make([]state.RunJournal, 0, len(journals))
	for _, journal := range journals {
		if !restartInputRecoveryRequired(journal) || daemon.hasRun(journal.Key()) {
			continue
		}
		if supervisoryRecoveryRequired(journal) {
			daemon.rememberWorkspaceRetention(journal.Key())
		}
		recoveries = append(recoveries, journal)
		if journal.PID > 0 {
			if !daemon.terminatePersistedProcessWithRetry(rootContext, journal.Key()) {
				return rootContext.Err()
			}
			if _, err := daemon.store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
				return fmt.Errorf("record stopped recovered input process %s/%d: %w", journal.RunID, journal.Generation, err)
			}
		}
	}
	for _, journal := range recoveries {
		if daemon.hasRun(journal.Key()) {
			continue
		}
		if supervisoryRecoveryRequired(journal) {
			daemon.persistWorkspaceRetention(journal.Key())
		}
		if err := daemon.drainRestartInputOutbox(rootContext, journal.Key()); err != nil && !isConclusiveRestartInputFailure(err) {
			return fmt.Errorf("drain input command outbox for %s/%d: %w", journal.RunID, journal.Generation, err)
		}
		if err := daemon.queueRestartInputFailure(rootContext, journal.Key()); err != nil {
			return fmt.Errorf("queue restart input failure for %s/%d: %w", journal.RunID, journal.Generation, err)
		}
	}
	return nil
}

// recoverUnclosedGoalSessions prevents a restart from silently attaching to,
// replacing, or cleaning up around a native session whose stopped state has not
// been proven. The Goal session journal is the durable source for this local
// recovery decision; control-plane reattachment is deliberately out of scope.
func (daemon *daemon) recoverUnclosedGoalSessions(ctx context.Context) error {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	for _, session := range sessions {
		key := state.RunKey{RunID: session.RunID, Generation: session.Generation}
		if key.RunID == "" || key.Generation <= 0 || daemon.hasRun(key) {
			continue
		}
		journal, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			if state.IsNotFound(loadErr) {
				if session.SessionState != state.GoalSessionStateClosed {
					if _, markErr := daemon.store.MarkGoalSessionUncertain(session.Key(), "associated run journal is unavailable during daemon recovery"); markErr != nil {
						return fmt.Errorf("mark Goal session uncertain without run %s/%d: %w", key.RunID, key.Generation, markErr)
					}
				}
				continue
			}
			return fmt.Errorf("load associated run %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
		processEvidence := journal.PID > 0 || strings.TrimSpace(journal.ProcessIdentity) != "" || !journal.StartedAt.IsZero()
		terminalKnown := journal.TerminalState != "" || journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending"
		for _, delivery := range journal.PendingGoalDeliveries {
			if delivery.Kind != state.GoalDeliverySessionAttach || delivery.DeliveryID != session.LocalHandleID || delivery.Ready {
				continue
			}
			updated, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, session.LocalHandleID)
			if discardErr != nil {
				return fmt.Errorf("discard unready Goal session attach for %s/%d: %w", key.RunID, key.Generation, discardErr)
			}
			journal = updated
			break
		}
		if session.SessionState == state.GoalSessionStateClosed && terminalKnown && !processEvidence {
			continue
		}

		// Retention is durable before any process-control action. The native
		// session could have changed the worktree after the last Run event.
		if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil {
			return fmt.Errorf("retain workspace for recovered Goal session %s/%d: %w", key.RunID, key.Generation, err)
		}
		if session.SessionState != state.GoalSessionStateClosed && session.LaunchState != state.GoalSessionLaunchStateUncertain {
			if _, err := daemon.store.MarkGoalSessionUncertain(session.Key(), "native Goal session was left unclosed across daemon restart"); err != nil {
				return fmt.Errorf("mark Goal session uncertain for %s/%d: %w", key.RunID, key.Generation, err)
			}
		}
		if journal.PID > 0 && strings.TrimSpace(journal.ProcessIdentity) != "" {
			if !daemon.terminatePersistedProcessWithRetry(rootContext, key) {
				return fmt.Errorf("stop recovered native process %s/%d: %w", key.RunID, key.Generation, rootContext.Err())
			}
			if _, err := daemon.store.ClearProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
				return fmt.Errorf("record stopped native process %s/%d: %w", key.RunID, key.Generation, err)
			}
		}
		if err := daemon.queueNativeGoalUsage(rootContext, key, nil); err != nil {
			return fmt.Errorf("queue recovered Goal usage for %s/%d: %w", key.RunID, key.Generation, err)
		}
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
			continue
		}
		if err := daemon.queueTerminalTransitionWithRetry(rootContext, key, "failed", map[string]string{
			"stage":   "goal_session_recovery",
			"reason":  string(protocol.TaskResultReasonUnknownOutcome),
			"summary": "native Goal session was not safely recoverable after daemon restart",
			"error":   "native Goal session was not safely recoverable after daemon restart",
		}); err != nil {
			return fmt.Errorf("queue unknown native outcome for %s/%d: %w", key.RunID, key.Generation, err)
		}
	}
	return nil
}

func restartInputRecoveryRequired(journal state.RunJournal) bool {
	if journal.InputCommandIntent == nil && !supervisoryRecoveryRequired(journal) {
		return false
	}
	switch journal.LocalState {
	case "stale", "terminal_pending", "cleanup_pending":
		return false
	default:
		return true
	}
}

func (daemon *daemon) drainRestartInputOutbox(ctx context.Context, key state.RunKey) error {
	ctx = context.WithValue(ctx, restartOutboxRecoveryContextKey{}, true)
	delay := minimumInterval
	for {
		if daemon.hasRun(key) {
			return nil
		}
		journal, err := daemon.store.LoadJournal(key)
		if state.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !restartInputRecoveryRequired(journal) {
			return nil
		}
		err = daemon.flushRun(ctx, journal)
		if err == nil {
			return nil
		}
		if errors.Is(err, errOutboxChanged) {
			continue
		}
		if isConclusiveRestartInputFailure(err) {
			return err
		}
		wait, retryable := restartInputRecoveryRetryDelay(ctx, err, delay)
		if !retryable {
			return err
		}
		timer := daemon.timer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.Chan():
		}
		delay = nextRetryDelay(delay)
	}
}

func restartInputRecoveryRetryDelay(ctx context.Context, err error, fallback time.Duration) (time.Duration, bool) {
	delay, retryable := control.RetryDelay(ctx, err, fallback)
	if !retryable {
		var responseError *control.ResponseError
		if !errors.As(err, &responseError) {
			return 0, false
		}
		delay, retryable = fallback, true
	}
	if delay < minimumInterval {
		delay = minimumInterval
	}
	return delay, retryable
}

func isConclusiveRestartInputFailure(err error) bool {
	return control.IsOwnershipLost(err) || control.IsTerminalGraceExpired(err)
}

func (daemon *daemon) queueRestartInputFailure(ctx context.Context, key state.RunKey) error {
	if daemon.hasRun(key) {
		return nil
	}
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !restartInputRecoveryRequired(journal) {
		return nil
	}
	recoveryError := "input command recovery cannot safely replay stdin"
	if supervisoryRecoveryRequired(journal) {
		recoveryError = "supervisory recovery cannot safely reattach a paused or controlled agent"
	}
	return daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", map[string]string{
		"stage":   "daemon_restart",
		"reason":  string(protocol.TaskResultReasonUnknownOutcome),
		"summary": recoveryError,
		"error":   recoveryError,
	})
}

func (daemon *daemon) heartbeat(ctx context.Context) {
	requestContext, cancel := daemon.controlContext(ctx)
	requestID := daemon.beginCommandRequest()
	snapshot, err := daemon.control.Heartbeat(requestContext, daemon.runtimeID, protocol.RuntimeHeartbeatRequest{RuntimeEpoch: daemon.runtimeEpoch, ActiveRuns: daemon.activeRuns()})
	cancel()
	if err != nil {
		daemon.finishCommandRequest(requestID)
		daemon.log.Warn("runtime_heartbeat_failed", "error", err)
		return
	}
	daemon.observeControlClock(snapshot.ServerTime)
	daemon.scheduleSnapshotForRequest(requestID, snapshot)
}

func (daemon *daemon) sync(ctx context.Context) {
	requestContext, cancel := daemon.controlContext(ctx)
	requestID := daemon.beginCommandRequest()
	snapshot, err := daemon.control.Dispatch(requestContext, daemon.runtimeID, daemon.runtimeEpoch)
	cancel()
	if err != nil {
		daemon.finishCommandRequest(requestID)
		daemon.log.Warn("runtime_poll_failed", "error", err)
		return
	}
	daemon.observeControlClock(snapshot.ServerTime)
	daemon.scheduleSnapshotForRequest(requestID, snapshot)
}

func (daemon *daemon) reconcile(ctx context.Context) bool {
	if err := daemon.recoverUnresolvedInputIntents(ctx); err != nil {
		daemon.log.Warn("recover_input_command_intents_failed", "error", err)
		return false
	}
	journals, err := daemon.store.ListJournals()
	if err != nil {
		daemon.log.Error("list_journals_failed", "error", err)
		return false
	}
	runs := make([]protocol.ReconcileRun, 0, len(journals))
	for _, journal := range journals {
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
			continue
		}
		if isReconcileState(journal.LocalState) && hasFullFence(journal) {
			runs = append(runs, protocol.ReconcileRun{RunID: journal.RunID, Generation: journal.Generation, ClaimedRuntimeEpoch: journal.ClaimedRuntimeEpoch, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken, LocalState: journal.LocalState, LastEventSequence: journal.LastEventSequence})
			continue
		}
		if !daemon.hasRun(journal.Key()) {
			daemon.stopRecoveredJournal(journal, "not eligible for reconcile")
		}
	}
	requestContext, cancel := daemon.controlContext(ctx)
	requestID := daemon.beginCommandRequest()
	response, err := daemon.control.Reconcile(requestContext, daemon.runtimeID, protocol.ReconcileRequest{RuntimeEpoch: daemon.runtimeEpoch, Runs: runs})
	cancel()
	if err != nil {
		daemon.finishCommandRequest(requestID)
		daemon.log.Warn("runtime_reconcile_failed", "error", err)
		return false
	}
	handledCancels := make(map[commandKey]struct{})
	for _, decision := range response.Decisions {
		key := state.RunKey{RunID: decision.RunID, Generation: decision.Generation}
		if decision.Decision == protocol.ReconcileCancel {
			if daemon.hasRun(key) {
				// A live run receives the durable command from this same snapshot.
				// Its in-memory process/session is the only authority able to stop it.
				continue
			}
			if journal, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
				commandID := reconcileCancelCommand(response.Commands, key)
				if daemon.cancelRecoveredJournal(ctx, journal, commandID) && commandID != "" {
					handledCancels[commandKey{run: key, id: commandID}] = struct{}{}
				}
			}
			continue
		}
		if decision.Decision != protocol.ReconcileContinue {
			if journal, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
				if daemon.hasRun(key) {
					daemon.terminateForLease(journal, string(decision.Decision))
				} else {
					daemon.stopRecoveredJournal(journal, string(decision.Decision))
				}
			}
			continue
		}
		if decision.LeaseExpiresAt != nil {
			_, _ = daemon.store.AdvanceLeaseExpiry(key, *decision.LeaseExpiresAt)
		}
	}
	// Reconcile decisions are the authoritative recovery instruction. In
	// particular, process-backed Goal sessions must see ReconcileCancel before
	// the conservative unknown-outcome fallback can terminalize their journal.
	if err := daemon.recoverUnclosedGoalSessions(ctx); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("recover_unclosed_goal_sessions_failed", "error", err)
		}
		return false
	}
	commands := response.Commands
	if len(handledCancels) != 0 {
		commands = make([]protocol.Command, 0, len(response.Commands)-len(handledCancels))
		for _, command := range response.Commands {
			if _, handled := handledCancels[commandKey{run: state.RunKey{RunID: command.RunID, Generation: command.Generation}, id: command.CommandID}]; !handled {
				commands = append(commands, command)
			}
		}
	}
	daemon.scheduleSnapshotForRequest(requestID, protocol.RuntimeSnapshot{Assignments: response.Assignments, Commands: commands})
	return true
}

func reconcileCancelCommand(commands []protocol.Command, key state.RunKey) string {
	for _, command := range commands {
		if command.Kind == "cancel" && command.RunID == key.RunID && command.Generation == key.Generation && command.CommandID != "" {
			return command.CommandID
		}
	}
	return ""
}

func (daemon *daemon) observeControlClock(serverTime time.Time) {
	if serverTime.IsZero() {
		return
	}
	skew := serverTime.Sub(daemon.now())
	absSkew := skew
	if absSkew < 0 {
		absSkew = -absSkew
	}
	outOfTolerance := absSkew > leaseSafetyMargin
	daemon.mu.Lock()
	changed := daemon.clockSkewWarned != outOfTolerance
	daemon.clockSkewWarned = outOfTolerance
	daemon.mu.Unlock()
	if !changed || daemon.log == nil {
		return
	}
	if outOfTolerance {
		daemon.log.Warn("control_clock_skew_detected", "skew_ms", skew.Milliseconds(), "lease_safety_margin_ms", leaseSafetyMargin.Milliseconds())
	} else {
		daemon.log.Info("control_clock_skew_cleared", "skew_ms", skew.Milliseconds(), "lease_safety_margin_ms", leaseSafetyMargin.Milliseconds())
	}
}

func isReconcileState(localState string) bool {
	switch localState {
	case "claimed", "running", "paused", "waiting_for_input", "cancelling":
		return true
	default:
		return false
	}
}

func hasFullFence(journal state.RunJournal) bool {
	fence := journal.Fence()
	return fence.RuntimeID != "" && fence.RuntimeEpoch > 0 && fence.Generation > 0 && fence.ClaimID != "" && fence.LeaseToken != ""
}

func (daemon *daemon) stopRecoveredJournal(journal state.RunJournal, reason string) {
	if journal.LocalState == "terminal_pending" {
		return
	}
	if journal.LocalState == "cleanup_pending" {
		daemon.enqueueCleanup(journal.Key())
		return
	}
	hasUnclosedGoalSession, err := daemon.recoveredGoalSessionRequiresStop(journal.Key())
	if err != nil {
		if daemon.log != nil {
			daemon.log.Warn("load_recovered_goal_session_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return
	}
	if hasUnclosedGoalSession && !recoveredProcessRecorded(journal) {
		daemon.retainUnknownGoalLaunchWorkspace(journal.Key())
		if daemon.log != nil {
			daemon.log.Warn("stop_recovered_goal_session_unproven", "run_id", journal.RunID, "generation", journal.Generation)
		}
		return
	}
	if recoveredProcessRecorded(journal) {
		if hasUnclosedGoalSession {
			daemon.retainUnknownGoalLaunchWorkspace(journal.Key())
		}
		if journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "" || daemon.options.terminatePersist == nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_process_unavailable", "run_id", journal.RunID, "generation", journal.Generation)
			}
			return
		}
		if err := daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_process_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return
		}
		if _, err := daemon.store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("record_recovered_process_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return
		}
	}
	if err := daemon.resolveRecoveredGoalSessionStopped(journal.Key()); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("resolve_recovered_goal_session_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return
	}
	updated, err := daemon.store.SetLocalState(journal.Key(), "stale")
	if err != nil {
		daemon.log.Warn("mark_recovered_journal_stale_failed", "run_id", journal.RunID, "error", err)
		return
	}
	if err := daemon.scheduleCleanup(context.Background(), updated); err != nil {
		daemon.log.Warn("cleanup_recovered_workspace_failed", "run_id", journal.RunID, "error", err)
		return
	}
	daemon.log.Warn("recovered_journal_stopped", "run_id", journal.RunID, "generation", journal.Generation, "reason", reason)
}

func recoveredProcessRecorded(journal state.RunJournal) bool {
	return journal.PID > 0 || strings.TrimSpace(journal.ProcessIdentity) != "" || !journal.StartedAt.IsZero()
}

// cancelRecoveredJournal has no in-memory owner to race against. It retains
// artifacts before process control, records a stopped process durably, then
// commits the cancellation and its matching command acknowledgement together.
// A failed stop deliberately leaves the journal non-stale and uncleaned.
func (daemon *daemon) cancelRecoveredJournal(ctx context.Context, journal state.RunJournal, commandID string) bool {
	key := journal.Key()
	if journal.LocalState == "cleanup_pending" || (journal.LocalState == "terminal_pending" && journal.TerminalVerdict != "") {
		return false
	}
	if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("retain_recovered_cancel_workspace_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	}
	hasUnclosedGoalSession, err := daemon.recoveredGoalSessionRequiresStop(key)
	if err != nil {
		if daemon.log != nil {
			daemon.log.Warn("load_recovered_goal_session_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	}
	if recoveredProcessRecorded(journal) {
		if strings.TrimSpace(journal.ProcessIdentity) == "" || daemon.options.terminatePersist == nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_cancel_process_unavailable", "run_id", journal.RunID, "generation", journal.Generation)
			}
			return false
		}
		if err := daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_cancel_process_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return false
		}
		if _, err := daemon.store.ClearProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("record_recovered_cancel_process_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return false
		}
	} else if hasUnclosedGoalSession {
		// The missing process marker is not proof that an uncertain native launch
		// never escaped. Keep the lineage barrier until a bounded stop has been
		// recorded instead of acknowledging a cancellation that may leave work.
		if daemon.log != nil {
			daemon.log.Warn("stop_recovered_cancel_goal_session_unproven", "run_id", journal.RunID, "generation", journal.Generation)
		}
		return false
	}
	queued := false
	if commandID != "" {
		if !daemon.queueCancellationReceipt(ctx, key, commandID) {
			return false
		}
		queued = true
	} else if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "cancelled", map[string]string{"reason": string(protocol.TaskResultReasonCancelled)}); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("queue_recovered_cancel_transition_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	} else {
		queued = true
	}
	if err := daemon.resolveRecoveredGoalSessionStopped(key); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("resolve_recovered_goal_session_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return queued
	}
	return true
}

func (daemon *daemon) recoveredGoalSessionRequiresStop(key state.RunKey) (bool, error) {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return false, err
	}
	for _, session := range sessions {
		if session.RunID == key.RunID && session.Generation == key.Generation && session.SessionState != state.GoalSessionStateClosed {
			return true, nil
		}
	}
	return false, nil
}

func (daemon *daemon) resolveRecoveredGoalSessionStopped(key state.RunKey) error {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.RunID != key.RunID || session.Generation != key.Generation {
			continue
		}
		if session.NeedsReconciliation() {
			if _, err := daemon.store.ResolveGoalSessionUncertainStopped(session.Key(), session.Compatibility()); err != nil {
				return err
			}
		} else if session.SessionState != state.GoalSessionStateClosed {
			if _, err := daemon.store.CloseGoalSession(session.Key()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (daemon *daemon) handleSnapshot(ctx context.Context, snapshot protocol.RuntimeSnapshot) {
	completions, ok := daemon.enqueueSnapshotCommandsForRequest(0, snapshot.Commands)
	if !ok || !waitCommandCompletions(ctx, completions) {
		return
	}
	for _, assignment := range snapshot.Assignments {
		if ctx.Err() != nil {
			return
		}
		daemon.startAssignment(ctx, assignment)
	}
}

func (daemon *daemon) scheduleSnapshot(snapshot protocol.RuntimeSnapshot) {
	daemon.scheduleSnapshotForRequest(0, snapshot)
}

func (daemon *daemon) scheduleSnapshotForRequest(requestID uint64, snapshot protocol.RuntimeSnapshot) {
	completions, ok := daemon.enqueueSnapshotCommandsForRequest(requestID, snapshot.Commands)
	if !ok {
		return
	}
	delayed := make(map[state.RunKey][]commandCompletion)
	for _, completion := range completions {
		delayed[completion.run] = append(delayed[completion.run], completion)
	}
	daemon.mu.Lock()
	ctx := daemon.background
	if ctx == nil || daemon.backgroundStop {
		daemon.mu.Unlock()
		return
	}
	delayedCount := 0
	for _, assignment := range snapshot.Assignments {
		if len(delayed[state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}]) > 0 {
			delayedCount++
		}
	}
	daemon.snapshotWG.Add(delayedCount)
	daemon.mu.Unlock()
	for _, assignment := range snapshot.Assignments {
		assignment := assignment
		relevant := delayed[state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}]
		if len(relevant) == 0 {
			if ctx.Err() == nil {
				daemon.startAssignment(ctx, assignment)
			}
			continue
		}
		go func() {
			defer daemon.snapshotWG.Done()
			if !waitCommandCompletions(ctx, relevant) || ctx.Err() != nil {
				return
			}
			daemon.startAssignment(ctx, assignment)
		}()
	}
}

func (daemon *daemon) enqueueSnapshotCommands(commands []protocol.Command) ([]commandCompletion, bool) {
	return daemon.enqueueSnapshotCommandsForRequest(0, commands)
}

func (daemon *daemon) enqueueSnapshotCommandsForRequest(requestID uint64, commands []protocol.Command) ([]commandCompletion, bool) {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	completions := make([]commandCompletion, 0, len(commands))
	var wake chan<- struct{}
	daemon.mu.Lock()
	tombstones := daemon.commandRequests[requestID]
	for _, command := range commands {
		key := commandKey{run: state.RunKey{RunID: command.RunID, Generation: command.Generation}, id: command.CommandID}
		if requestID != 0 {
			if _, suppressed := tombstones[key]; suppressed {
				continue
			}
		}
		completion, commandWake := daemon.enqueueCommandLocked(command)
		if completion == nil {
			if requestID != 0 {
				delete(daemon.commandRequests, requestID)
			}
			daemon.mu.Unlock()
			return nil, false
		}
		if commandWake != nil {
			wake = commandWake
		}
		completions = append(completions, commandCompletion{
			run:  state.RunKey{RunID: command.RunID, Generation: command.Generation},
			done: completion,
		})
	}
	if requestID != 0 {
		delete(daemon.commandRequests, requestID)
	}
	daemon.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return completions, true
}

func (daemon *daemon) beginCommandRequest() uint64 {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.commandRequests == nil {
		daemon.commandRequests = make(map[uint64]map[commandKey]struct{})
	}
	daemon.nextCommandRequest++
	requestID := daemon.nextCommandRequest
	daemon.commandRequests[requestID] = make(map[commandKey]struct{})
	return requestID
}

func (daemon *daemon) finishCommandRequest(requestID uint64) {
	if requestID == 0 {
		return
	}
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	delete(daemon.commandRequests, requestID)
	daemon.mu.Unlock()
}

func waitCommandCompletions(ctx context.Context, completions []commandCompletion) bool {
	for _, completion := range completions {
		select {
		case <-ctx.Done():
			return false
		case <-completion.done:
			if ctx.Err() != nil {
				return false
			}
		}
	}
	return true
}

func (daemon *daemon) startAssignment(ctx context.Context, assignment protocol.Assignment) {
	if ctx.Err() != nil {
		return
	}
	key := state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}
	if assignment.RunID == "" || assignment.Generation <= 0 {
		return
	}
	if journal, err := daemon.store.LoadJournal(key); err == nil && journal.LocalState == "cleanup_pending" {
		return
	}
	select {
	case daemon.slots <- struct{}{}:
	default:
		return
	}
	daemon.mu.Lock()
	if _, exists := daemon.running[key]; exists {
		daemon.mu.Unlock()
		<-daemon.slots
		return
	}
	runContext, cancel := context.WithCancel(ctx)
	daemon.running[key] = &runningRun{starting: true, cancel: cancel, slotHeld: true, cleanupBlocked: true}
	daemon.mu.Unlock()
	daemon.workers.Add(1)
	go func() {
		defer daemon.workers.Done()
		daemon.startAssigned(runContext, key, assignment)
	}()
}

func (daemon *daemon) startAssigned(ctx context.Context, key state.RunKey, assignment protocol.Assignment) {
	prepared := workspace.Prepared{}
	preparedOK := false
	workspacePersisted := false
	handoff := false
	defer func() {
		if handoff {
			return
		}
		if preparedOK && !workspacePersisted {
			return
		}
		if daemon.finishStarting(key, true) {
			daemon.signalOutbox()
			return
		}
		stale := daemon.isStale(key)
		if daemon.isTerminal(key) {
			return
		}
		if stale {
			journal, err := daemon.store.SetLocalState(key, "stale")
			daemon.releaseRun(key)
			if err == nil {
				_ = daemon.scheduleCleanup(context.Background(), journal)
			}
			return
		}
		if preparedOK {
			if journal, err := daemon.store.SetLocalState(key, "stale"); err == nil {
				daemon.releaseRun(key)
				_ = daemon.scheduleCleanup(context.Background(), journal)
				return
			}
		}
		daemon.releaseRun(key)
	}()

	journal, err := daemon.store.LoadJournal(key)
	if err == nil {
		if journal.RuntimeID != daemon.runtimeID || journal.ClaimedRuntimeEpoch != daemon.runtimeEpoch || journal.LocalState != "claiming" {
			return
		}
	} else if state.IsNotFound(err) {
		claimID, idErr := daemon.options.newID()
		if idErr != nil {
			daemon.log.Error("generate_claim_id_failed", "error", idErr)
			return
		}
		intent := state.ClaimIntent{Key: key, RuntimeKey: daemon.config.Runtime.RuntimeKey, RuntimeID: daemon.runtimeID, RuntimeEpoch: daemon.runtimeEpoch, ClaimID: claimID, LocalState: "claiming", Work: assignment.Work, WorkspaceBindingKey: daemon.config.Runtime.Workspace}
		journal, err = daemon.store.SaveClaimIntent(intent)
		if err != nil {
			daemon.log.Warn("save_claim_intent_failed", "run_id", assignment.RunID, "error", err)
			return
		}
	} else {
		daemon.log.Warn("load_claim_intent_failed", "run_id", assignment.RunID, "error", err)
		return
	}
	claim, err := daemon.claimWithRetryUntil(ctx, assignment.RunID, protocol.ClaimRequest{RuntimeID: daemon.runtimeID, RuntimeEpoch: daemon.runtimeEpoch, Generation: assignment.Generation, ClaimID: journal.ClaimID}, assignment.AssignmentExpiresAt, waitForRetry)
	if err != nil {
		if control.IsOwnershipLost(err) {
			daemon.stopRecoveredJournal(journal, "claim ownership lost")
		}
		daemon.log.Warn("claim_failed", "run_id", assignment.RunID, "error", err)
		return
	}
	journal, err = daemon.store.SaveClaimGrant(key, claim)
	if err != nil {
		daemon.log.Error("save_claim_grant_failed", "run_id", assignment.RunID, "error", err)
		return
	}
	admission, admissionPresent, admissionErr := parseAdmissionInput(claim.Work.Input)
	daemon.mu.Lock()
	active := daemon.running[key]
	cancelCommandID := ""
	operatorCancelled := active != nil && active.cancelCommandID != ""
	stale := active == nil
	contextCancelled := ctx.Err() != nil
	if active != nil {
		active.claimed = true
		cancelCommandID = active.cancelCommandID
		stale = active.stale
	}
	if !operatorCancelled && !stale && !contextCancelled && !admissionPresent {
		err = daemon.markRunning(key)
	}
	daemon.mu.Unlock()
	if operatorCancelled {
		receipt := daemon.queueCancellationReceipt(ctx, key, cancelCommandID)
		daemon.finishDeferredCommand(commandKey{run: key, id: cancelCommandID}, receipt)
		return
	}
	if stale || contextCancelled {
		return
	}
	if admissionPresent {
		if admissionErr != nil {
			daemon.queueFailure(ctx, key, "invalid_admission", admissionErr)
			return
		}
		if err := daemon.startGoalAdmission(ctx, key, claim, admission, func(value workspace.Prepared) {
			prepared = value
			preparedOK = true
			workspacePersisted = true
		}); err != nil {
			daemon.queueFailure(ctx, key, "goal_admission", err)
			return
		}
		handoff = true
		daemon.waitForRunWithContext(ctx, key)
		return
	}
	if err != nil {
		daemon.log.Error("queue_running_failed", "run_id", assignment.RunID, "error", err)
		daemon.queueFailure(ctx, key, "queue_running", err)
		return
	}
	daemon.signalOutboxFor(key)
	if _, err := daemon.store.MarkWorkspaceRecoveryRequired(key); err != nil {
		daemon.log.Error("mark_workspace_recovery_required_failed", "run_id", assignment.RunID, "error", err)
		daemon.queueFailure(ctx, key, "mark_workspace_recovery_required", err)
		return
	}
	prepared, err = daemon.workspace.Prepare(ctx, daemon.config.Runtime.Workspace, workspace.RunRef{RunID: key.RunID, Generation: key.Generation})
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "prepare_workspace", err)
		}
		return
	}
	preparedOK = true
	if !daemon.persistWorkspacePath(ctx, key, prepared.Path) {
		return
	}
	workspacePersisted = true
	if daemon.isCancelled(key) {
		return
	}
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	environment, err := execution.BuildEnvironment(profile.EnvAllowlist...)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "build_environment", err)
		}
		return
	}
	input, err := initialInput(profile, claim.Work, claim.ProviderAccess, daemon.config.ControlPlaneURL)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "encode_input", err)
		}
		return
	}
	providerToken := ""
	if claim.ProviderAccess != nil {
		providerToken = claim.ProviderAccess.Token
	}
	output := newAgentOutput(profile.EventFormat, providerToken)
	executionContext, stopExecution := context.WithCancelCause(ctx)
	defer stopExecution(nil)
	output.executionContext = executionContext
	sink := execution.SinkFunc(func(_ context.Context, event execution.Event) error {
		err := output.handle(daemon, key, event)
		if errors.Is(err, errRequiredDecisionPacket) {
			// A contract-invalid required wait cannot be resumed. Cancel only
			// this process context, even if stdout precedes process attachment;
			// it remains a failed execution rather than an operator cancellation.
			stopExecution(err)
		}
		return err
	})
	process, err := daemon.start(executionContext, execution.Invocation{
		Program:                profile.Command,
		Args:                   profile.Args,
		Dir:                    prepared.Path,
		Env:                    environment,
		InitialInput:           input,
		CloseInputAfterInitial: !profile.Interactive,
		PersistProcess: func(pid int, identity string) error {
			if daemon.options.recordProcess != nil {
				_, recordErr := daemon.options.recordProcess(key, pid, identity, daemon.now())
				return recordErr
			}
			_, recordErr := daemon.store.SetProcessDetails(key, pid, identity, daemon.now())
			return recordErr
		},
	}, sink)
	if err != nil {
		if cause := context.Cause(executionContext); errors.Is(cause, errRequiredDecisionPacket) {
			err = cause
		}
		if process != nil {
			daemon.mu.Lock()
			active = daemon.running[key]
			if active != nil {
				active.process = process
				active.prepared = prepared
				active.output = output
				active.stopExecution = stopExecution
			}
			daemon.mu.Unlock()
			if active == nil {
				daemon.terminateProcessBounded(process, 0)
				return
			}
			if pid, identity, detailsErr := processDetails(process); detailsErr != nil {
				err = errors.Join(err, fmt.Errorf("read process identity after start failure: %w", detailsErr))
			} else if daemon.options.recordProcess != nil {
				if _, recordErr := daemon.options.recordProcess(key, pid, identity, daemon.now()); recordErr != nil {
					err = errors.Join(err, fmt.Errorf("record process after start failure: %w", recordErr))
				}
			} else if _, recordErr := daemon.store.SetProcessDetails(key, pid, identity, daemon.now()); recordErr != nil {
				err = errors.Join(err, fmt.Errorf("record process after start failure: %w", recordErr))
			}
			daemon.mu.Lock()
			if active := daemon.running[key]; active != nil && active.process == process {
				active.startFailure = err
			}
			daemon.mu.Unlock()
			daemon.finishAttachedStart(key)
			handoff = true
			if !daemon.isCancelled(key) {
				daemon.queueFailure(ctx, key, "start_agent", err)
			}
			// Runner owns the physical command wait after it has exposed a
			// Process. Do not make daemon shutdown depend on an indeterminate
			// post-start failure path; the persisted identity remains available
			// to recovery if its bounded termination cannot prove exit.
			go daemon.watchFailedStartProcess(ctx, key, process)
			return
		}
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "start_agent", err)
		}
		return
	}
	daemon.mu.Lock()
	active = daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		daemon.terminateProcessBounded(process, 0)
		return
	}
	active.process = process
	active.prepared = prepared
	active.output = output
	active.stopExecution = stopExecution
	daemon.mu.Unlock()

	pid, identity, detailsErr := processDetails(process)
	if detailsErr != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "read_process_identity", detailsErr)
		}
		_ = process.Terminate(context.Background(), 0)
		if daemon.finishAttachedStart(key) {
			daemon.signalOutbox()
		}
		handoff = true
		daemon.waitForRunWithContext(ctx, key)
		return
	}
	if daemon.options.recordProcess != nil {
		_, err = daemon.options.recordProcess(key, pid, identity, daemon.options.clock())
	} else {
		_, err = daemon.store.SetProcessDetails(key, pid, identity, daemon.options.clock())
	}
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "record_process", err)
		}
		_ = process.Terminate(context.Background(), 0)
		if daemon.finishAttachedStart(key) {
			daemon.signalOutbox()
		}
		handoff = true
		daemon.waitForRunWithContext(ctx, key)
		return
	}
	if daemon.isCancelled(key) || ctx.Err() != nil {
		_ = process.Terminate(context.Background(), 0)
	}
	if daemon.finishAttachedStart(key) {
		daemon.signalOutbox()
	}
	handoff = true
	daemon.waitForRunWithContext(ctx, key)
}

func (daemon *daemon) markRunning(key state.RunKey) error {
	encoded, err := json.Marshal(map[string]any{})
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	transition := protocol.StateTransitionRequest{TransitionID: id, State: "running", Payload: encoded}
	_, err = daemon.store.QueueRunningTransition(key, transition)
	return err
}

func (daemon *daemon) releaseRun(key state.RunKey) {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	active := daemon.running[key]
	delete(daemon.running, key)
	daemon.releaseCommandReceiptsLocked(key)
	releaseSlot := active != nil && active.slotHeld
	renewCancel := context.CancelFunc(nil)
	terminalCancel := context.CancelFunc(nil)
	if active != nil {
		active.slotHeld = false
		renewCancel = active.renewCancel
		active.renewCancel = nil
		active.renewCancelID++
		terminalCancel = active.terminalCancel
		active.terminalCancel = nil
	}
	daemon.mu.Unlock()
	if active != nil && active.cancel != nil {
		active.cancel()
	}
	if renewCancel != nil {
		renewCancel()
	}
	if terminalCancel != nil {
		terminalCancel()
	}
	if releaseSlot {
		<-daemon.slots
	}
}

func (daemon *daemon) clearCompletedCommandReceipts(key state.RunKey) {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	daemon.releaseCommandReceiptsLocked(key)
}

func (daemon *daemon) releaseCommandReceiptsLocked(key state.RunKey) {
	for commandKey, queued := range daemon.queuedCommands {
		if commandKey.run == key {
			if !queued.completed {
				queued.completed = true
				close(queued.done)
			}
			delete(daemon.queuedCommands, commandKey)
		}
	}
}

func (daemon *daemon) releaseSlotOnce(key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	releaseSlot := active != nil && active.slotHeld
	terminalCancel := context.CancelFunc(nil)
	if active != nil {
		active.slotHeld = false
		terminalCancel = active.terminalCancel
		active.terminalCancel = nil
	}
	daemon.mu.Unlock()
	if terminalCancel != nil {
		terminalCancel()
	}
	if releaseSlot {
		<-daemon.slots
	}
}

func (daemon *daemon) scheduleTerminalSlotRelease(key state.RunKey, pendingAt time.Time) {
	daemon.mu.Lock()
	active := daemon.running[key]
	background := daemon.background
	if active == nil || !active.slotHeld || background == nil || daemon.backgroundStop {
		daemon.mu.Unlock()
		return
	}
	previousCancel := active.terminalCancel
	timerContext, cancel := context.WithCancel(background)
	active.terminalCancel = cancel
	daemon.terminalReleaseWG.Add(1)
	daemon.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	go func() {
		defer daemon.terminalReleaseWG.Done()
		deadline := pendingAt.Add(terminalGrace)
		delay := deadline.Sub(daemon.now())
		if delay > 0 {
			timer := daemon.timer(delay)
			select {
			case <-timerContext.Done():
				timer.Stop()
				return
			case <-timer.Chan():
			}
		}
		if timerContext.Err() != nil {
			return
		}
		daemon.releaseTerminalSlotAt(key, pendingAt)
	}()
}

func (daemon *daemon) releaseTerminalSlotAt(key state.RunKey, pendingAt time.Time) {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || journal.LocalState != "terminal_pending" || journal.TerminalVerdict != "" || !journal.TerminalPendingAt.Equal(pendingAt) {
		return
	}
	if daemon.now().Before(pendingAt.Add(terminalGrace)) {
		return
	}
	daemon.releaseSlotOnce(key)
}

func (daemon *daemon) beginTerminal(key state.RunKey) func(bool) {
	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		return func(bool) {}
	}
	active.terminalizing++
	renewCancel := active.renewCancel
	active.renewCancel = nil
	active.renewCancelID++
	daemon.mu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	return func(entered bool) {
		daemon.mu.Lock()
		defer daemon.mu.Unlock()
		active := daemon.running[key]
		if active == nil {
			return
		}
		if active.terminalizing > 0 {
			active.terminalizing--
		}
		if entered {
			active.terminal = true
		}
	}
}

func (daemon *daemon) isCancelled(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active != nil && active.cancelled
}

func (daemon *daemon) isStale(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active != nil && active.stale
}

func (daemon *daemon) isTerminal(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active != nil && (active.terminal || active.terminalizing > 0)
}

func processDetails(process Process) (int, string, error) {
	pid, identity := process.ProcessDetails()
	if pid > 0 && identity != "" {
		return pid, identity, nil
	}
	return 0, "", errors.New("process does not expose a persistent identity")
}

func parseAdmissionInput(input json.RawMessage) (protocol.Admission, bool, error) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return protocol.Admission{}, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return protocol.Admission{}, true, fmt.Errorf("%w: %v", errInvalidAdmission, err)
	}
	if nested, present := fields["goal_admission"]; present {
		if len(bytes.TrimSpace(nested)) == 0 || bytes.Equal(bytes.TrimSpace(nested), []byte("null")) {
			return protocol.Admission{}, false, nil
		}
		admission, err := protocol.ParseAdmission(nested)
		if err == nil {
			return admission, true, nil
		}
		if admissionSchemaVersion(nested) == protocol.AdmissionSchemaVersion {
			return protocol.Admission{}, true, fmt.Errorf("%w: %v", errInvalidAdmission, err)
		}
		return protocol.Admission{}, false, nil
	}
	if admissionSchemaVersion(trimmed) != protocol.AdmissionSchemaVersion {
		return protocol.Admission{}, false, nil
	}
	admission, err := protocol.ParseAdmission(trimmed)
	if err != nil {
		return protocol.Admission{}, true, fmt.Errorf("%w: %v", errInvalidAdmission, err)
	}
	return admission, true, nil
}

func admissionSchemaVersion(input json.RawMessage) string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		return ""
	}
	var schemaVersion string
	if err := json.Unmarshal(object["schema_version"], &schemaVersion); err != nil {
		return ""
	}
	return schemaVersion
}

func admissionLaunchFailure(admission protocol.Admission, capabilities harness.Capabilities, providerAccess *protocol.ProviderAccess) error {
	// Observe admissions require an Integration-owned external-check receipt.
	// Current native adapters cannot produce that receipt, so reject before
	// workspace, launch-intent, or session-journal side effects.
	if admission.Purpose == protocol.AdmissionPurposeObserve {
		return taskResultFailure(protocol.TaskResultReasonUnsupportedVersion, fmt.Errorf("Goal admission purpose %q is unsupported without a verified external-check adapter", admission.Purpose))
	}
	// Handoff is intentionally a distinct mode. This daemon has no verified
	// cross-harness native-session transfer, so reject it before any workspace,
	// launch-intent, or native-process side effect can be created.
	if admission.SessionMode == protocol.SessionModeHandoff {
		return taskResultFailure(protocol.TaskResultReasonHandoffUnsupported, fmt.Errorf("%s: cross-harness Goal session handoff is not supported by this daemon", protocol.TaskResultReasonHandoffUnsupported))
	}
	if err := capabilities.Require(harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel); err != nil {
		return err
	}
	if providerAccess != nil {
		return &harness.CapabilityError{
			Kind:       capabilities.Kind,
			Capability: harness.CapabilityProviderAccess,
			Reason:     "native provider broker bridge is not verified",
		}
	}
	if admission.Limits.MaxCostMicrousd != nil {
		if err := capabilities.Require(harness.CapabilityHardCostLimit); err != nil {
			return err
		}
	}
	if admission.SessionMode != protocol.SessionModeFresh || admission.RequestedSessionID != nil {
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("%s: only fresh native Goal sessions are implemented", protocol.TaskResultReasonResumeRejected))
	}
	if admission.Limits.MaxTurns != 1 {
		return errors.New("Goal admission must request exactly one native turn")
	}
	return nil
}

// startGoalAdmission crosses the staged native-launch barriers in their only
// safe order. Every pre-turn failure either closes the known process or leaves
// the local session journal explicitly uncertain; neither path starts a second
// native session on retry.
func (daemon *daemon) startGoalAdmission(ctx context.Context, key state.RunKey, claim protocol.ClaimResponse, admission protocol.Admission, workspacePersisted func(workspace.Prepared)) (returnErr error) {
	nativeLaunchAttempted := false
	defer func() {
		if returnErr == nil || !nativeLaunchAttempted {
			return
		}
		if _, typed := taskResultFailureReason(returnErr); !typed {
			returnErr = taskResultFailure(protocol.TaskResultReasonUnknownOutcome, returnErr)
		}
	}()
	if admission.ModelProfile != daemon.config.Runtime.AgentProfile {
		return fmt.Errorf("Goal admission model_profile %q does not match configured runtime agent_profile %q", admission.ModelProfile, daemon.config.Runtime.AgentProfile)
	}
	if err := admissionLaunchFailure(admission, daemon.harnessCapabilities, claim.ProviderAccess); err != nil {
		return err
	}
	if admission.Subject.ResourceID != daemon.config.Runtime.RepositoryResourceID {
		return errors.New("Goal admission subject resource_id does not match the configured runtime repository_resource_id")
	}
	if daemon.harnessCapabilities.Kind != harness.KindCodex {
		return fmt.Errorf("Goal admission harness %q is unsupported", daemon.harnessCapabilities.Kind)
	}
	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return errors.New("control API does not implement Goal session admission")
	}
	if daemon.harnessRegistry == nil {
		return errors.New("native harness registry is unavailable")
	}
	deadline := parseAdmissionDeadline(admission.Limits.DeadlineAt)
	if deadline.IsZero() || !deadline.After(daemon.now()) {
		return errors.New("Goal admission deadline has elapsed before native launch")
	}
	adapter, err := daemon.harnessRegistry.Lookup(harness.KindCodex)
	if err != nil {
		return fmt.Errorf("lookup Codex adapter: %w", err)
	}
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	providerAccess, err := goalProviderAccess(profile, claim.ProviderAccess, daemon.config.ControlPlaneURL)
	if err != nil {
		return err
	}

	if _, err := daemon.store.MarkWorkspaceRecoveryRequired(key); err != nil {
		return fmt.Errorf("mark workspace recovery required: %w", err)
	}
	subjectWorkspace, ok := daemon.workspace.(goalSubjectWorkspace)
	if !ok {
		return errors.New("workspace service does not support immutable Goal subjects")
	}
	preparedSubject, err := subjectWorkspace.PrepareSubject(ctx, daemon.config.Runtime.Workspace, workspace.RunRef{RunID: key.RunID, Generation: key.Generation}, admission.Subject)
	if err != nil {
		return fmt.Errorf("prepare admitted Subject workspace: %w", err)
	}
	prepared := preparedSubject.Prepared
	if preparedSubject.Subject != admission.Subject {
		return errors.New("prepared Goal workspace does not match the admitted Subject")
	}
	if !daemon.persistWorkspacePath(ctx, key, prepared.Path) {
		return errors.New("persist workspace path")
	}
	if workspacePersisted != nil {
		workspacePersisted(prepared)
	}
	fingerprint, err := workspaceFingerprint(ctx, prepared)
	if err != nil {
		return fmt.Errorf("fingerprint workspace: %w", err)
	}
	localHandleID, err := state.NewGoalSessionLocalHandleID()
	if err != nil {
		return fmt.Errorf("generate local native session handle: %w", err)
	}
	launchIntentID, err := state.NewGoalSessionLocalHandleID()
	if err != nil {
		return fmt.Errorf("generate native launch intent: %w", err)
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: localHandleID}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID:         launchIntentID,
		GoalID:                 admission.GoalID,
		GoalRevision:           admission.GoalRevision,
		WorkItemID:             admissionWorkItemIDValue(admission.WorkItemID),
		TaskID:                 claim.TaskID,
		RunID:                  key.RunID,
		Generation:             key.Generation,
		AdmissionID:            admission.AdmissionID,
		LocalHandleID:          localHandleID,
		RuntimeID:              daemon.runtimeID,
		RuntimeEpoch:           daemon.runtimeEpoch,
		HarnessKind:            string(daemon.harnessCapabilities.Kind),
		HarnessVersion:         daemon.harnessCapabilities.NativeVersion,
		AdapterVersion:         daemon.harnessCapabilities.ImplementationVersion,
		AdapterProtocolVersion: daemon.harnessCapabilities.ProtocolVersion,
		WorkspaceFingerprint:   fingerprint,
		SessionMode:            state.GoalSessionModeFresh,
	}
	if _, err := daemon.store.SaveGoalSessionLaunchIntent(intent); err != nil {
		return fmt.Errorf("save Goal session launch intent: %w", err)
	}
	if _, err := daemon.store.MarkGoalSessionLaunchStarted(sessionKey, daemon.now()); err != nil {
		return fmt.Errorf("mark Goal session launch: %w", err)
	}
	if _, err := daemon.store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID:               admission.GoalID,
		LocalHandleID:        localHandleID,
		HarnessKind:          string(daemon.harnessCapabilities.Kind),
		HarnessVersion:       daemon.harnessCapabilities.NativeVersion,
		AdapterVersion:       daemon.harnessCapabilities.ImplementationVersion,
		WorkspaceFingerprint: fingerprint,
		// Control owns a logical workspace key. The absolute local worktree path
		// remains in the daemon journal and must not become control-plane identity.
		Workspace:            daemon.config.Runtime.Workspace,
		RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		_, _ = daemon.store.AbortGoalSessionLaunchBeforeNativeStart(sessionKey)
		return fmt.Errorf("queue native Goal session attach intent: %w", err)
	}

	environment, err := execution.BuildEnvironment(profile.EnvAllowlist...)
	if err != nil {
		_, _ = daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID)
		_, _ = daemon.store.AbortGoalSessionLaunchBeforeNativeStart(sessionKey)
		return fmt.Errorf("build native environment: %w", err)
	}
	sink := harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		return daemon.queueNativeEvent(key, event)
	})
	session, err := adapter.Start(ctx, harness.StartRequest{
		AdmissionID:   admission.AdmissionID,
		LocalHandleID: localHandleID,
		Workspace:     prepared.Path,
		Goal:          claim.Work.Goal,
		ModelProfile:  admission.ModelProfile,
		Limits: harness.Limits{
			MaxTurns:        int(admission.Limits.MaxTurns),
			DeadlineAt:      parseAdmissionDeadline(admission.Limits.DeadlineAt),
			MaxCostMicrousd: admission.Limits.MaxCostMicrousd,
		},
		ProviderAccess: providerAccess,
		Invocation:     execution.Invocation{Program: profile.Command, Args: profile.Args, Dir: prepared.Path, Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if daemon.options.recordProcess != nil {
				_, err := daemon.options.recordProcess(key, pid, identity, daemon.now())
				return err
			}
			_, err := daemon.store.SetProcessDetails(key, pid, identity, daemon.now())
			return err
		},
	}, sink)
	nativeLaunchAttempted = true
	if err != nil {
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if _, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID); discardErr != nil {
			return fmt.Errorf("discard unsent native Goal session attach intent: %w", errors.Join(err, discardErr))
		}
		_, _ = daemon.store.MarkGoalSessionUncertain(sessionKey, "native transport start outcome is unknown: "+errorText(err))
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("start native Goal session: %w", err))
	}
	staged, ok := session.(harness.StagedSession)
	if !ok {
		if _, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID); discardErr != nil {
			return fmt.Errorf("discard unsent native Goal session attach intent: %w", discardErr)
		}
		cause := errors.New("native Goal adapter does not implement staged session launch")
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if abandonErr := daemon.abandonGoalSession(sessionKey, session, false, cause); abandonErr != nil {
			return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, abandonErr))
		}
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, cause)
	}
	if err := daemon.attachNativeSession(key, session, sessionKey, admission, prepared, deadline); err != nil {
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if _, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID); discardErr != nil {
			return fmt.Errorf("discard unsent native Goal session attach intent: %w", errors.Join(err, discardErr))
		}
		daemon.abandonGoalSession(sessionKey, session, false, err)
		return err
	}

	operationContext, operationCancel := context.WithDeadline(ctx, deadline)
	defer operationCancel()
	handle, err := staged.Open(operationContext)
	if err != nil {
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if _, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID); discardErr != nil {
			return fmt.Errorf("discard unsent native Goal session attach intent: %w", errors.Join(err, discardErr))
		}
		daemon.abandonGoalSession(sessionKey, session, false, err)
		return fmt.Errorf("open native Goal session: %w", err)
	}
	if _, err := daemon.store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: handle.ID, NativeSessionFilename: handle.Filename}); err != nil {
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if _, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID); discardErr != nil {
			return fmt.Errorf("discard unsent native Goal session attach intent: %w", errors.Join(err, discardErr))
		}
		daemon.abandonGoalSession(sessionKey, session, false, err)
		return fmt.Errorf("persist native Goal session handle: %w", err)
	}
	attached := true
	if _, err := daemon.markGoalSessionAttachDeliveryReady(key, localHandleID); err != nil {
		discardErr := daemon.discardUnreadyGoalSessionAttachDelivery(key, localHandleID)
		abandonErr := daemon.abandonGoalSession(sessionKey, session, attached, err)
		if abandonErr != nil {
			daemon.requireNativeSessionCloseRetry(key, session)
		}
		return fmt.Errorf("mark native Goal session attach delivery ready: %w", errors.Join(err, discardErr, abandonErr))
	}

	runJournal, err := daemon.store.LoadJournal(key)
	if err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return fmt.Errorf("load native run fence: %w", err)
	}
	var receipt *control.GoalSessionReceipt
	runJournal, receipt, err = daemon.deliverGoalDelivery(operationContext, runJournal, state.GoalDeliverySessionAttach, localHandleID, func(receipt control.GoalSessionReceipt) error {
		return validateGoalSessionReceipt(admission, claim, key, localHandleID, receipt)
	})
	if err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return fmt.Errorf("attach native Goal session: %w", err)
	}
	if receipt == nil {
		daemon.abandonGoalSession(sessionKey, session, attached, errors.New("native Goal session attach receipt is unavailable"))
		return errors.New("native Goal session attach receipt is unavailable")
	}
	requestContext, cancel := daemon.controlContext(operationContext)
	canonical, err := goalAPI.FetchRunContext(requestContext, key.RunID, runJournal.Fence())
	cancel()
	if err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return fmt.Errorf("fetch Goal run context: %w", err)
	}
	if err := validateGoalRunContext(admission, claim, key, *receipt, canonical); err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return err
	}
	canonicalJSON, err := json.Marshal(canonical.Context)
	if err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return fmt.Errorf("encode canonical Goal context: %w", err)
	}
	if err := daemon.markRunning(key); err != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, err)
		return fmt.Errorf("mark native Goal run running: %w", err)
	}
	daemon.mu.Lock()
	active := daemon.running[key]
	if active != nil {
		active.nativeSession = session
		active.goalSession = &sessionKey
		admissionCopy := admission
		active.goalAdmission = &admissionCopy
		active.prepared = prepared
	}
	daemon.mu.Unlock()
	if active == nil || daemon.isCancelled(key) || ctx.Err() != nil {
		daemon.abandonGoalSession(sessionKey, session, attached, errors.New("Goal run was cancelled before native turn start"))
		return errors.New("Goal run was cancelled before native turn start")
	}
	if err := staged.StartTurn(operationContext, harness.TurnRequest{Goal: claim.Work.Goal, Context: canonicalJSON}); err != nil {
		if abandonErr := daemon.abandonGoalSessionUncertain(sessionKey, session, err); abandonErr != nil {
			return fmt.Errorf("start native Goal turn: %w", errors.Join(err, abandonErr))
		}
		return fmt.Errorf("start native Goal turn: %w", err)
	}
	if daemon.finishAttachedStart(key) {
		daemon.signalOutbox()
	}
	return nil
}

// goalProviderAccess is intentionally separate from legacy initialInput: a
// native harness must receive the broker capability out of band rather than
// through a persisted prompt or process environment.
func goalProviderAccess(profile config.AgentProfile, access *protocol.ProviderAccess, controlPlaneURL string) (*protocol.ProviderAccess, error) {
	if access == nil {
		return nil, nil
	}
	if !profile.ProviderAccess {
		return nil, errors.New("agent profile does not allow provider access")
	}
	return resolveProviderAccess(controlPlaneURL, access)
}

func parseAdmissionDeadline(value string) time.Time {
	deadline, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return deadline.UTC()
}

func (daemon *daemon) attachNativeSession(key state.RunKey, session harness.Session, sessionKey state.GoalSessionKey, admission protocol.Admission, prepared workspace.Prepared, deadline time.Time) error {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil {
		return errors.New("Goal run is no longer active")
	}
	active.nativeSession = session
	active.goalSession = &sessionKey
	admissionCopy := admission
	active.goalAdmission = &admissionCopy
	active.nativeDeadline = deadline
	active.prepared = prepared
	return nil
}

func (daemon *daemon) clearNativeSession(key state.RunKey, session harness.Session) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if active := daemon.running[key]; active != nil && active.nativeSession == session {
		active.nativeSession = nil
		active.goalSession = nil
		active.goalAdmission = nil
		active.nativeDeadline = time.Time{}
	}
}

func (daemon *daemon) clearNativeSessionByValue(session harness.Session) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for _, active := range daemon.running {
		if active.nativeSession == session {
			active.nativeSession = nil
			active.goalSession = nil
			active.goalAdmission = nil
			active.nativeDeadline = time.Time{}
		}
	}
}

func (daemon *daemon) requireNativeSessionCloseRetry(key state.RunKey, session harness.Session) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if active := daemon.running[key]; active != nil && active.nativeSession == session {
		active.nativeCloseRetryRequired = true
		active.cleanupBlocked = true
	}
}

func (daemon *daemon) markGoalSessionAttachDeliveryReady(key state.RunKey, localHandleID string) (state.RunJournal, error) {
	mark := daemon.options.markGoalSessionAttachDeliveryReady
	if mark == nil {
		mark = daemon.store.MarkGoalSessionAttachDeliveryReady
	}
	journal, err := mark(key, localHandleID)
	if err == nil {
		return journal, nil
	}

	// A failed directory sync can follow a visible atomic rename. Read back the
	// journal before treating the attachment as unready so we never discard a
	// receipt that is already eligible for exact replay.
	observed, readErr := daemon.store.LoadJournal(key)
	if readErr == nil {
		for _, delivery := range observed.PendingGoalDeliveries {
			if delivery.Kind == state.GoalDeliverySessionAttach && delivery.DeliveryID == localHandleID && delivery.Ready {
				return observed, nil
			}
		}
	}
	if readErr != nil && !state.IsNotFound(readErr) {
		err = errors.Join(err, fmt.Errorf("read Goal session attach delivery after ready failure: %w", readErr))
	}
	return journal, err
}

func (daemon *daemon) discardUnreadyGoalSessionAttachDelivery(key state.RunKey, localHandleID string) error {
	discard := daemon.options.discardUnreadyGoalSessionAttachDelivery
	if discard == nil {
		discard = daemon.store.DiscardUnreadyGoalSessionAttachDelivery
	}
	_, err := discard(key, localHandleID)
	return err
}

// retryUnreadyGoalSessionAttachDeliveries clears only pre-send receipts once
// their starter has finished. It covers a transient failure while compensating
// a rejected Ready barrier without ever promoting an unready receipt.
func (daemon *daemon) retryUnreadyGoalSessionAttachDeliveries() {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	for _, journal := range journals {
		if daemon.isStarting(journal.Key()) {
			continue
		}
		for _, delivery := range journal.PendingGoalDeliveries {
			if delivery.Kind != state.GoalDeliverySessionAttach || delivery.Ready {
				continue
			}
			if err := daemon.discardUnreadyGoalSessionAttachDelivery(journal.Key(), delivery.DeliveryID); err != nil && daemon.log != nil {
				daemon.log.Warn("discard_unready_goal_session_attach_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
		}
	}
}

type blockedNativeSession struct {
	key     state.RunKey
	active  *runningRun
	session harness.Session
}

// retryBlockedNativeSessionCloses keeps cleanup owned by this daemon when a
// failed Ready barrier also encounters a bounded native Close failure. The
// durable session remains uncertain until Close and its process-marker cleanup
// both succeed.
func (daemon *daemon) retryBlockedNativeSessionCloses() {
	blocked := make([]blockedNativeSession, 0)
	daemon.mu.Lock()
	for key, active := range daemon.running {
		if active.nativeSession == nil || active.goalSession == nil || !active.nativeCloseRetryRequired || active.nativeCloseRetrying {
			continue
		}
		active.nativeCloseRetrying = true
		blocked = append(blocked, blockedNativeSession{key: key, active: active, session: active.nativeSession})
	}
	daemon.mu.Unlock()

	for _, candidate := range blocked {
		err := daemon.closeNativeGoalSession(candidate.key, candidate.active, candidate.session)
		if err == nil {
			daemon.clearNativeSession(candidate.key, candidate.session)
			daemon.mu.Lock()
			if daemon.running[candidate.key] == candidate.active {
				candidate.active.cleanupBlocked = false
				candidate.active.nativeCloseRetryRequired = false
			}
			daemon.mu.Unlock()
			daemon.signalOutboxFor(candidate.key)
		} else if daemon.log != nil {
			daemon.log.Warn("retry_native_goal_session_close_failed", "run_id", candidate.key.RunID, "generation", candidate.key.Generation, "error", err)
		}
		daemon.mu.Lock()
		if daemon.running[candidate.key] == candidate.active {
			candidate.active.nativeCloseRetrying = false
		}
		daemon.mu.Unlock()
	}
}

func (daemon *daemon) abandonGoalSession(key state.GoalSessionKey, session harness.Session, attached bool, cause error) error {
	closeContext, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
	closeErr := session.Close(closeContext)
	cancel()
	nativeCloseErr := closeErr
	if closeErr == nil {
		daemon.clearNativeSessionByValue(session)
	}
	var runKey state.RunKey
	if sessionJournal, err := daemon.store.LoadGoalSession(key); err == nil {
		runKey = state.RunKey{RunID: sessionJournal.RunID, Generation: sessionJournal.Generation}
	} else if !state.IsNotFound(err) {
		closeErr = errors.Join(closeErr, fmt.Errorf("load Goal session after close: %w", err))
	}
	if closeErr == nil && attached {
		if _, err := daemon.store.CloseGoalSession(key); err != nil {
			closeErr = err
		}
	}
	if closeErr == nil && runKey.RunID != "" && runKey.Generation > 0 {
		if err := daemon.clearNativeProcessDetails(runKey); err != nil {
			closeErr = err
		}
	}
	if nativeCloseErr != nil && runKey.RunID != "" && runKey.Generation > 0 {
		daemon.requireNativeSessionCloseRetry(runKey, session)
	}
	if closeErr == nil && attached {
		return nil
	}
	reason := "native Goal session did not reach a known stopped state"
	if cause != nil {
		reason += ": " + cause.Error()
	}
	if closeErr != nil {
		reason += "; close: " + closeErr.Error()
	}
	var retentionErr error
	if closeErr != nil && runKey.RunID != "" && runKey.Generation > 0 {
		retentionErr = daemon.retainUnknownGoalLaunchWorkspaceChecked(runKey)
	}
	_, uncertainErr := daemon.store.MarkGoalSessionUncertain(key, reason)
	return errors.Join(closeErr, retentionErr, uncertainErr)
}

func (daemon *daemon) abandonGoalSessionUncertain(key state.GoalSessionKey, session harness.Session, cause error) error {
	closeContext, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
	closeErr := session.Close(closeContext)
	cancel()
	nativeCloseErr := closeErr
	if closeErr == nil {
		daemon.clearNativeSessionByValue(session)
	}
	sessionJournal, sessionErr := daemon.store.LoadGoalSession(key)
	if sessionErr != nil {
		return errors.Join(closeErr, fmt.Errorf("load Goal session after uncertain close: %w", sessionErr))
	}
	runKey := state.RunKey{RunID: sessionJournal.RunID, Generation: sessionJournal.Generation}
	var clearErr error
	if closeErr == nil {
		clearErr = daemon.clearNativeProcessDetails(runKey)
	} else if nativeCloseErr != nil {
		daemon.requireNativeSessionCloseRetry(runKey, session)
	}
	reason := "native Goal turn start outcome is unknown"
	if cause != nil {
		reason += ": " + cause.Error()
	}
	if closeErr != nil {
		reason += "; close: " + closeErr.Error()
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(runKey)
	_, uncertainErr := daemon.store.MarkGoalSessionUncertain(key, reason)
	return errors.Join(closeErr, clearErr, retentionErr, uncertainErr)
}

// retainUnknownGoalLaunchWorkspace prevents cleanup from deleting a worktree
// that an unconfirmed native transport may still be modifying.
func (daemon *daemon) retainUnknownGoalLaunchWorkspace(key state.RunKey) {
	if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil && daemon.log != nil {
		daemon.log.Error("retain_unknown_goal_workspace_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
	}
}

func (daemon *daemon) retainUnknownGoalLaunchWorkspaceChecked(key state.RunKey) error {
	daemon.rememberWorkspaceRetention(key)
	retain := daemon.options.retainWorkspace
	if retain == nil {
		retain = daemon.store.RetainWorkspace
	}
	if _, err := retain(key); err != nil {
		return fmt.Errorf("persist workspace retention: %w", err)
	}
	return nil
}

func validateGoalSessionReceipt(admission protocol.Admission, claim protocol.ClaimResponse, key state.RunKey, localHandleID string, receipt control.GoalSessionReceipt) error {
	if receipt.GoalID != admission.GoalID || receipt.TaskID != claim.TaskID || receipt.RunID != key.RunID || receipt.ActiveRunID != key.RunID || receipt.LocalHandleID != localHandleID {
		return errors.New("attached Goal session receipt does not match admission and claim")
	}
	if receipt.RepositoryResourceID != admission.Subject.ResourceID || receipt.State != state.GoalSessionStateBusy {
		return errors.New("attached Goal session receipt has incompatible repository or state")
	}
	return nil
}

func validateGoalRunContext(admission protocol.Admission, claim protocol.ClaimResponse, key state.RunKey, receipt control.GoalSessionReceipt, runContext control.GoalRunContext) error {
	context := runContext.Context
	if runContext.GoalID != admission.GoalID || runContext.TaskID != claim.TaskID || runContext.RunID != key.RunID || runContext.Generation != key.Generation || runContext.SessionID == nil || *runContext.SessionID != receipt.ID {
		return errors.New("Goal run context does not match admission, claim, or attached session")
	}
	if context.GoalID != admission.GoalID || context.GoalRevision != admission.GoalRevision ||
		!equalOptionalGoalString(context.WorkItemID, admission.WorkItemID) ||
		context.WorkContract.Purpose != string(admission.Purpose) ||
		context.SnapshotID != admission.ContextSnapshotID || context.ContentHash != admission.ContextHash || context.Subject != admission.Subject {
		return errors.New("canonical Goal context does not match admission")
	}
	return nil
}

func admissionWorkItemIDValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func equalOptionalGoalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (daemon *daemon) queueNativeEvent(key state.RunKey, event harness.Event) error {
	at := event.At
	if at.IsZero() {
		at = daemon.now()
	}
	if event.Kind == harness.EventUsageObserved {
		if observation, ok := parseNativeUsageSnapshot(event.Payload); ok {
			observation.ObservedAt = at.UTC()
			journal, observationErr := daemon.store.RecordNativeUsageObservation(key, observation)
			if observationErr != nil {
				return observationErr
			}
			daemon.mu.Lock()
			if active := daemon.running[key]; active != nil && active.goalSession != nil {
				retained := journal.NativeUsageObservation
				if retained != nil {
					retainedCopy := *retained
					active.goalUsageObservation = &retainedCopy
					usage := usageFromNativeObservation(retainedCopy)
					active.goalUsage = &usage
					active.goalUsageAt = retainedCopy.ObservedAt.UTC()
					active.goalCachedInputTokens = retainedCopy.CachedInputTokens
				}
				active.goalUsageHasCached = true
			}
			daemon.mu.Unlock()
		}
	}
	payload := event.Payload
	if len(payload) == 0 || !json.Valid(payload) {
		encoded, err := json.Marshal(map[string]any{
			"native_kind": string(event.Kind), "code": event.Code, "message": event.Message,
			"data": string(event.Data), "diagnostic": event.Diagnostic,
		})
		if err != nil {
			return err
		}
		payload = encoded
	}
	return daemon.queueEvent(key, string(event.Kind), payload, at)
}

type nativeUsageSnapshot struct {
	Total *nativeUsageCounters `json:"total"`
}

type nativeUsageCounters struct {
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	InputTokens           *int64 `json:"input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
	TotalTokens           *int64 `json:"total_tokens"`
}

func parseNativeUsageSnapshot(payload json.RawMessage) (state.NativeUsageObservation, bool) {
	if len(payload) == 0 {
		return state.NativeUsageObservation{}, false
	}
	var snapshot nativeUsageSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil ||
		snapshot.Total == nil || snapshot.Total.CachedInputTokens == nil || snapshot.Total.InputTokens == nil ||
		snapshot.Total.OutputTokens == nil || snapshot.Total.ReasoningOutputTokens == nil || snapshot.Total.TotalTokens == nil ||
		*snapshot.Total.CachedInputTokens < 0 || *snapshot.Total.InputTokens < 0 || *snapshot.Total.OutputTokens < 0 ||
		*snapshot.Total.ReasoningOutputTokens < 0 || *snapshot.Total.TotalTokens < 0 {
		return state.NativeUsageObservation{}, false
	}
	return state.NativeUsageObservation{
		SchemaVersion:         state.NativeUsageObservationSchemaVersion,
		InputTokens:           *snapshot.Total.InputTokens,
		OutputTokens:          *snapshot.Total.OutputTokens,
		CachedInputTokens:     *snapshot.Total.CachedInputTokens,
		ReasoningOutputTokens: *snapshot.Total.ReasoningOutputTokens,
		TotalTokens:           *snapshot.Total.TotalTokens,
	}, true
}

func usageFromNativeObservation(observation state.NativeUsageObservation) harness.Usage {
	return harness.Usage{
		State:        harness.UsageUnknown,
		InputTokens:  observation.InputTokens,
		OutputTokens: observation.OutputTokens,
		CostMicrousd: "",
	}
}

const nativeGoalUsageKey = "native-final"

func (daemon *daemon) queueNativeGoalUsage(ctx context.Context, key state.RunKey, result *harness.TaskResult) error {
	usage, shouldQueue, err := daemon.prepareNativeGoalUsage(key, result)
	if err != nil || !shouldQueue {
		return err
	}
	return daemon.queueNativeGoalUsageRecord(key, usage)
}

func (daemon *daemon) prepareNativeGoalUsage(key state.RunKey, result *harness.TaskResult) (protocol.Usage, bool, error) {
	if !canonicalGoalRunID(key.RunID) {
		// Legacy/unit fixtures may use descriptive run IDs. Goal transport IDs
		// are UUIDs in production; do not let a compatibility fixture invent a
		// malformed accounting receipt.
		return protocol.Usage{}, false, nil
	}
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return protocol.Usage{}, false, nil
	}
	if err != nil {
		return protocol.Usage{}, false, err
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == state.GoalDeliveryUsage && retired.Delivery.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}
	for _, delivered := range journal.DeliveredGoalDeliveries {
		if delivered.Kind == state.GoalDeliveryUsage && delivered.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}

	observed := harness.Usage{}
	hasSnapshot := false
	observedAt := daemon.now().UTC()
	cachedInputTokens := int64(0)
	hasCachedInputTokens := false
	if journal.NativeUsageObservation != nil {
		observation := *journal.NativeUsageObservation
		observed = usageFromNativeObservation(observation)
		cachedInputTokens = observation.CachedInputTokens
		hasCachedInputTokens = true
		hasSnapshot = true
		observedAt = observation.ObservedAt.UTC()
	}
	daemon.mu.Lock()
	if !hasSnapshot {
		if active := daemon.running[key]; active != nil && active.goalUsageObservation != nil {
			observation := *active.goalUsageObservation
			observed = usageFromNativeObservation(observation)
			cachedInputTokens = observation.CachedInputTokens
			hasCachedInputTokens = true
			hasSnapshot = true
			observedAt = observation.ObservedAt.UTC()
		} else if active := daemon.running[key]; active != nil && active.goalUsage != nil {
			observed = *active.goalUsage
			hasSnapshot = true
			cachedInputTokens = active.goalCachedInputTokens
			hasCachedInputTokens = active.goalUsageHasCached
			if !active.goalUsageAt.IsZero() {
				observedAt = active.goalUsageAt.UTC()
			}
		}
	}
	daemon.mu.Unlock()
	if result != nil && !hasSnapshot && result.Usage.State != "" {
		observed = result.Usage
		hasSnapshot = result.Usage.State != harness.UsageUnknown
	}

	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	provider := strings.TrimSpace(profile.NativeModelProvider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(profile.NativeModel)
	if model == "" {
		model = "unknown"
	}
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      provider,
		Model:         model,
		CostBasis:     protocol.CostUnknown,
		ObservedAt:    observedAt.Format(time.RFC3339),
	}
	if hasSnapshot {
		inputTokens := observed.InputTokens
		outputTokens := observed.OutputTokens
		usage.InputTokens = &inputTokens
		usage.OutputTokens = &outputTokens
		if hasCachedInputTokens {
			usage.CachedInputTokens = &cachedInputTokens
		}
		if (observed.State == harness.UsageReported || observed.State == harness.UsageEstimated) && observed.CostMicrousd != "" && protocol.ValidateMicroUSD(observed.CostMicrousd) == nil {
			cost := observed.CostMicrousd
			usage.CostMicrousd = &cost
			if observed.State == harness.UsageReported {
				usage.CostBasis = protocol.CostReported
			} else {
				usage.CostBasis = protocol.CostEstimated
			}
		}
	}

	if usage.UsageID == "" {
		id, idErr := state.NewDaemonInstanceID()
		if idErr != nil {
			return protocol.Usage{}, false, idErr
		}
		usage.UsageID = id
	}
	return usage, true, nil
}

func (daemon *daemon) queueNativeGoalUsageRecord(key state.RunKey, usage protocol.Usage) error {
	queue := daemon.options.queueGoalUsage
	if queue == nil {
		queue = daemon.store.QueueGoalUsage
	}
	if _, err := queue(key, usage); err != nil {
		return err
	}
	daemon.signalOutboxFor(key)
	return nil
}

func canonicalGoalRunID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func initialInput(profile config.AgentProfile, work protocol.Work, providerAccess *protocol.ProviderAccess, controlPlaneURL string) ([]byte, error) {
	if providerAccess != nil && !profile.ProviderAccess {
		return nil, errors.New("agent profile does not allow provider access")
	}
	if profile.InputMode == config.InputModeGoal {
		return append([]byte(work.Goal), '\n'), nil
	}
	resolved, err := resolveProviderAccess(controlPlaneURL, providerAccess)
	if err != nil {
		return nil, err
	}
	if !profile.SupervisoryControl {
		return inputRecord(protocol.AgentInputRecordTaskInput, work.Goal, work.Input, resolved)
	}
	encoded, err := json.Marshal(protocol.AgentInputRecord{
		Type: protocol.AgentInputRecordTaskInput, Goal: work.Goal, Input: work.Input, ProviderAccess: resolved,
		Autonomy: &protocol.AutonomyPolicy{
			Mode:              "high",
			EscalationReasons: []string{"blocked", "consequential", "irreversible", "security", "business_policy", "expensive", "product_change"},
			RoutineDecisions:  "Proceed autonomously with implementation choices, tool selection, recoverable errors, and temporary uncertainty; do not request routine approvals.",
			ControlBoundary:   "Apply guidance, pause, and resume only at a safe boundary after the current atomic operation. Pause retains process and workspace and stops further autonomous progress until resume. Preserve the original goal.",
			Acknowledgement:   "After applying a control emit command_applied with its command_id, kind, and applied/rejected/failed outcome. Stdin receipt is not application. Consequential decisions require a concise waiting_for_input decision packet.",
		},
	})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func resolveProviderAccess(controlPlaneURL string, access *protocol.ProviderAccess) (*protocol.ProviderAccess, error) {
	if access == nil {
		return nil, nil
	}
	if access.Path != "/api/v1/provider-actions" {
		return nil, errors.New("provider access path is invalid")
	}
	base, err := url.Parse(controlPlaneURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("control plane URL is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if base.Path == "" {
		base.Path = "/api"
	}
	base.Path += strings.TrimPrefix(access.Path, "/api")
	base.RawPath = ""
	resolved := *access
	resolved.Path = base.String()
	return &resolved, nil
}

func inputRecord(recordType protocol.AgentInputRecordType, goal string, input json.RawMessage, providerAccess *protocol.ProviderAccess) ([]byte, error) {
	encoded, err := json.Marshal(protocol.AgentInputRecord{Type: recordType, Goal: goal, Input: input, ProviderAccess: providerAccess})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func canonicalInputDigest(payload json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

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
	_, err = daemon.store.QueueNextEvent(key, protocol.RunEvent{EventID: id, Kind: kind, OccurredAt: at, Payload: payload})
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
}

func (daemon *daemon) writeInputBounded(ctx context.Context, active *runningRun, process Process, input []byte) error {
	result := make(chan error, 1)
	go func() {
		result <- process.WriteInput(input)
	}()
	timer := daemon.timer(inputWriteTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.Chan():
		var stopExecution context.CancelCauseFunc
		var key state.RunKey
		daemon.mu.Lock()
		stopExecution = active.stopExecution
		for candidate, current := range daemon.running {
			if current == active {
				key = candidate
				break
			}
		}
		daemon.mu.Unlock()
		if daemon.log != nil {
			daemon.log.Warn("input_write_timeout", "run_id", key.RunID, "generation", key.Generation, "bytes", len(input))
		}
		if stopExecution != nil {
			stopExecution(errInputWriteTimeout)
		}
		return errInputWriteTimeout
	}
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

func (daemon *daemon) queueTerminalTransition(key state.RunKey, stateName string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	journal, err := daemon.store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded}, enteredAt)
	finishTerminal(err == nil)
	if err != nil {
		return err
	}
	daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
	daemon.signalOutbox()
	return nil
}

func (daemon *daemon) queueTerminalTransitionWithRetry(ctx context.Context, key state.RunKey, stateName string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	id, err := daemon.newIDWithRetry(rootContext, key, "", "terminal transition")
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()
	for {
		transition := protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded}
		var (
			journal  state.RunJournal
			queueErr error
		)
		if daemon.options.queueTerminalTransition != nil {
			journal, queueErr = daemon.options.queueTerminalTransition(key, transition, enteredAt)
		} else {
			journal, queueErr = daemon.store.QueueTerminalTransitionAt(key, transition, enteredAt)
		}
		if queueErr == nil {
			persisted = true
			daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
			daemon.signalOutbox()
			return nil
		}
		if existing, loadErr := daemon.store.LoadJournal(key); loadErr == nil && (existing.LocalState == "cleanup_pending" || (existing.LocalState == "terminal_pending" && existing.TerminalVerdict != "")) {
			return nil
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_terminal_transition_failed", "run_id", key.RunID, "generation", key.Generation, "state", stateName, "error", queueErr)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return rootContext.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) queueCancelledTerminalAndAcknowledgement(key state.RunKey, commandID string) error {
	return daemon.queueCancelledTerminalAndAcknowledgementWithContext(context.Background(), key, commandID)
}

func (daemon *daemon) queueCancelledTerminalAndAcknowledgementWithContext(ctx context.Context, key state.RunKey, commandID string) error {
	rootContext := daemon.rootContext(ctx)
	if daemon.commandAcknowledgementRetired(key) {
		return errors.New("command acknowledgement is no longer deliverable")
	}
	transitionID, err := daemon.newIDWithRetry(rootContext, key, commandID, "transition")
	if err != nil {
		return err
	}
	acknowledgementID, err := daemon.newIDWithRetry(rootContext, key, commandID, "acknowledgement")
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{})
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()
	transition := protocol.StateTransitionRequest{TransitionID: transitionID, State: "cancelled", Payload: payload}
	acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: commandID, Outcome: "applied", AckID: acknowledgementID}
	for {
		var journal state.RunJournal
		if daemon.options.queueCancelledTransitionAndAcknowledgement != nil {
			journal, err = daemon.options.queueCancelledTransitionAndAcknowledgement(key, transition, acknowledgement, enteredAt)
		} else {
			journal, err = daemon.store.QueueCancelledTransitionAndAcknowledgementAt(key, transition, acknowledgement, enteredAt)
		}
		if err == nil {
			persisted = true
			daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
			daemon.signalOutbox()
			return nil
		}
		if daemon.commandAcknowledgementRetired(key) {
			return errors.New("command acknowledgement is no longer deliverable")
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_cancelled_transition_and_acknowledgement_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return rootContext.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) waitForRun(key state.RunKey) {
	daemon.waitForRunWithContext(context.Background(), key)
}

func (daemon *daemon) waitForRunWithContext(ctx context.Context, key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	process := Process(nil)
	nativeSession := harness.Session(nil)
	output := (*agentOutput)(nil)
	if active != nil {
		process = active.process
		nativeSession = active.nativeSession
		output = active.output
	}
	daemon.mu.Unlock()
	if active == nil {
		return
	}
	if nativeSession != nil {
		daemon.waitForNativeRun(ctx, key, active, nativeSession)
		return
	}
	if process == nil {
		return
	}
	result := process.Wait()
	if output != nil {
		// Cancellation may stop delivery before Runner stores its sink error.
		// The cancellation cause was set first and survives that race.
		if output.executionContext != nil {
			if cause := context.Cause(output.executionContext); errors.Is(cause, errRequiredDecisionPacket) || errors.Is(cause, errInputWriteTimeout) {
				result.SinkError = cause
			}
		}
		if err := output.flush(daemon, key, daemon.now()); err != nil && result.SinkError == nil {
			result.SinkError = err
		}
	}
	active.inputMu.Lock()
	daemon.mu.Lock()
	cancelled := active.cancelled
	stale := active.stale
	startFailure := active.startFailure
	active.cleanupBlocked = false
	daemon.mu.Unlock()
	if stale {
		active.inputMu.Unlock()
		journal, err := daemon.store.SetLocalState(key, "stale")
		daemon.releaseRun(key)
		if err == nil {
			_ = daemon.scheduleCleanup(context.Background(), journal)
		}
		return
	}
	if !cancelled {
		if startFailure != nil {
			result.WaitError = errors.Join(startFailure, result.WaitError)
		}
		if err := daemon.supervisedExitFailure(key); err != nil && result.WaitError == nil {
			result.WaitError = err
		}
		if result.Success() {
			if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "completed", map[string]any{"exit_code": result.ExitCode}); err != nil {
				daemon.log.Error("queue_completed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
		} else {
			cause := result.WaitError
			if result.SinkError != nil {
				cause = result.SinkError
			}
			if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", map[string]any{
				"exit_code": result.ExitCode,
				"reason":    string(canonicalTaskResultReason(cause, nil, protocol.TaskResultReasonProcessFailure)),
				"summary":   "agent process exited unsuccessfully",
				"error":     errorText(cause),
			}); err != nil {
				daemon.log.Error("queue_failed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "stage", "process_exit", "error", err)
			}
		}
	}
	active.inputMu.Unlock()
	daemon.releaseCleanupAfterProcessExit(key)
}

// watchFailedStartProcess restores the usual exit cleanup when a Process was
// exposed together with a Start error. It is deliberately outside workers: a
// stuck Process must not make daemon shutdown wait forever. Its process marker
// was saved before this watcher is started, so restart recovery retains the
// authority to stop an unresolved child.
func (daemon *daemon) watchFailedStartProcess(ctx context.Context, key state.RunKey, process Process) {
	done := make(chan struct{}, 1)
	go func() {
		_ = process.Wait()
		done <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		return
	case <-done:
		if ctx.Err() == nil {
			daemon.waitForRunWithContext(context.Background(), key)
		}
	}
}

// waitForNativeRun deliberately treats a native TaskResult, not app-server
// process exit, as terminal execution truth. A successful terminal transition
// settles the bounded Task only; the semantic result remains a proposal and
// cannot accept a Goal outcome by itself.
func (daemon *daemon) waitForNativeRun(ctx context.Context, key state.RunKey, active *runningRun, session harness.Session) {
	staged, isStaged := session.(harness.StagedSession)
	waitContext := ctx
	stopWaitDeadline := func() {}
	if !active.nativeDeadline.IsZero() {
		waitContext, stopWaitDeadline = context.WithDeadline(ctx, active.nativeDeadline)
	}
	var result harness.TaskResult
	var waitErr error
	if isStaged {
		// A retained app-server stays alive after turn/completed. WaitTurn is the
		// close permission only; it must never expose an early TaskResult.
		waitErr = staged.WaitTurn(waitContext)
	} else {
		// Legacy/non-staged sessions retain the original physical Wait behavior.
		result, waitErr = session.Wait(waitContext)
	}
	stopWaitDeadline()
	if !isStaged && errors.Is(waitErr, context.DeadlineExceeded) {
		// The session remains live because its process context is not the wait
		// deadline. Request native interruption before Close performs the final
		// process stop, so an elapsed admission cannot keep making progress.
		interruptContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, interruptErr := session.Control(interruptContext, harness.ControlRequest{Kind: harness.ControlCancel})
		cancel()
		if interruptErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("interrupt native turn at deadline: %w", interruptErr))
		}
	}
	closeErr := daemon.closeNativeGoalSession(key, active, session)
	if isStaged {
		// Close joins the adapter watcher, but retain a bounded final Wait as a
		// second barrier for deferred interrupt classification and process truth.
		finalContext, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
		finalResult, finalWaitErr := session.Wait(finalContext)
		cancel()
		if finalWaitErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("wait native final result: %w", finalWaitErr))
		} else {
			result = finalResult
		}
	}

	daemon.mu.Lock()
	admission := active.goalAdmission
	prepared := active.prepared
	daemon.mu.Unlock()
	if waitErr == nil && closeErr == nil {
		if err := daemon.verifyNativeTaskResultSubject(ctx, prepared, admission, &result); err != nil {
			waitErr = err
		}
	}
	usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, &result)
	if usageErr != nil {
		daemon.deferNativeUsageRetry(key, active, result, waitErr, closeErr, nil, true, usageErr)
		return
	}
	if shouldQueue {
		daemon.rememberNativeFinalUsage(key, active, usage)
		if usageErr = daemon.queueNativeGoalUsageRecord(key, usage); usageErr != nil {
			daemon.deferNativeUsageRetry(key, active, result, waitErr, closeErr, &usage, false, usageErr)
			return
		}
	}
	daemon.completeNativeRunAfterUsage(ctx, key, active, result, waitErr, closeErr)
}

func (daemon *daemon) completeNativeRunAfterUsage(ctx context.Context, key state.RunKey, active *runningRun, result harness.TaskResult, waitErr, closeErr error) {
	active.inputMu.Lock()
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeUsageFinalized {
		daemon.mu.Unlock()
		active.inputMu.Unlock()
		return
	}
	admission := active.nativeFinalAdmission
	if admission == nil && active.goalAdmission != nil {
		admissionCopy := *active.goalAdmission
		admission = &admissionCopy
	}
	cancelled := active.cancelled
	stale := active.stale
	if closeErr == nil {
		active.cleanupBlocked = false
	} else {
		active.cleanupBlocked = true
	}
	active.nativeUsageRetryPending = false
	active.nativeUsageRetryInFlight = false
	// The native session has reached its final turn barrier. Keep renewal
	// disabled until releaseRun, even if terminal transition persistence later
	// needs another recovery pass.
	active.nativeUsageRenewalBlocked = true
	active.nativeUsageFinalized = true
	daemon.mu.Unlock()
	if stale {
		active.inputMu.Unlock()
		journal, err := daemon.store.SetLocalState(key, "stale")
		daemon.mu.Lock()
		cleanupBlocked := active.cleanupBlocked
		daemon.mu.Unlock()
		if !cleanupBlocked {
			daemon.releaseRun(key)
		}
		if err == nil && !cleanupBlocked {
			_ = daemon.scheduleCleanup(context.Background(), journal)
		}
		return
	}
	terminalQueued := false
	if !cancelled {
		stateName, payload := nativeTerminalPayload(result, admission, waitErr, closeErr)
		if err := daemon.queueTerminalTransitionWithRetry(ctx, key, stateName, payload); err != nil && daemon.log != nil {
			daemon.log.Error("queue_native_terminal_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		} else if err == nil {
			terminalQueued = true
		}
	}
	active.inputMu.Unlock()
	if terminalQueued {
		if err := daemon.clearNativeUsageRecoveryBarrier(key); err != nil && daemon.log != nil {
			daemon.log.Error("clear_native_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
	}
	daemon.releaseCleanupAfterProcessExit(key)
}

type nativeUsageRetryWork struct {
	key          state.RunKey
	active       *runningRun
	result       harness.TaskResult
	waitErr      error
	closeErr     error
	usage        *protocol.Usage
	needsPrepare bool
}

func (daemon *daemon) deferNativeUsageRetry(key state.RunKey, active *runningRun, result harness.TaskResult, waitErr, closeErr error, usage *protocol.Usage, needsPrepare bool, cause error) {
	now := daemon.now().UTC()
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeUsageFinalized || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight {
		daemon.mu.Unlock()
		return
	}
	finalResult := cloneNativeTaskResult(result)
	active.nativeFinalResult = &finalResult
	active.nativeFinalWaitErr = waitErr
	active.nativeFinalCloseErr = closeErr
	if active.goalAdmission != nil {
		admission := *active.goalAdmission
		active.nativeFinalAdmission = &admission
	}
	if usage != nil {
		finalUsage := cloneNativeUsage(*usage)
		active.nativeFinalUsage = &finalUsage
	}
	active.nativeUsageRetryAttempts = 1
	active.nativeUsageRetryAt = now.Add(minimumInterval)
	active.nativeUsageRetryDeadline = now.Add(nativeUsageRetryWindow)
	active.nativeUsageRetryPending = true
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryNeedsPrepare = needsPrepare
	active.nativeUsageRenewalBlocked = true
	active.nativeUsageRetryExhausted = false
	active.cleanupBlocked = true
	daemon.mu.Unlock()

	var recoveryErr error
	if usage != nil && !needsPrepare {
		if _, err := daemon.store.QueueNativeUsageRecovery(key, *usage); err != nil {
			recoveryErr = fmt.Errorf("persist native Goal usage recovery: %w", err)
		} else {
			daemon.signalOutboxFor(key)
		}
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	daemon.signalOutboxFor(key)
	if daemon.log != nil {
		daemon.log.Error("queue_native_goal_usage_failed", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, recoveryErr, retentionErr))
	}
}

func (daemon *daemon) flushPendingNativeUsage(ctx context.Context) {
	now := daemon.now().UTC()
	var retries []nativeUsageRetryWork
	var exhausted []nativeUsageRetryWork
	daemon.mu.Lock()
	for key, active := range daemon.running {
		if !active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.nativeFinalResult == nil {
			continue
		}
		if !active.nativeUsageRetryAt.IsZero() && now.Before(active.nativeUsageRetryAt) {
			continue
		}
		work := nativeUsageRetryWork{
			key:          key,
			active:       active,
			result:       cloneNativeTaskResult(*active.nativeFinalResult),
			waitErr:      active.nativeFinalWaitErr,
			closeErr:     active.nativeFinalCloseErr,
			needsPrepare: active.nativeUsageRetryNeedsPrepare,
		}
		if active.nativeFinalUsage != nil {
			usage := cloneNativeUsage(*active.nativeFinalUsage)
			work.usage = &usage
		}
		if active.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!active.nativeUsageRetryDeadline.IsZero() && !now.Before(active.nativeUsageRetryDeadline)) {
			active.nativeUsageRetryPending = false
			active.nativeUsageRetryInFlight = false
			active.nativeUsageRetryExhausted = true
			active.nativeUsageRenewalBlocked = true
			exhausted = append(exhausted, work)
			continue
		}
		active.nativeUsageRetryInFlight = true
		retries = append(retries, work)
	}
	daemon.mu.Unlock()

	for _, work := range exhausted {
		daemon.exhaustNativeUsageRetry(ctx, work.key, work.active, errors.New("native Goal usage persistence retry budget exhausted"))
	}
	for _, work := range retries {
		var usageErr error
		if work.usage != nil && !work.needsPrepare {
			usageErr = daemon.queueNativeGoalUsageRecord(work.key, *work.usage)
		} else {
			usage, shouldQueue, prepareErr := daemon.prepareNativeGoalUsage(work.key, &work.result)
			usageErr = prepareErr
			if usageErr == nil && shouldQueue {
				daemon.rememberNativeFinalUsage(work.key, work.active, usage)
				usageErr = daemon.queueNativeGoalUsageRecord(work.key, usage)
			}
		}
		if usageErr != nil {
			daemon.noteNativeUsageRetryFailure(ctx, work.key, work.active, usageErr)
			continue
		}
		daemon.completeNativeRunAfterUsage(ctx, work.key, work.active, work.result, work.waitErr, work.closeErr)
	}
}

func (daemon *daemon) noteNativeUsageRetryFailure(ctx context.Context, key state.RunKey, active *runningRun, cause error) {
	now := daemon.now().UTC()
	exhausted := false
	attempt := 0
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeUsageFinalized {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryAttempts++
	attempt = active.nativeUsageRetryAttempts
	if active.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!active.nativeUsageRetryDeadline.IsZero() && !now.Before(active.nativeUsageRetryDeadline)) {
		active.nativeUsageRetryPending = false
		active.nativeUsageRetryExhausted = true
		active.nativeUsageRenewalBlocked = true
		exhausted = true
	} else {
		active.nativeUsageRetryPending = true
		active.nativeUsageRetryAt = now.Add(nativeUsageRetryDelay(active.nativeUsageRetryAttempts))
	}
	daemon.mu.Unlock()

	if exhausted {
		daemon.exhaustNativeUsageRetry(ctx, key, active, cause)
		return
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	if daemon.log != nil {
		daemon.log.Warn("retry_native_goal_usage_failed", "run_id", key.RunID, "generation", key.Generation, "attempt", attempt, "error", errors.Join(cause, retentionErr))
	}
}

func (daemon *daemon) exhaustNativeUsageRetry(ctx context.Context, key state.RunKey, active *runningRun, cause error) {
	daemon.mu.Lock()
	if daemon.running[key] != active {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryPending = false
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryExhausted = true
	active.nativeUsageRenewalBlocked = true
	active.stale = true
	active.cleanupBlocked = true
	var finalUsage *protocol.Usage
	if active.nativeFinalUsage != nil {
		copy := cloneNativeUsage(*active.nativeFinalUsage)
		finalUsage = &copy
	}
	var goalSession *state.GoalSessionKey
	if active.goalSession != nil {
		copy := *active.goalSession
		goalSession = &copy
	}
	daemon.mu.Unlock()

	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)

	// A close failure already marks the native session uncertain. If the session
	// is still open for any other reason, make that recovery barrier durable
	// before dropping the in-memory run. A successfully closed session remains
	// closed; the retained journal/usage evidence below is the recovery record.
	var recoveryErr error
	if goalSession != nil {
		if sessionJournal, err := daemon.store.LoadGoalSession(*goalSession); err == nil {
			if sessionJournal.SessionState != state.GoalSessionStateClosed && !sessionJournal.NeedsReconciliation() {
				_, recoveryErr = daemon.store.MarkGoalSessionUncertain(*goalSession, "native Goal usage persistence retry budget exhausted")
			}
		} else if !state.IsNotFound(err) {
			recoveryErr = err
		}
	}

	if finalUsage == nil {
		usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, nil)
		if usageErr != nil || !shouldQueue {
			if usageErr == nil {
				usageErr = errors.New("native Goal usage recovery body is unavailable")
			}
			daemon.retryNativeUsageRecoveryPersistence(key, active, errors.Join(cause, usageErr, retentionErr, recoveryErr))
			return
		}
		finalUsage = &usage
		daemon.rememberNativeFinalUsage(key, active, usage)
	}

	// The accounting barrier and immutable receipt must become durable together.
	// A failed write keeps the stopped native run resident with its exact body so
	// a retry cannot invent a replacement usage identity.
	journal, usageEvidenceErr := daemon.store.QueueNativeUsageRecovery(key, *finalUsage)
	if usageEvidenceErr != nil {
		daemon.retryNativeUsageRecoveryPersistence(key, active, errors.Join(cause, retentionErr, recoveryErr, usageEvidenceErr))
		return
	}
	daemon.signalOutboxFor(key)
	var staleErr error
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
		_, staleErr = daemon.store.SetLocalState(key, "stale")
	}
	var terminalErr error
	if loaded, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
		if _, terminalErr = daemon.queueNativeUsageRecoveryTerminal(ctx, loaded, cause); terminalErr == nil {
			_, terminalErr = daemon.store.ClearNativeUsageRecoveryRequired(key, *finalUsage)
		}
	} else if !state.IsNotFound(loadErr) {
		terminalErr = loadErr
	}

	// All durable recovery markers have been attempted. Do not retain a dead
	// native run in daemon.running: renewal and command admission are already
	// disabled, and restart must not be required to release the slot/memory.
	daemon.releaseRun(key)
	if daemon.log != nil {
		daemon.log.Error("native_goal_usage_retry_exhausted", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, retentionErr, staleErr, recoveryErr, terminalErr))
	}
}

func (daemon *daemon) retryNativeUsageRecoveryPersistence(key state.RunKey, active *runningRun, cause error) {
	now := daemon.now().UTC()
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeUsageFinalized {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryPending = true
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryExhausted = true
	active.nativeUsageRetryAttempts = nativeUsageRetryLimit
	active.nativeUsageRetryAt = now.Add(nativeUsageRetryDelay(1))
	active.nativeUsageRetryDeadline = now.Add(nativeUsageRetryWindow)
	active.nativeUsageRetryNeedsPrepare = active.nativeFinalUsage == nil
	active.nativeUsageRenewalBlocked = true
	active.cleanupBlocked = true
	daemon.mu.Unlock()
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	daemon.signalOutboxFor(key)
	if daemon.log != nil {
		daemon.log.Error("persist_native_goal_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, retentionErr))
	}
}

func (daemon *daemon) queueNativeUsageRecoveryTerminal(ctx context.Context, journal state.RunJournal, cause error) (state.RunJournal, error) {
	if !journal.NativeUsageRecoveryRequired {
		return journal, nil
	}
	if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
		if journal.TerminalState == "" {
			return journal, errors.New("native usage recovery terminal state is missing")
		}
		return journal, nil
	}
	errorText := "native Goal usage persistence retry budget exhausted"
	if cause != nil {
		errorText = cause.Error()
	}
	if err := daemon.queueTerminalTransitionWithRetry(ctx, journal.Key(), "failed", map[string]string{
		"stage":   "native_goal_usage_recovery",
		"reason":  string(protocol.TaskResultReasonUnknownOutcome),
		"summary": "native Goal usage persistence retry budget exhausted",
		"error":   errorText,
	}); err != nil {
		return journal, err
	}
	updated, err := daemon.store.LoadJournal(journal.Key())
	if err != nil {
		return journal, err
	}
	return updated, nil
}

func nativeUsageRecoveryUsage(journal state.RunJournal) (protocol.Usage, bool) {
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey && delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Usage), true
		}
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey && delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Usage), true
		}
	}
	for _, delivery := range journal.RetiredGoalDeliveries {
		if delivery.Delivery.Kind == state.GoalDeliveryUsage && delivery.Delivery.DeliveryID == nativeGoalUsageKey && delivery.Delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Delivery.Usage), true
		}
	}
	return protocol.Usage{}, false
}

func (daemon *daemon) clearNativeUsageRecoveryBarrier(key state.RunKey) error {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || !journal.NativeUsageRecoveryRequired {
		return err
	}
	usage, ok := nativeUsageRecoveryUsage(journal)
	if !ok {
		return errors.New("native usage recovery body is unavailable")
	}
	_, err = daemon.store.ClearNativeUsageRecoveryRequired(key, usage)
	return err
}

// recoverNativeUsageDelivery restores a pre-existing immutable usage body. A
// legacy marker without one gets one explicit unknown accounting receipt before
// terminal recovery is allowed to proceed.
func (daemon *daemon) recoverNativeUsageDelivery(journal state.RunJournal) (state.RunJournal, protocol.Usage, error) {
	if usage, ok := nativeUsageRecoveryUsage(journal); ok {
		return journal, usage, nil
	}
	usage, shouldQueue, err := daemon.prepareNativeGoalUsage(journal.Key(), nil)
	if err != nil {
		return journal, protocol.Usage{}, err
	}
	if !shouldQueue {
		return journal, protocol.Usage{}, errors.New("native usage recovery body is unavailable")
	}
	updated, err := daemon.store.QueueGoalUsage(journal.Key(), usage)
	if err != nil {
		return journal, protocol.Usage{}, err
	}
	return updated, usage, nil
}

func nativeUsageRetryDelay(attempt int) time.Duration {
	delay := minimumInterval
	for index := 1; index < attempt; index++ {
		delay = nextRetryDelay(delay)
	}
	return delay
}

func (daemon *daemon) rememberNativeFinalUsage(key state.RunKey, active *runningRun, usage protocol.Usage) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.running[key] != active {
		return
	}
	copy := cloneNativeUsage(usage)
	active.nativeFinalUsage = &copy
	active.nativeUsageRetryNeedsPrepare = false
}

func cloneNativeUsage(usage protocol.Usage) protocol.Usage {
	copy := usage
	if usage.InputTokens != nil {
		value := *usage.InputTokens
		copy.InputTokens = &value
	}
	if usage.OutputTokens != nil {
		value := *usage.OutputTokens
		copy.OutputTokens = &value
	}
	if usage.CachedInputTokens != nil {
		value := *usage.CachedInputTokens
		copy.CachedInputTokens = &value
	}
	if usage.CostMicrousd != nil {
		value := *usage.CostMicrousd
		copy.CostMicrousd = &value
	}
	if usage.PriceVersion != nil {
		value := *usage.PriceVersion
		copy.PriceVersion = &value
	}
	if usage.SupersedesID != nil {
		value := *usage.SupersedesID
		copy.SupersedesID = &value
	}
	return copy
}

func cloneNativeTaskResult(result harness.TaskResult) harness.TaskResult {
	copy := result
	if result.Semantic != nil {
		semantic := *result.Semantic
		semantic.EvidenceRefs = slices.Clone(result.Semantic.EvidenceRefs)
		semantic.Diagnostics = slices.Clone(result.Semantic.Diagnostics)
		if result.Semantic.Blocker != nil {
			blocker := *result.Semantic.Blocker
			blocker.WorkItemIDs = slices.Clone(result.Semantic.Blocker.WorkItemIDs)
			semantic.Blocker = &blocker
		}
		if result.Semantic.ProposedNextAction != nil {
			action := *result.Semantic.ProposedNextAction
			if result.Semantic.ProposedNextAction.Blocker != nil {
				blocker := *result.Semantic.ProposedNextAction.Blocker
				blocker.WorkItemIDs = slices.Clone(result.Semantic.ProposedNextAction.Blocker.WorkItemIDs)
				action.Blocker = &blocker
			}
			semantic.ProposedNextAction = &action
		}
		if result.Semantic.Proposal != nil {
			proposal := append(json.RawMessage(nil), (*result.Semantic.Proposal)...)
			semantic.Proposal = &proposal
		}
		for index := range semantic.Diagnostics {
			if result.Semantic.Diagnostics[index].Severity != nil {
				severity := *result.Semantic.Diagnostics[index].Severity
				semantic.Diagnostics[index].Severity = &severity
			}
		}
		copy.Semantic = &semantic
	}
	if result.Reason != nil {
		reason := *result.Reason
		copy.Reason = &reason
	}
	return copy
}

func (daemon *daemon) closeNativeGoalSession(key state.RunKey, active *runningRun, session harness.Session) error {
	if active == nil || session == nil {
		return errors.New("native Goal session is unavailable")
	}
	daemon.mu.Lock()
	goalSession := active.goalSession
	if goalSession == nil {
		daemon.mu.Unlock()
		return errors.New("native Goal session key is unavailable")
	}
	daemon.mu.Unlock()
	closeContext, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
	closeErr := session.Close(closeContext)
	cancel()
	if closeErr != nil {
		daemon.requireNativeSessionCloseRetry(key, session)
		daemon.markNativeCleanupBlocked(active)
		retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
		_, uncertainErr := daemon.store.MarkGoalSessionUncertain(*goalSession, "native session close failed: "+closeErr.Error())
		return errors.Join(closeErr, retentionErr, uncertainErr)
	}
	sessionJournal, err := daemon.store.LoadGoalSession(*goalSession)
	if err != nil {
		daemon.markNativeCleanupBlocked(active)
		retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
		return errors.Join(fmt.Errorf("load durable Goal session after process stop: %w", err), retentionErr)
	}
	if sessionJournal.NeedsReconciliation() {
		_, err = daemon.store.ResolveGoalSessionUncertainStopped(*goalSession, sessionJournal.Compatibility())
	} else {
		_, err = daemon.store.CloseGoalSession(*goalSession)
	}
	if err != nil {
		daemon.markNativeCleanupBlocked(active)
		retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
		_, uncertainErr := daemon.store.MarkGoalSessionUncertain(*goalSession, "durable native session close failed after process stop: "+err.Error())
		return errors.Join(fmt.Errorf("record durable native session close: %w", err), retentionErr, uncertainErr)
	}
	if err := daemon.clearNativeProcessDetails(key); err != nil {
		daemon.markNativeCleanupBlocked(active)
		retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
		return errors.Join(err, retentionErr)
	}
	return nil
}

func (daemon *daemon) clearNativeProcessDetails(key state.RunKey) error {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("load native process record after close: %w", err)
	}
	if journal.PID == 0 && strings.TrimSpace(journal.ProcessIdentity) == "" && journal.StartedAt.IsZero() {
		return nil
	}
	if journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "" {
		return errors.New("native process record is incomplete after close")
	}
	if _, err := daemon.store.ClearProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
		return fmt.Errorf("clear native process record after close: %w", err)
	}
	return nil
}

func (daemon *daemon) markNativeCleanupBlocked(active *runningRun) {
	daemon.mu.Lock()
	if active != nil {
		active.cleanupBlocked = true
	}
	daemon.mu.Unlock()
}

func nativeTerminalPayload(result harness.TaskResult, admission *protocol.Admission, waitErr, closeErr error) (string, map[string]any) {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "native Goal execution failed"
	}
	payload := map[string]any{"summary": summary}
	if result.Semantic != nil {
		payload["task_result"] = result.Semantic
	}
	if waitErr != nil {
		payload["reason"] = string(canonicalTaskResultReason(waitErr, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = waitErr.Error()
		return "failed", payload
	}
	if closeErr != nil {
		payload["reason"] = string(canonicalTaskResultReason(closeErr, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = closeErr.Error()
		return "failed", payload
	}
	if admission == nil {
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = "native Goal admission is unavailable"
		return "failed", payload
	}
	if result.Semantic == nil {
		switch result.Kind {
		case harness.ResultCancelled:
			payload["reason"] = string(protocol.TaskResultReasonCancelled)
			return "cancelled", payload
		case harness.ResultUnknown:
			payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonUnknownOutcome))
			payload["error"] = "native turn outcome is unknown"
			return "failed", payload
		case harness.ResultFailed:
			payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonProcessFailure))
			return "failed", payload
		default:
			payload["reason"] = string(protocol.TaskResultReasonMissingResult)
			payload["error"] = "native turn finished without a canonical task_result"
			return "failed", payload
		}
	}
	if err := result.Semantic.Validate(); err != nil {
		payload["reason"] = string(canonicalTaskResultReason(err, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "invalid native task_result: " + err.Error()
		return "failed", payload
	}
	if err := protocol.ValidateTaskResultForAdmission(*admission, *result.Semantic); err != nil {
		payload["reason"] = string(canonicalTaskResultReason(err, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "native task_result is incompatible with admission: " + err.Error()
		return "failed", payload
	}
	if result.Semantic.Kind != protocol.TaskResultCandidateCompletion && result.Semantic.Subject != admission.Subject {
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "non-candidate native task_result Subject does not match the admitted Subject"
		return "failed", payload
	}
	switch result.Kind {
	case harness.ResultSucceeded:
		return "completed", payload
	case harness.ResultCancelled:
		payload["reason"] = string(protocol.TaskResultReasonCancelled)
		return "cancelled", payload
	case harness.ResultFailed:
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonProcessFailure))
		return "failed", payload
	default:
		payload["reason"] = string(protocol.TaskResultReasonUnknownOutcome)
		payload["error"] = "native turn outcome is unknown"
		return "failed", payload
	}
}

func typedTaskResultReason(cause error) (protocol.TaskResultReason, bool) {
	if reason, ok := taskResultFailureReason(cause); ok {
		return reason, true
	}
	return "", false
}

// canonicalTaskResultReason defines one precedence order for failed terminal
// payloads. A valid semantic TaskResult records the turn's own outcome and
// therefore outranks physical transport/process failures observed afterward.
func canonicalTaskResultReason(cause error, result *harness.TaskResult, fallback protocol.TaskResultReason) protocol.TaskResultReason {
	if result != nil && result.Semantic != nil && result.Semantic.Kind == protocol.TaskResultFailed && result.Semantic.Reason != nil && result.Semantic.Validate() == nil {
		return *result.Semantic.Reason
	}
	if reason, ok := typedTaskResultReason(cause); ok {
		return reason
	}
	if result != nil && result.Reason != nil {
		return *result.Reason
	}
	return fallback
}

// verifyNativeTaskResultSubject ensures a native model only proposes the
// artifact actually present in its daemon-owned worktree. A candidate result
// may advance from admission Subject A to commit B; all other result kinds
// remain bound to A.
func (daemon *daemon) verifyNativeTaskResultSubject(ctx context.Context, prepared workspace.Prepared, admission *protocol.Admission, result *harness.TaskResult) error {
	if admission == nil {
		return errors.New("native Goal admission is unavailable")
	}
	if result == nil || result.Semantic == nil {
		return nil
	}
	if err := result.Semantic.Validate(); err != nil {
		return fmt.Errorf("validate native task_result: %w", err)
	}
	if err := protocol.ValidateTaskResultForAdmission(*admission, *result.Semantic); err != nil {
		return fmt.Errorf("native task_result is incompatible with admission: %w", err)
	}
	subjectWorkspace, ok := daemon.workspace.(goalSubjectWorkspace)
	if !ok {
		return errors.New("workspace service does not support immutable Goal subjects")
	}
	actual, err := subjectWorkspace.DeriveSubject(ctx, prepared, admission.Subject.ResourceID)
	if err != nil {
		return fmt.Errorf("derive native worktree Subject: %w", err)
	}
	if result.Semantic.Subject != actual {
		return errors.New("native task_result Subject does not match the daemon-derived worktree Subject")
	}
	if result.Semantic.Kind != protocol.TaskResultCandidateCompletion && actual != admission.Subject {
		return errors.New("non-candidate native task_result advanced beyond the admitted Subject")
	}
	return nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (daemon *daemon) handleCommand(ctx context.Context, command protocol.Command) bool {
	key := state.RunKey{RunID: command.RunID, Generation: command.Generation}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == command.CommandID {
			return true
		}
	}
	for _, intent := range journal.ControlCommandIntents {
		if intent.CommandID == command.CommandID {
			return true
		}
	}
	if command.Kind == "cancel" {
		daemon.rememberWorkspaceRetention(key)
		defer daemon.persistWorkspaceRetention(key)
		daemon.mu.Lock()
		active := daemon.running[key]
		if active == nil {
			daemon.mu.Unlock()
			if journal.LocalState == "terminal_pending" {
				return daemon.queueCancellationReceipt(ctx, key, command.CommandID)
			}
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		if !active.claimed {
			active.cancelled = true
			active.cancelCommandID = command.CommandID
			daemon.mu.Unlock()
			return false
		}
		active.cancelled = true
		active.cancelCommandID = command.CommandID
		process := active.process
		nativeSession := active.nativeSession
		cancel := active.cancel
		terminalReserved := process == nil && nativeSession == nil && cancel != nil
		if terminalReserved {
			active.terminalizing++
		}
		daemon.mu.Unlock()
		if terminalReserved {
			defer daemon.releaseTerminalReservation(key)
		}
		var terminateErr error
		var finalResult harness.TaskResult
		finalResultKnown := false
		if nativeSession != nil {
			interruptContext, stopInterrupt := context.WithTimeout(ctx, 2*time.Second)
			receipt, controlErr := nativeSession.Control(interruptContext, harness.ControlRequest{CommandID: command.CommandID, Kind: harness.ControlCancel})
			stopInterrupt()
			controlFailure := controlErr
			if controlFailure == nil && receipt.Outcome != harness.ControlApplied {
				controlFailure = fmt.Errorf("native cancel was %s: %s", receipt.Outcome, receipt.Message)
			}
			// A control acknowledgement only accepts the native request. The
			// staged turn barrier is the first close permission; it never returns
			// an early TaskResult and it remains bounded when acknowledgement is
			// lost.
			waitContext, stopWait := context.WithTimeout(ctx, 2*time.Second)
			var waitErr error
			if staged, ok := nativeSession.(harness.StagedSession); ok {
				waitErr = staged.WaitTurn(waitContext)
			} else {
				finalResult, waitErr = nativeSession.Wait(waitContext)
				finalResultKnown = waitErr == nil
			}
			stopWait()
			if waitErr != nil && !errors.Is(waitErr, context.DeadlineExceeded) && !errors.Is(waitErr, context.Canceled) {
				terminateErr = waitErr
			}
			closeErr := daemon.closeNativeGoalSession(key, active, nativeSession)
			if controlFailure != nil {
				if closeErr != nil {
					terminateErr = errors.Join(terminateErr, controlFailure, closeErr)
				} else {
					// Preserve the existing cancellation contract: a bounded process
					// close is sufficient even when the native interrupt receipt was
					// unavailable.
					terminateErr = errors.Join(terminateErr)
				}
			} else {
				terminateErr = errors.Join(terminateErr, closeErr)
			}
			if _, ok := nativeSession.(harness.StagedSession); ok {
				finalContext, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
				result, finalWaitErr := nativeSession.Wait(finalContext)
				cancel()
				if finalWaitErr != nil {
					terminateErr = errors.Join(terminateErr, fmt.Errorf("wait native final result: %w", finalWaitErr))
				} else {
					finalResult = result
					finalResultKnown = true
				}
			}
		} else if process != nil {
			terminateErr = process.Terminate(ctx, 2*time.Second)
		} else if cancel != nil {
			cancel()
		}
		if nativeSession != nil {
			var usageResult *harness.TaskResult
			if finalResultKnown {
				usageResult = &finalResult
			}
			if usageErr := daemon.queueNativeGoalUsage(ctx, key, usageResult); usageErr != nil {
				// Do not acknowledge cancellation or allow terminal cleanup to
				// proceed while a terminal native outcome has no durable accounting.
				daemon.retainUnknownGoalLaunchWorkspace(key)
				if daemon.log != nil {
					daemon.log.Warn("queue_native_goal_usage_before_cancel_failed", "run_id", key.RunID, "generation", key.Generation, "error", usageErr)
				}
				return false
			}
		}
		if terminateErr != nil {
			daemon.queueFailure(ctx, key, "cancel_native", terminateErr)
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
		}
		active.inputMu.Lock()
		defer active.inputMu.Unlock()
		return daemon.queueCancellationReceipt(ctx, key, command.CommandID)
	}

	daemon.mu.Lock()
	active := daemon.running[key]
	daemon.mu.Unlock()
	if active == nil {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	if active.nativeSession != nil {
		// This vertical slice only has a verified cancellation delivery path.
		// Guidance, pause/resume, approval, and a second native turn remain
		// visible as unsupported rather than falling back to legacy stdin.
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	outcome := "rejected"
	switch command.Kind {
	case "guidance", "pause", "resume":
		return daemon.handleSupervisoryCommand(ctx, key, journal, active, command)
	case "provide_input":
		var object map[string]json.RawMessage
		if err := json.Unmarshal(command.Payload, &object); err != nil || object == nil {
			break
		}
		payloadDigest, err := canonicalInputDigest(command.Payload)
		if err != nil {
			break
		}
		input, err := inputRecord(protocol.AgentInputRecordProvideInput, journal.Work.Goal, command.Payload, nil)
		if err != nil {
			outcome = "failed"
			break
		}
		runningTransitionID, err := daemon.options.newID()
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
		}
		acknowledgementID, err := daemon.options.newID()
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
		}
		active.inputMu.Lock()
		defer active.inputMu.Unlock()
		daemon.mu.Lock()
		accepted := daemon.running[key] == active && activeRunAcceptsCommand(active)
		process := active.process
		daemon.mu.Unlock()
		if !accepted {
			current, loadErr := daemon.store.LoadJournal(key)
			if loadErr == nil && current.InputCommandIntent != nil && current.InputCommandIntent.CommandID == command.CommandID && current.InputCommandIntent.PayloadDigest == payloadDigest {
				if current.InputCommandIntent.Outcome == "" {
					return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
				}
				daemon.signalOutboxFor(key)
				return true
			}
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		prepared, created, err := daemon.store.PrepareProvideInput(key, state.InputCommandIntent{
			CommandID:           command.CommandID,
			PayloadDigest:       payloadDigest,
			RunningTransitionID: runningTransitionID,
			AckID:               acknowledgementID,
		})
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		intent := prepared.InputCommandIntent
		if intent == nil {
			return false
		}
		if !created {
			if intent.Outcome == "" {
				return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
			}
			daemon.signalOutboxFor(key)
			return true
		}
		if daemon.writeInputBounded(ctx, active, process, input) != nil {
			return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
		}
		return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "applied")
	}
	return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, outcome)
}

func (daemon *daemon) releaseTerminalReservation(key state.RunKey) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if active := daemon.running[key]; active != nil && active.terminalizing > 0 {
		active.terminalizing--
	}
}

func (daemon *daemon) runningRun(key state.RunKey) *runningRun {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.running[key]
}

func activeRunAcceptsCommand(active *runningRun) bool {
	return active != nil && active.process != nil && !active.starting && !active.cancelled && !active.stale && !active.terminal && active.terminalizing == 0
}

func (daemon *daemon) terminatePersistedProcessWithRetry(ctx context.Context, key state.RunKey) bool {
	for {
		journal, err := daemon.store.LoadJournal(key)
		if err == nil {
			if daemon.options.terminatePersist == nil {
				err = errors.New("persisted process termination is unavailable")
			} else {
				err = daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity)
			}
		}
		if err == nil {
			return true
		}
		if daemon.log != nil {
			daemon.log.Warn("terminate_persisted_process_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) queueCancellationReceipt(ctx context.Context, key state.RunKey, commandID string) bool {
	if err := daemon.queueCancelledTerminalAndAcknowledgementWithContext(ctx, key, commandID); err != nil {
		if daemon.log != nil {
			daemon.log.Error("queue_cancelled_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		return false
	}
	return true
}

func (daemon *daemon) newIDWithRetry(ctx context.Context, key state.RunKey, commandID, kind string) (string, error) {
	for {
		id, err := daemon.options.newID()
		if err == nil {
			return id, nil
		}
		if daemon.log != nil {
			daemon.log.Warn("generate_cancelled_receipt_id_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "kind", kind, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) queueCommandAcknowledgement(key state.RunKey, commandID, outcome string) bool {
	return daemon.queueCommandAcknowledgementWithContext(context.Background(), key, commandID, outcome)
}

func (daemon *daemon) queueCommandAcknowledgementWithContext(ctx context.Context, key state.RunKey, commandID, outcome string) bool {
	rootContext := daemon.rootContext(ctx)
	acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: commandID, Outcome: outcome}
	for {
		if daemon.commandAcknowledgementRetired(key) {
			return false
		}
		if acknowledgement.AckID == "" {
			id, err := daemon.options.newID()
			if err != nil {
				if daemon.log != nil {
					daemon.log.Warn("generate_command_acknowledgement_id_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
				}
				timer := daemon.timer(minimumInterval)
				select {
				case <-rootContext.Done():
					timer.Stop()
					return false
				case <-timer.Chan():
				}
				continue
			}
			acknowledgement.AckID = id
		}
		_, err := daemon.store.QueueCommandAcknowledgement(key, acknowledgement)
		if err == nil {
			daemon.signalOutbox()
			return true
		}
		if daemon.commandAcknowledgementRetired(key) {
			return false
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_command_acknowledgement_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) commandAcknowledgementRetired(key state.RunKey) bool {
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return true
	}
	if err != nil {
		return false
	}
	return state.CommandAcknowledgementRetired(journal)
}

func (daemon *daemon) completeProvideInputWithRetry(ctx context.Context, key state.RunKey, commandID, payloadDigest, outcome string) bool {
	rootContext := daemon.rootContext(ctx)
	for {
		if _, err := daemon.store.CompleteProvideInput(key, commandID, payloadDigest, outcome); err == nil {
			daemon.signalOutbox()
			return true
		} else if daemon.log != nil {
			daemon.log.Warn("complete_provide_input_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) renewLeases(ctx context.Context) {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	now := daemon.now()
	type renewal struct {
		journal   state.RunJournal
		response  protocol.LeaseHeartbeatResponse
		requestID uint64
		err       error
	}
	candidates := make([]state.RunJournal, 0, len(journals))
	for _, journal := range journals {
		if journal.LocalState == "terminal_pending" || journal.LeaseToken == "" {
			continue
		}
		if !hasFullFence(journal) {
			daemon.terminateForLease(journal, "lease fence is incomplete")
			continue
		}
		remaining := journal.LeaseExpiresAt.Sub(now)
		if remaining <= 0 {
			daemon.terminateForLease(journal, "lease expired")
			continue
		}
		if remaining <= leaseSafetyMargin {
			continue
		}
		if !daemon.renewalEligible(journal) {
			continue
		}
		if remaining > daemon.renewalThreshold() {
			continue
		}
		candidates = append(candidates, journal)
	}
	if len(candidates) == 0 {
		return
	}
	jobs := make(chan int)
	completed := make(chan renewal, len(candidates))
	workers := cap(daemon.slots)
	if workers < 1 {
		workers = 1
	}
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				journal := candidates[index]
				response, requestID, renewErr := daemon.renewLease(ctx, journal)
				completed <- renewal{journal: journal, response: response, requestID: requestID, err: renewErr}
			}
		}()
	}
	go func() {
		for index := range candidates {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		close(completed)
	}()
	for result := range completed {
		if ctx.Err() != nil {
			daemon.finishCommandRequest(result.requestID)
			continue
		}
		if !daemon.renewalStillEligible(result.journal) {
			daemon.finishCommandRequest(result.requestID)
			continue
		}
		current := daemon.now()
		if !current.Before(result.journal.LeaseExpiresAt) {
			daemon.finishCommandRequest(result.requestID)
			daemon.terminateForLease(result.journal, "lease expired during renewal")
			continue
		}
		if result.err != nil {
			daemon.finishCommandRequest(result.requestID)
			if errors.Is(result.err, context.Canceled) {
				continue
			}
			if control.IsOwnershipLost(result.err) || result.journal.LeaseExpiresAt.Sub(current) <= leaseSafetyMargin {
				daemon.terminateForLease(result.journal, "lease renewal failed")
			}
			continue
		}
		if !result.response.LeaseExpiresAt.After(current.Add(leaseSafetyMargin)) {
			daemon.finishCommandRequest(result.requestID)
			daemon.terminateForLease(result.journal, "renewed lease is already unsafe")
			continue
		}
		_, _ = daemon.store.AdvanceLeaseExpiry(result.journal.Key(), result.response.LeaseExpiresAt)
		_, _ = daemon.enqueueSnapshotCommandsForRequest(result.requestID, result.response.Commands)
	}
}

func (daemon *daemon) renewalThreshold() time.Duration {
	if daemon.leaseDuration <= 0 {
		return leaseSafetyMargin
	}
	threshold := daemon.leaseDuration * 3 / 4
	if threshold > 90*time.Second {
		return 90 * time.Second
	}
	return threshold
}

func (daemon *daemon) renewLease(ctx context.Context, journal state.RunJournal) (protocol.LeaseHeartbeatResponse, uint64, error) {
	var renewalID uint64
	daemon.mu.Lock()
	active := daemon.running[journal.Key()]
	if !activeRunCanRenew(active) {
		daemon.mu.Unlock()
		return protocol.LeaseHeartbeatResponse{}, 0, context.Canceled
	}
	requestLimit := journal.LeaseExpiresAt.Sub(daemon.now()) - leaseSafetyMargin
	if requestLimit <= 0 {
		daemon.mu.Unlock()
		return protocol.LeaseHeartbeatResponse{}, 0, errLeaseDeadlineReached
	}
	if requestLimit > controlRequestLimit {
		requestLimit = controlRequestLimit
	}
	renewalContext, cancel := context.WithTimeout(ctx, requestLimit)
	active.renewCancelID++
	renewalID = active.renewCancelID
	active.renewCancel = cancel
	daemon.mu.Unlock()
	requestID := daemon.beginCommandRequest()
	response, err := daemon.control.RenewLease(renewalContext, journal.RunID, protocol.LeaseHeartbeatRequest{Fence: journal.Fence()})
	cancel()
	if err != nil {
		daemon.finishCommandRequest(requestID)
		requestID = 0
	}
	daemon.mu.Lock()
	if active := daemon.running[journal.Key()]; active != nil && active.renewCancelID == renewalID {
		active.renewCancel = nil
	}
	daemon.mu.Unlock()
	return response, requestID, err
}

func (daemon *daemon) renewalEligible(snapshot state.RunJournal) bool {
	if snapshot.LeaseToken == "" || snapshot.LocalState == "terminal_pending" || snapshot.LocalState == "cleanup_pending" || snapshot.LocalState == "stale" {
		return false
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return activeRunCanRenew(daemon.running[snapshot.Key()])
}

func activeRunCanRenew(active *runningRun) bool {
	return active != nil && (active.process != nil || active.nativeSession != nil) &&
		!active.starting && !active.stale && !active.terminal && active.terminalizing == 0 && !active.nativeUsageRenewalBlocked
}

func (daemon *daemon) renewalStillEligible(snapshot state.RunJournal) bool {
	journal, err := daemon.store.LoadJournal(snapshot.Key())
	if err != nil || journal.Fence() != snapshot.Fence() {
		return false
	}
	return daemon.renewalEligible(journal)
}

func (daemon *daemon) terminateForLease(journal state.RunJournal, reason string) {
	if journal.LocalState == "terminal_pending" {
		return
	}
	if supervisoryRecoveryRequired(journal) {
		daemon.rememberWorkspaceRetention(journal.Key())
		defer daemon.persistWorkspaceRetention(journal.Key())
	}
	daemon.mu.Lock()
	active := daemon.running[journal.Key()]
	process := Process(nil)
	nativeSession := harness.Session(nil)
	if active != nil {
		active.cancelled = true
		active.stale = true
		process = active.process
		nativeSession = active.nativeSession
		if active.cancel != nil {
			active.cancel()
		}
	}
	daemon.mu.Unlock()
	if nativeSession != nil {
		if closeErr := daemon.closeNativeGoalSession(journal.Key(), active, nativeSession); closeErr != nil && daemon.log != nil {
			daemon.log.Error("close_native_session_for_lease_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", closeErr)
		}
	} else if process != nil {
		_ = process.Terminate(context.Background(), 0)
	} else {
		if active == nil {
			daemon.stopRecoveredJournal(journal, reason)
		}
	}
	_, _ = daemon.store.SetLocalState(journal.Key(), "stale")
}

func (daemon *daemon) flushAll(ctx context.Context) {
	daemon.retryBlockedNativeSessionCloses()
	daemon.retryUnreadyGoalSessionAttachDeliveries()
	daemon.flushPendingNativeUsage(ctx)
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	seen := make(map[state.RunKey]struct{}, len(journals))
	for _, journal := range journals {
		key := journal.Key()
		for {
			if ctx.Err() != nil {
				return
			}
			terminal := journal.LocalState == "terminal_pending" ||
				((journal.LocalState == "cleanup_pending" || journal.LocalState == "stale") && journal.HasPendingGoalDeliveries())
			if journal.LocalState == "cleanup_pending" && !journal.HasPendingGoalDeliveries() {
				_ = daemon.scheduleCleanup(ctx, journal)
				break
			}
			if terminal {
				seen[key] = struct{}{}
				if !daemon.outboxDue(journal) {
					break
				}
			}
			err := daemon.flushRun(ctx, journal)
			current, loadErr := daemon.store.LoadJournal(key)
			if state.IsNotFound(loadErr) {
				daemon.clearOutboxRetry(key)
				break
			}
			if loadErr != nil {
				if err != nil && terminal {
					daemon.recordOutboxFailure(ctx, journal, err)
				}
				break
			}
			if current.LocalState == "terminal_pending" ||
				((current.LocalState == "cleanup_pending" || current.LocalState == "stale") && current.HasPendingGoalDeliveries()) {
				seen[key] = struct{}{}
			}
			if err == nil {
				daemon.clearOutboxRetry(key)
				break
			}

			failedJournal := journal
			var failure *outboxFailure
			if errors.As(err, &failure) {
				failedJournal = failure.journal
			}
			if errors.Is(err, errOutboxChanged) || journalFingerprint(current) != journalFingerprint(failedJournal) {
				daemon.clearOutboxRetry(key)
				journal = current
				continue
			}
			if current.LocalState == "terminal_pending" ||
				((current.LocalState == "cleanup_pending" || current.LocalState == "stale") && current.HasPendingGoalDeliveries()) {
				daemon.recordOutboxFailure(ctx, current, err)
			}
			break
		}
	}
	daemon.flushLateGoalUsage(ctx)
	daemon.pruneOutboxRetries(seen)
}

func (daemon *daemon) outboxDue(journal state.RunJournal) bool {
	key := journal.Key()
	fingerprint := journalFingerprint(journal)
	now := daemon.now()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.outboxRetry == nil {
		daemon.outboxRetry = make(map[state.RunKey]outboxRetry)
	}
	entry, exists := daemon.outboxRetry[key]
	if !exists || entry.fingerprint != fingerprint {
		daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, delay: minimumInterval}
		return true
	}
	return !entry.permanent && !now.Before(entry.retryAt)
}

func (daemon *daemon) recordOutboxFailure(ctx context.Context, journal state.RunJournal, err error) {
	if ctx.Err() != nil {
		return
	}
	key := journal.Key()
	fingerprint := journalFingerprint(journal)
	daemon.mu.Lock()
	entry, exists := daemon.outboxRetry[key]
	if daemon.outboxRetry == nil {
		daemon.outboxRetry = make(map[state.RunKey]outboxRetry)
	}
	if !exists || entry.fingerprint != fingerprint {
		entry = outboxRetry{fingerprint: fingerprint, delay: minimumInterval}
		daemon.outboxRetry[key] = entry
	}
	fallback := entry.delay
	if fallback <= 0 {
		fallback = minimumInterval
	}
	daemon.mu.Unlock()

	delay, retryable := control.RetryDelay(ctx, err, fallback)
	if !retryable && isAmbiguousTerminalDeliveryFailure(journal, err) {
		delay, retryable = fallback, true
	}
	if !retryable && journal.HasPendingGoalDeliveries() {
		// Goal receipts have their own durable idempotency and must continue
		// independently of a conclusive terminal verdict. Never convert a
		// pending Goal delivery into a permanent local backoff.
		delay, retryable = fallback, true
	}
	if !retryable && isOutboxMaintenanceFailure(journal, err) {
		delay, retryable = fallback, true
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	current, exists := daemon.outboxRetry[key]
	if exists && current.fingerprint != fingerprint {
		return
	}
	if !retryable {
		daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, delay: fallback, permanent: true}
		return
	}
	if journal.LocalState == "terminal_pending" && delay < minimumInterval {
		delay = minimumInterval
	}
	now := daemon.now()
	retryAt := now.Add(delay)
	if journal.TerminalVerdict == "" && !journal.LeaseExpiresAt.IsZero() {
		deliveryDeadline := journal.LeaseExpiresAt.Add(terminalGrace)
		if now.Before(deliveryDeadline) && retryAt.After(deliveryDeadline) {
			retryAt = deliveryDeadline
		}
	}
	daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, retryAt: retryAt, delay: nextRetryDelay(fallback)}
}

func isAmbiguousTerminalDeliveryFailure(journal state.RunJournal, err error) bool {
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
		return false
	}
	var responseError *control.ResponseError
	return errors.As(err, &responseError)
}

func isOutboxMaintenanceFailure(journal state.RunJournal, err error) bool {
	var apiError *control.APIError
	var responseError *control.ResponseError
	if errors.As(err, &apiError) || errors.As(err, &responseError) {
		return false
	}
	return journal.LocalState == "terminal_pending" && journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingTransitions) == 0
}

func (daemon *daemon) clearOutboxRetry(key state.RunKey) {
	daemon.mu.Lock()
	delete(daemon.outboxRetry, key)
	daemon.mu.Unlock()
}

func (daemon *daemon) pruneOutboxRetries(seen map[state.RunKey]struct{}) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for key := range daemon.outboxRetry {
		if _, exists := seen[key]; !exists {
			delete(daemon.outboxRetry, key)
		}
	}
}

func journalFingerprint(journal state.RunJournal) string {
	eventIDs := make([]string, len(journal.PendingEvents))
	for index, event := range journal.PendingEvents {
		eventIDs[index] = event.EventID
	}
	transitionIDs := make([]string, len(journal.PendingTransitions))
	for index, transition := range journal.PendingTransitions {
		transitionIDs[index] = transition.TransitionID
	}
	acknowledgementIDs := make([]string, len(journal.PendingCommandAcknowledgements))
	for index, acknowledgement := range journal.PendingCommandAcknowledgements {
		acknowledgementIDs[index] = acknowledgement.AckID
	}
	goalDeliveryIDs := make([]string, len(journal.PendingGoalDeliveries))
	for index, delivery := range journal.PendingGoalDeliveries {
		goalDeliveryIDs[index] = string(delivery.Kind) + ":" + delivery.DeliveryID + ":" + delivery.PayloadDigest + ":" + strconv.FormatBool(delivery.Ready)
	}
	deliveredGoalDeliveryIDs := make([]string, len(journal.DeliveredGoalDeliveries))
	for index, delivery := range journal.DeliveredGoalDeliveries {
		deliveredGoalDeliveryIDs[index] = string(delivery.Kind) + ":" + delivery.DeliveryID + ":" + delivery.PayloadDigest
	}
	value := struct {
		Key                       state.RunKey
		Fence                     protocol.Fence
		LocalState                string
		LeaseExpiresAt            time.Time
		TerminalState             string
		TerminalPendingAt         time.Time
		TerminalVerdict           string
		PendingEventIDs           []string
		PendingTransitionIDs      []string
		AttemptedTransitionIDs    []string
		PendingAcknowledgementIDs []string
		PendingGoalDeliveryIDs    []string
		DeliveredGoalDeliveryIDs  []string
		NativeUsageObservation    *state.NativeUsageObservation
		NativeUsageRecovery       bool
	}{
		Key:                       journal.Key(),
		Fence:                     journal.Fence(),
		LocalState:                journal.LocalState,
		LeaseExpiresAt:            journal.LeaseExpiresAt,
		TerminalState:             journal.TerminalState,
		TerminalPendingAt:         journal.TerminalPendingAt,
		TerminalVerdict:           journal.TerminalVerdict,
		PendingEventIDs:           eventIDs,
		PendingTransitionIDs:      transitionIDs,
		AttemptedTransitionIDs:    journal.AttemptedTransitionIDs,
		PendingAcknowledgementIDs: acknowledgementIDs,
		PendingGoalDeliveryIDs:    goalDeliveryIDs,
		DeliveredGoalDeliveryIDs:  deliveredGoalDeliveryIDs,
		NativeUsageObservation:    journal.NativeUsageObservation,
		NativeUsageRecovery:       journal.NativeUsageRecoveryRequired,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func (daemon *daemon) isStarting(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active != nil && active.starting
}

func (daemon *daemon) finishStarting(key state.RunKey, cleanupReady bool) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil {
		return false
	}
	active.starting = false
	if cleanupReady && active.process == nil && active.nativeSession == nil {
		active.cleanupBlocked = false
	}
	return active.terminal
}

func (daemon *daemon) finishAttachedStart(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil {
		return false
	}
	active.starting = false
	return active.terminal
}

func (daemon *daemon) persistWorkspacePath(runContext context.Context, key state.RunKey, path string) bool {
	rootContext := runContext
	daemon.mu.Lock()
	if daemon.background != nil {
		rootContext = daemon.background
	}
	daemon.mu.Unlock()
	if rootContext == nil {
		rootContext = context.Background()
	}
	for {
		var err error
		if daemon.options.recordWorkspace != nil {
			_, err = daemon.options.recordWorkspace(key, path)
		} else {
			_, err = daemon.store.SetWorkspacePath(key, path)
		}
		if err == nil {
			return true
		}
		daemon.log.Warn("record_workspace_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) flushRun(ctx context.Context, journal state.RunJournal) (err error) {
	defer func() {
		if err == nil {
			return
		}
		var failure *outboxFailure
		if !errors.As(err, &failure) {
			err = &outboxFailure{journal: journal, err: err}
		}
	}()

	key := journal.Key()
	if journal.NativeUsageRecoveryRequired {
		updated, usage, recoveryErr := daemon.recoverNativeUsageDelivery(journal)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
		updated, recoveryErr = daemon.queueNativeUsageRecoveryTerminal(ctx, journal, nil)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
		updated, recoveryErr = daemon.store.ClearNativeUsageRecoveryRequired(key, usage)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
	}
	if journal.LocalState == "stale" {
		// A stale run must not send ordinary lifecycle traffic, but immutable Goal
		// receipts still need a final delivery or a durable retirement. Otherwise a
		// stale fence can wedge cleanup forever behind its own pending outbox.
		if !journal.HasPendingGoalDeliveries() {
			return daemon.scheduleCleanup(ctx, journal)
		}
		updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
		if err != nil {
			return err
		}
		if !updated.HasPendingGoalDeliveries() {
			return daemon.scheduleCleanup(ctx, updated)
		}
		return nil
	}
	if journal.LocalState == "cleanup_pending" {
		updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
		if err != nil {
			return err
		}
		if updated.HasPendingGoalDeliveries() {
			return nil
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	if daemon.isStarting(key) {
		return nil
	}
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict != "" && journal.TerminalVerdict != state.TerminalVerdictAccepted {
		updated, err := daemon.store.ResolveTerminalForCleanup(key, journal.TerminalVerdict, daemon.now())
		if err != nil {
			return err
		}
		return daemon.flushRun(ctx, updated)
	}
	updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
	journal = updated
	if err != nil {
		return err
	}
	predecessorsUnavailable := false
	updated, err = daemon.deliverEventsBeforeAppliedInputAcknowledgement(ctx, journal)
	journal = updated
	if err != nil {
		if journal.LocalState != "terminal_pending" || !terminalPredecessorUnavailable(err) {
			return err
		}
		updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
		if retireErr != nil {
			return retireErr
		}
		journal = updated
		predecessorsUnavailable = true
	}
	for len(journal.PendingTransitions) > 0 {
		nextTransition := journal.PendingTransitions[0]
		if isTerminalTransition(nextTransition.State) {
			if !predecessorsUnavailable {
				updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
				journal = updated
				if err != nil {
					return err
				}
				updated, err = daemon.deliverPendingEvents(ctx, journal)
				journal = updated
				if err != nil {
					if !terminalPredecessorUnavailable(err) {
						return err
					}
					updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
					if retireErr != nil {
						return retireErr
					}
					journal = updated
					predecessorsUnavailable = true
				}
				if delivered {
					continue
				}
			}
		} else {
			updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
			journal = updated
			if err != nil {
				return err
			}
			if delivered {
				updated, err := daemon.deliverPendingEvents(ctx, journal)
				journal = updated
				if err != nil {
					return err
				}
				continue
			}
		}
		updated, err := daemon.store.MarkTransitionAttempted(key, journal.PendingTransitions[0].TransitionID)
		if err != nil {
			return err
		}
		journal = updated
		transition := journal.PendingTransitions[0]
		wasTerminal := journal.LocalState == "terminal_pending"
		requestContext, cancel := daemon.controlContext(ctx)
		err = daemon.control.Transition(requestContext, journal.RunID, transition)
		cancel()
		current, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			return loadErr
		}
		journal = current
		if !current.HasPendingTransition(transition.TransitionID) {
			return errOutboxChanged
		}
		if err != nil {
			if !wasTerminal && journal.LocalState == "terminal_pending" && !isTerminalTransition(transition.State) {
				return errOutboxChanged
			}
			if isTerminalTransition(transition.State) {
				return daemon.handleTerminalDeliveryError(journal, err)
			}
			if journal.LocalState == "terminal_pending" && terminalPredecessorUnavailable(err) {
				updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
				if retireErr != nil {
					return retireErr
				}
				journal = updated
				predecessorsUnavailable = true
				continue
			}
			return daemon.handleOrdinaryDeliveryError(ctx, journal, err, "transition ownership lost")
		}
		if journal.LocalState == "terminal_pending" && isTerminalTransition(transition.State) {
			updated, err := daemon.resolveTerminal(journal, state.TerminalVerdictAccepted)
			if err != nil {
				return err
			}
			journal = updated
		}
		updated, err = daemon.store.MarkTransitionsDelivered(key, []string{transition.TransitionID})
		if err != nil {
			return err
		}
		journal = updated
	}
	updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
	journal = updated
	if err != nil {
		return err
	}
	if delivered {
		updated, err := daemon.deliverPendingEvents(ctx, journal)
		journal = updated
		if err != nil {
			return err
		}
	}
	for len(journal.PendingCommandAcknowledgements) > 0 {
		acknowledgement := journal.PendingCommandAcknowledgements[0]
		updated, err := daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
		journal = updated
		if err != nil {
			return err
		}
	}
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingEvents) == 0 && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 && !journal.HasPendingGoalDeliveries() {
		updated, err := daemon.enterCleanupPending(journal)
		if err != nil {
			return err
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	return nil
}

// flushGoalDeliveries transmits only caller-supplied typed Goal receipts. It
// never converts raw native events or TaskResult evidence references into
// evidence or usage. A missing receipt leaves the original immutable body in
// the journal for an exact retry after a timeout or daemon restart.
func (daemon *daemon) flushGoalDeliveries(ctx context.Context, journal state.RunJournal) (state.RunJournal, *control.GoalSessionReceipt, error) {
	var attachReceipt *control.GoalSessionReceipt
	for len(journal.PendingGoalDeliveries) > 0 {
		index := -1
		for candidateIndex, candidate := range journal.PendingGoalDeliveries {
			if candidate.Kind != state.GoalDeliverySessionAttach || candidate.Ready {
				index = candidateIndex
				break
			}
		}
		if index < 0 {
			return journal, attachReceipt, nil
		}
		delivery := journal.PendingGoalDeliveries[index]
		updated, receipt, err := daemon.deliverGoalDelivery(ctx, journal, delivery.Kind, delivery.DeliveryID, nil)
		if err != nil {
			var rejected *goalDeliveryRejectedError
			if errors.As(err, &rejected) {
				journal = updated
				continue
			}
			return journal, attachReceipt, err
		}
		journal = updated
		if receipt != nil {
			attachReceipt = receipt
		}
	}
	return journal, attachReceipt, nil
}

// deliverGoalDelivery sends one exact durable body and deletes it only after
// the control client has verified its typed receipt.
func (daemon *daemon) deliverGoalDelivery(ctx context.Context, journal state.RunJournal, kind state.GoalDeliveryKind, deliveryID string, validateAttach func(control.GoalSessionReceipt) error) (state.RunJournal, *control.GoalSessionReceipt, error) {
	var delivery *state.GoalDelivery
	for index := range journal.PendingGoalDeliveries {
		candidate := &journal.PendingGoalDeliveries[index]
		if candidate.Kind == kind && candidate.DeliveryID == deliveryID {
			delivery = candidate
			break
		}
	}
	if delivery == nil {
		return journal, nil, errors.New("Goal delivery is not pending")
	}
	if kind == state.GoalDeliverySessionAttach && !delivery.Ready {
		return journal, nil, nil
	}
	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return journal, nil, errors.New("control API does not implement Goal delivery")
	}
	requestContext, cancel := daemon.controlContext(ctx)
	var attachReceipt *control.GoalSessionReceipt
	var evidenceReceipt *control.GoalEvidenceReceipt
	var usageReceipt *control.GoalUsageReceipt
	var err error
	switch kind {
	case state.GoalDeliverySessionAttach:
		payload := delivery.SessionAttach
		if payload == nil {
			cancel()
			return journal, nil, errors.New("Goal session attach payload is missing")
		}
		receipt, callErr := goalAPI.AttachHarnessSession(requestContext, journal.RunID, control.GoalSessionAttachRequest{
			Fence:                delivery.Fence,
			LocalHandleID:        payload.LocalHandleID,
			HarnessKind:          payload.HarnessKind,
			HarnessVersion:       payload.HarnessVersion,
			AdapterVersion:       payload.AdapterVersion,
			WorkspaceFingerprint: payload.WorkspaceFingerprint,
			Workspace:            payload.Workspace,
			RepositoryResourceID: payload.RepositoryResourceID,
		})
		err = callErr
		if err == nil {
			attachReceipt = &receipt
		}
	case state.GoalDeliveryEvidence:
		if delivery.Evidence == nil {
			cancel()
			return journal, nil, errors.New("Goal evidence payload is missing")
		}
		receipt, callErr := goalAPI.AppendEvidence(requestContext, journal.RunID, delivery.Fence, *delivery.Evidence)
		err = callErr
		if err == nil {
			evidenceReceipt = &receipt
		}
	case state.GoalDeliveryUsage:
		if delivery.Usage == nil {
			cancel()
			return journal, nil, errors.New("Goal usage payload is missing")
		}
		receipt, callErr := goalAPI.RecordUsage(requestContext, journal.RunID, delivery.Fence, *delivery.Usage)
		err = callErr
		if err == nil {
			usageReceipt = &receipt
		}
	default:
		cancel()
		return journal, nil, errors.New("Goal delivery kind is invalid")
	}
	cancel()
	if err != nil {
		if statusCode, code, message, definitive := definitiveGoalDeliveryRejection(delivery.Kind, err); definitive {
			updated, retireErr := daemon.store.RetireGoalDelivery(journal.Key(), delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest, statusCode, code, message, daemon.now())
			if retireErr != nil {
				return journal, nil, retireErr
			}
			return updated, nil, &goalDeliveryRejectedError{err: err}
		}
		return journal, nil, err
	}
	if err := validateGoalDeliveryReceipt(journal, *delivery, attachReceipt, evidenceReceipt, usageReceipt); err != nil {
		return journal, nil, err
	}
	if attachReceipt != nil && validateAttach != nil {
		if err := validateAttach(*attachReceipt); err != nil {
			return journal, nil, err
		}
	}
	updated, err := daemon.store.MarkGoalDeliveryDelivered(journal.Key(), delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest)
	if err != nil {
		return journal, nil, err
	}
	return updated, attachReceipt, nil
}

func definitiveGoalDeliveryRejection(kind state.GoalDeliveryKind, err error) (int, string, string, bool) {
	var apiError *control.APIError
	if !errors.As(err, &apiError) {
		return 0, "", "", false
	}
	switch apiError.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden, http.StatusUnprocessableEntity:
		message := apiError.Message
		if message == "" {
			message = apiError.Error()
		}
		return apiError.StatusCode, string(apiError.Code), message, true
	case http.StatusConflict:
		// A usage-key conflict could be an earlier accepted accounting record or
		// a changed body. Without a readback receipt it is an unknown external
		// effect, except for the control plane's explicit idempotency conflict.
		if kind == state.GoalDeliveryUsage && apiError.Code != control.IdempotencyConflict {
			return 0, "", "", false
		}
		message := apiError.Message
		if message == "" {
			message = apiError.Error()
		}
		return apiError.StatusCode, string(apiError.Code), message, true
	default:
		return 0, "", "", false
	}
}

// flushLateGoalUsage drains the retained accounting ledger independently of
// the run lifecycle. It never renews a lease, recreates a run, or starts work.
func (daemon *daemon) flushLateGoalUsage(ctx context.Context) {
	ledgers, err := daemon.store.ListLateGoalUsage()
	if err != nil {
		return
	}
	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return
	}
	for _, ledger := range ledgers {
		for len(ledger.PendingUsageDeliveries) > 0 {
			delivery := ledger.PendingUsageDeliveries[0]
			if delivery.Usage == nil {
				return
			}
			requestContext, cancel := daemon.controlContext(ctx)
			receipt, callErr := goalAPI.RecordUsage(requestContext, ledger.RunID, ledger.Fence, *delivery.Usage)
			cancel()
			if callErr != nil {
				if statusCode, code, message, definitive := definitiveGoalDeliveryRejection(state.GoalDeliveryUsage, callErr); definitive {
					updated, retireErr := daemon.store.RetireLateGoalUsage(state.RunKey{RunID: ledger.RunID, Generation: ledger.Generation}, delivery.DeliveryID, delivery.PayloadDigest, statusCode, code, message, daemon.now())
					if retireErr == nil {
						ledger = updated
						continue
					}
				}
				break
			}
			if err := validateGoalDeliveryReceipt(state.RunJournal{RunID: ledger.RunID}, delivery, nil, nil, &receipt); err != nil {
				break
			}
			updated, err := daemon.store.MarkLateGoalUsageDelivered(state.RunKey{RunID: ledger.RunID, Generation: ledger.Generation}, delivery.DeliveryID, delivery.PayloadDigest)
			if err != nil {
				break
			}
			ledger = updated
		}
	}
}

// validateGoalDeliveryReceipt is a second deletion barrier. The production
// client performs strict wire validation, but the daemon must still protect its
// durable outbox if a different ControlAPI implementation returns a malformed
// or mismatched typed receipt.
func validateGoalDeliveryReceipt(journal state.RunJournal, delivery state.GoalDelivery, attach *control.GoalSessionReceipt, evidence *control.GoalEvidenceReceipt, usage *control.GoalUsageReceipt) error {
	switch delivery.Kind {
	case state.GoalDeliverySessionAttach:
		if delivery.SessionAttach == nil || attach == nil {
			return errors.New("Goal session attach receipt is missing")
		}
		payload := delivery.SessionAttach
		if attach.ID == "" || attach.GoalID != payload.GoalID || attach.RunID != journal.RunID || attach.RuntimeID != delivery.Fence.RuntimeID ||
			attach.ActiveRunID != journal.RunID || attach.LocalHandleID != payload.LocalHandleID || attach.HarnessKind != payload.HarnessKind ||
			attach.HarnessVersion != payload.HarnessVersion || attach.AdapterVersion != payload.AdapterVersion || attach.WorkspaceFingerprint != payload.WorkspaceFingerprint || attach.State != state.GoalSessionStateBusy {
			return errors.New("Goal session attach receipt does not match durable request")
		}
		if attach.Workspace != "" && attach.Workspace != payload.Workspace {
			return errors.New("Goal session attach receipt workspace does not match durable request")
		}
		if payload.RepositoryResourceID != nil && attach.RepositoryResourceID != *payload.RepositoryResourceID {
			return errors.New("Goal session attach receipt repository does not match durable request")
		}
	case state.GoalDeliveryEvidence:
		if delivery.Evidence == nil || evidence == nil || evidence.ID != delivery.Evidence.EvidenceID || evidence.RunID != journal.RunID || evidence.EvidenceKey != delivery.Evidence.EvidenceKey || evidence.Kind != string(delivery.Evidence.Kind) || evidence.SubjectHash != delivery.Evidence.SubjectHash || evidence.Verdict != string(delivery.Evidence.Verdict) {
			return errors.New("Goal evidence receipt does not match durable request")
		}
	case state.GoalDeliveryUsage:
		if delivery.Usage == nil || usage == nil || usage.ID != delivery.Usage.UsageID || usage.RunID != journal.RunID || usage.UsageKey != delivery.Usage.UsageKey || usage.CostBasis != string(delivery.Usage.CostBasis) || !sameOptionalString(usage.CostMicrousd, delivery.Usage.CostMicrousd) {
			return errors.New("Goal usage receipt does not match durable request")
		}
	default:
		return errors.New("Goal delivery kind is invalid")
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (daemon *daemon) deliverEventsBeforeAppliedInputAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, error) {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "applied" || intent.AcknowledgementDelivered {
		return daemon.deliverPendingEvents(ctx, journal)
	}
	events := make([]protocol.RunEvent, 0, len(journal.PendingEvents))
	for _, event := range journal.PendingEvents {
		if event.Sequence <= intent.EventSequenceBarrier {
			events = append(events, event)
		}
	}
	return daemon.deliverEvents(ctx, journal, events)
}

func (daemon *daemon) deliverPendingEvents(ctx context.Context, journal state.RunJournal) (state.RunJournal, error) {
	if len(journal.PendingEvents) == 0 && journal.DroppedOutputChunks > 0 {
		return daemon.queueOutputTruncatedMarker(journal)
	}
	return daemon.deliverEvents(ctx, journal, journal.PendingEvents)
}

func (daemon *daemon) deliverEvents(ctx context.Context, journal state.RunJournal, events []protocol.RunEvent) (state.RunJournal, error) {
	if len(events) == 0 {
		if journal.DroppedOutputChunks > 0 {
			return daemon.queueOutputTruncatedMarker(journal)
		}
		return journal, nil
	}
	key := journal.Key()
	wasTerminal := journal.LocalState == "terminal_pending"
	requestContext, cancel := daemon.controlContext(ctx)
	err := daemon.control.AppendEvents(requestContext, journal.RunID, protocol.AppendEventsRequest{Fence: journal.Fence(), Events: events})
	cancel()
	if err != nil {
		current, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			return journal, loadErr
		}
		journal = current
		if !wasTerminal && journal.LocalState == "terminal_pending" {
			return journal, errOutboxChanged
		}
		return journal, daemon.handleOrdinaryDeliveryError(ctx, journal, err, "event ownership lost")
	}
	ids := make([]string, len(events))
	for index, event := range events {
		ids[index] = event.EventID
	}
	updated, appended, err := daemon.store.MarkEventsDeliveredAndQueueOutputTruncatedMarker(key, ids, daemon.now(), daemon.options.newID)
	if err != nil {
		return journal, err
	}
	if appended {
		daemon.signalOutboxFor(key)
	}
	return updated, nil
}

func (daemon *daemon) queueOutputTruncatedMarker(journal state.RunJournal) (state.RunJournal, error) {
	updated, appended, err := daemon.store.QueueOutputTruncatedMarker(journal.Key(), daemon.now(), daemon.options.newID)
	if err != nil {
		return journal, err
	}
	if appended {
		daemon.signalOutboxFor(journal.Key())
	}
	return updated, nil
}

func (daemon *daemon) deliverAppliedInputAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, bool, error) {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "applied" || intent.AcknowledgementDelivered || journal.HasPendingTransition(intent.RunningTransitionID) {
		return daemon.deliverAppliedControlAcknowledgement(ctx, journal)
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.AckID == intent.AckID {
			updated, err := daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
			return updated, true, err
		}
	}
	return journal, false, errors.New("applied input acknowledgement is not pending")
}

func (daemon *daemon) deliverAcknowledgement(ctx context.Context, journal state.RunJournal, acknowledgement protocol.CommandAcknowledgement) (state.RunJournal, error) {
	key := journal.Key()
	wasTerminal := journal.LocalState == "terminal_pending"
	requestContext, cancel := daemon.controlContext(ctx)
	err := daemon.control.AcknowledgeCommand(requestContext, acknowledgement.CommandID, acknowledgement)
	cancel()
	current, loadErr := daemon.store.LoadJournal(key)
	if loadErr != nil {
		return journal, loadErr
	}
	journal = current
	if !hasPendingAcknowledgement(journal, acknowledgement.AckID) {
		return journal, errOutboxChanged
	}
	if err != nil {
		if !wasTerminal && journal.LocalState == "terminal_pending" {
			return journal, errOutboxChanged
		}
		if updated, retired, retireErr := daemon.retireAcceptedTerminalAcknowledgement(journal, acknowledgement, err); retired || retireErr != nil {
			if retireErr != nil {
				return journal, retireErr
			}
			return updated, nil
		}
		return journal, daemon.handleAcknowledgementDeliveryError(ctx, journal, err)
	}
	if journal.LocalState == "terminal_pending" {
		daemon.releaseSlotOnce(key)
	}
	updated, err := daemon.markCommandAcknowledgementDelivered(key, acknowledgement)
	if err != nil {
		return journal, err
	}
	return updated, nil
}

func hasPendingAcknowledgement(journal state.RunJournal, acknowledgementID string) bool {
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.AckID == acknowledgementID {
			return true
		}
	}
	return false
}

func isTerminalTransition(stateName string) bool {
	switch stateName {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (daemon *daemon) retireOrdinaryTerminalOutbox(journal state.RunJournal) (state.RunJournal, error) {
	if len(journal.PendingEvents) > 0 {
		eventIDs := make([]string, len(journal.PendingEvents))
		for index, event := range journal.PendingEvents {
			eventIDs[index] = event.EventID
		}
		updated, err := daemon.store.MarkEventsDelivered(journal.Key(), eventIDs)
		if err != nil {
			return state.RunJournal{}, err
		}
		journal = updated
	}
	transitionIDs := make([]string, 0, len(journal.PendingTransitions))
	for _, transition := range journal.PendingTransitions {
		if !isTerminalTransition(transition.State) {
			transitionIDs = append(transitionIDs, transition.TransitionID)
		}
	}
	if len(transitionIDs) == 0 {
		return journal, nil
	}
	return daemon.store.MarkTransitionsDelivered(journal.Key(), transitionIDs)
}

func terminalPredecessorUnavailable(err error) bool {
	return control.IsOwnershipLost(err) || control.IsTerminalGraceExpired(err)
}

func (daemon *daemon) resolveTerminal(journal state.RunJournal, verdict string) (state.RunJournal, error) {
	updated, err := daemon.store.ResolveTerminal(journal.Key(), verdict, daemon.now())
	if err != nil {
		return state.RunJournal{}, err
	}
	daemon.releaseSlotOnce(journal.Key())
	return updated, nil
}

func (daemon *daemon) handleTerminalDeliveryError(journal state.RunJournal, err error) error {
	if journal.LocalState == "terminal_pending" {
		verdict := ""
		switch {
		case control.IsOwnershipLost(err):
			verdict = state.TerminalVerdictOwnershipLost
		case control.IsTerminalGraceExpired(err):
			verdict = state.TerminalVerdictGraceExpired
		}
		if verdict != "" {
			updated, resolveErr := daemon.store.ResolveTerminalForCleanup(journal.Key(), verdict, daemon.now())
			if resolveErr != nil {
				return resolveErr
			}
			if cleanupErr := daemon.scheduleCleanup(context.Background(), updated); cleanupErr != nil {
				return cleanupErr
			}
			return err
		}
	}
	return err
}

func (daemon *daemon) handleAcknowledgementDeliveryError(ctx context.Context, journal state.RunJournal, err error) error {
	if journal.LocalState == "terminal_pending" {
		return daemon.handleTerminalDeliveryError(journal, err)
	}
	return daemon.handleOrdinaryDeliveryError(ctx, journal, err, "ack ownership lost")
}

func (daemon *daemon) retireAcceptedTerminalAcknowledgement(journal state.RunJournal, acknowledgement protocol.CommandAcknowledgement, err error) (state.RunJournal, bool, error) {
	if journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || (!control.IsOwnershipLost(err) && !control.IsTerminalGraceExpired(err) && !invalidatedSupervisoryAcknowledgement(journal, acknowledgement, err)) {
		return journal, false, nil
	}
	updated, markErr := daemon.markCommandAcknowledgementDelivered(journal.Key(), acknowledgement)
	if markErr != nil {
		return state.RunJournal{}, false, markErr
	}
	return updated, true, nil
}

func (daemon *daemon) markCommandAcknowledgementDelivered(key state.RunKey, acknowledgement protocol.CommandAcknowledgement) (state.RunJournal, error) {
	keyForCommand := commandKey{run: key, id: acknowledgement.CommandID}
	daemon.commandReceiptMu.Lock()
	daemon.mu.Lock()
	for requestID, tombstones := range daemon.commandRequests {
		if tombstones == nil {
			tombstones = make(map[commandKey]struct{})
		}
		tombstones[keyForCommand] = struct{}{}
		daemon.commandRequests[requestID] = tombstones
	}
	daemon.mu.Unlock()
	daemon.commandReceiptMu.Unlock()
	var (
		updated state.RunJournal
		err     error
	)
	if daemon.options.markCommandAcknowledgementsDelivered != nil {
		updated, err = daemon.options.markCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})
	} else {
		updated, err = daemon.store.MarkCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})
	}
	if err != nil {
		return state.RunJournal{}, err
	}
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	if queued := daemon.queuedCommands[keyForCommand]; queued != nil {
		queued.delivered = true
		if queued.completed {
			delete(daemon.queuedCommands, keyForCommand)
		}
	}
	daemon.mu.Unlock()
	return updated, nil
}

func (daemon *daemon) handleOrdinaryDeliveryError(ctx context.Context, journal state.RunJournal, err error, ownershipReason string) error {
	if control.IsOwnershipLost(err) && !restartOutboxRecovery(ctx) {
		daemon.terminateForLease(journal, ownershipReason)
	}
	return err
}

func restartOutboxRecovery(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(restartOutboxRecoveryContextKey{}).(bool)
	return value
}

func (daemon *daemon) activeRuns() []protocol.ActiveRun {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return nil
	}
	runs := make([]protocol.ActiveRun, 0, len(journals))
	for _, journal := range journals {
		stateName, include := activeRunState(journal)
		if include {
			runs = append(runs, protocol.ActiveRun{RunID: journal.RunID, Generation: journal.Generation, ClaimedRuntimeEpoch: journal.ClaimedRuntimeEpoch, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken, State: stateName})
		}
	}
	return runs
}

func activeRunState(journal state.RunJournal) (string, bool) {
	if journal.LeaseToken == "" || journal.LocalState == "stale" || journal.LocalState == "claiming" {
		return "", false
	}
	switch journal.LocalState {
	case "claimed", "running", "paused", "waiting_for_input", "cancelling":
		return journal.LocalState, true
	default:
		return "", false
	}
}

func (daemon *daemon) hasRun(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	_, exists := daemon.running[key]
	return exists
}
func (daemon *daemon) stopAll() {
	daemon.mu.Lock()
	processes := make([]Process, 0, len(daemon.running))
	for _, run := range daemon.running {
		run.cancelled = true
		if run.cancel != nil {
			run.cancel()
		}
		if run.process != nil {
			processes = append(processes, run.process)
		}
	}
	daemon.mu.Unlock()
	// Failed Ready-barrier cleanup has no worker waiting on the native session.
	// Give it one bounded in-process retry before the store becomes unavailable.
	daemon.retryBlockedNativeSessionCloses()
	for _, process := range processes {
		daemon.terminateProcessBounded(process, 0)
	}
}

// terminateProcessBounded makes shutdown independent of a Process
// implementation that fails to honour its cancellation context. The runner
// still owns physical reaping; the persisted marker remains for recovery when
// that cannot be confirmed during this daemon lifetime.
func (daemon *daemon) terminateProcessBounded(process Process, grace time.Duration) {
	if process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTerminateLimit)
	defer cancel()
	done := make(chan struct{}, 1)
	go func() {
		_ = process.Terminate(ctx, grace)
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-ctx.Done():
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

func (daemon *daemon) claimWithRetry(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	return daemon.claimWithRetryUntil(ctx, runID, request, time.Time{}, waitForRetry)
}

func (daemon *daemon) claimWithRetryWait(ctx context.Context, runID string, request protocol.ClaimRequest, waitFor func(context.Context, time.Duration) error) (protocol.ClaimResponse, error) {
	return daemon.claimWithRetryUntil(ctx, runID, request, time.Time{}, waitFor)
}

func (daemon *daemon) claimWithRetryUntil(ctx context.Context, runID string, request protocol.ClaimRequest, expiresAt time.Time, waitFor func(context.Context, time.Duration) error) (protocol.ClaimResponse, error) {
	delay := minimumInterval
	for {
		if !expiresAt.IsZero() && !daemon.now().Before(expiresAt) {
			return protocol.ClaimResponse{}, errAssignmentExpired
		}
		requestLimit := controlRequestLimit
		if !expiresAt.IsZero() {
			requestLimit = min(requestLimit, expiresAt.Sub(daemon.now()))
		}
		requestContext, cancel := context.WithTimeout(ctx, requestLimit)
		response, err := daemon.control.Claim(requestContext, runID, request)
		cancel()
		if err == nil {
			return response, err
		}
		if ctx.Err() != nil {
			return protocol.ClaimResponse{}, ctx.Err()
		}
		if !expiresAt.IsZero() && !daemon.now().Before(expiresAt) {
			return protocol.ClaimResponse{}, errAssignmentExpired
		}
		wait, shouldRetry := control.RetryDelay(ctx, err, delay)
		if !shouldRetry {
			return response, err
		}
		if wait < minimumInterval {
			wait = minimumInterval
		}
		if !expiresAt.IsZero() {
			wait = min(wait, expiresAt.Sub(daemon.now()))
		}
		if waitErr := waitFor(ctx, wait); waitErr != nil {
			return protocol.ClaimResponse{}, waitErr
		}
		delay = nextRetryDelay(delay)
	}
}

func retry(ctx context.Context, operation func() error) error {
	return retryWithWait(ctx, operation, waitForRetry)
}

func retryWithWait(ctx context.Context, operation func() error, waitFor func(context.Context, time.Duration) error) error {
	delay := minimumInterval
	for {
		if err := operation(); err == nil {
			return nil
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			wait, shouldRetry := control.RetryDelay(ctx, err, delay)
			if !shouldRetry {
				return err
			}
			if wait < minimumInterval {
				wait = minimumInterval
			}
			if waitErr := waitFor(ctx, wait); waitErr != nil {
				return waitErr
			}
			delay = nextRetryDelay(delay)
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextRetryDelay(delay time.Duration) time.Duration {
	if delay < retryMaximum/2 {
		return delay * 2
	}
	return retryMaximum
}
