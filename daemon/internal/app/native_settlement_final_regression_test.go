package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestNativeUsageRecoveryRestartPreservesObservedSettlement(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	semantic := validNativeTaskResult(t, admission)
	for _, test := range []struct {
		name        string
		result      harness.TaskResult
		commandID   string
		outcome     string
		wantState   string
		wantPayload string
	}{
		{
			name:      "completed result",
			result:    harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "completed before usage retry", Semantic: &semantic},
			wantState: "completed",
		},
		{
			name: "output failure with rejected late cancel",
			result: harness.TaskResult{
				Kind: harness.ResultSucceeded, Summary: "terminal output was incomplete", Semantic: &semantic,
				Process: execution.Result{OutputTruncated: true},
			},
			commandID:   "cancel-after-terminal",
			outcome:     "rejected",
			wantState:   "failed",
			wantPayload: `"output_truncated":true`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			t.Cleanup(func() { _ = store.Close() })
			usage := protocol.Usage{
				SchemaVersion: protocol.UsageSchemaVersion,
				UsageID:       "00000000-0000-4000-8000-000000000010",
				RunID:         key.RunID,
				UsageKey:      nativeGoalUsageKey,
				Provider:      "openai",
				Model:         "gpt-test",
				CostBasis:     protocol.CostReported,
				CostMicrousd:  stringPointer("29"),
				InputTokens:   int64Pointer(17),
				OutputTokens:  int64Pointer(5),
				ObservedAt:    "2026-09-20T01:00:00Z",
			}
			settlement, err := nativeUsageTerminalRecovery(&test.result, &admission, nil, nil, test.commandID, test.commandID != "", test.outcome == "rejected", nil)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := store.QueueNativeUsageRecoverySettlement(key, usage, settlement)
			if err != nil {
				t.Fatal(err)
			}
			if len(journal.PendingGoalDeliveries) != 1 || journal.PendingGoalDeliveries[0].Usage == nil || journal.PendingGoalDeliveries[0].Usage.InputTokens == nil || *journal.PendingGoalDeliveries[0].Usage.InputTokens != 17 || journal.PendingGoalDeliveries[0].Usage.OutputTokens == nil || *journal.PendingGoalDeliveries[0].Usage.OutputTokens != 5 {
				t.Fatalf("native usage recovery counters = %#v", journal.PendingGoalDeliveries)
			}

			restarted := &daemon{
				config:  testConfig(t),
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: make(map[state.RunKey]*runningRun),
				options: options{newID: ids(), clock: time.Now},
			}
			recovered, err := restarted.queueNativeUsageRecoveryTerminal(context.Background(), journal, nil)
			if err != nil {
				t.Fatalf("queue recovery terminal = %v", err)
			}
			if recovered.TerminalState != test.wantState || len(recovered.PendingTransitions) != 1 {
				t.Fatalf("recovered terminal = %#v", recovered)
			}
			payload := string(recovered.PendingTransitions[0].Payload)
			if !strings.Contains(payload, semantic.ResultID) || (test.wantPayload != "" && !strings.Contains(payload, test.wantPayload)) {
				t.Fatalf("recovered payload = %s", payload)
			}
			if test.commandID == "" {
				if len(recovered.PendingCommandAcknowledgements) != 0 {
					t.Fatalf("unexpected recovery acknowledgement = %#v", recovered.PendingCommandAcknowledgements)
				}
			} else if len(recovered.PendingCommandAcknowledgements) != 1 || recovered.PendingCommandAcknowledgements[0].CommandID != test.commandID || recovered.PendingCommandAcknowledgements[0].Outcome != test.outcome {
				t.Fatalf("recovered acknowledgement = %#v", recovered.PendingCommandAcknowledgements)
			}
		})
	}
}

