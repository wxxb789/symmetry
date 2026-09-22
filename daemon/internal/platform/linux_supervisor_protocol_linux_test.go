//go:build linux

package platform

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLinuxSupervisorFrameRejectsUnknownAndTrailingData(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(`{"version":1,"operation":"hello","unexpected":true}` + "\n"))
	var request linuxSupervisorRequest
	if err := readLinuxSupervisorFrame(reader, &request); err == nil {
		t.Fatal("readLinuxSupervisorFrame accepted unknown field")
	}

	reader = bufio.NewReader(strings.NewReader(`{"version":1,"operation":"hello"} {"version":1}` + "\n"))
	if err := readLinuxSupervisorFrame(reader, &request); err == nil {
		t.Fatal("readLinuxSupervisorFrame accepted trailing JSON")
	}
}

func TestLinuxSupervisorFrameBoundsAndSequence(t *testing.T) {
	if err := writeLinuxSupervisorFrame(discardWriter{}, strings.Repeat("x", linuxSupervisorMaxFrame)); !errors.Is(err, errLinuxSupervisorFrameTooLarge) {
		t.Fatalf("writeLinuxSupervisorFrame() error = %v, want frame-too-large", err)
	}
	fence := &linuxSupervisorSequenceFence{}
	if err := fence.accept(1); err != nil {
		t.Fatalf("accept(1) error = %v", err)
	}
	if err := fence.accept(1); !errors.Is(err, errLinuxSupervisorSequence) {
		t.Fatalf("accept(replay) error = %v", err)
	}
	if err := fence.accept(2); err != nil {
		t.Fatalf("accept(2) error = %v", err)
	}
}

func TestLinuxSupervisorOwnerWatchdogDistinguishesDisarmAndEOF(t *testing.T) {
	readFD, writeFD, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	watchdog := newLinuxSupervisorOwnerWatchdog(readFD)
	watchdog.disarm()
	select {
	case <-watchdog.ownerLost():
		t.Fatal("intentional disarm signaled owner loss")
	default:
	}
	_ = writeFD.Close()

	readFD2, writeFD2, err := os.Pipe()
	if err != nil {
		t.Fatalf("second os.Pipe() error = %v", err)
	}
	watchdog2 := newLinuxSupervisorOwnerWatchdog(readFD2)
	_ = writeFD2.Close()
	select {
	case <-watchdog2.ownerLost():
	case <-time.After(time.Second):
		t.Fatal("EOF did not signal owner loss")
	}
	watchdog2.disarm()
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
