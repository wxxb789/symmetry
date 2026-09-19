//go:build windows

package platform

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
)

func TestInspectLoopbackTCPPeerVerifiesNativeEstablishedConnection(t *testing.T) {
	_, _, tuple := openNativeLoopbackConnection(t)

	pid := os.Getpid()
	identity, err := ProcessIdentity(pid)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}

	found, err := inspectLoopbackTCPPeer(context.Background(), tuple, pid, identity)
	if err != nil {
		t.Fatalf("inspectLoopbackTCPPeer() error = %v", err)
	}
	if !found {
		t.Fatal("inspectLoopbackTCPPeer() did not find the native TCP connection")
	}

	if _, err := inspectLoopbackTCPPeer(context.Background(), tuple, pid, identity+"-different"); err == nil {
		t.Fatal("inspectLoopbackTCPPeer() accepted a changed process identity")
	}
}

func TestFindTCPPeerInTableRejectsMalformedBuffers(t *testing.T) {
	tuple := testTCPPeerTuple()
	for _, table := range [][]byte{
		nil,
		{1, 0, 0, 0},
		{0xff, 0xff, 0xff, 0xff},
	} {
		if _, err := findTCPPeerInTable(context.Background(), table, tuple, 7); err == nil {
			t.Fatalf("findTCPPeerInTable(%v) succeeded", table)
		}
	}
}

func TestReportedTCPTableBytesRejectsUnreportedTail(t *testing.T) {
	table := testTCPTable(tcpStateEstablished, testTCPPeerTuple(), 7)
	for _, size := range []uint32{0, tcpOwnerPIDTableHeaderSize - 1, uint32(len(table) + 1)} {
		if _, err := reportedTCPTableBytes(table, size); err == nil {
			t.Fatalf("reportedTCPTableBytes(size %d) succeeded", size)
		}
	}

	reported, err := reportedTCPTableBytes(table, tcpOwnerPIDTableHeaderSize)
	if err != nil {
		t.Fatalf("reportedTCPTableBytes() error = %v", err)
	}
	if len(reported) != tcpOwnerPIDTableHeaderSize {
		t.Fatalf("reported table length = %d, want %d", len(reported), tcpOwnerPIDTableHeaderSize)
	}
	if _, err := findTCPPeerInTable(context.Background(), reported, testTCPPeerTuple(), 7); err == nil {
		t.Fatal("findTCPPeerInTable() accepted a row outside the reported table size")
	}
}

func TestFindTCPPeerInTableRequiresEstablishedMatchingOwner(t *testing.T) {
	tuple := testTCPPeerTuple()
	if found, err := findTCPPeerInTable(context.Background(), testTCPTable(tcpStateEstablished-1, tuple, 7), tuple, 7); err != nil || found {
		t.Fatalf("non-established row = found %v, error %v", found, err)
	}

	wrongTuple := tcpPeerTuple{
		server: netip.MustParseAddrPort("127.0.0.1:47001"),
		client: tuple.client,
	}
	if found, err := findTCPPeerInTable(context.Background(), testTCPTable(tcpStateEstablished, wrongTuple, 7), tuple, 7); err != nil || found {
		t.Fatalf("unrelated row = found %v, error %v", found, err)
	}

	if _, err := findTCPPeerInTable(context.Background(), testTCPTable(tcpStateEstablished, tuple, 8), tuple, 7); err == nil {
		t.Fatal("findTCPPeerInTable() accepted a different owner PID")
	}
}

func openNativeLoopbackConnection(t *testing.T) (net.Conn, net.Conn, tcpPeerTuple) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan net.Conn, 1)
	acceptError := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			acceptError <- acceptErr
			return
		}
		accepted <- connection
	}()

	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var server net.Conn
	select {
	case server = <-accepted:
	case err := <-acceptError:
		t.Fatalf("accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("accept timed out")
	}
	t.Cleanup(func() { _ = server.Close() })

	serverAddress, err := netip.ParseAddrPort(client.RemoteAddr().String())
	if err != nil {
		t.Fatalf("parse server address: %v", err)
	}
	clientAddress, err := netip.ParseAddrPort(client.LocalAddr().String())
	if err != nil {
		t.Fatalf("parse client address: %v", err)
	}
	return client, server, tcpPeerTuple{server: serverAddress, client: clientAddress}
}

func testTCPPeerTuple() tcpPeerTuple {
	return tcpPeerTuple{
		server: netip.MustParseAddrPort("127.0.0.1:47000"),
		client: netip.MustParseAddrPort("127.0.0.1:47001"),
	}
}

func testTCPTable(state uint32, tuple tcpPeerTuple, pid uint32) []byte {
	table := make([]byte, tcpOwnerPIDTableHeaderSize+tcpOwnerPIDRowSize)
	binary.LittleEndian.PutUint32(table, 1)
	writeTCPRow(table[tcpOwnerPIDTableHeaderSize:], state, tuple, pid)
	return table
}

func writeTCPRow(row []byte, state uint32, tuple tcpPeerTuple, pid uint32) {
	binary.LittleEndian.PutUint32(row, state)
	binary.LittleEndian.PutUint32(row[4:], testTCPAddress(tuple.server.Addr()))
	binary.LittleEndian.PutUint32(row[8:], testTCPPort(tuple.server.Port()))
	binary.LittleEndian.PutUint32(row[12:], testTCPAddress(tuple.client.Addr()))
	binary.LittleEndian.PutUint32(row[16:], testTCPPort(tuple.client.Port()))
	binary.LittleEndian.PutUint32(row[20:], pid)
}

func testTCPAddress(address netip.Addr) uint32 {
	bytes := address.As4()
	return binary.LittleEndian.Uint32(bytes[:])
}

func testTCPPort(port uint16) uint32 {
	return uint32(port>>8) | uint32(port&0xff)<<8
}
