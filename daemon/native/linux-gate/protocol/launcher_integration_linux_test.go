//go:build linux && amd64

package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

func TestLinuxOwnerReleaseV2RejectsLegacyAndWrongDescriptorCount(t *testing.T) {
	pipes := newScratchNativePipes(t)
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	var token [32]byte
	token[0] = 1
	packet, err := EncodeRelease(token, command, guard)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(packet[8:10]) != releaseWireVersion ||
		binary.LittleEndian.Uint32(packet[48:52]) != releaseFDCount {
		t.Fatal("RELEASE encoder did not require v2 with exactly four descriptors")
	}
	descriptors := [releaseFDCount]*os.File{pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite, guard.OwnerPipe}
	if _, deadline, err := DecodeRelease(packet, token, descriptors); err != nil || deadline != binary.LittleEndian.Uint64(packet[52:60]) || deadline == 0 {
		t.Fatalf("decode exact guarded RELEASE v2: deadline=%d err=%v", deadline, err)
	}

	legacy := append([]byte(nil), packet...)
	binary.LittleEndian.PutUint16(legacy[8:10], wireVersion)
	if _, _, err := DecodeRelease(legacy, token, descriptors); err == nil {
		t.Fatal("RELEASE decoder accepted legacy v1 without owner guard")
	}
	wrongCount := append([]byte(nil), packet...)
	binary.LittleEndian.PutUint32(wrongCount[48:52], releaseStdioFDCount)
	if _, _, err := DecodeRelease(wrongCount, token, descriptors); err == nil {
		t.Fatal("RELEASE decoder accepted the old three-descriptor contract")
	}
}

func TestOwnerConfigV1CompatibilityAndV2StateDir(t *testing.T) {
	legacy, err := parseOwnerConfig([]byte("version=1\na_uid=1001\na_gid=1002\nb_uid=2001\nb_gid=2002\n"))
	if err != nil || legacy.StateDir != "" || legacy.AUID != 1001 || legacy.BUID != 2001 {
		t.Fatalf("legacy v1 owner config = %+v, err=%v", legacy, err)
	}
	current, err := parseOwnerConfig([]byte("version=2\na_uid=1001\na_gid=1002\nb_uid=2001\nb_gid=2002\nstate_dir=/var/lib/symmetry/state\n"))
	if err != nil || current.StateDir != "/var/lib/symmetry/state" {
		t.Fatalf("v2 owner config StateDir = %q, err=%v", current.StateDir, err)
	}
	for _, stateDir := range []string{"", "relative/state", "/", "/var/lib/../state", "/var//lib/state", "/var/lib/state/", "/var/lib/state\t"} {
		data := []byte("version=2\na_uid=1001\na_gid=1002\nb_uid=2001\nb_gid=2002\nstate_dir=" + stateDir + "\n")
		if _, err := parseOwnerConfig(data); err == nil {
			t.Errorf("v2 owner config accepted noncanonical StateDir %q", stateDir)
		}
	}
}

func TestNativeExecStatusCannotOverrideAPinnedImage(t *testing.T) {
	expected := nativeImageIdentity{Device: 7, Inode: 19, UID: 1001, GID: 1001, Mode: 0o100755, Size: 4096, Links: 1}
	image := &pinnedNativeImage{identity: expected}
	gate := &PreparedGate{nativeImage: image}
	if err := gate.verifyNativeImage(expected.Device, expected.Inode, image); err == nil || err.Error() != "native process pidfd is unavailable" {
		t.Fatalf("exact status did not pass the A-pinned image comparison: %v", err)
	}
	for _, status := range [][2]uint64{{expected.Device + 1, expected.Inode}, {expected.Device, expected.Inode + 1}} {
		err := gate.verifyNativeImage(status[0], status[1], image)
		if err == nil || !strings.Contains(err.Error(), "B exec-attempt status differs from A-pinned approved image") {
			t.Fatalf("tampered B exec-attempt dev/inode was not rejected by A-pinned status comparison: dev=%d ino=%d err=%v", status[0], status[1], err)
		}
	}
}

func TestFakeUserNamespaceDirectoryIsTypedFailClosed(t *testing.T) {
	identity, err := namespaceIdentityFromStat(4242, syscall.Stat_t{
		Dev: 7, Ino: 19, Mode: syscall.S_IFDIR | 0o755,
	})
	var inaccessible *InaccessibleUserNamespaceError
	if identity != (namespaceStat{}) || !errors.Is(err, ErrInaccessibleUserNamespace) || !errors.As(err, &inaccessible) {
		t.Fatalf("fake proc namespace directory was not rejected as typed inaccessible nsfs: identity=%+v err=%v", identity, err)
	}
	if inaccessible.PID != 4242 || inaccessible.Cause == nil {
		t.Fatalf("typed fake nsfs failure lost the exact process/cause: %+v", inaccessible)
	}
}

func TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-created scratch cgroup leaf and installed A/B gate")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if !filepath.IsAbs(leafPath) || filepath.Clean(leafPath) != leafPath ||
		!strings.HasPrefix(leafPath, "/sys/fs/cgroup/") ||
		!strings.HasPrefix(filepath.Base(leafPath), "symmetry-owner-test-") {
		t.Fatal("scratch test leaf must be a normalized /sys/fs/cgroup/symmetry-owner-test-* path")
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("unexpected-cgroup-FD test must run as the installed A service identity")
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open operator-created scratch leaf: %v", err)
	}
	defer unix.Close(leafFD)
	if err := verifyCgroupLeafFD(leafFD, config.AUID); err != nil {
		t.Fatal(err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("scratch leaf must begin empty: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("scratch leaf must begin with no PIDs: empty=%t err=%v", empty, err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("configure scratch test process as the A gate reaper: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0); err != nil {
			t.Errorf("restore scratch test process subreaper setting: %v", err)
		}
	})

	var gate *PreparedGate
	leaderReaped := false
	t.Cleanup(func() {
		if gate == nil {
			return
		}
		if !leaderReaped && gate.PIDFD() != nil {
			if err := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Errorf("kill only the exact scratch gate/native leader by pidfd: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := gate.WaitLeader(ctx)
			cancel()
			if err != nil && !errors.Is(err, unix.ECHILD) {
				t.Errorf("reap scratch leader by pidfd: %v", err)
			}
		}
		if err := gate.Close(); err != nil {
			t.Errorf("close scratch gate descriptors: %v", err)
		}
	})

	prepareContext, cancelPrepare := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelPrepare()
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("unexpected-cgroup-FD gate producer identity is incomplete")
		}
		if target, err := readPIDFDTarget(int(pidfd.Fd())); err != nil || target != identity.PID {
			return fmt.Errorf("unexpected-cgroup-FD root producer pidfd mismatch: target=%d err=%v", target, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prepare scratch A gate: %v", err)
	}
	identity := gate.Identity()
	if err := gate.VerifyLeafMembership(leafFD); err != nil {
		t.Fatalf("parked gate is not in the exact scratch leaf: %v", err)
	}
	if err := assertParkedAGate(identity, config); err != nil {
		t.Fatalf("parked gate inherited an unexpected descriptor or crossed a boundary: %v", err)
	}

	cgroupProcsFD, err := unix.Openat(leafFD, "cgroup.procs", unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open real scratch leaf cgroup.procs as A: %v", err)
	}
	defer unix.Close(cgroupProcsFD)
	statusFlags, err := unix.FcntlInt(uintptr(cgroupProcsFD), unix.F_GETFL, 0)
	if err != nil || statusFlags&unix.O_ACCMODE != unix.O_WRONLY {
		t.Fatalf("scratch cgroup.procs FD is not O_WRONLY: flags=%#x err=%v", statusFlags, err)
	}
	descriptorFlags, err := unix.FcntlInt(uintptr(cgroupProcsFD), unix.F_GETFD, 0)
	if err != nil || descriptorFlags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("scratch cgroup.procs FD is not close-on-exec: flags=%#x err=%v", descriptorFlags, err)
	}
	if target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", cgroupProcsFD)); err != nil || target != filepath.Join(leafPath, "cgroup.procs") {
		t.Fatalf("opened descriptor is not the exact pinned scratch leaf cgroup.procs: target=%q err=%v", target, err)
	}

	sleepPath, err := trustedSleepPath()
	if err != nil {
		t.Fatal(err)
	}
	pipes := newScratchNativePipes(t)
	command := NativeCommand{
		Path: sleepPath, Args: []string{sleepPath, "3600"}, Env: []string{}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	guard, ownerWriter := newScratchReleaseGuard(t, time.Minute)
	packet, err := EncodeRelease(gate.token, command, guard)
	if err != nil {
		t.Fatalf("encode otherwise valid guarded RELEASE: %v", err)
	}
	packetSize := len(packet)
	if binary.LittleEndian.Uint32(packet[48:52]) != releaseFDCount {
		t.Fatalf("guarded RELEASE header declares %d FDs, want %d", binary.LittleEndian.Uint32(packet[48:52]), releaseFDCount)
	}
	// The normal RELEASE has exactly four rights. Add a fifth writable cgroup
	// control FD to exercise the C receiver's ancillary-FD rejection directly.
	oob := unix.UnixRights(
		int(command.Stdin.Fd()), int(command.Stdout.Fd()), int(command.Stderr.Fd()), int(guard.OwnerPipe.Fd()), cgroupProcsFD,
	)
	gate.mu.Lock()
	if gate.state != gatePrepared || gate.release == nil || gate.execAck == nil {
		gate.mu.Unlock()
		clear(packet)
		clear(oob)
		t.Fatal("scratch gate is not parked with its one-use RELEASE and exec-status channels")
	}
	gate.state = gateReleaseAttempted
	gate.releaseWaiting = true
	written, sendErr := unix.SendmsgN(int(gate.release.Fd()), packet, oob, nil, unix.MSG_NOSIGNAL)
	clear(packet)
	clear(oob)
	clear(gate.token[:])
	releaseCloseErr := gate.closeReleaseLocked()
	gate.releaseWaiting = false
	gate.mu.Unlock()
	if sendErr != nil || written != packetSize || releaseCloseErr != nil {
		t.Fatalf("send guarded RELEASE with an unexpected fifth O_CLOEXEC writable cgroup.procs SCM_RIGHTS FD and close channel: wrote=%d/%d send=%v close=%v",
			written, packetSize, sendErr, releaseCloseErr)
	}

	waitContext, cancelWait := context.WithTimeout(context.Background(), 15*time.Second)
	exitInfo, err := gate.WaitLeader(waitContext)
	cancelWait()
	if err != nil {
		t.Fatalf("reap gate rejecting the fifth SCM_RIGHTS FD by exact pidfd: %v", err)
	}
	leaderReaped = true
	if exitInfo.Signo != int32(unix.SIGCHLD) || exitInfo.Code != 1 {
		t.Fatalf("gate did not reject the writable cgroup.procs RELEASE descriptor: signo=%d code=%d", exitInfo.Signo, exitInfo.Code)
	}
	var statusByte [64]byte
	n, oobn, flags, _, err := unix.Recvmsg(int(gate.execAck.Fd()), statusByte[:], nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != 0 || oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC|unix.MSG_EOR) != 0 {
		t.Fatalf("malformed RELEASE reached native exec status or left ancillary data: n=%d oob=%d flags=%#x err=%v", n, oobn, flags, err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("malformed RELEASE left the exact scratch leaf populated: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("malformed RELEASE left PIDs in the exact scratch leaf: empty=%t err=%v", empty, err)
	}
	if err := leafContainsPID(leafFD, identity.PID); err == nil {
		t.Fatal("reaped gate/native PID remains in scratch cgroup.procs")
	}
	if err := ownerWriter.Close(); err != nil {
		t.Fatalf("close scratch owner writer after gate rejection: %v", err)
	}
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped malformed-RELEASE gate: %v", err)
	}
	gate = nil
}

// TestInstalledLinuxOwnerBoundaryScratch exercises the installed setuid
// wrapper, clone3-to-leaf, A-owned namespace map, exact pidfd handoff, one-use
// RELEASE, B-only exec credentials, same-PID transition and absence of leaked
// cgroup/control FDs. It is intentionally opt-in and accepts only a clearly
// named empty scratch leaf; it must never target a production cgroup.
func TestInstalledLinuxOwnerBoundaryScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-created, explicitly named scratch cgroup leaf and installed test launcher")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if !filepath.IsAbs(leafPath) || filepath.Clean(leafPath) != leafPath ||
		!strings.HasPrefix(leafPath, "/sys/fs/cgroup/") ||
		!strings.HasPrefix(filepath.Base(leafPath), "symmetry-owner-test-") {
		t.Fatal("scratch test leaf must be a normalized /sys/fs/cgroup/symmetry-owner-test-* path")
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("scratch boundary test must run as the installed A service identity")
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open operator-created scratch leaf: %v", err)
	}
	defer unix.Close(leafFD)
	if err := verifyCgroupLeafFD(leafFD, config.AUID); err != nil {
		t.Fatal(err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("scratch leaf must begin empty: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("scratch leaf must begin with no PIDs: empty=%t err=%v", empty, err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("configure this scratch test process as the A helper reaper: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0); err != nil {
			t.Errorf("restore scratch test process subreaper setting: %v", err)
		}
	})

	var gate *PreparedGate
	leaderReaped := false
	t.Cleanup(func() {
		if gate == nil {
			return
		}
		if !leaderReaped && gate.PIDFD() != nil {
			if err := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Errorf("kill only the exact scratch gate/native leader by pidfd: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := gate.WaitLeader(ctx)
			cancel()
			if err != nil && !errors.Is(err, unix.ECHILD) {
				t.Errorf("reap scratch leader by pidfd: %v", err)
			}
		}
		if err := gate.Close(); err != nil {
			t.Errorf("close scratch gate descriptors: %v", err)
		}
	})

	helperIdentity, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("capture exact scratch helper identity: %v", err)
	}
	launcherRecorded := false
	var recordedLauncher LauncherIdentity
	prepareContext, cancelPrepare := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelPrepare()
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("transient root producer identity is incomplete")
		}
		if target, err := readPIDFDTarget(int(pidfd.Fd())); err != nil || target != identity.PID {
			return fmt.Errorf("transient root producer pidfd mismatch: target=%d err=%v", target, err)
		}
		status, err := readProcessStatus(identity.PID)
		if err != nil {
			return err
		}
		if status.parent != os.Getpid() || status.uids != [4]uint32{config.AUID, 0, 0, 0} || len(status.groups) != 0 {
			return fmt.Errorf("root READY arrived before the exact direct child was A-parented, root-elevated, and group-cleared: ppid=%d uid=%v groups=%v", status.parent, status.uids, status.groups)
		}
		populated, err := scratchLeafPopulated(leafFD)
		if err != nil || populated {
			return fmt.Errorf("root created a gate before the durable producer hook completed: populated=%t err=%v", populated, err)
		}
		recordedLauncher = identity
		launcherRecorded = true
		return nil
	})
	if err != nil {
		t.Fatalf("prepare A gate in scratch leaf: %v", err)
	}
	if !launcherRecorded {
		t.Fatal("Prepare returned without exposing the root producer fence")
	}
	launcherExit, ok := gate.LauncherExitProof()
	if !ok || launcherExit.Process.PID != recordedLauncher.PID || launcherExit.Process.Identity != recordedLauncher.Identity ||
		launcherExit.Witness.PID != helperIdentity.PID || launcherExit.Witness.Identity != helperIdentity.Identity ||
		!launcherExit.Waited || !launcherExit.Reaped {
		t.Fatalf("successful Prepare lacks exact waited/reaped root producer proof: proof=%+v launcher=%+v helper=%+v", launcherExit, recordedLauncher, helperIdentity)
	}
	assertScratchLauncherExitProof(t, launcherExit, recordedLauncher, helperIdentity)
	identity := gate.Identity()
	if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || identity.UserNSInode == 0 {
		t.Fatalf("parked gate identity is incomplete: %+v", identity)
	}
	if err := gate.VerifyLeafMembership(leafFD); err != nil {
		t.Fatalf("parked gate is not in the exact scratch leaf: %v", err)
	}
	if err := assertParkedAGate(identity, config); err != nil {
		t.Fatalf("pre-RELEASE gate crossed a security boundary: %v", err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || !populated {
		t.Fatalf("A-gate creation did not populate the scratch leaf: populated=%t err=%v", populated, err)
	}

	catPath, err := trustedCatPath()
	if err != nil {
		t.Fatal(err)
	}
	releaseContext, cancelRelease := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelRelease()
	pipes := newScratchNativePipes(t)
	missingPath := fmt.Sprintf("/symmetry-owner-missing-input-%d", os.Getpid())
	command := NativeCommand{
		Path: catPath, Args: []string{catPath, missingPath, "-"},
		Env: []string{}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	releaseGuard, _ := newScratchReleaseGuard(t, time.Minute)
	if err := gate.Release(releaseContext, leafFD, command, releaseGuard, nil); err != nil {
		t.Fatalf("one-use fixed-B RELEASE and same-PID exec: %v", err)
	}
	if err := pipes.closeNativeCopies(); err != nil {
		t.Fatalf("close A copies of transferred native stdio ends: %v", err)
	}
	if err := gate.Release(context.Background(), leafFD, command, releaseGuard, nil); !errors.Is(err, ErrReleaseUsed) {
		t.Fatalf("second RELEASE = %v, want single-use rejection", err)
	}
	if err := assertNativeBProcess(identity, config, catPath, command.Env); err != nil {
		t.Fatalf("native process did not retain the exact B-only boundary: %v", err)
	}
	if err := gate.VerifyLeafMembership(leafFD); err != nil {
		t.Fatalf("native PID left its exact scratch leaf across exec: %v", err)
	}

	stdinPayload := []byte("runner-stdin-roundtrip\n")
	if _, err := pipes.stdinWrite.Write(stdinPayload); err != nil {
		t.Fatalf("write Runner stdin pipe: %v", err)
	}
	if err := pipes.stdinWrite.Close(); err != nil {
		t.Fatalf("close Runner stdin writer: %v", err)
	}
	stdoutPayload, err := io.ReadAll(pipes.stdoutRead)
	if err != nil {
		t.Fatalf("read Runner stdout pipe: %v", err)
	}
	stderrPayload, err := io.ReadAll(pipes.stderrRead)
	if err != nil {
		t.Fatalf("read Runner stderr pipe: %v", err)
	}
	if !bytes.Equal(stdoutPayload, stdinPayload) || !bytes.Contains(stderrPayload, []byte(missingPath)) {
		t.Fatalf("native pipe roundtrip stdout=%q stderr=%q", stdoutPayload, stderrPayload)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := gate.WaitLeader(waitContext); err != nil {
		cancelWait()
		t.Fatalf("reap same-PID native leader by pidfd: %v", err)
	}
	cancelWait()
	leaderReaped = true
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("single-process scratch leaf did not become empty after leader reap: populated=%t err=%v", populated, err)
	}
	if err := leafContainsPID(leafFD, identity.PID); err == nil {
		t.Fatal("reaped native PID remains in scratch cgroup.procs")
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("scratch leaf retained a process after native exit: empty=%t err=%v", empty, err)
	}
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped success-case gate: %v", err)
	}
	gate = nil
	leaderReaped = false

	// An owner-pipe signal present before RELEASE must prevent the long-running
	// native image from executing, even though the RELEASE packet is delivered.
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("owner-pipe gate producer identity is incomplete")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prepare owner-pipe cancellation gate: %v", err)
	}
	guardedSleepPath, err := trustedSleepPath()
	if err != nil {
		t.Fatal(err)
	}
	guardedPipes := newScratchNativePipes(t)
	guardedCommand := NativeCommand{
		Path: guardedSleepPath, Args: []string{guardedSleepPath, "3600"}, Env: []string{}, Dir: "/",
		Stdin: guardedPipes.stdinRead, Stdout: guardedPipes.stdoutWrite, Stderr: guardedPipes.stderrWrite,
	}
	ownerGuard, ownerWriter := newScratchReleaseGuard(t, time.Minute)
	if _, err := ownerWriter.Write([]byte{1}); err != nil {
		t.Fatalf("signal owner-pipe cancellation before RELEASE: %v", err)
	}
	guardedReleaseContext, cancelGuardedRelease := context.WithTimeout(context.Background(), 15*time.Second)
	guardedReleaseErr := gate.Release(guardedReleaseContext, leafFD, guardedCommand, ownerGuard, nil)
	cancelGuardedRelease()
	if guardedReleaseErr == nil {
		t.Fatal("owner-pipe cancellation allowed the native image to exec")
	}
	waitContext, cancelWait = context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := gate.WaitLeader(waitContext); err != nil {
		cancelWait()
		t.Fatalf("reap gate rejected by owner-pipe guard: %v", err)
	}
	cancelWait()
	leaderReaped = true
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("owner-pipe cancellation left a native process in the scratch leaf: populated=%t err=%v", populated, err)
	}
	if err := gate.Close(); err != nil {
		t.Fatalf("close owner-pipe-canceled gate: %v", err)
	}
	gate = nil
	leaderReaped = false

	// A stale one-use token is delivered over a real authenticated release
	// socket. The static gate must exit without replacing itself with sleep.
	launcherRecorded = false
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("second transient root producer identity is incomplete")
		}
		launcherRecorded = true
		return nil
	})
	if err != nil {
		t.Fatalf("prepare stale-token A gate in scratch leaf: %v", err)
	}
	if !launcherRecorded {
		t.Fatal("stale-token gate was created without recording its root producer")
	}
	wrongToken := gate.token
	wrongToken[0] ^= 0xff
	stalePipes := newScratchNativePipes(t)
	staleCommand := NativeCommand{
		Path: catPath, Args: []string{catPath, "-"}, Env: []string{}, Dir: "/",
		Stdin: stalePipes.stdinRead, Stdout: stalePipes.stdoutWrite, Stderr: stalePipes.stderrWrite,
	}
	staleGuard, _ := newScratchReleaseGuard(t, time.Minute)
	stalePacket, err := EncodeRelease(wrongToken, staleCommand, staleGuard)
	if err != nil {
		t.Fatal(err)
	}
	gate.mu.Lock()
	gate.state = gateReleaseAttempted
	writeErr := sendPacketWithRights(int(gate.release.Fd()), stalePacket, []int{
		int(staleCommand.Stdin.Fd()), int(staleCommand.Stdout.Fd()), int(staleCommand.Stderr.Fd()), int(staleGuard.OwnerPipe.Fd()),
	})
	stalePipeCloseErr := stalePipes.closeNativeCopies()
	for index := range stalePacket {
		stalePacket[index] = 0
	}
	clear(gate.token[:])
	closeErr := gate.closeReleaseLocked()
	gate.mu.Unlock()
	if writeErr != nil || closeErr != nil || stalePipeCloseErr != nil {
		t.Fatalf("send one stale release token and close its descriptors: send=%v release=%v stdio=%v", writeErr, closeErr, stalePipeCloseErr)
	}
	waitContext, cancelWait = context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := gate.WaitLeader(waitContext); err != nil {
		cancelWait()
		t.Fatalf("reap gate after stale token rejection: %v", err)
	}
	cancelWait()
	leaderReaped = true
	var statusByte [1]byte
	n, oobn, flags, _, err := unix.Recvmsg(int(gate.execAck.Fd()), statusByte[:], nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != 0 || oobn != 0 || flags&unix.MSG_EOR != 0 {
		t.Fatalf("stale token reached a native exec attempt: n=%d oob=%d flags=%#x err=%v", n, oobn, flags, err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("stale-token gate did not leave scratch leaf empty: populated=%t err=%v", populated, err)
	}
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped stale-token gate: %v", err)
	}
	gate = nil
	leaderReaped = false

	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("EOF-case transient root producer identity is incomplete")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prepare EOF-case A gate in scratch leaf: %v", err)
	}
	if err := gate.Abort(); err != nil {
		t.Fatalf("abort parked gate before RELEASE: %v", err)
	}
	if err := gate.Abort(); err != nil {
		t.Fatalf("repeat gate abort: %v", err)
	}
	if err := gate.Close(); err == nil {
		t.Fatal("closing a live aborted gate must preserve its pidfd until the exact child is reaped")
	}
	waitContext, cancelWait = context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := gate.WaitLeader(waitContext); err != nil {
		cancelWait()
		t.Fatalf("reap gate after release-channel EOF: %v", err)
	}
	cancelWait()
	leaderReaped = true
	n, oobn, flags, _, err = unix.Recvmsg(int(gate.execAck.Fd()), statusByte[:], nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != 0 || oobn != 0 || flags&unix.MSG_EOR != 0 {
		t.Fatalf("release-channel EOF reached native exec: n=%d oob=%d flags=%#x err=%v", n, oobn, flags, err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("EOF-case gate did not leave scratch leaf empty: populated=%t err=%v", populated, err)
	}
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped EOF-case gate: %v", err)
	}
	gate = nil
	leaderReaped = false

	// The helper death case keeps a duplicate of the release writer in this
	// subreaper, so the gate must die from the authenticated owner pidfd/pdeath
	// fence rather than a coincident release-channel EOF.
	ownerPair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	ownerParent := os.NewFile(uintptr(ownerPair[0]), "owner-death-parent")
	ownerChild := os.NewFile(uintptr(ownerPair[1]), "owner-death-child")
	if ownerParent == nil || ownerChild == nil {
		closeFiles(ownerParent, ownerChild)
		t.Fatal("wrap owner-death test channel")
	}
	executable, err := os.Executable()
	if err != nil {
		closeFiles(ownerParent, ownerChild)
		t.Fatal(err)
	}
	helper := exec.Command(executable, "-test.run=^TestLinuxOwnerGateOwnerDeathHelper$")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_OWNER_HELPER=1", "SYMMETRY_LINUX_OWNER_TEST_LEAF="+leafPath)
	helper.ExtraFiles = []*os.File{ownerChild}
	helperPIDFD := -1
	helper.SysProcAttr = &syscall.SysProcAttr{PidFD: &helperPIDFD}
	if err := helper.Start(); err != nil {
		closeFiles(ownerParent, ownerChild)
		t.Fatalf("start isolated A owner-death helper: %v", err)
	}
	_ = ownerChild.Close()
	ownerPacket, ownerFDs, err := receiveTestRights(int(ownerParent.Fd()), 10*time.Second)
	if err != nil {
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("receive exact gate pidfd and held release writer: %v", err)
	}
	if len(ownerPacket) != 20 || string(ownerPacket[:8]) != "SYMLDEAD" || len(ownerFDs) != 2 {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("owner-death bootstrap has invalid frame or descriptors: bytes=%d fds=%d", len(ownerPacket), len(ownerFDs))
	}
	ownerGatePID := int(binary.LittleEndian.Uint32(ownerPacket[8:12]))
	ownerGateStart := binary.LittleEndian.Uint64(ownerPacket[12:20])
	ownerGatePIDFD, releaseHoldFD := ownerFDs[0], ownerFDs[1]
	if target, err := readPIDFDTarget(ownerGatePIDFD); err != nil || target != ownerGatePID {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("owner-death target pidfd mismatch: target=%d err=%v", target, err)
	}
	if start, err := readProcessStartTime(ownerGatePID); err != nil || start != ownerGateStart {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("owner-death gate start identity mismatch: start=%d err=%v", start, err)
	}
	ownerStatus, err := readProcessStatus(ownerGatePID)
	if err != nil || ownerStatus.parent != helper.Process.Pid || ownerStatus.uids != [4]uint32{config.AUID, config.AUID, config.AUID, config.AUID} {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("owner-death gate was not the exact adopted A child: status=%+v err=%v", ownerStatus, err)
	}
	if err := leafContainsPID(leafFD, ownerGatePID); err != nil {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("owner-death gate is not in the exact scratch leaf: %v", err)
	}
	if _, err := unix.Write(int(ownerParent.Fd()), []byte("X")); err != nil {
		closeFDs(ownerFDs)
		stopTestHelper(helper, helperPIDFD, ownerParent)
		_ = ownerParent.Close()
		t.Fatalf("authorize A helper exit: %v", err)
	}
	if err := helper.Wait(); err != nil {
		closeFDs(ownerFDs)
		_ = ownerParent.Close()
		t.Fatalf("A owner helper did not exit cleanly: %v", err)
	}
	_ = unix.Close(helperPIDFD)
	_ = ownerParent.Close()
	waitContext, cancelWait = context.WithTimeout(context.Background(), 10*time.Second)
	if err := waitProcessFD(waitContext, ownerGatePIDFD, time.Now().Add(10*time.Second)); err != nil {
		cancelWait()
		closeFDs(ownerFDs)
		_ = unix.Close(releaseHoldFD)
		t.Fatalf("A-gate survived owner death while its release writer remained open: %v", err)
	}
	var ownerGateExit unix.Siginfo
	if err := unix.Waitid(unix.P_PIDFD, ownerGatePIDFD, &ownerGateExit, unix.WEXITED, nil); err != nil {
		cancelWait()
		closeFDs(ownerFDs)
		_ = unix.Close(releaseHoldFD)
		t.Fatalf("anchor-subreeper did not reap exact owner-death gate by pidfd: %v", err)
	}
	cancelWait()
	leaderReaped = true
	_ = unix.Close(ownerGatePIDFD)
	_ = unix.Close(releaseHoldFD)
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("owner-loss gate did not leave scratch leaf empty: populated=%t err=%v", populated, err)
	}
}

