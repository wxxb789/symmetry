package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestNativeCloseFailureRetainsProcessEvidenceAndBlocksCleanup(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("native child stop failed")
	session := &closeFailureGoalSession{err: failure}
	active := &runningRun{nativeSession: session, goalSession: &sessionKey}
	daemon := &daemon{store: store, options: options{clock: time.Now}, running: map[state.RunKey]*runningRun{key: active}}

	if err := daemon.closeNativeGoalSession(key, active, session); !errors.Is(err, failure) {
		t.Fatalf("closeNativeGoalSession() error = %v, want %v", err, failure)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.PID != 71 || journal.ProcessIdentity != "native:71" || !active.cleanupBlocked {
		t.Fatalf("close failure evidence = journal:%#v active:%#v", journal, active)
	}
	sessionJournal, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !sessionJournal.IsUncertainLaunch() || sessionJournal.SessionState != state.GoalSessionStateUnavailable {
		t.Fatalf("close failure Goal session = %#v", sessionJournal)
	}

	terminal, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	terminal, err = store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.cleanupPending(context.Background(), terminal); err == nil {
		t.Fatal("cleanupPending() deleted unresolved native recovery evidence")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("run journal was deleted after unresolved native close: %v", err)
	}
}

func TestNativeCloseFailurePropagatesRetentionPersistenceError(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	failure := errors.New("native child stop failed")
	retentionFailure := errors.New("retention persistence failed")
	active := &runningRun{goalSession: &sessionKey}
	daemon := &daemon{
		store: store, running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: time.Now, retainWorkspace: func(state.RunKey) (state.RunJournal, error) { return state.RunJournal{}, retentionFailure }},
	}
	err = daemon.closeNativeGoalSession(key, active, &closeFailureGoalSession{err: failure})
	if !errors.Is(err, failure) || !errors.Is(err, retentionFailure) {
		t.Fatalf("close failure errors = %v, want native and retention errors", err)
	}
	if !active.cleanupBlocked || !daemon.workspaceRetentionRemembered(key) {
		t.Fatalf("close failure did not retain in-memory barrier: active=%#v", active)
	}
}

func TestNativeCloseFailureStaleRunDoesNotReleaseActiveRecoveryBarrier(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	active := &runningRun{nativeSession: &closeFailureGoalSession{err: errors.New("native child stop failed"), waitResult: harness.TaskResult{Kind: harness.ResultSucceeded}}, goalSession: &sessionKey, goalAdmission: &admission, stale: true}
	daemon := &daemon{store: store, options: options{newID: ids(), clock: time.Now}, running: map[state.RunKey]*runningRun{key: active}}

	daemon.waitForNativeRun(context.Background(), key, active, active.nativeSession)
	if daemon.runningRun(key) != active || !active.cleanupBlocked {
		t.Fatalf("stale native close failure released recovery barrier: running=%#v active=%#v", daemon.runningRun(key), active)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "stale" || journal.PID != 71 || journal.ProcessIdentity != "native:71" {
		t.Fatalf("stale native close failure journal = %#v", journal)
	}
}

func TestRecoveryClosedGoalSessionWritesUnknownOutcomeForNonterminalRun(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.CloseGoalSession(sessionKey); err != nil {
		t.Fatal(err)
	}
	terminated := 0
	daemon := &daemon{
		store: store, running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
			terminated++
			if pid != 71 || identity != "native:71" {
				t.Fatalf("persisted process = %d %q", pid, identity)
			}
			return nil
		}},
	}
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := daemon.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terminated != 1 {
		t.Fatalf("persisted process termination count = %d, want 1", terminated)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionState != state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateClosed || session.NeedsReconciliation() {
		t.Fatalf("closed Goal session changed during recovery: %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 || !strings.Contains(string(journal.PendingTransitions[0].Payload), string(protocol.TaskResultReasonUnknownOutcome)) {
		t.Fatalf("closed-session recovery journal = %#v", journal)
	}
	if err := daemon.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || terminated != 1 {
		t.Fatalf("closed-session recovery was not idempotent: transitions=%d terminations=%d", len(journal.PendingTransitions), terminated)
	}
}

func TestNativeGoalUsageProducerQueuesNormalizedUnknownCost(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	j.GoalDeliveryEnabled = true
	if err := store.SaveJournal(j); err != nil {
		t.Fatal(err)
	}
	sessionKey := state.GoalSessionKey{GoalID: "00000000-0000-4000-8000-000000000002", LocalHandleID: "00000000-0000-4000-8000-000000000003"}
	inputTokens, outputTokens, cachedTokens := int64(12), int64(7), int64(3)
	active := &runningRun{
		goalSession:           &sessionKey,
		goalUsage:             &harness.Usage{State: harness.UsageUnknown, InputTokens: inputTokens, OutputTokens: outputTokens},
		goalCachedInputTokens: cachedTokens,
		goalUsageHasCached:    true,
	}
	daemon := &daemon{
		config: testConfig(t), store: store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: time.Now},
	}
	if err := daemon.queueNativeGoalUsage(context.Background(), key, nil); err != nil {
		t.Fatal(err)
	}
	j, err = store.LoadJournal(key)
	if err != nil || len(j.PendingGoalDeliveries) != 1 || j.PendingGoalDeliveries[0].Usage == nil {
		t.Fatalf("usage delivery = %#v, error = %v", j.PendingGoalDeliveries, err)
	}
	usage := j.PendingGoalDeliveries[0].Usage
	if usage.CostBasis != protocol.CostUnknown || usage.CostMicrousd != nil || usage.InputTokens == nil || usage.OutputTokens == nil || usage.CachedInputTokens == nil || *usage.InputTokens != inputTokens || *usage.OutputTokens != outputTokens || *usage.CachedInputTokens != cachedTokens {
		t.Fatalf("normalized usage = %#v", usage)
	}
}

