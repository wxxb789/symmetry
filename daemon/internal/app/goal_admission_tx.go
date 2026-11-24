package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omo"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omp"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

type goalAdmissionCompensation struct {
	armed    bool
	released bool
	run      func(context.Context, error) error
}

type goalAdmissionTx struct {
	daemon             *daemon
	key                state.RunKey
	claim              protocol.ClaimResponse
	admission          protocol.Admission
	workspacePersisted func(workspace.Prepared)

	bindingID          string
	configuredKind     harness.Kind
	profile            config.AgentProfile
	providerAccess     *protocol.ProviderAccess
	linuxV2Required    bool
	linuxV2Owner       linuxV2NativeOwner
	linuxV2Environment []string
	goalAPI            goalControlAPI
	adapter            harness.Adapter
	environment        []string
	deadline           time.Time

	prepared      workspace.Prepared
	fingerprint   string
	localHandleID string
	sessionKey    state.GoalSessionKey
	resume        *harness.ResumeHandle
	attachQueued  bool
	attachReady   bool
	sink          harness.EventSink

	initialLeaseDeadlineAt time.Time
	initialLeaseDeadline   time.Duration
	nativeInvocation       execution.Invocation

	session          harness.Session
	staged           harness.StagedSession
	handle           harness.NativeSessionHandle
	operationContext context.Context
	operationCancel  context.CancelFunc
	attached         bool

	runJournal state.RunJournal
	receipt    *control.GoalSessionReceipt
	active     *runningRun
	turnErr    error

	linuxV2Fence              protocol.Fence
	linuxClone                *linuxCloneRun
	linuxCloneReuse           *authority.LinuxCloneReuseGrant
	releaseLinuxV2LaunchPhase func()
	providerBridge            *harness.ProviderBridgeLaunch

	bridgeCompensation        goalAdmissionCompensation
	cloneCompensation         goalAdmissionCompensation
	reuseCompensation         goalAdmissionCompensation
	nativeOutcomeCompensation goalAdmissionCompensation
	launchPhaseCompensation   goalAdmissionCompensation
}

func newGoalAdmissionTx(daemon *daemon, key state.RunKey, claim protocol.ClaimResponse, admission protocol.Admission, workspacePersisted func(workspace.Prepared)) *goalAdmissionTx {
	tx := &goalAdmissionTx{
		daemon:             daemon,
		key:                key,
		claim:              claim,
		admission:          admission,
		workspacePersisted: workspacePersisted,
	}
	tx.bridgeCompensation.run = tx.closeUnownedProviderBridge
	tx.cloneCompensation.run = tx.abortPreCheckoutLinuxClone
	tx.reuseCompensation.run = tx.abortPreNativeLinuxCloneReuse
	tx.nativeOutcomeCompensation.run = tx.typeNativeStartOutcome
	tx.launchPhaseCompensation.run = tx.releaseNativeLaunchPhase
	return tx
}

func (tx *goalAdmissionTx) abort(ctx context.Context, returnErr error) (aborted error) {
	aborted = returnErr
	// Deferred in reverse registration order so entries run bridge, clone, reuse, outcome,
	// launch-phase: one entry's panic cannot skip the remaining compensations.
	defer deferredCompensation(&tx.launchPhaseCompensation, ctx, &aborted)()
	defer deferredCompensation(&tx.nativeOutcomeCompensation, ctx, &aborted)()
	defer deferredCompensation(&tx.reuseCompensation, ctx, &aborted)()
	defer deferredCompensation(&tx.cloneCompensation, ctx, &aborted)()
	defer deferredCompensation(&tx.bridgeCompensation, ctx, &aborted)()
	return aborted
}

func deferredCompensation(entry *goalAdmissionCompensation, ctx context.Context, aborted *error) func() {
	return func() {
		if !entry.armed || entry.released {
			return
		}
		*aborted = entry.run(ctx, *aborted)
	}
}

func (tx *goalAdmissionTx) run(ctx context.Context) error {
	if settled, err := tx.validateGoalAdmissionInputs(ctx); err != nil || settled {
		return err
	}
	if settled, err := tx.prepareGoalSessionForLaunch(ctx); err != nil || settled {
		return err
	}
	if settled, err := tx.startGoalNativeSession(ctx); err != nil || settled {
		return err
	}
	if settled, err := tx.attachGoalNativeSession(ctx); err != nil || settled {
		return err
	}
	defer tx.operationCancel()
	if settled, err := tx.openGoalNativeSession(ctx); err != nil || settled {
		return err
	}
	// Holds start ownership through the returned StartTurn result: cancellation or lease expiry
	// may cancel operationContext meanwhile, but cannot close or settle before this caller sees it.
	defer tx.daemon.finishNativeStartPublication(tx.key)
	return tx.openGoalTurnAndSettleTerminalOwnership(ctx)
}

func (tx *goalAdmissionTx) closeUnownedProviderBridge(ctx context.Context, returnErr error) error {
	closeContext, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), piProviderBridgeCloseTimeout)
	closeErr := tx.providerBridge.Lifecycle.Close(closeContext)
	closeCancel()
	if closeErr != nil {
		returnErr = errors.Join(returnErr, fmt.Errorf("close unowned pi provider bridge: %w", closeErr))
	}
	return returnErr
}

func (tx *goalAdmissionTx) abortPreCheckoutLinuxClone(ctx context.Context, returnErr error) error {
	abortParent := context.Background()
	if ctx != nil {
		abortParent = context.WithoutCancel(ctx)
	}
	cleanupContext, cancel := tx.daemon.cleanupContext(abortParent)
	abortErr := tx.daemon.abortLinuxV2CloneBeforeCheckout(cleanupContext, tx.key, tx.linuxV2Fence, tx.linuxClone.grant.Intent)
	cancel()
	if abortErr != nil {
		returnErr = errors.Join(returnErr, fmt.Errorf("abort pre-checkout Linux clone after native-start refusal: %w", abortErr))
	}
	return returnErr
}

func (tx *goalAdmissionTx) abortPreNativeLinuxCloneReuse(ctx context.Context, returnErr error) error {
	abortParent := context.Background()
	if ctx != nil {
		abortParent = context.WithoutCancel(ctx)
	}
	cleanupContext, cancel := tx.daemon.cleanupContext(abortParent)
	abortErr := tx.daemon.abortLinuxCloneReuseBeforeNativeStart(cleanupContext, tx.key, tx.linuxV2Fence, *tx.linuxCloneReuse)
	cancel()
	if abortErr != nil {
		returnErr = errors.Join(returnErr, fmt.Errorf("retire pre-native Linux clone reuse after launch refusal: %w", abortErr))
	}
	return returnErr
}

func (tx *goalAdmissionTx) typeNativeStartOutcome(_ context.Context, returnErr error) error {
	if returnErr == nil {
		return nil
	}
	if _, typed := taskResultFailureReason(returnErr); !typed {
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, returnErr)
	}
	return returnErr
}

func (tx *goalAdmissionTx) releaseNativeLaunchPhase(_ context.Context, returnErr error) error {
	if tx.releaseLinuxV2LaunchPhase != nil {
		tx.releaseLinuxV2LaunchPhase()
	}
	return returnErr
}

func (tx *goalAdmissionTx) validateGoalAdmissionInputs(ctx context.Context) (bool, error) {
	if handled, err := tx.validateAdmissionEnvelope(ctx); err != nil || handled {
		return handled, err
	}
	if err := tx.validateConfiguredHarness(); err != nil {
		return false, err
	}
	if err := tx.validateAdapterInvocationArgs(); err != nil {
		return false, err
	}
	if err := tx.acquireNativeLaunchPhase(ctx); err != nil {
		return false, err
	}
	if err := tx.resolveGoalAdapter(ctx); err != nil {
		return false, err
	}
	if err := tx.buildNativeEnvironment(); err != nil {
		return false, err
	}
	if err := tx.prepareProviderBridge(ctx); err != nil {
		return false, err
	}
	return false, nil
}

