//go:build linux

package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func defaultProcessLauncher(
	ctx context.Context,
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error) {
	if ctx == nil {
		return nil, errors.New("native process launcher context is nil")
	}
	if command == nil {
		return nil, errors.New("native process command is missing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if invocation.linuxV2Requested() {
		return launchLinuxV2Process(ctx, command, invocation, stdinRead, stdoutWrite, stderrWrite)
	}
	writes := invocationWrites(invocation)
	capabilities := writes.Capabilities()
	if capabilities.SupervisorHandoffRequested() {
		return launchLinuxSupervisor(command, invocation, writes)
	}
	return launchLocalLinuxProcess(command)
}

// launchLinuxSupervisor is the durable Linux dispatch; the in-process test capability
// swaps in the local route a generated test main can dispatch.
var launchLinuxSupervisor = launchLinuxSupervisorProcess

func launchLinuxSupervisorProcess(command *exec.Cmd, invocation Invocation, writes RecorderFuncs) (*startedProcess, error) {
	if !writes.Capabilities().SupervisorHandoffComplete() {
		return nil, errors.New("durable Linux supervisor handoff callbacks are incomplete")
	}
	supervisor, launchErr := platform.StartLinuxSupervisor(linuxSupervisorStartSpec(command, invocation, writes))
	if supervisor == nil {
		return nil, launchErr
	}
	handoff := supervisor.Handoff()
	started := &startedProcess{
		pid:         supervisor.PID(),
		identity:    supervisor.Identity(),
		containment: supervisor,
		wait:        supervisor.Wait,
		kill:        func() error { return supervisor.Terminate(true) },
		close:       supervisor.Close,
		resume:      supervisor.Resume,
	}
	started.commitSupervisorHandoff = func(value authority.SupervisorHandoff, startedAt time.Time) error {
		if writes.CommitSupervisorHandoff != nil {
			if err := writes.CommitSupervisorHandoff(value, startedAt); err != nil {
				return err
			}
		}
		return supervisor.Commit()
	}
	if handoff.Validate() == nil {
		started.containmentHandoff = &handoff
	}
	started.containmentHandoffPrepareUnknown = supervisor.PrepareUnknown()
	started.containmentHandoffBindUnknown = supervisor.BindUnknown()
	if launchErr != nil {
		return started, fmt.Errorf("start durable Linux process tree: %w", launchErr)
	}
	return started, nil
}

func launchLocalLinuxProcess(command *exec.Cmd) (*startedProcess, error) {
	if err := command.Start(); err != nil {
		return nil, err
	}
	containment, identity, attachErr := platform.AttachProcess(command.Process)
	started := &startedProcess{
		command:     command,
		pid:         command.Process.Pid,
		identity:    identity,
		containment: containment,
		wait: func() (int, error) {
			waitErr := command.Wait()
			code := -1
			if command.ProcessState != nil {
				code = command.ProcessState.ExitCode()
			}
			return code, waitErr
		},
		kill:   func() error { return command.Process.Kill() },
		close:  func() error { return containment.Close() },
		resume: nil,
	}
	if attachErr != nil {
		return started, fmt.Errorf("contain Linux process tree: %w", attachErr)
	}
	return started, nil
}

func installInProcessSupervisorRouteForTesting() func() {
	previous := launchLinuxSupervisor
	launchLinuxSupervisor = func(command *exec.Cmd, _ Invocation, _ RecorderFuncs) (*startedProcess, error) {
		return launchLocalLinuxProcess(command)
	}
	return func() { launchLinuxSupervisor = previous }
}

func linuxSupervisorOwnerContext(invocation Invocation) string {
	// The app supplies a run-scoped owner context through the handoff callback
	// boundary. A bounded local value keeps direct Runner tests deterministic
	// without placing a credential in the helper endpoint.
	if invocation.Program != "" {
		return "linux-helper:" + invocation.Program
	}
	return "linux-helper:run"
}

func linuxSupervisorStartSpec(command *exec.Cmd, invocation Invocation, writes RecorderFuncs) platform.LinuxSupervisorStartSpec {
	return platform.LinuxSupervisorStartSpec{
		Command:                command,
		OwnerContext:           linuxSupervisorOwnerContext(invocation),
		Prepare:                writes.PrepareSupervisorHandoff,
		Bind:                   writes.BindSupervisorHandoff,
		RecordRecoveryScanSeal: writes.RecordSupervisorRecoveryScanSeal,
	}
}
