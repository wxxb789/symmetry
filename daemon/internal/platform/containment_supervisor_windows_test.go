//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

type fakeContainmentSupervisorLease struct {
	calls int
	err   error
}

func (lease *fakeContainmentSupervisorLease) close(time.Time) error {
	lease.calls++
	return lease.err
}

func (lease *fakeContainmentSupervisorLease) renew(time.Duration, uint64) error {
	return lease.err
}

func restoreContainmentSupervisorLauncher(t *testing.T, launcher func(syscall.Handle, int, string) (containmentSupervisorLease, string, error)) {
	t.Helper()
	previous := launchContainmentSupervisor
	launchContainmentSupervisor = launcher
	t.Cleanup(func() { launchContainmentSupervisor = previous })
}

func TestJobContainmentCloseReleasesIndependentSupervisor(t *testing.T) {
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
	supervisor := &fakeContainmentSupervisorLease{}
	job := &jobContainment{handle: syscall.Handle(1234), supervisor: supervisor}

	if err := job.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if supervisor.calls != 1 {
		t.Fatalf("supervisor close calls = %d, want 1", supervisor.calls)
	}
	if got, want := strings.Join(calls, ","), "terminate,query,close"; got != want {
		t.Fatalf("Job calls = %q, want %q", got, want)
	}
	if job.handle != 0 {
		t.Fatalf("Job handle = %d after Close(), want 0", job.handle)
	}
	if err := job.Close(); err != nil {
		t.Fatalf("repeated Close() error = %v", err)
	}
	if supervisor.calls != 1 {
		t.Fatalf("repeated supervisor close calls = %d, want 1", supervisor.calls)
	}
}

func TestPartialContainmentSupervisorLeaseDoesNotExposeDurableAuthority(t *testing.T) {
	partial := newPartialContainmentSupervisorLease(&processContainmentSupervisorLease{})
	if partial == nil {
		t.Fatal("newPartialContainmentSupervisorLease() returned nil")
	}
	if _, ok := partial.(containmentSupervisorAuthorityProvider); ok {
		t.Fatal("partial containment supervisor lease exposed durable authority")
	}
	if _, ok := partial.(containmentSupervisorStopLease); ok {
		t.Fatal("partial containment supervisor lease exposed stop/release authority")
	}
	if _, ok := partial.(ContainmentLeaseRenewer); ok {
		t.Fatal("partial containment supervisor lease exposed lease renewal capability")
	}

	job := &jobContainment{supervisor: partial}
	if job.ContainmentAuthority() != nil {
		t.Fatal("partial containment returned durable authority")
	}
	if job.ContainmentAuthorityAvailable() {
		t.Fatal("partial containment reported durable authority capability")
	}
}

func TestPartialContainmentCloseReleasesDaemonJobAfterSupervisorFailure(t *testing.T) {
	want := errors.New("helper termination failed")
	previousKill := killContainmentSupervisor
	killContainmentSupervisor = func(*supervisorProcessWait, time.Time) error { return want }
	t.Cleanup(func() { killContainmentSupervisor = previousKill })

	closed := 0
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
	if err := job.Close(); !errors.Is(err, want) {
		t.Fatalf("partial containment Close() error = %v, want %v", err, want)
	}
	if closed != 1 || job.handle != 0 {
		t.Fatalf("partial containment release = close:%d handle:%d, want 1 and 0", closed, job.handle)
	}
}

