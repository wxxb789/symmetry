//go:build windows

package execution

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestTerminateClosesStdinBeforeBlockedContainmentStopAndOwnsCleanupOnce(t *testing.T) {
	containment := newTerminationOrderContainment()
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	defer stdinRead.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	defer stderrRead.Close()

	stdinFirstByte := make(chan struct{})
	allowEOFRead := make(chan struct{})
	stdinEOF := make(chan struct{})
	stdinObservationErrors := make(chan error, 1)
	go observeTerminationTestStdin(stdinRead, stdinFirstByte, allowEOFRead, stdinEOF, stdinObservationErrors)

	started := &startedProcess{
		pid:         123,
		identity:    "termination-order",
		containment: containment,
		wait: func() (int, error) {
			<-containment.waitRelease
			return 0, nil
		},
		kill:  func() error { return nil },
		close: func() error { return nil },
	}
	process := newProcessFromStarted(started, &recordingSink{}, time.Now().UTC(), stdinWrite, nil)
	process.start(stdoutRead, stderrRead, context.Background())
	if err := stdoutWrite.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := stderrWrite.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	if err := process.WriteInput([]byte("x")); err != nil {
		t.Fatalf("initial stdin write error = %v", err)
	}
	waitTerminationOrder(t, stdinFirstByte, "stdin writer did not reach the pipe")

	writeResult := make(chan error, 1)
	go func() {
		writeResult <- process.WriteInput(bytes.Repeat([]byte("x"), 32<<20))
	}()

	terminateResult := make(chan error, 1)
	go func() {
		terminateResult <- process.Terminate(context.Background(), 0)
	}()
	waitTerminationOrder(t, containment.softCalled, "containment soft-stop was not entered")

	close(allowEOFRead)
	waitTerminationOrder(t, stdinEOF, "stdin EOF was not observed before soft-stop release")
	select {
	case err := <-stdinObservationErrors:
		t.Fatalf("stdin observer error = %v", err)
	default:
	}
	writeTimer := time.NewTimer(5 * time.Second)
	select {
	case err := <-writeResult:
		writeTimer.Stop()
		if !errors.Is(err, ErrInputClosed) {
			t.Fatalf("blocked WriteInput() error = %v, want ErrInputClosed", err)
		}
	case <-writeTimer.C:
		t.Fatal("blocked WriteInput() did not return before soft-stop release")
	}

	close(containment.softRelease)
	if err := <-terminateResult; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if err := process.Terminate(context.Background(), 0); err != nil {
		t.Fatalf("repeated Terminate() error = %v", err)
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("repeated FinalizeContainment() error = %v", err)
	}

	if got := containment.softCalls(); got != 1 {
		t.Fatalf("soft-stop calls = %d, want 1", got)
	}
	if got := containment.forceCalls(); got != 1 {
		t.Fatalf("force-stop calls = %d, want 1", got)
	}
	if got := containment.closeCalls(); got != 1 {
		t.Fatalf("containment close calls = %d, want 1", got)
	}
}

func observeTerminationTestStdin(file *os.File, firstByte, allowEOFRead, eof chan struct{}, observationErrors chan<- error) {
	defer file.Close()
	var buffer [1]byte
	if _, err := file.Read(buffer[:]); err != nil {
		observationErrors <- err
		return
	}
	close(firstByte)
	<-allowEOFRead
	for {
		_, err := file.Read(buffer[:])
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			close(eof)
			return
		}
		observationErrors <- err
		return
	}
}

func waitTerminationOrder(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatal(message)
	}
}

type terminationOrderContainment struct {
	softCalled  chan struct{}
	softRelease chan struct{}
	waitRelease chan struct{}
	softOnce    sync.Once
	forceOnce   sync.Once
	closeOnce   sync.Once
	mu          sync.Mutex
	softCount   int
	forceCount  int
	closeCount  int
}

func newTerminationOrderContainment() *terminationOrderContainment {
	return &terminationOrderContainment{
		softCalled:  make(chan struct{}),
		softRelease: make(chan struct{}),
		waitRelease: make(chan struct{}),
	}
}

func (containment *terminationOrderContainment) Terminate(force bool) error {
	containment.mu.Lock()
	if force {
		containment.forceCount++
	} else {
		containment.softCount++
	}
	containment.mu.Unlock()
	if !force {
		containment.softOnce.Do(func() { close(containment.softCalled) })
		<-containment.softRelease
		return nil
	}
	containment.forceOnce.Do(func() { close(containment.waitRelease) })
	return nil
}

func (containment *terminationOrderContainment) Close() error {
	containment.closeOnce.Do(func() {
		containment.mu.Lock()
		containment.closeCount++
		containment.mu.Unlock()
	})
	return nil
}

func (containment *terminationOrderContainment) softCalls() int {
	containment.mu.Lock()
	defer containment.mu.Unlock()
	return containment.softCount
}

func (containment *terminationOrderContainment) forceCalls() int {
	containment.mu.Lock()
	defer containment.mu.Unlock()
	return containment.forceCount
}

func (containment *terminationOrderContainment) closeCalls() int {
	containment.mu.Lock()
	defer containment.mu.Unlock()
	return containment.closeCount
}

var _ platform.Containment = (*terminationOrderContainment)(nil)
