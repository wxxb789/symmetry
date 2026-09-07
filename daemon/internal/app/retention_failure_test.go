package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// This fake models an automatic cleanup profile: calling Cleanup destroys the
// actual artifact. Retention must prevent the call, not merely alter success.
type artifactDeletingWorkspace struct {
	artifact string
	cleanups int
}

func (service *artifactDeletingWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: filepath.Dir(service.artifact)}, nil
}

func (service *artifactDeletingWorkspace) Recover(context.Context, string, workspace.RunRef, string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: filepath.Dir(service.artifact)}, nil
}

func (service *artifactDeletingWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	service.cleanups++
	return os.Remove(service.artifact)
}

func retainTestArtifact(t *testing.T, app *daemon, key state.RunKey) *artifactDeletingWorkspace {
	t.Helper()
	service := &artifactDeletingWorkspace{artifact: filepath.Join(t.TempDir(), "useful-result.txt")}
	if err := os.WriteFile(service.artifact, []byte("preserve useful work"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.SetWorkspacePath(key, filepath.Dir(service.artifact)); err != nil {
		t.Fatal(err)
	}
	app.workspace = service
	return service
}

func assertArtifactRetained(t *testing.T, service *artifactDeletingWorkspace) {
	t.Helper()
	data, err := os.ReadFile(service.artifact)
	if err != nil || string(data) != "preserve useful work" || service.cleanups != 0 {
		t.Fatalf("artifact was cleaned: data=%q error=%v cleanup calls=%d", data, err, service.cleanups)
	}
}

func TestRetentionWriteFailureNeverPreventsCancellationOrLeaseFencing(t *testing.T) {
	for _, scenario := range []string{"cancel", "expired_lease", "ownership_lost"} {
		t.Run(scenario, func(t *testing.T) {
			app, key, process := supervisoryDaemon(t)
			app.cleanupWake = make(chan struct{}, 1)
			app.background = context.Background()
			artifact := retainTestArtifact(t, app, key)
			if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
				t.Fatal("pause intent not prepared")
			}
			retentionAttempts := 0
			app.options.retainWorkspace = func(got state.RunKey) (state.RunJournal, error) {
				retentionAttempts++
				if got != key || process.terminations != 1 {
					t.Fatalf("retention persistence preceded fencing: key=%#v terminations=%d", got, process.terminations)
				}
				return state.RunJournal{}, os.ErrPermission
			}
			switch scenario {
			case "cancel":
				if !app.handleCommand(context.Background(), supervisoryCommand(key, "cancel-1", "cancel")) {
					t.Fatal("retention write failure blocked cancellation")
				}
			case "expired_lease":
				journal := supervisoryJournal(t, app, key)
				journal.LeaseExpiresAt = time.Now().Add(-time.Minute)
				if err := app.store.SaveJournal(journal); err != nil {
					t.Fatal(err)
				}
				app.renewLeases(context.Background())
			case "ownership_lost":
				ownershipLost := &control.APIError{Code: control.OwnershipLost}
				if err := app.handleOrdinaryDeliveryError(context.Background(), supervisoryJournal(t, app, key), ownershipLost, "ownership lost"); !errors.Is(err, ownershipLost) {
					t.Fatalf("ownership failure changed: %v", err)
				}
			}
			journal := supervisoryJournal(t, app, key)
			active := app.runningRun(key)
			if process.terminations != 1 || retentionAttempts != 1 || active == nil || !active.cancelled || (scenario != "cancel" && !active.stale) || !app.workspaceRetentionRemembered(key) {
				t.Fatalf("fencing failed: journal=%#v active=%#v terminations=%d retention attempts=%d", journal, active, process.terminations, retentionAttempts)
			}
			if scenario == "cancel" {
				if err := app.flushRun(context.Background(), journal); err != nil {
					t.Fatal(err)
				}
				journal = supervisoryJournal(t, app, key)
				if journal.LocalState != "cleanup_pending" {
					t.Fatalf("cancellation did not settle: %#v", journal)
				}
			} else if journal.LocalState != "stale" {
				t.Fatalf("expired ownership not fenced durably: %#v", journal)
			}
			app.releaseRun(key)
			if !app.workspaceRetentionRemembered(key) {
				t.Fatal("releaseRun forgot retention before cleanup")
			}
			if err := app.cleanupPending(context.Background(), journal); err != nil {
				t.Fatal(err)
			}
			assertArtifactRetained(t, artifact)
			if _, err := app.store.LoadJournal(key); !state.IsNotFound(err) {
				t.Fatalf("journal not retired after safe cleanup: %v", err)
			}
			if app.workspaceRetentionRemembered(key) {
				t.Fatal("successful journal retirement leaked retention marker")
			}
		})
	}
}

func TestCancellationStopsWhileRetentionAndTerminalJournalWritesFail(t *testing.T) {
	app, key, process := supervisoryDaemon(t)
	artifact := retainTestArtifact(t, app, key)
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
		t.Fatal("durable pause intent not prepared")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.options.retainWorkspace = func(state.RunKey) (state.RunJournal, error) {
		if process.terminations != 1 {
			t.Fatal("retention failure delayed process termination")
		}
		return state.RunJournal{}, os.ErrPermission
	}
	app.options.queueCancelledTransitionAndAcknowledgement = func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error) {
		if process.terminations != 1 {
			t.Fatal("terminal persistence preceded process termination")
		}
		cancel()
		return state.RunJournal{}, os.ErrPermission
	}
	if app.handleCommand(ctx, supervisoryCommand(key, "cancel-1", "cancel")) {
		t.Fatal("unwritable receipt was acknowledged")
	}
	journal := supervisoryJournal(t, app, key)
	if process.terminations != 1 || !app.runningRun(key).cancelled || journal.RetainWorkspace || journal.TerminalState != "" || !app.workspaceRetentionRemembered(key) {
		t.Fatalf("write failures bypassed mandatory stop: %#v", journal)
	}
	if err := app.cleanupRecoveredWorkspace(context.Background(), journal, false); err != nil {
		t.Fatal(err)
	}
	assertArtifactRetained(t, artifact)
	if !app.workspaceRetentionRemembered(key) {
		t.Fatal("unsettled journal lost its retention marker")
	}
}

