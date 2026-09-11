package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestStartPersistenceFailureRetainsProcessForMarkerRecovery(t *testing.T) {
	want := errors.New("journal unavailable")
	invocation := helperInvocation("stdout-then-wait", "persistence output")
	invocation.PersistProcess = func(int, string) error { return want }
	sink := &recordingSink{}
	containment := &scriptedContainment{}
	runner := testRunner(containment, "bound:process")
	process, err := runner.Start(context.Background(), invocation, sink)
	if process == nil {
		t.Fatal("Start() returned nil process after persistence failure")
	}
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want persistence failure", err)
	}
	if got := sink.output(Stdout); got != "" {
		t.Fatalf("stdout was delivered after persistence failure: %q", got)
	}
	if pid, identity := process.ProcessDetails(); pid <= 0 || identity != "bound:process" {
		t.Fatalf("ProcessDetails() = (%d, %q), want original process and attachment identity", pid, identity)
	}
	result := waitForResult(t, process)
	if !result.Terminated || result.ContainmentError != nil || result.TerminationError != nil {
		t.Fatalf("cleanup result = %#v, want completed bounded cleanup", result)
	}
	if got := containment.calls(); got != "soft,force,close" {
		t.Fatalf("containment calls = %q, want soft,force,close", got)
	}
}

func TestStartRetainsProcessAfterInitialInputFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	containment := &scriptedContainment{}
	persisted := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			kill := func() error {
				err := process.Kill()
				if errors.Is(err, os.ErrProcessDone) {
					return nil
				}
				return err
			}
			containment.setSoftForce(kill)
			containment.setDefaultForce(kill)
			cancel()
			return containment, "bound:process", nil
		},
	}
	invocation := helperInvocation("wait")
	invocation.InitialInput = []byte("must not be written after cancellation")
	invocation.PersistProcess = func(pid int, identity string) error {
		persisted = pid > 0 && identity == "bound:process"
		return nil
	}
	process, err := runner.Start(ctx, invocation, &recordingSink{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want initial-input cancellation", err)
	}
	if process == nil {
		t.Fatal("Start() discarded post-persistence Process after initial-input failure")
	}
	if !persisted {
		t.Fatal("PersistProcess was not called before initial-input failure")
	}
	if pid, identity := process.ProcessDetails(); pid <= 0 || identity != "bound:process" {
		t.Fatalf("ProcessDetails() = (%d, %q), want persistent original identity", pid, identity)
	}
	result := waitForResult(t, process)
	if !result.Terminated || result.ContainmentError != nil {
		t.Fatalf("initial-input cleanup result = %#v", result)
	}
}

func TestInputSetupFailureAlwaysRetainsProcess(t *testing.T) {
	want := errors.New("input setup failed")
	for _, operation := range []string{"initial input", "initial input close"} {
		t.Run(operation, func(t *testing.T) {
			process := startWithTestContainment(t, &scriptedContainment{})
			returned, err := cleanupAfterInputSetupFailure(process, want, operation)
			if returned != process {
				t.Fatal("cleanup did not retain the original Process")
			}
			if !errors.Is(err, want) {
				t.Fatalf("cleanup error = %v, want input setup failure", err)
			}
			if result := waitForResult(t, process); !result.Terminated {
				t.Fatalf("cleanup result = %#v, want terminated Process", result)
			}
		})
	}
}

func TestStartFailsBeforeSpawnWhenContainmentCannotBeConfigured(t *testing.T) {
	want := errors.New("pidfd process-group containment is unsupported")
	attached := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return want },
		attachProcess: func(*os.Process) (platform.Containment, string, error) {
			attached = true
			return nil, "", nil
		},
	}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if process != nil {
		t.Fatal("Start() returned a process after containment configuration failure")
	}
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want containment configuration failure", err)
	}
	if attached {
		t.Fatal("Start() attached a process after containment configuration failure")
	}
}

func TestStartRetainsPartialAttachmentForOwnedCleanup(t *testing.T) {
	want := errors.New("identity capture failed")
	containment := &scriptedContainment{}
	var attached *os.Process
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			attached = process
			containment.setDefaultForce(process.Kill)
			return containment, "", want
		},
	}
	sink := &recordingSink{}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), sink)
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want partial attachment failure", err)
	}
	if process == nil {
		t.Fatal("Start() returned nil process after partial attachment failure")
	}
	if attached != process.command.Process {
		t.Fatal("AttachProcess() did not receive the original command process")
	}
	if pid, identity := process.ProcessDetails(); pid <= 0 || identity != "" {
		t.Fatalf("ProcessDetails() = (%d, %q), want original process without invented identity", pid, identity)
	}
	if got := sink.output(Stdout); got != "" {
		t.Fatalf("stdout was delivered after partial attachment failure: %q", got)
	}
	result := waitForResult(t, process)
	if !errors.Is(result.ContainmentError, want) {
		t.Fatalf("ContainmentError = %v, want partial attachment failure", result.ContainmentError)
	}
	if !result.Terminated || result.TerminationError != nil {
		t.Fatalf("partial attachment cleanup result = %#v", result)
	}
	if got := containment.calls(); got != "soft,force,close" {
		t.Fatalf("containment calls = %q, want soft,force,close", got)
	}
}

