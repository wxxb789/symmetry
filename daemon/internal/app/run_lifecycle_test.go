package app

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

type lifecycleSession struct{ id string }

func (lifecycleSession) Control(context.Context, harness.ControlRequest) (harness.ControlReceipt, error) {
	return harness.ControlReceipt{}, nil
}
func (lifecycleSession) Wait(context.Context) (harness.TaskResult, error) {
	return harness.TaskResult{}, nil
}
func (lifecycleSession) Close(context.Context) error { return nil }

type lifecycleProcess struct{ id int }

func (lifecycleProcess) WriteInput([]byte) error                        { return nil }
func (lifecycleProcess) Terminate(context.Context, time.Duration) error { return nil }
func (lifecycleProcess) Wait() execution.Result                         { return execution.Result{} }
func (lifecycleProcess) ProcessDetails() (int, string)                  { return 0, "" }

var lifecycleFieldNames = []string{
	"starting", "claimed",
	"nativeStartInFlight", "nativeStartTerminalObserved",
	"cancelled", "cancelCommandID", "nativeCancelCommandID", "nativeCancelTerminalObserved", "nativeCancelRejected", "nativeCancelTerminateErr", "cancel",
	"stale", "nativeStartLeaseExpired", "nativeStartLeaseReason", "leaseRenewalClosed", "renewCancel", "renewCancelID",
	"terminalizing", "terminal", "terminalCancel",
	"nativeTerminalOwner", "nativeTerminalOwnerClaim", "nativeTerminalOwnerSequence", "nativeStartTerminalOwner",
	"nativeUsageRetryPending", "nativeUsageRetryInFlight", "nativeUsageRetryExhausted", "nativeUsageRetryNeedsPrepare", "nativeUsageRetryOwnerClaim", "nativeUsageRetryAt", "nativeUsageRetryDeadline", "nativeUsageRetryAttempts", "nativeUsageRenewalBlocked", "nativeUsageFinalized",
	"nativeCloseRetryRequired", "nativeCloseRetrying",
	"processExited", "processFinalizeOwner", "processFinalizeAttempts", "processFinalizeExhausted", "processStopWitness", "processStopPID", "processStopIdentity",
	"slotHeld",
	"cleanupBlocked",
}

var (
	lifecycleTestSession = lifecycleSession{id: "lifecycle-session"}
	lifecycleTestProcess = lifecycleProcess{id: 7}
)

