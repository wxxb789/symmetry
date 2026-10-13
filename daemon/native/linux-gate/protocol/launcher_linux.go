//go:build linux && amd64

// Package protocol implements the fixed-B transient launcher IPC used by the
// Linux native-run helper. Prepare returns only an A-credentialed parked gate;
// callers must durably commit the existing RunJournal handoff and receive the
// initial lease acknowledgement before calling Release exactly once.
package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	OwnerConfigMaxBytes        = 8192
	launcherPath               = "/usr/libexec/symmetry/symmetry-linux-launcher"
	cloneBootstrapPath         = "/usr/libexec/symmetry/symmetry-linux-clone-bootstrap"
	ownerConfigPath            = "/etc/symmetry/linux-owner.conf"
	maxOwnerConfigBytes        = OwnerConfigMaxBytes
	maxOwnerStateDirBytes      = 4095
	launchMagic                = "SYMLA001"
	wireVersion                = uint16(1)
	frameLaunchRequest         = uint16(1)
	frameLaunchResult          = uint16(2)
	frameGateReady             = uint16(3)
	frameGateMap               = uint16(4)
	frameGateParked            = uint16(5)
	frameRelease               = uint16(6)
	frameExecAttempt           = uint16(7)
	frameExecFailure           = uint16(8)
	frameLauncherReady         = uint16(9)
	frameLauncherReadyFailure  = uint16(10)
	frameBootstrapExecAttempt  = uint16(20)
	frameCheckoutReady         = uint16(21)
	frameCheckoutReadyAck      = uint16(22)
	frameNativeExecAttempt     = uint16(23)
	frameCloneBootstrapFailure = uint16(24)
	releaseWireVersion         = uint16(2)
	releaseCloneWireVersion    = uint16(3)
	cloneStatusWireVersion     = uint16(3)
	launchRequestSize          = 92
	launchResultSize           = 128
	gateReadySize              = 16
	gateMapSize                = 60
	gateParkedSize             = 16
	maxLauncherReadySize       = 512
	launcherReadyFailureSize   = 16
	cloneExecAttemptSize       = 28
	cloneFailureSize           = 20
	maxCloneStatusFrameSize    = 64 << 10
	execAttemptSize            = 28
	execFailureSize            = 16
	releaseHeaderSize          = 60
	releaseStdioFDCount        = 3
	releaseFDCount             = 4
	maxReleaseSize             = 128 << 10
	maxCommandString           = 16 << 10
	maxCommandVector           = 128
	bootstrapTimeout           = 15 * time.Second
	execAckTimeout             = 15 * time.Second
	cloneBootstrapTimeout      = 2 * time.Minute
)

var (
	ErrOutcomeUnknown            = errors.New("Linux owner launch outcome is unknown; retain the pinned leaf and do not retry")
	ErrReleaseUsed               = errors.New("Linux owner gate release is single-use")
	ErrInaccessibleUserNamespace = errors.New("Linux owner host cannot inspect the exact user-namespace nsfs identity")
)

// InaccessibleUserNamespaceError is returned when the host procfs/ptrace
// policy prevents A from reading the exact parked gate's user-namespace
// nsfs identity. Linux v2 must fail closed on this host; it never falls back
// to PID-only identity or a native RELEASE.
type InaccessibleUserNamespaceError struct {
	PID   int
	Cause error
}

func (failure *InaccessibleUserNamespaceError) Error() string {
	if failure == nil {
		return ErrInaccessibleUserNamespace.Error()
	}
	return fmt.Sprintf("%v for parked gate PID %d: %v", ErrInaccessibleUserNamespace, failure.PID, failure.Cause)
}

func (failure *InaccessibleUserNamespaceError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return errors.Join(ErrInaccessibleUserNamespace, failure.Cause)
}

// OwnerConfig is the non-secret root-owned A/B identity and optional pinned
// StateDir provisioned by the operator. StateDir is empty only for legacy v1
// owner config; such config remains valid for launch but not root cleanup.
// Namespace UID/GID 1 map to BUID/BGID; host A and host root are deliberately
// absent from both maps.
type OwnerConfig struct {
	AUID     uint32
	AGID     uint32
	BUID     uint32
	BGID     uint32
	StateDir string
}

// ProcessIdentity pins the gate/native process through its pidfd, boot ID and
// creation-time start tick. Namespace fields identify the A-created userns.
type ProcessIdentity struct {
	PID          int
	StartTime    uint64
	BootID       string
	UserNSDevice uint64
	UserNSInode  uint64
	HostUID      uint32
	HostGID      uint32
	OwnerPID     int
}

// nativeImageIdentity is captured from an A-opened O_PATH descriptor before
// RELEASE. The status bytes from B are only a claim; they must match this
// identity and the same-PID /proc/<pid>/exe readback independently.
type nativeImageIdentity struct {
	Device uint64
	Inode  uint64
	UID    uint32
	GID    uint32
	Mode   uint32
	Size   int64
	Links  uint64
}

// pinnedNativeImage retains the exact approved inode for the complete
// RELEASE/readback boundary and remembers the symlink-free path A inspected.
type pinnedNativeImage struct {
	file     *os.File
	path     string
	identity nativeImageIdentity
}

func (image *pinnedNativeImage) close() error {
	if image == nil || image.file == nil {
		return nil
	}
	err := image.file.Close()
	image.file = nil
	return err
}

// LauncherIdentity pins the transient root producer before its one launch
// request is sent. The caller's hook must persist this identity (or fail); a
// hook error closes the private channel and prevents gate creation.
type LauncherIdentity struct {
	PID       int
	StartTime uint64
	BootID    string
	Identity  string
}

// LauncherReady is emitted by the fixed root core only after it has verified
// the sanitized root environment, empty supplementary groups, inherited FDs,
// and authenticated A channel. Its identity must match the command.Start PID.
type LauncherReady struct {
	PID       int
	StartTime uint64
	Identity  string
}

// LauncherReadyFailureStage is a fixed, non-sensitive root startup phase that
// can be reported to the authenticated A channel before READY.
type LauncherReadyFailureStage uint32

const (
	LauncherReadyFailureEnvironment LauncherReadyFailureStage = iota + 1
	LauncherReadyFailureGroups
	LauncherReadyFailureParentFence
	LauncherReadyFailureDescriptors
	LauncherReadyFailurePeer
	LauncherReadyFailureLeaf
	LauncherReadyFailureIdentity
	LauncherReadyFailureEncoding
)

func (stage LauncherReadyFailureStage) String() string {
	switch stage {
	case LauncherReadyFailureEnvironment:
		return "root-environment"
	case LauncherReadyFailureGroups:
		return "supplementary-groups"
	case LauncherReadyFailureParentFence:
		return "parent-death-fence"
	case LauncherReadyFailureDescriptors:
		return "inherited-descriptors"
	case LauncherReadyFailurePeer:
		return "socket-peers"
	case LauncherReadyFailureLeaf:
		return "leaf-identity"
	case LauncherReadyFailureIdentity:
		return "launcher-identity"
	case LauncherReadyFailureEncoding:
		return "ready-encoding"
	default:
		return "unknown"
	}
}

// LauncherReadyFailureError is safe to surface to the A helper: it contains
// only a fixed stage code and never includes B-controlled data or paths.
type LauncherReadyFailureError struct {
	Stage LauncherReadyFailureStage
}

func (failure *LauncherReadyFailureError) Error() string {
	if failure == nil {
		return "transient root launcher failed before READY"
	}
	return "transient root launcher failed READY stage " + failure.Stage.String()
}

// LauncherStartHook durably records the exact root producer after the root
// core's group-cleared READY frame and before Prepare sends the sole gate
// request. pidfd is borrowed; duplicate it if an independent handle is needed.
type LauncherStartHook func(context.Context, LauncherIdentity, *os.File) error

// GateProductionAuth contains the existing per-run journal launch token and
// helper secret. Prepare sends them only over its authenticated inherited
// socket; neither value is placed in argv or environment.
type GateProductionAuth struct {
	LaunchToken string
	Secret      string
}

func (auth GateProductionAuth) Validate() error {
	if !canonicalLowerHex(auth.LaunchToken, authority.TokenBytes) || !canonicalLowerHex(auth.Secret, authority.SecretBytes) {
		return errors.New("Linux gate-production context requires the exact per-run token and secret")
	}
	return nil
}

// LaunchRequest is the authenticated A-to-root fixed prepare request.
// Secret is raw entropy and must be cleared after result verification.
type LaunchRequest struct {
	ReleaseToken [32]byte
	LaunchToken  string
	Secret       [32]byte
}

// LaunchRequestSize is the exact one-packet prepare request size.
const LaunchRequestSize = launchRequestSize

const (
	// LaunchStatusReady means the A gate is mapped, parked, and pidfd-transferred.
	LaunchStatusReady uint32 = iota
	// LaunchStatusRejected means setup failed and the launcher proved its gate child exited.
	LaunchStatusRejected
	// LaunchStatusUnresolved means the root launcher could not prove gate exit.
	LaunchStatusUnresolved
	// LaunchStatusNeverProduced is signed by root only for failures before the
	// core calls Cmd.Start; any attempted clone is unresolved or gate-stopped.
	LaunchStatusNeverProduced
)

// LaunchResult is the fixed result transferred over the launch socket. A
// pidfd is sent separately with SCM_RIGHTS when a gate exists.
type LaunchResult struct {
	Status        uint32
	Errno         uint32
	Token         [32]byte
	Identity      ProcessIdentity
	Authenticator [32]byte
}

// ErrGateNeverProduced identifies the only pre-gate outcome that can support
// a no-target reservation abort. The typed proof is constructed only after an
// authenticated root result and exact root-launcher pidfd exit/reap.
var ErrGateNeverProduced = errors.New("Linux A gate was never produced")

// GateNeverProducedError returns the root-authenticated outcome that no
// clone3 gate was attempted, bound to the exact root launcher process.
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

// Result returns the authenticated authority shape for the caller to bind to
// its current reservation. State still verifies launch-token and launcher
// identity equality; platform must verify the MAC before issuing it.
func (failure *GateNeverProducedError) Result() authority.LinuxCgroupGateProductionResult {
	if failure == nil {
		return authority.LinuxCgroupGateProductionResult{}
	}
	return failure.result
}

// Errno returns the root pre-clone failure errno without exposing the secret.
func (failure *GateNeverProducedError) Errno() uint32 {
	if failure == nil {
		return 0
	}
	return failure.errno
}

