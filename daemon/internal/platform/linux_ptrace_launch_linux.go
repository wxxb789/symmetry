//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	startLinuxPtraceCommand = func(command *exec.Cmd) error {
		return command.Start()
	}
	waitLinuxPtraceExecStop = waitLinuxPtraceExecStopFor
	detachLinuxPtrace       = unix.PtraceDetach
	linuxPtraceThreadID     = unix.Gettid
)

var (
	errLinuxPtraceLaunchResumed  = errors.New("Linux ptrace process is already resumed")
	errLinuxPtraceLaunchStopped  = errors.New("Linux ptrace process is still exec-stopped")
	errLinuxPtraceLaunchClosed   = errors.New("Linux ptrace process is unavailable")
	errLinuxPtraceLaunchFinished = errors.New("Linux ptrace process has already been waited")
	errLinuxPtraceOwnerClosed    = errors.New("Linux ptrace owner thread is unavailable")
)

// linuxPtraceOwner serializes every ptrace-sensitive operation for one
// tracee. The goroutine is pinned once for its entire lifetime; callers never
// pin and unpin arbitrary goroutines around individual operations.
type linuxPtraceOwner struct {
	requests chan linuxPtraceOwnerRequest
	done     chan struct{}

	mutex  sync.Mutex
	closed bool
}

type linuxPtraceOwnerRequest struct {
	operation func() error
	result    chan error
}

func newLinuxPtraceOwner() *linuxPtraceOwner {
	owner := &linuxPtraceOwner{
		requests: make(chan linuxPtraceOwnerRequest),
		done:     make(chan struct{}),
	}
	go owner.run()
	return owner
}

func (owner *linuxPtraceOwner) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(owner.done)
	for request := range owner.requests {
		request.result <- request.operation()
	}
}

func (owner *linuxPtraceOwner) call(operation func() error) error {
	if owner == nil || operation == nil {
		return errLinuxPtraceOwnerClosed
	}
	result := make(chan error, 1)
	owner.mutex.Lock()
	if owner.closed {
		owner.mutex.Unlock()
		return errLinuxPtraceOwnerClosed
	}
	owner.requests <- linuxPtraceOwnerRequest{operation: operation, result: result}
	owner.mutex.Unlock()
	return <-result
}

func (owner *linuxPtraceOwner) close() {
	if owner == nil {
		return
	}
	owner.mutex.Lock()
	if !owner.closed {
		owner.closed = true
		close(owner.requests)
	}
	owner.mutex.Unlock()
	<-owner.done
}

// LinuxProcessGroupAnchor is the creation-time fence for a Linux process
// group leader. The process must remain in this group and session for the
// anchor to remain valid.
type LinuxProcessGroupAnchor struct {
	PID       int
	PGRP      int64
	Session   int64
	StartTime uint64
}

// LinuxPtraceProcess owns a child held at the ptrace exec-stop. It is a small
// launch boundary for later supervisor integration; it does not own a cgroup
// or any durable state.
type LinuxPtraceProcess struct {
	command  *exec.Cmd
	pid      int
	pidfd    int
	identity string
	anchor   LinuxProcessGroupAnchor
	owner    *linuxPtraceOwner

	mutex            sync.Mutex
	resumed          bool
	terminated       bool
	ownerThreadID    int
	resumeThreadID   int
	abortThreadID    int
	lastWaitThreadID int

	waitStarted bool
	waited      bool
	waitDone    chan struct{}
	waitCode    int
	waitErr     error
}

