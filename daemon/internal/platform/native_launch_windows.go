//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// x/sys/windows v0.48.0 does not expose this Windows 10 attribute yet.
	procThreadAttributeJobList = 0x0002000d
	nativeWaitTimeout          = 0x00000102
)

var (
	errNativeLaunchClosed      = errors.New("native suspended process is closed")
	errNativeLaunchClosing     = errors.New("native suspended process is closing")
	errNativeLaunchResumed     = errors.New("native suspended process is already resumed")
	getNativeHandleInformation = kernel32.NewProc("GetHandleInformation")
)

// NativeLaunchSpec is the explicit input to LaunchSuspended.
//
// If CommandLine is non-empty it is passed verbatim (apart from the required
// terminating NUL). Otherwise Argv is escaped with windows.ComposeCommandLine.
// Application is passed as CreateProcessW's application name and may be empty
// when Argv[0] is sufficient for Windows path resolution. A nil Env inherits
// the current environment; a non-nil Env is passed as an explicit Unicode
// environment block. Cwd is the child working directory. Headless adds
// CREATE_NO_WINDOW and hides the startup window.
type NativeLaunchSpec struct {
	Application string
	Argv        []string
	CommandLine string
	Env         []string
	Cwd         string
	Headless    bool
	// Stdin, Stdout and Stderr are the exact handles exposed to the child.
	// Either all three must be nil or all three must be non-nil. When set,
	// LaunchSuspended marks only these handles inheritable and restricts the
	// child with PROC_THREAD_ATTRIBUTE_HANDLE_LIST.
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
}

// SuspendedProcess owns the primary process, primary thread, and containment
// Job Object returned by LaunchSuspended until AttachSuspendedProcess transfers
// the Job to the shared containment owner. The process starts suspended and is
// assigned to the Job before any child code runs.
type SuspendedProcess struct {
	mu sync.Mutex

	process windows.Handle
	thread  windows.Handle
	job     windows.Handle
	jobID   string
	pid     uint32

	resumed     bool
	terminated  bool
	closing     bool
	closed      bool
	closeDone   chan struct{}
	waiters     int
	waitersDone chan struct{}
	closeErr    error
	exitCode    uint32
	exitKnown   bool
}

// LaunchSuspended creates a suspended process in a fresh KILL_ON_JOB_CLOSE Job.
// The job is attached through PROC_THREAD_ATTRIBUTE_JOB_LIST, avoiding the
// post-CreateProcess assignment window present in legacy launch paths.
func LaunchSuspended(spec NativeLaunchSpec) (*SuspendedProcess, error) {
	application, commandLine, cwd, environment, err := prepareNativeLaunch(spec)
	if err != nil {
		return nil, err
	}
	standardHandles, restoreHandles, err := prepareNativeStandardHandles(spec)
	if err != nil {
		return nil, err
	}
	defer restoreHandles()

	job, jobID, err := createNamedContainmentJob()
	if err != nil {
		return nil, fmt.Errorf("create native launch job: %w", err)
	}
	keepJob := false
	defer func() {
		if !keepJob {
			_ = windows.CloseHandle(windows.Handle(job))
		}
	}()

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		windows.Handle(job),
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		return nil, fmt.Errorf("configure native launch job: %w", err)
	}

	attributeCount := uint32(1)
	if len(standardHandles) != 0 {
		attributeCount++
	}
	attributes, err := windows.NewProcThreadAttributeList(attributeCount)
	if err != nil {
		return nil, fmt.Errorf("allocate native launch attributes: %w", err)
	}
	defer attributes.Delete()

	jobValue := job
	if err := attributes.Update(
		procThreadAttributeJobList,
		unsafe.Pointer(&jobValue),
		unsafe.Sizeof(jobValue),
	); err != nil {
		return nil, fmt.Errorf("configure native launch Job Object attribute: %w", err)
	}
	if len(standardHandles) != 0 {
		if err := attributes.Update(
			windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
			unsafe.Pointer(&standardHandles[0]),
			uintptr(len(standardHandles))*unsafe.Sizeof(standardHandles[0]),
		); err != nil {
			return nil, fmt.Errorf("configure native launch handle attribute: %w", err)
		}
	}

	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
		},
		ProcThreadAttributeList: attributes.List(),
	}
	creationFlags := uint32(windows.CREATE_SUSPENDED | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if spec.Headless {
		creationFlags |= windows.CREATE_NO_WINDOW
		startup.Flags |= windows.STARTF_USESHOWWINDOW
		startup.ShowWindow = windows.SW_HIDE
	}
	if len(standardHandles) != 0 {
		startup.Flags |= windows.STARTF_USESTDHANDLES
		startup.StdInput = standardHandles[0]
		startup.StdOutput = standardHandles[1]
		startup.StdErr = standardHandles[2]
	}

	var processInfo windows.ProcessInformation
	inheritsHandles := len(standardHandles) != 0
	if err := windows.CreateProcess(
		application,
		commandLine,
		nil,
		nil,
		inheritsHandles,
		creationFlags,
		environment,
		cwd,
		&startup.StartupInfo,
		&processInfo,
	); err != nil {
		return nil, fmt.Errorf("create suspended native process: %w", err)
	}
	runtime.KeepAlive(spec.Stdin)
	runtime.KeepAlive(spec.Stdout)
	runtime.KeepAlive(spec.Stderr)

	keepJob = true
	return &SuspendedProcess{
		process:   processInfo.Process,
		thread:    processInfo.Thread,
		job:       windows.Handle(job),
		jobID:     jobID,
		pid:       processInfo.ProcessId,
		closeDone: make(chan struct{}),
	}, nil
}

