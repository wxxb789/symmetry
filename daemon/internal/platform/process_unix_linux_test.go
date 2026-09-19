//go:build linux

package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPIDFDGroupSupportProbeReleasesProcessHandle(t *testing.T) {
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	countPIDFDs := func() int {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if target == "anon_inode:[pidfd]" {
				count++
			}
		}
		return count
	}
	before := countPIDFDs()
	for range 16 {
		if err := checkPIDFDGroupSupport(); err != nil {
			t.Fatal(err)
		}
	}
	if after := countPIDFDs(); after != before {
		t.Fatalf("capability probes retained pidfds: before=%d after=%d", before, after)
	}
}

func TestConfigureHeadlessProcessIsNoOp(t *testing.T) {
	command := exec.Command("sleep", "1")
	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	if command.SysProcAttr != nil {
		t.Fatalf("ConfigureHeadlessProcess() changed SysProcAttr = %#v", command.SysProcAttr)
	}
	if err := ConfigureHeadlessProcess(nil); err != nil {
		t.Fatalf("ConfigureHeadlessProcess(nil) error = %v, want nil", err)
	}
}

func TestParseLinuxProcessStatExtractsGroupFenceAndStartTime(t *testing.T) {
	stat, err := parseLinuxProcessStat(syntheticLinuxProcessStatLine(123, "name with ) delimiter", 'S', 123, 77, 456))
	if err != nil {
		t.Fatalf("parseLinuxProcessStat() error = %v", err)
	}
	if stat != (linuxProcessStat{pid: 123, pgrp: 123, session: 77, startTime: 456}) {
		t.Fatalf("parsed stat = %#v", stat)
	}
}

func TestParseLinuxProcessStatRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{
		"",
		"123",
		"123 (name)",
		"123 (name) S 1",
		"123 (name) S 1 2 x 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19",
	} {
		if _, err := parseLinuxProcessStat(value); err == nil {
			t.Fatalf("parseLinuxProcessStat(%q) = nil error, want rejection", value)
		}
	}
}

func TestLinuxProcessGroupObservationRejectsEscapedLeaderAfterPIDFDESRCH(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	originalRead := readLinuxProcessStat
	readCalls := 0
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		readCalls++
		switch readCalls {
		case 1, 2:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		default:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp + 1, session: anchor.session, startTime: anchor.startTime}, nil
		}
	}
	t.Cleanup(func() { readLinuxProcessStat = originalRead })

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		return unix.ESRCH
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	if err := group.Close(); err == nil || !strings.Contains(err.Error(), "identity or fence changed") {
		t.Fatalf("Close() error = %v, want escaped-leader rejection", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0]", signals)
	}
	if group.fd != 47 || !group.ContainmentCloseRetryable() {
		t.Fatalf("escaped-leader failure left fd:%d retryable:%v, want retained fd and retryable", group.fd, group.ContainmentCloseRetryable())
	}
}

func TestWaitForProcessGroupExitFailsClosed(t *testing.T) {
	want := syscall.EPERM
	if err := waitForProcessGroupExit(context.Background(), 1234, time.Now().Add(time.Second), func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("permission failure = %v, want %v", err, want)
	}

	probes := 0
	err := waitForProcessGroupExit(context.Background(), 1234, time.Now(), func() error {
		probes++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "remained non-empty") {
		t.Fatalf("deadline failure = %v, want non-empty containment error", err)
	}
	if probes != 1 {
		t.Fatalf("deadline probes = %d, want 1", probes)
	}
}

func TestConfigureProcessFailsClosedWithoutPIDFDGroupSignal(t *testing.T) {
	want := errors.New("group pidfd signal unsupported")
	previous := probePIDFDGroupSupport
	probePIDFDGroupSupport = func() error { return want }
	t.Cleanup(func() { probePIDFDGroupSupport = previous })

	command := exec.Command("sleep", "1")
	if err := ConfigureProcess(command); !errors.Is(err, want) {
		t.Fatalf("ConfigureProcess() error = %v, want %v", err, want)
	}
	if command.SysProcAttr != nil {
		t.Fatalf("ConfigureProcess() changed SysProcAttr after failed capability probe: %#v", command.SysProcAttr)
	}
}

func TestConfigureProcessSetsProcessGroupAfterPIDFDSupportProbe(t *testing.T) {
	previous := probePIDFDGroupSupport
	probePIDFDGroupSupport = func() error { return nil }
	t.Cleanup(func() { probePIDFDGroupSupport = previous })

	command := exec.Command("sleep", "1")
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatalf("ConfigureProcess() SysProcAttr = %#v, want Setpgid", command.SysProcAttr)
	}
}

