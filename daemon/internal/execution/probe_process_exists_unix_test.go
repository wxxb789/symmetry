//go:build !windows

package execution

func probeProcessExists(pid int) bool {
	return unixProcessExists(pid)
}
