// Package execution launches and supervises configured coding-agent processes.
package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

const (
	readChunkSize           = 32 * 1024
	eventQueueCapacity      = 64
	defaultTerminationGrace = 5 * time.Second
)

// ErrInputClosed reports that the process input transport can no longer accept
// a complete write.
var ErrInputClosed = errors.New("process standard input is closed")

// Stream identifies the source of an output event.
type Stream string

const (
	// Stdout is output emitted on the child process's standard output.
	Stdout Stream = "stdout"
	// Stderr is output emitted on the child process's standard error.
	Stderr Stream = "stderr"
)

// Event is one ordered chunk of process output. Data is owned by the event and
// remains valid after Handle returns.
type Event struct {
	Stream   Stream
	Sequence uint64
	At       time.Time
	Data     []byte
}

// Sink receives output events on a dedicated delivery goroutine. Handle calls
// are strictly ordered by Event.Sequence.
type Sink interface {
	Handle(context.Context, Event) error
}

// SinkFunc adapts a function into a Sink.
type SinkFunc func(context.Context, Event) error

// Handle calls the wrapped function.
func (function SinkFunc) Handle(ctx context.Context, event Event) error {
	return function(ctx, event)
}

// Invocation describes one direct process launch. Program may be an absolute
// path or a trusted configured executable name resolved through exec.LookPath;
// it is never interpreted by a shell. Env is always passed to the child
// verbatim, including when it is empty, so the daemon environment is never
// inherited implicitly.
type Invocation struct {
	Program                string
	Args                   []string
	Dir                    string
	Env                    []string
	InitialInput           []byte
	CloseInputAfterInitial bool
	// PersistProcess runs immediately after the OS process identity is
	// captured, before output readers or the Process value are exposed.
	PersistProcess func(pid int, identity string) error
	// PersistProcessAuthority runs immediately after PersistProcess for a
	// supervisor-backed containment owner. Legacy callers may leave it nil.
	PersistProcessAuthority func(pid int, identity string, value *authority.Supervisor) error
	// PersistContainmentStopReceipt runs after the process containment boundary
	// proves an empty tree and before an independent supervisor is released.
	// A failure retains that helper authority for retry or restart recovery.
	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error
	// InitialLeaseDeadline arms an optional independent containment watchdog
	// after local authority persistence and before initial input is written. A
	// zero value leaves legacy/test containment unchanged. When
	// InitialLeaseDeadlineAt is set, it is authoritative and this field is only
	// retained as the relative-duration fallback for older callers.
	InitialLeaseDeadline time.Duration
	// InitialLeaseDeadlineAt is the local monotonic deadline established at the
	// claim request start. The runner recomputes the remaining duration immediately
	// before arming the watchdog so attach, helper handshakes and persistence can
	// only consume lease time.
	InitialLeaseDeadlineAt time.Time
	InitialLeaseSequence   uint64
}

// LeaseRenewer is an optional process capability. The app uses a type
// assertion so legacy and unsupported-platform process fakes do not acquire a
// second required lifecycle method.
type LeaseRenewer interface {
	RenewLease(deadline time.Duration, sequence uint64) error
	LeaseRenewalAvailable() bool
}

// Runner starts coding-agent process invocations.
type Runner struct {
	configureProcess func(*exec.Cmd) error
	attachProcess    func(*os.Process) (platform.Containment, string, error)
	launchProcess    processLauncher
	now              func() time.Time
}

// startedProcess is the small lifecycle boundary shared by the standard
// library launcher and the Windows native suspended launcher. The latter does
// not have an exec.Cmd-compatible ProcessState, so the process lifecycle is
// deliberately carried by functions instead of a second process abstraction.
type startedProcess struct {
	command                       *exec.Cmd
	pid                           int
	identity                      string
	containment                   platform.Containment
	persistAuthority              func(int, string, *authority.Supervisor) error
	containmentAuthority          *authority.Supervisor
	persistStopReceipt            func(int, string, authority.StopReceipt) error
	stopReceiptRequired           bool
	containmentAuthorityUncertain bool
	wait                          func() (int, error)
	kill                          func() error
	close                         func() error
	resume                        func() error
}

type processLauncher func(
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error)

var errNativeProcessLauncherUnavailable = errors.New("native suspended process launcher is unavailable")

// NewRunner creates a process runner.
func NewRunner() Runner {
	return Runner{
		configureProcess: platform.ConfigureProcess,
		attachProcess:    platform.AttachProcess,
		launchProcess:    defaultProcessLauncher,
		now:              time.Now,
	}
}

