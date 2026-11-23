package apptest

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

var errFaultStoreHook = errors.New("fault hook")

type hookRecorder struct {
	calls map[string]int
	args  map[string][]any
}

func newHookRecorder() *hookRecorder {
	return &hookRecorder{calls: make(map[string]int), args: make(map[string][]any)}
}

func (recorder *hookRecorder) replaceRun(name string, args ...any) (state.RunJournal, error) {
	recorder.calls[name]++
	recorder.args[name] = args
	return state.RunJournal{}, errFaultStoreHook
}

func (recorder *hookRecorder) replaceGoalSession(name string, args ...any) (state.GoalSessionJournal, error) {
	recorder.calls[name]++
	recorder.args[name] = args
	return state.GoalSessionJournal{}, errFaultStoreHook
}

func (recorder *hookRecorder) total() int {
	total := 0
	for _, count := range recorder.calls {
		total += count
	}
	return total
}

type faultStoreWriteCase struct {
	name     string
	seed     func(*FaultStore) error
	hook     func(*FaultStore, *hookRecorder)
	call     func(*FaultStore) (any, error)
	wantArgs []any
}

const (
	faultStoreWriteRunID    = "00000000-0000-4000-8000-000000000001"
	faultStoreWritePID      = 42
	faultStoreWriteIdentity = "windows:42:created-at"
)

func faultStoreWriteKey() state.RunKey {
	return state.RunKey{RunID: faultStoreWriteRunID, Generation: 1}
}

func faultStoreWriteStartedAt() time.Time {
	return time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
}

func faultStoreWriteUsage() protocol.Usage {
	return protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion, UsageID: "00000000-0000-4000-8000-000000000010",
		RunID: faultStoreWriteRunID, UsageKey: "native-final", Provider: "openai", Model: "gpt-test",
		CostBasis: protocol.CostUnknown, ObservedAt: "2026-09-09T01:00:00Z",
	}
}

func faultStoreWriteSupervisor() authority.Supervisor {
	return authority.Supervisor{
		Version: authority.SupervisorVersion, Secret: strings.Repeat("a", authority.SecretBytes*2),
		TargetPID: faultStoreWritePID, TargetIdentity: faultStoreWriteIdentity,
		PipeToken: strings.Repeat("b", authority.TokenBytes*2), JobID: strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID: 43, SupervisorIdentity: "windows:43:created-at",
	}
}

func faultStoreWriteHandoff() authority.SupervisorHandoff {
	creator := uint32(1)
	return authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Secret: strings.Repeat("b", authority.SecretBytes*2), TargetPID: faultStoreWritePID, TargetIdentity: faultStoreWriteIdentity,
		PipeToken: strings.Repeat("c", authority.TokenBytes*2), JobID: strings.Repeat("d", authority.TokenBytes*2),
		SupervisorPID: 43, SupervisorIdentity: "windows:43:created-at", CreatorSessionID: &creator,
	}
}

func faultStoreWriteStopReceipt() authority.StopReceipt {
	handoff := faultStoreWriteHandoff()
	return authority.StopReceipt{
		Version: authority.SupervisorVersion, Status: "stopped", TargetPID: faultStoreWritePID, TargetIdentity: faultStoreWriteIdentity,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID, SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
	}
}

func faultStoreWriteReleaseProof() authority.SupervisorHandoffReleaseProof {
	handoff := faultStoreWriteHandoff()
	return authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorVersion, Status: "stopped", LaunchToken: handoff.LaunchToken,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID, SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
	}
}

func faultStoreWriteAbortProof() authority.SupervisorHandoffAbortProof {
	handoff := faultStoreWriteHandoff()
	return authority.SupervisorHandoffAbortProof{
		Version: authority.SupervisorVersion, Disposition: "aborted", LaunchToken: handoff.LaunchToken, JobID: handoff.JobID,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
		SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
	}
}

func faultStoreWriteTransition(stateName string) protocol.StateTransitionRequest {
	return protocol.StateTransitionRequest{TransitionID: "transition-1", State: stateName, Payload: json.RawMessage(`{}`)}
}

func faultStoreWriteAcknowledgement() protocol.CommandAcknowledgement {
	key := faultStoreWriteKey()
	return protocol.CommandAcknowledgement{
		Fence: protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease-1"},
		RunID: key.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1",
	}
}

