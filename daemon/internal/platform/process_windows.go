//go:build windows

// Package platform contains process-control primitives that differ by OS.
package platform

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
	processTerminate                  = 0x0001
	processSetQuota                   = 0x0100
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createJobObject          = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	terminateJobObject       = kernel32.NewProc("TerminateJobObject")
)

// Containment owns the platform mechanism that keeps all descendants under the
// lifecycle of a launched process.
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
	mutex  sync.Mutex
	handle syscall.Handle
	pid    int
}

// ConfigureProcess has no setup work on Windows. taskkill targets the process
// tree by PID, so no Job Object or shell wrapper is required.
func ConfigureProcess(command *exec.Cmd) {
}

// AttachProcess adds the root process to a fresh Job Object. The kill-on-close
// limit makes descendants die even after the root exits or they are reparented.
func AttachProcess(pid int) (Containment, error) {
	handle, _, callError := createJobObject.Call(0, 0)
	if handle == 0 {
		return nil, fmt.Errorf("create job object: %w", callError)
	}
	job := syscall.Handle(handle)
	cleanup := func(errorValue error) (Containment, error) {
		_ = syscall.CloseHandle(job)
		return nil, errorValue
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

	process, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(pid))
	if err != nil {
		return cleanup(fmt.Errorf("open process for job assignment: %w", err))
	}
	defer syscall.CloseHandle(process)

	result, _, callError = assignProcessToJobObject.Call(uintptr(job), uintptr(process))
	if result == 0 {
		return cleanup(fmt.Errorf("assign process to job object: %w", callError))
	}
	return &jobContainment{handle: job, pid: pid}, nil
}

func (job *jobContainment) Terminate(force bool) error {
	job.mutex.Lock()
	defer job.mutex.Unlock()
	handle := job.handle
	pid := job.pid
	if handle == 0 {
		return nil
	}
	if force {
		result, _, callError := terminateJobObject.Call(uintptr(handle), 1)
		if result == 0 {
			return fmt.Errorf("terminate job object: %w", callError)
		}
		return nil
	}
	return terminateWithTaskkill(pid, false)
}

// Close releases the final Job Object handle. KILL_ON_JOB_CLOSE then stops all
// remaining descendants, including reparented processes.
func (job *jobContainment) Close() error {
	job.mutex.Lock()
	handle := job.handle
	job.handle = 0
	job.mutex.Unlock()
	if handle == 0 {
		return nil
	}
	if err := syscall.CloseHandle(handle); err != nil {
		return fmt.Errorf("close job object: %w", err)
	}
	return nil
}

// terminateWithTaskkill uses the Windows task manager utility directly. /T
// includes descendants; /F is reserved for the post-grace forced attempt.
func terminateWithTaskkill(pid int, force bool) error {
	arguments := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}

	output, err := exec.Command("taskkill", arguments...).CombinedOutput()
	if err == nil || processIsAlreadyGone(string(output)) {
		return nil
	}
	return fmt.Errorf("taskkill process tree %d: %w: %s", pid, err, strings.TrimSpace(string(output)))
}

func processIsAlreadyGone(output string) bool {
	normalized := strings.ToLower(output)
	return strings.Contains(normalized, "not found") ||
		strings.Contains(normalized, "no running instance") ||
		strings.Contains(normalized, "does not exist")
}
