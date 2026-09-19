//go:build windows

package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/windows"
)

const (
	containmentSupervisorProtocol       = 1
	containmentSupervisorPipePrefix     = `\\.\pipe\symmetry-containment-`
	containmentSupervisorMaxLine        = 16 << 10
	containmentSupervisorConnect        = 5 * time.Second
	containmentSupervisorProbeInterval  = 10 * time.Millisecond
	containmentSupervisorConnectionPoll = 1 * time.Second
	containmentSupervisorStopRetry      = 100 * time.Millisecond
)

var errContainmentStopUnproven = errors.New("persisted Windows containment stop is unproven")

var errContainmentSupervisorConnectionPoll = errors.New("containment supervisor connection poll timeout")

var errContainmentSupervisorLeaseExpired = errors.New("containment supervisor lease expired")

var errContainmentSupervisorLeaseStopped = errors.New("containment supervisor lease is stopped")

var errContainmentSupervisorStopReceiptUnavailable = errors.New("containment supervisor stop receipt is unavailable")

type containmentSupervisorEndpoint struct {
	TargetPID          int
	TargetIdentity     string
	SupervisorPID      int
	SupervisorIdentity string
	PipeName           string
	Token              string
	JobID              string
	Secret             string
}

type containmentSupervisorRequest struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	Sequence           uint64 `json:"sequence,omitempty"`
	DeadlineMS         int64  `json:"deadline_ms,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	Token              string `json:"token"`
	JobID              string `json:"job_id"`
	Secret             string `json:"secret"`
}

type containmentSupervisorResponse struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	Status             string `json:"status"`
	Sequence           uint64 `json:"sequence,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	Token              string `json:"token"`
	JobID              string `json:"job_id"`
	ActiveProcesses    uint32 `json:"active_processes"`
	Error              string `json:"error,omitempty"`
}

