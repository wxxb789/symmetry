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
	"sync"
	"sync/atomic"
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

func TestParseLinuxProcessStatReportsReleasedTaskAsAbsent(t *testing.T) {
	for _, value := range []string{
		syntheticLinuxProcessStatLine(123, "sh", 'X', -1, 77, 456),
		syntheticLinuxProcessStatLine(123, "sh", 'X', 123, -1, 456),
	} {
		if _, err := parseLinuxProcessStat(value); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("parseLinuxProcessStat(%q) = %v, want released task reported as absent", value, err)
		}
	}
}

func TestLinuxProcessGroupObservationRejectsEscapedLeaderAfterPIDFDESRCH(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	originalRead := readLinuxProcessStat
	originalChildren := readLinuxProcessChildren
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
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	t.Cleanup(func() {
		readLinuxProcessStat = originalRead
		readLinuxProcessChildren = originalChildren
	})

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

func TestProcessGroupRetainsAuthorityWhenDescendantEscapesProcessGroup(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	childAlive := true
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		switch pid {
		case anchor.pid:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		case 2345:
			if !childAlive {
				return linuxProcessStat{}, os.ErrNotExist
			}
			return linuxProcessStat{pid: 2345, pgrp: 2345, session: 2345, startTime: 789}, nil
		default:
			return linuxProcessStat{}, os.ErrNotExist
		}
	}
	readLinuxProcessChildren = func(pid int) ([]int, error) {
		if pid == anchor.pid && childAlive {
			return []int{2345}, nil
		}
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == 0 {
			return unix.ESRCH
		}
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	first := group.Close()
	if !errors.Is(first, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("first Close() error = %v, want escaped-descendant proof failure", first)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after escaped descendant fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("first Close() signals = %#v, want [SIGKILL 0]", signals)
	}

	childAlive = false
	if err := group.Close(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() after exact escaped identity disappeared = %v, want unresolved escaped subtree", err)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("escaped descendant state fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0, unix.SIGKILL, 0}) {
		t.Fatalf("all Close() signals = %#v, want two fenced attempts", signals)
	}
}

func TestCaptureLinuxEscapedDescendantsFailsClosedOnChildrenReadError(t *testing.T) {
	previousChildren := readLinuxProcessChildren
	want := errors.New("children unavailable")
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, want }
	t.Cleanup(func() { readLinuxProcessChildren = previousChildren })

	_, err := captureLinuxEscapedDescendants(1234, linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456})
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) || !errors.Is(err, want) {
		t.Fatalf("captureLinuxEscapedDescendants() error = %v, want unresolved children error wrapping %v", err, want)
	}
}

func TestCaptureLinuxEscapedDescendantsRetainsPartialEscapeOnLaterReadError(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	escapedPID := 2345
	unreadablePID := 3456
	childrenCalls := 0
	readStat := func(pid int) (linuxProcessStat, error) {
		switch pid {
		case escapedPID:
			return linuxProcessStat{pid: escapedPID, pgrp: 9999, session: 9999, startTime: 789}, nil
		case unreadablePID:
			return linuxProcessStat{}, unix.EACCES
		default:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
	}
	readChildren := func(pid int) ([]int, error) {
		childrenCalls++
		if pid == anchor.pid && childrenCalls == 1 {
			return []int{escapedPID, unreadablePID}, nil
		}
		return nil, nil
	}

	escaped, err := captureLinuxEscapedDescendantsWithReaders(anchor.pid, anchor, readStat, readChildren)
	if err == nil || !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("partial escaped scan error = %v, want unresolved scan error", err)
	}
	if len(escaped) != 1 || escaped[0].pid != escapedPID {
		t.Fatalf("partial escaped scan = %#v, want escaped pid %d retained", escaped, escapedPID)
	}
}

func TestCaptureLinuxEscapedDescendantsSkipsDescendantsReapedMidScan(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	escapedPID := 2345
	statGonePID := 3456
	childrenGonePID := 4567
	readStat := func(pid int) (linuxProcessStat, error) {
		switch pid {
		case escapedPID:
			return linuxProcessStat{pid: escapedPID, pgrp: 9999, session: 9999, startTime: 789}, nil
		case statGonePID:
			return linuxProcessStat{}, os.ErrNotExist
		default:
			return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: 900}, nil
		}
	}
	readChildren := func(pid int) ([]int, error) {
		switch pid {
		case anchor.pid:
			return []int{statGonePID, childrenGonePID, escapedPID}, nil
		case childrenGonePID:
			return nil, os.ErrNotExist
		default:
			return nil, nil
		}
	}

	escaped, err := captureLinuxEscapedDescendantsWithReaders(anchor.pid, anchor, readStat, readChildren)
	if err != nil {
		t.Fatalf("scan with descendants reaped mid-scan = %v, want nil", err)
	}
	if len(escaped) != 1 || escaped[0].pid != escapedPID {
		t.Fatalf("escaped descendants = %#v, want escaped pid %d retained", escaped, escapedPID)
	}

	readLeaderGone := func(pid int) ([]int, error) { return nil, os.ErrNotExist }
	if _, err := captureLinuxEscapedDescendantsWithReaders(anchor.pid, anchor, readStat, readLeaderGone); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leader children ENOENT = %v, want unresolved scan error for the monitor to classify", err)
	}
}

