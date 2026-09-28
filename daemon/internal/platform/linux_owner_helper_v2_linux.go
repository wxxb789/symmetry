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
	linuxOwnerHelperV2Version      = 1
	linuxOwnerHelperV2BootstrapFD  = 3
	linuxOwnerHelperV2LeafFD       = 4
	linuxOwnerHelperV2ParentFD     = 5
	linuxOwnerHelperV2OwnerPipeFD  = 6
	linuxOwnerHelperV2MaxFrame     = 192 << 10
	linuxOwnerHelperV2Timeout      = 2 * time.Minute
	linuxOwnerHelperV2StartupWait  = 5 * time.Second
	linuxOwnerHelperV2StopTimeout  = 30 * time.Second
	linuxOwnerHelperV2PollInterval = 25 * time.Millisecond
)

var (
	ErrLinuxOwnerHelperV2OwnerLost    = errors.New("Linux owner helper observed daemon-owner loss")
	ErrLinuxOwnerHelperV2NotCommitted = errors.New("Linux owner helper has no durable handoff acknowledgement")
	ErrLinuxOwnerHelperV2LeaseUnarmed = errors.New("Linux owner helper has no acknowledged initial lease")
	ErrLinuxOwnerHelperV2ReleaseUsed  = errors.New("Linux owner helper RELEASE is single-use")
	ErrLinuxOwnerHelperV2StopUnproven = errors.New("Linux owner helper stop is unproven")
	ErrLinuxOwnerHelperV2RecoveryOnly = errors.New("Linux owner helper is latched to recovery-only mode")
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
		!evidence.Helper.Equal(helper) || !evidence.NoProducerCanAct || !evidence.PinnedLeafFD ||
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
		!proof.GateOutcome.Equal(*evidence.GateOutcome) {
		return errors.New("Linux owner helper pre-gate physical proof is inconsistent")
	}
	return nil
}

