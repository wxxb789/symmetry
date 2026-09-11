package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGoalSessionJournalRoundTripAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if saved.LaunchState != GoalSessionLaunchStateIntent || saved.SessionState != GoalSessionStateBusy || saved.NeedsReconciliation() {
		t.Fatalf("saved launch intent = %#v", saved)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() error = %v", err)
	}
	if loaded.Intent() != saved.Intent() || loaded.SchemaVersion != goalSessionSchemaVersion || loaded.CreatedAt.IsZero() {
		t.Fatalf("loaded journal = %#v, want %#v", loaded, saved)
	}

	startedAt := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	if _, err := restarted.MarkGoalSessionLaunchStarted(saved.Key(), startedAt); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	attached, err := restarted.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{
		NativeSessionID:       "native-session-local-only",
		NativeSessionFilename: `C:\native\session.json`,
	}, testGoalSessionCompatibility())
	if err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}
	if attached.LaunchState != GoalSessionLaunchStateAttached || attached.NeedsReconciliation() {
		t.Fatalf("attached journal = %#v", attached)
	}
	if err := restarted.Close(); err != nil {
		t.Fatalf("restart Close() error = %v", err)
	}

	restartedAgain, err := New(directory)
	if err != nil {
		t.Fatalf("second restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restartedAgain.Close() })
	loaded, err = restartedAgain.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after attach error = %v", err)
	}
	if loaded.NativeSessionID != "native-session-local-only" || loaded.NativeSessionFilename != `C:\native\session.json` {
		t.Fatalf("native handle was not durable: %#v", loaded)
	}
}

func TestGoalSessionLaunchIntentReplayAndConflict(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	first, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("first SaveGoalSessionLaunchIntent() error = %v", err)
	}
	replayed, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("same SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if replayed != first {
		t.Fatalf("same intent replay = %#v, want %#v", replayed, first)
	}
	conflict := intent
	conflict.WorkspaceFingerprint = "sha256:workspace-two"
	if _, err := store.SaveGoalSessionLaunchIntent(conflict); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("changed intent error = %v, want ErrGoalSessionConflict", err)
	}
	duplicate := intent
	duplicate.LocalHandleID = "handle-2"
	if _, err := store.SaveGoalSessionLaunchIntent(duplicate); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("duplicate active launch error = %v, want ErrGoalSessionConflict", err)
	}
}

func TestGoalSessionHandoffLaunchIntentReplayPreservesSourceLineage(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	intent.SessionMode = GoalSessionModeHandoff
	intent.HandoffSourceRunID = "00000000-0000-4000-8000-000000000090"

	first, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if first.HandoffSourceRunID != intent.HandoffSourceRunID || first.NativeSessionID != "" || first.NativeSessionFilename != "" {
		t.Fatalf("handoff launch intent = %#v, want source provenance without a native handle", first)
	}
	replayed, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("replay SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if replayed != first {
		t.Fatalf("handoff replay = %#v, want %#v", replayed, first)
	}

	changedSource := intent
	changedSource.HandoffSourceRunID = "00000000-0000-4000-8000-000000000091"
	if _, err := store.SaveGoalSessionLaunchIntent(changedSource); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("changed handoff source error = %v, want ErrGoalSessionConflict", err)
	}
}

func TestGoalSessionHandoffLaunchIntentValidationKeepsNativeSessionsFresh(t *testing.T) {
	base := testGoalSessionIntent()
	validSource := "00000000-0000-4000-8000-000000000092"

	tests := []struct {
		name   string
		intent GoalSessionLaunchIntent
	}{
		{
			name: "handoff requires source Run UUID",
			intent: func() GoalSessionLaunchIntent {
				intent := base
				intent.SessionMode = GoalSessionModeHandoff
				return intent
			}(),
		},
		{
			name: "handoff rejects malformed source Run UUID",
			intent: func() GoalSessionLaunchIntent {
				intent := base
				intent.SessionMode = GoalSessionModeHandoff
				intent.HandoffSourceRunID = "not-a-uuid"
				return intent
			}(),
		},
		{
			name: "fresh rejects handoff source Run",
			intent: func() GoalSessionLaunchIntent {
				intent := base
				intent.HandoffSourceRunID = validSource
				return intent
			}(),
		},
		{
			name: "resume rejects handoff source Run",
			intent: func() GoalSessionLaunchIntent {
				intent := base
				intent.SessionMode = GoalSessionModeResume
				intent.HandoffSourceRunID = validSource
				return intent
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mustStore(t).SaveGoalSessionLaunchIntent(test.intent); err == nil {
				t.Fatal("SaveGoalSessionLaunchIntent() succeeded with invalid handoff lineage")
			}
		})
	}
}

func TestGoalSessionHandoffLineageSurvivesRestartAndRecovery(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	intent := testGoalSessionIntent()
	intent.SessionMode = GoalSessionModeHandoff
	intent.HandoffSourceRunID = "00000000-0000-4000-8000-000000000093"
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.MarkGoalSessionUncertain(saved.Key(), "restart after fresh handoff launch"); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() error = %v", err)
	}
	if loaded.HandoffSourceRunID != intent.HandoffSourceRunID || loaded.NativeSessionID != "" || loaded.NativeSessionFilename != "" || !loaded.NeedsReconciliation() {
		t.Fatalf("restarted handoff journal = %#v", loaded)
	}

	recovered, err := restarted.ResolveGoalSessionUncertain(saved.Key(), GoalSessionHandle{NativeSessionID: "fresh-handoff-native-session"}, testGoalSessionCompatibility())
	if err != nil {
		t.Fatalf("ResolveGoalSessionUncertain() error = %v", err)
	}
	if recovered.HandoffSourceRunID != intent.HandoffSourceRunID || recovered.NativeSessionID != "fresh-handoff-native-session" || recovered.NativeSessionFilename != "" {
		t.Fatalf("recovered handoff journal = %#v", recovered)
	}
}

func TestGoalSessionConcurrentIntentReplayIsSerialized(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
				t.Errorf("SaveGoalSessionLaunchIntent() error = %v", err)
			}
		}()
	}
	group.Wait()
	journals, err := store.ListGoalSessions()
	if err != nil {
		t.Fatalf("ListGoalSessions() error = %v", err)
	}
	if len(journals) != 1 || journals[0].Key() != intent.Key() {
		t.Fatalf("journals after concurrent replay = %#v", journals)
	}
}

func TestGoalSessionUnknownFieldsAreRejected(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	object["unexpected"] = true
	encoded, err = json.Marshal(object)
	if err != nil {
		t.Fatalf("Marshal(object) error = %v", err)
	}
	if err := os.WriteFile(store.goalSessionPath(saved.Key()), encoded, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.LoadGoalSession(saved.Key()); err == nil || !strings.Contains(err.Error(), "decode goal session journal") {
		t.Fatalf("LoadGoalSession() error = %v, want strict unknown-field failure", err)
	}
}

func TestGoalSessionCrashGapIsQueryableAndBlocksDuplicateRestart(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key(), time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}

	loaded, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() error = %v", err)
	}
	if !loaded.IsUncertainLaunch() || loaded.LaunchState != GoalSessionLaunchStateLaunching || loaded.NativeSessionID != "" {
		t.Fatalf("crash-gap journal = %#v", loaded)
	}
	if replayed, err := store.SaveGoalSessionLaunchIntent(intent); !errors.Is(err, ErrGoalSessionUncertain) || !replayed.NeedsReconciliation() {
		t.Fatalf("same uncertain replay = %#v, error = %v", replayed, err)
	}

	duplicate := intent
	duplicate.LocalHandleID = "handle-replacement"
	if _, err := store.SaveGoalSessionLaunchIntent(duplicate); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("duplicate restart error = %v, want ErrGoalSessionUncertain", err)
	}
	if err := store.CheckGoalSessionCompatibility(saved.Key(), testGoalSessionCompatibility()); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("compatibility check error = %v, want ErrGoalSessionUncertain", err)
	}

	if _, err := store.MarkGoalSessionUncertain(saved.Key(), "native creation returned before handle persistence"); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	reconciled, err := store.ResolveGoalSessionUncertain(saved.Key(), GoalSessionHandle{NativeSessionID: "reconciled-native"}, testGoalSessionCompatibility())
	if err != nil {
		t.Fatalf("ResolveGoalSessionUncertain() error = %v", err)
	}
	if reconciled.LaunchState != GoalSessionLaunchStateAttached || reconciled.NeedsReconciliation() {
		t.Fatalf("reconciled journal = %#v", reconciled)
	}
}