// Process is a running child process. Its fixed-size event queue decouples pipe
// draining from Sink latency. When the queue is full, readers apply bounded
// backpressure; Terminate remains independent of sink delivery and can still
// stop the entire process tree.
type Process struct {
	// PID is the operating-system process identifier of the launched agent.
	PID int
	// Identity combines the PID with platform process-creation data so a later
	// daemon instance can reject a recycled PID after restart.
	Identity string

	command                       *exec.Cmd
	backend                       *startedProcess
	sink                          Sink
	containment                   platform.Containment
	persistAuthority              func(int, string, *authority.Supervisor) error
	containmentAuthority          *authority.Supervisor
	persistStopReceipt            func(int, string, authority.StopReceipt) error
	stopReceiptRequired           bool
	containmentAuthorityUncertain bool
	sinkContext                   context.Context
	cancelSink                    context.CancelFunc

	stdinMutex       sync.Mutex
	stdin            *os.File
	stdinWritePermit chan struct{}

	events       chan Event
	eventMutex   sync.Mutex
	nextSequence uint64
	readers      sync.WaitGroup
	deliveryDone chan struct{}

	resultDone  chan struct{}
	commandDone chan struct{}
	result      Result

	errorMutex       sync.Mutex
	sinkError        error
	outputError      error
	containmentError error

	containmentFinalizeMutex       sync.Mutex
	containmentFinalized           bool
	containmentFinalizationStarted bool
	containmentCloseAttempted      bool
	containmentInitialError        error
	containmentReceiptSaved        bool

	terminationMutex sync.Mutex
	terminationDone  chan struct{}
	terminationStart bool
	terminated       bool
	terminationError error
	outputStop       chan struct{}
	outputStopOnce   sync.Once
	outputTruncated  bool
}

// Result describes the completed process without requiring callers to parse an
// exec.ExitError. WaitError is non-nil for a non-zero exit or launch wait fault.
type Result struct {
	PID        int
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	Terminated bool

	WaitError       error
	SinkError       error
	OutputError     error
	OutputTruncated bool

	TerminationError error
	ContainmentError error
}

// Success reports whether the process exited cleanly and all output reached the
// sink without an observed error.
func (result Result) Success() bool {
	return result.ExitCode == 0 && !result.Terminated && result.WaitError == nil &&
		result.SinkError == nil && result.OutputError == nil && result.TerminationError == nil &&
		result.ContainmentError == nil && !result.OutputTruncated
}

