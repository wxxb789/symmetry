//go:build linux && amd64

// symmetry-linux-clone-bootstrap is the fixed, root-installed B-owned stage
// entered by the A gate only after its one guarded RELEASE and complete B drop.
// It creates a standalone checkout from the A-owned immutable bundle, waits
// for A's durable checkout ACK, then execs the approved native image in this
// same PID. It never has root or A host credentials.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const bootstrapArgv0 = "symmetry-linux-clone-bootstrap"

type bootstrapArgs struct {
	requestFD   int
	gitFD       int
	ownerPIDFD  int
	statusFD    int
	ownerPipeFD int
	stdinFD     int
	stdoutFD    int
	stderrFD    int
	bUID        uint32
	bGID        uint32
}

type phaseError struct {
	stage linuxgate.CloneBootstrapFailureStage
	errno uint32
	cause error
}

func (failure *phaseError) Error() string {
	if failure == nil || failure.cause == nil {
		return "Linux clone bootstrap failed"
	}
	return failure.cause.Error()
}

func (failure *phaseError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func fail(stage linuxgate.CloneBootstrapFailureStage, err error) error {
	if err == nil {
		err = errors.New("Linux clone bootstrap phase failed")
	}
	return &phaseError{stage: stage, errno: bootstrapErrno(err), cause: err}
}

func main() {
	statusFD := statusFDFromArgs(os.Args)
	if err := runBootstrap(os.Args); err != nil {
		var failure *phaseError
		if !errors.As(err, &failure) {
			failure = &phaseError{stage: linuxgate.CloneFailureRequest, errno: uint32(unix.EPROTO), cause: err}
		}
		if statusFD >= 0 {
			if packet, encodeErr := linuxgate.EncodeCloneBootstrapFailure(failure.stage, failure.errno); encodeErr == nil {
				_ = sendStatusPacket(statusFD, packet)
				clear(packet)
			}
		}
	}
	os.Exit(1)
}

func statusFDFromArgs(argv []string) int {
	// argv[4] is the fixed exec-status descriptor in the C gate ABI. This
	// best-effort parse is only for a bounded failure frame; runBootstrap
	// validates the complete vector before using any descriptor.
	if len(argv) <= 4 {
		return -1
	}
	fd, err := parseInheritedFD(argv[4])
	if err != nil {
		return -1
	}
	return fd
}

func runBootstrap(argv []string) error {
	if len(argv) != 11 || argv[0] != bootstrapArgv0 {
		return fail(linuxgate.CloneFailureRequest, errors.New("clone bootstrap argv shape is invalid"))
	}
	args, err := parseBootstrapArgs(argv[1:])
	if err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	if err := verifyBIdentity(args.bUID, args.bGID); err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	if err := verifyControlDescriptors(args); err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	if err := markUnexpectedDescriptorsCloseOnExec(args); err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	if err := markInheritedDescriptorsCloseOnExec(args); err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}

	packet, err := readSealedCloneRequest(args.requestFD)
	if err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	command, leaseDeadlineNS, decodeErr := linuxgate.DecodeCloneBootstrapRequest(packet)
	clear(packet)
	_ = unix.Close(args.requestFD)
	if decodeErr != nil {
		return fail(linuxgate.CloneFailureRequest, decodeErr)
	}
	if command.Clone == nil {
		return fail(linuxgate.CloneFailureRequest, errors.New("clone bootstrap request has no RunJournal grant"))
	}
	grant := command.Clone.Clone()
	if err := grant.Validate(); err != nil {
		return fail(linuxgate.CloneFailureRequest, fmt.Errorf("validate exact clone grant: %w", err))
	}
	// DecodeCloneBootstrapRequest already validates the command fields without
	// requiring the A-owned SCM_RIGHTS pipe handles used only by the C gate.

	guard, err := newOwnerLeaseGuard(args.ownerPIDFD, args.ownerPipeFD, leaseDeadlineNS)
	if err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	defer guard.close()

	seed, err := openCloneSeed(grant)
	if err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	defer seed.close()
	if err := verifyCloneSeed(guard.ctx, seed, grant); err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(grant.Intent.RunID, grant.Intent.Generation)
	if err != nil {
		return fail(linuxgate.CloneFailureRequest, err)
	}
	bundleHeads, err := listBundleHeads(guard.ctx, args, seed.fd)
	if err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	if len(bundleHeads) != 1 || bundleHeads[0] != grant.Intent.BaseCommit+" "+baseRef {
		return fail(linuxgate.CloneFailureSeed, errors.New("immutable seed bundle does not contain exactly the admitted base ref and OID"))
	}
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}

	target, err := createFreshCloneTarget(grant)
	if err != nil {
		return fail(linuxgate.CloneFailureTarget, err)
	}
	defer target.close()
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureTarget, err)
	}
	if err := unix.Fchdir(target.fd); err != nil {
		return fail(linuxgate.CloneFailureTarget, fmt.Errorf("enter pinned fresh target directory: %w", err))
	}
	if err := initializeAndCheckout(guard.ctx, args, seed.fd, target, grant, baseRef); err != nil {
		var gitErr *gitCommandError
		if errors.As(err, &gitErr) {
			return fail(linuxgate.CloneFailureGit, err)
		}
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := verifyStandaloneCheckout(guard.ctx, args, seed.fd, target, grant); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := syncCloneTree(guard.ctx, target.fd); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := syncClonePath(target); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := verifySeedPathStillBound(seed, grant); err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	if err := verifyTargetPathStillBound(target, grant); err != nil {
		return fail(linuxgate.CloneFailureTarget, err)
	}
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}

	receipt := checkoutReceipt(grant, target)
	if err := receipt.Validate(); err != nil {
		return fail(linuxgate.CloneFailureCheckout, fmt.Errorf("construct B checkout receipt: %w", err))
	}
	ready, err := linuxgate.EncodeCloneCheckoutReady(receipt)
	if err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := guard.check(); err != nil {
		clear(ready)
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := sendStatusPacket(args.statusFD, ready); err != nil {
		clear(ready)
		return fail(linuxgate.CloneFailureCheckout, fmt.Errorf("send checkout-ready frame: %w", err))
	}
	clear(ready)

	if err := receiveCheckoutAck(guard, args.statusFD, receipt); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := verifyBIdentity(args.bUID, args.bGID); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}
	if err := verifySeedPathStillBound(seed, grant); err != nil {
		return fail(linuxgate.CloneFailureSeed, err)
	}
	if err := verifyTargetPathStillBound(target, grant); err != nil {
		return fail(linuxgate.CloneFailureTarget, err)
	}
	if err := verifyStandaloneCheckout(guard.ctx, args, seed.fd, target, grant); err != nil {
		return fail(linuxgate.CloneFailureCheckout, err)
	}
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}

	nativeImage, nativeIdentity, err := openNativeImage(command.Path)
	if err != nil {
		return fail(linuxgate.CloneFailureNativeImage, err)
	}
	defer nativeImage.Close()
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}
	if err := verifyTargetPathStillBound(target, grant); err != nil {
		return fail(linuxgate.CloneFailureTarget, err)
	}
	if err := unix.Fchdir(target.fd); err != nil {
		return fail(linuxgate.CloneFailureTarget, fmt.Errorf("restore exact B checkout as native working directory: %w", err))
	}
	if err := installNativeStdio(args); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}
	if err := closeBeforeNativeExec(args, seed, target); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}
	if err := setCloseOnExec(args.statusFD); err != nil {
		return fail(linuxgate.CloneFailureExec, fmt.Errorf("mark status endpoint close-on-exec: %w", err))
	}
	if err := guard.check(); err != nil {
		return fail(linuxgate.CloneFailureExec, err)
	}
	execVector, err := prepareNativeExecVector(command.Args, command.Env)
	if err != nil {
		return fail(linuxgate.CloneFailureExec, fmt.Errorf("prepare approved native exec vector: %w", err))
	}
	runtime.LockOSThread()
	attempt := linuxgate.EncodeNativeExecAttempt(uint64(nativeIdentity.Dev), nativeIdentity.Ino)
	if err := sendStatusPacket(args.statusFD, attempt); err != nil {
		clear(attempt)
		return fail(linuxgate.CloneFailureExec, fmt.Errorf("send native exec-attempt frame: %w", err))
	}
	clear(attempt)
	if err := execNativeImage(int(nativeImage.Fd()), &execVector); err != nil {
		return fail(linuxgate.CloneFailureExec, fmt.Errorf("same-PID guarded native execveat: %w", err))
	}
	return errors.New("execveat unexpectedly returned without an error")
}

