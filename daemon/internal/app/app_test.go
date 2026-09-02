package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestRunEnrollsOnceAndReusesPersistedIdentity(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	control := &fakeControl{}
	enrollment := &fakeEnrollment{}
	value := testConfig(t)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		control.cancel = cancel
		if err := Run(ctx, value, WithStore(store), WithControl(control), WithEnrollment(enrollment), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart)); err != nil {
			t.Fatalf("Run() attempt %d: %v", attempt, err)
		}
	}
	if enrollment.calls != 1 {
		t.Fatalf("Enroll calls = %d, want 1", enrollment.calls)
	}
	if control.registerCalls != 2 {
		t.Fatalf("RegisterSession calls = %d, want 2", control.registerCalls)
	}
	identity, err := store.LoadIdentity()
	if err != nil || identity.MachineID != "machine-1" {
		t.Fatalf("LoadIdentity() = %#v, %v", identity, err)
	}
}

func TestRunPersistsClaimIntentBeforeClaimAndHandlesNotification(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control := &fakeControl{
		assignment: protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "work"}},
		beforeClaim: func() {
			journals, listErr := store.ListJournals()
			if listErr != nil || len(journals) != 1 || journals[0].LocalState != "claiming" {
				t.Errorf("journal before Claim = %#v, err = %v", journals, listErr)
			}
		},
		cancel: cancel,
	}
	workEnabled := make(chan struct{})
	control.workEnabled = workEnabled
	notifier := fakeNotifier{before: func() { close(workEnabled) }, hints: []notification.Hint{{Type: "work_available", RuntimeID: "runtime-1"}}}
	started := make(chan execution.Invocation, 1)
	start := func(_ context.Context, invocation execution.Invocation, sink execution.Sink) (Process, error) {
		started <- invocation
		if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("agent output")}); err != nil {
			return nil, err
		}
		return fakeProcess{result: execution.Result{ExitCode: 0, FinishedAt: time.Now().UTC()}}, nil
	}
	err = Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(start), WithNotificationClient(notifier))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case invocation := <-started:
		if invocation.Program != "agent" || invocation.Dir == "" {
			t.Fatalf("Invocation = %#v", invocation)
		}
	default:
		t.Fatal("notification did not trigger assignment processing")
	}
	if control.claimCalls != 1 {
		t.Fatalf("Claim calls = %d, want 1", control.claimCalls)
	}
	if control.eventCalls == 0 {
		t.Fatal("expected output events to be flushed")
	}
}

func TestJSONLWaitingInputTransitionsAndAcknowledges(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: "run-1", Generation: 1, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	process := &recordingProcess{}
	daemon := &daemon{store: store, config: testConfig(t), options: options{newID: ids(), clock: time.Now}, control: &fakeControl{}, running: map[state.RunKey]*runningRun{key: {process: process}}, slots: make(chan struct{}, 1)}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"waiting_for_input\"}\n")}); err != nil {
		t.Fatal(err)
	}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "command-1", RunID: "run-1", Generation: 1, Kind: "provide_input", Payload: []byte(`{"answer":"yes"}`)})
	if got := string(process.input); got != "{\"answer\":\"yes\"}\n" {
		t.Fatalf("input = %q", got)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || len(journal.PendingTransitions) < 2 {
		t.Fatalf("journal = %#v", journal)
	}
}

func TestJSONLUnknownTypeIsStoredAsAgentEvent(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: "run-1", Generation: 1, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{newID: ids()}}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"custom\"}\n")}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 1 || journal.PendingEvents[0].Kind != "agent_event" {
		t.Fatalf("events = %#v", journal.PendingEvents)
	}
}

func TestClaimRetryReusesPersistedClaimID(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control := &fakeControl{claimErr: errors.New("temporary network failure")}
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   control,
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &fakeWorkspace{},
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			return fakeProcess{result: execution.Result{}}, nil
		},
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	assignment := protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}}
	daemon.startAssignment(context.Background(), assignment)
	daemon.workers.Wait()
	control.claimErr = nil
	daemon.startAssignment(context.Background(), assignment)
	daemon.workers.Wait()
	if len(control.claimIDs) != 2 || control.claimIDs[0] != control.claimIDs[1] {
		t.Fatalf("claim IDs = %#v, want a stable retry ID", control.claimIDs)
	}
}

