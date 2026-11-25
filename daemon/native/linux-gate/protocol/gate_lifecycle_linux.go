//go:build linux && amd64

package protocol

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	launcherPath          = "/usr/libexec/symmetry/symmetry-linux-launcher"
	cloneBootstrapPath    = "/usr/libexec/symmetry/symmetry-linux-clone-bootstrap"
	bootstrapTimeout      = 15 * time.Second
	execAckTimeout        = 15 * time.Second
	cloneBootstrapTimeout = 2 * time.Minute
)

var (
	ErrOutcomeUnknown            = errors.New("Linux owner launch outcome is unknown; retain the pinned leaf and do not retry")
	ErrReleaseUsed               = errors.New("Linux owner gate release is single-use")
	ErrInaccessibleUserNamespace = errors.New("Linux owner host cannot inspect the exact user-namespace nsfs identity")
)

// Runs after the group-cleared READY frame and before Prepare sends the sole
// gate request; the root core cannot clone before it. pidfd is borrowed.
type LauncherStartHook func(context.Context, LauncherIdentity, *os.File) error

// The only pre-gate outcome that can support a no-target reservation abort:
// the typed proof needs an authenticated root result and exact reap.
var ErrGateNeverProduced = errors.New("Linux A gate was never produced")

type GateNeverProducedError struct {
	result       authority.LinuxCgroupGateProductionResult
	launcherExit *authority.LinuxCgroupExitProof
	errno        uint32
}

func (failure *GateNeverProducedError) Error() string {
	if failure == nil {
		return ErrGateNeverProduced.Error()
	}
	return fmt.Sprintf("%s before gate clone (launcher pid=%d errno=%d)", ErrGateNeverProduced, failure.result.Launcher.PID, failure.errno)
}

func (failure *GateNeverProducedError) Unwrap() error { return ErrGateNeverProduced }

// Callers bind this to their reservation; the platform must verify the MAC
// before issuing it, and State still checks launch-token/launcher identity.
func (failure *GateNeverProducedError) Result() authority.LinuxCgroupGateProductionResult {
	if failure == nil {
		return authority.LinuxCgroupGateProductionResult{}
	}
	return failure.result
}

func (failure *GateNeverProducedError) Errno() uint32 {
	if failure == nil {
		return 0
	}
	return failure.errno
}

func (failure *GateNeverProducedError) LauncherExitProof() (authority.LinuxCgroupExitProof, bool) {
	if failure == nil || failure.launcherExit == nil {
		return authority.LinuxCgroupExitProof{}, false
	}
	return *failure.launcherExit, true
}

// A nil exit proof is explicitly unresolved: callers must not infer launcher
// exit from the error alone.
type LauncherOutcomeError struct {
	launcher LauncherIdentity
	exit     *authority.LinuxCgroupExitProof
	cause    error
}

func (failure *LauncherOutcomeError) Error() string {
	if failure == nil || failure.cause == nil {
		return "Linux transient launcher outcome is unresolved"
	}
	return fmt.Sprintf("Linux transient launcher pid=%d outcome: %v", failure.launcher.PID, failure.cause)
}

func (failure *LauncherOutcomeError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *LauncherOutcomeError) Producer() LauncherIdentity {
	if failure == nil {
		return LauncherIdentity{}
	}
	return failure.launcher
}

func (failure *LauncherOutcomeError) ExitProof() (authority.LinuxCgroupExitProof, bool) {
	if failure == nil || failure.exit == nil {
		return authority.LinuxCgroupExitProof{}, false
	}
	return *failure.exit, true
}

func launcherExitProof(producer, helper LauncherIdentity, command *exec.Cmd) *authority.LinuxCgroupExitProof {
	if command == nil || command.Process == nil || command.ProcessState == nil || command.Process.Pid != producer.PID ||
		producer.PID <= 1 || producer.Identity == "" || helper.PID <= 1 || helper.Identity == "" {
		return nil
	}
	proof := &authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion,
		Process: authority.LinuxCgroupProcessIdentity{PID: producer.PID, Identity: producer.Identity},
		Witness: authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity},
		Waited:  true,
		Reaped:  true,
	}
	expectedProcess := authority.LinuxCgroupProcessIdentity{PID: producer.PID, Identity: producer.Identity}
	expectedWitness := authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}
	if !proof.ValidFor(expectedProcess, expectedWitness) {
		return nil
	}
	return proof
}

// PIDFD returns a borrowed descriptor, and Release is legal only after the
// durable handoff commit and the initial lease acknowledgement.
type PreparedGate struct {
	mu sync.Mutex

	config          OwnerConfig
	identity        ProcessIdentity
	launcher        LauncherIdentity
	launcherExit    *authority.LinuxCgroupExitProof
	pidfd           *os.File
	release         *os.File
	execAck         *os.File
	nativeImage     *pinnedNativeImage
	bootstrapImage  *pinnedNativeImage
	token           [32]byte
	state           gateState
	waiting         bool
	waited          bool
	releaseWaiting  bool
	released        bool
	abortSignalSent bool
}

