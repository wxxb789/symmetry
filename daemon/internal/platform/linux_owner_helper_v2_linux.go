//go:build linux && amd64

package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxOwnerHelperV2Version                = 1
	linuxOwnerHelperV2BootstrapFD            = 3
	linuxOwnerHelperV2LeafFD                 = 4
	linuxOwnerHelperV2ParentFD               = 5
	linuxOwnerHelperV2OwnerPipeFD            = 6
	linuxOwnerHelperV2MaxFrame               = 192 << 10
	linuxOwnerHelperV2Timeout                = 2 * time.Minute
	linuxOwnerHelperV2StartupWait            = 5 * time.Second
	linuxOwnerHelperV2StopTimeout            = 30 * time.Second
	linuxOwnerHelperV2PollInterval           = 25 * time.Millisecond
	linuxOwnerHelperV2MaxMembershipProcesses = 1 << 16
	linuxOwnerHelperV2MaxMembershipCgroups   = 1 << 12
	linuxOwnerHelperV2MaxMembershipDepth     = 128
	linuxOwnerHelperV2MaxCgroupProcsBytes    = 1 << 20
	// Bound the raw /proc list at 11 bytes per permitted decimal PID.
	linuxOwnerAnchorMaxChildrenBytes           = 11 * linuxOwnerHelperV2MaxMembershipProcesses
	linuxOwnerAnchorMaxCreatorChainDepth       = 128
	linuxOwnerProcessChildrenSnapshotAttempts  = 3
	linuxOwnerHelperV2WitnessCrashAfterRelease = "crash_after_release_before_register"
)

var (
	ErrLinuxOwnerHelperV2OwnerLost      = errors.New("Linux owner helper observed daemon-owner loss")
	ErrLinuxOwnerHelperV2NotCommitted   = errors.New("Linux owner helper has no durable handoff acknowledgement")
	ErrLinuxOwnerHelperV2LeaseUnarmed   = errors.New("Linux owner helper has no acknowledged initial lease")
	ErrLinuxOwnerHelperV2ReleaseUsed    = errors.New("Linux owner helper RELEASE is single-use")
	ErrLinuxOwnerHelperV2StopUnproven   = errors.New("Linux owner helper stop is unproven")
	ErrLinuxOwnerHelperV2RecoveryOnly   = errors.New("Linux owner helper is latched to recovery-only mode")
	errLinuxOwnerHelperV2WitnessExit    = errors.New("Linux owner helper exited at the selected post-RELEASE witness boundary")
	errLinuxOwnerProcessChildrenChanged = errors.New("Linux process child census snapshot changed")
)

// LinuxOwnerHelperV2Prepared is evidence from the helper's actual parked-gate
// observation. PIDFD is an independently transferred pin for the daemon's
// emergency mirror; the helper retains its own pidfd inside PreparedGate.
type LinuxOwnerHelperV2Prepared struct {
	Gate          authority.LinuxCgroupGateIdentity
	Launcher      authority.LinuxCgroupProcessIdentity
	LauncherExit  authority.LinuxCgroupExitProof
	UserNamespace LinuxUserNamespaceIdentity
	PIDFD         *os.File
}

func (prepared *LinuxOwnerHelperV2Prepared) Close() error {
	if prepared == nil || prepared.PIDFD == nil {
		return nil
	}
	err := prepared.PIDFD.Close()
	prepared.PIDFD = nil
	return err
}

// LinuxOwnerHelperV2PrepareError preserves exact producer evidence on an
// unsuccessful preparation. A returned PIDFD is owned by the caller and is
// never treated as proof of a successful gate or exec.
type LinuxOwnerHelperV2PrepareError struct {
	Code         string
	Launcher     *authority.LinuxCgroupProcessIdentity
	LauncherExit *authority.LinuxCgroupExitProof
	GateProcess  *authority.LinuxCgroupProcessIdentity
	Gate         *authority.LinuxCgroupGateIdentity
	GateOutcome  *authority.LinuxCgroupGateProductionResult
	PIDFD        *os.File
}

func (failure *LinuxOwnerHelperV2PrepareError) Error() string {
	if failure == nil {
		return "Linux owner helper preparation failed"
	}
	return fmt.Sprintf("Linux owner helper preparation failed (%s)", failure.Code)
}

func (failure *LinuxOwnerHelperV2PrepareError) Unwrap() error {
	if failure != nil && failure.GateOutcome != nil {
		return linuxgate.ErrGateNeverProduced
	}
	if failure != nil && failure.Code == "launch_outcome_unknown" {
		return linuxgate.ErrOutcomeUnknown
	}
	return nil
}

func (failure *LinuxOwnerHelperV2PrepareError) Close() error {
	if failure == nil || failure.PIDFD == nil {
		return nil
	}
	err := failure.PIDFD.Close()
	failure.PIDFD = nil
	return err
}

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

// LinuxOwnerHelperV2PreGateStopEvidence is the helper's measured no-A-gate
// stop branch. A State receipt is still created only by the daemon from the
// exact journal grant and this typed physical evidence.
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

type LinuxOwnerHelperV2Status struct {
	RunKey                LinuxOwnerAnchorRunKey
	Fence                 protocol.Fence
	Helper                authority.LinuxCgroupProcessIdentity
	State                 string
	OwnerLost             bool
	CommitAcknowledged    bool
	ReleaseAttempted      bool
	Released              bool
	LeaseLive             bool
	LeaseSequence         uint64
	StopRequested         bool
	StopAttempted         bool
	ReleaseInFlight       bool
	WitnessDropObserved   bool
	WitnessDropOperation  string
	WitnessActionObserved bool
	WitnessAction         string
	Exit                  *LinuxOwnerHelperV2Exit
	Launcher              *authority.LinuxCgroupProcessIdentity
	LauncherExit          *authority.LinuxCgroupExitProof
	Gate                  *authority.LinuxCgroupGateIdentity
	UserNamespace         *LinuxUserNamespaceIdentity
	PIDFD                 *os.File
	StopEvidence          *LinuxOwnerHelperV2StopEvidence
	ErrorCode             string
}

// LinuxOwnerHelperV2Exit reports a kernel waitid(P_PIDFD) result from the
// helper, which is the gate's actual parent/reaper.
type LinuxOwnerHelperV2Exit struct {
	Process    authority.LinuxCgroupProcessIdentity
	Exited     bool
	ExitCode   int
	Signal     int
	CoreDumped bool
	Proof      authority.LinuxCgroupExitProof
}

const (
	linuxOwnerCLDExited = 1
	linuxOwnerCLDKilled = 2
	linuxOwnerCLDDumped = 3
)

// protocolFence is a local alias which keeps the v2 IPC's exported evidence
// independent of the state package while retaining the exact durable fence.
type protocolFence = protocol.Fence

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

// ValidateMirrorFor validates the anchor takeover proof used after helper
// death. Unlike ValidateFor, it requires the anchor's exact helper exit and
// complete run-wide wait/reap set; it never accepts helper-supplied booleans
// in place of the cached anchor StopLeaf result.
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

func linuxOwnerHelperV2Supervisor(grant LinuxOwnerAnchorGrant, helper, launcher authority.LinuxCgroupProcessIdentity, gate authority.LinuxCgroupGateIdentity, launcherExit authority.LinuxCgroupExitProof) authority.Supervisor {
	launcherExitCopy := cloneLinuxOwnerAnchorExitProof(&launcherExit)
	return authority.Supervisor{
		Version:   authority.SupervisorVersionV2,
		TargetPID: gate.Process.PID, TargetIdentity: gate.Process.Identity,
		SupervisorPID: helper.PID, SupervisorIdentity: helper.Identity,
		AnchorPID: grant.Anchor.PID, AnchorIdentity: grant.Anchor.Identity,
		LauncherPID: launcher.PID, LauncherIdentity: launcher.Identity,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  &grant.Leaf, LauncherExit: launcherExitCopy,
	}
}

type linuxOwnerHelperV2Bootstrap struct {
	Version                   int                                  `json:"version"`
	Grant                     LinuxOwnerAnchorGrant                `json:"grant"`
	AnchorSocket              string                               `json:"anchor_socket"`
	OwnerProcess              authority.LinuxCgroupProcessIdentity `json:"owner_process"`
	WitnessDropResponse       string                               `json:"witness_drop_response,omitempty"`
	WitnessHelperAction       string                               `json:"helper_action,omitempty"`
	WitnessHoldForReleaseFile bool                                 `json:"hold_for_release_file,omitempty"`
	WitnessRunID              string                               `json:"witness_run_id,omitempty"`
	WitnessGeneration         int64                                `json:"witness_generation,omitempty"`
}

type linuxOwnerHelperV2BootstrapReady struct {
	Version  int                                  `json:"version"`
	Status   string                               `json:"status"`
	Helper   authority.LinuxCgroupProcessIdentity `json:"helper"`
	Endpoint string                               `json:"endpoint"`
}

type linuxOwnerHelperV2Command struct {
	Path  string                     `json:"path"`
	Args  []string                   `json:"args"`
	Env   []string                   `json:"env"`
	Dir   string                     `json:"dir"`
	Clone *authority.LinuxCloneGrant `json:"clone,omitempty"`
}

type linuxOwnerHelperV2Request struct {
	Version             int                                         `json:"version"`
	Operation           string                                      `json:"operation"`
	ClientPID           int                                         `json:"client_pid"`
	ClientIdentity      string                                      `json:"client_identity"`
	Grant               LinuxOwnerAnchorGrant                       `json:"grant"`
	Helper              *authority.LinuxCgroupProcessIdentity       `json:"helper,omitempty"`
	Launcher            *authority.LinuxCgroupProcessIdentity       `json:"launcher,omitempty"`
	Gate                *authority.LinuxCgroupGateIdentity          `json:"gate,omitempty"`
	LauncherExit        *authority.LinuxCgroupExitProof             `json:"launcher_exit,omitempty"`
	Sequence            uint64                                      `json:"sequence,omitempty"`
	LeaseNanos          int64                                       `json:"lease_nanos,omitempty"`
	Accepted            bool                                        `json:"accepted,omitempty"`
	Command             *linuxOwnerHelperV2Command                  `json:"command,omitempty"`
	CheckoutReceipt     *authority.LinuxCloneCheckoutReceipt        `json:"checkout_receipt,omitempty"`
	Receipt             *authority.StopReceipt                      `json:"receipt,omitempty"`
	PreGateReceipt      *authority.LinuxCgroupPreGateStopReceipt    `json:"pre_gate_receipt,omitempty"`
	ReleaseProof        *authority.SupervisorHandoffReleaseProof    `json:"release_proof,omitempty"`
	PreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	AbortProof          *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
}

type linuxOwnerHelperV2StatusSnapshot struct {
	LeaseLive       bool   `json:"lease_live"`
	LeaseSequence   uint64 `json:"lease_sequence"`
	StopRequested   bool   `json:"stop_requested"`
	StopAttempted   bool   `json:"stop_attempted"`
	ReleaseInFlight bool   `json:"release_in_flight"`
}

type linuxOwnerHelperV2Response struct {
	Version               int                                        `json:"version"`
	Operation             string                                     `json:"operation"`
	Status                string                                     `json:"status"`
	RunKey                LinuxOwnerAnchorRunKey                     `json:"run_key"`
	Fence                 protocolFence                              `json:"fence"`
	LaunchToken           string                                     `json:"launch_token"`
	Helper                authority.LinuxCgroupProcessIdentity       `json:"helper"`
	OwnerLost             bool                                       `json:"owner_lost,omitempty"`
	WitnessDropObserved   bool                                       `json:"witness_drop_observed,omitempty"`
	WitnessDropOperation  string                                     `json:"witness_drop_operation,omitempty"`
	WitnessActionObserved bool                                       `json:"witness_action_observed,omitempty"`
	WitnessAction         string                                     `json:"witness_action,omitempty"`
	CommitAcknowledged    bool                                       `json:"commit_acknowledged,omitempty"`
	ReleaseAttempted      bool                                       `json:"release_attempted,omitempty"`
	Released              bool                                       `json:"released,omitempty"`
	LeaseSequence         uint64                                     `json:"lease_sequence,omitempty"`
	Launcher              *authority.LinuxCgroupProcessIdentity      `json:"launcher,omitempty"`
	Gate                  *authority.LinuxCgroupGateIdentity         `json:"gate,omitempty"`
	LauncherExit          *authority.LinuxCgroupExitProof            `json:"launcher_exit,omitempty"`
	GateProcess           *authority.LinuxCgroupProcessIdentity      `json:"gate_process,omitempty"`
	UserNamespace         *LinuxUserNamespaceIdentity                `json:"user_namespace,omitempty"`
	GateOutcome           *authority.LinuxCgroupGateProductionResult `json:"gate_outcome,omitempty"`
	StopEvidence          *LinuxOwnerHelperV2StopEvidence            `json:"stop_evidence,omitempty"`
	ExitResult            *LinuxOwnerHelperV2Exit                    `json:"exit_result,omitempty"`
	CheckoutReceipt       *authority.LinuxCloneCheckoutReceipt       `json:"checkout_receipt,omitempty"`
	StatusSnapshot        *linuxOwnerHelperV2StatusSnapshot          `json:"status_snapshot,omitempty"`
	ErrorCode             string                                     `json:"error_code,omitempty"`
}

func (response linuxOwnerHelperV2Response) matches(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) bool {
	return response.Version == linuxOwnerHelperV2Version && response.RunKey == grant.RunKey &&
		response.Fence == grant.Fence && response.LaunchToken == grant.LaunchToken && response.Helper.Equal(helper)
}

func linuxOwnerHelperV2CloneGrantMatchesRun(clone *authority.LinuxCloneGrant, grant LinuxOwnerAnchorGrant, target string) bool {
	return clone != nil && clone.Validate() == nil && clone.Intent.RunID == grant.RunKey.RunID &&
		clone.Intent.Generation == grant.RunKey.Generation && clone.Intent.Target == target
}

func linuxOwnerHelperV2CheckoutReceiptMatches(receipt authority.LinuxCloneCheckoutReceipt, clone authority.LinuxCloneGrant) bool {
	intent := clone.Intent
	return receipt.Validate() == nil && clone.Validate() == nil && receipt.RunID == intent.RunID &&
		receipt.Generation == intent.Generation && receipt.BindingKey == intent.BindingKey &&
		receipt.ResourceID == intent.ResourceID && receipt.Target == intent.Target && receipt.Nonce == intent.Nonce &&
		receipt.SeedDigest == clone.Seed.Digest && receipt.BaseCommit == intent.BaseCommit
}

func linuxOwnerHelperV2WitnessBootstrapValid(bootstrap linuxOwnerHelperV2Bootstrap) bool {
	if bootstrap.WitnessDropResponse == "" && bootstrap.WitnessHelperAction == "" {
		return !bootstrap.WitnessHoldForReleaseFile && bootstrap.WitnessRunID == "" && bootstrap.WitnessGeneration == 0
	}
	if bootstrap.WitnessRunID != bootstrap.Grant.RunKey.RunID || bootstrap.WitnessGeneration != bootstrap.Grant.RunKey.Generation {
		return false
	}
	if bootstrap.WitnessDropResponse != "" {
		return bootstrap.WitnessHelperAction == "" && !bootstrap.WitnessHoldForReleaseFile &&
			(bootstrap.WitnessDropResponse == "stop" || bootstrap.WitnessDropResponse == "ack-stop-receipt" || bootstrap.WitnessDropResponse == "launcher-ready")
	}
	return bootstrap.WitnessHelperAction == linuxOwnerHelperV2WitnessCrashAfterRelease && bootstrap.WitnessHoldForReleaseFile
}

// LinuxOwnerHelperV2Client is the daemon's authenticated per-run IPC handle.
// Closing it only closes this connection; the separate OwnerFD is the actual
// daemon-death latch returned by StartHelper.
type LinuxOwnerHelperV2Client struct {
	mu           sync.Mutex
	connection   *net.UnixConn
	grant        LinuxOwnerAnchorGrant
	helper       authority.LinuxCgroupProcessIdentity
	client       authority.LinuxCgroupProcessIdentity
	owner        linuxgate.OwnerConfig
	recoveryOnly bool
	closed       bool
}

// RunLinuxOwnerHelperV2 is the fixed hidden dispatch target. The only startup
// inputs are the anchor-authenticated bootstrap packet and inherited pinned
// parent/leaf/owner-pipe descriptors; no RunJournal or Store is opened here.
func RunLinuxOwnerHelperV2(args []string) error {
	if len(args) != 0 {
		return errors.New("Linux owner helper v2 accepts no command-line arguments")
	}
	bootstrapFile := os.NewFile(linuxOwnerHelperV2BootstrapFD, "linux-owner-helper-bootstrap")
	leafFile := os.NewFile(linuxOwnerHelperV2LeafFD, "linux-owner-helper-leaf-pin")
	parentFile := os.NewFile(linuxOwnerHelperV2ParentFD, "linux-owner-helper-parent-pin")
	ownerPipe := os.NewFile(linuxOwnerHelperV2OwnerPipeFD, "linux-owner-helper-owner-pipe")
	if bootstrapFile == nil || leafFile == nil || parentFile == nil || ownerPipe == nil {
		closeLinuxOwnerHelperFiles(bootstrapFile, leafFile, parentFile, ownerPipe)
		return errors.New("Linux owner helper v2 inherited descriptor set is incomplete")
	}
	defer closeLinuxOwnerHelperFiles(bootstrapFile, leafFile, parentFile)
	for _, file := range []*os.File{leafFile, parentFile, ownerPipe} {
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			_ = ownerPipe.Close()
			return fmt.Errorf("mark Linux owner helper inherited descriptor close-on-exec: %w", err)
		}
	}
	if err := verifyLinuxOwnerAnchorOwnerPipeReadFD(int(ownerPipe.Fd())); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	bootstrap, err := readLinuxOwnerHelperV2Bootstrap(bootstrapFile)
	if err != nil {
		_ = ownerPipe.Close()
		return err
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("read fixed Linux owner configuration in helper: %w", err)
	}
	if err := owner.Validate(); err != nil || uint32(os.Getuid()) != owner.AUID || uint32(os.Geteuid()) != owner.AUID ||
		uint32(os.Getgid()) != owner.AGID || uint32(os.Getegid()) != owner.AGID ||
		bootstrap.Grant.OwnerUID != owner.AUID || bootstrap.Grant.LauncherUID != 0 {
		_ = ownerPipe.Close()
		return errors.Join(errors.New("Linux owner helper v2 is not running under the fixed A identity"), err)
	}
	if bootstrap.Version != linuxOwnerHelperV2Version || bootstrap.Grant.Validate() != nil ||
		!canonicalAbsoluteLinuxPath(bootstrap.AnchorSocket) || bootstrap.OwnerProcess.Validate() != nil ||
		!linuxOwnerHelperV2WitnessBootstrapValid(bootstrap) {
		_ = ownerPipe.Close()
		return errors.New("Linux owner helper v2 bootstrap is invalid")
	}
	if err := ensureLinuxOwnerAnchorSubreaper(); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	helperID, err := ProcessIdentity(os.Getpid())
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("capture Linux owner helper identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(os.Getpid(), helperID, owner.AUID, owner.AGID, bootstrap.Grant.Anchor.PID, bootstrap.Grant.Parent.Path); err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("verify exact Linux owner helper parent and manager membership: %w", err)
	}
	anchorID, err := ProcessIdentity(bootstrap.Grant.Anchor.PID)
	if err != nil || anchorID != bootstrap.Grant.Anchor.Identity {
		_ = ownerPipe.Close()
		return fmt.Errorf("verify exact Linux owner anchor identity from helper: %w", errOr(err, errors.New("creation identity differs")))
	}
	parent, err := AdoptLinuxCgroupV2Parent(int(parentFile.Fd()), bootstrap.Grant.Parent)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("adopt the continuously pinned Linux cgroup parent: %w", err)
	}
	defer parent.Close()
	leaf, err := parent.AdoptLeaf(bootstrap.Grant.Leaf.LeafName, int(leafFile.Fd()), bootstrap.Grant.Leaf)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("adopt the exact reserved Linux cgroup leaf: %w", err)
	}
	defer leaf.Close()
	if err := verifyLinuxOwnerAnchorLeaf(leaf, bootstrap.Grant.Leaf, owner.AUID); err != nil {
		_ = ownerPipe.Close()
		return err
	}
	anchorClient, err := NewLinuxOwnerAnchorClient(bootstrap.AnchorSocket)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("connect the helper to its authenticated owner anchor: %w", err)
	}
	listener, socketIdentity, err := listenLinuxOwnerAnchor(bootstrap.Grant.RecoveryEndpoint, owner)
	if err != nil {
		_ = ownerPipe.Close()
		return fmt.Errorf("listen on the exact helper recovery endpoint: %w", err)
	}
	defer unlinkLinuxOwnerAnchorSocket(bootstrap.Grant.RecoveryEndpoint, socketIdentity, owner.AUID)
	helper := &linuxOwnerHelperV2{
		owner: owner, grant: bootstrap.Grant, helper: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: helperID},
		initialOwner: bootstrap.OwnerProcess, anchorClient: anchorClient, parent: parent, leaf: leaf,
		listener: listener, ownerPipe: ownerPipe, activeConns: make(map[*net.UnixConn]struct{}),
		witnessDropResponse:       bootstrap.WitnessDropResponse,
		witnessHelperAction:       bootstrap.WitnessHelperAction,
		witnessHoldForReleaseFile: bootstrap.WitnessHoldForReleaseFile,
		witnessRunKey:             LinuxOwnerAnchorRunKey{RunID: bootstrap.WitnessRunID, Generation: bootstrap.WitnessGeneration},
		lease:                     newLinuxOwnerHelperV2Lease(),
	}
	ready := linuxOwnerHelperV2BootstrapReady{
		Version: linuxOwnerHelperV2Version, Status: "ready", Helper: helper.helper,
		Endpoint: bootstrap.Grant.RecoveryEndpoint,
	}
	if err := writeLinuxOwnerHelperV2BootstrapReady(bootstrapFile, ready); err != nil {
		return fmt.Errorf("acknowledge fixed Linux owner helper startup: %w", err)
	}
	return helper.serve()
}

