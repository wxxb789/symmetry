//go:build linux && amd64

package platform

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

const (
	linuxOwnerAnchorProtocolVersion               = 1
	linuxOwnerAnchorReservationVersion            = 2
	linuxOwnerAnchorMaxFrame                      = 128 << 10
	linuxOwnerAnchorMaxSocketPath                 = 108
	linuxOwnerAnchorRequestTimeout                = 5 * time.Second
	linuxOwnerAnchorWaitTimeout                   = 30 * time.Second
	linuxOwnerAnchorCleanupTimeout                = 6 * time.Minute
	linuxOwnerAnchorWaitPoll                      = 25 * time.Millisecond
	linuxOwnerAnchorSupervisionRetry              = time.Second
	linuxOwnerAnchorReapPageSize                  = 4
	linuxOwnerAnchorOperationStart                = "start-helper-v2"
	linuxOwnerAnchorOperationAttach               = "attach-helper-v2"
	linuxOwnerAnchorOperationLookup               = "lookup-helper-v2"
	linuxOwnerAnchorOperationRetire               = "retire-helper-v2"
	linuxOwnerAnchorOperationReapRun              = "wait-reap-run-v2"
	linuxOwnerAnchorOperationWaitHelper           = "wait-helper-v2"
	linuxOwnerAnchorOperationRetireCleared        = "retire-cleared-run-v2"
	linuxOwnerAnchorOperationRetireAborted        = "retire-aborted-helper-v2"
	linuxOwnerAnchorOperationCloneCleanup         = "remove-b-clone-after-receipt"
	linuxOwnerAnchorOperationRegisterGate         = "register-gate-v2"
	linuxOwnerAnchorOperationAuthorizeGate        = "authorize-gate-release-v2"
	linuxOwnerAnchorOperationStopLeaf             = "stop-leaf-v2"
	linuxOwnerAnchorOperationClassifyChild        = "classify-run-child-origin-v2"
	linuxOwnerAnchorOperationIdentity             = "identity"
	linuxOwnerAnchorOperationRegisterProcess      = "register-process"
	linuxOwnerAnchorOperationWaitReap             = "wait-reap"
	linuxOwnerAnchorStopEvidenceVersion           = 1
	linuxOwnerAnchorRunChildClassificationVersion = 1
	linuxOwnerAnchorMaxUserNamespaceDepth         = 32
)

// DefaultLinuxOwnerAnchorSocketPath is the systemd-provisioned A-private
// endpoint used by the daemon preflight and the per-run v2 helper.
const DefaultLinuxOwnerAnchorSocketPath = "/run/symmetry-owner/owner.sock"

const linuxOwnerAnchorGateImagePath = "/usr/libexec/symmetry/symmetry-linux-gate"

const linuxOwnerAnchorWitnessConfigPath = "/etc/symmetry/linux-owner-witness.json"

const linuxOwnerAnchorWitnessConfigVersion = 1

const linuxOwnerAnchorWitnessConfigMaxBytes = 4096

var ErrLinuxOwnerAnchorHelperAlreadyStarted = errors.New("Linux cgroup-v2 owner helper already exists; attach to its exact identity instead of starting another")

const (
	linuxOwnerAnchorRunChildOriginRegisteredCurrent          linuxOwnerAnchorRunChildOrigin = "registered_current"
	linuxOwnerAnchorRunChildOriginGateUserNSCurrent          linuxOwnerAnchorRunChildOrigin = "gate_userns_current"
	linuxOwnerAnchorRunChildOriginRegisteredForeign          linuxOwnerAnchorRunChildOrigin = "registered_foreign"
	linuxOwnerAnchorRunChildOriginOtherUserNSForeign         linuxOwnerAnchorRunChildOrigin = "other_userns_foreign"
	linuxOwnerAnchorRunChildOriginCurrentRunEscaped          linuxOwnerAnchorRunChildOrigin = "current_run_escaped"
	linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly linuxOwnerAnchorRunChildOrigin = "pre_release_gate_physical_only"
	linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign        linuxOwnerAnchorRunChildOrigin = "physical_only_foreign"
	linuxOwnerAnchorRunChildOriginUnknown                    linuxOwnerAnchorRunChildOrigin = "unknown"
)

