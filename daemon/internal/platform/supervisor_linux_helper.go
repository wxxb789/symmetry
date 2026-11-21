//go:build linux

package platform

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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

type linuxSupervisorLaunch struct {
	Version        int      `json:"version"`
	Operation      string   `json:"operation"`
	Program        string   `json:"program"`
	Args           []string `json:"args,omitempty"`
	Env            []string `json:"env,omitempty"`
	Dir            string   `json:"dir,omitempty"`
	OwnerContext   string   `json:"owner_context"`
	LaunchToken    string   `json:"launch_token"`
	PipeToken      string   `json:"pipe_token"`
	JobID          string   `json:"job_id"`
	MirrorIdentity string   `json:"mirror_identity"`
	SecretDigest   string   `json:"secret_digest"`
	Endpoint       string   `json:"endpoint"`
	BootstrapFD    int      `json:"bootstrap_fd"`
	OwnerFD        int      `json:"owner_fd"`
	ControlFD      int      `json:"control_fd"`
	ResponseFD     int      `json:"response_fd"`
	StatusFD       int      `json:"status_fd"`
	MetadataFD     int      `json:"metadata_fd"`
	StdinFD        int      `json:"stdin_fd"`
	StdoutFD       int      `json:"stdout_fd"`
	StderrFD       int      `json:"stderr_fd"`
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

// Uses the inherited full-duplex metadata socket (one per launch); the redundant exact owner
// fields reject stale, cross-launch, or fabricated observations before the Store callback.
type linuxSupervisorScanRequest struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	LaunchToken        string `json:"launch_token"`
	OwnerKind          string `json:"owner_kind"`
	OwnerContext       string `json:"owner_context"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	MirrorIdentity     string `json:"mirror_identity"`
	Owner              string `json:"owner"`
	Sequence           uint64 `json:"sequence"`
	Phase              string `json:"phase"`
	FailureKind        string `json:"failure_kind,omitempty"`
}

type linuxSupervisorScanResponse struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	Status             string `json:"status"`
	LaunchToken        string `json:"launch_token"`
	OwnerKind          string `json:"owner_kind"`
	OwnerContext       string `json:"owner_context"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	MirrorIdentity     string `json:"mirror_identity"`
	Owner              string `json:"owner"`
	Sequence           uint64 `json:"sequence"`
	Phase              string `json:"phase"`
	Error              string `json:"error,omitempty"`
}

// Read only after the recovery endpoint has authenticated the peer and full launch identity.
type linuxSupervisorRecoveryResponse struct {
	linuxSupervisorResponse
	ScanSealSequence uint64 `json:"scan_seal_sequence,omitempty"`
}

// One helper-local lease timer: the callback latches expiry and publishes a wake-up, stop/proof
// stays serialized by the helper loop, and generation fencing makes a stale callback harmless.
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
			// Latch expiry before publishing the wake-up: a renewal racing the timer callback must fail
			// closed even if the helper loop has not consumed the notification yet.
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

// Latches the stopped state before the external stop/proof attempt begins, so a late renewal
// can never re-arm a lease after expiry.
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

// Closes a lease at its monotonic deadline even when the timer callback has not run; the
// wake-up lets the helper loop perform the physical stop/proof.
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

// One linearization point for resume/detach and owner loss: whichever acquires this lock first
// wins, and the other path fails closed without touching ptrace state.
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

func linuxSupervisorUnixConn(file *os.File) (*net.UnixConn, error) {
	if file == nil {
		return nil, errors.New("Linux supervisor metadata file is nil")
	}
	connection, err := net.FileConn(file)
	if err != nil {
		return nil, fmt.Errorf("duplicate Linux supervisor metadata connection: %w", err)
	}
	_ = file.Close()
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("Linux supervisor metadata connection is not a Unix socket")
	}
	return unixConnection, nil
}