type containmentSupervisorBootstrap struct {
	Version            int    `json:"version"`
	JobHandle          uint64 `json:"job_handle"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	Secret             string `json:"secret"`
}

type containmentSupervisorLease interface {
	close(deadline time.Time) error
	renew(deadline time.Duration, sequence uint64) error
}

type containmentSupervisorPartialLease interface {
	containmentSupervisorLease
	closePartial(deadline time.Time) error
	partialContainmentSupervisorLease()
}

// containmentSupervisorStopLease separates stop proof from authenticated
// helper release. The helper remains reachable after a successful close until
// the daemon persists the exact receipt and explicitly calls release.
type containmentSupervisorStopLease interface {
	containmentSupervisorLease
	stop(deadline time.Time) (authority.StopReceipt, error)
	release(deadline time.Time) error
}

// containmentSupervisorOwnerWatchdog observes an inherited read end whose
// write end remains owned by the daemon. EOF or any other read failure means
// the daemon owner is no longer alive. The helper keeps the watchdog local to
// the process and never serializes this handle into durable authority.
type containmentSupervisorOwnerWatchdog struct {
	file      *os.File
	lost      chan struct{}
	done      chan struct{}
	stateMu   sync.Mutex
	disarmed  bool
	closeOnce sync.Once
	lostOnce  sync.Once
}

func newContainmentSupervisorOwnerWatchdog(file *os.File) *containmentSupervisorOwnerWatchdog {
	watchdog := &containmentSupervisorOwnerWatchdog{
		file: file,
		lost: make(chan struct{}),
		done: make(chan struct{}),
	}
	go watchdog.watch()
	return watchdog
}

func (watchdog *containmentSupervisorOwnerWatchdog) watch() {
	defer close(watchdog.done)
	if watchdog == nil || watchdog.file == nil {
		return
	}
	var buffer [1]byte
	for {
		if _, err := watchdog.file.Read(buffer[:]); err != nil {
			watchdog.stateMu.Lock()
			disarmed := watchdog.disarmed
			watchdog.stateMu.Unlock()
			if !disarmed {
				watchdog.lostOnce.Do(func() { close(watchdog.lost) })
			}
			return
		}
	}
}

func (watchdog *containmentSupervisorOwnerWatchdog) ownerLost() <-chan struct{} {
	if watchdog == nil {
		return nil
	}
	return watchdog.lost
}

// disarm closes the inherited read end only after marking the close as
// intentional. This ordering is the graceful-close fence: the helper cannot
// mistake its own shutdown path for daemon owner loss.
func (watchdog *containmentSupervisorOwnerWatchdog) disarm() {
	if watchdog == nil {
		return
	}
	watchdog.stateMu.Lock()
	watchdog.disarmed = true
	watchdog.stateMu.Unlock()
	watchdog.closeOnce.Do(func() {
		if watchdog.file != nil {
			_ = watchdog.file.Close()
		}
	})
	<-watchdog.done
}

type containmentSupervisorAuthorityProvider interface {
	containmentAuthority() *authority.Supervisor
}

var (
	launchContainmentSupervisor         = launchContainmentSupervisorProcess
	requestContainmentSupervisor        = requestContainmentSupervisorPipe
	requestContainmentSupervisorRenew   = requestContainmentSupervisorRenewPipe
	readSupervisorProcessIdentity       = ProcessIdentity
	containmentSupervisorAfterFunc      = time.AfterFunc
	setContainmentHandleInformation     = syscall.SetHandleInformation
	startContainmentSupervisor          = func(command *exec.Cmd) error { return command.Start() }
	generateContainmentSupervisorSecret = randomContainmentSecret
	writeContainmentSupervisorBootstrap = writeBootstrapPayload
	killContainmentSupervisor           = func(waiter *supervisorProcessWait, deadline time.Time) error {
		return waiter.killAndWait(deadline)
	}
)

// TargetProcessIdentity validates the existing plain Windows process identity.
// Supervisor metadata is deliberately not encoded in this field; the typed
// authority is persisted beside it in the local journal.
func TargetProcessIdentity(value string) (string, bool) {
	if _, _, err := parseWindowsProcessIdentity(value); err != nil {
		return "", false
	}
	return value, true
}

// RecoverPersistedContainment is retained for legacy callers. A plain process
// marker has no supervisor authority and therefore cannot prove a Windows Job
// stop after a daemon restart.
func RecoverPersistedContainment(pid int, value string) error {
	return fmt.Errorf("%w: persisted supervisor authority is unavailable", errContainmentStopUnproven)
}

// RecoverPersistedContainmentWithAuthority asks the originally launched
// supervisor to terminate and release its original Job. It first verifies the
// persisted helper PID/creation identity and pipe owner, then sends the secret-
// bearing request. A missing or mismatched authority is always unknown stop.
func RecoverPersistedContainmentWithAuthority(pid int, value string, persisted *authority.Supervisor) (authority.StopReceipt, error) {
	if persisted == nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted supervisor authority is unavailable", errContainmentStopUnproven)
	}
	if err := persisted.Validate(); err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: invalid persisted supervisor authority", errContainmentStopUnproven)
	}
	targetPID, _, err := parseWindowsProcessIdentity(value)
	if err != nil || pid <= 0 || targetPID != pid || persisted.TargetPID != pid || persisted.TargetIdentity != value {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted target identity does not match authority", errContainmentStopUnproven)
	}
	if persisted.StopReceipt != nil && persisted.StopReceipt.ValidFor(*persisted) {
		return *persisted.StopReceipt, nil
	}
	endpoint, err := containmentEndpointForAuthority(*persisted)
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted containment authority is invalid", errContainmentStopUnproven)
	}
	deadline := time.Now().Add(containmentSupervisorConnect)
	response, err := requestContainmentSupervisor(endpoint, "recover", deadline)
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: recover through containment supervisor: %v", errContainmentStopUnproven, err)
	}
	if err := validateSupervisorResponse(endpoint, "recover", response); err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: invalid containment supervisor recovery proof: %v", errContainmentStopUnproven, err)
	}
	if response.Status != "stopped" || response.ActiveProcesses != 0 {
		return authority.StopReceipt{}, fmt.Errorf("%w: supervisor did not prove an empty Job", errContainmentStopUnproven)
	}
	return authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             response.Status,
		TargetPID:          response.TargetPID,
		TargetIdentity:     response.TargetIdentity,
		PipeToken:          response.Token,
		JobID:              response.JobID,
		SupervisorPID:      response.SupervisorPID,
		SupervisorIdentity: response.SupervisorIdentity,
		ActiveProcesses:    response.ActiveProcesses,
	}, nil
}

// ReleasePersistedContainmentWithAuthority releases the helper's inherited Job
// handle after the caller has durably acknowledged the exact stop receipt.
// The helper refuses this operation until its own stop state contains the same
// empty-Job proof, so release cannot be used as an active-process stop or lease
// rearm path.
func ReleasePersistedContainmentWithAuthority(pid int, value string, persisted *authority.Supervisor) error {
	if persisted == nil {
		return fmt.Errorf("%w: persisted supervisor authority is unavailable", errContainmentStopUnproven)
	}
	if err := persisted.Validate(); err != nil {
		return fmt.Errorf("%w: invalid persisted supervisor authority", errContainmentStopUnproven)
	}
	if persisted.StopReceipt == nil || !persisted.StopReceipt.ValidFor(*persisted) {
		return fmt.Errorf("%w: durable stop receipt is required before helper release", errContainmentStopUnproven)
	}
	targetPID, _, err := parseWindowsProcessIdentity(value)
	if err != nil || pid <= 0 || targetPID != pid || persisted.TargetPID != pid || persisted.TargetIdentity != value {
		return fmt.Errorf("%w: persisted target identity does not match authority", errContainmentStopUnproven)
	}
	endpoint, err := containmentEndpointForAuthority(*persisted)
	if err != nil {
		return fmt.Errorf("%w: persisted containment authority is invalid", errContainmentStopUnproven)
	}
	if _, err := readSupervisorProcessIdentity(endpoint.SupervisorPID); err != nil {
		// A durable stop receipt makes a missing helper harmless: the Job was
		// already proven empty, and a response-loss after release is safe to
		// replay as an idempotent release acknowledgement.
		if errors.Is(err, syscall.Errno(2)) || errors.Is(err, syscall.Errno(87)) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: read persisted supervisor identity: %v", errContainmentStopUnproven, err)
	}
	deadline := time.Now().Add(containmentSupervisorConnect)
	response, err := requestContainmentSupervisor(endpoint, "release", deadline)
	if err != nil {
		return fmt.Errorf("%w: release through containment supervisor: %v", errContainmentStopUnproven, err)
	}
	if err := validateSupervisorResponse(endpoint, "release", response); err != nil {
		return fmt.Errorf("%w: invalid containment supervisor release acknowledgement: %v", errContainmentStopUnproven, err)
	}
	if response.Status != "released" || response.ActiveProcesses != 0 {
		return fmt.Errorf("%w: supervisor did not acknowledge helper release", errContainmentStopUnproven)
	}
	return nil
}

func launchContainmentSupervisorProcess(job syscall.Handle, targetPID int, targetIdentity string) (containmentSupervisorLease, string, error) {
	if job == 0 || targetPID <= 0 || strings.TrimSpace(targetIdentity) == "" {
		return nil, "", errors.New("containment supervisor requires a Job handle and process identity")
	}
	// Go test binaries have their own generated main and cannot dispatch the
	// daemon's hidden supervisor mode. Keep native process tests deterministic;
	// the production daemon executable never matches this suffix and therefore
	// still requires a real independent supervisor.
	if isGoTestBinary(os.Args[0]) {
		return noopContainmentSupervisorLease{}, targetIdentity, nil
	}
	endpoint, err := newContainmentSupervisorEndpoint(targetPID, targetIdentity)
	if err != nil {
		return nil, "", err
	}
	bootstrapRead, bootstrapWrite, err := os.Pipe()
	if err != nil {
		return nil, "", fmt.Errorf("create containment supervisor bootstrap channel: %w", err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
		return nil, "", fmt.Errorf("create containment supervisor owner channel: %w", err)
	}
	closeBootstrap := func() {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
	}
	closeOwner := func() {
		_ = ownerRead.Close()
		_ = ownerWrite.Close()
	}
	if err := setContainmentHandleInformation(syscall.Handle(bootstrapRead.Fd()), syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("make containment supervisor bootstrap handle inheritable: %w", err)
	}
	if err := setContainmentHandleInformation(syscall.Handle(ownerRead.Fd()), syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("make containment supervisor owner handle inheritable: %w", err)
	}
	if err := setContainmentHandleInformation(syscall.Handle(ownerWrite.Fd()), syscall.HANDLE_FLAG_INHERIT, 0); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("make containment supervisor owner writer non-inheritable: %w", err)
	}

	if err := setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("make containment Job handle inheritable: %w", err)
	}
	clearInheritance := true
	defer func() {
		if clearInheritance {
			_ = setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, 0)
		}
	}()

	command := exec.Command(os.Args[0],
		"-containment-supervisor",
		"-bootstrap-handle", strconv.FormatUint(uint64(bootstrapRead.Fd()), 10),
		"-owner-handle", strconv.FormatUint(uint64(ownerRead.Fd()), 10),
	)
	if err := ConfigureHeadlessProcess(command); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("configure containment supervisor: %w", err)
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{
		job,
		syscall.Handle(bootstrapRead.Fd()),
		syscall.Handle(ownerRead.Fd()),
	}
	if err := startContainmentSupervisor(command); err != nil {
		closeBootstrap()
		closeOwner()
		return nil, "", fmt.Errorf("start containment supervisor: %w", err)
	}
	_ = bootstrapRead.Close()
	_ = ownerRead.Close()
	waiter := newSupervisorProcessWait(command.Process)
	endpoint.SupervisorPID = command.Process.Pid
	partialLease := &processContainmentSupervisorLease{endpoint: endpoint, waiter: waiter, ownerWrite: ownerWrite}
	clearInheritance = true
	if err := setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, 0); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return newPartialContainmentSupervisorLease(partialLease), targetIdentity, fmt.Errorf("clear containment Job inheritance: %w", err)
	}
	clearInheritance = false

	endpoint.SupervisorIdentity, err = readSupervisorProcessIdentity(endpoint.SupervisorPID)
	if err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		partialLease.endpoint = endpoint
		return newPartialContainmentSupervisorLease(partialLease), targetIdentity, fmt.Errorf("capture containment supervisor identity: %w", err)
	}
	endpoint.Secret, err = generateContainmentSupervisorSecret()
	if err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		partialLease.endpoint = endpoint
		return newPartialContainmentSupervisorLease(partialLease), targetIdentity, fmt.Errorf("generate containment supervisor authority: %w", err)
	}
	bootstrap := containmentSupervisorBootstrap{
		Version:            containmentSupervisorProtocol,
		JobHandle:          uint64(job),
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		PipeToken:          endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}
	if err := writeContainmentSupervisorBootstrap(bootstrapWrite, bootstrap, time.Now().Add(containmentSupervisorConnect)); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		partialLease.endpoint = endpoint
		return newPartialContainmentSupervisorLease(partialLease), targetIdentity, fmt.Errorf("send containment supervisor bootstrap: %w", err)
	}
	_ = bootstrapWrite.Close()
	partialLease.endpoint = endpoint
	lease := partialLease
	deadline := time.Now().Add(containmentSupervisorConnect)
	response, err := requestContainmentSupervisor(endpoint, "hello", deadline)
	if err != nil {
		partialLease.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return nil, "", fmt.Errorf("connect containment supervisor: %w", err)
	}
	if err := validateSupervisorResponse(endpoint, "hello", response); err != nil {
		partialLease.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return nil, "", fmt.Errorf("validate containment supervisor handshake: %w", err)
	}
	if response.Status != "ready" || response.SupervisorPID <= 0 || response.SupervisorIdentity == "" {
		partialLease.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return nil, "", errors.New("containment supervisor returned an incomplete handshake")
	}
	if response.SupervisorPID != endpoint.SupervisorPID || response.SupervisorIdentity != endpoint.SupervisorIdentity {
		partialLease.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return nil, "", errors.New("containment supervisor identity mismatch")
	}
	lease.endpoint = endpoint
	return lease, targetIdentity, nil
}

type noopContainmentSupervisorLease struct{}

func (noopContainmentSupervisorLease) close(time.Time) error { return nil }

func (noopContainmentSupervisorLease) renew(time.Duration, uint64) error { return nil }

func (noopContainmentSupervisorLease) stop(time.Time) (authority.StopReceipt, error) {
	return authority.StopReceipt{}, nil
}

func (noopContainmentSupervisorLease) release(time.Time) error { return nil }

func isGoTestBinary(path string) bool {
	if flag.CommandLine.Lookup("test.v") == nil {
		return false
	}
	name := strings.ToLower(path)
	return strings.HasSuffix(name, ".test.exe") || strings.HasSuffix(name, ".test")
}

type processContainmentSupervisorLease struct {
	mutex      sync.Mutex
	endpoint   containmentSupervisorEndpoint
	waiter     *supervisorProcessWait
	ownerWrite *os.File
	stopped    bool
	released   bool
	receipt    *authority.StopReceipt
	closeErr   error
}

// processContainmentSupervisorPartialLease owns only pre-authority cleanup.
// AttachSuspendedProcess may return this lease together with an error after the
// child has inherited the Job handle, so it must not advertise the durable
// authority or stop/release capabilities. jobContainment.Close then retains
// the helper cleanup attempt but still releases the daemon-side Job handle when
// that attempt is unproven.
type processContainmentSupervisorPartialLease struct {
	lease *processContainmentSupervisorLease
}

func newPartialContainmentSupervisorLease(lease *processContainmentSupervisorLease) containmentSupervisorLease {
	if lease == nil {
		return nil
	}
	return &processContainmentSupervisorPartialLease{lease: lease}
}

func (lease *processContainmentSupervisorPartialLease) close(deadline time.Time) error {
	if lease == nil || lease.lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	return lease.lease.closePartial(deadline)
}

func (lease *processContainmentSupervisorPartialLease) closePartial(deadline time.Time) error {
	if lease == nil || lease.lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	return lease.lease.closePartial(deadline)
}

func (*processContainmentSupervisorPartialLease) partialContainmentSupervisorLease() {}

func (lease *processContainmentSupervisorPartialLease) renew(deadline time.Duration, sequence uint64) error {
	if lease == nil || lease.lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	return lease.lease.renew(deadline, sequence)
}

// closePartial only owns pre-authority cleanup. It deliberately does not send
// a stop/release request with an incomplete endpoint; it closes the owner
// channel and retries killing/reaping the helper until that local cleanup is
// proven. The daemon-side Job remains owned by jobContainment and is released
// independently even when this helper cleanup is still unproven.
func (lease *processContainmentSupervisorLease) closePartial(deadline time.Time) error {
	if lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	lease.mutex.Lock()
	if lease.released {
		lease.mutex.Unlock()
		return nil
	}
	lease.closeOwnerWriterLocked()
	waiter := lease.waiter
	lease.mutex.Unlock()
	if waiter == nil || waiter.process == nil {
		return errors.New("containment supervisor process handle is missing")
	}
	if err := killContainmentSupervisor(waiter, deadline); err != nil {
		return fmt.Errorf("terminate partial containment supervisor: %w", err)
	}
	lease.mutex.Lock()
	lease.released = true
	lease.mutex.Unlock()
	return nil
}

func (lease *processContainmentSupervisorLease) LeaseRenewalAvailable() bool {
	return lease != nil
}

func (lease *processContainmentSupervisorLease) RenewLease(deadline time.Duration, sequence uint64) error {
	return lease.renew(deadline, sequence)
}

func (lease *processContainmentSupervisorLease) containmentAuthority() *authority.Supervisor {
	if lease == nil {
		return nil
	}
	value := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             lease.endpoint.Secret,
		TargetPID:          lease.endpoint.TargetPID,
		TargetIdentity:     lease.endpoint.TargetIdentity,
		PipeToken:          lease.endpoint.Token,
		JobID:              lease.endpoint.JobID,
		SupervisorPID:      lease.endpoint.SupervisorPID,
		SupervisorIdentity: lease.endpoint.SupervisorIdentity,
	}
	return &value
}

func (lease *processContainmentSupervisorLease) renew(deadline time.Duration, sequence uint64) error {
	if lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	if deadline <= 0 {
		return errors.New("containment supervisor lease deadline must be positive")
	}
	if sequence == 0 {
		return errors.New("containment supervisor lease sequence must be positive")
	}
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	if lease.stopped || lease.released {
		return errContainmentSupervisorLeaseStopped
	}
	if lease.waiter == nil || lease.waiter.process == nil {
		return errors.New("containment supervisor process handle is missing")
	}
	requestDeadline := time.Now().Add(containmentSupervisorConnectionPoll)
	response, err := requestContainmentSupervisorRenew(lease.endpoint, deadline, sequence, requestDeadline)
	if err != nil {
		return err
	}
	if err := validateSupervisorResponse(lease.endpoint, "renew", response); err != nil {
		return err
	}
	if response.Sequence != sequence {
		return fmt.Errorf("containment supervisor renewal sequence %d does not match %d", response.Sequence, sequence)
	}
	if response.Status != "renewed" && response.Status != "already_renewed" {
		if response.Status == "stopped" {
			return errContainmentSupervisorLeaseStopped
		}
		return fmt.Errorf("containment supervisor renew returned status %q", response.Status)
	}
	return nil
}

func (lease *processContainmentSupervisorLease) stop(deadline time.Time) (authority.StopReceipt, error) {
	if lease == nil {
		return authority.StopReceipt{}, errors.New("containment supervisor lease is missing")
	}
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	if lease.waiter == nil || lease.waiter.process == nil {
		return authority.StopReceipt{}, errors.New("containment supervisor process handle is missing")
	}
	if lease.receipt != nil {
		return *lease.receipt, nil
	}
	if lease.released {
		return authority.StopReceipt{}, errContainmentSupervisorStopReceiptUnavailable
	}
	response, err := requestContainmentSupervisor(lease.endpoint, "close", deadline)
	if err != nil {
		return authority.StopReceipt{}, err
	}
	if err := validateSupervisorResponse(lease.endpoint, "close", response); err != nil {
		return authority.StopReceipt{}, err
	}
	if response.Status != "stopped" || response.ActiveProcesses != 0 {
		return authority.StopReceipt{}, errors.New("containment supervisor close did not prove an empty Job")
	}
	receipt := authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             response.Status,
		TargetPID:          response.TargetPID,
		TargetIdentity:     response.TargetIdentity,
		PipeToken:          response.Token,
		JobID:              response.JobID,
		SupervisorPID:      response.SupervisorPID,
		SupervisorIdentity: response.SupervisorIdentity,
		ActiveProcesses:    response.ActiveProcesses,
	}
	lease.receipt = &receipt
	lease.stopped = true
	return receipt, nil
}

func (lease *processContainmentSupervisorLease) release(deadline time.Time) error {
	if lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	lease.mutex.Lock()
	defer lease.mutex.Unlock()
	if lease.released {
		return nil
	}
	if lease.receipt == nil || !lease.receipt.ValidFor(authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             lease.endpoint.Secret,
		TargetPID:          lease.endpoint.TargetPID,
		TargetIdentity:     lease.endpoint.TargetIdentity,
		PipeToken:          lease.endpoint.Token,
		JobID:              lease.endpoint.JobID,
		SupervisorPID:      lease.endpoint.SupervisorPID,
		SupervisorIdentity: lease.endpoint.SupervisorIdentity,
	}) {
		return errContainmentSupervisorStopReceiptUnavailable
	}
	response, err := requestContainmentSupervisor(lease.endpoint, "release", deadline)
	if err != nil {
		// A response can be lost after the helper has closed its Job handle. If
		// the helper is already gone, release is safely replayable because the
		// receipt was persisted before this operation was attempted.
		if lease.waiterExitedLocked() {
			lease.released = true
			lease.closeOwnerWriterLocked()
			return nil
		}
		return err
	}
	if err := validateSupervisorResponse(lease.endpoint, "release", response); err != nil {
		return err
	}
	if response.Status != "released" || response.ActiveProcesses != 0 {
		return errors.New("containment supervisor release did not acknowledge an empty Job")
	}
	lease.released = true
	lease.closeOwnerWriterLocked()
	return nil
}

func (lease *processContainmentSupervisorLease) waiterExitedLocked() bool {
	if lease == nil || lease.waiter == nil || lease.waiter.done == nil {
		return false
	}
	lease.waiter.ensureStarted()
	select {
	case <-lease.waiter.done:
		return true
	default:
		return false
	}
}

func (lease *processContainmentSupervisorLease) close(deadline time.Time) error {
	if lease == nil {
		return errors.New("containment supervisor lease is missing")
	}
	lease.mutex.Lock()
	if lease.closeErr != nil {
		err := lease.closeErr
		lease.mutex.Unlock()
		return err
	}
	if lease.released {
		lease.mutex.Unlock()
		return nil
	}
	lease.mutex.Unlock()

	if _, err := lease.stop(deadline); err != nil {
		lease.mutex.Lock()
		lease.closeErr = err
		lease.closeOwnerWriterLocked()
		lease.mutex.Unlock()
		// This path is used only while containment setup is being aborted
		// before durable authority is exposed. The caller owns the Job handle
		// and will terminate the target; reap the helper so no orphan remains.
		_ = lease.waiter.killAndWait(deadline)
		return err
	}
	if err := lease.release(deadline); err != nil {
		lease.mutex.Lock()
		lease.closeErr = err
		lease.closeOwnerWriterLocked()
		lease.mutex.Unlock()
		_ = lease.waiter.killAndWait(deadline)
		return err
	}
	return nil
}

func (lease *processContainmentSupervisorLease) closeOwnerWriterLocked() {
	if lease == nil || lease.ownerWrite == nil {
		return
	}
	_ = lease.ownerWrite.Close()
	lease.ownerWrite = nil
}

type supervisorProcessWait struct {
	process *os.Process
	once    sync.Once
	done    chan error
}

func newSupervisorProcessWait(process *os.Process) *supervisorProcessWait {
	return &supervisorProcessWait{process: process, done: make(chan error, 1)}
}

func (waiter *supervisorProcessWait) ensureStarted() {
	if waiter == nil || waiter.process == nil {
		return
	}
	waiter.once.Do(func() {
		go func() {
			_, err := waiter.process.Wait()
			waiter.done <- err
		}()
	})
}

func (waiter *supervisorProcessWait) wait(deadline time.Time) error {
	if waiter == nil || waiter.process == nil {
		return errors.New("containment supervisor process is missing")
	}
	waiter.ensureStarted()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return errors.New("containment supervisor wait deadline expired")
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case err := <-waiter.done:
		return err
	case <-timer.C:
		return errors.New("containment supervisor did not exit before deadline")
	}
}

func (waiter *supervisorProcessWait) killAndWait(deadline time.Time) error {
	if waiter == nil || waiter.process == nil {
		return nil
	}
	waiter.ensureStarted()
	killErr := waiter.process.Kill()
	waitErr := waiter.wait(deadline)
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		if waitErr == nil {
			return nil
		}
		return errors.Join(killErr, waitErr)
	}
	return waitErr
}

func terminateSupervisorProcess(process *os.Process, deadline time.Time) error {
	return newSupervisorProcessWait(process).killAndWait(deadline)
}

func requestContainmentSupervisorPipe(endpoint containmentSupervisorEndpoint, operation string, deadline time.Time) (containmentSupervisorResponse, error) {
	return requestContainmentSupervisorPipeWithRequest(endpoint, containmentSupervisorRequest{
		Version:            containmentSupervisorProtocol,
		Operation:          operation,
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		Token:              endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}, deadline)
}

func requestContainmentSupervisorRenewPipe(endpoint containmentSupervisorEndpoint, leaseRemaining time.Duration, sequence uint64, deadline time.Time) (containmentSupervisorResponse, error) {
	if leaseRemaining <= 0 {
		return containmentSupervisorResponse{}, errors.New("containment supervisor lease deadline must be positive")
	}
	if sequence == 0 {
		return containmentSupervisorResponse{}, errors.New("containment supervisor lease sequence must be positive")
	}
	return requestContainmentSupervisorPipeWithRequest(endpoint, containmentSupervisorRequest{
		Version:            containmentSupervisorProtocol,
		Operation:          "renew",
		Sequence:           sequence,
		DeadlineMS:         leaseRemaining.Milliseconds(),
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		Token:              endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}, deadline)
}

func requestContainmentSupervisorPipeWithRequest(endpoint containmentSupervisorEndpoint, request containmentSupervisorRequest, deadline time.Time) (containmentSupervisorResponse, error) {
	requireSupervisor := true
	if err := validateSupervisorEndpoint(endpoint, requireSupervisor); err != nil {
		return containmentSupervisorResponse{}, err
	}
	if request.Operation != "hello" && request.Operation != "close" && request.Operation != "recover" && request.Operation != "renew" && request.Operation != "release" {
		return containmentSupervisorResponse{}, fmt.Errorf("unknown containment supervisor operation %q", request.Operation)
	}
	var lastErr error
	for {
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = context.DeadlineExceeded
			}
			return containmentSupervisorResponse{}, fmt.Errorf("containment supervisor connection timeout: %w", lastErr)
		}
		pipe, openErr := openContainmentSupervisorPipe(endpoint.PipeName)
		if openErr != nil {
			lastErr = openErr
			if !retryableSupervisorPipeError(openErr) {
				return containmentSupervisorResponse{}, openErr
			}
			time.Sleep(containmentSupervisorProbeInterval)
			continue
		}
		serverPID := uint32(0)
		if err := windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &serverPID); err != nil {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, fmt.Errorf("read containment supervisor pipe owner: %w", err)
		}
		if int(serverPID) != endpoint.SupervisorPID {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, fmt.Errorf("containment supervisor pipe owner PID %d does not match %d", serverPID, endpoint.SupervisorPID)
		}
		serverIdentity, err := readSupervisorProcessIdentity(int(serverPID))
		if err != nil {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, fmt.Errorf("read containment supervisor pipe owner identity: %w", err)
		}
		if serverIdentity != endpoint.SupervisorIdentity {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, errors.New("containment supervisor pipe owner identity mismatch")
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, err
		}
		encoded = append(encoded, '\n')
		if err := writeSupervisorLine(pipe, encoded, deadline); err != nil {
			_ = pipe.Close()
			return containmentSupervisorResponse{}, err
		}
		line, err := readSupervisorLine(pipe, deadline)
		_ = pipe.Close()
		if err != nil {
			return containmentSupervisorResponse{}, err
		}
		var response containmentSupervisorResponse
		if err := decodeSupervisorJSON(line, &response); err != nil {
			return containmentSupervisorResponse{}, fmt.Errorf("decode containment supervisor response: %w", err)
		}
		if response.SupervisorPID != int(serverPID) {
			return containmentSupervisorResponse{}, fmt.Errorf("containment supervisor response PID %d does not match pipe owner %d", response.SupervisorPID, serverPID)
		}
		if serverIdentity != response.SupervisorIdentity {
			return containmentSupervisorResponse{}, errors.New("containment supervisor process identity mismatch")
		}
		return response, nil
	}
}

func openContainmentSupervisorPipe(name string) (*os.File, error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(name),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func retryableSupervisorPipeError(err error) bool {
	return errors.Is(err, windows.ERROR_PIPE_BUSY) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_SEM_TIMEOUT)
}

func writeSupervisorLine(file *os.File, line []byte, deadline time.Time) error {
	if len(line) > containmentSupervisorMaxLine {
		return errors.New("containment supervisor request is too large")
	}
	done := make(chan error, 1)
	go func() {
		written, err := file.Write(line)
		if err == nil && written != len(line) {
			err = io.ErrShortWrite
		}
		done <- err
	}()
	return waitSupervisorIO(file, done, deadline, "write containment supervisor request")
}

func readSupervisorLine(file *os.File, deadline time.Time) ([]byte, error) {
	result := make(chan struct {
		line []byte
		err  error
	}, 1)
	go func() {
		line, err := readBoundedLine(file)
		result <- struct {
			line []byte
			err  error
		}{line: line, err: err}
	}()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		_ = file.Close()
		return nil, errors.New("read containment supervisor response deadline expired")
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case output := <-result:
		return output.line, output.err
	case <-timer.C:
		_ = file.Close()
		output := <-result
		if output.err != nil {
			return nil, fmt.Errorf("read containment supervisor response: %w", output.err)
		}
		return nil, errors.New("read containment supervisor response deadline expired")
	}
}

func waitSupervisorIO(file *os.File, done <-chan error, deadline time.Time, operation string) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		_ = file.Close()
		return fmt.Errorf("%s deadline expired", operation)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
		return nil
	case <-timer.C:
		_ = file.Close()
		return fmt.Errorf("%s deadline expired", operation)
	}
}

func readBoundedLine(reader io.Reader) ([]byte, error) {
	line := make([]byte, 0, 512)
	byteBuffer := []byte{0}
	for len(line) < containmentSupervisorMaxLine {
		count, err := reader.Read(byteBuffer)
		if count > 0 {
			if byteBuffer[0] == '\n' {
				if len(line) == 0 || line[len(line)-1] == '\r' {
					return nil, errors.New("containment supervisor message is empty or has invalid line ending")
				}
				return line, nil
			}
			line = append(line, byteBuffer[0])
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("containment supervisor message exceeds size limit")
}

func validateSupervisorResponse(endpoint containmentSupervisorEndpoint, operation string, response containmentSupervisorResponse) error {
	if response.Version != containmentSupervisorProtocol {
		return fmt.Errorf("unexpected containment supervisor protocol version %d", response.Version)
	}
	if response.Operation != operation {
		return fmt.Errorf("containment supervisor operation %q does not match %q", response.Operation, operation)
	}
	if response.TargetPID != endpoint.TargetPID || response.TargetIdentity != endpoint.TargetIdentity {
		return errors.New("containment supervisor target identity mismatch")
	}
	if response.Token != endpoint.Token || response.JobID != endpoint.JobID {
		return errors.New("containment supervisor Job identity mismatch")
	}
	if response.SupervisorPID != endpoint.SupervisorPID {
		return errors.New("containment supervisor PID mismatch")
	}
	if response.SupervisorIdentity != endpoint.SupervisorIdentity {
		return errors.New("containment supervisor process identity mismatch")
	}
	if response.Status == "error" {
		if response.Error == "" {
			return errors.New("containment supervisor returned an error without details")
		}
		return errors.New(response.Error)
	}
	switch operation {
	case "hello":
		if response.Status != "ready" {
			return fmt.Errorf("unexpected containment supervisor hello status %q", response.Status)
		}
	case "close", "recover":
		if response.Status != "stopped" {
			return fmt.Errorf("unexpected containment supervisor stop status %q", response.Status)
		}
	case "release":
		if response.Status != "released" {
			return fmt.Errorf("unexpected containment supervisor release status %q", response.Status)
		}
	case "renew":
		if response.Status != "renewed" && response.Status != "already_renewed" && response.Status != "stopped" {
			return fmt.Errorf("unexpected containment supervisor renew status %q", response.Status)
		}
	default:
		return fmt.Errorf("unknown containment supervisor operation %q", operation)
	}
	if response.SupervisorPID <= 0 || response.SupervisorIdentity == "" {
		return errors.New("containment supervisor response omitted process identity")
	}
	return nil
}

func validateSupervisorEndpoint(endpoint containmentSupervisorEndpoint, requireSupervisor bool) error {
	if endpoint.TargetPID <= 0 || strings.TrimSpace(endpoint.TargetIdentity) == "" {
		return errors.New("containment supervisor target identity is required")
	}
	if requireSupervisor && (endpoint.SupervisorPID <= 0 || strings.TrimSpace(endpoint.SupervisorIdentity) == "") {
		return errors.New("containment supervisor identity is required")
	}
	if !validContainmentToken(endpoint.Token) || !validContainmentToken(endpoint.JobID) || !validContainmentSecret(endpoint.Secret) {
		return errors.New("containment supervisor token is invalid")
	}
	if endpoint.PipeName != containmentSupervisorPipePrefix+endpoint.Token {
		return errors.New("containment supervisor pipe name does not match token")
	}
	return nil
}

func parseWindowsProcessIdentity(value string) (int, uint64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "windows" {
		return 0, 0, errors.New("Windows process identity must be windows:<pid>:<creation-hex>")
	}
	pidValue, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || pidValue == 0 {
		return 0, 0, errors.New("Windows process identity PID is invalid")
	}
	if len(parts[2]) != 16 {
		return 0, 0, errors.New("Windows process creation identity must be 16 hex digits")
	}
	creation, err := strconv.ParseUint(parts[2], 16, 64)
	if err != nil {
		return 0, 0, errors.New("Windows process creation identity is invalid")
	}
	return int(pidValue), creation, nil
}

func containmentEndpointForIdentity(pid int, identity string) (containmentSupervisorEndpoint, error) {
	// This constructor is retained only for tests and diagnostics. A public
	// process identity must never be sufficient to recover a supervisor.
	identityPID, _, err := parseWindowsProcessIdentity(identity)
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	if pid <= 0 || identityPID != pid {
		return containmentSupervisorEndpoint{}, errors.New("Windows process identity PID does not match target PID")
	}
	token, err := randomContainmentToken()
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	jobID, err := randomContainmentToken()
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	secret, err := randomContainmentSecret()
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	endpoint := containmentSupervisorEndpoint{
		TargetPID:      pid,
		TargetIdentity: identity,
		PipeName:       containmentSupervisorPipePrefix + token,
		Token:          token,
		JobID:          jobID,
		Secret:         secret,
	}
	if err := validateSupervisorEndpoint(endpoint, false); err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	return endpoint, nil
}

func newContainmentSupervisorEndpoint(pid int, identity string) (containmentSupervisorEndpoint, error) {
	identityPID, _, err := parseWindowsProcessIdentity(identity)
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	if pid <= 0 || identityPID != pid {
		return containmentSupervisorEndpoint{}, errors.New("Windows process identity PID does not match target PID")
	}
	token, err := randomContainmentToken()
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	jobID, err := randomContainmentToken()
	if err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	return containmentSupervisorEndpoint{
		TargetPID:      pid,
		TargetIdentity: identity,
		PipeName:       containmentSupervisorPipePrefix + token,
		Token:          token,
		JobID:          jobID,
	}, nil
}

func containmentEndpointForAuthority(value authority.Supervisor) (containmentSupervisorEndpoint, error) {
	if err := value.Validate(); err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	endpoint := containmentSupervisorEndpoint{
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + value.PipeToken,
		Token:              value.PipeToken,
		JobID:              value.JobID,
		Secret:             value.Secret,
	}
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	return endpoint, nil
}

func decodeSupervisorJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("containment supervisor message contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validContainmentToken(value string) bool {
	if len(value) != authority.TokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validContainmentSecret(value string) bool {
	if len(value) != authority.SecretBytes*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func randomContainmentToken() (string, error) {
	value := make([]byte, authority.TokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomContainmentSecret() (string, error) {
	value := make([]byte, authority.SecretBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func writeBootstrapPayload(file *os.File, value containmentSupervisorBootstrap, deadline time.Time) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return writeSupervisorLine(file, encoded, deadline)
}

type containmentSupervisorArgs struct {
	bootstrapHandle syscall.Handle
	ownerHandle     syscall.Handle
}

// RunContainmentSupervisor is called only by the hidden daemon mode. It does
// not accept ordinary daemon configuration or unknown arguments.
func RunContainmentSupervisor(args []string) error {
	parsed, err := parseContainmentSupervisorArgs(args)
	if err != nil {
		return err
	}
	ownerFile := os.NewFile(uintptr(parsed.ownerHandle), "symmetry-containment-owner")
	if ownerFile == nil {
		return errors.New("containment supervisor owner handle is invalid")
	}
	defer ownerFile.Close()
	ownerWatchdog := newContainmentSupervisorOwnerWatchdog(ownerFile)
	defer ownerWatchdog.disarm()
	bootstrapFile := os.NewFile(uintptr(parsed.bootstrapHandle), "symmetry-containment-bootstrap")
	if bootstrapFile == nil {
		return errors.New("containment supervisor bootstrap handle is invalid")
	}
	defer bootstrapFile.Close()
	line, err := readBoundedLine(bootstrapFile)
	if err != nil {
		return fmt.Errorf("read containment supervisor bootstrap: %w", err)
	}
	var bootstrap containmentSupervisorBootstrap
	if err := decodeSupervisorJSON(line, &bootstrap); err != nil {
		return fmt.Errorf("decode containment supervisor bootstrap: %w", err)
	}
	if err := validateSupervisorBootstrap(bootstrap); err != nil {
		return err
	}
	return runContainmentSupervisor(parsed, bootstrap, ownerWatchdog)
}

func parseContainmentSupervisorArgs(args []string) (containmentSupervisorArgs, error) {
	set := flag.NewFlagSet("containment-supervisor", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	bootstrapHandleText := set.String("bootstrap-handle", "", "")
	ownerHandleText := set.String("owner-handle", "", "")
	if err := set.Parse(args); err != nil {
		return containmentSupervisorArgs{}, fmt.Errorf("parse containment supervisor arguments: %w", err)
	}
	if set.NArg() != 0 {
		return containmentSupervisorArgs{}, errors.New("containment supervisor does not accept positional arguments")
	}
	bootstrapValue, err := strconv.ParseUint(*bootstrapHandleText, 10, 64)
	if err != nil || bootstrapValue == 0 || uint64(syscall.Handle(bootstrapValue)) != bootstrapValue {
		return containmentSupervisorArgs{}, errors.New("containment supervisor bootstrap handle is invalid")
	}
	ownerValue, err := strconv.ParseUint(*ownerHandleText, 10, 64)
	if err != nil || ownerValue == 0 || uint64(syscall.Handle(ownerValue)) != ownerValue {
		return containmentSupervisorArgs{}, errors.New("containment supervisor owner handle is invalid")
	}
	return containmentSupervisorArgs{
		bootstrapHandle: syscall.Handle(bootstrapValue),
		ownerHandle:     syscall.Handle(ownerValue),
	}, nil
}

func validateSupervisorBootstrap(value containmentSupervisorBootstrap) error {
	if value.Version != containmentSupervisorProtocol || value.JobHandle == 0 || uint64(syscall.Handle(value.JobHandle)) != value.JobHandle {
		return errors.New("containment supervisor bootstrap is invalid")
	}
	if value.TargetPID <= 0 || strings.TrimSpace(value.TargetIdentity) == "" || value.SupervisorPID <= 0 || strings.TrimSpace(value.SupervisorIdentity) == "" {
		return errors.New("containment supervisor bootstrap process identity is invalid")
	}
	if !validContainmentToken(value.PipeToken) || !validContainmentToken(value.JobID) || !validContainmentSecret(value.Secret) {
		return errors.New("containment supervisor bootstrap authority is invalid")
	}
	if value.SupervisorPID != os.Getpid() {
		return errors.New("containment supervisor bootstrap PID mismatch")
	}
	identityPID, _, err := parseWindowsProcessIdentity(value.SupervisorIdentity)
	if err != nil || identityPID != value.SupervisorPID {
		return errors.New("containment supervisor bootstrap identity mismatch")
	}
	return nil
}

type containmentSupervisorStopState struct {
	mutex   sync.Mutex
	stopped bool
	active  uint32
	receipt *authority.StopReceipt
}

func (state *containmentSupervisorStopState) stop(job syscall.Handle) (uint32, error) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if state.stopped {
		return state.active, nil
	}
	active, err := terminateAndObserveSupervisorJob(job)
	if err != nil {
		return 0, err
	}
	if active != 0 {
		return 0, errors.New("containment supervisor stop did not prove an empty Job")
	}
	state.active = active
	state.stopped = true
	return active, nil
}

func (state *containmentSupervisorStopState) receiptFor(endpoint containmentSupervisorEndpoint) (authority.StopReceipt, bool) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if !state.stopped {
		return authority.StopReceipt{}, false
	}
	if state.receipt == nil {
		receipt := authority.StopReceipt{
			Version:            authority.SupervisorVersion,
			Status:             "stopped",
			TargetPID:          endpoint.TargetPID,
			TargetIdentity:     endpoint.TargetIdentity,
			PipeToken:          endpoint.Token,
			JobID:              endpoint.JobID,
			SupervisorPID:      endpoint.SupervisorPID,
			SupervisorIdentity: endpoint.SupervisorIdentity,
			ActiveProcesses:    state.active,
		}
		state.receipt = &receipt
	}
	return *state.receipt, true
}

func (state *containmentSupervisorStopState) stoppedSuccessfully() bool {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return state.stopped && state.active == 0
}

func (state *containmentSupervisorStopState) release(release func() error) error {
	if !state.stoppedSuccessfully() {
		return errContainmentSupervisorStopReceiptUnavailable
	}
	if release == nil {
		return errors.New("containment supervisor release callback is missing")
	}
	return release()
}

type containmentSupervisorLeaseState struct {
	mutex      sync.Mutex
	timer      *time.Timer
	stopped    bool
	expired    bool
	sequence   uint64
	generation uint64
	deadline   time.Duration
	deadlineAt time.Time
	onExpired  func()
}

func newContainmentSupervisorLeaseState(onExpired func()) *containmentSupervisorLeaseState {
	return &containmentSupervisorLeaseState{onExpired: onExpired}
}

func (state *containmentSupervisorLeaseState) renew(deadline time.Duration, sequence uint64) (bool, error) {
	if state == nil {
		return false, errContainmentSupervisorLeaseStopped
	}
	if deadline <= 0 || sequence == 0 {
		return false, errors.New("containment supervisor lease renewal is invalid")
	}
	state.mutex.Lock()
	if state.stopped {
		state.mutex.Unlock()
		return false, errContainmentSupervisorLeaseStopped
	}
	if state.expired {
		state.mutex.Unlock()
		return false, errContainmentSupervisorLeaseExpired
	}
	if !state.deadlineAt.IsZero() && !time.Now().Before(state.deadlineAt) {
		state.expired = true
		onExpired := state.onExpired
		state.mutex.Unlock()
		if onExpired != nil {
			onExpired()
		}
		return false, errContainmentSupervisorLeaseExpired
	}
	if sequence < state.sequence {
		state.mutex.Unlock()
		return false, fmt.Errorf("containment supervisor lease sequence %d is stale after %d", sequence, state.sequence)
	}
	if sequence == state.sequence {
		if deadline != state.deadline {
			state.mutex.Unlock()
			return false, fmt.Errorf("containment supervisor lease sequence %d changed its deadline", sequence)
		}
		state.mutex.Unlock()
		return true, nil
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.sequence = sequence
	state.generation++
	generation := state.generation
	state.deadline = deadline
	state.deadlineAt = time.Now().Add(deadline)
	state.timer = containmentSupervisorAfterFunc(deadline, func() { state.expire(generation) })
	state.mutex.Unlock()
	return false, nil
}

func (state *containmentSupervisorLeaseState) expire(generation uint64) {
	if state == nil {
		return
	}
	state.mutex.Lock()
	if state.stopped || state.expired || generation != state.generation {
		state.mutex.Unlock()
		return
	}
	state.expired = true
	onExpired := state.onExpired
	state.mutex.Unlock()
	if onExpired != nil {
		onExpired()
	}
}

func (state *containmentSupervisorLeaseState) stop() {
	if state == nil {
		return
	}
	state.mutex.Lock()
	state.stopped = true
	state.expired = true
	state.deadlineAt = time.Time{}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mutex.Unlock()
}

func runContainmentSupervisor(_ containmentSupervisorArgs, bootstrap containmentSupervisorBootstrap, watchdogs ...*containmentSupervisorOwnerWatchdog) error {
	var ownerWatchdog *containmentSupervisorOwnerWatchdog
	if len(watchdogs) > 0 {
		ownerWatchdog = watchdogs[0]
	}
	job := syscall.Handle(bootstrap.JobHandle)
	released := false
	release := func() error {
		if released {
			return nil
		}
		if err := closeJob(job); err != nil {
			return err
		}
		released = true
		return nil
	}
	stopState := containmentSupervisorStopState{}
	stopJobUntilEmpty := func() error {
		// One stop call is already bounded by containmentCloseDeadline. Keep
		// the named-pipe loop alive after an unproven attempt so an exact
		// authenticated recovery can retry; never block the helper forever in
		// a local retry loop.
		_, err := stopState.stop(job)
		return err
	}
	supervisorPID := os.Getpid()
	supervisorIdentity := bootstrap.SupervisorIdentity
	actualSupervisor, err := ProcessIdentity(supervisorPID)
	if err != nil || actualSupervisor != supervisorIdentity {
		stopErr := stopAndReleaseSupervisorJob(job, release)
		if err == nil {
			err = errors.New("containment supervisor identity mismatch")
		}
		return errors.Join(fmt.Errorf("capture containment supervisor identity: %w", err), stopErr)
	}
	endpoint := containmentSupervisorEndpoint{
		TargetPID:          bootstrap.TargetPID,
		TargetIdentity:     bootstrap.TargetIdentity,
		SupervisorPID:      supervisorPID,
		SupervisorIdentity: supervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + bootstrap.PipeToken,
		Token:              bootstrap.PipeToken,
		JobID:              bootstrap.JobID,
		Secret:             bootstrap.Secret,
	}
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		stopErr := stopAndReleaseSupervisorJob(job, release)
		return errors.Join(err, stopErr)
	}

	ownerLost := false
	ownerLossAttempted := false
	var recoveryRetained atomic.Bool
	leaseState := newContainmentSupervisorLeaseState(func() {
		if err := stopJobUntilEmpty(); err != nil {
			// Lease expiry is an independent stop boundary. Preserve the helper
			// and its Job authority when the first proof attempt fails so the
			// main loop can retry before any authenticated recovery or release.
			recoveryRetained.Store(true)
		}
	})
	var stopRetryMutex sync.Mutex
	stopRetryRunning := false
	nextStopRetry := time.Time{}
	scheduleRetainedStopRetry := func() {
		if stopState.stoppedSuccessfully() {
			return
		}
		now := time.Now()
		stopRetryMutex.Lock()
		if stopRetryRunning || now.Before(nextStopRetry) {
			stopRetryMutex.Unlock()
			return
		}
		stopRetryRunning = true
		nextStopRetry = now.Add(containmentSupervisorStopRetry)
		stopRetryMutex.Unlock()
		go func() {
			_, _ = stopState.stop(job)
			stopRetryMutex.Lock()
			stopRetryRunning = false
			stopRetryMutex.Unlock()
		}()
	}
	retainAfterStopAttempt := func() error {
		// Once a protocol/pipe failure has triggered a stop attempt, keep the
		// helper and its Job authority available for an authenticated recovery.
		// A failed proof must not release the Job or destroy the witness.
		leaseState.stop()
		recoveryRetained.Store(true)
		_, stopErr := stopState.stop(job)
		if stopErr != nil {
			scheduleRetainedStopRetry()
		}
		return stopErr
	}
	refreshOwnerLoss := func() {
		if ownerLost || ownerWatchdog == nil {
			return
		}
		select {
		case <-ownerWatchdog.ownerLost():
			ownerLost = true
		default:
		}
	}
	for {
		refreshOwnerLoss()
		if recoveryRetained.Load() && !stopState.stoppedSuccessfully() {
			scheduleRetainedStopRetry()
		}
		if ownerLost && !ownerLossAttempted {
			// Keep the helper alive after a proven owner-loss stop so a restarted
			// daemon can obtain and persist the exact stop receipt through recover.
			leaseState.stop()
			stopJobUntilEmpty()
			recoveryRetained.Store(true)
			ownerLossAttempted = true
		}

		pipe, err := createContainmentSupervisorPipe(endpoint.PipeName)
		if err != nil {
			if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
				if stopState.stoppedSuccessfully() {
					recoveryRetained.Store(true)
				}
			} else {
				_ = retainAfterStopAttempt()
			}
			time.Sleep(containmentSupervisorProbeInterval)
			continue
		}
		deadline := time.Now().Add(containmentSupervisorConnectionPoll)
		connected, err := connectContainmentSupervisorPipe(pipe, deadline)
		if err != nil {
			_ = pipe.Close()
			refreshOwnerLoss()
			if errors.Is(err, errContainmentSupervisorConnectionPoll) || ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
				if stopState.stoppedSuccessfully() {
					recoveryRetained.Store(true)
				}
				continue
			}
			_ = retainAfterStopAttempt()
			continue
		}
		requestLine, readErr := readSupervisorLine(pipe, time.Now().Add(containmentSupervisorConnectionPoll))
		if readErr != nil {
			_ = pipe.Close()
			refreshOwnerLoss()
			if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() || !connected {
				continue
			}
			_ = retainAfterStopAttempt()
			continue
		}
		refreshOwnerLoss()
		var request containmentSupervisorRequest
		decodeErr := decodeSupervisorJSON(requestLine, &request)
		if decodeErr != nil {
			_ = pipe.Close()
			if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
				if stopState.stoppedSuccessfully() {
					recoveryRetained.Store(true)
				}
				continue
			}
			_ = retainAfterStopAttempt()
			continue
		}
		if err := validateSupervisorRequest(endpoint, request); err != nil {
			_ = pipe.Close()
			if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
				if stopState.stoppedSuccessfully() {
					recoveryRetained.Store(true)
				}
				continue
			}
			_ = retainAfterStopAttempt()
			continue
		}
		response := containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          request.Operation,
			Sequence:           request.Sequence,
			TargetPID:          endpoint.TargetPID,
			TargetIdentity:     endpoint.TargetIdentity,
			SupervisorPID:      endpoint.SupervisorPID,
			SupervisorIdentity: endpoint.SupervisorIdentity,
			Token:              endpoint.Token,
			JobID:              endpoint.JobID,
		}
		if request.Operation == "hello" {
			if ownerLost {
				response.Status = "error"
				response.Error = "containment supervisor owner was lost; recovery is required"
			} else {
				response.Status = "ready"
				response.ActiveProcesses, err = queryJobActiveProcesses(job)
				if err != nil {
					response.Status = "error"
					response.Error = fmt.Sprintf("query Job during supervisor handshake: %v", err)
				}
			}
			writeErr := writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
			_ = pipe.Close()
			if writeErr != nil {
				if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
					if stopState.stoppedSuccessfully() {
						recoveryRetained.Store(true)
					}
					continue
				}
				_ = retainAfterStopAttempt()
				continue
			}
			if response.Status == "error" {
				if ownerLost || recoveryRetained.Load() || stopState.stoppedSuccessfully() {
					if stopState.stoppedSuccessfully() {
						recoveryRetained.Store(true)
					}
					continue
				}
				_ = retainAfterStopAttempt()
				continue
			}
			continue
		}
		if request.Operation == "renew" {
			if ownerLost {
				response.Status = "stopped"
				response.Error = "containment supervisor owner was lost; recovery is required"
			} else if wasReplay, renewErr := leaseState.renew(time.Duration(request.DeadlineMS)*time.Millisecond, request.Sequence); renewErr != nil {
				response.Status = "stopped"
				response.Error = renewErr.Error()
			} else {
				if wasReplay {
					response.Status = "already_renewed"
				} else {
					response.Status = "renewed"
				}
			}
			writeErr := writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
			_ = pipe.Close()
			if writeErr != nil {
				continue
			}
			continue
		}
		if request.Operation == "release" {
			if !stopState.stoppedSuccessfully() {
				response.Status = "error"
				response.Error = errContainmentSupervisorStopReceiptUnavailable.Error()
			} else if releaseErr := stopState.release(release); releaseErr != nil {
				response.Status = "error"
				response.Error = releaseErr.Error()
			} else {
				if ownerWatchdog != nil {
					ownerWatchdog.disarm()
				}
				response.Status = "released"
			}
			writeErr := writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
			_ = pipe.Close()
			if writeErr != nil {
				if response.Status == "released" {
					return nil
				}
				continue
			}
			if response.Status == "released" {
				return nil
			}
			continue
		}

		// Both explicit close and persisted recovery are terminal operations.
		// Latch no-more-renewals first, then prove an empty Job before disarming
		// the owner watchdog. A failed stop therefore retains an independent
		// owner/receipt path instead of silently losing the stop responsibility.
		wasAlreadyStopped := stopState.stoppedSuccessfully()
		leaseState.stop()
		response.Status = "stopped"
		response.ActiveProcesses, err = stopState.stop(job)
		if err != nil {
			response.Status = "error"
			response.Error = err.Error()
		} else {
			receipt, receiptOK := stopState.receiptFor(endpoint)
			if !receiptOK {
				response.Status = "error"
				response.Error = errContainmentSupervisorStopReceiptUnavailable.Error()
			} else {
				if request.Operation == "recover" || wasAlreadyStopped {
					recoveryRetained.Store(true)
				}
				response.ActiveProcesses = receipt.ActiveProcesses
				if ownerWatchdog != nil {
					ownerWatchdog.disarm()
				}
			}
		}
		writeErr := writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
		_ = pipe.Close()
		if writeErr != nil {
			// Keep a proven or unresolved Job authority alive for a retry. A
			// lost response must not turn an unknown stop into a success.
			continue
		}
		if response.Status != "stopped" {
			continue
		}
		if request.Operation == "close" || request.Operation == "recover" || recoveryRetained.Load() {
			continue
		}
		if releaseErr := release(); releaseErr != nil {
			// The Job is already proven empty; retain the helper and let the
			// next exact recovery request retry handle release.
			continue
		}
		return nil
	}
}

func validateSupervisorRequest(endpoint containmentSupervisorEndpoint, request containmentSupervisorRequest) error {
	if request.Version != containmentSupervisorProtocol {
		return fmt.Errorf("unexpected containment supervisor request version %d", request.Version)
	}
	if request.Operation != "hello" && request.Operation != "close" && request.Operation != "recover" && request.Operation != "renew" && request.Operation != "release" {
		return fmt.Errorf("unknown containment supervisor operation %q", request.Operation)
	}
	if request.Operation == "renew" {
		if request.Sequence == 0 || request.DeadlineMS <= 0 {
			return errors.New("containment supervisor renew requires positive sequence and deadline")
		}
	} else if request.Sequence != 0 || request.DeadlineMS != 0 {
		return errors.New("containment supervisor sequence and deadline are only valid for renew")
	}
	if request.TargetPID != endpoint.TargetPID || request.TargetIdentity != endpoint.TargetIdentity {
		return errors.New("containment supervisor target identity mismatch")
	}
	if request.Token != endpoint.Token || request.JobID != endpoint.JobID {
		return errors.New("containment supervisor Job identity mismatch")
	}
	if request.Secret != endpoint.Secret {
		return errors.New("containment supervisor authority secret mismatch")
	}
	if request.SupervisorPID != endpoint.SupervisorPID || request.SupervisorIdentity != endpoint.SupervisorIdentity {
		return errors.New("containment supervisor process identity mismatch")
	}
	return nil
}

func writeSupervisorResponse(file *os.File, response containmentSupervisorResponse, deadline time.Time) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return writeSupervisorLine(file, encoded, deadline)
}

func createContainmentSupervisorPipe(name string) (*os.File, error) {
	handle, err := windows.CreateNamedPipe(
		windows.StringToUTF16Ptr(name),
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		containmentSupervisorMaxLine,
		containmentSupervisorMaxLine,
		1000,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func connectContainmentSupervisorPipe(pipe *os.File, deadline time.Time) (bool, error) {
	result := make(chan error, 1)
	go func() {
		err := windows.ConnectNamedPipe(windows.Handle(pipe.Fd()), nil)
		if errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			err = nil
		}
		result <- err
	}()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		_ = pipe.Close()
		<-result
		return false, errContainmentSupervisorConnectionPoll
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case err := <-result:
		return err == nil, err
	case <-timer.C:
		_ = pipe.Close()
		<-result
		return false, errContainmentSupervisorConnectionPoll
	}
}

func terminateAndObserveSupervisorJob(job syscall.Handle) (uint32, error) {
	if err := terminateJob(job); err != nil {
		return 0, fmt.Errorf("terminate containment Job: %w", err)
	}
	if err := waitForEmptyJob(job, time.Now().Add(containmentCloseDeadline), queryJobActiveProcesses); err != nil {
		return 0, err
	}
	active, err := queryJobActiveProcesses(job)
	if err != nil {
		return 0, fmt.Errorf("query containment Job after stop: %w", err)
	}
	return active, nil
}

func stopAndReleaseSupervisorJob(job syscall.Handle, release func() error) error {
	active, stopErr := terminateAndObserveSupervisorJob(job)
	if stopErr != nil {
		return stopErr
	}
	if active != 0 {
		return fmt.Errorf("containment supervisor stop did not prove an empty Job: %d active processes remain", active)
	}
	if release == nil {
		return errors.New("containment supervisor release callback is missing")
	}
	return release()
}
