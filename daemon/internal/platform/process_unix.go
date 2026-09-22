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
	containmentCloseDeadline             = 5 * time.Second
	containmentCloseProbeInterval        = 10 * time.Millisecond
	containmentDescendantMonitorInterval = 10 * time.Millisecond
)

var (
	probePIDFDGroupSupport   = checkPIDFDGroupSupport
	duplicatePIDFD           = duplicatePIDFDHandle
	readProcessIdentity      = ProcessIdentity
	sendPIDFDSignal          = unix.PidfdSendSignal
	closePIDFD               = unix.Close
	readLinuxProcessStat     = readLinuxProcessStatFile
	readLinuxProcessChildren = readLinuxProcessChildrenFile
)

// ErrLinuxDescendantContainmentUnproven means that the original process-group
// fence was not enough to prove every descendant stopped. Callers must retain
// the process marker and retry the identity-bound containment operation.
var ErrLinuxDescendantContainmentUnproven = errors.New("Linux descendant containment is unproven")

// Containment owns a launched process's platform-specific termination boundary.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

// ConfigureHeadlessProcess is a no-op on Unix because child console windows
// are not created by the process-launch API used here.
func ConfigureHeadlessProcess(*exec.Cmd) error { return nil }

// ConfigureProcess verifies pidfd process-group signalling before making the
// launched process the leader of a new process group. Close additionally
// inspects the live descendant tree; descendants that create a session or move
// to another group remain unresolved because this authority cannot safely
// signal their subtree without a stronger kernel boundary.
func ConfigureProcess(command *exec.Cmd) error {
	if err := probePIDFDGroupSupport(); err != nil {
		return fmt.Errorf("pidfd process-group containment is unsupported: %w", err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
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
	group.startDescendantMonitor()
	return group, identity, nil
}

type processGroup struct {
	operationMutex     sync.Mutex
	mutex              sync.Mutex
	pid                int
	fd                 int
	anchor             linuxProcessGroupAnchor
	anchorCaptured     bool
	escapedDescendants []linuxProcessStat
	// descendantScanLost is irreversible for this authority: a later clean
	// scan cannot prove what escaped while observation was uncertain.
	descendantScanLost      bool
	descendantScanCompleted bool

	containmentUnprovenCallback          func() error
	containmentUnprovenCallbackAttempted bool
	containmentUnprovenPersisted         bool
	containmentUnprovenCallbackErr       error
	containmentUnprovenCallbackMutex     sync.Mutex

	monitorStop           chan struct{}
	monitorDone           chan struct{}
	monitorScanRequest    chan chan descendantMonitorScanResult
	monitorTerminalReason descendantMonitorTerminalReason
	monitorStopOnce       sync.Once
	escapeObserved        chan struct{}
	escapeObservedOnce    sync.Once
	monitorReadStat       func(int) (linuxProcessStat, error)
	monitorReadChildren   func(int) ([]int, error)
	closeCompleted        bool
	closeErr              error
	ownerReleasePending   bool
}

type descendantMonitorTerminalReason uint8

const (
	descendantMonitorTerminalNone descendantMonitorTerminalReason = iota
	descendantMonitorTerminalCleanLeaderAbsent
	descendantMonitorTerminalScanFailure
	descendantMonitorTerminalObservedEscape
	descendantMonitorTerminalOwnerStop
)

type descendantMonitorScanResult struct {
	leaderPresent  bool
	scanErr        error
	stopped        bool
	terminalReason descendantMonitorTerminalReason
}

func (reason descendantMonitorTerminalReason) String() string {
	switch reason {
	case descendantMonitorTerminalCleanLeaderAbsent:
		return "leader_absent"
	case descendantMonitorTerminalScanFailure:
		return "scan_failure"
	case descendantMonitorTerminalObservedEscape:
		return "observed_escape"
	case descendantMonitorTerminalOwnerStop:
		return "owner_stop"
	default:
		return "none"
	}
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
	return group.anchorCaptured && group.fd >= 0 && !group.closeCompleted && !group.ownerReleasePending
}

// SetContainmentUnprovenCallback installs the durable uncertainty callback
// after the process marker is available. Observation may race that install, so
// an already observed escape or scan loss is delivered synchronously here.
// Callback failures remain fail-closed and are retried by Close.
func (group *processGroup) SetContainmentUnprovenCallback(callback func() error) error {
	if group == nil {
		return errors.New("process-group containment is unavailable")
	}
	group.mutex.Lock()
	group.containmentUnprovenCallback = callback
	observed := group.containmentUnprovenObservedLocked()
	if callback != nil && observed && !group.containmentUnprovenPersisted {
		group.containmentUnprovenCallbackAttempted = false
	}
	group.mutex.Unlock()
	if !observed || callback == nil {
		return nil
	}
	return group.persistContainmentUnproven(true)
}

func (group *processGroup) containmentUnprovenObservedLocked() bool {
	return group.descendantScanLost || len(group.escapedDescendants) > 0
}

func (group *processGroup) markContainmentUnproven(scanLost bool) {
	group.mutex.Lock()
	if group.closeCompleted {
		group.mutex.Unlock()
		return
	}
	if scanLost {
		group.descendantScanLost = true
	}
	observed := group.containmentUnprovenObservedLocked()
	group.mutex.Unlock()
	if observed {
		// The monitor must not spin on a failed durable write. Close retries the
		// same callback while retaining the process-group authority.
		_ = group.persistContainmentUnproven(false)
	}
}

func (group *processGroup) persistContainmentUnproven(force bool) error {
	group.containmentUnprovenCallbackMutex.Lock()
	defer group.containmentUnprovenCallbackMutex.Unlock()

	group.mutex.Lock()
	if !group.containmentUnprovenObservedLocked() || group.containmentUnprovenPersisted {
		group.mutex.Unlock()
		return nil
	}
	if !force && group.containmentUnprovenCallbackAttempted {
		err := group.containmentUnprovenCallbackErr
		group.mutex.Unlock()
		return err
	}
	callback := group.containmentUnprovenCallback
	if callback == nil {
		group.mutex.Unlock()
		return nil
	}
	group.containmentUnprovenCallbackAttempted = true
	group.mutex.Unlock()

	err := callback()
	group.mutex.Lock()
	if err == nil {
		group.containmentUnprovenPersisted = true
		group.containmentUnprovenCallbackErr = nil
	} else {
		group.containmentUnprovenCallbackErr = err
	}
	group.mutex.Unlock()
	return err
}

func (group *processGroup) closeContainmentUnproven(cause error) error {
	persistErr := group.persistContainmentUnproven(true)
	result := fmt.Errorf("%w: descendant observation was uncertain", ErrLinuxDescendantContainmentUnproven)
	if cause != nil {
		result = errors.Join(result, cause)
	}
	if persistErr != nil {
		result = errors.Join(result, fmt.Errorf("persist containment uncertainty: %w", persistErr))
	}
	return result
}

func (group *processGroup) startDescendantMonitor() {
	if group == nil || !group.anchorCaptured {
		return
	}
	group.monitorStop = make(chan struct{})
	group.monitorDone = make(chan struct{})
	group.monitorScanRequest = make(chan chan descendantMonitorScanResult, 1)
	group.escapeObserved = make(chan struct{})
	group.monitorReadStat = readLinuxProcessStat
	group.monitorReadChildren = readLinuxProcessChildren
	go group.monitorDescendants()
}

func (group *processGroup) recordDescendantMonitorTerminalReason(reason descendantMonitorTerminalReason) descendantMonitorTerminalReason {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.monitorTerminalReason == descendantMonitorTerminalNone {
		group.monitorTerminalReason = reason
	}
	return group.monitorTerminalReason
}

func (group *processGroup) descendantMonitorTerminalResult() (descendantMonitorScanResult, error) {
	group.mutex.Lock()
	reason := group.monitorTerminalReason
	group.mutex.Unlock()
	result := descendantMonitorScanResult{stopped: true, terminalReason: reason}
	if reason == descendantMonitorTerminalCleanLeaderAbsent {
		return result, nil
	}
	if reason == descendantMonitorTerminalNone {
		return result, fmt.Errorf("%w: descendant monitor exited before final scan", ErrLinuxDescendantContainmentUnproven)
	}
	return result, fmt.Errorf("%w: descendant monitor terminated before final scan (%s)", ErrLinuxDescendantContainmentUnproven, reason)
}

func (group *processGroup) stopDescendantMonitor(deadline time.Time) error {
	if group == nil || group.monitorStop == nil {
		return nil
	}
	group.recordDescendantMonitorTerminalReason(descendantMonitorTerminalOwnerStop)
	group.monitorStopOnce.Do(func() { close(group.monitorStop) })
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return fmt.Errorf("%w: descendant monitor stop deadline elapsed", ErrLinuxDescendantContainmentUnproven)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-group.monitorDone:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: descendant monitor did not stop before deadline", ErrLinuxDescendantContainmentUnproven)
	}
}

func (group *processGroup) monitorDescendants() {
	defer close(group.monitorDone)
	scan := func() descendantMonitorScanResult {
		present, err := observeLinuxProcessGroupLeaderWithReader(group.pid, group.anchor, group.monitorReadStat)
		if err != nil {
			group.recordDescendantMonitorTerminalReason(descendantMonitorTerminalScanFailure)
			group.markContainmentUnproven(true)
			return descendantMonitorScanResult{scanErr: err, terminalReason: descendantMonitorTerminalScanFailure}
		}
		if !present {
			group.mutex.Lock()
			uncertain := !group.descendantScanCompleted || group.descendantScanLost || len(group.escapedDescendants) > 0
			group.mutex.Unlock()
			reason := group.recordDescendantMonitorTerminalReason(descendantMonitorTerminalCleanLeaderAbsent)
			if uncertain {
				group.markContainmentUnproven(true)
			}
			return descendantMonitorScanResult{stopped: true, terminalReason: reason}
		}
		escaped, scanErr := captureLinuxEscapedDescendantsWithReaders(group.pid, group.anchor, group.monitorReadStat, group.monitorReadChildren)
		if scanErr != nil {
			group.recordDescendantMonitorTerminalReason(descendantMonitorTerminalScanFailure)
			group.markContainmentUnproven(true)
			return descendantMonitorScanResult{leaderPresent: true, scanErr: scanErr, terminalReason: descendantMonitorTerminalScanFailure}
		}
		group.mutex.Lock()
		newEscape := false
		if !group.closeCompleted {
			group.descendantScanCompleted = true
			before := len(group.escapedDescendants)
			group.escapedDescendants = mergeLinuxEscapedDescendants(group.escapedDescendants, escaped)
			newEscape = len(group.escapedDescendants) > before
			if newEscape {
				group.escapeObservedOnce.Do(func() { close(group.escapeObserved) })
			}
		}
		group.mutex.Unlock()
		if newEscape {
			group.recordDescendantMonitorTerminalReason(descendantMonitorTerminalObservedEscape)
			group.markContainmentUnproven(false)
		}
		return descendantMonitorScanResult{leaderPresent: true}
	}
	if scan().stopped {
		return
	}
	ticker := time.NewTicker(containmentDescendantMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-group.monitorStop:
			return
		case resultChannel := <-group.monitorScanRequest:
			result := scan()
			resultChannel <- result
			if result.stopped {
				return
			}
		case <-ticker.C:
			if scan().stopped {
				return
			}
		}
	}
}

