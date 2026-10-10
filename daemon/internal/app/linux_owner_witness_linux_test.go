//go:build linux && amd64

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

func TestLinuxV2NativeOwnerWitnessRequiresOptInBeforeFixtureAccess(t *testing.T) {
	t.Setenv(LinuxV2NativeOwnerWitnessEnvironment, "")
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	err := RunLinuxV2NativeOwnerWitness(context.Background(), missing, missing, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("RunLinuxV2NativeOwnerWitness() error = %v, want opt-in rejection before file access", err)
	}
}

func TestLinuxV2NativeOwnerWitnessRejectsUntrustedFixturePath(t *testing.T) {
	t.Setenv(LinuxV2NativeOwnerWitnessEnvironment, "1")
	outside := filepath.Join(t.TempDir(), "config.json")
	err := RunLinuxV2NativeOwnerWitness(context.Background(), outside, outside, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "outside /opt/symmetry-owner-test") {
		t.Fatalf("RunLinuxV2NativeOwnerWitness() error = %v, want fixed-root rejection", err)
	}
}

func TestLinuxV2NativeOwnerWitnessReportsOnlySupportedPeerDenials(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{message: "duplicate exact socket with pidfd_getfd: operation not permitted", want: "pidfd_getfd_denied"},
		{message: "open launched TCP peer fdinfo: permission denied", want: "fdinfo_denied"},
		{message: "stat launched process user namespace: permission denied", want: "nsfs_namespace_denied"},
		{message: "TCP peer process identity changed", want: ""},
	}
	for _, test := range tests {
		if got := classifyLinuxV2NativeWitnessPeerDenial(errors.New(test.message)); got != test.want {
			t.Errorf("classify(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestLinuxV2NativeOwnerWitnessRejectsMismatchedClaimFenceAndSubject(t *testing.T) {
	value, owner, request := validLinuxV2NativeWitnessFixture()
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*linuxV2NativeWitnessRequest)
	}{
		{name: "run key", mutate: func(value *linuxV2NativeWitnessRequest) { value.RunKey.RunID = "other-run" }},
		{name: "fence lease", mutate: func(value *linuxV2NativeWitnessRequest) { value.Fence.LeaseToken = "other-lease" }},
		{name: "claim generation", mutate: func(value *linuxV2NativeWitnessRequest) { value.Claim.Generation++ }},
		{name: "work profile", mutate: func(value *linuxV2NativeWitnessRequest) { value.Claim.Work.AgentProfile = "other-profile" }},
		{name: "subject resource", mutate: func(value *linuxV2NativeWitnessRequest) {
			value.Subject.ResourceID = "22222222-2222-4222-8222-222222222222"
		}},
		{name: "provider operation", mutate: func(value *linuxV2NativeWitnessRequest) {
			value.Claim.ProviderAccess.Grants[0].Operations[0] = "change.upsert"
		}},
		{name: "provider resource", mutate: func(value *linuxV2NativeWitnessRequest) {
			value.Claim.ProviderAccess.Grants[0].ResourceID = "22222222-2222-4222-8222-222222222222"
		}},
		{name: "capability requirement", mutate: func(value *linuxV2NativeWitnessRequest) {
			value.Claim.Work.RequiredCapabilities = map[string]bool{"native.pi": true}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneLinuxV2NativeWitnessRequest(request)
			test.mutate(&candidate)
			if err := validateLinuxV2NativeWitnessRequest(candidate, value, owner); err == nil {
				t.Fatal("mismatched request claim/fence/Subject was accepted")
			}
		})
	}
}

func TestLinuxV2NativeOwnerWitnessReplayRequiredIsRestrictive(t *testing.T) {
	value, owner, request := validLinuxV2NativeWitnessFixture()
	request = stageReplayWitnessRequest(t, request)
	request.ReplayRequired = true
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err != nil {
		t.Fatalf("valid replay-required stage request rejected: %v", err)
	}
	if _, found, err := selectLinuxV2NativeWitnessJournal(nil, request); found || !errors.Is(err, errLinuxV2NativeWitnessReplayTargetMissing) {
		t.Fatalf("replay-required absent RunKey selection = found:%t error:%v, want fail before fresh launch", found, err)
	}
	request.ReplayRequired = false
	if _, found, err := selectLinuxV2NativeWitnessJournal(nil, request); err != nil || found {
		t.Fatalf("fresh authentic absent-RunKey selection = found:%t error:%v, want fresh admission", found, err)
	}
	request.Case = "pi_provider_action"
	request.ReplayRequired = true
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err == nil {
		t.Fatal("ReplayRequired was accepted outside the restrictive stage-replay witness")
	}
}

