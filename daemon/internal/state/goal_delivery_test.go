package state

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestGoalSessionAttachDeliveryIsDurableBeforeLaunchAndReadyAfterHandle(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDelivery()
	queued, err := store.QueueGoalSessionAttach(key, payload)
	if err != nil {
		t.Fatalf("QueueGoalSessionAttach() error = %v", err)
	}
	if len(queued.PendingGoalDeliveries) != 1 || queued.PendingGoalDeliveries[0].Ready {
		t.Fatalf("queued session attach = %#v, want one unready receipt", queued.PendingGoalDeliveries)
	}
	first := queued.PendingGoalDeliveries[0]
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatalf("exact QueueGoalSessionAttach() replay error = %v", err)
	}
	changed := payload
	changed.Workspace = `C:\other-worktree`
	if _, err := store.QueueGoalSessionAttach(key, changed); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("changed QueueGoalSessionAttach() error = %v, want ErrGoalDeliveryConflict", err)
	}

	ready, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatalf("MarkGoalSessionAttachDeliveryReady() error = %v", err)
	}
	if len(ready.PendingGoalDeliveries) != 1 || !ready.PendingGoalDeliveries[0].Ready || ready.PendingGoalDeliveries[0].PayloadDigest != first.PayloadDigest {
		t.Fatalf("ready session attach = %#v, want stable ready receipt", ready.PendingGoalDeliveries)
	}
	encoded, err := json.Marshal(ready)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "native-private-id") || strings.Contains(string(encoded), "native.json") {
		t.Fatalf("Goal delivery leaked raw native identity: %s", encoded)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliverySessionAttach, payload.LocalHandleID, first.PayloadDigest); err != nil {
		t.Fatalf("MarkGoalDeliveryDelivered() error = %v", err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HasPendingGoalDeliveries() {
		t.Fatalf("delivered Goal attach remained pending: %#v", loaded.PendingGoalDeliveries)
	}
	if len(loaded.DeliveredGoalDeliveries) != 1 || loaded.DeliveredGoalDeliveries[0].PayloadDigest != first.PayloadDigest {
		t.Fatalf("delivered Goal attach was not retained exactly: %#v", loaded.DeliveredGoalDeliveries)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliverySessionAttach, payload.LocalHandleID, first.PayloadDigest); err != nil {
		t.Fatalf("replayed MarkGoalDeliveryDelivered() error = %v", err)
	}
}

func TestNativeUsageObservationIgnoresOutOfOrderRegressionAcrossRestart(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	newer := NativeUsageObservation{
		SchemaVersion: 1, InputTokens: 100, OutputTokens: 40, CachedInputTokens: 20,
		ReasoningOutputTokens: 10, TotalTokens: 150, ObservedAt: time.Date(2026, 9, 9, 1, 0, 2, 0, time.UTC),
	}
	older := newer
	older.InputTokens = 50
	older.OutputTokens = 20
	older.TotalTokens = 80
	older.ObservedAt = time.Date(2026, 9, 9, 1, 0, 1, 0, time.UTC)
	if _, err := store.RecordNativeUsageObservation(key, newer); err != nil {
		t.Fatal(err)
	}
	retained, err := store.RecordNativeUsageObservation(key, older)
	if err != nil {
		t.Fatal(err)
	}
	if retained.NativeUsageObservation == nil || *retained.NativeUsageObservation != newer {
		t.Fatalf("regressed native usage observation = %#v, want %#v", retained.NativeUsageObservation, newer)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NativeUsageObservation == nil || *loaded.NativeUsageObservation != newer {
		t.Fatalf("reloaded native usage observation = %#v, want %#v", loaded.NativeUsageObservation, newer)
	}
}

func TestGoalUsageDeliverySurvivesConclusiveTerminalAndBlocksCleanup(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	usage := testGoalUsage(key.RunID)
	queued, err := store.QueueGoalUsage(key, usage)
	if err != nil {
		t.Fatalf("QueueGoalUsage() error = %v", err)
	}
	if len(queued.PendingGoalDeliveries) != 1 || queued.PendingGoalDeliveries[0].Fence != queued.Fence() {
		t.Fatalf("queued Goal usage = %#v", queued.PendingGoalDeliveries)
	}
	staleFence := queued
	staleFence.PendingGoalDeliveries[0].Fence.LeaseToken = "stale-lease"
	if err := store.SaveJournal(staleFence); err == nil {
		t.Fatal("SaveJournal() accepted a Goal delivery with a stale fence")
	}
	changed := usage
	changed.Model = "changed-model"
	if _, err := store.QueueGoalUsage(key, changed); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("changed QueueGoalUsage() error = %v, want ErrGoalDeliveryConflict", err)
	}
	transition := protocol.StateTransitionRequest{TransitionID: "terminal-1", State: "failed", Payload: json.RawMessage(`{}`)}
	if _, err := store.QueueTerminalTransitionAt(key, transition, time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveTerminalForCleanup() error = %v", err)
	}
	if terminal.LocalState != "cleanup_pending" || !terminal.HasPendingGoalDeliveries() {
		t.Fatalf("conclusive terminal lost late Goal usage: %#v", terminal)
	}
	if err := store.DeleteJournal(key); err == nil || !strings.Contains(err.Error(), "pending Goal delivery") {
		t.Fatalf("DeleteJournal() error = %v, want pending delivery guard", err)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliveryUsage, usage.UsageKey, queued.PendingGoalDeliveries[0].PayloadDigest); err != nil {
		t.Fatalf("MarkGoalDeliveryDelivered() error = %v", err)
	}
	delivered, err := store.LoadJournal(key)
	if err != nil || len(delivered.DeliveredGoalDeliveries) != 1 || delivered.DeliveredGoalDeliveries[0].Usage == nil || delivered.DeliveredGoalDeliveries[0].PayloadDigest != queued.PendingGoalDeliveries[0].PayloadDigest {
		t.Fatalf("delivered Goal usage record = %#v, error = %v", delivered.DeliveredGoalDeliveries, err)
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("DeleteJournal() after receipt error = %v", err)
	}
	ledger, err := store.LoadLateGoalUsage(key)
	if err != nil || ledger.Fence != terminal.Fence() || len(ledger.PendingUsageDeliveries) != 0 || len(ledger.DeliveredDeliveries) != 1 {
		t.Fatalf("late usage ledger after cleanup = %#v, error = %v", ledger, err)
	}
	late := usage
	late.UsageID = "00000000-0000-4000-8000-000000000008"
	late.UsageKey = "late-usage-final"
	if _, err := store.QueueGoalUsage(key, late); err != nil {
		t.Fatalf("QueueGoalUsage() after journal cleanup error = %v", err)
	}
	if _, err := store.LoadJournal(key); !IsNotFound(err) {
		t.Fatalf("late usage recreated execution journal: %v", err)
	}
	ledger, err = store.LoadLateGoalUsage(key)
	if err != nil || len(ledger.PendingUsageDeliveries) != 1 || ledger.PendingUsageDeliveries[0].Fence != terminal.Fence() {
		t.Fatalf("late usage ledger = %#v, error = %v", ledger, err)
	}
}

func TestNativeUsageRecoveryRequiresExactUsageBeforeClearOrJournalDeletion(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	usage := testGoalUsage(key.RunID)
	usage.UsageKey = "native-final"
	queued, err := store.QueueNativeUsageRecovery(key, usage)
	if err != nil {
		t.Fatalf("QueueNativeUsageRecovery() error = %v", err)
	}
	if !queued.NativeUsageRecoveryRequired || len(queued.PendingGoalDeliveries) != 1 || queued.PendingGoalDeliveries[0].Usage == nil || !reflect.DeepEqual(*queued.PendingGoalDeliveries[0].Usage, usage) {
		t.Fatalf("native usage recovery = %#v", queued)
	}
	wrong := usage
	wrong.Model = "other-model"
	if _, err := store.ClearNativeUsageRecoveryRequired(key, wrong); err == nil {
		t.Fatal("ClearNativeUsageRecoveryRequired() accepted a different usage body")
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliveryUsage, usage.UsageKey, queued.PendingGoalDeliveries[0].PayloadDigest); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(key); err == nil {
		t.Fatal("DeleteJournal() removed a native accounting recovery barrier")
	}
	if _, err := store.ClearNativeUsageRecoveryRequired(key, usage); err != nil {
		t.Fatalf("ClearNativeUsageRecoveryRequired() error = %v", err)
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("DeleteJournal() error = %v", err)
	}
}

func TestDeleteJournalIgnoresRetiredNonUsageDeliveries(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDelivery()
	queued, err := store.QueueGoalSessionAttach(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetireGoalDelivery(key, GoalDeliverySessionAttach, payload.LocalHandleID, queued.PendingGoalDeliveries[0].PayloadDigest, 409, "stale_admission", "stale attach", time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(key); err != nil {
		t.Fatalf("DeleteJournal() with retired non-usage delivery error = %v", err)
	}
	ledger, err := store.LoadLateGoalUsage(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.RetiredDeliveries) != 0 {
		t.Fatalf("late usage ledger retained non-usage retirement: %#v", ledger.RetiredDeliveries)
	}
}

func TestQueueGoalDeliveryWriteFailureLeavesNoPartialIntent(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	original := applyFileSecurity
	applyFileSecurity = func(string) error { return errors.New("injected local write failure") }
	t.Cleanup(func() { applyFileSecurity = original })
	if _, err := store.QueueGoalSessionAttach(key, testGoalSessionAttachDelivery()); err == nil {
		t.Fatal("QueueGoalSessionAttach() succeeded despite local write failure")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HasPendingGoalDeliveries() {
		t.Fatalf("failed write left partial Goal delivery: %#v", loaded.PendingGoalDeliveries)
	}
}

func TestKnownPreNativeAbortRetiresOnlyUnreadyAttachIntent(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDelivery()
	intent := GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000006", GoalID: payload.GoalID, GoalRevision: 1,
		RunID: key.RunID, Generation: key.Generation, AdmissionID: "00000000-0000-4000-8000-000000000007", LocalHandleID: payload.LocalHandleID,
		RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: payload.HarnessKind, HarnessVersion: payload.HarnessVersion,
		AdapterVersion: payload.AdapterVersion, AdapterProtocolVersion: 1, WorkspaceFingerprint: payload.WorkspaceFingerprint, SessionMode: GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(intent.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DiscardUnreadyGoalSessionAttachDelivery(key, payload.LocalHandleID); err != nil {
		t.Fatalf("DiscardUnreadyGoalSessionAttachDelivery() error = %v", err)
	}
	closed, err := store.AbortGoalSessionLaunchBeforeNativeStart(intent.Key())
	if err != nil {
		t.Fatalf("AbortGoalSessionLaunchBeforeNativeStart() error = %v", err)
	}
	if closed.LaunchState != GoalSessionLaunchStateClosed || closed.SessionState != GoalSessionStateClosed || closed.NeedsReconciliation() {
		t.Fatalf("known pre-native abort = %#v", closed)
	}
	if journal, err := store.LoadJournal(key); err != nil || journal.HasPendingGoalDeliveries() {
		t.Fatalf("pre-native abort deliveries = %#v, error = %v", journal.PendingGoalDeliveries, err)
	}
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DiscardUnreadyGoalSessionAttachDelivery(key, payload.LocalHandleID); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("discard ready attach error = %v, want ErrGoalDeliveryConflict", err)
	}
}

func testGoalDeliveryJournal(key RunKey) RunJournal {
	return RunJournal{
		RunID: key.RunID, Generation: key.Generation, RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 1,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 12, 31, 1, 0, 0, 0, time.UTC),
		LocalState: "running", Work: protocol.Work{Goal: "implement", Input: json.RawMessage(`{}`)}, WorkspaceBindingKey: "local",
	}
}

func testGoalSessionAttachDelivery() GoalSessionAttachDelivery {
	repositoryID := "00000000-0000-4000-8000-000000000002"
	return GoalSessionAttachDelivery{
		GoalID: "00000000-0000-4000-8000-000000000003", LocalHandleID: "00000000-0000-4000-8000-000000000004",
		HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test",
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`, RepositoryResourceID: &repositoryID,
	}
}

func testGoalUsage(runID string) protocol.Usage {
	return protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000005", RunID: runID,
		UsageKey: "late-usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown,
		ObservedAt: "2026-09-09T01:00:00Z",
	}
}
