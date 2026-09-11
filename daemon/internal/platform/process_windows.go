//go:build windows

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
	"unsafe"
)

const (
	containmentCloseDeadline            = 5 * time.Second
	containmentCloseProbeInterval       = 10 * time.Millisecond
	jobObjectBasicAccountingInformation = 1
	jobObjectExtendedLimitInformation   = 9
	jobObjectLimitKillOnJobClose        = 0x00002000
)

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	createJobObject                  = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObject          = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject         = kernel32.NewProc("AssignProcessToJobObject")
	terminateJobObject               = kernel32.NewProc("TerminateJobObject")
	queryInformationJobObject        = kernel32.NewProc("QueryInformationJobObject")
	terminateProcessForAttachFailure = terminateFailedAttachProcess
	terminateJob                     = terminateOwnedJob
	queryJobActiveProcesses          = queryOwnedJobActiveProcesses
	closeJob                         = syscall.CloseHandle
	waitForEmptyJob                  = waitForJobToEmpty
	readProcessIdentityFromHandle    = processIdentityFromHandle
)

// Containment owns a launched process's platform-specific termination boundary.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

type basicLimitInformation struct {
	perProcessUserTimeLimit int64
	perJobUserTimeLimit     int64
	limitFlags              uint32
	minimumWorkingSetSize   uintptr
	maximumWorkingSetSize   uintptr
	activeProcessLimit      uint32
	affinity                uintptr
	priorityClass           uint32
	schedulingClass         uint32
}

type basicAccountingInformation struct {
	totalUserTime             int64
	totalKernelTime           int64
	thisPeriodTotalUserTime   int64
	thisPeriodTotalKernelTime int64
	totalPageFaultCount       uint32
	totalProcesses            uint32
	activeProcesses           uint32
	totalTerminatedProcesses  uint32
}

type ioCounters struct {
	readOperationCount  uint64
	writeOperationCount uint64
	otherOperationCount uint64
	readTransferCount   uint64
	writeTransferCount  uint64
	otherTransferCount  uint64
}

type extendedLimitInformation struct {
	basicLimitInformation basicLimitInformation
	ioInfo                ioCounters
	processMemoryLimit    uintptr
	jobMemoryLimit        uintptr
	peakProcessMemoryUsed uintptr
	peakJobMemoryUsed     uintptr
}

type jobContainment struct {
	mutex        sync.Mutex
	handle       syscall.Handle
	closeStarted bool
	closeErr     error
}

// ConfigureProcess leaves inherited Job Object handling to AttachProcess.
// CREATE_BREAKAWAY_FROM_JOB fails before the child starts when an inherited
// CI Job Object does not permit it, while AssignProcessToJobObject retains the
// existing fail-closed containment path.
func ConfigureProcess(_ *exec.Cmd) error { return nil }

// AttachProcess adds the root process to a fresh Job Object. The job owns the
// root and descendants assigned after this call; processes that escape before
// post-Start assignment or deliberately break away are outside this boundary.
func AttachProcess(process *os.Process) (Containment, string, error) {
	if process == nil || process.Pid <= 0 {
		return nil, "", errors.New("process handle is required for containment")
	}

	handle, _, callError := createJobObject.Call(0, 0)
	if handle == 0 {
		return cleanupFailedAttach(process, 0, fmt.Errorf("create job object: %w", callError))
	}
	job := syscall.Handle(handle)
	cleanup := func(errorValue error) (Containment, string, error) {
		return cleanupFailedAttach(process, job, errorValue)
	}

	limits := extendedLimitInformation{}
	limits.basicLimitInformation.limitFlags = jobObjectLimitKillOnJobClose
	result, _, callError := setInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uintptr(unsafe.Sizeof(limits)),
	)
	if result == 0 {
		return cleanup(fmt.Errorf("configure job object: %w", callError))
	}

	contained := &jobContainment{handle: job}
	var identity string
	var callbackErr error
	var assigned bool
	if err := process.WithHandle(func(processHandle uintptr) {
		result, _, callError = assignProcessToJobObject.Call(uintptr(job), processHandle)
		if result == 0 {
			callbackErr = fmt.Errorf("assign process to job object: %w", callError)
			return
		}
		assigned = true
		identity, callbackErr = readProcessIdentityFromHandle(process.Pid, syscall.Handle(processHandle))
		if callbackErr != nil {
			callbackErr = fmt.Errorf("capture process creation identity: %w", callbackErr)
		}
	}); err != nil {
		return cleanup(fmt.Errorf("borrow process handle for job assignment: %w", err))
	}
	if callbackErr != nil {
		if assigned {
			return contained, identity, callbackErr
		}
		return cleanup(callbackErr)
	}
	return contained, identity, nil
}

