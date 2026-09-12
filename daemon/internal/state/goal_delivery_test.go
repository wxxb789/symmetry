package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	attached := saveAttachedGoalSessionForAttachDelivery(t, store, key, payload)
	if attached.NativeSessionFilename != "" {
		t.Fatalf("attached native filename = %q, want optional filename absent", attached.NativeSessionFilename)
	}
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

func TestGoalSessionStoppedDeliveryIsDurableAndUsesBindingAsReceiptIdentity(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := GoalSessionStoppedDelivery{
		SessionID:     "00000000-0000-4000-8000-000000000002",
		LocalHandleID: "00000000-0000-4000-8000-000000000003",
		BindingID:     "00000000-0000-4000-8000-000000000004",
	}
	queued, err := store.QueueGoalSessionStopped(key, payload)
	if err != nil {
		t.Fatalf("QueueGoalSessionStopped() error = %v", err)
	}
	if len(queued.PendingGoalDeliveries) != 1 || queued.PendingGoalDeliveries[0].Kind != GoalDeliverySessionStopped || queued.PendingGoalDeliveries[0].DeliveryID != payload.BindingID || !queued.PendingGoalDeliveries[0].Ready {
		t.Fatalf("queued session stop = %#v", queued.PendingGoalDeliveries)
	}
	first := queued.PendingGoalDeliveries[0]
	if _, err := store.QueueGoalSessionStopped(key, payload); err != nil {
		t.Fatalf("exact QueueGoalSessionStopped() replay error = %v", err)
	}
	changed := payload
	changed.SessionID = "00000000-0000-4000-8000-000000000005"
	if _, err := store.QueueGoalSessionStopped(key, changed); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("changed QueueGoalSessionStopped() error = %v, want ErrGoalDeliveryConflict", err)
	}
	if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliverySessionStopped, payload.BindingID, first.PayloadDigest); err != nil {
		t.Fatalf("MarkGoalDeliveryDelivered() error = %v", err)
	}
}

