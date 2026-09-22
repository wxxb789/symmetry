//go:build windows

package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var attachSuspendedProcessWithHandoff = platform.AttachSuspendedProcessWithHandoff

func defaultProcessLauncher(
	ctx context.Context,
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error) {
	if command == nil {
		return nil, fmt.Errorf("native process command is missing")
	}
	if ctx == nil {
		return nil, errors.New("native process launcher context is nil")
	}
	if durableSupervisorHandoffRequested(invocation) && !durableSupervisorHandoffCallbacksComplete(invocation) {
		return nil, errors.New("durable supervisor handoff callbacks are incomplete")
	}

	native, err := platform.LaunchSuspended(platform.NativeLaunchSpec{
		Application: command.Path,
		Argv:        append([]string(nil), command.Args...),
		Env:         cloneEnvironment(command.Env),
		Cwd:         command.Dir,
		Headless:    true,
		Stdin:       stdinRead,
		Stdout:      stdoutWrite,
		Stderr:      stderrWrite,
	})
	if err != nil {
		return nil, err
	}

	if durableSupervisorHandoffRequested(invocation) && platform.DurableSupervisorHandoffAvailable() {
		return launchNativeProcessWithSupervisorHandoff(ctx, native, command, invocation)
	}

	containment, identity, attachErr := platform.AttachSuspendedProcess(native)
	started := nativeStartedProcess(native, containment, identity)
	if attachErr != nil {
		return started, fmt.Errorf("contain process tree for %q: %w", command.Path, attachErr)
	}
	return started, nil
}

func launchNativeProcessWithSupervisorHandoff(ctx context.Context, native *platform.SuspendedProcess, command *exec.Cmd, invocation Invocation) (*startedProcess, error) {
	prepareUnknown := false
	bindUnknown := false
	prepare := func(value authority.SupervisorHandoff) error {
		firstErr := invocation.PrepareSupervisorHandoff(value.Clone())
		if firstErr == nil {
			return nil
		}
		if retryErr := invocation.PrepareSupervisorHandoff(value.Clone()); retryErr != nil {
			prepareUnknown = true
			return errors.Join(firstErr, retryErr)
		}
		return nil
	}
	bind := func(value authority.SupervisorHandoff, pid int, identity string) error {
		firstErr := invocation.BindSupervisorHandoff(value.Clone(), pid, identity)
		if firstErr == nil {
			return nil
		}
		if retryErr := invocation.BindSupervisorHandoff(value.Clone(), pid, identity); retryErr != nil {
			bindUnknown = true
			return errors.Join(firstErr, retryErr)
		}
		return nil
	}
	containment, handoff, identity, attachErr := attachSuspendedProcessWithHandoff(ctx, native, platform.SupervisorHandoffCallbacks{
		Prepare: prepare,
		Bind:    bind,
	})
	// AttachSuspendedProcessWithHandoff returns the helper identity on its
	// successful path, while Runner.Identity is the target identity persisted
	// beside the process marker. Prefer the exact target identity carried by the
	// durable handoff; retain the stage-specific return only before a handoff
	// exists (for example, an early target-identity capture failure).
	identity = targetIdentityForHandoff(handoff, identity)
	if attachErr != nil && containment == nil && handoff.Validate() == nil && handoff.SupervisorPID > 0 {
		// The platform may have stopped and detached its local Job after a late
		// attachment error while the independent helper remains retained. Keep a
		// recovery owner in Runner so this durable handoff is not orphaned.
		containment = newPreparedSupervisorCleanupContainment(native, handoff)
	}
	started := nativeStartedProcess(native, containment, identity)
	if handoff.Validate() == nil {
		cloned := handoff.Clone()
		started.containmentHandoff = &cloned
	}
	started.containmentHandoffPrepareUnknown = prepareUnknown
	started.containmentHandoffBindUnknown = bindUnknown
	if attachErr != nil {
		return started, fmt.Errorf("contain durable process tree for %q: %w", command.Path, attachErr)
	}
	return started, nil
}