func (tx *goalAdmissionTx) validateAdmissionEnvelope(ctx context.Context) (bool, error) {
	if err := tx.daemon.validateClaimLeaseForStart(tx.claim, tx.claim.RequestStartedAt, "native process"); err != nil {
		return false, err
	}
	if err := tx.admission.Validate(); err != nil {
		return false, fmt.Errorf("validate Goal admission: %w", err)
	}
	if tx.admission.ModelProfile != tx.daemon.config.Runtime.AgentProfile {
		return false, fmt.Errorf("Goal admission model_profile %q does not match configured runtime agent_profile %q", tx.admission.ModelProfile, tx.daemon.config.Runtime.AgentProfile)
	}
	if strings.TrimSpace(tx.daemon.config.Runtime.RepositoryResourceID) == "" {
		return false, errors.New("Goal admission requires a configured runtime repository_resource_id")
	}
	if tx.admission.Subject.ResourceID != tx.daemon.config.Runtime.RepositoryResourceID {
		return false, errors.New("Goal admission subject resource_id does not match the configured runtime repository_resource_id")
	}
	bindingID, err := goalSessionBindingID(tx.admission, tx.claim)
	if err != nil {
		return false, taskResultFailure(protocol.TaskResultReasonResumeRejected, err)
	}
	tx.bindingID = bindingID
	if tx.admission.Purpose == protocol.AdmissionPurposeValidate {
		deterministic, err := tx.daemon.tryDeterministicArtifactValidation(ctx, tx.key, tx.claim, tx.admission)
		if err != nil {
			// Control derives validation_failed from persisted failed validator
			// evidence; a local read/integrity failure has no such receipt.
			return false, err
		}
		if deterministic.Handled {
			tx.daemon.finishStarting(tx.key, true)
			tx.daemon.scheduleTerminalSlotRelease(tx.key, deterministic.Journal.TerminalPendingAt)
			tx.daemon.signalOutbox()
			return true, nil
		}
	}
	return false, nil
}

func (tx *goalAdmissionTx) validateConfiguredHarness() error {
	if err := admissionLaunchFailure(tx.admission, tx.daemon.harnessCapabilities, tx.claim.ProviderAccess); err != nil {
		return err
	}
	configuredKind, err := configuredHarnessKind(tx.daemon.config.Runtime.HarnessKind)
	if err != nil {
		return err
	}
	if configuredKind != tx.daemon.harnessCapabilities.Kind {
		return fmt.Errorf("configured Goal runtime harness %q does not match capability kind %q", configuredKind, tx.daemon.harnessCapabilities.Kind)
	}
	if !configuredKind.NativeGoal() {
		return fmt.Errorf("Goal admission harness %q is not a native staged adapter", configuredKind)
	}
	tx.configuredKind = configuredKind
	profile := tx.daemon.config.AgentProfiles[tx.daemon.config.Runtime.AgentProfile]
	tx.profile = profile
	providerAccess, err := goalProviderAccess(profile, tx.claim.ProviderAccess)
	if err != nil {
		return err
	}
	tx.providerAccess = providerAccess
	tx.linuxV2Required = requiresLinuxV2NativeOwner(configuredKind)
	return nil
}

func (tx *goalAdmissionTx) validateAdapterInvocationArgs() error {
	if tx.configuredKind == harness.KindPi {
		if err := pi.ValidateRPCProfileArgs(tx.profile.Args); err != nil {
			return fmt.Errorf("validate pi RPC invocation before native launch: %w", err)
		}
		if tx.providerAccess != nil {
			if err := pi.ValidateProviderBridgeProfileArgs(tx.profile.Args); err != nil {
				return fmt.Errorf("validate pi provider bridge invocation before native launch: %w", err)
			}
		}
	}
	if tx.configuredKind == harness.KindOMP {
		if err := omp.ValidateRPCProfileArgs(tx.profile.Args); err != nil {
			return fmt.Errorf("validate OMP RPC invocation before native launch: %w", err)
		}
	}
	if tx.configuredKind == harness.KindOmO {
		if err := omo.ValidateRPCProfileArgs(tx.profile.Args); err != nil {
			return fmt.Errorf("validate OmO RPC invocation before native launch: %w", err)
		}
	}
	return nil
}

func (tx *goalAdmissionTx) acquireNativeLaunchPhase(ctx context.Context) error {
	if !tx.linuxV2Required {
		return nil
	}
	if tx.claim.RunID != tx.key.RunID || tx.claim.Generation != tx.key.Generation || tx.claim.ClaimID == "" || tx.claim.LeaseToken == "" {
		return errors.New("Linux v2 native admission claim does not match the exact RunKey fence")
	}
	tx.linuxV2Fence = protocol.Fence{
		RuntimeID: tx.daemon.runtimeID, RuntimeEpoch: tx.daemon.runtimeEpoch, Generation: tx.key.Generation,
		ClaimID: tx.claim.ClaimID, LeaseToken: tx.claim.LeaseToken,
	}
	releasePhase, err := tx.daemon.acquireLinuxV2LaunchPhase(ctx, tx.key, tx.claim)
	tx.releaseLinuxV2LaunchPhase = releasePhase
	if tx.releaseLinuxV2LaunchPhase != nil {
		tx.launchPhaseCompensation.armed = true
	}
	if err != nil {
		return fmt.Errorf("wait for Linux v2 native launch phase: %w", err)
	}
	if err := tx.daemon.checkLinuxV2NativeAdmission(ctx, tx.key, tx.linuxV2Fence); err != nil {
		return fmt.Errorf("recheck exact Linux v2 owner after launch-phase wait: %w", err)
	}
	owner, environment, err := tx.daemon.prepareLinuxV2NativeOwner(ctx, tx.key, tx.linuxV2Fence, tx.configuredKind, tx.profile, tx.providerAccess != nil)
	if err != nil {
		return err
	}
	tx.linuxV2Owner = owner
	tx.linuxV2Environment = environment
	return nil
}

func (tx *goalAdmissionTx) resolveGoalAdapter(ctx context.Context) error {
	goalAPI, ok := tx.daemon.control.(goalControlAPI)
	if !ok {
		return errors.New("control API does not implement Goal session admission")
	}
	tx.goalAPI = goalAPI
	if tx.daemon.harnessRegistry == nil {
		return errors.New("native harness registry is unavailable")
	}
	deadline := parseAdmissionDeadline(tx.admission.Limits.DeadlineAt)
	if deadline.IsZero() || !deadline.After(tx.daemon.now()) {
		return errors.New("Goal admission deadline has elapsed before native launch")
	}
	tx.deadline = deadline
	adapter, err := tx.daemon.harnessRegistry.Lookup(tx.configuredKind)
	if err != nil {
		return fmt.Errorf("lookup configured Goal adapter %q: %w", tx.configuredKind, err)
	}
	tx.adapter = adapter
	probedCapabilities, err := tx.daemon.harnessRegistry.Probe(ctx, tx.configuredKind)
	if err != nil {
		return fmt.Errorf("probe configured Goal adapter %q before native launch: %w", tx.configuredKind, err)
	}
	if !reflect.DeepEqual(probedCapabilities, tx.daemon.harnessCapabilities) {
		return fmt.Errorf("configured Goal adapter %q capability projection changed since runtime registration", tx.configuredKind)
	}
	return nil
}

func (tx *goalAdmissionTx) buildNativeEnvironment() error {
	environment, err := execution.BuildEnvironment(tx.profile.EnvAllowlist...)
	if err != nil {
		return fmt.Errorf("build native environment: %w", err)
	}
	if tx.linuxV2Required {
		environment, err = mergeEnvironmentOverrides(environment, tx.linuxV2Environment)
		if err != nil {
			return fmt.Errorf("build fixed-B Linux native environment: %w", err)
		}
	}
	tx.environment = environment
	return nil
}

