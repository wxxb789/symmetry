//go:build linux && amd64

// symmetry-linux-launcher-core is the static, root-owned stage entered only
// through the small setuid UID/FD/environment sanitizer in wrapper.c. It is
// intentionally transient and has no daemon or journal authority.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	controlFD            = 3
	leafFD               = 4
	releaseFD            = 5
	execStatusFD         = 6
	gateImagePath        = "/usr/libexec/symmetry/symmetry-linux-gate"
	gateReadyTimeout     = 10 * time.Second
	gateKillTimeout      = 3 * time.Second
	launchRequestTimeout = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	config, err := protocol.ReadInstalledOwnerConfig()
	if err != nil {
		return err
	}
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if ruid != int(config.AUID) || euid != 0 || suid != 0 || rgid != int(config.AGID) || egid != int(config.AGID) || sgid != int(config.AGID) {
		return errors.New("transient launcher core is not the configured root/A caller")
	}
	controlType, err := unix.GetsockoptInt(controlFD, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || controlType != unix.SOCK_SEQPACKET {
		return fmt.Errorf("authenticate inherited A control socket type: type=%d err=%v", controlType, err)
	}
	peer, err := unix.GetsockoptUcred(controlFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != int32(os.Getppid()) || peer.Uid != config.AUID || peer.Gid != config.AGID {
		return fmt.Errorf("authenticate inherited A control peer: %w", errorOr(err, errors.New("SO_PEERCRED does not match direct A parent")))
	}
	reportReadyFailure := func(stage protocol.LauncherReadyFailureStage, cause error) error {
		sendErr := sendFrame(controlFD, protocol.EncodeLauncherReadyFailure(stage))
		return errors.Join(fmt.Errorf("root launcher READY stage %s: %w", stage.String(), cause), sendErr)
	}
	if wd, err := os.Getwd(); err != nil || wd != "/" {
		return reportReadyFailure(protocol.LauncherReadyFailureEnvironment,
			fmt.Errorf("transient launcher working directory is not fixed root: %q (%v)", wd, err))
	}
	if groups, err := unix.Getgroups(); err != nil || len(groups) != 0 {
		return reportReadyFailure(protocol.LauncherReadyFailureGroups,
			fmt.Errorf("transient root phase did not clear supplementary groups: %v groups=%v", err, groups))
	}
	// Go appends GOTRACEBACK=none when the root/A image starts with AT_SECURE.
	// The C wrapper still passes only PATH and LANG to execveat.
	if env := os.Environ(); (len(env) != 2 && (len(env) != 3 || env[2] != "GOTRACEBACK=none")) ||
		env[0] != "PATH=/usr/bin:/bin" || env[1] != "LANG=C" {
		return reportReadyFailure(protocol.LauncherReadyFailureEnvironment, errors.New("transient launcher environment is not the fixed minimal environment"))
	}
	if err := setParentDeathSignal(os.Getppid()); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureParentFence, err)
	}
	if err := verifyInheritedDescriptors(config); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureDescriptors, err)
	}
	if err := verifySocketPeer(releaseFD, peer); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailurePeer, fmt.Errorf("authenticate inherited release endpoint: %w", err))
	}
	if err := verifySocketPeer(execStatusFD, peer); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailurePeer, fmt.Errorf("authenticate inherited exec-status endpoint: %w", err))
	}
	if err := verifyCgroupLeaf(leafFD, config.AUID); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureLeaf, err)
	}
	launcherIdentity, err := protocol.CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureIdentity,
			fmt.Errorf("capture exact root launcher identity before readiness: %w", err))
	}
	ready, err := protocol.EncodeLauncherReady(launcherIdentity)
	if err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureEncoding,
			fmt.Errorf("encode exact root launcher readiness: %w", err))
	}
	if err := sendFrame(controlFD, ready); err != nil {
		return fmt.Errorf("send group-cleared root launcher readiness to A: %w", err)
	}

	// Do not enter launchAChildGate until A sends its one launch request. A's
	// helper sends that request only after its durable BindLauncher/ArmGrant hook.
	requestPacket, fds, err := receiveControlRequest(controlFD, time.Now().Add(launchRequestTimeout))
	if err != nil {
		return err
	}
	if len(fds) != 0 {
		closeFDs(fds)
		return errors.New("A launch request carried unexpected ancillary descriptors")
	}
	launchRequest, err := protocol.DecodeLaunchRequest(requestPacket)
	clear(requestPacket)
	if err != nil {
		_ = sendResult(controlFD, protocol.LaunchResult{Status: protocol.LaunchStatusRejected, Errno: errnoNumber(err)}, nil)
		return err
	}
	defer clear(launchRequest.Secret[:])
	return launchAChildGate(controlFD, leafFD, releaseFD, execStatusFD, peer, config, launchRequest)
}

