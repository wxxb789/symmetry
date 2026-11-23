//go:build linux

package platform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

// Supplied by the caller that still owns the daemon/store lifetime. Must return nil only
// after the old writer is quiesced.
type SupervisorHandoffAbortVerifier func() error

// Consumed by execution's Linux launcher.  The target command is serialized over a private
// inherited pipe; it never enters helper argv or environment.
type LinuxSupervisorStartSpec struct {
	Command                *exec.Cmd
	HelperCommand          *exec.Cmd
	HelperPath             string
	HelperArgs             []string
	OwnerContext           string
	Prepare                func(authority.SupervisorHandoff) error
	Bind                   func(authority.SupervisorHandoff, int, string) error
	RecordRecoveryScanSeal func(authority.Supervisor, uint64) error
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

	ownerWrite               *os.File
	controlWrite             *os.File
	responseRead             *os.File
	responseReader           *bufio.Reader
	statusRead               *os.File
	statusReader             *bufio.Reader
	scanConn                 *net.UnixConn
	scanEventsDone           chan struct{}
	scanPrepare              func(authority.SupervisorHandoff) error
	recoveryScanSealWrite    func(authority.Supervisor, uint64) error
	mirrorIdentity           string
	mirrorScanBarrierRestore func()
	pidfd                    int
	// Owns the duplicated pidfd; mirror may take over only after the exact helper has exited.
	mirror *processGroup
	// Mirror closed its pidfd; only the identity-bound read-only group proof remains retryable.
	finalProofPending bool

	requestMu     sync.Mutex
	stopMu        sync.Mutex
	statusMu      sync.Mutex
	leaseResumeMu sync.Mutex
	// Places helper exit at the control-write boundary and returns the resulting real pipe error.
	writeControlFrameForTesting func(*os.File, linuxSupervisorRequest) error

	committed             bool
	resumed               bool
	released              bool
	closed                bool
	receipt               *authority.StopReceipt
	closeErr              error
	responseDropTriggered bool
	responseTransportLost bool
	responseLossPending   bool
	// mirrorSealWitnessScanTriggered is a one-shot pre-resume production-witness latch.
	mirrorSealWitnessScanTriggered bool
	waitCode                       int
	waitErr                        error
	waitDone                       chan struct{}

	containmentUnprovenCallback func() error
	prepareUnknown              bool
	bindUnknown                 bool
}

// Starts the helper first; it launches the ptrace-stopped target and waits for
// prepare/bind/commit before allowing user code to run.
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
	if spec.Prepare == nil || spec.Bind == nil {
		return nil, errors.New("Linux supervisor requires durable prepare and bind callbacks")
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
	mirrorIdentity, err := linuxSupervisorRandomHex(authority.TokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate Linux supervisor mirror identity: %w", err)
	}
	secretDigest := sha256.Sum256([]byte(secret))
	endpoint, err := linuxSupervisorEndpointPath(jobID, os.TempDir(), "/tmp")
	if err != nil {
		return nil, err
	}
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
	metadataPair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
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
		Version:        linuxSupervisorProtocolVersion,
		Operation:      linuxSupervisorOpLaunch,
		Program:        spec.Command.Path,
		Args:           append([]string(nil), spec.Command.Args[1:]...),
		Env:            append([]string(nil), spec.Command.Env...),
		Dir:            spec.Command.Dir,
		OwnerContext:   ownerContext,
		LaunchToken:    launchToken,
		PipeToken:      pipeToken,
		JobID:          jobID,
		MirrorIdentity: mirrorIdentity,
		SecretDigest:   hex.EncodeToString(secretDigest[:]),
		Endpoint:       endpoint,
		BootstrapFD:    3,
		OwnerFD:        4,
		ControlFD:      5,
		ResponseFD:     6,
		StatusFD:       7,
		MetadataFD:     8,
		StdinFD:        9,
		StdoutFD:       10,
		StderrFD:       11,
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
	metadataConn, err := linuxSupervisorUnixConn(metadataDaemon)
	if err != nil {
		_ = unix.Close(pidfd)
		_ = help.Process.Kill()
		_, _ = help.Process.Wait()
		closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead, metadataDaemon)
		return nil, err
	}
	mirror := &processGroup{pid: hello.TargetPID, fd: pidfd, anchor: anchor.private(), anchorCaptured: true}
	supervisor := &linuxSupervisor{
		authority: value, handoff: handoff, anchor: anchor, helper: help.Process, helperIdentity: helperIdentity,
		helperDone: make(chan struct{}), ownerWrite: ownerWrite, controlWrite: controlWrite,
		responseRead: responseRead, responseReader: bufio.NewReader(responseRead),
		statusRead: statusRead, statusReader: bufio.NewReader(statusRead), scanConn: metadataConn,
		scanPrepare: spec.Prepare, recoveryScanSealWrite: spec.RecordRecoveryScanSeal,
		mirrorIdentity: mirrorIdentity, pidfd: pidfd, mirror: mirror,
	}
	go supervisor.waitHelper()

	preparedHandoff := handoff.Clone()
	preparedHandoff.SupervisorPID = 0
	preparedHandoff.SupervisorIdentity = ""
	if err := callLinuxSupervisorPrepare(spec.Prepare, preparedHandoff); err != nil {
		supervisor.prepareUnknown = true
		_ = supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline))
		return supervisor, fmt.Errorf("prepare Linux supervisor handoff: %w", err)
	}
	bindRequest := preparedHandoff.Clone()
	bindRequest.ScanUpdate = &authority.SupervisorScanUpdate{
		MirrorIdentity: mirrorIdentity, Owner: authority.SupervisorScanOwnerMirror,
		Sequence: 1, Phase: authority.SupervisorScanPhaseInitialize,
	}
	if err := callLinuxSupervisorBind(spec.Bind, bindRequest, handoff.SupervisorPID, handoff.SupervisorIdentity); err != nil {
		supervisor.bindUnknown = true
		_ = supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline))
		return supervisor, fmt.Errorf("bind Linux supervisor handoff: %w", err)
	}
	if err := supervisor.startScanEventServer(metadataConn, spec.Prepare); err != nil {
		_ = supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline))
		return supervisor, fmt.Errorf("start Linux supervisor scan event server: %w", err)
	}
	if err := mirror.pauseDescendantMonitoringUntilResume(); err != nil {
		_ = supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline))
		return supervisor, fmt.Errorf("pause Linux supervisor mirror monitoring before resume: %w", err)
	}
	if err := mirror.setDescendantScanObserver(supervisor.persistMirrorScanEvent); err != nil {
		_ = supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline))
		return supervisor, fmt.Errorf("install Linux supervisor mirror scan observer: %w", err)
	}
	supervisor.mirrorScanBarrierRestore = installLinuxSupervisorCleanScanBarrier(mirror, hello.TargetPID, hello.TargetIdentity, authority.SupervisorScanOwnerMirror)
	mirror.startDescendantMonitor()
	if err := mirror.waitForInitialDescendantScan(time.Now().Add(containmentCloseDeadline)); err != nil {
		return supervisor, fmt.Errorf("observe Linux supervisor mirror containment: %w", err)
	}
	if _, err := supervisor.request(linuxSupervisorOpBind, 0, 0); err != nil {
		return supervisor, fmt.Errorf("bind Linux supervisor helper: %w", err)
	}
	return supervisor, nil
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

