//go:build !linux || !amd64

package platform

import "errors"

// DefaultLinuxOwnerAnchorSocketPath is the fixed systemd socket location used
// by Linux owner preflight; unsupported platforms never create or connect it.
const DefaultLinuxOwnerAnchorSocketPath = "/run/symmetry-owner/owner.sock"

// LinuxOwnerAnchorClient is a sentinel type on unsupported targets so shared
// execution contracts can remain concrete while construction fails closed.
type LinuxOwnerAnchorClient struct{}

func NewLinuxOwnerAnchorClient(string) (*LinuxOwnerAnchorClient, error) {
	return nil, errors.New("Linux owner anchor client is supported only on Linux x86-64")
}

// RunLinuxOwnerAnchor is unavailable outside Linux x86-64. It must not launch
// a helper or route to the legacy process-group supervisor on other targets.
func RunLinuxOwnerAnchor([]string) error {
	return errors.New("Linux owner anchor is supported only on Linux x86-64")
}

// RunLinuxOwnerHelperV2 is unavailable outside Linux x86-64. It must not
// route to the legacy process-group helper or execute a target directly.
func RunLinuxOwnerHelperV2([]string) error {
	return errors.New("Linux owner helper v2 is supported only on Linux x86-64")
}
