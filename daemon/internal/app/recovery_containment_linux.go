//go:build linux

package app

import (
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func recoverPreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.StopReceipt, error) {
	if handoff.Version == authority.SupervisorHandoffVersionV2 {
		return authority.StopReceipt{}, errors.New("v2 handoff recovery requires its exact cgroup reservation and anchor helper")
	}
	value, err := handoff.ToSupervisor()
	if err != nil {
		return authority.StopReceipt{}, fmt.Errorf("convert Linux supervisor handoff: %w", err)
	}
	return platform.StopPersistedLinuxSupervisor(value)
}

func releasePreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	if handoff.Version == authority.SupervisorHandoffVersionV2 {
		return authority.SupervisorHandoffReleaseProof{}, errors.New("v2 handoff release requires its opaque pinned-leaf removal token")
	}
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
	if handoff.Version == authority.SupervisorHandoffVersionV2 {
		return authority.SupervisorHandoffAbortProof{}, errors.New("v2 reservation abort requires its exact producer-fencing proof")
	}
	// Recovery owns the exclusive Store lifetime after daemon restart, so the
	// previous owner is quiesced before this platform proof is requested.
	return platform.ProvePreparedSupervisorAborted(handoff, func() error { return nil })
}