func TestAttachProcessReturnsPartialContainmentAfterIdentityFailure(t *testing.T) {
	previousProbe := probePIDFDGroupSupport
	probePIDFDGroupSupport = func() error { return nil }
	t.Cleanup(func() { probePIDFDGroupSupport = previousProbe })

	command := exec.Command("sleep", "300")
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	want := errors.New("identity unavailable")
	previousIdentity := readProcessIdentity
	readProcessIdentity = func(int) (string, error) { return "", want }
	t.Cleanup(func() { readProcessIdentity = previousIdentity })

	containment, identity, err := AttachProcess(command.Process)
	if containment == nil || identity != "" || !errors.Is(err, want) {
		t.Fatalf("AttachProcess() = (%#v, %q, %v), want partial containment and identity error", containment, identity, err)
	}
	initialRetryer, initialOK := containment.(ContainmentCloseRetryer)
	if !initialOK || initialRetryer.ContainmentCloseRetryable() {
		t.Fatalf("partial containment initial retryability = (%v, %v), want implemented and false", initialRetryer, initialOK)
	}
	if err := containment.Terminate(true); err != nil {
		t.Fatalf("partial containment Terminate(true) error = %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("wait for terminated helper error = nil")
	}
	if err := containment.Close(); err == nil {
		t.Fatal("partial containment Close() = nil without an anchor, want unresolved")
	}
	if retryer, ok := containment.(ContainmentCloseRetryer); !ok || retryer.ContainmentCloseRetryable() {
		t.Fatalf("partial containment retryability = (%v, %v), want implemented and false", retryer, ok)
	}
}

func TestTerminateProcessGroupValidatesIdentityBeforePIDFDSignals(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	defer process.Release()
	previousIdentity := readProcessIdentity
	readProcessIdentity = func(int) (string, error) { return "actual", nil }
	t.Cleanup(func() { readProcessIdentity = previousIdentity })

	signals := 0
	restorePIDFDCalls(t, func(int, unix.Signal, *unix.Siginfo, int) error {
		signals++
		return nil
	}, func(int) error { return nil })
	if err := TerminateProcessGroup(context.Background(), process, "different"); err == nil || signals != 0 {
		t.Fatalf("identity-mismatched TerminateProcessGroup() = %v with %d signals, want mismatch and no signals", err, signals)
	}
}

func TestTerminateProcessGroupUsesBorrowedPIDFDForSignalAndReadback(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	defer process.Release()
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: int64(pid), session: 77, startTime: 456}, nil
	}
	t.Cleanup(func() {
		readProcessIdentity = previousIdentity
		readLinuxProcessStat = previousStat
	})

	var fd int
	var signals []unix.Signal
	restorePIDFDCalls(t, func(actualFD int, signal unix.Signal, _ *unix.Siginfo, flags int) error {
		fd = actualFD
		signals = append(signals, signal)
		if flags != unix.PIDFD_SIGNAL_PROCESS_GROUP {
			t.Fatalf("pidfd signal flags = %d, want PIDFD_SIGNAL_PROCESS_GROUP", flags)
		}
		if signal == 0 {
			return unix.ESRCH
		}
		return nil
	}, func(int) error { return nil })
	if err := TerminateProcessGroup(context.Background(), process, "expected"); err != nil {
		t.Fatalf("TerminateProcessGroup() error = %v", err)
	}
	if fd < 0 || !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("pidfd termination = fd:%d signals:%#v, want borrowed fd and [SIGKILL 0]", fd, signals)
	}
}

