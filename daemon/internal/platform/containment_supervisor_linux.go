//go:build linux

package platform

// Linux containment is deliberately implemented as a helper-first launch.
// The daemon never starts the target itself: the helper owns the target's
// ptrace state, process-group monitor, and reaping boundary.  The daemon keeps
// only a duplicate pidfd as a volatile emergency mirror.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	linuxSupervisorOpLaunch  = "launch"
	linuxSupervisorOpHello   = "hello"
	linuxSupervisorOpBind    = "bind"
	linuxSupervisorOpCommit  = "commit"
	linuxSupervisorOpResume  = "resume"
	linuxSupervisorOpStop    = "stop"
	linuxSupervisorOpClose   = "close"
	linuxSupervisorOpAbort   = "abort"
	linuxSupervisorOpRenew   = "renew"
	linuxSupervisorOpRelease = "release"
	linuxSupervisorOpWait    = "wait"

	linuxSupervisorOwnerKind             = authority.OwnerKindLinuxHelper
	linuxSupervisorTimeout               = 5 * time.Second
	linuxSupervisorProductionWitnessEnv  = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxSupervisorDropResponseOnceEnv   = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_ONCE"
	linuxSupervisorDropResponseMarkerEnv = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_FIRED_PATH"
	linuxSupervisorScanFailureModeEnv    = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE"
	linuxSupervisorScanFailureTriggerEnv = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH"
	linuxSupervisorScanFailureFiredEnv   = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH"
	linuxSupervisorScanFailureChildren   = "children_after_initial"
)

var (
	ErrLinuxSupervisorNotCommitted                  = errors.New("Linux supervisor authority is not committed")
	ErrLinuxSupervisorOwnerLost                     = errors.New("Linux supervisor owner was lost")
	ErrLinuxSupervisorHelperExited                  = errors.New("Linux supervisor helper exited")
	ErrLinuxSupervisorStopUnproven                  = errors.New("Linux supervisor stop is unproven")
	ErrLinuxSupervisorReleased                      = errors.New("Linux supervisor containment was released")
	ErrLinuxSupervisorLeaseExpired                  = errors.New("Linux supervisor lease expired")
	ErrLinuxSupervisorLeaseUnarmed                  = errors.New("Linux supervisor lease is not armed")
	errLinuxSupervisorInjectedDescendantScanFailure = errors.New("injected Linux supervisor descendant scan failure")

	linuxSupervisorLstat                   = os.Lstat
	linuxSupervisorProcessIdentity         = ProcessIdentity
	linuxSupervisorProveProcessGroupAbsent = ProvePersistedProcessGroupAbsent
)

// DurableSupervisorHandoffAvailable reports whether the current executable
// can dispatch the daemon's hidden supervisor mode. Go test binaries have a
// generated test main and must retain the local containment path instead.
func DurableSupervisorHandoffAvailable() bool {
	if flag.CommandLine.Lookup("test.v") == nil {
		return true
	}
	name := strings.ToLower(os.Args[0])
	return !strings.HasSuffix(name, ".test")
}

// SupervisorHandoffAbortVerifier is supplied by the caller that still owns
// the current daemon/store lifetime. It must return nil only after the old
// writer is quiesced.
type SupervisorHandoffAbortVerifier func() error

// LinuxSupervisorStartSpec is consumed by execution's Linux launcher.  The
// target command is serialized over a private inherited pipe; it never enters
// helper argv or environment.
type LinuxSupervisorStartSpec struct {
	Command       *exec.Cmd
	HelperCommand *exec.Cmd
	HelperPath    string
	HelperArgs    []string
	OwnerContext  string
	Prepare       func(authority.SupervisorHandoff) error
	Bind          func(authority.SupervisorHandoff, int, string) error
}

type linuxSupervisorLaunch struct {
	Version      int      `json:"version"`
	Operation    string   `json:"operation"`
	Program      string   `json:"program"`
	Args         []string `json:"args,omitempty"`
	Env          []string `json:"env,omitempty"`
	Dir          string   `json:"dir,omitempty"`
	OwnerContext string   `json:"owner_context"`
	LaunchToken  string   `json:"launch_token"`
	PipeToken    string   `json:"pipe_token"`
	JobID        string   `json:"job_id"`
	SecretDigest string   `json:"secret_digest"`
	Endpoint     string   `json:"endpoint"`
	BootstrapFD  int      `json:"bootstrap_fd"`
	OwnerFD      int      `json:"owner_fd"`
	ControlFD    int      `json:"control_fd"`
	ResponseFD   int      `json:"response_fd"`
	StatusFD     int      `json:"status_fd"`
	MetadataFD   int      `json:"metadata_fd"`
	StdinFD      int      `json:"stdin_fd"`
	StdoutFD     int      `json:"stdout_fd"`
	StderrFD     int      `json:"stderr_fd"`
}

type linuxSupervisorHello struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	LaunchToken        string `json:"launch_token"`
	OwnerKind          string `json:"owner_kind"`
	OwnerContext       string `json:"owner_context"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	TargetPGRP         int64  `json:"target_pgrp"`
	TargetSession      int64  `json:"target_session"`
	TargetStartTime    uint64 `json:"target_start_time"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
}

// linuxSupervisorLeaseState owns one helper-local lease timer. The timer
// callback latches expiry and publishes a wake-up; stop/proof remains
// serialized by the helper loop. Generation fencing makes a stale callback
// harmless after a newer renewal replaces the deadline.
type linuxSupervisorLeaseState struct {
	mu sync.Mutex

	timer      *time.Timer
	generation uint64
	sequence   uint64
	deadline   time.Duration
	deadlineAt time.Time
	stopped    bool
	ownerLost  bool
	armed      bool
	expiredGen uint64
	wake       chan struct{}
}

func newLinuxSupervisorLeaseState() *linuxSupervisorLeaseState {
	return &linuxSupervisorLeaseState{wake: make(chan struct{}, 1)}
}

