package opencode

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	DefaultExecutable       = "opencode"
	TestedVersion           = "1.18.30"
	defaultHealthRetryDelay = 50 * time.Millisecond
	defaultTerminationGrace = 5 * time.Second
	streamReadChunkSize     = 32 * 1024
)

var (
	errSessionClosed      = errors.New("opencode native session is closed")
	errTurnUnverified     = fmt.Errorf("%w: OpenCode terminal event semantics are unverified", harness.ErrNativeUnverified)
	errNilNativeProcess   = errors.New("opencode native process starter returned a nil process")
	versionPattern        = regexp.MustCompile(`\b([0-9]+\.[0-9]+\.[0-9]+)\b`)
	unsupportedOperations = "OpenCode native lifecycle behavior is unverified"
)

// CommandRunner is the injection boundary for deterministic executable probes.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, executable, args...).CombinedOutput()
}

// nativeProcess retains only the shared execution.Runner behavior this adapter
// needs. The API server's stdin is deliberately never used as a control path.
type nativeProcess interface {
	Wait() execution.Result
	Terminate(context.Context, time.Duration) error
	ProcessDetails() (int, string)
}

type processStarter func(context.Context, execution.Invocation, execution.Sink) (nativeProcess, error)

func runnerProcessStarter(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (nativeProcess, error) {
	process, err := execution.NewRunner().Start(ctx, invocation, sink)
	if err != nil {
		return nil, err
	}
	return process, nil
}

type api interface {
	Health(context.Context) error
	CreateSession(context.Context, CreateSessionRequest) (SessionInfo, error)
	Prompt(context.Context, string, PromptRequest) (PromptAdmission, error)
	Interrupt(context.Context, string) error
	OpenSessionEvents(context.Context, string, uint64) (io.ReadCloser, error)
}

type apiFactory func(Config) (api, error)

func newAPI(config Config) (api, error) { return NewClient(config) }

type portAllocator func() (int, error)

func allocateLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve OpenCode loopback port: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if port <= 0 {
		return 0, errors.New("reserve OpenCode loopback port: invalid port")
	}
	return port, nil
}

type credentialFactory func() (username, password string, err error)

func newCredentials() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate OpenCode server password: %w", err)
	}
	return "opencode", base64.RawURLEncoding.EncodeToString(bytes), nil
}

// Adapter launches an owned, local OpenCode serve process. It is intentionally
// not usable through capability admission yet: probe and all terminal behavior
// remain fail-closed until a real lifecycle capture verifies them.
type Adapter struct {
	executable      string
	runner          CommandRunner
	startProcess    processStarter
	newAPI          apiFactory
	allocatePort    portAllocator
	newCredentials  credentialFactory
	healthRetryWait time.Duration
	terminationWait time.Duration
}

// NewAdapter creates the private OpenCode transport adapter. The adapter must
// still be explicitly composed at the application root after lifecycle proof.
func NewAdapter(executables ...string) *Adapter {
	executable := DefaultExecutable
	if len(executables) > 0 && strings.TrimSpace(executables[0]) != "" {
		executable = executables[0]
	}
	return newAdapter(executable, nil, runnerProcessStarter, newAPI, allocateLoopbackPort, newCredentials)
}

// NewAdapterWithRunner supplies deterministic executable probing while keeping
// production process supervision unchanged.
func NewAdapterWithRunner(executable string, runner CommandRunner) *Adapter {
	if strings.TrimSpace(executable) == "" {
		executable = DefaultExecutable
	}
	return newAdapter(executable, runner, runnerProcessStarter, newAPI, allocateLoopbackPort, newCredentials)
}

func newAdapter(executable string, runner CommandRunner, start processStarter, apiFactory apiFactory, ports portAllocator, credentials credentialFactory) *Adapter {
	return &Adapter{
		executable:      executable,
		runner:          runner,
		startProcess:    start,
		newAPI:          apiFactory,
		allocatePort:    ports,
		newCredentials:  credentials,
		healthRetryWait: defaultHealthRetryDelay,
		terminationWait: defaultTerminationGrace,
	}
}

