//go:build linux && amd64

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

func (helper *linuxOwnerHelperV2) readRunLeafProcessIDs(ctx context.Context) (map[int]struct{}, error) {
	if helper == nil || ctx == nil || helper.leaf == nil || helper.grant.Validate() != nil ||
		helper.owner.Validate() != nil || helper.owner.AUID != helper.grant.OwnerUID ||
		!helper.leaf.Identity().Equal(helper.grant.Leaf) {
		return nil, errors.New("Linux owner helper leaf membership scan is incomplete")
	}
	pids, err := readLinuxOwnerAnchorRunLeafProcessIDs(ctx, helper.leaf, helper.grant, helper.owner.AUID)
	if err != nil {
		return nil, fmt.Errorf("read exact run-leaf process set for helper membership audit: %w", err)
	}
	if err := helper.leaf.verifyLive(); err != nil {
		return nil, fmt.Errorf("revalidate pinned run leaf after membership scan: %w", err)
	}
	return pids, nil
}

type linuxOwnerHelperV2RunProcessSnapshot struct {
	process     authority.LinuxCgroupProcessIdentity
	credentials linuxOwnerProcessCredentials
}

func linuxOwnerHelperV2RunTreeOrder(
	helperPID int,
	processes map[int]linuxOwnerHelperV2RunProcessSnapshot,
) ([]int, error) {
	if helperPID <= 1 {
		return nil, errors.New("run process tree lacks an exact helper PID")
	}
	if _, helperInLeaf := processes[helperPID]; helperInLeaf {
		return nil, errors.New("helper process unexpectedly appears in its reserved run leaf")
	}
	childrenByParent := make(map[int][]int, len(processes))
	for pid, snapshot := range processes {
		if pid <= 1 || snapshot.process.PID != pid || snapshot.process.Validate() != nil || snapshot.credentials.Parent <= 1 {
			return nil, errors.New("run leaf contains an invalid exact process snapshot")
		}
		if snapshot.credentials.Parent != helperPID {
			if _, parentInLeaf := processes[snapshot.credentials.Parent]; !parentInLeaf {
				return nil, errors.New("run leaf process parent is neither helper nor a pinned run-leaf member")
			}
		}
		childrenByParent[snapshot.credentials.Parent] = append(childrenByParent[snapshot.credentials.Parent], pid)
	}
	for _, children := range childrenByParent {
		sort.Ints(children)
	}
	seen := map[int]struct{}{helperPID: {}}
	order := make([]int, 1, len(processes)+1)
	order[0] = helperPID
	for cursor := 0; cursor < len(order); cursor++ {
		for _, pid := range childrenByParent[order[cursor]] {
			if _, duplicate := seen[pid]; duplicate {
				return nil, errors.New("run leaf process tree contains a duplicate or cyclic PID")
			}
			seen[pid] = struct{}{}
			order = append(order, pid)
		}
	}
	if len(order)-1 != len(processes) {
		return nil, errors.New("run leaf contains a process outside the exact helper-owned descendant tree")
	}
	return order[1:], nil
}

func linuxOwnerHelperGateProcessIdentity(identity linuxgate.ProcessIdentity) (authority.LinuxCgroupProcessIdentity, error) {
	if identity.PID <= 0 || identity.StartTime == 0 || identity.BootID == "" || identity.UserNSInode == 0 || identity.OwnerPID <= 0 {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("Linux parked-gate launch identity is incomplete")
	}
	actual, err := ProcessIdentity(identity.PID)
	if err != nil {
		return authority.LinuxCgroupProcessIdentity{}, err
	}
	if identity.BootID != readLinuxCgroupBootID() {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("Linux parked gate boot identity changed")
	}
	return authority.LinuxCgroupProcessIdentity{PID: identity.PID, Identity: actual}, nil
}