// LauncherExitProof returns the helper's exact root-launcher wait/reap proof.
func (failure *GateNeverProducedError) LauncherExitProof() (authority.LinuxCgroupExitProof, bool) {
	if failure == nil || failure.launcherExit == nil {
		return authority.LinuxCgroupExitProof{}, false
	}
	return *failure.launcherExit, true
}

// LauncherOutcomeError preserves producer identity and optional exact
// launcher exit/reap evidence on every post-Start error path. A nil proof is
// explicitly unresolved; callers must not infer exit from the error alone.
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

// NativeCommand is a complete execve request. Before RELEASE, A opens Path
// without following symlinks, requires a direct root/A-owned executable image
// under non-B-writable parents, and pins its inode for status and /proc
// readback. Env is not merged with the helper environment. The three A-owned
// pipe ends are borrowed:
// Release sends exact SCM_RIGHTS duplicates but does not close the caller's
// files. The runner must retire its child-side copies after Release settles so
// stdout and stderr readers can observe EOF. Launcher/release authentication
// material is never added to Args or Env.
type NativeCommand struct {
	Path   string
	Args   []string
	Env    []string
	Dir    string
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	Clone  *authority.LinuxCloneGrant
}

// ReleaseGuard binds the one-use RELEASE to the helper's live owner pipe and
// the absolute deadline from its acknowledged lease. OwnerPipe remains owned
// by the caller; Release duplicates it for SCM_RIGHTS transfer.
type ReleaseGuard struct {
	OwnerPipe     *os.File
	LeaseDeadline time.Time
}

// CloneBootstrapFailureStage is a fixed, non-sensitive phase in the B-owned
// clone/bootstrap handoff. Free-form Git output never crosses the status pipe.
type CloneBootstrapFailureStage uint32

const (
	CloneFailureRequest CloneBootstrapFailureStage = iota + 1
	CloneFailureSeed
	CloneFailureTarget
	CloneFailureGit
	CloneFailureCheckout
	CloneFailureNativeImage
	CloneFailureExec
)

// CloneBootstrapFailureError contains only a fixed stage and errno.
type CloneBootstrapFailureError struct {
	Stage CloneBootstrapFailureStage
	Errno uint32
}

func (failure *CloneBootstrapFailureError) Error() string {
	if failure == nil {
		return "Linux B clone bootstrap failed"
	}
	return fmt.Sprintf("Linux B clone bootstrap failed at stage %d (errno=%d)", failure.Stage, failure.Errno)
}

// PreparedGate owns the launch-transferred pidfd, release writer, exec-status
// reader, and any A-pinned executable images. PIDFD returns a borrowed
// descriptor; the caller must not close it. Release is legal only after durable
// handoff commit and the initial lease acknowledgement. Close never kills a
// process or creates stop proof.
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

