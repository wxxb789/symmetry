//go:build !windows

package platform

import "errors"

// RunContainmentSupervisor is only available in the Windows daemon binary.
// Keeping the symbol on other platforms lets the shared daemon entrypoint
// retain a compile-time-safe hidden-mode dispatch without changing Linux
// containment behavior.
func RunContainmentSupervisor([]string) error {
	return errors.New("Windows containment supervisor is unsupported on this platform")
}