// LaunchLinuxPtrace starts command with a fresh process group, direct-child
// death protection, ptrace tracing, and a retained pidfd. It returns only
// after the tracee has reached the SIGTRAP exec-stop, before its first user
// instruction can run, and after strict identity and group-anchor capture.
func LaunchLinuxPtrace(command *exec.Cmd) (*LinuxPtraceProcess, error) {
	if command == nil {
		return nil, errors.New("Linux ptrace command is required")
	}
	if command.Process != nil {
		return nil, errors.New("Linux ptrace command has already started")
	}
	if err := probePIDFDGroupSupport(); err != nil {
		return nil, fmt.Errorf("%w: Linux ptrace launch requires pidfd process-group support: %w", errors.ErrUnsupported, err)
	}

	pidfd := -1
	attributes := command.SysProcAttr
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	} else {
		copy := *attributes
		attributes = &copy
	}
	if attributes.Setsid {
		return nil, errors.New("Linux ptrace launch cannot combine Setsid with a process-group anchor")
	}
	if attributes.Foreground {
		return nil, errors.New("Linux ptrace launch cannot combine Foreground with a process-group anchor")
	}
	if attributes.Pgid != 0 {
		return nil, errors.New("Linux ptrace launch requires Pgid to be zero")
	}
	if attributes.Pdeathsig != 0 && attributes.Pdeathsig != syscall.SIGKILL {
		return nil, errors.New("Linux ptrace launch requires Pdeathsig SIGKILL")
	}
	attributes.Setpgid = true
	attributes.Pgid = 0
	attributes.Pdeathsig = syscall.SIGKILL
	attributes.Ptrace = true
	attributes.PidFD = &pidfd
	command.SysProcAttr = attributes

	process := &LinuxPtraceProcess{
		command: command,
		pidfd:   -1,
		owner:   newLinuxPtraceOwner(),
	}
	if err := process.owner.call(func() error { return process.launchOnOwner(&pidfd) }); err != nil {
		process.owner.close()
		return nil, err
	}
	return process, nil
}

func (process *LinuxPtraceProcess) launchOnOwner(pidfd *int) error {
	process.ownerThreadID = linuxPtraceThreadID()
	if err := startLinuxPtraceCommand(process.command); err != nil {
		return fmt.Errorf("start Linux ptrace process: %w", err)
	}
	process.pid = process.command.Process.Pid
	process.pidfd = *pidfd
	if process.pidfd < 0 {
		return process.abortLaunchOnOwner(fmt.Errorf("%w: Linux ptrace launch returned no pidfd", errors.ErrUnsupported))
	}

	status, err := waitLinuxPtraceExecStop(process.pid)
	if err != nil {
		return process.abortLaunchOnOwner(fmt.Errorf("wait for Linux ptrace exec-stop: %w", err))
	}
	if !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
		return process.abortLaunchOnOwner(fmt.Errorf("wait for Linux ptrace exec-stop: unexpected wait status %#x", uint32(status)))
	}

	identity, err := readProcessIdentity(process.pid)
	if err != nil {
		return process.abortLaunchOnOwner(fmt.Errorf("capture Linux ptrace process identity: %w", err))
	}
	anchor, err := captureLinuxProcessGroupAnchor(process.pid)
	if err != nil {
		return process.abortLaunchOnOwner(fmt.Errorf("capture Linux ptrace process-group anchor: %w", err))
	}
	process.identity = identity
	process.anchor = LinuxProcessGroupAnchor{
		PID:       anchor.pid,
		PGRP:      anchor.pgrp,
		Session:   anchor.session,
		StartTime: anchor.startTime,
	}
	return nil
}

