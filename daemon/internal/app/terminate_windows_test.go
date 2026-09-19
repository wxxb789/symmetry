//go:build windows

package app

import (
	"errors"
	"testing"
)

func TestTerminatePersistedProcessRejectsPIDIdentityMismatchAsUnproven(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "windows:99:new", nil }
	if err := terminatePersistedProcess(99, "windows:99:old"); !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("terminatePersistedProcess() error = %v, want unproven stop", err)
	}
}

func TestTerminatePersistedProcessDoesNotUsePIDOnlyStopAfterIdentityPrecheck(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "windows:99:old", nil }
	if err := terminatePersistedProcess(99, "windows:99:old"); !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("terminatePersistedProcess() error = %v, want unproven stop", err)
	}
}

func TestTerminatePersistedProcessFailsClosedWithoutVerifiedIdentity(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "", errors.New("unavailable") }
	if err := terminatePersistedProcess(99, "windows:99:old"); err == nil {
		t.Fatal("terminatePersistedProcess() succeeded without identity verification")
	}
}
