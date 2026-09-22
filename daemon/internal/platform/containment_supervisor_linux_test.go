//go:build linux

package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

func TestLinuxSupervisorLeaseStateReplaysAndFencesGenerations(t *testing.T) {
	state := newLinuxSupervisorLeaseState()
	t.Cleanup(state.stop)

	if replay, err := state.renew(time.Minute, 1); replay || err != nil {
		t.Fatalf("initial renewal = (%v, %v), want fresh renewal", replay, err)
	}
	if replay, err := state.renew(time.Minute, 1); !replay || err != nil {
		t.Fatalf("same renewal replay = (%v, %v), want idempotent replay", replay, err)
	}
	if _, err := state.renew(2*time.Minute, 1); err == nil {
		t.Fatal("same sequence with a different deadline was accepted")
	}
	if _, err := state.renew(time.Minute, 0); err == nil {
		t.Fatal("zero sequence was accepted")
	}

	state.mu.Lock()
	firstGeneration := state.generation
	state.mu.Unlock()
	if replay, err := state.renew(time.Minute, 2); replay || err != nil {
		t.Fatalf("newer renewal = (%v, %v), want fresh renewal", replay, err)
	}
	state.mu.Lock()
	state.expiredGen = firstGeneration
	state.mu.Unlock()
	if state.takeExpiry() {
		t.Fatal("stale timer generation stopped the lease")
	}
	latched := newLinuxSupervisorLeaseState()
	t.Cleanup(latched.stop)
	if _, err := latched.renew(time.Minute, 1); err != nil {
		t.Fatalf("latched lease renewal = %v", err)
	}
	latched.mu.Lock()
	latched.expiredGen = latched.generation
	latched.stopped = true
	latched.mu.Unlock()
	if _, err := latched.renew(time.Minute, 2); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("renewal raced with latched expiry = %v, want lease-expired", err)
	}
	if !latched.takeExpiry() {
		t.Fatal("latched expiry wake was not consumed")
	}

	if _, err := state.renew(5*time.Millisecond, 3); err != nil {
		t.Fatalf("short renewal error = %v", err)
	}
	select {
	case <-state.wakeup():
		if !state.takeExpiry() {
			t.Fatal("current timer wake did not expire the lease")
		}
	case <-time.After(time.Second):
		t.Fatal("lease timer did not fire")
	}
	if _, err := state.renew(time.Second, 4); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("renewal after expiry = %v, want lease-expired", err)
	}
}

func TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary(t *testing.T) {
	if DurableSupervisorHandoffAvailable() {
		t.Fatal("Go test binary advertised production Linux supervisor dispatch")
	}
}

func TestLinuxSupervisorLeaseStateLatchesDeadlineBeforeTimerCallback(t *testing.T) {
	newExpired := func() *linuxSupervisorLeaseState {
		state := newLinuxSupervisorLeaseState()
		state.mu.Lock()
		state.armed = true
		state.sequence = 1
		state.deadline = time.Minute
		state.generation = 1
		state.deadlineAt = time.Now().Add(-time.Second)
		state.mu.Unlock()
		t.Cleanup(state.stop)
		return state
	}

	lateRenew := newExpired()
	if _, err := lateRenew.renew(time.Minute, 2); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("renewal after monotonic deadline = %v, want lease-expired", err)
	}

	lateTake := newExpired()
	if !lateTake.takeExpiry() {
		t.Fatal("takeExpiry did not latch a due deadline before timer notification")
	}

	lateResume := newExpired()
	detached := false
	if err := lateResume.resume(nil, func() error {
		detached = true
		return nil
	}); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("resume after monotonic deadline = %v, want lease-expired", err)
	}
	if detached {
		t.Fatal("resume detached a target after the monotonic lease deadline")
	}
}

func TestLinuxSupervisorLeaseStateOwnerLossLinearizesWithResume(t *testing.T) {
	ownerFirst := newLinuxSupervisorLeaseState()
	t.Cleanup(ownerFirst.stop)
	if _, err := ownerFirst.renew(time.Minute, 1); err != nil {
		t.Fatalf("owner-first renewal = %v", err)
	}
	ownerFirst.latchOwnerLost()
	detached := false
	if err := ownerFirst.resume(nil, func() error {
		detached = true
		return nil
	}); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("resume after owner latch = %v, want lease-expired", err)
	}
	if detached {
		t.Fatal("owner-first resume detached the target")
	}
	if _, err := ownerFirst.renew(time.Minute, 2); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("renewal after owner latch = %v, want lease-expired", err)
	}

	resumeFirst := newLinuxSupervisorLeaseState()
	t.Cleanup(resumeFirst.stop)
	if _, err := resumeFirst.renew(time.Minute, 1); err != nil {
		t.Fatalf("resume-first renewal = %v", err)
	}
	detachStarted := make(chan struct{})
	releaseDetach := make(chan struct{})
	resumeDone := make(chan error, 1)
	go func() {
		resumeDone <- resumeFirst.resume(nil, func() error {
			close(detachStarted)
			<-releaseDetach
			return nil
		})
	}()
	select {
	case <-detachStarted:
	case <-time.After(time.Second):
		t.Fatal("resume did not reach the detach gate")
	}
	latchDone := make(chan struct{})
	go func() {
		resumeFirst.latchOwnerLost()
		close(latchDone)
	}()
	select {
	case <-latchDone:
		t.Fatal("owner loss latched before the in-flight detach released")
	default:
	}
	close(releaseDetach)
	if err := <-resumeDone; err != nil {
		t.Fatalf("resume-first detach = %v", err)
	}
	select {
	case <-latchDone:
	case <-time.After(time.Second):
		t.Fatal("owner loss did not latch after detach released")
	}
	if _, err := resumeFirst.renew(time.Minute, 2); !errors.Is(err, ErrLinuxSupervisorLeaseExpired) {
		t.Fatalf("renewal after resume-first owner loss = %v, want lease-expired", err)
	}
}