// LauncherIdentity returns the root producer recorded by Prepare's mandatory
// durable hook.
func (gate *PreparedGate) LauncherIdentity() LauncherIdentity {
	if gate == nil {
		return LauncherIdentity{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.launcher
}

// LauncherExitProof returns exact root-launcher wait/reap evidence observed
// before Prepare returned. A false result remains unresolved.
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

// ReadInstalledOwnerConfig reads the strict, root-owned, non-writable
// operator configuration. Its IDs and optional canonical StateDir are not secrets.
func ReadInstalledOwnerConfig() (OwnerConfig, error) {
	for _, path := range []string{"/etc", "/etc/symmetry"} {
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return OwnerConfig{}, fmt.Errorf("open fixed Linux owner config directory %q: %w", path, err)
		}
		var directory unix.Stat_t
		statErr := unix.Fstat(fd, &directory)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Gid != 0 || directory.Mode&0022 != 0 {
			return OwnerConfig{}, errors.Join(fmt.Errorf("fixed Linux owner config directory %q is not root-owned and non-writable", path), statErr, closeErr)
		}
	}
	fd, err := unix.Open(ownerConfigPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return OwnerConfig{}, fmt.Errorf("open fixed Linux owner config: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return OwnerConfig{}, fmt.Errorf("stat fixed Linux owner config: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != 0 ||
		stat.Mode&0022 != 0 || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > maxOwnerConfigBytes {
		return OwnerConfig{}, errors.New("fixed Linux owner config has unsafe ownership, mode, link count, or size")
	}
	content := make([]byte, stat.Size+1)
	total := 0
	for total < len(content) {
		n, readErr := unix.Read(fd, content[total:])
		if readErr != nil {
			return OwnerConfig{}, fmt.Errorf("read fixed Linux owner config: %w", readErr)
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total != int(stat.Size) {
		return OwnerConfig{}, errors.New("fixed Linux owner config changed while reading")
	}
	return parseOwnerConfig(content[:total])
}

func parseOwnerConfig(data []byte) (OwnerConfig, error) {
	lines := strings.Split(string(data), "\n")
	legacy := len(lines) == 6 && lines[5] == "" && lines[0] == "version=1"
	withStateDir := len(lines) == 7 && lines[6] == "" && lines[0] == "version=2"
	if !legacy && !withStateDir {
		return OwnerConfig{}, errors.New("fixed Linux owner config has an invalid version or shape")
	}
	values := [4]uint32{}
	for index, key := range []string{"a_uid", "a_gid", "b_uid", "b_gid"} {
		prefix := key + "="
		if !strings.HasPrefix(lines[index+1], prefix) {
			return OwnerConfig{}, fmt.Errorf("fixed Linux owner config field %q is missing or reordered", key)
		}
		text := strings.TrimPrefix(lines[index+1], prefix)
		parsed, err := strconv.ParseUint(text, 10, 32)
		if err != nil || text == "" || strconv.FormatUint(parsed, 10) != text {
			return OwnerConfig{}, fmt.Errorf("fixed Linux owner config field %q is not a canonical uint32", key)
		}
		values[index] = uint32(parsed)
	}
	config := OwnerConfig{AUID: values[0], AGID: values[1], BUID: values[2], BGID: values[3]}
	if withStateDir {
		const prefix = "state_dir="
		if !strings.HasPrefix(lines[5], prefix) {
			return OwnerConfig{}, errors.New("fixed Linux owner config state_dir field is missing or reordered")
		}
		config.StateDir = strings.TrimPrefix(lines[5], prefix)
		if err := validateOwnerStateDir(config.StateDir); err != nil {
			return OwnerConfig{}, err
		}
	}
	if err := config.Validate(); err != nil {
		return OwnerConfig{}, err
	}
	return config, nil
}

// Validate rejects identities which would collapse the host-UID or primary
// group boundary.
func (config OwnerConfig) Validate() error {
	if config.AUID == 0 || config.BUID == 0 || config.AUID == config.BUID {
		return errors.New("Linux owner A and fixed native B must be distinct non-root UIDs")
	}
	if config.AGID == 0 || config.BGID == 0 || config.AGID == config.BGID {
		return errors.New("Linux owner A and fixed native B must be distinct non-root primary GIDs")
	}
	if config.StateDir != "" {
		if err := validateOwnerStateDir(config.StateDir); err != nil {
			return err
		}
	}
	return nil
}

func validateOwnerStateDir(stateDir string) error {
	if stateDir == "" || len(stateDir) > maxOwnerStateDirBytes || !filepath.IsAbs(stateDir) ||
		filepath.Clean(stateDir) != stateDir || stateDir == string(filepath.Separator) {
		return errors.New("fixed Linux owner StateDir must be a canonical absolute non-root path")
	}
	for index := 0; index < len(stateDir); index++ {
		if stateDir[index] < 0x20 || stateDir[index] == 0x7f {
			return errors.New("fixed Linux owner StateDir contains a control character")
		}
	}
	for _, component := range strings.Split(strings.TrimPrefix(stateDir, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return errors.New("fixed Linux owner StateDir contains an unsafe path component")
		}
	}
	return nil
}

// Prepare starts only the root-owned static A gate inside the caller's pinned
// cgroup-v2 leaf. It waits for the root core's authenticated group-cleared READY
// frame, records the exact producer through the mandatory hook, and sends the
// sole launch request only after that durable hook succeeds. The root core
// cannot clone before receiving that request. Prepare then waits for the exact
// launcher exit and verifies A adopted the exact gate child. Callers must
// durably record the RunKey-scoped launch intent before calling Prepare. No
// native program runs here.
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
		// The authenticated request has not been sent. Closing the channel makes
		// the root wrapper/core exit without creating a gate; do not signal by PID.
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
	// The root producer's PID namespace cannot be read by unprivileged A on
	// all supported procfs mounts. Its identity is completed from READY below.
	launcherIdentity := LauncherIdentity{PID: command.Process.Pid, StartTime: launcherStartTime, BootID: helper.BootID}
	producer = launcherIdentity
	ready, err := receiveLauncherReady(ctx, int(controlHelper.Fd()), time.Now().Add(bootstrapTimeout))
	if err != nil {
		stopErr := stopLauncher(command, launcherPIDFD)
		closeFiles(controlHelper, releaseHelper, statusHelper)
		return nil, errors.Join(fmt.Errorf("%w: receive group-cleared root launcher readiness: %w", ErrOutcomeUnknown, err), stopErr)
	}
	// A cannot reliably stat /proc/<root-pid>/ns/pid after the setuid exec:
	// procfs may return the restricted symlink's inode instead of nsfs's inode.
	// The pinned launcher stays in A's PID namespace until this READY frame.
	// Compare the root core's report with A's own namespace and the exact PID
	// and start time captured from command.Start, not that restricted procfs path.
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

// Identity returns the creation-time identity reported by the trusted root
// launcher and independently verified by Prepare.
func (gate *PreparedGate) Identity() ProcessIdentity {
	if gate == nil {
		return ProcessIdentity{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.identity
}

// PIDFD returns a borrowed pidfd for the exact gate/native leader. It remains
// open across exec and must be retained until the caller has completed its
// versioned stop and reap proof.
func (gate *PreparedGate) PIDFD() *os.File {
	if gate == nil {
		return nil
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.pidfd
}

// VerifyLeafMembership checks the exact live leader against cgroup.procs from
// the caller's pinned leaf FD. Call before durable binding, before Release,
// and before stop proof; this point observation is not itself a stop receipt.
func (gate *PreparedGate) VerifyLeafMembership(leafFD int) error {
	if gate == nil || leafFD < 0 {
		return errors.New("Linux owner leaf verification requires a gate and pinned leaf FD")
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.verifyIdentity(leafFD, !gate.released)
}

// Release sends one guarded RELEASE; Clone commands use the v3 bootstrap and
// require A to durably record B's checkout-ready progress claim before native
// execution can begin. That B-origin receipt is not an A-certified clean
// checkout: A validates it against the exact grant and retains all journal and
// cgroup stop authority. The callback runs without gate.mu held. Unknown
// send/ACK results are terminal and are never resent.
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
	if (command.Clone != nil) != (onCheckoutReady != nil) {
		gate.mu.Unlock()
		return errors.New("Linux clone RELEASE requires exactly one checkout-ready callback")
	}
	var bootstrapImage *pinnedNativeImage
	if command.Clone != nil {
		image, bootstrapErr := verifyInstalledCloneBootstrap(gate.config)
		if bootstrapErr != nil {
			gate.mu.Unlock()
			return fmt.Errorf("verify fixed root-owned B clone bootstrap: %w", bootstrapErr)
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
		return errors.Join(fmt.Errorf("%w: verify A gate before RELEASE: %v", ErrOutcomeUnknown, err), abortErr)
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
	guard.OwnerPipe = ownerPipe
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
		return errors.Join(fmt.Errorf("%w: await guarded native exec result: %w", ErrOutcomeUnknown, waitErr), abortErr)
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
		return errors.Join(fmt.Errorf("native exec failed after RELEASE: errno=%d", status.errno), abortErr)
	}
	if !status.execAttempted {
		gate.state = gateUnresolved
		gate.mu.Unlock()
		abortErr := gate.Abort()
		return errors.Join(fmt.Errorf("%w: gate closed exec-status FD without an exec attempt", ErrOutcomeUnknown), abortErr)
	}
	if err := gate.verifyNativeImage(status.device, status.inode, gate.nativeImage); err != nil {
		gate.state = gateUnresolved
		gate.mu.Unlock()
		abortErr := gate.Abort()
		return errors.Join(fmt.Errorf("%w: verify same-PID native exec: %v", ErrOutcomeUnknown, err), abortErr)
	}
	gate.state = gateReleased
	gate.released = true
	gate.mu.Unlock()
	return nil
}

// Abort is idempotent. It terminalizes the gate, half-closes the exec-status
// writer, then closes the release writer. It never waits for the gate or
// signals a PID.
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
		return execStatus{}, fmt.Errorf("receive same-PID B bootstrap attempt: %w", err)
	}
	if err := verifyExecAttemptImageIdentity(device, inode, gate.bootstrapImage.identity); err != nil {
		return execStatus{}, fmt.Errorf("B bootstrap attempt differs from the fixed root-owned image: %w", err)
	}
	receipt, err := readCloneCheckoutReady(ctx, int(gate.execAck.Fd()), deadline)
	if err != nil {
		return execStatus{}, fmt.Errorf("receive B checkout-ready receipt: %w", err)
	}
	if err := validateCloneCheckoutReceipt(*command.Clone, receipt); err != nil {
		return execStatus{}, err
	}
	if err := gate.verifyNativeImage(device, inode, gate.bootstrapImage); err != nil {
		return execStatus{}, fmt.Errorf("verify exact B bootstrap process: %w", err)
	}
	if err := gate.verifyBLeafMembership(leafFD); err != nil {
		return execStatus{}, err
	}
	if err := validateReleaseGuardLive(guard); err != nil {
		return execStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return execStatus{}, err
	}
	// The caller's A-owned callback must perform the exact grant and target
	// dev/inode CAS in its journal/workspace path before returning. B's receipt
	// remains only a progress claim; it never carries A's journal or cgroup stop
	// authority.
	if err := onCheckoutReady(ctx, receipt); err != nil {
		return execStatus{}, fmt.Errorf("durably record clone checkout-ready receipt: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return execStatus{}, err
	}
	if err := validateReleaseGuardLive(guard); err != nil {
		return execStatus{}, err
	}
	ack, err := EncodeCloneCheckoutReadyAck(receipt)
	if err != nil {
		return execStatus{}, err
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
		return execStatus{}, fmt.Errorf("send durable checkout-ready ACK to bootstrap: %w", sendErr)
	}
	status, err := readCloneNativeExecResult(ctx, int(gate.execAck.Fd()), deadline)
	if err != nil {
		return execStatus{}, fmt.Errorf("receive same-PID native exec proof: %w", err)
	}
	return status, nil
}

// validateCloneCheckoutReceipt binds B's progress claim to A's exact grant; it
// does not certify a clean checkout or transfer A's journal/stop authority.
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

// WaitLeader waits and reaps only this pidfd-identified direct/adopted child.
// It says nothing about detached descendants or recursive leaf emptiness.
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

// Close retires protocol descriptors only after exact leader reap. Before reap
// it calls Abort to block pending execution and returns an error; it never
// signals a process, claims that a leaf is empty, or authorizes a journal clear.
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

func verifyExecAttemptImageIdentity(device, inode uint64, expected nativeImageIdentity) error {
	if expected.Inode == 0 || device != expected.Device || inode != expected.Inode {
		return errors.New("B exec-attempt status differs from A-pinned approved image")
	}
	return nil
}

func pinNativeImage(path string, config OwnerConfig) (*pinnedNativeImage, error) {
	parentFD, basename, err := openTrustedImageParent(path, config)
	if err != nil {
		return nil, err
	}
	fd, openErr := unix.Openat(parentFD, basename, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	parentCloseErr := unix.Close(parentFD)
	if openErr != nil {
		return nil, errors.Join(fmt.Errorf("open approved native image %q without following symlinks: %w", path, openErr), parentCloseErr)
	}
	if parentCloseErr != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("close approved native image parent descriptor: %w", parentCloseErr)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("stat A-pinned native image %q: %w", path, err)
	}
	identity, err := approvedNativeImageIdentity(stat, config)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("native image %q is not A-approved: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap pinned native image %q", path)
	}
	return &pinnedNativeImage{file: file, path: path, identity: identity}, nil
}

func openTrustedImageParent(path string, config OwnerConfig) (int, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > maxCommandString || strings.ContainsRune(path, '\x00') {
		return -1, "", errors.New("approved native image path must be a normalized absolute non-root path")
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(components) < 2 {
		return -1, "", errors.New("approved native image must have an A-controlled parent directory")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return -1, "", errors.New("approved native image path contains an unsafe component")
		}
	}
	currentFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open root for approved native image: %w", err)
	}
	if err := verifyTrustedImageDirectory(currentFD, config); err != nil {
		_ = unix.Close(currentFD)
		return -1, "", fmt.Errorf("root is not a trusted native-image parent: %w", err)
	}
	for _, component := range components[:len(components)-1] {
		nextFD, err := unix.Openat(currentFD, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			_ = unix.Close(currentFD)
			return -1, "", fmt.Errorf("open native-image parent component %q without following symlinks: %w", component, err)
		}
		if err := verifyTrustedImageDirectory(nextFD, config); err != nil {
			_ = unix.Close(nextFD)
			_ = unix.Close(currentFD)
			return -1, "", fmt.Errorf("native-image parent component %q is not A-controlled and non-B-writable: %w", component, err)
		}
		if err := unix.Close(currentFD); err != nil {
			_ = unix.Close(nextFD)
			return -1, "", fmt.Errorf("close prior native-image parent descriptor: %w", err)
		}
		currentFD = nextFD
	}
	return currentFD, components[len(components)-1], nil
}

func verifyTrustedImageDirectory(fd int, config OwnerConfig) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || (stat.Uid != 0 && stat.Uid != config.AUID) ||
		stat.Mode&0022 != 0 || stat.Mode&0111 == 0 {
		return errors.New("parent must be a root/A-owned directory with traversal permission and no group/world write")
	}
	return nil
}

func approvedNativeImageIdentity(stat unix.Stat_t, config OwnerConfig) (nativeImageIdentity, error) {
	identity := nativeImageIdentity{
		Device: uint64(stat.Dev), Inode: stat.Ino, UID: stat.Uid, GID: stat.Gid,
		Mode: stat.Mode, Size: stat.Size, Links: stat.Nlink,
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || (stat.Uid != 0 && stat.Uid != config.AUID) ||
		stat.Mode&0022 != 0 || stat.Mode&07000 != 0 || stat.Nlink == 0 || stat.Size <= 0 || stat.Mode&0111 == 0 {
		return nativeImageIdentity{}, errors.New("image must be a root/A-owned regular executable without set-ID bits or B write access")
	}
	return identity, nil
}

func (image *pinnedNativeImage) verifyCurrent(config OwnerConfig) error {
	if image == nil || image.file == nil || image.path == "" || image.identity.Inode == 0 {
		return errors.New("A-pinned native image descriptor is unavailable")
	}
	var pinnedStat unix.Stat_t
	if err := unix.Fstat(int(image.file.Fd()), &pinnedStat); err != nil {
		return fmt.Errorf("re-read A-pinned native image: %w", err)
	}
	pinnedIdentity, err := approvedNativeImageIdentity(pinnedStat, config)
	if err != nil || pinnedIdentity != image.identity {
		return fmt.Errorf("A-pinned native image metadata changed: %v", err)
	}
	current, err := pinNativeImage(image.path, config)
	if err != nil {
		return fmt.Errorf("re-open approved native image path: %w", err)
	}
	defer current.close()
	if current.identity != image.identity {
		return errors.New("approved native image path changed after A pinned it")
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
		return errors.New("native process pidfd is unavailable")
	}
	if err := unix.PidfdSendSignal(int(gate.pidfd.Fd()), 0, nil, 0); err != nil {
		return fmt.Errorf("native process exited before exec readback: %w", err)
	}
	if err := expected.verifyCurrent(gate.config); err != nil {
		return err
	}
	startTime, err := readProcessStartTime(gate.identity.PID)
	if err != nil || startTime != gate.identity.StartTime {
		return fmt.Errorf("native PID/start identity changed: start=%d err=%v", startTime, err)
	}
	status, err := readProcessStatus(gate.identity.PID)
	if err != nil {
		return err
	}
	if status.uids != [4]uint32{gate.config.BUID, gate.config.BUID, gate.config.BUID, gate.config.BUID} ||
		status.gids != [4]uint32{gate.config.BGID, gate.config.BGID, gate.config.BGID, gate.config.BGID} || len(status.groups) != 0 ||
		status.noNewPrivs != 1 || status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0 {
		return fmt.Errorf("native exec credentials or privilege state differ: uid=%v gid=%v groups=%v nnp=%d caps=%x/%x/%x/%x/%x",
			status.uids, status.gids, status.groups, status.noNewPrivs, status.capEff, status.capPrm, status.capInh, status.capBnd, status.capAmb)
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
		return fmt.Errorf("native user namespace changed across exec: dev=%d ino=%d err=%v", namespace.device, namespace.inode, err)
	}
	bootID, err := readBootID()
	if err != nil || bootID != gate.identity.BootID {
		return fmt.Errorf("native boot identity changed across exec: got=%q err=%v", bootID, err)
	}
	return nil
}

func verifyInstalledLauncher(config OwnerConfig) error {
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry"} {
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open fixed launcher directory %q: %w", path, err)
		}
		var directory unix.Stat_t
		statErr := unix.Fstat(fd, &directory)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Gid != 0 || directory.Mode&0022 != 0 {
			return errors.Join(fmt.Errorf("fixed launcher directory %q is not root-owned and non-writable", path), statErr, closeErr)
		}
	}
	fd, err := unix.Open(launcherPath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed root launcher: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat fixed root launcher: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != config.AGID ||
		stat.Mode&07777 != (unix.S_ISUID|0750) || stat.Nlink != 1 {
		return errors.New("fixed launcher is not root:A-group mode 04750, singly-linked, and executable only by A's dedicated group")
	}
	return nil
}

func verifyInstalledCloneBootstrap(config OwnerConfig) (*pinnedNativeImage, error) {
	if err := verifyInstalledLauncher(config); err != nil {
		return nil, err
	}
	image, err := pinNativeImage(cloneBootstrapPath, config)
	if err != nil {
		return nil, fmt.Errorf("open fixed B clone bootstrap: %w", err)
	}
	identity := image.identity
	if identity.UID != 0 || identity.GID != 0 || identity.Mode&07777 != 0755 || identity.Links != 1 {
		_ = image.close()
		return nil, errors.New("fixed B clone bootstrap is not singly-linked root:root mode 0755")
	}
	return image, nil
}

func verifyCgroupLeafFD(fd int, ownerUID uint32) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return fmt.Errorf("stat pinned cgroup leaf filesystem: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat pinned cgroup leaf: %w", err)
	}
	if uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != ownerUID {
		return errors.New("pinned cgroup leaf is not an A-owned cgroup-v2 directory")
	}
	return nil
}

func leafContainsPID(leafFD, pid int) error {
	fd, err := unix.Openat(leafFD, "cgroup.procs", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open pinned leaf cgroup.procs: %w", err)
	}
	defer unix.Close(fd)
	buffer := make([]byte, 64<<10)
	n, err := unix.Read(fd, buffer)
	if err != nil {
		return fmt.Errorf("read pinned leaf cgroup.procs: %w", err)
	}
	if n == len(buffer) {
		return errors.New("pinned leaf cgroup.procs exceeded the bounded read")
	}
	wanted := strconv.Itoa(pid)
	for _, line := range strings.Fields(string(buffer[:n])) {
		if line == wanted {
			return nil
		}
	}
	return fmt.Errorf("exact gate PID %d is not in the pinned run leaf", pid)
}

func verifyOnlyFixedBMaps(pid int, config OwnerConfig) error {
	uidMap, err := readProcFile(pid, "uid_map")
	if err != nil {
		return err
	}
	gidMap, err := readProcFile(pid, "gid_map")
	if err != nil {
		return err
	}
	setgroups, err := readProcFile(pid, "setgroups")
	if err != nil {
		return err
	}
	if canonicalMap(uidMap) != fmt.Sprintf("1 %d 1", config.BUID) || canonicalMap(gidMap) != fmt.Sprintf("1 %d 1", config.BGID) || strings.TrimSpace(setgroups) != "deny" {
		return fmt.Errorf("A gate user namespace is not the exact B-only map: uid=%q gid=%q setgroups=%q", uidMap, gidMap, setgroups)
	}
	return nil
}

func readProcFile(pid int, name string) (string, error) {
	if pid <= 1 || (name != "uid_map" && name != "gid_map" && name != "setgroups") {
		return "", errors.New("read Linux user-namespace proc file requires a fixed name and positive PID")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("read exact process %s: %w", name, err)
	}
	if len(data) == 0 || len(data) > 4096 {
		return "", fmt.Errorf("exact process %s has an invalid size %d", name, len(data))
	}
	return string(data), nil
}

func canonicalMap(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

type procStatus struct {
	uids       [4]uint32
	gids       [4]uint32
	groups     []uint32
	parent     int
	capEff     uint64
	capPrm     uint64
	capInh     uint64
	capBnd     uint64
	capAmb     uint64
	noNewPrivs uint32
}

func readProcessStatus(pid int) (procStatus, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return procStatus{}, fmt.Errorf("read exact process status: %w", err)
	}
	var status procStatus
	seen := uint16(0)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		parseIDs := func(destination *[4]uint32) error {
			if len(fields) != 4 {
				return errors.New("process status credential vector is not four IDs")
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
		parseNumber := func(base int, bits int) (uint64, error) {
			if len(fields) != 1 {
				return 0, errors.New("process status scalar is malformed")
			}
			return strconv.ParseUint(fields[0], base, bits)
		}
		var err error
		switch key {
		case "Uid":
			err = parseIDs(&status.uids)
			seen |= 1
		case "Gid":
			err = parseIDs(&status.gids)
			seen |= 2
		case "Groups":
			status.groups = make([]uint32, len(fields))
			for index, field := range fields {
				parsed, parseErr := strconv.ParseUint(field, 10, 32)
				if parseErr != nil {
					err = parseErr
					break
				}
				status.groups[index] = uint32(parsed)
			}
			seen |= 4
		case "PPid":
			var value uint64
			value, err = parseNumber(10, 31)
			status.parent = int(value)
			seen |= 8
		case "CapEff":
			status.capEff, err = parseNumber(16, 64)
			seen |= 16
		case "CapPrm":
			status.capPrm, err = parseNumber(16, 64)
			seen |= 32
		case "CapInh":
			status.capInh, err = parseNumber(16, 64)
			seen |= 64
		case "CapBnd":
			status.capBnd, err = parseNumber(16, 64)
			seen |= 256
		case "CapAmb":
			status.capAmb, err = parseNumber(16, 64)
			seen |= 512
		case "NoNewPrivs":
			var value uint64
			value, err = parseNumber(10, 32)
			status.noNewPrivs = uint32(value)
			seen |= 128
		}
		if err != nil {
			return procStatus{}, fmt.Errorf("parse process status %s: %w", key, err)
		}
	}
	if seen&15 != 15 {
		return procStatus{}, errors.New("process status lacks UID/GID/Groups/PPid fields")
	}
	if seen&1008 != 1008 {
		return procStatus{}, errors.New("process status lacks capability bounding/ambient or no_new_privs fields")
	}
	return status, nil
}

type namespaceStat struct{ device, inode uint64 }

func readUserNamespace(pid int) (namespaceStat, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/user", pid), &stat); err != nil {
		return namespaceStat{}, fmt.Errorf("stat exact user namespace: %w", err)
	}
	return namespaceIdentityFromStat(pid, stat)
}

func namespaceIdentityFromStat(pid int, stat syscall.Stat_t) (namespaceStat, error) {
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Ino == 0 {
		cause := fmt.Errorf("exact user namespace is not an accessible nsfs inode: mode=%#o inode=%d", stat.Mode, stat.Ino)
		return namespaceStat{}, &InaccessibleUserNamespaceError{PID: pid, Cause: cause}
	}
	return namespaceStat{device: uint64(stat.Dev), inode: stat.Ino}, nil
}

func readProcessStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read exact process stat: %w", err)
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return 0, errors.New("process stat lacks a command terminator")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("process stat lacks start-time field")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, fmt.Errorf("process start-time field is invalid: %w", err)
	}
	return start, nil
}

func readPIDFDTarget(pidfd int) (int, error) {
	fdInfo, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd))
	if err != nil {
		return 0, fmt.Errorf("read exact pidfd info: %w", err)
	}
	for _, line := range strings.Split(string(fdInfo), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && key == "Pid" {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil || pid <= 0 {
				return 0, errors.New("pidfd info has invalid target PID")
			}
			return pid, nil
		}
	}
	return 0, errors.New("fdinfo does not identify a pidfd target")
}

func readBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read kernel boot identity: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" || len(value) > 64 {
		return "", errors.New("kernel boot identity is empty or oversized")
	}
	return value, nil
}

// CaptureLauncherIdentity uses the platform's existing linux:v2 identity
// spelling (boot ID, PID namespace inode, PID, start tick). A restricted
// /proc/<pid>/ns/pid symlink is not a namespace identity.
func CaptureLauncherIdentity(pid int) (LauncherIdentity, error) {
	if pid <= 1 {
		return LauncherIdentity{}, errors.New("launcher identity requires a PID above one")
	}
	startTime, err := readProcessStartTime(pid)
	if err != nil {
		return LauncherIdentity{}, err
	}
	bootID, err := readBootID()
	if err != nil {
		return LauncherIdentity{}, err
	}
	var namespace syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/pid", pid), &namespace); err != nil {
		return LauncherIdentity{}, fmt.Errorf("stat launcher PID namespace: %w", err)
	}
	if namespace.Mode&syscall.S_IFMT != syscall.S_IFREG || namespace.Ino == 0 {
		return LauncherIdentity{}, errors.New("launcher PID namespace is not an accessible nsfs inode")
	}
	identity := fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, namespace.Ino, pid, startTime)
	return LauncherIdentity{PID: pid, StartTime: startTime, BootID: bootID, Identity: identity}, nil
}

func probePIDFDSupport() error {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return fmt.Errorf("open process pidfd: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("pidfd signal-zero probe: %w", err)
	}
	var info unix.Siginfo
	err = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
	if !errors.Is(err, unix.ECHILD) {
		return fmt.Errorf("pidfd waitid probe did not return the expected non-child result: %w", err)
	}
	return nil
}

func sendPacket(fd int, packet []byte) error {
	if len(packet) == 0 {
		return errors.New("Linux owner protocol packet is empty")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("Linux owner seqpacket was short: wrote %d of %d bytes", n, len(packet))
	}
	return nil
}

