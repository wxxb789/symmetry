//go:build linux

// Package platform contains process-control primitives that differ by OS.
package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
)

// Containment owns a launched process's platform-specific termination boundary.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

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
// identity while the caller still owns the unreaped process handle. It owns only
// the process group established by ConfigureProcess, not processes outside it.
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
		identity, callbackErr = readProcessIdentity(process.Pid)
		if callbackErr != nil {
			callbackErr = fmt.Errorf("capture process creation identity: %w", callbackErr)
		}
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
	closeCompleted bool
	closeErr       error
}

func (group *processGroup) Terminate(force bool) error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closeCompleted {
		return group.closeErr
	}
	return group.terminateLocked(force)
}

func (group *processGroup) terminateLocked(force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := group.signalLocked(unix.Signal(signal)); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}

func (group *processGroup) signalLocked(signal unix.Signal) error {
	if group.fd < 0 {
		return fmt.Errorf("%w: retained pidfd is unavailable", errors.ErrUnsupported)
	}
	return sendPIDFDSignal(group.fd, signal, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
}

// Close kills the owned process group, then observes that the group is empty.
// A successful return is proof only for members of the original process group.
func (group *processGroup) Close() error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closeCompleted {
		return group.closeErr
	}
	group.closeCompleted = true

	deadline := containmentDeadline(context.Background())
	var stopErr error
	if err := group.terminateLocked(true); err != nil {
		stopErr = fmt.Errorf("terminate process group %d during containment close: %w", group.pid, err)
	} else {
		stopErr = waitForProcessGroupExit(context.Background(), group.pid, deadline, func() error {
			return group.signalLocked(0)
		})
	}
	group.closeErr = errors.Join(stopErr, group.releasePIDFDLocked())
	return group.closeErr
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
		terminationErr = terminatePIDFDProcessGroup(ctx, int(handle), process.Pid)
	}); err != nil {
		return fmt.Errorf("borrow process handle for group termination: %w", err)
	}
	return terminationErr
}

func terminatePIDFDProcessGroup(ctx context.Context, fd, pid int) error {
	deadline := containmentDeadline(ctx)
	if err := sendPIDFDSignal(fd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("terminate process group %d: %w", pid, err)
	}
	return waitForProcessGroupExit(ctx, pid, deadline, func() error {
		return sendPIDFDSignal(fd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
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