func TestLinuxSupervisorOwnerWatchdogLatchesBeforeWakeAndDisarmDoesNotLatch(t *testing.T) {
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 2)
	watchdog := newLinuxSupervisorOwnerWatchdog(ownerRead, func() { order <- "latch" })
	if err := ownerWrite.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watchdog.ownerLost():
	case <-time.After(time.Second):
		t.Fatal("owner watchdog did not report loss")
	}
	order <- "wake"
	if got := <-order; got != "latch" {
		t.Fatalf("owner watchdog order = %q, want latch before wake", got)
	}
	watchdog.disarm()

	disarmRead, disarmWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	disarmed := false
	disarmedWatchdog := newLinuxSupervisorOwnerWatchdog(disarmRead, func() { disarmed = true })
	disarmedWatchdog.disarm()
	if err := disarmWrite.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-disarmedWatchdog.done:
	case <-time.After(time.Second):
		t.Fatal("disarmed owner watchdog did not stop")
	}
	if disarmed {
		t.Fatal("disarmed owner watchdog invoked the owner-loss latch")
	}
}

func TestLinuxSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_SUPERVISOR_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	if separator < 0 || separator > len(os.Args) {
		os.Exit(2)
	}
	if err := RunContainmentSupervisor(os.Args[separator:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	os.Exit(0)
}

func TestLinuxSupervisorHelperFirstLifecycle(t *testing.T) {
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatal(err)
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatal(err)
	}
	defer stderrRead.Close()

	command := exec.Command("/bin/sh", "-c", "printf durable")
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	var prepared, bound bool
	supervisor, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-lifecycle",
		Prepare: func(value authority.SupervisorHandoff) error {
			prepared = value.OwnerKind == authority.OwnerKindLinuxHelper && value.SupervisorPID == 0 && value.SupervisorIdentity == "" && value.TargetPID > 0 && value.TargetIdentity != "" && value.CreatorSessionID == nil
			return nil
		},
		Bind: func(value authority.SupervisorHandoff, pid int, identity string) error {
			bound = value.SupervisorPID == 0 && value.SupervisorIdentity == "" && pid > 0 && identity != ""
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StartLinuxSupervisor() error = %v", err)
	}
	if !prepared || !bound {
		t.Fatalf("handoff callbacks prepared=%v bound=%v", prepared, bound)
	}
	if supervisor == nil || supervisor.PID() <= 0 || supervisor.HelperPID() <= 0 {
		t.Fatalf("supervisor identities are incomplete: %#v", supervisor)
	}
	if err := supervisor.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RenewLease(time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Resume(); err != nil {
		t.Fatal(err)
	}
	code, err := supervisor.Wait()
	if err != nil || code != 0 {
		t.Fatalf("Wait() = (%d, %v), want (0,nil)", code, err)
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	receipt, ok := supervisor.ContainmentStopReceipt()
	if !ok || !receipt.ValidFor(*supervisor.ContainmentAuthority()) {
		t.Fatalf("stop receipt = %#v, available=%v", receipt, ok)
	}
	if err := supervisor.ReleaseContainment(); err != nil {
		t.Fatal(err)
	}
	if got := supervisor.ContainmentAuthority(); got == nil || got.StopReceipt == nil {
		t.Fatal("released supervisor lost durable stop receipt")
	}
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
}

func TestLinuxSupervisorMirrorRequiresHelperDeath(t *testing.T) {
	enableLinuxTestSubreaper(t)

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatal(err)
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatal(err)
	}
	defer stderrRead.Close()

	command := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "leader-exits-after-child")
	command.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	supervisor, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-mirror",
		Prepare:       func(authority.SupervisorHandoff) error { return nil },
		Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	leader := &linuxTestChildOwner{pid: supervisor.PID()}
	t.Cleanup(func() { leader.cleanup(t) })
	child := &linuxTestChildOwner{}
	t.Cleanup(func() { child.cleanup(t) })
	t.Cleanup(func() {
		if supervisor == nil {
			return
		}
		if supervisor.helper != nil {
			_ = supervisor.helper.Kill()
		}
		select {
		case <-supervisor.helperDone:
		case <-time.After(2 * time.Second):
		}
		if _, ok := supervisor.ContainmentStopReceipt(); !ok {
			_ = supervisor.Terminate(true)
		}
		_ = supervisor.ReleaseContainment()
	})
	if err := supervisor.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RenewLease(time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := stdoutRead.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := stdinWrite.Write([]byte{1}); err != nil {
		t.Fatalf("start target descendant = %v", err)
	}
	childLine, err := bufio.NewReader(stdoutRead).ReadString('\n')
	if err != nil {
		t.Fatalf("read target descendant PID = %v", err)
	}
	child.pid, err = strconv.Atoi(strings.TrimSpace(childLine))
	if err != nil || child.pid <= 0 {
		t.Fatalf("target descendant PID %q = %v", childLine, err)
	}
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("target descendant %d is not live: %v", child.pid, err)
	}
	childStat, err := readLinuxProcessStat(child.pid)
	if err != nil {
		t.Fatalf("read target descendant %d stat = %v", child.pid, err)
	}
	anchor := supervisor.ProcessGroupAnchor()
	if childStat.pgrp != anchor.PGRP || childStat.session != anchor.Session {
		t.Fatalf("target descendant anchor = pgrp:%d session:%d, want pgrp:%d session:%d", childStat.pgrp, childStat.session, anchor.PGRP, anchor.Session)
	}
	if err := supervisor.helper.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-supervisor.helperDone:
	case <-time.After(2 * time.Second):
		t.Fatal("helper did not exit after kill")
	}
	if err := supervisor.mirror.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
		t.Fatalf("mirror terminate before adopted-child reap = %v", err)
	}
	leader.reap(t)
	child.reap(t)
	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("mirror Terminate() error = %v", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); !ok {
		t.Fatal("mirror stop did not produce an exact receipt")
	}
	_ = supervisor.ReleaseContainment()
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
}

