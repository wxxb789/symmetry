//go:build linux

package platform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxTCPPeerFDLimit       = 1048576
	linuxTCPPeerFDInfoMaxSize = 64 * 1024
	linuxTCPPeerV2Timeout     = 5 * time.Second
)

func (identity LinuxUserNamespaceIdentity) valid() bool {
	return identity.Device != 0 && identity.Inode != 0
}

// CaptureLinuxUserNamespaceIdentity captures the user namespace of the exact
// launched process while it is still at the A gate. retainedPIDFD is borrowed;
// it must remain open and unmodified for this call. The verifier duplicates
// the descriptor and closes only its duplicate.
func CaptureLinuxUserNamespaceIdentity(pid int, identity string, retainedPIDFD *os.File) (LinuxUserNamespaceIdentity, error) {
	if err := validateLinuxPeerIdentity(pid, identity); err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	pidfd, err := duplicateLinuxPeerPIDFD(retainedPIDFD)
	if err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	defer unix.Close(pidfd)
	runtime.KeepAlive(retainedPIDFD)

	if err := verifyLinuxLaunchedProcess(pidfd, pid, identity); err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	expected, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	if err := verifyLinuxLaunchedProcess(pidfd, pid, identity); err != nil {
		return LinuxUserNamespaceIdentity{}, err
	}
	return expected, nil
}

// VerifyLoopbackTCPPeerV2 verifies possession of the exact connected IPv4
// loopback socket by the process named by the launch-retained pidfd. The
// expected user namespace must have been captured from the A gate before
// RELEASE. retainedPIDFD is borrowed: callers must keep it open and must not
// close or replace it until this function returns. conn must likewise remain
// open for the call. The function immediately duplicates the pidfd with
// close-on-exec and never opens a new pidfd from the numeric PID.
// Pending accept/FD installation is observed on that same connection within
// the five-second or shorter caller context bound, without redialing or
// sending application bytes. Permission and identity errors are not retried.
//
// Linux pidfd_getfd permission, procfs fdinfo visibility, pidfds,
// SO_NETNS_COOKIE, and IPv4 loopback TCP are required. Missing kernel support,
// procfs access, or permission returns an error; there is no identity/UID,
// nonce, port, or fdinfo-inode fallback. This proves exact socket-FD possession
// at inspection time, not authorship of all bytes sent by the shared native UID.
func VerifyLoopbackTCPPeerV2(ctx context.Context, conn net.Conn, launchedPID int, launchedIdentity string, retainedPIDFD *os.File, expectedUserNS LinuxUserNamespaceIdentity) error {
	if ctx == nil || conn == nil || launchedPID <= 0 || retainedPIDFD == nil || !expectedUserNS.valid() {
		return errors.New("v2 TCP peer verification requires context, connection, launched PID, pidfd and pinned user namespace")
	}
	if err := validateLinuxPeerIdentity(launchedPID, launchedIdentity); err != nil {
		return err
	}
	verificationContext, cancel := context.WithTimeout(ctx, linuxTCPPeerV2Timeout)
	defer cancel()
	if err := verificationContext.Err(); err != nil {
		return fmt.Errorf("verify v2 TCP peer: %w", err)
	}

	pidfd, err := duplicateLinuxPeerPIDFD(retainedPIDFD)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	runtime.KeepAlive(retainedPIDFD)

	networkNamespace, err := verifyLinuxPeerV2Process(pidfd, launchedPID, launchedIdentity, expectedUserNS, nil)
	if err != nil {
		return err
	}
	if err := verificationContext.Err(); err != nil {
		return fmt.Errorf("verify v2 TCP peer: %w", err)
	}
	tuple, networkCookie, rawConnection, err := linuxTCPPeerConnectionObservation(conn)
	if err != nil {
		return err
	}
	// connect may finish before accept installs the peer FD. Only an absent
	// inode/FD is pending; permission, identity and socket errors remain fatal.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := verificationContext.Err(); err != nil {
			return fmt.Errorf("verify v2 TCP peer: %w", err)
		}
		inode, err := linuxTCPPeerInode(verificationContext, tuple)
		if err != nil {
			return fmt.Errorf("read exact TCP peer inode: %w", err)
		}
		owned := false
		if inode != 0 {
			owned, err = linuxProcessOwnsSocketByPIDFD(verificationContext, pidfd, launchedPID, inode, tuple, networkCookie)
			if err != nil {
				return err
			}
		}
		if err := verificationContext.Err(); err != nil {
			return fmt.Errorf("verify v2 TCP peer: %w", err)
		}
		if owned {
			currentInode, err := linuxTCPPeerInode(verificationContext, tuple)
			if err != nil {
				return fmt.Errorf("recheck exact TCP peer inode: %w", err)
			}
			if currentInode != inode {
				return errors.New("exact TCP peer inode changed during verification")
			}
		}
		currentTuple, currentCookie, err := linuxTCPPeerRawConnectionObservation(rawConnection)
		if err != nil {
			return err
		}
		if currentTuple != tuple || currentCookie != networkCookie {
			return errors.New("accepted TCP connection changed during verification")
		}
		if _, err := verifyLinuxPeerV2Process(pidfd, launchedPID, launchedIdentity, expectedUserNS, networkNamespace); err != nil {
			return err
		}
		if err := verificationContext.Err(); err != nil {
			return fmt.Errorf("verify v2 TCP peer: %w", err)
		}
		if owned {
			return nil
		}
		select {
		case <-verificationContext.Done():
			return fmt.Errorf("verify v2 TCP peer: %w", verificationContext.Err())
		case <-ticker.C:
		}
		if err := verificationContext.Err(); err != nil {
			return fmt.Errorf("verify v2 TCP peer: %w", err)
		}
		if _, err := verifyLinuxPeerV2Process(pidfd, launchedPID, launchedIdentity, expectedUserNS, networkNamespace); err != nil {
			return err
		}
	}
}

