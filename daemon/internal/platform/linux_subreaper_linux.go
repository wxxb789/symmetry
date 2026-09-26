package platform

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var linuxChildSubreaper struct {
	sync.Once
	err error
}

// ensureLinuxChildSubreaper makes the daemon the adoption boundary for a
// helper's target tree. The mirror can then reap only the exact process group
// that it owns after the helper dies.
func ensureLinuxChildSubreaper() error {
	linuxChildSubreaper.Do(func() {
		linuxChildSubreaper.err = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
	})
	if linuxChildSubreaper.err != nil {
		return fmt.Errorf("enable Linux child subreaper: %w", linuxChildSubreaper.err)
	}
	return nil
}

// EnsureLinuxChildSubreaper installs the daemon-wide adoption boundary. It is
// intentionally called by the production daemon entrypoint, not every unit
// test that constructs a supervisor in-process.
func EnsureLinuxChildSubreaper() error {
	return ensureLinuxChildSubreaper()
}

// reapLinuxProcessGroupChildren reaps adopted children from one exact process
// group. It never waits on unrelated children owned by another run or helper.
func reapLinuxProcessGroupChildren(anchor linuxProcessGroupAnchor, deadline time.Time) error {
	if anchor.pgrp <= 0 {
		return errors.New("Linux process-group reaper requires a positive group anchor")
	}
	for {
		if remaining := time.Until(deadline); remaining <= 0 {
			return fmt.Errorf("reap Linux process group %d: deadline exceeded", anchor.pgrp)
		}
		var status unix.WaitStatus
		waitedPID, err := unix.Wait4(-int(anchor.pgrp), &status, unix.WNOHANG, nil)
		if waitedPID > 0 {
			continue
		}
		if errors.Is(err, unix.ECHILD) || errors.Is(err, unix.ESRCH) {
			return nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reap Linux process group %d: %w", anchor.pgrp, err)
		}
		remaining := time.Until(deadline)
		if remaining > 10*time.Millisecond {
			remaining = 10 * time.Millisecond
		}
		timer := time.NewTimer(remaining)
		<-timer.C
	}
}
