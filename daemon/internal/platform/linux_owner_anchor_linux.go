//go:build linux && amd64

package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
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

type linuxOwnerAnchorWitnessConfig struct {
	Version            int    `json:"version"`
	RunID              string `json:"run_id"`
	Generation         int64  `json:"generation"`
	DropResponse       string `json:"drop_response,omitempty"`
	HelperAction       string `json:"helper_action,omitempty"`
	HoldForReleaseFile bool   `json:"hold_for_release_file,omitempty"`
}

var ErrLinuxOwnerAnchorHelperAlreadyStarted = errors.New("Linux cgroup-v2 owner helper already exists; attach to its exact identity instead of starting another")

// LinuxOwnerAnchorRunKey duplicates only the platform-neutral durable RunKey
// shape. The platform package cannot import state because state consumes this
// package's cgroup release capability.
type LinuxOwnerAnchorRunKey struct {
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
}

// linuxOwnerAnchorRunChildOrigin is an Anchor-RPC-only process attribution
// result. It is never a State receipt and does not assert a creator edge for
// an adopted process whose original parent was not observed.
type linuxOwnerAnchorRunChildOrigin string

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

// linuxOwnerAnchorRunChildClassification binds an origin result to both the
// requested RunKey and, when known, the RunKey whose gate user namespace
// proves the process origin. This type travels only on the authenticated
// Anchor control socket; it is not persisted in the RunJournal.
type linuxOwnerAnchorRunChildClassification struct {
	Version      int                                  `json:"version"`
	RunKey       LinuxOwnerAnchorRunKey               `json:"run_key"`
	OriginRunKey LinuxOwnerAnchorRunKey               `json:"origin_run_key,omitempty"`
	Fence        protocol.Fence                       `json:"fence"`
	LaunchToken  string                               `json:"launch_token"`
	Helper       authority.LinuxCgroupProcessIdentity `json:"helper"`
	Process      authority.LinuxCgroupProcessIdentity `json:"process"`
	Role         string                               `json:"role,omitempty"`
	Origin       linuxOwnerAnchorRunChildOrigin       `json:"origin"`
}

func (classification linuxOwnerAnchorRunChildClassification) ValidateFor(
	grant LinuxOwnerAnchorGrant,
	helper, process authority.LinuxCgroupProcessIdentity,
) error {
	if grant.Validate() != nil || helper.Validate() != nil || process.Validate() != nil ||
		classification.Version != linuxOwnerAnchorRunChildClassificationVersion ||
		classification.RunKey != grant.RunKey || classification.Fence != grant.Fence ||
		classification.LaunchToken != grant.LaunchToken || !classification.Helper.Equal(helper) ||
		!classification.Process.Equal(process) {
		return errors.New("Linux owner anchor child classification differs from its exact RunKey and process")
	}
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		if classification.OriginRunKey != grant.RunKey || !linuxOwnerAnchorValidChildRole(classification.Role) {
			return errors.New("registered-current child classification lacks its exact origin RunKey or role")
		}
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if classification.OriginRunKey != grant.RunKey || classification.Role != "descendant" {
			return errors.New("gate-userns child classification is not an exact current-run descendant")
		}
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign:
		if !validLinuxOwnerAnchorRunID(classification.OriginRunKey.RunID) || classification.OriginRunKey.Generation <= 0 ||
			classification.OriginRunKey == grant.RunKey || classification.Role != "" && !linuxOwnerAnchorValidChildRole(classification.Role) {
			return errors.New("foreign child classification lacks a distinct exact origin RunKey")
		}
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		if classification.OriginRunKey != grant.RunKey || !linuxOwnerAnchorValidChildRole(classification.Role) {
			return errors.New("escaped child classification lacks the current RunKey or process role")
		}
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly, linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if classification.OriginRunKey != (LinuxOwnerAnchorRunKey{}) || classification.Role != "pre_release_gate" {
			return errors.New("physical-only gate classification invents a durable RunKey gate identity")
		}
	case linuxOwnerAnchorRunChildOriginUnknown:
		if classification.OriginRunKey != (LinuxOwnerAnchorRunKey{}) ||
			classification.Role != "" && !linuxOwnerAnchorValidChildRole(classification.Role) {
			return errors.New("unknown child classification carries an unproven origin RunKey")
		}
	default:
		return errors.New("Linux owner anchor child classification has an unknown origin kind")
	}
	return nil
}

func (classification linuxOwnerAnchorRunChildClassification) currentRunMember() bool {
	return classification.Origin == linuxOwnerAnchorRunChildOriginRegisteredCurrent ||
		classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent
}

func linuxOwnerAnchorClassifyNamespaceMembership(
	targetRun, originRun LinuxOwnerAnchorRunKey,
	cgroupPath, targetLeaf string,
) linuxOwnerAnchorRunChildOrigin {
	if !validLinuxOwnerAnchorRunID(targetRun.RunID) || targetRun.Generation <= 0 ||
		!validLinuxOwnerAnchorRunID(originRun.RunID) || originRun.Generation <= 0 || cgroupPath == "" || targetLeaf == "" {
		return linuxOwnerAnchorRunChildOriginUnknown
	}
	if originRun != targetRun {
		return linuxOwnerAnchorRunChildOriginOtherUserNSForeign
	}
	if linuxOwnerCgroupPathWithin(cgroupPath, targetLeaf) {
		return linuxOwnerAnchorRunChildOriginGateUserNSCurrent
	}
	return linuxOwnerAnchorRunChildOriginCurrentRunEscaped
}

func (classification linuxOwnerAnchorRunChildClassification) Equal(other linuxOwnerAnchorRunChildClassification) bool {
	return classification.Version == other.Version && classification.RunKey == other.RunKey &&
		classification.OriginRunKey == other.OriginRunKey && classification.Fence == other.Fence &&
		classification.LaunchToken == other.LaunchToken && classification.Helper.Equal(other.Helper) &&
		classification.Process.Equal(other.Process) && classification.Role == other.Role && classification.Origin == other.Origin
}

// LinuxOwnerAnchorCleanupVerifier is the read-only State seam used immediately
// before the anchor delegates an exact clone cleanup to the root worker. The
// implementation is supplied by cmd/symmetry-daemon to avoid a platform→State
// import cycle; Close releases its pinned StateDir/runs descriptors.
type LinuxOwnerAnchorCleanupVerifier interface {
	Verify(runID string, generation int64, grant authority.LinuxCloneGrant, intent authority.LinuxCloneCleanupIntent, prior *authority.LinuxCloneTargetRemovalProof, targetFDPresent bool) error
	Close() error
}

// LinuxOwnerAnchorCleanupVerifierFactory opens the State-owned read-only
// verifier from the root-installed StateDir. RunLinuxOwnerAnchor requires the
// factory; v2 config pins its reader before READY, while strict legacy v1 config
// has no StateDir and therefore keeps clone cleanup disabled.
type LinuxOwnerAnchorCleanupVerifierFactory func(stateDir string) (LinuxOwnerAnchorCleanupVerifier, error)

type linuxOwnerAnchorCloneCleanupWorker func(
	ctx context.Context,
	rootFD, targetFD int,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
) (authority.LinuxCloneTargetRemovalProof, error)

var errLinuxOwnerAnchorCleanupAuthority = errors.New("Linux owner anchor State cleanup authority is unproven")

// LinuxOwnerAnchorGrant is the durable, run-bound input to the anchor. The
// caller obtains it from the existing RunJournal after the cgroup leaf is
// durably bound. Secret, PipeToken, JobID and Fence.LeaseToken are local
// control material: they travel only over this private socket and must never
// be logged or placed in argv/environment. The anchor keeps no journal copy
// and makes no scheduling or lease decisions.
type LinuxOwnerAnchorGrant struct {
	ReservationVersion int                                  `json:"reservation_version"`
	RunKey             LinuxOwnerAnchorRunKey               `json:"run_key"`
	Fence              protocol.Fence                       `json:"fence"`
	LaunchToken        string                               `json:"launch_token"`
	Secret             string                               `json:"secret"`
	PipeToken          string                               `json:"pipe_token"`
	JobID              string                               `json:"job_id"`
	OwnerUID           uint32                               `json:"owner_uid"`
	LauncherUID        uint32                               `json:"launcher_uid"`
	RecoveryEndpoint   string                               `json:"recovery_endpoint"`
	CreateStarted      bool                                 `json:"create_started"`
	Anchor             authority.LinuxCgroupProcessIdentity `json:"anchor"`
	Parent             authority.LinuxCgroupParentIdentity  `json:"parent"`
	Leaf               authority.LinuxCgroupIdentity        `json:"leaf"`
}

// LinuxOwnerAnchorReapEvidence proves one exact wait/reap. It is not a
// physical-stop receipt: it says nothing about other members of the cgroup
// subtree and cannot authorize leaf release or journal clearing.
type LinuxOwnerAnchorReapEvidence struct {
	Version     int                                  `json:"version"`
	RunKey      LinuxOwnerAnchorRunKey               `json:"run_key"`
	Fence       protocol.Fence                       `json:"fence"`
	LaunchToken string                               `json:"launch_token"`
	Parent      authority.LinuxCgroupParentIdentity  `json:"parent"`
	Leaf        authority.LinuxCgroupIdentity        `json:"leaf"`
	Helper      authority.LinuxCgroupProcessIdentity `json:"helper"`
	Process     authority.LinuxCgroupProcessIdentity `json:"process"`
	ProcessRole string                               `json:"process_role"`
	HelperExit  authority.LinuxCgroupExitProof       `json:"helper_exit"`
	ProcessExit authority.LinuxCgroupExitProof       `json:"process_exit"`
}

// LinuxOwnerAnchorProducerFence is the helper's authenticated stop latch.
// It reports only that lease renewal, preparation and release have quiesced;
// it never attests to cgroup.kill or to the subsequent populated=0 observation.
type LinuxOwnerAnchorProducerFence struct {
	Version                int                                        `json:"version"`
	RunKey                 LinuxOwnerAnchorRunKey                     `json:"run_key"`
	Fence                  protocol.Fence                             `json:"fence"`
	LaunchToken            string                                     `json:"launch_token"`
	Helper                 authority.LinuxCgroupProcessIdentity       `json:"helper"`
	StopLatched            bool                                       `json:"stop_latched"`
	LeaseStopped           bool                                       `json:"lease_stopped"`
	PrepareAttempted       bool                                       `json:"prepare_attempted"`
	PrepareQuiesced        bool                                       `json:"prepare_quiesced"`
	ReleaseAttempted       bool                                       `json:"release_attempted"`
	ReleaseQuiesced        bool                                       `json:"release_quiesced"`
	Released               bool                                       `json:"released"`
	LeafMembershipVerified bool                                       `json:"leaf_membership_verified"`
	Launcher               *authority.LinuxCgroupProcessIdentity      `json:"launcher,omitempty"`
	LauncherExit           *authority.LinuxCgroupExitProof            `json:"launcher_exit,omitempty"`
	Gate                   *authority.LinuxCgroupGateIdentity         `json:"gate,omitempty"`
	UserNamespace          *LinuxUserNamespaceIdentity                `json:"user_namespace,omitempty"`
	UntrustedProcess       *authority.LinuxCgroupProcessIdentity      `json:"untrusted_process,omitempty"`
	GateOutcome            *authority.LinuxCgroupGateProductionResult `json:"gate_outcome,omitempty"`
}

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

// LinuxOwnerAnchorStopEvidence is the anchor's ephemeral physical-kill and
// empty-leaf observation. Anchor identifies the cgroup.kill writer; durable
// LinuxCgroupStopProof.Issuer remains the helper or mirror that coordinates
// receipt creation, and Reaper remains the owner of the reported waitid calls.
// This is not a State receipt; callers combine it with the durable reservation
// and exact wait/reap evidence before issuing one.
type LinuxOwnerAnchorStopEvidence struct {
	Version               int                                        `json:"version"`
	RunKey                LinuxOwnerAnchorRunKey                     `json:"run_key"`
	Fence                 protocol.Fence                             `json:"fence"`
	LaunchToken           string                                     `json:"launch_token"`
	Parent                authority.LinuxCgroupParentIdentity        `json:"parent"`
	Leaf                  authority.LinuxCgroupIdentity              `json:"leaf"`
	Anchor                authority.LinuxCgroupProcessIdentity       `json:"anchor"`
	Helper                authority.LinuxCgroupProcessIdentity       `json:"helper"`
	ProducerFence         *LinuxOwnerAnchorProducerFence             `json:"producer_fence,omitempty"`
	HelperExit            *authority.LinuxCgroupExitProof            `json:"helper_exit,omitempty"`
	Launcher              *authority.LinuxCgroupProcessIdentity      `json:"launcher,omitempty"`
	Gate                  *authority.LinuxCgroupGateIdentity         `json:"gate,omitempty"`
	UserNamespace         *LinuxUserNamespaceIdentity                `json:"user_namespace,omitempty"`
	UntrustedProcess      *authority.LinuxCgroupProcessIdentity      `json:"untrusted_process,omitempty"`
	GateOutcome           *authority.LinuxCgroupGateProductionResult `json:"gate_outcome,omitempty"`
	GateReleaseAuthorized bool                                       `json:"gate_release_authorized"`
	PinnedLeafFD          bool                                       `json:"pinned_leaf_fd"`
	KillAttempted         bool                                       `json:"kill_attempted"`
	KillAcknowledged      bool                                       `json:"kill_acknowledged"`
	CgroupPopulatedZero   bool                                       `json:"cgroup_populated_zero"`
	NoMigrationObserved   bool                                       `json:"no_migration_observed"`
	ProducerFenced        bool                                       `json:"producer_fenced"`
	DescendantsReaped     bool                                       `json:"descendants_reaped"`
	ReapCount             uint64                                     `json:"reap_count"`
	ReapDigest            string                                     `json:"reap_digest,omitempty"`
}

// LinuxOwnerAnchorRunReapResult pairs the anchor's physical stop witness with
// its complete, sorted per-process P_PIDFD wait/reap evidence.
type LinuxOwnerAnchorRunReapResult struct {
	Processes []LinuxOwnerAnchorReapEvidence `json:"processes"`
	Stop      LinuxOwnerAnchorStopEvidence   `json:"stop"`
}

// ValidateFor treats processes as the complete set the anchor itself waited
// and reaped. When the Anchor still owns the gate's nsfs pin, callers pass it
// so the serialized UserNamespace is checked against that independent witness.
// Omitting the pin performs wire-shape validation only; Anchor authority paths
// must supply the retained FD, while authenticated clients may validate the
// returned shape after the Anchor has checked its independent witness.
// It never turns ECHILD or a helper-owned prior wait into anchor reap evidence;
// callers may separately combine exact helper wait results.
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

func verifyLinuxOwnerAnchorStopEvidenceNamespacePin(evidence LinuxOwnerAnchorStopEvidence, retainedFD *os.File) error {
	if evidence.Gate == nil {
		if retainedFD != nil {
			return errors.New("pre-gate stop evidence unexpectedly has a retained gate namespace FD")
		}
		return nil
	}
	if retainedFD == nil || evidence.UserNamespace == nil || !evidence.UserNamespace.valid() {
		return errors.New("gate-bound Anchor stop evidence lacks its retained gate namespace pin")
	}
	identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(retainedFD)
	if err != nil || identity != *evidence.UserNamespace {
		return fmt.Errorf("Anchor stop evidence user namespace differs from its retained exact nsfs FD: %w", errOr(err, errors.New("device or inode differs")))
	}
	return nil
}

func validateLinuxOwnerAnchorReapSet(
	grant LinuxOwnerAnchorGrant,
	helper, anchor authority.LinuxCgroupProcessIdentity,
	helperExit authority.LinuxCgroupExitProof,
	processes []LinuxOwnerAnchorReapEvidence,
	digest string,
) error {
	if helperExit.Validate() != nil || !helperExit.ValidFor(helper, anchor) {
		return errors.New("Linux owner anchor reap set lacks exact helper exit/reap evidence")
	}
	for index, process := range processes {
		if !linuxOwnerAnchorValidChildRole(process.ProcessRole) ||
			process.ValidateFor(grant, helper, process.Process, process.ProcessRole) != nil ||
			!process.HelperExit.Equal(helperExit) || index > 0 && processes[index-1].Process.PID >= process.Process.PID {
			return errors.New("Linux owner anchor reap set contains invalid, duplicate, or unordered process evidence")
		}
	}
	encoded, err := json.Marshal(processes)
	if err != nil {
		return fmt.Errorf("encode exact Linux owner anchor reap set: %w", err)
	}
	actual := sha256.Sum256(encoded)
	if len(digest) != sha256.Size*2 || hex.EncodeToString(actual[:]) != digest {
		return errors.New("Linux owner anchor stop evidence digest differs from its exact reap set")
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

// RemoveBCloneAfterReceipt runs the installed root cleanup primitive inside
// this exact anchor process. It transfers only the caller's pinned O_PATH root
// and optional target descriptors, never paths or commands. Workspace validates
// the returned proof again before State records cleanup completion. cleanup
// must be the exact durable Begin-cleanup readback; its NormalReleaseProof
// supplies the anchor identity after ClearSupervisorHandoffV2 has removed the
// reservation Secret. A live target FD requires priorRemoval=nil; targetFD=-1
// is replay-only and requires the exact State-readback Removed proof. Callers
// must hold no State or cgroup lock while this bounded filesystem RPC runs.
func (client *LinuxOwnerAnchorClient) RemoveBCloneAfterReceipt(
	ctx context.Context,
	rootFD, targetFD int,
	cloneGrant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent,
	priorRemoval *authority.LinuxCloneTargetRemovalProof,
) (authority.LinuxCloneTargetRemovalProof, error) {
	if client == nil || ctx == nil || rootFD < 0 || targetFD < -1 {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux clone cleanup anchor request is incomplete")
	}
	if err := validateLinuxOwnerAnchorCloneCleanup(cloneGrant, cleanup, client.owner.AUID); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	if targetFD >= 0 {
		if priorRemoval != nil {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("live target cleanup must not reuse a prior removal proof")
		}
	} else if err := verifyLinuxOwnerAnchorPriorRemoval(rootFD, cleanup, priorRemoval, client.owner); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationCloneCleanup,
		RunKey:      LinuxOwnerAnchorRunKey{RunID: cleanup.Intent.RunID, Generation: cleanup.Intent.Generation},
		FenceDigest: cleanup.FenceDigest, TargetFDPresent: targetFD >= 0,
	}
	cloneGrantCopy := cloneGrant.Clone()
	cleanupCopy := cleanup.Clone()
	request.CloneGrant = &cloneGrantCopy
	request.Cleanup = &cleanupCopy
	if priorRemoval != nil {
		priorCopy := priorRemoval.Clone()
		request.PriorRemovalProof = &priorCopy
	}
	sendFDs := []int{rootFD}
	if targetFD >= 0 {
		sendFDs = append(sendFDs, targetFD)
	}
	response, rights, err := client.exchange(ctx, request, sendFDs...)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	if response.Status != "clone_removed" || response.RunKey != request.RunKey || response.FenceDigest != cleanup.FenceDigest ||
		response.Anchor.PID != cleanup.NormalReleaseProof.AnchorPID || response.Anchor.Identity != cleanup.NormalReleaseProof.AnchorIdentity ||
		response.CloneRemoval == nil || len(rights) != 0 || response.CloneRemoval.Validate() != nil ||
		!response.CloneRemoval.Intent.Equal(cleanup) ||
		targetFD >= 0 && (priorRemoval != nil || response.CloneRemoval.Disposition != authority.LinuxCloneTargetDispositionRemoved) ||
		targetFD < 0 && (priorRemoval == nil || !response.CloneRemoval.Equal(*priorRemoval)) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor returned no exact root clone-removal proof")
	}
	return response.CloneRemoval.Clone(), nil
}

func validateLinuxOwnerAnchorCloneCleanup(
	cloneGrant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent,
	ownerUID uint32,
) error {
	if cloneGrant.Validate() != nil || cleanup.Validate() != nil || ownerUID == 0 ||
		cloneGrant.Intent.RunID != cleanup.Intent.RunID || cloneGrant.Intent.Generation != cleanup.Intent.Generation ||
		!cloneGrant.Intent.Equal(cleanup.Intent) || !cloneGrant.Seed.Equal(cleanup.SeedReceipt) ||
		cleanup.NormalReleaseProof.AnchorPID <= 0 || cleanup.NormalReleaseProof.AnchorIdentity == "" ||
		cleanup.NormalReleaseProof.OwnerUID != ownerUID || cleanup.NormalReleaseProof.LauncherUID != 0 ||
		cleanup.NormalReleaseProof.CgroupIdentity == nil || cleanup.NormalReleaseProof.CgroupIdentity.Validate() != nil {
		return errors.New("Linux clone cleanup authority differs from the exact RunKey or normal-release anchor")
	}
	return nil
}

func verifyLinuxOwnerAnchorPriorRemoval(
	rootFD int,
	cleanup authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
	owner linuxgate.OwnerConfig,
) error {
	if rootFD < 0 || prior == nil || owner.Validate() != nil || cleanup.Validate() != nil ||
		prior.Validate() != nil || prior.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
		!prior.Intent.Equal(cleanup) {
		return errors.New("absent-target cleanup requires the exact previously durable removal proof")
	}
	flags, err := unix.FcntlInt(uintptr(rootFD), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH == 0 {
		return fmt.Errorf("verify pinned B clone root O_PATH descriptor: %w", errOr(err, errors.New("descriptor is not O_PATH")))
	}
	var stat unix.Stat_t
	if err := unix.Fstat(rootFD, &stat); err != nil {
		return fmt.Errorf("stat pinned B clone root: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != owner.BUID || stat.Gid != owner.BGID ||
		uint64(stat.Dev) != prior.RootDevice || prior.RootDevice != cleanup.CheckoutReceipt.TargetDevice || stat.Ino != prior.RootInode ||
		prior.TargetDevice != cleanup.CheckoutReceipt.TargetDevice || prior.TargetInode != cleanup.CheckoutReceipt.TargetInode {
		return errors.New("prior removal proof differs from the pinned B root or checkout inode")
	}
	path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", rootFD))
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path != cleanup.RootPath || strings.Contains(path, " (deleted)") {
		return errors.New("prior removal proof root differs from the pinned canonical B root")
	}
	return nil
}

// LinuxOwnerAnchorHelper owns the exact helper pidfd, a daemon-side pinned
// leaf, and the authenticated per-run control endpoint. OwnerFD is present
// only for the original StartHelper caller; EOF is the helper's owner-loss
// latch. An attached recovery client never gets a replacement owner pipe.
type LinuxOwnerAnchorHelper struct {
	PID        int
	Identity   string
	PIDFD      *os.File
	LeafFD     *os.File
	ParentFD   *os.File
	OwnerFD    *os.File
	Control    *LinuxOwnerHelperV2Client
	HelperExit *authority.LinuxCgroupExitProof
}

// Close retires only the local helper and leaf pins. It neither stops the
// helper nor removes its cgroup nor creates any journal evidence.
func (helper *LinuxOwnerAnchorHelper) Close() error {
	if helper == nil {
		return nil
	}
	var result error
	if helper.OwnerFD != nil {
		result = helper.OwnerFD.Close()
		helper.OwnerFD = nil
	}
	if helper.Control != nil {
		if err := helper.Control.Close(); result == nil {
			result = err
		}
		helper.Control = nil
	}
	if helper.PIDFD != nil {
		if err := helper.PIDFD.Close(); result == nil {
			result = err
		}
		helper.PIDFD = nil
	}
	if helper.LeafFD != nil {
		if err := helper.LeafFD.Close(); result == nil {
			result = err
		}
		helper.LeafFD = nil
	}
	if helper.ParentFD != nil {
		if err := helper.ParentFD.Close(); result == nil {
			result = err
		}
		helper.ParentFD = nil
	}
	return result
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

func makeLinuxOwnerAnchorReapEvidence(grant LinuxOwnerAnchorGrant, helper, process authority.LinuxCgroupProcessIdentity, role string, helperExit, processExit authority.LinuxCgroupExitProof) (LinuxOwnerAnchorReapEvidence, error) {
	evidence := LinuxOwnerAnchorReapEvidence{
		Version: linuxOwnerAnchorProtocolVersion, RunKey: grant.RunKey, Fence: grant.Fence,
		LaunchToken: grant.LaunchToken, Parent: grant.Parent, Leaf: grant.Leaf, Helper: helper, Process: process, ProcessRole: role,
		HelperExit: helperExit, ProcessExit: processExit,
	}
	if err := evidence.ValidateFor(grant, helper, process, role); err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	return evidence, nil
}

func cloneLinuxOwnerAnchorExitProof(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.HelperExit = nil
	return &cloned
}

// LinuxOwnerAnchorClient speaks only the fixed local protocol. It has no API
// for command/argv submission: the anchor starts only a fixed hidden helper
// mode in the existing symmetry-daemon binary.
type LinuxOwnerAnchorClient struct {
	socketPath string
	owner      linuxgate.OwnerConfig
}

// LinuxOwnerAnchorIdentity is an authenticated description of the live owner
// service. Parent is identity data only; Discover does not return a parent FD
// or re-establish pin authority after the caller loses its own live pin.
type LinuxOwnerAnchorIdentity struct {
	Process     authority.LinuxCgroupProcessIdentity
	Parent      authority.LinuxCgroupParentIdentity
	OwnerUID    uint32
	ManagerPath string
}

// NewLinuxOwnerAnchorClient uses the same fixed root-owned A/B configuration
// as the trusted Linux gate. It does not infer service names or trust a caller
// supplied UID mapping.
func NewLinuxOwnerAnchorClient(socketPath string) (*LinuxOwnerAnchorClient, error) {
	if err := validateLinuxOwnerAnchorSocketPath(socketPath); err != nil {
		return nil, err
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return nil, fmt.Errorf("read fixed Linux owner configuration: %w", err)
	}
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID {
		return nil, errors.New("Linux owner anchor client must run with the configured A UID/GID")
	}
	return &LinuxOwnerAnchorClient{socketPath: socketPath, owner: owner}, nil
}

// Discover authenticates the service using SO_PEERCRED and returns the exact
// anchor process and measured manager-parent identity needed to create a
// durable reservation. The caller must still retain its own pinned parent FD.
func (client *LinuxOwnerAnchorClient) Discover(ctx context.Context) (LinuxOwnerAnchorIdentity, error) {
	if client == nil || ctx == nil || ctx.Err() != nil {
		return LinuxOwnerAnchorIdentity{}, errors.New("Linux owner anchor discovery requires a live client and context")
	}
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorRequestTimeout)
	if err != nil {
		return LinuxOwnerAnchorIdentity{}, err
	}
	defer connection.Close()
	defer stopClose()
	peer, err := linuxOwnerAnchorPeerFromConn(connection, client.owner)
	if err != nil {
		return LinuxOwnerAnchorIdentity{}, fmt.Errorf("authenticate Linux owner anchor identity peer: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return LinuxOwnerAnchorIdentity{}, fmt.Errorf("capture daemon identity for owner-anchor discovery: %w", err)
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: "identity",
		ClientPID: os.Getpid(), ClientIdentity: clientIdentity,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return LinuxOwnerAnchorIdentity{}, fmt.Errorf("encode Linux owner anchor identity request: %w", err)
	}
	if written, _, err := connection.WriteMsgUnix(encoded, nil, nil); err != nil || written != len(encoded) {
		return LinuxOwnerAnchorIdentity{}, fmt.Errorf("send Linux owner anchor identity request: %w", errOr(err, io.ErrShortWrite))
	}
	response, rights, err := readLinuxOwnerAnchorResponse(connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerAnchorIdentity{}, err
	}
	if len(rights) != 0 || response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != "identity" || response.Status != "ready" ||
		response.Anchor.PID != peer.PID || response.Anchor.Identity != peer.Identity || response.Parent.Validate() != nil ||
		response.OwnerUID != client.owner.AUID || response.ManagerPath != response.Parent.Path ||
		response.ManagerPath == "/" ||
		response.RunKey != (LinuxOwnerAnchorRunKey{}) || response.Fence != (protocol.Fence{}) || response.FenceDigest != "" || response.LaunchToken != "" ||
		response.Helper != nil || response.Evidence != nil || len(response.ReapEvidence) != 0 || response.HelperExit != nil ||
		response.NextPID != 0 || response.More || response.CloneRemoval != nil ||
		linuxOwnerCgroupPathsOverlap(peer.CgroupPath, response.ManagerPath) {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerAnchorIdentity{}, errors.New("Linux owner anchor identity response differs from exact peer credentials or fixed configuration")
	}
	return LinuxOwnerAnchorIdentity{
		Process: response.Anchor, Parent: response.Parent, OwnerUID: response.OwnerUID, ManagerPath: response.ManagerPath,
	}, nil
}

func (client *LinuxOwnerAnchorClient) dial(ctx context.Context, timeout time.Duration) (*net.UnixConn, func() bool, error) {
	dialer := &net.Dialer{Timeout: timeout}
	connected, err := dialer.DialContext(ctx, "unixpacket", client.socketPath)
	if err != nil {
		return nil, nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	connection, ok := connected.(*net.UnixConn)
	if !ok {
		_ = connected.Close()
		return nil, nil, errors.New("Linux owner anchor client connected to a non-Unix socket")
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		stopClose()
		_ = connection.Close()
		return nil, nil, fmt.Errorf("set Linux owner anchor deadline: %w", err)
	}
	return connection, stopClose, nil
}

// StartHelper transfers the pinned leaf and a one-way owner-liveness pipe to
// the anchor. The anchor starts at most one exact helper for this RunKey. A
// caller retry after losing this response must use AttachHelper; it must not
// manufacture a new owner pipe or a second helper.
func (client *LinuxOwnerAnchorClient) StartHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, leafFD int) (*LinuxOwnerAnchorHelper, error) {
	if ctx == nil || client == nil || leafFD < 0 {
		return nil, errors.New("Linux owner anchor helper launch requires a client, context, and pinned leaf FD")
	}
	if err := grant.Validate(); err != nil {
		return nil, err
	}
	if grant.OwnerUID != client.owner.AUID || grant.LauncherUID != 0 {
		return nil, errors.New("Linux owner anchor grant UID boundary differs from fixed A/B configuration")
	}
	if err := verifyLinuxOwnerAnchorLeafFD(leafFD, grant.Leaf, client.owner.AUID); err != nil {
		return nil, fmt.Errorf("verify caller's pinned Linux cgroup leaf: %w", err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create Linux owner helper death latch: %w", err)
	}
	defer ownerRead.Close()
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationStart,
		Grant: grant,
	}
	response, rights, err := client.exchange(ctx, request, leafFD, int(ownerRead.Fd()))
	if err != nil {
		_ = ownerWrite.Close()
		closeLinuxOwnerAnchorFDs(rights)
		return nil, err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationStart) {
		_ = ownerWrite.Close()
		closeLinuxOwnerAnchorFDs(rights)
		return nil, errors.New("Linux owner anchor response does not match the exact run grant")
	}
	switch response.Status {
	case "started":
		if response.Helper == nil || response.Helper.Validate() != nil || len(rights) != 3 {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, errors.New("Linux owner anchor returned an incomplete helper identity or descriptor set")
		}
		if err := verifyLinuxOwnerAnchorPIDFD(rights[0], *response.Helper); err != nil {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify returned Linux owner helper pidfd: %w", err)
		}
		if err := verifyLinuxOwnerAProcess(response.Helper.PID, response.Helper.Identity, client.owner.AUID, client.owner.AGID, grant.Anchor.PID, grant.Parent.Path); err != nil {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify returned Linux owner helper credentials and manager membership: %w", err)
		}
		if err := verifyLinuxOwnerAnchorLeafFD(rights[1], grant.Leaf, client.owner.AUID); err != nil {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify returned independent Linux owner leaf pin: %w", err)
		}
		if err := verifyLinuxOwnerAnchorParentFD(rights[2], grant.Parent); err != nil {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify returned independent Linux owner parent pin: %w", err)
		}
		pidfdFile := os.NewFile(uintptr(rights[0]), "symmetry-owner-anchor-helper-pidfd")
		if pidfdFile == nil {
			_ = ownerWrite.Close()
			closeLinuxOwnerAnchorFDs(rights)
			return nil, errors.New("wrap returned Linux owner helper pidfd")
		}
		leafFile := os.NewFile(uintptr(rights[1]), "symmetry-owner-anchor-helper-leaf")
		if leafFile == nil {
			_ = ownerWrite.Close()
			_ = pidfdFile.Close()
			_ = unix.Close(rights[1])
			_ = unix.Close(rights[2])
			return nil, errors.New("wrap returned Linux owner helper leaf FD")
		}
		parentFile := os.NewFile(uintptr(rights[2]), "symmetry-owner-anchor-helper-parent")
		if parentFile == nil {
			_ = ownerWrite.Close()
			_ = pidfdFile.Close()
			_ = leafFile.Close()
			_ = unix.Close(rights[2])
			return nil, errors.New("wrap returned Linux owner helper parent FD")
		}
		control, err := dialLinuxOwnerHelperV2(ctx, client, grant, *response.Helper)
		if err != nil {
			_ = ownerWrite.Close()
			_ = pidfdFile.Close()
			_ = leafFile.Close()
			_ = parentFile.Close()
			return nil, fmt.Errorf("connect to the exact Linux owner helper: %w", err)
		}
		return &LinuxOwnerAnchorHelper{
			PID: response.Helper.PID, Identity: response.Helper.Identity,
			PIDFD: pidfdFile, LeafFD: leafFile, ParentFD: parentFile, OwnerFD: ownerWrite, Control: control,
		}, nil
	case "already_started":
		_ = ownerWrite.Close()
		closeLinuxOwnerAnchorFDs(rights)
		return nil, ErrLinuxOwnerAnchorHelperAlreadyStarted
	default:
		_ = ownerWrite.Close()
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("Linux owner anchor returned unsupported status %q", response.Status)
	}
}

// AttachHelper returns only the existing exact helper, its retained leaf pin,
// and a recovery/control connection. It never creates a replacement owner
// pipe or starts another child.
func (client *LinuxOwnerAnchorClient) AttachHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, expected authority.LinuxCgroupProcessIdentity) (*LinuxOwnerAnchorHelper, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || expected.Validate() != nil {
		return nil, errors.New("Linux owner helper attachment is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationAttach,
		Grant: grant, Helper: &expected,
	}
	response, rights, err := client.exchange(ctx, request)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationAttach) || response.Status != "attached" ||
		response.Helper == nil || !response.Helper.Equal(expected) || len(rights) != 3 {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, errors.New("Linux owner anchor did not return the exact existing helper")
	}
	if err := verifyLinuxOwnerAnchorPIDFD(rights[0], expected); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify attached Linux owner helper pidfd: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(expected.PID, expected.Identity, client.owner.AUID, client.owner.AGID, grant.Anchor.PID, grant.Parent.Path); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify attached Linux owner helper credentials: %w", err)
	}
	if err := verifyLinuxOwnerAnchorLeafFD(rights[1], grant.Leaf, client.owner.AUID); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify attached Linux owner leaf pin: %w", err)
	}
	if err := verifyLinuxOwnerAnchorParentFD(rights[2], grant.Parent); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify attached Linux owner parent pin: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(rights[0]), "symmetry-owner-anchor-helper-pidfd")
	leafFile := os.NewFile(uintptr(rights[1]), "symmetry-owner-anchor-helper-leaf")
	parentFile := os.NewFile(uintptr(rights[2]), "symmetry-owner-anchor-helper-parent")
	if pidfdFile == nil || leafFile == nil || parentFile == nil {
		if pidfdFile != nil {
			_ = pidfdFile.Close()
		} else {
			_ = unix.Close(rights[0])
		}
		if leafFile != nil {
			_ = leafFile.Close()
		} else {
			_ = unix.Close(rights[1])
		}
		if parentFile != nil {
			_ = parentFile.Close()
		} else {
			_ = unix.Close(rights[2])
		}
		return nil, errors.New("wrap attached Linux owner helper descriptors")
	}
	control, err := dialLinuxOwnerHelperV2(ctx, client, grant, expected)
	if err != nil {
		_ = pidfdFile.Close()
		_ = leafFile.Close()
		_ = parentFile.Close()
		return nil, fmt.Errorf("connect to the exact Linux owner recovery endpoint: %w", err)
	}
	return &LinuxOwnerAnchorHelper{PID: expected.PID, Identity: expected.Identity, PIDFD: pidfdFile, LeafFD: leafFile, ParentFD: parentFile, Control: control}, nil
}