func startLinuxOwnerAnchorHelperV2(anchor *linuxOwnerAnchor, grant LinuxOwnerAnchorGrant, ownerProcess authority.LinuxCgroupProcessIdentity, leafFD, ownerPipeFD int, witness *linuxOwnerAnchorWitnessConfig, beforeFork func() error) (*linuxOwnerAnchorRun, error) {
	if anchor == nil || grant.Validate() != nil || ownerProcess.Validate() != nil || leafFD < 0 || ownerPipeFD < 0 ||
		!grant.Anchor.Equal(anchor.identity) || !grant.Parent.Equal(anchor.parent.Identity()) || beforeFork == nil {
		return nil, errors.New("Linux owner anchor helper launch grant is incomplete")
	}
	if witness != nil && (witness.RunID != grant.RunKey.RunID || witness.Generation != grant.RunKey.Generation) {
		return nil, errors.New("Linux owner anchor witness selector differs from the exact RunKey")
	}
	if err := verifyLinuxOwnerHelperEndpointVacant(grant.RecoveryEndpoint, anchor.owner.AUID); err != nil {
		return nil, err
	}
	leaf, err := anchor.parent.AdoptLeaf(grant.Leaf.LeafName, leafFD, grant.Leaf)
	if err != nil {
		return nil, fmt.Errorf("anchor adopt exact helper leaf pin: %w", err)
	}
	defer leaf.Close()
	leafFile, err := leaf.DuplicateFD()
	if err != nil {
		return nil, err
	}
	parentFile, err := anchor.parent.DuplicateFD()
	if err != nil {
		_ = leafFile.Close()
		return nil, err
	}
	ownerRead, err := unix.FcntlInt(uintptr(ownerPipeFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		closeLinuxOwnerHelperFiles(leafFile, parentFile)
		return nil, fmt.Errorf("duplicate daemon owner-death pipe for helper: %w", err)
	}
	ownerFile := os.NewFile(uintptr(ownerRead), "symmetry-owner-helper-owner-latch")
	if ownerFile == nil {
		_ = unix.Close(ownerRead)
		closeLinuxOwnerHelperFiles(leafFile, parentFile)
		return nil, errors.New("wrap daemon owner-death pipe for helper")
	}
	bootFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		closeLinuxOwnerHelperFiles(leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("create fixed helper bootstrap channel: %w", err)
	}
	anchorBoot := os.NewFile(uintptr(bootFDs[0]), "symmetry-owner-anchor-helper-bootstrap")
	helperBoot := os.NewFile(uintptr(bootFDs[1]), "symmetry-owner-helper-bootstrap")
	if anchorBoot == nil || helperBoot == nil {
		_ = unix.Close(bootFDs[0])
		_ = unix.Close(bootFDs[1])
		closeLinuxOwnerHelperFiles(leafFile, parentFile, ownerFile)
		return nil, errors.New("wrap fixed helper bootstrap channel")
	}
	command := exec.Command("/proc/self/exe", "-linux-owner-helper-v2")
	command.Args = []string{"symmetry-daemon", "-linux-owner-helper-v2"}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.ExtraFiles = []*os.File{helperBoot, leafFile, parentFile, ownerFile}
	helperPIDFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &helperPIDFD}
	if err := beforeFork(); err != nil {
		closeLinuxOwnerHelperFiles(anchorBoot, helperBoot, leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("reserve exact RunKey before helper fork: %w", err)
	}
	if err := command.Start(); err != nil {
		closeLinuxOwnerHelperFiles(anchorBoot, helperBoot, leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("start the fixed hidden Linux owner helper: %w", err)
	}
	closeLinuxOwnerHelperFiles(helperBoot, leafFile, parentFile, ownerFile)
	if helperPIDFD < 0 {
		_ = anchorBoot.Close()
		return quarantineLinuxOwnerAnchorRun(grant, anchor.owner, authority.LinuxCgroupProcessIdentity{}, nil, leaf),
			errors.New("Linux owner helper was started without an exact pidfd; retain the RunKey as unresolved")
	}
	helperIdentity, identityErr := ProcessIdentity(command.Process.Pid)
	if identityErr != nil {
		file := os.NewFile(uintptr(helperPIDFD), "symmetry-owner-anchor-unresolved-helper-pidfd")
		quarantine := quarantineLinuxOwnerAnchorRun(grant, anchor.owner, authority.LinuxCgroupProcessIdentity{}, file, leaf)
		if file != nil {
			_ = pidfdKillAndReapUnidentified(int(file.Fd()))
			_ = file.Close()
		}
		_ = anchorBoot.Close()
		return quarantine, fmt.Errorf("capture exact new helper identity before granting it the RunKey: %w", identityErr)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: helperIdentity}
	pidfdFile := os.NewFile(uintptr(helperPIDFD), "symmetry-owner-anchor-helper-pidfd")
	if pidfdFile == nil {
		_ = anchorBoot.Close()
		return nil, errors.New("wrap exact Linux owner helper pidfd")
	}
	run, err := newLinuxOwnerAnchorRun(grant, helper, pidfdFile, leaf, anchor.owner)
	if err != nil {
		quarantine := quarantineLinuxOwnerAnchorRun(grant, anchor.owner, helper, pidfdFile, leaf)
		_ = pidfdKillAndReap(pidfdFile, helper)
		_ = pidfdFile.Close()
		_ = anchorBoot.Close()
		return quarantine, fmt.Errorf("retain exact anchor/helper/leaf ownership before grant: %w", err)
	}
	_ = pidfdFile.Close()
	bootstrap := linuxOwnerHelperV2Bootstrap{
		Version: linuxOwnerHelperV2Version, Grant: grant, AnchorSocket: anchor.socketPath,
		OwnerProcess: ownerProcess,
	}
	if witness != nil {
		bootstrap.WitnessDropResponse = witness.DropResponse
		bootstrap.WitnessHelperAction = witness.HelperAction
		bootstrap.WitnessHoldForReleaseFile = witness.HoldForReleaseFile
		bootstrap.WitnessRunID = witness.RunID
		bootstrap.WitnessGeneration = witness.Generation
	}
	encoded, err := json.Marshal(bootstrap)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		_ = anchorBoot.Close()
		return run, errors.Join(errors.New("encode bounded Linux owner helper bootstrap"), err)
	}
	if err := writeLinuxOwnerHelperV2Bootstrap(anchorBoot, encoded); err != nil {
		_ = anchorBoot.Close()
		return run, fmt.Errorf("send authenticated fixed helper bootstrap: %w", err)
	}
	ready, err := readLinuxOwnerHelperV2BootstrapReady(anchorBoot, linuxOwnerHelperV2StartupWait)
	_ = anchorBoot.Close()
	if err != nil {
		return run, fmt.Errorf("wait for exact helper startup acknowledgement: %w", err)
	}
	if ready.Version != linuxOwnerHelperV2Version || ready.Status != "ready" || !ready.Helper.Equal(helper) || ready.Endpoint != grant.RecoveryEndpoint {
		return run, errors.New("Linux owner helper startup acknowledgement differs from the exact child and endpoint")
	}
	if err := verifyLinuxOwnerAProcess(helper.PID, helper.Identity, anchor.owner.AUID, anchor.owner.AGID, anchor.identity.PID, anchor.managerPath); err != nil {
		return run, fmt.Errorf("verify launched helper credentials and manager membership: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), helper); err != nil {
		return run, fmt.Errorf("verify launched helper pidfd after bootstrap: %w", err)
	}
	return run, nil
}

func verifyLinuxOwnerHelperEndpointVacant(socketPath string, ownerUID uint32) error {
	if err := validateLinuxOwnerAnchorSocketPath(socketPath); err != nil {
		return err
	}
	directoryFD, _, err := openLinuxOwnerAnchorSocketDirectory(filepath.Dir(socketPath), ownerUID)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	var existing unix.Stat_t
	err = unix.Fstatat(directoryFD, filepath.Base(socketPath), &existing, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect helper recovery endpoint without following links: %w", err)
	}
	return errors.New("Linux owner helper recovery endpoint already exists; refusing replacement or a second helper")
}

func quarantineLinuxOwnerAnchorRun(grant LinuxOwnerAnchorGrant, owner linuxgate.OwnerConfig, helper authority.LinuxCgroupProcessIdentity, pidfd *os.File, leaf *LinuxCgroupV2Leaf) *linuxOwnerAnchorRun {
	leafCopy, err := duplicateLinuxOwnerAnchorLeaf(leaf)
	if err != nil {
		return nil
	}
	var pidfdCopy *os.File
	if pidfd != nil {
		fd, duplicateErr := unix.FcntlInt(pidfd.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if duplicateErr != nil {
			_ = leafCopy.Close()
			return nil
		}
		pidfdCopy = os.NewFile(uintptr(fd), "symmetry-owner-quarantined-helper-pidfd")
		if pidfdCopy == nil {
			_ = unix.Close(fd)
			_ = leafCopy.Close()
			return nil
		}
	}
	return &linuxOwnerAnchorRun{
		grant: grant, owner: owner, helper: helper, helperPIDFD: pidfdCopy, leaf: leafCopy,
		children: make(map[int]linuxOwnerAnchorTrackedProcess), reaped: make(map[int]LinuxOwnerAnchorReapEvidence),
	}
}

func verifyLinuxOwnerAnchorOwnerPipeReadFD(fd int) error {
	if fd < 0 {
		return errors.New("Linux owner helper owner-death pipe descriptor is invalid")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat Linux owner helper owner-death pipe: %w", err)
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFIFO || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return errors.Join(errors.New("Linux owner helper requires an anonymous read-only owner-death pipe"), err)
	}
	return nil
}

func verifyLinuxOwnerAnchorParentFD(fd int, expected authority.LinuxCgroupParentIdentity) error {
	parent, err := AdoptLinuxCgroupV2Parent(fd, expected)
	if err != nil {
		return fmt.Errorf("verify transferred pinned cgroup parent: %w", err)
	}
	return parent.Close()
}

func readLinuxOwnerHelperV2Bootstrap(file *os.File) (linuxOwnerHelperV2Bootstrap, error) {
	if file == nil {
		return linuxOwnerHelperV2Bootstrap{}, errors.New("Linux owner helper bootstrap descriptor is absent")
	}
	fd := int(file.Fd())
	deadline := time.Now().Add(linuxOwnerHelperV2StartupWait)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return linuxOwnerHelperV2Bootstrap{}, context.DeadlineExceeded
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(poll, int(remaining/time.Millisecond)+1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return linuxOwnerHelperV2Bootstrap{}, fmt.Errorf("poll fixed helper bootstrap: %w", err)
		}
		if count == 0 {
			continue
		}
		data := make([]byte, linuxOwnerAnchorMaxFrame+1)
		oob := make([]byte, syscall.CmsgSpace(4*4))
		n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
		if err != nil {
			return linuxOwnerHelperV2Bootstrap{}, fmt.Errorf("read fixed helper bootstrap: %w", err)
		}
		fds, rightsErr := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
		if len(fds) != 0 {
			closeLinuxOwnerAnchorFDs(fds)
			return linuxOwnerHelperV2Bootstrap{}, errors.New("fixed helper bootstrap carried unexpected descriptors")
		}
		if rightsErr != nil {
			return linuxOwnerHelperV2Bootstrap{}, rightsErr
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
			return linuxOwnerHelperV2Bootstrap{}, errors.New("fixed helper bootstrap exceeded its bounded frame")
		}
		decoder := json.NewDecoder(bytes.NewReader(data[:n]))
		decoder.DisallowUnknownFields()
		var bootstrap linuxOwnerHelperV2Bootstrap
		if err := decoder.Decode(&bootstrap); err != nil {
			return linuxOwnerHelperV2Bootstrap{}, err
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return linuxOwnerHelperV2Bootstrap{}, errors.New("fixed helper bootstrap contains trailing JSON")
		}
		return bootstrap, nil
	}
}

func writeLinuxOwnerHelperV2Bootstrap(file *os.File, packet []byte) error {
	if file == nil || len(packet) == 0 || len(packet) > linuxOwnerAnchorMaxFrame {
		return errors.New("fixed helper bootstrap packet is invalid")
	}
	n, err := unix.SendmsgN(int(file.Fd()), packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil || n != len(packet) {
		return fmt.Errorf("write fixed helper bootstrap packet: %w", errOr(err, io.ErrShortWrite))
	}
	return nil
}

func writeLinuxOwnerHelperV2BootstrapReady(file *os.File, ready linuxOwnerHelperV2BootstrapReady) error {
	encoded, err := json.Marshal(ready)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return errors.Join(errors.New("encode bounded fixed helper-ready packet"), err)
	}
	return writeLinuxOwnerHelperV2Bootstrap(file, encoded)
}

func readLinuxOwnerHelperV2BootstrapReady(file *os.File, timeout time.Duration) (linuxOwnerHelperV2BootstrapReady, error) {
	if file == nil || timeout <= 0 {
		return linuxOwnerHelperV2BootstrapReady{}, errors.New("fixed helper-ready wait is incomplete")
	}
	fd := int(file.Fd())
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return linuxOwnerHelperV2BootstrapReady{}, context.DeadlineExceeded
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(poll, int(remaining/time.Millisecond)+1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return linuxOwnerHelperV2BootstrapReady{}, err
		}
		if count == 0 {
			continue
		}
		data := make([]byte, linuxOwnerAnchorMaxFrame+1)
		oob := make([]byte, syscall.CmsgSpace(4*4))
		n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
		if err != nil {
			return linuxOwnerHelperV2BootstrapReady{}, err
		}
		fds, rightsErr := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
		if len(fds) != 0 {
			closeLinuxOwnerAnchorFDs(fds)
			return linuxOwnerHelperV2BootstrapReady{}, errors.New("helper-ready packet carried unexpected descriptors")
		}
		if rightsErr != nil {
			return linuxOwnerHelperV2BootstrapReady{}, rightsErr
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
			return linuxOwnerHelperV2BootstrapReady{}, errors.New("helper-ready packet exceeded its bounded frame")
		}
		decoder := json.NewDecoder(bytes.NewReader(data[:n]))
		decoder.DisallowUnknownFields()
		var ready linuxOwnerHelperV2BootstrapReady
		if err := decoder.Decode(&ready); err != nil {
			return linuxOwnerHelperV2BootstrapReady{}, err
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return linuxOwnerHelperV2BootstrapReady{}, errors.New("helper-ready packet contains trailing JSON")
		}
		return ready, nil
	}
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

// dialLinuxOwnerHelperV2 performs both SO_PEERCRED and exact creation-identity
// checks before it sends the RunKey-bound control grant.
func dialLinuxOwnerHelperV2(ctx context.Context, anchorClient *LinuxOwnerAnchorClient, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) (*LinuxOwnerHelperV2Client, error) {
	if ctx == nil || anchorClient == nil || grant.Validate() != nil || helper.Validate() != nil {
		return nil, errors.New("Linux owner helper control connection is incomplete")
	}
	connected, err := (&net.Dialer{Timeout: linuxOwnerHelperV2Timeout}).DialContext(ctx, "unixpacket", grant.RecoveryEndpoint)
	if err != nil {
		return nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	connection, ok := connected.(*net.UnixConn)
	if !ok {
		_ = connected.Close()
		return nil, errors.New("Linux owner helper endpoint is not a Unix seqpacket socket")
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, anchorClient.owner)
	if err != nil || peer.PID != helper.PID || peer.Identity != helper.Identity {
		_ = connection.Close()
		return nil, fmt.Errorf("authenticate exact Linux owner helper peer: %w", errOr(err, errors.New("PID or creation identity differs")))
	}
	client := &LinuxOwnerHelperV2Client{
		connection: connection, grant: grant, helper: helper,
		client: authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: clientIdentity}, owner: anchorClient.owner,
	}
	request := client.newRequest("hello")
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		_ = connection.Close()
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(connection, request); err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if !response.matches(grant, helper) || response.Operation != "hello" || len(rights) != 0 ||
		(response.Status != "ready" && response.Status != "recovery_only") {
		_ = connection.Close()
		return nil, errors.New("Linux owner helper did not authenticate the exact per-run control grant")
	}
	client.recoveryOnly = response.Status == "recovery_only"
	_ = connection.SetDeadline(time.Time{})
	return client, nil
}

func linuxOwnerHelperPeerFromConn(connection *net.UnixConn, owner linuxgate.OwnerConfig) (linuxOwnerAnchorPeer, error) {
	if connection == nil {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner helper peer connection is nil")
	}
	credentials, err := linuxOwnerAnchorPeerCredentials(connection)
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	if credentials.Pid <= 0 || credentials.Uid != owner.AUID || credentials.Gid != owner.AGID {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner helper peer SO_PEERCRED UID/GID/PID is not configured A")
	}
	identity, err := ProcessIdentity(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	if err := verifyLinuxOwnerAProcess(int(credentials.Pid), identity, owner.AUID, owner.AGID, 0, ""); err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	actual, err := ProcessIdentity(int(credentials.Pid))
	if err != nil || actual != identity {
		return linuxOwnerAnchorPeer{}, fmt.Errorf("Linux owner helper peer identity changed: %w", errOr(err, errors.New("creation identity differs")))
	}
	return linuxOwnerAnchorPeer{PID: int(credentials.Pid), Identity: identity, CgroupPath: cgroupPath}, nil
}

func writeLinuxOwnerHelperV2Request(connection *net.UnixConn, request linuxOwnerHelperV2Request, fds ...int) error {
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) == 0 || len(encoded) > linuxOwnerHelperV2MaxFrame {
		return errors.Join(errors.New("encode bounded Linux owner helper request"), err)
	}
	var rights []byte
	if len(fds) != 0 {
		rights = syscall.UnixRights(fds...)
	}
	written, _, err := connection.WriteMsgUnix(encoded, rights, nil)
	if err != nil || written != len(encoded) {
		return fmt.Errorf("write Linux owner helper request: %w", errOr(err, io.ErrShortWrite))
	}
	return nil
}

func readLinuxOwnerHelperV2Request(connection *net.UnixConn) (linuxOwnerHelperV2Request, []int, error) {
	data := make([]byte, linuxOwnerHelperV2MaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerHelperV2Request{}, nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerHelperV2Request{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerHelperV2MaxFrame {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Request{}, nil, errors.New("Linux owner helper request exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var request linuxOwnerHelperV2Request
	if err := decoder.Decode(&request); err != nil {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Request{}, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Request{}, nil, errors.New("Linux owner helper request has trailing JSON")
	}
	return request, fds, nil
}

func writeLinuxOwnerHelperV2Response(connection *net.UnixConn, response linuxOwnerHelperV2Response, fds ...int) error {
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) == 0 || len(encoded) > linuxOwnerHelperV2MaxFrame {
		return errors.Join(errors.New("encode bounded Linux owner helper response"), err)
	}
	var rights []byte
	if len(fds) != 0 {
		rights = syscall.UnixRights(fds...)
	}
	written, _, err := connection.WriteMsgUnix(encoded, rights, nil)
	if err != nil || written != len(encoded) {
		return fmt.Errorf("write Linux owner helper response: %w", errOr(err, io.ErrShortWrite))
	}
	return nil
}

func readLinuxOwnerHelperV2Response(connection *net.UnixConn) (linuxOwnerHelperV2Response, []int, error) {
	data := make([]byte, linuxOwnerHelperV2MaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerHelperV2Response{}, nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerHelperV2Response{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerHelperV2MaxFrame {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper response exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var response linuxOwnerHelperV2Response
	if err := decoder.Decode(&response); err != nil {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Response{}, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper response has trailing JSON")
	}
	if response.Operation != "status" && (response.WitnessActionObserved || response.WitnessAction != "") {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper exposed a selected action outside authenticated Status")
	}
	return response, fds, nil
}

func (client *LinuxOwnerHelperV2Client) newRequest(operation string) linuxOwnerHelperV2Request {
	return linuxOwnerHelperV2Request{
		Version: linuxOwnerHelperV2Version, Operation: operation,
		ClientPID: client.client.PID, ClientIdentity: client.client.Identity,
		Grant: client.grant,
	}
}

func linuxOwnerHelperV2ResponseError(response linuxOwnerHelperV2Response) error {
	if response.Status == "recovery_only" || response.ErrorCode == "owner_lost" {
		return ErrLinuxOwnerHelperV2RecoveryOnly
	}
	switch response.ErrorCode {
	case "owner_lost_during_release":
		return ErrLinuxOwnerHelperV2OwnerLost
	case "handoff_not_committed":
		return ErrLinuxOwnerHelperV2NotCommitted
	case "initial_lease_not_acknowledged":
		return ErrLinuxOwnerHelperV2LeaseUnarmed
	case "release_closed_or_used":
		return ErrLinuxOwnerHelperV2ReleaseUsed
	default:
		if response.ErrorCode == "" {
			return fmt.Errorf("Linux owner helper rejected operation with status %q", response.Status)
		}
		return fmt.Errorf("Linux owner helper rejected operation (%s)", response.ErrorCode)
	}
}

func (client *LinuxOwnerHelperV2Client) setDeadline(ctx context.Context, timeout time.Duration) error {
	if client == nil || ctx == nil || client.connection == nil || client.closed {
		return errors.New("Linux owner helper client is closed")
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	return client.connection.SetDeadline(deadline)
}

func (client *LinuxOwnerHelperV2Client) request(ctx context.Context, request linuxOwnerHelperV2Request, fds ...int) (linuxOwnerHelperV2Response, []int, error) {
	if client == nil || ctx == nil {
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper request is incomplete")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return linuxOwnerHelperV2Response{}, nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, request, fds...); err != nil {
		return linuxOwnerHelperV2Response{}, nil, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return linuxOwnerHelperV2Response{}, nil, err
	}
	if !response.matches(client.grant, client.helper) || response.Operation != request.Operation {
		closeLinuxOwnerAnchorFDs(rights)
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper response differs from the exact request grant")
	}
	_ = client.connection.SetDeadline(time.Time{})
	return response, rights, nil
}

func (client *LinuxOwnerHelperV2Client) Prepare(ctx context.Context, onLauncherStart func(context.Context, authority.LinuxCgroupProcessIdentity) error) (*LinuxOwnerHelperV2Prepared, error) {
	if client == nil || onLauncherStart == nil {
		return nil, errors.New("Linux owner helper prepare requires a durable launcher-ack callback")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, client.newRequest("prepare")); err != nil {
		return nil, err
	}
	var callbackErr error
	for {
		response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
		if err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, err
		}
		if !response.matches(client.grant, client.helper) {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, errors.New("Linux owner helper prepare event differs from the exact RunKey and helper")
		}
		switch response.Operation {
		case "launcher-start":
			if len(rights) != 0 || response.Launcher == nil || response.Launcher.Validate() != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("Linux owner helper launcher event is incomplete")
			}
			callbackErr = onLauncherStart(ctx, *response.Launcher)
			ack := client.newRequest("launcher-ack")
			ack.Launcher = response.Launcher
			ack.Accepted = callbackErr == nil
			if err := writeLinuxOwnerHelperV2Request(client.connection, ack); err != nil {
				return nil, err
			}
		case "gate-ready":
			if response.Status != "ready" || response.Gate == nil || response.Gate.Validate() != nil ||
				response.Launcher == nil || response.LauncherExit == nil || response.UserNamespace == nil || len(rights) != 1 {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("Linux owner helper returned an incomplete parked-gate event")
			}
			if err := verifyLinuxOwnerAnchorPIDFD(rights[0], response.Gate.Process); err != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, fmt.Errorf("verify daemon mirror pidfd for parked A gate: %w", err)
			}
			pidfd := os.NewFile(uintptr(rights[0]), "symmetry-daemon-linux-owner-gate-pidfd")
			if pidfd == nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("wrap transferred parked-gate pidfd")
			}
			prepared := &LinuxOwnerHelperV2Prepared{
				Gate: *response.Gate, Launcher: *response.Launcher, LauncherExit: *response.LauncherExit,
				UserNamespace: *response.UserNamespace, PIDFD: pidfd,
			}
			if err := prepared.ValidateFor(client.grant, client.helper); err != nil {
				_ = prepared.Close()
				return nil, err
			}
			_ = client.connection.SetDeadline(time.Time{})
			return prepared, nil
		case "prepare-failed":
			failure := &LinuxOwnerHelperV2PrepareError{
				Code: response.ErrorCode, Launcher: cloneLinuxOwnerProcessIdentity(response.Launcher),
				LauncherExit: cloneLinuxOwnerAnchorExitProof(response.LauncherExit),
				GateProcess:  cloneLinuxOwnerProcessIdentity(response.GateProcess),
				Gate:         cloneLinuxOwnerGateIdentity(response.Gate), GateOutcome: response.GateOutcome,
			}
			if response.GateProcess != nil {
				if len(rights) != 1 || verifyLinuxOwnerAnchorPIDFDIdentity(rights[0], *response.GateProcess) != nil {
					closeLinuxOwnerAnchorFDs(rights)
					return nil, errors.New("unresolved Linux owner gate did not carry its exact pidfd")
				}
				failure.PIDFD = os.NewFile(uintptr(rights[0]), "symmetry-unresolved-linux-owner-gate-pidfd")
				if failure.PIDFD == nil {
					closeLinuxOwnerAnchorFDs(rights)
					return nil, errors.New("wrap unresolved Linux owner gate pidfd")
				}
			} else if len(rights) != 0 {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("gate-free Linux owner preparation carried an unexpected pidfd")
			}
			if failure.GateOutcome != nil {
				if failure.Launcher == nil || failure.LauncherExit == nil || failure.GateOutcome.ValidateFor(client.grant.LaunchToken, *failure.Launcher) != nil ||
					!failure.LauncherExit.ValidForOwnedDescendant(*failure.Launcher, client.helper, client.grant.Anchor, nil) {
					_ = failure.Close()
					return nil, errors.New("Linux no-gate result lacks exact authenticated producer-exit evidence")
				}
			}
			_ = client.connection.SetDeadline(time.Time{})
			if callbackErr != nil {
				return nil, errors.Join(fmt.Errorf("durably bind and arm exact Linux root launcher: %w", callbackErr), failure)
			}
			return nil, failure
		default:
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("unexpected Linux owner helper prepare event %q", response.Operation)
		}
	}
}

func (prepared *LinuxOwnerHelperV2Prepared) ValidateFor(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) error {
	if prepared == nil || grant.Validate() != nil || helper.Validate() != nil || prepared.Gate.Validate() != nil ||
		prepared.Launcher.Validate() != nil || prepared.LauncherExit.Validate() != nil ||
		!prepared.LauncherExit.ValidForOwnedDescendant(prepared.Launcher, helper, grant.Anchor, nil) ||
		prepared.Gate.Process.Validate() != nil || prepared.Gate.HostUID != grant.OwnerUID ||
		!prepared.Gate.Cgroup.Equal(grant.Leaf) || !prepared.UserNamespace.valid() || prepared.PIDFD == nil {
		return errors.New("Linux owner helper prepared-gate evidence is not bound to the exact run")
	}
	return verifyLinuxOwnerAnchorPIDFD(int(prepared.PIDFD.Fd()), prepared.Gate.Process)
}

func (client *LinuxOwnerHelperV2Client) Commit(ctx context.Context, prepared LinuxOwnerHelperV2Prepared) error {
	if err := prepared.ValidateFor(client.grant, client.helper); err != nil {
		return err
	}
	request := client.newRequest("commit")
	request.Gate = &prepared.Gate
	request.Launcher = &prepared.Launcher
	request.LauncherExit = &prepared.LauncherExit
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if (response.Status != "committed" && response.Status != "already_committed") || len(rights) != 0 {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

// AcknowledgeHelper opens the one RunKey-scoped launch phase. The caller must
// invoke it only after its Store callback durably bound this exact helper and
// grant; the helper has no Store access and will not infer that ACK from a
// returned PID or socket connection.
func (client *LinuxOwnerHelperV2Client) AcknowledgeHelper(ctx context.Context) error {
	request := client.newRequest("helper-bound")
	request.Helper = cloneLinuxOwnerProcessIdentity(&client.helper)
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "bound" && response.Status != "already_bound" {
		return errors.New("Linux owner helper did not accept the durable helper/grant acknowledgement")
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) RenewLease(ctx context.Context, deadline time.Duration, sequence uint64) error {
	if deadline <= 0 || sequence == 0 {
		return errors.New("Linux owner helper lease deadline and sequence must be positive")
	}
	request := client.newRequest("renew-lease")
	request.LeaseNanos = int64(deadline)
	request.Sequence = sequence
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "acknowledged" && response.Status != "already_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	if response.LeaseSequence != sequence {
		return errors.New("Linux owner helper lease acknowledgement sequence differs")
	}
	return nil
}

// Release requests one guarded production Clone release. The callback must
// durably persist and read back the exact checkout receipt; no B exec proceeds
// unless it returns nil, and an unknown result never retries RELEASE.
func (client *LinuxOwnerHelperV2Client) Release(
	ctx context.Context,
	command linuxgate.NativeCommand,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error,
) error {
	if client == nil || ctx == nil {
		return errors.New("Linux owner helper release client is incomplete")
	}
	if command.Clone == nil || onCheckoutReady == nil ||
		!linuxOwnerHelperV2CloneGrantMatchesRun(command.Clone, client.grant, command.Dir) {
		return errors.New("production Linux owner helper release requires the exact B-owned clone grant and durable checkout callback")
	}
	if err := command.Validate(); err != nil {
		return err
	}
	cloneGrant := command.Clone.Clone()
	wire := &linuxOwnerHelperV2Command{
		Path: command.Path, Args: append([]string(nil), command.Args...),
		Env: append([]string(nil), command.Env...), Dir: command.Dir, Clone: &cloneGrant,
	}
	request := client.newRequest("release")
	request.Command = wire
	fds := []int{-1, -1, -1}
	if command.Stdin != nil {
		fds[0] = int(command.Stdin.Fd())
	}
	if command.Stdout != nil {
		fds[1] = int(command.Stdout.Fd())
	}
	if command.Stderr != nil {
		fds[2] = int(command.Stderr.Fd())
	}
	return client.releaseWithCheckoutReady(ctx, request, fds, cloneGrant, onCheckoutReady)
}

func (client *LinuxOwnerHelperV2Client) releaseWithCheckoutReady(
	ctx context.Context,
	request linuxOwnerHelperV2Request,
	fds []int,
	cloneGrant authority.LinuxCloneGrant,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error,
) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, request, fds...); err != nil {
		client.invalidateLocked()
		return err
	}
	checkoutSeen := false
	for {
		response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
		closeLinuxOwnerAnchorFDs(rights)
		if err != nil {
			client.invalidateLocked()
			return err
		}
		if !response.matches(client.grant, client.helper) || len(rights) != 0 {
			client.invalidateLocked()
			return errors.New("Linux owner helper release event differs from the exact request grant")
		}
		switch response.Operation {
		case "checkout-ready":
			if checkoutSeen || response.Status != "ready" || response.CheckoutReceipt == nil ||
				!linuxOwnerHelperV2CheckoutReceiptMatches(*response.CheckoutReceipt, cloneGrant) {
				client.invalidateLocked()
				return errors.New("Linux owner helper checkout-ready receipt differs from the exact clone grant")
			}
			checkoutSeen = true
			receipt := response.CheckoutReceipt.Clone()
			var callbackErr error
			if response.OwnerLost {
				callbackErr = ErrLinuxOwnerHelperV2OwnerLost
			} else if !response.CommitAcknowledged || !response.ReleaseAttempted || response.Released {
				callbackErr = errors.New("Linux owner helper checkout event is outside the committed one-use release phase")
			} else {
				callbackErr = onCheckoutReady(ctx, receipt)
			}
			ack := client.newRequest("checkout-ready-ack")
			ack.Accepted = callbackErr == nil
			ack.CheckoutReceipt = &receipt
			if err := writeLinuxOwnerHelperV2Request(client.connection, ack); err != nil {
				client.invalidateLocked()
				return errors.Join(callbackErr, err)
			}
			if callbackErr != nil {
				final, finalRights, readErr := readLinuxOwnerHelperV2Response(client.connection)
				closeLinuxOwnerAnchorFDs(finalRights)
				if readErr != nil {
					client.invalidateLocked()
					return errors.Join(callbackErr, readErr)
				}
				if !final.matches(client.grant, client.helper) || len(finalRights) != 0 || final.Operation != "release" {
					client.invalidateLocked()
					return errors.Join(callbackErr, errors.New("Linux owner helper omitted the terminal release result after checkout rejection"))
				}
				_ = client.connection.SetDeadline(time.Time{})
				if final.Status == "released" {
					return errors.Join(callbackErr, errors.New("Linux owner helper released the Clone after checkout callback rejection"))
				}
				return callbackErr
			}
		case "release":
			if response.Status != "released" {
				_ = client.connection.SetDeadline(time.Time{})
				return linuxOwnerHelperV2ResponseError(response)
			}
			if !checkoutSeen || response.OwnerLost || !response.CommitAcknowledged || !response.ReleaseAttempted ||
				!response.Released || response.CheckoutReceipt != nil {
				client.invalidateLocked()
				return errors.New("Linux owner helper release response violates the durable Clone checkout contract")
			}
			_ = client.connection.SetDeadline(time.Time{})
			return nil
		default:
			client.invalidateLocked()
			return fmt.Errorf("Linux owner helper sent unexpected release event %q", response.Operation)
		}
	}
}

func (client *LinuxOwnerHelperV2Client) invalidateLocked() {
	if client == nil || client.closed {
		return
	}
	client.closed = true
	if client.connection != nil {
		_ = client.connection.Close()
	}
}

func (client *LinuxOwnerHelperV2Client) Stop(ctx context.Context) (LinuxOwnerHelperV2StopEvidence, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux owner helper stop client is incomplete")
	}
	stopClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	defer stopClient.Close()
	request := stopClient.newRequest("stop")
	response, rights, err := stopClient.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	if response.Status != "stopped" || response.StopEvidence == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.Join(ErrLinuxOwnerHelperV2StopUnproven, linuxOwnerHelperV2ResponseError(response))
	}
	if response.StopEvidence.PreGate != nil {
		if err := response.StopEvidence.ValidateFor(client.grant, client.helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
			return LinuxOwnerHelperV2StopEvidence{}, err
		}
		return *response.StopEvidence, nil
	}
	var launcher authority.LinuxCgroupProcessIdentity
	var gate authority.LinuxCgroupGateIdentity
	if response.Launcher != nil {
		launcher = *response.Launcher
	}
	if response.Gate != nil {
		gate = *response.Gate
	}
	if err := response.StopEvidence.ValidateFor(client.grant, client.helper, launcher, gate); err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	return *response.StopEvidence, nil
}

// FenceBeforeAbort terminalizes a helper whose protocol.Prepare entrypoint was
// never called. App then waits/reaps the helper through the anchor before it
// asks State to abort the reserved leaf; an empty cgroup alone is not enough.
func (client *LinuxOwnerHelperV2Client) FenceBeforeAbort(ctx context.Context) (LinuxOwnerHelperV2PreGateStopEvidence, error) {
	request := client.newRequest("fence-before-abort")
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, err
	}
	if response.Status != "fenced_before_abort" || response.StopEvidence == nil || response.StopEvidence.PreGate == nil {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, linuxOwnerHelperV2ResponseError(response)
	}
	if err := response.StopEvidence.PreGate.ValidateFor(client.grant, client.helper); err != nil || response.StopEvidence.PreGate.PrepareAttempted {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, errors.New("helper fence evidence is not the exact no-Prepare reservation branch")
	}
	return *response.StopEvidence.PreGate, nil
}

// Wait blocks for exact helper-owned gate wait/reap. The pidfd readiness
// notification alone is never returned as an exit result.
func (client *LinuxOwnerHelperV2Client) Wait(ctx context.Context) (LinuxOwnerHelperV2Exit, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait is incomplete")
	}
	waitClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	defer waitClient.Close()
	waitClient.mu.Lock()
	defer waitClient.mu.Unlock()
	if err := waitClient.setDeadline(ctx, 0); err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = waitClient.connection.Close() })
	defer stopClose()
	request := waitClient.newRequest("wait")
	if err := writeLinuxOwnerHelperV2Request(waitClient.connection, request); err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(waitClient.connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Exit{}, err
	}
	if !response.matches(waitClient.grant, waitClient.helper) || response.Operation != "wait" || len(rights) != 0 ||
		response.Status != "waited" || response.ExitResult == nil || response.Gate == nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Exit{}, linuxOwnerHelperV2ResponseError(response)
	}
	result := *response.ExitResult
	if err := result.Proof.Validate(); err != nil || !result.Process.Equal(response.Gate.Process) ||
		!result.Proof.ValidForOwnedDescendant(result.Process, waitClient.helper, waitClient.grant.Anchor, nil) {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait result lacks exact gate reaping evidence")
	}
	_ = waitClient.connection.SetDeadline(time.Time{})
	return result, nil
}

