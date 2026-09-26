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

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestRunnerPassesCallerContextToProcessLauncher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(launchContext context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, _ *os.File, _ *os.File) (*startedProcess, error) {
			close(entered)
			<-launchContext.Done()
			return nil, launchContext.Err()
		},
	}
	result := make(chan error, 1)
	go func() {
		_, err := runner.Start(ctx, helperInvocation("wait"), &recordingSink{})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("process launcher was not entered")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after launcher context cancellation")
	}
}

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

func TestContainmentCloseErrorDoesNotPersistGenericUnprovenMarker(t *testing.T) {
	closeErr := errors.New("transient containment close failure")
	containment := &scriptedContainment{closeErr: closeErr}
	markerPersisted := false
	process, err := testRunner(containment, "bound:process").Start(
		context.Background(),
		Invocation{
			Program: os.Args[0],
			Args:    []string{"-test.run=^TestHelperProcess$", "--", "wait"},
			Env:     append(minimalEnvironment(), "GO_WANT_HELPER_PROCESS=1"),
			PersistContainmentUnproven: func(int, string) error {
				markerPersisted = true
				return nil
			},
		},
		&recordingSink{},
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, closeErr) {
		t.Fatalf("Terminate() error = %v, want %v", err, closeErr)
	}
	result := process.Wait()
	if !errors.Is(result.ContainmentError, closeErr) {
		t.Fatalf("ContainmentError = %v, want %v", result.ContainmentError, closeErr)
	}
	if markerPersisted {
		t.Fatal("generic containment close error persisted Linux-only uncertainty marker")
	}
}

func TestRunnerPersistsObservedContainmentUncertaintyAfterMarkerBeforeClose(t *testing.T) {
	for _, observed := range []bool{true, false} {
		t.Run(fmt.Sprintf("observed=%v", observed), func(t *testing.T) {
			containment := &earlyUnprovenContainment{observed: observed, closeGate: make(chan struct{})}
			markerReady := false
			persistenceCalls := 0
			invocation := helperInvocation("args")
			invocation.PersistProcess = func(int, string) error {
				containment.mutex.Lock()
				installed := containment.callback != nil
				containment.mutex.Unlock()
				if !installed {
					t.Fatal("containment uncertainty callback was not installed before marker persistence")
				}
				markerReady = true
				return nil
			}
			invocation.PersistContainmentUnproven = func(int, string) error {
				if !markerReady {
					return errors.New("process marker is not written")
				}
				persistenceCalls++
				return nil
			}

			runner := Runner{
				configureProcess: func(*exec.Cmd) error { return nil },
				attachProcess: func(process *os.Process) (platform.Containment, string, error) {
					return containment, "bound:process", nil
				},
			}
			process, err := runner.Start(context.Background(), invocation, &recordingSink{})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if process == nil {
				t.Fatal("Start() returned nil process")
			}
			wantCalls := 0
			if observed {
				wantCalls = 1
			}
			if persistenceCalls != wantCalls {
				t.Fatalf("early uncertainty persistence calls = %d, want %d", persistenceCalls, wantCalls)
			}
			close(containment.closeGate)
			result := waitForResult(t, process)
			if result.ContainmentError != nil {
				t.Fatalf("result containment error = %v", result.ContainmentError)
			}
		})
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

func TestStartPersistsMarkerBeforeCleaningRetainedInitialScanFailure(t *testing.T) {
	wantAttachErr := errors.New("initial descendant containment scan: leader absence before a complete descendant scan")
	containment := &earlyUnprovenContainment{observed: true}
	events := make([]string, 0, 2)
	invocation := helperInvocation("exit")
	invocation.PersistProcess = func(pid int, identity string) error {
		containment.mutex.Lock()
		callbackInstalled := containment.callback != nil
		containment.mutex.Unlock()
		if !callbackInstalled {
			t.Fatal("containment uncertainty callback was not installed before marker persistence")
		}
		if pid <= 0 || identity != "bound:process" {
			t.Fatalf("PersistProcess() = (%d, %q), want an exact attached process identity", pid, identity)
		}
		events = append(events, "marker")
		return nil
	}
	invocation.PersistContainmentUnproven = func(pid int, identity string) error {
		if pid <= 0 || identity != "bound:process" {
			t.Fatalf("PersistContainmentUnproven() = (%d, %q), want an exact attached process identity", pid, identity)
		}
		events = append(events, "uncertainty")
		return nil
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(*os.Process) (platform.Containment, string, error) {
			return containment, "bound:process", wantAttachErr
		},
	}
	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if !errors.Is(err, wantAttachErr) {
		t.Fatalf("Start() error = %v, want initial scan failure", err)
	}
	if process == nil {
		t.Fatal("Start() returned nil process after retained initial scan failure")
	}
	if len(events) != 2 || events[0] != "marker" || events[1] != "uncertainty" {
		t.Fatalf("persistence order = %#v, want [marker uncertainty] before cleanup", events)
	}
	result := waitForResult(t, process)
	if !errors.Is(result.ContainmentError, wantAttachErr) {
		t.Fatalf("ContainmentError = %v, want initial scan failure", result.ContainmentError)
	}
}

func TestFinalizeContainmentRetriesPartialHelperCleanup(t *testing.T) {
	want := errors.New("partial helper cleanup failed")
	containment := &retryablePartialContainment{
		scriptedContainment: &scriptedContainment{},
		firstErr:            want,
		retryable:           true,
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, "bound:process", nil
		},
	}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, want) {
		t.Fatalf("Terminate() error = %v, want partial helper error %v", err, want)
	}
	first := waitForResult(t, process)
	if !errors.Is(first.ContainmentError, want) {
		t.Fatalf("first process result containment error = %v, want %v", first.ContainmentError, want)
	}
	if containment.closeCount() != 1 {
		t.Fatalf("first finalization close calls = %d, want 1", containment.closeCount())
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("FinalizeContainment() retry error = %v", err)
	}
	second := process.Wait()
	if second.ContainmentError != nil {
		t.Fatalf("second process result containment error = %v, want nil", second.ContainmentError)
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("repeated FinalizeContainment() error = %v", err)
	}
	if containment.closeCount() != 2 {
		t.Fatalf("finalization close calls = %d, want exactly 2", containment.closeCount())
	}
}

