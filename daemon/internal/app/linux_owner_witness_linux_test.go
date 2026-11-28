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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/claude"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/linuxpreflight"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
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

func TestLinuxV2NativeWitnessRequestHarnessMustMatchTheConfiguredKind(t *testing.T) {
	value, owner, request := validLinuxV2NativeWitnessFixture()
	request = stageReplayWitnessRequest(t, request)
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err != nil {
		t.Fatalf("valid stage-replay fixture rejected: %v", err)
	}
	request.Harness = config.RuntimeHarnessCodex
	if err := validateLinuxV2NativeWitnessRequest(request, value, owner); err == nil {
		t.Fatal("a stage-replay request whose harness differs from the configured launch kind was accepted")
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
	if _, err := recoverLinuxV2NativeWitnessRunKey(context.Background(), value, store, selected, replayRequest); err != nil {
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

func TestLinuxV2NativeWitnessReleasedSummaryClearsStaleUnproven(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{summary: linuxV2NativeWitnessSummary{
		JournalUnproven: true,
		Release:         linuxV2NativeWitnessRelease{ClearPersisted: true},
	}}
	witness.fillSummaryFromJournal(state.RunJournal{})
	if witness.summary.JournalUnproven {
		t.Fatal("proven released journal retained a stale unproven flag")
	}
	if witness.summary.RecoveryState != "released" {
		t.Fatalf("released journal recovery state = %q", witness.summary.RecoveryState)
	}
}

func TestLinuxV2NativeWitnessPositiveSummaryRequiresTargetAndProvenJournal(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "codex_native_turn"},
		summary: linuxV2NativeWitnessSummary{
			Release: linuxV2NativeWitnessRelease{
				OpaquePlatformRelease: true, LeafRemoved: true, ClearPersisted: true, AnchorRetired: true,
			},
			Stop: linuxV2NativeWitnessStop{
				ReceiptVersion: authority.SupervisorHandoffVersionV2, KillAcknowledged: true, PopulatedZero: true,
				NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
			},
		},
	}
	if err := witness.validatePhysicalStopSummary(); err == nil {
		t.Fatal("positive witness without a target identity was accepted")
	}
	witness.summary.Target = linuxV2NativeWitnessTarget{PID: 42, Identity: "target"}
	witness.summary.JournalUnproven = true
	if err := witness.validatePhysicalStopSummary(); err == nil {
		t.Fatal("positive witness with an unproven journal was accepted")
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

type linuxV2NativeWitnessFailingWriter struct{ err error }

func (writer linuxV2NativeWitnessFailingWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestLinuxV2NativeWitnessUnresolvedOutcomeFailsTheCommand(t *testing.T) {
	summary := linuxV2NativeWitnessSummary{
		Version: 1, Case: "stage_replay", Harness: "pi", NativeVersion: "1.5.2",
		InstalledOwnerVerified: true,
		NativeReleaseState:     linuxV2NativeWitnessNativeReleaseUnknown,
		Outcome:                "unresolved",
		RecoveryState:          "unresolved",
		JournalUnproven:        true,
		FailureCategory:        "containment_unproven",
		RunKey:                 linuxV2NativeWitnessRunKey{RunID: "8f6a0c62-7a14-4d1f-9a2d-0c2b5e7f41aa", Generation: 3},
		Fence: linuxV2NativeWitnessFenceSummary{
			RuntimeID: "owner-witness-control-pi-durability-generic", RuntimeEpoch: 7, Generation: 3,
			ClaimID: "2f9b6d54-1c3a-4e08-8b71-6d5a4c3b2f19",
		},
		Subject: protocol.Subject{
			ResourceID: "b4fa8387-6c2e-4b1a-9f3d-2a7c8e5d0b64",
			Commit:     "3b23dead6c7825d19ae547d66cdc87eaca06ae12",
			TreeDigest: "sha256:c4ef64c9e0e2b8a1d7f3c6b0a9e4d2c8f1b7a3e5d9c0f2a4b6d8e0c2a4f6b8d1",
		},
	}
	want := "{\"version\":1,\"case\":\"stage_replay\",\"harness\":\"pi\",\"native_version\":\"1.5.2\",\"installed_owner_verified\":true," +
		"\"native_release_state\":\"unknown\",\"outcome\":\"unresolved\",\"recovery_state\":\"unresolved\",\"helper_drop_observed\":false," +
		"\"journal_unproven\":true,\"workspace_recovery_required\":false,\"workspace_journal_cleared\":false,\"no_target_abort\":false," +
		"\"failure_category\":\"containment_unproven\",\"run_key\":{\"run_id\":\"8f6a0c62-7a14-4d1f-9a2d-0c2b5e7f41aa\",\"generation\":3}," +
		"\"fence\":{\"runtime_id\":\"owner-witness-control-pi-durability-generic\",\"runtime_epoch\":7,\"generation\":3," +
		"\"claim_id\":\"2f9b6d54-1c3a-4e08-8b71-6d5a4c3b2f19\"},\"subject\":{\"resource_id\":\"b4fa8387-6c2e-4b1a-9f3d-2a7c8e5d0b64\"," +
		"\"commit\":\"3b23dead6c7825d19ae547d66cdc87eaca06ae12\"," +
		"\"tree_digest\":\"sha256:c4ef64c9e0e2b8a1d7f3c6b0a9e4d2c8f1b7a3e5d9c0f2a4b6d8e0c2a4f6b8d1\"}," +
		"\"stages\":{\"reservation_durable\":false,\"create_started\":false,\"leaf_bound\":false,\"helper_bound\":false," +
		"\"launcher_bound\":false,\"gate_bound\":false,\"handoff_prepared\":false,\"handoff_committed\":false," +
		"\"initial_lease_acked\":false,\"checkout_ready\":false,\"native_released\":false,\"native_turn_started\":false," +
		"\"stop_receipt_durable\":false,\"leaf_removed\":false,\"journal_cleared\":false,\"detached_child_live\":false}," +
		"\"target\":{\"pidfd_verified\":false,\"user_namespace\":{\"device\":0,\"inode\":0},\"network_namespace\":{\"device\":0,\"inode\":0}," +
		"\"same_pid_exec\":false},\"owners\":{\"anchor\":{},\"helper\":{},\"launcher\":{},\"gate\":{},\"reaper\":{}}," +
		"\"leaf\":{\"removed\":false},\"clone\":{\"checkout_ready\":false},\"stop\":{\"issuer\":{},\"reaper\":{},\"kill_acknowledged\":false," +
		"\"populated_zero\":false,\"no_migration_observed\":false,\"producer_fenced\":false,\"descendants_reaped\":false," +
		"\"launcher_exit\":{\"waited\":false,\"reaped\":false},\"gate_exit\":{\"waited\":false,\"reaped\":false}}," +
		"\"release\":{\"opaque_platform_release\":false,\"leaf_removed\":false,\"clear_persisted\":false,\"anchor_retired\":false}," +
		"\"provider_action\":{\"attempted\":false,\"completed_count\":0},\"health\":{\"attempted\":false,\"healthy\":false," +
		"\"peer_verified\":false,\"network_namespace_verified\":false,\"network_namespace\":{\"device\":0,\"inode\":0}}}\n"

	var output bytes.Buffer
	err := writeLinuxV2NativeWitnessSummaryAndExit(&output, summary)
	if !errors.Is(err, errLinuxV2NativeWitnessContainmentUnproven) {
		t.Fatalf("unresolved owner witness error = %v, want the deliberate containment-unproven exit", err)
	}
	if output.String() != want {
		t.Fatalf("unresolved owner witness response = %q, want %q", output.String(), want)
	}

	writeErr := errors.New("owner witness output is closed")
	err = writeLinuxV2NativeWitnessSummaryAndExit(linuxV2NativeWitnessFailingWriter{err: writeErr}, summary)
	if errors.Is(err, errLinuxV2NativeWitnessContainmentUnproven) || !errors.Is(err, writeErr) {
		t.Fatalf("closed owner witness output error = %v, want the write error without the sentinel", err)
	}

	for _, proven := range []struct {
		outcome string
		adjust  func(*linuxV2NativeWitnessSummary)
	}{
		{outcome: "no_native_effects", adjust: func(summary *linuxV2NativeWitnessSummary) {
			summary.RecoveryState = "no_native_effects"
			summary.JournalUnproven = false
			summary.FailureCategory = ""
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		}},
		{outcome: "aborted_no_target", adjust: func(summary *linuxV2NativeWitnessSummary) {
			summary.RecoveryState = "aborted_no_target"
			summary.JournalUnproven = false
			summary.NoTargetAbort = true
			summary.FailureCategory = ""
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		}},
		{outcome: "recovered", adjust: func(summary *linuxV2NativeWitnessSummary) {
			summary.RecoveryState = "released"
			summary.JournalUnproven = false
			summary.FailureCategory = ""
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseReleased
			summary.Stages.NativeReleased = true
		}},
	} {
		provenSummary := summary
		provenSummary.Outcome = proven.outcome
		proven.adjust(&provenSummary)
		var provenOutput bytes.Buffer
		if err := writeLinuxV2NativeWitnessSummaryAndExit(&provenOutput, provenSummary); err != nil {
			t.Errorf("%s owner witness error = %v, want exit 0", proven.outcome, err)
		}
		if provenOutput.Len() == 0 {
			t.Errorf("%s owner witness wrote no response bytes", proven.outcome)
		}
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

func linuxV2NativeWitnessExactPhysicalStop() linuxV2NativeWitnessStop {
	return linuxV2NativeWitnessStop{
		ReceiptVersion: authority.SupervisorVersionV2, ReceiptStatus: "stopped", ProofKind: "helper-cgroup-kill",
		Issuer:           linuxV2NativeWitnessProcess{PID: 60, Identity: "helper:60:start"},
		Reaper:           linuxV2NativeWitnessProcess{PID: 10, Identity: "anchor:10:start"},
		KillAcknowledged: true, PopulatedZero: true, NoMigrationObserved: true,
		ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: linuxV2NativeWitnessExit{PID: 61, Identity: "launcher:61:start", Waited: true, Reaped: true},
		GateExit:     linuxV2NativeWitnessExit{PID: 62, Identity: "gate:62:start", Waited: true, Reaped: true},
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
			Stop: linuxV2NativeWitnessExactPhysicalStop(),
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
			Stop:               linuxV2NativeWitnessExactPhysicalStop(),
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

func TestLinuxV2NativeWitnessRecoveryRefusalNamesTheGuard(t *testing.T) {
	tests := []struct {
		name       string
		caseName   string
		journal    state.RunJournal
		wantRefuse bool
		wantGuard  string
		wantReason string
	}{
		{
			name: "journal with unresolved authority", caseName: "stage_replay",
			journal: state.RunJournal{ContainmentHandoff: &authority.SupervisorHandoff{}},
		},
		{
			name: "clone intent alone keeps the authority", caseName: "stage_replay",
			journal: state.RunJournal{LinuxCloneIntent: &authority.LinuxCloneIntent{}},
		},
		{
			name: "released proof refuses", caseName: "stage_replay",
			journal:    state.RunJournal{ContainmentCgroupReleaseProof: &authority.SupervisorHandoffReleaseProof{}},
			wantRefuse: true, wantGuard: "journal_without_unresolved_authority", wantReason: "release_proof_present",
		},
		{
			name: "pre-gate release proof refuses", caseName: "stage_replay",
			journal:    state.RunJournal{ContainmentCgroupPreGateReleaseProof: &authority.LinuxCgroupPreGateReleaseProof{}},
			wantRefuse: true, wantGuard: "journal_without_unresolved_authority", wantReason: "release_proof_present",
		},
		{
			name: "clone abort proof refuses", caseName: "stage_replay",
			journal: state.RunJournal{
				LinuxCloneIntent: &authority.LinuxCloneIntent{}, LinuxCloneAbortProof: &authority.LinuxCloneAbortProof{},
			},
			wantRefuse: true, wantGuard: "journal_without_unresolved_authority", wantReason: "clone_abort_proof_present",
		},
		{
			name: "clone promotion receipt refuses", caseName: "stage_replay",
			journal: state.RunJournal{
				LinuxCloneIntent: &authority.LinuxCloneIntent{}, LinuxClonePromotionReceipt: &authority.LinuxClonePromotionReceipt{},
			},
			wantRefuse: true, wantGuard: "journal_without_unresolved_authority", wantReason: "clone_promotion_receipt_present",
		},
		{
			name: "empty journal refuses", caseName: "stage_replay", journal: state.RunJournal{},
			wantRefuse: true, wantGuard: "journal_without_unresolved_authority", wantReason: "no_unresolved_term",
		},
		{
			name: "another case refuses its own way", caseName: "codex_containment",
			journal:    state.RunJournal{ContainmentHandoff: &authority.SupervisorHandoff{}},
			wantRefuse: true, wantGuard: "case_not_stage_replay",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			refusal, refused := linuxV2NativeWitnessRecoveryRefusal(test.caseName, test.journal, errors.New("recover v2 owner run/1: pinned leaf mismatch"))
			if refused != test.wantRefuse {
				t.Fatalf("refused = %t, want %t (%+v)", refused, test.wantRefuse, refusal)
			}
			if !test.wantRefuse {
				return
			}
			if refusal.Guard != test.wantGuard || refusal.Reason != test.wantReason {
				t.Fatalf("refusal = %+v, want guard=%q reason=%q", refusal, test.wantGuard, test.wantReason)
			}
			if refusal.RecoveryErrorClass != "recovery_failed" {
				t.Fatalf("refusal recovery error class = %q, want the bounded default", refusal.RecoveryErrorClass)
			}
			if err := refusal.Validate(); err != nil {
				t.Fatalf("refusal code is not closed-vocabulary: %v", err)
			}
		})
	}
	refusal, refused := linuxV2NativeWitnessRecoveryRefusal("stage_replay",
		state.RunJournal{ContainmentCgroupReleaseProof: &authority.SupervisorHandoffReleaseProof{}},
		fmt.Errorf("recover v2 owner run/1: %w", context.DeadlineExceeded))
	if !refused || refusal.String() != "refusal=journal_without_unresolved_authority reason=release_proof_present recovery_error_class=timeout" {
		t.Fatalf("refusal rendering = %q, want the named guard, reason and class", refusal.String())
	}
}

func TestLinuxV2NativeWitnessRecoveryErrorClassClosesTheVocabulary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "no recovery error", err: nil, want: ""},
		{name: "cancelled", err: fmt.Errorf("recover v2 owner run/1: %w", context.Canceled), want: "cancelled"},
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout"},
		{name: "owner lost", err: platform.ErrLinuxOwnerHelperV2OwnerLost, want: "owner_lost"},
		{name: "helper recovery only", err: platform.ErrLinuxOwnerHelperV2RecoveryOnly, want: "helper_recovery_only"},
		{name: "outcome unknown", err: linuxgate.ErrOutcomeUnknown, want: "outcome_unknown"},
		{name: "gate never produced", err: linuxgate.ErrGateNeverProduced, want: "gate_never_produced"},
		{name: "cgroup unresolved", err: platform.ErrLinuxCgroupV2Unresolved, want: "cgroup_unresolved"},
		{name: "free-form recovery failure", err: errors.New("recover v2 owner run/1: pinned leaf mismatch"), want: "recovery_failed"},
		{
			name: "cancellation beats owner loss",
			err:  errors.Join(platform.ErrLinuxOwnerHelperV2OwnerLost, context.Canceled), want: "cancelled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxV2NativeWitnessRecoveryErrorClass(test.err); got != test.want {
				t.Fatalf("recovery error class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLinuxV2NativeWitnessRefusalDocumentIsIdentityOnly(t *testing.T) {
	identityFields := []string{
		"version", "case", "harness", "native_version", "installed_owner_verified", "run_key", "fence", "subject",
		"recovery_refusal",
	}
	document := linuxV2NativeWitnessRefusalDocument{
		Version: 1, Case: "stage_replay", Harness: "pi", NativeVersion: "1.5.2", InstalledOwnerVerified: true,
		RunKey: linuxV2NativeWitnessRunKey{RunID: "run-1", Generation: 2},
		Fence:  linuxV2NativeWitnessFenceSummary{RuntimeID: "runtime-1", RuntimeEpoch: 3, Generation: 2, ClaimID: "claim-1"},
		Subject: protocol.Subject{
			ResourceID: "resource-1", Commit: "1111111111111111111111111111111111111111", TreeDigest: "sha256:tree-1",
		},
		RecoveryRefusal: linuxV2NativeWitnessRefusal{
			Guard: "journal_without_unresolved_authority", Reason: "release_proof_present", RecoveryErrorClass: "recovery_failed",
		},
	}
	var encoded bytes.Buffer
	if err := writeLinuxV2NativeWitnessRefusal(&encoded, document); err != nil {
		t.Fatalf("write owner witness refusal document: %v", err)
	}
	raw := encoded.Bytes()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode owner witness refusal document: %v", err)
	}
	if len(decoded) != len(identityFields) {
		t.Fatalf("refusal document fields = %d, want the %d identity fields", len(decoded), len(identityFields))
	}
	for _, field := range identityFields {
		if _, present := decoded[field]; !present {
			t.Fatalf("refusal document omitted %q", field)
		}
	}
	if err := validateLinuxV2NativeWitnessRefusalDocument(raw); err != nil {
		t.Fatalf("identity-only refusal document was refused: %v", err)
	}
	genericDocument := func(decoded map[string]json.RawMessage) map[string]any {
		document := make(map[string]any, len(decoded))
		for key, rawValue := range decoded {
			var generic any
			if err := json.Unmarshal(rawValue, &generic); err != nil {
				t.Fatalf("re-decode refusal field %s: %v", key, err)
			}
			document[key] = generic
		}
		return document
	}
	authorityFields := map[string]any{
		"outcome":             "unresolved",
		"failure_category":    "containment_unproven",
		"recovery_state":      "unresolved",
		"journal_unproven":    true,
		"no_target_abort":     false,
		"response_lost_after": "stop",
		"stages":              map[string]any{"journal_cleared": true},
		"release":             map[string]any{"clear_persisted": true},
		"stop":                map[string]any{"receipt_version": authority.SupervisorVersionV2},
	}
	for field, value := range authorityFields {
		mutated := genericDocument(decoded)
		mutated[field] = value
		mutatedJSON, err := json.Marshal(mutated)
		if err != nil {
			t.Fatalf("encode mutated refusal document: %v", err)
		}
		if err := validateLinuxV2NativeWitnessRefusalDocument(mutatedJSON); err == nil {
			t.Fatalf("refusal document with %q authority was accepted", field)
		}
	}
	missing := genericDocument(decoded)
	delete(missing, "recovery_refusal")
	missingJSON, err := json.Marshal(missing)
	if err != nil {
		t.Fatalf("encode refusal document without its refusal code: %v", err)
	}
	if err := validateLinuxV2NativeWitnessRefusalDocument(missingJSON); err == nil {
		t.Fatal("refusal document without its refusal code was accepted")
	}
	freeForm := genericDocument(decoded)
	freeForm["recovery_refusal"] = map[string]any{"guard": "journal_without_unresolved_authority", "reason": "/opt/symmetry-owner-test/leaf"}
	freeFormJSON, err := json.Marshal(freeForm)
	if err != nil {
		t.Fatalf("encode free-form refusal: %v", err)
	}
	if err := validateLinuxV2NativeWitnessRefusalDocument(freeFormJSON); err == nil {
		t.Fatal("free-form refusal reason was accepted")
	}
}

func TestLinuxV2NativeWitnessUnresolvedSummaryTrippedNamesPredicates(t *testing.T) {
	retained := func() linuxV2NativeWitnessSummary {
		return linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "containment_unproven", JournalUnproven: true,
		}
	}
	tests := []struct {
		name   string
		mutate func(*linuxV2NativeWitnessSummary)
		want   []string
	}{
		{name: "retained journal", mutate: func(*linuxV2NativeWitnessSummary) {}},
		{name: "outcome", mutate: func(s *linuxV2NativeWitnessSummary) { s.Outcome = "recovered" }, want: []string{"outcome"}},
		{
			name: "failure category", mutate: func(s *linuxV2NativeWitnessSummary) { s.FailureCategory = "" },
			want: []string{"failure_category"},
		},
		{
			name: "journal unproven", mutate: func(s *linuxV2NativeWitnessSummary) { s.JournalUnproven = false },
			want: []string{"journal_unproven"},
		},
		{
			name: "no-target abort", mutate: func(s *linuxV2NativeWitnessSummary) { s.NoTargetAbort = true },
			want: []string{"no_target_abort"},
		},
		{
			name: "anchor retired", mutate: func(s *linuxV2NativeWitnessSummary) { s.Release.AnchorRetired = true },
			want: []string{"release.anchor_retired"},
		},
		{
			name: "opaque release", mutate: func(s *linuxV2NativeWitnessSummary) { s.Release.OpaquePlatformRelease = true },
			want: []string{"release.opaque_platform_release"},
		},
		{
			name: "clear persisted", mutate: func(s *linuxV2NativeWitnessSummary) { s.Release.ClearPersisted = true },
			want: []string{"release.clear_persisted"},
		},
		{
			name: "leaf removed", mutate: func(s *linuxV2NativeWitnessSummary) { s.Release.LeafRemoved = true },
			want: []string{"release.leaf_removed"},
		},
		{
			name: "exact versioned stop receipt",
			mutate: func(s *linuxV2NativeWitnessSummary) {
				s.Stop = linuxV2NativeWitnessExactPhysicalStop()
			},
		},
		{
			name: "versioned receipt without physical proof",
			mutate: func(s *linuxV2NativeWitnessSummary) {
				s.Stop.ReceiptVersion = authority.SupervisorVersionV2
			},
			want: []string{"stop.receipt_version"},
		},
		{
			name: "unknown stop receipt version",
			mutate: func(s *linuxV2NativeWitnessSummary) {
				s.Stop.ReceiptVersion = authority.SupervisorVersionV2 + 1
			},
			want: []string{"stop.receipt_version"},
		},
		{
			name: "absent stop receipt version stays untripped",
			mutate: func(s *linuxV2NativeWitnessSummary) {
				s.Stop = linuxV2NativeWitnessStop{ReceiptStatus: "stopped", ProofKind: "helper-cgroup-kill"}
			},
		},
		{
			name: "versioned receipt without its physical flags",
			mutate: func(s *linuxV2NativeWitnessSummary) {
				s.Stop = linuxV2NativeWitnessStop{
					ReceiptVersion: authority.SupervisorVersionV2, ReceiptStatus: "stopped", ProofKind: "helper-cgroup-kill",
					Issuer:           linuxV2NativeWitnessProcess{PID: 60, Identity: "helper:60:start"},
					Reaper:           linuxV2NativeWitnessProcess{PID: 10, Identity: "anchor:10:start"},
					KillAcknowledged: true, PopulatedZero: true, NoMigrationObserved: true,
					ProducerFenced: true, DescendantsReaped: false,
					LauncherExit: linuxV2NativeWitnessExit{PID: 61, Identity: "launcher:61:start", Waited: true, Reaped: true},
					GateExit:     linuxV2NativeWitnessExit{PID: 62, Identity: "gate:62:start", Waited: true, Reaped: true},
				}
			},
			want: []string{"stop.receipt_version"},
		},
		{
			name: "journal cleared", mutate: func(s *linuxV2NativeWitnessSummary) { s.Stages.JournalCleared = true },
			want: []string{"stages.journal_cleared"},
		},
		{
			name:   "ordered multi-trip",
			mutate: func(s *linuxV2NativeWitnessSummary) { s.Outcome = "completed"; s.NoTargetAbort = true },
			want:   []string{"outcome", "no_target_abort"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := retained()
			test.mutate(&summary)
			tripped := linuxV2NativeWitnessUnresolvedSummaryTripped(summary)
			if strings.Join(tripped, ",") != strings.Join(test.want, ",") {
				t.Fatalf("tripped predicates = %v, want %v", tripped, test.want)
			}
		})
	}
}

func TestLinuxV2NativeWitnessUnresolvedSummaryErrorNamesTheTrippedPredicate(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "stage_replay"},
		summary: linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "containment_unproven", JournalUnproven: true,
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseUnknown,
			Stop:               linuxV2NativeWitnessStop{ReceiptVersion: authority.SupervisorVersionV2},
		},
	}
	err := witness.validateFinalSummary()
	if err == nil || !strings.Contains(err.Error(), "stage replay unresolved outcome lacks retained journal/owner uncertainty: tripped stop.receipt_version") {
		t.Fatalf("unresolved summary refusal = %v, want the named tripped predicate", err)
	}
}

func TestLinuxV2NativeWitnessAllowsExactVersionedStopForUnresolvedRecovery(t *testing.T) {
	witness := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "stage_replay"},
		summary: linuxV2NativeWitnessSummary{
			Outcome: "unresolved", RecoveryState: "unresolved", FailureCategory: "containment_unproven", JournalUnproven: true,
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseReleased,
			Stages:             linuxV2NativeWitnessStages{NativeReleased: true, StopReceiptDurable: true},
			Stop:               linuxV2NativeWitnessExactPhysicalStop(),
		},
	}
	if err := witness.validateFinalSummary(); err != nil {
		t.Fatalf("exact versioned physical-stop proof was refused by the unresolved branch: %v", err)
	}
	witness.summary.Stop.DescendantsReaped = false
	if err := witness.validateFinalSummary(); err == nil || !strings.Contains(err.Error(), "tripped stop.receipt_version") {
		t.Fatalf("inexact versioned stop receipt was accepted: %v", err)
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
	share := linuxpreflight.StatIdentity{UID: owner.AUID, GID: owner.BGID, Mode: 0o2750}
	if err := linuxpreflight.ValidatePiExtensionShareIdentity(true, share, owner); err != nil {
		t.Fatalf("correct A-owned/B-group share rejected: %v", err)
	}
	share.GID = owner.AGID
	if err := linuxpreflight.ValidatePiExtensionShareIdentity(true, share, owner); err == nil {
		t.Fatal("Pi extension share with A's group was accepted")
	}
	share.GID = owner.BGID
	share.Mode = 0o2770
	if err := linuxpreflight.ValidatePiExtensionShareIdentity(true, share, owner); err == nil {
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

func TestLinuxV2NativeWitnessCodexCaseRequiresCodexHarness(t *testing.T) {
	if !linuxV2NativeWitnessCaseMatches("codex_native_turn", config.RuntimeHarnessCodex) {
		t.Fatal("Codex witness case did not select the Codex harness")
	}
	for _, other := range []string{config.RuntimeHarnessPi, config.RuntimeHarnessOpenCode, config.RuntimeHarnessGeneric} {
		if linuxV2NativeWitnessCaseMatches("codex_native_turn", other) {
			t.Fatalf("Codex witness case selected harness %q", other)
		}
	}
}

func TestLinuxV2NativeWitnessContainmentCasesMatchTheirKind(t *testing.T) {
	for _, accepted := range []struct {
		name    string
		harness string
	}{
		{"codex_containment", config.RuntimeHarnessCodex},
		{"claude_containment", config.RuntimeHarnessClaudeCode},
		{"stage_replay", config.RuntimeHarnessCodex},
		{"stage_replay", config.RuntimeHarnessClaudeCode},
	} {
		if !linuxV2NativeWitnessCaseMatches(accepted.name, accepted.harness) {
			t.Fatalf("case %q did not select harness %q", accepted.name, accepted.harness)
		}
	}
	for _, rejected := range []struct {
		name    string
		harness string
	}{
		{"codex_containment", config.RuntimeHarnessClaudeCode},
		{"claude_containment", config.RuntimeHarnessCodex},
		{"codex_containment", config.RuntimeHarnessGeneric},
		{"claude_containment", config.RuntimeHarnessOpenCode},
		{"stage_replay", config.RuntimeHarnessGeneric},
	} {
		if linuxV2NativeWitnessCaseMatches(rejected.name, rejected.harness) {
			t.Fatalf("case %q accepted harness %q", rejected.name, rejected.harness)
		}
	}
}

func TestLinuxV2NativeWitnessContainmentSelectsTheOpenOnlyPath(t *testing.T) {
	run := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Case: "codex_containment", Harness: config.RuntimeHarnessCodex}}
	if !run.containmentCase() {
		t.Fatal("codex containment case did not select the credential-free Open path")
	}
	run.request = linuxV2NativeWitnessRequest{Case: "claude_containment", Harness: config.RuntimeHarnessClaudeCode}
	if !run.containmentCase() {
		t.Fatal("claude containment case did not select the credential-free Open path")
	}
	run.request = linuxV2NativeWitnessRequest{Case: "stage_replay", Harness: config.RuntimeHarnessCodex}
	if !run.containmentCase() {
		t.Fatal("codex native-release replay leg did not select the credential-free Open path")
	}
	run.request = linuxV2NativeWitnessRequest{Case: "stage_replay", Harness: config.RuntimeHarnessOpenCode}
	if run.containmentCase() {
		t.Fatal("OpenCode stage replay was routed through the containment-only path")
	}
	run.request = linuxV2NativeWitnessRequest{Case: "codex_native_turn", Harness: config.RuntimeHarnessCodex}
	if run.containmentCase() {
		t.Fatal("codex_native_turn lost its turn path")
	}
}

func TestLinuxV2NativeWitnessContainmentSummaryRequiresTheOpenBoundary(t *testing.T) {
	run := &linuxV2NativeWitnessRun{
		request: linuxV2NativeWitnessRequest{Case: "codex_containment", Harness: config.RuntimeHarnessCodex},
		summary: linuxV2NativeWitnessSummary{
			Case:    "codex_containment",
			Harness: config.RuntimeHarnessCodex,
			Stages: linuxV2NativeWitnessStages{
				ReservationDurable: true, CreateStarted: true, LeafBound: true, HelperBound: true,
				LauncherBound: true, GateBound: true, HandoffPrepared: true, HandoffCommitted: true,
				InitialLeaseAcked: true, CheckoutReady: true, NativeReleased: true,
			},
			Owners: linuxV2NativeWitnessOwners{Gate: linuxV2NativeWitnessProcess{PID: 42, Identity: "target"}},
			Target: linuxV2NativeWitnessTarget{
				PID: 42, Identity: "target", PIDFDVerified: true, SamePIDExec: true,
				UserNamespace:    linuxV2NativeWitnessNamespace{Device: 1, Inode: 2},
				NetworkNamespace: linuxV2NativeWitnessNamespace{Device: 3, Inode: 4},
			},
			NativeReleaseState: linuxV2NativeWitnessNativeReleaseReleased,
			NativeBinary:       "/usr/local/bin/codex",
			NativeBinarySHA256: "sha256:" + strings.Repeat("a", 64),
			Stop: linuxV2NativeWitnessStop{
				ReceiptVersion: authority.SupervisorHandoffVersionV2, KillAcknowledged: true, PopulatedZero: true,
				NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
			},
			Release: linuxV2NativeWitnessRelease{
				OpaquePlatformRelease: true, LeafRemoved: true, ClearPersisted: true, AnchorRetired: true,
			},
		},
	}
	if err := run.validateFinalSummary(); err == nil || !strings.Contains(err.Error(), "credential-free native Open") {
		t.Fatalf("containment summary without the Open boundary error = %v", err)
	}
	run.summary.Containment = &linuxV2NativeWitnessContainment{NativeOpenReached: true}
	run.summary.NativeBinary = ""
	if err := run.validateFinalSummary(); err == nil || !strings.Contains(err.Error(), "executable path or digest") {
		t.Fatalf("containment summary without the binary digest error = %v", err)
	}
	run.summary.NativeBinary = "/usr/local/bin/codex"
	if err := run.validateFinalSummary(); err != nil {
		t.Fatalf("satisfied containment boundary was rejected: %v", err)
	}
}

func TestLinuxV2NativeWitnessConfigRequiresThePinnedKindVersion(t *testing.T) {
	owner := linuxgate.OwnerConfig{
		AUID: 60000, AGID: 60000, BUID: 60001, BGID: 60001,
		StateDir: linuxV2NativeOwnerWitnessStateDir,
	}
	for _, testCase := range []struct {
		kind    string
		version string
	}{
		{config.RuntimeHarnessCodex, codex.TestedVersion},
		{config.RuntimeHarnessClaudeCode, claude.TestedVersion},
	} {
		value := validLinuxV2NativeWitnessConfig(owner)
		profile := value.AgentProfiles["profile"]
		profile.ProviderAccess = false
		if testCase.kind == config.RuntimeHarnessCodex {
			profile.NativeModel = "gpt-5.1-codex"
			profile.NativeModelProvider = "openai"
		}
		value.AgentProfiles["profile"] = profile
		value.Runtime.HarnessKind = testCase.kind
		value.Runtime.HarnessVersion = testCase.version
		if err := validateLinuxV2NativeWitnessConfig(value, owner); err != nil {
			t.Fatalf("%s witness config with the pinned release rejected: %v", testCase.kind, err)
		}
		foreign := value
		foreign.Runtime.HarnessVersion = "0.0.0"
		if err := validateLinuxV2NativeWitnessConfig(foreign, owner); err == nil || !strings.Contains(err.Error(), "tested release") {
			t.Fatalf("%s witness config with a foreign release error = %v", testCase.kind, err)
		}
	}
}

// testLinuxV2NativeWitnessRetainedCloneJournal builds the released pre-checkout
// clone value the ordinary release leaves behind, through the App clone callbacks.
func testLinuxV2NativeWitnessRetainedCloneJournal(t *testing.T) state.RunJournal {
	t.Helper()
	store, key, _, grant, _ := linuxCloneCallbackFixtureAt(t, t.TempDir())
	defer store.Close()
	handoff, owner, _ := bindAppLinuxV2Owner(t, store, key, testAppLinuxV2Reservation())
	proof := linuxCloneAppReleaseProof(t, handoff, appLinuxV2StopReceipt(t, owner))
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	intent, seed := grant.Intent, grant.Seed
	journal.LinuxCloneIntent = &intent
	journal.LinuxCloneSeedReceipt = &seed
	journal.WorkspacePath = intent.Target
	journal.WorkspaceBindingKey = intent.BindingKey
	journal.WorkspaceRecoveryRequired = true
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	journal.ContainmentCgroup = nil
	journal.ContainmentAuthority = nil
	journal.ContainmentHandoff = nil
	journal.ContainmentScans = nil
	journal.ContainmentUnproven = false
	journal.ContainmentCgroupReleaseProof = &proof
	return journal
}

func TestLinuxV2NativeWitnessCloneRetentionNamesOnlyTheRetainedRelease(t *testing.T) {
	t.Parallel()
	retained := testLinuxV2NativeWitnessRetainedCloneJournal(t)
	if !linuxV2NativeWitnessCloneRetainedAfterRelease(retained) {
		t.Fatal("the released pre-checkout clone is not recognized as the retained shape")
	}
	if !linuxV2NativeWitnessCloneRetentionAfterRelease(retained, retained) {
		t.Fatal("an unchanged released pre-checkout clone is not classified as its deliberate retention")
	}
	cases := []struct {
		name   string
		before func(state.RunJournal) state.RunJournal
		after  func(state.RunJournal) state.RunJournal
	}{
		{
			name: "the cleanup attempt changed the retained row",
			before: func(journal state.RunJournal) state.RunJournal {
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				journal.RetainWorkspace = true
				return journal
			},
		},
		{
			name: "the row carries a checkout receipt",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneCheckoutReceipt = &authority.LinuxCloneCheckoutReceipt{}
				return journal
			},
		},
		{
			name: "the row carries a promotion receipt",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxClonePromotionReceipt = &authority.LinuxClonePromotionReceipt{}
				return journal
			},
		},
		{
			name: "the row carries a clone cleanup intent",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneCleanupIntent = &authority.LinuxCloneCleanupIntent{}
				return journal
			},
		},
		{
			name: "the row carries a pending clone abort",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneAbortPending = true
				return journal
			},
		},
		{
			name: "the row is not physically released",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.ContainmentCgroupReleaseProof = nil
				return journal
			},
		},
		{
			name: "the retained seed receipt is missing",
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneSeedReceipt = nil
				return journal
			},
		},
		{
			name: "the row no longer requires workspace recovery",
			before: func(journal state.RunJournal) state.RunJournal {
				journal.WorkspaceRecoveryRequired = false
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				journal.WorkspaceRecoveryRequired = false
				return journal
			},
		},
		{
			name: "the row's workspace binding differs from its clone intent",
			before: func(journal state.RunJournal) state.RunJournal {
				journal.WorkspaceBindingKey = "other-binding"
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				journal.WorkspaceBindingKey = "other-binding"
				return journal
			},
		},
		{
			name: "the row carries a clone reuse grant",
			before: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneReuseGrant = &authority.LinuxCloneReuseGrant{}
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				journal.LinuxCloneReuseGrant = &authority.LinuxCloneReuseGrant{}
				return journal
			},
		},
		{
			name: "the row requires native usage recovery",
			before: func(journal state.RunJournal) state.RunJournal {
				journal.NativeUsageRecoveryRequired = true
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				journal.NativeUsageRecoveryRequired = true
				return journal
			},
		},
		{
			name: "the row's clone intent is no longer journal-valid",
			before: func(journal state.RunJournal) state.RunJournal {
				intent := *journal.LinuxCloneIntent
				intent.BindingKey = ""
				journal.LinuxCloneIntent = &intent
				return journal
			},
			after: func(journal state.RunJournal) state.RunJournal {
				intent := *journal.LinuxCloneIntent
				intent.BindingKey = ""
				journal.LinuxCloneIntent = &intent
				return journal
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before, after := retained, retained
			if test.before != nil {
				before = test.before(retained)
			}
			after = test.after(retained)
			if linuxV2NativeWitnessCloneRetentionAfterRelease(before, after) {
				t.Fatal("a non-retention cleanup outcome was named as the deliberate clone retention")
			}
		})
	}
}