func TestProcessGroupRetainsOnlyFirstEscapedDescendantSample(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	first := linuxProcessStat{pid: 2345, pgrp: 2345, session: 2345, startTime: 789}
	second := linuxProcessStat{pid: 3456, pgrp: 3456, session: 3456, startTime: 987}
	readStat := func(pid int) (linuxProcessStat, error) {
		switch pid {
		case anchor.pid:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		case first.pid:
			return first, nil
		case second.pid:
			return second, nil
		default:
			return linuxProcessStat{}, fmt.Errorf("unexpected stat pid %d", pid)
		}
	}
	readChildren := func(pid int) ([]int, error) {
		if pid == anchor.pid {
			return []int{first.pid, second.pid}, nil
		}
		return nil, nil
	}

	escaped, err := captureLinuxEscapedDescendantsWithReaders(anchor.pid, anchor, readStat, readChildren)
	if err != nil {
		t.Fatalf("capture escaped descendants = %v", err)
	}
	if len(escaped) != 1 || escaped[0] != first {
		t.Fatalf("escaped diagnostic samples = %#v, want only first sample %#v", escaped, first)
	}

	group := &processGroup{anchor: anchor}
	if !group.recordEscapedDescendant(escaped) {
		t.Fatal("first escaped descendant was not recorded")
	}
	if group.recordEscapedDescendant([]linuxProcessStat{second}) {
		t.Fatal("later escape changed already-unproven state")
	}
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.escapedDescendant == nil || *group.escapedDescendant != first || !group.containmentUnprovenObservedLocked() {
		t.Fatalf("retained escape state = %#v, want sticky first sample %#v", group.escapedDescendant, first)
	}
}

func TestProcessGroupRetainsAuthorityAfterPostReadbackDescendantScanFailure(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid == anchor.pid {
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
		return linuxProcessStat{}, os.ErrNotExist
	}
	readLinuxProcessChildren = func(pid int) ([]int, error) {
		if pid == anchor.pid {
			return nil, errors.New("children read failed")
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == 0 {
			return unix.ESRCH
		}
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	first := group.Close()
	if !errors.Is(first, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("first Close() error = %v, want unresolved scan failure", first)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) || group.fd != 47 || !group.ContainmentCloseRetryable() {
		t.Fatalf("after scan failure signals:%#v fd:%d retryable:%v, want [SIGKILL 0], retained, retryable", signals, group.fd, group.ContainmentCloseRetryable())
	}

	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	if err := group.Close(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() after scan recovery = %v, want sticky unresolved result", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) || group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after sticky scan recovery signals:%#v fd:%d completed:%v retryable:%v, want [SIGKILL 0], retained, false, true", signals, group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
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
	if command.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("ConfigureProcess() Pdeathsig = %v, want SIGKILL", command.SysProcAttr.Pdeathsig)
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

func TestTerminatePersistedProcessGroupReleasesPartialAttachOwner(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	identityCalls := 0
	want := errors.New("identity unavailable")
	readProcessIdentity = func(int) (string, error) {
		identityCalls++
		if identityCalls == 1 {
			return "expected", nil
		}
		return "", want
	}
	t.Cleanup(func() {
		duplicatePIDFD = previousDuplicate
		readProcessIdentity = previousIdentity
	})

	closeCalls := 0
	restorePIDFDCalls(t, func(int, unix.Signal, *unix.Siginfo, int) error {
		t.Fatal("partial attach must not signal the process group")
		return nil
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		return nil
	})

	err = TerminatePersistedProcessGroup(context.Background(), process, "expected")
	if !errors.Is(err, want) {
		t.Fatalf("TerminatePersistedProcessGroup() error = %v, want %v", err, want)
	}
	if identityCalls != 2 {
		t.Fatalf("readProcessIdentity() calls = %d, want precheck plus attach", identityCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("partial attach pidfd close calls = %d, want 1", closeCalls)
	}
}

func TestTerminatePersistedProcessGroupReleasesOwnerAfterTerminateError(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: int64(pid), session: 77, startTime: 456}, nil
	}
	monitorStarted := make(chan struct{})
	var monitorStartedOnce sync.Once
	readLinuxProcessChildren = func(int) ([]int, error) {
		monitorStartedOnce.Do(func() { close(monitorStarted) })
		return nil, nil
	}
	t.Cleanup(func() {
		duplicatePIDFD = previousDuplicate
		readProcessIdentity = previousIdentity
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	want := errors.New("terminate failed")
	var signals []unix.Signal
	closeCalls := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		select {
		case <-monitorStarted:
		case <-time.After(time.Second):
			return errors.New("descendant monitor did not start")
		}
		return want
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		return nil
	})

	err = TerminatePersistedProcessGroup(context.Background(), process, "expected")
	if !errors.Is(err, want) {
		t.Fatalf("TerminatePersistedProcessGroup() error = %v, want %v", err, want)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) {
		t.Fatalf("termination signals = %#v, want [SIGKILL]", signals)
	}
	if closeCalls != 1 {
		t.Fatalf("terminate-error pidfd close calls = %d, want 1", closeCalls)
	}
}

func TestTerminatePersistedProcessGroupReleasesOwnerAfterUnprovenClose(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: int64(pid), session: 77, startTime: 456}, nil
	}
	monitorStarted := make(chan struct{})
	var monitorStartedOnce sync.Once
	wantScan := errors.New("descendant scan failed")
	readLinuxProcessChildren = func(int) ([]int, error) {
		monitorStartedOnce.Do(func() { close(monitorStarted) })
		return nil, wantScan
	}
	t.Cleanup(func() {
		duplicatePIDFD = previousDuplicate
		readProcessIdentity = previousIdentity
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	closeCalls := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		select {
		case <-monitorStarted:
		case <-time.After(time.Second):
			return errors.New("descendant monitor did not start")
		}
		if signal == 0 {
			return unix.ESRCH
		}
		return nil
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		return nil
	})

	err = TerminatePersistedProcessGroup(context.Background(), process, "expected")
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("TerminatePersistedProcessGroup() error = %v, want unresolved descendant scan", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) && !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) && !slices.Equal(signals, []unix.Signal{unix.SIGKILL, unix.SIGKILL, 0}) {
		t.Fatalf("termination signals = %#v, want [SIGKILL], [SIGKILL 0], or [SIGKILL SIGKILL 0] without a fabricated proof probe", signals)
	}
	if closeCalls != 1 {
		t.Fatalf("unproven-close pidfd close calls = %d, want 1", closeCalls)
	}
}

