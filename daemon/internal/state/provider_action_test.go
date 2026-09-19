package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	testProviderActionID     = "00000000-0000-4000-8000-000000000001"
	testProviderActionDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testProviderResourceID   = "00000000-0000-4000-8000-000000000002"
)

func TestProviderActionIntentPersistsBeforeDispatchAndReplaysTerminalOutcome(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-action", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}

	intent := testProviderActionIntent()
	prepared, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || !dispatch || len(prepared.ProviderActionIntents) != 1 {
		t.Fatalf("PrepareProviderAction() dispatch=%t journal=%#v error=%v", dispatch, prepared.ProviderActionIntents, err)
	}
	completed := intent
	completed.Outcome = ProviderActionOutcomeSucceeded
	completed.Result = json.RawMessage(`{"operation":"change.upsert","projected":true}`)
	if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
		t.Fatalf("CompleteProviderAction() error = %v", err)
	}

	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch {
		t.Fatalf("terminal replay dispatch=%t error=%v", dispatch, err)
	}
	if got := replayed.ProviderActionIntents[0]; !sameProviderActionIntent(got, completed) {
		t.Fatalf("terminal replay = %#v, want %#v", got, completed)
	}
	if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
		t.Fatalf("exact completion replay error = %v", err)
	}
}

func TestProviderActionIntentRejectsChangedRequestAndCompletion(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-conflict", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}

	changed := intent
	changed.RequestDigest = strings.Repeat("b", 64)
	if _, _, err := store.PrepareProviderAction(journal.Key(), changed); err == nil {
		t.Fatal("changed request reused provider action identity")
	}

	completed := intent
	completed.Outcome = ProviderActionOutcomeFailed
	completed.FailureCode = "control_rejected_409"
	if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
		t.Fatal(err)
	}
	changedCompletion := completed
	changedCompletion.FailureCode = "control_rejected_403"
	if _, err := store.CompleteProviderAction(journal.Key(), changedCompletion); err == nil {
		t.Fatal("conflicting provider action completion replaced the durable outcome")
	}
}

func TestProviderActionUnknownReplaysWithoutRedispatch(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-unknown", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	unknown := intent
	unknown.Outcome = ProviderActionOutcomeUnknown
	unknown.FailureCode = "control_action_unknown"
	if _, err := store.CompleteProviderAction(journal.Key(), unknown); err != nil {
		t.Fatal(err)
	}

	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch {
		t.Fatalf("unknown replay dispatch=%t error=%v", dispatch, err)
	}
	got := replayed.ProviderActionIntents[0]
	if !sameProviderActionIntent(got, unknown) {
		t.Fatalf("replayed provider action = %#v, want %#v", got, unknown)
	}
}

func TestRecoverProviderActionSettlesOnlyMatchingUnfinishedDispatch(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-targeted-recovery", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	first := testProviderActionIntent()
	second := first
	second.ActionID = "00000000-0000-4000-8000-000000000003"
	second.ActionKey = "tool-call-2"
	second.RequestDigest = strings.Repeat("b", 64)
	for _, intent := range []ProviderActionIntent{first, second} {
		if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
			t.Fatal(err)
		}
	}

	recovered, err := store.RecoverProviderAction(journal.Key(), first.ActionID, first.RequestDigest, "local_persistence_unknown")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ProviderActionIntents[0].Outcome != ProviderActionOutcomeUnknown || recovered.ProviderActionIntents[1].Outcome != "" {
		t.Fatalf("targeted recovery = %#v", recovered.ProviderActionIntents)
	}
	if _, err := store.RecoverProviderAction(journal.Key(), first.ActionID, strings.Repeat("c", 64), "local_persistence_unknown"); err == nil {
		t.Fatal("targeted recovery accepted a changed request digest")
	}
}

func TestRecoverProviderActionsMakesUnfinishedDispatchUnknownAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := testJournal("run-provider-restart", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
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
	recovered, err := restarted.RecoverProviderActions(journal.Key(), "daemon_restart_unknown")
	if err != nil {
		t.Fatal(err)
	}
	got := recovered.ProviderActionIntents[0]
	if got.Outcome != ProviderActionOutcomeUnknown || got.FailureCode != "daemon_restart_unknown" || len(got.Result) != 0 {
		t.Fatalf("recovered provider action = %#v", got)
	}
	if _, err := restarted.RecoverProviderActions(journal.Key(), "daemon_restart_unknown"); err != nil {
		t.Fatalf("recovery replay error = %v", err)
	}
}

func TestProviderActionPrepareRecoversAfterCommitAcknowledgementLoss(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-lost-ack", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	failed := false
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if !failed {
			failed = true
			if err := writeAtomic(path, data); err != nil {
				return err
			}
			return errors.New("injected acknowledgement loss")
		}
		return writeAtomic(path, data)
	})
	intent := testProviderActionIntent()
	if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err == nil {
		t.Fatal("PrepareProviderAction() ignored acknowledgement loss")
	}
	restore()

	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch || len(replayed.ProviderActionIntents) != 1 {
		t.Fatalf("prepare replay dispatch=%t intents=%#v error=%v", dispatch, replayed.ProviderActionIntents, err)
	}
}

func TestProviderActionExactReplayDoesNotRewriteJournal(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-read-only-replay", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
		t.Fatalf("initial prepare dispatch=%t error=%v", dispatch, err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("injected exact replay write failure")
	})
	defer restore()

	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch || len(replayed.ProviderActionIntents) != 1 || !sameProviderActionIntent(replayed.ProviderActionIntents[0], intent) {
		t.Fatalf("exact replay dispatch=%t intents=%#v error=%v", dispatch, replayed.ProviderActionIntents, err)
	}
	if writes != 0 {
		t.Fatalf("exact replay writer calls = %d, want zero", writes)
	}
}

