//go:build linux

package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
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
)

// TestProductionLinuxContainmentWitnessSpine is an opt-in production-binary
// witness for the three Linux owner transitions most likely to orphan a live
// native tree. It deliberately starts the daemon executable built from
// ./cmd/symmetry-daemon; the normal Linux launcher therefore dispatches the
// same executable through its default -containment-supervisor mode.
func TestProductionLinuxContainmentWitnessSpine(t *testing.T) {
	if os.Getenv(linuxProductionContainmentWitnessEnvironment) != "1" {
		t.Skip("set " + linuxProductionContainmentWitnessEnvironment + "=1 to run the Linux production containment witness")
	}

	environment := loadControlEnvironment(t)
	runRoot := t.TempDir()
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
	t.Run("daemon_sigkill_after_resume_with_descendant", func(t *testing.T) {
		runLinuxDaemonCrashAfterResumeWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "daemon-after-resume"))
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
	Version                int                           `json:"version"`
	Case                   string                        `json:"case"`
	Stage                  string                        `json:"stage"`
	Metadata               linuxWitnessMetadata          `json:"metadata"`
	CaseRoot               string                        `json:"case_root"`
	TaskID                 string                        `json:"task_id,omitempty"`
	RunID                  string                        `json:"run_id,omitempty"`
	Generation             int64                         `json:"generation,omitempty"`
	JournalKey             string                        `json:"journal_key,omitempty"`
	BarrierReachedPath     string                        `json:"barrier_reached_path,omitempty"`
	BarrierReleasePath     string                        `json:"barrier_release_path,omitempty"`
	BarrierReachedContents string                        `json:"barrier_reached_contents,omitempty"`
	BarrierReleaseContents string                        `json:"barrier_release_contents,omitempty"`
	Crash                  *linuxWitnessExitStatus       `json:"crash,omitempty"`
	JournalSnapshots       []linuxWitnessJournalSnapshot `json:"journal_snapshots"`
	ProcessSnapshots       []linuxWitnessProcessSnapshot `json:"process_snapshots"`
	RawOutput              map[string]string             `json:"raw_output,omitempty"`
	Notes                  []string                      `json:"notes,omitempty"`
}

type linuxWitnessExitStatus struct {
	RequestedSignal string `json:"requested_signal,omitempty"`
	Signaled        bool   `json:"signaled"`
	Signal          string `json:"signal,omitempty"`
	ExitCode        int    `json:"exit_code"`
	Error           string `json:"error,omitempty"`
}

type linuxWitnessJournalSnapshot struct {
	At                  time.Time                  `json:"at"`
	Path                string                     `json:"path"`
	RawSHA256           string                     `json:"raw_sha256"`
	RunID               string                     `json:"run_id"`
	Generation          int64                      `json:"generation"`
	LocalState          string                     `json:"local_state"`
	TerminalState       string                     `json:"terminal_state,omitempty"`
	TerminalVerdict     string                     `json:"terminal_verdict,omitempty"`
	PID                 int                        `json:"pid,omitempty"`
	ProcessIdentity     string                     `json:"process_identity,omitempty"`
	StartedAt           time.Time                  `json:"started_at,omitempty"`
	HasProcessDetails   bool                       `json:"has_process_details"`
	ContainmentUnproven bool                       `json:"containment_unproven"`
	RetainWorkspace     bool                       `json:"retain_workspace"`
	PendingEvents       int                        `json:"pending_events"`
	PendingTransitions  int                        `json:"pending_transitions"`
	Handoff             *linuxWitnessOwnerSnapshot `json:"handoff,omitempty"`
	Authority           *linuxWitnessOwnerSnapshot `json:"authority,omitempty"`
}

