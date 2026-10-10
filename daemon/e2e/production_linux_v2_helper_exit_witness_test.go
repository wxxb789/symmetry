//go:build linux && amd64

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	linuxV2HelperExitAfterReleaseCase       = "helper_exit_after_release_before_register"
	linuxV2HelperExitAfterReleaseAction     = "crash_after_release_before_register"
	linuxV2HelperExitAfterReleaseMarkerDir  = "helper-exit-after-release-before-register"
	linuxV2HelperExitAfterReleaseMarkerFile = "live-b-observed.json"
)

type linuxV2HelperExitLiveBMarker struct {
	Version               int    `json:"version"`
	Case                  string `json:"case"`
	HelperAction          string `json:"helper_action"`
	RunID                 string `json:"run_id"`
	Generation            int64  `json:"generation"`
	GatePID               int    `json:"gate_pid"`
	GateIdentitySHA256    string `json:"gate_identity_sha256"`
	HostUID               uint32 `json:"host_uid"`
	HostGID               uint32 `json:"host_gid"`
	UIDMap                string `json:"uid_map"`
	GIDMap                string `json:"gid_map"`
	Setgroups             string `json:"setgroups"`
	CapEff                string `json:"cap_eff"`
	NoNewPrivs            bool   `json:"no_new_privs"`
	LeafDevice            uint64 `json:"leaf_device"`
	LeafInode             uint64 `json:"leaf_inode"`
	LeafMembership        bool   `json:"leaf_membership"`
	WitnessActionObserved bool   `json:"witness_action_observed"`
}

