//go:build linux

package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

const (
	linuxProductionContainmentWitnessEnvironment     = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxProductionContainmentWitnessOutputDirectory = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS_OUTPUT_DIR"
	linuxCommitResumeBarrierReachedEnvironment       = "SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_REACHED_FILE"
	linuxCommitResumeBarrierReleaseEnvironment       = "SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_RELEASE_FILE"
	linuxLeaseArmedResumeBarrierReachedEnvironment   = "SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE"
	linuxLeaseArmedResumeBarrierReleaseEnvironment   = "SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_RELEASE_FILE"
	linuxSupervisorScanFailureModeEnvironment        = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE"
	linuxSupervisorScanFailureTriggerEnvironment     = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH"
	linuxSupervisorScanFailureFiredEnvironment       = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH"
	linuxSupervisorScanPendingReleaseEnvironment     = "SYMMETRY_LINUX_SUPERVISOR_SCAN_PENDING_RELEASE_PATH"
	linuxSupervisorScanSealAfterCleanMode            = "seal_after_clean_scan"
	linuxSupervisorScanSealOwnerEnvironment          = "SYMMETRY_LINUX_SUPERVISOR_SCAN_SEAL_OWNER"
	linuxStopReceiptBarrierReachedEnvironment        = "SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_REACHED_FILE"
	linuxStopReceiptBarrierReleaseEnvironment        = "SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_RELEASE_FILE"
)

// TestProductionLinuxContainmentWitnessSpine is an opt-in production-binary
// witness for Linux owner and resume transitions most likely to orphan or
// prematurely execute a native tree. It deliberately starts the daemon
// executable built from ./cmd/symmetry-daemon; the normal Linux launcher
// therefore dispatches the same executable through its default
// -containment-supervisor mode.
func TestProductionLinuxContainmentWitnessSpine(t *testing.T) {
	if os.Getenv(linuxProductionContainmentWitnessEnvironment) != "1" {
		t.Skip("set " + linuxProductionContainmentWitnessEnvironment + "=1 to run the Linux production containment witness")
	}

	environment := loadControlEnvironment(t)
	t.Run("task_submit_lost_response_idempotent_replay", func(t *testing.T) {
		runLinuxWitnessSubmitReplayIntegration(t, environment)
	})
	runRoot := linuxWitnessRunRoot(t)
	buildRoot := linuxWitnessCaseRoot(runRoot, "artifacts")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		t.Fatalf("create Linux witness artifact root: %v", err)
	}
	daemonBinary := buildProductionDaemonWitness(t, buildRoot)
	metadata := newLinuxWitnessMetadata(t, daemonBinary)
	writeLinuxWitnessProvenance(t, runRoot, "spine", metadata)

	t.Run("daemon_sigkill_bind_before_commit", func(t *testing.T) {
		runLinuxBindBeforeCommitWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "bind-before-commit"))
	})
	t.Run("daemon_sigkill_prepare_before_bind", func(t *testing.T) {
		runLinuxPrepareBeforeBindWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "prepare-before-bind"))
	})
	t.Run("daemon_sigkill_after_commit_before_resume", func(t *testing.T) {
		runLinuxDaemonCrashAfterCommitBeforeResumeWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "commit-before-resume"))
	})
	t.Run("daemon_sigkill_after_lease_arm_before_resume", func(t *testing.T) {
		runLinuxDaemonCrashAfterLeaseArmBeforeResumeWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "lease-arm-before-resume"))
	})
	t.Run("daemon_sigkill_after_resume_with_unsealed_mirror", func(t *testing.T) {
		runLinuxDaemonCrashAfterResumeUnsealedMirrorWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "daemon-after-resume"))
	})
	t.Run("daemon_sigkill_after_resume_at_both_scanner_clean_seal_boundary", func(t *testing.T) {
		runLinuxDaemonCrashAfterResumeAtCleanSealBoundaryWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "daemon-after-resume-clean-seal-boundary"))
	})
	t.Run("helper_sigkill_mirror_takeover", func(t *testing.T) {
		runLinuxHelperCrashMirrorWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "helper-mirror"))
	})
}

type linuxWitnessMetadata struct {
	Subject       string `json:"subject"`
	Tree          string `json:"tree"`
	TreeSHA256    string `json:"tree_sha256"`
	GoVersion     string `json:"go_version"`
	KernelRelease string `json:"kernel_release"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	SourcePath    string `json:"source_path"`
	SourceSHA256  string `json:"source_sha256"`
	BinaryPath    string `json:"binary_path"`
	BinarySHA256  string `json:"binary_sha256"`
}

type linuxWitnessEvidence struct {
	Version                  int                                     `json:"version"`
	Case                     string                                  `json:"case"`
	Stage                    string                                  `json:"stage"`
	Metadata                 linuxWitnessMetadata                    `json:"metadata"`
	CaseRoot                 string                                  `json:"case_root"`
	TaskID                   string                                  `json:"task_id,omitempty"`
	RunID                    string                                  `json:"run_id,omitempty"`
	Generation               int64                                   `json:"generation,omitempty"`
	JournalKey               string                                  `json:"journal_key,omitempty"`
	BarrierReachedPath       string                                  `json:"barrier_reached_path,omitempty"`
	BarrierReleasePath       string                                  `json:"barrier_release_path,omitempty"`
	BarrierReachedContents   string                                  `json:"barrier_reached_contents,omitempty"`
	BarrierReleaseContents   string                                  `json:"barrier_release_contents,omitempty"`
	ScanTriggerPath          string                                  `json:"scan_trigger_path,omitempty"`
	ScanReachedPath          string                                  `json:"scan_reached_path,omitempty"`
	ScanHelperReached        string                                  `json:"scan_helper_reached,omitempty"`
	ScanMirrorReached        string                                  `json:"scan_mirror_reached,omitempty"`
	ScanReleasePath          string                                  `json:"scan_release_path,omitempty"`
	ScanReleaseContents      string                                  `json:"scan_release_contents,omitempty"`
	StopReceiptReachedPath   string                                  `json:"stop_receipt_reached_path,omitempty"`
	StopReceiptReached       *linuxWitnessStopReceiptBarrierSnapshot `json:"stop_receipt_reached,omitempty"`
	StopReceiptReleasePath   string                                  `json:"stop_receipt_release_path,omitempty"`
	StopReceiptRelease       string                                  `json:"stop_receipt_release,omitempty"`
	Crash                    *linuxWitnessExitStatus                 `json:"crash,omitempty"`
	CrashAfterStopReceipt    *linuxWitnessExitStatus                 `json:"crash_after_stop_receipt,omitempty"`
	StopReceiptBeforeRelease *linuxReplayReceiptSnapshot             `json:"stop_receipt_before_release,omitempty"`
	JournalSnapshots         []linuxWitnessJournalSnapshot           `json:"journal_snapshots"`
	ProcessSnapshots         []linuxWitnessProcessSnapshot           `json:"process_snapshots"`
	RawOutput                map[string]string                       `json:"raw_output,omitempty"`
	Notes                    []string                                `json:"notes,omitempty"`
}

type linuxWitnessExitStatus struct {
	RequestedSignal string `json:"requested_signal,omitempty"`
	Signaled        bool   `json:"signaled"`
	Signal          string `json:"signal,omitempty"`
	ExitCode        int    `json:"exit_code"`
	Error           string `json:"error,omitempty"`
}

type linuxWitnessJournalSnapshot struct {
	At                  time.Time                   `json:"at"`
	Path                string                      `json:"path"`
	RawSHA256           string                      `json:"raw_sha256"`
	RunID               string                      `json:"run_id"`
	Generation          int64                       `json:"generation"`
	LocalState          string                      `json:"local_state"`
	TerminalState       string                      `json:"terminal_state,omitempty"`
	TerminalVerdict     string                      `json:"terminal_verdict,omitempty"`
	PID                 int                         `json:"pid,omitempty"`
	ProcessIdentity     string                      `json:"process_identity,omitempty"`
	StartedAt           time.Time                   `json:"started_at,omitempty"`
	LeaseExpiresAt      time.Time                   `json:"lease_expires_at,omitempty"`
	HasProcessDetails   bool                        `json:"has_process_details"`
	ContainmentUnproven bool                        `json:"containment_unproven"`
	RetainWorkspace     bool                        `json:"retain_workspace"`
	PendingEvents       int                         `json:"pending_events"`
	PendingTransitions  int                         `json:"pending_transitions"`
	ContainmentScans    *linuxWitnessScanSnapshot   `json:"containment_scans,omitempty"`
	StopReceipt         *linuxReplayReceiptSnapshot `json:"stop_receipt,omitempty"`
	Handoff             *linuxWitnessOwnerSnapshot  `json:"handoff,omitempty"`
	Authority           *linuxWitnessOwnerSnapshot  `json:"authority,omitempty"`
}

type linuxWitnessScanProgressSnapshot struct {
	PendingSequence     uint64 `json:"pending_sequence,omitempty"`
	CleanResultSequence uint64 `json:"clean_result_sequence,omitempty"`
	CompletedSequence   uint64 `json:"completed_sequence,omitempty"`
	SealedSequence      uint64 `json:"sealed_sequence,omitempty"`
	UnprovenSequence    uint64 `json:"unproven_sequence,omitempty"`
	FailureKind         string `json:"failure_kind,omitempty"`
}

type linuxWitnessScanSnapshot struct {
	TargetPID          int                              `json:"target_pid"`
	TargetIdentity     string                           `json:"target_identity"`
	SupervisorPID      int                              `json:"supervisor_pid"`
	SupervisorIdentity string                           `json:"supervisor_identity"`
	Helper             linuxWitnessScanProgressSnapshot `json:"helper"`
	Mirror             linuxWitnessScanProgressSnapshot `json:"mirror"`
}

func linuxWitnessScanProgressSnapshotFromState(progress state.ContainmentScanProgress) linuxWitnessScanProgressSnapshot {
	return linuxWitnessScanProgressSnapshot{
		PendingSequence:     progress.PendingSequence,
		CleanResultSequence: progress.CleanResultSequence,
		CompletedSequence:   progress.CompletedSequence,
		SealedSequence:      progress.SealedSequence,
		UnprovenSequence:    progress.UnprovenSequence,
		FailureKind:         progress.FailureKind,
	}
}

type linuxWitnessStopReceiptBarrierSnapshot struct {
	RunID      string                     `json:"run_id"`
	Generation int64                      `json:"generation"`
	PID        int                        `json:"pid"`
	Identity   string                     `json:"identity"`
	Receipt    linuxReplayReceiptSnapshot `json:"receipt"`
}

type linuxWitnessStopReceiptBarrierMarker struct {
	RunID      string                `json:"run_id"`
	Generation int64                 `json:"generation"`
	PID        int                   `json:"pid"`
	Identity   string                `json:"identity"`
	Receipt    authority.StopReceipt `json:"receipt"`
}

type linuxWitnessOwnerSnapshot struct {
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContextSHA256 string `json:"owner_context_sha256,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	JobIDSHA256        string `json:"job_id_sha256,omitempty"`
	HasStopReceipt     bool   `json:"has_stop_receipt"`
}

type linuxWitnessProcessSnapshot struct {
	At        time.Time `json:"at"`
	Role      string    `json:"role"`
	PID       int       `json:"pid"`
	Identity  string    `json:"identity,omitempty"`
	State     string    `json:"state,omitempty"`
	PGRP      int64     `json:"pgrp,omitempty"`
	Session   int64     `json:"session,omitempty"`
	StartTime uint64    `json:"start_time,omitempty"`
	Exists    bool      `json:"exists"`
	Error     string    `json:"error,omitempty"`
}

type linuxWitnessJournalFile struct {
	Path    string
	Raw     []byte
	Journal state.RunJournal
}

type linuxWitnessTarget struct {
	ConfigPath       string
	StateDir         string
	WorkspacePath    string
	TargetMarker     string
	DescendantMarker string
	EvidencePath     string
}

func newLinuxWitnessMetadata(t *testing.T, binaryPath string) linuxWitnessMetadata {
	t.Helper()
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Linux production witness source")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", ".."))
	subject := runLinuxWitnessCommand(t, repositoryRoot, "git", "rev-parse", "HEAD")
	tree := runLinuxWitnessCommand(t, repositoryRoot, "git", "rev-parse", "HEAD^{tree}")
	treeListing := runLinuxWitnessCommand(t, repositoryRoot, "git", "ls-tree", "-r", "--full-tree", subject)
	sourceHash, err := linuxWitnessSHA256(sourcePath)
	if err != nil {
		t.Fatalf("hash Linux production witness source: %v", err)
	}
	binaryHash, err := linuxWitnessSHA256(binaryPath)
	if err != nil {
		t.Fatalf("hash production daemon binary: %v", err)
	}
	return linuxWitnessMetadata{
		Subject:       subject,
		Tree:          tree,
		TreeSHA256:    linuxWitnessHashBytes([]byte(treeListing)),
		GoVersion:     runtime.Version(),
		KernelRelease: linuxWitnessKernelRelease(t),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		SourcePath:    sourcePath,
		SourceSHA256:  sourceHash,
		BinaryPath:    binaryPath,
		BinarySHA256:  binaryHash,
	}
}

func linuxWitnessOutputRoot(runRoot string) string {
	configured := strings.TrimSpace(os.Getenv(linuxProductionContainmentWitnessOutputDirectory))
	if configured == "" {
		return filepath.Clean(runRoot)
	}
	if absolute, err := filepath.Abs(configured); err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(configured)
}

func linuxWitnessCaseRoot(runRoot, name string) string {
	return filepath.Join(linuxWitnessOutputRoot(runRoot), name)
}

func linuxWitnessRunRoot(t *testing.T) string {
	t.Helper()
	if strings.TrimSpace(os.Getenv(linuxProductionContainmentWitnessOutputDirectory)) != "" {
		return t.TempDir()
	}
	root, err := os.MkdirTemp("", "symmetry-linux-containment-witness-")
	if err != nil {
		t.Fatalf("create local Linux witness workspace: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Linux witness raw local state retained at %s", root)
			return
		}
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove successful Linux witness workspace: %v", err)
		}
	})
	return root
}

func writeLinuxWitnessProvenance(t *testing.T, runRoot, label string, metadata any) {
	t.Helper()
	root := linuxWitnessOutputRoot(runRoot)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create Linux witness output root: %v", err)
	}
	contents, err := json.MarshalIndent(struct {
		CreatedAt time.Time `json:"created_at"`
		Metadata  any       `json:"metadata"`
	}{CreatedAt: time.Now().UTC(), Metadata: metadata}, "", "  ")
	if err != nil {
		t.Fatalf("encode Linux witness provenance: %v", err)
	}
	path := filepath.Join(root, "provenance-"+label+".json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write Linux witness provenance: %v", err)
	}
}

func linuxWitnessKernelRelease(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		t.Fatalf("read Linux kernel release: %v", err)
	}
	return strings.TrimSpace(string(contents))
}

func runLinuxBindBeforeCommitWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "bind-before-commit")
	evidence := linuxWitnessEvidence{Version: 1, Case: "daemon_sigkill_bind_before_commit", Stage: "initialized", Metadata: metadata, CaseRoot: caseRoot, RawOutput: map[string]string{}}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemon := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()

	operator := newOperator(t, environment)
	killResult := make(chan linuxBoundKillResult, 1)
	go func() {
		killResult <- waitAndKillLinuxDaemonAtBoundHandoff(daemon, target.StateDir, 45*time.Second)
	}()
	_ = submit(t, operator, daemonRun{profile: profileFromConfig(t, target.ConfigPath), workspace: workspaceFromConfig(t, target.ConfigPath)}, "linux-bind-before-commit", "slow")
	result := <-killResult
	if result.Err != nil {
		t.Fatalf("wait for bound pre-commit handoff: %v", result.Err)
	}
	if result.Exit.RequestedSignal != "SIGKILL" || !result.Exit.Signaled || result.Exit.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", result.Exit)
	}
	evidence.Stage = "crashed_bound_before_commit"
	evidence.Crash = &result.Exit
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(result.File))
	targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(result.Journal)
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_recovery", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_before_recovery", helperPID, helperIdentity),
	)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("bound handoff owner loss did not stop target: %v", err)
	}

	second := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, result.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("recover bound handoff: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Fatalf("bound handoff helper remained after recovery: %v", err)
	}
	evidence.Stage = "recovered_and_cleared"
	if snapshot, found, err := readLinuxWitnessJournalForKey(target.StateDir, result.Journal.Key()); err == nil && found {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(snapshot))
	} else if err != nil {
		t.Fatalf("read recovered bind journal: %v", err)
	}
	second.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
}

func runLinuxPrepareBeforeBindWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "prepare-before-bind")
	evidence := linuxWitnessEvidence{Version: 1, Case: "daemon_sigkill_prepare_before_bind", Stage: "initialized", Metadata: metadata, CaseRoot: caseRoot, RawOutput: map[string]string{}}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemon := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	killResult := make(chan linuxBoundKillResult, 1)
	go func() {
		killResult <- waitAndKillLinuxDaemonAtPreparedHandoff(daemon, target.StateDir, 45*time.Second)
	}()
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	_ = submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-prepare-before-bind", "slow")
	result := <-killResult
	if result.Err != nil {
		t.Fatalf("wait for prepared pre-bind handoff: %v", result.Err)
	}
	if result.Exit.RequestedSignal != "SIGKILL" || !result.Exit.Signaled || result.Exit.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", result.Exit)
	}
	handoff := result.Journal.ContainmentHandoff
	if handoff == nil || handoff.SupervisorPID != 0 || handoff.SupervisorIdentity != "" || handoff.StopReceipt != nil || result.Journal.ContainmentAuthority != nil {
		t.Fatalf("prepared handoff crash state = %#v, want unbound handoff without authority", linuxWitnessJournalSnapshotFromFile(result.File))
	}
	targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(result.Journal)
	if helperPID != 0 || helperIdentity != "" {
		t.Fatalf("prepared handoff exposed durable helper identity: pid=%d identity=%q", helperPID, helperIdentity)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target marker after pre-bind crash = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after pre-bind crash = %v, want absent", err)
	}
	evidence.Stage = "crashed_prepared_before_bind"
	evidence.Crash = &result.Exit
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(result.File))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_abort_proof", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("unbound_helper_before_abort_proof", helperPID, helperIdentity))
	evidence.Notes = append(evidence.Notes, "prepared handoff retained no durable helper identity; target markers remained absent because the helper held the target at the exec gate")
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	second := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	if err := waitForLinuxWitnessIdentityGone(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("prepared abort proof did not stop exact target: %v", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_abort_proof", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("unbound_helper_after_abort_proof", helperPID, helperIdentity))
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, result.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("prepared abort proof did not clear handoff: %v", err)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target marker after prepared abort = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after prepared abort = %v, want absent", err)
	}
	evidence.Stage = "abort_proof_cleared_without_receipt"
	if snapshot, found, err := readLinuxWitnessJournalForKey(target.StateDir, result.Journal.Key()); err == nil && found {
		if snapshot.Journal.ContainmentHandoff != nil || snapshot.Journal.ContainmentAuthority != nil || snapshot.Journal.HasProcessDetails() {
			t.Fatalf("prepared abort left containment state: %#v", linuxWitnessJournalSnapshotFromFile(snapshot))
		}
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(snapshot))
	} else if err != nil {
		t.Fatalf("read prepared abort journal: %v", err)
	}
	second.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
}

func runLinuxDaemonCrashAfterCommitBeforeResumeWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	runLinuxDaemonCrashBeforeResumeWitness(t, environment, daemonBinary, metadata, caseRoot, false)
}

func runLinuxDaemonCrashAfterLeaseArmBeforeResumeWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	runLinuxDaemonCrashBeforeResumeWitness(t, environment, daemonBinary, metadata, caseRoot, true)
}

func runLinuxDaemonCrashBeforeResumeWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string, leaseArmed bool) {
	t.Helper()
	targetName := "commit-before-resume"
	barrierName := "commit-resume"
	caseName := "daemon_sigkill_after_commit_before_resume"
	if leaseArmed {
		targetName = "lease-arm-before-resume"
		barrierName = "lease-armed-resume"
		caseName = "daemon_sigkill_after_lease_arm_before_resume"
	}
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, targetName)
	reachedPath := filepath.Join(caseRoot, barrierName+".reached")
	releasePath := filepath.Join(caseRoot, barrierName+".release")
	scanTriggerPath := ""
	scanReachedPath := ""
	scanReleasePath := ""
	receiptReachedPath := ""
	receiptReleasePath := ""
	if leaseArmed {
		scanTriggerPath = filepath.Join(caseRoot, "lease-armed-mirror-seal.trigger")
		scanReachedPath = filepath.Join(caseRoot, "lease-armed-mirror-seal.reached")
		scanReleasePath = filepath.Join(caseRoot, "lease-armed-mirror-seal.release")
		receiptReachedPath = filepath.Join(caseRoot, "stop-receipt-before-release.reached")
		receiptReleasePath = filepath.Join(caseRoot, "stop-receipt-before-release.release")
	}
	evidence := linuxWitnessEvidence{
		Version:                1,
		Case:                   caseName,
		Stage:                  "initialized",
		Metadata:               metadata,
		CaseRoot:               caseRoot,
		BarrierReachedPath:     reachedPath,
		BarrierReleasePath:     releasePath,
		ScanTriggerPath:        scanTriggerPath,
		ScanReachedPath:        scanReachedPath,
		ScanReleasePath:        scanReleasePath,
		StopReceiptReachedPath: receiptReachedPath,
		StopReceiptReleasePath: receiptReleasePath,
		RawOutput:              map[string]string{},
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	var (
		daemon                         *productionDaemonWitnessProcess
		second                         *productionDaemonWitnessProcess
		third                          *productionDaemonWitnessProcess
		task                           protocol.Task
		committed                      linuxWitnessJournalFile
		key                            state.RunKey
		targetPID, helperPID           int
		targetIdentity, helperIdentity string
		cleanupComplete                bool
	)
	defer func() {
		if contents, err := os.ReadFile(reachedPath); err == nil {
			evidence.BarrierReachedContents = string(contents)
		} else if !errors.Is(err, os.ErrNotExist) {
			evidence.Notes = append(evidence.Notes, fmt.Sprintf("read reached marker during teardown: %v", err))
		}
		if contents, err := os.ReadFile(releasePath); err == nil {
			evidence.BarrierReleaseContents = string(contents)
		} else if errors.Is(err, os.ErrNotExist) {
			evidence.BarrierReleaseContents = "<absent>"
		} else {
			evidence.Notes = append(evidence.Notes, fmt.Sprintf("read release marker during teardown: %v", err))
		}
		if scanReachedPath != "" {
			if contents, err := os.ReadFile(scanReachedPath + ".mirror"); err == nil {
				evidence.ScanMirrorReached = string(contents)
			}
			if contents, err := os.ReadFile(scanReleasePath); err == nil {
				evidence.ScanReleaseContents = string(contents)
			}
		}
		if key.RunID != "" {
			if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
				evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
				if targetPID == 0 {
					targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(current.Journal)
				}
			} else if err != nil {
				evidence.Notes = append(evidence.Notes, fmt.Sprintf("read journal during teardown: %v", err))
			}
		}
		if targetPID > 0 {
			evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
				linuxWitnessProcessSnapshotAt("teardown_target", targetPID, targetIdentity),
				linuxWitnessProcessSnapshotAt("teardown_helper", helperPID, helperIdentity))
		}
		evidence.RawOutput = linuxWitnessLogs(daemon, second)
		if third != nil {
			for path, contents := range linuxWitnessLogs(third, nil) {
				evidence.RawOutput[path] = contents
			}
		}
		if leaseArmed {
			if contents, err := os.ReadFile(receiptReachedPath); err == nil {
				if _, snapshot, decodeErr := decodeLinuxWitnessStopReceiptBarrierMarker(contents); decodeErr == nil {
					evidence.StopReceiptReached = &snapshot
				} else {
					evidence.Notes = append(evidence.Notes, fmt.Sprintf("decode stop-receipt marker during teardown: %v", decodeErr))
				}
			}
			if contents, err := os.ReadFile(receiptReleasePath); err == nil {
				evidence.StopReceiptRelease = string(contents)
			}
		}
		if evidence.Stage != "recovered_and_cleared" {
			evidence.Notes = append(evidence.Notes, "teardown diagnostics captured without changing the failure outcome")
		}
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	}()
	if leaseArmed {
		t.Cleanup(func() {
			if cleanupComplete {
				return
			}
			var cleanupErr error
			if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			if err := writeLinuxWitnessReleaseMarker(scanReleasePath); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			if key.RunID == "" {
				files, err := readLinuxWitnessJournals(target.StateDir)
				if err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("find unresolved journal for failure recovery: %w", err))
				} else {
					var unresolved []linuxWitnessJournalFile
					for _, file := range files {
						if file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentAuthority != nil || file.Journal.HasProcessDetails() {
							unresolved = append(unresolved, file)
						}
					}
					if len(unresolved) == 1 {
						key = unresolved[0].Journal.Key()
					} else if len(unresolved) > 1 || task.TaskID != "" {
						cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failure cleanup found %d unresolved journals for task %q", len(unresolved), task.TaskID))
					}
				}
			}
			if key.RunID != "" {
				current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
				if err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read exact-owner journal during failure cleanup: %w", err))
				} else if found {
					currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity := linuxWitnessOwnerIDs(current.Journal)
					ownerMatches := true
					if targetPID != 0 && (currentTargetPID != targetPID || currentTargetIdentity != targetIdentity || currentHelperPID != helperPID || currentHelperIdentity != helperIdentity) {
						ownerMatches = false
						cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failure cleanup journal owner changed: target=(%d,%q) helper=(%d,%q), original target=(%d,%q) helper=(%d,%q)", currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity))
					} else if targetPID == 0 {
						targetPID, targetIdentity, helperPID, helperIdentity = currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity
					}
					if ownerMatches && (current.Journal.ContainmentHandoff != nil || current.Journal.ContainmentAuthority != nil || current.Journal.HasProcessDetails()) {
						recovery, startErr := startLinuxRecoveryDaemonForCleanup(daemonBinary, target.ConfigPath, caseRoot, "failure-recovery")
						if startErr != nil {
							cleanupErr = errors.Join(cleanupErr, fmt.Errorf("start bound recovery daemon during failure cleanup: %w", startErr))
						} else {
							if waitErr := waitForLinuxWitnessJournalReleased(target.StateDir, key, 45*time.Second); waitErr != nil {
								cleanupErr = errors.Join(cleanupErr, fmt.Errorf("bound recovery did not clear journal during failure cleanup: %w", waitErr))
							}
							if stopErr := stopLinuxRecoveryDaemonForCleanup(recovery); stopErr != nil {
								cleanupErr = errors.Join(cleanupErr, fmt.Errorf("stop failure-cleanup recovery daemon: %w", stopErr))
							}
						}
					}
				}
				if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 5*time.Second); err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("containment journal remains unresolved after failure cleanup: %w", err))
				}
			} else if task.TaskID != "" {
				cleanupErr = errors.Join(cleanupErr, errors.New("failure cleanup cannot identify the task's bound recovery journal"))
			}
			if helperPID > 0 {
				if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failure cleanup could not prove exact helper %d/%q gone: %w", helperPID, helperIdentity, err))
				}
			}
			if targetPID > 0 {
				if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 15*time.Second); err != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failure cleanup could not prove exact target group %d/%q stopped: %w", targetPID, targetIdentity, err))
				}
			}
			if cleanupErr != nil && targetPID > 0 && helperPID > 0 && waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 100*time.Millisecond) == nil {
				evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
					linuxWitnessProcessSnapshotAt("failure_cleanup_helper_before_forced_stop", helperPID, helperIdentity))
				if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err == nil {
					if killErr := killLinuxWitnessProcess(t, helperPID, helperIdentity); killErr != nil {
						t.Errorf("failure cleanup could not stop exact helper %d/%q after bound recovery failed: %v", helperPID, helperIdentity, killErr)
					}
				}
				if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
					t.Errorf("failure cleanup could not prove exact helper %d/%q gone: %v", helperPID, helperIdentity, err)
				}
				evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
					linuxWitnessProcessSnapshotAt("failure_cleanup_helper_after_forced_stop", helperPID, helperIdentity))
			}
			if contents, err := os.ReadFile(receiptReachedPath); err == nil {
				if _, snapshot, decodeErr := decodeLinuxWitnessStopReceiptBarrierMarker(contents); decodeErr == nil {
					evidence.StopReceiptReached = &snapshot
				} else {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("decode stop-receipt marker during failure cleanup: %w", decodeErr))
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read stop-receipt reached marker during failure cleanup: %w", err))
			}
			if contents, err := os.ReadFile(receiptReleasePath); err == nil {
				evidence.StopReceiptRelease = string(contents)
			} else if !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read stop-receipt release marker during failure cleanup: %w", err))
			}
			if contents, err := os.ReadFile(scanReachedPath + ".mirror"); err == nil {
				evidence.ScanMirrorReached = string(contents)
			} else if !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read mirror-seal reached marker during failure cleanup: %w", err))
			}
			if contents, err := os.ReadFile(scanReleasePath); err == nil {
				evidence.ScanReleaseContents = string(contents)
			} else if !errors.Is(err, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read mirror-seal release marker during failure cleanup: %w", err))
			}

			if cleanupErr != nil {
				evidence.Notes = append(evidence.Notes, "failure cleanup could not prove exact helper stop/release: "+cleanupErr.Error())
				t.Errorf("Linux production witness failure cleanup could not prove exact helper stop/release: %v", cleanupErr)
			} else {
				evidence.Notes = append(evidence.Notes, "failure cleanup verified or recovered the original exact helper and target group")
			}
			evidence.RawOutput = linuxWitnessLogs(daemon, second)
			if third != nil {
				for path, contents := range linuxWitnessLogs(third, nil) {
					evidence.RawOutput[path] = contents
				}
			}
			if key.RunID != "" {
				if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
					evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
				} else if err != nil {
					evidence.Notes = append(evidence.Notes, fmt.Sprintf("read exact-owner journal for failure evidence: %v", err))
				}
			}
			writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		})
	} else {
		t.Cleanup(func() {
			cleanupLinuxUnsealedWitnessOwner(t, target, key, targetPID, targetIdentity, helperPID, helperIdentity, &evidence)
		})
	}

	reachedEnvironment := linuxCommitResumeBarrierReachedEnvironment
	releaseEnvironment := linuxCommitResumeBarrierReleaseEnvironment
	if leaseArmed {
		reachedEnvironment = linuxLeaseArmedResumeBarrierReachedEnvironment
		releaseEnvironment = linuxLeaseArmedResumeBarrierReleaseEnvironment
	}
	barrierEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		reachedEnvironment:                           reachedPath,
		releaseEnvironment:                           releasePath,
	}
	if leaseArmed {
		for name, value := range linuxWitnessScanSealEnvironment(scanTriggerPath, scanReachedPath, scanReleasePath, "mirror") {
			barrierEnvironment[name] = value
		}
	}
	daemon = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", barrierEnvironment)
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()

	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	label := "linux-commit-before-resume"
	if leaseArmed {
		label = "linux-lease-arm-before-resume"
		if err := primeLinuxWitnessScanSeal(scanTriggerPath, scanReachedPath, scanReleasePath); err != nil {
			t.Fatalf("prime pre-resume mirror-seal boundary before lease arm: %v", err)
		}
	}
	task = submit(t, operator, daemonRun{profile: profile, workspace: workspace}, label, "slow")
	evidence.TaskID = task.TaskID
	if task.RunID != nil {
		evidence.RunID = *task.RunID
	}
	if task.Generation != nil {
		evidence.Generation = *task.Generation
	}
	if err := waitForLinuxWitnessMarker(reachedPath, 45*time.Second); err != nil {
		t.Fatalf("wait for production commit/resume reached marker: %v", err)
	}
	reachedContents, err := os.ReadFile(reachedPath)
	if err != nil {
		t.Fatalf("read production commit/resume reached marker: %v", err)
	}
	evidence.BarrierReachedContents = string(reachedContents)
	leaseArmedMarker := "lease_armed=false"
	if leaseArmed {
		leaseArmedMarker = "lease_armed=true"
	}
	if !strings.Contains(evidence.BarrierReachedContents, "\n"+leaseArmedMarker+"\n") {
		t.Fatalf("resume reached marker = %q, want %s", evidence.BarrierReachedContents, leaseArmedMarker)
	}
	if _, err := os.Stat(releasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release marker before daemon crash = %v, want absent", err)
	}

	files, err := readLinuxWitnessJournals(target.StateDir)
	if err != nil {
		t.Fatalf("read journal after commit/resume reached marker: %v", err)
	}
	for _, file := range files {
		if file.Journal.ContainmentAuthority != nil && file.Journal.ContainmentHandoff == nil {
			committed = file
			break
		}
	}
	if committed.Path == "" {
		safeSnapshots := make([]linuxWitnessJournalSnapshot, 0, len(files))
		for _, file := range files {
			safeSnapshots = append(safeSnapshots, linuxWitnessJournalSnapshotFromFile(file))
		}
		t.Fatalf("journal after commit/resume reached marker did not expose durable committed authority: %#v", safeSnapshots)
	}
	if committed.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("committed authority unexpectedly contained stop receipt: %#v", linuxWitnessJournalSnapshotFromFile(committed))
	}
	key = committed.Journal.Key()
	evidence.TaskID = task.TaskID
	evidence.RunID = key.RunID
	evidence.Generation = key.Generation
	evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(committed.Journal)
	if leaseArmed {
		if !strings.Contains(evidence.BarrierReachedContents, fmt.Sprintf("pid=%d\n", daemon.command.Process.Pid)) ||
			!strings.Contains(evidence.BarrierReachedContents, fmt.Sprintf("target_pid=%d\n", targetPID)) {
			t.Fatalf("lease-armed reached marker = %q, want daemon pid %d and journal target pid %d",
				evidence.BarrierReachedContents, daemon.command.Process.Pid, targetPID)
		}
		if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
			t.Fatalf("exact suspended target identity before resume: %v", err)
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper identity before resume: %v", err)
		}
	}
	targetSnapshot := linuxWitnessProcessSnapshotAt("target_before_resume", targetPID, targetIdentity)
	helperSnapshot := linuxWitnessProcessSnapshotAt("helper_before_resume", helperPID, helperIdentity)
	if !targetSnapshot.Exists || !helperSnapshot.Exists {
		t.Fatalf("resume barrier process snapshots are incomplete: target=%#v helper=%#v", targetSnapshot, helperSnapshot)
	}
	if targetSnapshot.State != "T" && targetSnapshot.State != "t" {
		t.Fatalf("target process state before resume = %q, want Linux stopped state T or t: %#v", targetSnapshot.State, targetSnapshot)
	}
	if targetSnapshot.PGRP != int64(targetPID) || targetSnapshot.Session <= 0 {
		t.Fatalf("target group snapshot before resume is incomplete: %#v", targetSnapshot)
	}
	if helperSnapshot.StartTime == 0 {
		t.Fatalf("helper identity snapshot before resume is incomplete: %#v", helperSnapshot)
	}
	var sealedScanFile *linuxWitnessJournalFile
	if leaseArmed {
		mirrorSealed, _, mirrorReached, sealErr := awaitLinuxWitnessScanSeal(target.StateDir, key, scanReachedPath, "mirror", 45*time.Second)
		if sealErr != nil {
			t.Fatalf("wait for RenewLease mirror seal before lease-armed resume barrier: %v", sealErr)
		}
		if mirrorSealed.Journal.LeaseExpiresAt.IsZero() || !time.Now().Before(mirrorSealed.Journal.LeaseExpiresAt) {
			t.Fatalf("durable mirror seal was not observed before persisted lease expiry: seal_snapshot_at=%s lease_expires_at=%s", time.Now().UTC().Format(time.RFC3339Nano), mirrorSealed.Journal.LeaseExpiresAt.UTC().Format(time.RFC3339Nano))
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper remained available after mirror seal: %v", err)
		}
		sealedScanFile = &mirrorSealed
		evidence.ScanMirrorReached = mirrorReached
		if contents, err := os.ReadFile(scanReleasePath); err != nil {
			t.Fatalf("read lease-armed mirror-seal release marker: %v", err)
		} else {
			evidence.ScanReleaseContents = string(contents)
		}
		targetSnapshot = linuxWitnessProcessSnapshotAt("target_at_mirror_seal_before_resume", targetPID, targetIdentity)
		helperSnapshot = linuxWitnessProcessSnapshotAt("helper_at_mirror_seal_before_resume", helperPID, helperIdentity)
		if targetSnapshot.State != "T" && targetSnapshot.State != "t" {
			t.Fatalf("target state at mirror-seal barrier = %q, want stopped before resume: %#v", targetSnapshot.State, targetSnapshot)
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper at mirror-seal barrier: %v", err)
		}
		evidence.Notes = append(evidence.Notes, "successful initial RenewLease synchronously requested one mirror scan and returned only after its durable clean seal; the Runner lease-armed barrier and durable journal snapshot were both observed before the persisted lease expiry")
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker before resume = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant user-code marker before resume = %v, want absent", err)
	}
	if leaseArmed {
		evidence.Stage = "lease_armed_with_durable_mirror_seal_before_resume"
		evidence.Notes = append(evidence.Notes, "reached marker was observed after initial helper lease arm and before target resume; target and descendant markers were absent")
		evidence.Notes = append(evidence.Notes, "the production witness gate requested one durable mirror clean seal during successful initial RenewLease, before the Runner barrier and while the target remained stopped")
	} else {
		evidence.Stage = "committed_before_resume_lease_unarmed"
		evidence.Notes = append(evidence.Notes, "reached marker was observed after durable commit and before initial helper lease arm; target and descendant markers were absent")
	}
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(committed))
	if sealedScanFile != nil {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(*sealedScanFile))
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, targetSnapshot, helperSnapshot)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	crash := killLinuxProductionDaemon(daemon)
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", crash)
	}
	if leaseArmed {
		evidence.Stage = "daemon_crashed_after_lease_arm_with_durable_mirror_seal_before_resume"
	} else {
		evidence.Stage = "daemon_crashed_after_commit_before_resume"
	}
	evidence.Crash = &crash
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("helper owner loss did not stop target group before resume: %v", err)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after daemon crash = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant user-code marker after daemon crash = %v, want absent", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_owner_loss", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_owner_loss", helperPID, helperIdentity))
	if leaseArmed {
		unresolved, found, readErr := readLinuxWitnessJournalForKey(target.StateDir, key)
		if readErr != nil {
			t.Fatalf("read armed-but-unreceipted journal after daemon crash: %v", readErr)
		}
		if !found {
			t.Fatalf("journal after armed pre-resume crash: found=false, want retained committed authority without receipt")
		}
		if unresolved.Journal.ContainmentAuthority == nil || unresolved.Journal.ContainmentHandoff != nil ||
			unresolved.Journal.ContainmentAuthority.StopReceipt != nil || !unresolved.Journal.HasProcessDetails() || unresolved.Journal.ContainmentUnproven {
			gotReceipt := unresolved.Journal.ContainmentAuthority != nil && unresolved.Journal.ContainmentAuthority.StopReceipt != nil
			t.Fatalf("journal after armed pre-resume crash: authority=%t handoff=%t process_details=%t receipt=%t unproven=%t, want committed authority with exact process details, no receipt, and no unproven scan", unresolved.Journal.ContainmentAuthority != nil, unresolved.Journal.ContainmentHandoff != nil, unresolved.Journal.HasProcessDetails(), gotReceipt, unresolved.Journal.ContainmentUnproven)
		}
		gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(unresolved.Journal)
		if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity ||
			gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
			t.Fatalf("recovered journal owner identity = (%d, %q, %d, %q), want original (%d, %q, %d, %q)",
				gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity,
				targetPID, targetIdentity, helperPID, helperIdentity)
		}
		if !linuxWitnessJournalHasBoundScanLedger(unresolved.Journal) ||
			!linuxWitnessScanProgressIsSealedClean(unresolved.Journal.ContainmentScans.Mirror) {
			t.Fatalf("armed journal after daemon crash lost its durable mirror seal: scans_match=%t mirror_sealed_sequence=%d", linuxWitnessScanLedgerMatches(unresolved.Journal), linuxWitnessScanMirrorSealedSequence(unresolved.Journal))
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper did not remain available for restart replay: %v", err)
		}
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(unresolved))
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, linuxWitnessProcessSnapshotAt("helper_waiting_for_restart_replay", helperPID, helperIdentity))
		evidence.Notes = append(evidence.Notes, "daemon owner loss stopped the original target group while retaining its exact live helper and committed journal without a stop receipt for restart replay")
	} else {
		unresolved, found, readErr := readLinuxWitnessJournalForKey(target.StateDir, key)
		if readErr != nil {
			t.Fatalf("read unsealed commit-before-resume journal after daemon crash: %v", readErr)
		}
		if !found {
			t.Fatalf("journal after commit-before-resume crash: found=false, want retained unresolved containment")
		}
		journal := unresolved.Journal
		if journal.ContainmentAuthority == nil || journal.ContainmentHandoff != nil ||
			journal.ContainmentAuthority.StopReceipt != nil || !journal.HasProcessDetails() {
			gotReceipt := journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil
			t.Fatalf("journal after commit-before-resume crash: authority=%t handoff=%t process_details=%t receipt=%t, want retained authority with exact process details and no receipt/clear", journal.ContainmentAuthority != nil, journal.ContainmentHandoff != nil, journal.HasProcessDetails(), gotReceipt)
		}
		gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(journal)
		if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
			t.Fatalf("unsealed commit-before-resume journal owner identity = (%d,%q,%d,%q), want original (%d,%q,%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
		}
		if !linuxWitnessJournalHasBoundScanLedger(journal) || linuxWitnessScanMirrorSealedSequence(journal) != 0 {
			t.Fatalf("commit-before-resume crash journal is not unresolved at the unsealed-mirror boundary: scans_match=%t mirror_sealed_sequence=%d", linuxWitnessScanLedgerMatches(journal), linuxWitnessScanMirrorSealedSequence(journal))
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper did not retain unresolved commit-before-resume recovery endpoint: %v", err)
		}
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(unresolved))
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, linuxWitnessProcessSnapshotAt("helper_retaining_unresolved_commit_before_resume", helperPID, helperIdentity))
		evidence.Stage = "daemon_crash_after_commit_unsealed_mirror_retained_without_receipt"
		evidence.Notes = append(evidence.Notes, "hard daemon SIGKILL after durable commit and before initial lease arm/resume was followed by physical stop proof for the original process group; the exact authority journal remains unresolved with an unsealed mirror, no stop receipt, and no containment clear")
		evidence.Notes = append(evidence.Notes, "no restart recovery was attempted: this initial-only, unsealed scan boundary cannot truthfully produce the receipt required for helper release or journal clear")
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		return
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	second = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxStopReceiptBarrierReachedEnvironment:    receiptReachedPath,
		linuxStopReceiptBarrierReleaseEnvironment:    receiptReleasePath,
	})
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	recovery := second
	if leaseArmed {
		if err := waitForLinuxWitnessMarker(receiptReachedPath, 45*time.Second); err != nil {
			t.Fatalf("wait for durable stop-receipt-before-release barrier: %v", err)
		}
		receiptReached, err := os.ReadFile(receiptReachedPath)
		if err != nil {
			t.Fatalf("read durable stop-receipt-before-release marker: %v", err)
		}
		marker, markerSnapshot, err := decodeLinuxWitnessStopReceiptBarrierMarker(receiptReached)
		if err != nil {
			t.Fatalf("decode durable stop-receipt-before-release marker: %v", err)
		}
		evidence.StopReceiptReached = &markerSnapshot
		receiptFile, err := waitForLinuxWitnessJournal(target.StateDir, "durable stop receipt before helper release", 10*time.Second, func(value linuxWitnessJournalFile) bool {
			return value.Journal.Key() == key && value.Journal.ContainmentAuthority != nil &&
				value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails()
		})
		if err != nil {
			t.Fatalf("read durable stop receipt at release barrier: %v", err)
		}
		if receiptFile.Journal.ContainmentAuthority == nil || receiptFile.Journal.ContainmentAuthority.StopReceipt == nil {
			t.Fatalf("receipt barrier journal lacks its durable authority receipt: authority_present=%t handoff_present=%t process_details=%t", receiptFile.Journal.ContainmentAuthority != nil, receiptFile.Journal.ContainmentHandoff != nil, receiptFile.Journal.HasProcessDetails())
		}
		receipt := *receiptFile.Journal.ContainmentAuthority.StopReceipt
		if !receipt.ValidFor(*receiptFile.Journal.ContainmentAuthority) {
			t.Fatalf("receipt barrier receipt is invalid for its journal authority: %#v", linuxReplayReceiptSnapshotFromReceipt(receipt))
		}
		gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(receiptFile.Journal)
		if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
			t.Fatalf("receipt-barrier journal owner identity = (%d,%q,%d,%q), want original (%d,%q,%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
		}
		if marker.RunID != key.RunID || marker.Generation != key.Generation || marker.PID != targetPID || marker.Identity != targetIdentity || marker.Receipt != receipt {
			t.Fatalf("stop-receipt barrier marker identity/receipt = (%s/%d,%d,%q,%#v), want journal key %s/%d owner %d/%q and safe receipt snapshot %#v", marker.RunID, marker.Generation, marker.PID, marker.Identity, linuxReplayReceiptSnapshotFromReceipt(marker.Receipt), key.RunID, key.Generation, targetPID, targetIdentity, linuxReplayReceiptSnapshotFromReceipt(receipt))
		}
		if receiptFile.Journal.ContainmentHandoff != nil || !receiptFile.Journal.HasProcessDetails() {
			t.Fatalf("receipt barrier journal crossed clear boundary: authority_present=%t handoff_present=%t process_details=%t", receiptFile.Journal.ContainmentAuthority != nil, receiptFile.Journal.ContainmentHandoff != nil, receiptFile.Journal.HasProcessDetails())
		}
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper remained live before release barrier crash: %v", err)
		}
		if _, err := os.Lstat(receiptReleasePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stop-receipt release marker before crash = %v, want absent", err)
		}
		evidence.Stage = "stop_receipt_durable_before_helper_release"
		evidence.StopReceiptBeforeRelease = linuxReplayReceiptSnapshotFromReceipt(receipt)
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(receiptFile))
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
			linuxWitnessProcessSnapshotAt("target_at_stop_receipt_before_release", targetPID, targetIdentity),
			linuxWitnessProcessSnapshotAt("helper_at_stop_receipt_before_release", helperPID, helperIdentity))
		evidence.Notes = append(evidence.Notes, "the restart daemon reached the post-durable-receipt callback barrier while the exact helper remained live and the journal retained containment; helper release and clear had not run")
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		receiptCrash := killLinuxProductionDaemon(second)
		if receiptCrash.RequestedSignal != "SIGKILL" || !receiptCrash.Signaled || receiptCrash.Signal != "SIGKILL" {
			t.Fatalf("receipt-barrier daemon crash status = %#v, want SIGKILL", receiptCrash)
		}
		evidence.CrashAfterStopReceipt = &receiptCrash
		evidence.Stage = "daemon_crashed_after_receipt_before_helper_release"
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
			t.Fatalf("unblock stop-receipt barrier after daemon crash: %v", err)
		}
		if contents, err := os.ReadFile(receiptReleasePath); err == nil {
			evidence.StopReceiptRelease = string(contents)
		} else {
			t.Fatalf("read stop-receipt release marker after unblocking daemon: %v", err)
		}
		third = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third")
		defer func() {
			if third != nil && !third.waited {
				third.stop(t)
			}
		}()
		recovery = third
	}
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 45*time.Second); err != nil {
		t.Fatalf("recover pre-resume authority: %v", err)
	}
	if leaseArmed {
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("restarted daemon did not release the exact recovered helper: %v", err)
		}
		if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target user-code marker after recovery = %v, want absent", err)
		}
		if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("descendant user-code marker after recovery = %v, want absent", err)
		}
	}
	cleared, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil {
		t.Fatalf("read recovered commit-before-resume journal: %v", err)
	}
	if found {
		if cleared.Journal.ContainmentHandoff != nil || cleared.Journal.ContainmentAuthority != nil || cleared.Journal.HasProcessDetails() {
			t.Fatalf("recovered commit-before-resume journal retained containment: %#v", linuxWitnessJournalSnapshotFromFile(cleared))
		}
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(cleared))
	} else {
		// Recovery may retire the already-cleared journal before this readback.
		// waitForLinuxWitnessJournalReleased above has already observed the
		// release boundary; absence now is the expected post-clear state.
		evidence.Notes = append(evidence.Notes, "recovered commit-before-resume journal was retired after durable containment clear")
	}
	if got := getTask(t, operator, task.TaskID); got.RunID == nil || *got.RunID != key.RunID {
		t.Fatalf("recovered task run identity changed: %#v", got)
	}
	evidence.Stage = "recovered_and_cleared"
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_recovery", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_recovery", helperPID, helperIdentity))
	recovery.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	if third != nil {
		for path, contents := range linuxWitnessLogs(third, nil) {
			evidence.RawOutput[path] = contents
		}
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	cleanupComplete = true
}

