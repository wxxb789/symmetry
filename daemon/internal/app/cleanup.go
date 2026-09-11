package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const cleanupRetryMinimum = 30 * time.Second

func (daemon *daemon) runCleanup(ctx context.Context) {
	for {
		daemon.flushCleanups(ctx)
		timer := daemon.timer(daemon.cleanupWait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-daemon.cleanupSignal():
			timer.Stop()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) cleanupSignal() <-chan struct{} {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.cleanupWake
}

func (daemon *daemon) enqueueCleanup(key state.RunKey) {
	daemon.mu.Lock()
	if daemon.cleanupQueued == nil {
		daemon.cleanupQueued = make(map[state.RunKey]struct{})
	}
	daemon.cleanupQueued[key] = struct{}{}
	wake := daemon.cleanupWake
	daemon.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (daemon *daemon) cleanupWorkerActive() bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.cleanupWake != nil && daemon.background != nil
}

func (daemon *daemon) scheduleCleanup(ctx context.Context, journal state.RunJournal) error {
	if journal.HasProcessDetails() {
		// The terminal outbox may be accepted while its owning process remains
		// unproven. Capacity is releasable, but cleanup and journal deletion are
		// not until the exact persisted marker is cleared.
		daemon.releaseSlotOnce(journal.Key())
		daemon.enqueueCleanup(journal.Key())
		return nil
	}
	if journal.NativeUsageRecoveryRequired {
		return errors.New("native usage recovery remains pending")
	}
	if !daemon.releaseCleanupIfReady(journal.Key()) {
		return nil
	}
	daemon.enqueueCleanup(journal.Key())
	if daemon.cleanupWorkerActive() {
		return nil
	}
	if journal.HasPendingGoalDeliveries() {
		return nil
	}
	return daemon.cleanupPending(ctx, journal)
}

func (daemon *daemon) releaseCleanupIfReady(key state.RunKey) bool {
	daemon.releaseSlotOnce(key)
	if daemon.cleanupBlocked(key) {
		return false
	}
	daemon.releaseRun(key)
	return true
}

func (daemon *daemon) cleanupBlocked(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active != nil && active.cleanupBlocked
}

func (daemon *daemon) releaseCleanupAfterProcessExit(key state.RunKey) {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || journal.HasProcessDetails() {
		return
	}
	if journal.LocalState == "terminal_pending" {
		daemon.signalOutboxFor(key)
		return
	}
	if journal.LocalState != "cleanup_pending" {
		return
	}
	if err := daemon.scheduleCleanup(context.Background(), journal); err != nil {
		daemon.log.Warn("schedule_process_exit_cleanup_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
	}
}

func (daemon *daemon) enqueueRecoveredCleanups() {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		daemon.log.Warn("list_cleanup_journals_failed", "error", err)
		return
	}
	for _, journal := range journals {
		if journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" || (!journal.GoalDeliveryEnabled && journal.LocalState == "terminal_pending" && journal.HasProcessDetails()) {
			daemon.enqueueCleanup(journal.Key())
		}
	}
}

func (daemon *daemon) cleanupWait() time.Duration {
	now := daemon.now()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if len(daemon.cleanupQueued) == 0 {
		return maximumInterval
	}
	var next time.Time
	for key := range daemon.cleanupQueued {
		retryAt := daemon.cleanupRetry[key]
		if retryAt.IsZero() || !now.Before(retryAt) {
			return 0
		}
		if next.IsZero() || retryAt.Before(next) {
			next = retryAt
		}
	}
	return next.Sub(now)
}

func (daemon *daemon) cleanupDueKeys() []state.RunKey {
	now := daemon.now()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	keys := make([]state.RunKey, 0, len(daemon.cleanupQueued))
	for key := range daemon.cleanupQueued {
		retryAt := daemon.cleanupRetry[key]
		if retryAt.IsZero() || !now.Before(retryAt) {
			keys = append(keys, key)
		}
	}
	return keys
}

func (daemon *daemon) completeCleanup(key state.RunKey) {
	daemon.mu.Lock()
	delete(daemon.cleanupQueued, key)
	delete(daemon.cleanupRetry, key)
	daemon.mu.Unlock()
}

func (daemon *daemon) retryCleanup(key state.RunKey) {
	daemon.mu.Lock()
	if daemon.cleanupRetry == nil {
		daemon.cleanupRetry = make(map[state.RunKey]time.Time)
	}
	if _, queued := daemon.cleanupQueued[key]; queued {
		daemon.cleanupRetry[key] = daemon.now().Add(cleanupRetryMinimum)
	}
	daemon.mu.Unlock()
}

func (daemon *daemon) flushCleanups(ctx context.Context) {
	for _, key := range daemon.cleanupDueKeys() {
		if ctx.Err() != nil {
			return
		}
		journal, err := daemon.store.LoadJournal(key)
		if state.IsNotFound(err) {
			daemon.forgetRecoveredProcessStop(key)
			daemon.forgetWorkspaceRetention(key)
			daemon.completeCleanup(key)
			continue
		}
		if err != nil {
			daemon.retryCleanup(key)
			continue
		}
		if !journal.GoalDeliveryEnabled && journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
			daemon.mu.Lock()
			active := daemon.running[key]
			staleStopped := active != nil && active.stale && active.processStopWitness != nil && active.processStopWitness == active.process
			if staleStopped && journal.HasProcessDetails() {
				staleStopped = active.processStopPID == journal.PID && active.processStopIdentity == journal.ProcessIdentity
			}
			daemon.mu.Unlock()
			if staleStopped {
				journal, err = daemon.store.SetLocalState(key, "stale")
				if err != nil {
					daemon.retryCleanup(key)
					continue
				}
			}
		}
		if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
			daemon.completeCleanup(key)
			continue
		}
		if !journal.GoalDeliveryEnabled {
			ready, clearErr := daemon.resolveCleanupProcessMarker(ctx, journal)
			if clearErr != nil {
				if ctx.Err() == nil {
					daemon.retryCleanup(key)
				}
				continue
			}
			if !ready {
				daemon.retryCleanup(key)
				continue
			}
			if journal, err = daemon.store.LoadJournal(key); err != nil {
				if state.IsNotFound(err) {
					daemon.forgetWorkspaceRetention(key)
					daemon.completeCleanup(key)
				} else {
					daemon.retryCleanup(key)
				}
				continue
			}
		}
		if journal.LocalState == "terminal_pending" {
			daemon.signalOutboxFor(key)
			daemon.completeCleanup(key)
			continue
		}
		if !daemon.releaseCleanupIfReady(key) {
			daemon.retryCleanup(key)
			continue
		}
		if err := daemon.cleanupPending(ctx, journal); err != nil {
			if ctx.Err() == nil {
				daemon.retryCleanup(key)
			}
			continue
		}
		daemon.completeCleanup(key)
	}
}

