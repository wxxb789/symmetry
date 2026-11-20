package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// taskResultReasonError keeps a canonical terminal reason through local
// preflight failures without asking string matching to recover domain state.
type taskResultReasonError struct {
	reason protocol.TaskResultReason
	cause  error
}

func (err *taskResultReasonError) Error() string { return err.cause.Error() }

func (err *taskResultReasonError) Unwrap() error { return err.cause }

func taskResultFailure(reason protocol.TaskResultReason, cause error) error {
	if cause == nil {
		cause = errors.New(string(reason))
	}
	return &taskResultReasonError{reason: reason, cause: cause}
}

func taskResultFailureReason(cause error) (protocol.TaskResultReason, bool) {
	var typed *taskResultReasonError
	if errors.As(cause, &typed) {
		return typed.reason, true
	}
	return "", false
}

// linuxV2NativeOwner is the App-owned host boundary for new Linux Pi/OpenCode
// executions. Preflight is read-only and must succeed before workspace or
// provider-bridge effects; BuildInvocation binds the already-claimed Run to
// the exact Store callback chain immediately before the staged adapter starts.
type linuxV2NativeOwner interface {
	Preflight(context.Context, config.Config, harness.Kind, config.AgentProfile, bool) ([]string, error)
	BuildInvocation(context.Context, state.RunKey, protocol.Fence, execution.Invocation, execution.LinuxV2JournalCallbacks) (execution.Invocation, error)
	Close() error
}

type linuxV2NativeOwnerPiGroup interface {
	trustedPiProviderExtensionGroupID() uint32
}

func setTrustedPiProviderExtensionGroup(value *config.Config, owner linuxV2NativeOwner) error {
	if value == nil || owner == nil || value.Runtime.HarnessKind != config.RuntimeHarnessPi {
		return errors.New("trusted Linux Pi owner is unavailable")
	}
	groupOwner, ok := owner.(linuxV2NativeOwnerPiGroup)
	if !ok || groupOwner.trustedPiProviderExtensionGroupID() == 0 {
		return errors.New("Linux Pi owner has no trusted installed B primary group")
	}
	groupID := groupOwner.trustedPiProviderExtensionGroupID()
	if value.PiProviderExtensionGroupID != groupID {
		value.PiProviderExtensionGroupID = groupID
	}
	return nil
}

func validatePiProviderExtensionGroupID(value config.Config, trustedBGroup uint32) error {
	if trustedBGroup == 0 || value.PiProviderExtensionGroupID != trustedBGroup {
		return errors.New("trusted fixed-B primary group is unavailable")
	}
	return nil
}

type linuxV2NativeOwnerFactory func(config.Config) (linuxV2NativeOwner, error)

var defaultLinuxV2NativeOwnerFactory linuxV2NativeOwnerFactory = func(config.Config) (linuxV2NativeOwner, error) {
	return nil, errors.New("Linux v2 native owner is supported only on Linux amd64")
}

type runningRun struct {
	// inputMu serializes stdin delivery with input-related lifecycle mutations.
	// Never wait for it while holding daemon.mu.
	inputMu sync.Mutex
	// linuxCloneOperationMu serializes one RunKey's A-owned seed preparation
	// with its exact pre-checkout abort. It is owned by the active run, not a
	// second durable workspace lock.
	linuxCloneOperationMu sync.Mutex
	// nativeCloseMu serializes one native session Close with the durable session
	// and process-marker evidence that proves its result. It is never acquired
	// while daemon.mu is held.
	nativeCloseMu         sync.Mutex
	process               Process
	nativeSession         harness.Session
	goalSession           *state.GoalSessionKey
	goalAdmission         *protocol.Admission
	linuxClone            *linuxCloneRun
	goalUsage             *harness.Usage
	goalUsageAt           time.Time
	goalCachedInputTokens int64
	goalUsageHasCached    bool
	goalUsageObservation  *state.NativeUsageObservation
	nativeDeadline        time.Time
	prepared              workspace.Prepared
	output                *agentOutput
	starting              bool
	// nativeStartInFlight closes the publication window from Adapter.Start
	// through the returned StartTurn result. Cancellation and lease expiry may
	// request stop, but cannot settle until the returned session is visible.
	nativeStartInFlight         bool
	nativeStartTerminalObserved bool
	nativeStartLeaseExpired     bool
	nativeStartLeaseReason      string
	claimed                     bool
	cancel                      context.CancelFunc
	cancelled                   bool
	cancelCommandID             string
	stale                       bool
	terminal                    bool
	terminalizing               int
	nativeTerminalOwner         bool
	nativeTerminalOwnerClaim    uint64
	nativeTerminalOwnerSequence uint64
	nativeStartTerminalOwner    bool
	// A close retry may release the terminal owner only when that retry is the
	// continuation of cancelled-start compensation, never for another owner.
	slotHeld                 bool
	cleanupBlocked           bool
	processStopWitness       Process
	processStopPID           int
	processStopIdentity      string
	processExited            bool
	processFinalizeOwner     Process
	processFinalizeAttempts  int
	processFinalizeExhausted bool
	startFailure             error
	nativeCloseRetryRequired bool
	nativeCloseRetrying      bool
	// Native completion is retained locally until accounting is durably queued.
	nativeFinalResult            *harness.TaskResult
	nativeFinalWaitErr           error
	nativeFinalCloseErr          error
	nativeFinalAdmission         *protocol.Admission
	nativeFinalUsage             *protocol.Usage
	nativeUsageRetryAt           time.Time
	nativeUsageRetryDeadline     time.Time
	nativeUsageRetryAttempts     int
	nativeUsageRetryPending      bool
	nativeUsageRetryInFlight     bool
	nativeUsageRetryOwnerClaim   uint64
	nativeUsageRetryNeedsPrepare bool
	nativeUsageRenewalBlocked    bool
	nativeUsageRetryExhausted    bool
	nativeUsageFinalized         bool
	nativeCancelCommandID        string
	nativeCancelTerminalObserved bool
	nativeCancelRejected         bool
	nativeCancelTerminateErr     error
	outputDropWarned             bool
	stopExecution                context.CancelCauseFunc
	renewCancel                  context.CancelFunc
	renewCancelID                uint64
	localLeaseDeadlineAt         time.Time
	leaseSequence                uint64
	leaseRenewalClosed           bool
	terminalCancel               context.CancelFunc
}

type linuxCloneRun struct {
	mu                sync.Mutex
	workspace         linuxV2CloneWorkspace
	grant             authority.LinuxCloneGrant
	prepared          workspace.Prepared
	checkout          *authority.LinuxCloneCheckoutReceipt
	taskResult        *protocol.TaskResult
	promotionIntent   *authority.LinuxClonePromotionIntent
	promotion         *authority.LinuxClonePromotionReceipt
	promotionEligible bool
}

func requiresLinuxV2NativeOwner(kind harness.Kind) bool {
	return runtime.GOOS == "linux" && (kind == harness.KindPi || kind == harness.KindOpenCode)
}

func linuxV2RecoveryEndpoint(stateDir string, key state.RunKey) (string, error) {
	if stateDir == "" || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || key.RunID == "" || key.Generation <= 0 {
		return "", errors.New("Linux v2 recovery endpoint inputs are invalid")
	}
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", key.RunID, key.Generation)))
	path := filepath.Join(stateDir, "owner-"+hex.EncodeToString(identity[:])+".sock")
	if len(path) >= 100 {
		return "", errors.New("StateDir is too long for the private Linux v2 helper recovery endpoint")
	}
	return path, nil
}

func (daemon *daemon) prepareLinuxV2NativeOwner(ctx context.Context, key state.RunKey, fence protocol.Fence, kind harness.Kind, profile config.AgentProfile, providerAccess bool) (linuxV2NativeOwner, []string, error) {
	if _, err := linuxV2RecoveryEndpoint(daemon.config.StateDir, key); err != nil {
		return nil, nil, fmt.Errorf("Linux v2 native owner recovery endpoint preflight: %w", err)
	}
	if err := daemon.checkLinuxV2NativeAdmission(ctx, key, fence); err != nil {
		return nil, nil, fmt.Errorf("Linux v2 native owner admission is blocked by unresolved RunJournal ownership: %w", err)
	}
	daemon.linuxV2OwnerInitMu.Lock()
	owner := daemon.linuxV2Owner
	if owner == nil {
		factory := daemon.options.linuxV2NativeOwnerFactory
		if factory == nil {
			factory = defaultLinuxV2NativeOwnerFactory
		}
		created, err := factory(daemon.config)
		if err != nil {
			daemon.linuxV2OwnerInitMu.Unlock()
			return nil, nil, fmt.Errorf("initialize Linux v2 native owner: %w", err)
		}
		if created == nil {
			daemon.linuxV2OwnerInitMu.Unlock()
			return nil, nil, errors.New("initialize Linux v2 native owner: constructor returned nil")
		}
		daemon.linuxV2Owner = created
		owner = created
	}
	if kind == harness.KindPi {
		// This runtime-only value is derived from the owner constructed from the
		// strict root-installed A/B config; JSON and ambient environment can
		// never select the provider extension's group. Initialize it under the
		// same lock before any concurrent native preflight reads the config.
		if err := setTrustedPiProviderExtensionGroup(&daemon.config, owner); err != nil {
			daemon.linuxV2OwnerInitMu.Unlock()
			return nil, nil, errors.Join(err, owner.Close())
		}
	}
	daemon.linuxV2OwnerInitMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	environment, err := owner.Preflight(ctx, daemon.config, kind, profile, providerAccess)
	if err != nil {
		return nil, nil, fmt.Errorf("Linux v2 native owner preflight: %w", err)
	}
	return owner, environment, nil
}

func (daemon *daemon) checkLinuxV2NativeAdmissionBeforeClaim(ctx context.Context) error {
	return daemon.withLinuxV2NativeAdmission(ctx, state.RunKey{}, func(liveOwners map[state.RunKey]protocol.Fence) error {
		return daemon.store.CheckLinuxV2NativeAdmissionBeforeClaim(daemon.now(), liveOwners)
	})
}

func (daemon *daemon) checkLinuxV2NativeAdmission(ctx context.Context, key state.RunKey, fence protocol.Fence) error {
	return daemon.withLinuxV2NativeAdmission(ctx, key, func(liveOwners map[state.RunKey]protocol.Fence) error {
		return daemon.store.CheckLinuxV2NativeAdmission(key, fence, daemon.now(), liveOwners)
	})
}

func (daemon *daemon) bindLinuxCloneReuse(ctx context.Context, key state.RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant) error {
	if daemon == nil || daemon.store == nil {
		return errors.New("Linux clone reuse requires the App Store")
	}
	return daemon.withLinuxV2NativeAdmission(ctx, key, func(liveOwners map[state.RunKey]protocol.Fence) error {
		_, err := daemon.store.BindLinuxCloneReuse(key, fence, grant, liveOwners)
		return err
	})
}

