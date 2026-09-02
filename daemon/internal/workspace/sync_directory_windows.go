//go:build windows

package workspace

import (
	"fmt"
	"syscall"
)

const fileFlagBackupSemantics = 0x02000000

func syncDirectory(path string) error {
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		fileFlagBackupSemantics,
		0,
	)
	if err != nil {
		return fmt.Errorf("open directory %q: %w", path, err)
	}
	defer syscall.CloseHandle(handle)
	if err := syscall.FlushFileBuffers(handle); err != nil {
		// Windows does not support flushing directory handles on common filesystems.
		if err == syscall.ERROR_ACCESS_DENIED {
			return nil
		}
		return fmt.Errorf("flush directory %q: %w", path, err)
	}
	return nil
}
