//go:build linux

// Package platform contains process-control primitives that differ by OS.
package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	containmentCloseDeadline      = 5 * time.Second
	containmentCloseProbeInterval = 10 * time.Millisecond
)

var (
	probePIDFDGroupSupport = checkPIDFDGroupSupport
	duplicatePIDFD         = duplicatePIDFDHandle
	readProcessIdentity    = ProcessIdentity
	sendPIDFDSignal        = unix.PidfdSendSignal
	closePIDFD             = unix.Close
	readLinuxProcessStat   = readLinuxProcessStatFile
)

// Containment owns a launched process's platform-specific termination boundary.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

// ConfigureHeadlessProcess is a no-op on Unix because child console windows
// are not created by the process-launch API used here.
func ConfigureHeadlessProcess(*exec.Cmd) error { return nil }

// ConfigureProcess verifies pidfd process-group signalling before making the
// launched process the leader of a new process group. Containment owns only
// processes that remain in that original group; descendants that create a
// session or move to another group are outside this platform boundary.
func ConfigureProcess(command *exec.Cmd) error {
	if err := probePIDFDGroupSupport(); err != nil {
		return fmt.Errorf("pidfd process-group containment is unsupported: %w", err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

// AttachProcess retains a duplicate of the original process pidfd and captures
// identity and the process-group anchor while the caller still owns the
// unreaped process handle.
func AttachProcess(process *os.Process) (Containment, string, error) {
	if process == nil || process.Pid <= 0 {
		return nil, "", errors.New("process handle is required for containment")
	}

	group := &processGroup{pid: process.Pid, fd: -1}
	var identity string
	var callbackErr error
	if err := process.WithHandle(func(handle uintptr) {
		fd, duplicateErr := duplicatePIDFD(handle)
		if duplicateErr != nil {
			callbackErr = fmt.Errorf("duplicate process pidfd: %w", duplicateErr)
			return
		}
		group.fd = fd

		var identityErr error
		identity, identityErr = readProcessIdentity(process.Pid)
		if identityErr != nil {
			callbackErr = fmt.Errorf("capture process creation identity: %w", identityErr)
			return
		}
		anchor, anchorErr := captureLinuxProcessGroupAnchor(process.Pid)
		if anchorErr != nil {
			callbackErr = fmt.Errorf("capture process group anchor: %w", anchorErr)
			return
		}
		group.anchor = anchor
		group.anchorCaptured = true
	}); err != nil {
		return nil, "", fmt.Errorf("borrow process handle for containment: %w", err)
	}
	if callbackErr != nil {
		if group.fd >= 0 {
			return group, identity, callbackErr
		}
		return nil, "", callbackErr
	}
	return group, identity, nil
}

type processGroup struct {
	mutex          sync.Mutex
	pid            int
	fd             int
	anchor         linuxProcessGroupAnchor
	anchorCaptured bool
	closeCompleted bool
	closeErr       error
}

var _ ContainmentCloseRetryer = (*processGroup)(nil)

// ContainmentCloseRetryable reports whether Close may safely repeat the
// identity-bound stop/proof attempt. A retained pidfd is the authority for
// every retry; once the operation reaches a final state, no retry is allowed.
func (group *processGroup) ContainmentCloseRetryable() bool {
	if group == nil {
		return false
	}
	group.mutex.Lock()
	defer group.mutex.Unlock()
	return group.anchorCaptured && group.fd >= 0 && !group.closeCompleted
}

func (group *processGroup) Terminate(force bool) error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closeCompleted {
		return group.closeErr
	}
	err := group.terminateLocked(force)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func (group *processGroup) terminateLocked(force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return group.signalLocked(unix.Signal(signal))
}

func (group *processGroup) signalLocked(signal unix.Signal) error {
	if group.fd < 0 {
		return fmt.Errorf("%w: retained pidfd is unavailable", errors.ErrUnsupported)
	}
	return sendPIDFDSignal(group.fd, signal, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
}

// Close kills the owned process group, then waits for a pidfd group signal-0
// probe to return ESRCH while the original leader fence remains valid. A nil
// probe result is only a bounded observation; it is never converted to proof.
// Stop/proof failures retain an anchored pidfd for a bounded caller retry.
func (group *processGroup) Close() error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closeCompleted {
		return group.closeErr
	}

	deadline := containmentDeadline(context.Background())
	var stopErr error
	if !group.anchorCaptured {
		initialSignalErr := group.terminateLocked(true)
		if initialSignalErr != nil && !errors.Is(initialSignalErr, unix.ESRCH) {
			stopErr = fmt.Errorf("terminate process group %d during containment close: %w", group.pid, initialSignalErr)
		} else {
			stopErr = errors.New("process group leader anchor is unavailable")
		}
	} else {
		initialLeaderPresent, leaderErr := observeLinuxProcessGroupLeader(group.pid, group.anchor)
		if leaderErr != nil {
			stopErr = leaderErr
		} else {
			initialSignalErr := group.terminateLocked(true)
			if initialSignalErr != nil && !errors.Is(initialSignalErr, unix.ESRCH) {
				stopErr = fmt.Errorf("terminate process group %d during containment close: %w", group.pid, initialSignalErr)
			} else {
				stopErr = waitForProcessGroupExit(context.Background(), group.pid, deadline, func() error {
					return group.probeGroupExitLocked(context.Background(), deadline, initialLeaderPresent)
				})
			}
		}
	}
	if stopErr != nil && group.anchorCaptured && group.fd >= 0 {
		// The original group was not proven stopped. Keep the identity-bound
		// pidfd and leave closeCompleted false so the owner can retry Close.
		return stopErr
	}

	// No anchor is unsafe to retry. A proven stop is final as well, and the
	// pidfd close result is part of that final outcome; close(2) is never
	// replayed.
	group.closeCompleted = true
	group.closeErr = errors.Join(stopErr, group.releasePIDFDLocked())
	return group.closeErr
}

func (group *processGroup) probeGroupExitLocked(ctx context.Context, deadline time.Time, previousLeaderPresent bool) error {
	return probeLinuxProcessGroupExit(ctx, deadline, group.pid, group.anchor, previousLeaderPresent, group.signalLocked)
}

func (group *processGroup) releasePIDFDLocked() error {
	if group.fd < 0 {
		return nil
	}
	fd := group.fd
	group.fd = -1
	if err := closePIDFD(fd); err != nil {
		return fmt.Errorf("close retained process pidfd: %w", err)
	}
	return nil
}

func duplicatePIDFDHandle(handle uintptr) (int, error) {
	return unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
}

func checkPIDFDGroupSupport() error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return fmt.Errorf("find current process: %w", err)
	}
	defer process.Release()
	var probeErr error
	if err := process.WithHandle(func(handle uintptr) {
		probeErr = unix.PidfdSendSignal(int(handle), 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	}); err != nil {
		return err
	}
	// A process that is not itself a group leader has no PIDTYPE_PGID target,
	// so a supported group-signalling kernel returns ESRCH for this side-effect-
	// free probe. EINVAL remains the unsupported-flag failure.
	if errors.Is(probeErr, unix.ESRCH) {
		return nil
	}
	return probeErr
}

// TerminateProcessGroup terminates a process group only after the expected
// identity matches while the supplied process's pidfd is borrowed. It never
// falls back to a numeric PID or PGID lookup.
func TerminateProcessGroup(ctx context.Context, process *os.Process, expectedIdentity string) error {
	if ctx == nil {
		return errors.New("termination context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if process == nil || process.Pid <= 0 || expectedIdentity == "" {
		return errors.New("process handle and expected identity are required for process-group termination")
	}

	var terminationErr error
	if err := process.WithHandle(func(handle uintptr) {
		actualIdentity, identityErr := readProcessIdentity(process.Pid)
		if identityErr != nil {
			terminationErr = fmt.Errorf("read process identity before group termination: %w", identityErr)
			return
		}
		if actualIdentity != expectedIdentity {
			terminationErr = fmt.Errorf("process identity mismatch before group termination: got %q, want %q", actualIdentity, expectedIdentity)
			return
		}
		anchor, anchorErr := captureLinuxProcessGroupAnchor(process.Pid)
		if anchorErr != nil {
			terminationErr = fmt.Errorf("capture process group anchor before group termination: %w", anchorErr)
			return
		}
		terminationErr = terminatePIDFDProcessGroup(ctx, int(handle), process.Pid, anchor)
	}); err != nil {
		return fmt.Errorf("borrow process handle for group termination: %w", err)
	}
	return terminationErr
}

func terminatePIDFDProcessGroup(ctx context.Context, fd, pid int, anchor linuxProcessGroupAnchor) error {
	deadline := containmentDeadline(ctx)
	initialLeaderPresent, leaderErr := observeLinuxProcessGroupLeader(pid, anchor)
	if leaderErr != nil {
		return leaderErr
	}
	initialSignalErr := sendPIDFDSignal(fd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	if initialSignalErr != nil && !errors.Is(initialSignalErr, unix.ESRCH) {
		return fmt.Errorf("terminate process group %d: %w", pid, initialSignalErr)
	}
	signal := func(signal unix.Signal) error {
		return sendPIDFDSignal(fd, signal, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	}
	return waitForProcessGroupExit(ctx, pid, deadline, func() error {
		return probeLinuxProcessGroupExit(ctx, deadline, pid, anchor, initialLeaderPresent, signal)
	})
}

func containmentDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(containmentCloseDeadline)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func waitForProcessGroupExit(ctx context.Context, pid int, deadline time.Time, probe func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := probe()
		if errors.Is(err, syscall.ESRCH) {
			if budgetErr := checkProcessGroupProbeBudget(ctx, deadline); budgetErr != nil {
				return budgetErr
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("observe process group %d after containment close: %w", pid, err)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("process group %d remained non-empty after containment close deadline", pid)
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type linuxProcessStat struct {
	pid       int
	pgrp      int64
	session   int64
	startTime uint64
}

type linuxProcessGroupAnchor struct {
	pid       int
	pgrp      int64
	session   int64
	startTime uint64
}

func (stat linuxProcessStat) matchesAnchor(anchor linuxProcessGroupAnchor) bool {
	return stat.pid == anchor.pid && stat.pgrp == anchor.pgrp && stat.session == anchor.session && stat.startTime == anchor.startTime
}

func observeLinuxProcessGroupLeader(pid int, anchor linuxProcessGroupAnchor) (bool, error) {
	leader, err := readLinuxProcessStat(pid)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read process group leader %d: %w", pid, err)
	}
	if !leader.matchesAnchor(anchor) {
		return false, fmt.Errorf("process group leader %d identity or fence changed", pid)
	}
	return true, nil
}

func probeLinuxProcessGroupExit(ctx context.Context, deadline time.Time, pid int, anchor linuxProcessGroupAnchor, previousLeaderPresent bool, signal func(unix.Signal) error) error {
	leaderPresent, err := observeLinuxProcessGroupLeader(pid, anchor)
	if err != nil {
		return err
	}
	if !previousLeaderPresent && leaderPresent {
		return errors.New("process group leader appeared or was reused between fenced probes")
	}
	signalErr := signal(0)
	return observeLinuxProcessGroupAfterSignal(ctx, deadline, pid, anchor, leaderPresent, signalErr)
}

func observeLinuxProcessGroupAfterSignal(ctx context.Context, deadline time.Time, pid int, anchor linuxProcessGroupAnchor, leaderPresentBeforeSignal bool, signalErr error) error {
	if err := checkProcessGroupProbeBudget(ctx, deadline); err != nil {
		return err
	}
	if signalErr != nil && !errors.Is(signalErr, unix.ESRCH) {
		return signalErr
	}
	leaderPresentAfterSignal, err := observeLinuxProcessGroupLeader(pid, anchor)
	if err != nil {
		return err
	}
	if !leaderPresentBeforeSignal && leaderPresentAfterSignal {
		return errors.New("process group leader appeared or was reused during pidfd group probe")
	}
	if errors.Is(signalErr, unix.ESRCH) {
		return syscall.ESRCH
	}
	return nil
}

func captureLinuxProcessGroupAnchor(pid int) (linuxProcessGroupAnchor, error) {
	if pid <= 0 {
		return linuxProcessGroupAnchor{}, errors.New("process pid must be positive")
	}
	stat, err := readLinuxProcessStat(pid)
	if err != nil {
		return linuxProcessGroupAnchor{}, err
	}
	if stat.pid != pid {
		return linuxProcessGroupAnchor{}, fmt.Errorf("process stat pid = %d, want %d", stat.pid, pid)
	}
	if stat.pgrp != int64(pid) {
		return linuxProcessGroupAnchor{}, fmt.Errorf("process %d is in process group %d, want %d", pid, stat.pgrp, pid)
	}
	if stat.session == int64(pid) {
		return linuxProcessGroupAnchor{}, fmt.Errorf("process %d is a session leader; group anchor is unsafe", pid)
	}
	return linuxProcessGroupAnchor{
		pid:       stat.pid,
		pgrp:      int64(stat.pid),
		session:   stat.session,
		startTime: stat.startTime,
	}, nil
}

func readLinuxProcessStatFile(pid int) (linuxProcessStat, error) {
	if pid <= 0 {
		return linuxProcessStat{}, errors.New("process pid must be positive")
	}
	value, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return linuxProcessStat{}, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	stat, err := parseLinuxProcessStat(string(value))
	if err != nil {
		return linuxProcessStat{}, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	if stat.pid != pid {
		return linuxProcessStat{}, fmt.Errorf("read /proc/%d/stat returned pid %d", pid, stat.pid)
	}
	return stat, nil
}

func parseLinuxProcessStat(value string) (linuxProcessStat, error) {
	firstSpace := strings.IndexByte(value, ' ')
	if firstSpace <= 0 {
		return linuxProcessStat{}, errors.New("parse /proc stat: missing pid")
	}
	pid, err := strconv.Atoi(value[:firstSpace])
	if err != nil || pid <= 0 {
		if err == nil {
			err = errors.New("pid must be positive")
		}
		return linuxProcessStat{}, fmt.Errorf("parse /proc stat pid: %w", err)
	}

	closingParenthesis := strings.LastIndexByte(value, ')')
	if closingParenthesis <= firstSpace || closingParenthesis+1 >= len(value) || value[closingParenthesis+1] != ' ' {
		return linuxProcessStat{}, errors.New("parse /proc stat: malformed comm")
	}
	fields := strings.Fields(value[closingParenthesis+1:])
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return linuxProcessStat{}, errors.New("parse /proc stat: missing fields")
	}
	pgrp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return linuxProcessStat{}, fmt.Errorf("parse /proc stat process group: %w", err)
	}
	session, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return linuxProcessStat{}, fmt.Errorf("parse /proc stat session: %w", err)
	}
	startTime, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil || startTime == 0 {
		if err == nil {
			err = errors.New("start time must be positive")
		}
		return linuxProcessStat{}, fmt.Errorf("parse /proc stat start time: %w", err)
	}
	return linuxProcessStat{pid: pid, pgrp: pgrp, session: session, startTime: startTime}, nil
}

func checkProcessGroupProbeBudget(ctx context.Context, deadline time.Time) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