// renew accepts a strictly newer sequence, or an exact replay of the current
// sequence/deadline. The bool reports an idempotent replay.
func (state *linuxSupervisorLeaseState) renew(deadline time.Duration, sequence uint64) (bool, error) {
	if state == nil || deadline <= 0 || sequence == 0 {
		return false, errors.New("Linux supervisor lease deadline and sequence must be positive")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	now := time.Now()
	state.latchExpiredLocked(now)
	if state.stopped {
		return false, ErrLinuxSupervisorLeaseExpired
	}
	if sequence == state.sequence {
		if deadline == state.deadline {
			return true, nil
		}
		return false, errLinuxSupervisorSequence
	}
	if sequence < state.sequence {
		return false, errLinuxSupervisorSequence
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.sequence = sequence
	state.deadline = deadline
	state.deadlineAt = now.Add(deadline)
	state.armed = true
	state.generation++
	generation := state.generation
	state.expiredGen = 0
	state.timer = time.AfterFunc(deadline, func() {
		state.mu.Lock()
		if !state.stopped && state.generation == generation {
			state.expiredGen = generation
			// Latch expiry before publishing the wake-up. A renewal that
			// races the timer callback must fail closed even if the helper loop
			// has not consumed the callback notification yet.
			state.stopped = true
			state.deadlineAt = time.Time{}
			state.timer = nil
		}
		state.mu.Unlock()
		select {
		case state.wake <- struct{}{}:
		default:
		}
	})
	return false, nil
}

// takeExpiry consumes only the current generation's timer event. It latches
// the stopped state before the external stop/proof attempt begins, so a late
// renewal can never re-arm a lease after expiry.
func (state *linuxSupervisorLeaseState) takeExpiry() bool {
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.latchExpiredLocked(time.Now())
	if state.expiredGen == 0 || state.expiredGen != state.generation {
		return false
	}
	state.stopped = true
	state.expiredGen = 0
	state.deadlineAt = time.Time{}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	return true
}

func (state *linuxSupervisorLeaseState) stop() {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.stopped = true
	state.generation++
	state.expiredGen = 0
	state.deadlineAt = time.Time{}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()
}

func (state *linuxSupervisorLeaseState) wakeup() <-chan struct{} {
	if state == nil {
		return nil
	}
	return state.wake
}

// latchExpiredLocked closes a lease at its monotonic deadline even when the
// timer callback has not run yet. The wake-up lets the helper loop perform the
// physical stop/proof after a request observes the late timer.
func (state *linuxSupervisorLeaseState) latchExpiredLocked(now time.Time) bool {
	if state == nil || state.stopped || !state.armed || state.deadlineAt.IsZero() || now.Before(state.deadlineAt) {
		return false
	}
	state.stopped = true
	state.expiredGen = state.generation
	state.deadlineAt = time.Time{}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	select {
	case state.wake <- struct{}{}:
	default:
	}
	return true
}

// latchOwnerLost permanently closes the lease before the watchdog publishes
// its wake-up. Resume/detach and owner loss therefore have one linearization
// point: whichever acquires this lock first wins, and the other path fails
// closed without touching ptrace state.
func (state *linuxSupervisorLeaseState) latchOwnerLost() {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.ownerLost {
		return
	}
	state.ownerLost = true
	state.stopped = true
	state.generation++
	state.expiredGen = 0
	state.deadlineAt = time.Time{}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
}

type linuxSupervisor struct {
	mu sync.Mutex

	authority authority.Supervisor
	handoff   authority.SupervisorHandoff
	anchor    LinuxProcessGroupAnchor

	helper         *os.Process
	helperIdentity string
	helperDone     chan struct{}
	helperWaitErr  error

	ownerWrite     *os.File
	controlWrite   *os.File
	responseRead   *os.File
	responseReader *bufio.Reader
	statusRead     *os.File
	statusReader   *bufio.Reader
	pidfd          int
	// mirror observes the target tree independently of the helper. It owns the
	// duplicated pidfd and may take over only after the exact helper has exited.
	mirror *processGroup
	// finalProofPending means the mirror closed its pidfd and only the
	// identity-bound read-only group proof remains retryable.
	finalProofPending bool

	requestMu sync.Mutex
	statusMu  sync.Mutex

	committed             bool
	resumed               bool
	stopped               bool
	released              bool
	closed                bool
	receipt               *authority.StopReceipt
	closeErr              error
	responseDropTriggered bool
	responseTransportLost bool
	responseLossPending   bool
	waitCode              int
	waitErr               error
	waitDone              chan struct{}
	waitOnce              sync.Once

	containmentUnprovenCallback func() error
	prepareUnknown              bool
	bindUnknown                 bool
}

// StartLinuxSupervisor starts the helper first.  The helper then launches the
// ptrace-stopped target, sends its exact identity and a duplicate pidfd back,
// and waits for prepare/bind/commit before allowing user code to run.
func StartLinuxSupervisor(spec LinuxSupervisorStartSpec) (*linuxSupervisor, error) {
	if spec.Command == nil {
		return nil, errors.New("Linux supervisor target command is required")
	}
	if strings.TrimSpace(spec.Command.Path) == "" {
		return nil, errors.New("Linux supervisor target path is required")
	}
	if strings.TrimSpace(spec.OwnerContext) == "" {
		return nil, errors.New("Linux supervisor owner context is required")
	}
	if len(spec.Command.ExtraFiles) != 0 {
		return nil, errors.New("Linux supervisor target ExtraFiles are unsupported")
	}
	stdin, stdinOK := spec.Command.Stdin.(*os.File)
	stdout, stdoutOK := spec.Command.Stdout.(*os.File)
	stderr, stderrOK := spec.Command.Stderr.(*os.File)
	if !stdinOK || !stdoutOK || !stderrOK {
		return nil, errors.New("Linux supervisor target stdio must be *os.File")
	}

	launchToken, err := linuxSupervisorRandomHex(authority.TokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate Linux supervisor launch token: %w", err)
	}
	pipeToken, err := linuxSupervisorRandomHex(authority.TokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate Linux supervisor pipe token: %w", err)
	}
	jobID, err := linuxSupervisorRandomHex(authority.TokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate Linux supervisor job ID: %w", err)
	}
	secret, err := linuxSupervisorRandomHex(authority.SecretBytes)
	if err != nil {
		return nil, fmt.Errorf("generate Linux supervisor secret: %w", err)
	}
	secretDigest := sha256.Sum256([]byte(secret))
	endpoint := filepath.Join(os.TempDir(), "symmetry-linux-supervisor-"+jobID+".sock")
	_ = os.Remove(endpoint)
	ownerContext := spec.OwnerContext + "|endpoint=" + endpoint
	if len(ownerContext) > 4096 {
		return nil, errors.New("Linux supervisor owner context is too long")
	}

	bootstrapRead, bootstrapWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create Linux supervisor bootstrap pipe: %w", err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite)
		return nil, fmt.Errorf("create Linux supervisor owner pipe: %w", err)
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite)
		return nil, fmt.Errorf("create Linux supervisor control pipe: %w", err)
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite)
		return nil, fmt.Errorf("create Linux supervisor response pipe: %w", err)
	}
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite)
		return nil, fmt.Errorf("create Linux supervisor status pipe: %w", err)
	}
	metadataPair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite, statusRead, statusWrite)
		return nil, fmt.Errorf("create Linux supervisor metadata socket: %w", err)
	}
	metadataDaemon := os.NewFile(uintptr(metadataPair[0]), "linux-supervisor-metadata-daemon")
	metadataHelper := os.NewFile(uintptr(metadataPair[1]), "linux-supervisor-metadata-helper")
	if metadataDaemon == nil || metadataHelper == nil {
		_ = unix.Close(metadataPair[0])
		_ = unix.Close(metadataPair[1])
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite, statusRead, statusWrite)
		return nil, errors.New("create Linux supervisor metadata file")
	}

	help, err := linuxSupervisorHelperCommandV2(spec, bootstrapRead, ownerRead, controlRead, responseWrite, statusWrite, metadataHelper, stdin, stdout, stderr)
	if err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite, statusRead, statusWrite, metadataDaemon, metadataHelper)
		return nil, err
	}
	if err := help.Start(); err != nil {
		closeLinuxSupervisorFilesV2(bootstrapRead, bootstrapWrite, ownerRead, ownerWrite, controlRead, controlWrite, responseRead, responseWrite, statusRead, statusWrite, metadataDaemon, metadataHelper)
		return nil, fmt.Errorf("start Linux supervisor helper: %w", err)
	}
	helperIdentity, err := ProcessIdentity(help.Process.Pid)
	if err != nil {
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(bootstrapWrite, ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, fmt.Errorf("capture Linux supervisor helper identity: %w", err)
	}
	ownerContext = spec.OwnerContext + "|helper=" + helperIdentity + "|endpoint=" + endpoint
	if len(ownerContext) > 4096 {
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(bootstrapWrite, ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, errors.New("Linux supervisor owner context is too long")
	}
	closeLinuxSupervisorFilesV2(bootstrapRead, ownerRead, controlRead, responseWrite, statusWrite, metadataHelper)

	launch := linuxSupervisorLaunch{
		Version:      linuxSupervisorProtocolVersion,
		Operation:    linuxSupervisorOpLaunch,
		Program:      spec.Command.Path,
		Args:         append([]string(nil), spec.Command.Args[1:]...),
		Env:          append([]string(nil), spec.Command.Env...),
		Dir:          spec.Command.Dir,
		OwnerContext: ownerContext,
		LaunchToken:  launchToken,
		PipeToken:    pipeToken,
		JobID:        jobID,
		SecretDigest: hex.EncodeToString(secretDigest[:]),
		Endpoint:     endpoint,
		BootstrapFD:  3,
		OwnerFD:      4,
		ControlFD:    5,
		ResponseFD:   6,
		StatusFD:     7,
		MetadataFD:   8,
		StdinFD:      9,
		StdoutFD:     10,
		StderrFD:     11,
	}
	if err := writeLinuxSupervisorFrame(bootstrapWrite, launch); err != nil {
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(bootstrapWrite, ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, fmt.Errorf("send Linux supervisor launch: %w", err)
	}
	_ = bootstrapWrite.Close()

	pidfd, hello, err := receiveLinuxSupervisorHello(metadataDaemon, responseRead, helperIdentity)
	if err != nil {
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, err
	}
	anchor := LinuxProcessGroupAnchor{PID: hello.TargetPID, PGRP: hello.TargetPGRP, Session: hello.TargetSession, StartTime: hello.TargetStartTime}
	if hello.LaunchToken != launchToken || hello.PipeToken != pipeToken || hello.JobID != jobID || hello.OwnerContext != ownerContext || hello.SupervisorPID != help.Process.Pid || hello.SupervisorIdentity != helperIdentity || anchor.PID != hello.TargetPID || anchor.PGRP != int64(hello.TargetPID) || anchor.Session == int64(hello.TargetPID) || anchor.StartTime == 0 {
		_ = unix.Close(pidfd)
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, errors.New("Linux supervisor hello identity or anchor mismatch")
	}

	value := authority.Supervisor{Version: authority.SupervisorVersion, Secret: secret, LaunchToken: launchToken, OwnerKind: linuxSupervisorOwnerKind, OwnerContext: ownerContext, TargetPID: hello.TargetPID, TargetIdentity: hello.TargetIdentity, PipeToken: pipeToken, JobID: jobID, SupervisorPID: help.Process.Pid, SupervisorIdentity: helperIdentity}
	if err := value.Validate(); err != nil {
		_ = unix.Close(pidfd)
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, fmt.Errorf("validate Linux supervisor authority: %w", err)
	}
	handoff := authority.SupervisorHandoff{Version: authority.SupervisorHandoffVersion, LaunchToken: launchToken, Secret: secret, OwnerKind: linuxSupervisorOwnerKind, OwnerContext: ownerContext, TargetPID: hello.TargetPID, TargetIdentity: hello.TargetIdentity, PipeToken: pipeToken, JobID: jobID, SupervisorPID: help.Process.Pid, SupervisorIdentity: helperIdentity}

	mirror := &processGroup{pid: hello.TargetPID, fd: pidfd, anchor: anchor.private(), anchorCaptured: true}
	mirror.startDescendantMonitor()
	if err := mirror.waitForInitialDescendantScan(time.Now().Add(containmentCloseDeadline)); err != nil {
		_ = mirror.Terminate(true)
		_ = mirror.Close()
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, fmt.Errorf("observe Linux supervisor mirror containment: %w", err)
	}
	supervisor := &linuxSupervisor{authority: value, handoff: handoff, anchor: anchor, helper: help.Process, helperIdentity: helperIdentity, helperDone: make(chan struct{}), ownerWrite: ownerWrite, controlWrite: controlWrite, responseRead: responseRead, responseReader: bufio.NewReader(responseRead), statusRead: statusRead, statusReader: bufio.NewReader(statusRead), pidfd: pidfd, mirror: mirror}
	go supervisor.waitHelper()

	preparedHandoff := handoff.Clone()
	preparedHandoff.SupervisorPID = 0
	preparedHandoff.SupervisorIdentity = ""
	if spec.Prepare != nil {
		if err := callLinuxSupervisorPrepare(spec.Prepare, preparedHandoff); err != nil {
			supervisor.prepareUnknown = true
			return supervisor, fmt.Errorf("prepare Linux supervisor handoff: %w", err)
		}
	}
	if spec.Bind != nil {
		if err := callLinuxSupervisorBind(spec.Bind, preparedHandoff, handoff.SupervisorPID, handoff.SupervisorIdentity); err != nil {
			supervisor.bindUnknown = true
			return supervisor, fmt.Errorf("bind Linux supervisor handoff: %w", err)
		}
	}
	if _, err := supervisor.request(linuxSupervisorOpBind, 0, 0); err != nil {
		return supervisor, fmt.Errorf("bind Linux supervisor helper: %w", err)
	}
	return supervisor, nil
}

func linuxSupervisorHelperCommandV2(spec LinuxSupervisorStartSpec, bootstrapRead, ownerRead, controlRead, responseWrite, statusWrite, metadataHelper, stdin, stdout, stderr *os.File) (*exec.Cmd, error) {
	var command *exec.Cmd
	switch {
	case spec.HelperCommand != nil:
		copy := *spec.HelperCommand
		if copy.Process != nil {
			return nil, errors.New("Linux supervisor helper command has already started")
		}
		command = &copy
	case strings.TrimSpace(spec.HelperPath) != "":
		command = exec.Command(spec.HelperPath, spec.HelperArgs...)
	default:
		command = exec.Command(os.Args[0], "-containment-supervisor")
	}
	base := 3 + len(command.ExtraFiles)
	files := []*os.File{bootstrapRead, ownerRead, controlRead, responseWrite, statusWrite, metadataHelper, stdin, stdout, stderr}
	for _, file := range files {
		if file == nil {
			return nil, errors.New("Linux supervisor inherited file is nil")
		}
	}
	command.Args = append(command.Args,
		"--bootstrap-fd", strconv.Itoa(base),
		"--owner-fd", strconv.Itoa(base+1),
		"--control-fd", strconv.Itoa(base+2),
		"--response-fd", strconv.Itoa(base+3),
		"--status-fd", strconv.Itoa(base+4),
		"--metadata-fd", strconv.Itoa(base+5),
		"--stdin-fd", strconv.Itoa(base+6),
		"--stdout-fd", strconv.Itoa(base+7),
		"--stderr-fd", strconv.Itoa(base+8),
	)
	command.ExtraFiles = append(command.ExtraFiles, files...)
	return command, nil
}

func closeLinuxSupervisorFilesV2(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}

func linuxSupervisorRandomHex(size int) (string, error) {
	value := make([]byte, size)
	if size <= 0 {
		return "", errors.New("Linux supervisor random size must be positive")
	}
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func callLinuxSupervisorPrepare(callback func(authority.SupervisorHandoff) error, value authority.SupervisorHandoff) error {
	first := callback(value.Clone())
	if first == nil {
		return nil
	}
	second := callback(value.Clone())
	if second == nil {
		return nil
	}
	return errors.Join(first, second)
}

func callLinuxSupervisorBind(callback func(authority.SupervisorHandoff, int, string) error, value authority.SupervisorHandoff, pid int, identity string) error {
	first := callback(value.Clone(), pid, identity)
	if first == nil {
		return nil
	}
	second := callback(value.Clone(), pid, identity)
	if second == nil {
		return nil
	}
	return errors.Join(first, second)
}

func receiveLinuxSupervisorHello(metadata *os.File, response *os.File, helperIdentity string) (int, linuxSupervisorHello, error) {
	fd, err := receiveLinuxSupervisorFD(metadata)
	if err != nil {
		return -1, linuxSupervisorHello{}, fmt.Errorf("receive Linux supervisor pidfd: %w", err)
	}
	var hello linuxSupervisorHello
	if err := readLinuxSupervisorFrame(bufio.NewReader(response), &hello); err != nil {
		_ = unix.Close(fd)
		return -1, linuxSupervisorHello{}, fmt.Errorf("read Linux supervisor hello: %w", err)
	}
	if hello.Version != linuxSupervisorProtocolVersion || hello.Operation != linuxSupervisorOpHello || hello.SupervisorIdentity != helperIdentity {
		_ = unix.Close(fd)
		return -1, linuxSupervisorHello{}, errors.New("Linux supervisor hello protocol or helper identity mismatch")
	}
	return fd, hello, nil
}

func sendLinuxSupervisorFD(file *os.File, fd int) error {
	if file == nil || fd < 0 {
		return errors.New("Linux supervisor fd transport is invalid")
	}
	oob := unix.UnixRights(fd)
	return unix.Sendmsg(int(file.Fd()), []byte{1}, oob, nil, 0)
}

func receiveLinuxSupervisorFD(file *os.File) (int, error) {
	if file == nil {
		return -1, errors.New("Linux supervisor fd transport is nil")
	}
	data := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4))
	_, oobn, _, _, err := unix.Recvmsg(int(file.Fd()), data, oob, 0)
	if err != nil {
		return -1, err
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return -1, err
	}
	for _, message := range messages {
		fds, err := unix.ParseUnixRights(&message)
		if err != nil {
			return -1, err
		}
		if len(fds) == 1 {
			return fds[0], nil
		}
	}
	return -1, errors.New("Linux supervisor pidfd was not transferred")
}

func (supervisor *linuxSupervisor) waitHelper() {
	_, err := supervisor.helper.Wait()
	supervisor.mu.Lock()
	supervisor.helperWaitErr = err
	supervisor.mu.Unlock()
	close(supervisor.helperDone)
}

func (supervisor *linuxSupervisor) PID() int {
	if supervisor == nil {
		return 0
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.authority.TargetPID
}

func (supervisor *linuxSupervisor) Identity() string {
	if supervisor == nil {
		return ""
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.authority.TargetIdentity
}

func (supervisor *linuxSupervisor) ProcessGroupAnchor() LinuxProcessGroupAnchor {
	if supervisor == nil {
		return LinuxProcessGroupAnchor{}
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.anchor
}

func (supervisor *linuxSupervisor) HelperPID() int {
	if supervisor == nil || supervisor.helper == nil {
		return 0
	}
	return supervisor.helper.Pid
}

func (supervisor *linuxSupervisor) HelperIdentity() string {
	if supervisor == nil {
		return ""
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.helperIdentity
}

func (supervisor *linuxSupervisor) Handoff() authority.SupervisorHandoff {
	if supervisor == nil {
		return authority.SupervisorHandoff{}
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	value := supervisor.handoff.Clone()
	if supervisor.receipt != nil {
		receipt := *supervisor.receipt
		value.StopReceipt = &receipt
	}
	return value
}

func (supervisor *linuxSupervisor) PrepareUnknown() bool {
	if supervisor == nil {
		return false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.prepareUnknown
}

func (supervisor *linuxSupervisor) BindUnknown() bool {
	if supervisor == nil {
		return false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return supervisor.bindUnknown
}

func (supervisor *linuxSupervisor) ContainmentAuthority() *authority.Supervisor {
	if supervisor == nil {
		return nil
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	value := supervisor.authority.Clone()
	if supervisor.receipt != nil {
		receipt := *supervisor.receipt
		value.StopReceipt = &receipt
	}
	return &value
}

func (supervisor *linuxSupervisor) ContainmentAuthorityAvailable() bool {
	return supervisor != nil && supervisor.HelperPID() > 0 && supervisor.HelperIdentity() != ""
}

func (supervisor *linuxSupervisor) ContainmentStopReceipt() (authority.StopReceipt, bool) {
	if supervisor == nil {
		return authority.StopReceipt{}, false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.receipt == nil {
		return authority.StopReceipt{}, false
	}
	return *supervisor.receipt, true
}

func (supervisor *linuxSupervisor) ContainmentCloseRetryable() bool {
	if supervisor == nil {
		return false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return !supervisor.released && supervisor.receipt == nil && (supervisor.pidfd >= 0 || supervisor.finalProofPending)
}

func (supervisor *linuxSupervisor) SetContainmentUnprovenCallback(callback func() error) error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	supervisor.containmentUnprovenCallback = callback
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror != nil {
		return mirror.SetContainmentUnprovenCallback(callback)
	}
	return nil
}

func (supervisor *linuxSupervisor) Commit() error {
	if supervisor == nil {
		return ErrLinuxSupervisorHelperExited
	}
	supervisor.mu.Lock()
	if supervisor.committed {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.mu.Unlock()
	response, err := supervisor.request(linuxSupervisorOpCommit, 0, 0)
	if err != nil {
		return err
	}
	if response.Status != "committed" {
		return fmt.Errorf("Linux supervisor commit returned %q", response.Status)
	}
	supervisor.mu.Lock()
	supervisor.committed = true
	supervisor.mu.Unlock()
	return nil
}

func (supervisor *linuxSupervisor) Resume() error {
	if supervisor == nil {
		return ErrLinuxSupervisorHelperExited
	}
	supervisor.mu.Lock()
	if supervisor.resumed {
		supervisor.mu.Unlock()
		return errors.New("Linux supervisor target was already resumed")
	}
	if !supervisor.committed {
		supervisor.mu.Unlock()
		return ErrLinuxSupervisorNotCommitted
	}
	supervisor.mu.Unlock()
	response, err := supervisor.request(linuxSupervisorOpResume, 0, 0)
	if err != nil {
		return err
	}
	if response.Status != "resumed" {
		return fmt.Errorf("Linux supervisor resume returned %q", response.Status)
	}
	supervisor.mu.Lock()
	supervisor.resumed = true
	supervisor.mu.Unlock()
	return nil
}

func (supervisor *linuxSupervisor) Wait() (int, error) {
	if supervisor == nil {
		return -1, ErrLinuxSupervisorHelperExited
	}
	supervisor.statusMu.Lock()
	defer supervisor.statusMu.Unlock()
	if supervisor.waitDone != nil {
		<-supervisor.waitDone
		return supervisor.waitCode, supervisor.waitErr
	}
	supervisor.waitDone = make(chan struct{})
	defer close(supervisor.waitDone)
	var status linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(supervisor.statusReader, &status); err != nil {
		supervisor.waitErr = fmt.Errorf("read Linux supervisor target status: %w", err)
		return -1, supervisor.waitErr
	}
	if status.Version != linuxSupervisorProtocolVersion || status.Operation != linuxSupervisorOpWait || status.TargetPID != supervisor.PID() || status.TargetIdentity != supervisor.Identity() {
		supervisor.waitErr = errors.New("Linux supervisor target status identity mismatch")
		return -1, supervisor.waitErr
	}
	supervisor.waitCode = status.ExitCode
	if status.ExitError != "" {
		supervisor.waitErr = errors.New(status.ExitError)
	}
	return supervisor.waitCode, supervisor.waitErr
}

func (supervisor *linuxSupervisor) Terminate(force bool) error {
	_ = force
	return supervisor.stop(linuxSupervisorOpStop)
}

func (supervisor *linuxSupervisor) Close() error {
	if supervisor == nil {
		return nil
	}
	supervisor.mu.Lock()
	if supervisor.closeErr != nil {
		err := supervisor.closeErr
		supervisor.mu.Unlock()
		return err
	}
	if supervisor.receipt != nil {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.mu.Unlock()
	return supervisor.stop(linuxSupervisorOpClose)
}

func (supervisor *linuxSupervisor) stop(operation string) error {
	if supervisor == nil {
		return ErrLinuxSupervisorHelperExited
	}
	supervisor.mu.Lock()
	if supervisor.receipt != nil {
		supervisor.mu.Unlock()
		return nil
	}
	responseLossPending := supervisor.responseLossPending
	helperDead := channelClosedV2(supervisor.helperDone)
	supervisor.mu.Unlock()
	if helperDead {
		return supervisor.stopWithMirror()
	}
	if responseLossPending {
		return supervisor.recoverResponseLossStop()
	}
	response, err := supervisor.request(operation, 0, 0)
	if err != nil {
		if errors.Is(err, ErrLinuxSupervisorResponseLost) {
			return supervisor.recoverResponseLossStop()
		}
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	if response.Status != "stopped" || response.ActiveProcesses != 0 {
		return fmt.Errorf("%w: helper returned %q with %d active processes", ErrLinuxSupervisorStopUnproven, response.Status, response.ActiveProcesses)
	}
	if err := supervisor.verifyMirrorObservation(); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: daemon mirror proof: %v", ErrLinuxSupervisorStopUnproven, err)
	}
	receipt := supervisor.receiptFromResponse(response)
	supervisor.mu.Lock()
	supervisor.receipt = &receipt
	supervisor.authority.StopReceipt = &receipt
	supervisor.handoff.StopReceipt = &receipt
	supervisor.stopped = true
	supervisor.pidfd = -1
	supervisor.mu.Unlock()
	return nil
}

// recoverResponseLossStop replays a committed stop through the authenticated
// recovery endpoint. The normal response pipe may have been closed after the
// helper applied the request, so later Terminate/Close retries must retain this
// typed recovery boundary instead of degrading to helper-exited.
func (supervisor *linuxSupervisor) recoverResponseLossStop() error {
	if supervisor == nil {
		return ErrLinuxSupervisorHelperExited
	}
	value := supervisor.ContainmentAuthority()
	if value == nil {
		return fmt.Errorf("%w: Linux supervisor authority is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	// Keep response-loss sticky even after recovery succeeds. The daemon can
	// now have a receipt, but the normal response pipe is permanently
	// untrustworthy and must never be used by a later lifecycle operation.
	supervisor.mu.Lock()
	supervisor.responseLossPending = true
	supervisor.mu.Unlock()
	deadline := time.Now().Add(containmentCloseDeadline)
	var lastErr error
	for {
		if channelClosedV2(supervisor.helperDone) {
			return supervisor.stopWithMirror()
		}
		receipt, err := StopPersistedLinuxSupervisor(*value)
		retry := false
		if err == nil {
			if !receipt.ValidFor(*value) {
				lastErr = errors.New("response-loss recovery returned invalid stop receipt")
			} else if mirrorErr := supervisor.verifyMirrorObservation(); mirrorErr != nil {
				if supervisor.containmentUnprovenCallback != nil {
					_ = supervisor.containmentUnprovenCallback()
				}
				lastErr = fmt.Errorf("response-loss mirror proof: %w", mirrorErr)
			} else {
				supervisor.mu.Lock()
				supervisor.receipt = &receipt
				supervisor.authority.StopReceipt = &receipt
				supervisor.handoff.StopReceipt = &receipt
				supervisor.stopped = true
				supervisor.pidfd = -1
				supervisor.mu.Unlock()
				return nil
			}
		} else {
			lastErr = err
			retry = linuxSupervisorRecoveryRetryable(err)
		}
		if !retry {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		timer := time.NewTimer(remaining)
		<-timer.C
	}
	if lastErr == nil {
		lastErr = errors.New("response-loss recovery deadline expired")
	}
	return errors.Join(ErrLinuxSupervisorStopUnproven, ErrLinuxSupervisorResponseLost, lastErr)
}

func (supervisor *linuxSupervisor) stopWithMirror() error {
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	pid := supervisor.authority.TargetPID
	identity := supervisor.authority.TargetIdentity
	supervisor.mu.Unlock()
	if mirror == nil {
		return ErrLinuxSupervisorStopUnproven
	}
	deadline := containmentDeadline(context.Background())
	if err := mirror.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: mirror signal: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	mirror.mutex.Lock()
	mirror.skipNextCloseSignal = true
	mirror.mutex.Unlock()
	if mirror.anchorCaptured && mirror.anchor.pgrp > 0 {
		if err := reapLinuxProcessGroupChildren(mirror.anchor, deadline); err != nil {
			if supervisor.containmentUnprovenCallback != nil {
				_ = supervisor.containmentUnprovenCallback()
			}
			return fmt.Errorf("%w: mirror reap: %w", ErrLinuxSupervisorStopUnproven, err)
		}
	}
	if err := mirror.closeUntil(deadline); err != nil {
		mirror.mutex.Lock()
		mirrorFD := mirror.fd
		mirror.mutex.Unlock()
		supervisor.mu.Lock()
		supervisor.pidfd = mirrorFD
		supervisor.mu.Unlock()
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: mirror stop and descendant proof: %v", ErrLinuxSupervisorStopUnproven, err)
	}
	supervisor.mu.Lock()
	supervisor.pidfd = -1
	supervisor.finalProofPending = true
	supervisor.mu.Unlock()
	if err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), pid, identity, deadline); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: mirror proof: %v", ErrLinuxSupervisorStopUnproven, err)
	}
	receipt := supervisor.receiptFromResponse(linuxSupervisorResponse{Status: "stopped", ActiveProcesses: 0})
	supervisor.mu.Lock()
	supervisor.receipt = &receipt
	supervisor.authority.StopReceipt = &receipt
	supervisor.handoff.StopReceipt = &receipt
	supervisor.stopped = true
	supervisor.finalProofPending = false
	supervisor.mu.Unlock()
	return nil
}

// proveLinuxSupervisorGroupAbsentWithRetry tolerates only the short interval
// in which a killed process group is still visible to getpriority. Identity,
// namespace, permission, and all other proof failures remain fail-closed.
func proveLinuxSupervisorGroupAbsentWithRetry(ctx context.Context, pid int, identity string, deadline time.Time) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil proof context", ErrPersistedProcessGroupUnproven)
	}
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	proofCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		if budgetErr := checkProcessGroupProbeBudget(proofCtx, deadline); budgetErr != nil {
			return fmt.Errorf("%w: %w", ErrPersistedProcessGroupUnproven, budgetErr)
		}
		err := linuxSupervisorProveProcessGroupAbsent(proofCtx, pid, identity)
		if budgetErr := checkProcessGroupProbeBudget(proofCtx, deadline); budgetErr != nil {
			if err == nil {
				return fmt.Errorf("%w: proof completed after deadline: %w", ErrPersistedProcessGroupUnproven, budgetErr)
			}
			return errors.Join(err, budgetErr)
		}
		if err == nil {
			return nil
		}
		if !errors.Is(err, errPersistedProcessGroupPresent) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		timer := time.NewTimer(remaining)
		select {
		case <-proofCtx.Done():
			timer.Stop()
			return errors.Join(err, proofCtx.Err())
		case <-timer.C:
		}
	}
}

func (supervisor *linuxSupervisor) receiptFromResponse(response linuxSupervisorResponse) authority.StopReceipt {
	return authority.StopReceipt{Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: supervisor.authority.OwnerKind, OwnerContext: supervisor.authority.OwnerContext, TargetPID: supervisor.authority.TargetPID, TargetIdentity: supervisor.authority.TargetIdentity, PipeToken: supervisor.authority.PipeToken, JobID: supervisor.authority.JobID, SupervisorPID: supervisor.authority.SupervisorPID, SupervisorIdentity: supervisor.authority.SupervisorIdentity, ActiveProcesses: response.ActiveProcesses}
}

func (supervisor *linuxSupervisor) ReleaseContainment() error {
	if supervisor == nil {
		return nil
	}
	supervisor.mu.Lock()
	if supervisor.released {
		supervisor.mu.Unlock()
		return nil
	}
	if supervisor.receipt == nil {
		supervisor.mu.Unlock()
		return ErrLinuxSupervisorStopUnproven
	}
	helperDead := channelClosedV2(supervisor.helperDone)
	mirror := supervisor.mirror
	responseTransportLost := supervisor.responseTransportLost || supervisor.responseLossPending
	supervisor.mu.Unlock()
	if responseTransportLost {
		return supervisor.releaseThroughRecovery(mirror)
	}
	if helperDead {
		if err := supervisor.releaseAfterHelperDeath(mirror); err != nil {
			return err
		}
		return nil
	}
	response, err := supervisor.request(linuxSupervisorOpRelease, 0, 0)
	if err != nil {
		if errors.Is(err, ErrLinuxSupervisorResponseLost) {
			return supervisor.releaseThroughRecovery(mirror)
		}
		if channelClosedV2(supervisor.helperDone) {
			return supervisor.releaseAfterHelperDeath(mirror)
		}
		return fmt.Errorf("release Linux supervisor helper: %w", err)
	}
	if response.Status != "released" {
		return fmt.Errorf("release Linux supervisor helper returned %q", response.Status)
	}
	return supervisor.finalizeReleasedContainment(mirror)
}

func (supervisor *linuxSupervisor) finalizeReleasedContainment(mirror *processGroup) error {
	if supervisor == nil {
		return nil
	}
	supervisor.mu.Lock()
	fd := supervisor.pidfd
	ownerWrite, controlWrite, responseRead, statusRead := supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead
	supervisor.mu.Unlock()
	if mirror != nil {
		if err := releaseProcessGroupOwner(mirror); err != nil {
			return fmt.Errorf("release Linux supervisor mirror: %w", err)
		}
	} else if fd >= 0 {
		_ = unix.Close(fd)
	}
	supervisor.mu.Lock()
	supervisor.released = true
	supervisor.closed = true
	supervisor.pidfd = -1
	supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead = nil, nil, nil, nil
	supervisor.mu.Unlock()
	closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead)
	return nil
}

func (supervisor *linuxSupervisor) releaseThroughRecovery(mirror *processGroup) error {
	if supervisor == nil {
		return nil
	}
	value := supervisor.ContainmentAuthority()
	if value == nil || value.StopReceipt == nil || !value.StopReceipt.ValidFor(*value) {
		return ErrLinuxSupervisorStopUnproven
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	var lastErr error
	for {
		err := ReleasePersistedLinuxSupervisor(*value)
		if err == nil {
			return supervisor.finalizeReleasedContainment(mirror)
		}
		lastErr = err
		if !linuxSupervisorRecoveryRetryable(err) {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		timer := time.NewTimer(remaining)
		<-timer.C
	}
	return fmt.Errorf("release Linux supervisor through recovery endpoint: %w", lastErr)
}

func linuxSupervisorRecoveryRetryable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF)
}

func (supervisor *linuxSupervisor) verifyMirrorObservation() error {
	if supervisor == nil || supervisor.mirror == nil {
		return nil
	}
	mirror := supervisor.mirror
	deadline := time.Now().Add(containmentCloseDeadline)
	if mirror.monitorDone != nil && !channelClosedV2(mirror.monitorDone) {
		result, err := mirror.requestDescendantMonitorScan(deadline)
		if err != nil {
			return err
		}
		if result.scanErr != nil {
			return result.scanErr
		}
	}
	mirror.mutex.Lock()
	defer mirror.mutex.Unlock()
	if mirror.descendantScanLost || len(mirror.escapedDescendants) > 0 {
		return fmt.Errorf("%w: mirror observed descendant escape or scan loss", ErrLinuxDescendantContainmentUnproven)
	}
	if !mirror.descendantScanCompleted {
		return fmt.Errorf("%w: mirror has no completed descendant observation", ErrLinuxDescendantContainmentUnproven)
	}
	return nil
}

// releaseAfterHelperDeath is the only local release path. A durable receipt
// proves the target tree stopped; the helperDone fence proves the exact helper
// process has already exited, so no request may be sent over its dead pipes.
func (supervisor *linuxSupervisor) releaseAfterHelperDeath(mirror *processGroup) error {
	if supervisor == nil || !channelClosedV2(supervisor.helperDone) {
		return ErrLinuxSupervisorHelperExited
	}
	if actual, err := ProcessIdentity(supervisor.authority.SupervisorPID); err == nil {
		return fmt.Errorf("release Linux supervisor helper: exact helper is still alive as %q", actual)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release Linux supervisor helper: prove exact helper exit: %w", err)
	}
	if err := provePersistedLinuxSupervisorRelease(supervisor.authority); err != nil {
		return fmt.Errorf("release Linux supervisor physical proof: %w", err)
	}
	if mirror != nil {
		if err := releaseProcessGroupOwner(mirror); err != nil {
			return fmt.Errorf("release Linux supervisor mirror after helper death: %w", err)
		}
	}
	supervisor.mu.Lock()
	if supervisor.released {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.released = true
	supervisor.closed = true
	fd := supervisor.pidfd
	supervisor.pidfd = -1
	ownerWrite, controlWrite, responseRead, statusRead := supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead
	supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead = nil, nil, nil, nil
	supervisor.mu.Unlock()
	if mirror == nil && fd >= 0 {
		_ = unix.Close(fd)
	}
	closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead)
	return nil
}

func (supervisor *linuxSupervisor) AbortContainment() error {
	if supervisor == nil {
		return nil
	}
	if err := supervisor.stop(linuxSupervisorOpAbort); err != nil {
		return err
	}
	return supervisor.ReleaseContainment()
}

func (supervisor *linuxSupervisor) RenewLease(deadline time.Duration, sequence uint64) error {
	if deadline <= 0 || sequence == 0 {
		return errors.New("Linux supervisor lease deadline and sequence must be positive")
	}
	response, err := supervisor.request(linuxSupervisorOpRenew, sequence, deadline)
	if err != nil {
		return err
	}
	if response.Status != "renewed" && response.Status != "already_renewed" {
		return fmt.Errorf("Linux supervisor lease returned %q", response.Status)
	}
	return nil
}

func (supervisor *linuxSupervisor) LeaseRenewalAvailable() bool {
	if supervisor == nil {
		return false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return !supervisor.closed && !supervisor.released && !channelClosedV2(supervisor.helperDone)
}

func (supervisor *linuxSupervisor) request(operation string, sequence uint64, deadline time.Duration) (linuxSupervisorResponse, error) {
	supervisor.requestMu.Lock()
	defer supervisor.requestMu.Unlock()
	supervisor.mu.Lock()
	if supervisor.closed || supervisor.released {
		supervisor.mu.Unlock()
		return linuxSupervisorResponse{}, ErrLinuxSupervisorReleased
	}
	if supervisor.responseTransportLost || supervisor.responseLossPending || supervisor.responseRead == nil || supervisor.responseReader == nil {
		supervisor.responseTransportLost = true
		supervisor.responseLossPending = true
		supervisor.mu.Unlock()
		return linuxSupervisorResponse{}, ErrLinuxSupervisorResponseLost
	}
	if channelClosedV2(supervisor.helperDone) {
		supervisor.mu.Unlock()
		return linuxSupervisorResponse{}, ErrLinuxSupervisorHelperExited
	}
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: operation, LaunchToken: supervisor.authority.LaunchToken, Sequence: sequence, DeadlineMS: deadline.Milliseconds(), TargetPID: supervisor.authority.TargetPID, TargetIdentity: supervisor.authority.TargetIdentity, OwnerKind: supervisor.authority.OwnerKind, OwnerContext: supervisor.authority.OwnerContext, SupervisorPID: supervisor.authority.SupervisorPID, SupervisorIdentity: supervisor.authority.SupervisorIdentity, Token: supervisor.authority.PipeToken, JobID: supervisor.authority.JobID, Secret: supervisor.authority.Secret}
	control, reader := supervisor.controlWrite, supervisor.responseReader
	supervisor.mu.Unlock()
	if control == nil || reader == nil {
		return linuxSupervisorResponse{}, ErrLinuxSupervisorHelperExited
	}
	if err := writeLinuxSupervisorFrame(control, request); err != nil {
		return linuxSupervisorResponse{}, err
	}
	supervisor.dropResponseTransportOnce(operation)
	var response linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(reader, &response); err != nil {
		supervisor.mu.Lock()
		supervisor.responseTransportLost = true
		supervisor.responseLossPending = true
		supervisor.mu.Unlock()
		return linuxSupervisorResponse{}, errors.Join(ErrLinuxSupervisorResponseLost, err)
	}
	if response.Version != linuxSupervisorProtocolVersion || response.Operation != operation || response.LaunchToken != supervisor.authority.LaunchToken || response.TargetPID != supervisor.authority.TargetPID || response.TargetIdentity != supervisor.authority.TargetIdentity || response.OwnerKind != supervisor.authority.OwnerKind || response.OwnerContext != supervisor.authority.OwnerContext || response.SupervisorPID != supervisor.authority.SupervisorPID || response.SupervisorIdentity != supervisor.authority.SupervisorIdentity || response.Token != supervisor.authority.PipeToken || response.JobID != supervisor.authority.JobID {
		return linuxSupervisorResponse{}, errors.New("Linux supervisor response identity mismatch")
	}
	if response.Status == "error" {
		if response.Error == "" {
			return linuxSupervisorResponse{}, errors.New("Linux supervisor returned an unspecified error")
		}
		return response, errors.New(response.Error)
	}
	return response, nil
}

func (supervisor *linuxSupervisor) dropResponseTransportOnce(operation string) bool {
	if supervisor == nil || os.Getenv(linuxSupervisorProductionWitnessEnv) != "1" {
		return false
	}
	configuredOperation := os.Getenv(linuxSupervisorDropResponseOnceEnv)
	if configuredOperation != linuxSupervisorOpStop && configuredOperation != linuxSupervisorOpRelease || configuredOperation != operation {
		return false
	}

	supervisor.mu.Lock()
	if supervisor.responseDropTriggered || supervisor.responseRead == nil {
		supervisor.mu.Unlock()
		return false
	}
	responseRead := supervisor.responseRead
	supervisor.responseDropTriggered = true
	supervisor.responseTransportLost = true
	supervisor.responseLossPending = true
	supervisor.responseRead = nil
	supervisor.responseReader = nil
	supervisor.mu.Unlock()

	if markerPath := strings.TrimSpace(os.Getenv(linuxSupervisorDropResponseMarkerEnv)); markerPath != "" {
		_ = os.WriteFile(markerPath, []byte("fired\n"), 0o600)
	}
	_ = responseRead.Close()
	return true
}

type linuxSupervisorScanFailureInjection struct {
	triggerPath string
	firedPath   string
}

func linuxSupervisorScanFailureInjectionFromEnv() (linuxSupervisorScanFailureInjection, bool) {
	if os.Getenv(linuxSupervisorProductionWitnessEnv) != "1" ||
		strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureModeEnv)) == "" {
		return linuxSupervisorScanFailureInjection{}, false
	}
	if strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureModeEnv)) != linuxSupervisorScanFailureChildren {
		return linuxSupervisorScanFailureInjection{}, false
	}
	triggerPath := strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureTriggerEnv))
	firedPath := strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureFiredEnv))
	if triggerPath == "" || firedPath == "" || !filepath.IsAbs(triggerPath) || !filepath.IsAbs(firedPath) {
		return linuxSupervisorScanFailureInjection{}, false
	}
	return linuxSupervisorScanFailureInjection{triggerPath: triggerPath, firedPath: firedPath}, true
}