func linuxOwnerHelperUntrustedProcessIdentity(
	identity linuxgate.ProcessIdentity,
	pidfd *os.File,
	helper authority.LinuxCgroupProcessIdentity,
	launcher *authority.LinuxCgroupProcessIdentity,
) (authority.LinuxCgroupProcessIdentity, error) {
	if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || identity.OwnerPID <= 1 ||
		pidfd == nil || helper.Validate() != nil || launcher == nil || launcher.Validate() != nil || identity.OwnerPID != launcher.PID {
		return authority.LinuxCgroupProcessIdentity{}, errors.New("failed-Prepare child identity is incomplete or not launcher-owned")
	}
	actual, err := ProcessIdentity(identity.PID)
	if err != nil {
		return authority.LinuxCgroupProcessIdentity{}, err
	}
	parsed, err := parseLinuxProcessIdentity(actual)
	if err != nil || parsed.version != linuxProcessIdentityVersion || parsed.pid != identity.PID ||
		parsed.bootID != identity.BootID || parsed.startTime != identity.StartTime {
		return authority.LinuxCgroupProcessIdentity{}, errors.Join(errors.New("failed-Prepare child PID/start identity differs from its retained gate pidfd"), err)
	}
	process := authority.LinuxCgroupProcessIdentity{PID: identity.PID, Identity: actual}
	if err := verifyLinuxOwnerAnchorPIDFD(int(pidfd.Fd()), process); err != nil {
		return authority.LinuxCgroupProcessIdentity{}, fmt.Errorf("verify failed-Prepare child pidfd identity: %w", err)
	}
	credentials, err := readLinuxOwnerProcessCredentials(process.PID)
	if err != nil || credentials.Parent != helper.PID {
		return authority.LinuxCgroupProcessIdentity{}, fmt.Errorf("verify failed-Prepare child helper ancestry: %w", errOr(err, errors.New("parent differs")))
	}
	return process, nil
}

func cloneLinuxOwnerProcessIdentity(value *authority.LinuxCgroupProcessIdentity) *authority.LinuxCgroupProcessIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneLinuxOwnerGateIdentity(value *authority.LinuxCgroupGateIdentity) *authority.LinuxCgroupGateIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneLinuxOwnerGateIdentityIfValid(value authority.LinuxCgroupGateIdentity) *authority.LinuxCgroupGateIdentity {
	if value.Validate() != nil {
		return nil
	}
	return cloneLinuxOwnerGateIdentity(&value)
}

func linuxOwnerAnchorTrackedIdentity(children map[int]linuxOwnerAnchorTrackedProcess, role string) authority.LinuxCgroupProcessIdentity {
	for _, child := range children {
		if child.role == role {
			return child.identity
		}
	}
	return authority.LinuxCgroupProcessIdentity{}
}

