//go:build linux || windows

package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const tcpPeerHelperEnvironment = "GO_WANT_TCP_PEER_HELPER"

const tcpPeerHelperTimeout = 15 * time.Second

func TestVerifyLoopbackTCPPeerRejectsInvalidInputs(t *testing.T) {
	pipeClient, pipeServer := net.Pipe()
	defer pipeClient.Close()
	defer pipeServer.Close()

	for _, test := range []struct {
		name     string
		context  context.Context
		conn     net.Conn
		pid      int
		identity string
	}{
		{name: "nil context", context: nil, conn: tcpPeerAddressConn{server: netip.MustParseAddrPort("127.0.0.1:1"), client: netip.MustParseAddrPort("127.0.0.1:2")}, pid: 1, identity: "identity"},
		{name: "nil connection", context: context.Background(), conn: nil, pid: 1, identity: "identity"},
		{name: "non TCP connection", context: context.Background(), conn: pipeClient, pid: 1, identity: "identity"},
		{name: "IPv6 loopback", context: context.Background(), conn: tcpPeerAddressConn{server: netip.MustParseAddrPort("[::1]:1"), client: netip.MustParseAddrPort("[::1]:2")}, pid: 1, identity: "identity"},
		{name: "non loopback", context: context.Background(), conn: tcpPeerAddressConn{server: netip.MustParseAddrPort("192.0.2.1:1"), client: netip.MustParseAddrPort("127.0.0.1:2")}, pid: 1, identity: "identity"},
		{name: "zero server port", context: context.Background(), conn: tcpPeerAddressConn{server: netip.MustParseAddrPort("127.0.0.1:0"), client: netip.MustParseAddrPort("127.0.0.1:2")}, pid: 1, identity: "identity"},
		{name: "zero client port", context: context.Background(), conn: tcpPeerAddressConn{server: netip.MustParseAddrPort("127.0.0.1:1"), client: netip.MustParseAddrPort("127.0.0.1:0")}, pid: 1, identity: "identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := VerifyLoopbackTCPPeer(test.context, test.conn, test.pid, test.identity); err == nil {
				t.Fatal("VerifyLoopbackTCPPeer() accepted an invalid input")
			}
		})
	}
}

func TestVerifyLoopbackTCPPeerWithOwnedHelperProcess(t *testing.T) {
	if os.Getenv(tcpPeerHelperEnvironment) == "1" {
		runTCPPeerHelper(t)
		return
	}

	helper := startTCPPeerHelper(t)
	defer helper.stop(t)

	client, err := net.Dial("tcp4", helper.address)
	if err != nil {
		t.Fatalf("dial helper listener: %v", err)
	}
	defer client.Close()

	if runtime.GOOS == "linux" {
		tuple, err := tcpPeerTupleFromClient(client)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := inspectLoopbackTCPPeer(context.Background(), tuple, helper.pid, helper.identity)
		if err != nil {
			t.Fatalf("inspect peer before helper accept: %v", err)
		}
		if owned {
			t.Fatal("Linux peer ownership appeared before helper accepted the connection")
		}
	}

	verified := make(chan error, 1)
	go func() {
		verified <- VerifyLoopbackTCPPeer(context.Background(), client, helper.pid, helper.identity)
	}()
	helper.accept(t)
	select {
	case err := <-verified:
		if err != nil {
			t.Fatalf("VerifyLoopbackTCPPeer(owned helper) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("VerifyLoopbackTCPPeer did not observe helper accept")
	}

	parentIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity(parent) error = %v", err)
	}
	if err := VerifyLoopbackTCPPeer(context.Background(), client, os.Getpid(), parentIdentity); err == nil || runtime.GOOS == "linux" && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("VerifyLoopbackTCPPeer(client process as server owner) = %v; Linux must exhaust its internal observation deadline", err)
	}
	if err := VerifyLoopbackTCPPeer(context.Background(), client, helper.pid, helper.identity+"-wrong"); err == nil {
		t.Fatal("VerifyLoopbackTCPPeer accepted a changed process identity")
	}

	otherClient, err := net.Dial("tcp4", helper.address)
	if err != nil {
		t.Fatalf("dial second helper connection: %v", err)
	}
	otherTuple, err := tcpPeerTupleFromClient(otherClient)
	if err != nil {
		_ = otherClient.Close()
		t.Fatal(err)
	}
	helper.accept(t)
	helper.closeLast(t)
	if err := otherClient.Close(); err != nil {
		t.Fatalf("close second helper connection: %v", err)
	}
	otherContext, cancelOther := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err = VerifyLoopbackTCPPeer(otherContext, tcpPeerAddressConn{server: otherTuple.server, client: otherTuple.client}, helper.pid, helper.identity)
	cancelOther()
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("VerifyLoopbackTCPPeer accepted a different client ephemeral port: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyLoopbackTCPPeer(cancelled, client, helper.pid, helper.identity); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyLoopbackTCPPeer cancelled context error = %v", err)
	}

	helper.stop(t)
	if err := VerifyLoopbackTCPPeer(context.Background(), client, helper.pid, helper.identity); err == nil {
		t.Fatal("VerifyLoopbackTCPPeer accepted an exited helper process")
	}
}

type tcpPeerHelper struct {
	command  *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Reader
	cancel   context.CancelFunc
	address  string
	pid      int
	identity string
	stopOnce sync.Once
	stopErr  error
}