func TestNativeUsageObservationPersistsOutsideEventOutboxAndIgnoresRegression(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: "00000000-0000-4000-8000-000000000002", LocalHandleID: "00000000-0000-4000-8000-000000000003"}
	app := &daemon{
		config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now},
		running: map[state.RunKey]*runningRun{key: {goalSession: &sessionKey}},
	}
	newer := json.RawMessage(`{"total":{"cached_input_tokens":20,"input_tokens":100,"output_tokens":40,"reasoning_output_tokens":10,"total_tokens":150}}`)
	older := json.RawMessage(`{"total":{"cached_input_tokens":10,"input_tokens":50,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":75}}`)
	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventUsageObserved, Payload: newer, At: time.Date(2026, 9, 9, 1, 0, 2, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := app.queueNativeEvent(key, harness.Event{Kind: harness.EventUsageObserved, Payload: older, At: time.Date(2026, 9, 9, 1, 0, 1, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.NativeUsageObservation == nil || journal.NativeUsageObservation.InputTokens != 100 || journal.NativeUsageObservation.TotalTokens != 150 {
		t.Fatalf("native usage observation = %#v, want latest monotonic snapshot", journal.NativeUsageObservation)
	}
	if len(journal.PendingEvents) != 2 {
		t.Fatalf("usage events = %d, want both durable diagnostics", len(journal.PendingEvents))
	}
	eventIDs := []string{journal.PendingEvents[0].EventID, journal.PendingEvents[1].EventID}
	if _, err := store.MarkEventsDelivered(key, eventIDs); err != nil {
		t.Fatal(err)
	}
	restarted := &daemon{config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now}}
	usage, shouldQueue, err := restarted.prepareNativeGoalUsage(key, nil)
	if err != nil || !shouldQueue || usage.InputTokens == nil || *usage.InputTokens != 100 || usage.OutputTokens == nil || *usage.OutputTokens != 40 {
		t.Fatalf("restart usage composition = usage:%#v shouldQueue:%t error:%v", usage, shouldQueue, err)
	}
}

func TestDeliveredNativeUsageDoesNotRegenerateAfterRestart(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010",
		RunID: key.RunID, UsageKey: nativeGoalUsageKey, Provider: "openai", Model: "gpt-test",
		CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z",
	}
	queued, err := store.QueueGoalUsage(key, usage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, state.GoalDeliveryUsage, usage.UsageKey, queued.PendingGoalDeliveries[0].PayloadDigest); err != nil {
		t.Fatal(err)
	}
	app := &daemon{config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now}}
	if _, shouldQueue, err := app.prepareNativeGoalUsage(key, nil); err != nil || shouldQueue {
		t.Fatalf("prepareNativeGoalUsage() = shouldQueue:%t error:%v, want retained delivered body", shouldQueue, err)
	}
}

