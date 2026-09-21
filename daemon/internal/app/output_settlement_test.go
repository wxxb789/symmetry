package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const outputSettlementHelperEnvironment = "SYMMETRY_OUTPUT_SETTLEMENT_HELPER"

func init() {
	if os.Getenv(outputSettlementHelperEnvironment) != "1" {
		return
	}
	if err := runOutputSettlementHelper(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func runOutputSettlementHelper() error {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return errors.New("output settlement helper did not receive a turn")
	}
	if _, err := os.Stdout.Write([]byte("native output pending at semantic settlement\n")); err != nil {
		return fmt.Errorf("write pending native output: %w", err)
	}
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("wait for native input close: %w", err)
	}
	return nil
}

func TestRunnerOutputTruncationCannotBeHiddenByNativeSemanticSuccess(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := t.TempDir()
	capabilities := verifiedCodexCapabilities()
	adapter := newOutputSettlementAdapter(executable, workspacePath, capabilities, validNativeTaskResult(t, admission))
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, adapter); err != nil {
		t.Fatal(err)
	}

	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	originalFingerprint := workspaceFingerprint
	workspaceFingerprint = func(context.Context, workspace.Prepared) (string, error) {
		return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	t.Cleanup(func() { workspaceFingerprint = originalFingerprint })

	input, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	controlClient := &nativeAdmissionControl{
		fakeControl: &fakeControl{},
		work:        protocol.Work{Goal: "exercise output settlement", Input: input},
		admission:   admission,
	}
	workspaceService := &outputSettlementWorkspace{path: workspacePath, subject: admission.Subject}
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = capabilities.NativeVersion
	value.Runtime.AdapterVersion = capabilities.ImplementationVersion
	value.Runtime.AdapterProtocolVersion = capabilities.ProtocolVersion
	value.Runtime.RepositoryResourceID = admission.Subject.ResourceID
	app := &daemon{
		config:              value,
		store:               store,
		control:             controlClient,
		workspace:           workspaceService,
		harnessRegistry:     registry,
		harnessCapabilities: capabilities,
		log:                 slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:             options{newID: ids(), clock: time.Now},
		machineID:           "machine-1",
		runtimeID:           "runtime-1",
		runtimeEpoch:        1,
		running:             make(map[state.RunKey]*runningRun),
		slots:               make(chan struct{}, 1),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	app.startAssignment(ctx, protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
	app.workers.Wait()
	if err := ctx.Err(); err != nil {
		t.Fatalf("native output settlement did not complete: %v", err)
	}

	var session *outputSettlementSession
	select {
	case session = <-adapter.started:
	default:
		t.Fatal("native adapter did not expose its runner-backed session")
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !result.Process.OutputTruncated {
		t.Fatalf("runner result = %+v, want observed output truncation", result.Process)
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	outputTruncatedPersisted := false
	for _, event := range journal.PendingEvents {
		if event.Kind == "output_truncated" {
			outputTruncatedPersisted = true
			break
		}
	}
	if journal.TerminalState != "failed" && !outputTruncatedPersisted {
		t.Fatalf("runner output truncation was hidden by semantic success: terminal=%q events=%#v", journal.TerminalState, journal.PendingEvents)
	}
	if journal.TerminalState == "failed" {
		var terminal *protocol.StateTransitionRequest
		for index := range journal.PendingTransitions {
			if isTerminalTransition(journal.PendingTransitions[index].State) {
				terminal = &journal.PendingTransitions[index]
				break
			}
		}
		if terminal == nil {
			t.Fatalf("pending transitions = %#v, want a terminal transition", journal.PendingTransitions)
		}
		var payload map[string]any
		if err := json.Unmarshal(terminal.Payload, &payload); err != nil {
			t.Fatalf("decode terminal payload: %v", err)
		}
		if payload["output_truncated"] != true {
			t.Fatalf("terminal payload = %#v, want output_truncated=true", payload)
		}
	}
	if journal.LocalState != "terminal_pending" {
		t.Fatalf("durable output settlement state = %q, want terminal_pending before cleanup", journal.LocalState)
	}
	if calls := workspaceService.cleanupCalls.Load(); calls != 0 {
		t.Fatalf("workspace cleanup calls = %d before durable output settlement", calls)
	}
}

func TestNativeTerminalPayloadPreservesOutputFailureEvidence(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	tests := []struct {
		name        string
		process     execution.Result
		evidenceKey string
		evidence    string
	}{
		{name: "truncated", process: execution.Result{OutputTruncated: true}, evidenceKey: "output_truncated", evidence: "output was truncated"},
		{name: "sink error", process: execution.Result{SinkError: errors.New("sink unavailable")}, evidenceKey: "sink_error", evidence: "sink unavailable"},
		{name: "output error", process: execution.Result{OutputError: errors.New("pipe failed")}, evidenceKey: "output_error", evidence: "pipe failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateName, payload := nativeTerminalPayload(harness.TaskResult{
				Kind:    harness.ResultCancelled,
				Summary: "native cancellation completed",
				Process: test.process,
			}, &admission, nil, nil)
			if stateName != "failed" || payload["reason"] != string(protocol.TaskResultReasonProcessFailure) {
				t.Fatalf("terminal = %q payload = %#v", stateName, payload)
			}
			if test.evidenceKey == "output_truncated" {
				if payload[test.evidenceKey] != true {
					t.Fatalf("payload = %#v, want %s=true", payload, test.evidenceKey)
				}
			} else if payload[test.evidenceKey] != test.evidence {
				t.Fatalf("payload = %#v, want %s=%q", payload, test.evidenceKey, test.evidence)
			}
			errorText, _ := payload["error"].(string)
			if !strings.Contains(errorText, test.evidence) {
				t.Fatalf("payload error = %q, want evidence %q", errorText, test.evidence)
			}
		})
	}
}

func TestTerminalFlushDrainsNewOutputTruncatedMarkerBeforeCompletion(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, `C:\workspace`)
	now := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	payload := json.RawMessage(`{"chunk":"first"}`)
	if _, dropped, err := store.QueueOutputEvent(key, protocol.RunEvent{
		EventID: "output-1", Kind: "output", OccurredAt: now, Payload: payload,
	}, len(payload)); err != nil || dropped {
		t.Fatalf("QueueOutputEvent(first) dropped=%t error=%v", dropped, err)
	}
	if _, dropped, err := store.QueueOutputEvent(key, protocol.RunEvent{
		EventID: "output-2", Kind: "output", OccurredAt: now.Add(time.Second), Payload: json.RawMessage(`{"chunk":"discarded"}`),
	}, len(payload)); err != nil || !dropped {
		t.Fatalf("QueueOutputEvent(discarded) dropped=%t error=%v", dropped, err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{"exit_code":0}`),
	}); err != nil {
		t.Fatal(err)
	}

	controlClient := &orderingControl{recordEvents: true}
	app := &daemon{
		store: store, control: controlClient, workspace: &fakeWorkspace{},
		options: options{newID: func() (string, error) { return "output-truncated-1", nil }, clock: func() time.Time { return now.Add(2 * time.Second) }},
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if got, want := controlClient.calls, []string{"event:output", "event:output_truncated", "transition:completed"}; !sameStrings(got, want) {
		t.Fatalf("flush calls = %#v, want %#v", got, want)
	}
}

type outputSettlementAdapter struct {
	executable   string
	workspace    string
	capabilities harness.Capabilities
	result       protocol.TaskResult
	started      chan *outputSettlementSession
}

func newOutputSettlementAdapter(executable, workspacePath string, capabilities harness.Capabilities, result protocol.TaskResult) *outputSettlementAdapter {
	return &outputSettlementAdapter{
		executable:   executable,
		workspace:    workspacePath,
		capabilities: capabilities,
		result:       result,
		started:      make(chan *outputSettlementSession, 1),
	}
}

func (adapter *outputSettlementAdapter) Probe(context.Context) (harness.Capabilities, error) {
	return adapter.capabilities, nil
}

func (adapter *outputSettlementAdapter) Start(ctx context.Context, request harness.StartRequest, _ harness.EventSink) (harness.Session, error) {
	session := &outputSettlementSession{
		result:      adapter.result,
		outputReady: make(chan struct{}),
		closeDone:   make(chan struct{}),
	}
	invocation := request.Invocation
	invocation.Program = adapter.executable
	invocation.Args = nil
	invocation.Dir = adapter.workspace
	invocation.Env = append(os.Environ(), outputSettlementHelperEnvironment+"=1")
	invocation.InitialInput = nil
	invocation.CloseInputAfterInitial = false
	invocation.PersistProcess = request.PersistProcess
	invocation.PersistProcessAuthority = request.PersistProcessAuthority
	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
	process, err := execution.NewRunner().Start(ctx, invocation, execution.SinkFunc(session.handleOutput))
	if process == nil {
		return nil, err
	}
	session.process = process
	adapter.started <- session
	return session, err
}

type outputSettlementSession struct {
	process     *execution.Process
	result      protocol.TaskResult
	outputReady chan struct{}
	outputOnce  sync.Once
	closeOnce   sync.Once
	closeDone   chan struct{}
	closeErr    error
}

func (session *outputSettlementSession) handleOutput(ctx context.Context, _ execution.Event) error {
	session.outputOnce.Do(func() { close(session.outputReady) })
	<-ctx.Done()
	return ctx.Err()
}

func (session *outputSettlementSession) ProcessDetails() (int, string) {
	return session.process.ProcessDetails()
}

func (session *outputSettlementSession) Open(context.Context) (harness.NativeSessionHandle, error) {
	return harness.NativeSessionHandle{ID: "native-output-settlement"}, nil
}

func (session *outputSettlementSession) StartTurn(ctx context.Context, _ harness.TurnRequest) error {
	return session.process.WriteInputContext(ctx, []byte("start\n"))
}

func (session *outputSettlementSession) WaitTurn(ctx context.Context) error {
	select {
	case <-session.outputReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *outputSettlementSession) Control(_ context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	return harness.ControlReceipt{
		CommandID: request.CommandID,
		Kind:      request.Kind,
		Outcome:   harness.ControlUnsupported,
	}, nil
}

func (session *outputSettlementSession) Close(ctx context.Context) error {
	session.closeOnce.Do(func() {
		session.closeErr = session.process.Terminate(ctx, 0)
		close(session.closeDone)
	})
	select {
	case <-session.closeDone:
		return session.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *outputSettlementSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	select {
	case <-session.process.ResultDone():
		return harness.TaskResult{
			Kind:     harness.ResultSucceeded,
			Summary:  session.result.Summary,
			Semantic: &session.result,
			Process:  session.process.Wait(),
			Usage:    harness.Usage{State: harness.UsageUnknown},
		}, nil
	case <-ctx.Done():
		return harness.TaskResult{}, ctx.Err()
	}
}

type outputSettlementWorkspace struct {
	path         string
	subject      protocol.Subject
	cleanupCalls atomic.Int32
}

func (service *outputSettlementWorkspace) Prepare(_ context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: service.path, BindingKey: key, Run: run}, nil
}

func (service *outputSettlementWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, _ string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: service.path, BindingKey: key, Run: run}, nil
}

func (service *outputSettlementWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	service.cleanupCalls.Add(1)
	return nil
}

func (service *outputSettlementWorkspace) PrepareSubject(_ context.Context, key string, run workspace.RunRef, subject protocol.Subject) (workspace.SubjectWorkspace, error) {
	return workspace.SubjectWorkspace{
		Prepared: workspace.Prepared{Path: service.path, BindingKey: key, Run: run},
		Subject:  subject,
	}, nil
}

func (service *outputSettlementWorkspace) DeriveSubject(_ context.Context, _ workspace.Prepared, resourceID string) (protocol.Subject, error) {
	if service.subject.ResourceID != resourceID {
		return protocol.Subject{}, errors.New("output settlement workspace resource does not match admission")
	}
	return service.subject, nil
}
