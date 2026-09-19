//go:build linux

package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadLinuxTCPPeerInode(t *testing.T) {
	tuple := tcpPeerTuple{
		server: netip.MustParseAddrPort("127.0.0.1:4321"),
		client: netip.MustParseAddrPort("127.0.0.1:54321"),
	}
	const header = "  sl  local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
	row := func(local, remote netip.AddrPort, state string, inode string) string {
		encode := func(address netip.AddrPort) string {
			ip := address.Addr().As4()
			return fmt.Sprintf("%08X:%04X", binary.NativeEndian.Uint32(ip[:]), address.Port())
		}
		return fmt.Sprintf("0: %s %s %s 00000000:00000000 00:00000000 00000000 1000 0 %s 1\n", encode(local), encode(remote), state, inode)
	}
	valid := row(tuple.server, tuple.client, "01", "1234")
	for _, test := range []struct {
		name    string
		body    string
		want    uint64
		invalid bool
	}{
		{name: "accepted peer", body: valid, want: 1234},
		{name: "opposite direction", body: row(tuple.client, tuple.server, "01", "1234")},
		{name: "different client port", body: row(tuple.server, netip.MustParseAddrPort("127.0.0.1:54322"), "01", "1234")},
		{name: "listener is not peer", body: row(tuple.server, tuple.client, "0A", "1234")},
		{name: "time wait is not peer", body: row(tuple.server, tuple.client, "06", "1234")},
		{name: "not accepted", body: row(tuple.server, tuple.client, "01", "0")},
		{name: "empty table"},
		{name: "truncated", body: "0: 0100007F:10E1\n", invalid: true},
		{name: "invalid address", body: strings.Replace(valid, ":10E1", ":XXXX", 1), invalid: true},
		{name: "invalid inode", body: row(tuple.server, tuple.client, "01", "-1"), invalid: true},
		{name: "invalid state", body: row(tuple.server, tuple.client, "GG", "1234"), invalid: true},
		{name: "oversize line", body: strings.Repeat("x", 4097), invalid: true},
		{name: "ambiguous inode", body: valid + row(tuple.server, tuple.client, "01", "5678"), invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := readLinuxTCPPeerInode(context.Background(), strings.NewReader(header+test.body), tuple)
			if got != test.want || (err != nil) != test.invalid {
				t.Fatalf("readLinuxTCPPeerInode() = %d, %v; want %d, invalid=%v", got, err, test.want, test.invalid)
			}
		})
	}
	for _, value := range []string{"", "invalid header\n"} {
		if _, err := readLinuxTCPPeerInode(context.Background(), strings.NewReader(value), tuple); err == nil {
			t.Fatalf("accepted invalid header %q", value)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readLinuxTCPPeerInode(ctx, strings.NewReader(header+valid), tuple); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}

func TestReadLinuxTCPPeerInodeBoundsTable(t *testing.T) {
	const header = "sl local_address rem_address st\n"
	const row = "0: 00000000:0000 00000000:0000 01 0:0 0:0 0 0 0 0\n"
	_, err := readLinuxTCPPeerInode(context.Background(), strings.NewReader(header+strings.Repeat(row, 262145)), tcpPeerTuple{})
	if err == nil || !strings.Contains(err.Error(), "row limit") {
		t.Fatalf("oversize table error = %v", err)
	}
}

func TestVerifyLoopbackTCPPeerWaitsForDeferredAcceptWithoutWriting(t *testing.T) {
	listener := listenWithDeferredAccept(t)
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan acceptResult, 1)
	go func() {
		connection, err := listener.Accept()
		accepted <- acceptResult{connection: connection, err: err}
	}()

	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial deferred-accept listener: %v", err)
	}
	clientClosed := false
	t.Cleanup(func() {
		if !clientClosed {
			_ = client.Close()
		}
	})

	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	verifyContext, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := VerifyLoopbackTCPPeer(verifyContext, client, os.Getpid(), identity); err != nil {
		t.Fatalf("VerifyLoopbackTCPPeer() error = %v", err)
	}

	var server net.Conn
	select {
	case result := <-accepted:
		if result.err != nil {
			t.Fatalf("accept deferred connection: %v", result.err)
		}
		server = result.connection
	case <-verifyContext.Done():
		t.Fatal("server did not accept the connection after peer verification")
	}
	t.Cleanup(func() { _ = server.Close() })

	if err := server.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("set server read deadline: %v", err)
	}
	buffer := make([]byte, 1)
	count, readErr := server.Read(buffer)
	var networkErr net.Error
	if count != 0 || !errors.As(readErr, &networkErr) || !networkErr.Timeout() {
		t.Fatalf("server read before client close = %d, %v; want timeout without bytes", count, readErr)
	}
	if err := server.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear server read deadline: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	clientClosed = true
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set server EOF deadline: %v", err)
	}
	count, readErr = server.Read(buffer)
	if count != 0 || !errors.Is(readErr, io.EOF) {
		t.Fatalf("server read after client close = %d, %v; want EOF", count, readErr)
	}
}

func TestVerifyLoopbackTCPPeerDoesNotExtendCallerDeadlineDuringDeferredAccept(t *testing.T) {
	listener := listenWithDeferredAccept(t)
	t.Cleanup(func() { _ = listener.Close() })
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial deferred-accept listener: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	callerContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = VerifyLoopbackTCPPeer(callerContext, client, os.Getpid(), identity)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("VerifyLoopbackTCPPeer() error = %v, want caller deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("VerifyLoopbackTCPPeer() exceeded caller deadline: %v", elapsed)
	}
}

type acceptResult struct {
	connection net.Conn
	err        error
}

func listenWithDeferredAccept(t *testing.T) net.Listener {
	t.Helper()
	config := net.ListenConfig{
		Control: func(_ string, _ string, raw syscall.RawConn) error {
			var socketErr error
			if err := raw.Control(func(descriptor uintptr) {
				socketErr = syscall.SetsockoptInt(int(descriptor), syscall.IPPROTO_TCP, syscall.TCP_DEFER_ACCEPT, 1)
			}); err != nil {
				return err
			}
			return socketErr
		},
	}
	listener, err := config.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen with TCP_DEFER_ACCEPT: %v", err)
	}
	return listener
}
