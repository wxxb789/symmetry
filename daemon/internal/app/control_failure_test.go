package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/state"
)

type failingSupervisoryInputProcess struct {
	recordingProcess
}

func (process *failingSupervisoryInputProcess) WriteInput([]byte) error {
	if process.beforeWrite != nil {
		process.beforeWrite()
	}
	process.writes++
	return errors.New("stdin delivery failed")
}

func TestSupervisoryInputWriteFailureSettlesOnceWithoutChangingLifecycle(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	process := &failingSupervisoryInputProcess{}
	app.running[key].process = process
	process.beforeWrite = func() {
		journal := supervisoryJournal(t, app, key)
		if len(journal.ControlCommandIntents) != 1 || journal.ControlCommandIntents[0].Outcome != "" {
			t.Fatalf("stdin attempted without unresolved durable intent: %#v", journal)
		}
	}
	command := supervisoryCommand(key, "pause-1", "pause")
	if !app.handleCommand(context.Background(), command) || !app.handleCommand(context.Background(), command) {
		t.Fatal("failed stdin delivery did not settle")
	}
	journal := supervisoryJournal(t, app, key)
	if process.writes != 1 || journal.LocalState != "running" || len(journal.PendingTransitions) != 0 || len(journal.PendingEvents) != 1 || journal.PendingEvents[0].Kind != "command_applied" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "failed" || journal.ControlCommandIntents[0].Outcome != "failed" {
		t.Fatalf("stdin failure changed lifecycle or lost receipt: %#v, writes=%d", journal, process.writes)
	}
	ackID := journal.PendingCommandAcknowledgements[0].AckID
	if _, err := app.store.MarkCommandAcknowledgementsDelivered(key, []string{ackID}); err != nil {
		t.Fatal(err)
	}
	if !app.handleCommand(context.Background(), command) {
		t.Fatal("settled failed command replay did not converge")
	}
	if process.writes != 1 || len(supervisoryJournal(t, app, key).PendingCommandAcknowledgements) != 0 {
		t.Fatal("delivered failure was replayed to stdin")
	}
}

func TestPausedCleanProcessExitFailsAndPreservesArtifactsWhenRetentionWriteFails(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	artifact := retainTestArtifact(t, app, key)
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
		t.Fatal("pause intent not prepared")
	}
	supervisoryReceipt(t, app, key, "pause-1", "pause", "applied")
	retentionAttempts := 0
	app.options.retainWorkspace = func(state.RunKey) (state.RunJournal, error) {
		retentionAttempts++
		return state.RunJournal{}, os.ErrPermission
	}
	// recordingProcess.Wait returns successful exit. A paused worker cannot
	// turn that exit into completion and discard its recoverable artifacts.
	app.waitForRun(key)
	journal := supervisoryJournal(t, app, key)
	if journal.TerminalState != "failed" || journal.LocalState != "terminal_pending" || retentionAttempts != 1 || !app.workspaceRetentionRemembered(key) {
		t.Fatalf("paused success exit bypassed failure retention: %#v, retention attempts=%d", journal, retentionAttempts)
	}
	for _, transition := range journal.PendingTransitions {
		if transition.State == "completed" {
			t.Fatalf("paused process exit queued completed: %#v", journal.PendingTransitions)
		}
	}
	if err := app.cleanupRecoveredWorkspace(context.Background(), journal, false); err != nil {
		t.Fatal(err)
	}
	assertArtifactRetained(t, artifact)
}