func runLinuxDaemonCrashAfterResumeAtCleanSealBoundaryWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	runLinuxDaemonCrashAfterResumeWitnessMode(t, environment, daemonBinary, metadata, caseRoot, true)
}

func runLinuxDaemonCrashAfterResumeUnsealedMirrorWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	runLinuxDaemonCrashAfterResumeWitnessMode(t, environment, daemonBinary, metadata, caseRoot, false)
}

func runLinuxDaemonCrashAfterResumeWitnessMode(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string, sealedCleanBoundary bool) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "daemon-after-resume")
	caseName := "daemon_sigkill_after_resume_unsealed_mirror_fail_closed"
	scanTriggerPath := ""
	scanReachedPath := ""
	scanReleasePath := ""
	if sealedCleanBoundary {
		scanTriggerPath = filepath.Join(caseRoot, "both-scanner-clean-seal.trigger")
		scanReachedPath = filepath.Join(caseRoot, "both-scanner-clean-seal.reached")
		scanReleasePath = filepath.Join(caseRoot, "both-scanner-clean-seal.release")
		caseName = "daemon_sigkill_after_resume_at_both_scanner_clean_seal_boundary"
	}
	evidence := linuxWitnessEvidence{
		Version:         1,
		Case:            caseName,
		Stage:           "initialized",
		Metadata:        metadata,
		CaseRoot:        caseRoot,
		ScanTriggerPath: scanTriggerPath,
		ScanReachedPath: scanReachedPath,
		ScanReleasePath: scanReleasePath,
		RawOutput:       map[string]string{},
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	var daemon *productionDaemonWitnessProcess
	if sealedCleanBoundary {
		daemon = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", linuxWitnessScanSealEnvironment(scanTriggerPath, scanReachedPath, scanReleasePath, "both"))
	} else {
		daemon = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	}
	var (
		second                         *productionDaemonWitnessProcess
		key                            state.RunKey
		targetPID, helperPID           int
		targetIdentity, helperIdentity string
		cleanupComplete                bool
		negativeObserved               bool
	)
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	t.Cleanup(func() {
		if cleanupComplete {
			return
		}
		if negativeObserved {
			evidence.Notes = append(evidence.Notes, "expected negative witness retained the exact unresolved journal; cleanup did not retry unsupported recovery, issue protocol release, or clear containment")
			evidence.RawOutput = linuxWitnessLogs(daemon, second)
			cleanupLinuxUnsealedWitnessOwner(t, target, key, targetPID, targetIdentity, helperPID, helperIdentity, &evidence)
			return
		}
		if key.RunID != "" {
			if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
				evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
			} else if err != nil {
				evidence.Notes = append(evidence.Notes, "read exact-owner journal before failure cleanup: "+err.Error())
			}
		}
		if targetPID > 0 {
			evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
				linuxWitnessProcessSnapshotAt("target_before_failure_cleanup", targetPID, targetIdentity),
				linuxWitnessProcessSnapshotAt("helper_before_failure_cleanup", helperPID, helperIdentity))
		}
		evidence.RawOutput = linuxWitnessLogs(daemon, second)
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		cleanupTimeout := 45 * time.Second
		var releasePaths []string
		if sealedCleanBoundary {
			releasePaths = append(releasePaths, scanReleasePath)
		}
		cleanupErr := cleanupLinuxWitnessRecoveryAfterFailure(daemonBinary, target, caseRoot, key, targetPID, targetIdentity, helperPID, helperIdentity, cleanupTimeout, releasePaths...)
		if cleanupErr != nil {
			evidence.Notes = append(evidence.Notes, "failure cleanup could not prove exact owner recovery: "+cleanupErr.Error())
			t.Errorf("Linux daemon-crash witness cleanup could not prove exact owner recovery: %v", cleanupErr)
		}
		if helperPID > 0 && cleanupErr != nil {
			evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
				linuxWitnessProcessSnapshotAt("helper_after_failed_cleanup", helperPID, helperIdentity))
		}
		if sealedCleanBoundary {
			if contents, err := os.ReadFile(scanReleasePath); err == nil {
				evidence.ScanReleaseContents = string(contents)
			}
		}
		if key.RunID != "" {
			if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
				evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
			}
		}
		evidence.RawOutput = linuxWitnessLogs(daemon, second)
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	})
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-daemon-after-resume", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "resumed authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	key = file.Journal.Key()
	childPID, err := waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if markerPID, markerErr := readLinuxWitnessPIDMarker(target.TargetMarker); markerErr != nil || markerPID != targetPID {
		t.Fatalf("target marker = (%d, %v), want journal target pid %d", markerPID, markerErr, targetPID)
	}
	anchor := linuxWitnessProcessSnapshotAt("target_before_daemon_crash", targetPID, targetIdentity)
	child := linuxWitnessProcessSnapshotAt("same_group_descendant_before_daemon_crash", childPID, "")
	assertLinuxWitnessSameGroup(t, anchor, child)
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("target before ordinary daemon crash: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("same-group descendant before ordinary daemon crash: %v", err)
	}
	latest, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil || !found {
		t.Fatalf("read ordinary-run journal before daemon crash: found=%t error=%v", found, err)
	}
	gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(latest.Journal)
	if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
		t.Fatalf("ordinary-run journal owner changed before daemon crash: target=(%d,%q) helper=(%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity)
	}
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(latest))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, anchor, child,
		linuxWitnessProcessSnapshotAt("helper_before_daemon_crash", helperPID, helperIdentity))
	if sealedCleanBoundary {
		sealedFile, helperReached, mirrorReached, sealErr := waitForLinuxWitnessScanSeal(target.StateDir, key, scanTriggerPath, scanReachedPath, scanReleasePath, "both", 45*time.Second)
		if sealErr != nil {
			t.Fatalf("wait for both durable scanner seals before daemon crash: %v", sealErr)
		}
		anchor = linuxWitnessProcessSnapshotAt("target_at_both_scanner_clean_seal_boundary", targetPID, targetIdentity)
		child = linuxWitnessProcessSnapshotAt("same_group_descendant_at_both_scanner_clean_seal_boundary", childPID, "")
		assertLinuxWitnessSameGroup(t, anchor, child)
		if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
			t.Fatalf("target at both-scanner clean-seal boundary: %v", err)
		}
		if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
			t.Fatalf("same-group descendant at both-scanner clean-seal boundary: %v", err)
		}
		evidence.ScanHelperReached = helperReached
		evidence.ScanMirrorReached = mirrorReached
		if contents, err := os.ReadFile(scanReleasePath); err != nil {
			t.Fatalf("read both-scanner seal release marker: %v", err)
		} else {
			evidence.ScanReleaseContents = string(contents)
		}
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(sealedFile))
		evidence.Stage = "running_at_both_scanner_durable_clean_seal_boundary"
		evidence.Notes = append(evidence.Notes, "daemon SIGKILL was issued at a deliberately gated boundary after both helper and mirror clean scans had durable seal ACKs; this evidence is specific to the sealed boundary and does not claim ordinary unsealed owner loss can succeed")
	} else {
		evidence.Stage = "ordinary_run_with_live_descendant"
		evidence.Notes = append(evidence.Notes, "daemon SIGKILL was issued during an ordinary production run with a live same-group descendant; no test-only scan pause or preseal was configured")
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	crash := killLinuxProductionDaemon(daemon)
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", crash)
	}
	if sealedCleanBoundary {
		evidence.Stage = "daemon_crashed_after_resume_at_both_scanner_clean_seal_boundary"
	} else {
		evidence.Stage = "daemon_crashed_after_resume_with_unsealed_mirror"
	}
	evidence.Crash = &crash
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("daemon owner loss did not stop target group: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("daemon owner loss did not stop same-group descendant: %v", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_owner_loss", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_owner_loss", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_owner_loss", helperPID, helperIdentity))
	if stopped, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(stopped))
		gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(stopped.Journal)
		if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
			t.Fatalf("post-owner-loss journal changed containment identity: target=(%d,%q) helper=(%d,%q), want target=(%d,%q) helper=(%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
		}
		if stopped.Journal.ContainmentAuthority == nil || stopped.Journal.ContainmentHandoff != nil ||
			stopped.Journal.ContainmentAuthority.StopReceipt != nil || !stopped.Journal.HasProcessDetails() {
			t.Fatalf("post-owner-loss journal crossed receipt or clear boundary: authority=%t handoff=%t receipt=%t process_details=%t", stopped.Journal.ContainmentAuthority != nil, stopped.Journal.ContainmentHandoff != nil, stopped.Journal.ContainmentAuthority != nil && stopped.Journal.ContainmentAuthority.StopReceipt != nil, stopped.Journal.HasProcessDetails())
		}
		if sealedCleanBoundary {
			if !linuxWitnessJournalHasScanSealBoundary(stopped.Journal, "both") || stopped.Journal.ContainmentUnproven {
				t.Fatalf("journal after sealed-clean-boundary owner loss lost its durable helper/mirror proof: scans_match=%t both_sealed=%t unproven=%t", linuxWitnessScanLedgerMatches(stopped.Journal), stopped.Journal.ContainmentScans != nil && linuxWitnessScanProgressIsSealedClean(stopped.Journal.ContainmentScans.Helper) && linuxWitnessScanProgressIsSealedClean(stopped.Journal.ContainmentScans.Mirror), stopped.Journal.ContainmentUnproven)
			}
		} else {
			if !linuxWitnessScanLedgerMatches(stopped.Journal) || linuxWitnessScanMirrorSealedSequence(stopped.Journal) != 0 {
				t.Fatalf("ordinary unsealed daemon crash journal: authority=%t handoff=%t receipt=%t process_details=%t scans_match=%t mirror_sealed_sequence=%d", stopped.Journal.ContainmentAuthority != nil, stopped.Journal.ContainmentHandoff != nil, stopped.Journal.ContainmentAuthority != nil && stopped.Journal.ContainmentAuthority.StopReceipt != nil, stopped.Journal.HasProcessDetails(), linuxWitnessScanLedgerMatches(stopped.Journal), linuxWitnessScanMirrorSealedSequence(stopped.Journal))
			}
			evidence.Stage = "daemon_crash_unsealed_mirror_retained_without_receipt"
			evidence.Notes = append(evidence.Notes, "ordinary daemon SIGKILL physically stopped the original group but retained an unsealed mirror journal without receipt, helper release, or containment clear")
			evidence.Notes = append(evidence.Notes, "the expected negative boundary is retained as-is: cleanup performs no unsupported recovery retry, helper release, or journal clear")
			writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
			negativeObserved = true
			return
		}
	} else if err != nil {
		t.Fatalf("read ordinary-run journal after owner loss: %v", err)
	} else {
		t.Fatal("journal disappeared after owner loss before a recovery receipt was persisted")
	}

	second = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("recover daemon-crash authority: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Fatalf("recovery did not release exact original helper: %v", err)
	}
	if got := getTask(t, operator, task.TaskID); got.RunID == nil || *got.RunID != file.Journal.RunID {
		t.Fatalf("recovered task run identity changed: %#v", got)
	}
	evidence.Stage = "recovered_and_cleared"
	if snapshot, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key()); err == nil && found {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(snapshot))
	} else if err != nil {
		t.Fatalf("read recovered daemon-crash journal: %v", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_recovery", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_recovery", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_recovery", helperPID, helperIdentity))
	second.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	cleanupComplete = true
}

func runLinuxHelperCrashMirrorWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "helper-mirror")
	scanTriggerPath := filepath.Join(caseRoot, "clean-scan.trigger")
	scanReachedPath := filepath.Join(caseRoot, "clean-scan.reached")
	scanReleasePath := filepath.Join(caseRoot, "clean-scan.release")
	receiptReachedPath := filepath.Join(caseRoot, "helper-stop-receipt.reached")
	receiptReleasePath := filepath.Join(caseRoot, "helper-stop-receipt.release")
	evidence := linuxWitnessEvidence{
		Version:                1,
		Case:                   "helper_sigkill_mirror_takeover",
		Stage:                  "initialized",
		Metadata:               metadata,
		CaseRoot:               caseRoot,
		ScanTriggerPath:        scanTriggerPath,
		ScanReachedPath:        scanReachedPath,
		ScanReleasePath:        scanReleasePath,
		StopReceiptReachedPath: receiptReachedPath,
		StopReceiptReleasePath: receiptReleasePath,
		RawOutput:              map[string]string{},
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemonEnvironment := linuxWitnessScanSealEnvironment(scanTriggerPath, scanReachedPath, scanReleasePath, "both")
	daemonEnvironment[linuxStopReceiptBarrierReachedEnvironment] = receiptReachedPath
	daemonEnvironment[linuxStopReceiptBarrierReleaseEnvironment] = receiptReleasePath
	daemon := startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", daemonEnvironment)
	var (
		key                            state.RunKey
		targetPID, helperPID           int
		targetIdentity, helperIdentity string
		childPID                       int
		targetPIDFD                    = -1
		helperPIDFD                    = -1
		daemonPaused                   bool
		leaveDaemonForRecovery         bool
		operator                       *control.OperatorClient
		taskID                         string
		submitIdempotencyKey           string
		submitRequest                  protocol.TaskSubmitRequest
		submissionAttempted            bool
		cancelMutationID               string
		scanCleanupComplete            bool
	)
	defer func() {
		if daemon != nil && !daemon.waited {
			if leaveDaemonForRecovery {
				t.Logf("leaving owning daemon alive because exact physical stop remains unproven; local state: %s", caseRoot)
			} else {
				daemon.stop(t)
			}
		}
	}()
	defer func() {
		if targetPIDFD >= 0 {
			if err := unix.Close(targetPIDFD); err != nil {
				t.Errorf("close test-owned target pidfd: %v", err)
			}
		}
		if helperPIDFD >= 0 {
			if err := unix.Close(helperPIDFD); err != nil {
				t.Errorf("close test-owned helper pidfd: %v", err)
			}
		}
	}()
	operator = newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	submitIdempotencyKey = unique("linux-helper-mirror-submit")
	submitRequest = protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal: "linux-helper-mirror", AgentProfile: profile, Workspace: workspace, Input: json.RawMessage(`{"mode":"slow"}`),
	}}
	cancelMutationID = unique("linux-helper-mirror-cancel")
	defer func() {
		if !submissionAttempted {
			return
		}
		if scanCleanupComplete {
			return
		}
		leaveDaemonForRecovery = true
		cleanup := cleanupLinuxHelperCrashAfterFailure(linuxWitnessHelperCrashCleanupRequest{
			Daemon: daemon, DaemonPaused: &daemonPaused, Operator: operator,
			TaskID: &taskID, SubmissionAttempted: &submissionAttempted,
			SubmitIdempotencyKey: submitIdempotencyKey, SubmitRequest: submitRequest,
			CancelMutationID: cancelMutationID, Target: target, Key: &key,
			TargetPID: &targetPID, TargetIdentity: &targetIdentity, HelperPID: &helperPID, HelperIdentity: &helperIdentity,
			ChildPID: childPID, TargetPIDFD: &targetPIDFD, Timeout: 45 * time.Second,
			ScanReleasePath: scanReleasePath, ReceiptReachedPath: receiptReachedPath, ReceiptReleasePath: receiptReleasePath,
		})
		leaveDaemonForRecovery = cleanup.KeepDaemon
		evidence.TaskID = taskID
		evidence.RunID = key.RunID
		evidence.Generation = key.Generation
		evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, cleanup.JournalSnapshots...)
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, cleanup.ProcessSnapshots...)
		evidence.Notes = append(evidence.Notes, cleanup.Notes...)
		if cleanup.StopReceipt != nil {
			evidence.StopReceiptReached = cleanup.StopReceipt
		}
		if cleanup.Receipt != nil {
			evidence.StopReceiptBeforeRelease = cleanup.Receipt
		}
		if cleanup.ScanRelease != "" {
			evidence.ScanReleaseContents = cleanup.ScanRelease
		}
		if cleanup.ReceiptRelease != "" {
			evidence.StopReceiptRelease = cleanup.ReceiptRelease
		}
		evidence.RawOutput = linuxWitnessLogs(daemon, nil)
		if cleanup.Err != nil {
			evidence.Notes = append(evidence.Notes, "failure cleanup left explicit unresolved containment evidence: "+cleanup.Err.Error())
		}
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
		t.Logf("Linux helper-crash failure cleanup redacted evidence: journals=%#v processes=%#v", cleanup.JournalSnapshots, cleanup.ProcessSnapshots)
		if cleanup.Err != nil {
			t.Errorf("Linux helper-crash witness cleanup could not prove bounded recovery: %v; redacted journal snapshots=%#v process snapshots=%#v", cleanup.Err, cleanup.JournalSnapshots, cleanup.ProcessSnapshots)
		}
	}()
	leaveDaemonForRecovery = true
	submissionAttempted = true
	submitCtx, cancelSubmit := context.WithTimeout(context.Background(), 15*time.Second)
	task, submitErr := submitLinuxWitnessTaskWithReplay(submitCtx, operator, submitIdempotencyKey, submitRequest, 2)
	cancelSubmit()
	if submitErr != nil {
		t.Fatalf("helper-crash witness submission remains unresolved after exact idempotency replay; cleanup will retry only key %q and retain daemon/state if still unknown: %v", submitIdempotencyKey, submitErr)
	}
	taskID = task.TaskID
	evidence.TaskID = taskID
	file, err := waitForLinuxWitnessJournal(target.StateDir, "helper authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	key = file.Journal.Key()
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	evidence.RunID = key.RunID
	evidence.Generation = key.Generation
	evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
	targetPIDFD, err = openLinuxWitnessTargetPIDFD(targetPID, targetIdentity)
	if err != nil {
		t.Fatalf("retain test-owned exact target pidfd before helper crash: %v", err)
	}
	helperPIDFD, err = openLinuxWitnessPIDFD(helperPID, helperIdentity)
	if err != nil {
		t.Fatalf("retain test-owned exact helper pidfd before helper crash: %v", err)
	}
	childPID, err = waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	anchor := linuxWitnessProcessSnapshotAt("target_before_helper_crash", targetPID, targetIdentity)
	child := linuxWitnessProcessSnapshotAt("same_group_descendant_before_helper_crash", childPID, "")
	assertLinuxWitnessSameGroup(t, anchor, child)
	sealedFile, helperReached, mirrorReached, err := waitForLinuxWitnessScanSeal(target.StateDir, key, scanTriggerPath, scanReachedPath, scanReleasePath, "both", 45*time.Second)
	if err != nil {
		t.Fatalf("wait for durable helper/mirror scan seals: %v", err)
	}
	anchor = linuxWitnessProcessSnapshotAt("target_at_clean_scan_boundary", targetPID, targetIdentity)
	child = linuxWitnessProcessSnapshotAt("same_group_descendant_at_clean_scan_boundary", childPID, "")
	assertLinuxWitnessSameGroup(t, anchor, child)
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("target at clean-scan helper-crash boundary: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("same-group descendant at clean-scan helper-crash boundary: %v", err)
	}
	if contents, err := os.ReadFile(scanReleasePath); err != nil {
		t.Fatalf("read clean-scan seal release marker: %v", err)
	} else {
		evidence.ScanReleaseContents = string(contents)
	}
	evidence.ScanHelperReached = helperReached
	evidence.ScanMirrorReached = mirrorReached
	evidence.Stage = "running_at_durable_scan_seal_boundary"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(sealedFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, anchor, child,
		linuxWitnessProcessSnapshotAt("helper_before_helper_crash", helperPID, helperIdentity))
	evidence.Notes = append(evidence.Notes, "helper SIGKILL was issued only after both exact scanner clean results and irreversible seal ACKs were observed in the journal; the sealed mirror must still service explicit cancellation scans")
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemonPID := daemonProcessPID(daemon)
	daemonIdentity, err := platform.ProcessIdentity(daemonPID)
	if err != nil {
		t.Fatalf("read owning daemon identity before SIGSTOP: %v", err)
	}
	if err := daemon.command.Process.Signal(unix.SIGSTOP); err != nil {
		t.Fatalf("SIGSTOP owning daemon before helper crash: %v", err)
	}
	daemonPaused = true
	threadCount, err := waitForLinuxWitnessAllThreadsStopped(daemonPID, daemonIdentity, 3*time.Second)
	if err != nil {
		t.Fatalf("owning daemon did not reach process-wide stopped state before helper crash: %v", err)
	}
	pausedDaemon := linuxWitnessProcessSnapshotAt("daemon_all_threads_stopped_before_helper_crash", daemonPID, daemonIdentity)
	evidence.Notes = append(evidence.Notes, fmt.Sprintf("verified all %d daemon threads in /proc/%d/task reached T/t before helper SIGKILL", threadCount, daemonPID))
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("exact helper before pidfd SIGKILL: %v", err)
	}
	if err := killLinuxWitnessProcessByPIDFD(helperPIDFD); err != nil {
		t.Fatalf("SIGKILL exact Linux supervisor helper pidfd: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Fatalf("killed helper remained live: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("same-group descendant stopped while daemon was paused before mirror takeover: %v", err)
	}
	evidence.Stage = "helper_crashed_before_mirror_stop"
	evidence.Crash = &linuxWitnessExitStatus{RequestedSignal: "SIGKILL", Signaled: true, Signal: "SIGKILL", ExitCode: -1}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		pausedDaemon,
		linuxWitnessProcessSnapshotAt("target_after_helper_crash_before_cancel", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_helper_crash_before_cancel", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_helper_crash", helperPID, helperIdentity))
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	if err := daemon.command.Process.Signal(unix.SIGCONT); err != nil {
		t.Fatalf("SIGCONT owning daemon for mirror takeover: %v", err)
	}
	daemonPaused = false
	cancelLinuxWitnessTask(t, operator, task.TaskID, cancelMutationID)
	receiptJournal, receiptMarker, receipt, err := waitForLinuxWitnessStopReceiptBarrier(target.StateDir, key, targetPID, targetIdentity, helperPID, helperIdentity, receiptReachedPath, 45*time.Second)
	if err != nil {
		t.Fatalf("mirror takeover did not expose a real durable stop receipt before release: %v", err)
	}
	evidence.StopReceiptReached = &receiptMarker
	evidence.StopReceiptBeforeRelease = linuxReplayReceiptSnapshotFromReceipt(receipt)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(receiptJournal))
	evidence.Stage = "mirror_stop_receipt_durable_before_release"
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
		t.Fatalf("release real mirror stop-receipt barrier: %v", err)
	}
	if contents, err := os.ReadFile(receiptReleasePath); err != nil {
		t.Fatalf("read mirror stop-receipt release marker: %v", err)
	} else {
		evidence.StopReceiptRelease = string(contents)
	}
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("mirror takeover did not stop target group: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("mirror takeover did not stop same-group descendant: %v", err)
	}
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 45*time.Second); err != nil {
		t.Fatalf("mirror cleanup did not clear journal: %v", err)
	}
	evidence.Stage = "mirror_stopped_and_cleared"
	if snapshot, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(snapshot))
	} else if err != nil {
		t.Fatalf("read mirror journal after cleanup: %v", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_mirror_stop", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_mirror_stop", childPID, ""))
	leaveDaemonForRecovery = false
	daemon.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	scanCleanupComplete = true
}

