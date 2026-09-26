//go:build windows

package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func recoverPreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.StopReceipt, error) {
	return platform.RecoverPreparedSupervisor(handoff)
}

func releasePreparedSupervisorPlatform(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	return platform.ReleasePreparedSupervisor(handoff)
}

func provePreparedSupervisorAbortedPlatform(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
	// Recovery owns the exclusive Store lifetime after daemon restart, so the
	// previous owner is quiesced before this platform proof is requested.
	return platform.ProvePreparedSupervisorAborted(handoff, func() error { return nil })
}