func TestLinuxSupervisorGroupAbsentRetryWaitsForTransientPresence(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	previousPriority := readLinuxProcessGroupPriority
	previousProof := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() {
		readLinuxProcessGroupPriority = previousPriority
		linuxSupervisorProveProcessGroupAbsent = previousProof
	})

	calls := 0
	readLinuxProcessGroupPriority = func(which, who int) (int, error) {
		if which != unix.PRIO_PGRP || who != os.Getpid() {
			t.Fatalf("getpriority arguments = (%d, %d)", which, who)
		}
		calls++
		if calls == 1 {
			return 0, nil
		}
		return 0, unix.ESRCH
	}
	linuxSupervisorProveProcessGroupAbsent = ProvePersistedProcessGroupAbsent

	deadline := time.Now().Add(250 * time.Millisecond)
	if err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), os.Getpid(), identity, deadline); err != nil {
		t.Fatalf("proveLinuxSupervisorGroupAbsentWithRetry() = %v, want success", err)
	}
	if calls != 2 {
		t.Fatalf("getpriority calls = %d, want 2", calls)
	}
}

func TestLinuxSupervisorGroupAbsentRetryRejectsExpiredDeadline(t *testing.T) {
	previous := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })

	calls := 0
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error {
		calls++
		return fmt.Errorf("%w: group still visible", errPersistedProcessGroupPresent)
	}

	deadline := time.Now().Add(-time.Nanosecond)
	err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), 123, "identity", deadline)
	if !errors.Is(err, ErrPersistedProcessGroupUnproven) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired proof error = %v, want unproven + deadline categories", err)
	}
	if calls != 0 {
		t.Fatalf("group proof calls = %d, want 0 after expired deadline", calls)
	}
}

func TestLinuxSupervisorGroupAbsentRetryDoesNotRetryNonTransientErrors(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
	}{
		{name: "identity", cause: errors.New("PID namespace identity changed")},
		{name: "permission", cause: unix.EPERM},
		{name: "context", cause: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := linuxSupervisorProveProcessGroupAbsent
			t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })

			calls := 0
			linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error {
				calls++
				return fmt.Errorf("%w: %w", ErrPersistedProcessGroupUnproven, test.cause)
			}

			err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), 123, "identity", time.Now().Add(time.Second))
			if !errors.Is(err, test.cause) || errors.Is(err, errPersistedProcessGroupPresent) {
				t.Fatalf("non-transient proof error = %v, want cause %v without transient category", err, test.cause)
			}
			if calls != 1 {
				t.Fatalf("non-transient proof calls = %d, want 1", calls)
			}
		})
	}
}

func TestLinuxSupervisorGroupAbsentRetryPropagatesCancellation(t *testing.T) {
	previous := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	linuxSupervisorProveProcessGroupAbsent = func(ctx context.Context, _ int, _ string) error {
		calls++
		cancel()
		return fmt.Errorf("%w: group still visible", errPersistedProcessGroupPresent)
	}

	err := proveLinuxSupervisorGroupAbsentWithRetry(ctx, 123, "identity", time.Now().Add(time.Second))
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errPersistedProcessGroupPresent) {
		t.Fatalf("canceled proof error = %v, want canceled + transient-presence categories", err)
	}
	if calls != 1 {
		t.Fatalf("canceled proof calls = %d, want 1", calls)
	}
}

func TestLinuxSupervisorGroupAbsentRetryRejectsLateSuccessfulProbe(t *testing.T) {
	previous := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })

	calls := 0
	linuxSupervisorProveProcessGroupAbsent = func(ctx context.Context, _ int, _ string) error {
		calls++
		<-ctx.Done()
		return nil
	}

	err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), 123, "identity", time.Now().Add(20*time.Millisecond))
	if !errors.Is(err, ErrPersistedProcessGroupUnproven) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late successful proof error = %v, want unproven + deadline categories", err)
	}
	if calls != 1 {
		t.Fatalf("late successful proof calls = %d, want 1", calls)
	}
}

func TestLinuxSupervisorMirrorProofFailureRetainsRetryableAuthority(t *testing.T) {
	previous := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error {
		return fmt.Errorf("%w: %w", ErrPersistedProcessGroupUnproven, unix.EPERM)
	}

	helperDone := make(chan struct{})
	close(helperDone)
	callbackCalls := 0
	supervisor := &linuxSupervisor{
		authority:  testLinuxSupervisorAuthority(t),
		helperDone: helperDone,
		mirror:     &processGroup{closeCompleted: true},
		pidfd:      47,
		containmentUnprovenCallback: func() error {
			callbackCalls++
			return nil
		},
	}

	err := supervisor.Terminate(true)
	if !errors.Is(err, ErrLinuxSupervisorStopUnproven) || !strings.Contains(err.Error(), "mirror proof") {
		t.Fatalf("mirror proof failure = %v, want unresolved mirror proof", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok || supervisor.receipt != nil || supervisor.stopped {
		t.Fatalf("mirror proof failure state = receipt:%v stopped:%v, want no receipt and not stopped", supervisor.receipt, supervisor.stopped)
	}
	if supervisor.ContainmentAuthority().StopReceipt != nil {
		t.Fatal("mirror proof failure installed an authority stop receipt")
	}
	if supervisor.pidfd != -1 || !supervisor.finalProofPending || !supervisor.ContainmentCloseRetryable() {
		t.Fatalf("mirror proof failure authority = pidfd:%d finalProofPending:%v retryable:%v, want closed pidfd with proof-only retry", supervisor.pidfd, supervisor.finalProofPending, supervisor.ContainmentCloseRetryable())
	}
	if callbackCalls != 1 {
		t.Fatalf("containment-unproven callback calls = %d, want 1", callbackCalls)
	}
}

func TestLinuxSupervisorMirrorProofRetryProducesReceiptAfterTransientPresence(t *testing.T) {
	previous := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() { linuxSupervisorProveProcessGroupAbsent = previous })

	calls := 0
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("%w: group still visible", errPersistedProcessGroupPresent)
		}
		return nil
	}

	helperDone := make(chan struct{})
	close(helperDone)
	supervisor := &linuxSupervisor{
		authority:  testLinuxSupervisorAuthority(t),
		helperDone: helperDone,
		mirror:     &processGroup{closeCompleted: true},
		pidfd:      47,
	}

	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("mirror proof retry = %v, want receipt success", err)
	}
	if calls != 2 {
		t.Fatalf("mirror proof calls = %d, want first transient failure plus successful retry", calls)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); !ok || supervisor.receipt == nil || !supervisor.stopped {
		t.Fatalf("mirror proof retry state = receipt:%v stopped:%v available:%v, want successful receipt", supervisor.receipt, supervisor.stopped, ok)
	}
	if supervisor.pidfd != -1 || supervisor.finalProofPending || supervisor.ContainmentCloseRetryable() {
		t.Fatalf("mirror proof retry authority = pidfd:%d finalProofPending:%v retryable:%v, want finalized state", supervisor.pidfd, supervisor.finalProofPending, supervisor.ContainmentCloseRetryable())
	}
}

