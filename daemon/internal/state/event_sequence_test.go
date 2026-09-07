package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestOutputAndControlReceiptAllocateOneMonotonicEventSequence(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal := testJournal("concurrent-output", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := controlIntent("guidance-1", "guidance")
	if _, _, err := store.PrepareControlCommand(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errors := make(chan error, 2)
	go func() {
		<-start
		for index := 0; index < 20; index++ {
			if _, err := store.QueueNextEvent(journal.Key(), protocol.RunEvent{EventID: fmt.Sprintf("output-%d", index), Kind: "progress", OccurredAt: time.Now().UTC(), Payload: []byte(`{"message":"continuing"}`)}); err != nil {
				errors <- err
				return
			}
		}
		errors <- nil
	}()
	go func() {
		<-start
		_, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "guidance", "failed", controlReceipt(intent.CommandID, "guidance", "failed"))
		errors <- err
	}()
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	updated, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.PendingEvents) != 21 || updated.LastEventSequence != journal.LastEventSequence+21 {
		t.Fatalf("events lost during concurrent receipt: %#v", updated)
	}
	for index, event := range updated.PendingEvents {
		if event.Sequence != journal.LastEventSequence+int64(index)+1 {
			t.Fatalf("non-monotonic sequence at %d: %#v", index, event)
		}
	}
}
