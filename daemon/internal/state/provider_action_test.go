package state

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
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

func TestProviderActionUnknownReopensOnlyForExactRequest(t *testing.T) {
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

	reopened, dispatch, err := store.PrepareProviderAction(journal.Key(), intent)
	if err != nil || !dispatch {
		t.Fatalf("unknown reopen dispatch=%t error=%v", dispatch, err)
	}
	got := reopened.ProviderActionIntents[0]
	if got.Outcome != "" || got.FailureCode != "" || len(got.Result) != 0 {
		t.Fatalf("reopened provider action = %#v", got)
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

func testProviderActionIntent() ProviderActionIntent {
	return ProviderActionIntent{
		ActionID:      testProviderActionID,
		ActionKey:     "tool-call-1",
		RequestDigest: testProviderActionDigest,
		ResourceID:    testProviderResourceID,
		Operation:     "change.upsert",
	}
}