type linuxBoundKillResult struct {
	File    linuxWitnessJournalFile
	Journal state.RunJournal
	Exit    linuxWitnessExitStatus
	Err     error
}

func waitAndKillLinuxDaemonAtBoundHandoff(daemon *productionDaemonWitnessProcess, stateDir string, timeout time.Duration) linuxBoundKillResult {
	return waitAndKillLinuxDaemonAtHandoff(daemon, stateDir, timeout, func(journal state.RunJournal) bool {
		return journal.ContainmentHandoff != nil && journal.ContainmentHandoff.SupervisorPID > 0 && journal.ContainmentAuthority == nil
	})
}

func waitAndKillLinuxDaemonAtPreparedHandoff(daemon *productionDaemonWitnessProcess, stateDir string, timeout time.Duration) linuxBoundKillResult {
	return waitAndKillLinuxDaemonAtHandoff(daemon, stateDir, timeout, func(journal state.RunJournal) bool {
		return journal.ContainmentHandoff != nil && journal.ContainmentHandoff.SupervisorPID == 0 && journal.ContainmentHandoff.SupervisorIdentity == "" && journal.ContainmentAuthority == nil
	})
}

func waitAndKillLinuxDaemonAtHandoff(daemon *productionDaemonWitnessProcess, stateDir string, timeout time.Duration, match func(state.RunJournal) bool) linuxBoundKillResult {
	runsDir := filepath.Join(stateDir, "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		return linuxBoundKillResult{Err: err}
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return linuxBoundKillResult{Err: fmt.Errorf("initialize Linux journal watcher: %w", err)}
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, runsDir, unix.IN_CREATE|unix.IN_MOVED_TO|unix.IN_CLOSE_WRITE); err != nil {
		return linuxBoundKillResult{Err: fmt.Errorf("watch Linux journal directory: %w", err)}
	}
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 32<<10)
	for time.Now().Before(deadline) {
		files, scanErr := readLinuxWitnessJournals(stateDir)
		if scanErr != nil {
			return linuxBoundKillResult{Err: scanErr}
		}
		for _, file := range files {
			if match == nil || !match(file.Journal) {
				continue
			}
			exit := killLinuxProductionDaemon(daemon)
			return linuxBoundKillResult{File: file, Journal: file.Journal, Exit: exit}
		}
		_, readErr := unix.Read(fd, buffer)
		if readErr != nil && !errors.Is(readErr, unix.EAGAIN) && !errors.Is(readErr, unix.EINTR) {
			return linuxBoundKillResult{Err: fmt.Errorf("read Linux journal watcher: %w", readErr)}
		}
		runtime.Gosched()
	}
	return linuxBoundKillResult{Err: fmt.Errorf("bound pre-commit handoff did not appear within %s", timeout)}
}

func startLinuxResumeBarrierDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, agentMode, label string, extraEnvironment map[string]string) *productionDaemonWitnessProcess {
	t.Helper()
	stdoutPath := filepath.Join(workDir, label+"-daemon.stdout.log")
	stderrPath := filepath.Join(workDir, label+"-daemon.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open daemon stdout log: %v", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open daemon stderr log: %v", err)
	}
	command := exec.Command(binary, "-config", configPath)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("configure hidden daemon process: %v", err)
	}
	command.Env = witnessEnvironment(enrollmentToken, agentMode)
	for key, value := range extraEnvironment {
		command.Env = setLinuxWitnessEnvironment(command.Env, key, value)
	}
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start production daemon: %v", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
}

func setLinuxWitnessEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	updated := make([]string, 0, len(environment)+1)
	found := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				updated = append(updated, prefix+value)
				found = true
			}
			continue
		}
		updated = append(updated, entry)
	}
	if !found {
		updated = append(updated, prefix+value)
	}
	return updated
}

func startLinuxRecoveryDaemonForCleanup(binary, configPath, workDir, label string) (*productionDaemonWitnessProcess, error) {
	stdoutPath := filepath.Join(workDir, label+"-daemon.stdout.log")
	stderrPath := filepath.Join(workDir, label+"-daemon.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cleanup recovery daemon stdout: %w", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("open cleanup recovery daemon stderr: %w", err)
	}
	command := exec.Command(binary, "-config", configPath)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("configure cleanup recovery daemon: %w", err)
	}
	environment := witnessEnvironment("", "slow")
	environment = setLinuxWitnessEnvironment(environment, linuxProductionContainmentWitnessEnvironment, "1")
	command.Env = environment
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start cleanup recovery daemon: %w", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}, nil
}

func stopLinuxRecoveryDaemonForCleanup(process *productionDaemonWitnessProcess) error {
	if process == nil || process.waited || process.command == nil {
		return nil
	}
	if process.command.Process != nil {
		if err := process.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill cleanup recovery daemon: %w", err)
		}
	}
	waitErr := process.command.Wait()
	process.waited = true
	var closeErr error
	if process.stdout != nil {
		closeErr = errors.Join(closeErr, process.stdout.Close())
	}
	if process.stderr != nil {
		closeErr = errors.Join(closeErr, process.stderr.Close())
	}
	if waitErr != nil {
		processState := process.command.ProcessState
		if processState == nil {
			return errors.Join(fmt.Errorf("wait for cleanup recovery daemon: %w", waitErr), closeErr)
		}
		if status, ok := processState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			return errors.Join(fmt.Errorf("wait for cleanup recovery daemon: %w", waitErr), closeErr)
		}
	}
	return closeErr
}

func cleanupLinuxWitnessRecoveryAfterFailure(binary string, target linuxWitnessTarget, workDir string, key state.RunKey, targetPID int, targetIdentity string, helperPID int, helperIdentity string, timeout time.Duration, releasePaths ...string) error {
	var cleanupErr error
	for _, path := range releasePaths {
		if err := writeLinuxWitnessReleaseMarker(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if key.RunID == "" || key.Generation <= 0 {
		files, err := readLinuxWitnessJournals(target.StateDir)
		if err != nil {
			return errors.Join(cleanupErr, fmt.Errorf("find bound journal for cleanup recovery: %w", err))
		}
		var unresolved []linuxWitnessJournalFile
		for _, file := range files {
			if file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentAuthority != nil || file.Journal.HasProcessDetails() {
				unresolved = append(unresolved, file)
			}
		}
		if len(unresolved) != 1 {
			return errors.Join(cleanupErr, fmt.Errorf("task was submitted but cleanup cannot locate exactly one owner journal: found %d candidate bound journals", len(unresolved)))
		}
		key = unresolved[0].Journal.Key()
		if key.RunID == "" || key.Generation <= 0 {
			return errors.Join(cleanupErr, errors.New("cleanup owner journal has no exact run key"))
		}
	}
	if key.RunID != "" {
		current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("read bound journal for cleanup recovery: %w", err))
		} else if found {
			currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity := linuxWitnessOwnerIDs(current.Journal)
			ownerMatches := true
			if targetPID != 0 && (currentTargetPID != targetPID || currentTargetIdentity != targetIdentity || currentHelperPID != helperPID || currentHelperIdentity != helperIdentity) {
				ownerMatches = false
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup journal owner changed: target=(%d,%q) helper=(%d,%q), expected target=(%d,%q) helper=(%d,%q)", currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity))
			} else if targetPID == 0 {
				targetPID, targetIdentity, helperPID, helperIdentity = currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity
			}
			if ownerMatches && (current.Journal.ContainmentHandoff != nil || current.Journal.ContainmentAuthority != nil || current.Journal.HasProcessDetails()) {
				recovery, startErr := startLinuxRecoveryDaemonForCleanup(binary, target.ConfigPath, workDir, "failure-recovery")
				if startErr != nil {
					cleanupErr = errors.Join(cleanupErr, fmt.Errorf("start bound cleanup recovery daemon: %w", startErr))
				} else {
					if waitErr := waitForLinuxWitnessJournalReleased(target.StateDir, key, timeout); waitErr != nil {
						cleanupErr = errors.Join(cleanupErr, fmt.Errorf("bound recovery did not clear journal: %w", waitErr))
					}
					if stopErr := stopLinuxRecoveryDaemonForCleanup(recovery); stopErr != nil {
						cleanupErr = errors.Join(cleanupErr, fmt.Errorf("stop cleanup recovery daemon: %w", stopErr))
					}
				}
			}
		} else {
			cleanupErr = errors.Join(cleanupErr, errors.New("exact bound journal is absent before cleanup recovery"))
		}
		if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 5*time.Second); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("containment journal remains after cleanup recovery: %w", err))
		}
	}
	if helperPID > 0 {
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("exact helper %d/%q remains after cleanup recovery: %w", helperPID, helperIdentity, err))
		}
	}
	if targetPID > 0 {
		if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 15*time.Second); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("exact target group %d/%q remains after cleanup recovery: %w", targetPID, targetIdentity, err))
		}
	}
	return cleanupErr
}

