//go:build linux

package app

import (
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func recoverPreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.StopReceipt, error) {
	value, err := handoff.ToSupervisor()
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("convert Linux supervisor handoff: %w", err)
	}
	return platform.StopPersistedLinuxSupervisor(value)
}

func releasePreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	value, err := handoff.ToSupervisor()
	if err != nil {
		return authority.SupervisorHandoffReleaseProof{}, fmt.Errorf("convert Linux supervisor handoff: %w", err)
	}
	if value.StopReceipt == nil || !value.StopReceipt.ValidFor(value) {
		return authority.SupervisorHandoffReleaseProof{}, errors.New("Linux supervisor handoff stop receipt is required before release")
	}
	if err := platform.ReleasePersistedLinuxSupervisor(value); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, err
	}
	return handoff.ReleaseProof(), nil
}

func provePreparedSupervisorAbortedPlatform(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
	// Recovery owns the exclusive Store lifetime after daemon restart, so the
	// previous owner is quiesced before this platform proof is requested.
	return platform.ProvePreparedSupervisorAborted(handoff, func() error { return nil })
}