func (tx *goalAdmissionTx) prepareProviderBridge(ctx context.Context) error {
	providerBridge, err := tx.daemon.preparePiProviderBridge(ctx, tx.key, tx.configuredKind, tx.providerAccess)
	if err != nil {
		return err
	}
	tx.providerBridge = providerBridge
	if tx.providerBridge != nil {
		tx.bridgeCompensation.armed = true
	}
	return nil
}

func (tx *goalAdmissionTx) prepareGoalSessionForLaunch(ctx context.Context) (bool, error) {
	if tx.admission.SessionMode == protocol.SessionModeResume {
		if err := tx.prepareRetainedGoalSessionForLaunch(ctx); err != nil {
			return false, err
		}
	} else {
		if settled, err := tx.prepareFreshGoalSessionForLaunch(ctx); err != nil || settled {
			return settled, err
		}
	}
	if tx.workspacePersisted != nil {
		tx.workspacePersisted(tx.prepared)
	}
	if err := tx.queueGoalSessionAttach(ctx); err != nil {
		return false, err
	}
	if cancelled, err := tx.abortFreshLaunchIfCancelled(); cancelled {
		return true, err
	}
	return false, nil
}

func (tx *goalAdmissionTx) abortFreshLaunchIfCancelled() (bool, error) {
	if tx.resume != nil || !tx.daemon.isCancelled(tx.key) {
		return false, nil
	}
	var discardErr error
	if tx.attachQueued {
		discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
	}
	abortErr := tx.daemon.abortGoalSessionLaunchBeforeNativeStart(tx.sessionKey)
	if err := errors.Join(discardErr, abortErr); err != nil {
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("compensate cancelled native Goal launch: %w", err))
	}
	return true, nil
}

func (tx *goalAdmissionTx) prepareRetainedGoalSessionForLaunch(ctx context.Context) error {
	resumed, err := tx.recoverRetainedGoalSession(ctx)
	if err != nil {
		return err
	}
	if err := tx.markRetainedWorkspaceRecovery(ctx); err != nil {
		return err
	}
	if err := tx.queueRetainedSessionAttach(ctx); err != nil {
		return err
	}
	return tx.rebindRetainedGoalSession(ctx, resumed)
}

func (tx *goalAdmissionTx) recoverRetainedGoalSession(ctx context.Context) (resumedGoalSession, error) {
	resumed, resumeErr := tx.daemon.recoverNativeGoalSessionForResume(ctx, tx.key, tx.claim, tx.admission, tx.linuxV2Required)
	if resumeErr != nil {
		return resumedGoalSession{}, taskResultFailure(protocol.TaskResultReasonResumeRejected, resumeErr)
	}
	if resumed.reuse != nil {
		if !tx.linuxV2Required {
			return resumedGoalSession{}, taskResultFailure(protocol.TaskResultReasonResumeRejected, errors.New("Linux clone reuse returned for a non-Linux-v2 admission"))
		}
		if err := tx.daemon.bindLinuxCloneReuse(ctx, tx.key, tx.linuxV2Fence, *resumed.reuse); err != nil {
			return resumedGoalSession{}, taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("bind retained Linux clone reuse: %w", err))
		}
		tx.linuxCloneReuse = resumed.reuse
		if tx.linuxCloneReuse != nil {
			tx.reuseCompensation.armed = true
		}
	}
	tx.prepared = resumed.prepared
	tx.fingerprint = resumed.fingerprint
	tx.localHandleID = resumed.journal.LocalHandleID
	tx.sessionKey = resumed.journal.Key()
	resume := resumed.resume
	tx.resume = &resume
	return resumed, nil
}

func (tx *goalAdmissionTx) markRetainedWorkspaceRecovery(ctx context.Context) error {
	// The new Run refers to the retained workspace but never owns its
	// deletion. Persist that fact before consuming the predecessor receipt.
	if _, err := tx.daemon.store.MarkWorkspaceRecoveryRequired(tx.key); err != nil {
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("mark retained workspace recovery required: %w", err))
	}
	if !tx.daemon.persistWorkspacePath(ctx, tx.key, tx.prepared.Path) {
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, errors.New("persist retained workspace path"))
	}
	if err := tx.daemon.retainUnknownGoalLaunchWorkspaceChecked(tx.key); err != nil {
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, err)
	}
	return nil
}

func (tx *goalAdmissionTx) queueRetainedSessionAttach(_ context.Context) error {
	// Persist the unready target attachment before consuming the available certificate: a
	// failed write keeps the predecessor available, and unready-delivery recovery the rest.
	if _, err := tx.daemon.store.QueueGoalSessionAttach(tx.key, state.GoalSessionAttachDelivery{
		GoalID: tx.admission.GoalID, LocalHandleID: tx.localHandleID, BindingID: tx.bindingID,
		ServerIssuedBinding: false,
		HarnessKind:         string(tx.daemon.harnessCapabilities.Kind), HarnessVersion: tx.daemon.harnessCapabilities.NativeVersion,
		AdapterVersion: tx.daemon.harnessCapabilities.ImplementationVersion, WorkspaceFingerprint: tx.fingerprint,
		Workspace: tx.daemon.config.Runtime.Workspace, RepositoryResourceID: &tx.admission.Subject.ResourceID,
	}); err != nil {
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("queue retained native session attach intent: %w", err))
	}
	tx.attachQueued = true
	return nil
}

func (tx *goalAdmissionTx) rebindRetainedGoalSession(_ context.Context, resumed resumedGoalSession) error {
	rebound, rebindErr := tx.daemon.store.RebindGoalSessionForResume(tx.sessionKey, state.GoalSessionResumeRebind{
		RunID: tx.key.RunID, Generation: tx.key.Generation, TaskID: tx.claim.TaskID, AdmissionID: tx.admission.AdmissionID, BindingID: tx.bindingID,
		ExactStopCertificate: *resumed.journal.StopCertificate,
		Compatibility: state.GoalSessionCompatibility{
			MachineID: tx.daemon.machineID, RuntimeID: tx.daemon.runtimeID, RuntimeEpoch: tx.daemon.runtimeEpoch,
			HarnessKind: string(tx.daemon.harnessCapabilities.Kind), HarnessVersion: tx.daemon.harnessCapabilities.NativeVersion,
			AdapterVersion: tx.daemon.harnessCapabilities.ImplementationVersion, AdapterProtocolVersion: tx.daemon.harnessCapabilities.ProtocolVersion,
			WorkspaceFingerprint: tx.fingerprint, RepositoryResourceID: tx.admission.Subject.ResourceID,
		},
	})
	if rebindErr != nil {
		observed, observeErr := tx.daemon.store.LoadGoalSession(tx.sessionKey)
		if observeErr == nil && observed.SessionMode == state.GoalSessionModeResume && observed.RunID == tx.key.RunID &&
			observed.Generation == tx.key.Generation && observed.BindingID == tx.bindingID && observed.LocalHandleID == tx.localHandleID {
			tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
			return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("resume rebind outcome is unknown: %w", rebindErr))
		}
		if observeErr == nil && observed.SessionState == state.GoalSessionStateAvailable && observed.StopCertificate != nil &&
			observed.RunID == resumed.journal.RunID && observed.Generation == resumed.journal.Generation && observed.BindingID == resumed.journal.BindingID {
			if discardErr := tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID); discardErr != nil {
				return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(rebindErr, discardErr))
			}
			return taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("consume retained native session: %w", rebindErr))
		}
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		if observeErr != nil {
			return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(rebindErr, fmt.Errorf("read retained native session after rebind error: %w", observeErr)))
		}
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("resume rebind outcome is ambiguous: %w", rebindErr))
	}
	tx.sessionKey = rebound.Key()
	return nil
}