func (client *LinuxOwnerHelperV2Client) openParallelControl(ctx context.Context) (*LinuxOwnerHelperV2Client, error) {
	if client == nil || ctx == nil {
		return nil, errors.New("Linux owner helper parallel control connection is incomplete")
	}
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil || identity != client.client.Identity {
		return nil, fmt.Errorf("authenticate the original daemon for parallel helper control: %w", errOr(err, errors.New("daemon creation identity changed")))
	}
	connected, err := (&net.Dialer{Timeout: linuxOwnerHelperV2Timeout}).DialContext(ctx, "unixpacket", client.grant.RecoveryEndpoint)
	if err != nil {
		return nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	connection, ok := connected.(*net.UnixConn)
	if !ok {
		_ = connected.Close()
		return nil, errors.New("Linux owner helper endpoint is not a Unix seqpacket socket")
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, client.owner)
	if err != nil || peer.PID != client.helper.PID || peer.Identity != client.helper.Identity {
		_ = connection.Close()
		return nil, fmt.Errorf("authenticate exact helper for parallel wait: %w", errOr(err, errors.New("helper identity differs")))
	}
	parallel := &LinuxOwnerHelperV2Client{
		connection: connection, grant: client.grant, helper: client.helper,
		client: client.client, owner: client.owner, recoveryOnly: client.recoveryOnly,
	}
	if err := parallel.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		_ = connection.Close()
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(connection, parallel.newRequest("hello")); err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil || !response.matches(client.grant, client.helper) || response.Operation != "hello" || len(rights) != 0 ||
		(response.Status != "ready" && response.Status != "recovery_only") {
		_ = connection.Close()
		return nil, fmt.Errorf("authenticate parallel Linux owner helper session: %w", errOr(err, errors.New("helper denied the exact run grant")))
	}
	parallel.recoveryOnly = response.Status == "recovery_only"
	_ = connection.SetDeadline(time.Time{})
	return parallel, nil
}

func (client *LinuxOwnerHelperV2Client) Status(ctx context.Context) (LinuxOwnerHelperV2Status, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper status client is incomplete")
	}
	statusClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2Status{}, err
	}
	defer statusClient.Close()
	request := statusClient.newRequest("status")
	response, rights, err := statusClient.request(ctx, request)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, err
	}
	snapshot := response.StatusSnapshot
	if snapshot == nil || snapshot.LeaseSequence != response.LeaseSequence ||
		snapshot.LeaseLive && snapshot.LeaseSequence == 0 ||
		snapshot.StopAttempted && !snapshot.StopRequested ||
		snapshot.ReleaseInFlight && !response.ReleaseAttempted || response.Released && !response.ReleaseAttempted {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper Status lacks a consistent fresh lease/stop snapshot")
	}
	if response.WitnessActionObserved &&
		(response.WitnessAction != linuxOwnerHelperV2WitnessCrashAfterRelease || !response.ReleaseAttempted || !response.Released) ||
		!response.WitnessActionObserved && response.WitnessAction != "" {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper Status has an invalid post-RELEASE witness action")
	}
	status := LinuxOwnerHelperV2Status{
		RunKey: response.RunKey, Fence: response.Fence, Helper: response.Helper,
		State: response.Status, OwnerLost: response.OwnerLost,
		CommitAcknowledged: response.CommitAcknowledged,
		ReleaseAttempted:   response.ReleaseAttempted, Released: response.Released,
		LeaseLive: snapshot.LeaseLive, LeaseSequence: snapshot.LeaseSequence,
		StopRequested: snapshot.StopRequested, StopAttempted: snapshot.StopAttempted,
		ReleaseInFlight:     snapshot.ReleaseInFlight,
		WitnessDropObserved: response.WitnessDropObserved, WitnessDropOperation: response.WitnessDropOperation,
		WitnessActionObserved: response.WitnessActionObserved, WitnessAction: response.WitnessAction,
		Exit:          response.ExitResult,
		Launcher:      cloneLinuxOwnerProcessIdentity(response.Launcher),
		LauncherExit:  cloneLinuxOwnerAnchorExitProof(response.LauncherExit),
		Gate:          cloneLinuxOwnerGateIdentity(response.Gate),
		UserNamespace: response.UserNamespace,
		StopEvidence:  response.StopEvidence, ErrorCode: response.ErrorCode,
	}
	if response.Gate != nil {
		if len(rights) == 1 {
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(rights[0], response.Gate.Process); err != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return LinuxOwnerHelperV2Status{}, err
			}
			status.PIDFD = os.NewFile(uintptr(rights[0]), "symmetry-recovered-linux-owner-gate-pidfd")
			if status.PIDFD == nil {
				closeLinuxOwnerAnchorFDs(rights)
				return LinuxOwnerHelperV2Status{}, errors.New("wrap recovered Linux owner gate pidfd")
			}
			captured, err := CaptureLinuxUserNamespaceIdentity(response.Gate.Process.PID, response.Gate.Process.Identity, status.PIDFD)
			if err != nil || response.UserNamespace == nil || captured != *response.UserNamespace {
				_ = status.PIDFD.Close()
				return LinuxOwnerHelperV2Status{}, fmt.Errorf("verify recovered gate user namespace: %w", errOr(err, errors.New("namespace identity differs")))
			}
		} else if len(rights) != 0 || response.Status != "stopped" {
			closeLinuxOwnerAnchorFDs(rights)
			return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper recovery status lacks the exact retained gate pidfd")
		}
	} else if len(rights) != 0 {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("gate-free helper status carried an unexpected pidfd")
	}
	return status, nil
}

func (status *LinuxOwnerHelperV2Status) Close() error {
	if status == nil || status.PIDFD == nil {
		return nil
	}
	err := status.PIDFD.Close()
	status.PIDFD = nil
	return err
}