func validateLinuxSupervisorScanFailureEnv() error {
	if os.Getenv(linuxSupervisorProductionWitnessEnv) != "1" {
		return nil
	}
	mode := strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureModeEnv))
	if mode == "" {
		return nil
	}
	if mode != linuxSupervisorScanFailureChildren {
		return fmt.Errorf("unsupported Linux supervisor scan-failure mode %q", mode)
	}
	triggerPath := strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureTriggerEnv))
	firedPath := strings.TrimSpace(os.Getenv(linuxSupervisorScanFailureFiredEnv))
	if triggerPath == "" || firedPath == "" || !filepath.IsAbs(triggerPath) || !filepath.IsAbs(firedPath) {
		return errors.New("Linux supervisor scan-failure injection requires absolute trigger and fired marker paths")
	}
	return nil
}

func installLinuxSupervisorScanFailureInjection(group *processGroup, targetPID int, targetIdentity string) func() {
	injection, enabled := linuxSupervisorScanFailureInjectionFromEnv()
	if !enabled || group == nil || targetPID <= 0 || targetIdentity == "" {
		return func() {}
	}

	group.mutex.Lock()
	realStat := group.monitorReadStat
	realChildren := group.monitorReadChildren
	if realStat == nil || realChildren == nil {
		group.mutex.Unlock()
		return func() {}
	}
	var fired atomic.Bool
	var restored atomic.Bool
	var injectedStat func(int) (linuxProcessStat, error)
	var injectedChildren func(int) ([]int, error)
	restore := func() {
		if !restored.CompareAndSwap(false, true) {
			return
		}
		group.mutex.Lock()
		group.monitorReadStat = realStat
		group.monitorReadChildren = realChildren
		group.mutex.Unlock()
	}
	maybeInject := func(pid int) error {
		if pid != targetPID {
			return nil
		}
		if _, err := os.Stat(injection.triggerPath); err != nil {
			return nil
		}
		actualIdentity, identityErr := readProcessIdentity(pid)
		if identityErr != nil || actualIdentity != targetIdentity || !fired.CompareAndSwap(false, true) {
			return nil
		}
		markerErr := os.WriteFile(injection.firedPath, []byte("fired\n"), 0o600)
		restore()
		if markerErr != nil {
			return fmt.Errorf("%w: write fired marker: %v", errLinuxSupervisorInjectedDescendantScanFailure, markerErr)
		}
		return errLinuxSupervisorInjectedDescendantScanFailure
	}
	injectedStat = func(pid int) (linuxProcessStat, error) {
		return realStat(pid)
	}
	injectedChildren = func(pid int) ([]int, error) {
		if err := maybeInject(pid); err != nil {
			return nil, err
		}
		return realChildren(pid)
	}
	group.monitorReadStat = injectedStat
	group.monitorReadChildren = injectedChildren
	group.mutex.Unlock()
	return restore
}

