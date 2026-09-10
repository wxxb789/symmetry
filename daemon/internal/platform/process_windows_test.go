//go:build windows

package platform

import (
	"errors"
	"os/exec"
	"testing"
)

func TestConfigureProcessRequestsBreakawayFromInheritedJob(t *testing.T) {
	command := exec.Command("example.exe")
	ConfigureProcess(command)

	if command.SysProcAttr == nil {
		t.Fatal("ConfigureProcess() did not configure SysProcAttr")
	}
	if command.SysProcAttr.CreationFlags&createBreakawayFromJob == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_BREAKAWAY_FROM_JOB", command.SysProcAttr.CreationFlags)
	}
}

func TestCleanupFailedAttachTerminatesWholeProcessTree(t *testing.T) {
	want := errors.New("tree termination failed")
	calledPID := 0
	previous := terminateProcessTreeForAttachFailure
	terminateProcessTreeForAttachFailure = func(pid int) error {
		calledPID = pid
		return want
	}
	t.Cleanup(func() { terminateProcessTreeForAttachFailure = previous })

	attachErr := errors.New("assign failed")
	_, err := cleanupFailedAttach(1234, 0, attachErr)
	if calledPID != 1234 {
		t.Fatalf("tree termination PID = %d, want 1234", calledPID)
	}
	if !errors.Is(err, want) || !errors.Is(err, attachErr) {
		t.Fatalf("cleanupFailedAttach() error = %v, want attach and tree errors", err)
	}
}