func TestRunLifecycleTransitionSequences(t *testing.T) {
	tests := []struct {
		name    string
		initial func() *runningRun
		steps   []func(*runningRun)
		check   func(*testing.T, *runningRun)
	}{
		{
			name: "admission/attach_claim_and_finish_attached_start",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{starting: true}, cleanupBlocked: true}
			},
			steps: []func(*runningRun){
				(*runningRun).attachClaim,
				func(run *runningRun) {
					run.nativeSession = lifecycleTestSession
					if run.finishAttachedStart() {
						t.Fatalf("finishAttachedStart reported a terminal run for a live attach")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.claimed {
					t.Fatalf("attachClaim did not record the claim")
				}
				if run.starting {
					t.Fatalf("finishAttachedStart left the starting window open")
				}
				if !run.cleanupBlocked {
					t.Fatalf("finishAttachedStart recomputed the memo while a native session is attached")
				}
			},
		},
		{
			name: "admission/finish_starting_keeps_session_obligation",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{starting: true}, cleanupBlocked: true}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if run.finishStarting(true) {
						t.Fatalf("finishStarting reported a terminal run for a fresh start")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.starting {
					t.Fatalf("finishStarting left the starting window open")
				}
				if run.cleanupBlocked {
					t.Fatalf("finishStarting blocked cleanup without an obligation")
				}
			},
		},
		{
			name: "admission/finish_starting_reports_terminal",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{starting: true, terminal: true}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if !run.finishStarting(true) {
						t.Fatalf("finishStarting did not report the terminal run")
					}
					if run.starting {
						t.Fatalf("finishStarting left the starting window open")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.cleanupBlocked {
					t.Fatalf("finishStarting blocked cleanup for a terminal run with no obligation")
				}
			},
		},
		{
			name: "admission/finish_starting_skips_refresh_when_not_cleanup_ready",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{starting: true, nativeTerminalOwner: true}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if run.finishStarting(false) {
						t.Fatalf("finishStarting reported a terminal run that is not terminal")
					}
					if run.cleanupBlocked {
						t.Fatalf("finishStarting refreshed the memo despite cleanupReady=false")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.runLifecycle.cleanupBlocked() {
					t.Fatalf("the open settlement claim stopped blocking cleanup")
				}
			},
		},
		{
			name: "admission/finish_starting_skips_refresh_for_attached_process",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{starting: true, nativeTerminalOwner: true}, process: lifecycleTestProcess}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if run.finishStarting(true) {
						t.Fatalf("finishStarting reported a terminal run that is not terminal")
					}
					if run.cleanupBlocked {
						t.Fatalf("finishStarting refreshed the memo while a process is attached")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.runLifecycle.cleanupBlocked() {
					t.Fatalf("the open settlement claim stopped blocking cleanup")
				}
			},
		},
		{
			name:    "native_start_publication/publish_and_end",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				(*runningRun).beginNativeStartPublication,
				func(run *runningRun) {
					run.publishNativeStartResult(lifecycleTestSession, state.GoalSessionKey{}, protocol.Admission{}, workspace.Prepared{})
				},
				(*runningRun).endNativeStartPublication,
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeStartInFlight {
					t.Fatalf("endNativeStartPublication left the publication window open")
				}
				if run.nativeSession != harness.Session(lifecycleTestSession) {
					t.Fatalf("publishNativeStartResult did not publish the session")
				}
				if run.goalSession == nil || run.goalAdmission == nil {
					t.Fatalf("publishNativeStartResult dropped the session identity: goalSession=%v goalAdmission=%v", run.goalSession, run.goalAdmission)
				}
				if !run.cleanupBlocked {
					t.Fatalf("publishNativeStartResult did not block cleanup")
				}
			},
		},
		{
			name: "native_start_publication/transfer_terminal_to_settlement",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeStartInFlight: true, stale: true}, nativeSession: lifecycleTestSession, nativeDeadline: time.Now().UTC()}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.transferNativeStartTerminal(true)
					run.claimStartedSettlementOwner()
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeStartInFlight || run.stale || !run.nativeDeadline.IsZero() {
					t.Fatalf("transferNativeStartTerminal kept start or stale state: inFlight=%t stale=%t deadline=%v", run.nativeStartInFlight, run.stale, run.nativeDeadline)
				}
				if !run.nativeTerminalOwner || !run.nativeStartTerminalOwner {
					t.Fatalf("claimStartedSettlementOwner did not take start ownership: owner=%t startOwner=%t", run.nativeTerminalOwner, run.nativeStartTerminalOwner)
				}
				if run.terminalizing != 1 {
					t.Fatalf("terminalizing = %d after one settlement claim, want 1", run.terminalizing)
				}
				if !run.nativeUsageRenewalBlocked || !run.cleanupBlocked {
					t.Fatalf("claimStartedSettlementOwner left renewal or cleanup open: renewalBlocked=%t cleanupBlocked=%t", run.nativeUsageRenewalBlocked, run.cleanupBlocked)
				}
			},
		},
		{
			name:    "cancellation/request_record_clear",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.requestCancel("command-1")
					if !run.cancelled || run.cancelCommandID != "command-1" {
						t.Fatalf("requestCancel = (cancelled=%t, id=%q), want (true, command-1)", run.cancelled, run.cancelCommandID)
					}
				},
				func(run *runningRun) { run.recordNativeCancelObserved("command-1", true, false) },
				(*runningRun).clearCancel,
			},
			check: func(t *testing.T, run *runningRun) {
				if run.cancelled || run.cancelCommandID != "" {
					t.Fatalf("clearCancel kept the reservation: cancelled=%t id=%q", run.cancelled, run.cancelCommandID)
				}
				if run.nativeCancelCommandID != "command-1" || !run.nativeCancelTerminalObserved || run.nativeCancelRejected {
					t.Fatalf("recordNativeCancelObserved lost the outcome: id=%q observed=%t rejected=%t", run.nativeCancelCommandID, run.nativeCancelTerminalObserved, run.nativeCancelRejected)
				}
			},
		},
		{
			name: "cancellation/settle_cancelled_start_with_observed_terminal",
			initial: func() *runningRun {
				run := &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeStartInFlight: true}}
				run.beginRenewal(func() {})
				return run
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if renewCancel := run.settleCancelledStartWithObservedTerminal("command-1"); renewCancel == nil {
						t.Fatalf("settleCancelledStartWithObservedTerminal dropped the captured renewal handle")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.nativeTerminalOwner || !run.nativeStartTerminalOwner || run.nativeTerminalOwnerClaim != 1 || run.terminalizing != 1 {
					t.Fatalf("settleCancelledStartWithObservedTerminal did not claim settlement: owner=%t startOwner=%t claim=%d terminalizing=%d", run.nativeTerminalOwner, run.nativeStartTerminalOwner, run.nativeTerminalOwnerClaim, run.terminalizing)
				}
				if run.nativeStartInFlight {
					t.Fatalf("settleCancelledStartWithObservedTerminal left the publication window open")
				}
				if run.nativeCancelCommandID != "command-1" || !run.nativeCancelTerminalObserved || !run.nativeCancelRejected {
					t.Fatalf("settleCancelledStartWithObservedTerminal lost the cancel outcome: id=%q observed=%t rejected=%t", run.nativeCancelCommandID, run.nativeCancelTerminalObserved, run.nativeCancelRejected)
				}
				if !run.nativeUsageRenewalBlocked || run.renewCancel != nil || run.renewCancelID != 2 {
					t.Fatalf("settleCancelledStartWithObservedTerminal left renewal open: blocked=%t handle=%v id=%d", run.nativeUsageRenewalBlocked, run.renewCancel, run.renewCancelID)
				}
				if !run.cleanupBlocked {
					t.Fatalf("settleCancelledStartWithObservedTerminal did not block cleanup")
				}
			},
		},
		{
			name: "lease_loss/during_start",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeStartInFlight: true}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					startCancel, renewCancel := run.markLeaseLostDuringStart("lease expired during native turn start")
					if startCancel != nil || renewCancel != nil {
						t.Fatalf("markLeaseLostDuringStart returned handles that did not exist: start=%v renew=%v", startCancel, renewCancel)
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.stale || !run.leaseRenewalClosed || !run.nativeStartLeaseExpired {
					t.Fatalf("markLeaseLostDuringStart kept the run renewable: stale=%t closed=%t leaseExpired=%t", run.stale, run.leaseRenewalClosed, run.nativeStartLeaseExpired)
				}
				if run.nativeStartLeaseReason != "lease expired during native turn start" {
					t.Fatalf("nativeStartLeaseReason = %q", run.nativeStartLeaseReason)
				}
				if run.renewCancelID != 1 {
					t.Fatalf("renewCancelID = %d after invalidating the renewal, want 1", run.renewCancelID)
				}
			},
		},
		{
			name: "lease_loss/with_native_settlement",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true}, nativeSession: lifecycleTestSession}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					renewCancel, settled := run.markLeaseLostWithSettlement()
					if !settled || renewCancel != nil {
						t.Fatalf("markLeaseLostWithSettlement = (renew=%v, settled=%t), want (nil, true)", renewCancel, settled)
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.cancelled || !run.stale {
					t.Fatalf("markLeaseLostWithSettlement did not cancel the stale run: cancelled=%t stale=%t", run.cancelled, run.stale)
				}
				if !run.nativeTerminalOwner || !run.nativeUsageRenewalBlocked || !run.leaseRenewalClosed {
					t.Fatalf("markLeaseLostWithSettlement lost the settlement: owner=%t renewalBlocked=%t closed=%t", run.nativeTerminalOwner, run.nativeUsageRenewalBlocked, run.leaseRenewalClosed)
				}
			},
		},
		{
			name:    "terminal_reservation/reserve_release_publish",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				func(run *runningRun) {
					if renewCancel := run.reserveTerminal(); renewCancel != nil {
						t.Fatalf("reserveTerminal returned a renewal handle that did not exist")
					}
					run.releaseTerminalReservation()
					run.publishTerminal()
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.terminalizing != 0 {
					t.Fatalf("terminalizing = %d after releasing the reservation, want 0", run.terminalizing)
				}
				if !run.terminal || !run.leaseRenewalClosed {
					t.Fatalf("publishTerminal did not publish: terminal=%t renewalClosed=%t", run.terminal, run.leaseRenewalClosed)
				}
			},
		},
		{
			name:    "slot/release_returns_pending_cancel",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.reserveTerminalSlotRelease(func() {})
					if terminalCancel := run.releaseSlotOnce(); terminalCancel == nil {
						t.Fatalf("releaseSlotOnce did not return the pending terminal slot cancel func")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.slotHeld {
					t.Fatalf("releaseSlotOnce kept the slot held")
				}
				if run.terminalCancel != nil {
					t.Fatalf("releaseSlotOnce kept the terminal cancel handle")
				}
			},
		},
		{
			name: "slot/retire_run",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeTerminalOwner: true, nativeTerminalOwnerClaim: 3, terminalizing: 1, nativeUsageRetryOwnerClaim: 3}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					renewCancel, terminalCancel := run.retireRun()
					if renewCancel != nil || terminalCancel != nil {
						t.Fatalf("retireRun returned handles that did not exist: renew=%v terminal=%v", renewCancel, terminalCancel)
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.slotHeld || run.nativeTerminalOwner || run.nativeTerminalOwnerClaim != 0 || run.terminalizing != 0 {
					t.Fatalf("retireRun kept the run in the registry: slotHeld=%t owner=%t claim=%d terminalizing=%d", run.slotHeld, run.nativeTerminalOwner, run.nativeTerminalOwnerClaim, run.terminalizing)
				}
				if !run.leaseRenewalClosed || run.nativeUsageRetryOwnerClaim != 0 {
					t.Fatalf("retireRun kept renewal state: closed=%t retryOwnerClaim=%d", run.leaseRenewalClosed, run.nativeUsageRetryOwnerClaim)
				}
			},
		},
		{
			name:    "renewal/begin_clear_invalidate",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				func(run *runningRun) {
					if id := run.beginRenewal(func() {}); id != 1 {
						t.Fatalf("beginRenewal returned id %d, want 1", id)
					}
					run.clearRenewal()
					if renewCancel := run.invalidateRenewal(); renewCancel != nil {
						t.Fatalf("invalidateRenewal returned a handle clearRenewal had already dropped")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.renewCancel != nil {
					t.Fatalf("invalidateRenewal kept the renewal handle")
				}
				if run.renewCancelID != 2 {
					t.Fatalf("renewCancelID = %d after one begin and one invalidate, want 2", run.renewCancelID)
				}
			},
		},
		{
			name:    "settlement_ownership/claim_reserve_release_finalize",
			initial: func() *runningRun { return &runningRun{runLifecycle: runLifecycle{slotHeld: true}} },
			steps: []func(*runningRun){
				func(run *runningRun) {
					if claim := run.claimSettlementOwner(false); claim != 1 {
						t.Fatalf("first claim = %d, want 1", claim)
					}
					claim, renewCancel := run.reserveSettlementOwner()
					if claim != 2 || renewCancel != nil {
						t.Fatalf("second claim = (%d, %v), want (2, nil)", claim, renewCancel)
					}
					run.releaseSettlementOwner()
					run.finalizeConclusiveSettlement()
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeTerminalOwner || run.nativeStartTerminalOwner || run.nativeTerminalOwnerClaim != 0 {
					t.Fatalf("releaseSettlementOwner kept ownership: owner=%t startOwner=%t claim=%d", run.nativeTerminalOwner, run.nativeStartTerminalOwner, run.nativeTerminalOwnerClaim)
				}
				if run.terminalizing != 0 {
					t.Fatalf("terminalizing = %d after both claims released, want 0", run.terminalizing)
				}
				if !run.nativeUsageFinalized || run.cleanupBlocked {
					t.Fatalf("finalizeConclusiveSettlement left the barrier: finalized=%t cleanupBlocked=%t", run.nativeUsageFinalized, run.cleanupBlocked)
				}
			},
		},
		{
			name: "usage_barrier/schedule_attempt_fail_republish",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeTerminalOwner: true, nativeTerminalOwnerClaim: 1}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.scheduleUsageRetry(run.nativeTerminalOwnerClaim, true, time.Now().UTC())
					if !run.nativeUsageRetryPending || run.nativeUsageRetryInFlight || run.nativeUsageRetryExhausted || run.nativeUsageRetryAttempts != 1 || !run.nativeUsageRetryNeedsPrepare || run.nativeUsageRetryOwnerClaim != 1 || !run.nativeUsageRenewalBlocked || run.nativeUsageRetryAt.IsZero() || run.nativeUsageRetryDeadline.IsZero() || !run.cleanupBlocked {
						t.Fatalf("scheduleUsageRetry did not arm the barrier: pending=%t inFlight=%t exhausted=%t attempts=%d needsPrepare=%t ownerClaim=%d renewalBlocked=%t atZero=%t deadlineZero=%t cleanupBlocked=%t", run.nativeUsageRetryPending, run.nativeUsageRetryInFlight, run.nativeUsageRetryExhausted, run.nativeUsageRetryAttempts, run.nativeUsageRetryNeedsPrepare, run.nativeUsageRetryOwnerClaim, run.nativeUsageRenewalBlocked, run.nativeUsageRetryAt.IsZero(), run.nativeUsageRetryDeadline.IsZero(), run.cleanupBlocked)
					}
				},
				func(run *runningRun) {
					if !run.startUsageRetryAttempt(time.Now().UTC()) {
						t.Fatalf("startUsageRetryAttempt refused a fresh retry budget")
					}
					if !run.nativeUsageRetryInFlight {
						t.Fatalf("startUsageRetryAttempt did not open the attempt")
					}
				},
				func(run *runningRun) {
					run.nativeUsageRetryPending = false
					run.finishUsageRetryInFlight(false)
					if run.nativeUsageRetryInFlight || !run.nativeUsageRetryPending || !run.nativeUsageRetryAt.IsZero() || run.nativeUsageRetryOwnerClaim != 1 {
						t.Fatalf("finishUsageRetryInFlight = (inFlight=%t pending=%t atZero=%t ownerClaim=%d), want (false, true, true, 1)", run.nativeUsageRetryInFlight, run.nativeUsageRetryPending, run.nativeUsageRetryAt.IsZero(), run.nativeUsageRetryOwnerClaim)
					}
				},
				func(run *runningRun) {
					attempt, exhausted := run.recordUsageRetryFailure(time.Now().UTC())
					if attempt != 2 || exhausted {
						t.Fatalf("recordUsageRetryFailure = (%d, %t), want (2, false)", attempt, exhausted)
					}
					if run.nativeUsageRetryInFlight || !run.nativeUsageRetryPending || run.nativeUsageRetryAt.IsZero() {
						t.Fatalf("recordUsageRetryFailure did not re-arm the retry: inFlight=%t pending=%t atZero=%t", run.nativeUsageRetryInFlight, run.nativeUsageRetryPending, run.nativeUsageRetryAt.IsZero())
					}
				},
				func(run *runningRun) {
					run.nativeUsageRetryInFlight = true
					run.nativeUsageRenewalBlocked = false
					run.clearUsageRetryBeforePublication(false)
					if run.nativeUsageRetryPending || run.nativeUsageRetryInFlight || !run.nativeUsageRenewalBlocked {
						t.Fatalf("clearUsageRetryBeforePublication = (pending=%t inFlight=%t renewalBlocked=%t), want (false, false, true)", run.nativeUsageRetryPending, run.nativeUsageRetryInFlight, run.nativeUsageRenewalBlocked)
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeUsageRetryPending || run.nativeUsageRetryInFlight || run.nativeUsageRetryExhausted {
					t.Fatalf("the barrier stayed armed: pending=%t inFlight=%t exhausted=%t", run.nativeUsageRetryPending, run.nativeUsageRetryInFlight, run.nativeUsageRetryExhausted)
				}
				if !run.nativeUsageRenewalBlocked {
					t.Fatalf("clearUsageRetryBeforePublication re-enabled renewal")
				}
				if run.nativeUsageRetryAttempts != 2 || run.nativeUsageRetryDeadline.IsZero() {
					t.Fatalf("the retry schedule was not retained: attempts=%d deadlineZero=%t", run.nativeUsageRetryAttempts, run.nativeUsageRetryDeadline.IsZero())
				}
			},
		},
		{
			name: "usage_barrier/recovery_schedule_then_exhaust_for_cleanup",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeTerminalOwner: true, nativeTerminalOwnerClaim: 1, nativeUsageRetryAttempts: nativeUsageRetryLimit + 4}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.scheduleRecoveryUsageRetry(time.Now().UTC())
					if !run.nativeUsageRetryNeedsPrepare {
						t.Fatalf("scheduleRecoveryUsageRetry did not require a prepare step for a missing usage body")
					}
					run.exhaustUsageRetryForCleanup()
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.nativeUsageRetryExhausted || !run.stale || !run.cleanupBlocked {
					t.Fatalf("exhaustUsageRetryForCleanup left the run releasable: exhausted=%t stale=%t cleanupBlocked=%t", run.nativeUsageRetryExhausted, run.stale, run.cleanupBlocked)
				}
				if run.nativeUsageRetryAttempts != nativeUsageRetryLimit+5 {
					t.Fatalf("recovery attempt numbering = %d, want %d", run.nativeUsageRetryAttempts, nativeUsageRetryLimit+5)
				}
			},
		},
		{
			name: "usage_barrier/remember_final_usage",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeUsageRetryNeedsPrepare: true}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					usage := protocol.Usage{UsageID: "usage-1"}
					run.rememberFinalUsage(usage)
					usage.UsageID = "mutated-after-remember"
					if run.nativeFinalUsage == nil || run.nativeFinalUsage.UsageID != "usage-1" {
						t.Fatalf("rememberFinalUsage did not retain an isolated copy of the usage body: %v", run.nativeFinalUsage)
					}
					if run.nativeUsageRetryNeedsPrepare {
						t.Fatalf("rememberFinalUsage kept the prepare flag set")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeFinalUsage == nil || run.nativeFinalUsage.UsageID != "usage-1" {
					t.Fatalf("rememberFinalUsage did not retain the usage body: %v", run.nativeFinalUsage)
				}
				if run.nativeUsageRetryNeedsPrepare {
					t.Fatalf("rememberFinalUsage kept the prepare flag set")
				}
			},
		},
		{
			name: "usage_barrier/clear_retry_clears_the_cancel_observation",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{
					slotHeld:                     true,
					nativeUsageRetryInFlight:     true,
					nativeCancelCommandID:        "command-1",
					nativeCancelTerminalObserved: true,
					nativeCancelRejected:         true,
				}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.clearUsageRetryBeforePublication(false)
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeUsageRetryInFlight || !run.nativeUsageRenewalBlocked {
					t.Fatalf("clearUsageRetryBeforePublication = (inFlight=%t renewalBlocked=%t), want (false, true)", run.nativeUsageRetryInFlight, run.nativeUsageRenewalBlocked)
				}
				if run.nativeCancelTerminalObserved || run.nativeCancelRejected {
					t.Fatalf("clearUsageRetryBeforePublication kept the cancel observation: observed=%t rejected=%t", run.nativeCancelTerminalObserved, run.nativeCancelRejected)
				}
			},
		},
		{
			name: "close_retry/require_begin_end_publish",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true}, nativeSession: lifecycleTestSession, goalSession: &state.GoalSessionKey{}, goalAdmission: &protocol.Admission{}}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.requireCloseRetry()
					if !run.nativeCloseRetryRequired || !run.cleanupBlocked {
						t.Fatalf("requireCloseRetry = (required=%t cleanupBlocked=%t), want (true, true)", run.nativeCloseRetryRequired, run.cleanupBlocked)
					}
				},
				func(run *runningRun) {
					run.beginCloseRetry()
					if !run.nativeCloseRetrying || !run.nativeCloseRetryRequired {
						t.Fatalf("beginCloseRetry = (retrying=%t required=%t), want (true, true)", run.nativeCloseRetrying, run.nativeCloseRetryRequired)
					}
				},
				func(run *runningRun) {
					run.endCloseRetry()
					if run.nativeCloseRetrying || !run.nativeCloseRetryRequired {
						t.Fatalf("endCloseRetry = (retrying=%t required=%t), want (false, true)", run.nativeCloseRetrying, run.nativeCloseRetryRequired)
					}
				},
				(*runningRun).publishCloseRetrySuccess,
			},
			check: func(t *testing.T, run *runningRun) {
				if run.nativeCloseRetryRequired || run.nativeCloseRetrying {
					t.Fatalf("publishCloseRetrySuccess kept the retry: required=%t retrying=%t", run.nativeCloseRetryRequired, run.nativeCloseRetrying)
				}
				if run.nativeSession != nil || run.goalSession != nil || run.goalAdmission != nil {
					t.Fatalf("publishCloseRetrySuccess kept the session identity")
				}
				if run.cleanupBlocked {
					t.Fatalf("publishCloseRetrySuccess kept the cleanup memo set")
				}
			},
		},
		{
			name: "containment/mark_exit_stop_finalize_clear",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true}, process: lifecycleTestProcess}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if !run.markProcessExited(lifecycleTestProcess) {
						t.Fatalf("markProcessExited did not take finalization ownership")
					}
					if !run.processExited || !run.cleanupBlocked {
						t.Fatalf("markProcessExited = (exited=%t cleanupBlocked=%t), want (true, true)", run.processExited, run.cleanupBlocked)
					}
					if !run.recordProcessStop(lifecycleTestProcess, 71, "native:71", true) {
						t.Fatalf("recordProcessStop did not record the stop witness")
					}
					run.takeFinalizeOwnership(lifecycleTestProcess)
					run.recordFinalizeAttempt()
					if run.processFinalizeAttempts != 1 {
						t.Fatalf("recordFinalizeAttempt counted %d attempts, want 1", run.processFinalizeAttempts)
					}
					if run.markProcessExited(lifecycleTestProcess) {
						t.Fatalf("markProcessExited re-took finalization ownership from the current owner")
					}
					if run.processFinalizeAttempts != 1 {
						t.Fatalf("markProcessExited reset the attempt budget of the current owner")
					}
					run.markFinalizeExhausted()
					if !run.processFinalizeExhausted {
						t.Fatalf("markFinalizeExhausted did not exhaust the finalization budget")
					}
					run.recordFinalizedStop(lifecycleTestProcess, 71, "native:71")
					run.clearCleanupBlock()
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if !run.processExited || run.processFinalizeExhausted {
					t.Fatalf("the containment state was lost: exited=%t exhausted=%t", run.processExited, run.processFinalizeExhausted)
				}
				if run.processStopPID != 71 || run.processStopIdentity != "native:71" || run.processStopWitness != Process(lifecycleTestProcess) {
					t.Fatalf("the stop witness was lost: pid=%d identity=%q witness=%v", run.processStopPID, run.processStopIdentity, run.processStopWitness)
				}
				if run.processFinalizeAttempts != 1 {
					t.Fatalf("recordFinalizeAttempt counted %d attempts, want 1", run.processFinalizeAttempts)
				}
				if run.cleanupBlocked {
					t.Fatalf("clearCleanupBlock left the memo set")
				}
			},
		},
		{
			name: "containment/stop_resets_the_finalize_budget",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true}, process: lifecycleTestProcess}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					run.recordFinalizeAttempt()
					run.markFinalizeExhausted()
					if !run.recordProcessStop(lifecycleTestProcess, 71, "native:71", true) {
						t.Fatalf("recordProcessStop did not record the stop witness")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.processFinalizeAttempts != 0 || run.processFinalizeExhausted {
					t.Fatalf("recordProcessStop kept the finalize budget: attempts=%d exhausted=%t", run.processFinalizeAttempts, run.processFinalizeExhausted)
				}
				if run.processStopWitness != Process(lifecycleTestProcess) || run.processStopPID != 71 || run.processStopIdentity != "native:71" {
					t.Fatalf("recordProcessStop lost the stop witness: pid=%d identity=%q witness=%v", run.processStopPID, run.processStopIdentity, run.processStopWitness)
				}
			},
		},
		{
			name: "containment/stop_without_witness_stays_detectable",
			initial: func() *runningRun {
				return &runningRun{runLifecycle: runLifecycle{slotHeld: true, processExited: true, processStopWitness: lifecycleTestProcess, processStopPID: 71, processStopIdentity: "native:71"}, process: lifecycleTestProcess}
			},
			steps: []func(*runningRun){
				func(run *runningRun) {
					if run.recordProcessStop(lifecycleTestProcess, 0, "", false) {
						t.Fatalf("recordProcessStop reported a stop witness for a process that did not stop")
					}
				},
			},
			check: func(t *testing.T, run *runningRun) {
				if run.processStopWitness != nil || run.processStopPID != 0 || run.processStopIdentity != "" {
					t.Fatalf("recordProcessStop wrote a witness for a process that did not stop")
				}
				if run.phase() != phaseContainment {
					t.Fatalf("phase = %q after an exit without a stop witness, want %q", run.phase(), phaseContainment)
				}
			},
		},
	}

	for _, test := range tests {
		t.Logf("row %s", test.name)
		run := test.initial()
		for _, step := range test.steps {
			step(run)
		}
		test.check(t, run)
	}
}