// Start launches an invocation directly, without a command shell. It starts
// separate readers for stdout and stderr before asynchronously waiting for the
// command, and cancellation of ctx invokes the same tree termination path as
// an explicit Terminate call.
// After a successful OS launch, errors retain a non-nil Process. Callers must
// keep that owner and consume its final Wait result even when Start fails.
func (runner Runner) Start(ctx context.Context, invocation Invocation, sink Sink) (*Process, error) {
	if ctx == nil {
		return nil, errors.New("execution context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("execution context already cancelled: %w", err)
	}
	program, err := validateInvocation(invocation)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		sink = SinkFunc(func(context.Context, Event) error { return nil })
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite)
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite)
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := ctx.Err(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, fmt.Errorf("execution context already cancelled: %w", err)
	}

	command := exec.Command(program, invocation.Args...)
	command.Dir = invocation.Dir
	command.Env = cloneEnvironment(invocation.Env)
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	if err := runner.configure(command); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, fmt.Errorf("configure process containment for %q: %w", program, err)
	}

	startedAt := time.Now().UTC()
	var started *startedProcess
	if runner.launchProcess != nil {
		started, err = runner.launchProcess(command, invocation, stdinRead, stdoutWrite, stderrWrite)
		if err != nil && !errors.Is(err, errNativeProcessLauncherUnavailable) {
			if started == nil {
				closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
				return nil, fmt.Errorf("start %q: %w", invocation.Program, err)
			}
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("start %q: %w", invocation.Program, err),
				err,
			)
		}
		if err == nil && started == nil {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, fmt.Errorf("start %q: native launcher returned no process", invocation.Program)
		}
	}
	if started == nil {
		if err := command.Start(); err != nil {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, fmt.Errorf("start %q: %w", invocation.Program, err)
		}
		containment, identity, attachErr := runner.attach(command.Process)
		started = legacyStartedProcess(command, containment, identity)
		if attachErr != nil {
			wrappedErr := fmt.Errorf("contain process tree for %q: %w", program, attachErr)
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				wrappedErr,
				wrappedErr,
			)
		}
	}
	containment := started.containment
	identity := started.identity
	if invocation.PersistProcess != nil {
		if persistErr := invocation.PersistProcess(started.pid, identity); persistErr != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("persist process identity: %w", persistErr),
				nil,
			)
		}
	}
	if invocation.PersistProcessAuthority == nil {
		started.persistStopReceipt = invocation.PersistContainmentStopReceipt
		started.stopReceiptRequired = invocation.PersistContainmentStopReceipt != nil
	}
	if invocation.PersistProcessAuthority != nil {
		started.persistStopReceipt = invocation.PersistContainmentStopReceipt
		started.stopReceiptRequired = invocation.PersistContainmentStopReceipt != nil
		if capability, ok := containment.(platform.ContainmentAuthorityCapability); ok && !capability.ContainmentAuthorityAvailable() {
			// The Windows test binary deliberately uses the legacy in-process
			// containment seam because it cannot dispatch the daemon helper mode.
			// No authority is exposed, so recovery remains conservative.
		} else if provider, ok := containment.(platform.ContainmentAuthorityProvider); ok {
			value := provider.ContainmentAuthority()
			// Freeze the post-authority cleanup contract before invoking the
			// callback. The callback may return an error after its atomic rename;
			// either outcome must retain the receipt/release fence and remain
			// retryable without falling back to AbortContainment.
			started.containmentAuthorityUncertain = true
			if value == nil {
				const message = "containment authority is unavailable after process marker persistence"
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					errors.New(message),
					errors.New(message),
				)
			}
			authorityCopy := value.Clone()
			started.persistAuthority = invocation.PersistProcessAuthority
			started.containmentAuthority = &authorityCopy
			callbackAuthority := authorityCopy.Clone()
			if persistErr := invocation.PersistProcessAuthority(started.pid, identity, &callbackAuthority); persistErr != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("persist process containment authority: %w", persistErr),
					nil,
				)
			}
			started.containmentAuthorityUncertain = false
		}
	}
	if capability, ok := containment.(platform.ContainmentAuthorityCapability); ok && !capability.ContainmentAuthorityAvailable() {
		// A legacy/noop containment owner has no independently releasable
		// authority. Its local Close still proves the owned tree stopped, but it
		// cannot produce the durable supervisor receipt used by the real helper.
		// Do not turn the absence of that optional authority into a process
		// failure when the daemon supplies the receipt callback unconditionally.
		started.persistStopReceipt = nil
		started.stopReceiptRequired = false
	}
	initialLeaseDeadline, initialLeaseRequested, deadlineErr := runner.initialLeaseDeadline(invocation)
	if deadlineErr != nil {
		return cleanupFailedStartStarted(
			started,
			stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
			startedAt,
			deadlineErr,
			nil,
		)
	}
	if initialLeaseRequested {
		if renewer, ok := containment.(platform.ContainmentLeaseRenewer); ok && renewer.LeaseRenewalAvailable() {
			if invocation.InitialLeaseSequence == 0 {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					errors.New("initial containment lease sequence is missing"),
					nil,
				)
			}
			if err := renewer.RenewLease(initialLeaseDeadline, invocation.InitialLeaseSequence); err != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("arm initial containment lease: %w", err),
					nil,
				)
			}
		}
	}
	if started.resume != nil {
		if err := started.resume(); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("resume process %q: %w", program, err),
				nil,
			)
		}
	}

	// These ends belong only to the child after launch. Closing them here is
	// essential: otherwise the readers would never observe EOF.
	closeFiles(stdinRead, stdoutWrite, stderrWrite)

	process := newProcessFromStarted(started, sink, startedAt, stdinWrite, nil)
	process.start(stdoutRead, stderrRead, ctx)

	if len(invocation.InitialInput) > 0 {
		if err := process.WriteInputContext(ctx, invocation.InitialInput); err != nil {
			return cleanupAfterInputSetupFailure(process, fmt.Errorf("write initial input: %w", err), "initial input")
		}
	}
	if invocation.CloseInputAfterInitial {
		if err := process.CloseInput(); err != nil {
			return cleanupAfterInputSetupFailure(process, fmt.Errorf("close initial input: %w", err), "initial input close")
		}
	}
	return process, nil
}

var errInitialLeaseDeadlineExpired = errors.New("initial containment lease deadline has elapsed")

func (runner Runner) currentTime() time.Time {
	if runner.now != nil {
		return runner.now()
	}
	return time.Now()
}

func (runner Runner) initialLeaseDeadline(invocation Invocation) (time.Duration, bool, error) {
	if !invocation.InitialLeaseDeadlineAt.IsZero() {
		remaining := invocation.InitialLeaseDeadlineAt.Sub(runner.currentTime())
		if remaining <= 0 {
			return 0, true, errInitialLeaseDeadlineExpired
		}
		return remaining, true, nil
	}
	if invocation.InitialLeaseDeadline > 0 {
		return invocation.InitialLeaseDeadline, true, nil
	}
	return 0, false, nil
}

// cleanupAfterInputSetupFailure retains the post-start Process even when the
// bounded termination completes, because PersistProcess may already have
// committed a marker that its caller must compare-and-clear.
func cleanupAfterInputSetupFailure(process *Process, inputErr error, operation string) (*Process, error) {
	terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace)
	terminationErr := process.Terminate(terminationContext, 0)
	cancel()
	if terminationErr != nil {
		return process, errors.Join(inputErr, fmt.Errorf("terminate after %s failure: %w", operation, terminationErr))
	}
	return process, inputErr
}

