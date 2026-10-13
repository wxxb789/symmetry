//go:build !linux || !amd64

package state

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// LinuxCloneCleanupReader is unavailable outside Linux/amd64.
type LinuxCloneCleanupReader struct{}

// OpenLinuxCloneCleanupReader fails closed on unsupported platforms.
func OpenLinuxCloneCleanupReader(string) (*LinuxCloneCleanupReader, error) {
	return nil, errors.New("Linux clone cleanup reader is supported only on Linux/amd64")
}

// Verify cannot authorize cleanup on unsupported platforms.
func (*LinuxCloneCleanupReader) Verify(
	RunKey,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCleanupIntent,
	*authority.LinuxCloneTargetRemovalProof,
	bool,
) error {
	return errors.New("Linux clone cleanup reader is supported only on Linux/amd64")
}

func (*LinuxCloneCleanupReader) verify(
	RunKey,
	authority.LinuxCloneGrant,
	authority.LinuxCloneCleanupIntent,
	*authority.LinuxCloneTargetRemovalProof,
	bool,
	func() error,
) error {
	return errors.New("Linux clone cleanup reader is supported only on Linux/amd64")
}

// Close is an idempotent no-op for an unavailable reader.
func (*LinuxCloneCleanupReader) Close() error { return nil }