func TestTerminatePersistedProcessGroupReleasesOwnerAfterContextCancellation(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: int64(pid), session: 77, startTime: 456}, nil
	}
	monitorStarted := make(chan struct{})
	var monitorStartedOnce sync.Once
	readLinuxProcessChildren = func(int) ([]int, error) {
		monitorStartedOnce.Do(func() { close(monitorStarted) })
		return nil, nil
	}
	t.Cleanup(func() {
		duplicatePIDFD = previousDuplicate
		readProcessIdentity = previousIdentity
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var signals []unix.Signal
	closeCalls := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		select {
		case <-monitorStarted:
		case <-time.After(time.Second):
			return errors.New("descendant monitor did not start")
		}
		cancel()
		return nil
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		return nil
	})

	err = TerminatePersistedProcessGroup(ctx, process, "expected")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TerminatePersistedProcessGroup() error = %v, want context.Canceled", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) {
		t.Fatalf("termination signals = %#v, want [SIGKILL] without a stop proof", signals)
	}
	if closeCalls != 1 {
		t.Fatalf("cancellation pidfd close calls = %d, want 1", closeCalls)
	}
}

func TestReleaseProcessGroupOwnerReleasesPIDFDAndMonitor(t *testing.T) {
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: int64(pid), session: 77, startTime: 456}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	closeCalls := 0
	restorePIDFDCalls(t, func(int, unix.Signal, *unix.Siginfo, int) error {
		t.Fatal("owner release must not signal the process group")
		return nil
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		return nil
	})

	group := &processGroup{
		pid:            1234,
		fd:             47,
		anchor:         linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456},
		anchorCaptured: true,
	}
	group.startDescendantMonitor()
	if err := releaseProcessGroupOwner(group); err != nil {
		t.Fatalf("releaseProcessGroupOwner() error = %v", err)
	}
	select {
	case <-group.monitorDone:
	default:
		t.Fatal("releaseProcessGroupOwner() returned before descendant monitor stopped")
	}
	if group.fd != -1 || !group.closeCompleted || group.ContainmentCloseRetryable() {
		t.Fatalf("released owner state = fd:%d completed:%v retryable:%v, want -1,true,false", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
	if closeCalls != 1 {
		t.Fatalf("owner-release pidfd close calls = %d, want 1", closeCalls)
	}
}

func TestReleaseProcessGroupOwnerTransfersBlockedMonitorOwnership(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	entered := make(chan struct{})
	releaseReader := make(chan struct{})
	allowPIDFDClose := make(chan struct{})
	var enteredOnce sync.Once
	var releaseReaderOnce sync.Once
	var allowPIDFDCloseOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		enteredOnce.Do(func() { close(entered) })
		<-releaseReader
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	closeCalls := 0
	closeEntered := make(chan struct{})
	var closeEnteredOnce sync.Once
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		t.Errorf("owner release signalled process group with signal %v", signal)
		return nil
	}, func(fd int) error {
		if fd != 47 {
			t.Errorf("released pidfd = %d, want 47", fd)
		}
		closeCalls++
		closeEnteredOnce.Do(func() { close(closeEntered) })
		<-allowPIDFDClose
		return nil
	})

	group := &processGroup{
		pid:            anchor.pid,
		fd:             47,
		anchor:         anchor,
		anchorCaptured: true,
	}
	group.startDescendantMonitor()
	t.Cleanup(func() {
		releaseReaderOnce.Do(func() { close(releaseReader) })
		allowPIDFDCloseOnce.Do(func() { close(allowPIDFDClose) })
		select {
		case <-group.monitorDone:
		case <-time.After(containmentCloseDeadline + time.Second):
			t.Errorf("descendant monitor did not stop during cleanup")
		}
	})

	releaseResult := make(chan error, 1)
	go func() { releaseResult <- releaseProcessGroupOwner(group) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("descendant monitor did not enter the blocked reader")
	}

	var releaseErr error
	select {
	case releaseErr = <-releaseResult:
	case <-time.After(containmentCloseDeadline + time.Second):
		t.Fatal("releaseProcessGroupOwner() did not return after the monitor deadline")
	}
	if !errors.Is(releaseErr, ErrLinuxDescendantContainmentUnproven) || !strings.Contains(releaseErr.Error(), "descendant monitor did not stop before deadline") {
		t.Fatalf("releaseProcessGroupOwner() error = %v, want unresolved descendant monitor timeout", releaseErr)
	}

	group.mutex.Lock()
	pending := group.ownerReleasePending
	fd := group.fd
	completed := group.closeCompleted
	closeErr := group.closeErr
	group.mutex.Unlock()
	if !pending || fd != 47 || completed || !errors.Is(closeErr, ErrLinuxDescendantContainmentUnproven) || group.ContainmentCloseRetryable() {
		t.Fatalf("pending owner-release state = pending:%v fd:%d completed:%v retryable:%v closeErr:%v, want true,47,false,false,unresolved", pending, fd, completed, group.ContainmentCloseRetryable(), closeErr)
	}
	select {
	case <-closeEntered:
		t.Fatal("pidfd was released before the blocked descendant reader was released")
	default:
	}

	releaseReaderOnce.Do(func() { close(releaseReader) })
	select {
	case <-group.monitorDone:
	case <-time.After(time.Second):
		t.Fatal("descendant monitor did not stop after releasing the reader")
	}
	select {
	case <-closeEntered:
	case <-time.After(time.Second):
		t.Fatal("retire goroutine did not begin pidfd release")
	}
	allowPIDFDCloseOnce.Do(func() { close(allowPIDFDClose) })

	group.operationMutex.Lock()
	group.mutex.Lock()
	pending = group.ownerReleasePending
	fd = group.fd
	completed = group.closeCompleted
	closeErr = group.closeErr
	group.mutex.Unlock()
	group.operationMutex.Unlock()
	if closeCalls != 1 || pending || fd != -1 || !completed || !errors.Is(closeErr, ErrLinuxDescendantContainmentUnproven) || group.ContainmentCloseRetryable() {
		t.Fatalf("retired owner state = calls:%d pending:%v fd:%d completed:%v retryable:%v closeErr:%v, want 1,false,-1,true,false,unresolved", closeCalls, pending, fd, completed, group.ContainmentCloseRetryable(), closeErr)
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
	child := &linuxTestChildOwner{}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = containment.Close()
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

	if err := containment.Terminate(true); err != nil {
		t.Fatalf("Terminate(true) after forced observation failure: %v", err)
	}
	// The first Close() permanently records scan uncertainty, so a later Close()
	// must not signal again. Terminate the adopted child explicitly before that
	// sticky retry; otherwise reaping the live sleep helper can block indefinitely.
	child.reap(t)
	secondErr := containment.Close()
	if !errors.Is(secondErr, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() after leader reaping = %v, want unresolved descendant containment", secondErr)
	}
	group, ok := containment.(*processGroup)
	if !ok || !group.descendantScanLost || group.closeCompleted || group.fd < 0 || !group.ContainmentCloseRetryable() {
		t.Fatalf("sticky containment after leader reaping = (%T), want lost, incomplete, retained, retryable", containment)
	}
	if state, err := readLinuxProcessState(child.pid); err == nil && state != 'Z' && state != 'X' && state != 'x' {
		t.Fatalf("owned child %d remained live after Close(): state=%q", child.pid, state)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read owned child %d state after Close(): %v", child.pid, err)
	}
}