var errLinuxOwnerAnchorCleanupAuthority = errors.New("Linux owner anchor State cleanup authority is unproven")

func (fence LinuxOwnerAnchorProducerFence) ValidateFor(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) error {
	if grant.Validate() != nil || helper.Validate() != nil || fence.Version != linuxOwnerAnchorStopEvidenceVersion ||
		fence.RunKey != grant.RunKey || fence.Fence != grant.Fence || fence.LaunchToken != grant.LaunchToken ||
		!fence.Helper.Equal(helper) || !fence.StopLatched || !fence.LeaseStopped || !fence.PrepareQuiesced ||
		!fence.ReleaseQuiesced || !fence.LeafMembershipVerified || fence.Released && !fence.ReleaseAttempted {
		return errors.New("Linux owner anchor producer fence differs from the exact stop-latched run")
	}
	if !fence.PrepareAttempted {
		if fence.Launcher != nil || fence.LauncherExit != nil || fence.Gate != nil || fence.UserNamespace != nil ||
			fence.UntrustedProcess != nil || fence.GateOutcome != nil ||
			fence.ReleaseAttempted || fence.Released {
			return errors.New("Linux owner anchor stop fence invents a producer for an unprepared run")
		}
		return nil
	}
	if fence.Launcher == nil || fence.Launcher.Validate() != nil || fence.LauncherExit == nil || fence.LauncherExit.Validate() != nil ||
		!fence.LauncherExit.ValidForOwnedDescendant(*fence.Launcher, helper, grant.Anchor, nil) {
		return errors.New("Linux owner anchor stop fence lacks exact launcher exit/reap evidence")
	}
	if fence.Gate == nil {
		if fence.UserNamespace != nil || fence.ReleaseAttempted || fence.Released {
			return errors.New("Linux owner anchor pre-gate stop fence carries release or user-namespace state")
		}
		if fence.GateOutcome != nil {
			if fence.UntrustedProcess != nil || fence.GateOutcome.ValidateFor(grant.LaunchToken, *fence.Launcher) != nil ||
				linuxgate.VerifyGateNeverProducedAuthenticator(grant.Secret, *fence.GateOutcome) != nil {
				return errors.New("Linux owner anchor no-gate stop fence lacks the exact authenticated launcher outcome")
			}
			return nil
		}
		if fence.UntrustedProcess == nil || fence.UntrustedProcess.Validate() != nil ||
			fence.UntrustedProcess.Equal(*fence.Launcher) || fence.UntrustedProcess.Equal(helper) || fence.UntrustedProcess.Equal(grant.Anchor) {
			return errors.New("Linux owner anchor failed-Prepare fence lacks its exact untrusted child identity")
		}
		return nil
	}
	if fence.UntrustedProcess != nil || fence.GateOutcome != nil || fence.Gate.Validate() != nil || fence.Gate.HostUID != grant.OwnerUID ||
		!fence.Gate.Cgroup.Equal(grant.Leaf) || fence.UserNamespace == nil || !fence.UserNamespace.valid() {
		return errors.New("Linux owner anchor stop fence gate or user namespace differs from the exact reservation")
	}
	return nil
}

func (fence LinuxOwnerAnchorProducerFence) Clone() LinuxOwnerAnchorProducerFence {
	cloned := fence
	cloned.Launcher = cloneLinuxOwnerProcessIdentity(fence.Launcher)
	cloned.LauncherExit = cloneLinuxOwnerAnchorExitProof(fence.LauncherExit)
	cloned.Gate = cloneLinuxOwnerGateIdentity(fence.Gate)
	cloned.UntrustedProcess = cloneLinuxOwnerProcessIdentity(fence.UntrustedProcess)
	if fence.UserNamespace != nil {
		copy := *fence.UserNamespace
		cloned.UserNamespace = &copy
	}
	if fence.GateOutcome != nil {
		copy := *fence.GateOutcome
		cloned.GateOutcome = &copy
	}
	return cloned
}