func TestPartialContainmentSupervisorCloseRetriesHelperCleanup(t *testing.T) {
	firstErr := errors.New("helper termination still pending")
	attempts := 0
	previousKill := killContainmentSupervisor
	killContainmentSupervisor = func(*supervisorProcessWait, time.Time) error {
		attempts++
		if attempts == 1 {
			return firstErr
		}
		return nil
	}
	t.Cleanup(func() { killContainmentSupervisor = previousKill })

	partial := newPartialContainmentSupervisorLease(&processContainmentSupervisorLease{
		waiter: &supervisorProcessWait{process: &os.Process{}},
	})
	if err := partial.close(time.Now().Add(time.Second)); !errors.Is(err, firstErr) {
		t.Fatalf("first partial cleanup error = %v, want %v", err, firstErr)
	}
	if err := partial.close(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("retry partial cleanup error = %v", err)
	}
	if err := partial.close(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("repeated partial cleanup error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("partial helper cleanup attempts = %d, want 2", attempts)
	}
}

func TestLaunchContainmentSupervisorPostStartFailuresReturnNonDurablePartialLeases(t *testing.T) {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		t.Fatal("ComSpec is unavailable")
	}
	previousArg0 := os.Args[0]
	previousStart := startContainmentSupervisor
	previousSetHandleInformation := setContainmentHandleInformation
	previousIdentity := readSupervisorProcessIdentity
	previousSecret := generateContainmentSupervisorSecret
	previousBootstrap := writeContainmentSupervisorBootstrap
	t.Cleanup(func() {
		os.Args[0] = previousArg0
		startContainmentSupervisor = previousStart
		setContainmentHandleInformation = previousSetHandleInformation
		readSupervisorProcessIdentity = previousIdentity
		generateContainmentSupervisorSecret = previousSecret
		writeContainmentSupervisorBootstrap = previousBootstrap
	})

	os.Args[0] = comspec
	startContainmentSupervisor = func(command *exec.Cmd) error {
		command.Path = comspec
		command.Args = []string{comspec, "/c", "timeout /t 30 /nobreak >nul"}
		return command.Start()
	}

	targetIdentity := fmt.Sprintf("windows:%d:%016x", os.Getpid(), uint64(1))
	wantFailure := errors.New("injected post-start supervisor setup failure")
	tests := []struct {
		name  string
		setup func()
	}{
		{
			name: "clear job inheritance",
			setup: func() {
				calls := 0
				setContainmentHandleInformation = func(handle syscall.Handle, mask, flags uint32) error {
					calls++
					if calls == 5 {
						return wantFailure
					}
					return syscall.SetHandleInformation(handle, mask, flags)
				}
			},
		},
		{
			name: "capture supervisor identity",
			setup: func() {
				readSupervisorProcessIdentity = func(int) (string, error) {
					return "", wantFailure
				}
			},
		},
		{
			name: "generate supervisor secret",
			setup: func() {
				generateContainmentSupervisorSecret = func() (string, error) {
					return "", wantFailure
				}
			},
		},
		{
			name: "send bootstrap",
			setup: func() {
				writeContainmentSupervisorBootstrap = func(*os.File, containmentSupervisorBootstrap, time.Time) error {
					return wantFailure
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			jobValue, _, callErr := createJobObject.Call(0, 0)
			if jobValue == 0 {
				t.Fatalf("CreateJobObjectW() = (%d, %v)", jobValue, callErr)
			}
			job := syscall.Handle(jobValue)
			t.Cleanup(func() { _ = syscall.CloseHandle(job) })

			setContainmentHandleInformation = previousSetHandleInformation
			readSupervisorProcessIdentity = previousIdentity
			generateContainmentSupervisorSecret = previousSecret
			writeContainmentSupervisorBootstrap = previousBootstrap
			test.setup()

			lease, identity, err := launchContainmentSupervisorProcess(job, os.Getpid(), targetIdentity)
			if err == nil || !errors.Is(err, wantFailure) {
				t.Fatalf("launchContainmentSupervisorProcess() error = %v, want injected failure", err)
			}
			if identity != targetIdentity {
				t.Fatalf("launchContainmentSupervisorProcess() identity = %q, want %q", identity, targetIdentity)
			}
			if lease == nil {
				t.Fatal("launchContainmentSupervisorProcess() returned nil partial lease")
			}
			if _, ok := lease.(containmentSupervisorAuthorityProvider); ok {
				t.Fatal("post-start partial lease exposed durable authority")
			}
			if _, ok := lease.(containmentSupervisorStopLease); ok {
				t.Fatal("post-start partial lease exposed stop/release authority")
			}
			if _, ok := lease.(ContainmentLeaseRenewer); ok {
				t.Fatal("post-start partial lease exposed lease renewal capability")
			}
			if err := lease.close(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatalf("partial helper cleanup error = %v", err)
			}
		})
	}
}

func TestRecoverPersistedContainmentAfterDaemonOwnerLoss(t *testing.T) {
	const targetPID = 71
	const targetIdentity = "windows:71:0000000000000001"
	persisted := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		TargetPID:          targetPID,
		TargetIdentity:     targetIdentity,
		PipeToken:          strings.Repeat("b", authority.TokenBytes*2),
		JobID:              strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID:      72,
		SupervisorIdentity: "windows:72:0000000000000002",
	}
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() {
		requestContainmentSupervisor = previousRequest
	})
	var gotEndpoint containmentSupervisorEndpoint
	requestContainmentSupervisor = func(actual containmentSupervisorEndpoint, operation string, _ time.Time) (containmentSupervisorResponse, error) {
		gotEndpoint = actual
		if operation != "recover" {
			t.Fatalf("supervisor operation = %q, want recover", operation)
		}
		return containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          "recover",
			Status:             "stopped",
			TargetPID:          actual.TargetPID,
			TargetIdentity:     actual.TargetIdentity,
			SupervisorPID:      72,
			SupervisorIdentity: "windows:72:0000000000000002",
			Token:              actual.Token,
			JobID:              actual.JobID,
			ActiveProcesses:    0,
		}, nil
	}

	receipt, err := RecoverPersistedContainmentWithAuthority(targetPID, targetIdentity, &persisted)
	if err != nil {
		t.Fatalf("RecoverPersistedContainmentWithAuthority() error = %v", err)
	}
	if gotEndpoint.TargetPID != targetPID || gotEndpoint.TargetIdentity != targetIdentity || gotEndpoint.JobID != persisted.JobID || gotEndpoint.SupervisorPID != persisted.SupervisorPID || gotEndpoint.Secret != persisted.Secret {
		t.Fatalf("recovery endpoint = %#v, want persisted authority binding", gotEndpoint)
	}
	if !receipt.ValidFor(persisted) {
		t.Fatalf("stop receipt = %#v, want exact persisted authority witness", receipt)
	}
}