func parseBootstrapArgs(argv []string) (bootstrapArgs, error) {
	var args bootstrapArgs
	if len(argv) != 10 {
		return args, errors.New("clone bootstrap requires its exact inherited-FD argument vector")
	}
	values := []*int{
		&args.requestFD, &args.gitFD, &args.ownerPIDFD, &args.statusFD,
		&args.ownerPipeFD, &args.stdinFD, &args.stdoutFD, &args.stderrFD,
	}
	for index, destination := range values {
		fd, err := parseInheritedFD(argv[index])
		if err != nil {
			return bootstrapArgs{}, fmt.Errorf("parse fixed inherited descriptor %d: %w", index, err)
		}
		*destination = fd
	}
	bUID, err := parseCanonicalID(argv[8])
	if err != nil || bUID == 0 {
		return bootstrapArgs{}, errors.New("fixed B UID argument is invalid")
	}
	bGID, err := parseCanonicalID(argv[9])
	if err != nil || bGID == 0 {
		return bootstrapArgs{}, errors.New("fixed B GID argument is invalid")
	}
	args.bUID, args.bGID = bUID, bGID
	return args, nil
}

func parseInheritedFD(value string) (int, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 2 || strconv.FormatInt(parsed, 10) != value {
		return -1, errors.New("descriptor is not a canonical inherited FD above stdio")
	}
	return int(parsed), nil
}

