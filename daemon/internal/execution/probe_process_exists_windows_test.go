//go:build windows

package execution

func probeProcessExists(pid int) bool {
	exists, _ := windowsProcessExists(pid)
	return exists
}
