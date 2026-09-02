//go:build !windows && !linux

package app

import "errors"

func terminatePersistedProcess(_ int, _ string) error {
	return errors.New("persisted process termination is unsupported on this platform")
}
