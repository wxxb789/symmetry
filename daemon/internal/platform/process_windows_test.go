//go:build windows

package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestConfigureHeadlessProcessCreatesHeadlessSysProcAttr(t *testing.T) {
	command := exec.Command("example.exe")
	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}

	if command.SysProcAttr == nil {
		t.Fatal("ConfigureHeadlessProcess() left SysProcAttr nil")
	}
	if got, want := command.SysProcAttr.CreationFlags, uint32(windowsCreateNoWindow); got != want {
		t.Fatalf("CreationFlags = %#x, want %#x", got, want)
	}

	attributes := command.SysProcAttr
	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("repeated ConfigureHeadlessProcess() error = %v", err)
	}
	if command.SysProcAttr != attributes || command.SysProcAttr.CreationFlags != windowsCreateNoWindow {
		t.Fatalf("repeated ConfigureHeadlessProcess() changed attributes = %#v", command.SysProcAttr)
	}
}

func TestConfigureHeadlessProcessPreservesExistingSysProcAttr(t *testing.T) {
	attributes := &syscall.SysProcAttr{
		HideWindow:                 true,
		CmdLine:                    `example.exe "argument"`,
		CreationFlags:              0x00000200,
		Token:                      syscall.Token(7),
		NoInheritHandles:           true,
		AdditionalInheritedHandles: []syscall.Handle{11, 13},
		ParentProcess:              syscall.Handle(17),
	}
	before := *attributes
	command := exec.Command("example.exe")
	command.SysProcAttr = attributes

	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() with attributes error = %v", err)
	}
	if command.SysProcAttr != attributes {
		t.Fatalf("ConfigureHeadlessProcess() replaced existing SysProcAttr = %#v", command.SysProcAttr)
	}
	after := *attributes
	after.CreationFlags = before.CreationFlags
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("ConfigureHeadlessProcess() changed fields: before=%#v after=%#v", before, after)
	}
	if got, want := attributes.CreationFlags, before.CreationFlags|uint32(windowsCreateNoWindow); got != want {
		t.Fatalf("CreationFlags = %#x, want %#x", got, want)
	}
}

func TestConfigureHeadlessProcessRejectsConflictingConsoleCreationFlags(t *testing.T) {
	tests := []struct {
		name string
		flag uint32
		text string
	}{
		{name: "new console", flag: windowsCreateNewConsole, text: "CREATE_NEW_CONSOLE"},
		{name: "detached process", flag: windowsDetachedProcess, text: "DETACHED_PROCESS"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attributes := &syscall.SysProcAttr{CreationFlags: test.flag | 0x00000200}
			before := *attributes
			command := exec.Command("example.exe")
			command.SysProcAttr = attributes

			err := ConfigureHeadlessProcess(command)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("ConfigureHeadlessProcess() error = %v, want %s conflict", err, test.text)
			}
			if command.SysProcAttr != attributes || !reflect.DeepEqual(*attributes, before) {
				t.Fatalf("conflicting ConfigureHeadlessProcess() mutated attributes = %#v, want %#v", attributes, before)
			}
		})
	}
}

func TestConfigureHeadlessProcessRejectsNilCommand(t *testing.T) {
	if err := ConfigureHeadlessProcess(nil); err == nil {
		t.Fatal("ConfigureHeadlessProcess(nil) error = nil, want error")
	}
}

func TestConfigureProcessReusesHeadlessConfiguration(t *testing.T) {
	command := exec.Command("example.exe")
	if err := ConfigureProcess(command); err != nil {
		t.Fatalf("ConfigureProcess() error = %v", err)
	}
	if command.SysProcAttr == nil || command.SysProcAttr.CreationFlags&windowsCreateNoWindow == 0 {
		t.Fatalf("ConfigureProcess() SysProcAttr = %#v, want CREATE_NO_WINDOW", command.SysProcAttr)
	}
}

func TestConfigureHeadlessProcessCreatesNoConsoleWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHeadlessProcessHelper$", "--")
	command.Env = append(os.Environ(), "GO_WANT_HEADLESS_PROCESS_HELPER=1")
	if err := ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("headless process error = %v, output = %q", err, output)
	}
	if ctx.Err() != nil {
		t.Fatalf("headless process context = %v", ctx.Err())
	}
	if !strings.Contains(string(output), "console=0") {
		t.Fatalf("headless process output = %q, want console=0", output)
	}
}

func TestHeadlessProcessHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HEADLESS_PROCESS_HELPER") != "1" {
		return
	}

	getConsoleWindow := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	consoleWindow, _, _ := getConsoleWindow.Call()
	_, _ = fmt.Fprintf(os.Stdout, "console=%d\n", consoleWindow)
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

func TestPartialJobCloseRetriesHelperAfterBaseSuccess(t *testing.T) {
	wantHelper := errors.New("helper termination still pending")
	attempts := 0
	closed := 0
	previousKill := killContainmentSupervisor
	killContainmentSupervisor = func(*supervisorProcessWait, time.Time) error {
		attempts++
		if attempts == 1 {
			return wantHelper
		}
		return nil
	}
	t.Cleanup(func() { killContainmentSupervisor = previousKill })
	restoreJobCalls(t,
		func(syscall.Handle) error { return nil },
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error {
			closed++
			return nil
		},
	)

	partial := newPartialContainmentSupervisorLease(&processContainmentSupervisorLease{
		waiter: &supervisorProcessWait{process: &os.Process{}},
	})
	job := &jobContainment{handle: syscall.Handle(1234), supervisor: partial}
	first := job.Close()
	if !errors.Is(first, wantHelper) {
		t.Fatalf("first Close() = %v, want helper failure %v", first, wantHelper)
	}
	if closed != 1 || job.handle != 0 {
		t.Fatalf("first partial close = close:%d handle:%d, want 1 and 0", closed, job.handle)
	}
	second := job.Close()
	if second != nil {
		t.Fatalf("second Close() = %v, want helper retry success", second)
	}
	third := job.Close()
	if third != nil {
		t.Fatalf("third Close() = %v, want idempotent success", third)
	}
	if attempts != 2 || closed != 1 {
		t.Fatalf("partial close retries = helper:%d close:%d, want 2 and 1", attempts, closed)
	}
}

func TestPartialJobCloseRetriesHandleReleaseAfterHelperSuccess(t *testing.T) {
	wantRelease := errors.New("job handle release failed")
	helperCalls := 0
	previousKill := killContainmentSupervisor
	killContainmentSupervisor = func(*supervisorProcessWait, time.Time) error {
		helperCalls++
		return nil
	}
	t.Cleanup(func() { killContainmentSupervisor = previousKill })
	closed := 0
	restoreJobCalls(t,
		func(syscall.Handle) error { return nil },
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error {
			closed++
			if closed == 1 {
				return wantRelease
			}
			return nil
		},
	)

	partial := newPartialContainmentSupervisorLease(&processContainmentSupervisorLease{
		waiter: &supervisorProcessWait{process: &os.Process{}},
	})
	job := &jobContainment{handle: syscall.Handle(1234), supervisor: partial}
	first := job.Close()
	if !errors.Is(first, wantRelease) || job.handle == 0 {
		t.Fatalf("first partial Close() = %v handle:%d, want release error and retained handle", first, job.handle)
	}
	if !job.ContainmentCloseRetryable() {
		t.Fatal("partial Close() did not retain retry capability for the daemon Job handle")
	}
	second := job.Close()
	if !errors.Is(second, wantRelease) || job.handle != 0 {
		t.Fatalf("second partial Close() = %v handle:%d, want retained error and released handle", second, job.handle)
	}
	if job.ContainmentCloseRetryable() {
		t.Fatal("partial Close() retained retry capability after handle release")
	}
	third := job.Close()
	if !errors.Is(third, wantRelease) {
		t.Fatalf("third partial Close() = %v, want retained release error", third)
	}
	if helperCalls != 1 || closed != 2 {
		t.Fatalf("partial handle retry calls = helper:%d close:%d, want 1 and 2", helperCalls, closed)
	}
}