func TestProviderActionSettlementReplayDoesNotRewriteJournal(t *testing.T) {
	t.Run("completion", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-completion-read-only", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		intent := testProviderActionIntent()
		if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
			t.Fatal(err)
		}
		completed := intent
		completed.Outcome = ProviderActionOutcomeSucceeded
		completed.Result = json.RawMessage(`{"projected":true}`)
		if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
			t.Fatal(err)
		}

		var writes int
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
			writes++
			return errors.New("injected completion replay write failure")
		})
		defer restore()
		if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
			t.Fatalf("exact completion replay error = %v", err)
		}
		if writes != 0 {
			t.Fatalf("exact completion replay writer calls = %d, want zero", writes)
		}
	})

	t.Run("targeted recovery", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-targeted-recovery-read-only", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		intent := testProviderActionIntent()
		if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecoverProviderAction(journal.Key(), intent.ActionID, intent.RequestDigest, "local_persistence_unknown"); err != nil {
			t.Fatal(err)
		}

		var writes int
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
			writes++
			return errors.New("injected targeted recovery replay write failure")
		})
		defer restore()
		if _, err := store.RecoverProviderAction(journal.Key(), intent.ActionID, intent.RequestDigest, "local_persistence_unknown"); err != nil {
			t.Fatalf("exact targeted recovery replay error = %v", err)
		}
		if writes != 0 {
			t.Fatalf("exact targeted recovery replay writer calls = %d, want zero", writes)
		}
	})

	t.Run("bulk recovery", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-bulk-recovery-read-only", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		intent := testProviderActionIntent()
		if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RecoverProviderActions(journal.Key(), "daemon_restart_unknown"); err != nil {
			t.Fatal(err)
		}

		var writes int
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
			writes++
			return errors.New("injected bulk recovery replay write failure")
		})
		defer restore()
		if _, err := store.RecoverProviderActions(journal.Key(), "daemon_restart_unknown"); err != nil {
			t.Fatalf("exact bulk recovery replay error = %v", err)
		}
		if writes != 0 {
			t.Fatalf("exact bulk recovery replay writer calls = %d, want zero", writes)
		}
	})
}

func TestProviderActionExactReplaySkipsTerminalAndCapacityChecks(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-terminal-replay-read-only", 1)
		preparedIntent := testProviderActionIntent()
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		if _, dispatch, err := store.PrepareProviderAction(journal.Key(), preparedIntent); err != nil || !dispatch {
			t.Fatalf("initial terminal prepare dispatch=%t error=%v", dispatch, err)
		}
		completed := preparedIntent
		completed.Outcome = ProviderActionOutcomeSucceeded
		completed.Result = json.RawMessage(`{"projected":true}`)
		if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
			t.Fatal(err)
		}
		terminal, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{
			TransitionID: "terminal-replay-read-only",
			State:        "completed",
			Payload:      json.RawMessage(`{"stage":"provider_action"}`),
		}, journal.StartedAt.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		journal = terminal

		var writes int
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
			writes++
			return errors.New("injected terminal replay write failure")
		})
		defer restore()

		replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), preparedIntent)
		if err != nil || dispatch || len(replayed.ProviderActionIntents) != 1 || !sameProviderActionIntent(replayed.ProviderActionIntents[0], completed) {
			t.Fatalf("terminal exact replay dispatch=%t intents=%#v error=%v", dispatch, replayed.ProviderActionIntents, err)
		}
		if writes != 0 {
			t.Fatalf("terminal exact replay writer calls = %d, want zero", writes)
		}

		newIntent := testProviderActionIntent()
		newIntent.ActionID = "terminal-new-action"
		newIntent.ActionKey = "terminal-new-key"
		newIntent.RequestDigest = strings.Repeat("b", 64)
		if _, dispatch, err := store.PrepareProviderAction(journal.Key(), newIntent); err == nil || dispatch {
			t.Fatalf("new terminal action dispatch=%t error=%v, want rejection", dispatch, err)
		}
		if writes != 0 {
			t.Fatalf("new terminal action writer calls = %d, want zero", writes)
		}
	})

	t.Run("capacity", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-capacity-replay-read-only", 1)
		for index := 0; index < 33; index++ {
			intent := testProviderActionIntent()
			intent.ActionID = fmt.Sprintf("capacity-action-%d", index)
			intent.ActionKey = fmt.Sprintf("capacity-key-%d", index)
			intent.RequestDigest = fmt.Sprintf("%064x", index+1)
			journal.ProviderActionIntents = append(journal.ProviderActionIntents, intent)
		}
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}

		var writes int
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
			writes++
			return errors.New("injected capacity replay write failure")
		})
		defer restore()

		existing := journal.ProviderActionIntents[0]
		replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), existing)
		if err != nil || dispatch || len(replayed.ProviderActionIntents) != 33 {
			t.Fatalf("capacity exact replay dispatch=%t intents=%d error=%v", dispatch, len(replayed.ProviderActionIntents), err)
		}
		if writes != 0 {
			t.Fatalf("capacity exact replay writer calls = %d, want zero", writes)
		}
	})
}

func TestTerminalTransitionSettlesUnfinishedProviderActionAtomically(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-terminal", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, _, err := store.PrepareProviderAction(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "failed-provider-action",
		State:        "failed",
		Payload:      json.RawMessage(`{"stage":"provider_action"}`),
	}, time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	got := terminal.ProviderActionIntents[0]
	if got.Outcome != ProviderActionOutcomeUnknown || got.FailureCode != providerActionFailureTerminal {
		t.Fatalf("terminal provider action = %#v", got)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || !sameProviderActionIntent(loaded.ProviderActionIntents[0], got) {
		t.Fatalf("durable terminal provider action = %#v, error=%v", loaded.ProviderActionIntents, err)
	}
	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch || len(replayed.ProviderActionIntents) != 1 || !sameProviderActionIntent(replayed.ProviderActionIntents[0], got) {
		t.Fatalf("terminal provider action replay dispatch=%t intents=%#v error=%v", dispatch, replayed.ProviderActionIntents, err)
	}
}

