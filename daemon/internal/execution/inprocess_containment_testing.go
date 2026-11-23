package execution

import "github.com/wxxb789/symmetry/daemon/internal/platform"

// UseInProcessContainmentSupervisorForTesting keeps in-process launches on the
// local containment route: a generated Go test main cannot dispatch the helper
// the durable supervisor route spawns. Nothing in production calls it.
func UseInProcessContainmentSupervisorForTesting() func() {
	restorePlatform := platform.UseInProcessContainmentSupervisorForTesting()
	restoreRoute := installInProcessSupervisorRouteForTesting()
	return func() {
		restoreRoute()
		restorePlatform()
	}
}
