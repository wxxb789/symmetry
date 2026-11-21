package harness

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

// The daemon persists this value before it admits native work.
type ProcessIdentity interface {
	ProcessDetails() (int, string)
}

// Holds the session's process field and mutex so the session keeps a single process
// authority and lock discipline; build it after the session struct so the address is stable.
type ProcessForwarder[P ProcessIdentity] struct {
	mutex   *sync.Mutex
	process *P
}

func NewProcessForwarder[P ProcessIdentity](mutex *sync.Mutex, process *P) ProcessForwarder[P] {
	return ProcessForwarder[P]{mutex: mutex, process: process}
}

// Holds the session lock for the call, so identity cannot change between the nil
// check and the read.
func (forwarder ProcessForwarder[P]) Details() (int, string) {
	forwarder.mutex.Lock()
	defer forwarder.mutex.Unlock()
	return forwarder.DetailsLocked()
}

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

// The process is read under the session lock; a nil or typed-nil process has no lease
// capability, and the capability check runs after that lock is released.
func (forwarder ProcessForwarder[P]) LeaseRenewalAvailable() bool {
	process := forwarder.snapshot()
	if IsNilInterface(process) {
		return false
	}
	renewer, ok := any(process).(execution.LeaseRenewer)
	return ok && renewer.LeaseRenewalAvailable()
}

func (forwarder ProcessForwarder[P]) RenewLease(deadline time.Duration, sequence uint64) error {
	process := forwarder.snapshot()
	if IsNilInterface(process) {
		return errors.ErrUnsupported
	}
	renewer, ok := any(process).(execution.LeaseRenewer)
	if !ok {
		return errors.ErrUnsupported
	}
	return renewer.RenewLease(deadline, sequence)
}

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

func (forwarder ProcessForwarder[P]) snapshot() P {
	forwarder.mutex.Lock()
	defer forwarder.mutex.Unlock()
	return *forwarder.process
}