func TestProcessGroupUsesRetainedPIDFDForSignalsAndClose(t *testing.T) {
	var fds []int
	var signals []unix.Signal
	restorePIDFDCalls(t, func(fd int, signal unix.Signal, _ *unix.Siginfo, flags int) error {
		fds = append(fds, fd)
		signals = append(signals, signal)
		if flags != unix.PIDFD_SIGNAL_PROCESS_GROUP {
			t.Fatalf("pidfd signal flags = %d, want PIDFD_SIGNAL_PROCESS_GROUP", flags)
		}
		if signal == 0 {
			return unix.ESRCH
		}
		return nil
	}, func(int) error { return nil })

	previousStat := readLinuxProcessStat
	readLinuxProcessStat = func(int) (linuxProcessStat, error) { return linuxProcessStat{}, os.ErrNotExist }
	t.Cleanup(func() { readLinuxProcessStat = previousStat })
	group := &processGroup{pid: 1234, fd: 47, anchor: linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}, anchorCaptured: true}
	if err := group.Terminate(false); err != nil {
		t.Fatalf("Terminate(false) error = %v", err)
	}
	if err := group.Terminate(true); err != nil {
		t.Fatalf("Terminate(true) error = %v", err)
	}
	if err := group.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := group.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	if err := group.Terminate(true); err != nil {
		t.Fatalf("Terminate(true) after Close() error = %v", err)
	}
	if got, want := len(fds), 4; got != want {
		t.Fatalf("pidfd signal calls = %d, want %d", got, want)
	}
	for _, fd := range fds {
		if fd != 47 {
			t.Fatalf("pidfd signal fd = %d, want retained fd 47", fd)
		}
	}
	if got, want := signals, []unix.Signal{unix.SIGTERM, unix.SIGKILL, unix.SIGKILL, 0}; !slices.Equal(got, want) {
		t.Fatalf("signals = %#v, want %#v", got, want)
	}
	if group.fd != -1 {
		t.Fatalf("retained pidfd = %d after Close(), want -1", group.fd)
	}
}

func TestProcessGroupCloseTreatsPIDFDESRCHAsStopped(t *testing.T) {
	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		return unix.ESRCH
	}, func(int) error { return nil })
	previousStat := readLinuxProcessStat
	readLinuxProcessStat = func(int) (linuxProcessStat, error) { return linuxProcessStat{}, os.ErrNotExist }
	t.Cleanup(func() { readLinuxProcessStat = previousStat })

	group := &processGroup{pid: 1234, fd: 47, anchor: linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}, anchorCaptured: true}
	if err := group.Close(); err != nil {
		t.Fatalf("direct ESRCH close = %v", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) || group.fd != -1 {
		t.Fatalf("direct ESRCH close = signals:%#v fd:%d, want [SIGKILL 0] and released fd", signals, group.fd)
	}
}

func TestProcessGroupCloseRetriesTransientFailureWithRetainedPIDFD(t *testing.T) {
	want := errors.New("kill failed")
	signals := 0
	releases := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals++
		switch signals {
		case 1:
			if signal != unix.SIGKILL {
				t.Fatalf("first signal = %v, want SIGKILL", signal)
			}
			return want
		case 2:
			if signal != unix.SIGKILL {
				t.Fatalf("retry signal = %v, want SIGKILL", signal)
			}
			return nil
		case 3:
			if signal != 0 {
				t.Fatalf("proof signal = %v, want signal 0", signal)
			}
			return unix.ESRCH
		default:
			t.Fatalf("unexpected signal %v on call %d", signal, signals)
		}
		return nil
	}, func(int) error {
		releases++
		return nil
	})
	previousStat := readLinuxProcessStat
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		return linuxProcessStat{}, os.ErrNotExist
	}
	t.Cleanup(func() { readLinuxProcessStat = previousStat })

	group := &processGroup{pid: 1234, fd: 47, anchor: linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}, anchorCaptured: true}
	first := group.Close()
	if !errors.Is(first, want) {
		t.Fatalf("first Close() error = %v, want %v", first, want)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after transient failure fd:%d completed:%v retryable:%v, want retained fd, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
	second := group.Close()
	if second != nil {
		t.Fatalf("second Close() error = %v, want nil", second)
	}
	third := group.Close()
	if third != nil {
		t.Fatalf("cached Close() error = %v, want nil", third)
	}
	if group.ContainmentCloseRetryable() || !group.closeCompleted {
		t.Fatalf("successful Close() state = completed:%v retryable:%v, want complete and nonretryable", group.closeCompleted, group.ContainmentCloseRetryable())
	}
	if signals != 3 || releases != 1 || group.fd != -1 {
		t.Fatalf("Close() calls = signals:%d releases:%d fd:%d, want 3, 1, -1", signals, releases, group.fd)
	}
}