func TestProcessGroupCloseRejectsLiveSetSIDDescendant(t *testing.T) {
	enableLinuxTestSubreaper(t)
	command, stdin, stdout := startEscapedContainmentHelper(t)
	containment, identity, err := AttachProcess(command.Process)
	if err != nil {
		stopContainmentHelper(t, command)
		t.Fatalf("AttachProcess() error = %v", err)
	}
	if identity == "" {
		t.Fatal("AttachProcess() returned an empty identity")
	}

	waited := false
	waitStarted := false
	waitResult := make(chan error, 1)
	child := &linuxTestChildOwner{}
	group, ok := containment.(*processGroup)
	if !ok || group.escapeObserved == nil {
		t.Fatalf("containment = %T, want processGroup with descendant monitor", containment)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = stdout.Close()
		if waitStarted && !waited {
			_ = command.Process.Kill()
			if err := <-waitResult; err == nil {
				t.Errorf("leader Wait() error = nil during cleanup")
			}
		} else if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
		child.cleanup(t)
	})

	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("start escaped child: %v", err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read escaped child PID: %v", err)
	}
	child.pid, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil || child.pid <= 0 {
		t.Fatalf("escaped child PID %q: %v", line, err)
	}
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("escaped child %d was not running before leader exit: %v", child.pid, err)
	}

	select {
	case <-group.escapeObserved:
	case <-time.After(15 * time.Second):
		t.Fatal("descendant monitor did not observe setsid escape")
	}
	waitStarted = true
	go func() { waitResult <- command.Wait() }()
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("allow leader exit: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close escaped helper stdin: %v", err)
	}
	if err := <-waitResult; err != nil {
		t.Fatalf("leader Wait() error = %v, want natural exit", err)
	}
	waited = true
	closeErr := containment.Close()
	if !errors.Is(closeErr, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() error = %v, want unresolved escaped descendant", closeErr)
	}
	retryer, ok := containment.(ContainmentCloseRetryer)
	if !ok || !retryer.ContainmentCloseRetryable() {
		t.Fatalf("after escaped descendant Close() retryability = (%v, %v), want true", retryer, ok)
	}
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("setsid descendant %d was reported unresolved but is not live: %v", child.pid, err)
	}
}