func TestLinuxOwnerGateOwnerDeathHelper(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_HELPER") != "1" {
		t.Skip("internal subprocess witness")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("owner-death witness must run as A")
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leafFD)
	channel := 3
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	gate, err := Prepare(ctx, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("owner-death root producer identity is incomplete")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := gate.Identity()
	var message [20]byte
	copy(message[:8], "SYMLDEAD")
	binary.LittleEndian.PutUint32(message[8:12], uint32(identity.PID))
	binary.LittleEndian.PutUint64(message[12:20], identity.StartTime)
	oob := unix.UnixRights(int(gate.PIDFD().Fd()), int(gate.release.Fd()))
	n, err := unix.SendmsgN(channel, message[:], oob, nil, unix.MSG_NOSIGNAL)
	if err != nil || n != len(message) {
		t.Fatalf("transfer gate pidfd and held release writer: n=%d err=%v", n, err)
	}
	var exitRequest [8]byte
	n, _, flags, _, err := unix.Recvmsg(channel, exitRequest[:], nil, 0)
	if err != nil || n != 1 || flags&unix.MSG_EOR == 0 || exitRequest[0] != 'X' {
		t.Fatalf("receive owner-death instruction: n=%d flags=%#x err=%v", n, flags, err)
	}
	// Do not close gate.release: the parent test retains its transferred writer,
	// so gate shutdown must follow owner-pidfd/PDEATHSIG rather than channel EOF.
}

