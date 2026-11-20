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
	"syscall"
	"time"

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

const containmentSupervisorOwnerLostMessage = "containment supervisor owner was lost; recovery is required"

var errContainmentSupervisorOwnerLost = errors.New(containmentSupervisorOwnerLostMessage)

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

func newContainmentSupervisorOwnerWatchdog(file *os.File) *containmentSupervisorOwnerWatchdog {
	watchdog := &containmentSupervisorOwnerWatchdog{
		file: file,
		lost: make(chan struct{}),
		done: make(chan struct{}),
	}
	go watchdog.watch()
	return watchdog
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
	openContainmentSupervisorPipe       = openContainmentSupervisorPipeFile
	getContainmentSupervisorServerPID   = windows.GetNamedPipeServerProcessId
	containmentSupervisorConnectPending = func() {}
	readContainmentSupervisorLine       = readSupervisorLine
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

// SupervisorHandoffAbortVerifier is supplied by the caller that still owns
// the current daemon/store lifetime. It must return nil only after the old
// writer is quiesced; a boolean or an absent pipe is not sufficient proof.
type SupervisorHandoffAbortVerifier func() error

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
		pipe, err := openContainmentSupervisorEndpointPipe(endpoint.PipeName)
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

const containmentSupervisorPipeFileName = "containment supervisor pipe"

// sanitizeContainmentSupervisorPipeError preserves the typed cause while
// removing the random named-pipe path from OS error formatting.
func sanitizeContainmentSupervisorPipeError(err error) error {
	switch typed := err.(type) {
	case *os.PathError:
		copy := *typed
		copy.Path = containmentSupervisorPipeFileName
		copy.Err = sanitizeContainmentSupervisorPipeError(typed.Err)
		return &copy
	case *os.SyscallError:
		copy := *typed
		copy.Err = sanitizeContainmentSupervisorPipeError(typed.Err)
		return &copy
	default:
		return err
	}
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

func randomContainmentSecret() (string, error) {
	value := make([]byte, authority.SecretBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
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