func TestRecoverPersistedContainmentRejectsLegacyPlainIdentity(t *testing.T) {
	if err := RecoverPersistedContainment(71, "windows:71:0000000000000001"); !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("legacy recovery error = %v, want unproven stop", err)
	}
}

func TestRecoverPersistedContainmentRejectsIdentityMismatch(t *testing.T) {
	const targetPID = 71
	const targetIdentity = "windows:71:0000000000000001"
	previousIdentity := readSupervisorProcessIdentity
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() {
		readSupervisorProcessIdentity = previousIdentity
		requestContainmentSupervisor = previousRequest
	})
	requestCalled := false
	requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
		requestCalled = true
		return containmentSupervisorResponse{}, nil
	}
	readSupervisorProcessIdentity = func(pid int) (string, error) {
		if pid == targetPID {
			return "windows:71:0000000000000002", nil
		}
		return "", fmt.Errorf("unexpected identity PID %d", pid)
	}

	err := RecoverPersistedContainment(targetPID, targetIdentity)
	if !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("identity mismatch error = %v, want unproven stop", err)
	}
	if requestCalled {
		t.Fatal("identity mismatch reached supervisor request")
	}
}

func TestRecoverPersistedContainmentRejectsMissingOrTimedOutSupervisor(t *testing.T) {
	const targetPID = 71
	const targetIdentity = "windows:71:0000000000000001"
	previousIdentity := readSupervisorProcessIdentity
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() {
		readSupervisorProcessIdentity = previousIdentity
		requestContainmentSupervisor = previousRequest
	})
	readSupervisorProcessIdentity = func(pid int) (string, error) {
		if pid == targetPID {
			return targetIdentity, nil
		}
		return "", fmt.Errorf("unexpected identity PID %d", pid)
	}
	for _, test := range []struct {
		name string
		err  error
	}{{name: "missing", err: errors.New("supervisor missing")}, {name: "timeout", err: context.DeadlineExceeded}} {
		t.Run(test.name, func(t *testing.T) {
			requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
				return containmentSupervisorResponse{}, test.err
			}
			got := RecoverPersistedContainment(targetPID, targetIdentity)
			if !errors.Is(got, errContainmentStopUnproven) {
				t.Fatalf("recovery error = %v, want unproven stop", got)
			}
		})
	}
}

func TestContainmentSupervisorProtocolRejectsUnknownInput(t *testing.T) {
	var request containmentSupervisorRequest
	if err := decodeSupervisorJSON([]byte(`{"version":1,"operation":"hello","target_pid":71,"target_identity":"x","token":"00000000000000000000000000000000","job_id":"11111111111111111111111111111111","unknown":true}`), &request); err == nil {
		t.Fatal("decodeSupervisorJSON accepted unknown protocol field")
	}
	endpoint, err := containmentEndpointForIdentity(71, "windows:71:0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSupervisorRequest(endpoint, containmentSupervisorRequest{
		Version:        containmentSupervisorProtocol,
		Operation:      "unknown",
		TargetPID:      endpoint.TargetPID,
		TargetIdentity: endpoint.TargetIdentity,
		Token:          endpoint.Token,
		JobID:          endpoint.JobID,
	}); err == nil {
		t.Fatal("validateSupervisorRequest accepted unknown operation")
	}
}