func TestNativeUsageRecoveryFreshStoreFlushesExactSettlement(t *testing.T) {
	root := t.TempDir()
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000010",
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostReported,
		CostMicrousd:  stringPointer("29"),
		InputTokens:   int64Pointer(17),
		OutputTokens:  int64Pointer(5),
		ObservedAt:    "2026-09-20T05:30:00Z",
	}
	result := harness.TaskResult{Kind: harness.ResultFailed, Summary: "output was incomplete", Process: execution.Result{OutputTruncated: true}}
	settlement, err := nativeUsageTerminalRecovery(&result, nil, nil, nil, "cancel-1", true, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueNativeUsageRecoverySettlement(key, usage, settlement); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restartedStore, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restartedStore.Close() })
	controlClient := &goalDeliveryControl{fakeControl: &fakeControl{}, usageErrors: []error{transportError("hold recovered usage for inspection")}}
	restarted := &daemon{
		config: testConfig(t), store: restartedStore, control: controlClient,
		log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now},
	}
	journal, err := restartedStore.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.flushRun(context.Background(), journal); err == nil {
		t.Fatal("fresh-store flush unexpectedly delivered the injected usage failure")
	}
	recovered, err := restartedStore.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.NativeUsageRecoveryRequired || recovered.NativeUsageTerminalRecovery != nil || recovered.TerminalState != settlement.State || len(recovered.PendingTransitions) != 1 {
		t.Fatalf("fresh-store exact recovery = %#v", recovered)
	}
	if !bytes.Equal(recovered.PendingTransitions[0].Payload, settlement.Payload) {
		t.Fatalf("fresh-store terminal payload = %s, want %s", recovered.PendingTransitions[0].Payload, settlement.Payload)
	}
	if len(recovered.PendingCommandAcknowledgements) != 1 || recovered.PendingCommandAcknowledgements[0].CommandID != settlement.CommandID || recovered.PendingCommandAcknowledgements[0].Outcome != settlement.CommandOutcome {
		t.Fatalf("fresh-store acknowledgement = %#v", recovered.PendingCommandAcknowledgements)
	}
	recoveredUsage, ok := nativeUsageRecoveryUsage(recovered)
	if !ok || !reflect.DeepEqual(recoveredUsage, usage) {
		t.Fatalf("fresh-store usage = %#v, want %#v", recoveredUsage, usage)
	}
}

func TestNativeUsageRetryExhaustionPreservesDurableStopWitness(t *testing.T) {
	for _, mode := range []string{"pending", "projection-first", "available"} {
		t.Run(mode, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			t.Cleanup(func() { _ = store.Close() })
			sessionKey, controlSessionID, bindingID := saveRetainedGoalSession(t, store, key)
			if mode != "projection-first" {
				if _, err := store.MarkGoalSessionStoppedPending(sessionKey); err != nil {
					t.Fatal(err)
				}
			}
			queued, err := store.QueueGoalSessionStopped(key, state.GoalSessionStoppedDelivery{SessionID: controlSessionID, LocalHandleID: sessionKey.LocalHandleID, BindingID: bindingID})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "available" {
				if _, err := store.MarkGoalSessionAvailable(sessionKey, state.GoalSessionStopCertificate{
					RunID: key.RunID, Generation: key.Generation, SessionID: controlSessionID, LocalHandleID: sessionKey.LocalHandleID,
					BindingID: bindingID, DeliveryDigest: queued.PendingGoalDeliveries[0].PayloadDigest, ReceiptID: "00000000-0000-4000-8000-000000000011",
				}); err != nil {
					t.Fatal(err)
				}
			}
			usage := protocol.Usage{
				SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000012",
				RunID: key.RunID, UsageKey: nativeGoalUsageKey, Provider: "openai", Model: "gpt-test",
				CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-20T02:00:00Z",
			}
			result := harness.TaskResult{Kind: harness.ResultFailed, Summary: "usage recovery exhausted"}
			active := &runningRun{
				goalSession:                 &sessionKey,
				nativeFinalResult:           &result,
				nativeFinalUsage:            &usage,
				nativeTerminalOwner:         true,
				nativeTerminalOwnerClaim:    1,
				nativeTerminalOwnerSequence: 1,
				nativeUsageRetryOwnerClaim:  1,
				terminalizing:               1,
				cleanupBlocked:              true,
			}
			app := &daemon{
				config: testConfig(t), store: store, running: map[state.RunKey]*runningRun{key: active},
				log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids(), clock: time.Now},
			}
			app.exhaustNativeUsageRetry(context.Background(), key, active, errors.New("usage retries exhausted"))

			session, err := store.LoadGoalSession(sessionKey)
			if err != nil {
				t.Fatal(err)
			}
			wantState := state.GoalSessionStateUnavailable
			if mode == "available" {
				wantState = state.GoalSessionStateAvailable
			}
			if session.SessionState != wantState || session.NeedsReconciliation() || session.NativeSessionID != "native-thread-1" || session.ControlSessionID != controlSessionID || session.BindingID != bindingID {
				t.Fatalf("usage exhaustion changed stopped session = %#v", session)
			}
			if mode == "available" && session.StopCertificate == nil {
				t.Fatal("usage exhaustion erased the durable stop certificate")
			}
		})
	}
}