func TestRunLifecycleUnmodelledStatesStayWritableAndDetectable(t *testing.T) {
	t.Run("orphaned_usage_retry_is_superseded_by_the_next_claim", func(t *testing.T) {
		run := &runningRun{runLifecycle: runLifecycle{
			slotHeld:                   true,
			nativeUsageRetryPending:    true,
			nativeUsageRetryInFlight:   true,
			nativeUsageRetryExhausted:  true,
			nativeUsageRetryOwnerClaim: 9,
		}}
		if !run.runLifecycle.cleanupBlocked() {
			t.Fatalf("cleanupBlocked() = false for an orphaned retry, want true")
		}
		if claim := run.claimSettlementOwner(true); claim != 1 {
			t.Fatalf("claim = %d after the orphaned retry, want 1", claim)
		}
		if run.nativeUsageRetryPending || run.nativeUsageRetryInFlight || run.nativeUsageRetryExhausted || run.nativeUsageRetryOwnerClaim != 0 {
			t.Fatalf("the orphaned retry survived the claim: pending=%t inFlight=%t exhausted=%t ownerClaim=%d", run.nativeUsageRetryPending, run.nativeUsageRetryInFlight, run.nativeUsageRetryExhausted, run.nativeUsageRetryOwnerClaim)
		}
		if !run.runLifecycle.cleanupBlocked() || !run.cleanupBlocked {
			t.Fatalf("the claim did not leave cleanup blocked: memo=%t predicate=%t", run.cleanupBlocked, run.runLifecycle.cleanupBlocked())
		}
	})

	t.Run("released_slot_with_an_open_claim_is_detectable", func(t *testing.T) {
		run := &runningRun{runLifecycle: runLifecycle{nativeTerminalOwner: true, nativeUsageRetryPending: true}}
		if !run.runLifecycle.cleanupBlocked() {
			t.Fatalf("cleanupBlocked() = false for an open claim, want true")
		}
		if got := run.phase(); got != phaseRetired {
			t.Fatalf("phase = %q for a released slot with an open claim, want %q", got, phaseRetired)
		}
		run.releaseTerminalReservation()
		if run.terminalizing != 0 {
			t.Fatalf("releaseTerminalReservation moved terminalizing to %d, want 0", run.terminalizing)
		}
		run.releaseSettlementOwner()
		if run.nativeTerminalOwner || run.terminalizing != 0 {
			t.Fatalf("releaseSettlementOwner kept ownership: owner=%t terminalizing=%d", run.nativeTerminalOwner, run.terminalizing)
		}
		if !run.runLifecycle.cleanupBlocked() {
			t.Fatalf("cleanupBlocked() = false while the usage retry is still pending, want true")
		}
		run.exhaustUsageRetryForCleanup()
		if !run.runLifecycle.cleanupBlocked() {
			t.Fatalf("cleanupBlocked() = false after the retry was exhausted, want true")
		}
		run.finalizeConclusiveSettlement()
		if run.runLifecycle.cleanupBlocked() {
			t.Fatalf("cleanupBlocked() = true after the conclusive settlement, want false")
		}
	})

	t.Run("repeated_settlement_claims_keep_distinct_ownership", func(t *testing.T) {
		run := &runningRun{runLifecycle: runLifecycle{slotHeld: true}}
		first := run.claimSettlementOwner(true)
		second := run.claimSettlementOwner(false)
		if first == second {
			t.Fatalf("two claims share the owner claim id %d", first)
		}
		if !nativeTerminalOwnedBy(run, second) || nativeTerminalOwnedBy(run, first) {
			t.Fatalf("ownership did not move to the newest claim: claimed=%d", run.nativeTerminalOwnerClaim)
		}
	})

	t.Run("unrelated_flags_do_not_alias", func(t *testing.T) {
		run := &runningRun{runLifecycle: runLifecycle{slotHeld: true, cancelled: true, nativeCloseRetryRequired: true}}
		if run.phase() != phaseCloseRetry {
			t.Fatalf("phase = %q for a cancelled run with an open close retry, want %q", run.phase(), phaseCloseRetry)
		}
		run.clearCancel()
		if run.cancelled || run.cancelCommandID != "" {
			t.Fatalf("clearCancel moved unrelated state: cancelled=%t id=%q", run.cancelled, run.cancelCommandID)
		}
		if !run.nativeCloseRetryRequired {
			t.Fatalf("clearCancel cleared the close retry")
		}
	})
}

