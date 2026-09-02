// Package app owns the daemon control loop.
package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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
	minimumInterval     = time.Second
	maximumInterval     = time.Minute
	leaseSafetyMargin   = 5 * time.Second
	retryMaximum        = 30 * time.Second
	maxJSONLRecordBytes = 256 * 1024
	rawOutputChunkBytes = 32 * 1024
)

// ControlAPI is the authenticated protocol boundary used by the loop.
type ControlAPI interface {
	RegisterSession(context.Context, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error)
	Heartbeat(context.Context, string, protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error)
	Work(context.Context, string, int64) (protocol.RuntimeSnapshot, error)
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
	Enroll(context.Context, string, protocol.EnrollRequest) (protocol.EnrollResponse, error)
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

type options struct {
	httpClient       *http.Client
	store            *state.Store
	control          ControlAPI
	enrollment       EnrollmentAPI
	workspace        workspace.Service
	start            StartProcess
	notifications    NotificationClient
	logWriter        io.Writer
	clock            func() time.Time
	newID            func() (string, error)
	terminatePersist func(pid int, identity string) error
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
		newID:            state.NewDaemonInstanceID,
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

	store          *state.Store
	control        ControlAPI
	workspace      workspace.Service
	start          StartProcess
	runtimeID      string
	runtimeEpoch   int64
	leaseDuration  time.Duration
	pollEvery      time.Duration
	heartbeatEvery time.Duration
	running        map[state.RunKey]*runningRun
	slots          chan struct{}
	mu             sync.Mutex
	workers        sync.WaitGroup
}

type runningRun struct {
	process         Process
	prepared        workspace.Prepared
	parser          *jsonlParser
	starting        bool
	claimed         bool
	cancel          context.CancelFunc
	cancelled       bool
	cancelCommandID string
	stale           bool
	succeeded       bool
}

func (daemon *daemon) run(ctx context.Context) error {
	if err := daemon.initialize(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	defer daemon.close()

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
	leases := time.NewTicker(minimumInterval)
	defer poll.Stop()
	defer heartbeat.Stop()
	defer leases.Stop()

	for {
		select {
		case <-ctx.Done():
			daemon.stopAll()
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
		case <-leases.C:
			daemon.renewLeases(ctx)
			daemon.flushAll(ctx)
		}
	}
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
			var requestErr error
			response, requestErr = enrollment.Enroll(ctx, token, protocol.EnrollRequest{Machine: protocol.MachineEnrollment{Name: daemon.config.MachineName}})
			return requestErr
		})
		if enrollErr != nil {
			return fmt.Errorf("enroll machine: %w", enrollErr)
		}
		identity = state.MachineIdentity{MachineID: response.MachineID, MachineToken: response.MachineToken}
		if saveErr := daemon.store.SaveIdentity(identity); saveErr != nil {
			return fmt.Errorf("save machine identity: %w", saveErr)
		}
		daemon.log.Info("machine_enrolled", "machine_id", identity.MachineID)
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
	daemon.slots = make(chan struct{}, daemon.config.Runtime.Capacity)
	if err := retry(ctx, func() error { return daemon.flushTerminalJournals(ctx) }); err != nil {
		return fmt.Errorf("flush terminal journals before registration: %w", err)
	}
	instanceID, idErr := daemon.options.newID()
	if idErr != nil {
		return fmt.Errorf("generate daemon instance ID: %w", idErr)
	}
	registrationRequest := protocol.SessionRegistrationRequest{
		DaemonInstanceID: instanceID,
		Runtimes:         []protocol.RuntimeRegistration{{RuntimeKey: daemon.config.Runtime.RuntimeKey, Name: daemon.config.Runtime.Name, Capacity: daemon.config.Runtime.Capacity, AgentProfile: daemon.config.Runtime.AgentProfile, Workspace: daemon.config.Runtime.Workspace, Capabilities: json.RawMessage(`{}`)}},
	}
	var registration protocol.SessionRegistrationResponse
	registerErr := retry(ctx, func() error {
		var requestErr error
		registration, requestErr = daemon.control.RegisterSession(ctx, registrationRequest)
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
	daemon.reconcile(ctx)
	return nil
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

func (daemon *daemon) heartbeat(ctx context.Context) {
	snapshot, err := daemon.control.Heartbeat(ctx, daemon.runtimeID, protocol.RuntimeHeartbeatRequest{RuntimeEpoch: daemon.runtimeEpoch, ActiveRuns: daemon.activeRuns()})
	if err != nil {
		daemon.log.Warn("runtime_heartbeat_failed", "error", err)
		return
	}
	daemon.handleSnapshot(ctx, snapshot)
	daemon.flushAll(ctx)
}

func (daemon *daemon) sync(ctx context.Context) {
	snapshot, err := daemon.control.Work(ctx, daemon.runtimeID, daemon.runtimeEpoch)
	if err != nil {
		daemon.log.Warn("runtime_poll_failed", "error", err)
		daemon.flushAll(ctx)
		return
	}
	daemon.handleSnapshot(ctx, snapshot)
	daemon.flushAll(ctx)
}

func (daemon *daemon) reconcile(ctx context.Context) {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		daemon.log.Error("list_journals_failed", "error", err)
		return
	}
	runs := make([]protocol.ReconcileRun, 0, len(journals))
	for _, journal := range journals {
		if isReconcileState(journal.LocalState) && hasFullFence(journal) {
			runs = append(runs, protocol.ReconcileRun{RunID: journal.RunID, Generation: journal.Generation, ClaimedRuntimeEpoch: journal.ClaimedRuntimeEpoch, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken, LocalState: journal.LocalState, LastEventSequence: journal.LastEventSequence})
			continue
		}
		if !daemon.hasRun(journal.Key()) {
			daemon.stopRecoveredJournal(journal, "not eligible for reconcile")
		}
	}
	response, err := daemon.control.Reconcile(ctx, daemon.runtimeID, protocol.ReconcileRequest{RuntimeEpoch: daemon.runtimeEpoch, Runs: runs})
	if err != nil {
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
			_, _ = daemon.store.UpdateLeaseExpiry(key, *decision.LeaseExpiresAt)
		}
	}
	daemon.handleSnapshot(ctx, protocol.RuntimeSnapshot{Assignments: response.Assignments, Commands: response.Commands})
	daemon.flushAll(ctx)
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
	if journal.PID > 0 && daemon.options.terminatePersist != nil {
		if err := daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity); err != nil {
			daemon.log.Warn("stop_recovered_process_failed", "run_id", journal.RunID, "error", err)
			return
		}
	}
	if err := daemon.cleanupRecoveredWorkspace(journal, false); err != nil {
		daemon.log.Warn("cleanup_recovered_workspace_failed", "run_id", journal.RunID, "error", err)
		return
	}
	if err := daemon.store.DeleteJournal(journal.Key()); err != nil && !state.IsNotFound(err) {
		daemon.log.Warn("delete_recovered_journal_failed", "run_id", journal.RunID, "error", err)
		return
	}
	daemon.log.Warn("recovered_journal_stopped", "run_id", journal.RunID, "generation", journal.Generation, "reason", reason)
}

