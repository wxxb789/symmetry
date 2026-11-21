package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

var (
	errAssignmentExpired            = errors.New("assignment expired")
	errLeaseDeadlineReached         = errors.New("lease renewal deadline reached")
	errOutboxChanged                = errors.New("outbox changed during delivery")
	errInputWriteTimeout            = errors.New("agent did not consume standard input within the write timeout")
	errStartProcessNil              = errors.New("start process returned nil")
	errInvalidAdmission             = errors.New("invalid symmetry.admission.v1")
	errGoalAttachmentPending        = errors.New("retained Goal session has no verified current attachment")
	errPersistedProcessStopUnproven = errors.New("persisted process stop is unproven")
	errAuthoritativeTerminal        = errors.New("authoritative terminal already durable")
	errDaemonStoreClosing           = errors.New("daemon state store is closing")
	errContainmentStopObserver      = errors.New("containment stop observer failed")
	workspaceFingerprint            = workspace.Fingerprint
	linuxCloneAbortRecovery         = func(context.Context, *daemon, state.RunJournal) error {
		return errors.New("Linux clone pre-checkout abort recovery is unavailable")
	}
	linuxCloneTargetDeleterForRun = func(
		*daemon,
		authority.LinuxCloneGrant,
		authority.LinuxCloneCleanupIntent,
		*authority.LinuxCloneTargetRemovalProof,
	) (workspace.LinuxCloneTargetDeleter, error) {
		return nil, errors.New("Linux clone target removal requires the authenticated owner anchor")
	}
	linuxCloneCleanupPersistMutation = func(
		*daemon, *state.Store, state.RunKey, protocol.Fence, string,
		func() (state.RunJournal, error), func(state.RunJournal) bool,
	) error {
		return errors.New("Linux clone cleanup persistence is supported only by the Linux v2 App")
	}
	normalizeLinuxNativeStateDir          = func(path string) (string, error) { return path, nil }
	terminatePersistAuthorityWithScanSeal func(int, string, *authority.Supervisor) (authority.StopReceipt, uint64, error)
)

func (daemon *daemon) runLiveness(ctx context.Context) {
	for {
		daemon.renewLeases(ctx)
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) linuxV2ClaimLeaseRemaining(key state.RunKey, claim protocol.ClaimResponse) time.Duration {
	if deadline, ok := daemon.localLeaseDeadline(key); ok {
		return deadline.Sub(daemon.localNow())
	}
	if remaining := daemon.remainingLeaseDeadline(claim, claim.RequestStartedAt); remaining > 0 {
		return remaining
	}
	if !claim.LeaseExpiresAt.IsZero() {
		return claim.LeaseExpiresAt.Sub(daemon.now())
	}
	return 0
}

func (daemon *daemon) claimLocalLeaseDeadline(claim protocol.ClaimResponse, requestStarted time.Time) time.Time {
	if deadline := leaseDeadlineAt(claim, requestStarted); !deadline.IsZero() {
		return deadline
	}
	if claim.LeaseExpiresAt.IsZero() {
		return time.Time{}
	}
	remaining := claim.LeaseExpiresAt.Sub(daemon.now())
	if remaining <= 0 {
		return time.Time{}
	}
	return daemon.localNow().Add(remaining)
}

func (daemon *daemon) nativeStartLeaseExpired(key state.RunKey) (bool, string) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil || !active.nativeStartLeaseExpired {
		return false, ""
	}
	return true, active.nativeStartLeaseReason
}

