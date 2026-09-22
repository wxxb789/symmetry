//go:build !windows

package app

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

var errPreparedSupervisorRecoveryUnsupported = errors.New("prepared supervisor recovery is unsupported on this platform")

func recoverPreparedSupervisorPlatform(authority.SupervisorHandoff) (authority.StopReceipt, error) {
	return authority.StopReceipt{}, errPreparedSupervisorRecoveryUnsupported
}

func releasePreparedSupervisorPlatform(authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
	return authority.SupervisorHandoffReleaseProof{}, errPreparedSupervisorRecoveryUnsupported
}

func provePreparedSupervisorAbortedPlatform(authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
	return authority.SupervisorHandoffAbortProof{}, errPreparedSupervisorRecoveryUnsupported
}
