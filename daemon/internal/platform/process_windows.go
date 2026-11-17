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

	"golang.org/x/sys/windows"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

const (
	containmentCloseDeadline            = 5 * time.Second
	containmentCloseProbeInterval       = 10 * time.Millisecond
	jobObjectBasicAccountingInformation = 1
	jobObjectBasicProcessIdList         = 3
	jobObjectExtendedLimitInformation   = 9
	jobObjectLimitKillOnJobClose        = 0x00002000
	ownedJobProcessListLimit            = 1 << 16
	windowsCreateNoWindow               = 0x08000000
	windowsCreateNewConsole             = 0x00000010
	windowsDetachedProcess              = 0x00000008
)

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	createJobObject                  = kernel32.NewProc("CreateJobObjectW")
	openJobObject                    = kernel32.NewProc("OpenJobObjectW")
	setInformationJobObject          = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject         = kernel32.NewProc("AssignProcessToJobObject")
	terminateJobObject               = kernel32.NewProc("TerminateJobObject")
	queryInformationJobObject        = kernel32.NewProc("QueryInformationJobObject")
	terminateProcessForAttachFailure = terminateFailedAttachProcess
	terminateJob                     = terminateOwnedJob
	queryJobActiveProcesses          = queryOwnedJobActiveProcesses
	queryJobProcessIDs               = queryOwnedJobProcessIDs
	closeJob                         = syscall.CloseHandle
	waitForEmptyJob                  = waitForJobToEmpty
	readProcessIdentityFromHandle    = processIdentityFromHandle
)

// basicProcessIDList mirrors JOBOBJECT_BASIC_PROCESS_ID_LIST. The kernel fills
// both counts and as many process IDs as fit; processIDList is the first of
// numberOfProcessIDsInList entries in a buffer sized for the whole list.
type basicProcessIDList struct {
	numberOfAssignedProcesses uint32
	numberOfProcessIDsInList  uint32
	processIDList             [1]uintptr
}

// ownedJobProcess is a process the Job Object owned at the stop boundary,
// bound to the creation time that tells a recycled PID apart from that process.
type ownedJobProcess struct {
	pid      int
	identity string
}

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
	jobID        string
	supervisor   containmentSupervisorLease
	stopReceipt  *authority.StopReceipt
	closeStarted bool
	closeErr     error
	// Set only when stop proof succeeded and the daemon-side handle close was
	// the sole failed local cleanup step.
	handleReleaseRetryable         bool
	partialCleanupPending          bool
	partialContainmentBaseCloseErr error
}

// ContainmentAuthority returns a copy of the helper binding, if this Job is
// owned by the independent Windows supervisor. A nil result is deliberate for
// partial/legacy containment and must remain fail-closed during recovery.
func (job *jobContainment) ContainmentAuthority() *authority.Supervisor {
	if job == nil {
		return nil
	}
	job.mutex.Lock()
	supervisor := job.supervisor
	job.mutex.Unlock()
	if supervisor == nil {
		return nil
	}
	provider, ok := supervisor.(containmentSupervisorAuthorityProvider)
	if !ok {
		return nil
	}
	value := provider.containmentAuthority()
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	return &cloned
}

func (job *jobContainment) ContainmentAuthorityAvailable() bool {
	if job == nil {
		return false
	}
	job.mutex.Lock()
	supervisor := job.supervisor
	job.mutex.Unlock()
	if supervisor == nil {
		return false
	}
	_, ok := supervisor.(containmentSupervisorAuthorityProvider)
	return ok
}

// LeaseRenewalAvailable reports whether the independent supervisor can arm a
// monotonic local lease deadline. Legacy/test containment remains available but
// deliberately has no watchdog authority.
func (job *jobContainment) LeaseRenewalAvailable() bool {
	if job == nil {
		return false
	}
	job.mutex.Lock()
	supervisor := job.supervisor
	job.mutex.Unlock()
	if supervisor == nil {
		return false
	}
	_, ok := supervisor.(ContainmentLeaseRenewer)
	return ok
}

// ContainmentCloseRetryable reports whether Close may retry only a local
// cleanup step. Partial supervisor cleanup and a daemon-side Job handle close
// failure after a durable stop receipt are retryable; neither case replays a
// physical stop or helper release.
func (job *jobContainment) ContainmentCloseRetryable() bool {
	if job == nil {
		return false
	}
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if _, partial := job.supervisor.(containmentSupervisorPartialLease); partial {
		return job.partialCleanupPending || job.handle != 0
	}
	_, durable := job.supervisor.(containmentSupervisorAuthorityProvider)
	return durable && job.stopReceipt != nil && job.handleReleaseRetryable && job.handle != 0
}