func (daemon *daemon) renewLeases(ctx context.Context) {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	var now time.Time
	type renewal struct {
		journal         state.RunJournal
		response        protocol.LeaseHeartbeatResponse
		requestID       uint64
		requestStarted  time.Time
		elapsed         time.Duration
		relative        bool
		localDeadlineAt time.Time
		err             error
	}
	candidates := make([]state.RunJournal, 0, len(journals))
	relativeDeadlines := make(map[state.RunKey]time.Time, len(journals))
	relativeLease := make(map[state.RunKey]bool, len(journals))
	for _, journal := range journals {
		if journal.LocalState == "terminal_pending" || journal.LeaseToken == "" {
			continue
		}
		if !hasFullFence(journal) {
			daemon.terminateForLease(journal, "lease fence is incomplete")
			continue
		}
		localDeadlineAt, hasRelativeLease := daemon.localLeaseDeadline(journal.Key())
		var remaining time.Duration
		if hasRelativeLease {
			remaining = localDeadlineAt.Sub(daemon.localNow())
			if remaining <= 0 {
				daemon.terminateForLease(journal, "relative lease deadline elapsed")
				continue
			}
		} else {
			if now.IsZero() {
				now = daemon.now()
			}
			remaining = journal.LeaseExpiresAt.Sub(now)
			if remaining <= 0 {
				daemon.terminateForLease(journal, "lease expired")
				continue
			}
			if remaining <= leaseSafetyMargin {
				continue
			}
		}
		if !daemon.renewalEligible(journal) {
			continue
		}
		if remaining > daemon.renewalThreshold() {
			continue
		}
		relativeDeadlines[journal.Key()] = localDeadlineAt
		relativeLease[journal.Key()] = hasRelativeLease
		candidates = append(candidates, journal)
	}
	if len(candidates) == 0 {
		return
	}
	jobs := make(chan int)
	completed := make(chan renewal, len(candidates))
	workers := cap(daemon.slots)
	if workers < 1 {
		workers = 1
	}
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				journal := candidates[index]
				response, requestID, requestStarted, elapsed, renewErr := daemon.renewLeaseWithTimingAndStart(ctx, journal)
				completed <- renewal{
					journal:         journal,
					response:        response,
					requestID:       requestID,
					requestStarted:  requestStarted,
					elapsed:         elapsed,
					relative:        relativeLease[journal.Key()],
					localDeadlineAt: relativeDeadlines[journal.Key()],
					err:             renewErr,
				}
			}
		}()
	}
	go func() {
		for index := range candidates {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		close(completed)
	}()
	for result := range completed {
		if ctx.Err() != nil {
			daemon.finishCommandRequest(result.requestID)
			continue
		}
		if !daemon.renewalStillEligible(result.journal) {
			daemon.finishCommandRequest(result.requestID)
			continue
		}
		if result.relative || result.response.LeaseRemainingMS > 0 {
			// A first relative response may upgrade a legacy in-memory run.
			// Do not let the legacy wall-clock projection reject that response
			// before its relative authority is evaluated below.
			if result.relative && (result.localDeadlineAt.IsZero() || !daemon.localNow().Before(result.localDeadlineAt)) {
				daemon.finishCommandRequest(result.requestID)
				daemon.terminateForLease(result.journal, "relative lease deadline elapsed during renewal")
				continue
			}
		} else {
			current := daemon.now()
			if !current.Before(result.journal.LeaseExpiresAt) {
				daemon.finishCommandRequest(result.requestID)
				daemon.terminateForLease(result.journal, "lease expired during renewal")
				continue
			}
		}
		if result.err != nil {
			daemon.finishCommandRequest(result.requestID)
			if errors.Is(result.err, context.Canceled) {
				continue
			}
			if result.relative {
				if control.IsOwnershipLost(result.err) || result.localDeadlineAt.IsZero() || !daemon.localNow().Before(result.localDeadlineAt) {
					daemon.terminateForLease(result.journal, "lease renewal failed")
				}
			} else {
				current := daemon.now()
				if control.IsOwnershipLost(result.err) || result.journal.LeaseExpiresAt.Sub(current) <= leaseSafetyMargin {
					daemon.terminateForLease(result.journal, "lease renewal failed")
				}
			}
			continue
		}
		if result.relative || result.response.LeaseRemainingMS > 0 {
			if result.response.LeaseRemainingMS <= 0 {
				// A legacy response cannot extend a lease whose authority was
				// previously relative. Keep the existing local deadline and fail
				// closed instead of falling back to its absolute expiry.
				daemon.finishCommandRequest(result.requestID)
				continue
			}
			deadlineAt := leaseDeadlineAtFromRemaining(result.response.LeaseRemainingMS, result.requestStarted)
			localDeadline := time.Duration(result.response.LeaseRemainingMS)*time.Millisecond - result.elapsed - leaseSafetyMargin
			if localDeadline <= 0 {
				daemon.finishCommandRequest(result.requestID)
				daemon.terminateForLease(result.journal, "renewed lease has no safe local deadline")
				continue
			}
			supported, renewErr := daemon.renewLocalLeaseDuration(result.journal.Key(), localDeadline, deadlineAt)
			if renewErr != nil {
				daemon.finishCommandRequest(result.requestID)
				daemon.terminateForLease(result.journal, "local lease watchdog renewal failed")
				continue
			}
			if !supported {
				// Keep the daemon-side deadline even when this platform has no
				// independent watchdog capability; it still constrains future
				// renewal decisions.
				daemon.setLocalLeaseDeadline(result.journal.Key(), deadlineAt)
			}
		} else {
			// Do not persist the absolute field from a relative response: a
			// replayed wall-clock expiry must not outlive this local authority.
			current := daemon.now()
			if !result.response.LeaseExpiresAt.After(current.Add(leaseSafetyMargin)) {
				daemon.finishCommandRequest(result.requestID)
				daemon.terminateForLease(result.journal, "renewed lease is already unsafe")
				continue
			}
			if _, persistErr := daemon.store.AdvanceLeaseExpiry(result.journal.Key(), result.response.LeaseExpiresAt); persistErr != nil {
				daemon.finishCommandRequest(result.requestID)
				if daemon.log != nil {
					daemon.log.Warn("advance_lease_expiry_failed", "run_id", result.journal.RunID, "error", persistErr)
				}
				continue
			}
		}
		_, _ = daemon.enqueueSnapshotCommandsForRequest(result.requestID, result.response.Commands)
	}
}

