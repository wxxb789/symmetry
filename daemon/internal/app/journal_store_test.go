package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const journalStoreBaseMethods = `AbortGoalSessionLaunchBeforeNativeStart
AbortLinuxCloneReuseBeforeNativeStart
AdvanceLeaseExpiry
BeginLinuxCloneCleanup
BindLinuxCloneReuse
BindSupervisorHandoff
CheckLinuxV2NativeAdmission
CheckLinuxV2NativeAdmissionBeforeClaim
ClearContainmentUnproven
ClearNativeUsageRecoveryRequired
ClearProcessDetails
ClearSupervisorHandoff
ClearSupervisorHandoffAfterAbort
Close
CloseGoalSession
CommitSupervisorHandoff
CompleteControlCommand
CompleteLinuxCloneCleanup
CompleteProvideInput
CompleteProviderAction
DeleteEnrollmentIntent
DeleteJournal
DiscardUnreadyGoalSessionAttachDelivery
EnterCleanupPending
ListGoalSessions
ListJournals
ListLateGoalUsage
LoadEnrollmentIntent
LoadGoalSession
LoadGoalSessionByControlSessionID
LoadGoalSessionByHandle
LoadIdentity
LoadJournal
MarkCommandAcknowledgementsDelivered
MarkContainmentUnproven
MarkEventsDelivered
MarkEventsDeliveredAndQueueOutputTruncatedMarker
MarkGoalDeliveryDelivered
MarkGoalSessionAttachDeliveryReady
MarkGoalSessionAvailable
MarkGoalSessionLaunchStarted
MarkGoalSessionResumeNativeStopped
MarkGoalSessionResumeRejected
MarkGoalSessionResumeStartAttempted
MarkGoalSessionStoppedPending
MarkGoalSessionUncertain
MarkLateGoalUsageDelivered
MarkNativeUsageRecoveryRequired
MarkNativeUsageRecoverySettlement
MarkTransitionAttempted
MarkTransitionsDelivered
MarkWorkspaceRecoveryRequired
PersistGoalSessionControlAttachment
PersistGoalSessionHandle
PrepareControlCommand
PrepareProvideInput
PrepareProviderAction
PrepareSupervisorHandoff
QueueCommandAcknowledgement
QueueGoalEvidenceAndTerminalTransition
QueueGoalSessionAttach
QueueGoalSessionStopped
QueueGoalUsage
QueueNativeUsageRecovery
QueueNativeUsageRecoverySettlement
QueueNextEvent
QueueOutputEvent
QueueOutputTruncatedMarker
QueueRunningTransition
QueueTerminalTransition
QueueTerminalTransitionAndAcknowledgementAt
QueueTerminalTransitionAt
QueueTransition
QueueWaitingForInput
RebindGoalSessionForResume
RecordContainmentStopReceipt
RecordLinuxCloneTargetRemoval
RecordNativeUsageObservation
RecordSupervisorHandoffStopReceipt
RecordSupervisorRecoveryScanSeal
RecoverProviderAction
RecoverProviderActions
ResolveGoalSessionUncertainStopped
ResolveTerminal
ResolveTerminalForCleanup
RetainWorkspace
RetireGoalDelivery
RetireLateGoalUsage
SaveClaimGrant
SaveClaimIntent
SaveEnrollmentIntent
SaveGoalSessionLaunchIntent
SaveIdentity
SaveJournal
SetGoalSessionState
SetLocalState
SetProcessDetails
SetProcessDetailsWithAuthority
SetWorkspacePath`

func TestJournalStoreBaseMethodSetIsExact(t *testing.T) {
	assertMethodSet(t, "journalStoreBase",
		interfaceMethodNames(reflect.TypeOf((*journalStoreBase)(nil)).Elem()),
		strings.Split(journalStoreBaseMethods, "\n"))
}

func assertMethodSet(t *testing.T, label string, got, want []string) {
	if !slices.Equal(got, want) {
		t.Fatalf("%s members = %s\nwant %s\nadded %s\nremoved %s", label, got, want, difference(got, want), difference(want, got))
	}
}

func interfaceMethodNames(value reflect.Type) []string {
	names := make([]string, 0, value.NumMethod())
	for index := range value.NumMethod() {
		names = append(names, value.Method(index).Name)
	}
	slices.Sort(names)
	return names
}

func difference(left, right []string) []string {
	only := make([]string, 0, len(left))
	for _, value := range left {
		if !slices.Contains(right, value) {
			only = append(only, value)
		}
	}
	return only
}