func sendPacketWithRights(fd int, packet []byte, rights []int) error {
	if len(rights) != releaseFDCount {
		return errors.New("RELEASE v2 requires exactly three stdio pipes and the owner pipe")
	}
	oob := unix.UnixRights(rights...)
	n, err := unix.SendmsgN(fd, packet, oob, nil, unix.MSG_NOSIGNAL|unix.MSG_DONTWAIT)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("Linux owner RELEASE seqpacket was short: wrote %d of %d bytes", n, len(packet))
	}
	return nil
}

func receiveLauncherResult(ctx context.Context, fd int, deadline time.Time) ([]byte, int, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, -1, err
	}
	data := make([]byte, launchResultSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, -1, err
	}
	var fds []int
	if oobn != 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, -1, err
		}
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n != launchResultSize {
		closeFDs(fds)
		return nil, -1, errors.New("root launcher result has an invalid size or truncation flag")
	}
	if len(fds) > 1 {
		closeFDs(fds)
		return nil, -1, errors.New("root launcher returned more than one pidfd")
	}
	if len(fds) == 0 {
		return data, -1, nil
	}
	return data, fds[0], nil
}

func receiveLauncherReady(ctx context.Context, fd int, deadline time.Time) (LauncherReady, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return LauncherReady{}, err
	}
	data := make([]byte, maxLauncherReadySize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return LauncherReady{}, err
	}
	if oobn != 0 {
		fds, parseErr := parseRights(oob[:oobn])
		closeFDs(fds)
		if parseErr != nil {
			return LauncherReady{}, parseErr
		}
		return LauncherReady{}, errors.New("transient launcher readiness carried unexpected descriptors")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return LauncherReady{}, errors.New("transient launcher sent a zero-length READY packet")
		}
		return LauncherReady{}, io.EOF
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > maxLauncherReadySize {
		return LauncherReady{}, errors.New("transient launcher readiness has invalid size or truncation flags")
	}
	if n >= 12 && binary.LittleEndian.Uint16(data[10:12]) == frameLauncherReadyFailure {
		stage, err := DecodeLauncherReadyFailure(data[:n])
		if err != nil {
			return LauncherReady{}, err
		}
		return LauncherReady{}, &LauncherReadyFailureError{Stage: stage}
	}
	if n < 29 {
		return LauncherReady{}, errors.New("transient launcher readiness frame is too short")
	}
	return DecodeLauncherReady(data[:n])
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
			return nil, errors.New("Linux owner channel carried non-SCM_RIGHTS ancillary data")
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

func waitForStatus(ctx context.Context, fd int, deadline time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := 100 * time.Millisecond
		if !deadline.IsZero() {
			remaining = time.Until(deadline)
			if remaining <= 0 {
				return context.DeadlineExceeded
			}
		}
		pollFor := remaining
		if pollFor > 100*time.Millisecond {
			pollFor = 100 * time.Millisecond
		}
		millis := int(math.Ceil(float64(pollFor) / float64(time.Millisecond)))
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
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

func encodeLaunchRequest(token [32]byte, auth GateProductionAuth) ([]byte, error) {
	launchToken, err := hex.DecodeString(auth.LaunchToken)
	if err != nil || len(launchToken) != authority.TokenBytes || hex.EncodeToString(launchToken) != auth.LaunchToken {
		return nil, errors.New("Linux prepare request launch token is invalid")
	}
	secret, err := hex.DecodeString(auth.Secret)
	if err != nil || len(secret) != authority.SecretBytes || hex.EncodeToString(secret) != auth.Secret {
		clear(launchToken)
		return nil, errors.New("Linux prepare request authority secret is invalid")
	}
	packet := make([]byte, launchRequestSize)
	copy(packet[:8], launchMagic)
	binary.LittleEndian.PutUint16(packet[8:10], wireVersion)
	binary.LittleEndian.PutUint16(packet[10:12], frameLaunchRequest)
	copy(packet[12:44], token[:])
	copy(packet[44:60], launchToken)
	copy(packet[60:92], secret)
	clear(launchToken)
	clear(secret)
	return packet, nil
}

// EncodeLauncherReady binds the root core's group-cleared readiness event to
// the exact process identity which the A helper will durably record.
func EncodeLauncherReady(identity LauncherIdentity) ([]byte, error) {
	if identity.PID <= 1 || identity.PID > int(^uint32(0)>>1) || identity.StartTime == 0 ||
		identity.Identity == "" || len(identity.Identity) > 256 || strings.ContainsRune(identity.Identity, '\x00') {
		return nil, errors.New("transient launcher readiness identity is invalid")
	}
	packet := fixedFrame(frameLauncherReady, 12)
	packet = appendWireUint32(packet, uint32(identity.PID))
	var startTime [8]byte
	binary.LittleEndian.PutUint64(startTime[:], identity.StartTime)
	packet = append(packet, startTime[:]...)
	packet = appendWireString(packet, identity.Identity)
	if len(packet) > maxLauncherReadySize {
		clear(packet)
		return nil, errors.New("transient launcher readiness frame exceeds its fixed bound")
	}
	return packet, nil
}

// DecodeLauncherReady accepts only a bounded, exact producer identity frame.
func DecodeLauncherReady(packet []byte) (LauncherReady, error) {
	var ready LauncherReady
	if len(packet) < 29 || len(packet) > maxLauncherReadySize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLauncherReady {
		return ready, errors.New("transient launcher readiness magic, version, type, or size is invalid")
	}
	position := 12
	pid, err := readWireUint32(packet, &position)
	if err != nil || pid <= 1 {
		return LauncherReady{}, errors.New("transient launcher readiness PID is invalid")
	}
	if len(packet)-position < 8 {
		return LauncherReady{}, errors.New("transient launcher readiness start time is truncated")
	}
	ready.PID = int(pid)
	ready.StartTime = binary.LittleEndian.Uint64(packet[position : position+8])
	position += 8
	ready.Identity, err = readWireString(packet, &position)
	if err != nil || ready.StartTime == 0 || ready.Identity == "" || len(ready.Identity) > 256 || position != len(packet) {
		return LauncherReady{}, errors.New("transient launcher readiness identity is invalid or has trailing bytes")
	}
	return ready, nil
}

// EncodeLauncherReadyFailure reports one fixed readiness stage without
// serializing paths, syscall text, or any caller-controlled values.
func EncodeLauncherReadyFailure(stage LauncherReadyFailureStage) []byte {
	packet := fixedFrame(frameLauncherReadyFailure, launcherReadyFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], uint32(stage))
	return packet
}

// DecodeLauncherReadyFailure validates the fixed pre-READY failure frame.
func DecodeLauncherReadyFailure(packet []byte) (LauncherReadyFailureStage, error) {
	if validateFixedFrame(packet, frameLauncherReadyFailure, launcherReadyFailureSize) != nil {
		return 0, errors.New("transient launcher readiness failure frame is malformed")
	}
	stage := LauncherReadyFailureStage(binary.LittleEndian.Uint32(packet[12:16]))
	if stage < LauncherReadyFailureEnvironment || stage > LauncherReadyFailureEncoding {
		return 0, errors.New("transient launcher readiness failure stage is invalid")
	}
	return stage, nil
}

func DecodeLaunchResult(packet []byte) (LaunchResult, error) {
	if len(packet) != launchResultSize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLaunchResult {
		return LaunchResult{}, errors.New("root launcher result magic, version, or type is invalid")
	}
	var result LaunchResult
	copy(result.Token[:], packet[20:52])
	if binary.LittleEndian.Uint32(packet[68:72]) != 0 {
		return LaunchResult{}, errors.New("root launcher result reserved field is nonzero")
	}
	result.Status = binary.LittleEndian.Uint32(packet[12:16])
	result.Errno = binary.LittleEndian.Uint32(packet[16:20])
	result.Identity = ProcessIdentity{
		PID:          int(binary.LittleEndian.Uint32(packet[52:56])),
		HostUID:      binary.LittleEndian.Uint32(packet[56:60]),
		HostGID:      binary.LittleEndian.Uint32(packet[60:64]),
		OwnerPID:     int(binary.LittleEndian.Uint32(packet[64:68])),
		StartTime:    binary.LittleEndian.Uint64(packet[72:80]),
		UserNSDevice: binary.LittleEndian.Uint64(packet[80:88]),
		UserNSInode:  binary.LittleEndian.Uint64(packet[88:96]),
	}
	copy(result.Authenticator[:], packet[96:128])
	if result.Status > LaunchStatusNeverProduced || result.Errno > uint32(^uint32(0)>>1) {
		return LaunchResult{}, errors.New("root launcher result status or errno is invalid")
	}
	if result.Status == LaunchStatusReady && (result.Identity.PID <= 1 || result.Identity.StartTime == 0 ||
		result.Identity.OwnerPID <= 1 || result.Identity.UserNSInode == 0) {
		return LaunchResult{}, errors.New("root launcher returned an invalid gate identity")
	}
	return result, nil
}

// EncodeLaunchResult serializes the immutable root-to-A result frame.
func EncodeLaunchResult(result LaunchResult) ([]byte, error) {
	if result.Status > LaunchStatusNeverProduced || result.Errno > uint32(^uint32(0)>>1) {
		return nil, errors.New("Linux owner launcher result status or errno is invalid")
	}
	packet := fixedFrame(frameLaunchResult, launchResultSize)
	binary.LittleEndian.PutUint32(packet[12:16], result.Status)
	binary.LittleEndian.PutUint32(packet[16:20], result.Errno)
	copy(packet[20:52], result.Token[:])
	binary.LittleEndian.PutUint32(packet[52:56], uint32(result.Identity.PID))
	binary.LittleEndian.PutUint32(packet[56:60], result.Identity.HostUID)
	binary.LittleEndian.PutUint32(packet[60:64], result.Identity.HostGID)
	binary.LittleEndian.PutUint32(packet[64:68], uint32(result.Identity.OwnerPID))
	binary.LittleEndian.PutUint64(packet[72:80], result.Identity.StartTime)
	binary.LittleEndian.PutUint64(packet[80:88], result.Identity.UserNSDevice)
	binary.LittleEndian.PutUint64(packet[88:96], result.Identity.UserNSInode)
	copy(packet[96:128], result.Authenticator[:])
	return packet, nil
}