// TestInstalledLinuxOwnerUnsupportedPeerHostRefusesNative is the expected
// scratch result on WSL2 kernels which prevent A from reading the exact parked
// gate's user-namespace nsfs inode. It is separate from the positive boundary
// test: inaccessible nsfs must never be accepted as PID-only identity.
func TestInstalledLinuxOwnerUnsupportedPeerHostRefusesNative(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-created scratch cgroup leaf and installed A/B launcher")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if !filepath.IsAbs(leafPath) || filepath.Clean(leafPath) != leafPath ||
		!strings.HasPrefix(leafPath, "/sys/fs/cgroup/") ||
		!strings.HasPrefix(filepath.Base(leafPath), "symmetry-owner-test-") {
		t.Fatal("unsupported-host test requires a normalized named scratch cgroup leaf")
	}
	markerPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_NATIVE_MARKER")
	if markerPath == "" {
		markerPath = os.Getenv("SYMMETRY_LINUX_CLONE_TEST_MARKER")
	}
	if markerPath != "" && (!filepath.IsAbs(markerPath) || filepath.Clean(markerPath) != markerPath ||
		!strings.HasPrefix(markerPath, "/opt/symmetry-owner-test/")) {
		t.Fatal("native-exec marker must be a normalized path under /opt/symmetry-owner-test")
	}
	if err := requireScratchMarkerAbsent(markerPath); err != nil {
		t.Fatal(err)
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("unsupported-host test must run as the configured A service identity")
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open operator-created scratch leaf: %v", err)
	}
	defer unix.Close(leafFD)
	if err := verifyCgroupLeafFD(leafFD, config.AUID); err != nil {
		t.Fatal(err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("unsupported-host scratch leaf must begin empty: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("unsupported-host scratch cgroup.procs must begin empty: empty=%t err=%v", empty, err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("configure A as exact parked-gate reaper: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0); err != nil {
			t.Errorf("restore unsupported-host scratch subreaper state: %v", err)
		}
	})

	var gate *PreparedGate
	leaderReaped := false
	t.Cleanup(func() {
		if gate == nil {
			return
		}
		if !leaderReaped && gate.PIDFD() != nil {
			_ = gate.Abort()
			if err := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Errorf("stop only unsupported-host gate by its exact pidfd: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, waitErr := gate.WaitLeader(ctx)
			cancel()
			if waitErr != nil && !errors.Is(waitErr, unix.ECHILD) {
				t.Errorf("reap unsupported-host gate by pidfd: %v", waitErr)
			}
		}
		if err := gate.Close(); err != nil {
			t.Errorf("close unsupported-host gate descriptors: %v", err)
		}
	})

	helperIdentity, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("capture unsupported-host A helper identity: %v", err)
	}
	launcherRecorded := false
	var recordedLauncher LauncherIdentity
	prepareContext, cancelPrepare := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelPrepare()
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("unsupported-host root producer identity is incomplete")
		}
		if target, err := readPIDFDTarget(int(pidfd.Fd())); err != nil || target != identity.PID {
			return fmt.Errorf("unsupported-host root producer pidfd mismatch: target=%d err=%v", target, err)
		}
		recordedLauncher = identity
		launcherRecorded = true
		return nil
	})
	if err == nil {
		t.Fatalf("host accepted user-namespace identity despite the required WSL2 nsfs denial: gate=%+v", gate)
	}
	var inaccessible *InaccessibleUserNamespaceError
	if !errors.Is(err, ErrInaccessibleUserNamespace) || !errors.As(err, &inaccessible) {
		t.Fatalf("unsupported host did not return the typed inaccessible-nsfs failure: %T %v", err, err)
	}
	if gate == nil || gate.PIDFD() == nil || inaccessible.PID != gate.Identity().PID || !launcherRecorded {
		t.Fatalf("unsupported-host refusal omitted exact parked gate/root producer identity: gate=%+v error=%+v", gate, inaccessible)
	}
	launcherExit, ok := gate.LauncherExitProof()
	if !ok || !launcherExit.Waited || !launcherExit.Reaped ||
		launcherExit.Process.PID != recordedLauncher.PID || launcherExit.Witness.PID != helperIdentity.PID {
		t.Fatalf("unsupported-host failure lacks exact root launcher reap proof: %+v", launcherExit)
	}
	assertScratchLauncherExitProof(t, launcherExit, recordedLauncher, helperIdentity)

	identity := gate.Identity()
	if identity.PID <= 1 || identity.StartTime == 0 || identity.UserNSInode == 0 {
		t.Fatalf("unsupported-host parked gate identity is incomplete: %+v", identity)
	}
	if target, err := readPIDFDTarget(int(gate.PIDFD().Fd())); err != nil || target != identity.PID {
		t.Fatalf("unsupported-host gate pidfd changed: target=%d err=%v", target, err)
	}
	if start, err := readProcessStartTime(identity.PID); err != nil || start != identity.StartTime {
		t.Fatalf("unsupported-host gate PID/start changed: start=%d err=%v", start, err)
	}
	status, statusErr := readProcessStatus(identity.PID)
	if statusErr == nil {
		if status.parent != os.Getpid() || status.uids != [4]uint32{config.AUID, config.AUID, config.AUID, config.AUID} ||
			status.gids != [4]uint32{config.AGID, config.AGID, config.AGID, config.AGID} || len(status.groups) != 0 {
			t.Fatalf("unsupported-host refusal crossed into B credentials: %+v", status)
		}
	} else if !errors.Is(statusErr, os.ErrNotExist) {
		t.Fatalf("read unsupported-host parked-gate status: %v", statusErr)
	}
	var pidPoll [1]unix.PollFd
	pidPoll[0] = unix.PollFd{Fd: int32(gate.PIDFD().Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}
	if _, err := unix.Poll(pidPoll[:], 0); err != nil {
		t.Fatalf("poll exact unsupported-host gate pidfd: %v", err)
	}
	if statusErr == nil && pidPoll[0].Revents == 0 {
		imageFD, openErr := unix.Open("/usr/libexec/symmetry/symmetry-linux-gate", unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			t.Fatal(openErr)
		}
		var expected, actual unix.Stat_t
		expectedErr := unix.Fstat(imageFD, &expected)
		actualErr := unix.Stat(fmt.Sprintf("/proc/%d/exe", identity.PID), &actual)
		_ = unix.Close(imageFD)
		if expectedErr != nil {
			t.Fatalf("stat pinned static A gate image: %v", expectedErr)
		}
		if actualErr != nil && !errors.Is(actualErr, unix.EACCES) && !errors.Is(actualErr, unix.EPERM) {
			t.Fatalf("stat parked gate executable on unsupported host: %v", actualErr)
		}
		if actualErr == nil && (expected.Dev != actual.Dev || expected.Ino != actual.Ino) {
			t.Fatalf("unsupported-host parked PID is not the pinned A gate image: expected=%+v actual=%+v stat=%v/%v", expected, actual, expectedErr, actualErr)
		}
		if err := verifyOnlyFixedBMaps(identity.PID, config); err != nil {
			t.Fatalf("unsupported-host gate lost its exact B-only user map: %v", err)
		}
	}
	var statusByte [64]byte
	n, _, flags, _, statusReadErr := unix.Recvmsg(int(gate.execAck.Fd()), statusByte[:], nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if statusReadErr != nil && !errors.Is(statusReadErr, unix.EAGAIN) && !errors.Is(statusReadErr, unix.EWOULDBLOCK) {
		t.Fatalf("read pre-RELEASE native status channel: %v", statusReadErr)
	}
	if statusReadErr == nil && (n != 0 || flags&unix.MSG_EOR != 0) {
		t.Fatalf("unsupported-host gate reported a native/bootstrap exec frame before RELEASE: bytes=%d flags=%#x", n, flags)
	}
	leafPIDs, err := scratchLeafPIDs(leafFD)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafPIDs) > 1 || len(leafPIDs) == 1 && leafPIDs[0] != identity.PID {
		t.Fatalf("unsupported-host refusal left a non-gate process in the scratch leaf: %v", leafPIDs)
	}
	if err := requireScratchMarkerAbsent(markerPath); err != nil {
		t.Fatalf("unsupported-host refusal executed the native marker: %v", err)
	}

	if err := gate.Abort(); err != nil {
		t.Fatalf("close unsupported-host gate's one-use release channel: %v", err)
	}
	killErr := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0)
	if killErr != nil && !errors.Is(killErr, unix.ESRCH) {
		t.Fatalf("stop unsupported-host gate only through its pidfd: %v", killErr)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = gate.WaitLeader(waitContext)
	cancelWait()
	if err != nil {
		t.Fatalf("reap unsupported-host gate by pidfd: %v", err)
	}
	leaderReaped = true
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped unsupported-host gate: %v", err)
	}
	gate = nil
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("unsupported-host refusal failed to empty scratch leaf: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("unsupported-host refusal left a scratch process after reap: empty=%t err=%v", empty, err)
	}
	if err := requireScratchMarkerAbsent(markerPath); err != nil {
		t.Fatalf("unsupported-host cleanup executed the native marker: %v", err)
	}
}