func launchAChildGate(control, leaf, release, execStatus int, peer *unix.Ucred, config protocol.OwnerConfig, request protocol.LaunchRequest) error {
	defer clear(request.Secret[:])
	producer, identityErr := protocol.CaptureLauncherIdentity(os.Getpid())
	if identityErr != nil {
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(identityErr), Token: request.ReleaseToken}, nil)
		return fmt.Errorf("capture exact root producer identity before gate clone: %w", identityErr)
	}
	gateImage, err := openTrustedGateImage()
	if err != nil {
		return sendNeverProduced(control, request, producer, err)
	}

	ownerPIDFD, err := unix.PidfdOpen(int(peer.Pid), 0)
	if err != nil {
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("open authenticated A owner pidfd: %w", err))
	}
	if os.Getppid() != int(peer.Pid) {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, errors.New("A owner exited before gate creation"))
	}
	if err := unix.PidfdSendSignal(ownerPIDFD, 0, nil, 0); err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("probe exact A owner pidfd: %w", err))
	}

	syncFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("create gate mapping channel: %w", err))
	}
	syncParent, syncChild := syncFDs[0], syncFDs[1]
	defer unix.Close(syncParent)

	devNull, err := openNullDevice()
	if err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(syncParent)
		_ = unix.Close(syncChild)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, err)
	}

	gatePIDFD := -1
	gateFile := os.NewFile(uintptr(gateImage), "symmetry-linux-gate-image")
	releaseFile := os.NewFile(uintptr(release), "symmetry-linux-gate-release")
	syncFile := os.NewFile(uintptr(syncChild), "symmetry-linux-gate-map-channel")
	ownerFile := os.NewFile(uintptr(ownerPIDFD), "symmetry-linux-gate-owner-pidfd")
	statusFile := os.NewFile(uintptr(execStatus), "symmetry-linux-gate-exec-status")
	stdio := os.NewFile(uintptr(devNull), "symmetry-linux-gate-stdio")
	if gateFile == nil || releaseFile == nil || syncFile == nil || ownerFile == nil || statusFile == nil || stdio == nil {
		closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio)
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(syncParent)
		return sendNeverProduced(control, request, producer, errors.New("wrap static gate launch descriptors"))
	}
	cmd := exec.Command("/proc/self/fd/3")
	cmd.Args = []string{"symmetry-linux-gate"}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	cmd.Dir = "/"
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio, stdio, stdio
	cmd.ExtraFiles = []*os.File{gateFile, releaseFile, syncFile, ownerFile, statusFile}
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: leaf, PidFD: &gatePIDFD}
	if err := cmd.Start(); err != nil {
		clear(request.Secret[:])
		closeErr := closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio)
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(err), Token: request.ReleaseToken}, nil)
		return errors.Join(fmt.Errorf("clone static A gate result is uncertain: %w", err), closeErr)
	}
	clear(request.Secret[:])
	// Parent-side copies would defeat EOF-based owner/release detection and
	// close-on-exec status. The child has its own exact inherited descriptors.
	if err := closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("close transient root copies of gate FDs: %w", err))
	}
	if gatePIDFD < 0 {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, errors.New("clone3 did not return the required gate pidfd"))
	}
	if os.Getppid() != int(peer.Pid) {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, errors.New("A owner exited during gate creation"))
	}

	deadline := time.Now().Add(gateReadyTimeout)
	ready, fds, err := receiveFrame(syncParent, 16, deadline)
	if err != nil || len(fds) != 0 || protocol.ValidateGateReady(ready) != nil {
		closeFDs(fds)
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("gate did not reach A user namespace setup: %w", errorOr(err, protocol.ValidateGateReady(ready))))
	}
	if err := writeOnlyBMaps(cmd.Process.Pid, config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("write exact B-only user namespace maps: %w", err))
	}
	if err := verifyMappedGate(cmd.Process.Pid, int(os.Getpid()), int(peer.Pid), config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("verify A gate identity and B-only maps: %w", err))
	}
	if err := sendFrame(syncParent, protocol.EncodeGateMap(config, request.ReleaseToken)); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("authorize mapped A gate: %w", err))
	}
	parked, fds, err := receiveFrame(syncParent, 16, deadline)
	if err != nil || len(fds) != 0 || protocol.ValidateGateParked(parked) != nil {
		closeFDs(fds)
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("A gate did not park before RELEASE: %w", errorOr(err, protocol.ValidateGateParked(parked))))
	}
	if err := waitPeerEOF(syncParent, deadline); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("verify gate mapping-channel retirement: %w", err))
	}
	if err := unix.PidfdSendSignal(gatePIDFD, 0, nil, 0); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("probe parked gate pidfd: %w", err))
	}
	identity, err := processIdentity(cmd.Process.Pid, int(peer.Pid))
	if err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("capture exact A gate identity: %w", err))
	}
	if err := verifyMappedGate(cmd.Process.Pid, int(os.Getpid()), int(peer.Pid), config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("recheck A gate before pidfd handoff: %w", err))
	}
	if err := sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusReady, Token: request.ReleaseToken, Identity: identity}, []int{gatePIDFD}); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("transfer exact gate pidfd to A helper: %w", err))
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release transient launcher child handle after pidfd transfer: %w", err)
	}
	return nil
}