func TestParseContainmentSupervisorArgsRequiresInheritedOwnerHandle(t *testing.T) {
	parsed, err := parseContainmentSupervisorArgs([]string{
		"-bootstrap-handle", "12",
		"-owner-handle", "13",
	})
	if err != nil {
		t.Fatalf("parseContainmentSupervisorArgs() error = %v", err)
	}
	if parsed.bootstrapHandle != syscall.Handle(12) || parsed.ownerHandle != syscall.Handle(13) {
		t.Fatalf("parsed handles = (%d, %d), want (12, 13)", parsed.bootstrapHandle, parsed.ownerHandle)
	}
	if _, err := parseContainmentSupervisorArgs([]string{"-bootstrap-handle", "12"}); err == nil || !strings.Contains(err.Error(), "owner handle") {
		t.Fatalf("missing owner handle error = %v, want owner-handle validation", err)
	}
}

func TestParseDurableContainmentSupervisorRequiresInheritedBootstrap(t *testing.T) {
	_, err := parseContainmentSupervisorArgs([]string{
		"-durable",
		"-owner-handle", "13",
		"-job-handle", "14",
		"-target-pid", "71",
		"-target-identity", "windows:71:0000000000000001",
		"-pipe-token", strings.Repeat("a", authority.TokenBytes*2),
		"-job-id", strings.Repeat("b", authority.TokenBytes*2),
		"-job-name", containmentJobName(strings.Repeat("b", authority.TokenBytes*2)),
		"-launch-token", strings.Repeat("c", authority.TokenBytes*2),
	})
	if err == nil || !strings.Contains(err.Error(), "bootstrap handle is required") {
		t.Fatalf("missing durable bootstrap error = %v, want inherited bootstrap requirement", err)
	}
}

func TestContainmentSupervisorRejectsWrongSecretBeforeOperation(t *testing.T) {
	endpoint, err := containmentEndpointForIdentity(71, "windows:71:0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	request := containmentSupervisorRequest{
		Version:        containmentSupervisorProtocol,
		Operation:      "hello",
		TargetPID:      endpoint.TargetPID,
		TargetIdentity: endpoint.TargetIdentity,
		Token:          endpoint.Token,
		JobID:          endpoint.JobID,
		Secret:         strings.Repeat("f", authority.SecretBytes*2),
	}
	if request.Secret == endpoint.Secret {
		t.Fatal("test secret unexpectedly matched endpoint secret")
	}
	if err := validateSupervisorRequest(endpoint, request); err == nil || !strings.Contains(err.Error(), "authority secret mismatch") {
		t.Fatalf("wrong-secret validation error = %v, want authority secret mismatch", err)
	}
}

func TestContainmentSupervisorUnauthenticatedRequestDoesNotStopJob(t *testing.T) {
	supervisorPID := os.Getpid()
	supervisorIdentity, err := ProcessIdentity(supervisorPID)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	endpoint, err := containmentEndpointForIdentity(71, "windows:71:0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	endpoint.SupervisorPID = supervisorPID
	endpoint.SupervisorIdentity = supervisorIdentity
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		t.Fatalf("validateSupervisorEndpoint() error = %v", err)
	}

	bootstrap := containmentSupervisorBootstrap{
		Version:            containmentSupervisorProtocol,
		JobHandle:          uint64(syscall.Handle(1234)),
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		PipeToken:          endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}
	stopCalled := make(chan struct{})
	var stopOnce sync.Once
	stopAttempts := 0
	var stopMu sync.Mutex
	releaseCalls := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			stopMu.Lock()
			stopAttempts++
			stopMu.Unlock()
			stopOnce.Do(func() { close(stopCalled) })
			return nil
		},
		func(syscall.Handle) (uint32, error) { return 0, nil },
		func(syscall.Handle) error {
			stopMu.Lock()
			releaseCalls++
			stopMu.Unlock()
			return nil
		},
	)
	restoreJobWait(t, func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error { return nil })

	runDone := make(chan error, 1)
	go func() { runDone <- runContainmentSupervisor(containmentSupervisorArgs{}, bootstrap) }()
	cleanup := func() {
		deadline := time.Now().Add(5 * time.Second)
		_, _ = requestContainmentSupervisorPipe(endpoint, "close", deadline)
		_, _ = requestContainmentSupervisorPipe(endpoint, "release", deadline)
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
			t.Errorf("containment supervisor did not finish during cleanup")
		}
	}
	defer cleanup()

	wrongSecret := containmentSupervisorRequest{
		Version:            containmentSupervisorProtocol,
		Operation:          "hello",
		LaunchToken:        endpoint.LaunchToken,
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		Token:              endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             strings.Repeat("f", authority.SecretBytes*2),
	}
	if _, err := requestContainmentSupervisorPipeWithRequest(endpoint, wrongSecret, time.Now().Add(5*time.Second)); err == nil {
		t.Fatal("wrong-secret request unexpectedly received a supervisor response")
	}
	select {
	case <-stopCalled:
		t.Fatal("wrong-secret request stopped the containment Job")
	case <-time.After(200 * time.Millisecond):
	}
	stopMu.Lock()
	if stopAttempts != 0 || releaseCalls != 0 {
		stopMu.Unlock()
		t.Fatalf("unauthenticated request changed Job state: stop attempts=%d release calls=%d", stopAttempts, releaseCalls)
	}
	stopMu.Unlock()

	if _, err := requestContainmentSupervisorPipe(endpoint, "hello", time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("valid hello after wrong-secret request = %v", err)
	}
}

