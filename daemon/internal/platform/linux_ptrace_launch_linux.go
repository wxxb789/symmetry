//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
)

var (
	errLinuxPtraceLaunchResumed  = errors.New("Linux ptrace process is already resumed")
	errLinuxPtraceLaunchStopped  = errors.New("Linux ptrace process is still exec-stopped")
	errLinuxPtraceLaunchClosed   = errors.New("Linux ptrace process is unavailable")
	errLinuxPtraceLaunchFinished = errors.New("Linux ptrace process has already been waited")
)

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

	mutex      sync.Mutex
	resumed    bool
	terminated bool

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
		return nil, fmt.Errorf("%w: Linux ptrace launch requires pidfd process-group support: %v", errors.ErrUnsupported, err)
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

	if err := startLinuxPtraceCommand(command); err != nil {
		return nil, fmt.Errorf("start Linux ptrace process: %w", err)
	}
	process := &LinuxPtraceProcess{
		command: command,
		pid:     command.Process.Pid,
		pidfd:   pidfd,
	}
	if pidfd < 0 {
		return nil, process.abortLaunch(fmt.Errorf("%w: Linux ptrace launch returned no pidfd", errors.ErrUnsupported))
	}

	status, err := waitLinuxPtraceExecStop(process.pid)
	if err != nil {
		return nil, process.abortLaunch(fmt.Errorf("wait for Linux ptrace exec-stop: %w", err))
	}
	if !status.Stopped() || status.StopSignal() != unix.SIGTRAP {
		return nil, process.abortLaunch(fmt.Errorf("wait for Linux ptrace exec-stop: unexpected wait status %#x", uint32(status)))
	}

	identity, err := readProcessIdentity(process.pid)
	if err != nil {
		return nil, process.abortLaunch(fmt.Errorf("capture Linux ptrace process identity: %w", err))
	}
	anchor, err := captureLinuxProcessGroupAnchor(process.pid)
	if err != nil {
		return nil, process.abortLaunch(fmt.Errorf("capture Linux ptrace process-group anchor: %w", err))
	}
	process.identity = identity
	process.anchor = LinuxProcessGroupAnchor{
		PID:       anchor.pid,
		PGRP:      anchor.pgrp,
		Session:   anchor.session,
		StartTime: anchor.startTime,
	}
	return process, nil
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
	defer process.mutex.Unlock()
	if process.waited {
		return errLinuxPtraceLaunchFinished
	}
	if process.terminated {
		return errors.New("Linux ptrace process was terminated before resume")
	}
	if process.resumed {
		return errLinuxPtraceLaunchResumed
	}
	if err := detachLinuxPtrace(process.pid); err != nil {
		return fmt.Errorf("detach Linux ptrace process %d: %w", process.pid, err)
	}
	process.resumed = true
	return nil
}

// Terminate forcefully signals the retained pidfd-owned process group. It is
// valid while the target is still exec-stopped and never falls back to a
// numeric PID or PGID signal.
func (process *LinuxPtraceProcess) Terminate() error {
	if process == nil {
		return errLinuxPtraceLaunchClosed
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	if process.waited || process.terminated {
		return nil
	}
	if process.pidfd < 0 {
		return fmt.Errorf("terminate Linux ptrace process %d: %w", process.pid, errors.ErrUnsupported)
	}
	err := sendPIDFDSignal(process.pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	if err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("terminate Linux ptrace process group %d: %w", process.pid, err)
	}
	process.terminated = true
	return nil
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

	waitErr := process.command.Wait()
	code := -1
	if process.command.ProcessState != nil {
		code = process.command.ProcessState.ExitCode()
	}
	closeErr := process.releasePIDFD()

	process.mutex.Lock()
	process.waitCode = code
	process.waitErr = errors.Join(waitErr, closeErr)
	process.waited = true
	process.waitStarted = false
	close(done)
	process.mutex.Unlock()
	return code, process.waitErr
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

func (process *LinuxPtraceProcess) abortLaunch(cause error) error {
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