func parseLinuxOwnerChildPIDsBounded(contents []byte, maxBytes, maxPIDs int) ([]int, error) {
	if maxBytes < 0 || maxPIDs < 0 || len(contents) > maxBytes {
		return nil, errors.New("Linux /proc children list exceeds its bounded byte count")
	}
	pids := make([]int, 0)
	for cursor := 0; cursor < len(contents); {
		for cursor < len(contents) && linuxOwnerChildListSpace(contents[cursor]) {
			cursor++
		}
		if cursor == len(contents) {
			break
		}
		start := cursor
		for cursor < len(contents) && !linuxOwnerChildListSpace(contents[cursor]) {
			cursor++
		}
		if len(pids) >= maxPIDs {
			return nil, errors.New("Linux /proc children list exceeds its bounded process count")
		}
		token := contents[start:cursor]
		if len(token) > 10 || len(token) == 0 || token[0] == '0' {
			return nil, errors.New("Linux /proc children list contains an invalid PID")
		}
		pid := 0
		for _, digit := range token {
			if digit < '0' || digit > '9' {
				return nil, errors.New("Linux /proc children list contains an invalid PID")
			}
			pid = pid*10 + int(digit-'0')
		}
		if pid <= 1 {
			return nil, errors.New("Linux /proc children list contains an invalid PID")
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func readLinuxOwnerChildPIDsBounded(path string, maxBytes, maxPIDs int) ([]int, int, error) {
	if maxBytes < 0 || maxPIDs < 0 {
		return nil, 0, errors.New("Linux /proc children reader received negative bounds")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open bounded Linux /proc children list: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, len(contents), errors.Join(errors.New("read bounded Linux /proc children list"), readErr, closeErr)
	}
	pids, err := parseLinuxOwnerChildPIDsBounded(contents, maxBytes, maxPIDs)
	return pids, len(contents), err
}

func linuxOwnerChildListSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\v' || value == '\f'
}

func readLinuxOwnerProcessChildren(ctx context.Context, process authority.LinuxCgroupProcessIdentity) ([]int, error) {
	if ctx == nil {
		return nil, errors.New("Linux process child census requires a context")
	}
	var lastErr error
	for attempt := 0; attempt < linuxOwnerProcessChildrenSnapshotAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		children, err := readLinuxOwnerProcessChildrenSnapshot(ctx, process)
		if err == nil {
			return children, nil
		}
		lastErr = err
		if !errors.Is(err, errLinuxOwnerProcessChildrenChanged) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if attempt+1 < linuxOwnerProcessChildrenSnapshotAttempts {
			runtime.Gosched()
		}
	}
	return nil, fmt.Errorf("all-thread child census remained unstable after %d bounded attempts: %w", linuxOwnerProcessChildrenSnapshotAttempts, lastErr)
}

func readLinuxOwnerProcessChildrenSnapshot(ctx context.Context, process authority.LinuxCgroupProcessIdentity) ([]int, error) {
	if ctx == nil || process.Validate() != nil {
		return nil, errors.New("Linux process child census requires a context and exact process identity")
	}
	identity, err := ProcessIdentity(process.PID)
	if err != nil || identity != process.Identity {
		return nil, fmt.Errorf("verify process identity before all-thread child census: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	threadIDs, err := readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil {
		return nil, fmt.Errorf("read exact process thread set before child census: %w", err)
	}
	childrenByThread, err := readLinuxOwnerProcessThreadChildren(ctx, process.PID, threadIDs)
	if err != nil {
		return nil, err
	}
	children := make([]int, 0)
	seenChildren := make(map[int]struct{})
	for _, pids := range childrenByThread {
		for _, pid := range pids {
			if _, duplicate := seenChildren[pid]; duplicate {
				return nil, errors.New("Linux process child appears in multiple thread child lists")
			}
			seenChildren[pid] = struct{}{}
			children = append(children, pid)
		}
	}
	threadIDsAfter, err := readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil || !equalLinuxOwnerProcessThreadIDs(threadIDs, threadIDsAfter) {
		return nil, fmt.Errorf("%w: process TID set changed during all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, ErrLinuxOwnerThreadSnapshotDiffers))
	}
	verifiedChildrenByThread, err := readLinuxOwnerProcessThreadChildren(ctx, process.PID, threadIDs)
	if err != nil || !equalLinuxOwnerProcessChildLists(childrenByThread, verifiedChildrenByThread) {
		return nil, fmt.Errorf("%w: per-thread child lists changed during all-thread census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, errors.New("child snapshots differ")))
	}
	identityAfter, err := ProcessIdentity(process.PID)
	if err != nil || identityAfter != process.Identity {
		return nil, fmt.Errorf("revalidate process identity after all-thread child census: %w", errOr(err, ErrLinuxOwnerCreationIdentityChanged))
	}
	threadIDsAfter, err = readLinuxOwnerProcessThreadIDs(process.PID)
	if err != nil || !equalLinuxOwnerProcessThreadIDs(threadIDs, threadIDsAfter) {
		return nil, fmt.Errorf("%w: process TID set changed after all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(err, ErrLinuxOwnerThreadSnapshotDiffers))
	}
	sort.Ints(children)
	return children, nil
}

func readLinuxOwnerProcessThreadChildren(ctx context.Context, pid int, threadIDs []int) ([][]int, error) {
	childrenByThread := make([][]int, len(threadIDs))
	bytesRead, pidsRead := 0, 0
	for index, tid := range threadIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		childPath := fmt.Sprintf("/proc/%d/task/%d/children", pid, tid)
		pids, n, err := readLinuxOwnerChildPIDsBounded(
			childPath,
			linuxOwnerAnchorMaxChildrenBytes-bytesRead,
			linuxOwnerHelperV2MaxMembershipProcesses-pidsRead,
		)
		if err != nil {
			return nil, fmt.Errorf("read bounded child list for process TID %d: %w", tid, err)
		}
		sort.Ints(pids)
		bytesRead += n
		pidsRead += len(pids)
		childrenByThread[index] = pids
	}
	return childrenByThread, nil
}

func readLinuxOwnerProcessThreadIDs(pid int) ([]int, error) {
	if pid <= 1 {
		return nil, errors.New("Linux process thread census requires an exact PID")
	}
	directory, err := os.Open(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil, fmt.Errorf("open bounded Linux process task directory: %w", err)
	}
	entries, readErr := directory.ReadDir(linuxOwnerHelperV2MaxMembershipProcesses + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil {
		return nil, errors.Join(errors.New("read bounded Linux process task directory"), readErr, closeErr)
	}
	if len(entries) > linuxOwnerHelperV2MaxMembershipProcesses {
		return nil, errors.New("Linux process thread set exceeds its bounded task count")
	}
	threadIDs := make([]int, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || len(name) > 10 {
			return nil, errors.New("Linux process task directory contains an invalid TID entry")
		}
		tid, err := strconv.Atoi(name)
		if err != nil || tid <= 0 || strconv.Itoa(tid) != name {
			return nil, errors.New("Linux process task directory contains a noncanonical TID")
		}
		threadIDs = append(threadIDs, tid)
	}
	if len(threadIDs) == 0 {
		return nil, errors.New("Linux process task directory contains no live TIDs")
	}
	sort.Ints(threadIDs)
	return threadIDs, nil
}

func equalLinuxOwnerProcessThreadIDs(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalLinuxOwnerProcessChildLists(left, right [][]int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalLinuxOwnerProcessThreadIDs(left[index], right[index]) {
			return false
		}
	}
	return true
}