func TestNativeUsageRecoveryPersistenceUsesCappedBackoff(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	result := harness.TaskResult{Kind: harness.ResultFailed, Summary: "recovery persistence failed"}
	active := &runningRun{
		nativeFinalResult:           &result,
		nativeTerminalOwner:         true,
		nativeTerminalOwnerClaim:    1,
		nativeTerminalOwnerSequence: 1,
		nativeUsageRetryOwnerClaim:  1,
		nativeUsageRetryAttempts:    nativeUsageRetryLimit,
		terminalizing:               1,
		cleanupBlocked:              true,
	}
	app := &daemon{
		config: testConfig(t), store: store, running: map[state.RunKey]*runningRun{key: active},
		log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids(), clock: func() time.Time { return now }},
	}

	app.retryNativeUsageRecoveryPersistence(key, active, 1, errors.New("first persistence failure"))
	firstRetryAt := active.nativeUsageRetryAt
	firstDeadline := active.nativeUsageRetryDeadline
	if got := firstRetryAt.Sub(now); got != time.Second {
		t.Fatalf("first recovery retry delay = %s, want 1s", got)
	}
	if active.nativeUsageRetryAttempts != nativeUsageRetryLimit+1 {
		t.Fatalf("first recovery attempts = %d", active.nativeUsageRetryAttempts)
	}

	now = now.Add(time.Second)
	app.retryNativeUsageRecoveryPersistence(key, active, 1, errors.New("second persistence failure"))
	if got := active.nativeUsageRetryAt.Sub(now); got != 2*time.Second {
		t.Fatalf("second recovery retry delay = %s, want 2s", got)
	}
	if active.nativeUsageRetryAttempts != nativeUsageRetryLimit+2 {
		t.Fatalf("second recovery attempts = %d", active.nativeUsageRetryAttempts)
	}
	if !active.nativeUsageRetryDeadline.Equal(firstDeadline) {
		t.Fatalf("recovery deadline was extended: first=%s second=%s", firstDeadline, active.nativeUsageRetryDeadline)
	}

	active.nativeUsageRetryAttempts = nativeUsageRetryLimit + 100
	now = now.Add(2 * time.Second)
	app.retryNativeUsageRecoveryPersistence(key, active, 1, errors.New("later persistence failure"))
	if got := active.nativeUsageRetryAt.Sub(now); got != retryMaximum {
		t.Fatalf("capped recovery retry delay = %s, want %s", got, retryMaximum)
	}
}

func TestStartTurnObservedTerminalWinsCancellation(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	semantic := validNativeTaskResult(t, admission)
	app, store, baseSession, controlClient := nativeAdmissionDaemon(t, admission, semantic, nil)
	t.Cleanup(func() { _ = store.Close() })
	app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
	eventUnknownWrite := false
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		if !eventUnknownWrite && bytes.Contains(data, []byte(`"kind":"task_result"`)) {
			eventUnknownWrite = true
			return errors.New("injected post-write unknown task_result event")
		}
		return nil
	})
	defer restoreWriter()
	turnObserved := make(chan struct{})
	turnRelease := make(chan struct{})
	session := &blockingStartTurnNativeSession{
		fakeNativeGoalSession: baseSession,
		emitUsage:             true,
		emitTerminal:          true,
		turnObserved:          turnObserved,
		turnRelease:           turnRelease,
	}
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, &fixedNativeSessionAdapter{capabilities: verifiedCodexCapabilities(), session: session}); err != nil {
		t.Fatal(err)
	}
	app.harnessRegistry = registry
	key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000100", Generation: 1}
	app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
	awaitNativeSettlementSignal(t, turnObserved, "native StartTurn did not publish its terminal")
	commandID := "cancel-after-start-turn-terminal"
	if app.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: commandID, Kind: "cancel"}) {
		t.Fatal("cancellation was acknowledged before the start owner settled")
	}
	close(turnRelease)
	awaitNativeSettlementWorkers(t, app)
	if !eventUnknownWrite {
		t.Fatal("task_result event did not exercise post-write unknown readback")
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	terminalTransitions := 0
	for _, transition := range journal.PendingTransitions {
		if isTerminalTransition(transition.State) {
			terminalTransitions++
		}
	}
	if journal.TerminalState != "completed" || terminalTransitions != 1 || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("observed terminal was replaced by cancellation = %#v", journal)
	}
	if acknowledgement := journal.PendingCommandAcknowledgements[0]; acknowledgement.CommandID != commandID || acknowledgement.Outcome != "rejected" {
		t.Fatalf("late cancellation acknowledgement = %#v", acknowledgement)
	}
}

