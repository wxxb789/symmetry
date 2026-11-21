// Package app owns the daemon control loop.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/linuxpreflight"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	minimumInterval               = time.Second
	maximumInterval               = time.Minute
	reconcileRetryMax             = 30 * time.Second
	leaseSafetyMargin             = 5 * time.Second
	retryMaximum                  = 30 * time.Second
	terminalGrace                 = 8 * time.Minute
	controlRequestLimit           = 15 * time.Second
	nativeUsageRetryLimit         = 5
	nativeUsageRetryWindow        = 2 * time.Minute
	linuxV2AdmissionStatusTimeout = 2 * time.Second
	// Bound the full owner snapshot so per-owner status deadlines cannot add up
	// without limit when execution callbacks have no caller context.
	linuxV2AdmissionSnapshotTimeout = 5 * time.Second
	maxJSONLRecordBytes             = 256 * 1024
	maxSemanticEventBytes           = 64 * 1024
	rawOutputChunkBytes             = 32 * 1024
	maxPendingOutputBytes           = 2 << 20
	inputWriteTimeout               = 60 * time.Second
	shutdownTerminateLimit          = 2 * time.Second
)

const controlUTCTimestampLayout = "2006-01-02T15:04:05.000000Z07:00"

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

// goalSessionStopAPI is deliberately separate from the original Goal delivery
// interface so older embedded test/control implementations fail closed through
// the durable outbox instead of being treated as a successful release.
type goalSessionStopAPI interface {
	MarkHarnessSessionStopped(context.Context, string, control.GoalSessionStoppedRequest) (control.GoalSessionStoppedReceipt, error)
}

// goalSubjectWorkspace is the immutable artifact boundary required only by
// Goal execution. Legacy work intentionally retains workspace.Service's
// configured-ref behavior.
type goalSubjectWorkspace interface {
	PrepareSubject(context.Context, string, workspace.RunRef, protocol.Subject) (workspace.SubjectWorkspace, error)
	DeriveSubject(context.Context, workspace.Prepared, string) (protocol.Subject, error)
}

// linuxV2CloneWorkspace is the only workspace path permitted for new Linux
// Pi/OpenCode owners. Its methods keep all source Git operations in A-owned
// storage and never treat a B-owned .git directory as ownership evidence.
type linuxV2CloneWorkspace interface {
	PlanLinuxCloneIntent(string, workspace.RunRef, protocol.Subject) (authority.LinuxCloneIntent, error)
	PrepareLinuxCloneSeed(context.Context, authority.LinuxCloneIntent) (authority.LinuxCloneSeedReceipt, error)
	PrepareLinuxClone(context.Context, authority.LinuxCloneGrant) (workspace.Prepared, error)
	RecoverLinuxCloneReuse(context.Context, authority.LinuxCloneReuseGrant) (workspace.Prepared, error)
	ValidateLinuxCloneCheckout(authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt) error
	PromoteLinuxClone(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, authority.LinuxClonePromotionIntent, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error)
	ReadExistingLinuxClonePromotion(context.Context, authority.LinuxCloneIntent, authority.LinuxClonePromotionIntent, state.RunJournal, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error)
	RemoveLinuxCloneTarget(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, state.RunJournal, workspace.LinuxCloneTargetDeleter) (authority.LinuxCloneTargetRemovalProof, error)
	CleanupLinuxClone(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, state.RunJournal) (authority.LinuxCloneCleanupProof, error)
	AbortLinuxCloneBeforeCheckout(context.Context, authority.LinuxCloneIntent, *authority.LinuxCloneSeedReceipt,
		*authority.LinuxCgroupPreGateReleaseProof, *authority.LinuxCgroupReservationAbortProof,
		*authority.SupervisorHandoffReleaseProof) (authority.LinuxCloneAbortProof, error)
}

