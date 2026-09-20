package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestNativeStartCancellationAcrossBlockingPublicationStagesIsAcknowledged(t *testing.T) {
	for _, stage := range []string{"open", "attach", "context"} {
		t.Run(stage, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			app, store, _, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
			app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))

			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				app.workers.Wait()
				_ = store.Close()
			})
			switch stage {
			case "open":
				session := &blockingOpenNativeSession{
					fakeNativeGoalSession: &fakeNativeGoalSession{
						handle:          harness.NativeSessionHandle{ID: "native-thread-1"},
						result:          harness.TaskResult{Kind: harness.ResultCancelled},
						turnStarted:     make(chan struct{}),
						processPID:      71,
						processIdentity: "native:71",
					},
					entered: entered,
					release: release,
				}
				registry := harness.NewRegistry()
				if err := registry.Register(harness.KindCodex, &blockingOpenNativeAdapter{
					capabilities: verifiedCodexCapabilities(),
					session:      session,
				}); err != nil {
					t.Fatal(err)
				}
				app.harnessRegistry = registry
			case "attach", "context":
				app.control = &blockingNativeAdmissionControl{
					nativeAdmissionControl: controlClient,
					stage:                  stage,
					entered:                entered,
					release:                release,
				}
			default:
				t.Fatalf("unknown stage %q", stage)
			}

			key := state.RunKey{RunID: "run-1", Generation: 1}
			app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
			awaitNativeSettlementSignal(t, entered, "native start did not reach the blocked "+stage+" boundary")

			command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-during-" + stage, Kind: "cancel"}
			if app.handleCommand(context.Background(), command) {
				t.Fatal("cancellation was acknowledged before the native start owner settled")
			}
			awaitNativeSettlementWorkers(t, app)

			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.TerminalState != "cancelled" || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" {
				t.Fatalf("blocked %s cancellation terminal = %#v, want one cancelled transition", stage, journal)
			}
			if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != command.CommandID || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
				t.Fatalf("blocked %s cancellation acknowledgement = %#v, want one applied acknowledgement", stage, journal.PendingCommandAcknowledgements)
			}
		})
	}
}

func TestCancelledNativeStartCloseFailureQueuesExplicitUsageBeforeTerminal(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	session := &fakeNativeGoalSession{
		result: harness.TaskResult{
			Kind:    harness.ResultCancelled,
			Summary: "cancelled after native start publication",
			Usage: harness.Usage{
				State:        harness.UsageReported,
				InputTokens:  17,
				OutputTokens: 5,
				CostMicrousd: "29",
			},
		},
		turnStarted:     make(chan struct{}),
		processPID:      71,
		processIdentity: "native:71",
		closeErr:        errors.New("injected cancelled-start close failure"),
	}
	active := &runningRun{
		starting:            true,
		claimed:             true,
		nativeStartInFlight: true,
		nativeSession:       session,
		goalSession:         &sessionKey,
		goalAdmission:       &admission,
		cancelled:           true,
		cancelCommandID:     "cancel-start-close-failure",
		cleanupBlocked:      true,
	}
	var order []string
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			queueGoalUsage: func(runKey state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
				order = append(order, "usage")
				return store.QueueGoalUsage(runKey, usage)
			},
			queueTerminalTransitionAndAcknowledgement: func(runKey state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, at time.Time) (state.RunJournal, error) {
				order = append(order, "terminal")
				return store.QueueTerminalTransitionAndAcknowledgementAt(runKey, transition, acknowledgement, at)
			},
		},
	}

	settled, _ := app.settleCancelledNativeStart(context.Background(), key, sessionKey, sessionKey.LocalHandleID, session, false, false)
	if !settled {
		t.Fatal("cancelled native start was not settled")
	}
	if len(order) < 2 || order[0] != "usage" || order[1] != "terminal" {
		t.Fatalf("cancelled-start persistence order = %#v, want usage before terminal", order)
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	var usage *protocol.Usage
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			usage = delivery.Usage
			break
		}
	}
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 17 || usage.OutputTokens == nil || *usage.OutputTokens != 5 || usage.CostMicrousd == nil || *usage.CostMicrousd != "29" || usage.CostBasis != protocol.CostReported {
		t.Fatalf("cancelled-start explicit usage = %#v", usage)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "failed" {
		t.Fatalf("cancelled-start close failure settlement = %#v", journal)
	}
	if !active.nativeCloseRetryRequired || !active.cleanupBlocked || active.nativeSession != session {
		t.Fatalf("cancelled-start close failure lost cleanup barrier: %#v", active)
	}
	if active.nativeFinalUsage == nil || active.nativeFinalUsage.InputTokens == nil || *active.nativeFinalUsage.InputTokens != 17 {
		t.Fatalf("cancelled-start usage snapshot = %#v", active.nativeFinalUsage)
	}
}