func startTCPPeerHelper(t *testing.T) *tcpPeerHelper {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), tcpPeerHelperTimeout)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVerifyLoopbackTCPPeerWithOwnedHelperProcess$")
	command.WaitDelay = time.Second
	command.Env = append(os.Environ(), tcpPeerHelperEnvironment+"=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin: %v", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout: %v", err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start TCP peer helper: %v", err)
	}
	helper := &tcpPeerHelper{command: command, input: input, output: bufio.NewReader(output), cancel: cancel, pid: command.Process.Pid}
	t.Cleanup(func() { helper.cleanup(t) })
	address, err := helper.output.ReadString('\n')
	if err != nil {
		helper.cleanup(t)
		t.Fatalf("read helper address: %v", err)
	}
	identity, err := ProcessIdentity(command.Process.Pid)
	if err != nil {
		helper.cleanup(t)
		t.Fatalf("ProcessIdentity(helper) error = %v", err)
	}
	helper.address = strings.TrimSpace(address)
	helper.identity = identity
	return helper
}

func (helper *tcpPeerHelper) accept(t *testing.T) {
	t.Helper()
	if _, err := fmt.Fprintln(helper.input, "accept"); err != nil {
		t.Fatalf("request helper accept: %v", err)
	}
	line, err := helper.output.ReadString('\n')
	if err != nil {
		t.Fatalf("read helper accept receipt: %v", err)
	}
	if strings.TrimSpace(line) != "accepted" {
		t.Fatalf("helper accept receipt = %q", line)
	}
}

func (helper *tcpPeerHelper) stop(t *testing.T) {
	t.Helper()
	helper.stopOnce.Do(func() {
		var writeErr error
		if helper.input != nil {
			_, writeErr = fmt.Fprintln(helper.input, "exit")
			_ = helper.input.Close()
		}
		if writeErr != nil {
			helper.cancel()
		}
		helper.stopErr = errors.Join(writeErr, helper.command.Wait())
		helper.cancel()
	})
	if helper.stopErr != nil {
		t.Fatalf("stop TCP peer helper: %v", helper.stopErr)
	}
}

func (helper *tcpPeerHelper) cleanup(t *testing.T) {
	t.Helper()
	ran := false
	helper.stopOnce.Do(func() {
		ran = true
		if helper.input != nil {
			_ = helper.input.Close()
		}
		helper.cancel()
		helper.stopErr = helper.command.Wait()
	})
	if ran && helper.stopErr != nil && !errors.Is(helper.stopErr, context.Canceled) {
		t.Errorf("wait TCP peer helper during cleanup: %v", helper.stopErr)
	}
}

func (helper *tcpPeerHelper) closeLast(t *testing.T) {
	t.Helper()
	if _, err := fmt.Fprintln(helper.input, "close_last"); err != nil {
		t.Fatalf("request helper connection close: %v", err)
	}
	line, err := helper.output.ReadString('\n')
	if err != nil {
		t.Fatalf("read helper connection close receipt: %v", err)
	}
	if strings.TrimSpace(line) != "closed" {
		t.Fatalf("helper connection close receipt = %q", line)
	}
}

func runTCPPeerHelper(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("helper listen: %v", err)
	}
	defer listener.Close()
	if _, err := fmt.Fprintln(os.Stdout, listener.Addr().String()); err != nil {
		t.Fatalf("write helper address: %v", err)
	}
	var connections []net.Conn
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	reader := bufio.NewReader(os.Stdin)
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			t.Fatalf("read helper command: %v", err)
		}
		switch strings.TrimSpace(command) {
		case "accept":
			connection, err := listener.Accept()
			if err != nil {
				t.Fatalf("helper accept: %v", err)
			}
			connections = append(connections, connection)
			if _, err := fmt.Fprintln(os.Stdout, "accepted"); err != nil {
				t.Fatalf("write helper accept receipt: %v", err)
			}
		case "close_last":
			if len(connections) == 0 {
				t.Fatal("helper has no connection to close")
			}
			last := len(connections) - 1
			if err := connections[last].Close(); err != nil {
				t.Fatalf("close helper connection: %v", err)
			}
			connections = connections[:last]
			if _, err := fmt.Fprintln(os.Stdout, "closed"); err != nil {
				t.Fatalf("write helper close receipt: %v", err)
			}
		case "exit":
			return
		default:
			t.Fatalf("unknown helper command %q", command)
		}
	}
}

func tcpPeerTupleFromClient(connection net.Conn) (tcpPeerTuple, error) {
	server, serverOK := connection.RemoteAddr().(*net.TCPAddr)
	client, clientOK := connection.LocalAddr().(*net.TCPAddr)
	if !serverOK || !clientOK || server == nil || client == nil {
		return tcpPeerTuple{}, errors.New("test connection does not expose TCP addresses")
	}
	return tcpPeerTuple{
		server: netip.AddrPortFrom(server.AddrPort().Addr().Unmap(), server.AddrPort().Port()),
		client: netip.AddrPortFrom(client.AddrPort().Addr().Unmap(), client.AddrPort().Port()),
	}, nil
}

type tcpPeerAddressConn struct {
	server netip.AddrPort
	client netip.AddrPort
}

func (connection tcpPeerAddressConn) Read([]byte) (int, error)  { return 0, net.ErrClosed }
func (connection tcpPeerAddressConn) Write([]byte) (int, error) { return 0, net.ErrClosed }
func (connection tcpPeerAddressConn) Close() error              { return nil }
func (connection tcpPeerAddressConn) LocalAddr() net.Addr {
	return net.TCPAddrFromAddrPort(connection.client)
}
func (connection tcpPeerAddressConn) RemoteAddr() net.Addr {
	return net.TCPAddrFromAddrPort(connection.server)
}
func (connection tcpPeerAddressConn) SetDeadline(time.Time) error      { return nil }
func (connection tcpPeerAddressConn) SetReadDeadline(time.Time) error  { return nil }
func (connection tcpPeerAddressConn) SetWriteDeadline(time.Time) error { return nil }
