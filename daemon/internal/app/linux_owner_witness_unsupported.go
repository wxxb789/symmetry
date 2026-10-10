//go:build !linux || !amd64

package app

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const LinuxV2NativeOwnerWitnessEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS"

func RunLinuxV2NativeOwnerWitness(_ context.Context, _, _ string, _ io.Writer) error {
	if os.Getenv(LinuxV2NativeOwnerWitnessEnvironment) != "1" {
		return errors.New("Linux v2 native owner witness is disabled")
	}
	return errors.New("Linux v2 native owner witness is supported only on Linux amd64")
}

func (daemon *daemon) prepareLinuxV2CloneRun(context.Context, state.RunKey, protocol.Fence, protocol.Admission) (*linuxCloneRun, string, error) {
	return nil, "", errors.ErrUnsupported
}

func (daemon *daemon) installLinuxCloneRun(state.RunKey, *linuxCloneRun) error {
	return errors.ErrUnsupported
}

func (daemon *daemon) linuxV2JournalCallbacks(state.RunKey, protocol.Fence) execution.LinuxV2JournalCallbacks {
	return execution.LinuxV2JournalCallbacks{}
}

func (daemon *daemon) installLinuxCloneRunnerCallbacks(state.RunKey, protocol.Fence, protocol.Admission, *linuxCloneRun, *execution.LinuxV2JournalCallbacks) error {
	return errors.ErrUnsupported
}

func (daemon *daemon) abortLinuxV2CloneBeforeCheckout(context.Context, state.RunKey, protocol.Fence, authority.LinuxCloneIntent) error {
	return errors.ErrUnsupported
}