func TestGoalSessionLineageBlocksCrossGenerationButAllowsIndependentWorkItem(t *testing.T) {
	store := mustStore(t)
	firstIntent := testGoalSessionIntent()
	first, err := store.SaveGoalSessionLaunchIntent(firstIntent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent(first) error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(first.Key(), time.Date(2026, 9, 9, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}

	status, err := store.CheckGoalSessionLineage(firstIntent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() error = %v", err)
	}
	if !status.BlocksNewLaunch() || status.State != GoalSessionLineageStateUncertain || len(status.SessionKeys) != 1 || status.SessionKeys[0] != first.Key() {
		t.Fatalf("lineage status = %#v", status)
	}

	retry := firstIntent
	retry.LaunchIntentID = "intent-retry-2"
	retry.LocalHandleID = "handle-retry-2"
	retry.RunID = "run-2"
	retry.Generation = 2
	if _, err := store.SaveGoalSessionLaunchIntent(retry); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("cross-generation retry error = %v, want ErrGoalSessionUncertain", err)
	}

	independent := retry
	independent.LaunchIntentID = "intent-independent"
	independent.LocalHandleID = "handle-independent"
	independent.RunID = "run-independent"
	independent.Generation = 1
	independent.WorkItemID = "work-item-independent"
	independent.TaskID = "task-independent"
	if saved, err := store.SaveGoalSessionLaunchIntent(independent); err != nil {
		t.Fatalf("independent WorkItem launch error = %v", err)
	} else if saved.LineageKey() == firstIntent.LineageKey() {
		t.Fatalf("independent launch reused blocked lineage: %#v", saved)
	}
}

func TestGoalSessionLineageReleasesOnlyAfterExplicitKnownStopAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key(), time.Date(2026, 9, 9, 4, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.MarkGoalSessionUncertain(saved.Key(), "native launch result was lost"); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	status, err := restarted.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() after restart error = %v", err)
	}
	if !status.BlocksNewLaunch() || status.State != GoalSessionLineageStateUncertain {
		t.Fatalf("restarted lineage status = %#v", status)
	}

	retry := intent
	retry.LaunchIntentID = "intent-after-restart"
	retry.LocalHandleID = "handle-after-restart"
	retry.RunID = "run-after-restart"
	retry.Generation = 2
	if _, err := restarted.SaveGoalSessionLaunchIntent(retry); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("retry before known stop error = %v, want ErrGoalSessionUncertain", err)
	}

	wrong := testGoalSessionCompatibility()
	wrong.WorkspaceFingerprint = "sha256:other-workspace"
	if _, err := restarted.ResolveGoalSessionUncertainStopped(saved.Key(), wrong); !errors.Is(err, ErrGoalSessionWorkspaceMismatch) {
		t.Fatalf("ResolveGoalSessionUncertainStopped() mismatch error = %v", err)
	}
	if _, err := restarted.ResolveGoalSessionUncertainStopped(saved.Key(), testGoalSessionCompatibility()); err != nil {
		t.Fatalf("ResolveGoalSessionUncertainStopped() error = %v", err)
	}
	status, err = restarted.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() after known stop error = %v", err)
	}
	if status.BlocksNewLaunch() || status.State != GoalSessionLineageStateFree || len(status.SessionKeys) != 0 {
		t.Fatalf("lineage remained blocked after known stop: %#v", status)
	}
	if saved, err := restarted.SaveGoalSessionLaunchIntent(retry); err != nil {
		t.Fatalf("retry after known stop error = %v", err)
	} else if saved.Key() != retry.Key() {
		t.Fatalf("retry after known stop key = %#v, want %#v", saved.Key(), retry.Key())
	}
}

func TestGoalSessionLineageAttachedStateReleasesOnlyAfterClose(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-session"}); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}
	status, err := store.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() attached error = %v", err)
	}
	if !status.BlocksNewLaunch() || status.State != GoalSessionLineageStateAttached {
		t.Fatalf("attached lineage status = %#v", status)
	}
	retry := intent
	retry.LaunchIntentID = "intent-attached-retry"
	retry.LocalHandleID = "handle-attached-retry"
	retry.RunID = "run-attached-retry"
	retry.Generation = 2
	if _, err := store.SaveGoalSessionLaunchIntent(retry); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("attached retry error = %v, want ErrGoalSessionConflict", err)
	}
	if _, err := store.CloseGoalSession(saved.Key()); err != nil {
		t.Fatalf("CloseGoalSession() error = %v", err)
	}
	status, err = store.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() closed error = %v", err)
	}
	if status.BlocksNewLaunch() || status.State != GoalSessionLineageStateFree {
		t.Fatalf("closed lineage status = %#v", status)
	}
}

func TestGoalSessionLineageDirtyIndexRebuildsFromJournal(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	dirty := goalSessionLineageIndex{
		SchemaVersion: goalSessionLineageSchemaVersion,
		Lineage:       intent.LineageKey(),
		Dirty:         true,
		Entries:       []goalSessionLineageEntry{},
	}
	encoded, err := json.Marshal(dirty)
	if err != nil {
		t.Fatalf("Marshal(dirty index) error = %v", err)
	}
	if err := os.WriteFile(store.goalSessionLineagePath(intent.LineageKey()), encoded, 0o600); err != nil {
		t.Fatalf("WriteFile(dirty index) error = %v", err)
	}

	status, err := store.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() error = %v", err)
	}
	if !status.BlocksNewLaunch() || status.State != GoalSessionLineageStateUncertain || len(status.SessionKeys) != 1 || status.SessionKeys[0] != saved.Key() {
		t.Fatalf("rebuilt lineage status = %#v", status)
	}
	clean, err := store.loadGoalSessionLineageIndexLocked(intent.LineageKey())
	if err != nil {
		t.Fatalf("load rebuilt index error = %v", err)
	}
	if clean.Dirty || len(clean.Entries) != 1 || clean.Entries[0].SessionKey != saved.Key() {
		t.Fatalf("rebuilt index = %#v", clean)
	}
}

func TestGoalSessionReconciliationRequiresStrictCompatibilityIncludingClosedReplay(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.MarkGoalSessionUncertain(saved.Key(), "reconcile strict compatibility"); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}

	if _, err := store.ResolveGoalSessionUncertainStopped(saved.Key(), GoalSessionCompatibility{}); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("zero compatibility error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	if _, err := store.ResolveGoalSessionUncertain(saved.Key(), GoalSessionHandle{NativeSessionID: "native-reconciled"}, GoalSessionCompatibility{}); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("zero compatibility reattach error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	if _, err := store.ResolveGoalSessionUncertainStopped(saved.Key(), testGoalSessionCompatibility()); err != nil {
		t.Fatalf("ResolveGoalSessionUncertainStopped() error = %v", err)
	}

	wrong := testGoalSessionCompatibility()
	wrong.WorkspaceFingerprint = "sha256:wrong-after-close"
	if _, err := store.ResolveGoalSessionUncertainStopped(saved.Key(), wrong); !errors.Is(err, ErrGoalSessionWorkspaceMismatch) {
		t.Fatalf("closed replay mismatch error = %v, want ErrGoalSessionWorkspaceMismatch", err)
	}
	closed, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after closed replay error = %v", err)
	}
	if closed.LaunchState != GoalSessionLaunchStateClosed || closed.SessionState != GoalSessionStateClosed {
		t.Fatalf("closed journal changed after rejected replay: %#v", closed)
	}
	if replayed, err := store.ResolveGoalSessionUncertainStopped(saved.Key(), testGoalSessionCompatibility()); err != nil || replayed != closed {
		t.Fatalf("exact closed replay = %#v, error = %v, want %#v", replayed, err, closed)
	}
}