type LinuxOwnerHelperV2Status struct {
	State              string
	OwnerLost          bool
	CommitAcknowledged bool
	ReleaseAttempted   bool
	Released           bool
	Exit               *LinuxOwnerHelperV2Exit
	Launcher           *authority.LinuxCgroupProcessIdentity
	LauncherExit       *authority.LinuxCgroupExitProof
	Gate               *authority.LinuxCgroupGateIdentity
	UserNamespace      *LinuxUserNamespaceIdentity
	PIDFD              *os.File
	StopEvidence       *LinuxOwnerHelperV2StopEvidence
	ErrorCode          string
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
		if evidence.PreGate.ValidateFor(grant, helper) != nil || launcher.Validate() == nil || gate.Validate() == nil {
			return errors.New("Linux owner helper pre-gate stop evidence is mixed with gate-bound authority")
		}
		return nil
	}
	if launcher.Validate() != nil || gate.Validate() != nil || !evidence.Launcher.Equal(launcher) ||
		!evidence.Gate.Equal(gate) || evidence.Proof.ValidateFor(linuxOwnerHelperV2Supervisor(grant, helper, launcher, gate, evidence.Proof.LauncherExit)) != nil {
		return errors.New("Linux owner helper gate stop proof differs from the exact producer identities")
	}
	decodedDigest, err := hex.DecodeString(evidence.DescendantDigest)
	if err != nil || len(decodedDigest) != sha256.Size || hex.EncodeToString(decodedDigest) != evidence.DescendantDigest {
		return errors.New("Linux owner helper stop evidence has an invalid exact-child digest")
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
	Version      int                                  `json:"version"`
	Grant        LinuxOwnerAnchorGrant                `json:"grant"`
	AnchorSocket string                               `json:"anchor_socket"`
	OwnerProcess authority.LinuxCgroupProcessIdentity `json:"owner_process"`
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

type linuxOwnerHelperV2Response struct {
	Version            int                                        `json:"version"`
	Operation          string                                     `json:"operation"`
	Status             string                                     `json:"status"`
	RunKey             LinuxOwnerAnchorRunKey                     `json:"run_key"`
	Fence              protocolFence                              `json:"fence"`
	LaunchToken        string                                     `json:"launch_token"`
	Helper             authority.LinuxCgroupProcessIdentity       `json:"helper"`
	OwnerLost          bool                                       `json:"owner_lost,omitempty"`
	CommitAcknowledged bool                                       `json:"commit_acknowledged,omitempty"`
	ReleaseAttempted   bool                                       `json:"release_attempted,omitempty"`
	Released           bool                                       `json:"released,omitempty"`
	LeaseSequence      uint64                                     `json:"lease_sequence,omitempty"`
	Launcher           *authority.LinuxCgroupProcessIdentity      `json:"launcher,omitempty"`
	Gate               *authority.LinuxCgroupGateIdentity         `json:"gate,omitempty"`
	LauncherExit       *authority.LinuxCgroupExitProof            `json:"launcher_exit,omitempty"`
	GateProcess        *authority.LinuxCgroupProcessIdentity      `json:"gate_process,omitempty"`
	UserNamespace      *LinuxUserNamespaceIdentity                `json:"user_namespace,omitempty"`
	GateOutcome        *authority.LinuxCgroupGateProductionResult `json:"gate_outcome,omitempty"`
	StopEvidence       *LinuxOwnerHelperV2StopEvidence            `json:"stop_evidence,omitempty"`
	ExitResult         *LinuxOwnerHelperV2Exit                    `json:"exit_result,omitempty"`
	CheckoutReceipt    *authority.LinuxCloneCheckoutReceipt       `json:"checkout_receipt,omitempty"`
	ErrorCode          string                                     `json:"error_code,omitempty"`
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
		!canonicalAbsoluteLinuxPath(bootstrap.AnchorSocket) || bootstrap.OwnerProcess.Validate() != nil {
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
		lease: newLinuxOwnerHelperV2Lease(),
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

func startLinuxOwnerAnchorHelperV2(anchor *linuxOwnerAnchor, grant LinuxOwnerAnchorGrant, ownerProcess authority.LinuxCgroupProcessIdentity, leafFD, ownerPipeFD int) (*linuxOwnerAnchorRun, error) {
	if anchor == nil || grant.Validate() != nil || ownerProcess.Validate() != nil || leafFD < 0 || ownerPipeFD < 0 ||
		!grant.Anchor.Equal(anchor.identity) || !grant.Parent.Equal(anchor.parent.Identity()) {
		return nil, errors.New("Linux owner anchor helper launch grant is incomplete")
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
	request := client.newRequest("status")
	response, rights, err := client.request(ctx, request)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, err
	}
	status := LinuxOwnerHelperV2Status{
		State: response.Status, OwnerLost: response.OwnerLost,
		CommitAcknowledged: response.CommitAcknowledged,
		ReleaseAttempted:   response.ReleaseAttempted, Released: response.Released, Exit: response.ExitResult,
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
	owner        linuxgate.OwnerConfig
	grant        LinuxOwnerAnchorGrant
	helper       authority.LinuxCgroupProcessIdentity
	initialOwner authority.LinuxCgroupProcessIdentity
	anchorClient *LinuxOwnerAnchorClient
	parent       *LinuxCgroupV2Parent
	leaf         *LinuxCgroupV2Leaf
	listener     *net.UnixListener
	ownerPipe    *os.File
	lease        *linuxOwnerHelperV2Lease

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
	killAttempted              bool
	killAcknowledged           bool
	leafEmptyObserved          bool
	descendantReaped           map[int]authority.LinuxCgroupProcessIdentity
	stopEvidence               *LinuxOwnerHelperV2StopEvidence
	stopErrorCode              string
	receiptAcknowledged        bool
	acknowledgedReceipt        *authority.StopReceipt
	acknowledgedPreGateReceipt *authority.LinuxCgroupPreGateStopReceipt
	waitMu                     sync.Mutex
	waitStarted                bool
	waitDone                   chan struct{}
	waitResult                 *LinuxOwnerHelperV2Exit
	waitError                  error
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
	_, err := helper.stopOwnedWithSafeRetry(stopContext)
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
	ownerLost := helper.ownerLost
	prepared := helper.prepareResponse != nil
	evidence := helper.stopEvidence
	commitAcknowledged := helper.commitAcknowledged
	releaseAttempted := helper.releaseAttempted
	released := helper.released
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
	response.CommitAcknowledged = commitAcknowledged
	response.ReleaseAttempted = releaseAttempted
	response.Released = released
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
			if err := helper.anchorClient.RegisterProcess(context.Background(), helper.grant, helper.helper, helper.helper, process, "gate", gatePIDFD); err != nil {
				prepareErr = errors.Join(prepareErr, fmt.Errorf("register exact parked gate with owner anchor: %w", err))
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
	ownerPipe := helper.ownerPipe
	helper.stateMu.Unlock()
	onCheckoutReady := func(callbackContext context.Context, receipt authority.LinuxCloneCheckoutReceipt) error {
		if !linuxOwnerHelperV2CheckoutReceiptMatches(receipt, cloneGrant) {
			return errors.New("Linux gate checkout receipt differs from the exact clone grant")
		}
		return helper.relayCheckoutReady(callbackContext, connection, request, cloneGrant, receipt)
	}
	err := prepared.Release(releaseContext, int(helper.leaf.leafFD), command, linuxgate.ReleaseGuard{
		OwnerPipe: ownerPipe, LeaseDeadline: leaseDeadline,
	}, onCheckoutReady)
	cancelRelease()
	helper.stateMu.Lock()
	helper.releaseCancel = nil
	if err == nil {
		helper.released = true
	}
	helper.stateMu.Unlock()
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
		_, stopErr := helper.stopOwnedWithSafeRetry(ctx)
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
	evidence, err := helper.stopOwnedWithSafeRetry(ctx)
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

func (helper *linuxOwnerHelperV2) stopPinnedLeaf(ctx context.Context) error {
	helper.stateMu.Lock()
	killAttempted := helper.killAttempted
	killAcknowledged := helper.killAcknowledged
	leafEmpty := helper.leafEmptyObserved
	helper.stateMu.Unlock()
	if !killAcknowledged {
		if killAttempted {
			if !leafEmpty {
				if err := helper.leaf.WaitEmpty(ctx); err != nil {
					helper.setStopError("cgroup_kill_unacknowledged")
					return fmt.Errorf("%w: prior cgroup.kill write result is unknown and the exact pinned leaf is not empty: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
				}
				helper.stateMu.Lock()
				helper.leafEmptyObserved = true
				helper.stateMu.Unlock()
			}
			helper.setStopError("cgroup_kill_unacknowledged")
			return fmt.Errorf("%w: prior cgroup.kill write result is unknown; pinned emptiness does not acknowledge the write", ErrLinuxOwnerHelperV2StopUnproven)
		}
		if err := helper.leaf.VerifyKillControl(); err != nil {
			helper.setStopError("cgroup_kill_control_unavailable")
			return fmt.Errorf("%w: exact leaf kill control is not A-writable: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		if err := helper.leaf.Kill(ctx); err != nil {
			if errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
				helper.setStopError("cgroup_kill_not_attempted")
				return fmt.Errorf("%w: exact cgroup.kill syscall was not attempted and may be retried after producer fencing: %w", ErrLinuxOwnerHelperV2StopUnproven, err)
			}
			helper.stateMu.Lock()
			helper.killAttempted = true
			helper.stateMu.Unlock()
			helper.setStopError("cgroup_kill_unacknowledged")
			if emptyErr := helper.leaf.WaitEmpty(ctx); emptyErr == nil {
				helper.stateMu.Lock()
				helper.leafEmptyObserved = true
				helper.stateMu.Unlock()
				return fmt.Errorf("%w: exact cgroup.kill write result is unknown despite pinned empty observation: %w", ErrLinuxOwnerHelperV2StopUnproven, err)
			} else {
				return fmt.Errorf("%w: exact cgroup.kill write result is unknown: %w; pinned empty remeasurement: %v", ErrLinuxOwnerHelperV2StopUnproven, err, emptyErr)
			}
		}
		helper.stateMu.Lock()
		helper.killAttempted = true
		helper.killAcknowledged = true
		helper.stateMu.Unlock()
	}
	if !leafEmpty {
		if err := helper.leaf.WaitEmpty(ctx); err != nil {
			helper.setStopError("cgroup_empty_unproven")
			return fmt.Errorf("%w: exact recursive cgroup did not prove empty: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		helper.stateMu.Lock()
		helper.leafEmptyObserved = true
		helper.stateMu.Unlock()
	}
	return nil
}

func (helper *linuxOwnerHelperV2) reapAfterUnknownKill(ctx context.Context, gate *authority.LinuxCgroupGateIdentity) error {
	if helper == nil || ctx == nil {
		return errors.New("Linux owner helper unknown-kill reaping is incomplete")
	}
	helper.stateMu.Lock()
	unknownEmpty := helper.killAttempted && !helper.killAcknowledged && helper.leafEmptyObserved
	releaseDone := helper.releaseDone
	helper.stateMu.Unlock()
	if !unknownEmpty {
		return nil
	}
	if releaseDone != nil {
		select {
		case <-releaseDone:
		case <-ctx.Done():
			return fmt.Errorf("wait for release before unknown-kill child reap: %w", ctx.Err())
		}
	}
	if gate != nil {
		if _, err := helper.waitGate(ctx); err != nil {
			return fmt.Errorf("wait for exact gate before unknown-kill descendant reap: %w", err)
		}
	}
	if _, _, err := helper.reapAdoptedChildren(ctx, gate); err != nil {
		return fmt.Errorf("reap exact descendants after unknown cgroup.kill result: %w", err)
	}
	return nil
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
	releaseDone := helper.releaseDone
	helper.stateMu.Unlock()
	defer func() {
		helper.stateMu.Lock()
		helper.stopInFlight = false
		helper.stopDone = nil
		close(done)
		helper.stateMu.Unlock()
	}()
	if prepareStarted && prepareDone != nil {
		select {
		case <-prepareDone:
		case <-ctx.Done():
			helper.setStopError("prepare_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: wait for root producer fencing: %v", ErrLinuxOwnerHelperV2StopUnproven, ctx.Err())
		}
	}
	helper.stateMu.Lock()
	prepared := helper.prepared
	gate := cloneLinuxOwnerGateIdentity(helper.gate)
	launcher := cloneLinuxOwnerProcessIdentity(helper.launcher)
	launcherExit := cloneLinuxOwnerAnchorExitProof(helper.launcherExit)
	gateOutcome := helper.gateOutcome
	prepareStarted = helper.prepareStarted
	helper.stateMu.Unlock()
	if prepared == nil && gate == nil {
		if prepareStarted && (launcher == nil || launcherExit == nil || gateOutcome == nil ||
			gateOutcome.ValidateFor(helper.grant.LaunchToken, *launcher) != nil ||
			!launcherExit.ValidForOwnedDescendant(*launcher, helper.helper, helper.grant.Anchor, nil)) {
			helper.setStopError("no_gate_producer_exit_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: helper cannot prove that the exact launcher never produced a gate", ErrLinuxOwnerHelperV2StopUnproven)
		}
		if err := helper.stopPinnedLeaf(ctx); err != nil {
			return LinuxOwnerHelperV2StopEvidence{}, errors.Join(err, helper.reapAfterUnknownKill(ctx, nil))
		}
		if releaseDone != nil {
			select {
			case <-releaseDone:
			case <-ctx.Done():
				helper.setStopError("release_did_not_quiesce")
				return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: one-use release did not quiesce after Abort: %v", ErrLinuxOwnerHelperV2StopUnproven, ctx.Err())
			}
		}
		descendantCount, descendantDigest, err := helper.reapAdoptedChildren(ctx, nil)
		if err != nil {
			helper.setStopError("descendant_reap_unproven")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: pre-gate run child could not be exactly reaped: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
		}
		preGate := &LinuxOwnerHelperV2PreGateStopEvidence{
			Version: linuxOwnerHelperV2Version, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
			LaunchToken: helper.grant.LaunchToken, Parent: helper.grant.Parent, Leaf: helper.grant.Leaf,
			Anchor: helper.grant.Anchor, Helper: helper.helper, PrepareAttempted: prepareStarted,
			NoProducerCanAct: true, PinnedLeafFD: true, CgroupKillAcknowledged: true,
			CgroupPopulatedZero: true, NoMigrationObserved: true, DescendantsReaped: true,
			DescendantCount: descendantCount, DescendantDigest: descendantDigest,
		}
		if prepareStarted {
			preGate.Launcher = launcher
			preGate.LauncherExit = launcherExit
			preGate.GateOutcome = gateOutcome
			preGate.Proof = &authority.LinuxCgroupPreGateStopProof{
				Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
				Issuer: helper.helper, Reaper: helper.helper, PinnedLeafFD: true,
				CgroupKillAcknowledged: true, CgroupPopulatedZero: true, NoMigrationObserved: true,
				ProducerFenced: true, DescendantsReaped: true,
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
	if prepared == nil || gate == nil || launcher == nil || launcherExit == nil {
		helper.setStopError("gate_or_producer_identity_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact gate or producer wait is unavailable", ErrLinuxOwnerHelperV2StopUnproven)
	}
	if err := ctx.Err(); err != nil {
		helper.setStopError("stop_context_expired_before_kill")
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	if err := unix.PidfdSendSignal(int(prepared.PIDFD().Fd()), 0, nil, 0); err == nil {
		if membershipErr := prepared.VerifyLeafMembership(int(helper.leaf.leafFD)); membershipErr != nil {
			if stillLiveErr := unix.PidfdSendSignal(int(prepared.PIDFD().Fd()), 0, nil, 0); stillLiveErr == nil {
				helper.setStopError("gate_leaf_membership_unproven")
				return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: live gate differs from the exact pinned leaf: %v", ErrLinuxOwnerHelperV2StopUnproven, membershipErr)
			} else if !errors.Is(stillLiveErr, unix.ESRCH) {
				helper.setStopError("gate_identity_unproven")
				return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact gate liveness changed ambiguously: %v", ErrLinuxOwnerHelperV2StopUnproven, stillLiveErr)
			}
		}
	} else if !errors.Is(err, unix.ESRCH) {
		helper.setStopError("gate_identity_unproven")
		return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: exact gate pidfd identity could not be probed: %v", ErrLinuxOwnerHelperV2StopUnproven, err)
	}
	if err := helper.stopPinnedLeaf(ctx); err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.Join(err, helper.reapAfterUnknownKill(ctx, gate))
	}
	if releaseDone != nil {
		select {
		case <-releaseDone:
		case <-ctx.Done():
			helper.setStopError("release_did_not_quiesce")
			return LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("%w: one-use release did not quiesce after Abort: %v", ErrLinuxOwnerHelperV2StopUnproven, ctx.Err())
		}
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
		Issuer: helper.helper, Reaper: helper.helper, PinnedLeafFD: true,
		CgroupKillAcknowledged: true, CgroupPopulatedZero: true, NoMigrationObserved: true,
		ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: *launcherExit, GateExit: gateExit,
	}
	evidence := LinuxOwnerHelperV2StopEvidence{
		Version: linuxOwnerHelperV2Version, RunKey: helper.grant.RunKey, Fence: helper.grant.Fence,
		LaunchToken: helper.grant.LaunchToken, Parent: helper.grant.Parent, Leaf: helper.grant.Leaf,
		Anchor: helper.grant.Anchor, Helper: helper.helper, Launcher: *launcher,
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
	childrenPath := fmt.Sprintf("/proc/%d/task/%d/children", os.Getpid(), os.Getpid())
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
		contents, err := os.ReadFile(childrenPath)
		if err != nil {
			return 0, "", fmt.Errorf("read helper's exact kernel child list: %w", err)
		}
		fields := strings.Fields(string(contents))
		if len(fields) == 0 {
			sort.Strings(identityRows)
			digest := sha256.Sum256([]byte(strings.Join(identityRows, "\n")))
			return uint64(len(seen)), hex.EncodeToString(digest[:]), nil
		}
		for _, field := range fields {
			pid, err := strconv.Atoi(field)
			if err != nil || pid <= 1 || strconv.Itoa(pid) != field {
				return 0, "", errors.New("helper child list contains an invalid PID")
			}
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

func (helper *linuxOwnerHelperV2) stopOwnedWithSafeRetry(ctx context.Context) (LinuxOwnerHelperV2StopEvidence, error) {
	if helper == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux owner helper is unavailable")
	}
	evidence, err := helper.stopOwned(ctx)
	if err == nil || !errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
		return evidence, err
	}
	retryContext, cancel := context.WithTimeout(ctx, linuxOwnerHelperV2StopTimeout)
	defer cancel()
	return helper.stopOwned(retryContext)
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
	childrenPath := fmt.Sprintf("/proc/%d/task/%d/children", anchor.identity.PID, anchor.identity.PID)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contents, err := os.ReadFile(childrenPath)
		if err != nil {
			return nil, fmt.Errorf("read anchor's exact kernel child list: %w", err)
		}
		pids, err := parseLinuxOwnerChildPIDs(contents)
		if err != nil {
			return nil, err
		}
		var foundRunChild bool
		for _, pid := range pids {
			identity, err := ProcessIdentity(pid)
			if err != nil {
				return nil, fmt.Errorf("capture anchor-adopted process creation identity: %w", err)
			}
			process := authority.LinuxCgroupProcessIdentity{PID: pid, Identity: identity}
			tracked, known := run.children[pid]
			if known && !tracked.identity.Equal(process) {
				return nil, errors.New("anchor adopted a reused process PID for this run")
			}
			credentials, err := readLinuxOwnerProcessCredentials(pid)
			if err != nil || credentials.Parent != anchor.identity.PID {
				return nil, fmt.Errorf("verify exact anchor-adopted child ancestry: %w", errOr(err, errors.New("PPid differs from anchor")))
			}
			cgroupPath, err := readLinuxOwnerProcessCgroupPath(pid)
			if err != nil {
				return nil, err
			}
			role := "descendant"
			if known {
				role = tracked.role
				if err := verifyLinuxOwnerRunChildCredentials(pid, role, run.owner); err != nil {
					return nil, err
				}
				if !run.leafRemovedObserved {
					if err := verifyLinuxOwnerRunChildMembership(pid, role, run.grant, run.leaf); err != nil {
						return nil, err
					}
				}
			} else {
				leafPath := run.grant.Parent.Path + "/" + run.grant.Leaf.LeafName
				if !linuxOwnerCgroupPathWithin(cgroupPath, leafPath) {
					if anchor.childBelongsToAnotherRun(run.grant.RunKey, process, cgroupPath) {
						continue
					}
					return nil, errors.New("anchor observed an unregistered child outside this exact run leaf")
				}
				if err := verifyLinuxOwnerRunChildCredentials(pid, role, run.owner); err != nil {
					return nil, err
				}
				if !run.leafRemovedObserved {
					if err := verifyLinuxOwnerRunChildMembership(pid, role, run.grant, run.leaf); err != nil {
						return nil, err
					}
				}
			}
			foundRunChild = true
			if evidence, reaped := run.reaped[pid]; reaped {
				if err := evidence.ValidateFor(run.grant, run.helper, process, role); err != nil {
					return nil, err
				}
				return nil, errors.New("anchor child is marked reaped but still appears in the kernel child list")
			}
			if !known {
				pidfd, err := unix.PidfdOpen(pid, 0)
				if err != nil {
					return nil, fmt.Errorf("open pidfd for exact anchor-adopted run child: %w", err)
				}
				if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
					_ = unix.Close(pidfd)
					return nil, err
				}
				pidfdFile := os.NewFile(uintptr(pidfd), "symmetry-owner-anchor-run-child-pidfd")
				if pidfdFile == nil {
					_ = unix.Close(pidfd)
					return nil, errors.New("wrap exact anchor-adopted run child pidfd")
				}
				run.children[pid] = linuxOwnerAnchorTrackedProcess{
					identity: process, creator: run.helper, role: role, pidfd: pidfdFile,
				}
			}
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

func (anchor *linuxOwnerAnchor) childBelongsToAnotherRun(except LinuxOwnerAnchorRunKey, process authority.LinuxCgroupProcessIdentity, cgroupPath string) bool {
	for key, run := range anchor.runs {
		if key == except || run == nil {
			continue
		}
		if run.helper.Equal(process) {
			return true
		}
		if tracked, ok := run.children[process.PID]; ok && tracked.identity.Equal(process) {
			return true
		}
		otherLeaf := run.grant.Parent.Path + "/" + run.grant.Leaf.LeafName
		if linuxOwnerCgroupPathWithin(cgroupPath, otherLeaf) {
			return true
		}
	}
	return false
}

func parseLinuxOwnerChildPIDs(contents []byte) ([]int, error) {
	fields := strings.Fields(string(contents))
	pids := make([]int, len(fields))
	for index, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 || strconv.Itoa(pid) != field {
			return nil, errors.New("Linux /proc children list contains an invalid PID")
		}
		pids[index] = pid
	}
	return pids, nil
}