func inspectLoopbackTCPPeer(ctx context.Context, tuple tcpPeerTuple, pid int, identity string) (bool, error) {
	namespace, err := verifyLinuxPeerProcess(pid, identity, nil)
	if err != nil {
		return false, err
	}
	inode, err := linuxTCPPeerInode(ctx, tuple)
	if err != nil || inode == 0 {
		return false, err
	}
	owned, err := linuxProcessOwnsSocket(ctx, pid, inode)
	if err != nil || !owned {
		return false, err
	}
	// Re-read the connection after checking the FD: a recycled listener port or
	// stale inode must not turn a different connection into ownership evidence.
	current, err := linuxTCPPeerInode(ctx, tuple)
	if err != nil {
		return false, err
	}
	if _, err := verifyLinuxPeerProcess(pid, identity, namespace); err != nil {
		return false, err
	}
	return current == inode, nil
}

func verifyLinuxPeerProcess(pid int, identity string, previous os.FileInfo) (os.FileInfo, error) {
	actual, err := ProcessIdentity(pid)
	if err != nil {
		return nil, err
	}
	if actual != identity {
		return nil, errors.New("TCP peer process identity changed")
	}
	local, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("read local network namespace: %w", err)
	}
	peer, err := os.Stat(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, fmt.Errorf("read peer network namespace: %w", err)
	}
	if !os.SameFile(local, peer) || previous != nil && !os.SameFile(previous, peer) {
		return nil, errors.New("TCP peer network namespace does not match")
	}
	return peer, nil
}

func linuxTCPPeerInode(ctx context.Context, tuple tcpPeerTuple) (uint64, error) {
	file, err := os.Open("/proc/self/net/tcp")
	if err != nil {
		return 0, fmt.Errorf("open TCP ownership table: %w", err)
	}
	defer file.Close()
	return readLinuxTCPPeerInode(ctx, file, tuple)
}