func TestLinuxV2NativeWitnessSharedStoreAllowsOnlyProofSafeForeignRows(t *testing.T) {
	value, owner, request := validLinuxV2NativeWitnessFixture()
	request = stageReplayWitnessRequest(t, request)
	stateDir := t.TempDir()
	fixture := newLinuxCloneAppRetainedNonGoalFixtureAt(t, stateDir)
	store := fixture.store
	failedTerminal := loadWitnessJournal(t, store, fixture.key)
	failedTerminal.TerminalState = "failed"
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writeAppRunJournalFixture(t, stateDir, failedTerminal)
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatalf("reopen failed-terminal physically released Store: %v", err)
	}
	defer store.Close()
	fixture.store = store
	if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err != nil {
		t.Fatalf("valid failed-terminal v2-released retained non-Goal checkout was rejected: %v", err)
	}
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err != nil {
		t.Fatalf("fresh stage-replay request fixture is invalid: %v", err)
	}
	beforeSafe := snapshotLinuxV2NativeWitnessJournals(t, stateDir)
	journals, err := store.ListJournals()
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := selectLinuxV2NativeWitnessJournal(journals, request); err != nil || found {
		t.Fatalf("fresh RunKey with only a safe retained row = found:%t error:%v", found, err)
	}
	assertLinuxV2NativeWitnessJournalsUnchanged(t, beforeSafe, snapshotLinuxV2NativeWitnessJournals(t, stateDir))
	target := addLinuxV2NativeWitnessNoTargetAbortJournal(t, store, value, request, false)

	foreignKey := state.RunKey{RunID: "foreign-unresolved-run", Generation: 1}
	foreignJournal := addUnresolvedLinuxV2WitnessJournal(t, store, foreignKey)
	if !foreignJournal.HasPendingContainment() {
		t.Fatal("foreign owner fixture is not unresolved")
	}
	beforeBlocked := snapshotLinuxV2NativeWitnessJournals(t, stateDir)
	if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err == nil {
		t.Fatal("fresh native admission passed an unresolved foreign v2 owner")
	}
	assertLinuxV2NativeWitnessJournalsUnchanged(t, beforeBlocked, snapshotLinuxV2NativeWitnessJournals(t, stateDir))

	replayRequest := request
	replayRequest.ReplayRequired = true
	journals, err = store.ListJournals()
	if err != nil {
		t.Fatal(err)
	}
	selected, found, err := selectLinuxV2NativeWitnessJournal(journals, replayRequest)
	if err != nil || !found || selected.Key() != target.Key() {
		t.Fatalf("exact-key replay with an unresolved foreign owner = key:%#v found:%t error:%v", selected.Key(), found, err)
	}
	beforeReplay := snapshotLinuxV2NativeWitnessJournals(t, stateDir)
	if err := recoverLinuxV2NativeWitnessRunKey(context.Background(), value, store, selected); err != nil {
		t.Fatalf("recover only the selected exact RunKey: %v", err)
	}
	assertLinuxV2NativeWitnessJournalsUnchanged(t, beforeReplay, snapshotLinuxV2NativeWitnessJournals(t, stateDir))
	latestForeign := loadWitnessJournal(t, store, foreignKey)
	if latestForeign.Key() != foreignJournal.Key() || latestForeign.ContainmentCgroup == nil || foreignJournal.ContainmentCgroup == nil ||
		!latestForeign.ContainmentCgroup.Equal(*foreignJournal.ContainmentCgroup) {
		t.Fatal("exact-key replay changed the unrelated unresolved owner identity")
	}
}

func TestLinuxV2NativeWitnessForeignAdmissionRequiresExactNoTargetProof(t *testing.T) {
	for _, test := range []struct {
		name        string
		helperBound bool
	}{
		{name: "reservation durable"},
		{name: "helper bound", helperBound: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, owner, request := validLinuxV2NativeWitnessFixture()
			request = stageReplayWitnessRequest(t, request)
			foreignRequest := request
			foreignRequest.RunKey.RunID = "foreign-reservation-run"
			foreignRequest.Fence.ClaimID = "foreign-reservation-claim"
			foreignRequest.Fence.LeaseToken = "foreign-reservation-lease"
			foreignRequest.Claim.RunID = foreignRequest.RunKey.RunID
			foreignRequest.Claim.ClaimID = foreignRequest.Fence.ClaimID
			foreignRequest.Claim.LeaseToken = foreignRequest.Fence.LeaseToken
			if test.helperBound {
				foreignRequest.RunKey.RunID = "foreign-helper-run"
				foreignRequest.Fence.ClaimID = "foreign-helper-claim"
				foreignRequest.Fence.LeaseToken = "foreign-helper-lease"
				foreignRequest.Claim.RunID = foreignRequest.RunKey.RunID
				foreignRequest.Claim.ClaimID = foreignRequest.Fence.ClaimID
				foreignRequest.Claim.LeaseToken = foreignRequest.Fence.LeaseToken
			}
			stateDir := t.TempDir()
			store, err := state.New(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			journal := addLinuxV2NativeWitnessNoTargetAbortJournal(t, store, value, foreignRequest, test.helperBound)
			if journal.HasPendingContainment() || journal.HasProcessDetails() || journal.LinuxCloneAbortProof == nil {
				t.Fatalf("no-target abort fixture retains an owner: %#v", journal)
			}
			if err := store.CheckLinuxV2NativeAdmissionBeforeClaim(time.Now().UTC(), nil); err != nil {
				t.Fatalf("exact %s no-target abort proof was rejected: %v", test.name, err)
			}
			if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err != nil {
				t.Fatalf("fresh request fixture is invalid: %v", err)
			}
			journals, err := store.ListJournals()
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := selectLinuxV2NativeWitnessJournal(journals, request); err != nil || found {
				t.Fatalf("fresh RunKey identity selection with exact %s no-target proof = found:%t error:%v", test.name, found, err)
			}
		})
	}
}

func TestLinuxV2NativeOwnerWitnessUsesTrustedInstalledPiGroup(t *testing.T) {
	owner := linuxgate.OwnerConfig{AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001}
	value := config.Config{Runtime: config.Runtime{HarnessKind: config.RuntimeHarnessPi}}
	if err := verifyTrustedLinuxWitnessPiExtensionGroup(value, owner); err == nil {
		t.Fatal("unset Pi provider group was accepted")
	}
	if err := populateTrustedLinuxWitnessPiExtensionGroup(&value, owner); err != nil {
		t.Fatalf("populate installed B primary group: %v", err)
	}
	if value.PiProviderExtensionGroupID != owner.BGID {
		t.Fatalf("Pi extension group = %d, want installed B primary group %d", value.PiProviderExtensionGroupID, owner.BGID)
	}
	value.PiProviderExtensionGroupID = owner.AGID
	if err := verifyTrustedLinuxWitnessPiExtensionGroup(value, owner); err == nil {
		t.Fatal("A's group was accepted for the B-readable Pi extension share")
	}
}

