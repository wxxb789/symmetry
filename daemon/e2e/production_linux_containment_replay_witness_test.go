//go:build linux

package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

const linuxProductionContainmentReplayWitnessEnvironment = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_REPLAY_WITNESS"

const (
	linuxReplayProductionWitnessEnv = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxReplayDropOperationEnv     = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_ONCE"
	linuxReplayDropMarkerEnv        = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_FIRED_PATH"
)

// TestProductionLinuxContainmentReplayWitness exercises the production daemon
// and Linux helper across the durable stop-receipt, release, and clear fences.
// A daemon SIGKILL after a receipt write models a lost local response: the next
// production daemon must replay the exact receipt before it can release or
// clear. The second case kills the helper first so the mirror-proof path must
// reconstruct the same receipt after a daemon restart.
func TestProductionLinuxContainmentReplayWitness(t *testing.T) {
	if os.Getenv(linuxProductionContainmentReplayWitnessEnvironment) != "1" {
		t.Skip("set " + linuxProductionContainmentReplayWitnessEnvironment + "=1 to run the Linux production containment replay witness")
	}
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatal("SYMMETRY_E2E=1 is required when the Linux production containment replay witness is enabled")
	}

	environment := loadControlEnvironment(t)
	runRoot := t.TempDir()
	buildRoot := linuxWitnessCaseRoot(runRoot, "artifacts")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		t.Fatalf("create Linux replay witness artifact root: %v", err)
	}
	daemonBinary := buildProductionDaemonWitness(t, buildRoot)
	metadata := newLinuxReplayMetadata(t, daemonBinary)
	writeLinuxWitnessProvenance(t, runRoot, "replay", metadata)

	t.Run("stop_response_lost", func(t *testing.T) {
		runLinuxStopResponseLostCase(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "stop-response-lost"))
	})
	t.Run("release_response_lost", func(t *testing.T) {
		runLinuxReleaseResponseLostCase(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "release-response-lost"))
	})
	t.Run("helper_dead_mirror_proof_receipt_reconstruction", func(t *testing.T) {
		runLinuxContainmentReplayCase(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "helper-dead-mirror"), true)
	})
}

type linuxReplayMetadata struct {
	linuxWitnessMetadata
	TreeDigest      string `json:"tree_digest"`
	DirtyStatusHash string `json:"dirty_status_sha256"`
}

type linuxReplayEvidence struct {
	Version             int                             `json:"version"`
	Case                string                          `json:"case"`
	Stage               string                          `json:"stage"`
	Metadata            linuxReplayMetadata             `json:"metadata"`
	CaseRoot            string                          `json:"case_root"`
	CrashStatuses       []linuxWitnessExitStatus        `json:"crash_statuses,omitempty"`
	JournalSnapshots    []linuxWitnessJournalSnapshot   `json:"journal_snapshots"`
	RawJournalSnapshots []linuxReplayRawJournalSnapshot `json:"raw_journal_snapshots"`
	ProcessSnapshots    []linuxWitnessProcessSnapshot   `json:"process_snapshots"`
	ReceiptBeforeClear  *linuxReplayReceiptSnapshot     `json:"receipt_before_clear,omitempty"`
	ReplayCount         int                             `json:"replay_count"`
	ExactReceiptReplay  bool                            `json:"exact_receipt_replay"`
	ClearObserved       bool                            `json:"clear_observed"`
	ResponseDropOp      string                          `json:"response_drop_operation,omitempty"`
	ResponseDropMarker  string                          `json:"response_drop_marker,omitempty"`
	ResponseDropFired   bool                            `json:"response_drop_fired"`
	DuplicateStopMarker string                          `json:"duplicate_stop_marker,omitempty"`
	DuplicateStopSeen   bool                            `json:"duplicate_stop_seen"`
	RawOutput           map[string]string               `json:"raw_output,omitempty"`
	Notes               []string                        `json:"notes,omitempty"`
}