// Probe records version and serve-help transport evidence but always returns
// ErrNativeUnverified. Neither evidence proves native sessions or terminals.
func (adapter *Adapter) Probe(ctx context.Context) (harness.Capabilities, error) {
	if adapter == nil {
		return harness.Capabilities{}, errors.New("opencode adapter is nil")
	}
	if ctx == nil {
		return harness.Capabilities{}, errors.New("opencode probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return harness.Capabilities{}, err
	}
	capabilities := harness.UnsupportedCapabilities(harness.KindOpenCode, unsupportedOperations)
	runner := adapter.runner
	if runner == nil {
		runner = osCommandRunner{}
	}
	versionOutput, err := runner.Run(ctx, adapter.executable, "--version")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return harness.Capabilities{}, contextErr
		}
		capabilities.Unsupported[string(harness.CapabilityStart)] = "opencode version probe failed"
		return capabilities, fmt.Errorf("%w: opencode --version: %v", harness.ErrHarnessUnavailable, err)
	}
	version := parseVersion(string(versionOutput))
	capabilities.NativeVersion = version
	capabilities.VersionKnown = version != ""
	if version == "" {
		return capabilities, fmt.Errorf("%w: unable to parse OpenCode version from %q", harness.ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if version != TestedVersion {
		return capabilities, fmt.Errorf("%w: OpenCode %s is not in the tested version set", harness.ErrUnsupportedVersion, version)
	}
	helpOutput, err := runner.Run(ctx, adapter.executable, "serve", "--help")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return harness.Capabilities{}, contextErr
		}
		return capabilities, fmt.Errorf("%w: OpenCode serve help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	if !hasServeHelp(string(helpOutput)) {
		return capabilities, fmt.Errorf("%w: OpenCode serve help does not advertise required loopback options", harness.ErrNativeUnverified)
	}
	capabilities.TransportVerified = true
	return capabilities, harness.ErrNativeUnverified
}

func parseVersion(output string) string {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasServeHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "opencode serve") &&
		strings.Contains(value, "--hostname") &&
		strings.Contains(value, "--port") &&
		strings.Contains(value, "--pure")
}

// Start launches only the daemon-owned HTTP server. It records a process
// identity through execution.Runner before output delivery; Open creates the
// opaque native session and StartTurn admits exactly one prompt.
func (adapter *Adapter) Start(ctx context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	if adapter == nil {
		return nil, errors.New("opencode adapter is nil")
	}
	if ctx == nil {
		return nil, errors.New("opencode start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ProviderAccess != nil {
		return nil, unsupportedCapability(harness.CapabilityProviderAccess, "OpenCode provider broker bridge is not verified")
	}
	if request.Limits.MaxCostMicrousd != nil {
		return nil, unsupportedCapability(harness.CapabilityHardCostLimit, "OpenCode provider-enforced hard cost limit is not verified")
	}
	if request.Resume != nil {
		return nil, unsupportedCapability(harness.CapabilityResume, "OpenCode native resume is not verified")
	}
	if strings.TrimSpace(request.Workspace) == "" || !filepath.IsAbs(request.Workspace) {
		return nil, errors.New("opencode workspace must be an absolute path")
	}
	if sink == nil {
		return nil, errors.New("opencode event sink must not be nil")
	}
	if len(request.Invocation.InitialInput) != 0 || request.Invocation.CloseInputAfterInitial {
		return nil, errors.New("opencode native transport does not accept legacy initial input")
	}
	if adapter.startProcess == nil || adapter.newAPI == nil || adapter.allocatePort == nil || adapter.newCredentials == nil {
		return nil, errors.New("opencode adapter dependencies are incomplete")
	}
	port, err := adapter.allocatePort()
	if err != nil {
		return nil, err
	}
	username, password, err := adapter.newCredentials()
	if err != nil {
		return nil, err
	}
	client, err := adapter.newAPI(Config{
		BaseURL:  "http://127.0.0.1:" + strconv.Itoa(port),
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, fmt.Errorf("configure OpenCode local API: %w", err)
	}
	sessionContext, cancel := context.WithCancel(ctx)
	session := newNativeSession(sessionContext, cancel, sink, request.Workspace, client, adapter.healthRetryWait, adapter.terminationWait)
	invocation := execution.Invocation{
		Program:        adapter.executable,
		Args:           []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port), "--pure"},
		Dir:            request.Workspace,
		Env:            appendCredentialEnvironment(request.Invocation.Env, username, password),
		PersistProcess: request.PersistProcess,
	}
	process, err := adapter.startProcess(sessionContext, invocation, execution.SinkFunc(session.handleProcessOutput))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start OpenCode serve: %w", err)
	}
	if isNilNativeProcess(process) {
		cancel()
		return nil, errNilNativeProcess
	}
	session.mu.Lock()
	session.process = process
	session.mu.Unlock()
	go session.watchProcess(process)
	return session, nil
}