func sendNeverProduced(control int, request protocol.LaunchRequest, producer protocol.LauncherIdentity, cause error) error {
	launcher := authority.LinuxCgroupProcessIdentity{PID: producer.PID, Identity: producer.Identity}
	authenticator, signErr := protocol.SignGateNeverProducedAuthenticatorKey(request.Secret[:], request.LaunchToken, launcher)
	if signErr != nil {
		result := protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(errors.Join(cause, signErr)), Token: request.ReleaseToken}
		return errors.Join(cause, signErr, sendResult(control, result, nil))
	}
	result := protocol.LaunchResult{
		Status:        protocol.LaunchStatusNeverProduced,
		Errno:         errnoNumber(cause),
		Token:         request.ReleaseToken,
		Authenticator: authenticator,
	}
	return errors.Join(cause, sendResult(control, result, nil))
}

func failAfterStart(control int, cmd *exec.Cmd, pidfd int, token [32]byte, cause error) error {
	if pidfd < 0 {
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(cause), Token: token}, nil)
		return cause
	}
	_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	if waitPIDFD(pidfd, time.Now().Add(gateKillTimeout)) == nil {
		if cmd.Process != nil {
			_ = cmd.Wait()
		}
		_ = unix.Close(pidfd)
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusRejected, Errno: errnoNumber(cause), Token: token}, nil)
		return cause
	}
	identity, identityErr := processIdentity(cmd.Process.Pid, os.Getppid())
	if identityErr != nil {
		identity = protocol.ProcessIdentity{PID: cmd.Process.Pid, OwnerPID: os.Getppid()}
	}
	sendErr := sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(cause), Token: token, Identity: identity}, []int{pidfd})
	if err := cmd.Process.Release(); err != nil {
		sendErr = errors.Join(sendErr, err)
	}
	return errors.Join(cause, sendErr)
}

func verifyInheritedDescriptors(config protocol.OwnerConfig) error {
	for _, fd := range []int{controlFD, releaseFD, execStatusFD} {
		var socketType int
		socketType, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
		if err != nil || socketType != unix.SOCK_SEQPACKET {
			return fmt.Errorf("inherited FD %d is not SOCK_SEQPACKET: type=%d err=%v", fd, socketType, err)
		}
	}
	if err := verifyCgroupLeaf(leafFD, config.AUID); err != nil {
		return err
	}
	return nil
}