func TestNewSupervisorHandoffCarriesLaunchAndCreatorIdentity(t *testing.T) {
	jobID := strings.Repeat("b", authority.TokenBytes*2)
	handoff, err := NewSupervisorHandoff(71, "windows:71:0000000000000001", jobID)
	if err != nil {
		t.Fatalf("NewSupervisorHandoff() error = %v", err)
	}
	if err := handoff.Validate(); err != nil {
		t.Fatalf("handoff validation error = %v", err)
	}
	if !validContainmentToken(handoff.LaunchToken) {
		t.Fatalf("LaunchToken = %q, want valid token", handoff.LaunchToken)
	}
	if handoff.CreatorSessionID == nil {
		t.Fatal("CreatorSessionID is nil")
	}
}

func TestContainmentSupervisorOwnerWatchdogObservesOwnerEOF(t *testing.T) {
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	watchdog := newContainmentSupervisorOwnerWatchdog(ownerRead)
	defer watchdog.disarm()

	if err := ownerWrite.Close(); err != nil {
		t.Fatalf("close owner writer: %v", err)
	}
	select {
	case <-watchdog.ownerLost():
	case <-time.After(2 * time.Second):
		t.Fatal("owner watchdog did not observe owner EOF")
	}
}

func TestContainmentSupervisorOwnerWatchdogLeavesLiveOwnerArmed(t *testing.T) {
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	watchdog := newContainmentSupervisorOwnerWatchdog(ownerRead)
	defer watchdog.disarm()

	if _, err := ownerWrite.Write([]byte{1}); err != nil {
		t.Fatalf("write owner heartbeat byte: %v", err)
	}
	select {
	case <-watchdog.ownerLost():
		t.Fatal("live owner heartbeat was treated as owner loss")
	default:
	}
	if err := ownerWrite.Close(); err != nil {
		t.Fatalf("close owner writer: %v", err)
	}
	select {
	case <-watchdog.ownerLost():
	case <-time.After(2 * time.Second):
		t.Fatal("owner watchdog did not observe EOF after live owner closed")
	}
}

func TestContainmentSupervisorOwnerWatchdogDisarmSuppressesOwnerEOF(t *testing.T) {
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	watchdog := newContainmentSupervisorOwnerWatchdog(ownerRead)
	watchdog.disarm()
	if err := ownerWrite.Close(); err != nil {
		t.Fatalf("close owner writer: %v", err)
	}
	select {
	case <-watchdog.ownerLost():
		t.Fatal("disarmed owner watchdog reported owner loss")
	default:
	}
}

