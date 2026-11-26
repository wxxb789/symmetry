//go:build linux && amd64

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxV2CleanupApprovedWorkerProgram(t *testing.T) {
	resolvedWorkerShell, err := linuxV2CleanupApprovedWorkerProgram(linuxV2CleanupWorkerShell)
	if err != nil {
		t.Fatalf("the fixture's controlled B worker shell %s must resolve to an approved image: %v", linuxV2CleanupWorkerShell, err)
	}
	t.Logf("controlled B worker shell %s resolves to the approved image %s", linuxV2CleanupWorkerShell, resolvedWorkerShell)
	staging := t.TempDir()
	resolvedStaging, err := filepath.EvalSymlinks(staging)
	if err != nil {
		t.Fatal(err)
	}
	realDirectory := filepath.Join(staging, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(realDirectory, "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedShell := filepath.Join(resolvedStaging, "real", "shell")
	linkedDirectory := filepath.Join(staging, "linked")
	if err := os.Symlink("real", linkedDirectory); err != nil {
		t.Fatal(err)
	}
	linkedShell := filepath.Join(staging, "linked-shell")
	if err := os.Symlink(filepath.Join("real", "shell"), linkedShell); err != nil {
		t.Fatal(err)
	}
	for _, accepted := range []string{shell, filepath.Join(linkedDirectory, "shell"), linkedShell} {
		resolved, err := linuxV2CleanupApprovedWorkerProgram(accepted)
		if err != nil || resolved != resolvedShell {
			t.Fatalf("approved worker program for %q = %q, err=%v, want %q", accepted, resolved, err, resolvedShell)
		}
	}
	notExecutable := filepath.Join(realDirectory, "not-executable")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setIDShell := filepath.Join(realDirectory, "setid-shell")
	if err := os.WriteFile(setIDShell, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(setIDShell, 0o755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	worldWritableShell := filepath.Join(realDirectory, "world-writable-shell")
	if err := os.WriteFile(worldWritableShell, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(worldWritableShell, 0o777); err != nil {
		t.Fatal(err)
	}
	missingShell := filepath.Join(realDirectory, "missing-shell")
	directoryShell := filepath.Join(realDirectory, "directory-shell")
	if err := os.Mkdir(directoryShell, 0o755); err != nil {
		t.Fatal(err)
	}
	brokenShell := filepath.Join(staging, "broken-shell")
	if err := os.Symlink(filepath.Join("real", "absent"), brokenShell); err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct {
		path string
		want string
	}{
		{path: missingShell, want: "resolve controlled B worker shell"},
		{path: brokenShell, want: "resolve controlled B worker shell"},
		{path: "bin/sh", want: "not a canonical absolute path"},
		{path: "/", want: "not a canonical absolute path"},
		{path: notExecutable, want: "is not a plain executable file"},
		{path: setIDShell, want: "is not a plain executable file"},
		{path: worldWritableShell, want: "is not a plain executable file"},
		{path: directoryShell, want: "is not a plain executable file"},
	} {
		resolved, err := linuxV2CleanupApprovedWorkerProgram(refused.path)
		if err == nil || resolved != "" {
			t.Fatalf("worker program for %q = %q, err=%v, want a refusal", refused.path, resolved, err)
		}
		if !strings.Contains(err.Error(), refused.want) {
			t.Fatalf("worker program refusal for %q = %q, want detail %q", refused.path, err, refused.want)
		}
	}
}