func channelClosedV2(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (anchor LinuxProcessGroupAnchor) private() linuxProcessGroupAnchor {
	return linuxProcessGroupAnchor{pid: anchor.PID, pgrp: anchor.PGRP, session: anchor.Session, startTime: anchor.StartTime}
}

// RunContainmentSupervisor is the hidden production helper entrypoint.
func RunContainmentSupervisor(args []string) error {
	// Keep the helper's ptrace owner on one OS thread from target launch through
	// the later Resume/detach boundary.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ensureLinuxChildSubreaper(); err != nil {
		return err
	}
	fd, err := parseLinuxSupervisorFDs(args)
	if err != nil {
		return err
	}
	if err := validateLinuxSupervisorScanFailureEnv(); err != nil {
		return err
	}
	files := make([]*os.File, 0, 9)
	for _, value := range []struct {
		fd   int
		name string
	}{
		{fd.bootstrap, "bootstrap"}, {fd.owner, "owner"}, {fd.control, "control"}, {fd.response, "response"}, {fd.status, "status"}, {fd.metadata, "metadata"}, {fd.stdin, "stdin"}, {fd.stdout, "stdout"}, {fd.stderr, "stderr"},
	} {
		file := os.NewFile(uintptr(value.fd), "linux-supervisor-"+value.name)
		if file == nil {
			closeLinuxSupervisorFilesV2(files...)
			return errors.New("Linux supervisor inherited descriptor is invalid")
		}
		files = append(files, file)
	}
	defer closeLinuxSupervisorFilesV2(files...)
	// These descriptors belong exclusively to the helper protocol. Go's
	// exec_linux child path intentionally preserves inherited fd >= 3 unless
	// callers mark them close-on-exec, so never expose authority/control pipes
	// to the target command.
	for _, file := range files[:6] {
		unix.CloseOnExec(int(file.Fd()))
	}
	var launch linuxSupervisorLaunch
	if err := readLinuxSupervisorFrame(bufio.NewReader(files[0]), &launch); err != nil {
		return err
	}
	if launch.Version != linuxSupervisorProtocolVersion || launch.Operation != linuxSupervisorOpLaunch || launch.OwnerContext == "" || launch.Program == "" || !linuxSupervisorValidHexV2(launch.LaunchToken, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.PipeToken, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.JobID, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.SecretDigest, sha256.Size*2) {
		return errors.New("Linux supervisor launch is invalid")
	}
	if strings.TrimSpace(launch.Endpoint) == "" || !strings.Contains(launch.OwnerContext, "|endpoint=") {
		return errors.New("Linux supervisor recovery endpoint is invalid")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: launch.Endpoint, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen Linux supervisor recovery endpoint: %w", err)
	}
	_ = os.Chmod(launch.Endpoint, 0o600)
	defer func() {
		_ = listener.Close()
		_ = os.Remove(launch.Endpoint)
	}()
	command := exec.Command(launch.Program, launch.Args...)
	command.Env = append([]string(nil), launch.Env...)
	command.Dir = launch.Dir
	command.Stdin = files[6]
	command.Stdout = files[7]
	command.Stderr = files[8]
	target, err := LaunchLinuxPtrace(command)
	if err != nil {
		return fmt.Errorf("launch Linux supervisor target: %w", err)
	}
	containment, identity, attachErr := AttachProcess(command.Process)
	if attachErr != nil {
		if containment != nil {
			_ = containment.Terminate(true)
			_ = containment.Close()
		}
		_ = target.Terminate()
		_, _ = target.Wait()
		return fmt.Errorf("attach Linux supervisor target containment: %w", attachErr)
	}
	group, ok := containment.(*processGroup)
	if !ok || group == nil {
		return errors.New("Linux supervisor target containment owner is unavailable")
	}
	anchor := target.ProcessGroupAnchor()
	if identity != target.Identity() || group.anchor != anchor.private() {
		_ = group.Terminate(true)
		_ = group.Close()
		_ = target.Terminate()
		_, _ = target.Wait()
		return errors.New("Linux supervisor target identity or anchor mismatch")
	}
	restoreScanFailureInjection := installLinuxSupervisorScanFailureInjection(group, target.PID(), identity)
	defer restoreScanFailureInjection()
	groupFD, err := unix.FcntlInt(uintptr(group.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = group.Terminate(true)
		_ = group.Close()
		_ = target.Terminate()
		_, _ = target.Wait()
		return fmt.Errorf("duplicate Linux supervisor containment pidfd: %w", err)
	}
	if err := sendLinuxSupervisorFD(files[5], groupFD); err != nil {
		_ = unix.Close(groupFD)
		_ = group.Terminate(true)
		_ = group.Close()
		_ = target.Terminate()
		_, _ = target.Wait()
		return err
	}
	_ = unix.Close(groupFD)
	helperIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	var preHelloReceipt *authority.StopReceipt
	preHelloWaitDone := make(chan struct{})
	var preHelloWaitOnce sync.Once
	preHelloStartWait := func() {
		preHelloWaitOnce.Do(func() {
			go func() {
				_, _ = target.Wait()
				close(preHelloWaitDone)
			}()
		})
	}
	preHelloStop := func() error {
		if preHelloReceipt != nil {
			return nil
		}
		terminateErr := target.Terminate()
		if terminateErr != nil {
			if retryErr := target.Terminate(); retryErr != nil {
				return errors.Join(terminateErr, retryErr, group.Close())
			}
		}
		if err := group.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
			return err
		}
		group.mutex.Lock()
		group.skipNextCloseSignal = true
		group.mutex.Unlock()
		preHelloStartWait()
		<-preHelloWaitDone
		deadline := containmentDeadline(context.Background())
		if err := reapLinuxProcessGroupChildren(group.anchor, deadline); err != nil {
			return err
		}
		if err := group.closeUntil(deadline); err != nil {
			return err
		}
		receipt := authority.StopReceipt{Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, TargetPID: target.PID(), TargetIdentity: target.Identity(), PipeToken: launch.PipeToken, JobID: launch.JobID, SupervisorPID: os.Getpid(), SupervisorIdentity: helperIdentity, ActiveProcesses: 0}
		preHelloReceipt = &receipt
		return nil
	}
	hello := linuxSupervisorHello{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpHello, LaunchToken: launch.LaunchToken, OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, TargetPID: target.PID(), TargetIdentity: target.Identity(), TargetPGRP: anchor.PGRP, TargetSession: anchor.Session, TargetStartTime: anchor.StartTime, SupervisorPID: os.Getpid(), SupervisorIdentity: helperIdentity, PipeToken: launch.PipeToken, JobID: launch.JobID}
	if err := writeLinuxSupervisorFrame(files[3], hello); err != nil {
		if stopErr := preHelloStop(); stopErr != nil {
			return errors.Join(err, runLinuxSupervisorRecoveryLoop(listener, launch, target, &preHelloReceipt, "", preHelloStop, preHelloWaitDone))
		}
		return err
	}
	return runLinuxSupervisorHelperLoop(launch, listener, target, group, files[1], files[2], files[3], files[4], files[6], files[7], files[8])
}