func (tx *goalAdmissionTx) prepareFreshGoalSessionForLaunch(ctx context.Context) (bool, error) {
	if tx.linuxV2Required {
		if err := tx.daemon.checkLinuxV2NativeAdmission(ctx, tx.key, tx.linuxV2Fence); err != nil {
			return false, fmt.Errorf("Linux v2 owner changed before clone effects: %w", err)
		}
	}
	if _, err := tx.daemon.store.MarkWorkspaceRecoveryRequired(tx.key); err != nil {
		return false, fmt.Errorf("mark workspace recovery required: %w", err)
	}
	if tx.linuxV2Required {
		if settled, err := tx.prepareLinuxCloneWorkspace(ctx); err != nil || settled {
			return settled, err
		}
	} else {
		if settled, err := tx.prepareGoalSubjectWorkspace(ctx); err != nil || settled {
			return settled, err
		}
	}
	if err := tx.recordGoalSessionLaunchIntent(); err != nil {
		return false, err
	}
	if cancelled, err := tx.abortFreshLaunchIfCancelled(); cancelled {
		return true, err
	}
	if _, err := tx.daemon.store.MarkGoalSessionLaunchStarted(tx.sessionKey, tx.daemon.now()); err != nil {
		return false, fmt.Errorf("mark Goal session launch: %w", tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, err))
	}
	if cancelled, err := tx.abortFreshLaunchIfCancelled(); cancelled {
		return true, err
	}
	return false, nil
}

func (tx *goalAdmissionTx) prepareLinuxCloneWorkspace(ctx context.Context) (bool, error) {
	linuxClone, fingerprint, err := tx.daemon.prepareLinuxV2CloneRun(ctx, tx.key, tx.linuxV2Fence, tx.admission)
	tx.linuxClone = linuxClone
	tx.fingerprint = fingerprint
	if tx.linuxClone != nil {
		tx.cloneCompensation.armed = true
	}
	if err != nil {
		return false, err
	}
	tx.prepared = tx.linuxClone.prepared
	if err := tx.daemon.installLinuxCloneRun(tx.key, tx.linuxClone); err != nil {
		return false, err
	}
	if tx.workspacePersisted != nil {
		tx.workspacePersisted(tx.prepared)
		tx.workspacePersisted = nil
	}
	if tx.daemon.isCancelled(tx.key) {
		return true, nil
	}
	return false, nil
}

func (tx *goalAdmissionTx) prepareGoalSubjectWorkspace(ctx context.Context) (bool, error) {
	subjectWorkspace, ok := tx.daemon.workspace.(goalSubjectWorkspace)
	if !ok {
		return false, errors.New("workspace service does not support immutable Goal subjects")
	}
	preparedSubject, err := subjectWorkspace.PrepareSubject(ctx, tx.daemon.config.Runtime.Workspace, workspace.RunRef{RunID: tx.key.RunID, Generation: tx.key.Generation}, tx.admission.Subject)
	if err != nil {
		return false, fmt.Errorf("prepare admitted Subject workspace: %w", err)
	}
	tx.prepared = preparedSubject.Prepared
	if preparedSubject.Subject != tx.admission.Subject {
		return false, errors.New("prepared Goal workspace does not match the admitted Subject")
	}
	if !tx.daemon.persistWorkspacePath(ctx, tx.key, tx.prepared.Path) {
		return false, errors.New("persist workspace path")
	}
	if tx.workspacePersisted != nil {
		tx.workspacePersisted(tx.prepared)
		tx.workspacePersisted = nil
	}
	if tx.daemon.isCancelled(tx.key) {
		return true, nil
	}
	tx.fingerprint, err = workspaceFingerprint(ctx, tx.prepared)
	if err != nil {
		return false, fmt.Errorf("fingerprint workspace: %w", err)
	}
	if tx.daemon.isCancelled(tx.key) {
		return true, nil
	}
	return false, nil
}

func (tx *goalAdmissionTx) recordGoalSessionLaunchIntent() error {
	localHandleID, err := state.NewGoalSessionLocalHandleID()
	if err != nil {
		return fmt.Errorf("generate local native session handle: %w", err)
	}
	launchIntentID, err := state.NewGoalSessionLocalHandleID()
	if err != nil {
		return fmt.Errorf("generate native launch intent: %w", err)
	}
	tx.localHandleID = localHandleID
	tx.sessionKey = state.GoalSessionKey{GoalID: tx.admission.GoalID, LocalHandleID: localHandleID}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: launchIntentID, GoalID: tx.admission.GoalID, GoalRevision: tx.admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(tx.admission.WorkItemID), TaskID: tx.claim.TaskID, RunID: tx.key.RunID, Generation: tx.key.Generation,
		AdmissionID: tx.admission.AdmissionID, HandoffSourceRunID: admissionHandoffSourceRunID(tx.admission.HandoffSourceRunID), LocalHandleID: localHandleID,
		BindingID: tx.bindingID, MachineID: tx.daemon.machineID, RuntimeID: tx.daemon.runtimeID, RuntimeEpoch: tx.daemon.runtimeEpoch,
		HarnessKind: string(tx.daemon.harnessCapabilities.Kind), HarnessVersion: tx.daemon.harnessCapabilities.NativeVersion,
		AdapterVersion: tx.daemon.harnessCapabilities.ImplementationVersion, AdapterProtocolVersion: tx.daemon.harnessCapabilities.ProtocolVersion,
		WorkspaceFingerprint: tx.fingerprint, WorkspacePath: tx.prepared.Path, RepositoryResourceID: tx.admission.Subject.ResourceID, SessionMode: string(tx.admission.SessionMode),
	}
	if _, err := tx.daemon.store.SaveGoalSessionLaunchIntent(intent); err != nil {
		return fmt.Errorf("save Goal session launch intent: %w", tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, err))
	}
	return nil
}

func (tx *goalAdmissionTx) queueGoalSessionAttach(_ context.Context) error {
	if tx.attachQueued {
		return nil
	}
	if _, err := tx.daemon.store.QueueGoalSessionAttach(tx.key, state.GoalSessionAttachDelivery{
		GoalID: tx.admission.GoalID, LocalHandleID: tx.localHandleID, BindingID: tx.bindingID,
		ServerIssuedBinding: tx.admission.SessionMode != protocol.SessionModeResume,
		HarnessKind:         string(tx.daemon.harnessCapabilities.Kind), HarnessVersion: tx.daemon.harnessCapabilities.NativeVersion,
		AdapterVersion: tx.daemon.harnessCapabilities.ImplementationVersion, WorkspaceFingerprint: tx.fingerprint,
		// Control owns a logical workspace key. The absolute local worktree path
		// remains in the daemon journal and must not become control-plane identity.
		Workspace: tx.daemon.config.Runtime.Workspace, RepositoryResourceID: &tx.admission.Subject.ResourceID,
	}); err != nil {
		if tx.admission.SessionMode != protocol.SessionModeResume {
			if abortErr := tx.daemon.abortGoalSessionLaunchBeforeNativeStart(tx.sessionKey); abortErr != nil {
				return fmt.Errorf("queue native Goal session attach intent: %w", errors.Join(err, fmt.Errorf("abort native Goal session launch intent: %w", abortErr)))
			}
		}
		return fmt.Errorf("queue native Goal session attach intent: %w", err)
	}
	tx.attachQueued = true
	return nil
}