func TestLinuxV2NativeWitnessCloneRetentionSummaryVocabulary(t *testing.T) {
	t.Parallel()
	base := linuxV2NativeWitnessSummary{
		Case: "stage_replay", RecoveryState: "released",
		Clone: linuxV2NativeWitnessClone{IntentSHA256: strings.Repeat("a", 64), SeedDigest: "sha256:" + strings.Repeat("b", 64)},
	}
	if err := linuxV2NativeWitnessCloneRetentionError("stage_replay", base); err != nil {
		t.Fatalf("a summary without a named retention was refused: %v", err)
	}
	named := base
	named.Clone.Retention = linuxV2NativeWitnessCloneRetentionRetainedAfterRelease
	if err := linuxV2NativeWitnessCloneRetentionError("stage_replay", named); err != nil {
		t.Fatalf("the exact retained outcome was refused: %v", err)
	}
	cases := []struct {
		name     string
		caseName string
		mutate   func(*linuxV2NativeWitnessSummary)
	}{
		{name: "unknown retention code", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.Clone.Retention = "retained"
		}},
		{name: "another case", caseName: "codex_containment", mutate: func(summary *linuxV2NativeWitnessSummary) {}},
		{name: "another recovery state", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.RecoveryState = "unresolved"
		}},
		{name: "unproven journal", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.JournalUnproven = true
		}},
		{name: "checked out clone", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.Clone.CheckoutReady = true
		}},
		{name: "missing intent digest", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.Clone.IntentSHA256 = ""
		}},
		{name: "missing seed digest", caseName: "stage_replay", mutate: func(summary *linuxV2NativeWitnessSummary) {
			summary.Clone.SeedDigest = ""
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			summary := named
			test.mutate(&summary)
			if err := linuxV2NativeWitnessCloneRetentionError(test.caseName, summary); err == nil {
				t.Fatal("an inconsistent clone retention outcome was accepted")
			}
		})
	}
}