type linuxSupervisorFDs struct{ bootstrap, owner, control, response, status, metadata, stdin, stdout, stderr int }

func parseLinuxSupervisorFDs(args []string) (linuxSupervisorFDs, error) {
	set := flag.NewFlagSet("containment-supervisor", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	values := map[string]*string{}
	for _, name := range []string{"bootstrap", "owner", "control", "response", "status", "metadata", "stdin", "stdout", "stderr"} {
		values[name] = set.String(name+"-fd", "", "")
	}
	if err := set.Parse(args); err != nil {
		return linuxSupervisorFDs{}, err
	}
	parse := func(name string) (int, error) {
		value, err := strconv.Atoi(*values[name])
		if err != nil || value <= 2 {
			return 0, fmt.Errorf("Linux supervisor %s fd is invalid", name)
		}
		return value, nil
	}
	parsed := linuxSupervisorFDs{}
	fields := []struct {
		name string
		dst  *int
	}{{"bootstrap", &parsed.bootstrap}, {"owner", &parsed.owner}, {"control", &parsed.control}, {"response", &parsed.response}, {"status", &parsed.status}, {"metadata", &parsed.metadata}, {"stdin", &parsed.stdin}, {"stdout", &parsed.stdout}, {"stderr", &parsed.stderr}}
	for _, field := range fields {
		value, err := parse(field.name)
		if err != nil {
			return linuxSupervisorFDs{}, err
		}
		*field.dst = value
	}
	return parsed, nil
}

func runLinuxSupervisorHelperLoop(launch linuxSupervisorLaunch, listener *net.UnixListener, target *LinuxPtraceProcess, group *processGroup, ownerFile, controlFile, responseFile, statusFile, stdinFile, stdoutFile, stderrFile *os.File) error {
	leaseState := newLinuxSupervisorLeaseState()
	defer leaseState.stop()
	watchdog := newLinuxSupervisorOwnerWatchdog(ownerFile, leaseState.latchOwnerLost)
	defer watchdog.disarm()
	var stateMu sync.Mutex
	bound := false
	committed := false
	resumed := false
	stopped := false
	var receipt *authority.StopReceipt
	boundSecret := ""
	waitDone := make(chan struct{})
	var waitOnce sync.Once
	startWait := func() {
		waitOnce.Do(func() {
			go func() {
				code, waitErr := target.Wait()
				closeLinuxSupervisorFilesV2(stdinFile, stdoutFile, stderrFile)
				response := linuxSupervisorResponse{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpWait, Status: "exited", TargetPID: target.PID(), TargetIdentity: target.Identity(), ExitCode: code}
				if waitErr != nil {
					response.ExitError = waitErr.Error()
				}
				_ = writeLinuxSupervisorFrame(statusFile, response)
				close(waitDone)
			}()
		})
	}
	stop := func() error {
		leaseState.stop()
		stateMu.Lock()
		if receipt != nil {
			stateMu.Unlock()
			return nil
		}
		stateMu.Unlock()
		var terminateErr error
		if !resumed {
			terminateErr = target.Terminate()
			if terminateErr != nil {
				if retryErr := target.Terminate(); retryErr != nil {
					return errors.Join(terminateErr, retryErr, group.Close())
				}
			}
		}
		// Signal the exact process group before reaping the leader. Reaping first
		// can make a same-group descendant unreachable from the leader fence.
		if err := group.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
			return err
		}
		group.mutex.Lock()
		group.skipNextCloseSignal = true
		group.mutex.Unlock()
		startWait()
		<-waitDone
		deadline := containmentDeadline(context.Background())
		if err := reapLinuxProcessGroupChildren(group.anchor, deadline); err != nil {
			return err
		}
		if err := group.closeUntil(deadline); err != nil {
			return err
		}
		receiptValue := authority.StopReceipt{Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, TargetPID: target.PID(), TargetIdentity: target.Identity(), PipeToken: launch.PipeToken, JobID: launch.JobID, SupervisorPID: os.Getpid(), SupervisorIdentity: mustLinuxSupervisorIdentity(), ActiveProcesses: 0}
		receipt = &receiptValue
		stateMu.Lock()
		stopped = true
		stateMu.Unlock()
		return nil
	}
	enterRecovery := func() error {
		if err := stop(); err != nil {
			// A failed physical stop retains the helper's authority and the
			// authenticated recovery endpoint for a later retry.
			return runLinuxSupervisorRecoveryLoop(listener, launch, target, &receipt, boundSecret, stop, waitDone)
		}
		<-waitDone
		return runLinuxSupervisorRecoveryLoop(listener, launch, target, &receipt, boundSecret, stop, waitDone)
	}
	type requestEvent struct {
		request linuxSupervisorRequest
		err     error
	}
	requestEvents := make(chan requestEvent, 1)
	go func() {
		reader := bufio.NewReader(controlFile)
		for {
			var request linuxSupervisorRequest
			err := readLinuxSupervisorFrame(reader, &request)
			requestEvents <- requestEvent{request: request, err: err}
			if err != nil {
				return
			}
		}
	}()
	ownerLost := watchdog.ownerLost()
	for {
		select {
		case <-ownerLost:
			ownerLost = nil
			if err := stop(); err != nil {
				// Keep the exact helper endpoint alive for an authenticated
				// recovery retry. A failed proof never becomes a receipt.
				return runLinuxSupervisorRecoveryLoop(listener, launch, target, &receipt, boundSecret, stop, waitDone)
			}
			<-waitDone
			return runLinuxSupervisorRecoveryLoop(listener, launch, target, &receipt, boundSecret, stop, waitDone)
		case <-leaseState.wakeup():
			if !leaseState.takeExpiry() {
				continue
			}
			if err := stop(); err != nil {
				// Lease expiry does not prove that the owner process is dead. Keep
				// the authenticated control channel available so the live owner can
				// retry the physical stop; control EOF/owner loss will migrate to
				// the recovery endpoint below.
				continue
			}
			// The target is reaped and the receipt is durable. Continue serving
			// the original pipe so the owner can release it without a channel
			// migration race.
			continue
		case event := <-requestEvents:
			if event.err != nil {
				// Control EOF/error is itself an owner-loss boundary. The owner
				// watchdog may publish its notification later, so never return
				// the raw protocol error while target authority is still active.
				return enterRecovery()
			}
			request := event.request
			if !bound && request.Operation == linuxSupervisorOpAbort && linuxSupervisorUnboundAbortRequestMatches(request, launch, target) {
				if err := stop(); err != nil {
					if writeErr := writeLinuxSupervisorResponseV2(responseFile, request, "error", target, launch, 0, err.Error()); writeErr != nil {
						return enterRecovery()
					}
					continue
				}
				if err := writeLinuxSupervisorResponseV2(responseFile, request, "aborted", target, launch, 0, ""); err != nil {
					return enterRecovery()
				}
				<-waitDone
				return nil
			}
			if request.Operation == linuxSupervisorOpBind {
				if !linuxSupervisorRequestMatchesV2(request, launch, target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest) {
					continue
				}
				bound = true
				boundSecret = request.Secret
				if err := writeLinuxSupervisorResponseV2(responseFile, request, "bound", target, launch, 0, ""); err != nil {
					return enterRecovery()
				}
				continue
			}
			if !bound {
				if request.Operation == linuxSupervisorOpAbort || request.Operation == linuxSupervisorOpStop || request.Operation == linuxSupervisorOpClose {
					if !linuxSupervisorRequestMatchesV2(request, launch, target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest) {
						continue
					}
					if err := stop(); err != nil {
						if writeErr := writeLinuxSupervisorResponseV2(responseFile, request, "error", target, launch, 0, err.Error()); writeErr != nil {
							return enterRecovery()
						}
						continue
					}
					if err := writeLinuxSupervisorResponseV2(responseFile, request, "stopped", target, launch, 0, ""); err != nil {
						return enterRecovery()
					}
					<-waitDone
					return nil
				}
				continue
			}
			if !linuxSupervisorRequestMatchesV2(request, launch, target) || request.Secret == "" || !linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest) {
				continue
			}
			status := "ok"
			message := ""
			needsRecovery := false
			switch request.Operation {
			case linuxSupervisorOpCommit:
				committed = true
				status = "committed"
			case linuxSupervisorOpResume:
				if !committed {
					status, message = "error", ErrLinuxSupervisorNotCommitted.Error()
				} else if stopped {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else if err := leaseState.resume(func() bool { return channelClosedV2(watchdog.ownerLost()) }, target.Resume); err != nil {
					status, message = "error", err.Error()
				} else {
					resumed = true
					startWait()
					status = "resumed"
				}
			case linuxSupervisorOpRenew:
				replay, err := leaseState.renew(time.Duration(request.DeadlineMS)*time.Millisecond, request.Sequence)
				if err != nil {
					status, message = "error", err.Error()
				} else if replay {
					status = "already_renewed"
				} else {
					status = "renewed"
				}
			case linuxSupervisorOpStop, linuxSupervisorOpClose, linuxSupervisorOpAbort:
				if err := stop(); err != nil {
					status, message = "error", err.Error()
					needsRecovery = true
				} else {
					status = "stopped"
				}
			case linuxSupervisorOpRelease:
				if receipt == nil {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else {
					status = "released"
				}
			default:
				status, message = "error", "unknown Linux supervisor operation"
			}
			if err := writeLinuxSupervisorResponseV2(responseFile, request, status, target, launch, 0, message); err != nil {
				return enterRecovery()
			}
			if needsRecovery {
				// The error response reached the live daemon. Keep servicing the
				// original control channel so it can retry the physical proof;
				// switch to the recovery endpoint only when that channel itself is
				// lost or a response write fails.
				continue
			}
			if request.Operation == linuxSupervisorOpRelease && status == "released" {
				<-waitDone
				return nil
			}
		}
	}
}

func runLinuxSupervisorRecoveryLoop(listener *net.UnixListener, launch linuxSupervisorLaunch, target *LinuxPtraceProcess, receipt **authority.StopReceipt, boundSecret string, stop func() error, waitDone <-chan struct{}) error {
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			return err
		}
		if err := connection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
			_ = connection.Close()
			continue
		}
		reader := bufio.NewReader(connection)
		var request linuxSupervisorRequest
		if err := readLinuxSupervisorFrame(reader, &request); err != nil {
			_ = connection.Close()
			continue
		}
		unboundAbort := boundSecret == "" && request.Operation == linuxSupervisorOpAbort && request.SupervisorPID == 0 && request.SupervisorIdentity == ""
		valid := false
		if unboundAbort {
			valid = linuxSupervisorUnboundAbortRequestMatches(request, launch, target)
		} else {
			valid = linuxSupervisorRequestMatchesV2(request, launch, target) && request.Secret != ""
			if boundSecret != "" {
				valid = valid && request.Secret == boundSecret
			} else {
				valid = valid && linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest)
			}
		}
		status, message := "stopped", ""
		if !valid {
			status, message = "error", "Linux supervisor recovery identity mismatch"
		} else if unboundAbort {
			if receipt == nil || *receipt == nil {
				if stop == nil {
					status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
				} else if stopErr := stop(); stopErr != nil {
					status, message = "error", stopErr.Error()
				} else {
					status = "aborted"
				}
			} else {
				status = "aborted"
			}
		} else if (receipt == nil || *receipt == nil) && (request.Operation == linuxSupervisorOpStop || request.Operation == linuxSupervisorOpClose || request.Operation == linuxSupervisorOpAbort) {
			if stop == nil {
				status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
			} else if stopErr := stop(); stopErr != nil {
				status, message = "error", stopErr.Error()
			} else {
				status = "stopped"
			}
		} else if receipt == nil || *receipt == nil {
			status, message = "error", ErrLinuxSupervisorStopUnproven.Error()
		} else if request.Operation == linuxSupervisorOpRelease {
			status = "released"
		}
		identity := mustLinuxSupervisorIdentity()
		response := linuxSupervisorResponse{Version: linuxSupervisorProtocolVersion, Operation: request.Operation, Status: status, LaunchToken: launch.LaunchToken, Sequence: request.Sequence, TargetPID: target.PID(), TargetIdentity: target.Identity(), OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, SupervisorPID: os.Getpid(), SupervisorIdentity: identity, Token: launch.PipeToken, JobID: launch.JobID, ActiveProcesses: 0, Error: message}
		if err := writeLinuxSupervisorFrame(connection, response); err != nil {
			_ = connection.Close()
			continue
		}
		_ = connection.Close()
		if status == "aborted" {
			if waitDone != nil {
				<-waitDone
			}
			return nil
		}
		if status == "released" {
			return nil
		}
	}
}