type linuxWitnessFailureCleanupEvidence struct {
	JournalSnapshots []linuxWitnessJournalSnapshot
	ProcessSnapshots []linuxWitnessProcessSnapshot
	StopReceipt      *linuxWitnessStopReceiptBarrierSnapshot
	Receipt          *linuxReplayReceiptSnapshot
	ScanRelease      string
	ReceiptRelease   string
	Notes            []string
	Err              error
	KeepDaemon       bool
}

type linuxWitnessHelperCrashCleanupRequest struct {
	Daemon               *productionDaemonWitnessProcess
	DaemonPaused         *bool
	Operator             *control.OperatorClient
	TaskID               *string
	SubmissionAttempted  *bool
	SubmitIdempotencyKey string
	SubmitRequest        protocol.TaskSubmitRequest
	CancelMutationID     string
	Target               linuxWitnessTarget
	Key                  *state.RunKey
	TargetPID            *int
	TargetIdentity       *string
	HelperPID            *int
	HelperIdentity       *string
	ChildPID             int
	TargetPIDFD          *int
	Timeout              time.Duration
	ScanReleasePath      string
	ReceiptReachedPath   string
	ReceiptReleasePath   string
}

func submitLinuxWitnessTaskWithReplay(ctx context.Context, operator *control.OperatorClient, idempotencyKey string, request protocol.TaskSubmitRequest, attempts int) (protocol.Task, error) {
	if ctx == nil || operator == nil || idempotencyKey == "" || attempts <= 0 {
		return protocol.Task{}, errors.New("bounded task submission requires context, operator, idempotency key, and positive attempts")
	}
	var lastErr error
	for attempt := range attempts {
		task, err := operator.SubmitTask(ctx, idempotencyKey, request)
		if err == nil {
			if task.TaskID == "" {
				return protocol.Task{}, errors.New("idempotent task submission returned no task identity")
			}
			return task, nil
		}
		lastErr = err
		if err := ctx.Err(); err != nil {
			break
		}
		if attempt+1 < attempts {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return protocol.Task{}, fmt.Errorf("idempotent submission replay deadline: %w", ctx.Err())
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("submission attempts exhausted")
	}
	return protocol.Task{}, fmt.Errorf("same-key task submission remains unresolved after %d identical attempts: %w", attempts, lastErr)
}

type linuxWitnessSubmitReplayTransport struct {
	base             http.RoundTripper
	responsesToDrop  int
	droppedResponses int
	firstAcceptedID  string
	acceptedTaskIDs  []string
	idempotencyKeys  []string
	requestBodies    [][]byte
}

func (transport *linuxWitnessSubmitReplayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/v1/tasks") {
		return transport.base.RoundTrip(request)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	transport.idempotencyKeys = append(transport.idempotencyKeys, request.Header.Get("Idempotency-Key"))
	transport.requestBodies = append(transport.requestBodies, append([]byte(nil), body...))
	response, err := transport.base.RoundTrip(request)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response, err
	}
	contents, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	var accepted struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(contents, &accepted); err != nil || accepted.TaskID == "" {
		if err == nil {
			err = errors.New("accepted task response has no task_id")
		}
		return nil, err
	}
	if transport.firstAcceptedID == "" {
		transport.firstAcceptedID = accepted.TaskID
	}
	transport.acceptedTaskIDs = append(transport.acceptedTaskIDs, accepted.TaskID)
	if transport.droppedResponses < transport.responsesToDrop {
		transport.droppedResponses++
		return nil, errors.New("simulated lost response after live Control accepted SubmitTask")
	}
	response.Body = io.NopCloser(bytes.NewReader(contents))
	return response, nil
}

func runLinuxWitnessSubmitReplayIntegration(t *testing.T, environment e2eEnvironment) {
	t.Helper()
	t.Run("one_response_lost_then_same_key_readback", func(t *testing.T) {
		runLinuxWitnessSubmitReplayCase(t, environment, 1, false)
	})
	t.Run("both_initial_accepted_responses_lost_cleanup_replays", func(t *testing.T) {
		runLinuxWitnessSubmitReplayCase(t, environment, 2, true)
	})
}