// LookupHelper resolves a lost StartHelper response only through the live
// authenticated anchor's RunKey map. It never starts a child or reopens a
// helper endpoint by path alone.
func (client *LinuxOwnerAnchorClient) LookupHelper(ctx context.Context, grant LinuxOwnerAnchorGrant) (*LinuxOwnerAnchorHelper, error) {
	if client == nil || ctx == nil || grant.Validate() != nil {
		return nil, errors.New("Linux owner helper lookup is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationLookup, Grant: grant,
	}
	response, rights, err := client.exchange(ctx, request)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationLookup) || response.Helper == nil || response.Helper.Validate() != nil ||
		(response.Status != "attached" && response.Status != "helper_exited") {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, errors.New("Linux owner anchor has no attachable helper for the exact RunKey")
	}
	helper := *response.Helper
	if response.Status == "helper_exited" {
		if response.HelperExit == nil || !response.HelperExit.ValidFor(helper, grant.Anchor) || len(rights) != 2 {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, errors.New("anchor returned no exact helper exit or run pins")
		}
		if err := verifyLinuxOwnerAnchorLeafFD(rights[0], grant.Leaf, client.owner.AUID); err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify retained dead-helper leaf pin: %w", err)
		}
		if err := verifyLinuxOwnerAnchorParentFD(rights[1], grant.Parent); err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("verify retained dead-helper parent pin: %w", err)
		}
		leafFile := os.NewFile(uintptr(rights[0]), "symmetry-owner-anchor-helper-leaf")
		parentFile := os.NewFile(uintptr(rights[1]), "symmetry-owner-anchor-helper-parent")
		if leafFile == nil || parentFile == nil {
			if leafFile != nil {
				_ = leafFile.Close()
			} else {
				_ = unix.Close(rights[0])
			}
			if parentFile != nil {
				_ = parentFile.Close()
			} else {
				_ = unix.Close(rights[1])
			}
			return nil, errors.New("wrap retained dead-helper pins")
		}
		return &LinuxOwnerAnchorHelper{PID: helper.PID, Identity: helper.Identity, LeafFD: leafFile, ParentFD: parentFile, HelperExit: response.HelperExit}, nil
	}
	if len(rights) != 3 {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, errors.New("Linux owner anchor active helper response omitted an exact run pin")
	}
	if err := verifyLinuxOwnerAnchorPIDFD(rights[0], helper); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify RunKey-looked-up helper pidfd: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(helper.PID, helper.Identity, client.owner.AUID, client.owner.AGID, grant.Anchor.PID, grant.Parent.Path); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify RunKey-looked-up helper credentials: %w", err)
	}
	if err := verifyLinuxOwnerAnchorLeafFD(rights[1], grant.Leaf, client.owner.AUID); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify RunKey-looked-up helper leaf pin: %w", err)
	}
	if err := verifyLinuxOwnerAnchorParentFD(rights[2], grant.Parent); err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return nil, fmt.Errorf("verify RunKey-looked-up helper parent pin: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(rights[0]), "symmetry-owner-anchor-helper-pidfd")
	leafFile := os.NewFile(uintptr(rights[1]), "symmetry-owner-anchor-helper-leaf")
	parentFile := os.NewFile(uintptr(rights[2]), "symmetry-owner-anchor-helper-parent")
	if pidfdFile == nil || leafFile == nil || parentFile == nil {
		if pidfdFile != nil {
			_ = pidfdFile.Close()
		} else {
			_ = unix.Close(rights[0])
		}
		if leafFile != nil {
			_ = leafFile.Close()
		} else {
			_ = unix.Close(rights[1])
		}
		if parentFile != nil {
			_ = parentFile.Close()
		} else {
			_ = unix.Close(rights[2])
		}
		return nil, errors.New("wrap looked-up Linux owner helper descriptors")
	}
	control, err := dialLinuxOwnerHelperV2(ctx, client, grant, helper)
	if err != nil {
		_ = pidfdFile.Close()
		_ = leafFile.Close()
		_ = parentFile.Close()
		return nil, fmt.Errorf("connect to looked-up Linux owner recovery endpoint: %w", err)
	}
	return &LinuxOwnerAnchorHelper{PID: helper.PID, Identity: helper.Identity, PIDFD: pidfdFile, LeafFD: leafFile, ParentFD: parentFile, Control: control}, nil
}

// RegisterProcess binds an exact launcher, native PID, or known descendant to
// the run/helper after the caller has durably recorded that process identity.
// The parked trusted gate has a separate RegisterGate path so its pre-release
// user namespace can anchor the later same-PID A-to-B transition.
func (client *LinuxOwnerAnchorClient) RegisterProcess(ctx context.Context, grant LinuxOwnerAnchorGrant, helper, creator, process authority.LinuxCgroupProcessIdentity, role string, pidfd int) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || creator.Validate() != nil || process.Validate() != nil || pidfd < 0 {
		return errors.New("Linux owner anchor process registration is incomplete")
	}
	if role != "launcher" && role != "native" && role != "descendant" {
		return errors.New("Linux owner anchor process role is unsupported")
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, process); err != nil {
		return fmt.Errorf("verify process pidfd before owner-anchor registration: %w", err)
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: "register-process", Grant: grant,
		Helper: &helper, Creator: &creator, Process: &process, Role: role,
	}
	response, rights, err := client.exchange(ctx, request, pidfd)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, request.Operation) || response.Status != "registered" || len(rights) != 0 {
		return errors.New("Linux owner anchor did not authenticate the exact run-process registration")
	}
	return nil
}

// ClassifyRunChildOrigin asks the authenticated Anchor to classify one exact
// process against the target RunKey and all active gate-userns pins. The PIDFD
// is borrowed and transferred only for this bounded read-only request.
func (client *LinuxOwnerAnchorClient) ClassifyRunChildOrigin(
	ctx context.Context,
	grant LinuxOwnerAnchorGrant,
	helper, process authority.LinuxCgroupProcessIdentity,
	pidfd *os.File,
) (linuxOwnerAnchorRunChildClassification, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil ||
		process.Validate() != nil || pidfd == nil {
		return linuxOwnerAnchorRunChildClassification{}, errors.New("Linux owner anchor child-origin request is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(pidfd.Fd()), process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("verify exact process pidfd before origin request: %w", err)
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationClassifyChild,
		Grant: grant, Helper: &helper, Process: &process,
	}
	response, rights, err := client.exchange(ctx, request, int(pidfd.Fd()))
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return linuxOwnerAnchorRunChildClassification{}, err
	}
	classification := response.ChildClassification
	if !response.matches(grant, request.Operation) || response.Status != "classified" ||
		response.Helper == nil || !response.Helper.Equal(helper) || response.Evidence != nil ||
		response.StopEvidence != nil || len(response.ReapEvidence) != 0 || response.HelperExit != nil ||
		classification == nil || len(rights) != 0 {
		return linuxOwnerAnchorRunChildClassification{}, errors.New("Linux owner anchor returned no authenticated exact child-origin classification")
	}
	if err := classification.ValidateFor(grant, helper, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, err
	}
	return *classification, nil
}

// RegisterGate pins the authenticated helper's exact trusted A-gate result,
// pre-release user namespace, and pidfd. The anchor independently verifies
// the A credentials, B-only maps, setgroups denial, namespace inode, ancestry,
// and exact run-leaf membership before accepting the gate transition proof.
func (client *LinuxOwnerAnchorClient) RegisterGate(
	ctx context.Context,
	grant LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
	pidfd int,
) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil ||
		gate.Validate() != nil || gate.HostUID != grant.OwnerUID || !gate.Cgroup.Equal(grant.Leaf) ||
		!userNamespace.valid() || pidfd < 0 {
		return errors.New("Linux owner anchor gate registration is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, gate.Process); err != nil {
		return fmt.Errorf("verify parked-gate pidfd before owner-anchor registration: %w", err)
	}
	gateCopy := gate
	userNamespaceCopy := userNamespace
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRegisterGate,
		Grant: grant, Helper: &helper, Gate: &gateCopy, UserNamespace: &userNamespaceCopy,
	}
	response, rights, err := client.exchange(ctx, request, pidfd)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, request.Operation) || response.Status != "registered" || len(rights) != 0 {
		return errors.New("Linux owner anchor did not authenticate the exact parked-gate registration")
	}
	return nil
}

// AuthorizeGateRelease records one exact helper authorization before the
// parked A gate can become B. Exact retries after an uncertain ACK are
// accepted only while the same pinned process is still the pre-release A
// gate; a missing ACK must never be treated as permission to RELEASE.
func (client *LinuxOwnerAnchorClient) AuthorizeGateRelease(
	ctx context.Context,
	grant LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil ||
		gate.Validate() != nil || gate.HostUID != grant.OwnerUID || !gate.Cgroup.Equal(grant.Leaf) || !userNamespace.valid() {
		return errors.New("Linux owner anchor gate-release authorization is incomplete")
	}
	gateCopy := gate
	userNamespaceCopy := userNamespace
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationAuthorizeGate,
		Grant: grant, Helper: &helper, Gate: &gateCopy, UserNamespace: &userNamespaceCopy,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, request.Operation) || response.Status != "release_authorized" || len(rights) != 0 {
		return errors.New("Linux owner anchor did not authenticate the exact one-use gate-release authorization")
	}
	return nil
}

// WaitReap asks the independent owner anchor to wait and reap one previously
// registered process after the exact helper was itself waited and reaped. An
// absent run or child, ECHILD, unknown ancestry, or identity/membership error
// returns no evidence.
func (client *LinuxOwnerAnchorClient) WaitReap(ctx context.Context, grant LinuxOwnerAnchorGrant, helper, process authority.LinuxCgroupProcessIdentity, role string) (LinuxOwnerAnchorReapEvidence, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || process.Validate() != nil || !linuxOwnerAnchorValidChildRole(role) {
		return LinuxOwnerAnchorReapEvidence{}, errors.New("Linux owner anchor wait/reap request is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: "wait-reap", Grant: grant,
		Helper: &helper, Process: &process, Role: role,
	}
	response, rights, err := client.exchange(ctx, request, -1)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	if !response.matches(grant, request.Operation) || response.Status != "reaped" || len(rights) != 0 || response.Evidence == nil {
		return LinuxOwnerAnchorReapEvidence{}, errors.New("Linux owner anchor returned no exact run-bound wait/reap evidence")
	}
	if err := response.Evidence.ValidateFor(grant, helper, process, role); err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	return *response.Evidence, nil
}

// StopLeaf transfers cgroup.kill authority to the exact anchor after the
// helper has latched stop and fenced all producers. A retry after a known
// pre-syscall failure revalidates the same fence before the first actual write;
// once a write may have been attempted, a retry returns only cached evidence
// or remains unresolved and never writes again. The result is not a State receipt.
func (client *LinuxOwnerAnchorClient) StopLeaf(
	ctx context.Context,
	grant LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	producerFence LinuxOwnerAnchorProducerFence,
) (LinuxOwnerAnchorStopEvidence, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil ||
		producerFence.ValidateFor(grant, helper) != nil {
		return LinuxOwnerAnchorStopEvidence{}, errors.New("Linux owner anchor stop request lacks the exact producer fence")
	}
	fenceCopy := producerFence.Clone()
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationStopLeaf,
		Grant: grant, Helper: &helper, StopFence: &fenceCopy,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	if !response.matches(grant, request.Operation) || response.Status != "stopped" || len(rights) != 0 || response.StopEvidence == nil {
		return LinuxOwnerAnchorStopEvidence{}, errors.New("Linux owner anchor returned no exact cached StopLeaf evidence")
	}
	if err := response.StopEvidence.ValidateFor(grant, helper, grant.Anchor, nil, false); err != nil ||
		response.StopEvidence.ProducerFence == nil || !response.StopEvidence.ProducerFence.Equal(producerFence) || response.StopEvidence.HelperExit != nil {
		return LinuxOwnerAnchorStopEvidence{}, errors.Join(errors.New("Linux owner anchor StopLeaf evidence differs from the exact producer fence"), err)
	}
	return response.StopEvidence.Clone(), nil
}

// WaitReapRun asks the anchor to reap every currently adopted child bound to
// this exact RunKey after helper death. The response is paged so large native
// descendant sets cannot exceed the local seqpacket frame limit; every page
// carries the same cached, anchor-issued physical-stop observation.
func (client *LinuxOwnerAnchorClient) WaitReapRun(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) (LinuxOwnerAnchorRunReapResult, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil {
		return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor run-wide wait/reap request is incomplete")
	}
	result := make([]LinuxOwnerAnchorReapEvidence, 0)
	var stopEvidence *LinuxOwnerAnchorStopEvidence
	afterPID := 0
	for {
		request := linuxOwnerAnchorRequest{
			Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationReapRun,
			Grant: grant, Helper: &helper, AfterPID: afterPID,
		}
		response, rights, err := client.exchange(ctx, request)
		closeLinuxOwnerAnchorFDs(rights)
		if err != nil {
			return LinuxOwnerAnchorRunReapResult{}, err
		}
		if !response.matches(grant, linuxOwnerAnchorOperationReapRun) || response.Status != "reaped" ||
			response.Evidence != nil || len(rights) != 0 || response.StopEvidence == nil ||
			len(response.ReapEvidence) > linuxOwnerAnchorReapPageSize {
			return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor returned an invalid run-wide reap page")
		}
		if stopEvidence == nil {
			copy := response.StopEvidence.Clone()
			stopEvidence = &copy
		} else if !stopEvidence.Equal(*response.StopEvidence) {
			return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor changed cached StopLeaf evidence across reap pages")
		}
		for _, evidence := range response.ReapEvidence {
			if !linuxOwnerAnchorValidChildRole(evidence.ProcessRole) ||
				evidence.ValidateFor(grant, helper, evidence.Process, evidence.ProcessRole) != nil || evidence.Process.PID <= afterPID {
				return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor run-wide reap page contains non-exact evidence")
			}
			result = append(result, evidence)
		}
		if !response.More {
			if stopEvidence == nil {
				return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor returned no cached stop evidence")
			}
			if err := stopEvidence.ValidateFor(grant, helper, grant.Anchor, result, true); err != nil {
				return LinuxOwnerAnchorRunReapResult{}, err
			}
			return LinuxOwnerAnchorRunReapResult{Processes: result, Stop: *stopEvidence}, nil
		}
		if response.NextPID <= afterPID || len(response.ReapEvidence) == 0 {
			return LinuxOwnerAnchorRunReapResult{}, errors.New("Linux owner anchor run-wide reap cursor did not advance")
		}
		afterPID = response.NextPID
	}
}

// WaitHelper returns the anchor's exact P_PIDFD wait/reap proof for the
// recorded helper even when that helper produced no gate or descendants.
func (client *LinuxOwnerAnchorClient) WaitHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) (authority.LinuxCgroupExitProof, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil {
		return authority.LinuxCgroupExitProof{}, errors.New("Linux owner helper wait is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationWaitHelper,
		Grant: grant, Helper: &helper,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return authority.LinuxCgroupExitProof{}, err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationWaitHelper) || response.Status != "reaped" ||
		response.Helper == nil || !response.Helper.Equal(helper) || response.HelperExit == nil || len(rights) != 0 ||
		!response.HelperExit.ValidFor(helper, grant.Anchor) {
		return authority.LinuxCgroupExitProof{}, errors.New("Linux owner anchor did not return exact helper wait/reap evidence")
	}
	return *response.HelperExit, nil
}

func (client *LinuxOwnerAnchorClient) exchange(ctx context.Context, request linuxOwnerAnchorRequest, sendFDs ...int) (linuxOwnerAnchorResponse, []int, error) {
	if client == nil || ctx == nil || ctx.Err() != nil {
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor request has no live client or context")
	}
	var expectedAnchor authority.LinuxCgroupProcessIdentity
	cloneCleanup := request.Operation == linuxOwnerAnchorOperationCloneCleanup
	stopLeaf := request.Operation == linuxOwnerAnchorOperationStopLeaf
	classifyChild := request.Operation == linuxOwnerAnchorOperationClassifyChild
	if cloneCleanup {
		wantFDs := 1
		if request.TargetFDPresent {
			wantFDs++
		}
		if request.Grant != (LinuxOwnerAnchorGrant{}) || request.CloneGrant == nil || request.Cleanup == nil ||
			request.RunKey != (LinuxOwnerAnchorRunKey{RunID: request.Cleanup.Intent.RunID, Generation: request.Cleanup.Intent.Generation}) ||
			request.FenceDigest != request.Cleanup.FenceDigest || request.Fence != (protocol.Fence{}) ||
			len(sendFDs) != wantFDs || request.Helper != nil || request.Gate != nil || request.UserNamespace != nil ||
			request.StopFence != nil ||
			request.Creator != nil || request.Process != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil || request.AfterPID != 0 ||
			request.Cleanup.NormalReleaseProof.CgroupIdentity == nil ||
			(request.TargetFDPresent && request.PriorRemovalProof != nil) || (!request.TargetFDPresent && request.PriorRemovalProof == nil) {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor cleanup request is not bound to its durable RunKey and fence digest")
		}
		if err := validateLinuxOwnerAnchorCloneCleanup(*request.CloneGrant, *request.Cleanup, client.owner.AUID); err != nil {
			return linuxOwnerAnchorResponse{}, nil, err
		}
		if !request.TargetFDPresent {
			if err := verifyLinuxOwnerAnchorPriorRemoval(sendFDs[0], *request.Cleanup, request.PriorRemovalProof, client.owner); err != nil {
				return linuxOwnerAnchorResponse{}, nil, err
			}
		}
		expectedAnchor = authority.LinuxCgroupProcessIdentity{
			PID:      request.Cleanup.NormalReleaseProof.AnchorPID,
			Identity: request.Cleanup.NormalReleaseProof.AnchorIdentity,
		}
		if expectedAnchor.PID == os.Getpid() {
			return linuxOwnerAnchorResponse{}, nil, errors.New("clone cleanup daemon and owner anchor must be distinct exact processes")
		}
	} else if classifyChild {
		if len(sendFDs) != 1 || request.Grant.Validate() != nil || request.Helper == nil || request.Helper.Validate() != nil ||
			request.Process == nil || request.Process.Validate() != nil || request.Gate != nil || request.UserNamespace != nil ||
			request.StopFence != nil || request.Creator != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil ||
			request.RunKey != (LinuxOwnerAnchorRunKey{}) || request.Fence != (protocol.Fence{}) || request.FenceDigest != "" ||
			request.AfterPID != 0 || request.CloneGrant != nil || request.Cleanup != nil || request.PriorRemovalProof != nil || request.TargetFDPresent {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner child-origin request differs from its exact helper/process PIDFD")
		}
		if err := verifyLinuxOwnerAnchorPIDFD(sendFDs[0], *request.Process); err != nil {
			return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("verify exact process pidfd for owner-origin request: %w", err)
		}
		expectedAnchor = request.Grant.Anchor
	} else if stopLeaf {
		if len(sendFDs) != 0 || request.Grant.Validate() != nil || request.Helper == nil || request.Helper.Validate() != nil ||
			request.StopFence == nil || request.StopFence.ValidateFor(request.Grant, *request.Helper) != nil ||
			request.Gate != nil || request.UserNamespace != nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil ||
			request.RunKey != (LinuxOwnerAnchorRunKey{}) || request.Fence != (protocol.Fence{}) || request.FenceDigest != "" ||
			request.AfterPID != 0 || request.CloneGrant != nil || request.Cleanup != nil || request.PriorRemovalProof != nil || request.TargetFDPresent {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor StopLeaf request differs from its exact helper producer fence")
		}
		expectedAnchor = request.Grant.Anchor
	} else {
		if request.StopFence != nil {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor request carries an unexpected StopLeaf producer fence")
		}
		if request.Grant.Validate() != nil {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor request has no exact durable grant")
		}
		if request.Grant.OwnerUID != client.owner.AUID || request.Grant.LauncherUID != 0 {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor grant UID boundary differs from fixed A/B configuration")
		}
		if request.Grant.Anchor.PID == os.Getpid() {
			return linuxOwnerAnchorResponse{}, nil, errors.New("daemon client and Linux owner anchor must be distinct exact processes")
		}
		expectedAnchor = request.Grant.Anchor
	}
	timeout := linuxOwnerAnchorRequestTimeout
	if request.Operation == "wait-reap" || classifyChild {
		timeout = linuxOwnerAnchorWaitTimeout
	}
	if request.Operation == linuxOwnerAnchorOperationReapRun {
		timeout = linuxOwnerAnchorWaitTimeout
	}
	if request.Operation == linuxOwnerAnchorOperationWaitHelper {
		timeout = linuxOwnerAnchorWaitTimeout
	}
	if request.Operation == linuxOwnerAnchorOperationLookup {
		timeout = linuxOwnerAnchorWaitTimeout
	}
	if request.Operation == linuxOwnerAnchorOperationCloneCleanup {
		timeout = linuxOwnerAnchorCleanupTimeout
	}
	if stopLeaf {
		timeout = linuxOwnerAnchorWaitTimeout
	}
	connection, stopClose, err := client.dial(ctx, timeout)
	if err != nil {
		return linuxOwnerAnchorResponse{}, nil, err
	}
	defer connection.Close()
	defer stopClose()
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, expectedAnchor); err != nil {
		return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("authenticate Linux owner anchor peer: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("capture daemon peer identity for owner anchor request: %w", err)
	}
	request.ClientPID = os.Getpid()
	request.ClientIdentity = clientIdentity
	requestBytes, err := json.Marshal(request)
	if err != nil || len(requestBytes) > linuxOwnerAnchorMaxFrame {
		return linuxOwnerAnchorResponse{}, nil, errors.Join(errors.New("encode Linux owner anchor request within the frame limit"), err)
	}
	var rights []byte
	duplicates := make([]int, 0, len(sendFDs))
	defer closeLinuxOwnerAnchorFDs(duplicates)
	for _, sendFD := range sendFDs {
		if sendFD < 0 {
			continue
		}
		duplicate, err := unix.FcntlInt(uintptr(sendFD), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("duplicate pinned FD for Linux owner anchor transfer: %w", err)
		}
		duplicates = append(duplicates, duplicate)
	}
	if len(duplicates) > 3 {
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor descriptor transfer exceeds its fixed limit")
	}
	if len(duplicates) > 0 {
		rights = syscall.UnixRights(duplicates...)
	}
	written, _, err := connection.WriteMsgUnix(requestBytes, rights, nil)
	if err != nil || written != len(requestBytes) {
		return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("send authenticated Linux owner anchor request: %w", errOr(err, io.ErrShortWrite))
	}
	response, receivedFDs, err := readLinuxOwnerAnchorResponse(connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(receivedFDs)
		return linuxOwnerAnchorResponse{}, nil, err
	}
	if cloneCleanup {
		parent := request.Cleanup.NormalReleaseProof.CgroupIdentity.Parent
		if response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != request.Operation ||
			response.RunKey != request.RunKey || response.FenceDigest != request.FenceDigest ||
			response.LaunchToken != request.Cleanup.NormalReleaseProof.LaunchToken ||
			!response.Anchor.Equal(expectedAnchor) || !response.Parent.Equal(parent) || response.OwnerUID != client.owner.AUID ||
			response.ManagerPath != parent.Path || response.Fence != (protocol.Fence{}) ||
			response.Helper != nil || response.Evidence != nil || len(response.ReapEvidence) != 0 || response.HelperExit != nil ||
			response.NextPID != 0 || response.More {
			closeLinuxOwnerAnchorFDs(receivedFDs)
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor cleanup response differs from its exact intent and Anchor")
		}
	} else if !response.matches(request.Grant, request.Operation) {
		closeLinuxOwnerAnchorFDs(receivedFDs)
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor response does not match the exact request grant")
	}
	return response, receivedFDs, nil
}

type linuxOwnerAnchorRequest struct {
	Version             int                                         `json:"version"`
	Operation           string                                      `json:"operation"`
	ClientPID           int                                         `json:"client_pid"`
	ClientIdentity      string                                      `json:"client_identity"`
	Grant               LinuxOwnerAnchorGrant                       `json:"grant"`
	Helper              *authority.LinuxCgroupProcessIdentity       `json:"helper,omitempty"`
	Gate                *authority.LinuxCgroupGateIdentity          `json:"gate,omitempty"`
	UserNamespace       *LinuxUserNamespaceIdentity                 `json:"user_namespace,omitempty"`
	StopFence           *LinuxOwnerAnchorProducerFence              `json:"stop_fence,omitempty"`
	Creator             *authority.LinuxCgroupProcessIdentity       `json:"creator,omitempty"`
	Process             *authority.LinuxCgroupProcessIdentity       `json:"process,omitempty"`
	Role                string                                      `json:"role,omitempty"`
	ReleaseProof        *authority.SupervisorHandoffReleaseProof    `json:"release_proof,omitempty"`
	PreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	AbortProof          *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
	RunKey              LinuxOwnerAnchorRunKey                      `json:"run_key,omitempty"`
	Fence               protocol.Fence                              `json:"fence,omitempty"`
	FenceDigest         string                                      `json:"fence_digest,omitempty"`
	AfterPID            int                                         `json:"after_pid,omitempty"`
	CloneGrant          *authority.LinuxCloneGrant                  `json:"clone_grant,omitempty"`
	Cleanup             *authority.LinuxCloneCleanupIntent          `json:"cleanup,omitempty"`
	PriorRemovalProof   *authority.LinuxCloneTargetRemovalProof     `json:"prior_removal_proof,omitempty"`
	TargetFDPresent     bool                                        `json:"target_fd_present,omitempty"`
}

type linuxOwnerAnchorResponse struct {
	Version             int                                     `json:"version"`
	Operation           string                                  `json:"operation"`
	Status              string                                  `json:"status"`
	RunKey              LinuxOwnerAnchorRunKey                  `json:"run_key"`
	Fence               protocol.Fence                          `json:"fence"`
	FenceDigest         string                                  `json:"fence_digest,omitempty"`
	LaunchToken         string                                  `json:"launch_token"`
	Anchor              authority.LinuxCgroupProcessIdentity    `json:"anchor"`
	Parent              authority.LinuxCgroupParentIdentity     `json:"parent"`
	OwnerUID            uint32                                  `json:"owner_uid"`
	ManagerPath         string                                  `json:"manager_path"`
	Helper              *authority.LinuxCgroupProcessIdentity   `json:"helper,omitempty"`
	ChildClassification *linuxOwnerAnchorRunChildClassification `json:"child_classification,omitempty"`
	Evidence            *LinuxOwnerAnchorReapEvidence           `json:"evidence,omitempty"`
	ReapEvidence        []LinuxOwnerAnchorReapEvidence          `json:"reap_evidence,omitempty"`
	StopEvidence        *LinuxOwnerAnchorStopEvidence           `json:"stop_evidence,omitempty"`
	HelperExit          *authority.LinuxCgroupExitProof         `json:"helper_exit,omitempty"`
	NextPID             int                                     `json:"next_pid,omitempty"`
	More                bool                                    `json:"more,omitempty"`
	CloneRemoval        *authority.LinuxCloneTargetRemovalProof `json:"clone_removal_proof,omitempty"`
}

func (response linuxOwnerAnchorResponse) matches(grant LinuxOwnerAnchorGrant, operation string) bool {
	return response.Version == linuxOwnerAnchorProtocolVersion && response.Operation == operation &&
		response.RunKey == grant.RunKey && response.Fence == grant.Fence && response.LaunchToken == grant.LaunchToken &&
		response.Anchor.Equal(grant.Anchor) && response.Parent.Equal(grant.Parent) &&
		response.OwnerUID == grant.OwnerUID && response.ManagerPath == grant.Parent.Path
}

type linuxOwnerAnchorOptions struct {
	cgroupMount   string
	delegatedPath string
	socketPath    string
}

// RunLinuxOwnerAnchor is the hidden long-lived owner service entrypoint. It
// verifies the fixed A/B installation, pins the operator-selected cgroup2
// manager subgroup, installs and reads back its own subreaper flag, then
// serves one bounded authenticated request at a time. Shutdown closes only
// local descriptors; it never manufactures a RunJournal receipt.
func RunLinuxOwnerAnchor(args []string, openVerifier LinuxOwnerAnchorCleanupVerifierFactory) (result error) {
	if openVerifier == nil {
		return errors.New("Linux owner anchor requires a State cleanup verifier factory")
	}
	options, err := parseLinuxOwnerAnchorArgs(args)
	if err != nil {
		return err
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return fmt.Errorf("read fixed Linux owner configuration: %w", err)
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID {
		return errors.New("Linux owner anchor must run with the configured host UID/GID A")
	}
	cleanupVerifier, err := openLinuxOwnerAnchorCleanupVerifierForConfig(owner, openVerifier)
	if err != nil {
		return fmt.Errorf("prepare read-only StateDir clone-cleanup verifier before READY: %w", err)
	}
	if cleanupVerifier != nil {
		defer func() {
			if closeErr := cleanupVerifier.Close(); closeErr != nil {
				result = errors.Join(result, fmt.Errorf("close read-only StateDir clone-cleanup verifier: %w", closeErr))
			}
		}()
	} // strict v1 owner config has no StateDir; retain legacy launch/recovery but never allow clone cleanup.
	witness, err := readLinuxOwnerAnchorWitnessConfig(owner)
	if err != nil {
		return fmt.Errorf("validate root-owned witness selector before READY: %w", err)
	}
	parent, err := OpenLinuxCgroupV2Parent(options.cgroupMount, options.delegatedPath)
	if err != nil {
		return fmt.Errorf("open operator-delegated Linux cgroup2 manager subgroup: %w", err)
	}
	defer parent.Close()
	if parent.OwnerUID() != owner.AUID || parent.Identity().Path == "/" {
		return errors.New("Linux owner anchor delegated parent is not an A-owned manager subgroup")
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), parent.Identity().Path); err != nil {
		return fmt.Errorf("verify Linux owner anchor manager subgroup: %w", err)
	}
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		return err
	}
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return fmt.Errorf("capture Linux owner anchor process identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), anchorIdentity, owner.AUID, owner.AGID, 0, parent.Identity().Path); err != nil {
		return fmt.Errorf("verify Linux owner anchor A credentials and manager membership: %w", err)
	}
	anchorProcess := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity}
	anchorUserNamespaceFD, anchorUserNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(anchorProcess, -1)
	if err != nil {
		return fmt.Errorf("pin Linux owner anchor user namespace before READY: %w", err)
	}
	defer anchorUserNamespaceFD.Close()
	listener, socketIdentity, err := listenLinuxOwnerAnchor(options.socketPath, owner)
	if err != nil {
		return err
	}
	defer unlinkLinuxOwnerAnchorSocket(options.socketPath, socketIdentity, owner.AUID)
	anchor := &linuxOwnerAnchor{
		owner: owner, parent: parent, listener: listener,
		identity:        authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity},
		userNamespaceFD: anchorUserNamespaceFD, userNamespace: anchorUserNamespace,
		managerPath: parent.Identity().Path, socketPath: options.socketPath,
		runs: make(map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun), cleanupVerifier: cleanupVerifier, witness: witness,
		cloneCleanupWorker: linuxgate.RemoveBCloneAfterReceipt,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := notifyLinuxOwnerAnchorReady(); err != nil {
		return err
	}
	return anchor.serve(ctx)
}