func (runner Runner) configure(command *exec.Cmd) error {
	if runner.configureProcess != nil {
		return runner.configureProcess(command)
	}
	return platform.ConfigureProcess(command)
}

func (runner Runner) attach(process *os.Process) (platform.Containment, string, error) {
	if runner.attachProcess != nil {
		return runner.attachProcess(process)
	}
	return platform.AttachProcess(process)
}

func legacyStartedProcess(command *exec.Cmd, containment platform.Containment, identity string) *startedProcess {
	return &startedProcess{
		command:     command,
		pid:         command.Process.Pid,
		identity:    identity,
		containment: containment,
		wait: func() (int, error) {
			err := command.Wait()
			code := -1
			if command.ProcessState != nil {
				code = command.ProcessState.ExitCode()
			}
			return code, err
		},
		kill: command.Process.Kill,
	}
}

func newProcess(command *exec.Cmd, sink Sink, containment platform.Containment, identity string, startedAt time.Time, stdin *os.File) *Process {
	return newProcessFromStarted(legacyStartedProcess(command, containment, identity), sink, startedAt, stdin, nil)
}

func newProcessFromStarted(started *startedProcess, sink Sink, startedAt time.Time, stdin *os.File, persistStopReceipt func(int, string, authority.StopReceipt) error) *Process {
	if persistStopReceipt == nil && started != nil {
		persistStopReceipt = started.persistStopReceipt
	}
	var containmentAuthority *authority.Supervisor
	if started != nil && started.containmentAuthority != nil {
		authorityCopy := started.containmentAuthority.Clone()
		containmentAuthority = &authorityCopy
	}
	sinkContext, cancelSink := context.WithCancel(context.Background())
	process := &Process{
		PID:                           started.pid,
		Identity:                      started.identity,
		command:                       started.command,
		backend:                       started,
		sink:                          sink,
		containment:                   started.containment,
		persistAuthority:              started.persistAuthority,
		containmentAuthority:          containmentAuthority,
		persistStopReceipt:            persistStopReceipt,
		stopReceiptRequired:           started.stopReceiptRequired,
		containmentAuthorityUncertain: started.containmentAuthorityUncertain,
		sinkContext:                   sinkContext,
		cancelSink:                    cancelSink,
		stdin:                         stdin,
		stdinWritePermit:              make(chan struct{}, 1),
		events:                        make(chan Event, eventQueueCapacity),
		deliveryDone:                  make(chan struct{}),
		resultDone:                    make(chan struct{}),
		commandDone:                   make(chan struct{}),
		terminationDone:               make(chan struct{}),
		outputStop:                    make(chan struct{}),
		result: Result{
			PID:       started.pid,
			StartedAt: startedAt,
		},
	}
	process.stdinWritePermit <- struct{}{}
	return process
}

func (process *Process) start(stdoutRead, stderrRead *os.File, ctx context.Context) {
	process.readers.Add(2)
	go process.readOutput(stdoutRead, Stdout)
	go process.readOutput(stderrRead, Stderr)
	go process.deliverOutput()
	go process.waitForCompletion()
	go process.terminateWhenContextCancels(ctx)
}

// cleanupFailedStartStarted hands physical cleanup to Process so an
// indeterminate shutdown keeps its original process handle and is still reaped
// by one owner.
func cleanupFailedStartStarted(
	started *startedProcess,
	stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite *os.File,
	startedAt time.Time,
	startErr error,
	containmentErr error,
) (*Process, error) {
	// Do not expose pre-persistence output to the caller's sink. The cleanup
	// Process still drains the pipes and owns the child wait.
	closeFiles(stdinRead, stdoutWrite, stderrWrite)
	process := newProcessFromStarted(
		started,
		SinkFunc(func(context.Context, Event) error { return nil }),
		startedAt,
		stdinWrite,
		nil,
	)
	process.recordContainmentError(containmentErr)
	process.start(stdoutRead, stderrRead, context.Background())

	cleanupContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace+time.Second)
	terminationErr := process.Terminate(cleanupContext, 0)
	cancel()
	return process, errors.Join(startErr, terminationErr)
}

// WriteInput appends human input to the agent's standard input. Concurrent
// callers are serialized, preserving write order and avoiding interleaving.
func (process *Process) WriteInput(input []byte) error {
	return process.WriteInputContext(context.Background(), input)
}