func runLinuxWitnessSubmitReplayCase(t *testing.T, environment e2eEnvironment, dropResponses int, cleanupReplay bool) {
	t.Helper()
	baseClient := localHTTPClient(5 * time.Second)
	replayTransport := &linuxWitnessSubmitReplayTransport{base: baseClient.Transport, responsesToDrop: dropResponses}
	httpClient := &http.Client{Transport: replayTransport, Timeout: baseClient.Timeout}
	operator, err := control.NewOperatorClient(environment.baseURL, environment.operatorToken, httpClient)
	if err != nil {
		t.Fatalf("create operator client for live submit replay: %v", err)
	}
	profile, workspace := unique("linux-submit-replay-profile"), unique("linux-submit-replay-workspace")
	request := protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal: unique("linux-submit-replay-work"), AgentProfile: profile, Workspace: workspace, Input: json.RawMessage(`{"mode":"slow"}`),
	}}
	idempotencyKey := unique("linux-submit-replay")
	cancelMutationID := unique("linux-submit-replay-cancel")
	taskID := ""
	t.Cleanup(func() {
		if taskID == "" && len(replayTransport.acceptedTaskIDs) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			resolved, err := submitLinuxWitnessTaskWithReplay(ctx, operator, idempotencyKey, request, 2)
			cancel()
			if err != nil {
				t.Errorf("resolve accepted task after lost submit response during cleanup: %v", err)
			} else {
				taskID = resolved.TaskID
			}
		}
		ids := make(map[string]struct{}, len(replayTransport.acceptedTaskIDs)+1)
		for _, acceptedID := range replayTransport.acceptedTaskIDs {
			if acceptedID != "" {
				ids[acceptedID] = struct{}{}
			}
		}
		if taskID != "" {
			ids[taskID] = struct{}{}
		}
		for id := range ids {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			current, err := operator.GetTask(ctx, id)
			if err != nil {
				cancel()
				t.Errorf("read accepted submit-replay task %q during cleanup: %v", id, err)
				continue
			}
			if current.State != "cancelled" && current.State != "completed" && current.State != "failed" {
				cleanupCancelID := cancelMutationID
				if id != taskID {
					cleanupCancelID = unique("linux-submit-replay-cleanup-cancel")
				}
				if _, err := operator.CreateTaskCommand(ctx, id, cleanupCancelID, protocol.TaskCommandRequest{Kind: "cancel"}); err != nil {
					cancel()
					t.Errorf("cancel accepted submit-replay task %q during cleanup: %v", id, err)
					continue
				}
				for {
					current, err = operator.GetTask(ctx, id)
					if err == nil && (current.State == "cancelled" || current.State == "completed" || current.State == "failed") {
						break
					}
					if ctx.Err() != nil {
						t.Errorf("accepted submit-replay task %q did not settle during cleanup: %v", id, ctx.Err())
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
			cancel()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	task, submitErr := submitLinuxWitnessTaskWithReplay(ctx, operator, idempotencyKey, request, 2)
	cancel()
	if cleanupReplay {
		if submitErr == nil || replayTransport.droppedResponses != 2 {
			t.Fatalf("initial two-attempt submission unexpectedly returned after only two configured response losses")
		}
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		task, submitErr = submitLinuxWitnessTaskWithReplay(ctx, operator, idempotencyKey, request, 2)
		cancel()
	}
	if submitErr != nil {
		t.Fatalf("live Control submit replay after accepted response loss: %v", submitErr)
	}
	taskID = task.TaskID
	if err := assertLinuxWitnessSubmitReplay(replayTransport, idempotencyKey, task.TaskID, dropResponses, cleanupReplay); err != nil {
		t.Fatal(err)
	}
	if task.State != "queued" {
		t.Fatalf("live submit replay task state = %q, want queued work without a matching runtime", task.State)
	}
	cancelLinuxWitnessTask(t, operator, task.TaskID, cancelMutationID)
	waitForTask(t, operator, task.TaskID, 15*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	if taskID != replayTransport.firstAcceptedID {
		t.Fatalf("cancelled task ID %q differs from first accepted task ID %q", taskID, replayTransport.firstAcceptedID)
	}
}

func assertLinuxWitnessSubmitReplay(transport *linuxWitnessSubmitReplayTransport, idempotencyKey, taskID string, dropResponses int, cleanupReplay bool) error {
	if transport.droppedResponses != dropResponses || len(transport.acceptedTaskIDs) != dropResponses+1 {
		return fmt.Errorf("live Control accepted responses dropped=%d task IDs=%d, want %d dropped plus final readback", transport.droppedResponses, len(transport.acceptedTaskIDs), dropResponses)
	}
	if transport.firstAcceptedID == "" || transport.firstAcceptedID != taskID {
		return fmt.Errorf("live Control task ID=%q, first accepted TaskID=%q; want idempotent readback of the original task", taskID, transport.firstAcceptedID)
	}
	for _, acceptedID := range transport.acceptedTaskIDs {
		if acceptedID != taskID {
			return fmt.Errorf("live Control returned multiple task identities for one idempotency key: %q versus %q", acceptedID, taskID)
		}
	}
	wantRequests := 2
	if cleanupReplay {
		wantRequests = 3
	}
	if len(transport.idempotencyKeys) != wantRequests {
		return fmt.Errorf("live Control submit request count = %d, want %d", len(transport.idempotencyKeys), wantRequests)
	}
	for _, key := range transport.idempotencyKeys {
		if key != idempotencyKey {
			return fmt.Errorf("live Control submit idempotency key = %q, want %q", key, idempotencyKey)
		}
	}
	if len(transport.requestBodies) != wantRequests {
		return fmt.Errorf("live Control submit body count = %d, want %d", len(transport.requestBodies), wantRequests)
	}
	for _, body := range transport.requestBodies[1:] {
		if !bytes.Equal(transport.requestBodies[0], body) {
			return errors.New("live Control submit replay changed the accepted request body")
		}
	}
	return nil
}

type linuxWitnessCleanupOwner struct {
	Key            state.RunKey
	TargetPID      int
	TargetIdentity string
	HelperPID      int
	HelperIdentity string
	Journal        *linuxWitnessJournalFile
	JournalReadErr error
	NoOwner        bool
}

func waitForLinuxWitnessCleanupOwner(ctx context.Context, target linuxWitnessTarget, expected state.RunKey, targetPID int, targetIdentity string, helperPID int, helperIdentity string, operator *control.OperatorClient, taskID string) (linuxWitnessCleanupOwner, error) {
	if ctx == nil || operator == nil || taskID == "" {
		return linuxWitnessCleanupOwner{}, errors.New("submitted-task cleanup requires a context, operator, and task identity")
	}
	if expected.RunID != "" && expected.Generation > 0 && targetPID > 0 && targetIdentity != "" && helperPID > 0 && helperIdentity != "" {
		owner := linuxWitnessCleanupOwner{
			Key: expected, TargetPID: targetPID, TargetIdentity: targetIdentity,
			HelperPID: helperPID, HelperIdentity: helperIdentity,
		}
		file, found, err := readLinuxWitnessJournalForKey(target.StateDir, expected)
		if err != nil {
			owner.JournalReadErr = err
			return owner, nil
		}
		if !found {
			return owner, nil
		}
		gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(file.Journal)
		if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
			return linuxWitnessCleanupOwner{}, fmt.Errorf("exact journal owner changed: target=(%d,%q) helper=(%d,%q), expected target=(%d,%q) helper=(%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
		}
		owner.Journal = &file
		return owner, nil
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return linuxWitnessCleanupOwner{}, errors.New("submitted-task cleanup owner lookup requires a deadline")
	}
	var task protocol.Task
	var taskErr error
	var nextTaskRead time.Time
	for {
		files, err := readLinuxWitnessJournals(target.StateDir)
		if err != nil {
			return linuxWitnessCleanupOwner{}, fmt.Errorf("scan local journals for the submitted task owner: %w", err)
		}
		var candidates []linuxWitnessJournalFile
		for _, file := range files {
			if expected.RunID != "" && file.Journal.Key() == expected {
				candidates = append(candidates, file)
				continue
			}
			if expected.RunID == "" && linuxWitnessJournalHasPhysicalOwnerEvidence(file.Journal) {
				candidates = append(candidates, file)
			}
		}
		if expected.RunID == "" && len(files) == 1 && len(candidates) == 0 {
			candidates = append(candidates, files[0])
		}
		if len(candidates) > 1 {
			return linuxWitnessCleanupOwner{}, fmt.Errorf("submitted task has %d candidate local owner journals", len(candidates))
		}
		var candidate *linuxWitnessJournalFile
		if len(candidates) == 1 {
			candidate = &candidates[0]
			targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(candidate.Journal)
			if targetPID > 0 && targetIdentity != "" && helperPID > 0 && helperIdentity != "" {
				return linuxWitnessCleanupOwner{
					Key: candidate.Journal.Key(), TargetPID: targetPID, TargetIdentity: targetIdentity,
					HelperPID: helperPID, HelperIdentity: helperIdentity, Journal: candidate,
				}, nil
			}
		}

		if nextTaskRead.IsZero() || !time.Now().Before(nextTaskRead) {
			task, taskErr = operator.GetTask(ctx, taskID)
			nextTaskRead = time.Now().Add(250 * time.Millisecond)
		}
		if taskErr == nil && task.State == "cancelled" {
			markersAbsent, markerErr := linuxWitnessTargetMarkersAbsent(target)
			if markerErr != nil {
				return linuxWitnessCleanupOwner{}, fmt.Errorf("inspect target markers while locating submitted owner: %w", markerErr)
			}
			noPhysicalOwner := len(files) == 0 || (len(files) == 1 && candidate != nil && !linuxWitnessJournalHasPhysicalOwnerEvidence(candidate.Journal))
			if markersAbsent && noPhysicalOwner {
				if candidate != nil {
					if err := waitForLinuxWitnessJournalReleased(target.StateDir, candidate.Journal.Key(), time.Until(deadline)); err != nil {
						return linuxWitnessCleanupOwner{}, fmt.Errorf("prove launch-intent journal released before classifying no owner: %w", err)
					}
				}
				owner := linuxWitnessCleanupOwner{NoOwner: true}
				if candidate != nil {
					owner.Key = candidate.Journal.Key()
					owner.Journal = candidate
				}
				return owner, nil
			}
		}
		if err := ctx.Err(); err != nil {
			if taskErr != nil {
				return linuxWitnessCleanupOwner{}, fmt.Errorf("owner remained unlocated at cleanup deadline (task read %T): %w", taskErr, err)
			}
			return linuxWitnessCleanupOwner{}, fmt.Errorf("owner remained unlocated at cleanup deadline with task state %q: %w", task.State, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func linuxWitnessTargetMarkersAbsent(target linuxWitnessTarget) (bool, error) {
	for _, path := range []string{target.TargetMarker, target.DescendantMarker} {
		if path == "" {
			return false, errors.New("target process marker path is unavailable")
		}
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

func cleanupLinuxHelperCrashAfterFailure(request linuxWitnessHelperCrashCleanupRequest) linuxWitnessFailureCleanupEvidence {
	daemon, daemonPaused, operator := request.Daemon, request.DaemonPaused, request.Operator
	var taskID string
	if request.TaskID != nil {
		taskID = *request.TaskID
	}
	cancelMutationID := request.CancelMutationID
	target, timeout := request.Target, request.Timeout
	childPID := request.ChildPID
	var key state.RunKey
	var targetPID, helperPID, targetPIDFD int
	var targetIdentity, helperIdentity string
	if request.Key != nil {
		key = *request.Key
	}
	if request.TargetPID != nil {
		targetPID = *request.TargetPID
	}
	if request.TargetIdentity != nil {
		targetIdentity = *request.TargetIdentity
	}
	if request.HelperPID != nil {
		helperPID = *request.HelperPID
	}
	if request.HelperIdentity != nil {
		helperIdentity = *request.HelperIdentity
	}
	if request.TargetPIDFD != nil {
		targetPIDFD = *request.TargetPIDFD
	}
	scanReleasePath, receiptReachedPath, receiptReleasePath := request.ScanReleasePath, request.ReceiptReachedPath, request.ReceiptReleasePath
	result := linuxWitnessFailureCleanupEvidence{KeepDaemon: true}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	deadline := time.Now().Add(timeout)
	addErr := func(err error) {
		if err != nil {
			result.Err = errors.Join(result.Err, err)
		}
	}
	result.ProcessSnapshots = append(result.ProcessSnapshots, linuxWitnessProcessSnapshotAt("failure_cleanup_before_resume", daemonProcessPID(daemon), ""))
	if daemonPaused != nil && *daemonPaused {
		if daemon == nil || daemon.command == nil || daemon.command.Process == nil {
			addErr(errors.New("paused owning daemon handle is unavailable during failure cleanup"))
		} else {
			var resumeErr error
			for range 3 {
				resumeErr = daemon.command.Process.Signal(unix.SIGCONT)
				if resumeErr == nil || errors.Is(resumeErr, os.ErrProcessDone) || !errors.Is(resumeErr, unix.EINTR) {
					break
				}
			}
			if resumeErr != nil && !errors.Is(resumeErr, os.ErrProcessDone) {
				addErr(fmt.Errorf("resume paused owning daemon before failure recovery: %w", resumeErr))
			} else {
				*daemonPaused = false
				result.Notes = append(result.Notes, "failure cleanup released the daemon SIGSTOP before cancellation/recovery")
			}
		}
	}
	if scanReleasePath != "" {
		if err := writeLinuxWitnessReleaseMarker(scanReleasePath); err != nil {
			addErr(fmt.Errorf("release durable scan barrier during failure cleanup: %w", err))
		}
	}

	if operator == nil || request.TaskID == nil {
		addErr(errors.New("failure cleanup cannot resolve the submitted task without its operator and task identity slot"))
		result.KeepDaemon = true
		return result
	}
	if taskID == "" {
		if request.SubmissionAttempted == nil || !*request.SubmissionAttempted || request.SubmitIdempotencyKey == "" {
			addErr(errors.New("submission outcome is unresolved and no exact idempotency key is available for replay"))
			result.KeepDaemon = true
			return result
		}
		submitCtx, cancelSubmit := context.WithDeadline(context.Background(), deadline)
		task, submitErr := submitLinuxWitnessTaskWithReplay(submitCtx, operator, request.SubmitIdempotencyKey, request.SubmitRequest, 3)
		cancelSubmit()
		if submitErr != nil {
			addErr(fmt.Errorf("submitted task remains unresolved after replaying the exact idempotency key/request: %w", submitErr))
			result.KeepDaemon = true
			return result
		}
		taskID = task.TaskID
		*request.TaskID = taskID
		result.Notes = append(result.Notes, "resolved submit response by replaying the identical task request under its original idempotency key")
	}
	cancelTimeout := min(5*time.Second, time.Until(deadline))
	cancelCtx, cancelRequest := context.WithTimeout(context.Background(), cancelTimeout)
	cancelErr := requestLinuxWitnessTaskCancellation(cancelCtx, operator, taskID, cancelMutationID)
	cancelRequest()
	if cancelErr != nil {
		result.Notes = append(result.Notes, fmt.Sprintf("failure cleanup cancel request returned %T; details omitted and exact owner recovery is still required", cancelErr))
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	owner, ownerErr := waitForLinuxWitnessCleanupOwner(ctx, target, key, targetPID, targetIdentity, helperPID, helperIdentity, operator, taskID)
	if ownerErr != nil {
		addErr(fmt.Errorf("submitted task owner could not be resolved for cleanup: %w", ownerErr))
		result.KeepDaemon = true
		if files, err := readLinuxWitnessJournals(target.StateDir); err == nil {
			for _, file := range files {
				result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
			}
		}
		result.ProcessSnapshots = append(result.ProcessSnapshots,
			linuxWitnessProcessSnapshotAt("failure_cleanup_target_unresolved", targetPID, targetIdentity),
			linuxWitnessProcessSnapshotAt("failure_cleanup_helper_unresolved", helperPID, helperIdentity))
		return result
	}
	if owner.NoOwner {
		result.KeepDaemon = true
		addErr(errors.New("submitted task was cancelled but no exact owner journal was located; physical stop remains unresolved"))
		result.Notes = append(result.Notes, "no exact owner journal or target/descendant process marker was located; no receipt or clear is claimed and the daemon remains alive")
		if owner.Journal != nil {
			if request.Key != nil {
				*request.Key = owner.Key
			}
			result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(*owner.Journal))
		}
		return result
	}
	if targetPID > 0 && (targetPID != owner.TargetPID || targetIdentity != owner.TargetIdentity || helperPID != owner.HelperPID || helperIdentity != owner.HelperIdentity) {
		result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(*owner.Journal))
		addErr(fmt.Errorf("resolved cleanup owner changed: target=(%d,%q) helper=(%d,%q), expected target=(%d,%q) helper=(%d,%q)", owner.TargetPID, owner.TargetIdentity, owner.HelperPID, owner.HelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity))
		result.KeepDaemon = true
		return result
	}
	key, targetPID, targetIdentity = owner.Key, owner.TargetPID, owner.TargetIdentity
	helperPID, helperIdentity = owner.HelperPID, owner.HelperIdentity
	if request.Key != nil {
		*request.Key = key
	}
	if request.TargetPID != nil {
		*request.TargetPID = targetPID
	}
	if request.TargetIdentity != nil {
		*request.TargetIdentity = targetIdentity
	}
	if request.HelperPID != nil {
		*request.HelperPID = helperPID
	}
	if request.HelperIdentity != nil {
		*request.HelperIdentity = helperIdentity
	}
	if owner.Journal != nil {
		result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(*owner.Journal))
		if receipt := linuxWitnessJournalStopReceipt(owner.Journal.Journal); receipt != nil {
			result.Receipt = linuxReplayReceiptSnapshotFromReceipt(*receipt)
		}
	} else if owner.JournalReadErr != nil {
		result.Notes = append(result.Notes, fmt.Sprintf("known exact owner journal read returned %T; prior owner evidence is retained and exact receipt readback is still required", owner.JournalReadErr))
	} else if key.RunID != "" && targetPID > 0 && helperPID > 0 {
		result.Notes = append(result.Notes, "known exact process identities remain from prior journal evidence although the journal file is absent; no clear is inferred")
	}
	if targetPIDFD < 0 {
		if fd, err := openLinuxWitnessTargetPIDFD(targetPID, targetIdentity); err == nil {
			targetPIDFD = fd
			if request.TargetPIDFD != nil {
				*request.TargetPIDFD = fd
			}
		} else {
			result.Notes = append(result.Notes, fmt.Sprintf("test-owned target pidfd acquisition returned %T; normal recovery is still attempted", err))
		}
	}
	result.ProcessSnapshots = append(result.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("failure_cleanup_target_before_recovery", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("failure_cleanup_helper_before_recovery", helperPID, helperIdentity))
	if linuxWitnessDaemonAlive(daemon) {
		journal, marker, receipt, err := waitForLinuxWitnessStopReceiptBarrier(target.StateDir, key, targetPID, targetIdentity, helperPID, helperIdentity, receiptReachedPath, time.Until(deadline))
		if err != nil {
			addErr(fmt.Errorf("owning daemon did not expose the real stop receipt before cleanup deadline: %w", err))
		} else {
			result.StopReceipt = &marker
			result.Receipt = linuxReplayReceiptSnapshotFromReceipt(receipt)
			result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(journal))
			result.Notes = append(result.Notes, "failure cleanup observed the production daemon's identity-bound durable stop receipt before releasing its receipt barrier")
			if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
				addErr(fmt.Errorf("release real stop-receipt barrier during failure cleanup: %w", err))
			} else {
				if err := waitForLinuxWitnessTaskCancelled(ctx, operator, taskID); err != nil {
					addErr(fmt.Errorf("task did not reach cancelled after receipt-barrier release: %w", err))
				}
				if remaining := time.Until(deadline); remaining > 0 {
					if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, remaining); err != nil {
						addErr(fmt.Errorf("original target group did not stop after real receipt: %w", err))
					}
				} else {
					addErr(errors.New("cleanup deadline expired before original target group proof"))
				}
				if childPID > 0 {
					if remaining := time.Until(deadline); remaining > 0 {
						if err := waitForLinuxWitnessIdentityGone(childPID, "", remaining); err != nil {
							addErr(fmt.Errorf("same-group descendant %d remained after real receipt: %w", childPID, err))
						}
					} else {
						addErr(errors.New("cleanup deadline expired before descendant exit proof"))
					}
				}
				if remaining := time.Until(deadline); remaining > 0 {
					if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, remaining); err != nil {
						addErr(fmt.Errorf("exact helper did not exit after real receipt: %w", err))
					}
				} else {
					addErr(errors.New("cleanup deadline expired before exact helper exit proof"))
				}
				if remaining := time.Until(deadline); remaining > 0 {
					if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, remaining); err != nil {
						addErr(fmt.Errorf("journal did not clear after observed receipt and recovery: %w", err))
					}
				} else {
					addErr(errors.New("cleanup deadline expired before journal clear proof"))
				}
			}
		}
	} else {
		addErr(errors.New("owning daemon is not alive to perform receipt recovery"))
	}

	if result.Err == nil {
		if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err != nil {
			addErr(fmt.Errorf("read journal after receipt recovery: %w", err))
		} else if found {
			result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
			if current.Journal.ContainmentAuthority != nil || current.Journal.ContainmentHandoff != nil || current.Journal.HasProcessDetails() {
				addErr(errors.New("journal still contains physical containment after receipt recovery"))
			}
		}
		if result.Err == nil {
			result.ProcessSnapshots = append(result.ProcessSnapshots,
				linuxWitnessProcessSnapshotAt("failure_cleanup_target_after_receipt_recovery", targetPID, targetIdentity),
				linuxWitnessProcessSnapshotAt("failure_cleanup_helper_after_receipt_recovery", helperPID, helperIdentity))
			result.Notes = append(result.Notes, "failure cleanup proved real receipt recovery, original group absence, helper exit, and journal clear")
			if contents, err := os.ReadFile(scanReleasePath); err == nil {
				result.ScanRelease = string(contents)
			}
			if contents, err := os.ReadFile(receiptReleasePath); err == nil {
				result.ReceiptRelease = string(contents)
			}
			result.KeepDaemon = false
			return result
		}
	}

	result.Notes = append(result.Notes, "receipt recovery did not settle; fallback signals only the retained pidfd-owned original group and does not synthesize a receipt, add a protocol release, or clear the journal")
	var groupProof error
	if targetPID <= 0 || targetIdentity == "" {
		groupProof = errors.New("exact target PID and identity are unavailable")
	}
	if targetPID > 0 && targetIdentity != "" && targetPIDFD >= 0 {
		groupProof = stopLinuxWitnessGroupByPIDFD(targetPIDFD, targetPID, targetIdentity, 15*time.Second)
	} else if targetPID > 0 && targetIdentity != "" {
		groupProof = waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 2*time.Second)
	}
	if groupProof != nil {
		result.KeepDaemon = true
		addErr(fmt.Errorf("could not prove original pidfd-owned process group absent; helper is left untouched: %w", groupProof))
	} else {
		result.Notes = append(result.Notes, "the exact original process group was proved absent; fallback did not manufacture or release a receipt")
		childGone := true
		if childPID > 0 {
			if err := waitForLinuxWitnessIdentityGone(childPID, "", 10*time.Second); err != nil {
				childGone = false
				result.KeepDaemon = true
				addErr(fmt.Errorf("same-group descendant %d remains after original-group stop: %w", childPID, err))
			}
		}
		if childGone && helperPID > 0 && helperIdentity != "" {
			if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 2*time.Second); err == nil {
				result.Notes = append(result.Notes, "exact helper was already gone after original-group proof")
			} else {
				result.KeepDaemon = true
				result.Notes = append(result.Notes, "receipt remains unresolved; healthy exact helper was left alive for owner recovery")
			}
		} else {
			result.KeepDaemon = true
			if !childGone {
				addErr(errors.New("exact helper was left alive because descendant exit was not proven"))
			} else {
				addErr(errors.New("exact helper identity is unavailable after original group proof"))
			}
		}
	}
	if key.RunID != "" {
		if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err != nil {
			addErr(fmt.Errorf("preserve unresolved journal snapshot after fallback: %w", err))
		} else if found {
			result.JournalSnapshots = append(result.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
			if receipt := linuxWitnessJournalStopReceipt(current.Journal); receipt != nil {
				result.Receipt = linuxReplayReceiptSnapshotFromReceipt(*receipt)
			}
		} else if result.Receipt == nil {
			addErr(errors.New("journal disappeared without cleanup-observed receipt; clear cannot be claimed as proven"))
		} else {
			addErr(errors.New("journal disappeared during failure fallback; receipt was observed but fallback clear cannot be claimed as proven"))
		}
	}
	if receiptReachedPath != "" {
		if contents, err := os.ReadFile(receiptReachedPath); err == nil {
			marker, snapshot, decodeErr := decodeLinuxWitnessStopReceiptBarrierMarker(contents)
			if decodeErr != nil {
				addErr(fmt.Errorf("preserve redacted receipt-barrier marker after fallback: %w", decodeErr))
			} else if marker.RunID != key.RunID || marker.Generation != key.Generation || marker.PID != targetPID || marker.Identity != targetIdentity || result.Receipt == nil || *result.Receipt != *linuxReplayReceiptSnapshotFromReceipt(marker.Receipt) {
				addErr(errors.New("receipt-barrier marker after fallback does not match the exact observed run, target, and durable receipt"))
			} else {
				result.StopReceipt = &snapshot
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			addErr(fmt.Errorf("read receipt-barrier marker after fallback: %w", err))
		}
	}
	if contents, err := os.ReadFile(scanReleasePath); err == nil {
		result.ScanRelease = string(contents)
	}
	if contents, err := os.ReadFile(receiptReleasePath); err == nil {
		result.ReceiptRelease = string(contents)
	}
	result.ProcessSnapshots = append(result.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("failure_cleanup_target_after_pidfd_fallback", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("failure_cleanup_child_after_pidfd_fallback", childPID, ""),
		linuxWitnessProcessSnapshotAt("failure_cleanup_helper_after_pidfd_fallback", helperPID, helperIdentity))
	if result.Err == nil {
		result.Err = errors.New("cleanup fallback stopped the original group without a proven receipt/clear")
	}
	return result
}

func linuxWitnessJournalStopReceipt(journal state.RunJournal) *authority.StopReceipt {
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil {
		return journal.ContainmentAuthority.StopReceipt
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.StopReceipt != nil {
		return journal.ContainmentHandoff.StopReceipt
	}
	return nil
}

func linuxWitnessJournalHasPhysicalOwnerEvidence(journal state.RunJournal) bool {
	return journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentScans != nil ||
		journal.ContainmentUnproven || journal.HasProcessDetails()
}

func requestLinuxWitnessTaskCancellation(ctx context.Context, operator *control.OperatorClient, taskID, mutationID string) error {
	if ctx == nil || operator == nil || taskID == "" || mutationID == "" {
		return errors.New("task cancellation requires context, operator, task identity, and mutation identity")
	}
	task, err := operator.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("read task before cleanup cancellation: %w", err)
	}
	if task.State == "cancelled" {
		return nil
	}
	switch task.State {
	case "completed", "failed":
		return fmt.Errorf("submitted task is already terminal in state %q", task.State)
	}
	if _, err := operator.CreateTaskCommand(ctx, taskID, mutationID, protocol.TaskCommandRequest{Kind: "cancel"}); err != nil {
		return fmt.Errorf("request real task cancellation: %w", err)
	}
	return nil
}

func cancelLinuxWitnessTask(t *testing.T, operator *control.OperatorClient, taskID, mutationID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := requestLinuxWitnessTaskCancellation(ctx, operator, taskID, mutationID); err != nil {
		t.Fatalf("cancel task %q with stable mutation identity: %v", taskID, err)
	}
}

func waitForLinuxWitnessTaskCancelled(ctx context.Context, operator *control.OperatorClient, taskID string) error {
	if ctx == nil || operator == nil || taskID == "" {
		return errors.New("task cancellation proof requires context, operator, and task identity")
	}
	for {
		task, err := operator.GetTask(ctx, taskID)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("wait for cancelled task %q: %w", taskID, ctx.Err())
			}
		} else if task.State == "cancelled" {
			return nil
		} else if task.State == "completed" || task.State == "failed" {
			return fmt.Errorf("task reached terminal state %q instead of cancelled", task.State)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for task %q to become cancelled: %w", taskID, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func waitForLinuxWitnessStopReceiptBarrier(stateDir string, key state.RunKey, targetPID int, targetIdentity string, helperPID int, helperIdentity string, reachedPath string, timeout time.Duration) (linuxWitnessJournalFile, linuxWitnessStopReceiptBarrierSnapshot, authority.StopReceipt, error) {
	if key.RunID == "" || key.Generation <= 0 || targetPID <= 0 || targetIdentity == "" || helperPID <= 0 || helperIdentity == "" {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, errors.New("stop-receipt barrier requires the exact run and owner identities")
	}
	deadline := time.Now().Add(timeout)
	if err := waitForLinuxWitnessMarker(reachedPath, time.Until(deadline)); err != nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, err
	}
	contents, err := os.ReadFile(reachedPath)
	if err != nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, fmt.Errorf("read real stop-receipt barrier marker: %w", err)
	}
	marker, markerSnapshot, err := decodeLinuxWitnessStopReceiptBarrierMarker(contents)
	if err != nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, fmt.Errorf("decode real stop-receipt barrier marker: %w", err)
	}
	file, err := waitForLinuxWitnessJournal(stateDir, "real stop receipt before release", time.Until(deadline), func(value linuxWitnessJournalFile) bool {
		return value.Journal.Key() == key && linuxWitnessJournalStopReceipt(value.Journal) != nil && value.Journal.HasProcessDetails()
	})
	if err != nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, err
	}
	authorityValue := file.Journal.ContainmentAuthority
	if authorityValue == nil && file.Journal.ContainmentHandoff == nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, errors.New("receipt journal has no exact containment owner")
	}
	receipt := linuxWitnessJournalStopReceipt(file.Journal)
	if receipt == nil {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, errors.New("receipt journal lost the durable stop receipt")
	}
	valid := false
	if authorityValue != nil {
		valid = receipt.ValidFor(*authorityValue)
	} else {
		valid = receipt.ValidForHandoff(*file.Journal.ContainmentHandoff)
	}
	if !valid {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, errors.New("durable stop receipt is not bound to the journal owner")
	}
	gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(file.Journal)
	if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, fmt.Errorf("receipt journal owner=(%d,%q,%d,%q), expected=(%d,%q,%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
	}
	if marker.RunID != key.RunID || marker.Generation != key.Generation || marker.PID != targetPID || marker.Identity != targetIdentity || marker.Receipt != *receipt {
		return linuxWitnessJournalFile{}, linuxWitnessStopReceiptBarrierSnapshot{}, authority.StopReceipt{}, errors.New("real stop-receipt marker does not match the exact durable journal receipt")
	}
	return file, markerSnapshot, *receipt, nil
}

// openLinuxWitnessPIDFD retains another kernel pidfd reference to an exact live
// process. Matching its creation identity before and after pidfd_open binds the
// descriptor; cleanup signals never use numeric PIDs or PGIDs.
func openLinuxWitnessPIDFD(pid int, identity string) (int, error) {
	if pid <= 0 || identity == "" {
		return -1, errors.New("exact live PID and identity are required to retain a pidfd")
	}
	before, err := platform.ProcessIdentity(pid)
	if err != nil {
		return -1, fmt.Errorf("read process identity before pidfd open: %w", err)
	}
	if before != identity {
		return -1, fmt.Errorf("process identity before pidfd open = %q, want %q", before, identity)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return -1, fmt.Errorf("open test-owned pidfd for exact process %d: %w", pid, err)
	}
	after, err := platform.ProcessIdentity(pid)
	if err != nil || after != identity {
		_ = unix.Close(fd)
		if err != nil {
			return -1, fmt.Errorf("revalidate process identity after pidfd open: %w", err)
		}
		return -1, fmt.Errorf("process identity changed during pidfd open: got %q, want %q", after, identity)
	}
	return fd, nil
}

// openLinuxWitnessTargetPIDFD additionally verifies that the retained process
// is the original process-group leader used for PIDFD_SIGNAL_PROCESS_GROUP.
func openLinuxWitnessTargetPIDFD(pid int, identity string) (int, error) {
	fd, err := openLinuxWitnessPIDFD(pid, identity)
	if err != nil {
		return -1, err
	}
	snapshot := linuxWitnessProcessSnapshotAt("pidfd_target_anchor", pid, identity)
	if !snapshot.Exists || snapshot.PGRP != int64(pid) {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("retained pidfd target is not the exact process-group leader: %#v", snapshot)
	}
	return fd, nil
}

func killLinuxWitnessProcessByPIDFD(fd int) error {
	if fd < 0 {
		return errors.New("retained exact process pidfd is unavailable")
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		return fmt.Errorf("signal exact process pidfd: %w", err)
	}
	return nil
}

func stopLinuxWitnessGroupByPIDFD(fd, pid int, identity string, timeout time.Duration) error {
	if fd < 0 || pid <= 0 || identity == "" {
		return errors.New("retained exact target pidfd and identity are required for group stop")
	}
	if timeout <= 0 {
		return errors.New("positive timeout is required for pidfd group stop proof")
	}
	deadline := time.Now().Add(timeout)
	signalErr := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	if signalErr == nil || errors.Is(signalErr, unix.ESRCH) {
		return waitForLinuxWitnessProcessStopped(pid, identity, time.Until(deadline))
	}
	if !errors.Is(signalErr, unix.EINTR) && !errors.Is(signalErr, unix.EAGAIN) {
		proofErr := waitForLinuxWitnessProcessStopped(pid, identity, time.Until(deadline))
		if proofErr == nil {
			return nil
		}
		return errors.Join(fmt.Errorf("pidfd group SIGKILL: %w", signalErr), fmt.Errorf("original process group stop proof: %w", proofErr))
	}
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		probeWindow := min(100*time.Millisecond, remaining)
		if proofErr := waitForLinuxWitnessProcessStopped(pid, identity, probeWindow); proofErr == nil {
			return nil
		}
		signalErr = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
		if signalErr == nil || errors.Is(signalErr, unix.ESRCH) {
			return waitForLinuxWitnessProcessStopped(pid, identity, time.Until(deadline))
		}
		if !errors.Is(signalErr, unix.EINTR) && !errors.Is(signalErr, unix.EAGAIN) {
			proofErr := waitForLinuxWitnessProcessStopped(pid, identity, time.Until(deadline))
			if proofErr == nil {
				return nil
			}
			return errors.Join(fmt.Errorf("pidfd group SIGKILL: %w", signalErr), fmt.Errorf("original process group stop proof: %w", proofErr))
		}
	}
	proofErr := waitForLinuxWitnessProcessStopped(pid, identity, time.Until(deadline))
	return errors.Join(fmt.Errorf("last pidfd group SIGKILL attempt: %w", signalErr), fmt.Errorf("original process group stop proof: %w", proofErr))
}

func daemonProcessPID(daemon *productionDaemonWitnessProcess) int {
	if daemon == nil || daemon.command == nil || daemon.command.Process == nil {
		return 0
	}
	return daemon.command.Process.Pid
}

func linuxWitnessDaemonAlive(daemon *productionDaemonWitnessProcess) bool {
	if daemon == nil || daemon.waited || daemon.command == nil || daemon.command.Process == nil {
		return false
	}
	return daemon.command.Process.Signal(syscall.Signal(0)) == nil
}

