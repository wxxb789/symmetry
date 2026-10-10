package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	cleanupRetryMinimum       = 30 * time.Second
	processFinalizeRetryLimit = 3
)

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
	if daemon.deferStaleCleanupForPendingAcknowledgement(journal) {
		return nil
	}
	if journal.HasProcessDetails() {
		// The terminal outbox may be accepted while its owning process remains
		// unproven. Capacity is releasable, but cleanup and journal deletion are
		// not until the exact persisted marker is cleared.
		key := journal.Key()
		daemon.mu.Lock()
		active := daemon.running[key]
		startInFlight := active != nil && active.nativeStartInFlight
		daemon.mu.Unlock()
		if !startInFlight {
			daemon.releaseSlotOnce(key)
		}
		daemon.enqueueCleanup(key)
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

func (daemon *daemon) deferStaleCleanupForPendingAcknowledgement(journal state.RunJournal) bool {
	if journal.LocalState != "stale" || len(journal.PendingCommandAcknowledgements) == 0 {
		return false
	}
	key := journal.Key()
	daemon.mu.Lock()
	active := daemon.running[key]
	startInFlight := active != nil && active.nativeStartInFlight
	daemon.mu.Unlock()
	if !startInFlight {
		daemon.releaseSlotOnce(key)
	}
	daemon.signalOutboxFor(key)
	return true
}

func (daemon *daemon) releaseCleanupIfReady(key state.RunKey) bool {
	daemon.mu.Lock()
	active := daemon.running[key]
	startInFlight := active != nil && active.nativeStartInFlight
	blocked := active != nil && active.cleanupBlocked
	daemon.mu.Unlock()
	if startInFlight {
		return false
	}
	daemon.releaseSlotOnce(key)
	if blocked {
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
	if err != nil {
		return
	}
	if daemon.deferStaleCleanupForPendingAcknowledgement(journal) {
		return
	}
	if journal.HasProcessDetails() {
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
	active := daemon.running[key]
	if active != nil && active.processExited && active.process != nil && active.processStopWitness == nil && active.processFinalizeExhausted {
		// A failed finalizer is deliberately bounded per Process owner. Leave
		// the durable marker for restart recovery, but stop waking this daemon
		// forever on the same unresolved owner.
		delete(daemon.cleanupQueued, key)
		delete(daemon.cleanupRetry, key)
		daemon.mu.Unlock()
		return
	}
	if daemon.cleanupRetry == nil {
		daemon.cleanupRetry = make(map[state.RunKey]time.Time)
	}
	if _, queued := daemon.cleanupQueued[key]; queued {
		delay := cleanupRetryMinimum
		if active != nil && active.processExited && active.process != nil && active.processStopWitness == nil && active.processFinalizeAttempts > 1 {
			for attempt := 1; attempt < active.processFinalizeAttempts; attempt++ {
				delay *= 2
			}
		}
		daemon.cleanupRetry[key] = daemon.now().Add(delay)
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
		if daemon.deferStaleCleanupForPendingAcknowledgement(journal) {
			daemon.completeCleanup(key)
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
	if hasLinuxV2ContainmentState(journal) {
		daemon.mu.Lock()
		active := daemon.running[key]
		daemon.mu.Unlock()
		if active != nil {
			if active.processExited && active.process != nil && active.processStopWitness == active.process {
				latest, err := daemon.store.LoadJournal(key)
				if err != nil {
					return false, err
				}
				if !latest.HasProcessDetails() && !hasPendingLinuxV2Containment(latest) {
					return true, nil
				}
				return false, errors.New("Linux v2 process finalizer retained its exact owner marker")
			}
			if !active.processExited || active.process == nil {
				return false, nil
			}
			if finalized, err := daemon.finalizeExitedProcess(key, journal, active); err != nil {
				return false, err
			} else if !finalized {
				return false, nil
			}
			latest, err := daemon.store.LoadJournal(key)
			if err != nil {
				return false, err
			}
			if latest.HasProcessDetails() || hasPendingLinuxV2Containment(latest) {
				return false, errors.New("Linux v2 process finalizer did not clear the exact released owner")
			}
			return true, nil
		}
		if !journal.HasProcessDetails() && !hasPendingLinuxV2Containment(journal) {
			return true, nil
		}
		if err := daemon.stopPersistedProcess(ctx, journal); err != nil {
			return false, err
		}
		latest, err := daemon.store.LoadJournal(key)
		if err != nil {
			return false, err
		}
		if latest.HasProcessDetails() || hasPendingLinuxV2Containment(latest) {
			return false, errors.New("Linux v2 containment remains pending after exact recovery")
		}
		return true, nil
	}
	daemon.mu.Lock()
	active := daemon.running[key]
	hasWitness := active != nil && active.processStopWitness != nil && active.processStopWitness == active.process
	if hasWitness && journal.HasProcessDetails() {
		hasWitness = active.processStopPID > 0 && active.processStopIdentity != "" && active.processStopPID == journal.PID && active.processStopIdentity == journal.ProcessIdentity
	}
	daemon.mu.Unlock()
	if active != nil {
		if !hasWitness && active.processExited && active.process != nil {
			if finalized, finalizeErr := daemon.finalizeExitedProcess(key, journal, active); finalizeErr != nil {
				return false, finalizeErr
			} else if finalized {
				hasWitness = true
			}
		}
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

// finalizeExitedProcess is the only same-daemon retry path for a Process that
// already reported resultDone. It never calls Terminate and never waits for the
// child; the Process implementation owns only receipt persistence/release at
// this point. The owner identity is checked before and after the callback so a
// replacement Process cannot inherit an old finalization result.
func (daemon *daemon) finalizeExitedProcess(key state.RunKey, journal state.RunJournal, active *runningRun) (bool, error) {
	daemon.mu.Lock()
	if daemon.running[key] != active || active.process == nil || !active.processExited || active.processStopWitness != nil {
		daemon.mu.Unlock()
		return false, nil
	}
	process := active.process
	if active.processFinalizeOwner != process {
		active.processFinalizeOwner = process
		active.processFinalizeAttempts = 0
		active.processFinalizeExhausted = false
	}
	if active.processFinalizeExhausted {
		daemon.mu.Unlock()
		return false, errors.New("process containment finalization retry budget exhausted")
	}
	if active.processFinalizeAttempts >= processFinalizeRetryLimit {
		active.processFinalizeExhausted = true
		daemon.mu.Unlock()
		return false, errors.New("process containment finalization retry budget exhausted")
	}
	active.processFinalizeAttempts++
	daemon.mu.Unlock()

	finalizer, ok := process.(ProcessFinalizer)
	if !ok {
		err := errors.New("process containment finalizer is unavailable")
		daemon.mu.Lock()
		if daemon.running[key] == active && active.process == process && active.processFinalizeAttempts >= processFinalizeRetryLimit {
			active.processFinalizeExhausted = true
		}
		daemon.mu.Unlock()
		return false, err
	}
	if err := finalizer.FinalizeContainment(); err != nil {
		daemon.mu.Lock()
		if daemon.running[key] == active && active.process == process && active.processFinalizeAttempts >= processFinalizeRetryLimit {
			active.processFinalizeExhausted = true
		}
		daemon.mu.Unlock()
		return false, err
	}

	pid, identity := process.ProcessDetails()
	if pid != journal.PID || identity != journal.ProcessIdentity {
		return false, errors.New("process finalization owner does not match persisted process marker")
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.running[key] != active || active.process != process {
		return false, nil
	}
	active.processStopWitness = process
	active.processStopPID = pid
	active.processStopIdentity = identity
	active.cleanupBlocked = true
	active.processFinalizeExhausted = false
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
	if journal.LinuxCloneIntent != nil {
		return daemon.cleanupLinuxCloneWorkspace(ctx, journal)
	}
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

func (daemon *daemon) cleanupLinuxCloneWorkspace(ctx context.Context, journal state.RunJournal) error {
	if ctx == nil || daemon == nil || daemon.store == nil {
		return errors.New("Linux clone cleanup lacks its exact context, daemon, or Store")
	}
	if journal.LinuxCloneAbortProof != nil {
		if journal.LinuxCloneAbortPending || !linuxCloneAbortProofMatchesJournal(journal) {
			return errors.New("Linux clone abort tombstone does not match its completed no-target cleanup")
		}
		if journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired {
			return errors.New("Linux clone abort tombstone retained workspace fields")
		}
		return linuxCloneAbortRecovery(ctx, daemon, journal)
	}
	if journal.LinuxCloneAbortPending || linuxCloneAbortRecoveryEligible(journal) {
		if !linuxCloneAbortRecoveryEligible(journal) {
			return errors.New("Linux clone abort is unresolved after possible B execution; retain its fenced RunJournal")
		}
		if journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone abort is not eligible after checkout or promotion")
		}
		return linuxCloneAbortRecovery(ctx, daemon, journal)
	}
	if journal.LinuxCloneIntent == nil || journal.LinuxCloneSeedReceipt == nil ||
		journal.LinuxCloneCheckoutReceipt == nil || journal.LinuxClonePromotionReceipt == nil {
		return errors.New("Linux clone cleanup requires its exact workspace adapter and complete durable receipts")
	}
	key := journal.Key()
	fence := journal.Fence()
	if !linuxCloneCleanupJournalMatchesFence(journal, key, fence) {
		return errors.New("Linux clone cleanup journal does not match its exact RunKey and claim fence")
	}
	cloneWorkspace, ok := daemon.workspace.(linuxV2CloneWorkspace)
	if !ok {
		return errors.New("Linux clone cleanup requires its exact workspace adapter")
	}
	grant := authority.LinuxCloneGrant{
		Intent: journal.LinuxCloneIntent.Clone(), Seed: journal.LinuxCloneSeedReceipt.Clone(),
	}
	checkout := journal.LinuxCloneCheckoutReceipt.Clone()
	if err := grant.Validate(); err != nil || checkout.Validate() != nil ||
		checkout.RunID != key.RunID || checkout.Generation != key.Generation || checkout.Target != grant.Intent.Target ||
		checkout.BindingKey != grant.Intent.BindingKey || checkout.ResourceID != grant.Intent.ResourceID ||
		checkout.Nonce != grant.Intent.Nonce || checkout.SeedDigest != grant.Seed.Digest || checkout.BaseCommit != grant.Intent.BaseCommit {
		return errors.Join(errors.New("validate exact Linux clone cleanup grant and checkout receipt"), err)
	}

	latest, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read back Linux clone cleanup journal: %w", err)
	}
	if !linuxCloneCleanupJournalMatchesFence(latest, key, fence) || latest.LinuxCloneIntent == nil ||
		!latest.LinuxCloneIntent.Equal(grant.Intent) || latest.LinuxCloneSeedReceipt == nil ||
		!latest.LinuxCloneSeedReceipt.Equal(grant.Seed) || latest.LinuxCloneCheckoutReceipt == nil ||
		!latest.LinuxCloneCheckoutReceipt.Equal(checkout) || latest.LinuxClonePromotionIntent == nil ||
		latest.LinuxClonePromotionReceipt == nil {
		return errors.New("Linux clone cleanup authority changed from the exact terminal RunJournal")
	}
	cleanupIntent, err := state.LinuxCloneCleanupIntentForJournal(latest)
	if err != nil {
		return fmt.Errorf("derive exact Linux clone cleanup intent from RunJournal: %w", err)
	}
	if latest.LinuxCloneCleanupIntent != nil && !latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) {
		return errors.New("Linux clone cleanup intent differs from the exact RunJournal")
	}
	if err := daemon.requireUnownedLinuxCloneCleanupTarget(key, cleanupIntent, latest); err != nil {
		return err
	}

	cleanupProof := authority.LinuxCloneCleanupProof{}
	if latest.LinuxCloneCleanupProof != nil {
		cleanupProof = latest.LinuxCloneCleanupProof.Clone()
		if !latest.LinuxCloneCleanupEligible(key, fence) || !cleanupProof.Intent.Equal(cleanupIntent) {
			return errors.New("completed Linux clone cleanup proof differs from its eligible RunJournal")
		}
	} else {
		if !latest.LinuxCloneCleanupBeginEligible(key, fence) {
			return errors.New("Linux clone cleanup is not authorized by the exact terminal RunJournal")
		}
		if err := linuxCloneCleanupPersistMutation(daemon, daemon.store, key, fence, "begin Linux clone cleanup", func() (state.RunJournal, error) {
			return daemon.store.BeginLinuxCloneCleanup(key, fence)
		}, func(current state.RunJournal) bool {
			return current.LinuxCloneCleanupPending && current.LinuxCloneCleanupIntent != nil &&
				current.LinuxCloneCleanupIntent.Equal(cleanupIntent) && current.LinuxCloneCleanupProof == nil &&
				current.WorkspacePath == grant.Intent.Target && current.WorkspaceRecoveryRequired
		}); err != nil {
			return fmt.Errorf("persist exact Linux clone cleanup intent before Workspace effects: %w", err)
		}
		latest, err = daemon.store.LoadJournal(key)
		if err != nil || !linuxCloneCleanupJournalMatchesFence(latest, key, fence) || !latest.LinuxCloneCleanupPending ||
			latest.LinuxCloneCleanupIntent == nil || !latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) ||
			latest.LinuxCloneCleanupProof != nil || latest.WorkspacePath != grant.Intent.Target || !latest.WorkspaceRecoveryRequired {
			return errors.Join(errors.New("read back exact pending Linux clone cleanup intent"), err)
		}
	}

	var priorRemovalProof *authority.LinuxCloneTargetRemovalProof
	if latest.LinuxCloneTargetRemovalProof != nil {
		copy := latest.LinuxCloneTargetRemovalProof.Clone()
		if copy.Validate() != nil || !copy.Intent.Equal(cleanupIntent) ||
			copy.Disposition != authority.LinuxCloneTargetDispositionRemoved {
			return errors.New("stored Linux clone target-removal proof differs from the exact cleanup intent")
		}
		priorRemovalProof = &copy
	}
	cleanupContext, cancel := daemon.cleanupContext(ctx)
	defer cancel()
	if latest.LinuxCloneCleanupProof == nil {
		// Begin is durable before the B target can be removed. Recheck
		// volatile/persisted ownership immediately before that effect because
		// State cannot know about an in-process owner.
		latest, err = daemon.store.LoadJournal(key)
		if err != nil || !linuxCloneCleanupJournalMatchesFence(latest, key, fence) ||
			!latest.LinuxCloneCleanupPending || latest.LinuxCloneCleanupIntent == nil ||
			!latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) || latest.LinuxCloneCleanupProof != nil ||
			latest.RetainWorkspace || latest.WorkspacePath != grant.Intent.Target || !latest.WorkspaceRecoveryRequired {
			return errors.Join(errors.New("recheck exact pending Linux clone cleanup before target removal"), err)
		}
		if latest.LinuxCloneTargetRemovalProof == nil {
			if priorRemovalProof != nil {
				return errors.New("stored target-removal proof disappeared before Workspace replay")
			}
		} else if priorRemovalProof == nil {
			copy := latest.LinuxCloneTargetRemovalProof.Clone()
			if copy.Validate() != nil || !copy.Intent.Equal(cleanupIntent) {
				return errors.New("fresh RunJournal target-removal proof differs from exact cleanup intent")
			}
			priorRemovalProof = &copy
		} else if !latest.LinuxCloneTargetRemovalProof.Equal(*priorRemovalProof) {
			return errors.New("RunJournal target-removal proof changed before Workspace replay")
		}
		if !latest.LinuxCloneCleanupBeginEligible(key, fence) {
			return errors.New("Linux clone cleanup gates changed after the durable Begin intent")
		}
		if err := daemon.requireUnownedLinuxCloneCleanupTarget(key, cleanupIntent, latest); err != nil {
			return err
		}
		var deleter workspace.LinuxCloneTargetDeleter
		if priorRemovalProof == nil {
			deleter, err = linuxCloneTargetDeleterForRun(daemon, grant, cleanupIntent, nil)
			if err != nil {
				return fmt.Errorf("bind exact target-removal request to its authenticated Linux owner anchor: %w", err)
			}
		}
		targetRemovalProof, removeErr := cloneWorkspace.RemoveLinuxCloneTarget(cleanupContext, grant, checkout, latest, deleter)
		if removeErr != nil {
			return fmt.Errorf("remove exact B-owned Linux clone before A cleanup: %w", removeErr)
		}
		if targetRemovalProof.Validate() != nil || !targetRemovalProof.Intent.Equal(cleanupIntent) ||
			targetRemovalProof.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
			priorRemovalProof != nil && !targetRemovalProof.Equal(*priorRemovalProof) {
			return errors.New("Workspace returned no exact stored/root-proven Linux clone target removal")
		}
		if err := daemon.persistLinuxCloneTargetRemoval(key, fence, cleanupIntent, targetRemovalProof); err != nil {
			return fmt.Errorf("persist exact root target-removal proof before A cleanup: %w", err)
		}
		latest, err = daemon.store.LoadJournal(key)
		if err != nil || !linuxCloneCleanupJournalMatchesFence(latest, key, fence) || !latest.LinuxCloneCleanupPending ||
			latest.LinuxCloneCleanupIntent == nil || !latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) ||
			latest.LinuxCloneTargetRemovalProof == nil || !latest.LinuxCloneTargetRemovalProof.Equal(targetRemovalProof) ||
			latest.LinuxCloneCleanupProof != nil {
			return errors.Join(errors.New("read back exact durable Linux clone target-removal proof"), err)
		}
		if err := daemon.requireUnownedLinuxCloneCleanupTarget(key, cleanupIntent, latest); err != nil {
			return err
		}
		cleanupProof, err = cloneWorkspace.CleanupLinuxClone(cleanupContext, grant, checkout, latest)
		if err != nil {
			return fmt.Errorf("retire A-owned Linux clone material after durable B removal proof: %w", err)
		}
		if cleanupProof.Validate() != nil || !cleanupProof.Intent.Equal(cleanupIntent) ||
			!cleanupProof.TargetRemovalProof.Equal(targetRemovalProof) {
			return errors.New("Workspace returned no exact Linux clone cleanup proof")
		}
		if err := linuxCloneCleanupPersistMutation(daemon, daemon.store, key, fence, "complete Linux clone cleanup", func() (state.RunJournal, error) {
			return daemon.store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, cleanupProof)
		}, func(current state.RunJournal) bool {
			return !current.LinuxCloneCleanupPending && current.LinuxCloneCleanupIntent != nil &&
				current.LinuxCloneCleanupIntent.Equal(cleanupIntent) && current.LinuxCloneCleanupProof != nil &&
				current.LinuxCloneCleanupProof.Equal(cleanupProof) && current.WorkspacePath == "" &&
				!current.WorkspaceRecoveryRequired
		}); err != nil {
			return fmt.Errorf("persist exact Linux clone cleanup proof: %w", err)
		}
		latest, err = daemon.store.LoadJournal(key)
		if err != nil || !linuxCloneCleanupJournalMatchesFence(latest, key, fence) || latest.LinuxCloneCleanupPending ||
			latest.LinuxCloneCleanupIntent == nil || !latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) ||
			latest.LinuxCloneCleanupProof == nil || !latest.LinuxCloneCleanupProof.Equal(cleanupProof) ||
			latest.LinuxCloneTargetRemovalProof == nil || !latest.LinuxCloneTargetRemovalProof.Equal(targetRemovalProof) ||
			latest.WorkspacePath != "" || latest.WorkspaceRecoveryRequired {
			return errors.Join(errors.New("read back exact completed Linux clone cleanup proof"), err)
		}
	} else {
		// A completed proof can be the result of a prior lost ACK. Freshly
		// replay its exact State CAS before relying on it for journal deletion.
		if err := linuxCloneCleanupPersistMutation(daemon, daemon.store, key, fence, "replay completed Linux clone cleanup", func() (state.RunJournal, error) {
			return daemon.store.CompleteLinuxCloneCleanup(key, fence, cleanupIntent, cleanupProof)
		}, func(current state.RunJournal) bool {
			return !current.LinuxCloneCleanupPending && current.LinuxCloneCleanupIntent != nil &&
				current.LinuxCloneCleanupIntent.Equal(cleanupIntent) && current.LinuxCloneCleanupProof != nil &&
				current.LinuxCloneCleanupProof.Equal(cleanupProof) && current.WorkspacePath == "" &&
				!current.WorkspaceRecoveryRequired
		}); err != nil {
			return fmt.Errorf("replay exact completed Linux clone cleanup proof: %w", err)
		}
		latest, err = daemon.store.LoadJournal(key)
		if err != nil || !linuxCloneCleanupJournalMatchesFence(latest, key, fence) || !latest.LinuxCloneCleanupEligible(key, fence) ||
			latest.LinuxCloneCleanupIntent == nil || !latest.LinuxCloneCleanupIntent.Equal(cleanupIntent) ||
			latest.LinuxCloneCleanupProof == nil || !latest.LinuxCloneCleanupProof.Equal(cleanupProof) {
			return errors.Join(errors.New("read back replayed completed Linux clone cleanup proof"), err)
		}
	}

	verifiedProof, err := cloneWorkspace.CleanupLinuxClone(cleanupContext, grant, checkout, latest)
	if err != nil || !verifiedProof.Equal(cleanupProof) {
		return errors.Join(errors.New("reverify completed Linux clone cleanup before journal deletion"), err)
	}
	return nil
}

func (daemon *daemon) persistLinuxCloneTargetRemoval(
	key state.RunKey,
	fence protocol.Fence,
	expected authority.LinuxCloneCleanupIntent,
	proof authority.LinuxCloneTargetRemovalProof,
) error {
	if daemon == nil || daemon.store == nil || expected.Validate() != nil || proof.Validate() != nil ||
		proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.Intent.Equal(expected) {
		return errors.New("Linux clone target-removal proof is incomplete or differs from its cleanup intent")
	}
	if err := linuxCloneCleanupPersistMutation(daemon, daemon.store, key, fence, "record Linux clone target removal", func() (state.RunJournal, error) {
		return daemon.store.RecordLinuxCloneTargetRemoval(key, fence, expected, proof)
	}, func(current state.RunJournal) bool {
		return current.LinuxCloneCleanupPending && current.LinuxCloneCleanupIntent != nil &&
			current.LinuxCloneCleanupIntent.Equal(expected) && current.LinuxCloneTargetRemovalProof != nil &&
			current.LinuxCloneTargetRemovalProof.Equal(proof) && current.LinuxCloneCleanupProof == nil &&
			current.WorkspacePath == expected.Intent.Target && current.WorkspaceRecoveryRequired
	}); err != nil {
		return fmt.Errorf("persist exact B target-removal receipt: %w", err)
	}
	readback, err := daemon.store.LoadJournal(key)
	if err != nil || !linuxCloneCleanupJournalMatchesFence(readback, key, fence) ||
		!readback.LinuxCloneCleanupPending || readback.LinuxCloneCleanupIntent == nil ||
		!readback.LinuxCloneCleanupIntent.Equal(expected) || readback.LinuxCloneTargetRemovalProof == nil ||
		!readback.LinuxCloneTargetRemovalProof.Equal(proof) || readback.LinuxCloneCleanupProof != nil ||
		readback.WorkspacePath != expected.Intent.Target || !readback.WorkspaceRecoveryRequired {
		return errors.Join(errors.New("read back exact durable B target-removal receipt"), err)
	}
	return nil
}

func linuxCloneCleanupJournalMatchesFence(journal state.RunJournal, key state.RunKey, fence protocol.Fence) bool {
	return journal.Key() == key && journal.Fence() == fence && fence.Generation == key.Generation
}

func (daemon *daemon) requireUnownedLinuxCloneCleanupTarget(
	key state.RunKey,
	cleanup authority.LinuxCloneCleanupIntent,
	journal state.RunJournal,
) error {
	if journal.RetainWorkspace || daemon.workspaceRetentionRemembered(key) {
		return errors.New("Linux clone cleanup remains blocked by durable or volatile workspace retention")
	}
	retained, err := daemon.retainedGoalSessionWorkspace(key)
	if err != nil {
		return err
	}
	if retained {
		return errors.New("Linux clone cleanup remains blocked by a GoalSession workspace owner")
	}
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return fmt.Errorf("list RunJournals before Linux clone target removal: %w", err)
	}
	for _, other := range journals {
		if other.Key() == key {
			continue
		}
		if other.WorkspaceBindingKey == cleanup.Intent.BindingKey && other.WorkspacePath == cleanup.Intent.Target ||
			other.LinuxCloneIntent != nil && other.LinuxCloneIntent.BindingKey == cleanup.Intent.BindingKey &&
				other.LinuxCloneIntent.Target == cleanup.Intent.Target {
			return fmt.Errorf("Linux clone cleanup target is still named by another RunJournal %s/%d", other.RunID, other.Generation)
		}
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for ownerKey, active := range daemon.running {
		if active == nil {
			continue
		}
		if ownerKey == key || active.prepared.BindingKey == cleanup.Intent.BindingKey && active.prepared.Path == cleanup.Intent.Target ||
			active.linuxClone != nil && active.linuxClone.grant.Intent.BindingKey == cleanup.Intent.BindingKey &&
				active.linuxClone.grant.Intent.Target == cleanup.Intent.Target {
			return errors.New("Linux clone cleanup target is still owned by a volatile run")
		}
	}
	return nil
}

func linuxCloneAbortRecoveryEligible(journal state.RunJournal) bool {
	if journal.LinuxCloneIntent == nil || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven || journal.ContainmentCgroupReleaseProof != nil {
		return false
	}
	if journal.ContainmentCgroupPreGateReleaseProof != nil {
		proof := journal.ContainmentCgroupPreGateReleaseProof
		return journal.ContainmentCgroup == nil && proof.Validate() == nil && proof.Receipt.Validate() == nil
	}
	return journal.ContainmentCgroup == nil || journal.ContainmentCgroup.AbortProof != nil
}

func linuxCloneAbortProofMatchesJournal(journal state.RunJournal) bool {
	proof := journal.LinuxCloneAbortProof
	intent := journal.LinuxCloneIntent
	if proof == nil || intent == nil || proof.Validate() != nil || !proof.Intent.Equal(*intent) ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		proof.NormalReleaseProof != nil || journal.ContainmentCgroupReleaseProof != nil ||
		journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.HasProcessDetails() || journal.ContainmentScans != nil || journal.ContainmentUnproven {
		return false
	}
	seedMatches := proof.SeedReceipt == nil && journal.LinuxCloneSeedReceipt == nil ||
		proof.SeedReceipt != nil && journal.LinuxCloneSeedReceipt != nil && proof.SeedReceipt.Equal(*journal.LinuxCloneSeedReceipt)
	preGateMatches := proof.PreGateReleaseProof == nil && journal.ContainmentCgroupPreGateReleaseProof == nil ||
		proof.PreGateReleaseProof != nil && journal.ContainmentCgroupPreGateReleaseProof != nil &&
			proof.PreGateReleaseProof.Equal(*journal.ContainmentCgroupPreGateReleaseProof)
	var cgroupAbort *authority.LinuxCgroupReservationAbortProof
	if journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof != nil {
		copy := journal.ContainmentCgroup.AbortProof.Clone()
		cgroupAbort = &copy
	}
	cgroupMatches := proof.CgroupAbortProof == nil && cgroupAbort == nil ||
		proof.CgroupAbortProof != nil && cgroupAbort != nil && proof.CgroupAbortProof.Equal(*cgroupAbort)
	if journal.ContainmentCgroup != nil && cgroupAbort == nil {
		return false
	}
	return seedMatches && preGateMatches && cgroupMatches &&
		!(proof.PreGateReleaseProof != nil && proof.CgroupAbortProof != nil)
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
	if daemon.deferStaleCleanupForPendingAcknowledgement(journal) {
		return errors.New("command acknowledgement remains pending")
	}
	if journal.LocalState == "stale" && journal.TerminalState == "" && daemon.now().Before(journal.LeaseExpiresAt) {
		// Control can still send a cancel until the lost lease expires; after
		// that its reaper settles the Run. Keep the stale journal so that cancel
		// can be acknowledged against the recorded stop.
		return errors.New("stale run lease has not expired")
	}
	if len(journal.PendingCommandAcknowledgements) != 0 {
		if !state.CommandAcknowledgementRetired(journal) {
			daemon.signalOutboxFor(journal.Key())
			return errors.New("command acknowledgement remains pending")
		}
		for len(journal.PendingCommandAcknowledgements) > 0 {
			updated, err := daemon.markCommandAcknowledgementDelivered(journal.Key(), journal.PendingCommandAcknowledgements[0])
			if err != nil {
				return err
			}
			journal = updated
		}
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
	if journal.HasPendingContainment() {
		return errors.New("containment state remains pending")
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
