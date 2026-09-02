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

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

const (
	readChunkSize           = 32 * 1024
	eventQueueCapacity      = 64
	defaultTerminationGrace = 5 * time.Second
)

var errInputClosed = errors.New("process standard input is closed")

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
}

// Runner starts coding-agent process invocations.
type Runner struct{}

// NewRunner creates a process runner.
func NewRunner() Runner {
	return Runner{}
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

	command     *exec.Cmd
	sink        Sink
	containment platform.Containment
	sinkContext context.Context
	cancelSink  context.CancelFunc

	stdinMutex sync.Mutex
	stdin      *os.File

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
	platform.ConfigureProcess(command)

	startedAt := time.Now().UTC()
	if err := command.Start(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, fmt.Errorf("start %q: %w", invocation.Program, err)
	}
	containment, err := platform.AttachProcess(command.Process.Pid)
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("contain process tree for %q: %w", program, err)
	}
	identity, err := platform.ProcessIdentity(command.Process.Pid)
	if err != nil {
		_ = containment.Close()
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		_ = command.Wait()
		return nil, fmt.Errorf("capture process creation identity: %w", err)
	}

	// These ends belong only to the child after Start. Closing them here is
	// essential: otherwise the readers would never observe EOF.
	closeFiles(stdinRead, stdoutWrite, stderrWrite)

	sinkContext, cancelSink := context.WithCancel(context.Background())
	process := &Process{
		PID:             command.Process.Pid,
		Identity:        identity,
		command:         command,
		sink:            sink,
		containment:     containment,
		sinkContext:     sinkContext,
		cancelSink:      cancelSink,
		stdin:           stdinWrite,
		events:          make(chan Event, eventQueueCapacity),
		deliveryDone:    make(chan struct{}),
		resultDone:      make(chan struct{}),
		commandDone:     make(chan struct{}),
		terminationDone: make(chan struct{}),
		outputStop:      make(chan struct{}),
		result: Result{
			PID:       command.Process.Pid,
			StartedAt: startedAt,
		},
	}

	process.readers.Add(2)
	go process.readOutput(stdoutRead, Stdout)
	go process.readOutput(stderrRead, Stderr)
	go process.deliverOutput()
	go process.waitForCompletion()
	go process.terminateWhenContextCancels(ctx)

	if len(invocation.InitialInput) > 0 {
		if err := process.WriteInput(invocation.InitialInput); err != nil {
			terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace)
			_ = process.Terminate(terminationContext, 0)
			cancel()
			return nil, fmt.Errorf("write initial input: %w", err)
		}
	}
	if invocation.CloseInputAfterInitial {
		if err := process.CloseInput(); err != nil {
			terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace)
			_ = process.Terminate(terminationContext, 0)
			cancel()
			return nil, fmt.Errorf("close initial input: %w", err)
		}
	}
	return process, nil
}

// WriteInput appends human input to the agent's standard input. Concurrent
// callers are serialized, preserving write order and avoiding interleaving.
func (process *Process) WriteInput(input []byte) error {
	if len(input) == 0 {
		return nil
	}

	process.stdinMutex.Lock()
	defer process.stdinMutex.Unlock()
	if process.stdin == nil {
		return errInputClosed
	}

	for len(input) > 0 {
		written, err := process.stdin.Write(input)
		if err != nil {
			return fmt.Errorf("write process input: %w", err)
		}
		input = input[written:]
	}
	return nil
}

// CloseInput sends EOF to the agent while allowing its remaining stdout and
// stderr output to drain. It is safe to call more than once.
func (process *Process) CloseInput() error {
	process.stdinMutex.Lock()
	defer process.stdinMutex.Unlock()
	if process.stdin == nil {
		return nil
	}
	err := process.stdin.Close()
	process.stdin = nil
	if err != nil {
		return fmt.Errorf("close process input: %w", err)
	}
	return nil
}

// Wait waits for process exit, pipe draining, and queued sink delivery. The
// underlying command is waited exactly once regardless of how many callers use
// Wait concurrently.
func (process *Process) Wait() Result {
	<-process.resultDone
	return process.result
}

// ProcessDetails returns the restart-safe identity recorded when this process
// was started. Both values remain stable for the Process lifetime.
func (process *Process) ProcessDetails() (int, string) {
	return process.PID, process.Identity
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
			return nil
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
		defer process.terminationMutex.Unlock()
		return process.terminationError
	case <-ctx.Done():
		return ctx.Err()
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
	waitError := process.command.Wait()
	close(process.commandDone)
	_ = process.CloseInput()

	if !process.terminationRequested() {
		process.recordContainmentError(process.containment.Close())
	}

	process.readers.Wait()
	close(process.events)
	<-process.deliveryDone
	process.cancelSink()
	if process.terminationRequested() {
		process.recordContainmentError(process.containment.Close())
	}

	process.terminationMutex.Lock()
	terminated := process.terminated
	terminationError := process.terminationError
	process.terminationMutex.Unlock()

	process.errorMutex.Lock()
	process.result.ExitCode = process.command.ProcessState.ExitCode()
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
	softTerminationError := process.containment.Terminate(false)

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

	if err := process.containment.Terminate(true); err != nil {
		process.recordTerminationError(errors.Join(softTerminationError, err))
		return
	}
	<-process.resultDone
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
	if process.terminationError == nil {
		process.terminationError = err
	}
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
