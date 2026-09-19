//go:build windows

package app

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func init() {
	releasePersistedContainmentAuthority = releasePersistedProcessContainment
}

var readPersistedProcessIdentity = platform.ProcessIdentity

func terminatePersistedProcess(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	expectedTargetIdentity, verifiedContainment := platform.TargetProcessIdentity(identity)
	if !verifiedContainment {
		return fmt.Errorf("%w: persisted Windows containment identity is unavailable", errPersistedProcessStopUnproven)
	}
	actual, err := readPersistedProcessIdentity(pid)
	if errors.Is(err, syscall.Errno(87)) {
		return fmt.Errorf("%w: persisted process leader is absent", errPersistedProcessStopUnproven)
	}
	if err != nil {
		return fmt.Errorf("%w: read persisted process identity: %v", errPersistedProcessStopUnproven, err)
	}
	if actual != expectedTargetIdentity {
		return fmt.Errorf("%w: persisted process identity changed", errPersistedProcessStopUnproven)
	}
	if err := platform.RecoverPersistedContainment(pid, identity); err != nil {
		return fmt.Errorf("%w: %v", errPersistedProcessStopUnproven, err)
	}
	return nil
}

// terminatePersistedProcessWithAuthority is the only Windows recovery path
// allowed to use a persisted supervisor. It deliberately does not require the
// target leader PID to remain alive: the helper's original Job handle is the
// containment authority and can stop descendants after the leader exits.
func terminatePersistedProcessWithAuthority(pid int, identity string, value *authority.Supervisor) (authority.StopReceipt, error) {
	if value == nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted Windows containment authority is unavailable", errPersistedProcessStopUnproven)
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return authority.StopReceipt{}, fmt.Errorf("%w: persisted containment authority target mismatch", errPersistedProcessStopUnproven)
	}
	receipt, err := platform.RecoverPersistedContainmentWithAuthority(pid, identity, value)
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("%w: %v", errPersistedProcessStopUnproven, err)
	}
	return receipt, nil
}

func releasePersistedProcessContainment(pid int, identity string, value *authority.Supervisor) error {
	if value == nil {
		return fmt.Errorf("%w: persisted Windows containment authority is unavailable", errPersistedProcessStopUnproven)
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return fmt.Errorf("%w: persisted containment authority target mismatch", errPersistedProcessStopUnproven)
	}
	if value.StopReceipt == nil || !value.StopReceipt.ValidFor(*value) {
		return fmt.Errorf("%w: durable containment stop receipt is required before helper release", errPersistedProcessStopUnproven)
	}
	if err := platform.ReleasePersistedContainmentWithAuthority(pid, identity, value); err != nil {
		return fmt.Errorf("%w: %v", errPersistedProcessStopUnproven, err)
	}
	return nil
}