func TestRejectedCancelAfterObservedTerminalUsesFinalResultPastWaitTurnDeadline(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	semantic := validNativeTaskResult(t, admission)
	base := &fakeNativeGoalSession{
		result:          harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "terminal result", Semantic: &semantic},
		turnStarted:     make(chan struct{}),
		processPID:      71,
		processIdentity: "native:71",
	}
	session := &sharedTerminalWaitSession{
		fakeNativeGoalSession: base,
		terminalObserved:      make(chan struct{}),
		waitTurnEntered:       make(chan struct{}, 2),
		releaseWaitTurn:       make(chan struct{}),
	}
	var releaseWaitTurnOnce sync.Once
	releaseWaitTurn := func() {
		releaseWaitTurnOnce.Do(func() { close(session.releaseWaitTurn) })
	}
	t.Cleanup(releaseWaitTurn)
	active := &runningRun{
		claimed:       true,
		nativeSession: session,
		goalSession:   &sessionKey,
		goalAdmission: &admission,
		prepared:      workspace.Prepared{Path: `C:\workspace`},
	}
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		control: &fakeControl{},
		workspace: &fakeWorkspace{
			subject: admission.Subject,
		},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: time.Now},
	}

	waiterDone := make(chan struct{})
	go func() {
		app.waitForNativeRun(context.Background(), key, active, session)
		close(waiterDone)
	}()
	awaitNativeSettlementSignal(t, session.waitTurnEntered, "native waiter did not observe the terminal turn")

	command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-after-terminal-timeout", Kind: "cancel"}
	cancelContext, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	cancelDone := make(chan bool, 1)
	go func() {
		cancelDone <- app.handleCommand(cancelContext, command)
	}()
	awaitNativeSettlementSignal(t, session.waitTurnEntered, "cancellation did not share the blocked WaitTurn barrier")

	select {
	case acknowledged := <-cancelDone:
		if !acknowledged {
			t.Fatal("explicitly rejected cancellation was not durably acknowledged")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish after its WaitTurn deadline")
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "completed" || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "completed" {
		t.Fatalf("final native result was replaced after rejected cancellation: %#v", journal)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != command.CommandID || journal.PendingCommandAcknowledgements[0].Outcome != "rejected" {
		t.Fatalf("explicit cancel rejection acknowledgement = %#v", journal.PendingCommandAcknowledgements)
	}

	releaseWaitTurn()
	awaitNativeSettlementSignal(t, waiterDone, "original native terminal waiter did not exit")
}

func TestLateCancelSubjectMismatchSurvivesUsageRetry(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	candidate := admission.Subject
	candidate.Commit = strings.Repeat("1", 40)
	candidate.TreeDigest = "sha256:" + strings.Repeat("1", 64)
	candidateHash, err := candidate.Hash()
	if err != nil {
		t.Fatal(err)
	}
	semantic := validNativeTaskResult(t, admission)
	semantic.Kind = protocol.TaskResultCandidateCompletion
	semantic.Subject = candidate
	semantic.SubjectHash = candidateHash

	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	session := &rejectedCancelNativeSession{fakeNativeGoalSession: &fakeNativeGoalSession{
		result: harness.TaskResult{
			Kind:     harness.ResultSucceeded,
			Summary:  "candidate from the wrong worktree",
			Semantic: &semantic,
			Usage: harness.Usage{
				State:        harness.UsageReported,
				InputTokens:  11,
				OutputTokens: 7,
				CostMicrousd: "23",
			},
		},
		processPID:      71,
		processIdentity: "native:71",
	}}
	active := &runningRun{
		claimed:       true,
		nativeSession: session,
		goalSession:   &sessionKey,
		goalAdmission: &admission,
		prepared:      workspace.Prepared{Path: `C:\workspace`},
	}
	usageAttempts := 0
	app := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		workspace: &fakeWorkspace{subject: admission.Subject},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:   map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			queueGoalUsage: func(runKey state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
				usageAttempts++
				if usageAttempts == 1 {
					return state.RunJournal{}, errors.New("injected usage persistence failure")
				}
				return store.QueueGoalUsage(runKey, usage)
			},
		},
	}
	command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-subject-mismatch", Kind: "cancel"}
	if app.handleCommand(context.Background(), command) {
		t.Fatal("late cancellation was acknowledged before usage retry")
	}
	if active.nativeFinalWaitErr == nil || !strings.Contains(active.nativeFinalWaitErr.Error(), "does not match the daemon-derived worktree Subject") {
		t.Fatalf("deferred terminal validation error = %v", active.nativeFinalWaitErr)
	}

	app.mu.Lock()
	active.nativeUsageRetryAt = time.Time{}
	app.mu.Unlock()
	app.flushPendingNativeUsage(context.Background())
	app.backgroundWG.Wait()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || !strings.Contains(string(journal.PendingTransitions[0].Payload), "does not match the daemon-derived worktree Subject") {
		t.Fatalf("subject mismatch terminal after usage retry = %#v", journal)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != command.CommandID || journal.PendingCommandAcknowledgements[0].Outcome != "rejected" {
		t.Fatalf("subject mismatch cancellation acknowledgement = %#v", journal.PendingCommandAcknowledgements)
	}
}

func TestTerminalAndCancelAcknowledgementPostWriteFailureSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now},
	}

	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		return errors.New("injected post-write unknown terminal receipt")
	})
	payload := map[string]any{
		"reason":  string(protocol.TaskResultReasonUnknownOutcome),
		"summary": "native terminal observed while cancel response was lost",
	}
	commandID := "cancel-after-terminal"
	if err := app.queueTerminalTransitionAndAcknowledgementWithContext(context.Background(), key, "failed", payload, commandID, "failed"); err != nil {
		restore()
		_ = store.Close()
		t.Fatal(err)
	}
	restore()
	if writes != 1 {
		_ = store.Close()
		t.Fatalf("atomic terminal receipt writes = %d, want one write resolved by readback", writes)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedApp := &daemon{
		config:  testConfig(t),
		store:   restarted,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now},
	}
	command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: commandID, Kind: "cancel"}
	if !restartedApp.handleCommand(context.Background(), command) {
		t.Fatal("restart did not replay the durable terminal acknowledgement")
	}
	journal, err := restarted.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("restarted atomic terminal receipt = %#v", journal)
	}
	if acknowledgement := journal.PendingCommandAcknowledgements[0]; acknowledgement.CommandID != commandID || acknowledgement.Outcome != "failed" {
		t.Fatalf("restarted cancellation acknowledgement = %#v", acknowledgement)
	}
}

func TestAppliedNativeCancelOutputTruncationFailsWithEvidence(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	session := &fakeNativeGoalSession{
		result: harness.TaskResult{
			Kind:    harness.ResultCancelled,
			Summary: "cancelled with discarded output",
			Process: execution.Result{OutputTruncated: true},
		},
		processPID:      71,
		processIdentity: "native:71",
	}
	active := &runningRun{
		claimed:       true,
		nativeSession: session,
		goalSession:   &sessionKey,
		goalAdmission: &admission,
		prepared:      workspace.Prepared{Path: `C:\workspace`},
	}
	app := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		workspace: &fakeWorkspace{subject: admission.Subject},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:   map[state.RunKey]*runningRun{key: active},
		options:   options{newID: ids(), clock: time.Now},
	}
	command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-output-truncated", Kind: "cancel"}
	if !app.handleCommand(context.Background(), command) {
		t.Fatal("applied native cancellation was not durably acknowledged")
	}
	assertNativeOutputFailureTerminal(t, store, key, command.CommandID, "applied")
}

func TestCancelledNativeStartOutputTruncationFailsWithEvidence(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	session := &fakeNativeGoalSession{
		result: harness.TaskResult{
			Kind:    harness.ResultCancelled,
			Summary: "cancelled start with discarded output",
			Process: execution.Result{OutputTruncated: true},
		},
		processPID:      71,
		processIdentity: "native:71",
	}
	active := &runningRun{
		starting:            true,
		claimed:             true,
		nativeStartInFlight: true,
		nativeSession:       session,
		goalSession:         &sessionKey,
		goalAdmission:       &admission,
		cancelled:           true,
		cancelCommandID:     "cancel-start-output-truncated",
		cleanupBlocked:      true,
	}
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: time.Now},
	}
	settled, err := app.settleCancelledNativeStart(context.Background(), key, sessionKey, sessionKey.LocalHandleID, session, false, false)
	if err != nil || !settled {
		t.Fatalf("settle cancelled native start = %t, %v", settled, err)
	}
	assertNativeOutputFailureTerminal(t, store, key, active.cancelCommandID, "applied")
}

