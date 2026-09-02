//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

var getProcessTimes = kernel32.NewProc("GetProcessTimes")

// ProcessIdentity returns a persistent identity made from a PID and the
// creation FILETIME reported by Windows. A recycled PID has a new FILETIME.
func ProcessIdentity(pid int) (string, error) {
	process, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return "", fmt.Errorf("open process for creation time: %w", err)
	}
	defer syscall.CloseHandle(process)

	var creationTime syscall.Filetime
	var exitTime syscall.Filetime
	var kernelTime syscall.Filetime
	var userTime syscall.Filetime
	result, _, callError := getProcessTimes.Call(
		uintptr(process),
		uintptr(unsafe.Pointer(&creationTime)),
		uintptr(unsafe.Pointer(&exitTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if result == 0 {
		return "", fmt.Errorf("get process creation time: %w", callError)
	}

	creation := uint64(creationTime.HighDateTime)<<32 | uint64(creationTime.LowDateTime)
	return fmt.Sprintf("windows:%d:%016x", pid, creation), nil
}