func TestFullSupervisorLeaseUsesDurableStopReceiptAndReleasePath(t *testing.T) {
	endpoint := containmentSupervisorEndpoint{
		TargetPID:          71,
		TargetIdentity:     "windows:71:0000000000000001",
		SupervisorPID:      72,
		SupervisorIdentity: "windows:72:0000000000000002",
		Token:              strings.Repeat("a", authority.TokenBytes*2),
		JobID:              strings.Repeat("b", authority.TokenBytes*2),
		Secret:             strings.Repeat("c", authority.SecretBytes*2),
	}
	lease := &processContainmentSupervisorLease{
		endpoint: endpoint,
		waiter:   &supervisorProcessWait{process: &os.Process{}},
	}
	if _, ok := containmentSupervisorLease(lease).(containmentSupervisorPartialLease); ok {
		t.Fatal("full durable supervisor lease was classified as partial")
	}

	closeRequests := 0
	releaseRequests := 0
	previousRequest := requestContainmentSupervisor
	requestContainmentSupervisor = func(actual containmentSupervisorEndpoint, operation string, _ time.Time) (containmentSupervisorResponse, error) {
		if actual != endpoint {
			t.Fatalf("supervisor endpoint = %#v, want %#v", actual, endpoint)
		}
		status := "stopped"
		if operation == "close" {
			closeRequests++
		} else if operation == "release" {
			releaseRequests++
			status = "released"
		} else {
			t.Fatalf("unexpected supervisor operation %q", operation)
		}
		return containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          operation,
			Status:             status,
			TargetPID:          actual.TargetPID,
			TargetIdentity:     actual.TargetIdentity,
			SupervisorPID:      actual.SupervisorPID,
			SupervisorIdentity: actual.SupervisorIdentity,
			Token:              actual.Token,
			JobID:              actual.JobID,
			ActiveProcesses:    0,
		}, nil
	}
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })
	restoreJobCalls(t,
		func(syscall.Handle) error { return nil },
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error { return nil },
	)

	job := &jobContainment{handle: syscall.Handle(1234), supervisor: lease}
	if err := job.Close(); err != nil {
		t.Fatalf("full durable supervisor Close() error = %v", err)
	}
	if closeRequests != 1 || job.stopReceipt == nil {
		t.Fatalf("durable close = requests:%d receipt:%#v, want one close and receipt", closeRequests, job.stopReceipt)
	}
	if releaseRequests != 0 {
		t.Fatalf("durable close released helper before receipt acknowledgement: %d", releaseRequests)
	}
	if err := job.ReleaseContainment(); err != nil {
		t.Fatalf("ReleaseContainment() error = %v", err)
	}
	if releaseRequests != 1 || job.supervisor != nil {
		t.Fatalf("durable release = requests:%d supervisor:%#v, want one and nil", releaseRequests, job.supervisor)
	}
}