func TestNativeUsageRetryExhaustionKeepsAtomicCancelOutcomeAndOutputEvidence(t *testing.T) {
	tests := []struct {
		name             string
		process          execution.Result
		terminalObserved bool
		cancelRejected   bool
		wantOutcome      string
		wantEvidenceKey  string
		wantEvidence     string
	}{
		{
			name:             "rejected late cancel with truncated output",
			process:          execution.Result{OutputTruncated: true},
			terminalObserved: true,
			cancelRejected:   true,
			wantOutcome:      "rejected",
			wantEvidenceKey:  "output_truncated",
			wantEvidence:     "output was truncated",
		},
		{
			name:            "ordinary cancel with sink error",
			process:         execution.Result{SinkError: errors.New("sink unavailable")},
			wantOutcome:     "applied",
			wantEvidenceKey: "sink_error",
			wantEvidence:    "sink unavailable",
		},
		{
			name:            "ordinary cancel with output error",
			process:         execution.Result{OutputError: errors.New("pipe failed")},
			wantOutcome:     "applied",
			wantEvidenceKey: "output_error",
			wantEvidence:    "pipe failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			usage := protocol.Usage{
				SchemaVersion: protocol.UsageSchemaVersion,
				UsageID:       "00000000-0000-4000-8000-000000000010",
				RunID:         key.RunID,
				UsageKey:      nativeGoalUsageKey,
				Provider:      "openai",
				Model:         "gpt-test",
				CostBasis:     protocol.CostUnknown,
				ObservedAt:    "2026-09-20T01:00:00Z",
			}
			result := harness.TaskResult{Kind: harness.ResultCancelled, Summary: "usage retry exhausted", Process: test.process}
			active := &runningRun{
				nativeFinalResult:            &result,
				nativeFinalUsage:             &usage,
				nativeCancelCommandID:        "cancel-1",
				nativeCancelTerminalObserved: test.terminalObserved,
				nativeCancelRejected:         test.cancelRejected,
				nativeTerminalOwner:          true,
				terminalizing:                1,
				nativeUsageRetryExhausted:    true,
			}
			atomicCalls := 0
			app := &daemon{
				config:  testConfig(t),
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: map[state.RunKey]*runningRun{key: active},
				options: options{
					newID: ids(),
					clock: time.Now,
					queueTerminalTransitionAndAcknowledgement: func(runKey state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, at time.Time) (state.RunJournal, error) {
						atomicCalls++
						return store.QueueTerminalTransitionAndAcknowledgementAt(runKey, transition, acknowledgement, at)
					},
				},
			}

			app.exhaustNativeUsageRetry(context.Background(), key, active, errors.New("usage retries exhausted"))

			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if atomicCalls != 1 || journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || len(journal.PendingCommandAcknowledgements) != 1 {
				t.Fatalf("atomic usage exhaustion settlement: calls=%d journal=%#v", atomicCalls, journal)
			}
			if acknowledgement := journal.PendingCommandAcknowledgements[0]; acknowledgement.CommandID != "cancel-1" || acknowledgement.Outcome != test.wantOutcome {
				t.Fatalf("usage exhaustion acknowledgement = %#v", acknowledgement)
			}
			var payload map[string]any
			if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["reason"] != string(protocol.TaskResultReasonProcessFailure) {
				t.Fatalf("usage exhaustion payload = %#v", payload)
			}
			if test.wantEvidenceKey == "output_truncated" {
				if payload[test.wantEvidenceKey] != true {
					t.Fatalf("usage exhaustion payload = %#v", payload)
				}
			} else if payload[test.wantEvidenceKey] != test.wantEvidence {
				t.Fatalf("usage exhaustion payload = %#v", payload)
			}
			errorText, _ := payload["error"].(string)
			if !strings.Contains(errorText, test.wantEvidence) {
				t.Fatalf("usage exhaustion error = %q, want %q", errorText, test.wantEvidence)
			}
		})
	}
}

func TestLeaseOwnedNativeStartErrorsCleanupAndAcknowledgeBeforeReturn(t *testing.T) {
	for _, test := range []struct {
		name       string
		stage      string
		nilSession bool
	}{
		{name: "start session error", stage: "start"},
		{name: "start nil session error", stage: "start", nilSession: true},
		{name: "open error", stage: "open"},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			app, store, baseSession, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
			defer store.Close()
			app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
			key := state.RunKey{RunID: "run-1", Generation: 1}
			claim := saveClaimedNativeSettlementRun(t, store, key, controlClient.work)
			runContext, cancelRun := context.WithCancel(context.Background())
			t.Cleanup(cancelRun)
			active := &runningRun{starting: true, claimed: true, cancel: cancelRun, cleanupBlocked: true}
			app.running[key] = active

			stageErr := errors.New("injected " + test.name)
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseStage := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(releaseStage)
			if test.stage == "start" {
				adapterValue, lookupErr := app.harnessRegistry.Lookup(harness.KindCodex)
				if lookupErr != nil {
					t.Fatal(lookupErr)
				}
				adapter := adapterValue.(*fakeNativeGoalAdapter)
				adapter.startEntered = entered
				adapter.startRelease = release
				adapter.startReturnErr = stageErr
				adapter.returnTypedNil = test.nilSession
			} else {
				blocking := &blockingOpenNativeSession{
					fakeNativeGoalSession: baseSession,
					entered:               entered,
					release:               release,
					openErr:               stageErr,
					ignoreCancellation:    true,
				}
				registry := harness.NewRegistry()
				if err := registry.Register(harness.KindCodex, &blockingOpenNativeAdapter{capabilities: verifiedCodexCapabilities(), session: blocking}); err != nil {
					t.Fatal(err)
				}
				app.harnessRegistry = registry
			}

			errCh := make(chan error, 1)
			go func() {
				errCh <- app.startGoalAdmission(runContext, key, claim, admission, nil)
			}()
			awaitNativeSettlementSignal(t, entered, "native "+test.stage+" stage did not block")
			app.mu.Lock()
			active.cancelled = true
			active.cancelCommandID = "cancel-before-lease-" + test.stage
			active.localLeaseDeadlineAt = time.Now().Add(-time.Second)
			app.mu.Unlock()
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			leaseReason := "lease expired before " + test.stage + " returned"
			app.terminateForLease(journal, leaseReason)
			releaseStage()

			var startErr error
			select {
			case startErr = <-errCh:
			case <-time.After(2 * time.Second):
				t.Fatal("lease-owned native start failure did not return")
			}
			if startErr == nil || !strings.Contains(startErr.Error(), leaseReason) || !strings.Contains(startErr.Error(), stageErr.Error()) {
				t.Fatalf("lease-owned %s error = %v", test.name, startErr)
			}
			calls := baseSession.callsSnapshot()
			for _, call := range calls {
				if call == "start_turn" || (test.stage == "start" && call == "open") {
					t.Fatalf("lease-owned %s failure crossed a later stage: %#v", test.name, calls)
				}
			}
			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.LocalState != "stale" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != active.cancelCommandID || journal.PendingCommandAcknowledgements[0].Outcome != "failed" {
				t.Fatalf("lease-owned %s settlement = %#v", test.name, journal)
			}
			if test.nilSession {
				sessions, listErr := store.ListGoalSessions()
				if listErr != nil || len(sessions) != 1 || !goalSessionClosed(sessions[0]) || !journal.HasProcessDetails() {
					t.Fatalf("lease-owned nil session evidence = sessions:%#v journal:%#v error:%v", sessions, journal, listErr)
				}
			}
		})
	}
}