func TestFinalizeContainmentRetryRetainsPartialBaseError(t *testing.T) {
	helperErr := errors.New("partial helper cleanup failed")
	baseErr := errors.New("base containment stop failed")
	containment := &retryablePartialContainment{
		scriptedContainment: &scriptedContainment{},
		firstErr:            helperErr,
		secondErr:           baseErr,
		retryable:           true,
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, "bound:process", nil
		},
	}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, helperErr) {
		t.Fatalf("Terminate() error = %v, want helper error %v", err, helperErr)
	}
	first := waitForResult(t, process)
	if !errors.Is(first.ContainmentError, helperErr) {
		t.Fatalf("first process result containment error = %v, want %v", first.ContainmentError, helperErr)
	}
	if err := process.FinalizeContainment(); !errors.Is(err, baseErr) {
		t.Fatalf("FinalizeContainment() error = %v, want retained base error %v", err, baseErr)
	}
	second := process.Wait()
	if !errors.Is(second.ContainmentError, baseErr) || errors.Is(second.ContainmentError, helperErr) {
		t.Fatalf("second process result containment error = %v, want base only %v", second.ContainmentError, baseErr)
	}
	if err := process.FinalizeContainment(); !errors.Is(err, baseErr) {
		t.Fatalf("repeated FinalizeContainment() error = %v, want retained base error %v", err, baseErr)
	}
	if containment.closeCount() != 2 {
		t.Fatalf("finalization close calls = %d, want exactly 2", containment.closeCount())
	}
}