func TestLinuxSupervisorReleaseAfterHelperDeathUsesLocalDurableReceipt(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	receipt := testLinuxSupervisorStopReceipt(value)
	value.StopReceipt = &receipt

	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		ownerRead.Close()
		ownerWrite.Close()
		t.Fatal(err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		ownerRead.Close()
		ownerWrite.Close()
		controlRead.Close()
		controlWrite.Close()
		t.Fatal(err)
	}
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		ownerRead.Close()
		ownerWrite.Close()
		controlRead.Close()
		controlWrite.Close()
		responseRead.Close()
		responseWrite.Close()
		t.Fatal(err)
	}
	pidfdRead, pidfdWrite, err := os.Pipe()
	if err != nil {
		ownerRead.Close()
		ownerWrite.Close()
		controlRead.Close()
		controlWrite.Close()
		responseRead.Close()
		responseWrite.Close()
		statusRead.Close()
		statusWrite.Close()
		t.Fatal(err)
	}
	pidfd, err := unix.Dup(int(pidfdRead.Fd()))
	if err != nil {
		ownerRead.Close()
		ownerWrite.Close()
		controlRead.Close()
		controlWrite.Close()
		responseRead.Close()
		responseWrite.Close()
		statusRead.Close()
		statusWrite.Close()
		pidfdRead.Close()
		pidfdWrite.Close()
		t.Fatal(err)
	}

	supervisor := &linuxSupervisor{
		authority: value,
		handoff: authority.SupervisorHandoff{
			Version:            authority.SupervisorHandoffVersion,
			LaunchToken:        value.LaunchToken,
			Secret:             value.Secret,
			OwnerKind:          value.OwnerKind,
			OwnerContext:       value.OwnerContext,
			TargetPID:          value.TargetPID,
			TargetIdentity:     value.TargetIdentity,
			PipeToken:          value.PipeToken,
			JobID:              value.JobID,
			SupervisorPID:      value.SupervisorPID,
			SupervisorIdentity: value.SupervisorIdentity,
			StopReceipt:        &receipt,
		},
		receipt:      &receipt,
		helperDone:   make(chan struct{}),
		ownerWrite:   ownerWrite,
		controlWrite: controlWrite,
		responseRead: responseRead,
		statusRead:   statusRead,
		pidfd:        pidfd,
	}
	close(supervisor.helperDone)
	previousLstat := linuxSupervisorLstat
	previousIdentity := linuxSupervisorProcessIdentity
	previousGroupProof := linuxSupervisorProveProcessGroupAbsent
	linuxSupervisorLstat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	linuxSupervisorProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error { return nil }
	t.Cleanup(func() {
		linuxSupervisorLstat = previousLstat
		linuxSupervisorProcessIdentity = previousIdentity
		linuxSupervisorProveProcessGroupAbsent = previousGroupProof
		for _, file := range []*os.File{ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite, statusRead, statusWrite, pidfdRead, pidfdWrite} {
			_ = file.Close()
		}
		if supervisor.pidfd >= 0 {
			_ = unix.Close(supervisor.pidfd)
		}
	})

	if err := supervisor.ReleaseContainment(); err != nil {
		t.Fatalf("ReleaseContainment() after helper death = %v, want local receipt release", err)
	}
	if !supervisor.released || !supervisor.closed || supervisor.pidfd != -1 || supervisor.ownerWrite != nil || supervisor.controlWrite != nil || supervisor.responseRead != nil || supervisor.statusRead != nil {
		t.Fatalf("released supervisor state = released:%v closed:%v pidfd:%d owner:%v control:%v response:%v status:%v, want released,closed,-1,nil,nil,nil,nil", supervisor.released, supervisor.closed, supervisor.pidfd, supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead)
	}
	got, ok := supervisor.ContainmentStopReceipt()
	if !ok || !got.ValidFor(value) {
		t.Fatalf("local stop receipt = %#v, available=%v, want valid durable receipt after release", got, ok)
	}
	if authorityValue := supervisor.ContainmentAuthority(); authorityValue == nil || authorityValue.StopReceipt == nil || !authorityValue.StopReceipt.ValidFor(*authorityValue) {
		t.Fatalf("released authority = %#v, want receipt retained for clear", authorityValue)
	}
	if supervisor.ContainmentCloseRetryable() {
		t.Fatal("released supervisor remained retryable")
	}
	if err := supervisor.ReleaseContainment(); err != nil {
		t.Fatalf("repeated ReleaseContainment() = %v, want nil", err)
	}
}