func TestFullSupervisorLeaseRetriesHandleReleaseWithoutReplayingStop(t *testing.T) {
	wantRelease := errors.New("job handle release failed")
	endpoint := containmentSupervisorEndpoint{
		TargetPID:          71,
		TargetIdentity:     "windows:71:0000000000000001",
		SupervisorPID:      72,
		SupervisorIdentity: "windows:72:0000000000000002",
		Token:              strings.Repeat("a", authority.TokenBytes*2),
		JobID:              strings.Repeat("b", authority.TokenBytes*2),
		Secret:             strings.Repeat("c", authority.SecretBytes*2),
	}
	lease := &processContainmentSupervisorLease{
		endpoint: endpoint,
		waiter:   &supervisorProcessWait{process: &os.Process{}},
	}
	stopRequests := 0
	releaseRequests := 0
	previousRequest := requestContainmentSupervisor
	requestContainmentSupervisor = func(actual containmentSupervisorEndpoint, operation string, _ time.Time) (containmentSupervisorResponse, error) {
		if actual != endpoint {
			t.Fatalf("supervisor endpoint = %#v, want %#v", actual, endpoint)
		}
		status := "stopped"
		switch operation {
		case "close":
			stopRequests++
		case "release":
			releaseRequests++
			status = "released"
		default:
			t.Fatalf("unexpected supervisor operation %q", operation)
		}
		return containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          operation,
			Status:             status,
			TargetPID:          actual.TargetPID,
			TargetIdentity:     actual.TargetIdentity,
			SupervisorPID:      actual.SupervisorPID,
			SupervisorIdentity: actual.SupervisorIdentity,
			Token:              actual.Token,
			JobID:              actual.JobID,
			ActiveProcesses:    0,
		}, nil
	}
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })

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
				return wantRelease
			}
			return nil
		},
	)

	job := &jobContainment{handle: syscall.Handle(1234), supervisor: lease}
	first := job.Close()
	if !errors.Is(first, wantRelease) {
		t.Fatalf("first Close() = %v, want handle release error %v", first, wantRelease)
	}
	if stopRequests != 1 || releaseRequests != 0 || terminated != 1 || queried != 1 || closed != 1 {
		t.Fatalf("first durable close = stop:%d release:%d terminate:%d query:%d handleClose:%d, want 1, 0, 1, 1, 1", stopRequests, releaseRequests, terminated, queried, closed)
	}
	if job.stopReceipt == nil || job.handle == 0 || !job.ContainmentCloseRetryable() {
		t.Fatalf("first durable close state = receipt:%#v handle:%d retryable:%t, want receipt, retained handle, retryable", job.stopReceipt, job.handle, job.ContainmentCloseRetryable())
	}

	second := job.Close()
	if second != nil {
		t.Fatalf("second Close() = %v, want local handle release success", second)
	}
	if stopRequests != 1 || releaseRequests != 0 || terminated != 1 || queried != 1 || closed != 2 || job.handle != 0 {
		t.Fatalf("second durable close = stop:%d release:%d terminate:%d query:%d handleClose:%d handle:%d, want 1, 0, 1, 1, 2, 0", stopRequests, releaseRequests, terminated, queried, closed, job.handle)
	}
	if job.ContainmentCloseRetryable() {
		t.Fatal("successful local handle release retained retry capability")
	}

	third := job.Close()
	if third != nil {
		t.Fatalf("third Close() = %v, want idempotent success", third)
	}
	if stopRequests != 1 || releaseRequests != 0 || terminated != 1 || queried != 1 || closed != 2 {
		t.Fatalf("third durable close repeated physical work = stop:%d release:%d terminate:%d query:%d handleClose:%d, want 1, 0, 1, 1, 2", stopRequests, releaseRequests, terminated, queried, closed)
	}
}

func TestFullSupervisorLeaseRetainsBaseContainmentErrorsWithoutHandleRetry(t *testing.T) {
	terminationErr := errors.New("job termination failed")
	waitErr := errors.New("job observation failed")
	for _, test := range []struct {
		name      string
		terminate func(syscall.Handle) error
		query     func(syscall.Handle) (uint32, error)
		wait      error
		want      error
	}{
		{
			name: "termination",
			terminate: func(syscall.Handle) error {
				return terminationErr
			},
			query: func(syscall.Handle) (uint32, error) {
				t.Fatal("query ran after Job termination failure")
				return 0, nil
			},
			want: terminationErr,
		},
		{
			name:      "wait",
			terminate: func(syscall.Handle) error { return nil },
			query: func(syscall.Handle) (uint32, error) {
				t.Fatal("query ran through the injected wait failure")
				return 0, nil
			},
			wait: waitErr,
			want: waitErr,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := containmentSupervisorEndpoint{
				TargetPID:          71,
				TargetIdentity:     "windows:71:0000000000000001",
				SupervisorPID:      72,
				SupervisorIdentity: "windows:72:0000000000000002",
				Token:              strings.Repeat("a", authority.TokenBytes*2),
				JobID:              strings.Repeat("b", authority.TokenBytes*2),
				Secret:             strings.Repeat("c", authority.SecretBytes*2),
			}
			lease := &processContainmentSupervisorLease{
				endpoint: endpoint,
				waiter:   &supervisorProcessWait{process: &os.Process{}},
			}
			stopRequests := 0
			previousRequest := requestContainmentSupervisor
			requestContainmentSupervisor = func(actual containmentSupervisorEndpoint, operation string, _ time.Time) (containmentSupervisorResponse, error) {
				if operation != "close" {
					t.Fatalf("unexpected supervisor operation %q", operation)
				}
				stopRequests++
				return containmentSupervisorResponse{
					Version:            containmentSupervisorProtocol,
					Operation:          operation,
					Status:             "stopped",
					TargetPID:          actual.TargetPID,
					TargetIdentity:     actual.TargetIdentity,
					SupervisorPID:      actual.SupervisorPID,
					SupervisorIdentity: actual.SupervisorIdentity,
					Token:              actual.Token,
					JobID:              actual.JobID,
					ActiveProcesses:    0,
				}, nil
			}
			t.Cleanup(func() { requestContainmentSupervisor = previousRequest })

			closed := 0
			restoreJobCalls(t, test.terminate, test.query, func(syscall.Handle) error {
				closed++
				return nil
			})
			if test.wait != nil {
				restoreJobWait(t, func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
					return test.wait
				})
			}

			job := &jobContainment{handle: syscall.Handle(1234), supervisor: lease}
			first := job.Close()
			if !errors.Is(first, test.want) {
				t.Fatalf("first Close() = %v, want base error %v", first, test.want)
			}
			if job.stopReceipt == nil || job.ContainmentCloseRetryable() {
				t.Fatalf("base-error state = receipt:%#v retryable:%t, want receipt and no retry", job.stopReceipt, job.ContainmentCloseRetryable())
			}
			second := job.Close()
			if !errors.Is(second, test.want) {
				t.Fatalf("second Close() = %v, want retained base error %v", second, test.want)
			}
			if stopRequests != 1 || closed != 0 || job.handle == 0 {
				t.Fatalf("base-error retry state = stop:%d handleClose:%d handle:%d, want 1, 0, retained handle", stopRequests, closed, job.handle)
			}
		})
	}
}