func TestFinalizeContainmentRetriesPartialHandleReleaseAfterHelperSuccess(t *testing.T) {
	releaseErr := errors.New("job handle release failed")
	containment := &retryablePartialContainment{
		scriptedContainment: &scriptedContainment{},
		firstErr:            releaseErr,
		secondErr:           releaseErr,
		retryable:           true,
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			return containment, "bound:process", nil
		},
	}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); !errors.Is(err, releaseErr) {
		t.Fatalf("Terminate() error = %v, want release error %v", err, releaseErr)
	}
	first := waitForResult(t, process)
	if !errors.Is(first.ContainmentError, releaseErr) {
		t.Fatalf("first process result containment error = %v, want %v", first.ContainmentError, releaseErr)
	}
	if err := process.FinalizeContainment(); !errors.Is(err, releaseErr) {
		t.Fatalf("FinalizeContainment() error = %v, want stable release error %v", err, releaseErr)
	}
	second := process.Wait()
	if !errors.Is(second.ContainmentError, releaseErr) {
		t.Fatalf("second process result containment error = %v, want %v", second.ContainmentError, releaseErr)
	}
	if err := process.FinalizeContainment(); !errors.Is(err, releaseErr) {
		t.Fatalf("repeated FinalizeContainment() error = %v, want stable release error %v", err, releaseErr)
	}
	if containment.closeCount() != 2 {
		t.Fatalf("finalization close calls = %d, want exactly 2", containment.closeCount())
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

func TestStartPersistsTypedContainmentAuthorityWithoutMarkerOnlyCommit(t *testing.T) {
	containment := &authorityContainment{
		scriptedContainment: &scriptedContainment{},
		value: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TargetPID:          0,
			TargetIdentity:     "bound:process",
			PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			JobID:              "cccccccccccccccccccccccccccccccc",
			SupervisorPID:      99,
			SupervisorIdentity: "helper:99",
		},
	}
	markerCalls := 0
	authorityCalls := 0
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			containment.value.TargetPID = process.Pid
			return containment, "bound:process", nil
		},
	}
	invocation := helperInvocation("wait")
	invocation.PersistProcess = func(pid int, identity string) error {
		markerCalls++
		if pid <= 0 || identity != "bound:process" {
			t.Fatalf("marker persistence identity = (%d, %q), want bound process", pid, identity)
		}
		return nil
	}
	invocation.PersistProcessWithAuthority = func(pid int, identity string, value *authority.Supervisor) error {
		if markerCalls != 0 {
			t.Fatal("atomic process-authority persistence ran after the legacy marker callback")
		}
		authorityCalls++
		if pid <= 0 || identity != "bound:process" || value == nil || value.Secret == "" {
			t.Fatalf("authority persistence = (%d, %q, %#v), want complete authority pair", pid, identity, value)
		}
		return nil
	}
	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if markerCalls != 0 {
		t.Fatalf("PersistProcess calls = %d, want 0 for supervisor-backed launch", markerCalls)
	}
	if authorityCalls != 1 {
		t.Fatalf("PersistProcessWithAuthority calls = %d, want 1", authorityCalls)
	}
	if err := process.Terminate(context.Background(), 0); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = waitForResult(t, process)
}

func TestStartUsesAtomicProcessAuthorityBeforeLegacyCallbacks(t *testing.T) {
	containment := &authorityContainment{
		scriptedContainment: &scriptedContainment{},
		value: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TargetIdentity:     "bound:process",
			PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			JobID:              "cccccccccccccccccccccccccccccccc",
			SupervisorPID:      99,
			SupervisorIdentity: "helper:99",
		},
	}
	var events []string
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			containment.value.TargetPID = process.Pid
			return containment, "bound:process", nil
		},
	}
	invocation := helperInvocation("wait")
	invocation.PersistProcess = func(int, string) error {
		events = append(events, "marker")
		return nil
	}
	invocation.PersistProcessAuthority = func(pid int, identity string, value *authority.Supervisor) error {
		if pid <= 0 || identity != "bound:process" || value == nil || value.Secret == "" {
			t.Fatalf("authority persistence = (%d, %q, %#v), want complete authority pair", pid, identity, value)
		}
		events = append(events, "authority")
		return nil
	}
	invocation.PersistProcessWithAuthority = func(pid int, identity string, value *authority.Supervisor) error {
		if pid <= 0 || identity != "bound:process" || value == nil || value.Secret == "" {
			t.Fatalf("atomic persistence = (%d, %q, %#v), want complete authority pair", pid, identity, value)
		}
		events = append(events, "atomic")
		return nil
	}

	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(events) != 1 || events[0] != "atomic" {
		t.Fatalf("persistence callbacks = %#v, want only atomic callback", events)
	}
	if err := process.Terminate(context.Background(), 0); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = waitForResult(t, process)
}