func (daemon *daemon) cleanupRecoveredWorkspace(journal state.RunJournal, succeeded bool) error {
	if journal.WorkspacePath == "" {
		return nil
	}
	if journal.WorkspaceBindingKey == "" {
		return errors.New("recovered workspace binding key is missing")
	}
	prepared, err := daemon.workspace.Recover(context.Background(), journal.WorkspaceBindingKey, workspace.RunRef{RunID: journal.RunID, Generation: journal.Generation}, journal.WorkspacePath)
	if err != nil {
		return err
	}
	return daemon.workspace.Cleanup(context.Background(), prepared, succeeded)
}

func (daemon *daemon) flushTerminalJournals(ctx context.Context) error {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return err
	}
	for _, journal := range journals {
		if journal.LocalState != "terminal_pending" {
			continue
		}
		daemon.flushRun(ctx, journal)
		if _, err := daemon.store.LoadJournal(journal.Key()); err == nil {
			return fmt.Errorf("terminal journal %s/%d remains pending", journal.RunID, journal.Generation)
		} else if !state.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (daemon *daemon) handleSnapshot(ctx context.Context, snapshot protocol.RuntimeSnapshot) {
	for _, command := range snapshot.Commands {
		daemon.handleCommand(ctx, command)
	}
	for _, assignment := range snapshot.Assignments {
		daemon.startAssignment(ctx, assignment)
	}
}

func (daemon *daemon) startAssignment(ctx context.Context, assignment protocol.Assignment) {
	key := state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}
	if assignment.RunID == "" || assignment.Generation <= 0 {
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
	daemon.running[key] = &runningRun{starting: true, cancel: cancel}
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
	handoff := false
	defer func() {
		if handoff {
			return
		}
		cleanupSucceeded := true
		if preparedOK {
			cleanupSucceeded = daemon.cleanupWorkspace(prepared, false) == nil
		}
		stale := daemon.isStale(key)
		daemon.releaseRun(key)
		if stale && cleanupSucceeded {
			_ = daemon.store.DeleteJournal(key)
		}
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
	claim, err := daemon.claimWithRetry(ctx, assignment.RunID, protocol.ClaimRequest{RuntimeID: daemon.runtimeID, RuntimeEpoch: daemon.runtimeEpoch, Generation: assignment.Generation, ClaimID: journal.ClaimID})
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
	if operatorCancelled {
		err = daemon.queueTerminalTransition(key, "cancelled", map[string]any{})
	} else if !stale && !contextCancelled {
		err = daemon.markRunning(key)
	}
	daemon.mu.Unlock()
	if operatorCancelled {
		if err != nil {
			daemon.log.Error("queue_cancelled_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			daemon.queueCommandAcknowledgement(key, cancelCommandID, "failed")
		} else {
			daemon.queueCommandAcknowledgement(key, cancelCommandID, "applied")
		}
		return
	}
	if stale || contextCancelled {
		return
	}
	if err != nil {
		daemon.log.Error("queue_running_failed", "run_id", assignment.RunID, "error", err)
		daemon.queueFailure(key, "queue_running", err)
		return
	}
	prepared, err = daemon.workspace.Prepare(ctx, daemon.config.Runtime.Workspace, workspace.RunRef{RunID: key.RunID, Generation: key.Generation})
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "prepare_workspace", err)
		}
		return
	}
	preparedOK = true
	if daemon.isCancelled(key) {
		return
	}
	journal, err = daemon.store.LoadJournal(key)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "load_running_journal", err)
		}
		return
	}
	journal.WorkspacePath = prepared.Path
	if err := daemon.store.SaveJournal(journal); err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "record_workspace", err)
		}
		return
	}
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	environment, err := execution.BuildEnvironment(profile.EnvAllowlist...)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "build_environment", err)
		}
		return
	}
	input, err := initialInput(profile, claim.Work)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "encode_input", err)
		}
		return
	}
	parser := &jsonlParser{}
	sink := execution.SinkFunc(func(_ context.Context, event execution.Event) error {
		return daemon.queueOutput(key, profile.EventFormat, parser, event)
	})
	process, err := daemon.start(ctx, execution.Invocation{Program: profile.Command, Args: profile.Args, Dir: prepared.Path, Env: environment, InitialInput: input, CloseInputAfterInitial: !profile.Interactive}, sink)
	if err != nil {
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "start_agent", err)
		}
		return
	}
	pid, identity, detailsErr := processDetails(process)
	if detailsErr != nil {
		_ = process.Terminate(context.Background(), 0)
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "read_process_identity", detailsErr)
		}
		return
	}
	if _, err := daemon.store.SetProcessDetails(key, pid, identity, daemon.options.clock()); err != nil {
		_ = process.Terminate(context.Background(), 0)
		if !daemon.isCancelled(key) {
			daemon.queueFailure(key, "record_process", err)
		}
		return
	}
	daemon.mu.Lock()
	active = daemon.running[key]
	if active == nil {
		daemon.mu.Unlock()
		_ = process.Terminate(context.Background(), 0)
		return
	}
	active.process = process
	active.prepared = prepared
	active.parser = parser
	active.starting = false
	daemon.mu.Unlock()
	handoff = true
	daemon.waitForRun(key)
}