func (client *LinuxOwnerHelperV2Client) AcknowledgeStopReceipt(ctx context.Context, receipt authority.StopReceipt) error {
	request := client.newRequest("ack-stop-receipt")
	request.Receipt = &receipt
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "receipt_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) AcknowledgePreGateStopReceipt(ctx context.Context, receipt authority.LinuxCgroupPreGateStopReceipt) error {
	request := client.newRequest("ack-stop-receipt")
	request.PreGateReceipt = &receipt
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "receipt_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterClear(ctx context.Context, proof authority.SupervisorHandoffReleaseProof) error {
	request := client.newRequest("release-run")
	request.ReleaseProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterPreGateClear(ctx context.Context, proof authority.LinuxCgroupPreGateReleaseProof) error {
	request := client.newRequest("release-run")
	request.PreGateReleaseProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterReservationAbort(ctx context.Context, proof authority.LinuxCgroupReservationAbortProof) error {
	request := client.newRequest("release-run")
	request.AbortProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) Close() error {
	if client == nil {
		return nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed {
		return nil
	}
	client.closed = true
	if client.connection == nil {
		return nil
	}
	err := client.connection.Close()
	client.connection = nil
	return err
}

type linuxOwnerHelperV2Lease struct {
	mu         sync.Mutex
	sequence   uint64
	generation uint64
	duration   time.Duration
	deadline   time.Time
	timer      *time.Timer
	stopped    bool
	wake       chan struct{}
}

func newLinuxOwnerHelperV2Lease() *linuxOwnerHelperV2Lease {
	return &linuxOwnerHelperV2Lease{wake: make(chan struct{}, 1)}
}

func (lease *linuxOwnerHelperV2Lease) renew(duration time.Duration, sequence uint64) (bool, error) {
	if lease == nil || duration <= 0 || sequence == 0 {
		return false, errors.New("Linux owner helper lease is incomplete")
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.stopped || !lease.deadline.IsZero() && !time.Now().Before(lease.deadline) {
		lease.latchLocked()
		return false, errors.New("Linux owner helper lease is expired")
	}
	if sequence == lease.sequence {
		if !lease.deadline.IsZero() && duration == lease.duration {
			return true, nil
		}
		return false, errors.New("Linux owner helper lease sequence replay differs")
	}
	if sequence < lease.sequence || lease.sequence == 0 && sequence != 1 || lease.sequence > 0 && sequence != lease.sequence+1 {
		return false, errors.New("Linux owner helper lease sequence is not the exact next value")
	}
	if lease.timer != nil {
		lease.timer.Stop()
	}
	lease.sequence = sequence
	lease.generation++
	generation := lease.generation
	lease.duration = duration
	lease.deadline = time.Now().Add(duration)
	lease.timer = time.AfterFunc(duration, func() {
		lease.mu.Lock()
		if lease.sequence == sequence && lease.generation == generation &&
			!lease.stopped && !time.Now().Before(lease.deadline) {
			lease.latchLocked()
		}
		lease.mu.Unlock()
	})
	return false, nil
}

func (lease *linuxOwnerHelperV2Lease) latchLocked() {
	if lease == nil || lease.stopped {
		return
	}
	lease.stopped = true
	lease.deadline = time.Time{}
	if lease.timer != nil {
		lease.timer.Stop()
		lease.timer = nil
	}
	select {
	case lease.wake <- struct{}{}:
	default:
	}
}

func (lease *linuxOwnerHelperV2Lease) stop() {
	if lease == nil {
		return
	}
	lease.mu.Lock()
	lease.latchLocked()
	lease.mu.Unlock()
}

func (lease *linuxOwnerHelperV2Lease) isStopped() bool {
	if lease == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.stopped
}

func (lease *linuxOwnerHelperV2Lease) snapshot() (uint64, bool) {
	if lease == nil {
		return 0, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.deadline.IsZero() || lease.stopped || !time.Now().Before(lease.deadline) {
		if !lease.stopped {
			lease.latchLocked()
		}
		return lease.sequence, false
	}
	return lease.sequence, true
}

// peekStatus reports lease liveness without latching expiry or waking the
// owner-loss watcher. Status is an observation; effectful paths own expiry.
func (lease *linuxOwnerHelperV2Lease) peekStatus() (uint64, bool) {
	if lease == nil {
		return 0, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.sequence, !lease.deadline.IsZero() && !lease.stopped && time.Now().Before(lease.deadline)
}

func (lease *linuxOwnerHelperV2Lease) releaseGuard() (uint64, time.Time, bool) {
	if lease == nil {
		return 0, time.Time{}, false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.deadline.IsZero() || lease.stopped || !time.Now().Before(lease.deadline) {
		if !lease.stopped {
			lease.latchLocked()
		}
		return lease.sequence, time.Time{}, false
	}
	return lease.sequence, lease.deadline, true
}

type linuxOwnerHelperV2 struct {
	owner                     linuxgate.OwnerConfig
	grant                     LinuxOwnerAnchorGrant
	helper                    authority.LinuxCgroupProcessIdentity
	initialOwner              authority.LinuxCgroupProcessIdentity
	anchorClient              *LinuxOwnerAnchorClient
	parent                    *LinuxCgroupV2Parent
	leaf                      *LinuxCgroupV2Leaf
	listener                  *net.UnixListener
	ownerPipe                 *os.File
	lease                     *linuxOwnerHelperV2Lease
	witnessDropResponse       string
	witnessHelperAction       string
	witnessHoldForReleaseFile bool
	witnessRunKey             LinuxOwnerAnchorRunKey

	stateMu                    sync.Mutex
	activeConns                map[*net.UnixConn]struct{}
	activeOwner                authority.LinuxCgroupProcessIdentity
	recoveryOwner              authority.LinuxCgroupProcessIdentity
	ownerLost                  bool
	finalized                  bool
	finalizing                 bool
	anchorTakeoverRequested    bool
	finalization               *linuxOwnerHelperV2Finalization
	prepareStarted             bool
	prepareDone                chan struct{}
	prepareCancel              context.CancelFunc
	prepareResponse            *linuxOwnerHelperV2Response
	launcher                   *authority.LinuxCgroupProcessIdentity
	launcherExit               *authority.LinuxCgroupExitProof
	gateOutcome                *authority.LinuxCgroupGateProductionResult
	gate                       *authority.LinuxCgroupGateIdentity
	gateUserNamespace          *LinuxUserNamespaceIdentity
	prepared                   *linuxgate.PreparedGate
	untrustedProcess           *authority.LinuxCgroupProcessIdentity
	helperBoundAck             bool
	commitAcknowledged         bool
	releaseAttempted           bool
	releaseInFlight            bool
	releaseDone                chan struct{}
	releaseCancel              context.CancelFunc
	released                   bool
	stopRequested              bool
	stopAttempted              bool
	stopInFlight               bool
	stopDone                   chan struct{}
	descendantReaped           map[int]authority.LinuxCgroupProcessIdentity
	stopEvidence               *LinuxOwnerHelperV2StopEvidence
	stopErrorCode              string
	witnessDropUsed            bool
	witnessActionObserved      bool
	witnessAction              string
	witnessExitRequested       bool
	witnessExitCause           error
	receiptAcknowledged        bool
	acknowledgedReceipt        *authority.StopReceipt
	acknowledgedPreGateReceipt *authority.LinuxCgroupPreGateStopReceipt
	waitMu                     sync.Mutex
	waitStarted                bool
	waitDone                   chan struct{}
	waitResult                 *LinuxOwnerHelperV2Exit
	waitError                  error
	untrustedWaitMu            sync.Mutex
	untrustedWaitAttempted     bool
	untrustedWaitResult        *LinuxOwnerHelperV2Exit
	untrustedWaitError         error
}

// linuxOwnerHelperV2Finalization serializes pin retirement for one exact
// durable proof. The record remains as the in-memory idempotency tombstone
// after success; it is not a journal and cannot mint State authority.
type linuxOwnerHelperV2Finalization struct {
	kind         string
	releaseProof *authority.SupervisorHandoffReleaseProof
	preGateProof *authority.LinuxCgroupPreGateReleaseProof
	abortProof   *authority.LinuxCgroupReservationAbortProof
	done         chan struct{}
	err          error
	completed    bool
}

func (finalization *linuxOwnerHelperV2Finalization) matches(request linuxOwnerHelperV2Request) bool {
	if finalization == nil {
		return false
	}
	switch finalization.kind {
	case "normal":
		return request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil &&
			finalization.releaseProof != nil && finalization.releaseProof.Equal(*request.ReleaseProof)
	case "pre-gate":
		return request.ReleaseProof == nil && request.PreGateReleaseProof != nil && request.AbortProof == nil &&
			finalization.preGateProof != nil && finalization.preGateProof.Equal(*request.PreGateReleaseProof)
	case "abort":
		return request.ReleaseProof == nil && request.PreGateReleaseProof == nil && request.AbortProof != nil &&
			finalization.abortProof != nil && finalization.abortProof.Equal(*request.AbortProof)
	default:
		return false
	}
}

func (helper *linuxOwnerHelperV2) beginFinalization(request linuxOwnerHelperV2Request) (*linuxOwnerHelperV2Finalization, bool, bool, error) {
	if helper == nil {
		return nil, false, false, errors.New("Linux owner helper finalizer is unavailable")
	}
	if request.Operation != "release-run" || request.Helper != nil || request.Launcher != nil || request.Gate != nil ||
		request.LauncherExit != nil || request.Sequence != 0 || request.LeaseNanos != 0 || request.Accepted ||
		request.Command != nil || request.CheckoutReceipt != nil || request.Receipt != nil || request.PreGateReceipt != nil {
		return nil, false, false, errors.New("Linux owner helper finalization request carries unexpected fields")
	}
	var record linuxOwnerHelperV2Finalization
	switch {
	case request.AbortProof != nil && request.ReleaseProof == nil && request.PreGateReleaseProof == nil:
		if request.AbortProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper abort finalization proof is invalid")
		}
		record.kind = "abort"
		proof := request.AbortProof.Clone()
		record.abortProof = &proof
	case request.PreGateReleaseProof != nil && request.ReleaseProof == nil && request.AbortProof == nil:
		if request.PreGateReleaseProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper pre-gate finalization proof is invalid")
		}
		record.kind = "pre-gate"
		proof := request.PreGateReleaseProof.Clone()
		record.preGateProof = &proof
	case request.ReleaseProof != nil && request.PreGateReleaseProof == nil && request.AbortProof == nil:
		if request.ReleaseProof.Validate() != nil {
			return nil, false, false, errors.New("Linux owner helper normal finalization proof is invalid")
		}
		record.kind = "normal"
		proof := request.ReleaseProof.Clone()
		record.releaseProof = &proof
	default:
		return nil, false, false, errors.New("Linux owner helper finalization requires exactly one durable proof")
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	if helper.finalized {
		if helper.finalization != nil && helper.finalization.matches(request) && helper.finalization.err == nil {
			return helper.finalization, false, true, nil
		}
		return nil, false, false, errors.New("Linux owner helper was finalized with a different proof")
	}
	if helper.finalizing {
		if helper.finalization == nil || !helper.finalization.matches(request) {
			return nil, false, false, errors.New("Linux owner helper is finalizing a different proof")
		}
		return helper.finalization, false, false, nil
	}
	record.done = make(chan struct{})
	helper.finalization = &record
	helper.finalizing = true
	return helper.finalization, true, false, nil
}

func (helper *linuxOwnerHelperV2) finishFinalization(record *linuxOwnerHelperV2Finalization, err error, retryable bool) {
	if helper == nil || record == nil {
		return
	}
	helper.stateMu.Lock()
	if helper.finalization != record || record.completed {
		helper.stateMu.Unlock()
		return
	}
	record.err = err
	record.completed = true
	takeover := false
	if err == nil {
		helper.finalized = true
		helper.finalizing = false
	} else if retryable {
		helper.finalization = nil
		helper.finalizing = false
		takeover = helper.ownerLost
	} else {
		takeover = helper.ownerLost
	}
	close(record.done)
	helper.stateMu.Unlock()
	if takeover {
		helper.requestAnchorTakeover()
	}
}

func waitLinuxOwnerHelperV2Finalization(record *linuxOwnerHelperV2Finalization) error {
	if record == nil || record.done == nil {
		return errors.New("Linux owner helper finalization wait is incomplete")
	}
	timer := time.NewTimer(linuxOwnerHelperV2Timeout)
	defer timer.Stop()
	select {
	case <-record.done:
		return record.err
	case <-timer.C:
		return errors.New("Linux owner helper finalization remained in progress")
	}
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

func (helper *linuxOwnerHelperV2) serve() (result error) {
	if helper == nil || helper.listener == nil || helper.leaf == nil || helper.parent == nil || helper.ownerPipe == nil {
		return errors.New("Linux owner helper lifecycle is incomplete")
	}
	watchContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchersDone := make(chan struct{}, 2)
	var sessions sync.WaitGroup
	go func() {
		defer func() { watchersDone <- struct{}{} }()
		helper.watchOwnerPipe(watchContext)
	}()
	go func() {
		defer func() { watchersDone <- struct{}{} }()
		helper.watchLease(watchContext)
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	signalDone := make(chan struct{})
	go func() {
		defer close(signalDone)
		select {
		case <-watchContext.Done():
		case <-signals:
			if err := helper.markOwnerLost(); err != nil {
				helper.requestAnchorTakeover()
			}
		}
	}()
	defer func() {
		_ = helper.listener.Close()
		// Keep owner/lease watchers alive while active sessions quiesce. A
		// failed session drain must hand control to the anchor, not strand a
		// live helper outside its pidfd-death observer.
		sessionsDone := make(chan struct{})
		go func() {
			sessions.Wait()
			close(sessionsDone)
		}()
		drainTimer := time.NewTimer(linuxOwnerHelperV2StopTimeout)
		select {
		case <-sessionsDone:
			if !drainTimer.Stop() {
				select {
				case <-drainTimer.C:
				default:
				}
			}
		case <-drainTimer.C:
			result = errors.Join(result, errors.New("Linux owner helper control sessions did not quiesce; yielding to anchor containment"))
			helper.requestAnchorTakeover()
			return
		}
		cancel()
		<-watchersDone
		<-watchersDone
		<-signalDone
		if helper.ownerPipe != nil {
			_ = helper.ownerPipe.Close()
		}
		helper.lease.stop()
		if helper.prepared != nil {
			_ = helper.prepared.Close()
		}
		if helper.leaf != nil {
			_ = helper.leaf.Close()
		}
		if helper.parent != nil {
			_ = helper.parent.Close()
		}
	}()
	for {
		connection, err := helper.listener.AcceptUnix()
		if err != nil {
			if helper.isFinalized() {
				return nil
			}
			if helper.isWitnessExitRequested() {
				return helper.witnessExitError()
			}
			acceptErr := fmt.Errorf("accept Linux owner helper control connection: %w", sanitizeLinuxOwnerAnchorSocketError(err))
			ownerErr := helper.markOwnerLost()
			if ownerErr != nil {
				helper.requestAnchorTakeover()
			}
			return errors.Join(acceptErr, ownerErr)
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			_ = helper.serveOwnerConnection(connection)
			_ = connection.Close()
			if helper.isFinalized() {
				_ = helper.listener.Close()
			}
		}()
	}
}

func (helper *linuxOwnerHelperV2) watchOwnerPipe(ctx context.Context) {
	fd := int(helper.ownerPipe.Fd())
	for ctx.Err() == nil {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		_, err := unix.Poll(poll, int(linuxOwnerHelperV2PollInterval/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
		if poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) == 0 {
			continue
		}
		var byteValue [1]byte
		n, readErr := unix.Read(fd, byteValue[:])
		if readErr == nil && n == 0 || readErr != nil && !errors.Is(readErr, unix.EINTR) && !errors.Is(readErr, unix.EAGAIN) {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
		if n != 0 {
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
			return
		}
	}
}

func (helper *linuxOwnerHelperV2) watchLease(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-helper.lease.wake:
			if stopErr := helper.markOwnerLost(); stopErr != nil {
				helper.requestAnchorTakeover()
			}
		}
	}
}

func (helper *linuxOwnerHelperV2) markOwnerLost() error {
	if helper == nil {
		return nil
	}
	helper.stateMu.Lock()
	if helper.finalized || helper.ownerLost {
		helper.stateMu.Unlock()
		return nil
	}
	helper.ownerLost = true
	if helper.finalizing {
		takeover := helper.finalization != nil && helper.finalization.completed && helper.finalization.err != nil
		connections := make([]*net.UnixConn, 0, len(helper.activeConns))
		for connection := range helper.activeConns {
			connections = append(connections, connection)
		}
		helper.stateMu.Unlock()
		helper.lease.stop()
		for _, connection := range connections {
			_ = connection.Close()
		}
		if takeover {
			helper.requestAnchorTakeover()
		}
		return nil
	}
	if helper.prepareCancel != nil {
		helper.prepareCancel()
	}
	if helper.releaseCancel != nil {
		helper.releaseCancel()
	}
	if helper.prepared != nil && !helper.released {
		_ = helper.prepared.Abort()
	}
	connections := make([]*net.UnixConn, 0, len(helper.activeConns))
	for connection := range helper.activeConns {
		connections = append(connections, connection)
	}
	helper.stateMu.Unlock()
	helper.lease.stop()
	for _, connection := range connections {
		_ = connection.Close()
	}
	stopContext, cancel := context.WithTimeout(context.Background(), linuxOwnerHelperV2StopTimeout)
	_, err := helper.stopOwned(stopContext)
	cancel()
	return err
}

func (helper *linuxOwnerHelperV2) requestAnchorTakeover() {
	if helper == nil {
		return
	}
	helper.stateMu.Lock()
	if helper.anchorTakeoverRequested {
		helper.stateMu.Unlock()
		return
	}
	helper.anchorTakeoverRequested = true
	connections := make([]*net.UnixConn, 0, len(helper.activeConns))
	for connection := range helper.activeConns {
		connections = append(connections, connection)
	}
	listener := helper.listener
	helper.stateMu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
	// An unresolved helper cannot keep detached B alive by staying resident;
	// its anchor owns the independent pidfd watcher and pinned-leaf fallback.
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		os.Exit(1)
	}
}

func (helper *linuxOwnerHelperV2) isFinalized() bool {
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return helper.finalized
}

func (helper *linuxOwnerHelperV2) isWitnessExitRequested() bool {
	if helper == nil {
		return false
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return helper.witnessExitRequested
}

func (helper *linuxOwnerHelperV2) witnessExitError() error {
	if helper == nil {
		return errLinuxOwnerHelperV2WitnessExit
	}
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return errors.Join(errLinuxOwnerHelperV2WitnessExit, helper.witnessExitCause)
}

func (helper *linuxOwnerHelperV2) serveOwnerConnection(connection *net.UnixConn) error {
	if err := connection.SetDeadline(time.Now().Add(linuxOwnerAnchorRequestTimeout)); err != nil {
		return err
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, helper.owner)
	anchorPeer := peer.PID == helper.grant.Anchor.PID && peer.Identity == helper.grant.Anchor.Identity
	if err != nil || linuxOwnerCgroupPathsOverlap(peer.CgroupPath, helper.grant.Parent.Path) && !anchorPeer {
		return errors.Join(errors.New("Linux owner helper rejected an untrusted control peer"), err)
	}
	request, receivedFDs, err := readLinuxOwnerHelperV2Request(connection)
	closeLinuxOwnerAnchorFDs(receivedFDs)
	if err != nil {
		return err
	}
	if request.Operation != "hello" || !helper.requestMatchesPeer(request, peer) {
		return errors.New("Linux owner helper hello differs from exact SO_PEERCRED or RunKey grant")
	}
	helper.stateMu.Lock()
	if helper.finalized {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper already completed durable clear")
	}
	recoveryOnly := helper.ownerLost
	if !recoveryOnly && !anchorPeer && !peerProcessEqual(peer, helper.initialOwner) {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper initial control peer differs from the authenticated StartHelper caller")
	}
	peerIdentity := authority.LinuxCgroupProcessIdentity{PID: peer.PID, Identity: peer.Identity}
	if recoveryOnly && !anchorPeer {
		if helper.recoveryOwner.Validate() != nil {
			helper.recoveryOwner = peerIdentity
		} else if !helper.recoveryOwner.Equal(peerIdentity) {
			helper.stateMu.Unlock()
			return errors.New("Linux owner helper recovery peer differs from its first authenticated attacher")
		}
	} else if !anchorPeer && helper.activeOwner.Validate() != nil {
		helper.activeOwner = peerIdentity
	} else if !anchorPeer && !helper.activeOwner.Equal(peerIdentity) {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper control peer changed within the initial owner phase")
	}
	helper.activeConns[connection] = struct{}{}
	helper.stateMu.Unlock()
	defer func() {
		helper.stateMu.Lock()
		delete(helper.activeConns, connection)
		helper.stateMu.Unlock()
	}()
	helloStatus := "ready"
	if recoveryOnly {
		helloStatus = "recovery_only"
	}
	if err := helper.writeResponse(connection, "hello", helloStatus, nil, nil, nil, nil, nil, 0, nil, ""); err != nil {
		return err
	}
	for {
		if err := connection.SetDeadline(time.Now().Add(linuxOwnerHelperV2Timeout)); err != nil {
			return err
		}
		request, rights, err := readLinuxOwnerHelperV2Request(connection)
		if err != nil {
			return err
		}
		if !helper.requestMatchesPeer(request, peer) {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner helper request differs from exact SO_PEERCRED or RunKey grant")
		}
		if anchorPeer && request.Operation != "status" && request.Operation != "stop" && request.Operation != "release-run" {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner anchor recovery session may only inspect, stop, or finalize its exact helper")
		}
		finished, handleErr := helper.handleRequest(connection, request, rights)
		closeLinuxOwnerAnchorFDs(rights)
		if handleErr != nil {
			return handleErr
		}
		if finished {
			return nil
		}
	}
}

func (helper *linuxOwnerHelperV2) requestMatchesPeer(request linuxOwnerHelperV2Request, peer linuxOwnerAnchorPeer) bool {
	return request.Version == linuxOwnerHelperV2Version && request.Grant.Equal(helper.grant) &&
		request.ClientPID == peer.PID && request.ClientIdentity == peer.Identity
}

func peerProcessEqual(peer linuxOwnerAnchorPeer, expected authority.LinuxCgroupProcessIdentity) bool {
	return expected.Validate() == nil && peer.PID == expected.PID && peer.Identity == expected.Identity
}

func (helper *linuxOwnerHelperV2) baseResponse(operation, status string) linuxOwnerHelperV2Response {
	return linuxOwnerHelperV2Response{
		Version: linuxOwnerHelperV2Version, Operation: operation, Status: status,
		RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
		Helper: helper.helper,
	}
}

func (helper *linuxOwnerHelperV2) writeResponse(connection *net.UnixConn, operation, status string, launcher *authority.LinuxCgroupProcessIdentity, gate *authority.LinuxCgroupGateIdentity, launcherExit *authority.LinuxCgroupExitProof, gateProcess *authority.LinuxCgroupProcessIdentity, gateOutcome *authority.LinuxCgroupGateProductionResult, leaseSequence uint64, evidence *LinuxOwnerHelperV2StopEvidence, errorCode string, fds ...int) error {
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

func (helper *linuxOwnerHelperV2) handleRequest(connection *net.UnixConn, request linuxOwnerHelperV2Request, rights []int) (bool, error) {
	helper.stateMu.Lock()
	ownerLost := helper.ownerLost
	finalized := helper.finalized
	helper.stateMu.Unlock()
	if finalized {
		if request.Operation == "release-run" {
			return helper.handleFinalRelease(connection, request)
		}
		return false, helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "already_finalized")
	}
	helper.stateMu.Lock()
	finalizing := helper.finalizing
	helper.stateMu.Unlock()
	if finalizing && request.Operation != "status" && request.Operation != "release-run" {
		return false, helper.writeResponse(connection, request.Operation, "recovery_only", nil, nil, nil, nil, nil, 0, nil, "final_release_pending")
	}
	if ownerLost && request.Operation != "status" && request.Operation != "stop" && request.Operation != "fence-before-abort" && request.Operation != "ack-stop-receipt" && request.Operation != "release-run" {
		return false, helper.writeResponse(connection, request.Operation, "recovery_only", nil, nil, nil, nil, nil, 0, nil, "owner_lost")
	}
	switch request.Operation {
	case "status":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper status request carried unexpected descriptors")
		}
		return false, helper.handleStatus(connection)
	case "prepare":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper prepare request carried unexpected descriptors")
		}
		return false, helper.handlePrepare(connection)
	case "helper-bound":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper binding ACK carried unexpected descriptors")
		}
		return false, helper.handleHelperBound(connection, request)
	case "launcher-ack":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper launcher ACK carried unexpected descriptors")
		}
		return false, helper.handleLauncherAck(connection, request)
	case "commit":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper commit request carried unexpected descriptors")
		}
		return false, helper.handleCommit(connection, request)
	case "renew-lease":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper lease request carried unexpected descriptors")
		}
		return false, helper.handleRenewLease(connection, request)
	case "release":
		return false, helper.handleRelease(connection, request, rights)
	case "wait":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper wait request carried unexpected descriptors")
		}
		return false, helper.handleWait(connection)
	case "stop":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper stop request carried unexpected descriptors")
		}
		return false, helper.handleStop(connection)
	case "fence-before-abort":
		if len(rights) != 0 {
			return false, errors.New("Linux pre-abort fence request carried unexpected descriptors")
		}
		err := helper.handleFenceBeforeAbort(connection)
		return helper.isFinalized(), err
	case "ack-stop-receipt":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper receipt ACK carried unexpected descriptors")
		}
		return false, helper.handleReceiptAck(connection, request)
	case "release-run":
		if len(rights) != 0 {
			return false, errors.New("Linux owner helper final release carried unexpected descriptors")
		}
		return helper.handleFinalRelease(connection, request)
	default:
		return false, helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "unsupported_operation")
	}
}