func openLinuxOwnerAnchorCleanupVerifierForConfig(
	owner linuxgate.OwnerConfig,
	factory LinuxOwnerAnchorCleanupVerifierFactory,
) (LinuxOwnerAnchorCleanupVerifier, error) {
	if factory == nil {
		return nil, errors.New("Linux owner anchor requires a State cleanup verifier factory")
	}
	if owner.StateDir == "" {
		// Strict v1 owner configs remain usable for launch/recovery, but v2 clone
		// cleanup stays closed because no fixed StateDir is present.
		return nil, nil
	}
	if !validLinuxOwnerAnchorStateDir(owner.StateDir) {
		return nil, errors.New("Linux owner anchor requires canonical absolute StateDir from the root-installed owner configuration")
	}
	verifier, err := factory(owner.StateDir)
	if err != nil {
		return nil, err
	}
	if verifier == nil {
		return nil, errors.New("Linux owner anchor StateDir clone-cleanup verifier factory returned nil")
	}
	return verifier, nil
}

func parseLinuxOwnerAnchorArgs(args []string) (linuxOwnerAnchorOptions, error) {
	flags := flag.NewFlagSet("linux-owner-anchor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options linuxOwnerAnchorOptions
	flags.StringVar(&options.cgroupMount, "cgroup-mount", "", "operator-provisioned cgroup2 mount point")
	flags.StringVar(&options.delegatedPath, "delegated-path", "", "canonical relative manager subgroup beneath the cgroup2 mount")
	flags.StringVar(&options.socketPath, "socket", "", "stable A-private Unix seqpacket socket")
	if err := flags.Parse(args); err != nil {
		return linuxOwnerAnchorOptions{}, err
	}
	if flags.NArg() != 0 || !canonicalAbsoluteLinuxPath(options.cgroupMount) ||
		!canonicalDelegatedCgroupPath(options.delegatedPath) || options.delegatedPath == "" {
		return linuxOwnerAnchorOptions{}, errors.New("Linux owner anchor requires only -cgroup-mount, -delegated-path, and -socket flags")
	}
	if err := validateLinuxOwnerAnchorSocketPath(options.socketPath); err != nil {
		return linuxOwnerAnchorOptions{}, err
	}
	return options, nil
}

func validLinuxOwnerAnchorStateDir(value string) bool {
	if value == "" || len(value) > 4095 || !path.IsAbs(value) || path.Clean(value) != value || value == "/" {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x20 || value[index] == 0x7f {
			return false
		}
	}
	for _, component := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func readLinuxOwnerAnchorWitnessConfig(owner linuxgate.OwnerConfig) (*linuxOwnerAnchorWitnessConfig, error) {
	if owner.Validate() != nil {
		return nil, errors.New("Linux owner anchor witness config requires the fixed owner mapping")
	}
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open root for witness selector ancestry: %w", err)
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil || rootStat.Uid != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root witness selector ancestor: %w", errOr(err, errors.New("root ownership or mode is unsafe")))
	}
	etcFD, err := unix.Openat2(rootFD, "etc", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return nil, fmt.Errorf("open root-controlled Linux owner config directory: %w", err)
	}
	defer unix.Close(etcFD)
	var etcStat unix.Stat_t
	if err := unix.Fstat(etcFD, &etcStat); err != nil || etcStat.Uid != 0 || etcStat.Mode&unix.S_IFMT != unix.S_IFDIR || etcStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root-controlled /etc directory: %w", errOr(err, errors.New("directory ownership or mode is unsafe")))
	}
	symmetryFD, err := unix.Openat2(etcFD, "symmetry", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open root-controlled /etc/symmetry directory: %w", err)
	}
	defer unix.Close(symmetryFD)
	var directoryStat unix.Stat_t
	if err := unix.Fstat(symmetryFD, &directoryStat); err != nil || directoryStat.Uid != 0 ||
		directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR || directoryStat.Mode&0022 != 0 {
		return nil, fmt.Errorf("verify root-controlled /etc/symmetry directory: %w", errOr(err, errors.New("directory ownership or mode is unsafe")))
	}
	witnessName := filepath.Base(linuxOwnerAnchorWitnessConfigPath)
	witnessFD, err := unix.Openat2(symmetryFD, witnessName, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open root-controlled Linux owner witness selector: %w", err)
	}
	witnessFile := os.NewFile(uintptr(witnessFD), "linux-owner-root-witness-config")
	if witnessFile == nil {
		_ = unix.Close(witnessFD)
		return nil, errors.New("wrap root-controlled Linux owner witness selector")
	}
	defer witnessFile.Close()
	var witnessStat unix.Stat_t
	if err := unix.Fstat(witnessFD, &witnessStat); err != nil || witnessStat.Uid != 0 || witnessStat.Gid != owner.AGID ||
		witnessStat.Mode&unix.S_IFMT != unix.S_IFREG || witnessStat.Mode&0777 != 0440 || witnessStat.Nlink != 1 ||
		witnessStat.Size <= 0 || witnessStat.Size > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, fmt.Errorf("verify root-owned Linux owner witness selector: %w", errOr(err, errors.New("file ownership, mode, link count, or size is unsafe")))
	}
	contents, err := io.ReadAll(io.LimitReader(witnessFile, linuxOwnerAnchorWitnessConfigMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read root-controlled Linux owner witness selector: %w", err)
	}
	if len(contents) > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, errors.New("root-controlled Linux owner witness selector exceeds its bound")
	}
	return parseLinuxOwnerAnchorWitnessConfig(contents)
}

func parseLinuxOwnerAnchorWitnessConfig(contents []byte) (*linuxOwnerAnchorWitnessConfig, error) {
	if len(contents) == 0 || len(contents) > linuxOwnerAnchorWitnessConfigMaxBytes {
		return nil, errors.New("root-controlled Linux owner witness selector has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var witness linuxOwnerAnchorWitnessConfig
	if err := decoder.Decode(&witness); err != nil {
		return nil, fmt.Errorf("decode root-controlled Linux owner witness selector: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("root-controlled Linux owner witness selector has trailing JSON")
	}
	if witness.Version != linuxOwnerAnchorWitnessConfigVersion || !validLinuxOwnerAnchorRunID(witness.RunID) || witness.Generation <= 0 ||
		(witness.DropResponse == "") == (witness.HelperAction == "") ||
		witness.DropResponse != "" && (witness.DropResponse != "stop" && witness.DropResponse != "ack-stop-receipt" && witness.DropResponse != "launcher-ready" || witness.HoldForReleaseFile) ||
		witness.HelperAction != "" && (witness.HelperAction != "crash_after_release_before_register" || !witness.HoldForReleaseFile) {
		return nil, errors.New("root-controlled Linux owner witness selector fields are invalid")
	}
	return &witness, nil
}

type linuxOwnerAnchor struct {
	owner               linuxgate.OwnerConfig
	parent              *LinuxCgroupV2Parent
	listener            *net.UnixListener
	identity            authority.LinuxCgroupProcessIdentity
	userNamespaceFD     *os.File
	userNamespace       LinuxUserNamespaceIdentity
	managerPath         string
	socketPath          string
	stopping            bool
	shutdownDeadline    time.Time
	shutdownForced      bool
	shutdownError       error
	runs                map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun
	cleanupVerifier     LinuxOwnerAnchorCleanupVerifier
	cloneCleanupWorker  linuxOwnerAnchorCloneCleanupWorker
	witness             *linuxOwnerAnchorWitnessConfig
	cloneCleanupWorkers atomic.Int32
}

func (anchor *linuxOwnerAnchor) serve(ctx context.Context) (result error) {
	if anchor == nil {
		return errors.New("Linux owner anchor service is incomplete")
	}
	defer func() {
		anchor.stopping = true
		if anchor.listener != nil {
			_ = anchor.listener.Close()
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		for key, run := range anchor.runs {
			if run != nil && run.retired {
				if err := run.close(); err != nil {
					result = errors.Join(result, fmt.Errorf("close retired Linux owner run %s/%d: %w", key.RunID, key.Generation, err))
				}
				delete(anchor.runs, key)
				continue
			}
			if run == nil {
				result = errors.Join(result, fmt.Errorf("unresolved Linux owner RunKey %s/%d has no retained run pins", key.RunID, key.Generation))
				continue
			}
			stopErr := anchor.forceStopRunForShutdown(stopContext, run)
			if stopErr != nil {
				result = errors.Join(result, fmt.Errorf("final exact stop for unresolved Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, stopErr))
			}
			result = errors.Join(result, fmt.Errorf("Linux owner RunKey %s/%d remains unresolved; no durable receipt or clear was created", key.RunID, key.Generation))
		}
		cancel()
		if err := anchor.waitForCloneCleanupWorkers(); err != nil {
			result = errors.Join(result, fmt.Errorf("wait for bounded root clone cleanup worker before verifier close: %w", err))
		}
	}()
	if ctx == nil || anchor.listener == nil || anchor.parent == nil {
		return errors.New("Linux owner anchor service is incomplete")
	}
	for {
		if err := anchor.superviseExitedHelpers(); err != nil {
			anchor.stopping = true
			return fmt.Errorf("autonomous Linux owner helper-death containment failed: %w", err)
		}
		if ctx.Err() != nil {
			if !anchor.stopping {
				anchor.stopping = true
				anchor.shutdownDeadline = time.Now().Add(linuxOwnerAnchorWaitTimeout)
			}
			if anchor.allRunsRetired() {
				return nil
			}
			if !time.Now().Before(anchor.shutdownDeadline) {
				if anchor.shutdownForced {
					return errors.Join(anchor.shutdownError,
						errors.New("Linux owner anchor shutdown retained unresolved RunKeys after bounded stop/reap; no durable receipt or clear was created"))
				}
				stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				for key, run := range anchor.runs {
					if run != nil && !run.retired {
						if err := anchor.forceStopRunForShutdown(stopContext, run); err != nil {
							anchor.shutdownError = errors.Join(anchor.shutdownError,
								fmt.Errorf("stop Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, err))
						}
					}
				}
				cancel()
				anchor.shutdownForced = true
				anchor.shutdownDeadline = time.Now().Add(linuxOwnerAnchorWaitTimeout)
			}
		}
		if err := anchor.listener.SetDeadline(time.Now().Add(linuxOwnerAnchorWaitPoll)); err != nil {
			anchor.stopping = true
			stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
			stopErr := anchor.forceStopUnretiredRuns(stopContext)
			cancel()
			return errors.Join(fmt.Errorf("set Linux owner anchor accept deadline: %w", err), stopErr)
		}
		connection, err := anchor.listener.AcceptUnix()
		if err != nil {
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				continue
			}
			anchor.stopping = true
			stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
			stopErr := anchor.forceStopUnretiredRuns(stopContext)
			cancel()
			return errors.Join(fmt.Errorf("accept Linux owner anchor connection: %w", sanitizeLinuxOwnerAnchorSocketError(err)), stopErr)
		}
		anchor.serveConnection(connection)
	}
}

func (anchor *linuxOwnerAnchor) waitForCloneCleanupWorkers() error {
	if anchor == nil {
		return nil
	}
	deadline := time.Now().Add(linuxOwnerAnchorCleanupTimeout)
	timedOut := false
	for anchor.cloneCleanupWorkers.Load() != 0 {
		if !timedOut && !time.Now().Before(deadline) {
			timedOut = true
		}
		// Keep the State reader pinned until every worker exits. Its own
		// context and the owner unit's bounded stop timeout bound this wait.
		timer := time.NewTimer(linuxOwnerAnchorWaitPoll)
		<-timer.C
	}
	if timedOut {
		return context.DeadlineExceeded
	}
	return nil
}

func (anchor *linuxOwnerAnchor) allRunsRetired() bool {
	for _, run := range anchor.runs {
		if run == nil || !run.retired {
			return false
		}
	}
	return true
}

func (anchor *linuxOwnerAnchor) forceStopUnretiredRuns(ctx context.Context) error {
	if anchor == nil || ctx == nil {
		return ErrLinuxCgroupV2Unresolved
	}
	var result error
	for key, run := range anchor.runs {
		if run != nil && run.retired {
			continue
		}
		if run == nil {
			result = errors.Join(result, fmt.Errorf("unresolved Linux owner RunKey %s/%d has no retained run pins", key.RunID, key.Generation))
			continue
		}
		if err := anchor.forceStopRunForShutdown(ctx, run); err != nil {
			result = errors.Join(result, fmt.Errorf("stop unresolved Linux owner RunKey %s/%d: %w", key.RunID, key.Generation, err))
		}
	}
	return result
}

// superviseExitedHelpers is polled by the anchor's accept loop, independent
// of daemon requests. A helper death fences the exact run leaf and the anchor
// reaps its adopted children, but this observer never creates a journal
// receipt or releases the pinned RunKey.
func (anchor *linuxOwnerAnchor) superviseExitedHelpers() error {
	if anchor == nil {
		return errors.New("Linux owner anchor is nil")
	}
	now := time.Now()
	for _, run := range anchor.runs {
		if run == nil || run.retired || run.helperPIDFD == nil || run.helperDeathContainmentComplete ||
			run.supervisionTerminal || run.helperDeathContainmentAttempted && run.supervisionError == nil ||
			run.supervisionError != nil && now.Before(run.supervisionRetryAfter) {
			continue
		}
		var observationErr error
		if run.helperExit == nil {
			ready, err := linuxOwnerAnchorPIDFDReady(run.helperPIDFD)
			if err != nil {
				observationErr = fmt.Errorf("poll exact helper pidfd: %w", err)
			} else if !ready {
				continue
			} else {
				waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				err = run.waitHelper(waitContext)
				cancel()
				if err != nil {
					observationErr = fmt.Errorf("observe exact Linux owner helper death: %w", err)
				}
			}
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		var err error
		if run.helperExit != nil {
			err = anchor.ensureExitedHelperContained(stopContext, run)
		} else {
			run.helperDeathContainmentAttempted = true
			err = anchor.forceStopRunForShutdown(stopContext, run)
		}
		cancel()
		if err != nil {
			run.supervisionError = errors.Join(observationErr, fmt.Errorf("contain run after exact helper death: %w", err))
			retryWithoutNewKill := !run.leafKillAttempted && !run.leafKillCallStarted && !run.leafKillAcknowledged ||
				run.leafKillAcknowledged && !run.leafEmptyObserved || run.stopEvidence != nil
			if retryWithoutNewKill {
				run.helperDeathContainmentAttempted = false
				run.supervisionRetryAfter = time.Now().Add(linuxOwnerAnchorSupervisionRetry)
			} else {
				// A positive kill/empty observation without a complete run proof is
				// permanently unresolved in this in-memory Anchor lifetime. Never
				// retry the write or fail the owner unit and sweep sibling RunKeys.
				run.helperDeathContainmentAttempted = true
				run.supervisionTerminal = true
			}
			continue
		}
		run.helperDeathContainmentComplete = true
		run.helperDeathContainmentAttempted = true
		run.supervisionError = nil
		run.supervisionRetryAfter = time.Time{}
		run.supervisionTerminal = false
	}
	return nil
}

func linuxOwnerAnchorPIDFDReady(pidfd *os.File) (bool, error) {
	if pidfd == nil {
		return false, errors.New("Linux owner anchor helper pidfd is unavailable")
	}
	poll := [1]unix.PollFd{{Fd: int32(pidfd.Fd()), Events: unix.POLLIN}}
	for {
		poll[0].Revents = 0
		_, err := unix.Poll(poll[:], 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("poll exact Linux owner helper pidfd: %w", err)
		}
		if poll[0].Revents&unix.POLLNVAL != 0 {
			return false, errors.New("exact Linux owner helper pidfd became invalid")
		}
		return poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}

func (anchor *linuxOwnerAnchor) ensureExitedHelperContained(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("Linux owner anchor containment requires the exact helper exit/reap proof")
	}
	if run.helperDeathContainmentComplete {
		return nil
	}
	run.helperDeathContainmentAttempted = true
	if err := anchor.forceStopRunForShutdown(ctx, run); err != nil {
		run.supervisionError = err
		return err
	}
	run.helperDeathContainmentComplete = true
	run.helperDeathContainmentAttempted = true
	run.supervisionTerminal = false
	run.supervisionRetryAfter = time.Time{}
	run.supervisionError = nil
	return nil
}

func (anchor *linuxOwnerAnchor) verifyStopProducerFence(ctx context.Context, run *linuxOwnerAnchorRun, fence LinuxOwnerAnchorProducerFence) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit != nil ||
		run.stopLeafAttempted && (run.stopFence == nil || !run.stopFence.Equal(fence) || run.leafKillCallStarted ||
			run.leafKillAttempted || run.leafKillAcknowledged || run.stopEvidence != nil) ||
		run.helperPIDFD == nil || run.leaf == nil || fence.ValidateFor(run.grant, run.helper) != nil {
		return errors.New("Linux owner anchor StopLeaf producer fence is not live or exact")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		return fmt.Errorf("verify exact live helper at StopLeaf: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(run.helper.PID, run.helper.Identity, run.owner.AUID, run.owner.AGID, anchor.identity.PID, anchor.managerPath); err != nil {
		return fmt.Errorf("verify exact helper credentials at StopLeaf: %w", err)
	}
	if (run.gateIdentity == nil) != (fence.Gate == nil) ||
		run.gateIdentity != nil && (!run.gateIdentity.Equal(*fence.Gate) || run.gateUserNamespace != *fence.UserNamespace) {
		return errors.New("Linux owner anchor StopLeaf gate differs from the exact registered process")
	}
	if fence.UntrustedProcess != nil {
		child, exists := run.children[fence.UntrustedProcess.PID]
		if !exists || !child.identity.Equal(*fence.UntrustedProcess) || !child.creator.Equal(run.helper) ||
			child.role != "descendant" || child.pidfd == nil {
			return errors.New("failed-Prepare StopLeaf identity differs from its exact retained descendant registration")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), child.identity); err != nil {
			return fmt.Errorf("verify failed-Prepare child pidfd before anchor kill: %w", err)
		}
		credentials, err := readLinuxOwnerProcessCredentials(child.identity.PID)
		if err != nil || credentials.Parent != run.helper.PID {
			return fmt.Errorf("verify failed-Prepare child ancestry before anchor kill: %w", errOr(err, errors.New("parent differs from exact helper")))
		}
		if err := verifyLinuxOwnerRunChildCredentials(child.identity.PID, "gate", run.owner); err != nil {
			return fmt.Errorf("failed-Prepare child is not the exact unprivileged A process: %w", err)
		}
		if err := verifyLinuxOwnerRunChildMembership(child.identity.PID, "descendant", run.grant, run.leaf); err != nil {
			cgroupPath, pathErr := readLinuxOwnerProcessCgroupPath(child.identity.PID)
			if pathErr != nil || linuxOwnerCgroupPathWithin(cgroupPath, path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)) {
				return errors.Join(err, pathErr)
			}
			run.migrationObserved = true
		}
	}
	if run.gateReleaseAuthorized && !fence.ReleaseAttempted || fence.Released && !run.gateReleaseAuthorized {
		return errors.New("Linux owner anchor StopLeaf release fence differs from the one-use gate authorization")
	}
	if fence.Launcher != nil {
		launcher, exists := run.children[fence.Launcher.PID]
		if !exists || !launcher.identity.Equal(*fence.Launcher) || !launcher.creator.Equal(run.helper) ||
			launcher.role != "launcher" || launcher.pidfd == nil {
			return errors.New("Linux owner anchor StopLeaf launcher differs from its exact retained process pin")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(launcher.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("exact root launcher remains live at StopLeaf: %w", errOr(err, errors.New("launcher pidfd is not ready")))
		}
	} else {
		for _, child := range run.children {
			if err := ctx.Err(); err != nil {
				return err
			}
			if child.role == "launcher" {
				return errors.New("Linux owner anchor StopLeaf omitted its registered launcher")
			}
		}
	}
	if present, err := linuxOwnerAnchorPinnedLeafPresent(run); err != nil || !present {
		return fmt.Errorf("verify exact pinned leaf before StopLeaf: %w", errOr(err, errors.New("run leaf is absent")))
	}
	for _, child := range run.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if child.role == "launcher" {
			continue
		}
		if child.pidfd == nil {
			return errors.New("Linux owner anchor StopLeaf run child lacks an exact pidfd")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil {
			return fmt.Errorf("poll exact StopLeaf child pidfd: %w", err)
		}
		if ready {
			continue
		}
		if identity, err := ProcessIdentity(child.identity.PID); err != nil || identity != child.identity.Identity {
			return fmt.Errorf("verify live StopLeaf child identity: %w", errOr(err, errors.New("creation identity changed")))
		}
		switch child.role {
		case "gate":
			if run.gateIdentity == nil || !run.gateIdentity.Process.Equal(child.identity) {
				return errors.New("Linux owner anchor StopLeaf has an unbound gate child")
			}
			if fence.Released {
				if err := run.verifyAuthorizedGateExec(child.identity, true); err != nil {
					if looseErr := run.verifyAuthorizedGateExec(child.identity, false); looseErr != nil {
						return fmt.Errorf("helper reports RELEASEd without the exact B exec transition: %w", errors.Join(err, looseErr))
					}
					run.migrationObserved = true
				}
				continue
			}
			if run.gateReleaseAuthorized {
				if err := run.verifyAuthorizedGateExec(child.identity, true); err == nil {
					continue
				} else if looseErr := run.verifyAuthorizedGateExec(child.identity, false); looseErr == nil {
					run.migrationObserved = true
					continue
				}
			}
			if err := verifyLinuxOwnerRunChildCredentials(child.identity.PID, "gate", run.owner); err != nil {
				return err
			}
			if err := verifyLinuxOwnerAnchorGateNamespace(child.identity.PID, run.gateUserNamespace, run.owner); err != nil {
				return err
			}
			if err := verifyLinuxOwnerRunChildMembership(child.identity.PID, "gate", run.grant, run.leaf); err != nil {
				cgroupPath, pathErr := readLinuxOwnerProcessCgroupPath(child.identity.PID)
				if pathErr != nil || linuxOwnerCgroupPathWithin(cgroupPath, path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)) {
					return errors.Join(err, pathErr)
				}
				run.migrationObserved = true
			}
		case "native":
			if err := run.verifyAuthorizedGateExec(child.identity, true); err != nil {
				if looseErr := run.verifyAuthorizedGateExec(child.identity, false); looseErr != nil {
					return errors.Join(err, looseErr)
				}
				run.migrationObserved = true
			}
		case "descendant":
			if err := verifyLinuxOwnerRunChildCredentials(child.identity.PID, child.role, run.owner); err != nil {
				return err
			}
			if err := verifyLinuxOwnerRunChildMembership(child.identity.PID, child.role, run.grant, run.leaf); err != nil {
				cgroupPath, pathErr := readLinuxOwnerProcessCgroupPath(child.identity.PID)
				if pathErr != nil || linuxOwnerCgroupPathWithin(cgroupPath, path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)) {
					return errors.Join(err, pathErr)
				}
				run.migrationObserved = true
			}
		default:
			return errors.New("Linux owner anchor StopLeaf observed an unknown tracked child role")
		}
	}
	return anchor.verifyAnchorChildrenBeforeStopLeaf(ctx, run)
}

func (anchor *linuxOwnerAnchor) verifyNormalStopChildMembership(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	cgroupPath string,
) (bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || process.Validate() != nil || cgroupPath == "" {
		return false, errors.New("normal stop child membership check is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	if process.Equal(run.helper) {
		if cgroupPath != anchor.managerPath {
			return false, errors.New("exact helper is outside the pinned manager subgroup during StopLeaf")
		}
		return false, nil
	}
	var pidfd int
	if tracked, exists := run.children[process.PID]; exists {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return false, errors.New("anchor child differs from its exact current-run process pin")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			run.migrationObserved = true
			return false, fmt.Errorf("open exact anchor child pidfd before normal StopLeaf: %w", err)
		}
		defer unix.Close(opened)
		pidfd = opened
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return false, fmt.Errorf("classify exact anchor child before normal StopLeaf: %w", err)
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		run.migrationObserved = true
		return false, fmt.Errorf("recheck anchor child cgroup before normal StopLeaf: %w", errOr(err, errors.New("cgroup path changed")))
	}
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		if classification.Role == "launcher" {
			if cgroupPath != run.grant.Parent.Path {
				return false, errors.New("registered launcher left the exact manager subgroup before StopLeaf")
			}
		} else if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			return false, errors.New("registered current-run child left the exact run leaf before StopLeaf")
		}
		return false, nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			return false, errors.New("namespace-proven current-run child is outside the exact leaf before StopLeaf")
		}
		if _, exists := run.children[process.PID]; !exists {
			if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
				return false, fmt.Errorf("retain exact namespace-proven anchor child before StopLeaf: %w", err)
			}
		}
		return false, nil
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
		linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("different RunKey child is inside this exact run leaf before StopLeaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return false, nil
	default:
		run.migrationObserved = true
		return false, errors.New("anchor observed an unclassified process before normal StopLeaf")
	}
}

func (anchor *linuxOwnerAnchor) verifyAnchorChildrenBeforeStopLeaf(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helper.Validate() != nil || run.helperPIDFD == nil {
		return errors.New("normal StopLeaf anchor-child fence is incomplete")
	}
	pids, err := readLinuxOwnerProcessChildren(ctx, anchor.identity)
	if err != nil {
		return fmt.Errorf("read all-thread anchor child census before normal StopLeaf: %w", err)
	}
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("capture anchor child identity before normal StopLeaf: %w", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
		credentials, err := readLinuxOwnerProcessCredentials(pid)
		if err != nil || credentials.Parent != anchor.identity.PID {
			run.migrationObserved = true
			return fmt.Errorf("verify anchor-child ancestry before normal StopLeaf: %w", errOr(err, errors.New("parent PID differs")))
		}
		cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("read anchor child cgroup before normal StopLeaf: %w", err)
		}
		skip, err := anchor.verifyNormalStopChildMembership(ctx, run, process, cgroupPath)
		if err != nil {
			return fmt.Errorf("verify anchor-child RunKey membership before normal StopLeaf: %w", err)
		}
		if process.Equal(run.helper) {
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				return fmt.Errorf("recheck exact helper pidfd before normal StopLeaf: %w", err)
			}
			if actual, err := ProcessIdentity(run.helper.PID); err != nil || actual != run.helper.Identity {
				return fmt.Errorf("recheck exact helper identity before normal StopLeaf: %w", errOr(err, errors.New("creation identity differs")))
			}
			continue
		}
		if skip {
			continue
		}
		if tracked, exists := run.children[pid]; exists {
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
				return fmt.Errorf("verify exact current-run child pidfd before normal StopLeaf: %w", err)
			}
		}
		if actual, err := ProcessIdentity(pid); err != nil || actual != process.Identity {
			return fmt.Errorf("recheck anchor child identity before normal StopLeaf: %w", errOr(err, errors.New("creation identity differs")))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		return fmt.Errorf("recheck exact helper pidfd after normal StopLeaf child scan: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(run.helper.PID, run.helper.Identity, run.owner.AUID, run.owner.AGID,
		anchor.identity.PID, anchor.managerPath); err != nil {
		return fmt.Errorf("recheck exact helper credentials and manager subgroup after normal StopLeaf child scan: %w", err)
	}
	return nil
}

func (anchor *linuxOwnerAnchor) verifyNoUnknownRunProducer(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("Linux owner anchor takeover requires exact helper exit/reap evidence")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pids, err := readLinuxOwnerProcessChildren(ctx, anchor.identity)
	if err != nil {
		return fmt.Errorf("read all-thread anchor child census before takeover StopLeaf: %w", err)
	}
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("capture anchor child identity before takeover StopLeaf: %w", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
		credentials, err := readLinuxOwnerProcessCredentials(pid)
		if err != nil || credentials.Parent != anchor.identity.PID {
			run.migrationObserved = true
			return fmt.Errorf("verify anchor child ancestry before takeover StopLeaf: %w", errOr(err, errors.New("parent PID differs")))
		}
		if err := anchor.verifyTakeoverChildOrigin(ctx, run, process); err != nil {
			return err
		}
	}
	return nil
}

func (anchor *linuxOwnerAnchor) verifyTakeoverChildOrigin(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
) error {
	tracked, registered := run.children[process.PID]
	var pidfd int
	if registered {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return errors.New("anchor child differs from its exact run registration during takeover")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("open exact unregistered anchor-child pidfd during takeover: %w", err)
		}
		pidfd = opened
		defer unix.Close(pidfd)
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return fmt.Errorf("classify anchor child before takeover StopLeaf: %w", err)
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil {
		run.migrationObserved = true
		return fmt.Errorf("recheck anchor child cgroup during takeover: %w", err)
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		if classification.Role == "launcher" {
			ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
			if err != nil || !ready {
				return fmt.Errorf("registered launcher remains live at anchor takeover: %w", errOr(err, errors.New("pidfd is not ready")))
			}
			return nil
		}
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return nil
		}
		return nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return nil
		}
		if !registered {
			if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
				return fmt.Errorf("retain namespace-proven takeover child: %w", err)
			}
		}
		return nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
			return fmt.Errorf("retain pre-RegisterGate physical-only gate: %w", err)
		}
		return nil
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
		linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return errors.New("foreign RunKey process is inside this exact run leaf before takeover StopLeaf")
		}
		return nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return nil
	default:
		run.migrationObserved = true
		return errors.New("anchor observed an unclassified child before takeover StopLeaf")
	}
}