func TestNativeGoalUsageFailureBlocksTerminalizationAndRetainsRun(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	j.GoalDeliveryEnabled = true
	if err := store.SaveJournal(j); err != nil {
		t.Fatal(err)
	}
	usageFailure := errors.New("usage persistence failed")
	active := &runningRun{
		nativeSession: &closeFailureGoalSession{waitResult: harness.TaskResult{Kind: harness.ResultSucceeded}},
		goalSession:   &sessionKey,
		goalAdmission: &admission,
	}
	daemon := &daemon{
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(), clock: time.Now,
			queueGoalUsage: func(state.RunKey, protocol.Usage) (state.RunJournal, error) {
				return state.RunJournal{}, usageFailure
			},
		},
	}

	daemon.waitForNativeRun(context.Background(), key, active, active.nativeSession)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" || len(journal.PendingTransitions) != 0 || !journal.RetainWorkspace || !active.cleanupBlocked || activeRunCanRenew(active) || !active.nativeUsageRetryPending {
		t.Fatalf("usage failure terminalized or released run: journal=%#v active=%#v", journal, active)
	}
}

func TestNativeGoalUsageRetrySucceedsWithoutRestartAndFinalizesOnce(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	now := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usageCalls := 0
	terminalCalls := 0
	var usages []protocol.Usage
	active := &runningRun{
		nativeSession: &closeFailureGoalSession{waitResult: harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "done"}},
		goalSession:   &sessionKey,
		goalAdmission: &admission,
	}
	daemon := &daemon{
		config:  testConfig(t),
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(), clock: func() time.Time { return now },
			queueGoalUsage: func(runKey state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
				usageCalls++
				usages = append(usages, cloneNativeUsage(usage))
				if usageCalls == 1 {
					return state.RunJournal{}, errors.New("injected usage persistence failure")
				}
				return store.QueueGoalUsage(runKey, usage)
			},
			queueTerminalTransition: func(runKey state.RunKey, transition protocol.StateTransitionRequest, at time.Time) (state.RunJournal, error) {
				terminalCalls++
				return store.QueueTerminalTransitionAt(runKey, transition, at)
			},
		},
	}

	daemon.waitForNativeRun(context.Background(), key, active, active.nativeSession)
	if usageCalls != 1 || activeRunCanRenew(active) || !active.nativeUsageRetryPending {
		t.Fatalf("initial usage failure state: calls=%d renew=%t active=%#v", usageCalls, activeRunCanRenew(active), active)
	}
	active.nativeUsageRetryAt = now.Add(-time.Second)
	daemon.flushPendingNativeUsage(context.Background())
	if usageCalls != 2 || terminalCalls != 1 || active.nativeUsageRetryPending || !active.nativeUsageFinalized {
		t.Fatalf("usage retry finalization: usage_calls=%d terminal_calls=%d active=%#v", usageCalls, terminalCalls, active)
	}
	daemon.flushPendingNativeUsage(context.Background())
	if usageCalls != 2 || terminalCalls != 1 {
		t.Fatalf("usage retry duplicated delivery: usage_calls=%d terminal_calls=%d", usageCalls, terminalCalls)
	}
	if len(usages) != 2 || !reflect.DeepEqual(usages[0], usages[1]) {
		t.Fatalf("usage retry changed immutable body: %#v", usages)
	}
	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.PendingGoalDeliveries) != 1 || j.PendingGoalDeliveries[0].Kind != state.GoalDeliveryUsage || len(j.PendingTransitions) != 1 {
		t.Fatalf("final usage/terminal journal = %#v", j)
	}
	if j.PendingGoalDeliveries[0].Usage == nil || j.PendingGoalDeliveries[0].Usage.UsageKey != nativeGoalUsageKey || j.PendingTransitions[0].State != "failed" {
		t.Fatalf("final usage/terminal bodies = %#v", j)
	}
}

