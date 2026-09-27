//go:build windows

package e2e_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/windows"
)

const preAuthorityWitnessCommand = "-preauthority-witness"

const preAuthoritySupervisorJobPrefix = `Local\symmetry-containment-`

var preAuthorityOpenJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

type preAuthorityCrashCase struct {
	name             string
	crashAt          string
	wantHandoff      bool
	wantBoundHandoff bool
	wantAuthority    bool
	wantReceipt      bool
	wantCleared      bool
	wantReady        bool
}

type preAuthorityWitnessCrashReport struct {
	Stage              string `json:"stage"`
	Status             string `json:"status"`
	JobID              string `json:"job_id"`
	HasStopReceipt     bool   `json:"has_stop_receipt"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
}

type preAuthoritySentinelReadyRecord struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type preAuthorityWrongSecretWitness struct {
	Status             string `json:"status"`
	ActiveProcesses    uint32 `json:"active_processes"`
	TargetPID          int    `json:"target_pid"`
	TargetIdentity     string `json:"target_identity"`
	SupervisorPID      int    `json:"supervisor_pid"`
	SupervisorIdentity string `json:"supervisor_identity"`
	JobID              string `json:"job_id"`
}

type preAuthorityHelloRetryWitness struct {
	Status string `json:"status"`
}

type preAuthorityProcessWitness struct {
	role     string
	pid      int
	identity string
}

// TestProductionPreAuthorityCrashWitnessMatrix is an opt-in Windows witness
// for the non-test daemon binary. Each first process exits with code 137 at a
// named externally visible boundary. A second production process then loads
// only the durable journal and proves the exact recovery path. No authority or
// secret crosses the test process boundary.
func TestProductionPreAuthorityCrashWitnessMatrix(t *testing.T) {
	if os.Getenv("SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS") != "1" {
		t.Skip("set SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS=1 to run the production pre-authority witness")
	}

	runRoot := t.TempDir()
	daemonBinary := buildProductionDaemonWitness(t, runRoot)
	sentinelBinary := buildPreAuthoritySentinel(t, runRoot)
	cases := []preAuthorityCrashCase{
		{name: "prepare", crashAt: "prepare", wantHandoff: true},
		{name: "bind", crashAt: "bind", wantHandoff: true, wantBoundHandoff: true},
		{name: "commit", crashAt: "commit", wantAuthority: true},
		{name: "lease", crashAt: "lease", wantAuthority: true},
		{name: "resume", crashAt: "resume", wantAuthority: true, wantReady: true},
		{name: "stop", crashAt: "stop", wantAuthority: true, wantReady: true},
		{name: "receipt", crashAt: "receipt", wantAuthority: true, wantReceipt: true, wantReady: true},
		{name: "release", crashAt: "release", wantAuthority: true, wantReceipt: true, wantReady: true},
		{name: "clear", crashAt: "clear", wantCleared: true, wantReady: true},
	}

	for _, witnessCase := range cases {
		witnessCase := witnessCase
		t.Run(witnessCase.name, func(t *testing.T) {
			caseRoot := filepath.Join(runRoot, witnessCase.name)
			if err := os.MkdirAll(caseRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			stateDir := filepath.Join(caseRoot, "state")
			readyFile := filepath.Join(caseRoot, "sentinel.ready.json")
			resultFile := filepath.Join(caseRoot, "witness.result.json")
			runID := "preauthority-" + witnessCase.name
			const generation int64 = 1

			runPreAuthorityWitnessBinary(t, daemonBinary, caseRoot, preAuthorityWitnessInvocation{
				Mode:       "start",
				StateDir:   stateDir,
				RunID:      runID,
				Generation: generation,
				Sentinel:   sentinelBinary,
				ReadyFile:  readyFile,
				ResultFile: resultFile,
				CrashAt:    witnessCase.crashAt,
			}, true)

			key := state.RunKey{RunID: runID, Generation: generation}
			journal := loadPreAuthorityWitnessJournal(t, stateDir, key)
			crashReport := assertPreAuthorityCrashReport(t, resultFile, witnessCase)
			assertPreAuthorityCrashState(t, witnessCase, journal)
			if witnessCase.wantReady {
				waitForPreAuthorityWitnessPath(t, readyFile, 5*time.Second)
			} else if _, err := os.Stat(readyFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("sentinel ready file error = %v, want absent before resume", err)
			}
			processes := preAuthorityProcessWitnesses(t, journal, readyFile, crashReport)
			jobID, hasJobID := preAuthoritySupervisorJobID(journal, crashReport)

			runPreAuthorityWitnessBinary(t, daemonBinary, caseRoot, preAuthorityWitnessInvocation{
				Mode:       "recover",
				StateDir:   stateDir,
				RunID:      runID,
				Generation: generation,
				ResultFile: filepath.Join(caseRoot, "recovery.result.json"),
			}, false)

			recovered := loadPreAuthorityWitnessJournal(t, stateDir, key)
			if recovered.HasProcessDetails() || recovered.ContainmentAuthority != nil || recovered.ContainmentHandoff != nil {
				t.Fatalf("recovered journal retained containment: pid=%d identity=%q authority=%t handoff=%t", recovered.PID, recovered.ProcessIdentity, recovered.ContainmentAuthority != nil, recovered.ContainmentHandoff != nil)
			}
			if recovered.LocalState != journal.LocalState {
				t.Fatalf("recovery changed local state from %q to %q", journal.LocalState, recovered.LocalState)
			}
			if _, err := os.Stat(filepath.Join(caseRoot, "recovery.result.json")); err != nil {
				t.Fatalf("recovery result file: %v", err)
			}
			if hasJobID {
				assertPreAuthoritySupervisorJobReleased(t, jobID)
			} else {
				t.Logf("exact supervisor Job release is not directly observable for %s: neither the durable journal nor the production witness report carries JobID; retaining helper process liveness evidence", witnessCase.name)
			}
			for _, process := range processes {
				assertPreAuthorityProcessStopped(t, process)
			}
		})
	}
}

func preAuthoritySupervisorJobID(journal state.RunJournal, report preAuthorityWitnessCrashReport) (string, bool) {
	if journal.ContainmentAuthority != nil {
		return journal.ContainmentAuthority.JobID, true
	}
	if journal.ContainmentHandoff != nil {
		return journal.ContainmentHandoff.JobID, true
	}
	if report.JobID != "" {
		return report.JobID, true
	}
	return "", false
}

func assertPreAuthoritySupervisorJobReleased(t *testing.T, jobID string) {
	t.Helper()
	if len(jobID) != authority.TokenBytes*2 {
		t.Fatalf("supervisor Job ID length = %d, want %d", len(jobID), authority.TokenBytes*2)
	}
	if _, err := hex.DecodeString(jobID); err != nil {
		t.Fatalf("supervisor Job ID %q is not hexadecimal: %v", jobID, err)
	}
	name := preAuthoritySupervisorJobPrefix + jobID
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatalf("encode exact supervisor Job name %q: %v", name, err)
	}
	const desiredAccess = uint32(0x0004 | 0x0008 | windows.SYNCHRONIZE)
	handle, _, callErr := preAuthorityOpenJobObject.Call(uintptr(desiredAccess), 0, uintptr(unsafe.Pointer(namePointer)))
	if handle != 0 {
		_ = windows.CloseHandle(windows.Handle(handle))
		t.Fatalf("exact supervisor Job %q remained open after recovery", name)
	}
	if !errors.Is(callErr, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(callErr, windows.ERROR_PATH_NOT_FOUND) {
		t.Fatalf("open exact supervisor Job %q after recovery: %v", name, callErr)
	}
}

func TestProductionPreAuthorityWrongSecretFirstClientRace(t *testing.T) {
	if os.Getenv("SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS") != "1" {
		t.Skip("set SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS=1 to run the production wrong-secret first-client witness")
	}

	runRoot := t.TempDir()
	daemonBinary := buildProductionDaemonWitness(t, runRoot)
	sentinelBinary := buildPreAuthoritySentinel(t, runRoot)
	caseRoot := filepath.Join(runRoot, "wrong-secret-first-client")
	if err := os.MkdirAll(caseRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(caseRoot, "state")
	readyFile := filepath.Join(caseRoot, "sentinel.ready.json")
	resultFile := filepath.Join(caseRoot, "witness.result.json")
	markerFile := filepath.Join(caseRoot, "wrong-secret.marker.json")
	runID := "preauthority-wrong-secret-first-client"
	const generation int64 = 1

	runPreAuthorityWitnessBinary(t, daemonBinary, caseRoot, preAuthorityWitnessInvocation{
		Mode:              "start",
		StateDir:          stateDir,
		RunID:             runID,
		Generation:        generation,
		Sentinel:          sentinelBinary,
		ReadyFile:         readyFile,
		ResultFile:        resultFile,
		WrongSecretMarker: markerFile,
	}, false)

	contents, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("read wrong-secret witness marker: %v", err)
	}
	var marker preAuthorityWrongSecretWitness
	if err := json.Unmarshal(contents, &marker); err != nil {
		t.Fatalf("decode wrong-secret witness marker: %v", err)
	}
	if marker.Status != "rejected" {
		t.Fatalf("wrong-secret witness status = %q, want rejected", marker.Status)
	}
	if marker.ActiveProcesses == 0 || marker.TargetPID <= 0 || marker.SupervisorPID <= 0 || marker.JobID == "" {
		t.Fatalf("wrong-secret witness evidence is incomplete: %#v", marker)
	}
	if strings.TrimSpace(marker.TargetIdentity) == "" || strings.TrimSpace(marker.SupervisorIdentity) == "" {
		t.Fatalf("wrong-secret witness identities are incomplete: %#v", marker)
	}
	retryContents, err := os.ReadFile(markerFile + ".hello-retry.json")
	if err != nil {
		t.Fatalf("read hello retry witness marker: %v", err)
	}
	var retryMarker preAuthorityHelloRetryWitness
	if err := json.Unmarshal(retryContents, &retryMarker); err != nil {
		t.Fatalf("decode hello retry witness marker: %v", err)
	}
	if retryMarker.Status != "dropped" {
		t.Fatalf("hello retry witness status = %q, want dropped", retryMarker.Status)
	}
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatalf("legitimate hello/resume did not complete: %v", err)
	}

	key := state.RunKey{RunID: runID, Generation: generation}
	journal := loadPreAuthorityWitnessJournal(t, stateDir, key)
	if journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil {
		t.Fatalf("completed wrong-secret witness retained containment: %#v", journal)
	}
	assertPreAuthoritySupervisorJobReleased(t, marker.JobID)
}

func assertPreAuthorityCrashState(t *testing.T, witnessCase preAuthorityCrashCase, journal state.RunJournal) {
	t.Helper()
	if (journal.ContainmentHandoff != nil) != witnessCase.wantHandoff {
		t.Fatalf("handoff present = %t, want %t; journal pid=%d authority=%t", journal.ContainmentHandoff != nil, witnessCase.wantHandoff, journal.PID, journal.ContainmentAuthority != nil)
	}
	if witnessCase.wantBoundHandoff {
		if journal.ContainmentHandoff == nil || journal.ContainmentHandoff.SupervisorPID <= 0 || journal.ContainmentHandoff.SupervisorIdentity == "" {
			t.Fatalf("bound handoff = %#v, want helper identity", journal.ContainmentHandoff)
		}
	}
	if (journal.ContainmentAuthority != nil) != witnessCase.wantAuthority {
		t.Fatalf("authority present = %t, want %t", journal.ContainmentAuthority != nil, witnessCase.wantAuthority)
	}
	if journal.ContainmentAuthority != nil {
		if journal.PID <= 0 || journal.ProcessIdentity == "" || journal.StartedAt.IsZero() {
			t.Fatalf("committed process marker is incomplete: pid=%d identity=%q started=%s", journal.PID, journal.ProcessIdentity, journal.StartedAt)
		}
		if (journal.ContainmentAuthority.StopReceipt != nil) != witnessCase.wantReceipt {
			t.Fatalf("stop receipt present = %t, want %t", journal.ContainmentAuthority.StopReceipt != nil, witnessCase.wantReceipt)
		}
	}
	if witnessCase.wantCleared && (journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil) {
		t.Fatalf("cleared crash state retained ownership: %#v", journal)
	}
}

func assertPreAuthorityCrashReport(t *testing.T, path string, witnessCase preAuthorityCrashCase) preAuthorityWitnessCrashReport {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read crash report: %v", err)
	}
	var report preAuthorityWitnessCrashReport
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("decode crash report: %v", err)
	}
	if report.Stage != witnessCase.crashAt || report.Status != "hard-crash" {
		t.Fatalf("crash report = %#v, want stage %q and hard-crash status", report, witnessCase.crashAt)
	}
	// The clear crash report intentionally uses the post-clear journal flags;
	// its process binding is carried separately from the pre-clear journal.
	if !witnessCase.wantCleared && report.HasStopReceipt != witnessCase.wantReceipt {
		t.Fatalf("crash report stop receipt = %t, want %t", report.HasStopReceipt, witnessCase.wantReceipt)
	}
	return report
}

func preAuthorityProcessWitnesses(t *testing.T, journal state.RunJournal, readyPath string, report preAuthorityWitnessCrashReport) []preAuthorityProcessWitness {
	t.Helper()
	targetPID, targetIdentity := preAuthorityTargetBinding(t, journal, report)
	if targetPID <= 0 || strings.TrimSpace(targetIdentity) == "" {
		t.Fatalf("pre-authority target binding is incomplete: pid=%d identity=%q journal=%#v", targetPID, targetIdentity, journal)
	}

	ready, readyPresent := loadPreAuthoritySentinelReadyRecord(t, readyPath)
	if readyPresent {
		if ready.PID != targetPID {
			t.Fatalf("sentinel PID = %d, target PID = %d", ready.PID, targetPID)
		}
	}

	witnesses := []preAuthorityProcessWitness{
		{role: "target", pid: targetPID, identity: targetIdentity},
		{role: "sentinel", pid: targetPID, identity: targetIdentity},
	}
	helperPID, helperIdentity := preAuthorityHelperBinding(journal, report)
	if (helperPID == 0) != (strings.TrimSpace(helperIdentity) == "") {
		t.Fatalf("pre-authority helper binding is partial: pid=%d identity=%q journal=%#v", helperPID, helperIdentity, journal)
	}
	if helperPID > 0 {
		witnesses = append(witnesses, preAuthorityProcessWitness{
			role: "helper", pid: helperPID, identity: helperIdentity,
		})
	}
	return witnesses
}

func preAuthorityTargetBinding(t *testing.T, journal state.RunJournal, report preAuthorityWitnessCrashReport) (int, string) {
	t.Helper()
	if journal.ContainmentAuthority != nil {
		value := journal.ContainmentAuthority
		if journal.HasProcessDetails() && (journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity) {
			t.Fatalf("journal target marker disagrees with authority: pid=%d identity=%q authority=%#v", journal.PID, journal.ProcessIdentity, value)
		}
		return value.TargetPID, value.TargetIdentity
	}
	if journal.ContainmentHandoff != nil {
		return journal.ContainmentHandoff.TargetPID, journal.ContainmentHandoff.TargetIdentity
	}
	if journal.HasProcessDetails() {
		return journal.PID, journal.ProcessIdentity
	}
	return report.TargetPID, report.TargetIdentity
}

func preAuthorityHelperBinding(journal state.RunJournal, report preAuthorityWitnessCrashReport) (int, string) {
	if journal.ContainmentAuthority != nil {
		return journal.ContainmentAuthority.SupervisorPID, journal.ContainmentAuthority.SupervisorIdentity
	}
	if journal.ContainmentHandoff != nil {
		return journal.ContainmentHandoff.SupervisorPID, journal.ContainmentHandoff.SupervisorIdentity
	}
	return report.SupervisorPID, report.SupervisorIdentity
}

func loadPreAuthoritySentinelReadyRecord(t *testing.T, path string) (preAuthoritySentinelReadyRecord, bool) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return preAuthoritySentinelReadyRecord{}, false
	}
	if err != nil {
		t.Fatalf("read sentinel ready record: %v", err)
	}
	var record preAuthoritySentinelReadyRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		t.Fatalf("decode sentinel ready record: %v", err)
	}
	if record.PID <= 0 || record.StartedAt.IsZero() {
		t.Fatalf("sentinel ready record is incomplete: %#v", record)
	}
	return record, true
}

func assertPreAuthorityProcessStopped(t *testing.T, witness preAuthorityProcessWitness) {
	t.Helper()
	identity, err := platform.ProcessIdentity(witness.pid)
	if err != nil {
		if isProcessIdentityNotFoundError(err) {
			return
		}
		t.Fatalf("inspect %s process %d after recovery: %v", witness.role, witness.pid, err)
	}
	if identity != witness.identity {
		return
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(witness.pid))
	if err != nil {
		if isProcessIdentityNotFoundError(err) {
			return
		}
		t.Fatalf("open %s process %d after recovery: %v", witness.role, witness.pid, err)
	}
	defer windows.CloseHandle(handle)

	result, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil {
		t.Fatalf("wait for %s process %d after recovery: %v", witness.role, witness.pid, err)
	}
	switch result {
	case windows.WAIT_OBJECT_0:
		return
	case uint32(windows.WAIT_TIMEOUT):
		currentIdentity, identityErr := platform.ProcessIdentity(witness.pid)
		if identityErr != nil && isProcessIdentityNotFoundError(identityErr) {
			return
		}
		if identityErr == nil && currentIdentity != witness.identity {
			return
		}
		if identityErr != nil {
			t.Fatalf("%s process %d remained unproven after recovery: %v", witness.role, witness.pid, identityErr)
		}
		t.Fatalf("%s process %d with identity %q remained live after recovery", witness.role, witness.pid, witness.identity)
	default:
		t.Fatalf("wait for %s process %d returned %#x", witness.role, witness.pid, result)
	}
}

type preAuthorityWitnessInvocation struct {
	Mode              string
	StateDir          string
	RunID             string
	Generation        int64
	Sentinel          string
	ReadyFile         string
	ResultFile        string
	CrashAt           string
	WrongSecretMarker string
}

func runPreAuthorityWitnessBinary(t *testing.T, binary, workDir string, invocation preAuthorityWitnessInvocation, wantCrash bool) {
	t.Helper()
	stdoutPath := filepath.Join(workDir, invocation.Mode+".stdout.log")
	stderrPath := filepath.Join(workDir, invocation.Mode+".stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		t.Fatal(err)
	}
	defer stdout.Close()
	defer stderr.Close()

	arguments := []string{
		preAuthorityWitnessCommand,
		"-mode", invocation.Mode,
		"-state-dir", invocation.StateDir,
		"-run-id", invocation.RunID,
		"-generation", fmt.Sprintf("%d", invocation.Generation),
		"-result-file", invocation.ResultFile,
	}
	if invocation.Mode == "start" {
		arguments = append(arguments, "-sentinel", invocation.Sentinel, "-ready-file", invocation.ReadyFile)
		if invocation.CrashAt != "" {
			arguments = append(arguments, "-crash-at", invocation.CrashAt)
		}
		if invocation.WrongSecretMarker != "" {
			arguments = append(arguments, "-wrong-secret-marker", invocation.WrongSecretMarker)
		}
	}
	command := exec.Command(binary, arguments...)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatal(err)
	}
	err = command.Run()
	if wantCrash {
		if err == nil {
			t.Fatalf("production witness mode %s returned success; want hard crash at %s", invocation.Mode, invocation.CrashAt)
		}
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("production witness mode %s error = %v, want process exit", invocation.Mode, err)
		}
		if exitError.ExitCode() != 137 {
			t.Fatalf("production witness mode %s exit code = %d, want 137", invocation.Mode, exitError.ExitCode())
		}
		return
	}
	if err != nil {
		stdoutContents, _ := os.ReadFile(stdoutPath)
		stderrContents, _ := os.ReadFile(stderrPath)
		t.Fatalf("production witness mode %s failed: %v\nstdout:\n%s\nstderr:\n%s", invocation.Mode, err, stdoutContents, stderrContents)
	}
}

func loadPreAuthorityWitnessJournal(t *testing.T, stateDir string, key state.RunKey) state.RunJournal {
	t.Helper()
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func waitForPreAuthorityWitnessPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("path %q did not become ready within %s", path, timeout)
}

func buildPreAuthoritySentinel(t *testing.T, runRoot string) string {
	t.Helper()
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate pre-authority witness source")
	}
	daemonRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), ".."))
	output := filepath.Join(runRoot, "preauthority-sentinel.exe")
	command := exec.Command("go", "build", "-o", output, "./e2e/preauthority-sentinel")
	command.Dir = daemonRoot
	var outputBuffer bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &outputBuffer
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Run(); err != nil {
		t.Fatalf("build pre-authority sentinel: %v\n%s", err, strings.TrimSpace(outputBuffer.String()))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	return output
}