// Only the adapters that consume harness.StartRequest.StartProcess hand the
// witness the exact Process, so only they stage the release from daemon.start.
func TestLinuxV2NativeWitnessReleaseStagingFollowsTheLaunchSeam(t *testing.T) {
	for _, test := range []struct {
		kind harness.Kind
		want bool
	}{
		{kind: harness.KindCodex, want: true},
		{kind: harness.KindClaude, want: true},
		{kind: harness.KindPi, want: false},
		{kind: harness.KindOpenCode, want: false},
	} {
		if got := linuxV2NativeWitnessStagesReleaseFromStartProcess(test.kind); got != test.want {
			t.Errorf("witness stages the release from daemon.start for %q = %t, want %t", test.kind, got, test.want)
		}
	}
}

// linuxV2WitnessReleaseJournalStore serves one synthetic RunJournal; the release
// seam under test reaches LoadJournal only.
type linuxV2WitnessReleaseJournalStore struct {
	journalStore
	journal *state.RunJournal
}

func (store linuxV2WitnessReleaseJournalStore) LoadJournal(state.RunKey) (state.RunJournal, error) {
	return *store.journal, nil
}

type linuxV2WitnessReleaseFixture struct {
	witness   *linuxV2NativeWitnessRun
	target    authority.LinuxCgroupProcessIdentity
	callbacks *execution.LinuxV2JournalCallbacks
	store     linuxV2WitnessReleaseJournalStore
	journal   *state.RunJournal
	marker    string
	release   string
}

