package app

import (
	"fmt"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

type leakCheckTB interface {
	Cleanup(func())
	Error(args ...any)
	Helper()
}

// Call first in a test so this cleanup runs last, and never from a t.Parallel test: goleak scans
// the whole process, so only the baseline snapshot keeps a leak attributed to the test that stranded it.
func verifyNoLeakedGoroutines(tb leakCheckTB) {
	tb.Helper()
	baseline := goleak.IgnoreCurrent()
	tb.Cleanup(func() {
		goleak.VerifyNone(tb, baseline)
	})
}

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

func parkedLeakOracleGoroutine(started chan<- struct{}, release <-chan struct{}, exited chan<- struct{}) {
	close(started)
	<-release
	close(exited)
}

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

func (recorder *leakCheckRecorder) runCleanups() {
	for i := len(recorder.cleanups) - 1; i >= 0; i-- {
		recorder.cleanups[i]()
	}
}