// WriteInputContext appends input while allowing a caller to cancel a blocked
// pipe write. A nil result means every byte was written. Once cancellation or
// a write failure interrupts an attempted write, the input transport is closed
// permanently so a later caller cannot mistake a partial request for success.
func (process *Process) WriteInputContext(ctx context.Context, input []byte) error {
	if ctx == nil {
		return errors.New("input context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write process input: %w", err)
	}
	if len(input) == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("write process input: %w", ctx.Err())
	case <-process.stdinWritePermit:
	}
	defer func() { process.stdinWritePermit <- struct{}{} }()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write process input: %w", err)
	}
	process.stdinMutex.Lock()
	stdin := process.stdin
	process.stdinMutex.Unlock()
	if stdin == nil {
		return ErrInputClosed
	}

	cancellationDone := make(chan struct{})
	cancelled := false
	stopCancellation := context.AfterFunc(ctx, func() {
		cancelled = true
		_ = process.closeInputFile(stdin)
		close(cancellationDone)
	})

	writeErr := writeAll(stdin, input)
	if !stopCancellation() {
		<-cancellationDone
	}
	if cancelled {
		return fmt.Errorf("write process input cancelled; standard input is closed: %w", errors.Join(ctx.Err(), ErrInputClosed))
	}
	if writeErr != nil {
		_ = process.closeInputFile(stdin)
		return fmt.Errorf("write process input interrupted; standard input is closed: %w", errors.Join(writeErr, ErrInputClosed))
	}
	return nil
}

// CloseInput sends EOF to the agent while allowing its remaining stdout and
// stderr output to drain. It is safe to call more than once.
func (process *Process) CloseInput() error {
	process.stdinMutex.Lock()
	stdin := process.stdin
	process.stdin = nil
	process.stdinMutex.Unlock()
	if stdin == nil {
		return nil
	}
	err := stdin.Close()
	if err != nil {
		return fmt.Errorf("close process input: %w", err)
	}
	return nil
}

// closeInputFile closes stdin only when it is still the process's active
// transport. This lets a cancellation race safely with CloseInput.
func (process *Process) closeInputFile(stdin *os.File) error {
	process.stdinMutex.Lock()
	if process.stdin != stdin {
		process.stdinMutex.Unlock()
		return nil
	}
	process.stdin = nil
	process.stdinMutex.Unlock()
	return stdin.Close()
}

func writeAll(stdin *os.File, input []byte) error {
	for len(input) > 0 {
		written, err := stdin.Write(input)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		input = input[written:]
	}
	return nil
}

// Wait waits for process exit, pipe draining, and queued sink delivery. The
// underlying command is waited exactly once regardless of how many callers use
// Wait concurrently.
func (process *Process) Wait() Result {
	<-process.resultDone
	process.errorMutex.Lock()
	result := process.result
	result.ContainmentError = process.containmentError
	process.errorMutex.Unlock()
	return result
}

// ProcessDetails returns the restart-safe identity recorded when this process
// was started. Both values remain stable for the Process lifetime.
func (process *Process) ProcessDetails() (int, string) {
	if process == nil {
		return 0, ""
	}
	return process.PID, process.Identity
}

// LeaseRenewalAvailable reports whether the process has an independent local
// containment watchdog. Unsupported and legacy containment remains usable
// without this optional capability.
func (process *Process) LeaseRenewalAvailable() bool {
	if process == nil || process.containment == nil {
		return false
	}
	renewer, ok := process.containment.(platform.ContainmentLeaseRenewer)
	return ok && renewer.LeaseRenewalAvailable()
}

// RenewLease arms the independent containment watchdog with a relative local
// deadline. The caller owns the monotonic sequence for this process authority.
func (process *Process) RenewLease(deadline time.Duration, sequence uint64) error {
	if process == nil || process.containment == nil {
		return fmt.Errorf("%w: process containment lease is unavailable", errors.ErrUnsupported)
	}
	renewer, ok := process.containment.(platform.ContainmentLeaseRenewer)
	if !ok || !renewer.LeaseRenewalAvailable() {
		return fmt.Errorf("%w: process containment lease is unavailable", errors.ErrUnsupported)
	}
	return renewer.RenewLease(deadline, sequence)
}

