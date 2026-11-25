//go:build linux && amd64

package platform

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

// LinuxOwnerHelperV2StopEvidence contains only measured v2 stop observations;
// it is not a State receipt and cannot clear a journal or release a cgroup.
type LinuxOwnerHelperV2StopEvidence struct {
	Version          int                                    `json:"version"`
	RunKey           LinuxOwnerAnchorRunKey                 `json:"run_key"`
	Fence            protocolFence                          `json:"fence"`
	LaunchToken      string                                 `json:"launch_token"`
	Parent           authority.LinuxCgroupParentIdentity    `json:"parent"`
	Leaf             authority.LinuxCgroupIdentity          `json:"leaf"`
	Anchor           authority.LinuxCgroupProcessIdentity   `json:"anchor"`
	Helper           authority.LinuxCgroupProcessIdentity   `json:"helper"`
	AnchorStop       *LinuxOwnerAnchorStopEvidence          `json:"anchor_stop"`
	RunReapEvidence  []LinuxOwnerAnchorReapEvidence         `json:"run_reap_evidence,omitempty"`
	Launcher         authority.LinuxCgroupProcessIdentity   `json:"launcher"`
	Gate             authority.LinuxCgroupGateIdentity      `json:"gate"`
	Proof            authority.LinuxCgroupStopProof         `json:"proof"`
	DescendantCount  uint64                                 `json:"descendant_count"`
	DescendantDigest string                                 `json:"descendant_digest"`
	PreGate          *LinuxOwnerHelperV2PreGateStopEvidence `json:"pre_gate,omitempty"`
}