func TestProviderActionCapacityAllowsThirtyThirdSmallAction(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-capacity", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 33; index++ {
		intent := testProviderActionIntent()
		intent.ActionID = "action-" + strconv.Itoa(index)
		intent.ActionKey = "tool-call-" + strconv.Itoa(index)
		intent.RequestDigest = fmt.Sprintf("%064x", index+1)
		if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
			t.Fatalf("prepare %d dispatch=%t error=%v", index, dispatch, err)
		}
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 33 {
		t.Fatalf("small provider action count = %d, want 33", len(loaded.ProviderActionIntents))
	}
}

func TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded(t *testing.T) {
	store := mustStore(t)
	base := testJournal("run-provider-byte-capacity", 1)
	intent := testProviderActionIntent()

	// Find the smallest valid output payload that makes the candidate journal
	// cross the same serialized limit enforced by saveJournalLocked.
	low, high := 0, maxJournalFileBytes
	for low < high {
		mid := low + (high-low)/2
		candidate := journalWithProviderCapacityPayload(base, mid)
		candidate.ProviderActionIntents = []ProviderActionIntent{intent}
		if serializedJournalBytes(t, candidate) <= maxJournalFileBytes {
			low = mid + 1
		} else {
			high = mid
		}
	}
	nearLimit := journalWithProviderCapacityPayload(base, low)
	if serializedJournalBytes(t, nearLimit) > maxJournalFileBytes {
		t.Fatal("journal without provider action exceeded the byte limit")
	}
	if err := store.SaveJournal(nearLimit); err != nil {
		t.Fatalf("SaveJournal() near byte limit = %v", err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected over-budget provider action write")
	})
	defer restore()

	if _, dispatch, err := store.PrepareProviderAction(nearLimit.Key(), intent); !errors.Is(err, ErrProviderActionCapacity) || dispatch {
		t.Fatalf("over-budget dispatch=%t error=%v, want capacity rejection", dispatch, err)
	}
	if writes != 0 {
		t.Fatalf("over-budget writer calls = %d, want zero", writes)
	}
	loaded, err := store.LoadJournal(nearLimit.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 0 {
		t.Fatalf("over-budget journal retained %d provider actions, want zero", len(loaded.ProviderActionIntents))
	}
}

func TestProviderActionCapacityRejectsProviderBytesBeforeDispatch(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-action-byte-capacity", 1)
	resultPayload := `{"payload":"` + strings.Repeat("x", maxProviderActionResultBytes-len(`{"payload":""}`)) + `"}`
	for index := 0; index < 32; index++ {
		intent := testProviderActionIntent()
		intent.ActionID = fmt.Sprintf("provider-byte-action-%d", index)
		intent.ActionKey = fmt.Sprintf("provider-byte-key-%d", index)
		intent.RequestDigest = fmt.Sprintf("%064x", index+1)
		intent.Outcome = ProviderActionOutcomeSucceeded
		intent.Result = json.RawMessage(resultPayload)
		journal.ProviderActionIntents = append(journal.ProviderActionIntents, intent)
	}
	providerData, err := json.Marshal(journal.ProviderActionIntents)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerData) <= maxProviderActionBytes {
		t.Fatalf("provider action seed bytes = %d, want > %d", len(providerData), maxProviderActionBytes)
	}
	if serializedJournalBytes(t, journal) > maxJournalFileBytes {
		t.Fatal("provider action seed exceeded the full journal byte limit")
	}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	replayRequest := journal.ProviderActionIntents[0]
	replayRequest.Outcome = ""
	replayRequest.Result = nil
	replayRequest.FailureCode = ""
	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), replayRequest)
	if err != nil || dispatch || !sameProviderActionIntent(replayed.ProviderActionIntents[0], journal.ProviderActionIntents[0]) {
		t.Fatalf("over-budget exact replay dispatch=%t error=%v", dispatch, err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected provider action byte-budget write")
	})
	defer restore()

	newIntent := testProviderActionIntent()
	newIntent.ActionID = "provider-byte-overflow"
	newIntent.ActionKey = "provider-byte-overflow"
	newIntent.RequestDigest = strings.Repeat("f", 64)
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), newIntent); !errors.Is(err, ErrProviderActionCapacity) || dispatch {
		t.Fatalf("provider-byte overflow dispatch=%t error=%v, want capacity rejection", dispatch, err)
	}
	if writes != 0 {
		t.Fatalf("provider-byte overflow writer calls = %d, want zero", writes)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 32 {
		t.Fatalf("provider-byte overflow retained %d actions, want 32", len(loaded.ProviderActionIntents))
	}
}

func TestProviderActionFailureCodeCapacityUsesWorstCaseJSONEncoding(t *testing.T) {
	worst := maxProviderFailureCodeForCapacity()
	if !validRequiredString(worst, maxProviderFailureCodeBytes) {
		t.Fatal("worst-case failure code was rejected by the current state contract")
	}
	worstJSON, err := serializeJSONWithoutHTMLEscaping(worst)
	if err != nil {
		t.Fatalf("serializeJSONWithoutHTMLEscaping(worst) error = %v", err)
	}
	if got, want := len(worstJSON), 2+6*maxProviderFailureCodeBytes; got != want {
		t.Fatalf("worst-case failure code JSON bytes = %d, want %d", got, want)
	}
	plainJSON, err := serializeJSONWithoutHTMLEscaping(strings.Repeat("x", maxProviderFailureCodeBytes))
	if err != nil {
		t.Fatalf("serializeJSONWithoutHTMLEscaping(plain) error = %v", err)
	}
	if got, want := len(plainJSON), 2+maxProviderFailureCodeBytes; got != want {
		t.Fatalf("plain failure code JSON bytes = %d, want %d", got, want)
	}
	if got, want := len(worstJSON)-len(plainJSON), 1280; got != want {
		t.Fatalf("worst-case failure code encoding delta = %d, want %d", got, want)
	}
}

func TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch(t *testing.T) {
	store := mustStore(t)
	base := testJournal("run-provider-completion-reservation", 1)
	intent := testProviderActionIntent()
	candidate := base
	candidate.ProviderActionIntents = []ProviderActionIntent{intent}
	nearLimit := journalAtProviderFailureCodeCapacityWindow(t, candidate)
	seed := nearLimit
	seed.ProviderActionIntents = nil
	if err := store.SaveJournal(seed); err != nil {
		t.Fatalf("SaveJournal() near completion-reservation limit = %v", err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected completion-reservation write")
	})
	defer restore()

	if _, dispatch, err := store.PrepareProviderAction(seed.Key(), intent); !errors.Is(err, ErrProviderActionCapacity) || dispatch {
		t.Fatalf("completion-reservation dispatch=%t error=%v, want capacity rejection", dispatch, err)
	}
	if writes != 0 {
		t.Fatalf("completion-reservation writer calls = %d, want zero", writes)
	}
	loaded, err := store.LoadJournal(nearLimit.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 0 || len(loaded.PendingEvents) != len(seed.PendingEvents) || loaded.LastEventSequence != seed.LastEventSequence {
		t.Fatalf("completion-reservation rejection changed journal: %#v", loaded)
	}
}

func TestProviderActionPrepareRejectsNotAllowedBeforeWriting(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-not-allowed-sentinel", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "provider-action-not-allowed",
		State:        "failed",
		Payload:      json.RawMessage(`{"stage":"provider_action"}`),
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected provider action write")
	})
	defer restore()

	intent := testProviderActionIntent()
	intent.ActionID = "not-allowed-action"
	intent.ActionKey = "not-allowed-key"
	intent.RequestDigest = strings.Repeat("b", 64)
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); !errors.Is(err, ErrProviderActionNotAllowed) || dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t error=%v, want not-allowed rejection", dispatch, err)
	}
	if writes != 0 {
		t.Fatalf("not-allowed writer calls = %d, want zero", writes)
	}
}

func TestProviderActionPrepareOnlyDispatchesInActiveLocalStates(t *testing.T) {
	for _, localState := range []string{"claimed", "running", "waiting_for_input"} {
		t.Run("allows "+localState, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-provider-active-"+localState, 1)
			journal.LocalState = localState
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, dispatch, err := store.PrepareProviderAction(journal.Key(), testProviderActionIntent()); err != nil || !dispatch {
				t.Fatalf("active state %q dispatch=%t error=%v", localState, dispatch, err)
			}
		})
	}

	for _, localState := range []string{"claiming", "paused", "cancelling", "stale"} {
		t.Run("rejects "+localState, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-provider-inactive-"+localState, 1)
			journal.LocalState = localState
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			var writes int
			restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
				writes++
				return errors.New("unexpected inactive provider action write")
			})
			defer restore()
			if _, dispatch, err := store.PrepareProviderAction(journal.Key(), testProviderActionIntent()); !errors.Is(err, ErrProviderActionNotAllowed) || dispatch {
				t.Fatalf("inactive state %q dispatch=%t error=%v", localState, dispatch, err)
			}
			if writes != 0 {
				t.Fatalf("inactive state %q writer calls = %d, want zero", localState, writes)
			}
		})
	}
}

func TestProviderActionExactReplayRemainsReadOnlyAfterLocalStateBecomesStale(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-stale-replay", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
		t.Fatalf("initial prepare dispatch=%t error=%v", dispatch, err)
	}
	if _, err := store.SetLocalState(journal.Key(), "stale"); err != nil {
		t.Fatal(err)
	}
	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected stale replay write")
	})
	defer restore()
	replayed, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || dispatch || len(replayed.ProviderActionIntents) != 1 {
		t.Fatalf("stale exact replay dispatch=%t intents=%#v error=%v", dispatch, replayed.ProviderActionIntents, err)
	}
	if writes != 0 {
		t.Fatalf("stale exact replay writer calls = %d, want zero", writes)
	}
}