func TestGoalSessionJournalCommitSurvivesLineageIndexRefreshFailure(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	original := applyFileSecurity
	denied := errors.New("derived index refresh denied")
	calls := 0
	applyFileSecurity = func(path string) error {
		calls++
		if calls == 3 {
			return denied
		}
		return original(path)
	}
	started, startErr := store.MarkGoalSessionLaunchStarted(saved.Key())
	applyFileSecurity = original
	if startErr != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v after journal commit, calls=%d", startErr, calls)
	}
	if started.LaunchState != GoalSessionLaunchStateLaunching || !started.NeedsReconciliation() {
		t.Fatalf("started journal = %#v", started)
	}
	status, err := store.CheckGoalSessionLineage(testGoalSessionIntent().LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() after failed refresh error = %v", err)
	}
	if !status.BlocksNewLaunch() || status.State != GoalSessionLineageStateUncertain {
		t.Fatalf("rebuilt status after failed refresh = %#v", status)
	}
}

func TestGoalSessionIntentGapClosesIdempotentlyBeforeNativeLaunch(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	closed, err := store.AbortGoalSessionLaunchBeforeNativeStart(saved.Key())
	if err != nil {
		t.Fatalf("AbortGoalSessionLaunchBeforeNativeStart() error = %v", err)
	}
	if closed.LaunchState != GoalSessionLaunchStateClosed || closed.SessionState != GoalSessionStateClosed || closed.NeedsReconciliation() {
		t.Fatalf("closed pre-launch gap = %#v", closed)
	}
	if replayed, err := store.AbortGoalSessionLaunchBeforeNativeStart(saved.Key()); err != nil || replayed != closed {
		t.Fatalf("pre-launch abort replay = %#v, error = %v, want %#v", replayed, err, closed)
	}
	status, err := store.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() after pre-launch abort error = %v", err)
	}
	if status.BlocksNewLaunch() {
		t.Fatalf("pre-launch abort left lineage blocked: %#v", status)
	}

	retry := intent
	retry.LaunchIntentID = "intent-after-prelaunch-abort"
	retry.LocalHandleID = "handle-after-prelaunch-abort"
	retry.RunID = "run-after-prelaunch-abort"
	retry.Generation = 2
	if _, err := store.SaveGoalSessionLaunchIntent(retry); err != nil {
		t.Fatalf("retry after pre-launch abort error = %v", err)
	}
}

func TestGoalSessionRecoveryDoesNotUpgradeUnstartedIntentToUncertain(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	recovered, err := store.MarkGoalSessionUncertain(saved.Key(), "daemon restart before launch boundary")
	if err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	if recovered.LaunchState != GoalSessionLaunchStateClosed || recovered.SessionState != GoalSessionStateClosed || recovered.NeedsReconciliation() || recovered.UncertainReason != "" {
		t.Fatalf("unstarted intent was upgraded instead of closed: %#v", recovered)
	}
	status, err := store.CheckGoalSessionLineage(intent.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() after recovery error = %v", err)
	}
	if status.BlocksNewLaunch() {
		t.Fatalf("recovery of unstarted intent left lineage blocked: %#v", status)
	}
}

func TestGoalSessionCompatibilityRejectsOwnerVersionAndWorkspaceMismatch(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native"}); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}

	cases := []struct {
		name   string
		want   error
		mutate func(*GoalSessionCompatibility)
	}{
		{name: "owner", want: ErrGoalSessionOwnerMismatch, mutate: func(value *GoalSessionCompatibility) { value.OwnerID = "other-owner" }},
		{name: "version", want: ErrGoalSessionVersionMismatch, mutate: func(value *GoalSessionCompatibility) { value.AdapterProtocolVersion++ }},
		{name: "workspace", want: ErrGoalSessionWorkspaceMismatch, mutate: func(value *GoalSessionCompatibility) { value.WorkspaceFingerprint = "sha256:other-workspace" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expected := testGoalSessionCompatibility()
			test.mutate(&expected)
			if err := store.CheckGoalSessionCompatibility(saved.Key(), expected); !errors.Is(err, test.want) {
				t.Fatalf("CheckGoalSessionCompatibility() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestGoalSessionControlProjectionOmitsRawNativeIdentity(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	attached, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{
		NativeSessionID:       "native-private-id",
		NativeSessionFilename: `C:\private\native.json`,
	})
	if err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}
	encoded, err := json.Marshal(attached.ControlProjection())
	if err != nil {
		t.Fatalf("Marshal(ControlProjection()) error = %v", err)
	}
	if strings.Contains(string(encoded), "native-private-id") || strings.Contains(string(encoded), "native.json") {
		t.Fatalf("control projection leaked local native identity: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"local_handle_id":"handle-1"`) {
		t.Fatalf("control projection omitted local_handle_id: %s", encoded)
	}
}

func TestCloseGoalSessionClearsNativeHandleAndReplaysAfterRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key(), time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{
		NativeSessionID:       "native-session-local-only",
		NativeSessionFilename: `C:\native\session.json`,
	}, testGoalSessionCompatibility()); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}

	closed, err := store.CloseGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("CloseGoalSession() error = %v", err)
	}
	if closed.SessionState != GoalSessionStateClosed || closed.LaunchState != GoalSessionLaunchStateClosed || closed.NativeSessionID != "" || closed.NativeSessionFilename != "" {
		t.Fatalf("closed journal = %#v", closed)
	}
	if closed.Intent() != intent || closed.Compatibility() != testGoalSessionCompatibility() {
		t.Fatalf("close did not retain audit identity: %#v", closed)
	}
	if replayed, err := store.CloseGoalSession(saved.Key()); err != nil || replayed != closed {
		t.Fatalf("closed session replay = %#v, error = %v, want %#v", replayed, err, closed)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "replacement-native"}); !errors.Is(err, ErrGoalSessionClosed) {
		t.Fatalf("PersistGoalSessionHandle() after close error = %v, want ErrGoalSessionClosed", err)
	}
	if replayed, err := store.SaveGoalSessionLaunchIntent(intent); err != nil || replayed != closed {
		t.Fatalf("closed intent replay = %#v, error = %v, want %#v", replayed, err, closed)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	replayed, err := restarted.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after close error = %v", err)
	}
	if replayed != closed {
		t.Fatalf("replayed closed journal = %#v, want %#v", replayed, closed)
	}
	if err := restarted.CheckGoalSessionCompatibility(saved.Key(), testGoalSessionCompatibility()); !errors.Is(err, ErrGoalSessionClosed) {
		t.Fatalf("CheckGoalSessionCompatibility() after close error = %v, want ErrGoalSessionClosed", err)
	}
}