func reportLinuxSupervisorScanEvent(connection *net.UnixConn, reader *bufio.Reader, launch linuxSupervisorLaunch, target *LinuxPtraceProcess, helperIdentity string, event descendantScanEvent) error {
	if connection == nil || reader == nil || target == nil || helperIdentity == "" {
		return errors.New("Linux supervisor scan channel is unavailable")
	}
	if err := connection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
		return fmt.Errorf("set Linux supervisor scan deadline: %w", err)
	}
	defer connection.SetDeadline(time.Time{})
	request := linuxSupervisorScanRequest{
		Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpScan,
		LaunchToken: launch.LaunchToken, OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext,
		TargetPID: target.PID(), TargetIdentity: target.Identity(), SupervisorPID: os.Getpid(),
		SupervisorIdentity: helperIdentity, PipeToken: launch.PipeToken, JobID: launch.JobID,
		MirrorIdentity: launch.MirrorIdentity,
		Owner:          authority.SupervisorScanOwnerHelper, Sequence: event.Sequence, Phase: event.Phase,
		FailureKind: event.FailureKind,
	}
	if err := writeLinuxSupervisorFrame(connection, request); err != nil {
		return fmt.Errorf("send Linux supervisor scan observation: %w", err)
	}
	var response linuxSupervisorScanResponse
	if err := readLinuxSupervisorFrame(reader, &response); err != nil {
		return fmt.Errorf("read Linux supervisor scan acknowledgement: %w", err)
	}
	if response.Version != request.Version || response.Operation != request.Operation || response.LaunchToken != request.LaunchToken ||
		response.OwnerKind != request.OwnerKind || response.OwnerContext != request.OwnerContext ||
		response.TargetPID != request.TargetPID || response.TargetIdentity != request.TargetIdentity ||
		response.SupervisorPID != request.SupervisorPID || response.SupervisorIdentity != request.SupervisorIdentity ||
		response.PipeToken != request.PipeToken || response.JobID != request.JobID || response.MirrorIdentity != request.MirrorIdentity || response.Owner != request.Owner ||
		response.Sequence != request.Sequence || response.Phase != request.Phase {
		return errors.New("Linux supervisor scan acknowledgement identity mismatch")
	}
	if response.Status != "acknowledged" {
		if response.Error == "" {
			return errors.New("Linux supervisor scan observation was not acknowledged")
		}
		return errors.New(response.Error)
	}
	return nil
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
	// Linux limits SCM_RIGHTS to 253 descriptors per message. Receive the complete bounded
	// control payload so every unexpected descriptor can be closed before rejecting the helper response.
	oob := make([]byte, unix.CmsgSpace(253*4))
	n, oobn, flags, _, err := unix.Recvmsg(int(file.Fd()), data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return -1, err
	}
	receivedFD := -1
	closeReceivedFD := func() {
		if receivedFD >= 0 {
			_ = unix.Close(receivedFD)
			receivedFD = -1
		}
	}
	var receiveErr error
	if n != len(data) {
		receiveErr = errors.New("Linux supervisor fd payload is incomplete")
	}
	if flags&unix.MSG_CTRUNC != 0 && receiveErr == nil {
		receiveErr = errors.New("Linux supervisor fd control message is truncated")
	}
	messages, parseErr := unix.ParseSocketControlMessage(oob[:oobn])
	if parseErr != nil && receiveErr == nil {
		receiveErr = parseErr
	}
	fdCount := 0
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			if receiveErr == nil {
				receiveErr = errors.New("Linux supervisor fd control message is unexpected")
			}
			continue
		}
		fds, rightsErr := unix.ParseUnixRights(&message)
		if rightsErr != nil {
			if receiveErr == nil {
				receiveErr = rightsErr
			}
			continue
		}
		for _, fd := range fds {
			fdCount++
			if fdCount == 1 {
				receivedFD = fd
			} else {
				_ = unix.Close(fd)
			}
		}
	}
	if receiveErr != nil {
		closeReceivedFD()
		return -1, receiveErr
	}
	if fdCount != 1 {
		closeReceivedFD()
		return -1, errors.New("Linux supervisor pidfd transfer did not contain exactly one descriptor")
	}
	return receivedFD, nil
}

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
	helloInjection, injectWrongHelloSession, err := linuxSupervisorHelloSessionInjectionFromEnv()
	if err != nil {
		return err
	}
	if err := validateLinuxSupervisorOwnerLossProofBarrierEnv(); err != nil {
		return err
	}
	ownerRecoveryMarker, _, err := linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv()
	if err != nil {
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
	// Go's exec_linux child path preserves inherited fd >= 3 unless marked close-on-exec, so never
	// expose authority/control pipes to the target command.
	for _, file := range files[:6] {
		unix.CloseOnExec(int(file.Fd()))
	}
	var launch linuxSupervisorLaunch
	if err := readLinuxSupervisorFrame(bufio.NewReader(files[0]), &launch); err != nil {
		return err
	}
	if launch.Version != linuxSupervisorProtocolVersion || launch.Operation != linuxSupervisorOpLaunch || launch.OwnerContext == "" || launch.Program == "" || !linuxSupervisorValidHexV2(launch.LaunchToken, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.PipeToken, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.JobID, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.MirrorIdentity, authority.TokenBytes*2) || !linuxSupervisorValidHexV2(launch.SecretDigest, sha256.Size*2) {
		return errors.New("Linux supervisor launch is invalid")
	}
	if strings.TrimSpace(launch.Endpoint) == "" || !strings.Contains(launch.OwnerContext, "|endpoint=") {
		return errors.New("Linux supervisor recovery endpoint is invalid")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: launch.Endpoint, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen Linux supervisor recovery endpoint: %w", sanitizeLinuxSupervisorEndpointError(err))
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
	containment, identity, attachErr := attachProcessWithoutMonitor(command.Process)
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
	restoreScanFailureInjection := installLinuxSupervisorScanFailureInjectionAfterInitial(group, target.PID(), identity)
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
	metadataConn, err := linuxSupervisorUnixConn(files[5])
	if err != nil {
		_ = group.Terminate(true)
		_ = group.Close()
		_ = target.Terminate()
		_, _ = target.Wait()
		return err
	}
	defer metadataConn.Close()
	metadataReader := bufio.NewReader(metadataConn)
	helperIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	if err := group.setDescendantScanObserver(func(event descendantScanEvent) error {
		return reportLinuxSupervisorScanEvent(metadataConn, metadataReader, launch, target, helperIdentity, event)
	}); err != nil {
		return err
	}
	if err := group.pauseDescendantMonitoringUntilResume(); err != nil {
		return err
	}
	preHelloAborted := false
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
		deadline := containmentDeadline(context.Background())
		if preHelloAborted {
			return nil
		}
		terminateErr := target.Terminate()
		if terminateErr != nil {
			if retryErr := target.Terminate(); retryErr != nil {
				return errors.Join(terminateErr, retryErr, group.closePreparedAbort(deadline))
			}
		}
		if err := group.Terminate(true); err != nil && !errors.Is(err, unix.ESRCH) {
			return err
		}
		group.mutex.Lock()
		group.skipNextCloseSignal = true
		group.mutex.Unlock()
		preHelloStartWait()
		if err := waitForLinuxSupervisorTarget(preHelloWaitDone, deadline); err != nil {
			return err
		}
		if err := reapLinuxProcessGroupChildren(group.anchor, deadline); err != nil {
			return err
		}
		if err := group.closePreparedAbort(deadline); err != nil {
			return err
		}
		preHelloAborted = true
		return nil
	}
	hello := linuxSupervisorHello{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpHello, LaunchToken: launch.LaunchToken, OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, TargetPID: target.PID(), TargetIdentity: target.Identity(), TargetPGRP: anchor.PGRP, TargetSession: anchor.Session, TargetStartTime: anchor.StartTime, SupervisorPID: os.Getpid(), SupervisorIdentity: helperIdentity, PipeToken: launch.PipeToken, JobID: launch.JobID}
	actualHello := hello
	if injectWrongHelloSession {
		wrongSession, err := wrongLinuxSupervisorHelloSession(anchor)
		if err != nil {
			if stopErr := preHelloStop(); stopErr != nil {
				return errors.Join(err, newLinuxSupervisorPreHelloRecoverySession(listener, launch, target, preHelloStop, ownerRecoveryMarker).runRecoveryLoop())
			}
			return err
		}
		hello.TargetSession = wrongSession
	}
	if err := writeLinuxSupervisorFrame(files[3], hello); err != nil {
		if stopErr := preHelloStop(); stopErr != nil {
			return errors.Join(err, newLinuxSupervisorPreHelloRecoverySession(listener, launch, target, preHelloStop, ownerRecoveryMarker).runRecoveryLoop())
		}
		return err
	}
	if injectWrongHelloSession {
		if err := writeLinuxSupervisorHelloSessionWitness(helloInjection.firedPath, actualHello, hello); err != nil {
			if stopErr := preHelloStop(); stopErr != nil {
				return errors.Join(err, newLinuxSupervisorPreHelloRecoverySession(listener, launch, target, preHelloStop, ownerRecoveryMarker).runRecoveryLoop())
			}
			return fmt.Errorf("write Linux supervisor wrong-session hello witness: %w", err)
		}
		if helloInjection.releasePath != "" {
			if err := waitForLinuxSupervisorHelloSessionRelease(files[1], helloInjection.releasePath); err != nil {
				if stopErr := preHelloStop(); stopErr != nil {
					return errors.Join(err, newLinuxSupervisorPreHelloRecoverySession(listener, launch, target, preHelloStop, ownerRecoveryMarker).runRecoveryLoop())
				}
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
	}
	return newLinuxSupervisorSession(launch, listener, target, group, files[1], files[2], files[3], files[4], files[6], files[7], files[8], ownerRecoveryMarker).runHelperLoop()
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

// The status writer is part of waitDone so a blocked status pipe cannot be mistaken for a
// proven stop; callers retain the recovery authority when this bounded wait expires.
func waitForLinuxSupervisorTarget(waitDone <-chan struct{}, deadline time.Time) error {
	if waitDone == nil {
		return fmt.Errorf("%w: Linux supervisor target wait is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return fmt.Errorf("wait for Linux supervisor target: %w", errors.Join(ErrLinuxSupervisorStopUnproven, context.DeadlineExceeded))
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-waitDone:
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for Linux supervisor target: %w", errors.Join(ErrLinuxSupervisorStopUnproven, context.DeadlineExceeded))
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("wait for Linux supervisor target: %w", errors.Join(ErrLinuxSupervisorStopUnproven, context.DeadlineExceeded))
	}
}