func TestDeleteJournalBlocksUnresolvedProviderActionsAndCleansResolvedOutcomes(t *testing.T) {
	t.Run("stale unresolved action remains durable until recovery", func(t *testing.T) {
		store := mustStore(t)
		journal := stoppedTestJournal("run-provider-stale-cleanup", 1)
		journal.LocalState = "stale"
		journal.ProviderActionIntents = []ProviderActionIntent{testProviderActionIntent()}
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}

		if err := store.DeleteJournal(journal.Key()); err == nil || !strings.Contains(err.Error(), "provider action outcome remains pending") {
			t.Fatalf("DeleteJournal() error = %v, want unresolved provider action guard", err)
		}
		if _, err := store.LoadJournal(journal.Key()); err != nil {
			t.Fatalf("unresolved stale journal was removed: %v", err)
		}

		if _, err := store.RecoverProviderAction(journal.Key(), testProviderActionID, testProviderActionDigest, "stale_cleanup_unknown"); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteJournal(journal.Key()); err != nil {
			t.Fatalf("DeleteJournal() after durable unknown outcome = %v", err)
		}
	})

	for _, test := range []struct {
		name        string
		outcome     string
		result      string
		failureCode string
	}{
		{name: "succeeded", outcome: ProviderActionOutcomeSucceeded, result: `{"projected":true}`},
		{name: "failed", outcome: ProviderActionOutcomeFailed, failureCode: "control_rejected_409"},
		{name: "unknown", outcome: ProviderActionOutcomeUnknown, failureCode: "control_action_unknown"},
	} {
		t.Run("cleanup pending "+test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-provider-cleanup-"+test.name, 1)
			journal.LocalState = "cleanup_pending"
			journal.TerminalState = "failed"
			journal.TerminalPendingAt = time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
			journal.TerminalVerdict = TerminalVerdictOwnershipLost
			journal.TerminalResolvedAt = time.Date(2026, 9, 15, 1, 2, 4, 0, time.UTC)
			journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
			intent := testProviderActionIntent()
			intent.ActionID = "cleanup-" + test.name
			intent.ActionKey = "cleanup-key-" + test.name
			intent.Outcome = test.outcome
			intent.Result = []byte(test.result)
			intent.FailureCode = test.failureCode
			journal.ProviderActionIntents = []ProviderActionIntent{intent}
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if err := store.DeleteJournal(journal.Key()); err != nil {
				t.Fatalf("DeleteJournal() with durable %s outcome = %v", test.outcome, err)
			}
		})
	}
}

func TestRunJournalDecodeRejectsDuplicateProviderActionMembers(t *testing.T) {
	intentJSON, err := json.Marshal(testProviderActionIntent())
	if err != nil {
		t.Fatal(err)
	}
	nestedIntent := fmt.Sprintf(
		`{"action_id":%q,"action_key":%q,"request_digest":%q,"resource_id":%q,"operation":%q,"outcome":"","outcome":"succeeded","result":{"projected":true}}`,
		testProviderActionID,
		"tool-call-1",
		testProviderActionDigest,
		testProviderResourceID,
		"change.upsert",
	)

	for _, test := range []struct {
		name    string
		members string
	}{
		{
			name:    "top-level",
			members: `"provider_action_intents":[` + string(intentJSON) + `],"provider_action_intents":[]`,
		},
		{
			name:    "escaped top-level",
			members: `"provider_action_intents":[` + string(intentJSON) + `],"\u0070rovider_action_intents":[]`,
		},
		{
			name:    "nested intent",
			members: `"provider_action_intents":[` + nestedIntent + `]`,
		},
		{
			name:    "malformed duplicate",
			members: `"provider_action_intents":[` + string(intentJSON) + `],"provider_action_intents":[`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := stoppedTestJournal("run-provider-duplicate-"+strings.ReplaceAll(test.name, " ", "-"), 1)
			journal.LocalState = "stale"
			encoded, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			raw := strings.TrimSuffix(string(encoded), "}") + "," + test.members + "}"
			if err := os.WriteFile(store.journalPath(journal.Key()), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := store.LoadJournal(journal.Key()); err == nil {
				t.Fatal("LoadJournal() accepted duplicate provider action members")
			}
			if err := store.DeleteJournal(journal.Key()); err == nil {
				t.Fatal("DeleteJournal() accepted duplicate provider action members")
			}
			if _, err := os.Stat(store.journalPath(journal.Key())); err != nil {
				t.Fatalf("duplicate journal was removed: %v", err)
			}
		})
	}
}

func TestRunJournalDecodeRejectsCaseInsensitiveProviderActionAliases(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  func(RunJournal) string
	}{
		{
			name: "top-level last-wins alias",
			raw: func(journal RunJournal) string {
				encoded, _ := json.Marshal(journal)
				return strings.TrimSuffix(string(encoded), "}") + `,"PROVIDER_ACTION_INTENTS":[]}`
			},
		},
		{
			name: "nested provider action alias",
			raw: func(journal RunJournal) string {
				journal.ProviderActionIntents = []ProviderActionIntent{testProviderActionIntent()}
				encoded, _ := json.Marshal(journal)
				raw := strings.Replace(string(encoded), `"action_id":"`+testProviderActionID+`"`, `"ACTION_ID":"`+testProviderActionID+`"`, 1)
				return raw
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := stoppedTestJournal("run-provider-case-alias-"+strings.ReplaceAll(test.name, " ", "-"), 1)
			journal.LocalState = "stale"
			raw := test.raw(journal)
			if err := os.WriteFile(store.journalPath(journal.Key()), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.LoadJournal(journal.Key()); err == nil {
				t.Fatal("LoadJournal() accepted a case-insensitive provider action alias")
			}
			if err := store.DeleteJournal(journal.Key()); err == nil {
				t.Fatal("DeleteJournal() accepted a case-insensitive provider action alias")
			}
			if _, err := os.Stat(store.journalPath(journal.Key())); err != nil {
				t.Fatalf("case-alias journal was removed: %v", err)
			}
		})
	}
}

func TestRunJournalDecodeAcceptsExactProviderActionTags(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("run-provider-exact-tags", 1)
	completed := testProviderActionIntent()
	completed.Outcome = ProviderActionOutcomeSucceeded
	completed.Result = json.RawMessage(`{"projected":true}`)
	journal.ProviderActionIntents = []ProviderActionIntent{completed}
	encoded, err := serializeRunJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(journal.Key()), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() rejected exact tags: %v", err)
	}
	if len(loaded.ProviderActionIntents) != 1 || !sameProviderActionIntent(loaded.ProviderActionIntents[0], completed) {
		t.Fatalf("exact-tag provider action = %#v, want %#v", loaded.ProviderActionIntents, completed)
	}
}

