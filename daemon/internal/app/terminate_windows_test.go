//go:build windows

package app

import (
	"errors"
	"testing"
)

func TestTerminatePersistedProcessSkipsPIDIdentityMismatch(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	originalKill := killPersistedProcessTree
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
		killPersistedProcessTree = originalKill
	})
	readPersistedProcessIdentity = func(int) (string, error) { return "windows:99:new", nil }
	killed := false
	killPersistedProcessTree = func(int) error { killed = true; return nil }
	if err := terminatePersistedProcess(99, "windows:99:old"); err != nil {
		t.Fatalf("terminatePersistedProcess() error = %v", err)
	}
	if killed {
		t.Fatal("PID identity mismatch invoked taskkill")
	}
}

func TestTerminatePersistedProcessFailsClosedWithoutVerifiedIdentity(t *testing.T) {
	originalIdentity := readPersistedProcessIdentity
	originalKill := killPersistedProcessTree
	t.Cleanup(func() {
		readPersistedProcessIdentity = originalIdentity
		killPersistedProcessTree = originalKill
	})
	killed := false
	killPersistedProcessTree = func(int) error { killed = true; return nil }
	readPersistedProcessIdentity = func(int) (string, error) { return "", errors.New("unavailable") }
	if err := terminatePersistedProcess(99, "windows:99:old"); err == nil {
		t.Fatal("terminatePersistedProcess() succeeded without identity verification")
	}
	if killed {
		t.Fatal("unverified PID invoked taskkill")
	}
}