func TestStartTurnObservedTerminalTransfersLeaseToNativeSettlement(t *testing.T) {
	for _, test := range []struct {
		name         string
		emitUsage    bool
		emitTerminal bool
	}{
		{name: "with terminal", emitUsage: true, emitTerminal: true},
		{name: "with usage only", emitUsage: true},
		{name: "without observation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			semantic := validNativeTaskResult(t, admission)
			app, store, baseSession, controlClient := nativeAdmissionDaemon(t, admission, semantic, nil)
			defer store.Close()
			app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
			baseSession.result.Usage = harness.Usage{State: harness.UsageReported, InputTokens: 13, OutputTokens: 8, CostMicrousd: "34"}
			turnObserved := make(chan struct{})
			turnRelease := make(chan struct{})
			session := &blockingStartTurnNativeSession{
				fakeNativeGoalSession: baseSession,
				emitUsage:             test.emitUsage,
				emitTerminal:          test.emitTerminal,
				turnObserved:          turnObserved,
				turnRelease:           turnRelease,
			}
			registry := harness.NewRegistry()
			if err := registry.Register(harness.KindCodex, &fixedNativeSessionAdapter{capabilities: verifiedCodexCapabilities(), session: session}); err != nil {
				t.Fatal(err)
			}
			app.harnessRegistry = registry
			var releaseOnce sync.Once
			releaseTurn := func() { releaseOnce.Do(func() { close(turnRelease) }) }
			t.Cleanup(func() {
				releaseTurn()
				app.workers.Wait()
			})

			key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000100", Generation: 1}
			app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
			awaitNativeSettlementSignal(t, turnObserved, "native StartTurn did not publish its observed events")
			app.mu.Lock()
			active := app.running[key]
			if active == nil || !active.nativeStartInFlight {
				app.mu.Unlock()
				t.Fatalf("native StartTurn barrier = %#v", active)
			}
			app.mu.Unlock()
			commandID := "cancel-during-start-turn"
			if app.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: commandID, Kind: "cancel"}) {
				t.Fatal("start-turn cancellation was acknowledged before lease settlement")
			}
			app.mu.Lock()
			if app.running[key] != active {
				app.mu.Unlock()
				t.Fatal("native StartTurn owner disappeared before lease expiry")
			}
			active.localLeaseDeadlineAt = time.Now().Add(-time.Second)
			app.mu.Unlock()
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			app.terminateForLease(journal, "lease expired after StartTurn observation")
			releaseTurn()
			awaitNativeSettlementWorkers(t, app)

			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			calls := baseSession.callsSnapshot()
			wantOutcome := "failed"
			if test.emitTerminal {
				wantOutcome = "rejected"
			}
			if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != commandID || journal.PendingCommandAcknowledgements[0].Outcome != wantOutcome {
				t.Fatalf("lease-owned cancellation acknowledgement = %#v", journal.PendingCommandAcknowledgements)
			}
			if test.emitTerminal {
				if journal.LocalState != "terminal_pending" || journal.TerminalState != "completed" || !containsNativeSettlementCall(calls, "wait_turn") || !containsNativeSettlementCall(calls, "close") || !containsNativeSettlementCall(calls, "wait") {
					t.Fatalf("observed StartTurn terminal did not settle: journal=%#v calls=%#v", journal, calls)
				}
				usagePersisted := false
				for _, delivery := range append(append([]state.GoalDelivery(nil), journal.PendingGoalDeliveries...), journal.DeliveredGoalDeliveries...) {
					if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
						usagePersisted = true
						break
					}
				}
				if !usagePersisted {
					t.Fatalf("observed StartTurn usage was not retained: %#v", journal)
				}
				if active.nativeTerminalOwner || active.stale {
					t.Fatalf("observed StartTurn owner was not released: %#v", active)
				}
				return
			}
			if journal.LocalState != "stale" || durableTerminalPresent(journal) {
				t.Fatalf("unobserved StartTurn lease did not remain stale: journal=%#v calls=%#v", journal, calls)
			}
			usagePersisted := false
			for _, delivery := range append(append([]state.GoalDelivery(nil), journal.PendingGoalDeliveries...), journal.DeliveredGoalDeliveries...) {
				if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
					usagePersisted = true
					break
				}
			}
			if test.emitUsage {
				if !usagePersisted || !containsNativeSettlementCall(calls, "wait_turn") || !containsNativeSettlementCall(calls, "close") || !containsNativeSettlementCall(calls, "wait") {
					t.Fatalf("usage-only StartTurn lease lost final accounting: journal=%#v calls=%#v", journal, calls)
				}
				if active.nativeTerminalOwner || !active.stale {
					t.Fatalf("usage-only StartTurn settlement changed stale ownership: %#v", active)
				}
				return
			}
			if usagePersisted || containsNativeSettlementCall(calls, "wait_turn") {
				t.Fatalf("StartTurn without evidence entered native settlement: journal=%#v calls=%#v", journal, calls)
			}
		})
	}
}

func TestLateCancelSubjectVerificationStopsOnDaemonShutdown(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	semantic := validNativeTaskResult(t, admission)
	session := &rejectedCancelNativeSession{fakeNativeGoalSession: &fakeNativeGoalSession{
		result:          harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "terminal result", Semantic: &semantic},
		processPID:      71,
		processIdentity: "native:71",
	}}
	deriveEntered := make(chan struct{})
	shutdownContext, shutdown := context.WithCancel(context.Background())
	active := &runningRun{
		claimed:       true,
		nativeSession: session,
		goalSession:   &sessionKey,
		goalAdmission: &admission,
		prepared:      workspace.Prepared{Path: `C:\workspace`},
	}
	app := &daemon{
		config:     testConfig(t),
		store:      store,
		control:    &fakeControl{},
		workspace:  &blockingNativeSubjectWorkspace{fakeWorkspace: &fakeWorkspace{subject: admission.Subject}, entered: deriveEntered},
		log:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		background: shutdownContext,
		running:    map[state.RunKey]*runningRun{key: active},
		options:    options{newID: ids(), clock: time.Now},
	}
	command := protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-blocked-subject", Kind: "cancel"}
	done := make(chan bool, 1)
	go func() { done <- app.handleCommand(context.Background(), command) }()
	awaitNativeSettlementSignal(t, deriveEntered, "late-cancel Subject verification did not block")
	shutdown()
	select {
	case acknowledged := <-done:
		if !acknowledged {
			t.Fatal("shutdown-cancelled Subject verification did not preserve the terminal acknowledgement")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late-cancel Subject verification ignored daemon shutdown")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || !strings.Contains(string(journal.PendingTransitions[0].Payload), context.Canceled.Error()) {
		t.Fatalf("shutdown-cancelled Subject verification terminal = %#v", journal)
	}
}

