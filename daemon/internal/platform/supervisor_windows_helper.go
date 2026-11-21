//go:build windows

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
		return nil, sanitizeContainmentSupervisorPipeError(err)
	}
	return os.NewFile(uintptr(handle), containmentSupervisorPipeFileName), nil
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
	if err := terminateJobAndProveStop(job, time.Now().Add(containmentCloseDeadline)); err != nil {
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