func TestProviderActionResultRejectsDuplicateMembersBeforePersistence(t *testing.T) {
	for _, test := range []struct {
		name   string
		result json.RawMessage
	}{
		{name: "top-level", result: json.RawMessage(`{"projected":false,"projected":true}`)},
		{name: "nested", result: json.RawMessage(`{"metadata":{"source":"first","source":"second"}}`)},
		{name: "escaped", result: json.RawMessage(`{"\u0070rojected":false,"projected":true}`)},
	} {
		t.Run("SaveJournal rejects "+test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-provider-result-save-"+test.name, 1)
			intent := testProviderActionIntent()
			intent.Outcome = ProviderActionOutcomeSucceeded
			intent.Result = test.result
			journal.ProviderActionIntents = []ProviderActionIntent{intent}
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("SaveJournal() accepted duplicate provider action result members")
			}
		})

		t.Run("CompleteProviderAction rejects "+test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-provider-result-complete-"+test.name, 1)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			intent := testProviderActionIntent()
			if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
				t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
			}
			completed := intent
			completed.Outcome = ProviderActionOutcomeSucceeded
			completed.Result = test.result
			if _, err := store.CompleteProviderAction(journal.Key(), completed); err == nil {
				t.Fatal("CompleteProviderAction() accepted duplicate provider action result members")
			}
		})
	}

	t.Run("valid projected result remains accepted", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-provider-result-valid", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		intent := testProviderActionIntent()
		if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
			t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
		}
		completed := intent
		completed.Outcome = ProviderActionOutcomeSucceeded
		completed.Result = json.RawMessage(`{"projected":true,"metadata":{"source":"control"}}`)
		if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
			t.Fatalf("CompleteProviderAction() rejected valid projected result: %v", err)
		}
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || !sameProviderActionIntent(loaded.ProviderActionIntents[0], completed) {
			t.Fatalf("durable projected result = %#v, error = %v", loaded.ProviderActionIntents, err)
		}
	})
}

func TestProviderActionResultSemanticNormalizationPreservesOrderNumbersAndLiteralEscapes(t *testing.T) {
	input := json.RawMessage(`{
  "z": -0.00,
  "value": "\u003c\u003e\u0026",
  "literal": "\\u003c",
  "line": "\u2028\u2029"
}`)
	want := json.RawMessage("{\"z\":-0.00,\"value\":\"<>&\",\"literal\":\"\\\\u003c\",\"line\":\"\u2028\u2029\"}")
	normalized, err := normalizeProviderActionResult(input)
	if err != nil {
		t.Fatalf("normalizeProviderActionResult() error = %v", err)
	}
	if !bytes.Equal(normalized, want) {
		t.Fatalf("normalized result = %q, want %q", normalized, want)
	}
	if !validProviderActionResult(input) {
		t.Fatal("validProviderActionResult() rejected a semantically valid object")
	}

	changedNumber := json.RawMessage(`{"z": 0, "value": "<>&", "literal": "\\u003c", "line": "  "}`)
	if providerActionResultsEqual(input, changedNumber) {
		t.Fatal("providerActionResultsEqual() treated distinct numeric lexical forms as equal")
	}
	changedOrder := json.RawMessage(`{"value":"<>&","z":-0.00,"literal":"\\u003c","line":"  "}`)
	if providerActionResultsEqual(input, changedOrder) {
		t.Fatal("providerActionResultsEqual() treated reordered object members as equal")
	}
}

func TestProviderActionNewCompletionRejectsSemanticallySmallRawLargeResult(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-provider-new-raw-bound", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
	}
	rawLarge := json.RawMessage(`{"payload":"` + strings.Repeat(`\u003c`, maxProviderActionResultBytes/4) + `"}`)
	normalized, err := normalizeProviderActionResult(rawLarge)
	if err != nil || len(normalized) > maxProviderActionResultBytes {
		t.Fatalf("raw-large result is not semantically small: normalized=%d error=%v", len(normalized), err)
	}
	if len(rawLarge) <= maxProviderActionResultBytes {
		t.Fatalf("raw-large result bytes = %d, want over %d", len(rawLarge), maxProviderActionResultBytes)
	}
	if err := ValidateNewProviderActionResult(rawLarge); err == nil {
		t.Fatal("ValidateNewProviderActionResult() accepted a raw-large result")
	}
	completed := intent
	completed.Outcome = ProviderActionOutcomeSucceeded
	completed.Result = rawLarge
	if _, err := store.CompleteProviderAction(journal.Key(), completed); err == nil {
		t.Fatal("CompleteProviderAction() accepted a raw-large new completion")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProviderActionIntents[0].Outcome != "" || len(loaded.ProviderActionIntents[0].Result) != 0 {
		t.Fatalf("rejected completion changed unresolved intent: %#v", loaded.ProviderActionIntents[0])
	}
}

func TestProviderActionLegacyEscapedCompletionReplaysWithoutRewrite(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	key := RunKey{RunID: "run-provider-legacy-escaped-replay", Generation: 1}
	journal := testJournal(key.RunID, key.Generation)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(key, intent); err != nil || !dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
	}
	legacy := intent
	legacy.Outcome = ProviderActionOutcomeSucceeded
	legacy.Result = json.RawMessage(`{"payload":"\u003c\u003e\u0026"}`)
	legacyJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	legacyJournal.ProviderActionIntents[0] = legacy
	encoded, err := json.Marshal(legacyJournal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(key), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	var writes int
	restore := restarted.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected legacy replay rewrite")
	})
	defer restore()
	newRepresentation := intent
	newRepresentation.Outcome = ProviderActionOutcomeSucceeded
	newRepresentation.Result = json.RawMessage(`{"payload":"<>&"}`)
	if _, err := restarted.CompleteProviderAction(key, newRepresentation); err != nil {
		t.Fatalf("semantic completion replay error = %v", err)
	}
	if writes != 0 {
		t.Fatalf("semantic completion replay writer calls = %d, want zero", writes)
	}
}