func TestNativeGoalUsageRetryExhaustionBlocksRenewalAndRetainsEvidence(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	now := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usageCalls := 0
	active := &runningRun{
		nativeSession: &closeFailureGoalSession{waitResult: harness.TaskResult{Kind: harness.ResultSucceeded}},
		goalSession:   &sessionKey,
		goalAdmission: &admission,
	}
	daemon := &daemon{
		config:  testConfig(t),
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(), clock: func() time.Time { return now },
			queueGoalUsage: func(state.RunKey, protocol.Usage) (state.RunJournal, error) {
				usageCalls++
				return state.RunJournal{}, errors.New("persistent usage outage")
			},
		},
	}

	daemon.waitForNativeRun(context.Background(), key, active, active.nativeSession)
	for attempt := 0; attempt < nativeUsageRetryLimit-1; attempt++ {
		active.nativeUsageRetryAt = now.Add(-time.Second)
		daemon.flushPendingNativeUsage(context.Background())
	}
	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if usageCalls != nativeUsageRetryLimit || j.LocalState != "terminal_pending" || j.TerminalState != "failed" || len(j.PendingTransitions) != 1 || !j.RetainWorkspace || j.NativeUsageRecoveryRequired {
		t.Fatalf("exhausted usage retry journal: calls=%d journal=%#v", usageCalls, j)
	}
	if len(j.PendingGoalDeliveries) != 1 || j.PendingGoalDeliveries[0].Kind != state.GoalDeliveryUsage || j.PendingGoalDeliveries[0].DeliveryID != nativeGoalUsageKey || len(j.DeliveredGoalDeliveries) != 0 {
		t.Fatalf("exhausted usage retry did not retain an unacked usage delivery: %#v", j)
	}
	if activeRunCanRenew(active) || !active.nativeUsageRetryExhausted || !active.stale || !active.cleanupBlocked || daemon.runningRun(key) != nil {
		t.Fatalf("exhausted usage retry active state: renew=%t active=%#v", activeRunCanRenew(active), active)
	}
}

func TestNativeUsageRecoveryAtomicWriteFailureRetainsExactUsageUntilRetry(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010",
		RunID: key.RunID, UsageKey: nativeGoalUsageKey, Provider: "openai", Model: "gpt-test",
		CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z",
	}
	result := harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "done"}
	active := &runningRun{
		nativeFinalResult: &result,
		nativeFinalUsage:  &usage,
	}
	now := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	daemon := &daemon{
		config:  testConfig(t),
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: func() time.Time { return now }},
	}
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected native usage recovery write failure")
	})
	daemon.exhaustNativeUsageRetry(context.Background(), key, active, errors.New("usage retries exhausted"))

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.runningRun(key) != active || !active.nativeUsageRetryPending || !active.nativeUsageRetryExhausted || !active.cleanupBlocked || len(journal.PendingGoalDeliveries) != 0 || journal.NativeUsageRecoveryRequired {
		t.Fatalf("failed native usage recovery discarded in-memory evidence: active=%#v journal=%#v", active, journal)
	}
	restore()
	active.nativeUsageRetryAt = now.Add(-time.Second)
	daemon.flushPendingNativeUsage(context.Background())

	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.runningRun(key) != nil || journal.NativeUsageRecoveryRequired || len(journal.PendingGoalDeliveries) != 1 || journal.PendingGoalDeliveries[0].Usage == nil || !reflect.DeepEqual(*journal.PendingGoalDeliveries[0].Usage, usage) {
		t.Fatalf("native usage recovery did not retain and replay exact usage: journal=%#v", journal)
	}
}

func TestAbandonGoalSessionClearsPersistedProcessAfterVerifiedClose(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{clock: time.Now}}
	if err := daemon.abandonGoalSession(sessionKey, &closeFailureGoalSession{}, true, nil); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() {
		t.Fatalf("verified abandon retained process details: %#v", journal)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionState != state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateClosed {
		t.Fatalf("verified abandon session = %#v", session)
	}
}

func TestAbandonGoalSessionAfterStartTurnResponseLossPreservesUncertainty(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{clock: time.Now}}
	if err := daemon.abandonGoalSessionUncertain(sessionKey, &closeFailureGoalSession{}, errors.New("turn/start response lost")); err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !session.IsUncertainLaunch() || session.SessionState != state.GoalSessionStateUnavailable || session.LaunchState == state.GoalSessionLaunchStateClosed {
		t.Fatalf("start-turn uncertainty was released: %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.PID != 0 || journal.ProcessIdentity != "" {
		t.Fatalf("start-turn uncertainty recovery evidence = %#v", journal)
	}
}

func TestNativeWaitUsesFinalProcessFailureSnapshotForTerminalTransition(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	reason := protocol.TaskResultReasonProcessFailure
	final := harness.TaskResult{
		Kind:    harness.ResultFailed,
		Summary: "native process exited after terminal result",
		Reason:  &reason,
		Process: execution.Result{ExitCode: 7},
	}
	session := &closeFailureGoalSession{waitResult: final}
	active := &runningRun{
		nativeSession: session,
		goalSession:   &sessionKey,
		goalAdmission: &admission,
	}
	daemon := &daemon{
		store:   store,
		options: options{newID: ids(), clock: time.Now},
		running: map[state.RunKey]*runningRun{key: active},
		slots:   make(chan struct{}, 1),
	}

	daemon.waitForNativeRun(context.Background(), key, active, session)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "terminal_pending" && journal.TerminalState != "failed" {
		t.Fatalf("final process failure terminal state = %#v", journal)
	}
	if len(journal.PendingTransitions) == 0 || journal.PendingTransitions[len(journal.PendingTransitions)-1].State != "failed" || !strings.Contains(string(journal.PendingTransitions[len(journal.PendingTransitions)-1].Payload), "process_failure") {
		t.Fatalf("final process failure transition = %#v", journal.PendingTransitions)
	}
}