// DecodeLaunchRequest validates the exact A-to-root prepare request.
func DecodeLaunchRequest(packet []byte) (LaunchRequest, error) {
	var request LaunchRequest
	if len(packet) != launchRequestSize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLaunchRequest {
		return request, errors.New("Linux owner prepare request magic, version, type, or size is invalid")
	}
	copy(request.ReleaseToken[:], packet[12:44])
	request.LaunchToken = hex.EncodeToString(packet[44:60])
	copy(request.Secret[:], packet[60:92])
	if request.ReleaseToken == [32]byte{} || request.LaunchToken == strings.Repeat("0", authority.TokenBytes*2) || request.Secret == [32]byte{} {
		clear(request.Secret[:])
		return LaunchRequest{}, errors.New("Linux owner prepare request carries an empty token or secret")
	}
	return request, nil
}

// EncodeGateReady reports that the static gate dropped to A and created its
// child user namespace.
func EncodeGateReady() []byte { return fixedFrame(frameGateReady, gateReadySize) }

// ValidateGateReady validates the exact gate-to-root readiness frame.
func ValidateGateReady(packet []byte) error {
	return validateFixedFrame(packet, frameGateReady, gateReadySize)
}

// EncodeGateMap carries the fixed IDs and one-use token only over the private
// root-to-static-gate socket. It is never placed in argv or environment.
func EncodeGateMap(config OwnerConfig, token [32]byte) []byte {
	packet := fixedFrame(frameGateMap, gateMapSize)
	binary.LittleEndian.PutUint32(packet[12:16], config.AUID)
	binary.LittleEndian.PutUint32(packet[16:20], config.AGID)
	binary.LittleEndian.PutUint32(packet[20:24], config.BUID)
	binary.LittleEndian.PutUint32(packet[24:28], config.BGID)
	copy(packet[28:], token[:])
	return packet
}

// DecodeGateMap validates the exact root-to-gate B-only map acknowledgement.
func DecodeGateMap(packet []byte) (OwnerConfig, [32]byte, error) {
	var token [32]byte
	if err := validateFixedFrame(packet, frameGateMap, gateMapSize); err != nil {
		return OwnerConfig{}, token, err
	}
	config := OwnerConfig{
		AUID: binary.LittleEndian.Uint32(packet[12:16]),
		AGID: binary.LittleEndian.Uint32(packet[16:20]),
		BUID: binary.LittleEndian.Uint32(packet[20:24]),
		BGID: binary.LittleEndian.Uint32(packet[24:28]),
	}
	if err := config.Validate(); err != nil {
		return OwnerConfig{}, token, err
	}
	copy(token[:], packet[28:60])
	return config, token, nil
}

// EncodeGateParked reports that the A gate accepted its one-use token and is
// parked with no native instruction executed.
func EncodeGateParked() []byte { return fixedFrame(frameGateParked, gateParkedSize) }

// ValidateGateParked validates the exact pre-RELEASE parking acknowledgement.
func ValidateGateParked(packet []byte) error {
	return validateFixedFrame(packet, frameGateParked, gateParkedSize)
}

func validateFixedFrame(packet []byte, kind uint16, size int) error {
	if len(packet) != size || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return errors.New("Linux owner fixed frame magic, version, type, or size is invalid")
	}
	return nil
}

func fixedFrame(kind uint16, size int) []byte {
	packet := make([]byte, size)
	copy(packet[:8], launchMagic)
	binary.LittleEndian.PutUint16(packet[8:10], wireVersion)
	binary.LittleEndian.PutUint16(packet[10:12], kind)
	return packet
}

// EncodeRelease creates a one-use guarded RELEASE. Non-clone scratch commands
// use v2; a clone grant is encoded only in v3 and is never downgraded. The
// fixed header binds an absolute CLOCK_MONOTONIC cutoff; SCM_RIGHTS always
// carries three stdio pipes and the owner-pipe read end.
func EncodeRelease(token [32]byte, command NativeCommand, guard ReleaseGuard) ([]byte, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	if err := validateReleaseOwnerPipe(guard.OwnerPipe, command); err != nil {
		return nil, err
	}
	deadlineNS, err := monotonicDeadlineNS(guard.LeaseDeadline)
	if err != nil {
		return nil, err
	}
	return encodeGuardedRelease(token, command, deadlineNS)
}

func encodeGuardedRelease(token [32]byte, command NativeCommand, deadlineNS uint64) ([]byte, error) {
	if deadlineNS == 0 {
		return nil, errors.New("Linux RELEASE requires a nonzero monotonic lease cutoff")
	}
	body := make([]byte, 0, 24+len(command.Path)+len(command.Dir))
	body = appendWireString(body, command.Path)
	body = appendWireUint32(body, uint32(len(command.Args)))
	for _, value := range command.Args {
		body = appendWireString(body, value)
	}
	body = appendWireUint32(body, uint32(len(command.Env)))
	for _, value := range command.Env {
		body = appendWireString(body, value)
	}
	body = appendWireString(body, command.Dir)
	version := releaseWireVersion
	if command.Clone != nil {
		cloneGrant, err := json.Marshal(command.Clone)
		if err != nil {
			return nil, fmt.Errorf("encode A-authorized Linux clone grant: %w", err)
		}
		if len(cloneGrant) == 0 || len(cloneGrant) > maxReleaseSize-releaseHeaderSize-len(body)-4 {
			clear(cloneGrant)
			return nil, errors.New("Linux clone grant exceeds the guarded RELEASE bound")
		}
		body = appendWireUint32(body, uint32(len(cloneGrant)))
		body = append(body, cloneGrant...)
		clear(cloneGrant)
		version = releaseCloneWireVersion
	}
	if len(body) > maxReleaseSize-releaseHeaderSize {
		return nil, errors.New("Linux owner native command exceeds the release bound")
	}
	packet := fixedFrame(frameRelease, releaseHeaderSize+len(body))
	binary.LittleEndian.PutUint16(packet[8:10], version)
	copy(packet[12:44], token[:])
	binary.LittleEndian.PutUint32(packet[44:48], uint32(len(body)))
	binary.LittleEndian.PutUint32(packet[48:52], releaseFDCount)
	binary.LittleEndian.PutUint64(packet[52:60], deadlineNS)
	copy(packet[releaseHeaderSize:], body)
	return packet, nil
}

// DecodeRelease accepts a direct v2 or clone v3 frame, the exact fresh token,
// four descriptors, and a bounded complete command.
func DecodeRelease(packet []byte, expectedToken [32]byte, descriptors [releaseFDCount]*os.File) (NativeCommand, uint64, error) {
	return decodeReleaseFrame(packet, expectedToken, descriptors, true, false)
}

// DecodeCloneBootstrapRequest decodes the sealed, token-scrubbed v3 request
// passed to the trusted B bootstrap. The inherited control descriptors are
// validated by the C gate before this process is entered.
func DecodeCloneBootstrapRequest(packet []byte) (NativeCommand, uint64, error) {
	var noToken [32]byte
	return decodeReleaseFrame(packet, noToken, [releaseFDCount]*os.File{}, false, true)
}

func decodeReleaseFrame(packet []byte, expectedToken [32]byte, descriptors [releaseFDCount]*os.File,
	validateDescriptors, bootstrap bool) (NativeCommand, uint64, error) {
	if len(packet) < releaseHeaderSize || len(packet) > maxReleaseSize || string(packet[:8]) != launchMagic ||
		(binary.LittleEndian.Uint16(packet[8:10]) != releaseWireVersion && binary.LittleEndian.Uint16(packet[8:10]) != releaseCloneWireVersion) ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameRelease {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE v2/v3 header version or type is invalid")
	}
	version := binary.LittleEndian.Uint16(packet[8:10])
	if bootstrap && version != releaseCloneWireVersion {
		return NativeCommand{}, 0, errors.New("clone bootstrap requires RELEASE v3")
	}
	if subtle.ConstantTimeCompare(packet[12:44], expectedToken[:]) != 1 {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE token is stale")
	}
	bodySize := binary.LittleEndian.Uint32(packet[44:48])
	if binary.LittleEndian.Uint32(packet[48:52]) != releaseFDCount {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE requires exactly four descriptors")
	}
	if uint64(bodySize) != uint64(len(packet)-releaseHeaderSize) {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE body size is invalid")
	}
	deadlineNS := binary.LittleEndian.Uint64(packet[52:60])
	if deadlineNS == 0 {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE monotonic deadline is empty")
	}
	body := packet[releaseHeaderSize:]
	position := 0
	path, err := readWireString(body, &position)
	if err != nil {
		return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE path: %w", err)
	}
	argc, err := readWireUint32(body, &position)
	if err != nil || argc == 0 || argc > maxCommandVector {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE argv count is invalid")
	}
	command := NativeCommand{Path: path, Args: make([]string, int(argc))}
	for index := range command.Args {
		command.Args[index], err = readWireString(body, &position)
		if err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE argv[%d]: %w", index, err)
		}
	}
	envc, err := readWireUint32(body, &position)
	if err != nil || envc > maxCommandVector {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE environment count is invalid")
	}
	command.Env = make([]string, int(envc))
	for index := range command.Env {
		command.Env[index], err = readWireString(body, &position)
		if err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE env[%d]: %w", index, err)
		}
	}
	command.Dir, err = readWireString(body, &position)
	if err != nil {
		return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE working directory: %w", err)
	}
	if version == releaseCloneWireVersion {
		cloneSize, sizeErr := readWireUint32(body, &position)
		if sizeErr != nil || cloneSize == 0 || uint64(cloneSize) > uint64(len(body)-position) {
			return NativeCommand{}, 0, errors.New("Linux clone RELEASE grant payload is empty or truncated")
		}
		decoder := json.NewDecoder(bytes.NewReader(body[position : position+int(cloneSize)]))
		decoder.DisallowUnknownFields()
		var grant authority.LinuxCloneGrant
		if err := decoder.Decode(&grant); err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux clone RELEASE grant: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return NativeCommand{}, 0, errors.New("Linux clone RELEASE grant has trailing JSON data")
		}
		if err := grant.Validate(); err != nil {
			return NativeCommand{}, 0, fmt.Errorf("validate Linux clone RELEASE grant: %w", err)
		}
		command.Clone = &grant
		position += int(cloneSize)
	}
	if position != len(body) {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE has trailing binary data")
	}
	if validateDescriptors {
		command.Stdin, command.Stdout, command.Stderr = descriptors[0], descriptors[1], descriptors[2]
		if err := command.Validate(); err != nil {
			return NativeCommand{}, 0, err
		}
		if err := validateReleaseOwnerPipe(descriptors[3], command); err != nil {
			return NativeCommand{}, 0, err
		}
	} else if err := command.validateFields(); err != nil {
		return NativeCommand{}, 0, err
	}
	if bootstrap && command.Clone == nil {
		return NativeCommand{}, 0, errors.New("clone bootstrap request lacks an A-authorized grant")
	}
	return command, deadlineNS, nil
}