// LinuxOwnerHelperV2PreGateStopEvidence is the helper's measured no-A-gate stop branch; only the
// daemon creates the State receipt, from the exact journal grant and this typed physical evidence.
type LinuxOwnerHelperV2PreGateStopEvidence struct {
	Version                int                                        `json:"version"`
	RunKey                 LinuxOwnerAnchorRunKey                     `json:"run_key"`
	Fence                  protocolFence                              `json:"fence"`
	LaunchToken            string                                     `json:"launch_token"`
	Parent                 authority.LinuxCgroupParentIdentity        `json:"parent"`
	Leaf                   authority.LinuxCgroupIdentity              `json:"leaf"`
	Anchor                 authority.LinuxCgroupProcessIdentity       `json:"anchor"`
	Helper                 authority.LinuxCgroupProcessIdentity       `json:"helper"`
	AnchorStop             *LinuxOwnerAnchorStopEvidence              `json:"anchor_stop"`
	PrepareAttempted       bool                                       `json:"prepare_attempted"`
	Launcher               *authority.LinuxCgroupProcessIdentity      `json:"launcher,omitempty"`
	LauncherExit           *authority.LinuxCgroupExitProof            `json:"launcher_exit,omitempty"`
	GateOutcome            *authority.LinuxCgroupGateProductionResult `json:"gate_outcome,omitempty"`
	Proof                  *authority.LinuxCgroupPreGateStopProof     `json:"proof,omitempty"`
	NoProducerCanAct       bool                                       `json:"no_producer_can_act"`
	PinnedLeafFD           bool                                       `json:"pinned_leaf_fd"`
	CgroupKillAcknowledged bool                                       `json:"cgroup_kill_acknowledged"`
	CgroupPopulatedZero    bool                                       `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool                                       `json:"no_migration_observed"`
	DescendantsReaped      bool                                       `json:"descendants_reaped"`
	DescendantCount        uint64                                     `json:"descendant_count"`
	DescendantDigest       string                                     `json:"descendant_digest"`
}

func (evidence LinuxOwnerHelperV2PreGateStopEvidence) ValidateFor(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) error {
	if grant.Validate() != nil || helper.Validate() != nil || evidence.Version != linuxOwnerHelperV2Version ||
		evidence.RunKey != grant.RunKey || evidence.Fence != grant.Fence || evidence.LaunchToken != grant.LaunchToken ||
		!evidence.Parent.Equal(grant.Parent) || !evidence.Leaf.Equal(grant.Leaf) || !evidence.Anchor.Equal(grant.Anchor) ||
		!evidence.Helper.Equal(helper) || evidence.AnchorStop == nil ||
		evidence.AnchorStop.ValidateFor(grant, helper, grant.Anchor, nil, false) != nil ||
		!evidence.NoProducerCanAct || !evidence.PinnedLeafFD ||
		!evidence.CgroupKillAcknowledged || !evidence.CgroupPopulatedZero || !evidence.NoMigrationObserved || !evidence.DescendantsReaped {
		return errors.New("Linux owner helper pre-gate stop evidence differs from the exact reservation or physical observations")
	}
	digest, err := hex.DecodeString(evidence.DescendantDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != evidence.DescendantDigest {
		return errors.New("Linux owner helper pre-gate stop evidence has an invalid exact-child digest")
	}
	if !evidence.PrepareAttempted {
		if evidence.Launcher != nil || evidence.LauncherExit != nil || evidence.GateOutcome != nil || evidence.Proof != nil {
			return errors.New("Linux owner helper pre-effect evidence contains a launcher or gate result")
		}
		return nil
	}
	if evidence.Launcher == nil || evidence.Launcher.Validate() != nil || evidence.LauncherExit == nil ||
		evidence.LauncherExit.Validate() != nil || evidence.GateOutcome == nil || evidence.Proof == nil ||
		!evidence.LauncherExit.ValidForOwnedDescendant(*evidence.Launcher, helper, grant.Anchor, nil) ||
		evidence.GateOutcome.ValidateFor(grant.LaunchToken, *evidence.Launcher) != nil {
		return errors.New("Linux owner helper no-gate evidence lacks the authenticated launcher exit")
	}
	proof := evidence.Proof
	if proof.Version != authority.LinuxCgroupIdentityVersion || proof.Kind != authority.LinuxCgroupStopProofHelper ||
		!proof.Issuer.Equal(helper) || !proof.Reaper.Equal(helper) || !proof.PinnedLeafFD ||
		!proof.CgroupKillAcknowledged || !proof.CgroupPopulatedZero || !proof.NoMigrationObserved ||
		!proof.ProducerFenced || !proof.DescendantsReaped || !proof.LauncherExit.Equal(*evidence.LauncherExit) ||
		!proof.GateOutcome.Equal(*evidence.GateOutcome) ||
		proof.CgroupKillAcknowledged != evidence.AnchorStop.KillAcknowledged ||
		proof.CgroupPopulatedZero != evidence.AnchorStop.CgroupPopulatedZero ||
		proof.NoMigrationObserved != evidence.AnchorStop.NoMigrationObserved ||
		proof.ProducerFenced != evidence.AnchorStop.ProducerFenced {
		return errors.New("Linux owner helper pre-gate physical proof is inconsistent")
	}
	return nil
}

const (
	linuxOwnerCLDExited = 1
	linuxOwnerCLDKilled = 2
	linuxOwnerCLDDumped = 3
)

func (evidence LinuxOwnerHelperV2StopEvidence) ValidateFor(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, launcher authority.LinuxCgroupProcessIdentity, gate authority.LinuxCgroupGateIdentity) error {
	if grant.Validate() != nil || helper.Validate() != nil || evidence.Version != linuxOwnerHelperV2Version ||
		evidence.RunKey != grant.RunKey || evidence.Fence != grant.Fence ||
		evidence.LaunchToken != grant.LaunchToken || !evidence.Parent.Equal(grant.Parent) || !evidence.Leaf.Equal(grant.Leaf) ||
		!evidence.Anchor.Equal(grant.Anchor) || !evidence.Helper.Equal(helper) {
		return errors.New("Linux owner helper stop evidence differs from the exact RunKey, fence, gate, or producers")
	}
	if evidence.PreGate != nil {
		if evidence.AnchorStop != nil || len(evidence.RunReapEvidence) != 0 || evidence.PreGate.ValidateFor(grant, helper) != nil ||
			launcher.Validate() == nil || gate.Validate() == nil {
			return errors.New("Linux owner helper pre-gate stop evidence is mixed with gate-bound authority")
		}
		return nil
	}
	if evidence.AnchorStop == nil || evidence.AnchorStop.HelperExit != nil || len(evidence.RunReapEvidence) != 0 ||
		evidence.AnchorStop.ValidateFor(grant, helper, grant.Anchor, nil, false) != nil ||
		evidence.AnchorStop.UntrustedProcess != nil ||
		!equalLinuxOwnerAnchorProcessPointer(evidence.AnchorStop.Launcher, &launcher) ||
		!equalLinuxOwnerAnchorGatePointer(evidence.AnchorStop.Gate, &gate) ||
		evidence.Proof.CgroupKillAcknowledged != evidence.AnchorStop.KillAcknowledged ||
		evidence.Proof.CgroupPopulatedZero != evidence.AnchorStop.CgroupPopulatedZero ||
		evidence.Proof.NoMigrationObserved != evidence.AnchorStop.NoMigrationObserved ||
		evidence.Proof.ProducerFenced != evidence.AnchorStop.ProducerFenced ||
		launcher.Validate() != nil || gate.Validate() != nil || !evidence.Launcher.Equal(launcher) ||
		!evidence.Gate.Equal(gate) || evidence.Proof.ValidateFor(linuxOwnerHelperV2Supervisor(grant, helper, launcher, gate, evidence.Proof.LauncherExit)) != nil {
		return errors.New("Linux owner helper gate stop proof differs from the exact producer identities")
	}
	decodedDigest, err := hex.DecodeString(evidence.DescendantDigest)
	if err != nil || len(decodedDigest) != sha256.Size || hex.EncodeToString(decodedDigest) != evidence.DescendantDigest {
		return errors.New("Linux owner helper stop evidence has an invalid exact-child digest")
	}
	return nil
}

// ValidateMirrorFor validates the post-helper-death anchor takeover proof: unlike ValidateFor it
// requires the anchor's exact helper exit and run-wide wait/reap set, never helper-supplied booleans.
func (evidence LinuxOwnerHelperV2StopEvidence) ValidateMirrorFor(
	grant LinuxOwnerAnchorGrant,
	helper, mirror authority.LinuxCgroupProcessIdentity,
	launcher authority.LinuxCgroupProcessIdentity,
	launcherExit authority.LinuxCgroupExitProof,
	gate authority.LinuxCgroupGateIdentity,
) error {
	if grant.Validate() != nil || helper.Validate() != nil || mirror.Validate() != nil ||
		launcher.Validate() != nil || launcherExit.Validate() != nil || gate.Validate() != nil || evidence.PreGate != nil ||
		evidence.Version != linuxOwnerHelperV2Version || evidence.RunKey != grant.RunKey ||
		evidence.Fence != grant.Fence || evidence.LaunchToken != grant.LaunchToken ||
		!evidence.Parent.Equal(grant.Parent) || !evidence.Leaf.Equal(grant.Leaf) ||
		!evidence.Anchor.Equal(grant.Anchor) || !evidence.Helper.Equal(helper) ||
		!evidence.Launcher.Equal(launcher) || !evidence.Gate.Equal(gate) || evidence.AnchorStop == nil ||
		evidence.AnchorStop.HelperExit == nil ||
		!equalLinuxOwnerAnchorProcessPointer(evidence.AnchorStop.Launcher, &launcher) ||
		!equalLinuxOwnerAnchorGatePointer(evidence.AnchorStop.Gate, &gate) ||
		evidence.AnchorStop.UserNamespace == nil ||
		!launcherExit.Process.Equal(launcher) || !launcherExit.ValidForOwnedDescendant(launcher, helper, grant.Anchor, nil) ||
		evidence.DescendantCount != evidence.AnchorStop.ReapCount || evidence.DescendantDigest != evidence.AnchorStop.ReapDigest {
		return errors.New("Linux v2 mirror stop evidence differs from the exact helper-death run")
	}
	if err := evidence.AnchorStop.ValidateFor(grant, helper, grant.Anchor, evidence.RunReapEvidence, true); err != nil {
		return fmt.Errorf("validate cached anchor mirror stop/reap evidence: %w", err)
	}
	if evidence.AnchorStop.HelperExit.Validate() != nil || !evidence.AnchorStop.HelperExit.ValidFor(helper, grant.Anchor) ||
		evidence.Proof.HelperExit == nil || !evidence.Proof.HelperExit.Equal(*evidence.AnchorStop.HelperExit) ||
		evidence.Proof.Kind != authority.LinuxCgroupStopProofMirror || !evidence.Proof.Issuer.Equal(mirror) ||
		!evidence.Proof.Reaper.Equal(grant.Anchor) || evidence.Proof.PinnedLeafFD != evidence.AnchorStop.PinnedLeafFD ||
		!evidence.Proof.LauncherExit.Equal(launcherExit) ||
		evidence.Proof.CgroupKillAcknowledged != evidence.AnchorStop.KillAcknowledged ||
		evidence.Proof.CgroupPopulatedZero != evidence.AnchorStop.CgroupPopulatedZero ||
		evidence.Proof.NoMigrationObserved != evidence.AnchorStop.NoMigrationObserved ||
		evidence.Proof.ProducerFenced != evidence.AnchorStop.ProducerFenced ||
		!evidence.Proof.DescendantsReaped {
		return errors.New("Linux v2 mirror proof is not derived from the exact anchor stop and helper exit")
	}
	var gateReap *LinuxOwnerAnchorReapEvidence
	for index := range evidence.RunReapEvidence {
		item := &evidence.RunReapEvidence[index]
		if item.ProcessRole != "gate" && item.ProcessRole != "native" {
			continue
		}
		if !item.Process.Equal(gate.Process) ||
			item.ProcessRole == "native" && !evidence.AnchorStop.GateReleaseAuthorized || gateReap != nil {
			return errors.New("Linux v2 mirror run-reap set contains an invalid or duplicate same-PID gate transition")
		}
		gateReap = item
	}
	if gateReap != nil && !gateReap.ProcessExit.Equal(evidence.Proof.GateExit) {
		return errors.New("Linux v2 mirror gate exit differs from the anchor's exact run-reap set")
	}
	supervisor := linuxOwnerHelperV2Supervisor(grant, helper, launcher, gate, launcherExit)
	mirrorCopy := mirror
	supervisor.Mirror = &mirrorCopy
	if err := evidence.Proof.ValidateFor(supervisor); err != nil {
		return fmt.Errorf("validate Linux v2 mirror StopReceipt proof: %w", err)
	}
	return nil
}

func pidfdKillAndReap(pidfd *os.File, expected authority.LinuxCgroupProcessIdentity) error {
	if pidfd == nil {
		return errors.New("unbootstrapped helper pidfd is unavailable")
	}
	fd := int(pidfd.Fd())
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(fd, expected); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		return fmt.Errorf("kill exact unbootstrapped helper pidfd: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	return linuxOwnerAnchorWaitPIDFD(ctx, pidfd, expected)
}

func pidfdKillAndReapUnidentified(pidfd int) error {
	if pidfd < 0 {
		return errors.New("unidentified helper pidfd is invalid")
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		return err
	}
	deadline := time.Now().Add(linuxOwnerAnchorWaitTimeout)
	for time.Now().Before(deadline) {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PIDFD, pidfd, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Signo != 0 {
			for {
				var reaped unix.Siginfo
				err := unix.Waitid(unix.P_PIDFD, pidfd, &reaped, unix.WEXITED, nil)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil {
					return err
				}
				if reaped.Signo != int32(unix.SIGCHLD) {
					return errors.New("unidentified helper wait did not return SIGCHLD")
				}
				return nil
			}
		}
		poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		if _, err := unix.Poll(poll, int(linuxOwnerAnchorWaitPoll/time.Millisecond)); err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
	}
	return context.DeadlineExceeded
}

func (helper *linuxOwnerHelperV2) stopEvidenceSnapshot() *LinuxOwnerHelperV2StopEvidence {
	if helper == nil {
		return nil
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	if helper.stopEvidence == nil {
		return nil
	}
	evidence := cloneLinuxOwnerHelperV2StopEvidence(*helper.stopEvidence)
	return &evidence
}

func (helper *linuxOwnerHelperV2) baseResponse(operation, status string) linuxOwnerHelperV2Response {
	return linuxOwnerHelperV2Response{
		Version: linuxOwnerHelperV2Version, Operation: operation, Status: status,
		RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
		Helper: helper.helper,
	}
}

func (helper *linuxOwnerHelperV2) writeResponse(connection *net.UnixConn, operation, status string, launcher *authority.LinuxCgroupProcessIdentity, gate *authority.LinuxCgroupGateIdentity, launcherExit *authority.LinuxCgroupExitProof, gateProcess *authority.LinuxCgroupProcessIdentity, gateOutcome *authority.LinuxCgroupGateProductionResult, leaseSequence uint64, evidence *LinuxOwnerHelperV2StopEvidence, errorCode string, fds ...int) error {
	return helper.writeResponseWithDetail(connection, nil, operation, status, launcher, gate, launcherExit, gateProcess, gateOutcome, leaseSequence, evidence, errorCode, fds...)
}

// The unresolved release response is the only writer of the bounded release
// error detail; every other response passes nil.
func (helper *linuxOwnerHelperV2) writeResponseWithDetail(connection *net.UnixConn, releaseErrorDetail *LinuxOwnerHelperV2ReleaseErrorDetail, operation, status string, launcher *authority.LinuxCgroupProcessIdentity, gate *authority.LinuxCgroupGateIdentity, launcherExit *authority.LinuxCgroupExitProof, gateProcess *authority.LinuxCgroupProcessIdentity, gateOutcome *authority.LinuxCgroupGateProductionResult, leaseSequence uint64, evidence *LinuxOwnerHelperV2StopEvidence, errorCode string, fds ...int) error {
	if err := releaseErrorDetail.Validate(); err != nil {
		return fmt.Errorf("Linux owner helper release response detail: %w", err)
	}
	if helper.dropWitnessResponse(connection, operation, status, evidence) {
		return nil
	}
	response := helper.baseResponse(operation, status)
	response.Launcher = launcher
	response.Gate = gate
	response.LauncherExit = launcherExit
	response.GateProcess = gateProcess
	response.GateOutcome = gateOutcome
	response.LeaseSequence = leaseSequence
	response.StopEvidence = evidence
	response.ErrorCode = errorCode
	response.ReleaseErrorDetail = releaseErrorDetail
	helper.stateMu.Lock()
	response.OwnerLost = helper.ownerLost
	response.CommitAcknowledged = helper.commitAcknowledged
	response.ReleaseAttempted = helper.releaseAttempted
	response.Released = helper.released
	helper.stateMu.Unlock()
	return writeLinuxOwnerHelperV2Response(connection, response, fds...)
}

func (helper *linuxOwnerHelperV2) dropWitnessResponse(
	connection *net.UnixConn,
	operation, status string,
	evidence *LinuxOwnerHelperV2StopEvidence,
) bool {
	if helper == nil || connection == nil {
		return false
	}
	helper.stateMu.Lock()
	if helper.witnessDropUsed || helper.witnessRunKey != helper.grant.RunKey ||
		helper.witnessDropResponse != operation {
		helper.stateMu.Unlock()
		return false
	}
	mutated := false
	switch operation {
	case "stop":
		mutated = status == "stopped" && evidence != nil && helper.stopEvidence != nil &&
			evidence.RunKey == helper.grant.RunKey && evidence.Fence == helper.grant.Fence &&
			helper.stopEvidence.RunKey == helper.grant.RunKey && helper.stopEvidence.Fence == helper.grant.Fence
	case "ack-stop-receipt":
		mutated = status == "receipt_acknowledged" &&
			((helper.receiptAcknowledged && helper.acknowledgedReceipt != nil) || helper.acknowledgedPreGateReceipt != nil)
	}
	if !mutated {
		helper.stateMu.Unlock()
		return false
	}
	helper.witnessDropUsed = true
	helper.stateMu.Unlock()
	// The operation's measured state is already cached. Closing only this
	// response channel creates a lost-ACK witness without forging evidence.
	_ = connection.Close()
	return true
}

func (helper *linuxOwnerHelperV2) sendPrepareResponse(connection *net.UnixConn, response linuxOwnerHelperV2Response) error {
	if helper.dropPreparedGateResponse(connection, response) {
		return nil
	}
	if response.Operation == "gate-ready" && response.Gate != nil && helper.prepared != nil {
		pidfd, err := unix.FcntlInt(helper.prepared.PIDFD().Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("duplicate exact parked gate pidfd for daemon mirror: %w", err)
		}
		defer unix.Close(pidfd)
		return writeLinuxOwnerHelperV2Response(connection, response, pidfd)
	}
	if response.Operation == "prepare-failed" && response.GateProcess != nil && helper.prepared != nil {
		pidfd, err := unix.FcntlInt(helper.prepared.PIDFD().Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("duplicate unresolved gate pidfd for daemon recovery: %w", err)
		}
		defer unix.Close(pidfd)
		return writeLinuxOwnerHelperV2Response(connection, response, pidfd)
	}
	return writeLinuxOwnerHelperV2Response(connection, response)
}

func (helper *linuxOwnerHelperV2) dropPreparedGateResponse(connection *net.UnixConn, response linuxOwnerHelperV2Response) bool {
	if helper == nil || connection == nil {
		return false
	}
	helper.stateMu.Lock()
	cached := helper.prepareResponse
	valid := helper.witnessDropResponse == "launcher-ready" && !helper.witnessDropUsed &&
		helper.witnessRunKey == helper.grant.RunKey && helper.prepareStarted && helper.prepared != nil && helper.gate != nil &&
		helper.launcher != nil && helper.launcherExit != nil && cached != nil && cached.Operation == "gate-ready" &&
		cached.Status == "ready" && response.Operation == "gate-ready" && response.Status == "ready" &&
		cached.Gate != nil && response.Gate != nil && cached.Gate.Equal(*response.Gate) &&
		cached.Launcher != nil && response.Launcher != nil && cached.Launcher.Equal(*response.Launcher) &&
		cached.LauncherExit != nil && response.LauncherExit != nil && cached.LauncherExit.Equal(*response.LauncherExit) &&
		cached.UserNamespace != nil && response.UserNamespace != nil && *cached.UserNamespace == *response.UserNamespace &&
		helper.gateUserNamespace != nil && *helper.gateUserNamespace == *response.UserNamespace &&
		helper.gate.Equal(*response.Gate) && helper.launcher.Equal(*response.Launcher) && helper.launcherExit.Equal(*response.LauncherExit)
	if valid {
		helper.witnessDropUsed = true
	}
	helper.stateMu.Unlock()
	if !valid {
		return false
	}
	_ = connection.Close()
	return true
}

func (helper *linuxOwnerHelperV2) cachePrepareFailure(connection *net.UnixConn, code string, outcome *authority.LinuxCgroupGateProductionResult) error {
	response := linuxOwnerHelperV2Response{
		Version: linuxOwnerHelperV2Version, Operation: "prepare-failed", Status: "unresolved",
		RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
		Helper: helper.helper, ErrorCode: code, GateOutcome: outcome,
	}
	helper.stateMu.Lock()
	helper.prepareResponse = &response
	helper.stateMu.Unlock()
	return helper.sendPrepareResponse(connection, response)
}

func (helper *linuxOwnerHelperV2) handleStop(connection *net.UnixConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerHelperV2StopTimeout)
	defer cancel()
	evidence, err := helper.stopOwned(ctx)
	if err != nil {
		responseErr := helper.writeResponse(connection, "stop", "unresolved", nil, nil, nil, nil, nil, 0, nil, linuxOwnerHelperV2ErrorCode(err))
		helper.requestAnchorTakeover()
		return responseErr
	}
	if evidence.PreGate != nil {
		return helper.writeResponse(connection, "stop", "stopped", evidence.PreGate.Launcher, nil, evidence.PreGate.LauncherExit, nil, evidence.PreGate.GateOutcome, 0, &evidence, "")
	}
	return helper.writeResponse(connection, "stop", "stopped", &evidence.Launcher, &evidence.Gate, &evidence.Proof.LauncherExit, nil, nil, 0, &evidence, "")
}

func (helper *linuxOwnerHelperV2) handleFenceBeforeAbort(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	evidence := helper.stopEvidence
	if helper.finalizing || helper.finalized || evidence == nil || evidence.PreGate == nil || evidence.PreGate.PrepareAttempted || helper.prepareStarted ||
		!evidence.PreGate.NoProducerCanAct {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "fence-before-abort", "rejected", nil, nil, nil, nil, nil, 0, nil, "launcher_or_gate_may_exist")
	}
	helper.finalizing = true
	helper.stateMu.Unlock()
	if err := helper.closeAfterClear(); err != nil {
		responseErr := helper.writeResponse(connection, "fence-before-abort", "unresolved", nil, nil, nil, nil, nil, 0, evidence, "helper_pin_retirement_failed")
		helper.requestAnchorTakeover()
		return responseErr
	}
	if err := helper.writeResponse(connection, "fence-before-abort", "fenced_before_abort", nil, nil, nil, nil, nil, 0, evidence, ""); err != nil {
		helper.requestAnchorTakeover()
		return err
	}
	helper.finalizeAfterClear(connection)
	return nil
}

func (helper *linuxOwnerHelperV2) verifyLeafMembershipBeforeStop(
	ctx context.Context,
	prepared *linuxgate.PreparedGate,
	launcher *authority.LinuxCgroupProcessIdentity,
	launcherExit *authority.LinuxCgroupExitProof,
	gate *authority.LinuxCgroupGateIdentity,
	untrustedProcess *authority.LinuxCgroupProcessIdentity,
	releaseAttempted, released bool,
) error {
	if helper == nil || ctx == nil || helper.leaf == nil ||
		!helper.leaf.Identity().Equal(helper.grant.Leaf) ||
		(launcher == nil) != (launcherExit == nil) || released && !releaseAttempted ||
		(releaseAttempted && gate == nil) || gate != nil && (prepared == nil || untrustedProcess != nil) ||
		gate == nil && prepared != nil && untrustedProcess == nil ||
		untrustedProcess != nil && (prepared == nil || gate != nil || releaseAttempted || released || untrustedProcess.Validate() != nil) {
		return errors.New("Linux owner helper pre-kill leaf-membership audit is incomplete")
	}
	if err := helper.leaf.verifyLive(); err != nil {
		return fmt.Errorf("verify exact pinned leaf before anchor stop: %w", err)
	}
	if launcher != nil && (launcher.Validate() != nil ||
		!launcherExit.ValidForOwnedDescendant(*launcher, helper.helper, helper.grant.Anchor, nil)) {
		return errors.New("Linux owner helper has no exact completed launcher producer fence")
	}
	gateLive := false
	untrustedLive := false
	if gate != nil {
		preparedIdentity := prepared.Identity()
		registeredIdentity, parseErr := parseLinuxProcessIdentity(gate.Process.Identity)
		if prepared.PIDFD() == nil || parseErr != nil || registeredIdentity.version != linuxProcessIdentityVersion ||
			registeredIdentity.pid != preparedIdentity.PID || registeredIdentity.bootID != preparedIdentity.BootID ||
			registeredIdentity.startTime != preparedIdentity.StartTime || gate.Process.PID != preparedIdentity.PID ||
			helper.gateUserNamespace == nil || !helper.gateUserNamespace.valid() ||
			helper.gateUserNamespace.Device != preparedIdentity.UserNSDevice ||
			helper.gateUserNamespace.Inode != preparedIdentity.UserNSInode {
			return errors.New("Linux owner helper gate/PIDFD/user namespace differs from the exact prepared leaf member")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(prepared.PIDFD())
		if err != nil {
			return fmt.Errorf("probe exact gate PIDFD before anchor stop: %w", err)
		}
		gateLive = !ready
		if gateLive {
			identity, err := ProcessIdentity(gate.Process.PID)
			if err != nil || identity != gate.Process.Identity {
				return fmt.Errorf("verify exact live gate identity before anchor stop: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
			}
			if err := prepared.VerifyLeafMembership(int(helper.leaf.leafFD)); err != nil {
				return fmt.Errorf("verify exact live gate remains in its prepared leaf: %w", err)
			}
		}
	}
	if untrustedProcess != nil {
		preparedIdentity, err := linuxOwnerHelperUntrustedProcessIdentity(prepared.Identity(), prepared.PIDFD(), helper.helper, launcher)
		if err != nil || !preparedIdentity.Equal(*untrustedProcess) {
			return fmt.Errorf("verify exact failed-Prepare child PIDFD identity: %w", errOr(err, ErrLinuxOwnerIdentityDiffers))
		}
		ready, err := linuxOwnerAnchorPIDFDReady(prepared.PIDFD())
		if err != nil {
			return fmt.Errorf("probe failed-Prepare child PIDFD before anchor stop: %w", err)
		}
		untrustedLive = !ready
	}
	leafPIDs, err := helper.readRunLeafProcessIDs(ctx)
	if err != nil {
		return fmt.Errorf("read exact run-leaf process set before recursive membership audit: %w", err)
	}
	if len(leafPIDs) > linuxOwnerHelperV2MaxMembershipProcesses {
		return errors.New("recursive run-process membership scan exceeds its bounded process count")
	}
	processes := make(map[int]linuxOwnerHelperV2RunProcessSnapshot, len(leafPIDs))
	for pid := range leafPIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			return fmt.Errorf("capture exact run-leaf process identity for membership audit: %w", err)
		}
		credentials, err := readLinuxOwnerProcessCredentials(pid)
		if err != nil {
			return fmt.Errorf("read exact run-leaf process credentials for membership audit: %w", err)
		}
		processes[pid] = linuxOwnerHelperV2RunProcessSnapshot{
			process: authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}, credentials: credentials,
		}
	}
	if launcher != nil {
		if _, exists := leafPIDs[launcher.PID]; exists {
			return errors.New("root launcher PID remains in the run leaf after its exact producer wait")
		}
	}
	treeOrder, err := linuxOwnerHelperV2RunTreeOrder(helper.helper.PID, processes)
	if err != nil {
		return fmt.Errorf("validate recursive helper-owned process tree: %w", err)
	}
	identities := map[int]string{helper.helper.PID: helper.helper.Identity}
	helperIdentity, err := ProcessIdentity(helper.helper.PID)
	if err != nil || helperIdentity != helper.helper.Identity {
		return fmt.Errorf("verify exact helper identity before StopLeaf: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	verifiedParents := map[int]struct{}{helper.helper.PID: {}}
	descendants := make(map[int]authority.LinuxCgroupProcessIdentity)
	liveDescendants := make(map[int]struct{})
	gateSeen := false
	untrustedSeen := false
	helper.stateMu.Lock()
	previouslyReaped := make(map[int]authority.LinuxCgroupProcessIdentity, len(helper.descendantReaped))
	for pid, identity := range helper.descendantReaped {
		previouslyReaped[pid] = identity
	}
	helper.stateMu.Unlock()
	for _, pid := range treeOrder {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot := processes[pid]
		parentPID := snapshot.credentials.Parent
		if _, parentVerified := verifiedParents[parentPID]; !parentVerified {
			parentIdentity, parentKnown := identities[parentPID]
			if !parentKnown {
				return errors.New("recursive helper-owned process tree lost an exact parent identity")
			}
			actualParentIdentity, err := ProcessIdentity(parentPID)
			if err != nil || actualParentIdentity != parentIdentity {
				return fmt.Errorf("verify exact process-tree parent identity before StopLeaf: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
			}
			verifiedParents[parentPID] = struct{}{}
		}
		process := snapshot.process
		identity, err := ProcessIdentity(pid)
		if err != nil || identity != process.Identity {
			return fmt.Errorf("capture exact recursive run-process identity: %w", errOr(err, ErrLinuxOwnerCreationIdentityChanged))
		}
		if launcher != nil && pid == launcher.PID {
			if process.Equal(*launcher) {
				return errors.New("root launcher remains in the recursive process tree after its exact wait")
			}
			return errors.New("launcher PID was reused in the recursive process tree before StopLeaf")
		}
		if previous, wasReaped := previouslyReaped[pid]; wasReaped {
			if previous.Equal(process) {
				return errors.New("an exactly reaped descendant remains in the live process tree")
			}
			return errors.New("a reaped descendant PID was reused before StopLeaf")
		}
		if gate != nil && pid == gate.Process.PID && !process.Equal(gate.Process) {
			return errors.New("gate PID was reused before the anchor stop fence")
		}
		if untrustedProcess != nil && pid == untrustedProcess.PID && !process.Equal(*untrustedProcess) {
			return errors.New("failed-Prepare child PID was reused before the anchor stop fence")
		}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			return fmt.Errorf("open exact descendant pidfd before StopLeaf: %w", err)
		}
		if err := verifyLinuxOwnerAnchorPIDFD(pidfd, process); err != nil {
			_ = unix.Close(pidfd)
			return err
		}
		pidfdFile := os.NewFile(uintptr(pidfd), "linux-owner-helper-stop-membership-pidfd")
		if pidfdFile == nil {
			_ = unix.Close(pidfd)
			return errors.New("wrap exact run descendant pidfd for StopLeaf membership")
		}
		pidfdReady, pollErr := linuxOwnerAnchorPIDFDReady(pidfdFile)
		_ = pidfdFile.Close()
		if pollErr != nil {
			return fmt.Errorf("poll exact descendant pidfd before StopLeaf: %w", pollErr)
		}
		credentials, err := readLinuxOwnerProcessCredentials(pid)
		if err != nil || credentials != snapshot.credentials || credentials.Parent != parentPID {
			return fmt.Errorf("revalidate exact recursive process ancestry before StopLeaf: %w", errOr(err, errors.New("credentials or parent changed")))
		}
		identityAfter, err := ProcessIdentity(pid)
		if err != nil || identityAfter != process.Identity {
			return fmt.Errorf("revalidate exact recursive process identity before StopLeaf: %w", errOr(err, ErrLinuxOwnerCreationIdentityChanged))
		}
		role := "descendant"
		if gate != nil && process.Equal(gate.Process) {
			gateSeen = true
			role = "gate"
			if released {
				role = "native"
			}
		}
		if untrustedProcess != nil && process.Equal(*untrustedProcess) {
			untrustedSeen = true
		}
		if !pidfdReady {
			if role == "gate" && releaseAttempted {
				gateCredErr := verifyLinuxOwnerRunChildCredentials(pid, "gate", helper.owner)
				if gateCredErr != nil {
					if nativeCredErr := verifyLinuxOwnerRunChildCredentials(pid, "native", helper.owner); nativeCredErr != nil {
						return errors.Join(gateCredErr, nativeCredErr)
					}
				}
			} else if err := verifyLinuxOwnerRunChildCredentials(pid, role, helper.owner); err != nil {
				return err
			}
			if err := verifyLinuxOwnerRunChildMembership(pid, role, helper.grant, helper.leaf); err != nil {
				return fmt.Errorf("verify recursive run-process leaf membership before StopLeaf: %w", err)
			}
			liveDescendants[pid] = struct{}{}
		}
		identities[pid] = process.Identity
		descendants[pid] = process
	}
	if gateLive && !gateSeen {
		return errors.New("live gate PIDFD was absent from the recursive helper-owned process tree")
	}
	if untrustedLive && !untrustedSeen {
		return errors.New("live failed-Prepare child PIDFD was absent from the recursive helper-owned process tree")
	}
	leafPIDs, err = helper.readRunLeafProcessIDs(ctx)
	if err != nil {
		return fmt.Errorf("read recursive exact leaf members before StopLeaf: %w", err)
	}
	for pid := range leafPIDs {
		if _, runMember := descendants[pid]; !runMember {
			return errors.New("exact run leaf contains a process outside the recursive helper-owned run tree")
		}
	}
	for pid := range liveDescendants {
		if _, leafMember := leafPIDs[pid]; !leafMember {
			return errors.New("live recursive run descendant is absent from the exact leaf subtree")
		}
	}
	return nil
}

func (helper *linuxOwnerHelperV2) stopLeafThroughAnchor(ctx context.Context, fence LinuxOwnerAnchorProducerFence) (*LinuxOwnerAnchorStopEvidence, error) {
	if helper == nil || ctx == nil || helper.anchorClient == nil || helper.leaf == nil || fence.ValidateFor(helper.grant, helper.helper) != nil {
		return nil, errors.New("Linux owner helper lacks the exact anchor StopLeaf producer fence")
	}
	evidence, err := helper.anchorClient.StopLeaf(ctx, helper.grant, helper.helper, fence)
	if err != nil {
		return nil, fmt.Errorf("anchor StopLeaf outcome is unresolved: %w", err)
	}
	if err := evidence.ValidateFor(helper.grant, helper.helper, helper.grant.Anchor, nil, false); err != nil ||
		evidence.ProducerFence == nil || !evidence.ProducerFence.Equal(fence) || evidence.HelperExit != nil {
		return nil, errors.Join(errors.New("anchor StopLeaf evidence differs from the exact live-helper producer fence"), err)
	}
	if err := helper.leaf.WaitEmpty(ctx); err != nil {
		return nil, fmt.Errorf("remeasure exact pinned leaf after anchor StopLeaf: %w", err)
	}
	copy := evidence.Clone()
	return &copy, nil
}

func (helper *linuxOwnerHelperV2) stopOwned(ctx context.Context) (LinuxOwnerHelperV2StopEvidence, error) {
	if ctx == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux owner helper stop context is nil")
	}
	helper.stateMu.Lock()
	if helper.stopEvidence != nil {
		evidence := cloneLinuxOwnerHelperV2StopEvidence(*helper.stopEvidence)
		helper.stateMu.Unlock()
		return evidence, nil
	}
	if helper.stopInFlight {
		done := helper.stopDone
		helper.stateMu.Unlock()
		select {
		case <-ctx.Done():
			return LinuxOwnerHelperV2StopEvidence{}, ctx.Err()
		case <-done:
		}
		helper.stateMu.Lock()
		if helper.stopEvidence != nil {
			evidence := cloneLinuxOwnerHelperV2StopEvidence(*helper.stopEvidence)
			helper.stateMu.Unlock()
			return evidence, nil
		}
		code := helper.stopErrorCode
		helper.stateMu.Unlock()
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: concurrent stop attempt was unresolved (%s)", ErrLinuxOwnerHelperV2StopUnproven, code)
	}
	helper.stopAttempted = true
	helper.stopRequested = true
	helper.stopInFlight = true
	helper.stopDone = make(chan struct{})
	done := helper.stopDone
	helper.stopErrorCode = ""
	helper.lease.stop()
	if helper.prepareCancel != nil {
		helper.prepareCancel()
	}
	if helper.releaseCancel != nil {
		helper.releaseCancel()
	}
	preparedForAbort := helper.prepared
	if preparedForAbort != nil && !helper.released {
		_ = preparedForAbort.Abort()
	}
	prepareStarted := helper.prepareStarted
	prepareDone := helper.prepareDone
	releaseAttempted := helper.releaseAttempted
	releaseDone := helper.releaseDone
	helper.stateMu.Unlock()
	defer func() {
		helper.stateMu.Lock()
		helper.stopInFlight = false
		helper.stopDone = nil
		close(done)
		helper.stateMu.Unlock()
	}()
	prepareQuiesced := !prepareStarted
	if prepareStarted {
		if prepareDone == nil {
			helper.setStopError("prepare_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: Prepare has no exact completion signal", ErrLinuxOwnerHelperV2StopUnproven)
		}
		select {
		case <-prepareDone:
			prepareQuiesced = true
		case <-ctx.Done():
			helper.setStopError("prepare_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: wait for root producer fencing: %v", ErrLinuxOwnerHelperV2StopUnproven, ctx.Err())
		}
	}
	releaseQuiesced := !releaseAttempted
	if releaseAttempted {
		if releaseDone == nil {
			helper.setStopError("release_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: RELEASE has no exact completion signal", ErrLinuxOwnerHelperV2StopUnproven)
		}
		select {
		case <-releaseDone:
			releaseQuiesced = true
		case <-ctx.Done():
			helper.setStopError("release_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: one-use RELEASE did not quiesce after stop latch: %v", ErrLinuxOwnerHelperV2StopUnproven, ctx.Err())
		}
	}
	helper.stateMu.Lock()
	prepared := helper.prepared
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	launcherExit := cloneLinuxOwnerAnchorExitProof(helper.launcherExit)
	gateOutcome := helper.gateOutcome
	gateUserNamespace := helper.gateUserNamespace
	currentPrepareStarted := helper.prepareStarted
	currentReleaseAttempted := helper.releaseAttempted
	released := helper.released
	stopLatched := helper.stopRequested && helper.stopAttempted
	helper.stateMu.Unlock()
	if currentPrepareStarted != prepareStarted || currentReleaseAttempted != releaseAttempted || !stopLatched || !helper.lease.isStopped() {
		helper.setStopError("producer_fence_state_changed")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: helper producer-fence state changed after stop latch", ErrLinuxOwnerHelperV2StopUnproven)
	}
	if prepared == nil && gate == nil {
		if prepareStarted && (launcher == nil || launcherExit == nil || gateOutcome == nil ||
			gateOutcome.ValidateFor(helper.grant.LaunchToken, *launcher) != nil ||
			!launcherExit.ValidForOwnedDescendant(*launcher, helper.helper, helper.grant.Anchor, nil)) {
			helper.setStopError("no_gate_producer_exit_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: helper cannot prove that the exact launcher never produced a gate", ErrLinuxOwnerHelperV2StopUnproven)
		}
		if releaseAttempted || released || releaseDone != nil && !releaseQuiesced {
			helper.setStopError("pre_gate_release_state_invalid")
			return LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux owner helper pre-gate stop has an impossible RELEASE phase")
		}
		if err := helper.verifyLeafMembershipBeforeStop(ctx, nil, launcher, launcherExit, nil, nil, false, false); err != nil {
			helper.setStopError("leaf_membership_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("verify pre-gate producer membership before anchor StopLeaf: %w", err)
		}
		var gateOutcomeCopy *authority.LinuxCgroupGateProductionResult
		if gateOutcome != nil {
			copy := *gateOutcome
			gateOutcomeCopy = &copy
		}
		fence := LinuxOwnerAnchorProducerFence{
			Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
			LaunchToken: helper.grant.LaunchToken, Helper: helper.helper,
			StopLatched: stopLatched, LeaseStopped: helper.lease.isStopped(),
			PrepareAttempted: prepareStarted, PrepareQuiesced: prepareQuiesced,
			ReleaseAttempted: releaseAttempted, ReleaseQuiesced: releaseQuiesced, Released: released,
			LeafMembershipVerified: true, Launcher: cloneLinuxOwnerProcessIdentity(launcher),
			LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit), GateOutcome: gateOutcomeCopy,
		}
		anchorStop, err := helper.stopLeafThroughAnchor(ctx, fence)
		if err != nil {
			helper.setStopError("anchor_stop_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, err
		}
		descendantCount, descendantDigest, err := helper.reapAdoptedChildren(ctx, nil)
		if err != nil {
			helper.setStopError("descendant_reap_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: pre-gate run child could not be exactly reaped: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		preGate := &LinuxOwnerHelperV2PreGateStopEvidence{
			Version: linuxOwnerHelperV2Version, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
			LaunchToken: helper.grant.LaunchToken, Parent: helper.grant.Parent, Leaf: helper.grant.Leaf,
			Anchor: helper.grant.Anchor, Helper: helper.helper, AnchorStop: anchorStop, PrepareAttempted: prepareStarted,
			NoProducerCanAct: anchorStop.ProducerFenced, PinnedLeafFD: anchorStop.PinnedLeafFD,
			CgroupKillAcknowledged: anchorStop.KillAcknowledged,
			CgroupPopulatedZero:    anchorStop.CgroupPopulatedZero,
			NoMigrationObserved:    anchorStop.NoMigrationObserved, DescendantsReaped: true,
			DescendantCount: descendantCount, DescendantDigest: descendantDigest,
		}
		if prepareStarted {
			preGate.Launcher = launcher
			preGate.LauncherExit = launcherExit
			preGate.GateOutcome = gateOutcome
			preGate.Proof = &authority.LinuxCgroupPreGateStopProof{
				Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
				Issuer: helper.helper, Reaper: helper.helper, PinnedLeafFD: anchorStop.PinnedLeafFD,
				CgroupKillAcknowledged: anchorStop.KillAcknowledged,
				CgroupPopulatedZero:    anchorStop.CgroupPopulatedZero,
				NoMigrationObserved:    anchorStop.NoMigrationObserved,
				ProducerFenced:         anchorStop.ProducerFenced, DescendantsReaped: true,
				LauncherExit: *launcherExit, GateOutcome: *gateOutcome,
			}
		}
		evidence := LinuxOwnerHelperV2StopEvidence{
			Version: linuxOwnerHelperV2Version, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
			LaunchToken: helper.grant.LaunchToken, Parent: helper.grant.Parent, Leaf: helper.grant.Leaf,
			Anchor: helper.grant.Anchor, Helper: helper.helper, PreGate: preGate,
			DescendantCount: descendantCount, DescendantDigest: descendantDigest,
		}
		if err := evidence.ValidateFor(helper.grant, helper.helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
			helper.setStopError("pre_gate_evidence_invalid")
			return LinuxOwnerHelperV2StopEvidence{}, err
		}
		helper.stateMu.Lock()
		clonedEvidence := cloneLinuxOwnerHelperV2StopEvidence(evidence)
		helper.stopEvidence = &clonedEvidence
		helper.stateMu.Unlock()
		return evidence, nil
	}
	if prepared != nil && gate == nil {
		if !prepareStarted || launcher == nil || launcherExit == nil || gateOutcome != nil || releaseAttempted || released {
			helper.setStopError("untrusted_prepare_child_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: failed-Prepare child lacks an exact launcher fence or contradicts the no-gate outcome", ErrLinuxOwnerHelperV2StopUnproven)
		}
		helper.stateMu.Lock()
		untrustedProcess := cloneLinuxOwnerProcessIdentity(helper.untrustedProcess)
		helper.stateMu.Unlock()
		if untrustedProcess == nil {
			process, err := linuxOwnerHelperUntrustedProcessIdentity(prepared.Identity(), prepared.PIDFD(), helper.helper, launcher)
			if err != nil {
				helper.setStopError("untrusted_prepare_identity_unproven")
				return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact failed-Prepare child identity is unavailable: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
			}
			helper.stateMu.Lock()
			if helper.untrustedProcess == nil {
				helper.untrustedProcess = cloneLinuxOwnerProcessIdentity(&process)
			} else if !helper.untrustedProcess.Equal(process) {
				helper.stateMu.Unlock()
				helper.setStopError("untrusted_prepare_identity_conflict")
				return LinuxOwnerHelperV2StopEvidence{}, errors.New("failed-Prepare child identity changed during producer fencing")
			}
			untrustedProcess = cloneLinuxOwnerProcessIdentity(helper.untrustedProcess)
			helper.stateMu.Unlock()
		}
		if err := helper.verifyLeafMembershipBeforeStop(ctx, prepared, launcher, launcherExit, nil, untrustedProcess, false, false); err != nil {
			helper.setStopError("untrusted_prepare_membership_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: failed-Prepare child tree is not in the exact run leaf: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		fence := LinuxOwnerAnchorProducerFence{
			Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
			LaunchToken: helper.grant.LaunchToken, Helper: helper.helper,
			StopLatched: stopLatched, LeaseStopped: helper.lease.isStopped(),
			PrepareAttempted: true, PrepareQuiesced: prepareQuiesced,
			ReleaseQuiesced: releaseQuiesced, LeafMembershipVerified: true,
			Launcher:     cloneLinuxOwnerProcessIdentity(launcher),
			LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit), UntrustedProcess: untrustedProcess,
		}
		if _, err := helper.stopLeafThroughAnchor(ctx, fence); err != nil {
			helper.setStopError("untrusted_prepare_anchor_stop_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, err
		}
		if _, err := helper.waitUntrustedPreparedChild(ctx, prepared, *untrustedProcess); err != nil {
			helper.setStopError("untrusted_prepare_child_wait_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact failed-Prepare child wait/reap is unproven: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		if _, _, err := helper.reapAdoptedChildren(ctx, nil); err != nil {
			helper.setStopError("untrusted_prepare_descendant_reap_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: failed-Prepare descendants were not exactly reaped: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		helper.setStopError("untrusted_prepare_child_stopped_without_receipt")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: anchor stopped an untrusted failed-Prepare child, but no authenticated GateNeverProduced result permits a durable stop receipt", ErrLinuxOwnerHelperV2StopUnproven)
	}
	if prepared == nil || gate == nil || launcher == nil || launcherExit == nil {
		helper.setStopError("gate_or_producer_identity_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact gate or producer wait is unavailable", ErrLinuxOwnerHelperV2StopUnproven)
	}
	if err := ctx.Err(); err != nil {
		helper.setStopError("stop_context_expired_before_anchor_stop")
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	if err := helper.verifyLeafMembershipBeforeStop(ctx, prepared, launcher, launcherExit, gate, nil, releaseAttempted, released); err != nil {
		helper.setStopError("leaf_membership_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("verify exact run process tree before anchor StopLeaf: %w", err)
	}
	var userNamespace *LinuxUserNamespaceIdentity
	if gateUserNamespace != nil {
		copy := *gateUserNamespace
		userNamespace = &copy
	}
	fence := LinuxOwnerAnchorProducerFence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
		LaunchToken: helper.grant.LaunchToken, Helper: helper.helper,
		StopLatched: stopLatched, LeaseStopped: helper.lease.isStopped(),
		PrepareAttempted: prepareStarted, PrepareQuiesced: prepareQuiesced,
		ReleaseAttempted: releaseAttempted, ReleaseQuiesced: releaseQuiesced, Released: released,
		LeafMembershipVerified: true, Launcher: cloneLinuxOwnerProcessIdentity(launcher),
		LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit), Gate: cloneLinuxOwnerGateIdentity(gate),
		UserNamespace: userNamespace,
	}
	anchorStop, err := helper.stopLeafThroughAnchor(ctx, fence)
	if err != nil {
		helper.setStopError("anchor_stop_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	waitResult, err := helper.waitGate(ctx)
	if err != nil {
		helper.setStopError("gate_wait_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact gate pidfd wait/reap failed: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
	}
	descendantCount, descendantDigest, err := helper.reapAdoptedChildren(ctx, gate)
	if err != nil {
		helper.setStopError("descendant_reap_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: adopted run descendant could not be exactly reaped: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
	}
	gateExit := waitResult.Proof
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: helper.helper, Reaper: helper.helper, PinnedLeafFD: anchorStop.PinnedLeafFD,
		CgroupKillAcknowledged: anchorStop.KillAcknowledged, CgroupPopulatedZero: anchorStop.CgroupPopulatedZero,
		NoMigrationObserved: anchorStop.NoMigrationObserved, ProducerFenced: anchorStop.ProducerFenced,
		DescendantsReaped: true,
		LauncherExit:      *launcherExit, GateExit: gateExit,
	}
	evidence := LinuxOwnerHelperV2StopEvidence{
		Version: linuxOwnerHelperV2Version, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
		LaunchToken: helper.grant.LaunchToken, Parent: helper.grant.Parent, Leaf: helper.grant.Leaf,
		Anchor: helper.grant.Anchor, Helper: helper.helper, AnchorStop: anchorStop, Launcher: *launcher,
		Gate: *gate, Proof: proof, DescendantCount: descendantCount, DescendantDigest: descendantDigest,
	}
	if err := evidence.ValidateFor(helper.grant, helper.helper, *launcher, *gate); err != nil {
		helper.setStopError("stop_evidence_validation_failed")
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	if err := helper.prepared.Close(); err != nil {
		helper.setStopError("gate_descriptor_close_failed")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: close exact reaped gate owner: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
	}
	helper.stateMu.Lock()
	clonedEvidence := cloneLinuxOwnerHelperV2StopEvidence(evidence)
	helper.stopEvidence = &clonedEvidence
	helper.stateMu.Unlock()
	return evidence, nil
}

func (helper *linuxOwnerHelperV2) reapAdoptedChildren(ctx context.Context, gate *authority.LinuxCgroupGateIdentity) (uint64, string, error) {
	seen := make(map[int]authority.LinuxCgroupProcessIdentity, len(helper.descendantReaped))
	identityRows := make([]string, 0, len(helper.descendantReaped))
	for pid, process := range helper.descendantReaped {
		seen[pid] = process
		identityRows = append(identityRows, strconv.Itoa(pid)+":"+process.Identity)
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		pids, err := readLinuxOwnerProcessChildren(ctx, helper.helper)
		if err != nil {
			return 0, "", fmt.Errorf("read helper's exact kernel child list: %w", err)
		}
		if len(pids) == 0 {
			sort.Strings(identityRows)
			digest := sha256.Sum256([]byte(strings.Join(identityRows, "\n")))
			return uint64(len(seen)), hex.EncodeToString(digest[:]), nil
		}
		for _, pid := range pids {
			if gate != nil && pid == gate.Process.PID {
				return 0, "", errors.New("gate remained in helper child list after exact gate wait")
			}
			identity, err := ProcessIdentity(pid)
			if err != nil {
				return 0, "", fmt.Errorf("capture adopted child creation identity: %w", err)
			}
			process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
			if previous, exists := seen[pid]; exists && !previous.Equal(process) {
				return 0, "", errors.New("adopted child PID was reused during stop proof")
			}
			if _, exists := seen[pid]; exists {
				return 0, "", errors.New("exactly reaped descendant remains in the helper child list")
			}
			credentials, err := readLinuxOwnerProcessCredentials(pid)
			if err != nil || credentials.Parent != os.Getpid() {
				return 0, "", fmt.Errorf("verify exact adopted child parent: %w", errOr(err, ErrLinuxOwnerParentDiffersFromHelper))
			}
			if err := verifyLinuxOwnerRunChildCredentials(pid, "descendant", helper.owner); err != nil {
				return 0, "", err
			}
			if err := verifyLinuxOwnerRunChildMembership(pid, "descendant", helper.grant, helper.leaf); err != nil {
				return 0, "", err
			}
			pidfd, err := unix.PidfdOpen(pid, 0)
			if err != nil {
				return 0, "", fmt.Errorf("open pidfd for kernel-adopted run child: %w", err)
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
				_ = unix.Close(pidfd)
				return 0, "", err
			}
			pidfdFile := os.NewFile(uintptr(pidfd), "symmetry-owner-helper-adopted-child-pidfd")
			if pidfdFile == nil {
				_ = unix.Close(pidfd)
				return 0, "", errors.New("wrap pidfd for adopted run child")
			}
			classification, err := helper.anchorClient.ClassifyRunChildOrigin(
				ctx, helper.grant, helper.helper, process, pidfdFile,
			)
			if err != nil {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("anchor could not prove adopted child origin before helper wait: %w", err)
			}
			if err := classification.ValidateFor(helper.grant, helper.helper, process); err != nil {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("anchor returned an invalid adopted-helper child classification: %w", err)
			}
			if classification.Origin == linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly ||
				classification.Origin == linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign ||
				classification.Role != "descendant" || !classification.currentRunMember() {
				_ = pidfdFile.Close()
				return 0, "", errors.New("anchor classified adopted helper child outside the exact current RunKey descendant origin")
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(pidfdFile.Fd()), process); err != nil {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("revalidate adopted child pidfd after origin proof: %w", err)
			}
			identityAfter, err := ProcessIdentity(pid)
			if err != nil || identityAfter != process.Identity {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("revalidate adopted child identity after origin proof: %w", errOr(err, ErrLinuxOwnerCreationIdentityChanged))
			}
			credentials, err = readLinuxOwnerProcessCredentials(pid)
			if err != nil || credentials.Parent != os.Getpid() {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("revalidate adopted child parent after origin proof: %w", errOr(err, ErrLinuxOwnerParentDiffersFromHelper))
			}
			if err := verifyLinuxOwnerRunChildCredentials(pid, "descendant", helper.owner); err != nil {
				_ = pidfdFile.Close()
				return 0, "", err
			}
			if err := verifyLinuxOwnerRunChildMembership(pid, "descendant", helper.grant, helper.leaf); err != nil {
				_ = pidfdFile.Close()
				return 0, "", err
			}
			if err := linuxOwnerAnchorWaitPIDFD(ctx, pidfdFile, process); err != nil {
				_ = pidfdFile.Close()
				return 0, "", err
			}
			_ = pidfdFile.Close()
			seen[pid] = process
			if helper.descendantReaped == nil {
				helper.descendantReaped = make(map[int]authority.LinuxCgroupProcessIdentity)
			}
			helper.descendantReaped[pid] = process
			identityRows = append(identityRows, strconv.Itoa(pid)+":"+identity)
		}
	}
}

func (helper *linuxOwnerHelperV2) handleReceiptAck(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	if (request.Receipt == nil) == (request.PreGateReceipt == nil) {
		return helper.writeResponse(connection, "ack-stop-receipt", "rejected", nil, nil, nil, nil, nil, 0, nil, "receipt_missing")
	}
	if request.PreGateReceipt != nil {
		helper.stateMu.Lock()
		evidence := helper.stopEvidence
		if helper.acknowledgedPreGateReceipt != nil {
			exactReplay := helper.acknowledgedPreGateReceipt.Equal(*request.PreGateReceipt)
			helper.stateMu.Unlock()
			if !exactReplay {
				return helper.writeResponse(connection, "ack-stop-receipt", "rejected", nil, nil, nil, nil, nil, 0, nil, "receipt_replay_conflict")
			}
			return helper.writeResponse(connection, "ack-stop-receipt", "receipt_acknowledged", nil, nil, nil, nil, nil, 0, evidence, "")
		}
		helper.stateMu.Unlock()
		if evidence == nil || evidence.PreGate == nil || evidence.PreGate.Proof == nil ||
			!linuxOwnerHelperV2PreGateReceiptMatches(*request.PreGateReceipt, helper.grant, helper.helper, *evidence.PreGate) {
			return helper.writeResponse(connection, "ack-stop-receipt", "rejected", nil, nil, nil, nil, nil, 0, nil, "pre_gate_receipt_differs_from_measured_stop")
		}
		helper.stateMu.Lock()
		receipt := request.PreGateReceipt.Clone()
		helper.acknowledgedPreGateReceipt = &receipt
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "ack-stop-receipt", "receipt_acknowledged", nil, nil, nil, nil, nil, 0, evidence, "")
	}
	helper.stateMu.Lock()
	evidence := helper.stopEvidence
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	if helper.receiptAcknowledged {
		exactReplay := helper.acknowledgedReceipt != nil && helper.acknowledgedReceipt.Equal(*request.Receipt)
		helper.stateMu.Unlock()
		if !exactReplay {
			return helper.writeResponse(connection, "ack-stop-receipt", "rejected", nil, nil, nil, nil, nil, 0, nil, "receipt_replay_conflict")
		}
		return helper.writeResponse(connection, "ack-stop-receipt", "receipt_acknowledged", nil, nil, nil, nil, nil, 0, evidence, "")
	}
	helper.stateMu.Unlock()
	if evidence == nil || gate == nil || launcher == nil || !linuxOwnerHelperV2ReceiptMatches(*request.Receipt, helper.grant, helper.helper, *launcher, *gate, evidence.Proof) {
		return helper.writeResponse(connection, "ack-stop-receipt", "rejected", nil, nil, nil, nil, nil, 0, nil, "receipt_differs_from_measured_stop")
	}
	helper.stateMu.Lock()
	helper.receiptAcknowledged = true
	clonedReceipt := *request.Receipt
	if request.Receipt.CgroupIdentity != nil {
		identity := *request.Receipt.CgroupIdentity
		clonedReceipt.CgroupIdentity = &identity
	}
	if request.Receipt.LinuxProof != nil {
		proof := *request.Receipt.LinuxProof
		proof.LauncherExit = *cloneLinuxOwnerAnchorExitProof(&request.Receipt.LinuxProof.LauncherExit)
		proof.GateExit = *cloneLinuxOwnerAnchorExitProof(&request.Receipt.LinuxProof.GateExit)
		proof.HelperExit = cloneLinuxOwnerAnchorExitProof(request.Receipt.LinuxProof.HelperExit)
		clonedReceipt.LinuxProof = &proof
	}
	helper.acknowledgedReceipt = &clonedReceipt
	helper.stateMu.Unlock()
	return helper.writeResponse(connection, "ack-stop-receipt", "receipt_acknowledged", nil, nil, nil, nil, nil, 0, evidence, "")
}

func (helper *linuxOwnerHelperV2) handleFinalRelease(connection *net.UnixConn, request linuxOwnerHelperV2Request) (bool, error) {
	record, owner, alreadyFinalized, claimErr := helper.beginFinalization(request)
	if claimErr != nil {
		return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "finalization_proof_conflict")
	}
	evidence := helper.stopEvidenceSnapshot()
	if alreadyFinalized {
		return true, helper.writeResponse(connection, "release-run", "released_after_clear", nil, nil, nil, nil, nil, 0, evidence, "")
	}
	if !owner {
		if err := waitLinuxOwnerHelperV2Finalization(record); err != nil {
			return false, helper.writeResponse(connection, "release-run", "unresolved", nil, nil, nil, nil, nil, 0, evidence, "finalization_in_progress")
		}
		return true, helper.writeResponse(connection, "release-run", "released_after_clear", nil, nil, nil, nil, nil, 0, evidence, "")
	}
	return helper.performFinalRelease(connection, request, record)
}

func (helper *linuxOwnerHelperV2) performFinalRelease(
	connection *net.UnixConn,
	request linuxOwnerHelperV2Request,
	record *linuxOwnerHelperV2Finalization,
) (bool, error) {
	if request.AbortProof != nil && request.ReleaseProof == nil && request.PreGateReleaseProof == nil {
		helper.stateMu.Lock()
		evidence := helper.stopEvidence
		helper.stateMu.Unlock()
		proof := request.AbortProof
		helperIdentity := helper.helper
		if proof.Validate() != nil || proof.Helper == nil || !proof.Helper.Equal(helperIdentity) ||
			!proof.ValidFor(helper.grant.LaunchToken, helper.grant.Parent, helper.grant.Leaf.LeafName, true,
				helper.grant.OwnerUID, helper.grant.Anchor, proof.Mirror, proof.Helper, proof.Launcher) ||
			evidence == nil || evidence.PreGate == nil || evidence.PreGate.PrepareAttempted ||
			!evidence.PreGate.NoProducerCanAct || !evidence.PreGate.PinnedLeafFD || !evidence.PreGate.CgroupKillAcknowledged ||
			!evidence.PreGate.CgroupPopulatedZero || !evidence.PreGate.NoMigrationObserved || !evidence.PreGate.DescendantsReaped {
			helper.finishFinalization(record, errors.New("reservation abort proof differs from measured pre-gate stop"), true)
			return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "reservation_abort_proof_mismatch")
		}
		if err := helper.closeAfterClear(); err != nil {
			helper.finishFinalization(record, err, false)
			return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "aborted_helper_pin_retirement_failed")
		}
		helper.finishFinalization(record, nil, false)
		helper.finalizeAfterClear(connection)
		return true, helper.writeResponse(connection, "release-run", "released_after_clear", nil, nil, nil, nil, nil, 0, helper.stopEvidenceSnapshot(), "")
	}
	if request.PreGateReleaseProof != nil && request.ReleaseProof == nil {
		helper.stateMu.Lock()
		evidence := helper.stopEvidence
		receipt := helper.acknowledgedPreGateReceipt
		helper.stateMu.Unlock()
		proof := request.PreGateReleaseProof
		if evidence == nil || evidence.PreGate == nil || proof.Validate() != nil ||
			!proof.Receipt.CgroupIdentity.Equal(helper.grant.Leaf) || !proof.Receipt.Anchor.Equal(helper.grant.Anchor) ||
			!proof.Receipt.Helper.Equal(helper.helper) || !linuxOwnerHelperV2PreGateReceiptMatches(proof.Receipt, helper.grant, helper.helper, *evidence.PreGate) ||
			receipt != nil && !proof.ValidFor(*receipt) {
			helper.finishFinalization(record, errors.New("pre-gate release proof differs from the acknowledged receipt"), true)
			return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "pre_gate_release_or_receipt_mismatch")
		}
		if receipt == nil {
			receiptCopy := proof.Receipt.Clone()
			helper.stateMu.Lock()
			helper.acknowledgedPreGateReceipt = &receiptCopy
			helper.stateMu.Unlock()
		}
		if err := helper.closeAfterClear(); err != nil {
			helper.finishFinalization(record, err, false)
			return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "pre_gate_pin_retirement_failed")
		}
		helper.finishFinalization(record, nil, false)
		helper.finalizeAfterClear(connection)
		return true, helper.writeResponse(connection, "release-run", "released_after_clear", nil, nil, nil, nil, nil, 0, helper.stopEvidenceSnapshot(), "")
	}
	if request.ReleaseProof == nil || request.PreGateReleaseProof != nil || request.AbortProof != nil {
		helper.finishFinalization(record, errors.New("normal release proof shape is invalid"), true)
		return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "release_proof_missing")
	}
	helper.stateMu.Lock()
	evidence := helper.stopEvidence
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	receiptAcknowledged := helper.receiptAcknowledged
	acknowledgedReceipt := helper.acknowledgedReceipt
	helper.stateMu.Unlock()
	if evidence == nil || gate == nil || launcher == nil || request.ReleaseProof.StopReceipt == nil ||
		!linuxOwnerHelperV2ReceiptMatches(*request.ReleaseProof.StopReceipt, helper.grant, helper.helper, *launcher, *gate, evidence.Proof) ||
		!linuxOwnerHelperV2ReleaseProofMatches(*request.ReleaseProof, helper.grant, helper.helper, gate.Process, *launcher, evidence.Proof.LauncherExit) ||
		(acknowledgedReceipt != nil && !request.ReleaseProof.StopReceipt.Equal(*acknowledgedReceipt)) {
		helper.finishFinalization(record, errors.New("normal release proof differs from the acknowledged stop receipt"), true)
		return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "durable_release_or_receipt_missing")
	}
	if !receiptAcknowledged {
		receiptCopy := *request.ReleaseProof.StopReceipt
		if receiptCopy.CgroupIdentity != nil {
			identity := *receiptCopy.CgroupIdentity
			receiptCopy.CgroupIdentity = &identity
		}
		if receiptCopy.LinuxProof != nil {
			proofCopy := *receiptCopy.LinuxProof
			proofCopy.LauncherExit = *cloneLinuxOwnerAnchorExitProof(&receiptCopy.LinuxProof.LauncherExit)
			proofCopy.GateExit = *cloneLinuxOwnerAnchorExitProof(&receiptCopy.LinuxProof.GateExit)
			proofCopy.HelperExit = cloneLinuxOwnerAnchorExitProof(receiptCopy.LinuxProof.HelperExit)
			receiptCopy.LinuxProof = &proofCopy
		}
		helper.stateMu.Lock()
		helper.receiptAcknowledged = true
		helper.acknowledgedReceipt = &receiptCopy
		helper.stateMu.Unlock()
	}
	if err := helper.closeAfterClear(); err != nil {
		helper.finishFinalization(record, err, false)
		return false, helper.writeResponse(connection, "release-run", "rejected", nil, nil, nil, nil, nil, 0, nil, "helper_pin_retirement_failed")
	}
	helper.finishFinalization(record, nil, false)
	helper.finalizeAfterClear(connection)
	return true, helper.writeResponse(connection, "release-run", "released_after_clear", nil, nil, nil, nil, nil, 0, helper.stopEvidenceSnapshot(), "")
}

func (helper *linuxOwnerHelperV2) finalizeAfterClear(_ *net.UnixConn) {
	helper.stateMu.Lock()
	helper.finalized = true
	helper.finalizing = false
	listener := helper.listener
	helper.stateMu.Unlock()
	helper.lease.stop()
	if listener != nil {
		_ = listener.Close()
	}
}

func (helper *linuxOwnerHelperV2) setStopError(code string) {
	helper.stateMu.Lock()
	helper.stopErrorCode = code
	helper.stateMu.Unlock()
}

func (helper *linuxOwnerHelperV2) ensureGateWait() (<-chan struct{}, error) {
	helper.stateMu.Lock()
	prepared := helper.prepared
	helper.stateMu.Unlock()
	if prepared == nil {
		return nil, errors.New("Linux owner helper has no prepared gate to wait")
	}
	helper.waitMu.Lock()
	if !helper.waitStarted {
		helper.waitStarted = true
		helper.waitDone = make(chan struct{})
		done := helper.waitDone
		go func() {
			info, err := prepared.WaitLeader(context.Background())
			var result *LinuxOwnerHelperV2Exit
			if err == nil {
				result, err = linuxOwnerHelperV2ExitResult(info, helper.gate.Process, helper.helper)
			}
			helper.waitMu.Lock()
			helper.waitResult = result
			helper.waitError = err
			close(done)
			helper.waitMu.Unlock()
		}()
	}
	done := helper.waitDone
	helper.waitMu.Unlock()
	return done, nil
}

func (helper *linuxOwnerHelperV2) waitGateForConnection(connection *net.UnixConn) (LinuxOwnerHelperV2Exit, error) {
	done, err := helper.ensureGateWait()
	if err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	if connection == nil {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait connection is nil")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	var fd int
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	for {
		select {
		case <-done:
			helper.waitMu.Lock()
			result, waitErr := helper.waitResult, helper.waitError
			helper.waitMu.Unlock()
			if result == nil {
				return LinuxOwnerHelperV2Exit{}, fmt.Errorf("%w: exact gate wait/reap failed: %v", ErrLinuxOwnerHelperV2StopUnproven, waitErr)
			}
			return *result, nil
		default:
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		if _, err := unix.Poll(poll, int(linuxOwnerHelperV2PollInterval/time.Millisecond)); err != nil && !errors.Is(err, unix.EINTR) {
			return LinuxOwnerHelperV2Exit{}, fmt.Errorf("poll owner wait connection: %w", err)
		}
		if poll[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return LinuxOwnerHelperV2Exit{}, io.EOF
		}
		if poll[0].Revents&unix.POLLIN != 0 {
			return LinuxOwnerHelperV2Exit{}, errors.New("unexpected request arrived while exact gate wait was outstanding")
		}
	}
}

func (helper *linuxOwnerHelperV2) waitGate(ctx context.Context) (LinuxOwnerHelperV2Exit, error) {
	if ctx == nil {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait context is nil")
	}
	done, err := helper.ensureGateWait()
	if err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	select {
	case <-ctx.Done():
		return LinuxOwnerHelperV2Exit{}, ctx.Err()
	case <-done:
		helper.waitMu.Lock()
		result, waitErr := helper.waitResult, helper.waitError
		helper.waitMu.Unlock()
		if result == nil {
			return LinuxOwnerHelperV2Exit{}, fmt.Errorf("%w: exact gate wait/reap failed: %v", ErrLinuxOwnerHelperV2StopUnproven, waitErr)
		}
		return *result, nil
	}
}

func (helper *linuxOwnerHelperV2) waitUntrustedPreparedChild(
	ctx context.Context,
	prepared *linuxgate.PreparedGate,
	process authority.LinuxCgroupProcessIdentity,
) (LinuxOwnerHelperV2Exit, error) {
	if helper == nil || ctx == nil || prepared == nil || process.Validate() != nil {
		return LinuxOwnerHelperV2Exit{}, errors.New("failed-Prepare child wait is incomplete")
	}
	helper.untrustedWaitMu.Lock()
	if helper.untrustedWaitResult != nil {
		result := *helper.untrustedWaitResult
		result.Proof = result.Proof.Clone()
		helper.untrustedWaitMu.Unlock()
		return result, nil
	}
	if helper.untrustedWaitError != nil {
		err := helper.untrustedWaitError
		helper.untrustedWaitMu.Unlock()
		return LinuxOwnerHelperV2Exit{}, err
	}
	if helper.untrustedWaitAttempted {
		helper.untrustedWaitMu.Unlock()
		return LinuxOwnerHelperV2Exit{}, errors.New("failed-Prepare child wait is already in progress")
	}
	helper.untrustedWaitAttempted = true
	helper.untrustedWaitMu.Unlock()
	info, err := prepared.WaitLeader(ctx)
	if err != nil {
		helper.untrustedWaitMu.Lock()
		if ctx.Err() != nil {
			helper.untrustedWaitAttempted = false
		} else {
			helper.untrustedWaitError = err
		}
		helper.untrustedWaitMu.Unlock()
		return LinuxOwnerHelperV2Exit{}, fmt.Errorf("wait/reap exact failed-Prepare child by its retained pidfd: %w", err)
	}
	result, err := linuxOwnerHelperV2ExitResult(info, process, helper.helper)
	if err != nil || result == nil || !result.Proof.ValidForOwnedDescendant(process, helper.helper, helper.grant.Anchor, nil) {
		if err == nil {
			err = errors.New("failed-Prepare child wait proof differs from the exact helper-owned identity")
		}
		helper.untrustedWaitMu.Lock()
		helper.untrustedWaitError = err
		helper.untrustedWaitMu.Unlock()
		return LinuxOwnerHelperV2Exit{}, err
	}
	copy := *result
	copy.Proof = copy.Proof.Clone()
	helper.untrustedWaitMu.Lock()
	helper.untrustedWaitResult = &copy
	helper.untrustedWaitMu.Unlock()
	return copy, nil
}

func linuxOwnerHelperV2ExitResult(info unix.Siginfo, process, helper authority.LinuxCgroupProcessIdentity) (*LinuxOwnerHelperV2Exit, error) {
	if process.Validate() != nil || helper.Validate() != nil || info.Signo != int32(unix.SIGCHLD) {
		return nil, errors.New("Linux owner helper exact wait returned an invalid process status")
	}
	status, err := linuxOwnerSiginfoStatus(&info)
	if err != nil {
		return nil, err
	}
	result := &LinuxOwnerHelperV2Exit{
		Process: process,
		Proof: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: process,
			Witness: helper, Waited: true, Reaped: true,
		},
	}
	switch info.Code {
	case linuxOwnerCLDExited:
		result.Exited = true
		result.ExitCode = int(status)
	case linuxOwnerCLDKilled:
		result.Signal = int(status)
	case linuxOwnerCLDDumped:
		result.Signal = int(status)
		result.CoreDumped = true
	default:
		return nil, errors.New("Linux owner helper exact wait returned an unsupported wait code")
	}
	if err := result.Proof.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

// linuxOwnerSiginfoStatus reads si_status from the siginfo_t union written by waitid(P_PIDFD); x/sys
// hides the union payload, so the offset is the Linux UAPI SIGCHLD layout (si_pid, si_uid, si_status).
func linuxOwnerSiginfoStatus(info *unix.Siginfo) (int32, error) {
	if info == nil || unsafe.Sizeof(*info) != 128 {
		return 0, errors.New("Linux amd64 siginfo_t layout differs from the wait-status decoder")
	}
	bytes := (*[128]byte)(unsafe.Pointer(info))
	return int32(binary.LittleEndian.Uint32(bytes[24:28])), nil
}

func (helper *linuxOwnerHelperV2) handleWait(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	allowed := helper.released || helper.stopRequested || helper.stopAttempted
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	helper.stateMu.Unlock()
	if !allowed || gate == nil {
		return helper.writeResponse(connection, "wait", "rejected", nil, nil, nil, nil, nil, 0, nil, "gate_not_released")
	}
	result, err := helper.waitGateForConnection(connection)
	if err != nil {
		return helper.writeResponse(connection, "wait", "unresolved", nil, gate, nil, nil, nil, 0, nil, "gate_wait_unproven")
	}
	response := helper.baseResponse("wait", "waited")
	response.Gate = gate
	response.ExitResult = &result
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return err
	}
	return writeLinuxOwnerHelperV2Response(connection, response)
}

func isGateNeverProduced(err error) bool {
	var failure *linuxgate.GateNeverProducedError
	return errors.As(err, &failure)
}

func cloneLinuxOwnerHelperV2StopEvidence(value LinuxOwnerHelperV2StopEvidence) LinuxOwnerHelperV2StopEvidence {
	cloned := value
	if value.AnchorStop != nil {
		stop := value.AnchorStop.Clone()
		cloned.AnchorStop = &stop
	}
	cloned.RunReapEvidence = append([]LinuxOwnerAnchorReapEvidence(nil), value.RunReapEvidence...)
	for index := range cloned.RunReapEvidence {
		cloned.RunReapEvidence[index].HelperExit = value.RunReapEvidence[index].HelperExit.Clone()
		cloned.RunReapEvidence[index].ProcessExit = value.RunReapEvidence[index].ProcessExit.Clone()
	}
	if value.PreGate != nil {
		preGate := *value.PreGate
		if value.PreGate.AnchorStop != nil {
			stop := value.PreGate.AnchorStop.Clone()
			preGate.AnchorStop = &stop
		}
		preGate.Launcher = cloneLinuxOwnerProcessIdentity(value.PreGate.Launcher)
		preGate.LauncherExit = cloneLinuxOwnerAnchorExitProof(value.PreGate.LauncherExit)
		if value.PreGate.GateOutcome != nil {
			gateOutcome := *value.PreGate.GateOutcome
			preGate.GateOutcome = &gateOutcome
		}
		if value.PreGate.Proof != nil {
			proof := *value.PreGate.Proof
			proof.HelperExit = cloneLinuxOwnerAnchorExitProof(value.PreGate.Proof.HelperExit)
			preGate.Proof = &proof
		}
		cloned.PreGate = &preGate
	}
	cloned.Proof.LauncherExit = *cloneLinuxOwnerAnchorExitProof(&value.Proof.LauncherExit)
	cloned.Proof.GateExit = *cloneLinuxOwnerAnchorExitProof(&value.Proof.GateExit)
	cloned.Proof.HelperExit = cloneLinuxOwnerAnchorExitProof(value.Proof.HelperExit)
	return cloned
}

func (helper *linuxOwnerHelperV2) closeAfterClear() error {
	if helper == nil {
		return nil
	}
	helper.stateMu.Lock()
	prepared := helper.prepared
	helper.prepared = nil
	ownerPipe := helper.ownerPipe
	helper.ownerPipe = nil
	leaf := helper.leaf
	helper.leaf = nil
	parent := helper.parent
	helper.parent = nil
	helper.stateMu.Unlock()
	var result error
	if prepared != nil {
		if err := prepared.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	if ownerPipe != nil {
		if err := ownerPipe.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	if leaf != nil {
		if err := leaf.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	if parent != nil {
		if err := parent.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func closeLinuxOwnerHelperFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}