type linuxWitnessOwnerSnapshot struct {
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	JobID              string `json:"job_id,omitempty"`
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
		t.Fatalf("prepared handoff crash state = %#v, want unbound handoff without authority", result.Journal)
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
			t.Fatalf("prepared abort left containment state: %#v", snapshot.Journal)
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
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "commit-before-resume")
	reachedPath := filepath.Join(caseRoot, "commit-resume.reached")
	releasePath := filepath.Join(caseRoot, "commit-resume.release")
	evidence := linuxWitnessEvidence{
		Version:            1,
		Case:               "daemon_sigkill_after_commit_before_resume",
		Stage:              "initialized",
		Metadata:           metadata,
		CaseRoot:           caseRoot,
		BarrierReachedPath: reachedPath,
		BarrierReleasePath: releasePath,
		RawOutput:          map[string]string{},
	}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	var (
		daemon                         *productionDaemonWitnessProcess
		second                         *productionDaemonWitnessProcess
		task                           protocol.Task
		committed                      linuxWitnessJournalFile
		key                            state.RunKey
		targetPID, helperPID           int
		targetIdentity, helperIdentity string
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
		if evidence.Stage != "recovered_and_cleared" {
			evidence.Notes = append(evidence.Notes, "teardown diagnostics captured without changing the failure outcome")
		}
		writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	}()

	barrierEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxCommitResumeBarrierReachedEnvironment:   reachedPath,
		linuxCommitResumeBarrierReleaseEnvironment:   releasePath,
	}
	daemon = startLinuxCommitResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", barrierEnvironment)
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()

	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task = submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-commit-before-resume", "slow")
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
	if !strings.Contains(evidence.BarrierReachedContents, "lease_armed=false") {
		t.Fatalf("commit/resume reached marker = %q, want lease_armed=false", evidence.BarrierReachedContents)
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
		t.Fatalf("journal after commit/resume reached marker did not expose durable committed authority: %#v", files)
	}
	if committed.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("committed authority unexpectedly contained stop receipt: %#v", committed.Journal.ContainmentAuthority)
	}
	key = committed.Journal.Key()
	evidence.TaskID = task.TaskID
	evidence.RunID = key.RunID
	evidence.Generation = key.Generation
	evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(committed.Journal)
	targetSnapshot := linuxWitnessProcessSnapshotAt("target_before_resume", targetPID, targetIdentity)
	helperSnapshot := linuxWitnessProcessSnapshotAt("helper_before_resume", helperPID, helperIdentity)
	if !targetSnapshot.Exists || !helperSnapshot.Exists {
		t.Fatalf("commit/resume barrier process snapshots are incomplete: target=%#v helper=%#v", targetSnapshot, helperSnapshot)
	}
	if targetSnapshot.State != "T" && targetSnapshot.State != "t" {
		t.Fatalf("target process state before resume = %q, want Linux stopped state T or t: %#v", targetSnapshot.State, targetSnapshot)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker before resume = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant user-code marker before resume = %v, want absent", err)
	}
	evidence.Stage = "committed_before_resume_lease_unarmed"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(committed))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, targetSnapshot, helperSnapshot)
	evidence.Notes = append(evidence.Notes, "reached marker was observed after durable commit and before initial helper lease arm; target and descendant markers were absent")
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	crash := killLinuxProductionDaemon(daemon)
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", crash)
	}
	evidence.Stage = "daemon_crashed_after_commit_before_resume"
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
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	second = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 45*time.Second); err != nil {
		t.Fatalf("recover commit-before-resume authority: %v", err)
	}
	cleared, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil {
		t.Fatalf("read recovered commit-before-resume journal: %v", err)
	}
	if !found {
		t.Fatalf("recovered commit-before-resume journal %s/%d disappeared", key.RunID, key.Generation)
	}
	if cleared.Journal.ContainmentHandoff != nil || cleared.Journal.ContainmentAuthority != nil || cleared.Journal.HasProcessDetails() {
		t.Fatalf("recovered commit-before-resume journal retained containment: %#v", cleared.Journal)
	}
	if got := getTask(t, operator, task.TaskID); got.RunID == nil || *got.RunID != key.RunID {
		t.Fatalf("recovered task run identity changed: %#v", got)
	}
	evidence.Stage = "recovered_and_cleared"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(cleared))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_recovery", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_recovery", helperPID, helperIdentity))
	second.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
}

func runLinuxDaemonCrashAfterResumeWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "daemon-after-resume")
	evidence := linuxWitnessEvidence{Version: 1, Case: "daemon_sigkill_after_resume_with_descendant", Stage: "initialized", Metadata: metadata, CaseRoot: caseRoot, RawOutput: map[string]string{}}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemon := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-daemon-after-resume", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "resumed authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(file.Journal)
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
	evidence.Stage = "running_after_resume"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, anchor, child,
		linuxWitnessProcessSnapshotAt("helper_before_daemon_crash", helperPID, helperIdentity))
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	crash := killLinuxProductionDaemon(daemon)
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", crash)
	}
	evidence.Stage = "daemon_crashed_after_resume"
	evidence.Crash = &crash
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("daemon owner loss did not stop target group: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("daemon owner loss did not stop same-group descendant: %v", err)
	}

	second := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("recover daemon-crash authority: %v", err)
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
}

func runLinuxHelperCrashMirrorWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "helper-mirror")
	evidence := linuxWitnessEvidence{Version: 1, Case: "helper_sigkill_mirror_takeover", Stage: "initialized", Metadata: metadata, CaseRoot: caseRoot, RawOutput: map[string]string{}}
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	daemon := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-helper-mirror", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "helper authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(file.Journal)
	childPID, err := waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	anchor := linuxWitnessProcessSnapshotAt("target_before_helper_crash", targetPID, targetIdentity)
	child := linuxWitnessProcessSnapshotAt("same_group_descendant_before_helper_crash", childPID, "")
	assertLinuxWitnessSameGroup(t, anchor, child)
	evidence.Stage = "running_before_helper_crash"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, anchor, child,
		linuxWitnessProcessSnapshotAt("helper_before_helper_crash", helperPID, helperIdentity))
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	if err := killLinuxWitnessProcess(t, helperPID, helperIdentity); err != nil {
		t.Fatal(err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Fatalf("killed helper remained live: %v", err)
	}
	assertLinuxWitnessTargetAfterHelperDeath(t, targetPID, targetIdentity)
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("same-group descendant stopped before mirror takeover: %v", err)
	}
	evidence.Stage = "helper_crashed_before_mirror_stop"
	evidence.Crash = &linuxWitnessExitStatus{RequestedSignal: "SIGKILL", Signaled: true, Signal: "SIGKILL", ExitCode: -1}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_helper_crash_before_cancel", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_helper_crash_before_cancel", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_helper_crash", helperPID, helperIdentity))
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)

	cancelTask(t, operator, task.TaskID)
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("mirror takeover did not stop target group: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("mirror takeover did not stop same-group descendant: %v", err)
	}
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("mirror cleanup did not clear journal: %v", err)
	}
	evidence.Stage = "mirror_stopped_and_cleared"
	if snapshot, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key()); err == nil && found {
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(snapshot))
	} else if err != nil {
		t.Fatalf("read mirror journal after cleanup: %v", err)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_mirror_stop", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("same_group_descendant_after_mirror_stop", childPID, ""))
	daemon.stop(t)
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxWitnessEvidence(t, target.EvidencePath, evidence)
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

func startLinuxCommitResumeBarrierDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, agentMode, label string, extraEnvironment map[string]string) *productionDaemonWitnessProcess {
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
		"sleep 600 &\n" +
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
		HasProcessDetails:   journal.HasProcessDetails(),
		ContainmentUnproven: journal.ContainmentUnproven,
		RetainWorkspace:     journal.RetainWorkspace,
		PendingEvents:       len(journal.PendingEvents),
		PendingTransitions:  len(journal.PendingTransitions),
	}
	if journal.ContainmentHandoff != nil {
		owner := linuxWitnessOwnerSnapshotFromHandoff(*journal.ContainmentHandoff)
		snapshot.Handoff = &owner
	}
	if journal.ContainmentAuthority != nil {
		owner := linuxWitnessOwnerSnapshotFromAuthority(*journal.ContainmentAuthority)
		snapshot.Authority = &owner
	}
	return snapshot
}

func linuxWitnessOwnerSnapshotFromHandoff(value authority.SupervisorHandoff) linuxWitnessOwnerSnapshot {
	return linuxWitnessOwnerSnapshot{OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, JobID: value.JobID, HasStopReceipt: value.StopReceipt != nil}
}

func linuxWitnessOwnerSnapshotFromAuthority(value authority.Supervisor) linuxWitnessOwnerSnapshot {
	return linuxWitnessOwnerSnapshot{OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, JobID: value.JobID, HasStopReceipt: value.StopReceipt != nil}
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

func assertLinuxWitnessTargetAfterHelperDeath(t *testing.T, pid int, identity string) {
	t.Helper()
	snapshot := linuxWitnessProcessSnapshotAt("target_after_helper_death", pid, identity)
	if !snapshot.Exists {
		t.Fatalf("target disappeared after helper death: %#v", snapshot)
	}
	if identity != "" && snapshot.Identity != identity {
		t.Fatalf("target identity after helper death = %q, want %q", snapshot.Identity, identity)
	}
	// Pdeathsig may make the ptrace target a zombie immediately when its helper
	// is SIGKILLed. The durable mirror still owns the exact PID/group fence;
	// the same-group descendant must remain live until mirror cancellation.
	if snapshot.State != "Z" && snapshot.State != "T" && snapshot.State != "t" && snapshot.State != "S" && snapshot.State != "R" {
		t.Fatalf("target entered an unexpected state after helper death: %#v", snapshot)
	}
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
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := waitForLinuxWitnessIdentityGone(pid, identity, 100*time.Millisecond); err == nil {
			return nil
		}
		if platform.ProvePersistedProcessGroupAbsent(context.Background(), pid, identity) == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process group led by %d did not reach a stopped proof", pid)
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
