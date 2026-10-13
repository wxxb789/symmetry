//go:build linux

package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestWaitForLinuxSupervisorTargetReturnsWhenComplete(t *testing.T) {
	waitDone := make(chan struct{})
	close(waitDone)

	if err := waitForLinuxSupervisorTarget(waitDone, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("waitForLinuxSupervisorTarget() = %v, want nil", err)
	}
}

func TestWaitForLinuxSupervisorTargetBoundsDeadline(t *testing.T) {
	waitDone := make(chan struct{})
	started := time.Now()
	err := waitForLinuxSupervisorTarget(waitDone, started.Add(25*time.Millisecond))
	if !errors.Is(err, ErrLinuxSupervisorStopUnproven) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForLinuxSupervisorTarget() error = %v, want unresolved deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitForLinuxSupervisorTarget() took %v after deadline, want bounded return", elapsed)
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

func linuxSupervisorTestProcessHasPIDFDFor(processPID, targetPID int) (bool, error) {
	if processPID <= 0 || targetPID <= 0 {
		return false, errors.New("Linux supervisor pidfd witness requires positive PIDs")
	}
	fdInfoDirectory := filepath.Join("/proc", strconv.Itoa(processPID), "fdinfo")
	entries, err := os.ReadDir(fdInfoDirectory)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(fdInfoDirectory, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(contents), "\n") {
			field, value, ok := strings.Cut(line, ":")
			if !ok || field != "Pid" {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(value))
			if err == nil && pid == targetPID {
				return true, nil
			}
		}
	}
	return false, nil
}

func TestLinuxSupervisorReceivedFDIsCloseOnExecAndRejectsExtras(t *testing.T) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	sender := os.NewFile(uintptr(pair[0]), "linux-supervisor-fd-test-sender")
	receiver := os.NewFile(uintptr(pair[1]), "linux-supervisor-fd-test-receiver")
	t.Cleanup(func() {
		_ = sender.Close()
		_ = receiver.Close()
	})
	pipeRead, pipeWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = pipeRead.Close()
		_ = pipeWrite.Close()
	})

	if err := sendLinuxSupervisorFD(sender, int(pipeRead.Fd())); err != nil {
		t.Fatalf("send one descriptor: %v", err)
	}
	received, err := receiveLinuxSupervisorFD(receiver)
	if err != nil {
		t.Fatalf("receive one descriptor: %v", err)
	}
	defer unix.Close(received)
	flags, err := unix.FcntlInt(uintptr(received), unix.F_GETFD, 0)
	if err != nil {
		t.Fatalf("read received descriptor flags: %v", err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("received descriptor is not close-on-exec")
	}

	beforeEntries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("count descriptors before extra-rights transfer: %v", err)
	}
	oob := unix.UnixRights(int(pipeRead.Fd()), int(pipeWrite.Fd()))
	if _, err := unix.SendmsgN(int(sender.Fd()), []byte{1}, oob, nil, 0); err != nil {
		t.Fatalf("send unexpected extra descriptors: %v", err)
	}
	if extra, err := receiveLinuxSupervisorFD(receiver); err == nil {
		_ = unix.Close(extra)
		t.Fatal("receive accepted more than one descriptor")
	}
	afterEntries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("count descriptors after extra-rights transfer: %v", err)
	}
	if len(afterEntries) != len(beforeEntries) {
		t.Fatalf("open descriptor count after rejected transfer = %d, before = %d; received extras were not all closed", len(afterEntries), len(beforeEntries))
	}
}

func TestLinuxSupervisorMetadataSocketTargetWitness(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_SUPERVISOR_TARGET_FD_WITNESS") != "1" {
		return
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		fmt.Fprintln(os.Stdout, "SYMMETRY_METADATA_SOCKET_ACCESSIBLE=unknown")
		return
	}
	accessible := false
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd < 3 {
			continue
		}
		domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
		if err != nil || domain != unix.AF_UNIX {
			continue
		}
		socketType, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
		if err != nil || socketType != unix.SOCK_STREAM {
			continue
		}
		accessible = true
		_, _ = unix.SendmsgN(fd, []byte("metadata-fd-witness"), nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
	}
	fmt.Fprintf(os.Stdout, "SYMMETRY_METADATA_SOCKET_ACCESSIBLE=%t\n", accessible)
	if targetPIDText := os.Getenv("SYMMETRY_LINUX_SUPERVISOR_TARGET_PIDFD_WITNESS"); targetPIDText != "" {
		targetPID, err := strconv.Atoi(targetPIDText)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid target pidfd witness PID")
			fmt.Fprintln(os.Stdout, "SYMMETRY_OLD_PIDFD_ACCESSIBLE=unknown")
		} else {
			inherited, err := linuxSupervisorTestProcessHasPIDFDFor(os.Getpid(), targetPID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "inspect target pidfds: %v\n", err)
				fmt.Fprintln(os.Stdout, "SYMMETRY_OLD_PIDFD_ACCESSIBLE=unknown")
			} else {
				fmt.Fprintf(os.Stdout, "SYMMETRY_OLD_PIDFD_ACCESSIBLE=%t\n", inherited)
			}
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestLinuxSupervisorTargetCannotRetainMetadataSocketAfterExec(t *testing.T) {
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutRead.Close()
	defer stdoutWrite.Close()

	command := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorMetadataSocketTargetWitness$", "--")
	command.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_TARGET_FD_WITNESS=1")
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = os.Stderr
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	var (
		scanMu            sync.Mutex
		helperScanUpdates []authority.SupervisorScanUpdate
		supervisor        *linuxSupervisor
	)
	supervisor, err = StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-metadata-fd-inheritance",
		Prepare: func(value authority.SupervisorHandoff) error {
			if update := value.ScanUpdate; update != nil && update.Owner == authority.SupervisorScanOwnerHelper {
				scanMu.Lock()
				helperScanUpdates = append(helperScanUpdates, *update)
				scanMu.Unlock()
			}
			return nil
		},
		Bind: func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("StartLinuxSupervisor() error = %v", err)
	}
	t.Cleanup(func() {
		_ = stdinWrite.Close()
		if _, ok := supervisor.ContainmentStopReceipt(); !ok {
			if err := supervisor.Close(); err != nil {
				if supervisor.helper != nil {
					_ = supervisor.helper.Kill()
				}
				select {
				case <-supervisor.helperDone:
				case <-time.After(2 * time.Second):
				}
				if err := supervisor.Terminate(true); err != nil {
					t.Errorf("cleanup Linux supervisor after witness failure: %v", err)
				}
			}
		}
		if _, ok := supervisor.ContainmentStopReceipt(); ok {
			if err := supervisor.ReleaseContainment(); err != nil {
				t.Errorf("release Linux supervisor after witness: %v", err)
			}
		}
	})
	if err := stdinRead.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stdoutWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RenewLease(30*time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Resume(); err != nil {
		t.Fatal(err)
	}
	output := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := bufio.NewReader(stdoutRead).ReadString('\n')
		output <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()
	var witnessLine string
	select {
	case result := <-output:
		if result.err != nil {
			t.Errorf("read target descriptor witness: %v", result.err)
		}
		witnessLine = result.line
	case <-time.After(5 * time.Second):
		t.Error("target did not report its inherited socket descriptors")
	}
	if witnessLine != "SYMMETRY_METADATA_SOCKET_ACCESSIBLE=false\n" {
		t.Errorf("target metadata socket witness = %q, want no accessible socket", witnessLine)
	}
	if err := stdinWrite.Close(); err != nil {
		t.Errorf("release target witness process: %v", err)
	}
	code, waitErr := supervisor.Wait()
	if waitErr != nil || code != 0 {
		t.Errorf("target Wait() = (%d, %v), want (0, nil)", code, waitErr)
	}
	if err := supervisor.Close(); err != nil {
		t.Errorf("Close() after target FD witness = %v", err)
	}
	receipt, ok := supervisor.ContainmentStopReceipt()
	authorityValue := supervisor.ContainmentAuthority()
	if !ok || authorityValue == nil || !receipt.ValidFor(*authorityValue) {
		t.Errorf("stop receipt = (%#v, %v), want authenticated helper receipt", receipt, ok)
	} else if err := supervisor.ReleaseContainment(); err != nil {
		t.Errorf("ReleaseContainment() = %v", err)
	}
	scanMu.Lock()
	updates := append([]authority.SupervisorScanUpdate(nil), helperScanUpdates...)
	scanMu.Unlock()
	if len(updates) == 0 {
		t.Fatal("helper did not send any scan request over its metadata channel")
	}
	for _, update := range updates {
		if update.Owner != authority.SupervisorScanOwnerHelper || update.Sequence == 0 ||
			update.SupervisorPID != supervisor.HelperPID() || update.SupervisorIdentity != supervisor.HelperIdentity() ||
			update.MirrorIdentity != supervisor.mirrorIdentity {
			t.Fatalf("helper metadata scan request lost authenticated identity: %#v", update)
		}
	}
}

