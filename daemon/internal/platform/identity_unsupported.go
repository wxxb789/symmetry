//go:build !windows && !linux

package platform

import "fmt"

// ProcessIdentity fails closed on platforms without a verified creation-time
// implementation. Callers must not treat PID alone as restart-safe identity.
func ProcessIdentity(pid int) (string, error) {
	return "", fmt.Errorf("process creation identity is unsupported on this platform")
}