func cleanupFailedAttach(process *os.Process, job syscall.Handle, attachErr error) (Containment, string, error) {
	terminateErr := terminateProcessForAttachFailure(process)
	var closeErr error
	if job != 0 {
		if err := closeJob(job); err != nil {
			closeErr = fmt.Errorf("close unassigned job object: %w", err)
		}
	}
	if terminateErr != nil || closeErr != nil {
		cleanupErr := closeErr
		if terminateErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate process after containment setup failure: %w", terminateErr))
		}
		return nil, "", errors.Join(attachErr, cleanupErr)
	}
	return nil, "", attachErr
}

func terminateFailedAttachProcess(process *os.Process) error {
	if process == nil {
		return errors.New("process handle is required")
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func (job *jobContainment) Terminate(force bool) error {
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.closeStarted {
		return job.closeErr
	}
	handle := job.handle
	if handle == 0 {
		return nil
	}
	if force {
		if err := terminateJob(handle); err != nil {
			return fmt.Errorf("terminate job object: %w", err)
		}
		return nil
	}
	return fmt.Errorf("%w: soft process-tree termination is unavailable on Windows", errors.ErrUnsupported)
}

// Close terminates the owned Job Object and observes an empty Job before
// releasing its final handle. If proof fails, it still best-effort releases the
// handle to trigger KILL_ON_JOB_CLOSE. Later calls may retry only a failed
// handle release; they preserve the original stop-proof error and never signal
// the Job Object again.
func (job *jobContainment) Close() error {
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.closeStarted {
		_ = job.releaseHandleLocked()
		return job.closeErr
	}
	job.closeStarted = true

	handle := job.handle
	if handle == 0 {
		return nil
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	var stopErr error
	if err := terminateJob(handle); err != nil {
		stopErr = fmt.Errorf("terminate job object during containment close: %w", err)
	} else if err := waitForEmptyJob(handle, deadline, queryJobActiveProcesses); err != nil {
		stopErr = err
	}
	job.closeErr = errors.Join(stopErr, job.releaseHandleLocked())
	return job.closeErr
}

func (job *jobContainment) releaseHandleLocked() error {
	handle := job.handle
	if handle == 0 {
		return nil
	}
	if err := closeJob(handle); err != nil {
		return fmt.Errorf("close job object after containment close: %w", err)
	}
	job.handle = 0
	return nil
}

func terminateOwnedJob(handle syscall.Handle) error {
	result, _, callError := terminateJobObject.Call(uintptr(handle), 1)
	if result == 0 {
		return callError
	}
	return nil
}

func queryOwnedJobActiveProcesses(handle syscall.Handle) (uint32, error) {
	accounting := basicAccountingInformation{}
	result, _, callError := queryInformationJobObject.Call(
		uintptr(handle),
		jobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&accounting)),
		uintptr(unsafe.Sizeof(accounting)),
		0,
	)
	if result == 0 {
		return 0, callError
	}
	return accounting.activeProcesses, nil
}

func waitForJobToEmpty(handle syscall.Handle, deadline time.Time, query func(syscall.Handle) (uint32, error)) error {
	for {
		active, err := query(handle)
		if err != nil {
			return fmt.Errorf("query job object after containment close: %w", err)
		}
		if active == 0 {
			return nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("job object remained non-empty after containment close deadline")
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		time.Sleep(remaining)
	}
}

// TerminateProcessGroup is unavailable on Windows because Job Objects, not
// process groups, provide the supported containment primitive here.
func TerminateProcessGroup(context.Context, *os.Process, string) error {
	return fmt.Errorf("%w: pidfd process-group termination is unavailable on Windows", errors.ErrUnsupported)
}