func (fence LinuxOwnerAnchorProducerFence) Equal(other LinuxOwnerAnchorProducerFence) bool {
	return fence.Version == other.Version && fence.RunKey == other.RunKey && fence.Fence == other.Fence &&
		fence.LaunchToken == other.LaunchToken && fence.Helper.Equal(other.Helper) && fence.StopLatched == other.StopLatched &&
		fence.LeaseStopped == other.LeaseStopped && fence.PrepareAttempted == other.PrepareAttempted &&
		fence.PrepareQuiesced == other.PrepareQuiesced && fence.ReleaseAttempted == other.ReleaseAttempted &&
		fence.ReleaseQuiesced == other.ReleaseQuiesced && fence.Released == other.Released &&
		fence.LeafMembershipVerified == other.LeafMembershipVerified &&
		equalLinuxOwnerAnchorProcessPointer(fence.Launcher, other.Launcher) &&
		equalLinuxOwnerAnchorExitPointer(fence.LauncherExit, other.LauncherExit) &&
		equalLinuxOwnerAnchorGatePointer(fence.Gate, other.Gate) &&
		equalLinuxOwnerAnchorUserNamespacePointer(fence.UserNamespace, other.UserNamespace) &&
		equalLinuxOwnerAnchorProcessPointer(fence.UntrustedProcess, other.UntrustedProcess) &&
		equalLinuxOwnerAnchorGateOutcomePointer(fence.GateOutcome, other.GateOutcome)
}

