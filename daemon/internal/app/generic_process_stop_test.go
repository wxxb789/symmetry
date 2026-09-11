package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestGenericStopErrorsRetainRecoveryBarrierAfterTerminalAcceptance(t *testing.T) {
	for _, test := range []struct {
		name   string
		result execution.Result
	}{
		{name: "containment", result: execution.Result{ContainmentError: errors.New("owned group remained")}},
		{name: "termination", result: execution.Result{TerminationError: errors.New("termination remained unproven")}},
	} {
		for _, marker := range []bool{true, false} {
			name := test.name + "/without_marker"
			if marker {
				name = test.name + "/with_marker"
			}
			t.Run(name, func(t *testing.T) {
				store, key := claimedStore(t)
				defer store.Close()
				if marker {
					if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
				active := &runningRun{process: fakeProcess{result: test.result}, cleanupBlocked: true, slotHeld: true}
				app := &daemon{
					store: store, control: &fakeControl{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)),
					running: map[state.RunKey]*runningRun{key: active}, slots: make(chan struct{}, 1),
					options: options{newID: ids(), clock: time.Now},
				}
				app.slots <- struct{}{}
				app.waitForRunWithContext(context.Background(), key)
				app.flushAll(context.Background())
				app.flushCleanups(context.Background())
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				if journal.HasProcessDetails() != marker || !journal.RetainWorkspace || journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted {
					t.Fatalf("unproven stop lost its durable recovery barrier: marker=%t retain=%t state=%s verdict=%s", journal.HasProcessDetails(), journal.RetainWorkspace, journal.LocalState, journal.TerminalVerdict)
				}
				if app.runningRun(key) != active || !active.cleanupBlocked || len(app.slots) != 0 {
					t.Fatalf("terminal capacity and physical cleanup were conflated: active=%t blocked=%t slots=%d", app.runningRun(key) == active, active.cleanupBlocked, len(app.slots))
				}
			})
		}
	}
}