func TestRetainedGoalSessionStopPreservesNativeAndControlIdentity(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	intent.LocalHandleID = "00000000-0000-4000-8000-000000000010"
	intent.BindingID = "00000000-0000-4000-8000-000000000011"
	intent.WorkspacePath = `C:\worktree\retained`
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-retained", NativeSessionFilename: `C:\native\retained.json`}); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000012"
	attachmentReceiptID := "00000000-0000-4000-8000-000000000015"
	if _, err := store.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, attachmentReceiptID, false); err != nil {
		t.Fatalf("PersistGoalSessionControlAttachment() error = %v", err)
	}
	attached, err := store.LoadGoalSession(saved.Key())
	if err != nil || attached.ControlAttachmentReceiptID != attachmentReceiptID || !attached.HasVerifiedControlAttachment() {
		t.Fatalf("persisted attachment receipt = %#v, error = %v", attached, err)
	}
	if _, err := store.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, "00000000-0000-4000-8000-000000000016", false); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("PersistGoalSessionControlAttachment() with changed receipt error = %v, want ErrGoalSessionConflict", err)
	}
	pending, err := store.MarkGoalSessionStoppedPending(saved.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionStoppedPending() error = %v", err)
	}
	if pending.SessionState != GoalSessionStateUnavailable || pending.LaunchState != GoalSessionLaunchStateAttached || pending.NativeSessionID != "native-retained" || pending.NativeSessionFilename == "" || pending.ControlSessionID != controlSessionID || pending.WorkspacePath != intent.WorkspacePath {
		t.Fatalf("stopped-pending journal = %#v", pending)
	}
	stopReceiptID := "00000000-0000-4000-8000-000000000014"
	certificate := GoalSessionStopCertificate{RunID: intent.RunID, Generation: intent.Generation, SessionID: controlSessionID, LocalHandleID: intent.LocalHandleID, BindingID: intent.BindingID, DeliveryDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReceiptID: stopReceiptID}
	available, err := store.MarkGoalSessionAvailable(saved.Key(), certificate)
	if err != nil {
		t.Fatalf("MarkGoalSessionAvailable() error = %v", err)
	}
	if available.SessionState != GoalSessionStateAvailable || available.NativeSessionID != "native-retained" || available.ControlSessionID != controlSessionID || available.StopCertificate == nil || available.StopCertificate.ReceiptID != stopReceiptID || available.BindingID != intent.BindingID {
		t.Fatalf("available retained journal = %#v", available)
	}
	if replayed, err := store.MarkGoalSessionAvailable(saved.Key(), certificate); err != nil || replayed.StopCertificate == nil || *replayed.StopCertificate != certificate || replayed.SessionState != GoalSessionStateAvailable {
		t.Fatalf("MarkGoalSessionAvailable() exact replay = %#v, error = %v", replayed, err)
	}
	staleCertificate := certificate
	staleCertificate.BindingID = "00000000-0000-4000-8000-000000000013"
	if _, err := store.MarkGoalSessionAvailable(saved.Key(), staleCertificate); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("MarkGoalSessionAvailable() with stale binding error = %v, want ErrGoalSessionConflict", err)
	}
	staleCertificate = certificate
	staleCertificate.ReceiptID = "00000000-0000-4000-8000-000000000016"
	if _, err := store.MarkGoalSessionAvailable(saved.Key(), staleCertificate); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("MarkGoalSessionAvailable() with changed receipt error = %v, want ErrGoalSessionConflict", err)
	}
	staleCertificate = certificate
	staleCertificate.DeliveryDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.MarkGoalSessionAvailable(saved.Key(), staleCertificate); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("MarkGoalSessionAvailable() with changed digest error = %v, want ErrGoalSessionConflict", err)
	}
}

