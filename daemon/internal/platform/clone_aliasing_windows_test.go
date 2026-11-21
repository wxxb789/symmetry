//go:build windows

package platform

import "testing"

// TestCloneAliasingWindowsContainmentSessionID covers the one clone function
// compiled under //go:build windows. The clone copies one uint32 behind a
// pointer, so the case is written directly instead of sharing the Linux-only
// harness in clone_aliasing_linux_test.go: the clone must copy the pointee, and
// writing through the original pointer must not be visible through the clone.
func TestCloneAliasingWindowsContainmentSessionID(t *testing.T) {
	original := new(uint32)
	*original = 4242
	cloned := cloneContainmentSessionID(original)
	if cloned == nil {
		t.Fatal("clone returned nil for a non-nil session id")
	}
	if *cloned != *original {
		t.Fatalf("clone content differs from the populated original: clone %d, original %d", *cloned, *original)
	}
	if cloned == original {
		t.Fatal("clone returned the original pointer")
	}
	*original = 7777
	if *cloned != 4242 {
		t.Fatalf("clone shares the pointee with the original: clone changed to %d", *cloned)
	}
}