func TestLinuxSupervisorMirrorDescendantScanFailureDoesNotProduceReceipt(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	pid := value.TargetPID
	anchor := LinuxProcessGroupAnchor{PID: pid, PGRP: int64(pid), Session: 77, StartTime: 456}

	pidfdRead, pidfdWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pidfd, err := unix.Dup(int(pidfdRead.Fd()))
	if err != nil {
		pidfdRead.Close()
		pidfdWrite.Close()
		t.Fatal(err)
	}
	var supervisor *linuxSupervisor
	t.Cleanup(func() {
		_ = pidfdRead.Close()
		_ = pidfdWrite.Close()
		if supervisor.pidfd >= 0 {
			_ = unix.Close(supervisor.pidfd)
		}
	})

	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	previousSignal := sendPIDFDSignal
	readCalls := 0
	wantScan := errors.New("descendant scan failed")
	readLinuxProcessStat = func(lookedUpPID int) (linuxProcessStat, error) {
		if lookedUpPID != pid {
			return linuxProcessStat{}, fmt.Errorf("unexpected process stat pid %d", lookedUpPID)
		}
		readCalls++
		if readCalls <= 3 {
			return linuxProcessStat{pid: pid, pgrp: int64(pid), session: anchor.Session, startTime: anchor.StartTime}, nil
		}
		if readCalls == 4 {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{}, wantScan
	}
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	var signals []unix.Signal
	sendPIDFDSignal = func(fd int, signal unix.Signal, _ *unix.Siginfo, flags int) error {
		if fd != pidfd {
			t.Errorf("mirror signal fd = %d, want %d", fd, pidfd)
		}
		if flags != unix.PIDFD_SIGNAL_PROCESS_GROUP {
			t.Errorf("mirror signal flags = %d, want PIDFD_SIGNAL_PROCESS_GROUP", flags)
		}
		signals = append(signals, signal)
		if signal == unix.SIGKILL {
			return nil
		}
		return unix.ESRCH
	}
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
		sendPIDFDSignal = previousSignal
	})

	mirror := &processGroup{pid: pid, fd: pidfd, anchor: anchor.private(), anchorCaptured: true}
	mirror.startDescendantMonitor()
	if err := mirror.waitForInitialDescendantScan(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("mirror initial scan = %v", err)
	}
	supervisor = &linuxSupervisor{authority: value, anchor: anchor, helperDone: make(chan struct{}), pidfd: pidfd, mirror: mirror}
	close(supervisor.helperDone)

	err = supervisor.Terminate(true)
	if !errors.Is(err, ErrLinuxSupervisorStopUnproven) {
		t.Fatalf("mirror Terminate() error = %v, want unresolved descendant scan", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok {
		t.Fatal("mirror descendant scan failure produced a successful stop receipt")
	}
	if supervisor.stopped {
		t.Fatal("mirror descendant scan failure marked supervisor stopped")
	}
	if supervisor.pidfd != pidfd || !supervisor.ContainmentCloseRetryable() {
		t.Fatalf("mirror failure state = pidfd:%d retryable:%v, want retained pidfd %d and retryable", supervisor.pidfd, supervisor.ContainmentCloseRetryable(), pidfd)
	}
	if len(signals) != 2 || signals[0] != unix.SIGKILL || signals[1] != 0 {
		t.Fatalf("mirror signals = %#v, want [SIGKILL 0] proof sequence", signals)
	}
}

func TestLinuxSupervisorResponseDropGateOffPreservesResponse(t *testing.T) {
	t.Setenv(linuxSupervisorProductionWitnessEnv, "0")
	t.Setenv(linuxSupervisorDropResponseOnceEnv, linuxSupervisorOpStop)
	supervisor, responseDone, releaseResponse := newLinuxSupervisorRequestHarness(t)
	releaseResponse()

	response, err := supervisor.request(linuxSupervisorOpStop, 0, 0)
	if err != nil || response.Status != "ok" {
		t.Fatalf("gate-off request = (%#v, %v), want response", response, err)
	}
	if err := <-responseDone; err != nil {
		t.Fatalf("gate-off response writer = %v", err)
	}
	if supervisor.responseDropTriggered || supervisor.responseRead == nil {
		t.Fatalf("gate-off transport state = triggered:%v response:%v, want false and open", supervisor.responseDropTriggered, supervisor.responseRead)
	}
}

func TestLinuxSupervisorResponseDropWrongOperationPreservesResponse(t *testing.T) {
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorDropResponseOnceEnv, "bogus")
	supervisor, responseDone, releaseResponse := newLinuxSupervisorRequestHarness(t)
	releaseResponse()

	response, err := supervisor.request(linuxSupervisorOpStop, 0, 0)
	if err != nil || response.Status != "ok" {
		t.Fatalf("wrong-operation request = (%#v, %v), want response", response, err)
	}
	if err := <-responseDone; err != nil {
		t.Fatalf("wrong-operation response writer = %v", err)
	}
	if supervisor.responseDropTriggered || supervisor.responseRead == nil {
		t.Fatalf("wrong-operation transport state = triggered:%v response:%v, want false and open", supervisor.responseDropTriggered, supervisor.responseRead)
	}
}

func TestLinuxSupervisorResponseDropStopFiresOnceAndWritesMarker(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "response-drop-fired")
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorDropResponseOnceEnv, linuxSupervisorOpStop)
	t.Setenv(linuxSupervisorDropResponseMarkerEnv, markerPath)
	supervisor, responseDone, releaseResponse := newLinuxSupervisorRequestHarness(t)

	if _, err := supervisor.request(linuxSupervisorOpStop, 0, 0); err == nil {
		t.Fatal("response-drop request error = nil, want closed response transport")
	}
	releaseResponse()
	if err := <-responseDone; err == nil {
		t.Fatal("response writer error = nil, want closed client transport")
	}
	contents, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read response-drop marker: %v", err)
	}
	if string(contents) != "fired\n" {
		t.Fatalf("response-drop marker = %q, want fired marker", contents)
	}
	if !supervisor.responseDropTriggered || supervisor.responseRead != nil {
		t.Fatalf("response-drop transport state = triggered:%v response:%v, want true,nil", supervisor.responseDropTriggered, supervisor.responseRead)
	}
	supervisor.dropResponseTransportOnce(linuxSupervisorOpStop)
	if !supervisor.responseDropTriggered || supervisor.responseRead != nil {
		t.Fatal("repeated response-drop invocation changed once-only state")
	}
}

func TestLinuxSupervisorResponseDropReleasePreservesRecoveryEndpoint(t *testing.T) {
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorDropResponseOnceEnv, linuxSupervisorOpRelease)
	supervisor, responseDone, releaseResponse := newLinuxSupervisorRequestHarness(t)

	if _, err := supervisor.request(linuxSupervisorOpRelease, 0, 0); err == nil {
		t.Fatal("release response-drop request error = nil, want closed response transport")
	}
	releaseResponse()
	if err := <-responseDone; err == nil {
		t.Fatal("release response writer error = nil, want closed client transport")
	}
	if !supervisor.responseDropTriggered || supervisor.responseRead != nil {
		t.Fatalf("release response-drop transport state = triggered:%v response:%v, want true,nil", supervisor.responseDropTriggered, supervisor.responseRead)
	}
	if supervisor.controlWrite == nil || supervisor.ownerWrite == nil {
		t.Fatal("response drop closed control or owner transport, losing recovery endpoint")
	}
	if !strings.Contains(supervisor.authority.OwnerContext, "|endpoint=") {
		t.Fatalf("recovery owner context = %q, want endpoint fence", supervisor.authority.OwnerContext)
	}
}

