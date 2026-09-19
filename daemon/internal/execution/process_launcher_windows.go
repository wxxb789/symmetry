//go:build windows

package execution

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func defaultProcessLauncher(
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error) {
	if command == nil {
		return nil, fmt.Errorf("native process command is missing")
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

	containment, identity, attachErr := platform.AttachSuspendedProcess(native)
	started := &startedProcess{
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
	if attachErr != nil {
		return started, fmt.Errorf("contain process tree for %q: %w", command.Path, attachErr)
	}
	return started, nil
}