func readLinuxTCPPeerInode(ctx context.Context, reader io.Reader, tuple tcpPeerTuple) (uint64, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4096)
	if !scanner.Scan() {
		return 0, errors.New("TCP ownership table has no header")
	}
	header := strings.Fields(scanner.Text())
	if len(header) < 4 || header[1] != "local_address" || header[2] != "rem_address" || header[3] != "st" {
		return 0, errors.New("TCP ownership table has an invalid header")
	}
	var matched uint64
	for row := 0; scanner.Scan(); row++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if row >= 262144 {
			return 0, errors.New("TCP ownership table exceeds row limit")
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			return 0, errors.New("TCP ownership table has a truncated row")
		}
		local, localErr := parseLinuxTCPAddress(fields[1])
		remote, remoteErr := parseLinuxTCPAddress(fields[2])
		state, stateErr := strconv.ParseUint(fields[3], 16, 8)
		inode, inodeErr := strconv.ParseUint(fields[9], 10, 64)
		if localErr != nil || remoteErr != nil || stateErr != nil || inodeErr != nil {
			return 0, errors.New("TCP ownership table has an invalid row")
		}
		if state == 1 && inode != 0 && local == tuple.server && remote == tuple.client {
			if matched != 0 && matched != inode {
				return 0, errors.New("TCP ownership table has ambiguous peer inodes")
			}
			matched = inode
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read TCP ownership table: %w", err)
	}
	return matched, ctx.Err()
}

func parseLinuxTCPAddress(value string) (netip.AddrPort, error) {
	if len(value) != 13 || value[8] != ':' {
		return netip.AddrPort{}, errors.New("invalid TCP table address")
	}
	address, err := strconv.ParseUint(value[:8], 16, 32)
	if err != nil {
		return netip.AddrPort{}, err
	}
	port, err := strconv.ParseUint(value[9:], 16, 16)
	if err != nil {
		return netip.AddrPort{}, err
	}
	var ip [4]byte
	binary.NativeEndian.PutUint32(ip[:], uint32(address))
	return netip.AddrPortFrom(netip.AddrFrom4(ip), uint16(port)), nil
}

