//go:build !windows && !linux

package app

import (
	"errors"
	"fmt"
)

func terminatePersistedProcess(_ int, _ string) error {
	return fmt.Errorf("%w: %v", errPersistedProcessStopUnproven, errors.New("persisted process termination is unsupported on this platform"))
}