func (daemon *daemon) renewLocalLease(key state.RunKey, deadline time.Duration) (bool, error) {
	if deadline <= 0 {
		return false, errors.New("local lease deadline must be positive")
	}
	return daemon.renewLocalLeaseDuration(key, deadline, time.Time{})
}

func (daemon *daemon) renewLocalLeaseDuration(key state.RunKey, deadline time.Duration, deadlineAt time.Time) (bool, error) {
	daemon.mu.Lock()
	active := daemon.running[key]
	if !activeRunCanRenew(active) {
		daemon.mu.Unlock()
		return false, nil
	}
	if !deadlineAt.IsZero() {
		deadline = deadlineAt.Sub(daemon.localNow())
		if deadline <= 0 {
			daemon.mu.Unlock()
			return false, errLeaseDeadlineReached
		}
	}
	var renewer execution.LeaseRenewer
	if active.process != nil {
		renewer, _ = active.process.(execution.LeaseRenewer)
	}
	if renewer == nil && active.nativeSession != nil {
		renewer, _ = active.nativeSession.(execution.LeaseRenewer)
	}
	if renewer == nil || !renewer.LeaseRenewalAvailable() {
		daemon.mu.Unlock()
		return false, nil
	}
	active.leaseSequence++
	if active.leaseSequence == 0 {
		active.leaseSequence = 1
	}
	sequence := active.leaseSequence
	daemon.mu.Unlock()
	if err := renewer.RenewLease(deadline, sequence); err != nil {
		return true, err
	}
	if !deadlineAt.IsZero() {
		daemon.setLocalLeaseDeadline(key, deadlineAt)
	}
	return true, nil
}

func (daemon *daemon) localLeaseDeadline(key state.RunKey) (time.Time, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil || active.localLeaseDeadlineAt.IsZero() {
		return time.Time{}, false
	}
	return active.localLeaseDeadlineAt, true
}

func (daemon *daemon) setLocalLeaseDeadline(key state.RunKey, deadlineAt time.Time) {
	if deadlineAt.IsZero() {
		return
	}
	daemon.mu.Lock()
	if active := daemon.running[key]; active != nil {
		active.localLeaseDeadlineAt = deadlineAt
	}
	daemon.mu.Unlock()
}

func (daemon *daemon) renewalThreshold() time.Duration {
	if daemon.leaseDuration <= 0 {
		return leaseSafetyMargin
	}
	threshold := daemon.leaseDuration * 3 / 4
	if threshold > 90*time.Second {
		return 90 * time.Second
	}
	return threshold
}

// remainingLeaseDeadline converts Control's server-derived relative lease
// budget into a conservative local duration. The budget is measured from the
// claim/renewal request start, so workspace preparation, response latency and
// journaling can only consume authority; they never mint extra time.
func (daemon *daemon) remainingLeaseDeadline(claim protocol.ClaimResponse, requestStarted time.Time) time.Duration {
	return remainingLeaseDeadlineAt(leaseDeadlineAt(claim, requestStarted), daemon.localNow())
}

