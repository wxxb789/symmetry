package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestDurablePiProviderBridgeExecutorReplaysSuccessAcrossStoreRestart(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-replay", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	request := testPiProviderRequest()
	first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if first.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("first response = %#v", first)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	replayExecutor, err := newDurablePiProviderBridgeExecutor(stub, restarted, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	second := callPiProviderActionExecutor(t, replayExecutor, context.Background(), testPiProviderActionID, request)
	if second.Outcome != pi.ProviderBridgeOutcomeSucceeded || string(second.Result) != string(first.Result) {
		t.Fatalf("replayed response = %#v, want %#v", second, first)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls = %d, want one", got)
	}
}

func TestDurablePiProviderBridgeExecutorRejectsChangedInputAcrossRestart(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-conflict", Generation: 1}
	store := newPiProviderActionStore(t, t.TempDir(), key)
	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	if response := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest()); response.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("first response = %#v", response)
	}
	changed := testPiProviderRequest()
	changed.Input = json.RawMessage(`{"title":"different"}`)
	response := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, changed)
	if response.Outcome != pi.ProviderBridgeOutcomeFailed || response.FailureCode != piProviderActionFailureLocalConflict {
		t.Fatalf("changed request response = %#v", response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls = %d, want one", got)
	}
}

func TestDurablePiProviderBridgeExecutorDoesNotExposeSuccessAfterCompletionAcknowledgementLoss(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-completion-ack-loss", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	var writes atomic.Int32
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		write := writes.Add(1)
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if write == 2 {
			return errors.New("injected completion acknowledgement loss")
		}
		return nil
	})
	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if first.Outcome != pi.ProviderBridgeOutcomeUnknown || first.FailureCode != piProviderActionFailureLocalPersistence {
		t.Fatalf("ack-loss response = %#v", first)
	}
	restore()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	replayExecutor, err := newDurablePiProviderBridgeExecutor(stub, restarted, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	second := callPiProviderActionExecutor(t, replayExecutor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if second.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("durable replay after ack loss = %#v", second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls = %d, want one", got)
	}
}

func TestDurablePiProviderBridgeExecutorReplaysRecoveredUnfinishedDispatchAsUnknown(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-recovered", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	intent, err := piProviderActionIntent(testPiProviderActionID, testPiProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, dispatch, err := store.PrepareProviderAction(key, intent); err != nil || !dispatch {
		t.Fatalf("prepare dispatch=%t error=%v", dispatch, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if _, err := restarted.RecoverProviderActions(key, "daemon_restart_unknown"); err != nil {
		t.Fatal(err)
	}
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		t.Fatal("recovered unfinished action reached Control")
		return control.ProviderActionResponse{}, nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, restarted, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	response := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if response.Outcome != pi.ProviderBridgeOutcomeUnknown || response.FailureCode != "daemon_restart_unknown" {
		t.Fatalf("recovered response = %#v", response)
	}
}

func newPiProviderActionStore(t *testing.T, directory string, key state.RunKey) *state.Store {
	t.Helper()
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	journal := state.RunJournal{
		RunID:               key.RunID,
		Generation:          key.Generation,
		RuntimeKey:          "pi",
		RuntimeID:           "runtime-1",
		ClaimedRuntimeEpoch: 1,
		ClaimID:             "claim-1",
		LeaseToken:          "lease-1",
		LeaseExpiresAt:      time.Now().UTC().Add(time.Minute),
		LocalState:          "running",
		Work: protocol.Work{
			Goal:         "provider action",
			AgentProfile: "pi",
			Workspace:    "isolated",
			Input:        json.RawMessage(`{}`),
		},
		WorkspacePath:       `C:\work\provider-action`,
		WorkspaceBindingKey: "binding-1",
	}
	if err := store.SaveJournal(journal); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store
}

func testPiProviderSuccess() control.ProviderActionResponse {
	return control.ProviderActionResponse{
		Outcome: control.ProviderActionSucceeded,
		Result:  json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"}}`),
	}
}

func writeAppStateAtomic(path string, data []byte) error {
	// The state package owns the production atomic replace implementation. This
	// injected writer commits the bytes before returning a synthetic lost ACK.
	return os.WriteFile(path, data, 0o600)
}
