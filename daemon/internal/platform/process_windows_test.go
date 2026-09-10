//go:build windows

package platform

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureProcessDoesNotRequireBreakawayFromInheritedJob(t *testing.T) {
	command := exec.Command("example.exe")
	ConfigureProcess(command)

	if command.SysProcAttr != nil {
		t.Fatalf("ConfigureProcess() changed SysProcAttr = %#v", command.SysProcAttr)
	}

	attributes := &syscall.SysProcAttr{CreationFlags: 0x00000200}
	command.SysProcAttr = attributes
	ConfigureProcess(command)
	if command.SysProcAttr != attributes || command.SysProcAttr.CreationFlags != 0x00000200 {
		t.Fatalf("ConfigureProcess() changed existing SysProcAttr = %#v", command.SysProcAttr)
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