// TestLinuxOwnerNativeOwnerDeathScratch proves the post-RELEASE owner-loss
// fence on an isolated scratch leaf. The helper transfers the native pidfd and
// then exits while the test retains no launch-control writer; the native image
// must die through the re-armed parent-death signal and remain reapable by the
// A subreaper.
func TestLinuxOwnerNativeOwnerDeathScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-created scratch cgroup leaf and installed owner launcher")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if !filepath.IsAbs(leafPath) || filepath.Clean(leafPath) != leafPath ||
		!strings.HasPrefix(leafPath, "/sys/fs/cgroup/") || !strings.HasPrefix(filepath.Base(leafPath), "symmetry-owner-test-") {
		t.Fatal("owner-loss test requires a normalized named scratch cgroup leaf")
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("owner-loss test must run as configured A")
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leafFD)
	if err := verifyCgroupLeafFD(leafFD, config.AUID); err != nil {
		t.Fatal(err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("owner-loss scratch leaf must begin empty: populated=%t err=%v", populated, err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0); err != nil {
			t.Errorf("restore owner-loss scratch subreaper state: %v", err)
		}
	})

	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parentChannel := os.NewFile(uintptr(pair[0]), "native-owner-loss-parent")
	childChannel := os.NewFile(uintptr(pair[1]), "native-owner-loss-child")
	if parentChannel == nil || childChannel == nil {
		closeFiles(parentChannel, childChannel)
		t.Fatal("wrap native owner-loss channel")
	}
	executable, err := os.Executable()
	if err != nil {
		closeFiles(parentChannel, childChannel)
		t.Fatal(err)
	}
	helper := exec.Command(executable, "-test.run=^TestLinuxOwnerNativeOwnerDeathHelper$")
	helper.Env = append(os.Environ(), "SYMMETRY_LINUX_NATIVE_HELPER=1", "SYMMETRY_LINUX_OWNER_TEST_LEAF="+leafPath)
	helper.ExtraFiles = []*os.File{childChannel}
	helperPIDFD := -1
	helper.SysProcAttr = &syscall.SysProcAttr{PidFD: &helperPIDFD}
	if err := helper.Start(); err != nil {
		closeFiles(parentChannel, childChannel)
		t.Fatal(err)
	}
	_ = childChannel.Close()
	var nativePIDFD = -1
	defer func() {
		if nativePIDFD >= 0 {
			if err := unix.PidfdSendSignal(nativePIDFD, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Errorf("stop only the exact scratch native pidfd: %v", err)
			}
			_ = unix.Close(nativePIDFD)
		}
		_ = parentChannel.Close()
		if helperPIDFD >= 0 {
			_ = unix.PidfdSendSignal(helperPIDFD, unix.SIGKILL, nil, 0)
			_ = unix.Close(helperPIDFD)
		}
		if helper.Process != nil {
			_ = helper.Wait()
		}
	}()

	packet, descriptors, err := receiveTestRights(int(parentChannel.Fd()), 30*time.Second)
	if err != nil {
		t.Fatalf("receive post-release native pidfd: %v", err)
	}
	if len(packet) != 20 || string(packet[:8]) != "SYMLNAT1" || len(descriptors) != 1 {
		closeFDs(descriptors)
		t.Fatalf("post-release bootstrap shape invalid: bytes=%d FDs=%d", len(packet), len(descriptors))
	}
	nativePID := int(binary.LittleEndian.Uint32(packet[8:12]))
	nativeStart := binary.LittleEndian.Uint64(packet[12:20])
	nativePIDFD = descriptors[0]
	if target, err := readPIDFDTarget(nativePIDFD); err != nil || target != nativePID {
		t.Fatalf("post-release native pidfd identity mismatch: target=%d err=%v", target, err)
	}
	status, err := readProcessStatus(nativePID)
	if err != nil || status.parent != helper.Process.Pid ||
		status.uids != [4]uint32{config.BUID, config.BUID, config.BUID, config.BUID} ||
		status.gids != [4]uint32{config.BGID, config.BGID, config.BGID, config.BGID} ||
		status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0 || status.noNewPrivs != 1 {
		t.Fatalf("native B process boundary invalid before owner loss: status=%+v err=%v", status, err)
	}
	if start, err := readProcessStartTime(nativePID); err != nil || start != nativeStart {
		t.Fatalf("native start identity mismatch: start=%d err=%v", start, err)
	}
	if err := verifyOnlyFixedBMaps(nativePID, config); err != nil {
		t.Fatal(err)
	}
	if err := leafContainsPID(leafFD, nativePID); err != nil {
		t.Fatalf("native process left exact run leaf: %v", err)
	}
	if _, err := unix.Write(int(parentChannel.Fd()), []byte("X")); err != nil {
		t.Fatalf("instruct A helper to exit while native runs: %v", err)
	}
	if err := helper.Wait(); err != nil {
		t.Fatalf("A helper exited unsuccessfully after RELEASE: %v", err)
	}
	_ = unix.Close(helperPIDFD)
	helperPIDFD = -1
	waitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := waitProcessFD(waitContext, nativePIDFD, time.Now().Add(10*time.Second)); err != nil {
		cancel()
		t.Fatalf("native process survived helper owner loss: %v", err)
	}
	var exitInfo unix.Siginfo
	if err := unix.Waitid(unix.P_PIDFD, nativePIDFD, &exitInfo, unix.WEXITED, nil); err != nil {
		cancel()
		t.Fatalf("A subreaper could not reap owner-loss native pidfd: %v", err)
	}
	cancel()
	_ = unix.Close(nativePIDFD)
	nativePIDFD = -1
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("post-release owner loss did not empty the scratch leaf: populated=%t err=%v", populated, err)
	}
}