func newLinuxV2WitnessReleaseFixture(t *testing.T, hold string) linuxV2WitnessReleaseFixture {
	return newLinuxV2WitnessReleaseFixtureWithJournal(t, hold, nil)
}

func newLinuxV2WitnessReleaseFixtureWithJournal(t *testing.T, hold string, mutate func(*state.RunJournal)) linuxV2WitnessReleaseFixture {
	t.Helper()
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("read the exact live release target identity: %v", err)
	}
	target := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity}
	fence := protocol.Fence{
		RuntimeID: "witness-release-runtime", RuntimeEpoch: 4, Generation: 1,
		ClaimID: "witness-release-claim", LeaseToken: "witness-release-lease",
	}
	key := state.RunKey{RunID: "witness-release-run", Generation: fence.Generation}
	request := linuxV2NativeWitnessRequest{
		Case: "stage_replay", Harness: config.RuntimeHarnessPi,
		RunKey: linuxV2NativeWitnessRunKey{RunID: key.RunID, Generation: key.Generation},
		Fence:  fence,
		Barrier: linuxV2NativeWitnessBarrier{
			Directory: t.TempDir(), TimeoutSeconds: 30, HoldAfter: hold,
		},
	}
	request.Barrier.ReleaseFile = filepath.Join(request.Barrier.Directory, "release")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	// The barriers are built directly: the production constructor requires the
	// root-installed fixture root that only the guest has.
	witness := &linuxV2NativeWitnessRun{
		ctx: ctx, request: request,
		ownerConfig: linuxgate.OwnerConfig{AUID: uint32(os.Geteuid()), BUID: uint32(os.Geteuid())},
		barrier: &linuxV2NativeWitnessBarriers{
			request: request.Barrier, caseName: request.Case, runKey: request.RunKey, aUID: uint32(os.Geteuid()),
		},
	}
	journal := state.RunJournal{
		RunID: key.RunID, Generation: key.Generation, RuntimeID: fence.RuntimeID,
		ClaimedRuntimeEpoch: fence.RuntimeEpoch, ClaimID: fence.ClaimID, LeaseToken: fence.LeaseToken,
		RunContainmentState: state.RunContainmentState{
			PID: target.PID, ProcessIdentity: target.Identity,
			ContainmentCgroup:    &state.LinuxCgroupReservation{Gate: &authority.LinuxCgroupGateIdentity{Process: target}},
			ContainmentAuthority: &authority.Supervisor{TargetPID: target.PID, TargetIdentity: target.Identity},
		},
	}
	if mutate != nil {
		mutate(&journal)
	}
	store := linuxV2WitnessReleaseJournalStore{journal: &journal}
	callbacks := execution.LinuxV2JournalCallbacks{}
	if err := witness.wrapLinuxV2Callbacks(nil, store, &callbacks); err != nil {
		t.Fatalf("wrap the owner witness release callbacks: %v", err)
	}
	t.Cleanup(func() { _ = witness.closeCapturedFDs() })
	return linuxV2WitnessReleaseFixture{
		witness: witness, target: target, callbacks: &callbacks, store: store, journal: &journal,
		marker:  filepath.Join(request.Barrier.Directory, linuxV2NativeWitnessReleaseStage+".json"),
		release: request.Barrier.ReleaseFile,
	}
}