func linuxProcessOwnsSocket(ctx context.Context, pid int, inode uint64) (bool, error) {
	path := fmt.Sprintf("/proc/%d/fd", pid)
	directory, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open TCP peer process descriptors: %w", err)
	}
	defer directory.Close()
	want := fmt.Sprintf("socket:[%d]", inode)
	for count := 0; ; {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		names, readErr := directory.Readdirnames(256)
		for _, name := range names {
			count++
			if count > 1048576 {
				return false, errors.New("TCP peer process exceeds descriptor limit")
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			target, err := os.Readlink(path + "/" + name)
			if errors.Is(err, os.ErrNotExist) {
				continue // The process may close unrelated descriptors during the scan.
			}
			if err != nil {
				return false, fmt.Errorf("read TCP peer descriptor: %w", err)
			}
			if target == want {
				return true, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
		if readErr != nil {
			return false, fmt.Errorf("read TCP peer process descriptors: %w", readErr)
		}
	}
}

func validateLinuxPeerIdentity(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("v2 TCP peer verification requires a positive PID and strict process identity")
	}
	parsed, err := parseLinuxProcessIdentity(identity)
	if err != nil || parsed.version != linuxProcessIdentityVersion || parsed.pid != pid {
		if err != nil {
			return fmt.Errorf("v2 TCP peer requires a strict Linux v2 process identity: %w", err)
		}
		return errors.New("v2 TCP peer process identity is not strict Linux v2 for the launched PID")
	}
	return nil
}

func duplicateLinuxPeerPIDFD(file *os.File) (int, error) {
	if file == nil {
		return -1, errors.New("v2 TCP peer verification requires the launch-retained pidfd")
	}
	fd := file.Fd()
	if fd == ^uintptr(0) || fd > uintptr(^uint32(0)>>1) {
		runtime.KeepAlive(file)
		return -1, errors.New("launch-retained pidfd is closed or invalid")
	}
	duplicate, err := unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
	runtime.KeepAlive(file)
	if err != nil {
		return -1, fmt.Errorf("duplicate launch-retained pidfd: %w", err)
	}
	return duplicate, nil
}

func verifyLinuxLaunchedProcess(pidfd, pid int, identity string) error {
	if err := verifyLinuxPeerPIDFD(pidfd, pid); err != nil {
		return err
	}
	actual, err := ProcessIdentity(pid)
	if err != nil {
		return fmt.Errorf("read launched TCP peer process identity: %w", err)
	}
	if actual != identity {
		return errors.New("launched TCP peer process identity changed")
	}
	return verifyLinuxPeerPIDFD(pidfd, pid)
}

// ErrLinuxPeerPIDFDMismatch means the retained pidfd names a different process
// than the launch identity, before any cross-process socket inspection.
var ErrLinuxPeerPIDFDMismatch = errors.New("launch-retained pidfd does not name the launched process")

func verifyLinuxPeerPIDFD(pidfd, pid int) error {
	if err := linuxPIDFDIsLive(pidfd); err != nil {
		return err
	}
	actualPID, err := linuxPIDFDProcessPID(pidfd)
	if err != nil {
		return err
	}
	if actualPID != pid {
		return fmt.Errorf("%w: PID %d, not launched PID %d", ErrLinuxPeerPIDFDMismatch, actualPID, pid)
	}
	if err := linuxPIDFDIsLive(pidfd); err != nil {
		return err
	}
	return nil
}

func linuxPIDFDIsLive(pidfd int) error {
	var descriptors = [1]unix.PollFd{{Fd: int32(pidfd), Events: int16(unix.POLLIN)}}
	for {
		descriptors[0].Revents = 0
		_, err := unix.Poll(descriptors[:], 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("poll launch-retained pidfd: %w", err)
		}
		if descriptors[0].Revents != 0 {
			return errors.New("launch-retained pidfd target has exited or is no longer valid")
		}
		return nil
	}
}

func linuxPIDFDProcessPID(pidfd int) (int, error) {
	file, err := os.Open(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd))
	if err != nil {
		return 0, fmt.Errorf("read launch-retained pidfd info: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 256), 4096)
	var actualPID int
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Pid:") {
			continue
		}
		if found {
			return 0, errors.New("launch-retained descriptor has duplicate pidfd PID fields")
		}
		value, parseErr := strconv.Atoi(strings.TrimSpace(line[len("Pid:"):]))
		if parseErr != nil || value <= 0 {
			return 0, errors.New("launch-retained pidfd is not live")
		}
		actualPID = value
		found = true
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read launch-retained pidfd info: %w", err)
	}
	if !found {
		return 0, errors.New("launch-retained descriptor is not a pidfd")
	}
	return actualPID, nil
}

func readLinuxUserNamespaceIdentity(pid int) (LinuxUserNamespaceIdentity, error) {
	if pid <= 0 {
		return LinuxUserNamespaceIdentity{}, errors.New("user namespace lookup requires a positive PID")
	}
	var stat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/user", pid), &stat); err != nil {
		return LinuxUserNamespaceIdentity{}, fmt.Errorf("stat launched process user namespace: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return LinuxUserNamespaceIdentity{}, errors.New("launched process user namespace is not an accessible nsfs inode")
	}
	identity := LinuxUserNamespaceIdentity{Device: stat.Dev, Inode: stat.Ino}
	if !identity.valid() {
		return LinuxUserNamespaceIdentity{}, errors.New("launched process user namespace identity is invalid")
	}
	return identity, nil
}