func (daemon *daemon) markRunning(key state.RunKey) error {
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		return err
	}
	_, err := daemon.store.SetLocalState(key, "running")
	return err
}

func (daemon *daemon) releaseRun(key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	delete(daemon.running, key)
	daemon.mu.Unlock()
	if active != nil && active.cancel != nil {
		active.cancel()
	}
	<-daemon.slots
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

func (daemon *daemon) cleanupWorkspace(prepared workspace.Prepared, succeeded bool) error {
	if err := daemon.workspace.Cleanup(context.Background(), prepared, succeeded); err != nil {
		daemon.log.Warn("cleanup_workspace_failed", "path", prepared.Path, "error", err)
		return err
	}
	return nil
}

func processDetails(process Process) (int, string, error) {
	pid, identity := process.ProcessDetails()
	if pid > 0 && identity != "" {
		return pid, identity, nil
	}
	return 0, "", errors.New("process does not expose a persistent identity")
}

func initialInput(profile config.AgentProfile, work protocol.Work) ([]byte, error) {
	if profile.InputMode == config.InputModeGoal {
		return append([]byte(work.Goal), '\n'), nil
	}
	encoded, err := json.Marshal(struct {
		Goal  string          `json:"goal"`
		Input json.RawMessage `json:"input"`
	}{Goal: work.Goal, Input: work.Input})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
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
			return daemon.queueRawEvent(key, execution.Event{Stream: event.Stream, At: event.At, Data: record.data})
		}
		payload, ok := decoded.(map[string]any)
		if !ok {
			return daemon.queueRawEvent(key, execution.Event{Stream: event.Stream, At: event.At, Data: record.data})
		}
		kind := "agent_event"
		var declaredType string
		if raw, ok := payload["type"].(string); ok {
			declaredType = raw
		}
		switch declaredType {
		case "progress", "waiting_for_input":
			kind = declaredType
		}
		if err := daemon.queueEvent(key, kind, json.RawMessage(record.data), event.At); err != nil {
			return err
		}
		if kind == "waiting_for_input" {
			_ = daemon.queueTransition(key, "waiting_for_input", json.RawMessage(record.data))
			_, _ = daemon.store.SetLocalState(key, "waiting_for_input")
		}
	}
	return nil
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
	return err
}

