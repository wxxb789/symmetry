package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