func parseCanonicalID(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || value == "" || strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("UID/GID is not a canonical uint32")
	}
	return uint32(parsed), nil
}

func verifyBIdentity(hostUID, hostGID uint32) error {
	if os.Getuid() != 1 || os.Geteuid() != 1 || os.Getgid() != 1 || os.Getegid() != 1 {
		return errors.New("clone bootstrap did not enter namespace B UID/GID 1")
	}
	groups, err := unix.Getgroups()
	if err != nil {
		return fmt.Errorf("read clone bootstrap supplementary groups: %w", err)
	}
	if len(groups) != 0 {
		return fmt.Errorf("clone bootstrap supplementary groups are not empty: %v", groups)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return fmt.Errorf("read clone bootstrap credential status: %w", err)
	}
	if len(status) == 0 || len(status) > 64<<10 {
		return errors.New("clone bootstrap credential status is empty or oversized")
	}
	if err := validateBStatus(status, hostUID, hostGID); err != nil {
		return err
	}
	uidMap, err := readProcSmall("/proc/self/uid_map")
	if err != nil {
		return fmt.Errorf("read clone bootstrap UID map: %w", err)
	}
	if canonicalProcMap(uidMap) != fmt.Sprintf("1 %d 1", hostUID) {
		return fmt.Errorf("clone bootstrap UID map is not the fixed B-only mapping: %q", uidMap)
	}
	gidMap, err := readProcSmall("/proc/self/gid_map")
	if err != nil {
		return fmt.Errorf("read clone bootstrap GID map: %w", err)
	}
	if canonicalProcMap(gidMap) != fmt.Sprintf("1 %d 1", hostGID) {
		return fmt.Errorf("clone bootstrap GID map is not the fixed B-only mapping: %q", gidMap)
	}
	setgroups, err := readProcSmall("/proc/self/setgroups")
	if err != nil {
		return fmt.Errorf("read clone bootstrap setgroups control: %w", err)
	}
	if strings.TrimSpace(setgroups) != "deny" {
		return fmt.Errorf("clone bootstrap user namespace does not deny setgroups: %q", setgroups)
	}
	return nil
}

func validateBStatus(data []byte, hostUID, hostGID uint32) error {
	seen := uint16(0)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		switch key {
		case "Uid":
			if !allIDs(fields, hostUID) {
				return errors.New("clone bootstrap proc status UID vector differs from fixed host B")
			}
			seen |= 1
		case "Gid":
			if !allIDs(fields, hostGID) {
				return errors.New("clone bootstrap proc status GID vector differs from fixed host B")
			}
			seen |= 2
		case "Groups":
			if len(fields) != 0 {
				return errors.New("clone bootstrap proc status exposes supplementary groups")
			}
			seen |= 4
		case "CapEff", "CapPrm", "CapInh", "CapBnd", "CapAmb":
			if len(fields) != 1 || strings.TrimLeft(fields[0], "0") != "" {
				return fmt.Errorf("clone bootstrap proc status %s is nonzero or malformed", key)
			}
			switch key {
			case "CapEff":
				seen |= 8
			case "CapPrm":
				seen |= 16
			case "CapInh":
				seen |= 32
			case "CapBnd":
				seen |= 64
			case "CapAmb":
				seen |= 128
			}
		case "NoNewPrivs":
			if len(fields) != 1 || fields[0] != "1" {
				return errors.New("clone bootstrap lacks no_new_privs")
			}
			seen |= 256
		}
	}
	if seen != 511 {
		return errors.New("clone bootstrap proc status lacks exact credentials, empty groups, zero caps, or no_new_privs")
	}
	return nil
}

