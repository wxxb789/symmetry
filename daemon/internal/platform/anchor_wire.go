//go:build linux && amd64

package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

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
		return linuxOwnerAnchorPeer{}, fmt.Errorf("Linux owner anchor peer identity changed during authentication: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
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
