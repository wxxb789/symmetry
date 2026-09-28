//go:build linux && amd64

package platform

import (
	"bytes"
	"context"
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
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxOwnerAnchorProtocolVersion        = 1
	linuxOwnerAnchorReservationVersion     = 2
	linuxOwnerAnchorMaxFrame               = 128 << 10
	linuxOwnerAnchorMaxSocketPath          = 108
	linuxOwnerAnchorRequestTimeout         = 5 * time.Second
	linuxOwnerAnchorWaitTimeout            = 30 * time.Second
	linuxOwnerAnchorWaitPoll               = 25 * time.Millisecond
	linuxOwnerAnchorReapPageSize           = 4
	linuxOwnerAnchorOperationStart         = "start-helper-v2"
	linuxOwnerAnchorOperationAttach        = "attach-helper-v2"
	linuxOwnerAnchorOperationLookup        = "lookup-helper-v2"
	linuxOwnerAnchorOperationRetire        = "retire-helper-v2"
	linuxOwnerAnchorOperationReapRun       = "wait-reap-run-v2"
	linuxOwnerAnchorOperationWaitHelper    = "wait-helper-v2"
	linuxOwnerAnchorOperationRetireCleared = "retire-cleared-run-v2"
	linuxOwnerAnchorOperationRetireAborted = "retire-aborted-helper-v2"
)

// DefaultLinuxOwnerAnchorSocketPath is the systemd-provisioned A-private
// endpoint used by the daemon preflight and the per-run v2 helper.
const DefaultLinuxOwnerAnchorSocketPath = "/run/symmetry-owner/owner.sock"

var ErrLinuxOwnerAnchorHelperAlreadyStarted = errors.New("Linux cgroup-v2 owner helper already exists; attach to its exact identity instead of starting another")

// LinuxOwnerAnchorRunKey duplicates only the platform-neutral durable RunKey
// shape. The platform package cannot import state because state consumes this
// package's cgroup release capability.
type LinuxOwnerAnchorRunKey struct {
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
}

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
// for command/argv submission: the anchor, when implemented, may start only a
// fixed hidden helper mode in the existing symmetry-daemon binary.
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
		response.RunKey != (LinuxOwnerAnchorRunKey{}) || response.Fence != (protocol.Fence{}) || response.LaunchToken != "" ||
		response.Helper != nil || response.Evidence != nil ||
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

