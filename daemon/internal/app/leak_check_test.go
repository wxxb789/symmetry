package app

import (
	"fmt"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

// leakCheckTB is the part of testing.TB the per-test leak oracle uses.
// *testing.T satisfies it directly; TestLeakOracleDetectsLeakedGoroutine drives
// the same oracle through a recording implementation, so the oracle's liveness
// is pinned by a test and not only by an out-of-repo mutation record.
type leakCheckTB interface {
	Cleanup(func())
	Error(args ...any)
	Helper()
}

// verifyNoLeakedGoroutines marks the test failed if it leaves a goroutine
// running when it finishes, including the daemon's own background workers.
// Call it first in a test so its cleanup runs last, after the test's own
// cleanups have joined the workers it started.
//
// The baseline snapshot taken here is what keeps a failure attributable:
// goleak's VerifyNone scans every goroutine in the process, so without it a
// goroutine stranded by an earlier test in the package (for example
// writeInputBounded's writer) would fail whichever test runs next and name the
// wrong test as the leak's owner.
//
// The check must not be used from tests that call t.Parallel: VerifyNone cannot
// associate goroutines with a specific test, so a parallel test would inherit
// its neighbours' goroutines; goleak's parallel form is VerifyTestMain over the
// whole package.
//
// There is deliberately no package-wide goleak TestMain yet: writeInputBounded
// (app.go) can strand one writer goroutine per timed-out input write (the F15
// entry in docs/TODO.md), so a whole-package check would need wildcard ignore
// lists that would hide real leaks. Per-test verification keeps this oracle
// honest until that writer goroutine owns its exit path.
func verifyNoLeakedGoroutines(tb leakCheckTB) {
	tb.Helper()
	baseline := goleak.IgnoreCurrent()
	tb.Cleanup(func() {
		goleak.VerifyNone(tb, baseline)
	})
}

// TestLeakOracleDetectsLeakedGoroutine pins the liveness of
// verifyNoLeakedGoroutines: it drives the oracle itself with a recording TB
// while a goroutine is parked in this test, and requires the recorded failure
// to name this test. Neuter the oracle (a no-op body, or an option that ignores
// everything) and this test fails while the 13 verifying tests would stay
// green. It joins the parked goroutine before returning so no later test's
// check sees residue.
func TestLeakOracleDetectsLeakedGoroutine(t *testing.T) {
	recorder := &leakCheckRecorder{}
	verifyNoLeakedGoroutines(recorder)

	release := make(chan struct{})
	started := make(chan struct{})
	exited := make(chan struct{})
	go parkedLeakOracleGoroutine(started, release, exited)
	<-started
	recorder.runCleanups()
	close(release)
	<-exited

	if len(recorder.failures) == 0 {
		t.Fatal("verifyNoLeakedGoroutines reported no failure while a goroutine was parked in this test; the per-test leak oracle is inert")
	}
	if report := strings.Join(recorder.failures, "\n"); !strings.Contains(report, t.Name()) {
		t.Fatalf("verifyNoLeakedGoroutines failed without naming the goroutine parked by this test; reported:\n%s", report)
	}
}

// parkedLeakOracleGoroutine blocks until release is closed. It is a named
// function so the oracle's report reads as this test's own goroutine.
func parkedLeakOracleGoroutine(started chan<- struct{}, release <-chan struct{}, exited chan<- struct{}) {
	close(started)
	<-release
	close(exited)
}

// leakCheckRecorder records what the leak oracle reports instead of failing the
// test that expects the failure.
type leakCheckRecorder struct {
	cleanups []func()
	failures []string
}

func (*leakCheckRecorder) Helper() {}

func (recorder *leakCheckRecorder) Error(args ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintln(args...))
}

func (recorder *leakCheckRecorder) Cleanup(cleanup func()) {
	recorder.cleanups = append(recorder.cleanups, cleanup)
}

// runCleanups runs the recorded cleanups in testing.T's order: last registered
// first.
func (recorder *leakCheckRecorder) runCleanups() {
	for i := len(recorder.cleanups) - 1; i >= 0; i-- {
		recorder.cleanups[i]()
	}
}