func (group *processGroup) requestDescendantMonitorScan(deadline time.Time) (descendantMonitorScanResult, error) {
	if group == nil || group.monitorStop == nil || group.monitorDone == nil || group.monitorScanRequest == nil {
		return descendantMonitorScanResult{}, fmt.Errorf("%w: descendant monitor is unavailable", ErrLinuxDescendantContainmentUnproven)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return descendantMonitorScanResult{}, fmt.Errorf("%w: descendant monitor final scan deadline elapsed", ErrLinuxDescendantContainmentUnproven)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	resultChannel := make(chan descendantMonitorScanResult, 1)
	select {
	case group.monitorScanRequest <- resultChannel:
	case <-group.monitorDone:
		return group.descendantMonitorTerminalResult()
	case <-timer.C:
		return descendantMonitorScanResult{}, fmt.Errorf("%w: descendant monitor final scan request timed out", ErrLinuxDescendantContainmentUnproven)
	}
	select {
	case result := <-resultChannel:
		return result, nil
	case <-group.monitorDone:
		select {
		case result := <-resultChannel:
			return result, nil
		default:
			return group.descendantMonitorTerminalResult()
		}
	case <-timer.C:
		return descendantMonitorScanResult{}, fmt.Errorf("%w: descendant monitor final scan timed out", ErrLinuxDescendantContainmentUnproven)
	}
}

func (group *processGroup) Terminate(force bool) error {
	group.operationMutex.Lock()
	defer group.operationMutex.Unlock()

	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closeCompleted || group.ownerReleasePending {
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
	return group.closeUntil(containmentDeadline(context.Background()))
}

func (group *processGroup) closeUntil(deadline time.Time) error {
	group.operationMutex.Lock()
	defer group.operationMutex.Unlock()

	group.mutex.Lock()
	if group.closeCompleted {
		err := group.closeErr
		group.mutex.Unlock()
		return err
	}
	if group.ownerReleasePending {
		err := group.closeErr
		group.mutex.Unlock()
		return err
	}
	alreadyUnproven := group.descendantScanLost
	monitorPresent := group.monitorStop != nil
	anchorCaptured := group.anchorCaptured
	group.mutex.Unlock()

	if alreadyUnproven {
		return group.finishUnprovenClose(deadline, nil)
	}

	group.mutex.Lock()
	if group.descendantScanLost {
		group.mutex.Unlock()
		return group.finishUnprovenClose(deadline, nil)
	}
	group.mutex.Unlock()

	var stopErr error
	initialLeaderPresent := false
	if anchorCaptured {
		var leaderErr error
		initialLeaderPresent, leaderErr = observeLinuxProcessGroupLeader(group.pid, group.anchor)
		if leaderErr != nil {
			return group.finishUnprovenClose(deadline, leaderErr)
		}
		if !initialLeaderPresent && monitorPresent {
			group.mutex.Lock()
			uncertain := !group.descendantScanCompleted || group.descendantScanLost || len(group.escapedDescendants) > 0
			group.mutex.Unlock()
			if uncertain {
				return group.finishUnprovenClose(deadline, nil)
			}
		}
	}

	group.mutex.Lock()
	initialSignalErr := group.terminateLocked(true)
	group.mutex.Unlock()
	if initialSignalErr != nil && !errors.Is(initialSignalErr, unix.ESRCH) {
		stopErr = fmt.Errorf("terminate process group %d during containment close: %w", group.pid, initialSignalErr)
	} else if anchorCaptured {
		// Keep the monitor active through SIGKILL and group readback. The final
		// scan request is issued only after this readback succeeds, so an escape
		// observed during the wait cannot be hidden by an earlier scan.
		stopErr = waitForProcessGroupExit(context.Background(), group.pid, deadline, func() error {
			group.mutex.Lock()
			defer group.mutex.Unlock()
			return group.probeGroupExitLocked(context.Background(), deadline, initialLeaderPresent)
		})
		if stopErr == nil && monitorPresent {
			// The request is serviced by the monitor itself, so a blocked reader
			// remains bounded by deadline without creating an unowned scan
			// goroutine.
			result, requestErr := group.requestDescendantMonitorScan(deadline)
			finalScanErr := requestErr
			if finalScanErr == nil {
				finalScanErr = result.scanErr
				if finalScanErr == nil && !result.leaderPresent {
					group.mutex.Lock()
					cleanTerminal := result.terminalReason == descendantMonitorTerminalCleanLeaderAbsent &&
						group.descendantScanCompleted &&
						!group.descendantScanLost &&
						len(group.escapedDescendants) == 0
					group.mutex.Unlock()
					if !cleanTerminal {
						finalScanErr = fmt.Errorf("%w: leader disappeared without a clean descendant terminal proof", ErrLinuxDescendantContainmentUnproven)
					}
				}
			}
			if finalScanErr != nil {
				group.mutex.Lock()
				group.descendantScanLost = true
				group.mutex.Unlock()
				stopErr = finalScanErr
			}
		}
		if stopErr == nil && !monitorPresent {
			// The proof scan follows the kill and the group-exit readback. A
			// scan before SIGKILL leaves an escape window between observation
			// and termination.
			postKillLeaderPresent, leaderErr := observeLinuxProcessGroupLeader(group.pid, group.anchor)
			if leaderErr != nil {
				stopErr = leaderErr
			} else if postKillLeaderPresent {
				escaped, scanErr := captureLinuxEscapedDescendants(group.pid, group.anchor)
				if scanErr != nil {
					group.mutex.Lock()
					group.descendantScanLost = true
					group.mutex.Unlock()
					stopErr = scanErr
				} else {
					group.mutex.Lock()
					group.descendantScanCompleted = true
					group.escapedDescendants = mergeLinuxEscapedDescendants(group.escapedDescendants, escaped)
					group.mutex.Unlock()
				}
			}
		}
	} else {
		stopErr = errors.New("process group leader anchor is unavailable")
	}

	// Do not stop the monitor until the kill, group readback, and post-kill
	// scan have all completed. A stop timeout transfers no ownership and keeps
	// the process marker retryable.
	monitorErr := group.stopDescendantMonitor(deadline)
	if monitorErr != nil {
		group.mutex.Lock()
		group.descendantScanLost = true
		group.mutex.Unlock()
		if stopErr == nil {
			stopErr = monitorErr
		} else {
			stopErr = errors.Join(stopErr, monitorErr)
		}
	}

	group.mutex.Lock()
	retainAuthority := stopErr != nil && group.anchorCaptured && group.fd >= 0
	uncertain := group.descendantScanLost
	escapedErr := group.rejectEscapedDescendants()
	if uncertain {
		group.mutex.Unlock()
		return group.closeContainmentUnproven(stopErr)
	}
	if retainAuthority {
		group.mutex.Unlock()
		return stopErr
	}
	if escapedErr != nil {
		// A process outside the original group cannot be safely signalled by
		// this authority. Keep the original marker unresolved; a later retry
		// cannot prove descendants that may have been created after the scan.
		group.mutex.Unlock()
		return group.closeContainmentUnproven(errors.Join(stopErr, escapedErr))
	}

	// No anchor is unsafe to retry. A proven stop is final as well, and the
	// pidfd close result is part of that final outcome; close(2) is never
	// replayed.
	group.closeCompleted = true
	group.closeErr = errors.Join(stopErr, group.releasePIDFDLocked())
	err := group.closeErr
	group.mutex.Unlock()
	return err
}

func (group *processGroup) finishUnprovenClose(deadline time.Time, cause error) error {
	monitorErr := group.stopDescendantMonitor(deadline)
	group.mutex.Lock()
	group.descendantScanLost = true
	group.mutex.Unlock()
	if monitorErr != nil {
		cause = errors.Join(cause, monitorErr)
	}
	return group.closeContainmentUnproven(cause)
}

func (group *processGroup) probeGroupExitLocked(ctx context.Context, deadline time.Time, previousLeaderPresent bool) error {
	return probeLinuxProcessGroupExit(ctx, deadline, group.pid, group.anchor, previousLeaderPresent, group.signalLocked)
}

func (group *processGroup) rejectEscapedDescendants() error {
	if len(group.escapedDescendants) == 0 {
		return nil
	}
	// Once an escape has been observed, this process-group authority cannot
	// prove the escaped subtree's descendants were also stopped. Keep the
	// marker unresolved even if the recorded process itself disappears.
	return fmt.Errorf("%w: descendant escaped original process group %d", ErrLinuxDescendantContainmentUnproven, group.anchor.pgrp)
}

func mergeLinuxEscapedDescendants(existing, discovered []linuxProcessStat) []linuxProcessStat {
	if len(discovered) == 0 {
		return existing
	}
	merged := append([]linuxProcessStat(nil), existing...)
	for _, candidate := range discovered {
		known := false
		for _, value := range merged {
			if value.pid == candidate.pid && value.startTime == candidate.startTime {
				known = true
				break
			}
		}
		if !known {
			merged = append(merged, candidate)
		}
	}
	return merged
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

// TerminatePersistedProcessGroup applies the same descendant-aware containment
// boundary used by a live process to a marker recovered after daemon restart.
// A missing leader is not a stop proof: the original group may be gone while a
// descendant escaped before the daemon observed it.
func TerminatePersistedProcessGroup(ctx context.Context, process *os.Process, expectedIdentity string) (resultErr error) {
	if ctx == nil {
		return errors.New("termination context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if process == nil || process.Pid <= 0 || expectedIdentity == "" {
		return errors.New("process handle and expected identity are required for persisted process-group termination")
	}
	actualIdentity, err := readProcessIdentity(process.Pid)
	if err != nil {
		return fmt.Errorf("read persisted process identity before group termination: %w", err)
	}
	if actualIdentity != expectedIdentity {
		return fmt.Errorf("process identity mismatch before persisted group termination: got %q, want %q", actualIdentity, expectedIdentity)
	}
	containment, _, err := AttachProcess(process)
	if err != nil {
		if containment != nil {
			if releaseErr := releaseProcessGroupOwner(containment); releaseErr != nil {
				return errors.Join(fmt.Errorf("attach persisted process containment: %w", err), fmt.Errorf("release partial persisted containment: %w", releaseErr))
			}
		}
		return fmt.Errorf("attach persisted process containment: %w", err)
	}
	defer func() {
		closeErr := releaseProcessGroupOwner(containment)
		if closeErr == nil {
			return
		}
		if resultErr == nil {
			resultErr = fmt.Errorf("release persisted process containment: %w", closeErr)
			return
		}
		resultErr = errors.Join(resultErr, fmt.Errorf("release persisted process containment: %w", closeErr))
	}()
	if err := containment.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("terminate persisted process group: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if group, ok := containment.(*processGroup); ok {
		if err := group.closeUntil(containmentDeadline(ctx)); err != nil {
			return fmt.Errorf("prove persisted process-group stop: %w", err)
		}
		return nil
	}
	if err := containment.Close(); err != nil {
		return fmt.Errorf("close persisted process containment: %w", err)
	}
	return nil
}

// releaseProcessGroupOwner retires a temporary recovery containment owner
// without converting an unproven stop into success. If the monitor cannot stop
// within the bounded release window, ownership transfers to a one-shot retire
// goroutine that waits for monitorDone before releasing the pidfd.
func releaseProcessGroupOwner(containment Containment) error {
	if containment == nil {
		return nil
	}
	if group, ok := containment.(*processGroup); ok {
		group.operationMutex.Lock()
		defer group.operationMutex.Unlock()

		monitorErr := group.stopDescendantMonitor(time.Now().Add(containmentCloseDeadline))
		group.mutex.Lock()
		if monitorErr != nil {
			// Keep the duplicate pidfd and the monitor-owned group alive when
			// shutdown is not proven. Marking this owner complete here would
			// abandon both the retry authority and the monitor goroutine.
			group.descendantScanLost = true
			group.closeCompleted = false
			group.closeErr = errors.Join(group.closeErr, monitorErr)
			if !group.ownerReleasePending {
				group.ownerReleasePending = true
				go group.retireProcessGroupOwner()
			}
			group.mutex.Unlock()
			return monitorErr
		}
		releaseErr := group.releasePIDFDLocked()
		group.ownerReleasePending = false
		group.closeCompleted = true
		group.closeErr = errors.Join(group.closeErr, monitorErr, releaseErr)
		group.mutex.Unlock()
		return errors.Join(monitorErr, releaseErr)
	}
	return containment.Close()
}

func (group *processGroup) retireProcessGroupOwner() {
	if group == nil || group.monitorDone == nil {
		return
	}
	<-group.monitorDone

	group.operationMutex.Lock()
	defer group.operationMutex.Unlock()
	group.mutex.Lock()
	if !group.ownerReleasePending {
		group.mutex.Unlock()
		return
	}
	releaseErr := group.releasePIDFDLocked()
	group.ownerReleasePending = false
	group.closeCompleted = true
	group.closeErr = errors.Join(group.closeErr, releaseErr)
	group.mutex.Unlock()
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
	return observeLinuxProcessGroupLeaderWithReader(pid, anchor, readLinuxProcessStat)
}

func observeLinuxProcessGroupLeaderWithReader(pid int, anchor linuxProcessGroupAnchor, readStat func(int) (linuxProcessStat, error)) (bool, error) {
	leader, err := readStat(pid)
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

func captureLinuxEscapedDescendants(pid int, anchor linuxProcessGroupAnchor) ([]linuxProcessStat, error) {
	return captureLinuxEscapedDescendantsWithReaders(pid, anchor, readLinuxProcessStat, readLinuxProcessChildren)
}

func captureLinuxEscapedDescendantsWithReaders(pid int, anchor linuxProcessGroupAnchor, readStat func(int) (linuxProcessStat, error), readChildren func(int) ([]int, error)) ([]linuxProcessStat, error) {
	pending := []int{pid}
	visited := map[int]struct{}{pid: {}}
	var escaped []linuxProcessStat

	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		children, err := readChildren(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: process %d disappeared during fenced scan: %w", ErrLinuxDescendantContainmentUnproven, current, err)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: read children of process %d: %w", ErrLinuxDescendantContainmentUnproven, current, err)
		}
		for _, child := range children {
			if child <= 0 {
				return nil, fmt.Errorf("%w: child pid %d is invalid", ErrLinuxDescendantContainmentUnproven, child)
			}
			if _, seen := visited[child]; seen {
				continue
			}
			visited[child] = struct{}{}
			stat, statErr := readStat(child)
			if errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: descendant %d disappeared during fenced scan: %w", ErrLinuxDescendantContainmentUnproven, child, statErr)
			}
			if statErr != nil {
				return nil, fmt.Errorf("%w: read descendant %d: %w", ErrLinuxDescendantContainmentUnproven, child, statErr)
			}
			if stat.pgrp != anchor.pgrp || stat.session != anchor.session {
				escaped = append(escaped, stat)
			}
			pending = append(pending, child)
		}
	}
	return escaped, nil
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

func readLinuxProcessChildrenFile(pid int) ([]int, error) {
	if pid <= 0 {
		return nil, errors.New("process pid must be positive")
	}
	value, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if err != nil {
		return nil, fmt.Errorf("read /proc/%d/task/%d/children: %w", pid, pid, err)
	}
	fields := strings.Fields(string(value))
	children := make([]int, 0, len(fields))
	for _, field := range fields {
		child, parseErr := strconv.Atoi(field)
		if parseErr != nil || child <= 0 {
			if parseErr == nil {
				parseErr = errors.New("pid must be positive")
			}
			return nil, fmt.Errorf("parse process child pid %q: %w", field, parseErr)
		}
		children = append(children, child)
	}
	return children, nil
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
