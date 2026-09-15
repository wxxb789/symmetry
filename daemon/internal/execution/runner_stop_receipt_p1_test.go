package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestAuthorityPersistenceFailureFreezesReceiptFence(t *testing.T) {
	containment := newP1AuthorityContainment()
	wantAuthorityErr := errors.New("authority CAS write failed")
	invocation := helperInvocation("args")
	invocation.PersistProcessAuthority = func(_ int, _ string, value *authority.Supervisor) error {
		value.Secret = "mutated-by-callback"
		return wantAuthorityErr
	}
	process, err := p1ReceiptRunner(containment).Start(context.Background(), invocation, &recordingSink{})
	if process == nil || !errors.Is(err, wantAuthorityErr) {
		t.Fatalf("Start() = (%T, %v), want retained process and authority error", process, err)
	}
	result := waitForResult(t, process)
	if result.ContainmentError == nil {
		t.Fatal("authority persistence failure was finalized without a receipt fence")
	}
	if containment.abortCalls != 0 || containment.releaseCalls != 0 {
		t.Fatalf("authority failure cleanup = abort:%d release:%d, want both zero", containment.abortCalls, containment.releaseCalls)
	}
	if containment.authority.Secret == "mutated-by-callback" {
		t.Fatal("PersistProcessAuthority callback received the provider's mutable authority")
	}
}

func TestAuthorityPersistenceRetryPrecedesReceiptAndRelease(t *testing.T) {
	containment := newP1AuthorityContainment()
	authorityReceipt := containment.receipt
	containment.authority.StopReceipt = &authorityReceipt
	wantAuthorityErr := errors.New("initial authority write failed")
	var events []string
	var mutex sync.Mutex
	authorityAttempts := 0
	invocation := helperInvocation("args")
	invocation.PersistProcessAuthority = func(_ int, _ string, value *authority.Supervisor) error {
		mutex.Lock()
		defer mutex.Unlock()
		authorityAttempts++
		events = append(events, "authority")
		if value.Secret != containment.authority.Secret {
			t.Fatalf("authority retry secret = %q, want immutable fence", value.Secret)
		}
		if value.StopReceipt == nil || *value.StopReceipt != authorityReceipt {
			t.Fatalf("authority retry stop receipt = %#v, want immutable fence %#v", value.StopReceipt, authorityReceipt)
		}
		if authorityAttempts == 1 {
			value.Secret = "mutated-by-first-callback"
			value.StopReceipt.Status = "mutated-by-first-callback"
			return wantAuthorityErr
		}
		return nil
	}
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		mutex.Lock()
		events = append(events, "receipt")
		mutex.Unlock()
		return nil
	}
	containment.onRelease = func() {
		mutex.Lock()
		events = append(events, "release")
		mutex.Unlock()
	}

	process, err := p1ReceiptRunner(containment).Start(context.Background(), invocation, &recordingSink{})
	if process == nil || !errors.Is(err, wantAuthorityErr) {
		t.Fatalf("Start() = (%T, %v), want retained process and authority error", process, err)
	}
	result := waitForResult(t, process)
	if result.ContainmentError != nil {
		t.Fatalf("result containment error = %v, want nil after durable retry", result.ContainmentError)
	}
	mutex.Lock()
	gotEvents := append([]string(nil), events...)
	mutex.Unlock()
	if !slices.Equal(gotEvents, []string{"authority", "authority", "receipt", "release"}) {
		t.Fatalf("finalization order = %#v, want authority retry before receipt/release", gotEvents)
	}
	if containment.abortCalls != 0 || containment.releaseCalls != 1 {
		t.Fatalf("authority retry cleanup = abort:%d release:%d, want 0, 1", containment.abortCalls, containment.releaseCalls)
	}
}

func TestLinuxLegacyNoAuthorityContainmentIgnoresStopReceiptCallback(t *testing.T) {
	containment := &p1PlainContainment{}
	invocation := helperInvocation("args")
	receiptCalls := 0
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		receiptCalls++
		return errors.New("legacy containment must not persist a stop receipt")
	}
	process, err := p1PlainRunner(containment).Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v, want process to start", err)
	}
	result := waitForResult(t, process)
	if result.ContainmentError != nil || receiptCalls != 0 || containment.closeCalls != 1 {
		t.Fatalf("legacy no-authority finalization = result:%#v receipt calls:%d close calls:%d, want no containment error, no receipt callback, and one close", result, receiptCalls, containment.closeCalls)
	}
}

