//go:build linux && amd64

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

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

// protocolFence is a local alias which keeps the v2 IPC's exported evidence
// independent of the state package while retaining the exact durable fence.
type protocolFence = protocol.Fence

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
	Path  string                          `json:"path"`
	Args  []string                        `json:"args"`
	Env   []string                        `json:"env"`
	Dir   string                          `json:"dir"`
	Clone *authority.LinuxCloneGrant      `json:"clone,omitempty"`
	Reuse *authority.LinuxCloneReuseGrant `json:"reuse,omitempty"`
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

func linuxOwnerHelperV2CloneGrantMatchesRun(command linuxgate.NativeCommand, grant LinuxOwnerAnchorGrant, target string) bool {
	if command.Clone != nil && command.Reuse != nil {
		return false
	}
	if command.Clone != nil {
		return command.Clone.Validate() == nil && command.Clone.Intent.RunID == grant.RunKey.RunID &&
			command.Clone.Intent.Generation == grant.RunKey.Generation && command.Clone.Intent.Target == target
	}
	return command.Reuse != nil && command.Reuse.Validate() == nil &&
		command.Reuse.RunID == grant.RunKey.RunID && command.Reuse.Generation == grant.RunKey.Generation &&
		command.Reuse.SourceCheckout.Target == target
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
		return linuxOwnerAnchorPeer{}, fmt.Errorf("Linux owner helper peer identity changed: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
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

func linuxOwnerHelperV2ResponseError(response linuxOwnerHelperV2Response) error {
	if response.Status == "recovery_only" || response.ErrorCode == "owner_lost" {
		return ErrLinuxOwnerHelperV2RecoveryOnly
	}
	if response.Operation == "release" && response.Status == "unresolved" && response.StopEvidence != nil {
		copy := *response.StopEvidence
		return &LinuxOwnerHelperV2ReleaseUnresolvedError{Code: response.ErrorCode, Evidence: &copy}
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

func peerProcessEqual(peer linuxOwnerAnchorPeer, expected authority.LinuxCgroupProcessIdentity) bool {
	return expected.Validate() == nil && peer.PID == expected.PID && peer.Identity == expected.Identity
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
