//go:build windows

package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net/netip"
	"syscall"
	"unsafe"
)

const (
	addressFamilyINET           = 2
	tcpTableOwnerPIDConnections = 4
	tcpStateEstablished         = 5
	errorInsufficientBuffer     = 122
	processSynchronize          = 0x00100000
	waitObject0                 = 0
	waitTimeout                 = 258
	waitFailed                  = 0xFFFFFFFF
	tcpOwnerPIDTableHeaderSize  = 4
	tcpOwnerPIDRowSize          = 24
	maximumTCPTableBytes        = 64 << 20
	tcpTableQueryAttempts       = 4
)

var (
	iphlpapi            = syscall.NewLazyDLL("iphlpapi.dll")
	getExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
	waitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

// inspectLoopbackTCPPeer verifies the process that owns the server-side half
// of an already-connected IPv4 loopback TCP connection.
func inspectLoopbackTCPPeer(ctx context.Context, tuple tcpPeerTuple, pid int, identity string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if pid <= 0 || uint64(pid) > uint64(^uint32(0)) {
		return false, errors.New("peer process PID is invalid")
	}
	if identity == "" {
		return false, errors.New("peer process identity is required")
	}

	process, err := openProcessForIdentity(pid, processSynchronize)
	if err != nil {
		return false, fmt.Errorf("open peer process: %w", err)
	}
	defer syscall.CloseHandle(process)

	actualIdentity, err := processIdentityFromHandle(pid, process)
	if err != nil {
		return false, fmt.Errorf("read peer process creation identity: %w", err)
	}
	if actualIdentity != identity {
		return false, errors.New("peer process identity does not match persisted identity")
	}
	if err := ensureProcessIsAlive(process); err != nil {
		return false, err
	}

	table, err := readExtendedTCPTable(ctx)
	if err != nil {
		return false, err
	}
	found, err := findTCPPeerInTable(ctx, table, tuple, uint32(pid))
	if err != nil {
		return false, err
	}
	if err := ensureProcessIsAlive(process); err != nil {
		return false, err
	}
	return found, nil
}

func ensureProcessIsAlive(process syscall.Handle) error {
	result, _, callError := waitForSingleObject.Call(uintptr(process), 0)
	switch result {
	case waitTimeout:
		return nil
	case waitObject0:
		return errors.New("peer process has exited")
	case waitFailed:
		return fmt.Errorf("wait for peer process: %w", callError)
	default:
		return fmt.Errorf("wait for peer process: unexpected result %d", result)
	}
}

func readExtendedTCPTable(ctx context.Context) ([]byte, error) {
	var required uint32
	status, _, _ := getExtendedTCPTable.Call(
		0,
		uintptr(unsafe.Pointer(&required)),
		0,
		addressFamilyINET,
		tcpTableOwnerPIDConnections,
		0,
	)
	if status != 0 && status != errorInsufficientBuffer {
		return nil, fmt.Errorf("GetExtendedTcpTable returned status %d", status)
	}
	if required < tcpOwnerPIDTableHeaderSize {
		required = tcpOwnerPIDTableHeaderSize
	}

	for attempt := 0; attempt < tcpTableQueryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(required) > maximumTCPTableBytes || uint64(required) > uint64(int(^uint(0)>>1)) {
			return nil, fmt.Errorf("GetExtendedTcpTable requested invalid buffer size %d", required)
		}

		table := make([]byte, int(required))
		actualSize := uint32(len(table))
		status, _, _ = getExtendedTCPTable.Call(
			uintptr(unsafe.Pointer(&table[0])),
			uintptr(unsafe.Pointer(&actualSize)),
			0,
			addressFamilyINET,
			tcpTableOwnerPIDConnections,
			0,
		)
		switch status {
		case 0:
			return reportedTCPTableBytes(table, actualSize)
		case errorInsufficientBuffer:
			if actualSize <= uint32(len(table)) {
				return nil, fmt.Errorf("GetExtendedTcpTable returned non-growing buffer size %d", actualSize)
			}
			required = actualSize
		default:
			return nil, fmt.Errorf("GetExtendedTcpTable returned status %d", status)
		}
	}

	return nil, errors.New("GetExtendedTcpTable changed too quickly to obtain a stable table")
}

func reportedTCPTableBytes(table []byte, size uint32) ([]byte, error) {
	if size < tcpOwnerPIDTableHeaderSize || uint64(size) > uint64(len(table)) {
		return nil, fmt.Errorf("GetExtendedTcpTable returned invalid table size %d", size)
	}
	return table[:int(size)], nil
}

func findTCPPeerInTable(ctx context.Context, table []byte, tuple tcpPeerTuple, pid uint32) (bool, error) {
	if len(table) < tcpOwnerPIDTableHeaderSize {
		return false, errors.New("GetExtendedTcpTable returned a truncated table header")
	}
	rowCount := binary.LittleEndian.Uint32(table[:tcpOwnerPIDTableHeaderSize])
	available := len(table) - tcpOwnerPIDTableHeaderSize
	if uint64(rowCount)*tcpOwnerPIDRowSize > uint64(available) {
		return false, errors.New("GetExtendedTcpTable returned a truncated table row set")
	}

	for index := uint32(0); index < rowCount; index++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		offset := tcpOwnerPIDTableHeaderSize + int(index)*tcpOwnerPIDRowSize
		state := binary.LittleEndian.Uint32(table[offset:])
		local := tcpTableAddrPort(
			binary.LittleEndian.Uint32(table[offset+4:]),
			binary.LittleEndian.Uint32(table[offset+8:]),
		)
		remote := tcpTableAddrPort(
			binary.LittleEndian.Uint32(table[offset+12:]),
			binary.LittleEndian.Uint32(table[offset+16:]),
		)
		if local != tuple.server || remote != tuple.client || state != tcpStateEstablished {
			continue
		}

		if owner := binary.LittleEndian.Uint32(table[offset+20:]); owner != pid {
			return false, errors.New("connected TCP peer is owned by a different process")
		}
		return true, nil
	}

	return false, nil
}

func tcpTableAddrPort(address uint32, port uint32) netip.AddrPort {
	return netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{byte(address), byte(address >> 8), byte(address >> 16), byte(address >> 24)}),
		bits.ReverseBytes16(uint16(port)),
	)
}