func TestLegacyControlAttachmentJournalLoadsButCannotResumeOrRelease(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	intent := testGoalSessionIntent()
	intent.LocalHandleID = "00000000-0000-4000-8000-000000000010"
	intent.BindingID = "00000000-0000-4000-8000-000000000011"
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-retained"}); err != nil {
		t.Fatal(err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000012"
	attachmentReceiptID := "00000000-0000-4000-8000-000000000015"
	if _, err := store.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, attachmentReceiptID, false); err != nil {
		t.Fatal(err)
	}
	path := store.goalSessionPath(saved.Key())
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "control_attachment_receipt_id")
	delete(raw, "control_attachment_lineage")
	legacyEncoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacyEncoded, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart with legacy attachment journal: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	legacy, err := restarted.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.IsLegacyControlAttachment() || legacy.HasVerifiedControlAttachment() || legacy.ControlSessionID != controlSessionID || legacy.ControlAttachmentReceiptID != "" {
		t.Fatalf("legacy attachment journal = %#v", legacy)
	}
	if err := restarted.CheckGoalSessionCompatibility(saved.Key(), testGoalSessionCompatibility()); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("legacy CheckGoalSessionCompatibility() error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	if _, err := restarted.MarkGoalSessionStoppedPending(saved.Key()); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("legacy MarkGoalSessionStoppedPending() error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	certificate := GoalSessionStopCertificate{RunID: intent.RunID, Generation: intent.Generation, SessionID: controlSessionID, LocalHandleID: intent.LocalHandleID, BindingID: intent.BindingID, DeliveryDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReceiptID: "00000000-0000-4000-8000-000000000014"}
	if _, err := restarted.MarkGoalSessionAvailable(saved.Key(), certificate); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("legacy MarkGoalSessionAvailable() error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	upgraded, err := restarted.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, attachmentReceiptID, false)
	if err != nil || !upgraded.HasVerifiedControlAttachment() || upgraded.IsLegacyControlAttachment() {
		t.Fatalf("upgrade legacy attachment = %#v, error = %v", upgraded, err)
	}
	if _, err := restarted.MarkGoalSessionStoppedPending(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionStoppedPending() after upgrade error = %v", err)
	}
}

func TestRetainedGoalSessionStopCertificateSurvivesStoreRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	intent := testGoalSessionIntent()
	intent.LocalHandleID = "00000000-0000-4000-8000-000000000010"
	intent.BindingID = "00000000-0000-4000-8000-000000000011"
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-retained"}); err != nil {
		t.Fatal(err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000012"
	if _, err := store.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, "00000000-0000-4000-8000-000000000015", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionStoppedPending(saved.Key()); err != nil {
		t.Fatal(err)
	}
	certificate := GoalSessionStopCertificate{RunID: intent.RunID, Generation: intent.Generation, SessionID: controlSessionID, LocalHandleID: intent.LocalHandleID, BindingID: intent.BindingID, DeliveryDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReceiptID: "00000000-0000-4000-8000-000000000014"}
	if _, err := store.MarkGoalSessionAvailable(saved.Key(), certificate); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadGoalSession(saved.Key())
	if err != nil || loaded.StopCertificate == nil || *loaded.StopCertificate != certificate || !loaded.HasVerifiedControlAttachment() || loaded.SessionState != GoalSessionStateAvailable {
		t.Fatalf("restarted retained stop certificate = %#v, error = %v", loaded, err)
	}
}

func TestLoadGoalSessionByControlSessionIDFindsRetainedSessionAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := mustAvailableRetainedGoalSession(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadGoalSessionByControlSessionID(source.ControlSessionID)
	if err != nil {
		t.Fatalf("LoadGoalSessionByControlSessionID() error = %v", err)
	}
	if loaded.Key() != source.Key() || loaded.NativeSessionID != source.NativeSessionID || loaded.StopCertificate == nil {
		t.Fatalf("loaded retained session = %#v", loaded)
	}
}

func TestRebindGoalSessionForResumeConsumesExactStoppedAttachment(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	owner := source.WorkspaceOwnerRunKey
	lineage := *source.ControlAttachmentLineage

	rebind := testGoalSessionResumeRebind(certificate)
	rebound, err := store.RebindGoalSessionForResume(source.Key(), rebind)
	if err != nil {
		t.Fatalf("RebindGoalSessionForResume() error = %v", err)
	}
	if rebound.RunID != rebind.RunID || rebound.Generation != rebind.Generation || rebound.TaskID != rebind.TaskID ||
		rebound.AdmissionID != rebind.AdmissionID || rebound.BindingID != rebind.BindingID || rebound.SessionMode != GoalSessionModeResume ||
		rebound.SessionState != GoalSessionStateBusy || rebound.StopCertificate != nil || rebound.ControlAttachmentReceiptID != "" {
		t.Fatalf("rebound journal current execution = %#v", rebound)
	}
	if rebound.NativeSessionID != source.NativeSessionID || rebound.NativeSessionFilename != source.NativeSessionFilename ||
		rebound.WorkspacePath != source.WorkspacePath || rebound.WorkspaceOwnerRunKey != owner ||
		rebound.ControlSessionID != source.ControlSessionID || rebound.ControlAttachmentLineage == nil || *rebound.ControlAttachmentLineage != lineage {
		t.Fatalf("rebind did not preserve retained identity/lineage: %#v", rebound)
	}
	if rebound.HasVerifiedControlAttachment() {
		t.Fatalf("rebind treated predecessor attachment as verification for new Run: %#v", rebound)
	}
	oldLineage, err := store.CheckGoalSessionLineage(source.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() for source task error = %v", err)
	}
	if oldLineage.BlocksNewLaunch() {
		t.Fatalf("source task lineage remained blocked after rebind: %#v", oldLineage)
	}
	newLineage, err := store.CheckGoalSessionLineage(rebound.LineageKey())
	if err != nil {
		t.Fatalf("CheckGoalSessionLineage() for resume task error = %v", err)
	}
	if !newLineage.BlocksNewLaunch() || newLineage.State != GoalSessionLineageStateAttached {
		t.Fatalf("resume task lineage was not durably blocked: %#v", newLineage)
	}

	replayed, err := store.RebindGoalSessionForResume(source.Key(), rebind)
	if err != nil {
		t.Fatalf("exact RebindGoalSessionForResume() replay error = %v", err)
	}
	if replayed.RunID != rebound.RunID || replayed.Generation != rebound.Generation || replayed.TaskID != rebound.TaskID ||
		replayed.BindingID != rebound.BindingID || replayed.ResumeSourceStopCertificate == nil || *replayed.ResumeSourceStopCertificate != certificate {
		t.Fatalf("exact rebind replay = %#v", replayed)
	}
	changedCompatibility := rebind
	changedCompatibility.Compatibility.HarnessVersion = "9.9.9"
	if _, err := store.RebindGoalSessionForResume(source.Key(), changedCompatibility); !errors.Is(err, ErrGoalSessionVersionMismatch) {
		t.Fatalf("rebind replay with changed compatibility error = %v, want ErrGoalSessionVersionMismatch", err)
	}
	latePredecessor, err := store.MarkGoalSessionAvailable(source.Key(), certificate)
	if err != nil {
		t.Fatalf("MarkGoalSessionAvailable() for consumed predecessor error = %v", err)
	}
	if latePredecessor.RunID != rebind.RunID || latePredecessor.Generation != rebind.Generation ||
		latePredecessor.BindingID != rebind.BindingID || latePredecessor.SessionState != GoalSessionStateBusy || latePredecessor.StopCertificate != nil {
		t.Fatalf("late predecessor receipt changed rebound session: %#v", latePredecessor)
	}
	stalePredecessor := certificate
	stalePredecessor.ReceiptID = "00000000-0000-4000-8000-000000000027"
	if _, err := store.MarkGoalSessionAvailable(source.Key(), stalePredecessor); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("stale consumed predecessor receipt error = %v, want ErrGoalSessionConflict", err)
	}

	newAttachmentReceiptID := "00000000-0000-4000-8000-000000000021"
	reattached, err := store.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, rebind.BindingID, newAttachmentReceiptID, false)
	if err != nil {
		t.Fatalf("PersistGoalSessionControlAttachment() after rebind error = %v", err)
	}
	if !reattached.HasVerifiedControlAttachment() || reattached.ControlAttachmentReceiptID != newAttachmentReceiptID || reattached.ControlAttachmentLineage == nil ||
		reattached.ControlAttachmentLineage.Original != lineage.Original || reattached.ControlAttachmentLineage.Current.RunID != rebind.RunID ||
		reattached.ControlAttachmentLineage.Current.BindingID != rebind.BindingID || reattached.ControlAttachmentLineage.Current.ReceiptID != newAttachmentReceiptID {
		t.Fatalf("reattached resume journal = %#v", reattached)
	}
	if _, err := store.MarkGoalSessionResumeStartAttempted(source.Key()); err != nil {
		t.Fatalf("MarkGoalSessionResumeStartAttempted() error = %v", err)
	}
	if _, err := store.MarkGoalSessionStoppedPending(source.Key()); err != nil {
		t.Fatalf("MarkGoalSessionStoppedPending() for resumed Run error = %v", err)
	}
	secondCertificate := GoalSessionStopCertificate{
		RunID: rebind.RunID, Generation: rebind.Generation, SessionID: source.ControlSessionID, LocalHandleID: source.LocalHandleID,
		BindingID: rebind.BindingID, DeliveryDigest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ReceiptID: "00000000-0000-4000-8000-000000000025",
	}
	availableAgain, err := store.MarkGoalSessionAvailable(source.Key(), secondCertificate)
	if err != nil {
		t.Fatalf("MarkGoalSessionAvailable() for resumed Run error = %v", err)
	}
	if availableAgain.ResumeSourceStopCertificate != nil {
		t.Fatalf("available resumed Run retained consumed source certificate: %#v", availableAgain)
	}
	secondRebind := testGoalSessionResumeRebind(secondCertificate)
	secondRebind.RunID = "run-3"
	secondRebind.Generation = 1
	secondRebind.TaskID = "task-3"
	secondRebind.AdmissionID = "admission-3"
	secondRebind.BindingID = "00000000-0000-4000-8000-000000000026"
	if _, err := store.RebindGoalSessionForResume(source.Key(), secondRebind); err != nil {
		t.Fatalf("second RebindGoalSessionForResume() error = %v", err)
	}
	if _, err := store.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, secondRebind.BindingID, "00000000-0000-4000-8000-000000000029", false); err != nil {
		t.Fatalf("PersistGoalSessionControlAttachment() for second rebind error = %v", err)
	}
	if _, err := store.MarkGoalSessionResumeStartAttempted(source.Key()); err != nil {
		t.Fatalf("MarkGoalSessionResumeStartAttempted() for second rebind error = %v", err)
	}
	if _, err := store.MarkGoalSessionStoppedPending(source.Key()); err != nil {
		t.Fatalf("MarkGoalSessionStoppedPending() for second rebind error = %v", err)
	}
	thirdCertificate := GoalSessionStopCertificate{
		RunID: secondRebind.RunID, Generation: secondRebind.Generation, SessionID: source.ControlSessionID, LocalHandleID: source.LocalHandleID,
		BindingID: secondRebind.BindingID, DeliveryDigest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ReceiptID: "00000000-0000-4000-8000-000000000030",
	}
	if _, err := store.MarkGoalSessionAvailable(source.Key(), thirdCertificate); err != nil {
		t.Fatalf("MarkGoalSessionAvailable() for second rebind error = %v", err)
	}
	for _, target := range []struct {
		name   string
		mutate func(*GoalSessionResumeRebind)
	}{
		{name: "historical Run", mutate: func(value *GoalSessionResumeRebind) { value.RunID = certificate.RunID }},
		{name: "historical binding", mutate: func(value *GoalSessionResumeRebind) { value.BindingID = certificate.BindingID }},
	} {
		t.Run(target.name, func(t *testing.T) {
			candidate := testGoalSessionResumeRebind(thirdCertificate)
			target.mutate(&candidate)
			if _, err := store.RebindGoalSessionForResume(source.Key(), candidate); !errors.Is(err, ErrGoalSessionConflict) {
				t.Fatalf("historical resume target error = %v, want ErrGoalSessionConflict", err)
			}
		})
	}
	lateFirst, err := store.MarkGoalSessionAvailable(source.Key(), certificate)
	if err != nil {
		t.Fatalf("late first-cycle stop receipt after second rebind error = %v", err)
	}
	if lateFirst.RunID != secondRebind.RunID || lateFirst.Generation != secondRebind.Generation || lateFirst.BindingID != secondRebind.BindingID ||
		lateFirst.SessionState != GoalSessionStateAvailable || lateFirst.ResumeStartPending || lateFirst.StopCertificate == nil || *lateFirst.StopCertificate != thirdCertificate {
		t.Fatalf("late first-cycle receipt changed second rebind: %#v", lateFirst)
	}
}

func TestRebindGoalSessionForResumeRejectsStaleCertificateAndCompetingCAS(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(certificate)

	stale := rebind
	stale.ExactStopCertificate.DeliveryDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.RebindGoalSessionForResume(source.Key(), stale); !errors.Is(err, ErrGoalSessionConflict) {
		t.Fatalf("stale certificate rebind error = %v, want ErrGoalSessionConflict", err)
	}
	before, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if before.StopCertificate == nil || *before.StopCertificate != certificate || before.SessionState != GoalSessionStateAvailable {
		t.Fatalf("stale rebind mutated source: %#v", before)
	}
	for _, target := range []struct {
		name   string
		mutate func(*GoalSessionResumeRebind)
	}{
		{name: "same Run", mutate: func(value *GoalSessionResumeRebind) { value.RunID = certificate.RunID }},
		{name: "same binding", mutate: func(value *GoalSessionResumeRebind) { value.BindingID = certificate.BindingID }},
	} {
		t.Run(target.name, func(t *testing.T) {
			candidate := rebind
			target.mutate(&candidate)
			if _, err := store.RebindGoalSessionForResume(source.Key(), candidate); !errors.Is(err, ErrGoalSessionConflict) {
				t.Fatalf("predecessor-identical target error = %v, want ErrGoalSessionConflict", err)
			}
		})
	}

	competing := rebind
	competing.RunID = "run-3"
	competing.Generation = 3
	competing.TaskID = "task-3"
	competing.AdmissionID = "admission-3"
	competing.BindingID = "00000000-0000-4000-8000-000000000022"
	start := make(chan struct{})
	errorsByTarget := make(chan error, 2)
	for _, request := range []GoalSessionResumeRebind{rebind, competing} {
		request := request
		go func() {
			<-start
			_, err := store.RebindGoalSessionForResume(source.Key(), request)
			errorsByTarget <- err
		}()
	}
	close(start)
	first, second := <-errorsByTarget, <-errorsByTarget
	if !((first == nil && errors.Is(second, ErrGoalSessionConflict)) || (second == nil && errors.Is(first, ErrGoalSessionConflict))) {
		t.Fatalf("competing rebind errors = %v, %v; want one success and one conflict", first, second)
	}
}