// RenewLease forwards a relative deadline to the independent supervisor while
// serializing it with Close through the Job mutex.
func (job *jobContainment) RenewLease(deadline time.Duration, sequence uint64) error {
	if job == nil {
		return errors.New("containment lease is missing")
	}
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.closeStarted {
		return errors.New("containment lease is stopped")
	}
	if job.supervisor == nil {
		return fmt.Errorf("%w: containment supervisor lease is unavailable", errors.ErrUnsupported)
	}
	renewer, ok := job.supervisor.(ContainmentLeaseRenewer)
	if !ok || !renewer.LeaseRenewalAvailable() {
		return fmt.Errorf("%w: containment supervisor lease renewal is unavailable", errors.ErrUnsupported)
	}
	return renewer.RenewLease(deadline, sequence)
}

// ContainmentStopReceipt returns the cached positive stop witness. It is
// intentionally separate from Close so callers can persist it before helper
// release.
func (job *jobContainment) ContainmentStopReceipt() (authority.StopReceipt, bool) {
	if job == nil {
		return authority.StopReceipt{}, false
	}
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.stopReceipt == nil {
		return authority.StopReceipt{}, false
	}
	return *job.stopReceipt, true
}

// ReleaseContainment performs the authenticated helper release after the
// exact stop receipt has been durably recorded. Legacy/no-supervisor jobs do
// not have a second authority and therefore have nothing to release.
func (job *jobContainment) ReleaseContainment() error {
	if job == nil {
		return errors.New("containment job is missing")
	}
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.supervisor == nil {
		return nil
	}
	if job.stopReceipt == nil {
		return errors.New("durable containment stop receipt is required before helper release")
	}
	if releaser, ok := job.supervisor.(containmentSupervisorStopLease); ok {
		if err := releaser.release(time.Now().Add(containmentCloseDeadline)); err != nil {
			return err
		}
		job.supervisor = nil
		return nil
	}
	return errors.New("containment supervisor release capability is unavailable")
}

// AbortContainment is the pre-authority startup cleanup path. It is valid
// only before the daemon has committed a supervisor authority to the journal.
func (job *jobContainment) AbortContainment() error {
	if job == nil {
		return errors.New("containment job is missing")
	}
	if err := job.Close(); err != nil {
		return err
	}
	return job.ReleaseContainment()
}

// ConfigureHeadlessProcess starts console applications without creating a
// console window. The standard library does not expose names for these Windows
// creation flags, so keep the values local to this platform implementation.
func ConfigureHeadlessProcess(command *exec.Cmd) error {
	return configureHeadlessProcess(command)
}

func configureHeadlessProcess(command *exec.Cmd) error {
	if command == nil {
		return errors.New("process command is required")
	}

	attributes := command.SysProcAttr
	if attributes != nil {
		flags := attributes.CreationFlags
		if flags&windowsCreateNewConsole != 0 {
			return errors.New("headless process configuration conflicts with CREATE_NEW_CONSOLE")
		}
		if flags&windowsDetachedProcess != 0 {
			return errors.New("headless process configuration conflicts with DETACHED_PROCESS")
		}
	}

	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
		command.SysProcAttr = attributes
	}
	attributes.CreationFlags |= windowsCreateNoWindow
	return nil
}

// ConfigureProcess leaves inherited Job Object handling to AttachProcess and
// reuses the headless process configuration before the process is started.
func ConfigureProcess(command *exec.Cmd) error {
	return configureHeadlessProcess(command)
}