func (gate *PreparedGate) LauncherIdentity() LauncherIdentity {
	if gate == nil {
		return LauncherIdentity{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.launcher
}

func (gate *PreparedGate) LauncherExitProof() (authority.LinuxCgroupExitProof, bool) {
	if gate == nil {
		return authority.LinuxCgroupExitProof{}, false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.launcherExit == nil {
		return authority.LinuxCgroupExitProof{}, false
	}
	return *gate.launcherExit, true
}

type gateState uint8

const (
	gatePrepared gateState = iota + 1
	gateReleaseAttempted
	gateReleased
	gateAborted
	gateClosed
	gateUnresolved
)

// Callers must durably record the RunKey-scoped launch intent first: the root
// core cannot clone before the durable hook succeeds and the request is sent.
func Prepare(ctx context.Context, leafFD int, gateAuth GateProductionAuth, onLauncherStart LauncherStartHook) (preparedResult *PreparedGate, prepareErr error) {
	var producerStarted bool
	var producer LauncherIdentity
	var helper LauncherIdentity
	var launcherCommand *exec.Cmd
	defer func() {
		if !producerStarted || prepareErr == nil {
			return
		}
		outcome := &LauncherOutcomeError{launcher: producer, exit: launcherExitProof(producer, helper, launcherCommand), cause: prepareErr}
		prepareErr = outcome
	}()
	if ctx == nil || leafFD < 0 {
		return nil, errors.New("Linux owner prepare requires context and a pinned cgroup leaf FD")
	}
	if onLauncherStart == nil {
		return nil, errors.New("Linux owner prepare requires durable transient-launcher identity recording")
	}
	if err := gateAuth.Validate(); err != nil {
		return nil, err
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		return nil, err
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		return nil, errors.New("Linux owner prepare caller is not the configured host UID/GID A")
	}
	helper, err = CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("capture exact A helper identity for launch outcome: %w", err)
	}
	if err := verifyCgroupLeafFD(leafFD, config.AUID); err != nil {
		return nil, err
	}
	if err := verifyInstalledLauncher(config); err != nil {
		return nil, err
	}
	if err := probePIDFDSupport(); err != nil {
		return nil, fmt.Errorf("Linux owner launcher requires pidfds before any privileged execution: %w", err)
	}

	token := [32]byte{}
	if _, err := rand.Read(token[:]); err != nil {
		return nil, fmt.Errorf("generate one-use Linux owner release token: %w", err)
	}
	defer clear(token[:])

	controlFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("create authenticated Linux owner launch channel: %w", err)
	}
	controlHelper := os.NewFile(uintptr(controlFDs[0]), "linux-owner-launch-helper")
	controlLauncher := os.NewFile(uintptr(controlFDs[1]), "linux-owner-launcher-channel")
	if controlHelper == nil || controlLauncher == nil {
		closeFiles(controlHelper, controlLauncher)
		return nil, errors.New("create Linux owner launch channel files")
	}

	releaseFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		closeFiles(controlHelper, controlLauncher)
		return nil, fmt.Errorf("create one-use Linux owner release channel: %w", err)
	}
	releaseHelper := os.NewFile(uintptr(releaseFDs[0]), "linux-owner-release-helper")
	releaseGate := os.NewFile(uintptr(releaseFDs[1]), "linux-owner-release-gate")
	if releaseHelper == nil || releaseGate == nil {
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate)
		return nil, errors.New("create Linux owner release channel files")
	}

	statusFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate)
		return nil, fmt.Errorf("create Linux owner exec-status channel: %w", err)
	}
	statusHelper := os.NewFile(uintptr(statusFDs[0]), "linux-owner-exec-status-helper")
	statusGate := os.NewFile(uintptr(statusFDs[1]), "linux-owner-exec-status-gate")
	if statusHelper == nil || statusGate == nil {
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate, statusHelper, statusGate)
		return nil, errors.New("create Linux owner exec-status channel files")
	}

	leafCopy, err := unix.FcntlInt(uintptr(leafFD), unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate, statusHelper, statusGate)
		return nil, fmt.Errorf("duplicate pinned Linux owner leaf FD: %w", err)
	}
	leafFile := os.NewFile(uintptr(leafCopy), "linux-owner-cgroup-leaf")
	if leafFile == nil {
		_ = unix.Close(leafCopy)
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate, statusHelper, statusGate)
		return nil, errors.New("wrap duplicate Linux owner leaf FD")
	}
	command := exec.Command(launcherPath)
	launcherCommand = command
	command.Args = []string{"symmetry-linux-launcher"}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.ExtraFiles = []*os.File{controlLauncher, leafFile, releaseGate, statusGate}
	launcherPIDFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &launcherPIDFD}
	if err := command.Start(); err != nil {
		if command.Process != nil {
			producerStarted = true
			producer, _ = CaptureLauncherIdentity(command.Process.Pid)
		}
		closeFiles(controlHelper, controlLauncher, releaseHelper, releaseGate, statusHelper, statusGate, leafFile)
		return nil, fmt.Errorf("start fixed Linux owner launcher: %w", err)
	}
	producerStarted = true
	closeErr := closeFiles(controlLauncher, releaseGate, statusGate, leafFile)
	if launcherPIDFD < 0 {
		// The request was never sent: closing the channel makes the root wrapper exit
		// without creating a gate, so do not signal by PID here.
		closeErr = errors.Join(closeErr, closeFiles(controlHelper, releaseHelper, statusHelper))
		return nil, errors.Join(fmt.Errorf("%w: Go did not return a launcher pidfd", ErrOutcomeUnknown), closeErr)
	}
	launcherFile := os.NewFile(uintptr(launcherPIDFD), "linux-owner-launcher-pidfd")
	if launcherFile == nil {
		_ = stopLauncher(command, launcherPIDFD)
		_ = unix.Close(launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, fmt.Errorf("%w: wrap launcher pidfd", ErrOutcomeUnknown)
	}
	defer launcherFile.Close()
	if closeErr != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeErr = errors.Join(closeErr, closeFiles(controlHelper, releaseHelper, statusHelper))
		return nil, errors.Join(fmt.Errorf("%w: close launcher's inherited descriptor copies", ErrOutcomeUnknown), closeErr, stopErr)
	}

	controlPeer, err := unix.GetsockoptUcred(int(controlHelper.Fd()), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || controlPeer.Uid != config.AUID || controlPeer.Gid != config.AGID || int(controlPeer.Pid) != os.Getpid() {
		stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, fmt.Errorf("authenticate Linux owner launch peer: %w", errOr(err, errors.New("launch peer credentials differ from A")))
	}
	launcherStartTime, err := readProcessStartTime(command.Process.Pid)
	if err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: capture transient root launcher start time: %v", ErrOutcomeUnknown, err), stopErr)
	}
	// Unprivileged A cannot read the root producer's PID namespace on every
	// supported procfs mount; the identity is completed from READY below.
	launcherIdentity := LauncherIdentity{PID: command.Process.Pid, StartTime: launcherStartTime, BootID: helper.BootID}
	producer = launcherIdentity
	ready, err := receiveLauncherReady(ctx, int(controlHelper.Fd()), time.Now().Add(bootstrapTimeout))
	if err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: receive group-cleared root launcher readiness: %w", ErrOutcomeUnknown, err), stopErr)
	}
	// A cannot reliably stat /proc/<root-pid>/ns/pid after the setuid exec: procfs
	// may return the restricted symlink inode, so compare the READY report instead.
	var pidNamespace syscall.Stat_t
	if err := syscall.Stat("/proc/self/ns/pid", &pidNamespace); err != nil || pidNamespace.Ino == 0 {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: read A helper PID namespace for root READY: %v", ErrOutcomeUnknown, err), stopErr)
	}
	expectedIdentity := fmt.Sprintf("linux:v2:%s:%d:%d:%d", helper.BootID, pidNamespace.Ino, launcherIdentity.PID, launcherIdentity.StartTime)
	if ready.PID != launcherIdentity.PID || ready.StartTime != launcherIdentity.StartTime || ready.Identity != expectedIdentity {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: root launcher readiness differs from the exact command.Start PID/start and A PID namespace", ErrOutcomeUnknown), stopErr)
	}
	launcherIdentity.Identity = ready.Identity
	producer = launcherIdentity
	if err := unix.PidfdSendSignal(launcherPIDFD, 0, nil, 0); err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: probe exact root launcher after READY: %v", ErrOutcomeUnknown, err), stopErr)
	}
	pidfdPID, err := readPIDFDTarget(launcherPIDFD)
	startTime, startErr := readProcessStartTime(command.Process.Pid)
	if err != nil || pidfdPID != launcherIdentity.PID || startErr != nil || startTime != launcherIdentity.StartTime {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: READY producer no longer matches its command.Start pidfd: pidfd=%d err=%v start=%d startErr=%v", ErrOutcomeUnknown, pidfdPID, err, startTime, startErr), stopErr)
	}
	launcherStatus, err := readProcessStatus(command.Process.Pid)
	if err != nil || launcherStatus.parent != os.Getpid() || launcherStatus.uids != [4]uint32{config.AUID, 0, 0, 0} ||
		launcherStatus.gids != [4]uint32{config.AGID, config.AGID, config.AGID, config.AGID} || len(launcherStatus.groups) != 0 {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: transient root launcher identity differs from direct A producer: status=%+v err=%v", ErrOutcomeUnknown, launcherStatus, err), stopErr)
	}
	if err := ctx.Err(); err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("record transient root launcher before gate production: %w", err), stopErr)
	}
	if err := onLauncherStart(ctx, launcherIdentity, launcherFile); err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("record transient root launcher before gate production: %w", err), stopErr)
	}
	launchRequest, err := encodeLaunchRequest(token, gateAuth)
	if err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("encode authenticated Linux owner launch request: %w", err), stopErr)
	}
	if err := sendPacket(int(controlHelper.Fd()), launchRequest); err != nil {
		clear(launchRequest)
		stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, fmt.Errorf("send Linux owner launch request: %w", err)
	}
	clear(launchRequest)

	deadline := time.Now().Add(bootstrapTimeout)
	packet, pidfd, receiveErr := receiveLauncherResult(ctx, int(controlHelper.Fd()), deadline)
	closeErr = controlHelper.Close()
	if receiveErr != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: receive fixed launcher result: %v", ErrOutcomeUnknown, receiveErr), closeErr, stopErr)
	}
	result, err := DecodeLaunchResult(packet)
	if err != nil {
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: invalid fixed launcher result: %v", ErrOutcomeUnknown, err), closeErr, stopErr)
	}
	if subtle.ConstantTimeCompare(result.Token[:], token[:]) != 1 {
		clear(result.Token[:])
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: root launcher result token mismatch", ErrOutcomeUnknown), closeErr, stopErr)
	}
	defer clear(result.Token[:])
	defer clear(result.Authenticator[:])
	if (result.Status == LaunchStatusReady && pidfd < 0) ||
		((result.Status == LaunchStatusRejected || result.Status == LaunchStatusNeverProduced) && pidfd >= 0) {
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: root launcher status disagrees with pidfd transfer", ErrOutcomeUnknown), closeErr, stopErr)
	}
	if result.Status == LaunchStatusUnresolved && pidfd < 0 {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: root launcher could not identify its unresolved gate", ErrOutcomeUnknown), closeErr, stopErr)
	}
	if err := waitProcessFD(ctx, launcherPIDFD, deadline); err != nil {
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: wait for exact root launcher exit: %v", ErrOutcomeUnknown, err), closeErr, stopErr)
	}
	launcherWaitErr := command.Wait()
	launcherExit := launcherExitProof(launcherIdentity, helper, command)
	if launcherWaitErr == nil && launcherExit == nil {
		if pidfd >= 0 {
			gateFile := os.NewFile(uintptr(pidfd), "linux-owner-unresolved-gate-pidfd")
			if gateFile != nil {
				identity := result.Identity
				identity.BootID = launcherIdentity.BootID
				unresolved := &PreparedGate{
					config: config, identity: identity, launcher: launcherIdentity, pidfd: gateFile,
					release: releaseHelper, execAck: statusHelper, token: token, state: gateUnresolved,
				}
				releaseCloseErr := unresolved.closeReleaseLocked()
				clear(unresolved.token[:])
				return unresolved, errors.Join(fmt.Errorf("%w: exact launcher wait/reap proof is unavailable after a successful wait", ErrOutcomeUnknown), closeErr, releaseCloseErr)
			}
			_ = unix.Close(pidfd)
		}
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: exact launcher wait/reap proof is unavailable after a successful wait", ErrOutcomeUnknown), closeErr)
	}
	if result.Status == LaunchStatusNeverProduced {
		closeFiles(releaseHelper, statusHelper)
		if command.ProcessState == nil {
			return nil, errors.Join(fmt.Errorf("%w: root launcher exit was not waited", ErrOutcomeUnknown), closeErr, launcherWaitErr)
		}
		exitProof := launcherExit
		if exitProof == nil {
			return nil, errors.Join(fmt.Errorf("%w: exact root launcher wait/reap proof is unavailable", ErrOutcomeUnknown), closeErr, launcherWaitErr)
		}
		gateResult := authority.LinuxCgroupGateProductionResult{
			Version:       authority.LinuxCgroupIdentityVersion,
			LaunchToken:   gateAuth.LaunchToken,
			Launcher:      authority.LinuxCgroupProcessIdentity{PID: launcherIdentity.PID, Identity: launcherIdentity.Identity},
			Outcome:       authority.LinuxCgroupGateOutcomeNeverCreated,
			Authenticator: hex.EncodeToString(result.Authenticator[:]),
		}
		if err := VerifyGateNeverProducedAuthenticator(gateAuth.Secret, gateResult); err != nil {
			return nil, errors.Join(fmt.Errorf("%w: authenticate root preclone outcome: %v", ErrOutcomeUnknown, err), closeErr, launcherWaitErr)
		}
		return nil, errors.Join(&GateNeverProducedError{result: gateResult, launcherExit: exitProof, errno: result.Errno}, closeErr)
	}
	if result.Status != LaunchStatusReady {
		if pidfd >= 0 {
			if result.Status == LaunchStatusUnresolved {
				gateFile := os.NewFile(uintptr(pidfd), "linux-owner-unresolved-gate-pidfd")
				closeFiles(releaseHelper, statusHelper)
				if gateFile == nil {
					_ = unix.Close(pidfd)
					return nil, errors.Join(fmt.Errorf("%w: launcher could not prove the A gate stopped", ErrOutcomeUnknown), launcherWaitErr, closeErr)
				}
				identity := result.Identity
				identity.BootID = launcherIdentity.BootID
				return &PreparedGate{
						config: config, identity: identity, launcher: launcherIdentity, launcherExit: launcherExit,
						pidfd: gateFile, state: gateUnresolved,
					},
					errors.Join(fmt.Errorf("%w: launcher reported an unresolved A gate (errno=%d)", ErrOutcomeUnknown, result.Errno), launcherWaitErr, closeErr)
			}
			closeErr = errors.Join(closeErr, unix.Close(pidfd))
		}
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("Linux owner launcher rejected setup (status=%d errno=%d)", result.Status, result.Errno), launcherWaitErr, closeErr)
	}
	if launcherWaitErr != nil {
		if pidfd >= 0 {
			gateFile := os.NewFile(uintptr(pidfd), "linux-owner-unresolved-gate-pidfd")
			closeFiles(releaseHelper, statusHelper)
			if gateFile != nil {
				identity := result.Identity
				identity.BootID = launcherIdentity.BootID
				return &PreparedGate{
						config: config, identity: identity, launcher: launcherIdentity, launcherExit: launcherExit,
						pidfd: gateFile, state: gateUnresolved,
					},
					errors.Join(fmt.Errorf("%w: root launcher wait result is uncertain", ErrOutcomeUnknown), launcherWaitErr, closeErr)
			}
			_ = unix.Close(pidfd)
		}
		closeFiles(releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: root launcher wait result is uncertain", ErrOutcomeUnknown), launcherWaitErr, closeErr)
	}
	if closeErr != nil {
		if pidfd < 0 {
			closeFiles(releaseHelper, statusHelper)
			return nil, errors.Join(fmt.Errorf("%w: close authenticated launch channel", ErrOutcomeUnknown), closeErr)
		}
		gateFile := os.NewFile(uintptr(pidfd), "linux-owner-unresolved-gate-pidfd")
		if gateFile == nil {
			_ = unix.Close(pidfd)
			closeFiles(releaseHelper, statusHelper)
			return nil, errors.Join(fmt.Errorf("%w: wrap gate pidfd after launch channel close error", ErrOutcomeUnknown), closeErr)
		}
		identity := result.Identity
		identity.BootID = launcherIdentity.BootID
		unresolved := &PreparedGate{
			config: config, identity: identity, launcher: launcherIdentity, launcherExit: launcherExit,
			pidfd: gateFile, release: releaseHelper, execAck: statusHelper, token: token, state: gateUnresolved,
		}
		releaseCloseErr := unresolved.closeReleaseLocked()
		clear(unresolved.token[:])
		return unresolved, errors.Join(fmt.Errorf("%w: launch result channel close was uncertain; RELEASE is forbidden", ErrOutcomeUnknown), closeErr, releaseCloseErr)
	}
	if pidfd < 0 {
		closeFiles(releaseHelper, statusHelper)
		return nil, fmt.Errorf("%w: successful launch result omitted the gate pidfd", ErrOutcomeUnknown)
	}
	gatePIDFD := os.NewFile(uintptr(pidfd), "linux-owner-gate-pidfd")
	if gatePIDFD == nil {
		_ = unix.Close(pidfd)
		closeFiles(releaseHelper, statusHelper)
		return nil, fmt.Errorf("%w: wrap gate pidfd", ErrOutcomeUnknown)
	}

	identity := result.Identity
	identity.BootID = launcherIdentity.BootID
	if identity.HostUID != config.AUID || identity.HostGID != config.AGID || identity.OwnerPID != os.Getpid() {
		_ = gatePIDFD.Close()
		closeFiles(releaseHelper, statusHelper)
		return nil, fmt.Errorf("%w: root launcher gate host identity differs from A", ErrOutcomeUnknown)
	}
	prepared := &PreparedGate{
		config:       config,
		identity:     identity,
		launcher:     launcherIdentity,
		launcherExit: launcherExit,
		pidfd:        gatePIDFD,
		release:      releaseHelper,
		execAck:      statusHelper,
		token:        token,
		state:        gatePrepared,
	}
	if err := prepared.verifyIdentity(leafFD, true); err != nil {
		prepared.mu.Lock()
		prepared.state = gateUnresolved
		closeErr := prepared.closeReleaseLocked()
		prepared.mu.Unlock()
		return prepared, errors.Join(fmt.Errorf("%w: verify parked A gate: %w", ErrOutcomeUnknown, err), closeErr)
	}
	return prepared, nil
}

