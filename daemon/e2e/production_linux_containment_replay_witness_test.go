//go:build linux

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	linuxReplayProductionWitnessEnv     = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxReplayDropOperationEnv         = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_ONCE"
	linuxReplayDropMarkerEnv            = "SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_FIRED_PATH"
	linuxReplayReleaseBarrierReachedEnv = "SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_REACHED_FILE"
	linuxReplayReleaseBarrierReleaseEnv = "SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_RELEASE_FILE"
)

// TestProductionLinuxContainmentReplayWitness exercises the production daemon
// and Linux helper across the durable stop-receipt, release, and clear fences.
// The stop-response-loss case waits for authenticated recovery to persist and
// read back the exact receipt before crashing the daemon; restart must replay
// it before release or clear. The helper-death case seals both scanners first,
// then proves a same-group descendant remains live while the daemon is paused
// before the mirror takes over.
func TestProductionLinuxContainmentReplayWitness(t *testing.T) {
	if os.Getenv(linuxProductionContainmentReplayWitnessEnvironment) != "1" {
		t.Skip("set " + linuxProductionContainmentReplayWitnessEnvironment + "=1 to run the Linux production containment replay witness")
	}
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatal("SYMMETRY_E2E=1 is required when the Linux production containment replay witness is enabled")
	}

	environment := loadControlEnvironment(t)
	runRoot := linuxWitnessRunRoot(t)
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
		runLinuxHelperCrashReplayWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "helper-dead-mirror"))
	})
}

type linuxReplayMetadata struct {
	linuxWitnessMetadata
	TreeDigest      string `json:"tree_digest"`
	DirtyStatusHash string `json:"dirty_status_sha256"`
}

type linuxReplayEvidence struct {
	Version                         int                                     `json:"version"`
	Case                            string                                  `json:"case"`
	Stage                           string                                  `json:"stage"`
	Metadata                        linuxReplayMetadata                     `json:"metadata"`
	CaseRoot                        string                                  `json:"case_root"`
	TaskID                          string                                  `json:"task_id,omitempty"`
	RunID                           string                                  `json:"run_id,omitempty"`
	Generation                      int64                                   `json:"generation,omitempty"`
	JournalKey                      string                                  `json:"journal_key,omitempty"`
	CrashStatuses                   []linuxWitnessExitStatus                `json:"crash_statuses,omitempty"`
	JournalSnapshots                []linuxWitnessJournalSnapshot           `json:"journal_snapshots"`
	RawJournalSnapshots             []linuxReplayRawJournalSnapshot         `json:"raw_journal_snapshots"`
	ProcessSnapshots                []linuxWitnessProcessSnapshot           `json:"process_snapshots"`
	ReceiptBeforeClear              *linuxReplayReceiptSnapshot             `json:"receipt_before_clear,omitempty"`
	ReplayCount                     int                                     `json:"replay_count"`
	ExactReceiptReplay              bool                                    `json:"exact_receipt_replay"`
	ClearObserved                   bool                                    `json:"clear_observed"`
	ResponseDropOp                  string                                  `json:"response_drop_operation,omitempty"`
	ResponseDropMarker              string                                  `json:"response_drop_marker,omitempty"`
	ResponseDropFired               bool                                    `json:"response_drop_fired"`
	BarrierReachedPath              string                                  `json:"barrier_reached_path,omitempty"`
	BarrierReleasePath              string                                  `json:"barrier_release_path,omitempty"`
	BarrierReached                  string                                  `json:"barrier_reached,omitempty"`
	BarrierRelease                  string                                  `json:"barrier_release,omitempty"`
	StopReceiptBarrierReachedPath   string                                  `json:"stop_receipt_barrier_reached_path,omitempty"`
	StopReceiptBarrierReleasePath   string                                  `json:"stop_receipt_barrier_release_path,omitempty"`
	StopReceiptBarrierReached       *linuxWitnessStopReceiptBarrierSnapshot `json:"stop_receipt_barrier_reached,omitempty"`
	StopReceiptBarrierRelease       string                                  `json:"stop_receipt_barrier_release,omitempty"`
	ReplayReceiptBarrierReachedPath string                                  `json:"replay_receipt_barrier_reached_path,omitempty"`
	ReplayReceiptBarrierReleasePath string                                  `json:"replay_receipt_barrier_release_path,omitempty"`
	ReplayReceiptBarrierReached     *linuxWitnessStopReceiptBarrierSnapshot `json:"replay_receipt_barrier_reached,omitempty"`
	ReplayReceiptBarrierRelease     string                                  `json:"replay_receipt_barrier_release,omitempty"`
	ScanTriggerPath                 string                                  `json:"scan_trigger_path,omitempty"`
	ScanReachedPath                 string                                  `json:"scan_reached_path,omitempty"`
	ScanReleasePath                 string                                  `json:"scan_release_path,omitempty"`
	ScanHelperReached               string                                  `json:"scan_helper_reached,omitempty"`
	ScanMirrorReached               string                                  `json:"scan_mirror_reached,omitempty"`
	ScanRelease                     string                                  `json:"scan_release,omitempty"`
	DuplicateStopMarker             string                                  `json:"duplicate_stop_marker,omitempty"`
	DuplicateStopSeen               bool                                    `json:"duplicate_stop_seen"`
	RawOutput                       map[string]string                       `json:"raw_output,omitempty"`
	Notes                           []string                                `json:"notes,omitempty"`
}

type linuxReplayRawJournalSnapshot struct {
	At       time.Time       `json:"at"`
	Path     string          `json:"path"`
	SHA256   string          `json:"sha256"`
	Snapshot json.RawMessage `json:"snapshot"`
}