func TestProviderActionLegacyOversizedEscapedResultLoadsAndReplaysWithoutRewrite(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	key := RunKey{RunID: "run-provider-legacy-oversized-replay", Generation: 1}
	journal := testJournal(key.RunID, key.Generation)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(key, intent); err != nil || !dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
	}
	legacy := intent
	legacy.Outcome = ProviderActionOutcomeSucceeded
	legacy.Result = json.RawMessage(`{"payload":"` + strings.Repeat(`\u003c`, maxProviderActionResultBytes/4) + `"}`)
	if len(legacy.Result) <= maxProviderActionResultBytes {
		t.Fatalf("legacy result bytes = %d, want over %d", len(legacy.Result), maxProviderActionResultBytes)
	}
	normalized, err := normalizeProviderActionResult(legacy.Result)
	if err != nil || len(normalized) > maxProviderActionResultBytes {
		t.Fatalf("legacy result is not semantically within historical bound: normalized=%d error=%v", len(normalized), err)
	}
	legacyJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	legacyJournal.ProviderActionIntents[0] = legacy
	encoded, err := json.Marshal(legacyJournal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(key), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() legacy oversized result error = %v", err)
	}
	if string(loaded.ProviderActionIntents[0].Result) != string(legacy.Result) {
		t.Fatalf("legacy result changed on load: got %d bytes, want %d", len(loaded.ProviderActionIntents[0].Result), len(legacy.Result))
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	var writes int
	restore := restarted.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected legacy oversized replay rewrite")
	})
	defer restore()
	if _, err := restarted.CompleteProviderAction(key, legacy); err != nil {
		t.Fatalf("legacy oversized exact completion replay error = %v", err)
	}
	if writes != 0 {
		t.Fatalf("legacy oversized exact replay writer calls = %d, want zero", writes)
	}
}

func TestProviderActionMaximallyHTMLEscapedResultSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := testJournal("run-provider-html-escaped-result", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}

	intent := testProviderActionIntent()
	if _, dispatch, err := store.PrepareProviderAction(journal.Key(), intent); err != nil || !dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t error=%v", dispatch, err)
	}
	result := json.RawMessage(`{"payload":"` + strings.Repeat("<", maxProviderActionResultBytes-len(`{"payload":""}`)) + `"}`)
	if len(result) != maxProviderActionResultBytes {
		t.Fatalf("result bytes = %d, want %d", len(result), maxProviderActionResultBytes)
	}
	completed := intent
	completed.Outcome = ProviderActionOutcomeSucceeded
	completed.Result = result
	if _, err := store.CompleteProviderAction(journal.Key(), completed); err != nil {
		t.Fatalf("CompleteProviderAction() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after restart error = %v", err)
	}
	if len(loaded.ProviderActionIntents) != 1 || !sameProviderActionIntent(loaded.ProviderActionIntents[0], completed) {
		t.Fatalf("reloaded provider action = %#v, want %#v", loaded.ProviderActionIntents, completed)
	}
	if len(loaded.ProviderActionIntents[0].Result) > maxProviderActionResultBytes || !json.Valid(loaded.ProviderActionIntents[0].Result) || protocol.RejectDuplicateJSONMembers(loaded.ProviderActionIntents[0].Result) != nil {
		t.Fatalf("reloaded provider action result is not a valid bounded raw JSON value: %d bytes", len(loaded.ProviderActionIntents[0].Result))
	}
}