func TestRebindGoalSessionForResumeRequiresExactCompatibility(t *testing.T) {
	tests := []struct {
		name   string
		want   error
		mutate func(*GoalSessionCompatibility)
	}{
		{name: "machine", want: ErrGoalSessionOwnerMismatch, mutate: func(value *GoalSessionCompatibility) { value.MachineID = "machine-2" }},
		{name: "runtime", want: ErrGoalSessionOwnerMismatch, mutate: func(value *GoalSessionCompatibility) { value.RuntimeID = "runtime-2" }},
		{name: "native version", want: ErrGoalSessionVersionMismatch, mutate: func(value *GoalSessionCompatibility) { value.HarnessVersion = "9.9.9" }},
		{name: "adapter version", want: ErrGoalSessionVersionMismatch, mutate: func(value *GoalSessionCompatibility) { value.AdapterProtocolVersion++ }},
		{name: "workspace fingerprint", want: ErrGoalSessionWorkspaceMismatch, mutate: func(value *GoalSessionCompatibility) { value.WorkspaceFingerprint = "sha256:workspace-two" }},
		{name: "repository", want: ErrGoalSessionWorkspaceMismatch, mutate: func(value *GoalSessionCompatibility) {
			value.RepositoryResourceID = "00000000-0000-4000-8000-000000000023"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			source, certificate := mustAvailableRetainedGoalSession(t, store)
			rebind := testGoalSessionResumeRebind(certificate)
			test.mutate(&rebind.Compatibility)
			if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); !errors.Is(err, test.want) {
				t.Fatalf("RebindGoalSessionForResume() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRebindGoalSessionForResumeCannotBypassCurrentAttachmentBeforeAvailability(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(certificate)
	if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionResumeStartAttempted(source.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetGoalSessionState(source.Key(), GoalSessionStateUnavailable); err != nil {
		t.Fatalf("SetGoalSessionState(unavailable) error = %v", err)
	}
	forged := GoalSessionStopCertificate{
		RunID: rebind.RunID, Generation: rebind.Generation, SessionID: source.ControlSessionID,
		LocalHandleID: source.LocalHandleID, BindingID: rebind.BindingID,
		DeliveryDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ReceiptID:      "00000000-0000-4000-8000-000000000024",
	}
	if _, err := store.MarkGoalSessionAvailable(source.Key(), forged); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("MarkGoalSessionAvailable() without current attachment error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	journal, err := store.LoadGoalSession(source.Key())
	if err != nil || journal.StopCertificate != nil || journal.SessionState != GoalSessionStateUnavailable {
		t.Fatalf("availability bypass changed journal = %#v, error = %v", journal, err)
	}
}

func TestRebindGoalSessionForResumeFailsClosedForLegacyOrAfterRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	path := store.goalSessionPath(source.Key())
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "workspace_owner_run_key")
	delete(raw, "control_attachment_lineage")
	legacy, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if _, err := restarted.RebindGoalSessionForResume(source.Key(), testGoalSessionResumeRebind(certificate)); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("legacy RebindGoalSessionForResume() error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
}

func TestRebindGoalSessionForResumeSurvivesStoreRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	rebind := testGoalSessionResumeRebind(certificate)
	rebind.Compatibility.RuntimeEpoch++
	rebound, err := restarted.RebindGoalSessionForResume(source.Key(), rebind)
	if err != nil {
		t.Fatalf("RebindGoalSessionForResume() after restart error = %v", err)
	}
	if rebound.WorkspaceOwnerRunKey != source.WorkspaceOwnerRunKey || rebound.NativeSessionID != source.NativeSessionID ||
		rebound.RunID != "run-2" || rebound.Generation != 1 || rebound.StopCertificate != nil {
		t.Fatalf("rebound journal after restart = %#v", rebound)
	}
}

func TestRebindGoalSessionPreStartPhaseSurvivesAttachmentReadbackAndRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(certificate)
	rebound, err := store.RebindGoalSessionForResume(source.Key(), rebind)
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.ResumeStartPending {
		t.Fatalf("rebind did not persist pre-start phase: %#v", rebound)
	}
	// This represents a recovery/readback that obtains the new Control attach
	// receipt before pi.Start is permitted. It must not consume the pre-start
	// recovery fact.
	if _, err := store.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, rebind.BindingID, "00000000-0000-4000-8000-000000000028", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	pending, err := restarted.LoadGoalSession(source.Key())
	if err != nil || !pending.ResumeStartPending || !pending.HasVerifiedControlAttachment() || pending.StopCertificate != nil {
		t.Fatalf("pre-start phase after readback/restart = %#v, error = %v", pending, err)
	}
	attempted, err := restarted.MarkGoalSessionResumeStartAttempted(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if attempted.ResumeStartPending {
		t.Fatalf("native Start boundary did not clear pre-start phase: %#v", attempted)
	}
}

func TestRebindGoalSessionKnownNativeStopPrecedesCurrentAttachment(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(predecessor)
	if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.MarkGoalSessionResumeNativeStopped(source.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionResumeNativeStopped() before attach error = %v", err)
	}
	if stopped.SessionState != GoalSessionStateUnavailable || stopped.ResumeStartPending || !stopped.HasKnownRetainedResumeNativeStop() ||
		stopped.NativeSessionID != source.NativeSessionID || stopped.ControlSessionID != source.ControlSessionID || stopped.ControlAttachmentReceiptID != "" {
		t.Fatalf("known native stop before attachment = %#v", stopped)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	persisted, err := restarted.LoadGoalSession(source.Key())
	if err != nil || !persisted.HasKnownRetainedResumeNativeStop() || persisted.ResumeStartPending {
		t.Fatalf("known native stop after restart = %#v, error = %v", persisted, err)
	}
	currentCertificate := GoalSessionStopCertificate{
		RunID: rebind.RunID, Generation: rebind.Generation, SessionID: source.ControlSessionID, LocalHandleID: source.LocalHandleID,
		BindingID: rebind.BindingID, DeliveryDigest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ReceiptID: "00000000-0000-4000-8000-000000000031",
	}
	if _, err := restarted.MarkGoalSessionAvailable(source.Key(), currentCertificate); !errors.Is(err, ErrGoalSessionCompatibilityIncomplete) {
		t.Fatalf("MarkGoalSessionAvailable() before current attach error = %v, want ErrGoalSessionCompatibilityIncomplete", err)
	}
	if _, err := restarted.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, rebind.BindingID, "00000000-0000-4000-8000-000000000032", false); err != nil {
		t.Fatal(err)
	}
	available, err := restarted.MarkGoalSessionAvailable(source.Key(), currentCertificate)
	if err != nil {
		t.Fatal(err)
	}
	if available.HasKnownRetainedResumeNativeStop() || available.ResumeSourceStopCertificate != nil || available.StopCertificate == nil || *available.StopCertificate != currentCertificate {
		t.Fatalf("current Control stop receipt did not resolve known native stop: %#v", available)
	}
}

func TestMarkGoalSessionResumeRejectedSurvivesRestartAndStopDelivery(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(predecessor)
	if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); err != nil {
		t.Fatal(err)
	}
	rejected, err := store.MarkGoalSessionResumeRejected(source.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionResumeRejected() error = %v", err)
	}
	if rejected.SessionState != GoalSessionStateUnavailable || rejected.LaunchState != GoalSessionLaunchStateAttached ||
		rejected.ResumeStartPending || !rejected.ResumeNativeStopKnown || rejected.ResumeTerminalReason != "resume_rejected" ||
		!rejected.HasKnownRetainedResumeNativeStop() || rejected.StopCertificate != nil || rejected.ResumeSourceStopCertificate == nil {
		t.Fatalf("rejected resume journal = %#v", rejected)
	}
	replayed, err := store.MarkGoalSessionResumeRejected(source.Key())
	if err != nil {
		t.Fatalf("idempotent MarkGoalSessionResumeRejected() error = %v", err)
	}
	if replayed.UpdatedAt != rejected.UpdatedAt || replayed.ResumeTerminalReason != rejected.ResumeTerminalReason ||
		!replayed.HasKnownRetainedResumeNativeStop() || replayed.ResumeSourceStopCertificate == nil || rejected.ResumeSourceStopCertificate == nil ||
		*replayed.ResumeSourceStopCertificate != *rejected.ResumeSourceStopCertificate {
		t.Fatalf("idempotent rejected resume = %#v, want %#v", replayed, rejected)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	persisted, err := restarted.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after rejected resume restart error = %v", err)
	}
	if persisted.ResumeTerminalReason != "resume_rejected" || !persisted.HasKnownRetainedResumeNativeStop() || persisted.ResumeStartPending {
		t.Fatalf("rejected resume classification was not durable: %#v", persisted)
	}
	if _, err := restarted.MarkGoalSessionResumeNativeStopped(source.Key()); err != nil {
		t.Fatalf("idempotent MarkGoalSessionResumeNativeStopped() error = %v", err)
	}
	persisted, err = restarted.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ResumeTerminalReason != "resume_rejected" {
		t.Fatalf("native-stop replay cleared rejected classification: %#v", persisted)
	}

	if _, err := restarted.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, rebind.BindingID, "00000000-0000-4000-8000-000000000035", false); err != nil {
		t.Fatal(err)
	}
	stopped, err := restarted.MarkGoalSessionStoppedPending(source.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionStoppedPending() after rejected resume error = %v", err)
	}
	if stopped.ResumeTerminalReason != "resume_rejected" || !stopped.HasKnownRetainedResumeNativeStop() {
		t.Fatalf("generic stop accounting weakened rejected classification: %#v", stopped)
	}

	currentCertificate := GoalSessionStopCertificate{
		RunID: rebind.RunID, Generation: rebind.Generation, SessionID: source.ControlSessionID, LocalHandleID: source.LocalHandleID,
		BindingID: rebind.BindingID, DeliveryDigest: "abababababababababababababababababababababababababababababababab",
		ReceiptID: "00000000-0000-4000-8000-000000000036",
	}
	available, err := restarted.MarkGoalSessionAvailable(source.Key(), currentCertificate)
	if err != nil {
		t.Fatalf("MarkGoalSessionAvailable() after rejected resume error = %v", err)
	}
	if available.ResumeTerminalReason != "" || available.ResumeNativeStopKnown || available.ResumeSourceStopCertificate != nil ||
		available.StopCertificate == nil || *available.StopCertificate != currentCertificate {
		t.Fatalf("current stop receipt did not clear rejected classification: %#v", available)
	}
}

func TestMarkGoalSessionResumeRejectedUpgradesUnclassifiedKnownStop(t *testing.T) {
	store := mustStore(t)
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	if _, err := store.RebindGoalSessionForResume(source.Key(), testGoalSessionResumeRebind(predecessor)); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.MarkGoalSessionResumeNativeStopped(source.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionResumeNativeStopped() error = %v", err)
	}
	if !stopped.HasKnownRetainedResumeNativeStop() || stopped.ResumeTerminalReason != "" {
		t.Fatalf("unclassified known stop = %#v", stopped)
	}
	rejected, err := store.MarkGoalSessionResumeRejected(source.Key())
	if err != nil {
		t.Fatalf("MarkGoalSessionResumeRejected() upgrade error = %v", err)
	}
	if !rejected.HasKnownRetainedResumeNativeStop() || rejected.ResumeTerminalReason != "resume_rejected" {
		t.Fatalf("known stop was not upgraded to rejected classification: %#v", rejected)
	}
}

func TestGoalSessionResumeTerminalReasonJournalInvariant(t *testing.T) {
	store := mustStore(t)
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	if _, err := store.RebindGoalSessionForResume(source.Key(), testGoalSessionResumeRebind(predecessor)); err != nil {
		t.Fatal(err)
	}
	rejected, err := store.MarkGoalSessionResumeRejected(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*GoalSessionJournal)
	}{
		{name: "unknown reason", mutate: func(journal *GoalSessionJournal) { journal.ResumeTerminalReason = "other" }},
		{name: "without native stop witness", mutate: func(journal *GoalSessionJournal) { journal.ResumeNativeStopKnown = false }},
		{name: "while busy", mutate: func(journal *GoalSessionJournal) { journal.SessionState = GoalSessionStateBusy }},
		{name: "with stop certificate", mutate: func(journal *GoalSessionJournal) {
			certificate := predecessor
			journal.StopCertificate = &certificate
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			journal := rejected
			test.mutate(&journal)
			if err := validateGoalSessionJournal(journal); err == nil {
				t.Fatal("validateGoalSessionJournal() accepted invalid rejected-resume classification")
			}
		})
	}
}

func TestMarkGoalSessionResumeNativeStoppedWriteFailureLeavesPreStartState(t *testing.T) {
	store := mustStore(t)
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	if _, err := store.RebindGoalSessionForResume(source.Key(), testGoalSessionResumeRebind(predecessor)); err != nil {
		t.Fatal(err)
	}
	original := applyFileSecurity
	applyFileSecurity = func(string) error { return errors.New("injected state write failure") }
	t.Cleanup(func() { applyFileSecurity = original })
	if _, err := store.MarkGoalSessionResumeNativeStopped(source.Key()); err == nil {
		t.Fatal("MarkGoalSessionResumeNativeStopped() succeeded despite injected write failure")
	}
	journal, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if journal.SessionState != GoalSessionStateBusy || !journal.ResumeStartPending || journal.ResumeNativeStopKnown {
		t.Fatalf("failed native-stop mutation changed journal: %#v", journal)
	}
}

func TestLegacyResumeSourceWithoutHistoryPreservesLateAckAndPromotesHistory(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	source, predecessor := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(predecessor)
	if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionControlAttachment(source.Key(), source.ControlSessionID, rebind.BindingID, "00000000-0000-4000-8000-000000000033", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionResumeNativeStopped(source.Key()); err != nil {
		t.Fatal(err)
	}
	path := store.goalSessionPath(source.Key())
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "consumed_stop_certificates")
	legacy, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if _, err := restarted.MarkGoalSessionAvailable(source.Key(), predecessor); err != nil {
		t.Fatalf("legacy predecessor late ACK error = %v", err)
	}
	currentCertificate := GoalSessionStopCertificate{
		RunID: rebind.RunID, Generation: rebind.Generation, SessionID: source.ControlSessionID, LocalHandleID: source.LocalHandleID,
		BindingID: rebind.BindingID, DeliveryDigest: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		ReceiptID: "00000000-0000-4000-8000-000000000034",
	}
	resolved, err := restarted.MarkGoalSessionAvailable(source.Key(), currentCertificate)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ConsumedStopCertificates == nil || !resolved.ConsumedStopCertificates.containsExact(predecessor) || resolved.ResumeSourceStopCertificate != nil {
		t.Fatalf("legacy resume source did not promote consumed history: %#v", resolved)
	}
	if _, err := restarted.MarkGoalSessionAvailable(source.Key(), predecessor); err != nil {
		t.Fatalf("promoted predecessor late ACK error = %v", err)
	}
}

func mustAvailableRetainedGoalSession(t *testing.T, store *Store) (GoalSessionJournal, GoalSessionStopCertificate) {
	t.Helper()
	intent := testGoalSessionIntent()
	intent.LocalHandleID = "00000000-0000-4000-8000-000000000010"
	intent.BindingID = "00000000-0000-4000-8000-000000000011"
	intent.WorkspacePath = `C:\worktree\retained`
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-retained", NativeSessionFilename: `C:\native\retained.json`}); err != nil {
		t.Fatal(err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000012"
	attachmentReceiptID := "00000000-0000-4000-8000-000000000015"
	if _, err := store.PersistGoalSessionControlAttachment(saved.Key(), controlSessionID, intent.BindingID, attachmentReceiptID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionStoppedPending(saved.Key()); err != nil {
		t.Fatal(err)
	}
	certificate := GoalSessionStopCertificate{
		RunID: intent.RunID, Generation: intent.Generation, SessionID: controlSessionID, LocalHandleID: intent.LocalHandleID,
		BindingID: intent.BindingID, DeliveryDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReceiptID: "00000000-0000-4000-8000-000000000014",
	}
	available, err := store.MarkGoalSessionAvailable(saved.Key(), certificate)
	if err != nil {
		t.Fatal(err)
	}
	return available, certificate
}

func testGoalSessionResumeRebind(certificate GoalSessionStopCertificate) GoalSessionResumeRebind {
	return GoalSessionResumeRebind{
		RunID: "run-2", Generation: 1, TaskID: "task-2", AdmissionID: "admission-2",
		BindingID: "00000000-0000-4000-8000-000000000020", ExactStopCertificate: certificate,
		Compatibility: testGoalSessionCompatibility(),
	}
}

func TestCloseGoalSessionRejectsUncertainJournalAndPreservesIt(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key(), time.Date(2026, 9, 9, 3, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.MarkGoalSessionUncertain(saved.Key(), "native process status is unavailable"); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	before, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() error = %v", err)
	}
	if _, err := store.CloseGoalSession(saved.Key()); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("CloseGoalSession() error = %v, want ErrGoalSessionUncertain", err)
	}
	after, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after failed close error = %v", err)
	}
	if after != before || !after.NeedsReconciliation() || after.LaunchState != GoalSessionLaunchStateUncertain {
		t.Fatalf("uncertain journal changed by close: before=%#v after=%#v", before, after)
	}
	if err := store.DeleteGoalSession(saved.Key()); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("DeleteGoalSession() error = %v, want ErrGoalSessionUncertain", err)
	}
}