type linuxReplayRawJournalSnapshot struct {
	At       time.Time       `json:"at"`
	Path     string          `json:"path"`
	SHA256   string          `json:"sha256"`
	Snapshot json.RawMessage `json:"snapshot"`
}

type linuxReplayReceiptSnapshot struct {
	Version            int    `json:"version"`
	Status             string `json:"status"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	PipeTokenSHA256    string `json:"pipe_token_sha256"`
	JobID              string `json:"job_id"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	ActiveProcesses    uint32 `json:"active_processes"`
}

type linuxReplayKillResult struct {
	File linuxWitnessJournalFile
	Exit linuxWitnessExitStatus
	Err  error
}

func newLinuxReplayMetadata(t *testing.T, binaryPath string) linuxReplayMetadata {
	t.Helper()
	metadata := newLinuxWitnessMetadata(t, binaryPath)
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Linux containment replay witness source")
	}
	daemonRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), ".."))
	metadata.SourcePath = sourcePath
	var err error
	if metadata.SourceSHA256, err = linuxWitnessSHA256(sourcePath); err != nil {
		t.Fatalf("hash Linux replay witness source: %v", err)
	}
	status := runLinuxWitnessCommand(t, daemonRoot, "git", "status", "--short", "--untracked-files=all")
	return linuxReplayMetadata{
		linuxWitnessMetadata: metadata,
		TreeDigest:           runLinuxWitnessCommand(t, daemonRoot, "git", "rev-parse", "HEAD^{tree}"),
		DirtyStatusHash:      linuxWitnessHashBytes([]byte(status)),
	}
}

