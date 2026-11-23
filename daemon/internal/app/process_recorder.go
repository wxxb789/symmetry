package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// processRecorder binds one run's key to the persist*Callback factories, so each
// durable write performs exactly the Store mutation, fence check, replay and error
// wrapping the callback it replaces performed.
func (daemon *daemon) processRecorder(key state.RunKey, persistProcess func(pid int, identity string) error) execution.Recorder {
	persistAuthority := daemon.persistProcessAuthorityCallback(key)
	return execution.NewRecorder(execution.RecorderFuncs{
		PrepareSupervisorHandoff:           daemon.prepareSupervisorHandoffCallback(key),
		BindSupervisorHandoff:              daemon.bindSupervisorHandoffCallback(key),
		CommitSupervisorHandoff:            daemon.commitSupervisorHandoffCallback(key),
		RecordSupervisorHandoffStopReceipt: daemon.recordSupervisorHandoffStopReceiptCallback(key),
		ClearSupervisorHandoff:             daemon.clearSupervisorHandoffCallback(key),
		PersistProcess:                     persistProcess,
		PersistProcessWithAuthority:        persistAuthority,
		PersistProcessAuthority:            persistAuthority,
		PersistContainmentStopReceipt:      daemon.persistContainmentStopReceiptCallback(key),
		PersistContainmentUnproven:         daemon.markContainmentUnprovenCallback(key),
		RecordSupervisorRecoveryScanSeal:   daemon.recordSupervisorRecoveryScanSealCallback(key),
	})
}

func (daemon *daemon) recordSupervisorRecoveryScanSealCallback(key state.RunKey) func(authority.Supervisor, uint64) error {
	return func(value authority.Supervisor, sequence uint64) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		if daemon.storeClosing || daemon.store == nil {
			return errDaemonStoreClosing
		}
		return daemon.persistSupervisorRecoveryScanSeal(key, value, sequence)
	}
}
