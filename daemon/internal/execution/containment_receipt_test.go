package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestProcessPersistsContainmentStopReceiptBeforeRelease(t *testing.T) {
	containment := newReceiptContainment()
	var events []string
	var mutex sync.Mutex
	invocation := helperInvocation("args")
	invocation.PersistContainmentStopReceipt = func(_ int, _ string, receipt authority.StopReceipt) error {
		if receipt.Status != "stopped" || receipt.ActiveProcesses != 0 {
			t.Fatalf("stop receipt = %#v, want an empty stopped witness", receipt)
		}
		mutex.Lock()
		events = append(events, "persist")
		mutex.Unlock()
		return nil
	}
	containment.onRelease = func() {
		mutex.Lock()
		events = append(events, "release")
		mutex.Unlock()
	}
	runner := receiptTestRunner(containment)
	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := waitForResult(t, process)
	if !result.Success() {
		t.Fatalf("process result = %#v, want success", result)
	}
	mutex.Lock()
	gotEvents := append([]string(nil), events...)
	mutex.Unlock()
	if len(gotEvents) != 2 || gotEvents[0] != "persist" || gotEvents[1] != "release" {
		t.Fatalf("containment events = %#v, want [persist release]", gotEvents)
	}
	if containment.closeCalls != 1 || containment.releaseCalls != 1 {
		t.Fatalf("containment calls = close:%d release:%d, want one each", containment.closeCalls, containment.releaseCalls)
	}
}

func TestProcessRetriesReceiptPersistenceAfterWaitWithoutRestopping(t *testing.T) {
	containment := newReceiptContainment()
	writeAttempts := 0
	want := errors.New("journal write failed")
	invocation := helperInvocation("args")
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		writeAttempts++
		if writeAttempts == 1 {
			return want
		}
		return nil
	}
	runner := receiptTestRunner(containment)
	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := waitForResult(t, process)
	if first.ContainmentError == nil || !errors.Is(first.ContainmentError, want) {
		t.Fatalf("first process result containment error = %v, want %v", first.ContainmentError, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); err != nil {
		t.Fatalf("retry Terminate() error = %v", err)
	}
	second := process.Wait()
	if second.ContainmentError != nil {
		t.Fatalf("second process result containment error = %v, want nil", second.ContainmentError)
	}
	if writeAttempts != 2 {
		t.Fatalf("receipt write attempts = %d, want two", writeAttempts)
	}
	if containment.closeCalls != 1 || containment.releaseCalls != 1 {
		t.Fatalf("containment calls = close:%d release:%d, want one each", containment.closeCalls, containment.releaseCalls)
	}
}

func TestProcessRetriesDurableHandleReleaseBeforeReceiptAndHelperRelease(t *testing.T) {
	wantRelease := errors.New("job handle release failed")
	containment := newDurableHandleRetryContainment(wantRelease)
	invocation := helperInvocation("args")
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		containment.recordEvent("persist")
		return nil
	}

	process, err := durableHandleRetryRunner(containment).Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := waitForResult(t, process)
	if !errors.Is(first.ContainmentError, wantRelease) {
		t.Fatalf("first process result containment error = %v, want %v", first.ContainmentError, wantRelease)
	}
	if got := containment.snapshot(); got.closeCalls != 1 || got.stopCalls != 1 || got.handleReleaseCalls != 1 || got.releaseCalls != 0 {
		t.Fatalf("first durable cleanup = %#v, want one stop/close, one handle release attempt, and no helper release", got)
	}

	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("FinalizeContainment() retry error = %v", err)
	}
	second := process.Wait()
	if second.ContainmentError != nil {
		t.Fatalf("second process result containment error = %v, want nil after local retry", second.ContainmentError)
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("repeated FinalizeContainment() error = %v", err)
	}

	state := containment.snapshot()
	if state.closeCalls != 2 || state.stopCalls != 1 || state.handleReleaseCalls != 2 || state.releaseCalls != 1 {
		t.Fatalf("final durable cleanup = %#v, want close:2 stop:1 handle-release:2 helper-release:1", state)
	}
	wantEvents := []string{"stop", "handle-release", "handle-release", "persist", "helper-release"}
	if !slices.Equal(state.events, wantEvents) {
		t.Fatalf("durable cleanup order = %#v, want %#v", state.events, wantEvents)
	}
}