func runLinuxStopResponseLostCase(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxReplayMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "stop-response-lost")
	responseMarker := filepath.Join(caseRoot, "stop-response.fired")
	duplicateStopMarker := filepath.Join(caseRoot, "unexpected-stop.fired")
	evidence := linuxReplayEvidence{
		Version:             1,
		Case:                "stop_response_lost",
		Stage:               "initialized",
		Metadata:            metadata,
		CaseRoot:            caseRoot,
		ResponseDropOp:      "stop",
		ResponseDropMarker:  responseMarker,
		DuplicateStopMarker: duplicateStopMarker,
		RawOutput:           map[string]string{},
		Notes: []string{
			"production helper stop response is dropped once after the stop request is written",
			"the first restart reconstructs the stop receipt; the final restart is armed to fail if it repeats stop",
			"the recovery endpoint is held by a test-owned partial frame until the first daemon SIGKILL",
		},
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	first := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", "stop", responseMarker)
	defer func() {
		if first != nil && !first.waited {
			first.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-stop-response-loss", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux stop-response-loss authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
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
	recoveryBarrierMarker := filepath.Join(caseRoot, "recovery-barrier.fired")
	recoveryBarrier := armLinuxReplayRecoveryBarrier(t, file.Journal.ContainmentAuthority, recoveryBarrierMarker)
	defer recoveryBarrier.release(t)
	cancelTask(t, operator, task.TaskID)
	waitForLinuxReplayMarker(t, responseMarker, 20*time.Second)
	lostFile, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil || !found {
		t.Fatalf("read journal after dropped stop response: found=%t error=%v", found, err)
	}
	if lostFile.Journal.ContainmentAuthority == nil || lostFile.Journal.ContainmentAuthority.StopReceipt != nil || !lostFile.Journal.HasProcessDetails() {
		t.Fatalf("dropped stop response crossed receipt/clear boundary: %#v", lostFile.Journal)
	}
	firstCrash := killLinuxProductionDaemon(first)
	evidence.Stage = "stop_response_lost_before_receipt"
	evidence.ResponseDropFired = true
	evidence.CrashStatuses = append(evidence.CrashStatuses, firstCrash)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(lostFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file), linuxReplayRawJournalSnapshotFromFile(lostFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_stop_response_loss", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_after_stop_response_loss", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_stop_response_loss", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	recoveryBarrier.release(t)

	beforeInfo, err := os.Stat(lostFile.Path)
	if err != nil {
		t.Fatalf("stat journal before stop receipt reconstruction: %v", err)
	}
	second := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", "", "")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	secondKill := make(chan linuxReplayKillResult, 1)
	go func() {
		secondKill <- waitAndKillLinuxReplayDaemon(second, target.StateDir, file.Journal.Key(), beforeInfo.ModTime())
	}()
	secondResult := <-secondKill
	if secondResult.Err != nil {
		t.Fatal(secondResult.Err)
	}
	reconstructed := requireLinuxReplayReceipt(t, secondResult.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, secondResult.File.Journal, reconstructed)
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(reconstructed)
	evidence.ReplayCount = 1
	evidence.ExactReceiptReplay = true
	evidence.Stage = "stop_receipt_reconstructed_before_clear"
	evidence.CrashStatuses = append(evidence.CrashStatuses, secondResult.Exit)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(secondResult.File))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(secondResult.File))
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	third := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third", "stop", duplicateStopMarker)
	defer func() {
		if third != nil && !third.waited {
			third.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("stop receipt release/clear: %v", err)
	}
	assertLinuxReplayMarkerAbsent(t, duplicateStopMarker)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("target after stop receipt replay: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("descendant after stop receipt replay: %v", err)
	}
	if helperPID > 0 {
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("helper after stop receipt replay: %v", err)
		}
	}
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	evidence.Stage = "stop_receipt_replayed_release_clear"
	evidence.ClearObserved = true
	evidence.DuplicateStopSeen = linuxReplayMarkerExists(duplicateStopMarker)
	evidence.RawOutput = linuxWitnessLogs(first, second)
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
}

func runLinuxReleaseResponseLostCase(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxReplayMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "release-response-lost")
	responseMarker := filepath.Join(caseRoot, "release-response.fired")
	duplicateStopMarker := filepath.Join(caseRoot, "unexpected-stop.fired")
	evidence := linuxReplayEvidence{
		Version:             1,
		Case:                "release_response_lost",
		Stage:               "initialized",
		Metadata:            metadata,
		CaseRoot:            caseRoot,
		ResponseDropOp:      "release",
		ResponseDropMarker:  responseMarker,
		DuplicateStopMarker: duplicateStopMarker,
		RawOutput:           map[string]string{},
		Notes: []string{
			"production helper release response is dropped once after the stop receipt is durable",
			"restart must release from the exact existing receipt and must not issue a second stop",
		},
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	first := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", "release", responseMarker)
	defer func() {
		if first != nil && !first.waited {
			first.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-release-response-loss", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux release-response-loss authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
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
	cancelTask(t, operator, task.TaskID)
	receiptFile, err := waitForLinuxWitnessJournal(target.StateDir, "Linux stop receipt before release response loss", 30*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails()
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := requireLinuxReplayReceipt(t, receiptFile.Journal)
	assertLinuxReplayReceiptBeforeClear(t, receiptFile.Journal, receipt)
	waitForLinuxReplayMarker(t, responseMarker, 20*time.Second)
	firstCrash := killLinuxProductionDaemon(first)
	evidence.Stage = "release_response_lost_after_receipt"
	evidence.ResponseDropFired = true
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(receipt)
	evidence.CrashStatuses = append(evidence.CrashStatuses, firstCrash)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(receiptFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file), linuxReplayRawJournalSnapshotFromFile(receiptFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_release_response_loss", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_after_release_response_loss", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_release_response_loss", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	beforeInfo, err := os.Stat(receiptFile.Path)
	if err != nil {
		t.Fatalf("stat journal before release replay: %v", err)
	}
	second := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", "stop", duplicateStopMarker)
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	secondKill := make(chan linuxReplayKillResult, 1)
	go func() {
		secondKill <- waitAndKillLinuxReplayDaemon(second, target.StateDir, file.Journal.Key(), beforeInfo.ModTime())
	}()
	secondResult := <-secondKill
	if secondResult.Err != nil {
		t.Fatal(secondResult.Err)
	}
	replayed := requireLinuxReplayReceipt(t, secondResult.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, secondResult.File.Journal, replayed)
	if replayed != receipt {
		t.Fatalf("release replay changed stop receipt: first=%#v replay=%#v", receipt, replayed)
	}
	evidence.Stage = "release_receipt_replayed_before_clear"
	evidence.ReplayCount = 1
	evidence.ExactReceiptReplay = true
	evidence.CrashStatuses = append(evidence.CrashStatuses, secondResult.Exit)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(secondResult.File))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(secondResult.File))
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	third := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third", "", "")
	defer func() {
		if third != nil && !third.waited {
			third.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("release replay clear: %v", err)
	}
	assertLinuxReplayMarkerAbsent(t, duplicateStopMarker)
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("target after release replay: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("descendant after release replay: %v", err)
	}
	if helperPID > 0 {
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("helper after release replay: %v", err)
		}
	}
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	evidence.Stage = "release_replayed_clear"
	evidence.ClearObserved = true
	evidence.DuplicateStopSeen = linuxReplayMarkerExists(duplicateStopMarker)
	evidence.RawOutput = linuxWitnessLogs(first, second)
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
}

func startLinuxReplayDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, agentMode, label, dropOperation, markerPath string) *productionDaemonWitnessProcess {
	t.Helper()
	stdoutPath := filepath.Join(workDir, label+"-daemon.stdout.log")
	stderrPath := filepath.Join(workDir, label+"-daemon.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open Linux replay daemon stdout: %v", err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open Linux replay daemon stderr: %v", err)
	}
	command := exec.Command(binary, "-config", configPath)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("configure Linux replay daemon: %v", err)
	}
	command.Env = linuxReplayDaemonEnvironment(enrollmentToken, agentMode, dropOperation, markerPath)
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start Linux replay daemon: %v", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
}

func linuxReplayDaemonEnvironment(enrollmentToken, agentMode, dropOperation, markerPath string) []string {
	environment := witnessEnvironment(enrollmentToken, agentMode)
	set := func(key, value string) {
		prefix := key + "="
		for index, entry := range environment {
			if strings.HasPrefix(entry, prefix) {
				environment[index] = prefix + value
				return
			}
		}
		environment = append(environment, prefix+value)
	}
	remove := func(key string) {
		prefix := key + "="
		filtered := environment[:0]
		for _, entry := range environment {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		environment = filtered
	}
	remove(linuxReplayProductionWitnessEnv)
	remove(linuxReplayDropOperationEnv)
	remove(linuxReplayDropMarkerEnv)
	if dropOperation != "" {
		set(linuxReplayProductionWitnessEnv, "1")
		set(linuxReplayDropOperationEnv, dropOperation)
		set(linuxReplayDropMarkerEnv, markerPath)
	}
	return environment
}

func waitForLinuxReplayMarker(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			if string(contents) != "fired\n" {
				t.Fatalf("response-drop marker %q = %q, want fired marker", path, contents)
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read response-drop marker %q: %v", path, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("response-drop marker %q did not fire within %s", path, timeout)
}

func assertLinuxReplayMarkerAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected duplicate-stop response-drop marker %q: %v", path, err)
	}
}

func linuxReplayMarkerExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type linuxReplayRecoveryBarrier struct {
	connection net.Conn
}

func armLinuxReplayRecoveryBarrier(t *testing.T, value *authority.Supervisor, markerPath string) *linuxReplayRecoveryBarrier {
	t.Helper()
	if value == nil {
		t.Fatal("cannot arm Linux replay recovery barrier without containment authority")
	}
	endpoint, err := linuxReplayRecoveryEndpoint(value.OwnerContext)
	if err != nil {
		t.Fatalf("parse Linux replay recovery endpoint: %v", err)
	}
	connection, err := net.DialTimeout("unix", endpoint, 5*time.Second)
	if err != nil {
		t.Fatalf("connect Linux replay recovery barrier: %v", err)
	}
	if _, err := connection.Write([]byte("{")); err != nil {
		_ = connection.Close()
		t.Fatalf("write Linux replay recovery barrier frame: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("fired\n"), 0o600); err != nil {
		_ = connection.Close()
		t.Fatalf("write Linux replay recovery barrier marker: %v", err)
	}
	return &linuxReplayRecoveryBarrier{connection: connection}
}

func (barrier *linuxReplayRecoveryBarrier) release(t *testing.T) {
	t.Helper()
	if barrier == nil || barrier.connection == nil {
		return
	}
	connection := barrier.connection
	barrier.connection = nil
	if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("release Linux replay recovery barrier: %v", err)
	}
}

func linuxReplayRecoveryEndpoint(ownerContext string) (string, error) {
	marker := "|endpoint="
	index := strings.LastIndex(ownerContext, marker)
	if index < 0 {
		return "", errors.New("Linux replay owner context has no recovery endpoint")
	}
	endpoint := strings.TrimSpace(ownerContext[index+len(marker):])
	if endpoint == "" || len(endpoint) >= 108 || !filepath.IsAbs(endpoint) {
		return "", errors.New("Linux replay recovery endpoint is invalid")
	}
	return endpoint, nil
}

func runLinuxContainmentReplayCase(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxReplayMetadata, caseRoot string, killHelper bool) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "containment-replay")
	caseName := "daemon_crash_after_receipt"
	notes := []string{
		"helper response loss and daemon restart are exercised by the dedicated stop_response_lost and release_response_lost cases",
	}
	if killHelper {
		caseName = "helper_dead_mirror_receipt_reconstruction"
		notes = append(notes, "helper was SIGKILLed before cancellation; the Linux mirror supplied the identity-bound stop proof")
	}
	evidence := linuxReplayEvidence{
		Version:     1,
		Case:        caseName,
		Stage:       "initialized",
		Metadata:    metadata,
		CaseRoot:    caseRoot,
		RawOutput:   map[string]string{},
		Notes:       notes,
		ReplayCount: 0,
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	daemon := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, caseName, "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux production containment authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
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
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("target before replay case: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("descendant before replay case: %v", err)
	}
	if killHelper {
		if err := killLinuxWitnessProcess(t, helperPID, helperIdentity); err != nil {
			t.Fatalf("kill Linux supervisor helper: %v", err)
		}
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("wait for Linux supervisor helper death: %v", err)
		}
		// The helper's Pdeathsig may reap the ptrace leader before this snapshot;
		// the same-group descendant is the durable live member that proves the
		// daemon mirror still had physical work to contain after helper death.
		if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
			t.Fatalf("same-group descendant stopped before mirror takeover: %v", err)
		}
	}

	firstKill := make(chan linuxReplayKillResult, 1)
	go func() {
		firstKill <- waitAndKillLinuxReplayDaemon(daemon, target.StateDir, file.Journal.Key(), time.Time{})
	}()
	cancelTask(t, operator, task.TaskID)
	first := <-firstKill
	firstReceipt := requireLinuxReplayReceipt(t, first.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, first.File.Journal, firstReceipt)
	evidence.Stage = "receipt_durable_before_first_daemon_crash"
	evidence.CrashStatuses = append(evidence.CrashStatuses, first.Exit)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(first.File))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file), linuxReplayRawJournalSnapshotFromFile(first.File))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_first_daemon_crash", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_before_first_daemon_crash", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_before_first_daemon_crash", helperPID, helperIdentity),
	)
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(firstReceipt)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	secondBefore, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil || !found {
		t.Fatalf("read receipt journal before replay: found=%t error=%v", found, err)
	}
	secondInfo, err := os.Stat(secondBefore.Path)
	if err != nil {
		t.Fatalf("stat receipt journal before replay: %v", err)
	}
	secondBeforeModTime := secondInfo.ModTime()
	second := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second")
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	secondKill := make(chan linuxReplayKillResult, 1)
	go func() {
		secondKill <- waitAndKillLinuxReplayDaemon(second, target.StateDir, file.Journal.Key(), secondBeforeModTime)
	}()
	secondResult := <-secondKill
	if secondResult.Err != nil {
		t.Fatal(secondResult.Err)
	}
	secondReceipt := requireLinuxReplayReceipt(t, secondResult.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, secondResult.File.Journal, secondReceipt)
	if firstReceipt != secondReceipt {
		t.Fatalf("replayed stop receipt changed: first=%#v second=%#v", firstReceipt, secondReceipt)
	}
	evidence.Stage = "exact_receipt_replay_durable_before_second_daemon_crash"
	evidence.CrashStatuses = append(evidence.CrashStatuses, secondResult.Exit)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(secondResult.File))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(secondResult.File))
	evidence.ReplayCount = 2
	evidence.ExactReceiptReplay = true
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_second_daemon_crash", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_before_second_daemon_crash", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_before_second_daemon_crash", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	third := startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third")
	defer func() {
		if third != nil && !third.waited {
			third.stop(t)
		}
	}()
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("receipt replay release/clear: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("target after receipt replay: %v", err)
	}
	if err := waitForLinuxWitnessProcessStopped(childPID, "", 20*time.Second); err != nil {
		t.Fatalf("descendant after receipt replay: %v", err)
	}
	if helperPID > 0 {
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("helper after receipt replay: %v", err)
		}
	}
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	evidence.Stage = "replayed_receipt_released_and_cleared"
	finalSnapshots, finalAbsent := linuxReplayReleasedSnapshot(t, target.StateDir, file.Journal.Key())
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, finalSnapshots...)
	evidence.ClearObserved = finalAbsent || len(finalSnapshots) > 0
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_replay", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_after_replay", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_replay", helperPID, helperIdentity),
	)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	t.Logf("Linux containment replay evidence: %s", target.EvidencePath)
}