func TestStartUsesAttachmentIdentityFromOriginalProcess(t *testing.T) {
	containment := &scriptedContainment{}
	var attached *os.Process
	var persistedPID int
	var persistedIdentity string
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			attached = process
			containment.setDefaultForce(process.Kill)
			return containment, fmt.Sprintf("bound:%d", process.Pid), nil
		},
	}
	invocation := helperInvocation("wait")
	invocation.PersistProcess = func(pid int, identity string) error {
		persistedPID, persistedIdentity = pid, identity
		return nil
	}
	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if attached != process.command.Process {
		t.Fatal("AttachProcess() did not receive the original command process")
	}
	if got, want := process.Identity, fmt.Sprintf("bound:%d", process.PID); got != want {
		t.Fatalf("Process identity = %q, want %q", got, want)
	}
	if persistedPID != process.PID || persistedIdentity != process.Identity {
		t.Fatalf("PersistProcess() = (%d, %q), want (%d, %q)", persistedPID, persistedIdentity, process.PID, process.Identity)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = waitForResult(t, process)
}

func TestTerminateFallsBackAfterUnsupportedSoftStopAndPreservesGrace(t *testing.T) {
	containment := &scriptedContainment{softErr: fmt.Errorf("soft stop unavailable: %w", errors.ErrUnsupported)}
	process := startWithTestContainment(t, containment)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if err := process.Terminate(ctx, 100*time.Millisecond); err != nil {
		t.Fatalf("Terminate() error = %v, want successful hard fallback", err)
	}
	if elapsed := time.Since(started); elapsed < 75*time.Millisecond {
		t.Fatalf("hard fallback started after %v, want configured grace", elapsed)
	}
	result := waitForResult(t, process)
	if result.TerminationError != nil {
		t.Fatalf("TerminationError = %v, want unsupported soft stop ignored after fallback", result.TerminationError)
	}
	if got := containment.calls(); got != "soft,force,close" {
		t.Fatalf("containment calls = %q, want soft,force,close", got)
	}
}

func TestTerminatePreservesGenuineSoftFailureAfterHardFallback(t *testing.T) {
	want := errors.New("soft stop denied")
	containment := &scriptedContainment{softErr: want}
	process := startWithTestContainment(t, containment)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, want) {
		t.Fatalf("Terminate() error = %v, want genuine soft failure", err)
	}
	result := waitForResult(t, process)
	if !errors.Is(result.TerminationError, want) {
		t.Fatalf("TerminationError = %v, want genuine soft failure", result.TerminationError)
	}
}

func TestTerminatePreservesSoftFailureJoinedWithUnsupported(t *testing.T) {
	want := errors.New("soft stop permission denied")
	containment := &scriptedContainment{softErr: errors.Join(errors.ErrUnsupported, want)}
	process := startWithTestContainment(t, containment)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, want) {
		t.Fatalf("Terminate() error = %v, want joined genuine soft failure", err)
	}
	result := waitForResult(t, process)
	if !errors.Is(result.TerminationError, want) {
		t.Fatalf("TerminationError = %v, want joined genuine soft failure", result.TerminationError)
	}
}

type scriptedContainment struct {
	mutex sync.Mutex

	softErr  error
	forceErr error
	closeErr error
	soft     func() error
	force    func() error
	callsLog []string
}

func (containment *scriptedContainment) Terminate(force bool) error {
	containment.mutex.Lock()
	if force {
		containment.callsLog = append(containment.callsLog, "force")
		forceCall := containment.force
		forceErr := containment.forceErr
		containment.mutex.Unlock()
		if forceCall != nil {
			return forceCall()
		}
		return forceErr
	}
	containment.callsLog = append(containment.callsLog, "soft")
	softCall := containment.soft
	softErr := containment.softErr
	containment.mutex.Unlock()
	if softCall != nil {
		return softCall()
	}
	return softErr
}

func (containment *scriptedContainment) setSoftForce(force func() error) {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	if containment.soft == nil {
		containment.soft = force
	}
}

func (containment *scriptedContainment) Close() error {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	containment.callsLog = append(containment.callsLog, "close")
	return containment.closeErr
}

func (containment *scriptedContainment) setDefaultForce(force func() error) {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	if containment.force == nil {
		containment.force = force
	}
}

func (containment *scriptedContainment) calls() string {
	containment.mutex.Lock()
	defer containment.mutex.Unlock()
	return strings.Join(containment.callsLog, ",")
}

func testRunner(containment *scriptedContainment, identity string) Runner {
	return Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, identity, nil
		},
	}
}

func startWithTestContainment(t *testing.T, containment *scriptedContainment) *Process {
	t.Helper()
	process, err := testRunner(containment, "bound:process").Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return process
}
