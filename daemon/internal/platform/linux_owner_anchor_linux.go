//go:build linux && amd64

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

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

// LinuxOwnerAnchorClient speaks only the fixed local protocol. It has no API
// for command/argv submission: the anchor starts only a fixed hidden helper
// mode in the existing symmetry-daemon binary.
type LinuxOwnerAnchorClient struct {
	socketPath string
	owner      linuxgate.OwnerConfig
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
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorTimeoutFor(linuxOwnerAnchorOperationIdentity))
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
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationIdentity,
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
	if len(rights) != 0 || response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != linuxOwnerAnchorOperationIdentity || response.Status != "ready" ||
		response.Anchor.PID != peer.PID || response.Anchor.Identity != peer.Identity || response.Parent.Validate() != nil ||
		response.OwnerUID != client.owner.AUID || response.ManagerPath != response.Parent.Path ||
		response.ManagerPath == "/" ||
		response.RunKey != (LinuxOwnerAnchorRunKey{}) || response.Fence != (protocol.Fence{}) || response.FenceDigest != "" || response.LaunchToken != "" ||
		response.Helper != nil || response.Evidence != nil || len(response.ReapEvidence) != 0 || response.HelperExit != nil ||
		response.NextPID != 0 || response.More || response.CloneRemoval != nil ||
		!linuxOwnerAnchorInManager(peer.CgroupPath, response.ManagerPath) {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerAnchorIdentity{}, errors.New("Linux owner anchor identity response differs from exact peer credentials or fixed configuration")
	}
	return LinuxOwnerAnchorIdentity{
		Process: response.Anchor, Parent: response.Parent, OwnerUID: response.OwnerUID, ManagerPath: response.ManagerPath,
	}, nil
}

// The fixed owner unit places the anchor in the delegated manager subgroup
// itself; helpers and run leaves are created beneath it. Anchors that only
// overlap that path as a parent or child are a different topology.
func linuxOwnerAnchorInManager(anchorPath, managerPath string) bool {
	return anchorPath != "" && anchorPath == managerPath
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
	if err := verifyLinuxOwnerAnchorChildPIDFD(pidfd, process, role); err != nil {
		return fmt.Errorf("verify process pidfd before owner-anchor registration: %w", err)
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRegisterProcess, Grant: grant,
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
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationWaitReap, Grant: grant,
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
	row, _ := linuxOwnerAnchorOperationShapeFor(request.Operation)
	cloneCleanup := request.Operation == linuxOwnerAnchorOperationCloneCleanup
	stopLeaf := request.Operation == linuxOwnerAnchorOperationStopLeaf
	classifyChild := request.Operation == linuxOwnerAnchorOperationClassifyChild
	if row.clientCheckedFDs {
		expectedFDs := row.clientSendFDs
		if request.TargetFDPresent {
			expectedFDs = row.clientSendFDsWithTarget
		}
		if len(sendFDs) != expectedFDs {
			return linuxOwnerAnchorResponse{}, nil, errors.New(row.clientFDsMismatch)
		}
	}
	if cloneCleanup {
		if row.presenceRejected(request) || row.shapeRejected(request) ||
			request.Cleanup.NormalReleaseProof.CgroupIdentity == nil {
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
		if request.Grant.Validate() != nil || row.presenceRejected(request) || row.shapeRejected(request) {
			return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner child-origin request differs from its exact helper/process PIDFD")
		}
		if err := verifyLinuxOwnerAnchorPIDFD(sendFDs[0], *request.Process); err != nil {
			return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("verify exact process pidfd for owner-origin request: %w", err)
		}
		expectedAnchor = request.Grant.Anchor
	} else if stopLeaf {
		if request.Grant.Validate() != nil || row.presenceRejected(request) || row.shapeRejected(request) {
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
	timeout := row.timeoutClass.duration()
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
	if len(duplicates) > linuxOwnerAnchorRequestFDCap {
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

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