func verifySocketPeer(fd int, expected *unix.Ucred) error {
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return err
	}
	if peer.Pid != expected.Pid || peer.Uid != expected.Uid || peer.Gid != expected.Gid {
		return fmt.Errorf("socket peer differs from authenticated A process: got %#v want %#v", peer, expected)
	}
	return nil
}

func verifyCgroupLeaf(fd int, aUID uint32) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return fmt.Errorf("stat inherited cgroup leaf filesystem: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat inherited cgroup leaf: %w", err)
	}
	if uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != aUID {
		return errors.New("inherited leaf FD is not an A-owned cgroup-v2 directory")
	}
	return nil
}

func openTrustedGateImage() (int, error) {
	fd, err := unix.Open(gateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open pinned static gate image: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("stat pinned gate image: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0 ||
		stat.Mode&0111 == 0 || stat.Nlink != 1 {
		_ = unix.Close(fd)
		return -1, errors.New("gate image is not singly-linked, root-owned, executable, and immutable to A/B")
	}
	return fd, nil
}

func openNullDevice() (int, error) {
	fd, err := unix.Open("/dev/null", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open fixed /dev/null: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(stat.Rdev)) != 1 || unix.Minor(uint64(stat.Rdev)) != 3 {
		_ = unix.Close(fd)
		return -1, errors.New("fixed /dev/null is not the null character device")
	}
	return fd, nil
}

func writeOnlyBMaps(pid int, config protocol.OwnerConfig) error {
	procFD, err := unix.Open(fmt.Sprintf("/proc/%d", pid), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open exact gate proc directory: %w", err)
	}
	defer unix.Close(procFD)
	if err := writeProcAt(procFD, "uid_map", fmt.Sprintf("1 %d 1\n", config.BUID)); err != nil {
		return fmt.Errorf("write sole B uid mapping: %w", err)
	}
	// The hosted system probe established that setgroups cannot be cleared by A
	// from inside this user namespace, even before gid_map. The root wrapper has
	// already emptied groups before the gate dropped to A; root now denies
	// setgroups before writing the B-only gid_map.
	if err := writeProcAt(procFD, "setgroups", "deny\n"); err != nil {
		return fmt.Errorf("deny user-namespace setgroups before gid_map: %w", err)
	}
	if err := writeProcAt(procFD, "gid_map", fmt.Sprintf("1 %d 1\n", config.BGID)); err != nil {
		return fmt.Errorf("write sole B gid mapping: %w", err)
	}
	return nil
}

func writeProcAt(procFD int, name, value string) error {
	fd, err := unix.Openat(procFD, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	n, writeErr := unix.Write(fd, []byte(value))
	closeErr := unix.Close(fd)
	if writeErr != nil || n != len(value) || closeErr != nil {
		return errors.Join(writeErr, closeErr, shortWrite(n, len(value)))
	}
	return nil
}

func verifyMappedGate(pid, launcherPID, ownerPID int, config protocol.OwnerConfig) error {
	status, err := readStatus(pid)
	if err != nil {
		return err
	}
	wantedUIDs := [4]uint32{config.AUID, config.AUID, config.AUID, config.AUID}
	wantedGIDs := [4]uint32{config.AGID, config.AGID, config.AGID, config.AGID}
	if status.uids != wantedUIDs || status.gids != wantedGIDs || len(status.groups) != 0 || status.parent != launcherPID {
		return fmt.Errorf("gate did not remain A with empty groups under root launcher: uid=%v gid=%v groups=%v ppid=%d",
			status.uids, status.gids, status.groups, status.parent)
	}
	if err := verifyMap(pid, "uid_map", fmt.Sprintf("1 %d 1", config.BUID)); err != nil {
		return err
	}
	if err := verifyMap(pid, "gid_map", fmt.Sprintf("1 %d 1", config.BGID)); err != nil {
		return err
	}
	setgroups, err := readProcFile(pid, "setgroups")
	if err != nil {
		return err
	}
	if strings.TrimSpace(setgroups) != "deny" {
		return errors.New("gate user namespace does not deny setgroups")
	}
	userNS, err := namespaceStat(pid)
	if err != nil {
		return err
	}
	ownerNS, err := namespaceStat(ownerPID)
	if err != nil {
		return err
	}
	if userNS == ownerNS {
		return errors.New("gate did not create a distinct A-owned user namespace")
	}
	return nil
}

type processStatus struct {
	uids   [4]uint32
	gids   [4]uint32
	groups []uint32
	parent int
}

func readStatus(pid int) (processStatus, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return processStatus{}, fmt.Errorf("read exact gate status: %w", err)
	}
	var status processStatus
	seen := uint8(0)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		parseIDs := func(destination *[4]uint32) error {
			if len(fields) != 4 {
				return errors.New("process status credential vector is malformed")
			}
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return err
				}
				destination[index] = uint32(parsed)
			}
			return nil
		}
		switch key {
		case "Uid":
			if err := parseIDs(&status.uids); err != nil {
				return processStatus{}, err
			}
			seen |= 1
		case "Gid":
			if err := parseIDs(&status.gids); err != nil {
				return processStatus{}, err
			}
			seen |= 2
		case "Groups":
			status.groups = make([]uint32, len(fields))
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return processStatus{}, err
				}
				status.groups[index] = uint32(parsed)
			}
			seen |= 4
		case "PPid":
			if len(fields) != 1 {
				return processStatus{}, errors.New("process PPid is malformed")
			}
			parent, err := strconv.Atoi(fields[0])
			if err != nil || parent <= 0 {
				return processStatus{}, errors.New("process PPid is invalid")
			}
			status.parent = parent
			seen |= 8
		}
	}
	if seen != 15 {
		return processStatus{}, errors.New("gate status lacks UID/GID/Groups/PPid")
	}
	return status, nil
}

