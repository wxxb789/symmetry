//go:build windows

package platform

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestContainmentSupervisorStopStateReplaysExactReceipt(t *testing.T) {
	terminates := 0
	queries := 0
	restoreJobCalls(t,
		func(syscall.Handle) error {
			terminates++
			return nil
		},
		func(syscall.Handle) (uint32, error) {
			queries++
			return 0, nil
		},
		func(syscall.Handle) error { return nil },
	)
	previousWait := waitForEmptyJob
	waitForEmptyJob = func(syscall.Handle, time.Time, func(syscall.Handle) (uint32, error)) error { return nil }
	t.Cleanup(func() { waitForEmptyJob = previousWait })

	endpoint := containmentSupervisorEndpoint{
		TargetPID:          71,
		TargetIdentity:     "windows:71:0000000000000001",
		SupervisorPID:      72,
		SupervisorIdentity: "windows:72:0000000000000002",
		Token:              strings.Repeat("a", authority.TokenBytes*2),
		JobID:              strings.Repeat("b", authority.TokenBytes*2),
		Secret:             strings.Repeat("c", authority.SecretBytes*2),
	}
	state := containmentSupervisorStopState{}
	if active, err := state.stop(syscall.Handle(1234)); err != nil || active != 0 {
		t.Fatalf("first stop = (%d, %v), want empty proof", active, err)
	}
	first, ok := state.receiptFor(endpoint)
	if !ok {
		t.Fatal("first stop did not produce a receipt")
	}
	if active, err := state.stop(syscall.Handle(1234)); err != nil || active != 0 {
		t.Fatalf("replayed stop = (%d, %v), want cached proof", active, err)
	}
	second, ok := state.receiptFor(endpoint)
	if !ok || second != first {
		t.Fatalf("replayed receipt = %#v, want exact first receipt %#v", second, first)
	}
	if terminates != 1 || queries != 1 {
		t.Fatalf("stop calls = terminate:%d query:%d, want one each", terminates, queries)
	}
}

func TestContainmentSupervisorReleaseRequiresStopProofAndDoesNotRearmLease(t *testing.T) {
	released := false
	state := containmentSupervisorStopState{}
	if err := state.release(func() error {
		released = true
		return nil
	}); !errors.Is(err, errContainmentSupervisorStopReceiptUnavailable) {
		t.Fatalf("release before stop = %v, want missing receipt", err)
	}
	if released {
		t.Fatal("release before stop invoked the Job release callback")
	}

	lease := newContainmentSupervisorLeaseState(nil)
	if replay, err := lease.renew(time.Second, 1); replay || err != nil {
		t.Fatalf("initial lease renewal = (%t, %v), want grant", replay, err)
	}
	lease.stop()
	if err := state.release(func() error { return nil }); err == nil {
		t.Fatal("unproven release unexpectedly succeeded")
	}
	if _, err := lease.renew(time.Second, 2); !errors.Is(err, errContainmentSupervisorLeaseStopped) {
		t.Fatalf("release path rearmed stopped lease: %v", err)
	}
}

func TestRecoverPersistedContainmentReplaysDurableReceiptWithoutContactingHelper(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })
	contacted := false
	requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
		contacted = true
		return containmentSupervisorResponse{}, errors.New("helper must not be contacted for durable receipt replay")
	}

	receipt, err := RecoverPersistedContainmentWithAuthority(persisted.TargetPID, persisted.TargetIdentity, &persisted)
	if err != nil {
		t.Fatalf("RecoverPersistedContainmentWithAuthority() error = %v", err)
	}
	if contacted || receipt != *persisted.StopReceipt {
		t.Fatalf("replayed receipt = %#v, contacted=%t, want %#v and no helper contact", receipt, contacted, *persisted.StopReceipt)
	}
}

func TestRecoverPersistedContainmentRejectsMismatchedDurableReceipt(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	bad := *persisted.StopReceipt
	bad.PipeToken = strings.Repeat("d", authority.TokenBytes*2)
	persisted.StopReceipt = &bad
	contacted := false
	previousRequest := requestContainmentSupervisor
	requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
		contacted = true
		return containmentSupervisorResponse{}, nil
	}
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })

	if _, err := RecoverPersistedContainmentWithAuthority(persisted.TargetPID, persisted.TargetIdentity, &persisted); !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("mismatched receipt error = %v, want unproven stop", err)
	}
	if contacted {
		t.Fatal("mismatched durable receipt reached the helper")
	}
}

func TestReleasePersistedContainmentRequiresDurableReceiptAcknowledgement(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	persisted.StopReceipt = nil
	contacted := false
	previousRequest := requestContainmentSupervisor
	requestContainmentSupervisor = func(containmentSupervisorEndpoint, string, time.Time) (containmentSupervisorResponse, error) {
		contacted = true
		return containmentSupervisorResponse{}, nil
	}
	t.Cleanup(func() { requestContainmentSupervisor = previousRequest })

	if err := ReleasePersistedContainmentWithAuthority(persisted.TargetPID, persisted.TargetIdentity, &persisted); !errors.Is(err, errContainmentStopUnproven) {
		t.Fatalf("release without receipt error = %v, want unproven stop", err)
	}
	if contacted {
		t.Fatal("release without durable receipt reached the helper")
	}
}