func faultStoreWriteGoalSessionIntent() state.GoalSessionLaunchIntent {
	return state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: "00000000-0000-4000-8000-000000000002",
		GoalRevision: 1, WorkItemID: "00000000-0000-4000-8000-000000000006", TaskID: "task-1",
		RunID: faultStoreWriteRunID, Generation: 1, AdmissionID: "00000000-0000-4000-8000-000000000005",
		LocalHandleID: "00000000-0000-4000-8000-000000000003", BindingID: "00000000-0000-4000-8000-000000000004",
		RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: "codex", HarnessVersion: "0.168.0",
		AdapterVersion: "symmetry-daemon:test", AdapterProtocolVersion: 1,
		WorkspaceFingerprint: "sha256:" + strings.Repeat("a", 64), WorkspacePath: "worktree-path",
		SessionMode: state.GoalSessionModeFresh,
	}
}

func faultStoreWriteAttachDelivery() state.GoalSessionAttachDelivery {
	intent := faultStoreWriteGoalSessionIntent()
	return state.GoalSessionAttachDelivery{
		GoalID: intent.GoalID, LocalHandleID: intent.LocalHandleID, BindingID: intent.BindingID,
		HarnessKind: "codex", HarnessVersion: "0.168.0", AdapterVersion: "symmetry-daemon:test",
		WorkspaceFingerprint: intent.WorkspaceFingerprint, Workspace: "worktree-path",
	}
}

func faultStoreWriteSeedJournal(store *FaultStore) error {
	return store.SaveJournal(faultStoreTestJournal(faultStoreWriteRunID, 1))
}

func faultStoreWriteSeedClaim(store *FaultStore) error {
	journal := faultStoreTestJournal(faultStoreWriteRunID, 1)
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	return store.SaveJournal(journal)
}

func faultStoreWriteSeedHandoff(store *FaultStore) error {
	if err := faultStoreWriteSeedClaim(store); err != nil {
		return err
	}
	_, err := store.PrepareSupervisorHandoff(faultStoreWriteKey(), faultStoreWriteHandoff())
	return err
}

func faultStoreWriteSeedHandoffBound(store *FaultStore) error {
	if err := faultStoreWriteSeedHandoff(store); err != nil {
		return err
	}
	handoff := faultStoreWriteHandoff()
	_, err := store.BindSupervisorHandoff(faultStoreWriteKey(), handoff, handoff.SupervisorPID, handoff.SupervisorIdentity)
	return err
}

func faultStoreWriteSeedGoalSession(store *FaultStore) error {
	_, err := store.SaveGoalSessionLaunchIntent(faultStoreWriteGoalSessionIntent())
	return err
}

func faultStoreWriteSeedGoalSessionStarted(store *FaultStore) error {
	if err := faultStoreWriteSeedGoalSession(store); err != nil {
		return err
	}
	key := faultStoreWriteGoalSessionIntent().Key()
	if _, err := store.MarkGoalSessionLaunchStarted(key); err != nil {
		return err
	}
	_, err := store.PersistGoalSessionHandle(key, state.GoalSessionHandle{NativeSessionID: "native-session-1"})
	return err
}

func faultStoreWriteSeedAttachDelivery(store *FaultStore) error {
	if err := faultStoreWriteSeedJournal(store); err != nil {
		return err
	}
	_, err := store.QueueGoalSessionAttach(faultStoreWriteKey(), faultStoreWriteAttachDelivery())
	return err
}

