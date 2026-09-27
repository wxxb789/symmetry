package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestPrelaunchRecoveryKeepsIntentUntilTerminalIsDurable(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}

	stateDirectory := t.TempDir()
	store, key := claimedStoreAt(t, stateDirectory)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.MarkWorkspaceRecoveryRequired(key); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, `C:\workspace`)
	sessionKey := state.GoalSessionKey{
		GoalID:        admission.GoalID,
		LocalHandleID: "00000000-0000-4000-8000-000000000007",
	}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID:         "00000000-0000-4000-8000-000000000008",
		GoalID:                 admission.GoalID,
		GoalRevision:           admission.GoalRevision,
		WorkItemID:             admissionWorkItemIDValue(admission.WorkItemID),
		TaskID:                 "task-1",
		RunID:                  key.RunID,
		Generation:             key.Generation,
		AdmissionID:            admission.AdmissionID,
		LocalHandleID:          sessionKey.LocalHandleID,
		RuntimeID:              "runtime-1",
		RuntimeEpoch:           1,
		HarnessKind:            "codex",
		HarnessVersion:         codex.TestedVersion,
		AdapterVersion:         "symmetry-daemon:test",
		AdapterProtocolVersion: 1,
		WorkspaceFingerprint:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SessionMode:            state.GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}

	startCalls := 0
	failure := errors.New("injected terminal persistence failure")
	queueCalls := 0
	failedRecovery := &daemon{
		config: testConfig(t),
		store:  store,
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			startCalls++
			return nil, errors.New("unexpected native start")
		},
		options: options{
			clock: time.Now,
			newID: ids(),
			queueTerminalTransition: func(state.RunKey, protocol.StateTransitionRequest, time.Time) (state.RunJournal, error) {
				queueCalls++
				return state.RunJournal{}, failure
			},
		},
		running: make(map[state.RunKey]*runningRun),
		slots:   make(chan struct{}, 1),
	}
	if err := failedRecovery.recoverUnclosedGoalSessions(context.Background()); !errors.Is(err, errRecoveryPending) || !errors.Is(err, failure) {
		t.Fatalf("first recovery error = %v, want pending terminal persistence failure", err)
	}
	if queueCalls != 2 {
		t.Fatalf("terminal queue calls = %d, want two consecutive failed writes", queueCalls)
	}
	assertPrelaunchRecoveryIntent(t, store, key, sessionKey)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restartedStore, err := state.New(stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedStore.Close()
	restarted := &daemon{
		config: testConfig(t),
		store:  restartedStore,
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			startCalls++
			return nil, errors.New("unexpected native start")
		},
		options: options{clock: time.Now, newID: ids()},
		running: make(map[state.RunKey]*runningRun),
		slots:   make(chan struct{}, 1),
	}
	if err := restarted.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatalf("restart recovery error = %v", err)
	}
	if startCalls != 0 {
		t.Fatalf("native start calls = %d, want none", startCalls)
	}

	session, err := restartedStore.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.LaunchState != state.GoalSessionLaunchStateClosed || session.SessionState != state.GoalSessionStateClosed || session.NeedsReconciliation() {
		t.Fatalf("recovered pre-launch session = %#v, want a known closed session", session)
	}
	journal, err := restartedStore.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 {
		t.Fatalf("recovered terminal journal = %#v, want exactly one failed transition", journal)
	}
	var payload map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stage"] != "goal_session_recovery" || payload["reason"] != string(protocol.TaskResultReasonProcessFailure) {
		t.Fatalf("recovered terminal payload = %#v, want process_failure", payload)
	}
	if journal.RetainWorkspace || journal.NativeUsageRecoveryRequired || hasGoalUsageDelivery(journal) {
		t.Fatalf("pre-launch recovery invented workspace retention or usage: %#v", journal)
	}
}

func assertPrelaunchRecoveryIntent(t *testing.T, store *state.Store, key state.RunKey, sessionKey state.GoalSessionKey) {
	t.Helper()
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.LaunchState != state.GoalSessionLaunchStateIntent || session.SessionState != state.GoalSessionStateBusy || session.LaunchAttempted || session.NeedsReconciliation() || session.NativeSessionID != "" || session.NativeSessionFilename != "" {
		t.Fatalf("failed terminal write changed recoverable launch intent: %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" || len(journal.PendingTransitions) != 0 || journal.RetainWorkspace || journal.NativeUsageRecoveryRequired || hasGoalUsageDelivery(journal) {
		t.Fatalf("failed terminal write crossed the pre-launch recovery barrier: %#v", journal)
	}
}

func hasGoalUsageDelivery(journal state.RunJournal) bool {
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage {
			return true
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == state.GoalDeliveryUsage {
			return true
		}
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage {
			return true
		}
	}
	return false
}
