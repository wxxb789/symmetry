//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// The witness report is the only durable artifact of a reached stage: a swallowed
// writer error would publish a healthy-looking invocation with missing evidence.

var recoveryReport = preAuthorityWitnessReport{Mode: "recover", Stage: "recover", Status: "already-clear"}

func TestWritePreAuthorityWitnessReportPublishesReportAtomically(t *testing.T) {
	directory := t.TempDir()
	resultFile := filepath.Join(directory, "witness.result.json")
	report := preAuthorityWitnessReport{
		Mode:               "start",
		Stage:              "complete",
		Status:             "completed",
		RunID:              "unit-report",
		Generation:         3,
		JobID:              "0123456789abcdef",
		PID:                42,
		HasProcessDetails:  true,
		HasContainment:     true,
		HasStopReceipt:     true,
		TargetPID:          42,
		TargetIdentity:     "identity",
		SupervisorPID:      41,
		SupervisorIdentity: "supervisor",
	}
	if err := writePreAuthorityWitnessReport(preAuthorityWitnessOptions{resultFile: resultFile}, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "witness.result.json" {
		t.Fatalf("result directory entries = %v, want only the published report", entries)
	}
	contents, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	var published preAuthorityWitnessReport
	if err := json.Unmarshal(contents, &published); err != nil {
		t.Fatalf("decode published report: %v", err)
	}
	if published != report {
		t.Fatalf("published report = %#v, want %#v", published, report)
	}
	wantKeys := []string{
		"mode", "stage", "status", "run_id", "generation", "job_id", "pid",
		"has_process_details", "has_containment", "has_pending_handoff", "has_stop_receipt",
		"target_pid", "target_identity", "supervisor_pid", "supervisor_identity",
	}
	if len(decoded) != len(wantKeys) {
		t.Fatalf("published report carries %d fields, want %d: %v", len(decoded), len(wantKeys), decoded)
	}
	for _, key := range wantKeys {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("published report misses field %q", key)
		}
	}
}

func TestWritePreAuthorityWitnessReportFailsOnBlockedResultDirectory(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(blocker, "witness.result.json")
	err := writePreAuthorityWitnessReport(preAuthorityWitnessOptions{resultFile: resultFile}, recoveryReport)
	if err == nil {
		t.Fatal("write report with a file as result directory returned success; want the result directory failure")
	}
	if !strings.Contains(err.Error(), "result directory") {
		t.Fatalf("write report error = %v, want the failed result directory step", err)
	}
	if _, statErr := os.Stat(resultFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("result file stat error = %v, want no report", statErr)
	}
	assertNoPreAuthorityReportTemporaries(t, root)
}

func TestWritePreAuthorityWitnessReportFailsToPublishOverDirectory(t *testing.T) {
	root := t.TempDir()
	resultFile := filepath.Join(root, "witness.result.json")
	if err := os.Mkdir(resultFile, 0o700); err != nil {
		t.Fatal(err)
	}
	err := writePreAuthorityWitnessReport(preAuthorityWitnessOptions{resultFile: resultFile}, recoveryReport)
	if err == nil {
		t.Fatal("write report over an existing directory returned success; want the publish failure")
	}
	if !strings.Contains(err.Error(), "publish") {
		t.Fatalf("write report error = %v, want the failed publish step", err)
	}
	if info, statErr := os.Stat(resultFile); statErr != nil || !info.IsDir() {
		t.Fatalf("result path stat = %v/%v, want the untouched directory", info, statErr)
	}
	assertNoPreAuthorityReportTemporaries(t, root)
}

func TestWritePreAuthorityWitnessReportSkipsEmptyResultFile(t *testing.T) {
	if err := writePreAuthorityWitnessReport(preAuthorityWitnessOptions{}, recoveryReport); err != nil {
		t.Fatalf("write report without a result file: %v", err)
	}
}

func TestRunPreAuthorityWitnessRecoveryPropagatesReportFailure(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	key := state.RunKey{RunID: "unit-preauthority-report", Generation: 1}
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensurePreAuthorityWitnessJournal(store, key, stateDir); err != nil {
		t.Fatalf("seed witness journal: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(blocker, "witness.result.json")
	err = runPreAuthorityWitness([]string{
		"-mode", "recover",
		"-state-dir", stateDir,
		"-run-id", key.RunID,
		"-generation", "1",
		"-result-file", blocked,
	})
	if err == nil {
		t.Fatal("recovery with an unwritable report path returned success; a missing report must not exit 0")
	}
	if !strings.Contains(err.Error(), "result directory") {
		t.Fatalf("recovery report error = %v, want the failed result directory step", err)
	}
	if _, statErr := os.Stat(blocked); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("blocked report stat error = %v, want no report", statErr)
	}

	resultFile := filepath.Join(root, "witness.result.json")
	if err := runPreAuthorityWitness([]string{
		"-mode", "recover",
		"-state-dir", stateDir,
		"-run-id", key.RunID,
		"-generation", "1",
		"-result-file", resultFile,
	}); err != nil {
		t.Fatalf("recovery with a writable report path: %v", err)
	}
	contents, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("read recovery report: %v", err)
	}
	var published preAuthorityWitnessReport
	if err := json.Unmarshal(contents, &published); err != nil {
		t.Fatalf("decode recovery report: %v", err)
	}
	if published.Mode != "recover" || published.Stage != "recover" || published.Status != "already-clear" ||
		published.RunID != key.RunID || published.Generation != key.Generation {
		t.Fatalf("recovery report = %#v, want recover/already-clear for %s generation %d", published, key.RunID, key.Generation)
	}
}

func TestPreAuthorityWitnessCommandExitsOneWhenReportMissing(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	key := state.RunKey{RunID: "unit-preauthority-exit", Generation: 1}
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensurePreAuthorityWitnessJournal(store, key, stateDir); err != nil {
		t.Fatalf("seed witness journal: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	// init() maps the entrypoint error to exit 1, which only a real process can
	// observe, so run this test binary through the entrypoint.
	command := exec.Command(os.Args[0],
		"-preauthority-witness",
		"-mode", "recover",
		"-state-dir", stateDir,
		"-run-id", key.RunID,
		"-generation", "1",
		"-result-file", filepath.Join(blocker, "witness.result.json"),
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("witness run with an unwritable report path exited 0; a missing report must not exit 0: %s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("witness run error = %v with output %q, want exit status 1", err, output)
	}
	if !strings.Contains(string(output), "result directory") {
		t.Fatalf("witness run output = %q, want the failed result directory step", output)
	}
}

func assertNoPreAuthorityReportTemporaries(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".preauthority-report-") {
			t.Fatalf("temporary report file %q survived a failed write", entry.Name())
		}
	}
}