type nsIdentity struct{ dev, ino uint64 }

func namespaceStat(pid int) (nsIdentity, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/user", pid), &stat); err != nil {
		return nsIdentity{}, fmt.Errorf("stat gate user namespace: %w", err)
	}
	return nsIdentity{dev: uint64(stat.Dev), ino: stat.Ino}, nil
}

func verifyMap(pid int, name, expected string) error {
	value, err := readProcFile(pid, name)
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(value), " ") != expected {
		return fmt.Errorf("gate %s is not the exact fixed-B map: %q", name, value)
	}
	return nil
}

func readProcFile(pid int, name string) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("read gate %s: %w", name, err)
	}
	return string(data), nil
}

func processIdentity(pid, ownerPID int) (protocol.ProcessIdentity, error) {
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return protocol.ProcessIdentity{}, fmt.Errorf("read gate process stat: %w", err)
	}
	end := bytes.LastIndexByte(statData, ')')
	if end < 0 || end+1 >= len(statData) {
		return protocol.ProcessIdentity{}, errors.New("gate process stat is malformed")
	}
	fields := strings.Fields(string(statData[end+1:]))
	if len(fields) <= 19 {
		return protocol.ProcessIdentity{}, errors.New("gate process start time is missing")
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || startTime == 0 {
		return protocol.ProcessIdentity{}, errors.New("gate process start time is invalid")
	}
	namespace, err := namespaceStat(pid)
	if err != nil {
		return protocol.ProcessIdentity{}, err
	}
	status, err := readStatus(pid)
	if err != nil {
		return protocol.ProcessIdentity{}, err
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return protocol.ProcessIdentity{}, fmt.Errorf("read system boot identity: %w", err)
	}
	return protocol.ProcessIdentity{
		PID:          pid,
		StartTime:    startTime,
		BootID:       strings.TrimSpace(string(bootID)),
		UserNSDevice: namespace.dev,
		UserNSInode:  namespace.ino,
		HostUID:      status.uids[0],
		HostGID:      status.gids[0],
		OwnerPID:     ownerPID,
	}, nil
}

