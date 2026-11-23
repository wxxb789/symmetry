// Package apptest holds test doubles for the daemon app package's real seams.
package apptest

import (
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// FaultStore wraps a real Store and lets one hook replace one write method.
// A nil hook delegates to the embedded Store method with the same arguments.
type FaultStore struct {
	*state.Store

	OnSetWorkspacePath                             func(state.RunKey, string) (state.RunJournal, error)
	OnSetProcessDetails                            func(state.RunKey, int, string, time.Time) (state.RunJournal, error)
	OnSetProcessDetailsWithAuthority               func(state.RunKey, int, string, time.Time, authority.Supervisor) (state.RunJournal, error)
	OnMarkContainmentUnproven                      func(state.RunKey, int, string) (state.RunJournal, error)
	OnClearContainmentUnproven                     func(state.RunKey, int, string) (state.RunJournal, error)
	OnRecordContainmentStopReceipt                 func(state.RunKey, int, string, authority.StopReceipt) (state.RunJournal, error)
	OnPrepareSupervisorHandoff                     func(state.RunKey, authority.SupervisorHandoff) (state.RunJournal, error)
	OnBindSupervisorHandoff                        func(state.RunKey, authority.SupervisorHandoff, int, string) (state.RunJournal, error)
	OnCommitSupervisorHandoff                      func(state.RunKey, authority.SupervisorHandoff, time.Time) (state.RunJournal, error)
	OnRecordSupervisorHandoffStopReceipt           func(state.RunKey, authority.SupervisorHandoff, authority.StopReceipt) (state.RunJournal, error)
	OnRecordSupervisorRecoveryScanSeal             func(state.RunKey, authority.Supervisor, uint64) (state.RunJournal, error)
	OnClearSupervisorHandoff                       func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) (state.RunJournal, error)
	OnClearSupervisorHandoffAfterAbort             func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) (state.RunJournal, error)
	OnQueueTerminalTransitionAt                    func(state.RunKey, protocol.StateTransitionRequest, time.Time) (state.RunJournal, error)
	OnQueueGoalUsage                               func(state.RunKey, protocol.Usage) (state.RunJournal, error)
	OnQueueNativeUsageRecovery                     func(state.RunKey, protocol.Usage) (state.RunJournal, error)
	OnQueueNativeUsageRecoverySettlement           func(state.RunKey, protocol.Usage, *state.NativeUsageTerminalRecovery) (state.RunJournal, error)
	OnMarkGoalSessionAttachDeliveryReady           func(state.RunKey, string) (state.RunJournal, error)
	OnDiscardUnreadyGoalSessionAttachDelivery      func(state.RunKey, string) (state.RunJournal, error)
	OnAbortGoalSessionLaunchBeforeNativeStart      func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	OnMarkGoalSessionUncertain                     func(state.GoalSessionKey, string) (state.GoalSessionJournal, error)
	OnMarkCommandAcknowledgementsDelivered         func(state.RunKey, []string) (state.RunJournal, error)
	OnQueueTerminalTransitionAndAcknowledgementAt  func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
	OnQueueCancelledTransitionAndAcknowledgementAt func(state.RunKey, protocol.StateTransitionRequest, protocol.CommandAcknowledgement, time.Time) (state.RunJournal, error)
	OnRetainWorkspace                              func(state.RunKey) (state.RunJournal, error)
	OnLoadGoalSession                              func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	OnCloseGoalSession                             func(state.GoalSessionKey) (state.GoalSessionJournal, error)
	OnClearProcessDetails                          func(state.RunKey, int, string) (state.RunJournal, error)
}

// NewFaultStore opens a real Store over directory and wraps it.
func NewFaultStore(directory string) (*FaultStore, error) {
	store, err := state.New(directory)
	if err != nil {
		return nil, err
	}
	return NewFaultStoreFrom(store), nil
}

// NewFaultStoreFrom wraps an already open Store.
func NewFaultStoreFrom(store *state.Store) *FaultStore {
	return &FaultStore{Store: store}
}

func (store *FaultStore) SetWorkspacePath(key state.RunKey, path string) (state.RunJournal, error) {
	if store.OnSetWorkspacePath != nil {
		return store.OnSetWorkspacePath(key, path)
	}
	return store.Store.SetWorkspacePath(key, path)
}