func monotonicDeadlineNS(deadline time.Time) (uint64, error) {
	if deadline.IsZero() {
		return 0, errors.New("Linux RELEASE requires an acknowledged monotonic lease deadline")
	}
	if deadline == deadline.Round(0) {
		return 0, errors.New("Linux RELEASE lease deadline has no monotonic component")
	}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return 0, fmt.Errorf("read CLOCK_MONOTONIC for RELEASE: %w", err)
	}
	// Sample Go's monotonic deadline after CLOCK_MONOTONIC so conversion is
	// conservative: syscall/dispatch delay can shorten, never extend, the lease.
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, errors.New("Linux RELEASE lease deadline has already expired")
	}
	if now.Sec < 0 || now.Nsec < 0 || now.Nsec >= 1_000_000_000 {
		return 0, errors.New("CLOCK_MONOTONIC returned an invalid time")
	}
	const nanosecondsPerSecond = uint64(1_000_000_000)
	seconds := uint64(now.Sec)
	if seconds > ^uint64(0)/nanosecondsPerSecond {
		return 0, errors.New("CLOCK_MONOTONIC seconds overflow RELEASE deadline")
	}
	baseNS := seconds * nanosecondsPerSecond
	nanoseconds := uint64(now.Nsec)
	if nanoseconds > ^uint64(0)-baseNS {
		return 0, errors.New("CLOCK_MONOTONIC nanoseconds overflow RELEASE deadline")
	}
	nowNS := baseNS + nanoseconds
	delta := uint64(remaining.Nanoseconds())
	if delta > ^uint64(0)-nowNS {
		return 0, errors.New("lease deadline overflows monotonic nanoseconds")
	}
	return nowNS + delta, nil
}

func validateReleaseOwnerPipe(ownerPipe *os.File, command NativeCommand) error {
	if ownerPipe == nil {
		return errors.New("Linux RELEASE requires the helper-owned read-only owner pipe")
	}
	fd := int(ownerPipe.Fd())
	if fd < 0 {
		return errors.New("Linux RELEASE owner pipe descriptor is closed")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat helper owner pipe: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFIFO || stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
		return errors.New("Linux RELEASE owner pipe is not an A-owned FIFO")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
		return fmt.Errorf("Linux RELEASE owner pipe is not read-only: flags=%#x err=%v", flags, err)
	}
	for _, pipe := range []*os.File{command.Stdin, command.Stdout, command.Stderr} {
		var pipeStat unix.Stat_t
		if err := unix.Fstat(int(pipe.Fd()), &pipeStat); err != nil {
			return fmt.Errorf("stat native stdio while checking owner pipe: %w", err)
		}
		if stat.Dev == pipeStat.Dev && stat.Ino == pipeStat.Ino {
			return errors.New("Linux RELEASE owner pipe aliases a native stdio pipe")
		}
	}
	return nil
}

func (command NativeCommand) Validate() error {
	if err := command.validateFields(); err != nil {
		return err
	}
	return validateRunnerPipes(command.Stdin, command.Stdout, command.Stderr)
}

func (command NativeCommand) validateFields() error {
	if !filepath.IsAbs(command.Path) || command.Path == "/" || len(command.Path) > maxCommandString || strings.ContainsRune(command.Path, '\x00') {
		return errors.New("Linux owner native executable path must be an absolute non-root path")
	}
	if !filepath.IsAbs(command.Dir) || len(command.Dir) > maxCommandString || strings.ContainsRune(command.Dir, '\x00') {
		return errors.New("Linux owner native working directory must be absolute")
	}
	if len(command.Args) == 0 || len(command.Args) > maxCommandVector || len(command.Env) > maxCommandVector {
		return errors.New("Linux owner native argv/env vector has an invalid length")
	}
	if command.Clone != nil {
		if err := command.Clone.Validate(); err != nil {
			return fmt.Errorf("validate B-owned standalone clone grant: %w", err)
		}
		if command.Dir != command.Clone.Intent.Target {
			return errors.New("Linux clone native working directory differs from its exact target")
		}
	}
	total := len(command.Path) + len(command.Dir)
	for _, value := range command.Args {
		if value == "" || len(value) > maxCommandString || strings.ContainsRune(value, '\x00') {
			return errors.New("Linux owner native argv contains an empty, oversized, or NUL value")
		}
		total += len(value)
	}
	seen := make(map[string]struct{}, len(command.Env))
	for _, value := range command.Env {
		if len(value) > maxCommandString || strings.ContainsRune(value, '\x00') {
			return errors.New("Linux owner native environment contains an oversized or NUL value")
		}
		key, _, found := strings.Cut(value, "=")
		if !found || key == "" || strings.ContainsRune(key, '\x00') {
			return errors.New("Linux owner native environment entry is not KEY=VALUE")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("Linux owner native environment repeats key %q", key)
		}
		seen[key] = struct{}{}
		total += len(value)
	}
	if total > maxReleaseSize-releaseHeaderSize-4096 {
		return errors.New("Linux owner native argv/env exceeds the release bound")
	}
	return nil
}

func validateRunnerPipes(stdin, stdout, stderr *os.File) error {
	files := [releaseStdioFDCount]*os.File{stdin, stdout, stderr}
	wantedAccess := [releaseStdioFDCount]int{unix.O_RDONLY, unix.O_WRONLY, unix.O_WRONLY}
	type pipeIdentity struct{ device, inode uint64 }
	var identities [releaseStdioFDCount]pipeIdentity
	var descriptors [releaseStdioFDCount]int
	for index, file := range files {
		if file == nil {
			return errors.New("native command requires Runner-owned stdin-read, stdout-write, and stderr-write pipes")
		}
		descriptors[index] = int(file.Fd())
		if descriptors[index] < 0 {
			return errors.New("native standard-I/O pipe descriptor is closed")
		}
		var stat unix.Stat_t
		if err := unix.Fstat(descriptors[index], &stat); err != nil {
			return fmt.Errorf("stat native standard-I/O pipe %d: %w", index, err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFIFO || stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
			return fmt.Errorf("native standard-I/O descriptor %d is not an A-owned pipe", index)
		}
		flags, err := unix.FcntlInt(uintptr(descriptors[index]), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != wantedAccess[index] {
			return fmt.Errorf("native standard-I/O pipe %d has the wrong access direction: flags=%#x err=%v", index, flags, err)
		}
		identities[index] = pipeIdentity{device: uint64(stat.Dev), inode: stat.Ino}
		for prior := 0; prior < index; prior++ {
			if descriptors[prior] == descriptors[index] || identities[prior] == identities[index] {
				return errors.New("native stdin/stdout/stderr must be three distinct pipe ends")
			}
		}
	}
	return nil
}

func appendWireUint32(destination []byte, value uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	return append(destination, encoded[:]...)
}

func appendWireUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}

func appendWireString(destination []byte, value string) []byte {
	destination = appendWireUint32(destination, uint32(len(value)))
	return append(destination, value...)
}

func readWireUint32(body []byte, position *int) (uint32, error) {
	if position == nil || *position < 0 || len(body)-*position < 4 {
		return 0, errors.New("length-prefixed command body ended before uint32")
	}
	value := binary.LittleEndian.Uint32(body[*position : *position+4])
	*position += 4
	return value, nil
}

func readWireUint64(body []byte, position *int) (uint64, error) {
	if position == nil || *position < 0 || len(body)-*position < 8 {
		return 0, errors.New("length-prefixed command body ended before uint64")
	}
	value := binary.LittleEndian.Uint64(body[*position : *position+8])
	*position += 8
	return value, nil
}

func readWireString(body []byte, position *int) (string, error) {
	length, err := readWireUint32(body, position)
	if err != nil {
		return "", err
	}
	if length > maxCommandString || uint64(length) > uint64(len(body)-*position) {
		return "", errors.New("length-prefixed command string is oversized or truncated")
	}
	start := *position
	*position += int(length)
	value := body[start:*position]
	if bytes.IndexByte(value, 0) >= 0 {
		return "", errors.New("length-prefixed command string contains NUL")
	}
	return string(value), nil
}

// EncodeExecAttempt is written immediately before same-PID execveat; successful
// exec closes this status descriptor because the gate marks it CLOEXEC.
func EncodeExecAttempt(device, inode uint64) []byte {
	packet := fixedFrame(frameExecAttempt, execAttemptSize)
	binary.LittleEndian.PutUint64(packet[12:20], device)
	binary.LittleEndian.PutUint64(packet[20:28], inode)
	return packet
}

// EncodeExecFailure reports a fail-closed pre-exec/execveat error.
func EncodeExecFailure(errno uint32) []byte {
	packet := fixedFrame(frameExecFailure, execFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], errno)
	return packet
}

func cloneStatusFrame(kind uint16, size int) []byte {
	packet := fixedFrame(kind, size)
	binary.LittleEndian.PutUint16(packet[8:10], cloneStatusWireVersion)
	return packet
}

func validateCloneStatusFrame(packet []byte, kind uint16, size int) error {
	if len(packet) != size || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return errors.New("Linux clone status frame version, type, or size is invalid")
	}
	return nil
}

func encodeCloneExecAttempt(kind uint16, device, inode uint64) []byte {
	packet := cloneStatusFrame(kind, cloneExecAttemptSize)
	binary.LittleEndian.PutUint64(packet[12:20], device)
	binary.LittleEndian.PutUint64(packet[20:28], inode)
	return packet
}

func decodeCloneExecAttempt(packet []byte, kind uint16) (uint64, uint64, error) {
	if err := validateCloneStatusFrame(packet, kind, cloneExecAttemptSize); err != nil {
		return 0, 0, err
	}
	device := binary.LittleEndian.Uint64(packet[12:20])
	inode := binary.LittleEndian.Uint64(packet[20:28])
	if inode == 0 {
		return 0, 0, errors.New("Linux clone exec-attempt frame lacks an image inode")
	}
	return device, inode, nil
}

// EncodeBootstrapExecAttempt is written by the fixed gate immediately before
// it execs the root-installed B clone bootstrap in the same PID.
func EncodeBootstrapExecAttempt(device, inode uint64) []byte {
	return encodeCloneExecAttempt(frameBootstrapExecAttempt, device, inode)
}

// DecodeBootstrapExecAttempt accepts the exact same-PID bootstrap image proof.
func DecodeBootstrapExecAttempt(packet []byte) (uint64, uint64, error) {
	return decodeCloneExecAttempt(packet, frameBootstrapExecAttempt)
}