func (helper *linuxOwnerHelperV2) handleStatus(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	userNamespace := helper.gateUserNamespace
	launcherExit := cloneLinuxOwnerAnchorExitProof(helper.launcherExit)
	leaseSequence, leaseLive := helper.lease.peekStatus()
	ownerLost := helper.ownerLost
	prepared := helper.prepareResponse != nil
	evidence := helper.stopEvidence
	commitAcknowledged := helper.commitAcknowledged
	releaseAttempted := helper.releaseAttempted
	releaseInFlight := helper.releaseInFlight
	released := helper.released
	stopRequested := helper.stopRequested
	stopAttempted := helper.stopAttempted
	witnessDropObserved := helper.witnessDropUsed
	witnessDropOperation := ""
	if witnessDropObserved {
		witnessDropOperation = helper.witnessDropResponse
	}
	witnessActionObserved := helper.witnessActionObserved
	witnessAction := helper.witnessAction
	if witnessActionObserved && (witnessAction != linuxOwnerHelperV2WitnessCrashAfterRelease ||
		helper.witnessRunKey != helper.grant.RunKey || !released || !releaseAttempted) ||
		!witnessActionObserved && witnessAction != "" {
		helper.stateMu.Unlock()
		return errors.New("Linux owner helper witness action is not bound to a successful exact RELEASE")
	}
	if !witnessActionObserved {
		witnessAction = ""
	}
	var errorCode string
	if evidence == nil && helper.stopAttempted {
		errorCode = helper.stopErrorCode
	}
	var gatePIDFD int = -1
	if gate != nil && helper.prepared != nil {
		if retained := helper.prepared.PIDFD(); retained != nil {
			gatePIDFD, _ = unix.FcntlInt(retained.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		}
	}
	helper.stateMu.Unlock()
	status := "active"
	if ownerLost {
		status = "recovery_only"
	}
	if evidence != nil {
		status = "stopped"
	}
	if !prepared && !ownerLost {
		status = "ready"
	}
	response := helper.baseResponse("status", status)
	response.Launcher = launcher
	response.Gate = gate
	response.LauncherExit = launcherExit
	response.UserNamespace = userNamespace
	response.StopEvidence = evidence
	response.ErrorCode = errorCode
	response.OwnerLost = ownerLost
	response.WitnessDropObserved = witnessDropObserved
	response.WitnessDropOperation = witnessDropOperation
	response.WitnessActionObserved = witnessActionObserved
	response.WitnessAction = witnessAction
	response.CommitAcknowledged = commitAcknowledged
	response.ReleaseAttempted = releaseAttempted
	response.Released = released
	response.LeaseSequence = leaseSequence
	response.StatusSnapshot = &linuxOwnerHelperV2StatusSnapshot{
		LeaseLive: leaseLive, LeaseSequence: leaseSequence,
		StopRequested: stopRequested, StopAttempted: stopAttempted, ReleaseInFlight: releaseInFlight,
	}
	helper.waitMu.Lock()
	if helper.waitDone != nil {
		select {
		case <-helper.waitDone:
			response.ExitResult = helper.waitResult
			if helper.waitError != nil {
				response.ErrorCode = "gate_wait_unproven"
			}
		default:
		}
	}
	helper.waitMu.Unlock()
	if gatePIDFD >= 0 {
		defer unix.Close(gatePIDFD)
		return writeLinuxOwnerHelperV2Response(connection, response, gatePIDFD)
	}
	return writeLinuxOwnerHelperV2Response(connection, response)
}

func (helper *linuxOwnerHelperV2) handlePrepare(connection *net.UnixConn) error {
	helper.stateMu.Lock()
	if !helper.helperBoundAck || helper.ownerLost || helper.stopRequested || helper.stopAttempted {
		helper.stateMu.Unlock()
		code := "helper_bind_unacknowledged"
		if helper.ownerLost || helper.stopRequested || helper.stopAttempted {
			code = "owner_lost"
		}
		return helper.writeResponse(connection, "prepare-failed", "rejected", nil, nil, nil, nil, nil, 0, nil, code)
	}
	if helper.prepareResponse != nil {
		response := *helper.prepareResponse
		helper.stateMu.Unlock()
		return helper.sendPrepareResponse(connection, response)
	}
	if helper.prepareStarted {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "prepare-failed", "unresolved", nil, nil, nil, nil, nil, 0, nil, "prepare_in_progress")
	}
	helper.prepareStarted = true
	helper.prepareDone = make(chan struct{})
	prepareContext, cancel := context.WithCancel(context.Background())
	helper.prepareCancel = cancel
	done := helper.prepareDone
	helper.stateMu.Unlock()
	defer func() {
		cancel()
		helper.stateMu.Lock()
		if helper.prepareDone == done {
			close(done)
		}
		helper.stateMu.Unlock()
	}()
	if err := helper.leaf.VerifyKillControl(); err != nil {
		return helper.cachePrepareFailure(connection, "leaf_kill_control_unavailable", nil)
	}

	birth, err := helper.leaf.BirthFD()
	if err != nil {
		return helper.cachePrepareFailure(connection, "leaf_unavailable", nil)
	}
	defer birth.Close()
	gateAuth := linuxgate.GateProductionAuth{LaunchToken: helper.grant.LaunchToken, Secret: helper.grant.Secret}
	var launcherIdentity authority.LinuxCgroupProcessIdentity
	onLauncherStart := func(ctx context.Context, launcher linuxgate.LauncherIdentity, pidfd *os.File) error {
		process := authority.LinuxCgroupProcessIdentity{PID: launcher.PID, Identity: launcher.Identity}
		if process.Validate() != nil || pidfd == nil {
			return errors.New("Linux root launcher event has no exact identity or pidfd")
		}
		if err := helper.anchorClient.RegisterProcess(ctx, helper.grant, helper.helper, helper.helper, process, "launcher", int(pidfd.Fd())); err != nil {
			return fmt.Errorf("register exact root launcher with owner anchor: %w", err)
		}
		launcherIdentity = process
		helper.stateMu.Lock()
		helper.launcher = cloneLinuxOwnerProcessIdentity(&process)
		helper.stateMu.Unlock()
		if err := helper.writeResponse(connection, "launcher-start", "awaiting_durable_grant", &process, nil, nil, nil, nil, 0, nil, ""); err != nil {
			return err
		}
		request, rights, err := readLinuxOwnerHelperV2Request(connection)
		if err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return err
		}
		if len(rights) != 0 {
			closeLinuxOwnerAnchorFDs(rights)
			return errors.New("Linux owner launcher ACK carried unexpected descriptors")
		}
		helper.stateMu.Lock()
		activeOwner := helper.activeOwner
		helper.stateMu.Unlock()
		if request.Version != linuxOwnerHelperV2Version || !request.Grant.Equal(helper.grant) ||
			request.ClientPID != activeOwner.PID || request.ClientIdentity != activeOwner.Identity ||
			request.Operation != "launcher-ack" || request.Launcher == nil || !request.Launcher.Equal(process) || !request.Accepted {
			return errors.New("daemon did not return an exact durable BindLauncher+ArmGrant ACK")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	preparedGate, prepareErr := linuxgate.Prepare(prepareContext, int(birth.Fd()), gateAuth, onLauncherStart)
	var gateIdentity authority.LinuxCgroupGateIdentity
	var processIdentity *authority.LinuxCgroupProcessIdentity
	var userNamespace *LinuxUserNamespaceIdentity
	var launcherExit *authority.LinuxCgroupExitProof
	var gatePIDFD int = -1
	if preparedGate != nil {
		identity := preparedGate.Identity()
		process, identityErr := linuxOwnerHelperGateProcessIdentity(identity)
		gateFile := preparedGate.PIDFD()
		if identityErr == nil && gateFile != nil {
			processIdentity = &process
			gatePIDFD = int(gateFile.Fd())
			if prepareErr == nil {
				namespace, namespaceErr := CaptureLinuxUserNamespaceIdentity(process.PID, process.Identity, preparedGate.PIDFD())
				if namespaceErr != nil || namespace.Device != identity.UserNSDevice || namespace.Inode != identity.UserNSInode {
					prepareErr = errors.Join(prepareErr, fmt.Errorf("capture exact parked-gate user namespace: %w", errOr(namespaceErr, errors.New("user namespace differs from launcher result"))))
				} else {
					userNamespace = &namespace
				}
			}
			if prepareErr == nil {
				gateIdentity = authority.LinuxCgroupGateIdentity{
					Process: process, HostUID: helper.owner.AUID, ObservedAt: time.Now().UTC(),
					BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: helper.grant.Leaf,
					BornInLeaf: true, TrustedImage: true,
				}
				if err := gateIdentity.Validate(); err != nil {
					prepareErr = fmt.Errorf("validate exact A gate identity: %w", err)
				}
			}
			registerContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorRequestTimeout)
			var registerErr error
			if gateIdentity.Validate() == nil && gateIdentity.Process.Equal(process) && userNamespace != nil {
				registerErr = helper.anchorClient.RegisterGate(registerContext, helper.grant, helper.helper, gateIdentity, *userNamespace, gatePIDFD)
			} else {
				// A failed Prepare may retain an exact A child for emergency stop,
				// but must never establish gate-release transition authority.
				if credentialErr := verifyLinuxOwnerRunChildCredentials(process.PID, "descendant", helper.owner); credentialErr != nil {
					registerErr = fmt.Errorf("failed-Prepare child credentials are unresolved: %w", credentialErr)
				} else if membershipErr := verifyLinuxOwnerRunChildMembership(process.PID, "descendant", helper.grant, helper.leaf); membershipErr != nil {
					registerErr = fmt.Errorf("failed-Prepare child membership is unresolved: %w", membershipErr)
				} else {
					registerErr = helper.anchorClient.RegisterProcess(registerContext, helper.grant, helper.helper, helper.helper, process, "descendant", gatePIDFD)
				}
			}
			cancel()
			if registerErr != nil {
				prepareErr = errors.Join(prepareErr, fmt.Errorf("register exact parked gate with owner anchor: %w", registerErr))
			}
		} else {
			if identityErr == nil {
				identityErr = errors.New("prepared gate omitted its retained pidfd")
			}
			prepareErr = errors.Join(prepareErr, identityErr)
		}
	}
	var observedLauncherExit authority.LinuxCgroupExitProof
	launcherExitProven := false
	if preparedGate != nil {
		observedLauncherExit, launcherExitProven = preparedGate.LauncherExitProof()
	}
	if !launcherExitProven {
		var neverProduced *linuxgate.GateNeverProducedError
		if errors.As(prepareErr, &neverProduced) {
			observedLauncherExit, launcherExitProven = neverProduced.LauncherExitProof()
		}
	}
	if !launcherExitProven {
		var launcherOutcome *linuxgate.LauncherOutcomeError
		if errors.As(prepareErr, &launcherOutcome) {
			observedLauncherExit, launcherExitProven = launcherOutcome.ExitProof()
		}
	}
	if launcherExitProven {
		if launcherIdentity.Validate() == nil && observedLauncherExit.ValidForOwnedDescendant(launcherIdentity, helper.helper, helper.grant.Anchor, nil) {
			launcherExit = &observedLauncherExit
		} else {
			prepareErr = errors.Join(prepareErr, errors.New("protocol launcher exit proof differs from the exact helper-owned launcher"))
		}
	} else if launcherIdentity.Validate() == nil && (preparedGate != nil || isGateNeverProduced(prepareErr)) {
		prepareErr = errors.Join(prepareErr, errors.New("protocol returned no exact launcher exit/reap proof"))
	}
	var gateOutcome *authority.LinuxCgroupGateProductionResult
	var neverProduced *linuxgate.GateNeverProducedError
	if errors.As(prepareErr, &neverProduced) {
		result := neverProduced.Result()
		gateOutcome = &result
	}
	helper.stateMu.Lock()
	if launcherIdentity.Validate() == nil {
		helper.launcher = cloneLinuxOwnerProcessIdentity(&launcherIdentity)
	}
	if launcherExit != nil {
		helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
	}
	if gateOutcome != nil {
		helper.gateOutcome = gateOutcome
	}
	helper.stateMu.Unlock()
	if preparedGate == nil || prepareErr != nil {
		if preparedGate != nil {
			helper.stateMu.Lock()
			helper.prepared = preparedGate
			if gateOutcome != nil {
				helper.gateOutcome = gateOutcome
			}
			if gateIdentity.Validate() == nil {
				helper.gate = cloneLinuxOwnerGateIdentity(&gateIdentity)
				helper.gateUserNamespace = userNamespace
			}
			if launcherExit != nil {
				helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
			}
			helper.stateMu.Unlock()
		}
		result := linuxOwnerHelperV2Response{
			Version: linuxOwnerHelperV2Version, Operation: "prepare-failed", Status: "unresolved",
			RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
			Helper: helper.helper, Launcher: cloneLinuxOwnerProcessIdentity(&launcherIdentity),
			Gate: cloneLinuxOwnerGateIdentityIfValid(gateIdentity), LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit),
			GateProcess: processIdentity, UserNamespace: userNamespace, GateOutcome: gateOutcome,
			ErrorCode: linuxOwnerHelperV2ErrorCode(prepareErr),
		}
		helper.stateMu.Lock()
		helper.prepareResponse = &result
		helper.stateMu.Unlock()
		return helper.sendPrepareResponse(connection, result)
	}
	if processIdentity == nil || launcherExit == nil || userNamespace == nil {
		helper.stateMu.Lock()
		helper.prepared = preparedGate
		helper.stateMu.Unlock()
		return helper.cachePrepareFailure(connection, "producer_identity_unproven", nil)
	}
	helper.stateMu.Lock()
	helper.prepared = preparedGate
	helper.gate = cloneLinuxOwnerGateIdentity(&gateIdentity)
	helper.gateUserNamespace = userNamespace
	helper.launcher = cloneLinuxOwnerProcessIdentity(&launcherIdentity)
	helper.launcherExit = cloneLinuxOwnerAnchorExitProof(launcherExit)
	result := linuxOwnerHelperV2Response{
		Version: linuxOwnerHelperV2Version, Operation: "gate-ready", Status: "ready",
		RunKey: helper.grant.RunKey, Fence: helper.grant.Fence, LaunchToken: helper.grant.LaunchToken,
		Helper: helper.helper, Launcher: cloneLinuxOwnerProcessIdentity(&launcherIdentity),
		Gate: cloneLinuxOwnerGateIdentity(&gateIdentity), LauncherExit: cloneLinuxOwnerAnchorExitProof(launcherExit),
		UserNamespace: userNamespace,
	}
	helper.prepareResponse = &result
	helper.stateMu.Unlock()
	return helper.sendPrepareResponse(connection, result)
}

func (helper *linuxOwnerHelperV2) handleHelperBound(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	if request.Helper == nil || !request.Helper.Equal(helper.helper) || helper.ownerLost || helper.stopRequested || helper.stopAttempted || helper.prepareStarted {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "helper-bound", "rejected", nil, nil, nil, nil, nil, 0, nil, "exact_helper_bind_ack_required")
	}
	if helper.helperBoundAck {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "helper-bound", "already_bound", nil, nil, nil, nil, nil, 0, nil, "")
	}
	helper.helperBoundAck = true
	helper.stateMu.Unlock()
	return helper.writeResponse(connection, "helper-bound", "bound", nil, nil, nil, nil, nil, 0, nil, "")
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

func (helper *linuxOwnerHelperV2) handleLauncherAck(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	// launcher-ack is consumed synchronously by the protocol.Prepare hook.
	return helper.writeResponse(connection, request.Operation, "rejected", nil, nil, nil, nil, nil, 0, nil, "unexpected_ack")
}

func (helper *linuxOwnerHelperV2) handleCommit(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	valid := !helper.ownerLost && !helper.stopRequested && !helper.stopAttempted && helper.prepareResponse != nil &&
		helper.prepared != nil && helper.gate != nil && helper.launcher != nil && helper.launcherExit != nil &&
		request.Gate != nil && request.Gate.Equal(*helper.gate) && request.Launcher != nil && request.Launcher.Equal(*helper.launcher) &&
		request.LauncherExit != nil && request.LauncherExit.Equal(*helper.launcherExit)
	if valid && helper.commitAcknowledged {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "commit", "already_committed", nil, nil, nil, nil, nil, 0, nil, "")
	}
	if valid {
		helper.commitAcknowledged = true
	}
	helper.stateMu.Unlock()
	if !valid {
		return helper.writeResponse(connection, "commit", "rejected", nil, nil, nil, nil, nil, 0, nil, "durable_handoff_ack_mismatch")
	}
	return helper.writeResponse(connection, "commit", "committed", nil, nil, nil, nil, nil, 0, nil, "")
}

func (helper *linuxOwnerHelperV2) handleRenewLease(connection *net.UnixConn, request linuxOwnerHelperV2Request) error {
	helper.stateMu.Lock()
	allowed := helper.commitAcknowledged && !helper.ownerLost && !helper.stopRequested && !helper.stopAttempted &&
		(request.Sequence == 1 || helper.released)
	if !allowed || request.LeaseNanos <= 0 {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "renew-lease", "rejected", nil, nil, nil, nil, nil, 0, nil, "commit_or_lease_state_invalid")
	}
	replay, err := helper.lease.renew(time.Duration(request.LeaseNanos), request.Sequence)
	if err != nil {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "renew-lease", "rejected", nil, nil, nil, nil, nil, 0, nil, "lease_sequence_or_deadline_invalid")
	}
	sequence, _ := helper.lease.snapshot()
	helper.stateMu.Unlock()
	status := "acknowledged"
	if replay {
		status = "already_acknowledged"
	}
	return helper.writeResponse(connection, "renew-lease", status, nil, nil, nil, nil, nil, sequence, nil, "")
}