func unsupportedCapability(capability harness.Capability, reason string) error {
	return &harness.CapabilityError{Kind: harness.KindOpenCode, Capability: capability, Reason: reason}
}

func appendCredentialEnvironment(environment []string, username, password string) []string {
	result := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(key, "OPENCODE_SERVER_USERNAME") || strings.EqualFold(key, "OPENCODE_SERVER_PASSWORD")) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "OPENCODE_SERVER_USERNAME="+username, "OPENCODE_SERVER_PASSWORD="+password)
}

func isNilNativeProcess(process nativeProcess) bool {
	if process == nil {
		return true
	}
	value := reflectValue(process)
	return value
}

func reflectValue(process nativeProcess) bool {
	value := reflect.ValueOf(process)
	return value.Kind() == reflect.Ptr && value.IsNil()
}

type openAttempt struct {
	done   chan struct{}
	handle harness.NativeSessionHandle
	err    error
}

type interruptAttempt struct {
	done chan struct{}
	err  error
}

type closeAttempt struct {
	done chan struct{}
	err  error
}

// streamHandle makes cancellation from the process watcher and explicit Close
// converge on one underlying body close. io.ReadCloser itself has no general
// concurrent-Close guarantee.
type streamHandle struct {
	body io.ReadCloser
	once sync.Once
	err  error
}

func (stream *streamHandle) Read(data []byte) (int, error) {
	return stream.body.Read(data)
}

func (stream *streamHandle) Close() error {
	if stream == nil {
		return nil
	}
	stream.once.Do(func() { stream.err = stream.body.Close() })
	return stream.err
}

type nativeSession struct {
	context context.Context
	cancel  context.CancelFunc
	sink    harness.EventSink

	workspace       string
	client          api
	healthRetryWait time.Duration
	terminationWait time.Duration

	mu              sync.Mutex
	process         nativeProcess
	opened          bool
	closed          bool
	openAttempt     *openAttempt
	openErr         error
	handle          harness.NativeSessionHandle
	turnStarted     bool
	turnStarting    bool
	turnErr         error
	turnCancel      context.CancelFunc
	stream          *streamHandle
	streamDone      chan struct{}
	streamErr       error
	sinkErr         error
	eventCount      uint64
	lastSequence    uint64
	processResult   execution.Result
	processDone     chan struct{}
	processDoneOnce sync.Once

	interruptMu        sync.Mutex
	interruptAttempt   *interruptAttempt
	interruptSucceeded bool
	closeMu            sync.Mutex
	closeAttempt       *closeAttempt
	closeSucceeded     bool
}

func newNativeSession(ctx context.Context, cancel context.CancelFunc, sink harness.EventSink, workspace string, client api, healthRetryWait, terminationWait time.Duration) *nativeSession {
	return &nativeSession{
		context:         ctx,
		cancel:          cancel,
		sink:            sink,
		workspace:       workspace,
		client:          client,
		healthRetryWait: healthRetryWait,
		terminationWait: terminationWait,
		processDone:     make(chan struct{}),
	}
}

func (session *nativeSession) ProcessDetails() (int, string) {
	if session == nil {
		return 0, ""
	}
	session.mu.Lock()
	process := session.process
	session.mu.Unlock()
	if process == nil {
		return 0, ""
	}
	return process.ProcessDetails()
}