func TestTaskResultUnknownWriteReadbackPrecedesEventRetirement(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	active := &runningRun{nativeStartInFlight: true}
	controlClient := &eventDeliveryInterleavingControl{fakeControl: &fakeControl{}, appended: make(chan struct{})}
	app := &daemon{
		config: testConfig(t), store: store, control: controlClient,
		log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: time.Now},
	}
	deliveryDone := make(chan error, 1)
	injected := false
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		if injected {
			return nil
		}
		var journal state.RunJournal
		if err := json.Unmarshal(data, &journal); err != nil || len(journal.PendingEvents) == 0 || journal.PendingEvents[len(journal.PendingEvents)-1].Kind != string(harness.EventTaskResult) {
			return nil
		}
		injected = true
		event := journal.PendingEvents[len(journal.PendingEvents)-1]
		go func() {
			_, err := app.deliverEvents(context.Background(), journal, []protocol.RunEvent{event})
			deliveryDone <- err
		}()
		awaitNativeSettlementSignal(t, controlClient.appended, "task_result delivery did not reach retirement barrier")
		return errors.New("injected post-write unknown task_result event")
	})
	defer restoreWriter()

	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventTaskResult, At: time.Now().UTC(), Payload: json.RawMessage(`{"kind":"failed"}`)}); err != nil {
		t.Fatalf("queue task_result after unknown write = %v", err)
	}
	if !active.nativeStartTerminalObserved {
		t.Fatal("post-write task_result was not recorded as terminal observation")
	}
	select {
	case err := <-deliveryDone:
		if err != nil {
			t.Fatalf("retire delivered task_result = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivered task_result did not retire after readback")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 0 {
		t.Fatalf("delivered task_result remained pending: %#v", journal.PendingEvents)
	}
}

func TestUsageObservationPostWriteUnknownUsesMonotonicReadback(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	sessionKey := state.GoalSessionKey{GoalID: "goal-1", LocalHandleID: "local-1"}
	active := &runningRun{goalSession: &sessionKey}
	app := &daemon{
		config: testConfig(t), store: store,
		log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: time.Now},
	}
	injected := false
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		if !injected {
			injected = true
			return errors.New("injected post-write unknown usage observation")
		}
		return nil
	})
	defer restoreWriter()

	payload := json.RawMessage(`{"total":{"input_tokens":11,"output_tokens":7,"cached_input_tokens":3,"reasoning_output_tokens":2,"total_tokens":23}}`)
	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventUsageObserved, At: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC), Payload: payload}); err != nil {
		t.Fatalf("queue usage observation after unknown write = %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.NativeUsageObservation == nil || journal.NativeUsageObservation.TotalTokens != 23 || active.goalUsage == nil || active.goalUsage.InputTokens != 11 || active.goalUsage.OutputTokens != 7 {
		t.Fatalf("post-write usage observation was not retained: journal=%#v active=%#v", journal.NativeUsageObservation, active)
	}
}