func TestLinuxV2NativeWitnessStageReplayKeepsStaticGateDistinctFromBTarget(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "stage_replay"},
		summary: linuxV2NativeWitnessSummary{
			Stages: linuxV2NativeWitnessStages{
				ReservationDurable: true, CreateStarted: true, LeafBound: true, HelperBound: true,
				LauncherBound: true, GateBound: true, HandoffPrepared: true, HandoffCommitted: true,
			},
			Owners:             linuxV2NativeWitnessOwners{Gate: linuxV2NativeWitnessProcess{PID: 42, Identity: "a-gate"}},
			Target:             linuxV2NativeWitnessTarget{PID: 42, Identity: "a-gate"},
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseNotReleased,
			Stop: linuxV2NativeWitnessStop{
				ReceiptVersion: authority.SupervisorHandoffVersionV2, KillAcknowledged: true, PopulatedZero: true,
				NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
			},
			Release: linuxV2NativeWitnessRelease{
				OpaquePlatformRelease: true, LeafRemoved: true, ClearPersisted: true, AnchorRetired: true,
			},
		},
	}
	witness.normalizeStageReplayTarget()
	if witness.summary.Target.PID != 0 || witness.summary.Target.Identity != "" {
		t.Fatalf("pre-RELEASE static gate leaked into B target summary: %+v", witness.summary.Target)
	}
	if err := witness.validateStageReplayPhysicalStopSummary(); err != nil {
		t.Fatalf("exact pre-RELEASE physical recovery rejected: %v", err)
	}

	witness.summary.Target = linuxV2NativeWitnessTarget{PID: 42, Identity: "a-gate"}
	witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseUnknown
	witness.normalizeStageReplayTarget()
	if witness.summary.Target.PID != 42 || witness.summary.Target.Identity != "a-gate" {
		t.Fatalf("unknown release state erased the exact gate/target identity: %+v", witness.summary.Target)
	}
	if err := witness.validateStageReplayPhysicalStopSummary(); err != nil {
		t.Fatalf("receipt-backed recovery with unknown RELEASE outcome rejected: %v", err)
	}

	witness.summary.Stages.InitialLeaseAcked = true
	witness.summary.Stages.CheckoutReady = true
	witness.summary.Stages.NativeReleased = true
	witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseReleased
	if err := witness.validateStageReplayPhysicalStopSummary(); err == nil {
		t.Fatal("native RELEASE stage without an exact B pidfd/namespace target was accepted")
	}
}

func TestLinuxV2NativeWitnessStageReplayRequiresExactUnresolvedCategory(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "stage_replay"},
		summary: linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "containment_unproven", JournalUnproven: true,
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseUnknown,
		},
	}
	if err := witness.validateFinalSummary(); err != nil {
		t.Fatalf("exact still-unproven RunJournal rejected: %v", err)
	}
	witness.summary.FailureCategory = ""
	if err := witness.validateFinalSummary(); err == nil {
		t.Fatal("unresolved recovery error was reported without its exact non-secret failure category")
	}
}

func TestLinuxV2NativeWitnessReportsOnlyExactAuthenticatedHelperDrop(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	fence := journal.Fence()
	reservation := testAppLinuxV2Reservation()
	if _, err := store.ReserveLinuxCgroup(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, fence, reservation, nil); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	reservation = journal.ContainmentCgroup.Clone()
	leaf := testAppLinuxV2Leaf(reservation)
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, leaf); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	reservation = journal.ContainmentCgroup.Clone()
	helper := testAppLinuxV2Helper()
	if _, err := store.BindLinuxCgroupHelper(key, reservation, helper); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil || journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil {
		t.Fatalf("read helper-bound RunJournal: %#v, error=%v", journal.ContainmentCgroup, err)
	}
	fence = journal.Fence()
	witness := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{
		Case: "stage_replay", RunKey: linuxV2NativeWitnessRunKey{RunID: key.RunID, Generation: key.Generation}, Fence: fence,
	}}
	status := execution.LinuxV2WitnessDropStatus{
		RunKey: key, Fence: fence, Helper: *journal.ContainmentCgroup.Helper,
		Observed: true, Operation: "launcher-ready",
	}
	process := &linuxV2WitnessDropStatusFakeProcess{status: status}
	if err := witness.captureHelperDropStatus(store, process); err != nil {
		t.Fatalf("capture exact helper drop status: %v", err)
	}
	if !witness.summary.HelperDropObserved || witness.summary.HelperDropOperation != "launcher-ready" ||
		witness.summary.ResponseLostAfter != "launcher-ready" {
		t.Fatalf("authenticated helper drop summary = %#v", witness.summary)
	}

	status.Fence.LeaseToken = "different-lease"
	process.status = status
	other := &linuxV2NativeWitnessRun{request: witness.request}
	if err := other.captureHelperDropStatus(store, process); err == nil {
		t.Fatal("helper-drop status with a stale claim fence was accepted")
	}
}

