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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/windows"
)

const (
	containmentSupervisorProtocol       = 1
	containmentSupervisorPipePrefix     = `\\.\pipe\symmetry-containment-`
	containmentSupervisorJobPrefix      = `Local\symmetry-containment-`
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

var errContainmentSupervisorOwnerLost = errors.New("containment supervisor owner was lost; recovery is required")

const containmentSupervisorOwnerLostMessage = "containment supervisor owner was lost; recovery is required"

// SupervisorHandoffStage identifies which side of the durable handoff was
// reached when a helper reports owner loss.
type SupervisorHandoffStage string

const (
	SupervisorHandoffStagePreAuth  SupervisorHandoffStage = "pre_auth"
	SupervisorHandoffStagePostAuth SupervisorHandoffStage = "post_auth"
)

// SupervisorOwnerLostError is returned for a post-auth bootstrap replay after
// the helper latched daemon-owner EOF. The returned handoff remains exact and
// can be passed to RecoverPreparedSupervisor; callers must not bootstrap it
// again. Pre-auth owner loss remains an ordinary bootstrap failure because no
// authenticated helper handoff exists yet.
type SupervisorOwnerLostError struct {
	Stage SupervisorHandoffStage
	Err   error
}

func (err *SupervisorOwnerLostError) Error() string {
	if err == nil {
		return "supervisor owner was lost"
	}
	if err.Err == nil {
		return fmt.Sprintf("supervisor owner was lost during %s; recovery is required", err.Stage)
	}
	return fmt.Sprintf("supervisor owner was lost during %s; recovery is required: %v", err.Stage, err.Err)
}

func (err *SupervisorOwnerLostError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// IsSupervisorOwnerLost reports whether err is the stage-aware post-auth
// owner-loss result. It is intentionally false for pre-auth transport errors.
func IsSupervisorOwnerLost(err error) bool {
	var ownerLost *SupervisorOwnerLostError
	return errors.As(err, &ownerLost)
}

type containmentSupervisorEndpoint struct {
	LaunchToken        string
	TargetPID          int
	TargetIdentity     string
	SupervisorPID      int
	SupervisorIdentity string
	PipeName           string
	Token              string
	JobID              string
	JobName            string
	Secret             string
}

type containmentSupervisorRequest struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	LaunchToken        string `json:"launch_token,omitempty"`
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
	LaunchToken        string `json:"launch_token,omitempty"`
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
	Version int `json:"version"`
	// JobHandle is retained for the legacy inherited-handle bootstrap. Durable
	// handoff bootstraps leave it zero and use JobName so the helper opens the
	// exact named Job after authentication.
	JobHandle          uint64 `json:"job_handle,omitempty"`
	JobName            string `json:"job_name,omitempty"`
	LaunchToken        string `json:"launch_token,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	PipeToken          string `json:"pipe_token"`
	JobID              string `json:"job_id"`
	Secret             string `json:"secret"`
}

// SupervisorHandoffCallbacks are the only durable-state hooks owned by the
// caller. Prepare must complete before the independent helper is started;
// Bind is called only after the daemon has verified the helper's pipe server
// PID and Windows process-creation identity on the same connection.
type SupervisorHandoffCallbacks struct {
	Prepare func(authority.SupervisorHandoff) error
	Bind    func(authority.SupervisorHandoff, int, string) error
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
	writeContainmentSupervisorHello     = writeSupervisorResponse
	openContainmentSupervisorPipe       = openContainmentSupervisorPipeFile
	getContainmentSupervisorServerPID   = windows.GetNamedPipeServerProcessId
	containmentSupervisorConnectPending = func() {}
	readContainmentSupervisorLine       = readSupervisorLine
	runContainmentSupervisorAfterAuth   = runContainmentSupervisorWithJob
	processIdentityRunning              = processIdentityIsRunning
	getCurrentContainmentSessionID      = currentContainmentSessionID
	openSupervisorObservationProcess    = func(pid int) (syscall.Handle, error) {
		return openProcessForIdentity(pid, windows.SYNCHRONIZE)
	}
	readSupervisorObservationIdentity = processIdentityFromHandle
	waitSupervisorObservationProcess  = func(handle syscall.Handle) (uint32, error) {
		return windows.WaitForSingleObject(windows.Handle(handle), 0)
	}
	closeSupervisorObservationProcess = syscall.CloseHandle
	killContainmentSupervisor         = func(waiter *supervisorProcessWait, deadline time.Time) error {
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

// bootstrapContainmentSupervisor performs the authenticated first exchange
// on the exact pre-auth pipe connection. The secret-bearing bootstrap is
// written only after the server PID and creation identity have been checked
// and the caller's durable CAS bind has succeeded.
func bootstrapContainmentSupervisor(ctx context.Context, endpoint containmentSupervisorEndpoint, handoff authority.SupervisorHandoff, bind func(authority.SupervisorHandoff, int, string) error) error {
	if ctx == nil {
		return errors.New("durable containment bootstrap context is nil")
	}
	if err := handoff.Validate(); err != nil {
		return err
	}
	if endpoint.LaunchToken != handoff.LaunchToken || endpoint.Token != handoff.PipeToken || endpoint.JobID != handoff.JobID {
		return errors.New("durable containment bootstrap endpoint does not match handoff")
	}
	deadline := time.Now().Add(containmentSupervisorConnect)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = context.DeadlineExceeded
			}
			return fmt.Errorf("durable containment bootstrap connection timeout: %w", lastErr)
		}
		pipe, err := openContainmentSupervisorPipe(endpoint.PipeName)
		if err != nil {
			lastErr = err
			if !retryableSupervisorPipeError(err) {
				return fmt.Errorf("open durable containment bootstrap pipe: %w", err)
			}
			if waitErr := waitForContainmentSupervisorRetry(ctx, deadline); waitErr != nil {
				if errors.Is(waitErr, context.DeadlineExceeded) {
					continue
				}
				return fmt.Errorf("durable containment bootstrap cancelled: %w", waitErr)
			}
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		serverPID := uint32(0)
		if err := withContainmentSupervisorPipeHandle(pipe, func(handle windows.Handle) error {
			return getContainmentSupervisorServerPID(handle, &serverPID)
		}); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("read durable containment bootstrap pipe owner: %w", err)
		}
		if endpoint.SupervisorPID != 0 && int(serverPID) != endpoint.SupervisorPID {
			_ = pipe.Close()
			return fmt.Errorf("durable containment bootstrap pipe owner PID %d does not match %d", serverPID, endpoint.SupervisorPID)
		}
		serverIdentity, err := readSupervisorProcessIdentity(int(serverPID))
		if err != nil {
			_ = pipe.Close()
			return fmt.Errorf("read durable containment bootstrap pipe owner identity: %w", err)
		}
		if endpoint.SupervisorIdentity != "" && serverIdentity != endpoint.SupervisorIdentity {
			_ = pipe.Close()
			return errors.New("durable containment bootstrap pipe owner identity mismatch")
		}
		if endpoint.SupervisorPID == 0 {
			endpoint.SupervisorPID = int(serverPID)
			endpoint.SupervisorIdentity = serverIdentity
		}
		bound := handoff.Clone()
		if bound.SupervisorPID != 0 || bound.SupervisorIdentity != "" {
			if bound.SupervisorPID != int(serverPID) || bound.SupervisorIdentity != serverIdentity {
				_ = pipe.Close()
				return errors.New("durable containment bootstrap helper identity mismatch")
			}
		} else {
			if bind == nil {
				_ = pipe.Close()
				return errors.New("durable containment bootstrap bind callback is required")
			}
			if err := bind(bound.Clone(), int(serverPID), serverIdentity); err != nil {
				_ = pipe.Close()
				return fmt.Errorf("bind durable containment supervisor identity: %w", err)
			}
			bound.SupervisorPID = int(serverPID)
			bound.SupervisorIdentity = serverIdentity
		}
		if err := ctx.Err(); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		bootstrap := containmentSupervisorBootstrap{
			Version:            containmentSupervisorProtocol,
			JobName:            endpoint.JobName,
			LaunchToken:        endpoint.LaunchToken,
			TargetPID:          endpoint.TargetPID,
			TargetIdentity:     endpoint.TargetIdentity,
			SupervisorPID:      endpoint.SupervisorPID,
			SupervisorIdentity: endpoint.SupervisorIdentity,
			PipeToken:          endpoint.Token,
			JobID:              endpoint.JobID,
			Secret:             endpoint.Secret,
		}
		if err := writeContainmentSupervisorBootstrapContext(ctx, pipe, bootstrap, deadline); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("send durable containment supervisor bootstrap: %w", err)
		}
		if err := ctx.Err(); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		line, err := readContainmentSupervisorLineContext(ctx, pipe, deadline)
		_ = pipe.Close()
		if err != nil {
			return fmt.Errorf("read durable containment supervisor hello: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		var response containmentSupervisorResponse
		if err := decodeSupervisorJSON(line, &response); err != nil {
			return fmt.Errorf("decode durable containment supervisor hello: %w", err)
		}
		if err := validateSupervisorResponse(endpoint, "hello", response); err != nil {
			return fmt.Errorf("validate durable containment supervisor hello: %w", err)
		}
		if response.Status != "ready" || response.ActiveProcesses == 0 {
			return errors.New("durable containment supervisor hello did not prove an active Job")
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("durable containment bootstrap cancelled: %w", err)
		}
		return nil
	}
}

// DiscoverPreparedSupervisor reads only the pre-auth pipe server identity. It
// never sends the secret and therefore cannot authorize a bootstrap by itself.
// A missing pipe, PID, or Job is returned as an error rather than interpreted
// as an abort or a successful stop.
func DiscoverPreparedSupervisor(handoff authority.SupervisorHandoff) (authority.SupervisorHandoff, error) {
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoff{}, err
	}
	if handoff.SupervisorPID == 0 || handoff.SupervisorIdentity == "" {
		return authority.SupervisorHandoff{}, errors.New("unbound supervisor handoff requires an explicit abort proof")
	}
	endpoint := containmentSupervisorEndpoint{
		LaunchToken:        handoff.LaunchToken,
		TargetPID:          handoff.TargetPID,
		TargetIdentity:     handoff.TargetIdentity,
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + handoff.PipeToken,
		Token:              handoff.PipeToken,
		JobID:              handoff.JobID,
		JobName:            containmentJobName(handoff.JobID),
		Secret:             handoff.Secret,
	}
	deadline := time.Now().Add(containmentSupervisorConnectionPoll)
	for {
		pipe, err := openContainmentSupervisorPipe(endpoint.PipeName)
		if err == nil {
			serverPID := uint32(0)
			if err := withContainmentSupervisorPipeHandle(pipe, func(handle windows.Handle) error {
				return getContainmentSupervisorServerPID(handle, &serverPID)
			}); err != nil {
				_ = pipe.Close()
				return authority.SupervisorHandoff{}, fmt.Errorf("read prepared supervisor pipe owner: %w", err)
			}
			serverIdentity, err := readSupervisorProcessIdentity(int(serverPID))
			_ = pipe.Close()
			if err != nil {
				return authority.SupervisorHandoff{}, fmt.Errorf("read prepared supervisor identity: %w", err)
			}
			if endpoint.SupervisorPID != 0 && endpoint.SupervisorPID != int(serverPID) {
				return authority.SupervisorHandoff{}, errors.New("prepared supervisor PID mismatch")
			}
			if endpoint.SupervisorIdentity != "" && endpoint.SupervisorIdentity != serverIdentity {
				return authority.SupervisorHandoff{}, errors.New("prepared supervisor creation identity mismatch")
			}
			bound := handoff.Clone()
			bound.SupervisorPID = int(serverPID)
			bound.SupervisorIdentity = serverIdentity
			if err := bound.Validate(); err != nil {
				return authority.SupervisorHandoff{}, err
			}
			return bound, nil
		}
		if !retryableSupervisorPipeError(err) || time.Now().After(deadline) {
			return authority.SupervisorHandoff{}, fmt.Errorf("discover prepared supervisor pipe: %w", err)
		}
		time.Sleep(containmentSupervisorProbeInterval)
	}
}

// BootstrapPreparedSupervisor authenticates a helper discovered through its
// pre-auth pipe. When the handoff has no helper identity, Bind is invoked after
// the same-connection server identity check and before the secret-bearing
// bootstrap is written.
func BootstrapPreparedSupervisor(handoff authority.SupervisorHandoff, bind func(authority.SupervisorHandoff, int, string) error) (authority.SupervisorHandoff, error) {
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoff{}, err
	}
	if handoff.SupervisorPID == 0 || handoff.SupervisorIdentity == "" {
		return authority.SupervisorHandoff{}, errors.New("unbound supervisor handoff requires an explicit abort proof")
	}
	endpoint := containmentSupervisorEndpoint{
		LaunchToken:        handoff.LaunchToken,
		TargetPID:          handoff.TargetPID,
		TargetIdentity:     handoff.TargetIdentity,
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + handoff.PipeToken,
		Token:              handoff.PipeToken,
		JobID:              handoff.JobID,
		JobName:            containmentJobName(handoff.JobID),
		Secret:             handoff.Secret,
	}
	bound := handoff.Clone()
	if err := bootstrapContainmentSupervisor(context.Background(), endpoint, handoff, nil); err != nil {
		if handoff.SupervisorPID != 0 && errors.Is(err, errContainmentSupervisorOwnerLost) {
			return handoff.Clone(), &SupervisorOwnerLostError{
				Stage: SupervisorHandoffStagePostAuth,
				Err:   err,
			}
		}
		return authority.SupervisorHandoff{}, err
	}
	if err := bound.Validate(); err != nil {
		return authority.SupervisorHandoff{}, err
	}
	return bound, nil
}

// AbortPreparedSupervisor is an explicit live-daemon pre-authority stop. The
// helper refuses abort after owner EOF; callers must use Recover and retain
// the durable stop receipt in that case.
func AbortPreparedSupervisor(handoff authority.SupervisorHandoff) (authority.StopReceipt, error) {
	return stopPreparedSupervisor(handoff, "abort")
}

// RecoverPreparedSupervisor obtains the exact replayable empty-Job witness
// after owner loss or an uncertain commit boundary.
func RecoverPreparedSupervisor(handoff authority.SupervisorHandoff) (authority.StopReceipt, error) {
	if handoff.StopReceipt != nil && handoff.StopReceipt.ValidForHandoff(handoff) {
		return *handoff.StopReceipt, nil
	}
	return stopPreparedSupervisor(handoff, "recover")
}

func stopPreparedSupervisor(handoff authority.SupervisorHandoff, operation string) (authority.StopReceipt, error) {
	if err := handoff.Validate(); err != nil {
		return authority.StopReceipt{}, err
	}
	endpoint, err := containmentEndpointForHandoff(handoff)
	if err != nil {
		return authority.StopReceipt{}, err
	}
	response, err := requestContainmentSupervisor(endpoint, operation, time.Now().Add(containmentSupervisorConnect))
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%s prepared supervisor: %w", operation, err)
	}
	if err := validateSupervisorResponse(endpoint, operation, response); err != nil {
		return authority.StopReceipt{}, err
	}
	if (operation == "abort" && response.Status != "aborted") || (operation == "recover" && response.Status != "stopped") || response.ActiveProcesses != 0 {
		return authority.StopReceipt{}, fmt.Errorf("prepared supervisor %s did not prove an empty Job", operation)
	}
	return authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		TargetPID:          response.TargetPID,
		TargetIdentity:     response.TargetIdentity,
		PipeToken:          response.Token,
		JobID:              response.JobID,
		SupervisorPID:      response.SupervisorPID,
		SupervisorIdentity: response.SupervisorIdentity,
		ActiveProcesses:    response.ActiveProcesses,
	}, nil
}

// ReleasePreparedSupervisor releases the helper only after the exact stop
// receipt has been durably recorded by the caller.
func ReleasePreparedSupervisor(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	if handoff.StopReceipt == nil || !handoff.StopReceipt.ValidForHandoff(handoff) {
		return authority.SupervisorHandoffReleaseProof{}, errContainmentSupervisorStopReceiptUnavailable
	}
	return releasePreparedSupervisor(handoff)
}

// ReleaseAbortedSupervisor is the explicit pre-authority counterpart to
// ReleasePreparedSupervisor. It is intentionally separate so a caller cannot
// accidentally release an uncertain/committed handoff without a durable stop
// receipt.
func ReleaseAbortedSupervisor(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	return releasePreparedSupervisor(handoff)
}

func releasePreparedSupervisor(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	endpoint, err := containmentEndpointForHandoff(handoff)
	if err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	response, err := requestContainmentSupervisor(endpoint, "release", time.Now().Add(containmentSupervisorConnect))
	if err != nil {
		if handoff.StopReceipt != nil && handoff.StopReceipt.ValidForHandoff(handoff) {
			if proof, replayErr := releaseProofAfterLostResponse(endpoint, handoff); replayErr == nil {
				return proof, nil
			} else {
				return authority.SupervisorHandoffReleaseProof{}, errors.Join(
					fmt.Errorf("release prepared supervisor: %w", err),
					replayErr,
				)
			}
		}
		return authority.SupervisorHandoffReleaseProof{}, fmt.Errorf("release prepared supervisor: %w", err)
	}
	if err := validateSupervisorResponse(endpoint, "release", response); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	if response.Status != "released" || response.ActiveProcesses != 0 {
		return authority.SupervisorHandoffReleaseProof{}, errors.New("prepared supervisor release was not acknowledged")
	}
	return handoff.ReleaseProof(), nil
}

func releaseProofAfterLostResponse(endpoint containmentSupervisorEndpoint, handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	if err := proveSupervisorExitedOrAbsent(endpoint.SupervisorPID, endpoint.SupervisorIdentity); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, fmt.Errorf("prove prepared supervisor exit after lost release response: %w", err)
	}
	return handoff.ReleaseProof(), nil
}

// proveSupervisorExitedOrAbsent proves that the exact helper is either gone,
// replaced by a different process identity, or exited. A matching identity
// with WAIT_TIMEOUT is deliberately unresolved: a missing pipe is not proof
// that a live helper released its authority.
func proveSupervisorExitedOrAbsent(pid int, expectedIdentity string) error {
	handle, err := openSupervisorObservationProcess(pid)
	if err != nil {
		if supervisorProcessMissing(err) {
			return nil
		}
		return fmt.Errorf("open helper observation process: %w", err)
	}
	defer func() { _ = closeSupervisorObservationProcess(handle) }()

	actualIdentity, err := readSupervisorObservationIdentity(pid, handle)
	if err != nil {
		return fmt.Errorf("read helper observation identity: %w", err)
	}
	if actualIdentity != expectedIdentity {
		return nil
	}

	waitResult, err := waitSupervisorObservationProcess(handle)
	if err != nil {
		return fmt.Errorf("wait for helper observation: %w", err)
	}
	switch waitResult {
	case waitObject0:
		return nil
	case waitTimeout:
		return errors.New("helper with exact identity is still live")
	default:
		return fmt.Errorf("wait for helper observation returned %#x", waitResult)
	}
}

func supervisorProcessMissing(err error) bool {
	return errors.Is(err, syscall.Errno(2)) || errors.Is(err, syscall.Errno(87)) || errors.Is(err, os.ErrNotExist)
}

func supervisorPipeMissing(err error) bool {
	return errors.Is(err, syscall.Errno(2)) || errors.Is(err, os.ErrNotExist)
}

func processIdentityIsRunning(pid int) (bool, error) {
	handle, err := openProcessForIdentity(pid, 0)
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(windows.Handle(handle), &exitCode); err != nil {
		return false, err
	}
	return exitCode == 259, nil // STILL_ACTIVE
}

// SupervisorHandoffAbortVerifier is supplied by the caller that still owns
// the current daemon/store lifetime. It must return nil only after the old
// writer is quiesced; a boolean or an absent pipe is not sufficient proof.
type SupervisorHandoffAbortVerifier func() error

// ProvePreparedSupervisorAborted proves a definitive pre-authority abort for
// an exact prepared handoff. It requires caller-owned old-writer quiescence,
// an exact creator/recovery Windows session, an absent exact named Job, an
// absent exact target identity, and (when bound) an absent exact helper
// identity. PID reuse, a recreated Job, an access error, or any other unproven
// absence fails closed.
func ProvePreparedSupervisorAborted(handoff authority.SupervisorHandoff, verifyOldOwnerQuiesced SupervisorHandoffAbortVerifier) (authority.SupervisorHandoffAbortProof, error) {
	if verifyOldOwnerQuiesced == nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff abort verifier is required")
	}
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if handoff.StopReceipt != nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff abort cannot carry a stop receipt")
	}
	if handoff.CreatorSessionID == nil {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff creator session is unavailable")
	}
	recoverySessionID, err := getCurrentContainmentSessionID()
	if err != nil {
		return authority.SupervisorHandoffAbortProof{}, fmt.Errorf("capture supervisor handoff recovery session: %w", err)
	}
	if recoverySessionID != *handoff.CreatorSessionID {
		return authority.SupervisorHandoffAbortProof{}, fmt.Errorf("supervisor handoff creator session %d does not match recovery session %d", *handoff.CreatorSessionID, recoverySessionID)
	}
	targetPID, _, err := parseWindowsProcessIdentity(handoff.TargetIdentity)
	if err != nil || targetPID != handoff.TargetPID {
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff target identity does not match PID")
	}
	if err := verifyOldOwnerQuiesced(); err != nil {
		return authority.SupervisorHandoffAbortProof{}, fmt.Errorf("verify old supervisor owner quiescence: %w", err)
	}
	if err := proveContainmentJobAbsent(handoff.JobID); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if err := proveExactProcessAbsent(handoff.TargetPID, handoff.TargetIdentity, "target"); err != nil {
		return authority.SupervisorHandoffAbortProof{}, err
	}
	if handoff.SupervisorPID > 0 {
		if err := proveExactProcessAbsent(handoff.SupervisorPID, handoff.SupervisorIdentity, "supervisor"); err != nil {
			return authority.SupervisorHandoffAbortProof{}, err
		}
	}
	proof := authority.SupervisorHandoffAbortProof{
		Version:            authority.SupervisorHandoffAbortProofVersion,
		Disposition:        authority.SupervisorHandoffAbortDisposition,
		LaunchToken:        handoff.LaunchToken,
		JobID:              handoff.JobID,
		CreatorSessionID:   cloneContainmentSessionID(handoff.CreatorSessionID),
		TargetPID:          handoff.TargetPID,
		TargetIdentity:     handoff.TargetIdentity,
		SupervisorPID:      handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity,
	}
	if err := proof.Validate(); err != nil || !proof.ValidFor(handoff) {
		if err != nil {
			return authority.SupervisorHandoffAbortProof{}, err
		}
		return authority.SupervisorHandoffAbortProof{}, errors.New("supervisor handoff abort proof does not match handoff")
	}
	return proof, nil
}

func proveContainmentJobAbsent(jobID string) error {
	if !validContainmentToken(jobID) {
		return errors.New("supervisor handoff abort Job identifier is invalid")
	}
	handle, err := openNamedContainmentJob(containmentJobName(jobID))
	if err == nil {
		_ = closeJob(handle)
		return errors.New("supervisor handoff abort named Job still exists")
	}
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, syscall.Errno(2)) {
		return nil
	}
	return fmt.Errorf("supervisor handoff abort named Job absence is unproven: %w", err)
}

func proveExactProcessAbsent(pid int, expectedIdentity, label string) error {
	actualIdentity, err := readSupervisorProcessIdentity(pid)
	if err != nil {
		if supervisorProcessMissing(err) {
			return nil
		}
		return fmt.Errorf("supervisor handoff abort %s process absence is unproven: %w", label, err)
	}
	if actualIdentity != expectedIdentity {
		return fmt.Errorf("supervisor handoff abort %s process identity mismatch", label)
	}
	running, runningErr := processIdentityRunning(pid)
	if runningErr != nil {
		if supervisorProcessMissing(runningErr) {
			return nil
		}
		return fmt.Errorf("supervisor handoff abort %s process liveness is unproven: %w", label, runningErr)
	}
	if running {
		return fmt.Errorf("supervisor handoff abort %s process still exists", label)
	}
	return nil
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
// rearm path. A lost release acknowledgement additionally requires an exact
// helper exit/absence observation before the authority can be cleared.
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
	actualIdentity, identityErr := readSupervisorProcessIdentity(endpoint.SupervisorPID)
	if identityErr != nil {
		// A durable stop receipt makes a missing helper harmless only after the
		// exact helper identity has been independently observed absent. Do not
		// turn one failed identity read into release proof by itself.
		if supervisorProcessMissing(identityErr) {
			if observationErr := proveSupervisorExitedOrAbsent(endpoint.SupervisorPID, endpoint.SupervisorIdentity); observationErr == nil {
				return nil
			} else {
				return fmt.Errorf("%w: prove helper exit after missing release identity: %v", errContainmentStopUnproven, observationErr)
			}
		}
		return fmt.Errorf("%w: read persisted supervisor identity: %v", errContainmentStopUnproven, identityErr)
	}
	if actualIdentity != endpoint.SupervisorIdentity {
		return fmt.Errorf("%w: persisted supervisor identity mismatch", errContainmentStopUnproven)
	}
	deadline := time.Now().Add(containmentSupervisorConnect)
	response, err := requestContainmentSupervisor(endpoint, "release", deadline)
	if err != nil {
		// A durable empty-Job receipt makes a missing helper pipe an idempotent
		// release replay only after the exact helper process is independently
		// proven exited or absent. A live helper, PID reuse, or observation error
		// must retain the authority as unresolved.
		if supervisorPipeMissing(err) {
			if observationErr := proveSupervisorExitedOrAbsent(endpoint.SupervisorPID, endpoint.SupervisorIdentity); observationErr == nil {
				return nil
			} else {
				return fmt.Errorf("%w: prove helper exit after missing release pipe: %v", errContainmentStopUnproven, observationErr)
			}
		}
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

// AttachSuspendedProcessWithHandoff is the durable suspended-launch boundary.
// It records the immutable handoff before starting the helper, binds the
// helper identity only after same-connection pipe validation, and returns the
// target's primary-thread resume responsibility to the caller. The caller
// must commit authority and arm its initial lease before invoking Resume on the
// returned SuspendedProcess.
func AttachSuspendedProcessWithHandoff(ctx context.Context, process *SuspendedProcess, callbacks SupervisorHandoffCallbacks) (Containment, authority.SupervisorHandoff, string, error) {
	if ctx == nil {
		return nil, authority.SupervisorHandoff{}, "", errors.New("suspended process context is nil")
	}
	if process == nil {
		return nil, authority.SupervisorHandoff{}, "", errors.New("suspended process is required")
	}
	if callbacks.Prepare == nil {
		return nil, authority.SupervisorHandoff{}, "", errors.New("supervisor handoff prepare callback is required")
	}
	if callbacks.Bind == nil {
		return nil, authority.SupervisorHandoff{}, "", errors.New("supervisor handoff bind callback is required")
	}

	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return nil, authority.SupervisorHandoff{}, "", errors.New("suspended process is already closed")
	}
	if process.closing {
		process.mu.Unlock()
		return nil, authority.SupervisorHandoff{}, "", errors.New("suspended process is closing")
	}
	job := process.job
	jobID := process.jobID
	processHandle := process.process
	targetPID := int(process.pid)
	process.mu.Unlock()
	if job == 0 || processHandle == 0 || targetPID <= 0 || !validContainmentToken(jobID) {
		return nil, authority.SupervisorHandoff{}, "", errors.New("suspended process durable handles are incomplete")
	}

	targetIdentity, err := readProcessIdentityFromHandle(targetPID, syscall.Handle(processHandle))
	if err != nil {
		return nil, authority.SupervisorHandoff{}, "", fmt.Errorf("capture suspended process creation identity: %w", err)
	}
	handoff, err := NewSupervisorHandoff(targetPID, targetIdentity, jobID)
	if err != nil {
		return nil, authority.SupervisorHandoff{}, targetIdentity, err
	}
	// This is the durable fence. No helper process is created before this
	// callback returns successfully.
	if err := callbacks.Prepare(handoff.Clone()); err != nil {
		return nil, handoff, targetIdentity, fmt.Errorf("prepare supervisor handoff: %w", err)
	}

	supervisor, bound, helperIdentity, launchErr := launchContainmentSupervisorProcessWithHandoff(ctx, syscall.Handle(job), handoff, callbacks.Bind)
	if supervisor == nil && launchErr != nil {
		return nil, bound, targetIdentity, launchErr
	}
	contained := &jobContainment{handle: syscall.Handle(job), jobID: jobID, supervisor: supervisor}
	process.mu.Lock()
	if process.closed || process.closing || process.job != job || process.jobID != jobID {
		process.mu.Unlock()
		_ = contained.Close()
		return nil, bound, targetIdentity, errors.New("suspended process changed during durable containment attachment")
	}
	process.job = 0
	process.jobID = ""
	process.mu.Unlock()
	if launchErr != nil {
		return contained, bound, helperIdentity, fmt.Errorf("start durable containment supervisor: %w", launchErr)
	}
	return contained, bound, helperIdentity, nil
}

// AttachSuspendedProcessWithSupervisorHandoff is the descriptive alias kept
// for callers that use the full authority terminology.
func AttachSuspendedProcessWithSupervisorHandoff(ctx context.Context, process *SuspendedProcess, callbacks SupervisorHandoffCallbacks) (Containment, authority.SupervisorHandoff, string, error) {
	return AttachSuspendedProcessWithHandoff(ctx, process, callbacks)
}

func launchContainmentSupervisorProcessWithHandoff(ctx context.Context, job syscall.Handle, handoff authority.SupervisorHandoff, bind func(authority.SupervisorHandoff, int, string) error) (containmentSupervisorLease, authority.SupervisorHandoff, string, error) {
	if ctx == nil {
		return nil, handoff, "", errors.New("durable containment supervisor context is nil")
	}
	if job == 0 {
		return nil, handoff, "", errors.New("durable containment supervisor requires a Job handle")
	}
	if err := handoff.Validate(); err != nil {
		return nil, handoff, "", err
	}
	if handoff.SupervisorPID != 0 || handoff.SupervisorIdentity != "" {
		return nil, handoff, "", errors.New("durable containment supervisor handoff already has helper identity")
	}

	bootstrapRead, bootstrapWrite, err := os.Pipe()
	if err != nil {
		return nil, handoff, "", fmt.Errorf("create durable containment bootstrap channel: %w", err)
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
		return nil, handoff, "", fmt.Errorf("create durable containment owner channel: %w", err)
	}
	closeOwner := func() {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
		_ = ownerRead.Close()
		_ = ownerWrite.Close()
	}
	if err := setContainmentHandleInformation(syscall.Handle(bootstrapRead.Fd()), syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("make durable containment bootstrap handle inheritable: %w", err)
	}
	if err := setContainmentHandleInformation(syscall.Handle(ownerRead.Fd()), syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("make durable containment owner handle inheritable: %w", err)
	}
	if err := setContainmentHandleInformation(syscall.Handle(ownerWrite.Fd()), syscall.HANDLE_FLAG_INHERIT, 0); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("make durable containment owner writer non-inheritable: %w", err)
	}
	if err := setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("make durable containment Job handle inheritable: %w", err)
	}
	clearJobInheritance := true
	defer func() {
		if clearJobInheritance {
			_ = setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, 0)
		}
	}()

	command := exec.Command(os.Args[0],
		"-containment-supervisor",
		"-durable",
		"-bootstrap-handle", strconv.FormatUint(uint64(bootstrapRead.Fd()), 10),
		"-owner-handle", strconv.FormatUint(uint64(ownerRead.Fd()), 10),
		"-job-handle", strconv.FormatUint(uint64(job), 10),
		"-target-pid", strconv.Itoa(handoff.TargetPID),
		"-target-identity", handoff.TargetIdentity,
		"-pipe-token", handoff.PipeToken,
		"-job-id", handoff.JobID,
		"-job-name", containmentJobName(handoff.JobID),
		"-launch-token", handoff.LaunchToken,
	)
	if err := ConfigureHeadlessProcess(command); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("configure durable containment supervisor: %w", err)
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{job, syscall.Handle(bootstrapRead.Fd()), syscall.Handle(ownerRead.Fd())}
	if err := startContainmentSupervisor(command); err != nil {
		closeOwner()
		return nil, handoff, "", fmt.Errorf("start durable containment supervisor: %w", err)
	}
	_ = bootstrapRead.Close()
	_ = ownerRead.Close()
	waiter := newSupervisorProcessWait(command.Process)
	partial := &processContainmentSupervisorLease{
		endpoint: containmentSupervisorEndpoint{
			LaunchToken:    handoff.LaunchToken,
			TargetPID:      handoff.TargetPID,
			TargetIdentity: handoff.TargetIdentity,
			PipeName:       containmentSupervisorPipePrefix + handoff.PipeToken,
			Token:          handoff.PipeToken,
			JobID:          handoff.JobID,
			JobName:        containmentJobName(handoff.JobID),
			Secret:         handoff.Secret,
		},
		waiter:     waiter,
		ownerWrite: ownerWrite,
	}
	if err := setContainmentHandleInformation(job, syscall.HANDLE_FLAG_INHERIT, 0); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		partial.ownerWrite = nil
		return newPartialContainmentSupervisorLease(partial), handoff, "", fmt.Errorf("clear durable containment Job inheritance: %w", err)
	}
	clearJobInheritance = false

	helperIdentity, err := readSupervisorProcessIdentity(command.Process.Pid)
	if err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return newPartialContainmentSupervisorLease(partial), handoff, "", fmt.Errorf("capture durable containment supervisor identity: %w", err)
	}
	bound := handoff.Clone()
	bound.SupervisorPID = command.Process.Pid
	bound.SupervisorIdentity = helperIdentity
	endpoint, err := containmentEndpointForHandoff(bound)
	if err != nil {
		return newPartialContainmentSupervisorLease(partial), bound, helperIdentity, err
	}
	partial.endpoint = endpoint
	if err := ctx.Err(); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return newPartialContainmentSupervisorLease(partial), handoff, "", fmt.Errorf("durable containment bootstrap cancelled: %w", err)
	}
	if bind == nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return newPartialContainmentSupervisorLease(partial), handoff, "", errors.New("durable containment supervisor bind callback is required")
	}
	// The inherited anonymous bootstrap channel is the pre-authentication
	// boundary. Its peer is the exact helper child just started, so a same-user
	// process cannot race a public named pipe before Bind completes.
	if err := bind(handoff.Clone(), command.Process.Pid, helperIdentity); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		bound.SupervisorPID = command.Process.Pid
		bound.SupervisorIdentity = helperIdentity
		return partial, bound, helperIdentity, fmt.Errorf("bind durable containment supervisor identity: %w", err)
	}
	bound.SupervisorPID = command.Process.Pid
	bound.SupervisorIdentity = helperIdentity
	endpoint, err = containmentEndpointForHandoff(bound)
	if err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return partial, bound, helperIdentity, err
	}
	partial.endpoint = endpoint
	bootstrap := containmentSupervisorBootstrap{
		Version:            containmentSupervisorProtocol,
		JobName:            endpoint.JobName,
		LaunchToken:        endpoint.LaunchToken,
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		PipeToken:          endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             endpoint.Secret,
	}
	if err := writeContainmentSupervisorBootstrapContext(ctx, bootstrapWrite, bootstrap, time.Now().Add(containmentSupervisorConnect)); err != nil {
		_ = bootstrapWrite.Close()
		_ = ownerWrite.Close()
		return partial, bound, helperIdentity, fmt.Errorf("send durable containment supervisor bootstrap: %w", err)
	}
	_ = bootstrapWrite.Close()
	deadline := time.Now().Add(containmentSupervisorConnect)
	if markerPath := os.Getenv("SYMMETRY_PREAUTHORITY_WRONG_SECRET_MARKER"); markerPath != "" {
		if err := runContainmentSupervisorWrongSecretFirstClient(endpoint, syscall.Handle(job), markerPath, deadline); err != nil {
			_ = ownerWrite.Close()
			return partial, bound, helperIdentity, fmt.Errorf("run wrong-secret first-client witness: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		_ = ownerWrite.Close()
		return partial, bound, helperIdentity, fmt.Errorf("durable containment bootstrap cancelled: %w", err)
	}
	response, err := requestContainmentSupervisor(endpoint, "hello", deadline)
	if err != nil {
		partial.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return partial, bound, helperIdentity, fmt.Errorf("connect durable containment supervisor: %w", err)
	}
	if err := validateSupervisorResponse(endpoint, "hello", response); err != nil {
		partial.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return partial, bound, helperIdentity, fmt.Errorf("validate durable containment supervisor handshake: %w", err)
	}
	if response.Status != "ready" || response.ActiveProcesses == 0 {
		partial.closeOwnerWriterLocked()
		_ = waiter.killAndWait(time.Now().Add(containmentSupervisorConnect))
		return partial, bound, helperIdentity, errors.New("durable containment supervisor returned an incomplete handshake")
	}
	lease := &processContainmentSupervisorLease{endpoint: endpoint, waiter: waiter, ownerWrite: ownerWrite}
	return lease, bound, helperIdentity, nil
}

type containmentSupervisorWrongSecretWitness struct {
	Status             string `json:"status"`
	ActiveProcesses    uint32 `json:"active_processes"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	JobID              string `json:"job_id"`
}

type containmentSupervisorHelloRetryWitness struct {
	Status string `json:"status"`
}

// runContainmentSupervisorWrongSecretFirstClient is an opt-in production
// witness hook. It runs after the inherited bootstrap has been accepted and
// before the legitimate hello, so the first named-pipe request is a complete
// endpoint request with only the authority secret changed. The marker carries
// no secret-bearing field and is written only after the helper rejected that
// request while the exact Job and target were still live.
func runContainmentSupervisorWrongSecretFirstClient(endpoint containmentSupervisorEndpoint, job syscall.Handle, markerPath string, deadline time.Time) error {
	wrongSecret := strings.Repeat("0", len(endpoint.Secret))
	if wrongSecret == endpoint.Secret {
		wrongSecret = strings.Repeat("1", len(endpoint.Secret))
	}
	request := containmentSupervisorRequest{
		Version:            containmentSupervisorProtocol,
		Operation:          "hello",
		LaunchToken:        endpoint.LaunchToken,
		TargetPID:          endpoint.TargetPID,
		TargetIdentity:     endpoint.TargetIdentity,
		SupervisorPID:      endpoint.SupervisorPID,
		SupervisorIdentity: endpoint.SupervisorIdentity,
		Token:              endpoint.Token,
		JobID:              endpoint.JobID,
		Secret:             wrongSecret,
	}
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("wrong-secret first-client witness timed out")
		}
		pipe, err := openContainmentSupervisorPipe(endpoint.PipeName)
		if err != nil {
			if !retryableSupervisorPipeError(err) {
				return fmt.Errorf("open supervisor pipe for wrong-secret witness: %w", err)
			}
			time.Sleep(containmentSupervisorProbeInterval)
			continue
		}
		serverPID := uint32(0)
		if err := withContainmentSupervisorPipeHandle(pipe, func(handle windows.Handle) error {
			return getContainmentSupervisorServerPID(handle, &serverPID)
		}); err != nil {
			_ = pipe.Close()
			if retryableSupervisorHelloTransportError(err) {
				continue
			}
			return fmt.Errorf("read wrong-secret witness pipe owner: %w", err)
		}
		if int(serverPID) != endpoint.SupervisorPID {
			_ = pipe.Close()
			return fmt.Errorf("wrong-secret witness pipe owner PID %d does not match %d", serverPID, endpoint.SupervisorPID)
		}
		serverIdentity, err := readSupervisorProcessIdentity(int(serverPID))
		if err != nil {
			_ = pipe.Close()
			return fmt.Errorf("read wrong-secret witness pipe owner identity: %w", err)
		}
		if serverIdentity != endpoint.SupervisorIdentity {
			_ = pipe.Close()
			return errors.New("wrong-secret witness pipe owner identity mismatch")
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			_ = pipe.Close()
			return err
		}
		encoded = append(encoded, '\n')
		if err := writeSupervisorLine(pipe, encoded, deadline); err != nil {
			_ = pipe.Close()
			return fmt.Errorf("write wrong-secret witness request: %w", err)
		}
		_, readErr := readSupervisorLine(pipe, deadline)
		_ = pipe.Close()
		if readErr == nil {
			return errors.New("wrong-secret witness request received a response")
		}
		if !retryableSupervisorHelloTransportError(readErr) {
			return fmt.Errorf("wrong-secret witness rejection was not a peer disconnect: %w", readErr)
		}
		activeProcesses, err := queryJobActiveProcesses(job)
		if err != nil {
			return fmt.Errorf("query Job after wrong-secret witness request: %w", err)
		}
		actualTargetIdentity, err := readSupervisorProcessIdentity(endpoint.TargetPID)
		if err != nil {
			return fmt.Errorf("read target identity after wrong-secret witness request: %w", err)
		}
		if actualTargetIdentity != endpoint.TargetIdentity {
			return fmt.Errorf("wrong-secret witness target identity changed from %q to %q", endpoint.TargetIdentity, actualTargetIdentity)
		}
		running, err := processIdentityIsRunning(endpoint.TargetPID)
		if err != nil {
			return fmt.Errorf("inspect target after wrong-secret witness request: %w", err)
		}
		if !running || activeProcesses == 0 {
			return fmt.Errorf("wrong-secret witness did not observe a live target and Job: running=%t active=%d", running, activeProcesses)
		}
		evidence := containmentSupervisorWrongSecretWitness{
			Status:             "rejected",
			ActiveProcesses:    activeProcesses,
			TargetPID:          endpoint.TargetPID,
			TargetIdentity:     endpoint.TargetIdentity,
			SupervisorPID:      endpoint.SupervisorPID,
			SupervisorIdentity: endpoint.SupervisorIdentity,
			JobID:              endpoint.JobID,
		}
		if err := writeContainmentSupervisorWitnessMarker(markerPath, evidence); err != nil {
			return fmt.Errorf("write wrong-secret witness marker: %w", err)
		}
		return nil
	}
}