// JobID returns the random identifier embedded in the Local named Job Object
// created for this suspended process. It is safe to expose to the durable
// journal because it is an object name, not a credential.
func (process *SuspendedProcess) JobID() string {
	if process == nil {
		return ""
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.jobID
}

// prepareNativeStandardHandles duplicates the requested stdio handles into
// inheritable launch-owned handles. The handle-list attribute still limits the
// child to exactly these three handles; duplicating instead of mutating the
// caller's handles avoids inheritance-flag races between concurrent launches.
func prepareNativeStandardHandles(spec NativeLaunchSpec) ([]windows.Handle, func(), error) {
	files := []*os.File{spec.Stdin, spec.Stdout, spec.Stderr}
	present := 0
	for _, file := range files {
		if file != nil {
			present++
		}
	}
	if present == 0 {
		return nil, func() {}, nil
	}
	if present != len(files) {
		return nil, func() {}, errors.New("native launch standard handles must be all set or all nil")
	}

	duplicates := make([]windows.Handle, 0, len(files))
	seen := make(map[windows.Handle]windows.Handle, len(files))
	closeDuplicates := func() {
		for _, handle := range duplicates {
			_ = windows.CloseHandle(handle)
		}
	}
	handles := make([]windows.Handle, len(files))
	for index, file := range files {
		source := windows.Handle(file.Fd())
		if source == 0 || source == windows.InvalidHandle {
			closeDuplicates()
			return nil, func() {}, errors.New("native launch standard handle is invalid")
		}
		if duplicate, ok := seen[source]; ok {
			handles[index] = duplicate
			continue
		}
		var duplicate windows.Handle
		if err := windows.DuplicateHandle(
			windows.CurrentProcess(),
			source,
			windows.CurrentProcess(),
			&duplicate,
			0,
			true,
			windows.DUPLICATE_SAME_ACCESS,
		); err != nil {
			closeDuplicates()
			return nil, func() {}, fmt.Errorf("duplicate native launch standard handle: %w", err)
		}
		duplicates = append(duplicates, duplicate)
		seen[source] = duplicate
		handles[index] = duplicate
	}

	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		closeDuplicates()
	}
	return handles, cleanup, nil
}

func nativeHandleInformation(handle windows.Handle) (uint32, error) {
	var flags uint32
	result, _, callError := getNativeHandleInformation.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&flags)),
	)
	if result == 0 {
		return 0, callError
	}
	return flags, nil
}