func verifyLinuxPeerV2Process(pidfd, pid int, identity string, expectedUserNS LinuxUserNamespaceIdentity, previousNetworkNS os.FileInfo) (os.FileInfo, error) {
	if err := verifyLinuxPeerPIDFD(pidfd, pid); err != nil {
		return nil, err
	}
	networkNS, err := verifyLinuxPeerProcess(pid, identity, previousNetworkNS)
	if err != nil {
		return nil, err
	}
	userNS, err := readLinuxUserNamespaceIdentity(pid)
	if err != nil {
		return nil, err
	}
	if userNS != expectedUserNS {
		return nil, errors.New("launched TCP peer user namespace does not match the A-gate namespace")
	}
	if err := verifyLinuxPeerPIDFD(pidfd, pid); err != nil {
		return nil, err
	}
	return networkNS, nil
}

func linuxTCPPeerConnectionObservation(connection net.Conn) (tcpPeerTuple, uint64, syscall.RawConn, error) {
	localAddress, localOK := connection.LocalAddr().(*net.TCPAddr)
	remoteAddress, remoteOK := connection.RemoteAddr().(*net.TCPAddr)
	if !localOK || !remoteOK || localAddress == nil || remoteAddress == nil {
		return tcpPeerTuple{}, 0, nil, errors.New("v2 TCP peer verification requires a live TCP connection")
	}
	local, err := linuxTCPAddrPort(localAddress)
	if err != nil {
		return tcpPeerTuple{}, 0, nil, err
	}
	remote, err := linuxTCPAddrPort(remoteAddress)
	if err != nil {
		return tcpPeerTuple{}, 0, nil, err
	}
	if !local.Addr().Is4() || !local.Addr().IsLoopback() || !remote.Addr().Is4() || !remote.Addr().IsLoopback() {
		return tcpPeerTuple{}, 0, nil, errors.New("v2 TCP peer verification supports only IPv4 loopback connections")
	}

	systemConnection, ok := connection.(syscall.Conn)
	if !ok {
		return tcpPeerTuple{}, 0, nil, errors.New("v2 TCP peer verification requires a syscall-backed TCP connection")
	}
	rawConnection, err := systemConnection.SyscallConn()
	if err != nil {
		return tcpPeerTuple{}, 0, nil, fmt.Errorf("get accepted TCP connection descriptor: %w", err)
	}
	if rawConnection == nil {
		return tcpPeerTuple{}, 0, nil, errors.New("accepted TCP connection has no live raw descriptor")
	}
	observation, networkCookie, err := linuxTCPPeerRawConnectionObservation(rawConnection)
	if err != nil {
		return tcpPeerTuple{}, 0, nil, err
	}
	tuple := tcpPeerTuple{server: remote, client: local}
	if observation != tuple {
		return tcpPeerTuple{}, 0, nil, errors.New("accepted TCP connection addresses do not match its live socket")
	}
	return tuple, networkCookie, rawConnection, nil
}

func linuxTCPAddrPort(address *net.TCPAddr) (netip.AddrPort, error) {
	if address == nil || address.Port <= 0 || address.Port > 65535 {
		return netip.AddrPort{}, errors.New("v2 TCP peer verification requires valid TCP ports")
	}
	result := address.AddrPort()
	result = netip.AddrPortFrom(result.Addr().Unmap(), result.Port())
	if !result.IsValid() || result.Port() == 0 {
		return netip.AddrPort{}, errors.New("v2 TCP peer verification requires valid TCP addresses")
	}
	return result, nil
}

type linuxTCPSocketObservation struct {
	local         netip.AddrPort
	remote        netip.AddrPort
	networkCookie uint64
}

func linuxTCPPeerRawConnectionObservation(connection syscall.RawConn) (tcpPeerTuple, uint64, error) {
	var observation linuxTCPSocketObservation
	var observationErr error
	if err := connection.Control(func(fd uintptr) {
		observation, observationErr = inspectLinuxTCPSocket(int(fd))
	}); err != nil {
		return tcpPeerTuple{}, 0, fmt.Errorf("inspect live TCP connection: %w", err)
	}
	if observationErr != nil {
		return tcpPeerTuple{}, 0, fmt.Errorf("inspect live TCP connection: %w", observationErr)
	}
	return tcpPeerTuple{server: observation.remote, client: observation.local}, observation.networkCookie, nil
}