func allIDs(fields []string, expected uint32) bool {
	if len(fields) != 4 {
		return false
	}
	for _, field := range fields {
		parsed, err := strconv.ParseUint(field, 10, 32)
		if err != nil || uint32(parsed) != expected {
			return false
		}
	}
	return true
}

func readProcSmall(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 4096 {
		return "", errors.New("proc identity file is empty or oversized")
	}
	return string(data), nil
}

func canonicalProcMap(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func verifyControlDescriptors(args bootstrapArgs) error {
	all := []int{args.requestFD, args.gitFD, args.ownerPIDFD, args.statusFD, args.ownerPipeFD,
		args.stdinFD, args.stdoutFD, args.stderrFD}
	for index, fd := range all {
		for prior := 0; prior < index; prior++ {
			if fd == all[prior] {
				return errors.New("clone bootstrap inherited duplicate control or stdio descriptors")
			}
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
			return fmt.Errorf("validate inherited clone bootstrap descriptor %d: %w", fd, err)
		}
	}
	kind, err := unix.GetsockoptInt(args.statusFD, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return fmt.Errorf("read clone bootstrap status socket type: %w", err)
	}
	if kind != unix.SOCK_SEQPACKET {
		return fmt.Errorf("clone bootstrap status descriptor is not seqpacket: type=%d", kind)
	}
	peer, err := unix.GetsockoptUcred(args.statusFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return fmt.Errorf("read A status peer credentials: %w", err)
	}
	if peer.Pid <= 1 {
		return errors.New("A status peer PID is invalid")
	}
	ownerPID, err := pidfdTargetPID(args.ownerPIDFD)
	if err != nil {
		return err
	}
	if ownerPID != int(peer.Pid) {
		return fmt.Errorf("owner pidfd does not identify the authenticated A status peer: target=%d peer=%d", ownerPID, peer.Pid)
	}
	var ownerPipe unix.Stat_t
	if err := unix.Fstat(args.ownerPipeFD, &ownerPipe); err != nil {
		return fmt.Errorf("stat owner guard descriptor: %w", err)
	}
	if ownerPipe.Mode&unix.S_IFMT != unix.S_IFIFO {
		return fmt.Errorf("owner guard descriptor is not a FIFO: mode=%#o", ownerPipe.Mode)
	}
	pipeFlags, err := unix.FcntlInt(uintptr(args.ownerPipeFD), unix.F_GETFL, 0)
	if err != nil {
		return fmt.Errorf("read owner guard descriptor flags: %w", err)
	}
	if pipeFlags&unix.O_ACCMODE != unix.O_RDONLY {
		return fmt.Errorf("owner guard descriptor is not read-only: flags=%#x", pipeFlags)
	}
	stdioFDs := []int{args.stdinFD, args.stdoutFD, args.stderrFD}
	access := []int{unix.O_RDONLY, unix.O_WRONLY, unix.O_WRONLY}
	var identities [3]unix.Stat_t
	for index, fd := range stdioFDs {
		if err := validateNativePipe(fd, access[index]); err != nil {
			return fmt.Errorf("validate native stdio pipe %d: %w", index, err)
		}
		if err := unix.Fstat(fd, &identities[index]); err != nil {
			return err
		}
		for prior := 0; prior < index; prior++ {
			if identities[index].Dev == identities[prior].Dev && identities[index].Ino == identities[prior].Ino {
				return errors.New("native stdin/stdout/stderr must be distinct pipe ends")
			}
		}
		if identities[index].Dev == ownerPipe.Dev && identities[index].Ino == ownerPipe.Ino {
			return errors.New("native stdio aliases the A owner-liveness pipe")
		}
	}
	return nil
}

func validateNativePipe(fd, access int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errors.New("descriptor is not a pipe")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return fmt.Errorf("read pipe access flags: %w", err)
	}
	if flags&unix.O_ACCMODE != access {
		return fmt.Errorf("pipe has wrong access mode %#x", flags)
	}
	return nil
}