func TestCancelledNativeStartAbortPostWriteReadbackConvergesAndReleasesOwner(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveLaunchingGoalSession(t, store, key, admission, sessionKey)
	active := &runningRun{
		starting:            true,
		claimed:             true,
		nativeStartInFlight: true,
		cancelled:           true,
		cancelCommandID:     "cancel-abort-post-write",
		cleanupBlocked:      true,
	}
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			abortGoalSessionLaunchBeforeNativeStart: func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
				journal, err := store.AbortGoalSessionLaunchBeforeNativeStart(key)
				if err != nil {
					return state.GoalSessionJournal{}, err
				}
				return journal, errors.New("injected post-write abort outcome")
			},
		},
	}

	settled, err := app.settleCancelledNativeStart(context.Background(), key, sessionKey, sessionKey.LocalHandleID, nil, false, true)
	if err != nil || !settled {
		t.Fatalf("settle cancelled nil-session start = %t, %v", settled, err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("post-write abort settlement = %#v", journal)
	}
	sessionJournal, err := store.LoadGoalSession(sessionKey)
	if err != nil || !goalSessionClosed(sessionJournal) {
		t.Fatalf("post-write abort Goal session = %#v, %v", sessionJournal, err)
	}
	if active.nativeTerminalOwner || active.terminalizing != 0 {
		t.Fatalf("post-write abort retained terminal owner: %#v", active)
	}
}

func TestCancelledNativeStartAbortFailureRetainsRecoveryAndCannotCleanup(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveLaunchingGoalSession(t, store, key, admission, sessionKey)
	active := &runningRun{
		starting:            true,
		claimed:             true,
		nativeStartInFlight: true,
		cancelled:           true,
		cancelCommandID:     "cancel-abort-uncommitted",
		cleanupBlocked:      true,
	}
	app := &daemon{
		config:    testConfig(t),
		store:     store,
		workspace: &fakeWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:   map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			abortGoalSessionLaunchBeforeNativeStart: func(state.GoalSessionKey) (state.GoalSessionJournal, error) {
				return state.GoalSessionJournal{}, errors.New("injected uncommitted abort failure")
			},
		},
	}

	settled, err := app.settleCancelledNativeStart(context.Background(), key, sessionKey, sessionKey.LocalHandleID, nil, false, true)
	if err == nil || !settled {
		t.Fatalf("settle uncommitted nil-session abort = %t, %v", settled, err)
	}
	if active.nativeTerminalOwner || active.terminalizing != 0 {
		t.Fatalf("uncommitted abort retained terminal owner: %#v", active)
	}
	runJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !runJournal.RetainWorkspace || durableTerminalPresent(runJournal) || len(runJournal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("uncommitted abort run recovery = %#v", runJournal)
	}
	sessionJournal, err := store.LoadGoalSession(sessionKey)
	if err != nil || !sessionJournal.NeedsReconciliation() {
		t.Fatalf("uncommitted abort Goal session recovery = %#v, %v", sessionJournal, err)
	}
	stale, err := store.SetLocalState(key, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.cleanupPending(context.Background(), stale); err == nil {
		t.Fatal("cleanup deleted a run with unresolved cancelled-start recovery")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("unresolved cancelled-start journal was deleted: %v", err)
	}
}

func TestFinishStartingPreservesNativeCleanupBarrier(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 1}
	active := &runningRun{starting: true, cleanupBlocked: true, nativeCloseRetryRequired: true}
	app := &daemon{running: map[state.RunKey]*runningRun{key: active}}

	app.finishStarting(key, true)

	if active.starting || !active.cleanupBlocked {
		t.Fatalf("finishStarting cleared a live native cleanup barrier: %#v", active)
	}
}

func TestFinishAttachedStartClearsCleanupBarrierWithoutExecutionOwner(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 1}
	active := &runningRun{starting: true, cancelled: true, cleanupBlocked: true}
	app := &daemon{running: map[state.RunKey]*runningRun{key: active}}

	if !app.finishAttachedStart(key) {
		t.Fatal("cancelled start did not request outbox wake")
	}
	if active.starting || active.cleanupBlocked {
		t.Fatalf("ownerless cancelled start retained cleanup barrier: %#v", active)
	}
}

func assertNativeOutputFailureTerminal(t *testing.T, store *state.Store, key state.RunKey, commandID, outcome string) {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 {
		t.Fatalf("native output failure terminal = %#v", journal)
	}
	var payload map[string]any
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	errorText, _ := payload["error"].(string)
	if payload["reason"] != string(protocol.TaskResultReasonProcessFailure) || payload["output_truncated"] != true || !strings.Contains(errorText, "output was truncated") {
		t.Fatalf("native output failure payload = %#v", payload)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != commandID || journal.PendingCommandAcknowledgements[0].Outcome != outcome {
		t.Fatalf("native output failure acknowledgement = %#v", journal.PendingCommandAcknowledgements)
	}
}

func saveClaimedNativeSettlementRun(t *testing.T, store *state.Store, key state.RunKey, work protocol.Work) protocol.ClaimResponse {
	t.Helper()
	claim := protocol.ClaimResponse{
		RunID: key.RunID, TaskID: "task-1", Generation: key.Generation, ClaimID: "claim-1",
		LeaseToken: "lease-1", LeaseExpiresAt: time.Now().Add(time.Minute), Work: work,
	}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: claim.ClaimID,
		LocalState: "claiming", Work: work, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, claim); err != nil {
		t.Fatal(err)
	}
	return claim
}

func saveLaunchingGoalSession(t *testing.T, store *state.Store, key state.RunKey, admission protocol.Admission, sessionKey state.GoalSessionKey) {
	t.Helper()
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-1", RunID: key.RunID, Generation: key.Generation, AdmissionID: admission.AdmissionID,
		LocalHandleID: sessionKey.LocalHandleID, RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: "codex", HarnessVersion: "0.153.4",
		AdapterVersion: "symmetry-daemon:test", AdapterProtocolVersion: 1,
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SessionMode: state.GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
		t.Fatal(err)
	}
}

func containsNativeSettlementCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

type fixedNativeSessionAdapter struct {
	capabilities harness.Capabilities
	session      *blockingStartTurnNativeSession
}

func (adapter *fixedNativeSessionAdapter) Probe(context.Context) (harness.Capabilities, error) {
	return adapter.capabilities, nil
}

func (adapter *fixedNativeSessionAdapter) Start(_ context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	adapter.session.recordCall("start")
	adapter.session.request = request
	adapter.session.sink = sink
	pid, identity := adapter.session.ProcessDetails()
	if request.PersistProcess != nil {
		if err := request.PersistProcess(pid, identity); err != nil {
			return nil, err
		}
	}
	return adapter.session, nil
}

type blockingStartTurnNativeSession struct {
	*fakeNativeGoalSession
	emitUsage    bool
	emitTerminal bool
	turnObserved chan struct{}
	turnRelease  <-chan struct{}
	once         sync.Once
}

func (session *blockingStartTurnNativeSession) StartTurn(ctx context.Context, request harness.TurnRequest) error {
	session.recordCall("start_turn")
	session.turnRequest = request
	if session.emitUsage || session.emitTerminal {
		usagePayload := json.RawMessage(`{"total":{"cached_input_tokens":1,"input_tokens":13,"output_tokens":8,"reasoning_output_tokens":0,"total_tokens":21}}`)
		if err := session.sink.Handle(ctx, harness.Event{Kind: harness.EventUsageObserved, At: time.Now().UTC(), Payload: usagePayload}); err != nil {
			return err
		}
	}
	if session.emitTerminal {
		payload, err := json.Marshal(session.result.Semantic)
		if err != nil {
			return err
		}
		if err := session.sink.Handle(ctx, harness.Event{Kind: harness.EventTaskResult, At: time.Now().UTC(), Payload: payload}); err != nil {
			return err
		}
	}
	session.once.Do(func() { close(session.turnObserved) })
	<-session.turnRelease
	return nil
}

type blockingNativeSubjectWorkspace struct {
	*fakeWorkspace
	entered chan struct{}
	once    sync.Once
}

func (service *blockingNativeSubjectWorkspace) DeriveSubject(ctx context.Context, _ workspace.Prepared, _ string) (protocol.Subject, error) {
	service.once.Do(func() { close(service.entered) })
	<-ctx.Done()
	return protocol.Subject{}, ctx.Err()
}

type rejectedCancelNativeSession struct {
	*fakeNativeGoalSession
}

func (session *rejectedCancelNativeSession) Control(_ context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	session.recordCall("control:" + string(request.Kind))
	return harness.ControlReceipt{
		CommandID: request.CommandID,
		Kind:      request.Kind,
		Outcome:   harness.ControlRejected,
		Message:   "native turn already terminal",
	}, nil
}

func TestOrphanedNativeUsageRetryCannotReleaseCancellationOwner(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000077"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	session := &fakeNativeGoalSession{
		result:      harness.TaskResult{Kind: harness.ResultCancelled},
		turnStarted: make(chan struct{}),
	}
	orphanedResult := harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "orphaned retry"}
	orphanedUsage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000078",
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostUnknown,
		ObservedAt:    "2026-09-20T01:00:00Z",
	}
	active := &runningRun{
		claimed:                    true,
		nativeSession:              session,
		goalSession:                &sessionKey,
		goalAdmission:              &admission,
		nativeFinalResult:          &orphanedResult,
		nativeFinalUsage:           &orphanedUsage,
		nativeUsageRetryPending:    true,
		nativeUsageRetryAttempts:   1,
		nativeUsageRetryOwnerClaim: 0,
		cleanupBlocked:             true,
	}
	terminalEntered := make(chan struct{})
	releaseTerminal := make(chan struct{})
	var terminalEnteredOnce sync.Once
	terminalCalls := 0
	usageAttempts := 0
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			queueGoalUsage: func(got state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
				usageAttempts++
				return store.QueueGoalUsage(got, usage)
			},
			queueTerminalTransitionAndAcknowledgement: func(got state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, at time.Time) (state.RunJournal, error) {
				terminalCalls++
				terminalEnteredOnce.Do(func() { close(terminalEntered) })
				<-releaseTerminal
				return store.QueueTerminalTransitionAndAcknowledgementAt(got, transition, acknowledgement, at)
			},
		},
	}

	app.flushPendingNativeUsage(context.Background())
	if usageAttempts != 0 || active.nativeUsageRetryInFlight {
		t.Fatalf("ownerless usage retry started: attempts=%d active=%#v", usageAttempts, active)
	}

	cancelDone := make(chan bool, 1)
	go func() {
		cancelDone <- app.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-owner", Kind: "cancel"})
	}()
	awaitNativeSettlementSignal(t, terminalEntered, "cancellation did not reach terminal persistence")
	app.mu.Lock()
	cancelOwnerClaim := active.nativeTerminalOwnerClaim
	ownerBeforeStaleRetry := active.nativeTerminalOwner
	terminalizingBeforeStaleRetry := active.terminalizing
	app.mu.Unlock()
	if cancelOwnerClaim == 0 || !ownerBeforeStaleRetry || terminalizingBeforeStaleRetry == 0 {
		t.Fatalf("cancellation did not hold a distinct terminal claim: %#v", active)
	}

	app.completeNativeRunAfterUsageOwned(context.Background(), key, active, 0, orphanedResult, nil, nil, true)
	app.mu.Lock()
	ownerAfterStaleRetry := active.nativeTerminalOwner
	claimAfterStaleRetry := active.nativeTerminalOwnerClaim
	terminalizingAfterStaleRetry := active.terminalizing
	app.mu.Unlock()
	if !ownerAfterStaleRetry || claimAfterStaleRetry != cancelOwnerClaim || terminalizingAfterStaleRetry != terminalizingBeforeStaleRetry {
		t.Fatalf("stale usage retry released cancellation owner: %#v", active)
	}

	close(releaseTerminal)
	select {
	case acknowledged := <-cancelDone:
		if !acknowledged {
			t.Fatal("cancellation owner did not publish its acknowledgement")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation settlement did not finish")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if terminalCalls != 1 || len(journal.PendingTransitions) != 1 || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != "cancel-owner" {
		t.Fatalf("cancellation settlement duplicated or lost terminal evidence: calls=%d journal=%#v", terminalCalls, journal)
	}
	if usageAttempts != 1 {
		t.Fatalf("usage attempts = %d, want only the cancellation owner's accounting", usageAttempts)
	}
	if active.nativeTerminalOwner || active.nativeTerminalOwnerClaim != 0 || active.terminalizing != 0 || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.cleanupBlocked {
		t.Fatalf("cancellation settlement retained ownership or cleanup barriers: %#v", active)
	}
}