func writeLinuxSupervisorResponseV2(writer *os.File, request linuxSupervisorRequest, status string, target *LinuxPtraceProcess, launch linuxSupervisorLaunch, active uint32, message string) error {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	return writeLinuxSupervisorFrame(writer, linuxSupervisorResponse{Version: linuxSupervisorProtocolVersion, Operation: request.Operation, Status: status, LaunchToken: launch.LaunchToken, Sequence: request.Sequence, TargetPID: target.PID(), TargetIdentity: target.Identity(), OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, SupervisorPID: os.Getpid(), SupervisorIdentity: identity, Token: launch.PipeToken, JobID: launch.JobID, ActiveProcesses: active, Error: message})
}

func mustLinuxSupervisorIdentity() string {
	identity, _ := ProcessIdentity(os.Getpid())
	return identity
}

// StopPersistedLinuxSupervisor authenticates the helper recovery endpoint and
// returns a fresh exact stop receipt.  Endpoint absence, helper identity
// mismatch, and unknown responses are all unresolved outcomes.
func StopPersistedLinuxSupervisor(value authority.Supervisor) (authority.StopReceipt, error) {
	if err := value.Validate(); err != nil {
		return authority.StopReceipt{}, fmt.Errorf("validate persisted Linux supervisor authority: %w", err)
	}
	connection, err := dialLinuxSupervisorRecovery(value)
	if err != nil {
		if !isLinuxSupervisorRecoveryEndpointUnreachable(err) {
			return authority.StopReceipt{}, fmt.Errorf("connect persisted Linux supervisor: %w", err)
		}
		if proofErr := provePersistedLinuxSupervisorStop(value); proofErr != nil {
			return authority.StopReceipt{}, errors.Join(fmt.Errorf("connect persisted Linux supervisor: %w", err), proofErr)
		}
		return persistedLinuxSupervisorStopReceipt(value)
	}
	defer connection.Close()
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpStop, LaunchToken: value.LaunchToken, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, Token: value.PipeToken, JobID: value.JobID, Secret: value.Secret}
	response, err := exchangeLinuxSupervisorRecovery(connection, request, value)
	if err != nil {
		return authority.StopReceipt{}, err
	}
	if response.Status != "stopped" || response.ActiveProcesses != 0 {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted helper returned %q", ErrLinuxSupervisorStopUnproven, response.Status)
	}
	return persistedLinuxSupervisorStopReceipt(value)
}