func TestMarkGoalSessionUncertainClearsAttachedHandleAndSurvivesReplay(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key(), time.Date(2026, 9, 9, 3, 2, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{
		NativeSessionID:       "native-private-id",
		NativeSessionFilename: `C:\private\native.json`,
	}, testGoalSessionCompatibility()); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}

	uncertain, err := store.MarkGoalSessionUncertain(saved.Key(), "native process status is unavailable")
	if err != nil {
		t.Fatalf("MarkGoalSessionUncertain() error = %v", err)
	}
	if uncertain.LaunchState != GoalSessionLaunchStateUncertain || uncertain.SessionState != GoalSessionStateUnavailable || uncertain.NativeSessionID != "" || uncertain.NativeSessionFilename != "" || !uncertain.NeedsReconciliation() {
		t.Fatalf("uncertain journal = %#v", uncertain)
	}
	if uncertain.Intent() != intent || uncertain.Compatibility() != testGoalSessionCompatibility() {
		t.Fatalf("uncertain journal did not retain audit identity: %#v", uncertain)
	}
	if retried, err := store.MarkGoalSessionUncertain(saved.Key(), "native process status is unavailable"); err != nil || retried.LaunchState != GoalSessionLaunchStateUncertain || retried.NativeSessionID != "" || retried.NativeSessionFilename != "" {
		t.Fatalf("uncertain retry = %#v, error = %v", retried, err)
	}
	if replayed, err := store.SaveGoalSessionLaunchIntent(intent); !errors.Is(err, ErrGoalSessionUncertain) || replayed.NativeSessionID != "" || replayed.NativeSessionFilename != "" {
		t.Fatalf("uncertain intent replay = %#v, error = %v", replayed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatalf("restart New() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	replayed, err := restarted.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after restart error = %v", err)
	}
	if replayed.LaunchState != GoalSessionLaunchStateUncertain || replayed.NativeSessionID != "" || replayed.NativeSessionFilename != "" || !replayed.NeedsReconciliation() {
		t.Fatalf("replayed uncertain journal = %#v", replayed)
	}
	if err := restarted.DeleteGoalSession(saved.Key()); !errors.Is(err, ErrGoalSessionUncertain) {
		t.Fatalf("DeleteGoalSession() after restart error = %v, want ErrGoalSessionUncertain", err)
	}
}