func (supervisor *linuxSupervisor) startScanEventServer(connection *net.UnixConn, prepare func(authority.SupervisorHandoff) error) error {
	if supervisor == nil || connection == nil || prepare == nil {
		return errors.New("Linux supervisor scan event server dependencies are incomplete")
	}
	done := make(chan struct{})
	supervisor.mu.Lock()
	if (supervisor.scanConn != nil && supervisor.scanConn != connection) || supervisor.scanEventsDone != nil {
		supervisor.mu.Unlock()
		return errors.New("Linux supervisor scan event server is already running")
	}
	supervisor.scanConn = connection
	supervisor.scanEventsDone = done
	supervisor.scanPrepare = prepare
	supervisor.mu.Unlock()
	go supervisor.serveScanEvents(connection, done, prepare)
	return nil
}

func (supervisor *linuxSupervisor) serveScanEvents(connection *net.UnixConn, done chan struct{}, prepare func(authority.SupervisorHandoff) error) {
	defer close(done)
	reader := bufio.NewReader(connection)
	for {
		var request linuxSupervisorScanRequest
		if err := readLinuxSupervisorFrame(reader, &request); err != nil {
			return
		}
		response := linuxSupervisorScanResponse{
			Version: request.Version, Operation: request.Operation, Status: "rejected",
			LaunchToken: request.LaunchToken, OwnerKind: request.OwnerKind, OwnerContext: request.OwnerContext,
			TargetPID: request.TargetPID, TargetIdentity: request.TargetIdentity,
			SupervisorPID: request.SupervisorPID, SupervisorIdentity: request.SupervisorIdentity,
			PipeToken: request.PipeToken, JobID: request.JobID, MirrorIdentity: request.MirrorIdentity, Owner: request.Owner,
			Sequence: request.Sequence, Phase: request.Phase,
		}
		if err := supervisor.persistHelperScanRequest(request, prepare); err != nil {
			response.Error = err.Error()
		} else {
			response.Status = "acknowledged"
		}
		if err := connection.SetWriteDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
			return
		}
		writeErr := writeLinuxSupervisorFrame(connection, response)
		_ = connection.SetWriteDeadline(time.Time{})
		if writeErr != nil {
			return
		}
	}
}

func (supervisor *linuxSupervisor) persistHelperScanRequest(request linuxSupervisorScanRequest, prepare func(authority.SupervisorHandoff) error) error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	value := supervisor.authority.Clone()
	handoff := supervisor.handoff.Clone()
	expectedMirrorIdentity := supervisor.mirrorIdentity
	supervisor.mu.Unlock()
	if request.Version != linuxSupervisorProtocolVersion || request.Operation != linuxSupervisorOpScan ||
		request.OwnerKind != value.OwnerKind || request.OwnerContext != value.OwnerContext ||
		request.LaunchToken != value.LaunchToken || request.TargetPID != value.TargetPID || request.TargetIdentity != value.TargetIdentity ||
		request.SupervisorPID != value.SupervisorPID || request.SupervisorIdentity != value.SupervisorIdentity ||
		request.PipeToken != value.PipeToken || request.JobID != value.JobID || request.MirrorIdentity != expectedMirrorIdentity ||
		request.Owner != authority.SupervisorScanOwnerHelper || request.Sequence == 0 {
		return errors.New("Linux supervisor scan observation identity mismatch")
	}
	handoff.SupervisorPID = 0
	handoff.SupervisorIdentity = ""
	handoff.ScanUpdate = &authority.SupervisorScanUpdate{
		SupervisorPID: request.SupervisorPID, SupervisorIdentity: request.SupervisorIdentity,
		MirrorIdentity: request.MirrorIdentity,
		Owner:          request.Owner, Sequence: request.Sequence, Phase: request.Phase, FailureKind: request.FailureKind,
	}
	if err := handoff.Validate(); err != nil {
		return fmt.Errorf("validate Linux supervisor scan observation: %w", err)
	}
	return callLinuxSupervisorPrepare(prepare, handoff)
}