// AttachProcess adds the root process to a fresh Job Object. The job owns the
// root and descendants assigned after this call; processes that escape before
// post-Start assignment or deliberately break away are outside this boundary.
func AttachProcess(process *os.Process) (Containment, string, error) {
	if process == nil || process.Pid <= 0 {
		return nil, "", errors.New("process handle is required for containment")
	}

	job, jobID, err := createNamedContainmentJob()
	if err != nil {
		return cleanupFailedAttach(process, 0, fmt.Errorf("create job object: %w", err))
	}
	var supervisor containmentSupervisorLease
	cleanup := func(errorValue error) (Containment, string, error) {
		var supervisorErr error
		if supervisor != nil {
			supervisorErr = supervisor.close(time.Now().Add(containmentCloseDeadline))
		}
		return cleanupFailedAttach(process, job, errors.Join(errorValue, supervisorErr))
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

	contained := &jobContainment{handle: job, jobID: jobID}
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
	contained.supervisor, identity, callbackErr = launchContainmentSupervisor(job, process.Pid, identity)
	supervisor = contained.supervisor
	if callbackErr != nil {
		return cleanup(fmt.Errorf("start containment supervisor: %w", callbackErr))
	}
	if contained.supervisor == nil {
		return cleanup(errors.New("containment supervisor lease is missing"))
	}
	return contained, identity, nil
}

// AttachSuspendedProcess binds the Job created by LaunchSuspended to the same
// containment owner used by the legacy path. The target remains suspended
// throughout identity capture and supervisor setup. On success the Job handle
// is transferred out of SuspendedProcess so exactly one owner closes it.
func AttachSuspendedProcess(process *SuspendedProcess) (Containment, string, error) {
	if process == nil {
		return nil, "", errors.New("suspended process is required for containment")
	}

	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return nil, "", errors.New("suspended process is already closed")
	}
	if process.closing {
		process.mu.Unlock()
		return nil, "", errors.New("suspended process is closing")
	}
	job := process.job
	processHandle := process.process
	pid := int(process.pid)
	process.mu.Unlock()
	if job == 0 || processHandle == 0 || pid <= 0 {
		return nil, "", errors.New("suspended process handles are incomplete")
	}

	identity, err := readProcessIdentityFromHandle(pid, syscall.Handle(processHandle))
	if err != nil {
		return nil, "", fmt.Errorf("capture suspended process creation identity: %w", err)
	}
	supervisor, targetIdentity, supervisorErr := launchContainmentSupervisor(syscall.Handle(job), pid, identity)
	if targetIdentity != "" {
		identity = targetIdentity
	}
	if supervisor == nil && supervisorErr != nil {
		return nil, identity, fmt.Errorf("start containment supervisor for suspended process: %w", supervisorErr)
	}

	contained := &jobContainment{handle: syscall.Handle(job), supervisor: supervisor}
	process.mu.Lock()
	if process.closed || process.closing || process.job != job {
		process.mu.Unlock()
		_ = contained.Close()
		return nil, identity, errors.New("suspended process changed during containment attachment")
	}
	process.job = 0
	process.mu.Unlock()
	if supervisorErr != nil {
		return contained, identity, fmt.Errorf("start containment supervisor for suspended process: %w", supervisorErr)
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

// Close terminates the owned Job Object, observes an empty Job, and asks the
// independent supervisor to release its duplicate handle before releasing the
// daemon-side handle. If proof or supervisor release fails, it still performs
// best-effort handle cleanup and preserves the error for later calls.
func (job *jobContainment) Close() error {
	job.mutex.Lock()
	defer job.mutex.Unlock()
	if job.closeStarted {
		// A failed stop proof remains retryable. Once a receipt exists, Close is
		// idempotent and the caller may retry callback persistence/release
		// without re-signalling the Job.
		_, retryableStop := job.supervisor.(containmentSupervisorStopLease)
		_, durableAuthority := job.supervisor.(containmentSupervisorAuthorityProvider)
		if partial, ok := job.supervisor.(containmentSupervisorPartialLease); ok && job.partialCleanupPending {
			return job.closePartialSupervisorLocked(partial, nil)
		}
		if !durableAuthority {
			_ = job.releaseHandleLocked()
			return job.closeErr
		}
		retryableStop = retryableStop && durableAuthority
		if job.closeErr != nil && job.stopReceipt == nil && !retryableStop {
			return job.closeErr
		}
		if job.stopReceipt != nil {
			if job.closeErr != nil && !job.handleReleaseRetryable {
				return job.closeErr
			}
			if err := job.releaseHandleLocked(); err != nil {
				job.handleReleaseRetryable = true
				job.closeErr = err
				return err
			}
			job.handleReleaseRetryable = false
			job.closeErr = nil
			return nil
		}
	}
	job.closeStarted = true

	handle := job.handle
	if handle == 0 {
		if partial, ok := job.supervisor.(containmentSupervisorPartialLease); ok {
			return job.closePartialSupervisorLocked(partial, nil)
		}
		if supervisor, ok := job.supervisor.(containmentSupervisorStopLease); ok {
			if _, durableAuthority := job.supervisor.(containmentSupervisorAuthorityProvider); !durableAuthority {
				job.closeErr = job.supervisor.close(time.Now().Add(containmentCloseDeadline))
				return job.closeErr
			}
			receipt, err := supervisor.stop(time.Now().Add(containmentCloseDeadline))
			if err != nil {
				job.closeErr = err
				return err
			}
			job.stopReceipt = &receipt
			return nil
		}
		if job.supervisor != nil {
			job.closeErr = job.supervisor.close(time.Now().Add(containmentCloseDeadline))
		}
		return job.closeErr
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	var stopErr error
	if err := terminateJobAndProveStop(handle, deadline); err != nil {
		stopErr = err
	}
	if partial, ok := job.supervisor.(containmentSupervisorPartialLease); ok {
		return job.closePartialSupervisorLocked(partial, stopErr)
	}
	var supervisorErr error
	if supervisor, ok := job.supervisor.(containmentSupervisorStopLease); ok {
		if _, durableAuthority := job.supervisor.(containmentSupervisorAuthorityProvider); !durableAuthority {
			// This lease has no authority that could witness a stop, so no
			// stored receipt can answer for the physical proof on a later
			// call. Latch the failed proof in the cached terminal error: the
			// daemon-side Job handle is released here (or by the retry that
			// consumes this error), and the proof cannot be re-derived after
			// that.
			supervisorErr = job.supervisor.close(deadline)
			if supervisorErr == nil {
				job.closeErr = errors.Join(stopErr, job.releaseHandleLocked())
				return job.closeErr
			}
			job.closeErr = errors.Join(stopErr, supervisorErr)
			return job.closeErr
		}
		var receipt authority.StopReceipt
		receipt, supervisorErr = supervisor.stop(deadline)
		if supervisorErr == nil {
			job.stopReceipt = &receipt
		}
	} else if job.supervisor != nil {
		supervisorErr = job.supervisor.close(deadline)
	}
	if !durableAuthority(job.supervisor) {
		job.closeErr = errors.Join(stopErr, supervisorErr, job.releaseHandleLocked())
		return job.closeErr
	}
	if stopErr == nil && supervisorErr == nil {
		// The daemon-side duplicate is no longer needed after the supervisor
		// proved the Job empty. The independent helper remains retained until
		// ReleaseContainment is called after durable receipt persistence.
		job.closeErr = job.releaseHandleLocked()
		job.handleReleaseRetryable = job.stopReceipt != nil && job.handle != 0 && job.closeErr != nil
		return errors.Join(stopErr, supervisorErr, job.closeErr)
	}
	job.handleReleaseRetryable = false
	job.closeErr = errors.Join(stopErr, supervisorErr)
	return job.closeErr
}

func durableAuthority(supervisor containmentSupervisorLease) bool {
	if supervisor == nil {
		return false
	}
	_, ok := supervisor.(containmentSupervisorAuthorityProvider)
	return ok
}

func (job *jobContainment) closePartialSupervisorLocked(partial containmentSupervisorPartialLease, baseErr error) error {
	if job.partialContainmentBaseCloseErr == nil {
		releaseErr := job.releaseHandleLocked()
		job.partialContainmentBaseCloseErr = errors.Join(baseErr, releaseErr)
	} else {
		_ = job.releaseHandleLocked()
	}
	helperErr := partial.closePartial(time.Now().Add(containmentCloseDeadline))
	job.partialCleanupPending = helperErr != nil
	job.closeErr = errors.Join(job.partialContainmentBaseCloseErr, helperErr)
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

// queryOwnedJobProcessIDs lists the processes the Job Object owns and binds each
// to its creation-time identity. The list is sampled before the Job is
// terminated because the Job stops reporting a member as soon as it terminates.
// A member Windows no longer knows has stopped and needs no settlement; any
// other bind failure fails the proof closed, and a PID recycled between the two
// kernel calls can only make the settlement check fail closed too, never report
// an unproven stop as proven.
func queryOwnedJobProcessIDs(handle syscall.Handle) ([]ownedJobProcess, error) {
	for size := unsafe.Sizeof(basicProcessIDList{}); size <= ownedJobProcessListLimit; size *= 2 {
		buffer := make([]byte, size)
		result, _, callError := queryInformationJobObject.Call(
			uintptr(handle),
			jobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			0,
		)
		if result == 0 {
			if errors.Is(callError, syscall.ERROR_MORE_DATA) {
				continue
			}
			return nil, fmt.Errorf("list owned job processes: %w", callError)
		}
		list := (*basicProcessIDList)(unsafe.Pointer(&buffer[0]))
		capacity := (uintptr(len(buffer)) - unsafe.Sizeof(basicProcessIDList{})) / unsafe.Sizeof(uintptr(0))
		if uintptr(list.numberOfProcessIDsInList) > capacity+1 {
			return nil, errors.New("owned job process list does not fit the sampled buffer")
		}
		return ownedJobProcesses(list, list.numberOfProcessIDsInList)
	}
	return nil, errors.New("owned job process list exceeds the bounded stop sample")
}

func ownedJobProcesses(head *basicProcessIDList, count uint32) ([]ownedJobProcess, error) {
	if count == 0 {
		return nil, nil
	}
	processIDs := unsafe.Slice(&head.processIDList[0], count)
	owned := make([]ownedJobProcess, 0, count)
	for _, processID := range processIDs {
		identity, err := ProcessIdentity(int(processID))
		if err != nil {
			if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return nil, fmt.Errorf("bind owned job process %d identity: %w", processID, err)
			}
			// Windows reports an unknown PID this way; the member is gone.
			continue
		}
		owned = append(owned, ownedJobProcess{pid: int(processID), identity: identity})
	}
	return owned, nil
}

// waitOwnedProcessesStopped confirms every process the Job Object owned at the
// stop boundary has stopped. Job accounting reaches zero when the Job starts
// terminating its members, which is not an observation that any of them has
// stopped running, so this waits on the process objects themselves, bound to
// the same close deadline, and fails closed when an owned process is still
// running then.
func waitOwnedProcessesStopped(owned []ownedJobProcess, deadline time.Time) error {
	for _, member := range owned {
		for {
			running, detail := ownedProcessRunning(member)
			if !running {
				break
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return fmt.Errorf("owned process %d (%s) was still running after the containment close deadline: %s", member.pid, member.identity, detail)
			}
			if remaining > containmentCloseProbeInterval {
				remaining = containmentCloseProbeInterval
			}
			time.Sleep(remaining)
		}
	}
	return nil
}

// ownedProcessRunning reports whether the recorded owned process is still
// running. The creation-time identity is what makes that a statement about the
// owned process object rather than about whatever now happens to hold the PID.
func ownedProcessRunning(member ownedJobProcess) (bool, string) {
	handle, err := openProcessForIdentity(member.pid, syscall.SYNCHRONIZE)
	if err != nil {
		return false, fmt.Sprintf("the owned process is no longer openable: %v", err)
	}
	defer syscall.CloseHandle(handle)
	actual, err := processIdentityFromHandle(member.pid, handle)
	if err != nil {
		return true, fmt.Sprintf("read the owned process creation time: %v", err)
	}
	if actual != member.identity {
		return false, fmt.Sprintf("PID %d now has identity %s instead of the owned %s", member.pid, actual, member.identity)
	}
	status, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		return true, fmt.Sprintf("wait for the owned process: %v", err)
	}
	if status == syscall.WAIT_OBJECT_0 {
		return false, "the owned process object has terminated"
	}
	return true, fmt.Sprintf("the owned process object has not terminated (wait status 0x%x)", status)
}

// terminateJobAndProveStop terminates the Job Object and establishes the stop
// proof both containment owners publish: the Job must be empty within the
// deadline, and every process it owned at the stop boundary must be observed
// stopped, bound to that process's creation-time identity. An owned process
// that is still running when the deadline expires fails the proof, so a caller
// that reports success has settled the processes it claimed to stop.
func terminateJobAndProveStop(handle syscall.Handle, deadline time.Time) error {
	owned, ownedErr := queryJobProcessIDs(handle)
	if err := terminateJob(handle); err != nil {
		return fmt.Errorf("terminate job object during containment close: %w", err)
	}
	if err := waitForEmptyJob(handle, deadline, queryJobActiveProcesses); err != nil {
		return err
	}
	if ownedErr != nil {
		return ownedErr
	}
	if err := waitOwnedProcessesStopped(owned, deadline); err != nil {
		return err
	}
	return nil
}

// TerminateProcessGroup is unavailable on Windows because Job Objects, not
// process groups, provide the supported containment primitive here.
func TerminateProcessGroup(context.Context, *os.Process, string) error {
	return fmt.Errorf("%w: pidfd process-group termination is unavailable on Windows", errors.ErrUnsupported)
}