func TestPersistentTerminalJournalFailureAfterUsageRetryDoesNotBlockOtherRunOutbox(t *testing.T) {
	store, blockedKey := claimedGoalDeliveryStore(t)
	otherKey := state.RunKey{RunID: "00000000-0000-4000-8000-000000000002", Generation: 1}
	saveClaimedGoalRun(t, store, otherKey)
	if _, err := store.QueueTransition(otherKey, protocol.StateTransitionRequest{TransitionID: "other-running", State: "running", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000010",
		RunID:         blockedKey.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostReported,
		CostMicrousd:  stringPointer("31"),
		ObservedAt:    "2026-09-20T01:00:00Z",
	}
	result := harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "usage retry completed"}
	active := &runningRun{
		nativeFinalResult:        &result,
		nativeFinalUsage:         &usage,
		nativeUsageRetryPending:  true,
		nativeUsageRetryAttempts: 1,
		nativeTerminalOwner:      true,
		terminalizing:            1,
		cleanupBlocked:           true,
	}
	terminalAttempted := make(chan struct{})
	var terminalAttemptOnce sync.Once
	terminalWritesBlocked := true
	otherDelivered := make(chan struct{})
	controlClient := &outboxIsolationControl{
		goalDeliveryControl: &goalDeliveryControl{fakeControl: &fakeControl{}},
		otherRunID:          otherKey.RunID,
		otherDelivered:      otherDelivered,
	}
	ctx, cancel := context.WithCancel(context.Background())
	flushDone := make(chan struct{})
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		control: controlClient,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{blockedKey: active},
		options: options{
			newID:    ids(),
			clock:    time.Now,
			newTimer: blockedTimerFactory,
			queueTerminalTransition: func(runKey state.RunKey, transition protocol.StateTransitionRequest, at time.Time) (state.RunJournal, error) {
				terminalAttemptOnce.Do(func() { close(terminalAttempted) })
				if terminalWritesBlocked {
					return state.RunJournal{}, errors.New("persistent terminal journal failure")
				}
				return store.QueueTerminalTransitionAt(runKey, transition, at)
			},
		},
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-flushDone:
		case <-time.After(time.Second):
		}
		app.backgroundWG.Wait()
		_ = store.Close()
	})

	go func() {
		app.flushAll(ctx)
		close(flushDone)
	}()

	awaitNativeSettlementSignal(t, terminalAttempted, "usage retry did not reach terminal journal persistence")
	journal, err := store.LoadJournal(blockedKey)
	if err != nil {
		t.Fatal(err)
	}
	usagePersisted := false
	for _, delivery := range append(append([]state.GoalDelivery(nil), journal.PendingGoalDeliveries...), journal.DeliveredGoalDeliveries...) {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			usagePersisted = true
			break
		}
	}
	if !usagePersisted {
		t.Fatalf("usage retry did not persist usage before terminal failure: %#v", journal)
	}
	awaitNativeSettlementSignal(t, otherDelivered, "persistent terminal journal failure blocked an unrelated run outbox")

	cancel()
	awaitNativeSettlementSignal(t, flushDone, "outbox flush did not stop after cancellation")
	app.backgroundWG.Wait()
	firstJournal, err := store.LoadJournal(blockedKey)
	if err != nil {
		t.Fatal(err)
	}
	firstUsageDigest := ""
	for _, delivery := range append(append([]state.GoalDelivery(nil), firstJournal.PendingGoalDeliveries...), firstJournal.DeliveredGoalDeliveries...) {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			firstUsageDigest = delivery.PayloadDigest
			break
		}
	}
	if firstUsageDigest == "" {
		t.Fatalf("first terminal failure lost exact usage body: %#v", firstJournal)
	}
	if !firstJournal.NativeUsageRecoveryRequired || firstJournal.NativeUsageTerminalRecovery == nil {
		t.Fatalf("first terminal failure lost exact recovery barrier: %#v", firstJournal)
	}

	terminalWritesBlocked = false
	app.flushPendingNativeUsage(context.Background())
	app.backgroundWG.Wait()
	finalJournal, err := store.LoadJournal(blockedKey)
	if err != nil {
		t.Fatal(err)
	}
	finalUsageDigest := ""
	for _, delivery := range append(append([]state.GoalDelivery(nil), finalJournal.PendingGoalDeliveries...), finalJournal.DeliveredGoalDeliveries...) {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			finalUsageDigest = delivery.PayloadDigest
			break
		}
	}
	if finalUsageDigest != firstUsageDigest || len(finalJournal.PendingTransitions) != 1 || !isTerminalTransition(finalJournal.PendingTransitions[0].State) {
		t.Fatalf("terminal retry changed usage or duplicated terminal: first=%q final=%q journal=%#v", firstUsageDigest, finalUsageDigest, finalJournal)
	}
	if finalJournal.NativeUsageRecoveryRequired || finalJournal.NativeUsageTerminalRecovery != nil {
		t.Fatalf("successful terminal retry retained recovery barrier: %#v", finalJournal)
	}
	if active.nativeTerminalOwner || active.nativeTerminalOwnerClaim != 0 || active.terminalizing != 0 || active.nativeUsageRetryInFlight || active.cleanupBlocked || !active.nativeUsageFinalized {
		t.Fatalf("terminal retry did not release the native owner: %#v", active)
	}
}

