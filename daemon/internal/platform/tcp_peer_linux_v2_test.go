//go:build linux

package platform

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestParseLinuxFDInfoInode(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    uint64
		found   bool
		invalid bool
	}{
		{name: "socket inode", value: "pos:\t0\nflags:\t02000002\nmnt_id:\t11\nino:\t12345\n", want: 12345, found: true},
		{name: "no inode", value: "Pid:\t123\nNSpid:\t123\n"},
		{name: "zero inode", value: "ino:\t0\n", found: true},
		{name: "empty inode", value: "ino:\t\n", invalid: true},
		{name: "non decimal inode", value: "ino:\t12x\n", invalid: true},
		{name: "overflow inode", value: "ino:\t18446744073709551616\n", invalid: true},
		{name: "duplicate inode", value: "ino:\t1\nino:\t2\n", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, found, err := parseLinuxFDInfoInode([]byte(test.value))
			if found != test.found || got != test.want || (err != nil) != test.invalid {
				t.Fatalf("parseLinuxFDInfoInode() = %d, %v, %v; want %d, %v, invalid=%v", got, found, err, test.want, test.found, test.invalid)
			}
		})
	}
}

func TestLinuxTCPPeerV2RejectsDatagramSocket(t *testing.T) {
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen UDP socket: %v", err)
	}
	defer packet.Close()
	systemConnection, ok := packet.(syscall.Conn)
	if !ok {
		t.Fatal("UDP connection does not expose its system socket")
	}
	rawConnection, err := systemConnection.SyscallConn()
	if err != nil {
		t.Fatalf("get UDP socket descriptor: %v", err)
	}
	var socketType int
	var socketTypeErr error
	if controlErr := rawConnection.Control(func(fd uintptr) {
		socketType, socketTypeErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
	}); controlErr != nil {
		t.Fatalf("read UDP socket type: %v", controlErr)
	}
	if socketTypeErr != nil {
		t.Fatalf("read UDP socket type: %v", socketTypeErr)
	}
	if socketType != unix.SOCK_DGRAM {
		t.Fatalf("socket type = %d, want SOCK_DGRAM", socketType)
	}
	var socketErr error
	if err := rawConnection.Control(func(fd uintptr) {
		var stat unix.Stat_t
		if err := unix.Fstat(int(fd), &stat); err != nil {
			socketErr = err
			return
		}
		_, socketErr = linuxPIDFDGetfdSocketMatches(int(fd), stat.Ino, tcpPeerTuple{}, 1)
	}); err != nil {
		t.Fatalf("inspect UDP socket descriptor: %v", err)
	}
	if socketErr == nil {
		t.Fatal("accepted a UDP datagram socket as an IPv4 TCP stream")
	}
}

func TestLinuxTCPPeerV2RejectsDifferentNetworkCookie(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP socket: %v", err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial TCP socket: %v", err)
	}
	defer client.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept TCP socket: %v", err)
	}
	defer accepted.Close()
	systemConnection, ok := client.(syscall.Conn)
	if !ok {
		t.Fatal("TCP connection does not expose its system socket")
	}
	rawConnection, err := systemConnection.SyscallConn()
	if err != nil {
		t.Fatalf("get TCP socket descriptor: %v", err)
	}
	var matchErr error
	if err := rawConnection.Control(func(fd uintptr) {
		observation, err := inspectLinuxTCPSocket(int(fd))
		if err != nil {
			matchErr = err
			return
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(fd), &stat); err != nil {
			matchErr = err
			return
		}
		_, matchErr = linuxPIDFDGetfdSocketMatches(int(fd), stat.Ino, tcpPeerTuple{server: observation.local, client: observation.remote}, observation.networkCookie^1)
	}); err != nil {
		t.Fatalf("inspect TCP socket descriptor: %v", err)
	}
	if errors.Is(matchErr, unix.ENOPROTOOPT) || errors.Is(matchErr, unix.EOPNOTSUPP) {
		t.Skipf("SO_NETNS_COOKIE is unavailable on this Linux host: %v", matchErr)
	}
	if matchErr == nil {
		t.Fatal("accepted a TCP socket with a different network namespace cookie")
	}
}