func TestRunLifecycleCleanupBlockedTruthTable(t *testing.T) {
	flags := []struct {
		name string
		set  func(*runLifecycle)
	}{
		{"nativeTerminalOwner", func(state *runLifecycle) { state.nativeTerminalOwner = true }},
		{"nativeCloseRetryRequired", func(state *runLifecycle) { state.nativeCloseRetryRequired = true }},
		{"nativeCloseRetrying", func(state *runLifecycle) { state.nativeCloseRetrying = true }},
		{"nativeUsageRetryPending", func(state *runLifecycle) { state.nativeUsageRetryPending = true }},
		{"nativeUsageRetryInFlight", func(state *runLifecycle) { state.nativeUsageRetryInFlight = true }},
		{"nativeUsageRetryExhausted", func(state *runLifecycle) { state.nativeUsageRetryExhausted = true }},
	}
	for mask := range 1 << len(flags) {
		state := &runLifecycle{}
		var names []string
		for index, flag := range flags {
			if mask&(1<<index) == 0 {
				continue
			}
			flag.set(state)
			names = append(names, flag.name)
		}
		want := len(names) > 0
		if got := state.cleanupBlocked(); got != want {
			t.Fatalf("cleanupBlocked() = %t for {%s}, want %t", got, strings.Join(names, ", "), want)
		}
	}
}

