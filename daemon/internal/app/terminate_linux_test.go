//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"golang.org/x/sys/unix"
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

func TestTerminatePersistedProcessProvesAbsentLeaderWithoutProcessSideEffects(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	originalFind := findPersistedProcess
	originalTerminate := terminatePersistedProcessGroup
	originalProof := provePersistedProcessGroupAbsent
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
		findPersistedProcess = originalFind
		terminatePersistedProcessGroup = originalTerminate
		provePersistedProcessGroupAbsent = originalProof
	})
	readPersistedProcessIdentity = func(pid int) (string, error) {
		if pid != 99 {
			t.Fatalf("identity pid = %d, want 99", pid)
		}
		return "", os.ErrNotExist
	}
	findPersistedProcess = func(int) (*os.Process, error) {
		t.Fatal("proven absent process group must not find a process handle")
		return nil, nil
	}
	terminatePersistedProcessGroup = func(context.Context, *os.Process, string) error {
		t.Fatal("proven absent process group must not signal")
		return nil
	}
	proofCalled := false
	provePersistedProcessGroupAbsent = func(ctx context.Context, pid int, identity string) error {
		proofCalled = true
		if ctx == nil || pid != 99 || identity != "linux:v2:expected" {
			t.Fatalf("absence proof input = ctx:%v pid:%d identity:%q", ctx, pid, identity)
		}
		return nil
	}

	err := terminatePersistedProcessWithContext(context.Background(), 99, "linux:v2:expected")
	if !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("terminatePersistedProcessWithContext() error = %v, want unresolved descendant stop", err)
	}
	if !proofCalled {
		t.Fatal("absent process leader did not invoke process-group absence proof")
	}
}

func TestTerminatePersistedProcessMapsAbsentProofFailuresAndAvoidsSideEffects(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "group exists", err: platform.ErrPersistedProcessGroupUnproven},
		{name: "other errno", err: unix.EPERM},
		{name: "context canceled", err: context.Canceled},
		{name: "context deadline", err: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalIdentity := readPersistedProcessIdentity
			originalFind := findPersistedProcess
			originalTerminate := terminatePersistedProcessGroup
			originalProof := provePersistedProcessGroupAbsent
			t.Cleanup(func() {
				readPersistedProcessIdentity = originalIdentity
				findPersistedProcess = originalFind
				terminatePersistedProcessGroup = originalTerminate
				provePersistedProcessGroupAbsent = originalProof
			})
			readPersistedProcessIdentity = func(int) (string, error) { return "", os.ErrNotExist }
			findPersistedProcess = func(int) (*os.Process, error) {
				t.Fatal("unproven absence must not find a process handle")
				return nil, nil
			}
			terminatePersistedProcessGroup = func(context.Context, *os.Process, string) error {
				t.Fatal("unproven absence must not signal")
				return nil
			}
			proofContext := context.Background()
			proofErr := test.err
			if errors.Is(test.err, context.Canceled) {
				var cancel context.CancelFunc
				proofContext, cancel = context.WithCancel(context.Background())
				cancel()
				proofErr = proofContext.Err()
			} else if errors.Is(test.err, context.DeadlineExceeded) {
				var cancel context.CancelFunc
				proofContext, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				proofErr = proofContext.Err()
			}
			provePersistedProcessGroupAbsent = func(ctx context.Context, pid int, identity string) error {
				if ctx != proofContext {
					t.Fatalf("proof context = %p, want %p", ctx, proofContext)
				}
				if pid != 99 || identity != "linux:v2:expected" {
					t.Fatalf("absence proof input = pid:%d identity:%q", pid, identity)
				}
				return proofErr
			}

			err := terminatePersistedProcessWithContext(proofContext, 99, "linux:v2:expected")
			if !errors.Is(err, errPersistedProcessStopUnproven) {
				t.Fatalf("terminatePersistedProcessWithContext() error = %v, want unproven stop", err)
			}
			if !errors.Is(err, test.err) {
				t.Fatalf("terminatePersistedProcessWithContext() error = %v, want underlying %v", err, test.err)
			}
		})
	}
}

func TestTerminatePersistedProcessRejectsChangedIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		read func(int) (string, error)
	}{
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

func TestReleasePersistedLinuxContainmentAuthorityRequiresHelperEndpoint(t *testing.T) {
	value := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		OwnerKind:          authority.OwnerKindLinuxHelper,
		OwnerContext:       "linux-helper:legacy",
		TargetPID:          99,
		TargetIdentity:     "linux:99:expected",
		PipeToken:          strings.Repeat("b", authority.TokenBytes*2),
		JobID:              strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID:      100,
		SupervisorIdentity: "linux:100:expected",
	}
	value.StopReceipt = &authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		OwnerKind:          value.OwnerKind,
		OwnerContext:       value.OwnerContext,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		ActiveProcesses:    0,
	}

	err := releasePersistedLinuxContainmentAuthority(value.TargetPID, value.TargetIdentity, &value)
	if !errors.Is(err, errPersistedProcessStopUnproven) || !strings.Contains(err.Error(), "recovery endpoint") {
		t.Fatalf("releasePersistedLinuxContainmentAuthority() error = %v, want missing recovery endpoint", err)
	}
}
