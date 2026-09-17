//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const linuxProcessIdentityVersion = 2

// ErrPersistedProcessGroupUnproven classifies every result other than a
// verified ESRCH from getpriority(PRIO_PGRP, pid). Callers must retain the
// persisted marker when errors.Is reports this category.
var ErrPersistedProcessGroupUnproven = errors.New("persisted process group absence is unproven")

var (
	readLinuxBootID               = readLinuxBootIDFile
	readLinuxPIDNamespaceInfo     = readLinuxPIDNamespaceInfoFile
	readLinuxProcessStartTime     = readLinuxProcessStartTimeFile
	readLinuxProcessGroupPriority = unix.Getpriority
)

type linuxPIDNamespaceInfo struct {
	link  string
	inode uint64
}

type linuxProcessIdentity struct {
	version           int
	bootID            string
	pidNamespaceInode uint64
	pid               int
	startTime         uint64
}

// ProcessIdentity returns the strict Linux v2 identity used for persisted
// process ownership. The process namespace must be the daemon's current
// namespace and its symlink inode must agree with the kernel-reported inode.
func ProcessIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("process pid must be positive")
	}

	bootID, err := readLinuxBootID()
	if err != nil {
		return "", fmt.Errorf("read Linux boot ID: %w", err)
	}
	if !isCanonicalLinuxBootID(bootID) {
		return "", fmt.Errorf("read Linux boot ID: non-canonical value %q", bootID)
	}

	targetNamespace, err := readLinuxPIDNamespaceInfo(pid)
	if err != nil {
		return "", fmt.Errorf("read process %d PID namespace: %w", pid, err)
	}
	selfNamespace, err := readLinuxPIDNamespaceInfo(0)
	if err != nil {
		return "", fmt.Errorf("read daemon PID namespace: %w", err)
	}
	if targetNamespace.link != selfNamespace.link || targetNamespace.inode != selfNamespace.inode {
		return "", fmt.Errorf("process %d PID namespace does not match daemon namespace: target=%q/%d self=%q/%d", pid, targetNamespace.link, targetNamespace.inode, selfNamespace.link, selfNamespace.inode)
	}

	startTime, err := readLinuxProcessStartTime(pid)
	if err != nil {
		return "", fmt.Errorf("read process %d start time: %w", pid, err)
	}
	if startTime == 0 {
		return "", fmt.Errorf("read process %d start time: value must be positive", pid)
	}
	return fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, targetNamespace.inode, pid, startTime), nil
}

func readLinuxBootIDFile() (string, error) {
	value, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(value))
	if !isCanonicalLinuxBootID(bootID) {
		return "", fmt.Errorf("boot ID %q is not a canonical lowercase UUID", bootID)
	}
	return bootID, nil
}

func readLinuxPIDNamespaceInfoFile(pid int) (linuxPIDNamespaceInfo, error) {
	path := "/proc/self/ns/pid"
	if pid > 0 {
		path = fmt.Sprintf("/proc/%d/ns/pid", pid)
	} else if pid < 0 {
		return linuxPIDNamespaceInfo{}, errors.New("process pid must be non-negative for namespace lookup")
	}

	link, err := os.Readlink(path)
	if err != nil {
		return linuxPIDNamespaceInfo{}, err
	}
	linkInode, err := parseLinuxPIDNamespaceLink(link)
	if err != nil {
		return linuxPIDNamespaceInfo{}, fmt.Errorf("malformed namespace link %q: %w", link, err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return linuxPIDNamespaceInfo{}, fmt.Errorf("stat namespace link: %w", err)
	}
	statInode := uint64(stat.Ino)
	if statInode == 0 || statInode != linkInode {
		return linuxPIDNamespaceInfo{}, fmt.Errorf("namespace link inode %d does not match stat inode %d", linkInode, statInode)
	}
	return linuxPIDNamespaceInfo{link: link, inode: statInode}, nil
}

func parseLinuxPIDNamespaceLink(value string) (uint64, error) {
	if !strings.HasPrefix(value, "pid:[") || !strings.HasSuffix(value, "]") {
		return 0, errors.New("PID namespace link must have the form pid:[N]")
	}
	return parseCanonicalPositiveUint(strings.TrimSuffix(strings.TrimPrefix(value, "pid:["), "]"), "PID namespace inode")
}

func readLinuxProcessStartTimeFile(pid int) (uint64, error) {
	if pid <= 0 {
		return 0, errors.New("process pid must be positive")
	}
	value, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	startTime, err := linuxProcessStartTime(string(value))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(startTime, 10, 64)
}

func linuxProcessStartTime(stat string) (string, error) {
	closingParenthesis := strings.LastIndexByte(stat, ')')
	if closingParenthesis < 0 || closingParenthesis+1 >= len(stat) || stat[closingParenthesis+1] != ' ' {
		return "", errors.New("parse process start time: malformed stat")
	}
	fields := strings.Fields(stat[closingParenthesis+1:])
	// Field 3 is the first field after comm; starttime is field 22.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", errors.New("parse process start time: missing starttime")
	}
	value, err := parseCanonicalPositiveUint(fields[startTimeIndex], "process start time")
	if err != nil {
		return "", fmt.Errorf("parse process start time: %w", err)
	}
	return strconv.FormatUint(value, 10), nil
}