// PID returns the target process identifier.
func (process *LinuxPtraceProcess) PID() int {
	if process == nil {
		return 0
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.pid
}

// Identity returns the strict process creation identity captured at the
// exec-stop.
func (process *LinuxPtraceProcess) Identity() string {
	if process == nil {
		return ""
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.identity
}

// ProcessGroupAnchor returns the exact group/session/start-time fence captured
// while the target was still ptrace-stopped.
func (process *LinuxPtraceProcess) ProcessGroupAnchor() LinuxProcessGroupAnchor {
	if process == nil {
		return LinuxProcessGroupAnchor{}
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.anchor
}

// Resume performs the one ptrace detach that releases the target from its
// exec-stop. Calling Resume twice is rejected.
func (process *LinuxPtraceProcess) Resume() error {
	if process == nil {
		return errLinuxPtraceLaunchClosed
	}
	process.mutex.Lock()
	if process.waited {
		process.mutex.Unlock()
		return errLinuxPtraceLaunchFinished
	}
	if process.terminated {
		process.mutex.Unlock()
		return errors.New("Linux ptrace process was terminated before resume")
	}
	if process.resumed {
		process.mutex.Unlock()
		return errLinuxPtraceLaunchResumed
	}
	owner := process.owner
	pid := process.pid
	process.mutex.Unlock()
	return owner.call(func() error {
		if err := detachLinuxPtrace(pid); err != nil {
			return fmt.Errorf("detach Linux ptrace process %d: %w", pid, err)
		}
		process.mutex.Lock()
		process.resumeThreadID = linuxPtraceThreadID()
		process.resumed = true
		process.mutex.Unlock()
		return nil
	})
}

// Terminate forcefully signals the retained pidfd-owned process group. It is
// valid while the target is still exec-stopped and never falls back to a
// numeric PID or PGID signal.
func (process *LinuxPtraceProcess) Terminate() error {
	if process == nil {
		return errLinuxPtraceLaunchClosed
	}
	process.mutex.Lock()
	if process.waited || process.terminated {
		process.mutex.Unlock()
		return nil
	}
	if process.pidfd < 0 {
		if process.waitStarted {
			process.mutex.Unlock()
			return nil
		}
		process.mutex.Unlock()
		return fmt.Errorf("terminate Linux ptrace process %d: %w", process.pid, errors.ErrUnsupported)
	}
	owner := process.owner
	pidfd := process.pidfd
	pid := process.pid
	preResume := !process.resumed
	process.mutex.Unlock()
	terminate := func() error {
		process.mutex.Lock()
		defer process.mutex.Unlock()
		if process.waited || process.pidfd != pidfd {
			return nil
		}
		err := sendPIDFDSignal(pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
		if err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("terminate Linux ptrace process group %d: %w", pid, err)
		}
		process.terminated = true
		if preResume {
			process.abortThreadID = linuxPtraceThreadID()
		}
		return nil
	}
	if preResume {
		return owner.call(terminate)
	}
	return terminate()
}

// Wait blocks until the target exits and returns its process exit code. The
// target must be resumed first unless Terminate was called while it was
// exec-stopped.
func (process *LinuxPtraceProcess) Wait() (int, error) {
	if process == nil {
		return -1, errLinuxPtraceLaunchClosed
	}
	process.mutex.Lock()
	if process.waited {
		code, err := process.waitCode, process.waitErr
		process.mutex.Unlock()
		return code, err
	}
	if process.waitStarted {
		done := process.waitDone
		process.mutex.Unlock()
		<-done
		process.mutex.Lock()
		code, err := process.waitCode, process.waitErr
		process.mutex.Unlock()
		return code, err
	}
	if !process.resumed && !process.terminated {
		process.mutex.Unlock()
		return -1, errLinuxPtraceLaunchStopped
	}
	process.waitStarted = true
	process.waitDone = make(chan struct{})
	done := process.waitDone
	process.mutex.Unlock()

	code := -1
	waitErr := process.owner.call(func() error {
		process.lastWaitThreadID = linuxPtraceThreadID()
		waitErr := process.command.Wait()
		if process.command.ProcessState != nil {
			code = process.command.ProcessState.ExitCode()
		}
		return waitErr
	})
	closeErr := process.releasePIDFD()

	process.mutex.Lock()
	process.waitCode = code
	process.waitErr = errors.Join(waitErr, closeErr)
	process.waited = true
	process.waitStarted = false
	resultErr := process.waitErr
	close(done)
	process.mutex.Unlock()
	process.owner.close()
	return code, resultErr
}

func (process *LinuxPtraceProcess) releasePIDFD() error {
	process.mutex.Lock()
	fd := process.pidfd
	process.pidfd = -1
	process.mutex.Unlock()
	if fd < 0 {
		return nil
	}
	if err := closePIDFD(fd); err != nil {
		return fmt.Errorf("close Linux ptrace process pidfd: %w", err)
	}
	return nil
}

func (process *LinuxPtraceProcess) abortLaunchOnOwner(cause error) error {
	process.abortThreadID = linuxPtraceThreadID()
	var cleanupErr error
	if process.pidfd >= 0 {
		if err := sendPIDFDSignal(process.pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil && !errors.Is(err, unix.ESRCH) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate failed Linux ptrace launch: %w", err))
		}
	} else if process.command != nil && process.command.Process != nil {
		if err := process.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, unix.ESRCH) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate failed Linux ptrace launch: %w", err))
		}
	}
	if process.command != nil && process.command.Process != nil {
		if err := process.command.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, unix.ECHILD) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("wait for failed Linux ptrace launch: %w", err))
		}
	}
	if err := process.releasePIDFD(); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return errors.Join(cause, cleanupErr)
}

func waitLinuxPtraceExecStopFor(pid int) (unix.WaitStatus, error) {
	if pid <= 0 {
		return 0, errors.New("Linux ptrace process pid must be positive")
	}
	for {
		var status unix.WaitStatus
		waitedPID, err := unix.Wait4(pid, &status, 0, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if waitedPID != pid {
			return 0, fmt.Errorf("waited for pid %d, got pid %d", pid, waitedPID)
		}
		return status, nil
	}
}