func TestLinuxSupervisorOverlappingRunsDoNotInheritPriorDescriptors(t *testing.T) {
	type targetIO struct {
		stdinRead, stdinWrite   *os.File
		stdoutRead, stdoutWrite *os.File
		stderrRead, stderrWrite *os.File
	}
	newTargetIO := func() targetIO {
		var pipes targetIO
		t.Cleanup(func() {
			for _, file := range []*os.File{pipes.stdinRead, pipes.stdinWrite, pipes.stdoutRead, pipes.stdoutWrite, pipes.stderrRead, pipes.stderrWrite} {
				if file != nil {
					_ = file.Close()
				}
			}
		})
		var err error
		pipes.stdinRead, pipes.stdinWrite, err = os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		pipes.stdoutRead, pipes.stdoutWrite, err = os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		pipes.stderrRead, pipes.stderrWrite, err = os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		return pipes
	}
	newHelper := func() *exec.Cmd {
		helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
		helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
		return helper
	}
	cleanupSupervisor := func(name string, supervisor *linuxSupervisor, stdinWrite *os.File) {
		if stdinWrite != nil {
			_ = stdinWrite.Close()
		}
		if supervisor == nil {
			return
		}
		supervisor.mu.Lock()
		released := supervisor.released
		supervisor.mu.Unlock()
		if released {
			return
		}
		if _, ok := supervisor.ContainmentStopReceipt(); !ok {
			if err := supervisor.Close(); err != nil {
				if supervisor.helper != nil {
					_ = supervisor.helper.Kill()
				}
				select {
				case <-supervisor.helperDone:
				case <-time.After(2 * time.Second):
					t.Errorf("%s helper did not exit during bounded cleanup", name)
				}
				if err := supervisor.Terminate(true); err != nil {
					t.Errorf("terminate %s after descriptor witness failure: %v", name, err)
				}
			}
		}
		if _, ok := supervisor.ContainmentStopReceipt(); ok {
			if err := supervisor.ReleaseContainment(); err != nil {
				t.Errorf("release %s after descriptor witness: %v", name, err)
			}
		}
	}

	firstIO := newTargetIO()
	firstCommand := exec.Command("/bin/sh", "-c", "exec sleep 600")
	firstCommand.Stdin, firstCommand.Stdout, firstCommand.Stderr = firstIO.stdinRead, firstIO.stdoutWrite, firstIO.stderrWrite
	first, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       firstCommand,
		HelperCommand: newHelper(),
		OwnerContext:  "linux-helper:test-overlap-fd-inheritance-a",
		Prepare:       func(authority.SupervisorHandoff) error { return nil },
		Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("start run A: %v", err)
	}
	t.Cleanup(func() { cleanupSupervisor("run A", first, firstIO.stdinWrite) })
	_ = firstIO.stdinRead.Close()
	_ = firstIO.stdoutWrite.Close()
	_ = firstIO.stderrWrite.Close()
	if err := first.Commit(); err != nil {
		t.Fatalf("commit run A: %v", err)
	}
	if err := first.RenewLease(30*time.Second, 1); err != nil {
		t.Fatalf("arm run A lease: %v", err)
	}
	if err := first.Resume(); err != nil {
		t.Fatalf("resume run A: %v", err)
	}
	firstPID := first.PID()
	if first.pidfd < 0 {
		t.Fatal("run A did not retain its pidfd")
	}
	flags, err := unix.FcntlInt(uintptr(first.pidfd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatalf("read run A pidfd descriptor flags: %v", err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("run A pidfd is not close-on-exec in the daemon")
	}

	secondIO := newTargetIO()
	secondCommand := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorMetadataSocketTargetWitness$", "--")
	secondCommand.Env = append(os.Environ(),
		"SYMMETRY_LINUX_SUPERVISOR_TARGET_FD_WITNESS=1",
		"SYMMETRY_LINUX_SUPERVISOR_TARGET_PIDFD_WITNESS="+strconv.Itoa(firstPID),
	)
	secondCommand.Stdin, secondCommand.Stdout, secondCommand.Stderr = secondIO.stdinRead, secondIO.stdoutWrite, secondIO.stderrWrite
	second, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       secondCommand,
		HelperCommand: newHelper(),
		OwnerContext:  "linux-helper:test-overlap-fd-inheritance-b",
		Prepare:       func(authority.SupervisorHandoff) error { return nil },
		Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("start run B while run A pidfd is retained: %v", err)
	}
	t.Cleanup(func() { cleanupSupervisor("run B", second, secondIO.stdinWrite) })
	_ = secondIO.stdinRead.Close()
	_ = secondIO.stdoutWrite.Close()
	_ = secondIO.stderrWrite.Close()
	if inherited, err := linuxSupervisorTestProcessHasPIDFDFor(second.HelperPID(), firstPID); err != nil {
		t.Fatalf("inspect run B helper descriptors: %v", err)
	} else if inherited {
		t.Fatalf("run B helper inherited run A pidfd for target %d", firstPID)
	}
	if err := second.Commit(); err != nil {
		t.Fatalf("commit run B: %v", err)
	}
	if err := second.RenewLease(30*time.Second, 1); err != nil {
		t.Fatalf("arm run B lease: %v", err)
	}
	if err := second.Resume(); err != nil {
		t.Fatalf("resume run B: %v", err)
	}
	type witnessResult struct {
		metadataSocket, oldPIDFD string
		err                      error
	}
	witness := make(chan witnessResult, 1)
	go func() {
		reader := bufio.NewReader(secondIO.stdoutRead)
		metadataSocket, err := reader.ReadString('\n')
		if err != nil {
			witness <- witnessResult{metadataSocket: metadataSocket, err: err}
			return
		}
		oldPIDFD, err := reader.ReadString('\n')
		witness <- witnessResult{metadataSocket: metadataSocket, oldPIDFD: oldPIDFD, err: err}
	}()
	select {
	case result := <-witness:
		if result.err != nil {
			t.Fatalf("read run B descriptor witness: %v", result.err)
		}
		if result.metadataSocket != "SYMMETRY_METADATA_SOCKET_ACCESSIBLE=false\n" {
			t.Errorf("run B metadata socket witness = %q, want no scan metadata socket", result.metadataSocket)
		}
		if result.oldPIDFD != "SYMMETRY_OLD_PIDFD_ACCESSIBLE=false\n" {
			t.Errorf("run B old pidfd witness = %q, want run A pidfd inaccessible", result.oldPIDFD)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run B did not report its descriptor inheritance witness")
	}
	if err := secondIO.stdinWrite.Close(); err != nil {
		t.Fatalf("release run B witness target: %v", err)
	}
	if code, err := second.Wait(); err != nil || code != 0 {
		t.Fatalf("run B Wait() = (%d, %v), want (0, nil)", code, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close run B helper: %v", err)
	}
	if receipt, ok := second.ContainmentStopReceipt(); !ok || !receipt.ValidFor(*second.ContainmentAuthority()) {
		t.Fatalf("run B helper stop receipt = (%#v, %v), want authenticated receipt", receipt, ok)
	}
	if err := second.ReleaseContainment(); err != nil {
		t.Fatalf("release run B containment: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close run A helper: %v", err)
	}
	if receipt, ok := first.ContainmentStopReceipt(); !ok || !receipt.ValidFor(*first.ContainmentAuthority()) {
		t.Fatalf("run A helper stop receipt = (%#v, %v), want authenticated receipt", receipt, ok)
	}
	if err := first.ReleaseContainment(); err != nil {
		t.Fatalf("release run A containment: %v", err)
	}
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

func TestLinuxSupervisorOwnerLossProofBarrierIsPostReapAndOptIn(t *testing.T) {
	startSupervisor := func(t *testing.T, directory string) (*linuxSupervisor, string, string, string, func()) {
		t.Helper()
		reachedPath := filepath.Join(directory, "owner-stop-reached")
		releasePath := filepath.Join(directory, "owner-stop-release")
		resultPath := filepath.Join(directory, "owner-stop-result")
		targetMarker := filepath.Join(directory, "target-ran")

		stdinRead, stdinWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stdoutRead, stdoutWrite, err := os.Pipe()
		if err != nil {
			_ = stdinRead.Close()
			_ = stdinWrite.Close()
			t.Fatal(err)
		}
		stderrRead, stderrWrite, err := os.Pipe()
		if err != nil {
			for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite} {
				_ = file.Close()
			}
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite} {
				_ = file.Close()
			}
		})

		command := exec.Command("/bin/sh", "-c", `printf ran > "$1"; exec sleep 600`, "sh", targetMarker)
		command.Stdin = stdinRead
		command.Stdout = stdoutWrite
		command.Stderr = stderrWrite
		helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
		environment := make([]string, 0, len(os.Environ())+5)
		for _, value := range os.Environ() {
			name, _, _ := strings.Cut(value, "=")
			switch name {
			case "SYMMETRY_LINUX_SUPERVISOR_HELPER", linuxSupervisorProductionWitnessEnv,
				linuxSupervisorOwnerLossProofReachedEnv, linuxSupervisorOwnerLossProofReleaseEnv, linuxSupervisorOwnerLossProofResultEnv,
				linuxSupervisorOwnerRecoveryAuthenticatedEnv:
				continue
			}
			environment = append(environment, value)
		}
		helper.Env = append(environment,
			"SYMMETRY_LINUX_SUPERVISOR_HELPER=1",
			linuxSupervisorProductionWitnessEnv+"=1",
			linuxSupervisorOwnerLossProofReachedEnv+"="+reachedPath,
			linuxSupervisorOwnerLossProofReleaseEnv+"="+releasePath,
			linuxSupervisorOwnerLossProofResultEnv+"="+resultPath,
		)
		supervisor, err := StartLinuxSupervisor(LinuxSupervisorStartSpec{
			Command:       command,
			HelperCommand: helper,
			OwnerContext:  "linux-helper:test-owner-loss-proof-barrier",
			Prepare:       func(authority.SupervisorHandoff) error { return nil },
			Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
		})
		if err != nil {
			if supervisor != nil {
				_ = supervisor.AbortContainment()
			}
			t.Fatalf("StartLinuxSupervisor() error = %v", err)
		}
		ownerLost := false
		t.Cleanup(func() {
			if supervisor.released {
				return
			}
			if !ownerLost {
				if err := supervisor.AbortContainment(); err != nil {
					t.Errorf("cleanup Linux supervisor: %v", err)
				}
				return
			}
			_ = os.WriteFile(releasePath, []byte("release\n"), 0o600)
			value := supervisor.ContainmentAuthority()
			if value == nil {
				return
			}
			var receipt authority.StopReceipt
			var stopErr error
			for range 100 {
				receipt, stopErr = StopPersistedLinuxSupervisor(*value)
				if stopErr == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if stopErr != nil {
				t.Errorf("recover Linux supervisor during cleanup: %v", stopErr)
				return
			}
			value.StopReceipt = &receipt
			if err := ReleasePersistedLinuxSupervisor(*value); err != nil {
				t.Errorf("release Linux supervisor during cleanup: %v", err)
				return
			}
			select {
			case <-supervisor.helperDone:
			case <-time.After(5 * time.Second):
				t.Error("Linux supervisor helper did not exit during cleanup")
				return
			}
			supervisor.mu.Lock()
			supervisor.receipt = &receipt
			supervisor.authority.StopReceipt = &receipt
			supervisor.handoff.StopReceipt = &receipt
			supervisor.mu.Unlock()
			if err := supervisor.releaseAfterHelperDeath(supervisor.mirror); err != nil {
				t.Errorf("release Linux supervisor mirror during cleanup: %v", err)
			}
		})
		if err := supervisor.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		return supervisor, reachedPath, releasePath, resultPath, func() { ownerLost = true }
	}

	t.Run("owner loss reaches the reaped pre-proof boundary", func(t *testing.T) {
		supervisor, reachedPath, releasePath, resultPath, markOwnerLost := startSupervisor(t, t.TempDir())
		if supervisor.resumed {
			t.Fatal("witness target resumed before owner loss")
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(reachedPath), "target-ran")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target user-code marker before owner loss = %v, want absent while exec-stopped", err)
		}
		if err := supervisor.ownerWrite.Close(); err != nil {
			t.Fatalf("close owner pipe: %v", err)
		}
		markOwnerLost()
		contents, err := waitForLinuxSupervisorTestMarker(reachedPath, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "post_reap_pre_proof\n" {
			t.Fatalf("owner-loss reached marker = %q, want post-reap/pre-proof marker", contents)
		}
		if _, err := ProcessIdentity(supervisor.PID()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target identity after reached marker = error %v, want reaped target", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(reachedPath), "target-ran")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target user-code marker = %v, want absent while exec-stopped", err)
		}
		if _, err := os.Lstat(resultPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("proof result marker before release = %v, want absent", err)
		}
		if _, ok := supervisor.ContainmentStopReceipt(); ok {
			t.Fatal("daemon mirror minted a stop receipt before the proof barrier was released")
		}

		release, err := os.OpenFile(releasePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatalf("create proof release marker: %v", err)
		}
		if _, err := io.WriteString(release, "release\n"); err != nil {
			_ = release.Close()
			t.Fatalf("write proof release marker: %v", err)
		}
		if err := release.Close(); err != nil {
			t.Fatalf("close proof release marker: %v", err)
		}
		result, err := waitForLinuxSupervisorTestMarker(resultPath, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if string(result) != "proof_attempted\n" {
			t.Fatalf("proof result marker = %q, want an attempt-only marker without physical-success status", result)
		}

		value := supervisor.ContainmentAuthority()
		if value == nil {
			t.Fatal("missing Linux supervisor recovery authority")
		}
		var receipt authority.StopReceipt
		var stopErr error
		for range 100 {
			receipt, stopErr = StopPersistedLinuxSupervisor(*value)
			if stopErr == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if stopErr != nil {
			t.Fatalf("StopPersistedLinuxSupervisor() = %v", stopErr)
		}
		value.StopReceipt = &receipt
		if err := ReleasePersistedLinuxSupervisor(*value); err != nil {
			t.Fatalf("ReleasePersistedLinuxSupervisor() error = %v", err)
		}
		select {
		case <-supervisor.helperDone:
		case <-time.After(5 * time.Second):
			t.Fatal("Linux supervisor helper did not exit after recovery release")
		}
		supervisor.mu.Lock()
		supervisor.receipt = &receipt
		supervisor.authority.StopReceipt = &receipt
		supervisor.handoff.StopReceipt = &receipt
		supervisor.mu.Unlock()
		if err := supervisor.releaseAfterHelperDeath(supervisor.mirror); err != nil {
			t.Fatalf("release Linux supervisor mirror after recovery: %v", err)
		}
	})

	t.Run("normal stop does not enter the barrier", func(t *testing.T) {
		supervisor, reachedPath, _, resultPath, _ := startSupervisor(t, t.TempDir())
		if err := supervisor.Terminate(true); err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
		if _, err := os.Lstat(reachedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("normal stop reached owner-loss barrier: %v", err)
		}
		if _, err := os.Lstat(resultPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("normal stop wrote owner-loss proof result: %v", err)
		}
		if err := supervisor.ReleaseContainment(); err != nil {
			t.Fatalf("ReleaseContainment() error = %v", err)
		}
	})
}

func TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec(t *testing.T) {
	witnessDir := t.TempDir()
	firedPath := filepath.Join(witnessDir, "wrong-session-hello.json")
	targetMarker := filepath.Join(witnessDir, "target-ran")
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorHelloSessionInjectionEnv, "1")
	t.Setenv(linuxSupervisorHelloSessionFiredEnv, firedPath)
	t.Setenv(linuxSupervisorHelloSessionReleaseEnv, "")

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		_ = stdinWrite.Close()
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		_ = stdinWrite.Close()
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite} {
			_ = file.Close()
		}
	})
	command := exec.Command("/bin/sh", "-c", `printf ran > "$1"; exec sleep 600`, "sh", targetMarker)
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	supervisor, startErr := StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-wrong-hello-session",
		Prepare:       func(authority.SupervisorHandoff) error { return nil },
		Bind:          func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if startErr == nil || !strings.Contains(startErr.Error(), "observe Linux supervisor mirror containment") {
		t.Fatalf("StartLinuxSupervisor() error = %v, want initial procfs mirror rejection", startErr)
	}
	if supervisor == nil {
		t.Fatalf("StartLinuxSupervisor() returned no owner for cleanup after mirror rejection: %v", startErr)
	}
	defer func() {
		if !channelClosedV2(supervisor.helperDone) {
			if supervisor.helper != nil {
				_ = supervisor.helper.Kill()
			}
			select {
			case <-supervisor.helperDone:
			case <-time.After(5 * time.Second):
				t.Error("wrong-session helper did not exit during bounded cleanup")
			}
		}
		if err := closeLinuxWrongHelloTestOwner(supervisor); err != nil {
			t.Errorf("close wrong-session test owner: %v", err)
		}
	}()

	contents, err := waitForLinuxSupervisorTestMarker(firedPath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var witness linuxSupervisorHelloSessionWitness
	if err := json.Unmarshal(contents, &witness); err != nil {
		t.Fatalf("decode wrong-session helper witness: %v", err)
	}
	actual, transmitted := witness.Actual, witness.Transmitted
	if actual.TargetPID <= 0 || actual.TargetIdentity == "" || actual.TargetPGRP != int64(actual.TargetPID) || actual.TargetSession <= 0 || actual.TargetStartTime == 0 {
		t.Fatalf("captured helper anchor is incomplete: %#v", actual)
	}
	if transmitted.TargetPID != actual.TargetPID || transmitted.TargetIdentity != actual.TargetIdentity ||
		transmitted.TargetPGRP != actual.TargetPGRP || transmitted.TargetStartTime != actual.TargetStartTime ||
		transmitted.TargetSession <= 0 || transmitted.TargetSession == int64(transmitted.TargetPID) || transmitted.TargetSession == actual.TargetSession {
		t.Fatalf("transmitted helper anchor = %#v, want only a plausible wrong session from %#v", transmitted, actual)
	}
	if witness.SupervisorPID != supervisor.HelperPID() || witness.SupervisorIdentity != supervisor.HelperIdentity() {
		t.Fatalf("wrong-session witness helper identity = %d/%q, want %d/%q", witness.SupervisorPID, witness.SupervisorIdentity, supervisor.HelperPID(), supervisor.HelperIdentity())
	}
	liveIdentity, err := ProcessIdentity(actual.TargetPID)
	if err != nil || liveIdentity != actual.TargetIdentity {
		t.Fatalf("actual target identity = %q, error=%v, want captured %q", liveIdentity, err, actual.TargetIdentity)
	}
	stat, err := readLinuxProcessStat(actual.TargetPID)
	if err != nil {
		t.Fatalf("read actual target procfs anchor: %v", err)
	}
	if stat.pgrp != actual.TargetPGRP || stat.session != actual.TargetSession || stat.startTime != actual.TargetStartTime {
		t.Fatalf("actual target procfs anchor = pid:%d pgrp:%d session:%d start:%d, marker captured %#v", actual.TargetPID, stat.pgrp, stat.session, stat.startTime, actual)
	}
	if _, err := os.Stat(targetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker = %v, want absent before resume", err)
	}
	if supervisor.committed || supervisor.resumed {
		t.Fatalf("wrong-session mirror rejection crossed commit/resume: committed=%t resumed=%t", supervisor.committed, supervisor.resumed)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok {
		t.Fatal("wrong-session precommit rejection minted a stop receipt")
	}

	// The helper's exact target anchor, not the rejected mirror anchor, must
	// prove the precommit abort before the test drops the remaining owner FDs.
	_ = supervisor.AbortContainment()
	select {
	case <-supervisor.helperDone:
	case <-time.After(5 * time.Second):
		t.Fatal("wrong-session helper did not finish precommit abort within the bound")
	}
	handoff := supervisor.Handoff()
	if _, err := ProvePreparedSupervisorAborted(handoff, func() error { return nil }); err != nil {
		t.Fatalf("exact helper/target/group abort proof: %v", err)
	}
	if _, err := os.Stat(targetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after abort = %v, want absent", err)
	}
}

func waitForLinuxSupervisorTestMarker(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			return contents, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("wrong-session helper marker %q did not appear within %s", path, timeout)
}

func closeLinuxWrongHelloTestOwner(supervisor *linuxSupervisor) error {
	if supervisor == nil {
		return nil
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	var closeErr error
	if err := supervisor.closeScanTransport(deadline); err != nil {
		closeErr = errors.Join(closeErr, err)
	}
	supervisor.restoreMirrorScanBarrier()
	if supervisor.mirror != nil {
		if err := supervisor.mirror.stopDescendantMonitor(deadline); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	supervisor.mu.Lock()
	fd := supervisor.pidfd
	supervisor.pidfd = -1
	ownerWrite, controlWrite := supervisor.ownerWrite, supervisor.controlWrite
	responseRead, statusRead := supervisor.responseRead, supervisor.statusRead
	supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead = nil, nil, nil, nil
	supervisor.mu.Unlock()
	if supervisor.mirror != nil {
		supervisor.mirror.mutex.Lock()
		supervisor.mirror.fd = -1
		supervisor.mirror.mutex.Unlock()
	}
	closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead)
	if fd >= 0 {
		if err := unix.Close(fd); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
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
	// An in-flight response can be lost at the same time as helper death;
	// mirror takeover must win over the recovery-endpoint retry path.
	supervisor.responseLossPending = true
	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("mirror Terminate() with adopted children = %v", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); !ok {
		t.Fatal("mirror stop did not produce an exact receipt")
	}
	_ = supervisor.ReleaseContainment()
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
}

func TestLinuxSupervisorStopWriteFailureAfterHelperExitUsesSealedMirror(t *testing.T) {
	enableLinuxTestSubreaper(t)

	sealDir := t.TempDir()
	triggerPath := filepath.Join(sealDir, "scan.trigger")
	firedPath := filepath.Join(sealDir, "scan.reached")
	releasePath := filepath.Join(sealDir, "scan.release")
	if err := os.WriteFile(releasePath, []byte("released\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(linuxSupervisorProductionWitnessEnv, "1")
	t.Setenv(linuxSupervisorScanFailureModeEnv, linuxSupervisorScanSealAfterClean)
	t.Setenv(linuxSupervisorScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxSupervisorScanFailureFiredEnv, firedPath)
	t.Setenv(linuxSupervisorScanPendingReleaseEnv, releasePath)
	t.Setenv(linuxSupervisorScanSealOwnerEnv, "both")

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		_ = stdinWrite.Close()
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		_ = stdinWrite.Close()
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite} {
			_ = file.Close()
		}
	})

	command := exec.Command(os.Args[0], "-test.run=^TestProcessGroupContainmentHelper$", "--", "leader-exits-after-child")
	command.Env = append(os.Environ(), "GO_WANT_PROCESS_GROUP_CONTAINMENT_HELPER=1")
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	helper := exec.Command(os.Args[0], "-test.run=^TestLinuxSupervisorHelperProcess$", "--")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_SUPERVISOR_HELPER=1")
	var (
		scanMu      sync.Mutex
		scanUpdates []authority.SupervisorScanUpdate
		supervisor  *linuxSupervisor
		leader      linuxTestChildOwner
		child       linuxTestChildOwner
	)
	supervisor, err = StartLinuxSupervisor(LinuxSupervisorStartSpec{
		Command:       command,
		HelperCommand: helper,
		OwnerContext:  "linux-helper:test-write-failure-after-helper-exit",
		Prepare: func(value authority.SupervisorHandoff) error {
			if value.ScanUpdate != nil {
				scanMu.Lock()
				scanUpdates = append(scanUpdates, *value.ScanUpdate)
				scanMu.Unlock()
			}
			return nil
		},
		Bind: func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	t.Cleanup(func() {
		if supervisor != nil {
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
		}
		leader.cleanup(t)
		child.cleanup(t)
	})
	if err != nil {
		t.Fatal(err)
	}
	leader.pid = supervisor.PID()
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
	if err := os.WriteFile(triggerPath, []byte("seal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForMarker := func(path string) error {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(path); err == nil {
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if !time.Now().Before(deadline) {
				return context.DeadlineExceeded
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for _, owner := range []string{authority.SupervisorScanOwnerHelper, authority.SupervisorScanOwnerMirror} {
		if err := waitForMarker(firedPath + "." + owner); err != nil {
			t.Fatalf("wait for durable %s scan seal with live descendant: %v", owner, err)
		}
	}
	if err := syscall.Kill(child.pid, 0); err != nil {
		t.Fatalf("same-group descendant %d stopped before helper-crash boundary: %v", child.pid, err)
	}

	writeHookCalls := 0
	var requestWriteErr error
	supervisor.writeControlFrameForTesting = func(control *os.File, request linuxSupervisorRequest) error {
		writeHookCalls++
		if err := supervisor.helper.Kill(); err != nil {
			t.Errorf("kill exact helper at control-write boundary: %v", err)
			return err
		}
		select {
		case <-supervisor.helperDone:
		case <-time.After(2 * time.Second):
			t.Error("helper did not exit at control-write boundary")
		}
		requestWriteErr = writeLinuxSupervisorFrame(control, request)
		return requestWriteErr
	}
	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("Terminate() after helper died between request check and write = %v", err)
	}
	if writeHookCalls != 1 {
		t.Fatalf("control-write boundary hook calls = %d, want one request and no duplicate request", writeHookCalls)
	}
	if !errors.Is(requestWriteErr, syscall.EPIPE) {
		t.Fatalf("control write failure = %v, want EPIPE after exact helper exit", requestWriteErr)
	}
	if !supervisor.responseTransportLost || !supervisor.responseLossPending {
		t.Fatalf("write failure transport state = lost:%t pending:%t, want sticky unknown outcome", supervisor.responseTransportLost, supervisor.responseLossPending)
	}
	receipt, ok := supervisor.ContainmentStopReceipt()
	if !ok || !receipt.ValidFor(supervisor.authority) || receipt.ActiveProcesses != 0 {
		t.Fatalf("helper-death mirror receipt = (%#v, %v), want exact stopped receipt", receipt, ok)
	}
	if err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), supervisor.PID(), supervisor.Identity(), time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("target and same-group descendant remain after mirror receipt: %v", err)
	}
	scanMu.Lock()
	updates := append([]authority.SupervisorScanUpdate(nil), scanUpdates...)
	scanMu.Unlock()
	sealCount := map[string]int{}
	assertAllSealedCount := 0
	for _, update := range updates {
		if update.SupervisorPID != supervisor.HelperPID() || update.SupervisorIdentity != supervisor.HelperIdentity() ||
			update.MirrorIdentity != supervisor.mirrorIdentity {
			t.Fatalf("scan journal callback received unauthenticated owner identity: %#v", update)
		}
		if update.Phase == authority.SupervisorScanPhaseSeal {
			sealCount[update.Owner]++
		}
		if update.Phase == authority.SupervisorScanPhaseAssertAllSealed {
			assertAllSealedCount++
		}
	}
	if sealCount[authority.SupervisorScanOwnerHelper] != 1 || sealCount[authority.SupervisorScanOwnerMirror] != 1 || assertAllSealedCount != 1 {
		t.Fatalf("durable scan journal callbacks: helper_seals=%d mirror_seals=%d assert_all_sealed=%d", sealCount[authority.SupervisorScanOwnerHelper], sealCount[authority.SupervisorScanOwnerMirror], assertAllSealedCount)
	}
}

func TestLinuxSupervisorMirrorRejectsHelperIdentityMismatch(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	helperDone := make(chan struct{})
	close(helperDone)
	previousIdentity := linuxSupervisorProcessIdentity
	t.Cleanup(func() { linuxSupervisorProcessIdentity = previousIdentity })
	linuxSupervisorProcessIdentity = func(pid int) (string, error) {
		if pid != value.SupervisorPID {
			t.Fatalf("helper identity probe PID = %d, want %d", pid, value.SupervisorPID)
		}
		return value.SupervisorIdentity + "-replacement", nil
	}
	callbackCalls := 0
	mirror := &processGroup{pid: value.TargetPID, fd: -1}
	supervisor := &linuxSupervisor{
		authority: value, helperDone: helperDone, mirror: mirror, pidfd: 47,
		containmentUnprovenCallback: func() error {
			callbackCalls++
			return nil
		},
	}
	err := supervisor.Terminate(true)
	if !errors.Is(err, ErrLinuxSupervisorStopUnproven) || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("helper identity mismatch stop = %v, want unresolved exact-helper fence", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok || supervisor.receipt != nil {
		t.Fatalf("helper identity mismatch installed an unauthenticated receipt: %#v", supervisor.receipt)
	}
	if supervisor.pidfd != 47 || mirror.closeCompleted {
		t.Fatalf("helper identity mismatch consumed mirror authority: pidfd=%d mirror_closed=%t", supervisor.pidfd, mirror.closeCompleted)
	}
	if callbackCalls != 1 {
		t.Fatalf("containment-unproven callback calls = %d, want 1", callbackCalls)
	}
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
	if _, ok := supervisor.ContainmentStopReceipt(); ok || supervisor.receipt != nil {
		t.Fatalf("mirror proof failure installed a stop receipt: receipt=%v available=%v", supervisor.receipt, ok)
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
	value := testLinuxSupervisorAuthority(t)
	mirror, handoff, mirrorIdentity, scanPrepare := newLinuxSupervisorSealedMirrorFixture(t, value)
	supervisor := &linuxSupervisor{
		authority: value, handoff: handoff, helperDone: helperDone, mirror: mirror,
		mirrorIdentity: mirrorIdentity, scanPrepare: scanPrepare, pidfd: 47,
	}

	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("mirror proof retry = %v, want receipt success", err)
	}
	if calls != 2 {
		t.Fatalf("mirror proof calls = %d, want first transient failure plus successful retry", calls)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); !ok || supervisor.receipt == nil {
		t.Fatalf("mirror proof retry did not install a successful receipt: receipt=%v available=%v", supervisor.receipt, ok)
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

	// Keep the fixture's stat script reserved for the close-requested scan.
	// Periodic scans are valid in production, but letting one consume the
	// scripted failure makes the signal/probe path depend on scheduler timing.
	mirror := &processGroup{
		pid: pid, fd: pidfd, anchor: anchor.private(), anchorCaptured: true,
		pauseDescendantMonitoring: true,
	}
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
	if supervisor.pidfd != pidfd || !supervisor.ContainmentCloseRetryable() {
		t.Fatalf("mirror failure state = pidfd:%d retryable:%v, want retained pidfd %d and retryable", supervisor.pidfd, supervisor.ContainmentCloseRetryable(), pidfd)
	}
	mirror.mutex.Lock()
	scanLost, closeCompleted, retainedFD := mirror.descendantScanLost, mirror.closeCompleted, mirror.fd
	mirror.mutex.Unlock()
	if !scanLost || closeCompleted || retainedFD != pidfd {
		t.Fatalf("mirror scan failure state = lost:%v completed:%v fd:%d, want sticky loss, incomplete close, retained fd %d", scanLost, closeCompleted, retainedFD, pidfd)
	}
	if len(signals) != 2 || signals[0] != unix.SIGKILL || signals[1] != 0 {
		t.Fatalf("mirror signals = %#v, want [SIGKILL 0] proof sequence", signals)
	}
}

func TestLinuxSupervisorEndpointPathSkipsUnusableDirectories(t *testing.T) {
	jobID := strings.Repeat("a", authority.TokenBytes*2)
	usable, err := os.MkdirTemp("/tmp", "sym")
	if err != nil {
		t.Skipf("create short temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(usable) })
	long := filepath.Join(usable, strings.Repeat("d", linuxSupervisorEndpointMax))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Skipf("create long directory: %v", err)
	}
	missing := filepath.Join(usable, "missing")

	// procfs exists and fits sun_path but cannot hold a socket, even for root.
	endpoint, err := linuxSupervisorEndpointPath(jobID, missing, long, "relative", "/proc", usable)
	if err != nil {
		t.Fatalf("linuxSupervisorEndpointPath() = %v", err)
	}
	if filepath.Dir(endpoint) != usable {
		t.Fatalf("endpoint = %q, want it under %q", endpoint, usable)
	}
	if _, err := linuxSupervisorEndpoint("owner|endpoint=" + endpoint); err != nil {
		t.Fatalf("chosen endpoint is not accepted by recovery parsing: %v", err)
	}
	if _, err := linuxSupervisorEndpointPath(jobID, missing, long, "/proc"); err == nil {
		t.Fatal("linuxSupervisorEndpointPath() without a usable directory = nil error")
	}
}

func TestLinuxSupervisorMirrorObservationRequiresCleanCompletedScan(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	mirrorIdentity := strings.Repeat("e", authority.TokenBytes*2)
	handoff := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: value.LaunchToken, Secret: value.Secret,
		OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, TargetPID: value.TargetPID,
		TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity,
	}
	newSupervisor := func(mirror *processGroup) *linuxSupervisor {
		return &linuxSupervisor{
			authority: value, handoff: handoff, mirror: mirror, mirrorIdentity: mirrorIdentity,
			scanPrepare: func(request authority.SupervisorHandoff) error {
				update := request.ScanUpdate
				if update == nil || update.Owner != authority.SupervisorScanOwnerMirror ||
					update.Phase != authority.SupervisorScanPhaseAssertClean || update.Sequence == 0 ||
					update.MirrorIdentity != mirrorIdentity || update.SupervisorPID != value.SupervisorPID ||
					update.SupervisorIdentity != value.SupervisorIdentity {
					return errors.New("mirror clean assertion identity mismatch")
				}
				return nil
			},
		}
	}

	clean := &processGroup{
		descendantScanSequence: 1, descendantScanCompletedSequence: 1, descendantScanCompleted: true,
		monitorTerminalReason: descendantMonitorTerminalCleanLeaderAbsent,
	}
	if err := newSupervisor(clean).verifyMirrorObservation(); err != nil {
		t.Fatalf("clean completed mirror verification = %v, want nil", err)
	}

	for _, test := range []struct {
		name   string
		mirror *processGroup
	}{
		{name: "no completed scan", mirror: &processGroup{monitorTerminalReason: descendantMonitorTerminalCleanLeaderAbsent}},
		{name: "scan failure", mirror: &processGroup{descendantScanCompleted: true, descendantScanLost: true}},
		{
			name: "observed escape",
			mirror: &processGroup{
				descendantScanCompleted: true,
				escapedDescendant:       &linuxProcessStat{pid: 2345, pgrp: 2345, session: 2345, startTime: 789},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := newSupervisor(test.mirror).verifyMirrorObservation(); !errors.Is(err, ErrLinuxDescendantContainmentUnproven) {
				t.Fatalf("mirror verification = %v, want unresolved proof", err)
			}
		})
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
	if supervisor.controlWrite != nil || supervisor.ownerWrite == nil {
		t.Fatalf("response-drop control/owner state = %v/%v, want closed control and retained owner", supervisor.controlWrite, supervisor.ownerWrite)
	}
	if _, err := supervisor.request(linuxSupervisorOpStop, 0, 0); !errors.Is(err, ErrLinuxSupervisorResponseLost) {
		t.Fatalf("request after response drop = %v, want ErrLinuxSupervisorResponseLost", err)
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
	receipt := testLinuxSupervisorStopReceipt(supervisor.authority)
	supervisor.receipt = &receipt

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
	if supervisor.controlWrite != nil || supervisor.ownerWrite == nil {
		t.Fatalf("response drop control/owner state = %v/%v, want closed control and open owner transport", supervisor.controlWrite, supervisor.ownerWrite)
	}
	if !strings.Contains(supervisor.authority.OwnerContext, "|endpoint=") {
		t.Fatalf("recovery owner context = %q, want endpoint fence", supervisor.authority.OwnerContext)
	}
}

func TestLinuxSupervisorResponseLossStopUsesRecoveryReceipt(t *testing.T) {
	value := testLinuxSupervisorRecoveryAuthority(t)
	mirror, handoff, mirrorIdentity, scanPrepare := newLinuxSupervisorSealedMirrorFixture(t, value)
	requests := newLinuxSupervisorRecoveryTestServer(t, value, "stopped", "", mirror.descendantScanSealSequence())
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	supervisor := &linuxSupervisor{
		authority:             value,
		handoff:               handoff,
		mirror:                mirror,
		mirrorIdentity:        mirrorIdentity,
		scanPrepare:           scanPrepare,
		helperDone:            make(chan struct{}),
		controlWrite:          controlWrite,
		responseTransportLost: true,
		responseLossPending:   true,
	}
	t.Cleanup(func() {
		_ = controlRead.Close()
		_ = controlWrite.Close()
	})

	if err := supervisor.Terminate(true); err != nil {
		t.Fatalf("Terminate() after response loss = %v", err)
	}
	select {
	case request := <-requests:
		if request.Operation != linuxSupervisorOpStop || request.Secret != value.Secret {
			t.Fatalf("recovery stop request = %#v, want authenticated stop", request)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery stop request was not received")
	}
	receipt, ok := supervisor.ContainmentStopReceipt()
	if !ok || !receipt.ValidFor(value) {
		t.Fatalf("recovered stop receipt = %#v, available=%v, want exact receipt", receipt, ok)
	}
	if !supervisor.responseTransportLost || !supervisor.responseLossPending {
		t.Fatalf("response-loss state = transport:%v pending:%v, want sticky", supervisor.responseTransportLost, supervisor.responseLossPending)
	}
	if supervisor.controlWrite != nil {
		t.Fatal("response-loss recovery retained the normal control writer")
	}
	_ = controlWrite.Close()
	data, err := io.ReadAll(controlRead)
	if err != nil {
		t.Fatalf("read normal control pipe = %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("normal control pipe received %d bytes during recovery", len(data))
	}
}

func TestStopPersistedLinuxSupervisorRequiresTerminalScanSeal(t *testing.T) {
	value := testLinuxSupervisorRecoveryAuthority(t)
	requests := newLinuxSupervisorRecoveryTestServer(t, value, "stopped", "", 0)
	_, sequence, err := StopPersistedLinuxSupervisorWithScanSeal(value)
	if !errors.Is(err, ErrLinuxSupervisorStopUnproven) || !strings.Contains(err.Error(), "no terminal scan seal") || sequence != 0 {
		t.Fatalf("persisted stop without terminal seal = sequence %d, error %v; want fail-closed unproven result", sequence, err)
	}
	select {
	case request := <-requests:
		if request.Operation != linuxSupervisorOpStop || request.Secret != value.Secret {
			t.Fatalf("missing-seal recovery request = %#v, want authenticated stop", request)
		}
	case <-time.After(time.Second):
		t.Fatal("missing-seal recovery request was not received")
	}
}

type linuxSupervisorResponseReadGate struct {
	reader      io.Reader
	entered     chan struct{}
	release     chan struct{}
	readOnce    sync.Once
	releaseOnce sync.Once
}

func (gate *linuxSupervisorResponseReadGate) Read(value []byte) (int, error) {
	gate.readOnce.Do(func() {
		close(gate.entered)
		<-gate.release
	})
	return gate.reader.Read(value)
}

func (gate *linuxSupervisorResponseReadGate) unblock() {
	gate.releaseOnce.Do(func() { close(gate.release) })
}

// TestLinuxSupervisorResponseWriteLossReplaysStopWhileHelperLives keeps the
// response read blocked until the helper write succeeds, then exposes recovery
// only after the queued stop request is followed by control EOF.
func TestLinuxSupervisorResponseWriteLossReplaysStopWhileHelperLives(t *testing.T) {
	t.Setenv(linuxSupervisorProductionWitnessEnv, "0")
	value := testLinuxSupervisorRecoveryAuthority(t)
	mirror, handoff, mirrorIdentity, scanPrepare := newLinuxSupervisorSealedMirrorFixture(t, value)
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		t.Fatal(err)
	}
	recoveryListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	recoveryListener.SetUnlinkOnClose(false)

	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		recoveryListener.Close()
		_ = os.Remove(endpoint)
		t.Fatal(err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		_ = controlRead.Close()
		_ = controlWrite.Close()
		_ = recoveryListener.Close()
		_ = os.Remove(endpoint)
		t.Fatal(err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		_ = controlRead.Close()
		_ = controlWrite.Close()
		_ = responseRead.Close()
		_ = responseWrite.Close()
		_ = recoveryListener.Close()
		_ = os.Remove(endpoint)
		t.Fatal(err)
	}
	gate := &linuxSupervisorResponseReadGate{
		reader:  responseRead,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	supervisor := &linuxSupervisor{
		authority: value, handoff: handoff, mirror: mirror, mirrorIdentity: mirrorIdentity,
		scanPrepare: scanPrepare, helperDone: make(chan struct{}), ownerWrite: ownerWrite,
		controlWrite: controlWrite, responseRead: responseRead, responseReader: bufio.NewReader(gate),
	}
	t.Cleanup(func() {
		gate.unblock()
		_ = controlWrite.Close()
		_ = recoveryListener.Close()
		for _, file := range []*os.File{controlRead, responseRead, responseWrite, ownerRead, ownerWrite} {
			_ = file.Close()
		}
		_ = os.Remove(endpoint)
	})

	deadline := time.Now().Add(3 * time.Second)
	waitSignal := func(label string, signal <-chan struct{}) {
		t.Helper()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("%s exceeded the absolute recovery deadline", label)
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-signal:
		case <-timer.C:
			t.Fatalf("%s did not complete before the absolute recovery deadline", label)
		}
	}
	normalRequests := make(chan linuxSupervisorRequest, 1)
	controlEOF := make(chan struct{})
	controlReadResult := make(chan error, 1)
	go func() {
		defer close(controlEOF)
		reader := bufio.NewReader(controlRead)
		var request linuxSupervisorRequest
		if readErr := readLinuxSupervisorFrame(reader, &request); readErr != nil {
			controlReadResult <- fmt.Errorf("read initial control request: %w", readErr)
			return
		}
		normalRequests <- request
		var unexpected linuxSupervisorRequest
		controlReadResult <- readLinuxSupervisorFrame(reader, &unexpected)
	}()

	recoveryRequests := make(chan linuxSupervisorRequest, 1)
	recoveryResult := make(chan error, 1)
	go func() {
		<-controlEOF
		connection, acceptErr := recoveryListener.AcceptUnix()
		if acceptErr != nil {
			recoveryResult <- acceptErr
			return
		}
		defer connection.Close()
		var request linuxSupervisorRequest
		if readErr := readLinuxSupervisorFrame(bufio.NewReader(connection), &request); readErr != nil {
			recoveryResult <- readErr
			return
		}
		recoveryRequests <- request
		recoveryResult <- writeLinuxSupervisorFrame(connection, linuxSupervisorRecoveryResponse{
			linuxSupervisorResponse: linuxSupervisorResponse{
				Version:            linuxSupervisorProtocolVersion,
				Operation:          request.Operation,
				Status:             "stopped",
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
				ActiveProcesses:    0,
			},
			ScanSealSequence: mirror.descendantScanSealSequence(),
		})
	}()

	stopResult := make(chan error, 1)
	go func() { stopResult <- supervisor.Terminate(true) }()
	var normalRequest linuxSupervisorRequest
	select {
	case normalRequest = <-normalRequests:
	case <-time.After(time.Until(deadline)):
		t.Fatal("normal stop request was not written before the absolute recovery deadline")
	}
	if normalRequest.Operation != linuxSupervisorOpStop || normalRequest.Secret != value.Secret {
		t.Fatalf("normal helper request operation/secret mismatch: operation=%q", normalRequest.Operation)
	}
	waitSignal("normal response read gate", gate.entered)
	if supervisor.controlWrite == nil {
		t.Fatal("normal control writer was not open before the response was lost")
	}
	if err := writeLinuxSupervisorFrame(responseWrite, linuxSupervisorResponse{
		Version:            linuxSupervisorProtocolVersion,
		Operation:          normalRequest.Operation,
		Status:             "stopped",
		LaunchToken:        value.LaunchToken,
		Sequence:           normalRequest.Sequence,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		Token:              value.PipeToken,
		JobID:              value.JobID,
		ActiveProcesses:    0,
	}); err != nil {
		t.Fatalf("write successful normal helper response: %v", err)
	}
	select {
	case <-controlEOF:
		t.Fatal("control pipe closed before the successful response was lost")
	default:
	}
	if err := responseRead.Close(); err != nil {
		t.Fatalf("close response read end after successful helper write: %v", err)
	}
	gate.unblock()

	select {
	case stopErr := <-stopResult:
		if stopErr != nil {
			t.Fatalf("live stop response-loss replay = %v", stopErr)
		}
	case <-time.After(time.Until(deadline)):
		t.Fatal("live stop response-loss replay exceeded the absolute recovery deadline")
	}
	waitSignal("control EOF after the queued stop request", controlEOF)
	if readErr := <-controlReadResult; !errors.Is(readErr, io.EOF) {
		t.Fatalf("normal control pipe after queued stop request = %v, want EOF", readErr)
	}
	var recoveryRequest linuxSupervisorRequest
	select {
	case recoveryRequest = <-recoveryRequests:
	case <-time.After(time.Until(deadline)):
		t.Fatal("authenticated recovery stop was not issued before the absolute deadline")
	}
	if recoveryRequest.Operation != linuxSupervisorOpStop || recoveryRequest.LaunchToken != value.LaunchToken || recoveryRequest.Secret != value.Secret ||
		recoveryRequest.TargetPID != value.TargetPID || recoveryRequest.TargetIdentity != value.TargetIdentity ||
		recoveryRequest.OwnerKind != value.OwnerKind || recoveryRequest.OwnerContext != value.OwnerContext ||
		recoveryRequest.SupervisorPID != value.SupervisorPID || recoveryRequest.SupervisorIdentity != value.SupervisorIdentity ||
		recoveryRequest.Token != value.PipeToken || recoveryRequest.JobID != value.JobID {
		t.Fatalf("recovery stop did not preserve the exact supervisor identity: operation=%q launch=%q target=%d", recoveryRequest.Operation, recoveryRequest.LaunchToken, recoveryRequest.TargetPID)
	}
	if err := <-recoveryResult; err != nil {
		t.Fatalf("write recovery stop response: %v", err)
	}
	receipt, ok := supervisor.ContainmentStopReceipt()
	if !ok || !receipt.ValidFor(value) {
		t.Fatalf("live response-loss stop receipt = %#v, available=%v, want exact authority-bound receipt", receipt, ok)
	}
	if supervisor.controlWrite != nil || supervisor.ownerWrite == nil {
		t.Fatalf("post-recovery transport state = control:%v owner:%v, want closed control and retained owner", supervisor.controlWrite, supervisor.ownerWrite)
	}
	if !supervisor.responseTransportLost || !supervisor.responseLossPending {
		t.Fatalf("post-recovery response-loss state = transport:%v pending:%v, want sticky", supervisor.responseTransportLost, supervisor.responseLossPending)
	}
	select {
	case <-supervisor.helperDone:
		t.Fatal("helper exited before the live daemon recovered its stop receipt")
	default:
	}
}

func TestLinuxSupervisorReleaseResponseLossUsesRecoveryWhenReaderNil(t *testing.T) {
	value := testLinuxSupervisorRecoveryAuthority(t)
	receipt := testLinuxSupervisorStopReceipt(value)
	value.StopReceipt = &receipt
	requests := newLinuxSupervisorRecoveryTestServer(t, value, "released", "", 0)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	supervisor := &linuxSupervisor{
		authority:             value,
		receipt:               &receipt,
		helperDone:            make(chan struct{}),
		controlWrite:          controlWrite,
		pidfd:                 -1,
		responseTransportLost: true,
		responseLossPending:   true,
	}
	t.Cleanup(func() {
		_ = controlRead.Close()
		_ = controlWrite.Close()
	})

	if err := supervisor.ReleaseContainment(); err != nil {
		t.Fatalf("ReleaseContainment() after response loss = %v", err)
	}
	select {
	case request := <-requests:
		if request.Operation != linuxSupervisorOpRelease || request.Secret != value.Secret {
			t.Fatalf("recovery release request = %#v, want authenticated release", request)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery release request was not received")
	}
	if !supervisor.released || !supervisor.closed {
		t.Fatalf("release state = released:%v closed:%v, want both true", supervisor.released, supervisor.closed)
	}
	if supervisor.controlWrite != nil {
		t.Fatal("response-loss release retained the normal control writer")
	}
	if got, ok := supervisor.ContainmentStopReceipt(); !ok || !got.ValidFor(value) {
		t.Fatalf("release cleared stop receipt: %#v, available=%v", got, ok)
	}
	data, err := io.ReadAll(controlRead)
	if err != nil {
		t.Fatalf("read normal control pipe after release = %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("normal control pipe received %d bytes during recovery release", len(data))
	}
}

func TestLinuxSupervisorReleaseResponseLossFailureRetainsAuthority(t *testing.T) {
	value := testLinuxSupervisorRecoveryAuthority(t)
	receipt := testLinuxSupervisorStopReceipt(value)
	value.StopReceipt = &receipt
	requests := newLinuxSupervisorRecoveryTestServer(t, value, "error", "release unresolved", 0)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	supervisor := &linuxSupervisor{
		authority:             value,
		receipt:               &receipt,
		helperDone:            make(chan struct{}),
		controlWrite:          controlWrite,
		pidfd:                 -1,
		responseTransportLost: true,
		responseLossPending:   true,
	}
	t.Cleanup(func() {
		_ = controlRead.Close()
		_ = controlWrite.Close()
	})

	if err := supervisor.ReleaseContainment(); err == nil {
		t.Fatal("ReleaseContainment() after recovery release error = nil")
	}
	select {
	case request := <-requests:
		if request.Operation != linuxSupervisorOpRelease {
			t.Fatalf("recovery failure request operation = %q, want release", request.Operation)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery release request was not received")
	}
	if supervisor.released || supervisor.closed {
		t.Fatalf("failed release state = released:%v closed:%v, want both false", supervisor.released, supervisor.closed)
	}
	if got, ok := supervisor.ContainmentStopReceipt(); !ok || !got.ValidFor(value) {
		t.Fatalf("failed release lost stop receipt: %#v, available=%v", got, ok)
	}
	if got := supervisor.ContainmentAuthority(); got == nil || got.StopReceipt == nil || !got.StopReceipt.ValidFor(*got) {
		t.Fatalf("failed release lost authority receipt: %#v", got)
	}
	_ = controlWrite.Close()
	data, err := io.ReadAll(controlRead)
	if err != nil {
		t.Fatalf("read normal control pipe after failed release = %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("normal control pipe received %d bytes during failed recovery release", len(data))
	}
}

func TestLinuxSupervisorResponseLossNeverReusesNormalRequestPipe(t *testing.T) {
	value := testLinuxSupervisorAuthority(t)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		_ = controlRead.Close()
		_ = controlWrite.Close()
		t.Fatal(err)
	}
	supervisor := &linuxSupervisor{
		authority:             value,
		helperDone:            make(chan struct{}),
		controlWrite:          controlWrite,
		responseRead:          responseRead,
		responseReader:        bufio.NewReader(responseRead),
		responseTransportLost: true,
		responseLossPending:   true,
	}
	t.Cleanup(func() {
		for _, file := range []*os.File{controlRead, controlWrite, responseRead, responseWrite} {
			_ = file.Close()
		}
	})

	if err := supervisor.RenewLease(time.Second, 1); !errors.Is(err, ErrLinuxSupervisorResponseLost) {
		t.Fatalf("RenewLease() after response loss = %v, want response-loss error", err)
	}
	if supervisor.controlWrite != nil {
		t.Fatal("preflight response-loss detection retained the normal control writer")
	}
	_ = controlWrite.Close()
	data, err := io.ReadAll(controlRead)
	if err != nil {
		t.Fatalf("read normal control pipe = %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("normal control pipe received %d bytes after sticky response loss", len(data))
	}
}

func newLinuxSupervisorRecoveryTestServer(t *testing.T, value authority.Supervisor, status, message string, scanSealSequence uint64) <-chan linuxSupervisorRequest {
	t.Helper()
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan linuxSupervisorRequest, 1)
	serverErrors := make(chan error, 1)
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(endpoint)
	})
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.Close()
		var request linuxSupervisorRequest
		if err := readLinuxSupervisorFrame(bufio.NewReader(connection), &request); err != nil {
			serverErrors <- err
			return
		}
		requests <- request
		serverErrors <- writeLinuxSupervisorFrame(connection, linuxSupervisorRecoveryResponse{
			linuxSupervisorResponse: linuxSupervisorResponse{
				Version:            linuxSupervisorProtocolVersion,
				Operation:          request.Operation,
				Status:             status,
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
				ActiveProcesses:    0,
				Error:              message,
			},
			ScanSealSequence: scanSealSequence,
		})
	}()
	return requests
}

func newLinuxSupervisorSealedMirrorFixture(t *testing.T, value authority.Supervisor) (*processGroup, authority.SupervisorHandoff, string, func(authority.SupervisorHandoff) error) {
	t.Helper()
	mirrorIdentity := strings.Repeat("e", authority.TokenBytes*2)
	handoff := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: value.LaunchToken, Secret: value.Secret,
		OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, TargetPID: value.TargetPID,
		TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity,
	}
	monitorDone := make(chan struct{})
	close(monitorDone)
	mirror := &processGroup{
		descendantScanSequence: 1, descendantScanCompletedSequence: 1, descendantScanCompleted: true,
		monitorDone: monitorDone, monitorTerminalReason: descendantMonitorTerminalCleanLeaderAbsent,
		physicalStopProven: true, closeCompleted: true,
	}
	if err := mirror.sealDescendantScanAfterMonitorStop(); err != nil {
		t.Fatalf("seal fixture mirror scan: %v", err)
	}
	scanPrepare := func(request authority.SupervisorHandoff) error {
		update := request.ScanUpdate
		if update == nil || update.Owner != authority.SupervisorScanOwnerMirror || update.Sequence != 1 ||
			update.MirrorIdentity != mirrorIdentity || update.SupervisorPID != value.SupervisorPID ||
			update.SupervisorIdentity != value.SupervisorIdentity {
			return errors.New("mirror scan seal fixture identity mismatch")
		}
		if update.Phase != authority.SupervisorScanPhaseAssertClean && update.Phase != authority.SupervisorScanPhaseAssertAllSealed {
			return errors.New("mirror scan seal fixture phase mismatch")
		}
		return nil
	}
	return mirror, handoff, mirrorIdentity, scanPrepare
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
	if _, ok := supervisor.ContainmentStopReceipt(); ok {
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
	var sealSequence uint64
	var stopErr error
	for attempt := 0; attempt < 8; attempt++ {
		receipt, sealSequence, stopErr = StopPersistedLinuxSupervisorWithScanSeal(*value)
		if stopErr == nil {
			break
		}
		runtime.Gosched()
	}
	if stopErr != nil {
		t.Fatalf("StopPersistedLinuxSupervisorWithScanSeal() after control EOF = %v", stopErr)
	}
	if sealSequence == 0 {
		t.Fatal("authenticated helper recovery response omitted its terminal scan seal sequence")
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

func TestLinuxSupervisorPreCommitAbortUsesPhysicalAbsenceNotReceipt(t *testing.T) {
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
		Command: command, HelperCommand: helper, OwnerContext: "linux-helper:precommit-abort",
		Prepare: func(authority.SupervisorHandoff) error { return nil },
		Bind:    func(authority.SupervisorHandoff, int, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = supervisor.AbortContainment()
		_ = stdinRead.Close()
		_ = stdoutWrite.Close()
		_ = stderrWrite.Close()
	}()
	handoff := supervisor.Handoff()
	if handoff.SupervisorPID <= 0 || handoff.SupervisorIdentity == "" {
		t.Fatalf("pre-commit handoff = %#v, want exact bound helper", handoff)
	}
	if err := supervisor.AbortContainment(); err != nil {
		t.Fatalf("AbortContainment() before commit = %v", err)
	}
	if _, ok := supervisor.ContainmentStopReceipt(); ok {
		t.Fatal("pre-commit abort created a stop receipt")
	}
	proof, err := ProvePreparedSupervisorAborted(handoff, func() error { return nil })
	if err != nil || !proof.ValidFor(handoff) {
		t.Fatalf("exact pre-commit abort proof = %#v, %v", proof, err)
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

func TestProvePreparedSupervisorAbortedAcceptsExactBoundPreResumeLoss(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "supervisor.sock")
	handoff := testLinuxSupervisorUnboundHandoff(endpoint)
	handoff.SupervisorPID = 4321
	handoff.SupervisorIdentity = "linux:v2:helper-creation-identity"
	if err := handoff.Validate(); err != nil {
		t.Fatalf("bound pre-resume handoff: %v", err)
	}
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

	proof, err := ProvePreparedSupervisorAborted(handoff, func() error { return nil })
	if err != nil {
		t.Fatalf("ProvePreparedSupervisorAborted() for exact bound abort: %v", err)
	}
	if !proof.ValidFor(handoff) || proof.SupervisorPID != handoff.SupervisorPID ||
		proof.SupervisorIdentity != handoff.SupervisorIdentity {
		t.Fatalf("bound abort proof = %#v, want exact helper identity and launch", proof)
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

func TestStopPersistedLinuxSupervisorDoesNotReconstructReceiptAfterDeadHelper(t *testing.T) {
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

	if _, err := StopPersistedLinuxSupervisor(value); !errors.Is(err, ErrLinuxSupervisorStopUnproven) {
		t.Fatalf("StopPersistedLinuxSupervisor() after dead helper = %v, want unresolved without a durable receipt", err)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("stop fallback retired endpoint early: %v", err)
	}

	receipt := testLinuxSupervisorStopReceipt(value)
	value.StopReceipt = &receipt
	if err := ReleasePersistedLinuxSupervisor(value); err != nil {
		t.Fatalf("ReleasePersistedLinuxSupervisor() with durable stop receipt = %v", err)
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

func testLinuxSupervisorRecoveryAuthority(t *testing.T) authority.Supervisor {
	t.Helper()
	value := testLinuxSupervisorAuthority(t)
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() for recovery authority: %v", err)
	}
	value.SupervisorPID = os.Getpid()
	value.SupervisorIdentity = identity
	if err := value.Validate(); err != nil {
		t.Fatalf("synthetic recovery authority = %v", err)
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

func TestSanitizeLinuxSupervisorEndpointError(t *testing.T) {
	marker := "/tmp/symmetry-linux-supervisor-known-marker.sock"
	for _, test := range []struct {
		name        string
		cause       error
		unreachable bool
		timeout     bool
	}{
		{name: "refused", cause: syscall.ECONNREFUSED, unreachable: true},
		{name: "missing", cause: os.ErrNotExist, unreachable: true},
		{name: "deadline", cause: os.ErrDeadlineExceeded, timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := &net.OpError{
				Op:     "dial",
				Net:    "unix",
				Source: &net.UnixAddr{Name: marker, Net: "unix"},
				Addr:   &net.UnixAddr{Name: marker, Net: "unix"},
				Err:    &os.PathError{Op: "connect", Path: marker, Err: test.cause},
			}
			err := sanitizeLinuxSupervisorEndpointError(raw)
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("sanitized endpoint error = %q, contains endpoint marker", err)
			}
			if !strings.Contains(err.Error(), "dial unix") || !strings.Contains(err.Error(), "connect") {
				t.Fatalf("sanitized endpoint error = %q, lost operation context", err)
			}
			if !errors.Is(err, test.cause) {
				t.Fatalf("errors.Is(%v, %v) = false", err, test.cause)
			}
			var opErr *net.OpError
			if !errors.As(err, &opErr) || opErr.Addr != nil || opErr.Source != nil {
				t.Fatalf("sanitized endpoint net.OpError = %#v, want path-free typed operation", opErr)
			}
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) || pathErr.Path == marker {
				t.Fatalf("sanitized endpoint PathError = %#v, want safe typed path", pathErr)
			}
			if got := isLinuxSupervisorRecoveryEndpointUnreachable(err); got != test.unreachable {
				t.Fatalf("endpoint unreachable classification = %v, want %v", got, test.unreachable)
			}
			var networkErr net.Error
			if !errors.As(err, &networkErr) || networkErr.Timeout() != test.timeout {
				t.Fatalf("network timeout classification = (%v, %v), want (%v, %v)", networkErr, networkErr != nil && networkErr.Timeout(), true, test.timeout)
			}
		})
	}
}