func (fixture linuxV2WitnessReleaseFixture) readMarker(t *testing.T) linuxV2NativeWitnessStageMarker {
	t.Helper()
	return fixture.readStageMarker(t, linuxV2NativeWitnessReleaseStage)
}

func (fixture linuxV2WitnessReleaseFixture) readStageMarker(t *testing.T, stage string) linuxV2NativeWitnessStageMarker {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(fixture.marker), stage+".json"))
	if err != nil {
		t.Fatalf("read the %s stage marker: %v", stage, err)
	}
	var marker linuxV2NativeWitnessStageMarker
	if err := json.Unmarshal(contents, &marker); err != nil {
		t.Fatalf("decode the %s stage marker: %v", stage, err)
	}
	return marker
}

func (fixture linuxV2WitnessReleaseFixture) writeRelease(t *testing.T) {
	t.Helper()
	staged := fixture.release + ".staged"
	if err := os.WriteFile(staged, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("write the witness release marker: %v", err)
	}
	if err := os.Rename(staged, fixture.release); err != nil {
		t.Fatalf("publish the witness release marker: %v", err)
	}
}

func TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget(t *testing.T) {
	fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
	done := make(chan error, 1)
	go func() { done <- fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, false) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Lstat(fixture.marker); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect the release stage marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the runner release path never staged its requested durable stage")
		}
		select {
		case err := <-done:
			t.Fatalf("release callback returned before its held stage: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	fixture.writeRelease(t)
	if err := <-done; err != nil {
		t.Fatalf("release callback after its held stage: %v", err)
	}

	var userNamespace, networkNamespace unix.Stat_t
	if err := unix.Stat("/proc/self/ns/user", &userNamespace); err != nil {
		t.Fatalf("stat the live user namespace: %v", err)
	}
	if err := unix.Stat("/proc/self/ns/net", &networkNamespace); err != nil {
		t.Fatalf("stat the live network namespace: %v", err)
	}
	marker := fixture.readMarker(t)
	if marker.Stage != linuxV2NativeWitnessReleaseStage || marker.Case != "stage_replay" ||
		marker.HoldAfter != linuxV2NativeWitnessReleaseStage ||
		marker.NativeReleaseState != linuxV2NativeWitnessNativeReleaseReleased {
		t.Fatalf("release stage marker = %#v, want the exact held released stage", marker)
	}
	if marker.Target.PID != fixture.target.PID || marker.Target.Identity != fixture.target.Identity ||
		!marker.Target.PIDFDVerified || marker.Target.HostUID != fixture.witness.ownerConfig.BUID || !marker.Target.SamePIDExec ||
		marker.Target.UserNamespace.Device != uint64(userNamespace.Dev) ||
		marker.Target.UserNamespace.Inode != userNamespace.Ino ||
		marker.Target.NetworkNamespace.Device != uint64(networkNamespace.Dev) ||
		marker.Target.NetworkNamespace.Inode != networkNamespace.Ino ||
		marker.Owners.Gate.PID != marker.Target.PID || marker.Owners.Gate.Identity != marker.Target.Identity {
		t.Fatalf("release stage marker target = %#v, want the exact live same-PID peer evidence", marker.Target)
	}
	fixture.witness.mu.Lock()
	peer, target := fixture.witness.peer, fixture.witness.summary.Target
	fixture.witness.mu.Unlock()
	if peer == nil || peer.PIDFD == nil || peer.PID != fixture.target.PID || peer.Identity != fixture.target.Identity ||
		target != marker.Target {
		t.Fatalf("release witness evidence = %#v/%#v, want the durable marker target and its pidfd", peer, target)
	}
}

func TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage(t *testing.T) {
	fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
	fixture.witness.request.Harness = config.RuntimeHarnessCodex
	if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, false); err != nil {
		t.Fatalf("daemon.start release path callback: %v", err)
	}
	if _, err := os.Lstat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("daemon.start release path staged the runner release marker: %v", err)
	}
}