func waitAndKillLinuxReplayDaemon(daemon *productionDaemonWitnessProcess, stateDir string, key state.RunKey, after time.Time) linuxReplayKillResult {
	runsDir := filepath.Join(stateDir, "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		return linuxReplayKillResult{Err: fmt.Errorf("create Linux journal directory: %w", err)}
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return linuxReplayKillResult{Err: fmt.Errorf("initialize Linux journal watcher: %w", err)}
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, runsDir, unix.IN_CREATE|unix.IN_MOVED_TO|unix.IN_CLOSE_WRITE); err != nil {
		return linuxReplayKillResult{Err: fmt.Errorf("watch Linux journal directory: %w", err)}
	}
	deadline := time.Now().Add(45 * time.Second)
	buffer := make([]byte, 32<<10)
	for time.Now().Before(deadline) {
		files, scanErr := readLinuxWitnessJournals(stateDir)
		if scanErr != nil {
			return linuxReplayKillResult{Err: fmt.Errorf("read Linux journal directory: %w", scanErr)}
		}
		for _, file := range files {
			if file.Journal.Key() != key || file.Journal.ContainmentAuthority == nil || file.Journal.ContainmentAuthority.StopReceipt == nil || !file.Journal.HasProcessDetails() {
				continue
			}
			if !after.IsZero() {
				info, statErr := os.Stat(file.Path)
				if statErr != nil || !info.ModTime().After(after) {
					continue
				}
			}
			exit := killLinuxProductionDaemon(daemon)
			return linuxReplayKillResult{File: file, Exit: exit}
		}
		if _, readErr := unix.Read(fd, buffer); readErr != nil && !errors.Is(readErr, unix.EAGAIN) && !errors.Is(readErr, unix.EINTR) {
			return linuxReplayKillResult{Err: fmt.Errorf("read Linux journal watcher: %w", readErr)}
		}
		runtime.Gosched()
	}
	return linuxReplayKillResult{Err: fmt.Errorf("Linux journal %s/%d did not expose a durable stop receipt within 45s", key.RunID, key.Generation)}
}

