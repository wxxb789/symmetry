package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

type tcpPeerTuple struct {
	server netip.AddrPort
	client netip.AddrPort
}

// VerifyLoopbackTCPPeer verifies an already-connected IPv4 loopback peer against
// the exact process identity. No application bytes are sent. Kernel snapshots
// cannot defend against a trusted process deliberately transferring its socket.
func VerifyLoopbackTCPPeer(ctx context.Context, conn net.Conn, pid int, identity string) error {
	if ctx == nil || conn == nil || pid <= 0 || identity == "" {
		return errors.New("TCP peer verification requires context, connection, PID and identity")
	}
	server, serverOK := conn.RemoteAddr().(*net.TCPAddr)
	client, clientOK := conn.LocalAddr().(*net.TCPAddr)
	if !serverOK || !clientOK || server == nil || client == nil {
		return errors.New("TCP peer verification requires TCP addresses")
	}
	if server.Port <= 0 || server.Port > 65535 || client.Port <= 0 || client.Port > 65535 {
		return errors.New("TCP peer verification requires valid connection ports")
	}
	tuple := tcpPeerTuple{server: server.AddrPort(), client: client.AddrPort()}
	tuple.server = netip.AddrPortFrom(tuple.server.Addr().Unmap(), tuple.server.Port())
	tuple.client = netip.AddrPortFrom(tuple.client.Addr().Unmap(), tuple.client.Port())
	for _, address := range []netip.AddrPort{tuple.server, tuple.client} {
		if !address.Addr().Is4() || !address.Addr().IsLoopback() || address.Port() == 0 {
			return errors.New("TCP peer verification supports only IPv4 loopback connections")
		}
	}

	// connect can finish before accept installs an FD in the server. Observe the
	// same connection briefly, never redial or send a readiness request first.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify TCP peer ownership: %w", err)
		}
		owned, err := inspectLoopbackTCPPeer(ctx, tuple, pid, identity)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify TCP peer ownership: %w", err)
		}
		if owned {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("verify TCP peer ownership: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