type linuxReplayReceiptSnapshot struct {
	Version            int    `json:"version"`
	LegacyV1NoV2Proof  bool   `json:"legacy_v1_no_v2_proof"`
	Status             string `json:"status"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContextSHA256 string `json:"owner_context_sha256,omitempty"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	PipeTokenSHA256    string `json:"pipe_token_sha256"`
	JobIDSHA256        string `json:"job_id_sha256,omitempty"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	ActiveProcesses    uint32 `json:"active_processes"`
}

type linuxReplayKillResult struct {
	File linuxWitnessJournalFile
	Exit linuxWitnessExitStatus
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
	scanTriggerPath := filepath.Join(caseRoot, "stop-response-loss-scan.trigger")
	scanReachedPath := filepath.Join(caseRoot, "stop-response-loss-scan.reached")
	scanReleasePath := filepath.Join(caseRoot, "stop-response-loss-scan.release")
	receiptReachedPath := filepath.Join(caseRoot, "stop-receipt-before-release.reached")
	receiptReleasePath := filepath.Join(caseRoot, "stop-receipt-before-release.release")
	replayReachedPath := filepath.Join(caseRoot, "replay-receipt-before-release.reached")
	replayReleasePath := filepath.Join(caseRoot, "replay-receipt-before-release.release")
	evidence := linuxReplayEvidence{
		Version:                         1,
		Case:                            "stop_response_lost",
		Stage:                           "initialized",
		Metadata:                        metadata,
		CaseRoot:                        caseRoot,
		ResponseDropOp:                  "stop",
		ResponseDropMarker:              responseMarker,
		StopReceiptBarrierReachedPath:   receiptReachedPath,
		StopReceiptBarrierReleasePath:   receiptReleasePath,
		ReplayReceiptBarrierReachedPath: replayReachedPath,
		ReplayReceiptBarrierReleasePath: replayReleasePath,
		ScanTriggerPath:                 scanTriggerPath,
		ScanReachedPath:                 scanReachedPath,
		ScanReleasePath:                 scanReleasePath,
		DuplicateStopMarker:             duplicateStopMarker,
		RawOutput:                       map[string]string{},
		Notes: []string{
			"production helper stop response is dropped once after the stop request is written",
			"authenticated recovery persists and reads back the exact receipt before the first daemon SIGKILL",
			"both durable scanner seals are observed before cancellation and retained with the receipt",
			"the restart replays the exact same receipt behind the durable-readback barrier before a final restart releases and clears it",
		},
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	firstEnvironment := linuxWitnessScanSealEnvironment(scanTriggerPath, scanReachedPath, scanReleasePath, "both")
	firstEnvironment[linuxStopReceiptBarrierReachedEnvironment] = receiptReachedPath
	firstEnvironment[linuxStopReceiptBarrierReleaseEnvironment] = receiptReleasePath
	first := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", "stop", responseMarker, "", "", firstEnvironment)
	defer func() {
		if first != nil && !first.waited {
			first.stop(t)
		}
	}()
	firstReceiptBarrierReleased := false
	defer func() {
		if !firstReceiptBarrierReleased {
			if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
				t.Errorf("release first Linux stop-receipt barrier during cleanup: %v", err)
			} else {
				if contents, err := os.ReadFile(receiptReleasePath); err == nil {
					evidence.StopReceiptBarrierRelease = string(contents)
				} else {
					t.Errorf("read first stop-receipt barrier release during cleanup: %v", err)
				}
			}
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
	sealedFile, helperReached, mirrorReached, err := waitForLinuxWitnessScanSeal(target.StateDir, file.Journal.Key(), scanTriggerPath, scanReachedPath, scanReleasePath, "both", 45*time.Second)
	if err != nil {
		t.Fatalf("wait for durable helper/mirror scan seals before stop-response loss: %v", err)
	}
	if !linuxWitnessJournalHasScanSealBoundary(sealedFile.Journal, "both") {
		t.Fatalf("stop-response-loss journal lacks both durable scanner seals: %#v", linuxWitnessJournalSnapshotFromFile(sealedFile).ContainmentScans)
	}
	anchor := linuxWitnessProcessSnapshotAt("target_before_stop_response_loss", targetPID, targetIdentity)
	child := linuxWitnessProcessSnapshotAt("same_group_descendant_before_stop_response_loss", childPID, "")
	assertLinuxWitnessSameGroup(t, anchor, child)
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("target before stop-response loss: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("same-group descendant before stop-response loss: %v", err)
	}
	for _, path := range []string{responseMarker, receiptReachedPath, receiptReleasePath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stop-response-loss barrier marker %q before cancel = %v, want absent", path, err)
		}
	}
	cancelTask(t, operator, task.TaskID)
	waitForLinuxReplayMarker(t, responseMarker, 20*time.Second)
	if err := waitForLinuxWitnessMarker(receiptReachedPath, 20*time.Second); err != nil {
		t.Fatalf("wait for durable stop-receipt-before-release marker after authenticated recovery: %v", err)
	}
	receiptReached, err := os.ReadFile(receiptReachedPath)
	if err != nil {
		t.Fatalf("read durable stop-receipt-before-release marker: %v", err)
	}
	firstMarker, firstMarkerSnapshot, err := decodeLinuxWitnessStopReceiptBarrierMarker(receiptReached)
	if err != nil {
		t.Fatalf("decode durable stop-receipt-before-release marker: %v", err)
	}
	receiptFile, err := waitForLinuxWitnessJournal(target.StateDir, "durable stop receipt after response loss", 10*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.Key() == file.Journal.Key() && value.Journal.ContainmentAuthority != nil &&
			value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails() &&
			linuxWitnessJournalHasScanSealBoundary(value.Journal, "both")
	})
	if err != nil {
		t.Fatalf("read durable receipt with both scanner seals after authenticated recovery: %v", err)
	}
	firstReceipt := requireLinuxReplayReceipt(t, receiptFile.Journal)
	assertLinuxReplayReceiptBeforeClear(t, receiptFile.Journal, firstReceipt)
	gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(receiptFile.Journal)
	if firstMarker.RunID != file.Journal.RunID || firstMarker.Generation != file.Journal.Generation ||
		firstMarker.PID != targetPID || firstMarker.Identity != targetIdentity || firstMarker.Receipt != firstReceipt ||
		gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
		t.Fatalf("durable stop-receipt barrier marker/journal identity mismatch: marker=(%s/%d,%d,%q,%#v) journal=(%s/%d,%d,%q,%d,%q,%#v)",
			firstMarker.RunID, firstMarker.Generation, firstMarker.PID, firstMarker.Identity, linuxReplayReceiptSnapshotFromReceipt(firstMarker.Receipt),
			receiptFile.Journal.RunID, receiptFile.Journal.Generation, gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity,
			linuxReplayReceiptSnapshotFromReceipt(firstReceipt))
	}
	if receiptFile.Journal.ContainmentUnproven {
		t.Fatalf("durable stop receipt is marked containment-unproven: %#v", linuxWitnessJournalSnapshotFromFile(receiptFile))
	}
	if _, err := os.Lstat(receiptReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first stop-receipt barrier release marker before daemon crash = %v, want absent", err)
	}
	evidence.StopReceiptBarrierReached = &firstMarkerSnapshot
	evidence.StopReceiptBarrierRelease = "<absent>"
	evidence.ScanHelperReached = helperReached
	evidence.ScanMirrorReached = mirrorReached
	scanReleaseContents, err := os.ReadFile(scanReleasePath)
	if err != nil {
		t.Fatalf("read durable scan-seal release marker: %v", err)
	}
	evidence.ScanRelease = string(scanReleaseContents)
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(firstReceipt)
	evidence.ResponseDropFired = true
	evidence.Stage = "stop_receipt_durable_before_first_daemon_crash"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(sealedFile), linuxWitnessJournalSnapshotFromFile(receiptFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file), linuxReplayRawJournalSnapshotFromFile(sealedFile), linuxReplayRawJournalSnapshotFromFile(receiptFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_at_stop_receipt_before_release", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_at_stop_receipt_before_release", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_at_stop_receipt_before_release", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	firstCrash := killLinuxProductionDaemon(first)
	if firstCrash.RequestedSignal != "SIGKILL" || !firstCrash.Signaled || firstCrash.Signal != "SIGKILL" {
		t.Fatalf("first daemon crash status = %#v, want SIGKILL after durable receipt observation", firstCrash)
	}
	evidence.CrashStatuses = append(evidence.CrashStatuses, firstCrash)
	evidence.Stage = "first_daemon_crashed_after_durable_stop_receipt"
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	replayEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxStopReceiptBarrierReachedEnvironment:    replayReachedPath,
		linuxStopReceiptBarrierReleaseEnvironment:    replayReleasePath,
	}
	second := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", "", "", "", "", replayEnvironment)
	defer func() {
		if second != nil && !second.waited {
			second.stop(t)
		}
	}()
	secondReceiptBarrierReleased := false
	defer func() {
		if !secondReceiptBarrierReleased {
			if err := writeLinuxWitnessReleaseMarker(replayReleasePath); err != nil {
				t.Errorf("release replay stop-receipt barrier during cleanup: %v", err)
			} else {
				if contents, err := os.ReadFile(replayReleasePath); err == nil {
					evidence.ReplayReceiptBarrierRelease = string(contents)
				} else {
					t.Errorf("read replay stop-receipt barrier release during cleanup: %v", err)
				}
			}
		}
	}()
	if err := waitForLinuxWitnessMarker(replayReachedPath, 20*time.Second); err != nil {
		t.Fatalf("wait for durable replay stop-receipt-before-release marker: %v", err)
	}
	replayReached, err := os.ReadFile(replayReachedPath)
	if err != nil {
		t.Fatalf("read replay stop-receipt-before-release marker: %v", err)
	}
	replayMarker, replayMarkerSnapshot, err := decodeLinuxWitnessStopReceiptBarrierMarker(replayReached)
	if err != nil {
		t.Fatalf("decode replay stop-receipt-before-release marker: %v", err)
	}
	replayedFile, err := waitForLinuxWitnessJournal(target.StateDir, "exact replayed stop receipt before release", 10*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.Key() == file.Journal.Key() && value.Journal.ContainmentAuthority != nil &&
			value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails()
	})
	if err != nil {
		t.Fatalf("read exact replayed stop receipt before release: %v", err)
	}
	replayed := requireLinuxReplayReceipt(t, replayedFile.Journal)
	assertLinuxReplayReceiptBeforeClear(t, replayedFile.Journal, replayed)
	if replayed != firstReceipt || replayMarker.RunID != file.Journal.RunID || replayMarker.Generation != file.Journal.Generation ||
		replayMarker.PID != targetPID || replayMarker.Identity != targetIdentity || replayMarker.Receipt != firstReceipt {
		t.Fatalf("replayed stop receipt/barrier changed durable identity: first=%#v replayed=%#v marker=(%s/%d,%d,%q,%#v)",
			linuxReplayReceiptSnapshotFromReceipt(firstReceipt), linuxReplayReceiptSnapshotFromReceipt(replayed), replayMarker.RunID, replayMarker.Generation, replayMarker.PID, replayMarker.Identity,
			linuxReplayReceiptSnapshotFromReceipt(replayMarker.Receipt))
	}
	if _, err := os.Lstat(replayReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replay stop-receipt barrier release marker before daemon crash = %v, want absent", err)
	}
	evidence.ReplayReceiptBarrierReached = &replayMarkerSnapshot
	evidence.ReplayReceiptBarrierRelease = "<absent>"
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(replayed)
	evidence.ReplayCount = 1
	evidence.ExactReceiptReplay = true
	evidence.Stage = "exact_stop_receipt_replayed_before_clear"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(replayedFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(replayedFile))
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	secondCrash := killLinuxProductionDaemon(second)
	if secondCrash.RequestedSignal != "SIGKILL" || !secondCrash.Signaled || secondCrash.Signal != "SIGKILL" {
		t.Fatalf("replay daemon crash status = %#v, want SIGKILL before release/clear", secondCrash)
	}
	evidence.CrashStatuses = append(evidence.CrashStatuses, secondCrash)
	evidence.Stage = "exact_receipt_replayed_before_replay_daemon_crash"
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	if err := writeLinuxWitnessReleaseMarker(receiptReleasePath); err != nil {
		t.Fatalf("release first stop-receipt barrier after exact replay: %v", err)
	}
	firstReceiptBarrierReleased = true
	firstReleaseContents, err := os.ReadFile(receiptReleasePath)
	if err != nil {
		t.Fatalf("read first stop-receipt barrier release: %v", err)
	}
	evidence.StopReceiptBarrierRelease = string(firstReleaseContents)
	if err := writeLinuxWitnessReleaseMarker(replayReleasePath); err != nil {
		t.Fatalf("release replay stop-receipt barrier after SIGKILL: %v", err)
	}
	secondReceiptBarrierReleased = true
	replayReleaseContents, err := os.ReadFile(replayReleasePath)
	if err != nil {
		t.Fatalf("read replay stop-receipt barrier release: %v", err)
	}
	evidence.ReplayReceiptBarrierRelease = string(replayReleaseContents)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	third := startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third", "stop", duplicateStopMarker, "", "", nil)
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
	finalSnapshots, finalAbsent := linuxReplayReleasedSnapshot(t, target.StateDir, file.Journal.Key())
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, finalSnapshots...)
	if finalFile, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key()); err == nil && found {
		evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(finalFile))
	}
	evidence.ClearObserved = finalAbsent || len(finalSnapshots) > 0
	evidence.DuplicateStopSeen = linuxReplayMarkerExists(duplicateStopMarker)
	evidence.RawOutput = linuxWitnessLogs(first, second)
	for path, contents := range linuxWitnessLogs(third, nil) {
		evidence.RawOutput[path] = contents
	}
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
}

func runLinuxReleaseResponseLostCase(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxReplayMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "release-response-lost")
	responseMarker := filepath.Join(caseRoot, "release-response.fired")
	barrierReachedPath := filepath.Join(caseRoot, "release-response-loss.reached")
	barrierReleasePath := filepath.Join(caseRoot, "release-response-loss.release")
	replayReachedPath := filepath.Join(caseRoot, "replay-receipt-before-release.reached")
	replayReleasePath := filepath.Join(caseRoot, "replay-receipt-before-release.release")
	duplicateStopMarker := filepath.Join(caseRoot, "unexpected-stop.fired")
	evidence := linuxReplayEvidence{
		Version:                         1,
		Case:                            "release_response_lost",
		Stage:                           "initialized",
		Metadata:                        metadata,
		CaseRoot:                        caseRoot,
		ResponseDropOp:                  "release",
		ResponseDropMarker:              responseMarker,
		BarrierReachedPath:              barrierReachedPath,
		BarrierReleasePath:              barrierReleasePath,
		ReplayReceiptBarrierReachedPath: replayReachedPath,
		ReplayReceiptBarrierReleasePath: replayReleasePath,
		DuplicateStopMarker:             duplicateStopMarker,
		RawOutput:                       map[string]string{},
		Notes: []string{
			"the daemon release request is written once, its response transport is dropped once, and automatic recovery is held behind a test-owned barrier",
			"restart must replay the exact existing receipt and hit the durable-readback callback bound to this run and generation before helper release",
			"the replay daemon is SIGKILLed only after the callback marker and full in-memory receipt match; the final restart performs one release and never issues a duplicate stop",
		},
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	var (
		first, second, third           *productionDaemonWitnessProcess
		task                           protocol.Task
		key                            state.RunKey
		targetPID, helperPID           int
		targetIdentity, helperIdentity string
		childPID                       int
		receipt                        authority.StopReceipt
	)
	defer func() {
		if contents, err := os.ReadFile(barrierReachedPath); err == nil {
			evidence.BarrierReached = string(contents)
		} else if !errors.Is(err, os.ErrNotExist) {
			evidence.Notes = append(evidence.Notes, fmt.Sprintf("read release barrier reached marker during teardown: %v", err))
		}
		if contents, err := os.ReadFile(barrierReleasePath); err == nil {
			evidence.BarrierRelease = string(contents)
		} else if errors.Is(err, os.ErrNotExist) {
			evidence.BarrierRelease = "<absent>"
		} else {
			evidence.Notes = append(evidence.Notes, fmt.Sprintf("read release barrier marker during teardown: %v", err))
		}
		if key.RunID != "" {
			if current, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
				evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(current))
				evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(current))
				if targetPID == 0 {
					targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(current.Journal)
				}
			} else if err != nil {
				evidence.Notes = append(evidence.Notes, fmt.Sprintf("read release journal during teardown: %v", err))
			}
		}
		if targetPID > 0 {
			evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
				linuxWitnessProcessSnapshotAt("teardown_target", targetPID, targetIdentity),
				linuxWitnessProcessSnapshotAt("teardown_descendant", childPID, ""),
				linuxWitnessProcessSnapshotAt("teardown_helper", helperPID, helperIdentity))
		}
		evidence.RawOutput = linuxWitnessLogs(first, second)
		for path, contents := range linuxWitnessLogs(third, nil) {
			evidence.RawOutput[path] = contents
		}
		if evidence.Stage != "release_replayed_clear" {
			evidence.Notes = append(evidence.Notes, "teardown diagnostics captured without changing the failure outcome")
		}
		writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

		if first != nil && !first.waited {
			first.stop(t)
		}
		if second != nil && !second.waited {
			second.stop(t)
		}
		if third != nil && !third.waited {
			third.stop(t)
		}
	}()

	first = startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", "release", responseMarker, barrierReachedPath, barrierReleasePath, nil)
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task = submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-release-response-loss", "slow")
	evidence.TaskID = task.TaskID
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux release-response-loss authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	key = file.Journal.Key()
	evidence.RunID = key.RunID
	evidence.Generation = key.Generation
	evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	childPID, err = waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(barrierReachedPath) == filepath.Clean(barrierReleasePath) {
		t.Fatalf("release response-loss barrier paths must be distinct: %q", barrierReachedPath)
	}
	if _, err := os.Lstat(barrierReachedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release response-loss reached marker before cancel = %v, want absent", err)
	}
	if _, err := os.Lstat(barrierReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release response-loss release marker before cancel = %v, want absent", err)
	}
	cancelTask(t, operator, task.TaskID)
	receiptFile, err := waitForLinuxWitnessJournal(target.StateDir, "Linux stop receipt before release response loss", 30*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails()
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt = requireLinuxReplayReceipt(t, receiptFile.Journal)
	assertLinuxReplayReceiptBeforeClear(t, receiptFile.Journal, receipt)
	waitForLinuxReplayMarker(t, responseMarker, 20*time.Second)
	if err := waitForLinuxWitnessMarker(barrierReachedPath, 20*time.Second); err != nil {
		t.Fatalf("wait for release response-loss reached marker: %v", err)
	}
	reachedContents, err := os.ReadFile(barrierReachedPath)
	if err != nil {
		t.Fatalf("read release response-loss reached marker: %v", err)
	}
	evidence.BarrierReached = string(reachedContents)
	if !strings.Contains(evidence.BarrierReached, "operation=release\n") || !strings.Contains(evidence.BarrierReached, "receipt_present=true\n") || !strings.Contains(evidence.BarrierReached, "pid=") || !strings.Contains(evidence.BarrierReached, "time=") {
		t.Fatalf("release response-loss reached marker = %q, want daemon identity and release receipt witness", evidence.BarrierReached)
	}
	if _, err := os.Lstat(barrierReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release response-loss barrier released before daemon crash = %v, want absent", err)
	}
	reachedFile, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil || !found {
		t.Fatalf("read journal after release response-loss reached marker: found=%t error=%v", found, err)
	}
	reachedReceipt := requireLinuxReplayReceipt(t, reachedFile.Journal)
	if reachedReceipt != receipt {
		t.Fatalf("release response-loss reached journal changed receipt: before=%#v reached=%#v", linuxReplayReceiptSnapshotFromReceipt(receipt), linuxReplayReceiptSnapshotFromReceipt(reachedReceipt))
	}
	firstCrash := killLinuxProductionDaemon(first)
	if firstCrash.RequestedSignal != "SIGKILL" || !firstCrash.Signaled || firstCrash.Signal != "SIGKILL" {
		t.Fatalf("first daemon crash status = %#v, want SIGKILL", firstCrash)
	}
	evidence.Stage = "release_response_lost_before_recovery"
	evidence.ResponseDropFired = true
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(receipt)
	evidence.CrashStatuses = append(evidence.CrashStatuses, firstCrash)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(receiptFile), linuxWitnessJournalSnapshotFromFile(reachedFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file), linuxReplayRawJournalSnapshotFromFile(receiptFile), linuxReplayRawJournalSnapshotFromFile(reachedFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_before_first_daemon_crash", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_before_first_daemon_crash", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_before_first_daemon_crash", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	if err := os.WriteFile(barrierReleasePath, []byte("released\n"), 0o600); err != nil {
		t.Fatalf("release Linux response-loss daemon barrier: %v", err)
	}
	evidence.BarrierRelease = "released\n"

	for _, path := range []string{replayReachedPath, replayReleasePath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replay stop-receipt barrier marker %q before second daemon = %v, want absent", path, err)
		}
	}
	secondReplayEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxStopReceiptBarrierReachedEnvironment:    replayReachedPath,
		linuxStopReceiptBarrierReleaseEnvironment:    replayReleasePath,
	}
	second = startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", "stop", duplicateStopMarker, "", "", secondReplayEnvironment)
	secondReplayBarrierReleased := false
	defer func() {
		if !secondReplayBarrierReleased {
			if err := writeLinuxWitnessReleaseMarker(replayReleasePath); err != nil {
				t.Errorf("release replay stop-receipt barrier during cleanup: %v", err)
			} else if contents, err := os.ReadFile(replayReleasePath); err != nil {
				t.Errorf("read replay stop-receipt barrier release during cleanup: %v", err)
			} else {
				evidence.ReplayReceiptBarrierRelease = string(contents)
			}
		}
	}()
	if err := waitForLinuxWitnessMarker(replayReachedPath, 20*time.Second); err != nil {
		t.Fatalf("wait for durable replay stop-receipt-before-release marker: %v", err)
	}
	replayReached, err := os.ReadFile(replayReachedPath)
	if err != nil {
		t.Fatalf("read replay stop-receipt-before-release marker: %v", err)
	}
	replayMarker, replayMarkerSnapshot, err := decodeLinuxWitnessStopReceiptBarrierMarker(replayReached)
	if err != nil {
		t.Fatalf("decode replay stop-receipt-before-release marker: %v", err)
	}
	replayedFile, err := waitForLinuxWitnessJournal(target.StateDir, "exact replayed stop receipt before release", 10*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.Key() == key && value.Journal.ContainmentAuthority != nil &&
			value.Journal.ContainmentAuthority.StopReceipt != nil && value.Journal.HasProcessDetails()
	})
	if err != nil {
		t.Fatalf("read exact replayed stop receipt before release: %v", err)
	}
	replayed := requireLinuxReplayReceipt(t, replayedFile.Journal)
	assertLinuxReplayReceiptBeforeClear(t, replayedFile.Journal, replayed)
	if replayed != receipt || replayMarker.RunID != key.RunID || replayMarker.Generation != key.Generation ||
		replayMarker.PID != targetPID || replayMarker.Identity != targetIdentity || replayMarker.Receipt != receipt {
		t.Fatalf("release replay changed stop receipt or callback identity: first=%#v replay=%#v marker=(%s/%d,%d,%q,%#v)",
			linuxReplayReceiptSnapshotFromReceipt(receipt), linuxReplayReceiptSnapshotFromReceipt(replayed), replayMarker.RunID, replayMarker.Generation, replayMarker.PID, replayMarker.Identity,
			linuxReplayReceiptSnapshotFromReceipt(replayMarker.Receipt))
	}
	if _, err := os.Lstat(replayReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replay stop-receipt barrier release marker before daemon crash = %v, want absent", err)
	}
	// The lost release response may follow a successful helper exit or an
	// unapplied release. Neither outcome changes the durable stop receipt; the
	// final replay must prove the exact helper gone before containment clears.
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("helper_at_replayed_receipt_before_release", helperPID, helperIdentity))
	evidence.ReplayReceiptBarrierReached = &replayMarkerSnapshot
	evidence.ReplayReceiptBarrierRelease = "<absent>"
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(replayed)
	evidence.Stage = "release_receipt_replayed_before_clear"
	evidence.ReplayCount = 1
	evidence.ExactReceiptReplay = true
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(replayedFile))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(replayedFile))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_at_replayed_receipt_before_release", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_at_replayed_receipt_before_release", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_at_replayed_receipt_before_release", helperPID, helperIdentity),
	)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	secondCrash := killLinuxProductionDaemon(second)
	if secondCrash.RequestedSignal != "SIGKILL" || !secondCrash.Signaled || secondCrash.Signal != "SIGKILL" {
		t.Fatalf("release replay daemon crash status = %#v, want SIGKILL after exact durable receipt readback and before release", secondCrash)
	}
	evidence.CrashStatuses = append(evidence.CrashStatuses, secondCrash)
	evidence.Stage = "release_receipt_replayed_before_second_daemon_crash"
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	if err := writeLinuxWitnessReleaseMarker(replayReleasePath); err != nil {
		t.Fatalf("release replay stop-receipt barrier after SIGKILL: %v", err)
	}
	secondReplayBarrierReleased = true
	replayReleaseContents, err := os.ReadFile(replayReleasePath)
	if err != nil {
		t.Fatalf("read replay stop-receipt barrier release: %v", err)
	}
	evidence.ReplayReceiptBarrierRelease = string(replayReleaseContents)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

	third = startLinuxReplayDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third", "", "", "", "", nil)
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
	finalSnapshots, finalAbsent := linuxReplayReleasedSnapshot(t, target.StateDir, key)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, finalSnapshots...)
	if finalFile, found, err := readLinuxWitnessJournalForKey(target.StateDir, key); err == nil && found {
		evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(finalFile))
	}
	evidence.ClearObserved = finalAbsent || len(finalSnapshots) > 0
	evidence.DuplicateStopSeen = linuxReplayMarkerExists(duplicateStopMarker)
	evidence.RawOutput = linuxWitnessLogs(first, second)
	for path, contents := range linuxWitnessLogs(third, nil) {
		evidence.RawOutput[path] = contents
	}
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
}

func startLinuxReplayDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, agentMode, label, dropOperation, markerPath, barrierReachedPath, barrierReleasePath string, extraEnvironment map[string]string) *productionDaemonWitnessProcess {
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
	command.Env = linuxReplayDaemonEnvironment(enrollmentToken, agentMode, dropOperation, markerPath, barrierReachedPath, barrierReleasePath)
	for key, value := range extraEnvironment {
		command.Env = setLinuxWitnessEnvironment(command.Env, key, value)
	}
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start Linux replay daemon: %v", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
}

func linuxReplayDaemonEnvironment(enrollmentToken, agentMode, dropOperation, markerPath, barrierReachedPath, barrierReleasePath string) []string {
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
	remove(linuxReplayReleaseBarrierReachedEnv)
	remove(linuxReplayReleaseBarrierReleaseEnv)
	if dropOperation != "" {
		set(linuxReplayProductionWitnessEnv, "1")
		set(linuxReplayDropOperationEnv, dropOperation)
		set(linuxReplayDropMarkerEnv, markerPath)
	}
	if barrierReachedPath != "" && barrierReleasePath != "" {
		set(linuxReplayReleaseBarrierReachedEnv, barrierReachedPath)
		set(linuxReplayReleaseBarrierReleaseEnv, barrierReleasePath)
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

func runLinuxHelperCrashReplayWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxReplayMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "containment-replay")
	const caseName = "helper_dead_mirror_receipt_reconstruction"
	notes := []string{
		"helper response loss and daemon restart are exercised by the dedicated stop_response_lost and release_response_lost cases",
		"helper and mirror clean scan seals are durably observed before helper SIGKILL",
		"daemon SIGSTOP holds the owning process so helper death cannot trigger its Pdeathsig mirror response before same-group-child verification",
		"the first daemon is SIGKILLed at a real durable-receipt barrier; replay is separately held at the exact receipt-readback barrier before its SIGKILL",
		"failure cleanup releases SIGSTOP first, then attempts real cancellation/recovery before its exact pidfd-only fallback",
	}
	scanTriggerPath := filepath.Join(caseRoot, "helper-dead-scan.trigger")
	scanReachedPath := filepath.Join(caseRoot, "helper-dead-scan.reached")
	scanReleasePath := filepath.Join(caseRoot, "helper-dead-scan.release")
	receiptReachedPath := filepath.Join(caseRoot, "helper-dead-stop-receipt.reached")
	receiptReleasePath := filepath.Join(caseRoot, "helper-dead-stop-receipt.release")
	replayReceiptReachedPath := filepath.Join(caseRoot, "helper-dead-replay-receipt.reached")
	replayReceiptReleasePath := filepath.Join(caseRoot, "helper-dead-replay-receipt.release")
	evidence := linuxReplayEvidence{
		Version:                         1,
		Case:                            caseName,
		Stage:                           "initialized",
		Metadata:                        metadata,
		CaseRoot:                        caseRoot,
		ScanTriggerPath:                 scanTriggerPath,
		ScanReachedPath:                 scanReachedPath,
		ScanReleasePath:                 scanReleasePath,
		StopReceiptBarrierReachedPath:   receiptReachedPath,
		StopReceiptBarrierReleasePath:   receiptReleasePath,
		ReplayReceiptBarrierReachedPath: replayReceiptReachedPath,
		ReplayReceiptBarrierReleasePath: replayReceiptReleasePath,
		RawOutput:                       map[string]string{},
		Notes:                           notes,
		ReplayCount:                     0,
	}
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

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
		cleanupComplete                bool
		taskID                         string
		submitIdempotencyKey           string
		submitRequest                  protocol.TaskSubmitRequest
		submissionAttempted            bool
		cancelMutationID               string
		second, third                  *productionDaemonWitnessProcess
	)
	defer func() {
		if leaveDaemonForRecovery {
			t.Logf("leaving Linux replay owner daemons alive because exact physical stop remains unproven; local state: %s", caseRoot)
			return
		}
		for _, process := range []*productionDaemonWitnessProcess{third, second, daemon} {
			if process != nil && !process.waited {
				process.stop(t)
			}
		}
	}()
	defer func() {
		if targetPIDFD >= 0 {
			if err := unix.Close(targetPIDFD); err != nil {
				t.Errorf("close replay witness test-owned target pidfd: %v", err)
			}
		}
		if helperPIDFD >= 0 {
			if err := unix.Close(helperPIDFD); err != nil {
				t.Errorf("close replay witness test-owned helper pidfd: %v", err)
			}
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	submitIdempotencyKey = unique("linux-helper-replay-submit")
	submitRequest = protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal: caseName, AgentProfile: profile, Workspace: workspace, Input: json.RawMessage(`{"mode":"slow"}`),
	}}
	cancelMutationID = unique("linux-helper-replay-cancel")
	defer func() {
		if !submissionAttempted {
			return
		}
		if cleanupComplete {
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
			evidence.StopReceiptBarrierReached = cleanup.StopReceipt
		}
		if cleanup.Receipt != nil {
			evidence.ReceiptBeforeClear = cleanup.Receipt
		}
		if cleanup.ScanRelease != "" {
			evidence.ScanRelease = cleanup.ScanRelease
		}
		if cleanup.ReceiptRelease != "" {
			evidence.StopReceiptBarrierRelease = cleanup.ReceiptRelease
		}
		for _, snapshot := range cleanup.JournalSnapshots {
			redacted, _ := json.Marshal(snapshot)
			evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshot{
				At: snapshot.At, Path: snapshot.Path, SHA256: snapshot.RawSHA256, Snapshot: redacted,
			})
		}
		evidence.RawOutput = linuxWitnessLogs(daemon, nil)
		if cleanup.Err != nil {
			evidence.Notes = append(evidence.Notes, "failure cleanup left explicit unresolved containment evidence: "+cleanup.Err.Error())
		}
		writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
		t.Logf("Linux helper-crash replay cleanup redacted evidence: journals=%#v processes=%#v", cleanup.JournalSnapshots, cleanup.ProcessSnapshots)
		if cleanup.Err != nil {
			t.Errorf("Linux helper-crash replay cleanup could not prove bounded recovery: %v; redacted journal snapshots=%#v process snapshots=%#v", cleanup.Err, cleanup.JournalSnapshots, cleanup.ProcessSnapshots)
		}
	}()
	leaveDaemonForRecovery = true
	submissionAttempted = true
	submitCtx, cancelSubmit := context.WithTimeout(context.Background(), 15*time.Second)
	task, submitErr := submitLinuxWitnessTaskWithReplay(submitCtx, operator, submitIdempotencyKey, submitRequest, 2)
	cancelSubmit()
	if submitErr != nil {
		t.Fatalf("helper-crash replay submission remains unresolved after exact idempotency replay; cleanup will retry only key %q and retain daemon/state if still unknown: %v", submitIdempotencyKey, submitErr)
	}
	taskID = task.TaskID
	evidence.TaskID = taskID
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux production containment authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.LocalState == "running"
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	key = file.Journal.Key()
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(file))
	evidence.RunID = key.RunID
	evidence.Generation = key.Generation
	evidence.JournalKey = fmt.Sprintf("%s/%d", key.RunID, key.Generation)
	targetPIDFD, err = openLinuxWitnessTargetPIDFD(targetPID, targetIdentity)
	if err != nil {
		t.Fatalf("retain replay witness exact target pidfd before helper crash: %v", err)
	}
	helperPIDFD, err = openLinuxWitnessPIDFD(helperPID, helperIdentity)
	if err != nil {
		t.Fatalf("retain replay witness exact helper pidfd before helper crash: %v", err)
	}
	childPID, err = waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("target before replay case: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
		t.Fatalf("descendant before replay case: %v", err)
	}
	{
		sealedFile, helperReached, mirrorReached, sealErr := waitForLinuxWitnessScanSeal(target.StateDir, key, scanTriggerPath, scanReachedPath, scanReleasePath, "both", 45*time.Second)
		if sealErr != nil {
			t.Fatalf("wait for durable helper/mirror scan seals before helper SIGKILL: %v", sealErr)
		}
		if !linuxWitnessJournalHasScanSealBoundary(sealedFile.Journal, "both") {
			t.Fatalf("helper-crash journal lacks both durable scan seals: %#v", linuxWitnessJournalSnapshotFromFile(sealedFile).ContainmentScans)
		}
		anchor := linuxWitnessProcessSnapshotAt("target_at_both_scan_seals", targetPID, targetIdentity)
		child := linuxWitnessProcessSnapshotAt("same_group_descendant_at_both_scan_seals", childPID, "")
		assertLinuxWitnessSameGroup(t, anchor, child)
		if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
			t.Fatalf("target at both durable scan seals: %v", err)
		}
		if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
			t.Fatalf("same-group descendant at both durable scan seals: %v", err)
		}
		evidence.ScanHelperReached = helperReached
		evidence.ScanMirrorReached = mirrorReached
		scanReleaseContents, err := os.ReadFile(scanReleasePath)
		if err != nil {
			t.Fatalf("read helper-crash scan-seal release marker: %v", err)
		}
		evidence.ScanRelease = string(scanReleaseContents)
		evidence.Stage = "running_at_both_durable_scan_seals"
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(sealedFile))
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, anchor, child,
			linuxWitnessProcessSnapshotAt("helper_before_helper_sigkill", helperPID, helperIdentity))
		writeLinuxReplayEvidence(t, target.EvidencePath, evidence)

		daemonPID := daemonProcessPID(daemon)
		daemonIdentity, err := platform.ProcessIdentity(daemonPID)
		if err != nil {
			t.Fatalf("read owning daemon identity before replay SIGSTOP: %v", err)
		}
		if err := daemon.command.Process.Signal(unix.SIGSTOP); err != nil {
			t.Fatalf("SIGSTOP owning daemon before helper SIGKILL: %v", err)
		}
		daemonPaused = true
		threadCount, err := waitForLinuxWitnessAllThreadsStopped(daemonPID, daemonIdentity, 3*time.Second)
		if err != nil {
			t.Fatalf("owning daemon did not reach process-wide stopped state before helper SIGKILL: %v", err)
		}
		pausedDaemon := linuxWitnessProcessSnapshotAt("daemon_all_threads_stopped_before_helper_sigkill", daemonPID, daemonIdentity)
		evidence.Notes = append(evidence.Notes, fmt.Sprintf("verified all %d daemon threads in /proc/%d/task reached T/t before helper SIGKILL", threadCount, daemonPID))
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
			t.Fatalf("exact helper before replay pidfd SIGKILL: %v", err)
		}
		if err := killLinuxWitnessProcessByPIDFD(helperPIDFD); err != nil {
			t.Fatalf("SIGKILL exact Linux supervisor helper pidfd: %v", err)
		}
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 5*time.Second); err != nil {
			t.Fatalf("wait for Linux supervisor helper death: %v", err)
		}
		if err := assertLinuxWitnessProcessLive(childPID, ""); err != nil {
			t.Fatalf("same-group descendant stopped while daemon was paused after helper death: %v", err)
		}
		child = linuxWitnessProcessSnapshotAt("same_group_descendant_after_helper_sigkill_while_daemon_paused", childPID, "")
		assertLinuxWitnessSameGroup(t, anchor, child)
		evidence.Stage = "helper_crashed_same_group_descendant_live_while_daemon_paused"
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, pausedDaemon,
			linuxWitnessProcessSnapshotAt("target_after_helper_sigkill_while_daemon_paused", targetPID, targetIdentity), child,
			linuxWitnessProcessSnapshotAt("helper_after_helper_sigkill_while_daemon_paused", helperPID, helperIdentity))
		writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
		if err := daemon.command.Process.Signal(unix.SIGCONT); err != nil {
			t.Fatalf("SIGCONT owning daemon for mirror takeover: %v", err)
		}
		daemonPaused = false
		if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
			t.Fatalf("wait for Linux supervisor helper death after daemon resume: %v", err)
		}
	}

	cancelLinuxWitnessTask(t, operator, task.TaskID, cancelMutationID)
	firstFile, marker, receipt, receiptErr := waitForLinuxWitnessStopReceiptBarrier(target.StateDir, key, targetPID, targetIdentity, helperPID, helperIdentity, receiptReachedPath, 45*time.Second)
	if receiptErr != nil {
		t.Fatalf("wait for production helper-crash stop receipt before first daemon crash: %v", receiptErr)
	}
	evidence.StopReceiptBarrierReached = &marker
	evidence.ReceiptBeforeClear = linuxReplayReceiptSnapshotFromReceipt(receipt)
	if _, err := os.Stat(receiptReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first-daemon stop-receipt release barrier = %v, want held before SIGKILL", err)
	}
	evidence.StopReceiptBarrierRelease = "<absent>"
	exit := killLinuxProductionDaemon(daemon)
	if exit.RequestedSignal != "SIGKILL" || !exit.Signaled || exit.Signal != "SIGKILL" {
		t.Fatalf("first helper-replay daemon crash status = %#v, want SIGKILL at held receipt barrier", exit)
	}
	first := linuxReplayKillResult{File: firstFile, Exit: exit}
	firstReceipt := requireLinuxReplayReceipt(t, first.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, first.File.Journal, firstReceipt)
	if !linuxWitnessJournalHasScanSealBoundary(first.File.Journal, "both") {
		t.Fatalf("helper-dead mirror stop receipt lacks its durable helper and mirror seals: %#v", linuxWitnessJournalSnapshotFromFile(first.File).ContainmentScans)
	}
	evidence.Stage = "receipt_durable_before_first_daemon_crash"
	evidence.CrashStatuses = append(evidence.CrashStatuses, first.Exit)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(first.File))
	evidence.RawJournalSnapshots = append(evidence.RawJournalSnapshots, linuxReplayRawJournalSnapshotFromFile(first.File))
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
	if secondBefore.Journal.ContainmentAuthority == nil || secondBefore.Journal.ContainmentAuthority.StopReceipt == nil || *secondBefore.Journal.ContainmentAuthority.StopReceipt != firstReceipt {
		t.Fatalf("journal before receipt replay does not retain the exact first receipt: %#v", linuxWitnessJournalSnapshotFromFile(secondBefore))
	}
	secondEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
		linuxStopReceiptBarrierReachedEnvironment:    replayReceiptReachedPath,
		linuxStopReceiptBarrierReleaseEnvironment:    replayReceiptReleasePath,
	}
	second = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "second", secondEnvironment)
	replayFile, replayMarker, replayReceipt, replayErr := waitForLinuxWitnessStopReceiptBarrier(target.StateDir, key, targetPID, targetIdentity, helperPID, helperIdentity, replayReceiptReachedPath, 45*time.Second)
	if replayErr != nil {
		t.Fatalf("second daemon did not expose exact replayed stop receipt before SIGKILL: %v", replayErr)
	}
	if replayReceipt != firstReceipt {
		t.Fatalf("second daemon replay receipt changed before crash: first=%#v second=%#v", linuxReplayReceiptSnapshotFromReceipt(firstReceipt), linuxReplayReceiptSnapshotFromReceipt(replayReceipt))
	}
	evidence.ReplayReceiptBarrierReached = &replayMarker
	evidence.ReplayReceiptBarrierRelease = "<absent>"
	if _, err := os.Stat(replayReceiptReleasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second-daemon replay receipt release barrier = %v, want held before SIGKILL", err)
	}
	secondExit := killLinuxProductionDaemon(second)
	if secondExit.RequestedSignal != "SIGKILL" || !secondExit.Signaled || secondExit.Signal != "SIGKILL" {
		t.Fatalf("second helper-replay daemon crash status = %#v, want SIGKILL at held replay receipt barrier", secondExit)
	}
	secondResult := linuxReplayKillResult{File: replayFile, Exit: secondExit}
	secondReceipt := requireLinuxReplayReceipt(t, secondResult.File.Journal)
	assertLinuxReplayReceiptBeforeClear(t, secondResult.File.Journal, secondReceipt)
	if firstReceipt != secondReceipt {
		t.Fatalf("replayed stop receipt changed: first=%#v second=%#v", linuxReplayReceiptSnapshotFromReceipt(firstReceipt), linuxReplayReceiptSnapshotFromReceipt(secondReceipt))
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

	third = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "third")
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, key, 45*time.Second); err != nil {
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
	finalSnapshots, finalAbsent := linuxReplayReleasedSnapshot(t, target.StateDir, key)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, finalSnapshots...)
	evidence.ClearObserved = finalAbsent || len(finalSnapshots) > 0
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_replay", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("descendant_after_replay", childPID, ""),
		linuxWitnessProcessSnapshotAt("helper_after_replay", helperPID, helperIdentity),
	)
	evidence.RawOutput = linuxWitnessLogs(daemon, second)
	leaveDaemonForRecovery = false
	third.stop(t)
	writeLinuxReplayEvidence(t, target.EvidencePath, evidence)
	t.Logf("Linux containment replay evidence: %s", target.EvidencePath)
	cleanupComplete = true
}

func requireLinuxReplayReceipt(t *testing.T, journal state.RunJournal) authority.StopReceipt {
	t.Helper()
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil {
		t.Fatalf("journal has no durable containment receipt: %#v", linuxWitnessJournalSnapshotFromFile(linuxWitnessJournalFile{Journal: journal}))
	}
	receipt := *journal.ContainmentAuthority.StopReceipt
	if !receipt.ValidFor(*journal.ContainmentAuthority) {
		t.Fatalf("journal stop receipt is not bound to authority: %#v", linuxReplayReceiptSnapshotFromReceipt(receipt))
	}
	return receipt
}

func linuxReplayReceiptSnapshotFromReceipt(receipt authority.StopReceipt) *linuxReplayReceiptSnapshot {
	return &linuxReplayReceiptSnapshot{
		Version:            receipt.Version,
		LegacyV1NoV2Proof:  receipt.Version == authority.SupervisorVersion && receipt.LinuxProof == nil,
		Status:             receipt.Status,
		OwnerKind:          receipt.OwnerKind,
		OwnerContextSHA256: linuxWitnessHashBytes([]byte(receipt.OwnerContext)),
		TargetPID:          receipt.TargetPID,
		TargetIdentity:     receipt.TargetIdentity,
		PipeTokenSHA256:    linuxWitnessHashBytes([]byte(receipt.PipeToken)),
		JobIDSHA256:        linuxWitnessHashBytes([]byte(receipt.JobID)),
		SupervisorPID:      receipt.SupervisorPID,
		SupervisorIdentity: receipt.SupervisorIdentity,
		ActiveProcesses:    receipt.ActiveProcesses,
	}
}

func assertLinuxReplayReceiptBeforeClear(t *testing.T, journal state.RunJournal, receipt authority.StopReceipt) {
	t.Helper()
	assertLinuxReplayV1NoV2Receipt(t, journal, &receipt)
	if !journal.HasProcessDetails() || journal.ContainmentAuthority == nil {
		t.Fatalf("receipt was observed after clear: %#v", linuxWitnessJournalSnapshotFromFile(linuxWitnessJournalFile{Journal: journal}))
	}
	if journal.ContainmentAuthority.StopReceipt == nil || *journal.ContainmentAuthority.StopReceipt != receipt {
		t.Fatalf("journal receipt changed before clear: %#v", linuxWitnessJournalSnapshotFromFile(linuxWitnessJournalFile{Journal: journal}))
	}
}

func assertLinuxReplayV1NoV2Receipt(t *testing.T, journal state.RunJournal, receipt *authority.StopReceipt) {
	t.Helper()
	if receipt != nil && (receipt.Version != authority.SupervisorVersion || receipt.LinuxProof != nil) {
		t.Fatalf("legacy Linux receipt was upgraded to v2 or changed version: %#v", linuxReplayReceiptSnapshotFromReceipt(*receipt))
	}
	if journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
		t.Fatalf("legacy Linux journal contains a forged cgroup-v2 reservation or release receipt: %#v", linuxWitnessJournalSnapshotFromFile(linuxWitnessJournalFile{Journal: journal}))
	}
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version != authority.SupervisorVersion {
		t.Fatalf("legacy Linux replay changed its authority version: %#v", journal.ContainmentAuthority)
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version != authority.SupervisorHandoffVersion {
		t.Fatalf("legacy Linux replay changed its handoff version: %#v", journal.ContainmentHandoff)
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
	assertLinuxReplayV1NoV2Receipt(t, file.Journal, nil)
	if file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentAuthority != nil || file.Journal.HasProcessDetails() {
		t.Fatalf("final Linux replay journal retained containment: %#v", linuxWitnessJournalSnapshotFromFile(file))
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