// Terminate requests graceful process-tree termination, waits grace, then
// forcefully stops the remaining tree. Multiple callers share one operation;
// the first grace duration wins and all calls are otherwise idempotent.
func (process *Process) Terminate(ctx context.Context, grace time.Duration) error {
	if ctx == nil {
		return errors.New("termination context must not be nil")
	}
	process.terminationMutex.Lock()
	if !process.terminationStart {
		select {
		case <-process.resultDone:
			process.terminationMutex.Unlock()
			return process.closeContainment()
		default:
		}
		process.terminationStart = true
		process.terminated = true
		process.stopOutputDelivery()
		go process.terminateTree(grace)
	}
	done := process.terminationDone
	process.terminationMutex.Unlock()

	select {
	case <-done:
		process.terminationMutex.Lock()
		terminationError := process.terminationError
		process.terminationMutex.Unlock()
		return errors.Join(terminationError, process.containmentFailure())
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FinalizeContainment retries only the durable receipt/release phase after the
// process result is already final. It never waits for the child and never
// reissues physical termination. Callers that have not observed resultDone get
// an immediate error rather than creating an unbounded cleanup wait.
func (process *Process) FinalizeContainment() error {
	if process == nil {
		return errors.New("process is nil")
	}
	select {
	case <-process.resultDone:
		return process.closeContainment()
	default:
		return errors.New("process result is not complete")
	}
}

func (process *Process) readOutput(reader *os.File, stream Stream) {
	defer process.readers.Done()
	defer reader.Close()

	buffer := make([]byte, readChunkSize)
	for {
		count, err := reader.Read(buffer)
		if count > 0 && !process.enqueue(stream, buffer[:count]) {
			return
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			process.recordOutputError(fmt.Errorf("read %s: %w", stream, err))
		}
		return
	}
}

func (process *Process) enqueue(stream Stream, data []byte) bool {
	select {
	case <-process.outputStop:
		return false
	default:
	}

	process.eventMutex.Lock()
	defer process.eventMutex.Unlock()

	event := Event{
		Stream:   stream,
		Sequence: process.nextSequence + 1,
		At:       time.Now().UTC(),
		Data:     append([]byte(nil), data...),
	}
	select {
	case process.events <- event:
		process.nextSequence++
		return true
	case <-process.outputStop:
		return false
	}
}

func (process *Process) deliverOutput() {
	defer close(process.deliveryDone)
	for event := range process.events {
		if process.outputDeliveryStopped() {
			continue
		}
		if err := process.sink.Handle(process.sinkContext, event); err != nil && !process.outputDeliveryStopped() {
			process.recordSinkError(err)
		}
	}
}

func (process *Process) waitForCompletion() {
	exitCode := -1
	var waitError error
	if process.backend == nil || process.backend.wait == nil {
		waitError = errors.New("process wait backend is unavailable")
	} else {
		exitCode, waitError = process.backend.wait()
	}
	close(process.commandDone)
	_ = process.CloseInput()

	if !process.terminationRequested() {
		process.closeContainment()
	}

	process.readers.Wait()
	close(process.events)
	<-process.deliveryDone
	process.cancelSink()
	if process.terminationRequested() {
		process.closeContainment()
	}
	if process.backend != nil && process.backend.close != nil {
		if err := process.backend.close(); err != nil {
			// The containment receipt/release boundary is independent from local
			// process-handle cleanup. Keep this failure observable without
			// misreporting a successfully proven containment stop.
			process.recordTerminationError(fmt.Errorf("close started process: %w", err))
		}
	}

	process.terminationMutex.Lock()
	terminated := process.terminated
	terminationError := process.terminationError
	process.terminationMutex.Unlock()

	process.errorMutex.Lock()
	process.result.ExitCode = exitCode
	process.result.FinishedAt = time.Now().UTC()
	process.result.Terminated = terminated
	process.result.WaitError = waitError
	process.result.SinkError = process.sinkError
	process.result.OutputError = process.outputError
	process.result.OutputTruncated = process.outputTruncated
	process.result.TerminationError = terminationError
	process.result.ContainmentError = process.containmentError
	process.errorMutex.Unlock()
	close(process.resultDone)
}

func (process *Process) terminateWhenContextCancels(ctx context.Context) {
	select {
	case <-ctx.Done():
		terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace+time.Second)
		_ = process.Terminate(terminationContext, defaultTerminationGrace)
		cancel()
	case <-process.resultDone:
	}
}

func (process *Process) terminateTree(grace time.Duration) {
	defer close(process.terminationDone)
	var softTerminationError error
	if process.containment != nil {
		softTerminationError = process.containment.Terminate(false)
		if softTerminationError != nil && !isUnsupportedOnly(softTerminationError) {
			process.recordTerminationError(softTerminationError)
		}
	}

	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-process.resultDone:
			return
		case <-timer.C:
		}
	} else {
		select {
		case <-process.resultDone:
			return
		default:
		}
	}

	if process.containment == nil {
		if err := process.killProcess(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			process.recordTerminationError(err)
			return
		}
		<-process.resultDone
		return
	}

	if err := process.containment.Terminate(true); err != nil {
		// Containment is responsible for descendants, but it must not be the
		// sole termination mechanism for the root process. Otherwise a failed
		// job/task-kill leaves command.Wait blocked forever after a failed
		// initial-input cleanup path.
		killErr := process.killProcess()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		process.recordTerminationError(errors.Join(err, killErr))
		return
	}
	<-process.resultDone
}

func (process *Process) killProcess() error {
	if process == nil {
		return errors.New("process is nil")
	}
	if process.backend != nil && process.backend.kill != nil {
		return process.backend.kill()
	}
	if process.command == nil || process.command.Process == nil {
		return errors.New("process kill backend is unavailable")
	}
	return process.command.Process.Kill()
}