// Open establishes authenticated readiness and persists the server-returned
// native session identity before any turn can be admitted.
func (session *nativeSession) Open(ctx context.Context) (harness.NativeSessionHandle, error) {
	if session == nil {
		return harness.NativeSessionHandle{}, errors.New("opencode native session is nil")
	}
	if ctx == nil {
		return harness.NativeSessionHandle{}, errors.New("opencode open context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return harness.NativeSessionHandle{}, err
	}
	session.mu.Lock()
	if session.openErr != nil {
		err := session.openErr
		session.mu.Unlock()
		return harness.NativeSessionHandle{}, err
	}
	if session.closed {
		session.mu.Unlock()
		return harness.NativeSessionHandle{}, errSessionClosed
	}
	if session.opened {
		handle := session.handle
		session.mu.Unlock()
		return handle, nil
	}
	if attempt := session.openAttempt; attempt != nil {
		session.mu.Unlock()
		select {
		case <-attempt.done:
			return attempt.handle, attempt.err
		case <-ctx.Done():
			return harness.NativeSessionHandle{}, ctx.Err()
		}
	}
	attempt := &openAttempt{done: make(chan struct{})}
	session.openAttempt = attempt
	session.mu.Unlock()

	handle, err := session.openOnce(ctx)
	if err != nil {
		cleanupContext, cancel := context.WithTimeout(context.Background(), session.terminationGrace()*2)
		cleanupErr := session.Close(cleanupContext)
		cancel()
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}
	session.mu.Lock()
	if err == nil && session.closed {
		err = errSessionClosed
		handle = harness.NativeSessionHandle{}
	}
	attempt.handle = handle
	attempt.err = err
	if err == nil {
		session.opened = true
		session.handle = handle
	} else {
		session.openErr = err
	}
	if session.openAttempt == attempt {
		session.openAttempt = nil
	}
	close(attempt.done)
	session.mu.Unlock()
	return handle, err
}

func (session *nativeSession) openOnce(ctx context.Context) (harness.NativeSessionHandle, error) {
	if err := session.waitForHealth(ctx); err != nil {
		return harness.NativeSessionHandle{}, err
	}
	info, err := session.client.CreateSession(ctx, CreateSessionRequest{ID: randomID("ses_"), Location: SessionLocation{Directory: session.workspace}})
	if err != nil {
		return harness.NativeSessionHandle{}, fmt.Errorf("create OpenCode session: %w", err)
	}
	session.mu.Lock()
	closed := session.closed
	session.mu.Unlock()
	if closed {
		return harness.NativeSessionHandle{}, errSessionClosed
	}
	return harness.NativeSessionHandle{ID: info.ID}, nil
}

func (session *nativeSession) waitForHealth(ctx context.Context) error {
	for {
		err := session.client.Health(ctx)
		if err == nil {
			return nil
		}
		if !retryableHealthError(err) {
			return fmt.Errorf("check OpenCode health: %w", err)
		}
		wait := session.healthRetryWait
		if wait <= 0 {
			wait = defaultHealthRetryDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("check OpenCode health: %w", ctx.Err())
		case <-session.context.Done():
			timer.Stop()
			return errSessionClosed
		case <-timer.C:
		}
	}
}

func retryableHealthError(err error) bool {
	return !errors.Is(err, ErrInvalidConfig) && !errors.Is(err, ErrUnexpectedStatus) && !errors.Is(err, ErrMalformedResponse) && !errors.Is(err, ErrInvalidHealth)
}

// StartTurn opens the per-session durable stream before it admits one prompt.
// Admission is recorded as a native frame only; no output becomes a task result.
func (session *nativeSession) StartTurn(ctx context.Context, request harness.TurnRequest) (err error) {
	if session == nil {
		return errors.New("opencode native session is nil")
	}
	if ctx == nil {
		return errors.New("opencode start turn context must not be nil")
	}
	if strings.TrimSpace(request.Goal) == "" || !validJSONObject(request.Context) {
		return errors.New("opencode turn requires a non-empty goal and JSON object context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return errSessionClosed
	}
	if !session.opened || !isID(session.handle.ID, "ses_") {
		session.mu.Unlock()
		return errors.New("opencode native session has not opened")
	}
	if session.turnStarted || session.turnStarting {
		session.mu.Unlock()
		return errors.New("opencode native session already owns a turn")
	}
	if session.turnErr != nil {
		err := session.turnErr
		session.mu.Unlock()
		return err
	}
	if session.sinkErr != nil {
		err := session.sinkErr
		session.mu.Unlock()
		return err
	}
	turnContext, turnCancel := context.WithCancel(ctx)
	session.turnStarting = true
	session.turnCancel = turnCancel
	handle := session.handle
	session.mu.Unlock()

	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		turnCancel()
		session.mu.Lock()
		if session.turnErr == nil {
			if err == nil {
				err = errTurnUnverified
			}
			session.turnErr = err
		}
		session.turnCancel = nil
		session.turnStarting = false
		session.mu.Unlock()
		cleanupContext, cancel := context.WithTimeout(context.Background(), session.terminationGrace()*2)
		_ = session.Close(cleanupContext)
		cancel()
	}()
	if err := session.emit(harness.Event{Kind: harness.EventSessionStarted}); err != nil {
		return err
	}
	promptID := randomID("msg_")
	streamBody, err := session.client.OpenSessionEvents(turnContext, handle.ID, 0)
	if err != nil {
		return fmt.Errorf("open OpenCode session events: %w", err)
	}
	stream := &streamHandle{body: streamBody}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		stream.Close()
		return errSessionClosed
	}
	session.stream = stream
	session.streamDone = make(chan struct{})
	streamDone := session.streamDone
	session.mu.Unlock()
	go session.watchSessionEvents(stream, streamDone, handle.ID, promptID)

	prompt := PromptRequest{ID: promptID, Text: buildPrompt(request.Goal, request.Context), Delivery: "steer", Resume: false}
	admission, err := session.client.Prompt(turnContext, handle.ID, prompt)
	if err != nil {
		stream.Close()
		return fmt.Errorf("admit OpenCode prompt: %w", err)
	}
	if admission.ID != promptID || admission.SessionID != handle.ID {
		stream.Close()
		return fmt.Errorf("admit OpenCode prompt: %w", ErrIdentityMismatch)
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		stream.Close()
		return errSessionClosed
	}
	if session.sinkErr != nil {
		err := session.sinkErr
		session.mu.Unlock()
		stream.Close()
		return err
	}
	session.turnStarting = false
	session.turnStarted = true
	succeeded = true
	session.mu.Unlock()
	if err := session.emit(harness.Event{Kind: harness.EventNativeFrame, Code: "opencode_prompt_admitted", Message: "OpenCode prompt admission observed; terminal outcome remains unverified"}); err != nil {
		return err
	}
	return nil
}

