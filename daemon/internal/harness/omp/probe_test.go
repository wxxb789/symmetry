package omp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestProbeUnknownBlobPromotesNothing(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "omp-unknown.exe")
	if err := os.WriteFile(executable, []byte("unknown executable bytes"), 0700); err != nil {
		t.Fatal(err)
	}
	capabilities, err := NewAdapter(executable).Probe(context.Background())
	if !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("Probe error=%v", err)
	}
	if capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Handoff || capabilities.Verified || capabilities.TransportVerified || capabilities.VersionKnown {
		t.Fatalf("unknown blob promoted: %+v", capabilities)
	}
}

func TestObservedTransportDoesNotPromoteNativeOperations(t *testing.T) {
	capabilities := observedCapabilities()
	if err := capabilities.Validate(); err != nil {
		t.Fatal(err)
	}
	if !capabilities.VersionKnown || !capabilities.TransportVerified || capabilities.NativeVersion != TestedVersion || capabilities.ProtocolVersion != 2 || capabilities.ImplementationVersion != ImplementationVersion {
		t.Fatalf("lost observed facts: %+v", capabilities)
	}
	for _, operation := range []harness.Capability{harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel, harness.CapabilityResume, harness.CapabilityHandoff, harness.CapabilityGuidance, harness.CapabilityPause, harness.CapabilityApprovalResponse, harness.CapabilityUsage, harness.CapabilityHardCostLimit, harness.CapabilityProviderAccess} {
		if capabilities.Supports(operation) {
			t.Fatalf("unverified operation %s promoted", operation)
		}
	}
}

func TestRPCProfileArgumentsRejectAuthorityAndTransportOverrides(t *testing.T) {
	directory := t.TempDir()
	base := []string{"--session-dir", directory}
	for _, extra := range [][]string{{"--continue"}, {"--resume", "x"}, {"--mode", "rpc"}, {"--cwd", directory}, {"--api-key", "do-not-accept"}, {"--auto-approve"}, {"--approval-mode", "yolo"}, {"--extension", "x"}, {"--config", "x"}, {"--profile", "x"}, {"--tools", "read,task"}, {"--no-ui=true"}, {"--session-dir", directory}, {"positional prompt"}, {"@file"}} {
		args := append(append([]string(nil), base...), extra...)
		if err := ValidateRPCProfileArgs(args); err == nil {
			t.Fatalf("accepted unsafe selector %q", extra)
		}
	}
	if err := ValidateRPCProfileArgs([]string{"--model", "audited/model"}); err == nil {
		t.Fatal("missing explicit session-dir accepted")
	}
	args := append(base, "--model=audited/model", "--thinking", "auto", "--tools", "read,edit,write,bash", "--no-extensions", "--no-skills", "--no-rules", "--no-context-files", "--no-lsp", "--no-pty", "--no-prewalk", "--no-title", "--no-ui")
	if err := ValidateRPCProfileArgs(args); err != nil {
		t.Fatal(err)
	}
}

func TestResumeRequiresCompatibleVersionAndRetainedContainedFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := privateDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(directory, "retained.jsonl")
	if err := os.WriteFile(filename, []byte("native history"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("outside native history"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, resume := range []*harness.ResumeHandle{
		{NativeVersion: "unknown", NativeSessionID: "session", NativeSessionFilename: filename},
		{NativeVersion: TestedVersion, NativeSessionID: "session", NativeSessionFilename: filepath.Join(directory, "missing.jsonl")},
		{NativeVersion: TestedVersion, NativeSessionID: "session", NativeSessionFilename: outside},
		{NativeVersion: TestedVersion, NativeSessionFilename: filename},
	} {
		if _, err := resumeFilename(resume, directory); !errors.Is(err, ErrResumeRejected) {
			t.Fatalf("resume accepted or untyped failure: %v", err)
		}
	}
	if _, err := resumeFilename(&harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "session", NativeSessionFilename: filename}, directory); err != nil {
		t.Fatal(err)
	}
	t.Run("existing-inside-symlink-to-outside", func(t *testing.T) {
		link := filepath.Join(directory, "outside-link.jsonl")
		if err := os.Symlink(outside, link); err != nil {
			// ERROR_PRIVILEGE_NOT_HELD is the only supported-platform reason
			// to omit this case; Linux must exercise the actual symlink.
			if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skip("Windows account lacks symbolic-link privilege")
			}
			t.Fatal(err)
		}
		if data, err := os.ReadFile(link); err != nil || string(data) != "outside native history" {
			t.Fatalf("symlink does not reach the existing outside file: %v", err)
		}
		resume := &harness.ResumeHandle{NativeVersion: TestedVersion, NativeSessionID: "session", NativeSessionFilename: link}
		if _, err := resumeFilename(resume, directory); !errors.Is(err, ErrResumeRejected) {
			t.Fatalf("existing outside symlink accepted or untyped failure: %v", err)
		}
	})
}
