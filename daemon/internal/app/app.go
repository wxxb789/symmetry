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
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	minimumInterval       = time.Second
	maximumInterval       = time.Minute
	leaseSafetyMargin     = 5 * time.Second
	retryMaximum          = 30 * time.Second
	terminalGrace         = 8 * time.Minute
	controlRequestLimit   = 15 * time.Second
	maxJSONLRecordBytes   = 256 * 1024
	maxSemanticEventBytes = 64 * 1024
	rawOutputChunkBytes   = 32 * 1024
)

var (
	errAssignmentExpired    = errors.New("assignment expired")
	errLeaseDeadlineReached = errors.New("lease renewal deadline reached")
	errOutboxChanged        = errors.New("outbox changed during delivery")
)

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
	markCommandAcknowledgementsDelivered       func(state.RunKey, []string) (state.RunJournal, error)
	queueCancelledTransitionAndAcknowledgement func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
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

	store              *state.Store
	control            ControlAPI
	workspace          workspace.Service
	start              StartProcess
	runtimeID          string
	runtimeEpoch       int64
	leaseDuration      time.Duration
	pollEvery          time.Duration
	heartbeatEvery     time.Duration
	running            map[state.RunKey]*runningRun
	slots              chan struct{}
	mu                 sync.Mutex
	commandReceiptMu   sync.Mutex
	workers            sync.WaitGroup
	background         context.Context
	backgroundCancel   context.CancelFunc
	backgroundStop     bool
	backgroundWG       sync.WaitGroup
	terminalReleaseWG  sync.WaitGroup
	commandWG          sync.WaitGroup
	snapshotWG         sync.WaitGroup
	outboxWake         chan struct{}
	outboxRetry        map[state.RunKey]outboxRetry
	cleanupWake        chan struct{}
	cleanupQueued      map[state.RunKey]struct{}
	cleanupRetry       map[state.RunKey]time.Time
	commandWake        chan struct{}
	commandQueue       []*queuedCommand
	queuedCommands     map[commandKey]*queuedCommand
	commandLanes       map[state.RunKey]*commandLane
	commandRequests    map[uint64]map[commandKey]struct{}
	nextCommandRequest uint64
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
	inputMu         sync.Mutex
	process         Process
	prepared        workspace.Prepared
	output          *agentOutput
	starting        bool
	claimed         bool
	cancel          context.CancelFunc
	cancelled       bool
	cancelCommandID string
	stale           bool
	terminal        bool
	terminalizing   int
	slotHeld        bool
	cleanupBlocked  bool
	renewCancel     context.CancelFunc
	renewCancelID   uint64
	terminalCancel  context.CancelFunc
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

	// Registration is complete before liveness starts, but reconciliation can
	// block on the control plane. Keep lease maintenance alive while it does.
	daemon.reconcile(ctx)

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
				daemon.reconcile(ctx)
			}
			trigger()
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