func TestRunLifecyclePhaseTable(t *testing.T) {
	tests := []struct {
		name string
		run  *runningRun
		want runPhase
	}{
		{"new", &runningRun{runLifecycle: runLifecycle{slotHeld: true}}, phaseNew},
		{"claimed", &runningRun{runLifecycle: runLifecycle{slotHeld: true, claimed: true}}, phaseClaimed},
		{"attached_process", &runningRun{runLifecycle: runLifecycle{slotHeld: true, claimed: true}, process: lifecycleTestProcess}, phaseAttached},
		{"attached_native", &runningRun{runLifecycle: runLifecycle{slotHeld: true}, nativeSession: lifecycleTestSession}, phaseAttached},
		{"starting", &runningRun{runLifecycle: runLifecycle{slotHeld: true, claimed: true, nativeStartInFlight: true}}, phaseStarting},
		{"cancelling", &runningRun{runLifecycle: runLifecycle{slotHeld: true, cancelled: true, nativeStartInFlight: true}}, phaseCancelling},
		{"lease_lost", &runningRun{runLifecycle: runLifecycle{slotHeld: true, stale: true, cancelled: true}}, phaseLeaseLost},
		{"reserving", &runningRun{runLifecycle: runLifecycle{slotHeld: true, terminalizing: 1, stale: true}}, phaseReserving},
		{"containment", &runningRun{runLifecycle: runLifecycle{slotHeld: true, processExited: true}}, phaseContainment},
		{"containment_from_memo", &runningRun{runLifecycle: runLifecycle{slotHeld: true}, cleanupBlocked: true}, phaseContainment},
		{"close_retry", &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeCloseRetrying: true}, cleanupBlocked: true}, phaseCloseRetry},
		{"usage_retry", &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeUsageRetryPending: true, processExited: true}}, phaseUsageRetry},
		{"settlement", &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeTerminalOwner: true, nativeCloseRetryRequired: true}}, phaseSettlement},
		{"usage_finalized", &runningRun{runLifecycle: runLifecycle{slotHeld: true, nativeUsageFinalized: true}}, phaseSettlement},
		{"terminal_dominates_retries", &runningRun{runLifecycle: runLifecycle{slotHeld: true, terminal: true, nativeUsageRetryExhausted: true, stale: true}}, phaseTerminal},
		{"released_dominates_terminal", &runningRun{runLifecycle: runLifecycle{terminal: true, nativeUsageRetryPending: true}}, phaseRetired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.run.phase(); got != test.want {
				t.Fatalf("phase() = %q, want %q", got, test.want)
			}
		})
	}
}