func faultStoreWriteCases() []faultStoreWriteCase {
	return []faultStoreWriteCase{
		{
			name: "SetWorkspacePath",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetWorkspacePath = func(key state.RunKey, path string) (state.RunJournal, error) {
					return recorder.replaceRun("SetWorkspacePath", key, path)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetWorkspacePath(faultStoreWriteKey(), "delegated-workspace")
			},
			wantArgs: []any{faultStoreWriteKey(), "delegated-workspace"},
		},
		{
			name: "SetProcessDetails",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetProcessDetails = func(key state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("SetProcessDetails", key, pid, identity, startedAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetProcessDetails(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStartedAt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStartedAt()},
		},
		{
			name: "SetProcessDetailsWithAuthority",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnSetProcessDetailsWithAuthority = func(key state.RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (state.RunJournal, error) {
					return recorder.replaceRun("SetProcessDetailsWithAuthority", key, pid, identity, startedAt, value)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.SetProcessDetailsWithAuthority(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStartedAt(), faultStoreWriteSupervisor())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStartedAt(), faultStoreWriteSupervisor()},
		},
		{
			name: "MarkContainmentUnproven",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkContainmentUnproven = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkContainmentUnproven", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkContainmentUnproven(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity},
		},
		{
			name: "ClearContainmentUnproven",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearContainmentUnproven = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("ClearContainmentUnproven", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearContainmentUnproven(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity},
		},
		{
			name: "RecordContainmentStopReceipt",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRecordContainmentStopReceipt = func(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) (state.RunJournal, error) {
					return recorder.replaceRun("RecordContainmentStopReceipt", key, pid, identity, receipt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RecordContainmentStopReceipt(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStopReceipt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity, faultStoreWriteStopReceipt()},
		},
		{
			name: "PrepareSupervisorHandoff",
			seed: faultStoreWriteSeedClaim,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnPrepareSupervisorHandoff = func(key state.RunKey, handoff authority.SupervisorHandoff) (state.RunJournal, error) {
					return recorder.replaceRun("PrepareSupervisorHandoff", key, handoff)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.PrepareSupervisorHandoff(faultStoreWriteKey(), faultStoreWriteHandoff())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff()},
		},
		{
			name: "BindSupervisorHandoff",
			seed: faultStoreWriteSeedHandoff,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnBindSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("BindSupervisorHandoff", key, expected, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				handoff := faultStoreWriteHandoff()
				return store.BindSupervisorHandoff(faultStoreWriteKey(), handoff, handoff.SupervisorPID, handoff.SupervisorIdentity)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteHandoff().SupervisorPID, faultStoreWriteHandoff().SupervisorIdentity},
		},
		{
			name: "CommitSupervisorHandoff",
			seed: faultStoreWriteSeedHandoffBound,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnCommitSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("CommitSupervisorHandoff", key, expected, startedAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.CommitSupervisorHandoff(faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteStartedAt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteStartedAt()},
		},
		{
			name: "RecordSupervisorHandoffStopReceipt",
			seed: faultStoreWriteSeedHandoffBound,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRecordSupervisorHandoffStopReceipt = func(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (state.RunJournal, error) {
					return recorder.replaceRun("RecordSupervisorHandoffStopReceipt", key, expected, receipt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RecordSupervisorHandoffStopReceipt(faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteStopReceipt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteStopReceipt()},
		},
		{
			name: "ClearSupervisorHandoff",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearSupervisorHandoff = func(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
					return recorder.replaceRun("ClearSupervisorHandoff", key, expected, proof)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearSupervisorHandoff(faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteReleaseProof())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteReleaseProof()},
		},
		{
			name: "ClearSupervisorHandoffAfterAbort",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearSupervisorHandoffAfterAbort = func(key state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
					return recorder.replaceRun("ClearSupervisorHandoffAfterAbort", key, expected, proof)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearSupervisorHandoffAfterAbort(faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteAbortProof())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteHandoff(), faultStoreWriteAbortProof()},
		},
		{
			name: "QueueTerminalTransitionAt",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueTerminalTransitionAt = func(key state.RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueTerminalTransitionAt", key, transition, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueTerminalTransitionAt(faultStoreWriteKey(), faultStoreWriteTransition("completed"), faultStoreWriteStartedAt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteTransition("completed"), faultStoreWriteStartedAt()},
		},
		{
			name: "QueueGoalUsage",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueGoalUsage = func(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
					return recorder.replaceRun("QueueGoalUsage", key, usage)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueGoalUsage(faultStoreWriteKey(), faultStoreWriteUsage())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteUsage()},
		},
		{
			name: "QueueNativeUsageRecovery",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueNativeUsageRecovery = func(key state.RunKey, usage protocol.Usage) (state.RunJournal, error) {
					return recorder.replaceRun("QueueNativeUsageRecovery", key, usage)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueNativeUsageRecovery(faultStoreWriteKey(), faultStoreWriteUsage())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteUsage()},
		},
		{
			name: "QueueNativeUsageRecoverySettlement",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueNativeUsageRecoverySettlement = func(key state.RunKey, usage protocol.Usage, settlement *state.NativeUsageTerminalRecovery) (state.RunJournal, error) {
					return recorder.replaceRun("QueueNativeUsageRecoverySettlement", key, usage, settlement)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueNativeUsageRecoverySettlement(faultStoreWriteKey(), faultStoreWriteUsage(), nil)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteUsage(), (*state.NativeUsageTerminalRecovery)(nil)},
		},
		{
			name: "MarkGoalSessionAttachDeliveryReady",
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkGoalSessionAttachDeliveryReady = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkGoalSessionAttachDeliveryReady", key, localHandleID)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkGoalSessionAttachDeliveryReady(faultStoreWriteKey(), faultStoreWriteAttachDelivery().LocalHandleID)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteAttachDelivery().LocalHandleID},
		},
		{
			name: "DiscardUnreadyGoalSessionAttachDelivery",
			seed: faultStoreWriteSeedAttachDelivery,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnDiscardUnreadyGoalSessionAttachDelivery = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
					return recorder.replaceRun("DiscardUnreadyGoalSessionAttachDelivery", key, localHandleID)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.DiscardUnreadyGoalSessionAttachDelivery(faultStoreWriteKey(), faultStoreWriteAttachDelivery().LocalHandleID)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteAttachDelivery().LocalHandleID},
		},
		{
			name: "AbortGoalSessionLaunchBeforeNativeStart",
			seed: faultStoreWriteSeedGoalSession,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnAbortGoalSessionLaunchBeforeNativeStart = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("AbortGoalSessionLaunchBeforeNativeStart", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.AbortGoalSessionLaunchBeforeNativeStart(faultStoreWriteGoalSessionIntent().Key())
			},
			wantArgs: []any{faultStoreWriteGoalSessionIntent().Key()},
		},
		{
			name: "MarkGoalSessionUncertain",
			seed: faultStoreWriteSeedGoalSession,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkGoalSessionUncertain = func(key state.GoalSessionKey, reason string) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("MarkGoalSessionUncertain", key, reason)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkGoalSessionUncertain(faultStoreWriteGoalSessionIntent().Key(), "uncertain reason")
			},
			wantArgs: []any{faultStoreWriteGoalSessionIntent().Key(), "uncertain reason"},
		},
		{
			name: "MarkCommandAcknowledgementsDelivered",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnMarkCommandAcknowledgementsDelivered = func(key state.RunKey, acknowledgementIDs []string) (state.RunJournal, error) {
					return recorder.replaceRun("MarkCommandAcknowledgementsDelivered", key, acknowledgementIDs)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.MarkCommandAcknowledgementsDelivered(faultStoreWriteKey(), []string{"ack-1"})
			},
			wantArgs: []any{faultStoreWriteKey(), []string{"ack-1"}},
		},
		{
			name: "QueueTerminalTransitionAndAcknowledgementAt",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueTerminalTransitionAndAcknowledgementAt = func(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueTerminalTransitionAndAcknowledgementAt", key, transition, acknowledgement, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueTerminalTransitionAndAcknowledgementAt(faultStoreWriteKey(), faultStoreWriteTransition("completed"), faultStoreWriteAcknowledgement(), faultStoreWriteStartedAt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteTransition("completed"), faultStoreWriteAcknowledgement(), faultStoreWriteStartedAt()},
		},
		{
			name: "QueueCancelledTransitionAndAcknowledgementAt",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnQueueCancelledTransitionAndAcknowledgementAt = func(key state.RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (state.RunJournal, error) {
					return recorder.replaceRun("QueueCancelledTransitionAndAcknowledgementAt", key, transition, acknowledgement, pendingAt)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.QueueCancelledTransitionAndAcknowledgementAt(faultStoreWriteKey(), faultStoreWriteTransition("cancelled"), faultStoreWriteAcknowledgement(), faultStoreWriteStartedAt())
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWriteTransition("cancelled"), faultStoreWriteAcknowledgement(), faultStoreWriteStartedAt()},
		},
		{
			name: "RetainWorkspace",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnRetainWorkspace = func(key state.RunKey) (state.RunJournal, error) {
					return recorder.replaceRun("RetainWorkspace", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.RetainWorkspace(faultStoreWriteKey())
			},
			wantArgs: []any{faultStoreWriteKey()},
		},
		{
			name: "LoadGoalSession",
			seed: faultStoreWriteSeedGoalSession,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnLoadGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("LoadGoalSession", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.LoadGoalSession(faultStoreWriteGoalSessionIntent().Key())
			},
			wantArgs: []any{faultStoreWriteGoalSessionIntent().Key()},
		},
		{
			name: "CloseGoalSession",
			seed: faultStoreWriteSeedGoalSessionStarted,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnCloseGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					return recorder.replaceGoalSession("CloseGoalSession", key)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.CloseGoalSession(faultStoreWriteGoalSessionIntent().Key())
			},
			wantArgs: []any{faultStoreWriteGoalSessionIntent().Key()},
		},
		{
			name: "ClearProcessDetails",
			seed: faultStoreWriteSeedJournal,
			hook: func(store *FaultStore, recorder *hookRecorder) {
				store.OnClearProcessDetails = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					return recorder.replaceRun("ClearProcessDetails", key, pid, identity)
				}
			},
			call: func(store *FaultStore) (any, error) {
				return store.ClearProcessDetails(faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity)
			},
			wantArgs: []any{faultStoreWriteKey(), faultStoreWritePID, faultStoreWriteIdentity},
		},
	}
}

func TestFaultStoreHookReplacesEveryWrite(t *testing.T) {
	cases := faultStoreWriteCases()
	if len(cases) != 27 {
		t.Fatalf("write case count = %d, want 27", len(cases))
	}
	seen := make(map[string]struct{}, len(cases))
	for _, testCase := range cases {
		if _, exists := seen[testCase.name]; exists {
			t.Fatalf("duplicate write case %q", testCase.name)
		}
		seen[testCase.name] = struct{}{}
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewFaultStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewFaultStore() error = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			recorder := newHookRecorder()
			testCase.hook(store, recorder)
			if _, err := testCase.call(store); !errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want %v", testCase.name, err, errFaultStoreHook)
			}
			if count := recorder.calls[testCase.name]; count != 1 {
				t.Fatalf("%s hook ran %d times, want 1", testCase.name, count)
			}
			if args := recorder.args[testCase.name]; !reflect.DeepEqual(args, testCase.wantArgs) {
				t.Fatalf("%s hook arguments = %#v, want %#v", testCase.name, args, testCase.wantArgs)
			}
		})
	}
}

func TestFaultStoreNilHookDelegatesEveryWrite(t *testing.T) {
	cases := faultStoreWriteCases()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := openFaultStore(t)
			if testCase.seed != nil {
				if err := testCase.seed(store); err != nil {
					t.Fatalf("%s() seed error = %v", testCase.name, err)
				}
			}
			recorder := newHookRecorder()
			for _, other := range cases {
				if other.name != testCase.name {
					other.hook(store, recorder)
				}
			}
			journal, err := testCase.call(store)
			if errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want the embedded store result", testCase.name, err)
			}
			if testCase.seed != nil {
				if err != nil {
					t.Fatalf("%s() error = %v over a seeded fixture, want nil", testCase.name, err)
				}
				if reflect.DeepEqual(journal, reflect.Zero(reflect.TypeOf(journal)).Interface()) {
					t.Fatalf("%s() journal = %#v over a seeded fixture, want the store's non-zero journal", testCase.name, journal)
				}
			}
			base := openFaultStore(t)
			if testCase.seed != nil {
				if err := testCase.seed(base); err != nil {
					t.Fatalf("%s() base seed error = %v", testCase.name, err)
				}
			}
			directJournal, directErr := directFaultStoreCall(t, base.Store, testCase)
			if !faultStoreDelegatedJournalEqual(journal, directJournal) {
				t.Fatalf("%s() journal = %#v, want embedded store journal %#v", testCase.name, journal, directJournal)
			}
			if errText, directText := faultStoreErrorText(err), faultStoreErrorText(directErr); errText != directText {
				t.Fatalf("%s() error = %v, want embedded store error %v", testCase.name, err, directErr)
			}
			if count := recorder.total(); count != 0 {
				t.Fatalf("%s() invoked %d hook(s) with no hook set, want 0", testCase.name, count)
			}
		})
	}
}

func openFaultStore(t *testing.T) *FaultStore {
	t.Helper()
	store, err := NewFaultStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFaultStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func faultStoreDelegatedJournalEqual(fault, direct any) bool {
	return reflect.DeepEqual(faultStoreWithoutSeedTimestamps(fault), faultStoreWithoutSeedTimestamps(direct))
}

func faultStoreWithoutSeedTimestamps(value any) any {
	journal, ok := value.(state.GoalSessionJournal)
	if !ok {
		return value
	}
	journal.CreatedAt, journal.UpdatedAt, journal.LaunchAttemptedAt = time.Time{}, time.Time{}, time.Time{}
	return journal
}

func TestFaultStoreDelegatesToTheEmbeddedStore(t *testing.T) {
	base, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("state.New() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	seed := faultStoreTestJournal("delegation-run", 1)
	if err := base.SaveJournal(seed); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	store := NewFaultStoreFrom(base)
	if store.Store != base {
		t.Fatalf("NewFaultStoreFrom() embedded store = %p, want %p", store.Store, base)
	}
	path := "delegated-workspace"
	got, err := store.SetWorkspacePath(seed.Key(), path)
	if err != nil {
		t.Fatalf("SetWorkspacePath() error = %v", err)
	}
	if got.WorkspacePath != path {
		t.Fatalf("SetWorkspacePath() workspace path = %q, want %q", got.WorkspacePath, path)
	}
	want, err := base.LoadJournal(seed.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if want.WorkspacePath != path {
		t.Fatalf("persisted workspace path = %q, want %q", want.WorkspacePath, path)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetWorkspacePath() journal = %#v, want persisted journal %#v", got, want)
	}
	again, err := store.LoadJournal(seed.Key())
	if err != nil {
		t.Fatalf("LoadJournal() through FaultStore error = %v", err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("FaultStore journal = %#v, want persisted journal %#v", again, want)
	}
}

func TestFaultStoreHooksAreIndependent(t *testing.T) {
	cases := faultStoreWriteCases()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewFaultStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewFaultStore() error = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			recorder := newHookRecorder()
			testCase.hook(store, recorder)
			if _, err := testCase.call(store); !errors.Is(err, errFaultStoreHook) {
				t.Fatalf("%s() error = %v, want %v", testCase.name, err, errFaultStoreHook)
			}
			for _, other := range cases {
				if other.name == testCase.name {
					continue
				}
				if _, err := other.call(store); errors.Is(err, errFaultStoreHook) {
					t.Fatalf("%s() with only the %s hook set returned %v", other.name, testCase.name, err)
				}
			}
			if count := recorder.total(); count != 1 {
				t.Fatalf("%s hook ran %d times, want 1", testCase.name, count)
			}
		})
	}
}

func directFaultStoreCall(t *testing.T, store *state.Store, testCase faultStoreWriteCase) (any, error) {
	t.Helper()
	method := reflect.ValueOf(store).MethodByName(testCase.name)
	if !method.IsValid() {
		t.Fatalf("state.Store has no method %q", testCase.name)
	}
	args := make([]reflect.Value, 0, len(testCase.wantArgs))
	for _, arg := range testCase.wantArgs {
		args = append(args, reflect.ValueOf(arg))
	}
	results := method.Call(args)
	if len(results) != 2 {
		t.Fatalf("%s returned %d values, want 2", testCase.name, len(results))
	}
	journal := results[0].Interface()
	if results[1].IsNil() {
		return journal, nil
	}
	return journal, results[1].Interface().(error)
}

func faultStoreErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func faultStoreTestJournal(runID string, generation int64) state.RunJournal {
	return state.RunJournal{
		RunID: runID, Generation: generation, RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 3,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC),
		LocalState:    "running",
		Work:          protocol.Work{Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{}`)},
		WorkspacePath: "seed-workspace", WorkspaceBindingKey: "binding-1",
		PID: 42, ProcessIdentity: "windows:42:created-at", StartedAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC),
		LastEventSequence: 1,
	}
}