func (tx *goalAdmissionTx) startGoalNativeSession(ctx context.Context) (bool, error) {
	tx.sink = harness.EventSinkFunc(tx.queueNativeEventSink)
	tx.initialLeaseDeadlineAt = leaseDeadlineAt(tx.claim, tx.claim.RequestStartedAt)
	tx.initialLeaseDeadline = remainingLeaseDeadlineAt(tx.initialLeaseDeadlineAt, tx.daemon.localNow())
	if err := tx.revalidateStartLease(); err != nil {
		return false, err
	}
	if cancelled, err := tx.abortFreshLaunchIfCancelled(); cancelled {
		return true, err
	}
	if err := tx.buildNativeInvocation(ctx); err != nil {
		return false, err
	}
	if err := tx.publishRetainedNativeStartBoundary(ctx); err != nil {
		return false, err
	}
	tx.armNativeLeaseSequence()
	if settled, err := tx.acquireNativeStartOwnership(ctx); err != nil || settled {
		return settled, err
	}
	tx.handOffLaunchToNativeOwner()
	startErr := tx.launchNativeProcess(ctx)
	if settled, err := tx.settleStartCancelledAfterLaunch(ctx); err != nil || settled {
		return settled, err
	}
	if settled, err := tx.settleCancelledProcessStart(ctx, startErr); err != nil || settled {
		return settled, err
	}
	if settled, err := tx.settleExpiredNativeStartFailure(ctx, "start native Goal session", startErr, false, tx.resume == nil && tx.attachQueued && !tx.attachReady); err != nil || settled {
		return settled, err
	}
	if startErr != nil {
		return false, tx.failNativeStart(ctx, startErr)
	}
	if settled, err := tx.settleLeaseExpiredProcessStart(ctx); err != nil || settled {
		return settled, err
	}
	if err := tx.requireStagedSession(); err != nil {
		return false, err
	}
	return false, nil
}

func (tx *goalAdmissionTx) revalidateStartLease() error {
	if err := tx.daemon.validateClaimLeaseForStart(tx.claim, tx.claim.RequestStartedAt, "native process"); err != nil {
		if tx.resume == nil {
			var discardErr error
			if tx.attachQueued && !tx.attachReady {
				discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
			}
			compensationErr := tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, err)
			return errors.Join(compensationErr, discardErr)
		}
		return err
	}
	return nil
}

func (tx *goalAdmissionTx) buildNativeInvocation(ctx context.Context) error {
	tx.nativeInvocation = execution.Invocation{
		Program:                tx.profile.Command,
		Args:                   tx.profile.Args,
		Dir:                    tx.prepared.Path,
		Env:                    tx.environment,
		InitialLeaseDeadline:   tx.initialLeaseDeadline,
		InitialLeaseDeadlineAt: tx.initialLeaseDeadlineAt,
		InitialLeaseSequence:   1,
		Recorder:               tx.daemon.processRecorder(tx.key, tx.persistNativeProcess),
	}
	if !tx.linuxV2Required {
		return nil
	}
	if err := tx.requireLinuxV2CloneGrant(); err != nil {
		return err
	}
	if err := tx.applyLinuxV2Invocation(ctx); err != nil {
		return err
	}
	return tx.attachLinuxCloneGrant(ctx)
}

func (tx *goalAdmissionTx) requireLinuxV2CloneGrant() error {
	if tx.linuxClone != nil || tx.linuxCloneReuse != nil {
		return nil
	}
	buildErr := errors.New("Linux v2 native admission has no durably reserved B-owned git_clone_v2 grant")
	var discardErr error
	if tx.attachQueued && !tx.attachReady {
		discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
	}
	return errors.Join(tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, buildErr), discardErr)
}

func (tx *goalAdmissionTx) applyLinuxV2Invocation(ctx context.Context) error {
	tx.nativeInvocation.LinuxV2Required = true
	nativeInvocation, err := tx.linuxV2Owner.BuildInvocation(ctx, tx.key, tx.linuxV2Fence, tx.nativeInvocation, tx.daemon.linuxV2JournalCallbacks(tx.key, tx.linuxV2Fence))
	tx.nativeInvocation = nativeInvocation
	if err != nil {
		buildErr := fmt.Errorf("build explicit Linux v2 native invocation: %w", err)
		var discardErr error
		if tx.attachQueued && !tx.attachReady {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		if tx.resume == nil {
			return errors.Join(tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, buildErr), discardErr)
		}
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, errors.Join(buildErr, discardErr))
	}
	if !tx.nativeInvocation.LinuxV2Required || tx.nativeInvocation.LinuxV2 == nil {
		buildErr := errors.New("Linux v2 native owner did not supply the required run grant and callbacks")
		var discardErr error
		if tx.attachQueued && !tx.attachReady {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		if tx.resume == nil {
			return errors.Join(tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, buildErr), discardErr)
		}
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, errors.Join(buildErr, discardErr))
	}
	if tx.nativeInvocation.LinuxV2.RunKey != tx.key || tx.nativeInvocation.LinuxV2.Fence != tx.linuxV2Fence {
		buildErr := errors.New("Linux v2 native owner changed the exact RunKey or claim fence")
		var discardErr error
		if tx.attachQueued && !tx.attachReady {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		return errors.Join(tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, buildErr), discardErr)
	}
	return nil
}

func (tx *goalAdmissionTx) attachLinuxCloneGrant(_ context.Context) error {
	if tx.linuxClone != nil {
		grant := tx.linuxClone.grant.Clone()
		tx.nativeInvocation.LinuxV2.CloneGrant = &grant
		if err := tx.daemon.installLinuxCloneRunnerCallbacks(tx.key, tx.linuxV2Fence, tx.admission, tx.linuxClone, &tx.nativeInvocation.LinuxV2.Callbacks); err != nil {
			buildErr := fmt.Errorf("install B-owned Linux clone callbacks: %w", err)
			var discardErr error
			if tx.attachQueued && !tx.attachReady {
				discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
			}
			return errors.Join(tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, buildErr), discardErr)
		}
	} else if tx.linuxCloneReuse != nil {
		grant := tx.linuxCloneReuse.Clone()
		tx.nativeInvocation.LinuxV2.CloneReuse = &grant
	}
	return nil
}

func (tx *goalAdmissionTx) publishRetainedNativeStartBoundary(_ context.Context) error {
	if tx.resume == nil {
		return nil
	}
	// For a retained session, Ready is also the durable native-start boundary: an unready
	// rebind is compensable, but a ready one may be a new process — never a missing-PID stop.
	if _, err := tx.daemon.markGoalSessionAttachDeliveryReady(tx.key, tx.localHandleID); err != nil {
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("persist retained native-start boundary: %w", err))
	}
	if _, err := tx.daemon.store.MarkGoalSessionResumeStartAttempted(tx.sessionKey); err != nil {
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("persist retained native-start attempt: %w", err))
	}
	tx.attachReady = true
	return nil
}

func (tx *goalAdmissionTx) armNativeLeaseSequence() {
	if tx.initialLeaseDeadline <= 0 {
		return
	}
	tx.daemon.mu.Lock()
	if active := tx.daemon.running[tx.key]; active != nil {
		// Runner already consumed sequence 1 while arming the native watchdog; the first
		// daemon renewal must therefore use sequence 2.
		active.leaseSequence = 1
	}
	tx.daemon.mu.Unlock()
}