func writeContainmentSupervisorWitnessMarker(path string, value any) error {
	contents, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o600)
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

// DurableSupervisorHandoffAvailable reports whether the current executable can
// dispatch the daemon's hidden supervisor mode. Test binaries use a generated
// test main and must retain the legacy local containment path instead.
func DurableSupervisorHandoffAvailable() bool {
	return !isGoTestBinary(os.Args[0])
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
		LaunchToken:        lease.endpoint.LaunchToken,
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
		LaunchToken:        lease.endpoint.LaunchToken,
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
		LaunchToken:        endpoint.LaunchToken,
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
		LaunchToken:        endpoint.LaunchToken,
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
	if request.Operation != "hello" && request.Operation != "close" && request.Operation != "recover" && request.Operation != "abort" && request.Operation != "renew" && request.Operation != "release" {
		return containmentSupervisorResponse{}, fmt.Errorf("unknown containment supervisor operation %q", request.Operation)
	}
	retryHelloTransport := request.Operation == "hello"
	var lastErr error
	retryAfterHelloTransportError := func(err error) bool {
		if !retryHelloTransport || !retryableSupervisorHelloTransportError(err) {
			return false
		}
		lastErr = err
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		wait := containmentSupervisorProbeInterval
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
		return true
	}
	for {
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = context.DeadlineExceeded
			}
			err := fmt.Errorf("containment supervisor connection timeout: %w", lastErr)
			return containmentSupervisorResponse{}, err
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
		if err := withContainmentSupervisorPipeHandle(pipe, func(handle windows.Handle) error {
			return getContainmentSupervisorServerPID(handle, &serverPID)
		}); err != nil {
			_ = pipe.Close()
			if retryAfterHelloTransportError(err) {
				continue
			}
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
			if retryAfterHelloTransportError(err) {
				continue
			}
			return containmentSupervisorResponse{}, err
		}
		line, err := readSupervisorLine(pipe, deadline)
		_ = pipe.Close()
		if err != nil {
			if retryAfterHelloTransportError(err) {
				continue
			}
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

func openContainmentSupervisorPipeFile(name string) (*os.File, error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(name),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func withContainmentSupervisorPipeHandle(file *os.File, action func(windows.Handle) error) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var actionErr error
	if err := raw.Control(func(fd uintptr) {
		actionErr = action(windows.Handle(fd))
	}); err != nil {
		return err
	}
	return actionErr
}

func retryableSupervisorPipeError(err error) bool {
	return errors.Is(err, windows.ERROR_PIPE_BUSY) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_SEM_TIMEOUT)
}

// retryableSupervisorHelloTransportError covers only a disconnected initial
// hello exchange. An unauthenticated client can consume one named-pipe
// instance and close it before the legitimate daemon hello is read; the
// caller may retry that transport boundary, but must not retry semantic or
// authenticated protocol failures.
func retryableSupervisorHelloTransportError(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED)
}

