package state

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestPrepareProvideInputRejectsChangedPayloadForConfirmedCommandID(t *testing.T) {
	store := mustStore(t)
	j := testJournal("provide-input-idempotence", 1)
	j.LocalState = "waiting_for_input"
	if err := store.SaveJournal(j); err != nil {
		t.Fatal(err)
	}

	original := InputCommandIntent{
		CommandID:           "command-1",
		PayloadDigest:       strings.Repeat("a", sha256.Size*2),
		RunningTransitionID: "running-1",
		AckID:               "ack-1",
	}
	prepared, created, err := store.PrepareProvideInput(j.Key(), original)
	if err != nil || !created {
		t.Fatalf("PrepareProvideInput(original) created=%t error=%v", created, err)
	}
	confirmed := *prepared.InputCommandIntent
	if _, err := store.CompleteProvideInput(j.Key(), original.CommandID, original.PayloadDigest, "applied"); err != nil {
		t.Fatalf("CompleteProvideInput() error=%v", err)
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(j.Key(), []string{original.AckID}); err != nil {
		t.Fatalf("MarkCommandAcknowledgementsDelivered() error=%v", err)
	}
	if _, err := store.SetLocalState(j.Key(), "waiting_for_input"); err != nil {
		t.Fatalf("SetLocalState() error=%v", err)
	}
	confirmed.Outcome = "applied"
	confirmed.AcknowledgementDelivered = true

	changed := original
	changed.PayloadDigest = strings.Repeat("b", sha256.Size*2)
	changed.RunningTransitionID = "running-2"
	changed.AckID = "ack-2"
	if _, created, err := store.PrepareProvideInput(j.Key(), changed); err == nil || created {
		t.Fatalf("PrepareProvideInput(changed) created=%t error=%v, want fail-closed conflict", created, err)
	}

	loaded, err := store.LoadJournal(j.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InputCommandIntent == nil || *loaded.InputCommandIntent != confirmed {
		t.Fatalf("changed payload replaced original intent: %#v", loaded.InputCommandIntent)
	}

	if _, created, err := store.PrepareProvideInput(j.Key(), original); err != nil || created {
		t.Fatalf("PrepareProvideInput(exact replay) created=%t error=%v", created, err)
	}
}