func isCanonicalLinuxBootID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch {
		case index == 8 || index == 13 || index == 18 || index == 23:
			if character != '-' {
				return false
			}
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

func parseCanonicalPositiveUint(value, label string) (uint64, error) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, fmt.Errorf("%s must be a canonical positive decimal", label)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("%s must be a canonical positive decimal", label)
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		if err == nil {
			err = errors.New("value must be positive")
		}
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	return parsed, nil
}

// parseLinuxProcessIdentity recognizes the historical v1 marker so callers
// can report it explicitly, but only v2 is eligible for a new absence proof.
func parseLinuxProcessIdentity(value string) (linuxProcessIdentity, error) {
	parts := strings.Split(value, ":")
	switch {
	case len(parts) == 4 && parts[0] == "linux":
		pid, err := parseCanonicalPositiveUint(parts[2], "v1 pid")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		startTime, err := parseCanonicalPositiveUint(parts[3], "v1 start time")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		if parts[1] == "" || strings.ContainsAny(parts[1], " \t\r\n") {
			return linuxProcessIdentity{}, errors.New("v1 boot ID is empty or contains whitespace")
		}
		return linuxProcessIdentity{version: 1, bootID: parts[1], pid: int(pid), startTime: startTime}, nil
	case len(parts) == 5 && parts[0] == "linux" && parts[1] == "v1":
		pid, err := parseCanonicalPositiveUint(parts[3], "v1 pid")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		startTime, err := parseCanonicalPositiveUint(parts[4], "v1 start time")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		if parts[2] == "" || strings.ContainsAny(parts[2], " \t\r\n") {
			return linuxProcessIdentity{}, errors.New("v1 boot ID is empty or contains whitespace")
		}
		return linuxProcessIdentity{version: 1, bootID: parts[2], pid: int(pid), startTime: startTime}, nil
	case len(parts) == 6 && parts[0] == "linux" && parts[1] == "v2":
		if !isCanonicalLinuxBootID(parts[2]) {
			return linuxProcessIdentity{}, errors.New("v2 boot ID is not canonical")
		}
		namespace, err := parseCanonicalPositiveUint(parts[3], "v2 PID namespace inode")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		pid, err := parseCanonicalPositiveUint(parts[4], "v2 pid")
		if err != nil || pid > uint64(^uint(0)>>1) {
			if err == nil {
				err = errors.New("pid is outside the platform int range")
			}
			return linuxProcessIdentity{}, fmt.Errorf("v2 pid: %w", err)
		}
		startTime, err := parseCanonicalPositiveUint(parts[5], "v2 start time")
		if err != nil {
			return linuxProcessIdentity{}, err
		}
		return linuxProcessIdentity{version: linuxProcessIdentityVersion, bootID: parts[2], pidNamespaceInode: namespace, pid: int(pid), startTime: startTime}, nil
	default:
		return linuxProcessIdentity{}, errors.New("unsupported Linux process identity format")
	}
}

// ProvePersistedProcessGroupAbsent proves only that the exact v2 process
// identity's process group is absent. It never sends a signal and treats a
// successful getpriority call as unresolved because the group still exists.
func ProvePersistedProcessGroupAbsent(ctx context.Context, pid int, expectedIdentity string) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrPersistedProcessGroupUnproven)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: context before proof: %w", ErrPersistedProcessGroupUnproven, err)
	}
	if pid <= 0 || expectedIdentity == "" {
		return fmt.Errorf("%w: positive pid and expected identity are required", ErrPersistedProcessGroupUnproven)
	}
	parsed, err := parseLinuxProcessIdentity(expectedIdentity)
	if err != nil {
		return fmt.Errorf("%w: parse expected identity: %v", ErrPersistedProcessGroupUnproven, err)
	}
	if parsed.version != linuxProcessIdentityVersion {
		return fmt.Errorf("%w: v1 identity cannot authorize a new process-group proof", ErrPersistedProcessGroupUnproven)
	}
	if parsed.pid != pid {
		return fmt.Errorf("%w: expected identity pid %d does not match pid %d", ErrPersistedProcessGroupUnproven, parsed.pid, pid)
	}

	bootID, err := readLinuxBootID()
	if err != nil {
		return fmt.Errorf("%w: read current boot ID: %v", ErrPersistedProcessGroupUnproven, err)
	}
	if bootID != parsed.bootID {
		return fmt.Errorf("%w: boot ID changed from %q to %q", ErrPersistedProcessGroupUnproven, parsed.bootID, bootID)
	}
	selfNamespace, err := readLinuxPIDNamespaceInfo(0)
	if err != nil {
		return fmt.Errorf("%w: read current PID namespace: %v", ErrPersistedProcessGroupUnproven, err)
	}
	if selfNamespace.inode != parsed.pidNamespaceInode {
		return fmt.Errorf("%w: PID namespace inode changed from %d to %d", ErrPersistedProcessGroupUnproven, parsed.pidNamespaceInode, selfNamespace.inode)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: context before group proof: %w", ErrPersistedProcessGroupUnproven, err)
	}

	_, priorityErr := readLinuxProcessGroupPriority(unix.PRIO_PGRP, pid)
	if errors.Is(priorityErr, unix.ESRCH) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: context after group proof: %w", ErrPersistedProcessGroupUnproven, err)
		}
		return nil
	}
	if priorityErr == nil {
		return fmt.Errorf("%w: getpriority found process group %d", ErrPersistedProcessGroupUnproven, pid)
	}
	return fmt.Errorf("%w: getpriority process group %d: %w", ErrPersistedProcessGroupUnproven, pid, priorityErr)
}