func markUnexpectedDescriptorsCloseOnExec(args bootstrapArgs) error {
	allowed := map[int]struct{}{
		args.requestFD: {}, args.gitFD: {}, args.ownerPIDFD: {}, args.statusFD: {}, args.ownerPipeFD: {},
		args.stdinFD: {}, args.stdoutFD: {}, args.stderrFD: {},
	}
	directory, err := os.Open("/proc/self/fd")
	if err != nil {
		return fmt.Errorf("open proc descriptor table: %w", err)
	}
	entries, readErr := directory.ReadDir(4096)
	directoryFD := int(directory.Fd())
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil || len(entries) == 4096 {
		return errors.Join(errors.New("read bounded inherited descriptor table"), readErr, closeErr)
	}
	for _, entry := range entries {
		fd, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || fd <= 2 || fd == directoryFD {
			continue
		}
		if _, keep := allowed[fd]; keep {
			continue
		}
		flags, getErr := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if errors.Is(getErr, unix.EBADF) {
			continue
		}
		if getErr != nil {
			return fmt.Errorf("read inherited descriptor flags for fd %d: %w", fd, getErr)
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags|unix.FD_CLOEXEC); err != nil {
			return fmt.Errorf("mark unrecognized inherited fd %d close-on-exec: %w", fd, err)
		}
	}
	return nil
}

func markInheritedDescriptorsCloseOnExec(args bootstrapArgs) error {
	for _, fd := range []int{args.requestFD, args.gitFD, args.ownerPIDFD, args.statusFD,
		args.ownerPipeFD, args.stdinFD, args.stdoutFD, args.stderrFD} {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			return fmt.Errorf("read inherited descriptor %d flags: %w", fd, err)
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags|unix.FD_CLOEXEC); err != nil {
			return fmt.Errorf("mark inherited descriptor %d close-on-exec: %w", fd, err)
		}
	}
	return nil
}

func pidfdTargetPID(fd int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return 0, fmt.Errorf("read owner pidfd info: %w", err)
	}
	if len(data) == 0 || len(data) > 4096 {
		return 0, errors.New("owner pidfd info is empty or oversized")
	}
	var target int
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || key != "Pid" {
			continue
		}
		if found {
			return 0, errors.New("owner pidfd info repeats its PID")
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || parsed <= 1 {
			return 0, errors.New("owner pidfd target PID is invalid")
		}
		target, found = parsed, true
	}
	if !found {
		return 0, errors.New("owner pidfd info does not name a live PID")
	}
	return target, nil
}

func readSealedCloneRequest(fd int) ([]byte, error) {
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil {
		return nil, fmt.Errorf("read clone request memfd seals: %w", err)
	}
	const required = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
	if seals&required != required {
		return nil, errors.New("clone request memfd is not sealed against write, growth, shrink, and seal changes")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, fmt.Errorf("stat clone request memfd: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size <= 0 || stat.Size > 128<<10 {
		return nil, errors.New("clone request memfd is not a bounded regular file")
	}
	packet := make([]byte, int(stat.Size))
	for offset := 0; offset < len(packet); {
		n, err := unix.Pread(fd, packet[offset:], int64(offset))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			clear(packet)
			return nil, fmt.Errorf("read sealed clone request: %w", err)
		}
		if n == 0 {
			clear(packet)
			return nil, errors.New("sealed clone request ended before its measured size")
		}
		offset += n
	}
	var extra [1]byte
	n, err := unix.Pread(fd, extra[:], stat.Size)
	for errors.Is(err, unix.EINTR) {
		n, err = unix.Pread(fd, extra[:], stat.Size)
	}
	if err != nil {
		clear(packet)
		return nil, fmt.Errorf("check sealed clone request end: %w", err)
	}
	if n != 0 {
		clear(packet)
		return nil, errors.New("sealed clone request exceeds its measured size")
	}
	return packet, nil
}