// ReleasePersistedLinuxSupervisor releases a helper after the caller has
// durably recorded the exact stop receipt.
func ReleasePersistedLinuxSupervisor(value authority.Supervisor) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.StopReceipt == nil || !value.StopReceipt.ValidFor(value) {
		return ErrLinuxSupervisorStopUnproven
	}
	connection, err := dialLinuxSupervisorRecovery(value)
	if err != nil {
		if !isLinuxSupervisorRecoveryEndpointUnreachable(err) {
			return fmt.Errorf("connect persisted Linux supervisor for release: %w", err)
		}
		if proofErr := provePersistedLinuxSupervisorRelease(value); proofErr == nil {
			return nil
		} else {
			return errors.Join(fmt.Errorf("connect persisted Linux supervisor for release: %w", err), proofErr)
		}
	}
	defer connection.Close()
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpRelease, LaunchToken: value.LaunchToken, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, Token: value.PipeToken, JobID: value.JobID, Secret: value.Secret}
	response, err := exchangeLinuxSupervisorRecovery(connection, request, value)
	if err != nil {
		return err
	}
	if response.Status != "released" {
		return fmt.Errorf("release persisted Linux supervisor returned %q", response.Status)
	}
	return nil
}

func persistedLinuxSupervisorStopReceipt(value authority.Supervisor) (authority.StopReceipt, error) {
	receipt := authority.StopReceipt{Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0}
	if !receipt.ValidFor(value) {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted helper receipt identity mismatch", ErrLinuxSupervisorStopUnproven)
	}
	return receipt, nil
}

func isLinuxSupervisorRecoveryEndpointUnreachable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// provePersistedLinuxSupervisorStop is the restart-safe local stop boundary.
// An absent endpoint is not sufficient by itself: the exact helper, target
// identity, and original process group must all be absent as well.
func provePersistedLinuxSupervisorStop(value authority.Supervisor) error {
	if err := proveLinuxSupervisorHelperAbsent(value.SupervisorPID, value.SupervisorIdentity); err != nil {
		return err
	}
	if err := proveLinuxSupervisorTargetAbsent(value.TargetPID, value.TargetIdentity); err != nil {
		return err
	}
	deadline := containmentDeadline(context.Background())
	if err := proveLinuxSupervisorGroupAbsentWithRetry(context.Background(), value.TargetPID, value.TargetIdentity, deadline); err != nil {
		return fmt.Errorf("prove persisted Linux supervisor process group absent: %w", err)
	}
	return nil
}

// provePersistedLinuxSupervisorRelease extends the stop proof with stale
// endpoint retirement. The endpoint is cleared only after the exact stop
// receipt has been reconstructed and durably supplied by the caller.
func provePersistedLinuxSupervisorRelease(value authority.Supervisor) error {
	if err := provePersistedLinuxSupervisorStop(value); err != nil {
		return err
	}
	return retireLinuxSupervisorEndpoint(value.OwnerContext)
}

func proveLinuxSupervisorHelperAbsent(pid int, expectedIdentity string) error {
	actualIdentity, err := linuxSupervisorProcessIdentity(pid)
	if err == nil {
		if actualIdentity == expectedIdentity {
			return errors.New("Linux supervisor helper is still present")
		}
		return fmt.Errorf("Linux supervisor helper PID %d has a different live identity %q", pid, actualIdentity)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("prove Linux supervisor helper absent: %w", err)
}

// retireLinuxSupervisorEndpoint removes only a stale socket that no longer
// accepts connections. A live or replacement listener remains unresolved.
func retireLinuxSupervisorEndpoint(ownerContext string) error {
	endpoint, err := linuxSupervisorEndpoint(ownerContext)
	if err != nil {
		return fmt.Errorf("parse Linux supervisor recovery endpoint: %w", err)
	}
	info, err := linuxSupervisorLstat(endpoint)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Linux supervisor recovery endpoint: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("Linux supervisor recovery endpoint is not a socket")
	}
	connection, dialErr := net.DialTimeout("unix", endpoint, linuxSupervisorTimeout)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("Linux supervisor recovery endpoint still accepts connections")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("probe stale Linux supervisor recovery endpoint: %w", dialErr)
	}
	if err := os.Remove(endpoint); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale Linux supervisor recovery endpoint: %w", err)
	}
	if _, err := linuxSupervisorLstat(endpoint); err == nil {
		return errors.New("Linux supervisor recovery endpoint remained after stale removal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("verify stale Linux supervisor recovery endpoint removal: %w", err)
	}
	return nil
}

// ProvePreparedSupervisorAborted proves that an unbound Linux supervisor
// handoff left no live recovery endpoint, exact target, or original process
// group. Any live object or uncertain observation remains unresolved; this
// proof never claims that a target was stopped.
func ProvePreparedSupervisorAborted(handoff authority.SupervisorHandoff, verifyOldOwnerQuiesced SupervisorHandoffAbortVerifier) (authority.SupervisorHandoffAbortProof, error) {
	if verifyOldOwnerQuiesced == nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff abort verifier is required")
	}
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if handoff.OwnerKind != authority.OwnerKindLinuxHelper || handoff.CreatorSessionID != nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("Linux supervisor abort proof has invalid owner binding")
	}
	if handoff.SupervisorPID != 0 {
		return authority.SupervisorHandoffAbortProof{}, errors.New("Linux supervisor abort proof requires an unbound handoff")
	}
	if handoff.StopReceipt != nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("Linux supervisor abort proof cannot carry a stop receipt")
	}
	if err := verifyOldOwnerQuiesced(); err != nil {
		return authority.SupervisorHandoffAbortProof{}, fmt.Errorf("verify old supervisor owner quiescence: %w", err)
	}
	if _, err := requestLinuxSupervisorAbort(handoff); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if err := waitForLinuxSupervisorAbortAbsence(handoff); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}

	proof := authority.SupervisorHandoffAbortProof{
		Version:        authority.SupervisorHandoffAbortProofVersion,
		Disposition:    authority.SupervisorHandoffAbortDisposition,
		LaunchToken:    handoff.LaunchToken,
		JobID:          handoff.JobID,
		OwnerKind:      handoff.OwnerKind,
		OwnerContext:   handoff.OwnerContext,
		TargetPID:      handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity,
	}
	if err := proof.Validate(); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if !proof.ValidFor(handoff) {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff abort proof does not match handoff")
	}
	return proof, nil
}