func prepareNativeLaunch(spec NativeLaunchSpec) (*uint16, *uint16, *uint16, *uint16, error) {
	if spec.CommandLine != "" && len(spec.Argv) != 0 {
		return nil, nil, nil, nil, errors.New("native launch command line and argv are mutually exclusive")
	}
	if spec.Application == "" && spec.CommandLine == "" && len(spec.Argv) == 0 {
		return nil, nil, nil, nil, errors.New("native launch application or argv is required")
	}

	var application *uint16
	var err error
	if spec.Application != "" {
		application, err = windows.UTF16PtrFromString(spec.Application)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("encode native launch application: %w", err)
		}
	}

	commandLineText := spec.CommandLine
	if commandLineText == "" {
		if len(spec.Argv) != 0 {
			commandLineText = windows.ComposeCommandLine(spec.Argv)
		} else {
			commandLineText = windows.ComposeCommandLine([]string{spec.Application})
		}
	}
	var commandLine *uint16
	if commandLineText != "" {
		commandLine, err = windows.UTF16PtrFromString(commandLineText)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("encode native launch command line: %w", err)
		}
	}

	var cwd *uint16
	if spec.Cwd != "" {
		cwd, err = windows.UTF16PtrFromString(spec.Cwd)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("encode native launch working directory: %w", err)
		}
	}

	environment, err := nativeEnvironmentBlock(spec.Env)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return application, commandLine, cwd, environment, nil
}

func nativeEnvironmentBlock(env []string) (*uint16, error) {
	block, err := nativeEnvironmentBlockValues(env)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	return &block[0], nil
}

func nativeEnvironmentBlockValues(env []string) ([]uint16, error) {
	if env == nil {
		return nil, nil
	}

	entries := append([]string(nil), env...)
	for _, entry := range entries {
		if entry == "" {
			return nil, errors.New("native launch environment contains an empty entry")
		}
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, errors.New("native launch environment contains NUL")
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToUpper(environmentName(entries[i])) < strings.ToUpper(environmentName(entries[j]))
	})

	block := make([]uint16, 0)
	for _, entry := range entries {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, fmt.Errorf("encode native launch environment: %w", err)
		}
		block = append(block, encoded[:len(encoded)-1]...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = []uint16{0, 0}
	} else {
		block = append(block, 0)
	}
	return block, nil
}

func environmentName(entry string) string {
	if index := strings.IndexByte(entry, '='); index >= 0 {
		return entry[:index]
	}
	return entry
}

// PID returns the process identifier assigned by CreateProcessW.
func (process *SuspendedProcess) PID() uint32 {
	if process == nil {
		return 0
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.pid
}

// Resume releases the primary thread's initial suspend count.
func (process *SuspendedProcess) Resume() error {
	if process == nil {
		return errNativeLaunchClosed
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return errNativeLaunchClosed
	}
	if process.closing {
		return errNativeLaunchClosing
	}
	if process.resumed {
		return errNativeLaunchResumed
	}
	if _, err := windows.ResumeThread(process.thread); err != nil {
		return fmt.Errorf("resume native process %d: %w", process.pid, err)
	}
	process.resumed = true
	return nil
}

// Terminate forcefully ends the process with exitCode. It leaves the handles
// owned so that the caller can still Wait and then Close them.
func (process *SuspendedProcess) Terminate(exitCode uint32) error {
	if process == nil {
		return errNativeLaunchClosed
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return errNativeLaunchClosed
	}
	if process.closing {
		return errNativeLaunchClosing
	}
	if process.terminated {
		return nil
	}
	if err := windows.TerminateProcess(process.process, exitCode); err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			if signaled, waitErr := nativeProcessSignaled(process.process); waitErr == nil && signaled {
				process.terminated = true
				return nil
			}
		}
		return fmt.Errorf("terminate native process %d: %w", process.pid, err)
	}
	process.terminated = true
	return nil
}

// Wait blocks until the process exits and returns its exit code.
func (process *SuspendedProcess) Wait() (uint32, error) {
	return process.WaitContext(context.Background())
}