func (gate *PreparedGate) Identity() ProcessIdentity {
	if gate == nil {
		return ProcessIdentity{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.identity
}

// Borrowed pidfd: it stays valid across the leader's exec and must be retained
// until the caller completes its stop and reap proof.
func (gate *PreparedGate) PIDFD() *os.File {
	if gate == nil {
		return nil
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.pidfd
}

// Call before durable binding, before Release and before stop proof; a point
// observation is not itself a stop receipt.
func (gate *PreparedGate) VerifyLeafMembership(leafFD int) error {
	if gate == nil || leafFD < 0 {
		return errors.New("Linux owner leaf verification requires a gate and pinned leaf FD")
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.verifyIdentity(leafFD, !gate.released)
}

// Bounded release sub-step vocabulary for the helper's unresolved-response
// detail; keep it closed, that field is redacted.
const (
	ReleaseSubStepGateAuthorize  = "gate_authorize"
	ReleaseSubStepCloneBootstrap = "clone_bootstrap"
	ReleaseSubStepCloneCheckout  = "clone_checkout"
	ReleaseSubStepNativeExec     = "native_exec"
	ReleaseSubStepRegister       = "register"
)

// Bounded release arm vocabulary: one code per failure site, so the helper's
// unresolved-response detail can name the failing arm instead of the error text.
const (
	ReleaseArmGateMissingIdentity = "missing_gate_identity"
	ReleaseArmGateAuthorize       = "anchor_authorize"
	ReleaseArmGateIdentity        = "gate_identity"

	ReleaseArmBootstrapInstalled = "installed_bootstrap"
	ReleaseArmBootstrapAttempt   = "bootstrap_attempt"
	ReleaseArmBootstrapImage     = "bootstrap_image"
	ReleaseArmBootstrapProcess   = "bootstrap_process"
	ReleaseArmLeafMembership     = "leaf_membership"

	ReleaseArmCheckoutReceipt         = "checkout_receipt"
	ReleaseArmCheckoutReceiptMismatch = "checkout_receipt_mismatch"
	ReleaseArmCheckoutCallback        = "checkout_callback"
	ReleaseArmCheckoutAckEncode       = "checkout_ack_encode"
	ReleaseArmCheckoutAckSend         = "checkout_ack_send"

	ReleaseArmNativeAwaitResult = "await_result"
	ReleaseArmNativeErrno       = "exec_errno"
	ReleaseArmNativeNoAttempt   = "no_exec_attempt"
	ReleaseArmNativeProcess     = "native_process"
	ReleaseArmNativeReadback    = "exec_readback"
	ReleaseArmNativeCloneProof  = "clone_exec_proof"

	ReleaseArmRegisterProcess = "native_register"
	ReleaseArmRegisterWaiter  = "gate_waiter"
)

// Bounded wait-error classes for the arms that read the exec-status channel.
const (
	ReleaseWaitClassDeadline   = "deadline"
	ReleaseWaitClassPeerClosed = "peer_closed"
	ReleaseWaitClassOther      = "other"
)

// Bounded guest stages of a decoded clone-bootstrap failure frame (the numbers
// come from CloneBootstrapFailureStage in wire.go, sent by the C gate).
const (
	ReleaseGuestStageRequest     = "request"
	ReleaseGuestStageSeed        = "seed"
	ReleaseGuestStageTarget      = "target"
	ReleaseGuestStageGit         = "git"
	ReleaseGuestStageCheckout    = "checkout"
	ReleaseGuestStageNativeImage = "native_image"
	ReleaseGuestStageExec        = "exec"
)

// ReleaseArmShape is the bounded detail shape an arm can carry.
type ReleaseArmShape struct {
	SubStep        string
	WaitClass      bool
	GuestBootstrap bool
}

// ReleaseSubStepError labels the release sub-step and arm that failed so the
// helper can report a bounded, redacted detail without re-parsing error text.
type ReleaseSubStepError struct {
	SubStep    string
	Arm        string
	WaitClass  string
	GuestStage string
	Errno      int
	Err        error
}

// LookupReleaseArmShape resolves a closed arm code to its sub-step and the
// detail fields the arm can legitimately carry.
func LookupReleaseArmShape(arm string) (ReleaseArmShape, bool) {
	switch arm {
	case ReleaseArmGateMissingIdentity, ReleaseArmGateAuthorize, ReleaseArmGateIdentity:
		return ReleaseArmShape{SubStep: ReleaseSubStepGateAuthorize}, true
	case ReleaseArmBootstrapInstalled, ReleaseArmBootstrapImage, ReleaseArmBootstrapProcess, ReleaseArmLeafMembership:
		return ReleaseArmShape{SubStep: ReleaseSubStepCloneBootstrap}, true
	case ReleaseArmBootstrapAttempt:
		return ReleaseArmShape{SubStep: ReleaseSubStepCloneBootstrap, WaitClass: true, GuestBootstrap: true}, true
	case ReleaseArmCheckoutReceipt:
		return ReleaseArmShape{SubStep: ReleaseSubStepCloneCheckout, WaitClass: true, GuestBootstrap: true}, true
	case ReleaseArmCheckoutReceiptMismatch, ReleaseArmCheckoutCallback,
		ReleaseArmCheckoutAckEncode, ReleaseArmCheckoutAckSend:
		return ReleaseArmShape{SubStep: ReleaseSubStepCloneCheckout}, true
	case ReleaseArmNativeAwaitResult:
		return ReleaseArmShape{SubStep: ReleaseSubStepNativeExec, WaitClass: true}, true
	case ReleaseArmNativeCloneProof:
		return ReleaseArmShape{SubStep: ReleaseSubStepNativeExec, WaitClass: true, GuestBootstrap: true}, true
	case ReleaseArmNativeErrno, ReleaseArmNativeNoAttempt, ReleaseArmNativeProcess, ReleaseArmNativeReadback:
		return ReleaseArmShape{SubStep: ReleaseSubStepNativeExec}, true
	case ReleaseArmRegisterProcess, ReleaseArmRegisterWaiter:
		return ReleaseArmShape{SubStep: ReleaseSubStepRegister}, true
	default:
		return ReleaseArmShape{}, false
	}
}

// ReleaseWaitErrorClass reduces a failed exec-status read to its closed class;
// the raw error stays out of the bounded detail.
func ReleaseWaitErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return ReleaseWaitClassDeadline
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, unix.ENOTCONN), errors.Is(err, unix.EPIPE), errors.Is(err, unix.ECONNRESET):
		return ReleaseWaitClassPeerClosed
	default:
		return ReleaseWaitClassOther
	}
}

// ReleaseGuestStageName maps a decoded guest clone-bootstrap failure to its
// closed stage name.
func ReleaseGuestStageName(stage CloneBootstrapFailureStage) (string, bool) {
	switch stage {
	case CloneFailureRequest:
		return ReleaseGuestStageRequest, true
	case CloneFailureSeed:
		return ReleaseGuestStageSeed, true
	case CloneFailureTarget:
		return ReleaseGuestStageTarget, true
	case CloneFailureGit:
		return ReleaseGuestStageGit, true
	case CloneFailureCheckout:
		return ReleaseGuestStageCheckout, true
	case CloneFailureNativeImage:
		return ReleaseGuestStageNativeImage, true
	case CloneFailureExec:
		return ReleaseGuestStageExec, true
	default:
		return "", false
	}
}

// nativeExecProcessSideError labels verifyNativeImage's process-side failures
// (pidfd, exit, identity, credentials, namespace, boot); Error stays verbatim.
type nativeExecProcessSideError struct{ cause error }

func (failure *nativeExecProcessSideError) Error() string { return failure.cause.Error() }

func (failure *nativeExecProcessSideError) Unwrap() error { return failure.cause }

// NativeExecProcessSide wraps a process-side verify failure; Error is verbatim.
func NativeExecProcessSide(err error) error { return &nativeExecProcessSideError{cause: err} }

// NativeExecVerifyArm names the bounded arm of a verifyNativeImage failure.
func NativeExecVerifyArm(err error) string {
	var processSide *nativeExecProcessSideError
	if errors.As(err, &processSide) {
		return ReleaseArmNativeProcess
	}
	return ReleaseArmNativeReadback
}

// NewReleaseSubStepError bounds one failed release arm and never re-labels an already-labelled
// error; an arm outside the closed table is not shape-classified, and the wire validator refuses it.
func NewReleaseSubStepError(subStep, arm string, errno int, err error) *ReleaseSubStepError {
	var existing *ReleaseSubStepError
	if errors.As(err, &existing) {
		return existing
	}
	failure := &ReleaseSubStepError{SubStep: subStep, Arm: arm, Errno: errno, Err: err}
	shape, ok := LookupReleaseArmShape(arm)
	if !ok {
		return failure
	}
	if shape.WaitClass {
		failure.WaitClass = ReleaseWaitErrorClass(err)
	}
	if shape.GuestBootstrap {
		var guest *CloneBootstrapFailureError
		if errors.As(err, &guest) {
			if stage, found := ReleaseGuestStageName(guest.Stage); found {
				failure.GuestStage = stage
				if failure.Errno == 0 {
					failure.Errno = int(guest.Errno)
				}
			}
		}
	}
	return failure
}

func (failure *ReleaseSubStepError) Error() string {
	if failure == nil || failure.Err == nil {
		return "Linux owner RELEASE sub-step failed"
	}
	return failure.Err.Error()
}

func (failure *ReleaseSubStepError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// One guarded RELEASE: unknown send/ACK results are terminal and never resent,
// and the checkout-ready callback runs without gate.mu held.
func (gate *PreparedGate) Release(ctx context.Context, leafFD int, command NativeCommand, guard ReleaseGuard,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error) error {
	if gate == nil || ctx == nil {
		return errors.New("Linux owner release requires a prepared gate and context")
	}
	gate.mu.Lock()
	if gate.state != gatePrepared {
		gate.mu.Unlock()
		return ErrReleaseUsed
	}
	if err := command.Validate(); err != nil {
		gate.mu.Unlock()
		return err
	}
	if err := validateReleaseOwnerPipe(guard.OwnerPipe, command); err != nil {
		gate.mu.Unlock()
		return err
	}
	if command.Clone != nil && command.Reuse != nil {
		gate.mu.Unlock()
		return errors.New("Linux native command cannot carry both clone and reuse authority")
	}
	if (command.Clone != nil) != (onCheckoutReady != nil) {
		gate.mu.Unlock()
		return errors.New("Linux clone RELEASE requires exactly one checkout-ready callback")
	}
	var bootstrapImage *pinnedNativeImage
	if command.Clone != nil {
		image, bootstrapErr := verifyInstalledCloneBootstrap(gate.config)
		if bootstrapErr != nil {
			gate.mu.Unlock()
			return NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmBootstrapInstalled, 0,
				fmt.Errorf("verify fixed root-owned B clone bootstrap: %w", bootstrapErr))
		}
		bootstrapImage = image
	}
	if err := ctx.Err(); err != nil {
		_ = bootstrapImage.close()
		gate.mu.Unlock()
		return err
	}
	nativeImage, err := pinNativeImage(command.Path, gate.config)
	if err != nil {
		_ = bootstrapImage.close()
		gate.mu.Unlock()
		return fmt.Errorf("pin A-approved native executable before RELEASE: %w", err)
	}
	imagePinsTransferred := false
	defer func() {
		if !imagePinsTransferred {
			_ = nativeImage.close()
			_ = bootstrapImage.close()
		}
	}()
	if err := gate.verifyIdentity(leafFD, true); err != nil {
		abortErr := gate.abortLocked()
		gate.mu.Unlock()
		return errors.Join(NewReleaseSubStepError(ReleaseSubStepGateAuthorize, ReleaseArmGateIdentity, 0,
			fmt.Errorf("%w: verify A gate before RELEASE: %v", ErrOutcomeUnknown, err)), abortErr)
	}
	deadlineNS, err := monotonicDeadlineNS(guard.LeaseDeadline)
	if err != nil {
		gate.mu.Unlock()
		return err
	}
	ownerPipeFD, err := unix.FcntlInt(guard.OwnerPipe.Fd(), unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		gate.mu.Unlock()
		return fmt.Errorf("duplicate helper owner pipe for RELEASE: %w", err)
	}
	ownerPipe := os.NewFile(uintptr(ownerPipeFD), "linux-owner-release-guard")
	if ownerPipe == nil {
		_ = unix.Close(ownerPipeFD)
		gate.mu.Unlock()
		return errors.New("wrap duplicated helper owner pipe for RELEASE")
	}
	packet, err := encodeGuardedRelease(gate.token, command, deadlineNS)
	if err != nil {
		_ = ownerPipe.Close()
		gate.mu.Unlock()
		return err
	}
	if gate.release == nil || gate.execAck == nil {
		clear(packet)
		_ = ownerPipe.Close()
		gate.state = gateUnresolved
		abortErr := gate.abortLocked()
		gate.mu.Unlock()
		return errors.Join(fmt.Errorf("%w: release or exec-status channel is unavailable", ErrOutcomeUnknown), abortErr)
	}
	gate.nativeImage = nativeImage
	gate.bootstrapImage = bootstrapImage
	imagePinsTransferred = true
	gate.state = gateReleaseAttempted
	fd := int(gate.release.Fd())
	gate.releaseWaiting = true
	rights := []int{int(command.Stdin.Fd()), int(command.Stdout.Fd()), int(command.Stderr.Fd()), int(ownerPipe.Fd())}
	writeErr := sendPacketWithRights(fd, packet, rights)
	for index := range packet {
		packet[index] = 0
	}
	ownerCloseErr := ownerPipe.Close()
	clear(gate.token[:])
	closeErr := gate.closeReleaseLocked()
	if writeErr != nil || closeErr != nil || ownerCloseErr != nil {
		gate.releaseWaiting = false
		if gate.state == gateReleaseAttempted {
			gate.state = gateUnresolved
		}
		abortErr := gate.abortLocked()
		gate.mu.Unlock()
		return errors.Join(fmt.Errorf("%w: one-use RELEASE v2 send/close result is uncertain: %v",
			ErrOutcomeUnknown, errors.Join(writeErr, closeErr, ownerCloseErr)), abortErr)
	}
	gate.mu.Unlock()
	defer func() {
		gate.mu.Lock()
		gate.releaseWaiting = false
		gate.mu.Unlock()
	}()

	var status execStatus
	var waitErr error
	if command.Clone != nil {
		status, waitErr = gate.awaitCloneRelease(ctx, leafFD, command, guard, onCheckoutReady)
	} else {
		ackDeadline := time.Now().Add(execAckTimeout)
		if guard.LeaseDeadline.Before(ackDeadline) {
			ackDeadline = guard.LeaseDeadline
		}
		if err := waitForStatus(ctx, int(gate.execAck.Fd()), ackDeadline); err != nil {
			waitErr = fmt.Errorf("wait for close-on-exec result: %w", err)
		} else {
			status, waitErr = receiveExecStatus(ctx, int(gate.execAck.Fd()), ackDeadline)
		}
	}
	if waitErr != nil {
		abortErr := gate.Abort()
		return errors.Join(NewReleaseSubStepError(ReleaseSubStepNativeExec, ReleaseArmNativeAwaitResult, 0,
			fmt.Errorf("%w: await guarded native exec result: %w", ErrOutcomeUnknown, waitErr)), abortErr)
	}
	gate.mu.Lock()
	gate.releaseWaiting = false
	if gate.state != gateReleaseAttempted {
		gate.mu.Unlock()
		return fmt.Errorf("%w: RELEASE was aborted before its exec acknowledgement settled", ErrOutcomeUnknown)
	}
	if status.errno != 0 {
		gate.state = gateUnresolved
		gate.mu.Unlock()
		abortErr := gate.Abort()
		return errors.Join(NewReleaseSubStepError(ReleaseSubStepNativeExec, ReleaseArmNativeErrno, int(status.errno),
			fmt.Errorf("native exec failed after RELEASE: errno=%d", status.errno)), abortErr)
	}
	if !status.execAttempted {
		gate.state = gateUnresolved
		gate.mu.Unlock()
		abortErr := gate.Abort()
		return errors.Join(NewReleaseSubStepError(ReleaseSubStepNativeExec, ReleaseArmNativeNoAttempt, 0,
			fmt.Errorf("%w: gate closed exec-status FD without an exec attempt", ErrOutcomeUnknown)), abortErr)
	}
	if err := gate.verifyNativeImage(status.device, status.inode, gate.nativeImage); err != nil {
		gate.state = gateUnresolved
		gate.mu.Unlock()
		abortErr := gate.Abort()
		return errors.Join(NewReleaseSubStepError(ReleaseSubStepNativeExec, NativeExecVerifyArm(err), 0,
			fmt.Errorf("%w: verify same-PID native exec: %v", ErrOutcomeUnknown, err)), abortErr)
	}
	gate.state = gateReleased
	gate.released = true
	gate.mu.Unlock()
	return nil
}

// Idempotent, and never waits for the gate or signals a PID.
func (gate *PreparedGate) Abort() error {
	if gate == nil {
		return nil
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.abortLocked()
}

func (gate *PreparedGate) abortLocked() error {
	if gate.state == gateClosed {
		return nil
	}
	gate.state = gateAborted
	clear(gate.token[:])
	var abortErr error
	if gate.execAck != nil && !gate.abortSignalSent {
		if err := unix.Shutdown(int(gate.execAck.Fd()), unix.SHUT_WR); err != nil && !errors.Is(err, unix.ENOTCONN) {
			abortErr = fmt.Errorf("half-close gate exec-status control channel: %w", err)
		} else {
			gate.abortSignalSent = true
		}
	}
	return errors.Join(abortErr, gate.closeReleaseLocked())
}

func (gate *PreparedGate) awaitCloneRelease(ctx context.Context, leafFD int, command NativeCommand,
	guard ReleaseGuard, onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error) (execStatus, error) {
	if command.Clone == nil || onCheckoutReady == nil {
		return execStatus{}, errors.New("clone RELEASE lacks its grant or durable checkout callback")
	}
	if gate.bootstrapImage == nil {
		return execStatus{}, errors.New("clone RELEASE lacks A's pinned fixed bootstrap image")
	}
	deadline := time.Now().Add(cloneBootstrapTimeout)
	if guard.LeaseDeadline.Before(deadline) {
		deadline = guard.LeaseDeadline
	}
	device, inode, err := readCloneBootstrapExecAttempt(ctx, int(gate.execAck.Fd()), deadline)
	if err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmBootstrapAttempt, 0,
			fmt.Errorf("receive same-PID B bootstrap attempt: %w", err))
	}
	if err := verifyExecAttemptImageIdentity(device, inode, gate.bootstrapImage.identity); err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmBootstrapImage, 0,
			fmt.Errorf("B bootstrap attempt differs from the fixed root-owned image: %w", err))
	}
	receipt, err := readCloneCheckoutReady(ctx, int(gate.execAck.Fd()), deadline)
	if err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneCheckout, ReleaseArmCheckoutReceipt, 0,
			fmt.Errorf("receive B checkout-ready receipt: %w", err))
	}
	if err := validateCloneCheckoutReceipt(*command.Clone, receipt); err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneCheckout, ReleaseArmCheckoutReceiptMismatch, 0, err)
	}
	if err := gate.verifyNativeImage(device, inode, gate.bootstrapImage); err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmBootstrapProcess, 0,
			fmt.Errorf("verify exact B bootstrap process: %w", err))
	}
	if err := gate.verifyBLeafMembership(leafFD); err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmLeafMembership, 0, err)
	}
	if err := validateReleaseGuardLive(guard); err != nil {
		return execStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return execStatus{}, err
	}
	// The callback must perform the exact grant and target dev/inode CAS before
	// returning; B's receipt stays a progress claim without A's stop authority.
	if err := onCheckoutReady(ctx, receipt); err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneCheckout, ReleaseArmCheckoutCallback, 0,
			fmt.Errorf("durably record clone checkout-ready receipt: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return execStatus{}, err
	}
	if err := validateReleaseGuardLive(guard); err != nil {
		return execStatus{}, err
	}
	ack, err := EncodeCloneCheckoutReadyAck(receipt)
	if err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneCheckout, ReleaseArmCheckoutAckEncode, 0, err)
	}
	gate.mu.Lock()
	if gate.state != gateReleaseAttempted {
		gate.mu.Unlock()
		clear(ack)
		return execStatus{}, errors.New("clone RELEASE was aborted before checkout ACK")
	}
	if err := ctx.Err(); err != nil {
		gate.mu.Unlock()
		clear(ack)
		return execStatus{}, err
	}
	if err := validateReleaseGuardLive(guard); err != nil {
		gate.mu.Unlock()
		clear(ack)
		return execStatus{}, err
	}
	sendErr := sendCloneStatusPacket(int(gate.execAck.Fd()), ack)
	clear(ack)
	gate.mu.Unlock()
	if sendErr != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepCloneCheckout, ReleaseArmCheckoutAckSend, 0,
			fmt.Errorf("send durable checkout-ready ACK to bootstrap: %w", sendErr))
	}
	status, err := readCloneNativeExecResult(ctx, int(gate.execAck.Fd()), deadline)
	if err != nil {
		return execStatus{}, NewReleaseSubStepError(ReleaseSubStepNativeExec, ReleaseArmNativeCloneProof, 0,
			fmt.Errorf("receive same-PID native exec proof: %w", err))
	}
	return status, nil
}