func (store *FaultStore) SetProcessDetails(key state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error) {
	if store.OnSetProcessDetails != nil {
		return store.OnSetProcessDetails(key, pid, identity, startedAt)
	}
	return store.Store.SetProcessDetails(key, pid, identity, startedAt)
}

func (store *FaultStore) SetProcessDetailsWithAuthority(key state.RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (state.RunJournal, error) {
	if store.OnSetProcessDetailsWithAuthority != nil {
		return store.OnSetProcessDetailsWithAuthority(key, pid, identity, startedAt, value)
	}
	return store.Store.SetProcessDetailsWithAuthority(key, pid, identity, startedAt, value)
}

func (store *FaultStore) MarkContainmentUnproven(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
	if store.OnMarkContainmentUnproven != nil {
		return store.OnMarkContainmentUnproven(key, pid, identity)
	}
	return store.Store.MarkContainmentUnproven(key, pid, identity)
}

func (store *FaultStore) ClearContainmentUnproven(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
	if store.OnClearContainmentUnproven != nil {
		return store.OnClearContainmentUnproven(key, pid, identity)
	}
	return store.Store.ClearContainmentUnproven(key, pid, identity)
}

func (store *FaultStore) RecordContainmentStopReceipt(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) (state.RunJournal, error) {
	if store.OnRecordContainmentStopReceipt != nil {
		return store.OnRecordContainmentStopReceipt(key, pid, identity, receipt)
	}
	return store.Store.RecordContainmentStopReceipt(key, pid, identity, receipt)
}

func (store *FaultStore) PrepareSupervisorHandoff(key state.RunKey, handoff authority.SupervisorHandoff) (state.RunJournal, error) {
	if store.OnPrepareSupervisorHandoff != nil {
		return store.OnPrepareSupervisorHandoff(key, handoff)
	}
	return store.Store.PrepareSupervisorHandoff(key, handoff)
}

func (store *FaultStore) BindSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, pid int, identity string) (state.RunJournal, error) {
	if store.OnBindSupervisorHandoff != nil {
		return store.OnBindSupervisorHandoff(key, expected, pid, identity)
	}
	return store.Store.BindSupervisorHandoff(key, expected, pid, identity)
}

func (store *FaultStore) CommitSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (state.RunJournal, error) {
	if store.OnCommitSupervisorHandoff != nil {
		return store.OnCommitSupervisorHandoff(key, expected, startedAt)
	}
	return store.Store.CommitSupervisorHandoff(key, expected, startedAt)
}

func (store *FaultStore) RecordSupervisorHandoffStopReceipt(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (state.RunJournal, error) {
	if store.OnRecordSupervisorHandoffStopReceipt != nil {
		return store.OnRecordSupervisorHandoffStopReceipt(key, expected, receipt)
	}
	return store.Store.RecordSupervisorHandoffStopReceipt(key, expected, receipt)
}

func (store *FaultStore) RecordSupervisorRecoveryScanSeal(key state.RunKey, expected authority.Supervisor, sequence uint64) (state.RunJournal, error) {
	if store.OnRecordSupervisorRecoveryScanSeal != nil {
		return store.OnRecordSupervisorRecoveryScanSeal(key, expected, sequence)
	}
	return store.Store.RecordSupervisorRecoveryScanSeal(key, expected, sequence)
}

func (store *FaultStore) ClearSupervisorHandoff(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
	if store.OnClearSupervisorHandoff != nil {
		return store.OnClearSupervisorHandoff(key, expected, proof)
	}
	return store.Store.ClearSupervisorHandoff(key, expected, proof)
}

func (store *FaultStore) ClearSupervisorHandoffAfterAbort(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
	if store.OnClearSupervisorHandoffAfterAbort != nil {
		return store.OnClearSupervisorHandoffAfterAbort(key, expected, proof)
	}
	return store.Store.ClearSupervisorHandoffAfterAbort(key, expected, proof)
}

func (store *FaultStore) QueueTerminalTransitionAt(key state.RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error) {
	if store.OnQueueTerminalTransitionAt != nil {
		return store.OnQueueTerminalTransitionAt(key, transition, pendingAt)
	}
	return store.Store.QueueTerminalTransitionAt(key, transition, pendingAt)
}

func (store *FaultStore) QueueGoalUsage(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
	if store.OnQueueGoalUsage != nil {
		return store.OnQueueGoalUsage(key, usage)
	}
	return store.Store.QueueGoalUsage(key, usage)
}

func (store *FaultStore) QueueNativeUsageRecovery(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
	if store.OnQueueNativeUsageRecovery != nil {
		return store.OnQueueNativeUsageRecovery(key, usage)
	}
	return store.Store.QueueNativeUsageRecovery(key, usage)
}

func (store *FaultStore) QueueNativeUsageRecoverySettlement(key state.RunKey, usage protocol.Usage, settlement *state.NativeUsageTerminalRecovery) (state.RunJournal, error) {
	if store.OnQueueNativeUsageRecoverySettlement != nil {
		return store.OnQueueNativeUsageRecoverySettlement(key, usage, settlement)
	}
	return store.Store.QueueNativeUsageRecoverySettlement(key, usage, settlement)
}

func (store *FaultStore) MarkGoalSessionAttachDeliveryReady(key state.RunKey, localHandleID string) (state.RunJournal, error) {
	if store.OnMarkGoalSessionAttachDeliveryReady != nil {
		return store.OnMarkGoalSessionAttachDeliveryReady(key, localHandleID)
	}
	return store.Store.MarkGoalSessionAttachDeliveryReady(key, localHandleID)
}

func (store *FaultStore) DiscardUnreadyGoalSessionAttachDelivery(key state.RunKey, localHandleID string) (state.RunJournal, error) {
	if store.OnDiscardUnreadyGoalSessionAttachDelivery != nil {
		return store.OnDiscardUnreadyGoalSessionAttachDelivery(key, localHandleID)
	}
	return store.Store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID)
}

