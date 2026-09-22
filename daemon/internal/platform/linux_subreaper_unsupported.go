//go:build !linux

package platform

// EnsureLinuxChildSubreaper is a no-op on platforms without Linux child
// subreaper semantics.
func EnsureLinuxChildSubreaper() error { return nil }