func TestOpenSuccessAfterCancellationOrLeaseKeepsStartOwnershipThroughClose(t *testing.T) {
	for _, mode := range []string{"cancel", "lease"} {
		t.Run(mode, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			app, store, _, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
			t.Cleanup(func() { _ = store.Close() })
			app.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
			entered := make(chan struct{})
			release := make(chan struct{})
			session := &blockingOpenNativeSession{
				fakeNativeGoalSession: &fakeNativeGoalSession{
					handle: harness.NativeSessionHandle{ID: "native-thread-1"}, result: harness.TaskResult{Kind: harness.ResultCancelled},
					turnStarted: make(chan struct{}), processPID: 71, processIdentity: "native:71",
				},
				entered: entered, release: release, ignoreCancellation: true,
			}
			registry := harness.NewRegistry()
			if err := registry.Register(harness.KindCodex, &blockingOpenNativeAdapter{capabilities: verifiedCodexCapabilities(), session: session}); err != nil {
				t.Fatal(err)
			}
			app.harnessRegistry = registry
			key := state.RunKey{RunID: "run-1", Generation: 1}
			app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
			awaitNativeSettlementSignal(t, entered, "native Open did not block")
			active := app.runningRun(key)
			if active == nil || !active.nativeStartInFlight || !active.slotHeld {
				t.Fatalf("native Open did not retain start ownership = %#v", active)
			}
			if mode == "cancel" {
				if app.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-open", Kind: "cancel"}) {
					t.Fatal("Open cancellation was acknowledged before compensation")
				}
			} else {
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				app.terminateForLease(journal, "lease expired during Open")
				app.flushCleanups(context.Background())
				if !active.slotHeld || len(app.slots) != 1 {
					t.Fatalf("stale cleanup released capacity before Open compensation: active=%#v slots=%d", active, len(app.slots))
				}
			}
			close(release)
			awaitNativeSettlementWorkers(t, app)
			calls := session.callsSnapshot()
			if !containsNativeSettlementCall(calls, "close") || containsNativeSettlementCall(calls, "start_turn") {
				t.Fatalf("Open %s compensation calls = %#v", mode, calls)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.HasProcessDetails() || active.nativeStartInFlight || active.nativeTerminalOwner || active.terminalizing != 0 || active.nativeSession != nil || active.cleanupBlocked {
				t.Fatalf("Open %s compensation retained execution ownership: active=%#v journal=%#v", mode, active, journal)
			}
			if mode == "cancel" {
				if journal.LocalState != "terminal_pending" || journal.TerminalState != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != "cancel-open" || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
					t.Fatalf("Open cancellation settlement = %#v", journal)
				}
			} else if journal.LocalState != "stale" || durableTerminalPresent(journal) || len(journal.PendingCommandAcknowledgements) != 0 {
				t.Fatalf("Open lease settlement = %#v", journal)
			}
		})
	}
}

func TestCancelledNativeStartFinalWaitStopsOnDaemonShutdown(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	t.Cleanup(func() { _ = store.Close() })
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	finalEntered := make(chan struct{})
	session := &shutdownFinalWaitSession{
		fakeNativeGoalSession: &fakeNativeGoalSession{result: harness.TaskResult{Kind: harness.ResultCancelled}, processPID: 71, processIdentity: "native:71"},
		finalEntered:          finalEntered,
	}
	active := &runningRun{
		starting: true, claimed: true, nativeStartInFlight: true, nativeSession: session, goalSession: &sessionKey,
		goalAdmission: &admission, cancelled: true, cancelCommandID: "cancel-shutdown", cleanupBlocked: true,
	}
	background, shutdown := context.WithCancel(context.Background())
	app := &daemon{
		config: testConfig(t), store: store, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), background: background,
		running: map[state.RunKey]*runningRun{key: active}, options: options{newID: ids(), clock: time.Now},
	}
	done := make(chan struct{})
	go func() {
		_, _ = app.settleCancelledNativeStart(context.Background(), key, sessionKey, sessionKey.LocalHandleID, session, false, false)
		close(done)
	}()
	awaitNativeSettlementSignal(t, finalEntered, "cancelled-start final Wait did not begin")
	shutdown()
	awaitNativeSettlementSignal(t, done, "cancelled-start final Wait ignored daemon shutdown")
	if !session.finalCancelled() {
		t.Fatal("cancelled-start final Wait did not observe daemon shutdown")
	}
}

type shutdownFinalWaitSession struct {
	*fakeNativeGoalSession
	mu           sync.Mutex
	waits        int
	finalEntered chan struct{}
	cancelled    bool
}

type eventDeliveryInterleavingControl struct {
	*fakeControl
	once     sync.Once
	appended chan struct{}
}

func (control *eventDeliveryInterleavingControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	control.once.Do(func() { close(control.appended) })
	return nil
}

func (session *shutdownFinalWaitSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	session.mu.Lock()
	session.waits++
	waits := session.waits
	session.mu.Unlock()
	if waits == 1 {
		return session.result, nil
	}
	close(session.finalEntered)
	<-ctx.Done()
	session.mu.Lock()
	session.cancelled = true
	session.mu.Unlock()
	return harness.TaskResult{Kind: harness.ResultCancelled}, ctx.Err()
}

func (session *shutdownFinalWaitSession) finalCancelled() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.cancelled
}

func int64Pointer(value int64) *int64 {
	return &value
}