// goalRecoveredSubjectWorkspace is the retained-session counterpart to
// goalSubjectWorkspace. Resume must recover the original daemon-owned
// workspace; it must never materialize a new worktree for the new Run.
type goalRecoveredSubjectWorkspace interface {
	goalSubjectWorkspace
	RecoverSubject(context.Context, string, workspace.RunRef, string, protocol.Subject) (workspace.SubjectWorkspace, error)
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

// processResultCompletion is the optional completion witness for a Process
// whose Wait method may block. The execution runner exposes this barrier so the
// app can wait for completion without starting an unowned Wait goroutine.
type processResultCompletion interface {
	ResultDone() <-chan struct{}
}

// ProcessFinalizer is an optional post-exit boundary. Implementations must
// retry only durable containment receipt/release work and must return without
// waiting when the process result is not already complete.
type ProcessFinalizer interface {
	FinalizeContainment() error
}

// StartProcess permits deterministic runner tests without exposing os/exec to
// the control loop's callers.
type StartProcess func(context.Context, execution.Invocation, execution.Sink) (Process, error)

type linuxV2AdmissionStatusReader interface {
	LinuxV2LiveOwnerStatus(context.Context) (execution.LinuxV2LiveOwnerStatus, error)
}

var containmentOnlyRecovery = func(context.Context, config.Config) error {
	return errors.New("containment-only recovery is supported only on Linux amd64")
}

// RecoverContainmentOnly reconciles process ownership under the same exclusive
// StateDir lock as Run. Its platform implementation must not register, claim,
// schedule, launch native code, or drain the ordinary outbox.
func RecoverContainmentOnly(ctx context.Context, value config.Config) error {
	if ctx == nil {
		return errors.New("containment-only recovery context must not be nil")
	}
	return containmentOnlyRecovery(ctx, value)
}

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
	harnessRegistry                            *harness.Registry
	processObserver                            func(state.RunKey, int, string, time.Time)
	containmentStopObserver                    func(state.RunKey, int, string, authority.StopReceipt) error
	notifications                              NotificationClient
	logWriter                                  io.Writer
	clock                                      func() time.Time
	localClock                                 func() time.Time
	newTimer                                   func(time.Duration) deadlineTimer
	newID                                      func() (string, error)
	newMachineToken                            func() (string, error)
	terminatePersist                           func(pid int, identity string) error
	terminatePersistAuthority                  func(pid int, identity string, value *authority.Supervisor) (authority.StopReceipt, error)
	terminatePersistAuthorityWithScanSeal      func(pid int, identity string, value *authority.Supervisor) (authority.StopReceipt, uint64, error)
	recordWorkspace                            func(state.RunKey, string) (state.RunJournal, error)
	recordProcess                              func(state.RunKey, int, string, time.Time) (state.RunJournal, error)
	recordProcessAuthority                     func(state.RunKey, int, string, authority.Supervisor, time.Time) (state.RunJournal, error)
	markContainmentUnproven                    func(state.RunKey, int, string) (state.RunJournal, error)
	clearContainmentUnproven                   func(state.RunKey, int, string) (state.RunJournal, error)
	recordContainmentStopReceipt               func(state.RunKey, int, string, authority.StopReceipt) (state.RunJournal, error)
	prepareSupervisorHandoff                   func(state.RunKey, authority.SupervisorHandoff) (state.RunJournal, error)
	bindSupervisorHandoff                      func(state.RunKey, authority.SupervisorHandoff, int, string) (state.RunJournal, error)
	commitSupervisorHandoff                    func(state.RunKey, authority.SupervisorHandoff, time.Time) (state.RunJournal, error)
	recordSupervisorHandoffStopReceipt         func(state.RunKey, authority.SupervisorHandoff, authority.StopReceipt) (state.RunJournal, error)
	clearSupervisorHandoff                     func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) (state.RunJournal, error)
	clearSupervisorHandoffAfterAbort           func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) (state.RunJournal, error)
	queueTerminalTransition                    func(state.RunKey, protocol.StateTransitionRequest, time.Time) (state.RunJournal, error)
	queueGoalUsage                             func(state.RunKey, protocol.Usage) (state.RunJournal, error)
	markGoalSessionAttachDeliveryReady         func(state.RunKey, string) (state.RunJournal, error)
	discardUnreadyGoalSessionAttachDelivery    func(state.RunKey, string) (state.RunJournal, error)
	abortGoalSessionLaunchBeforeNativeStart    func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	markGoalSessionUncertain                   func(state.GoalSessionKey, string) (state.GoalSessionJournal, error)
	markCommandAcknowledgementsDelivered       func(state.RunKey, []string) (state.RunJournal, error)
	queueTerminalTransitionAndAcknowledgement  func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
	queueCancelledTransitionAndAcknowledgement func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
	retainWorkspace                            func(state.RunKey) (state.RunJournal, error)
	loadGoalSession                            func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	closeGoalSession                           func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	clearProcessDetails                        func(state.RunKey, int, string) (state.RunJournal, error)
	nativeCloseRetryPublished                  func()
	linuxV2NativeOwnerFactory                  linuxV2NativeOwnerFactory
}