// WaitTurn never treats an HTTP admission, event stream, or child exit as an
// OpenCode turn terminal. A future mapping must explicitly replace this gate.
func (session *nativeSession) WaitTurn(ctx context.Context) error {
	if session == nil {
		return errors.New("opencode native session is nil")
	}
	if ctx == nil {
		return errors.New("opencode wait-turn context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errTurnUnverified
}

// Wait returns a supervised process outcome but never ResultSucceeded without
// a separately verified, exact terminal event and structured task result.
func (session *nativeSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	if session == nil {
		return harness.TaskResult{}, errors.New("opencode native session is nil")
	}
	if ctx == nil {
		return harness.TaskResult{}, errors.New("opencode wait context must not be nil")
	}
	select {
	case <-session.processDone:
		session.mu.Lock()
		result := harness.TaskResult{
			Kind:         harness.ResultUnknown,
			Summary:      "OpenCode process ended without a verified native terminal result",
			Process:      session.processResult,
			Usage:        harness.Usage{State: harness.UsageUnknown},
			EventCount:   session.eventCount,
			LastSequence: session.lastSequence,
		}
		session.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return harness.TaskResult{Kind: harness.ResultCancelled, Summary: "OpenCode wait cancelled"}, ctx.Err()
	}
}

// Control exposes only cancellation delivery. It is deliberately independent
// from completion: accepted interrupt remains non-terminal native evidence.
func (session *nativeSession) Control(ctx context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	if session == nil {
		return harness.ControlReceipt{}, errors.New("opencode native session is nil")
	}
	if ctx == nil {
		return harness.ControlReceipt{}, errors.New("opencode control context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return harness.ControlReceipt{}, err
	}
	if request.Kind != harness.ControlCancel {
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlUnsupported, Capability: capabilityForControl(request.Kind), Message: unsupportedOperations, AppliedAt: time.Now().UTC()}, nil
	}
	session.mu.Lock()
	if session.closed || !session.turnStarted || !isID(session.handle.ID, "ses_") {
		session.mu.Unlock()
		return harness.ControlReceipt{}, errSessionClosed
	}
	handle := session.handle.ID
	session.mu.Unlock()
	err := session.interrupt(ctx, handle)
	if err != nil {
		return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlFailed, Capability: harness.CapabilityCancel, Message: err.Error(), AppliedAt: time.Now().UTC()}, err
	}
	return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlApplied, Capability: harness.CapabilityCancel, Message: "OpenCode interrupt accepted; terminal outcome remains unverified", AppliedAt: time.Now().UTC()}, nil
}

