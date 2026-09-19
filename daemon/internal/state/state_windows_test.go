//go:build windows

package state

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWindowsGoalSessionPathsUseProtectedAccountOnlyDACL(t *testing.T) {
	store := mustStore(t)
	intent := testGoalSessionIntent()
	saved, err := store.SaveGoalSessionLaunchIntent(intent)
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}

	for _, path := range []string{
		store.goalSessionsDir(),
		store.goalSessionPath(saved.Key()),
		store.goalSessionLineagePath(saved.LineageKey()),
	} {
		if err := verifyWindowsPrivatePath(path); err != nil {
			t.Fatalf("verifyWindowsPrivatePath(%q) error = %v", path, err)
		}
	}
}

func TestWindowsWriteAtomicRetriesAfterReaderHandleReleases(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	oldData := []byte(`{"version":1,"state":"old"}`)
	newData := []byte(`{"version":1,"state":"new"}`)
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	reader := openWindowsRenameReader(t, path)
	readerOpen := true
	t.Cleanup(func() {
		if readerOpen {
			_ = syscall.CloseHandle(reader)
		}
	})

	originalRename := renameStateFileOS
	firstFailure := make(chan struct{}, 1)
	renameStateFileOS = func(source, destination string) error {
		err := os.Rename(source, destination)
		if err != nil && isRetryableStateRenameError(err) {
			select {
			case firstFailure <- struct{}{}:
			default:
			}
		}
		return err
	}
	t.Cleanup(func() { renameStateFileOS = originalRename })

	done := make(chan error, 1)
	go func() { done <- writeAtomic(path, newData) }()
	select {
	case <-firstFailure:
	case err := <-done:
		if err == nil {
			t.Fatal("writeAtomic() succeeded while destination reader handle was held")
		}
		t.Fatalf("writeAtomic() returned before observing the blocked rename: %v", err)
	case <-time.After(time.Second):
		if err := syscall.CloseHandle(reader); err != nil {
			readerOpen = false
			t.Fatalf("CloseHandle(reader) after writer timeout = %v", err)
		}
		readerOpen = false
		select {
		case err := <-done:
			t.Fatalf("writeAtomic() did not attempt the blocked rename: returned %v after release", err)
		case <-time.After(time.Second):
			t.Fatal("writeAtomic() did not attempt the blocked rename")
		}
	}

	if err := syscall.CloseHandle(reader); err != nil {
		t.Fatalf("CloseHandle(reader) error = %v", err)
	}
	readerOpen = false
	if err := <-done; err != nil {
		t.Fatalf("writeAtomic() error after reader release = %v", err)
	}
	assertWindowsAtomicContent(t, path, newData)
	assertNoWindowsOwnedTemps(t, directory)
}

func TestWindowsWriteAtomicBoundedFailurePreservesDestinationAndCleansTemp(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	oldData := []byte(`{"version":1,"state":"old"}`)
	newData := []byte(`{"version":1,"state":"new"}`)
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	reader := openWindowsRenameReader(t, path)
	readerOpen := true
	t.Cleanup(func() {
		if readerOpen {
			_ = syscall.CloseHandle(reader)
		}
	})

	originalRename := renameStateFileOS
	var firstErr error
	var attempts atomic.Int32
	renameStateFileOS = func(source, destination string) error {
		attempts.Add(1)
		err := os.Rename(source, destination)
		if firstErr == nil {
			firstErr = err
		}
		return err
	}
	t.Cleanup(func() { renameStateFileOS = originalRename })

	started := time.Now()
	err := writeAtomic(path, newData)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("writeAtomic() succeeded while destination reader handle was held")
	}
	if firstErr == nil || err != firstErr {
		t.Fatalf("writeAtomic() error = %v, first rename error = %v; want original rename failure", err, firstErr)
	}
	if !isRetryableStateRenameError(err) {
		t.Fatalf("writeAtomic() error = %v, want a retryable Windows rename error", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("rename attempts = %d, want bounded retry", attempts.Load())
	}
	if elapsed > 750*time.Millisecond {
		t.Fatalf("writeAtomic() elapsed = %s, want bounded failure near 250ms", elapsed)
	}

	if err := syscall.CloseHandle(reader); err != nil {
		t.Fatalf("CloseHandle(reader) error = %v", err)
	}
	readerOpen = false
	assertWindowsAtomicContent(t, path, oldData)
	assertNoWindowsOwnedTemps(t, directory)
	if err := writeAtomic(path, newData); err != nil {
		t.Fatalf("writeAtomic() after reader release = %v", err)
	}
	assertWindowsAtomicContent(t, path, newData)
	assertNoWindowsOwnedTemps(t, directory)
}

func TestWindowsRenameErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrapped access denied", err: fmt.Errorf("rename: %w", syscall.ERROR_ACCESS_DENIED), want: true},
		{name: "wrapped sharing violation", err: fmt.Errorf("rename: %w", stateWindowsSharingViolation), want: true},
		{name: "permanent error", err: fmt.Errorf("rename: %w", syscall.ERROR_FILE_NOT_FOUND), want: false},
		{name: "message only", err: errors.New("sharing violation"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isRetryableStateRenameError(test.err); got != test.want {
				t.Fatalf("isRetryableStateRenameError() = %t, want %t for %v", got, test.want, test.err)
			}
		})
	}
}

func TestWindowsRenameDoesNotRetryPermanentFailure(t *testing.T) {
	want := fmt.Errorf("rename: %w", syscall.ERROR_FILE_NOT_FOUND)
	var calls atomic.Int32
	err := renameStateFileWith(func(string, string) error {
		calls.Add(1)
		return want
	}, "source", "destination")
	if err != want {
		t.Fatalf("renameStateFileWith() error = %v, want original error %v", err, want)
	}
	if calls.Load() != 1 {
		t.Fatalf("rename calls = %d, want 1 for permanent error", calls.Load())
	}
}

func openWindowsRenameReader(t *testing.T, path string) syscall.Handle {
	t.Helper()
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_READ,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("CreateFile(%q) error = %v", path, err)
	}
	return handle
}

func assertWindowsAtomicContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, got, want)
	}
}

func assertNoWindowsOwnedTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", directory, err)
	}
	for _, entry := range entries {
		if isOwnedTemp(entry.Name()) {
			t.Fatalf("owned temporary file %q remains", entry.Name())
		}
	}
}