func TestLegacyGoalDeliveryDigestAndJournalRemainReadableWithoutBinding(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	delivery := GoalDelivery{Kind: GoalDeliverySessionAttach, DeliveryID: "00000000-0000-4000-8000-000000000004", Fence: testGoalDeliveryJournal(key).Fence(), Ready: true, SessionAttach: &GoalSessionAttachDelivery{
		GoalID: "00000000-0000-4000-8000-000000000003", LocalHandleID: "00000000-0000-4000-8000-000000000004", HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test",
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`,
	}}
	legacyEncoded, err := json.Marshal(struct {
		Kind          GoalDeliveryKind
		DeliveryID    string
		Fence         protocol.Fence
		SessionAttach *GoalSessionAttachDelivery
		Evidence      *protocol.Evidence
		Usage         *protocol.Usage
	}{delivery.Kind, delivery.DeliveryID, delivery.Fence, delivery.SessionAttach, delivery.Evidence, delivery.Usage})
	if err != nil {
		t.Fatal(err)
	}
	legacySum := sha256.Sum256(legacyEncoded)
	wantDigest := hex.EncodeToString(legacySum[:])
	gotDigest, err := goalDeliveryDigest(delivery)
	if err != nil || gotDigest != wantDigest {
		t.Fatalf("legacy Goal delivery digest = %q, %v; want %q", gotDigest, err, wantDigest)
	}
	delivery.PayloadDigest = gotDigest
	journal := testGoalDeliveryJournal(key)
	journal.GoalDeliveryEnabled = true
	journal.PendingGoalDeliveries = []GoalDelivery{delivery}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() rejected legacy attach delivery: %v", err)
	}
	if _, err := store.QueueGoalSessionAttach(key, *delivery.SessionAttach); err == nil {
		t.Fatal("QueueGoalSessionAttach() accepted a new attachment without binding ID")
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

func TestQueueGoalEvidenceAndTerminalTransitionIsAtomicAndIdempotent(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	base := testGoalDeliveryJournal(key)
	if err := store.SaveJournal(base); err != nil {
		t.Fatal(err)
	}
	evidence := []protocol.Evidence{
		testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000011", "evidence-1"),
		testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000013", "evidence-2"),
	}
	refs := []string{evidence[0].EvidenceID, evidence[1].EvidenceID}
	payload, err := json.Marshal(map[string]any{
		"summary": "validated candidate",
		"task_result": map[string]any{
			"kind":          string(protocol.TaskResultCandidateCompletion),
			"evidence_refs": refs,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	transition := protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: payload}
	pendingAt := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	queued, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, transition, pendingAt)
	if err != nil {
		t.Fatalf("QueueGoalEvidenceAndTerminalTransition() error = %v", err)
	}
	if queued.LocalState != "terminal_pending" || queued.TerminalState != "completed" || !queued.TerminalPendingAt.Equal(pendingAt) || len(queued.PendingGoalDeliveries) != len(evidence) || len(queued.PendingTransitions) != 1 {
		t.Fatalf("queued fixed evidence and terminal = %#v", queued)
	}
	if queued.PendingGoalDeliveries[0].Evidence == nil || queued.PendingGoalDeliveries[1].Evidence == nil {
		t.Fatalf("queued evidence bodies missing: %#v", queued.PendingGoalDeliveries)
	}

	replayed, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, transition, pendingAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("exact QueueGoalEvidenceAndTerminalTransition() replay error = %v", err)
	}
	if !reflect.DeepEqual(replayed, queued) {
		t.Fatalf("exact replay changed durable receipt: got %#v want %#v", replayed, queued)
	}

	changedEvidence := append([]protocol.Evidence(nil), evidence...)
	changedEvidence[0].EvidenceKey = "evidence-1-changed"
	if _, err := store.QueueGoalEvidenceAndTerminalTransition(key, changedEvidence, transition, pendingAt); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("changed evidence body error = %v, want ErrGoalDeliveryConflict", err)
	}
	changedPayload := append(json.RawMessage(nil), payload...)
	changedPayload = json.RawMessage(string(changedPayload) + " ")
	changedTransition := transition
	changedTransition.Payload = changedPayload
	if _, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, changedTransition, pendingAt); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("changed terminal body error = %v, want ErrGoalDeliveryConflict", err)
	}
	unchanged, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unchanged, queued) {
		t.Fatalf("conflicting replay mutated journal: got %#v want %#v", unchanged, queued)
	}
}

func TestQueueGoalEvidenceAndTerminalTransitionWriteFailureLeavesNoPartialState(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	base := testGoalDeliveryJournal(key)
	if err := store.SaveJournal(base); err != nil {
		t.Fatal(err)
	}
	base, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	evidence := []protocol.Evidence{testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000011", "evidence-1")}
	payload, err := json.Marshal(map[string]any{"task_result": map[string]any{"kind": string(protocol.TaskResultCandidateCompletion), "evidence_refs": []string{evidence[0].EvidenceID}}})
	if err != nil {
		t.Fatal(err)
	}
	transition := protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: payload}
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return errors.New("injected journal write failure") })
	t.Cleanup(restore)
	if _, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, transition, time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("QueueGoalEvidenceAndTerminalTransition() succeeded despite injected write failure")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, base) {
		t.Fatalf("failed atomic write left partial evidence or terminal: got %#v want %#v", loaded, base)
	}
}

func TestQueueGoalEvidenceAndTerminalTransitionDoesNotAddEvidenceAfterCancellation(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	base := testGoalDeliveryJournal(key)
	if err := store.SaveJournal(base); err != nil {
		t.Fatal(err)
	}
	cancelled := protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{"reason":"cancelled"}`)}
	before, err := store.QueueTerminalTransitionAt(key, cancelled, time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	evidence := []protocol.Evidence{testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000011", "evidence-1")}
	payload, err := json.Marshal(map[string]any{"task_result": map[string]any{"kind": string(protocol.TaskResultCandidateCompletion), "evidence_refs": []string{evidence[0].EvidenceID}}})
	if err != nil {
		t.Fatal(err)
	}
	completed := protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: payload}
	if _, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, completed, time.Date(2026, 9, 12, 1, 1, 0, 0, time.UTC)); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("evidence after authoritative cancellation error = %v, want ErrGoalDeliveryConflict", err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, before) || len(loaded.PendingGoalDeliveries) != 0 || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].State != "cancelled" {
		t.Fatalf("cancellation race mutated journal or added evidence: got %#v want %#v", loaded, before)
	}
}

