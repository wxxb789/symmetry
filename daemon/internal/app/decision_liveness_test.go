package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const decisionLivenessChildEnv = "SYMMETRY_TEST_DECISION_LIVENESS_CHILD"

func TestDecisionLivenessChild(t *testing.T) {
	if os.Getenv(decisionLivenessChildEnv) != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, `{"type":"waiting_for_input","question":"Choose a strategy?"}`)
	// This is a real blocked interactive child, not a process whose Wait has
	// already completed. A sink error alone cannot release this read.
	if !scanner.Scan() {
		os.Exit(3)
	}
	os.Exit(0)
}

type decisionLivenessControl struct {
	orderingControl
	required bool
}

func (api *decisionLivenessControl) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	claim, err := api.fakeControl.Claim(ctx, runID, request)
	if api.required {
		claim.Work.RequiredCapabilities = map[string]bool{"supervisory_control": true}
	}
	return claim, err
}

type decisionLivenessWorkspace struct {
	fakeWorkspace
	directory string
}

func (service decisionLivenessWorkspace) Prepare(_ context.Context, binding string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: service.directory, BindingKey: binding, Run: run}, nil
}

func decisionLivenessDaemon(t *testing.T, required bool) (*daemon, state.RunKey) {
	t.Helper()
	t.Setenv(decisionLivenessChildEnv, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	configuration := testConfig(t)
	configuration.AgentProfiles["local"] = config.AgentProfile{
		Command: executable, Args: []string{"-test.run=^TestDecisionLivenessChild$"}, InputMode: config.InputModeJSON,
		EventFormat: config.EventFormatJSONL, Interactive: true, SupervisoryControl: true, EnvAllowlist: []string{decisionLivenessChildEnv},
	}
	app := &daemon{
		config: configuration, store: store, options: options{newID: ids(), clock: time.Now},
		control: &decisionLivenessControl{required: required}, workspace: &decisionLivenessWorkspace{directory: t.TempDir()},
		log: slog.New(slog.NewJSONHandler(io.Discard, nil)), runtimeID: "runtime-1", runtimeEpoch: 1,
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1), outboxWake: make(chan struct{}, 1),
	}
	return app, state.RunKey{RunID: "decision-liveness", Generation: 1}
}

func TestRequiredInvalidDecisionStopsRealBlockedChildBeforeAndAfterAttachment(t *testing.T) {
	for _, beforeAttachment := range []bool{false, true} {
		name := "normal_launch"
		if beforeAttachment {
			name = "receipt_before_attachment"
		}
		t.Run(name, func(t *testing.T) {
			app, key := decisionLivenessDaemon(t, true)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			runner := execution.NewRunner()
			app.start = func(executionContext context.Context, invocation execution.Invocation, sink execution.Sink) (Process, error) {
				receipt := make(chan error, 1)
				var once sync.Once
				wrapped := execution.SinkFunc(func(sinkContext context.Context, event execution.Event) error {
					err := sink.Handle(sinkContext, event)
					if err != nil {
						once.Do(func() { receipt <- err })
					}
					return err
				})
				process, err := runner.Start(executionContext, invocation, wrapped)
				if err != nil || !beforeAttachment {
					return process, err
				}
				select {
				case err := <-receipt:
					if !errors.Is(err, errRequiredDecisionPacket) {
						return process, fmt.Errorf("unexpected sink failure: %w", err)
					}
				case <-ctx.Done():
					return process, ctx.Err()
				}
				// The daemon has not attached process yet. Its dedicated process
				// context must already have been cancelled by the invalid packet.
				select {
				case <-executionContext.Done():
					return process, nil
				case <-ctx.Done():
					return process, ctx.Err()
				}
			}
			app.startAssignment(ctx, protocol.Assignment{RunID: key.RunID, Generation: key.Generation})
			done := make(chan struct{})
			go func() { app.workers.Wait(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("invalid required decision left the real child blocked on stdin")
			}
			if ctx.Err() != nil {
				t.Fatal("invalid decision was stopped only by test timeout")
			}
			journal := supervisoryJournal(t, app, key)
			if journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || app.isCancelled(key) {
				t.Fatalf("contract failure became operator cancellation or remained live: %#v", journal)
			}
			foundCause := false
			for _, transition := range journal.PendingTransitions {
				if transition.State == "waiting_for_input" {
					t.Fatal("invalid decision poisoned lifecycle outbox")
				}
				if transition.State == "failed" && strings.Contains(string(transition.Payload), errRequiredDecisionPacket.Error()) {
					foundCause = true
				}
			}
			if !foundCause {
				t.Fatalf("terminal failure lost original decision cause: %#v", journal.PendingTransitions)
			}
			for _, event := range journal.PendingEvents {
				if event.Kind == "waiting_for_input" {
					t.Fatal("invalid decision poisoned event outbox")
				}
			}
			if err := app.flushRun(context.Background(), journal); err != nil {
				t.Fatal(err)
			}
			if len(app.slots) != 0 || app.hasRun(key) {
				t.Fatal("failed invalid decision retained execution capacity after flush")
			}
		})
	}
}

func TestLegacyQuestionOnlyDecisionKeepsRealChildLiveUntilInput(t *testing.T) {
	app, key := decisionLivenessDaemon(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runner := execution.NewRunner()
	launched := make(chan context.Context, 1)
	app.start = func(executionContext context.Context, invocation execution.Invocation, sink execution.Sink) (Process, error) {
		launched <- executionContext
		return runner.Start(executionContext, invocation, sink)
	}
	app.startAssignment(ctx, protocol.Assignment{RunID: key.RunID, Generation: key.Generation})
	done := make(chan struct{})
	go func() { app.workers.Wait(); close(done) }()
	var executionContext context.Context
	select {
	case executionContext = <-launched:
	case <-ctx.Done():
		t.Fatal("legacy child did not launch")
	}
	// Wakeups are emitted after durable output and attachment. Wait on those
	// actual notifications, rather than polling a journal or sleeping.
	for {
		select {
		case <-app.outboxWake:
			app.mu.Lock()
			active := app.running[key]
			attached := active != nil && !active.starting && active.process != nil
			app.mu.Unlock()
			journal := supervisoryJournal(t, app, key)
			if attached && journal.LocalState == "waiting_for_input" {
				goto waiting
			}
		case <-done:
			t.Fatal("legacy question-only child stopped before explicit input")
		case <-ctx.Done():
			t.Fatal("legacy question-only child never reached a live wait")
		}
	}

waiting:
	if executionContext.Err() != nil {
		t.Fatalf("legacy wait cancelled execution context: %v", context.Cause(executionContext))
	}
	select {
	case <-done:
		t.Fatal("legacy process ended while waiting for input")
	default:
	}
	if !app.handleCommand(context.Background(), protocol.Command{CommandID: "answer-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: []byte(`{"answer":"Use staging."}`)}) {
		t.Fatal("legacy input rejected")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("legacy process did not finish after explicit input")
	}
	if journal := supervisoryJournal(t, app, key); journal.TerminalState != "completed" {
		t.Fatalf("legacy input did not finish normal execution: %#v", journal)
	}
}