func nativeStartedProcess(native *platform.SuspendedProcess, containment platform.Containment, identity string) *startedProcess {
	return &startedProcess{
		pid:         int(native.PID()),
		identity:    identity,
		containment: containment,
		wait: func() (int, error) {
			code, waitErr := native.Wait()
			if waitErr != nil {
				return int(code), waitErr
			}
			if code != 0 {
				return int(code), fmt.Errorf("exit status %d", code)
			}
			return 0, nil
		},
		kill:   func() error { return native.Terminate(1) },
		close:  native.Close,
		resume: native.Resume,
	}
}

func durableSupervisorHandoffRequested(invocation Invocation) bool {
	return invocation.PrepareSupervisorHandoff != nil ||
		invocation.BindSupervisorHandoff != nil ||
		invocation.CommitSupervisorHandoff != nil ||
		invocation.RecordSupervisorHandoffStopReceipt != nil ||
		invocation.ClearSupervisorHandoff != nil
}

func durableSupervisorHandoffCallbacksComplete(invocation Invocation) bool {
	if invocation.PrepareSupervisorHandoff == nil || invocation.BindSupervisorHandoff == nil {
		return false
	}
	return invocation.CommitSupervisorHandoff != nil
}

func targetIdentityForHandoff(handoff authority.SupervisorHandoff, fallback string) string {
	if handoff.TargetIdentity != "" {
		return handoff.TargetIdentity
	}
	return fallback
}

// preparedSupervisorCleanupContainment is used only when the platform has
// already transferred the helper's durable authority but cannot return its
// normal jobContainment owner. It makes recovery, receipt persistence and
// release explicit without introducing another scheduler or journal owner.
type preparedSupervisorCleanupContainment struct {
	mu       sync.Mutex
	native   *platform.SuspendedProcess
	handoff  authority.SupervisorHandoff
	receipt  *authority.StopReceipt
	released bool
}

func newPreparedSupervisorCleanupContainment(native *platform.SuspendedProcess, handoff authority.SupervisorHandoff) platform.Containment {
	return &preparedSupervisorCleanupContainment{native: native, handoff: handoff.Clone()}
}

func (containment *preparedSupervisorCleanupContainment) Terminate(force bool) error {
	if !force {
		return errors.ErrUnsupported
	}
	if containment == nil || containment.native == nil {
		return errors.New("prepared supervisor cleanup process is missing")
	}
	return containment.native.Terminate(1)
}

func (containment *preparedSupervisorCleanupContainment) Close() error {
	if containment == nil {
		return errors.New("prepared supervisor cleanup containment is missing")
	}
	containment.mu.Lock()
	defer containment.mu.Unlock()
	if containment.receipt != nil {
		return nil
	}
	receipt, err := platform.RecoverPreparedSupervisor(containment.handoff.Clone())
	if err != nil {
		return err
	}
	containment.receipt = &receipt
	return nil
}

func (containment *preparedSupervisorCleanupContainment) ContainmentCloseRetryable() bool {
	if containment == nil {
		return false
	}
	containment.mu.Lock()
	defer containment.mu.Unlock()
	return containment.receipt == nil
}

func (containment *preparedSupervisorCleanupContainment) ContainmentStopReceipt() (authority.StopReceipt, bool) {
	if containment == nil {
		return authority.StopReceipt{}, false
	}
	containment.mu.Lock()
	defer containment.mu.Unlock()
	if containment.receipt == nil {
		return authority.StopReceipt{}, false
	}
	return *containment.receipt, true
}

func (containment *preparedSupervisorCleanupContainment) ReleaseContainment() error {
	if containment == nil {
		return errors.New("prepared supervisor cleanup containment is missing")
	}
	containment.mu.Lock()
	defer containment.mu.Unlock()
	if containment.released {
		return nil
	}
	if containment.receipt == nil {
		return errors.New("prepared supervisor stop receipt is required before release")
	}
	handoff := containment.handoff.Clone()
	receipt := *containment.receipt
	handoff.StopReceipt = &receipt
	if _, err := platform.ReleasePreparedSupervisor(handoff); err != nil {
		return err
	}
	containment.released = true
	return nil
}

var _ platform.Containment = (*preparedSupervisorCleanupContainment)(nil)
var _ platform.ContainmentCloseRetryer = (*preparedSupervisorCleanupContainment)(nil)
var _ platform.ContainmentStopReceiptProvider = (*preparedSupervisorCleanupContainment)(nil)
var _ platform.ContainmentStopReceiptReleaser = (*preparedSupervisorCleanupContainment)(nil)