func TestRestartRetentionWriteFailureStillTerminatesAndPreservesDurableControlArtifacts(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	artifact := retainTestArtifact(t, app, key)
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
		t.Fatal("pause intent not prepared")
	}
	if _, err := app.store.SetProcessDetails(key, 71, "agent:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	app.running = make(map[state.RunKey]*runningRun)
	terminated, retentionAttempts := 0, 0
	app.options.terminatePersist = func(pid int, identity string) error {
		if pid != 71 || identity != "agent:71" {
			t.Fatalf("wrong persisted process: %d %q", pid, identity)
		}
		terminated++
		return nil
	}
	app.options.retainWorkspace = func(state.RunKey) (state.RunJournal, error) {
		retentionAttempts++
		if terminated != 1 {
			t.Fatal("recovery retention write preceded process termination")
		}
		return state.RunJournal{}, os.ErrPermission
	}
	if err := app.recoverUnresolvedInputIntents(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal := supervisoryJournal(t, app, key)
	if terminated != 1 || retentionAttempts != 1 || journal.TerminalState != "failed" || journal.RetainWorkspace || len(journal.ControlCommandIntents) != 1 || journal.ControlCommandIntents[0].Outcome != "rejected" {
		t.Fatalf("unsafe recovery under failed retention persistence: %#v, terminated=%d attempts=%d", journal, terminated, retentionAttempts)
	}
	// A second restart loses memory-only retention. The durable control intent
	// must independently stop an automatic cleanup profile from deleting work.
	app.retainedWorkspaces = nil
	if err := app.cleanupRecoveredWorkspace(context.Background(), journal, false); err != nil {
		t.Fatal(err)
	}
	assertArtifactRetained(t, artifact)
}
