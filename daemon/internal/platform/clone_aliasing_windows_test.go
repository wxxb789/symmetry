//go:build windows

package platform

import "testing"

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
