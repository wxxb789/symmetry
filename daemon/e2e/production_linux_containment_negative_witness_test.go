//go:build linux

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const linuxNegativeWitnessEnvironment = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_WITNESS"

const (
	linuxScanFailureModeEnv    = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE"
	linuxScanFailureTriggerEnv = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH"
	linuxScanFailureFiredEnv   = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH"
)

type linuxNegativeWitnessMetadata struct {
	linuxWitnessMetadata
}

type linuxNegativeWitnessEvidence struct {
	Version          int                           `json:"version"`
	Case             string                        `json:"case"`
	Status           string                        `json:"status"`
	Metadata         linuxNegativeWitnessMetadata  `json:"metadata"`
	CaseRoot         string                        `json:"case_root"`
	Crash            *linuxWitnessExitStatus       `json:"crash,omitempty"`
	Error            string                        `json:"error,omitempty"`
	JournalSnapshots []linuxWitnessJournalSnapshot `json:"journal_snapshots"`
	ProcessSnapshots []linuxWitnessProcessSnapshot `json:"process_snapshots"`
	Assertions       map[string]bool               `json:"assertions"`
	RawOutput        map[string]string             `json:"raw_output,omitempty"`
	Notes            []string                      `json:"notes,omitempty"`
}

// TestProductionLinuxContainmentNegativeWitness is an opt-in witness for
// rejection and fail-closed evidence on the real daemon and hidden helper
// binary. When enabled, none of its cases are skipped.
func TestProductionLinuxContainmentNegativeWitness(t *testing.T) {
	if os.Getenv(linuxNegativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxNegativeWitnessEnvironment + "=1 to run the Linux negative containment witness")
	}

	environment := loadControlEnvironment(t)
	runRoot := t.TempDir()
	buildRoot := linuxWitnessCaseRoot(runRoot, "artifacts")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		t.Fatalf("create Linux negative witness artifact root: %v", err)
	}
	daemonBinary := buildProductionDaemonWitness(t, buildRoot)
	metadata := newLinuxNegativeWitnessMetadata(t, daemonBinary)
	writeLinuxWitnessProvenance(t, runRoot, "negative", metadata)

	t.Run("pid_mismatch_rejected", func(t *testing.T) {
		runLinuxNegativeAuthorityMismatchWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "pid-mismatch"), "pid_mismatch_rejected", func(value *authority.Supervisor) {
			value.TargetPID++
		})
	})
	t.Run("identity_anchor_session_mismatch_rejected", func(t *testing.T) {
		runLinuxNegativeAuthorityMismatchWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "identity-mismatch"), "identity_anchor_session_mismatch_rejected", func(value *authority.Supervisor) {
			parts := strings.Split(value.TargetIdentity, ":")
			if len(parts) != 6 || parts[0] != "linux" || parts[1] != "v2" {
				t.Fatalf("target identity = %q, want strict Linux v2 identity", value.TargetIdentity)
			}
			startTime, err := strconv.ParseUint(parts[5], 10, 64)
			if err != nil || startTime == ^uint64(0) {
				t.Fatalf("parse target start time from %q: %v", value.TargetIdentity, err)
			}
			parts[5] = strconv.FormatUint(startTime+1, 10)
			value.TargetIdentity = strings.Join(parts, ":")
		})
	})
	t.Run("observed_setsid_escape", func(t *testing.T) {
		runLinuxObservedSetsidEscapeWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "setsid-escape"))
	})
	t.Run("injected_descendant_scan_failure", func(t *testing.T) {
		runLinuxInjectedScanFailureWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "scan-failure"))
	})
}

func newLinuxNegativeWitnessMetadata(t *testing.T, binaryPath string) linuxNegativeWitnessMetadata {
	t.Helper()
	metadata := newLinuxWitnessMetadata(t, binaryPath)
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Linux negative witness source")
	}
	sourceSHA256, hashErr := linuxWitnessSHA256(sourcePath)
	if hashErr != nil {
		t.Fatalf("hash Linux negative witness source: %v", hashErr)
	}
	metadata.SourcePath = sourcePath
	metadata.SourceSHA256 = sourceSHA256
	return linuxNegativeWitnessMetadata{linuxWitnessMetadata: metadata}
}

func runLinuxNegativeAuthorityMismatchWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot, name string, mutate func(*authority.Supervisor)) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, name)
	evidence := linuxNegativeWitnessEvidence{
		Version:    1,
		Case:       name,
		Status:     "initialized",
		Metadata:   metadata,
		CaseRoot:   caseRoot,
		Assertions: map[string]bool{},
	}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	var daemon *productionDaemonWitnessProcess
	daemon = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
	}()
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, name, "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "Linux negative authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.LocalState == "running" && value.Journal.ContainmentAuthority != nil
	})
	if err != nil {
		t.Fatal(err)
	}
	value := file.Journal.ContainmentAuthority.Clone()
	mutate(&value)
	_, stopErr := platform.StopPersistedLinuxSupervisor(value)
	if stopErr == nil {
		t.Fatal("mutated Linux supervisor authority unexpectedly produced a stop receipt")
	}
	after, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil {
		t.Fatalf("read journal after rejected authority: %v", err)
	}
	if !found {
		t.Fatal("journal disappeared after rejected authority")
	}
	if after.Journal.ContainmentAuthority == nil || after.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("rejected authority changed stop receipt state: %#v", after.Journal.ContainmentAuthority)
	}
	if after.Journal.ContainmentHandoff == nil && !after.Journal.HasProcessDetails() {
		t.Fatal("rejected authority cleared the durable process marker")
	}
	evidence.Status = "rejected"
	evidence.Error = stopErr.Error()
	evidence.JournalSnapshots = append(evidence.JournalSnapshots,
		linuxWitnessJournalSnapshotFromFile(file),
		linuxWitnessJournalSnapshotFromFile(after))
	evidence.Assertions["stop_error"] = true
	evidence.Assertions["no_stop_receipt"] = after.Journal.ContainmentAuthority != nil && after.Journal.ContainmentAuthority.StopReceipt == nil
	evidence.Assertions["no_clear"] = after.Journal.ContainmentHandoff != nil || after.Journal.HasProcessDetails()
	evidence.Assertions["unresolved_marker"] = after.Journal.ContainmentUnproven || after.Journal.ContainmentHandoff != nil || after.Journal.ContainmentAuthority != nil || after.Journal.HasProcessDetails()
	evidence.Assertions["same_run"] = after.Journal.Key() == file.Journal.Key()
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	cancelTask(t, operator, task.TaskID)
	waitForTask(t, operator, task.TaskID, 45*time.Second, func(value protocol.Task) bool { return value.State == "cancelled" })
	if err := waitForLinuxWitnessJournalReleased(target.StateDir, file.Journal.Key(), 45*time.Second); err != nil {
		t.Fatalf("cleanup after authority rejection: %v", err)
	}
}

func runLinuxObservedSetsidEscapeWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot string) {
	t.Helper()
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "setsid-escape")
	if err := os.WriteFile(filepath.Join(caseRoot, "linux-containment-target.sh"), []byte(linuxNegativeSetsidTargetScript(target.TargetMarker, target.DescendantMarker)), 0o700); err != nil {
		t.Fatalf("write setsid escape target: %v", err)
	}
	evidence := linuxNegativeWitnessEvidence{Version: 1, Case: "observed_setsid_escape", Status: "initialized", Metadata: metadata, CaseRoot: caseRoot, Assertions: map[string]bool{}}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	var daemon *productionDaemonWitnessProcess
	var targetPID, childPID, helperPID int
	var targetIdentity, helperIdentity string
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
		cleanupLinuxNegativeWitnessProcess(t, targetPID, targetIdentity, childPID, helperPID, helperIdentity)
	}()
	daemon = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-observed-setsid-escape", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "setsid escape authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.LocalState == "running" && value.Journal.ContainmentAuthority != nil
	})
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	childPID, err = waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	targetSnapshot := linuxWitnessProcessSnapshotAt("target_before_setsid_stop", targetPID, targetIdentity)
	escapedSnapshot := linuxWitnessProcessSnapshotAt("setsid_descendant_before_stop", childPID, "")
	if !targetSnapshot.Exists || !escapedSnapshot.Exists || escapedSnapshot.PGRP == targetSnapshot.PGRP || escapedSnapshot.Session == targetSnapshot.Session {
		daemon.stop(t)
		t.Fatalf("setsid descendant was not observed outside the original group: target=%#v descendant=%#v", targetSnapshot, escapedSnapshot)
	}
	file, err = waitForLinuxWitnessJournal(target.StateDir, "durable setsid escape marker", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentUnproven && value.Journal.HasProcessDetails() && value.Journal.ContainmentAuthority != nil
	})
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	crash := killLinuxProductionDaemon(daemon)
	after, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("setsid escape journal disappeared after daemon crash")
	}
	if after.Journal.ContainmentAuthority == nil || after.Journal.ContainmentAuthority.StopReceipt != nil || !after.Journal.ContainmentUnproven || !after.Journal.HasProcessDetails() {
		t.Fatalf("setsid escape was not retained fail-closed: %#v", after.Journal)
	}
	evidence.Status = "unresolved"
	evidence.Crash = &crash
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(after))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, targetSnapshot, escapedSnapshot, linuxWitnessProcessSnapshotAt("helper_after_daemon_crash", helperPID, helperIdentity))
	evidence.Assertions["observed_escape"] = true
	evidence.Assertions["no_stop_receipt"] = after.Journal.ContainmentAuthority != nil && after.Journal.ContainmentAuthority.StopReceipt == nil
	evidence.Assertions["no_clear"] = after.Journal.HasProcessDetails() || after.Journal.ContainmentAuthority != nil
	evidence.Assertions["containment_unproven"] = after.Journal.ContainmentUnproven
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	evidence.Notes = []string{"The child intentionally called setsid; the original pidfd group is the only verified authority."}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	_ = task
}

func runLinuxInjectedScanFailureWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot string) {
	t.Helper()
	triggerPath := filepath.Join(caseRoot, "scan-failure.trigger")
	firedPath := filepath.Join(caseRoot, "scan-failure.fired")
	t.Setenv("SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS", "1")
	t.Setenv(linuxScanFailureModeEnv, "children_after_initial")
	t.Setenv(linuxScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxScanFailureFiredEnv, firedPath)
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "scan-failure")
	if err := os.WriteFile(filepath.Join(caseRoot, "linux-containment-target.sh"), []byte(linuxNegativeScanFailureTargetScript(target.TargetMarker, target.DescendantMarker)), 0o700); err != nil {
		t.Fatalf("write scan-failure target: %v", err)
	}
	evidence := linuxNegativeWitnessEvidence{Version: 1, Case: "injected_descendant_scan_failure", Status: "initialized", Metadata: metadata, CaseRoot: caseRoot, Assertions: map[string]bool{}}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatalf("write scan-failure trigger: %v", err)
	}

	var daemon *productionDaemonWitnessProcess
	var targetPID, churnPID, helperPID int
	var targetIdentity, helperIdentity string
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
		cleanupLinuxNegativeWitnessProcess(t, targetPID, targetIdentity, churnPID, helperPID, helperIdentity)
	}()
	daemon = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	operator := newOperator(t, environment)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-injected-scan-failure", "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "scan-failure authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.LocalState == "running" && value.Journal.ContainmentAuthority != nil
	})
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(file.Journal)
	churnPID, err = waitForLinuxWitnessPIDMarker(target.DescendantMarker, 20*time.Second)
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	if err := waitForLinuxWitnessMarker(firedPath, 20*time.Second); err != nil {
		daemon.stop(t)
		t.Fatalf("wait for deterministic scan-failure marker: %v", err)
	}
	cancelTask(t, operator, task.TaskID)
	file, err = waitForLinuxWitnessJournal(target.StateDir, "injected descendant scan failure marker", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentUnproven && value.Journal.HasProcessDetails() && value.Journal.ContainmentAuthority != nil
	})
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	beforeCrash := linuxWitnessProcessSnapshotAt("scan_churner_before_daemon_crash", churnPID, "")
	crash := killLinuxProductionDaemon(daemon)
	after, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("scan-failure journal disappeared after daemon crash")
	}
	if after.Journal.ContainmentAuthority == nil || after.Journal.ContainmentAuthority.StopReceipt != nil || !after.Journal.ContainmentUnproven || !after.Journal.HasProcessDetails() {
		t.Fatalf("scan-failure state was not retained fail-closed: %#v", after.Journal)
	}
	evidence.Status = "unresolved"
	evidence.Crash = &crash
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(after))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, beforeCrash, linuxWitnessProcessSnapshotAt("target_after_daemon_crash", targetPID, targetIdentity), linuxWitnessProcessSnapshotAt("helper_after_daemon_crash", helperPID, helperIdentity))
	evidence.Assertions["scan_failure_injected"] = true
	evidence.Assertions["scan_failure_fired"] = true
	evidence.Assertions["no_stop_receipt"] = after.Journal.ContainmentAuthority != nil && after.Journal.ContainmentAuthority.StopReceipt == nil
	evidence.Assertions["no_clear"] = after.Journal.HasProcessDetails() || after.Journal.ContainmentAuthority != nil
	evidence.Assertions["containment_unproven"] = after.Journal.ContainmentUnproven
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	evidence.Notes = []string{"The production helper received one exact-target descendant reader failure after its initial scan; the fired marker and unresolved journal prove the failure was not converted into a stop receipt."}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	_ = task
	_ = operator
}