func isUnsupportedOnly(err error) bool {
	if err == nil {
		return false
	}
	type multiUnwrapper interface{ Unwrap() []error }
	if wrapped, ok := err.(multiUnwrapper); ok {
		wrappedErrors := wrapped.Unwrap()
		return len(wrappedErrors) == 1 && isUnsupportedOnly(wrappedErrors[0])
	}
	type singleUnwrapper interface{ Unwrap() error }
	if wrapped, ok := err.(singleUnwrapper); ok {
		return isUnsupportedOnly(wrapped.Unwrap())
	}
	return errors.Is(err, errors.ErrUnsupported)
}

func (process *Process) closeContainment() error {
	if process == nil || process.containment == nil {
		return nil
	}
	process.containmentFinalizeMutex.Lock()
	defer process.containmentFinalizeMutex.Unlock()
	if !process.containmentFinalizationStarted {
		process.containmentFinalizationStarted = true
		process.containmentInitialError = process.containmentFailure()
	}
	if process.containmentFinalized {
		return process.containmentFinalizeError()
	}
	provider, hasReceiptProvider := process.containment.(platform.ContainmentStopReceiptProvider)
	if !hasReceiptProvider && process.stopReceiptRequired {
		err := errors.New("containment stop receipt provider is missing")
		process.setContainmentError(errors.Join(process.containmentInitialError, err))
		return err
	}
	if !hasReceiptProvider && process.containmentAuthorityUncertain {
		err := errors.New("containment authority persistence is uncertain")
		process.setContainmentError(errors.Join(process.containmentInitialError, err))
		return err
	}

	if !process.containmentCloseAttempted {
		process.containmentCloseAttempted = true
		if err := process.containment.Close(); err != nil {
			process.setContainmentError(errors.Join(process.containmentInitialError, err))
			return err
		}
	}

	if !hasReceiptProvider {
		process.containmentFinalized = true
		process.setContainmentError(process.containmentInitialError)
		return nil
	}
	receipt, available := provider.ContainmentStopReceipt()
	if available {
		if process.containmentAuthorityUncertain {
			if process.persistAuthority == nil || process.containmentAuthority == nil {
				err := errors.New("containment authority persistence callback is unavailable")
				process.setContainmentError(errors.Join(process.containmentInitialError, err))
				return err
			}
			authorityCopy := process.containmentAuthority.Clone()
			callbackAuthority := authorityCopy.Clone()
			if err := process.persistAuthority(process.PID, process.Identity, &callbackAuthority); err != nil {
				process.setContainmentError(errors.Join(process.containmentInitialError, fmt.Errorf("retry process containment authority persistence: %w", err)))
				return err
			}
			process.containmentAuthorityUncertain = false
		}
		if !process.containmentReceiptSaved {
			if process.persistStopReceipt == nil {
				if process.containmentAuthorityUncertain {
					err := errors.New("containment authority persistence is uncertain")
					process.setContainmentError(errors.Join(process.containmentInitialError, err))
					return err
				}
				if !process.stopReceiptRequired {
					if aborter, ok := process.containment.(platform.ContainmentStopReceiptAborter); ok {
						if err := aborter.AbortContainment(); err != nil {
							process.setContainmentError(errors.Join(process.containmentInitialError, err))
							return err
						}
						process.containmentReceiptSaved = true
					} else {
						err := errors.New("pre-authority containment abort capability is missing")
						process.setContainmentError(errors.Join(process.containmentInitialError, err))
						return err
					}
				} else {
					err := errors.New("durable containment stop receipt callback is missing")
					process.setContainmentError(errors.Join(process.containmentInitialError, err))
					return err
				}
			}
			if process.persistStopReceipt != nil {
				if err := process.persistStopReceipt(process.PID, process.Identity, receipt); err != nil {
					process.setContainmentError(errors.Join(process.containmentInitialError, fmt.Errorf("persist containment stop receipt: %w", err)))
					return err
				}
				process.containmentReceiptSaved = true
			}
		}
		if process.persistStopReceipt == nil && !process.stopReceiptRequired {
			if process.containmentAuthorityUncertain {
				err := errors.New("containment authority persistence is uncertain")
				process.setContainmentError(errors.Join(process.containmentInitialError, err))
				return err
			}
			// AbortContainment already performed the release.
			process.containmentFinalized = true
			process.setContainmentError(process.containmentInitialError)
			return nil
		}
		if releaser, ok := process.containment.(platform.ContainmentStopReceiptReleaser); ok {
			if err := releaser.ReleaseContainment(); err != nil {
				process.setContainmentError(errors.Join(process.containmentInitialError, fmt.Errorf("release containment authority: %w", err)))
				return err
			}
		} else {
			err := errors.New("containment stop receipt releaser is missing")
			process.setContainmentError(errors.Join(process.containmentInitialError, err))
			return err
		}
	} else if process.stopReceiptRequired || process.containmentAuthorityUncertain {
		err := errors.New("containment stop receipt is unavailable")
		process.setContainmentError(errors.Join(process.containmentInitialError, err))
		return err
	}
	process.containmentFinalized = true
	process.setContainmentError(process.containmentInitialError)
	return nil
}

