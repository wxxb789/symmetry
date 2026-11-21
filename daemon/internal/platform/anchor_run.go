//go:build linux && amd64

package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

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

func (classification linuxOwnerAnchorRunChildClassification) Equal(other linuxOwnerAnchorRunChildClassification) bool {
	return classification.Version == other.Version && classification.RunKey == other.RunKey &&
		classification.OriginRunKey == other.OriginRunKey && classification.Fence == other.Fence &&
		classification.LaunchToken == other.LaunchToken && classification.Helper.Equal(other.Helper) &&
		classification.Process.Equal(other.Process) && classification.Role == other.Role && classification.Origin == other.Origin
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
			if err := verifyLinuxOwnerAnchorChildPIDFD(pidfd, process, role); err != nil {
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
	if err := verifyLinuxOwnerAnchorChildPIDFD(pidfd, process, role); err != nil {
		return err
	}
	if role != "launcher" {
		if actual, err := ProcessIdentity(process.PID); err != nil || actual != process.Identity {
			return fmt.Errorf("verify registered Linux owner child identity: %w", errOr(err, errors.New("creation identity differs")))
		}
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
	if err := verifyLinuxOwnerAnchorChildPIDFD(pidfd, process, role); err != nil {
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
