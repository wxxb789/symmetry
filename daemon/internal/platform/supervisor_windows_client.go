//go:build windows

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/windows"
)

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

// Owner loss is intentionally false for pre-auth transport errors.
func IsSupervisorOwnerLost(err error) bool {
	var ownerLost *SupervisorOwnerLostError
	return errors.As(err, &ownerLost)
}

// Supervisor metadata is deliberately not encoded in this field; the typed authority is
// persisted beside it in the local journal.
func TargetProcessIdentity(value string) (string, bool) {
	if _, _, err := parseWindowsProcessIdentity(value); err != nil {
		return "", false
	}
	return value, true
}

// The secret-bearing bootstrap is written only after the server PID and creation identity have
// been checked and the caller's durable CAS bind has succeeded.
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
		pipe, err := openContainmentSupervisorEndpointPipe(endpoint.PipeName)
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

// Never sends the secret and cannot authorize a bootstrap by itself. A missing pipe, PID, or Job
// is returned as an error rather than interpreted as an abort or a successful stop.
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
		pipe, err := openContainmentSupervisorEndpointPipe(endpoint.PipeName)
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

// When the handoff has no helper identity, Bind is invoked after the same-connection server
// identity check and before the secret-bearing bootstrap is written.
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

// The helper refuses abort after owner EOF; callers must use Recover and retain the durable
// stop receipt in that case.
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

// Explicit pre-authority counterpart to ReleasePreparedSupervisor, intentionally separate so a
// caller cannot release an uncertain/committed handoff without a durable stop receipt.
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

// A matching identity with WAIT_TIMEOUT is deliberately unresolved: a missing pipe is not proof
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

// Requires caller-owned old-writer quiescence, an exact creator/recovery Windows session, and
// exact absence of Job/target/helper; PID reuse, a recreated Job, or an access error fails closed.
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

// A plain process marker has no supervisor authority and therefore cannot prove a Windows Job
// stop after a daemon restart.
func RecoverPersistedContainment(pid int, value string) error {
	return fmt.Errorf("%w: persisted supervisor authority is unavailable", errContainmentStopUnproven)
}

// Verifies the persisted helper PID/creation identity and pipe owner before sending the
// secret-bearing request; a missing or mismatched authority is always unknown stop.
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

// The helper refuses release until its own stop state contains the same empty-Job proof, and a
// lost acknowledgement needs an exact helper exit/absence observation before clearing authority.
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
		// A durable stop receipt makes a missing helper harmless only after the exact helper identity is
		// independently observed absent; never turn one failed identity read into release proof.
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
		// A missing helper pipe is an idempotent release replay only after the exact helper process is
		// independently proven exited or absent; a live helper or PID reuse stays unresolved.
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

// Records the immutable handoff before starting the helper and binds identity only after
// same-connection pipe validation; the caller must commit authority and arm its initial lease before Resume.
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
	// Durable fence: no helper process is created before this callback returns successfully.
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
	// The inherited anonymous bootstrap channel is the pre-authentication boundary: its peer is the
	// exact helper child, so no same-user process can race a public named pipe before Bind completes.
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

// Owns only pre-authority cleanup: it must not advertise durable authority or stop/release
// capabilities; jobContainment still releases the daemon-side Job handle when that attempt is unproven.
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

// Pre-authority cleanup only: never a stop/release request with an incomplete endpoint; retry
// killing/reaping the helper, while jobContainment releases the daemon-side Job independently.
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
		// The release response can be lost after the helper closed its Job handle; if the helper is
		// already gone, replay is safe because the receipt was persisted before this operation.
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
		// Used only while containment setup is aborted before durable authority: the caller owns the Job
		// and will terminate the target, so reap the helper so no orphan remains.
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
		pipe, openErr := openContainmentSupervisorEndpointPipe(endpoint.PipeName)
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
		return nil, sanitizeContainmentSupervisorPipeError(err)
	}
	return os.NewFile(uintptr(handle), containmentSupervisorPipeFileName), nil
}

func openContainmentSupervisorEndpointPipe(name string) (*os.File, error) {
	file, err := openContainmentSupervisorPipe(name)
	if err != nil {
		return nil, sanitizeContainmentSupervisorPipeError(err)
	}
	return file, nil
}

// Caller must durably prepare before starting the helper; the named Job identifier is never a
// credential, and CreatorSessionID keeps a cross-session absence from counting as abort proof.
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

// The name derives only from the public random Job identifier; the authority secret never enters
// a kernel object name, command line, environment, or log.
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

func writeBootstrapPayload(file *os.File, value containmentSupervisorBootstrap, deadline time.Time) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return writeSupervisorLine(file, encoded, deadline)
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