func (daemon *daemon) queueFailure(key state.RunKey, stage string, cause error) {
	if err := daemon.queueTerminalTransition(key, "failed", map[string]string{"stage": stage, "error": cause.Error()}); err != nil {
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
	_, err = daemon.store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded})
	return err
}

func (daemon *daemon) waitForRun(key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	process := Process(nil)
	if active != nil {
		process = active.process
	}
	daemon.mu.Unlock()
	if active == nil || process == nil {
		return
	}
	result := process.Wait()
	daemon.mu.Lock()
	active.succeeded = result.Success()
	cancelled := active.cancelled
	stale := active.stale
	daemon.mu.Unlock()
	if stale {
		cleanupSucceeded := daemon.cleanupWorkspace(active.prepared, false) == nil
		daemon.releaseRun(key)
		if cleanupSucceeded {
			_ = daemon.store.DeleteJournal(key)
		}
		return
	}
	if !cancelled {
		if result.Success() {
			if err := daemon.queueTerminalTransition(key, "completed", map[string]any{"exit_code": result.ExitCode}); err != nil {
				daemon.log.Error("queue_completed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
		} else {
			if err := daemon.queueTerminalTransition(key, "failed", map[string]any{"exit_code": result.ExitCode, "error": errorText(result.WaitError)}); err != nil {
				daemon.log.Error("queue_failed_transition_failed", "run_id", key.RunID, "generation", key.Generation, "stage", "process_exit", "error", err)
			}
		}
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (daemon *daemon) handleCommand(ctx context.Context, command protocol.Command) {
	key := state.RunKey{RunID: command.RunID, Generation: command.Generation}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == command.CommandID {
			return
		}
	}
	if command.Kind == "cancel" {
		daemon.mu.Lock()
		active := daemon.running[key]
		if active == nil {
			daemon.mu.Unlock()
			daemon.queueCommandAcknowledgement(key, command.CommandID, "rejected")
			return
		}
		if !active.claimed {
			active.cancelled = true
			active.cancelCommandID = command.CommandID
			daemon.mu.Unlock()
			return
		}
		active.cancelled = true
		process := active.process
		cancel := active.cancel
		daemon.mu.Unlock()
		var terminateErr error
		if process != nil {
			terminateErr = process.Terminate(ctx, 2*time.Second)
		} else if cancel != nil {
			cancel()
		}
		outcome := "applied"
		if terminateErr != nil {
			outcome = "failed"
		} else if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
			daemon.log.Error("queue_cancelled_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			outcome = "failed"
		}
		daemon.queueCommandAcknowledgement(key, command.CommandID, outcome)
		return
	}

	daemon.mu.Lock()
	active := daemon.running[key]
	starting := active != nil && active.starting
	process := Process(nil)
	if active != nil {
		process = active.process
	}
	daemon.mu.Unlock()
	if active == nil || starting || process == nil {
		daemon.queueCommandAcknowledgement(key, command.CommandID, "rejected")
		return
	}
	outcome := "rejected"
	switch command.Kind {
	case "provide_input":
		if !json.Valid(command.Payload) {
			break
		}
		input := append(append([]byte(nil), command.Payload...), '\n')
		if err := process.WriteInput(input); err != nil {
			outcome = "failed"
		} else {
			_ = daemon.queueTransition(key, "running", map[string]any{})
			_, _ = daemon.store.SetLocalState(key, "running")
			outcome = "applied"
		}
	}
	daemon.queueCommandAcknowledgement(key, command.CommandID, outcome)
}

func (daemon *daemon) queueCommandAcknowledgement(key state.RunKey, commandID, outcome string) {
	id, err := daemon.options.newID()
	if err != nil {
		return
	}
	_, _ = daemon.store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: commandID, Outcome: outcome, AckID: id})
}

func (daemon *daemon) renewLeases(ctx context.Context) {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	now := daemon.options.clock()
	type renewal struct {
		index    int
		journal  state.RunJournal
		response protocol.LeaseHeartbeatResponse
		err      error
	}
	candidates := make([]state.RunJournal, 0, len(journals))
	for _, journal := range journals {
		if journal.LeaseToken == "" || journal.LocalState == "stale" {
			continue
		}
		remaining := journal.LeaseExpiresAt.Sub(now)
		margin := leaseSafetyMargin
		if daemon.leaseDuration > 0 && daemon.leaseDuration/3 < margin {
			margin = daemon.leaseDuration / 3
		}
		if remaining <= 0 {
			daemon.terminateForLease(journal, "lease expired")
			continue
		}
		if remaining > margin*2 {
			continue
		}
		candidates = append(candidates, journal)
	}
	if len(candidates) == 0 {
		return
	}
	results := make([]renewal, len(candidates))
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
				response, renewErr := daemon.control.RenewLease(ctx, journal.RunID, protocol.LeaseHeartbeatRequest{Fence: journal.Fence()})
				completed <- renewal{index: index, journal: journal, response: response, err: renewErr}
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
		results[result.index] = result
	}
	for _, result := range results {
		if result.err != nil {
			if control.IsOwnershipLost(result.err) || result.journal.LeaseExpiresAt.Sub(now) <= leaseSafetyMargin {
				daemon.terminateForLease(result.journal, "lease renewal failed")
			}
			continue
		}
		_, _ = daemon.store.UpdateLeaseExpiry(result.journal.Key(), result.response.LeaseExpiresAt)
		for _, command := range result.response.Commands {
			daemon.handleCommand(ctx, command)
		}
	}
}