// resolveCleanupProcessMarker clears a marker only after an exact in-memory
// Wait witness or an identity-bound recovered process stop proves its exit.
func (daemon *daemon) resolveCleanupProcessMarker(ctx context.Context, journal state.RunJournal) (bool, error) {
	key := journal.Key()
	daemon.mu.Lock()
	active := daemon.running[key]
	hasWitness := active != nil && active.processStopWitness != nil && active.processStopWitness == active.process
	if hasWitness && journal.HasProcessDetails() {
		hasWitness = active.processStopPID > 0 && active.processStopIdentity != "" && active.processStopPID == journal.PID && active.processStopIdentity == journal.ProcessIdentity
	}
	daemon.mu.Unlock()
	if active != nil {
		if !hasWitness {
			return false, nil
		}
		if !journal.HasProcessDetails() {
			daemon.clearCleanupBlockedAfterStop(key, active)
			return true, nil
		}
		if _, err := daemon.clearPersistedProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
			cleared, readErr := daemon.processMarkerCleared(key)
			if readErr != nil {
				return false, readErr
			}
			if !cleared {
				return false, err
			}
		}
		daemon.clearCleanupBlockedAfterStop(key, active)
		return true, nil
	}
	if !journal.HasProcessDetails() {
		return true, nil
	}
	if err := daemon.stopPersistedProcess(ctx, journal); err != nil {
		return false, err
	}
	if _, err := daemon.clearPersistedProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
		cleared, readErr := daemon.processMarkerCleared(key)
		if readErr != nil {
			return false, readErr
		}
		if !cleared {
			return false, err
		}
	}
	return true, nil
}

func (daemon *daemon) clearCleanupBlockedAfterStop(key state.RunKey, expected *runningRun) {
	daemon.mu.Lock()
	if daemon.running[key] == expected && expected.processStopWitness == expected.process {
		expected.cleanupBlocked = false
	}
	daemon.mu.Unlock()
}

func (daemon *daemon) processMarkerCleared(key state.RunKey) (bool, error) {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false, err
	}
	if journal.HasProcessDetails() {
		return false, nil
	}
	daemon.forgetRecoveredProcessStop(key)
	return true, nil
}

func (daemon *daemon) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if daemon.background != nil {
		ctx = daemon.background
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, daemon.config.CleanupTimeout())
}

func (daemon *daemon) cleanupRecoveredWorkspace(ctx context.Context, journal state.RunJournal, succeeded bool) error {
	if journal.RetainWorkspace || journal.TerminalState == "cancelled" || daemon.workspaceRetentionRemembered(journal.Key()) ||
		(journal.TerminalState != "completed" && supervisoryRecoveryRequired(journal)) {
		return nil
	}
	if journal.WorkspacePath == "" && !journal.WorkspaceRecoveryRequired {
		return nil
	}
	if journal.WorkspaceBindingKey == "" {
		return errors.New("recovered workspace binding key is missing")
	}
	retained, err := daemon.retainedGoalSessionWorkspace(journal.Key())
	if err != nil {
		return err
	}
	if retained {
		return nil
	}
	cleanupContext, cancel := daemon.cleanupContext(ctx)
	defer cancel()
	prepared, err := daemon.workspace.Recover(cleanupContext, journal.WorkspaceBindingKey, workspace.RunRef{RunID: journal.RunID, Generation: journal.Generation}, journal.WorkspacePath)
	if err != nil {
		return err
	}
	return daemon.workspace.Cleanup(cleanupContext, prepared, succeeded)
}