type closeFailureGoalSession struct {
	err        error
	waitResult harness.TaskResult
	waitErr    error
}

func (*closeFailureGoalSession) Control(context.Context, harness.ControlRequest) (harness.ControlReceipt, error) {
	return harness.ControlReceipt{}, errors.New("unexpected native control")
}

func (session *closeFailureGoalSession) Wait(context.Context) (harness.TaskResult, error) {
	return session.waitResult, session.waitErr
}

func (session *closeFailureGoalSession) Close(context.Context) error { return session.err }

func TestGoalUsageOutboxRetriesExactBodyAndBlocksTerminalCleanup(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010", RunID: key.RunID,
		UsageKey: "late-usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z",
	}
	queued, err := store.QueueGoalUsage(key, usage)
	if err != nil {
		t.Fatalf("QueueGoalUsage() error = %v", err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	journal, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}, usageErrors: []error{errors.New("response lost after receiver commit")}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{clock: time.Now}}

	if err := daemon.cleanupPending(context.Background(), journal); err == nil {
		t.Fatal("cleanupPending() succeeded despite pending Goal usage")
	}
	if _, _, err := daemon.flushGoalDeliveries(context.Background(), journal); err == nil {
		t.Fatal("first flushGoalDeliveries() succeeded despite lost response")
	}
	afterFailure, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFailure.PendingGoalDeliveries) != 1 || afterFailure.PendingGoalDeliveries[0].PayloadDigest != queued.PendingGoalDeliveries[0].PayloadDigest {
		t.Fatalf("lost response changed durable Goal usage: %#v", afterFailure.PendingGoalDeliveries)
	}

	updated, _, err := daemon.flushGoalDeliveries(context.Background(), afterFailure)
	if err != nil {
		t.Fatalf("second flushGoalDeliveries() error = %v", err)
	}
	if updated.HasPendingGoalDeliveries() {
		t.Fatalf("verified usage receipt remained pending: %#v", updated.PendingGoalDeliveries)
	}
	if len(client.usages) != 2 || !reflect.DeepEqual(client.usages[0], client.usages[1]) || client.fences[0] != client.fences[1] || client.fences[0] != queued.Fence() {
		t.Fatalf("usage retries were not exact: usages=%#v fences=%#v", client.usages, client.fences)
	}
	if err := daemon.cleanupPending(context.Background(), updated); err != nil {
		t.Fatalf("cleanupPending() after verified usage error = %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal after cleanup error = %v, want not found", err)
	}
}

