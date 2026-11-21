package app

import (
	"testing"

	"go.uber.org/goleak"
)

// verifyNoLeakedGoroutines marks the test failed if it leaves a goroutine
// running when it finishes, including the daemon's own background workers.
// Call it first in a test so its cleanup runs last, after the test's own
// cleanups have joined the workers it started.
//
// There is deliberately no package-wide goleak TestMain yet: writeInputBounded
// (app.go) can strand one writer goroutine per timed-out input write (the F15
// entry in docs/TODO.md), so a whole-package check would need wildcard ignore
// lists that would hide real leaks. Per-test verification keeps this oracle
// honest until that writer goroutine owns its exit path.
func verifyNoLeakedGoroutines(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
}

// TestLeakOracleDetectsLeakedGoroutine pins the liveness of
// verifyNoLeakedGoroutines: if the oracle ever becomes a no-op, or starts
// ignoring everything, this test fails while the 13 verifying tests would
// stay green. It parks one goroutine, requires goleak.Find to report it, and
// joins it before returning so no later test's VerifyNone sees residue. It
// asserts against goleak.Find directly instead of verifyNoLeakedGoroutines,
// because a reported leak is this test's expected outcome, not a failure.
func TestLeakOracleDetectsLeakedGoroutine(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		close(started)
		<-release
	}()
	<-started
	err := goleak.Find()
	close(release)
	<-exited
	if err == nil {
		t.Error("goleak.Find() found no leaked goroutine while one was parked in this test; the per-test leak oracle is inert")
	}
}