// EncodeNativeExecAttempt is written by the B bootstrap immediately before it
// execs the requested native CLI in the same PID.
func EncodeNativeExecAttempt(device, inode uint64) []byte {
	return encodeCloneExecAttempt(frameNativeExecAttempt, device, inode)
}

// DecodeNativeExecAttempt accepts the exact same-PID native image proof.
func DecodeNativeExecAttempt(packet []byte) (uint64, uint64, error) {
	return decodeCloneExecAttempt(packet, frameNativeExecAttempt)
}

func encodeCloneCheckoutReceipt(kind uint16, receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	packet := cloneStatusFrame(kind, 12)
	packet = appendWireUint32(packet, uint32(receipt.Version))
	packet = appendWireString(packet, receipt.RunID)
	packet = appendWireUint64(packet, uint64(receipt.Generation))
	packet = appendWireString(packet, receipt.BindingKey)
	packet = appendWireString(packet, receipt.ResourceID)
	packet = appendWireString(packet, receipt.Target)
	packet = appendWireString(packet, receipt.Nonce)
	packet = appendWireString(packet, receipt.SeedDigest)
	packet = appendWireString(packet, receipt.BaseCommit)
	packet = appendWireUint64(packet, receipt.TargetDevice)
	packet = appendWireUint64(packet, receipt.TargetInode)
	if len(packet) > maxCloneStatusFrameSize {
		clear(packet)
		return nil, errors.New("Linux clone checkout receipt exceeds the status frame bound")
	}
	return packet, nil
}

func decodeCloneCheckoutReceipt(packet []byte, kind uint16) (authority.LinuxCloneCheckoutReceipt, error) {
	var receipt authority.LinuxCloneCheckoutReceipt
	if len(packet) < 12+4+4+1+8+4*6+16 || len(packet) > maxCloneStatusFrameSize ||
		string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return receipt, errors.New("Linux clone checkout frame header or size is invalid")
	}
	position := 12
	version, err := readWireUint32(packet, &position)
	if err != nil {
		return receipt, err
	}
	receipt.Version = int(version)
	receipt.RunID, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	generation, err := readWireUint64(packet, &position)
	if err != nil || generation > uint64(^uint64(0)>>1) {
		return authority.LinuxCloneCheckoutReceipt{}, errors.New("Linux clone checkout generation is invalid")
	}
	receipt.Generation = int64(generation)
	receipt.BindingKey, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.ResourceID, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.Target, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.Nonce, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.SeedDigest, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.BaseCommit, err = readWireString(packet, &position)
	if err != nil || len(packet)-position != 16 {
		return authority.LinuxCloneCheckoutReceipt{}, errors.New("Linux clone checkout image identity is truncated or has trailing bytes")
	}
	receipt.TargetDevice = binary.LittleEndian.Uint64(packet[position : position+8])
	receipt.TargetInode = binary.LittleEndian.Uint64(packet[position+8 : position+16])
	if err := receipt.Validate(); err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	return receipt, nil
}

// EncodeCloneCheckoutReady sends B's exact new-target inode readback to A.
func EncodeCloneCheckoutReady(receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	return encodeCloneCheckoutReceipt(frameCheckoutReady, receipt)
}

// DecodeCloneCheckoutReady validates the B bootstrap's checkout-ready frame.
func DecodeCloneCheckoutReady(packet []byte) (authority.LinuxCloneCheckoutReceipt, error) {
	return decodeCloneCheckoutReceipt(packet, frameCheckoutReady)
}

// EncodeCloneCheckoutReadyAck is sent only after A durably records the exact
// checkout receipt. The same receipt body makes the ACK nonce/target-bound.
func EncodeCloneCheckoutReadyAck(receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	return encodeCloneCheckoutReceipt(frameCheckoutReadyAck, receipt)
}

// DecodeCloneCheckoutReadyAck validates the exact A callback acknowledgement.
func DecodeCloneCheckoutReadyAck(packet []byte) (authority.LinuxCloneCheckoutReceipt, error) {
	return decodeCloneCheckoutReceipt(packet, frameCheckoutReadyAck)
}

// EncodeCloneBootstrapFailure reports one fixed bootstrap phase and errno.
func EncodeCloneBootstrapFailure(stage CloneBootstrapFailureStage, errno uint32) ([]byte, error) {
	if stage < CloneFailureRequest || stage > CloneFailureExec || errno == 0 || errno > uint32(^uint32(0)>>1) {
		return nil, errors.New("Linux clone bootstrap failure code is invalid")
	}
	packet := cloneStatusFrame(frameCloneBootstrapFailure, cloneFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], uint32(stage))
	binary.LittleEndian.PutUint32(packet[16:20], errno)
	return packet, nil
}

// DecodeCloneBootstrapFailure returns only fixed, non-sensitive failure data.
func DecodeCloneBootstrapFailure(packet []byte) (*CloneBootstrapFailureError, error) {
	if validateCloneStatusFrame(packet, frameCloneBootstrapFailure, cloneFailureSize) != nil {
		return nil, errors.New("Linux clone bootstrap failure frame is malformed")
	}
	stage := CloneBootstrapFailureStage(binary.LittleEndian.Uint32(packet[12:16]))
	errno := binary.LittleEndian.Uint32(packet[16:20])
	if stage < CloneFailureRequest || stage > CloneFailureExec || errno == 0 || errno > uint32(^uint32(0)>>1) {
		return nil, errors.New("Linux clone bootstrap failure frame code is invalid")
	}
	return &CloneBootstrapFailureError{Stage: stage, Errno: errno}, nil
}

func receiveCloneStatusPacket(ctx context.Context, fd int, deadline time.Time) ([]byte, bool, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, false, err
	}
	data := make([]byte, maxCloneStatusFrameSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, false, err
	}
	if oobn != 0 {
		fds, parseErr := parseRights(oob[:oobn])
		closeFDs(fds)
		if parseErr != nil {
			return nil, false, parseErr
		}
		return nil, false, errors.New("clone status channel carried unexpected descriptors")
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > maxCloneStatusFrameSize {
		return nil, false, errors.New("clone status frame is truncated or oversized")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return nil, false, errors.New("clone status channel carried a zero-length packet")
		}
		return nil, true, nil
	}
	if n < 12 || string(data[:8]) != launchMagic || binary.LittleEndian.Uint16(data[8:10]) != cloneStatusWireVersion {
		return nil, false, errors.New("clone status frame magic or version is invalid")
	}
	return data[:n], false, nil
}

func sendCloneStatusPacket(fd int, packet []byte) error {
	if len(packet) < 12 || len(packet) > maxCloneStatusFrameSize ||
		string(packet[:8]) != launchMagic || binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion {
		return errors.New("clone status packet is not a bounded v3 frame")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL|unix.MSG_DONTWAIT)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("clone status seqpacket short write: %d/%d", n, len(packet))
	}
	return nil
}

func readCloneBootstrapExecAttempt(ctx context.Context, fd int, deadline time.Time) (uint64, uint64, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return 0, 0, err
	}
	if eof {
		return 0, 0, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return 0, 0, decodeErr
		}
		return 0, 0, failure
	}
	return decodeCloneExecAttempt(packet, frameBootstrapExecAttempt)
}

func readCloneCheckoutReady(ctx context.Context, fd int, deadline time.Time) (authority.LinuxCloneCheckoutReceipt, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	if eof {
		return authority.LinuxCloneCheckoutReceipt{}, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return authority.LinuxCloneCheckoutReceipt{}, decodeErr
		}
		return authority.LinuxCloneCheckoutReceipt{}, failure
	}
	return DecodeCloneCheckoutReady(packet)
}

func readCloneNativeExecResult(ctx context.Context, fd int, deadline time.Time) (execStatus, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return execStatus{}, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return execStatus{}, decodeErr
		}
		return execStatus{}, failure
	}
	device, inode, err := DecodeNativeExecAttempt(packet)
	if err != nil {
		return execStatus{}, err
	}
	final, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return execStatus{execAttempted: true, device: device, inode: inode}, nil
	}
	if len(final) >= 12 && binary.LittleEndian.Uint16(final[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(final)
		if decodeErr != nil {
			return execStatus{}, decodeErr
		}
		return execStatus{}, failure
	}
	return execStatus{}, errors.New("clone status channel carried data after native exec attempt")
}

func receiveExecStatus(ctx context.Context, fd int, deadline time.Time) (execStatus, error) {
	first, err := receiveStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if len(first) == execFailureSize {
		if err := validateFixedFrame(first, frameExecFailure, execFailureSize); err != nil {
			return execStatus{}, err
		}
		return execStatus{errno: binary.LittleEndian.Uint32(first[12:16])}, nil
	}
	if err := validateFixedFrame(first, frameExecAttempt, execAttemptSize); err != nil {
		return execStatus{}, err
	}
	status := execStatus{
		execAttempted: true,
		device:        binary.LittleEndian.Uint64(first[12:20]),
		inode:         binary.LittleEndian.Uint64(first[20:28]),
	}
	if status.inode == 0 {
		return execStatus{}, errors.New("exec-attempt frame has no executable inode")
	}
	second, eof, err := receiveStatusPacketOrEOF(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return status, nil
	}
	if len(second) != execFailureSize || validateFixedFrame(second, frameExecFailure, execFailureSize) != nil {
		return execStatus{}, errors.New("exec-status channel carried an invalid post-attempt frame")
	}
	status.execAttempted = false
	status.errno = binary.LittleEndian.Uint32(second[12:16])
	return status, nil
}

type execStatus struct {
	execAttempted bool
	errno         uint32
	device        uint64
	inode         uint64
}

func receiveStatusPacket(ctx context.Context, fd int, deadline time.Time) ([]byte, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, err
	}
	data := make([]byte, execAttemptSize+1)
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, err
	}
	if n == 0 || oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, errors.New("exec-status channel closed or carried ancillary data before a frame")
	}
	return data[:n], nil
}

func receiveStatusPacketOrEOF(ctx context.Context, fd int, deadline time.Time) ([]byte, bool, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, false, err
	}
	data := make([]byte, execFailureSize+1)
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, false, err
	}
	if oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, false, errors.New("exec-status channel carried an invalid second packet")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return nil, false, errors.New("exec-status channel carried a zero-length packet instead of EOF")
		}
		return nil, true, nil
	}
	return data[:n], false, nil
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

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
