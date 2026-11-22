//go:build linux && amd64

// symmetry-linux-clone-bootstrap is the fixed, root-installed B-owned stage
// entered by the A gate only after its one guarded RELEASE and complete B drop.
// It creates a standalone checkout from the A-owned immutable bundle, waits
// for A's durable checkout ACK, then execs the approved native image in this
// same PID. It never has root or A host credentials.
package main

import (
	"errors"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/clonebootstrap"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func main() {
	statusFD := clonebootstrap.StatusFDFromArgs(os.Args)
	if err := clonebootstrap.RunBootstrap(os.Args); err != nil {
		var failure *clonebootstrap.PhaseError
		if !errors.As(err, &failure) {
			failure = &clonebootstrap.PhaseError{Stage: linuxgate.CloneFailureRequest, Errno: uint32(unix.EPROTO), Cause: err}
		}
		if statusFD >= 0 {
			if packet, encodeErr := linuxgate.EncodeCloneBootstrapFailure(failure.Stage, failure.Errno); encodeErr == nil {
				_ = clonebootstrap.SendStatusPacket(statusFD, packet)
				clear(packet)
			}
		}
	}
	os.Exit(1)
}