func requireLinuxReplayReceipt(t *testing.T, journal state.RunJournal) authority.StopReceipt {
	t.Helper()
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil {
		t.Fatalf("journal has no durable containment receipt: %#v", journal)
	}
	receipt := *journal.ContainmentAuthority.StopReceipt
	if !receipt.ValidFor(*journal.ContainmentAuthority) {
		t.Fatalf("journal stop receipt is not bound to authority: %#v", receipt)
	}
	return receipt
}

func linuxReplayReceiptSnapshotFromReceipt(receipt authority.StopReceipt) *linuxReplayReceiptSnapshot {
	return &linuxReplayReceiptSnapshot{
		Version:            receipt.Version,
		Status:             receipt.Status,
		OwnerKind:          receipt.OwnerKind,
		OwnerContext:       receipt.OwnerContext,
		TargetPID:          receipt.TargetPID,
		TargetIdentity:     receipt.TargetIdentity,
		PipeTokenSHA256:    linuxWitnessHashBytes([]byte(receipt.PipeToken)),
		JobID:              receipt.JobID,
		SupervisorPID:      receipt.SupervisorPID,
		SupervisorIdentity: receipt.SupervisorIdentity,
		ActiveProcesses:    receipt.ActiveProcesses,
	}
}

func assertLinuxReplayReceiptBeforeClear(t *testing.T, journal state.RunJournal, receipt authority.StopReceipt) {
	t.Helper()
	if !journal.HasProcessDetails() || journal.ContainmentAuthority == nil {
		t.Fatalf("receipt was observed after clear: %#v", journal)
	}
	if journal.ContainmentAuthority.StopReceipt == nil || *journal.ContainmentAuthority.StopReceipt != receipt {
		t.Fatalf("journal receipt changed before clear: %#v", journal.ContainmentAuthority)
	}
}