func (daemon *daemon) terminateForLease(journal state.RunJournal, reason string) {
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
	for _, journal := range journals {
		daemon.flushRun(ctx, journal)
	}
}

func (daemon *daemon) flushRun(ctx context.Context, journal state.RunJournal) {
	key := journal.Key()
	if journal.LocalState == "stale" {
		return
	}
	terminalSucceeded := hasTransition(journal.PendingTransitions, "completed")
	if len(journal.PendingEvents) > 0 {
		if err := daemon.control.AppendEvents(ctx, journal.RunID, protocol.AppendEventsRequest{Fence: journal.Fence(), Events: journal.PendingEvents}); err != nil {
			if control.IsOwnershipLost(err) {
				daemon.terminateForLease(journal, "event ownership lost")
			}
			return
		}
		ids := make([]string, len(journal.PendingEvents))
		for index, event := range journal.PendingEvents {
			ids[index] = event.EventID
		}
		journal, _ = daemon.store.MarkEventsDelivered(key, ids)
	}
	cancelledPending := hasTransition(journal.PendingTransitions, "cancelled")
	sentAcknowledgements := make([]string, 0, len(journal.PendingCommandAcknowledgements))
	if cancelledPending {
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if err := daemon.control.AcknowledgeCommand(ctx, acknowledgement.CommandID, acknowledgement); err != nil {
				if control.IsOwnershipLost(err) {
					daemon.terminateForLease(journal, "ack ownership lost")
				}
				return
			}
			sentAcknowledgements = append(sentAcknowledgements, acknowledgement.AckID)
		}
	}
	for len(journal.PendingTransitions) > 0 {
		transition := journal.PendingTransitions[0]
		if err := daemon.control.Transition(ctx, journal.RunID, transition); err != nil {
			if control.IsOwnershipLost(err) {
				daemon.terminateForLease(journal, "transition ownership lost")
			}
			return
		}
		journal, _ = daemon.store.MarkTransitionsDelivered(key, []string{transition.TransitionID})
	}
	if cancelledPending {
		if len(sentAcknowledgements) > 0 {
			journal, _ = daemon.store.MarkCommandAcknowledgementsDelivered(key, sentAcknowledgements)
		}
	} else {
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if err := daemon.control.AcknowledgeCommand(ctx, acknowledgement.CommandID, acknowledgement); err != nil {
				if control.IsOwnershipLost(err) {
					daemon.terminateForLease(journal, "ack ownership lost")
				}
				return
			}
			journal, _ = daemon.store.MarkCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})
		}
	}
	if journal.LocalState == "terminal_pending" && len(journal.PendingTransitions) == 0 {
		daemon.mu.Lock()
		active := daemon.running[key]
		prepared := workspace.Prepared{}
		succeeded := false
		if active != nil {
			prepared = active.prepared
			succeeded = active.succeeded
		}
		daemon.mu.Unlock()
		if active != nil {
			if err := daemon.workspace.Cleanup(context.Background(), prepared, succeeded); err != nil {
				daemon.log.Warn("cleanup_terminal_workspace_failed", "run_id", journal.RunID, "error", err)
				return
			}
			daemon.mu.Lock()
			delete(daemon.running, key)
			daemon.mu.Unlock()
			<-daemon.slots
		} else if err := daemon.cleanupRecoveredWorkspace(journal, terminalSucceeded); err != nil {
			daemon.log.Warn("cleanup_terminal_workspace_failed", "run_id", journal.RunID, "error", err)
			return
		}
		if err := daemon.store.DeleteJournal(key); err != nil {
			daemon.log.Warn("delete_terminal_journal_failed", "run_id", journal.RunID, "error", err)
		}
	}
}

func hasTransition(transitions []protocol.StateTransitionRequest, stateName string) bool {
	for _, transition := range transitions {
		if transition.State == stateName {
			return true
		}
	}
	return false
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
	case "terminal_pending":
		if hasTransition(journal.PendingTransitions, "cancelled") {
			return "cancelling", true
		}
		return "running", true
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
	delay := minimumInterval
	for {
		response, err := daemon.control.Claim(ctx, runID, request)
		if err == nil || control.IsOwnershipLost(err) || !retryableClaim(err) {
			return response, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.ClaimResponse{}, ctx.Err()
		case <-timer.C:
		}
		if delay < retryMaximum/2 {
			delay *= 2
		} else {
			delay = retryMaximum
		}
	}
}

func retryableClaim(err error) bool {
	var apiError *control.APIError
	if !errors.As(err, &apiError) {
		return true
	}
	switch apiError.Code {
	case control.RateLimited, control.ServiceUnavailable, control.UnexpectedHTTPStatus:
		return true
	default:
		return false
	}
}

func retry(ctx context.Context, operation func() error) error {
	delay := minimumInterval
	for {
		if err := operation(); err == nil {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < retryMaximum/2 {
			delay *= 2
		} else {
			delay = retryMaximum
		}
	}
}