func TestProcessGroupCloseCachesPIDFDCloseFailureAsFinal(t *testing.T) {
	want := errors.New("close failed")
	signals := 0
	closes := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals++
		if signal == 0 {
			return unix.ESRCH
		}
		if signal != unix.SIGKILL {
			t.Fatalf("signal = %v, want SIGKILL or signal 0", signal)
		}
		return nil
	}, func(int) error {
		closes++
		return want
	})
	previousStat := readLinuxProcessStat
	readLinuxProcessStat = func(int) (linuxProcessStat, error) { return linuxProcessStat{}, os.ErrNotExist }
	t.Cleanup(func() { readLinuxProcessStat = previousStat })

	group := &processGroup{pid: 1234, fd: 47, anchor: linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}, anchorCaptured: true}
	first := group.Close()
	second := group.Close()
	if !errors.Is(first, want) || !errors.Is(second, want) {
		t.Fatalf("cached close errors = (%v, %v), want %v", first, second, want)
	}
	if group.ContainmentCloseRetryable() || !group.closeCompleted || group.fd != -1 || signals != 2 || closes != 1 {
		t.Fatalf("final close state = completed:%v retryable:%v fd:%d signals:%d closes:%d, want true,false,-1,2,1", group.closeCompleted, group.ContainmentCloseRetryable(), group.fd, signals, closes)
	}
}

func TestProcessGroupCloseSerializesConcurrentCallers(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			close(entered)
			<-release
			return nil
		}
		return unix.ESRCH
	}, func(int) error { return nil })
	previousStat := readLinuxProcessStat
	readLinuxProcessStat = func(int) (linuxProcessStat, error) { return linuxProcessStat{}, os.ErrNotExist }
	t.Cleanup(func() { readLinuxProcessStat = previousStat })

	group := &processGroup{pid: 1234, fd: 47, anchor: linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}, anchorCaptured: true}
	firstResult := make(chan error, 1)
	go func() { firstResult <- group.Close() }()
	<-entered
	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondResult <- group.Close()
	}()
	<-secondStarted

	close(release)
	released = true
	if err := <-firstResult; err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := <-secondResult; err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got, want := signals, []unix.Signal{unix.SIGKILL, 0}; !slices.Equal(got, want) {
		t.Fatalf("signals = %#v, want one SIGKILL and one readback", got)
	}
}

func TestProcessGroupCloseAfterLeaderReapedStopsLiveChild(t *testing.T) {
	enableLinuxTestSubreaper(t)
	command, stdin, stdout := startContainmentHelper(t)
	containment, identity, err := AttachProcess(command.Process)
	if err != nil {
		stopContainmentHelper(t, command)
		t.Fatalf("AttachProcess() error = %v", err)
	}
	if identity == "" {
		t.Fatal("AttachProcess() returned an empty identity")
	}
	waited := false
	closed := false
	child := &linuxTestChildOwner{}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = stdout.Close()
		if !closed {
			_ = containment.Close()
		}
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
		child.cleanup(t)
	})

	child.pid = releaseContainmentHelper(t, stdin, stdout)
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("owned child %d was not running before leader exit: %v", child.pid, err)
	}
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("allow leader exit: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close helper stdin: %v", err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("wait for leader exit: %v", err)
	}
	waited = true
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("owned child %d was not live after leader reaping: %v", child.pid, err)
	}

	previousStat := readLinuxProcessStat
	forcedObservationFailure := true
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if forcedObservationFailure {
			forcedObservationFailure = false
			return linuxProcessStat{}, errors.New("forced process-group observation failure")
		}
		return previousStat(pid)
	}
	t.Cleanup(func() { readLinuxProcessStat = previousStat })
	firstErr := containment.Close()
	if firstErr == nil {
		t.Fatal("first Close() after forced observation failure = nil")
	}
	retryer, ok := containment.(ContainmentCloseRetryer)
	if !ok || !retryer.ContainmentCloseRetryable() {
		t.Fatalf("after forced observation failure retryability = (%v, %v), want implemented and true", retryer, ok)
	}
	if group, ok := containment.(*processGroup); !ok || group.fd < 0 || group.closeCompleted {
		t.Fatalf("after forced observation failure containment = (%T), want retained incomplete processGroup fd", containment)
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- containment.Close() }()
	// The test owns the adopted child and reaps it concurrently with Close. This
	// prevents a zombie-only observation from being accepted as final ESRCH.
	child.reap(t)
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() after leader reaping: %v", err)
	}
	closed = true
	if state, err := readLinuxProcessState(child.pid); err == nil && state != 'Z' && state != 'X' && state != 'x' {
		t.Fatalf("owned child %d remained live after Close(): state=%q", child.pid, state)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read owned child %d state after Close(): %v", child.pid, err)
	}
}