func TestVerifyLoopbackTCPPeerV2UsesRetainedPIDFDAndPinnedNamespace(t *testing.T) {
	helper := startTCPPeerHelper(t)
	client, err := net.Dial("tcp4", helper.address)
	if err != nil {
		t.Fatalf("dial helper listener: %v", err)
	}
	defer client.Close()
	helper.accept(t)

	retainedPIDFD := openLinuxPeerTestPIDFD(t, helper.pid)
	defer retainedPIDFD.Close()
	userNamespace, err := CaptureLinuxUserNamespaceIdentity(helper.pid, helper.identity, retainedPIDFD)
	if err != nil {
		t.Fatalf("capture A-gate user namespace identity: %v", err)
	}
	if !userNamespace.valid() {
		t.Fatalf("captured invalid user namespace identity: %#v", userNamespace)
	}

	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, os.Getpid(), helper.identity, retainedPIDFD, userNamespace); err == nil {
		t.Fatal("accepted a process identity whose embedded PID does not match the launch PID")
	}
	wrongStartIdentity := helper.identity + "0"
	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, wrongStartIdentity, retainedPIDFD, userNamespace); err == nil {
		t.Fatal("accepted a changed process start identity")
	}
	wrongUserNamespace := userNamespace
	wrongUserNamespace.Inode++
	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, helper.identity, retainedPIDFD, wrongUserNamespace); err == nil {
		t.Fatal("accepted a different pinned user namespace")
	}

	daemonPIDFD := openLinuxPeerTestPIDFD(t, os.Getpid())
	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, helper.identity, daemonPIDFD, userNamespace); err == nil {
		t.Fatal("accepted a pidfd for a different process")
	}
	if err := daemonPIDFD.Close(); err != nil {
		t.Fatalf("close wrong-process pidfd: %v", err)
	}
	closedPIDFD := openLinuxPeerTestPIDFD(t, helper.pid)
	if err := closedPIDFD.Close(); err != nil {
		t.Fatalf("close stale pidfd: %v", err)
	}
	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, helper.identity, closedPIDFD, userNamespace); err == nil {
		t.Fatal("accepted a closed retained pidfd")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyLoopbackTCPPeerV2(cancelled, client, helper.pid, helper.identity, retainedPIDFD, userNamespace); !errors.Is(err, context.Canceled) {
		t.Fatalf("verification with a canceled context = %v, want context.Canceled", err)
	}

	unrelatedListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for unrelated socket: %v", err)
	}
	defer unrelatedListener.Close()
	unrelatedClient, err := net.Dial("tcp4", unrelatedListener.Addr().String())
	if err != nil {
		t.Fatalf("dial unrelated socket: %v", err)
	}
	defer unrelatedClient.Close()
	unrelatedAccepted, err := unrelatedListener.Accept()
	if err != nil {
		t.Fatalf("accept unrelated socket: %v", err)
	}
	defer unrelatedAccepted.Close()
	if err := VerifyLoopbackTCPPeerV2(context.Background(), unrelatedClient, helper.pid, helper.identity, retainedPIDFD, userNamespace); err == nil {
		t.Fatal("accepted a different socket held only by the verifier process")
	}

	// A denied pidfd_getfd, absent kernel primitive, or missing socket-cookie
	// support is a supported fail-closed result; none may turn into v1 ownership.
	verifyErr := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, helper.identity, retainedPIDFD, userNamespace)
	if verifyErr != nil && !linuxTCPPeerV2UnavailableOrDenied(verifyErr) {
		t.Fatalf("VerifyLoopbackTCPPeerV2() error = %v", verifyErr)
	}

	helper.stop(t)
	if err := VerifyLoopbackTCPPeerV2(context.Background(), client, helper.pid, helper.identity, retainedPIDFD, userNamespace); err == nil {
		t.Fatal("accepted an exited process through its retained pidfd")
	}
}