func linuxReplayReleasedSnapshot(t *testing.T, stateDir string, key state.RunKey) ([]linuxWitnessJournalSnapshot, bool) {
	t.Helper()
	file, found, err := readLinuxWitnessJournalForKey(stateDir, key)
	if err != nil {
		t.Fatalf("read final Linux replay journal: %v", err)
	}
	if !found {
		return nil, true
	}
	if file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentAuthority != nil || file.Journal.HasProcessDetails() {
		t.Fatalf("final Linux replay journal retained containment: %#v", file.Journal)
	}
	return []linuxWitnessJournalSnapshot{linuxWitnessJournalSnapshotFromFile(file)}, false
}

func linuxReplayRawJournalSnapshotFromFile(file linuxWitnessJournalFile) linuxReplayRawJournalSnapshot {
	snapshot := linuxWitnessJournalSnapshotFromFile(file)
	raw, _ := json.Marshal(snapshot)
	return linuxReplayRawJournalSnapshot{At: time.Now().UTC(), Path: file.Path, SHA256: linuxWitnessHashBytes(file.Raw), Snapshot: raw}
}

func writeLinuxReplayEvidence(t *testing.T, path string, evidence linuxReplayEvidence) {
	t.Helper()
	contents, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode Linux replay evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create Linux replay evidence directory: %v", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".linux-replay-")
	if err != nil {
		t.Fatalf("create Linux replay evidence temp file: %v", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		t.Fatalf("write Linux replay evidence: %v", err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("close Linux replay evidence: %v", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		t.Fatalf("publish Linux replay evidence: %v", err)
	}
}