func TestReleasePersistedContainmentAuthenticatesAndAcknowledgesRelease(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	previousIdentity := readSupervisorProcessIdentity
	previousRequest := requestContainmentSupervisor
	t.Cleanup(func() {
		readSupervisorProcessIdentity = previousIdentity
		requestContainmentSupervisor = previousRequest
	})
	readSupervisorProcessIdentity = func(pid int) (string, error) {
		if pid != persisted.SupervisorPID {
			t.Fatalf("identity lookup PID = %d, want %d", pid, persisted.SupervisorPID)
		}
		return persisted.SupervisorIdentity, nil
	}
	called := false
	requestContainmentSupervisor = func(endpoint containmentSupervisorEndpoint, operation string, _ time.Time) (containmentSupervisorResponse, error) {
		called = true
		if operation != "release" || endpoint.Secret != persisted.Secret {
			t.Fatalf("release request = operation:%q secret:%q", operation, endpoint.Secret)
		}
		return containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          "release",
			Status:             "released",
			TargetPID:          endpoint.TargetPID,
			TargetIdentity:     endpoint.TargetIdentity,
			SupervisorPID:      endpoint.SupervisorPID,
			SupervisorIdentity: endpoint.SupervisorIdentity,
			Token:              endpoint.Token,
			JobID:              endpoint.JobID,
			ActiveProcesses:    0,
		}, nil
	}

	if err := ReleasePersistedContainmentWithAuthority(persisted.TargetPID, persisted.TargetIdentity, &persisted); err != nil {
		t.Fatalf("ReleasePersistedContainmentWithAuthority() error = %v", err)
	}
	if !called {
		t.Fatal("release request was not sent")
	}
}

func TestReleaseProofAfterLostResponseRequiresExactHelperExit(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	handoff := authority.SupervisorHandoff{
		Version:            authority.SupervisorHandoffVersion,
		LaunchToken:        strings.Repeat("d", authority.TokenBytes*2),
		Secret:             persisted.Secret,
		TargetPID:          persisted.TargetPID,
		TargetIdentity:     persisted.TargetIdentity,
		PipeToken:          persisted.PipeToken,
		JobID:              persisted.JobID,
		SupervisorPID:      persisted.SupervisorPID,
		SupervisorIdentity: persisted.SupervisorIdentity,
	}
	previousOpen := openSupervisorObservationProcess
	previousRead := readSupervisorObservationIdentity
	previousWait := waitSupervisorObservationProcess
	previousClose := closeSupervisorObservationProcess
	t.Cleanup(func() {
		openSupervisorObservationProcess = previousOpen
		readSupervisorObservationIdentity = previousRead
		waitSupervisorObservationProcess = previousWait
		closeSupervisorObservationProcess = previousClose
	})
	closed := false
	openSupervisorObservationProcess = func(int) (syscall.Handle, error) { return syscall.Handle(123), nil }
	readSupervisorObservationIdentity = func(int, syscall.Handle) (string, error) { return handoff.SupervisorIdentity, nil }
	waitSupervisorObservationProcess = func(syscall.Handle) (uint32, error) { return waitObject0, nil }
	closeSupervisorObservationProcess = func(syscall.Handle) error { closed = true; return nil }

	proof, err := releaseProofAfterLostResponse(containmentSupervisorEndpoint{
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
	}, handoff)
	if err != nil {
		t.Fatalf("releaseProofAfterLostResponse() error = %v", err)
	}
	if !closed || !proof.ValidFor(handoff) {
		t.Fatalf("release proof = %#v closed=%t, want exact proof and helper observation close", proof, closed)
	}

	waitSupervisorObservationProcess = func(syscall.Handle) (uint32, error) { return waitTimeout, nil }
	if _, err := releaseProofAfterLostResponse(containmentSupervisorEndpoint{
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
	}, handoff); err == nil {
		t.Fatal("releaseProofAfterLostResponse() accepted a live exact helper after lost response")
	}
}

func TestReleasePersistedContainmentDoesNotAcceptMissingIdentityReadAlone(t *testing.T) {
	persisted := testSupervisorAuthorityWithReceipt()
	previousIdentity := readSupervisorProcessIdentity
	previousOpen := openSupervisorObservationProcess
	previousRead := readSupervisorObservationIdentity
	previousWait := waitSupervisorObservationProcess
	previousClose := closeSupervisorObservationProcess
	t.Cleanup(func() {
		readSupervisorProcessIdentity = previousIdentity
		openSupervisorObservationProcess = previousOpen
		readSupervisorObservationIdentity = previousRead
		waitSupervisorObservationProcess = previousWait
		closeSupervisorObservationProcess = previousClose
	})
	readSupervisorProcessIdentity = func(int) (string, error) { return "", syscall.Errno(2) }
	openSupervisorObservationProcess = func(int) (syscall.Handle, error) { return syscall.Handle(123), nil }
	readSupervisorObservationIdentity = func(int, syscall.Handle) (string, error) { return persisted.SupervisorIdentity, nil }
	waitSupervisorObservationProcess = func(syscall.Handle) (uint32, error) { return waitTimeout, nil }
	closeSupervisorObservationProcess = func(syscall.Handle) error { return nil }

	if err := ReleasePersistedContainmentWithAuthority(persisted.TargetPID, persisted.TargetIdentity, &persisted); err == nil {
		t.Fatal("ReleasePersistedContainmentWithAuthority() accepted a missing identity read while exact helper remained live")
	}
}

func testSupervisorAuthorityWithReceipt() authority.Supervisor {
	value := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		TargetPID:          71,
		TargetIdentity:     "windows:71:0000000000000001",
		PipeToken:          strings.Repeat("b", authority.TokenBytes*2),
		JobID:              strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID:      72,
		SupervisorIdentity: "windows:72:0000000000000002",
	}
	value.StopReceipt = &authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		ActiveProcesses:    0,
	}
	return value
}