func TestLinuxV2NativeWitnessAllowsReceiptBackedLostClearAck(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{
			Case: "stage_replay", DropAckAfter: linuxV2NativeWitnessClearedStage,
			Barrier: linuxV2NativeWitnessBarrier{HoldAfter: linuxV2NativeWitnessClearedStage},
		},
		summary: linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "response_lost",
			ResponseLostAfter: linuxV2NativeWitnessClearedStage, JournalUnproven: true,
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseReleased,
			Stages: linuxV2NativeWitnessStages{
				NativeReleased: true, StopReceiptDurable: true, LeafRemoved: true, JournalCleared: true,
			},
			Stop: linuxV2NativeWitnessStop{
				ReceiptVersion: authority.SupervisorVersionV2, ReceiptStatus: "stopped", ProofKind: "helper-cgroup-kill",
				Issuer:           linuxV2NativeWitnessProcess{PID: 60, Identity: "helper:60:start"},
				Reaper:           linuxV2NativeWitnessProcess{PID: 10, Identity: "anchor:10:start"},
				KillAcknowledged: true, PopulatedZero: true, NoMigrationObserved: true,
				ProducerFenced: true, DescendantsReaped: true,
				LauncherExit: linuxV2NativeWitnessExit{PID: 61, Identity: "launcher:61:start", Waited: true, Reaped: true},
				GateExit:     linuxV2NativeWitnessExit{PID: 62, Identity: "gate:62:start", Waited: true, Reaped: true},
			},
			Release: linuxV2NativeWitnessRelease{
				Version: authority.SupervisorHandoffVersionV2, LeafDisposition: authority.LinuxCgroupLeafRemoved,
				OpaquePlatformRelease: true, LeafRemoved: true, ClearPersisted: true,
			},
		},
	}
	if err := witness.validateFinalSummary(); err != nil {
		t.Fatalf("receipt-backed lost-clear response rejected: %v", err)
	}
	witness.summary.Release.ClearPersisted = false
	if err := witness.validateFinalSummary(); err == nil {
		t.Fatal("lost-clear response without durable clear proof was accepted")
	}
	witness.request.DropAckAfter = ""
	witness.request.Barrier.HoldAfter = linuxV2NativeWitnessLauncherStage
	witness.summary.Release.ClearPersisted = true
	witness.summary.ResponseLostAfter = "launcher-ready"
	witness.summary.HelperDropObserved = true
	witness.summary.HelperDropOperation = "launcher-ready"
	witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
	witness.summary.Stages.NativeReleased = false
	if err := witness.validateFinalSummary(); err != nil {
		t.Fatalf("safe launcher-ready cleanup tombstone rejected: %v", err)
	}
}

func TestLinuxV2NativeWitnessAllowsExactStopReceiptForLostReceiptAck(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{
			Case: "stage_replay", DropAckAfter: linuxV2NativeWitnessStopReceiptStage,
			Barrier: linuxV2NativeWitnessBarrier{HoldAfter: linuxV2NativeWitnessStopReceiptStage},
		},
		summary: linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "response_lost",
			ResponseLostAfter: linuxV2NativeWitnessStopReceiptStage, JournalUnproven: true,
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseReleased,
			Stages:             linuxV2NativeWitnessStages{NativeReleased: true, StopReceiptDurable: true},
			Stop: linuxV2NativeWitnessStop{
				ReceiptVersion: authority.SupervisorVersionV2, ReceiptStatus: "stopped", ProofKind: "helper-cgroup-kill",
				Issuer:           linuxV2NativeWitnessProcess{PID: 60, Identity: "helper:60:start"},
				Reaper:           linuxV2NativeWitnessProcess{PID: 10, Identity: "anchor:10:start"},
				KillAcknowledged: true, PopulatedZero: true, NoMigrationObserved: true,
				ProducerFenced: true, DescendantsReaped: true,
				LauncherExit: linuxV2NativeWitnessExit{PID: 61, Identity: "launcher:61:start", Waited: true, Reaped: true},
				GateExit:     linuxV2NativeWitnessExit{PID: 62, Identity: "gate:62:start", Waited: true, Reaped: true},
			},
		},
	}
	if err := witness.validateFinalSummary(); err != nil {
		t.Fatalf("exact v2 receipt after lost receipt ACK rejected: %v", err)
	}
	witness.summary.Stop.ReceiptVersion = 0
	if err := witness.validateFinalSummary(); err == nil {
		t.Fatal("lost receipt ACK without exact v2 receipt was accepted")
	}
}

type linuxV2WitnessDropStatusFakeProcess struct {
	status execution.LinuxV2WitnessDropStatus
	err    error
}

func (*linuxV2WitnessDropStatusFakeProcess) WriteInput([]byte) error { return nil }

func (*linuxV2WitnessDropStatusFakeProcess) Terminate(context.Context, time.Duration) error {
	return nil
}

func (*linuxV2WitnessDropStatusFakeProcess) Wait() execution.Result { return execution.Result{} }

func (process *linuxV2WitnessDropStatusFakeProcess) ProcessDetails() (int, string) {
	return process.status.Helper.PID, process.status.Helper.Identity
}

func (process *linuxV2WitnessDropStatusFakeProcess) LinuxV2WitnessDropStatus(context.Context) (execution.LinuxV2WitnessDropStatus, error) {
	return process.status, process.err
}

func TestLinuxV2NativeWitnessCommittedClearPublishesLeafRemovedFirst(t *testing.T) {
	clearFailure := errors.New("stop before committed Store clear")
	var calls []string
	err := runLinuxV2WitnessReleaseClearCallbacks(
		func() error {
			calls = append(calls, "leaf_removed")
			return nil
		},
		func() error {
			if strings.Join(calls, ",") != "leaf_removed" {
				return errors.New("committed Store clear began before leaf_removed")
			}
			calls = append(calls, "store_clear")
			return clearFailure
		},
		func() error {
			calls = append(calls, "journal_cleared")
			return nil
		},
	)
	if !errors.Is(err, clearFailure) || strings.Join(calls, ",") != "leaf_removed,store_clear" {
		t.Fatalf("normal v2 clear barrier sequence = %v, calls=%v; want leaf_removed before one clear and no journal_cleared", err, calls)
	}
}