func requestLinuxSupervisorAbort(handoff authority.SupervisorHandoff) (bool, error) {
	endpoint, err := linuxSupervisorEndpoint(handoff.OwnerContext)
	if err != nil {
		return false, fmt.Errorf("connect Linux supervisor recovery endpoint: %w", err)
	}
	connection, err := net.DialTimeout("unix", endpoint, linuxSupervisorTimeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("connect Linux supervisor recovery endpoint: %w", err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return false, errors.New("Linux supervisor abort connection is not Unix")
	}
	defer unixConnection.Close()
	if err := unixConnection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
		return true, fmt.Errorf("set Linux supervisor abort deadline: %w", err)
	}
	defer unixConnection.SetDeadline(time.Time{})
	if err := verifyLinuxSupervisorUnboundPeer(unixConnection, handoff.OwnerContext); err != nil {
		return true, err
	}
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpAbort, LaunchToken: handoff.LaunchToken, TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity, OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext, Token: handoff.PipeToken, JobID: handoff.JobID, Secret: handoff.Secret}
	if err := writeLinuxSupervisorFrame(unixConnection, request); err != nil {
		return true, fmt.Errorf("send Linux supervisor abort request: %w", err)
	}
	var response linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(bufio.NewReader(unixConnection), &response); err != nil {
		return true, fmt.Errorf("read Linux supervisor abort response: %w", err)
	}
	if response.Version != linuxSupervisorProtocolVersion || response.Operation != linuxSupervisorOpAbort || response.Sequence != request.Sequence || response.LaunchToken != handoff.LaunchToken || response.TargetPID != handoff.TargetPID || response.TargetIdentity != handoff.TargetIdentity || response.OwnerKind != handoff.OwnerKind || response.OwnerContext != handoff.OwnerContext || response.Token != handoff.PipeToken || response.JobID != handoff.JobID {
		return true, errors.New("Linux supervisor abort response identity mismatch")
	}
	if response.Status == "error" {
		if response.Error == "" {
			return true, errors.New("Linux supervisor abort returned an unspecified error")
		}
		return true, errors.New(response.Error)
	}
	if response.Status != "aborted" || response.ActiveProcesses != 0 {
		return true, fmt.Errorf("Linux supervisor abort returned %q with %d active processes", response.Status, response.ActiveProcesses)
	}
	if response.SupervisorPID <= 0 || response.SupervisorIdentity == "" {
		return true, errors.New("Linux supervisor abort response has no helper identity")
	}
	if err := verifyLinuxSupervisorPeer(unixConnection, response.SupervisorPID, response.SupervisorIdentity); err != nil {
		return true, err
	}
	actualIdentity, err := linuxSupervisorProcessIdentity(response.SupervisorPID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return true, fmt.Errorf("Linux supervisor abort helper identity is not exact: %v", err)
	}
	if err == nil && actualIdentity != response.SupervisorIdentity {
		return true, fmt.Errorf("Linux supervisor abort helper identity is not exact: got %q, want %q", actualIdentity, response.SupervisorIdentity)
	}
	return true, nil
}

func waitForLinuxSupervisorAbortAbsence(handoff authority.SupervisorHandoff) error {
	deadline := time.Now().Add(linuxSupervisorTimeout)
	var lastErr error
	for {
		endpointErr := proveLinuxSupervisorEndpointAbsent(handoff.OwnerContext)
		targetErr := proveLinuxSupervisorTargetAbsent(handoff.TargetPID, handoff.TargetIdentity)
		groupErr := linuxSupervisorProveProcessGroupAbsent(context.Background(), handoff.TargetPID, handoff.TargetIdentity)
		if endpointErr == nil && targetErr == nil && groupErr == nil {
			return nil
		}
		var causes []error
		if endpointErr != nil {
			causes = append(causes, endpointErr)
		}
		if targetErr != nil {
			causes = append(causes, targetErr)
		}
		if groupErr != nil {
			causes = append(causes, fmt.Errorf("prove Linux supervisor process group absent: %w", groupErr))
		}
		lastErr = errors.Join(causes...)
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("Linux supervisor abort absence remained unproven: %w", lastErr)
		}
		if remaining > containmentCloseProbeInterval {
			remaining = containmentCloseProbeInterval
		}
		timer := time.NewTimer(remaining)
		<-timer.C
	}
}

func proveLinuxSupervisorEndpointAbsent(ownerContext string) error {
	endpoint, err := linuxSupervisorEndpoint(ownerContext)
	if err != nil {
		return fmt.Errorf("prove Linux supervisor recovery endpoint absent: %w", err)
	}
	if _, err := linuxSupervisorLstat(endpoint); err == nil {
		return errors.New("Linux supervisor recovery endpoint is still present")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prove Linux supervisor recovery endpoint absent: %w", err)
	}
	return nil
}

func proveLinuxSupervisorTargetAbsent(pid int, expectedIdentity string) error {
	actualIdentity, err := linuxSupervisorProcessIdentity(pid)
	if err == nil {
		if actualIdentity == expectedIdentity {
			return errors.New("Linux supervisor target is still present")
		}
		return fmt.Errorf("Linux supervisor target PID %d has a different live identity %q", pid, actualIdentity)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("prove Linux supervisor target absent: %w", err)
}

func dialLinuxSupervisorRecovery(value authority.Supervisor) (*net.UnixConn, error) {
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		return nil, err
	}
	connection, err := net.DialTimeout("unix", endpoint, linuxSupervisorTimeout)
	if err != nil {
		return nil, err
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("Linux supervisor recovery connection is not Unix")
	}
	return unixConnection, nil
}

func exchangeLinuxSupervisorRecovery(connection *net.UnixConn, request linuxSupervisorRequest, value authority.Supervisor) (linuxSupervisorResponse, error) {
	if err := connection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
		return linuxSupervisorResponse{}, fmt.Errorf("set Linux supervisor recovery deadline: %w", err)
	}
	defer connection.SetDeadline(time.Time{})
	if err := verifyLinuxSupervisorPeerBeforeSecret(connection, value.SupervisorPID, value.SupervisorIdentity); err != nil {
		return linuxSupervisorResponse{}, err
	}
	if err := writeLinuxSupervisorFrame(connection, request); err != nil {
		return linuxSupervisorResponse{}, err
	}
	var response linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(bufio.NewReader(connection), &response); err != nil {
		return linuxSupervisorResponse{}, err
	}
	if response.Version != linuxSupervisorProtocolVersion || response.Operation != request.Operation || response.LaunchToken != value.LaunchToken || response.TargetPID != value.TargetPID || response.TargetIdentity != value.TargetIdentity || response.OwnerKind != value.OwnerKind || response.OwnerContext != value.OwnerContext || response.SupervisorPID != value.SupervisorPID || response.SupervisorIdentity != value.SupervisorIdentity || response.Token != value.PipeToken || response.JobID != value.JobID {
		return linuxSupervisorResponse{}, errors.New("Linux supervisor recovery response identity mismatch")
	}
	if response.Status == "error" {
		if response.Error == "" {
			return linuxSupervisorResponse{}, errors.New("Linux supervisor recovery returned an unspecified error")
		}
		return linuxSupervisorResponse{}, errors.New(response.Error)
	}
	if err := verifyLinuxSupervisorPeer(connection, response.SupervisorPID, response.SupervisorIdentity); err != nil {
		return linuxSupervisorResponse{}, err
	}
	actual, err := ProcessIdentity(response.SupervisorPID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return linuxSupervisorResponse{}, fmt.Errorf("Linux supervisor recovery helper identity is not exact: %v", err)
	}
	if err == nil && actual != response.SupervisorIdentity {
		return linuxSupervisorResponse{}, fmt.Errorf("Linux supervisor recovery helper identity is not exact: got %q, want %q", actual, response.SupervisorIdentity)
	}
	return response, nil
}

func verifyLinuxSupervisorPeer(connection *net.UnixConn, expectedPID int, expectedIdentity string) error {
	if connection == nil || expectedPID <= 0 || expectedIdentity == "" {
		return errors.New("Linux supervisor peer identity is incomplete")
	}
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if credentials == nil || int(credentials.Pid) != expectedPID {
		return fmt.Errorf("Linux supervisor peer PID mismatch: got %d, want %d", credentialsPID(credentials), expectedPID)
	}
	actual, err := ProcessIdentity(expectedPID)
	if err != nil {
		// A legitimate release response may race the helper's final exit. The
		// socket peer credentials are already bound to the exact PID; absence
		// after that response is the expected terminal state.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read Linux supervisor peer process identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func verifyLinuxSupervisorPeerBeforeSecret(connection *net.UnixConn, expectedPID int, expectedIdentity string) error {
	if connection == nil || expectedPID <= 0 || expectedIdentity == "" {
		return errors.New("Linux supervisor peer identity is incomplete")
	}
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if int(credentials.Pid) != expectedPID {
		return fmt.Errorf("Linux supervisor peer PID mismatch: got %d, want %d", credentialsPID(credentials), expectedPID)
	}
	actual, err := ProcessIdentity(expectedPID)
	if err != nil {
		return fmt.Errorf("read Linux supervisor peer process identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func verifyLinuxSupervisorUnboundPeer(connection *net.UnixConn, ownerContext string) error {
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if credentials.Pid <= 0 {
		return errors.New("Linux supervisor unbound peer PID is invalid")
	}
	expectedIdentity, err := linuxSupervisorHelperIdentity(ownerContext)
	if err != nil {
		return err
	}
	actual, err := linuxSupervisorProcessIdentity(int(credentials.Pid))
	if err != nil {
		return fmt.Errorf("read Linux supervisor unbound peer identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor unbound peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func linuxSupervisorHelperIdentity(ownerContext string) (string, error) {
	marker := "|helper="
	start := strings.LastIndex(ownerContext, marker)
	if start < 0 {
		return "", errors.New("Linux supervisor owner context has no helper identity fence")
	}
	value := ownerContext[start+len(marker):]
	if end := strings.Index(value, "|endpoint="); end >= 0 {
		value = value[:end]
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) >= 4096 {
		return "", errors.New("Linux supervisor helper identity fence is invalid")
	}
	return value, nil
}

func linuxSupervisorPeerCredentials(connection *net.UnixConn) (*unix.Ucred, error) {
	if connection == nil {
		return nil, errors.New("Linux supervisor peer connection is nil")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("capture Linux supervisor peer credentials: %w", err)
	}
	var credentials *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("read Linux supervisor peer credentials: %w", err)
	}
	if controlErr != nil {
		return nil, fmt.Errorf("read Linux supervisor peer credentials: %w", controlErr)
	}
	if credentials == nil {
		return nil, errors.New("Linux supervisor peer credentials are unavailable")
	}
	return credentials, nil
}

func credentialsPID(credentials *unix.Ucred) int {
	if credentials == nil {
		return 0
	}
	return int(credentials.Pid)
}

func linuxSupervisorEndpoint(ownerContext string) (string, error) {
	marker := "|endpoint="
	index := strings.LastIndex(ownerContext, marker)
	if index < 0 {
		return "", errors.New("Linux supervisor owner context has no recovery endpoint")
	}
	endpoint := strings.TrimSpace(ownerContext[index+len(marker):])
	if endpoint == "" || len(endpoint) >= 108 || filepath.IsAbs(endpoint) == false {
		return "", errors.New("Linux supervisor recovery endpoint is invalid")
	}
	return endpoint, nil
}

func linuxSupervisorValidHexV2(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func linuxSupervisorSecretMatchesV2(secret, digest string) bool {
	computed := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(computed[:]) == digest
}

func linuxSupervisorRequestMatchesV2(request linuxSupervisorRequest, launch linuxSupervisorLaunch, target *LinuxPtraceProcess) bool {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil || request.Version != linuxSupervisorProtocolVersion || request.LaunchToken != launch.LaunchToken || request.TargetPID != target.PID() || request.TargetIdentity != target.Identity() || request.OwnerKind != linuxSupervisorOwnerKind || request.OwnerContext != launch.OwnerContext || request.SupervisorPID != os.Getpid() || request.SupervisorIdentity != identity || request.Token != launch.PipeToken || request.JobID != launch.JobID {
		return false
	}
	return request.Operation != linuxSupervisorOpLaunch && request.Operation != linuxSupervisorOpHello
}

func linuxSupervisorUnboundAbortRequestMatches(request linuxSupervisorRequest, launch linuxSupervisorLaunch, target *LinuxPtraceProcess) bool {
	return request.Version == linuxSupervisorProtocolVersion && request.Operation == linuxSupervisorOpAbort && request.LaunchToken == launch.LaunchToken && request.TargetPID == target.PID() && request.TargetIdentity == target.Identity() && request.OwnerKind == linuxSupervisorOwnerKind && request.OwnerContext == launch.OwnerContext && request.SupervisorPID == 0 && request.SupervisorIdentity == "" && request.Token == launch.PipeToken && request.JobID == launch.JobID && request.Secret != "" && linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest)
}