func receiveCheckoutAck(guard *ownerLeaseGuard, fd int, receipt authority.LinuxCloneCheckoutReceipt) error {
	packet, err := receiveStatusPacket(guard, fd)
	if err != nil {
		return fmt.Errorf("receive durable checkout-ready ACK: %w", err)
	}
	ack, err := linuxgate.DecodeCloneCheckoutReadyAck(packet)
	clear(packet)
	if err != nil {
		return fmt.Errorf("decode durable checkout-ready ACK: %w", err)
	}
	if !ack.Equal(receipt) {
		return errors.New("durable checkout-ready ACK differs from the exact B receipt")
	}
	return guard.check()
}

func receiveStatusPacket(guard *ownerLeaseGuard, fd int) ([]byte, error) {
	for {
		if err := guard.check(); err != nil {
			return nil, err
		}
		remaining, err := guard.remaining()
		if err != nil {
			return nil, err
		}
		pollFor := 100 * time.Millisecond
		if remaining < pollFor {
			pollFor = remaining
		}
		millis := int((pollFor + time.Millisecond - 1) / time.Millisecond)
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(pollFDs, millis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("poll A checkout ACK socket: %w", err)
		}
		if count == 0 {
			continue
		}
		data := make([]byte, 64<<10)
		oob := make([]byte, unix.CmsgSpace(4*4))
		n, oobn, flags, _, recvErr := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
		if recvErr != nil {
			return nil, fmt.Errorf("receive A checkout ACK: %w", recvErr)
		}
		if oobn != 0 {
			closeAncillaryRights(oob[:oobn])
			return nil, errors.New("A checkout ACK carried unexpected ancillary data")
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > 64<<10 {
			return nil, errors.New("A checkout ACK is truncated or oversized")
		}
		if n == 0 {
			if flags&unix.MSG_EOR != 0 {
				return nil, errors.New("A checkout ACK is an empty seqpacket")
			}
			return nil, errors.New("A closed checkout ACK status channel")
		}
		return data[:n], nil
	}
}

func closeAncillaryRights(oob []byte) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return
	}
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			continue
		}
		fds, err := unix.ParseUnixRights(&message)
		if err == nil {
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
		}
	}
}

func sendStatusPacket(fd int, packet []byte) error {
	if len(packet) < 12 || len(packet) > 64<<10 {
		return errors.New("clone status frame is outside its fixed size bound")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL|unix.MSG_DONTWAIT)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("clone status seqpacket was short: %d/%d", n, len(packet))
	}
	return nil
}

func bootstrapErrno(err error) uint32 {
	if errors.Is(err, errLeaseExpired) {
		return uint32(unix.ETIMEDOUT)
	}
	if errors.Is(err, errOwnerStopped) {
		return uint32(unix.ECANCELED)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return uint32(unix.ETIMEDOUT)
	}
	if errors.Is(err, context.Canceled) {
		return uint32(unix.ECANCELED)
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 && uint64(errno) <= uint64(^uint32(0)>>1) {
		return uint32(errno)
	}
	return uint32(unix.EPERM)
}

func installNativeStdio(args bootstrapArgs) error {
	pairs := [][2]int{{args.stdinFD, 0}, {args.stdoutFD, 1}, {args.stderrFD, 2}}
	for _, pair := range pairs {
		if pair[0] == pair[1] {
			continue
		}
		if err := unix.Dup2(pair[0], pair[1]); err != nil {
			return fmt.Errorf("install native standard descriptor %d: %w", pair[1], err)
		}
	}
	return nil
}

func closeBeforeNativeExec(args bootstrapArgs, seed *cloneSeed, target *cloneTarget) error {
	for _, fd := range []int{args.gitFD,
		args.stdinFD, args.stdoutFD, args.stderrFD} {
		if fd <= 2 {
			continue
		}
		if err := unix.Close(fd); err != nil && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("close non-stdio bootstrap descriptor %d: %w", fd, err)
		}
	}
	if seed != nil {
		if err := seed.close(); err != nil {
			return fmt.Errorf("close pinned seed descriptors: %w", err)
		}
	}
	if target != nil {
		if err := target.close(); err != nil {
			return fmt.Errorf("close pinned checkout descriptors: %w", err)
		}
	}
	return nil
}

func setCloseOnExec(fd int) error {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		return err
	}
	_, err = unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags|unix.FD_CLOEXEC)
	return err
}
