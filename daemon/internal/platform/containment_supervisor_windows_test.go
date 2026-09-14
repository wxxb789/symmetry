//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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