func TestLinuxOwnerNativeOwnerDeathHelper(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_NATIVE_HELPER") != "1" {
		t.Skip("internal post-release owner-death subprocess")
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		t.Fatal("native owner-death helper must run as A")
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leafFD)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	gate, err := Prepare(ctx, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("post-release root producer identity is incomplete")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sleepPath, err := trustedSleepPath()
	if err != nil {
		t.Fatal(err)
	}
	pipes := newScratchNativePipes(t)
	command := NativeCommand{
		Path: sleepPath, Args: []string{sleepPath, "3600"}, Env: []string{}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	releaseContext, cancelRelease := context.WithTimeout(context.Background(), 15*time.Second)
	err = gate.Release(releaseContext, leafFD, command, guard, nil)
	cancelRelease()
	if err != nil {
		t.Fatal(err)
	}
	if err := pipes.closeNativeCopies(); err != nil {
		t.Fatalf("close owner-helper copies of transferred native stdio: %v", err)
	}
	identity := gate.Identity()
	var message [20]byte
	copy(message[:8], "SYMLNAT1")
	binary.LittleEndian.PutUint32(message[8:12], uint32(identity.PID))
	binary.LittleEndian.PutUint64(message[12:20], identity.StartTime)
	if n, err := unix.SendmsgN(3, message[:], unix.UnixRights(int(gate.PIDFD().Fd())), nil, unix.MSG_NOSIGNAL); err != nil || n != len(message) {
		t.Fatalf("transfer native pidfd to owner-loss witness: n=%d err=%v", n, err)
	}
	var instruction [1]byte
	n, _, flags, _, err := unix.Recvmsg(3, instruction[:], nil, 0)
	if err != nil || n != 1 || instruction[0] != 'X' || flags&unix.MSG_EOR == 0 {
		t.Fatalf("receive post-release owner-exit instruction: n=%d flags=%#x err=%v", n, flags, err)
	}
	// Return without signaling or reaping native B. The kernel parent-death
	// fence must stop the exact exec image while A's external mirror can observe
	// and reap it by the transferred pidfd.
}

func receiveTestRights(fd int, timeout time.Duration) ([]byte, []int, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	count, err := unix.Poll(fds, int(timeout/time.Millisecond))
	if err != nil {
		return nil, nil, err
	}
	if count == 0 {
		return nil, nil, context.DeadlineExceeded
	}
	data := make([]byte, 64)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, nil, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, nil, errors.New("owner-death bootstrap was truncated")
	}
	var received []int
	if oobn != 0 {
		messages, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return nil, nil, err
		}
		for _, message := range messages {
			fds, err := unix.ParseUnixRights(&message)
			if err != nil {
				closeFDs(received)
				return nil, nil, err
			}
			received = append(received, fds...)
		}
	}
	return data[:n], received, nil
}

