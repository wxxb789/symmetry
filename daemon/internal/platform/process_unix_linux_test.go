//go:build linux

package platform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0, unix.SIGKILL}) || group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after sticky scan recovery signals:%#v fd:%d completed:%v retryable:%v, want [SIGKILL 0 SIGKILL], retained, false, true", signals, group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
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
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) && !slices.Equal(signals, []unix.Signal{unix.SIGKILL, unix.SIGKILL}) && !slices.Equal(signals, []unix.Signal{unix.SIGKILL, 0}) && !slices.Equal(signals, []unix.Signal{unix.SIGKILL, unix.SIGKILL, 0}) {
		t.Fatalf("termination signals = %#v, want one or two SIGKILL signals, optionally followed by a group-exit probe", signals)
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
	stopDeadline := time.Now().Add(containmentCloseDeadline)
	firstErr := containment.Close()
	if !errors.Is(firstErr, ErrLinuxDescendantContainmentUnproven) || !strings.Contains(firstErr.Error(), "forced process-group observation failure") {
		t.Fatalf("first Close() after forced observation failure = %v, want unresolved containment preserving the read error", firstErr)
	}
	retryer, ok := containment.(ContainmentCloseRetryer)
	if !ok || !retryer.ContainmentCloseRetryable() {
		t.Fatalf("after forced observation failure retryability = (%v, %v), want implemented and true", retryer, ok)
	}
	if group, ok := containment.(*processGroup); !ok || group.fd < 0 || group.closeCompleted {
		t.Fatalf("after forced observation failure containment = (%T), want retained incomplete processGroup fd", containment)
	}

	// The failed leader observation cannot prove containment, but the retained
	// pidfd still safely stops same-group descendants before Close returns.
	waitForLinuxTestProcessStopped(t, child.pid, stopDeadline)
	child.reap(t)
	secondErr := containment.Close()
	if !errors.Is(secondErr, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() after leader reaping = %v, want unresolved descendant containment", secondErr)
	}
	waitForLinuxTestProcessStopped(t, child.pid, stopDeadline)
	child.reap(t)
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

func TestProcessGroupCloseStopsLiveChildAfterObservedScanLoss(t *testing.T) {
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
	group, ok := containment.(*processGroup)
	if !ok {
		t.Fatalf("containment = %T, want processGroup", containment)
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
	childStat, err := readLinuxProcessStat(child.pid)
	if err != nil || childStat.pgrp != group.anchor.pgrp || childStat.session != group.anchor.session {
		t.Fatalf("owned child %d fence = %#v err:%v, want original process group %d/session %d", child.pid, childStat, err, group.anchor.pgrp, group.anchor.session)
	}
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("owned child %d was not running before leader exit: %v", child.pid, err)
	}

	group.mutex.Lock()
	group.monitorReadChildren = func(int) ([]int, error) {
		return nil, errors.New("forced descendant scan failure")
	}
	group.mutex.Unlock()
	scan, scanErr := group.requestDescendantMonitorScan(time.Now().Add(containmentCloseDeadline))
	group.mutex.Lock()
	scanLost := group.descendantScanLost
	group.mutex.Unlock()
	if scanErr != nil || scan.scanErr == nil || !scanLost {
		t.Fatalf("forced monitor scan = result:%#v requestErr:%v lost:%v, want observed scan loss", scan, scanErr, scanLost)
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
	if _, err := group.requestDescendantMonitorScan(time.Now().Add(containmentCloseDeadline)); err != nil && !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("final monitor scan after leader exit = %v", err)
	}
	select {
	case <-group.monitorDone:
	case <-time.After(containmentCloseDeadline):
		t.Fatal("descendant monitor did not stop after natural leader exit")
	}
	if !group.descendantScanLost {
		t.Fatal("monitor scan uncertainty was cleared after natural leader exit")
	}

	signalFailure := errors.New("forced pidfd process-group signal failure")
	previousSend := sendPIDFDSignal
	var signals []unix.Signal
	sendPIDFDSignal = func(fd int, signal unix.Signal, _ *unix.Siginfo, flags int) error {
		if fd != group.fd || flags != unix.PIDFD_SIGNAL_PROCESS_GROUP {
			t.Errorf("force-stop authority = fd:%d flags:%d, want retained pidfd fd:%d and process-group signalling", fd, flags, group.fd)
		}
		signals = append(signals, signal)
		return signalFailure
	}
	t.Cleanup(func() { sendPIDFDSignal = previousSend })

	firstErr := containment.Close()
	if !errors.Is(firstErr, ErrLinuxDescendantContainmentUnproven) || !errors.Is(firstErr, signalFailure) {
		t.Fatalf("Close() after forced group-signal failure = %v, want unresolved containment wrapping signal failure", firstErr)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) || !group.descendantScanLost || group.fd < 0 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after failed force stop signals:%#v fd:%d completed:%v retryable:%v, want one SIGKILL, retained, incomplete, retryable", signals, group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
	if state, err := readLinuxProcessState(child.pid); err != nil || state == 'Z' || state == 'X' || state == 'x' {
		t.Fatalf("owned child %d after failed force stop = state:%q err:%v, want live", child.pid, state, err)
	}

	sendPIDFDSignal = func(fd int, signal unix.Signal, info *unix.Siginfo, flags int) error {
		if fd != group.fd || flags != unix.PIDFD_SIGNAL_PROCESS_GROUP {
			t.Errorf("retry force-stop authority = fd:%d flags:%d, want retained pidfd fd:%d and process-group signalling", fd, flags, group.fd)
		}
		signals = append(signals, signal)
		return previousSend(fd, signal, info, flags)
	}
	stopDeadline := time.Now().Add(containmentCloseDeadline)
	secondErr := containment.Close()
	if !errors.Is(secondErr, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() after successful force-stop retry = %v, want unresolved containment", secondErr)
	}
	waitForLinuxTestProcessStopped(t, child.pid, stopDeadline)
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL, unix.SIGKILL}) {
		t.Fatalf("force-stop signal calls = %#v, want failed SIGKILL and pidfd-bound retry", signals)
	}
	if !group.descendantScanLost || group.closeCompleted || group.fd < 0 || !group.ContainmentCloseRetryable() {
		t.Fatalf("after successful force-stop retry lost:%v completed:%v fd:%d retryable:%v, want uncertainty retained and authority retryable", group.descendantScanLost, group.closeCompleted, group.fd, group.ContainmentCloseRetryable())
	}
	child.reap(t)
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
	var scanOnce sync.Once

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(int) (linuxProcessStat, error) {
		select {
		case <-leaderExit:
			return linuxProcessStat{}, os.ErrNotExist
		default:
			return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		scanOnce.Do(func() { close(scanStarted) })
		<-leaderExit
		return nil, errors.New("forced descendant scan failure")
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		return unix.ESRCH
	}, func(int) error { return nil })

	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-scanStarted
	close(leaderExit)
	select {
	case <-group.monitorDone:
	case <-time.After(containmentCloseDeadline):
		t.Fatal("descendant monitor did not stop after the scan failure")
	}
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
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) {
		t.Fatalf("Close() signals = %#v, want a pidfd-bound SIGKILL attempt without a proof probe", signals)
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
			if !result.leaderPresent || result.stopped || result.scanErr != nil || group.monitorTerminalReason != descendantMonitorTerminalObservedEscape {
				t.Fatalf("escape scan result = %#v reason = %s, want leader-present observed escape without scan error", result, group.monitorTerminalReason)
			}
			if group.descendantScanLost {
				t.Fatal("descendant reaped mid-scan was recorded as scan loss")
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

func TestProcessGroupSealWaitsForMonitorJoinAndReplaysExactSequence(t *testing.T) {
	done := make(chan struct{})
	var events []descendantScanEvent
	var afterSealSequence uint64
	sealCalls := 0
	group := &processGroup{
		monitorDone:                     done,
		descendantScanSequence:          7,
		descendantScanCompletedSequence: 7,
		descendantScanCompleted:         true,
		descendantScanObserver: func(event descendantScanEvent) error {
			events = append(events, event)
			if event.Phase == "seal" {
				sealCalls++
				if sealCalls == 1 {
					return errors.New("seal write outcome is unknown")
				}
			}
			return nil
		},
		afterDescendantScanSeal: func(sequence uint64) {
			afterSealSequence = sequence
		},
	}
	if err := group.sealDescendantScanAfterMonitorStop(); err == nil {
		t.Fatal("scanner seal succeeded before the monitor joined")
	}
	if len(events) != 0 || afterSealSequence != 0 {
		t.Fatalf("pre-join seal side effects = events:%#v marker sequence:%d", events, afterSealSequence)
	}
	close(done)
	if err := group.sealDescendantScanAfterMonitorStop(); err != nil {
		t.Fatalf("seal after monitor join with exact replay = %v", err)
	}
	if len(events) != 2 || events[0] != (descendantScanEvent{Sequence: 7, Phase: "seal"}) || events[1] != events[0] {
		t.Fatalf("seal replay events = %#v, want two exact sequence-7 seal updates", events)
	}
	if !group.descendantScanSealed || group.recoverySealSequence != 7 || afterSealSequence != 7 {
		t.Fatalf("sealed local state = sealed:%v sequence:%d marker:%d", group.descendantScanSealed, group.recoverySealSequence, afterSealSequence)
	}
	if err := group.assertDescendantScanSealedLocally(); err != nil {
		t.Fatalf("exact local seal was not available after acknowledgement: %v", err)
	}

	empty := &processGroup{monitorDone: make(chan struct{})}
	close(empty.monitorDone)
	if err := empty.sealDescendantScanAfterMonitorStop(); err != nil || empty.descendantScanSealed {
		t.Fatalf("empty pre-scan abort seal = err:%v sealed:%v, want no invented seal", err, empty.descendantScanSealed)
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
	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		return unix.EPERM
	}, func(int) error { return nil })
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
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) || !errors.Is(err, want) || !errors.Is(err, unix.EPERM) {
		t.Fatalf("Close() = %v, want unresolved containment wrapping marker and force-stop errors", err)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) || group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("failed uncertainty persistence state = fd:%d completed:%v retryable:%v, want retained, incomplete, retryable", group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}
}

