//go:build linux

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func terminatePersistedProcess(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	actual, err := platform.ProcessIdentity(pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read persisted process identity: %w", err)
	}
	if actual != identity {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
