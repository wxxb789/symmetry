//go:build !linux && !windows

// Package platform contains process-control primitives that differ by OS.
package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Containment owns a launched process's platform-specific termination boundary.
type Containment interface {
	Terminate(force bool) error
	Close() error
}

// ConfigureProcess fails closed where native process containment is unsupported.
func ConfigureProcess(*exec.Cmd) error {
	return fmt.Errorf("%w: native process containment is unsupported on this platform", errors.ErrUnsupported)
}

// AttachProcess fails closed where native process containment is unsupported.
func AttachProcess(*os.Process) (Containment, string, error) {
	return nil, "", fmt.Errorf("%w: native process containment is unsupported on this platform", errors.ErrUnsupported)
}

// TerminateProcessGroup fails closed where pidfd process-group termination is unsupported.
func TerminateProcessGroup(context.Context, *os.Process, string) error {
	return fmt.Errorf("%w: pidfd process-group termination is unsupported on this platform", errors.ErrUnsupported)
}