func TestProcessGroupMonitorFailureRetainsUncertaintyBeforeLeaderExit(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	scanStarted := make(chan struct{})
	leaderExit := make(chan struct{})
	leaderAbsent := make(chan struct{})
	var scanOnce sync.Once
	var absentOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		select {
		case <-leaderExit:
			absentOnce.Do(func() { close(leaderAbsent) })
			return linuxProcessStat{}, os.ErrNotExist
		default:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		scanOnce.Do(func() { close(scanStarted) })
		return nil, errors.New("forced descendant scan failure")
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-scanStarted
	close(leaderExit)
	<-leaderAbsent
	if err := group.stopDescendantMonitor(time.Now().Add(containmentCloseDeadline)); err != nil {
		t.Fatalf("stopDescendantMonitor() error = %v", err)
	}

	if !group.descendantScanLost {
		t.Fatal("descendant monitor lost uncertainty after a scan failure and leader exit")
	}
	if group.monitorTerminalReason != descendantMonitorTerminalScanFailure {
		t.Fatalf("monitor terminal reason = %s, want %s", group.monitorTerminalReason, descendantMonitorTerminalScanFailure)
	}
	if err := group.Close(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() error = %v, want unresolved descendant containment", err)
	}
	if !group.ContainmentCloseRetryable() {
		t.Fatal("Close() made containment non-retryable after monitor uncertainty")
	}
}

func TestProcessGroupMonitorLeaderChildrenENOENTWithLeaderPresentFailsClosed(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	var stateMu sync.Mutex
	childrenCalls := 0

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(pid int) ([]int, error) {
		stateMu.Lock()
		childrenCalls++
		call := childrenCalls
		stateMu.Unlock()
		if call == 1 {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	t.Cleanup(func() {
		_ = group.stopDescendantMonitor(time.Now().Add(time.Second))
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	group.startDescendantMonitor()
	if err := group.waitForInitialDescendantScan(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("initial descendant scan = %v", err)
	}
	result, err := group.requestDescendantMonitorScan(time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("descendant scan request = %v", err)
	}
	if result.scanErr == nil || result.terminalReason != descendantMonitorTerminalScanFailure {
		t.Fatalf("scan result = %#v, want scan failure while the leader is still present", result)
	}
	if err := group.stopDescendantMonitor(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("stopDescendantMonitor() = %v", err)
	}
	if !group.descendantScanLost {
		t.Fatal("leader-present children ENOENT did not retain sticky scan loss")
	}
}

func TestProcessGroupMonitorTreatsLeaderReapedMidScanAsLeaderAbsent(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	var stateMu sync.Mutex
	leaderPresent := true
	var persistCalls atomic.Int32
	childrenCalls := 0

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if pid != anchor.pid {
			return linuxProcessStat{}, fmt.Errorf("unexpected stat pid %d", pid)
		}
		if !leaderPresent {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(pid int) ([]int, error) {
		stateMu.Lock()
		defer stateMu.Unlock()
		childrenCalls++
		if pid != anchor.pid {
			return nil, fmt.Errorf("unexpected children pid %d", pid)
		}
		if childrenCalls == 1 {
			return nil, nil
		}
		leaderPresent = false
		return nil, os.ErrNotExist
	}

	group := &processGroup{
		pid:            anchor.pid,
		fd:             47,
		anchor:         anchor,
		anchorCaptured: true,
		containmentUnprovenCallback: func() error {
			persistCalls.Add(1)
			return nil
		},
	}
	t.Cleanup(func() {
		_ = group.stopDescendantMonitor(time.Now().Add(time.Second))
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	group.startDescendantMonitor()
	if err := group.waitForInitialDescendantScan(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("initial descendant scan = %v", err)
	}
	result, err := group.requestDescendantMonitorScan(time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("scan after leader reaped mid-scan = %v", err)
	}
	if !result.stopped || result.scanErr != nil || result.terminalReason != descendantMonitorTerminalCleanLeaderAbsent {
		t.Fatalf("scan after leader reaped mid-scan = %#v, want clean leader absence", result)
	}
	if got := persistCalls.Load(); got != 0 {
		t.Fatalf("containment uncertainty persistence calls = %d, want 0", got)
	}
	if clean, details := group.cleanDescendantTerminalProof(result); !clean {
		t.Fatalf("terminal proof = unclean (%s), want clean", details)
	}
}

func TestProcessGroupMonitorPreservesEscapeBeforeDescendantReapedMidScan(t *testing.T) {
	for _, test := range []struct {
		name          string
		childrenAfter int
	}{
		{name: "descendant stat", childrenAfter: 1},
		{name: "children read", childrenAfter: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
			escapedPID := 2345
			missingPID := 3456
			var stateMu sync.Mutex
			childrenCalls := 0

			previousStat := readLinuxProcessStat
			previousChildren := readLinuxProcessChildren
			readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
				switch pid {
				case escapedPID:
					return linuxProcessStat{pid: escapedPID, pgrp: 9999, session: 9999, startTime: 789}, nil
				case missingPID:
					return linuxProcessStat{}, os.ErrNotExist
				}
				return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
			}
			readLinuxProcessChildren = func(pid int) ([]int, error) {
				if pid == escapedPID && test.childrenAfter == 2 {
					return nil, os.ErrNotExist
				}
				if pid != anchor.pid {
					return nil, nil
				}
				stateMu.Lock()
				childrenCalls++
				call := childrenCalls
				stateMu.Unlock()
				if call == 1 {
					return nil, nil
				}
				if test.childrenAfter == 1 {
					return []int{escapedPID, missingPID}, nil
				}
				return []int{escapedPID}, nil
			}

			group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
			t.Cleanup(func() {
				_ = group.stopDescendantMonitor(time.Now().Add(time.Second))
				readLinuxProcessStat = previousStat
				readLinuxProcessChildren = previousChildren
			})
			group.startDescendantMonitor()
			if err := group.waitForInitialDescendantScan(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("initial descendant scan = %v", err)
			}
			result, err := group.requestDescendantMonitorScan(time.Now().Add(time.Second))
			if err != nil {
				t.Fatalf("escape scan request = %v", err)
			}
			if err := group.stopDescendantMonitor(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("stopDescendantMonitor() = %v", err)
			}
			if !result.leaderPresent || result.stopped || group.monitorTerminalReason != descendantMonitorTerminalObservedEscape {
				t.Fatalf("escape scan result = %#v reason = %s, want leader-present observed escape", result, group.monitorTerminalReason)
			}
			if group.escapedDescendant == nil || group.escapedDescendant.pid != escapedPID {
				t.Fatalf("escape state = %#v, want escaped pid %d retained", group.escapedDescendant, escapedPID)
			}
			if err := (&linuxSupervisor{mirror: group}).verifyMirrorObservation(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
				t.Fatalf("escape mirror verification = %v, want unresolved proof", err)
			}
		})
	}
}

func TestLeaderVanishedDuringScanRequiresExactLeaderAbsence(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	enoent := fmt.Errorf("leader children: %w", os.ErrNotExist)
	absent := func(int) (linuxProcessStat, error) { return linuxProcessStat{}, os.ErrNotExist }
	if !leaderVanishedDuringScan(anchor.pid, anchor, enoent, absent) {
		t.Fatal("ENOENT scan with an absent leader was not classified as leader absence")
	}
	for _, test := range []struct {
		name     string
		scanErr  error
		readStat func(int) (linuxProcessStat, error)
	}{
		{name: "non-ENOENT scan error", scanErr: unix.EACCES, readStat: absent},
		{
			name:    "leader still present",
			scanErr: enoent,
			readStat: func(int) (linuxProcessStat, error) {
				return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
			},
		},
		{
			name:    "identity mismatch",
			scanErr: enoent,
			readStat: func(int) (linuxProcessStat, error) {
				return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime + 1}, nil
			},
		},
		{
			name:     "permission error",
			scanErr:  enoent,
			readStat: func(int) (linuxProcessStat, error) { return linuxProcessStat{}, unix.EPERM },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if leaderVanishedDuringScan(anchor.pid, anchor, test.scanErr, test.readStat) {
				t.Fatal("scan failure was classified as leader absence")
			}
		})
	}
}

func TestProcessGroupPersistsExistingUncertaintyWhenCallbackIsInstalled(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	markerReady := false
	markerErr := errors.New("process marker is not written")
	calls := 0
	group := &processGroup{
		pid:               anchor.pid,
		fd:                47,
		anchor:            anchor,
		anchorCaptured:    true,
		escapedDescendant: &linuxProcessStat{pid: 2345, pgrp: 2345, session: 2345, startTime: 789},
	}
	callback := func() error {
		calls++
		if !markerReady {
			return markerErr
		}
		return nil
	}

	if err := group.SetContainmentUnprovenCallback(callback); !errors.Is(err, markerErr) {
		t.Fatalf("SetContainmentUnprovenCallback() before marker = %v, want %v", err, markerErr)
	}
	if group.containmentUnprovenPersisted {
		t.Fatal("containment uncertainty was marked persisted before the process marker existed")
	}
	markerReady = true
	if err := group.SetContainmentUnprovenCallback(callback); err != nil {
		t.Fatalf("SetContainmentUnprovenCallback() after marker = %v", err)
	}
	if !group.containmentUnprovenPersisted || calls != 2 {
		t.Fatalf("containment persistence = persisted:%v calls:%d, want true and two deterministic attempts", group.containmentUnprovenPersisted, calls)
	}
}

func TestProcessGroupContainmentCallbackFailureRetainsAuthority(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	want := errors.New("marker write failed")
	group := &processGroup{
		pid:                anchor.pid,
		fd:                 47,
		anchor:             anchor,
		anchorCaptured:     true,
		descendantScanLost: true,
	}
	if err := group.SetContainmentUnprovenCallback(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("SetContainmentUnprovenCallback() = %v, want %v", err, want)
	}
	err := group.Close()
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) || !errors.Is(err, want) {
		t.Fatalf("Close() = %v, want unresolved containment wrapping %v", err, want)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("failed uncertainty persistence state = fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
}

func TestAttachProcessWaitsForInitialDescendantScan(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	anchor := linuxProcessGroupAnchor{pid: process.Pid, pgrp: int64(process.Pid), session: 77, startTime: 456}
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	var scanStartedOnce sync.Once
	readLinuxProcessChildren = func(int) ([]int, error) {
		scanStartedOnce.Do(func() { close(scanStarted) })
		<-releaseScan
		return nil, nil
	}
	var releaseScanOnce sync.Once
	var group *processGroup
	type attachResult struct {
		containment Containment
		identity    string
		err         error
	}
	result := make(chan attachResult, 1)
	t.Cleanup(func() {
		releaseScanOnce.Do(func() { close(releaseScan) })
		if group != nil && group.monitorDone != nil {
			select {
			case <-group.monitorDone:
			case <-time.After(time.Second):
				t.Errorf("initial descendant monitor did not stop during cleanup")
			}
		}
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
		readProcessIdentity = previousIdentity
		duplicatePIDFD = previousDuplicate
	})

	go func() {
		containment, identity, attachErr := AttachProcess(process)
		result <- attachResult{containment: containment, identity: identity, err: attachErr}
	}()

	select {
	case <-scanStarted:
	case <-time.After(time.Second):
		t.Fatal("initial descendant scan did not start")
	}
	select {
	case attached := <-result:
		t.Fatalf("AttachProcess() returned before initial scan completed: containment=%T identity=%q err=%v", attached.containment, attached.identity, attached.err)
	default:
	}

	releaseScanOnce.Do(func() { close(releaseScan) })
	attached := <-result
	if attached.err != nil {
		t.Fatalf("AttachProcess() error = %v, want nil after initial scan", attached.err)
	}
	if attached.identity != "expected" {
		t.Fatalf("AttachProcess() identity = %q, want expected", attached.identity)
	}
	var ok bool
	group, ok = attached.containment.(*processGroup)
	if !ok || group == nil {
		t.Fatalf("AttachProcess() containment = %T, want *processGroup", attached.containment)
	}
	if !group.initialScanComplete || group.descendantScanLost {
		t.Fatalf("initial scan state = complete:%v lost:%v, want true,false", group.initialScanComplete, group.descendantScanLost)
	}
	if err := group.stopDescendantMonitor(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("stopDescendantMonitor() = %v", err)
	}
}

func TestAttachProcessRejectsImmediateLeaderExitBeforeInitialDescendantScan(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	t.Cleanup(func() { _ = process.Release() })

	previousDuplicate := duplicatePIDFD
	previousIdentity := readProcessIdentity
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	duplicatePIDFD = func(uintptr) (int, error) { return 47, nil }
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	anchor := linuxProcessGroupAnchor{pid: process.Pid, pgrp: int64(process.Pid), session: 77, startTime: 456}
	statCalls := 0
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		statCalls++
		if statCalls == 1 {
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
		return linuxProcessStat{}, os.ErrNotExist
	}
	childrenCalls := 0
	readLinuxProcessChildren = func(int) ([]int, error) {
		childrenCalls++
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
		readProcessIdentity = previousIdentity
		duplicatePIDFD = previousDuplicate
	})

	containment, identity, attachErr := AttachProcess(process)
	if containment == nil || identity != "expected" || !errors.Is(attachErr, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("AttachProcess() = (%T, %q, %v), want retained containment, expected identity, unresolved containment", containment, identity, attachErr)
	}
	group, ok := containment.(*processGroup)
	if !ok || group == nil {
		t.Fatalf("AttachProcess() containment = %T, want *processGroup", containment)
	}
	if !strings.Contains(attachErr.Error(), "leader absence before a complete descendant scan") {
		t.Fatalf("AttachProcess() error = %v, want immediate-leader-exit barrier detail", attachErr)
	}
	if !group.initialScanComplete || !group.descendantScanLost || group.monitorTerminalReason != descendantMonitorTerminalCleanLeaderAbsent {
		t.Fatalf("initial exit state = complete:%v lost:%v terminal:%s, want true,true,leader_absent", group.initialScanComplete, group.descendantScanLost, group.monitorTerminalReason)
	}
	if childrenCalls != 0 {
		t.Fatalf("initial leader-exit scan read children %d times, want 0", childrenCalls)
	}
	select {
	case <-group.monitorDone:
	default:
		t.Fatal("AttachProcess() returned before the initial descendant monitor stopped")
	}
}

func TestProcessGroupLeaderExitAfterSuccessfulDescendantScanRemainsProven(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	leaderExit := make(chan struct{})
	leaderGone := make(chan struct{})
	scanComplete := make(chan struct{})
	var goneOnce sync.Once
	var scanOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid != anchor.pid {
			return linuxProcessStat{}, os.ErrNotExist
		}
		select {
		case <-leaderExit:
			goneOnce.Do(func() { close(leaderGone) })
			return linuxProcessStat{}, os.ErrNotExist
		default:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		scanOnce.Do(func() { close(scanComplete) })
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		return unix.ESRCH
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-scanComplete
	close(leaderExit)
	<-leaderGone
	if err := group.stopDescendantMonitor(time.Now().Add(containmentCloseDeadline)); err != nil {
		t.Fatalf("stopDescendantMonitor() = %v", err)
	}
	if !group.descendantScanCompleted || group.descendantScanLost {
		t.Fatalf("monitor state = complete:%v lost:%v, want clean scan", group.descendantScanCompleted, group.descendantScanLost)
	}
	if group.monitorTerminalReason != descendantMonitorTerminalCleanLeaderAbsent {
		t.Fatalf("monitor terminal reason = %s, want %s", group.monitorTerminalReason, descendantMonitorTerminalCleanLeaderAbsent)
	}
	if err := group.Close(); err != nil {
		t.Fatalf("Close() after clean leader reaping = %v, want nil", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0]", signals)
	}
	if group.fd != -1 || !group.closeCompleted || group.ContainmentCloseRetryable() {
		t.Fatalf("after proven leader reaping fd:%d completed:%v retryable:%v, want released, complete, non-retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
}

func TestProcessGroupCloseWaitsForDelayedCleanLeaderAbsentTerminal(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	initialScan := make(chan struct{})
	delayedScan := make(chan struct{})
	closeObserved := make(chan struct{})
	releaseScan := make(chan struct{})
	var scanCalls atomic.Int32
	var leaderAbsent atomic.Bool
	var initialOnce sync.Once
	var delayedOnce sync.Once
	var closeOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid != anchor.pid {
			return linuxProcessStat{}, os.ErrNotExist
		}
		call := scanCalls.Add(1)
		if call == 1 {
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
		if call == 2 {
			delayedOnce.Do(func() { close(delayedScan) })
			<-releaseScan
		}
		if call >= 3 {
			closeOnce.Do(func() { close(closeObserved) })
		}
		if leaderAbsent.Load() {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		initialOnce.Do(func() { close(initialScan) })
		return nil, nil
	}
	t.Cleanup(func() {
		select {
		case <-releaseScan:
		default:
			close(releaseScan)
		}
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		if signal == 0 {
			return unix.ESRCH
		}
		t.Fatalf("unexpected signal %v", signal)
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-initialScan
	leaderAbsent.Store(true)
	<-delayedScan

	closeDone := make(chan error, 1)
	go func() { closeDone <- group.Close() }()
	<-closeObserved
	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned before delayed clean terminal was released: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseScan)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() after delayed clean terminal = %v, want nil", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0]", signals)
	}
	if group.descendantScanLost || !group.descendantScanCompleted || group.fd != -1 || !group.closeCompleted {
		t.Fatalf("successful close state = lost:%v complete:%v fd:%d closed:%v, want false,true,-1,true", group.descendantScanLost, group.descendantScanCompleted, group.fd, group.closeCompleted)
	}
}

func TestProcessGroupCloseWaitsForInitialScanBeforeCleanLeaderAbsentTerminal(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	initialChildrenEntered := make(chan struct{})
	releaseInitialScan := make(chan struct{})
	closeObserved := make(chan struct{})
	var statCalls atomic.Int32
	var leaderAbsent atomic.Bool
	var initialOnce sync.Once
	var closeOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid != anchor.pid {
			return linuxProcessStat{}, os.ErrNotExist
		}
		call := statCalls.Add(1)
		if call == 1 {
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
		if call >= 2 {
			closeOnce.Do(func() { close(closeObserved) })
		}
		if leaderAbsent.Load() {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		if statCalls.Load() == 1 {
			initialOnce.Do(func() { close(initialChildrenEntered) })
			<-releaseInitialScan
		}
		return nil, nil
	}
	t.Cleanup(func() {
		select {
		case <-releaseInitialScan:
		default:
			close(releaseInitialScan)
		}
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		if signal == 0 {
			return unix.ESRCH
		}
		t.Fatalf("unexpected signal %v", signal)
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-initialChildrenEntered
	leaderAbsent.Store(true)
	closeDone := make(chan error, 1)
	go func() { closeDone <- group.Close() }()
	<-closeObserved
	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned before initial scan was released: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseInitialScan)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() after delayed initial scan = %v, want nil", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0]", signals)
	}
	if group.descendantScanLost || !group.descendantScanCompleted || group.fd != -1 || !group.closeCompleted {
		t.Fatalf("successful close state = lost:%v complete:%v fd:%d closed:%v, want false,true,-1,true", group.descendantScanLost, group.descendantScanCompleted, group.fd, group.closeCompleted)
	}
}

func TestProcessGroupCloseRejectsLeaderDisappearanceWithoutPostReadbackScan(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	initialScan := make(chan struct{})
	var initialScanOnce sync.Once
	leaderPresent := true

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if !leaderPresent {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		initialScanOnce.Do(func() { close(initialScan) })
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		if signal == 0 {
			return unix.ESRCH
		}
		t.Fatalf("unexpected signal %v", signal)
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-initialScan
	if err := group.stopDescendantMonitor(time.Now().Add(containmentCloseDeadline)); err != nil {
		t.Fatalf("stopDescendantMonitor() = %v", err)
	}
	leaderPresent = false

	err := group.Close()
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() without post-readback scan = %v, want unresolved containment", err)
	}
	if group.monitorTerminalReason != descendantMonitorTerminalOwnerStop {
		t.Fatalf("monitor terminal reason = %s, want %s", group.monitorTerminalReason, descendantMonitorTerminalOwnerStop)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0] before unresolved result", signals)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after missing post-readback scan fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
}

func TestCleanDescendantTerminalProofReportsUnprovenState(t *testing.T) {
	group := &processGroup{
		descendantScanCompleted: true,
		descendantScanLost:      true,
	}
	result := descendantMonitorScanResult{terminalReason: descendantMonitorTerminalCleanLeaderAbsent}

	clean, details := group.cleanDescendantTerminalProof(result)
	if clean {
		t.Fatal("cleanDescendantTerminalProof() = true, want false after scan loss")
	}
	for _, detail := range []string{
		"terminal_reason=leader_absent",
		"scan_completed=true",
		"scan_lost=true",
		"observed_escape=false",
	} {
		if !strings.Contains(details, detail) {
			t.Fatalf("proof details = %q, want %q", details, detail)
		}
	}
}

func TestProcessGroupCloseSucceedsAfterPostReadbackDescendantScan(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	initialScan := make(chan struct{})
	postReadbackScan := make(chan struct{})
	readbackComplete := make(chan struct{})
	var initialScanOnce sync.Once
	var postReadbackScanOnce sync.Once
	var readbackOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		initialScanOnce.Do(func() { close(initialScan) })
		select {
		case <-readbackComplete:
			postReadbackScanOnce.Do(func() { close(postReadbackScan) })
		default:
		}
		return nil, nil
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		if signal == 0 {
			readbackOnce.Do(func() { close(readbackComplete) })
			return unix.ESRCH
		}
		t.Fatalf("unexpected signal %v", signal)
		return nil
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-initialScan
	if err := group.Close(); err != nil {
		t.Fatalf("Close() with post-readback descendant scan = %v, want nil", err)
	}
	select {
	case <-postReadbackScan:
	default:
		t.Fatal("Close() returned without a descendant scan after group readback")
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) {
		t.Fatalf("signals = %#v, want [SIGKILL 0]", signals)
	}
	if group.descendantScanLost || !group.descendantScanCompleted || group.fd != -1 || !group.closeCompleted {
		t.Fatalf("successful close state = lost:%v complete:%v fd:%d closed:%v, want false,true,-1,true", group.descendantScanLost, group.descendantScanCompleted, group.fd, group.closeCompleted)
	}
}

func TestProcessGroupCloseBoundsBlockedDescendantMonitor(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return nil, nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-entered
	deadline := time.Now().Add(25 * time.Millisecond)
	started := time.Now()
	err := group.closeUntil(deadline)
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("closeUntil() error = %v, want bounded unresolved result", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("closeUntil() took %v after monitor deadline, want bounded return", elapsed)
	}
	if group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after monitor timeout fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}

	close(release)
	<-group.monitorDone
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

func startEscapedContainmentHelper(t *testing.T) (*exec.Cmd, io.WriteCloser, io.ReadCloser) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "leader-holds-setsid-child")
	command.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("create escaped helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("create escaped helper stdout: %v", err)
	}
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start escaped helper: %v", err)
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
	case "leader-holds-setsid-child":
		var release [1]byte
		if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(6)
		}
		readyReader, readyWriter, err := os.Pipe()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(7)
		}
		defer readyReader.Close()
		child := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "setsid-child")
		child.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
		child.ExtraFiles = []*os.File{readyWriter}
		if err := child.Start(); err != nil {
			readyWriter.Close()
			fmt.Fprint(os.Stderr, err)
			os.Exit(8)
		}
		readyWriter.Close()
		var ready [1]byte
		if _, err := io.ReadFull(readyReader, ready[:]); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(9)
		}
		if _, err := fmt.Fprintln(os.Stdout, child.Process.Pid); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(10)
		}
		if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(11)
		}
	case "setsid-child":
		if _, err := unix.Setsid(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(12)
		}
		readyWriter := os.NewFile(uintptr(3), "ready")
		if readyWriter == nil {
			fmt.Fprint(os.Stderr, "ready pipe is unavailable")
			os.Exit(13)
		}
		if _, err := readyWriter.Write([]byte{1}); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(14)
		}
		_ = readyWriter.Close()
		for {
			if err := unix.Pause(); err != nil && !errors.Is(err, unix.EINTR) {
				fmt.Fprint(os.Stderr, err)
				os.Exit(15)
			}
		}
	default:
		os.Exit(2)
	}
}