// validateClaimLeaseForStart keeps replayed claims from crossing a local
// process or native-session boundary after their server lease is already
// unsafe. Relative responses remain authoritative when present; legacy
// responses must prove enough absolute wall-clock time for the safety margin.
func (daemon *daemon) validateClaimLeaseForStart(claim protocol.ClaimResponse, requestStarted time.Time, subject string) error {
	if claim.LeaseRemainingMS > 0 {
		if remainingLeaseDeadlineAt(leaseDeadlineAt(claim, requestStarted), daemon.localNow()) <= 0 {
			return fmt.Errorf("server lease remaining time was consumed before %s startup", subject)
		}
		return nil
	}
	// Control validation rejects a missing absolute expiry before a real claim
	// reaches this boundary. Keep lower-level callers compatible when they
	// intentionally omit the legacy field, but never accept an unsafe value
	// when it is present.
	if claim.LeaseExpiresAt.IsZero() {
		return nil
	}
	if !claim.LeaseExpiresAt.After(daemon.now().Add(leaseSafetyMargin)) {
		return fmt.Errorf("server lease expiry was consumed before %s startup", subject)
	}
	return nil
}

func leaseDeadlineAt(claim protocol.ClaimResponse, requestStarted time.Time) time.Time {
	return leaseDeadlineAtFromRemaining(claim.LeaseRemainingMS, requestStarted)
}

func leaseDeadlineAtFromRemaining(remainingMS int64, requestStarted time.Time) time.Time {
	if remainingMS <= 0 || requestStarted.IsZero() {
		return time.Time{}
	}
	return requestStarted.Add(time.Duration(remainingMS)*time.Millisecond - leaseSafetyMargin)
}

func remainingLeaseDeadlineAt(deadlineAt, now time.Time) time.Duration {
	if deadlineAt.IsZero() || now.IsZero() {
		return 0
	}
	return deadlineAt.Sub(now)
}

func (daemon *daemon) renewLease(ctx context.Context, journal state.RunJournal) (protocol.LeaseHeartbeatResponse, uint64, error) {
	response, requestID, _, err := daemon.renewLeaseWithTiming(ctx, journal)
	return response, requestID, err
}

func (daemon *daemon) renewLeaseWithTiming(ctx context.Context, journal state.RunJournal) (protocol.LeaseHeartbeatResponse, uint64, time.Duration, error) {
	response, requestID, _, elapsed, err := daemon.renewLeaseWithTimingAndStart(ctx, journal)
	return response, requestID, elapsed, err
}

func (daemon *daemon) renewLeaseWithTimingAndStart(ctx context.Context, journal state.RunJournal) (protocol.LeaseHeartbeatResponse, uint64, time.Time, time.Duration, error) {
	requestStarted := daemon.localNow()
	var renewalID uint64
	daemon.mu.Lock()
	active := daemon.running[journal.Key()]
	if !activeRunCanRenew(active) {
		daemon.mu.Unlock()
		return protocol.LeaseHeartbeatResponse{}, 0, requestStarted, 0, context.Canceled
	}
	requestLimit := time.Duration(0)
	if !active.localLeaseDeadlineAt.IsZero() {
		requestLimit = active.localLeaseDeadlineAt.Sub(requestStarted)
	} else {
		requestLimit = journal.LeaseExpiresAt.Sub(daemon.now()) - leaseSafetyMargin
	}
	if requestLimit <= 0 {
		daemon.mu.Unlock()
		return protocol.LeaseHeartbeatResponse{}, 0, requestStarted, 0, errLeaseDeadlineReached
	}
	if requestLimit > controlRequestLimit {
		requestLimit = controlRequestLimit
	}
	renewalContext, cancel := context.WithTimeout(ctx, requestLimit)
	active.renewCancelID++
	renewalID = active.renewCancelID
	active.renewCancel = cancel
	daemon.mu.Unlock()
	requestID := daemon.beginCommandRequest()
	response, err := daemon.control.RenewLease(renewalContext, journal.RunID, protocol.LeaseHeartbeatRequest{Fence: journal.Fence()})
	cancel()
	if err != nil {
		daemon.finishCommandRequest(requestID)
		requestID = 0
	}
	daemon.mu.Lock()
	if active := daemon.running[journal.Key()]; active != nil && active.renewCancelID == renewalID {
		active.renewCancel = nil
	}
	daemon.mu.Unlock()
	elapsed := daemon.localNow().Sub(requestStarted)
	if elapsed < 0 {
		elapsed = 0
	}
	return response, requestID, requestStarted, elapsed, err
}