func (process *Process) containmentFailure() error {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	return process.containmentError
}

func (process *Process) containmentFinalizeError() error {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	return process.containmentError
}

func (process *Process) setContainmentError(err error) {
	process.errorMutex.Lock()
	process.containmentError = err
	process.errorMutex.Unlock()
}

func (process *Process) stopOutputDelivery() {
	process.outputStopOnce.Do(func() {
		process.errorMutex.Lock()
		process.outputTruncated = true
		process.errorMutex.Unlock()
		close(process.outputStop)
		process.cancelSink()
	})
}

func (process *Process) outputDeliveryStopped() bool {
	select {
	case <-process.outputStop:
		return true
	default:
		return false
	}
}

func (process *Process) terminationRequested() bool {
	process.terminationMutex.Lock()
	defer process.terminationMutex.Unlock()
	return process.terminationStart
}

func (process *Process) recordSinkError(err error) {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.sinkError == nil {
		process.sinkError = err
	}
}

func (process *Process) recordOutputError(err error) {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.outputError == nil {
		process.outputError = err
	}
}

func (process *Process) recordContainmentError(err error) {
	if err == nil {
		return
	}
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.containmentError == nil {
		process.containmentError = err
	}
}

func (process *Process) recordTerminationError(err error) {
	if err == nil {
		return
	}
	process.terminationMutex.Lock()
	defer process.terminationMutex.Unlock()
	process.terminationError = errors.Join(process.terminationError, err)
}

func validateInvocation(invocation Invocation) (string, error) {
	if strings.TrimSpace(invocation.Program) == "" {
		return "", errors.New("program must not be empty")
	}
	program, err := exec.LookPath(invocation.Program)
	if err != nil {
		return "", fmt.Errorf("find program %q: %w", invocation.Program, err)
	}
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(program))
		if extension == ".bat" || extension == ".cmd" {
			return "", errors.New("program must not be a batch script; configure a direct executable")
		}
	}
	if invocation.Dir != "" && !filepath.IsAbs(invocation.Dir) {
		return "", errors.New("working directory must be absolute when set")
	}
	for _, entry := range invocation.Env {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" || strings.ContainsRune(key, '\x00') || strings.ContainsRune(entry, '\x00') {
			return "", fmt.Errorf("invalid environment entry %q", entry)
		}
		if isControlCredential(key) {
			return "", fmt.Errorf("environment entry %q is a reserved Symmetry control credential", key)
		}
	}
	return program, nil
}

// BuildEnvironment creates an explicit child environment containing only
// operating-system essentials and named variables from the daemon environment.
// SYMMETRY_* values are excluded unless their exact name is allowlisted; control
// credentials remain prohibited even when requested explicitly.
func BuildEnvironment(allowlist ...string) ([]string, error) {
	requested := make(map[string]struct{}, len(allowlist))
	for _, key := range allowlist {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("invalid environment allowlist key %q", key)
		}
		if isControlCredential(key) {
			return nil, fmt.Errorf("environment allowlist key %q is a reserved Symmetry control credential", key)
		}
		requested[normalizeEnvironmentKey(key)] = struct{}{}
	}

	allowed := make(map[string]struct{}, len(essentialEnvironmentKeys)+len(requested))
	for _, key := range essentialEnvironmentKeys {
		allowed[normalizeEnvironmentKey(key)] = struct{}{}
	}
	for key := range requested {
		allowed[key] = struct{}{}
	}

	values := make(map[string]string, len(allowed))
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, include := allowed[normalizeEnvironmentKey(key)]; include {
			values[key] = value
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment, nil
}

var essentialEnvironmentKeys = []string{
	"ComSpec", "HOME", "HOMEDRIVE", "HOMEPATH", "LANG", "LC_ALL", "LC_CTYPE", "PATH", "PATHEXT", "SystemRoot", "SYSTEMROOT", "TEMP", "TERM", "TMP", "TMPDIR", "USERPROFILE", "WINDIR",
}

func isControlCredential(key string) bool {
	normalized := strings.ToUpper(key)
	if strings.HasPrefix(normalized, "SYMMETRY_CONTROL_") || strings.HasPrefix(normalized, "SYMMETRY_AUTH_") {
		return true
	}
	return strings.HasPrefix(normalized, "SYMMETRY_") &&
		(strings.HasSuffix(normalized, "TOKEN") || strings.HasSuffix(normalized, "_SECRET") ||
			normalized == "SYMMETRY_API_KEY" || normalized == "SYMMETRY_CREDENTIAL")
}

func normalizeEnvironmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func cloneEnvironment(environment []string) []string {
	cloned := make([]string, len(environment))
	copy(cloned, environment)
	return cloned
}

func closeFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}