func scratchGateProductionAuth() GateProductionAuth {
	return GateProductionAuth{LaunchToken: strings.Repeat("b", 32), Secret: strings.Repeat("c", 64)}
}

func newScratchReleaseGuard(t *testing.T, lease time.Duration) (ReleaseGuard, *os.File) {
	t.Helper()
	ownerPipe, ownerWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create scratch owner pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = ownerPipe.Close()
		_ = ownerWriter.Close()
	})
	return ReleaseGuard{OwnerPipe: ownerPipe, LeaseDeadline: time.Now().Add(lease)}, ownerWriter
}

func assertScratchLauncherExitProof(t *testing.T, proof authority.LinuxCgroupExitProof, launcher, helper LauncherIdentity) {
	t.Helper()
	wantLauncher := authority.LinuxCgroupProcessIdentity{PID: launcher.PID, Identity: launcher.Identity}
	wantHelper := authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}
	if err := proof.Validate(); err != nil || !proof.ValidFor(wantLauncher, wantHelper) {
		t.Fatalf("root launcher exit proof is not the exact waited/reaped child of this helper: proof=%+v err=%v", proof, err)
	}
}

func stopTestHelper(command *exec.Cmd, pidfd int, channel *os.File) {
	if pidfd >= 0 {
		_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	} else if channel != nil {
		_ = channel.Close()
	}
	if command != nil && command.Process != nil {
		_ = command.Wait()
	}
	if pidfd >= 0 {
		_ = unix.Close(pidfd)
	}
}

func assertParkedAGate(identity ProcessIdentity, config OwnerConfig) error {
	status, err := readProcessStatus(identity.PID)
	if err != nil {
		return err
	}
	if status.parent != os.Getpid() || status.uids != [4]uint32{config.AUID, config.AUID, config.AUID, config.AUID} ||
		status.gids != [4]uint32{config.AGID, config.AGID, config.AGID, config.AGID} || len(status.groups) != 0 {
		return fmt.Errorf("A-gate host IDs/groups or adoption changed: ppid=%d uid=%v gid=%v groups=%v", status.parent, status.uids, status.gids, status.groups)
	}
	if err := verifyOnlyFixedBMaps(identity.PID, config); err != nil {
		return err
	}
	if start, err := readProcessStartTime(identity.PID); err != nil || start != identity.StartTime {
		return fmt.Errorf("A-gate PID/start identity changed: start=%d err=%v", start, err)
	}
	gateImage, err := unix.Open("/usr/libexec/symmetry/symmetry-linux-gate", unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(gateImage)
	var gateStat, processStat unix.Stat_t
	if err := unix.Fstat(gateImage, &gateStat); err != nil {
		return err
	}
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", identity.PID), &processStat); err != nil {
		return err
	}
	if gateStat.Dev != processStat.Dev || gateStat.Ino != processStat.Ino {
		return errors.New("native executable ran before RELEASE; parked process is not the pinned static gate")
	}
	fdDir := fmt.Sprintf("/proc/%d/fd", identity.PID)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return err
	}
	pidfds, sockets := 0, 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			return err
		}
		if strings.Contains(target, "cgroup") {
			return fmt.Errorf("parked A gate inherited migration-capable cgroup descriptor %q", target)
		}
		if target == "anon_inode:[pidfd]" {
			pidfds++
		}
		if strings.HasPrefix(target, "socket:[") {
			sockets++
		}
	}
	if pidfds != 1 || sockets != 2 {
		return fmt.Errorf("A-gate FD allowlist differs: pidfds=%d sockets=%d", pidfds, sockets)
	}
	return nil
}