func TestContainmentSupervisorStopStateDoesNotReplayProvenStop(t *testing.T) {
	terminates := 0
	queries := 0
	releases := 0
	previousTerminate := terminateJob
	previousQuery := queryJobActiveProcesses
	previousWait := waitForEmptyJob
	previousClose := closeJob
	terminateJob = func(syscall.Handle) error {
		terminates++
		return nil
	}
	queryJobActiveProcesses = func(syscall.Handle) (uint32, error) {
		queries++
		return 0, nil
	}
	waitForEmptyJob = func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
		return nil
	}
	closeJob = func(syscall.Handle) error {
		releases++
		return nil
	}
	t.Cleanup(func() {
		terminateJob = previousTerminate
		queryJobActiveProcesses = previousQuery
		waitForEmptyJob = previousWait
		closeJob = previousClose
	})

	state := containmentSupervisorStopState{}
	if active, err := state.stop(syscall.Handle(1234)); err != nil || active != 0 {
		t.Fatalf("first stop = (%d, %v), want empty proof", active, err)
	}
	if active, err := state.stop(syscall.Handle(1234)); err != nil || active != 0 {
		t.Fatalf("replayed stop = (%d, %v), want cached proof", active, err)
	}
	if terminates != 1 || queries != 1 || releases != 0 {
		t.Fatalf("stop calls = terminate:%d query:%d release:%d, want 1, 1, 0", terminates, queries, releases)
	}
}

func TestContainmentSupervisorStopStateRetriesAfterUnprovenFailure(t *testing.T) {
	want := errors.New("empty-job proof failed")
	attempts := 0
	previousTerminate := terminateJob
	previousWait := waitForEmptyJob
	previousQuery := queryJobActiveProcesses
	previousClose := closeJob
	terminateJob = func(syscall.Handle) error {
		attempts++
		return nil
	}
	waitForEmptyJob = func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
		if attempts == 1 {
			return want
		}
		return nil
	}
	queryJobActiveProcesses = func(syscall.Handle) (uint32, error) { return 0, nil }
	closeJob = func(syscall.Handle) error { return nil }
	t.Cleanup(func() {
		terminateJob = previousTerminate
		waitForEmptyJob = previousWait
		queryJobActiveProcesses = previousQuery
		closeJob = previousClose
	})

	state := containmentSupervisorStopState{}
	if _, err := state.stop(syscall.Handle(1234)); !errors.Is(err, want) {
		t.Fatalf("first stop error = %v, want %v", err, want)
	}
	if active, err := state.stop(syscall.Handle(1234)); err != nil || active != 0 {
		t.Fatalf("retry stop = (%d, %v), want empty proof", active, err)
	}
	if attempts != 2 {
		t.Fatalf("termination attempts = %d, want 2", attempts)
	}
}

func TestStopAndReleaseSupervisorJobDoesNotReleaseBeforeEmptyProof(t *testing.T) {
	want := errors.New("empty-job proof failed")
	releases := 0
	previousTerminate := terminateJob
	previousWait := waitForEmptyJob
	previousQuery := queryJobActiveProcesses
	previousClose := closeJob
	terminateJob = func(syscall.Handle) error { return nil }
	waitForEmptyJob = func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
		return want
	}
	queryJobActiveProcesses = func(syscall.Handle) (uint32, error) { return 0, nil }
	closeJob = func(syscall.Handle) error {
		releases++
		return nil
	}
	t.Cleanup(func() {
		terminateJob = previousTerminate
		waitForEmptyJob = previousWait
		queryJobActiveProcesses = previousQuery
		closeJob = previousClose
	})

	if err := stopAndReleaseSupervisorJob(syscall.Handle(1234), func() error { return closeJob(syscall.Handle(1234)) }); !errors.Is(err, want) {
		t.Fatalf("stopAndReleaseSupervisorJob() error = %v, want %v", err, want)
	}
	if releases != 0 {
		t.Fatalf("release calls = %d, want 0 before an empty-job proof", releases)
	}
}

func TestRecoverPersistedContainmentValidatesCallerIdentityBeforeReceiptReplay(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	requestCalled := false
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })
	requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
		requestCalled = true
		return containmentSupervisorResponse{}, nil
	}

	_, err := RecoverPersistedContainmentWithAuthority(
		persisted.TargetPID+1,
		persisted.TargetIdentity,
		&persisted,
	)
	if !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("identity mismatch error = %v, want unproven stop", err)
	}
	if requestCalled {
		t.Fatal("receipt replay contacted the helper before validating caller identity")
	}
}

func TestContainmentSupervisorLeaseStateRejectsLateRenewal(t *testing.T) {
	callbacks := installContainmentSupervisorAfterFunc(t)
	stopped := 0
	state := newContainmentSupervisorLeaseState(func() { stopped++ })

	if replay, err := state.renew(time.Second, 1); replay || err != nil {
		t.Fatalf("initial renewal = (%t, %v), want new grant", replay, err)
	}
	callbacks.run(0)
	if _, err := state.renew(time.Second, 2); !errors.Is(err, errContainmentSupervisorLeaseExpired) {
		t.Fatalf("late renewal error = %v, want lease expiry", err)
	}
	if stopped != 1 {
		t.Fatalf("expiry callback calls = %d, want 1", stopped)
	}
}

