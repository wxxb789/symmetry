//go:build windows

package platform

import (
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// UseInProcessContainmentSupervisorForTesting keeps in-process launches on the local
// route a generated test main can dispatch; nothing in production calls it.
func UseInProcessContainmentSupervisorForTesting() func() {
	previous := launchContainmentSupervisor
	launchContainmentSupervisor = func(_ syscall.Handle, _ int, targetIdentity string) (containmentSupervisorLease, string, error) {
		return noopContainmentSupervisorLease{}, targetIdentity, nil
	}
	return func() { launchContainmentSupervisor = previous }
}

// noopContainmentSupervisorLease claims the stop/release capability the legacy
// local route needs without owning any durable authority.
type noopContainmentSupervisorLease struct{}

func (noopContainmentSupervisorLease) close(time.Time) error { return nil }

func (noopContainmentSupervisorLease) renew(time.Duration, uint64) error { return nil }

func (noopContainmentSupervisorLease) stop(time.Time) (authority.StopReceipt, error) {
	return authority.StopReceipt{}, nil
}

func (noopContainmentSupervisorLease) release(time.Time) error { return nil }