func (tx *goalAdmissionTx) acquireNativeStartOwnership(ctx context.Context) (bool, error) {
	if tx.daemon.beginNativeStart(tx.key) {
		return false, nil
	}
	if tx.resume == nil {
		var discardErr error
		if tx.attachQueued && !tx.attachReady {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		compensationErr := tx.daemon.compensatePreNativeGoalLaunchFailure(tx.key, tx.sessionKey, errors.New("native Goal start was rejected by cancellation or lease expiry"))
		return true, errors.Join(compensationErr, discardErr)
	}
	if tx.daemon.isCancelled(tx.key) {
		if err := tx.daemon.settleCancelledResumeBeforeNativeStart(ctx, tx.key, tx.sessionKey); err != nil {
			tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
			return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled retained resume before native start: %w", err))
		}
		tx.daemon.finishAttachedStart(tx.key)
		return true, nil
	}
	return true, taskResultFailure(protocol.TaskResultReasonResumeRejected, errors.New("native Goal start was rejected by cancellation or lease expiry"))
}

func (tx *goalAdmissionTx) handOffLaunchToNativeOwner() {
	if tx.linuxClone != nil {
		tx.cloneCompensation.released = true
	}
	if tx.linuxCloneReuse != nil {
		tx.reuseCompensation.released = true
	}
}

func (tx *goalAdmissionTx) launchNativeProcess(ctx context.Context) error {
	request := harness.StartRequest{
		AdmissionID:   tx.admission.AdmissionID,
		LocalHandleID: tx.localHandleID,
		Workspace:     tx.prepared.Path,
		Goal:          tx.claim.Work.Goal,
		ModelProfile:  tx.admission.ModelProfile,
		Resume:        tx.resume,
		Limits: harness.Limits{
			MaxTurns:        int(tx.admission.Limits.MaxTurns),
			DeadlineAt:      parseAdmissionDeadline(tx.admission.Limits.DeadlineAt),
			MaxCostMicrousd: tx.admission.Limits.MaxCostMicrousd,
		},
		ProviderAccess: tx.providerAccess,
		ProviderBridge: tx.providerBridge,
		Grant:          harness.NewLaunchGrant(tx.nativeInvocation),
		Invocation:     tx.nativeInvocation,
	}
	request = harness.PopulateOpencodeRecorderMembers(request)
	session, err := tx.adapter.Start(ctx, request, tx.sink)
	tx.session = session
	tx.nativeOutcomeCompensation.armed = true
	if isNilHarnessSession(tx.session) {
		// An adapter can return a typed-nil harness.Session through an interface.
		// Treat it as no transferred cleanup owner.
		tx.session = nil
	}
	if tx.session != nil {
		tx.bridgeCompensation.released = true
	}
	// Publish even a nil result so the Adapter.Start barrier cannot strand a
	// cancellation when start fails without returning a session.
	tx.daemon.attachPartialNativeSession(tx.key, tx.session, tx.sessionKey, tx.admission, tx.prepared)
	if tx.linuxV2Required && tx.session != nil && err == nil {
		if releaseErr := tx.daemon.confirmLinuxV2LiveRelease(ctx, tx.key, tx.linuxV2Fence); releaseErr != nil {
			err = fmt.Errorf("prove exact Linux v2 initial lease ACK and RELEASE before admitting another start: %w", releaseErr)
		} else if tx.releaseLinuxV2LaunchPhase != nil {
			tx.releaseLinuxV2LaunchPhase()
			tx.releaseLinuxV2LaunchPhase = nil
			tx.launchPhaseCompensation.released = true
		}
	}
	return err
}

func (tx *goalAdmissionTx) persistNativeProcess(pid int, identity string) error {
	persistErr := tx.daemon.persistProcessMarker(tx.key, pid, identity, tx.daemon.now())
	if persistErr == nil && tx.daemon.options.processObserver != nil {
		tx.daemon.options.processObserver(tx.key, pid, identity, tx.daemon.now())
	}
	return persistErr
}

func (tx *goalAdmissionTx) queueNativeEventSink(_ context.Context, event harness.Event) error {
	return tx.daemon.queueNativeEvent(tx.key, event)
}

func (tx *goalAdmissionTx) settlePublishedCancellation(ctx context.Context, stage string, cause error, discardAttach bool) (bool, error) {
	if tx.session == nil || !tx.daemon.isCancelled(tx.key) {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(context.WithoutCancel(ctx), tx.key, tx.sessionKey, tx.localHandleID, tx.session, discardAttach, false)
	if !settled {
		return false, nil
	}
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal %s: %w", stage, errors.Join(cause, settleErr)))
	}
	return true, nil
}

func (tx *goalAdmissionTx) settleExpiredNativeStartFailure(ctx context.Context, stage string, stageErr error, attached, discardAttach bool) (bool, error) {
	if stageErr == nil {
		return false, nil
	}
	leaseExpired, leaseReason := tx.daemon.nativeStartLeaseExpired(tx.key)
	if !leaseExpired {
		return false, nil
	}
	tx.daemon.finishNativeStartPublication(tx.key)
	if leaseReason == "" {
		leaseReason = "lease expired during native start"
	}
	cause := errors.Join(errors.New(leaseReason), fmt.Errorf("%s: %w", stage, stageErr))
	tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
	var discardErr error
	if discardAttach {
		discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
	}
	var abandonErr error
	if tx.session != nil {
		abandonErr = tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, attached, cause)
	} else if tx.resume == nil {
		abortErr := tx.daemon.abortGoalSessionLaunchBeforeNativeStart(tx.sessionKey)
		if abortErr != nil {
			abandonErr = tx.daemon.resolveCancelledNativeStartAbortFailure(tx.key, tx.sessionKey, fmt.Errorf("abort lease-expired native Goal launch: %w", abortErr))
		}
	} else {
		abandonErr = tx.daemon.markGoalSessionUncertain(tx.sessionKey, boundedGoalSessionUncertainReason("lease-expired retained native start returned no session: "+stageErr.Error()))
	}
	ackErr := tx.daemon.acknowledgeExpiredNativeStartCancellation(ctx, tx.key)
	if abandonErr != nil || ackErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, discardErr, abandonErr, ackErr))
	}
	return true, taskResultFailure(protocol.TaskResultReasonProcessFailure, errors.Join(cause, discardErr))
}

func (tx *goalAdmissionTx) settleStartCancelledAfterLaunch(ctx context.Context) (bool, error) {
	if tx.session == nil {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(ctx, tx.key, tx.sessionKey, tx.localHandleID, tx.session, tx.attachQueued && !tx.attachReady, false)
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal start: %w", settleErr))
	}
	return settled, nil
}

func (tx *goalAdmissionTx) settleCancelledProcessStart(ctx context.Context, startErr error) (bool, error) {
	if startErr == nil || tx.session != nil || tx.resume != nil || !errors.Is(startErr, context.Canceled) {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(ctx, tx.key, tx.sessionKey, tx.localHandleID, nil, tx.attachQueued && !tx.attachReady, true)
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal start: %w", settleErr))
	}
	return settled, nil
}

func (tx *goalAdmissionTx) failNativeStart(_ context.Context, startErr error) error {
	tx.daemon.finishNativeStartPublication(tx.key)
	if tx.session != nil {
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		var discardErr error
		if tx.resume == nil {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, startErr)
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(fmt.Errorf("start native Goal session: %w", startErr), discardErr, abandonErr))
	}
	if tx.resume != nil && (errors.Is(startErr, pi.ErrResumeRejected) || errors.Is(startErr, omp.ErrResumeRejected) || errors.Is(startErr, omo.ErrResumeRejected)) {
		// The adapter rejected the retained handle before returning a session: persist the
		// known native stop before any Control RPC so a timeout stays repairable by readback.
		if _, stopErr := tx.daemon.store.MarkGoalSessionResumeRejected(tx.sessionKey); stopErr != nil {
			return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(startErr, fmt.Errorf("persist rejected retained native stop: %w", stopErr)))
		}
		return taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("start retained native session: %w", startErr))
	}
	tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
	if tx.resume != nil {
		// Ready proves this call was allowed to attempt a native resume: on an ambiguous
		// transport failure keep the exact attachment identity — restart recovery needs it.
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("start retained native session: %w", startErr))
	}
	if _, discardErr := tx.daemon.store.DiscardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID); discardErr != nil {
		return fmt.Errorf("discard unsent native Goal session attach intent: %w", errors.Join(startErr, discardErr))
	}
	uncertainErr := tx.daemon.markGoalSessionUncertain(tx.sessionKey, boundedGoalSessionUncertainReason("native transport start outcome is unknown: "+errorText(startErr)))
	if uncertainErr != nil {
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(
			fmt.Errorf("start native Goal session: %w", startErr),
			fmt.Errorf("mark native Goal session uncertain: %w", uncertainErr),
		))
	}
	return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("start native Goal session: %w", startErr))
}