func TestSetGoalSessionStateClosedUsesSafeClose(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-private-id"}); err != nil {
		t.Fatalf("PersistGoalSessionHandle() error = %v", err)
	}
	closed, err := store.SetGoalSessionState(saved.Key(), GoalSessionStateClosed)
	if err != nil {
		t.Fatalf("SetGoalSessionState(closed) error = %v", err)
	}
	if closed.NativeSessionID != "" || closed.LaunchState != GoalSessionLaunchStateClosed {
		t.Fatalf("SetGoalSessionState(closed) retained native identity: %#v", closed)
	}
}

func TestSetGoalSessionStateRejectsAvailableWithoutCertificate(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(saved.Key(), GoalSessionHandle{NativeSessionID: "native-private-id"}); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetGoalSessionState(saved.Key(), GoalSessionStateAvailable); err == nil {
		t.Fatal("SetGoalSessionState(available) succeeded without a certificate")
	}
	after, err := store.LoadGoalSession(saved.Key())
	if err != nil || after != before {
		t.Fatalf("available setter bypass changed journal: before=%#v after=%#v error=%v", before, after, err)
	}
}

func TestGoalSessionAtomicWriteFailureLeavesPriorRecord(t *testing.T) {
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	original := applyFileSecurity
	denied := errors.New("security setup denied")
	applyFileSecurity = func(string) error { return denied }
	t.Cleanup(func() { applyFileSecurity = original })
	if _, err := store.MarkGoalSessionLaunchStarted(saved.Key()); err == nil {
		t.Fatal("MarkGoalSessionLaunchStarted() succeeded when atomic write was denied")
	}
	loaded, err := store.LoadGoalSession(saved.Key())
	if err != nil {
		t.Fatalf("LoadGoalSession() after failed write error = %v", err)
	}
	if loaded.LaunchState != GoalSessionLaunchStateIntent || loaded.LaunchAttempted {
		t.Fatalf("failed atomic write mutated prior record: %#v", loaded)
	}
}

func testGoalSessionIntent() GoalSessionLaunchIntent {
	return GoalSessionLaunchIntent{
		LaunchIntentID:         "intent-1",
		GoalID:                 "goal-1",
		GoalRevision:           2,
		WorkItemID:             "work-item-1",
		TaskID:                 "task-1",
		RunID:                  "run-1",
		Generation:             1,
		AdmissionID:            "admission-1",
		LocalHandleID:          "handle-1",
		OwnerID:                "owner-1",
		MachineID:              "machine-1",
		RuntimeID:              "runtime-1",
		RuntimeEpoch:           7,
		DaemonInstanceID:       "daemon-1",
		HarnessKind:            "codex",
		HarnessVersion:         "1.2.3",
		AdapterVersion:         "adapter-4",
		AdapterProtocolVersion: 3,
		WorkspaceFingerprint:   "sha256:workspace-one",
		WorkspaceOwnerRunKey:   WorkspaceOwnerRunKey{RunID: "run-1", Generation: 1},
		RepositoryResourceID:   "00000000-0000-4000-8000-000000000017",
		SessionMode:            GoalSessionModeFresh,
	}
}

func testGoalSessionCompatibility() GoalSessionCompatibility {
	intent := testGoalSessionIntent()
	return GoalSessionCompatibility{
		OwnerID:                intent.OwnerID,
		MachineID:              intent.MachineID,
		RuntimeID:              intent.RuntimeID,
		RuntimeEpoch:           intent.RuntimeEpoch,
		DaemonInstanceID:       intent.DaemonInstanceID,
		HarnessKind:            intent.HarnessKind,
		HarnessVersion:         intent.HarnessVersion,
		AdapterVersion:         intent.AdapterVersion,
		AdapterProtocolVersion: intent.AdapterProtocolVersion,
		WorkspaceFingerprint:   intent.WorkspaceFingerprint,
		RepositoryResourceID:   intent.RepositoryResourceID,
	}
}
