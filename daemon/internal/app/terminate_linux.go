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

func init() {
	releasePersistedContainmentAuthority = releasePersistedLinuxContainmentAuthority
}

func terminatePersistedProcessWithAuthority(pid int, identity string, persisted *authority.Supervisor) (authority.StopReceipt, error) {
	if persisted == nil || persisted.TargetPID != pid || persisted.TargetIdentity != identity {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted Linux authority identity mismatch", errPersistedProcessStopUnproven)
	}
	receipt, err := platform.StopPersistedLinuxSupervisor(*persisted)
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: recover persisted Linux supervisor: %w", errPersistedProcessStopUnproven, err)
	}
	return receipt, nil
}

func releasePersistedLinuxContainmentAuthority(pid int, identity string, persisted *authority.Supervisor) error {
	if persisted == nil || persisted.TargetPID != pid || persisted.TargetIdentity != identity {
		return fmt.Errorf("%w: persisted Linux authority identity mismatch", errPersistedProcessStopUnproven)
	}
	// Legacy/non-Linux authority records remain compatible journal data. Only
	// the explicit Linux helper owner may authorize the Linux endpoint release
	// proof; legacy records retain the shared recovery seam's no-op boundary.
	switch persisted.OwnerKind {
	case "", authority.OwnerKindLegacyWindows:
		return nil
	case authority.OwnerKindLinuxHelper:
		// Continue with the authenticated Linux helper release proof below.
	default:
		return fmt.Errorf("%w: unsupported persisted authority owner kind %q", errPersistedProcessStopUnproven, persisted.OwnerKind)
	}
	if err := platform.ReleasePersistedLinuxSupervisor(*persisted); err != nil {
		return fmt.Errorf("%w: release persisted Linux supervisor: %w", errPersistedProcessStopUnproven, err)
	}
	return nil
}