// WaitContext waits for process exit or context cancellation. It is separate
// from Wait so later integrations can bound startup without taking ownership
// of the process handles away from this type.
func (process *SuspendedProcess) WaitContext(ctx context.Context) (uint32, error) {
	if process == nil {
		return 0, errNativeLaunchClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	process.mu.Lock()
	if process.closed {
		err := process.closeErr
		code, known := process.exitCode, process.exitKnown
		process.mu.Unlock()
		if known {
			return code, err
		}
		if err == nil {
			err = errNativeLaunchClosed
		}
		return 0, err
	}
	if process.closing {
		closeDone := process.closeDone
		process.mu.Unlock()
		select {
		case <-closeDone:
			process.mu.Lock()
			code, known, err := process.exitCode, process.exitKnown, process.closeErr
			process.mu.Unlock()
			if known {
				return code, err
			}
			return 0, errNativeLaunchClosing
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	handle := process.process
	process.waiters++
	if process.waiters == 1 {
		process.waitersDone = make(chan struct{})
	}
	process.mu.Unlock()
	defer process.endWait()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		event, err := windows.WaitForSingleObject(handle, 50)
		if err != nil {
			return 0, fmt.Errorf("wait for native process %d: %w", process.pid, err)
		}
		switch event {
		case windows.WAIT_OBJECT_0:
			code, err := nativeExitCode(handle)
			if err != nil {
				return 0, fmt.Errorf("read native process %d exit code: %w", process.pid, err)
			}
			process.mu.Lock()
			process.exitCode = code
			process.exitKnown = true
			process.mu.Unlock()
			return code, nil
		case nativeWaitTimeout:
			continue
		default:
			return 0, fmt.Errorf("wait for native process %d returned %#x", process.pid, event)
		}
	}
}

// Close terminates a still-running process, waits for it, and releases the
// thread and process handles. It also releases the Job when this process still
// owns it; AttachSuspendedProcess transfers that ownership to jobContainment.
func (process *SuspendedProcess) Close() error {
	if process == nil {
		return nil
	}
	process.mu.Lock()
	if process.closed {
		err := process.closeErr
		process.mu.Unlock()
		return err
	}
	if process.closing {
		closeDone := process.closeDone
		process.mu.Unlock()
		<-closeDone
		process.mu.Lock()
		err := process.closeErr
		process.mu.Unlock()
		return err
	}
	process.closing = true
	processHandle := process.process
	threadHandle := process.thread
	jobHandle := process.job
	pid := process.pid
	waitersDone := process.waitersDone
	process.mu.Unlock()

	var closeErr error
	if signaled, err := nativeProcessSignaled(processHandle); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("probe native process %d before close: %w", pid, err))
		if terminateErr := windows.TerminateProcess(processHandle, 1); terminateErr != nil && terminateErr != windows.ERROR_ACCESS_DENIED {
			closeErr = errors.Join(closeErr, fmt.Errorf("terminate native process %d during close: %w", pid, terminateErr))
		}
	} else if !signaled {
		if err := windows.TerminateProcess(processHandle, 1); err != nil && err != windows.ERROR_ACCESS_DENIED {
			closeErr = errors.Join(closeErr, fmt.Errorf("terminate native process %d during close: %w", pid, err))
		}
	}

	if _, err := windows.WaitForSingleObject(processHandle, windows.INFINITE); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("wait for native process %d during close: %w", pid, err))
	} else if code, err := nativeExitCode(processHandle); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("read native process %d exit code during close: %w", pid, err))
	} else {
		process.mu.Lock()
		process.exitCode = code
		process.exitKnown = true
		process.mu.Unlock()
	}
	if waitersDone != nil {
		<-waitersDone
	}

	if err := windows.CloseHandle(threadHandle); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close native process %d thread handle: %w", pid, err))
	}
	if err := windows.CloseHandle(processHandle); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close native process %d process handle: %w", pid, err))
	}
	if jobHandle != 0 {
		if err := windows.CloseHandle(jobHandle); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close native process %d Job handle: %w", pid, err))
		}
	}

	process.mu.Lock()
	process.process = 0
	process.thread = 0
	process.job = 0
	process.jobID = ""
	process.closeErr = closeErr
	process.closed = true
	close(process.closeDone)
	process.mu.Unlock()
	return closeErr
}

func (process *SuspendedProcess) endWait() {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.waiters--
	if process.waiters == 0 {
		close(process.waitersDone)
		process.waitersDone = nil
	}
}

func nativeProcessSignaled(handle windows.Handle) (bool, error) {
	event, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	if event == windows.WAIT_OBJECT_0 {
		return true, nil
	}
	if event == nativeWaitTimeout {
		return false, nil
	}
	return false, fmt.Errorf("wait probe returned %#x", event)
}

func nativeExitCode(handle windows.Handle) (uint32, error) {
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return 0, err
	}
	return code, nil
}