func TestVerifyLoopbackTCPPeerV2WaitsForPendingAcceptOnSameConnectionWithoutWriting(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for pending TCP accept: %v", err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("connect before server accept: %v", err)
	}
	defer client.Close()

	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("capture test-owned process identity: %v", err)
	}
	retainedPIDFD := openLinuxPeerTestPIDFD(t, os.Getpid())
	defer retainedPIDFD.Close()
	userNamespace, err := CaptureLinuxUserNamespaceIdentity(os.Getpid(), identity, retainedPIDFD)
	if err != nil {
		t.Fatalf("capture pinned test-owned user namespace: %v", err)
	}
	rawConnection, err := client.(syscall.Conn).SyscallConn()
	if err != nil {
		t.Fatalf("get connected TCP socket: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pending := make(chan struct{})
	accepted := make(chan struct{})
	observed := &pendingAcceptTCPPeerRawConn{RawConn: rawConnection, ctx: ctx, pending: pending, accepted: accepted}
	connection := pendingAcceptTCPPeerConn{Conn: client, raw: observed}
	verified := make(chan error, 1)
	go func() {
		verified <- VerifyLoopbackTCPPeerV2(ctx, connection, os.Getpid(), identity, retainedPIDFD, userNamespace)
	}()
	select {
	case <-pending:
		// The first absent-inode/FD scan has completed, but accept is still gated.
	case verifyErr := <-verified:
		if verifyErr != nil && linuxTCPPeerV2UnavailableOrDenied(verifyErr) {
			t.Skipf("Linux v2 peer primitives are unavailable before accept: %v", verifyErr)
		}
		t.Fatalf("verification returned before the pending accept could install its FD: %v", verifyErr)
	case <-ctx.Done():
		t.Fatal("verification did not observe the pending accept within its caller bound")
	}
	server, err := listener.Accept()
	if err != nil {
		t.Fatalf("install accepted peer FD: %v", err)
	}
	defer server.Close()
	if server.LocalAddr().String() != client.RemoteAddr().String() || server.RemoteAddr().String() != client.LocalAddr().String() {
		t.Fatal("accepted socket is not the original connected TCP peer")
	}
	close(accepted)
	var verifyErr error
	select {
	case verifyErr = <-verified:
	case <-ctx.Done():
		t.Fatal("verification did not observe FD installation on the original connection")
	}
	if verifyErr != nil && !linuxTCPPeerV2UnavailableOrDenied(verifyErr) {
		t.Fatalf("verify original connection after accept: %v", verifyErr)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close verified client: %v", err)
	}
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("bound zero-write inspection: %v", err)
	}
	var buffer [1]byte
	if count, readErr := server.Read(buffer[:]); count != 0 || !errors.Is(readErr, io.EOF) {
		t.Fatalf("peer read after verification = %d, %v; want EOF without application bytes", count, readErr)
	}
	if verifyErr != nil {
		t.Skipf("pending accept was observed, but this host denies or lacks Linux v2 socket possession proof: %v", verifyErr)
	}
}

type pendingAcceptTCPPeerConn struct {
	net.Conn
	raw syscall.RawConn
}

func (connection pendingAcceptTCPPeerConn) SyscallConn() (syscall.RawConn, error) {
	return connection.raw, nil
}

type pendingAcceptTCPPeerRawConn struct {
	syscall.RawConn
	ctx      context.Context
	pending  chan<- struct{}
	accepted <-chan struct{}
	controls int
}

func (connection *pendingAcceptTCPPeerRawConn) Control(inspect func(uintptr)) error {
	err := connection.RawConn.Control(inspect)
	connection.controls++
	if err != nil || connection.controls != 2 {
		return err
	}
	// Keep the real socket inspection; gate accept only after its first recheck.
	close(connection.pending)
	select {
	case <-connection.accepted:
		return nil
	case <-connection.ctx.Done():
		return connection.ctx.Err()
	}
}

func openLinuxPeerTestPIDFD(t *testing.T, pid int) *os.File {
	t.Helper()
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			t.Skipf("pidfd_open is unavailable on this Linux host: %v", err)
		}
		t.Fatalf("open test pidfd for PID %d: %v", pid, err)
	}
	file := os.NewFile(uintptr(fd), "test-retained-pidfd")
	if file == nil {
		_ = unix.Close(fd)
		t.Fatal("os.NewFile returned nil for pidfd")
	}
	return file
}

func linuxTCPPeerV2UnavailableOrDenied(err error) bool {
	return errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) ||
		errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.ENOPROTOOPT) || errors.Is(err, unix.EOPNOTSUPP) ||
		strings.Contains(err.Error(), "permission denied")
}