func TestStartResumesOnlyAfterAuthorityAndInitialLeaseBarriers(t *testing.T) {
	base := time.Now()
	now := base
	containment := &deadlineContainment{
		scriptedContainment: &scriptedContainment{},
		authority: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TargetIdentity:     "bound:process",
			PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			JobID:              "cccccccccccccccccccccccccccccccc",
			SupervisorPID:      99,
			SupervisorIdentity: "helper:99",
		},
	}
	markerPersisted := false
	authorityPersisted := false
	resumed := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(context.Context, *exec.Cmd, Invocation, *os.File, *os.File, *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid:         123,
				identity:    "bound:process",
				containment: containment,
				wait:        func() (int, error) { return 0, nil },
				kill:        func() error { return nil },
				close:       func() error { return nil },
				resume: func() error {
					if markerPersisted || !authorityPersisted || len(containment.renewals) != 1 {
						return errors.New("resume barrier was incomplete")
					}
					resumed = true
					return nil
				},
			}, nil
		},
		now: func() time.Time { return now },
	}
	invocation := Invocation{
		Program:              os.Args[0],
		InitialLeaseDeadline: 5 * time.Second,
		InitialLeaseSequence: 1,
		PersistProcess: func(int, string) error {
			markerPersisted = true
			return nil
		},
		PersistProcessAuthority: func(pid int, identity string, value *authority.Supervisor) error {
			t.Fatal("legacy authority callback was called when atomic persistence is available")
			return nil
		},
		PersistProcessWithAuthority: func(pid int, identity string, value *authority.Supervisor) error {
			authorityPersisted = pid == 123 && identity == "bound:process" && value != nil && value.Secret != ""
			return nil
		},
	}

	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !resumed {
		t.Fatal("native process was not resumed")
	}
	if markerPersisted {
		t.Fatal("PersistProcess was called for authority-capable launch")
	}
	if !authorityPersisted {
		t.Fatalf("PersistProcessWithAuthority persisted:%t, want one complete authority persistence", authorityPersisted)
	}
	if result := waitForResult(t, process); !result.Success() {
		t.Fatalf("barrier result = %#v, want successful completion", result)
	}
}

func TestStartArmsInitialLeaseFromAbsoluteDeadlineAfterStartupBarriers(t *testing.T) {
	base := time.Now()
	now := base
	containment := &deadlineContainment{
		scriptedContainment: &scriptedContainment{},
		authority: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TargetIdentity:     "bound:process",
			PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			JobID:              "cccccccccccccccccccccccccccccccc",
			SupervisorPID:      99,
			SupervisorIdentity: "helper:99",
		},
	}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			now = now.Add(time.Second)
			return containment, "bound:process", nil
		},
		now: func() time.Time { return now },
	}
	invocation := helperInvocation("wait")
	invocation.InitialLeaseDeadline = 30 * time.Second
	invocation.InitialLeaseDeadlineAt = base.Add(5 * time.Second)
	invocation.InitialLeaseSequence = 7
	markerCalls := 0
	invocation.PersistProcess = func(int, string) error {
		markerCalls++
		return nil
	}
	invocation.PersistProcessAuthority = func(int, string, *authority.Supervisor) error {
		t.Fatal("legacy authority callback was called when atomic persistence is available")
		return nil
	}
	invocation.PersistProcessWithAuthority = func(pid int, identity string, value *authority.Supervisor) error {
		if markerCalls != 0 {
			t.Fatal("atomic process-authority persistence ran after the legacy marker callback")
		}
		if pid <= 0 || identity != "bound:process" || value == nil || value.Secret == "" {
			t.Fatalf("authority persistence = (%d, %q, %#v), want complete authority pair", pid, identity, value)
		}
		now = now.Add(500 * time.Millisecond)
		return nil
	}

	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if markerCalls != 0 {
		t.Fatalf("PersistProcess calls = %d, want 0 for supervisor-backed launch", markerCalls)
	}
	if len(containment.renewals) != 1 {
		t.Fatalf("renewal calls = %d, want 1", len(containment.renewals))
	}
	if got, want := containment.renewals[0].deadline, 3500*time.Millisecond; got != want {
		t.Fatalf("armed lease deadline = %s, want %s", got, want)
	}
	if got := containment.renewals[0].sequence; got != 7 {
		t.Fatalf("armed lease sequence = %d, want 7", got)
	}
	if err := process.Terminate(context.Background(), 0); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = waitForResult(t, process)
}

