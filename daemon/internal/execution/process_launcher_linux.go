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
	if durableSupervisorHandoffRequestedLinux(invocation) && platform.DurableSupervisorHandoffAvailable() {
		if !durableSupervisorHandoffCallbacksCompleteLinux(invocation) {
			return nil, errors.New("durable Linux supervisor handoff callbacks are incomplete")
		}
		supervisor, launchErr := platform.StartLinuxSupervisor(platform.LinuxSupervisorStartSpec{
			Command:      command,
			OwnerContext: linuxSupervisorOwnerContext(invocation),
			Prepare:      invocation.PrepareSupervisorHandoff,
			Bind:         invocation.BindSupervisorHandoff,
		})
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
			if invocation.CommitSupervisorHandoff != nil {
				if err := invocation.CommitSupervisorHandoff(value, startedAt); err != nil {
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
	_ = stdinRead
	_ = stdoutWrite
	_ = stderrWrite
	if attachErr != nil {
		return started, fmt.Errorf("contain Linux process tree: %w", attachErr)
	}
	return started, nil
}

func durableSupervisorHandoffRequestedLinux(invocation Invocation) bool {
	return invocation.PrepareSupervisorHandoff != nil ||
		invocation.BindSupervisorHandoff != nil ||
		invocation.CommitSupervisorHandoff != nil ||
		invocation.RecordSupervisorHandoffStopReceipt != nil ||
		invocation.ClearSupervisorHandoff != nil
}

func durableSupervisorHandoffCallbacksCompleteLinux(invocation Invocation) bool {
	return invocation.PrepareSupervisorHandoff != nil && invocation.BindSupervisorHandoff != nil && invocation.CommitSupervisorHandoff != nil
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