func (daemon *daemon) abortLinuxCloneReuseBeforeNativeStart(ctx context.Context, key state.RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant) error {
	if daemon == nil || daemon.store == nil {
		return errors.New("Linux clone reuse abort requires the App Store")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	_, err := daemon.store.AbortLinuxCloneReuseBeforeNativeStart(key, fence, grant)
	return err
}

// withLinuxV2NativeAdmission keeps App admission snapshots ordered with the
// Store CAS while never holding daemon.mu across status RPCs or State access.
func (daemon *daemon) withLinuxV2NativeAdmission(
	ctx context.Context,
	excludedKey state.RunKey,
	apply func(map[state.RunKey]protocol.Fence) error,
) error {
	if daemon == nil || daemon.store == nil || apply == nil {
		return errors.New("Linux v2 admission requires the App Store and check")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	boundedContext, cancel := context.WithTimeout(ctx, linuxV2AdmissionSnapshotTimeout)
	defer cancel()
	ctx = boundedContext
	if err := ctx.Err(); err != nil {
		return err
	}
	daemon.linuxV2AdmissionMu.Lock()
	defer daemon.linuxV2AdmissionMu.Unlock()
	if excludedKey != (state.RunKey{}) {
		if err := daemon.checkLinuxV2TargetAdmission(excludedKey); err != nil {
			return err
		}
	}
	liveOwners, err := daemon.linuxV2LiveOwnerFences(ctx, excludedKey)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if excludedKey != (state.RunKey{}) {
		if err := daemon.checkLinuxV2TargetAdmission(excludedKey); err != nil {
			return err
		}
	}
	return apply(liveOwners)
}

func claimNativeTerminalOwner(active *runningRun, startOwner bool) uint64 {
	if !active.nativeTerminalOwner && (active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.nativeUsageRetryExhausted || active.nativeUsageRetryOwnerClaim != 0) {
		// A retry without its originating terminal claim cannot publish or
		// release anything. A later legitimate owner supersedes that orphaned
		// in-memory work while retaining the durable usage evidence.
		active.nativeUsageRetryPending = false
		active.nativeUsageRetryInFlight = false
		active.nativeUsageRetryExhausted = false
		active.nativeUsageRetryOwnerClaim = 0
	}
	active.nativeTerminalOwnerSequence++
	if active.nativeTerminalOwnerSequence == 0 {
		active.nativeTerminalOwnerSequence++
	}
	active.nativeTerminalOwner = true
	active.nativeTerminalOwnerClaim = active.nativeTerminalOwnerSequence
	active.nativeStartTerminalOwner = startOwner
	active.terminalizing++
	active.cleanupBlocked = nativeCleanupBlocked(active)
	return active.nativeTerminalOwnerClaim
}

func nativeTerminalOwnedBy(active *runningRun, ownerClaim uint64) bool {
	return active != nil && active.nativeTerminalOwner && active.nativeTerminalOwnerClaim == ownerClaim
}

func (daemon *daemon) reserveNativeTerminalClaim(key state.RunKey, expected *runningRun) (uint64, bool) {
	now := daemon.localNow()
	daemon.mu.Lock()
	active := daemon.running[key]
	if expected == nil || active != expected || expected.nativeTerminalOwner || expected.cancelled || expected.terminal || expected.terminalizing != 0 ||
		(!expected.localLeaseDeadlineAt.IsZero() && !now.Before(expected.localLeaseDeadlineAt)) {
		daemon.mu.Unlock()
		return 0, false
	}
	ownerClaim := claimNativeTerminalOwner(expected, false)
	renewCancel := expected.renewCancel
	expected.renewCancel = nil
	expected.renewCancelID++
	daemon.mu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	return ownerClaim, true
}

// reserveNativeObservedTerminalClaim linearizes a completed native turn with
// lease expiry. Once WaitTurn returns terminal evidence, elapsed wall time alone
// cannot let a watchdog that has not yet claimed settlement replace it.
func (daemon *daemon) reserveNativeObservedTerminalClaim(key state.RunKey, expected *runningRun) (uint64, bool) {
	daemon.eventReceiptMu.Lock()
	daemon.mu.Lock()
	active := daemon.running[key]
	if expected == nil || active != expected || expected.nativeTerminalOwner || expected.cancelled || expected.terminal || expected.terminalizing != 0 {
		daemon.mu.Unlock()
		daemon.eventReceiptMu.Unlock()
		return 0, false
	}
	ownerClaim := claimNativeTerminalOwner(expected, true)
	expected.nativeStartTerminalObserved = true
	expected.nativeUsageRenewalBlocked = true
	renewCancel := expected.renewCancel
	expected.renewCancel = nil
	expected.renewCancelID++
	daemon.mu.Unlock()
	daemon.eventReceiptMu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	return ownerClaim, true
}

// beginNativeStart owns Adapter.Start through the returned StartTurn result.
// Cancellation and lease expiry may cancel the launch context while this bit
// is set, but neither may settle an unpublished native session.
func (daemon *daemon) beginNativeStart(key state.RunKey) bool {
	now := daemon.localNow()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	// Focused admission tests may call startGoalAdmission without a runningRun;
	// production assignment paths always install one.
	if active == nil {
		return true
	}
	if active.nativeStartInFlight || active.cancelled || active.stale || active.terminal || active.terminalizing != 0 ||
		(!active.localLeaseDeadlineAt.IsZero() && !now.Before(active.localLeaseDeadlineAt)) {
		return false
	}
	active.nativeStartInFlight = true
	return true
}

// publishNativeStartResult makes the adapter result visible without releasing
// start ownership. The caller releases it only after cancellation, lease loss,
// or StartTurn has settled.
func (daemon *daemon) publishNativeStartResult(key state.RunKey, session harness.Session, sessionKey state.GoalSessionKey, admission protocol.Admission, prepared workspace.Prepared) {
	if isNilHarnessSession(session) {
		session = nil
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil || session == nil {
		return
	}
	admissionCopy := admission
	active.nativeSession = session
	active.goalSession = &sessionKey
	active.goalAdmission = &admissionCopy
	active.prepared = prepared
	active.cleanupBlocked = true
}

func (daemon *daemon) finishNativeStartPublication(key state.RunKey) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if active := daemon.running[key]; active != nil {
		active.nativeStartInFlight = false
	}
}

// transferNativeStartTerminal lets terminal or usage evidence durably observed
// during StartTurn retain settlement ownership after the lease watchdog fires.
// Only a TaskResult proves that the stale lease outcome may be replaced.
func (daemon *daemon) transferNativeStartTerminal(key state.RunKey, expected *runningRun, session harness.Session) bool {
	daemon.eventReceiptMu.Lock()
	defer daemon.eventReceiptMu.Unlock()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active != expected || active.nativeSession != session || !active.nativeStartInFlight || active.nativeTerminalOwner || active.terminal || active.terminalizing != 0 {
		return false
	}
	terminalObserved := active.nativeStartTerminalObserved
	usageObserved := active.goalUsageObservation != nil || active.goalUsage != nil
	if !terminalObserved && !usageObserved {
		return false
	}
	active.nativeStartInFlight = false
	if terminalObserved {
		active.stale = false
		active.nativeDeadline = time.Time{}
	}
	claimNativeTerminalOwner(active, true)
	active.nativeUsageRenewalBlocked = true
	active.cleanupBlocked = nativeCleanupBlocked(active)
	return true
}

func (daemon *daemon) nativeStartTerminalOwnerClaim(key state.RunKey, expected *runningRun) (uint64, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.running[key] != expected || expected == nil || !expected.nativeStartTerminalOwner || !nativeTerminalOwnedBy(expected, expected.nativeTerminalOwnerClaim) {
		return 0, false
	}
	return expected.nativeTerminalOwnerClaim, true
}

// releaseNativeTerminalOwner is idempotent because terminal delivery, stale
// recovery, and run release can converge on the same native completion.
func (daemon *daemon) releaseNativeTerminalOwner(key state.RunKey, expected *runningRun, ownerClaim uint64) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active != expected || !nativeTerminalOwnedBy(active, ownerClaim) {
		return
	}
	active.nativeTerminalOwner = false
	active.nativeTerminalOwnerClaim = 0
	active.nativeStartTerminalOwner = false
	if active.terminalizing > 0 {
		active.terminalizing--
	}
	active.cleanupBlocked = nativeCleanupBlocked(active)
}

func (daemon *daemon) finalizeConclusiveNativeTerminalOwner(key state.RunKey, expected *runningRun, ownerClaim uint64) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active != expected || !nativeTerminalOwnedBy(active, ownerClaim) {
		return
	}
	active.nativeUsageFinalized = true
	active.nativeUsageRetryPending = false
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryExhausted = false
	active.nativeUsageRetryOwnerClaim = 0
	active.nativeTerminalOwner = false
	active.nativeTerminalOwnerClaim = 0
	active.nativeStartTerminalOwner = false
	if active.terminalizing > 0 {
		active.terminalizing--
	}
	active.cleanupBlocked = nativeCleanupBlocked(active)
}

func processDetails(process Process) (int, string, error) {
	if isNilProcess(process) {
		return 0, "", errors.New("process does not expose a persistent identity")
	}
	pid, identity := process.ProcessDetails()
	if pid > 0 && identity != "" {
		return pid, identity, nil
	}
	return 0, "", errors.New("process does not expose a persistent identity")
}

func (daemon *daemon) processDetailsAlreadyRecorded(key state.RunKey, pid int, identity string) (bool, error) {
	if daemon.store == nil {
		return false, nil
	}
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return journal.PID == pid && journal.ProcessIdentity == identity && !journal.StartedAt.IsZero(), nil
}

func isNilProcess(process Process) bool {
	if process == nil {
		return true
	}
	value := reflect.ValueOf(process)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilHarnessSession(session harness.Session) bool {
	if session == nil {
		return true
	}
	value := reflect.ValueOf(session)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// goalProviderAccess is intentionally separate from legacy initialInput: a
// native harness must receive the broker capability out of band rather than
// through a persisted prompt or process environment.
func goalProviderAccess(profile config.AgentProfile, access *protocol.ProviderAccess) (*protocol.ProviderAccess, error) {
	if access == nil {
		return nil, nil
	}
	if !profile.ProviderAccess {
		return nil, errors.New("agent profile does not allow provider access")
	}
	copy := *access
	copy.Grants = make([]protocol.ProviderGrant, len(access.Grants))
	for index, grant := range access.Grants {
		copy.Grants[index] = grant
		copy.Grants[index].Operations = append([]string(nil), grant.Operations...)
	}
	return &copy, nil
}

// resolveCancelledNativeStartAbortFailure distinguishes a failed abort write
// from an unknown native owner. Exact missing/closed readback proves the abort;
// every other outcome retains the workspace and durable reconciliation state.
func (daemon *daemon) resolveCancelledNativeStartAbortFailure(key state.RunKey, sessionKey state.GoalSessionKey, cause error) error {
	observed, readErr := daemon.loadGoalSession(sessionKey)
	if state.IsNotFound(readErr) || (readErr == nil && goalSessionClosed(observed)) {
		return nil
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	uncertainErr := daemon.markGoalSessionUncertain(sessionKey, "cancelled native Goal launch abort outcome is unproven: "+errorText(cause))
	resolved, resolveErr := daemon.loadGoalSession(sessionKey)
	if state.IsNotFound(resolveErr) || (resolveErr == nil && goalSessionClosed(resolved)) {
		return nil
	}
	if resolveErr == nil && resolved.NeedsReconciliation() {
		resolveErr = errors.New("Goal session remains unresolved after cancelled-start abort failure")
	}
	if readErr != nil && !state.IsNotFound(readErr) {
		readErr = fmt.Errorf("read Goal session after cancelled-start abort failure: %w", readErr)
	} else {
		readErr = nil
	}
	if resolveErr != nil && !state.IsNotFound(resolveErr) {
		resolveErr = fmt.Errorf("read Goal session after cancelled-start uncertainty persistence: %w", resolveErr)
	}
	return errors.Join(cause, retentionErr, readErr, uncertainErr, resolveErr)
}

// compensatePreNativeGoalLaunchFailure resolves a failed pre-native write by
// exact readback. Only a missing or closed journal proves that no native owner
// remains; every ambiguous outcome retains the workspace for recovery.
func (daemon *daemon) compensatePreNativeGoalLaunchFailure(key state.RunKey, sessionKey state.GoalSessionKey, cause error) error {
	observed, readErr := daemon.store.LoadGoalSession(sessionKey)
	if state.IsNotFound(readErr) || (readErr == nil && goalSessionClosed(observed)) {
		return taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
	}
	if readErr != nil {
		daemon.retainUnknownGoalLaunchWorkspace(key)
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, fmt.Errorf("read Goal session after pre-native failure: %w", readErr)))
	}

	abortErr := daemon.abortGoalSessionLaunchBeforeNativeStart(sessionKey)
	if abortErr == nil {
		return taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
	}
	resolved, resolveErr := daemon.store.LoadGoalSession(sessionKey)
	if state.IsNotFound(resolveErr) || (resolveErr == nil && goalSessionClosed(resolved)) {
		return taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
	}
	if resolveErr == nil {
		uncertainErr := daemon.markGoalSessionUncertain(sessionKey, "pre-native Goal session compensation is unproven")
		if uncertainErr == nil {
			resolved, resolvedErr := daemon.store.LoadGoalSession(sessionKey)
			if state.IsNotFound(resolvedErr) || (resolvedErr == nil && goalSessionClosed(resolved)) {
				return taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
			}
			if resolvedErr != nil {
				resolveErr = resolvedErr
			} else {
				resolveErr = errors.New("Goal session remains unresolved after uncertainty compensation")
			}
		} else {
			resolveErr = uncertainErr
		}
	}
	daemon.retainUnknownGoalLaunchWorkspace(key)
	return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, fmt.Errorf("abort pre-native Goal session: %w", abortErr), fmt.Errorf("read Goal session after compensation: %w", resolveErr)))
}

