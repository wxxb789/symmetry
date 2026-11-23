package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

// DefaultProcessPID is the live pid a Process double reports before termination.
const DefaultProcessPID = 42

// Process is a configurable Process double. A nil hook answers with the
// permissive behaviour most app fakes use: writes and terminations succeed, a
// terminated process has no live pid, and Wait returns Result.
type Process struct {
	Result                execution.Result
	KeepPIDAfterTerminate bool
	OnWriteInput          func([]byte) error
	OnTerminate           func(context.Context, time.Duration) error
	OnWait                func() execution.Result
	OnProcessDetails      func() (int, string)

	mu           sync.Mutex
	inputs       [][]byte
	terminations int
	terminated   bool
}

func (process *Process) WriteInput(input []byte) error {
	process.mu.Lock()
	process.inputs = append(process.inputs, append([]byte(nil), input...))
	hook := process.OnWriteInput
	process.mu.Unlock()
	if hook != nil {
		return hook(input)
	}
	return nil
}

func (process *Process) Terminate(ctx context.Context, grace time.Duration) error {
	process.mu.Lock()
	process.terminations++
	hook := process.OnTerminate
	process.mu.Unlock()
	if hook != nil {
		return hook(ctx, grace)
	}
	process.mu.Lock()
	process.terminated = true
	process.mu.Unlock()
	return nil
}

func (process *Process) Wait() execution.Result {
	hook := process.OnWait
	if hook != nil {
		return hook()
	}
	return process.Result
}

func (process *Process) ProcessDetails() (int, string) {
	hook := process.OnProcessDetails
	if hook != nil {
		return hook()
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.terminated && !process.KeepPIDAfterTerminate {
		return 0, ""
	}
	return DefaultProcessPID, "test:42"
}

func (process *Process) Inputs() [][]byte {
	process.mu.Lock()
	defer process.mu.Unlock()
	inputs := make([][]byte, 0, len(process.inputs))
	for _, input := range process.inputs {
		inputs = append(inputs, append([]byte(nil), input...))
	}
	return inputs
}

func (process *Process) Terminations() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.terminations
}

func (process *Process) Terminated() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.terminated
}