// ValidateFor treats processes as the complete set the anchor waited and reaped and never turns
// ECHILD or a helper-owned wait into reap evidence; Anchors pass the nsfs pin (omitting it checks wire shape only).
func (evidence LinuxOwnerAnchorStopEvidence) ValidateFor(
	grant LinuxOwnerAnchorGrant,
	helper, anchor authority.LinuxCgroupProcessIdentity,
	processes []LinuxOwnerAnchorReapEvidence,
	requireReaped bool,
	retainedGateNamespaceFD ...*os.File,
) error {
	if len(retainedGateNamespaceFD) > 1 {
		return errors.New("Linux owner anchor stop evidence has multiple retained gate namespace pins")
	}
	if grant.Validate() != nil || helper.Validate() != nil || anchor.Validate() != nil || evidence.Version != linuxOwnerAnchorStopEvidenceVersion ||
		evidence.RunKey != grant.RunKey || evidence.Fence != grant.Fence || evidence.LaunchToken != grant.LaunchToken ||
		!evidence.Parent.Equal(grant.Parent) || !evidence.Leaf.Equal(grant.Leaf) || !evidence.Anchor.Equal(anchor) ||
		!evidence.Helper.Equal(helper) || !evidence.PinnedLeafFD || !evidence.KillAttempted || !evidence.KillAcknowledged ||
		!evidence.CgroupPopulatedZero || !evidence.NoMigrationObserved || !evidence.ProducerFenced {
		return errors.New("Linux owner anchor stop evidence is not bound to the exact run and acknowledged physical kill")
	}
	if evidence.ProducerFence != nil {
		if evidence.ProducerFence.ValidateFor(grant, helper) != nil ||
			!equalLinuxOwnerAnchorProcessPointer(evidence.Launcher, evidence.ProducerFence.Launcher) ||
			!equalLinuxOwnerAnchorGatePointer(evidence.Gate, evidence.ProducerFence.Gate) ||
			!equalLinuxOwnerAnchorUserNamespacePointer(evidence.UserNamespace, evidence.ProducerFence.UserNamespace) ||
			!equalLinuxOwnerAnchorProcessPointer(evidence.UntrustedProcess, evidence.ProducerFence.UntrustedProcess) ||
			!equalLinuxOwnerAnchorGateOutcomePointer(evidence.GateOutcome, evidence.ProducerFence.GateOutcome) ||
			evidence.ProducerFence.Released && !evidence.GateReleaseAuthorized ||
			evidence.GateReleaseAuthorized && !evidence.ProducerFence.ReleaseAttempted {
			return errors.New("Linux owner anchor stop evidence differs from the helper's exact producer fence")
		}
	}
	if evidence.HelperExit != nil && !evidence.HelperExit.ValidFor(helper, anchor) {
		return errors.New("Linux owner anchor stop evidence helper exit differs from the exact anchor wait")
	}
	if evidence.ProducerFence == nil && evidence.HelperExit == nil {
		return errors.New("Linux owner anchor stop evidence has neither a live-helper fence nor an exact helper exit")
	}
	if evidence.Launcher != nil && evidence.Launcher.Validate() != nil ||
		evidence.Gate != nil && (evidence.Gate.Validate() != nil || evidence.Gate.HostUID != grant.OwnerUID || !evidence.Gate.Cgroup.Equal(grant.Leaf)) ||
		evidence.UserNamespace != nil && !evidence.UserNamespace.valid() ||
		evidence.UntrustedProcess != nil && evidence.UntrustedProcess.Validate() != nil ||
		evidence.Gate != nil && evidence.UserNamespace == nil ||
		evidence.Gate == nil && evidence.UserNamespace != nil || evidence.Gate != nil && evidence.UntrustedProcess != nil ||
		evidence.GateReleaseAuthorized && (evidence.Gate == nil || evidence.UserNamespace == nil) {
		return errors.New("Linux owner anchor stop evidence contains an invalid producer identity")
	}
	if len(retainedGateNamespaceFD) == 1 {
		if err := verifyLinuxOwnerAnchorStopEvidenceNamespacePin(evidence, retainedGateNamespaceFD[0]); err != nil {
			return err
		}
	}
	if evidence.GateOutcome != nil && (evidence.Launcher == nil ||
		evidence.GateOutcome.ValidateFor(grant.LaunchToken, *evidence.Launcher) != nil ||
		linuxgate.VerifyGateNeverProducedAuthenticator(grant.Secret, *evidence.GateOutcome) != nil) {
		return errors.New("Linux owner anchor stop evidence contains an unauthenticated no-gate result")
	}
	if requireReaped {
		if evidence.HelperExit == nil || !evidence.DescendantsReaped || evidence.ReapCount != uint64(len(processes)) {
			return errors.New("Linux owner anchor run-reap stop evidence lacks exact helper and descendant waits")
		}
		if err := validateLinuxOwnerAnchorReapSet(grant, helper, anchor, *evidence.HelperExit, processes, evidence.ReapDigest); err != nil {
			return err
		}
		for _, process := range processes {
			switch process.ProcessRole {
			case "gate":
				if evidence.Gate == nil || !evidence.Gate.Process.Equal(process.Process) {
					return errors.New("anchor gate reap differs from the exact registered gate identity")
				}
			case "native":
				if !evidence.GateReleaseAuthorized || evidence.Gate == nil || !evidence.Gate.Process.Equal(process.Process) {
					return errors.New("anchor native reap lacks exact registered gate and release authorization")
				}
			}
		}
		if evidence.ProducerFence != nil && evidence.ProducerFence.UntrustedProcess != nil {
			found := false
			for _, process := range processes {
				if process.ProcessRole == "descendant" && process.Process.Equal(*evidence.ProducerFence.UntrustedProcess) {
					found = true
					break
				}
			}
			if !found {
				return errors.New("anchor run-reap set omits the exact failed-Prepare child")
			}
		}
	} else if evidence.DescendantsReaped || evidence.ReapCount != 0 || evidence.ReapDigest != "" {
		return errors.New("live-helper StopLeaf response invents anchor child-reap evidence")
	}
	return nil
}