// settleCancelledResumeBeforeNativeStart releases a retained resume turn when
// the daemon rejected Adapter.Start locally. The predecessor stop certificate
// proves that no new native turn was running.
func (daemon *daemon) settleCancelledResumeBeforeNativeStart(ctx context.Context, key state.RunKey, sessionKey state.GoalSessionKey) error {
	if _, err := daemon.store.MarkGoalSessionResumeNativeStopped(sessionKey); err != nil {
		return err
	}
	daemon.mu.Lock()
	active := daemon.running[key]
	commandID := ""
	if active != nil {
		commandID = active.cancelCommandID
	}
	daemon.mu.Unlock()
	if commandID == "" {
		return nil
	}
	if !daemon.queueCancellationReceipt(ctx, key, commandID) {
		return errors.New("queue cancelled terminal after retained resume pre-start rejection")
	}
	return nil
}

// acknowledgeExpiredNativeStartCancellation preserves a cancellation command
// accepted before the lease watchdog won the native-start race.
func (daemon *daemon) acknowledgeExpiredNativeStartCancellation(ctx context.Context, key state.RunKey) error {
	daemon.mu.Lock()
	active := daemon.running[key]
	commandID := ""
	if active != nil {
		commandID = active.cancelCommandID
	}
	daemon.mu.Unlock()
	return daemon.acknowledgeExpiredNativeStartCancellationCommand(ctx, key, commandID)
}

// settleCancelledNativeStart closes a session returned while cancellation owns
// the start-publication race. A failed close is retried independently, while
// this owner still captures usage and publishes the durable terminal outcome.
func (daemon *daemon) settleCancelledNativeStart(ctx context.Context, key state.RunKey, sessionKey state.GoalSessionKey, localHandleID string, session harness.Session, discardAttach bool, abortFresh bool) (bool, error) {
	daemon.eventReceiptMu.Lock()
	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil || active.nativeSession != session || !active.cancelled || active.cancelCommandID == "" || active.stale || active.nativeStartLeaseExpired || active.terminal || active.terminalizing != 0 {
		daemon.mu.Unlock()
		daemon.eventReceiptMu.Unlock()
		return false, nil
	}
	commandID := active.cancelCommandID
	if active.nativeStartTerminalObserved {
		claimNativeTerminalOwner(active, true)
		active.nativeStartInFlight = false
		active.nativeCancelCommandID = commandID
		active.nativeCancelTerminalObserved = true
		active.nativeCancelRejected = true
		active.nativeUsageRenewalBlocked = true
		active.cleanupBlocked = nativeCleanupBlocked(active)
		renewCancel := active.renewCancel
		active.renewCancel = nil
		active.renewCancelID++
		daemon.mu.Unlock()
		daemon.eventReceiptMu.Unlock()
		if renewCancel != nil {
			renewCancel()
		}
		daemon.waitForNativeRun(daemon.failedStartFinalizationContext(), key, active, session)
		return true, nil
	}
	ownerClaim := claimNativeTerminalOwner(active, false)
	active.nativeStartInFlight = false
	renewCancel := active.renewCancel
	active.renewCancel = nil
	active.renewCancelID++
	daemon.mu.Unlock()
	daemon.eventReceiptMu.Unlock()
	if renewCancel != nil {
		renewCancel()
	}
	if session == nil {
		defer daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
	}
	var discardErr error
	var closeErr error
	if discardAttach {
		if err := daemon.discardUnreadyGoalSessionAttachDelivery(key, localHandleID); err != nil {
			discardErr = fmt.Errorf("discard unready native Goal session attach: %w", err)
		}
	}
	if session == nil {
		if !abortFresh {
			return true, errors.Join(discardErr, errors.New("cancelled native Goal start has no session compensation"))
		}
		if err := daemon.abortGoalSessionLaunchBeforeNativeStart(sessionKey); err != nil {
			abortErr := fmt.Errorf("abort cancelled native Goal launch: %w", err)
			if resolveErr := daemon.resolveCancelledNativeStartAbortFailure(key, sessionKey, abortErr); resolveErr != nil {
				return true, errors.Join(discardErr, resolveErr)
			}
		}
	} else {
		closeErr = daemon.abandonGoalSession(sessionKey, session, true, errors.New("native Goal start was cancelled after publication"))
	}
	if daemon.finishAttachedStart(key) {
		daemon.signalOutbox()
	}
	if session != nil {
		finalContext, cancel := context.WithTimeout(daemon.failedStartFinalizationContext(), controlRequestLimit)
		finalResult, finalWaitErr := session.Wait(finalContext)
		cancel()
		terminalErr := errors.Join(finalWaitErr, closeErr)
		terminalDiagnosticErr := terminalErr
		if terminalErr != nil {
			terminalDiagnosticErr = errors.Join(discardErr, terminalErr)
		}
		daemon.mu.Lock()
		if daemon.running[key] == active {
			active.nativeCancelCommandID = commandID
			active.nativeCancelTerminalObserved = false
			active.nativeCancelRejected = false
			active.nativeCancelTerminateErr = terminalDiagnosticErr
		}
		daemon.mu.Unlock()
		var usageResult *harness.TaskResult
		if finalWaitErr == nil {
			usageResult = &finalResult
		}
		usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, usageResult)
		var usageCopy *protocol.Usage
		if usageErr == nil && shouldQueue {
			daemon.rememberNativeFinalUsage(key, active, usage)
			usageCopy = &usage
		}
		if usageErr == nil {
			usageErr = daemon.queueNativeGoalUsageSettlement(key, active, ownerClaim, finalResult, finalWaitErr, closeErr, usageCopy)
		}
		if usageErr != nil {
			daemon.deferNativeUsageRetry(key, active, ownerClaim, finalResult, finalWaitErr, closeErr, usageCopy, !shouldQueue, usageErr)
			daemon.finishDeferredCommand(commandKey{run: key, id: commandID}, false)
			if terminalErr == nil {
				return true, discardErr
			}
			return true, nil
		}
		if terminalErr != nil || nativeProcessOutputFailure(finalResult.Process) != nil {
			daemon.completeNativeRunAfterUsageOwned(context.WithoutCancel(ctx), key, active, ownerClaim, finalResult, finalWaitErr, closeErr, false)
			latest, loadErr := daemon.store.LoadJournal(key)
			acknowledged := loadErr == nil && hasPendingCommandAcknowledgement(latest, commandID)
			daemon.finishDeferredCommand(commandKey{run: key, id: commandID}, acknowledged)
			return true, nil
		}
	}
	settlementContext := context.WithoutCancel(ctx)
	if !daemon.queueCancellationReceipt(settlementContext, key, commandID) {
		return true, errors.Join(discardErr, errors.New("queue cancelled terminal after native Goal start"))
	}
	daemon.finishDeferredCommand(commandKey{run: key, id: commandID}, true)
	daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
	return true, discardErr
}

func (daemon *daemon) publishNativeCloseRetrySuccess(key state.RunKey, expected *runningRun, session harness.Session) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active != expected || active.nativeSession != session {
		return
	}
	active.nativeSession = nil
	active.goalSession = nil
	active.goalAdmission = nil
	active.nativeDeadline = time.Time{}
	active.nativeCloseRetryRequired = false
	active.nativeCloseRetrying = false
	active.cleanupBlocked = nativeCleanupBlocked(active)
}

func (daemon *daemon) publishNativeCloseRetryAfterEvidence(key state.RunKey, active *runningRun, session harness.Session, goalSession state.GoalSessionKey, reason string, markUncertain bool) error {
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	var uncertaintyErr error
	if markUncertain {
		_, uncertaintyErr = daemon.store.MarkGoalSessionUncertain(goalSession, reason)
	}
	// Publish retry only after all recovery evidence has been attempted. A
	// concurrent final waiter then observes the actual close/accounting flags.
	daemon.requireNativeSessionCloseRetry(key, session)
	return errors.Join(retentionErr, uncertaintyErr)
}