func assertNativeBProcess(identity ProcessIdentity, config OwnerConfig, path string, expectedEnv []string) error {
	if start, err := readProcessStartTime(identity.PID); err != nil || start != identity.StartTime {
		return fmt.Errorf("native PID/start identity changed: start=%d err=%v", start, err)
	}
	status, err := readProcessStatus(identity.PID)
	if err != nil {
		return err
	}
	if status.uids != [4]uint32{config.BUID, config.BUID, config.BUID, config.BUID} ||
		status.gids != [4]uint32{config.BGID, config.BGID, config.BGID, config.BGID} || len(status.groups) != 0 ||
		status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0 || status.noNewPrivs != 1 {
		return fmt.Errorf("native B credential/capability state differs: uid=%v gid=%v groups=%v caps=%x/%x/%x/%x/%x nnp=%d",
			status.uids, status.gids, status.groups, status.capEff, status.capPrm, status.capInh, status.capBnd, status.capAmb, status.noNewPrivs)
	}
	if err := verifyOnlyFixedBMaps(identity.PID, config); err != nil {
		return err
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", identity.PID))
	if err != nil {
		return err
	}
	want, err := os.Stat(path)
	if err != nil {
		return err
	}
	actualStat, actualOK := actual.Sys().(*syscall.Stat_t)
	wantStat, wantOK := want.Sys().(*syscall.Stat_t)
	if !actualOK || !wantOK || actualStat.Dev != wantStat.Dev || actualStat.Ino != wantStat.Ino {
		return errors.New("native image differs from the pinned executable")
	}
	environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", identity.PID))
	if err != nil {
		return err
	}
	wantEnvironment := []byte(strings.Join(expectedEnv, "\x00"))
	if len(expectedEnv) != 0 {
		wantEnvironment = append(wantEnvironment, 0)
	}
	if !bytes.Equal(environ, wantEnvironment) {
		return fmt.Errorf("native process environment differs from the explicit release vector: got=%q want=%q", environ, wantEnvironment)
	}
	fdDir := fmt.Sprintf("/proc/%d/fd", identity.PID)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "0" && entry.Name() != "1" && entry.Name() != "2" {
			return fmt.Errorf("native B inherited descriptor %s beyond stdio", entry.Name())
		}
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			return err
		}
		if strings.Contains(target, "cgroup") || target == "anon_inode:[pidfd]" || strings.HasPrefix(target, "socket:[") {
			return fmt.Errorf("native B inherited A control or cgroup descriptor %q", target)
		}
	}
	return nil
}

func trustedSleepPath() (string, error) {
	for _, candidate := range []string{"/usr/bin/sleep", "/bin/sleep"} {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil && filepath.IsAbs(resolved) {
			if stat, statErr := os.Stat(resolved); statErr == nil && stat.Mode().IsRegular() && stat.Mode()&0111 != 0 {
				return resolved, nil
			}
		}
	}
	return "", errors.New("scratch owner test requires an installed native sleep executable")
}

func trustedCatPath() (string, error) {
	for _, candidate := range []string{"/bin/cat", "/usr/bin/cat"} {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil && filepath.IsAbs(resolved) {
			if stat, statErr := os.Stat(resolved); statErr == nil && stat.Mode().IsRegular() && stat.Mode()&0111 != 0 {
				return resolved, nil
			}
		}
	}
	return "", errors.New("scratch owner test requires an installed native cat ELF")
}

type scratchNativePipes struct {
	stdinRead   *os.File
	stdinWrite  *os.File
	stdoutRead  *os.File
	stdoutWrite *os.File
	stderrRead  *os.File
	stderrWrite *os.File
}

func newScratchNativePipes(t *testing.T) *scratchNativePipes {
	t.Helper()
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create scratch stdin pipe: %v", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite)
		t.Fatalf("create scratch stdout pipe: %v", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite)
		t.Fatalf("create scratch stderr pipe: %v", err)
	}
	pipes := &scratchNativePipes{
		stdinRead: stdinRead, stdinWrite: stdinWrite,
		stdoutRead: stdoutRead, stdoutWrite: stdoutWrite,
		stderrRead: stderrRead, stderrWrite: stderrWrite,
	}
	t.Cleanup(func() {
		_ = closeFiles(pipes.stdinRead, pipes.stdinWrite, pipes.stdoutRead, pipes.stdoutWrite, pipes.stderrRead, pipes.stderrWrite)
	})
	return pipes
}

func (pipes *scratchNativePipes) closeNativeCopies() error {
	if pipes == nil {
		return errors.New("scratch native pipe set is nil")
	}
	var err error
	if pipes.stdinRead != nil {
		err = errors.Join(err, pipes.stdinRead.Close())
		pipes.stdinRead = nil
	}
	if pipes.stdoutWrite != nil {
		err = errors.Join(err, pipes.stdoutWrite.Close())
		pipes.stdoutWrite = nil
	}
	if pipes.stderrWrite != nil {
		err = errors.Join(err, pipes.stderrWrite.Close())
		pipes.stderrWrite = nil
	}
	return err
}

func scratchLeafPopulated(leafFD int) (bool, error) {
	fd, err := unix.Openat(leafFD, "cgroup.events", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	buffer := make([]byte, 4096)
	n, err := unix.Read(fd, buffer)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(buffer[:n]), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			if fields[1] == "0" {
				return false, nil
			}
			if fields[1] == "1" {
				return true, nil
			}
			return false, fmt.Errorf("invalid cgroup.events populated value %q", fields[1])
		}
	}
	return false, errors.New("cgroup.events lacks populated field")
}

func scratchLeafProcsEmpty(leafFD int) (bool, error) {
	fd, err := unix.Openat(leafFD, "cgroup.procs", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	var data [4096]byte
	n, err := unix.Read(fd, data[:])
	if err != nil {
		return false, err
	}
	return len(strings.Fields(string(data[:n]))) == 0, nil
}

func scratchLeafPIDs(leafFD int) ([]int, error) {
	fd, err := unix.Openat(leafFD, "cgroup.procs", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	buffer := make([]byte, 64<<10)
	n, err := unix.Read(fd, buffer)
	if err != nil {
		return nil, err
	}
	if n == len(buffer) {
		return nil, errors.New("scratch cgroup.procs exceeds its bounded read")
	}
	fields := strings.Fields(string(buffer[:n]))
	pids := make([]int, len(fields))
	seen := make(map[int]struct{}, len(fields))
	for index, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 {
			return nil, errors.New("scratch cgroup.procs contains an invalid PID")
		}
		if _, duplicate := seen[pid]; duplicate {
			return nil, errors.New("scratch cgroup.procs repeats a PID")
		}
		seen[pid] = struct{}{}
		pids[index] = pid
	}
	return pids, nil
}

func requireScratchMarkerAbsent(path string) error {
	if path == "" {
		return nil
	}
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect unsupported-host native marker: %w", err)
	}
	return errors.New("native execution marker exists on an unsupported peer host")
}
