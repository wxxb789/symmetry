//go:build windows

package app

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var readPersistedProcessIdentity = platform.ProcessIdentity

func terminatePersistedProcess(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	actual, err := readPersistedProcessIdentity(pid)
	if errors.Is(err, syscall.Errno(87)) {
		return fmt.Errorf("%w: persisted process leader is absent", errPersistedProcessStopUnproven)
	}
	if err != nil {
		return fmt.Errorf("%w: read persisted process identity: %v", errPersistedProcessStopUnproven, err)
	}
	if actual != identity {
		return fmt.Errorf("%w: persisted process identity changed", errPersistedProcessStopUnproven)
	}
	// A daemon restart cannot reopen its anonymous Job Object, so no PID-based
	// command can prove that every original Job member has stopped.
	return fmt.Errorf("%w: Windows Job Object membership cannot be read back after daemon restart", errPersistedProcessStopUnproven)
}