func (session *nativeSession) interrupt(ctx context.Context, handle string) error {
	session.interruptMu.Lock()
	if session.interruptSucceeded {
		session.interruptMu.Unlock()
		return nil
	}
	if attempt := session.interruptAttempt; attempt != nil {
		session.interruptMu.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &interruptAttempt{done: make(chan struct{})}
	session.interruptAttempt = attempt
	session.interruptMu.Unlock()
	err := session.client.Interrupt(ctx, handle)
	session.interruptMu.Lock()
	attempt.err = err
	if err == nil {
		session.interruptSucceeded = true
	}
	if session.interruptAttempt == attempt {
		session.interruptAttempt = nil
	}
	close(attempt.done)
	session.interruptMu.Unlock()
	return err
}

// Close serializes native interruption and process-tree cleanup. Failed close
// attempts remain retryable; successful close is idempotent.
func (session *nativeSession) Close(ctx context.Context) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("opencode close context must not be nil")
	}
	session.closeMu.Lock()
	if session.closeSucceeded {
		session.closeMu.Unlock()
		return nil
	}
	if attempt := session.closeAttempt; attempt != nil {
		session.closeMu.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &closeAttempt{done: make(chan struct{})}
	session.closeAttempt = attempt
	session.closeMu.Unlock()
	err := session.closeOnce(ctx)
	session.closeMu.Lock()
	attempt.err = err
	if err == nil {
		session.closeSucceeded = true
	}
	if session.closeAttempt == attempt {
		session.closeAttempt = nil
	}
	close(attempt.done)
	session.closeMu.Unlock()
	return err
}

func (session *nativeSession) closeOnce(ctx context.Context) error {
	session.mu.Lock()
	session.closed = true
	handle := session.handle.ID
	turnStarted := session.turnStarted || session.turnStarting
	turnCancel := session.turnCancel
	stream := session.stream
	streamDone := session.streamDone
	process := session.process
	session.mu.Unlock()
	if turnCancel != nil {
		turnCancel()
	}
	var interruptErr error
	if turnStarted && isID(handle, "ses_") {
		interruptErr = session.interrupt(ctx, handle)
	}
	if stream != nil {
		_ = stream.Close()
	}
	session.cancel()
	var terminateErr error
	if process != nil {
		terminateErr = process.Terminate(ctx, session.terminationGrace())
	}
	var streamErr error
	if streamDone != nil {
		select {
		case <-streamDone:
		case <-ctx.Done():
			streamErr = ctx.Err()
		}
	}
	var processErr error
	if process != nil {
		select {
		case <-session.processDone:
		case <-ctx.Done():
			processErr = ctx.Err()
		}
	}
	if terminateErr == nil && streamErr == nil && processErr == nil {
		return nil
	}
	return errors.Join(interruptErr, terminateErr, streamErr, processErr)
}

func (session *nativeSession) terminationGrace() time.Duration {
	if session.terminationWait <= 0 {
		return defaultTerminationGrace
	}
	return session.terminationWait
}