func (anchor *linuxOwnerAnchor) skipChildOwnedByAnotherRun(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	cgroupPath string,
) (bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || process.Validate() != nil || cgroupPath == "" {
		return false, errors.New("classify anchor child ownership requires an exact run and process identity")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var pidfd int
	if tracked, exists := run.children[process.PID]; exists {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return false, errors.New("anchor child differs from its exact current-run registration")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		opened, err := unix.PidfdOpen(process.PID, 0)
		if err != nil {
			return false, fmt.Errorf("open exact anchor child pidfd for cross-run classification: %w", err)
		}
		defer unix.Close(opened)
		pidfd = opened
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		run.migrationObserved = true
		return false, fmt.Errorf("classify anchor child RunKey origin: %w", err)
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		run.migrationObserved = true
		return false, fmt.Errorf("recheck anchor child cgroup for cross-run classification: %w", errOr(err, errors.New("cgroup path changed")))
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("anchor observed a different RunKey's child in this exact run leaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent, linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		return false, nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		return false, nil
	case linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		if linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return false, errors.New("another RunKey's physical-only gate is inside this exact run leaf")
		}
		return true, nil
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return false, errors.New("anchor observed a current-run process outside its exact run leaf")
	default:
		run.migrationObserved = true
		return false, errors.New("anchor observed an unclassified child during cross-run ownership check")
	}
}

func (anchor *linuxOwnerAnchor) verifyNoForeignRunProcessInLeaf(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || !run.grant.Anchor.Equal(anchor.identity) {
		return errors.New("foreign-run leaf membership check lacks exact anchor authority")
	}
	processes, err := readLinuxOwnerAnchorRunLeafProcessIDs(ctx, run.leaf, run.grant, run.owner.AUID)
	if err != nil {
		return fmt.Errorf("read bounded recursive pinned-leaf process set before cgroup.kill: %w", err)
	}
	pids := make([]int, 0, len(processes))
	for pid := range processes {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := ProcessIdentity(pid)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("capture exact pinned-leaf process identity before cgroup.kill: %w", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			run.migrationObserved = true
			return fmt.Errorf("open exact pinned-leaf process pidfd before cgroup.kill: %w", err)
		}
		classification, classifyErr := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
		if classifyErr != nil {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("classify exact pinned-leaf process before cgroup.kill: %w", classifyErr)
		}
		switch classification.Origin {
		case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
			if classification.Role == "launcher" {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return errors.New("registered root launcher is inside the exact run leaf before cgroup.kill")
			}
		case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
			beforePath, err := readLinuxOwnerProcessCgroupPath(pid)
			if err != nil {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return fmt.Errorf("recheck namespace-proven current-run process cgroup during leaf census: %w", err)
			}
			if !linuxOwnerCgroupPathWithin(beforePath, leafPath) {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				continue
			}
			if _, exists := run.children[pid]; !exists {
				if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
					_ = unix.Close(pidfd)
					run.migrationObserved = true
					return fmt.Errorf("retain exact namespace-proven leaf process before cgroup.kill: %w", err)
				}
			}
		case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
			if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
				_ = unix.Close(pidfd)
				run.migrationObserved = true
				return fmt.Errorf("retain pre-RegisterGate physical-only gate before cgroup.kill: %w", err)
			}
		case linuxOwnerAnchorRunChildOriginRegisteredForeign, linuxOwnerAnchorRunChildOriginOtherUserNSForeign,
			linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return errors.New("foreign RunKey process is inside this exact run leaf before cgroup.kill")
		case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			continue
		default:
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return errors.New("unattributable process is inside the exact run leaf before cgroup.kill")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process pidfd before cgroup.kill: %w", err)
		}
		if actual, err := ProcessIdentity(pid); err != nil || actual != process.Identity {
			_ = unix.Close(pidfd)
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process identity before cgroup.kill: %w", errOr(err, errors.New("creation identity differs")))
		}
		cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
		_ = unix.Close(pidfd)
		if err != nil || !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			run.migrationObserved = true
			return fmt.Errorf("recheck pinned-leaf process membership before cgroup.kill: %w", errOr(err, errors.New("process left the exact run leaf")))
		}
	}
	return nil
}

func (anchor *linuxOwnerAnchor) establishRunNoMigrationProof(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	fence *LinuxOwnerAnchorProducerFence,
) error {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || !run.grant.Anchor.Equal(anchor.identity) {
		return errors.New("NoMigrationObserved requires the exact retained Anchor and RunKey")
	}
	if fence != nil && fence.ValidateFor(run.grant, run.helper) != nil {
		return errors.New("NoMigrationObserved premise differs from the exact producer fence")
	}
	run.noMigrationProven = false
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(run.physicalOnlyChildren) != 0 {
		// The gate can be physically contained under its exact launcher-birth
		// premise, but without a registered GateIdentity it cannot participate
		// in a RunKey-complete StopReceipt.
		return nil
	}
	if run.migrationObserved {
		return nil
	}
	if run.gateIdentity == nil {
		if fence == nil || fence.Launcher == nil || fence.LauncherExit == nil ||
			fence.Gate != nil || fence.UserNamespace != nil || run.gateUserNamespaceFD != nil || run.gateImageFD != nil {
			// A helper-death takeover before RegisterGate has no authenticated
			// no-gate result. It remains physically containable only when the
			// earlier producer scans show no unknown process; it cannot issue a
			// run-bound stop receipt.
			return nil
		}
		if !fence.LauncherExit.ValidForOwnedDescendant(*fence.Launcher, run.helper, anchor.identity, nil) {
			return errors.New("pre-gate no-target proof lacks exact helper-owned launcher reap evidence")
		}
		if fence.GateOutcome != nil {
			if fence.UntrustedProcess != nil ||
				linuxgate.VerifyGateNeverProducedAuthenticator(run.grant.Secret, *fence.GateOutcome) != nil ||
				!fence.GateOutcome.Launcher.Equal(*fence.Launcher) {
				return errors.New("pre-gate no-gate migration premise differs from its exact authenticated launcher outcome")
			}
			launcherSeen := false
			for _, child := range run.children {
				if child.role != "launcher" || !child.identity.Equal(*fence.Launcher) ||
					!child.creator.Equal(run.helper) || child.pidfd == nil ||
					child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate no-target proof contains an unbound root launcher")
				}
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), child.identity); err != nil {
					return fmt.Errorf("verify pre-gate launcher pidfd: %w", err)
				}
				ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
				if err != nil || !ready {
					return fmt.Errorf("verify pre-gate launcher exit: %w", errOr(err, errors.New("pidfd is not ready")))
				}
				launcherSeen = true
			}
			if !launcherSeen {
				return errors.New("pre-gate no-target proof lacks its retained exact root launcher")
			}
			run.noMigrationProven = true
			return nil
		}
		if fence.UntrustedProcess == nil || run.gateReleaseAuthorized || fence.ReleaseAttempted || fence.Released {
			return nil
		}
		untrusted, exists := run.children[fence.UntrustedProcess.PID]
		if !exists || !untrusted.identity.Equal(*fence.UntrustedProcess) || untrusted.role != "descendant" ||
			!untrusted.creator.Equal(run.helper) || untrusted.pidfd == nil ||
			untrusted.classification.ValidateFor(run.grant, run.helper, untrusted.identity) != nil ||
			untrusted.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return errors.New("pre-gate failed-Prepare migration premise lacks its exact retained A child")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(untrusted.pidfd.Fd()), untrusted.identity); err != nil {
			return fmt.Errorf("verify failed-Prepare child pidfd for no-migration proof: %w", err)
		}
		if err := verifyLinuxOwnerRunChildCredentials(untrusted.identity.PID, "gate", run.owner); err != nil {
			return fmt.Errorf("verify failed-Prepare child remains host A: %w", err)
		}
		if !run.migrationObserved {
			if err := verifyLinuxOwnerRunChildMembership(untrusted.identity.PID, "descendant", run.grant, run.leaf); err != nil {
				return err
			}
		}
		launcherSeen, untrustedSeen := false, false
		for _, child := range run.children {
			switch child.role {
			case "launcher":
				if !child.identity.Equal(*fence.Launcher) || child.pidfd == nil ||
					child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate failed-Prepare proof contains an unbound root launcher")
				}
				launcherSeen = true
			case "descendant":
				if !child.identity.Equal(*fence.UntrustedProcess) || !child.creator.Equal(run.helper) ||
					child.pidfd == nil || child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
					child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
					return errors.New("pre-gate failed-Prepare proof contains an unbound helper descendant")
				}
				untrustedSeen = true
			default:
				return errors.New("pre-gate failed-Prepare proof contains a gate or native process")
			}
		}
		if !launcherSeen || !untrustedSeen {
			return errors.New("pre-gate failed-Prepare proof lacks its exact producer set")
		}
		run.noMigrationProven = true
		return nil
	}
	if run.gateIdentity.Validate() != nil || !run.gateIdentity.TrustedImage || !run.gateIdentity.BornInLeaf ||
		!run.gateIdentity.Cgroup.Equal(run.grant.Leaf) || !run.gateReadyAuthenticated || run.gateImageFD == nil {
		return errors.New("whole-leaf kill lacks the exact authenticated GateReady/image proof")
	}
	if err := run.verifyPinnedGateUserNamespace(); err != nil {
		return err
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(run.gateUserNamespaceFD)
	if err != nil || parentNamespace != anchor.userNamespace {
		return fmt.Errorf("verify exact gate user-namespace parent before cgroup.kill: %w", errOr(err, errors.New("parent namespace differs")))
	}
	var gateImageStat unix.Stat_t
	if err := unix.Fstat(int(run.gateImageFD.Fd()), &gateImageStat); err != nil ||
		gateImageStat.Mode&unix.S_IFMT != unix.S_IFREG || gateImageStat.Uid != 0 || gateImageStat.Gid != 0 ||
		gateImageStat.Mode&0022 != 0 || gateImageStat.Mode&0111 == 0 || gateImageStat.Nlink != 1 {
		return fmt.Errorf("verify exact pinned gate image before cgroup.kill: %w", errOr(err, errors.New("root-owned image identity differs")))
	}
	if fence != nil {
		if fence.Gate == nil || !fence.Gate.Equal(*run.gateIdentity) || fence.UserNamespace == nil ||
			*fence.UserNamespace != run.gateUserNamespace || fence.ValidateFor(run.grant, run.helper) != nil {
			return errors.New("normal StopLeaf migration premise differs from the exact helper GateReady fence")
		}
	}
	// The pinned trusted static gate passed its inherited-FD census before
	// GateReady, then enforced the fixed four-rights RELEASE allowlist, B-only
	// maps, no-new-privileges, and zero host capabilities before exec. The
	// Anchor rechecks B credentials/namespace and every present leaf member
	// above. No kernel snapshot can exclude an arbitrary concurrent UID-A
	// cgroup.procs migration; this proof depends on the provisioned trusted-A
	// birth-only placement contract. Any future A migration API must serialize
	// through this Anchor and record source/destination before its effect.
	run.noMigrationProven = true
	return nil
}

func validateLinuxOwnerAnchorCreatorChain(ctx context.Context, run *linuxOwnerAnchorRun, child linuxOwnerAnchorTrackedProcess) error {
	if ctx == nil || run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || child.identity.Validate() != nil ||
		child.creator.Validate() != nil || child.pidfd == nil || !linuxOwnerAnchorValidChildRole(child.role) ||
		child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
		child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent || child.classification.Role != child.role {
		return errors.New("RunKey creator chain has incomplete process identities")
	}
	current := child
	var visited [linuxOwnerAnchorMaxCreatorChainDepth]int
	visitedCount := 0
	for step := 0; step < linuxOwnerAnchorMaxCreatorChainDepth; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for index := 0; index < visitedCount; index++ {
			if visited[index] == current.identity.PID {
				return errors.New("RunKey child creator ancestry contains a cycle")
			}
		}
		visited[visitedCount] = current.identity.PID
		visitedCount++
		if current.creator.Equal(run.helper) {
			if run.helperExit != nil {
				if !run.helperExit.ValidFor(run.helper, run.grant.Anchor) {
					return errors.New("RunKey helper creator exit differs from its exact anchor wait")
				}
				return nil
			}
			if run.helperPIDFD == nil {
				return errors.New("live RunKey helper creator has no retained pidfd")
			}
			ready, err := linuxOwnerAnchorPIDFDReady(run.helperPIDFD)
			if err != nil || ready {
				return fmt.Errorf("verify live RunKey helper creator: %w", errOr(err, errors.New("pidfd is not live")))
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				return fmt.Errorf("verify live RunKey helper creator pidfd: %w", err)
			}
			if err := verifyLinuxOwnerAProcess(run.helper.PID, run.helper.Identity, run.owner.AUID, run.owner.AGID,
				run.grant.Anchor.PID, run.grant.Parent.Path); err != nil {
				return fmt.Errorf("verify live RunKey helper creator ancestry: %w", err)
			}
			return nil
		}
		parent, exists := run.children[current.creator.PID]
		if !exists || !parent.identity.Equal(current.creator) || !linuxOwnerAnchorValidChildRole(parent.role) {
			return errors.New("RunKey child creator is not the exact retained parent identity")
		}
		if evidence, reaped := run.reaped[parent.identity.PID]; reaped {
			if !evidence.Process.Equal(parent.identity) || evidence.ValidateFor(run.grant, run.helper, parent.identity, parent.role) != nil {
				return errors.New("RunKey child creator has invalid exact anchor wait/reap evidence")
			}
		} else {
			if parent.pidfd == nil {
				return errors.New("unreaped RunKey child creator has no retained exact pidfd")
			}
			ready, err := linuxOwnerAnchorPIDFDReady(parent.pidfd)
			if err != nil {
				return fmt.Errorf("poll retained RunKey child creator pidfd: %w", err)
			}
			if ready {
				// The retained pidfd and stored creator identity remain the exact
				// historical edge even if the parent has exited and been reaped
				// outside the anchor's wait domain.
				flags, err := unix.FcntlInt(parent.pidfd.Fd(), unix.F_GETFD, 0)
				if err != nil || flags&unix.FD_CLOEXEC == 0 {
					return errors.Join(errors.New("exited RunKey child creator pidfd is not a retained close-on-exec descriptor"), err)
				}
			} else {
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(parent.pidfd.Fd()), parent.identity); err != nil {
					return fmt.Errorf("verify live RunKey child creator pidfd: %w", err)
				}
				if identity, err := ProcessIdentity(parent.identity.PID); err != nil || identity != parent.identity.Identity {
					return fmt.Errorf("verify live RunKey child creator identity: %w", errOr(err, errors.New("creation identity differs")))
				}
				if err := verifyLinuxOwnerRunChildCredentials(parent.identity.PID, parent.role, run.owner); err != nil {
					return fmt.Errorf("verify live RunKey child creator credentials: %w", err)
				}
				if !run.leafRemovedObserved {
					if err := verifyLinuxOwnerRunChildMembership(parent.identity.PID, parent.role, run.grant, run.leaf); err != nil {
						return fmt.Errorf("verify live RunKey child creator membership: %w", err)
					}
				}
			}
		}
		current = parent
	}
	return errors.New("RunKey child creator ancestry exceeds its bounded depth")
}

func (anchor *linuxOwnerAnchor) stopRunLeaf(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	fence *LinuxOwnerAnchorProducerFence,
) (LinuxOwnerAnchorStopEvidence, error) {
	if anchor == nil || ctx == nil || run == nil || run.retired || run.leaf == nil || run.helperPIDFD == nil || run.grant.Validate() != nil {
		return LinuxOwnerAnchorStopEvidence{}, ErrLinuxCgroupV2Unresolved
	}
	if err := ctx.Err(); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	preWriteRetry := run.stopLeafAttempted && !run.leafKillAttempted && !run.leafKillAcknowledged && !run.leafKillCallStarted
	if fence != nil && (!run.stopLeafAttempted || preWriteRetry) {
		if err := anchor.verifyStopProducerFence(ctx, run, *fence); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
	} else if fence == nil && (run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity)) {
		return LinuxOwnerAnchorStopEvidence{}, errors.New("anchor takeover StopLeaf lacks exact helper exit/reap evidence")
	}
	if run.stopLeafAttempted {
		cached, err := run.cachedStopLeaf(fence)
		if err != nil {
			if run.leafKillAttempted && !run.leafKillAcknowledged {
				if emptyErr := run.leaf.WaitEmpty(ctx); emptyErr == nil {
					run.leafEmptyObserved = true
				}
			}
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if cached != nil {
			if err := anchor.remeasureStopLeaf(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
			return *cached, nil
		}
		if preWriteRetry {
			if fence == nil {
				if err := anchor.fenceRunLaunchers(ctx, run); err != nil {
					return LinuxOwnerAnchorStopEvidence{}, err
				}
			}
			if err := anchor.verifyNoForeignRunProcessInLeaf(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
			if err := anchor.establishRunNoMigrationProof(ctx, run, fence); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
		}
	} else {
		if fence == nil {
			if err := anchor.verifyNoUnknownRunProducer(ctx, run); err != nil {
				return LinuxOwnerAnchorStopEvidence{}, err
			}
		}
		if err := anchor.verifyNoForeignRunProcessInLeaf(ctx, run); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if err := anchor.establishRunNoMigrationProof(ctx, run, fence); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		if err := ctx.Err(); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, err
		}
		run.stopLeafAttempted = true
		run.stopRequested = true
		if fence != nil {
			copy := fence.Clone()
			run.stopFence = &copy
		}
	}
	if !run.leafKillAcknowledged {
		if run.leafKillAttempted {
			return LinuxOwnerAnchorStopEvidence{}, errors.New("previous cgroup.kill write result is unknown; no retry is permitted")
		}
		if err := run.leaf.VerifyKillControl(); err != nil {
			return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("verify exact cgroup.kill control before one-use StopLeaf: %w", err)
		}
		if err := run.writeCgroupKillOnce(ctx, run.leaf.Kill); err != nil {
			if run.leafKillAttempted {
				if emptyErr := run.leaf.WaitEmpty(ctx); emptyErr == nil {
					run.leafEmptyObserved = true
				}
			}
			return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("anchor cgroup.kill attempt is not positively acknowledged and cannot be replayed: %w", err)
		}
	}
	if err := run.leaf.WaitEmpty(ctx); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("wait for exact recursive leaf empty after anchor cgroup.kill: %w", err)
	}
	run.leafEmptyObserved = true
	if _, err := linuxOwnerAnchorPinnedLeafPresent(run); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("remeasure exact pinned leaf after anchor cgroup.kill: %w", err)
	}
	evidence := makeLinuxOwnerAnchorStopEvidence(run, anchor.identity)
	if err := evidence.ValidateFor(run.grant, run.helper, anchor.identity, nil, false, run.gateUserNamespaceFD); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	copy := evidence.Clone()
	run.stopEvidence = &copy
	return evidence, nil
}

func (run *linuxOwnerAnchorRun) writeCgroupKillOnce(ctx context.Context, write func(context.Context) error) error {
	if ctx == nil || write == nil || run == nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, errors.New("anchor cgroup.kill operation is incomplete"))
	}
	if run.leafKillAcknowledged {
		return nil
	}
	if run.leafKillCallStarted {
		if run.leafKillAttempted {
			return errors.New("anchor cgroup.kill write outcome is unknown and cannot be replayed")
		}
		return errors.New("anchor cgroup.kill call was already attempted and cannot be replayed")
	}
	run.leafKillCallStarted = true
	err := write(ctx)
	if err != nil {
		if errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
			run.leafKillCallStarted = false
		} else {
			run.leafKillAttempted = true
		}
		return err
	}
	run.leafKillAttempted = true
	run.leafKillAcknowledged = true
	return nil
}

func (run *linuxOwnerAnchorRun) cachedStopLeaf(fence *LinuxOwnerAnchorProducerFence) (*LinuxOwnerAnchorStopEvidence, error) {
	if run == nil || !run.stopLeafAttempted {
		return nil, errors.New("anchor StopLeaf cache lookup has no recorded one-use attempt")
	}
	if fence != nil && (run.stopFence == nil || !run.stopFence.Equal(*fence)) {
		return nil, errors.New("StopLeaf replay differs from its exact one-use producer fence")
	}
	if run.stopEvidence != nil {
		if !run.leafKillAttempted || !run.leafKillAcknowledged {
			return nil, errors.New("cached anchor StopLeaf evidence lacks a positive write acknowledgement")
		}
		copy := run.stopEvidence.Clone()
		return &copy, nil
	}
	if !run.leafKillAcknowledged {
		if run.leafKillAttempted {
			return nil, errors.New("prior anchor cgroup.kill write result is unknown; no retry is permitted")
		}
		if run.leafKillCallStarted {
			return nil, errors.New("anchor cgroup.kill write attempt is still unresolved")
		}
		// No syscall was attempted. The caller must repeat the exact producer
		// fence or takeover scan before the first physical write.
		return nil, nil
	}
	return nil, nil
}

func (anchor *linuxOwnerAnchor) fenceRunLaunchers(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("anchor launcher fencing requires exact helper exit/reap evidence")
	}
	for _, child := range run.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if child.role != "launcher" {
			continue
		}
		if child.pidfd == nil {
			return errors.New("registered root launcher lacks its exact pidfd during anchor takeover")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil {
			return fmt.Errorf("poll exact root launcher before takeover: %w", err)
		}
		if ready {
			continue
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), child.identity); err != nil {
			return fmt.Errorf("verify live root launcher before takeover: %w", err)
		}
		if err := unix.PidfdSendSignal(int(child.pidfd.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("signal exact root launcher during takeover: %w", err)
		}
		if err := waitLinuxOwnerAnchorPIDFDReady(ctx, child.pidfd); err != nil {
			return fmt.Errorf("wait for exact root launcher fence during takeover: %w", err)
		}
	}
	return anchor.verifyNoUnknownRunProducer(ctx, run)
}

func waitLinuxOwnerAnchorPIDFDReady(ctx context.Context, pidfd *os.File) error {
	if ctx == nil || pidfd == nil {
		return errors.New("wait for exact process pidfd requires a live context and pidfd")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready, err := linuxOwnerAnchorPIDFDReady(pidfd)
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(linuxOwnerAnchorWaitPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (anchor *linuxOwnerAnchor) remeasureStopLeaf(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.stopEvidence == nil || run.leaf == nil ||
		!run.leafKillAttempted || !run.leafKillAcknowledged || !run.stopEvidence.KillAcknowledged || run.migrationObserved {
		return errors.New("anchor StopLeaf replay lacks a cached positive cgroup.kill acknowledgement")
	}
	if err := verifyLinuxOwnerAnchorStopEvidenceNamespacePin(*run.stopEvidence, run.gateUserNamespaceFD); err != nil {
		return err
	}
	if run.stopEvidence.Gate != nil {
		if err := run.verifyPinnedGateUserNamespace(); err != nil {
			return err
		}
	}
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return fmt.Errorf("remeasure exact pinned leaf identity on StopLeaf replay: %w", err)
	}
	if present {
		if err := run.leaf.WaitEmpty(ctx); err != nil {
			return fmt.Errorf("remeasure recursive empty leaf on StopLeaf replay: %w", err)
		}
	}
	// linuxOwnerAnchorPinnedLeafPresent treats exact absence under the retained
	// parent FD as rmdir-backed emptiness; it also checks the detached FD's
	// populated state when that kernel observation remains available.
	return nil
}

func (anchor *linuxOwnerAnchor) finalizeRunStopEvidence(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	processes []LinuxOwnerAnchorReapEvidence,
) (LinuxOwnerAnchorStopEvidence, error) {
	if anchor == nil || ctx == nil || run == nil || run.stopEvidence == nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) || run.migrationObserved {
		return LinuxOwnerAnchorStopEvidence{}, errors.New("anchor run stop proof lacks the exact helper wait/reap and cached kill")
	}
	if err := anchor.remeasureStopLeaf(ctx, run); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("recheck cached anchor kill/empty proof before run-reap response: %w", err)
	}
	evidence := run.stopEvidence.Clone()
	helperExit := *cloneLinuxOwnerAnchorExitProof(run.helperExit)
	evidence.HelperExit = &helperExit
	evidence.DescendantsReaped = true
	evidence.ReapCount = uint64(len(processes))
	encoded, err := json.Marshal(processes)
	if err != nil {
		return LinuxOwnerAnchorStopEvidence{}, fmt.Errorf("encode exact anchor run reaps: %w", err)
	}
	digest := sha256.Sum256(encoded)
	evidence.ReapDigest = hex.EncodeToString(digest[:])
	if err := evidence.ValidateFor(run.grant, run.helper, anchor.identity, processes, true, run.gateUserNamespaceFD); err != nil {
		return LinuxOwnerAnchorStopEvidence{}, err
	}
	run.stopEvidence = new(LinuxOwnerAnchorStopEvidence)
	*run.stopEvidence = evidence.Clone()
	return evidence, nil
}

func makeLinuxOwnerAnchorStopEvidence(run *linuxOwnerAnchorRun, anchor authority.LinuxCgroupProcessIdentity) LinuxOwnerAnchorStopEvidence {
	evidence := LinuxOwnerAnchorStopEvidence{
		Version: linuxOwnerAnchorStopEvidenceVersion, RunKey: run.grant.RunKey, Fence: run.grant.Fence,
		LaunchToken: run.grant.LaunchToken, Parent: run.grant.Parent, Leaf: run.grant.Leaf,
		Anchor: anchor, Helper: run.helper, PinnedLeafFD: true, KillAttempted: run.leafKillAttempted,
		KillAcknowledged: run.leafKillAcknowledged, CgroupPopulatedZero: run.leafEmptyObserved,
		NoMigrationObserved: run.noMigrationProven && !run.migrationObserved, ProducerFenced: true,
		GateReleaseAuthorized: run.gateReleaseAuthorized,
	}
	if run.stopFence != nil {
		fence := run.stopFence.Clone()
		evidence.ProducerFence = &fence
		evidence.Launcher = cloneLinuxOwnerProcessIdentity(fence.Launcher)
		evidence.Gate = cloneLinuxOwnerGateIdentity(fence.Gate)
		evidence.UntrustedProcess = cloneLinuxOwnerProcessIdentity(fence.UntrustedProcess)
		if fence.UserNamespace != nil {
			copy := *fence.UserNamespace
			evidence.UserNamespace = &copy
		}
		if fence.GateOutcome != nil {
			copy := *fence.GateOutcome
			evidence.GateOutcome = &copy
		}
	} else {
		evidence.HelperExit = cloneLinuxOwnerAnchorExitProof(run.helperExit)
		if run.gateIdentity != nil {
			gate := *run.gateIdentity
			evidence.Gate = &gate
			userNamespace := run.gateUserNamespace
			evidence.UserNamespace = &userNamespace
		}
		for _, child := range run.children {
			if child.role == "launcher" {
				launcher := child.identity
				evidence.Launcher = &launcher
				break
			}
		}
	}
	return evidence
}