func newLinuxSupervisorRequestHarness(t *testing.T) (*linuxSupervisor, <-chan error, func()) {
	t.Helper()
	value := testLinuxSupervisorAuthority(t)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		controlRead.Close()
		controlWrite.Close()
		t.Fatal(err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		controlRead.Close()
		controlWrite.Close()
		responseRead.Close()
		responseWrite.Close()
		t.Fatal(err)
	}
	supervisor := &linuxSupervisor{
		authority:      value,
		helperDone:     make(chan struct{}),
		ownerWrite:     ownerWrite,
		controlWrite:   controlWrite,
		responseRead:   responseRead,
		responseReader: bufio.NewReader(responseRead),
	}
	responseDone := make(chan error, 1)
	responseGate := make(chan struct{})
	var responseGateOnce sync.Once
	releaseResponse := func() { responseGateOnce.Do(func() { close(responseGate) }) }
	go func() {
		requestReader := bufio.NewReader(controlRead)
		var request linuxSupervisorRequest
		if err := readLinuxSupervisorFrame(requestReader, &request); err != nil {
			responseDone <- err
			return
		}
		<-responseGate
		responseDone <- writeLinuxSupervisorFrame(responseWrite, linuxSupervisorResponse{
			Version:            linuxSupervisorProtocolVersion,
			Operation:          request.Operation,
			Status:             "ok",
			LaunchToken:        value.LaunchToken,
			Sequence:           request.Sequence,
			TargetPID:          value.TargetPID,
			TargetIdentity:     value.TargetIdentity,
			OwnerKind:          value.OwnerKind,
			OwnerContext:       value.OwnerContext,
			SupervisorPID:      value.SupervisorPID,
			SupervisorIdentity: value.SupervisorIdentity,
			Token:              value.PipeToken,
			JobID:              value.JobID,
		})
	}()
	t.Cleanup(func() {
		releaseResponse()
		for _, file := range []*os.File{controlRead, controlWrite, responseRead, responseWrite, ownerRead, ownerWrite} {
			_ = file.Close()
		}
	})
	return supervisor, responseDone, releaseResponse
}

func TestLinuxSupervisorScanFailureInjectionGateOffPreservesReader(t *testing.T) {
	triggerPath := filepath.Join(t.TempDir(), "trigger")
	firedPath := filepath.Join(t.TempDir(), "fired")
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(linuxSupervisorProductionWitnessEnv, "0")
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)

	called := 0
	group := &processGroup{
		monitorReadStat: func(int) (linuxProcessStat, error) { return linuxProcessStat{}, nil },
		monitorReadChildren: func(int) ([]int, error) {
			called++
			return []int{7}, nil
		},
	}
	restore := installLinuxSupervisorScanFailureInjection(group, os.Getpid(), "unused")
	t.Cleanup(restore)
	children, err := group.monitorReadChildren(os.Getpid())
	if err != nil || len(children) != 1 || called != 1 {
		t.Fatalf("gate-off reader = (%v, %v), calls=%d, want real reader", children, err, called)
	}
	if _, err := os.Stat(firedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-off fired marker = %v, want absent", err)
	}
}

func TestLinuxSupervisorScanFailureInjectionConfigurationFailsClosed(t *testing.T) {
	t.Setenv(linuxSupervisorProductionWitnessEnv, "0")
	t.Setenv(linuxSupervisorScanFailureModeEnv, "unsupported")
	if err := validateLinuxSupervisorScanFailureEnv(); err != nil {
		t.Fatalf("master gate off validation = %v, want nil", err)
	}

	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	if err := validateLinuxSupervisorScanFailureEnv(); err == nil {
		t.Fatal("unsupported scan-failure mode validation = nil, want error")
	}
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	if err := validateLinuxSupervisorScanFailureEnv(); err == nil {
		t.Fatal("missing scan-failure marker paths validation = nil, want error")
	}
}

func TestLinuxSupervisorScanFailureInjectionTriggerBeforeAndAfterInstall(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		createTrigger bool
	}{
		{name: "trigger before install", createTrigger: true},
		{name: "trigger after install", createTrigger: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			triggerPath := filepath.Join(directory, "trigger")
			firedPath := filepath.Join(directory, "fired")
			t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
			t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
			t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
			t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)
			if test.createTrigger {
				if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			calls := 0
			group := &processGroup{
				monitorReadStat: func(int) (linuxProcessStat, error) { return linuxProcessStat{}, nil },
				monitorReadChildren: func(int) ([]int, error) {
					calls++
					return []int{7}, nil
				},
			}
			restore := installLinuxSupervisorScanFailureInjection(group, os.Getpid(), identity)
			t.Cleanup(restore)
			if test.createTrigger {
				children, err := group.monitorReadChildren(os.Getpid())
				if !errors.Is(err, errLinuxSupervisorInjectedDescendantScanFailure) || children != nil {
					t.Fatalf("pre-installed trigger reader = (%v, %v), want distinct scan failure", children, err)
				}
			} else {
				if children, err := group.monitorReadChildren(os.Getpid()); err != nil || len(children) != 1 || calls != 1 {
					t.Fatalf("pre-trigger reader = (%v, %v), calls=%d, want real reader", children, err, calls)
				}
				if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			children, err := group.monitorReadChildren(os.Getpid())
			if test.createTrigger {
				if err != nil || len(children) != 1 || calls != 1 {
					t.Fatalf("restored reader = (%v, %v), calls=%d, want real reader", children, err, calls)
				}
			} else if !errors.Is(err, errLinuxSupervisorInjectedDescendantScanFailure) || children != nil {
				t.Fatalf("post-installed trigger reader = (%v, %v), want distinct scan failure", children, err)
			}
			marker, err := os.ReadFile(firedPath)
			if err != nil || string(marker) != "fired\n" {
				t.Fatalf("fired marker = (%q, %v), want fired marker", marker, err)
			}
			children, err = group.monitorReadChildren(os.Getpid())
			if err != nil || len(children) != 1 || calls != 2 {
				t.Fatalf("second restored reader = (%v, %v), calls=%d, want real reader once", children, err, calls)
			}
		})
	}
}