func TestLinuxV2NativeWitnessCommittedClearRejectsUnissuedRelease(t *testing.T) {
	clearCalls := 0
	callbacks := execution.LinuxV2JournalCallbacks{
		ClearProcessDetailsV2: func(int, string, platform.LinuxCgroupV2Release) error {
			clearCalls++
			return nil
		},
	}
	witness := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Case: "stage_replay"}}
	if err := witness.wrapLinuxV2Callbacks(nil, nil, &callbacks); err != nil {
		t.Fatalf("wrap normal v2 clear callback: %v", err)
	}
	if err := callbacks.ClearProcessDetailsV2(42, "a-gate", platform.LinuxCgroupV2Release{}); err == nil {
		t.Fatal("unissued platform release reached the normal Store clear")
	}
	if clearCalls != 0 {
		t.Fatalf("Store clear calls for an unissued platform release = %d, want 0", clearCalls)
	}
}

func TestNormalAppPiOwnerCopiesOnlyTrustedGroupBeforePreflight(t *testing.T) {
	owner := &fakeLinuxV2NativeOwner{kind: harness.KindPi}
	value := config.Config{Runtime: config.Runtime{HarnessKind: config.RuntimeHarnessPi}, PiProviderExtensionGroupID: 77}
	if err := setTrustedPiProviderExtensionGroup(&value, owner); err != nil {
		t.Fatalf("set trusted App Pi group: %v", err)
	}
	if value.PiProviderExtensionGroupID != owner.trustedPiProviderExtensionGroupID() {
		t.Fatalf("App Pi extension group = %d, want installed owner group %d", value.PiProviderExtensionGroupID, owner.trustedPiProviderExtensionGroupID())
	}
	if err := validatePiProviderExtensionGroupID(value, owner.trustedPiProviderExtensionGroupID()); err != nil {
		t.Fatalf("trusted App Pi extension group rejected: %v", err)
	}
	value.PiProviderExtensionGroupID++
	if err := validatePiProviderExtensionGroupID(value, owner.trustedPiProviderExtensionGroupID()); err == nil {
		t.Fatal("mismatched App Pi extension group was accepted")
	}
}

func TestPiProviderShareIdentityRequiresInstalledBGroup(t *testing.T) {
	owner := linuxgate.OwnerConfig{AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001}
	share := linuxStatIdentity{uid: owner.AUID, gid: owner.BGID, mode: 0o2750}
	if err := validateLinuxPiExtensionShareIdentity(true, share, owner); err != nil {
		t.Fatalf("correct A-owned/B-group share rejected: %v", err)
	}
	share.gid = owner.AGID
	if err := validateLinuxPiExtensionShareIdentity(true, share, owner); err == nil {
		t.Fatal("Pi extension share with A's group was accepted")
	}
	share.gid = owner.BGID
	share.mode = 0o2770
	if err := validateLinuxPiExtensionShareIdentity(true, share, owner); err == nil {
		t.Fatal("B-writable Pi extension share was accepted")
	}
}

func TestLinuxV2NativeOwnerWitnessRejectsGenericComposeAndMissingDelegation(t *testing.T) {
	owner := linuxgate.OwnerConfig{
		AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001,
		StateDir: linuxV2NativeOwnerWitnessStateDir,
	}
	generic := config.Config{
		ControlPlaneURL: "https://control.example.test", StateDir: filepath.Join(linuxV2NativeOwnerWitnessRoot, "generic", "state"),
		MachineName: "owner-witness-generic",
		Runtime: config.Runtime{RuntimeKey: "witness", Name: "witness", Capacity: 1, AgentProfile: "profile", Workspace: "primary",
			HarnessKind: config.RuntimeHarnessGeneric, HarnessVersion: "legacy", AdapterVersion: "legacy", AdapterProtocolVersion: 1},
		AgentProfiles: map[string]config.AgentProfile{"profile": {Command: "/opt/symmetry-owner-test/bin/agent", InputMode: config.InputModeGoal}},
		Workspaces:    map[string]config.Workspace{"primary": {Policy: config.WorkspacePolicyExistingCheckout, Path: filepath.Join(linuxV2NativeOwnerWitnessRoot, "generic", "checkout"), Cleanup: config.CleanupAlways}},
	}
	if err := validateLinuxV2NativeWitnessConfig(generic, owner); err == nil {
		t.Fatal("generic/rootless-Compose runtime was accepted for native owner witness")
	}

	piConfig := validLinuxV2NativeWitnessConfig(owner)
	piConfig.LinuxOwnerCgroupMount = ""
	piConfig.LinuxOwnerDelegatedPath = ""
	piConfig.LinuxOwnerSocket = ""
	if err := validateLinuxV2NativeWitnessConfig(piConfig, owner); err == nil || !strings.Contains(err.Error(), "delegated cgroup-v2 parent") {
		t.Fatalf("missing delegation config error = %v, want fail-closed missing-parent rejection", err)
	}
	if err := validateLinuxV2NativeWitnessInstalledOwner(linuxgate.OwnerConfig{}, owner.AUID, owner.AUID, owner.AGID, owner.AGID); err == nil {
		t.Fatal("missing installed owner config was accepted")
	}
}

func TestLinuxV2NativeWitnessStateDirExceptionIsExactAndInstalled(t *testing.T) {
	owner := linuxgate.OwnerConfig{
		AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001,
		StateDir: linuxV2NativeOwnerWitnessStateDir,
	}
	value := validLinuxV2NativeWitnessConfig(owner)
	if err := validateLinuxV2NativeWitnessConfig(value, owner); err != nil {
		t.Fatalf("exact root-installed short StateDir rejected: %v", err)
	}
	for _, path := range []string{
		"/var/lib/symmetry-v2-other",
		"/var/lib/symmetry-other",
		filepath.Join(linuxV2NativeOwnerWitnessStateDir, "child"),
		filepath.Join(linuxV2NativeOwnerWitnessRoot, "pi", "state"),
	} {
		candidate := value
		candidate.StateDir = path
		if err := validateLinuxV2NativeWitnessConfig(candidate, owner); err == nil {
			t.Errorf("unapproved StateDir %q accepted with the fixed installed OwnerConfig path", path)
		}
	}
	mismatchedOwner := owner
	mismatchedOwner.StateDir = "/var/lib/symmetry-v2-elsewhere"
	if err := validateLinuxV2NativeWitnessConfig(value, mismatchedOwner); err == nil {
		t.Fatal("fixed witness StateDir was accepted when root-installed OwnerConfig.StateDir differed")
	}
}

