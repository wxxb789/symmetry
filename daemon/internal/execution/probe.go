package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrOutputLimitExceeded reports that a bounded command emitted more output
// than the caller allowed. No partial output is returned with this error.
var ErrOutputLimitExceeded = errors.New("bounded command output exceeds limit")

// RunBoundedCommand runs one direct invocation, collects stdout and stderr in
// their observed delivery order, and returns only when the complete process
// tree has stopped and both pipes have been drained. The invocation is always
// given EOF on stdin. This seam is intentionally ephemeral: it does not
// install or invoke durable process/journal callbacks.
func RunBoundedCommand(ctx context.Context, invocation Invocation, maxOutputBytes int) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("bounded command context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bounded command context already cancelled: %w", err)
	}
	if maxOutputBytes <= 0 {
		return nil, errors.New("bounded command output limit must be positive")
	}

	// A bounded probe has no durable owner. Keep the direct invocation and its
	// process-tree behavior, but prevent inherited persistence hooks or lease
	// state from turning this ephemeral check into a journal write/watchdog.
	invocation.CloseInputAfterInitial = true
	invocation.PersistProcess = nil
	invocation.PersistProcessAuthority = nil
	invocation.PersistContainmentStopReceipt = nil
	invocation.InitialLeaseDeadline = 0
	invocation.InitialLeaseDeadlineAt = time.Time{}
	invocation.InitialLeaseSequence = 0

	collector := newBoundedOutputCollector(maxOutputBytes)
	process, startErr := NewRunner().Start(ctx, invocation, collector)
	if process == nil {
		return nil, startErr
	}

	if startErr != nil {
		terminationErr := terminateBoundedProcess(process)
		result := process.Wait()
		return nil, errors.Join(startErr, terminationErr, boundedProcessErrors(result))
	}

	// Start can race with a very short-lived child. Install the limit handler
	// after Start, then recheck the collector so an early overflow is not lost.
	collector.setLimitHandler(func() {
		go func() { _ = terminateBoundedProcess(process) }()
	})

	resultDone := make(chan Result, 1)
	go func() { resultDone <- process.Wait() }()

	var (
		result         Result
		terminationErr error
	)
	select {
	case result = <-resultDone:
	case <-ctx.Done():
		terminationErr = terminateBoundedProcess(process)
		result = <-resultDone
	}

	// The process can win the select at the same instant that its caller's
	// deadline fires. The caller's context remains authoritative after Wait.
	contextErr := ctx.Err()
	if contextErr != nil {
		return nil, errors.Join(contextErr, terminationErr, boundedProcessErrors(result))
	}
	if collector.exceeded() {
		return nil, errors.Join(ErrOutputLimitExceeded, terminationErr, boundedProcessErrors(result))
	}
	if processErr := boundedProcessErrors(result); processErr != nil {
		return nil, errors.Join(terminationErr, processErr)
	}
	return collector.output(), nil
}

func terminateBoundedProcess(process *Process) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace+time.Second)
	defer cancel()
	if err := process.Terminate(cleanupContext, 0); err != nil {
		return fmt.Errorf("terminate bounded command: %w", err)
	}
	return nil
}

type boundedOutputCollector struct {
	mutex        sync.Mutex
	data         bytes.Buffer
	maxBytes     int
	limitReached bool
	limitHandler func()
	limitOnce    sync.Once
}

func newBoundedOutputCollector(maxBytes int) *boundedOutputCollector {
	return &boundedOutputCollector{maxBytes: maxBytes}
}

func (collector *boundedOutputCollector) Handle(_ context.Context, event Event) error {
	collector.mutex.Lock()
	if collector.limitReached {
		collector.mutex.Unlock()
		return nil
	}
	if len(event.Data) > collector.maxBytes-collector.data.Len() {
		collector.limitReached = true
		collector.mutex.Unlock()
		collector.notifyLimit()
		return fmt.Errorf("%w: output exceeded %d bytes", ErrOutputLimitExceeded, collector.maxBytes)
	}
	_, _ = collector.data.Write(event.Data)
	collector.mutex.Unlock()
	return nil
}

func (collector *boundedOutputCollector) setLimitHandler(handler func()) {
	collector.mutex.Lock()
	collector.limitHandler = handler
	reached := collector.limitReached
	collector.mutex.Unlock()
	if reached {
		collector.notifyLimit()
	}
}

func (collector *boundedOutputCollector) notifyLimit() {
	collector.mutex.Lock()
	handler := collector.limitHandler
	collector.mutex.Unlock()
	if handler != nil {
		collector.limitOnce.Do(handler)
	}
}

func (collector *boundedOutputCollector) exceeded() bool {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return collector.limitReached
}

func (collector *boundedOutputCollector) output() []byte {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return append([]byte(nil), collector.data.Bytes()...)
}

func boundedProcessErrors(result Result) error {
	var errorsFound []error
	if result.WaitError != nil {
		errorsFound = append(errorsFound, fmt.Errorf("wait process: %w", result.WaitError))
	}
	if result.SinkError != nil {
		errorsFound = append(errorsFound, fmt.Errorf("collect process output: %w", result.SinkError))
	}
	if result.OutputError != nil {
		errorsFound = append(errorsFound, fmt.Errorf("read process output: %w", result.OutputError))
	}
	if result.TerminationError != nil {
		errorsFound = append(errorsFound, fmt.Errorf("terminate process: %w", result.TerminationError))
	}
	if result.ContainmentError != nil {
		errorsFound = append(errorsFound, fmt.Errorf("close process containment: %w", result.ContainmentError))
	}
	return errors.Join(errorsFound...)
}