func (daemon *daemon) enterCleanupPending(journal state.RunJournal) (state.RunJournal, error) {
	updated, err := daemon.store.EnterCleanupPending(journal.Key())
	if err != nil {
		return state.RunJournal{}, err
	}
	return updated, nil
}

func (daemon *daemon) cleanupPending(ctx context.Context, journal state.RunJournal) error {
	if journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
		return nil
	}
	if journal.HasPendingGoalDeliveries() {
		return errors.New("Goal delivery remains pending")
	}
	if goalArtifactReceiptRequired(journal) {
		return errors.New("Goal candidate artifact acceptance or publish receipt remains pending")
	}
	if goalRecovery, err := daemon.goalRecoveryEvidenceRequired(journal); err != nil {
		return err
	} else if goalRecovery {
		return errors.New("Goal native recovery evidence remains pending")
	}
	if journal.HasProcessDetails() {
		return errors.New("process identity remains pending")
	}
	if err := daemon.cleanupRecoveredWorkspace(ctx, journal, journal.LocalState == "cleanup_pending" && journal.TerminalState == "completed"); err != nil {
		daemon.log.Warn("cleanup_terminal_workspace_failed", "run_id", journal.RunID, "error", err)
		return err
	}
	if err := daemon.store.DeleteJournal(journal.Key()); err != nil && !state.IsNotFound(err) {
		daemon.log.Warn("delete_terminal_journal_failed", "run_id", journal.RunID, "error", err)
		return err
	}
	daemon.forgetRecoveredProcessStop(journal.Key())
	daemon.forgetWorkspaceRetention(journal.Key())
	daemon.clearCompletedCommandReceipts(journal.Key())
	return nil
}

// goalArtifactReceiptRequired keeps a successful candidate completion distinct
// from a durably published candidate artifact. Terminal delivery only records
// that the process result reached control; it is not outcome acceptance.
func goalArtifactReceiptRequired(journal state.RunJournal) bool {
	if !journal.GoalDeliveryEnabled || journal.TerminalState != "completed" {
		return false
	}
	admission, present, err := parseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Purpose != protocol.AdmissionPurposeImplement {
		return err != nil || !present
	}
	switch journal.TerminalTaskResultKind {
	case protocol.TaskResultCandidateCompletion:
		// A candidate can only be cleaned after the immutable artifact receipt is
		// delivered under the same run fence.
	case protocol.TaskResultProgress,
		protocol.TaskResultBlocked,
		protocol.TaskResultRepairRequired,
		protocol.TaskResultReplanRequired,
		protocol.TaskResultFailed,
		protocol.TaskResultPlanProposed:
		return false
	default:
		// A missing marker could be an older or interrupted candidate completion.
		// Retain the workspace rather than infer that no artifact was proposed.
		return true
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryEvidence && delivery.Evidence != nil && delivery.Evidence.Kind == protocol.EvidenceArtifact {
			return false
		}
	}
	return true
}

func (daemon *daemon) goalRecoveryEvidenceRequired(journal state.RunJournal) (bool, error) {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return true, fmt.Errorf("list Goal sessions before cleanup: %w", err)
	}
	for _, session := range sessions {
		if session.RunID != journal.RunID || session.Generation != journal.Generation {
			continue
		}
		if session.NeedsReconciliation() {
			return true, nil
		}
		if session.SessionState == state.GoalSessionStateClosed {
			continue
		}
		if session.SessionState == state.GoalSessionStateAvailable && retainedGoalSessionAvailable(session) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (daemon *daemon) retainedGoalSessionWorkspace(key state.RunKey) (bool, error) {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return false, fmt.Errorf("list Goal sessions before workspace cleanup: %w", err)
	}
	for _, session := range sessions {
		owner := session.WorkspaceOwnerRunKey
		if owner.RunID == "" || owner.Generation <= 0 {
			// Journals written before workspace ownership was explicit still own
			// the workspace associated with their recorded execution. Retain
			// conservatively rather than deleting a resumable legacy worktree.
			owner = state.WorkspaceOwnerRunKey{RunID: session.RunID, Generation: session.Generation}
		}
		// A retained workspace is owned by the Run that first materialized it,
		// not by the current execution fields. Resume deliberately rotates those
		// fields to a new Run while continuing to use the original worktree.
		if owner.RunID != key.RunID || owner.Generation != key.Generation {
			continue
		}
		current, loadErr := daemon.store.LoadJournal(state.RunKey{RunID: session.RunID, Generation: session.Generation})
		if loadErr == nil && current.RetainWorkspace {
			return true, nil
		}
		if loadErr != nil && !state.IsNotFound(loadErr) {
			return false, fmt.Errorf("load retained Goal session current Run before workspace cleanup: %w", loadErr)
		}
		if session.SessionState != state.GoalSessionStateClosed && session.LaunchState != state.GoalSessionLaunchStateClosed {
			return true, nil
		}
	}
	return false, nil
}