var lifecycleConstructors = map[string]bool{
	"app.go:startAssignment":                true,
	"linux_owner_witness_linux.go:runFresh": true,
}

func TestRunLifecycleWritesStayInTheModule(t *testing.T) {
	fset := token.NewFileSet()
	modulePath := "run_lifecycle.go"
	module, err := parser.ParseFile(fset, modulePath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", modulePath, err)
	}

	declared := map[string]bool{}
	ast.Inspect(module, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "runLifecycle" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				declared[name.Name] = true
			}
			if len(field.Names) == 0 {
				t.Fatalf("runLifecycle declares an embedded field %s", field.Type)
			}
		}
		return false
	})
	if len(declared) == 0 {
		t.Fatalf("runLifecycle struct declaration not found in %s", modulePath)
	}

	wantModule := map[string]bool{}
	wantWrites := map[string]bool{}
	for _, name := range lifecycleFieldNames {
		wantWrites[name] = true
	}
	for _, name := range lifecycleFieldNames {
		if name == "cleanupBlocked" {
			continue
		}
		wantModule[name] = true
	}
	if missing, extra := setNameDelta(wantModule, declared); len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("runLifecycle field set drifted: missing=%v extra=%v", missing, extra)
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}
	sort.Strings(files)
	embeddingChecked := false
	runningRunHandles := map[string]bool{}
	var sites []string
	for _, path := range files {
		if path == modulePath || strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if path == "native_run.go" {
			embeddingChecked = checkRunLifecycleEmbedding(t, path, parsed)
			runningRunHandles = funcFieldsOf(parsed, "runningRun")
		}
		sites = append(sites, lifecycleWriteSites(path, fset, parsed, wantWrites)...)
	}
	if len(sites) > 0 {
		t.Fatalf("lifecycle writes outside %s:\n%s", modulePath, strings.Join(sites, "\n"))
	}
	if !embeddingChecked {
		t.Fatalf("runningRun does not embed runLifecycle in native_run.go")
	}

	handleNames := map[string]bool{"Lock": true, "Unlock": true, "RLock": true, "TryLock": true}
	for name := range funcFieldsOf(module, "runLifecycle") {
		handleNames[name] = true
	}
	for name := range runningRunHandles {
		handleNames[name] = true
	}
	var calls []string
	ast.Inspect(module, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if handleNames[selector.Sel.Name] {
			calls = append(calls, fmt.Sprintf("%s:%d calls %s", modulePath, fset.Position(call.Pos()).Line, selector.Sel.Name))
		}
		return true
	})
	if len(calls) > 0 {
		t.Fatalf("the module takes a lock or invokes a captured handle:\n%s", strings.Join(calls, "\n"))
	}
}