func TestResultDoneFinalizationRetriesReceiptWithoutRepeatingClose(t *testing.T) {
	containment := newP1AuthorityContainment()
	writeAttempts := 0
	invocation := helperInvocation("args")
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		writeAttempts++
		if writeAttempts == 1 {
			return errors.New("receipt write after stop")
		}
		return nil
	}
	process, err := p1ReceiptRunner(containment).Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := waitForResult(t, process)
	if first.ContainmentError == nil {
		t.Fatal("first result omitted the receipt persistence failure")
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("FinalizeContainment() error = %v", err)
	}
	if writeAttempts != 2 || containment.closeCalls != 1 || containment.releaseCalls != 1 {
		t.Fatalf("finalization = writes:%d closes:%d releases:%d, want 2, 1, 1", writeAttempts, containment.closeCalls, containment.releaseCalls)
	}
}

func TestResumeStartupFailureDoesNotBecomeContainmentErrorAfterRelease(t *testing.T) {
	containment := newP1AuthorityContainment()
	resumeErr := errors.New("resume failed")
	backendCloseErr := errors.New("native close after resume failure")
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(*exec.Cmd, Invocation, *os.File, *os.File, *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid:         123,
				identity:    "bound:process",
				containment: containment,
				wait:        func() (int, error) { return 0, nil },
				kill:        func() error { return nil },
				close:       func() error { return backendCloseErr },
				resume:      func() error { return resumeErr },
			}, nil
		},
	}
	invocation := helperInvocation("args")
	invocation.PersistProcessAuthority = func(int, string, *authority.Supervisor) error { return nil }
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error { return nil }

	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if process == nil || !errors.Is(err, resumeErr) {
		t.Fatalf("Start() = (%T, %v), want retained process and resume error", process, err)
	}
	result := waitForResult(t, process)
	if result.ContainmentError != nil {
		t.Fatalf("result containment error = %v, want nil after receipt/release", result.ContainmentError)
	}
	if !errors.Is(result.TerminationError, backendCloseErr) {
		t.Fatalf("result termination error = %v, want backend close error", result.TerminationError)
	}
	if containment.releaseCalls != 1 {
		t.Fatalf("containment release calls = %d, want one", containment.releaseCalls)
	}
}

type p1AuthorityContainment struct {
	*receiptContainment
	authority  *authority.Supervisor
	abortCalls int
}

func newP1AuthorityContainment() *p1AuthorityContainment {
	return &p1AuthorityContainment{
		receiptContainment: newReceiptContainment(),
		authority: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TargetIdentity:     "bound:process",
			PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			JobID:              "cccccccccccccccccccccccccccc",
			SupervisorPID:      99,
			SupervisorIdentity: "helper:99",
		},
	}
}

func (containment *p1AuthorityContainment) ContainmentAuthority() *authority.Supervisor {
	return containment.authority
}

func (containment *p1AuthorityContainment) AbortContainment() error {
	containment.abortCalls++
	return nil
}

func p1ReceiptRunner(containment *p1AuthorityContainment) Runner {
	return Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			containment.authority.TargetPID = process.Pid
			containment.authority.TargetIdentity = "bound:process"
			return containment, "bound:process", nil
		},
	}
}

type p1PlainContainment struct{ closeCalls int }

func (*p1PlainContainment) Terminate(bool) error { return nil }

func (containment *p1PlainContainment) Close() error {
	containment.closeCalls++
	return nil
}

func p1PlainRunner(containment *p1PlainContainment) Runner {
	return Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(*os.Process) (platform.Containment, string, error) {
			return containment, "plain:process", nil
		},
	}
}

var _ platform.ContainmentAuthorityProvider = (*p1AuthorityContainment)(nil)
var _ platform.ContainmentStopReceiptAborter = (*p1AuthorityContainment)(nil)
var _ platform.Containment = (*p1PlainContainment)(nil)