func linuxNegativeSetsidTargetScript(targetMarker, descendantMarker string) string {
	return "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' \"$$\" > " + linuxWitnessShellQuote(targetMarker) + "\n" +
		"setsid sh -c 'trap \"\" TERM; while :; do sleep 1; done' &\n" +
		"child=\"$!\"\n" +
		"printf '%s\\n' \"$child\" > " + linuxWitnessShellQuote(descendantMarker) + "\n" +
		"printf '%s\\n' '{\"type\":\"progress\",\"message\":\"setsid escape witness started\"}'\n" +
		"while :; do sleep 1; done\n"
}

func linuxNegativeScanFailureTargetScript(targetMarker, descendantMarker string) string {
	return "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' \"$$\" > " + linuxWitnessShellQuote(targetMarker) + "\n" +
		"(while :; do for i in 1 2 3 4 5 6 7 8; do (true) & done; wait; done) &\n" +
		"churner=\"$!\"\n" +
		"printf '%s\\n' \"$churner\" > " + linuxWitnessShellQuote(descendantMarker) + "\n" +
		"printf '%s\\n' '{\"type\":\"progress\",\"message\":\"descendant scan churn witness started\"}'\n" +
		"while :; do sleep 1; done\n"
}

func cleanupLinuxNegativeWitnessProcess(t *testing.T, targetPID int, targetIdentity string, childPID, helperPID int, helperIdentity string) {
	t.Helper()
	if childPID > 0 {
		if err := assertLinuxWitnessProcessLive(childPID, ""); err == nil {
			_ = killLinuxWitnessProcess(t, childPID, "")
		}
		_ = waitForLinuxWitnessIdentityGone(childPID, "", 5*time.Second)
	}
	if targetPID > 0 {
		_ = waitForLinuxWitnessIdentityGone(targetPID, targetIdentity, 5*time.Second)
	}
	if helperPID > 0 {
		if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err == nil {
			_ = killLinuxWitnessProcess(t, helperPID, helperIdentity)
		}
		_ = waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 5*time.Second)
	}
}

func writeLinuxNegativeWitnessEvidence(t *testing.T, path string, evidence linuxNegativeWitnessEvidence) {
	t.Helper()
	contents, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode Linux negative witness evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create Linux negative witness evidence directory: %v", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".linux-negative-witness-")
	if err != nil {
		t.Fatalf("create Linux negative witness evidence temp file: %v", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		t.Fatalf("write Linux negative witness evidence: %v", err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("close Linux negative witness evidence: %v", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		t.Fatalf("publish Linux negative witness evidence: %v", err)
	}
}