func (anchor *linuxOwnerAnchor) forceStopRunForShutdown(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if ctx == nil || run == nil || run.leaf == nil || run.helperPIDFD == nil || run.grant.Validate() != nil {
		return ErrLinuxCgroupV2Unresolved
	}
	if run.helperExit == nil {
		var signalErr error
		if err := unix.PidfdSendSignal(int(run.helperPIDFD.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			signalErr = fmt.Errorf("signal exact helper during bounded anchor shutdown: %w", err)
		}
		if err := run.waitHelper(ctx); err != nil {
			return errors.Join(signalErr, fmt.Errorf("wait/reap exact helper during bounded anchor shutdown: %w", err))
		}
	}
	if err := anchor.fenceRunLaunchers(ctx, run); err != nil {
		return fmt.Errorf("fence exact run launcher before anchor-owned cgroup.kill: %w", err)
	}
	stopContext, cancel := context.WithTimeout(ctx, linuxOwnerAnchorWaitTimeout)
	_, stopErr := anchor.stopRunLeaf(stopContext, run, nil)
	cancel()
	if stopErr != nil {
		var reapErr error
		if run.leafEmptyObserved && run.helperExit != nil {
			_, reapErr = anchor.reapRunChildren(ctx, run)
		}
		return errors.Join(fmt.Errorf("anchor-owned cgroup.kill/empty evidence is unresolved: %w", stopErr), reapErr)
	}
	processes, err := anchor.reapRunChildren(ctx, run)
	if err != nil {
		return fmt.Errorf("reap exact adopted run children after anchor-owned cgroup.kill: %w", err)
	}
	finalEvidence, err := anchor.finalizeRunStopEvidence(ctx, run, processes)
	if err != nil {
		return fmt.Errorf("finalize exact anchor stop/reap evidence: %w", err)
	}
	if err := finalEvidence.ValidateFor(run.grant, run.helper, anchor.identity, processes, true, run.gateUserNamespaceFD); err != nil {
		return fmt.Errorf("validate final anchor stop/reap evidence: %w", err)
	}
	return nil
}

func (anchor *linuxOwnerAnchor) serveConnection(connection *net.UnixConn) (detached bool) {
	defer func() {
		if !detached {
			_ = connection.Close()
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorRequestTimeout)); err != nil {
		return
	}
	peer, err := linuxOwnerAnchorPeerFromConn(connection, anchor.owner)
	if err != nil || peer.PID == anchor.identity.PID {
		return
	}
	request, receivedFDs, err := readLinuxOwnerAnchorRequest(connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(receivedFDs)
		return
	}
	defer closeLinuxOwnerAnchorFDs(receivedFDs)
	if request.Version != linuxOwnerAnchorProtocolVersion || request.ClientPID != peer.PID || request.ClientIdentity != peer.Identity {
		return
	}
	if request.Operation != linuxOwnerAnchorOperationCloneCleanup &&
		(request.CloneGrant != nil || request.Cleanup != nil || request.TargetFDPresent) {
		return
	}
	gateOperation := request.Operation == linuxOwnerAnchorOperationRegisterGate || request.Operation == linuxOwnerAnchorOperationAuthorizeGate
	stopLeafOperation := request.Operation == linuxOwnerAnchorOperationStopLeaf
	classifyChildOperation := request.Operation == linuxOwnerAnchorOperationClassifyChild
	if !gateOperation && (request.Gate != nil || request.UserNamespace != nil) {
		return
	}
	if !stopLeafOperation && request.StopFence != nil {
		return
	}
	if anchor.stopping && (request.Operation == "identity" || request.Operation == linuxOwnerAnchorOperationStart ||
		request.Operation == "register-process" || gateOperation || stopLeafOperation || classifyChildOperation) {
		anchor.writeResponse(connection, request, "anchor_stopping", nil, nil)
		return
	}
	if request.Operation == "identity" {
		if linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
			return
		}
		if len(receivedFDs) != 0 || request.Grant != (LinuxOwnerAnchorGrant{}) || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil || request.CloneGrant != nil || request.Cleanup != nil || request.TargetFDPresent ||
			request.RunKey != (LinuxOwnerAnchorRunKey{}) || request.Fence != (protocol.Fence{}) {
			return
		}
		if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
			return
		}
		if err := anchor.parent.verifyPinnedParent(); err != nil {
			return
		}
		anchor.writeResponse(connection, request, "ready", nil, nil)
		return
	}
	if request.Operation == linuxOwnerAnchorOperationRetireCleared || request.Operation == linuxOwnerAnchorOperationRetireAborted {
		validProofShape := request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil ||
			request.ReleaseProof == nil && request.PreGateReleaseProof != nil && request.AbortProof == nil ||
			request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof != nil
		if len(receivedFDs) != 0 || request.Grant != (LinuxOwnerAnchorGrant{}) ||
			request.Helper == nil || request.Helper.Validate() != nil || request.RunKey.Generation <= 0 ||
			request.Fence.Generation != request.RunKey.Generation || !validLinuxOwnerAnchorRunID(request.RunKey.RunID) ||
			!validProofShape || request.Operation == linuxOwnerAnchorOperationRetireAborted && request.AbortProof == nil ||
			request.Operation == linuxOwnerAnchorOperationRetireCleared && request.AbortProof != nil ||
			linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
			return
		}
		if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorWaitTimeout)); err != nil {
			return
		}
		if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
			return
		}
		if err := anchor.parent.verifyPinnedParent(); err != nil {
			return
		}
		if request.Operation == linuxOwnerAnchorOperationRetireCleared {
			anchor.handleRetireClearedRun(connection, request)
		} else {
			anchor.handleRetireAbortedHelper(connection, request)
		}
		return
	}
	if request.Operation == linuxOwnerAnchorOperationCloneCleanup {
		detached = anchor.serveCloneCleanup(connection, peer, request, receivedFDs)
		return
	}
	if request.Grant.Validate() != nil || request.Grant.OwnerUID != anchor.owner.AUID || request.Grant.LauncherUID != 0 ||
		!request.Grant.Anchor.Equal(anchor.identity) ||
		!request.Grant.Parent.Equal(anchor.parent.Identity()) {
		return
	}
	helperPeer := false
	if (request.Operation == "register-process" || gateOperation || stopLeafOperation || classifyChildOperation) && request.Helper != nil {
		run := anchor.runs[request.Grant.RunKey]
		helperPeer = run != nil && run.grant.Equal(request.Grant) && run.helper.Equal(*request.Helper) &&
			peer.PID == run.helper.PID && peer.Identity == run.helper.Identity
	}
	if linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) && !helperPeer {
		return
	}
	operationTimeout := linuxOwnerAnchorRequestTimeout
	if request.Operation == "wait-reap" || request.Operation == linuxOwnerAnchorOperationReapRun ||
		request.Operation == linuxOwnerAnchorOperationWaitHelper || request.Operation == linuxOwnerAnchorOperationLookup {
		operationTimeout = linuxOwnerAnchorWaitTimeout
	}
	if stopLeafOperation || classifyChildOperation {
		operationTimeout = linuxOwnerAnchorWaitTimeout
	}
	if err := connection.SetDeadline(time.Now().Add(operationTimeout)); err != nil {
		return
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
		return
	}
	if err := anchor.parent.verifyPinnedParent(); err != nil {
		return
	}
	switch request.Operation {
	case linuxOwnerAnchorOperationStart:
		if len(receivedFDs) != 2 || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		if anchor.stopping {
			anchor.writeResponse(connection, request, "anchor_stopping", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorLeafFD(receivedFDs[0], request.Grant.Leaf, anchor.owner.AUID); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorOwnerPipeReadFD(receivedFDs[1]); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		if existing, exists := anchor.runs[request.Grant.RunKey]; exists {
			if existing == nil || !existing.grant.Equal(request.Grant) {
				anchor.writeResponse(connection, request, "run_unresolved", nil, nil)
				return
			}
			anchor.writeResponse(connection, request, "already_started", &existing.helper, nil)
			return
		}
		var witnessForRun *linuxOwnerAnchorWitnessConfig
		if anchor.witness != nil && anchor.witness.RunID == request.Grant.RunKey.RunID &&
			anchor.witness.Generation == request.Grant.RunKey.Generation {
			copy := *anchor.witness
			witnessForRun = &copy
		}
		ownerProcess := authority.LinuxCgroupProcessIdentity{PID: peer.PID, Identity: peer.Identity}
		beforeFork := func() error { return anchor.recordHelperForkMayStart(request.Grant.RunKey) }
		run, err := startLinuxOwnerAnchorHelperV2(
			anchor, request.Grant, ownerProcess, receivedFDs[0], receivedFDs[1], witnessForRun, beforeFork,
		)
		if run != nil {
			anchor.runs[request.Grant.RunKey] = run
		}
		if err != nil || run == nil {
			status := "request_rejected"
			if _, forkMayHaveOccurred := anchor.runs[request.Grant.RunKey]; forkMayHaveOccurred {
				status = "start_unresolved"
			}
			anchor.writeResponse(connection, request, status, nil, nil)
			return
		}
		helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "start_unresolved", &run.helper, nil)
			return
		}
		anchor.writeResponseWithFDs(connection, request, "started", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		_ = parentFD.Close()
	case linuxOwnerAnchorOperationAttach:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if run.retired {
			anchor.writeResponse(connection, request, "retired", &run.helper, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
			anchor.writeResponse(connection, request, "helper_lost", &run.helper, nil)
			return
		}
		helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = unix.Close(helperFD)
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "attach_unresolved", &run.helper, nil)
			return
		}
		anchor.writeResponseWithFDs(connection, request, "attached", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
		_ = unix.Close(helperFD)
		_ = leafFD.Close()
		_ = parentFD.Close()
	case linuxOwnerAnchorOperationLookup:
		if len(receivedFDs) != 0 || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || run.helper.Validate() != nil {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if run.retired {
			anchor.writeResponse(connection, request, "retired", &run.helper, nil)
			return
		}
		helperExited := run.helperExit != nil
		if !helperExited {
			if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
				waitErr := run.waitHelper(waitContext)
				cancel()
				if waitErr != nil {
					anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
					return
				}
				helperExited = true
			}
		}
		leafFD, err := run.leaf.DuplicateFD()
		if err != nil {
			anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
			return
		}
		parentFD, err := anchor.parent.DuplicateFD()
		if err != nil {
			_ = leafFD.Close()
			anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
			return
		}
		if helperExited {
			anchor.writeResponseWithRunPins(connection, request, "helper_exited", &run.helper, run.helperExit, int(leafFD.Fd()), int(parentFD.Fd()))
		} else {
			helperFD, err := unix.FcntlInt(run.helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			if err != nil {
				_ = leafFD.Close()
				_ = parentFD.Close()
				anchor.writeResponse(connection, request, "lookup_unresolved", &run.helper, nil)
				return
			}
			anchor.writeResponseWithFDs(connection, request, "attached", &run.helper, nil, helperFD, int(leafFD.Fd()), int(parentFD.Fd()))
			_ = unix.Close(helperFD)
		}
		_ = leafFD.Close()
		_ = parentFD.Close()
	case "register-process":
		if len(receivedFDs) != 1 || request.Helper == nil || request.Creator == nil || request.Process == nil ||
			request.Gate != nil || request.UserNamespace != nil ||
			(request.Role != "launcher" && request.Role != "native" && request.Role != "descendant") {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := run.registerChild(*request.Process, *request.Creator, request.Role, receivedFDs[0]); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "registered", nil, nil)
	case linuxOwnerAnchorOperationClassifyChild:
		if len(receivedFDs) != 1 || !validLinuxOwnerAnchorChildOriginRequest(request) || !helperPeer {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(receivedFDs[0], *request.Process); err != nil {
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		credentials, err := readLinuxOwnerProcessCredentials(request.Process.PID)
		if err != nil || credentials.Parent != run.helper.PID {
			run.migrationObserved = true
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		classifyContext, cancel := context.WithTimeout(context.Background(), operationTimeout)
		classification, err := anchor.classifyRunChildOrigin(classifyContext, run, *request.Process, receivedFDs[0])
		cancel()
		credentials, parentErr := readLinuxOwnerProcessCredentials(request.Process.PID)
		if err != nil || parentErr != nil || credentials.Parent != run.helper.PID {
			run.migrationObserved = true
			anchor.writeResponseWithChildClassification(connection, request, "origin_unproven", linuxOwnerAnchorUnknownChildClassification(run, *request.Process))
			return
		}
		anchor.writeResponseWithChildClassification(connection, request, "classified", classification)
	case linuxOwnerAnchorOperationRegisterGate:
		if len(receivedFDs) != 1 || !validLinuxOwnerAnchorGateRequest(request) || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		for key, other := range anchor.runs {
			if key == request.Grant.RunKey || other == nil || other.retired || other.gateUserNamespaceFD == nil {
				continue
			}
			otherNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(other.gateUserNamespaceFD)
			if err != nil || otherNamespace == *request.UserNamespace {
				anchor.writeResponse(connection, request, "request_rejected", nil, nil)
				return
			}
		}
		if err := run.registerGate(*request.Gate, *request.UserNamespace, receivedFDs[0], anchor.userNamespaceFD); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "registered", nil, nil)
	case linuxOwnerAnchorOperationAuthorizeGate:
		if len(receivedFDs) != 0 || !validLinuxOwnerAnchorGateRequest(request) || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		if err := run.authorizeGateRelease(*request.Gate, *request.UserNamespace); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "release_authorized", nil, nil)
	case linuxOwnerAnchorOperationStopLeaf:
		if len(receivedFDs) != 0 || !validLinuxOwnerAnchorStopLeafRequest(request) {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) || !helperPeer {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		stopEvidence, err := anchor.stopRunLeaf(stopContext, run, request.StopFence)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		anchor.writeResponseWithStopEvidence(connection, request, "stopped", stopEvidence)
	case "wait-reap":
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process == nil || !linuxOwnerAnchorValidChildRole(request.Role) {
			return
		}
		run := anchor.runs[request.Grant.RunKey]
		if run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		tracked, registered := run.children[request.Process.PID]
		if !registered || !tracked.identity.Equal(*request.Process) || tracked.role != request.Role {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		if err := anchor.ensureExitedHelperContained(waitContext, run); err != nil {
			cancel()
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		evidence, err := run.reapChild(waitContext, anchor.identity, *request.Process, request.Role)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
			return
		}
		anchor.writeResponse(connection, request, "reaped", nil, &evidence)
	case linuxOwnerAnchorOperationReapRun:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" || request.AfterPID < 0 {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		if err := anchor.ensureExitedHelperContained(waitContext, run); err != nil {
			cancel()
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		evidence, err := anchor.reapRunChildren(waitContext, run)
		if err != nil {
			cancel()
			anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
			return
		}
		stopEvidence, err := anchor.finalizeRunStopEvidence(waitContext, run, evidence)
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "stop_unproven", nil, nil)
			return
		}
		filtered := make([]LinuxOwnerAnchorReapEvidence, 0, linuxOwnerAnchorReapPageSize)
		for _, item := range evidence {
			if item.Process.PID > request.AfterPID && len(filtered) < linuxOwnerAnchorReapPageSize {
				filtered = append(filtered, item)
			}
		}
		more := false
		nextPID := 0
		if len(filtered) > 0 {
			nextPID = filtered[len(filtered)-1].Process.PID
			for _, item := range evidence {
				if item.Process.PID > nextPID {
					more = true
					break
				}
			}
		}
		anchor.writeResponseWithReapEvidence(connection, request, "reaped", filtered, nextPID, more, stopEvidence)
	case linuxOwnerAnchorOperationWaitHelper:
		if len(receivedFDs) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" {
			return
		}
		run, exists := anchor.runs[request.Grant.RunKey]
		if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
			anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
			return
		}
		waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		if run.helperExit == nil {
			if err := run.waitHelper(waitContext); err != nil {
				cancel()
				anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
				return
			}
		}
		cancel()
		anchor.writeResponseWithHelperExit(connection, request, *run.helperExit)
	case linuxOwnerAnchorOperationRetire:
		anchor.handleRetireHelper(connection, request, receivedFDs)
	default:
		return
	}
	return
}

func (anchor *linuxOwnerAnchor) recordHelperForkMayStart(key LinuxOwnerAnchorRunKey) error {
	if anchor == nil || anchor.runs == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 {
		return errors.New("Linux owner anchor pre-fork tombstone identity is invalid")
	}
	if _, exists := anchor.runs[key]; exists {
		return ErrLinuxOwnerAnchorHelperAlreadyStarted
	}
	// This callback runs immediately before cmd.Start. Any later uncertain
	// outcome keeps the tombstone; pre-fork validation failures stay retryable.
	anchor.runs[key] = nil
	return nil
}

func validLinuxOwnerAnchorChildOriginRequest(request linuxOwnerAnchorRequest) bool {
	return request.Operation == linuxOwnerAnchorOperationClassifyChild && request.Grant.Validate() == nil &&
		request.Helper != nil && request.Helper.Validate() == nil && request.Process != nil && request.Process.Validate() == nil &&
		request.Gate == nil && request.UserNamespace == nil && request.StopFence == nil && request.Creator == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

func linuxOwnerAnchorUnknownChildClassification(run *linuxOwnerAnchorRun, process authority.LinuxCgroupProcessIdentity) linuxOwnerAnchorRunChildClassification {
	if run == nil {
		return linuxOwnerAnchorRunChildClassification{}
	}
	return linuxOwnerAnchorRunChildClassification{
		Version: linuxOwnerAnchorRunChildClassificationVersion,
		RunKey:  run.grant.RunKey, Fence: run.grant.Fence, LaunchToken: run.grant.LaunchToken,
		Helper: run.helper, Process: process, Origin: linuxOwnerAnchorRunChildOriginUnknown,
	}
}

func validLinuxOwnerAnchorGateRequest(request linuxOwnerAnchorRequest) bool {
	return (request.Operation == linuxOwnerAnchorOperationRegisterGate || request.Operation == linuxOwnerAnchorOperationAuthorizeGate) &&
		request.Helper != nil && request.Helper.Validate() == nil && request.Gate != nil && request.Gate.Validate() == nil &&
		request.UserNamespace != nil && request.UserNamespace.valid() && request.Creator == nil && request.Process == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

func validLinuxOwnerAnchorStopLeafRequest(request linuxOwnerAnchorRequest) bool {
	return request.Operation == linuxOwnerAnchorOperationStopLeaf && request.Helper != nil && request.Helper.Validate() == nil &&
		request.StopFence != nil && request.StopFence.ValidateFor(request.Grant, *request.Helper) == nil &&
		request.Gate == nil && request.UserNamespace == nil && request.Creator == nil && request.Process == nil && request.Role == "" &&
		request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
		request.RunKey == (LinuxOwnerAnchorRunKey{}) && request.Fence == (protocol.Fence{}) && request.FenceDigest == "" &&
		request.AfterPID == 0 && request.CloneGrant == nil && request.Cleanup == nil && request.PriorRemovalProof == nil && !request.TargetFDPresent
}

// runCloneCleanupAfterStateReadback is called only after the socket request,
// pinned descriptor, exact intent and optional prior proof pass anchor-side
// structural checks. State remains authoritative for whether Begin/receipt
// facts were actually committed; the root worker is unreachable until that
// pinned readback succeeds.
func (anchor *linuxOwnerAnchor) runCloneCleanupAfterStateReadback(
	ctx context.Context,
	request linuxOwnerAnchorRequest,
	rootFD, targetFD int,
	cloneGrant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent,
	priorRemoval *authority.LinuxCloneTargetRemovalProof,
) (authority.LinuxCloneTargetRemovalProof, error) {
	if anchor == nil || ctx == nil || ctx.Err() != nil || request.Operation != linuxOwnerAnchorOperationCloneCleanup ||
		request.Cleanup == nil || request.CloneGrant == nil || request.RunKey != (LinuxOwnerAnchorRunKey{RunID: cleanup.Intent.RunID, Generation: cleanup.Intent.Generation}) ||
		request.FenceDigest != cleanup.FenceDigest || !request.Cleanup.Equal(cleanup) || !request.CloneGrant.Equal(cloneGrant) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup worker is not bound to its exact request")
	}
	if rootFD < 0 || targetFD < -1 || request.TargetFDPresent != (targetFD >= 0) ||
		(request.TargetFDPresent && priorRemoval != nil) || (!request.TargetFDPresent && priorRemoval == nil) ||
		(request.PriorRemovalProof == nil) != (priorRemoval == nil) ||
		request.PriorRemovalProof != nil && !request.PriorRemovalProof.Equal(*priorRemoval) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup descriptors differ from the exact prior-removal request")
	}
	if anchor.cleanupVerifier == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errLinuxOwnerAnchorCleanupAuthority
	}
	if err := anchor.cleanupVerifier.Verify(
		request.RunKey.RunID, request.RunKey.Generation, cloneGrant, cleanup, priorRemoval, request.TargetFDPresent,
	); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(errLinuxOwnerAnchorCleanupAuthority, err)
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("State cleanup readback outlasted its bounded anchor request: %w", err)
	}
	if anchor.cloneCleanupWorker == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux owner anchor clone cleanup worker is unavailable")
	}
	return anchor.cloneCleanupWorker(ctx, rootFD, targetFD, cloneGrant, cleanup, priorRemoval)
}

func (anchor *linuxOwnerAnchor) serveCloneCleanup(
	connection *net.UnixConn,
	peer linuxOwnerAnchorPeer,
	request linuxOwnerAnchorRequest,
	receivedFDs []int,
) bool {
	wantFDs := 1
	if request.TargetFDPresent {
		wantFDs++
	}
	if len(receivedFDs) != wantFDs || request.Grant != (LinuxOwnerAnchorGrant{}) ||
		request.CloneGrant == nil || request.Cleanup == nil || request.Helper != nil || request.Creator != nil ||
		request.Process != nil || request.Role != "" || request.ReleaseProof != nil ||
		request.PreGateReleaseProof != nil || request.AbortProof != nil ||
		request.TargetFDPresent && request.PriorRemovalProof != nil || !request.TargetFDPresent && request.PriorRemovalProof == nil ||
		request.AfterPID != 0 ||
		request.RunKey != (LinuxOwnerAnchorRunKey{RunID: request.Cleanup.Intent.RunID, Generation: request.Cleanup.Intent.Generation}) ||
		request.Fence != (protocol.Fence{}) || request.FenceDigest != request.Cleanup.FenceDigest ||
		linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
		return false
	}
	expectedAnchor := authority.LinuxCgroupProcessIdentity{
		PID:      request.Cleanup.NormalReleaseProof.AnchorPID,
		Identity: request.Cleanup.NormalReleaseProof.AnchorIdentity,
	}
	if err := validateLinuxOwnerAnchorCloneCleanup(*request.CloneGrant, *request.Cleanup, anchor.owner.AUID); err != nil ||
		!expectedAnchor.Equal(anchor.identity) || request.Cleanup.NormalReleaseProof.CgroupIdentity == nil ||
		!request.Cleanup.NormalReleaseProof.CgroupIdentity.Parent.Equal(anchor.parent.Identity()) {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if !request.TargetFDPresent {
		if err := verifyLinuxOwnerAnchorPriorRemoval(receivedFDs[0], *request.Cleanup, request.PriorRemovalProof, anchor.owner); err != nil {
			anchor.writeResponse(connection, request, "request_rejected", nil, nil)
			return false
		}
	}
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorCleanupTimeout)); err != nil {
		return false
	}
	if err := verifyLinuxOwnerAnchorManagerMembership(os.Getpid(), anchor.managerPath); err != nil {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if err := anchor.parent.verifyPinnedParent(); err != nil {
		anchor.writeResponse(connection, request, "request_rejected", nil, nil)
		return false
	}
	if anchor.cloneCleanupWorkers.Add(1) > 1 {
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_busy", nil, nil)
		return false
	}
	rootCopy, err := unix.FcntlInt(uintptr(receivedFDs[0]), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
		return false
	}
	rootFile := os.NewFile(uintptr(rootCopy), "symmetry-owner-anchor-clone-root")
	if rootFile == nil {
		_ = unix.Close(rootCopy)
		anchor.cloneCleanupWorkers.Add(-1)
		anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
		return false
	}
	targetFD := -1
	var targetFile *os.File
	if request.TargetFDPresent {
		targetCopy, duplicateErr := unix.FcntlInt(uintptr(receivedFDs[1]), unix.F_DUPFD_CLOEXEC, 0)
		if duplicateErr != nil {
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
			return false
		}
		targetFile = os.NewFile(uintptr(targetCopy), "symmetry-owner-anchor-clone-target")
		if targetFile == nil {
			_ = unix.Close(targetCopy)
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			anchor.writeResponse(connection, request, "cleanup_unproven", nil, nil)
			return false
		}
		targetFD = int(targetFile.Fd())
	}
	requestCopy := request
	cloneGrant := request.CloneGrant.Clone()
	cleanup := request.Cleanup.Clone()
	var priorRemoval *authority.LinuxCloneTargetRemovalProof
	if request.PriorRemovalProof != nil {
		priorCopy := request.PriorRemovalProof.Clone()
		priorRemoval = &priorCopy
	}
	go func() {
		defer func() {
			if targetFile != nil {
				_ = targetFile.Close()
			}
			_ = rootFile.Close()
			anchor.cloneCleanupWorkers.Add(-1)
			_ = connection.Close()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorCleanupTimeout)
		proof, err := anchor.runCloneCleanupAfterStateReadback(
			ctx, requestCopy, int(rootFile.Fd()), targetFD, cloneGrant, cleanup, priorRemoval,
		)
		cancel()
		if err != nil || proof.Validate() != nil || !proof.Intent.Equal(cleanup) ||
			priorRemoval == nil && proof.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
			priorRemoval != nil && !proof.Equal(*priorRemoval) {
			status := "clone_cleanup_unproven"
			if errors.Is(err, errLinuxOwnerAnchorCleanupAuthority) {
				status = "cleanup_authority_unproven"
			}
			anchor.writeResponse(connection, requestCopy, status, nil, nil)
			return
		}
		anchor.writeResponseWithCloneRemoval(connection, requestCopy, "clone_removed", nil, nil, &proof)
	}()
	return true
}

func (anchor *linuxOwnerAnchor) writeResponse(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence) {
	anchor.writeResponseWithCloneRemoval(connection, request, status, helper, evidence, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithStopEvidence(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	evidence LinuxOwnerAnchorStopEvidence,
) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, StopEvidence: &evidence,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithCloneRemoval(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence, cloneRemoval *authority.LinuxCloneTargetRemovalProof) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, Evidence: evidence, CloneRemoval: cloneRemoval,
	}
	if request.Operation == linuxOwnerAnchorOperationCloneCleanup && request.Cleanup != nil {
		response.RunKey = request.RunKey
		response.FenceDigest = request.FenceDigest
		response.LaunchToken = request.Cleanup.NormalReleaseProof.LaunchToken
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithFDs(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence, fds ...int) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, Evidence: evidence,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame || len(fds) == 0 {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, syscall.UnixRights(fds...), nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithReapEvidence(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	evidence []LinuxOwnerAnchorReapEvidence,
	nextPID int,
	more bool,
	stopEvidence LinuxOwnerAnchorStopEvidence,
) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, ReapEvidence: evidence, StopEvidence: &stopEvidence, NextPID: nextPID, More: more,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithChildClassification(
	connection *net.UnixConn,
	request linuxOwnerAnchorRequest,
	status string,
	classification linuxOwnerAnchorRunChildClassification,
) {
	copy := classification
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: request.Helper, ChildClassification: &copy,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithHelperExit(connection *net.UnixConn, request linuxOwnerAnchorRequest, proof authority.LinuxCgroupExitProof) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: "reaped",
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: request.Helper, HelperExit: &proof,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, nil, nil)
}

func (anchor *linuxOwnerAnchor) writeResponseWithRunPins(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, helperExit *authority.LinuxCgroupExitProof, fds ...int) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, HelperExit: helperExit,
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame || len(fds) == 0 {
		return
	}
	_, _, _ = connection.WriteMsgUnix(encoded, syscall.UnixRights(fds...), nil)
}

func readLinuxOwnerAnchorRequest(connection *net.UnixConn) (linuxOwnerAnchorRequest, []int, error) {
	data := make([]byte, linuxOwnerAnchorMaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerAnchorRequest{}, nil, fmt.Errorf("read Linux owner anchor request: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerAnchorRequest{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
		return linuxOwnerAnchorRequest{}, fds, errors.New("Linux owner anchor request exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var request linuxOwnerAnchorRequest
	if err := decoder.Decode(&request); err != nil {
		return linuxOwnerAnchorRequest{}, fds, fmt.Errorf("decode Linux owner anchor request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return linuxOwnerAnchorRequest{}, fds, errors.New("Linux owner anchor request has trailing JSON")
	}
	return request, fds, nil
}

func readLinuxOwnerAnchorResponse(connection *net.UnixConn) (linuxOwnerAnchorResponse, []int, error) {
	data := make([]byte, linuxOwnerAnchorMaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("read Linux owner anchor response: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerAnchorResponse{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor response exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var response linuxOwnerAnchorResponse
	if err := decoder.Decode(&response); err != nil {
		return linuxOwnerAnchorResponse{}, fds, fmt.Errorf("decode Linux owner anchor response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return linuxOwnerAnchorResponse{}, fds, errors.New("Linux owner anchor response has trailing JSON")
	}
	for _, fd := range fds {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			closeLinuxOwnerAnchorFDs(fds)
			return linuxOwnerAnchorResponse{}, nil, errors.Join(errors.New("received Linux owner anchor descriptor is not close-on-exec"), err)
		}
	}
	return response, fds, nil
}

func parseLinuxOwnerAnchorRights(oob []byte, flags int) ([]int, error) {
	if len(oob) == 0 {
		if flags&unix.MSG_CTRUNC != 0 {
			return nil, errors.New("Linux owner anchor ancillary data was truncated")
		}
		return nil, nil
	}
	messages, err := syscall.ParseSocketControlMessage(oob)
	var fds []int
	for _, message := range messages {
		if message.Header.Level != syscall.SOL_SOCKET || message.Header.Type != syscall.SCM_RIGHTS {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, errors.New("Linux owner anchor received unsupported ancillary data")
		}
		values, err := syscall.ParseUnixRights(&message)
		if err != nil {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, fmt.Errorf("parse Linux owner anchor descriptor transfer: %w", err)
		}
		fds = append(fds, values...)
	}
	if err != nil {
		closeLinuxOwnerAnchorFDs(fds)
		return nil, fmt.Errorf("parse Linux owner anchor ancillary data: %w", err)
	}
	if flags&unix.MSG_CTRUNC != 0 {
		closeLinuxOwnerAnchorFDs(fds)
		return nil, errors.New("Linux owner anchor ancillary data was truncated")
	}
	for _, fd := range fds {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			closeLinuxOwnerAnchorFDs(fds)
			return nil, fmt.Errorf("mark received Linux owner anchor descriptor close-on-exec: %w", err)
		}
	}
	return fds, nil
}

func linuxOwnerAnchorPeerFromConn(connection *net.UnixConn, owner linuxgate.OwnerConfig) (linuxOwnerAnchorPeer, error) {
	if connection == nil {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner anchor peer connection is nil")
	}
	credentials, err := linuxOwnerAnchorPeerCredentials(connection)
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	if credentials.Pid <= 0 || credentials.Uid != owner.AUID || credentials.Gid != owner.AGID {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner anchor peer SO_PEERCRED UID/GID/PID is not configured A")
	}
	identity, err := ProcessIdentity(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, fmt.Errorf("read exact Linux owner anchor peer identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(int(credentials.Pid), identity, owner.AUID, owner.AGID, 0, ""); err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	actualIdentity, err := ProcessIdentity(int(credentials.Pid))
	if err != nil || actualIdentity != identity {
		return linuxOwnerAnchorPeer{}, fmt.Errorf("Linux owner anchor peer identity changed during authentication: %w", errOr(err, errors.New("creation identity differs")))
	}
	return linuxOwnerAnchorPeer{PID: int(credentials.Pid), Identity: identity, CgroupPath: cgroupPath}, nil
}

type linuxOwnerAnchorPeer struct {
	PID        int
	Identity   string
	CgroupPath string
}

func verifyLinuxOwnerAnchorPeer(connection *net.UnixConn, owner linuxgate.OwnerConfig, expected authority.LinuxCgroupProcessIdentity) error {
	if expected.Validate() != nil {
		return errors.New("expected Linux owner anchor process identity is invalid")
	}
	peer, err := linuxOwnerAnchorPeerFromConn(connection, owner)
	if err != nil {
		return err
	}
	if peer.PID != expected.PID || peer.Identity != expected.Identity {
		return errors.New("Linux owner anchor peer PID/creation identity does not match the durable anchor identity")
	}
	return nil
}

func linuxOwnerAnchorPeerCredentials(connection *net.UnixConn) (*unix.Ucred, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("access Linux owner anchor socket: %w", err)
	}
	var credentials *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("read Linux owner anchor SO_PEERCRED: %w", err)
	}
	if controlErr != nil || credentials == nil {
		return nil, fmt.Errorf("read Linux owner anchor SO_PEERCRED: %w", errOr(controlErr, errors.New("credentials unavailable")))
	}
	return credentials, nil
}

func verifyLinuxOwnerAProcess(pid int, expectedIdentity string, uid, gid uint32, expectedParent int, expectedCgroup string) error {
	if pid <= 0 || expectedIdentity == "" || uid == 0 || gid == 0 {
		return errors.New("Linux owner A process identity or credentials are incomplete")
	}
	actualIdentity, err := ProcessIdentity(pid)
	if err != nil || actualIdentity != expectedIdentity {
		return fmt.Errorf("Linux owner A process creation identity mismatch: %w", errOr(err, errors.New("identity differs")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(pid)
	if err != nil {
		return err
	}
	if credentials.UIDs != [4]uint32{uid, uid, uid, uid} || credentials.GIDs != [4]uint32{gid, gid, gid, gid} {
		return errors.New("Linux owner A process does not retain the configured real/effective/saved/filesystem UID/GID")
	}
	if expectedParent > 0 && credentials.Parent != expectedParent {
		return errors.New("Linux owner A process ancestry does not match the exact expected parent")
	}
	if expectedCgroup != "" {
		cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
		if err != nil {
			return err
		}
		if cgroupPath != expectedCgroup {
			return errors.New("Linux owner A process is not in the exact configured cgroup subgroup")
		}
	}
	actualIdentity, err = ProcessIdentity(pid)
	if err != nil || actualIdentity != expectedIdentity {
		return fmt.Errorf("Linux owner A process identity changed during credential check: %w", errOr(err, errors.New("creation identity differs")))
	}
	return nil
}

type linuxOwnerProcessCredentials struct {
	UIDs   [4]uint32
	GIDs   [4]uint32
	Parent int
}

func readLinuxOwnerProcessCredentials(pid int) (linuxOwnerProcessCredentials, error) {
	if pid <= 0 {
		return linuxOwnerProcessCredentials{}, errors.New("Linux owner process PID must be positive")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return linuxOwnerProcessCredentials{}, fmt.Errorf("read Linux owner process %d status: %w", pid, err)
	}
	var result linuxOwnerProcessCredentials
	var gotUIDs, gotGIDs, gotParent bool
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Uid":
			if gotUIDs {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates Uid")
			}
			values, err := parseLinuxOwnerIDs(value, 4)
			if err != nil {
				return linuxOwnerProcessCredentials{}, fmt.Errorf("parse Linux owner process Uid: %w", err)
			}
			copy(result.UIDs[:], values)
			gotUIDs = true
		case "Gid":
			if gotGIDs {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates Gid")
			}
			values, err := parseLinuxOwnerIDs(value, 4)
			if err != nil {
				return linuxOwnerProcessCredentials{}, fmt.Errorf("parse Linux owner process Gid: %w", err)
			}
			copy(result.GIDs[:], values)
			gotGIDs = true
		case "PPid":
			if gotParent {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status duplicates PPid")
			}
			parent, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parent < 0 {
				return linuxOwnerProcessCredentials{}, errors.New("Linux owner process PPid is invalid")
			}
			result.Parent = parent
			gotParent = true
		}
	}
	if !gotUIDs || !gotGIDs || !gotParent {
		return linuxOwnerProcessCredentials{}, errors.New("Linux owner process status lacks Uid/Gid/PPid")
	}
	return result, nil
}

func parseLinuxOwnerIDs(value string, count int) ([]uint32, error) {
	fields := strings.Fields(value)
	if len(fields) != count {
		return nil, errors.New("credential field has wrong number of IDs")
	}
	result := make([]uint32, count)
	for index, field := range fields {
		parsed, err := strconv.ParseUint(field, 10, 32)
		if err != nil || strconv.FormatUint(parsed, 10) != field {
			return nil, errors.New("credential ID is not canonical unsigned decimal")
		}
		result[index] = uint32(parsed)
	}
	return result, nil
}

func readLinuxOwnerProcessCgroupPath(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("Linux cgroup membership PID must be positive")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", fmt.Errorf("read Linux owner process %d cgroup membership: %w", pid, err)
	}
	var result string
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || fields[0] == "" {
			return "", errors.New("Linux process cgroup membership contains a malformed line")
		}
		if fields[0] == "0" && fields[1] == "" {
			if result != "" || !canonicalCgroupHierarchyPath(fields[2]) {
				return "", errors.New("Linux unified cgroup membership is missing, duplicated, or noncanonical")
			}
			result = fields[2]
		}
	}
	if result == "" {
		return "", errors.New("Linux process has no unified cgroup-v2 membership")
	}
	return result, nil
}

func verifyLinuxOwnerAnchorManagerMembership(pid int, managerPath string) error {
	if managerPath == "" || managerPath == "/" {
		return errors.New("Linux owner anchor requires a non-root manager subgroup")
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
	if err != nil {
		return err
	}
	if cgroupPath != managerPath {
		return fmt.Errorf("Linux owner anchor process cgroup %q differs from pinned manager subgroup", cgroupPath)
	}
	return nil
}

func linuxOwnerCgroupPathsOverlap(left, right string) bool {
	return left == right || linuxOwnerCgroupPathWithin(left, right) || linuxOwnerCgroupPathWithin(right, left)
}

func linuxOwnerCgroupPathWithin(candidate, parent string) bool {
	if parent == "/" {
		return strings.HasPrefix(candidate, "/")
	}
	return candidate == parent || strings.HasPrefix(candidate, parent+"/")
}

func ensureLinuxOwnerAnchorSubreaper() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("enable independent Linux owner-anchor child subreaper: %w", err)
	}
	var enabled int32
	_, _, errno := unix.Syscall6(unix.SYS_PRCTL, uintptr(unix.PR_GET_CHILD_SUBREAPER), uintptr(unsafe.Pointer(&enabled)), 0, 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("read back Linux owner-anchor subreaper state: %w", errno)
	}
	if enabled != 1 {
		return errors.New("Linux owner-anchor process did not retain PR_SET_CHILD_SUBREAPER")
	}
	return nil
}

func validateLinuxOwnerAnchorSocketPath(socketPath string) error {
	if !canonicalAbsoluteLinuxPath(socketPath) || len(socketPath) >= linuxOwnerAnchorMaxSocketPath || filepath.Base(socketPath) == "." || filepath.Base(socketPath) == "/" {
		return errors.New("Linux owner anchor socket path must be canonical, absolute, and fit sockaddr_un")
	}
	return nil
}

type linuxOwnerAnchorSocketIdentity struct {
	device uint64
	inode  uint64
}

func listenLinuxOwnerAnchor(socketPath string, owner linuxgate.OwnerConfig) (*net.UnixListener, linuxOwnerAnchorSocketIdentity, error) {
	if err := validateLinuxOwnerAnchorSocketPath(socketPath); err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, err
	}
	directoryFD, directoryIdentity, err := openLinuxOwnerAnchorSocketDirectory(filepath.Dir(socketPath), owner.AUID)
	if err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, err
	}
	defer unix.Close(directoryFD)
	var existing unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(socketPath), &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, errors.New("Linux owner anchor socket path already exists; refusing stale or replacement endpoint")
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("inspect Linux owner anchor socket path: %w", err)
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socketPath, Net: "unixpacket"})
	if err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("listen on stable Linux owner anchor socket: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("set private Linux owner anchor socket permissions: %w", err)
	}
	var socketStat, directoryAfter unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(socketPath), &socketStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("verify bound Linux owner anchor socket: %w", err)
	}
	if err := unix.Fstat(directoryFD, &directoryAfter); err != nil || directoryAfter.Dev != directoryIdentity.Dev || directoryAfter.Ino != directoryIdentity.Ino ||
		socketStat.Mode&unix.S_IFMT != unix.S_IFSOCK || socketStat.Uid != owner.AUID || socketStat.Gid != owner.AGID || socketStat.Mode&0777 != 0600 {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, errors.Join(errors.New("bound Linux owner anchor socket or containing directory changed identity"), err)
	}
	listener.SetUnlinkOnClose(false)
	return listener, linuxOwnerAnchorSocketIdentity{device: uint64(socketStat.Dev), inode: socketStat.Ino}, nil
}

