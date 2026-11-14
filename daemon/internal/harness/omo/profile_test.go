package omo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func writeRetainedHeader(t *testing.T, filename, id, workspace string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"type": "session", "version": 3, "id": id, "cwd": workspace, "timestamp": "2026-11-14T00:00:00.000Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProfileCannotOverrideIsolationOrSessionProvenance(t *testing.T) {
	directory := t.TempDir()
	valid := []string{"--provider", "relay", "--model", "recorded", "--thinking", "minimal", "--session-dir", directory, "--name", "bounded turn"}
	if err := ValidateRPCProfileArgs(valid); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"implicit directory":  {},
		"relative directory":  {"--session-dir", "relative"},
		"repeated directory":  {"--session-dir", directory, "--session-dir", directory},
		"permission override": {"--session-dir", directory, "--permission-preset", "full-access"},
		"extension override":  {"--session-dir", directory, "--extension", "untrusted.js"},
		"transport override":  {"--session-dir", directory, "--mode=rpc"},
		"retained override":   {"--session-dir", directory, "--session", "untrusted.jsonl"},
		"tool override":       {"--session-dir", directory, "--tools", "bash"},
		"tool exclusion":      {"--session-dir", directory, "--exclude-tools", "read"},
		"tool disable":        {"--session-dir", directory, "--no-tools"},
		"NUL":                 {"--session-dir", directory, "--name", "a\x00b"},
		"missing value":       {"--session-dir", directory, "--model"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateRPCProfileArgs(args); err == nil {
				t.Fatal("unsafe profile accepted")
			}
		})
	}
}

func TestResumeBindsHeaderWorkspaceAndContainedRegularFile(t *testing.T) {
	directory, workspace := t.TempDir(), t.TempDir()
	filename := filepath.Join(directory, "retained.jsonl")
	writeRetainedHeader(t, filename, "native-1", workspace)
	resume := harness.ResumeHandle{LocalHandleID: "local-1", NativeSessionID: "native-1", NativeSessionFilename: filename, WorkspaceFingerprint: "machine-worktree-fingerprint", NativeVersion: TestedVersion}
	args := []string{"--session-dir", directory}
	if state, err := resumeState(&resume, args, workspace); err != nil || state.SessionID != "native-1" {
		t.Fatalf("valid resume = %+v, %v", state, err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*harness.ResumeHandle)
	}{
		{"wrong ID", func(value *harness.ResumeHandle) { value.NativeSessionID = "other" }},
		{"missing fingerprint", func(value *harness.ResumeHandle) { value.WorkspaceFingerprint = "" }},
		{"unknown version", func(value *harness.ResumeHandle) { value.NativeVersion = "5.1.70" }},
		{"directory not file", func(value *harness.ResumeHandle) { value.NativeSessionFilename = directory }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			value := resume
			mutation.change(&value)
			if _, err := resumeState(&value, args, workspace); !errors.Is(err, ErrResumeRejected) {
				t.Fatalf("resume = %v", err)
			}
		})
	}
	if _, err := resumeState(&resume, args, t.TempDir()); !errors.Is(err, ErrResumeRejected) {
		t.Fatalf("other workspace = %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	writeRetainedHeader(t, outside, "native-1", workspace)
	resume.NativeSessionFilename = outside
	if _, err := resumeState(&resume, args, workspace); !errors.Is(err, ErrResumeRejected) {
		t.Fatalf("outside retained file = %v", err)
	}
	link := filepath.Join(directory, "escape.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	resume.NativeSessionFilename = link
	if _, err := resumeState(&resume, args, workspace); !errors.Is(err, ErrResumeRejected) {
		t.Fatalf("symlink escape = %v", err)
	}
}