// WithHTTPClient replaces the HTTP transport used for production clients.
func WithHTTPClient(client *http.Client) Options {
	return func(value *options) { value.httpClient = client }
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
	if requiresLinuxV2NativeOwner(harness.Kind(value.Runtime.HarnessKind)) {
		stateDir, err := linuxpreflight.NormalizeOwnerStateDir(value.StateDir)
		if err != nil {
			return fmt.Errorf("normalize Linux v2 StateDir against root working directory: %w", err)
		}
		value.StateDir = stateDir
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	settings := options{
		httpClient:                            &http.Client{Timeout: 30 * time.Second},
		logWriter:                             os.Stderr,
		clock:                                 func() time.Time { return time.Now().UTC() },
		localClock:                            time.Now,
		newTimer:                              func(delay time.Duration) deadlineTimer { return systemTimer{timer: time.NewTimer(delay)} },
		newID:                                 state.NewDaemonInstanceID,
		newMachineToken:                       state.NewMachineToken,
		terminatePersist:                      terminatePersistedProcess,
		terminatePersistAuthority:             terminatePersistedProcessWithAuthority,
		terminatePersistAuthorityWithScanSeal: terminatePersistAuthorityWithScanSeal,
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
		config: value, options: settings, harnessRegistry: settings.harnessRegistry,
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
	machineID           string
	runtimeID           string
	runtimeEpoch        int64
	leaseDuration       time.Duration
	pollEvery           time.Duration
	heartbeatEvery      time.Duration
	running             map[state.RunKey]*runningRun
	slots               chan struct{}
	mu                  sync.Mutex
	commandReceiptMu    sync.Mutex
	eventReceiptMu      sync.Mutex
	storeLifetimeMu     sync.RWMutex
	storeClosing        bool
	workers             sync.WaitGroup
	background          context.Context
	backgroundCancel    context.CancelFunc
	backgroundStop      bool
	backgroundWG        sync.WaitGroup
	terminalReleaseWG   sync.WaitGroup
	failedStartWatchWG  sync.WaitGroup
	// linuxV2AdmissionMu serializes live-owner snapshots and their short State
	// admission/CAS calls. Never hold daemon.mu while entering Store; helper and
	// pidfd observations are collected before the State call.
	linuxV2AdmissionMu sync.Mutex
	// linuxV2LaunchPhaseMu serializes only the pre-RELEASE launch phase. It is
	// released after exact live RELEASE proof or completed abort handling.
	linuxV2LaunchPhaseMu    sync.Mutex
	linuxV2LaunchPhaseOwner state.RunKey
	linuxV2LaunchPhaseDone  chan struct{}
	linuxV2OwnerInitMu      sync.Mutex
	linuxV2Owner            linuxV2NativeOwner
	recoveryOnly            bool
	commandWG               sync.WaitGroup
	snapshotWG              sync.WaitGroup
	outboxWake              chan struct{}
	outboxRetry             map[state.RunKey]outboxRetry
	cleanupWake             chan struct{}
	cleanupQueued           map[state.RunKey]struct{}
	cleanupRetry            map[state.RunKey]time.Time
	recoveredStops          map[state.RunKey]recoveredProcessStop
	retainedWorkspaces      map[state.RunKey]struct{}
	clockSkewWarned         bool
	commandWake             chan struct{}
	commands                commandQueue
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
			daemon.failedStartWatchWG.Wait()
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
	daemon.commands = newCommandQueue()
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
	daemon.machineID = identity.MachineID
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
		// A registration is an authority boundary. Physical reconciliation may
		// remain pending after a durable unknown-outcome fallback, but retention,
		// session uncertainty, usage, and terminal outbox persistence failures
		// must stop registration rather than become an in-memory warning.
		if !errors.Is(err, errRecoveryRegistrationReady) {
			return fmt.Errorf("recover unclosed Goal sessions: %w", err)
		}
		if daemon.log != nil {
			daemon.log.Warn("recover_unclosed_goal_sessions_pending", "error", err)
		}
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
	daemon.storeLifetimeMu.Lock()
	defer daemon.storeLifetimeMu.Unlock()
	daemon.storeClosing = true
	daemon.linuxV2OwnerInitMu.Lock()
	if daemon.linuxV2Owner != nil {
		_ = daemon.linuxV2Owner.Close()
		daemon.linuxV2Owner = nil
	}
	daemon.linuxV2OwnerInitMu.Unlock()
	if daemon.options.store == nil && daemon.store != nil {
		_ = daemon.store.Close()
	}
}

func appErrorOr(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}

func linuxNativeRuntimeRequiresGoalAdmission(harnessKind string) bool {
	return runtime.GOOS == "linux" &&
		(harnessKind == config.RuntimeHarnessPi || harnessKind == config.RuntimeHarnessOpenCode)
}

func (daemon *daemon) linuxV2LaunchPhasePendingFor(key state.RunKey) bool {
	if daemon == nil {
		return false
	}
	daemon.linuxV2LaunchPhaseMu.Lock()
	pending := daemon.linuxV2LaunchPhaseOwner != (state.RunKey{}) && daemon.linuxV2LaunchPhaseOwner != key
	daemon.linuxV2LaunchPhaseMu.Unlock()
	return pending
}

// acquireLinuxV2LaunchPhase allows only one new native run to cross the
// pre-RELEASE boundary at a time. Waiters keep their exact Control claims and
// stop waiting before their local lease safety margin; the gate ends at proven
// RELEASE rather than serializing the lifetime of released tasks.
func (daemon *daemon) acquireLinuxV2LaunchPhase(ctx context.Context, key state.RunKey, claim protocol.ClaimResponse) (func(), error) {
	if daemon == nil || ctx == nil || key.RunID == "" || key.Generation <= 0 {
		return nil, errors.New("Linux v2 launch phase requires a context and exact RunKey")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := daemon.linuxV2ClaimLeaseRemaining(key, claim)
		if remaining <= leaseSafetyMargin {
			return nil, errLeaseDeadlineReached
		}

		daemon.linuxV2LaunchPhaseMu.Lock()
		if daemon.linuxV2LaunchPhaseOwner == (state.RunKey{}) {
			done := make(chan struct{})
			daemon.linuxV2LaunchPhaseOwner = key
			daemon.linuxV2LaunchPhaseDone = done
			daemon.linuxV2LaunchPhaseMu.Unlock()

			var releaseOnce sync.Once
			return func() {
				releaseOnce.Do(func() {
					daemon.linuxV2LaunchPhaseMu.Lock()
					if daemon.linuxV2LaunchPhaseOwner == key && daemon.linuxV2LaunchPhaseDone == done {
						daemon.linuxV2LaunchPhaseOwner = state.RunKey{}
						daemon.linuxV2LaunchPhaseDone = nil
						close(done)
					}
					daemon.linuxV2LaunchPhaseMu.Unlock()
				})
			}, nil
		}
		if daemon.linuxV2LaunchPhaseOwner == key {
			daemon.linuxV2LaunchPhaseMu.Unlock()
			return nil, errors.New("RunKey already owns the Linux v2 launch phase")
		}
		done := daemon.linuxV2LaunchPhaseDone
		daemon.linuxV2LaunchPhaseMu.Unlock()
		if done == nil {
			return nil, errors.New("Linux v2 launch phase owner has no completion signal")
		}

		timer := daemon.timer(remaining - leaseSafetyMargin)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-done:
			timer.Stop()
		case <-timer.Chan():
			timer.Stop()
			return nil, errLeaseDeadlineReached
		}
	}
}

// withLinuxV2EffectReadyTarget checks the exact current claim immediately
// before an A-owned Git effect. The Store rechecks the fence, lease, lifecycle,
// cancellation, and uncertainty under its own lock; no daemon lock is held
// while the external effect runs.
func (daemon *daemon) withLinuxV2EffectReadyTarget(ctx context.Context, key state.RunKey, fence protocol.Fence, effect func() error) error {
	if daemon == nil || daemon.store == nil || ctx == nil || effect == nil {
		return errors.New("Linux v2 effect boundary requires a context and effect")
	}
	if daemon.recoveryOnly {
		return errors.New("containment-only recovery cannot create a Linux clone promotion ref")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := daemon.checkLinuxV2NativeAdmission(ctx, key, fence); err != nil {
		return fmt.Errorf("recheck exact Linux v2 target lease before A-owned effect: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return effect()
}

// checkLinuxV2TargetAdmission rejects local cancellation, shutdown, terminal
// ownership, and an expired local claim before an exact Store check or effect
// CAS. It snapshots under daemon.mu but never carries that lock into State.
func (daemon *daemon) checkLinuxV2TargetAdmission(key state.RunKey) error {
	if key.RunID == "" || key.Generation <= 0 {
		return errors.New("Linux v2 target admission requires an exact RunKey")
	}
	now := daemon.localNow()
	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		return errors.New("Linux v2 target RunKey is no longer owned by this App")
	}
	deadline := active.localLeaseDeadlineAt
	blocked := !active.claimed || active.cancelled || active.stale || active.cancelCommandID != "" ||
		active.nativeCancelCommandID != "" || active.nativeCancelTerminalObserved || active.nativeCancelTerminateErr != nil ||
		active.nativeStartTerminalObserved || active.nativeStartLeaseExpired || active.terminal || active.terminalizing != 0 ||
		active.nativeTerminalOwner || active.nativeStartTerminalOwner || active.leaseRenewalClosed ||
		active.nativeUsageRenewalBlocked || active.processExited || active.processStopWitness != nil ||
		active.processFinalizeOwner != nil || active.nativeCloseRetryRequired || active.nativeCloseRetrying || active.startFailure != nil
	daemon.mu.Unlock()
	if blocked {
		return errors.New("Linux v2 target RunKey is cancelled, stale, terminalizing, or otherwise unavailable")
	}
	if deadline.IsZero() || !now.Add(leaseSafetyMargin).Before(deadline) {
		return fmt.Errorf("Linux v2 target local lease is expired or within its safety margin: %w", errLeaseDeadlineReached)
	}
	return nil
}

// linuxV2LiveOwnerFences includes only exact process owners that the current
// App still owns, whose local lease is unexpired, and whose helper independently
// proves its pidfds, lease ACK, commit, and completed RELEASE. A run may still
// be finishing staged startup after RELEASE; pre-RELEASE or ambiguous runs are
// left out so State fails closed on their rows.
func (daemon *daemon) linuxV2LiveOwnerFences(ctx context.Context, excludedKey state.RunKey) (map[state.RunKey]protocol.Fence, error) {
	type candidate struct {
		key     state.RunKey
		active  *runningRun
		process Process
		session harness.Session
	}
	candidates := make([]candidate, 0)
	localNow := daemon.localNow()
	daemon.mu.Lock()
	for key, active := range daemon.running {
		if key == excludedKey || active == nil || isNilHarnessSession(active.nativeSession) ||
			!active.claimed || active.cancelled || active.stale ||
			active.cancelCommandID != "" || active.nativeCancelCommandID != "" || active.nativeCancelTerminalObserved ||
			active.nativeCancelTerminateErr != nil || active.nativeStartTerminalObserved || active.nativeStartLeaseExpired ||
			active.terminal || active.terminalizing != 0 || active.leaseRenewalClosed ||
			active.nativeUsageRenewalBlocked || active.processExited || active.processStopWitness != nil ||
			active.processFinalizeOwner != nil || active.nativeCloseRetryRequired || active.nativeCloseRetrying ||
			active.startFailure != nil || active.localLeaseDeadlineAt.IsZero() ||
			!localNow.Add(leaseSafetyMargin).Before(active.localLeaseDeadlineAt) {
			continue
		}
		candidates = append(candidates, candidate{
			key: key, active: active, process: active.process, session: active.nativeSession,
		})
	}
	daemon.mu.Unlock()

	liveOwners := make(map[state.RunKey]protocol.Fence, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		journal, err := daemon.store.LoadJournal(candidate.key)
		if err != nil {
			return nil, fmt.Errorf("read in-memory Linux v2 owner journal %s/%d: %w", candidate.key.RunID, candidate.key.Generation, err)
		}
		if journal.RuntimeID != daemon.runtimeID || journal.ClaimedRuntimeEpoch != daemon.runtimeEpoch ||
			journal.ContainmentAuthority == nil || journal.ContainmentAuthority.Version != authority.SupervisorVersionV2 ||
			journal.ContainmentAuthority.OwnerKind != authority.OwnerKindLinuxHelper || journal.ContainmentAuthority.StopReceipt != nil ||
			journal.ContainmentHandoff != nil || journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil ||
			journal.ContainmentCgroup.Gate == nil || !journal.ContainmentCgroup.LauncherGrantPossible ||
			journal.ContainmentCgroup.AbortProof != nil || journal.ContainmentCgroup.PreGateStopReceipt != nil ||
			journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
			journal.ContainmentUnproven || journal.TerminalState != "" || journal.TerminalVerdict != "" ||
			journal.NativeUsageRecoveryRequired || journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil ||
			journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent != nil ||
			journal.LinuxCloneTargetRemovalProof != nil || journal.LinuxCloneCleanupProof != nil || journal.LeaseToken == "" ||
			!journal.LeaseExpiresAt.After(daemon.now().Add(leaseSafetyMargin)) || !journal.HasProcessDetails() {
			return nil, fmt.Errorf("in-memory Linux v2 owner %s/%d does not match an active committed Journal", candidate.key.RunID, candidate.key.Generation)
		}
		switch journal.LocalState {
		case "claimed", "running", "paused", "waiting_for_input":
		default:
			return nil, fmt.Errorf("in-memory Linux v2 owner %s/%d is not in an active lease state", candidate.key.RunID, candidate.key.Generation)
		}
		pid, identity, err := linuxV2AdmissionTargetDetails(candidate.process, candidate.session)
		if err != nil || pid != journal.PID || identity != journal.ProcessIdentity ||
			pid != journal.ContainmentAuthority.TargetPID || identity != journal.ContainmentAuthority.TargetIdentity {
			return nil, errors.Join(fmt.Errorf("in-memory Linux v2 owner %s/%d differs from its exact Journal target", candidate.key.RunID, candidate.key.Generation), err)
		}
		liveStatusReader, ok := candidate.process.(linuxV2AdmissionStatusReader)
		if !ok && !isNilHarnessSession(candidate.session) {
			liveStatusReader, ok = candidate.session.(linuxV2AdmissionStatusReader)
		}
		if !ok {
			return nil, fmt.Errorf("in-memory Linux v2 owner %s/%d has no production liveness status API", candidate.key.RunID, candidate.key.Generation)
		}
		statusContext, cancel := context.WithTimeout(ctx, linuxV2AdmissionStatusTimeout)
		status, statusErr := liveStatusReader.LinuxV2LiveOwnerStatus(statusContext)
		cancel()
		if statusErr != nil {
			return nil, fmt.Errorf("verify in-memory Linux v2 owner %s/%d liveness: %w", candidate.key.RunID, candidate.key.Generation, statusErr)
		}
		helper := *journal.ContainmentCgroup.Helper
		if status.RunKey != candidate.key || status.Fence != journal.Fence() ||
			!status.Target.Equal(authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}) ||
			!status.Helper.Equal(helper) || status.Helper.Validate() != nil || status.Target.Validate() != nil ||
			status.Helper.PID != journal.ContainmentAuthority.SupervisorPID ||
			status.Helper.Identity != journal.ContainmentAuthority.SupervisorIdentity ||
			!status.HelperPIDFDLive || !status.TargetPIDFDLive || status.LeaseSequence == 0 || !status.LeaseLive ||
			status.OwnerLost || !status.CommitAcknowledged || status.StopRequested || status.StopAttempted ||
			!status.ReleaseAttempted || !status.ReleaseSucceeded || status.ReleaseInFlight || status.ReleaseUncertain || status.Terminal {
			return nil, fmt.Errorf("in-memory Linux v2 owner %s/%d is not a proven live committed-and-released owner", candidate.key.RunID, candidate.key.Generation)
		}

		currentLocalNow := daemon.localNow()
		daemon.mu.Lock()
		current := daemon.running[candidate.key]
		stillActive := current == candidate.active && !isNilHarnessSession(current.nativeSession) &&
			current.claimed && !current.cancelled && !current.stale &&
			current.cancelCommandID == "" && current.nativeCancelCommandID == "" && !current.nativeCancelTerminalObserved &&
			current.nativeCancelTerminateErr == nil && !current.nativeStartTerminalObserved && !current.nativeStartLeaseExpired &&
			!current.terminal && current.terminalizing == 0 && !current.leaseRenewalClosed &&
			!current.nativeUsageRenewalBlocked && !current.processExited && current.processStopWitness == nil &&
			current.processFinalizeOwner == nil && !current.nativeCloseRetryRequired && !current.nativeCloseRetrying &&
			current.startFailure == nil && !current.localLeaseDeadlineAt.IsZero() &&
			currentLocalNow.Add(leaseSafetyMargin).Before(current.localLeaseDeadlineAt)
		currentProcess := Process(nil)
		currentSession := harness.Session(nil)
		if stillActive {
			currentProcess = current.process
			currentSession = current.nativeSession
		}
		daemon.mu.Unlock()
		if !stillActive || isNilProcess(currentProcess) && isNilHarnessSession(currentSession) {
			return nil, fmt.Errorf("in-memory Linux v2 owner %s/%d changed during admission snapshot", candidate.key.RunID, candidate.key.Generation)
		}
		currentPID, currentIdentity, currentErr := linuxV2AdmissionTargetDetails(currentProcess, currentSession)
		if currentErr != nil || currentPID != pid || currentIdentity != identity {
			return nil, errors.Join(fmt.Errorf("in-memory Linux v2 owner %s/%d changed its process identity during admission snapshot", candidate.key.RunID, candidate.key.Generation), currentErr)
		}
		liveOwners[candidate.key] = journal.Fence()
	}
	return liveOwners, nil
}

func (daemon *daemon) confirmLinuxV2LiveRelease(ctx context.Context, key state.RunKey, fence protocol.Fence) error {
	if key.RunID == "" || key.Generation <= 0 || fence.Generation != key.Generation {
		return errors.New("Linux v2 RELEASE confirmation requires the exact RunKey fence")
	}
	return daemon.withLinuxV2NativeAdmission(ctx, state.RunKey{}, func(liveOwners map[state.RunKey]protocol.Fence) error {
		if observed, ok := liveOwners[key]; !ok || observed != fence {
			return errors.New("returned Linux v2 native session has no exact live lease-ACKed RELEASE proof")
		}
		return nil
	})
}

func linuxV2AdmissionTargetDetails(process Process, session harness.Session) (int, string, error) {
	if !isNilProcess(process) {
		return processDetails(process)
	}
	staged, ok := session.(harness.StagedSession)
	if !ok || isNilHarnessSession(session) {
		return 0, "", errors.New("live Linux v2 native session does not expose its process identity")
	}
	pid, identity := staged.ProcessDetails()
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		return 0, "", errors.New("live Linux v2 native session identity is incomplete")
	}
	return pid, identity, nil
}

// mergeEnvironmentOverrides removes ambient values for every explicitly
// trusted key before appending the root-config-derived B environment. This
// prevents an allowlisted A HOME/XDG value from competing with B's fixed paths.
func mergeEnvironmentOverrides(environment, overrides []string) ([]string, error) {
	for index, entry := range overrides {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" || strings.ContainsRune(entry, '\x00') {
			return nil, errors.New("trusted native environment override is invalid")
		}
		for _, previous := range overrides[:index] {
			previousKey, _, _ := strings.Cut(previous, "=")
			if previousKey == key {
				return nil, fmt.Errorf("trusted native environment overrides duplicate %q", key)
			}
		}
	}
	merged := environment[:0]
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			return nil, errors.New("native environment entry is invalid")
		}
		replaced := false
		for _, override := range overrides {
			overrideKey, _, _ := strings.Cut(override, "=")
			if key == overrideKey {
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, entry)
		}
	}
	return append(merged, overrides...), nil
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