func openLinuxOwnerAnchorSocketDirectory(directory string, ownerUID uint32) (int, unix.Stat_t, error) {
	if !canonicalAbsoluteLinuxPath(directory) {
		return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory is not canonical")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, fmt.Errorf("open root for Linux owner anchor socket path: %w", err)
	}
	parts := strings.Split(strings.TrimPrefix(directory, "/"), "/")
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory has an unsafe component")
		}
		how := &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
		}
		next, err := unix.Openat2(fd, part, how)
		if err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("open Linux owner anchor socket directory component: %w", err)
		}
		_ = unix.Close(fd)
		fd = next
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("stat Linux owner anchor socket directory component: %w", err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || (stat.Uid != 0 && stat.Uid != ownerUID) || stat.Mode&0022 != 0 {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket path crosses a non-root writable or untrusted directory")
		}
		if index == len(parts)-1 && (stat.Uid != ownerUID || stat.Mode&0700 != 0700 || stat.Mode&0077 != 0) {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory must be A-owned and private")
		}
	}
	var final unix.Stat_t
	if err := unix.Fstat(fd, &final); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("stat final Linux owner anchor socket directory: %w", err)
	}
	return fd, final, nil
}

func unlinkLinuxOwnerAnchorSocket(socketPath string, expected linuxOwnerAnchorSocketIdentity, ownerUID uint32) {
	if expected.inode == 0 {
		return
	}
	directoryFD, directoryIdentity, err := openLinuxOwnerAnchorSocketDirectory(filepath.Dir(socketPath), ownerUID)
	if err != nil {
		return
	}
	defer unix.Close(directoryFD)
	var stat unix.Stat_t
	if unix.Fstatat(directoryFD, filepath.Base(socketPath), &stat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		uint64(stat.Dev) != expected.device || stat.Ino != expected.inode || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != ownerUID {
		return
	}
	var currentDirectory unix.Stat_t
	if unix.Fstat(directoryFD, &currentDirectory) != nil || currentDirectory.Dev != directoryIdentity.Dev || currentDirectory.Ino != directoryIdentity.Ino {
		return
	}
	_ = unix.Unlinkat(directoryFD, filepath.Base(socketPath), 0)
}

func notifyLinuxOwnerAnchorReady() error {
	address := os.Getenv("NOTIFY_SOCKET")
	if address == "" {
		return nil
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("connect systemd Linux owner anchor notification socket: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("READY=1\nSTATUS=delegated cgroup2 parent, manager subgroup, subreaper, and private socket verified\n")); err != nil {
		return fmt.Errorf("notify systemd Linux owner anchor readiness: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	return nil
}

func verifyLinuxOwnerAnchorLeafFD(fd int, expected authority.LinuxCgroupIdentity, ownerUID uint32) error {
	if fd < 0 || expected.Validate() != nil {
		return errors.New("Linux owner anchor leaf FD or identity is invalid")
	}
	if err := requireLinuxCgroup2FD(fd); err != nil {
		return err
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil {
		return err
	}
	if ownerUID == 0 || stat.mount != expected.Parent.MountID || stat.device != expected.LeafDevice || stat.inode != expected.LeafInode || stat.uid != ownerUID {
		return errors.New("Linux owner anchor leaf FD does not match the exact A-owned reserved identity")
	}
	return nil
}

func verifyLinuxOwnerAnchorPIDFD(fd int, expected authority.LinuxCgroupProcessIdentity) error {
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(fd, expected); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("probe exact Linux owner anchor pidfd: %w", err)
	}
	return nil
}

func verifyLinuxOwnerAnchorPIDFDIdentity(fd int, expected authority.LinuxCgroupProcessIdentity) error {
	if fd < 0 || expected.Validate() != nil {
		return errors.New("Linux owner anchor pidfd or process identity is invalid")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		return errors.Join(errors.New("Linux owner anchor pidfd is not close-on-exec"), err)
	}
	pid, err := readLinuxOwnerPIDFDTarget(fd)
	if err != nil || pid != expected.PID {
		return fmt.Errorf("Linux owner anchor pidfd target mismatch: %w", errOr(err, errors.New("PID differs")))
	}
	identity, err := ProcessIdentity(pid)
	if err != nil || identity != expected.Identity {
		return fmt.Errorf("Linux owner anchor pidfd creation identity mismatch: %w", errOr(err, errors.New("identity differs")))
	}
	return nil
}

func readLinuxOwnerPIDFDTarget(fd int) (int, error) {
	if fd < 0 {
		return 0, errors.New("Linux pidfd is invalid")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return 0, fmt.Errorf("read exact Linux pidfd fdinfo: %w", err)
	}
	var pid int
	found := false
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != "Pid" {
			continue
		}
		if found {
			return 0, errors.New("Linux pidfd fdinfo contains duplicate PID fields")
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return 0, errors.New("Linux pidfd fdinfo has no live positive target PID")
		}
		pid, found = parsed, true
	}
	if !found {
		return 0, errors.New("Linux pidfd fdinfo omits target PID")
	}
	return pid, nil
}

// linuxOwnerAnchorWaitPIDFD waits and reaps only a pidfd-bound child. ECHILD,
// missing procfs identity, a replaced numeric PID, or a different pidfd target
// is unresolved evidence, never a successful wait. Membership and RunKey
// binding are checked by linuxOwnerAnchorRun before this kernel wait primitive.
func linuxOwnerAnchorWaitPIDFD(ctx context.Context, pidfd *os.File, expected authority.LinuxCgroupProcessIdentity) error {
	if ctx == nil || pidfd == nil || expected.Validate() != nil {
		return errors.New("Linux owner anchor wait requires a context, exact process identity, and pidfd")
	}
	bounded, cancel := context.WithTimeout(ctx, linuxOwnerAnchorWaitTimeout)
	defer cancel()
	fd := int(pidfd.Fd())
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(fd, expected); err != nil {
		return err
	}
	for {
		if err := bounded.Err(); err != nil {
			return fmt.Errorf("wait for exact Linux owner anchor child: %w", err)
		}
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.ECHILD) {
			return fmt.Errorf("wait for exact Linux owner anchor child: %w (ECHILD is not positive reap evidence)", err)
		}
		if err != nil {
			return fmt.Errorf("waitid exact Linux owner anchor pidfd: %w", err)
		}
		if info.Signo != 0 {
			if info.Signo != int32(unix.SIGCHLD) {
				return errors.New("waitid exact Linux owner pidfd returned an unexpected signal record")
			}
			identity, identityErr := ProcessIdentity(expected.PID)
			if identityErr != nil || identity != expected.Identity {
				return fmt.Errorf("verify exact Linux owner child identity before reap: %w", errOr(identityErr, errors.New("creation identity changed")))
			}
			for {
				var reaped unix.Siginfo
				err := unix.Waitid(unix.P_PIDFD, fd, &reaped, unix.WEXITED, nil)
				if errors.Is(err, unix.EINTR) {
					continue
				}
				if err != nil {
					return fmt.Errorf("reap exact Linux owner anchor child: %w", err)
				}
				if reaped.Signo != int32(unix.SIGCHLD) {
					return errors.New("reap exact Linux owner pidfd returned no SIGCHLD wait record")
				}
				return nil
			}
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		remaining := linuxOwnerAnchorWaitPoll
		if deadline, ok := bounded.Deadline(); ok && time.Until(deadline) < remaining {
			remaining = time.Until(deadline)
		}
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		_, err = unix.Poll(poll, int(remaining/time.Millisecond)+1)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("poll exact Linux owner anchor pidfd: %w", err)
		}
	}
}

// linuxOwnerAnchorRun contains ephemeral process ownership only. It is not a
// journal: the existing Store remains the sole durable authority. A run entry
// can be populated only after an authenticated helper/daemon handoff has
// pinned the exact leaf and identities.
type linuxOwnerAnchorRun struct {
	grant                  LinuxOwnerAnchorGrant
	owner                  linuxgate.OwnerConfig
	helper                 authority.LinuxCgroupProcessIdentity
	helperPIDFD            *os.File
	helperExit             *authority.LinuxCgroupExitProof
	leaf                   *LinuxCgroupV2Leaf
	gateIdentity           *authority.LinuxCgroupGateIdentity
	gateUserNamespace      LinuxUserNamespaceIdentity
	gateUserNamespaceFD    *os.File
	gateImageFD            *os.File
	gateReadyAuthenticated bool
	gateReleaseAuthorized  bool
	stopRequested          bool
	stopLeafAttempted      bool
	stopFence              *LinuxOwnerAnchorProducerFence
	stopEvidence           *LinuxOwnerAnchorStopEvidence
	// migrationObserved is sticky: a foreign, escaped, or unattributable
	// process observation permanently prevents this ephemeral run from claiming
	// NoMigrationObserved, even if that process later exits or leaves the leaf.
	migrationObserved bool
	// noMigrationProven is set only after the authenticated GateReady/image,
	// B-only namespace/credential and complete pinned-leaf membership checks.
	// It is not copied into durable State independently of AnchorStop.
	noMigrationProven bool
	// These observations are ephemeral emergency-stop state only; none can
	// authorize State receipt creation or journal clearing.
	helperDeathContainmentComplete  bool
	helperDeathContainmentAttempted bool
	supervisionRetryAfter           time.Time
	supervisionTerminal             bool
	supervisionError                error
	leafKillAttempted               bool
	leafKillCallStarted             bool
	leafKillAcknowledged            bool
	leafEmptyObserved               bool
	leafRemovedObserved             bool
	children                        map[int]linuxOwnerAnchorTrackedProcess
	physicalOnlyChildren            map[int]linuxOwnerAnchorPhysicalOnlyChild
	// reaped retains exact responses only while this anchor process lives. It
	// is not durable and cannot recover proof after correlated anchor loss.
	reaped                map[int]LinuxOwnerAnchorReapEvidence
	retired               bool
	retiredRelease        *authority.SupervisorHandoffReleaseProof
	retiredPreGateRelease *authority.LinuxCgroupPreGateReleaseProof
	retiredAbort          *authority.LinuxCgroupReservationAbortProof
}

type linuxOwnerAnchorTrackedProcess struct {
	identity       authority.LinuxCgroupProcessIdentity
	creator        authority.LinuxCgroupProcessIdentity
	classification linuxOwnerAnchorRunChildClassification
	role           string
	pidfd          *os.File
}

// linuxOwnerAnchorPhysicalOnlyChild pins a pre-RegisterGate A gate solely so
// the subreaper can wait/reap its exact PIDFD after whole-leaf containment. It
// is deliberately separate from RunKey children and never contributes durable
// ReapEvidence, StopEvidence, or receipt authority.
type linuxOwnerAnchorPhysicalOnlyChild struct {
	identity       authority.LinuxCgroupProcessIdentity
	classification linuxOwnerAnchorRunChildClassification
	pidfd          *os.File
	imageFD        *os.File
	waited         bool
}

func newLinuxOwnerAnchorRun(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, helperPIDFD *os.File, leaf *LinuxCgroupV2Leaf, owner linuxgate.OwnerConfig) (*linuxOwnerAnchorRun, error) {
	if grant.Validate() != nil || helper.Validate() != nil || helperPIDFD == nil || leaf == nil || owner.Validate() != nil ||
		owner.AUID == 0 || owner.AGID == 0 || grant.OwnerUID != owner.AUID || grant.LauncherUID != 0 ||
		leaf.OwnerUID() != owner.AUID || !leaf.Identity().Equal(grant.Leaf) {
		return nil, errors.New("Linux owner anchor run binding is incomplete")
	}
	anchorIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil || os.Getpid() != grant.Anchor.PID || anchorIdentity != grant.Anchor.Identity {
		return nil, errors.Join(errors.New("Linux owner anchor process identity differs from the durable grant"), err)
	}
	if err := verifyLinuxOwnerAnchorLeaf(leaf, grant.Leaf, owner.AUID); err != nil {
		return nil, err
	}
	if err := verifyLinuxOwnerAnchorPIDFD(int(helperPIDFD.Fd()), helper); err != nil {
		return nil, err
	}
	if err := verifyLinuxOwnerAProcess(helper.PID, helper.Identity, owner.AUID, owner.AGID, grant.Anchor.PID, grant.Parent.Path); err != nil {
		return nil, fmt.Errorf("verify exact Linux owner helper credentials and manager subgroup: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFD(int(helperPIDFD.Fd()), helper); err != nil {
		return nil, fmt.Errorf("helper identity changed during owner-run binding: %w", err)
	}
	helperCopy, err := unix.FcntlInt(helperPIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("retain exact Linux owner helper pidfd: %w", err)
	}
	leafCopy, err := duplicateLinuxOwnerAnchorLeaf(leaf)
	if err != nil {
		_ = unix.Close(helperCopy)
		return nil, err
	}
	helperFile := os.NewFile(uintptr(helperCopy), "symmetry-owner-anchor-run-helper-pidfd")
	if helperFile == nil {
		_ = unix.Close(helperCopy)
		_ = leafCopy.Close()
		return nil, errors.New("wrap retained Linux owner helper pidfd")
	}
	return &linuxOwnerAnchorRun{
		grant: grant, owner: owner, helper: helper,
		helperPIDFD: helperFile,
		leaf:        leafCopy, children: make(map[int]linuxOwnerAnchorTrackedProcess), reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}, nil
}

func duplicateLinuxOwnerAnchorLeaf(leaf *LinuxCgroupV2Leaf) (*LinuxCgroupV2Leaf, error) {
	if leaf == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return nil, err
	}
	parentFD, err := unix.FcntlInt(uintptr(leaf.parentFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate Linux owner anchor leaf parent pin: %w", err)
	}
	leafFD, err := unix.FcntlInt(uintptr(leaf.leafFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("duplicate Linux owner anchor leaf pin: %w", err)
	}
	return &LinuxCgroupV2Leaf{parentFD: parentFD, leafFD: leafFD, identity: leaf.identity, ownerUID: leaf.ownerUID}, nil
}

// linuxOwnerAnchorPinnedLeafPresent validates both retained FDs before using
// the pinned parent to distinguish the exact original leaf from absence or a
// same-name replacement. Exact absence is the kernel's rmdir-backed empty
// observation; serialized caller booleans never supply it.
func linuxOwnerAnchorPinnedLeafPresent(run *linuxOwnerAnchorRun) (bool, error) {
	if run == nil || run.leaf == nil || run.grant.Validate() != nil ||
		run.leaf.OwnerUID() != run.grant.OwnerUID || !run.leaf.Identity().Equal(run.grant.Leaf) {
		return false, errors.New("Linux owner anchor has no exact pinned run leaf")
	}
	leaf := run.leaf
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if leaf.parentFD < 0 || leaf.leafFD < 0 || leaf.ownerUID != run.grant.OwnerUID || !leaf.identity.Equal(run.grant.Leaf) {
		return false, ErrLinuxCgroupV2Unresolved
	}
	if err := requireLinuxCgroup2FD(leaf.parentFD); err != nil {
		return false, fmt.Errorf("verify pinned run parent filesystem: %w", err)
	}
	if err := requireLinuxCgroup2FD(leaf.leafFD); err != nil {
		return false, fmt.Errorf("verify pinned run leaf filesystem: %w", err)
	}
	parentStat, err := statLinuxCgroupFD(leaf.parentFD)
	if err != nil || parentStat.mount != run.grant.Parent.MountID || parentStat.device != run.grant.Parent.Device ||
		parentStat.inode != run.grant.Parent.Inode || parentStat.uid != run.grant.OwnerUID {
		return false, fmt.Errorf("pinned Linux owner parent identity changed: %w", errOr(err, ErrLinuxCgroupV2Unresolved))
	}
	leafStat, err := statLinuxCgroupFD(leaf.leafFD)
	if err != nil || leafStat.mount != run.grant.Leaf.Parent.MountID || leafStat.device != run.grant.Leaf.LeafDevice ||
		leafStat.inode != run.grant.Leaf.LeafInode || leafStat.uid != run.grant.OwnerUID ||
		leafStat.mode&unix.S_IFMT != unix.S_IFDIR {
		return false, fmt.Errorf("pinned Linux owner leaf identity changed: %w", errOr(err, ErrLinuxCgroupV2Unresolved))
	}
	var named unix.Stat_t
	err = unix.Fstatat(leaf.parentFD, run.grant.Leaf.LeafName, &named, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		// A cgroup2 directory cannot be removed while populated. If the old
		// directory FD still exposes cgroup.events, reject any contradiction;
		// path absence itself remains the exact pinned-parent removal result.
		if populated, readErr := readLinuxCgroupDirectoryPopulated(leaf.leafFD,
			run.grant.Parent.MountID, run.grant.Parent.Device, run.grant.OwnerUID); readErr == nil && populated {
			return false, errors.New("exact pinned run leaf is absent but still reports populated")
		}
		run.leafRemovedObserved = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read exact pinned run leaf name under its parent: %w", err)
	}
	if uint64(named.Dev) != leafStat.device || named.Ino != leafStat.inode || named.Uid != run.grant.OwnerUID ||
		named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false, errors.New("same-name Linux owner leaf differs from the exact retained cgroup pin")
	}
	if err := requireLinuxCgroupNameAt(leaf.parentFD, run.grant.Leaf.LeafName, leafStat); err != nil {
		return false, err
	}
	return true, nil
}

func (run *linuxOwnerAnchorRun) registerChild(process authority.LinuxCgroupProcessIdentity, creator authority.LinuxCgroupProcessIdentity, role string, pidfd int) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || process.Validate() != nil ||
		creator.Validate() != nil || pidfd < 0 || run.helperPIDFD == nil || run.helperExit != nil || run.leaf == nil || run.stopRequested {
		return errors.New("Linux owner anchor child registration is not within the live helper phase")
	}
	if role != "launcher" && role != "gate" && role != "native" && role != "descendant" {
		return errors.New("Linux owner anchor refuses an unknown process role")
	}
	if !creator.Equal(run.helper) {
		parent, ok := run.children[creator.PID]
		if !ok || parent.pidfd == nil || !parent.identity.Equal(creator) {
			return errors.New("Linux owner anchor child has an unregistered creator ancestry")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(parent.pidfd.Fd()), creator); err != nil {
			return fmt.Errorf("verify Linux owner child creator identity: %w", err)
		}
	} else if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		return fmt.Errorf("verify Linux owner helper creator identity: %w", err)
	}
	if existing, exists := run.children[process.PID]; exists {
		if !existing.identity.Equal(process) || !existing.creator.Equal(creator) ||
			existing.classification.ValidateFor(run.grant, run.helper, process) != nil ||
			existing.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return errors.New("Linux owner anchor refuses a reused process PID")
		}
		if existing.role == role {
			if evidence, reaped := run.reaped[process.PID]; reaped {
				return evidence.ValidateFor(run.grant, run.helper, process, role)
			}
			if role == "native" {
				if err := run.verifyAuthorizedGateExec(process, true); err != nil {
					return err
				}
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
				return fmt.Errorf("verify replayed Linux owner child pidfd: %w", err)
			}
			return nil
		}
		if existing.role != "gate" || role != "native" {
			return errors.New("Linux owner anchor refuses invalid same-PID role transition")
		}
		if err := run.verifyAuthorizedGateExec(process, true); err != nil {
			return fmt.Errorf("verify authorized same-PID A-gate to B-native transition: %w", err)
		}
	} else if role == "native" {
		return errors.New("Linux owner anchor refuses a native process without its exact registered A-gate")
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, process); err != nil {
		return err
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("verify registered Linux owner child identity: %w", errOr(err, errors.New("creation identity differs")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != creator.PID {
		return fmt.Errorf("verify registered Linux owner child ancestry: %w", errOr(err, errors.New("parent PID differs")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, role, run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerRunChildMembership(process.PID, role, run.grant, run.leaf); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorPIDFD(pidfd, process); err != nil {
		return err
	}
	if existing, exists := run.children[process.PID]; exists {
		existing.role = role
		existing.classification.Role = role
		run.children[process.PID] = existing
		return nil
	}
	if process.Equal(run.helper) || process.Equal(run.grant.Anchor) {
		return errors.New("Linux owner anchor child aliases its helper or anchor")
	}
	copyFD, err := unix.FcntlInt(uintptr(pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("retain exact Linux owner child pidfd: %w", err)
	}
	classification := linuxOwnerAnchorRunChildClassification{
		Version: linuxOwnerAnchorRunChildClassificationVersion,
		RunKey:  run.grant.RunKey, OriginRunKey: run.grant.RunKey,
		Fence: run.grant.Fence, LaunchToken: run.grant.LaunchToken,
		Helper: run.helper, Process: process, Role: role,
		Origin: linuxOwnerAnchorRunChildOriginRegisteredCurrent,
	}
	run.children[process.PID] = linuxOwnerAnchorTrackedProcess{
		identity: process, creator: creator, classification: classification, role: role,
		pidfd: os.NewFile(uintptr(copyFD), "symmetry-owner-anchor-run-child-pidfd"),
	}
	return nil
}

// trackNamespaceProvenChild retains an exact PIDFD and Anchor-classified
// run-origin result for a process whose creator edge was lost at subreaper
// adoption. It deliberately leaves creator zero; only the exact namespace
// ancestry and RunKey-bound classification supply its origin.
func (run *linuxOwnerAnchorRun) trackNamespaceProvenChild(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	classification linuxOwnerAnchorRunChildClassification,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperPIDFD == nil ||
		run.leaf == nil || run.gateUserNamespaceFD == nil ||
		process.Validate() != nil || pidfd < 0 ||
		classification.ValidateFor(run.grant, run.helper, process) != nil ||
		classification.Origin != linuxOwnerAnchorRunChildOriginGateUserNSCurrent || classification.Role != "descendant" {
		return errors.New("anchor namespace-derived child lacks the exact RunKey and gate-namespace evidence")
	}
	if err := run.verifyPinnedGateUserNamespace(); err != nil {
		return err
	}
	if run.helperExit != nil {
		if !run.helperExit.ValidFor(run.helper, run.grant.Anchor) {
			return errors.New("anchor namespace-derived child helper exit differs from the exact wait")
		}
	} else {
		if verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper) != nil {
			return errors.New("anchor namespace-derived child live-helper phase lacks the exact helper pidfd")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(run.helperPIDFD)
		if err != nil || ready {
			return fmt.Errorf("verify namespace-derived child helper remains live: %w", errOr(err, errors.New("helper pidfd is ready")))
		}
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("verify namespace-derived adopted child pidfd: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("verify namespace-derived adopted child identity: %w", errOr(err, errors.New("creation identity differs")))
	}
	if !run.leafRemovedObserved {
		if err := verifyLinuxOwnerRunChildCredentials(process.PID, "native", run.owner); err != nil {
			return fmt.Errorf("verify namespace-derived adopted child B credentials: %w", err)
		}
		if err := verifyLinuxOwnerRunChildMembership(process.PID, "descendant", run.grant, run.leaf); err != nil {
			return err
		}
	}
	if existing, exists := run.children[process.PID]; exists {
		if !existing.identity.Equal(process) {
			return errors.New("anchor namespace-derived process PID was reused")
		}
		if existing.classification.ValidateFor(run.grant, run.helper, process) != nil {
			return errors.New("anchor namespace-derived process conflicts with its prior origin classification")
		}
		if existing.classification.Origin == linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return nil
		}
		if existing.classification.Origin != linuxOwnerAnchorRunChildOriginGateUserNSCurrent ||
			!existing.classification.Equal(classification) || existing.pidfd == nil {
			return errors.New("anchor namespace-derived process differs from its retained exact origin pin")
		}
		return verifyLinuxOwnerAnchorPIDFDIdentity(int(existing.pidfd.Fd()), process)
	}
	copyFD, err := unix.FcntlInt(uintptr(pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("retain namespace-derived adopted child pidfd: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(copyFD), "symmetry-owner-anchor-namespace-child-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(copyFD)
		return errors.New("wrap namespace-derived adopted child pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(pidfdFile.Fd()), process); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck retained namespace-derived child pidfd: %w", err)
	}
	if run.children == nil {
		run.children = make(map[int]linuxOwnerAnchorTrackedProcess)
	}
	run.children[process.PID] = linuxOwnerAnchorTrackedProcess{
		identity: process, classification: classification, role: "descendant", pidfd: pidfdFile,
	}
	return nil
}

func (run *linuxOwnerAnchorRun) trackPreReleaseGatePhysicalOnlyChild(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	classification linuxOwnerAnchorRunChildClassification,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, run.grant.Anchor) || run.retired || run.stopRequested || run.stopLeafAttempted || run.stopFence != nil || run.gateIdentity != nil ||
		run.gateReleaseAuthorized || run.gateUserNamespaceFD != nil || run.gateImageFD != nil || run.gateReadyAuthenticated ||
		process.Validate() != nil || pidfd < 0 ||
		classification.ValidateFor(run.grant, run.helper, process) != nil ||
		classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
		return errors.New("pre-RegisterGate physical-only child lacks exact helper-death and no-release evidence")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("verify physical-only gate pidfd: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("verify physical-only gate identity: %w", errOr(err, errors.New("creation identity differs")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != run.grant.Anchor.PID {
		return fmt.Errorf("verify physical-only gate Anchor parent: %w", errOr(err, errors.New("parent PID differs")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
		return err
	}
	if _, exists := run.children[process.PID]; exists {
		return errors.New("physical-only pre-release gate PID aliases a retained RunKey child")
	}
	launcherCount := 0
	for _, child := range run.children {
		if child.identity.Validate() != nil || child.role != "launcher" || child.creator.Validate() != nil || !child.creator.Equal(run.helper) || child.pidfd == nil ||
			child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
			child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return errors.New("physical-only pre-release gate lacks an exclusive exact root launcher producer")
		}
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("physical-only gate root launcher remains live: %w", errOr(err, errors.New("launcher pidfd is not ready")))
		}
		launcherCount++
	}
	if launcherCount != 1 {
		return errors.New("physical-only pre-release gate does not have exactly one registered root launcher")
	}
	if existing, exists := run.physicalOnlyChildren[process.PID]; exists {
		if !existing.identity.Equal(process) || !existing.classification.Equal(classification) || existing.pidfd == nil || existing.imageFD == nil {
			return errors.New("physical-only gate PID conflicts with its retained Anchor process identity")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(existing.pidfd.Fd()), process); err != nil {
			return err
		}
		return verifyLinuxOwnerAnchorGateImageFD(existing.imageFD, process, int(existing.pidfd.Fd()))
	}
	if len(run.physicalOnlyChildren) != 0 {
		return errors.New("run already has a different physical-only pre-release gate")
	}
	imageFD, err := pinLinuxOwnerAnchorGateImage(process, pidfd, run.owner)
	if err != nil {
		return fmt.Errorf("retain exact static gate image for physical-only containment: %w", err)
	}
	keepImage := false
	defer func() {
		if !keepImage {
			_ = imageFD.Close()
		}
	}()
	copyFD, err := unix.FcntlInt(uintptr(pidfd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("retain physical-only gate pidfd: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(copyFD), "symmetry-owner-anchor-physical-only-gate-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(copyFD)
		return errors.New("wrap physical-only gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(pidfdFile.Fd()), process); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck retained physical-only gate pidfd: %w", err)
	}
	if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck physical-only gate membership after image pin: %w", err)
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(imageFD, process, int(pidfdFile.Fd())); err != nil {
		_ = pidfdFile.Close()
		return fmt.Errorf("recheck retained physical-only gate image: %w", err)
	}
	if run.physicalOnlyChildren == nil {
		run.physicalOnlyChildren = make(map[int]linuxOwnerAnchorPhysicalOnlyChild)
	}
	run.physicalOnlyChildren[process.PID] = linuxOwnerAnchorPhysicalOnlyChild{
		identity: process, classification: classification, pidfd: pidfdFile, imageFD: imageFD,
	}
	keepImage = true
	return nil
}

func (anchor *linuxOwnerAnchor) reapPhysicalOnlyChild(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
) error {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil ||
		!run.grant.Anchor.Equal(anchor.identity) || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) || run.leaf == nil ||
		!(run.leafRemovedObserved || run.leafKillAttempted && run.leafKillAcknowledged && run.leafEmptyObserved) || process.Validate() != nil {
		return errors.New("physical-only gate wait lacks the exact helper-death and empty-leaf observation")
	}
	child, exists := run.physicalOnlyChildren[process.PID]
	if !exists || !child.identity.Equal(process) || child.pidfd == nil || child.waited ||
		child.imageFD == nil ||
		child.classification.ValidateFor(run.grant, run.helper, process) != nil ||
		child.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
		return errors.New("physical-only gate wait lacks its exact retained Anchor PIDFD")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(child.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("verify retained physical-only gate PIDFD before wait: %w", err)
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, child.pidfd, process); err != nil {
		return fmt.Errorf("wait/reap exact physical-only gate with P_PIDFD: %w", err)
	}
	child.waited = true
	run.physicalOnlyChildren[process.PID] = child
	return nil
}

func (run *linuxOwnerAnchorRun) registerGate(
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
	pidfd int,
	anchorUserNamespaceFD *os.File,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil ||
		gate.Validate() != nil || gate.HostUID != run.owner.AUID || !gate.Cgroup.Equal(run.grant.Leaf) ||
		!userNamespace.valid() || pidfd < 0 || anchorUserNamespaceFD == nil || run.gateReleaseAuthorized || run.stopRequested {
		return errors.New("Linux owner anchor trusted A-gate registration is incomplete or too late")
	}
	if run.gateIdentity != nil && (!run.gateIdentity.Equal(gate) || run.gateUserNamespace != userNamespace) {
		return errors.New("Linux owner anchor refuses a different gate or user namespace for the same run")
	}
	gateNamespaceFD, capturedNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(gate.Process, pidfd)
	if err != nil {
		return fmt.Errorf("pin exact parked-gate user namespace: %w", err)
	}
	keepNamespace := false
	defer func() {
		if !keepNamespace {
			_ = gateNamespaceFD.Close()
		}
	}()
	if capturedNamespace != userNamespace {
		return errors.New("parked-gate user namespace differs from its helper-supplied device/inode")
	}
	gateImageFD, err := pinLinuxOwnerAnchorGateImage(gate.Process, pidfd, run.owner)
	if err != nil {
		return fmt.Errorf("pin trusted installed static gate after authenticated READY: %w", err)
	}
	keepImage := false
	defer func() {
		if !keepImage {
			_ = gateImageFD.Close()
		}
	}()
	anchorNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchorUserNamespaceFD)
	if err != nil {
		return fmt.Errorf("verify pinned Anchor user namespace: %w", err)
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(gateNamespaceFD)
	if err != nil || parentNamespace != anchorNamespace {
		return fmt.Errorf("verify gate user namespace is a direct child of the pinned Anchor namespace: %w", errOr(err, errors.New("parent namespace differs")))
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("verify exact parked-gate user namespace and B-only maps before registration: %w", err)
	}
	if err := run.registerChild(gate.Process, run.helper, "gate", pidfd); err != nil {
		return err
	}
	tracked, exists := run.children[gate.Process.PID]
	if !exists || !tracked.identity.Equal(gate.Process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil {
		return errors.New("Linux owner anchor gate registration lacks its exact retained pidfd")
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("verify exact parked-gate user namespace and B-only maps: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), gate.Process); err != nil {
		return fmt.Errorf("recheck parked-gate pidfd after namespace verification: %w", err)
	}
	if actual, err := ProcessIdentity(gate.Process.PID); err != nil || actual != gate.Process.Identity {
		return fmt.Errorf("recheck parked-gate creation identity after namespace verification: %w", errOr(err, errors.New("identity changed")))
	}
	if actual, err := linuxOwnerAnchorUserNamespaceFDIdentity(gateNamespaceFD); err != nil || actual != userNamespace {
		return fmt.Errorf("recheck retained gate user namespace after registration: %w", errOr(err, errors.New("device or inode changed")))
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(gateImageFD, gate.Process, pidfd); err != nil {
		return fmt.Errorf("recheck exact trusted static gate after registration: %w", err)
	}
	if run.gateUserNamespaceFD != nil {
		actual, err := linuxOwnerAnchorUserNamespaceFDIdentity(run.gateUserNamespaceFD)
		if err != nil || actual != userNamespace {
			return fmt.Errorf("replay gate namespace pin differs from the exact registration: %w", errOr(err, errors.New("device or inode changed")))
		}
		if run.gateImageFD == nil {
			return errors.New("replayed gate registration lost its exact image pin")
		}
		var oldImage, newImage unix.Stat_t
		if err := unix.Fstat(int(run.gateImageFD.Fd()), &oldImage); err != nil {
			return fmt.Errorf("stat retained gate image on registration replay: %w", err)
		}
		if err := unix.Fstat(int(gateImageFD.Fd()), &newImage); err != nil || oldImage.Dev != newImage.Dev || oldImage.Ino != newImage.Ino {
			return fmt.Errorf("replayed gate image differs from its exact retained pin: %w", errOr(err, errors.New("device or inode differs")))
		}
		return nil
	}
	gateCopy := gate
	run.gateIdentity = &gateCopy
	run.gateUserNamespace = userNamespace
	run.gateUserNamespaceFD = gateNamespaceFD
	run.gateImageFD = gateImageFD
	run.gateReadyAuthenticated = true
	keepNamespace = true
	keepImage = true
	return nil
}

func (run *linuxOwnerAnchorRun) authorizeGateRelease(
	gate authority.LinuxCgroupGateIdentity,
	userNamespace LinuxUserNamespaceIdentity,
) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperExit != nil || run.stopRequested ||
		run.helperPIDFD == nil || run.gateIdentity == nil || !run.gateIdentity.Equal(gate) ||
		run.gateUserNamespace != userNamespace || !userNamespace.valid() || run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux owner anchor gate-release authorization differs from the registered gate")
	}
	tracked, exists := run.children[gate.Process.PID]
	if !exists || !tracked.identity.Equal(gate.Process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, gate.Process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux owner anchor gate-release authorization lacks its exact parked gate")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
		return fmt.Errorf("verify exact helper before gate-release authorization: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), gate.Process); err != nil {
		return fmt.Errorf("verify exact parked gate before release authorization: %w", err)
	}
	if actual, err := ProcessIdentity(gate.Process.PID); err != nil || actual != gate.Process.Identity {
		return fmt.Errorf("verify parked gate identity before release authorization: %w", errOr(err, errors.New("creation identity changed")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(gate.Process.PID)
	if err != nil || credentials.Parent != run.helper.PID {
		return fmt.Errorf("verify parked gate ancestry before release authorization: %w", errOr(err, errors.New("parent PID differs from exact helper")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(gate.Process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerRunChildMembership(gate.Process.PID, "gate", run.grant, run.leaf); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(gate.Process.PID, userNamespace, run.owner); err != nil {
		return fmt.Errorf("revalidate exact parked-gate user namespace before release: %w", err)
	}
	if run.gateReleaseAuthorized {
		return nil
	}
	run.gateReleaseAuthorized = true
	return nil
}

func (run *linuxOwnerAnchorRun) verifyRegisteredGateA(process authority.LinuxCgroupProcessIdentity, verifyMembership bool) error {
	if run == nil || run.grant.Validate() != nil || process.Validate() != nil || run.gateIdentity == nil ||
		!run.gateIdentity.Process.Equal(process) || !run.gateUserNamespace.valid() || run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux parked gate differs from its exact registered process and user namespace")
	}
	tracked, exists := run.children[process.PID]
	if !exists || !tracked.identity.Equal(process) || !tracked.creator.Equal(run.helper) || tracked.role != "gate" || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux parked gate lacks its exact retained A-gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("verify exact parked-gate pidfd during anchor reap: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("verify exact parked-gate creation identity during anchor reap: %w", errOr(err, errors.New("identity changed")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != run.grant.Anchor.PID {
		return fmt.Errorf("verify parked-gate anchor ancestry: %w", errOr(err, errors.New("parent PID differs from anchor")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(process.PID, run.gateUserNamespace, run.owner); err != nil {
		return fmt.Errorf("verify parked-gate user namespace during anchor reap: %w", err)
	}
	if verifyMembership && !run.leafRemovedObserved {
		if err := verifyLinuxOwnerRunChildMembership(process.PID, "gate", run.grant, run.leaf); err != nil {
			return err
		}
	}
	if run.leafRemovedObserved {
		ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("live parked gate remains after exact run-leaf removal: %w", errOr(err, errors.New("pidfd is not ready")))
		}
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("recheck exact parked-gate pidfd during anchor reap: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("recheck parked-gate creation identity during anchor reap: %w", errOr(err, errors.New("identity changed")))
	}
	return nil
}

func (run *linuxOwnerAnchorRun) verifyAuthorizedGateExec(process authority.LinuxCgroupProcessIdentity, verifyMembership bool) error {
	if run == nil || run.grant.Validate() != nil || process.Validate() != nil || run.gateIdentity == nil ||
		!run.gateReleaseAuthorized || !run.gateIdentity.Process.Equal(process) || !run.gateUserNamespace.valid() ||
		run.verifyPinnedGateUserNamespace() != nil {
		return errors.New("Linux native process lacks exact pre-registered gate and one-use release authorization")
	}
	tracked, exists := run.children[process.PID]
	if !exists || !tracked.identity.Equal(process) || !tracked.creator.Equal(run.helper) ||
		(tracked.role != "gate" && tracked.role != "native") || tracked.pidfd == nil ||
		tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
		tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
		return errors.New("Linux native process differs from its exact retained A-gate pidfd")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("verify same-PID native pidfd: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("verify same-PID native creation identity: %w", errOr(err, errors.New("identity changed")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "native", run.owner); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorGateNamespace(process.PID, run.gateUserNamespace, run.owner); err != nil {
		return fmt.Errorf("verify same-PID native user namespace and B-only maps: %w", err)
	}
	if err := verifyLinuxOwnerAnchorNativePrivilegeState(process.PID); err != nil {
		return err
	}
	if verifyMembership && !run.leafRemovedObserved {
		if err := verifyLinuxOwnerRunChildMembership(process.PID, "native", run.grant, run.leaf); err != nil {
			return err
		}
	}
	if run.leafRemovedObserved {
		ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
		if err != nil || !ready {
			return fmt.Errorf("live native process remains after exact run-leaf removal: %w", errOr(err, errors.New("pidfd is not ready")))
		}
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
		return fmt.Errorf("recheck same-PID native pidfd: %w", err)
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return fmt.Errorf("recheck same-PID native creation identity: %w", errOr(err, errors.New("identity changed")))
	}
	return nil
}

func (run *linuxOwnerAnchorRun) verifyPinnedGateUserNamespace() error {
	if run == nil || run.gateUserNamespaceFD == nil || run.gateImageFD == nil ||
		!run.gateReadyAuthenticated || !run.gateUserNamespace.valid() {
		return errors.New("Linux owner run lacks its authenticated GateReady, image, or user-namespace pin")
	}
	identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(run.gateUserNamespaceFD)
	if err != nil || identity != run.gateUserNamespace {
		return fmt.Errorf("verify retained gate user-namespace FD: %w", errOr(err, errors.New("device or inode differs")))
	}
	var imageStat unix.Stat_t
	if err := unix.Fstat(int(run.gateImageFD.Fd()), &imageStat); err != nil ||
		imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return fmt.Errorf("verify retained static gate image FD: %w", errOr(err, errors.New("root-owned image identity differs")))
	}
	return nil
}

func verifyLinuxOwnerAnchorGateNamespace(pid int, expected LinuxUserNamespaceIdentity, owner linuxgate.OwnerConfig) error {
	if pid <= 0 || !expected.valid() || owner.Validate() != nil {
		return errors.New("Linux owner anchor gate namespace identity is incomplete")
	}
	actual, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil || actual != expected {
		return fmt.Errorf("Linux owner anchor gate user namespace differs from its pre-release pin: %w", errOr(err, errors.New("device or inode differs")))
	}
	uidMap, err := readLinuxOwnerAnchorProcText(pid, "uid_map", 4096)
	if err != nil {
		return fmt.Errorf("read exact gate uid_map: %w", err)
	}
	gidMap, err := readLinuxOwnerAnchorProcText(pid, "gid_map", 4096)
	if err != nil {
		return fmt.Errorf("read exact gate gid_map: %w", err)
	}
	setgroups, err := readLinuxOwnerAnchorProcText(pid, "setgroups", 256)
	if err != nil {
		return fmt.Errorf("read exact gate setgroups policy: %w", err)
	}
	if canonicalLinuxOwnerAnchorMap(uidMap) != fmt.Sprintf("1 %d 1", owner.BUID) ||
		canonicalLinuxOwnerAnchorMap(gidMap) != fmt.Sprintf("1 %d 1", owner.BGID) || strings.TrimSpace(setgroups) != "deny" {
		return errors.New("Linux owner anchor gate user namespace is not the exact fixed-B-only map with setgroups denied")
	}
	rechecked, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil || rechecked != expected {
		return fmt.Errorf("recheck Linux owner anchor gate user namespace identity: %w", errOr(err, errors.New("device or inode changed")))
	}
	return nil
}

func verifyLinuxOwnerAnchorGateImage(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	owner linuxgate.OwnerConfig,
) error {
	if process.Validate() != nil || pidfd < 0 || owner.Validate() != nil {
		return errors.New("trusted Linux gate image check is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("verify exact gate pidfd before image readback: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("verify exact gate process before image readback: %w", errOr(err, errors.New("creation identity differs")))
	}
	for _, directory := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry"} {
		fd, err := unix.Open(directory, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open fixed trusted gate image directory %q: %w", directory, err)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0 {
			return errors.Join(fmt.Errorf("fixed gate image directory %q is not root-owned and non-writable", directory), statErr, closeErr)
		}
	}
	imageFD, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed installed Linux gate image: %w", err)
	}
	defer unix.Close(imageFD)
	var imageStat unix.Stat_t
	if err := unix.Fstat(imageFD, &imageStat); err != nil {
		return fmt.Errorf("stat fixed installed Linux gate image: %w", err)
	}
	if imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return errors.New("fixed Linux gate image is not singly-linked, root-owned, executable, and immutable to A/B")
	}
	readFD, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed Linux gate image for static-ELF verification: %w", err)
	}
	readFile := os.NewFile(uintptr(readFD), "symmetry-owner-anchor-gate-image-read")
	if readFile == nil {
		_ = unix.Close(readFD)
		return errors.New("wrap fixed Linux gate image read FD")
	}
	defer readFile.Close()
	var readStat unix.Stat_t
	if err := unix.Fstat(readFD, &readStat); err != nil || readStat.Dev != imageStat.Dev || readStat.Ino != imageStat.Ino {
		return fmt.Errorf("recheck fixed Linux gate image inode for ELF verification: %w", errOr(err, errors.New("inode changed")))
	}
	image, err := elf.NewFile(readFile)
	if err != nil {
		return fmt.Errorf("parse fixed Linux gate ELF image: %w", err)
	}
	for _, program := range image.Progs {
		if program.Type == elf.PT_INTERP {
			_ = image.Close()
			return errors.New("fixed Linux gate ELF image is dynamically linked")
		}
	}
	staticAMD64 := image.Class == elf.ELFCLASS64 && image.Data == elf.ELFDATA2LSB && image.Machine == elf.EM_X86_64
	_ = image.Close()
	if !staticAMD64 {
		return errors.New("fixed Linux gate ELF image is not static Linux/amd64")
	}
	var processStat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", process.PID), &processStat); err != nil {
		return fmt.Errorf("read exact gate executable inode: %w", err)
	}
	if processStat.Mode&unix.S_IFMT != unix.S_IFREG || processStat.Dev != imageStat.Dev || processStat.Ino != imageStat.Ino {
		return errors.New("exact gate process is not executing the pinned static A gate image")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("recheck exact gate pidfd after image readback: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("recheck exact gate process after image readback: %w", errOr(err, errors.New("creation identity differs")))
	}
	return nil
}

func pinLinuxOwnerAnchorGateImage(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	owner linuxgate.OwnerConfig,
) (*os.File, error) {
	if err := verifyLinuxOwnerAnchorGateImage(process, pidfd, owner); err != nil {
		return nil, err
	}
	fd, err := unix.Open(linuxOwnerAnchorGateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("retain fixed installed Linux gate image: %w", err)
	}
	image := os.NewFile(uintptr(fd), "symmetry-owner-anchor-pinned-gate-image")
	if image == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap retained static gate image FD")
	}
	if err := verifyLinuxOwnerAnchorGateImageFD(image, process, pidfd); err != nil {
		_ = image.Close()
		return nil, err
	}
	return image, nil
}

func verifyLinuxOwnerAnchorGateImageFD(
	image *os.File,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
) error {
	if image == nil || process.Validate() != nil || pidfd < 0 {
		return errors.New("pinned gate image/process identity is incomplete")
	}
	var imageStat unix.Stat_t
	if err := unix.Fstat(int(image.Fd()), &imageStat); err != nil {
		return fmt.Errorf("stat retained fixed gate image: %w", err)
	}
	if imageStat.Mode&unix.S_IFMT != unix.S_IFREG || imageStat.Uid != 0 || imageStat.Gid != 0 ||
		imageStat.Mode&0022 != 0 || imageStat.Mode&0111 == 0 || imageStat.Nlink != 1 {
		return errors.New("retained Linux gate image lost its root-owned non-writable identity")
	}
	var processStat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", process.PID), &processStat); err != nil {
		return fmt.Errorf("read exact gate executable against retained image pin: %w", err)
	}
	if processStat.Mode&unix.S_IFMT != unix.S_IFREG || processStat.Dev != imageStat.Dev || processStat.Ino != imageStat.Ino {
		return errors.New("exact gate process executable differs from the retained root-owned static image")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return fmt.Errorf("recheck exact gate pidfd against retained image pin: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return fmt.Errorf("recheck exact gate process against retained image pin: %w", errOr(err, errors.New("creation identity differs")))
	}
	return nil
}

type linuxOwnerAnchorNamespacePin struct {
	runKey    LinuxOwnerAnchorRunKey
	namespace LinuxUserNamespaceIdentity
	fd        *os.File
}

func openLinuxOwnerAnchorUserNamespaceFD(
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
) (*os.File, LinuxUserNamespaceIdentity, error) {
	if process.Validate() != nil || pidfd < -1 || pidfd < 0 && process.PID != os.Getpid() {
		return nil, LinuxUserNamespaceIdentity{}, errors.New("pin exact Linux process user namespace requires a valid process identity and pidfd")
	}
	if pidfd >= 0 {
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("verify process pidfd before user-namespace pin: %w", err)
		}
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("verify process identity before user-namespace pin: %w", errOr(err, errors.New("creation identity differs")))
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/ns/user", process.PID), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("open exact process user namespace: %w", err)
	}
	namespaceFile := os.NewFile(uintptr(fd), "symmetry-owner-anchor-user-namespace")
	if namespaceFile == nil {
		_ = unix.Close(fd)
		return nil, LinuxUserNamespaceIdentity{}, errors.New("wrap exact process user namespace FD")
	}
	namespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(namespaceFile)
	if err != nil {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, err
	}
	current, err := readLinuxUserNamespaceIdentity(process.PID)
	if err != nil || current != namespace {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("compare pinned nsfs inode with exact process namespace: %w", errOr(err, errors.New("device or inode differs")))
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		_ = namespaceFile.Close()
		return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("recheck process identity after user-namespace pin: %w", errOr(err, errors.New("creation identity differs")))
	}
	if pidfd >= 0 {
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			_ = namespaceFile.Close()
			return nil, LinuxUserNamespaceIdentity{}, fmt.Errorf("recheck process pidfd after user-namespace pin: %w", err)
		}
	}
	runtime.KeepAlive(namespaceFile)
	return namespaceFile, namespace, nil
}

func linuxOwnerAnchorUserNamespaceFDIdentity(namespace *os.File) (LinuxUserNamespaceIdentity, error) {
	if namespace == nil {
		return LinuxUserNamespaceIdentity{}, errors.New("Linux user-namespace FD is unavailable")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(namespace.Fd()), &stat); err != nil {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("stat pinned Linux user-namespace FD: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return LinuxUserNamespaceIdentity{}, errors.New("pinned Linux user-namespace FD is not an nsfs inode")
	}
	namespaceType, err := unix.IoctlRetInt(int(namespace.Fd()), unix.NS_GET_NSTYPE)
	if err != nil || namespaceType != unix.CLONE_NEWUSER {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("verify pinned nsfs object is a user namespace: %w", errOr(err, errors.New("namespace type differs")))
	}
	identity := LinuxUserNamespaceIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
	if !identity.valid() {
		return LinuxUserNamespaceIdentity{}, errors.New("pinned Linux user-namespace identity is invalid")
	}
	return identity, nil
}

func linuxOwnerAnchorUserNamespaceParentIdentity(namespace *os.File) (LinuxUserNamespaceIdentity, error) {
	if _, err := linuxOwnerAnchorUserNamespaceFDIdentity(namespace); err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	parentFD, err := unix.IoctlRetInt(int(namespace.Fd()), unix.NS_GET_PARENT)
	if err != nil {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("read parent user namespace with NS_GET_PARENT: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), "symmetry-owner-anchor-parent-user-namespace")
	if parent == nil {
		_ = unix.Close(parentFD)
		return LinuxUserNamespaceIdentity{}, errors.New("wrap parent Linux user-namespace FD")
	}
	defer parent.Close()
	return linuxOwnerAnchorUserNamespaceFDIdentity(parent)
}

// linuxOwnerAnchorUserNamespaceRun returns the exact active RunKey whose
// pinned gate namespace is an ancestor of process. The Anchor's own pinned
// user namespace is the traversal stop: an inaccessible parent, malformed
// nsfs object, changed PID, or unstable namespace returns an error and cannot
// be interpreted as current-run origin.
func linuxOwnerAnchorUserNamespaceRun(
	ctx context.Context,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	pins []linuxOwnerAnchorNamespacePin,
	anchorNamespaceFD *os.File,
) (LinuxOwnerAnchorRunKey, bool, error) {
	if ctx == nil || process.Validate() != nil || pidfd < 0 || anchorNamespaceFD == nil {
		return LinuxOwnerAnchorRunKey{}, false, errors.New("Linux run user-namespace ancestry check is incomplete")
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify process pidfd before user-namespace ancestry check: %w", err)
	}
	anchorNamespace, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchorNamespaceFD)
	if err != nil {
		return LinuxOwnerAnchorRunKey{}, false, err
	}
	for index, pin := range pins {
		if !validLinuxOwnerAnchorRunID(pin.runKey.RunID) || pin.runKey.Generation <= 0 || pin.fd == nil || !pin.namespace.valid() {
			return LinuxOwnerAnchorRunKey{}, false, errors.New("active RunKey gate user-namespace pin is incomplete")
		}
		identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(pin.fd)
		if err != nil || identity != pin.namespace || identity == anchorNamespace {
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify active RunKey gate user-namespace pin: %w", errOr(err, errors.New("identity is invalid or aliases the Anchor namespace")))
		}
		for previous := 0; previous < index; previous++ {
			if pins[previous].runKey == pin.runKey || pins[previous].namespace == pin.namespace {
				return LinuxOwnerAnchorRunKey{}, false, errors.New("active RunKey gate user-namespace pins are not distinct")
			}
		}
	}
	processNamespaceFD, processNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(process, pidfd)
	if err != nil {
		return LinuxOwnerAnchorRunKey{}, false, err
	}
	defer processNamespaceFD.Close()
	current := processNamespaceFD
	currentOwned := false
	defer func() {
		if currentOwned && current != nil {
			_ = current.Close()
		}
	}()
	var match LinuxOwnerAnchorRunKey
	found := false
	for depth := 0; depth <= linuxOwnerAnchorMaxUserNamespaceDepth; depth++ {
		if err := ctx.Err(); err != nil {
			return LinuxOwnerAnchorRunKey{}, false, err
		}
		currentIdentity, err := linuxOwnerAnchorUserNamespaceFDIdentity(current)
		if err != nil {
			return LinuxOwnerAnchorRunKey{}, false, err
		}
		if currentIdentity == anchorNamespace {
			break
		}
		for _, pin := range pins {
			if currentIdentity == pin.namespace {
				match = pin.runKey
				found = true
				break
			}
		}
		if found {
			break
		}
		if depth == linuxOwnerAnchorMaxUserNamespaceDepth {
			return LinuxOwnerAnchorRunKey{}, false, errors.New("user-namespace ancestry exceeds the kernel's bounded nesting depth")
		}
		parentFD, err := unix.IoctlRetInt(int(current.Fd()), unix.NS_GET_PARENT)
		if err != nil {
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("walk bounded NS_GET_PARENT chain: %w", err)
		}
		parent := os.NewFile(uintptr(parentFD), "symmetry-owner-anchor-user-namespace-parent")
		if parent == nil {
			_ = unix.Close(parentFD)
			return LinuxOwnerAnchorRunKey{}, false, errors.New("wrap parent user-namespace ancestry FD")
		}
		parentIdentity, err := linuxOwnerAnchorUserNamespaceFDIdentity(parent)
		if err != nil || parentIdentity == currentIdentity {
			_ = parent.Close()
			return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("verify parent user-namespace ancestry FD: %w", errOr(err, errors.New("namespace ancestry cycle")))
		}
		if currentOwned {
			_ = current.Close()
		}
		current = parent
		currentOwned = true
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process identity after user-namespace ancestry: %w", errOr(err, errors.New("creation identity differs")))
	}
	currentProcessNamespace, err := readLinuxUserNamespaceIdentity(process.PID)
	if err != nil || currentProcessNamespace != processNamespace {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process user namespace after ancestry walk: %w", errOr(err, errors.New("device or inode changed")))
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return LinuxOwnerAnchorRunKey{}, false, fmt.Errorf("recheck process pidfd after user-namespace ancestry: %w", err)
	}
	return match, found, nil
}

func (anchor *linuxOwnerAnchor) classifyRunChildOrigin(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
) (linuxOwnerAnchorRunChildClassification, error) {
	unknown := linuxOwnerAnchorUnknownChildClassification(run, process)
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil ||
		!run.grant.Anchor.Equal(anchor.identity) || process.Validate() != nil || pidfd < 0 {
		return unknown, errors.New("Linux owner anchor child-origin classifier is not bound to an exact active run")
	}
	if active, exists := anchor.runs[run.grant.RunKey]; !exists || active != run || !run.grant.Equal(active.grant) {
		return unknown, errors.New("Linux owner anchor child-origin classifier run is not the retained exact RunKey")
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return unknown, fmt.Errorf("verify exact process pidfd before origin classification: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return unknown, fmt.Errorf("verify process identity before origin classification: %w", errOr(err, errors.New("creation identity differs")))
	}
	if physicalOnly, exists := run.physicalOnlyChildren[process.PID]; exists {
		if !physicalOnly.identity.Equal(process) || physicalOnly.pidfd == nil || physicalOnly.imageFD == nil ||
			physicalOnly.classification.ValidateFor(run.grant, run.helper, process) != nil ||
			physicalOnly.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
			return unknown, errors.New("physical-only gate PID conflicts with its retained exact Anchor pin")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(physicalOnly.pidfd.Fd()), process); err != nil {
			return unknown, fmt.Errorf("verify retained physical-only gate pidfd: %w", err)
		}
		if err := verifyLinuxOwnerAnchorGateImageFD(physicalOnly.imageFD, process, int(physicalOnly.pidfd.Fd())); err != nil {
			return unknown, fmt.Errorf("verify retained physical-only static gate image: %w", err)
		}
		return physicalOnly.classification, nil
	}
	trackedAtStart, trackedAtStartExists := run.children[process.PID]
	emptyAfterStop := run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved
	if trackedAtStartExists && trackedAtStart.identity.Equal(process) && trackedAtStart.pidfd != nil &&
		trackedAtStart.role == trackedAtStart.classification.Role &&
		trackedAtStart.classification.currentRunMember() &&
		trackedAtStart.classification.ValidateFor(run.grant, run.helper, process) == nil && emptyAfterStop {
		ready, err := linuxOwnerAnchorPIDFDReady(trackedAtStart.pidfd)
		if err != nil {
			return unknown, fmt.Errorf("poll cached namespace-proven child pidfd: %w", err)
		}
		if ready {
			if trackedAtStart.classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent ||
				trackedAtStart.role == "gate" || trackedAtStart.role == "native" {
				if err := run.verifyPinnedGateUserNamespace(); err != nil {
					return unknown, err
				}
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(trackedAtStart.pidfd.Fd()), process); err != nil {
				return unknown, fmt.Errorf("recheck cached namespace-proven child pidfd: %w", err)
			}
			return trackedAtStart.classification, nil
		}
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil {
		return unknown, fmt.Errorf("read process cgroup before origin classification: %w", err)
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	processInTargetLeaf := linuxOwnerCgroupPathWithin(cgroupPath, leafPath)
	var sourceRun *linuxOwnerAnchorRun
	var sourceKey LinuxOwnerAnchorRunKey
	var tracked *linuxOwnerAnchorTrackedProcess
	for key, candidate := range anchor.runs {
		if err := ctx.Err(); err != nil {
			return unknown, err
		}
		if candidate == nil || candidate.retired {
			continue
		}
		if candidate.grant.Validate() != nil || candidate.grant.RunKey != key || candidate.helper.Validate() != nil {
			return unknown, errors.New("anchor has an invalid active RunKey while classifying a process")
		}
		if physicalOnly, exists := candidate.physicalOnlyChildren[process.PID]; exists {
			if candidate.helperExit == nil || !candidate.helperExit.ValidFor(candidate.helper, anchor.identity) ||
				!physicalOnly.identity.Equal(process) || physicalOnly.pidfd == nil || physicalOnly.imageFD == nil || physicalOnly.waited ||
				physicalOnly.classification.ValidateFor(candidate.grant, candidate.helper, process) != nil ||
				physicalOnly.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
				return unknown, errors.New("physical-only foreign gate differs from its retained exact PIDFD origin")
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(physicalOnly.pidfd.Fd()), process); err != nil {
				return unknown, fmt.Errorf("verify physical-only source gate pidfd: %w", err)
			}
			if err := verifyLinuxOwnerAnchorGateImageFD(physicalOnly.imageFD, process, int(physicalOnly.pidfd.Fd())); err != nil {
				return unknown, fmt.Errorf("verify physical-only source gate image: %w", err)
			}
			if key == run.grant.RunKey {
				return physicalOnly.classification, nil
			}
			if processInTargetLeaf {
				run.migrationObserved = true
			}
			return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath,
				linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign, LinuxOwnerAnchorRunKey{}, "pre_release_gate")
		}
		if candidate.helper.Equal(process) {
			if sourceRun != nil {
				return unknown, errors.New("process identity collides with multiple active RunKeys")
			}
			sourceRun = candidate
			sourceKey = key
			continue
		}
		if child, exists := candidate.children[process.PID]; exists {
			if !child.identity.Equal(process) {
				return unknown, errors.New("anchor child PID conflicts with a retained process identity")
			}
			if sourceRun != nil {
				return unknown, errors.New("process identity collides with multiple active RunKeys")
			}
			childCopy := child
			sourceRun = candidate
			sourceKey = key
			tracked = &childCopy
		}
	}
	if sourceRun != nil {
		if sourceRun.helper.Validate() != nil || !sourceRun.grant.Anchor.Equal(anchor.identity) {
			return unknown, errors.New("registered process source RunKey differs from the exact Anchor")
		}
		if sourceRun.helper.Equal(process) {
			if sourceRun.helperPIDFD == nil || sourceRun.helperExit != nil ||
				verifyLinuxOwnerAnchorPIDFDIdentity(int(sourceRun.helperPIDFD.Fd()), process) != nil {
				return unknown, errors.New("registered helper process is not pinned as an exact live child")
			}
			if sourceKey == run.grant.RunKey {
				if processInTargetLeaf {
					run.migrationObserved = true
				}
				return unknown, errors.New("helper itself is not a run-child origin candidate")
			}
			if processInTargetLeaf {
				run.migrationObserved = true
			}
			return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd,
				cgroupPath, linuxOwnerAnchorRunChildOriginRegisteredForeign, sourceKey, "")
		}
		if tracked == nil || tracked.pidfd == nil || tracked.classification.ValidateFor(sourceRun.grant, sourceRun.helper, process) != nil {
			return unknown, errors.New("registered child source lacks its exact retained PIDFD and RunKey classification")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), process); err != nil {
			return unknown, fmt.Errorf("verify retained source-run child pidfd: %w", err)
		}
		switch tracked.classification.Origin {
		case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
			if tracked.creator.Validate() != nil || tracked.classification.OriginRunKey != sourceKey {
				return unknown, errors.New("registered source-run child lacks its exact observed creator edge")
			}
			if linuxOwnerCgroupPathWithin(cgroupPath, path.Join(sourceRun.grant.Parent.Path, sourceRun.grant.Leaf.LeafName)) {
				if err := validateLinuxOwnerAnchorCreatorChain(ctx, sourceRun, *tracked); err != nil {
					return unknown, fmt.Errorf("verify registered source-run creator chain: %w", err)
				}
			}
		case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
			if tracked.creator.Validate() == nil || tracked.classification.OriginRunKey != sourceKey ||
				tracked.role != "descendant" {
				return unknown, errors.New("namespace-derived source-run child carries a synthetic creator edge")
			}
			pins, err := anchor.activeGateNamespacePins()
			if err != nil {
				return unknown, err
			}
			originRun, found, err := linuxOwnerAnchorUserNamespaceRun(ctx, process, pidfd, pins, anchor.userNamespaceFD)
			if err != nil || !found || originRun != sourceKey {
				return unknown, fmt.Errorf("recheck namespace-derived source-run origin: %w", errOr(err, errors.New("gate namespace ancestry differs")))
			}
		default:
			return unknown, errors.New("retained source-run child has an unsupported origin classification")
		}
		if sourceKey == run.grant.RunKey {
			if tracked.classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent {
				if run.leafKillAttempted && run.leafKillAcknowledged && run.leafEmptyObserved {
					ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
					if err != nil {
						return unknown, fmt.Errorf("recheck cached namespace-proven child pidfd after cgroup.empty: %w", err)
					}
					if ready {
						if err := tracked.classification.ValidateFor(run.grant, run.helper, process); err != nil {
							return unknown, err
						}
						return tracked.classification, nil
					}
				}
				if !processInTargetLeaf {
					run.migrationObserved = true
					return anchor.makeLinuxOwnerAnchorChildClassification(run, process, sourceKey, "descendant",
						linuxOwnerAnchorRunChildOriginCurrentRunEscaped), nil
				}
				if err := verifyLinuxOwnerRunChildCredentials(process.PID, "native", run.owner); err != nil {
					return unknown, err
				}
				return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath,
					linuxOwnerAnchorRunChildOriginGateUserNSCurrent, sourceKey, "descendant")
			}
			if tracked.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
				return unknown, errors.New("current-run child classification is not registered or namespace-proven")
			}
			role := tracked.role
			if role == "launcher" && cgroupPath != run.grant.Parent.Path || role != "launcher" && !processInTargetLeaf {
				run.migrationObserved = true
				return anchor.makeLinuxOwnerAnchorChildClassification(run, process, sourceKey, role,
					linuxOwnerAnchorRunChildOriginCurrentRunEscaped), nil
			}
			switch role {
			case "launcher":
				if err := verifyLinuxOwnerRunChildCredentials(process.PID, role, run.owner); err != nil {
					return unknown, err
				}
			case "gate":
				if processInTargetLeaf && run.gateReleaseAuthorized {
					if err := run.verifyAuthorizedGateExec(process, true); err == nil {
						role = "native"
						tracked.role = role
						tracked.classification.Role = role
						run.children[process.PID] = *tracked
					} else if err := run.verifyRegisteredGateA(process, true); err != nil {
						return unknown, fmt.Errorf("verify registered gate/native transition during origin classification: %w", errors.Join(err))
					}
				} else if err := run.verifyRegisteredGateA(process, true); err != nil {
					return unknown, err
				}
			case "native":
				if err := run.verifyAuthorizedGateExec(process, processInTargetLeaf); err != nil {
					return unknown, err
				}
			case "descendant":
				if err := verifyLinuxOwnerRunChildCredentials(process.PID, role, run.owner); err != nil {
					return unknown, err
				}
				if err := validateLinuxOwnerAnchorCreatorChain(ctx, run, *tracked); err != nil {
					return unknown, fmt.Errorf("verify current-run registered creator chain: %w", err)
				}
			default:
				return unknown, errors.New("registered current-run child has an unsupported process role")
			}
			return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath,
				linuxOwnerAnchorRunChildOriginRegisteredCurrent, sourceKey, role)
		}
		if processInTargetLeaf {
			run.migrationObserved = true
		}
		return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath,
			linuxOwnerAnchorRunChildOriginRegisteredForeign, sourceKey, tracked.role)
	}
	if process.Equal(run.helper) || process.Equal(anchor.identity) {
		return unknown, errors.New("Anchor/helper process cannot be classified as an unregistered run descendant")
	}
	pins, err := anchor.activeGateNamespacePins()
	if err != nil {
		return unknown, err
	}
	originRunKey, found, err := linuxOwnerAnchorUserNamespaceRun(ctx, process, pidfd, pins, anchor.userNamespaceFD)
	if err != nil {
		return unknown, err
	}
	if !found {
		physicalOnly, eligible, err := anchor.classifyUnregisteredPreReleaseGate(ctx, run, process, pidfd, cgroupPath)
		if err != nil {
			return unknown, err
		}
		if eligible {
			return physicalOnly, nil
		}
		run.migrationObserved = true
		return unknown, nil
	}
	origin := linuxOwnerAnchorClassifyNamespaceMembership(run.grant.RunKey, originRunKey, cgroupPath, leafPath)
	if origin == linuxOwnerAnchorRunChildOriginOtherUserNSForeign {
		if processInTargetLeaf {
			run.migrationObserved = true
		}
		return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath, origin, originRunKey, "descendant")
	}
	if origin == linuxOwnerAnchorRunChildOriginUnknown {
		run.migrationObserved = true
		return unknown, nil
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "native", run.owner); err != nil {
		return unknown, fmt.Errorf("verify unregistered B descendant host credentials: %w", err)
	}
	if origin == linuxOwnerAnchorRunChildOriginCurrentRunEscaped {
		run.migrationObserved = true
		return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath, origin, originRunKey, "descendant")
	}
	return anchor.finishLinuxOwnerAnchorChildClassification(ctx, run, process, pidfd, cgroupPath,
		linuxOwnerAnchorRunChildOriginGateUserNSCurrent, originRunKey, "descendant")
}

func (anchor *linuxOwnerAnchor) activeGateNamespacePins() ([]linuxOwnerAnchorNamespacePin, error) {
	if anchor == nil || anchor.userNamespaceFD == nil || !anchor.userNamespace.valid() {
		return nil, errors.New("Anchor host user namespace pin is unavailable")
	}
	identity, err := linuxOwnerAnchorUserNamespaceFDIdentity(anchor.userNamespaceFD)
	if err != nil || identity != anchor.userNamespace {
		return nil, fmt.Errorf("verify exact Anchor user namespace pin: %w", errOr(err, errors.New("device or inode differs")))
	}
	pins := make([]linuxOwnerAnchorNamespacePin, 0, len(anchor.runs))
	for key, run := range anchor.runs {
		if run == nil || run.retired {
			continue
		}
		if run.grant.Validate() != nil || run.grant.RunKey != key {
			return nil, errors.New("Anchor has an invalid active RunKey while building namespace origin pins")
		}
		if run.gateIdentity == nil {
			if run.gateUserNamespaceFD != nil {
				return nil, errors.New("Anchor run has a user-namespace FD without an exact gate identity")
			}
			continue
		}
		if err := run.verifyPinnedGateUserNamespace(); err != nil {
			return nil, err
		}
		parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(run.gateUserNamespaceFD)
		if err != nil || parentNamespace != anchor.userNamespace {
			return nil, fmt.Errorf("verify active gate user namespace parent: %w", errOr(err, errors.New("parent namespace differs from Anchor")))
		}
		pins = append(pins, linuxOwnerAnchorNamespacePin{runKey: key, namespace: run.gateUserNamespace, fd: run.gateUserNamespaceFD})
	}
	sort.Slice(pins, func(left, right int) bool {
		if pins[left].runKey.RunID == pins[right].runKey.RunID {
			return pins[left].runKey.Generation < pins[right].runKey.Generation
		}
		return pins[left].runKey.RunID < pins[right].runKey.RunID
	})
	return pins, nil
}

func (anchor *linuxOwnerAnchor) classifyUnregisteredPreReleaseGate(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	cgroupPath string,
) (linuxOwnerAnchorRunChildClassification, bool, error) {
	if anchor == nil || ctx == nil || run == nil || run.grant.Validate() != nil ||
		run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) ||
		run.retired || run.gateIdentity != nil || run.gateUserNamespaceFD != nil || run.gateImageFD != nil ||
		run.gateReadyAuthenticated || run.gateReleaseAuthorized || run.stopRequested || run.stopLeafAttempted || run.stopFence != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	leafPath := path.Join(run.grant.Parent.Path, run.grant.Leaf.LeafName)
	if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate pidfd: %w", err)
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate identity: %w", errOr(err, errors.New("creation identity differs")))
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != anchor.identity.PID {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate Anchor parent: %w", errOr(err, errors.New("parent PID differs")))
	}
	if err := verifyLinuxOwnerRunChildCredentials(process.PID, "gate", run.owner); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	launcherCount := 0
	for _, child := range run.children {
		if child.role != "launcher" {
			// A physical-only gate is accepted only in the narrow fork gap
			// after the run's sole registered root launcher and before any gate
			// or native identity was installed.
			return linuxOwnerAnchorRunChildClassification{}, false, nil
		}
		if child.identity.Validate() != nil || child.creator.Validate() != nil || !child.creator.Equal(run.helper) ||
			child.pidfd == nil || child.classification.ValidateFor(run.grant, run.helper, child.identity) != nil ||
			child.classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent {
			return linuxOwnerAnchorRunChildClassification{}, false, errors.New("pre-release gate source lacks the exact registered root launcher")
		}
		// RegisterProcess already bound this retained PIDFD to the exact
		// launcher start identity. The helper may have waitid-reaped it before
		// Anchor observes helper death, in which case fdinfo intentionally no
		// longer exposes a numeric PID; never reopen that number after exit.
		ready, err := linuxOwnerAnchorPIDFDReady(child.pidfd)
		if err != nil || !ready {
			return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify root launcher exited before unregistered gate containment: %w", errOr(err, errors.New("launcher pidfd is not ready")))
		}
		launcherCount++
	}
	if launcherCount != 1 {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	gateNamespaceFD, gateNamespace, err := openLinuxOwnerAnchorUserNamespaceFD(process, pidfd)
	if err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("pin possible pre-release gate user namespace: %w", err)
	}
	defer gateNamespaceFD.Close()
	if gateNamespace == anchor.userNamespace {
		return linuxOwnerAnchorRunChildClassification{}, false, nil
	}
	parentNamespace, err := linuxOwnerAnchorUserNamespaceParentIdentity(gateNamespaceFD)
	if err != nil || parentNamespace != anchor.userNamespace {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("verify possible pre-release gate namespace parent: %w", errOr(err, errors.New("namespace is not an Anchor child")))
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("recheck possible pre-release gate identity: %w", errOr(err, errors.New("creation identity differs")))
	}
	actualCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || actualCgroupPath != cgroupPath {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("recheck possible pre-release gate leaf membership: %w", errOr(err, errors.New("cgroup path changed")))
	}
	classification := anchor.makeLinuxOwnerAnchorChildClassification(
		run, process, LinuxOwnerAnchorRunKey{}, "pre_release_gate", linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly,
	)
	if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, err
	}
	// Only the authenticated Anchor RegisterGate transition can install a gate
	// identity, and AuthorizeGateRelease rejects requests until that transition
	// has pinned GateReady. The fixed helper calls AuthorizeGateRelease before
	// writing the one-use private RELEASE socket, so an exact helper exit with
	// neither state set proves this pre-RegisterGate gate could not have received
	// RELEASE. Keep that proof's image inode pinned through containment.
	if err := run.trackPreReleaseGatePhysicalOnlyChild(process, pidfd, classification); err != nil {
		return linuxOwnerAnchorRunChildClassification{}, false, fmt.Errorf("retain exact pre-release gate origin proof: %w", err)
	}
	return classification, true, nil
}

func (anchor *linuxOwnerAnchor) makeLinuxOwnerAnchorChildClassification(
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	originRun LinuxOwnerAnchorRunKey,
	role string,
	origin linuxOwnerAnchorRunChildOrigin,
) linuxOwnerAnchorRunChildClassification {
	classification := linuxOwnerAnchorUnknownChildClassification(run, process)
	classification.OriginRunKey = originRun
	classification.Role = role
	classification.Origin = origin
	return classification
}

func (anchor *linuxOwnerAnchor) finishLinuxOwnerAnchorChildClassification(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	pidfd int,
	cgroupPath string,
	origin linuxOwnerAnchorRunChildOrigin,
	originRun LinuxOwnerAnchorRunKey,
	role string,
) (linuxOwnerAnchorRunChildClassification, error) {
	unknown := linuxOwnerAnchorUnknownChildClassification(run, process)
	if ctx == nil || ctx.Err() != nil || anchor == nil || run == nil || process.Validate() != nil || pidfd < 0 ||
		!linuxOwnerAnchorValidChildRole(role) {
		return unknown, errors.New("finish exact run-child classification requires a live bounded request")
	}
	if identity, err := ProcessIdentity(process.PID); err != nil || identity != process.Identity {
		return unknown, fmt.Errorf("recheck child creation identity after origin classification: %w", errOr(err, errors.New("creation identity differs")))
	}
	if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
		return unknown, fmt.Errorf("recheck child pidfd after origin classification: %w", err)
	}
	currentCgroupPath, err := readLinuxOwnerProcessCgroupPath(process.PID)
	if err != nil || currentCgroupPath != cgroupPath {
		return unknown, fmt.Errorf("recheck child cgroup after origin classification: %w", errOr(err, errors.New("cgroup path changed")))
	}
	classification := anchor.makeLinuxOwnerAnchorChildClassification(run, process, originRun, role, origin)
	if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
		return unknown, err
	}
	return classification, nil
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

func readLinuxOwnerAnchorProcText(pid int, name string, maxBytes int64) (string, error) {
	if pid <= 1 || maxBytes <= 0 || (name != "uid_map" && name != "gid_map" && name != "setgroups" && name != "status") {
		return "", errors.New("Linux owner anchor proc read requires a fixed filename, positive PID, and byte bound")
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("open exact Linux owner process %s: %w", name, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read exact Linux owner process %s: %w", name, err)
	}
	if int64(len(contents)) > maxBytes {
		return "", fmt.Errorf("exact Linux owner process %s exceeds its bound", name)
	}
	return string(contents), nil
}

func verifyLinuxOwnerAnchorNativePrivilegeState(pid int) error {
	contents, err := readLinuxOwnerAnchorProcText(pid, "status", 1<<20)
	if err != nil {
		return err
	}
	var groupsSeen, noNewPrivsSeen bool
	var capabilitySeen [5]bool
	var noNewPrivs uint64
	var capabilities [5]uint64
	capabilityFields := [...]string{"CapEff", "CapPrm", "CapInh", "CapBnd", "CapAmb"}
	for _, line := range strings.Split(contents, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Groups":
			if groupsSeen || len(strings.Fields(value)) != 0 {
				return errors.New("Linux native process has supplementary groups or duplicate Groups status")
			}
			groupsSeen = true
		case "NoNewPrivs":
			if noNewPrivsSeen {
				return errors.New("Linux native process status duplicates NoNewPrivs")
			}
			parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if parseErr != nil {
				return errors.New("Linux native process NoNewPrivs status is malformed")
			}
			noNewPrivs, noNewPrivsSeen = parsed, true
		default:
			for index, name := range capabilityFields {
				if key != name {
					continue
				}
				if capabilitySeen[index] {
					return fmt.Errorf("Linux native process status duplicates %s", name)
				}
				parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
				if parseErr != nil {
					return fmt.Errorf("Linux native process %s status is malformed", name)
				}
				capabilities[index], capabilitySeen[index] = parsed, true
			}
		}
	}
	if !groupsSeen || !noNewPrivsSeen || noNewPrivs != 1 {
		return errors.New("Linux native process status lacks empty Groups or NoNewPrivs=1")
	}
	for index, name := range capabilityFields {
		if !capabilitySeen[index] || capabilities[index] != 0 {
			return fmt.Errorf("Linux native process %s is missing or nonzero", name)
		}
	}
	return nil
}

func verifyLinuxOwnerRunChildCredentials(pid int, role string, owner linuxgate.OwnerConfig) error {
	if pid <= 0 || owner.Validate() != nil {
		return errors.New("Linux owner child credential check is incomplete")
	}
	credentials, err := readLinuxOwnerProcessCredentials(pid)
	if err != nil {
		return err
	}
	switch role {
	case "launcher":
		if credentials.UIDs != [4]uint32{owner.AUID, 0, 0, 0} || credentials.GIDs != [4]uint32{owner.AGID, owner.AGID, owner.AGID, owner.AGID} {
			return errors.New("Linux transient launcher credentials differ from the fixed A-to-root contract")
		}
	case "gate":
		if credentials.UIDs != [4]uint32{owner.AUID, owner.AUID, owner.AUID, owner.AUID} || credentials.GIDs != [4]uint32{owner.AGID, owner.AGID, owner.AGID, owner.AGID} {
			return errors.New("Linux parked gate credentials differ from configured host UID/GID A")
		}
	case "native":
		if credentials.UIDs != [4]uint32{owner.BUID, owner.BUID, owner.BUID, owner.BUID} || credentials.GIDs != [4]uint32{owner.BGID, owner.BGID, owner.BGID, owner.BGID} {
			return errors.New("Linux native process credentials differ from configured host UID/GID B")
		}
	case "descendant":
		ownerA := credentials.UIDs == [4]uint32{owner.AUID, owner.AUID, owner.AUID, owner.AUID} && credentials.GIDs == [4]uint32{owner.AGID, owner.AGID, owner.AGID, owner.AGID}
		ownerB := credentials.UIDs == [4]uint32{owner.BUID, owner.BUID, owner.BUID, owner.BUID} && credentials.GIDs == [4]uint32{owner.BGID, owner.BGID, owner.BGID, owner.BGID}
		if !ownerA && !ownerB {
			return errors.New("Linux run descendant has unknown host UID/GID credentials")
		}
	default:
		return errors.New("Linux owner anchor refuses an unknown process role")
	}
	return nil
}

func (run *linuxOwnerAnchorRun) waitHelper(ctx context.Context) error {
	if run == nil || run.grant.Validate() != nil || run.helper.Validate() != nil || run.helperPIDFD == nil || run.helperExit != nil {
		return errors.New("Linux owner anchor helper wait is not bound to a live run")
	}
	if err := verifyLinuxOwnerAProcess(run.helper.PID, run.helper.Identity, run.owner.AUID, run.owner.AGID, run.grant.Anchor.PID, run.grant.Parent.Path); err != nil {
		return fmt.Errorf("verify exact Linux owner helper before wait: %w", err)
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, run.helperPIDFD, run.helper); err != nil {
		return err
	}
	proof := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: run.helper, Witness: run.grant.Anchor, Waited: true, Reaped: true,
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	run.helperExit = &proof
	return nil
}

func (run *linuxOwnerAnchorRun) close() error {
	if run == nil {
		return nil
	}
	var result error
	if run.helperPIDFD != nil {
		result = run.helperPIDFD.Close()
		run.helperPIDFD = nil
	}
	for pid, child := range run.children {
		if child.pidfd != nil {
			if err := child.pidfd.Close(); result == nil {
				result = err
			}
		}
		delete(run.children, pid)
	}
	for pid, child := range run.physicalOnlyChildren {
		if child.pidfd != nil {
			if err := child.pidfd.Close(); result == nil {
				result = err
			}
		}
		if child.imageFD != nil {
			if err := child.imageFD.Close(); result == nil {
				result = err
			}
		}
		delete(run.physicalOnlyChildren, pid)
	}
	clear(run.reaped)
	if run.gateUserNamespaceFD != nil {
		if err := run.gateUserNamespaceFD.Close(); result == nil {
			result = err
		}
		run.gateUserNamespaceFD = nil
	}
	if run.gateImageFD != nil {
		if err := run.gateImageFD.Close(); result == nil {
			result = err
		}
		run.gateImageFD = nil
	}
	run.gateIdentity = nil
	run.gateUserNamespace = LinuxUserNamespaceIdentity{}
	run.gateReadyAuthenticated = false
	run.gateReleaseAuthorized = false
	run.stopRequested = false
	run.stopLeafAttempted = false
	run.stopFence = nil
	run.stopEvidence = nil
	run.noMigrationProven = false
	if run.leaf != nil {
		if err := run.leaf.Close(); result == nil {
			result = err
		}
		run.leaf = nil
	}
	return result
}

func (run *linuxOwnerAnchorRun) waitForAdoptedChild(ctx context.Context, anchor authority.LinuxCgroupProcessIdentity, target authority.LinuxCgroupProcessIdentity, role string) error {
	if ctx == nil || run == nil || run.leaf == nil {
		return errors.New("Linux owner anchor adoption wait is incomplete")
	}
	bounded, cancel := context.WithTimeout(ctx, linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved {
		if tracked, exists := run.children[target.PID]; exists && tracked.identity.Equal(target) && tracked.role == role &&
			tracked.role == tracked.classification.Role &&
			tracked.pidfd != nil && tracked.classification.ValidateFor(run.grant, run.helper, target) == nil &&
			tracked.classification.currentRunMember() {
			ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
			if err != nil {
				return fmt.Errorf("poll exact empty-leaf child pidfd before wait: %w", err)
			}
			if ready {
				if tracked.classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent {
					if err := run.verifyPinnedGateUserNamespace(); err != nil {
						return err
					}
				}
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(tracked.pidfd.Fd()), target); err != nil {
					return fmt.Errorf("recheck retained empty-leaf child pidfd: %w", err)
				}
				// The exact process and its in-leaf origin were retained before
				// cgroup.kill. Empty plus this PIDFD now permits P_PIDFD to prove
				// adoption/reap without depending on dead-task procfs entries.
				return nil
			}
		}
	}
	for {
		if err := bounded.Err(); err != nil {
			return fmt.Errorf("wait for exact Linux run child adoption: %w", err)
		}
		identity, err := ProcessIdentity(target.PID)
		if err != nil || identity != target.Identity {
			return fmt.Errorf("verify Linux run child identity during adoption: %w", errOr(err, errors.New("creation identity changed")))
		}
		switch role {
		case "gate":
			if err := run.verifyRegisteredGateA(target, !run.leafRemovedObserved); err != nil {
				return fmt.Errorf("revalidate exact A gate before anchor wait/reap: %w", err)
			}
		case "native":
			if err := run.verifyAuthorizedGateExec(target, !run.leafRemovedObserved); err != nil {
				return fmt.Errorf("revalidate exact authorized B native before anchor wait/reap: %w", err)
			}
		default:
			if run.leafRemovedObserved {
				tracked, exists := run.children[target.PID]
				if !exists || !tracked.identity.Equal(target) || tracked.role != role || tracked.pidfd == nil {
					return errors.New("removed-leaf reap target lacks its earlier exact anchor registration")
				}
				if err := verifyLinuxOwnerRunChildCredentials(target.PID, role, run.owner); err != nil {
					return err
				}
				ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
				if err != nil || !ready {
					return fmt.Errorf("live run child remains after exact run-leaf removal: %w", errOr(err, errors.New("pidfd is not ready")))
				}
			} else if err := verifyLinuxOwnerRunChildMembership(target.PID, role, run.grant, run.leaf); err != nil {
				return err
			}
		}
		credentials, err := readLinuxOwnerProcessCredentials(target.PID)
		if err != nil {
			return err
		}
		if credentials.Parent == anchor.PID {
			return nil
		}
		parent, known := run.children[credentials.Parent]
		if !known || parent.pidfd == nil {
			return errors.New("Linux owner anchor refuses run child with unknown live parent ancestry")
		}
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(parent.pidfd.Fd()), parent.identity); err != nil {
			return fmt.Errorf("verify Linux run-child ancestor pidfd: %w", err)
		}
		if run.leafRemovedObserved {
			if err := verifyLinuxOwnerRunChildCredentials(parent.identity.PID, parent.role, run.owner); err != nil {
				return err
			}
		} else if err := verifyLinuxOwnerRunChildMembership(parent.identity.PID, parent.role, run.grant, run.leaf); err != nil {
			return err
		}
		timer := time.NewTimer(linuxOwnerAnchorWaitPoll)
		select {
		case <-bounded.Done():
			timer.Stop()
			return fmt.Errorf("wait for exact Linux run child adoption: %w", bounded.Err())
		case <-timer.C:
		}
	}
}

func (run *linuxOwnerAnchorRun) reapChild(ctx context.Context, anchor authority.LinuxCgroupProcessIdentity, target authority.LinuxCgroupProcessIdentity, role string) (LinuxOwnerAnchorReapEvidence, error) {
	if run == nil || run.grant.Validate() != nil || anchor.Validate() != nil || target.Validate() != nil ||
		!anchor.Equal(run.grant.Anchor) || run.helper.Validate() != nil || run.helperExit == nil ||
		!run.helperExit.ValidFor(run.helper, anchor) || run.leaf == nil ||
		!run.leaf.Identity().Equal(run.grant.Leaf) {
		return LinuxOwnerAnchorReapEvidence{}, errors.New("Linux owner anchor run is not bound to the exact grant, helper exit, and pinned leaf")
	}
	tracked, ok := run.children[target.PID]
	if !ok || !tracked.identity.Equal(target) || tracked.role != role || tracked.pidfd == nil {
		return LinuxOwnerAnchorReapEvidence{}, errors.New("Linux owner anchor refuses an unregistered run child")
	}
	if evidence, ok := run.reaped[target.PID]; ok {
		if err := evidence.ValidateFor(run.grant, run.helper, target, role); err != nil {
			return LinuxOwnerAnchorReapEvidence{}, err
		}
		return evidence, nil
	}
	if err := run.waitForAdoptedChild(ctx, anchor, target, tracked.role); err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	if err := linuxOwnerAnchorWaitPIDFD(ctx, tracked.pidfd, target); err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	processExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: target, Witness: anchor, Waited: true, Reaped: true,
		HelperExit: cloneLinuxOwnerAnchorExitProof(run.helperExit),
	}
	evidence, err := makeLinuxOwnerAnchorReapEvidence(run.grant, run.helper, target, tracked.role, *run.helperExit, processExit)
	if err != nil {
		return LinuxOwnerAnchorReapEvidence{}, err
	}
	if run.reaped == nil {
		run.reaped = make(map[int]LinuxOwnerAnchorReapEvidence)
	}
	run.reaped[target.PID] = evidence
	return evidence, nil
}

func verifyLinuxOwnerAnchorLeaf(leaf *LinuxCgroupV2Leaf, expected authority.LinuxCgroupIdentity, ownerUID uint32) error {
	if leaf == nil || leaf.OwnerUID() != ownerUID || !leaf.Identity().Equal(expected) {
		return errors.New("Linux owner anchor leaf pin differs from the exact run grant")
	}
	return leaf.verifyLive()
}

func readLinuxOwnerAnchorRunLeafProcessIDs(
	ctx context.Context,
	leaf *LinuxCgroupV2Leaf,
	grant LinuxOwnerAnchorGrant,
	ownerUID uint32,
) (map[int]struct{}, error) {
	if ctx == nil || leaf == nil || grant.Validate() != nil || ownerUID == 0 ||
		ownerUID != grant.OwnerUID || !leaf.Identity().Equal(grant.Leaf) {
		return nil, errors.New("anchor recursive leaf census is not bound to its exact run grant")
	}
	leafPin, err := leaf.DuplicateFD()
	if err != nil {
		return nil, fmt.Errorf("duplicate exact leaf pin for anchor membership census: %w", err)
	}
	defer leafPin.Close()
	rootFD := int(leafPin.Fd())
	parent := grant.Parent
	leafIdentity := grant.Leaf
	seenDirectories := make(map[[2]uint64]struct{})
	processes := make(map[int]struct{})
	directoryCount := 1
	var totalProcsBytes int64
	var scan func(int, int) error
	scan = func(directoryFD, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > linuxOwnerHelperV2MaxMembershipDepth {
			return errors.New("anchor run-leaf census exceeds its bounded nested-cgroup depth")
		}
		stat, err := statLinuxCgroupFD(directoryFD)
		if err != nil || stat.mount != parent.MountID || stat.device != parent.Device || stat.uid != ownerUID ||
			stat.mode&unix.S_IFMT != unix.S_IFDIR || stat.mode&0300 != 0300 {
			return fmt.Errorf("verify exact anchor run-leaf subtree directory: %w", errOr(err, errors.New("mount, device, owner, or mode differs")))
		}
		if directoryFD == rootFD && (stat.device != leafIdentity.LeafDevice || stat.inode != leafIdentity.LeafInode) {
			return errors.New("anchor run-leaf census root differs from the exact reserved inode")
		}
		directoryKey := [2]uint64{stat.device, stat.inode}
		if _, duplicate := seenDirectories[directoryKey]; duplicate {
			return errors.New("anchor run-leaf census encountered a duplicate cgroup directory identity")
		}
		seenDirectories[directoryKey] = struct{}{}
		procsFD, err := openLinuxCgroupControlAt(directoryFD, "cgroup.procs", unix.O_RDONLY)
		if err != nil {
			return fmt.Errorf("open pinned anchor run-leaf cgroup.procs: %w", err)
		}
		if err := verifyLinuxCgroupControlFileOnMount(procsFD, parent.MountID, parent.Device); err != nil {
			_ = unix.Close(procsFD)
			return err
		}
		procsFile := os.NewFile(uintptr(procsFD), "symmetry-owner-anchor-leaf-cgroup-procs")
		if procsFile == nil {
			_ = unix.Close(procsFD)
			return errors.New("wrap pinned anchor run-leaf cgroup.procs")
		}
		remainingBytes := int64(linuxOwnerHelperV2MaxCgroupProcsBytes) - totalProcsBytes
		if remainingBytes < 0 {
			_ = procsFile.Close()
			return errors.New("anchor run-leaf census exhausted its aggregate cgroup.procs byte bound")
		}
		contents, readErr := io.ReadAll(io.LimitReader(procsFile, remainingBytes+1))
		closeErr := procsFile.Close()
		if readErr != nil || closeErr != nil || int64(len(contents)) > remainingBytes {
			return errors.Join(errors.New("read bounded pinned anchor run-leaf cgroup.procs"), readErr, closeErr)
		}
		totalProcsBytes += int64(len(contents))
		for _, field := range strings.Fields(string(contents)) {
			pid, parseErr := strconv.Atoi(field)
			if parseErr != nil || pid <= 1 || strconv.Itoa(pid) != field {
				return errors.New("anchor run-leaf cgroup.procs contains a noncanonical PID")
			}
			if _, duplicate := processes[pid]; duplicate {
				return errors.New("anchor run-leaf cgroup.procs repeats a PID across nested cgroups")
			}
			processes[pid] = struct{}{}
			if len(processes) > linuxOwnerHelperV2MaxMembershipProcesses {
				return errors.New("anchor run-leaf census exceeds its bounded process count")
			}
		}
		names, err := readLinuxCgroupDirectoryNames(directoryFD)
		if err != nil {
			return fmt.Errorf("enumerate nested anchor run-leaf cgroups: %w", err)
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if name == "." || name == ".." {
				continue
			}
			var childStat unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &childStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return fmt.Errorf("stat nested anchor run-leaf cgroup: %w", err)
			}
			if childStat.Mode&unix.S_IFMT != unix.S_IFDIR {
				continue
			}
			directoryCount++
			if directoryCount > linuxOwnerHelperV2MaxMembershipCgroups {
				return errors.New("anchor run-leaf census exceeds its bounded nested-cgroup count")
			}
			childFD, err := unix.Openat2(directoryFD, name, &unix.OpenHow{
				Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
				Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
			})
			if err != nil {
				return fmt.Errorf("open nested anchor run-leaf cgroup without following links: %w", err)
			}
			childIdentity, identityErr := statLinuxCgroupFD(childFD)
			if identityErr != nil || childIdentity.mount != parent.MountID || childIdentity.device != parent.Device ||
				childIdentity.inode != childStat.Ino || childIdentity.uid != ownerUID ||
				childIdentity.mode&unix.S_IFMT != unix.S_IFDIR || childIdentity.mode&0300 != 0300 {
				_ = unix.Close(childFD)
				return fmt.Errorf("verify nested anchor run-leaf directory pin: %w", errOr(identityErr, errors.New("directory identity changed")))
			}
			scanErr := scan(childFD, depth+1)
			closeErr := unix.Close(childFD)
			if scanErr != nil || closeErr != nil {
				return errors.Join(scanErr, closeErr)
			}
		}
		return nil
	}
	if err := scan(rootFD, 0); err != nil {
		return nil, err
	}
	if err := leaf.verifyLive(); err != nil {
		return nil, fmt.Errorf("revalidate exact leaf after anchor membership census: %w", err)
	}
	return processes, nil
}

func verifyLinuxOwnerRunChildMembership(pid int, role string, grant LinuxOwnerAnchorGrant, leaf *LinuxCgroupV2Leaf) error {
	if pid <= 0 || grant.Validate() != nil || leaf == nil || !leaf.Identity().Equal(grant.Leaf) {
		return errors.New("Linux owner anchor run-child membership identity is incomplete")
	}
	if err := leaf.verifyLive(); err != nil {
		return err
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
	if err != nil {
		return err
	}
	leafPath := path.Join(grant.Parent.Path, grant.Leaf.LeafName)
	switch role {
	case "gate", "native", "descendant":
		if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
			return errors.New("Linux owner anchor process is outside the exact reserved run leaf")
		}
	case "launcher":
		if cgroupPath != grant.Parent.Path {
			return errors.New("Linux owner anchor launcher is outside the exact manager subgroup")
		}
	default:
		return errors.New("Linux owner anchor refuses an unknown run process role")
	}
	return nil
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

func closeLinuxOwnerAnchorFDs(fds []int) {
	for _, fd := range fds {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}

func sanitizeLinuxOwnerAnchorSocketError(err error) error {
	switch typed := err.(type) {
	case *net.OpError:
		copy := *typed
		copy.Source = nil
		copy.Addr = nil
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	case *os.PathError:
		copy := *typed
		copy.Path = "Linux owner anchor socket"
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	case *os.SyscallError:
		copy := *typed
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	default:
		return err
	}
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