func (evidence LinuxOwnerAnchorStopEvidence) Clone() LinuxOwnerAnchorStopEvidence {
	cloned := evidence
	if evidence.ProducerFence != nil {
		fence := evidence.ProducerFence.Clone()
		cloned.ProducerFence = &fence
	}
	cloned.HelperExit = cloneLinuxOwnerAnchorExitProof(evidence.HelperExit)
	cloned.Launcher = cloneLinuxOwnerProcessIdentity(evidence.Launcher)
	cloned.Gate = cloneLinuxOwnerGateIdentity(evidence.Gate)
	cloned.UntrustedProcess = cloneLinuxOwnerProcessIdentity(evidence.UntrustedProcess)
	if evidence.UserNamespace != nil {
		copy := *evidence.UserNamespace
		cloned.UserNamespace = &copy
	}
	if evidence.GateOutcome != nil {
		copy := *evidence.GateOutcome
		cloned.GateOutcome = &copy
	}
	return cloned
}

func (evidence LinuxOwnerAnchorStopEvidence) Equal(other LinuxOwnerAnchorStopEvidence) bool {
	return evidence.Version == other.Version && evidence.RunKey == other.RunKey && evidence.Fence == other.Fence &&
		evidence.LaunchToken == other.LaunchToken && evidence.Parent.Equal(other.Parent) && evidence.Leaf.Equal(other.Leaf) &&
		evidence.Anchor.Equal(other.Anchor) && evidence.Helper.Equal(other.Helper) &&
		equalLinuxOwnerAnchorProducerFencePointer(evidence.ProducerFence, other.ProducerFence) &&
		equalLinuxOwnerAnchorExitPointer(evidence.HelperExit, other.HelperExit) &&
		equalLinuxOwnerAnchorProcessPointer(evidence.Launcher, other.Launcher) &&
		equalLinuxOwnerAnchorGatePointer(evidence.Gate, other.Gate) &&
		equalLinuxOwnerAnchorUserNamespacePointer(evidence.UserNamespace, other.UserNamespace) &&
		equalLinuxOwnerAnchorProcessPointer(evidence.UntrustedProcess, other.UntrustedProcess) &&
		equalLinuxOwnerAnchorGateOutcomePointer(evidence.GateOutcome, other.GateOutcome) &&
		evidence.GateReleaseAuthorized == other.GateReleaseAuthorized &&
		evidence.PinnedLeafFD == other.PinnedLeafFD && evidence.KillAttempted == other.KillAttempted &&
		evidence.KillAcknowledged == other.KillAcknowledged && evidence.CgroupPopulatedZero == other.CgroupPopulatedZero &&
		evidence.NoMigrationObserved == other.NoMigrationObserved && evidence.ProducerFenced == other.ProducerFenced &&
		evidence.DescendantsReaped == other.DescendantsReaped && evidence.ReapCount == other.ReapCount &&
		evidence.ReapDigest == other.ReapDigest
}

