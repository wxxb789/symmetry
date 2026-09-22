//go:build !windows && !linux

package platform

import "errors"

// RunContainmentSupervisor is available only on platforms with a native
// containment helper. Keeping the symbol on other platforms lets the shared
// daemon entrypoint retain a compile-time-safe hidden-mode dispatch.
func RunContainmentSupervisor([]string) error {
	return errors.New("Windows containment supervisor is unsupported on this platform")
}