func TestJournalFingerprintTracksGoalDeliveryState(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	before, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	payload := state.GoalSessionAttachDelivery{
		GoalID: "00000000-0000-4000-8000-000000000002", LocalHandleID: "00000000-0000-4000-8000-000000000003",
		HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test",
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`,
	}
	queued, err := store.QueueGoalSessionAttach(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatal(err)
	}
	if journalFingerprint(before) == journalFingerprint(queued) || journalFingerprint(queued) == journalFingerprint(ready) {
		t.Fatalf("Goal delivery mutation did not change retry fingerprint: before=%s queued=%s ready=%s", journalFingerprint(before), journalFingerprint(queued), journalFingerprint(ready))
	}
}

func TestGoalUsageConflictRemainsPendingForAccountingRecovery(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010", RunID: key.RunID, UsageKey: "rejected-usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z"}
	if _, err := store.QueueGoalUsage(key, usage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	journal, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}, usageErrors: []error{&control.APIError{StatusCode: http.StatusConflict, Code: control.StateConflict, Message: "stale admission"}}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{clock: time.Now}}
	updated, _, err := daemon.flushGoalDeliveries(context.Background(), journal)
	if err == nil {
		t.Fatal("flushGoalDeliveries() succeeded despite ambiguous usage conflict")
	}
	if !updated.HasPendingGoalDeliveries() || len(updated.RetiredGoalDeliveries) != 0 || updated.PendingGoalDeliveries[0].Usage == nil || updated.PendingGoalDeliveries[0].Usage.CostBasis != protocol.CostUnknown {
		t.Fatalf("ambiguous usage conflict did not preserve unknown accounting: %#v", updated)
	}
	if err := daemon.cleanupPending(context.Background(), updated); err == nil {
		t.Fatal("cleanupPending() removed journal with unresolved usage accounting")
	}
}

func TestGoalUsageIdempotencyConflictRetiresDefinitiveRejection(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010", RunID: key.RunID, UsageKey: "rejected-usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z"}
	if _, err := store.QueueGoalUsage(key, usage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	journal, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}, usageErrors: []error{&control.APIError{StatusCode: http.StatusConflict, Code: control.IdempotencyConflict, Message: "usage key body differs"}}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{clock: time.Now}}
	updated, _, err := daemon.flushGoalDeliveries(context.Background(), journal)
	if err != nil {
		t.Fatalf("flushGoalDeliveries() error = %v", err)
	}
	if updated.HasPendingGoalDeliveries() || len(updated.RetiredGoalDeliveries) != 1 || updated.RetiredGoalDeliveries[0].Code != string(control.IdempotencyConflict) {
		t.Fatalf("definitive usage idempotency conflict was not retired: %#v", updated)
	}
}

func TestDefinitiveGoalEvidenceRejectionRetiresAndAllowsCleanup(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	evidence := testGoalEvidence(t, key.RunID)
	if _, err := store.QueueGoalEvidence(key, evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	journal, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}, evidenceErrors: []error{&control.APIError{StatusCode: http.StatusConflict, Code: control.StateConflict, Message: "stale admission"}}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{clock: time.Now}}
	updated, _, err := daemon.flushGoalDeliveries(context.Background(), journal)
	if err != nil {
		t.Fatalf("flushGoalDeliveries() error = %v", err)
	}
	if updated.HasPendingGoalDeliveries() || len(updated.RetiredGoalDeliveries) != 1 {
		t.Fatalf("definitive evidence rejection was not durably retired: %#v", updated)
	}
	if err := daemon.cleanupPending(context.Background(), updated); err != nil {
		t.Fatalf("cleanupPending() after evidence rejection error = %v", err)
	}
}

func TestRecoveryDiscardsUnreadyGoalSessionAttachBeforeTerminalCleanup(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
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
	attach := state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: sessionKey.LocalHandleID, HarnessKind: "codex", HarnessVersion: "0.153.4",
		AdapterVersion: "symmetry-daemon:test", WorkspaceFingerprint: intent.WorkspaceFingerprint, Workspace: "local",
	}
	if _, err := store.QueueGoalSessionAttach(key, attach); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now}, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	if err := daemon.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingGoalDeliveries) != 0 {
		t.Fatalf("unready attach remained after recovery: %#v", journal.PendingGoalDeliveries)
	}
	if journal.TerminalState != "failed" || !journal.RetainWorkspace || len(journal.PendingTransitions) != 1 {
		t.Fatalf("recovered journal = %#v", journal)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !session.IsUncertainLaunch() || session.SessionState != state.GoalSessionStateUnavailable {
		t.Fatalf("recovered session = %#v", session)
	}
}

func TestLateGoalUsageFlushUsesRetainedOriginalFenceWithoutRunJournal(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	initial := protocol.Usage{SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010", RunID: key.RunID, UsageKey: "initial", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z"}
	queued, err := store.QueueGoalUsage(key, initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, state.GoalDeliveryUsage, initial.UsageKey, queued.PendingGoalDeliveries[0].PayloadDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "terminal", State: "failed", Payload: []byte(`{}`)}, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 2, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatal(err)
	}
	late := initial
	late.UsageID = "00000000-0000-4000-8000-000000000013"
	late.UsageKey = "final-usage"
	if _, err := store.QueueGoalUsage(key, late); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("late usage recreated run journal: %v", err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, options: options{clock: time.Now}}
	daemon.flushLateGoalUsage(context.Background())
	ledger, err := store.LoadLateGoalUsage(key)
	if err != nil || len(ledger.PendingUsageDeliveries) != 0 || len(client.fences) != 1 || client.fences[0] != ledger.Fence {
		t.Fatalf("late usage flush = ledger:%#v fences:%#v error:%v", ledger, client.fences, err)
	}
}

func TestStaleGoalDeliveryStartupScanCleansAfterFinalReceipt(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000020",
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostUnknown,
		ObservedAt:    "2026-09-09T01:00:00Z",
	}
	if _, err := store.QueueGoalUsage(key, usage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "stale"); err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}}
	daemon := &daemon{config: testConfig(t), store: store, control: client, options: options{clock: time.Now}}
	daemon.enqueueRecoveredCleanups()
	daemon.flushAll(context.Background())
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("stale journal after final Goal receipt = %v, want deleted", err)
	}
	ledger, err := store.LoadLateGoalUsage(key)
	if err != nil || len(ledger.DeliveredDeliveries) != 1 || len(client.usages) != 1 {
		t.Fatalf("stale Goal delivery cleanup = ledger:%#v usages:%#v error:%v", ledger, client.usages, err)
	}
}

func TestGoalDeliveryMalformedOrMismatchedReceiptRetainsExactIntent(t *testing.T) {
	tests := []struct {
		name  string
		queue func(*state.Store, state.RunKey) (state.RunJournal, state.GoalDeliveryKind, string, error)
		setup func(*goalDeliveryControl)
	}{
		{
			name: "attach malformed",
			queue: func(store *state.Store, key state.RunKey) (state.RunJournal, state.GoalDeliveryKind, string, error) {
				payload := state.GoalSessionAttachDelivery{GoalID: "00000000-0000-4000-8000-000000000002", LocalHandleID: "00000000-0000-4000-8000-000000000003", HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test", WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`}
				journal, err := store.QueueGoalSessionAttach(key, payload)
				if err == nil {
					journal, err = store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
				}
				return journal, state.GoalDeliverySessionAttach, payload.LocalHandleID, err
			},
			setup: func(client *goalDeliveryControl) { client.attachReceipt = &control.GoalSessionReceipt{} },
		},
		{
			name: "attach mismatch",
			queue: func(store *state.Store, key state.RunKey) (state.RunJournal, state.GoalDeliveryKind, string, error) {
				payload := state.GoalSessionAttachDelivery{GoalID: "00000000-0000-4000-8000-000000000002", LocalHandleID: "00000000-0000-4000-8000-000000000003", HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test", WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`}
				journal, err := store.QueueGoalSessionAttach(key, payload)
				if err == nil {
					journal, err = store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
				}
				return journal, state.GoalDeliverySessionAttach, payload.LocalHandleID, err
			},
			setup: func(client *goalDeliveryControl) {
				client.attachReceipt = &control.GoalSessionReceipt{ID: "receipt", LocalHandleID: "other"}
			},
		},
		{
			name: "evidence mismatch",
			queue: func(store *state.Store, key state.RunKey) (state.RunJournal, state.GoalDeliveryKind, string, error) {
				evidence := testGoalEvidence(t, key.RunID)
				journal, err := store.QueueGoalEvidence(key, evidence)
				return journal, state.GoalDeliveryEvidence, evidence.EvidenceKey, err
			},
			setup: func(client *goalDeliveryControl) {
				client.evidenceReceipt = &control.GoalEvidenceReceipt{ID: "00000000-0000-4000-8000-000000000011", RunID: "00000000-0000-4000-8000-000000000001", EvidenceKey: "other"}
			},
		},
		{
			name: "usage mismatch",
			queue: func(store *state.Store, key state.RunKey) (state.RunJournal, state.GoalDeliveryKind, string, error) {
				usage := protocol.Usage{SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010", RunID: key.RunID, UsageKey: "usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z"}
				journal, err := store.QueueGoalUsage(key, usage)
				return journal, state.GoalDeliveryUsage, usage.UsageKey, err
			},
			setup: func(client *goalDeliveryControl) {
				client.usageReceipt = &control.GoalUsageReceipt{ID: "00000000-0000-4000-8000-000000000010", RunID: "00000000-0000-4000-8000-000000000001", UsageKey: "usage", CostBasis: "reported"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			journal, kind, deliveryID, err := test.queue(store, key)
			if err != nil {
				t.Fatal(err)
			}
			original := journal.PendingGoalDeliveries[0]
			client := &goalDeliveryControl{fakeControl: &fakeControl{}}
			test.setup(client)
			daemon := &daemon{config: testConfig(t), store: store, control: client, options: options{clock: time.Now}}
			if _, _, err := daemon.deliverGoalDelivery(context.Background(), journal, kind, deliveryID, nil); err == nil {
				t.Fatal("deliverGoalDelivery() succeeded with invalid receipt")
			}
			loaded, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.PendingGoalDeliveries) != 1 || !reflect.DeepEqual(loaded.PendingGoalDeliveries[0], original) {
				t.Fatalf("invalid receipt changed durable delivery: got=%#v want=%#v", loaded.PendingGoalDeliveries, original)
			}
		})
	}
}

