//go:build linux

package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	linuxPtraceHelperEnvironment = "SYMMETRY_LINUX_PTRACE_LAUNCH_HELPER"
	linuxPtraceHelperOutput      = "SYMMETRY_LINUX_PTRACE_LAUNCH_OUTPUT"
)

func TestLaunchLinuxPtraceStopsBeforeUserCodeAndCapturesFence(t *testing.T) {
	output := filepath.Join(t.TempDir(), "started")
	command := linuxPtraceHelperCommand(t, "write", output)
	process, err := LaunchLinuxPtrace(command)
	if err != nil {
		t.Fatalf("LaunchLinuxPtrace() error = %v", err)
	}
	t.Cleanup(func() {
		if _, waitErr := process.Wait(); waitErr != nil {
			t.Errorf("cleanup Wait() error = %v", waitErr)
		}
	})

	attributes := command.SysProcAttr
	if attributes == nil || !attributes.Setpgid || attributes.Pgid != 0 || attributes.Pdeathsig != syscall.SIGKILL || !attributes.Ptrace || attributes.PidFD == nil {
		t.Fatalf("SysProcAttr = %#v, want Setpgid/Pdeathsig SIGKILL/Ptrace/PidFD", attributes)
	}
	if process.pidfd < 0 {
		t.Fatalf("retained pidfd = %d, want non-negative", process.pidfd)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("helper output before Resume() error = %v, want os.ErrNotExist", err)
	}
	if _, err := process.Wait(); !errors.Is(err, errLinuxPtraceLaunchStopped) {
		t.Fatalf("Wait() before Resume() error = %v, want %v", err, errLinuxPtraceLaunchStopped)
	}

	if process.PID() <= 0 {
		t.Fatalf("PID() = %d, want positive", process.PID())
	}
	if process.Identity() == "" || !strings.HasPrefix(process.Identity(), "linux:v2:") {
		t.Fatalf("Identity() = %q, want strict Linux v2 identity", process.Identity())
	}
	anchor := process.ProcessGroupAnchor()
	if anchor.PID != process.PID() || anchor.PGRP != int64(process.PID()) || anchor.Session == int64(process.PID()) || anchor.StartTime == 0 {
		t.Fatalf("ProcessGroupAnchor() = %#v, want child group anchor", anchor)
	}

	if err := process.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := process.Resume(); !errors.Is(err, errLinuxPtraceLaunchResumed) {
		t.Fatalf("repeated Resume() error = %v, want %v", err, errLinuxPtraceLaunchResumed)
	}
	if code, err := process.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait() = (%d, %v), want (0, nil)", code, err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read helper output: %v", err)
	}
	if string(contents) != "ran" {
		t.Fatalf("helper output = %q, want %q", contents, "ran")
	}
}

func TestLinuxPtraceTerminateStopsExecStoppedGroup(t *testing.T) {
	output := filepath.Join(t.TempDir(), "started")
	command := linuxPtraceHelperCommand(t, "block", output)
	process, err := LaunchLinuxPtrace(command)
	if err != nil {
		t.Fatalf("LaunchLinuxPtrace() error = %v", err)
	}
	if err := process.Terminate(); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if err := process.Terminate(); err != nil {
		t.Fatalf("repeated Terminate() error = %v", err)
	}
	code, waitErr := process.Wait()
	if waitErr == nil || code == 0 {
		t.Fatalf("Wait() = (%d, %v), want non-zero killed-process result", code, waitErr)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("helper output after stopped termination error = %v, want os.ErrNotExist", err)
	}
}

func TestLaunchLinuxPtraceFailsClosedWhenPIDFDGroupSignalIsUnavailable(t *testing.T) {
	previous := probePIDFDGroupSupport
	probePIDFDGroupSupport = func() error { return errors.New("pidfd group signal unavailable") }
	t.Cleanup(func() { probePIDFDGroupSupport = previous })

	command := exec.Command("true")
	if _, err := LaunchLinuxPtrace(command); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("LaunchLinuxPtrace() error = %v, want errors.ErrUnsupported", err)
	}
	if command.Process != nil || command.SysProcAttr != nil {
		t.Fatalf("failed capability probe changed command: process=%v attrs=%#v", command.Process, command.SysProcAttr)
	}
}

func TestLaunchLinuxPtraceRejectsConflictingProcessGroupConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		attr syscall.SysProcAttr
	}{
		{name: "setsid", attr: syscall.SysProcAttr{Setsid: true}},
		{name: "foreground", attr: syscall.SysProcAttr{Foreground: true}},
		{name: "pgid", attr: syscall.SysProcAttr{Pgid: 1234}},
		{name: "pdeathsig", attr: syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command("true")
			command.SysProcAttr = &test.attr
			if _, err := LaunchLinuxPtrace(command); err == nil {
				t.Fatal("LaunchLinuxPtrace() error = nil, want conflicting configuration rejection")
			}
			if command.Process != nil {
				t.Fatal("conflicting configuration started a process")
			}
		})
	}
}

func TestLaunchLinuxPtraceCleansUpWhenExecStopIsUnexpected(t *testing.T) {
	previous := waitLinuxPtraceExecStop
	waitLinuxPtraceExecStop = func(int) (unix.WaitStatus, error) {
		return unix.WaitStatus(syscall.SIGSTOP<<8 | 0x7f), nil
	}
	t.Cleanup(func() { waitLinuxPtraceExecStop = previous })

	output := filepath.Join(t.TempDir(), "started")
	command := linuxPtraceHelperCommand(t, "write", output)
	if _, err := LaunchLinuxPtrace(command); err == nil || !strings.Contains(err.Error(), "unexpected wait status") {
		t.Fatalf("LaunchLinuxPtrace() error = %v, want unexpected-stop failure", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("helper output after unexpected stop error = %v, want os.ErrNotExist", err)
	}
}

func linuxPtraceHelperCommand(t *testing.T, mode, output string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxPtraceLaunchHelper$", "--", mode)
	command.Env = append(os.Environ(),
		linuxPtraceHelperEnvironment+"=1",
		linuxPtraceHelperOutput+"="+output,
	)
	return command
}

func TestLinuxPtraceLaunchHelper(t *testing.T) {
	if os.Getenv(linuxPtraceHelperEnvironment) != "1" {
		return
	}
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		os.Exit(2)
	}
	output := os.Getenv(linuxPtraceHelperOutput)
	switch os.Args[len(os.Args)-1] {
	case "write":
		if err := os.WriteFile(output, []byte("ran"), 0o600); err != nil {
			os.Exit(3)
		}
	case "block":
		select {}
	default:
		os.Exit(4)
	}
	os.Exit(0)
}
