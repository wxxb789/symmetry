//go:build linux

package platform

import (
	"bufio"
	"errors"
	"io"
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

func TestLinuxSupervisorFrameBounds(t *testing.T) {
	if err := writeLinuxSupervisorFrame(io.Discard, strings.Repeat("x", linuxSupervisorMaxFrame)); !errors.Is(err, errLinuxSupervisorFrameTooLarge) {
		t.Fatalf("writeLinuxSupervisorFrame() error = %v, want frame-too-large", err)
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