func validateCloneCheckoutReceipt(grant authority.LinuxCloneGrant, receipt authority.LinuxCloneCheckoutReceipt) error {
	if err := receipt.Validate(); err != nil {
		return fmt.Errorf("invalid B checkout-ready receipt: %w", err)
	}
	intent, seed := grant.Intent, grant.Seed
	if receipt.Version != intent.Version || receipt.RunID != intent.RunID || receipt.Generation != intent.Generation ||
		receipt.BindingKey != intent.BindingKey || receipt.ResourceID != intent.ResourceID || receipt.Target != intent.Target ||
		receipt.Nonce != intent.Nonce || receipt.SeedDigest != seed.Digest || receipt.BaseCommit != intent.BaseCommit {
		return errors.New("B checkout-ready receipt differs from the exact RunJournal clone grant")
	}
	return nil
}

func validateReleaseGuardLive(guard ReleaseGuard) error {
	if guard.OwnerPipe == nil || guard.LeaseDeadline.IsZero() || time.Until(guard.LeaseDeadline) <= 0 {
		return errors.New("RELEASE owner or acknowledged lease guard is no longer live")
	}
	fd := int(guard.OwnerPipe.Fd())
	if fd < 0 {
		return errors.New("RELEASE owner pipe closed before native execution")
	}
	pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for {
		count, err := unix.Poll(pollFDs, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("poll live RELEASE owner pipe: %w", err)
		}
		if count != 0 || pollFDs[0].Revents != 0 {
			return errors.New("RELEASE owner pipe signaled before native execution")
		}
		return nil
	}
}

