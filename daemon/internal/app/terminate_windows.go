//go:build windows

package app

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var (
	readPersistedProcessIdentity = platform.ProcessIdentity
	killPersistedProcessTree     = killProcessTree
)

func terminatePersistedProcess(pid int, identity string) error {
	if pid <= 0 || identity == "" {
		return errors.New("persisted process identity is required")
	}
	actual, err := readPersistedProcessIdentity(pid)
	if errors.Is(err, syscall.Errno(87)) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read persisted process identity: %w", err)
	}
	if actual != identity {
		return nil
	}
	return killPersistedProcessTree(pid)
}

func killProcessTree(pid int) error {
	output, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("terminate persisted process tree %d: %w: %s", pid, err, output)
	}
	return nil
}