func (helper *linuxOwnerHelperV2) handleRelease(connection *net.UnixConn, request linuxOwnerHelperV2Request, rights []int) error {
	if len(rights) != 3 || request.Command == nil || request.Command.Clone == nil || request.Helper != nil ||
		request.Launcher != nil || request.Gate != nil || request.LauncherExit != nil || request.Sequence != 0 ||
		request.LeaseNanos != 0 || request.Accepted || request.CheckoutReceipt != nil || request.Receipt != nil ||
		request.PreGateReceipt != nil || request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_or_command_invalid")
	}
	files := [3]*os.File{}
	fileNames := [3]string{"symmetry-owner-stdin-read", "symmetry-owner-stdout-write", "symmetry-owner-stderr-write"}
	for index, fd := range rights {
		duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			closeLinuxOwnerHelperFiles(files[:]...)
			return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_duplicate_failed")
		}
		files[index] = os.NewFile(uintptr(duplicate), fileNames[index])
		if files[index] == nil {
			_ = unix.Close(duplicate)
			closeLinuxOwnerHelperFiles(files[:]...)
			return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "stdio_wrap_failed")
		}
	}
	defer closeLinuxOwnerHelperFiles(files[:]...)
	command := linuxgate.NativeCommand{
		Path: request.Command.Path, Args: append([]string(nil), request.Command.Args...),
		Env: append([]string(nil), request.Command.Env...), Dir: request.Command.Dir,
		Stdin: files[0], Stdout: files[1], Stderr: files[2],
	}
	cloneGrant := request.Command.Clone.Clone()
	if !linuxOwnerHelperV2CloneGrantMatchesRun(&cloneGrant, helper.grant, command.Dir) {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "clone_grant_mismatch")
	}
	command.Clone = &cloneGrant
	if err := command.Validate(); err != nil {
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, 0, nil, "native_command_invalid")
	}
	helper.stateMu.Lock()
	sequence, leaseDeadline, leaseLive := helper.lease.releaseGuard()
	if helper.ownerLost {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "owner_lost")
	}
	if !helper.commitAcknowledged {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "handoff_not_committed")
	}
	if !leaseLive {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "initial_lease_not_acknowledged")
	}
	if helper.stopRequested || helper.stopAttempted || helper.releaseAttempted || helper.prepared == nil || helper.gate == nil {
		helper.stateMu.Unlock()
		return helper.writeResponse(connection, "release", "rejected", nil, nil, nil, nil, nil, sequence, nil, "release_closed_or_used")
	}
	helper.releaseAttempted = true
	helper.releaseInFlight = true
	helper.releaseDone = make(chan struct{})
	releaseDone := helper.releaseDone
	releaseContext, cancelRelease := context.WithTimeout(context.Background(), linuxOwnerHelperV2Timeout)
	helper.releaseCancel = cancelRelease
	prepared := helper.prepared
	gateProcess := helper.gate.Process
	gateIdentity := cloneLinuxOwnerGateIdentity(helper.gate)
	var gateUserNamespace *LinuxUserNamespaceIdentity
	if helper.gateUserNamespace != nil {
		copy := *helper.gateUserNamespace
		gateUserNamespace = &copy
	}
	ownerPipe := helper.ownerPipe
	helper.stateMu.Unlock()
	onCheckoutReady := func(callbackContext context.Context, receipt authority.LinuxCloneCheckoutReceipt) error {
		if !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, cloneGrant) {
			return errors.New("Linux gate checkout receipt differs from the exact clone grant")
		}
		return helper.relayCheckoutReady(callbackContext, connection, request, cloneGrant, receipt)
	}
	var err error
	if gateIdentity == nil || gateUserNamespace == nil {
		err = errors.New("Linux owner helper lacks the exact registered gate/user namespace for release authorization")
	} else {
		err = helper.anchorClient.AuthorizeGateRelease(releaseContext, helper.grant, helper.helper, *gateIdentity, *gateUserNamespace)
	}
	if err == nil {
		err = prepared.Release(releaseContext, int(helper.leaf.leafFD), command, linuxgate.ReleaseGuard{
			OwnerPipe: ownerPipe, LeaseDeadline: leaseDeadline,
		}, onCheckoutReady)
	}
	helper.stateMu.Lock()
	helper.releaseCancel = nil
	witnessAction := ""
	if err == nil {
		helper.released = true
		if helper.witnessHelperAction != "" {
			if helper.witnessHelperAction != linuxOwnerHelperV2WitnessCrashAfterRelease ||
				helper.witnessRunKey != helper.grant.RunKey || !helper.witnessHoldForReleaseFile {
				err = errors.New("Linux owner helper post-RELEASE witness action differs from its exact RunKey contract")
			} else {
				witnessAction = helper.witnessHelperAction
				helper.witnessAction = witnessAction
				helper.witnessActionObserved = true
			}
		}
	}
	if witnessAction != "" {
		// The Gate RELEASE is complete, so an owner-loss stop no longer needs to
		// wait for the native RegisterProcess callback that this witness omits.
		helper.releaseInFlight = false
		close(releaseDone)
	}
	helper.stateMu.Unlock()
	cancelRelease()
	if witnessAction != "" {
		if err := helper.waitForWitnessSelectorUnlink(); err != nil {
			return helper.exitAtWitnessAction(fmt.Errorf("wait for root to unlink the exact post-RELEASE selector: %w", err))
		}
		return helper.exitAtWitnessAction(nil)
	}
	if err == nil {
		registerContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorRequestTimeout)
		registerErr := helper.anchorClient.RegisterProcess(registerContext, helper.grant, helper.helper, helper.helper, gateProcess, "native", int(prepared.PIDFD().Fd()))
		cancel()
		if registerErr != nil {
			err = fmt.Errorf("register same-PID B exec with owner anchor: %w", registerErr)
		} else if _, waitErr := helper.ensureGateWait(); waitErr != nil {
			err = fmt.Errorf("start exact helper-owned gate waiter: %w", waitErr)
		}
	}
	helper.stateMu.Lock()
	helper.releaseInFlight = false
	close(releaseDone)
	sequence, leaseDeadline, leaseLive = helper.lease.releaseGuard()
	ownerLost := helper.ownerLost
	stopRequested := helper.stopRequested || helper.stopAttempted
	helper.stateMu.Unlock()
	if err != nil || ownerLost || stopRequested || !leaseLive {
		ctx, cancel := context.WithTimeout(context.Background(), linuxOwnerHelperV2StopTimeout)
		_, stopErr := helper.stopOwned(ctx)
		cancel()
		code := "release_outcome_unknown"
		if ownerLost {
			code = "owner_lost_during_release"
		} else if !leaseLive {
			code = "lease_expired_during_release"
		}
		responseErr := helper.writeResponse(connection, "release", "unresolved", nil, nil, nil, nil, nil, sequence, nil, code)
		if stopErr != nil {
			helper.requestAnchorTakeover()
		}
		return responseErr
	}
	return helper.writeResponse(connection, "release", "released", nil, nil, nil, nil, nil, sequence, nil, "")
}

func (helper *linuxOwnerHelperV2) waitForWitnessSelectorUnlink() error {
	if helper == nil {
		return errors.New("Linux owner helper witness selector wait is incomplete")
	}
	helper.stateMu.Lock()
	valid := helper.witnessActionObserved && helper.witnessAction == linuxOwnerHelperV2WitnessCrashAfterRelease &&
		helper.witnessRunKey == helper.grant.RunKey && helper.witnessHoldForReleaseFile && helper.released
	helper.stateMu.Unlock()
	if !valid {
		return errors.New("Linux owner helper witness selector wait lacks the exact successful RELEASE action")
	}
	directoryFD, err := openLinuxOwnerHelperWitnessSelectorDirectory()
	if err != nil {
		return fmt.Errorf("open pinned root witness selector directory: %w", err)
	}
	defer unix.Close(directoryFD)
	timer := time.NewTimer(linuxOwnerHelperV2Timeout)
	defer timer.Stop()
	ticker := time.NewTicker(linuxOwnerHelperV2PollInterval)
	defer ticker.Stop()
	selectorName := filepath.Base(linuxOwnerAnchorWitnessConfigPath)
	for {
		var selectorStat unix.Stat_t
		err := unix.Fstatat(directoryFD, selectorName, &selectorStat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect pinned root witness selector without following links: %w", err)
		}
		helper.stateMu.Lock()
		ownerLost := helper.ownerLost
		helper.stateMu.Unlock()
		if ownerLost {
			return ErrLinuxOwnerHelperV2OwnerLost
		}
		if _, _, leaseLive := helper.lease.releaseGuard(); !leaseLive {
			return ErrLinuxOwnerHelperV2LeaseUnarmed
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return errors.New("root did not unlink the selected Linux owner witness selector before the bounded wait expired")
		}
	}
}

func openLinuxOwnerHelperWitnessSelectorDirectory() (int, error) {
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil || rootStat.Uid != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Mode&0022 != 0 {
		return -1, fmt.Errorf("verify filesystem root ownership and mode: %w", errOr(err, errors.New("root is not root-owned and non-writable by group/world")))
	}
	etcFD, err := unix.Openat2(rootFD, "etc", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return -1, fmt.Errorf("open root-controlled /etc directory: %w", err)
	}
	defer unix.Close(etcFD)
	var etcStat unix.Stat_t
	if err := unix.Fstat(etcFD, &etcStat); err != nil || etcStat.Uid != 0 || etcStat.Mode&unix.S_IFMT != unix.S_IFDIR || etcStat.Mode&0022 != 0 {
		return -1, fmt.Errorf("verify /etc ownership and mode: %w", errOr(err, errors.New("/etc is not root-owned and non-writable by group/world")))
	}
	directoryFD, err := unix.Openat2(etcFD, "symmetry", &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return -1, fmt.Errorf("open root-controlled /etc/symmetry directory: %w", err)
	}
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil || directoryStat.Uid != 0 ||
		directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR || directoryStat.Mode&0022 != 0 {
		_ = unix.Close(directoryFD)
		return -1, fmt.Errorf("verify /etc/symmetry ownership and mode: %w", errOr(err, errors.New("directory is not root-owned and non-writable by group/world")))
	}
	return directoryFD, nil
}

func (helper *linuxOwnerHelperV2) exitAtWitnessAction(cause error) error {
	if helper == nil {
		return errors.Join(errLinuxOwnerHelperV2WitnessExit, cause)
	}
	helper.stateMu.Lock()
	valid := helper.witnessActionObserved && helper.witnessAction == linuxOwnerHelperV2WitnessCrashAfterRelease &&
		helper.witnessRunKey == helper.grant.RunKey && helper.released
	if valid {
		helper.witnessExitRequested = true
		helper.witnessExitCause = cause
	}
	helper.stateMu.Unlock()
	if !valid {
		return errors.Join(cause, errors.New("Linux owner helper witness exit lacks the exact observed action"))
	}
	if helper.listener == nil {
		helper.stateMu.Lock()
		helper.witnessExitCause = errors.Join(helper.witnessExitCause, errors.New("Linux owner helper witness exit has no control listener"))
		helper.stateMu.Unlock()
		return helper.witnessExitError()
	}
	if err := helper.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		helper.stateMu.Lock()
		helper.witnessExitCause = errors.Join(helper.witnessExitCause, fmt.Errorf("close helper listener at witness exit: %w", err))
		helper.stateMu.Unlock()
		helper.requestAnchorTakeover()
		return helper.witnessExitError()
	}
	return helper.witnessExitError()
}

func (helper *linuxOwnerHelperV2) relayCheckoutReady(
	ctx context.Context,
	connection *net.UnixConn,
	initial linuxOwnerHelperV2Request,
	cloneGrant authority.LinuxCloneGrant,
	receipt authority.LinuxCloneCheckoutReceipt,
) error {
	if helper == nil || ctx == nil || connection == nil || !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, cloneGrant) {
		return errors.New("Linux owner helper checkout-ready relay is incomplete")
	}
	deadline := time.Now().Add(linuxOwnerHelperV2Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set checkout-ready callback deadline: %w", err)
	}
	response := helper.baseResponse("checkout-ready", "ready")
	copyReceipt := receipt.Clone()
	response.CheckoutReceipt = &copyReceipt
	helper.stateMu.Lock()
	response.OwnerLost = helper.ownerLost
	response.CommitAcknowledged = helper.commitAcknowledged
	response.ReleaseAttempted = helper.releaseAttempted
	response.Released = helper.released
	helper.stateMu.Unlock()
	if err := writeLinuxOwnerHelperV2Response(connection, response); err != nil {
		return fmt.Errorf("send exact B checkout receipt to durable owner: %w", err)
	}
	request, rights, err := readLinuxOwnerHelperV2Request(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return fmt.Errorf("read checkout-ready acknowledgement: %w", err)
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, helper.owner)
	if err != nil || peer.PID != initial.ClientPID || peer.Identity != initial.ClientIdentity ||
		!helper.requestMatchesPeer(request, peer) || request.Operation != "checkout-ready-ack" || len(rights) != 0 ||
		!request.Accepted || request.CheckoutReceipt == nil || !request.CheckoutReceipt.Equal(receipt) ||
		request.Helper != nil || request.Launcher != nil || request.Gate != nil || request.LauncherExit != nil ||
		request.Sequence != 0 || request.LeaseNanos != 0 || request.Command != nil || request.Receipt != nil ||
		request.PreGateReceipt != nil || request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil {
		return errors.Join(errors.New("checkout-ready acknowledgement is absent, rejected, or mismatched"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	helper.stateMu.Lock()
	ownerLost := helper.ownerLost || helper.stopRequested || helper.stopAttempted
	_, _, leaseLive := helper.lease.releaseGuard()
	helper.stateMu.Unlock()
	if ownerLost || !leaseLive {
		return errors.New("owner loss or lease expiry fenced the clone checkout acknowledgement")
	}
	return nil
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

func (helper *linuxOwnerHelperV2) readRunLeafProcessIDs(ctx context.Context) (map[int]struct{}, error) {
	if helper == nil || ctx == nil || helper.leaf == nil || helper.grant.Validate() != nil ||
		helper.owner.Validate() != nil || helper.owner.AUID != helper.grant.OwnerUID ||
		!helper.leaf.Identity().Equal(helper.grant.Leaf) {
		return nil, errors.New("Linux owner helper leaf membership scan is incomplete")
	}
	pids, err := readLinuxOwnerAnchorRunLeafProcessIDs(ctx, helper.leaf, helper.grant, helper.owner.AUID)
	if err != nil {
		return nil, fmt.Errorf("read exact run-leaf process set for helper membership audit: %w", err)
	}
	if err := helper.leaf.verifyLive(); err != nil {
		return nil, fmt.Errorf("revalidate pinned run leaf after membership scan: %w", err)
	}
	return pids, nil
}

type linuxOwnerHelperV2RunProcessSnapshot struct {
	process     authority.LinuxCgroupProcessIdentity
	credentials linuxOwnerProcessCredentials
}

func linuxOwnerHelperV2RunTreeOrder(
	helperPID int,
	processes map[int]linuxOwnerHelperV2RunProcessSnapshot,
) ([]int, error) {
	if helperPID <= 1 {
		return nil, errors.New("run process tree lacks an exact helper PID")
	}
	if _, helperInLeaf := processes[helperPID]; helperInLeaf {
		return nil, errors.New("helper process unexpectedly appears in its reserved run leaf")
	}
	childrenByParent := make(map[int][]int, len(processes))
	for pid, snapshot := range processes {
		if pid <= 1 || snapshot.process.PID != pid || snapshot.process.Validate() != nil || snapshot.credentials.Parent <= 1 {
			return nil, errors.New("run leaf contains an invalid exact process snapshot")
		}
		if snapshot.credentials.Parent != helperPID {
			if _, parentInLeaf := processes[snapshot.credentials.Parent]; !parentInLeaf {
				return nil, errors.New("run leaf process parent is neither helper nor a pinned run-leaf member")
			}
		}
		childrenByParent[snapshot.credentials.Parent] = append(childrenByParent[snapshot.credentials.Parent], pid)
	}
	for _, children := range childrenByParent {
		sort.Ints(children)
	}
	seen := map[int]struct{}{helperPID: {}}
	order := make([]int, 1, len(processes)+1)
	order[0] = helperPID
	for cursor := 0; cursor < len(order); cursor++ {
		for _, pid := range childrenByParent[order[cursor]] {
			if _, duplicate := seen[pid]; duplicate {
				return nil, errors.New("run leaf process tree contains a duplicate or cyclic PID")
			}
			seen[pid] = struct{}{}
			order = append(order, pid)
		}
	}
	if len(order)-1 != len(processes) {
		return nil, errors.New("run leaf contains a process outside the exact helper-owned descendant tree")
	}
	return order[1:], nil
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
				return fmt.Errorf("verify exact live gate identity before anchor stop: %w", errOr(err, errors.New("creation identity differs")))
			}
			if err := prepared.VerifyLeafMembership(int(helper.leaf.leafFD)); err != nil {
				return fmt.Errorf("verify exact live gate remains in its prepared leaf: %w", err)
			}
		}
	}
	if untrustedProcess != nil {
		preparedIdentity, err := linuxOwnerHelperUntrustedProcessIdentity(prepared.Identity(), prepared.PIDFD(), helper.helper, launcher)
		if err != nil || !preparedIdentity.Equal(*untrustedProcess) {
			return fmt.Errorf("verify exact failed-Prepare child PIDFD identity: %w", errOr(err, errors.New("identity differs")))
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
		return fmt.Errorf("verify exact helper identity before StopLeaf: %w", errOr(err, errors.New("creation identity differs")))
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
				return fmt.Errorf("verify exact process-tree parent identity before StopLeaf: %w", errOr(err, errors.New("creation identity differs")))
			}
			verifiedParents[parentPID] = struct{}{}
		}
		process := snapshot.process
		identity, err := ProcessIdentity(pid)
		if err != nil || identity != process.Identity {
			return fmt.Errorf("capture exact recursive run-process identity: %w", errOr(err, errors.New("creation identity changed")))
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
			return fmt.Errorf("revalidate exact recursive process identity before StopLeaf: %w", errOr(err, errors.New("creation identity changed")))
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
				return 0, "", fmt.Errorf("verify exact adopted child parent: %w", errOr(err, errors.New("parent differs from helper")))
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
				return 0, "", fmt.Errorf("revalidate adopted child identity after origin proof: %w", errOr(err, errors.New("creation identity changed")))
			}
			credentials, err = readLinuxOwnerProcessCredentials(pid)
			if err != nil || credentials.Parent != os.Getpid() {
				_ = pidfdFile.Close()
				return 0, "", fmt.Errorf("revalidate adopted child parent after origin proof: %w", errOr(err, errors.New("parent differs from helper")))
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

func linuxOwnerHelperV2PreGateReceiptMatches(receipt authority.LinuxCgroupPreGateStopReceipt, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, evidence LinuxOwnerHelperV2PreGateStopEvidence) bool {
	return receipt.Validate() == nil && evidence.ValidateFor(grant, helper) == nil && evidence.Proof != nil &&
		receipt.Version == authority.LinuxCgroupIdentityVersion && receipt.LaunchToken == grant.LaunchToken &&
		receipt.CgroupIdentity.Equal(grant.Leaf) && receipt.OwnerUID == grant.OwnerUID && receipt.LauncherUID == grant.LauncherUID &&
		receipt.Anchor.Equal(grant.Anchor) && receipt.Helper.Equal(helper) && evidence.Launcher != nil &&
		receipt.Launcher.Equal(*evidence.Launcher) && receipt.Proof.Equal(*evidence.Proof)
}

func linuxOwnerHelperV2ReceiptMatches(receipt authority.StopReceipt, grant LinuxOwnerAnchorGrant, helper, launcher authority.LinuxCgroupProcessIdentity, gate authority.LinuxCgroupGateIdentity, proof authority.LinuxCgroupStopProof) bool {
	// StopReceipt intentionally has no launch-token field; the enclosing IPC
	// request is accepted only after requestMatchesPeer compares the complete
	// RunKey/fence/token/secret grant. Match every receipt identity it does carry.
	return receipt.Version == authority.SupervisorVersionV2 && receipt.Status == "stopped" && receipt.ActiveProcesses == 0 &&
		grant.Validate() == nil && receipt.TargetPID == gate.Process.PID && receipt.TargetIdentity == gate.Process.Identity &&
		receipt.SupervisorPID == helper.PID && receipt.SupervisorIdentity == helper.Identity &&
		receipt.OwnerUID == grant.OwnerUID && receipt.LauncherUID == grant.LauncherUID &&
		receipt.PipeToken == grant.PipeToken && receipt.JobID == grant.JobID &&
		receipt.TargetBirthKind == authority.LinuxCgroupTargetBirthTrustedAGate && receipt.CgroupIdentity != nil &&
		receipt.CgroupIdentity.Equal(grant.Leaf) && receipt.LinuxProof != nil && receipt.LinuxProof.Equal(proof) &&
		proof.LauncherExit.Process.Equal(launcher)
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

func linuxOwnerHelperV2ReleaseProofMatches(proof authority.SupervisorHandoffReleaseProof, grant LinuxOwnerAnchorGrant, helper, target, launcher authority.LinuxCgroupProcessIdentity, launcherExit authority.LinuxCgroupExitProof) bool {
	return proof.Validate() == nil && proof.Version == authority.SupervisorHandoffVersionV2 &&
		proof.Status == "released" && proof.LaunchToken == grant.LaunchToken &&
		proof.TargetPID == target.PID && proof.TargetIdentity == target.Identity &&
		proof.SupervisorPID == helper.PID && proof.SupervisorIdentity == helper.Identity &&
		proof.OwnerUID == grant.OwnerUID && proof.LauncherUID == grant.LauncherUID &&
		proof.PipeToken == grant.PipeToken && proof.JobID == grant.JobID &&
		proof.CgroupIdentity != nil && proof.CgroupIdentity.Equal(grant.Leaf) &&
		proof.AnchorPID == grant.Anchor.PID && proof.AnchorIdentity == grant.Anchor.Identity &&
		proof.TargetBirthKind == authority.LinuxCgroupTargetBirthTrustedAGate &&
		proof.LauncherPID == launcher.PID && proof.LauncherIdentity == launcher.Identity &&
		proof.LauncherExit != nil && proof.LauncherExit.Equal(launcherExit) &&
		proof.LauncherExit.ValidForOwnedDescendant(launcher, helper, grant.Anchor, nil)
}

func (helper *linuxOwnerHelperV2) setStopError(code string) {
	helper.stateMu.Lock()
	helper.stopErrorCode = code
	helper.stateMu.Unlock()
}

func (helper *linuxOwnerHelperV2) isOwnerLost() bool {
	helper.stateMu.Lock()
	defer helper.stateMu.Unlock()
	return helper.ownerLost
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

// linuxOwnerSiginfoStatus reads si_status from the Linux amd64 siginfo_t
// union written by waitid(P_PIDFD). x/sys exposes the fixed header as fields
// but intentionally hides the union payload; the offset is the Linux UAPI
// SIGCHLD layout (si_pid, si_uid, si_status).
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

func linuxOwnerHelperGateProcessIdentity(identity linuxgate.ProcessIdentity) (authority.LinuxCgroupProcessIdentity, error) {
	if identity.PID <= 0 || identity.StartTime == 0 || identity.BootID == "" || identity.UserNSInode == 0 || identity.OwnerPID <= 0 {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("Linux parked-gate launch identity is incomplete")
	}
	actual, err := ProcessIdentity(identity.PID)
	if err != nil {
		return authority.LinuxCgroupProcessIdentity{}, err
	}
	if identity.BootID != readLinuxCgroupBootID() {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("Linux parked gate boot identity changed")
	}
	return authority.LinuxCgroupProcessIdentity{PID: identity.PID, Identity: actual}, nil
}

func linuxOwnerHelperUntrustedProcessIdentity(
	identity linuxgate.ProcessIdentity,
	pidfd *os.File,
	helper authority.LinuxCgroupProcessIdentity,
	launcher *authority.LinuxCgroupProcessIdentity,
) (authority.LinuxCgroupProcessIdentity, error) {
	if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || identity.OwnerPID <= 1 ||
		pidfd == nil || helper.Validate() != nil || launcher == nil || launcher.Validate() != nil || identity.OwnerPID != launcher.PID {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("failed-Prepare child identity is incomplete or not launcher-owned")
	}
	actual, err := ProcessIdentity(identity.PID)
	if err != nil {
		return authority.LinuxCgroupProcessIdentity{}, err
	}
	parsed, err := parseLinuxProcessIdentity(actual)
	if err != nil || parsed.version != linuxProcessIdentityVersion || parsed.pid != identity.PID ||
		parsed.bootID != identity.BootID || parsed.startTime != identity.StartTime {
		return authority.LinuxCgroupProcessIdentity{}, errors.Join(errors.New("failed-Prepare child PID/start identity differs from its retained gate pidfd"), err)
	}
	process := authority.LinuxCgroupProcessIdentity{PID: identity.PID, Identity: actual}
	if err := verifyLinuxOwnerAnchorPIDFD(int(pidfd.Fd()), process); err != nil {
		return authority.LinuxCgroupProcessIdentity{}, fmt.Errorf("verify failed-Prepare child pidfd identity: %w", err)
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != helper.PID {
		return authority.LinuxCgroupProcessIdentity{}, fmt.Errorf("verify failed-Prepare child helper ancestry: %w", errOr(err, errors.New("parent differs")))
	}
	return process, nil
}

func isGateNeverProduced(err error) bool {
	var failure *linuxgate.GateNeverProducedError
	return errors.As(err, &failure)
}

func linuxOwnerHelperV2ErrorCode(err error) string {
	if err == nil {
		return "unknown"
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, linuxgate.ErrOutcomeUnknown):
		return "launch_outcome_unknown"
	case errors.Is(err, linuxgate.ErrGateNeverProduced):
		return "gate_never_produced"
	case errors.Is(err, ErrLinuxCgroupV2Unresolved):
		return "cgroup_unresolved"
	default:
		return "operation_failed"
	}
}

func cloneLinuxOwnerProcessIdentity(value *authority.LinuxCgroupProcessIdentity) *authority.LinuxCgroupProcessIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneLinuxOwnerGateIdentity(value *authority.LinuxCgroupGateIdentity) *authority.LinuxCgroupGateIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneLinuxOwnerGateIdentityIfValid(value authority.LinuxCgroupGateIdentity) *authority.LinuxCgroupGateIdentity {
	if value.Validate() != nil {
		return nil
	}
	return cloneLinuxOwnerGateIdentity(&value)
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

func (client *LinuxOwnerAnchorClient) RetireHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, proof authority.SupervisorHandoffReleaseProof) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux owner anchor retirement is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetire,
		Grant: grant, Helper: &helper, ReleaseProof: &proof,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationRetire) || (response.Status != "retired" && response.Status != "already_retired") || len(rights) != 0 {
		return errors.New("Linux owner anchor retained the helper or leaf after retirement request")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetirePreGateHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, proof authority.LinuxCgroupPreGateReleaseProof) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux owner anchor pre-gate retirement is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetire,
		Grant: grant, Helper: &helper, PreGateReleaseProof: &proof,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationRetire) || (response.Status != "retired" && response.Status != "already_retired") || len(rights) != 0 {
		return errors.New("Linux owner anchor retained pre-gate helper pins after retirement")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetireClearedRun(ctx context.Context, key LinuxOwnerAnchorRunKey, fence protocol.Fence, helper authority.LinuxCgroupProcessIdentity, release LinuxCgroupV2Release) error {
	normalProof, normalIssued := release.Snapshot()
	preGateProof, preGateIssued := release.SnapshotPreGate()
	if normalIssued == preGateIssued {
		return errors.New("Linux cgroup release capability is absent or ambiguous")
	}
	if normalIssued {
		return client.retireClearedRunWithProofs(ctx, key, fence, helper, &normalProof, nil)
	}
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, nil, &preGateProof)
}

// RetireClearedRunWithProof is the restart-safe path for the exact durable
// post-clear normal release tombstone. It does not manufacture an opaque
// release capability; the anchor independently verifies its retained pins.
func (client *LinuxOwnerAnchorClient) RetireClearedRunWithProof(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	proof authority.SupervisorHandoffReleaseProof,
) error {
	if proof.Validate() != nil || proof.CgroupIdentity == nil {
		return errors.New("Linux cleared-run normal release proof is incomplete")
	}
	proofCopy := proof.Clone()
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, &proofCopy, nil)
}

// RetirePreGateClearedRunWithProof is the restart-safe path for the exact
// durable post-clear PID-zero release tombstone.
func (client *LinuxOwnerAnchorClient) RetirePreGateClearedRunWithProof(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	proof authority.LinuxCgroupPreGateReleaseProof,
) error {
	if proof.Validate() != nil {
		return errors.New("Linux cleared-run pre-gate release proof is incomplete")
	}
	proofCopy := proof.Clone()
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, nil, &proofCopy)
}