func lifecycleWriteSites(path string, fset *token.FileSet, parsed *ast.File, wantWrites map[string]bool) []string {
	var sites []string
	add := func(pos token.Pos, message string) {
		sites = append(sites, fmt.Sprintf("%s:%d %s", path, fset.Position(pos).Line, message))
	}
	isLifecycleName := func(name string) bool {
		return name == "runLifecycle" || wantWrites[name]
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for _, target := range typed.Lhs {
				recordLifecycleTarget(target, add, isLifecycleName)
			}
		case *ast.IncDecStmt:
			recordLifecycleTarget(typed.X, add, isLifecycleName)
		case *ast.UnaryExpr:
			if typed.Op != token.AND {
				return true
			}
			if selector, ok := typed.X.(*ast.SelectorExpr); ok && isLifecycleName(selector.Sel.Name) {
				add(typed.Pos(), fmt.Sprintf("escapes &%s", selector.Sel.Name))
			}
		case *ast.KeyValueExpr:
			ident, ok := typed.Key.(*ast.Ident)
			if !ok || ident.Name != "runLifecycle" {
				return true
			}
			if owner := enclosingFunc(parsed, typed.Pos()); owner == "" || !lifecycleConstructors[path+":"+owner] {
				add(typed.Pos(), fmt.Sprintf("constructs a runLifecycle value outside the allowlist (owner %q)", owner))
			}
		}
		return true
	})
	return sites
}

