package execution

import "github.com/wxxb789/symmetry/daemon/internal/platform"

// UseInProcessContainmentSupervisorForTesting keeps in-process launches on the local
// route a generated test main can dispatch; nothing in production calls it.
func UseInProcessContainmentSupervisorForTesting() func() {
	restorePlatform := platform.UseInProcessContainmentSupervisorForTesting()
	restoreRoute := installInProcessSupervisorRouteForTesting()
	return func() {
		restoreRoute()
		restorePlatform()
	}
}