type goalDeliveryControl struct {
	*fakeControl
	usageErrors     []error
	evidenceErrors  []error
	usages          []protocol.Usage
	fences          []protocol.Fence
	attachReceipt   *control.GoalSessionReceipt
	evidenceReceipt *control.GoalEvidenceReceipt
	usageReceipt    *control.GoalUsageReceipt
}

func (client *goalDeliveryControl) AttachHarnessSession(_ context.Context, _ string, request control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	if client.attachReceipt != nil {
		return *client.attachReceipt, nil
	}
	return control.GoalSessionReceipt{}, errors.New("unexpected session attach")
}

func (client *goalDeliveryControl) FetchRunContext(context.Context, string, protocol.Fence) (control.GoalRunContext, error) {
	return control.GoalRunContext{}, errors.New("unexpected context request")
}

func (client *goalDeliveryControl) AppendEvidence(_ context.Context, runID string, _ protocol.Fence, evidence protocol.Evidence) (control.GoalEvidenceReceipt, error) {
	if len(client.evidenceErrors) != 0 {
		err := client.evidenceErrors[0]
		client.evidenceErrors = client.evidenceErrors[1:]
		return control.GoalEvidenceReceipt{}, err
	}
	if client.evidenceReceipt != nil {
		return *client.evidenceReceipt, nil
	}
	return control.GoalEvidenceReceipt{ID: evidence.EvidenceID, RunID: runID, EvidenceKey: evidence.EvidenceKey, Kind: string(evidence.Kind), SubjectHash: evidence.SubjectHash, Verdict: string(evidence.Verdict)}, nil
}