func TestLinuxSupervisorScanFailureInjectionIsStickyAndProducesNoReceipt(t *testing.T) {
	identity := "linux:v2:scan-failure-target"
	targetPID := 1234
	directory := t.TempDir()
	triggerPath := filepath.Join(directory, "trigger")
	firedPath := filepath.Join(directory, "fired")
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanFailureChildren)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)

	previousIdentity := readProcessIdentity
	t.Cleanup(func() { readProcessIdentity = previousIdentity })
	readProcessIdentity = func(pid int) (string, error) {
		if pid == targetPID {
			return identity, nil
		}
		return "", os.ErrNotExist
	}
	anchor := linuxProcessGroupAnchor{pid: targetPID, pgrp: int64(targetPID), session: 77, startTime: 456}
	previousStat := readLinuxProcessStat
	previousChildren := readLinuxProcessChildren
	readLinuxProcessStat = func(pid int) (linuxProcessStat, error) {
		if pid != targetPID {
			return linuxProcessStat{}, os.ErrNotExist
		}
		return linuxProcessStat{pid: targetPID, pgrp: int64(targetPID), session: anchor.session, startTime: anchor.startTime}, nil
	}
	readLinuxProcessChildren = func(int) ([]int, error) { return nil, nil }
	t.Cleanup(func() {
		readLinuxProcessStat = previousStat
		readLinuxProcessChildren = previousChildren
	})
	group := &processGroup{
		pid:            targetPID,
		fd:             47,
		anchor:         anchor,
		anchorCaptured: true,
	}
	group.startDescendantMonitor()
	if err := group.waitForInitialDescendantScan(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("initial scan = %v", err)
	}
	restore := installLinuxSupervisorScanFailureInjection(group, targetPID, identity)
	t.Cleanup(restore)

	result, err := group.requestDescendantMonitorScan(time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("triggered scan request = %v", err)
	}
	if !errors.Is(result.scanErr, errLinuxSupervisorInjectedDescendantScanFailure) {
		t.Fatalf("triggered scan result = %#v, want injected failure", result)
	}
	if !group.descendantScanLost {
		t.Fatal("scan failure did not latch sticky descendant uncertainty")
	}

	helperDone := make(chan struct{})
	close(helperDone)
	value := testLinuxSupervisorAuthority(t)
	supervisor := &linuxSupervisor{authority: value, helperDone: helperDone, mirror: group, pidfd: 47}
	if err := supervisor.Terminate(true); !errors.Is(err, ErrLinuxSupervisorStopUnproven) {
		t.Fatalf("sticky mirror Terminate() = %v, want unresolved stop", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok || supervisor.stopped {
		t.Fatal("sticky scan failure produced a stop receipt")
	}
	if !supervisor.ContainmentCloseRetryable() {
		t.Fatal("sticky scan failure did not retain retryable authority")
	}
	if marker, err := os.ReadFile(firedPath); err != nil || string(marker) != "fired\n" {
		t.Fatalf("sticky fired marker = (%q, %v), want fired marker", marker, err)
	}
}

func TestLinuxSupervisorControlEOFFailsClosedBeforeOwnerLoss(t *testing.T) {
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatal(err)
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatal(err)
	}
	defer stderrRead.Close()

	command := exec.Command("/bin/sh", "-c", "sleep 10")
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	supervisor, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-recovery",
		Prepare:       func(authority.SupervisorHandoff) error { return nil },
		Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RenewLease(time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Resume(); err != nil {
		t.Fatal(err)
	}
	value := supervisor.ContainmentAuthority()
	if value == nil {
		t.Fatal("missing Linux supervisor authority")
	}
	// Keep the owner writer open so its watchdog cannot publish owner loss.
	// Closing control first deterministically delivers EOF to requestEvents.
	_ = supervisor.controlWrite.Close()
	var receipt authority.StopReceipt
	var stopErr error
	for attempt := 0; attempt < 8; attempt++ {
		receipt, stopErr = StopPersistedLinuxSupervisor(*value)
		if stopErr == nil {
			break
		}
		runtime.Gosched()
	}
	if stopErr != nil {
		t.Fatalf("StopPersistedLinuxSupervisor() after control EOF = %v", stopErr)
	}
	if err := proveLinuxSupervisorTargetAbsent(value.TargetPID, value.TargetIdentity); err != nil {
		t.Fatalf("control EOF left target live: %v", err)
	}
	if err := linuxSupervisorProveProcessGroupAbsent(context.Background(), value.TargetPID, value.TargetIdentity); err != nil {
		t.Fatalf("control EOF left process group unproven: %v", err)
	}
	select {
	case <-supervisor.helperDone:
		t.Fatal("helper exited before recovery release")
	default:
	}
	value.StopReceipt = &receipt
	if err := ReleasePersistedLinuxSupervisor(*value); err != nil {
		t.Fatal(err)
	}
	// The recovery release has completed; close the still-open owner writer so
	// the watchdog's deferred shutdown cannot extend the helper exit fence.
	_ = supervisor.ownerWrite.Close()
	select {
	case <-supervisor.helperDone:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit after recovery release")
	}
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
}

func TestProvePreparedSupervisorAbortedRejectsLiveObjects(t *testing.T) {
	tests := []struct {
		name            string
		endpointPresent bool
		targetPresent   bool
		groupPresent    bool
		want            string
	}{
		{name: "endpoint", endpointPresent: true, want: "endpoint"},
		{name: "target", targetPresent: true, want: "target is still present"},
		{name: "group", groupPresent: true, want: "process group absent"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			endpoint := filepath.Join(t.TempDir(), "supervisor.sock")
			if test.endpointPresent {
				if err := os.WriteFile(endpoint, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			handoff := testLinuxSupervisorUnboundHandoff(endpoint)
			previousIdentity := linuxSupervisorProcessIdentity
			previousGroupProof := linuxSupervisorProveProcessGroupAbsent
			t.Cleanup(func() {
				linuxSupervisorProcessIdentity = previousIdentity
				linuxSupervisorProveProcessGroupAbsent = previousGroupProof
			})
			linuxSupervisorProcessIdentity = func(int) (string, error) {
				if test.targetPresent {
					return handoff.TargetIdentity, nil
				}
				return "", os.ErrNotExist
			}
			linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error {
				if test.groupPresent {
					return errors.New("process group remains present")
				}
				return nil
			}

			_, err := ProvePreparedSupervisorAborted(handoff, func() error { return nil })
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ProvePreparedSupervisorAborted() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestProvePreparedSupervisorAbortedReturnsExactProof(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "supervisor.sock")
	handoff := testLinuxSupervisorUnboundHandoff(endpoint)
	previousIdentity := linuxSupervisorProcessIdentity
	previousGroupProof := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() {
		linuxSupervisorProcessIdentity = previousIdentity
		linuxSupervisorProveProcessGroupAbsent = previousGroupProof
	})
	linuxSupervisorProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error { return nil }

	proof, err := ProvePreparedSupervisorAborted(handoff, func() error { return nil })
	if err != nil {
		t.Fatalf("ProvePreparedSupervisorAborted() error = %v", err)
	}
	if err := proof.Validate(); err != nil || !proof.ValidFor(handoff) {
		t.Fatalf("abort proof = %#v, validate=%v, want exact handoff proof", proof, err)
	}
}

func TestReleasePersistedLinuxSupervisorAllowsExactDeadOwnerProof(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	value.TargetPID = os.Getpid() + 100001
	value.TargetIdentity = "linux:v2:dead-target"
	value.StopReceipt = func() *authority.StopReceipt {
		receipt := testLinuxSupervisorStopReceipt(value)
		return &receipt
	}()

	previousLstat := linuxSupervisorLstat
	previousIdentity := linuxSupervisorProcessIdentity
	previousGroupProof := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() {
		linuxSupervisorLstat = previousLstat
		linuxSupervisorProcessIdentity = previousIdentity
		linuxSupervisorProveProcessGroupAbsent = previousGroupProof
	})
	linuxSupervisorLstat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	linuxSupervisorProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error { return nil }

	if err := ReleasePersistedLinuxSupervisor(value); err != nil {
		t.Fatalf("ReleasePersistedLinuxSupervisor() = %v, want exact dead-owner proof", err)
	}
}

func TestStopPersistedLinuxSupervisorReconstructsReceiptAfterDeadHelper(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	value.TargetPID = os.Getpid() + 100001
	value.TargetIdentity = "linux:v2:dead-target"
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("stale endpoint setup = %v, want socket pathname retained", err)
	}

	previousIdentity := linuxSupervisorProcessIdentity
	previousGroupProof := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() {
		linuxSupervisorProcessIdentity = previousIdentity
		linuxSupervisorProveProcessGroupAbsent = previousGroupProof
		_ = os.Remove(endpoint)
	})
	linuxSupervisorProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error { return nil }

	receipt, err := StopPersistedLinuxSupervisor(value)
	if err != nil {
		t.Fatalf("StopPersistedLinuxSupervisor() after dead helper = %v", err)
	}
	if !receipt.ValidFor(value) {
		t.Fatalf("reconstructed receipt = %#v, want exact authority-bound receipt", receipt)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("stop fallback retired endpoint early: %v", err)
	}

	value.StopReceipt = &receipt
	if err := ReleasePersistedLinuxSupervisor(value); err != nil {
		t.Fatalf("ReleasePersistedLinuxSupervisor() after reconstructed receipt = %v", err)
	}
	if _, err := os.Lstat(endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale endpoint after release = %v, want removed", err)
	}
}

func TestReleasePersistedLinuxSupervisorRetiresStaleEndpoint(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	receipt := testLinuxSupervisorStopReceipt(value)
	value.StopReceipt = &receipt
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("stale endpoint setup = %v, want socket pathname retained", err)
	}

	previousIdentity := linuxSupervisorProcessIdentity
	previousGroupProof := linuxSupervisorProveProcessGroupAbsent
	t.Cleanup(func() {
		linuxSupervisorProcessIdentity = previousIdentity
		linuxSupervisorProveProcessGroupAbsent = previousGroupProof
		_ = os.Remove(endpoint)
	})
	linuxSupervisorProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
	linuxSupervisorProveProcessGroupAbsent = func(context.Context, int, string) error { return nil }

	if err := ReleasePersistedLinuxSupervisor(value); err != nil {
		t.Fatalf("ReleasePersistedLinuxSupervisor() = %v, want stale endpoint retirement", err)
	}
	if _, err := os.Lstat(endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale endpoint remains: %v", err)
	}
}

