//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestTerminatePersistedProcessUsesIdentityBoundPlatformTerminator(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	originalFind := findPersistedProcess
	originalTerminate := terminatePersistedProcessGroup
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
		findPersistedProcess = originalFind
		terminatePersistedProcessGroup = originalTerminate
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "linux:99:expected", nil }
	findPersistedProcess = func(pid int) (*os.Process, error) { return &os.Process{Pid: pid}, nil }
	called := false
	terminatePersistedProcessGroup = func(ctx context.Context, process *os.Process, identity string) error {
		if ctx == nil || process == nil || process.Pid != 99 || identity != "linux:99:expected" {
			t.Fatalf("terminator input = ctx:%v process:%#v identity:%q", ctx, process, identity)
		}
		called = true
		return nil
	}

	if err := terminatePersistedProcess(99, "linux:99:expected"); err != nil {
		t.Fatalf("terminatePersistedProcess() error = %v", err)
	}
	if !called {
		t.Fatal("matching identity did not use the platform terminator")
	}
}

func TestTerminatePersistedProcessRejectsAbsentOrChangedIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		read func(int) (string, error)
	}{
		{name: "absent", read: func(int) (string, error) { return "", os.ErrNotExist }},
		{name: "changed", read: func(int) (string, error) { return "linux:99:new", nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalIdentity := readPersistedProcessIdentity
			originalFind := findPersistedProcess
			originalTerminate := terminatePersistedProcessGroup
			t.Cleanup(func() {
				readPersistedProcessIdentity = originalIdentity
				findPersistedProcess = originalFind
				terminatePersistedProcessGroup = originalTerminate
			})
			readPersistedProcessIdentity = test.read
			findPersistedProcess = func(int) (*os.Process, error) {
				t.Fatal("unverified identity must not find a process handle")
				return nil, nil
			}
			terminatePersistedProcessGroup = func(context.Context, *os.Process, string) error {
				t.Fatal("unverified identity must not terminate a process group")
				return nil
			}

			err := terminatePersistedProcess(99, "linux:99:old")
			if !errors.Is(err, errPersistedProcessStopUnproven) {
				t.Fatalf("terminatePersistedProcess() error = %v, want unproven stop", err)
			}
		})
	}
}

func TestTerminatePersistedProcessFailsClosedWhenPlatformCannotProveExit(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	originalFind := findPersistedProcess
	originalTerminate := terminatePersistedProcessGroup
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
		findPersistedProcess = originalFind
		terminatePersistedProcessGroup = originalTerminate
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "linux:99:expected", nil }
	findPersistedProcess = func(pid int) (*os.Process, error) { return &os.Process{Pid: pid}, nil }
	terminatePersistedProcessGroup = func(context.Context, *os.Process, string) error { return errors.New("group remains non-empty") }

	err := terminatePersistedProcess(99, "linux:99:expected")
	if !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("terminatePersistedProcess() error = %v, want unproven stop", err)
	}
}
