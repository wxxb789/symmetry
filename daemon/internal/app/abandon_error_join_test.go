package app

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	app.running[key] = &runningRun{starting: true, claimed: true}

	sentinel := errors.New("injected uncertain Goal session write failure")
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if strings.Contains(string(data), `"launch_state":"uncertain"`) {
			return sentinel
		}
		if err := writeAppStateAtomic(path, data); err != nil {
			return err
		}
		if strings.Contains(string(data), `"kind":"session_attach"`) && strings.Contains(string(data), `"ready":true`) {
			// Behind the ready barrier the run journal is corrupt, so the next
			// fence read fails and the admission abandons the native session.
			return os.WriteFile(path, []byte("{"), 0o600)
		}
		return nil
	})
	defer restoreWriter()

	err = app.startGoalAdmission(context.Background(), key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work,
	}, admission, nil)
	if err == nil {
		t.Fatal("admission returned no error after the run-journal fence read failed")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("admission error = %v, want the abandon-internal sentinel joined", err)
	}
	_, reloadErr := store.LoadJournal(key)
	if reloadErr == nil {
		t.Fatal("run journal decoded after the injected corruption")
	}
	primary := reloadErr.Error()
	want := "load native run fence: " + primary + "\n" + primary +
		"\npersist workspace retention: " + primary +
		"\nwrite goal session journal: " + sentinel.Error()
	if err.Error() != want {
		t.Fatalf("admission error = %q, want %q", err.Error(), want)
	}
	calls := session.callsSnapshot()
	if !slices.Contains(calls, "close") || slices.Contains(calls, "start_turn") {
		t.Fatalf("abandoned native session calls = %v", calls)
	}
}