func validLinuxV2NativeWitnessFixture() (config.Config, linuxgate.OwnerConfig, linuxV2NativeWitnessRequest) {
	owner := linuxgate.OwnerConfig{
		AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001,
		StateDir: linuxV2NativeOwnerWitnessStateDir,
	}
	resource := "11111111-1111-4111-8111-111111111111"
	value := validLinuxV2NativeWitnessConfig(owner)
	request := linuxV2NativeWitnessRequest{
		Version: 1, Case: "pi_provider_action", Harness: config.RuntimeHarnessPi,
		RunKey: linuxV2NativeWitnessRunKey{RunID: "witness-run-1", Generation: 1},
		Fence:  protocol.Fence{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: 1, ClaimID: "claim-1", LeaseToken: "authentic-control-lease"},
		Claim: protocol.ClaimResponse{
			RunID: "witness-run-1", TaskID: "task-1", Generation: 1, ClaimID: "claim-1", LeaseToken: "authentic-control-lease",
			LeaseExpiresAt: time.Now().Add(2 * time.Minute), LeaseRemainingMS: 120000,
			Work: protocol.Work{Goal: "sync the admitted repository", AgentProfile: "profile", Workspace: "primary",
				RequiredCapabilities: map[string]bool{"provider_access": true}},
			ProviderAccess: &protocol.ProviderAccess{Path: "/api/v1/provider-actions", Token: "signed-control-provider-token", Grants: []protocol.ProviderGrant{{
				ResourceID: resource, Provider: "github", Kind: "repository", Operations: []string{"resource.sync"},
			}}},
		},
		Subject: protocol.Subject{ResourceID: resource, Commit: strings.Repeat("a", 40), TreeDigest: "sha256:" + strings.Repeat("b", 64)},
		Barrier: linuxV2NativeWitnessBarrier{Directory: filepath.Join(linuxV2NativeOwnerWitnessRoot, "markers", "pi"), TimeoutSeconds: 30, HoldAfter: "none"},
	}
	return value, owner, request
}

func cloneLinuxV2NativeWitnessRequest(value linuxV2NativeWitnessRequest) linuxV2NativeWitnessRequest {
	cloned := value
	cloned.Claim = value.Claim
	if value.Claim.ProviderAccess != nil {
		access := *value.Claim.ProviderAccess
		access.Grants = append([]protocol.ProviderGrant(nil), value.Claim.ProviderAccess.Grants...)
		for index := range access.Grants {
			access.Grants[index].Operations = append([]string(nil), access.Grants[index].Operations...)
		}
		cloned.Claim.ProviderAccess = &access
	}
	if value.Claim.Work.RequiredCapabilities != nil {
		cloned.Claim.Work.RequiredCapabilities = make(map[string]bool, len(value.Claim.Work.RequiredCapabilities))
		for name, enabled := range value.Claim.Work.RequiredCapabilities {
			cloned.Claim.Work.RequiredCapabilities[name] = enabled
		}
	}
	return cloned
}

func stageReplayWitnessRequest(t *testing.T, request linuxV2NativeWitnessRequest) linuxV2NativeWitnessRequest {
	t.Helper()
	admission, present, err := protocol.ParseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse stage-replay Subject fixture: present=%t error=%v", present, err)
	}
	admission.Subject = request.Subject
	input, err := json.Marshal(admission)
	if err != nil {
		t.Fatalf("encode stage-replay admission input: %v", err)
	}
	request.Case = "stage_replay"
	request.Claim.Work.Input = input
	request.Claim.Work.RequiredCapabilities = nil
	request.Claim.ProviderAccess = nil
	request.Barrier = linuxV2NativeWitnessBarrier{
		Directory:      filepath.Join(linuxV2NativeOwnerWitnessRoot, "markers", "stage-replay"),
		TimeoutSeconds: 30, HoldAfter: linuxV2NativeWitnessCommitStage,
		ReleaseFile: filepath.Join(linuxV2NativeOwnerWitnessRoot, "markers", "stage-replay", "release"),
	}
	request.DropAckAfter = ""
	return request
}

func addLinuxV2NativeWitnessRunJournal(
	t *testing.T,
	store *state.Store,
	value config.Config,
	request linuxV2NativeWitnessRequest,
	reserveClone bool,
) state.RunJournal {
	t.Helper()
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	work := request.Claim.Work
	intent := state.ClaimIntent{
		Key: key, RuntimeKey: value.Runtime.RuntimeKey, RuntimeID: request.Fence.RuntimeID,
		RuntimeEpoch: request.Fence.RuntimeEpoch, ClaimID: request.Fence.ClaimID,
		LocalState: "claiming", Work: work, WorkspaceBindingKey: work.Workspace,
	}
	if _, err := store.SaveClaimIntent(intent); err != nil {
		t.Fatalf("save owner witness claim intent: %v", err)
	}
	if _, err := store.SaveClaimGrant(key, request.Claim); err != nil {
		t.Fatalf("save owner witness claim grant: %v", err)
	}
	if reserveClone {
		binding, ok := value.Workspaces[work.Workspace]
		if !ok {
			t.Fatalf("owner witness workspace %q is missing", work.Workspace)
		}
		clone := authority.LinuxCloneIntent{
			Version: authority.LinuxCloneVersion, RunID: key.RunID, Generation: key.Generation,
			BindingKey: work.Workspace, ResourceID: request.Subject.ResourceID,
			BaseCommit: request.Subject.Commit, BaseTreeDigest: request.Subject.TreeDigest,
			Nonce:    strings.Repeat("a", authority.LinuxCloneNonceBytes*2),
			SeedPath: filepath.Join(binding.SeedRoot, "binding-"+work.Workspace, "run-"+key.RunID, fmt.Sprintf("generation-%d.bundle", key.Generation)),
			Target:   filepath.Join(binding.Root, "binding-"+work.Workspace, "run-"+key.RunID, fmt.Sprintf("generation-%d", key.Generation)),
		}
		callbacks := linuxV2AppTestDaemon(t, store, key, false).linuxCloneJournalCallbacks(key, request.Fence)
		if err := callbacks.Reserve(clone); err != nil {
			t.Fatalf("reserve exact witness clone intent: %v", err)
		}
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("load seeded witness journal: %v", err)
	}
	return journal
}

