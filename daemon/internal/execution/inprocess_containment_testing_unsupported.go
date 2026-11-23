//go:build !linux && !windows

package execution

func installInProcessSupervisorRouteForTesting() func() { return func() {} }
