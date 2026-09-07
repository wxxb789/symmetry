package app

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/state"
)

var errRequiredDecisionPacket = errors.New("supervisory agent requires a valid consequential decision packet")

func (daemon *daemon) validateWaitingPacket(key state.RunKey, payload map[string]any) error {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return err
	}
	_, packetPresent := payload["decision"]
	if !validDecisionPacket(payload) {
		if journal.Work.RequiredCapabilities["supervisory_control"] {
			return errRequiredDecisionPacket
		}
		if packetPresent {
			return errors.New("agent decision packet is invalid")
		}
	}
	return nil
}