func addUnresolvedLinuxV2WitnessJournal(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
	t.Helper()
	work := protocol.Work{Goal: "foreign unresolved owner", AgentProfile: "local", Workspace: "local", Input: json.RawMessage(`{}`)}
	claim := state.ClaimIntent{
		Key: key, RuntimeKey: "foreign-runtime", RuntimeID: "foreign-runtime-id", RuntimeEpoch: 1,
		ClaimID: "foreign-claim", LocalState: "claiming", Work: work, WorkspaceBindingKey: work.Workspace,
	}
	if _, err := store.SaveClaimIntent(claim); err != nil {
		t.Fatalf("save foreign unresolved claim intent: %v", err)
	}
	grant := protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: claim.ClaimID,
		LeaseToken: "foreign-lease", LeaseExpiresAt: time.Now().Add(time.Hour), Work: work,
	}
	if _, err := store.SaveClaimGrant(key, grant); err != nil {
		t.Fatalf("save foreign unresolved claim grant: %v", err)
	}
	fence := protocol.Fence{
		RuntimeID: claim.RuntimeID, RuntimeEpoch: claim.RuntimeEpoch,
		Generation: key.Generation, ClaimID: claim.ClaimID, LeaseToken: grant.LeaseToken,
	}
	reservation := testAppLinuxV2Reservation()
	reservation.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
	reservation.Secret = strings.Repeat("e", authority.SecretBytes*2)
	reservation.PipeToken = strings.Repeat("d", authority.TokenBytes*2)
	reservation.JobID = strings.Repeat("c", authority.TokenBytes*2)
	reservation.LeafName = strings.Repeat("b", authority.TokenBytes*2)
	reservation.Anchor = authority.LinuxCgroupProcessIdentity{PID: 110, Identity: "foreign-anchor:110:start"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 150, Identity: "foreign-daemon:150:start"}
	reservation.Mirror = &mirror
	callbacks := linuxV2AppTestDaemon(t, store, key, false).linuxV2JournalCallbacks(key, fence)
	if err := callbacks.Reserve(reservation); err != nil {
		t.Fatalf("reserve foreign unresolved cgroup owner: %v", err)
	}
	if err := callbacks.MarkCreateStarted(reservation); err != nil {
		t.Fatalf("mark foreign cgroup creation started: %v", err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindLeaf(reservation, testAppLinuxV2Leaf(reservation)); err != nil {
		t.Fatalf("bind foreign cgroup leaf: %v", err)
	}
	reservation = loadAppLinuxV2Reservation(t, store, key)
	if err := callbacks.BindHelper(reservation, authority.LinuxCgroupProcessIdentity{PID: 120, Identity: "foreign-helper:120:start"}); err != nil {
		t.Fatalf("bind foreign cgroup helper: %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("load foreign unresolved owner journal: %v", err)
	}
	return journal
}

func addLinuxV2NativeWitnessNoTargetAbortJournal(
	t *testing.T,
	store *state.Store,
	value config.Config,
	request linuxV2NativeWitnessRequest,
	helperBound bool,
) state.RunJournal {
	t.Helper()
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	if journal := addLinuxV2NativeWitnessRunJournal(t, store, value, request, true); journal.LinuxCloneIntent == nil {
		t.Fatal("no-target witness fixture lacks its exact clone intent")
	}
	callbacks := linuxV2AppTestDaemon(t, store, key, false).linuxV2JournalCallbacks(key, request.Fence)
	reservation := testAppLinuxV2Reservation()
	reservation.LaunchToken = strings.Repeat("1", authority.TokenBytes*2)
	reservation.Secret = strings.Repeat("2", authority.SecretBytes*2)
	reservation.PipeToken = strings.Repeat("3", authority.TokenBytes*2)
	reservation.JobID = strings.Repeat("4", authority.TokenBytes*2)
	reservation.LeafName = strings.Repeat("5", authority.TokenBytes*2)
	reservation.Anchor = authority.LinuxCgroupProcessIdentity{PID: 110, Identity: "witness-anchor:110:start"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 150, Identity: "witness-daemon:150:start"}
	reservation.Mirror = &mirror
	if err := callbacks.Reserve(reservation); err != nil {
		t.Fatalf("reserve exact no-target witness cgroup: %v", err)
	}
	if !helperBound {
		if _, err := store.AbortNeverCreatedLinuxCgroupReservation(key, request.Fence, reservation); err != nil {
			t.Fatalf("record exact creator-fenced never-created cgroup abort: %v", err)
		}
	} else {
		if err := callbacks.MarkCreateStarted(reservation); err != nil {
			t.Fatalf("mark no-target witness cgroup creation: %v", err)
		}
		reservation = loadAppLinuxV2Reservation(t, store, key)
		if err := callbacks.BindLeaf(reservation, testAppLinuxV2Leaf(reservation)); err != nil {
			t.Fatalf("bind no-target witness leaf: %v", err)
		}
		reservation = loadAppLinuxV2Reservation(t, store, key)
		if err := callbacks.BindHelper(reservation, testAppLinuxV2Helper()); err != nil {
			t.Fatalf("bind no-target witness helper: %v", err)
		}
		reservation = loadAppLinuxV2Reservation(t, store, key)
		helperExit := testAppLinuxV2Exit(*reservation.Helper, reservation.Anchor)
		if err := callbacks.RecordHelperExit(reservation, helperExit); err != nil {
			t.Fatalf("record exact helper wait/reap before no-target abort: %v", err)
		}
		reservation = loadAppLinuxV2Reservation(t, store, key)
		leaf := *reservation.LeafIdentity
		helper := *reservation.Helper
		exit := reservation.HelperExit.Clone()
		proof := authority.LinuxCgroupReservationAbortProof{
			Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: reservation.LaunchToken,
			Parent: reservation.Parent, LeafName: reservation.LeafName, CreateStarted: true,
			LeafIdentity: &leaf, LeafDisposition: authority.LinuxCgroupAbortLeafRemovedEmpty,
			OwnerUID: reservation.OwnerUID, Anchor: reservation.Anchor, Mirror: *reservation.Mirror,
			Helper: &helper, HelperExit: &exit, Witness: reservation.Anchor,
			CreatorFenced: true, NoProducerCanAct: true, ParentUnchanged: true,
			LeafEmpty: true, LeafAbsent: true, RmdirSucceeded: true,
		}
		if err := callbacks.AbortReservation(reservation, proof); err != nil {
			t.Fatalf("record exact helper-bound no-target cgroup abort: %v", err)
		}
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.ContainmentCgroup == nil || journal.ContainmentCgroup.AbortProof == nil || journal.LinuxCloneIntent == nil {
		t.Fatalf("read exact no-target cgroup journal: %#v error=%v", journal, err)
	}
	intent := journal.LinuxCloneIntent.Clone()
	if _, err := store.FenceLinuxCloneBeforeCheckout(key, request.Fence, intent); err != nil {
		t.Fatalf("persist exact clone creator fence for no-target abort: %v", err)
	}
	reservation = journal.ContainmentCgroup.Clone()
	cgroupAbort := reservation.AbortProof.Clone()
	baseRef, err := authority.LinuxCloneBaseRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatalf("derive no-target base ref: %v", err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil {
		t.Fatalf("derive no-target promotion ref: %v", err)
	}
	cloneAbort := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent, CgroupAbortProof: &cgroupAbort,
		BaseRef: baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
	if _, err := store.CompleteLinuxCloneAbort(key, request.Fence, intent, cloneAbort); err != nil {
		t.Fatalf("persist exact clone no-target tombstone: %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatalf("read completed exact no-target tombstone: %v", err)
	}
	return journal
}

func snapshotLinuxV2NativeWitnessJournals(t *testing.T, stateDir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stateDir, "runs"))
	if err != nil {
		t.Fatalf("read owner-witness runs directory: %v", err)
	}
	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected owner-witness journal directory %q", entry.Name())
		}
		contents, err := os.ReadFile(filepath.Join(stateDir, "runs", entry.Name()))
		if err != nil {
			t.Fatalf("read owner-witness journal %q: %v", entry.Name(), err)
		}
		snapshot[entry.Name()] = contents
	}
	return snapshot
}

func assertLinuxV2NativeWitnessJournalsUnchanged(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("owner-witness journal file count changed: before=%d after=%d", len(before), len(after))
	}
	for name, contents := range before {
		if current, found := after[name]; !found || !bytes.Equal(current, contents) {
			t.Fatalf("unrelated owner-witness journal %q changed or disappeared", name)
		}
	}
}

