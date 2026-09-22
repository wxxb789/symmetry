//go:build linux

package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var (
	readPersistedProcessIdentity     = platform.ProcessIdentity
	findPersistedProcess             = os.FindProcess
	terminatePersistedProcessGroup   = platform.TerminatePersistedProcessGroup
	provePersistedProcessGroupAbsent = platform.ProvePersistedProcessGroupAbsent
)

func terminatePersistedProcess(pid int, identity string) error {
	return terminatePersistedProcessWithContext(context.Background(), pid, identity)
}

// This helper is used only after recovery has rejected a journal already
// marked ContainmentUnproven. A missing leader plus an ESRCH process-group
// proof cannot rediscover descendants that escaped the original group.
func terminatePersistedProcessWithContext(ctx context.Context, pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	actual, err := readPersistedProcessIdentity(pid)
	if errors.Is(err, os.ErrNotExist) {
		if proofErr := provePersistedProcessGroupAbsent(ctx, pid, identity); proofErr != nil {
			return fmt.Errorf("%w: prove persisted process group absence: %w", errPersistedProcessStopUnproven, proofErr)
		}
		return fmt.Errorf("%w: process-group absence does not prove descendant containment", errPersistedProcessStopUnproven)
	}
	if err != nil {
		return fmt.Errorf("%w: read persisted process identity: %w", errPersistedProcessStopUnproven, err)
	}
	if actual != identity {
		return fmt.Errorf("%w: persisted process identity changed", errPersistedProcessStopUnproven)
	}
	process, err := findPersistedProcess(pid)
	if err != nil {
		return fmt.Errorf("%w: find persisted process: %w", errPersistedProcessStopUnproven, err)
	}
	defer process.Release()
	if err := terminatePersistedProcessGroup(ctx, process, identity); err != nil {
		return fmt.Errorf("%w: terminate persisted process group: %w", errPersistedProcessStopUnproven, err)
	}
	return nil
}

func terminatePersistedProcessWithAuthority(_ int, _ string, _ *authority.Supervisor) (authority.StopReceipt, error) {
	return authority.StopReceipt{}, fmt.Errorf("%w: persisted containment authority is unsupported on Linux", errPersistedProcessStopUnproven)
}
