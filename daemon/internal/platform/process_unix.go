//go:build !windows

// Package platform contains process-control primitives that differ by OS.
package platform

import (
	"errors"
	"os/exec"
	"syscall"
)

// Containment owns the platform mechanism that keeps all descendants under the
// lifecycle of a launched process.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

// ConfigureProcess isolates a child in its own process group so its complete
// descendant tree can be signalled without affecting the daemon.
func ConfigureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// TerminateProcessTree sends a termination signal to the process group whose
// leader is pid. A missing process is already terminated and is not an error.
func AttachProcess(pid int) (Containment, error) {
	return processGroup{pid: pid}, nil
}

type processGroup struct {
	pid int
}

func (group processGroup) Terminate(force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(-group.pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// Close reaps descendants that remain in the process group after the root
// process exits, including those retaining an inherited stdout or stderr pipe.
func (group processGroup) Close() error {
	return group.Terminate(true)
}