func loadWitnessJournal(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("load owner-witness journal %s/%d: %v", key.RunID, key.Generation, err)
	}
	return journal
}

func validLinuxV2NativeWitnessConfig(owner linuxgate.OwnerConfig) config.Config {
	resource := "11111111-1111-4111-8111-111111111111"
	value := config.Config{
		ControlPlaneURL:            "https://control.example.test",
		StateDir:                   owner.StateDir,
		PiProviderExtensionDir:     filepath.Join(linuxV2NativeOwnerWitnessRoot, "pi", "provider-share"),
		PiProviderExtensionGroupID: owner.BGID,
		LinuxOwnerCgroupMount:      "/sys/fs/cgroup",
		LinuxOwnerDelegatedPath:    "system.slice/symmetry-owner.service/manager",
		LinuxOwnerSocket:           "/run/symmetry-owner/anchor.sock",
		MachineName:                "owner-witness-pi",
		Runtime: config.Runtime{
			RuntimeKey: "witness", Name: "witness", Capacity: 1, AgentProfile: "profile", Workspace: "primary",
			RepositoryResourceID: resource, HarnessKind: config.RuntimeHarnessPi, HarnessVersion: pi.TestedVersion,
			AdapterVersion: "adapter-v2", AdapterProtocolVersion: 2,
		},
		AgentProfiles: map[string]config.AgentProfile{"profile": {
			Command:   filepath.Join(linuxV2NativeOwnerWitnessRoot, "bin", "pi"),
			InputMode: config.InputModeJSON, ProviderAccess: true,
			EnvAllowlist: []string{linuxV2NativeOwnerWitnessHookEnv},
		}},
		Workspaces: map[string]config.Workspace{"primary": {
			Policy:     config.WorkspacePolicyGitCloneV2,
			Repository: filepath.Join(linuxV2NativeOwnerWitnessRoot, "source"),
			Root:       filepath.Join(linuxV2NativeOwnerWitnessRoot, "checkouts"),
			SeedRoot:   filepath.Join(linuxV2NativeOwnerWitnessRoot, "seeds"),
			Cleanup:    config.CleanupNever,
		}},
	}
	return value
}