func inspectLinuxTCPSocket(fd int) (linuxTCPSocketObservation, error) {
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket domain: %w", err)
	}
	typeValue, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket type: %w", err)
	}
	protocol, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket protocol: %w", err)
	}
	if domain != unix.AF_INET || typeValue != unix.SOCK_STREAM || protocol != unix.IPPROTO_TCP {
		return linuxTCPSocketObservation{}, errors.New("socket is not an IPv4 TCP stream")
	}
	localSockaddr, err := unix.Getsockname(fd)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket local address: %w", err)
	}
	remoteSockaddr, err := unix.Getpeername(fd)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket peer address: %w", err)
	}
	local, err := linuxSockaddrIPv4AddrPort(localSockaddr)
	if err != nil {
		return linuxTCPSocketObservation{}, err
	}
	remote, err := linuxSockaddrIPv4AddrPort(remoteSockaddr)
	if err != nil {
		return linuxTCPSocketObservation{}, err
	}
	if !local.Addr().IsLoopback() || !remote.Addr().IsLoopback() {
		return linuxTCPSocketObservation{}, errors.New("socket is not connected over IPv4 loopback")
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil {
		return linuxTCPSocketObservation{}, fmt.Errorf("read TCP socket network namespace cookie: %w", err)
	}
	if cookie == 0 {
		return linuxTCPSocketObservation{}, errors.New("TCP socket network namespace cookie is zero")
	}
	return linuxTCPSocketObservation{local: local, remote: remote, networkCookie: cookie}, nil
}

func linuxSockaddrIPv4AddrPort(address unix.Sockaddr) (netip.AddrPort, error) {
	inet, ok := address.(*unix.SockaddrInet4)
	if !ok || inet == nil || inet.Port <= 0 || inet.Port > 65535 {
		return netip.AddrPort{}, errors.New("TCP socket does not have a valid IPv4 address")
	}
	return netip.AddrPortFrom(netip.AddrFrom4(inet.Addr), uint16(inet.Port)), nil
}