func TestFlushRunDefersNativeUsageRecoveryToLiveTerminalOwner(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000079",
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostReported,
		CostMicrousd:  stringPointer("41"),
		ObservedAt:    "2026-09-20T02:00:00Z",
	}
	if _, err := store.QueueNativeUsageRecovery(key, usage); err != nil {
		t.Fatal(err)
	}
	result := harness.TaskResult{Kind: harness.ResultFailed, Summary: "exact live terminal result"}
	active := &runningRun{
		nativeTerminalOwner:         true,
		nativeTerminalOwnerClaim:    1,
		nativeTerminalOwnerSequence: 1,
		terminalizing:               1,
		nativeFinalResult:           &result,
		nativeFinalUsage:            &usage,
		nativeUsageRetryPending:     true,
		nativeUsageRetryAttempts:    1,
		nativeUsageRetryOwnerClaim:  1,
		cleanupBlocked:              true,
	}
	terminalEntered := make(chan struct{})
	releaseTerminal := make(chan struct{})
	var terminalEnteredOnce sync.Once
	var terminalCallsMu sync.Mutex
	terminalCalls := 0
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clock: time.Now,
			queueTerminalTransition: func(got state.RunKey, transition protocol.StateTransitionRequest, at time.Time) (state.RunJournal, error) {
				terminalCallsMu.Lock()
				terminalCalls++
				terminalCallsMu.Unlock()
				terminalEnteredOnce.Do(func() { close(terminalEntered) })
				<-releaseTerminal
				return store.QueueTerminalTransitionAt(got, transition, at)
			},
		},
	}

	flushAllDone := make(chan struct{})
	go func() {
		app.flushAll(context.Background())
		close(flushAllDone)
	}()
	awaitNativeSettlementSignal(t, terminalEntered, "live usage retry did not reach terminal persistence")
	select {
	case <-flushAllDone:
	case <-time.After(2 * time.Second):
		t.Fatal("flushAll blocked behind live terminal publication")
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	flushRunDone := make(chan error, 1)
	go func() { flushRunDone <- app.flushRun(context.Background(), journal) }()
	select {
	case err := <-flushRunDone:
		if err != nil {
			t.Fatalf("flushRun while live usage owner active error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("flushRun raced the live usage owner")
	}
	terminalCallsMu.Lock()
	callsBeforeRelease := terminalCalls
	terminalCallsMu.Unlock()
	if callsBeforeRelease != 1 || durableTerminalPresent(journal) {
		t.Fatalf("generic recovery competed with live terminal owner: calls=%d journal=%#v", callsBeforeRelease, journal)
	}

	close(releaseTerminal)
	app.backgroundWG.Wait()
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	terminalCallsMu.Lock()
	finalTerminalCalls := terminalCalls
	terminalCallsMu.Unlock()
	if finalTerminalCalls != 1 || len(journal.PendingTransitions) != 1 || !strings.Contains(string(journal.PendingTransitions[0].Payload), result.Summary) {
		t.Fatalf("live terminal did not remain authoritative: calls=%d journal=%#v", finalTerminalCalls, journal)
	}
	if journal.NativeUsageRecoveryRequired {
		t.Fatalf("live terminal left native usage recovery pending: %#v", journal)
	}
	if active.nativeTerminalOwner || active.nativeTerminalOwnerClaim != 0 || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.cleanupBlocked || !active.nativeUsageFinalized {
		t.Fatalf("live usage settlement retained ownership or cleanup barriers: %#v", active)
	}
}

func TestCleanupDoesNotReleaseSlotWhileNativeStartOwnsPublication(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 1}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	app := &daemon{
		running: map[state.RunKey]*runningRun{key: {
			nativeStartInFlight: true,
			cleanupBlocked:      true,
			slotHeld:            true,
		}},
		slots: slots,
	}

	if app.releaseCleanupIfReady(key) {
		t.Fatal("cleanup became ready while native start still owned publication")
	}
	if len(slots) != 1 {
		t.Fatalf("available slots = %d, want zero while native start is in flight", 1-len(slots))
	}
	active := app.runningRun(key)
	if active == nil || !active.slotHeld {
		t.Fatalf("native start lost its capacity owner: %#v", active)
	}
}

type blockingOpenNativeAdapter struct {
	capabilities harness.Capabilities
	session      *blockingOpenNativeSession
}

func (adapter *blockingOpenNativeAdapter) Probe(context.Context) (harness.Capabilities, error) {
	return adapter.capabilities, nil
}

func (adapter *blockingOpenNativeAdapter) Start(_ context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	adapter.session.recordCall("start")
	adapter.session.request = request
	adapter.session.sink = sink
	pid, identity := adapter.session.ProcessDetails()
	if request.PersistProcess != nil {
		if err := request.PersistProcess(pid, identity); err != nil {
			return nil, err
		}
	}
	return adapter.session, nil
}

type blockingOpenNativeSession struct {
	*fakeNativeGoalSession
	entered            chan struct{}
	release            <-chan struct{}
	openErr            error
	ignoreCancellation bool
	once               sync.Once
}

func (session *blockingOpenNativeSession) Open(ctx context.Context) (harness.NativeSessionHandle, error) {
	session.recordCall("open")
	session.once.Do(func() { close(session.entered) })
	if session.ignoreCancellation {
		<-session.release
		return session.handle, session.openErr
	}
	select {
	case <-ctx.Done():
		return harness.NativeSessionHandle{}, ctx.Err()
	case <-session.release:
		return session.handle, session.openErr
	}
}

type blockingNativeAdmissionControl struct {
	*nativeAdmissionControl
	stage   string
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (client *blockingNativeAdmissionControl) AttachHarnessSession(ctx context.Context, runID string, request control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	if client.stage == "attach" {
		if err := client.block(ctx); err != nil {
			return control.GoalSessionReceipt{}, err
		}
	}
	return client.nativeAdmissionControl.AttachHarnessSession(ctx, runID, request)
}

func (client *blockingNativeAdmissionControl) FetchRunContext(ctx context.Context, runID string, fence protocol.Fence) (control.GoalRunContext, error) {
	if client.stage == "context" {
		if err := client.block(ctx); err != nil {
			return control.GoalRunContext{}, err
		}
	}
	return client.nativeAdmissionControl.FetchRunContext(ctx, runID, fence)
}

func (client *blockingNativeAdmissionControl) block(ctx context.Context) error {
	client.once.Do(func() { close(client.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-client.release:
		return nil
	}
}

type sharedTerminalWaitSession struct {
	*fakeNativeGoalSession
	terminalObserved chan struct{}
	waitTurnEntered  chan struct{}
	releaseWaitTurn  chan struct{}
	observeOnce      sync.Once
}

func (session *sharedTerminalWaitSession) WaitTurn(ctx context.Context) error {
	session.recordCall("wait_turn")
	session.observeOnce.Do(func() { close(session.terminalObserved) })
	session.waitTurnEntered <- struct{}{}
	select {
	case <-session.releaseWaitTurn:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *sharedTerminalWaitSession) Control(_ context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	session.recordCall("control:" + string(request.Kind))
	select {
	case <-session.terminalObserved:
		return harness.ControlReceipt{
			CommandID: request.CommandID,
			Kind:      request.Kind,
			Outcome:   harness.ControlRejected,
			Message:   "native turn already terminal",
		}, nil
	default:
		return harness.ControlReceipt{}, errors.New("cancel reached adapter before terminal observation")
	}
}

type outboxIsolationControl struct {
	*goalDeliveryControl
	otherRunID     string
	otherDelivered chan struct{}
	once           sync.Once
}

func (client *outboxIsolationControl) Transition(ctx context.Context, runID string, request protocol.StateTransitionRequest) error {
	if runID == client.otherRunID {
		client.once.Do(func() { close(client.otherDelivered) })
	}
	return client.goalDeliveryControl.Transition(ctx, runID, request)
}

func awaitNativeSettlementSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatal(failure)
	}
}

func awaitNativeSettlementWorkers(t *testing.T, app *daemon) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		app.workers.Wait()
		close(done)
	}()
	awaitNativeSettlementSignal(t, done, "native settlement workers did not finish")
}