func TestPartialJobCloseRetainsBaseErrorAcrossHelperRetry(t *testing.T) {
	terminationErr := errors.New("job termination failed")
	waitErr := errors.New("job observation failed")
	releaseErr := errors.New("job handle release failed")
	tests := []struct {
		name      string
		terminate func(syscall.Handle) error
		query     func(syscall.Handle) (uint32, error)
		base      error
		wantClose int
	}{
		{
			name: "termination",
			terminate: func(syscall.Handle) error {
				return terminationErr
			},
			query: func(syscall.Handle) (uint32, error) {
				t.Fatal("query ran after Job termination failure")
				return 0, nil
			},
			base: terminationErr,
		},
		{
			name:      "wait",
			terminate: func(syscall.Handle) error { return nil },
			query: func(syscall.Handle) (uint32, error) {
				return 0, waitErr
			},
			base: waitErr,
		},
		{
			name:      "handle release",
			terminate: func(syscall.Handle) error { return nil },
			query:     func(syscall.Handle) (uint32, error) { return 0, nil },
			base:      releaseErr,
			wantClose: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantHelper := errors.New("helper termination still pending")
			attempts := 0
			previousKill := killContainmentSupervisor
			killContainmentSupervisor = func(*supervisorProcessWait, time.Time) error {
				attempts++
				if attempts == 1 {
					return wantHelper
				}
				return nil
			}
			t.Cleanup(func() { killContainmentSupervisor = previousKill })

			closed := 0
			baseErr := test.base
			restoreJobCalls(t,
				test.terminate,
				test.query,
				func(handle syscall.Handle) error {
					closed++
					if test.name == "handle release" && closed == 1 {
						return baseErr
					}
					return nil
				},
			)

			partial := newPartialContainmentSupervisorLease(&processContainmentSupervisorLease{
				waiter: &supervisorProcessWait{process: &os.Process{}},
			})
			job := &jobContainment{handle: syscall.Handle(1234), supervisor: partial}
			first := job.Close()
			if !errors.Is(first, baseErr) || !errors.Is(first, wantHelper) {
				t.Fatalf("first Close() = %v, want base %v and helper %v", first, baseErr, wantHelper)
			}
			second := job.Close()
			if !errors.Is(second, baseErr) || errors.Is(second, wantHelper) {
				t.Fatalf("second Close() = %v, want retained base only %v", second, baseErr)
			}
			third := job.Close()
			if !errors.Is(third, baseErr) || errors.Is(third, wantHelper) {
				t.Fatalf("third Close() = %v, want idempotent retained base %v", third, baseErr)
			}
			if attempts != 2 {
				t.Fatalf("helper cleanup attempts = %d, want 2", attempts)
			}
			wantClose := test.wantClose
			if wantClose == 0 {
				wantClose = 1
			}
			if closed != wantClose {
				t.Fatalf("Job handle close calls = %d, want %d", closed, wantClose)
			}
		})
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
	installContainmentSupervisorTestLauncher(t)
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
		if err := ConfigureProcess(child); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(4)
		}
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

