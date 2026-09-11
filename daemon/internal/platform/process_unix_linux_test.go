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

func TestProcessGroupCloseStopsOwnedChildAfterLeaderExit(t *testing.T) {
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

	containment, identity, err := AttachProcess(command.Process)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("AttachProcess() error = %v", err)
	}
	if identity == "" {
		t.Fatal("AttachProcess() returned an empty identity")
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = containment.Close()
		}
		_ = command.Wait()
	})

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
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("owned child %d was not running before leader exit: %v", childPID, err)
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
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("owned child %d was not running after leader exit: %v", childPID, err)
	}

	if err := containment.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	closed = true
	if err := syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child %d still exists after Close(): %v", childPID, err)
	}
}

func TestWaitForProcessGroupExitFailsClosed(t *testing.T) {
	want := syscall.EPERM
	if err := waitForProcessGroupExit(context.Background(), 1234, time.Now(), func() error { return want }); !errors.Is(err, want) {
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
	if err := containment.Terminate(true); err != nil {
		t.Fatalf("partial containment Terminate(true) error = %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("wait for terminated helper error = nil")
	}
	if err := containment.Close(); err != nil {
		t.Fatalf("partial containment Close() error = %v", err)
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
	readProcessIdentity = func(int) (string, error) { return "expected", nil }
	t.Cleanup(func() { readProcessIdentity = previousIdentity })

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

func TestProcessGroupUsesRetainedPIDFDForSignalsAndCloseCache(t *testing.T) {
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

	group := &processGroup{pid: 1234, fd: 47}
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

func TestProcessGroupClosePreservesFailureWithoutResignalling(t *testing.T) {
	want := errors.New("kill failed")
	signals := 0
	releases := 0
	restorePIDFDCalls(t, func(_ int, signal unix.Signal, _ *unix.Siginfo, _ int) error {
		signals++
		if signal != unix.SIGKILL {
			t.Fatalf("signal = %v, want SIGKILL", signal)
		}
		return want
	}, func(int) error {
		releases++
		return nil
	})

	group := &processGroup{pid: 1234, fd: 47}
	first := group.Close()
	second := group.Close()
	soft := group.Terminate(false)
	force := group.Terminate(true)
	if !errors.Is(first, want) || !errors.Is(second, want) || !errors.Is(soft, want) || !errors.Is(force, want) {
		t.Fatalf("terminal errors = (%v, %v, %v, %v), want preserved %v", first, second, soft, force, want)
	}
	if signals != 1 || releases != 1 || group.fd != -1 {
		t.Fatalf("failed Close() calls = signals:%d releases:%d fd:%d, want 1, 1, -1", signals, releases, group.fd)
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

	group := &processGroup{pid: 1234, fd: 47}
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