func waitForContainmentSupervisorRetry(ctx context.Context, deadline time.Time) error {
	if ctx == nil {
		return errors.New("containment supervisor retry context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	wait := containmentSupervisorProbeInterval
	if remaining < wait {
		wait = remaining
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func writeContainmentSupervisorBootstrapContext(ctx context.Context, file *os.File, value containmentSupervisorBootstrap, deadline time.Time) error {
	return runContainmentSupervisorIOContext(ctx, file, deadline, "write containment supervisor bootstrap", func() error {
		return writeContainmentSupervisorBootstrap(file, value, deadline)
	})
}

func readContainmentSupervisorLineContext(ctx context.Context, file *os.File, deadline time.Time) ([]byte, error) {
	var line []byte
	err := runContainmentSupervisorIOContext(ctx, file, deadline, "read containment supervisor response", func() error {
		var err error
		line, err = readContainmentSupervisorLine(file, deadline)
		return err
	})
	if err != nil {
		return nil, err
	}
	return line, nil
}

// runContainmentSupervisorIOContext owns the goroutine used by the existing
// file-based I/O hooks. Cancellation must cancel the Windows handle before the
// file is closed, then wait for the operation to return so no I/O goroutine
// survives the bootstrap failure boundary.
func runContainmentSupervisorIOContext(ctx context.Context, file *os.File, deadline time.Time, operation string, action func() error) error {
	if ctx == nil {
		return errors.New("containment supervisor I/O context is nil")
	}
	if file == nil {
		return errors.New("containment supervisor I/O file is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s cancelled: %w", operation, err)
	}

	done := make(chan error, 1)
	go func() { done <- action() }()

	var timer *time.Timer
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return cancelAndDrainContainmentSupervisorIO(file, done, fmt.Errorf("%s deadline expired: %w", operation, os.ErrDeadlineExceeded))
		}
		timer = time.NewTimer(remaining)
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
		return nil
	case <-ctx.Done():
		return cancelAndDrainContainmentSupervisorIO(file, done, fmt.Errorf("%s cancelled: %w", operation, ctx.Err()))
	case <-timeout:
		if err := ctx.Err(); err != nil {
			return cancelAndDrainContainmentSupervisorIO(file, done, fmt.Errorf("%s cancelled: %w", operation, err))
		}
		return cancelAndDrainContainmentSupervisorIO(file, done, fmt.Errorf("%s deadline expired: %w", operation, os.ErrDeadlineExceeded))
	}
}

func cancelAndDrainContainmentSupervisorIO(file *os.File, done <-chan error, terminationErr error) error {
	var cancelErr error
	if err := withContainmentSupervisorPipeHandle(file, func(handle windows.Handle) error {
		return windows.CancelIoEx(handle, nil)
	}); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		cancelErr = err
	}
	closeErr := file.Close()
	ioErr := <-done
	if cancelErr == nil && closeErr == nil && ioErr == nil {
		return terminationErr
	}
	joined := []error{terminationErr}
	if cancelErr != nil {
		joined = append(joined, fmt.Errorf("cancel containment supervisor I/O: %w", cancelErr))
	}
	if closeErr != nil {
		joined = append(joined, fmt.Errorf("close containment supervisor I/O: %w", closeErr))
	}
	if ioErr != nil {
		joined = append(joined, fmt.Errorf("containment supervisor I/O after cancellation: %w", ioErr))
	}
	return errors.Join(joined...)
}

func writeSupervisorLine(file *os.File, line []byte, deadline time.Time) error {
	if len(line) > containmentSupervisorMaxLine {
		return errors.New("containment supervisor request is too large")
	}
	if err := file.SetWriteDeadline(deadline); err != nil {
		if errors.Is(err, os.ErrNoDeadline) {
			return writeSupervisorLineWithoutDeadline(file, line, deadline)
		}
		return fmt.Errorf("write containment supervisor request: %w", err)
	}
	written, err := file.Write(line)
	if err != nil {
		return fmt.Errorf("write containment supervisor request: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf("write containment supervisor request: %w", io.ErrShortWrite)
	}
	return nil
}

func readSupervisorLine(file *os.File, deadline time.Time) ([]byte, error) {
	if err := file.SetReadDeadline(deadline); err != nil {
		if errors.Is(err, os.ErrNoDeadline) {
			return readSupervisorLineWithoutDeadline(file, deadline)
		}
		return nil, fmt.Errorf("read containment supervisor response: %w", err)
	}
	line, err := readBoundedLine(file)
	if err != nil {
		return nil, fmt.Errorf("read containment supervisor response: %w", err)
	}
	return line, nil
}

func readSupervisorLineWithoutDeadline(file *os.File, deadline time.Time) ([]byte, error) {
	if supervisorDeadlineExpired(deadline) {
		return nil, fmt.Errorf("read containment supervisor response: %w", os.ErrDeadlineExceeded)
	}
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
	if deadline.IsZero() {
		output := <-result
		return output.line, output.err
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		closeErr := file.Close()
		output := <-result
		return nil, joinSupervisorDeadlineFailure("read containment supervisor response", output.err, closeErr)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case output := <-result:
		return output.line, output.err
	case <-timer.C:
		closeErr := file.Close()
		output := <-result
		return nil, joinSupervisorDeadlineFailure("read containment supervisor response", output.err, closeErr)
	}
}

func supervisorDeadlineExpired(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
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
	if endpoint.LaunchToken != "" && response.LaunchToken != endpoint.LaunchToken {
		return errors.New("containment supervisor launch identifier mismatch")
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
		if response.Error == containmentSupervisorOwnerLostMessage {
			return fmt.Errorf("%w: %s", errContainmentSupervisorOwnerLost, response.Error)
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
	case "abort":
		if response.Status != "aborted" && response.Status != "released" {
			return fmt.Errorf("unexpected containment supervisor abort status %q", response.Status)
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
	if !validContainmentToken(endpoint.Token) || !validContainmentToken(endpoint.JobID) {
		return errors.New("containment supervisor token is invalid")
	}
	if endpoint.Secret == "" {
		// The durable helper validates this endpoint before authentication. It
		// may carry only the non-secret launch binding until the daemon has
		// verified the pipe server and completed the durable helper CAS bind.
		if requireSupervisor || endpoint.LaunchToken == "" || !validContainmentToken(endpoint.LaunchToken) {
			return errors.New("containment supervisor secret or pre-auth launch identifier is invalid")
		}
	} else if !validContainmentSecret(endpoint.Secret) {
		return errors.New("containment supervisor secret is invalid")
	}
	if endpoint.PipeName != containmentSupervisorPipePrefix+endpoint.Token {
		return errors.New("containment supervisor pipe name does not match token")
	}
	if endpoint.LaunchToken != "" && !validContainmentToken(endpoint.LaunchToken) {
		return errors.New("containment supervisor launch identifier is invalid")
	}
	if endpoint.JobName != "" && endpoint.JobName != containmentJobName(endpoint.JobID) {
		return errors.New("containment supervisor Job name does not match identifier")
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
		LaunchToken:        value.LaunchToken,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + value.PipeToken,
		Token:              value.PipeToken,
		JobID:              value.JobID,
		JobName:            containmentJobName(value.JobID),
		Secret:             value.Secret,
	}
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	return endpoint, nil
}

// NewSupervisorHandoff creates the immutable authority material for a
// suspended launch. The caller must durably prepare the returned value before
// starting an independent supervisor. The named Job identifier is supplied by
// SuspendedProcess.JobID and is never a credential. CreatorSessionID records
// the daemon's Windows session so pre-authority abort recovery cannot mistake
// a cross-session absence for proof.
func NewSupervisorHandoff(targetPID int, targetIdentity, jobID string) (authority.SupervisorHandoff, error) {
	if targetPID <= 0 || strings.TrimSpace(targetIdentity) == "" {
		return authority.SupervisorHandoff{}, errors.New("supervisor handoff target identity is required")
	}
	identityPID, _, err := parseWindowsProcessIdentity(targetIdentity)
	if err != nil || identityPID != targetPID {
		return authority.SupervisorHandoff{}, errors.New("supervisor handoff target identity does not match PID")
	}
	if !validContainmentToken(jobID) {
		return authority.SupervisorHandoff{}, errors.New("supervisor handoff Job identifier is invalid")
	}
	creatorSessionID, err := getCurrentContainmentSessionID()
	if err != nil {
		return authority.SupervisorHandoff{}, fmt.Errorf("capture supervisor handoff creator session: %w", err)
	}
	launchToken, err := randomContainmentToken()
	if err != nil {
		return authority.SupervisorHandoff{}, fmt.Errorf("generate supervisor handoff launch identifier: %w", err)
	}
	pipeToken, err := randomContainmentToken()
	if err != nil {
		return authority.SupervisorHandoff{}, fmt.Errorf("generate supervisor handoff pipe identifier: %w", err)
	}
	secret, err := randomContainmentSecret()
	if err != nil {
		return authority.SupervisorHandoff{}, fmt.Errorf("generate supervisor handoff secret: %w", err)
	}
	value := authority.SupervisorHandoff{
		Version:        authority.SupervisorHandoffVersion,
		LaunchToken:    launchToken,
		Secret:         secret,
		TargetPID:      targetPID,
		TargetIdentity: targetIdentity,
		PipeToken:      pipeToken,
		JobID:          jobID,
		CreatorSessionID: func() *uint32 {
			value := creatorSessionID
			return &value
		}(),
	}
	if err := value.Validate(); err != nil {
		return authority.SupervisorHandoff{}, err
	}
	return value, nil
}

func cloneContainmentSessionID(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func currentContainmentSessionID() (uint32, error) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &sessionID); err != nil {
		return 0, err
	}
	return sessionID, nil
}

func containmentEndpointForHandoff(value authority.SupervisorHandoff) (containmentSupervisorEndpoint, error) {
	if err := value.Validate(); err != nil {
		return containmentSupervisorEndpoint{}, err
	}
	if value.SupervisorPID <= 0 || strings.TrimSpace(value.SupervisorIdentity) == "" {
		return containmentSupervisorEndpoint{}, errors.New("supervisor handoff helper identity is not bound")
	}
	endpoint := containmentSupervisorEndpoint{
		LaunchToken:        value.LaunchToken,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + value.PipeToken,
		Token:              value.PipeToken,
		JobID:              value.JobID,
		JobName:            containmentJobName(value.JobID),
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

// createNamedContainmentJob creates the single named Job Object used by the
// durable suspended-launch path. The name is intentionally derived only from
// the public random Job identifier; the authority secret never enters a
// kernel object name, command line, environment, or log.
func createNamedContainmentJob() (syscall.Handle, string, error) {
	jobID, err := randomContainmentToken()
	if err != nil {
		return 0, "", fmt.Errorf("generate containment Job identifier: %w", err)
	}
	name := containmentSupervisorJobPrefix + jobID
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, "", fmt.Errorf("encode containment Job name: %w", err)
	}
	result, _, callError := createJobObject.Call(0, uintptr(unsafe.Pointer(namePointer)))
	if result == 0 {
		return 0, "", fmt.Errorf("create named containment Job: %w", callError)
	}
	if errors.Is(callError, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(windows.Handle(result))
		return 0, "", errors.New("named containment Job identifier already exists")
	}
	return syscall.Handle(result), jobID, nil
}

func containmentJobName(jobID string) string {
	return containmentSupervisorJobPrefix + jobID
}

func openNamedContainmentJob(name string) (syscall.Handle, error) {
	if !strings.HasPrefix(name, containmentSupervisorJobPrefix) {
		return 0, errors.New("containment Job name has an invalid namespace")
	}
	jobID := strings.TrimPrefix(name, containmentSupervisorJobPrefix)
	if !validContainmentToken(jobID) || name != containmentJobName(jobID) {
		return 0, errors.New("containment Job name does not match its identifier")
	}
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("encode containment Job name: %w", err)
	}
	const desiredAccess = uint32(0x0004 | 0x0008 | windows.SYNCHRONIZE) // QUERY | TERMINATE | SYNCHRONIZE
	result, _, callError := openJobObject.Call(uintptr(desiredAccess), 0, uintptr(unsafe.Pointer(namePointer)))
	if result == 0 {
		return 0, callError
	}
	return syscall.Handle(result), nil
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

func writeSupervisorLineWithoutDeadline(file *os.File, line []byte, deadline time.Time) error {
	if len(line) > containmentSupervisorMaxLine {
		return errors.New("containment supervisor request is too large")
	}
	if supervisorDeadlineExpired(deadline) {
		return fmt.Errorf("write containment supervisor request: %w", os.ErrDeadlineExceeded)
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

func waitSupervisorIO(file *os.File, done <-chan error, deadline time.Time, operation string) error {
	if deadline.IsZero() {
		if err := <-done; err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		closeErr := file.Close()
		ioErr := <-done
		return joinSupervisorDeadlineFailure(operation, ioErr, closeErr)
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
		closeErr := file.Close()
		ioErr := <-done
		return joinSupervisorDeadlineFailure(operation, ioErr, closeErr)
	}
}

func joinSupervisorDeadlineFailure(operation string, ioErr, closeErr error) error {
	deadlineErr := fmt.Errorf("%s deadline expired: %w", operation, os.ErrDeadlineExceeded)
	if ioErr == nil && closeErr == nil {
		return deadlineErr
	}
	joined := []error{deadlineErr}
	if ioErr != nil {
		joined = append(joined, fmt.Errorf("%s: %w", operation, ioErr))
	}
	if closeErr != nil {
		joined = append(joined, closeErr)
	}
	return errors.Join(joined...)
}

type containmentSupervisorArgs struct {
	bootstrapHandle syscall.Handle
	ownerHandle     syscall.Handle
	jobHandle       syscall.Handle
	durable         bool
	targetPID       int
	targetIdentity  string
	pipeToken       string
	jobID           string
	jobName         string
	launchToken     string
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
	if parsed.durable {
		return runDurableContainmentSupervisor(parsed, ownerWatchdog)
	}
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
	jobHandleText := set.String("job-handle", "", "")
	durable := set.Bool("durable", false, "")
	targetPIDText := set.String("target-pid", "", "")
	targetIdentity := set.String("target-identity", "", "")
	pipeToken := set.String("pipe-token", "", "")
	jobID := set.String("job-id", "", "")
	jobName := set.String("job-name", "", "")
	launchToken := set.String("launch-token", "", "")
	if err := set.Parse(args); err != nil {
		return containmentSupervisorArgs{}, fmt.Errorf("parse containment supervisor arguments: %w", err)
	}
	if set.NArg() != 0 {
		return containmentSupervisorArgs{}, errors.New("containment supervisor does not accept positional arguments")
	}
	ownerValue, err := strconv.ParseUint(*ownerHandleText, 10, 64)
	if err != nil || ownerValue == 0 || uint64(syscall.Handle(ownerValue)) != ownerValue {
		return containmentSupervisorArgs{}, errors.New("containment supervisor owner handle is invalid")
	}
	if *durable {
		if *bootstrapHandleText == "" {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor bootstrap handle is required")
		}
		bootstrapValue, parseErr := strconv.ParseUint(*bootstrapHandleText, 10, 64)
		if parseErr != nil || bootstrapValue == 0 || uint64(syscall.Handle(bootstrapValue)) != bootstrapValue {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor bootstrap handle is invalid")
		}
		jobValue, parseErr := strconv.ParseUint(*jobHandleText, 10, 64)
		if parseErr != nil || jobValue == 0 || uint64(syscall.Handle(jobValue)) != jobValue {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor Job handle is invalid")
		}
		parsedPID, parseErr := strconv.ParseUint(*targetPIDText, 10, 32)
		if parseErr != nil || parsedPID == 0 {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor target PID is invalid")
		}
		if _, _, parseErr := parseWindowsProcessIdentity(*targetIdentity); parseErr != nil {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor target identity is invalid")
		}
		if !validContainmentToken(*pipeToken) || !validContainmentToken(*jobID) || *jobName != containmentJobName(*jobID) || !validContainmentToken(*launchToken) {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor bootstrap identity is invalid")
		}
		identityPID, _, parseErr := parseWindowsProcessIdentity(*targetIdentity)
		if parseErr != nil || identityPID != int(parsedPID) {
			return containmentSupervisorArgs{}, errors.New("durable containment supervisor target identity PID mismatch")
		}
		return containmentSupervisorArgs{
			bootstrapHandle: syscall.Handle(bootstrapValue),
			ownerHandle:     syscall.Handle(ownerValue),
			jobHandle:       syscall.Handle(jobValue),
			durable:         true,
			targetPID:       int(parsedPID),
			targetIdentity:  *targetIdentity,
			pipeToken:       *pipeToken,
			jobID:           *jobID,
			jobName:         *jobName,
			launchToken:     *launchToken,
		}, nil
	}
	bootstrapValue, err := strconv.ParseUint(*bootstrapHandleText, 10, 64)
	if err != nil || bootstrapValue == 0 || uint64(syscall.Handle(bootstrapValue)) != bootstrapValue {
		return containmentSupervisorArgs{}, errors.New("containment supervisor bootstrap handle is invalid")
	}
	return containmentSupervisorArgs{bootstrapHandle: syscall.Handle(bootstrapValue), ownerHandle: syscall.Handle(ownerValue)}, nil
}

func validateSupervisorBootstrap(value containmentSupervisorBootstrap) error {
	if value.Version != containmentSupervisorProtocol {
		return errors.New("containment supervisor bootstrap is invalid")
	}
	if value.JobHandle == 0 {
		if value.JobName == "" || value.JobName != containmentJobName(value.JobID) || !validContainmentToken(value.LaunchToken) {
			return errors.New("durable containment supervisor Job bootstrap is invalid")
		}
	} else if uint64(syscall.Handle(value.JobHandle)) != value.JobHandle || value.JobName != "" || value.LaunchToken != "" {
		return errors.New("containment supervisor inherited Job bootstrap is invalid")
	}
	if value.TargetPID <= 0 || strings.TrimSpace(value.TargetIdentity) == "" || value.SupervisorPID <= 0 || strings.TrimSpace(value.SupervisorIdentity) == "" {
		return errors.New("containment supervisor bootstrap process identity is invalid")
	}
	targetIdentityPID, _, err := parseWindowsProcessIdentity(value.TargetIdentity)
	if err != nil || targetIdentityPID != value.TargetPID {
		return errors.New("containment supervisor bootstrap target identity mismatch")
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

func runDurableContainmentSupervisor(args containmentSupervisorArgs, ownerWatchdog *containmentSupervisorOwnerWatchdog) error {
	if args.bootstrapHandle == 0 {
		return errors.New("durable containment supervisor bootstrap channel is required")
	}
	bootstrapFile := os.NewFile(uintptr(args.bootstrapHandle), "symmetry-containment-bootstrap")
	if bootstrapFile == nil {
		return errors.New("durable containment supervisor bootstrap handle is invalid")
	}
	defer bootstrapFile.Close()
	line, err := readBoundedLine(bootstrapFile)
	if err != nil {
		// Before the secret-bearing bootstrap arrives, the inherited Job is the
		// only cleanup authority. An owner crash or cancellation must stop and
		// release it rather than leave an unbound child alive.
		_, _ = terminateAndObserveSupervisorJob(args.jobHandle)
		_ = closeJob(args.jobHandle)
		return fmt.Errorf("read durable containment supervisor bootstrap: %w", err)
	}
	var bootstrap containmentSupervisorBootstrap
	if err := decodeSupervisorJSON(line, &bootstrap); err != nil {
		_, _ = terminateAndObserveSupervisorJob(args.jobHandle)
		_ = closeJob(args.jobHandle)
		return fmt.Errorf("decode durable containment supervisor bootstrap: %w", err)
	}
	if err := validateSupervisorBootstrap(bootstrap); err != nil {
		_, _ = terminateAndObserveSupervisorJob(args.jobHandle)
		_ = closeJob(args.jobHandle)
		return err
	}
	if bootstrap.JobName != args.jobName || bootstrap.LaunchToken != args.launchToken || bootstrap.TargetPID != args.targetPID ||
		bootstrap.TargetIdentity != args.targetIdentity || bootstrap.PipeToken != args.pipeToken || bootstrap.JobID != args.jobID ||
		bootstrap.SupervisorPID != os.Getpid() {
		_, _ = terminateAndObserveSupervisorJob(args.jobHandle)
		_ = closeJob(args.jobHandle)
		return errors.New("durable containment supervisor bootstrap does not match launch identity")
	}
	actualIdentity, identityErr := ProcessIdentity(os.Getpid())
	if identityErr != nil || actualIdentity != bootstrap.SupervisorIdentity {
		_, _ = terminateAndObserveSupervisorJob(args.jobHandle)
		_ = closeJob(args.jobHandle)
		if identityErr != nil {
			return fmt.Errorf("capture durable containment supervisor identity: %w", identityErr)
		}
		return errors.New("durable containment supervisor identity mismatch")
	}
	bootstrap.JobHandle = uint64(args.jobHandle)
	return runContainmentSupervisor(args, bootstrap, ownerWatchdog)

}

type durableContainmentOwnerState struct {
	ownerLost     bool
	stopAttempted bool
}

func refreshDurableContainmentOwnerLoss(state *durableContainmentOwnerState, ownerWatchdog *containmentSupervisorOwnerWatchdog, job syscall.Handle) {
	if state == nil || state.ownerLost || ownerWatchdog == nil {
		return
	}
	select {
	case <-ownerWatchdog.ownerLost():
		state.ownerLost = true
		if !state.stopAttempted {
			state.stopAttempted = true
			_, _ = terminateAndObserveSupervisorJob(job)
		}
	default:
	}
}

func finishDurableContainmentSupervisorHello(args containmentSupervisorArgs, bootstrap containmentSupervisorBootstrap, job syscall.Handle, ownerWatchdog *containmentSupervisorOwnerWatchdog, pipe *os.File, response containmentSupervisorResponse) error {
	_ = writeContainmentSupervisorHello(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
	_ = pipe.Close()
	// The helper has already authenticated and holds the inherited Job handle. A
	// lost hello write is therefore an uncertain response, not a reason to close
	// the recovery owner. Re-enter the post-auth loop so exact bootstrap or
	// recover replay can obtain the stop receipt and release proof.
	return retainAuthenticatedContainmentSupervisor(args, bootstrap, job, ownerWatchdog)
}

func retainAuthenticatedContainmentSupervisor(args containmentSupervisorArgs, bootstrap containmentSupervisorBootstrap, job syscall.Handle, ownerWatchdog *containmentSupervisorOwnerWatchdog) error {
	bootstrap.JobHandle = uint64(job)
	return runContainmentSupervisorAfterAuth(args, bootstrap, job, ownerWatchdog)
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

func runContainmentSupervisor(args containmentSupervisorArgs, bootstrap containmentSupervisorBootstrap, watchdogs ...*containmentSupervisorOwnerWatchdog) error {
	job := syscall.Handle(bootstrap.JobHandle)
	if job == 0 {
		var err error
		job, err = openNamedContainmentJob(bootstrap.JobName)
		if err != nil {
			return err
		}
	}
	return runContainmentSupervisorWithJob(args, bootstrap, job, watchdogs...)
}

func runContainmentSupervisorWithJob(_ containmentSupervisorArgs, bootstrap containmentSupervisorBootstrap, job syscall.Handle, watchdogs ...*containmentSupervisorOwnerWatchdog) error {
	var ownerWatchdog *containmentSupervisorOwnerWatchdog
	if len(watchdogs) > 0 {
		ownerWatchdog = watchdogs[0]
	}
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
		LaunchToken:        bootstrap.LaunchToken,
		TargetPID:          bootstrap.TargetPID,
		TargetIdentity:     bootstrap.TargetIdentity,
		SupervisorPID:      supervisorPID,
		SupervisorIdentity: supervisorIdentity,
		PipeName:           containmentSupervisorPipePrefix + bootstrap.PipeToken,
		Token:              bootstrap.PipeToken,
		JobID:              bootstrap.JobID,
		JobName:            bootstrap.JobName,
		Secret:             bootstrap.Secret,
	}
	if err := validateSupervisorEndpoint(endpoint, true); err != nil {
		stopErr := stopAndReleaseSupervisorJob(job, release)
		return errors.Join(err, stopErr)
	}
	dropFirstHelloResponse := os.Getenv("SYMMETRY_PREAUTHORITY_DROP_FIRST_HELLO_RESPONSE") == "1"
	helloRetryMarkerPath := os.Getenv("SYMMETRY_PREAUTHORITY_HELLO_RETRY_MARKER")

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
			time.Sleep(containmentSupervisorProbeInterval)
			continue
		}
		deadline := time.Now().Add(containmentSupervisorConnectionPoll)
		_, err = connectContainmentSupervisorPipe(pipe, deadline)
		if err != nil {
			_ = pipe.Close()
			refreshOwnerLoss()
			continue
		}
		requestLine, readErr := readSupervisorLine(pipe, time.Now().Add(containmentSupervisorConnectionPoll))
		if readErr != nil {
			_ = pipe.Close()
			refreshOwnerLoss()
			// A client has not authenticated until the request secret and the
			// complete endpoint fence validate. An unauthenticated disconnect is
			// not owner loss and must never gain Job-stop authority.
			continue
		}
		refreshOwnerLoss()
		// A lost hello response can leave the daemon with an uncertain bootstrap
		// result. Accept the exact secret-bearing bootstrap replay on a fresh
		// connection before decoding ordinary post-auth requests.
		if endpoint.JobName != "" {
			var replayBootstrap containmentSupervisorBootstrap
			if decodeErr := decodeSupervisorJSON(requestLine, &replayBootstrap); decodeErr == nil && replayBootstrap.JobName != "" {
				if validateErr := validateSupervisorBootstrap(replayBootstrap); validateErr != nil ||
					replayBootstrap.JobName != endpoint.JobName || replayBootstrap.LaunchToken != endpoint.LaunchToken ||
					replayBootstrap.TargetPID != endpoint.TargetPID || replayBootstrap.TargetIdentity != endpoint.TargetIdentity ||
					replayBootstrap.SupervisorPID != endpoint.SupervisorPID || replayBootstrap.SupervisorIdentity != endpoint.SupervisorIdentity ||
					replayBootstrap.PipeToken != endpoint.Token || replayBootstrap.JobID != endpoint.JobID || replayBootstrap.Secret != endpoint.Secret {
					_ = pipe.Close()
					continue
				}
				response := containmentSupervisorResponse{
					Version: containmentSupervisorProtocol, Operation: "hello", Status: "ready",
					LaunchToken: endpoint.LaunchToken, TargetPID: endpoint.TargetPID,
					TargetIdentity: endpoint.TargetIdentity, SupervisorPID: endpoint.SupervisorPID,
					SupervisorIdentity: endpoint.SupervisorIdentity, Token: endpoint.Token, JobID: endpoint.JobID,
				}
				if ownerLost {
					response.Status = "error"
					response.Error = containmentSupervisorOwnerLostMessage
				} else {
					response.ActiveProcesses, err = queryJobActiveProcesses(job)
				}
				if err != nil {
					response.Status = "error"
					response.Error = fmt.Sprintf("query Job during bootstrap replay: %v", err)
				}
				writeErr := writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
				_ = pipe.Close()
				if writeErr != nil {
					continue
				}
				continue
			}
		}
		var request containmentSupervisorRequest
		decodeErr := decodeSupervisorJSON(requestLine, &request)
		if decodeErr != nil {
			_ = pipe.Close()
			continue
		}
		if err := validateSupervisorRequest(endpoint, request); err != nil {
			_ = pipe.Close()
			continue
		}
		if request.Operation == "hello" && dropFirstHelloResponse {
			dropFirstHelloResponse = false
			if helloRetryMarkerPath != "" {
				if markerErr := writeContainmentSupervisorWitnessMarker(helloRetryMarkerPath, containmentSupervisorHelloRetryWitness{Status: "dropped"}); markerErr != nil {
					_ = pipe.Close()
					return fmt.Errorf("write hello retry witness marker: %w", markerErr)
				}
			}
			// The authenticated request is intentionally left without a response.
			// The client must classify the resulting peer disconnect as a transient
			// initial-hello failure and retry on a fresh pipe instance.
			_ = pipe.Close()
			continue
		}
		response := containmentSupervisorResponse{
			Version:            containmentSupervisorProtocol,
			Operation:          request.Operation,
			LaunchToken:        endpoint.LaunchToken,
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
				response.Error = containmentSupervisorOwnerLostMessage
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
				continue
			}
			if response.Status == "error" {
				continue
			}
			continue
		}
		if request.Operation == "renew" {
			if ownerLost {
				response.Status = "stopped"
				response.Error = containmentSupervisorOwnerLostMessage
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
		if request.Operation == "abort" && ownerLost {
			response.Status = "error"
			response.Error = containmentSupervisorOwnerLostMessage
			_ = writeSupervisorResponse(pipe, response, time.Now().Add(containmentSupervisorConnectionPoll))
			_ = pipe.Close()
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
				if request.Operation == "recover" || request.Operation == "abort" || wasAlreadyStopped {
					recoveryRetained.Store(true)
				}
				response.ActiveProcesses = receipt.ActiveProcesses
				if request.Operation == "abort" {
					response.Status = "aborted"
				}
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
		if request.Operation == "close" || request.Operation == "recover" || request.Operation == "abort" || recoveryRetained.Load() {
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
	if request.Operation != "hello" && request.Operation != "close" && request.Operation != "recover" && request.Operation != "abort" && request.Operation != "renew" && request.Operation != "release" {
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
	if endpoint.LaunchToken != "" && request.LaunchToken != endpoint.LaunchToken {
		return errors.New("containment supervisor launch identifier mismatch")
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
	securityDescriptor, err := containmentSupervisorSecurityDescriptor()
	if err != nil {
		return nil, fmt.Errorf("build containment supervisor pipe DACL: %w", err)
	}
	securityAttributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}
	handle, err := windows.CreateNamedPipe(
		windows.StringToUTF16Ptr(name),
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		containmentSupervisorMaxLine,
		containmentSupervisorMaxLine,
		1000,
		&securityAttributes,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

// containmentSupervisorSecurityDescriptor grants the current Windows account
// full access to this one pipe and protects the DACL from inherited entries.
// The pipe is additionally created with PIPE_REJECT_REMOTE_CLIENTS.
func containmentSupervisorSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current account SID: %w", err)
	}
	sid := user.User.Sid.String()
	return windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + sid + ")")
}

func connectContainmentSupervisorPipe(pipe *os.File, deadline time.Time) (bool, error) {
	var connected bool
	err := withContainmentSupervisorPipeHandle(pipe, func(handle windows.Handle) error {
		var err error
		connected, err = connectContainmentSupervisorPipeHandle(handle, deadline)
		return err
	})
	return connected, err
}

func connectContainmentSupervisorPipeHandle(handle windows.Handle, deadline time.Time) (bool, error) {
	if supervisorDeadlineExpired(deadline) {
		return false, errContainmentSupervisorConnectionPoll
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return false, fmt.Errorf("create containment supervisor connection event: %w", err)
	}
	defer windows.CloseHandle(event)

	overlapped := windows.Overlapped{HEvent: event | 1}
	var pinner runtime.Pinner
	pinner.Pin(&overlapped)
	defer pinner.Unpin()
	err = windows.ConnectNamedPipe(handle, &overlapped)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return true, nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return false, err
	}
	containmentSupervisorConnectPending()

	waitResult, waitErr := windows.WaitForSingleObject(event, containmentSupervisorWaitMilliseconds(deadline))
	if waitResult == windows.WAIT_OBJECT_0 && waitErr == nil {
		var transferred uint32
		err := windows.GetOverlappedResult(handle, &overlapped, &transferred, false)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
			return false, errors.Join(errContainmentSupervisorConnectionPoll, err)
		}
		return false, err
	}

	if waitResult == uint32(windows.WAIT_TIMEOUT) && waitErr == nil {
		return cancelAndDrainContainmentSupervisorConnect(handle, &overlapped, errContainmentSupervisorConnectionPoll)
	}
	terminationErr := waitErr
	if terminationErr == nil {
		terminationErr = fmt.Errorf("wait for containment supervisor connection returned status 0x%x", waitResult)
	}
	return cancelAndDrainContainmentSupervisorConnect(handle, &overlapped, terminationErr)
}

func containmentSupervisorWaitMilliseconds(deadline time.Time) uint32 {
	if deadline.IsZero() {
		return windows.INFINITE
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	milliseconds := uint64(remaining / time.Millisecond)
	if remaining%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds == 0 {
		milliseconds = 1
	}
	if milliseconds >= uint64(windows.INFINITE) {
		return windows.INFINITE - 1
	}
	return uint32(milliseconds)
}

func cancelAndDrainContainmentSupervisorConnect(handle windows.Handle, overlapped *windows.Overlapped, terminationErr error) (bool, error) {
	if terminationErr == nil {
		terminationErr = errContainmentSupervisorConnectionPoll
	}
	cancelErr := windows.CancelIoEx(handle, overlapped)
	if errors.Is(cancelErr, windows.ERROR_NOT_FOUND) {
		cancelErr = nil
	}
	var transferred uint32
	drainErr := windows.GetOverlappedResult(handle, overlapped, &transferred, true)
	if drainErr == nil {
		return false, joinContainmentSupervisorConnectTermination(terminationErr, cancelErr, nil)
	}
	return false, joinContainmentSupervisorConnectTermination(terminationErr, cancelErr, drainErr)
}

func joinContainmentSupervisorConnectTermination(terminationErr, cancelErr, drainErr error) error {
	if cancelErr == nil && drainErr == nil {
		return terminationErr
	}
	return errors.Join(terminationErr, cancelErr, drainErr)
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
