//go:build !windows && !linux

package app

import (
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func terminatePersistedProcess(_ int, _ string) error {
	return fmt.Errorf("%w: %v", errPersistedProcessStopUnproven, errors.New("persisted process termination is unsupported on this platform"))
}

func terminatePersistedProcessWithAuthority(_ int, _ string, _ *authority.Supervisor) (authority.StopReceipt, error) {
	return authority.StopReceipt{}, fmt.Errorf("%w: persisted containment authority is unsupported on this platform", errPersistedProcessStopUnproven)
}