func testLinuxSupervisorUnboundHandoff(endpoint string) authority.SupervisorHandoff {
	return authority.SupervisorHandoff{
		Version:        authority.SupervisorHandoffVersion,
		LaunchToken:    strings.Repeat("a", authority.TokenBytes*2),
		Secret:         strings.Repeat("b", authority.SecretBytes*2),
		OwnerKind:      authority.OwnerKindLinuxHelper,
		OwnerContext:   "linux-helper:test|endpoint=" + endpoint,
		TargetPID:      1234,
		TargetIdentity: "linux:v2:test-target",
		PipeToken:      strings.Repeat("c", authority.TokenBytes*2),
		JobID:          strings.Repeat("d", authority.TokenBytes*2),
	}
}

func testLinuxSupervisorAuthority(t *testing.T) authority.Supervisor {
	t.Helper()
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	value := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		LaunchToken:        strings.Repeat("b", authority.TokenBytes*2),
		OwnerKind:          authority.OwnerKindLinuxHelper,
		OwnerContext:       "linux-helper:test-local-owner|endpoint=" + filepath.Join(t.TempDir(), "supervisor.sock"),
		TargetPID:          os.Getpid(),
		TargetIdentity:     identity,
		PipeToken:          strings.Repeat("c", authority.TokenBytes*2),
		JobID:              strings.Repeat("d", authority.TokenBytes*2),
		SupervisorPID:      os.Getpid() + 100000,
		SupervisorIdentity: "linux-helper:test-supervisor",
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("synthetic Linux supervisor authority = %v", err)
	}
	return value
}

func testLinuxSupervisorStopReceipt(value authority.Supervisor) authority.StopReceipt {
	return authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		ActiveProcesses:    0,
	}
}