func TestContainmentSupervisorLeaseStateIgnoresStaleTimerAfterRenewal(t *testing.T) {
	callbacks := installContainmentSupervisorAfterFunc(t)
	expired := 0
	state := newContainmentSupervisorLeaseState(func() { expired++ })

	if replay, err := state.renew(time.Second, 1); replay || err != nil {
		t.Fatalf("initial renewal = (%t, %v), want new grant", replay, err)
	}
	if replay, err := state.renew(time.Second, 2); replay || err != nil {
		t.Fatalf("renewal after initial grant = (%t, %v), want new grant", replay, err)
	}

	callbacks.run(0)
	if expired != 0 {
		t.Fatalf("stale expiry callback calls = %d, want 0", expired)
	}
	if replay, err := state.renew(time.Second, 2); !replay || err != nil {
		t.Fatalf("replay after stale callback = (%t, %v), want success", replay, err)
	}
	callbacks.run(1)
	if expired != 1 {
		t.Fatalf("current expiry callback calls = %d, want 1", expired)
	}
}

func TestContainmentSupervisorLeaseExpiryRetainsRecoveryAfterStopFailure(t *testing.T) {
	callbacks := installContainmentSupervisorAfterFunc(t)
	supervisorPID := os.Getpid()
	supervisorIdentity, err := ProcessIdentity(supervisorPID)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}

	endpoint, err := containmentEndpointForIdentity(71, "windows:71:0000000000000001")
	if err != nil {
		t.Fatalf("containmentEndpointForIdentity() error = %v", err)
	}
	endpoint.SupervisorPID = supervisorPID
	endpoint.SupervisorIdentity = supervisorIdentity
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		t.Fatalf("validateSupervisorEndpoint() error = %v", err)
	}

	bootstrap := containmentSupervisorBootstrap{
		Version:            containmentSupervisorProtocol,
		JobHandle:          uint64(syscall.Handle(1234)),
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		PipeToken:          endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}

	firstStopErr := errors.New("first expiry stop failed")
	var stopMu sync.Mutex
	stopAttempts := 0
	secondStopComplete := make(chan struct{})
	var secondStopCompleteOnce sync.Once
	releaseCalled := make(chan struct{})
	var releaseOnce sync.Once
	restoreJobCalls(t,
		func(syscall.Handle) error {
			stopMu.Lock()
			stopAttempts++
			attempt := stopAttempts
			stopMu.Unlock()
			if attempt == 1 {
				return firstStopErr
			}
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			stopMu.Lock()
			attempt := stopAttempts
			stopMu.Unlock()
			if attempt >= 2 {
				secondStopCompleteOnce.Do(func() { close(secondStopComplete) })
			}
			return 0, nil
		},
		func(syscall.Handle) error {
			releaseOnce.Do(func() { close(releaseCalled) })
			return nil
		},
	)
	restoreJobWait(t, func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error {
		return nil
	})

	runDone := make(chan error, 1)
	runFinished := make(chan struct{})
	go func() {
		runErr := runContainmentSupervisor(containmentSupervisorArgs{}, bootstrap)
		close(runFinished)
		runDone <- runErr
	}()
	defer func() {
		select {
		case <-runFinished:
			return
		default:
		}
		deadline := time.Now().Add(5 * time.Second)
		_, _ = requestContainmentSupervisorPipe(endpoint, "recover", deadline)
		_, _ = requestContainmentSupervisorPipe(endpoint, "release", deadline)
		select {
		case <-runFinished:
		case <-time.After(5 * time.Second):
			t.Errorf("containment supervisor did not finish during cleanup")
		}
	}()

	if _, err := requestContainmentSupervisorPipe(endpoint, "hello", time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("initial hello error = %v", err)
	}
	if _, err := requestContainmentSupervisorRenewPipe(endpoint, time.Second, 1, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("initial renewal error = %v", err)
	}
	if len(callbacks.callbacks) != 1 {
		t.Fatalf("expiry callbacks = %d, want 1", len(callbacks.callbacks))
	}

	callbacks.run(0)
	stopMu.Lock()
	gotAttempts := stopAttempts
	stopMu.Unlock()
	if gotAttempts != 1 {
		t.Fatalf("expiry stop attempts = %d, want 1", gotAttempts)
	}

	// Wake the named-pipe loop. A failed expiry stop must retain recovery so
	// the loop schedules the second stop before an explicit recover request.
	if _, err := requestContainmentSupervisorPipe(endpoint, "hello", time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("post-expiry hello error = %v", err)
	}
	select {
	case <-secondStopComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("failed expiry stop was not retried by the supervisor loop")
	}

	stopMu.Lock()
	gotAttempts = stopAttempts
	stopMu.Unlock()
	if gotAttempts != 2 {
		t.Fatalf("retained stop attempts = %d, want 2", gotAttempts)
	}
	if _, err := requestContainmentSupervisorPipe(endpoint, "recover", time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("recovery after retained stop error = %v", err)
	}
	if _, err := requestContainmentSupervisorPipe(endpoint, "release", time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("release after retained stop = %v", err)
	}
	select {
	case <-releaseCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("successful recovery did not release the supervisor Job")
	}
	select {
	case runErr := <-runDone:
		if runErr != nil {
			t.Fatalf("runContainmentSupervisor() error = %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("containment supervisor did not complete after release")
	}
}

type containmentSupervisorAfterFuncCallbacks struct {
	callbacks []func()
	timers    []*time.Timer
}

func installContainmentSupervisorAfterFunc(t *testing.T) *containmentSupervisorAfterFuncCallbacks {
	t.Helper()
	callbacks := &containmentSupervisorAfterFuncCallbacks{}
	previous := containmentSupervisorAfterFunc
	containmentSupervisorAfterFunc = func(_ time.Duration, callback func()) *time.Timer {
		callbacks.callbacks = append(callbacks.callbacks, callback)
		timer := time.NewTimer(time.Hour)
		callbacks.timers = append(callbacks.timers, timer)
		return timer
	}
	t.Cleanup(func() {
		containmentSupervisorAfterFunc = previous
		for _, timer := range callbacks.timers {
			timer.Stop()
		}
	})
	return callbacks
}

func (callbacks *containmentSupervisorAfterFuncCallbacks) run(index int) {
	callbacks.callbacks[index]()
}

func TestContainmentSupervisorLeaseStateSequencesAndReplay(t *testing.T) {
	state := newContainmentSupervisorLeaseState(nil)
	if replay, err := state.renew(time.Second, 1); replay || err != nil {
		t.Fatalf("initial renewal = (%t, %v), want new grant", replay, err)
	}
	if replay, err := state.renew(time.Second, 1); !replay || err != nil {
		t.Fatalf("identical replay = (%t, %v), want idempotent replay", replay, err)
	}
	if _, err := state.renew(2*time.Second, 1); err == nil {
		t.Fatal("same sequence with different deadline was accepted")
	}
	if _, err := state.renew(time.Second, 0); err == nil {
		t.Fatal("zero sequence was accepted")
	}
	state.stop()
	if _, err := state.renew(time.Second, 2); !errors.Is(err, errContainmentSupervisorLeaseStopped) {
		t.Fatalf("renewal after stop = %v, want stopped", err)
	}
}

func installContainmentSupervisorTestLauncher(t *testing.T) {
	t.Helper()
	restoreContainmentSupervisorLauncher(t, func(_ syscall.Handle, _ int, targetIdentity string) (containmentSupervisorLease, string, error) {
		if _, _, err := parseWindowsProcessIdentity(targetIdentity); err != nil {
			return nil, "", err
		}
		return &fakeContainmentSupervisorLease{}, targetIdentity, nil
	})
}

func TestContainmentEndpointDoesNotDeriveAuthorityFromPlainIdentity(t *testing.T) {
	identity := "windows:71:0000000000000001"
	first, err := containmentEndpointForIdentity(71, identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := containmentEndpointForIdentity(71, identity)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("public process identity produced a reusable authority endpoint: first=%#v second=%#v", first, second)
	}
	if first.Secret == second.Secret || first.Token == second.Token || first.JobID == second.JobID {
		t.Fatal("random supervisor authority values were reused")
	}
	if got, ok := TargetProcessIdentity(identity); !ok || got != identity {
		t.Fatalf("TargetProcessIdentity() = %q, %v; want plain identity", got, ok)
	}
	if _, ok := TargetProcessIdentity("windows-containment-v1:legacy"); ok {
		t.Fatal("accepted composite containment identity")
	}
}