func (session *nativeSession) watchProcess(process nativeProcess) {
	result := process.Wait()
	session.mu.Lock()
	session.processResult = result
	stream := session.stream
	turnCancel := session.turnCancel
	session.mu.Unlock()
	if turnCancel != nil {
		turnCancel()
	}
	if stream != nil {
		_ = stream.Close()
	}
	session.processDoneOnce.Do(func() { close(session.processDone) })
}

func (session *nativeSession) watchSessionEvents(stream *streamHandle, done chan struct{}, sessionID, messageID string) {
	defer close(done)
	decoder := NewDecoder(defaultMaxFrameBytes)
	validator, err := NewSessionEventValidator(sessionID, 0)
	if err != nil {
		session.recordStreamError(err)
		return
	}
	buffer := make([]byte, streamReadChunkSize)
	for {
		count, readErr := stream.Read(buffer)
		if count > 0 {
			frames, feedErr := decoder.Feed(buffer[:count])
			if feedErr != nil {
				session.recordStreamError(feedErr)
				return
			}
			if err := session.observeSessionFrames(validator, frames, messageID); err != nil {
				session.recordStreamError(err)
				return
			}
		}
		if readErr == nil {
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			session.recordStreamError(readErr)
			return
		}
		frames, err := decoder.Close()
		if err != nil {
			session.recordStreamError(err)
			return
		}
		if err := session.observeSessionFrames(validator, frames, messageID); err != nil {
			session.recordStreamError(err)
			return
		}
		session.recordStreamError(errStreamEndedUnknown)
		return
	}
}

func (session *nativeSession) observeSessionFrames(validator *SessionEventValidator, frames []Frame, messageID string) error {
	for _, frame := range frames {
		event, err := validator.Observe(frame)
		if err != nil {
			return err
		}
		if event.MessageID != messageID {
			return fmt.Errorf("%w: expected message %q", ErrIdentityMismatch, messageID)
		}
		if err := session.emit(harness.Event{Kind: harness.EventNativeFrame, Sequence: frame.Sequence, Code: "opencode_session_event", Message: "OpenCode durable session event observed"}); err != nil {
			return err
		}
	}
	return nil
}

func (session *nativeSession) handleProcessOutput(ctx context.Context, event execution.Event) error {
	return session.emit(harness.Event{Kind: harness.EventDiagnostic, Stream: string(event.Stream), Sequence: event.Sequence, At: event.At, Diagnostic: true, Code: "opencode_process_output", Message: "OpenCode serve process emitted output; bytes withheld from journal"})
}

func (session *nativeSession) recordStreamError(err error) {
	if err == nil {
		return
	}
	session.mu.Lock()
	if session.streamErr == nil {
		session.streamErr = err
	}
	session.mu.Unlock()
	_ = session.emit(harness.Event{Kind: harness.EventDiagnostic, Diagnostic: true, Code: "opencode_stream_error", Message: err.Error()})
}

func (session *nativeSession) emit(event harness.Event) error {
	if session == nil || session.sink == nil {
		return errors.New("opencode event sink is nil")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if err := session.sink.Handle(context.Background(), event); err != nil {
		session.mu.Lock()
		if session.sinkErr == nil {
			session.sinkErr = err
		}
		session.mu.Unlock()
		return err
	}
	session.mu.Lock()
	session.eventCount++
	if event.Sequence > session.lastSequence {
		session.lastSequence = event.Sequence
	}
	session.mu.Unlock()
	return nil
}

func validJSONObject(raw json.RawMessage) bool {
	_, err := strictObject(raw)
	return err == nil
}

func buildPrompt(goal string, data json.RawMessage) string {
	return "Goal:\n" + goal + "\n\nContext JSON:\n" + string(data)
}

func randomID(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// This fallback preserves opaque uniqueness enough to fail closed at the
		// server boundary instead of ever fabricating a successful session.
		return prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(bytes)
}

func capabilityForControl(kind harness.ControlKind) harness.Capability {
	switch kind {
	case harness.ControlPause:
		return harness.CapabilityPause
	case harness.ControlResume:
		return harness.CapabilityResume
	case harness.ControlGuidance:
		return harness.CapabilityGuidance
	case harness.ControlApprovalResponse:
		return harness.CapabilityApprovalResponse
	default:
		return harness.Capability(kind)
	}
}