func (supervisor *linuxSupervisor) persistMirrorScanEvent(event descendantScanEvent) error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	handoff := supervisor.handoff.Clone()
	prepare := supervisor.scanPrepare
	helperPID := supervisor.authority.SupervisorPID
	helperIdentity := supervisor.authority.SupervisorIdentity
	mirrorIdentity := supervisor.mirrorIdentity
	supervisor.mu.Unlock()
	if prepare == nil {
		return errors.New("Linux supervisor scan persistence callback is unavailable")
	}
	handoff.SupervisorPID = 0
	handoff.SupervisorIdentity = ""
	handoff.ScanUpdate = &authority.SupervisorScanUpdate{
		SupervisorPID: helperPID, SupervisorIdentity: helperIdentity,
		MirrorIdentity: mirrorIdentity,
		Owner:          authority.SupervisorScanOwnerMirror, Sequence: event.Sequence,
		Phase: event.Phase, FailureKind: event.FailureKind,
	}
	if err := handoff.Validate(); err != nil {
		return fmt.Errorf("validate Linux mirror scan update: %w", err)
	}
	return callLinuxSupervisorPrepare(prepare, handoff)
}

func (supervisor *linuxSupervisor) assertDurableScanStateClean() error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror == nil {
		return fmt.Errorf("%w: daemon mirror is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	mirror.mutex.Lock()
	sequence := mirror.descendantScanSequence
	clean := sequence > 0 && sequence == mirror.descendantScanCompletedSequence && !mirror.descendantScanInFlight &&
		mirror.descendantScanCompleted && !mirror.descendantScanLost && mirror.escapedDescendant == nil
	mirror.mutex.Unlock()
	if !clean {
		return fmt.Errorf("%w: daemon mirror has no clean scan sequence", ErrLinuxSupervisorStopUnproven)
	}
	return supervisor.persistMirrorScanEvent(descendantScanEvent{Sequence: sequence, Phase: authority.SupervisorScanPhaseAssertClean})
}

func (supervisor *linuxSupervisor) assertDurableScanStateSealed() error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror == nil {
		return fmt.Errorf("%w: daemon mirror is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	mirror.mutex.Lock()
	sequence := mirror.descendantScanSequence
	sealed := mirror.descendantScanSealed && sequence > 0 && sequence == mirror.descendantScanCompletedSequence &&
		!mirror.descendantScanInFlight && mirror.descendantScanCompleted && !mirror.descendantScanLost && mirror.escapedDescendant == nil
	mirror.mutex.Unlock()
	if !sealed {
		return fmt.Errorf("%w: daemon mirror has no exact clean scan seal", ErrLinuxSupervisorStopUnproven)
	}
	return supervisor.persistMirrorScanEvent(descendantScanEvent{Sequence: sequence, Phase: authority.SupervisorScanPhaseAssertAllSealed})
}

func (supervisor *linuxSupervisor) waitForScanEvents(deadline time.Time) error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	done := supervisor.scanEventsDone
	supervisor.mu.Unlock()
	if done == nil {
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return fmt.Errorf("wait for Linux supervisor scan events: %w", context.DeadlineExceeded)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("wait for Linux supervisor scan events: %w", context.DeadlineExceeded)
	}
}

func (supervisor *linuxSupervisor) closeScanTransport(deadline time.Time) error {
	if supervisor == nil {
		return nil
	}
	supervisor.mu.Lock()
	connection := supervisor.scanConn
	supervisor.scanConn = nil
	supervisor.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
	return supervisor.waitForScanEvents(deadline)
}

func (supervisor *linuxSupervisor) restoreMirrorScanBarrier() {
	if supervisor == nil {
		return
	}
	supervisor.mu.Lock()
	restore := supervisor.mirrorScanBarrierRestore
	supervisor.mirrorScanBarrierRestore = nil
	supervisor.mu.Unlock()
	if restore != nil {
		restore()
	}
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
	supervisor.leaseResumeMu.Lock()
	defer supervisor.leaseResumeMu.Unlock()
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
	if err := supervisor.verifyMirrorObservation(); err != nil {
		return fmt.Errorf("verify Linux supervisor observations before resume: %w", err)
	}
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror == nil {
		return fmt.Errorf("%w: daemon mirror is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	response, err := supervisor.request(linuxSupervisorOpResume, 0, 0)
	if err != nil {
		return err
	}
	if response.Status != "resumed" {
		return fmt.Errorf("Linux supervisor resume returned %q", response.Status)
	}
	if err := mirror.resumeDescendantMonitoring(); err != nil {
		return fmt.Errorf("resume Linux supervisor mirror monitoring: %w", err)
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
	supervisor.stopMu.Lock()
	defer supervisor.stopMu.Unlock()
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
		if errors.Is(err, ErrLinuxSupervisorResponseLost) || errors.Is(err, errLinuxSupervisorControlWriteLost) {
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
	if err := supervisor.freezeMirrorObservation(); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: freeze daemon mirror observation: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	receipt := supervisor.receiptFromResponse(response)
	supervisor.mu.Lock()
	supervisor.receipt = &receipt
	supervisor.authority.StopReceipt = &receipt
	supervisor.handoff.StopReceipt = &receipt
	supervisor.pidfd = -1
	supervisor.mu.Unlock()
	return nil
}

// The normal response pipe may have been closed after the helper applied the request, so later
// Terminate/Close retries must retain this typed recovery boundary instead of degrading to helper-exited.
func (supervisor *linuxSupervisor) recoverResponseLossStop() error {
	if supervisor == nil {
		return ErrLinuxSupervisorHelperExited
	}
	value := supervisor.ContainmentAuthority()
	if value == nil {
		return fmt.Errorf("%w: Linux supervisor authority is unavailable", ErrLinuxSupervisorStopUnproven)
	}
	// Keep response-loss sticky even after recovery succeeds: the normal response pipe is permanently
	// untrustworthy and must never be used by a later lifecycle operation.
	supervisor.requestMu.Lock()
	supervisor.closeControlAfterResponseLossRequestLocked()
	supervisor.requestMu.Unlock()
	deadline := time.Now().Add(containmentCloseDeadline)
	freezeUnproven := func(err error) error {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("response-loss mirror freeze: %w", err)
	}
	var lastErr error
	for {
		if channelClosedV2(supervisor.helperDone) {
			return supervisor.stopWithMirror()
		}
		receipt, sequence, source, err := stopPersistedLinuxSupervisor(*value)
		// The helper may die while the recovery endpoint is contacted. A receipt reconstructed from
		// endpoint absence is not a healthy-helper stop response; re-enter mirror takeover for exact proof.
		if source == linuxSupervisorStopReceiptFromAbsenceProof || channelClosedV2(supervisor.helperDone) {
			return supervisor.stopWithMirror()
		}
		retry := false
		if err == nil {
			if !receipt.ValidFor(*value) {
				lastErr = errors.New("response-loss recovery returned invalid stop receipt")
			} else if mirrorErr := supervisor.verifyMirrorObservation(); mirrorErr != nil {
				if supervisor.containmentUnprovenCallback != nil {
					_ = supervisor.containmentUnprovenCallback()
				}
				lastErr = fmt.Errorf("response-loss mirror proof: %w", mirrorErr)
			} else if mirrorErr := supervisor.sealMirrorObservation(); mirrorErr != nil {
				lastErr = freezeUnproven(mirrorErr)
			} else if mirrorErr := supervisor.recordRecoveryScanSeal(sequence); mirrorErr != nil {
				lastErr = freezeUnproven(mirrorErr)
			} else if mirrorErr := supervisor.assertDurableScanStateSealed(); mirrorErr != nil {
				lastErr = freezeUnproven(mirrorErr)
			} else {
				supervisor.mu.Lock()
				supervisor.receipt = &receipt
				supervisor.authority.StopReceipt = &receipt
				supervisor.handoff.StopReceipt = &receipt
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
	deadline := containmentDeadline(context.Background())
	if err := supervisor.waitForExactHelperDeath(deadline); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: helper death fence: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	if mirror == nil {
		return ErrLinuxSupervisorStopUnproven
	}
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
	supervisor.restoreMirrorScanBarrier()
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
	if err := supervisor.waitForScanEvents(deadline); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: helper scan observations did not drain: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	if err := supervisor.closeScanTransport(deadline); err != nil {
		return fmt.Errorf("%w: close drained helper scan channel: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	if err := supervisor.assertDurableScanStateSealed(); err != nil {
		if supervisor.containmentUnprovenCallback != nil {
			_ = supervisor.containmentUnprovenCallback()
		}
		return fmt.Errorf("%w: mirror takeover scan seals: %w", ErrLinuxSupervisorStopUnproven, err)
	}
	receipt := supervisor.receiptFromResponse(linuxSupervisorResponse{Status: "stopped", ActiveProcesses: 0})
	supervisor.mu.Lock()
	supervisor.receipt = &receipt
	supervisor.authority.StopReceipt = &receipt
	supervisor.handoff.StopReceipt = &receipt
	supervisor.finalProofPending = false
	supervisor.mu.Unlock()
	return nil
}

// A closed wait channel or a failed control write alone is not enough to authorize mirror takeover.
func (supervisor *linuxSupervisor) waitForExactHelperDeath(deadline time.Time) error {
	if supervisor == nil || supervisor.helperDone == nil {
		return errors.New("Linux supervisor helper wait is unavailable")
	}
	if supervisor.authority.SupervisorPID <= 0 || supervisor.authority.SupervisorIdentity == "" {
		return errors.New("Linux supervisor helper identity is unavailable")
	}
	if !channelClosedV2(supervisor.helperDone) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("wait for exact Linux supervisor helper exit: %w", context.DeadlineExceeded)
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-supervisor.helperDone:
		case <-timer.C:
			return fmt.Errorf("wait for exact Linux supervisor helper exit: %w", context.DeadlineExceeded)
		}
	}
	actualIdentity, err := linuxSupervisorProcessIdentity(supervisor.authority.SupervisorPID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read exact Linux supervisor helper identity after wait: %w", err)
	}
	if actualIdentity != supervisor.authority.SupervisorIdentity {
		return fmt.Errorf("Linux supervisor helper identity mismatch after wait: got %q, want %q", actualIdentity, supervisor.authority.SupervisorIdentity)
	}
	return fmt.Errorf("exact Linux supervisor helper %d remains alive after wait", supervisor.authority.SupervisorPID)
}

// Tolerates only the short interval in which a killed process group is still visible to getpriority;
// identity, namespace, permission, and all other proof failures remain fail-closed.
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
		supervisor.requestMu.Lock()
		supervisor.closeControlAfterResponseLossRequestLocked()
		supervisor.requestMu.Unlock()
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
		if errors.Is(err, ErrLinuxSupervisorResponseLost) || errors.Is(err, errLinuxSupervisorControlWriteLost) {
			if barrierErr := supervisor.waitForLinuxSupervisorReleaseResponseLossBarrier(); barrierErr != nil {
				return barrierErr
			}
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
	if err := supervisor.closeScanTransport(time.Now().Add(containmentCloseDeadline)); err != nil {
		return fmt.Errorf("close Linux supervisor scan channel: %w", err)
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
	lost := mirror.descendantScanLost || mirror.escapedDescendant != nil
	completed := mirror.descendantScanCompleted
	sequence := mirror.descendantScanSequence
	completedSequence := mirror.descendantScanCompletedSequence
	inFlight := mirror.descendantScanInFlight
	mirror.mutex.Unlock()
	if lost {
		return fmt.Errorf("%w: mirror observed descendant escape or scan loss", ErrLinuxDescendantContainmentUnproven)
	}
	if !completed || sequence == 0 || sequence != completedSequence || inFlight {
		return fmt.Errorf("%w: mirror has no completed descendant observation", ErrLinuxDescendantContainmentUnproven)
	}
	return supervisor.assertDurableScanStateClean()
}

func (supervisor *linuxSupervisor) freezeMirrorObservation() error {
	if err := supervisor.sealMirrorObservation(); err != nil {
		return err
	}
	return supervisor.assertDurableScanStateSealed()
}

func (supervisor *linuxSupervisor) sealMirrorObservation() error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror == nil {
		return fmt.Errorf("%w: daemon mirror is unavailable", ErrLinuxDescendantContainmentUnproven)
	}
	if mirror.monitorStop != nil && !channelClosedV2(mirror.monitorDone) {
		if err := mirror.stopDescendantMonitor(time.Now().Add(containmentCloseDeadline)); err != nil {
			return err
		}
	}
	supervisor.restoreMirrorScanBarrier()
	if err := mirror.sealDescendantScanAfterMonitorStop(); err != nil {
		return err
	}
	if err := mirror.assertDescendantScanSealedLocally(); err != nil {
		return err
	}
	return nil
}

// An absent write keeps the base fail-closed path: the all-sealed assertion refuses.
func (supervisor *linuxSupervisor) recordRecoveryScanSeal(sequence uint64) error {
	if supervisor == nil {
		return errors.New("Linux supervisor is unavailable")
	}
	if supervisor.recoveryScanSealWrite == nil {
		return nil
	}
	return supervisor.recoveryScanSealWrite(*supervisor.ContainmentAuthority(), sequence)
}

// The only local release path: a durable receipt proves the target tree stopped, and the helperDone
// fence proves the exact helper exited, so no request may be sent over its dead pipes.
func (supervisor *linuxSupervisor) releaseAfterHelperDeath(mirror *processGroup) error {
	if supervisor == nil || !channelClosedV2(supervisor.helperDone) {
		return ErrLinuxSupervisorHelperExited
	}
	if actual, err := ProcessIdentity(supervisor.authority.SupervisorPID); err == nil {
		return fmt.Errorf("release Linux supervisor helper: exact helper is still alive as %q", actual)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release Linux supervisor helper: prove exact helper exit: %w", err)
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	if err := supervisor.waitForScanEvents(deadline); err != nil {
		return fmt.Errorf("release Linux supervisor with undrained scan events: %w", err)
	}
	if err := provePersistedLinuxSupervisorRelease(supervisor.authority); err != nil {
		return fmt.Errorf("release Linux supervisor physical proof: %w", err)
	}
	if mirror != nil {
		if err := releaseProcessGroupOwner(mirror); err != nil {
			return fmt.Errorf("release Linux supervisor mirror after helper death: %w", err)
		}
	}
	if err := supervisor.closeScanTransport(deadline); err != nil {
		return fmt.Errorf("close Linux supervisor scan channel after helper death: %w", err)
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
	supervisor.mu.Lock()
	precommit := !supervisor.committed && !supervisor.resumed
	released := supervisor.released
	supervisor.mu.Unlock()
	if released {
		return nil
	}
	if precommit {
		return supervisor.abortPreparedContainment()
	}
	if err := supervisor.stop(linuxSupervisorOpAbort); err != nil {
		return err
	}
	return supervisor.ReleaseContainment()
}

func (supervisor *linuxSupervisor) abortPreparedContainment() error {
	if supervisor == nil {
		return nil
	}
	supervisor.stopMu.Lock()
	defer supervisor.stopMu.Unlock()
	response, err := supervisor.request(linuxSupervisorOpAbort, 0, 0)
	if err != nil {
		return fmt.Errorf("abort prepared Linux supervisor: %w", err)
	}
	if response.Status != "aborted" || response.ActiveProcesses != 0 {
		return fmt.Errorf("abort prepared Linux supervisor returned %q with %d active processes", response.Status, response.ActiveProcesses)
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	if supervisor.helperDone != nil {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("wait for aborted Linux supervisor helper: %w", context.DeadlineExceeded)
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-supervisor.helperDone:
		case <-timer.C:
			return fmt.Errorf("wait for aborted Linux supervisor helper: %w", context.DeadlineExceeded)
		}
	}
	if err := supervisor.waitForScanEvents(deadline); err != nil {
		return fmt.Errorf("drain prepared Linux supervisor scan events: %w", err)
	}
	supervisor.mu.Lock()
	mirror := supervisor.mirror
	handoff := supervisor.handoff.Clone()
	supervisor.mu.Unlock()
	if mirror != nil {
		if err := mirror.closePreparedAbort(deadline); err != nil {
			return fmt.Errorf("stop prepared Linux supervisor mirror: %w", err)
		}
	}
	if err := waitForLinuxSupervisorAbortAbsence(handoff); err != nil {
		return err
	}
	if err := supervisor.closeScanTransport(deadline); err != nil {
		return fmt.Errorf("close prepared Linux supervisor scan transport: %w", err)
	}
	supervisor.restoreMirrorScanBarrier()

	supervisor.mu.Lock()
	if supervisor.released {
		supervisor.mu.Unlock()
		return nil
	}
	fd := supervisor.pidfd
	supervisor.pidfd = -1
	supervisor.released = true
	supervisor.closed = true
	ownerWrite, controlWrite, responseRead, statusRead := supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead
	supervisor.ownerWrite, supervisor.controlWrite, supervisor.responseRead, supervisor.statusRead = nil, nil, nil, nil
	supervisor.mu.Unlock()
	var closeErr error
	// closePreparedAbort already released the mirror-owned pidfd when mirror != nil.
	if fd >= 0 && mirror == nil {
		closeErr = unix.Close(fd)
	}
	closeLinuxSupervisorFilesV2(ownerWrite, controlWrite, responseRead, statusRead)
	if closeErr != nil {
		return fmt.Errorf("close prepared Linux supervisor pidfd: %w", closeErr)
	}
	return nil
}

func (supervisor *linuxSupervisor) RenewLease(deadline time.Duration, sequence uint64) error {
	if deadline <= 0 || sequence == 0 {
		return errors.New("Linux supervisor lease deadline and sequence must be positive")
	}
	supervisor.leaseResumeMu.Lock()
	defer supervisor.leaseResumeMu.Unlock()
	response, err := supervisor.request(linuxSupervisorOpRenew, sequence, deadline)
	if err != nil {
		return err
	}
	if response.Status != "renewed" && response.Status != "already_renewed" {
		return fmt.Errorf("Linux supervisor lease returned %q", response.Status)
	}
	return supervisor.sealInitialMirrorScanForWitness(sequence)
}

// Lets the explicitly gated lease-armed pre-resume witness record a durable mirror seal after its
// first lease acknowledgement; all other launch and renewal paths are unchanged.
func (supervisor *linuxSupervisor) sealInitialMirrorScanForWitness(sequence uint64) error {
	injection, enabled := linuxSupervisorScanFailureInjectionFromEnv()
	if !enabled || strings.TrimSpace(linuxSupervisorWitnessConfigFromEnv().leaseArmedBarrierReachedPath) == "" ||
		sequence != 1 || !injection.sealAfterClean || injection.sealOwner != authority.SupervisorScanOwnerMirror {
		return nil
	}
	supervisor.mu.Lock()
	if !supervisor.committed || supervisor.resumed || supervisor.mirrorSealWitnessScanTriggered {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.mirrorSealWitnessScanTriggered = true
	mirror := supervisor.mirror
	supervisor.mu.Unlock()
	if mirror == nil {
		return fmt.Errorf("%w: pre-resume mirror witness scan is unavailable", ErrLinuxDescendantContainmentUnproven)
	}
	deadline := time.Now().Add(containmentCloseDeadline)
	result, err := mirror.requestDescendantMonitorScan(deadline)
	if err != nil {
		return fmt.Errorf("request pre-resume Linux mirror witness scan: %w", err)
	}
	if result.scanErr != nil {
		return fmt.Errorf("pre-resume Linux mirror witness scan: %w", result.scanErr)
	}
	if !result.leaderPresent || result.stopped {
		return fmt.Errorf("%w: pre-resume Linux mirror witness scan did not observe the stopped target", ErrLinuxDescendantContainmentUnproven)
	}
	mirror.mutex.Lock()
	done := mirror.monitorDone
	mirror.mutex.Unlock()
	remaining := time.Until(deadline)
	if done == nil || remaining <= 0 {
		return fmt.Errorf("%w: pre-resume Linux mirror seal did not stop its monitor", ErrLinuxDescendantContainmentUnproven)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return fmt.Errorf("%w: wait for pre-resume Linux mirror seal: %w", ErrLinuxDescendantContainmentUnproven, context.DeadlineExceeded)
	}
	if err := mirror.sealDescendantScanAfterMonitorStop(); err != nil {
		return fmt.Errorf("seal pre-resume Linux mirror witness scan: %w", err)
	}
	if err := mirror.assertDescendantScanSealedLocally(); err != nil {
		return fmt.Errorf("verify pre-resume Linux mirror witness seal: %w", err)
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
		controlWrite := supervisor.markResponseTransportLostLocked()
		supervisor.mu.Unlock()
		if controlWrite != nil {
			_ = controlWrite.Close()
		}
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
	var writeErr error
	if supervisor.writeControlFrameForTesting != nil {
		writeErr = supervisor.writeControlFrameForTesting(control, request)
	} else {
		writeErr = writeLinuxSupervisorFrame(control, request)
	}
	if writeErr != nil {
		supervisor.mu.Lock()
		controlWrite := supervisor.markResponseTransportLostLocked()
		supervisor.mu.Unlock()
		if controlWrite != nil {
			_ = controlWrite.Close()
		}
		return linuxSupervisorResponse{}, errors.Join(errLinuxSupervisorControlWriteLost, fmt.Errorf("write Linux supervisor request: %w", writeErr))
	}
	supervisor.dropResponseTransportOnce(operation)
	var response linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(reader, &response); err != nil {
		supervisor.mu.Lock()
		controlWrite := supervisor.markResponseTransportLostLocked()
		supervisor.mu.Unlock()
		if controlWrite != nil {
			_ = controlWrite.Close()
		}
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

// Caller must hold supervisor.mu and requestMu, and must close the returned file only after
// releasing supervisor.mu.
func (supervisor *linuxSupervisor) markResponseTransportLostLocked() *os.File {
	supervisor.responseTransportLost = true
	supervisor.responseLossPending = true
	controlWrite := supervisor.controlWrite
	supervisor.controlWrite = nil
	return controlWrite
}

// closeControlAfterResponseLossRequestLocked preserves pipe ordering: callers
// hold requestMu, while mu protects detaching the writer from other cleanup.
func (supervisor *linuxSupervisor) closeControlAfterResponseLossRequestLocked() {
	supervisor.mu.Lock()
	controlWrite := supervisor.markResponseTransportLostLocked()
	supervisor.mu.Unlock()
	if controlWrite != nil {
		_ = controlWrite.Close()
	}
}

func (supervisor *linuxSupervisor) dropResponseTransportOnce(operation string) bool {
	if supervisor == nil {
		return false
	}
	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.productionWitness != "1" {
		return false
	}
	configuredOperation := witness.dropResponseOnce
	if configuredOperation != linuxSupervisorOpStop && configuredOperation != linuxSupervisorOpRelease || configuredOperation != operation {
		return false
	}

	supervisor.mu.Lock()
	if supervisor.responseDropTriggered || supervisor.responseRead == nil ||
		(operation == linuxSupervisorOpRelease && supervisor.receipt == nil) {
		supervisor.mu.Unlock()
		return false
	}
	responseRead := supervisor.responseRead
	supervisor.responseDropTriggered = true
	supervisor.responseRead = nil
	supervisor.responseReader = nil
	controlWrite := supervisor.markResponseTransportLostLocked()
	supervisor.mu.Unlock()

	if markerPath := strings.TrimSpace(witness.dropResponseFiredPath); markerPath != "" {
		_ = publishLinuxSupervisorWitnessFile(markerPath, []byte("fired\n"))
	}
	_ = responseRead.Close()
	if controlWrite != nil {
		_ = controlWrite.Close()
	}
	return true
}

// Opt-in production witness seam: the reached marker is created before automatic recovery, and
// recovery/clear proceeds only after the test-owned release marker appears.
func (supervisor *linuxSupervisor) waitForLinuxSupervisorReleaseResponseLossBarrier() error {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.productionWitness != "1" ||
		witness.dropResponseOnce != linuxSupervisorOpRelease {
		return nil
	}
	supervisor.mu.Lock()
	dropTriggered := supervisor.responseDropTriggered && supervisor.responseTransportLost
	receiptPresent := supervisor.receipt != nil
	supervisor.mu.Unlock()
	if !dropTriggered {
		return nil
	}
	reachedPath, reachedConfigured := witness.releaseBarrierReachedPath, witness.releaseBarrierReachedSet
	releasePath, releaseConfigured := witness.releaseBarrierReleasePath, witness.releaseBarrierReleaseSet
	if !reachedConfigured && !releaseConfigured {
		return nil
	}
	if reachedConfigured != releaseConfigured {
		return fmt.Errorf("Linux supervisor release response-loss barrier requires both %s and %s", linuxSupervisorReleaseBarrierReachedEnv, linuxSupervisorReleaseBarrierReleaseEnv)
	}
	reachedPath = strings.TrimSpace(reachedPath)
	releasePath = strings.TrimSpace(releasePath)
	if reachedPath == "" || releasePath == "" {
		return errors.New("Linux supervisor release response-loss barrier marker paths must not be empty")
	}
	if filepath.Clean(reachedPath) == filepath.Clean(releasePath) {
		return errors.New("Linux supervisor release response-loss barrier marker paths must be distinct")
	}
	if err := assertLinuxSupervisorBarrierPathAbsent(reachedPath); err != nil {
		return fmt.Errorf("validate release response-loss reached marker %q: %w", reachedPath, err)
	}
	if err := assertLinuxSupervisorBarrierPathAbsent(releasePath); err != nil {
		return fmt.Errorf("validate release response-loss release marker %q: %w", releasePath, err)
	}
	if !receiptPresent {
		return errors.New("Linux supervisor release response-loss barrier requires a stop receipt")
	}

	marker := fmt.Sprintf(
		"pid=%d\ntime=%s\noperation=%s\nreceipt_present=%t\n",
		os.Getpid(),
		time.Now().UTC().Format(time.RFC3339Nano),
		linuxSupervisorOpRelease,
		receiptPresent,
	)
	if err := publishLinuxSupervisorWitnessFile(reachedPath, []byte(marker)); err != nil {
		return fmt.Errorf("publish release response-loss reached marker %q: %w", reachedPath, err)
	}

	deadline := time.NewTimer(linuxSupervisorReleaseBarrierTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(linuxSupervisorReleaseBarrierPollInterval)
	defer poll.Stop()
	for {
		if _, err := os.Lstat(releasePath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("observe release response-loss release marker %q: %w", releasePath, err)
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("release response-loss barrier release marker %q did not appear within %s", releasePath, linuxSupervisorReleaseBarrierTimeout)
		case <-poll.C:
		}
	}
}

// Endpoint absence, helper identity mismatch, and unknown responses are all unresolved outcomes.
func StopPersistedLinuxSupervisor(value authority.Supervisor) (authority.StopReceipt, error) {
	receipt, _, _, err := stopPersistedLinuxSupervisor(value)
	return receipt, err
}

func StopPersistedLinuxSupervisorWithScanSeal(value authority.Supervisor) (authority.StopReceipt, uint64, error) {
	receipt, sequence, _, err := stopPersistedLinuxSupervisor(value)
	return receipt, sequence, err
}

func stopPersistedLinuxSupervisor(value authority.Supervisor) (authority.StopReceipt, uint64, linuxSupervisorStopReceiptSource, error) {
	if err := value.Validate(); err != nil {
		return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown, fmt.Errorf("validate persisted Linux supervisor authority: %w", err)
	}
	connection, err := dialLinuxSupervisorRecovery(value)
	if err != nil {
		if !isLinuxSupervisorRecoveryEndpointUnreachable(err) {
			return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown, fmt.Errorf("connect persisted Linux supervisor: %w", err)
		}
		if value.StopReceipt == nil || !value.StopReceipt.ValidFor(value) {
			return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown,
				fmt.Errorf("%w: absent helper endpoint cannot mint a stop receipt", ErrLinuxSupervisorStopUnproven)
		}
		if proofErr := provePersistedLinuxSupervisorStop(value); proofErr != nil {
			return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown, errors.Join(fmt.Errorf("connect persisted Linux supervisor: %w", err), proofErr)
		}
		return *value.StopReceipt, 0, linuxSupervisorStopReceiptFromAbsenceProof, nil
	}
	defer connection.Close()
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpStop, LaunchToken: value.LaunchToken, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, Token: value.PipeToken, JobID: value.JobID, Secret: value.Secret}
	response, err := exchangeLinuxSupervisorRecovery(connection, request, value)
	if err != nil {
		return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown, err
	}
	if response.Status != "stopped" || response.ActiveProcesses != 0 {
		return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown, fmt.Errorf("%w: persisted helper returned %q", ErrLinuxSupervisorStopUnproven, response.Status)
	}
	if response.ScanSealSequence == 0 {
		return authority.StopReceipt{}, 0, linuxSupervisorStopReceiptUnknown,
			fmt.Errorf("%w: persisted helper returned no terminal scan seal", ErrLinuxSupervisorStopUnproven)
	}
	receipt, receiptErr := persistedLinuxSupervisorStopReceipt(value)
	return receipt, response.ScanSealSequence, linuxSupervisorStopReceiptFromHelper, receiptErr
}

// Releases a helper after the caller has durably recorded the exact stop receipt.
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

// An absent endpoint is not sufficient by itself: the exact helper, target identity, and
// original process group must all be absent as well.
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

// The endpoint is cleared only after the exact stop receipt has been reconstructed and durably
// supplied by the caller.
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
		return fmt.Errorf("inspect Linux supervisor recovery endpoint: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("Linux supervisor recovery endpoint is not a socket")
	}
	connection, dialErr := net.DialTimeout("unix", endpoint, linuxSupervisorTimeout)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("Linux supervisor recovery endpoint still accepts connections")
	}
	dialErr = sanitizeLinuxSupervisorEndpointError(dialErr)
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("probe stale Linux supervisor recovery endpoint: %w", dialErr)
	}
	if err := os.Remove(endpoint); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale Linux supervisor recovery endpoint: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	if _, err := linuxSupervisorLstat(endpoint); err == nil {
		return errors.New("Linux supervisor recovery endpoint remained after stale removal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("verify stale Linux supervisor recovery endpoint removal: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	return nil
}

// A bound helper is authenticated before its secret is sent; any live object or uncertain
// observation remains unresolved, and this proof never mints a stop receipt.
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
		Version:            authority.SupervisorHandoffAbortProofVersion,
		Disposition:        authority.SupervisorHandoffAbortDisposition,
		LaunchToken:        handoff.LaunchToken,
		JobID:              handoff.JobID,
		OwnerKind:          handoff.OwnerKind,
		OwnerContext:       handoff.OwnerContext,
		TargetPID:          handoff.TargetPID,
		TargetIdentity:     handoff.TargetIdentity,
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
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
		err = sanitizeLinuxSupervisorEndpointError(err)
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
		return true, fmt.Errorf("set Linux supervisor abort deadline: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	defer unixConnection.SetDeadline(time.Time{})
	expectedPID := handoff.SupervisorPID
	expectedIdentity := handoff.SupervisorIdentity
	if expectedPID > 0 {
		if err := verifyLinuxSupervisorPeerBeforeSecret(unixConnection, expectedPID, expectedIdentity); err != nil {
			return true, err
		}
	} else {
		if err := verifyLinuxSupervisorUnboundPeer(unixConnection, handoff.OwnerContext); err != nil {
			return true, err
		}
		credentials, err := linuxSupervisorPeerCredentials(unixConnection)
		if err != nil {
			return true, sanitizeLinuxSupervisorEndpointError(err)
		}
		expectedPID = int(credentials.Pid)
		expectedIdentity, err = linuxSupervisorHelperIdentity(handoff.OwnerContext)
		if err != nil {
			return true, err
		}
	}
	request := linuxSupervisorRequest{Version: linuxSupervisorProtocolVersion, Operation: linuxSupervisorOpAbort, LaunchToken: handoff.LaunchToken, TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity, OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext, SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity, Token: handoff.PipeToken, JobID: handoff.JobID, Secret: handoff.Secret}
	if err := writeLinuxSupervisorFrame(unixConnection, request); err != nil {
		return true, fmt.Errorf("send Linux supervisor abort request: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	var response linuxSupervisorResponse
	if err := readLinuxSupervisorFrame(bufio.NewReader(unixConnection), &response); err != nil {
		return true, fmt.Errorf("read Linux supervisor abort response: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	if response.Version != linuxSupervisorProtocolVersion || response.Operation != linuxSupervisorOpAbort || response.Sequence != request.Sequence || response.LaunchToken != handoff.LaunchToken || response.TargetPID != handoff.TargetPID || response.TargetIdentity != handoff.TargetIdentity || response.OwnerKind != handoff.OwnerKind || response.OwnerContext != handoff.OwnerContext || response.SupervisorPID != expectedPID || response.SupervisorIdentity != expectedIdentity || response.Token != handoff.PipeToken || response.JobID != handoff.JobID {
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
	if err := verifyLinuxSupervisorPeer(unixConnection, expectedPID, expectedIdentity); err != nil {
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
		var helperErr error
		if handoff.SupervisorPID > 0 {
			helperErr = proveLinuxSupervisorHelperAbsent(handoff.SupervisorPID, handoff.SupervisorIdentity)
		}
		targetErr := proveLinuxSupervisorTargetAbsent(handoff.TargetPID, handoff.TargetIdentity)
		groupErr := linuxSupervisorProveProcessGroupAbsent(context.Background(), handoff.TargetPID, handoff.TargetIdentity)
		if endpointErr == nil && helperErr == nil && targetErr == nil && groupErr == nil {
			return nil
		}
		var causes []error
		if endpointErr != nil {
			causes = append(causes, endpointErr)
		}
		if helperErr != nil {
			causes = append(causes, helperErr)
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
		return fmt.Errorf("prove Linux supervisor recovery endpoint absent: %w", sanitizeLinuxSupervisorEndpointError(err))
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
