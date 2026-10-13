//go:build !linux

package platform

import (
	"context"
	"errors"
	"net"
	"os"
)

// CaptureLinuxUserNamespaceIdentity is unavailable outside Linux. The native
// Windows peer verifier retains its existing platform-specific ownership path.
func CaptureLinuxUserNamespaceIdentity(int, string, *os.File) (LinuxUserNamespaceIdentity, error) {
	return LinuxUserNamespaceIdentity{}, errors.ErrUnsupported
}

// VerifyLoopbackTCPPeerV2 never substitutes Windows peer evidence for a Linux
// cgroup owner's retained pidfd and user namespace.
func VerifyLoopbackTCPPeerV2(context.Context, net.Conn, int, string, *os.File, LinuxUserNamespaceIdentity) error {
	return errors.ErrUnsupported
}