func equalLinuxOwnerAnchorProducerFencePointer(left, right *LinuxOwnerAnchorProducerFence) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalLinuxOwnerAnchorProcessPointer(left, right *authority.LinuxCgroupProcessIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalLinuxOwnerAnchorExitPointer(left, right *authority.LinuxCgroupExitProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalLinuxOwnerAnchorGatePointer(left, right *authority.LinuxCgroupGateIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalLinuxOwnerAnchorUserNamespacePointer(left, right *LinuxUserNamespaceIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalLinuxOwnerAnchorGateOutcomePointer(
	left, right *authority.LinuxCgroupGateProductionResult,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func (grant LinuxOwnerAnchorGrant) Validate() error {
	if grant.ReservationVersion != linuxOwnerAnchorReservationVersion || !grant.CreateStarted ||
		!validLinuxOwnerAnchorRunID(grant.RunKey.RunID) || grant.RunKey.Generation <= 0 ||
		grant.Fence.Generation != grant.RunKey.Generation || !validLinuxOwnerAnchorField(grant.Fence.RuntimeID, 256) ||
		grant.Fence.RuntimeEpoch <= 0 || !validLinuxOwnerAnchorField(grant.Fence.ClaimID, 256) ||
		!validLinuxOwnerAnchorField(grant.Fence.LeaseToken, 8192) ||
		!linuxSupervisorValidHexV2(grant.LaunchToken, authority.TokenBytes*2) ||
		!linuxSupervisorValidHexV2(grant.Secret, authority.SecretBytes*2) ||
		!linuxSupervisorValidHexV2(grant.PipeToken, authority.TokenBytes*2) ||
		!linuxSupervisorValidHexV2(grant.JobID, authority.TokenBytes*2) ||
		grant.OwnerUID == 0 || grant.LauncherUID != 0 ||
		!canonicalAbsoluteLinuxPath(grant.RecoveryEndpoint) || len(grant.RecoveryEndpoint) >= linuxSupervisorEndpointMax ||
		grant.Anchor.Validate() != nil || grant.Parent.Validate() != nil || grant.Leaf.Validate() != nil ||
		!grant.Leaf.Parent.Equal(grant.Parent) {
		return errors.New("Linux owner anchor grant is incomplete or inconsistent")
	}
	return nil
}

func (grant LinuxOwnerAnchorGrant) Equal(other LinuxOwnerAnchorGrant) bool {
	return grant.ReservationVersion == other.ReservationVersion && grant.RunKey == other.RunKey && grant.Fence == other.Fence &&
		grant.LaunchToken == other.LaunchToken && grant.Secret == other.Secret && grant.PipeToken == other.PipeToken &&
		grant.JobID == other.JobID && grant.OwnerUID == other.OwnerUID && grant.LauncherUID == other.LauncherUID &&
		grant.RecoveryEndpoint == other.RecoveryEndpoint && grant.CreateStarted == other.CreateStarted &&
		grant.Anchor.Equal(other.Anchor) && grant.Parent.Equal(other.Parent) && grant.Leaf.Equal(other.Leaf)
}

func (evidence LinuxOwnerAnchorReapEvidence) ValidateFor(grant LinuxOwnerAnchorGrant, helper, process authority.LinuxCgroupProcessIdentity, role string) error {
	if grant.Validate() != nil || helper.Validate() != nil || process.Validate() != nil || evidence.Version != linuxOwnerAnchorProtocolVersion ||
		evidence.RunKey != grant.RunKey || evidence.Fence != grant.Fence || evidence.LaunchToken != grant.LaunchToken ||
		!evidence.Parent.Equal(grant.Parent) || !evidence.Leaf.Equal(grant.Leaf) || !evidence.Helper.Equal(helper) ||
		!evidence.Process.Equal(process) || evidence.ProcessRole != role || !linuxOwnerAnchorValidChildRole(role) ||
		evidence.HelperExit.Validate() != nil || evidence.ProcessExit.Validate() != nil ||
		!evidence.HelperExit.ValidFor(helper, grant.Anchor) ||
		!evidence.ProcessExit.ValidForOwnedDescendant(process, helper, grant.Anchor, &evidence.HelperExit) {
		return errors.New("Linux owner anchor wait/reap evidence is not bound to the exact RunKey, fence, helper, and anchor")
	}
	return nil
}

func canonicalLinuxOwnerAnchorMap(value string) string {
	fields := strings.Fields(value)
	if len(fields) != 3 {
		return ""
	}
	for _, field := range fields {
		parsed, err := strconv.ParseUint(field, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != field {
			return ""
		}
	}
	return strings.Join(fields, " ")
}

func validLinuxOwnerAnchorRunID(value string) bool {
	return validLinuxOwnerAnchorField(value, 256)
}

func validLinuxOwnerAnchorField(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char == 0x7f {
			return false
		}
	}
	return true
}

func linuxOwnerAnchorValidChildRole(role string) bool {
	return role == "launcher" || role == "gate" || role == "native" || role == "descendant"
}