func linuxProcessOwnsSocketByPIDFD(ctx context.Context, pidfd, pid int, inode uint64, tuple tcpPeerTuple, networkCookie uint64) (bool, error) {
	directory, err := os.Open(fmt.Sprintf("/proc/%d/fdinfo", pid))
	if err != nil {
		return false, fmt.Errorf("open launched TCP peer fdinfo directory: %w", err)
	}
	defer directory.Close()
	directoryFD := int(directory.Fd())
	for count := 0; ; {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		names, readErr := directory.Readdirnames(256)
		for _, name := range names {
			count++
			if count > linuxTCPPeerFDLimit {
				return false, errors.New("launched TCP peer exceeds fdinfo scan limit")
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			targetFD, parseErr := parseLinuxFDInfoDescriptor(name)
			if parseErr != nil {
				return false, errors.New("launched TCP peer fdinfo directory contains an invalid descriptor name")
			}
			candidateInode, infoErr := linuxProcessFDInfoInodeAt(ctx, directoryFD, name)
			if errors.Is(infoErr, os.ErrNotExist) || errors.Is(infoErr, unix.ENOENT) {
				continue // A descriptor may close while the bounded fdinfo scan runs.
			}
			if infoErr != nil {
				return false, infoErr
			}
			if candidateInode != inode {
				continue
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			duplicate, err := unix.PidfdGetfd(pidfd, targetFD, 0)
			if err != nil {
				return false, fmt.Errorf("duplicate exact TCP peer socket with pidfd_getfd: %w", err)
			}
			valid, socketErr := linuxPIDFDGetfdSocketMatches(duplicate, inode, tuple, networkCookie)
			closeErr := unix.Close(duplicate)
			if socketErr != nil {
				return false, socketErr
			}
			if closeErr != nil {
				return false, fmt.Errorf("close duplicated TCP peer socket: %w", closeErr)
			}
			return valid, nil
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
		if readErr != nil {
			return false, fmt.Errorf("read launched TCP peer fdinfo directory: %w", readErr)
		}
	}
}

func parseLinuxFDInfoDescriptor(name string) (int, error) {
	if len(name) == 0 || len(name) > 1 && name[0] == '0' {
		return 0, errors.New("fdinfo descriptor is not canonical decimal")
	}
	maximum := int(^uint(0) >> 1)
	value := 0
	for _, digit := range name {
		if digit < '0' || digit > '9' {
			return 0, errors.New("fdinfo descriptor is not decimal")
		}
		next := int(digit - '0')
		if value > (maximum-next)/10 {
			return 0, errors.New("fdinfo descriptor overflows int")
		}
		value = value*10 + next
	}
	return value, nil
}

func linuxProcessFDInfoInodeAt(ctx context.Context, directoryFD int, name string) (uint64, error) {
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, fmt.Errorf("open launched TCP peer fdinfo: %w", err)
	}
	defer unix.Close(fd)
	var contents [linuxTCPPeerFDInfoMaxSize + 1]byte
	read := 0
	for read < len(contents) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		count, readErr := unix.Read(fd, contents[read:])
		read += count
		if errors.Is(readErr, unix.EINTR) {
			continue
		}
		if readErr != nil {
			return 0, fmt.Errorf("read launched TCP peer fdinfo: %w", readErr)
		}
		if count == 0 {
			break
		}
	}
	if read > linuxTCPPeerFDInfoMaxSize {
		return 0, errors.New("launched TCP peer fdinfo entry exceeds size limit")
	}
	inode, found, err := parseLinuxFDInfoInode(contents[:read])
	if err != nil {
		return 0, fmt.Errorf("parse launched TCP peer fdinfo: %w", err)
	}
	if !found {
		return 0, nil
	}
	return inode, nil
}

func parseLinuxFDInfoInode(contents []byte) (uint64, bool, error) {
	found := false
	var inode uint64
	for len(contents) > 0 {
		line := contents
		if newline := bytes.IndexByte(contents, '\n'); newline >= 0 {
			line = contents[:newline]
			contents = contents[newline+1:]
		} else {
			contents = nil
		}
		if !bytes.HasPrefix(line, []byte("ino:")) {
			continue
		}
		if found {
			return 0, false, errors.New("fdinfo contains duplicate inode fields")
		}
		value := line[len("ino:"):]
		for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
			value = value[1:]
		}
		if len(value) == 0 {
			return 0, false, errors.New("fdinfo inode is empty")
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return 0, false, errors.New("fdinfo inode is not canonical decimal")
			}
			next := uint64(digit - '0')
			if inode > (^uint64(0)-next)/10 {
				return 0, false, errors.New("fdinfo inode overflows uint64")
			}
			inode = inode*10 + next
		}
		found = true
	}
	return inode, found, nil
}

func linuxPIDFDGetfdSocketMatches(fd int, inode uint64, tuple tcpPeerTuple, networkCookie uint64) (bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, fmt.Errorf("stat duplicated TCP peer socket: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino != inode {
		return false, errors.New("pidfd_getfd candidate is not the exact socket inode")
	}
	observation, err := inspectLinuxTCPSocket(fd)
	if err != nil {
		return false, err
	}
	if observation.local != tuple.server || observation.remote != tuple.client {
		return false, errors.New("pidfd_getfd socket does not match the exact reversed TCP tuple")
	}
	if observation.networkCookie != networkCookie {
		return false, errors.New("pidfd_getfd socket network namespace does not match the accepted connection")
	}
	return true, nil
}
