//go:build !linux && !windows

package platform

import (
	"context"
	"errors"
)

func inspectLoopbackTCPPeer(context.Context, tcpPeerTuple, int, string) (bool, error) {
	return false, errors.New("TCP peer ownership verification is unsupported on this platform")
}