func TestStartFailsClosedWhenInitialLeaseDeadlineExpiresBeforeArm(t *testing.T) {
	base := time.Now()
	now := base
	containment := &deadlineContainment{scriptedContainment: &scriptedContainment{}}
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			containment.setDefaultForce(process.Kill)
			now = now.Add(2 * time.Second)
			return containment, "bound:process", nil
		},
		now: func() time.Time { return now },
	}
	invocation := helperInvocation("wait")
	invocation.InitialLeaseDeadlineAt = base.Add(2 * time.Second)
	invocation.InitialLeaseSequence = 1

	process, err := runner.Start(context.Background(), invocation, &recordingSink{})
	if process == nil {
		t.Fatal("Start() returned nil process after failed arm")
	}
	if !errors.Is(err, errInitialLeaseDeadlineExpired) {
		t.Fatalf("Start() error = %v, want expired initial lease", err)
	}
	if len(containment.renewals) != 0 {
		t.Fatalf("renewal calls = %d, want 0 after expiry", len(containment.renewals))
	}
	result := waitForResult(t, process)
	if !result.Terminated || result.ContainmentError != nil {
		t.Fatalf("cleanup result = %#v, want fail-closed termination with successful containment cleanup", result)
	}
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

type earlyUnprovenContainment struct {
	mutex      sync.Mutex
	observed   bool
	callback   func() error
	closeGate  chan struct{}
	closeCalls int
}

func (containment *earlyUnprovenContainment) Terminate(bool) error { return nil }

func (containment *earlyUnprovenContainment) Close() error {
	containment.mutex.Lock()
	containment.closeCalls++
	gate := containment.closeGate
	containment.mutex.Unlock()
	if gate != nil {
		<-gate
	}
	return nil
}

func (containment *earlyUnprovenContainment) SetContainmentUnprovenCallback(callback func() error) error {
	containment.mutex.Lock()
	containment.callback = callback
	observed := containment.observed
	containment.mutex.Unlock()
	if observed {
		return callback()
	}
	return nil
}

func (*earlyUnprovenContainment) ContainmentCloseRetryable() bool { return true }

type retryablePartialContainment struct {
	*scriptedContainment
	closeMutex sync.Mutex
	closeCalls int
	firstErr   error
	secondErr  error
	retryable  bool
}

func (containment *retryablePartialContainment) Close() error {
	containment.closeMutex.Lock()
	defer containment.closeMutex.Unlock()
	containment.closeCalls++
	if containment.closeCalls == 1 {
		return containment.firstErr
	}
	containment.retryable = false
	return containment.secondErr
}

func (containment *retryablePartialContainment) ContainmentCloseRetryable() bool {
	containment.closeMutex.Lock()
	defer containment.closeMutex.Unlock()
	return containment.retryable
}

func (containment *retryablePartialContainment) closeCount() int {
	containment.closeMutex.Lock()
	defer containment.closeMutex.Unlock()
	return containment.closeCalls
}

type authorityContainment struct {
	*scriptedContainment
	value *authority.Supervisor
}

func (containment *authorityContainment) ContainmentAuthority() *authority.Supervisor {
	cloned := containment.value.Clone()
	return &cloned
}

type deadlineContainment struct {
	*scriptedContainment
	authority *authority.Supervisor
	renewals  []leaseRenewal
}

func (containment *deadlineContainment) ContainmentAuthorityAvailable() bool {
	return containment.authority != nil
}

type leaseRenewal struct {
	deadline time.Duration
	sequence uint64
}

func (containment *deadlineContainment) ContainmentAuthority() *authority.Supervisor {
	if containment.authority == nil {
		return nil
	}
	cloned := containment.authority.Clone()
	return &cloned
}

func (*deadlineContainment) LeaseRenewalAvailable() bool { return true }

func (containment *deadlineContainment) RenewLease(deadline time.Duration, sequence uint64) error {
	containment.renewals = append(containment.renewals, leaseRenewal{deadline: deadline, sequence: sequence})
	return nil
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