func TestLinuxV2NativeWitnessStartProcessReleaseAckLossKeepsTheClosureStage(t *testing.T) {
	fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
	fixture.witness.request.Harness = config.RuntimeHarnessCodex
	fixture.witness.request.DropAckAfter = linuxV2NativeWitnessReleaseStage
	peerFD, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatalf("open the launch-transferred closure peer pidfd: %v", err)
	}
	closurePeer := &execution.LinuxV2Peer{
		PID: fixture.target.PID, Identity: fixture.target.Identity,
		PIDFD: os.NewFile(uintptr(peerFD), "witness-closure-peer-pidfd"),
	}
	t.Cleanup(func() { _ = closurePeer.Close() })
	fixture.witness.mu.Lock()
	fixture.witness.peer = closurePeer
	fixture.witness.summary.Target = linuxV2NativeWitnessTarget{
		PID: fixture.target.PID, Identity: fixture.target.Identity, PIDFDVerified: true,
	}
	fixture.witness.mu.Unlock()
	if err := fixture.witness.stage(linuxV2NativeWitnessReleaseStage, nil, fixture.store, false); err != nil {
		t.Fatalf("stage the daemon.start release hold: %v", err)
	}
	before, err := os.ReadFile(fixture.marker)
	if err != nil {
		t.Fatalf("read the daemon.start release marker: %v", err)
	}
	// The helper accepted the RELEASE before this ACK: the journal has moved on.
	fixture.journal.ContainmentAuthority.LauncherPID++
	if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, false); !errors.Is(err, execution.ErrLinuxV2MutationAckLost) {
		t.Fatalf("ACK-loss release callback error = %v, want the lost-ACK category", err)
	}
	after, err := os.ReadFile(fixture.marker)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("ACK-loss release callback rewrote the daemon.start marker: %v", err)
	}
	if closurePeer.PIDFD != nil {
		t.Fatal("ACK-loss release callback leaked the replaced launch-transferred peer pidfd")
	}
	fixture.witness.mu.Lock()
	peer := fixture.witness.peer
	fixture.witness.mu.Unlock()
	if peer == nil || peer == closurePeer || peer.PIDFD == nil || peer.PID != fixture.target.PID {
		t.Fatalf("ACK-loss release witness evidence = %#v, want the captured replacement peer", peer)
	}
	marker := fixture.readMarker(t)
	if marker.DropAckAfter != linuxV2NativeWitnessReleaseStage || !marker.Target.PIDFDVerified ||
		marker.Target.PID != fixture.target.PID || marker.Owners.Gate.Identity != fixture.target.Identity {
		t.Fatalf("ACK-loss release marker = %#v, want the exact lost-ACK boundary with peer evidence", marker)
	}
}

func TestLinuxV2NativeWitnessRunnerReleaseRejectionsStayFailClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*state.RunJournal)
	}{
		{name: "gate PID mismatch", mutate: func(journal *state.RunJournal) {
			journal.ContainmentCgroup.Gate.Process.PID = os.Getpid() + 1
		}},
		{name: "authority target mismatch", mutate: func(journal *state.RunJournal) {
			journal.ContainmentAuthority.TargetPID = os.Getpid() + 1
		}},
		{name: "zero PID", mutate: func(journal *state.RunJournal) {
			journal.PID = 0
		}},
		{name: "missing gate", mutate: func(journal *state.RunJournal) {
			journal.ContainmentCgroup.Gate = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLinuxV2WitnessReleaseFixtureWithJournal(t, linuxV2NativeWitnessReleaseStage, test.mutate)
			if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, false); err == nil {
				t.Fatal("release callback accepted a journal outside the exact committed target")
			}
			fixture.witness.mu.Lock()
			peer := fixture.witness.peer
			fixture.witness.mu.Unlock()
			if peer != nil {
				t.Fatalf("rejected release witness evidence = %#v, want no peer publication", peer)
			}
			if _, err := os.Lstat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected release staged its held marker: %v", err)
			}
		})
	}
}

func TestLinuxV2NativeWitnessRunnerReleaseReplaySkipsWithoutStaging(t *testing.T) {
	fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
	if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, true); err != nil {
		t.Fatalf("replay release callback: %v", err)
	}
	fixture.witness.mu.Lock()
	peer := fixture.witness.peer
	fixture.witness.mu.Unlock()
	if peer != nil {
		t.Fatalf("replay release witness evidence = %#v, want no peer publication", peer)
	}
	if _, err := os.Lstat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replay release staged its held marker: %v", err)
	}
}

func TestLinuxV2NativeWitnessReleaseSeamRequiresTheRequestHarness(t *testing.T) {
	fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
	fixture.witness.request.Harness = ""
	if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessReleaseStage, false); err == nil {
		t.Fatal("release callback accepted a request without a configured harness")
	}
	if _, err := os.Lstat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unparsable request harness staged its held marker: %v", err)
	}
	fixture.witness.mu.Lock()
	peer := fixture.witness.peer
	fixture.witness.mu.Unlock()
	if peer != nil {
		t.Fatalf("unparsable request harness published witness evidence: %#v", peer)
	}
}

func TestLinuxV2NativeWitnessLeaseAckStagesThePlainNativeReleaseHold(t *testing.T) {
	for _, test := range []struct {
		name    string
		harness string
	}{
		{name: "migration_pi", harness: config.RuntimeHarnessPi},
		{name: "claude_hold_after_native_release", harness: config.RuntimeHarnessClaudeCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
			fixture.witness.request.Harness = test.harness
			if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessLeaseStage, false); err != nil {
				t.Fatalf("initial lease ACK callback: %v", err)
			}
			marker := fixture.readStageMarker(t, linuxV2NativeWitnessLeaseStage)
			if marker.Stage != linuxV2NativeWitnessLeaseStage || marker.HoldAfter != linuxV2NativeWitnessReleaseStage ||
				marker.NativeReleaseState != linuxV2NativeWitnessNativeReleaseNotReleased || marker.DropAckAfter != "" {
				t.Fatalf("initial lease marker = %#v, want the exact not-released pre-checkout boundary", marker)
			}
			if marker.Target.PIDFDVerified || marker.Target.HostUID != 0 || marker.Target.SamePIDExec {
				t.Fatalf("initial lease marker target = %#v, want no fabricated B peer proof", marker.Target)
			}
			fixture.witness.mu.Lock()
			acked := fixture.witness.summary.Stages.InitialLeaseAcked
			fixture.witness.mu.Unlock()
			if !acked {
				t.Fatal("plain native_release hold did not record the initial lease ACK")
			}
			if _, err := os.Lstat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("initial lease ACK staged the release marker: %v", err)
			}
		})
	}
}

func TestLinuxV2NativeWitnessLeaseAckDoesNotTouchTheRunnerLeaseBoundary(t *testing.T) {
	leaseMarker := func(fixture linuxV2WitnessReleaseFixture) string {
		return filepath.Join(filepath.Dir(fixture.marker), linuxV2NativeWitnessLeaseStage+".json")
	}
	t.Run("watcher_not_yet_staged", func(t *testing.T) {
		fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessLeaseStage)
		done := make(chan error, 1)
		go func() { done <- fixture.callbacks.DropAckAfter(linuxV2NativeWitnessLeaseStage, false) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("runner lease ACK callback: %v", err)
			}
		case <-time.After(5 * time.Second):
			if _, err := os.Lstat(leaseMarker(fixture)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the ACK staged over the runner-held lease boundary: %v", err)
			}
			t.Fatal("runner lease ACK callback did not return for the runner-held boundary")
		}
		if _, err := os.Lstat(leaseMarker(fixture)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the ACK staged over the runner-held lease boundary: %v", err)
		}
		fixture.witness.mu.Lock()
		acked := fixture.witness.summary.Stages.InitialLeaseAcked
		fixture.witness.mu.Unlock()
		if acked {
			t.Fatal("the ACK claimed the runner-held lease boundary without its watcher marker")
		}
	})
	t.Run("watcher_staged", func(t *testing.T) {
		fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessLeaseStage)
		if err := fixture.witness.stage(linuxV2NativeWitnessLeaseStage, nil, fixture.store, false); err != nil {
			t.Fatalf("stage the runner-held lease boundary: %v", err)
		}
		before, err := os.ReadFile(leaseMarker(fixture))
		if err != nil {
			t.Fatalf("read the runner-held lease marker: %v", err)
		}
		fixture.journal.ContainmentAuthority.LauncherPID++
		if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessLeaseStage, false); err != nil {
			t.Fatalf("runner lease ACK callback after its watcher: %v", err)
		}
		after, err := os.ReadFile(leaseMarker(fixture))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("the ACK rewrote the runner-held lease marker: %v", err)
		}
	})
}

func TestLinuxV2NativeWitnessLeaseAckReplayStagesNothing(t *testing.T) {
	leaseMarker := func(fixture linuxV2WitnessReleaseFixture) string {
		return filepath.Join(filepath.Dir(fixture.marker), linuxV2NativeWitnessLeaseStage+".json")
	}
	t.Run("restored_marker", func(t *testing.T) {
		fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
		if err := fixture.witness.stage(linuxV2NativeWitnessLeaseStage, nil, fixture.store, false); err != nil {
			t.Fatalf("stage the restored lease marker: %v", err)
		}
		before, err := os.ReadFile(leaseMarker(fixture))
		if err != nil {
			t.Fatalf("read the restored lease marker: %v", err)
		}
		fixture.journal.ContainmentAuthority.LauncherPID++
		if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessLeaseStage, false); err != nil {
			t.Fatalf("replayed lease ACK callback: %v", err)
		}
		after, err := os.ReadFile(leaseMarker(fixture))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("the replayed lease ACK rewrote the restored marker: %v", err)
		}
	})
	t.Run("acked_on_entry", func(t *testing.T) {
		fixture := newLinuxV2WitnessReleaseFixture(t, linuxV2NativeWitnessReleaseStage)
		if err := fixture.callbacks.DropAckAfter(linuxV2NativeWitnessLeaseStage, true); err != nil {
			t.Fatalf("acked-on-entry lease callback: %v", err)
		}
		if _, err := os.Lstat(leaseMarker(fixture)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("acked-on-entry lease callback staged a marker: %v", err)
		}
	})
}

func TestLinuxV2NativeWitnessPIDFDIdentityArmRefusesAChangedTarget(t *testing.T) {
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("read the live target identity: %v", err)
	}
	pidfd, err := openExactLiveLinuxV2WitnessPIDFD(os.Getpid(), identity+"-stale", "arm target")
	if err == nil {
		_ = pidfd.Close()
		t.Fatal("pidfd capture accepted an identity that changed after PidfdOpen")
	}
	if !strings.Contains(err.Error(), "identity changed after pidfd open") {
		t.Fatalf("pidfd identity arm error = %v, want the post-open identity change", err)
	}
}

