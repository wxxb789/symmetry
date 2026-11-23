//go:build !windows

package platform

// UseInProcessContainmentSupervisorForTesting is a no-op off Windows, where the
// local containment route never spawns an independent supervisor helper.
func UseInProcessContainmentSupervisorForTesting() func() {
	return func() {}
}
