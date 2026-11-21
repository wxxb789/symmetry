package harness

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

// ProcessIdentity is the narrow process-identity seam a staged session
// forwards. The daemon persists this value before it admits native work.
type ProcessIdentity interface {
	ProcessDetails() (int, string)
}

// ProcessForwarder forwards the optional capabilities of the process one
// staged session owns. It holds the session's process field and the mutex that
// guards it, so the session keeps a single process authority and its existing
// lock discipline. Build the forwarder after the session struct so the field
// address it holds is stable.
type ProcessForwarder[P ProcessIdentity] struct {
	mutex   *sync.Mutex
	process *P
}

// NewProcessForwarder binds a session's process field and the mutex that
// guards it.
func NewProcessForwarder[P ProcessIdentity](mutex *sync.Mutex, process *P) ProcessForwarder[P] {
	return ProcessForwarder[P]{mutex: mutex, process: process}
}

// Details reports the exact process identity. It holds the session lock for the
// call, so identity cannot change between the nil check and the read.
func (forwarder ProcessForwarder[P]) Details() (int, string) {
	forwarder.mutex.Lock()
	defer forwarder.mutex.Unlock()
	return forwarder.DetailsLocked()
}

// DetailsLocked is Details for a caller that already holds the session lock.
func (forwarder ProcessForwarder[P]) DetailsLocked() (int, string) {
	if forwarder.process == nil {
		return 0, ""
	}
	process := *forwarder.process
	if IsNilInterface(process) {
		return 0, ""
	}
	return process.ProcessDetails()
}

// LeaseRenewalAvailable reports whether the owned process can renew a
// containment lease. The process is read under the session lock; the capability
// check runs after that lock is released.
func (forwarder ProcessForwarder[P]) LeaseRenewalAvailable() bool {
	renewer, ok := any(forwarder.snapshot()).(execution.LeaseRenewer)
	return ok && renewer.LeaseRenewalAvailable()
}

// RenewLease renews the containment lease of the owned process, or reports
// errors.ErrUnsupported when the process has no lease capability.
func (forwarder ProcessForwarder[P]) RenewLease(deadline time.Duration, sequence uint64) error {
	renewer, ok := any(forwarder.snapshot()).(execution.LeaseRenewer)
	if !ok {
		return errors.ErrUnsupported
	}
	return renewer.RenewLease(deadline, sequence)
}

// LiveOwnerStatus forwards the owned process's bounded, read-only Linux v2
// helper observation, or reports errors.ErrUnsupported for a process that
// cannot provide it.
func (forwarder ProcessForwarder[P]) LiveOwnerStatus(ctx context.Context) (execution.LinuxV2LiveOwnerStatus, error) {
	process := forwarder.snapshot()
	if IsNilInterface(process) {
		return execution.LinuxV2LiveOwnerStatus{}, errors.ErrUnsupported
	}
	provider, ok := any(process).(interface {
		LinuxV2LiveOwnerStatus(context.Context) (execution.LinuxV2LiveOwnerStatus, error)
	})
	if !ok {
		return execution.LinuxV2LiveOwnerStatus{}, errors.ErrUnsupported
	}
	return provider.LinuxV2LiveOwnerStatus(ctx)
}

// snapshot reads the session's current process under its lock.
func (forwarder ProcessForwarder[P]) snapshot() P {
	forwarder.mutex.Lock()
	defer forwarder.mutex.Unlock()
	return *forwarder.process
}