func TestLinuxV2NativeWitnessPIDFDLivenessArmRefusesAnExitedTarget(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the exited-target helper: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Wait() })
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatalf("open the exited-target pidfd: %v", err)
	}
	defer unix.Close(fd)
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready, pollErr := unix.Poll(poll, 0)
		if pollErr != nil {
			t.Fatalf("poll the exited-target pidfd: %v", pollErr)
		}
		if ready == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper target never exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		t.Fatalf("read the exited target identity: %v", err)
	}
	pidfd, err := openExactLiveLinuxV2WitnessPIDFD(pid, identity, "exited arm target")
	if err == nil {
		_ = pidfd.Close()
		t.Fatal("pidfd capture accepted a target whose pidfd already signalled its exit")
	}
	if !strings.Contains(err.Error(), "pidfd is not live") {
		t.Fatalf("pidfd liveness arm error = %v, want the exited target refusal", err)
	}
}

func TestLinuxV2NativeWitnessUserNamespaceShapeArmRefusesNonNsfsInodes(t *testing.T) {
	for _, test := range []struct {
		name string
		stat unix.Stat_t
	}{
		{name: "directory_mode", stat: unix.Stat_t{Mode: unix.S_IFDIR | 0o755, Dev: 4, Ino: 5}},
		{name: "zero_device", stat: unix.Stat_t{Mode: unix.S_IFREG, Dev: 0, Ino: 5}},
		{name: "zero_inode", stat: unix.Stat_t{Mode: unix.S_IFREG, Dev: 4, Ino: 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := linuxV2NativeWitnessUserNamespaceFromStat(test.stat); err == nil {
				t.Fatal("user-namespace shape arm accepted a stat outside the exact nsfs inode")
			}
		})
	}
	value, err := linuxV2NativeWitnessUserNamespaceFromStat(unix.Stat_t{Mode: unix.S_IFREG, Dev: 4, Ino: 5})
	if err != nil || value.Device != 4 || value.Inode != 5 {
		t.Fatalf("exact nsfs stat = %#v/%v, want device 4 inode 5", value, err)
	}
}

func TestLinuxV2NativeWitnessOwnerLossStage(t *testing.T) {
	request := func(caseName, hold string) linuxV2NativeWitnessRequest {
		return linuxV2NativeWitnessRequest{Case: caseName, Barrier: linuxV2NativeWitnessBarrier{HoldAfter: hold}}
	}
	for _, test := range []struct {
		name    string
		request linuxV2NativeWitnessRequest
		want    string
	}{
		{name: "native_release hold", request: request("stage_replay", "native_release"), want: "native_release"},
		{name: "stop_receipt_durable hold", request: request("stage_replay", "stop_receipt_durable"), want: "stop_receipt_durable"},
		{name: "pre-release hold", request: request("stage_replay", "lease_armed")},
		{name: "checkout_ready hold", request: request("stage_replay", "checkout_ready")},
		{name: "unreleased hold", request: request("stage_replay", "handoff_prepared")},
		{name: "leaf_removed hold", request: request("stage_replay", "leaf_removed")},
		{name: "containment case", request: request("claude_containment", "native_release")},
		{name: "empty request", request: request("", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxV2NativeWitnessOwnerLossStage(test.request); got != test.want {
				t.Fatalf("owner-loss snapshot stage = %q, want %q", got, test.want)
			}
		})
	}
	if linuxV2NativeWitnessOwnerLossExpected(state.RunJournal{}) {
		t.Fatal("an empty journal was accepted as the committed-live owner-loss boundary")
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: 71, Identity: "linux:v2:held-helper"}
	live := state.RunJournal{RunContainmentState: state.RunContainmentState{
		ContainmentCgroup:    &state.LinuxCgroupReservation{Helper: &helper},
		ContainmentAuthority: &authority.Supervisor{Version: authority.SupervisorVersionV2},
	}}
	if !linuxV2NativeWitnessOwnerLossExpected(live) {
		t.Fatal("the committed-live boundary was refused")
	}
	exited := live
	exited.ContainmentCgroup = &state.LinuxCgroupReservation{Helper: &helper, HelperExit: &authority.LinuxCgroupExitProof{}}
	if linuxV2NativeWitnessOwnerLossExpected(exited) {
		t.Fatal("an exited helper was accepted as the committed-live owner-loss boundary")
	}
}

func TestLinuxV2NativeWitnessOwnerLossSummaryShape(t *testing.T) {
	request := linuxV2NativeWitnessRequest{Case: "stage_replay", Barrier: linuxV2NativeWitnessBarrier{HoldAfter: "native_release"}}
	helper := linuxV2NativeWitnessProcess{PID: 71, Identity: "linux:v2:held-helper"}
	captured := linuxV2NativeWitnessOwnerLoss{Version: 1, Stage: "native_release", Captured: true, Helper: helper, OwnerLost: true}
	failed := linuxV2NativeWitnessOwnerLoss{Version: 1, Stage: "native_release", Helper: helper, Error: "held helper did not latch"}
	for _, test := range []struct {
		name     string
		snapshot *linuxV2NativeWitnessOwnerLoss
		wantOK   bool
	}{
		{name: "absent snapshot", wantOK: true},
		{name: "captured latch without a proof yet", snapshot: &captured, wantOK: true},
		{name: "bounded failure", snapshot: &failed, wantOK: true},
		{name: "silent failure", snapshot: &linuxV2NativeWitnessOwnerLoss{Version: 1, Stage: "native_release", Helper: helper}},
		{name: "unheld stage", snapshot: func() *linuxV2NativeWitnessOwnerLoss {
			value := captured
			value.Stage = "lease_armed"
			return &value
		}()},
		{name: "stage mismatch", snapshot: func() *linuxV2NativeWitnessOwnerLoss {
			value := captured
			value.Stage = "stop_receipt_durable"
			return &value
		}()},
		{name: "missing helper identity", snapshot: func() *linuxV2NativeWitnessOwnerLoss {
			value := captured
			value.Helper.Identity = ""
			return &value
		}()},
		{name: "unsupported version", snapshot: func() *linuxV2NativeWitnessOwnerLoss {
			value := captured
			value.Version = 2
			return &value
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateLinuxV2NativeWitnessOwnerLoss(test.snapshot, request); (err == nil) != test.wantOK {
				t.Fatalf("owner-loss snapshot validation error = %v, want ok=%t", err, test.wantOK)
			}
		})
	}
}

func TestLinuxV2NativeWitnessOwnerLossSnapshotPrecedesRecovery(t *testing.T) {
	helper := authority.LinuxCgroupProcessIdentity{PID: 71, Identity: "linux:v2:held-helper"}
	journal := state.RunJournal{
		RunID: "owner-loss-seam-run", Generation: 1,
		RunContainmentState: state.RunContainmentState{
			ContainmentCgroup:    &state.LinuxCgroupReservation{Helper: &helper},
			ContainmentAuthority: &authority.Supervisor{Version: authority.SupervisorVersionV2},
		},
	}
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("open ordering-row Store: %v", err)
	}
	defer store.Close()
	stageRequest := func(hold string) linuxV2NativeWitnessRequest {
		return linuxV2NativeWitnessRequest{Case: "stage_replay", Barrier: linuxV2NativeWitnessBarrier{HoldAfter: hold}}
	}
	captured := &linuxV2NativeWitnessOwnerLoss{Version: 1, Stage: "native_release", Captured: true, Helper: linuxV2NativeWitnessProcess{PID: helper.PID, Identity: helper.Identity}, OwnerLost: true}
	originalCapture, originalRecover := linuxV2NativeWitnessCaptureOwnerLoss, linuxV2NativeWitnessInvokeRecovery
	defer func() {
		linuxV2NativeWitnessCaptureOwnerLoss = originalCapture
		linuxV2NativeWitnessInvokeRecovery = originalRecover
	}()
	var order []string
	linuxV2NativeWitnessCaptureOwnerLoss = func(context.Context, *daemon, state.RunJournal, string) *linuxV2NativeWitnessOwnerLoss {
		order = append(order, "snapshot")
		return captured
	}
	linuxV2NativeWitnessInvokeRecovery = func(context.Context, *daemon, []state.RunJournal) error {
		order = append(order, "recovery")
		return nil
	}
	snapshot, err := recoverLinuxV2NativeWitnessRunKey(context.Background(), config.Config{}, store, journal, stageRequest("native_release"))
	if err != nil || snapshot != captured {
		t.Fatalf("owner-loss seam = snapshot %#v error %v, want the captured snapshot", snapshot, err)
	}
	if len(order) != 2 || order[0] != "snapshot" || order[1] != "recovery" {
		t.Fatalf("owner-loss seam order = %v, want the snapshot before the recovery's first stop", order)
	}
	for _, test := range []struct {
		name    string
		request linuxV2NativeWitnessRequest
		journal state.RunJournal
	}{
		{name: "unproven hold carries no snapshot", request: stageRequest("lease_armed"), journal: journal},
		{name: "legacy journal carries no snapshot", request: stageRequest("native_release"), journal: state.RunJournal{RunID: "owner-loss-seam-run", Generation: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			order = nil
			snapshot, err := recoverLinuxV2NativeWitnessRunKey(context.Background(), config.Config{}, store, test.journal, test.request)
			if err != nil || snapshot != nil || len(order) != 1 || order[0] != "recovery" {
				t.Fatalf("owner-loss seam = snapshot %#v order %v error %v, want recovery without a snapshot", snapshot, order, err)
			}
		})
	}
}

