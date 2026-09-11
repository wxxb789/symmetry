//go:build linux

package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var (
	readPersistedProcessIdentity   = platform.ProcessIdentity
	findPersistedProcess           = os.FindProcess
	terminatePersistedProcessGroup = platform.TerminateProcessGroup
)

func terminatePersistedProcess(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	actual, err := readPersistedProcessIdentity(pid)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: persisted process leader is absent", errPersistedProcessStopUnproven)
	}
	if err != nil {
		return fmt.Errorf("%w: read persisted process identity: %v", errPersistedProcessStopUnproven, err)
	}
	if actual != identity {
		return fmt.Errorf("%w: persisted process identity changed", errPersistedProcessStopUnproven)
	}
	process, err := findPersistedProcess(pid)
	if err != nil {
		return fmt.Errorf("%w: find persisted process: %v", errPersistedProcessStopUnproven, err)
	}
	defer process.Release()
	if err := terminatePersistedProcessGroup(context.Background(), process, identity); err != nil {
		return fmt.Errorf("%w: terminate persisted process group: %v", errPersistedProcessStopUnproven, err)
	}
	return nil
}
