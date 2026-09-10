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
	daemon.mu.Lock()
	active := daemon.running[key]
	blocked := active != nil && active.cleanupBlocked
	daemon.mu.Unlock()
	if blocked {
		return false
	}
	daemon.releaseRun(key)
	return true
}

func (daemon *daemon) releaseCleanupAfterProcessExit(key state.RunKey) {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || journal.LocalState != "cleanup_pending" {
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
		if journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" {
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
			daemon.forgetWorkspaceRetention(key)
			daemon.completeCleanup(key)
			continue
		}
		if err != nil {
			daemon.retryCleanup(key)
			continue
		}
		if journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
			daemon.completeCleanup(key)
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
	if journal.GoalDeliveryEnabled && (journal.PID > 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero()) {
		return errors.New("Goal native process identity remains pending")
	}
	if err := daemon.cleanupRecoveredWorkspace(ctx, journal, journal.LocalState == "cleanup_pending" && journal.TerminalState == "completed"); err != nil {
		daemon.log.Warn("cleanup_terminal_workspace_failed", "run_id", journal.RunID, "error", err)
		return err
	}
	if err := daemon.store.DeleteJournal(journal.Key()); err != nil && !state.IsNotFound(err) {
		daemon.log.Warn("delete_terminal_journal_failed", "run_id", journal.RunID, "error", err)
		return err
	}
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
		if session.NeedsReconciliation() || session.SessionState != state.GoalSessionStateClosed {
			return true, nil
		}
	}
	return false, nil
}
