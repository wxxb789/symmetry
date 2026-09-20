package execution

import (
	"context"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestWaitForCompletionPublishesNaturalExitBeforeLateTerminate(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	waitStarted := make(chan struct{})
	waitRelease := make(chan struct{})
	closeReached := make(chan struct{})
	process := newResultRaceProcess(
		func() (int, error) {
			close(waitStarted)
			<-waitRelease
			return 0, nil
		},
		func() error {
			close(closeReached)
			return nil
		},
		func() error { return os.ErrProcessDone },
	)

	process.errorMutex.Lock()
	errorMutexLocked := true
	defer func() {
		if errorMutexLocked {
			process.errorMutex.Unlock()
		}
	}()

	go process.deliverOutput()
	go process.waitForCompletion()
	<-waitStarted
	close(waitRelease)
	<-closeReached
	runtime.Gosched()

	if process.terminationMutex.TryLock() {
		process.terminationMutex.Unlock()
		t.Fatal("termination mutex was released before result publication")
	}

	terminateStarted := make(chan struct{})
	terminateDone := make(chan error, 1)
	go func() {
		close(terminateStarted)
		terminateDone <- process.Terminate(context.Background(), 0)
	}()
	<-terminateStarted
	runtime.Gosched()

	process.errorMutex.Unlock()
	errorMutexLocked = false
	first := waitForResult(t, process)
	if err := waitForResultRaceTermination(t, terminateDone); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	second := process.Wait()

	if first.Terminated {
		t.Fatal("Terminated = true, want false after natural exit won publication")
	}
	if first != second {
		t.Fatalf("repeated Wait() results differ: first = %#v, second = %#v", first, second)
	}
	process.terminationMutex.Lock()
	terminationStarted := process.terminationStart
	terminated := process.terminated
	process.terminationMutex.Unlock()
	if terminationStarted || terminated {
		t.Fatalf("late Terminate changed terminal state: terminationStart = %t, terminated = %t", terminationStarted, terminated)
	}
}

func TestWaitForCompletionPublishesTerminateWinner(t *testing.T) {
	waitStarted := make(chan struct{})
	waitRelease := make(chan struct{})
	var releaseWait sync.Once
	process := newResultRaceProcess(
		func() (int, error) {
			close(waitStarted)
			<-waitRelease
			return 0, nil
		},
		func() error { return nil },
		func() error {
			releaseWait.Do(func() { close(waitRelease) })
			return nil
		},
	)

	go process.deliverOutput()
	go process.waitForCompletion()
	<-waitStarted

	terminateDone := make(chan error, 1)
	go func() { terminateDone <- process.Terminate(context.Background(), 0) }()
	if err := waitForResultRaceTermination(t, terminateDone); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	first := waitForResult(t, process)
	second := process.Wait()

	if !first.Terminated {
		t.Fatal("Terminated = false, want true after Terminate won terminal ownership")
	}
	if first != second {
		t.Fatalf("repeated Wait() results differ: first = %#v, second = %#v", first, second)
	}
}

func newResultRaceProcess(wait func() (int, error), closeProcess, kill func() error) *Process {
	started := &startedProcess{
		pid:      17,
		identity: "result-race",
		wait:     wait,
		close:    closeProcess,
		kill:     kill,
	}
	return newProcessFromStarted(started, SinkFunc(func(context.Context, Event) error {
		return nil
	}), time.Now().UTC(), nil, nil)
}

func waitForResultRaceTermination(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Terminate() did not return")
		return nil
	}
}
