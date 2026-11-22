package app

import (
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// journalStoreBase is the durable store seam of this package: exactly the *state.Store methods
// package app selects on every build target, so a test can substitute a store at this boundary.
// journalStore is this set plus the target-only methods; the *_test.go pins hold both sets.
type journalStoreBase interface {
	AbortGoalSessionLaunchBeforeNativeStart(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	AbortLinuxCloneReuseBeforeNativeStart(key state.RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant) (state.RunJournal, error)
	AdvanceLeaseExpiry(key state.RunKey, expiry time.Time) (state.RunJournal, error)
	BeginLinuxCloneCleanup(key state.RunKey, fence protocol.Fence) (state.RunJournal, error)
	BindLinuxCloneReuse(key state.RunKey, fence protocol.Fence, grant authority.LinuxCloneReuseGrant, liveOwners map[state.RunKey]protocol.Fence) (state.RunJournal, error)
	BindSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, pid int, identity string) (state.RunJournal, error)
	CheckLinuxV2NativeAdmission(key state.RunKey, fence protocol.Fence, now time.Time, liveOwners map[state.RunKey]protocol.Fence) error
	CheckLinuxV2NativeAdmissionBeforeClaim(now time.Time, liveOwners map[state.RunKey]protocol.Fence) error
	ClearContainmentUnproven(key state.RunKey, pid int, identity string) (state.RunJournal, error)
	ClearNativeUsageRecoveryRequired(key state.RunKey, usage protocol.Usage) (state.RunJournal, error)
	ClearProcessDetails(key state.RunKey, pid int, identity string) (state.RunJournal, error)
	ClearSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (state.RunJournal, error)
	ClearSupervisorHandoffAfterAbort(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (state.RunJournal, error)
	Close() error
	CloseGoalSession(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	CommitSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (state.RunJournal, error)
	CompleteControlCommand(key state.RunKey, commandID string, kind string, outcome string, event protocol.RunEvent) (state.RunJournal, error)
	CompleteLinuxCloneCleanup(key state.RunKey, fence protocol.Fence, expected authority.LinuxCloneCleanupIntent, proof authority.LinuxCloneCleanupProof) (state.RunJournal, error)
	CompleteProvideInput(key state.RunKey, commandID string, payloadDigest string, outcome string) (state.RunJournal, error)
	CompleteProviderAction(key state.RunKey, completed state.ProviderActionIntent) (state.RunJournal, error)
	DeleteEnrollmentIntent() error
	DeleteJournal(key state.RunKey) error
	DiscardUnreadyGoalSessionAttachDelivery(key state.RunKey, localHandleID string) (state.RunJournal, error)
	EnterCleanupPending(key state.RunKey) (state.RunJournal, error)
	ListGoalSessions() ([]state.GoalSessionJournal, error)
	ListJournals() ([]state.RunJournal, error)
	ListLateGoalUsage() ([]state.LateGoalUsageLedger, error)
	LoadEnrollmentIntent() (state.EnrollmentIntent, error)
	LoadGoalSession(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	LoadGoalSessionByControlSessionID(controlSessionID string) (state.GoalSessionJournal, error)
	LoadGoalSessionByHandle(localHandleID string) (state.GoalSessionJournal, error)
	LoadIdentity() (state.MachineIdentity, error)
	LoadJournal(key state.RunKey) (state.RunJournal, error)
	MarkCommandAcknowledgementsDelivered(key state.RunKey, acknowledgementIDs []string) (state.RunJournal, error)
	MarkContainmentUnproven(key state.RunKey, pid int, identity string) (state.RunJournal, error)
	MarkEventsDelivered(key state.RunKey, eventIDs []string) (state.RunJournal, error)
	MarkEventsDeliveredAndQueueOutputTruncatedMarker(key state.RunKey, eventIDs []string, at time.Time, newEventID func() (string, error)) (state.RunJournal, bool, error)
	MarkGoalDeliveryDelivered(key state.RunKey, kind state.GoalDeliveryKind, deliveryID string, payloadDigest string) (state.RunJournal, error)
	MarkGoalSessionAttachDeliveryReady(key state.RunKey, localHandleID string) (state.RunJournal, error)
	MarkGoalSessionAvailable(key state.GoalSessionKey, certificate state.GoalSessionStopCertificate) (state.GoalSessionJournal, error)
	MarkGoalSessionLaunchStarted(key state.GoalSessionKey, at ...time.Time) (state.GoalSessionJournal, error)
	MarkGoalSessionResumeNativeStopped(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	MarkGoalSessionResumeRejected(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	MarkGoalSessionResumeStartAttempted(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	MarkGoalSessionStoppedPending(key state.GoalSessionKey) (state.GoalSessionJournal, error)
	MarkGoalSessionUncertain(key state.GoalSessionKey, reason string) (state.GoalSessionJournal, error)
	MarkLateGoalUsageDelivered(key state.RunKey, deliveryID string, payloadDigest string) (state.LateGoalUsageLedger, error)
	MarkNativeUsageRecoveryRequired(key state.RunKey) (state.RunJournal, error)
	MarkNativeUsageRecoverySettlement(key state.RunKey, settlement *state.NativeUsageTerminalRecovery) (state.RunJournal, error)
	MarkTransitionAttempted(key state.RunKey, transitionID string) (state.RunJournal, error)
	MarkTransitionsDelivered(key state.RunKey, transitionIDs []string) (state.RunJournal, error)
	MarkWorkspaceRecoveryRequired(key state.RunKey) (state.RunJournal, error)
	PersistGoalSessionControlAttachment(key state.GoalSessionKey, controlSessionID string, bindingID string, attachmentReceiptID string, serverIssuedBinding bool) (state.GoalSessionJournal, error)
	PersistGoalSessionHandle(key state.GoalSessionKey, handle state.GoalSessionHandle, expected ...state.GoalSessionCompatibility) (state.GoalSessionJournal, error)
	PrepareControlCommand(key state.RunKey, intent state.ControlCommandIntent) (state.RunJournal, bool, error)
	PrepareProvideInput(key state.RunKey, intent state.InputCommandIntent) (state.RunJournal, bool, error)
	PrepareProviderAction(key state.RunKey, intent state.ProviderActionIntent) (state.RunJournal, bool, error)
	PrepareSupervisorHandoff(key state.RunKey, handoff authority.SupervisorHandoff) (state.RunJournal, error)
	QueueCommandAcknowledgement(key state.RunKey, acknowledgement protocol.CommandAcknowledgement) (state.RunJournal, error)
	QueueGoalEvidenceAndTerminalTransition(key state.RunKey, evidence []protocol.Evidence, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error)
	QueueGoalSessionAttach(key state.RunKey, payload state.GoalSessionAttachDelivery) (state.RunJournal, error)
	QueueGoalSessionStopped(key state.RunKey, payload state.GoalSessionStoppedDelivery) (state.RunJournal, error)
	QueueGoalUsage(key state.RunKey, usage protocol.Usage) (state.RunJournal, error)
	QueueNativeUsageRecovery(key state.RunKey, usage protocol.Usage) (state.RunJournal, error)
	QueueNativeUsageRecoverySettlement(key state.RunKey, usage protocol.Usage, settlement *state.NativeUsageTerminalRecovery) (state.RunJournal, error)
	QueueNextEvent(key state.RunKey, event protocol.RunEvent) (state.RunJournal, error)
	QueueOutputEvent(key state.RunKey, event protocol.RunEvent, budget int) (state.RunJournal, bool, error)
	QueueOutputTruncatedMarker(key state.RunKey, at time.Time, newEventID func() (string, error)) (state.RunJournal, bool, error)
	QueueRunningTransition(key state.RunKey, transition protocol.StateTransitionRequest) (state.RunJournal, error)
	QueueTerminalTransition(key state.RunKey, transition protocol.StateTransitionRequest) (state.RunJournal, error)
	QueueTerminalTransitionAndAcknowledgementAt(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error)
	QueueTerminalTransitionAt(key state.RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error)
	QueueTransition(key state.RunKey, transition protocol.StateTransitionRequest) (state.RunJournal, error)
	QueueWaitingForInput(key state.RunKey, event protocol.RunEvent, transition protocol.StateTransitionRequest) (state.RunJournal, error)
	RebindGoalSessionForResume(key state.GoalSessionKey, rebind state.GoalSessionResumeRebind) (state.GoalSessionJournal, error)
	RecordContainmentStopReceipt(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) (state.RunJournal, error)
	RecordLinuxCloneTargetRemoval(key state.RunKey, fence protocol.Fence, expected authority.LinuxCloneCleanupIntent, proof authority.LinuxCloneTargetRemovalProof) (state.RunJournal, error)
	RecordNativeUsageObservation(key state.RunKey, observation state.NativeUsageObservation) (state.RunJournal, error)
	RecordSupervisorHandoffStopReceipt(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (state.RunJournal, error)
	RecordSupervisorRecoveryScanSeal(key state.RunKey, expected authority.Supervisor, sequence uint64) (state.RunJournal, error)
	RecoverProviderAction(key state.RunKey, actionID string, requestDigest string, failureCode string) (state.RunJournal, error)
	RecoverProviderActions(key state.RunKey, failureCode string) (state.RunJournal, error)
	ResolveGoalSessionUncertainStopped(key state.GoalSessionKey, expected state.GoalSessionCompatibility) (state.GoalSessionJournal, error)
	ResolveTerminal(key state.RunKey, verdict string, resolvedAt time.Time) (state.RunJournal, error)
	ResolveTerminalForCleanup(key state.RunKey, verdict string, resolvedAt time.Time) (state.RunJournal, error)
	RetainWorkspace(key state.RunKey) (state.RunJournal, error)
	RetireGoalDelivery(key state.RunKey, kind state.GoalDeliveryKind, deliveryID string, payloadDigest string, statusCode int, code string, message string, retiredAt time.Time) (state.RunJournal, error)
	RetireLateGoalUsage(key state.RunKey, deliveryID string, payloadDigest string, statusCode int, code string, message string, retiredAt time.Time) (state.LateGoalUsageLedger, error)
	SaveClaimGrant(key state.RunKey, grant protocol.ClaimResponse) (state.RunJournal, error)
	SaveClaimIntent(intent state.ClaimIntent) (state.RunJournal, error)
	SaveEnrollmentIntent(intent state.EnrollmentIntent) error
	SaveGoalSessionLaunchIntent(intent state.GoalSessionLaunchIntent) (state.GoalSessionJournal, error)
	SaveIdentity(identity state.MachineIdentity) error
	SaveJournal(journal state.RunJournal) error
	SetGoalSessionState(key state.GoalSessionKey, sessionState string) (state.GoalSessionJournal, error)
	SetLocalState(key state.RunKey, localState string) (state.RunJournal, error)
	SetProcessDetails(key state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error)
	SetProcessDetailsWithAuthority(key state.RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (state.RunJournal, error)
	SetWorkspacePath(key state.RunKey, path string) (state.RunJournal, error)
}

var _ journalStore = (*state.Store)(nil)