func (client *LinuxOwnerAnchorClient) retireClearedRunWithProofs(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	normalProof *authority.SupervisorHandoffReleaseProof,
	preGateProof *authority.LinuxCgroupPreGateReleaseProof,
) error {
	if client == nil || ctx == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 ||
		fence.Generation != key.Generation || helper.Validate() != nil || (normalProof == nil) == (preGateProof == nil) {
		return errors.New("Linux cleared-run retirement identity is incomplete")
	}
	var anchor authority.LinuxCgroupProcessIdentity
	var parent authority.LinuxCgroupParentIdentity
	var ownerUID uint32
	if normalProof != nil {
		if normalProof.Validate() != nil || normalProof.CgroupIdentity == nil {
			return errors.New("normal Linux release capability lacks its exact cgroup receipt")
		}
		anchor = authority.LinuxCgroupProcessIdentity{PID: normalProof.AnchorPID, Identity: normalProof.AnchorIdentity}
		parent = normalProof.CgroupIdentity.Parent
		ownerUID = normalProof.OwnerUID
	} else {
		if preGateProof.Validate() != nil {
			return errors.New("pre-gate Linux release capability lacks its exact reservation receipt")
		}
		anchor = preGateProof.Receipt.Anchor
		parent = preGateProof.Receipt.CgroupIdentity.Parent
		ownerUID = preGateProof.Receipt.OwnerUID
	}
	if anchor.Validate() != nil || parent.Validate() != nil || ownerUID != client.owner.AUID {
		return errors.New("Linux release capability differs from the fixed anchor owner")
	}
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorWaitTimeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	defer stopClose()
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, anchor); err != nil {
		return fmt.Errorf("authenticate anchor for cleared-run retirement: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetireCleared,
		ClientPID: os.Getpid(), ClientIdentity: clientIdentity,
		RunKey: key, Fence: fence, Helper: &helper,
	}
	if normalProof != nil {
		request.ReleaseProof = normalProof
	} else {
		request.PreGateReleaseProof = preGateProof
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return errors.Join(errors.New("encode bounded cleared-run retirement request"), err)
	}
	if written, _, err := connection.WriteMsgUnix(encoded, nil, nil); err != nil || written != len(encoded) {
		return fmt.Errorf("send cleared-run retirement request: %w", errOr(err, io.ErrShortWrite))
	}
	response, rights, err := readLinuxOwnerAnchorResponse(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != linuxOwnerAnchorOperationRetireCleared ||
		(response.Status != "retired" && response.Status != "already_retired") || response.RunKey != key || response.Fence != fence ||
		!response.Anchor.Equal(anchor) || !response.Parent.Equal(parent) || response.OwnerUID != ownerUID || response.ManagerPath != parent.Path ||
		response.Helper == nil || !response.Helper.Equal(helper) {
		return errors.New("anchor retirement response differs from the exact cleared RunKey/fence and release capability")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetireAbortedHelper(ctx context.Context, key LinuxOwnerAnchorRunKey, fence protocol.Fence, helper authority.LinuxCgroupProcessIdentity, proof authority.LinuxCgroupReservationAbortProof) error {
	if client == nil || ctx == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 ||
		fence.Generation != key.Generation || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux aborted-helper retirement is incomplete")
	}
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorWaitTimeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	defer stopClose()
	anchor := proof.Anchor
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, anchor); err != nil {
		return fmt.Errorf("authenticate anchor for aborted-helper retirement: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetireAborted,
		ClientPID: os.Getpid(), ClientIdentity: clientIdentity,
		RunKey: key, Fence: fence, Helper: &helper, AbortProof: &proof,
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return errors.Join(errors.New("encode bounded aborted-helper retirement request"), err)
	}
	if written, _, err := connection.WriteMsgUnix(encoded, nil, nil); err != nil || written != len(encoded) {
		return fmt.Errorf("send aborted-helper retirement request: %w", errOr(err, io.ErrShortWrite))
	}
	response, rights, err := readLinuxOwnerAnchorResponse(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != linuxOwnerAnchorOperationRetireAborted ||
		(response.Status != "retired" && response.Status != "already_retired") || response.RunKey != key || response.Fence != fence ||
		!response.Anchor.Equal(anchor) || response.Helper == nil || !response.Helper.Equal(helper) {
		return errors.New("anchor did not retire the exact aborted helper proof")
	}
	return nil
}

func (anchor *linuxOwnerAnchor) handleRetireHelper(connection *net.UnixConn, request linuxOwnerAnchorRequest, rights []int) {
	if len(rights) != 0 || request.Helper == nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
		(request.ReleaseProof == nil) == (request.PreGateReleaseProof == nil) {
		return
	}
	run, exists := anchor.runs[request.Grant.RunKey]
	if !exists || run == nil || !run.grant.Equal(request.Grant) || !run.helper.Equal(*request.Helper) {
		anchor.writeResponse(connection, request, "run_unavailable", nil, nil)
		return
	}
	if run.retired {
		if request.ReleaseProof != nil && run.retiredRelease != nil && run.retiredRelease.Equal(*request.ReleaseProof) ||
			request.PreGateReleaseProof != nil && run.retiredPreGateRelease != nil && run.retiredPreGateRelease.Equal(*request.PreGateReleaseProof) {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	if request.PreGateReleaseProof != nil {
		proof := request.PreGateReleaseProof
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		if proof.Validate() != nil || proof.Receipt.LaunchToken != run.grant.LaunchToken ||
			!proof.Receipt.CgroupIdentity.Equal(run.grant.Leaf) || !proof.Receipt.Anchor.Equal(run.grant.Anchor) ||
			!proof.Receipt.Helper.Equal(run.helper) || launcher.Validate() != nil ||
			!proof.Receipt.Launcher.Equal(launcher) || !proof.Receipt.Proof.LauncherExit.Process.Equal(launcher) ||
			!proof.Receipt.Proof.LauncherExit.ValidForOwnedDescendant(launcher, run.helper, run.grant.Anchor, nil) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", nil, nil)
			return
		}
	} else {
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		target := linuxOwnerAnchorTrackedIdentity(run.children, "native")
		if target.Validate() != nil {
			target = linuxOwnerAnchorTrackedIdentity(run.children, "gate")
		}
		if request.ReleaseProof.LauncherExit == nil || !linuxOwnerHelperV2ReleaseProofMatches(*request.ReleaseProof, run.grant, run.helper,
			target, launcher, *request.ReleaseProof.LauncherExit) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", nil, nil)
			return
		}
	}
	present, leafErr := linuxOwnerAnchorPinnedLeafPresent(run)
	if leafErr != nil || present {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	retireContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := run.waitHelper(retireContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if err := anchor.verifyRetiredRunPhysicalState(retireContext, run); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	if request.ReleaseProof != nil {
		proof := request.ReleaseProof.Clone()
		run.retiredRelease = &proof
	} else {
		proof := request.PreGateReleaseProof.Clone()
		run.retiredPreGateRelease = &proof
	}
	anchor.writeResponse(connection, request, "retired", nil, nil)
}

func (anchor *linuxOwnerAnchor) handleRetireClearedRun(connection *net.UnixConn, request linuxOwnerAnchorRequest) {
	run, exists := anchor.runs[request.RunKey]
	if !exists || run == nil || run.grant.Fence != request.Fence || !run.helper.Equal(*request.Helper) {
		return
	}
	request.Grant = run.grant
	if run.retired {
		exact := request.ReleaseProof != nil && run.retiredRelease != nil && run.retiredRelease.Equal(*request.ReleaseProof) ||
			request.PreGateReleaseProof != nil && run.retiredPreGateRelease != nil && run.retiredPreGateRelease.Equal(*request.PreGateReleaseProof)
		if exact {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	if request.ReleaseProof != nil {
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		target := linuxOwnerAnchorTrackedIdentity(run.children, "native")
		if target.Validate() != nil {
			target = linuxOwnerAnchorTrackedIdentity(run.children, "gate")
		}
		if request.ReleaseProof.LauncherExit == nil || !linuxOwnerHelperV2ReleaseProofMatches(*request.ReleaseProof,
			run.grant, run.helper, target, launcher, *request.ReleaseProof.LauncherExit) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", &run.helper, nil)
			return
		}
	} else {
		proof := request.PreGateReleaseProof
		launcher := linuxOwnerAnchorTrackedIdentity(run.children, "launcher")
		if proof == nil || proof.Validate() != nil || proof.Receipt.LaunchToken != run.grant.LaunchToken ||
			!proof.Receipt.CgroupIdentity.Equal(run.grant.Leaf) || !proof.Receipt.Anchor.Equal(run.grant.Anchor) ||
			!proof.Receipt.Helper.Equal(run.helper) || launcher.Validate() != nil ||
			!proof.Receipt.Launcher.Equal(launcher) || !proof.Receipt.Proof.LauncherExit.Process.Equal(launcher) ||
			!proof.Receipt.Proof.LauncherExit.ValidForOwnedDescendant(launcher, run.helper, run.grant.Anchor, nil) {
			anchor.writeResponse(connection, request, "release_proof_mismatch", &run.helper, nil)
			return
		}
	}
	present, leafErr := linuxOwnerAnchorPinnedLeafPresent(run)
	if leafErr != nil || present {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	retireContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), run.helper); err == nil {
			anchorClient := &LinuxOwnerAnchorClient{socketPath: anchor.socketPath, owner: anchor.owner}
			control, dialErr := dialLinuxOwnerHelperV2(retireContext, anchorClient, run.grant, run.helper)
			if dialErr != nil {
				anchor.writeResponse(connection, request, "helper_release_unresolved", &run.helper, nil)
				return
			}
			if request.ReleaseProof != nil {
				dialErr = control.ReleaseAfterClear(retireContext, *request.ReleaseProof)
			} else {
				dialErr = control.ReleaseAfterPreGateClear(retireContext, *request.PreGateReleaseProof)
			}
			_ = control.Close()
			if dialErr != nil {
				anchor.writeResponse(connection, request, "helper_release_unresolved", &run.helper, nil)
				return
			}
		}
		if err := run.waitHelper(retireContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if err := anchor.verifyRetiredRunPhysicalState(retireContext, run); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "release_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	if request.ReleaseProof != nil {
		proof := request.ReleaseProof.Clone()
		run.retiredRelease = &proof
	} else {
		proof := request.PreGateReleaseProof.Clone()
		run.retiredPreGateRelease = &proof
	}
	anchor.writeResponse(connection, request, "retired", &run.helper, nil)
}

func (anchor *linuxOwnerAnchor) handleRetireAbortedHelper(connection *net.UnixConn, request linuxOwnerAnchorRequest) {
	run, exists := anchor.runs[request.RunKey]
	if !exists || run == nil || run.grant.Fence != request.Fence || !run.helper.Equal(*request.Helper) {
		return
	}
	request.Grant = run.grant
	proof := request.AbortProof
	if run.retired {
		if proof != nil && run.retiredAbort != nil && run.retiredAbort.Equal(*proof) {
			anchor.writeResponse(connection, request, "already_retired", &run.helper, nil)
		} else {
			anchor.writeResponse(connection, request, "retirement_conflict", &run.helper, nil)
		}
		return
	}
	helperIdentity := run.helper
	helperPointer := &helperIdentity
	if proof == nil || proof.Validate() != nil || proof.LaunchToken != run.grant.LaunchToken ||
		!proof.Parent.Equal(run.grant.Parent) || proof.LeafName != run.grant.Leaf.LeafName ||
		!proof.CreateStarted || proof.CreateStarted != run.grant.CreateStarted || proof.LeafIdentity == nil ||
		!proof.LeafIdentity.Equal(run.grant.Leaf) ||
		(proof.LeafDisposition != authority.LinuxCgroupAbortLeafRemovedEmpty &&
			proof.LeafDisposition != authority.LinuxCgroupAbortLeafAbsentAfterRmdir) || proof.OwnerUID != run.grant.OwnerUID ||
		!proof.Anchor.Equal(run.grant.Anchor) || !proof.ValidFor(run.grant.LaunchToken, run.grant.Parent,
		run.grant.Leaf.LeafName, run.grant.CreateStarted, run.grant.OwnerUID, run.grant.Anchor, proof.Mirror, helperPointer, nil) ||
		proof.Launcher != nil || proof.Helper == nil || !proof.Helper.Equal(run.helper) || proof.HelperExit == nil ||
		!proof.HelperExit.ValidFor(run.helper, run.grant.Anchor) {
		anchor.writeResponse(connection, request, "abort_proof_mismatch", &run.helper, nil)
		return
	}
	for _, child := range run.children {
		if child.role == "launcher" || child.role == "gate" || child.role == "native" {
			anchor.writeResponse(connection, request, "abort_has_producer", &run.helper, nil)
			return
		}
	}
	waitContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
	defer cancel()
	if run.helperExit == nil {
		if err := run.waitHelper(waitContext); err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", &run.helper, nil)
			return
		}
	}
	if !run.helperExit.Equal(*proof.HelperExit) {
		anchor.writeResponse(connection, request, "helper_exit_mismatch", &run.helper, nil)
		return
	}
	if err := anchor.verifyRetiredRunPhysicalState(waitContext, run); err != nil {
		anchor.writeResponse(connection, request, "abort_unproven", &run.helper, nil)
		return
	}
	if err := run.close(); err != nil {
		anchor.writeResponse(connection, request, "abort_unproven", &run.helper, nil)
		return
	}
	run.retired = true
	proofCopy := proof.Clone()
	run.retiredAbort = &proofCopy
	anchor.writeResponse(connection, request, "retired", &run.helper, nil)
}

func linuxOwnerAnchorTrackedIdentity(children map[int]linuxOwnerAnchorTrackedProcess, role string) authority.LinuxCgroupProcessIdentity {
	for _, child := range children {
		if child.role == role {
			return child.identity
		}
	}
	return authority.LinuxCgroupProcessIdentity{}
}

func (anchor *linuxOwnerAnchor) verifyRetiredRunPhysicalState(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || run.leaf == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return errors.New("anchor retirement lacks exact helper exit/reap and pinned leaf authority")
	}
	if run.stopEvidence != nil {
		if err := verifyLinuxOwnerAnchorStopEvidenceNamespacePin(*run.stopEvidence, run.gateUserNamespaceFD); err != nil {
			return fmt.Errorf("verify cached AnchorStop namespace pin before retirement: %w", err)
		}
	}
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return fmt.Errorf("verify exact pinned leaf before retirement: %w", err)
	}
	if present {
		return errors.New("exact pinned run leaf remains present despite the release or abort proof")
	}
	run.leafRemovedObserved = true
	run.leafEmptyObserved = true
	if _, err := anchor.reapRunChildren(ctx, run); err != nil {
		return fmt.Errorf("reap every exact anchor-adopted run child before retirement: %w", err)
	}
	if len(run.physicalOnlyChildren) != 0 {
		return errors.New("physical-only pre-RegisterGate gate remains outside RunReapEvidence; run cannot be retired or cleared")
	}
	return nil
}

func (anchor *linuxOwnerAnchor) reapRunChildren(ctx context.Context, run *linuxOwnerAnchorRun) ([]LinuxOwnerAnchorReapEvidence, error) {
	if anchor == nil || ctx == nil || run == nil || run.helperExit == nil || run.leaf == nil ||
		!run.helperExit.ValidFor(run.helper, anchor.identity) {
		return nil, errors.New("anchor run-wide reap is not fenced by exact helper death and leaf pins")
	}
	wasRemoved := run.leafRemovedObserved
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return nil, fmt.Errorf("verify exact pinned leaf before anchor reap: %w", err)
	}
	if present {
		if wasRemoved {
			return nil, errors.New("exact pinned run leaf reappeared after removal was observed")
		}
		if err := run.leaf.WaitEmpty(ctx); err != nil {
			return nil, fmt.Errorf("observe exact reserved leaf empty before anchor reap: %w", err)
		}
		run.leafEmptyObserved = true
	} else {
		run.leafRemovedObserved = true
		run.leafEmptyObserved = true
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pids, err := readLinuxOwnerProcessChildren(ctx, anchor.identity)
		if err != nil {
			return nil, fmt.Errorf("read anchor's exact kernel child list: %w", err)
		}
		var foundRunChild bool
		for _, pid := range pids {
			tracked, known := run.children[pid]
			if known && (tracked.identity.PID != pid || tracked.identity.Validate() != nil || tracked.pidfd == nil) {
				return nil, errors.New("anchor run child lacks its exact retained process identity and pidfd")
			}
			if physicalOnly, exists := run.physicalOnlyChildren[pid]; exists {
				process := physicalOnly.identity
				if known || process.PID != pid || process.Validate() != nil || physicalOnly.pidfd == nil ||
					physicalOnly.classification.ValidateFor(run.grant, run.helper, process) != nil ||
					physicalOnly.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
					return nil, errors.New("anchor physical-only gate child differs from its exact dedicated PIDFD tracker")
				}
				if _, alreadyInRunEvidence := run.reaped[pid]; alreadyInRunEvidence {
					return nil, errors.New("physical-only pre-RegisterGate child entered RunReapEvidence")
				}
				// This A gate is physical-containment-only, never a Run child receipt.
				if err := anchor.reapPhysicalOnlyChild(ctx, run, process); err != nil {
					return nil, fmt.Errorf("wait/reap exact physical-only pre-RegisterGate child: %w", err)
				}
				continue
			}
			var classification linuxOwnerAnchorRunChildClassification
			classified := false
			postKillRetained := false
			postKillObservation := run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved
			var process authority.LinuxCgroupProcessIdentity
			if known && tracked.classification.currentRunMember() {
				process = tracked.identity
				tracked, classification, err = anchor.classifyRunChildForReap(ctx, run, process, tracked, known)
				if err != nil {
					return nil, err
				}
				classified = true
				if postKillObservation {
					ready, err := linuxOwnerAnchorPIDFDReady(tracked.pidfd)
					if err != nil {
						return nil, fmt.Errorf("poll exact retained child pidfd after run-leaf containment: %w", err)
					}
					postKillRetained = ready && classification.currentRunMember()
				}
			}
			if !postKillRetained {
				identity, err := ProcessIdentity(pid)
				if err != nil {
					return nil, fmt.Errorf("capture anchor-adopted process creation identity: %w", err)
				}
				process = authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
				if known && !tracked.identity.Equal(process) {
					return nil, errors.New("anchor adopted a reused process PID for this run")
				}
				credentials, err := readLinuxOwnerProcessCredentials(pid)
				if err != nil || credentials.Parent != anchor.identity.PID {
					return nil, fmt.Errorf("verify exact anchor-adopted child ancestry: %w", errOr(err, errors.New("PPid differs from anchor")))
				}
			}
			if !known {
				cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
				if err != nil {
					return nil, fmt.Errorf("read untracked anchor child cgroup before cross-RunKey skip: %w", err)
				}
				otherRunChild, err := anchor.skipChildOwnedByAnotherRun(ctx, run, process, cgroupPath)
				if err != nil {
					return nil, fmt.Errorf("classify unknown anchor child before run-wide reap: %w", err)
				}
				if otherRunChild {
					continue
				}
			}
			if evidence, reaped := run.reaped[pid]; reaped {
				if err := evidence.ValidateFor(run.grant, run.helper, process, evidence.ProcessRole); err != nil {
					return nil, err
				}
				return nil, errors.New("anchor child is marked reaped but still appears in the kernel child list")
			}
			if !classified {
				tracked, classification, err = anchor.classifyRunChildForReap(ctx, run, process, tracked, known)
				if err != nil {
					return nil, err
				}
			}
			if classification.Origin == linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
				return nil, errors.New("physical-only pre-RegisterGate child lacks its dedicated Anchor PIDFD tracker")
			}
			role := tracked.role
			if classification.Role != role {
				return nil, errors.New("anchor child origin role differs from its exact retained run classification")
			}
			if !postKillRetained {
				switch role {
				case "gate":
					if transitionErr := run.verifyAuthorizedGateExec(process, !run.leafRemovedObserved); transitionErr == nil {
						role = "native"
						tracked.role = role
						tracked.classification.Role = role
						run.children[pid] = tracked
					} else if gateErr := run.verifyRegisteredGateA(process, !run.leafRemovedObserved); gateErr != nil {
						return nil, errors.Join(fmt.Errorf("verify authorized gate-to-native transition: %w", transitionErr), gateErr)
					}
				case "native":
					if err := run.verifyAuthorizedGateExec(process, !run.leafRemovedObserved); err != nil {
						return nil, fmt.Errorf("verify exact authorized native role before reap: %w", err)
					}
				default:
					if err := verifyLinuxOwnerRunChildCredentials(pid, role, run.owner); err != nil {
						return nil, err
					}
					postKillNamespaceOrigin := classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent &&
						(run.leafRemovedObserved || run.leafKillAcknowledged && run.leafEmptyObserved)
					if !run.leafRemovedObserved && !postKillNamespaceOrigin {
						if err := verifyLinuxOwnerRunChildMembership(pid, role, run.grant, run.leaf); err != nil {
							return nil, err
						}
					}
				}
			}
			foundRunChild = true
			if _, err := run.reapChild(ctx, anchor.identity, process, role); err != nil {
				return nil, err
			}
		}
		if !foundRunChild {
			break
		}
	}
	result := make([]LinuxOwnerAnchorReapEvidence, 0, len(run.reaped))
	for _, evidence := range run.reaped {
		result = append(result, evidence)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Process.PID < result[right].Process.PID })
	return result, nil
}