func TestGoalEvidenceTerminalCannotBeReplacedByCancellation(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	base := testGoalDeliveryJournal(key)
	if err := store.SaveJournal(base); err != nil {
		t.Fatal(err)
	}
	evidence := []protocol.Evidence{testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000011", "evidence-1")}
	payload, err := json.Marshal(map[string]any{"task_result": map[string]any{"kind": string(protocol.TaskResultCandidateCompletion), "evidence_refs": []string{evidence[0].EvidenceID}}})
	if err != nil {
		t.Fatal(err)
	}
	completed := protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: payload}
	pendingAt := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	queued, err := store.QueueGoalEvidenceAndTerminalTransition(key, evidence, completed, pendingAt)
	if err != nil {
		t.Fatalf("QueueGoalEvidenceAndTerminalTransition() error = %v", err)
	}
	cancelled := protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{"reason":"cancelled"}`)}
	if _, err := store.QueueTerminalTransitionAt(key, cancelled, pendingAt.Add(time.Second)); err == nil {
		t.Fatal("QueueTerminalTransitionAt(cancelled) replaced completed evidence terminal")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, queued) || loaded.TerminalState != "completed" || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != completed.TransitionID || len(loaded.PendingGoalDeliveries) != 1 || loaded.PendingGoalDeliveries[0].Evidence == nil || loaded.PendingGoalDeliveries[0].Evidence.EvidenceID != evidence[0].EvidenceID {
		t.Fatalf("cancellation mutated completed evidence terminal: got %#v want %#v", loaded, queued)
	}
}

func TestPersistedGoalDeliveryRequiresStablePayloadDigest(t *testing.T) {
	t.Run("evidence", func(t *testing.T) {
		store := mustStore(t)
		key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
		if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
			t.Fatal(err)
		}
		evidence := testAtomicGoalEvidence(t, key.RunID, "00000000-0000-4000-8000-000000000011", "evidence-1")
		queued, err := store.QueueGoalEvidence(key, evidence)
		if err != nil {
			t.Fatal(err)
		}
		digest := queued.PendingGoalDeliveries[0].PayloadDigest
		invalid := queued
		invalid.PendingGoalDeliveries[0].PayloadDigest = ""
		if err := store.SaveJournal(invalid); err == nil {
			t.Fatal("SaveJournal() accepted an evidence delivery with an empty payload digest")
		}
		if replayed, err := store.QueueGoalEvidence(key, evidence); err != nil || len(replayed.PendingGoalDeliveries) != 1 || replayed.PendingGoalDeliveries[0].PayloadDigest != digest {
			t.Fatalf("valid evidence replay = %#v, error = %v", replayed, err)
		}
		if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliveryEvidence, evidence.EvidenceKey, digest); err != nil {
			t.Fatalf("valid evidence delivery after replay error = %v", err)
		}
	})

	t.Run("usage", func(t *testing.T) {
		store := mustStore(t)
		key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
		if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
			t.Fatal(err)
		}
		usage := testGoalUsage(key.RunID)
		queued, err := store.QueueGoalUsage(key, usage)
		if err != nil {
			t.Fatal(err)
		}
		digest := queued.PendingGoalDeliveries[0].PayloadDigest
		invalid := queued
		invalid.PendingGoalDeliveries[0].PayloadDigest = ""
		if err := store.SaveJournal(invalid); err == nil {
			t.Fatal("SaveJournal() accepted a usage delivery with an empty payload digest")
		}
		if replayed, err := store.QueueGoalUsage(key, usage); err != nil || len(replayed.PendingGoalDeliveries) != 1 || replayed.PendingGoalDeliveries[0].PayloadDigest != digest {
			t.Fatalf("valid usage replay = %#v, error = %v", replayed, err)
		}
		if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliveryUsage, usage.UsageKey, digest); err != nil {
			t.Fatalf("valid usage delivery after replay error = %v", err)
		}
	})

	t.Run("late usage ledger load", func(t *testing.T) {
		store := mustStore(t)
		key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
		if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
			t.Fatal(err)
		}
		usage := testGoalUsage(key.RunID)
		queued, err := store.QueueGoalUsage(key, usage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, time.Date(2026, 9, 12, 1, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.MarkGoalDeliveryDelivered(key, GoalDeliveryUsage, usage.UsageKey, queued.PendingGoalDeliveries[0].PayloadDigest); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteJournal(key); err != nil {
			t.Fatal(err)
		}
		ledger, err := store.LoadLateGoalUsage(key)
		if err != nil || len(ledger.DeliveredDeliveries) != 1 {
			t.Fatalf("valid late usage ledger = %#v, error = %v", ledger, err)
		}
		invalid := ledger
		invalid.DeliveredDeliveries[0].PayloadDigest = ""
		encoded, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.lateGoalUsagePath(key), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadLateGoalUsage(key); err == nil {
			t.Fatal("LoadLateGoalUsage() accepted a delivered usage with an empty payload digest")
		}
	})
}

func TestDeleteJournalIgnoresRetiredNonUsageDeliveries(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDelivery()
	saveAttachedGoalSessionForAttachDelivery(t, store, key, payload)
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
	readyPayload := payload
	readyPayload.LocalHandleID = "00000000-0000-4000-8000-000000000008"
	saveAttachedGoalSessionForAttachDelivery(t, store, key, readyPayload)
	if _, err := store.QueueGoalSessionAttach(key, readyPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, readyPayload.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DiscardUnreadyGoalSessionAttachDelivery(key, readyPayload.LocalHandleID); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("discard ready attach error = %v, want ErrGoalDeliveryConflict", err)
	}
}

func TestMarkGoalSessionAttachDeliveryReadyRejectsUnprovenOrMismatchedSession(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, *Store, RunKey, GoalSessionAttachDelivery)
		matches func(error) bool
	}{
		{
			name: "missing session",
			matches: func(err error) bool {
				return IsNotFound(err)
			},
		},
		{
			name: "intent only",
			setup: func(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery) {
				saveGoalSessionIntentForAttachDelivery(t, store, key, payload, GoalSessionModeFresh)
			},
			matches: func(err error) bool {
				return errors.Is(err, ErrGoalSessionNotAttached)
			},
		},
		{
			name: "launching",
			setup: func(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery) {
				session := saveGoalSessionIntentForAttachDelivery(t, store, key, payload, GoalSessionModeFresh)
				if _, err := store.MarkGoalSessionLaunchStarted(session.Key()); err != nil {
					t.Fatal(err)
				}
			},
			matches: func(err error) bool {
				return errors.Is(err, ErrGoalSessionUncertain)
			},
		},
		{
			name: "uncertain",
			setup: func(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery) {
				session := saveGoalSessionIntentForAttachDelivery(t, store, key, payload, GoalSessionModeFresh)
				if _, err := store.MarkGoalSessionLaunchStarted(session.Key()); err != nil {
					t.Fatal(err)
				}
				if _, err := store.MarkGoalSessionUncertain(session.Key(), "native launch outcome is unknown"); err != nil {
					t.Fatal(err)
				}
			},
			matches: func(err error) bool {
				return errors.Is(err, ErrGoalSessionUncertain)
			},
		},
		{
			name: "workspace mismatch",
			setup: func(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery) {
				mismatched := payload
				mismatched.WorkspaceFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				saveAttachedGoalSessionForAttachDelivery(t, store, key, mismatched)
			},
			matches: func(err error) bool {
				return errors.Is(err, ErrGoalDeliveryConflict)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
			if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
				t.Fatal(err)
			}
			payload := testGoalSessionAttachDelivery()
			if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
				t.Fatal(err)
			}
			if test.setup != nil {
				test.setup(t, store, key, payload)
			}

			_, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
			if !test.matches(err) {
				t.Fatalf("MarkGoalSessionAttachDeliveryReady() error = %v", err)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if len(journal.PendingGoalDeliveries) != 1 || journal.PendingGoalDeliveries[0].Ready {
				t.Fatalf("rejected attach delivery changed = %#v", journal.PendingGoalDeliveries)
			}
		})
	}
}

func TestMarkGoalSessionAttachDeliveryReadyAllowsFreshServerIssuedBindingWithoutFilename(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "00000000-0000-4000-8000-000000000001", Generation: 1}
	if err := store.SaveJournal(testGoalDeliveryJournal(key)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDelivery()
	payload.BindingID = ""
	payload.ServerIssuedBinding = true
	attached := saveAttachedGoalSessionForAttachDelivery(t, store, key, payload)
	if attached.BindingID != "" || attached.NativeSessionFilename != "" {
		t.Fatalf("fresh server-issued session = %#v, want empty binding and filename", attached)
	}
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	ready, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatalf("MarkGoalSessionAttachDeliveryReady() error = %v", err)
	}
	if len(ready.PendingGoalDeliveries) != 1 || !ready.PendingGoalDeliveries[0].Ready {
		t.Fatalf("server-issued attach delivery = %#v, want ready", ready.PendingGoalDeliveries)
	}
}

func TestMarkGoalSessionAttachDeliveryReadyRejectsResumeOldBinding(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(certificate)
	key := RunKey{RunID: rebind.RunID, Generation: rebind.Generation}
	if err := store.SaveJournal(testGoalDeliveryJournalForSession(key, source)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebindGoalSessionForResume(source.Key(), rebind); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDeliveryForSession(source)
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("MarkGoalSessionAttachDeliveryReady() with predecessor binding error = %v, want ErrGoalDeliveryConflict", err)
	}
}

func TestMarkGoalSessionAttachDeliveryReadyPermitsQueuedResumeAfterRebindBeforeStart(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	rebind := testGoalSessionResumeRebind(certificate)
	key := RunKey{RunID: rebind.RunID, Generation: rebind.Generation}
	if err := store.SaveJournal(testGoalDeliveryJournalForSession(key, source)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDeliveryForSession(source)
	payload.BindingID = rebind.BindingID
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID); !errors.Is(err, ErrGoalDeliveryConflict) {
		t.Fatalf("MarkGoalSessionAttachDeliveryReady() before rebind error = %v, want ErrGoalDeliveryConflict", err)
	}
	rebound, err := store.RebindGoalSessionForResume(source.Key(), rebind)
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.ResumeStartPending {
		t.Fatalf("rebound session = %#v, want pre-start resume", rebound)
	}
	ready, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatalf("MarkGoalSessionAttachDeliveryReady() after rebind error = %v", err)
	}
	if len(ready.PendingGoalDeliveries) != 1 || !ready.PendingGoalDeliveries[0].Ready {
		t.Fatalf("rebound attach delivery = %#v, want ready before StartAttempted", ready.PendingGoalDeliveries)
	}
	if _, err := store.MarkGoalSessionResumeStartAttempted(source.Key()); err != nil {
		t.Fatalf("MarkGoalSessionResumeStartAttempted() after ready error = %v", err)
	}
}

func TestMarkGoalSessionAttachDeliveryReadyReplayDoesNotMutateAfterRebind(t *testing.T) {
	store := mustStore(t)
	source, certificate := mustAvailableRetainedGoalSession(t, store)
	key := RunKey{RunID: source.RunID, Generation: source.Generation}
	if err := store.SaveJournal(testGoalDeliveryJournalForSession(key, source)); err != nil {
		t.Fatal(err)
	}
	payload := testGoalSessionAttachDeliveryForSession(source)
	if _, err := store.QueueGoalSessionAttach(key, payload); err != nil {
		t.Fatal(err)
	}
	ready, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebindGoalSessionForResume(source.Key(), testGoalSessionResumeRebind(certificate)); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.MarkGoalSessionAttachDeliveryReady(key, payload.LocalHandleID)
	if err != nil {
		t.Fatalf("replayed MarkGoalSessionAttachDeliveryReady() error = %v", err)
	}
	if !reflect.DeepEqual(replayed, ready) {
		t.Fatalf("ready replay changed historical receipt: got %#v want %#v", replayed, ready)
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
		BindingID:   "00000000-0000-4000-8000-000000000005",
		HarnessKind: "codex", HarnessVersion: "0.153.4", AdapterVersion: "symmetry-daemon:test",
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Workspace: `C:\worktree`, RepositoryResourceID: &repositoryID,
	}
}

func saveAttachedGoalSessionForAttachDelivery(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery) GoalSessionJournal {
	t.Helper()
	session := saveGoalSessionIntentForAttachDelivery(t, store, key, payload, GoalSessionModeFresh)
	if _, err := store.MarkGoalSessionLaunchStarted(session.Key()); err != nil {
		t.Fatal(err)
	}
	attached, err := store.PersistGoalSessionHandle(session.Key(), GoalSessionHandle{NativeSessionID: "native-private-id"})
	if err != nil {
		t.Fatal(err)
	}
	return attached
}

func saveGoalSessionIntentForAttachDelivery(t *testing.T, store *Store, key RunKey, payload GoalSessionAttachDelivery, mode string) GoalSessionJournal {
	t.Helper()
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	repositoryResourceID := ""
	if payload.RepositoryResourceID != nil {
		repositoryResourceID = *payload.RepositoryResourceID
	}
	intent := GoalSessionLaunchIntent{
		LaunchIntentID:         "attach-intent-" + payload.LocalHandleID,
		GoalID:                 payload.GoalID,
		GoalRevision:           1,
		WorkItemID:             "work-item-1",
		TaskID:                 "task-1",
		RunID:                  key.RunID,
		Generation:             key.Generation,
		AdmissionID:            "admission-1",
		LocalHandleID:          payload.LocalHandleID,
		OwnerID:                "owner-1",
		MachineID:              "machine-1",
		RuntimeID:              run.RuntimeID,
		RuntimeEpoch:           run.ClaimedRuntimeEpoch,
		DaemonInstanceID:       "daemon-1",
		HarnessKind:            payload.HarnessKind,
		HarnessVersion:         payload.HarnessVersion,
		AdapterVersion:         payload.AdapterVersion,
		AdapterProtocolVersion: 1,
		WorkspaceFingerprint:   payload.WorkspaceFingerprint,
		WorkspacePath:          payload.Workspace,
		WorkspaceOwnerRunKey:   WorkspaceOwnerRunKey{RunID: key.RunID, Generation: key.Generation},
		RepositoryResourceID:   repositoryResourceID,
		BindingID:              payload.BindingID,
		SessionMode:            mode,
	}
	if mode == GoalSessionModeHandoff {
		intent.HandoffSourceRunID = "00000000-0000-4000-8000-000000000009"
	}
	session, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func testGoalDeliveryJournalForSession(key RunKey, session GoalSessionJournal) RunJournal {
	journal := testGoalDeliveryJournal(key)
	journal.RuntimeID = session.RuntimeID
	journal.ClaimedRuntimeEpoch = session.RuntimeEpoch
	return journal
}

func testGoalSessionAttachDeliveryForSession(session GoalSessionJournal) GoalSessionAttachDelivery {
	payload := GoalSessionAttachDelivery{
		GoalID:               session.GoalID,
		LocalHandleID:        session.LocalHandleID,
		BindingID:            session.BindingID,
		HarnessKind:          session.HarnessKind,
		HarnessVersion:       session.HarnessVersion,
		AdapterVersion:       session.AdapterVersion,
		WorkspaceFingerprint: session.WorkspaceFingerprint,
		Workspace:            session.WorkspacePath,
	}
	if session.RepositoryResourceID != "" {
		repositoryResourceID := session.RepositoryResourceID
		payload.RepositoryResourceID = &repositoryResourceID
	}
	return payload
}

func testGoalUsage(runID string) protocol.Usage {
	return protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000005", RunID: runID,
		UsageKey: "late-usage", Provider: "openai", Model: "gpt-test", CostBasis: protocol.CostUnknown,
		ObservedAt: "2026-09-09T01:00:00Z",
	}
}

func testAtomicGoalEvidence(t *testing.T, runID, evidenceID, evidenceKey string) protocol.Evidence {
	t.Helper()
	subject := protocol.Subject{ResourceID: "00000000-0000-4000-8000-000000000012", Commit: "0000000000000000000000000000000000000000", TreeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	profileHash := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	evidence, err := protocol.ParseEvidence([]byte(fmt.Sprintf(`{"schema_version":"symmetry.evidence.v1","evidence_id":"%s","run_id":"%s","evidence_key":"%s","kind":"check","subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},"subject_hash":"%s","source_ref":{"kind":"check","ref":"check-run","validator_profile":"default-checks","subject_hash":"%s"},"source_revision":"profile:default-checks","validator_profile":"default-checks","verdict":"passed","payload":{"predicate_id":"tests","subject":{"resource_id":"%s","commit":"%s","tree_digest":"%s"},"profile_digest":"%s","command_argv_digest":"%s","exit_code":0,"subject_hash":"%s","started_at":"2026-09-09T01:00:00Z","finished_at":"2026-09-09T01:01:00Z","output_ref":{"kind":"artifact","value":"artifact:check-output"}},"observed_at":"2026-09-09T01:01:00Z"}`, evidenceID, runID, evidenceKey, subject.ResourceID, subject.Commit, subject.TreeDigest, subjectHash, subjectHash, subject.ResourceID, subject.Commit, subject.TreeDigest, profileHash, profileHash, subjectHash)))
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