func recordLifecycleTarget(target ast.Expr, add func(token.Pos, string), isLifecycleName func(string) bool) {
	selector, ok := target.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if selector.Sel.Name == "runLifecycle" {
		add(selector.Pos(), "replaces the whole runLifecycle value")
		return
	}
	if isLifecycleName(selector.Sel.Name) {
		add(selector.Pos(), fmt.Sprintf("writes %s", selector.Sel.Name))
	}
}

func enclosingFunc(parsed *ast.File, pos token.Pos) string {
	name := ""
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Pos() > pos || pos > fn.End() {
			continue
		}
		name = fn.Name.Name
	}
	return name
}

func funcFieldsOf(parsed *ast.File, typeName string) map[string]bool {
	fields := map[string]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != typeName {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			switch typed := field.Type.(type) {
			case *ast.FuncType:
			case *ast.SelectorExpr:
				if !strings.Contains(typed.Sel.Name, "Func") {
					continue
				}
			default:
				continue
			}
			for _, name := range field.Names {
				fields[name.Name] = true
			}
		}
		return false
	})
	return fields
}

func checkRunLifecycleEmbedding(t *testing.T, path string, parsed *ast.File) bool {
	t.Helper()
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "runningRun" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			if len(field.Names) != 0 {
				continue
			}
			if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "runLifecycle" {
				found = true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("%s: runningRun does not embed runLifecycle", path)
	}
	return true
}

func setNameDelta(want, got map[string]bool) (missing, extra []string) {
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