func TestProviderActionMutationPreservesUnresolvedCompletionCapacity(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base := testJournal("run-provider-mutation-reservation", 1)
	if err := store.SaveJournal(base); err != nil {
		t.Fatal(err)
	}
	intent := testProviderActionIntent()
	prepared, dispatch, err := store.PrepareProviderAction(base.Key(), intent)
	if err != nil || !dispatch {
		t.Fatalf("PrepareProviderAction() dispatch=%t journal=%#v error=%v", dispatch, prepared.ProviderActionIntents, err)
	}

	mutated := journalAtProviderFailureCodeCapacityWindow(t, prepared)
	outputPayload := mutated.PendingEvents[0].Payload
	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected completion-capacity mutation write")
	})
	queued, dropped, err := store.QueueOutputEvent(base.Key(), protocol.RunEvent{
		EventID:    mutated.PendingEvents[0].EventID,
		Kind:       "output",
		OccurredAt: mutated.PendingEvents[0].OccurredAt,
		Payload:    outputPayload,
	}, len(outputPayload))
	restore()
	if !errors.Is(err, ErrProviderActionCapacity) || dropped || queued.RunID != "" {
		t.Fatalf("QueueOutputEvent() journal=%#v dropped=%t error=%v, want capacity rejection", queued, dropped, err)
	}
	if writes != 0 {
		t.Fatalf("completion-capacity mutation writer calls = %d, want zero", writes)
	}
	unchanged, err := store.LoadJournal(base.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.PendingEvents) != 0 || unchanged.LastEventSequence != prepared.LastEventSequence || len(unchanged.ProviderActionIntents) != 1 || !sameProviderActionIntent(unchanged.ProviderActionIntents[0], intent) {
		t.Fatalf("completion-capacity rejection changed journal: %#v", unchanged)
	}

	completed := intent
	completed.Outcome = ProviderActionOutcomeUnknown
	completed.Result = maxProviderActionResult()
	completed.FailureCode = maxProviderFailureCodeForCapacity()
	if _, err := store.CompleteProviderAction(base.Key(), completed); err != nil {
		t.Fatalf("CompleteProviderAction() after rejected output mutation error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(base.Key())
	if err != nil || len(loaded.ProviderActionIntents) != 1 || !sameProviderActionIntent(loaded.ProviderActionIntents[0], completed) || len(loaded.PendingEvents) != 0 || loaded.LastEventSequence != prepared.LastEventSequence {
		t.Fatalf("restarted maximum unknown provider action = %#v, error=%v", loaded, err)
	}
}

func TestProviderActionCompletionRejectsOverLimitWorstCaseUnknownWithoutMutation(t *testing.T) {
	store := mustStore(t)
	base := testJournal("run-provider-worst-case-completion", 1)
	intent := testProviderActionIntent()
	base.ProviderActionIntents = []ProviderActionIntent{intent}

	low, high := 0, maxJournalFileBytes
	for low < high {
		mid := low + (high-low)/2
		candidate := journalWithProviderCapacityPayload(base, mid)
		if serializedJournalBytes(t, candidate) <= maxJournalFileBytes {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low == 0 {
		t.Fatal("could not find an over-limit completion boundary")
	}
	seed := journalWithProviderCapacityPayload(base, low-1)
	if serializedJournalBytes(t, seed) > maxJournalFileBytes {
		t.Fatal("seed journal exceeded the journal limit")
	}
	completion := intent
	completion.Outcome = ProviderActionOutcomeUnknown
	completion.Result = maxProviderActionResult()
	completion.FailureCode = maxProviderFailureCodeForCapacity()
	completionCandidate := seed
	completionCandidate.ProviderActionIntents = []ProviderActionIntent{completion}
	if serializedJournalBytes(t, completionCandidate) <= maxJournalFileBytes {
		t.Fatal("seed journal left enough room for a worst-case unknown completion")
	}
	if err := store.SaveJournal(seed); err != nil {
		t.Fatalf("SaveJournal() seed error = %v", err)
	}

	var writes int
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return errors.New("unexpected over-limit completion write")
	})
	defer restore()
	if _, err := store.CompleteProviderAction(seed.Key(), completion); !errors.Is(err, ErrProviderActionCapacity) {
		t.Fatalf("CompleteProviderAction() error = %v, want capacity rejection", err)
	}
	if writes != 0 {
		t.Fatalf("over-limit completion writer calls = %d, want zero", writes)
	}
	loaded, err := store.LoadJournal(seed.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProviderActionIntents) != 1 || loaded.ProviderActionIntents[0].Outcome != "" || len(loaded.ProviderActionIntents[0].Result) != 0 || loaded.ProviderActionIntents[0].FailureCode != "" {
		t.Fatalf("over-limit completion mutated unresolved intent: %#v", loaded.ProviderActionIntents)
	}
}

func journalWithProviderCapacityPayload(base RunJournal, payloadBytes int) RunJournal {
	journal := base
	if journal.LastEventSequence < 2 {
		journal.LastEventSequence = 2
	}
	journal.PendingEvents = []protocol.RunEvent{{
		EventID:    "provider-capacity-output",
		Sequence:   2,
		Kind:       "output",
		OccurredAt: time.Date(2026, time.September, 17, 1, 2, 3, 0, time.UTC),
		Payload:    json.RawMessage(strconv.Quote(strings.Repeat("x", payloadBytes))),
	}}
	return journal
}

func journalAtProviderFailureCodeCapacityWindow(t *testing.T, base RunJournal) RunJournal {
	t.Helper()
	if len(base.ProviderActionIntents) != 1 || base.ProviderActionIntents[0].Outcome != "" {
		t.Fatal("capacity-window journal must contain exactly one unresolved provider action")
	}
	completion := func(journal RunJournal, failureCode string) RunJournal {
		journal.ProviderActionIntents = append([]ProviderActionIntent(nil), journal.ProviderActionIntents...)
		journal.ProviderActionIntents[0].Outcome = ProviderActionOutcomeUnknown
		journal.ProviderActionIntents[0].Result = maxProviderActionResult()
		journal.ProviderActionIntents[0].FailureCode = failureCode
		return journal
	}
	plainFailureCode := strings.Repeat("x", maxProviderFailureCodeBytes)
	low, high := 0, maxJournalFileBytes
	for low < high {
		mid := low + (high-low+1)/2
		candidate := completion(journalWithProviderCapacityPayload(base, mid), plainFailureCode)
		if serializedJournalBytes(t, candidate) <= maxJournalFileBytes {
			low = mid
		} else {
			high = mid - 1
		}
	}
	nearLimit := journalWithProviderCapacityPayload(base, low)
	plain := completion(nearLimit, plainFailureCode)
	worst := completion(nearLimit, maxProviderFailureCodeForCapacity())
	plainBytes := serializedJournalBytes(t, plain)
	worstBytes := serializedJournalBytes(t, worst)
	if got, want := worstBytes-plainBytes, 1280; got != want {
		t.Fatalf("provider completion failure-code encoding delta = %d, want %d", got, want)
	}
	if plainBytes > maxJournalFileBytes || worstBytes <= maxJournalFileBytes {
		t.Fatalf("provider completion capacity window plain=%d worst=%d limit=%d", plainBytes, worstBytes, maxJournalFileBytes)
	}
	if err := validateProviderActionJournalSize(nearLimit); err != nil {
		t.Fatalf("unresolved provider action candidate does not fit ordinary journal: %v", err)
	}
	return nearLimit
}

func serializedJournalBytes(t *testing.T, journal RunJournal) int {
	t.Helper()
	data, err := serializeRunJournal(journal)
	if err != nil {
		t.Fatalf("serializeRunJournal(journal) error = %v", err)
	}
	return len(data)
}

func testProviderActionIntent() ProviderActionIntent {
	return ProviderActionIntent{
		ActionID:      testProviderActionID,
		ActionKey:     "tool-call-1",
		RequestDigest: testProviderActionDigest,
		ResourceID:    testProviderResourceID,
		Operation:     "change.upsert",
	}
}