// retainUnknownGoalLaunchWorkspace prevents cleanup from deleting a worktree
// that an unconfirmed native transport may still be modifying.
func (daemon *daemon) retainUnknownGoalLaunchWorkspace(key state.RunKey) {
	if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil && daemon.log != nil {
		daemon.log.Error("retain_unknown_goal_workspace_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
	}
}

func (daemon *daemon) retainUnknownGoalLaunchWorkspaceChecked(key state.RunKey) error {
	daemon.rememberWorkspaceRetention(key)
	retain := daemon.options.retainWorkspace
	if retain == nil {
		retain = daemon.store.RetainWorkspace
	}
	if _, err := retain(key); err != nil {
		return fmt.Errorf("persist workspace retention: %w", err)
	}
	return nil
}

func (daemon *daemon) queueNativeEvent(key state.RunKey, event harness.Event) error {
	at := event.At
	if at.IsZero() {
		at = daemon.now()
	}
	if event.Kind == harness.EventUsageObserved {
		if observation, ok := parseNativeUsageSnapshot(event.Payload); ok {
			observation.ObservedAt = at.UTC()
			journal, observationErr := daemon.store.RecordNativeUsageObservation(key, observation)
			if observationErr != nil {
				observed, loadErr := daemon.store.LoadJournal(key)
				if loadErr != nil || observed.NativeUsageObservation == nil || !observed.NativeUsageObservation.Covers(observation) {
					return errors.Join(observationErr, loadErr)
				}
				journal = observed
			}
			daemon.mu.Lock()
			if active := daemon.running[key]; active != nil && active.goalSession != nil {
				retained := journal.NativeUsageObservation
				if retained != nil {
					retainedCopy := *retained
					active.goalUsageObservation = &retainedCopy
					usage := usageFromNativeObservation(retainedCopy)
					active.goalUsage = &usage
					active.goalUsageAt = retainedCopy.ObservedAt.UTC()
					active.goalCachedInputTokens = retainedCopy.CachedInputTokens
				}
				active.goalUsageHasCached = true
			}
			daemon.mu.Unlock()
		}
	}
	payload := event.Payload
	if len(payload) == 0 || !json.Valid(payload) {
		encoded, err := json.Marshal(map[string]any{
			"native_kind": string(event.Kind), "code": event.Code, "message": event.Message,
			"data": string(event.Data), "diagnostic": event.Diagnostic,
		})
		if err != nil {
			return err
		}
		payload = encoded
	}
	var (
		cloneRun    *linuxCloneRun
		cloneResult *protocol.TaskResult
	)
	if event.Kind == harness.EventTaskResult {
		daemon.mu.Lock()
		if active := daemon.running[key]; active != nil {
			cloneRun = active.linuxClone
		}
		daemon.mu.Unlock()
		if cloneRun != nil {
			result, err := protocol.ParseTaskResult(payload)
			if err != nil {
				return fmt.Errorf("parse Linux clone TaskResult before durable event delivery: %w", err)
			}
			cloneRun.mu.Lock()
			duplicate := cloneRun.taskResult != nil
			cloneRun.mu.Unlock()
			if duplicate {
				return errors.New("Linux clone native session emitted more than one TaskResult")
			}
			cloneResult = &result
		}
	}
	if err := daemon.queueEvent(key, string(event.Kind), payload, at); err != nil {
		return err
	}
	if cloneResult != nil {
		daemon.mu.Lock()
		active := daemon.running[key]
		stillOwned := active != nil && active.linuxClone == cloneRun
		daemon.mu.Unlock()
		if !stillOwned {
			return errors.New("Linux clone run owner changed while persisting its TaskResult")
		}
		cloneRun.mu.Lock()
		defer cloneRun.mu.Unlock()
		if cloneRun.taskResult != nil {
			return errors.New("Linux clone native session emitted more than one persisted TaskResult")
		}
		copy := *cloneResult
		cloneRun.taskResult = &copy
	}
	return nil
}

type nativeUsageSnapshot struct {
	Total *nativeUsageCounters `json:"total"`
}

type nativeUsageCounters struct {
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	InputTokens           *int64 `json:"input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
	TotalTokens           *int64 `json:"total_tokens"`
}

func parseNativeUsageSnapshot(payload json.RawMessage) (state.NativeUsageObservation, bool) {
	if len(payload) == 0 {
		return state.NativeUsageObservation{}, false
	}
	var snapshot nativeUsageSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil ||
		snapshot.Total == nil || snapshot.Total.CachedInputTokens == nil || snapshot.Total.InputTokens == nil ||
		snapshot.Total.OutputTokens == nil || snapshot.Total.ReasoningOutputTokens == nil || snapshot.Total.TotalTokens == nil ||
		*snapshot.Total.CachedInputTokens < 0 || *snapshot.Total.InputTokens < 0 || *snapshot.Total.OutputTokens < 0 ||
		*snapshot.Total.ReasoningOutputTokens < 0 || *snapshot.Total.TotalTokens < 0 {
		return state.NativeUsageObservation{}, false
	}
	return state.NativeUsageObservation{
		SchemaVersion:         state.NativeUsageObservationSchemaVersion,
		InputTokens:           *snapshot.Total.InputTokens,
		OutputTokens:          *snapshot.Total.OutputTokens,
		CachedInputTokens:     *snapshot.Total.CachedInputTokens,
		ReasoningOutputTokens: *snapshot.Total.ReasoningOutputTokens,
		TotalTokens:           *snapshot.Total.TotalTokens,
	}, true
}

func usageFromNativeObservation(observation state.NativeUsageObservation) harness.Usage {
	return harness.Usage{
		State:        harness.UsageUnknown,
		InputTokens:  observation.InputTokens,
		OutputTokens: observation.OutputTokens,
		CostMicrousd: "",
	}
}

const nativeGoalUsageKey = "native-final"

func (daemon *daemon) queueNativeGoalUsage(ctx context.Context, key state.RunKey, result *harness.TaskResult) error {
	usage, shouldQueue, err := daemon.prepareNativeGoalUsage(key, result)
	if err != nil || !shouldQueue {
		return err
	}
	return daemon.queueNativeGoalUsageRecord(key, usage)
}

func (daemon *daemon) prepareNativeGoalUsage(key state.RunKey, result *harness.TaskResult) (protocol.Usage, bool, error) {
	return daemon.prepareNativeGoalUsageWithSnapshot(key, result, true)
}

// prepareNativeUnknownGoalUsage creates the conservative recovery receipt. A
// cumulative usage observation is useful for ordinary finalization, but it is
// not proof of the final total after an unproven native stop.
func (daemon *daemon) prepareNativeUnknownGoalUsage(key state.RunKey) (protocol.Usage, bool, error) {
	return daemon.prepareNativeGoalUsageWithSnapshot(key, nil, false)
}

func (daemon *daemon) prepareNativeGoalUsageWithSnapshot(key state.RunKey, result *harness.TaskResult, allowSnapshot bool) (protocol.Usage, bool, error) {
	if !canonicalGoalRunID(key.RunID) {
		// Legacy/unit fixtures may use descriptive run IDs. Goal transport IDs
		// are UUIDs in production; do not let a compatibility fixture invent a
		// malformed accounting receipt.
		return protocol.Usage{}, false, nil
	}
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return protocol.Usage{}, false, nil
	}
	if err != nil {
		return protocol.Usage{}, false, err
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == state.GoalDeliveryUsage && retired.Delivery.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}
	for _, delivered := range journal.DeliveredGoalDeliveries {
		if delivered.Kind == state.GoalDeliveryUsage && delivered.DeliveryID == nativeGoalUsageKey {
			return protocol.Usage{}, false, nil
		}
	}

	observed := harness.Usage{}
	hasSnapshot := false
	observedAt := daemon.now().UTC()
	cachedInputTokens := int64(0)
	hasCachedInputTokens := false
	if allowSnapshot && journal.NativeUsageObservation != nil {
		observation := *journal.NativeUsageObservation
		observed = usageFromNativeObservation(observation)
		cachedInputTokens = observation.CachedInputTokens
		hasCachedInputTokens = true
		hasSnapshot = true
		observedAt = observation.ObservedAt.UTC()
	}
	daemon.mu.Lock()
	if allowSnapshot && !hasSnapshot {
		if active := daemon.running[key]; active != nil && active.goalUsageObservation != nil {
			observation := *active.goalUsageObservation
			observed = usageFromNativeObservation(observation)
			cachedInputTokens = observation.CachedInputTokens
			hasCachedInputTokens = true
			hasSnapshot = true
			observedAt = observation.ObservedAt.UTC()
		} else if active := daemon.running[key]; active != nil && active.goalUsage != nil {
			observed = *active.goalUsage
			hasSnapshot = true
			cachedInputTokens = active.goalCachedInputTokens
			hasCachedInputTokens = active.goalUsageHasCached
			if !active.goalUsageAt.IsZero() {
				observedAt = active.goalUsageAt.UTC()
			}
		}
	}
	daemon.mu.Unlock()
	if allowSnapshot && result != nil && !hasSnapshot && result.Usage.State != "" {
		observed = result.Usage
		hasSnapshot = result.Usage.State != harness.UsageUnknown
	}

	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	provider := strings.TrimSpace(profile.NativeModelProvider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(profile.NativeModel)
	if model == "" {
		model = "unknown"
	}
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		RunID:         key.RunID,
		UsageKey:      nativeGoalUsageKey,
		Provider:      provider,
		Model:         model,
		CostBasis:     protocol.CostUnknown,
		ObservedAt:    formatControlUTCTimestamp(observedAt),
	}
	if hasSnapshot {
		inputTokens := observed.InputTokens
		outputTokens := observed.OutputTokens
		usage.InputTokens = &inputTokens
		usage.OutputTokens = &outputTokens
		if hasCachedInputTokens {
			usage.CachedInputTokens = &cachedInputTokens
		}
		if (observed.State == harness.UsageReported || observed.State == harness.UsageEstimated) && observed.CostMicrousd != "" && protocol.ValidateMicroUSD(observed.CostMicrousd) == nil {
			cost := observed.CostMicrousd
			usage.CostMicrousd = &cost
			if observed.State == harness.UsageReported {
				usage.CostBasis = protocol.CostReported
			} else {
				usage.CostBasis = protocol.CostEstimated
			}
		}
	}

	if usage.UsageID == "" {
		id, idErr := state.NewDaemonInstanceID()
		if idErr != nil {
			return protocol.Usage{}, false, idErr
		}
		usage.UsageID = id
	}
	if allowSnapshot {
		daemon.mu.Lock()
		if active := daemon.running[key]; active != nil {
			if active.nativeFinalUsage != nil {
				usage = cloneNativeUsage(*active.nativeFinalUsage)
			} else {
				reserved := cloneNativeUsage(usage)
				active.nativeFinalUsage = &reserved
			}
		}
		daemon.mu.Unlock()
	}
	return usage, true, nil
}

// queueNativeUnknownUsageRecovery persists the accounting barrier together
// with an immutable unknown usage body. The optional queue hook is retained for
// focused tests; production uses the state store's atomic mutation.
func (daemon *daemon) queueNativeUnknownUsageRecovery(key state.RunKey) error {
	usage, shouldQueue, err := daemon.prepareNativeUnknownGoalUsage(key)
	if err != nil || !shouldQueue {
		return err
	}
	if queue := daemon.options.queueGoalUsage; queue != nil {
		if _, err := daemon.store.MarkNativeUsageRecoveryRequired(key); err != nil {
			return err
		}
		if _, err := queue(key, usage); err != nil {
			return err
		}
		daemon.signalOutboxFor(key)
		return nil
	}
	if _, err := daemon.store.QueueNativeUsageRecovery(key, usage); err != nil {
		return err
	}
	daemon.signalOutboxFor(key)
	return nil
}

func (daemon *daemon) queueNativeGoalUsageRecord(key state.RunKey, usage protocol.Usage) error {
	queue := daemon.options.queueGoalUsage
	if queue == nil {
		queue = daemon.store.QueueGoalUsage
	}
	if _, err := queue(key, usage); err != nil {
		return err
	}
	daemon.signalOutboxFor(key)
	return nil
}

func (daemon *daemon) queueNativeGoalUsageSettlement(key state.RunKey, active *runningRun, ownerClaim uint64, result harness.TaskResult, waitErr, closeErr error, usage *protocol.Usage) error {
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) {
		daemon.mu.Unlock()
		return errors.New("native terminal owner changed before usage settlement")
	}
	var admission *protocol.Admission
	if active.nativeFinalAdmission != nil {
		copy := *active.nativeFinalAdmission
		admission = &copy
	} else if active.goalAdmission != nil {
		copy := *active.goalAdmission
		admission = &copy
	}
	commandID := active.nativeCancelCommandID
	terminalObserved := active.nativeCancelTerminalObserved
	cancelRejected := active.nativeCancelRejected
	terminateErr := active.nativeCancelTerminateErr
	daemon.mu.Unlock()

	settlement, err := nativeUsageTerminalRecovery(&result, admission, waitErr, closeErr, commandID, terminalObserved, cancelRejected, terminateErr)
	if err != nil {
		return err
	}
	if usage == nil {
		journal, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			return loadErr
		}
		if _, ok := nativeUsageRecoveryUsage(journal); !ok {
			return nil
		}
		_, err = daemon.store.MarkNativeUsageRecoverySettlement(key, settlement)
	} else if queue := daemon.options.queueGoalUsage; queue != nil {
		if _, err = daemon.store.MarkNativeUsageRecoverySettlement(key, settlement); err == nil {
			_, err = queue(key, *usage)
		}
	} else {
		_, err = daemon.store.QueueNativeUsageRecoverySettlement(key, *usage, settlement)
	}
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return err
}

func canonicalGoalRunID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func (daemon *daemon) writeInputBounded(ctx context.Context, active *runningRun, process Process, input []byte) error {
	result := make(chan error, 1)
	go func() {
		result <- process.WriteInput(input)
	}()
	timer := daemon.timer(inputWriteTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.Chan():
		var stopExecution context.CancelCauseFunc
		var key state.RunKey
		daemon.mu.Lock()
		stopExecution = active.stopExecution
		for candidate, current := range daemon.running {
			if current == active {
				key = candidate
				break
			}
		}
		daemon.mu.Unlock()
		if daemon.log != nil {
			daemon.log.Warn("input_write_timeout", "run_id", key.RunID, "generation", key.Generation, "bytes", len(input))
		}
		if stopExecution != nil {
			stopExecution(errInputWriteTimeout)
		}
		return errInputWriteTimeout
	}
}

func (daemon *daemon) recordGenericProcessExit(key state.RunKey, expected *runningRun, process Process, pid int, identity string, stopped bool) bool {
	daemon.mu.Lock()
	if daemon.running[key] != expected || expected.process != process {
		daemon.mu.Unlock()
		return false
	}
	expected.cleanupBlocked = true
	expected.processExited = true
	if expected.processFinalizeOwner != process {
		expected.processFinalizeOwner = process
		expected.processFinalizeAttempts = 0
		expected.processFinalizeExhausted = false
		delete(daemon.cleanupRetry, key)
	}
	if stopped {
		expected.processStopWitness = process
		expected.processStopPID = pid
		expected.processStopIdentity = identity
		expected.processFinalizeAttempts = 0
		expected.processFinalizeExhausted = false
		delete(daemon.cleanupRetry, key)
	} else {
		expected.processStopWitness = nil
		expected.processStopPID = 0
		expected.processStopIdentity = ""
	}
	daemon.mu.Unlock()
	if !stopped {
		daemon.retainUnknownGoalLaunchWorkspace(key)
	}
	return true
}

// watchFailedStartProcess restores the usual exit cleanup when a Process was
// exposed together with a Start error. It is deliberately outside workers: a
// stuck Process must not make daemon shutdown wait forever. Its process marker
// was saved before this watcher is started, so restart recovery retains the
// authority to stop an unresolved child.
func (daemon *daemon) watchFailedStartProcess(ctx, finalizationContext context.Context, key state.RunKey, process Process) {
	completion, ok := process.(processResultCompletion)
	if !ok {
		// A custom Process without a completion witness cannot be waited by the
		// app without creating an unowned goroutine. Its persisted identity stays
		// as the recovery authority.
		return
	}
	resultDone := completion.ResultDone()
	if resultDone == nil {
		return
	}
	finish := func() {
		result := process.Wait()
		daemon.waitForRunResultWithContext(finalizationContext, key, &result)
	}
	select {
	case <-ctx.Done():
		select {
		case <-resultDone:
			finish()
		case <-finalizationContext.Done():
		}
	case <-resultDone:
		finish()
	}
}

func nativeTurnTerminalObserved(err error) bool {
	return err == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded))
}

// waitForNativeRun deliberately treats a native TaskResult, not app-server
// process exit, as terminal execution truth. A successful terminal transition
// settles the bounded Task only; the semantic result remains a proposal and
// cannot accept a Goal outcome by itself.
func (daemon *daemon) waitForNativeRun(ctx context.Context, key state.RunKey, active *runningRun, session harness.Session) {
	staged, isStaged := session.(harness.StagedSession)
	waitContext := ctx
	stopWaitDeadline := func() {}
	daemon.mu.Lock()
	deadline := active.nativeDeadline
	admission := active.goalAdmission
	if admission != nil {
		admissionCopy := *admission
		admission = &admissionCopy
		// A close retry can clear the live session while this waiter is blocked
		// on the final native result. Retain the admitted context for its exact
		// terminal/usage publication rather than rereading mutable active state.
		active.nativeFinalAdmission = &admissionCopy
	}
	prepared := active.prepared
	daemon.mu.Unlock()
	if !deadline.IsZero() {
		waitContext, stopWaitDeadline = context.WithDeadline(ctx, deadline)
	}
	var result harness.TaskResult
	var waitErr error
	ownerClaim := uint64(0)
	if isStaged {
		// A retained app-server stays alive after turn/completed. WaitTurn is the
		// close permission only; it must never expose an early TaskResult.
		waitErr = staged.WaitTurn(waitContext)
	} else {
		// Legacy/non-staged sessions retain the original physical Wait behavior.
		result, waitErr = session.Wait(waitContext)
	}
	if nativeTurnTerminalObserved(waitErr) {
		// Both a valid terminal and a terminal validation failure are observations
		// of this turn. Reserve before Close so late cancellation or lease expiry
		// cannot replace either outcome. If one already owns the race, stop here.
		if claim, owned := daemon.nativeStartTerminalOwnerClaim(key, active); owned {
			ownerClaim = claim
		} else {
			claim, reserved := daemon.reserveNativeObservedTerminalClaim(key, active)
			if !reserved {
				stopWaitDeadline()
				return
			}
			ownerClaim = claim
		}
	}
	stopWaitDeadline()
	if !isStaged && errors.Is(waitErr, context.DeadlineExceeded) {
		// The session remains live because its process context is not the wait
		// deadline. Request native interruption before Close performs the final
		// process stop, so an elapsed admission cannot keep making progress.
		interruptContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, interruptErr := session.Control(interruptContext, harness.ControlRequest{Kind: harness.ControlCancel})
		cancel()
		if interruptErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("interrupt native turn at deadline: %w", interruptErr))
		}
	}
	closeErr := daemon.closeNativeGoalSession(key, active, session)
	if isStaged {
		// Close joins the adapter watcher, but retain a bounded final Wait as a
		// second barrier for deferred interrupt classification and process truth.
		finalContext, cancel := context.WithTimeout(daemon.failedStartFinalizationContext(), controlRequestLimit)
		finalResult, finalWaitErr := session.Wait(finalContext)
		cancel()
		if finalWaitErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("wait native final result: %w", finalWaitErr))
		} else {
			result = finalResult
		}
	}
	if ownerClaim == 0 {
		if claim, owned := daemon.nativeStartTerminalOwnerClaim(key, active); owned {
			ownerClaim = claim
		} else {
			claim, reserved := daemon.reserveNativeTerminalClaim(key, active)
			if !reserved {
				return
			}
			ownerClaim = claim
		}
	}

	if waitErr == nil && closeErr == nil {
		if err := daemon.verifyNativeTaskResultSubject(ctx, key, prepared, admission, &result); err != nil {
			waitErr = err
		}
	}
	usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, &result)
	var usageForSettlement *protocol.Usage
	if shouldQueue {
		daemon.rememberNativeFinalUsage(key, active, usage)
		usageForSettlement = &usage
	}
	if usageErr == nil {
		usageErr = daemon.queueNativeGoalUsageSettlement(key, active, ownerClaim, result, waitErr, closeErr, usageForSettlement)
	}
	if usageErr != nil {
		daemon.deferNativeUsageRetry(key, active, ownerClaim, result, waitErr, closeErr, usageForSettlement, !shouldQueue, usageErr)
		return
	}
	daemon.completeNativeRunAfterUsageOwned(ctx, key, active, ownerClaim, result, waitErr, closeErr, false)
}

func (daemon *daemon) completeNativeRunAfterUsage(ctx context.Context, key state.RunKey, active *runningRun, result harness.TaskResult, waitErr, closeErr error, usageRetryCompletion bool) {
	daemon.mu.Lock()
	ownerClaim := uint64(0)
	if daemon.running[key] == active && active != nil {
		ownerClaim = active.nativeTerminalOwnerClaim
	}
	daemon.mu.Unlock()
	daemon.completeNativeRunAfterUsageOwned(ctx, key, active, ownerClaim, result, waitErr, closeErr, usageRetryCompletion)
}

func (daemon *daemon) completeNativeRunAfterUsageOwned(ctx context.Context, key state.RunKey, active *runningRun, ownerClaim uint64, result harness.TaskResult, waitErr, closeErr error, usageRetryCompletion bool) {
	settlementDone := false
	if usageRetryCompletion {
		defer func() {
			cleanupReady := false
			staleCleanup := false
			retryCurrent := false
			daemon.mu.Lock()
			if daemon.running[key] == active && active.nativeUsageRetryOwnerClaim == ownerClaim && (settlementDone || nativeTerminalOwnedBy(active, ownerClaim)) {
				retryCurrent = true
				active.nativeUsageRetryInFlight = false
				if !settlementDone && !active.nativeUsageFinalized {
					active.nativeUsageRetryPending = true
					active.nativeUsageRetryAt = time.Time{}
				} else {
					active.nativeUsageRetryOwnerClaim = 0
				}
				active.cleanupBlocked = nativeCleanupBlocked(active)
				cleanupReady = settlementDone && !active.cleanupBlocked
				staleCleanup = cleanupReady && active.stale
			}
			daemon.mu.Unlock()
			if !retryCurrent {
				return
			}
			if !settlementDone {
				daemon.signalOutboxFor(key)
			} else if staleCleanup {
				daemon.releaseRun(key)
			} else if cleanupReady {
				daemon.releaseCleanupAfterProcessExit(key)
			}
		}()
	}
	daemon.mu.Lock()
	ownerCurrent := daemon.running[key] == active && active != nil && !active.nativeUsageFinalized && nativeTerminalOwnedBy(active, ownerClaim) &&
		(!usageRetryCompletion || active.nativeUsageRetryOwnerClaim == ownerClaim)
	daemon.mu.Unlock()
	if !ownerCurrent {
		return
	}
	active.inputMu.Lock()
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeUsageFinalized || !nativeTerminalOwnedBy(active, ownerClaim) ||
		(usageRetryCompletion && active.nativeUsageRetryOwnerClaim != ownerClaim) {
		daemon.mu.Unlock()
		active.inputMu.Unlock()
		return
	}
	if !usageRetryCompletion && (active.nativeUsageRetryPending || active.nativeUsageRetryInFlight) {
		// A newer accounting retry owns terminal publication. An older completion
		// must not clear its durable recovery barrier or publish stale close state.
		daemon.mu.Unlock()
		active.inputMu.Unlock()
		return
	}
	admission := active.nativeFinalAdmission
	if admission == nil && active.goalAdmission != nil {
		admissionCopy := *active.goalAdmission
		admission = &admissionCopy
	}
	cancelled := active.cancelled
	stale := active.stale
	nativeTerminalOwner := nativeTerminalOwnedBy(active, ownerClaim)
	nativeCancelCommandID := active.nativeCancelCommandID
	nativeCancelTerminalObserved := active.nativeCancelTerminalObserved
	nativeCancelRejected := active.nativeCancelRejected
	nativeCancelTerminateErr := active.nativeCancelTerminateErr
	active.nativeUsageRetryPending = false
	if !usageRetryCompletion {
		active.nativeUsageRetryInFlight = false
	}
	// The native session has reached its final turn barrier. Keep renewal
	// disabled until releaseRun, even if terminal transition persistence later
	// needs another recovery pass.
	active.nativeUsageRenewalBlocked = true
	active.nativeCancelTerminalObserved = false
	active.nativeCancelRejected = false
	// closeErr is terminal diagnosis from this observation. The current retry
	// and accounting flags, rather than that historical error, own cleanup.
	active.cleanupBlocked = nativeCleanupBlocked(active)
	daemon.mu.Unlock()
	if stale {
		active.inputMu.Unlock()
		_, err := daemon.persistStaleAndEnqueueCleanup(key)
		if err != nil {
			if daemon.log != nil {
				daemon.log.Warn("persist_native_terminal_stale_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
			return
		}
		daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
		daemon.mu.Lock()
		cleanupBlocked := active.cleanupBlocked
		daemon.mu.Unlock()
		if !cleanupBlocked {
			daemon.releaseRun(key)
		}
		settlementDone = true
		return
	}
	if nativeCancelCommandID != "" && nativeCancelTerminalObserved {
		active.inputMu.Unlock()
		terminalQueued := false
		stateName, payload := nativeTerminalPayload(result, admission, waitErr, closeErr)
		outcome := "failed"
		if nativeCancelRejected {
			outcome = "rejected"
		}
		terminalQueued = daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, key, stateName, payload, nativeCancelCommandID, outcome) == nil
		if !terminalQueued {
			if latest, loadErr := daemon.store.LoadJournal(key); loadErr == nil && durableTerminalPresent(latest) {
				if nativeCancelRejected {
					terminalQueued = daemon.replayOrRejectCancellationAcknowledgement(ctx, key, nativeCancelCommandID, latest)
				} else {
					terminalQueued = daemon.queueCommandAcknowledgementWithContext(ctx, key, nativeCancelCommandID, "failed")
				}
			}
		}
		if terminalQueued {
			settlementDone = true
			daemon.mu.Lock()
			if daemon.running[key] == active {
				active.nativeUsageFinalized = true
			}
			daemon.mu.Unlock()
			if err := daemon.clearNativeUsageRecoveryBarrier(key); err != nil && daemon.log != nil {
				daemon.log.Error("clear_native_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
			daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
		}
		daemon.releaseCleanupAfterProcessExit(key)
		return
	}
	if nativeCancelCommandID != "" {
		active.inputMu.Unlock()
		terminalQueued := false
		if nativeProcessOutputFailure(result.Process) != nil {
			stateName, payload := nativeTerminalPayload(result, admission, waitErr, closeErr)
			outcome := "applied"
			if nativeCancelTerminateErr != nil {
				outcome = "failed"
			}
			terminalQueued = daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, key, stateName, payload, nativeCancelCommandID, outcome) == nil
		} else if nativeCancelTerminateErr != nil {
			payload := map[string]string{
				"stage":   "cancel_native",
				"reason":  string(canonicalTaskResultReason(nativeCancelTerminateErr, nil, protocol.TaskResultReasonUnknownOutcome)),
				"summary": "native cancellation failed after durable stop",
				"error":   nativeCancelTerminateErr.Error(),
			}
			terminalQueued = daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, key, "failed", payload, nativeCancelCommandID, "failed") == nil
		} else {
			terminalQueued = daemon.queueCancellationReceipt(ctx, key, nativeCancelCommandID)
		}
		if !terminalQueued {
			if latest, loadErr := daemon.store.LoadJournal(key); loadErr == nil && durableTerminalPresent(latest) {
				terminalQueued = daemon.replayOrRejectCancellationAcknowledgement(ctx, key, nativeCancelCommandID, latest)
			}
		}
		if terminalQueued {
			settlementDone = true
			daemon.mu.Lock()
			if daemon.running[key] == active {
				active.nativeUsageFinalized = true
			}
			daemon.mu.Unlock()
			if err := daemon.clearNativeUsageRecoveryBarrier(key); err != nil && daemon.log != nil {
				daemon.log.Error("clear_native_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
			}
			daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
		}
		daemon.releaseCleanupAfterProcessExit(key)
		return
	}
	terminalQueued := false
	if !cancelled || nativeTerminalOwner {
		stateName, payload := nativeTerminalPayload(result, admission, waitErr, closeErr)
		if err := daemon.queueTerminalTransitionWithRetry(ctx, key, stateName, payload); err != nil && daemon.log != nil {
			daemon.log.Error("queue_native_terminal_transition_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		} else if err == nil {
			terminalQueued = true
		}
	}
	active.inputMu.Unlock()
	if !terminalQueued {
		// A queue conflict can mean another terminal writer won. Read back the
		// durable terminal before retaining this owner forever.
		if latest, loadErr := daemon.store.LoadJournal(key); loadErr == nil && durableTerminalPresent(latest) {
			terminalQueued = true
		}
	}
	if terminalQueued {
		settlementDone = true
		daemon.mu.Lock()
		if daemon.running[key] == active {
			active.nativeUsageFinalized = true
		}
		daemon.mu.Unlock()
		if err := daemon.clearNativeUsageRecoveryBarrier(key); err != nil && daemon.log != nil {
			daemon.log.Error("clear_native_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", err)
		}
		daemon.releaseNativeTerminalOwner(key, active, ownerClaim)
	}
	daemon.releaseCleanupAfterProcessExit(key)
}

type nativeUsageRetryWork struct {
	key          state.RunKey
	active       *runningRun
	ownerClaim   uint64
	result       harness.TaskResult
	waitErr      error
	closeErr     error
	usage        *protocol.Usage
	needsPrepare bool
}

func (daemon *daemon) deferNativeUsageRetry(key state.RunKey, active *runningRun, ownerClaim uint64, result harness.TaskResult, waitErr, closeErr error, usage *protocol.Usage, needsPrepare bool, cause error) {
	now := daemon.now().UTC()
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) || active.nativeUsageFinalized || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight {
		daemon.mu.Unlock()
		return
	}
	finalResult := cloneNativeTaskResult(result)
	active.nativeFinalResult = &finalResult
	active.nativeFinalWaitErr = waitErr
	active.nativeFinalCloseErr = closeErr
	if active.goalAdmission != nil {
		admission := *active.goalAdmission
		active.nativeFinalAdmission = &admission
	}
	if usage != nil {
		finalUsage := cloneNativeUsage(*usage)
		active.nativeFinalUsage = &finalUsage
	}
	active.nativeUsageRetryAttempts = 1
	active.nativeUsageRetryAt = now.Add(minimumInterval)
	active.nativeUsageRetryDeadline = now.Add(nativeUsageRetryWindow)
	active.nativeUsageRetryPending = true
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryOwnerClaim = ownerClaim
	active.nativeUsageRetryNeedsPrepare = needsPrepare
	active.nativeUsageRenewalBlocked = true
	active.nativeUsageRetryExhausted = false
	active.cleanupBlocked = true
	finalAdmission := active.nativeFinalAdmission
	cancelCommandID := active.nativeCancelCommandID
	cancelTerminalObserved := active.nativeCancelTerminalObserved
	cancelRejected := active.nativeCancelRejected
	cancelTerminateErr := active.nativeCancelTerminateErr
	daemon.mu.Unlock()

	var recoveryErr error
	settlement, settlementErr := nativeUsageTerminalRecovery(&finalResult, finalAdmission, waitErr, closeErr, cancelCommandID, cancelTerminalObserved, cancelRejected, cancelTerminateErr)
	if settlementErr != nil {
		recoveryErr = settlementErr
	} else if usage != nil && !needsPrepare {
		if _, err := daemon.store.QueueNativeUsageRecoverySettlement(key, *usage, settlement); err != nil {
			recoveryErr = fmt.Errorf("persist native Goal usage recovery: %w", err)
		} else {
			daemon.signalOutboxFor(key)
		}
	} else if _, err := daemon.store.MarkNativeUsageRecoverySettlement(key, settlement); err != nil {
		recoveryErr = fmt.Errorf("persist native Goal terminal recovery: %w", err)
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	daemon.signalOutboxFor(key)
	if daemon.log != nil {
		daemon.log.Error("queue_native_goal_usage_failed", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, recoveryErr, retentionErr))
	}
}

func (daemon *daemon) flushPendingNativeUsage(ctx context.Context) {
	now := daemon.now().UTC()
	var retries []nativeUsageRetryWork
	var exhausted []nativeUsageRetryWork
	daemon.mu.Lock()
	for key, active := range daemon.running {
		ownerClaim := active.nativeUsageRetryOwnerClaim
		if !active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.nativeFinalResult == nil || !nativeTerminalOwnedBy(active, ownerClaim) {
			continue
		}
		if !active.nativeUsageRetryAt.IsZero() && now.Before(active.nativeUsageRetryAt) {
			continue
		}
		work := nativeUsageRetryWork{
			key:          key,
			active:       active,
			ownerClaim:   ownerClaim,
			result:       cloneNativeTaskResult(*active.nativeFinalResult),
			waitErr:      active.nativeFinalWaitErr,
			closeErr:     active.nativeFinalCloseErr,
			needsPrepare: active.nativeUsageRetryNeedsPrepare,
		}
		if active.nativeFinalUsage != nil {
			usage := cloneNativeUsage(*active.nativeFinalUsage)
			work.usage = &usage
		}
		if active.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!active.nativeUsageRetryDeadline.IsZero() && !now.Before(active.nativeUsageRetryDeadline)) {
			active.nativeUsageRetryPending = false
			active.nativeUsageRetryInFlight = false
			active.nativeUsageRetryExhausted = true
			active.nativeUsageRenewalBlocked = true
			exhausted = append(exhausted, work)
			continue
		}
		active.nativeUsageRetryInFlight = true
		retries = append(retries, work)
	}
	daemon.mu.Unlock()

	for _, work := range exhausted {
		daemon.exhaustNativeUsageRetryOwned(ctx, work.key, work.active, work.ownerClaim, errors.New("native Goal usage persistence retry budget exhausted"))
	}
	for _, work := range retries {
		var usageErr error
		if work.usage != nil && !work.needsPrepare {
			usageErr = daemon.queueNativeGoalUsageSettlement(work.key, work.active, work.ownerClaim, work.result, work.waitErr, work.closeErr, work.usage)
		} else {
			usage, shouldQueue, prepareErr := daemon.prepareNativeGoalUsage(work.key, &work.result)
			usageErr = prepareErr
			if usageErr == nil {
				var usageForSettlement *protocol.Usage
				if shouldQueue {
					daemon.rememberNativeFinalUsage(work.key, work.active, usage)
					usageForSettlement = &usage
				}
				usageErr = daemon.queueNativeGoalUsageSettlement(work.key, work.active, work.ownerClaim, work.result, work.waitErr, work.closeErr, usageForSettlement)
			}
		}
		if usageErr != nil {
			daemon.noteNativeUsageRetryFailure(ctx, work.key, work.active, work.ownerClaim, usageErr)
			continue
		}
		daemon.backgroundWG.Add(1)
		go func(work nativeUsageRetryWork) {
			defer daemon.backgroundWG.Done()
			daemon.completeNativeRunAfterUsageOwned(ctx, work.key, work.active, work.ownerClaim, work.result, work.waitErr, work.closeErr, true)
		}(work)
	}
}

func (daemon *daemon) noteNativeUsageRetryFailure(ctx context.Context, key state.RunKey, active *runningRun, ownerClaim uint64, cause error) {
	now := daemon.now().UTC()
	exhausted := false
	attempt := 0
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) || active.nativeUsageRetryOwnerClaim != ownerClaim || active.nativeUsageFinalized {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryAttempts++
	attempt = active.nativeUsageRetryAttempts
	if active.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!active.nativeUsageRetryDeadline.IsZero() && !now.Before(active.nativeUsageRetryDeadline)) {
		active.nativeUsageRetryPending = false
		active.nativeUsageRetryExhausted = true
		active.nativeUsageRenewalBlocked = true
		exhausted = true
	} else {
		active.nativeUsageRetryPending = true
		active.nativeUsageRetryAt = now.Add(nativeUsageRetryDelay(active.nativeUsageRetryAttempts))
	}
	daemon.mu.Unlock()

	if exhausted {
		daemon.exhaustNativeUsageRetryOwned(ctx, key, active, ownerClaim, cause)
		return
	}
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	if daemon.log != nil {
		daemon.log.Warn("retry_native_goal_usage_failed", "run_id", key.RunID, "generation", key.Generation, "attempt", attempt, "error", errors.Join(cause, retentionErr))
	}
}

func (daemon *daemon) exhaustNativeUsageRetry(ctx context.Context, key state.RunKey, active *runningRun, cause error) {
	daemon.mu.Lock()
	ownerClaim := uint64(0)
	ownerCurrent := daemon.running[key] == active && active != nil && active.nativeTerminalOwner
	if ownerCurrent {
		ownerClaim = active.nativeTerminalOwnerClaim
	}
	daemon.mu.Unlock()
	if !ownerCurrent {
		var reserved bool
		ownerClaim, reserved = daemon.reserveNativeTerminalClaim(key, active)
		if !reserved {
			return
		}
	}
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryOwnerClaim = ownerClaim
	daemon.mu.Unlock()
	daemon.exhaustNativeUsageRetryOwned(ctx, key, active, ownerClaim, cause)
}

func (daemon *daemon) exhaustNativeUsageRetryOwned(ctx context.Context, key state.RunKey, active *runningRun, ownerClaim uint64, cause error) {
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) || active.nativeUsageRetryOwnerClaim != ownerClaim {
		daemon.mu.Unlock()
		return
	}
	active.nativeUsageRetryPending = false
	active.nativeUsageRetryInFlight = false
	active.nativeUsageRetryExhausted = true
	active.nativeUsageRenewalBlocked = true
	active.stale = true
	active.cleanupBlocked = true
	cancelCommandID := active.nativeCancelCommandID
	cancelTerminalObserved := active.nativeCancelTerminalObserved
	cancelRejected := cancelTerminalObserved && active.nativeCancelRejected
	cancelTerminateErr := active.nativeCancelTerminateErr
	var finalResult *harness.TaskResult
	if active.nativeFinalResult != nil {
		copy := cloneNativeTaskResult(*active.nativeFinalResult)
		finalResult = &copy
	}
	finalWaitErr := active.nativeFinalWaitErr
	finalCloseErr := active.nativeFinalCloseErr
	var finalAdmission *protocol.Admission
	if active.nativeFinalAdmission != nil {
		copy := *active.nativeFinalAdmission
		finalAdmission = &copy
	}
	cancelOutcome := "failed"
	if cancelRejected {
		cancelOutcome = "rejected"
	}
	var finalUsage *protocol.Usage
	if active.nativeFinalUsage != nil {
		copy := cloneNativeUsage(*active.nativeFinalUsage)
		finalUsage = &copy
	}
	var goalSession *state.GoalSessionKey
	if active.goalSession != nil {
		copy := *active.goalSession
		goalSession = &copy
	}
	daemon.mu.Unlock()

	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	if retentionErr != nil {
		// A terminal fallback is unsafe until workspace retention is durable.
		daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, retentionErr))
		return
	}

	// A close failure already marks the native session uncertain. If the session
	// is still open for any other reason, make that recovery barrier durable
	// before dropping the in-memory run. A successfully closed session remains
	// closed; the retained journal/usage evidence below is the recovery record.
	var recoveryErr error
	if goalSession != nil {
		sessionJournal, sessionErr := daemon.store.LoadGoalSession(*goalSession)
		if sessionErr == nil {
			runJournal, loadErr := daemon.store.LoadJournal(key)
			if loadErr == nil {
				runJournal, loadErr = daemon.ensureRetainedGoalSessionStopDelivery(key, sessionJournal, runJournal)
			}
			if loadErr == nil {
				sessionJournal, loadErr = daemon.store.LoadGoalSession(*goalSession)
			}
			if loadErr == nil && !retainedGoalSessionTerminalState(sessionJournal, runJournal) && sessionJournal.SessionState != state.GoalSessionStateClosed && !sessionJournal.NeedsReconciliation() {
				_, loadErr = daemon.store.MarkGoalSessionUncertain(*goalSession, "native Goal usage persistence retry budget exhausted")
			}
			recoveryErr = loadErr
		} else if !state.IsNotFound(sessionErr) {
			recoveryErr = sessionErr
		}
	}
	if recoveryErr != nil {
		daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, retentionErr, recoveryErr))
		return
	}

	if finalUsage == nil {
		usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, finalResult)
		if usageErr != nil || !shouldQueue {
			if usageErr == nil {
				usageErr = errors.New("native Goal usage recovery body is unavailable")
			}
			daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, usageErr, retentionErr, recoveryErr))
			return
		}
		finalUsage = &usage
		daemon.rememberNativeFinalUsage(key, active, usage)
	}

	// The accounting barrier and immutable receipt must become durable together.
	// A failed write keeps the stopped native run resident with its exact body so
	// a retry cannot invent a replacement usage identity.
	settlement, settlementErr := nativeUsageTerminalRecovery(finalResult, finalAdmission, finalWaitErr, finalCloseErr, cancelCommandID, cancelTerminalObserved, cancelRejected, cancelTerminateErr)
	if settlementErr != nil {
		daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, retentionErr, recoveryErr, settlementErr))
		return
	}
	if settlement != nil && settlement.CommandID != "" {
		cancelOutcome = settlement.CommandOutcome
	}
	journal, usageEvidenceErr := daemon.store.QueueNativeUsageRecoverySettlement(key, *finalUsage, settlement)
	if usageEvidenceErr != nil {
		daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, retentionErr, recoveryErr, usageEvidenceErr))
		return
	}
	daemon.signalOutboxFor(key)
	var staleErr error
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" {
		_, staleErr = daemon.store.SetLocalState(key, "stale")
	}
	var terminalErr error
	if loaded, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
		if loaded.LocalState == "terminal_pending" || loaded.LocalState == "cleanup_pending" {
			if loaded.TerminalState == "" {
				terminalErr = errors.New("native usage recovery terminal state is missing")
			} else if cancelCommandID != "" && !hasPendingCommandAcknowledgementOutcome(loaded, cancelCommandID, cancelOutcome) && !state.CommandAcknowledgementRetired(loaded) {
				terminalErr = errors.New("native usage recovery terminal is missing its cancellation acknowledgement")
			}
		} else {
			_, terminalErr = daemon.queueNativeUsageRecoveryTerminal(ctx, loaded, cause)
		}
		if terminalErr == nil {
			_, terminalErr = daemon.store.ClearNativeUsageRecoveryRequired(key, *finalUsage)
		}
	} else if !state.IsNotFound(loadErr) {
		terminalErr = loadErr
	}
	if terminalErr != nil {
		daemon.retryNativeUsageRecoveryPersistence(key, active, ownerClaim, errors.Join(cause, retentionErr, staleErr, recoveryErr, terminalErr))
		return
	}
	// All durable recovery markers have been attempted. Do not retain a dead
	// native run in daemon.running: renewal and command admission are already
	// disabled, and restart must not be required to release the slot/memory.
	daemon.releaseRun(key)
	if daemon.log != nil {
		daemon.log.Error("native_goal_usage_retry_exhausted", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, retentionErr, staleErr, recoveryErr, terminalErr))
	}
}

func nativeUsageRetryDelay(attempt int) time.Duration {
	delay := minimumInterval
	for index := 1; index < attempt && delay < retryMaximum; index++ {
		delay = nextRetryDelay(delay)
	}
	return delay
}

func (daemon *daemon) rememberNativeFinalUsage(key state.RunKey, active *runningRun, usage protocol.Usage) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.running[key] != active {
		return
	}
	copy := cloneNativeUsage(usage)
	active.nativeFinalUsage = &copy
	active.nativeUsageRetryNeedsPrepare = false
}

func cloneNativeUsage(usage protocol.Usage) protocol.Usage {
	copy := usage
	if usage.InputTokens != nil {
		value := *usage.InputTokens
		copy.InputTokens = &value
	}
	if usage.OutputTokens != nil {
		value := *usage.OutputTokens
		copy.OutputTokens = &value
	}
	if usage.CachedInputTokens != nil {
		value := *usage.CachedInputTokens
		copy.CachedInputTokens = &value
	}
	if usage.CostMicrousd != nil {
		value := *usage.CostMicrousd
		copy.CostMicrousd = &value
	}
	if usage.PriceVersion != nil {
		value := *usage.PriceVersion
		copy.PriceVersion = &value
	}
	if usage.SupersedesID != nil {
		value := *usage.SupersedesID
		copy.SupersedesID = &value
	}
	return copy
}

func cloneNativeTaskResult(result harness.TaskResult) harness.TaskResult {
	copy := result
	if result.Semantic != nil {
		semantic := *result.Semantic
		semantic.EvidenceRefs = slices.Clone(result.Semantic.EvidenceRefs)
		semantic.Diagnostics = slices.Clone(result.Semantic.Diagnostics)
		if result.Semantic.Blocker != nil {
			blocker := *result.Semantic.Blocker
			blocker.WorkItemIDs = slices.Clone(result.Semantic.Blocker.WorkItemIDs)
			semantic.Blocker = &blocker
		}
		if result.Semantic.ProposedNextAction != nil {
			action := *result.Semantic.ProposedNextAction
			if result.Semantic.ProposedNextAction.Blocker != nil {
				blocker := *result.Semantic.ProposedNextAction.Blocker
				blocker.WorkItemIDs = slices.Clone(result.Semantic.ProposedNextAction.Blocker.WorkItemIDs)
				action.Blocker = &blocker
			}
			semantic.ProposedNextAction = &action
		}
		if result.Semantic.Proposal != nil {
			proposal := append(json.RawMessage(nil), (*result.Semantic.Proposal)...)
			semantic.Proposal = &proposal
		}
		for index := range semantic.Diagnostics {
			if result.Semantic.Diagnostics[index].Severity != nil {
				severity := *result.Semantic.Diagnostics[index].Severity
				semantic.Diagnostics[index].Severity = &severity
			}
		}
		copy.Semantic = &semantic
	}
	if result.Reason != nil {
		reason := *result.Reason
		copy.Reason = &reason
	}
	return copy
}

func (daemon *daemon) clearNativeProcessDetailsExact(key state.RunKey, pid int, identity string) error {
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		return errors.New("native process stop proof has no persistent identity")
	}
	if _, err := daemon.clearPersistedProcessDetails(key, pid, identity); err != nil {
		return fmt.Errorf("clear native process record after close: %w", err)
	}
	return nil
}

func closeNativeSessionWithStopProof(session harness.Session) (int, string, error) {
	if isNilHarnessSession(session) {
		return 0, "", errors.New("native session is unavailable")
	}
	var pid int
	var identity string
	if staged, ok := session.(harness.StagedSession); ok {
		pid, identity = staged.ProcessDetails()
	}
	var identityErr error
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		identityErr = errors.New("native session does not expose a persistent process identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlRequestLimit)
	defer cancel()
	if err := session.Close(ctx); err != nil {
		return pid, identity, errors.Join(identityErr, err)
	}
	var physical execution.Result
	var waitErr error
	if stopped, ok := session.(harness.StopProofSession); ok {
		// A sticky turn/protocol failure remains authoritative for the terminal
		// result, but cannot negate a separately joined physical stop barrier.
		physical, waitErr = stopped.WaitStopped(ctx)
	} else {
		var result harness.TaskResult
		result, waitErr = session.Wait(ctx)
		physical = result.Process
	}
	if waitErr != nil {
		return pid, identity, errors.Join(identityErr, fmt.Errorf("wait for native process stop after close: %w", waitErr))
	}
	if physical.ContainmentError != nil {
		return pid, identity, errors.Join(identityErr, fmt.Errorf("native process containment after close: %w", physical.ContainmentError))
	}
	if physical.TerminationError != nil {
		return pid, identity, errors.Join(identityErr, fmt.Errorf("native process termination after close: %w", physical.TerminationError))
	}
	return pid, identity, identityErr
}

func nativeProcessOutputFailure(result execution.Result) error {
	var failures []error
	if result.OutputTruncated {
		failures = append(failures, errors.New("native process output was truncated"))
	}
	if result.SinkError != nil {
		failures = append(failures, fmt.Errorf("collect native process output: %w", result.SinkError))
	}
	if result.OutputError != nil {
		failures = append(failures, fmt.Errorf("read native process output: %w", result.OutputError))
	}
	return errors.Join(failures...)
}

func nativeUsageTerminalRecovery(result *harness.TaskResult, admission *protocol.Admission, waitErr, closeErr error, commandID string, terminalObserved, cancelRejected bool, terminateErr error) (*state.NativeUsageTerminalRecovery, error) {
	if result == nil {
		return nil, nil
	}
	stateName := ""
	var payload any
	outcome := ""
	switch {
	case commandID == "":
		stateName, payload = nativeTerminalPayload(*result, admission, waitErr, closeErr)
	case terminalObserved:
		stateName, payload = nativeTerminalPayload(*result, admission, waitErr, closeErr)
		outcome = "failed"
		if cancelRejected {
			outcome = "rejected"
		}
	case nativeProcessOutputFailure(result.Process) != nil:
		stateName, payload = nativeTerminalPayload(*result, admission, waitErr, closeErr)
		outcome = "applied"
		if terminateErr != nil {
			outcome = "failed"
		}
	case terminateErr != nil:
		stateName = "failed"
		payload = map[string]string{
			"stage":   "cancel_native",
			"reason":  string(canonicalTaskResultReason(terminateErr, nil, protocol.TaskResultReasonUnknownOutcome)),
			"summary": "native cancellation failed after durable stop",
			"error":   terminateErr.Error(),
		}
		outcome = "failed"
	default:
		stateName = "cancelled"
		payload = map[string]any{}
		outcome = "applied"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &state.NativeUsageTerminalRecovery{State: stateName, Payload: encoded, CommandID: commandID, CommandOutcome: outcome}, nil
}

func typedTaskResultReason(cause error) (protocol.TaskResultReason, bool) {
	if reason, ok := taskResultFailureReason(cause); ok {
		return reason, true
	}
	return "", false
}

// canonicalTaskResultReason defines one precedence order for failed terminal
// payloads. A valid semantic TaskResult records the turn's own outcome and
// therefore outranks physical transport/process failures observed afterward.
func canonicalTaskResultReason(cause error, result *harness.TaskResult, fallback protocol.TaskResultReason) protocol.TaskResultReason {
	if result != nil && result.Semantic != nil && result.Semantic.Kind == protocol.TaskResultFailed && result.Semantic.Reason != nil && result.Semantic.Validate() == nil {
		return *result.Semantic.Reason
	}
	if reason, ok := typedTaskResultReason(cause); ok {
		return reason
	}
	if result != nil && result.Reason != nil {
		return *result.Reason
	}
	return fallback
}

// verifyNativeTaskResultSubject ensures a native model only proposes the
// artifact actually present in its daemon-owned worktree. A candidate result
// may advance from admission Subject A to commit B; all other result kinds
// remain bound to A.
func (daemon *daemon) verifyNativeTaskResultSubject(ctx context.Context, key state.RunKey, prepared workspace.Prepared, admission *protocol.Admission, result *harness.TaskResult) error {
	if admission == nil {
		return errors.New("native Goal admission is unavailable")
	}
	if key.RunID == "" || key.Generation <= 0 {
		return errors.New("native Subject verification requires an exact RunKey")
	}
	if prepared.Run.RunID != "" || prepared.Run.Generation != 0 {
		if prepared.Run.RunID != key.RunID || prepared.Run.Generation != key.Generation {
			if admission.SessionMode != protocol.SessionModeResume || admission.RequestedSessionID == nil {
				return errors.New("native Subject workspace belongs to a different RunKey")
			}
			session, err := daemon.store.LoadGoalSessionByControlSessionID(*admission.RequestedSessionID)
			if err != nil {
				return fmt.Errorf("load retained native Subject workspace owner: %w", err)
			}
			// Resume rotates execution authority, never the worktree's original owner.
			if session.SessionMode != state.GoalSessionModeResume || session.HarnessKind != string(daemon.harnessCapabilities.Kind) ||
				(daemon.harnessCapabilities.Kind != harness.KindPi && daemon.harnessCapabilities.Kind != harness.KindOMP && daemon.harnessCapabilities.Kind != harness.KindOmO) ||
				session.ResumeSourceStopCertificate == nil || !session.HasVerifiedControlAttachment() || session.NeedsReconciliation() ||
				session.RunID != key.RunID || session.Generation != key.Generation || session.AdmissionID != admission.AdmissionID ||
				session.GoalID != admission.GoalID || session.GoalRevision != admission.GoalRevision ||
				session.WorkItemID != admissionWorkItemIDValue(admission.WorkItemID) || session.RepositoryResourceID != admission.Subject.ResourceID ||
				session.MachineID != daemon.machineID || session.RuntimeID != daemon.runtimeID || session.RuntimeEpoch != daemon.runtimeEpoch ||
				session.WorkspaceOwnerRunKey.RunID != prepared.Run.RunID || session.WorkspaceOwnerRunKey.Generation != prepared.Run.Generation ||
				session.WorkspacePath != prepared.Path || prepared.BindingKey != daemon.config.Runtime.Workspace {
				return errors.New("native Subject workspace does not match the exact retained resume owner")
			}
			fingerprint, err := workspaceFingerprint(ctx, prepared)
			if err != nil {
				return fmt.Errorf("fingerprint retained native Subject workspace: %w", err)
			}
			if fingerprint != session.WorkspaceFingerprint {
				return errors.New("native Subject workspace fingerprint differs from its retained resume owner")
			}
		}
	}
	if result == nil || result.Semantic == nil {
		return nil
	}
	if err := result.Semantic.Validate(); err != nil {
		return fmt.Errorf("validate native task_result: %w", err)
	}
	if err := protocol.ValidateTaskResultForAdmission(*admission, *result.Semantic); err != nil {
		return fmt.Errorf("native task_result is incompatible with admission: %w", err)
	}
	if journal, err := daemon.store.LoadJournal(key); err != nil {
		return fmt.Errorf("load immutable Subject evidence before terminal validation: %w", err)
	} else if journal.LinuxCloneIntent != nil {
		return verifyLinuxCloneTaskResultSubject(journal, key, admission, *result.Semantic)
	} else if journal.LinuxCloneReuseGrant != nil {
		return verifyLinuxCloneReuseTaskResultSubject(journal, key, admission, *result.Semantic)
	}
	subjectWorkspace, ok := daemon.workspace.(goalSubjectWorkspace)
	if !ok {
		return errors.New("workspace service does not support immutable Goal subjects")
	}
	actual, err := subjectWorkspace.DeriveSubject(ctx, prepared, admission.Subject.ResourceID)
	if err != nil {
		return fmt.Errorf("derive native worktree Subject: %w", err)
	}
	if result.Semantic.Subject != actual {
		return errors.New("native task_result Subject does not match the daemon-derived worktree Subject")
	}
	if result.Semantic.Kind != protocol.TaskResultCandidateCompletion && actual != admission.Subject {
		return errors.New("non-candidate native task_result advanced beyond the admitted Subject")
	}
	return nil
}

func verifyLinuxCloneReuseTaskResultSubject(journal state.RunJournal, key state.RunKey, admission *protocol.Admission, result protocol.TaskResult) error {
	grant := journal.LinuxCloneReuseGrant
	if grant == nil || grant.Validate() != nil || grant.RunID != key.RunID || grant.Generation != key.Generation ||
		grant.Source.Intent.ResourceID != admission.Subject.ResourceID ||
		grant.Source.Intent.BaseCommit != admission.Subject.Commit ||
		grant.Source.Intent.BaseTreeDigest != admission.Subject.TreeDigest {
		return errors.New("Linux clone reuse grant differs from the immutable admitted Subject")
	}
	if result.Kind == protocol.TaskResultCandidateCompletion {
		return errors.New("Linux clone reuse candidate promotion is not implemented")
	}
	if result.Subject != admission.Subject {
		return errors.New("non-candidate Linux clone reuse TaskResult differs from its admitted Subject")
	}
	return nil
}

func verifyLinuxCloneTaskResultSubject(journal state.RunJournal, key state.RunKey, admission *protocol.Admission, result protocol.TaskResult) error {
	intent := journal.LinuxCloneIntent
	if intent == nil || intent.Validate() != nil || intent.RunID != key.RunID || intent.Generation != key.Generation ||
		intent.ResourceID != admission.Subject.ResourceID || intent.BaseCommit != admission.Subject.Commit ||
		intent.BaseTreeDigest != admission.Subject.TreeDigest {
		return errors.New("Linux clone intent differs from the immutable admitted Subject")
	}
	if result.Kind != protocol.TaskResultCandidateCompletion {
		if result.Subject != admission.Subject {
			return errors.New("non-candidate Linux clone TaskResult differs from its admitted Subject")
		}
		return nil
	}
	promotion := journal.LinuxClonePromotionReceipt
	if promotion == nil || promotion.Validate() != nil {
		return errors.New("candidate Linux clone TaskResult lacks an A-validated promotion receipt")
	}
	expectedRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		return err
	}
	if promotion.Ref != expectedRef || result.Subject.ResourceID != intent.ResourceID ||
		result.Subject.Commit != promotion.Commit || result.Subject.TreeDigest != promotion.TreeDigest {
		return errors.New("candidate Linux clone TaskResult differs from the A-promoted commit/tree Subject")
	}
	return nil
}

func (daemon *daemon) runningRun(key state.RunKey) *runningRun {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.running[key]
}

func (daemon *daemon) liveNativeUsageSettlementOwns(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	if active == nil || (!active.nativeUsageRetryPending && !active.nativeUsageRetryInFlight && !active.nativeUsageRetryExhausted) {
		return false
	}
	return nativeTerminalOwnedBy(active, active.nativeUsageRetryOwnerClaim)
}

func (daemon *daemon) stopAll() {
	daemon.mu.Lock()
	processes := make([]Process, 0, len(daemon.running))
	for _, run := range daemon.running {
		run.cancelled = true
		if run.cancel != nil {
			run.cancel()
		}
		if run.process != nil {
			processes = append(processes, run.process)
		}
	}
	daemon.mu.Unlock()
	// Failed Ready-barrier cleanup has no worker waiting on the native session.
	// Give it one bounded in-process retry before the store becomes unavailable.
	daemon.retryBlockedNativeSessionCloses()
	for _, process := range processes {
		daemon.terminateProcessBounded(process, 0)
	}
}

// terminateProcessBounded makes shutdown independent of a Process
// implementation that fails to honour its cancellation context. The runner
// still owns physical reaping; the persisted marker remains for recovery when
// that cannot be confirmed during this daemon lifetime.
func (daemon *daemon) terminateProcessBounded(process Process, grace time.Duration) {
	if process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTerminateLimit)
	defer cancel()
	done := make(chan struct{}, 1)
	go func() {
		_ = process.Terminate(ctx, grace)
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