func linuxWitnessJournalRetainsUnreceiptedOwner(journal state.RunJournal) bool {
	if journal.ContainmentAuthority != nil && journal.ContainmentHandoff == nil {
		return journal.ContainmentAuthority.StopReceipt == nil && journal.HasProcessDetails()
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentAuthority == nil {
		handoff := journal.ContainmentHandoff
		return handoff.StopReceipt == nil && handoff.TargetPID > 0 && handoff.TargetIdentity != "" &&
			handoff.SupervisorPID > 0 && handoff.SupervisorIdentity != ""
	}
	return false
}

func cleanupLinuxUnsealedWitnessOwner(t *testing.T, target linuxWitnessTarget, key state.RunKey, targetPID int, targetIdentity string, helperPID int, helperIdentity string, evidence *linuxWitnessEvidence) {
	t.Helper()
	if key.RunID == "" {
		files, err := readLinuxWitnessJournals(target.StateDir)
		if err != nil {
			t.Errorf("read Linux witness journals before exact-helper teardown: %v", err)
			evidence.Notes = append(evidence.Notes, "test teardown could not locate the exact unresolved journal: "+err.Error())
			writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
			return
		}
		var unresolved []linuxWitnessJournalFile
		for _, file := range files {
			if file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentAuthority != nil || file.Journal.HasProcessDetails() {
				unresolved = append(unresolved, file)
			}
		}
		if len(unresolved) == 0 {
			if helperPID <= 0 || waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 100*time.Millisecond) == nil {
				return
			}
			err := errors.New("no unresolved journal identifies the still-live exact helper")
			t.Errorf("Linux witness exact-helper teardown refused owner without a retained journal: %v", err)
			evidence.Notes = append(evidence.Notes, "test teardown left a still-live helper untouched because no unresolved journal proved its current owner: "+err.Error())
			writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
			return
		}
		if len(unresolved) != 1 {
			err := fmt.Errorf("found %d unresolved journals, cannot select an exact helper", len(unresolved))
			t.Errorf("Linux witness exact-helper teardown: %v", err)
			evidence.Notes = append(evidence.Notes, "test teardown could not select a unique unresolved journal: "+err.Error())
			writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
			return
		}
		key = unresolved[0].Journal.Key()
	}
	journal, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil || !found {
		if err == nil {
			if helperPID <= 0 || waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 100*time.Millisecond) == nil {
				return
			}
			err = errors.New("exact unresolved journal is absent while its helper remains live")
		}
		t.Errorf("read exact unresolved journal before helper teardown: %v", err)
		evidence.Notes = append(evidence.Notes, "test teardown could not verify the exact unresolved journal before helper termination: "+err.Error())
		writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
		return
	}
	currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity := linuxWitnessOwnerIDs(journal.Journal)
	if targetPID == 0 || targetIdentity == "" {
		targetPID, targetIdentity = currentTargetPID, currentTargetIdentity
	}
	if helperPID == 0 || helperIdentity == "" {
		helperPID, helperIdentity = currentHelperPID, currentHelperIdentity
	}
	if currentTargetPID != targetPID || currentTargetIdentity != targetIdentity || currentHelperPID != helperPID || currentHelperIdentity != helperIdentity {
		err := fmt.Errorf("journal owner target=(%d,%q) helper=(%d,%q), expected target=(%d,%q) helper=(%d,%q)", currentTargetPID, currentTargetIdentity, currentHelperPID, currentHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
		t.Errorf("Linux witness exact-helper teardown refused changed owner: %v", err)
		evidence.Notes = append(evidence.Notes, "test teardown refused to touch a changed owner identity: "+err.Error())
		writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
		return
	}
	if targetPID <= 0 || targetIdentity == "" || helperPID <= 0 || helperIdentity == "" {
		err := errors.New("exact positive target/helper PIDs and identities are required")
		t.Errorf("Linux witness exact-helper teardown refused incomplete owner fence: %v", err)
		evidence.Notes = append(evidence.Notes, "test teardown refused helper termination because exact target/helper identities were incomplete")
		writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
		return
	}
	if !linuxWitnessJournalRetainsUnreceiptedOwner(journal.Journal) {
		err := errors.New("journal does not retain an exact unreceipted containment owner")
		t.Errorf("Linux witness exact-helper teardown refused journal state: %v", err)
		evidence.Notes = append(evidence.Notes, "test teardown refused to terminate a helper without a retained no-receipt authority snapshot")
		writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
		return
	}
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(journal))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_unsealed_helper_teardown", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_before_unsealed_helper_teardown", helperPID, helperIdentity))
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 2*time.Second); err != nil {
		t.Errorf("Linux witness exact-helper teardown lacks original process-group stop proof: %v", err)
		evidence.Notes = append(evidence.Notes, "test teardown left the exact helper alive because original process-group stop proof was absent: "+err.Error())
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
			linuxWitnessProcessSnapshotAt("target_without_teardown_stop_proof", targetPID, targetIdentity),
			linuxWitnessProcessSnapshotAt("helper_left_alive_without_teardown_stop_proof", helperPID, helperIdentity))
		writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
		return
	}
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err == nil {
		if killErr := killLinuxWitnessProcess(t, helperPID, helperIdentity); killErr != nil {
			t.Errorf("Linux witness could not SIGKILL exact helper %d/%q after original group stop proof: %v", helperPID, helperIdentity, killErr)
			evidence.Notes = append(evidence.Notes, "test teardown could not SIGKILL the exact helper after proving the original group stopped: "+killErr.Error())
		} else {
			evidence.Notes = append(evidence.Notes, "test teardown SIGKILLed the identity-matching helper only after proving the original process group stopped; this is not a stop receipt, protocol release, or journal clear")
		}
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Errorf("Linux witness could not prove exact helper %d/%q gone after test teardown: %v", helperPID, helperIdentity, err)
		evidence.Notes = append(evidence.Notes, "test teardown could not prove the exact helper gone: "+err.Error())
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_unsealed_helper_teardown", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_unsealed_helper_teardown", helperPID, helperIdentity))
	retained, stillFound, readErr := readLinuxWitnessJournalForKey(target.StateDir, key)
	if readErr != nil || !stillFound {
		if readErr == nil {
			readErr = errors.New("unresolved journal disappeared during exact-helper teardown")
		}
		t.Errorf("verify unresolved journal after exact-helper teardown: %v", readErr)
		evidence.Notes = append(evidence.Notes, "test teardown could not verify the unresolved journal after helper termination: "+readErr.Error())
	} else {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(retained))
		retainedTargetPID, retainedTargetIdentity, retainedHelperPID, retainedHelperIdentity := linuxWitnessOwnerIDs(retained.Journal)
		if retainedTargetPID != targetPID || retainedTargetIdentity != targetIdentity || retainedHelperPID != helperPID || retainedHelperIdentity != helperIdentity {
			t.Errorf("unresolved journal owner changed during exact-helper teardown: target=(%d,%q) helper=(%d,%q)", retainedTargetPID, retainedTargetIdentity, retainedHelperPID, retainedHelperIdentity)
			evidence.Notes = append(evidence.Notes, "unresolved journal owner identity changed during test teardown")
		}
		if !linuxWitnessJournalRetainsUnreceiptedOwner(retained.Journal) {
			t.Errorf("unresolved journal crossed receipt/clear boundary during exact-helper teardown")
			evidence.Notes = append(evidence.Notes, "unresolved journal changed across test teardown; no cleanup recovery or journal clear was requested")
		}
		if linuxWitnessScanMirrorSealedSequence(retained.Journal) != 0 {
			t.Errorf("unsealed negative journal gained a mirror seal during exact-helper teardown")
			evidence.Notes = append(evidence.Notes, "mirror seal state changed during teardown of the unsealed negative boundary")
		}
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, *evidence)
}

func prepareLinuxWitnessTarget(t *testing.T, environment e2eEnvironment, caseRoot, name string) linuxWitnessTarget {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(caseRoot, "state", "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspacePath := filepath.Join(caseRoot, "workspace")
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(caseRoot, "state")
	targetMarker := filepath.Join(caseRoot, "target.pid")
	descendantMarker := filepath.Join(caseRoot, "descendant.pid")
	scriptPath := filepath.Join(caseRoot, "linux-containment-target.sh")
	script := linuxWitnessTargetScript(targetMarker, descendantMarker)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write Linux containment target: %v", err)
	}
	profile := unique("linux-" + name + "-profile")
	workspace := unique("linux-" + name + "-workspace")
	value := daemonConfig(environment, name, stateDir, workspacePath, profile, workspace)
	profileValue := value.AgentProfiles[profile]
	profileValue.Command = scriptPath
	profileValue.Args = nil
	profileValue.InputMode = "json"
	profileValue.Interactive = true
	profileValue.EventFormat = "jsonl"
	value.AgentProfiles[profile] = profileValue
	configPath := filepath.Join(caseRoot, "daemon.json")
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal Linux witness daemon config: %v", err)
	}
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatalf("write Linux witness daemon config: %v", err)
	}
	return linuxWitnessTarget{ConfigPath: configPath, StateDir: stateDir, WorkspacePath: workspacePath, TargetMarker: targetMarker, DescendantMarker: descendantMarker, EvidencePath: filepath.Join(caseRoot, "witness-evidence.json")}
}

func linuxWitnessTargetScript(targetMarker, descendantMarker string) string {
	return "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' \"$$\" > " + linuxWitnessShellQuote(targetMarker) + "\n" +
		"sh -c 'trap \"\" HUP; exec sleep 600' &\n" +
		"child=\"$!\"\n" +
		"printf '%s\\n' \"$child\" > " + linuxWitnessShellQuote(descendantMarker) + "\n" +
		"printf '%s\\n' '{\"type\":\"progress\",\"message\":\"linux containment witness started\"}'\n" +
		"while :; do sleep 1; done\n"
}

func linuxWitnessShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func profileAndWorkspace(t *testing.T, configPath string) (string, string) {
	t.Helper()
	value, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var configValue struct {
		AgentProfiles map[string]struct{} `json:"agent_profiles"`
		Runtime       struct {
			AgentProfile string `json:"agent_profile"`
			Workspace    string `json:"workspace"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(value, &configValue); err != nil {
		t.Fatal(err)
	}
	return configValue.Runtime.AgentProfile, configValue.Runtime.Workspace
}

func profileFromConfig(t *testing.T, configPath string) string {
	profile, _ := profileAndWorkspace(t, configPath)
	return profile
}

func workspaceFromConfig(t *testing.T, configPath string) string {
	_, workspace := profileAndWorkspace(t, configPath)
	return workspace
}

func readLinuxWitnessJournals(stateDir string) ([]linuxWitnessJournalFile, error) {
	entries, err := os.ReadDir(filepath.Join(stateDir, "runs"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]linuxWitnessJournalFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "journal-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(stateDir, "runs", entry.Name())
		raw, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		var journal state.RunJournal
		if err := json.Unmarshal(raw, &journal); err != nil {
			continue
		}
		files = append(files, linuxWitnessJournalFile{Path: path, Raw: raw, Journal: journal})
	}
	return files, nil
}

func readLinuxWitnessJournalForKey(stateDir string, key state.RunKey) (linuxWitnessJournalFile, bool, error) {
	files, err := readLinuxWitnessJournals(stateDir)
	if err != nil {
		return linuxWitnessJournalFile{}, false, err
	}
	for _, file := range files {
		if file.Journal.Key() == key {
			return file, true, nil
		}
	}
	return linuxWitnessJournalFile{}, false, nil
}

func waitForLinuxWitnessJournal(stateDir, description string, timeout time.Duration, predicate func(linuxWitnessJournalFile) bool) (linuxWitnessJournalFile, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		files, err := readLinuxWitnessJournals(stateDir)
		if err != nil {
			lastErr = err
		} else {
			for _, file := range files {
				if predicate(file) {
					return file, nil
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return linuxWitnessJournalFile{}, fmt.Errorf("%s did not appear within %s: %v", description, timeout, lastErr)
}

func waitForLinuxWitnessJournalReleased(stateDir string, key state.RunKey, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		file, found, err := readLinuxWitnessJournalForKey(stateDir, key)
		if err != nil {
			return err
		}
		if !found || (file.Journal.ContainmentHandoff == nil && file.Journal.ContainmentAuthority == nil && !file.Journal.HasProcessDetails()) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("journal %s/%d retained containment after %s", key.RunID, key.Generation, timeout)
}

func linuxWitnessJournalHasBoundScanLedger(journal state.RunJournal) bool {
	return !journal.ContainmentUnproven && linuxWitnessScanLedgerMatches(journal)
}

func linuxWitnessScanLedgerMatches(journal state.RunJournal) bool {
	scans := journal.ContainmentScans
	owner := journal.ContainmentAuthority
	if scans == nil || owner == nil || !journal.HasProcessDetails() {
		return false
	}
	return scans.LaunchToken == owner.LaunchToken && scans.TargetPID == journal.PID &&
		scans.TargetIdentity == journal.ProcessIdentity && scans.SupervisorPID == owner.SupervisorPID &&
		scans.SupervisorIdentity == owner.SupervisorIdentity && scans.MirrorIdentity != ""
}

func linuxWitnessScanProgressIsClean(progress state.ContainmentScanProgress) bool {
	return progress.PendingSequence == 0 && progress.CleanResultSequence == 0 &&
		progress.UnprovenSequence == 0 && progress.SealedSequence == 0 && progress.FailureKind == "" &&
		progress.CompletedSequence > 0
}

func linuxWitnessScanProgressIsSealedClean(progress state.ContainmentScanProgress) bool {
	return progress.PendingSequence == 0 && progress.CleanResultSequence == 0 &&
		progress.UnprovenSequence == 0 && progress.FailureKind == "" && progress.CompletedSequence > 0 &&
		progress.SealedSequence > 0 && progress.SealedSequence == progress.CompletedSequence
}

func linuxWitnessJournalHasScanSealBoundary(journal state.RunJournal, sealedOwner string) bool {
	if !linuxWitnessJournalHasBoundScanLedger(journal) {
		return false
	}
	scans := journal.ContainmentScans
	switch sealedOwner {
	case "helper":
		return linuxWitnessScanProgressIsSealedClean(scans.Helper) && linuxWitnessScanProgressIsClean(scans.Mirror)
	case "mirror":
		return linuxWitnessScanProgressIsClean(scans.Helper) && linuxWitnessScanProgressIsSealedClean(scans.Mirror)
	case "both":
		return linuxWitnessScanProgressIsSealedClean(scans.Helper) && linuxWitnessScanProgressIsSealedClean(scans.Mirror)
	default:
		return false
	}
}

func linuxWitnessScanMirrorSealedSequence(journal state.RunJournal) uint64 {
	if journal.ContainmentScans == nil {
		return 0
	}
	return journal.ContainmentScans.Mirror.SealedSequence
}

func linuxWitnessScanSealEnvironment(triggerPath, reachedPath, releasePath, owner string) map[string]string {
	return map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxSupervisorScanFailureModeEnvironment:    linuxSupervisorScanSealAfterCleanMode,
		linuxSupervisorScanFailureTriggerEnvironment: triggerPath,
		linuxSupervisorScanFailureFiredEnvironment:   reachedPath,
		linuxSupervisorScanPendingReleaseEnvironment: releasePath,
		linuxSupervisorScanSealOwnerEnvironment:      owner,
	}
}

func primeLinuxWitnessScanSeal(triggerPath, reachedPath, releasePath string) error {
	for _, path := range []string{triggerPath, reachedPath, reachedPath + ".helper", reachedPath + ".mirror", releasePath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("scan seal marker %q before trigger = %v, want absent", path, err)
		}
	}
	if err := os.WriteFile(triggerPath, []byte("witness-ready\n"), 0o600); err != nil {
		return fmt.Errorf("prime Linux scan seal trigger: %w", err)
	}
	if err := writeLinuxWitnessReleaseMarker(releasePath); err != nil {
		return fmt.Errorf("prime Linux scan seal release: %w", err)
	}
	return nil
}

func waitForLinuxWitnessScanSeal(stateDir string, key state.RunKey, triggerPath, reachedPath, releasePath, owner string, timeout time.Duration) (linuxWitnessJournalFile, string, string, error) {
	if err := primeLinuxWitnessScanSeal(triggerPath, reachedPath, releasePath); err != nil {
		return linuxWitnessJournalFile{}, "", "", fmt.Errorf("prepare Linux scan seal boundary: %w", err)
	}
	return awaitLinuxWitnessScanSeal(stateDir, key, reachedPath, owner, timeout)
}

func awaitLinuxWitnessScanSeal(stateDir string, key state.RunKey, reachedPath, owner string, timeout time.Duration) (linuxWitnessJournalFile, string, string, error) {
	deadline := time.Now().Add(timeout)
	if owner != "helper" && owner != "mirror" && owner != "both" {
		return linuxWitnessJournalFile{}, "", "", fmt.Errorf("unsupported Linux scan seal owner %q", owner)
	}
	helperReached, mirrorReached := "", ""
	if owner == "helper" || owner == "both" {
		if err := waitForLinuxWitnessMarker(reachedPath+".helper", time.Until(deadline)); err != nil {
			return linuxWitnessJournalFile{}, "", "", fmt.Errorf("wait for durable helper scan seal: %w", err)
		}
		contents, err := os.ReadFile(reachedPath + ".helper")
		if err != nil {
			return linuxWitnessJournalFile{}, "", "", fmt.Errorf("read durable helper scan seal marker: %w", err)
		}
		helperReached = string(contents)
	}
	if owner == "mirror" || owner == "both" {
		if err := waitForLinuxWitnessMarker(reachedPath+".mirror", time.Until(deadline)); err != nil {
			return linuxWitnessJournalFile{}, "", "", fmt.Errorf("wait for durable mirror scan seal: %w", err)
		}
		contents, err := os.ReadFile(reachedPath + ".mirror")
		if err != nil {
			return linuxWitnessJournalFile{}, "", "", fmt.Errorf("read durable mirror scan seal marker: %w", err)
		}
		mirrorReached = string(contents)
	}
	file, err := waitForLinuxWitnessJournal(stateDir, "durable Linux scan seal boundary", time.Until(deadline), func(value linuxWitnessJournalFile) bool {
		return value.Journal.Key() == key && linuxWitnessJournalHasScanSealBoundary(value.Journal, owner)
	})
	if err != nil {
		return linuxWitnessJournalFile{}, "", "", err
	}
	return file, helperReached, mirrorReached, nil
}

func decodeLinuxWitnessStopReceiptBarrierMarker(contents []byte) (linuxWitnessStopReceiptBarrierMarker, linuxWitnessStopReceiptBarrierSnapshot, error) {
	var marker linuxWitnessStopReceiptBarrierMarker
	if err := json.Unmarshal(contents, &marker); err != nil {
		return linuxWitnessStopReceiptBarrierMarker{}, linuxWitnessStopReceiptBarrierSnapshot{}, err
	}
	snapshot := linuxWitnessStopReceiptBarrierSnapshot{
		RunID:      marker.RunID,
		Generation: marker.Generation,
		PID:        marker.PID,
		Identity:   marker.Identity,
		Receipt:    *linuxReplayReceiptSnapshotFromReceipt(marker.Receipt),
	}
	return marker, snapshot, nil
}

func linuxWitnessJournalSnapshotFromFile(file linuxWitnessJournalFile) linuxWitnessJournalSnapshot {
	journal := file.Journal
	snapshot := linuxWitnessJournalSnapshot{
		At:                  time.Now().UTC(),
		Path:                file.Path,
		RawSHA256:           linuxWitnessHashBytes(file.Raw),
		RunID:               journal.RunID,
		Generation:          journal.Generation,
		LocalState:          journal.LocalState,
		TerminalState:       journal.TerminalState,
		TerminalVerdict:     journal.TerminalVerdict,
		PID:                 journal.PID,
		ProcessIdentity:     journal.ProcessIdentity,
		StartedAt:           journal.StartedAt,
		LeaseExpiresAt:      journal.LeaseExpiresAt,
		HasProcessDetails:   journal.HasProcessDetails(),
		ContainmentUnproven: journal.ContainmentUnproven,
		RetainWorkspace:     journal.RetainWorkspace,
		PendingEvents:       len(journal.PendingEvents),
		PendingTransitions:  len(journal.PendingTransitions),
	}
	if journal.ContainmentHandoff != nil {
		owner := linuxWitnessOwnerSnapshotFromHandoff(*journal.ContainmentHandoff)
		snapshot.Handoff = &owner
		if journal.ContainmentHandoff.StopReceipt != nil {
			snapshot.StopReceipt = linuxReplayReceiptSnapshotFromReceipt(*journal.ContainmentHandoff.StopReceipt)
		}
	}
	if journal.ContainmentAuthority != nil {
		owner := linuxWitnessOwnerSnapshotFromAuthority(*journal.ContainmentAuthority)
		snapshot.Authority = &owner
		if journal.ContainmentAuthority.StopReceipt != nil {
			snapshot.StopReceipt = linuxReplayReceiptSnapshotFromReceipt(*journal.ContainmentAuthority.StopReceipt)
		}
	}
	if scans := journal.ContainmentScans; scans != nil {
		snapshot.ContainmentScans = &linuxWitnessScanSnapshot{
			TargetPID:          scans.TargetPID,
			TargetIdentity:     scans.TargetIdentity,
			SupervisorPID:      scans.SupervisorPID,
			SupervisorIdentity: scans.SupervisorIdentity,
			Helper:             linuxWitnessScanProgressSnapshotFromState(scans.Helper),
			Mirror:             linuxWitnessScanProgressSnapshotFromState(scans.Mirror),
		}
	}
	return snapshot
}

func linuxWitnessOwnerSnapshotFromHandoff(value authority.SupervisorHandoff) linuxWitnessOwnerSnapshot {
	return linuxWitnessOwnerSnapshot{
		OwnerKind:          value.OwnerKind,
		OwnerContextSHA256: linuxWitnessHashBytes([]byte(value.OwnerContext)),
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		JobIDSHA256:        linuxWitnessHashBytes([]byte(value.JobID)),
		HasStopReceipt:     value.StopReceipt != nil,
	}
}

func linuxWitnessOwnerSnapshotFromAuthority(value authority.Supervisor) linuxWitnessOwnerSnapshot {
	return linuxWitnessOwnerSnapshot{
		OwnerKind:          value.OwnerKind,
		OwnerContextSHA256: linuxWitnessHashBytes([]byte(value.OwnerContext)),
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		JobIDSHA256:        linuxWitnessHashBytes([]byte(value.JobID)),
		HasStopReceipt:     value.StopReceipt != nil,
	}
}

func TestLinuxWitnessCleanupRecoveryRejectsUnlocatedSubmittedOwner(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "runs"), 0o700); err != nil {
		t.Fatalf("create empty witness journal directory: %v", err)
	}
	target := linuxWitnessTarget{StateDir: stateDir}
	err := cleanupLinuxWitnessRecoveryAfterFailure("", target, t.TempDir(), state.RunKey{}, 0, "", 0, "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "task was submitted but cleanup cannot locate exactly one owner journal") {
		t.Fatalf("empty-owner cleanup error = %v, want explicit unresolved-owner diagnostic", err)
	}
}

func TestLinuxWitnessJournalSnapshotRedactsCredentials(t *testing.T) {
	const (
		secret       = "projection-secret-marker-7d59f4"
		leaseToken   = "projection-lease-marker-5381cc"
		claimID      = "projection-claim-marker-92c205"
		pipeToken    = "projection-pipe-marker-d71a30"
		jobID        = "projection-job-marker-2314af"
		launchToken  = "projection-launch-marker-858abc"
		mirrorID     = "projection-mirror-marker-b2835c"
		ownerContext = "helper=/tmp/projection-endpoint-marker.sock|endpoint=/tmp/projection-job-marker-2314af.sock"
		targetID     = "target-identity-visible-61a4d2"
		helperID     = "helper-identity-visible-90c21f"
	)
	raw := []byte(secret + "|" + leaseToken + "|" + claimID + "|" + pipeToken + "|" + jobID + "|" + launchToken + "|" + mirrorID + "|" + ownerContext)
	journal := state.RunJournal{
		RunID:           "visible-run-id",
		Generation:      7,
		ClaimID:         claimID,
		LeaseToken:      leaseToken,
		LocalState:      "running",
		PID:             31001,
		ProcessIdentity: targetID,
		ContainmentScans: &state.ContainmentScanJournal{
			Version: 1, LaunchToken: launchToken, MirrorIdentity: mirrorID,
			TargetPID: 31001, TargetIdentity: targetID, SupervisorPID: 31002, SupervisorIdentity: helperID,
			Helper: state.ContainmentScanProgress{CompletedSequence: 2, SealedSequence: 2},
			Mirror: state.ContainmentScanProgress{CompletedSequence: 2, SealedSequence: 2},
		},
		ContainmentAuthority: &authority.Supervisor{
			Version:            authority.SupervisorVersion,
			Secret:             secret,
			OwnerKind:          authority.OwnerKindLinuxHelper,
			OwnerContext:       ownerContext,
			TargetPID:          31001,
			TargetIdentity:     targetID,
			PipeToken:          pipeToken,
			JobID:              jobID,
			SupervisorPID:      31002,
			SupervisorIdentity: helperID,
			StopReceipt: &authority.StopReceipt{
				Version:            authority.SupervisorVersion,
				Status:             "stopped",
				OwnerKind:          authority.OwnerKindLinuxHelper,
				OwnerContext:       ownerContext,
				TargetPID:          31001,
				TargetIdentity:     targetID,
				PipeToken:          pipeToken,
				JobID:              jobID,
				SupervisorPID:      31002,
				SupervisorIdentity: helperID,
			},
		},
	}
	file := linuxWitnessJournalFile{Path: "journal-path-visible", Raw: raw, Journal: journal}
	evidence, err := json.Marshal(linuxWitnessJournalSnapshotFromFile(file))
	if err != nil {
		t.Fatalf("encode synthetic journal evidence projection: %v", err)
	}
	replayEvidence, err := json.Marshal(linuxReplayRawJournalSnapshotFromFile(file))
	if err != nil {
		t.Fatalf("encode synthetic replay evidence projection: %v", err)
	}
	var replayProjection linuxReplayRawJournalSnapshot
	if err := json.Unmarshal(replayEvidence, &replayProjection); err != nil {
		t.Fatalf("decode synthetic replay evidence projection: %v", err)
	}
	negativeEvidence, err := json.Marshal(linuxNegativeWitnessEvidence{JournalSnapshots: []linuxWitnessJournalSnapshot{linuxWitnessJournalSnapshotFromFile(file)}})
	if err != nil {
		t.Fatalf("encode synthetic negative witness projection: %v", err)
	}
	if replayProjection.SHA256 != linuxWitnessHashBytes(raw) {
		t.Errorf("replay evidence raw-journal digest does not bind the synthetic journal")
	}
	serialized := string(evidence) + "\n" + string(replayEvidence) + "\n" + string(negativeEvidence)
	for _, value := range []struct {
		name  string
		value string
	}{
		{name: "secret", value: secret},
		{name: "lease token", value: leaseToken},
		{name: "claim ID", value: claimID},
		{name: "pipe token", value: pipeToken},
		{name: "job ID", value: jobID},
		{name: "launch token", value: launchToken},
		{name: "mirror identity", value: mirrorID},
		{name: "owner context or helper endpoint", value: ownerContext},
	} {
		if strings.Contains(serialized, value.value) {
			t.Errorf("journal evidence projection exposed raw %s", value.name)
		}
	}

	var projection struct {
		Authority   linuxWitnessOwnerSnapshot   `json:"authority"`
		StopReceipt *linuxReplayReceiptSnapshot `json:"stop_receipt"`
	}
	if err := json.Unmarshal(evidence, &projection); err != nil {
		t.Fatalf("decode synthetic journal evidence projection: %v", err)
	}
	if projection.Authority.TargetPID != 31001 || projection.Authority.TargetIdentity != targetID ||
		projection.Authority.SupervisorPID != 31002 || projection.Authority.SupervisorIdentity != helperID {
		t.Errorf("journal projection lost owner identities: target=%d/%q helper=%d/%q",
			projection.Authority.TargetPID, projection.Authority.TargetIdentity, projection.Authority.SupervisorPID, projection.Authority.SupervisorIdentity)
	}
	if projection.Authority.OwnerContextSHA256 != linuxWitnessHashBytes([]byte(ownerContext)) ||
		projection.Authority.JobIDSHA256 != linuxWitnessHashBytes([]byte(jobID)) {
		t.Errorf("owner projection digests do not bind the original owner context and JobID")
	}
	if projection.StopReceipt == nil || projection.StopReceipt.TargetPID != 31001 || projection.StopReceipt.TargetIdentity != targetID ||
		projection.StopReceipt.SupervisorPID != 31002 || projection.StopReceipt.SupervisorIdentity != helperID {
		t.Errorf("receipt projection lost owner identities")
	}
	if projection.StopReceipt != nil && (projection.StopReceipt.OwnerContextSHA256 != linuxWitnessHashBytes([]byte(ownerContext)) ||
		projection.StopReceipt.JobIDSHA256 != linuxWitnessHashBytes([]byte(jobID)) ||
		projection.StopReceipt.PipeTokenSHA256 != linuxWitnessHashBytes([]byte(pipeToken))) {
		t.Errorf("receipt projection digests do not bind the original owner context, JobID, and pipe token")
	}
}

func linuxWitnessOwnerIDs(journal state.RunJournal) (targetPID int, targetIdentity string, helperPID int, helperIdentity string) {
	if journal.ContainmentAuthority != nil {
		value := journal.ContainmentAuthority
		return value.TargetPID, value.TargetIdentity, value.SupervisorPID, value.SupervisorIdentity
	}
	if journal.ContainmentHandoff != nil {
		value := journal.ContainmentHandoff
		return value.TargetPID, value.TargetIdentity, value.SupervisorPID, value.SupervisorIdentity
	}
	return journal.PID, journal.ProcessIdentity, 0, ""
}

func linuxWitnessProcessSnapshotAt(role string, pid int, identity string) linuxWitnessProcessSnapshot {
	snapshot := linuxWitnessProcessSnapshot{At: time.Now().UTC(), Role: role, PID: pid, Identity: identity}
	if pid <= 0 {
		snapshot.Error = "pid is not positive"
		return snapshot
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		snapshot.Error = err.Error()
		return snapshot
	}
	closing := strings.LastIndexByte(string(raw), ')')
	if closing < 0 || closing+2 >= len(raw) {
		snapshot.Error = "malformed /proc stat"
		return snapshot
	}
	fields := strings.Fields(string(raw)[closing+2:])
	if len(fields) <= 19 {
		snapshot.Error = "short /proc stat"
		return snapshot
	}
	snapshot.State = fields[0]
	snapshot.PGRP, _ = strconv.ParseInt(fields[2], 10, 64)
	snapshot.Session, _ = strconv.ParseInt(fields[3], 10, 64)
	snapshot.StartTime, _ = strconv.ParseUint(fields[19], 10, 64)
	snapshot.Exists = true
	return snapshot
}

func assertLinuxWitnessSameGroup(t *testing.T, target, descendant linuxWitnessProcessSnapshot) {
	t.Helper()
	if !target.Exists || !descendant.Exists {
		t.Fatalf("target/descendant snapshots are incomplete: target=%#v descendant=%#v", target, descendant)
	}
	if target.PGRP != int64(target.PID) || descendant.PGRP != target.PGRP || descendant.Session != target.Session {
		t.Fatalf("target/descendant group anchor mismatch: target=%#v descendant=%#v", target, descendant)
	}
}

func waitForLinuxWitnessPIDMarker(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(value)))
			if parseErr == nil && pid > 0 {
				return pid, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return 0, fmt.Errorf("pid marker %q did not appear within %s", path, timeout)
}

func readLinuxWitnessPIDMarker(path string) (int, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(value)))
	if err != nil || pid <= 0 {
		if err == nil {
			err = errors.New("pid must be positive")
		}
		return 0, err
	}
	return pid, nil
}

func killLinuxProductionDaemon(process *productionDaemonWitnessProcess) linuxWitnessExitStatus {
	status := linuxWitnessExitStatus{RequestedSignal: "SIGKILL", ExitCode: -1}
	if process == nil || process.command == nil || process.command.Process == nil {
		status.Error = "production daemon process is unavailable"
		return status
	}
	if err := process.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		status.Error = err.Error()
	}
	waitErr := process.command.Wait()
	process.waited = true
	_ = process.stdout.Close()
	_ = process.stderr.Close()
	if waitErr != nil {
		status.Error = waitErr.Error()
	}
	if process.command.ProcessState != nil {
		status.ExitCode = process.command.ProcessState.ExitCode()
		if waitStatus, ok := process.command.ProcessState.Sys().(syscall.WaitStatus); ok {
			status.Signaled = waitStatus.Signaled()
			if status.Signaled {
				status.Signal = linuxWitnessSignalName(waitStatus.Signal())
			}
		}
	}
	return status
}

func linuxWitnessSignalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	default:
		return signal.String()
	}
}

func killLinuxWitnessProcess(t *testing.T, pid int, identity string) error {
	t.Helper()
	return killLinuxWitnessProcessExact(pid, identity)
}

func killLinuxWitnessProcessExact(pid int, identity string) error {
	if err := assertLinuxWitnessProcessLive(pid, identity); err != nil {
		return err
	}
	return unix.Kill(pid, unix.SIGKILL)
}

func assertLinuxWitnessProcessLive(pid int, identity string) error {
	if pid <= 0 {
		return errors.New("pid is not positive")
	}
	actual, err := platform.ProcessIdentity(pid)
	if err != nil {
		return err
	}
	if identity != "" && actual != identity {
		return fmt.Errorf("process identity = %q, want %q", actual, identity)
	}
	snapshot := linuxWitnessProcessSnapshotAt("live", pid, actual)
	if !snapshot.Exists || snapshot.State == "Z" {
		return fmt.Errorf("process %d is not live: %#v", pid, snapshot)
	}
	return nil
}

func waitForLinuxWitnessIdentityGone(pid int, identity string, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		actual, err := platform.ProcessIdentity(pid)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if errors.Is(err, syscall.ESRCH) {
			_, statErr := os.Stat(fmt.Sprintf("/proc/%d/stat", pid))
			if errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			if statErr != nil {
				return fmt.Errorf("inspect process %d after an exit-race read: %w", pid, statErr)
			}
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
		if identity != "" && actual != identity {
			return nil
		}
		snapshot := linuxWitnessProcessSnapshotAt("gone-probe", pid, actual)
		if snapshot.State == "Z" {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process %d with identity %q remained live", pid, identity)
}

func waitForLinuxWitnessProcessStopped(pid int, identity string, timeout time.Duration) error {
	if identity == "" {
		// Descendant PID markers are not group anchors. Callers prove the exact
		// original leader's group separately before checking a child has exited.
		return waitForLinuxWitnessIdentityGone(pid, "", timeout)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if platform.ProvePersistedProcessGroupAbsent(context.Background(), pid, identity) == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process group led by %d did not reach a stopped proof", pid)
}

// waitForLinuxWitnessAllThreadsStopped observes each task entry because the
// leader's /proc/<pid>/stat state does not prove every daemon worker has
// completed the SIGSTOP group-stop transition.
func waitForLinuxWitnessAllThreadsStopped(pid int, identity string, timeout time.Duration) (int, error) {
	if pid <= 0 || identity == "" {
		return 0, errors.New("positive daemon PID and identity are required for process-wide stop proof")
	}
	deadline := time.Now().Add(timeout)
	lastStates := "<unobserved>"
	for time.Now().Before(deadline) {
		actual, err := platform.ProcessIdentity(pid)
		if err != nil {
			return 0, fmt.Errorf("read daemon identity during process-wide stop proof: %w", err)
		}
		if actual != identity {
			return 0, fmt.Errorf("daemon identity during process-wide stop proof = %q, want %q", actual, identity)
		}
		taskDirectory := fmt.Sprintf("/proc/%d/task", pid)
		threads, err := os.ReadDir(taskDirectory)
		if err != nil {
			return 0, fmt.Errorf("enumerate daemon threads for stop proof: %w", err)
		}
		states := make([]string, 0, len(threads))
		allStopped := len(threads) > 0
		for _, thread := range threads {
			if !thread.IsDir() {
				allStopped = false
				states = append(states, thread.Name()+":not-a-thread-directory")
				continue
			}
			tid, parseErr := strconv.Atoi(thread.Name())
			if parseErr != nil || tid <= 0 {
				allStopped = false
				states = append(states, thread.Name()+":invalid-tid")
				continue
			}
			raw, readErr := os.ReadFile(filepath.Join(taskDirectory, thread.Name(), "stat"))
			if readErr != nil {
				allStopped = false
				states = append(states, fmt.Sprintf("%d:unreadable", tid))
				continue
			}
			state, stateErr := linuxWitnessProcStatState(raw)
			if stateErr != nil {
				allStopped = false
				states = append(states, fmt.Sprintf("%d:malformed", tid))
				continue
			}
			states = append(states, fmt.Sprintf("%d:%s", tid, state))
			if state != "T" && state != "t" {
				allStopped = false
			}
		}
		lastStates = strings.Join(states, ",")
		if allStopped {
			latestThreads, listErr := os.ReadDir(taskDirectory)
			if listErr == nil && slices.EqualFunc(threads, latestThreads, func(first, second os.DirEntry) bool {
				return first.Name() == second.Name()
			}) {
				latestIdentity, identityErr := platform.ProcessIdentity(pid)
				if identityErr == nil && latestIdentity == identity {
					return len(threads), nil
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, fmt.Errorf("daemon %d threads did not all reach T/t state before timeout; observed [%s]", pid, lastStates)
}

func linuxWitnessProcStatState(raw []byte) (string, error) {
	closing := strings.LastIndexByte(string(raw), ')')
	if closing < 0 || closing+2 >= len(raw) {
		return "", errors.New("malformed /proc stat")
	}
	fields := strings.Fields(string(raw)[closing+2:])
	if len(fields) == 0 || len(fields[0]) != 1 {
		return "", errors.New("missing /proc stat state")
	}
	return fields[0], nil
}

func waitForLinuxWitnessMarker(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if contents, err := os.ReadFile(path); err == nil && len(contents) > 0 {
			return nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("marker %q did not appear within %s", path, timeout)
}

func writeLinuxWitnessReleaseMarker(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("Linux witness release marker path is empty")
	}
	marker, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Linux witness release marker %q is not a regular file", path)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create Linux witness release marker %q: %w", path, err)
	}
	if _, err := marker.WriteString("released\n"); err != nil {
		_ = marker.Close()
		return fmt.Errorf("write Linux witness release marker %q: %w", path, err)
	}
	if err := marker.Sync(); err != nil {
		_ = marker.Close()
		return fmt.Errorf("sync Linux witness release marker %q: %w", path, err)
	}
	if err := marker.Close(); err != nil {
		return fmt.Errorf("close Linux witness release marker %q: %w", path, err)
	}
	return nil
}

func linuxWitnessSHA256(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return linuxWitnessHashBytes(contents), nil
}

func linuxWitnessHashBytes(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func writeLinuxWitnessEvidence(t *testing.T, path string, evidence linuxWitnessEvidence) {
	t.Helper()
	contents, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode Linux witness evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create Linux witness evidence directory: %v", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".linux-witness-")
	if err != nil {
		t.Fatalf("create Linux witness evidence temp file: %v", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		t.Fatalf("write Linux witness evidence: %v", err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("close Linux witness evidence: %v", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		t.Fatalf("publish Linux witness evidence: %v", err)
	}
}

func linuxWitnessLogs(first, second *productionDaemonWitnessProcess) map[string]string {
	logs := make(map[string]string)
	for _, process := range []*productionDaemonWitnessProcess{first, second} {
		if process == nil {
			continue
		}
		for _, file := range []*os.File{process.stdout, process.stderr} {
			if file == nil {
				continue
			}
			path := file.Name()
			contents, err := os.ReadFile(path)
			if err == nil {
				logs[path] = string(contents)
			}
		}
	}
	return logs
}

func runLinuxWitnessCommand(t *testing.T, directory, name string, arguments ...string) string {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	contents, err := command.Output()
	if err != nil {
		t.Fatalf("run %s %s: %v", name, strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(contents))
}

// Keep the authority package import local to this file's evidence conversion;
// the production witness never serializes the secret-bearing authority field.