func restorePIDFDCalls(t *testing.T, send func(int, unix.Signal, *unix.Siginfo, int) error, close func(int) error) {
	t.Helper()
	previousSend := sendPIDFDSignal
	previousClose := closePIDFD
	sendPIDFDSignal = send
	closePIDFD = close
	t.Cleanup(func() {
		sendPIDFDSignal = previousSend
		closePIDFD = previousClose
	})
}

func syntheticLinuxProcessStatLine(pid int, comm string, state byte, pgrp, session int64, startTime uint64) string {
	fields := []string{string(state), "1", strconv.FormatInt(pgrp, 10), strconv.FormatInt(session, 10)}
	for len(fields) <= 18 {
		fields = append(fields, "0")
	}
	fields = append(fields, strconv.FormatUint(startTime, 10))
	return fmt.Sprintf("%d (%s) %s", pid, comm, strings.Join(fields, " "))
}

func readLinuxProcessState(pid int) (byte, error) {
	value, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	firstSpace := strings.IndexByte(string(value), ' ')
	closingParenthesis := strings.LastIndexByte(string(value), ')')
	if firstSpace < 0 || closingParenthesis < 0 || closingParenthesis+2 >= len(value) {
		return 0, errors.New("malformed process stat")
	}
	return value[closingParenthesis+2], nil
}

func enableLinuxTestSubreaper(t *testing.T) {
	t.Helper()
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Skipf("child subreaper is unavailable: %v", err)
	}
	t.Cleanup(func() { _ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0) })
}

type linuxTestChildOwner struct {
	pid    int
	reaped bool
}

func (owner *linuxTestChildOwner) reap(t *testing.T) {
	t.Helper()
	if owner == nil || owner.pid <= 0 || owner.reaped {
		return
	}
	var status unix.WaitStatus
	if _, err := unix.Wait4(owner.pid, &status, 0, nil); err != nil && !errors.Is(err, unix.ECHILD) && !errors.Is(err, unix.ESRCH) {
		t.Errorf("Wait4(%d): %v", owner.pid, err)
	}
	owner.reaped = true
}

func (owner *linuxTestChildOwner) cleanup(t *testing.T) {
	t.Helper()
	if owner == nil || owner.pid <= 0 || owner.reaped {
		return
	}
	_ = syscall.Kill(owner.pid, syscall.SIGKILL)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status unix.WaitStatus
		waitedPID, err := unix.Wait4(owner.pid, &status, unix.WNOHANG, nil)
		if waitedPID == owner.pid || errors.Is(err, unix.ECHILD) || errors.Is(err, unix.ESRCH) {
			owner.reaped = true
			return
		}
		if err != nil {
			t.Errorf("Wait4(%d, WNOHANG): %v", owner.pid, err)
			owner.reaped = true
			return
		}
		if !time.Now().Before(deadline) {
			t.Errorf("child %d was not reaped before cleanup deadline", owner.pid)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startContainmentHelper(t *testing.T) (*exec.Cmd, io.WriteCloser, io.ReadCloser) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "leader-exits-after-child")
	command.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("create helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("create helper stdout: %v", err)
	}
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	return command, stdin, stdout
}

func stopContainmentHelper(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_ = command.Wait()
}

func releaseContainmentHelper(t *testing.T, stdin io.Writer, stdout io.Reader) int {
	t.Helper()
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release helper: %v", err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse child PID %q: %v", line, err)
	}
	return childPID
}

func TestProcessGroupContainmentHelper(t *testing.T) {
	if os.Getenv("GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER") != "1" {
		return
	}

	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator == -1 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}

	switch os.Args[separator+1] {
	case "leader-exits-after-child":
		var release [1]byte
		if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(3)
		}
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(4)
		}
		_, _ = fmt.Fprintln(os.Stdout, child.Process.Pid)
		if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(5)
		}
	default:
		os.Exit(2)
	}
}