func (daemon *daemon) renewalEligible(snapshot state.RunJournal) bool {
	if snapshot.LeaseToken == "" || snapshot.LocalState == "terminal_pending" || snapshot.LocalState == "cleanup_pending" || snapshot.LocalState == "stale" {
		return false
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return activeRunCanRenew(daemon.running[snapshot.Key()])
}

func activeRunCanRenew(active *runningRun) bool {
	return active != nil && (active.process != nil || active.nativeSession != nil) &&
		!active.stale && !active.terminal && active.terminalizing == 0 && !active.nativeUsageRenewalBlocked && !active.leaseRenewalClosed
}

func (daemon *daemon) renewalStillEligible(snapshot state.RunJournal) bool {
	journal, err := daemon.store.LoadJournal(snapshot.Key())
	if err != nil || journal.Fence() != snapshot.Fence() {
		return false
	}
	return daemon.renewalEligible(journal)
}

func (daemon *daemon) terminateForLease(journal state.RunJournal, reason string) {
	if journal.LocalState == "terminal_pending" {
		return
	}
	key := journal.Key()
	daemon.eventReceiptMu.Lock()
	daemon.mu.Lock()
	active := daemon.running[key]
	if active != nil && (active.nativeTerminalOwner || active.terminal || active.terminalizing > 0) {
		// A terminal owner already owns the transition-to-durable window.
		daemon.mu.Unlock()
		daemon.eventReceiptMu.Unlock()
		return
	}
	if active != nil && active.nativeStartInFlight {
		if !active.nativeStartLeaseExpired {
			active.nativeStartLeaseExpired = true
			active.nativeStartLeaseReason = reason
		}
		active.stale = true
		active.leaseRenewalClosed = true
		startCancel := active.cancel
		active.cancel = nil
		renewCancel := active.renewCancel
		active.renewCancel = nil
		active.renewCancelID++
		daemon.mu.Unlock()
		daemon.eventReceiptMu.Unlock()
		if startCancel != nil {
			startCancel()
		}
		if renewCancel != nil {
			renewCancel()
		}
		daemon.rememberWorkspaceRetention(key)
		daemon.retainUnknownGoalLaunchWorkspace(key)
		if _, err := daemon.persistStaleAndEnqueueCleanup(key); err != nil && daemon.log != nil {
			daemon.log.Warn("mark_native_start_lease_expired_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		return
	}
	process := Process(nil)
	nativeSettlementOwned := false
	var renewCancel context.CancelFunc
	if active != nil {
		active.cancelled = true
		active.stale = true
		process = active.process
		if active.nativeSession != nil {
			claimNativeTerminalOwner(active, true)
			active.nativeUsageRenewalBlocked = true
			active.leaseRenewalClosed = true
			renewCancel = active.renewCancel
			active.renewCancel = nil
			active.renewCancelID++
			nativeSettlementOwned = true
		}
		if active.cancel != nil {
			active.cancel()
		}
	}
	daemon.mu.Unlock()
	daemon.eventReceiptMu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	if supervisoryRecoveryRequired(journal) {
		daemon.rememberWorkspaceRetention(key)
		defer daemon.persistWorkspaceRetention(key)
	}
	if nativeSettlementOwned {
		if _, err := daemon.persistStaleAndEnqueueCleanup(key); err != nil && daemon.log != nil {
			daemon.log.Warn("mark_lease_expired_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		return
	}
	if process != nil {
		_ = process.Terminate(context.Background(), 0)
	} else {
		if active == nil {
			daemon.stopRecoveredJournal(journal, reason)
		}
	}
	if _, err := daemon.persistStaleAndEnqueueCleanup(key); err != nil && daemon.log != nil {
		daemon.log.Warn("mark_lease_expired_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
	}
}