func (anchor *linuxOwnerAnchor) classifyRunChildForReap(
	ctx context.Context,
	run *linuxOwnerAnchorRun,
	process authority.LinuxCgroupProcessIdentity,
	tracked linuxOwnerAnchorTrackedProcess,
	known bool,
) (linuxOwnerAnchorTrackedProcess, linuxOwnerAnchorRunChildClassification, error) {
	if anchor == nil || ctx == nil || run == nil || process.Validate() != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("anchor run-child classification is incomplete")
	}
	pidfd := -1
	if known {
		if !tracked.identity.Equal(process) || tracked.pidfd == nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("registered run child lacks its exact pidfd")
		}
		pidfd = int(tracked.pidfd.Fd())
	} else {
		var err error
		pidfd, err = unix.PidfdOpen(process.PID, 0)
		if err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("open pidfd for unregistered anchor child classification: %w", err)
		}
		defer unix.Close(pidfd)
		if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, err
		}
	}
	classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
	if err != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("classify exact anchor run-child origin before wait: %w", err)
	}
	if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
		return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, err
	}
	switch classification.Origin {
	case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
		registered, exists := run.children[process.PID]
		if !exists || !registered.identity.Equal(process) || registered.pidfd == nil ||
			registered.role != classification.Role || !registered.classification.Equal(classification) {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("registered-current origin lacks the exact retained RunKey child")
		}
		return registered, classification, nil
	case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
		if classification.Role != "descendant" {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("gate-userns current origin is not a run descendant")
		}
		if known {
			if !tracked.identity.Equal(process) || tracked.classification.ValidateFor(run.grant, run.helper, process) != nil ||
				!tracked.classification.Equal(classification) || tracked.role != classification.Role {
				return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("namespace-proven current child differs from its retained origin pin")
			}
			return tracked, classification, nil
		}
		if err := run.trackNamespaceProvenChild(process, pidfd, classification); err != nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, fmt.Errorf("retain exact namespace-proven anchor child: %w", err)
		}
		registered, exists := run.children[process.PID]
		if !exists || !registered.identity.Equal(process) || registered.pidfd == nil ||
			!registered.classification.Equal(classification) || registered.creator.Validate() == nil {
			return linuxOwnerAnchorTrackedProcess{}, linuxOwnerAnchorRunChildClassification{}, errors.New("namespace-proven child was not retained without a synthetic creator")
		}
		return registered, classification, nil
	case linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly:
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("physical-only pre-RegisterGate gate cannot become RunReapEvidence")
	case linuxOwnerAnchorRunChildOriginPhysicalOnlyForeign:
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("foreign physical-only gate is not a current-RunKey reap candidate")
	case linuxOwnerAnchorRunChildOriginCurrentRunEscaped:
		run.migrationObserved = true
		return linuxOwnerAnchorTrackedProcess{}, classification, errors.New("exact current-run child escaped its reserved cgroup leaf")
	default:
		return linuxOwnerAnchorTrackedProcess{}, classification, fmt.Errorf("Anchor classified child as non-member of this RunKey (%s)", classification.Origin)
	}
}

func (anchor *linuxOwnerAnchor) childBelongsToAnotherRun(
	ctx context.Context,
	except LinuxOwnerAnchorRunKey,
	process authority.LinuxCgroupProcessIdentity,
) (bool, error) {
	if anchor == nil || ctx == nil || process.Validate() != nil || anchor.identity.Validate() != nil {
		return false, errors.New("classify another RunKey child requires exact anchor and process identities")
	}
	if len(anchor.runs) > linuxOwnerHelperV2MaxMembershipProcesses {
		return false, errors.New("anchor RunKey set exceeds its bounded child-origin scan")
	}
	for key, run := range anchor.runs {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if key == except || run == nil {
			continue
		}
		if run.grant.Validate() != nil || run.grant.RunKey != key || run.helper.Validate() != nil {
			return false, errors.New("anchor has an unresolved RunKey while classifying an adopted child")
		}
		if physicalOnly, exists := run.physicalOnlyChildren[process.PID]; exists {
			if !physicalOnly.identity.Equal(process) || physicalOnly.pidfd == nil || physicalOnly.waited ||
				physicalOnly.classification.ValidateFor(run.grant, run.helper, process) != nil ||
				physicalOnly.classification.Origin != linuxOwnerAnchorRunChildOriginPreReleaseGatePhysicalOnly {
				return false, errors.New("other RunKey physical-only gate differs from its exact retained identity")
			}
			if run.retired || run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
				return false, errors.New("physical-only foreign gate lacks its exact live RunKey fence")
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(physicalOnly.pidfd.Fd()), process); err != nil {
				return false, fmt.Errorf("verify foreign physical-only gate pidfd: %w", err)
			}
			identity, err := ProcessIdentity(process.PID)
			if err != nil || identity != process.Identity {
				return false, fmt.Errorf("verify foreign physical-only gate identity: %w", errOr(err, errors.New("creation identity differs")))
			}
			credentials, err := readLinuxOwnerProcessCredentials(process.PID)
			if err != nil || credentials.Parent != anchor.identity.PID {
				return false, fmt.Errorf("verify foreign physical-only gate adoption: %w", errOr(err, errors.New("PPid differs from Anchor")))
			}
			return true, nil
		}
		if run.retired {
			if run.helper.Equal(process) {
				return false, errors.New("retired RunKey helper remains in the anchor child list")
			}
			if child, exists := run.children[process.PID]; exists && child.identity.Equal(process) {
				return false, errors.New("retired RunKey child remains in the anchor child list")
			}
			continue
		}
		if run.helper.Equal(process) {
			if run.helperExit != nil {
				return false, errors.New("reaped other RunKey helper remains in the anchor child list")
			}
			if run.helperPIDFD == nil {
				return false, errors.New("another RunKey helper lacks its previously bound pidfd")
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(int(run.helperPIDFD.Fd()), run.helper); err != nil {
				return false, fmt.Errorf("verify exact other-RunKey helper pidfd: %w", err)
			}
			identity, err := ProcessIdentity(run.helper.PID)
			if err != nil || identity != run.helper.Identity {
				return false, fmt.Errorf("verify exact other-RunKey helper identity: %w", errOr(err, errors.New("creation identity differs")))
			}
			credentials, err := readLinuxOwnerProcessCredentials(run.helper.PID)
			if err != nil || credentials.Parent != anchor.identity.PID {
				return false, fmt.Errorf("verify other-RunKey helper ancestry: %w", errOr(err, errors.New("PPid differs from anchor")))
			}
			return true, nil
		}
		tracked, exists := run.children[process.PID]
		if !exists || !tracked.identity.Equal(process) {
			continue
		}
		if !linuxOwnerAnchorValidChildRole(tracked.role) || tracked.pidfd == nil {
			return false, errors.New("other RunKey child lacks its exact creator, role, or retained pidfd")
		}
		if run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
			return false, errors.New("adopted other RunKey child lacks its exact helper exit proof")
		}
		if err := tracked.classification.ValidateFor(run.grant, run.helper, process); err != nil {
			return false, fmt.Errorf("verify stored other-RunKey child origin: %w", err)
		}
		switch tracked.classification.Origin {
		case linuxOwnerAnchorRunChildOriginRegisteredCurrent:
			if err := validateLinuxOwnerAnchorCreatorChain(ctx, run, tracked); err != nil {
				return false, fmt.Errorf("verify exact creator chain for other RunKey child: %w", err)
			}
		case linuxOwnerAnchorRunChildOriginGateUserNSCurrent:
			if tracked.creator.Validate() == nil || tracked.role != "descendant" {
				return false, errors.New("namespace-derived other RunKey child carries a synthetic creator edge")
			}
		default:
			return false, errors.New("other RunKey child has no registered or namespace-bound origin")
		}
		if reaped, alreadyReaped := run.reaped[process.PID]; alreadyReaped && reaped.Process.Equal(process) {
			return false, errors.New("previously reaped other RunKey child remains in the anchor child list")
		}
		classification, err := anchor.classifyRunChildOrigin(ctx, run, process, int(tracked.pidfd.Fd()))
		if err != nil {
			return false, fmt.Errorf("recheck exact other-RunKey child origin: %w", err)
		}
		if err := classification.ValidateFor(run.grant, run.helper, process); err != nil ||
			classification.OriginRunKey != run.grant.RunKey ||
			(classification.Origin != linuxOwnerAnchorRunChildOriginRegisteredCurrent &&
				classification.Origin != linuxOwnerAnchorRunChildOriginGateUserNSCurrent &&
				classification.Origin != linuxOwnerAnchorRunChildOriginCurrentRunEscaped) {
			return false, errors.Join(errors.New("other RunKey child origin changed during pidfd recheck"), err)
		}
		return true, nil
	}
	pidfd := -1
	for key, run := range anchor.runs {
		if err := ctx.Err(); err != nil {
			if pidfd >= 0 {
				_ = unix.Close(pidfd)
			}
			return false, err
		}
		if key == except || run == nil || run.retired {
			continue
		}
		if pidfd < 0 {
			var err error
			pidfd, err = unix.PidfdOpen(process.PID, 0)
			if err != nil {
				return false, fmt.Errorf("open exact pidfd for unregistered RunKey origin: %w", err)
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
				_ = unix.Close(pidfd)
				return false, err
			}
		}
		classification, err := anchor.classifyRunChildOrigin(ctx, run, process, pidfd)
		if err != nil {
			_ = unix.Close(pidfd)
			return false, fmt.Errorf("classify unregistered child against other RunKey: %w", err)
		}
		if err := classification.ValidateFor(run.grant, run.helper, process); err != nil {
			_ = unix.Close(pidfd)
			return false, fmt.Errorf("validate unregistered child origin against other RunKey: %w", err)
		}
		if classification.OriginRunKey == run.grant.RunKey &&
			(classification.Origin == linuxOwnerAnchorRunChildOriginRegisteredCurrent ||
				classification.Origin == linuxOwnerAnchorRunChildOriginGateUserNSCurrent ||
				classification.Origin == linuxOwnerAnchorRunChildOriginCurrentRunEscaped) {
			_ = unix.Close(pidfd)
			if run.helperExit == nil || !run.helperExit.ValidFor(run.helper, anchor.identity) {
				return false, errors.New("unregistered other-RunKey origin lacks exact helper exit/reparent proof")
			}
			return true, nil
		}
	}
	if pidfd >= 0 {
		_ = unix.Close(pidfd)
	}
	return false, nil
}

func parseLinuxOwnerChildPIDs(contents []byte) ([]int, error) {
	return parseLinuxOwnerChildPIDsBounded(contents, linuxOwnerAnchorMaxChildrenBytes, linuxOwnerHelperV2MaxMembershipProcesses)
}

func parseLinuxOwnerChildPIDsBounded(contents []byte, maxBytes, maxPIDs int) ([]int, error) {
	if maxBytes < 0 || maxPIDs < 0 || len(contents) > maxBytes {
		return nil, errors.New("Linux /proc children list exceeds its bounded byte count")
	}
	pids := make([]int, 0)
	for cursor := 0; cursor < len(contents); {
		for cursor < len(contents) && linuxOwnerChildListSpace(contents[cursor]) {
			cursor++
		}
		if cursor == len(contents) {
			break
		}
		start := cursor
		for cursor < len(contents) && !linuxOwnerChildListSpace(contents[cursor]) {
			cursor++
		}
		if len(pids) >= maxPIDs {
			return nil, errors.New("Linux /proc children list exceeds its bounded process count")
		}
		token := contents[start:cursor]
		if len(token) > 10 || len(token) == 0 || token[0] == '0' {
			return nil, errors.New("Linux /proc children list contains an invalid PID")
		}
		pid := 0
		for _, digit := range token {
			if digit < '0' || digit > '9' {
				return nil, errors.New("Linux /proc children list contains an invalid PID")
			}
			pid = pid*10 + int(digit-'0')
		}
		if pid <= 1 {
			return nil, errors.New("Linux /proc children list contains an invalid PID")
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func readLinuxOwnerChildPIDs(path string) ([]int, error) {
	pids, _, err := readLinuxOwnerChildPIDsBounded(path, linuxOwnerAnchorMaxChildrenBytes, linuxOwnerHelperV2MaxMembershipProcesses)
	return pids, err
}

func readLinuxOwnerChildPIDsBounded(path string, maxBytes, maxPIDs int) ([]int, int, error) {
	if maxBytes < 0 || maxPIDs < 0 {
		return nil, 0, errors.New("Linux /proc children reader received negative bounds")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open bounded Linux /proc children list: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, len(contents), errors.Join(errors.New("read bounded Linux /proc children list"), readErr, closeErr)
	}
	pids, err := parseLinuxOwnerChildPIDsBounded(contents, maxBytes, maxPIDs)
	return pids, len(contents), err
}

func linuxOwnerChildListSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\v' || value == '\f'
}

func readLinuxOwnerProcessChildren(ctx context.Context, process authority.LinuxCgroupProcessIdentity) ([]int, error) {
	if ctx == nil {
		return nil, errors.New("Linux process child census requires a context")
	}
	var lastErr error
	for attempt := 0; attempt < linuxOwnerProcessChildrenSnapshotAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		children, err := readLinuxOwnerProcessChildrenSnapshot(ctx, process)
		if err == nil {
			return children, nil
		}
		lastErr = err
		if !errors.Is(err, errLinuxOwnerProcessChildrenChanged) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if attempt+1 < linuxOwnerProcessChildrenSnapshotAttempts {
			runtime.Gosched()
		}
	}
	return nil, fmt.Errorf("all-thread child census remained unstable after %d bounded attempts: %w", linuxOwnerProcessChildrenSnapshotAttempts, lastErr)
}

func readLinuxOwnerProcessChildrenSnapshot(ctx context.Context, process authority.LinuxCgroupProcessIdentity) ([]int, error) {
	if ctx == nil || process.Validate() != nil {
		return nil, errors.New("Linux process child census requires a context and exact process identity")
	}
	identity, err := ProcessIdentity(process.PID)
	if err != nil || identity != process.Identity {
		return nil, fmt.Errorf("verify process identity before all-thread child census: %w", errOr(err, errors.New("creation identity differs")))
	}
	threadIDs, err := readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil {
		return nil, fmt.Errorf("read exact process thread set before child census: %w", err)
	}
	childrenByThread, err := readLinuxOwnerProcessThreadChildren(ctx, process.PID, threadIDs)
	if err != nil {
		return nil, err
	}
	children := make([]int, 0)
	seenChildren := make(map[int]struct{})
	for _, pids := range childrenByThread {
		for _, pid := range pids {
			if _, duplicate := seenChildren[pid]; duplicate {
				return nil, errors.New("Linux process child appears in multiple thread child lists")
			}
			seenChildren[pid] = struct{}{}
			children = append(children, pid)
		}
	}
	threadIDsAfter, err := readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil || !equalLinuxOwnerProcessThreadIDs(threadIDs, threadIDsAfter) {
		return nil, fmt.Errorf("%w: process TID set changed during all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, errors.New("thread snapshot differs")))
	}
	verifiedChildrenByThread, err := readLinuxOwnerProcessThreadChildren(ctx, process.PID, threadIDs)
	if err != nil || !equalLinuxOwnerProcessChildLists(childrenByThread, verifiedChildrenByThread) {
		return nil, fmt.Errorf("%w: per-thread child lists changed during all-thread census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, errors.New("child snapshots differ")))
	}
	identityAfter, err := ProcessIdentity(process.PID)
	if err != nil || identityAfter != process.Identity {
		return nil, fmt.Errorf("revalidate process identity after all-thread child census: %w", errOr(err, errors.New("creation identity changed")))
	}
	threadIDsAfter, err = readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil || !equalLinuxOwnerProcessThreadIDs(threadIDs, threadIDsAfter) {
		return nil, fmt.Errorf("%w: process TID set changed after all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, errors.New("thread snapshot differs")))
	}
	sort.Ints(children)
	return children, nil
}

func readLinuxOwnerProcessThreadChildren(ctx context.Context, pid int, threadIDs []int) ([][]int, error) {
	childrenByThread := make([][]int, len(threadIDs))
	bytesRead, pidsRead := 0, 0
	for index, tid := range threadIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		childPath := fmt.Sprintf("/proc/%d/task/%d/children", pid, tid)
		pids, n, err := readLinuxOwnerChildPIDsBounded(
			childPath,
			linuxOwnerAnchorMaxChildrenBytes-bytesRead,
			linuxOwnerHelperV2MaxMembershipProcesses-pidsRead,
		)
		if err != nil {
			return nil, fmt.Errorf("read bounded child list for process TID %d: %w", tid, err)
		}
		sort.Ints(pids)
		bytesRead += n
		pidsRead += len(pids)
		childrenByThread[index] = pids
	}
	return childrenByThread, nil
}

func readLinuxOwnerProcessThreadIDs(pid int) ([]int, error) {
	if pid <= 1 {
		return nil, errors.New("Linux process thread census requires an exact PID")
	}
	directory, err := os.Open(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil, fmt.Errorf("open bounded Linux process task directory: %w", err)
	}
	entries, readErr := directory.ReadDir(linuxOwnerHelperV2MaxMembershipProcesses + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
		return nil, errors.Join(errors.New("read bounded Linux process task directory"), readErr, closeErr)
	}
	if len(entries) > linuxOwnerHelperV2MaxMembershipProcesses {
		return nil, errors.New("Linux process thread set exceeds its bounded task count")
	}
	threadIDs := make([]int, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || len(name) > 10 {
			return nil, errors.New("Linux process task directory contains an invalid TID entry")
		}
		tid, err := strconv.Atoi(name)
		if err != nil || tid <= 0 || strconv.Itoa(tid) != name {
			return nil, errors.New("Linux process task directory contains a noncanonical TID")
		}
		threadIDs = append(threadIDs, tid)
	}
	if len(threadIDs) == 0 {
		return nil, errors.New("Linux process task directory contains no live TIDs")
	}
	sort.Ints(threadIDs)
	return threadIDs, nil
}

func equalLinuxOwnerProcessThreadIDs(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalLinuxOwnerProcessChildLists(left, right [][]int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalLinuxOwnerProcessThreadIDs(left[index], right[index]) {
			return false
		}
	}
	return true
}
