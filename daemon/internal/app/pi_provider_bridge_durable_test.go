package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestDurablePiProviderBridgeExecutorRedactsEscapedTokenWithLargeNumberAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-large-number-unsafe", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	var calls atomic.Int32
	unsafeResult := json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"},"big":1e10000,"credential":"\u0070rovider-token-do-not-leak"}`)
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		response := testPiProviderSuccess()
		response.Result = unsafeResult
		return response, nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	request := testPiProviderRequest()
	first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if first.Outcome != pi.ProviderBridgeOutcomeUnknown || first.FailureCode != piProviderActionFailureControlResult || len(first.Result) != 0 {
		t.Fatalf("first response = %#v, want redacted unknown", first)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls after initial dispatch = %d, want one", got)
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.ProviderActionIntents) != 1 {
		t.Fatalf("provider action intents = %d, want one", len(journal.ProviderActionIntents))
	}
	intent := journal.ProviderActionIntents[0]
	if intent.Outcome != state.ProviderActionOutcomeUnknown || intent.FailureCode != piProviderActionFailureControlResult || len(intent.Result) != 0 {
		t.Fatalf("persisted provider action = %#v, want redacted unknown without result", intent)
	}
	if err := filepath.Walk(directory, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || filepath.Base(path) == ".symmetry-daemon.lock" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if content := string(data); strings.Contains(content, testPiProviderToken) || strings.Contains(content, `\u0070rovider-token-do-not-leak`) {
			return fmt.Errorf("journal file %q retained provider token", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	if second.Outcome != pi.ProviderBridgeOutcomeUnknown || second.FailureCode != piProviderActionFailureControlResult || len(second.Result) != 0 {
		t.Fatalf("replayed response = %#v, want exact redacted unknown", second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls after restart replay = %d, want one total", got)
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

func TestDurablePiProviderBridgeExecutorPreWritePersistenceFailureIsUnknownWithoutDispatch(t *testing.T) {
	directory := t.TempDir()
	key := state.RunKey{RunID: "run-provider-prewrite-failure", Generation: 1}
	store := newPiProviderActionStore(t, directory, key)
	var writes atomic.Int32
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes.Add(1)
		return errors.New("injected pre-write persistence failure")
	})
	defer restore()

	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	response := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if response.Outcome != pi.ProviderBridgeOutcomeUnknown || response.FailureCode != piProviderActionFailureLocalPersistence || len(response.Result) != 0 {
		t.Fatalf("pre-write persistence response = %#v, want unknown/local persistence", response)
	}
	if calls.Load() != 0 {
		t.Fatalf("Control calls = %d, want zero after pre-write failure", calls.Load())
	}
	if writes.Load() != 1 {
		t.Fatalf("pre-write writer calls = %d, want one", writes.Load())
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.ProviderActionIntents) != 0 {
		t.Fatalf("pre-write failure left provider intent: %#v", journal.ProviderActionIntents)
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

func TestDurablePiProviderBridgeExecutorDoesNotPersistUnsafeTypedSuccessOrRedispatch(t *testing.T) {
	valid := testPiProviderSuccess()
	cases := []struct {
		name     string
		response control.ProviderActionResponse
	}{
		{
			name: "succeeded empty object",
			response: control.ProviderActionResponse{
				Outcome: control.ProviderActionSucceeded,
				Result:  json.RawMessage(`{}`),
			},
		},
		{
			name: "contradictory typed success",
			response: func() control.ProviderActionResponse {
				value := valid
				value.Projected = false
				return value
			}(),
		},
		{
			name: "invalid unknown",
			response: control.ProviderActionResponse{
				Outcome:        control.ProviderActionUnknown,
				Result:         json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","outcome":"unknown","projected":true,"readback_status":"unconfirmed","readback":{}}`),
				Operation:      "change.upsert",
				ResourceID:     testPiProviderResource,
				Projected:      true,
				ReadbackStatus: "unconfirmed",
				Readback:       json.RawMessage(`{}`),
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			key := state.RunKey{RunID: "run-provider-unsafe-typed-" + test.name, Generation: 1}
			store := newPiProviderActionStore(t, t.TempDir(), key)
			var calls atomic.Int32
			stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				calls.Add(1)
				return test.response, nil
			}}
			executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
			if err != nil {
				t.Fatal(err)
			}
			first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if first.Outcome != pi.ProviderBridgeOutcomeUnknown || first.FailureCode != piProviderActionFailureControlResult || len(first.Result) != 0 {
				t.Fatalf("unsafe first response = %#v, want redacted unknown", first)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if len(journal.ProviderActionIntents) != 1 || journal.ProviderActionIntents[0].Outcome != state.ProviderActionOutcomeUnknown || journal.ProviderActionIntents[0].FailureCode != piProviderActionFailureControlResult || len(journal.ProviderActionIntents[0].Result) != 0 {
				t.Fatalf("unsafe completion journal = %#v, want unknown without result", journal.ProviderActionIntents)
			}
			second := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if second.Outcome != first.Outcome || second.FailureCode != first.FailureCode || len(second.Result) != 0 {
				t.Fatalf("unsafe replay response = %#v, want exact redacted replay", second)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("Control calls = %d, want one dispatch and no redispatch", got)
			}
		})
	}
}

func TestDurablePiProviderBridgeExecutorRejectsRawLargeNewResultBeforeCompletion(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-raw-large-new-result", Generation: 1}
	store := newPiProviderActionStore(t, t.TempDir(), key)
	response := testPiProviderSuccess()
	response.Result = json.RawMessage(strings.TrimSuffix(string(response.Result), "}") + `,"payload":"` + strings.Repeat(`\u003c`, piProviderActionMaxResultBytes/4) + `"}`)
	if len(response.Result) <= piProviderActionMaxResultBytes {
		t.Fatalf("raw-large result bytes = %d, want over %d", len(response.Result), piProviderActionMaxResultBytes)
	}
	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return response, nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if first.Outcome != pi.ProviderBridgeOutcomeUnknown || first.FailureCode != piProviderActionFailureControlResult || len(first.Result) != 0 {
		t.Fatalf("raw-large first response = %#v, want redacted unknown", first)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 1 || loaded.ProviderActionIntents[0].Outcome != state.ProviderActionOutcomeUnknown || loaded.ProviderActionIntents[0].FailureCode != piProviderActionFailureControlResult || len(loaded.ProviderActionIntents[0].Result) != 0 {
		t.Fatalf("raw-large completion journal = %#v, want unknown without result", loaded.ProviderActionIntents)
	}
	second := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
	if second.Outcome != first.Outcome || second.FailureCode != first.FailureCode || len(second.Result) != 0 {
		t.Fatalf("raw-large replay = %#v, want exact redacted replay", second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls = %d, want one dispatch and no redispatch", got)
	}
}

func TestDurablePiProviderBridgeExecutorScrubsUnsafeSeededReplayResults(t *testing.T) {
	tests := []struct {
		name        string
		outcome     string
		result      json.RawMessage
		failureCode string
	}{
		{
			name:    "succeeded_result",
			outcome: state.ProviderActionOutcomeSucceeded,
			result:  json.RawMessage(`{"projected":true,"token":"` + testPiProviderToken + `"}`),
		},
		{
			name:        "unknown_result",
			outcome:     state.ProviderActionOutcomeUnknown,
			result:      json.RawMessage(`{"status":"pending","token":"` + testPiProviderToken + `"}`),
			failureCode: piProviderActionFailureControlUnknown,
		},
		{
			name:        "failed_code",
			outcome:     state.ProviderActionOutcomeFailed,
			failureCode: "control_rejected_" + testPiProviderToken,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := state.RunKey{RunID: "run-provider-unsafe-" + test.name, Generation: 1}
			store := newPiProviderActionStore(t, t.TempDir(), key)
			request := testPiProviderRequest()
			request.ActionKey = "unsafe-" + test.name
			actionID := "unsafe-action-" + test.name
			intent, err := piProviderActionIntent(actionID, request)
			if err != nil {
				t.Fatal(err)
			}
			intent.Outcome = test.outcome
			intent.Result = append(json.RawMessage(nil), test.result...)
			intent.FailureCode = test.failureCode
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			journal.ProviderActionIntents = []state.ProviderActionIntent{intent}
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}

			var calls atomic.Int32
			stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				calls.Add(1)
				return testPiProviderSuccess(), nil
			}}
			executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
			if err != nil {
				t.Fatal(err)
			}
			response := callPiProviderActionExecutor(t, executor, context.Background(), actionID, request)
			if response.Outcome != pi.ProviderBridgeOutcomeUnknown || response.FailureCode != piProviderActionFailureControlResult || len(response.Result) != 0 {
				t.Fatalf("unsafe replay response = %#v, want redacted unknown", response)
			}
			if strings.Contains(fmt.Sprintf("%#v", response), testPiProviderToken) {
				t.Fatalf("unsafe replay response leaked provider token: %#v", response)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("Control calls = %d, want zero for replay", got)
			}
		})
	}
}

func TestDurablePiProviderBridgeExecutorRejectsEscapedDuplicateSeededReplayResult(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-escaped-duplicate", Generation: 1}
	store := newPiProviderActionStore(t, t.TempDir(), key)
	intent, err := piProviderActionIntent("escaped-duplicate-action", testPiProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	intent.Outcome = state.ProviderActionOutcomeSucceeded
	intent.Result = json.RawMessage(`{"payload":"provider-token-do-not-leak","\u0070ayload":"safe"}`)

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.ProviderActionIntents = []state.ProviderActionIntent{intent}
	if err := store.SaveJournal(journal); err == nil {
		t.Fatal("SaveJournal accepted a provider result with escaped duplicate members")
	}

	persisted, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.ProviderActionIntents) != 0 {
		t.Fatalf("rejected provider result was persisted: %#v", persisted.ProviderActionIntents)
	}
}

func TestDurablePiProviderBridgeExecutorRejectsCapacityBeforeDispatch(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-capacity", Generation: 1}
	store := newPiProviderActionStore(t, t.TempDir(), key)
	resultPayload := `{"payload":"` + strings.Repeat("x", piProviderActionMaxResultBytes-len(`{"payload":""}`)) + `"}`
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 32; index++ {
		request := testPiProviderRequest()
		request.ActionKey = fmt.Sprintf("action-key-%d", index)
		intent, err := piProviderActionIntent(fmt.Sprintf("action-%d", index), request)
		if err != nil {
			t.Fatal(err)
		}
		intent.Outcome = state.ProviderActionOutcomeSucceeded
		intent.Result = json.RawMessage(resultPayload)
		journal.ProviderActionIntents = append(journal.ProviderActionIntents, intent)
	}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	replayRequest := testPiProviderRequest()
	replayRequest.ActionKey = "action-key-0"
	replayed := callPiProviderActionExecutor(t, executor, context.Background(), "action-0", replayRequest)
	if replayed.Outcome != pi.ProviderBridgeOutcomeSucceeded || string(replayed.Result) != resultPayload {
		t.Fatalf("over-budget exact replay = %#v, want safe stored success", replayed)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Control calls after exact replay = %d, want zero", got)
	}

	request := testPiProviderRequest()
	request.ActionKey = "action-key-overflow"
	response := callPiProviderActionExecutor(t, executor, context.Background(), "action-overflow", request)
	if response.Outcome != pi.ProviderBridgeOutcomeFailed || response.FailureCode != "local_action_rejected" {
		t.Fatalf("capacity response = %#v, want failed local rejection", response)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Control calls = %d, want zero before dispatch", got)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	stored := loaded.ProviderActionIntents[0]
	seeded := journal.ProviderActionIntents[0]
	if len(loaded.ProviderActionIntents) != 32 ||
		stored.ActionID != seeded.ActionID ||
		stored.ActionKey != seeded.ActionKey ||
		stored.RequestDigest != seeded.RequestDigest ||
		stored.ResourceID != seeded.ResourceID ||
		stored.Operation != seeded.Operation ||
		stored.Outcome != seeded.Outcome ||
		string(stored.Result) != string(seeded.Result) ||
		stored.FailureCode != seeded.FailureCode {
		t.Fatalf("over-budget journal changed: %#v", loaded.ProviderActionIntents)
	}
}

func TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-completion-reservation", Generation: 1}
	store := newPiProviderActionStore(t, t.TempDir(), key)
	base, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}

	first, err := piProviderActionIntent("completion-reservation-action-1", testPiProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := testPiProviderRequest()
	secondRequest.ActionKey = "completion-reservation-key-2"
	second, err := piProviderActionIntent("completion-reservation-action-2", secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	newRequest := testPiProviderRequest()
	newRequest.ActionKey = "completion-reservation-key-3"
	newIntent, err := piProviderActionIntent("completion-reservation-action-3", newRequest)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := []state.ProviderActionIntent{first, second}

	// Keep SaveJournal as the production serializer and limit oracle without
	// repeatedly writing multi-megabyte search candidates to disk.
	restoreWriter := store.SetAtomicWriterForTesting(func(string, []byte) error { return nil })
	low, high := 0, 4<<20
	for high-low > 1 {
		mid := low + (high-low)/2
		candidate := piProviderCapacityJournal(base, mid, append(append([]state.ProviderActionIntent(nil), unresolved...), newIntent))
		if err := store.SaveJournal(candidate); err == nil {
			low = mid
		} else {
			high = mid
		}
	}
	restoreWriter()
	nearLimit := piProviderCapacityJournal(base, low, unresolved)
	if err := store.SaveJournal(nearLimit); err != nil {
		t.Fatalf("SaveJournal() near completion-reservation limit = %v", err)
	}

	completionCandidate := piProviderCapacityJournal(base, low, append(append([]state.ProviderActionIntent(nil), unresolved...), newIntent))
	completionResult := json.RawMessage(`{"payload":"` + strings.Repeat("x", piProviderActionMaxResultBytes-len(`{"payload":""}`)) + `"}`)
	for index := range completionCandidate.ProviderActionIntents {
		completionCandidate.ProviderActionIntents[index].Outcome = state.ProviderActionOutcomeUnknown
		completionCandidate.ProviderActionIntents[index].Result = append(json.RawMessage(nil), completionResult...)
		completionCandidate.ProviderActionIntents[index].FailureCode = piProviderActionFailureControlUnknown
	}
	if err := store.SaveJournal(completionCandidate); err == nil {
		t.Fatal("maximum legal completion candidate unexpectedly fit the journal")
	}
	if err := store.SaveJournal(nearLimit); err != nil {
		t.Fatalf("restore near-limit journal = %v", err)
	}

	var calls atomic.Int32
	stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
		calls.Add(1)
		return testPiProviderSuccess(), nil
	}}
	executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
	if err != nil {
		t.Fatal(err)
	}
	response := callPiProviderActionExecutor(t, executor, context.Background(), newIntent.ActionID, newRequest)
	if response.Outcome != pi.ProviderBridgeOutcomeFailed || response.FailureCode != piProviderActionFailureLocalRejected {
		t.Fatalf("completion-reservation response = %#v, want local rejection", response)
	}
	if response.Outcome == pi.ProviderBridgeOutcomeUnknown {
		t.Fatal("capacity rejection was downgraded to unknown after a supposed success")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Control calls = %d, want zero before dispatch", got)
	}

	replayed := callPiProviderActionExecutor(t, executor, context.Background(), newIntent.ActionID, newRequest)
	if replayed.Outcome != pi.ProviderBridgeOutcomeFailed || replayed.FailureCode != piProviderActionFailureLocalRejected {
		t.Fatalf("completion-reservation retry = %#v, want local rejection", replayed)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Control calls after retry = %d, want zero", got)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != len(unresolved) {
		t.Fatalf("completion-reservation journal intents = %d, want %d", len(loaded.ProviderActionIntents), len(unresolved))
	}
}

func piProviderCapacityJournal(base state.RunJournal, payloadBytes int, intents []state.ProviderActionIntent) state.RunJournal {
	journal := base
	journal.LastEventSequence = 1
	journal.PendingEvents = []protocol.RunEvent{{
		EventID:    "provider-capacity-output",
		Sequence:   1,
		Kind:       "output",
		OccurredAt: time.Date(2026, time.September, 17, 1, 2, 3, 0, time.UTC),
		Payload:    json.RawMessage(`"` + strings.Repeat("x", payloadBytes) + `"`),
	}}
	journal.ProviderActionIntents = append([]state.ProviderActionIntent(nil), intents...)
	return journal
}

func TestDurablePiProviderBridgeExecutorRejectsInactiveLocalStateBeforeDispatch(t *testing.T) {
	for _, localState := range []string{"claiming", "paused", "cancelling", "stale"} {
		t.Run(localState, func(t *testing.T) {
			key := state.RunKey{RunID: "run-provider-inactive-" + localState, Generation: 1}
			store := newPiProviderActionStore(t, t.TempDir(), key)
			if _, err := store.SetLocalState(key, localState); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			stub := &piProviderActionControlStub{fn: func(context.Context, protocol.ProviderAccess, string, string, string, json.RawMessage) (control.ProviderActionResponse, error) {
				calls.Add(1)
				return testPiProviderSuccess(), nil
			}}
			executor, err := newDurablePiProviderBridgeExecutor(stub, store, key, testPiProviderAccess())
			if err != nil {
				t.Fatal(err)
			}
			response := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, testPiProviderRequest())
			if response.Outcome != pi.ProviderBridgeOutcomeFailed || response.FailureCode != "local_action_rejected" {
				t.Fatalf("inactive state response = %#v, want failed local rejection", response)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("Control calls = %d, want zero before dispatch", got)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if len(journal.ProviderActionIntents) != 0 {
				t.Fatalf("inactive state persisted provider intent: %#v", journal.ProviderActionIntents)
			}
		})
	}
}

func TestDurablePiProviderBridgeExecutorReplaysAfterLocalStateBecomesStale(t *testing.T) {
	key := state.RunKey{RunID: "run-provider-stale-replay", Generation: 1}
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
	request := testPiProviderRequest()
	first := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if first.Outcome != pi.ProviderBridgeOutcomeSucceeded {
		t.Fatalf("initial provider response = %#v", first)
	}
	if _, err := store.SetLocalState(key, "stale"); err != nil {
		t.Fatal(err)
	}
	replayed := callPiProviderActionExecutor(t, executor, context.Background(), testPiProviderActionID, request)
	if replayed.Outcome != first.Outcome || string(replayed.Result) != string(first.Result) {
		t.Fatalf("stale provider replay = %#v, want %#v", replayed, first)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Control calls = %d, want one initial dispatch", got)
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
		Outcome:    control.ProviderActionSucceeded,
		Result:     json.RawMessage(`{"operation":"change.upsert","resource_id":"` + testPiProviderResource + `","projected":true,"work_item_id":"00000000-0000-4000-8000-000000000003","delivery":{"url":"https://example.test/change"}}`),
		Operation:  "change.upsert",
		ResourceID: testPiProviderResource,
		WorkItemID: "00000000-0000-4000-8000-000000000003",
		Projected:  true,
		Delivery:   json.RawMessage(`{"url":"https://example.test/change"}`),
	}
}

func writeAppStateAtomic(path string, data []byte) error {
	// The state package owns the production atomic replace implementation. This
	// injected writer commits the bytes before returning a synthetic lost ACK.
	return os.WriteFile(path, data, 0o600)
}