// localNow is intentionally separate from now. LeaseRemainingMS is a
// relative Control measurement, so its local deadline must use the daemon's
// monotonic-capable local clock rather than a wall-clock projection that may
// be skewed from Control.
func (daemon *daemon) localNow() time.Time {
	if daemon.options.localClock != nil {
		return daemon.options.localClock()
	}
	return time.Now()
}

// formatControlUTCTimestamp matches Control's utc_datetime_usec boundary.
// Sub-microsecond precision cannot be represented by that contract, so it is
// truncated after normalizing to UTC; the fixed-width layout retains trailing
// zeroes that RFC3339Nano would omit.
func formatControlUTCTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format(controlUTCTimestampLayout)
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

func (daemon *daemon) failedStartFinalizationContext() context.Context {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.background != nil {
		return daemon.background
	}
	return context.Background()
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
	recoveryPending := false
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
		if journal.ContainmentUnproven || journal.ContainmentHandoff != nil {
			// Process containment recovery owns this lineage until the exact
			// marker/handoff is cleared. Do not advertise it as a live reconcile
			// candidate while a new assignment could overlap the old owner.
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
		if !errors.Is(err, errRecoveryPending) {
			daemon.finishCommandRequest(requestID)
			return false
		}
		recoveryPending = true
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
	return !recoveryPending
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

func (daemon *daemon) startAssignment(ctx context.Context, assignment protocol.Assignment) {
	if ctx.Err() != nil {
		return
	}
	key := state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}
	if assignment.RunID == "" || assignment.Generation <= 0 {
		return
	}
	if journal, err := daemon.store.LoadJournal(key); err == nil {
		if journal.LocalState == "cleanup_pending" || journal.ContainmentUnproven || journal.ContainmentHandoff != nil ||
			linuxNativeRuntimeRequiresGoalAdmission(daemon.config.Runtime.HarnessKind) && hasLinuxV2ContainmentState(journal) {
			return
		}
	}
	if linuxNativeRuntimeRequiresGoalAdmission(daemon.config.Runtime.HarnessKind) && !daemon.linuxV2LaunchPhasePendingFor(key) {
		if err := daemon.checkLinuxV2NativeAdmissionBeforeClaim(ctx); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("linux_v2_native_assignment_blocked_by_owner_recovery", "run_id", key.RunID,
					"generation", key.Generation, "error", err)
			}
			return
		}
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
			journal, err := daemon.persistStaleAndEnqueueCleanup(key)
			if err != nil {
				if daemon.log != nil {
					daemon.log.Warn("persist_startup_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
				}
				return
			}
			_ = daemon.scheduleCleanup(context.Background(), journal)
			return
		}
		if preparedOK {
			if journal, err := daemon.persistStaleAndEnqueueCleanup(key); err == nil {
				_ = daemon.scheduleCleanup(context.Background(), journal)
				return
			} else if daemon.log != nil {
				daemon.log.Warn("persist_prepared_startup_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
			return
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
	claimStarted := daemon.localNow()
	claim, err := daemon.claimWithRetryUntil(ctx, assignment.RunID, protocol.ClaimRequest{RuntimeID: daemon.runtimeID, RuntimeEpoch: daemon.runtimeEpoch, Generation: assignment.Generation, ClaimID: journal.ClaimID}, assignment.AssignmentExpiresAt, waitForRetry)
	if err != nil {
		if control.IsOwnershipLost(err) {
			daemon.stopRecoveredJournal(journal, "claim ownership lost")
		}
		daemon.log.Warn("claim_failed", "run_id", assignment.RunID, "error", err)
		return
	}
	claim.RequestStartedAt = claimStarted
	localLeaseDeadline := leaseDeadlineAt(claim, claimStarted)
	if linuxNativeRuntimeRequiresGoalAdmission(daemon.config.Runtime.HarnessKind) {
		localLeaseDeadline = daemon.claimLocalLeaseDeadline(claim, claimStarted)
	}
	daemon.setLocalLeaseDeadline(key, localLeaseDeadline)
	journal, err = daemon.store.SaveClaimGrant(key, claim)
	if err != nil {
		daemon.log.Error("save_claim_grant_failed", "run_id", assignment.RunID, "error", err)
		return
	}
	admission, admissionPresent, admissionErr := parseAdmissionInput(claim.Work.Input)
	var legacyLaunchErr error
	if !admissionPresent && daemon.config.Runtime.HarnessKind == config.RuntimeHarnessOpenCode {
		legacyLaunchErr = daemon.harnessCapabilities.Require(harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel)
	}
	goalAdmissionRequired := linuxNativeRuntimeRequiresGoalAdmission(daemon.config.Runtime.HarnessKind) || daemon.config.Runtime.HarnessKind == config.RuntimeHarnessOMP || daemon.config.Runtime.HarnessKind == config.RuntimeHarnessOmO
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
	if !operatorCancelled && !stale && !contextCancelled && !admissionPresent && !goalAdmissionRequired && legacyLaunchErr == nil {
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
			if daemon.isStale(key) {
				return
			}
			daemon.queueFailure(ctx, key, "goal_admission", err)
			return
		}
		handoff = true
		if daemon.finishAttachedStart(key) {
			daemon.signalOutbox()
		}
		daemon.waitForRunWithContext(ctx, key)
		return
	}
	if daemon.config.Runtime.HarnessKind == config.RuntimeHarnessOMP || daemon.config.Runtime.HarnessKind == config.RuntimeHarnessOmO {
		daemon.queueFailure(ctx, key, "goal_admission_required", taskResultFailure(protocol.TaskResultReasonUnsupportedVersion, errors.New("OMP/OmO runtime requires a symmetry.admission.v1 Goal admission")))
		return
	}
	if linuxNativeRuntimeRequiresGoalAdmission(daemon.config.Runtime.HarnessKind) {
		daemon.queueFailure(ctx, key, "linux_v2_required", errors.New("Linux native Pi/OpenCode runtime requires an exact symmetry.admission.v1 RunKey/fence"))
		return
	}
	if legacyLaunchErr != nil {
		daemon.queueFailure(ctx, key, "harness_capability", legacyLaunchErr)
		return
	}
	if err != nil {
		daemon.log.Error("queue_running_failed", "run_id", assignment.RunID, "error", err)
		daemon.queueFailure(ctx, key, "queue_running", err)
		return
	}
	if leaseErr := daemon.validateClaimLeaseForStart(claim, claimStarted, "process"); leaseErr != nil {
		daemon.queueFailure(ctx, key, "initial_lease_deadline", leaseErr)
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
	initialLeaseDeadlineAt := leaseDeadlineAt(claim, claimStarted)
	initialLeaseDeadline := remainingLeaseDeadlineAt(initialLeaseDeadlineAt, daemon.localNow())
	if leaseErr := daemon.validateClaimLeaseForStart(claim, claimStarted, "process"); leaseErr != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(ctx, key, "initial_lease_deadline", leaseErr)
		}
		return
	}
	process, err := daemon.start(executionContext, execution.Invocation{
		Program:                            profile.Command,
		Args:                               profile.Args,
		Dir:                                prepared.Path,
		Env:                                environment,
		InitialInput:                       input,
		CloseInputAfterInitial:             !profile.Interactive,
		InitialLeaseDeadline:               initialLeaseDeadline,
		InitialLeaseDeadlineAt:             initialLeaseDeadlineAt,
		InitialLeaseSequence:               1,
		PrepareSupervisorHandoff:           daemon.prepareSupervisorHandoffCallback(key),
		BindSupervisorHandoff:              daemon.bindSupervisorHandoffCallback(key),
		CommitSupervisorHandoff:            daemon.commitSupervisorHandoffCallback(key),
		RecordSupervisorHandoffStopReceipt: daemon.recordSupervisorHandoffStopReceiptCallback(key),
		ClearSupervisorHandoff:             daemon.clearSupervisorHandoffCallback(key),
		PersistContainmentUnproven:         daemon.markContainmentUnprovenCallback(key),
		PersistProcessWithAuthority:        daemon.persistProcessAuthorityCallback(key),
		PersistProcess: func(pid int, identity string) error {
			return daemon.persistProcessMarker(key, pid, identity, daemon.now())
		},
		PersistProcessAuthority: func(pid int, identity string, value *authority.Supervisor) error {
			return daemon.persistProcessAuthorityCallback(key)(pid, identity, value)
		},
		PersistContainmentStopReceipt: daemon.persistContainmentStopReceiptCallback(key),
	}, sink)
	if isNilProcess(process) {
		process = nil
		if err == nil {
			err = errStartProcessNil
		}
	}
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
			if _, _, detailsErr := processDetails(process); detailsErr != nil {
				err = errors.Join(err, fmt.Errorf("read process identity after start failure: %w", detailsErr))
			}
			// Runner owns launch persistence. In particular, do not create a
			// marker-only record after an atomic process/authority write fails.
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
			daemon.failedStartWatchWG.Add(1)
			finalizationContext := daemon.failedStartFinalizationContext()
			go func() {
				defer daemon.failedStartWatchWG.Done()
				daemon.watchFailedStartProcess(ctx, finalizationContext, key, process)
			}()
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
	if initialLeaseDeadline > 0 {
		active.leaseSequence = 1
	}
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
	alreadyRecorded, readErr := daemon.processDetailsAlreadyRecorded(key, pid, identity)
	if readErr != nil {
		err = readErr
	} else if !alreadyRecorded {
		if daemon.options.recordProcess != nil {
			_, err = daemon.options.recordProcess(key, pid, identity, daemon.options.clock())
		} else {
			_, err = daemon.store.SetProcessDetails(key, pid, identity, daemon.options.clock())
		}
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

// persistStaleAndEnqueueCleanup owns the lease-loss durability boundary. A
// failed journal write is resolved only by reading back the exact stale state;
// until that proof exists, callers must retain their in-memory recovery owner.
func (daemon *daemon) persistStaleAndEnqueueCleanup(key state.RunKey) (state.RunJournal, error) {
	journal, err := daemon.store.SetLocalState(key, "stale")
	if err == nil {
		daemon.enqueueCleanup(key)
		return journal, nil
	}
	readback, readErr := daemon.store.LoadJournal(key)
	if readErr == nil && readback.LocalState == "stale" {
		daemon.enqueueCleanup(key)
		return readback, nil
	}
	if readErr != nil {
		return state.RunJournal{}, errors.Join(err, fmt.Errorf("read back stale state: %w", readErr))
	}
	return state.RunJournal{}, err
}

func (daemon *daemon) releaseRun(key state.RunKey) {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	active := daemon.running[key]
	delete(daemon.running, key)
	daemon.commands.dropRun(key)
	releaseSlot := active != nil && active.slotHeld
	renewCancel := context.CancelFunc(nil)
	terminalCancel := context.CancelFunc(nil)
	if active != nil {
		active.slotHeld = false
		active.leaseRenewalClosed = true
		if active.nativeTerminalOwner {
			active.nativeTerminalOwner = false
			active.nativeTerminalOwnerClaim = 0
			active.nativeStartTerminalOwner = false
			if active.terminalizing > 0 {
				active.terminalizing--
			}
		}
		active.nativeUsageRetryOwnerClaim = 0
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
	daemon.commands.dropRun(key)
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
			active.leaseRenewalClosed = true
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

func (daemon *daemon) waitForRun(key state.RunKey) {
	daemon.waitForRunWithContext(context.Background(), key)
}

func (daemon *daemon) waitForRunWithContext(ctx context.Context, key state.RunKey) {
	daemon.waitForRunResultWithContext(ctx, key, nil)
}

func (daemon *daemon) waitForRunResultWithContext(ctx context.Context, key state.RunKey, observedResult *execution.Result) {
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
	processPID, processIdentity, _ := processDetails(process)
	var result execution.Result
	if observedResult == nil {
		result = process.Wait()
	} else {
		result = *observedResult
	}
	if !daemon.ownsRunningProcess(key, active, process) {
		return
	}
	processStopped := result.ContainmentError == nil && result.TerminationError == nil
	active.inputMu.Lock()
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
	daemon.mu.Lock()
	if daemon.running[key] != active || active.process != process {
		daemon.mu.Unlock()
		active.inputMu.Unlock()
		return
	}
	cancelled := active.cancelled
	stale := active.stale
	terminal := active.terminal
	startFailure := active.startFailure
	daemon.mu.Unlock()
	// Record the exact owner and its completed Wait result before any terminal
	// delivery. A receipt/finalization fault must not make cleanup depend on a
	// potentially slow or cancelled Control transition.
	if !daemon.recordGenericProcessExit(key, active, process, processPID, processIdentity, processStopped) {
		active.inputMu.Unlock()
		return
	}
	if stale {
		active.inputMu.Unlock()
		if daemon.settleStaleCancellation(ctx, key) {
			daemon.releaseSlotOnce(key)
			daemon.enqueueCleanup(key)
			return
		}
		_, err := daemon.store.SetLocalState(key, "stale")
		daemon.releaseSlotOnce(key)
		daemon.enqueueCleanup(key)
		if err != nil && daemon.log != nil {
			daemon.log.Warn("persist_stale_process_exit_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		return
	}
	if !cancelled && !terminal {
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
	daemon.enqueueCleanup(key)
	daemon.releaseCleanupAfterProcessExit(key)
}

func (daemon *daemon) ownsRunningProcess(key state.RunKey, expected *runningRun, process Process) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.running[key] == expected && expected.process == process
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (daemon *daemon) releaseTerminalReservation(key state.RunKey) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if active := daemon.running[key]; active != nil && active.terminalizing > 0 {
		active.terminalizing--
	}
}

// reloadCancellationBeforeSideEffect is the last local authority check before
// a cancel command touches a native session, process, or execution context.
// Store I/O is deliberately outside daemon.mu; the in-memory reservation is
// checked again after the read so a terminal owner can win without being
// overwritten by a late cancellation flag.
func (daemon *daemon) reloadCancellationBeforeSideEffect(key state.RunKey, ownTerminalReservation bool) (state.RunJournal, bool) {
	journ, err := daemon.store.LoadJournal(key)
	if err != nil {
		return state.RunJournal{}, false
	}
	daemon.mu.Lock()
	active := daemon.running[key]
	terminalizing := 0
	terminal := false
	if active != nil {
		terminalizing = active.terminalizing
		terminal = active.terminal
	}
	daemon.mu.Unlock()
	allowedTerminalizing := 0
	if ownTerminalReservation {
		allowedTerminalizing = 1
	}
	if terminal || terminalizing > allowedTerminalizing || durableTerminalPresent(journ) {
		return journ, false
	}
	return journ, true
}

func (daemon *daemon) clearCancellationReservation(key state.RunKey, commandID string) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil || active.cancelCommandID != commandID || active.terminal {
		return
	}
	active.cancelled = false
	active.cancelCommandID = ""
}

func (daemon *daemon) rejectCancellationIfTerminalDurable(ctx context.Context, key state.RunKey, commandID string, journal state.RunJournal) bool {
	if !durableTerminalPresent(journal) || state.CommandAcknowledgementRetired(journal) {
		return false
	}
	return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, commandID, journal)
}

func (daemon *daemon) replayOrRejectCancellationAcknowledgement(ctx context.Context, key state.RunKey, commandID string, journal state.RunJournal) bool {
	if commandID == "" || state.CommandAcknowledgementRetired(journal) {
		return false
	}
	if hasPendingCommandAcknowledgement(journal, commandID) {
		daemon.signalOutboxFor(key)
		return true
	}
	return daemon.queueCommandAcknowledgementWithContext(ctx, key, commandID, "rejected")
}

func (daemon *daemon) reloadRecoveredStopBeforeSideEffect(expected state.RunJournal) (state.RunJournal, bool) {
	key := expected.Key()
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return state.RunJournal{}, false
	}
	if journal.Key() != key || journal.Fence() != expected.Fence() {
		return journal, false
	}
	daemon.mu.Lock()
	active := daemon.running[key]
	blocked := active != nil && (active.terminal || active.terminalizing > 0)
	daemon.mu.Unlock()
	if blocked || durableTerminalPresent(journal) {
		return journal, false
	}
	return journal, true
}

func (daemon *daemon) queueCancellationReceipt(ctx context.Context, key state.RunKey, commandID string) bool {
	if err := daemon.queueCancelledTerminalAndAcknowledgementWithContext(ctx, key, commandID); err != nil {
		if errors.Is(err, errAuthoritativeTerminal) {
			journal, loadErr := daemon.store.LoadJournal(key)
			if loadErr == nil && state.CommandAcknowledgementRetired(journal) {
				return false
			}
			if loadErr == nil {
				return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, commandID, journal)
			}
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, commandID, "rejected")
		}
		if daemon.log != nil {
			daemon.log.Error("queue_cancelled_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		return false
	}
	return true
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
		active.cleanupBlocked = nativeCleanupBlocked(active)
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
	if active.process == nil && active.nativeSession == nil {
		active.cleanupBlocked = nativeCleanupBlocked(active)
	}
	// Cancellation accepted during native publication is replayed only after
	// the start owner has settled the returned session.
	return active.terminal || active.cancelled
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