func (daemon *daemon) initialize(ctx context.Context) error {
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
	daemon.slots = make(chan struct{}, daemon.config.Runtime.Capacity)
	instanceID, idErr := daemon.options.newID()
	if idErr != nil {
		return fmt.Errorf("generate daemon instance ID: %w", idErr)
	}
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	structuredInput := profile.InputMode == config.InputModeJSON
	registrationRequest := protocol.SessionRegistrationRequest{
		Runtimes: []protocol.RuntimeRegistration{{
			RuntimeKey: daemon.config.Runtime.RuntimeKey, Name: daemon.config.Runtime.Name,
			Capacity: daemon.config.Runtime.Capacity, AgentProfile: daemon.config.Runtime.AgentProfile,
			Workspace: daemon.config.Runtime.Workspace,
			Capabilities: protocol.RuntimeCapabilities{
				StructuredInput: structuredInput,
				ProviderAccess:  profile.ProviderAccess,
			},
		}},
	}
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
		recoveries = append(recoveries, journal)
		if journal.PID > 0 && !daemon.terminatePersistedProcessWithRetry(rootContext, journal.Key()) {
			return rootContext.Err()
		}
	}
	for _, journal := range recoveries {
		if daemon.hasRun(journal.Key()) {
			continue
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

func restartInputRecoveryRequired(journal state.RunJournal) bool {
	if journal.InputCommandIntent == nil {
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
	return daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", map[string]string{
		"stage": "daemon_restart",
		"error": "input command recovery cannot safely replay stdin",
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
	daemon.scheduleSnapshotForRequest(requestID, snapshot)
}

func (daemon *daemon) reconcile(ctx context.Context) {
	if err := daemon.recoverUnresolvedInputIntents(ctx); err != nil {
		daemon.log.Warn("recover_input_command_intents_failed", "error", err)
		return
	}
	journals, err := daemon.store.ListJournals()
	if err != nil {
		daemon.log.Error("list_journals_failed", "error", err)
		return
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
		return
	}
	for _, decision := range response.Decisions {
		key := state.RunKey{RunID: decision.RunID, Generation: decision.Generation}
		if decision.Decision == protocol.ReconcileCancel {
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
	daemon.scheduleSnapshotForRequest(requestID, protocol.RuntimeSnapshot{Assignments: response.Assignments, Commands: response.Commands})
}

func isReconcileState(localState string) bool {
	switch localState {
	case "claimed", "running", "waiting_for_input", "cancelling":
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
	if journal.PID > 0 && daemon.options.terminatePersist != nil {
		if err := daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity); err != nil {
			daemon.log.Warn("stop_recovered_process_failed", "run_id", journal.RunID, "error", err)
			return
		}
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
	if !operatorCancelled && !stale && !contextCancelled {
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
	sink := execution.SinkFunc(func(_ context.Context, event execution.Event) error {
		return output.handle(daemon, key, event)
	})
	process, err := daemon.start(ctx, execution.Invocation{Program: profile.Command, Args: profile.Args, Dir: prepared.Path, Env: environment, InitialInput: input, CloseInputAfterInitial: !profile.Interactive}, sink)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "start_agent", err)
		}
		return
	}
	daemon.mu.Lock()
	active = daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		_ = process.Terminate(context.Background(), 0)
		_ = process.Wait()
		return
	}
	active.process = process
	active.prepared = prepared
	active.output = output
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
	return inputRecord(protocol.AgentInputRecordTaskInput, work.Goal, work.Input, resolved)
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
	reference, err := url.Parse(access.Path)
	if err != nil {
		return nil, errors.New("provider access path is invalid")
	}
	resolved := *access
	resolved.Path = base.ResolveReference(reference).String()
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
	format   config.EventFormat
	parser   *jsonlParser
	redactor *streamingTokenRedactor
	stdoutAt time.Time
	flushed  bool
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
		if kind == "waiting_for_input" {
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
	return daemon.queueEvent(key, "output", payload, event.At)
}

func (daemon *daemon) queueEvent(key state.RunKey, kind string, payload json.RawMessage, at time.Time) error {
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return err
	}
	_, err = daemon.store.QueueEvent(key, protocol.RunEvent{EventID: id, Sequence: journal.LastEventSequence + 1, Kind: kind, OccurredAt: at, Payload: payload})
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
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
	if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", map[string]string{"stage": stage, "error": cause.Error()}); err != nil {
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
	output := (*agentOutput)(nil)
	if active != nil {
		process = active.process
		output = active.output
	}
	daemon.mu.Unlock()
	if active == nil || process == nil {
		return
	}
	result := process.Wait()
	if output != nil {
		if err := output.flush(daemon, key, daemon.now()); err != nil && result.SinkError == nil {
			result.SinkError = err
		}
	}
	active.inputMu.Lock()
	daemon.mu.Lock()
	cancelled := active.cancelled
	stale := active.stale
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
		if result.Success() {
			if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "completed", map[string]any{"exit_code": result.ExitCode}); err != nil {
				daemon.log.Error("queue_completed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
		} else {
			if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "failed", map[string]any{"exit_code": result.ExitCode, "error": errorText(result.WaitError)}); err != nil {
				daemon.log.Error("queue_failed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "stage", "process_exit", "error", err)
			}
		}
	}
	active.inputMu.Unlock()
	daemon.releaseCleanupAfterProcessExit(key)
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
	if command.Kind == "cancel" {
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
		process := active.process
		cancel := active.cancel
		terminalReserved := process == nil && cancel != nil
		if terminalReserved {
			active.terminalizing++
		}
		daemon.mu.Unlock()
		if terminalReserved {
			defer daemon.releaseTerminalReservation(key)
		}
		var terminateErr error
		if process != nil {
			terminateErr = process.Terminate(ctx, 2*time.Second)
		} else if cancel != nil {
			cancel()
		}
		if terminateErr != nil {
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
	outcome := "rejected"
	switch command.Kind {
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
		if process.WriteInput(input) != nil {
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
	return active != nil && active.process != nil && !active.starting && !active.stale && !active.terminal && active.terminalizing == 0
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
	daemon.mu.Lock()
	active := daemon.running[journal.Key()]
	process := Process(nil)
	if active != nil {
		active.cancelled = true
		active.stale = true
		process = active.process
		if active.cancel != nil {
			active.cancel()
		}
	}
	daemon.mu.Unlock()
	if process != nil {
		_ = process.Terminate(context.Background(), 0)
	} else {
		if active == nil {
			daemon.stopRecoveredJournal(journal, reason)
		}
	}
	_, _ = daemon.store.SetLocalState(journal.Key(), "stale")
}

func (daemon *daemon) flushAll(ctx context.Context) {
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
			terminal := journal.LocalState == "terminal_pending"
			if journal.LocalState == "cleanup_pending" {
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
			if current.LocalState == "terminal_pending" {
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
			if current.LocalState == "terminal_pending" {
				daemon.recordOutboxFailure(ctx, current, err)
			}
			break
		}
	}
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
	if journal.LocalState != "terminal_pending" {
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
	if cleanupReady && active.process == nil {
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
	if journal.LocalState == "stale" {
		return nil
	}
	if journal.LocalState == "cleanup_pending" {
		return daemon.scheduleCleanup(ctx, journal)
	}
	if daemon.isStarting(key) {
		return nil
	}
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict != "" && journal.TerminalVerdict != state.TerminalVerdictAccepted {
		updated, err := daemon.store.ResolveTerminalForCleanup(key, journal.TerminalVerdict, daemon.now())
		if err != nil {
			return err
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	predecessorsUnavailable := false
	updated, err := daemon.deliverEventsBeforeAppliedInputAcknowledgement(ctx, journal)
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
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 {
		updated, err := daemon.enterCleanupPending(journal)
		if err != nil {
			return err
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	return nil
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
	return daemon.deliverEvents(ctx, journal, journal.PendingEvents)
}

func (daemon *daemon) deliverEvents(ctx context.Context, journal state.RunJournal, events []protocol.RunEvent) (state.RunJournal, error) {
	if len(events) == 0 {
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
	updated, err := daemon.store.MarkEventsDelivered(key, ids)
	if err != nil {
		return journal, err
	}
	return updated, nil
}

func (daemon *daemon) deliverAppliedInputAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, bool, error) {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "applied" || intent.AcknowledgementDelivered || journal.HasPendingTransition(intent.RunningTransitionID) {
		return journal, false, nil
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
	if journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || (!control.IsOwnershipLost(err) && !control.IsTerminalGraceExpired(err)) {
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
	case "claimed", "running", "waiting_for_input", "cancelling":
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
	for _, process := range processes {
		_ = process.Terminate(context.Background(), 0)
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
