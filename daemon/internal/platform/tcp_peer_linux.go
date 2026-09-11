//go:build linux

package platform

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

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