type receiptContainment struct {
	*scriptedContainment
	receipt      authority.StopReceipt
	closeCalls   int
	releaseCalls int
	onRelease    func()
}

func newReceiptContainment() *receiptContainment {
	return &receiptContainment{
		scriptedContainment: &scriptedContainment{},
		receipt: authority.StopReceipt{
			Version:         authority.SupervisorVersion,
			Status:          "stopped",
			TargetPID:       1,
			TargetIdentity:  "bound:process",
			ActiveProcesses: 0,
		},
	}
}

func receiptTestRunner(containment *receiptContainment) Runner {
	return Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, "bound:process", nil
		},
	}
}

func (containment *receiptContainment) Close() error {
	if containment.closeCalls == 0 {
		containment.closeCalls++
	}
	return nil
}

func (containment *receiptContainment) ContainmentStopReceipt() (authority.StopReceipt, bool) {
	return containment.receipt, true
}

func (containment *receiptContainment) ReleaseContainment() error {
	containment.releaseCalls++
	if containment.onRelease != nil {
		containment.onRelease()
	}
	return nil
}

var _ platform.Containment = (*receiptContainment)(nil)
var _ platform.ContainmentStopReceiptProvider = (*receiptContainment)(nil)
var _ platform.ContainmentStopReceiptReleaser = (*receiptContainment)(nil)

type durableHandleRetryContainment struct {
	*scriptedContainment
	mutex              sync.Mutex
	receipt            authority.StopReceipt
	firstReleaseError  error
	closeCalls         int
	stopCalls          int
	handleReleaseCalls int
	releaseCalls       int
	retryable          bool
	events             []string
}

type durableHandleRetryState struct {
	closeCalls         int
	stopCalls          int
	handleReleaseCalls int
	releaseCalls       int
	events             []string
}

func newDurableHandleRetryContainment(firstReleaseError error) *durableHandleRetryContainment {
	return &durableHandleRetryContainment{
		scriptedContainment: &scriptedContainment{},
		receipt: authority.StopReceipt{
			Version:         authority.SupervisorVersion,
			Status:          "stopped",
			TargetPID:       1,
			TargetIdentity:  "bound:process",
			ActiveProcesses: 0,
		},
		firstReleaseError: firstReleaseError,
	}
}

func durableHandleRetryRunner(containment *durableHandleRetryContainment) Runner {
	return Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, "bound:process", nil
		},
	}
}

func (containment *durableHandleRetryContainment) Close() error {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	containment.closeCalls++
	if containment.stopCalls == 0 {
		containment.stopCalls++
		containment.events = append(containment.events, "stop")
	}
	containment.handleReleaseCalls++
	containment.events = append(containment.events, "handle-release")
	if containment.handleReleaseCalls == 1 {
		containment.retryable = true
		return containment.firstReleaseError
	}
	containment.retryable = false
	return nil
}

func (containment *durableHandleRetryContainment) ContainmentCloseRetryable() bool {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	return containment.retryable
}

func (containment *durableHandleRetryContainment) ContainmentStopReceipt() (authority.StopReceipt, bool) {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	return containment.receipt, containment.stopCalls != 0
}

func (containment *durableHandleRetryContainment) ReleaseContainment() error {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	containment.releaseCalls++
	containment.events = append(containment.events, "helper-release")
	return nil
}

func (containment *durableHandleRetryContainment) recordEvent(event string) {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	containment.events = append(containment.events, event)
}

func (containment *durableHandleRetryContainment) snapshot() durableHandleRetryState {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	return durableHandleRetryState{
		closeCalls:         containment.closeCalls,
		stopCalls:          containment.stopCalls,
		handleReleaseCalls: containment.handleReleaseCalls,
		releaseCalls:       containment.releaseCalls,
		events:             append([]string(nil), containment.events...),
	}
}

var _ platform.Containment = (*durableHandleRetryContainment)(nil)
var _ platform.ContainmentCloseRetryer = (*durableHandleRetryContainment)(nil)
var _ platform.ContainmentStopReceiptProvider = (*durableHandleRetryContainment)(nil)
var _ platform.ContainmentStopReceiptReleaser = (*durableHandleRetryContainment)(nil)