func (tx *goalAdmissionTx) settleLeaseExpiredProcessStart(ctx context.Context) (bool, error) {
	leaseExpired, leaseReason := tx.daemon.nativeStartLeaseExpired(tx.key)
	if !leaseExpired {
		return false, nil
	}
	if leaseReason == "" {
		leaseReason = "lease expired during native start"
	}
	cause := errors.New(leaseReason)
	tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
	var discardErr error
	if tx.resume == nil {
		discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
	}
	abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, cause)
	ackErr := tx.daemon.acknowledgeExpiredNativeStartCancellation(ctx, tx.key)
	if abandonErr != nil || ackErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, discardErr, abandonErr, ackErr))
	}
	return true, taskResultFailure(protocol.TaskResultReasonProcessFailure, errors.Join(cause, discardErr))
}

func (tx *goalAdmissionTx) requireStagedSession() error {
	staged, ok := tx.session.(harness.StagedSession)
	if !ok {
		tx.daemon.finishNativeStartPublication(tx.key)
		cause := errors.New("native Goal adapter does not implement staged session launch")
		var discardErr error
		if tx.resume == nil {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, cause)
		return taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, discardErr, abandonErr))
	}
	tx.staged = staged
	return nil
}

func (tx *goalAdmissionTx) attachGoalNativeSession(ctx context.Context) (bool, error) {
	if err := tx.daemon.attachNativeSession(tx.key, tx.session, tx.sessionKey, tx.admission, tx.prepared, tx.deadline); err != nil {
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "session publication", err, tx.attachQueued && !tx.attachReady); settled {
			return true, settleErr
		}
		tx.daemon.finishNativeStartPublication(tx.key)
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		var discardErr error
		if tx.resume == nil {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, err)
		return false, errors.Join(err, discardErr, abandonErr)
	}
	tx.operationContext, tx.operationCancel = context.WithDeadline(ctx, tx.deadline)
	return false, nil
}

func (tx *goalAdmissionTx) openGoalNativeSession(ctx context.Context) (bool, error) {
	handle, err := tx.staged.Open(tx.operationContext)
	if err != nil {
		if settled, settleErr := tx.settleExpiredNativeStartFailure(ctx, "open native Goal session", err, false, tx.resume == nil && tx.attachQueued && !tx.attachReady); settled {
			return true, settleErr
		}
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "open", err, tx.attachQueued && !tx.attachReady); settled {
			return true, settleErr
		}
		tx.daemon.finishNativeStartPublication(tx.key)
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		var discardErr error
		if tx.resume == nil {
			discardErr = tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		}
		if tx.resume != nil && (errors.Is(err, pi.ErrResumeRejected) || errors.Is(err, omp.ErrResumeRejected) || errors.Is(err, omo.ErrResumeRejected)) {
			attachErr := tx.daemon.attachResumedNativeSession(tx.operationContext, tx.key, tx.claim, tx.admission, tx.sessionKey)
			abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, attachErr == nil, err)
			if attachErr == nil && abandonErr == nil {
				return false, taskResultFailure(protocol.TaskResultReasonResumeRejected, fmt.Errorf("open retained native session: %w", err))
			}
			return false, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(err, attachErr, abandonErr))
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, err)
		return false, fmt.Errorf("open native Goal session: %w", errors.Join(err, discardErr, abandonErr))
	}
	tx.handle = handle
	if tx.resume != nil && (handle.ID != tx.resume.NativeSessionID || handle.Filename != tx.resume.NativeSessionFilename) {
		tx.daemon.finishNativeStartPublication(tx.key)
		cause := errors.New("retained native session returned a different native handle")
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		if abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, cause); abandonErr != nil {
			return false, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, abandonErr))
		}
		return false, taskResultFailure(protocol.TaskResultReasonResumeRejected, cause)
	}
	return false, nil
}

func (tx *goalAdmissionTx) openGoalTurnAndSettleTerminalOwnership(ctx context.Context) error {
	canonicalJSON, settled, err := tx.attachPublishedNativeSession(ctx)
	if err != nil || settled {
		return err
	}
	return tx.runGoalTurn(ctx, canonicalJSON)
}

func (tx *goalAdmissionTx) attachPublishedNativeSession(ctx context.Context) ([]byte, bool, error) {
	if settled, err := tx.settleCancelledBeforeTurnStart(ctx); err != nil || settled {
		return nil, settled, err
	}
	if settled, err := tx.settleLeaseExpiredAttachedStart(ctx); err != nil || settled {
		return nil, settled, err
	}
	if tx.resume == nil {
		if err := tx.persistNativeSessionHandle(); err != nil {
			return nil, false, err
		}
	}
	if settled, err := tx.markNativeSessionAttached(ctx); err != nil || settled {
		return nil, settled, err
	}
	if err := tx.loadNativeRunJournal(); err != nil {
		return nil, false, err
	}
	if settled, err := tx.deliverNativeSessionAttach(ctx); err != nil || settled {
		return nil, settled, err
	}
	canonicalJSON, settled, err := tx.fetchGoalRunContextJSON(ctx)
	if err != nil || settled {
		return nil, settled, err
	}
	if settled, err := tx.markNativeRunRunning(ctx); err != nil || settled {
		return nil, settled, err
	}
	if settled, err := tx.settleCancelledAttachedStart(ctx); err != nil || settled {
		return nil, settled, err
	}
	if err := tx.abandonCancelledOperationContext(ctx); err != nil {
		return nil, false, err
	}
	return canonicalJSON, false, nil
}

func (tx *goalAdmissionTx) runGoalTurn(ctx context.Context, canonicalJSON []byte) error {
	tx.turnErr = tx.staged.StartTurn(tx.operationContext, harness.TurnRequest{Goal: tx.claim.Work.Goal, Context: canonicalJSON})
	if settled, err := tx.settleLeaseExpiredTurn(ctx); err != nil || settled {
		return err
	}
	if tx.settleObservedTerminalCancellation() {
		return nil
	}
	if settled, err := tx.settleCancelledTurnStart(ctx); err != nil || settled {
		return err
	}
	if settled, err := tx.settleNativeTurnFailure(); err != nil || settled {
		return err
	}
	if tx.daemon.finishAttachedStart(tx.key) {
		tx.daemon.signalOutbox()
	}
	return nil
}

func (tx *goalAdmissionTx) settleCancelledBeforeTurnStart(ctx context.Context) (bool, error) {
	if !tx.daemon.isCancelled(tx.key) {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(ctx, tx.key, tx.sessionKey, tx.localHandleID, tx.session, tx.attachQueued && !tx.attachReady, false)
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal start after Open: %w", settleErr))
	}
	return settled, nil
}

func (tx *goalAdmissionTx) settleLeaseExpiredAttachedStart(ctx context.Context) (bool, error) {
	leaseExpired, leaseReason := tx.daemon.nativeStartLeaseExpired(tx.key)
	if !leaseExpired {
		return false, nil
	}
	if leaseReason == "" {
		leaseReason = "lease expired during native start"
	}
	cause := errors.New(leaseReason)
	tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
	abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attachReady, cause)
	ackErr := tx.daemon.acknowledgeExpiredNativeStartCancellation(ctx, tx.key)
	if abandonErr != nil || ackErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, abandonErr, ackErr))
	}
	return true, taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
}

func (tx *goalAdmissionTx) persistNativeSessionHandle() error {
	if _, err := tx.daemon.store.PersistGoalSessionHandle(tx.sessionKey, state.GoalSessionHandle{NativeSessionID: tx.handle.ID, NativeSessionFilename: tx.handle.Filename}); err != nil {
		tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
		discardErr := tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, false, err)
		return fmt.Errorf("persist native Goal session handle: %w", errors.Join(err, discardErr, abandonErr))
	}
	return nil
}

func (tx *goalAdmissionTx) markNativeSessionAttached(ctx context.Context) (bool, error) {
	tx.attached = true
	if tx.attachReady {
		return false, nil
	}
	if _, err := tx.daemon.markGoalSessionAttachDeliveryReady(tx.key, tx.localHandleID); err != nil {
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "attachment readiness", err, true); settled {
			return true, settleErr
		}
		discardErr := tx.daemon.discardUnreadyGoalSessionAttachDelivery(tx.key, tx.localHandleID)
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		if abandonErr != nil {
			tx.daemon.requireNativeSessionCloseRetry(tx.key, tx.session)
		}
		return false, fmt.Errorf("mark native Goal session attach delivery ready: %w", errors.Join(err, discardErr, abandonErr))
	}
	return false, nil
}

