package harness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

const genericTerminationGrace = 250 * time.Millisecond

// ProcessHandle is the narrow part of execution.Process used by the generic
// adapter. Keeping the dependency at this seam makes cancellation testable
// without introducing another process supervisor.
type ProcessHandle interface {
	Wait() execution.Result
	Terminate(context.Context, time.Duration) error
}

// ProcessRunner starts one direct process invocation and routes output to the
// supplied execution sink.
type ProcessRunner interface {
	Start(context.Context, execution.Invocation, execution.Sink) (ProcessHandle, error)
}

type executionRunner struct {
	runner execution.Runner
}

func (runner executionRunner) Start(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (ProcessHandle, error) {
	process, err := runner.runner.Start(ctx, invocation, sink)
	if process == nil {
		return nil, err
	}
	return process, err
}

// GenericAdapter wraps the existing direct process runner. It deliberately
// treats child output as opaque bytes; it does not interpret the legacy
// symmetry-fake-agent JSON as a native protocol.
type GenericAdapter struct {
	runner ProcessRunner
}

// NewGenericAdapter creates a generic legacy adapter. A nil runner uses the
// daemon's existing process runner.
func NewGenericAdapter(runners ...ProcessRunner) *GenericAdapter {
	var runner ProcessRunner = executionRunner{runner: execution.NewRunner()}
	if len(runners) > 0 && runners[0] != nil {
		runner = runners[0]
	}
	return &GenericAdapter{runner: runner}
}

// NewGenericAdapterWithRunner is the explicit constructor for tests or a
// daemon-owned narrow process runner.
func NewGenericAdapterWithRunner(runner ProcessRunner) *GenericAdapter {
	return NewGenericAdapter(runner)
}

// NewGenericAdapterWithExecutionRunner wraps the concrete runner already owned
// by the daemon without requiring callers to implement the narrow interface.
func NewGenericAdapterWithExecutionRunner(runner execution.Runner) *GenericAdapter {
	return &GenericAdapter{runner: executionRunner{runner: runner}}
}

// Probe reports only the generic process lifecycle that is implemented here.
// Native resume, guidance, approval and usage semantics remain explicit
// unsupported values.
func (adapter *GenericAdapter) Probe(ctx context.Context) (Capabilities, error) {
	if ctx == nil {
		return Capabilities{}, errors.New("harness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	capabilities := Capabilities{
		Kind:                  KindGeneric,
		NativeVersion:         "generic-v1",
		ImplementationVersion: "generic-v1",
		ProtocolVersion:       1,
		VersionKnown:          true,
		TransportVerified:     true,
		Verified:              true,
		Start:                 true,
		Events:                true,
		Cancel:                true,
		Guidance:              GuidanceUnsupported,
		Pause:                 PauseUnsupported,
		Usage:                 UsageUnknown,
		Unsupported: map[string]string{
			string(CapabilityResume):           "generic process runner has no retained native session",
			string(CapabilityGuidance):         "generic process runner does not verify guidance application",
			string(CapabilityPause):            "process interruption is cancellation, not safe pause",
			string(CapabilityApprovalResponse): "generic process runner has no native approval bridge",
			string(CapabilityUsage):            "generic process runner has no authoritative usage counters",
			string(CapabilityHardCostLimit):    "generic process runner has no provider-enforced hard cost limit",
		},
	}
	if err := capabilities.Validate(); err != nil {
		return Capabilities{}, fmt.Errorf("generic capability contract: %w", err)
	}
	return capabilities, nil
}

// Start launches the configured invocation and forwards opaque process output
// as EventOutput records. A nil sink is accepted as a discard sink.
func (adapter *GenericAdapter) Start(ctx context.Context, request StartRequest, sink EventSink) (Session, error) {
	if adapter == nil || adapter.runner == nil {
		return nil, errors.New("generic adapter runner is nil")
	}
	if ctx == nil {
		return nil, errors.New("harness start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ProviderAccess != nil {
		return nil, &CapabilityError{
			Kind:       KindGeneric,
			Capability: CapabilityProviderAccess,
			Reason:     "generic process runner cannot expose an ephemeral provider broker capability",
		}
	}
	if request.Limits.MaxCostMicrousd != nil {
		return nil, &CapabilityError{
			Kind:       KindGeneric,
			Capability: CapabilityHardCostLimit,
			Reason:     "generic process runner has no provider-enforced hard cost limit",
		}
	}
	if request.Resume != nil {
		return nil, &CapabilityError{
			Kind:       KindGeneric,
			Capability: CapabilityResume,
			Reason:     "generic process runner cannot resume a native session",
		}
	}
	if sink == nil {
		sink = EventSinkFunc(func(context.Context, Event) error { return nil })
	}
	invocation := request.Invocation
	invocation.PersistProcess = request.PersistProcess
	process, err := adapter.runner.Start(ctx, invocation, execution.SinkFunc(func(eventContext context.Context, event execution.Event) error {
		return sink.Handle(eventContext, Event{
			Kind:     EventOutput,
			Stream:   string(event.Stream),
			Sequence: event.Sequence,
			At:       event.At,
			Data:     append([]byte(nil), event.Data...),
		})
	}))
	if isNilProcessHandle(process) {
		if err != nil {
			return nil, fmt.Errorf("start generic harness: %w", err)
		}
		return nil, errors.New("generic process runner returned a nil process")
	}
	session := &genericSession{process: process, resultDone: make(chan struct{})}
	go session.await()
	if err != nil {
		return session, fmt.Errorf("start generic harness: %w", err)
	}
	return session, nil
}

func isNilProcessHandle(process ProcessHandle) bool {
	if process == nil {
		return true
	}
	value := reflect.ValueOf(process)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type genericSession struct {
	process ProcessHandle

	closeMutex     sync.Mutex
	closeAttempt   *genericCloseAttempt
	closeSucceeded bool

	resultDone chan struct{}
	result     TaskResult
}

type genericCloseAttempt struct {
	done chan struct{}
	err  error
}

func (session *genericSession) await() {
	processResult := session.process.Wait()
	result := TaskResult{
		Process: processResult,
		Usage: Usage{
			State: UsageUnknown,
		},
	}
	switch {
	case processResult.Terminated:
		result.Kind = ResultCancelled
		result.Summary = "generic process terminated"
	case processResult.Success():
		result.Kind = ResultSucceeded
		result.Summary = "generic process completed"
	case processResult.WaitError != nil || processResult.SinkError != nil ||
		processResult.OutputError != nil || processResult.TerminationError != nil ||
		processResult.ContainmentError != nil:
		result.Kind = ResultFailed
		result.Summary = "generic process failed"
	default:
		result.Kind = ResultUnknown
		result.Summary = "generic process ended without a verified result"
	}
	session.result = result
	close(session.resultDone)
}

func (session *genericSession) Control(ctx context.Context, request ControlRequest) (ControlReceipt, error) {
	if ctx == nil {
		return ControlReceipt{}, errors.New("control context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return ControlReceipt{}, err
	}
	if request.Kind != ControlCancel {
		capability := controlCapability(request.Kind)
		return ControlReceipt{}, &CapabilityError{
			Kind:       KindGeneric,
			Capability: capability,
			Reason:     "generic process runner does not implement this native control",
		}
	}
	if err := session.Close(ctx); err != nil {
		return ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: ControlFailed, Capability: CapabilityCancel, Message: err.Error()}, err
	}
	return ControlReceipt{
		CommandID:  request.CommandID,
		Kind:       request.Kind,
		Outcome:    ControlApplied,
		Capability: CapabilityCancel,
		AppliedAt:  time.Now().UTC(),
	}, nil
}

func (session *genericSession) Wait(ctx context.Context) (TaskResult, error) {
	if ctx == nil {
		return TaskResult{}, errors.New("wait context must not be nil")
	}
	done := session.done()
	select {
	case <-done:
		return session.result, nil
	case <-ctx.Done():
		closeContext, cancel := context.WithTimeout(context.Background(), genericTerminationGrace+time.Second)
		closeErr := session.Close(closeContext)
		cancel()
		if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
			return TaskResult{}, errors.Join(ctx.Err(), closeErr)
		}
		return TaskResult{Kind: ResultCancelled, Summary: "generic wait cancelled"}, ctx.Err()
	}
}

func (session *genericSession) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("close context must not be nil")
	}
	session.closeMutex.Lock()
	if session.closeSucceeded {
		session.closeMutex.Unlock()
		return nil
	}
	if attempt := session.closeAttempt; attempt != nil {
		session.closeMutex.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &genericCloseAttempt{done: make(chan struct{})}
	session.closeAttempt = attempt
	session.closeMutex.Unlock()

	err := session.process.Terminate(ctx, genericTerminationGrace)
	session.closeMutex.Lock()
	attempt.err = err
	if err == nil {
		session.closeSucceeded = true
	}
	if session.closeAttempt == attempt {
		session.closeAttempt = nil
	}
	close(attempt.done)
	session.closeMutex.Unlock()
	return err
}

func (session *genericSession) done() <-chan struct{} {
	return session.resultDone
}

func controlCapability(kind ControlKind) Capability {
	switch kind {
	case ControlPause:
		return CapabilityPause
	case ControlResume:
		return CapabilityResume
	case ControlGuidance:
		return CapabilityGuidance
	case ControlApprovalResponse:
		return CapabilityApprovalResponse
	default:
		return Capability(kind)
	}
}
