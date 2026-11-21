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