func TestCommandAcknowledgementIsIdempotentAndUsesAllowedOutcomes(t *testing.T) {
	store, key := claimedStore(t)
	process := &recordingProcess{}
	daemon := &daemon{
		store:   store,
		options: options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {process: process}},
	}
	t.Cleanup(func() { _ = store.Close() })
	command := protocol.Command{CommandID: "command-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: []byte(`{"answer":"yes"}`)}
	daemon.handleCommand(context.Background(), command)
	daemon.handleCommand(context.Background(), command)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(process.input); got != "{\"answer\":\"yes\"}\n" {
		t.Fatalf("input = %q, want one write", got)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("acknowledgements = %#v", journal.PendingCommandAcknowledgements)
	}

	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "command-2", RunID: key.RunID, Generation: key.Generation, Kind: "unknown"})
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := journal.PendingCommandAcknowledgements[1].Outcome; got != "rejected" {
		t.Fatalf("unknown command outcome = %q, want rejected", got)
	}
}

func TestAssignmentQueuesRunningBeforeSynchronousAgentOutput(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := make(chan state.RunJournal, 1)
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &fakeWorkspace{},
		start: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (Process, error) {
			if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("output")}); err != nil {
				return nil, err
			}
			journal, loadErr := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
			if loadErr != nil {
				return nil, loadErr
			}
			observed <- journal
			return fakeProcess{result: execution.Result{}}, nil
		},
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}})
	daemon.workers.Wait()
	select {
	case journal := <-observed:
		if len(journal.PendingTransitions) == 0 || journal.PendingTransitions[0].State != "running" || len(journal.PendingEvents) != 1 {
			t.Fatalf("journal = %#v", journal)
		}
	default:
		t.Fatal("agent output was not observed")
	}
}

func TestStartAssignmentDoesNotBlockReactor(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entered := make(chan struct{})
	gate := make(chan struct{})
	control := &fakeControl{claimBlock: gate, claimEntered: entered}
	daemon := &daemon{
		config:       testConfig(t),
		store:        store,
		control:      control,
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    &fakeWorkspace{},
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	done := make(chan struct{})
	go func() {
		daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startAssignment blocked the reactor")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("claim worker did not start")
	}
	close(gate)
	daemon.workers.Wait()
}

func TestPreparedWorkspaceIsCleanedAfterStartFailure(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cleaned := make(chan bool, 1)
	daemon := &daemon{
		config:       testConfig(t),
		store:        store,
		control:      &fakeControl{},
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    &trackingWorkspace{cleaned: cleaned},
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	daemon.workers.Wait()
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("cleanup used success policy after failed start")
		}
	default:
		t.Fatal("prepared workspace was not cleaned")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 2 || journal.PendingTransitions[0].State != "running" || journal.PendingTransitions[1].State != "failed" {
		t.Fatalf("transitions = %#v", journal.PendingTransitions)
	}
}

func TestLeaseExpiryCancelsBlockedPrepareWithoutProcess(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := newBlockingWorkspace()
	daemon := startupTestDaemon(t, store, workspace)
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.terminateForLease(journal, "lease expired")
	select {
	case <-workspace.cancelled:
	case <-time.After(time.Second):
		t.Fatal("lease expiry did not cancel blocked Prepare")
	}
	daemon.workers.Wait()
	select {
	case succeeded := <-workspace.cleaned:
		if succeeded {
			t.Fatal("stale startup cleanup used success policy")
		}
	default:
		t.Fatal("completed Prepare was not cleaned after lease expiry")
	}
}

func TestCancelDuringBlockedPrepareAcknowledgesAndCleansUp(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := newBlockingWorkspace()
	daemon := startupTestDaemon(t, store, workspace)
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	select {
	case <-workspace.cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancel command did not cancel blocked Prepare")
	}
	daemon.workers.Wait()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 2 || journal.PendingTransitions[0].State != "running" || journal.PendingTransitions[1].State != "cancelled" {
		t.Fatalf("transitions = %#v", journal.PendingTransitions)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("acknowledgements = %#v", journal.PendingCommandAcknowledgements)
	}
	select {
	case succeeded := <-workspace.cleaned:
		if succeeded {
			t.Fatal("cancelled startup cleanup used success policy")
		}
	default:
		t.Fatal("completed Prepare was not cleaned after cancellation")
	}
}

func TestFlushCancelledAcknowledgesBeforeTerminalTransitionAndRetries(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &orderingControl{failCancelledOnce: true}
	daemon := &daemon{store: store, control: api, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids()}}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.SetLocalState(key, "terminal_pending")
	daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	ackID := journal.PendingCommandAcknowledgements[0].AckID
	transitionID := journal.PendingTransitions[1].TransitionID
	daemon.flushRun(context.Background(), journal)

	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].AckID != ackID || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].TransitionID != transitionID {
		t.Fatalf("failed flush did not preserve retry IDs: %#v", journal)
	}
	if got, want := api.calls, []string{"ack:cancel-1", "transition:running", "transition:cancelled"}; !sameStrings(got, want) {
		t.Fatalf("first flush calls = %#v, want %#v", got, want)
	}

	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
	want := []string{"ack:cancel-1", "transition:running", "transition:cancelled", "ack:cancel-1", "transition:cancelled"}
	if !sameStrings(api.calls, want) {
		t.Fatalf("retry calls = %#v, want %#v", api.calls, want)
	}
}

func TestFlushInputTransitionStillPrecedesAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &orderingControl{}
	daemon := &daemon{store: store, control: api, options: options{newID: ids()}}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	daemon.queueCommandAcknowledgement(key, "input-1", "applied")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.flushRun(context.Background(), journal)
	if got, want := api.calls, []string{"transition:running", "ack:input-1"}; !sameStrings(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}

func startupTestDaemon(t *testing.T, store *state.Store, service workspace.Service) *daemon {
	t.Helper()
	return &daemon{
		config:       testConfig(t),
		store:        store,
		control:      &fakeControl{},
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    service,
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
}

func claimedStore(t *testing.T) (*state.Store, state.RunKey) {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	return store, key
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{ControlPlaneURL: "https://control.example.test/api", StateDir: t.TempDir(), MachineName: "machine", AgentProfiles: map[string]config.AgentProfile{"local": {Command: "agent", InputMode: config.InputModeGoal, EventFormat: config.EventFormatRaw}}, Workspaces: map[string]config.Workspace{"local": {Policy: config.WorkspacePolicyExistingCheckout, Path: t.TempDir(), Cleanup: config.CleanupNever}}, Runtime: config.Runtime{RuntimeKey: "default", Name: "runtime", Capacity: 1, AgentProfile: "local", Workspace: "local"}}
}

func ids() func() (string, error) {
	var mutex sync.Mutex
	value := 0
	return func() (string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		value++
		return "id-" + string(rune('0'+value)), nil
	}
}
func failStart(context.Context, execution.Invocation, execution.Sink) (Process, error) {
	return nil, errors.New("unexpected start")
}

type fakeEnrollment struct{ calls int }

func (client *fakeEnrollment) Enroll(context.Context, string, protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	client.calls++
	return protocol.EnrollResponse{MachineID: "machine-1", MachineToken: "machine-token"}, nil
}

type fakeControl struct {
	registerCalls int
	claimCalls    int
	eventCalls    int
	assignment    protocol.Assignment
	beforeClaim   func()
	cancel        context.CancelFunc
	claimErr      error
	claimIDs      []string
	workEnabled   <-chan struct{}
	claimBlock    <-chan struct{}
	claimEntered  chan<- struct{}
}

type orderingControl struct {
	fakeControl
	calls             []string
	terminal          bool
	failCancelledOnce bool
}

func (client *orderingControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.calls = append(client.calls, "transition:"+request.State)
	if request.State == "cancelled" && client.failCancelledOnce {
		client.failCancelledOnce = false
		return errors.New("temporary transition failure")
	}
	if request.State == "cancelled" {
		client.terminal = true
	}
	return nil
}

func (client *orderingControl) AcknowledgeCommand(_ context.Context, commandID string, _ protocol.CommandAcknowledgement) error {
	client.calls = append(client.calls, "ack:"+commandID)
	if client.terminal {
		return &control.APIError{Code: control.OwnershipLost}
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (client *fakeControl) RegisterSession(_ context.Context, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.registerCalls++
	if client.cancel != nil && client.assignment.RunID == "" {
		client.cancel()
	}
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}}, nil
}
func (client *fakeControl) Heartbeat(context.Context, string, protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	return protocol.RuntimeSnapshot{}, nil
}
func (client *fakeControl) Work(context.Context, string, int64) (protocol.RuntimeSnapshot, error) {
	if client.workEnabled != nil {
		select {
		case <-client.workEnabled:
		default:
			return protocol.RuntimeSnapshot{}, nil
		}
	}
	if client.assignment.RunID == "" {
		return protocol.RuntimeSnapshot{}, nil
	}
	assignment := client.assignment
	client.assignment = protocol.Assignment{}
	return protocol.RuntimeSnapshot{Assignments: []protocol.Assignment{assignment}}, nil
}
func (client *fakeControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.claimCalls++
	client.claimIDs = append(client.claimIDs, request.ClaimID)
	if client.claimEntered != nil {
		client.claimEntered <- struct{}{}
	}
	if client.claimBlock != nil {
		<-client.claimBlock
	}
	if client.beforeClaim != nil {
		client.beforeClaim()
	}
	if client.claimErr != nil {
		return protocol.ClaimResponse{}, client.claimErr
	}
	return protocol.ClaimResponse{RunID: runID, Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "work"}}, nil
}
func (client *fakeControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (client *fakeControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	client.eventCalls++
	return nil
}
func (client *fakeControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	if request.State == "completed" && client.cancel != nil {
		client.cancel()
	}
	return nil
}
func (client *fakeControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	return protocol.ReconcileResponse{}, nil
}
func (client *fakeControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	return nil
}

type fakeWorkspace struct{}

func (*fakeWorkspace) Prepare(_ context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}
func (*fakeWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error { return nil }

type trackingWorkspace struct{ cleaned chan<- bool }

func (*trackingWorkspace) Prepare(_ context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}

func (workspace *trackingWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	workspace.cleaned <- succeeded
	return nil
}

type blockingWorkspace struct {
	entered   chan struct{}
	cancelled chan struct{}
	cleaned   chan bool
}

func newBlockingWorkspace() *blockingWorkspace {
	return &blockingWorkspace{entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1), cleaned: make(chan bool, 1)}
}

func (service *blockingWorkspace) Prepare(ctx context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	service.entered <- struct{}{}
	<-ctx.Done()
	service.cancelled <- struct{}{}
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}

func (service *blockingWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	service.cleaned <- succeeded
	return nil
}

type fakeNotifier struct {
	before func()
	hints  []notification.Hint
}

func (client fakeNotifier) Run(ctx context.Context, hints chan<- notification.Hint) error {
	if client.before != nil {
		client.before()
	}
	for _, hint := range client.hints {
		hints <- hint
	}
	<-ctx.Done()
	return ctx.Err()
}

type fakeProcess struct{ result execution.Result }

func (process fakeProcess) WriteInput([]byte) error                        { return nil }
func (process fakeProcess) Terminate(context.Context, time.Duration) error { return nil }
func (process fakeProcess) Wait() execution.Result                         { return process.result }

type recordingProcess struct{ input []byte }

func (process *recordingProcess) WriteInput(input []byte) error {
	process.input = append(process.input, input...)
	return nil
}
func (*recordingProcess) Terminate(context.Context, time.Duration) error { return nil }
func (*recordingProcess) Wait() execution.Result                         { return execution.Result{} }