func receiveControlRequest(fd int, deadline time.Time) ([]byte, []int, error) {
	if err := waitFD(fd, deadline); err != nil {
		return nil, nil, fmt.Errorf("wait for one authenticated A launch request: %w", err)
	}
	data := make([]byte, protocol.LaunchRequestSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, nil, fmt.Errorf("receive authenticated A launch request: %w", err)
	}
	var fds []int
	if oobn > 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, nil, err
		}
	}
	if n != protocol.LaunchRequestSize || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		closeFDs(fds)
		return nil, nil, errors.New("A launch request has invalid size or truncation flags")
	}
	return data[:n], fds, nil
}

func waitFD(fd int, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("authenticated A launch request deadline expired")
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(pollFDs, int((remaining+time.Millisecond-1)/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
}

func waitPeerEOF(fd int, deadline time.Time) error {
	if err := waitFD(fd, deadline); err != nil {
		return err
	}
	var packet [1]byte
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, packet[:], oob, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if oobn != 0 {
		if received, parseErr := parseRights(oob[:oobn]); parseErr == nil {
			closeFDs(received)
		} else {
			err = errors.Join(err, parseErr)
		}
	}
	if err != nil || n != 0 || flags&unix.MSG_EOR != 0 {
		return fmt.Errorf("gate mapping channel did not close cleanly: n=%d flags=%#x err=%v", n, flags, err)
	}
	return nil
}

func receiveFrame(fd, max int, deadline time.Time) ([]byte, []int, error) {
	if err := waitFD(fd, deadline); err != nil {
		return nil, nil, err
	}
	data := make([]byte, max)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, nil, err
	}
	var fds []int
	if oobn != 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, nil, err
		}
	}
	if n == 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		closeFDs(fds)
		return nil, nil, errors.New("gate mapping frame is empty or truncated")
	}
	return data[:n], fds, nil
}

func parseRights(oob []byte) ([]int, error) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			closeFDs(fds)
			return nil, errors.New("gate mapping channel carried unexpected ancillary data")
		}
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			closeFDs(fds)
			return nil, err
		}
		fds = append(fds, rights...)
	}
	return fds, nil
}

func sendFrame(fd int, packet []byte) error {
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("gate mapping seqpacket short write: %d/%d", n, len(packet))
	}
	return nil
}

func sendResult(fd int, result protocol.LaunchResult, passedFDs []int) error {
	packet, err := protocol.EncodeLaunchResult(result)
	if err != nil {
		return err
	}
	var oob []byte
	if len(passedFDs) > 0 {
		if len(passedFDs) != 1 {
			return errors.New("root launcher may transfer exactly one gate pidfd")
		}
		oob = unix.UnixRights(passedFDs...)
	}
	n, err := unix.SendmsgN(fd, packet, oob, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("root launcher result short write: %d/%d", n, len(packet))
	}
	return nil
}

func openProcDir(pid int) (int, error) {
	return unix.Open(fmt.Sprintf("/proc/%d", pid), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func waitPIDFD(pidfd int, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("pidfd wait deadline exceeded")
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		millis := int((remaining + time.Millisecond - 1) / time.Millisecond)
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(fds, millis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
}

func setParentDeathSignal(expectedParent int) error {
	if expectedParent <= 1 {
		return errors.New("transient launcher has no valid A parent")
	}
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		return fmt.Errorf("arm transient launcher parent-death fence: %w", err)
	}
	if os.Getppid() != expectedParent {
		return errors.New("A owner exited while arming transient launcher parent-death fence")
	}
	return nil
}

func errnoNumber(err error) uint32 {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno > 0 {
		return uint32(errno)
	}
	return uint32(unix.EPERM)
}

func errorOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func shortWrite(actual, want int) error {
	if actual == want {
		return nil
	}
	return fmt.Errorf("short proc control write: %d/%d", actual, want)
}

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
}

func closeFiles(files ...*os.File) error {
	var closeErr error
	for _, file := range files {
		if file != nil {
			closeErr = errors.Join(closeErr, file.Close())
		}
	}
	return closeErr
}

// These local binary helpers keep the root stage's proc writes fixed-format;
// no command path, environment, cwd, or B-controlled bytes enter this phase.