// TestStorelessDaemonTakesEveryStoreNilBranch pins every daemon.store == nil branch to its
// base-revision outcome for a daemon built without a Store: a typed nil must not skip a branch.
func TestStorelessDaemonTakesEveryStoreNilBranch(t *testing.T) {
	key := state.RunKey{RunID: "run", Generation: 1}
	probes := []storelessProbe{
		{"app.go:524 injected absent store stays untyped nil", func(t *testing.T, daemon *daemon) {
			settings := options{}
			withStore(nil)(&settings)
			if settings.store != nil {
				t.Fatalf("injected absent store = %#v, want an untyped nil", settings.store)
			}
		}},
		{"app.go:708 close skips the store Close", func(t *testing.T, daemon *daemon) {
			daemon.close()
			if !daemon.storeClosing {
				t.Fatal("close did not mark the store lifetime closed")
			}
		}},
		{"app.go:801 withLinuxV2EffectReadyTarget", func(t *testing.T, daemon *daemon) {
			err := daemon.withLinuxV2EffectReadyTarget(context.Background(), key, protocol.Fence{}, func() error { return nil })
			requireErrorMessage(t, err, "Linux v2 effect boundary requires a context and effect")
		}},
		{"cleanup.go:545 cleanupLinuxCloneWorkspace", func(t *testing.T, daemon *daemon) {
			err := daemon.cleanupLinuxCloneWorkspace(context.Background(), state.RunJournal{})
			requireErrorMessage(t, err, "Linux clone cleanup lacks its exact context, daemon, or Store")
		}},
		{"cleanup.go:769 persistLinuxCloneTargetRemoval", func(t *testing.T, daemon *daemon) {
			err := daemon.persistLinuxCloneTargetRemoval(key, protocol.Fence{}, authority.LinuxCloneCleanupIntent{}, authority.LinuxCloneTargetRemovalProof{})
			requireErrorMessage(t, err, "Linux clone target-removal proof is incomplete or differs from its cleanup intent")
		}},
		{"cleanup_glue.go:42 selectStoreWrite refuses", func(t *testing.T, daemon *daemon) {
			err := daemon.clearContainmentUnprovenCallback(key)(81, "windows:81:0000000000000001")
			if !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("storeless containment clear = %v, want %v", err, errDaemonStoreClosing)
			}
		}},
		{"cleanup_glue.go:44 override wins without a store", func(t *testing.T, daemon *daemon) {
			calls := 0
			daemon.options.markContainmentUnproven = func(state.RunKey, int, string) (state.RunJournal, error) {
				calls++
				return state.RunJournal{}, nil
			}
			if err := daemon.markContainmentUnprovenCallback(key)(81, "windows:81:0000000000000001"); err != nil {
				t.Fatalf("override without a store = %v, want nil", err)
			}
			if calls != 1 {
				t.Fatalf("override calls = %d, want 1", calls)
			}
		}},
		{"cleanup_glue.go:59 persistProcessAuthorityCallback", func(t *testing.T, daemon *daemon) {
			err := daemon.persistProcessAuthorityCallback(key)(81, "windows:81:0000000000000001", nil)
			if !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("storeless authority callback = %v, want %v", err, errDaemonStoreClosing)
			}
		}},
		{"cleanup_glue.go:78 persistProcessMarker", func(t *testing.T, daemon *daemon) {
			err := daemon.persistProcessMarker(key, 81, "windows:81:0000000000000001", time.Now())
			if !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("storeless process marker = %v, want %v", err, errDaemonStoreClosing)
			}
		}},
		{"cleanup_glue.go:93 marker mirror error skips the readback", func(t *testing.T, daemon *daemon) {
			daemon.options.recordProcess = func(state.RunKey, int, string, time.Time) (state.RunJournal, error) {
				return state.RunJournal{}, errStoreCallbackProbe
			}
			err := daemon.persistProcessMarker(key, 81, "windows:81:0000000000000001", time.Now())
			if !errors.Is(err, errStoreCallbackProbe) {
				t.Fatalf("marker mirror error = %v, want %v", err, errStoreCallbackProbe)
			}
		}},
		{"cleanup_glue.go:290 verifyDurableContainmentStopReceipt", func(t *testing.T, daemon *daemon) {
			err := daemon.verifyDurableContainmentStopReceipt(key, 81, "windows:81:0000000000000001", authority.StopReceipt{})
			requireErrorMessage(t, err, "state store is unavailable")
		}},
		{"cleanup_glue.go:396 clearPersistedProcessDetails skips the readback", func(t *testing.T, daemon *daemon) {
			cleared := 0
			daemon.options.clearContainmentUnproven = func(state.RunKey, int, string) (state.RunJournal, error) {
				return state.RunJournal{}, nil
			}
			daemon.options.clearProcessDetails = func(state.RunKey, int, string) (state.RunJournal, error) {
				cleared++
				return state.RunJournal{RunID: key.RunID, Generation: key.Generation}, nil
			}
			journal, err := daemon.clearPersistedProcessDetails(key, 81, "windows:81:0000000000000001")
			if err != nil || cleared != 1 || journal.RunID != key.RunID {
				t.Fatalf("storeless process-detail clear = journal:%#v calls:%d err:%v", journal, cleared, err)
			}
		}},
		{"deterministic_validation.go:90 artifact validation", func(t *testing.T, daemon *daemon) {
			_, err := daemon.tryDeterministicArtifactValidation(context.Background(), key, protocol.ClaimResponse{}, protocol.Admission{Purpose: protocol.AdmissionPurposeValidate})
			if err == nil || !strings.Contains(err.Error(), "state store is unavailable") {
				t.Fatalf("storeless artifact validation = %v, want the state-store refusal", err)
			}
		}},
		{"native_run.go:217 bindLinuxCloneReuse", func(t *testing.T, daemon *daemon) {
			err := daemon.bindLinuxCloneReuse(context.Background(), key, protocol.Fence{}, authority.LinuxCloneReuseGrant{})
			requireErrorMessage(t, err, "Linux clone reuse requires the App Store")
		}},
		{"native_run.go:227 abortLinuxCloneReuseBeforeNativeStart", func(t *testing.T, daemon *daemon) {
			err := daemon.abortLinuxCloneReuseBeforeNativeStart(context.Background(), key, protocol.Fence{}, authority.LinuxCloneReuseGrant{})
			requireErrorMessage(t, err, "Linux clone reuse abort requires the App Store")
		}},
		{"native_run.go:246 withLinuxV2NativeAdmission", func(t *testing.T, daemon *daemon) {
			err := daemon.withLinuxV2NativeAdmission(context.Background(), key, func(map[state.RunKey]protocol.Fence) error { return nil })
			requireErrorMessage(t, err, "Linux v2 admission requires the App Store and check")
		}},
		{"native_run.go:428 processDetailsAlreadyRecorded", func(t *testing.T, daemon *daemon) {
			recorded, err := daemon.processDetailsAlreadyRecorded(key, 81, "windows:81:0000000000000001")
			if recorded || err != nil {
				t.Fatalf("storeless process-detail probe = %v, %v, want false, nil", recorded, err)
			}
		}},
		{"pi_provider_bridge.go:50 durable provider bridge", func(t *testing.T, daemon *daemon) {
			_, err := newDurablePiProviderBridgeExecutor(nil, daemon.store, key, protocol.ProviderAccess{})
			if !errors.Is(err, errPiProviderActionStoreUnavailable) {
				t.Fatalf("storeless provider bridge = %v, want %v", err, errPiProviderActionStoreUnavailable)
			}
		}},
		{"pi_provider_bridge.go:180 preparePiProviderBridge", func(t *testing.T, daemon *daemon) {
			_, err := daemon.preparePiProviderBridge(context.Background(), key, harness.KindPi, &protocol.ProviderAccess{})
			requireErrorMessage(t, err, "pi provider bridge prerequisites are unavailable")
		}},
		{"recovery.go:619 persistSupervisorRecoveryScanSeal", func(t *testing.T, daemon *daemon) {
			err := daemon.persistSupervisorRecoveryScanSeal(key, authority.Supervisor{}, 1)
			requireErrorMessage(t, err, "state store is unavailable")
		}},
		{"recovery.go:635 persistContainmentStopReceipt", func(t *testing.T, daemon *daemon) {
			err := daemon.persistContainmentStopReceipt(key, 81, "windows:81:0000000000000001", authority.StopReceipt{})
			requireErrorMessage(t, err, "state store is unavailable")
		}},
		{"recovery.go:705 queueRecoveryTerminalTransition", func(t *testing.T, daemon *daemon) {
			err := daemon.queueRecoveryTerminalTransition(context.Background(), key, "failed", nil)
			requireErrorMessage(t, err, "state store is unavailable")
		}},
		{"recovery_glue.go:33 persistSupervisorHandoffStopReceipt", func(t *testing.T, daemon *daemon) {
			handoff := testAppHandoffFixture(81, "windows:81:0000000000000001")
			receipt := authority.StopReceipt{
				Version: authority.SupervisorVersion, Status: "stopped",
				TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
				PipeToken: handoff.PipeToken, JobID: handoff.JobID,
				SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
			}
			if !receipt.ValidForHandoff(handoff) {
				t.Fatal("probe receipt must match its handoff")
			}
			err := daemon.persistSupervisorHandoffStopReceipt(key, handoff, receipt, false)
			if !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("storeless handoff stop receipt = %v, want %v", err, errDaemonStoreClosing)
			}
		}},
	}
	runStorelessProbes(t, probes)
}

type storelessProbe struct {
	site string
	run  func(t *testing.T, daemon *daemon)
}

func runStorelessProbes(t *testing.T, probes []storelessProbe) {
	for _, probe := range probes {
		t.Run(probe.site, func(t *testing.T) {
			probe.run(t, &daemon{})
		})
	}
}

func requireErrorMessage(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