func TestProcessGroupDoesNotReadProcBeforeDurableScanAcknowledgement(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	statReads := 0
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		statReads++
		return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	ackErr := errors.New("durable scan begin was not acknowledged")
	var events []descendantScanEvent
	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	t.Cleanup(func() { _ = group.stopDescendantMonitor(time.Now().Add(time.Second)) })
	if err := group.setDescendantScanObserver(func(event descendantScanEvent) error {
		events = append(events, event)
		if event.Phase == "begin" {
			return ackErr
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	group.startDescendantMonitor()
	if err := group.waitForInitialDescendantScan(time.Now().Add(time.Second)); !errors.Is(err, ackErr) {
		t.Fatalf("initial scan without durable begin ack = %v, want %v", err, ackErr)
	}
	if statReads != 0 {
		t.Fatalf("/proc stat reads = %d before pre-scan ack, want zero", statReads)
	}
	if len(events) != 2 || events[0].Phase != "begin" || events[1].Phase != "unproven" || events[0].Sequence != 1 || events[1].Sequence != 1 {
		t.Fatalf("scan observer events = %#v, want begin and fail-closed sequence-one result", events)
	}
}

func TestProcessGroupAcknowledgesObservedEscapeBeforeRetiringScanFence(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	const escapedPID = 2345
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid == anchor.pid {
			return linuxProcessStat{pid: pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
		}
		if pid == escapedPID {
			return linuxProcessStat{pid: pid, pgrp: 9999, session: 9999, startTime: 789}, nil
		}
		return linuxProcessStat{}, unix.ESRCH
	}
	readLinuxProcessChildren = func(pid int) ([]int, error) {
		if pid == anchor.pid {
			return []int{escapedPID}, nil
		}
		return nil, nil
	}
	var events []descendantScanEvent
	group := &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	t.Cleanup(func() { _ = group.stopDescendantMonitor(time.Now().Add(time.Second)) })
	if err := group.setDescendantScanObserver(func(event descendantScanEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	group.startDescendantMonitor()
	err := group.waitForInitialDescendantScan(time.Now().Add(time.Second))
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("initial escaped scan = %v, want unproven containment", err)
	}
	if len(events) != 2 || events[0].Phase != "begin" || events[1].Phase != "unproven" || events[0].Sequence != events[1].Sequence || events[1].FailureKind != "observed_escape" {
		t.Fatalf("escape scan events = %#v, want matching begin then durable observed_escape", events)
	}
	if group.escapedDescendant == nil || group.monitorTerminalReason != descendantMonitorTerminalObservedEscape {
		t.Fatalf("scan state = escape:%#v terminal:%s, want retained observed escape", group.escapedDescendant, group.monitorTerminalReason)
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
	if !group.initialScanComplete || !group.descendantScanLost {
		t.Fatalf("initial exit state = complete:%v lost:%v, want retained scan uncertainty", group.initialScanComplete, group.descendantScanLost)
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

func TestProcessGroupCloseForceStopsWhenFinalMonitorScanTimesOut(t *testing.T) {
	anchor := linuxProcessGroupAnchor{pid: 1234, pgrp: 1234, session: 77, startTime: 456}
	childrenEntered := make(chan struct{})
	releaseChildren := make(chan struct{})
	var enteredOnce sync.Once
	var leaderAbsent atomic.Bool
	var group *processGroup
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid != anchor.pid {
			return linuxProcessStat{}, os.ErrNotExist
		}
		if leaderAbsent.Load() {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: anchor.pid, pgrp: anchor.pgrp, session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) {
		enteredOnce.Do(func() { close(childrenEntered) })
		<-releaseChildren
		return nil, nil
	}
	t.Cleanup(func() {
		select {
		case <-releaseChildren:
		default:
			close(releaseChildren)
		}
		if group != nil && group.monitorDone != nil {
			<-group.monitorDone
		}
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})

	var signals []unix.Signal
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals = append(signals, signal)
		if signal != unix.SIGKILL {
			t.Fatalf("signal = %v, want only unresolved SIGKILL attempt", signal)
		}
		return nil
	}, func(int) error { return nil })

	group = &processGroup{pid: anchor.pid, fd: 47, anchor: anchor, anchorCaptured: true}
	group.startDescendantMonitor()
	<-childrenEntered
	leaderAbsent.Store(true)
	deadline := time.Now().Add(25 * time.Millisecond)
	started := time.Now()
	err := group.closeUntil(deadline)
	if !errors.Is(err, ErrLinuxDescendantContainmentUnproven) || !strings.Contains(err.Error(), "final scan timed out") {
		t.Fatalf("closeUntil() error = %v, want preserved final-scan timeout and unresolved containment", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("closeUntil() took %v after final-scan deadline, want bounded return", elapsed)
	}
	if !slices.Equal(signals, []unix.Signal{unix.SIGKILL}) {
		t.Fatalf("closeUntil() signals = %#v, want SIGKILL without a proof probe", signals)
	}
	if !group.descendantScanLost || group.fd != 47 || group.closeCompleted || !group.ContainmentCloseRetryable() {
		t.Fatalf("after final-scan timeout lost:%v fd:%d completed:%v retryable:%v, want unresolved retained authority", group.descendantScanLost, group.fd, group.closeCompleted, group.ContainmentCloseRetryable())
	}

	close(releaseChildren)
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

func waitForLinuxTestProcessStopped(t *testing.T, pid int, deadline time.Time) {
	t.Helper()
	for {
		state, err := readLinuxProcessState(pid)
		stopped := errors.Is(err, os.ErrNotExist) || err == nil && (state == 'Z' || state == 'X' || state == 'x')
		if stopped {
			if !time.Now().Before(deadline) {
				t.Fatalf("process %d stopped after the containment deadline", pid)
			}
			return
		}
		if err != nil {
			t.Fatalf("read process %d state while waiting for stop: %v", pid, err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("process %d remained live in state %q after stop deadline", pid, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	case "worker-thread-child":
		// Fork from a locked thread other than the main thread so only that
		// task's children file lists the child.
		started := make(chan int)
		var forkFromWorker func()
		forkFromWorker = func() {
			runtime.LockOSThread()
			if unix.Gettid() == os.Getpid() {
				runtime.UnlockOSThread()
				go forkFromWorker()
				return
			}
			child := exec.Command("sleep", "30")
			if err := child.Start(); err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(16)
			}
			started <- child.Process.Pid
			select {}
		}
		go forkFromWorker()
		if _, err := fmt.Fprintln(os.Stdout, <-started); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(17)
		}
		var release [1]byte
		_, _ = io.ReadFull(os.Stdin, release[:])
	default:
		os.Exit(2)
	}
}

func TestReadLinuxProcessChildrenIncludesWorkerThreadChildren(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "worker-thread-child")
	command.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var childPID int
	t.Cleanup(func() {
		if childPID > 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
		_ = stdin.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read worker-thread child PID: %v", err)
	}
	childPID, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil || childPID <= 0 {
		t.Fatalf("worker-thread child PID %q: %v", line, err)
	}
	children, err := readLinuxProcessChildrenFile(command.Process.Pid)
	if err != nil {
		t.Fatalf("readLinuxProcessChildrenFile() error = %v", err)
	}
	for _, child := range children {
		if child == childPID {
			return
		}
	}
	t.Fatalf("readLinuxProcessChildrenFile(%d) = %v, want worker-thread child %d", command.Process.Pid, children, childPID)
}

// An exiting worker thread hands its children to the main thread. The main
// thread is read last so a child moved during the pass is still observed.
func TestReadLinuxThreadChildrenSeesChildMovedFromExitingWorker(t *testing.T) {
	const pid, worker, child = 100, 101, 200
	for _, workerErr := range []error{nil, os.ErrNotExist} {
		t.Run(fmt.Sprintf("worker read error %v", workerErr), func(t *testing.T) {
			moved := false
			readTask := func(_ int, tid int) ([]byte, error) {
				switch {
				case tid == worker && !moved:
					moved = true
					if workerErr != nil {
						return nil, workerErr
					}
					return nil, nil
				case tid == pid && moved:
					return []byte(strconv.Itoa(child)), nil
				}
				return nil, nil
			}
			children, err := readLinuxThreadChildren(pid, func(int) ([]int, error) { return []int{pid, worker}, nil }, readTask)
			if err != nil || !slices.Contains(children, child) {
				t.Fatalf("readLinuxThreadChildren() = (%v, %v), want child %d moved to the main thread", children, err, child)
			}
		})
	}
	if _, err := readLinuxThreadChildren(pid, func(int) ([]int, error) { return []int{pid}, nil }, func(int, int) ([]byte, error) {
		return nil, os.ErrNotExist
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing main thread error = %v, want not-exist for leader-absence handling", err)
	}
}

const (
	linuxPIDReuseNamespaceHelperEnv = "SYMMETRY_LINUX_PID_REUSE_NAMESPACE_HELPER"
	linuxPIDReuseParentNamespaceEnv = "SYMMETRY_LINUX_PID_REUSE_PARENT_NAMESPACE"
	linuxPIDReuseVictimEnv          = "SYMMETRY_LINUX_PID_REUSE_VICTIM"
)

// This exercises native kernel/platform identity and pidfd behavior only; it
// does not prove daemon restart recovery.
func TestLinuxPIDReuseContainmentRejectsReusedPID(t *testing.T) {
	if err := checkPIDFDGroupSupport(); err != nil {
		t.Fatalf("required prerequisite unavailable: Linux pidfd process-group signalling: %v", err)
	}
	unsharePath, err := exec.LookPath("unshare")
	if err != nil {
		t.Fatalf("required prerequisite unavailable: unshare is required for a private user+PID namespace: %v", err)
	}
	parentNamespace, err := readLinuxPIDNamespaceInfo(0)
	if err != nil {
		t.Fatalf("read parent PID namespace before unshare: %v", err)
	}

	var output bytes.Buffer
	command := exec.Command(
		unsharePath,
		"--user", "--map-root-user", "--pid", "--fork", "--mount-proc",
		os.Args[0], "-test.run=^TestLinuxPIDReuseNamespaceWitnessHelper$", "-test.v", "-test.timeout=25s",
	)
	command.Env = append(os.Environ(),
		linuxPIDReuseNamespaceHelperEnv+"=1",
		fmt.Sprintf("%s=%d", linuxPIDReuseParentNamespaceEnv, parentNamespace.inode),
	)
	command.Stdout = &output
	command.Stderr = &output
	pidfd := -1
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &pidfd}
	if err := command.Start(); err != nil {
		t.Fatalf("required prerequisite unavailable: could not start unshare for a private user+PID namespace: %v", err)
	}
	if pidfd < 0 {
		waitErr := command.Wait()
		t.Fatalf("required prerequisite unavailable: Go did not retain an unshare pidfd for bounded cleanup (wait error: %v); output:\n%s", waitErr, output.String())
	}
	t.Cleanup(func() {
		if err := unix.Close(pidfd); err != nil {
			t.Errorf("close unshare pidfd: %v", err)
		}
	})
	waitForLinuxPIDReuseNamespaceCommand(t, command, pidfd, &output)
	t.Logf("private namespace witness output:\n%s", strings.TrimSpace(output.String()))
}

func TestLinuxPIDReuseNamespaceWitnessHelper(t *testing.T) {
	if os.Getenv(linuxPIDReuseNamespaceHelperEnv) != "1" {
		return
	}
	parentNamespaceInode, err := strconv.ParseUint(os.Getenv(linuxPIDReuseParentNamespaceEnv), 10, 64)
	if err != nil || parentNamespaceInode == 0 {
		t.Fatalf("parent PID namespace inode %q is invalid: %v", os.Getenv(linuxPIDReuseParentNamespaceEnv), err)
	}
	if os.Getpid() != 1 {
		t.Fatalf("private PID namespace helper PID = %d, want PID 1", os.Getpid())
	}
	privateNamespace, err := readLinuxPIDNamespaceInfo(0)
	if err != nil {
		t.Fatalf("read private PID namespace identity: %v", err)
	}
	if privateNamespace.inode == parentNamespaceInode {
		t.Fatalf("private PID namespace inode = %d, same as parent namespace; refusing to touch ns_last_pid", privateNamespace.inode)
	}
	if err := checkPIDFDGroupSupport(); err != nil {
		t.Fatalf("required prerequisite unavailable inside the private PID namespace: pidfd process-group signalling: %v", err)
	}

	original := startLinuxPIDReuseChild(t)
	originalPID := original.command.Process.Pid
	if originalPID <= 1 {
		t.Fatalf("original child PID = %d, want a non-init process in the private namespace", originalPID)
	}
	originalIdentityValue, originalIdentity, originalAnchor := captureLinuxPIDReuseIdentity(t, originalPID, privateNamespace.inode)
	// Process start times are recorded in clock ticks; this separates the two
	// identities even when the replacement is allocated immediately after reap.
	time.Sleep(100 * time.Millisecond)
	original.killAndReap(t)
	if err := unix.PidfdSendSignal(original.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("original pidfd group signal-0 after reap = %v, want ESRCH", err)
	}

	lastPIDPath := "/proc/sys/kernel/ns_last_pid"
	resetValue := strconv.Itoa(originalPID - 1)
	if err := os.WriteFile(lastPIDPath, []byte(resetValue), 0); err != nil {
		t.Fatalf("required prerequisite unavailable: private PID namespace-local %s must be writable; host-global PID allocator was not modified: %v", lastPIDPath, err)
	}
	lastPIDBytes, err := os.ReadFile(lastPIDPath)
	if err != nil {
		t.Fatalf("read back namespace-local ns_last_pid: %v", err)
	}
	lastPID, err := strconv.Atoi(strings.TrimSpace(string(lastPIDBytes)))
	if err != nil || lastPID != originalPID-1 {
		t.Fatalf("namespace-local ns_last_pid = %q (parsed %d, error %v), want %d", strings.TrimSpace(string(lastPIDBytes)), lastPID, err, originalPID-1)
	}

	replacement := startLinuxPIDReuseChild(t)
	replacementPID := replacement.command.Process.Pid
	if replacementPID != originalPID {
		t.Fatalf("private namespace reused PID = %d, want original PID %d after resetting namespace-local ns_last_pid", replacementPID, originalPID)
	}
	replacementIdentityValue, replacementIdentity, replacementAnchor := captureLinuxPIDReuseIdentity(t, replacementPID, privateNamespace.inode)
	if replacementIdentity.bootID != originalIdentity.bootID || replacementIdentity.pidNamespaceInode != originalIdentity.pidNamespaceInode || replacementIdentity.pid != originalIdentity.pid {
		t.Fatalf("reused identity tuple = %#v, original = %#v; boot ID, PID namespace, and numeric PID must match", replacementIdentity, originalIdentity)
	}
	if replacementIdentity.startTime <= originalIdentity.startTime {
		t.Fatalf("reused PID start time = %d, original start time = %d; expected a later process identity", replacementIdentity.startTime, originalIdentity.startTime)
	}
	if replacementAnchor.pgrp != originalAnchor.pgrp || replacementAnchor.session != originalAnchor.session {
		t.Fatalf("replacement group fence = %#v, original = %#v; numeric process-group reuse was not established", replacementAnchor, originalAnchor)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("replacement pidfd group signal-0 = %v, want live reused process group", err)
	}

	if err := unix.PidfdSendSignal(original.pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("stale original pidfd group SIGKILL = %v, want ESRCH without targeting replacement", err)
	}
	if err := unix.PidfdSendSignal(original.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("stale original pidfd group signal-0 with replacement alive = %v, want ESRCH", err)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("replacement pidfd group signal-0 after stale original signal = %v, want replacement still alive", err)
	}
	if replacementIdentityValue == originalIdentityValue {
		t.Fatal("ProcessIdentity returned the same identity for the original and its reused PID")
	}

	if err := ProvePersistedProcessGroupAbsent(context.Background(), originalPID, originalIdentityValue); !errors.Is(err, ErrPersistedProcessGroupUnproven) || !errors.Is(err, errPersistedProcessGroupPresent) {
		t.Fatalf("persisted group absence proof with a live reused numeric group = %v, want unproven because the group is present", err)
	}

	group := &processGroup{pid: originalPID, fd: original.pidfd, anchor: originalAnchor, anchorCaptured: true}
	original.pidfd = -1 // transfer the retained original pidfd to processGroup
	t.Cleanup(func() {
		if group.fd >= 0 {
			if err := unix.Close(group.fd); err != nil {
				t.Errorf("close stale original process-group pidfd: %v", err)
			}
			group.fd = -1
		}
	})
	if err := group.Close(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
		t.Fatalf("Close() with a reused leader PID = %v, want containment to remain unproven", err)
	}
	if group.closeCompleted || group.physicalStopProven || !group.descendantScanLost || group.fd < 0 {
		t.Fatalf("stale process-group close state = completed:%t physical-stop:%t scan-lost:%t pidfd:%d; want unresolved retained authority", group.closeCompleted, group.physicalStopProven, group.descendantScanLost, group.fd)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("replacement pidfd group signal-0 after Close() = %v, want replacement still alive", err)
	}
	if currentIdentity, err := ProcessIdentity(replacementPID); err != nil || currentIdentity != replacementIdentityValue {
		t.Fatalf("replacement identity after stale Close() = %q, %v; want unchanged identity %q", currentIdentity, err, replacementIdentityValue)
	}
	t.Logf("reused PID witness: pid=%d pidns=%d boot=%s starttime=%d->%d; stale original group signal returned ESRCH while replacement stayed alive", originalPID, privateNamespace.inode, originalIdentity.bootID, originalIdentity.startTime, replacementIdentity.startTime)
}

func TestLinuxPIDReuseVictim(t *testing.T) {
	if os.Getenv(linuxPIDReuseVictimEnv) != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatalf("wait for process owner release: %v", err)
	}
}

type linuxPIDReuseChild struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	pidfd   int
	reaped  bool
}

func startLinuxPIDReuseChild(t *testing.T) *linuxPIDReuseChild {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxPIDReuseVictim$")
	command.Env = append(os.Environ(), linuxPIDReuseVictimEnv+"=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("create PID-reuse victim stdin: %v", err)
	}
	pidfd := -1
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
		PidFD:     &pidfd,
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		t.Fatalf("start PID-reuse victim: %v", err)
	}
	child := &linuxPIDReuseChild{command: command, stdin: stdin, pidfd: pidfd}
	t.Cleanup(func() { child.cleanup(t) })
	if pidfd < 0 {
		_ = stdin.Close()
		child.stdin = nil
		if err := command.Wait(); err != nil {
			t.Fatalf("wait for victim after pidfd acquisition failure: %v", err)
		}
		child.reaped = true
		t.Fatalf("required prerequisite unavailable: victim launch did not return a pidfd")
	}
	return child
}

func (child *linuxPIDReuseChild) killAndReap(t *testing.T) {
	t.Helper()
	if err := unix.PidfdSendSignal(child.pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("kill original process group through retained pidfd: %v", err)
	}
	err := child.command.Wait()
	child.reaped = true
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Wait() after pidfd SIGKILL = %v, want signal termination", err)
	}
	status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("victim wait status = %#v, want SIGKILL", exitErr.ProcessState.Sys())
	}
}

func (child *linuxPIDReuseChild) cleanup(t *testing.T) {
	t.Helper()
	if child == nil {
		return
	}
	if !child.reaped && child.pidfd >= 0 {
		if err := unix.PidfdSendSignal(child.pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil && !errors.Is(err, unix.ESRCH) {
			t.Errorf("kill PID-reuse child during cleanup through pidfd: %v", err)
		}
	}
	if child.stdin != nil {
		if err := child.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Errorf("close PID-reuse child stdin: %v", err)
		}
		child.stdin = nil
	}
	if !child.reaped {
		err := child.command.Wait()
		child.reaped = true
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Errorf("wait for PID-reuse child during cleanup: %v", err)
		}
	}
	if child.pidfd >= 0 {
		if err := unix.Close(child.pidfd); err != nil {
			t.Errorf("close PID-reuse child pidfd: %v", err)
		}
		child.pidfd = -1
	}
}

func captureLinuxPIDReuseIdentity(t *testing.T, pid int, namespaceInode uint64) (string, linuxProcessIdentity, linuxProcessGroupAnchor) {
	t.Helper()
	identityValue, err := ProcessIdentity(pid)
	if err != nil {
		t.Fatalf("ProcessIdentity(%d): %v", pid, err)
	}
	identity, err := parseLinuxProcessIdentity(identityValue)
	if err != nil {
		t.Fatalf("parse process identity %q: %v", identityValue, err)
	}
	anchor, err := captureLinuxProcessGroupAnchor(pid)
	if err != nil {
		t.Fatalf("capture process-group anchor for %d: %v", pid, err)
	}
	if identity.version != linuxProcessIdentityVersion || identity.pid != pid || identity.pidNamespaceInode != namespaceInode || identity.startTime != anchor.startTime {
		t.Fatalf("process identity %q and anchor %#v do not match expected PID %d and namespace inode %d", identityValue, anchor, pid, namespaceInode)
	}
	return identityValue, identity, anchor
}

func waitForLinuxPIDReuseNamespaceCommand(t *testing.T, command *exec.Cmd, pidfd int, output *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			killErr := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			waitErr := command.Wait()
			t.Fatalf("private namespace PID-reuse witness exceeded 30 seconds; terminated unshare via pidfd (signal error: %v, wait error: %v); output:\n%s", killErr, waitErr, output.String())
		}
		pollMillis := int((remaining + time.Millisecond - 1) / time.Millisecond)
		if pollMillis > 100 {
			pollMillis = 100
		}
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		count, err := unix.Poll(fds, pollMillis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			killErr := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			waitErr := command.Wait()
			t.Fatalf("poll unshare pidfd: %v (termination signal error: %v, wait error: %v); output:\n%s", err, killErr, waitErr, output.String())
		}
		if count > 0 {
			break
		}
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("required private user+PID namespace prerequisite unavailable or PID-reuse witness failed: unshare --user --map-root-user --pid --fork --mount-proc could not complete: %v; output:\n%s", err, output.String())
	}
}