// RegisterProcess binds a durable gate, launcher, native PID, or known
// descendant to the exact run/helper after the caller has durably recorded
// that process identity. The server independently checks its pidfd, ancestry,
// host credentials, pinned leaf membership, and manager/leaf path.
func (client *LinuxOwnerAnchorClient) RegisterProcess(ctx context.Context, grant LinuxOwnerAnchorGrant, helper, creator, process authority.LinuxCgroupProcessIdentity, role string, pidfd int) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || creator.Validate() != nil || process.Validate() != nil || pidfd < 0 {
		return errors.New("Linux owner anchor process registration is incomplete")
	}
	if role != "launcher" && role != "gate" && role != "native" && role != "descendant" {
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

// WaitReapRun asks the anchor to reap every currently adopted child bound to
// this exact RunKey after helper death. The response is paged so large native
// descendant sets cannot exceed the local seqpacket frame limit.
func (client *LinuxOwnerAnchorClient) WaitReapRun(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) ([]LinuxOwnerAnchorReapEvidence, error) {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil {
		return nil, errors.New("Linux owner anchor run-wide wait/reap request is incomplete")
	}
	var result []LinuxOwnerAnchorReapEvidence
	afterPID := 0
	for {
		request := linuxOwnerAnchorRequest{
			Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationReapRun,
			Grant: grant, Helper: &helper, AfterPID: afterPID,
		}
		response, rights, err := client.exchange(ctx, request)
		closeLinuxOwnerAnchorFDs(rights)
		if err != nil {
			return nil, err
		}
		if !response.matches(grant, linuxOwnerAnchorOperationReapRun) || response.Status != "reaped" ||
			response.Evidence != nil || len(rights) != 0 || len(response.ReapEvidence) > linuxOwnerAnchorReapPageSize {
			return nil, errors.New("Linux owner anchor returned an invalid run-wide reap page")
		}
		for _, evidence := range response.ReapEvidence {
			if !linuxOwnerAnchorValidChildRole(evidence.ProcessRole) ||
				evidence.ValidateFor(grant, helper, evidence.Process, evidence.ProcessRole) != nil || evidence.Process.PID <= afterPID {
				return nil, errors.New("Linux owner anchor run-wide reap page contains non-exact evidence")
			}
			result = append(result, evidence)
		}
		if !response.More {
			return result, nil
		}
		if response.NextPID <= afterPID || len(response.ReapEvidence) == 0 {
			return nil, errors.New("Linux owner anchor run-wide reap cursor did not advance")
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
	if client == nil || ctx == nil || ctx.Err() != nil || request.Grant.Validate() != nil {
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor request has no live client, context, or exact grant")
	}
	if request.Grant.OwnerUID != client.owner.AUID || request.Grant.LauncherUID != 0 {
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor grant UID boundary differs from fixed A/B configuration")
	}
	if request.Grant.Anchor.PID == os.Getpid() {
		return linuxOwnerAnchorResponse{}, nil, errors.New("daemon client and Linux owner anchor must be distinct exact processes")
	}
	timeout := linuxOwnerAnchorRequestTimeout
	if request.Operation == "wait-reap" {
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
	connection, stopClose, err := client.dial(ctx, timeout)
	if err != nil {
		return linuxOwnerAnchorResponse{}, nil, err
	}
	defer connection.Close()
	defer stopClose()
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, request.Grant.Anchor); err != nil {
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
	if !response.matches(request.Grant, request.Operation) {
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
	Creator             *authority.LinuxCgroupProcessIdentity       `json:"creator,omitempty"`
	Process             *authority.LinuxCgroupProcessIdentity       `json:"process,omitempty"`
	Role                string                                      `json:"role,omitempty"`
	ReleaseProof        *authority.SupervisorHandoffReleaseProof    `json:"release_proof,omitempty"`
	PreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	AbortProof          *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
	RunKey              LinuxOwnerAnchorRunKey                      `json:"run_key,omitempty"`
	Fence               protocol.Fence                              `json:"fence,omitempty"`
	AfterPID            int                                         `json:"after_pid,omitempty"`
}

type linuxOwnerAnchorResponse struct {
	Version      int                                   `json:"version"`
	Operation    string                                `json:"operation"`
	Status       string                                `json:"status"`
	RunKey       LinuxOwnerAnchorRunKey                `json:"run_key"`
	Fence        protocol.Fence                        `json:"fence"`
	LaunchToken  string                                `json:"launch_token"`
	Anchor       authority.LinuxCgroupProcessIdentity  `json:"anchor"`
	Parent       authority.LinuxCgroupParentIdentity   `json:"parent"`
	OwnerUID     uint32                                `json:"owner_uid"`
	ManagerPath  string                                `json:"manager_path"`
	Helper       *authority.LinuxCgroupProcessIdentity `json:"helper,omitempty"`
	Evidence     *LinuxOwnerAnchorReapEvidence         `json:"evidence,omitempty"`
	ReapEvidence []LinuxOwnerAnchorReapEvidence        `json:"reap_evidence,omitempty"`
	HelperExit   *authority.LinuxCgroupExitProof       `json:"helper_exit,omitempty"`
	NextPID      int                                   `json:"next_pid,omitempty"`
	More         bool                                  `json:"more,omitempty"`
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
func RunLinuxOwnerAnchor(args []string) error {
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
	listener, socketIdentity, err := listenLinuxOwnerAnchor(options.socketPath, owner)
	if err != nil {
		return err
	}
	defer unlinkLinuxOwnerAnchorSocket(options.socketPath, socketIdentity, owner.AUID)
	anchor := &linuxOwnerAnchor{
		owner: owner, parent: parent, listener: listener,
		identity:    authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: anchorIdentity},
		managerPath: parent.Identity().Path, socketPath: options.socketPath,
		runs: make(map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := notifyLinuxOwnerAnchorReady(); err != nil {
		return err
	}
	return anchor.serve(ctx)
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

type linuxOwnerAnchor struct {
	owner            linuxgate.OwnerConfig
	parent           *LinuxCgroupV2Parent
	listener         *net.UnixListener
	identity         authority.LinuxCgroupProcessIdentity
	managerPath      string
	socketPath       string
	stopping         bool
	shutdownDeadline time.Time
	shutdownForced   bool
	shutdownError    error
	runs             map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun
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
		defer cancel()
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
		_ = connection.Close()
	}
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
	for _, run := range anchor.runs {
		if run == nil || run.retired || run.helperPIDFD == nil || run.helperDeathContainmentAttempted || run.helperDeathContainmentComplete {
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
			return run.supervisionError
		}
		run.helperDeathContainmentComplete = true
		run.supervisionError = nil
	}
	for _, run := range anchor.runs {
		if run != nil && !run.retired && run.helperDeathContainmentAttempted &&
			!run.helperDeathContainmentComplete && run.supervisionError != nil {
			return run.supervisionError
		}
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
	run.supervisionError = nil
	return nil
}

func (anchor *linuxOwnerAnchor) forceStopRunForShutdown(ctx context.Context, run *linuxOwnerAnchorRun) error {
	if ctx == nil || run == nil || run.leaf == nil || run.helperPIDFD == nil || run.grant.Validate() != nil {
		return ErrLinuxCgroupV2Unresolved
	}
	var helperSignalErr error
	var helperWaitErr error
	if run.helperExit == nil {
		if err := unix.PidfdSendSignal(int(run.helperPIDFD.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			helperSignalErr = fmt.Errorf("signal exact helper during bounded anchor shutdown: %w", err)
		}
		if err := run.waitHelper(ctx); err != nil {
			helperWaitErr = fmt.Errorf("wait/reap exact helper during bounded anchor shutdown: %w", err)
		}
	}
	var launcherSignalErr error
	for _, child := range run.children {
		if child.role != "launcher" || child.pidfd == nil {
			continue
		}
		if err := unix.PidfdSendSignal(int(child.pidfd.Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			launcherSignalErr = errors.Join(launcherSignalErr, fmt.Errorf("fence exact root launcher during anchor shutdown: %w", err))
		}
	}
	present, err := linuxOwnerAnchorPinnedLeafPresent(run)
	if err != nil {
		return errors.Join(helperSignalErr, fmt.Errorf("verify exact pinned leaf during anchor shutdown: %w", err))
	}
	if !present {
		run.leafRemovedObserved = true
		run.leafEmptyObserved = true
	} else if !run.leafEmptyObserved {
		leafContext, cancel := context.WithTimeout(context.Background(), linuxOwnerAnchorWaitTimeout)
		defer cancel()
		if !run.leafKillAcknowledged {
			if run.leafKillAttempted {
				if err := run.leaf.WaitEmpty(leafContext); err != nil {
					return errors.Join(helperSignalErr, helperWaitErr, fmt.Errorf("unknown prior exact cgroup.kill result remains populated during anchor shutdown: %w", err))
				}
				run.leafEmptyObserved = true
			} else {
				if err := run.leaf.VerifyKillControl(); err != nil {
					return errors.Join(helperSignalErr, helperWaitErr, fmt.Errorf("verify exact cgroup kill control during anchor shutdown: %w", err))
				}
				if err := run.leaf.Kill(leafContext); err != nil {
					if errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
						return errors.Join(helperSignalErr, helperWaitErr, fmt.Errorf("exact cgroup.kill was not written during anchor shutdown: %w", err))
					}
					run.leafKillAttempted = true
					if emptyErr := run.leaf.WaitEmpty(leafContext); emptyErr != nil {
						return errors.Join(helperSignalErr, helperWaitErr, fmt.Errorf("cgroup.kill result is unknown during anchor shutdown: %w", err), emptyErr)
					}
					run.leafEmptyObserved = true
				} else {
					run.leafKillAttempted = true
					run.leafKillAcknowledged = true
				}
			}
		}
		if !run.leafEmptyObserved {
			if err := run.leaf.WaitEmpty(leafContext); err != nil {
				return errors.Join(helperSignalErr, helperWaitErr, fmt.Errorf("wait for exact run leaf empty during anchor shutdown: %w", err))
			}
			run.leafEmptyObserved = true
		}
	}
	if run.helperExit == nil {
		return errors.Join(helperSignalErr, helperWaitErr, errors.New("anchor cannot reap run children without exact helper wait/reap proof"))
	}
	if _, err := anchor.reapRunChildren(ctx, run); err != nil {
		return errors.Join(helperSignalErr, helperWaitErr, launcherSignalErr, fmt.Errorf("reap exact adopted run children during anchor shutdown: %w", err))
	}
	return nil
}

func (anchor *linuxOwnerAnchor) serveConnection(connection *net.UnixConn) {
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
	if anchor.stopping && (request.Operation == "identity" || request.Operation == linuxOwnerAnchorOperationStart || request.Operation == "register-process") {
		anchor.writeResponse(connection, request, "anchor_stopping", nil, nil)
		return
	}
	if request.Operation == "identity" {
		if linuxOwnerCgroupPathsOverlap(peer.CgroupPath, anchor.managerPath) {
			return
		}
		if len(receivedFDs) != 0 || request.Grant != (LinuxOwnerAnchorGrant{}) || request.Helper != nil || request.Creator != nil || request.Process != nil || request.Role != "" ||
			request.ReleaseProof != nil || request.PreGateReleaseProof != nil || request.AbortProof != nil ||
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
	if request.Grant.Validate() != nil || request.Grant.OwnerUID != anchor.owner.AUID || request.Grant.LauncherUID != 0 ||
		!request.Grant.Anchor.Equal(anchor.identity) ||
		!request.Grant.Parent.Equal(anchor.parent.Identity()) {
		return
	}
	helperPeer := false
	if request.Operation == "register-process" && request.Helper != nil {
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
		// Retain the RunKey tombstone before the first fork. An uncertain launch
		// response can never cause this anchor to create a second child.
		anchor.runs[request.Grant.RunKey] = nil
		ownerProcess := authority.LinuxCgroupProcessIdentity{PID: peer.PID, Identity: peer.Identity}
		run, err := startLinuxOwnerAnchorHelperV2(anchor, request.Grant, ownerProcess, receivedFDs[0], receivedFDs[1])
		if run != nil {
			anchor.runs[request.Grant.RunKey] = run
		}
		if err != nil {
			anchor.writeResponse(connection, request, "start_unresolved", nil, nil)
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
		if len(receivedFDs) != 1 || request.Helper == nil || request.Creator == nil || request.Process == nil {
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
		cancel()
		if err != nil {
			anchor.writeResponse(connection, request, "wait_unproven", nil, nil)
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
		anchor.writeResponseWithReapEvidence(connection, request, "reaped", filtered, nextPID, more)
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
}

func (anchor *linuxOwnerAnchor) writeResponse(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, helper *authority.LinuxCgroupProcessIdentity, evidence *LinuxOwnerAnchorReapEvidence) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, Helper: helper, Evidence: evidence,
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

func (anchor *linuxOwnerAnchor) writeResponseWithReapEvidence(connection *net.UnixConn, request linuxOwnerAnchorRequest, status string, evidence []LinuxOwnerAnchorReapEvidence, nextPID int, more bool) {
	response := linuxOwnerAnchorResponse{
		Version: linuxOwnerAnchorProtocolVersion, Operation: request.Operation, Status: status,
		RunKey: request.Grant.RunKey, Fence: request.Grant.Fence, LaunchToken: request.Grant.LaunchToken,
		Anchor: anchor.identity, Parent: anchor.parent.Identity(), OwnerUID: anchor.owner.AUID,
		ManagerPath: anchor.managerPath, ReapEvidence: evidence, NextPID: nextPID, More: more,
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
	grant       LinuxOwnerAnchorGrant
	owner       linuxgate.OwnerConfig
	helper      authority.LinuxCgroupProcessIdentity
	helperPIDFD *os.File
	helperExit  *authority.LinuxCgroupExitProof
	leaf        *LinuxCgroupV2Leaf
	// These observations are ephemeral emergency-stop state only; none can
	// authorize State receipt creation or journal clearing.
	helperDeathContainmentComplete  bool
	helperDeathContainmentAttempted bool
	supervisionError                error
	leafKillAttempted               bool
	leafKillAcknowledged            bool
	leafEmptyObserved               bool
	leafRemovedObserved             bool
	children                        map[int]linuxOwnerAnchorTrackedProcess
	// reaped retains exact responses only while this anchor process lives. It
	// is not durable and cannot recover proof after correlated anchor loss.
	reaped                map[int]LinuxOwnerAnchorReapEvidence
	retired               bool
	retiredRelease        *authority.SupervisorHandoffReleaseProof
	retiredPreGateRelease *authority.LinuxCgroupPreGateReleaseProof
	retiredAbort          *authority.LinuxCgroupReservationAbortProof
}

type linuxOwnerAnchorTrackedProcess struct {
	identity authority.LinuxCgroupProcessIdentity
	creator  authority.LinuxCgroupProcessIdentity
	role     string
	pidfd    *os.File
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
		creator.Validate() != nil || pidfd < 0 || run.helperPIDFD == nil || run.helperExit != nil || run.leaf == nil {
		return errors.New("Linux owner anchor child registration is not within the live helper phase")
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
		if !existing.identity.Equal(process) || !existing.creator.Equal(creator) {
			return errors.New("Linux owner anchor refuses a reused process PID")
		}
		if existing.role == role {
			if evidence, reaped := run.reaped[process.PID]; reaped {
				return evidence.ValidateFor(run.grant, run.helper, process, role)
			}
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, process); err != nil {
				return fmt.Errorf("verify replayed Linux owner child pidfd: %w", err)
			}
			return nil
		}
		if existing.role != "gate" || role != "native" {
			return errors.New("Linux owner anchor refuses invalid same-PID role transition")
		}
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
	run.children[process.PID] = linuxOwnerAnchorTrackedProcess{
		identity: process, creator: creator, role: role,
		pidfd: os.NewFile(uintptr(copyFD), "symmetry-owner-anchor-run-child-pidfd"),
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
	clear(run.reaped)
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
	for {
		if err := bounded.Err(); err != nil {
			return fmt.Errorf("wait for exact Linux run child adoption: %w", err)
		}
		identity, err := ProcessIdentity(target.PID)
		if err != nil || identity != target.Identity {
			return fmt.Errorf("verify Linux run child identity during adoption: %w", errOr(err, errors.New("creation identity changed")))
		}
		if run.leafRemovedObserved {
			tracked, exists := run.children[target.PID]
			if !exists || !tracked.identity.Equal(target) || tracked.role != role {
				return errors.New("removed-leaf reap target lacks its earlier exact anchor registration")
			}
			if err := verifyLinuxOwnerRunChildCredentials(target.PID, role, run.owner); err != nil {
				return err
			}
		} else if err := verifyLinuxOwnerRunChildMembership(target.PID, role, run.grant, run.leaf); err != nil {
			return err
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