func TestLinuxV2NativeWitnessOwnerLossCaptureKeepsTheRecoveryTimeout(t *testing.T) {
	helper := authority.LinuxCgroupProcessIdentity{PID: 71, Identity: "linux:v2:held-helper"}
	journal := state.RunJournal{
		RunID: "owner-loss-budget-run", Generation: 1,
		RunContainmentState: state.RunContainmentState{
			ContainmentCgroup:    &state.LinuxCgroupReservation{Helper: &helper},
			ContainmentAuthority: &authority.Supervisor{Version: authority.SupervisorVersionV2},
		},
	}
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatalf("open budget-row Store: %v", err)
	}
	defer store.Close()
	originalCapture, originalRecover := linuxV2NativeWitnessCaptureOwnerLoss, linuxV2NativeWitnessInvokeRecovery
	defer func() {
		linuxV2NativeWitnessCaptureOwnerLoss = originalCapture
		linuxV2NativeWitnessInvokeRecovery = originalRecover
	}()
	const cleanup = 100 * time.Millisecond
	start := time.Now()
	var captureDeadline time.Time
	linuxV2NativeWitnessCaptureOwnerLoss = func(ctx context.Context, _ *daemon, _ state.RunJournal, _ string) *linuxV2NativeWitnessOwnerLoss {
		captureDeadline, _ = ctx.Deadline()
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
		return &linuxV2NativeWitnessOwnerLoss{Version: 1, Stage: "native_release", Captured: true}
	}
	var recoveryLive bool
	var recoveryRemaining time.Duration
	linuxV2NativeWitnessInvokeRecovery = func(ctx context.Context, _ *daemon, _ []state.RunJournal) error {
		recoveryLive = ctx.Err() == nil
		if deadline, ok := ctx.Deadline(); ok {
			recoveryRemaining = time.Until(deadline)
		}
		return nil
	}
	request := linuxV2NativeWitnessRequest{Case: "stage_replay", Barrier: linuxV2NativeWitnessBarrier{HoldAfter: "native_release"}}
	if _, err := recoverLinuxV2NativeWitnessRunKey(context.Background(), config.Config{CleanupTimeoutMS: int64(cleanup / time.Millisecond)}, store, journal, request); err != nil {
		t.Fatalf("owner-loss budget row recovery error = %v", err)
	}
	if own := captureDeadline.Sub(start); own < 30*time.Second {
		t.Fatalf("owner-loss capture deadline offset = %s, want the capture's own 40 s bound", own)
	}
	if !recoveryLive {
		t.Fatal("recovery was admitted with the capture's exhausted context; its CleanupTimeout must start with the recovery")
	}
	if recoveryRemaining <= 0 || recoveryRemaining > 2*cleanup {
		t.Fatalf("recovery context remaining = %s, want its full configured %s cleanup timeout", recoveryRemaining, cleanup)
	}
}

type linuxV2NativeWitnessOwnerLossAnchorStub struct {
	identity platform.LinuxOwnerAnchorIdentity
	helper   func() *platform.LinuxOwnerAnchorHelper
	lookups  int
}

func (stub *linuxV2NativeWitnessOwnerLossAnchorStub) Discover(context.Context) (platform.LinuxOwnerAnchorIdentity, error) {
	return stub.identity, nil
}

func (stub *linuxV2NativeWitnessOwnerLossAnchorStub) LookupHelper(context.Context, platform.LinuxOwnerAnchorGrant) (*platform.LinuxOwnerAnchorHelper, error) {
	stub.lookups++
	return stub.helper(), nil
}

func linuxV2NativeWitnessOwnerLossTerminalFixture(
	t *testing.T,
	held authority.LinuxCgroupProcessIdentity,
	exited authority.LinuxCgroupProcessIdentity,
) (state.RunJournal, *daemon, *linuxV2NativeWitnessOwnerLossAnchorStub) {
	t.Helper()
	heldIdentity := held
	reservation := testAppLinuxV2Reservation()
	reservation.CreateStarted = true
	reservation.Helper = &heldIdentity
	leaf := testAppLinuxV2Leaf(reservation)
	reservation.LeafIdentity = &leaf
	if err := reservation.Validate(); err != nil {
		t.Fatalf("owner-loss terminal fixture reservation is structurally invalid: %v", err)
	}
	journal := state.RunJournal{
		RunID: "owner-loss-terminal-run", Generation: 3,
		RuntimeID: "runtime-owner-loss-terminal", ClaimedRuntimeEpoch: 1,
		ClaimID: "claim-owner-loss-terminal", LeaseToken: "lease-owner-loss-terminal",
		RunContainmentState: state.RunContainmentState{
			ContainmentCgroup:    &reservation,
			ContainmentAuthority: &authority.Supervisor{Version: authority.SupervisorVersionV2},
		},
	}
	grant := linuxV2GrantFromJournal(journal.Key(), linuxV2FenceFromJournal(journal), reservation)
	if err := grant.Validate(); err != nil {
		t.Fatalf("owner-loss terminal fixture grant is structurally invalid: %v", err)
	}
	exit := testAppLinuxV2Exit(exited, reservation.Anchor)
	if err := exit.Validate(); err != nil {
		t.Fatalf("owner-loss terminal fixture helper exit is structurally invalid: %v", err)
	}
	stub := &linuxV2NativeWitnessOwnerLossAnchorStub{
		identity: platform.LinuxOwnerAnchorIdentity{Process: reservation.Anchor, Parent: reservation.Parent, OwnerUID: reservation.OwnerUID},
	}
	stub.helper = func() *platform.LinuxOwnerAnchorHelper {
		proof := exit
		return &platform.LinuxOwnerAnchorHelper{PID: exited.PID, Identity: exited.Identity, HelperExit: &proof}
	}
	original := linuxV2NativeWitnessOwnerLossAnchor
	linuxV2NativeWitnessOwnerLossAnchor = func(*linuxV2OwnerResources) linuxV2NativeWitnessAnchor { return stub }
	t.Cleanup(func() { linuxV2NativeWitnessOwnerLossAnchor = original })
	return journal, &daemon{linuxV2Owner: &linuxV2OwnerResources{}}, stub
}

func TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit(t *testing.T) {
	held := testAppLinuxV2Helper()
	journal, recovery, stub := linuxV2NativeWitnessOwnerLossTerminalFixture(t, held, held)
	start := time.Now()
	snapshot := captureLinuxV2NativeWitnessOwnerLoss(context.Background(), recovery, journal, "native_release")
	elapsed := time.Since(start)
	if stub.lookups != 1 {
		t.Fatalf("terminal helper_exited capture dials = %d, want the single terminal classification", stub.lookups)
	}
	if elapsed > linuxV2NativeWitnessOwnerLossStopWait/8 {
		t.Fatalf("terminal helper_exited capture elapsed = %s, want a return well inside the %s bound", elapsed, linuxV2NativeWitnessOwnerLossStopWait)
	}
	if snapshot.Captured || snapshot.OwnerLost || snapshot.ReleaseAuthorized || snapshot.StopProof != nil {
		t.Fatalf("terminal helper_exited capture = %#v, want the unchanged uncaptured snapshot", snapshot)
	}
	if snapshot.Version != 1 || snapshot.Stage != "native_release" ||
		snapshot.Helper.PID != held.PID || snapshot.Helper.Identity != held.Identity {
		t.Fatalf("terminal helper_exited capture shape = %#v, want the exact held helper at version 1", snapshot)
	}
	if snapshot.Error != "anchor returned no live exact held helper for the owner-loss snapshot" {
		t.Fatalf("terminal helper_exited capture error = %q, want the unchanged unheld-helper message", snapshot.Error)
	}
}

func TestLinuxV2NativeWitnessOwnerLossCaptureKeepsPollingForAnotherHelpersExit(t *testing.T) {
	held := testAppLinuxV2Helper()
	foreign := authority.LinuxCgroupProcessIdentity{PID: 99, Identity: "linux:v2:foreign-helper"}
	journal, recovery, stub := linuxV2NativeWitnessOwnerLossTerminalFixture(t, held, foreign)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminalHelper := stub.helper
	stub.helper = func() *platform.LinuxOwnerAnchorHelper {
		if stub.lookups == 2 {
			cancel()
		}
		return terminalHelper()
	}
	snapshot := captureLinuxV2NativeWitnessOwnerLoss(ctx, recovery, journal, "native_release")
	if stub.lookups < 2 {
		t.Fatalf("foreign helper_exited capture dials = %d, want the poll to continue past one classification", stub.lookups)
	}
	if snapshot.Captured || snapshot.OwnerLost || snapshot.StopProof != nil ||
		!strings.Contains(snapshot.Error, "no live exact held helper") {
		t.Fatalf("foreign helper_exited capture = %#v, want the unchanged uncaptured snapshot with its reason", snapshot)
	}
	if snapshot.Version != 1 || snapshot.Stage != "native_release" ||
		snapshot.Helper.PID != held.PID || snapshot.Helper.Identity != held.Identity {
		t.Fatalf("foreign helper_exited capture shape = %#v, want the exact held helper at version 1", snapshot)
	}
}

func TestLinuxV2NativeWitnessContextCarriesCanonicalSubjectHash(t *testing.T) {
	subject := protocol.Subject{
		ResourceID: "0f8fad5b-d9cb-469f-a165-70867728950e",
		Commit:     strings.Repeat("1", 40),
		TreeDigest: "sha256:" + strings.Repeat("0", 64),
	}
	runKey := linuxV2NativeWitnessRunKey{RunID: "c2-native-context", Generation: 7}
	witness := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Subject: subject, RunKey: runKey}}
	want := `{"version":1,"subject":{"resource_id":"0f8fad5b-d9cb-469f-a165-70867728950e",` +
		`"commit":"1111111111111111111111111111111111111111",` +
		`"tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},` +
		`"subject_hash":"sha256:7777b01831c228bb006afaeb0d6097dd33ffa2a012077ab76d63117570b706de",` +
		`"run_key":{"run_id":"c2-native-context","generation":7}}`
	if got := string(witness.nativeContext()); got != want {
		t.Fatalf("native context = %s, want %s", got, want)
	}
	hashless := &linuxV2NativeWitnessRun{request: linuxV2NativeWitnessRequest{Subject: protocol.Subject{ResourceID: "sample-resource"}}}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("native context emitted a payload for a subject that cannot be hashed")
			}
		}()
		_ = hashless.nativeContext()
	}()
}
