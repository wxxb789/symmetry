//go:build windows

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigureProcessDoesNotRequireBreakawayFromInheritedJob(t *testing.T) {
	command := exec.Command("example.exe")
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}

	if command.SysProcAttr != nil {
		t.Fatalf("ConfigureProcess() changed SysProcAttr = %#v", command.SysProcAttr)
	}

	attributes := &syscall.SysProcAttr{CreationFlags: 0x00000200}
	command.SysProcAttr = attributes
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() with attributes error = %v", err)
	}
	if command.SysProcAttr != attributes || command.SysProcAttr.CreationFlags != 0x00000200 {
		t.Fatalf("ConfigureProcess() changed existing SysProcAttr = %#v", command.SysProcAttr)
	}
}

func TestCleanupFailedAttachTerminatesOriginalProcessHandle(t *testing.T) {
	want := errors.New("process termination failed")
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	called := false
	previous := terminateProcessForAttachFailure
	terminateProcessForAttachFailure = func(actual *os.Process) error {
		called = actual == process
		return want
	}
	t.Cleanup(func() { terminateProcessForAttachFailure = previous })

	attachErr := errors.New("assign failed")
	containment, identity, err := cleanupFailedAttach(process, 0, attachErr)
	if containment != nil || identity != "" {
		t.Fatalf("cleanupFailedAttach() = (%#v, %q), want no partial containment", containment, identity)
	}
	if !called {
		t.Fatal("cleanupFailedAttach() did not terminate the original process handle")
	}
	if !errors.Is(err, want) || !errors.Is(err, attachErr) {
		t.Fatalf("cleanupFailedAttach() error = %v, want attach and process errors", err)
	}
}

func TestSoftTerminateIsUnsupportedWithoutJobTermination(t *testing.T) {
	terminated := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminated++
			return nil
		},
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error { return nil },
	)

	err := (&jobContainment{handle: syscall.Handle(1234)}).Terminate(false)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Terminate(false) error = %v, want errors.ErrUnsupported", err)
	}
	if terminated != 0 {
		t.Fatalf("soft Terminate() terminated job %d times, want 0", terminated)
	}
}

func TestAttachProcessReturnsPartialContainmentAfterIdentityFailure(t *testing.T) {
	command := exec.Command("cmd", "/c", "ping", "-t", "127.0.0.1")
	if err := command.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	want := errors.New("identity unavailable")
	previous := readProcessIdentityFromHandle
	readProcessIdentityFromHandle = func(int, syscall.Handle) (string, error) { return "", want }
	t.Cleanup(func() { readProcessIdentityFromHandle = previous })

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

func TestCloseWaitsForObservedEmptyJobBeforeClosingHandle(t *testing.T) {
	var calls []string
	restoreJobCalls(t,
		func(syscall.Handle) error {
			calls = append(calls, "terminate")
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			calls = append(calls, "query")
			return 0, nil
		},
		func(syscall.Handle) error {
			calls = append(calls, "close")
			return nil
		},
	)

	job := &jobContainment{handle: syscall.Handle(1234)}
	if err := job.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "terminate,query,close"; got != want {
		t.Fatalf("Close() calls = %q, want %q", got, want)
	}
	if job.handle != 0 {
		t.Fatalf("job handle = %d after successful Close(), want 0", job.handle)
	}
	if err := job.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	if err := job.Terminate(true); err != nil {
		t.Fatalf("Terminate(true) after Close() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "terminate,query,close"; got != want {
		t.Fatalf("post-Close calls = %q, want %q", got, want)
	}
}

func TestCloseReleasesJobAfterTerminateFailureAndCachesError(t *testing.T) {
	want := errors.New("terminate failed")
	terminated := 0
	closed := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminated++
			return want
		},
		func(syscall.Handle) (uint32, error) {
			t.Fatal("Close() queried a job after termination failed")
			return 0, nil
		},
		func(syscall.Handle) error {
			closed++
			return nil
		},
	)

	job := &jobContainment{handle: syscall.Handle(1234)}
	first := job.Close()
	second := job.Close()
	soft := job.Terminate(false)
	force := job.Terminate(true)
	if !errors.Is(first, want) || !errors.Is(second, want) || !errors.Is(soft, want) || !errors.Is(force, want) {
		t.Fatalf("terminal errors = (%v, %v, %v, %v), want preserved %v", first, second, soft, force, want)
	}
	if terminated != 1 {
		t.Fatalf("terminate calls = %d, want 1", terminated)
	}
	if closed != 1 || job.handle != 0 {
		t.Fatalf("release after failed termination = calls:%d handle:%d, want 1 and 0", closed, job.handle)
	}
}

func TestCloseReleasesJobAfterQueryFailureAndCachesError(t *testing.T) {
	want := errors.New("query failed")
	terminated := 0
	queried := 0
	closed := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminated++
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			queried++
			return 0, want
		},
		func(syscall.Handle) error {
			closed++
			return nil
		},
	)

	job := &jobContainment{handle: syscall.Handle(1234)}
	first := job.Close()
	second := job.Close()
	if !errors.Is(first, want) || !errors.Is(second, want) || !errors.Is(job.Terminate(true), want) {
		t.Fatalf("terminal errors = (%v, %v), want preserved %v", first, second, want)
	}
	if terminated != 1 || queried != 1 || closed != 1 || job.handle != 0 {
		t.Fatalf("failed query calls = terminate:%d query:%d close:%d handle:%d, want 1, 1, 1, 0", terminated, queried, closed, job.handle)
	}
}