func (tx *goalAdmissionTx) loadNativeRunJournal() error {
	runJournal, err := tx.daemon.store.LoadJournal(tx.key)
	if err != nil {
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return fmt.Errorf("load native run fence: %w", errors.Join(err, abandonErr))
	}
	tx.runJournal = runJournal
	return nil
}

func (tx *goalAdmissionTx) deliverNativeSessionAttach(ctx context.Context) (bool, error) {
	var receipt *control.GoalSessionReceipt
	runJournal, receipt, err := tx.daemon.deliverGoalDelivery(tx.operationContext, tx.runJournal, state.GoalDeliverySessionAttach, tx.localHandleID, func(receipt control.GoalSessionReceipt) error {
		return validateGoalSessionReceipt(tx.admission, tx.claim, tx.key, tx.localHandleID, receipt)
	})
	if err != nil {
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "attachment delivery", err, false); settled {
			return true, settleErr
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return false, fmt.Errorf("attach native Goal session: %w", errors.Join(err, abandonErr))
	}
	tx.runJournal = runJournal
	if receipt == nil {
		cause := errors.New("native Goal session attach receipt is unavailable")
		return false, errors.Join(cause, tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, cause))
	}
	tx.receipt = receipt
	return false, nil
}

func (tx *goalAdmissionTx) fetchGoalRunContextJSON(ctx context.Context) ([]byte, bool, error) {
	requestContext, cancel := tx.daemon.controlContext(tx.operationContext)
	canonical, err := tx.goalAPI.FetchRunContext(requestContext, tx.key.RunID, tx.runJournal.Fence())
	cancel()
	if err != nil {
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "context fetch", err, false); settled {
			return nil, true, settleErr
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return nil, false, fmt.Errorf("fetch Goal run context: %w", errors.Join(err, abandonErr))
	}
	if err := validateGoalRunContext(tx.admission, tx.claim, tx.key, *tx.receipt, canonical); err != nil {
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return nil, false, errors.Join(err, abandonErr)
	}
	canonicalJSON, err := json.Marshal(canonical.Context)
	if err != nil {
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return nil, false, fmt.Errorf("encode canonical Goal context: %w", errors.Join(err, abandonErr))
	}
	return canonicalJSON, false, nil
}

func (tx *goalAdmissionTx) markNativeRunRunning(ctx context.Context) (bool, error) {
	if err := tx.daemon.markRunning(tx.key); err != nil {
		if settled, settleErr := tx.settlePublishedCancellation(ctx, "running publication", err, false); settled {
			return true, settleErr
		}
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, err)
		return false, fmt.Errorf("mark native Goal run running: %w", errors.Join(err, abandonErr))
	}
	tx.daemon.mu.Lock()
	active := tx.daemon.running[tx.key]
	if active != nil {
		active.nativeSession = tx.session
		active.goalSession = &tx.sessionKey
		admissionCopy := tx.admission
		active.goalAdmission = &admissionCopy
		active.prepared = tx.prepared
	}
	tx.daemon.mu.Unlock()
	tx.active = active
	if active == nil {
		abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, errors.New("Goal run was cancelled before native turn start"))
		return false, errors.Join(errors.New("Goal run ended before native turn start"), abandonErr)
	}
	return false, nil
}

func (tx *goalAdmissionTx) settleCancelledAttachedStart(ctx context.Context) (bool, error) {
	if !tx.daemon.isCancelled(tx.key) {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(ctx, tx.key, tx.sessionKey, tx.localHandleID, tx.session, false, false)
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal start before turn: %w", settleErr))
	}
	return settled, nil
}

func (tx *goalAdmissionTx) abandonCancelledOperationContext(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, tx.attached, ctx.Err())
	return errors.Join(ctx.Err(), abandonErr)
}

func (tx *goalAdmissionTx) settleLeaseExpiredTurn(ctx context.Context) (bool, error) {
	leaseExpired, leaseReason := tx.daemon.nativeStartLeaseExpired(tx.key)
	if !leaseExpired {
		return false, nil
	}
	if tx.daemon.transferNativeStartTerminal(tx.key, tx.active, tx.session) {
		tx.daemon.mu.Lock()
		commandID := ""
		if tx.daemon.running[tx.key] == tx.active {
			commandID = tx.active.cancelCommandID
		}
		tx.daemon.mu.Unlock()
		ackErr := tx.daemon.acknowledgeExpiredNativeStartCancellationCommand(ctx, tx.key, commandID)
		tx.daemon.waitForNativeRun(tx.daemon.failedStartFinalizationContext(), tx.key, tx.active, tx.session)
		if ackErr != nil {
			ackErr = tx.daemon.acknowledgeExpiredNativeStartCancellationCommand(ctx, tx.key, commandID)
		}
		if ackErr != nil {
			return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, ackErr)
		}
		return true, nil
	}
	tx.daemon.finishNativeStartPublication(tx.key)
	if leaseReason == "" {
		leaseReason = "lease expired during native turn start"
	}
	cause := errors.Join(errors.New(leaseReason), tx.turnErr)
	tx.daemon.retainUnknownGoalLaunchWorkspace(tx.key)
	abandonErr := tx.daemon.abandonGoalSession(tx.sessionKey, tx.session, true, cause)
	ackErr := tx.daemon.acknowledgeExpiredNativeStartCancellation(ctx, tx.key)
	if abandonErr != nil || ackErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, errors.Join(cause, abandonErr, ackErr))
	}
	return true, taskResultFailure(protocol.TaskResultReasonProcessFailure, cause)
}

func (tx *goalAdmissionTx) settleObservedTerminalCancellation() bool {
	tx.daemon.mu.Lock()
	terminalObserved := tx.daemon.running[tx.key] == tx.active && tx.active.nativeStartTerminalObserved
	tx.daemon.mu.Unlock()
	if !terminalObserved || !tx.daemon.isCancelled(tx.key) || !tx.daemon.transferNativeStartTerminal(tx.key, tx.active, tx.session) {
		return false
	}
	tx.daemon.mu.Lock()
	if tx.daemon.running[tx.key] == tx.active && nativeTerminalOwnedBy(tx.active, tx.active.nativeTerminalOwnerClaim) {
		tx.active.recordNativeCancelObserved(tx.active.cancelCommandID, true, true)
	}
	tx.daemon.mu.Unlock()
	tx.daemon.waitForNativeRun(tx.daemon.failedStartFinalizationContext(), tx.key, tx.active, tx.session)
	return true
}

func (tx *goalAdmissionTx) settleCancelledTurnStart(ctx context.Context) (bool, error) {
	if !tx.daemon.isCancelled(tx.key) {
		return false, nil
	}
	settled, settleErr := tx.daemon.settleCancelledNativeStart(ctx, tx.key, tx.sessionKey, tx.localHandleID, tx.session, false, false)
	if settleErr != nil {
		return true, taskResultFailure(protocol.TaskResultReasonUnknownOutcome, fmt.Errorf("settle cancelled native Goal turn start: %w", settleErr))
	}
	return settled, nil
}

func (tx *goalAdmissionTx) settleNativeTurnFailure() (bool, error) {
	if tx.turnErr == nil {
		return false, nil
	}
	if abandonErr := tx.daemon.abandonGoalSessionUncertain(tx.sessionKey, tx.session, tx.turnErr); abandonErr != nil {
		return true, fmt.Errorf("start native Goal turn: %w", errors.Join(tx.turnErr, abandonErr))
	}
	return true, fmt.Errorf("start native Goal turn: %w", tx.turnErr)
}