func TestJobContainmentReleaseSynchronizesRenewalAndAuthorityAccess(t *testing.T) {
	supervisor := &blockingContainmentSupervisor{
		releaseEntered: make(chan struct{}),
		allowRelease:   make(chan struct{}),
	}
	job := &jobContainment{
		supervisor:  supervisor,
		stopReceipt: &authority.StopReceipt{Status: "stopped"},
	}

	if job.ContainmentAuthority() == nil || !job.ContainmentAuthorityAvailable() || !job.LeaseRenewalAvailable() {
		t.Fatal("supervisor-backed containment did not expose authority and renewal capabilities before release")
	}

	const readers = 2
	start := make(chan struct{})
	stop := make(chan struct{})
	ready := make(chan struct{}, readers)
	done := make(chan struct{}, readers)
	readAccessPaths := func() {
		_ = job.ContainmentAuthority()
		_ = job.ContainmentAuthorityAvailable()
		_ = job.LeaseRenewalAvailable()
	}
	for range readers {
		go func() {
			<-start
			readAccessPaths()
			ready <- struct{}{}
			for {
				select {
				case <-stop:
					done <- struct{}{}
					return
				default:
					readAccessPaths()
				}
			}
		}()
	}
	close(start)
	for range readers {
		<-ready
	}

	releaseResult := make(chan error, 1)
	go func() { releaseResult <- job.ReleaseContainment() }()
	<-supervisor.releaseEntered
	close(supervisor.allowRelease)
	if err := <-releaseResult; err != nil {
		t.Fatalf("ReleaseContainment() error = %v", err)
	}
	close(stop)
	for range readers {
		<-done
	}

	if job.ContainmentAuthority() != nil {
		t.Fatal("ContainmentAuthority() returned authority after release")
	}
	if job.ContainmentAuthorityAvailable() {
		t.Fatal("ContainmentAuthorityAvailable() = true after release")
	}
	if job.LeaseRenewalAvailable() {
		t.Fatal("LeaseRenewalAvailable() = true after release")
	}
	if err := job.RenewLease(time.Second, 1); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("RenewLease() after release error = %v, want errors.ErrUnsupported", err)
	}
	if err := job.ReleaseContainment(); err != nil {
		t.Fatalf("repeated ReleaseContainment() error = %v, want nil", err)
	}
}

type blockingContainmentSupervisor struct {
	releaseEntered chan struct{}
	allowRelease   chan struct{}
}

func (*blockingContainmentSupervisor) close(time.Time) error {
	return nil
}

func (*blockingContainmentSupervisor) renew(time.Duration, uint64) error {
	return nil
}

func (*blockingContainmentSupervisor) stop(time.Time) (authority.StopReceipt, error) {
	return authority.StopReceipt{Status: "stopped"}, nil
}

func (supervisor *blockingContainmentSupervisor) release(time.Time) error {
	close(supervisor.releaseEntered)
	<-supervisor.allowRelease
	return nil
}

func (*blockingContainmentSupervisor) containmentAuthority() *authority.Supervisor {
	return &authority.Supervisor{Version: authority.SupervisorVersion}
}

func (*blockingContainmentSupervisor) LeaseRenewalAvailable() bool {
	return true
}

func (*blockingContainmentSupervisor) RenewLease(time.Duration, uint64) error {
	return nil
}

func windowsProcessExists(pid int) (bool, string) {
	command := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	if err := ConfigureProcess(command); err != nil {
		return false, err.Error()
	}
	output, err := command.Output()
	if err != nil {
		return false, err.Error()
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid)), string(output)
}