func (client *goalDeliveryControl) RecordUsage(_ context.Context, runID string, fence protocol.Fence, usage protocol.Usage) (control.GoalUsageReceipt, error) {
	client.usages = append(client.usages, usage)
	client.fences = append(client.fences, fence)
	if len(client.usageErrors) != 0 {
		err := client.usageErrors[0]
		client.usageErrors = client.usageErrors[1:]
		return control.GoalUsageReceipt{}, err
	}
	if client.usageReceipt != nil {
		return *client.usageReceipt, nil
	}
	return control.GoalUsageReceipt{ID: usage.UsageID, RunID: runID, UsageKey: usage.UsageKey, CostBasis: string(usage.CostBasis), CostMicrousd: usage.CostMicrousd}, nil
}

func testGoalEvidence(t *testing.T, runID string) protocol.Evidence {
	t.Helper()
	subject := protocol.Subject{ResourceID: "00000000-0000-4000-8000-000000000012", Commit: "0000000000000000000000000000000000000000", TreeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	profileHash := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	evidence, err := protocol.ParseEvidence([]byte(fmt.Sprintf(`{"schema_version":"symmetry.evidence.v1","evidence_id":"00000000-0000-4000-8000-000000000011","run_id":"%s","evidence_key":"evidence","kind":"check","subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},"subject_hash":"%s","source_ref":{"kind":"check","ref":"check-run","validator_profile":"default-checks","subject_hash":"%s"},"source_revision":"profile:default-checks","validator_profile":"default-checks","verdict":"passed","payload":{"predicate_id":"tests","subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},"profile_digest":"%s","command_argv_digest":"%s","exit_code":0,"subject_hash":"%s","started_at":"2026-09-09T01:00:00Z","finished_at":"2026-09-09T01:01:00Z","output_ref":{"kind":"artifact","value":"artifact:check-output"}},"observed_at":"2026-09-09T01:01:00Z"}`, runID, subject.ResourceID, subject.Commit, subject.TreeDigest, subjectHash, subjectHash, subject.ResourceID, subject.Commit, subject.TreeDigest, profileHash, profileHash, subjectHash)))
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func claimedGoalDeliveryStore(t *testing.T) (*state.Store, state.RunKey) {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key := state.RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	work := protocol.Work{Goal: "goal", Input: []byte(`{}`)}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", LocalState: "claiming", Work: work, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 12, 31, 1, 0, 0, 0, time.UTC), Work: work}); err != nil {
		t.Fatal(err)
	}
	return store, key
}