func TestCloseReleasesJobAfterDeadlineFailureAndCachesError(t *testing.T) {
	want := errors.New("observation deadline")
	terminated := 0
	closed := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminated++
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			t.Fatal("Close() queried through the real waiter instead of the timeout seam")
			return 0, nil
		},
		func(syscall.Handle) error {
			closed++
			return nil
		},
	)
	restoreJobWait(t, func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
		return want
	})

	job := &jobContainment{handle: syscall.Handle(1234)}
	first := job.Close()
	second := job.Close()
	if !errors.Is(first, want) || !errors.Is(second, want) || !errors.Is(job.Terminate(true), want) {
		t.Fatalf("terminal errors = (%v, %v), want preserved %v", first, second, want)
	}
	if terminated != 1 || closed != 1 || job.handle != 0 {
		t.Fatalf("deadline failure calls = terminate:%d close:%d handle:%d, want 1, 1, 0", terminated, closed, job.handle)
	}
}

func TestCloseRetriesHandleReleaseWithoutReplayingStop(t *testing.T) {
	want := errors.New("close handle failed")
	terminated := 0
	queried := 0
	closed := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminated++
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			queried++
			return 0, nil
		},
		func(syscall.Handle) error {
			closed++
			if closed == 1 {
				return want
			}
			return nil
		},
	)

	job := &jobContainment{handle: syscall.Handle(1234)}
	first := job.Close()
	if !errors.Is(first, want) || job.handle == 0 {
		t.Fatalf("first Close() = %v with handle %d, want release error and retained handle", first, job.handle)
	}
	if err := job.Terminate(true); !errors.Is(err, want) {
		t.Fatalf("Terminate(true) after failed release = %v, want preserved %v", err, want)
	}
	second := job.Close()
	third := job.Close()
	if !errors.Is(second, want) || !errors.Is(third, want) {
		t.Fatalf("later Close() errors = (%v, %v), want preserved %v", second, third, want)
	}
	if terminated != 1 || queried != 1 || closed != 2 || job.handle != 0 {
		t.Fatalf("failed release calls = terminate:%d query:%d close:%d handle:%d, want 1, 1, 2, 0", terminated, queried, closed, job.handle)
	}
}

func TestWaitForJobToEmptyFailsClosed(t *testing.T) {
	want := errors.New("query failed")
	if err := waitForJobToEmpty(1234, time.Now(), func(syscall.Handle) (uint32, error) {
		return 0, want
	}); !errors.Is(err, want) {
		t.Fatalf("query failure = %v, want %v", err, want)
	}

	queries := 0
	err := waitForJobToEmpty(1234, time.Now(), func(syscall.Handle) (uint32, error) {
		queries++
		return 1, nil
	})
	if err == nil || !strings.Contains(err.Error(), "remained non-empty") {
		t.Fatalf("deadline failure = %v, want non-empty containment error", err)
	}
	if queries != 1 {
		t.Fatalf("deadline queries = %d, want 1", queries)
	}
}

func TestTerminateAndCloseSerialize(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls int
	restoreJobCalls(t,
		func(syscall.Handle) error {
			calls++
			if calls == 1 {
				close(entered)
				<-release
			}
			return nil
		},
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error { return nil },
	)

	job := &jobContainment{handle: syscall.Handle(1234)}
	terminateResult := make(chan error, 1)
	go func() { terminateResult <- job.Terminate(true) }()
	<-entered
	closeStarted := make(chan struct{})
	closeResult := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeResult <- job.Close()
	}()
	<-closeStarted
	select {
	case err := <-closeResult:
		t.Fatalf("Close() completed while Terminate() held containment ownership: %v", err)
	default:
	}
	close(release)
	if err := <-terminateResult; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("job termination calls = %d, want 2", calls)
	}
}

func TestCloseStopsOwnedChildAfterLeaderExit(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestJobContainmentHelper$", "--", "leader-exits-after-child")
	command.Env = append(os.Environ(), "GO_WANT_JOB_CONTAINMENT_HELPER=1")
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
	if exists, details := windowsProcessExists(childPID); !exists {
		t.Fatalf("owned child %d was not running before leader exit; tasklist output: %q", childPID, details)
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
	if exists, details := windowsProcessExists(childPID); !exists {
		t.Fatalf("owned child %d was not running after leader exit; tasklist output: %q", childPID, details)
	}

	if err := containment.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	closed = true
	if exists, details := windowsProcessExists(childPID); exists {
		t.Fatalf("owned child %d survived Close(); tasklist output: %q", childPID, details)
	}
}

func restoreJobCalls(t *testing.T, terminate func(syscall.Handle) error, query func(syscall.Handle) (uint32, error), closeHandle func(syscall.Handle) error) {
	t.Helper()
	previousTerminate := terminateJob
	previousQuery := queryJobActiveProcesses
	previousClose := closeJob
	terminateJob = terminate
	queryJobActiveProcesses = query
	closeJob = closeHandle
	t.Cleanup(func() {
		terminateJob = previousTerminate
		queryJobActiveProcesses = previousQuery
		closeJob = previousClose
	})
}

func restoreJobWait(t *testing.T, wait func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error) {
	t.Helper()
	previous := waitForEmptyJob
	waitForEmptyJob = wait
	t.Cleanup(func() { waitForEmptyJob = previous })
}

func TestJobContainmentHelper(t *testing.T) {
	if os.Getenv("GO_WANT_JOB_CONTAINMENT_HELPER") != "1" {
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
		child := exec.Command("cmd", "/c", "ping", "-t", "127.0.0.1")
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

func windowsProcessExists(pid int) (bool, string) {
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, err.Error()
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid)), string(output)
}