func (gate *PreparedGate) verifyBLeafMembership(leafFD int) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state != gateReleaseAttempted {
		return errors.New("clone RELEASE was aborted before checkout ACK")
	}
	if err := gate.verifyIdentity(leafFD, false); err != nil {
		return fmt.Errorf("verify exact gate leaf identity after checkout: %w", err)
	}
	return nil
}

// Reaps only the pidfd-identified direct/adopted child: nothing about detached
// descendants or recursive leaf emptiness.
func (gate *PreparedGate) WaitLeader(ctx context.Context) (unix.Siginfo, error) {
	if gate == nil || ctx == nil {
		return unix.Siginfo{}, errors.New("Linux owner wait requires a gate and context")
	}
	gate.mu.Lock()
	if gate.pidfd == nil {
		gate.mu.Unlock()
		return unix.Siginfo{}, errors.New("Linux owner gate pidfd is unavailable")
	}
	if gate.waited || gate.waiting {
		gate.mu.Unlock()
		return unix.Siginfo{}, errors.New("Linux owner leader wait is already in progress or complete")
	}
	gate.waiting = true
	pidfd, err := unix.FcntlInt(gate.pidfd.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	gate.mu.Unlock()
	if err != nil {
		gate.mu.Lock()
		gate.waiting = false
		gate.mu.Unlock()
		return unix.Siginfo{}, fmt.Errorf("duplicate exact gate pidfd for wait: %w", err)
	}
	defer unix.Close(pidfd)
	if err := waitProcessFD(ctx, pidfd, time.Time{}); err != nil {
		gate.mu.Lock()
		gate.waiting = false
		gate.mu.Unlock()
		return unix.Siginfo{}, err
	}
	var info unix.Siginfo
	var waitErr error
	for {
		waitErr = unix.Waitid(unix.P_PIDFD, pidfd, &info, unix.WEXITED, nil)
		if !errors.Is(waitErr, unix.EINTR) {
			break
		}
	}
	if waitErr != nil {
		gate.mu.Lock()
		gate.waiting = false
		gate.mu.Unlock()
		return unix.Siginfo{}, fmt.Errorf("reap exact Linux owner leader by pidfd: %w", waitErr)
	}
	gate.mu.Lock()
	gate.waiting = false
	gate.waited = true
	gate.mu.Unlock()
	return info, nil
}

// Retires descriptors only after exact leader reap, otherwise Abort; it never
// signals a process or authorizes a journal clear.
func (gate *PreparedGate) Close() error {
	if gate == nil {
		return nil
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state == gateClosed {
		return nil
	}
	if !gate.waited || gate.releaseWaiting {
		abortErr := gate.abortLocked()
		return errors.Join(errors.New("Linux owner leader must be reaped and RELEASE acknowledgement settled before closing its retained authority"), abortErr)
	}
	result := gate.closeReleaseLocked()
	if gate.execAck != nil {
		result = errors.Join(result, gate.execAck.Close())
		gate.execAck = nil
	}
	result = errors.Join(result, gate.nativeImage.close())
	gate.nativeImage = nil
	result = errors.Join(result, gate.bootstrapImage.close())
	gate.bootstrapImage = nil
	if gate.pidfd != nil {
		result = errors.Join(result, gate.pidfd.Close())
		gate.pidfd = nil
	}
	clear(gate.token[:])
	gate.state = gateClosed
	return result
}

func (gate *PreparedGate) closeReleaseLocked() error {
	if gate.release == nil {
		return nil
	}
	err := gate.release.Close()
	gate.release = nil
	return err
}

func (gate *PreparedGate) verifyIdentity(leafFD int, requireA bool) error {
	if gate.pidfd == nil || gate.identity.PID <= 0 || gate.identity.StartTime == 0 {
		return errors.New("gate pidfd or creation identity is unavailable")
	}
	if err := unix.PidfdSendSignal(int(gate.pidfd.Fd()), 0, nil, 0); err != nil {
		return fmt.Errorf("probe exact gate pidfd: %w", err)
	}
	pid, err := readPIDFDTarget(int(gate.pidfd.Fd()))
	if err != nil || pid != gate.identity.PID {
		return fmt.Errorf("gate pidfd target differs from launch identity: pid=%d err=%v", pid, err)
	}
	startTime, err := readProcessStartTime(gate.identity.PID)
	if err != nil || startTime != gate.identity.StartTime {
		return fmt.Errorf("gate PID/start identity changed: start=%d err=%v", startTime, err)
	}
	namespace, err := readUserNamespace(gate.identity.PID)
	var unavailable *InaccessibleUserNamespaceError
	if errors.As(err, &unavailable) {
		return unavailable
	}
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return &InaccessibleUserNamespaceError{PID: gate.identity.PID, Cause: err}
	}
	if err != nil || namespace.device != gate.identity.UserNSDevice || namespace.inode != gate.identity.UserNSInode {
		return fmt.Errorf("gate user namespace changed: dev=%d ino=%d err=%v", namespace.device, namespace.inode, err)
	}
	status, err := readProcessStatus(gate.identity.PID)
	if err != nil {
		return err
	}
	if status.parent != gate.identity.OwnerPID || len(status.groups) != 0 {
		return fmt.Errorf("gate is not adopted by exact A helper or has supplementary groups: ppid=%d groups=%v", status.parent, status.groups)
	}
	if requireA && (status.uids != [4]uint32{gate.config.AUID, gate.config.AUID, gate.config.AUID, gate.config.AUID} ||
		status.gids != [4]uint32{gate.config.AGID, gate.config.AGID, gate.config.AGID, gate.config.AGID}) {
		return fmt.Errorf("pre-release gate host credentials are not uniformly A: uid=%v gid=%v", status.uids, status.gids)
	}
	if gate.released && (status.uids != [4]uint32{gate.config.BUID, gate.config.BUID, gate.config.BUID, gate.config.BUID} ||
		status.gids != [4]uint32{gate.config.BGID, gate.config.BGID, gate.config.BGID, gate.config.BGID} ||
		status.noNewPrivs != 1 || status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0) {
		return fmt.Errorf("post-release native identity or capability state changed: uid=%v gid=%v nnp=%d caps=%x/%x/%x/%x/%x",
			status.uids, status.gids, status.noNewPrivs, status.capEff, status.capPrm, status.capInh, status.capBnd, status.capAmb)
	}
	if err := verifyOnlyFixedBMaps(gate.identity.PID, gate.config); err != nil {
		return err
	}
	if err := verifyCgroupLeafFD(leafFD, gate.config.AUID); err != nil {
		return err
	}
	if err := leafContainsPID(leafFD, gate.identity.PID); err != nil {
		return err
	}
	return nil
}

func (gate *PreparedGate) verifyNativeImage(device, inode uint64, expected *pinnedNativeImage) error {
	if expected == nil {
		return errors.New("A-pinned approved native image is unavailable")
	}
	if err := verifyExecAttemptImageIdentity(device, inode, expected.identity); err != nil {
		return err
	}
	if gate.pidfd == nil {
		return NativeExecProcessSide(errors.New("native process pidfd is unavailable"))
	}
	if err := unix.PidfdSendSignal(int(gate.pidfd.Fd()), 0, nil, 0); err != nil {
		return NativeExecProcessSide(fmt.Errorf("native process exited before exec readback: %w", err))
	}
	if err := expected.verifyCurrent(gate.config); err != nil {
		return err
	}
	startTime, err := readProcessStartTime(gate.identity.PID)
	if err != nil || startTime != gate.identity.StartTime {
		return NativeExecProcessSide(fmt.Errorf("native PID/start identity changed: start=%d err=%v", startTime, err))
	}
	status, err := readProcessStatus(gate.identity.PID)
	if err != nil {
		return NativeExecProcessSide(err)
	}
	if status.uids != [4]uint32{gate.config.BUID, gate.config.BUID, gate.config.BUID, gate.config.BUID} ||
		status.gids != [4]uint32{gate.config.BGID, gate.config.BGID, gate.config.BGID, gate.config.BGID} || len(status.groups) != 0 ||
		status.noNewPrivs != 1 || status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0 {
		return NativeExecProcessSide(fmt.Errorf("native exec credentials or privilege state differ: uid=%v gid=%v groups=%v nnp=%d caps=%x/%x/%x/%x/%x",
			status.uids, status.gids, status.groups, status.noNewPrivs, status.capEff, status.capPrm, status.capInh, status.capBnd, status.capAmb))
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", gate.identity.PID))
	if err != nil {
		return fmt.Errorf("stat exact native image: %w", err)
	}
	actualStat, actualOK := actual.Sys().(*syscall.Stat_t)
	if !actualOK || uint64(actualStat.Dev) != expected.identity.Device || actualStat.Ino != expected.identity.Inode {
		return errors.New("same-PID /proc executable readback differs from A-pinned approved image")
	}
	namespace, err := readUserNamespace(gate.identity.PID)
	if err != nil || namespace.device != gate.identity.UserNSDevice || namespace.inode != gate.identity.UserNSInode {
		return NativeExecProcessSide(fmt.Errorf("native user namespace changed across exec: dev=%d ino=%d err=%v",
			namespace.device, namespace.inode, err))
	}
	bootID, err := readBootID()
	if err != nil || bootID != gate.identity.BootID {
		return NativeExecProcessSide(fmt.Errorf("native boot identity changed across exec: got=%q err=%v", bootID, err))
	}
	return nil
}

func waitProcessFD(ctx context.Context, pidfd int, deadline time.Time) error {
	if pidfd < 0 {
		return errors.New("pidfd is unavailable")
	}
	if err := waitForStatus(ctx, pidfd, deadline); err != nil {
		return err
	}
	var poll [1]unix.PollFd
	poll[0] = unix.PollFd{Fd: int32(pidfd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}
	count, err := unix.Poll(poll[:], 0)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("pidfd readiness was not stable")
	}
	return nil
}

func stopLauncher(command *exec.Cmd, pidfd int) error {
	if pidfd >= 0 {
		if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("signal exact transient launcher pidfd: %w", err)
		}
		if err := waitProcessFD(context.Background(), pidfd, time.Now().Add(5*time.Second)); err != nil {
			return fmt.Errorf("wait for exact transient launcher exit: %w", err)
		}
	}
	if command != nil && command.Process != nil {
		if err := command.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, unix.ECHILD) {
			return fmt.Errorf("reap transient launcher: %w", err)
		}
	}
	return nil
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

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
