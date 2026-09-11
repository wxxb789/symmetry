//go:build linux

package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
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