func (store *FaultStore) AbortGoalSessionLaunchBeforeNativeStart(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
	if store.OnAbortGoalSessionLaunchBeforeNativeStart != nil {
		return store.OnAbortGoalSessionLaunchBeforeNativeStart(key)
	}
	return store.Store.AbortGoalSessionLaunchBeforeNativeStart(key)
}

func (store *FaultStore) MarkGoalSessionUncertain(key state.GoalSessionKey, reason string) (state.GoalSessionJournal, error) {
	if store.OnMarkGoalSessionUncertain != nil {
		return store.OnMarkGoalSessionUncertain(key, reason)
	}
	return store.Store.MarkGoalSessionUncertain(key, reason)
}

func (store *FaultStore) MarkCommandAcknowledgementsDelivered(key state.RunKey, acknowledgementIDs []string) (state.RunJournal, error) {
	if store.OnMarkCommandAcknowledgementsDelivered != nil {
		return store.OnMarkCommandAcknowledgementsDelivered(key, acknowledgementIDs)
	}
	return store.Store.MarkCommandAcknowledgementsDelivered(key, acknowledgementIDs)
}

func (store *FaultStore) QueueTerminalTransitionAndAcknowledgementAt(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
	if store.OnQueueTerminalTransitionAndAcknowledgementAt != nil {
		return store.OnQueueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt)
	}
	return store.Store.QueueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt)
}

func (store *FaultStore) QueueCancelledTransitionAndAcknowledgementAt(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
	if store.OnQueueCancelledTransitionAndAcknowledgementAt != nil {
		return store.OnQueueCancelledTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt)
	}
	return store.Store.QueueCancelledTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt)
}

func (store *FaultStore) RetainWorkspace(key state.RunKey) (state.RunJournal, error) {
	if store.OnRetainWorkspace != nil {
		return store.OnRetainWorkspace(key)
	}
	return store.Store.RetainWorkspace(key)
}

func (store *FaultStore) LoadGoalSession(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
	if store.OnLoadGoalSession != nil {
		return store.OnLoadGoalSession(key)
	}
	return store.Store.LoadGoalSession(key)
}

func (store *FaultStore) CloseGoalSession(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
	if store.OnCloseGoalSession != nil {
		return store.OnCloseGoalSession(key)
	}
	return store.Store.CloseGoalSession(key)
}

func (store *FaultStore) ClearProcessDetails(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
	if store.OnClearProcessDetails != nil {
		return store.OnClearProcessDetails(key, pid, identity)
	}
	return store.Store.ClearProcessDetails(key, pid, identity)
}