type linuxV2HelperExitAfterReleaseSnapshot struct {
	Action                 string                       `json:"action"`
	RunID                  string                       `json:"run_id"`
	Generation             int64                        `json:"generation"`
	HelperPID              int                          `json:"helper_pid"`
	HelperIdentitySHA256   string                       `json:"helper_identity_sha256"`
	GatePIDFDVerified      bool                         `json:"gate_pidfd_verified"`
	UserNamespaceDevice    uint64                       `json:"user_namespace_device"`
	UserNamespaceInode     uint64                       `json:"user_namespace_inode"`
	NativeCommandVerified  bool                         `json:"native_command_verified"`
	NoWritableCgroupFDs    bool                         `json:"no_writable_cgroup_fds"`
	HelperWaited           bool                         `json:"helper_waited"`
	HelperReaped           bool                         `json:"helper_reaped"`
	LiveB                  linuxV2HelperExitLiveBMarker `json:"live_b"`
	AnchorPID              int                          `json:"anchor_pid"`
	AnchorIdentitySHA256   string                       `json:"anchor_identity_sha256"`
	AnchorReapedGate       bool                         `json:"anchor_reaped_gate"`
	StopReceiptVersion     int                          `json:"stop_receipt_version"`
	StopProofKind          string                       `json:"stop_proof_kind"`
	CgroupKillAcknowledged bool                         `json:"cgroup_kill_acknowledged"`
	CgroupPopulatedZero    bool                         `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool                         `json:"no_migration_observed"`
	ProducerFenced         bool                         `json:"producer_fenced"`
	DescendantsReaped      bool                         `json:"descendants_reaped"`
	LeafRemoved            bool                         `json:"leaf_removed"`
	JournalCleared         bool                         `json:"journal_cleared"`
}

type linuxV2HelperExitProcessStatus struct {
	UIDs        [4]uint32
	GIDs        [4]uint32
	GroupsEmpty bool
	CapEff      string
	NoNewPrivs  bool
	UIDMap      string
	GIDMap      string
	Setgroups   string
}

// TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister exercises
// the installed helper's authenticated crash window after a real B RELEASE
// but before native RegisterProcess. It passes only after the A mirror records
// a valid v2 physical-stop receipt and receipt-backed clear for that exact B
// PID/start/leaf; unknown release outcomes never become cleanup evidence.
func TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the installed helper release-window crash witness")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical helper-exit fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, linuxV2HelperExitAfterReleaseCase)
	configPath := filepath.Join(caseRoot, "config.json")
	requestPath := filepath.Join(caseRoot, "request.json")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, configPath)
	t.Setenv(linuxV2StageReplayRequestEnvironment, requestPath)
	runProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister(t, caseRoot, linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "checkout_ready", markerDirectory: linuxV2HelperExitAfterReleaseMarkerDir,
		helperAction: linuxV2HelperExitAfterReleaseAction,
	})
}

func runProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister(
	t *testing.T,
	caseRoot string,
	testCase linuxV2NativeWitnessCase,
) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot ||
		!linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") ||
		testCase.helperAction != linuxV2HelperExitAfterReleaseAction || testCase.barrierStage != "checkout_ready" {
		t.Fatalf("helper exit fixture does not bind the exact OpenCode checkout-ready Run beneath %s", caseRoot)
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	if err := linuxV2ValidateOwnerHelperWitnessSelector(fixture); err != nil {
		t.Fatalf("root selector does not bind exact RunKey/helper action/hold: %v", err)
	}
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID,
		Generation: fixture.request.RunKey.Generation, NoNativeFallback: true,
		Outcome: "unresolved", FailureCategory: "helper_exit_after_release_before_native_register",
		Subject:  linuxV2SubjectSnapshotFromRequest(fixture.request),
		GitHooks: &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Notes: []string{
			"the root-owned selector bound the helper action to this exact RunKey",
			"authenticated helper Status observed successful PreparedGate RELEASE while the exact B process was live before native RegisterProcess",
			"the App receives no fabricated RELEASE ACK or task result; the A anchor mirror receipt is the only accepted physical-stop evidence",
			"failure_category records the intentional helper-exit injection; outcome is containment evidence, not task success",
		},
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted helper release-window witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("helper release-window witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	barrierReleased, rootSelectorReleased := false, false
	var anchorClient *platform.LinuxOwnerAnchorClient
	var anchoredHelper *platform.LinuxOwnerAnchorHelper
	var helperStatus *platform.LinuxOwnerHelperV2Status
	defer func() {
		if !barrierReleased {
			if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
				t.Errorf("release checkout-ready barrier during helper-exit cleanup: %v", err)
			}
		}
		if !rootSelectorReleased && t.Failed() {
			t.Log("root helper selector remains armed; fixture controller must enforce its bounded 30-second cleanup")
		}
		if helperStatus != nil {
			_ = helperStatus.Close()
		}
		if anchoredHelper != nil {
			_ = anchoredHelper.Close()
		}
		if command != nil && command.process != nil && !command.process.waited {
			if err := waitLinuxV2OwnerWitnessCommand(command, 45*time.Second, true); err != nil {
				t.Errorf("wait for bounded helper-exit witness cleanup: %v", err)
			}
		}
	}()
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact %s RunKey boundary before RELEASE: %v", testCase.barrierStage, err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, testCase.barrierStage)
	if err != nil {
		t.Fatalf("read exact %s checkout marker: %v", testCase.barrierStage, err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key || initial.Journal.Fence() != fixture.request.Fence {
		t.Fatalf("read exact RunJournal at checkout_ready: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, testCase.barrierStage, marker, initial.Journal); err != nil {
		t.Fatalf("checkout_ready marker lacks its exact durable gate/clone/lease boundary: %v", err)
	}
	reservationPointer := initial.Journal.ContainmentCgroup
	if reservationPointer == nil || reservationPointer.Gate == nil || reservationPointer.Helper == nil || reservationPointer.HelperExit != nil || reservationPointer.LeafIdentity == nil ||
		initial.Journal.LinuxCloneCheckoutReceipt == nil || initial.Journal.ContainmentCgroupReleaseProof != nil ||
		initial.Journal.ContainmentCgroupPreGateReleaseProof != nil || reservationPointer.PreGateStopReceipt != nil ||
		initial.Journal.HasProcessDetails() || initial.Journal.ContainmentUnproven ||
		initial.Journal.ContainmentHandoff != nil || initial.Journal.ContainmentAuthority == nil ||
		initial.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatal("checkout_ready fixture did not preserve its exact pre-RELEASE gate/helper/leaf and committed authority without process marker or stop receipt")
	}
	reservation := reservationPointer.Clone()
	grant, err := linuxV2OwnerAnchorGrantFromJournal(initial.Journal, reservation)
	if err != nil {
		t.Fatalf("construct exact authenticated owner-anchor grant from RunJournal: %v", err)
	}
	anchorClient, err = platform.NewLinuxOwnerAnchorClient(fixture.value.LinuxOwnerSocket)
	if err != nil {
		t.Fatalf("connect A to the installed owner anchor: %v", err)
	}
	lookupContext, cancelLookup := context.WithTimeout(context.Background(), 10*time.Second)
	anchoredHelper, err = anchorClient.LookupHelper(lookupContext, grant)
	cancelLookup()
	if err != nil || anchoredHelper == nil || anchoredHelper.Control == nil || anchoredHelper.PIDFD == nil {
		t.Fatalf("lookup the exact prepared helper through the RunKey anchor: %v", err)
	}
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		t.Fatalf("release checkout_ready barrier into the real App-owned Gate RELEASE: %v", err)
	}
	barrierReleased = true
	helperStatus, err = linuxV2WaitHelperExitActionStatus(anchoredHelper.Control, fixture, reservation, 10*time.Second)
	if err != nil {
		t.Fatalf("observe authenticated helper action after Gate RELEASE: %v", err)
	}
	if initial.Journal.LinuxCloneCheckoutReceipt == nil {
		t.Fatal("checkout_ready RunJournal omitted its exact B clone receipt")
	}
	liveMarker, err := linuxV2ObserveReleasedBHelperExit(
		fixture, reservation, *helperStatus, *initial.Journal.LinuxCloneCheckoutReceipt,
	)
	if err != nil {
		t.Fatalf("verify the real released B process before helper death: %v", err)
	}
	liveJournal, liveFound, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !liveFound || liveJournal.Journal.ContainmentCgroupReleaseProof != nil ||
		liveJournal.Journal.ContainmentCgroupPreGateReleaseProof != nil ||
		liveJournal.Journal.ContainmentHandoff != nil && liveJournal.Journal.ContainmentHandoff.StopReceipt != nil ||
		liveJournal.Journal.ContainmentAuthority != nil && liveJournal.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("RunJournal published a StopReceipt before the observed live B process was physically stopped: found=%t error=%v", liveFound, err)
	}
	if err := linuxV2ValidateOwnerHelperWitnessSelector(fixture); err != nil {
		t.Fatalf("root helper selector disappeared or changed before live-B evidence was accepted: %v", err)
	}
	witness.HelperExitAfterRelease = &liveMarker
	witness.Leaf = linuxV2BuildLeafSnapshot(*reservation.LeafIdentity)
	witness.Clone = linuxV2BuildCloneSnapshot(*initial.Journal.LinuxCloneCheckoutReceipt, fixture.owner.BUID)
	owners, err := linuxV2OwnerSnapshotFromReservation(reservation, fixture.owner.AGID)
	if err != nil {
		t.Fatalf("snapshot exact A anchor/helper/launcher/gate identities: %v", err)
	}
	witness.Owners = &owners
	if err := linuxV2WriteHelperExitLiveBCheckpoint(filepath.Join(fixture.request.Barrier.Directory, linuxV2HelperExitAfterReleaseMarkerFile), liveMarker.LiveB); err != nil {
		t.Fatalf("publish atomic root-poll checkpoint for exact live B RELEASE identity: %v", err)
	}
	if err := linuxV2WaitHelperWitnessSelectorRemoved(15 * time.Second); err != nil {
		t.Fatalf("root CI did not unlink the exact helper-release selector after live B proof: %v", err)
	}
	rootSelectorReleased = true
	if err := linuxV2WaitPIDFDExited(int(anchoredHelper.PIDFD.Fd()), 15*time.Second); err != nil {
		t.Fatalf("anchor did not wait/reap the helper after its selected post-RELEASE exit: %v", err)
	}
	helperExitContext, cancelHelperExit := context.WithTimeout(context.Background(), 15*time.Second)
	helperExit, err := anchorClient.WaitHelper(helperExitContext, grant, *reservation.Helper)
	cancelHelperExit()
	if err != nil || helperExit.Validate() != nil || !helperExit.ValidFor(*reservation.Helper, reservation.Anchor) ||
		!helperExit.Waited || !helperExit.Reaped {
		t.Fatalf("anchor has no exact waited/reaped helper exit proof: %v", err)
	}
	if err := linuxV2WaitPIDFDExited(int(helperStatus.PIDFD.Fd()), 20*time.Second); err != nil {
		t.Fatalf("anchor did not physically stop and reap the exact live B pidfd: %v", err)
	}
	reapContext, cancelReap := context.WithTimeout(context.Background(), 15*time.Second)
	anchorRunReap, err := anchorClient.WaitReapRun(reapContext, grant, *reservation.Helper)
	cancelReap()
	if err != nil {
		t.Fatalf("read cached exact anchor WaitReapRun/StopLeaf evidence after helper exit: %v", err)
	}
	if anchorRunReap.Stop.ValidateFor(grant, *reservation.Helper, reservation.Anchor, anchorRunReap.Processes, true) != nil ||
		anchorRunReap.Stop.ProducerFence != nil || anchorRunReap.Stop.Gate == nil || !anchorRunReap.Stop.Gate.Equal(*reservation.Gate) ||
		!anchorRunReap.Stop.GateReleaseAuthorized || anchorRunReap.Stop.HelperExit == nil ||
		!anchorRunReap.Stop.HelperExit.Equal(helperExit) {
		t.Fatal("helper-exit recovery did not return exact anchor StopLeaf evidence for the waited/reaped helper")
	}
	witness.AnchorStop = linuxV2BuildAnchorStopSnapshot(anchorRunReap.Stop)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for App owner recovery after helper exit: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(command.exitErr, &exitErr) || exitErr.ExitCode() <= 0 {
		t.Fatalf("helper exit before native registration returned %v, want explicit failed App witness (no native TaskResult)", command.exitErr)
	}
	if responseInfo, err := os.Stat(command.responsePath); err != nil || responseInfo.Size() != 0 {
		t.Fatalf("failed pre-RegisterProcess App witness emitted a success response: %v", err)
	}
	final, finalErr := waitLinuxV2HelperExitMirrorReceipt(fixture.stateDir, key, 45*time.Second)
	if finalErr != nil {
		t.Fatalf("read exact A-mirror release proof after helper loss: %v", finalErr)
	}
	stop, err := linuxV2ValidateHelperExitMirrorReceipt(initial.Journal, reservation, helperExit, anchorRunReap, final.Journal)
	if err != nil {
		t.Fatalf("helper-exit RunJournal does not prove exact mirror stop/release: %v", err)
	}
	liveMarker.HelperWaited = true
	liveMarker.HelperReaped = true
	liveMarker.AnchorReapedGate = true
	liveMarker.StopReceiptVersion = stop.Version
	liveMarker.StopProofKind = stop.LinuxProof.Kind
	liveMarker.CgroupKillAcknowledged = stop.LinuxProof.CgroupKillAcknowledged
	liveMarker.CgroupPopulatedZero = stop.LinuxProof.CgroupPopulatedZero
	liveMarker.NoMigrationObserved = stop.LinuxProof.NoMigrationObserved
	liveMarker.ProducerFenced = stop.LinuxProof.ProducerFenced
	liveMarker.DescendantsReaped = stop.LinuxProof.DescendantsReaped
	if err := linuxV2RequireEmptyHelperExitLeaf(fixture, *reservation.LeafIdentity); err != nil {
		t.Fatalf("root leaf remains after anchor receipt-backed release: %v", err)
	}
	proof := final.Journal.ContainmentCgroupReleaseProof
	liveMarker.LeafRemoved = proof.LeafDisposition == authority.LinuxCgroupLeafRemoved ||
		proof.LeafDisposition == authority.LinuxCgroupLeafAbsentAfterReceipt
	liveMarker.JournalCleared = final.Journal.ContainmentCgroup == nil &&
		final.Journal.ContainmentCgroupPreGateReleaseProof == nil && final.Journal.ContainmentAuthority == nil &&
		final.Journal.ContainmentHandoff == nil && !final.Journal.HasProcessDetails()
	witness.HelperExitAfterRelease = &liveMarker
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, true)
	witness.Outcome = "helper_exit_after_release_before_native_register_stopped_by_anchor_mirror"
	witness.RecoveryState = "released"
	witness.LeafAbsent = true
}

func linuxV2OwnerAnchorGrantFromJournal(journal state.RunJournal, reservation state.LinuxCgroupReservation) (platform.LinuxOwnerAnchorGrant, error) {
	if err := reservation.Validate(); err != nil || reservation.LeafIdentity == nil || reservation.Helper == nil || reservation.Gate == nil ||
		reservation.Launcher == nil || reservation.LauncherExit == nil || reservation.Mirror == nil {
		return platform.LinuxOwnerAnchorGrant{}, errors.Join(errors.New("RunJournal has no exact helper/gate/leaf reservation"), err)
	}
	value := journal.ContainmentAuthority
	if value == nil || value.Validate() != nil || value.Version != authority.SupervisorVersionV2 ||
		value.LaunchToken != reservation.LaunchToken || value.Secret != reservation.Secret ||
		value.OwnerKind != authority.OwnerKindLinuxHelper || value.OwnerContext != reservation.RecoveryEndpoint ||
		value.TargetPID != reservation.Gate.Process.PID || value.TargetIdentity != reservation.Gate.Process.Identity ||
		value.OwnerUID != reservation.OwnerUID || value.LauncherUID != reservation.LauncherUID ||
		value.PipeToken != reservation.PipeToken || value.JobID != reservation.JobID ||
		value.SupervisorPID != reservation.Helper.PID || value.SupervisorIdentity != reservation.Helper.Identity ||
		value.TargetBirthKind != reservation.Gate.BirthKind || value.CgroupIdentity == nil ||
		!value.CgroupIdentity.Equal(*reservation.LeafIdentity) || value.AnchorPID != reservation.Anchor.PID ||
		value.AnchorIdentity != reservation.Anchor.Identity || reservation.Launcher == nil ||
		value.LauncherPID != reservation.Launcher.PID || value.LauncherIdentity != reservation.Launcher.Identity ||
		value.LauncherExit == nil || reservation.LauncherExit == nil || !value.LauncherExit.Equal(*reservation.LauncherExit) ||
		value.Mirror == nil || reservation.Mirror == nil || !value.Mirror.Equal(*reservation.Mirror) {
		return platform.LinuxOwnerAnchorGrant{}, errors.New("RunJournal committed authority differs from the exact helper/gate/leaf reservation")
	}
	return platform.LinuxOwnerAnchorGrant{
		ReservationVersion: state.LinuxCgroupReservationVersion,
		RunKey:             platform.LinuxOwnerAnchorRunKey{RunID: journal.RunID, Generation: journal.Generation},
		Fence:              journal.Fence(), LaunchToken: reservation.LaunchToken, Secret: reservation.Secret,
		PipeToken: reservation.PipeToken, JobID: reservation.JobID, OwnerUID: reservation.OwnerUID,
		LauncherUID: reservation.LauncherUID, RecoveryEndpoint: reservation.RecoveryEndpoint,
		CreateStarted: reservation.CreateStarted, Anchor: reservation.Anchor, Parent: reservation.Parent,
		Leaf: *reservation.LeafIdentity,
	}, nil
}

func linuxV2WaitHelperExitActionStatus(
	client *platform.LinuxOwnerHelperV2Client,
	fixture linuxV2NativeFixture,
	reservation state.LinuxCgroupReservation,
	timeout time.Duration,
) (*platform.LinuxOwnerHelperV2Status, error) {
	if client == nil {
		return nil, errors.New("helper Status client is unavailable")
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		status, err := client.Status(ctx)
		cancel()
		if err == nil && status.WitnessActionObserved {
			if status.WitnessAction != linuxV2HelperExitAfterReleaseAction || !status.Released || !status.ReleaseAttempted ||
				status.OwnerLost || status.ErrorCode != "" || status.RunKey.RunID != fixture.request.RunKey.RunID ||
				status.RunKey.Generation != fixture.request.RunKey.Generation || status.Fence != fixture.request.Fence ||
				status.Helper.Validate() != nil || reservation.Helper == nil || !status.Helper.Equal(*reservation.Helper) ||
				status.Gate == nil || reservation.Gate == nil || !status.Gate.Process.Equal(reservation.Gate.Process) ||
				reservation.LeafIdentity == nil || !status.Gate.Cgroup.Equal(*reservation.LeafIdentity) ||
				status.UserNamespace == nil || status.UserNamespace.Device == 0 || status.UserNamespace.Inode == 0 || status.PIDFD == nil {
				if status.PIDFD != nil {
					_ = status.Close()
				}
				return nil, errors.New("authenticated helper Status does not bind the exact post-RELEASE helper/Gate/UserNamespace")
			}
			return &status, nil
		}
		if status.PIDFD != nil {
			_ = status.Close()
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("helper did not report the exact post-RELEASE action within %s: %w", timeout, lastErr)
	}
	return nil, fmt.Errorf("helper did not report the exact post-RELEASE action within %s", timeout)
}

func linuxV2ObserveReleasedBHelperExit(
	fixture linuxV2NativeFixture,
	reservation state.LinuxCgroupReservation,
	status platform.LinuxOwnerHelperV2Status,
	checkout authority.LinuxCloneCheckoutReceipt,
) (linuxV2HelperExitAfterReleaseSnapshot, error) {
	if status.PIDFD == nil || status.Gate == nil || reservation.LeafIdentity == nil || reservation.Gate == nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("helper Status omitted the exact live gate PIDFD or pinned leaf")
	}
	gate := status.Gate.Process
	if gate.PID <= 0 || gate.Identity == "" || gate.PID != reservation.Gate.Process.PID || gate.Identity != reservation.Gate.Process.Identity {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("Status Gate identity differs from the exact pre-release RunJournal Gate")
	}
	if fdPID, err := linuxV2PIDFDInfoPID(int(status.PIDFD.Fd())); err != nil || fdPID != gate.PID {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.Join(errors.New("Status gate pidfd does not bind the exact gate PID"), err)
	}
	if err := linuxV2RequireLivePIDFD(int(status.PIDFD.Fd())); err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	identity, err := platform.ProcessIdentity(gate.PID)
	if err != nil || identity != gate.Identity {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.Join(errors.New("released B gate process identity differs from its pidfd/RunJournal"), err)
	}
	userNamespace, err := platform.CaptureLinuxUserNamespaceIdentity(gate.PID, gate.Identity, status.PIDFD)
	if err != nil || status.UserNamespace == nil || userNamespace != *status.UserNamespace {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.Join(errors.New("released B process user namespace differs from authenticated helper Status"), err)
	}
	processStatus, err := linuxV2ReadHelperExitBStatus(gate.PID)
	if err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	processStatus.UIDMap, err = linuxV2ReadHelperExitUserMap(gate.PID, "uid_map")
	if err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	processStatus.GIDMap, err = linuxV2ReadHelperExitUserMap(gate.PID, "gid_map")
	if err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	processStatus.Setgroups, err = linuxV2ReadHelperExitUserMap(gate.PID, "setgroups")
	if err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	if processStatus.UIDs != [4]uint32{fixture.owner.BUID, fixture.owner.BUID, fixture.owner.BUID, fixture.owner.BUID} ||
		processStatus.GIDs != [4]uint32{fixture.owner.BGID, fixture.owner.BGID, fixture.owner.BGID, fixture.owner.BGID} ||
		!processStatus.GroupsEmpty || processStatus.CapEff != "0000000000000000" || !processStatus.NoNewPrivs ||
		processStatus.UIDMap != fmt.Sprintf("1 %d 1", fixture.owner.BUID) ||
		processStatus.GIDMap != fmt.Sprintf("1 %d 1", fixture.owner.BGID) || processStatus.Setgroups != "deny" {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("released B process differs from the exact fixed UID/GID maps or has privilege/capability state")
	}
	leaf := *reservation.LeafIdentity
	if err := verifyLinuxV2LeafOnDisk(fixture.value, fixture.owner.AUID, leaf); err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	leafPath := linuxV2LeafPath(fixture.value, leaf)
	if err := linuxV2CgroupContainsPID(leafPath, gate.PID); err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	if err := linuxV2RequireNoWritableCgroupFDsInLeaf(leafPath, gate.PID, gate.Identity, fixture.owner.BUID, fixture.owner.BGID); err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, err
	}
	if checkout.Validate() != nil || checkout.RunID != fixture.request.RunKey.RunID ||
		checkout.Generation != fixture.request.RunKey.Generation || checkout.TargetDevice == 0 || checkout.TargetInode == 0 {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("released B process has no exact B-owned Store checkout receipt")
	}
	if !linuxV2PathWithin(checkout.Target, fixture.workspace.Root) {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("exact B Store checkout escaped the configured clone root")
	}
	cloneInfo, err := os.Lstat(checkout.Target)
	if err != nil {
		return linuxV2HelperExitAfterReleaseSnapshot{}, fmt.Errorf("stat exact B-owned Store checkout: %w", err)
	}
	cloneStat, ok := cloneInfo.Sys().(*syscall.Stat_t)
	if !cloneInfo.IsDir() || !ok || cloneStat.Uid != fixture.owner.BUID ||
		uint64(cloneStat.Dev) != checkout.TargetDevice || cloneStat.Ino != checkout.TargetInode {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.New("live helper-exit Run does not retain the exact B-owned Store checkout inode")
	}
	commandLine, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", gate.PID))
	if err != nil || !linuxV2NativeCommandMatches("opencode", commandLine) {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.Join(errors.New("released B process is not the exact native OpenCode serve command"), err)
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", gate.PID))
	if err != nil || filepath.Clean(executable) != fixture.profile.Command {
		return linuxV2HelperExitAfterReleaseSnapshot{}, errors.Join(errors.New("released B process executable differs from the pinned OpenCode CLI"), err)
	}
	snapshot := linuxV2HelperExitAfterReleaseSnapshot{
		Action: linuxV2HelperExitAfterReleaseAction, RunID: fixture.request.RunKey.RunID,
		Generation: fixture.request.RunKey.Generation, HelperPID: status.Helper.PID,
		HelperIdentitySHA256: linuxWitnessHashBytes([]byte(status.Helper.Identity)),
		LiveB: linuxV2HelperExitLiveBMarker{
			Version: 1, Case: linuxV2HelperExitAfterReleaseCase, RunID: fixture.request.RunKey.RunID,
			HelperAction: linuxV2HelperExitAfterReleaseAction,
			Generation:   fixture.request.RunKey.Generation, GatePID: gate.PID,
			GateIdentitySHA256: linuxWitnessHashBytes([]byte(gate.Identity)), HostUID: processStatus.UIDs[0], HostGID: processStatus.GIDs[0],
			UIDMap: processStatus.UIDMap, GIDMap: processStatus.GIDMap, Setgroups: processStatus.Setgroups,
			CapEff: processStatus.CapEff, NoNewPrivs: processStatus.NoNewPrivs,
			LeafDevice: leaf.LeafDevice, LeafInode: leaf.LeafInode, LeafMembership: true,
			WitnessActionObserved: true,
		},
		GatePIDFDVerified: true, UserNamespaceDevice: userNamespace.Device, UserNamespaceInode: userNamespace.Inode,
		NativeCommandVerified: true, NoWritableCgroupFDs: true,
		AnchorPID: reservation.Anchor.PID, AnchorIdentitySHA256: linuxWitnessHashBytes([]byte(reservation.Anchor.Identity)),
	}
	return snapshot, nil
}

func linuxV2ReadHelperExitBStatus(pid int) (linuxV2HelperExitProcessStatus, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return linuxV2HelperExitProcessStatus{}, fmt.Errorf("read exact B process status: %w", err)
	}
	var result linuxV2HelperExitProcessStatus
	var uidSeen, gidSeen, groupsSeen, noNewPrivsSeen bool
	var capsSeen [5]bool
	capNames := [...]string{"CapEff", "CapPrm", "CapInh", "CapBnd", "CapAmb"}
	capValues := [5]uint64{}
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Uid", "Gid":
			fields := strings.Fields(value)
			if len(fields) != 4 {
				return linuxV2HelperExitProcessStatus{}, errors.New("B process status omitted complete real/effective/saved/fs identity")
			}
			target := &result.UIDs
			seen := &uidSeen
			if key == "Gid" {
				target, seen = &result.GIDs, &gidSeen
			}
			if *seen {
				return linuxV2HelperExitProcessStatus{}, fmt.Errorf("B process status duplicated %s", key)
			}
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return linuxV2HelperExitProcessStatus{}, fmt.Errorf("B process %s field is malformed", key)
				}
				target[index] = uint32(parsed)
			}
			*seen = true
		case "Groups":
			if groupsSeen {
				return linuxV2HelperExitProcessStatus{}, errors.New("B process status duplicated Groups")
			}
			groupsSeen = true
			result.GroupsEmpty = len(strings.Fields(value)) == 0
		case "NoNewPrivs":
			if noNewPrivsSeen {
				return linuxV2HelperExitProcessStatus{}, errors.New("B process status duplicated NoNewPrivs")
			}
			parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if err != nil {
				return linuxV2HelperExitProcessStatus{}, errors.New("B process NoNewPrivs field is malformed")
			}
			result.NoNewPrivs = parsed == 1
			noNewPrivsSeen = true
		default:
			for index, name := range capNames {
				if key != name {
					continue
				}
				if capsSeen[index] {
					return linuxV2HelperExitProcessStatus{}, fmt.Errorf("B process status duplicated %s", name)
				}
				parsed, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
				if err != nil {
					return linuxV2HelperExitProcessStatus{}, fmt.Errorf("B process %s field is malformed", name)
				}
				capsSeen[index], capValues[index] = true, parsed
			}
		}
	}
	if !uidSeen || !gidSeen || !groupsSeen || !noNewPrivsSeen {
		return linuxV2HelperExitProcessStatus{}, errors.New("B process status omitted UID/GID/Groups/NoNewPrivs")
	}
	for index, name := range capNames {
		if !capsSeen[index] || capValues[index] != 0 {
			return linuxV2HelperExitProcessStatus{}, fmt.Errorf("B process %s missing or nonzero", name)
		}
	}
	result.CapEff = fmt.Sprintf("%016x", capValues[0])
	return result, nil
}

func linuxV2ReadHelperExitUserMap(pid int, name string) (string, error) {
	if name != "uid_map" && name != "gid_map" && name != "setgroups" {
		return "", errors.New("unsupported proc map name")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("read B process %s: %w", name, err)
	}
	return strings.Join(strings.Fields(string(contents)), " "), nil
}

func linuxV2WriteHelperExitLiveBCheckpoint(path string, marker linuxV2HelperExitLiveBMarker) error {
	contents, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return errors.New("encode live B checkpoint")
	}
	directory := filepath.Dir(path)
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("live B checkpoint parent is not canonical and absolute")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("live B checkpoint already exists or cannot be inspected")
	}
	temporary, err := os.CreateTemp(directory, ".live-b-observed-*.tmp")
	if err != nil {
		return errors.New("create private same-directory live B checkpoint")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.New("secure live B checkpoint temporary")
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return errors.New("write live B checkpoint temporary")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("sync live B checkpoint temporary")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("close live B checkpoint temporary")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("atomically publish live B checkpoint")
	}
	parent, err := os.Open(directory)
	if err != nil {
		return errors.New("open live B checkpoint parent for sync")
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return errors.New("sync live B checkpoint parent")
	}
	return nil
}

func linuxV2WaitHelperWitnessSelectorRemoved(timeout time.Duration) error {
	path := "/etc/symmetry/linux-owner-witness.json"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("root CI did not unlink the exact helper selector after live-B observation")
}

func waitLinuxV2HelperExitMirrorReceipt(stateDirectory string, key state.RunKey, timeout time.Duration) (linuxWitnessJournalFile, error) {
	return waitForLinuxWitnessJournal(stateDirectory, "helper-exit mirror v2 StopReceipt", timeout, func(file linuxWitnessJournalFile) bool {
		return file.Journal.Key() == key && file.Journal.ContainmentCgroupReleaseProof != nil &&
			file.Journal.ContainmentCgroupReleaseProof.StopReceipt != nil
	})
}

func linuxV2RequireEmptyHelperExitLeaf(fixture linuxV2NativeFixture, identity authority.LinuxCgroupIdentity) error {
	path := linuxV2LeafPath(fixture.value, identity)
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect exact pinned helper-exit leaf after receipt: %w", err)
	}
	return errors.New("exact pinned helper-exit leaf remains after its StopReceipt and release tombstone")
}

func linuxV2ValidateHelperExitMirrorReceipt(
	initial state.RunJournal,
	reservation state.LinuxCgroupReservation,
	helperExit authority.LinuxCgroupExitProof,
	anchorRunReap platform.LinuxOwnerAnchorRunReapResult,
	final state.RunJournal,
) (authority.StopReceipt, error) {
	if reservation.Validate() != nil || helperExit.Validate() != nil || reservation.Helper == nil ||
		!helperExit.ValidFor(*reservation.Helper, reservation.Anchor) || reservation.Gate == nil || reservation.Mirror == nil ||
		reservation.Launcher == nil || reservation.LeafIdentity == nil || reservation.LauncherExit == nil {
		return authority.StopReceipt{}, errors.New("initial RunJournal omitted exact gate/helper/mirror/leaf/launcher-exit identities")
	}
	if initial.ContainmentAuthority == nil || initial.ContainmentAuthority.Validate() != nil ||
		initial.ContainmentAuthority.StopReceipt != nil || initial.ContainmentHandoff != nil {
		return authority.StopReceipt{}, errors.New("initial RunJournal omitted a valid exact committed authority to bind the release proof")
	}
	grant, err := linuxV2OwnerAnchorGrantFromJournal(initial, reservation)
	if err != nil || anchorRunReap.Stop.ValidateFor(grant, *reservation.Helper, reservation.Anchor, anchorRunReap.Processes, true) != nil ||
		anchorRunReap.Stop.ProducerFence != nil || anchorRunReap.Stop.Gate == nil || !anchorRunReap.Stop.Gate.Equal(*reservation.Gate) ||
		!anchorRunReap.Stop.GateReleaseAuthorized || anchorRunReap.Stop.HelperExit == nil ||
		!anchorRunReap.Stop.HelperExit.Equal(helperExit) {
		return authority.StopReceipt{}, errors.Join(errors.New("anchor WaitReapRun omitted the exact helper-death StopLeaf result"), err)
	}
	proof := final.ContainmentCgroupReleaseProof
	if proof == nil || proof.Validate() != nil || proof.Version != authority.SupervisorHandoffVersionV2 ||
		proof.Status != "released" || proof.LaunchToken != reservation.LaunchToken || proof.PipeToken != reservation.PipeToken ||
		proof.JobID != reservation.JobID || proof.OwnerUID != reservation.OwnerUID || proof.LauncherUID != reservation.LauncherUID ||
		proof.TargetPID != reservation.Gate.Process.PID || proof.TargetIdentity != reservation.Gate.Process.Identity ||
		proof.TargetBirthKind != authority.LinuxCgroupTargetBirthTrustedAGate ||
		proof.SupervisorPID != reservation.Helper.PID || proof.SupervisorIdentity != reservation.Helper.Identity ||
		proof.AnchorPID != reservation.Anchor.PID || proof.AnchorIdentity != reservation.Anchor.Identity ||
		proof.LauncherPID != reservation.Launcher.PID || proof.LauncherIdentity != reservation.Launcher.Identity ||
		proof.CgroupIdentity == nil || !proof.CgroupIdentity.Equal(*reservation.LeafIdentity) ||
		proof.LauncherExit == nil || !proof.LauncherExit.Equal(*reservation.LauncherExit) ||
		proof.Mirror == nil || !proof.Mirror.Equal(*reservation.Mirror) {
		return authority.StopReceipt{}, errors.New("App State lacks the exact receipt-backed gate/leaf release tombstone")
	}
	receipt := proof.StopReceipt
	if receipt == nil || receipt.Version != authority.SupervisorVersionV2 || receipt.Status != "stopped" ||
		receipt.ActiveProcesses != 0 || receipt.TargetPID != reservation.Gate.Process.PID ||
		receipt.TargetIdentity != reservation.Gate.Process.Identity || receipt.CgroupIdentity == nil ||
		!receipt.CgroupIdentity.Equal(*reservation.LeafIdentity) || receipt.OwnerUID != reservation.OwnerUID ||
		receipt.LauncherUID != reservation.LauncherUID || receipt.LinuxProof == nil {
		return authority.StopReceipt{}, errors.New("App State lacks an exact v2 StopReceipt for the released B gate")
	}
	expectedAuthority := initial.ContainmentAuthority.Clone()
	expectedAuthority.StopReceipt = receipt
	if !proof.ValidForSupervisor(expectedAuthority) {
		return authority.StopReceipt{}, errors.New("release proof differs from the exact pre-release RunJournal authority")
	}
	stop := receipt.LinuxProof
	if stop.Kind != authority.LinuxCgroupStopProofMirror || reservation.Mirror == nil || reservation.Helper == nil ||
		!stop.Issuer.Equal(*reservation.Mirror) || !stop.Reaper.Equal(reservation.Anchor) ||
		anchorRunReap.Stop.Gate == nil || !anchorRunReap.Stop.Gate.Equal(*reservation.Gate) ||
		reservation.LauncherExit == nil || !stop.LauncherExit.Equal(*reservation.LauncherExit) ||
		!stop.PinnedLeafFD || !stop.CgroupKillAcknowledged || !stop.CgroupPopulatedZero ||
		!stop.NoMigrationObserved || !stop.ProducerFenced || !stop.DescendantsReaped ||
		!anchorRunReap.Stop.KillAttempted || !anchorRunReap.Stop.KillAcknowledged ||
		!anchorRunReap.Stop.PinnedLeafFD || !anchorRunReap.Stop.CgroupPopulatedZero ||
		!anchorRunReap.Stop.NoMigrationObserved || !anchorRunReap.Stop.ProducerFenced ||
		!anchorRunReap.Stop.DescendantsReaped || stop.PinnedLeafFD != anchorRunReap.Stop.PinnedLeafFD ||
		stop.CgroupKillAcknowledged != anchorRunReap.Stop.KillAcknowledged ||
		stop.CgroupPopulatedZero != anchorRunReap.Stop.CgroupPopulatedZero ||
		stop.NoMigrationObserved != anchorRunReap.Stop.NoMigrationObserved ||
		stop.ProducerFenced != anchorRunReap.Stop.ProducerFenced ||
		stop.DescendantsReaped != anchorRunReap.Stop.DescendantsReaped ||
		stop.HelperExit == nil || !stop.HelperExit.Equal(*anchorRunReap.Stop.HelperExit) ||
		!stop.GateExit.Process.Equal(reservation.Gate.Process) || !stop.GateExit.Witness.Equal(reservation.Anchor) ||
		!stop.GateExit.Waited || !stop.GateExit.Reaped || stop.GateExit.HelperExit == nil ||
		!stop.GateExit.HelperExit.Equal(helperExit) || stop.HelperExit == nil || !stop.HelperExit.Equal(helperExit) {
		return authority.StopReceipt{}, errors.New("mirror StopReceipt does not carry exact helper/gate wait-reap and physical leaf proof")
	}
	if final.RunID != initial.RunID || final.Generation != initial.Generation || final.Fence() != initial.Fence() ||
		final.ContainmentCgroup != nil || final.ContainmentCgroupPreGateReleaseProof != nil ||
		final.ContainmentAuthority != nil || final.ContainmentHandoff != nil || final.HasProcessDetails() ||
		final.ContainmentUnproven || final.TerminalTaskResultKind != "" || final.TerminalState == "completed" ||
		final.GoalDeliveryEnabled || final.HasPendingGoalDeliveries() {
		return authority.StopReceipt{}, errors.New("RunJournal release did not clear the exact owner without retaining or fabricating another receipt")
	}
	return *receipt, nil
}
