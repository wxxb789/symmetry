//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"strconv"
)

func terminatePersistedProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	output, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("terminate persisted process tree %d: %w: %s", pid, err, output)
	}
	return nil
}
