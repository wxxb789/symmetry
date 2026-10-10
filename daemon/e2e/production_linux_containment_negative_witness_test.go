//go:build linux

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

const linuxNegativeWitnessEnvironment = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_WITNESS"

const (
	linuxNegativePIDReuseNamespaceHelperEnv        = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_NAMESPACE_HELPER"
	linuxNegativePIDReuseVictimEnv                 = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_VICTIM"
	linuxNegativePIDReuseParentPIDNamespaceEnv     = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_PARENT_PID_NAMESPACE"
	linuxNegativePIDReuseParentNetworkNamespaceEnv = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_PARENT_NETWORK_NAMESPACE"
	linuxNegativePIDReuseExpectedControlHashEnv    = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_CONTROL_URL_SHA256"
	linuxNegativePIDReuseDaemonBinaryEnv           = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_DAEMON_BINARY"
	linuxNegativePIDReuseCaseRootEnv               = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_CASE_ROOT"
	linuxNegativePIDReuseMetadataEnv               = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_PID_REUSE_METADATA"
	linuxOwnerStopBeforeProofReachedEnv            = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_REACHED_FILE"
	linuxOwnerStopBeforeProofReleaseEnv            = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_RELEASE_FILE"
	linuxOwnerStopProofResultEnv                   = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_PROOF_RESULT_FILE"
	linuxOwnerRecoveryAuthenticatedEnv             = "SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_RECOVERY_AUTHENTICATED_FILE"
	linuxWrongHelloSessionInjectionEnv             = "SYMMETRY_LINUX_SUPERVISOR_TEST_INJECT_WRONG_HELLO_SESSION"
	linuxWrongHelloSessionFiredEnv                 = "SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_FIRED_PATH"
	linuxWrongHelloSessionReleaseEnv               = "SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_RELEASE_PATH"
	linuxScanFailureModeEnv                        = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE"
	linuxScanFailureTriggerEnv                     = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH"
	linuxScanFailureFiredEnv                       = "SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH"
	linuxScanPendingReleaseEnv                     = "SYMMETRY_LINUX_SUPERVISOR_SCAN_PENDING_RELEASE_PATH"
	linuxScanPendingMode                           = "children_after_initial_pending"
)

type linuxNegativeWitnessMetadata struct {
	linuxWitnessMetadata
}

type linuxNegativeWitnessEvidence struct {
	Version          int                               `json:"version"`
	Case             string                            `json:"case"`
	Status           string                            `json:"status"`
	Metadata         linuxNegativeWitnessMetadata      `json:"metadata"`
	CaseRoot         string                            `json:"case_root"`
	Crash            *linuxWitnessExitStatus           `json:"crash,omitempty"`
	JournalSnapshots []linuxWitnessJournalSnapshot     `json:"journal_snapshots"`
	ProcessSnapshots []linuxWitnessProcessSnapshot     `json:"process_snapshots"`
	HelperHello      *linuxNegativeHelloSessionWitness `json:"helper_hello,omitempty"`
	PIDReuse         *linuxNegativePIDReuseEvidence    `json:"pid_reuse,omitempty"`
	Assertions       map[string]bool                   `json:"assertions"`
	RawOutput        map[string]string                 `json:"raw_output,omitempty"`
	Notes            []string                          `json:"notes,omitempty"`
}

type linuxNegativePIDReuseEvidence struct {
	TaskID                                   string                                `json:"task_id,omitempty"`
	ParentPIDNamespaceInode                  uint64                                `json:"parent_pid_namespace_inode,omitempty"`
	PrivatePIDNamespaceInode                 uint64                                `json:"private_pid_namespace_inode,omitempty"`
	ParentNetworkNamespaceInode              uint64                                `json:"parent_network_namespace_inode,omitempty"`
	PrivateNetworkNamespaceInode             uint64                                `json:"private_network_namespace_inode,omitempty"`
	ControlURLSHA256                         string                                `json:"control_url_sha256,omitempty"`
	NamespaceCreationFlags                   []string                              `json:"namespace_creation_flags,omitempty"`
	NamespaceAllocatorPath                   string                                `json:"namespace_allocator_path,omitempty"`
	NamespaceAllocatorBefore                 string                                `json:"namespace_allocator_before,omitempty"`
	NamespaceAllocatorResetTo                int                                   `json:"namespace_allocator_reset_to,omitempty"`
	NamespaceAllocatorReadback               string                                `json:"namespace_allocator_readback,omitempty"`
	PIDFDGroupSignalSupported                bool                                  `json:"pidfd_group_signal_supported"`
	OriginalTarget                           *linuxNegativePIDReuseProcessIdentity `json:"original_target,omitempty"`
	Helper                                   *linuxNegativePIDReuseProcessIdentity `json:"helper,omitempty"`
	Replacement                              *linuxNegativePIDReuseProcessIdentity `json:"replacement,omitempty"`
	OwnerStopBeforeProofReachedPath          string                                `json:"owner_stop_before_proof_reached_path,omitempty"`
	OwnerStopBeforeProofReleasePath          string                                `json:"owner_stop_before_proof_release_path,omitempty"`
	OwnerStopProofResultPath                 string                                `json:"owner_stop_proof_result_path,omitempty"`
	RecoveryAuthenticatedMarkerPath          string                                `json:"recovery_authenticated_marker_path,omitempty"`
	OwnerStopBeforeProofReached              bool                                  `json:"owner_stop_before_proof_reached"`
	OwnerStopBeforeProofReachedSHA256        string                                `json:"owner_stop_before_proof_reached_sha256,omitempty"`
	OwnerStopBeforeProofRelease              bool                                  `json:"owner_stop_before_proof_release"`
	OwnerStopProofResult                     string                                `json:"owner_stop_proof_result,omitempty"`
	RecoveryHelperScanSealEligible           bool                                  `json:"recovery_helper_scan_seal_eligible"`
	RecoveryHelperScanSealEligibleAfterProof bool                                  `json:"recovery_helper_scan_seal_eligible_after_proof"`
	RecoveryStopFailureObserved              bool                                  `json:"recovery_stop_failure_observed"`
	RecoveryStopFailureEvent                 string                                `json:"recovery_stop_failure_event,omitempty"`
	RecoveryRuntimeRegistered                bool                                  `json:"recovery_runtime_registered"`
	CrashStatuses                            []linuxNegativePIDReuseCrashStatus    `json:"crash_statuses,omitempty"`
	RecoveryDaemonShutdown                   *linuxWitnessExitStatus               `json:"recovery_daemon_shutdown,omitempty"`
	Assertions                               map[string]bool                       `json:"assertions"`
	OutputDigests                            map[string]string                     `json:"output_digests,omitempty"`
	Notes                                    []string                              `json:"notes,omitempty"`
}

type linuxNegativePIDReuseProcessIdentity struct {
	PID       int    `json:"pid"`
	Identity  string `json:"identity"`
	PGRP      int64  `json:"pgrp"`
	Session   int64  `json:"session"`
	StartTime uint64 `json:"start_time"`
}

type linuxNegativePIDReuseCrashStatus struct {
	Stage  string                 `json:"stage"`
	Status linuxWitnessExitStatus `json:"status"`
}

type linuxNegativeHelloAnchor struct {
	TargetPID       int    `json:"target_pid"`
	TargetIdentity  string `json:"target_identity"`
	TargetPGRP      int64  `json:"target_pgrp"`
	TargetSession   int64  `json:"target_session"`
	TargetStartTime uint64 `json:"target_start_time"`
}

type linuxNegativeHelloSessionWitness struct {
	Actual             linuxNegativeHelloAnchor `json:"actual"`
	Transmitted        linuxNegativeHelloAnchor `json:"transmitted"`
	SupervisorPID      int                      `json:"supervisor_pid"`
	SupervisorIdentity string                   `json:"supervisor_identity"`
}

// TestProductionLinuxContainmentNegativeWitness is an opt-in witness for
// rejection and fail-closed evidence on the real daemon and hidden helper
// binary. When enabled, none of its cases are skipped.
func TestProductionLinuxContainmentNegativeWitness(t *testing.T) {
	if os.Getenv(linuxNegativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxNegativeWitnessEnvironment + "=1 to run the Linux negative containment witness")
	}
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatalf("required prerequisite unavailable: set SYMMETRY_E2E=1 when %s=1; the production witness must not skip its private-namespace case", linuxNegativeWitnessEnvironment)
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

	t.Run("wrong_hello_session_rejected_before_resume_owner_sigkill", func(t *testing.T) {
		runLinuxWrongHelloSessionWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "wrong-hello-session"))
	})
	t.Run("observed_setsid_escape", func(t *testing.T) {
		runLinuxObservedSetsidEscapeWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "setsid-escape"))
	})
	t.Run("injected_descendant_scan_failure", func(t *testing.T) {
		runLinuxInjectedScanFailureWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "scan-failure"))
	})
	t.Run("helper_scan_failure_sigkill_before_cancel", func(t *testing.T) {
		runLinuxHelperScanCrashWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "helper-scan-failure-crash"), false)
	})
	t.Run("helper_pending_scan_sigkill", func(t *testing.T) {
		runLinuxHelperScanCrashWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "helper-scan-pending-crash"), true)
	})
	t.Run("daemon_owner_loss_literal_pid_reuse_rejected_after_restart", func(t *testing.T) {
		runLinuxProductionPIDReuseNamespaceWitness(t, environment, daemonBinary, metadata, linuxWitnessCaseRoot(runRoot, "literal-pid-reuse"))
	})
}

// TestProductionLinuxContainmentNegativeWitnessPIDReuseNamespaceHelper runs
// only inside the private namespace started by the negative witness above.
// Keeping this as a separate test lets the outer runner observe a real PID 1
// without recursively running the whole opt-in suite.
func TestProductionLinuxContainmentNegativeWitnessPIDReuseNamespaceHelper(t *testing.T) {
	if os.Getenv(linuxNegativePIDReuseNamespaceHelperEnv) != "1" {
		return
	}
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatalf("required prerequisite unavailable: nested production witness requires SYMMETRY_E2E=1")
	}
	if os.Getenv(linuxNegativeWitnessEnvironment) != "1" {
		t.Fatalf("required prerequisite unavailable: nested production witness requires %s=1", linuxNegativeWitnessEnvironment)
	}

	parentPIDNamespace, err := linuxNegativeParseNamespaceInodeEnv(linuxNegativePIDReuseParentPIDNamespaceEnv)
	if err != nil {
		t.Fatal(err)
	}
	parentNetworkNamespace, err := linuxNegativeParseNamespaceInodeEnv(linuxNegativePIDReuseParentNetworkNamespaceEnv)
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := os.Getenv(linuxNegativePIDReuseDaemonBinaryEnv)
	caseRoot := os.Getenv(linuxNegativePIDReuseCaseRootEnv)
	if binaryPath == "" || !filepath.IsAbs(binaryPath) {
		t.Fatalf("required prerequisite unavailable: production daemon binary path must be absolute")
	}
	if caseRoot == "" || !filepath.IsAbs(caseRoot) {
		t.Fatalf("required prerequisite unavailable: PID-reuse case root must be absolute")
	}
	var metadata linuxNegativeWitnessMetadata
	if err := json.Unmarshal([]byte(os.Getenv(linuxNegativePIDReuseMetadataEnv)), &metadata); err != nil {
		t.Fatalf("decode parent production witness metadata: %v", err)
	}
	if metadata.BinaryPath != binaryPath || metadata.BinarySHA256 == "" {
		t.Fatalf("nested daemon binary does not match parent-built production metadata")
	}
	actualBinarySHA256, err := linuxWitnessSHA256(binaryPath)
	if err != nil || actualBinarySHA256 != metadata.BinarySHA256 {
		t.Fatalf("production daemon binary changed before nested witness (hash error: %v)", err)
	}
	expectedControlURLSHA256 := os.Getenv(linuxNegativePIDReuseExpectedControlHashEnv)
	if len(expectedControlURLSHA256) != 64 {
		t.Fatalf("required prerequisite unavailable: parent Control URL hash is missing or malformed")
	}

	runLinuxProductionPIDReuseNamespaceCase(t, metadata, binaryPath, caseRoot, parentPIDNamespace, parentNetworkNamespace, expectedControlURLSHA256)
}

func runLinuxProductionPIDReuseNamespaceWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot string) {
	t.Helper()
	unsharePath, err := exec.LookPath("unshare")
	if err != nil {
		t.Fatalf("required prerequisite unavailable: unshare is required for the production PID-reuse witness: %v", err)
	}
	if !linuxNegativeControlURLIsLoopback(environment.baseURL) {
		t.Fatalf("required prerequisite unavailable: production PID-reuse witness requires a loopback Control URL")
	}
	if !filepath.IsAbs(daemonBinary) {
		t.Fatalf("production daemon binary path %q is not absolute", daemonBinary)
	}
	caseRoot, err = filepath.Abs(caseRoot)
	if err != nil {
		t.Fatalf("resolve production PID-reuse case root: %v", err)
	}
	parentPIDNamespace, err := linuxNegativeNamespaceInode("/proc/self/ns/pid")
	if err != nil {
		t.Fatalf("read parent PID namespace before unshare: %v", err)
	}
	parentNetworkNamespace, err := linuxNegativeNamespaceInode("/proc/self/ns/net")
	if err != nil {
		t.Fatalf("read parent network namespace before unshare: %v", err)
	}
	metadataContents, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("encode parent production witness metadata for nested runner: %v", err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve e2e test binary for private PID namespace: %v", err)
	}
	var output bytes.Buffer
	command := exec.Command(
		unsharePath,
		"--user", "--map-root-user", "--pid", "--fork", "--mount-proc",
		testBinary,
		"-test.run=^TestProductionLinuxContainmentNegativeWitnessPIDReuseNamespaceHelper$",
		"-test.v", "-test.timeout=240s",
	)
	childEnvironment := append([]string(nil), os.Environ()...)
	for _, name := range []string{linuxOwnerStopBeforeProofReachedEnv, linuxOwnerStopBeforeProofReleaseEnv, linuxOwnerStopProofResultEnv, linuxOwnerRecoveryAuthenticatedEnv} {
		childEnvironment = unsetLinuxNegativeWitnessEnvironment(childEnvironment, name)
	}
	for name, value := range map[string]string{
		linuxNegativePIDReuseNamespaceHelperEnv:        "1",
		linuxNegativePIDReuseParentPIDNamespaceEnv:     strconv.FormatUint(parentPIDNamespace, 10),
		linuxNegativePIDReuseParentNetworkNamespaceEnv: strconv.FormatUint(parentNetworkNamespace, 10),
		linuxNegativePIDReuseExpectedControlHashEnv:    linuxWitnessHashBytes([]byte(environment.baseURL)),
		linuxNegativePIDReuseDaemonBinaryEnv:           daemonBinary,
		linuxNegativePIDReuseCaseRootEnv:               caseRoot,
		linuxNegativePIDReuseMetadataEnv:               string(metadataContents),
	} {
		childEnvironment = setLinuxWitnessEnvironment(childEnvironment, name, value)
	}
	command.Env = childEnvironment
	command.Stdout = &output
	command.Stderr = &output
	namespaceRunnerPIDFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL, PidFD: &namespaceRunnerPIDFD}
	if err := command.Start(); err != nil {
		t.Fatalf("required prerequisite unavailable: could not start unshare for private user+PID namespaces: %v", err)
	}
	if namespaceRunnerPIDFD < 0 {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		t.Fatalf("required prerequisite unavailable: Go did not retain an unshare pidfd for bounded cleanup (signal error: %v, wait error: %v; nested output: %d bytes sha256=%s)", killErr, waitErr, output.Len(), linuxWitnessHashBytes(output.Bytes()))
	}
	t.Cleanup(func() {
		if err := unix.Close(namespaceRunnerPIDFD); err != nil {
			t.Errorf("close private namespace runner pidfd: %v", err)
		}
	})
	if err := waitForLinuxNegativePIDReuseNamespaceCommand(command, namespaceRunnerPIDFD, &output, 5*time.Minute); err != nil {
		t.Fatalf("%s: %v; nested output: %d bytes sha256=%s; evidence=%s", linuxNegativePIDReuseNamespaceFailureSummary(output.String()), err, output.Len(), linuxWitnessHashBytes(output.Bytes()), filepath.Join(caseRoot, "witness-evidence.json"))
	}

	evidencePath := filepath.Join(caseRoot, "witness-evidence.json")
	contents, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatalf("nested production PID-reuse witness did not publish evidence (it may have skipped): %v; nested output: %d bytes sha256=%s", err, output.Len(), linuxWitnessHashBytes(output.Bytes()))
	}
	var evidence linuxNegativeWitnessEvidence
	if err := json.Unmarshal(contents, &evidence); err != nil {
		t.Fatalf("decode nested production PID-reuse evidence: %v", err)
	}
	if evidence.Case != "daemon_owner_loss_literal_pid_reuse_rejected_after_restart" || evidence.Status != "passed" || evidence.PIDReuse == nil {
		t.Fatalf("nested production PID-reuse evidence is incomplete: case=%q status=%q details=%t", evidence.Case, evidence.Status, evidence.PIDReuse != nil)
	}
	for _, assertion := range []string{
		"private_pid_namespace",
		"network_namespace_unchanged",
		"control_loopback_unchanged",
		"namespace_local_allocator_only",
		"lease_armed_mirror_sealed",
		"helper_recovery_eligible_at_crash",
		"target_exec_stopped",
		"daemon_owner_sigkill",
		"target_reaped_before_proof",
		"pidfd_group_signal_supported",
		"same_pid_and_pgrp_reused",
		"replacement_start_time_advanced",
		"owner_proof_identity_mismatch",
		"helper_recovery_eligible_after_owner_proof",
		"replacement_alive_after_owner_proof",
		"target_user_code_absent_after_owner_proof",
		"journal_unreceipted_after_owner_proof",
		"restart_recovery_attempt_observed",
		"restart_recovery_authenticated_helper_request",
		"restart_runtime_registered_after_recovery",
		"journal_unreceipted_after_restart",
		"replacement_alive_after_restart",
		"target_user_code_absent_after_restart",
		"helper_peer_identity_exact",
	} {
		if !evidence.PIDReuse.Assertions[assertion] {
			t.Fatalf("nested production PID-reuse evidence lacks assertion %q", assertion)
		}
	}
	if evidence.Metadata.Subject != metadata.Subject || evidence.Metadata.BinarySHA256 != metadata.BinarySHA256 {
		t.Fatalf("nested production PID-reuse evidence subject/binary does not match parent-built metadata")
	}
	if !evidence.PIDReuse.PIDFDGroupSignalSupported {
		t.Fatalf("nested production PID-reuse evidence lacks live pidfd process-group signal capability")
	}
	if evidence.PIDReuse.OutputDigests == nil {
		evidence.PIDReuse.OutputDigests = make(map[string]string)
	}
	evidence.PIDReuse.OutputDigests["nested_test_runner"] = fmt.Sprintf("%d bytes sha256:%s", output.Len(), linuxWitnessHashBytes(output.Bytes()))
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)
	t.Logf("production PID-reuse evidence: subject=%s target=%#v helper=%#v replacement=%#v kernel=%s pidns=%d netns=%d evidence=%s", evidence.Metadata.Subject, evidence.PIDReuse.OriginalTarget, evidence.PIDReuse.Helper, evidence.PIDReuse.Replacement, evidence.Metadata.KernelRelease, evidence.PIDReuse.PrivatePIDNamespaceInode, evidence.PIDReuse.PrivateNetworkNamespaceInode, evidencePath)
}

func waitForLinuxNegativePIDReuseNamespaceCommand(command *exec.Cmd, pidfd int, output *bytes.Buffer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return terminateLinuxNegativePIDReuseNamespaceCommand(command, pidfd, output, "private namespace witness exceeded its deadline")
		}
		pollMillis := int((remaining + time.Millisecond - 1) / time.Millisecond)
		if pollMillis > 100 {
			pollMillis = 100
		}
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		count, err := unix.Poll(fds, pollMillis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return terminateLinuxNegativePIDReuseNamespaceCommand(command, pidfd, output, "poll private namespace runner")
		}
		if count == 0 {
			continue
		}
		if err := command.Wait(); err != nil {
			return fmt.Errorf("nested namespace test exited unsuccessfully: %w", err)
		}
		return nil
	}
}

func linuxNegativePIDReuseNamespaceFailureSummary(output string) string {
	value := strings.ToLower(output)
	if strings.Contains(value, "required prerequisite unavailable") {
		return "required prerequisite unavailable: nested test could not satisfy a namespace, Control, allocator, or pidfd capability"
	}
	if strings.Contains(value, "operation not permitted") || strings.Contains(value, "permission denied") {
		return "required prerequisite unavailable: kernel or container policy denied private user/PID namespace creation"
	}
	if strings.Contains(value, "unshare") && strings.Contains(value, "failed") {
		return "required prerequisite unavailable: unshare could not create the requested private user/PID namespace"
	}
	return "required private PID-reuse production witness failed; raw nested output is omitted to avoid credential leakage"
}

func terminateLinuxNegativePIDReuseNamespaceCommand(command *exec.Cmd, pidfd int, output *bytes.Buffer, reason string) error {
	groupErr := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP)
	if groupErr != nil && !errors.Is(groupErr, unix.ESRCH) {
		groupErr = errors.Join(groupErr, unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0))
	}
	if exitErr := waitForLinuxNegativePIDFDExit(pidfd, 20*time.Second); exitErr != nil {
		return fmt.Errorf("%s; pidfd process-group signal=%v; runner did not exit during bounded cleanup: %w; output=%d bytes sha256=%s", reason, groupErr, exitErr, output.Len(), linuxWitnessHashBytes(output.Bytes()))
	}
	waitErr := command.Wait()
	return fmt.Errorf("%s; pidfd process-group signal=%v; runner wait=%v; output=%d bytes sha256=%s", reason, groupErr, waitErr, output.Len(), linuxWitnessHashBytes(output.Bytes()))
}

func unsetLinuxNegativeWitnessEnvironment(environment []string, name string) []string {
	prefix := name + "="
	filtered := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func linuxNegativeNamespaceInode(path string) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, err
	}
	if stat.Ino == 0 {
		return 0, errors.New("namespace inode is zero")
	}
	return uint64(stat.Ino), nil
}

func linuxNegativeParseNamespaceInodeEnv(name string) (uint64, error) {
	value, err := strconv.ParseUint(os.Getenv(name), 10, 64)
	if err != nil || value == 0 {
		if err == nil {
			err = errors.New("namespace inode must be positive")
		}
		return 0, fmt.Errorf("required prerequisite unavailable: parse %s: %w", name, err)
	}
	return value, nil
}

func linuxNegativeControlURLIsLoopback(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func runLinuxProductionPIDReuseNamespaceCase(t *testing.T, metadata linuxNegativeWitnessMetadata, daemonBinary, caseRoot string, parentPIDNamespace, parentNetworkNamespace uint64, expectedControlURLSHA256 string) {
	t.Helper()
	evidencePath := filepath.Join(caseRoot, "witness-evidence.json")
	proofReachedPath := filepath.Join(caseRoot, "owner-stop-before-proof.reached")
	proofReleasePath := filepath.Join(caseRoot, "owner-stop-before-proof.release")
	proofResultPath := filepath.Join(caseRoot, "owner-stop-proof.result")
	recoveryAuthenticatedPath := filepath.Join(caseRoot, "owner-recovery-authenticated.marker")
	proofPaths := []string{proofReachedPath, proofReleasePath, proofResultPath}
	markerPaths := []string{proofReachedPath, proofReleasePath, proofResultPath, recoveryAuthenticatedPath}
	evidence := linuxNegativeWitnessEvidence{
		Version: 1, Case: "daemon_owner_loss_literal_pid_reuse_rejected_after_restart", Status: "initialized",
		Metadata: metadata, CaseRoot: caseRoot, Assertions: map[string]bool{},
		PIDReuse: &linuxNegativePIDReuseEvidence{
			ParentPIDNamespaceInode: parentPIDNamespace, ParentNetworkNamespaceInode: parentNetworkNamespace,
			ControlURLSHA256: expectedControlURLSHA256, NamespaceAllocatorPath: "/proc/sys/kernel/ns_last_pid",
			Assertions: map[string]bool{}, OutputDigests: map[string]string{},
		},
	}
	evidence.PIDReuse.OwnerStopBeforeProofReachedPath = proofReachedPath
	evidence.PIDReuse.OwnerStopBeforeProofReleasePath = proofReleasePath
	evidence.PIDReuse.OwnerStopProofResultPath = proofResultPath
	evidence.PIDReuse.RecoveryAuthenticatedMarkerPath = recoveryAuthenticatedPath
	evidence.PIDReuse.NamespaceCreationFlags = []string{"--user", "--map-root-user", "--pid", "--fork", "--mount-proc"}
	if err := os.MkdirAll(caseRoot, 0o700); err != nil {
		t.Fatalf("create private-namespace PID-reuse case root: %v", err)
	}
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)

	var (
		target                 linuxWitnessTarget
		targetPrepared         bool
		firstDaemon            *productionDaemonWitnessProcess
		firstDaemonPIDFD       = -1
		firstDaemonIdentity    string
		recoveryDaemon         *productionDaemonWitnessProcess
		recoveryDaemonPIDFD    = -1
		recoveryDaemonIdentity string
		replacement            *linuxNegativePIDReuseVictim
		key                    state.RunKey
		targetPID              int
		targetIdentity         string
		helperPID              int
		helperIdentity         string
		cleanupErrors          []error
	)
	defer func() {
		cleanupErrors = cleanupLinuxProductionPIDReuseWitness(
			t, &evidence, target, targetPrepared, &firstDaemon, firstDaemonPIDFD,
			&recoveryDaemon, recoveryDaemonPIDFD, replacement, &key, &targetPID, &targetIdentity,
			&helperPID, &helperIdentity, proofPaths,
		)
		if firstDaemonPIDFD >= 0 {
			if err := unix.Close(firstDaemonPIDFD); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("close first production daemon pidfd: %w", err))
			}
		}
		if recoveryDaemonPIDFD >= 0 {
			if err := unix.Close(recoveryDaemonPIDFD); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("close recovery production daemon pidfd: %w", err))
			}
		}
		evidence.PIDReuse.OutputDigests["first_daemon_stdout"] = linuxNegativePIDReuseLogDigest(firstDaemon, true)
		evidence.PIDReuse.OutputDigests["first_daemon_stderr"] = linuxNegativePIDReuseLogDigest(firstDaemon, false)
		evidence.PIDReuse.OutputDigests["recovery_daemon_stdout"] = linuxNegativePIDReuseLogDigest(recoveryDaemon, true)
		evidence.PIDReuse.OutputDigests["recovery_daemon_stderr"] = linuxNegativePIDReuseLogDigest(recoveryDaemon, false)
		if len(cleanupErrors) > 0 {
			for _, cleanupErr := range cleanupErrors {
				evidence.PIDReuse.Notes = append(evidence.PIDReuse.Notes, "bounded pidfd-only cleanup could not complete: "+cleanupErr.Error())
				t.Errorf("Linux production PID-reuse witness cleanup: %v", cleanupErr)
			}
		}
		if t.Failed() {
			evidence.Status = "failed"
		} else {
			evidence.Status = "passed"
		}
		writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)
	}()

	if os.Getpid() != 1 {
		t.Fatalf("required private namespace unavailable: nested production witness PID = %d, want PID 1", os.Getpid())
	}
	privatePIDNamespace, err := linuxNegativeNamespaceInode("/proc/self/ns/pid")
	if err != nil {
		t.Fatalf("read nested private PID namespace identity: %v", err)
	}
	if privatePIDNamespace == parentPIDNamespace {
		t.Fatalf("required private namespace unavailable: PID namespace inode %d matches parent; refusing to write ns_last_pid", privatePIDNamespace)
	}
	privateNetworkNamespace, err := linuxNegativeNamespaceInode("/proc/self/ns/net")
	if err != nil {
		t.Fatalf("read nested network namespace identity: %v", err)
	}
	if privateNetworkNamespace != parentNetworkNamespace {
		t.Fatalf("Control network namespace changed inside PID-reuse witness: child netns=%d parent netns=%d", privateNetworkNamespace, parentNetworkNamespace)
	}
	evidence.PIDReuse.PrivatePIDNamespaceInode = privatePIDNamespace
	evidence.PIDReuse.PrivateNetworkNamespaceInode = privateNetworkNamespace
	evidence.PIDReuse.Assertions["private_pid_namespace"] = true
	evidence.PIDReuse.Assertions["network_namespace_unchanged"] = true

	if !linuxNegativeControlURLIsLoopback(os.Getenv("SYMMETRY_E2E_URL")) && os.Getenv("SYMMETRY_E2E_URL") != "" {
		t.Fatalf("required prerequisite unavailable: nested Control URL is not loopback")
	}
	environment := loadControlEnvironment(t)
	if !linuxNegativeControlURLIsLoopback(environment.baseURL) {
		t.Fatalf("required prerequisite unavailable: nested production witness Control URL is not loopback")
	}
	if actualControlURLSHA256 := linuxWitnessHashBytes([]byte(environment.baseURL)); actualControlURLSHA256 != expectedControlURLSHA256 {
		t.Fatalf("Control URL changed across private PID namespace setup (parent hash=%s child hash=%s)", expectedControlURLSHA256, actualControlURLSHA256)
	}
	evidence.PIDReuse.Assertions["control_loopback_unchanged"] = true

	for _, markerPath := range markerPaths {
		if !filepath.IsAbs(markerPath) {
			t.Fatalf("Linux supervisor witness marker path %q is not absolute", markerPath)
		}
	}
	if err := validateLinuxNegativeDistinctPaths(markerPaths); err != nil {
		t.Fatalf("validate Linux supervisor witness marker paths: %v", err)
	}
	for _, markerPath := range markerPaths {
		if _, err := os.Lstat(markerPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Linux supervisor witness marker %q before production daemon launch = %v, want absent", markerPath, err)
		}
	}

	target = prepareLinuxWitnessTarget(t, environment, caseRoot, "production-literal-pid-reuse")
	targetPrepared = true
	leaseReachedPath := filepath.Join(caseRoot, "lease-armed-resume.reached")
	leaseReleasePath := filepath.Join(caseRoot, "lease-armed-resume.release")
	scanTriggerPath := filepath.Join(caseRoot, "lease-armed-mirror-seal.trigger")
	scanReachedPath := filepath.Join(caseRoot, "lease-armed-mirror-seal.reached")
	scanReleasePath := filepath.Join(caseRoot, "lease-armed-mirror-seal.release")
	if err := primeLinuxWitnessScanSeal(scanTriggerPath, scanReachedPath, scanReleasePath); err != nil {
		t.Fatalf("prime pre-resume mirror seal before production PID-reuse launch: %v", err)
	}
	firstEnvironment := map[string]string{
		linuxProductionContainmentWitnessEnvironment:   "1",
		linuxLeaseArmedResumeBarrierReachedEnvironment: leaseReachedPath,
		linuxLeaseArmedResumeBarrierReleaseEnvironment: leaseReleasePath,
		linuxOwnerStopBeforeProofReachedEnv:            proofReachedPath,
		linuxOwnerStopBeforeProofReleaseEnv:            proofReleasePath,
		linuxOwnerStopProofResultEnv:                   proofResultPath,
		linuxOwnerRecoveryAuthenticatedEnv:             recoveryAuthenticatedPath,
	}
	for name, value := range linuxWitnessScanSealEnvironment(scanTriggerPath, scanReachedPath, scanReleasePath, "mirror") {
		firstEnvironment[name] = value
	}
	firstDaemon = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first", firstEnvironment)
	firstDaemonPIDFD, firstDaemonIdentity, err = openLinuxNegativeWitnessProcessPIDFD(firstDaemon.command.Process.Pid)
	if err != nil {
		t.Fatalf("open exact production daemon pidfd before owner-loss crash: %v", err)
	}
	firstDaemonSnapshot := linuxWitnessProcessSnapshotAt("first_daemon_before_owner_loss", firstDaemon.command.Process.Pid, firstDaemonIdentity)
	if !firstDaemonSnapshot.Exists || firstDaemonSnapshot.State == "Z" {
		t.Fatalf("first production daemon is not live before task submission: %#v", firstDaemonSnapshot)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, firstDaemonSnapshot)
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-production-literal-pid-reuse", "slow")
	evidence.PIDReuse.TaskID = task.TaskID
	if err := waitForLinuxWitnessMarker(leaseReachedPath, 45*time.Second); err != nil {
		t.Fatalf("wait for production daemon lease-armed/pre-resume marker: %v", err)
	}
	leaseMarker, err := os.ReadFile(leaseReachedPath)
	if err != nil {
		t.Fatalf("read production daemon lease-armed marker: %v", err)
	}
	if !strings.Contains(string(leaseMarker), "\nlease_armed=true\n") ||
		!strings.Contains(string(leaseMarker), fmt.Sprintf("pid=%d\n", firstDaemon.command.Process.Pid)) {
		t.Fatalf("production daemon lease-armed barrier did not identify the live daemon; marker_bytes=%d", len(leaseMarker))
	}

	initialJournal, err := waitForLinuxWitnessJournal(target.StateDir, "durable lease-armed Linux process authority", 30*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentAuthority != nil && value.Journal.ContainmentHandoff == nil
	})
	if err != nil {
		t.Fatal(err)
	}
	key = initialJournal.Journal.Key()
	if key.RunID == "" || key.Generation <= 0 {
		t.Fatalf("lease-armed journal key is incomplete: run_id=%q generation=%d", key.RunID, key.Generation)
	}
	targetPID, targetIdentity, helperPID, helperIdentity = linuxWitnessOwnerIDs(initialJournal.Journal)
	if targetPID <= 1 || targetIdentity == "" || helperPID <= 1 || helperIdentity == "" ||
		initialJournal.Journal.PID != targetPID || initialJournal.Journal.ProcessIdentity != targetIdentity ||
		initialJournal.Journal.ContainmentAuthority.StopReceipt != nil || !initialJournal.Journal.HasProcessDetails() {
		t.Fatalf("lease-armed production authority is incomplete or already receipted: target=(%d,%q) helper=(%d,%q) receipt=%t process_details=%t", targetPID, targetIdentity, helperPID, helperIdentity, initialJournal.Journal.ContainmentAuthority.StopReceipt != nil, initialJournal.Journal.HasProcessDetails())
	}
	if !strings.Contains(string(leaseMarker), fmt.Sprintf("target_pid=%d\n", targetPID)) {
		t.Fatalf("lease-armed marker target does not match durable authority PID %d", targetPID)
	}

	sealedJournal, _, mirrorSealMarker, err := awaitLinuxWitnessScanSeal(target.StateDir, key, scanReachedPath, "mirror", 45*time.Second)
	if err != nil {
		t.Fatalf("wait for durable mirror seal before production PID-reuse owner loss: %v", err)
	}
	if !linuxWitnessJournalHasScanSealBoundary(sealedJournal.Journal, "mirror") || sealedJournal.Journal.LeaseExpiresAt.IsZero() || !time.Now().Before(sealedJournal.Journal.LeaseExpiresAt) {
		t.Fatalf("lease-armed authority lacks an unexpired durable mirror seal: %#v", linuxWitnessJournalSnapshotFromFile(sealedJournal))
	}
	targetBefore := linuxWitnessProcessSnapshotAt("target_exec_stopped_before_owner_loss", targetPID, targetIdentity)
	helperBefore := linuxWitnessProcessSnapshotAt("helper_before_owner_loss", helperPID, helperIdentity)
	if !targetBefore.Exists || (targetBefore.State != "T" && targetBefore.State != "t") ||
		targetBefore.PGRP != int64(targetPID) || targetBefore.Session == int64(targetPID) || targetBefore.StartTime == 0 ||
		!helperBefore.Exists || helperBefore.StartTime == 0 {
		t.Fatalf("lease-armed production target/helper are not exact and stopped: target=%#v helper=%#v", targetBefore, helperBefore)
	}
	if err := assertLinuxWitnessProcessLive(targetPID, targetIdentity); err != nil {
		t.Fatalf("exact exec-stopped production target before daemon crash: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("exact production helper before daemon crash: %v", err)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker before owner-loss crash = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker before owner-loss crash = %v, want absent", err)
	}
	evidence.JournalSnapshots = append(evidence.JournalSnapshots,
		linuxWitnessJournalSnapshotFromFile(initialJournal),
		linuxWitnessJournalSnapshotFromFile(sealedJournal),
	)
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, targetBefore, helperBefore)
	evidence.PIDReuse.OriginalTarget = linuxNegativePIDReuseProcessIdentityFromSnapshot(targetBefore)
	evidence.PIDReuse.Helper = linuxNegativePIDReuseProcessIdentityFromSnapshot(helperBefore)
	evidence.PIDReuse.Assertions["lease_armed_mirror_sealed"] = true
	evidence.PIDReuse.Assertions["target_exec_stopped"] = true
	evidence.PIDReuse.Assertions["target_user_code_absent_before_owner_loss"] = true
	evidence.PIDReuse.Notes = append(evidence.PIDReuse.Notes,
		"the production target remained exec-stopped after initial lease arm and a durable mirror scan seal; only private user, PID, and mount/proc namespaces were created, with the network namespace and loopback Control endpoint unchanged",
		"daemon and helper logs are represented only by byte counts and SHA-256 digests; raw log contents are not copied into evidence",
		"the durable mirror-seal marker SHA-256 is "+linuxWitnessHashBytes([]byte(mirrorSealMarker)),
	)
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)

	time.Sleep(150 * time.Millisecond)
	beforeCrashJournal, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil {
		t.Fatalf("re-read production journal immediately before owner-loss crash: %v", err)
	}
	if !found || !beforeCrashJournal.Journal.RecoveryHelperScanSealEligible() {
		t.Fatalf("production journal lost helper recovery eligibility before owner loss: found=%t eligible=%t", found, found && beforeCrashJournal.Journal.RecoveryHelperScanSealEligible())
	}
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(beforeCrashJournal))
	evidence.PIDReuse.Assertions["helper_recovery_eligible_at_crash"] = true
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)
	crash := killLinuxNegativeProductionDaemon(firstDaemon, firstDaemonPIDFD)
	evidence.Crash = &crash
	evidence.PIDReuse.CrashStatuses = append(evidence.PIDReuse.CrashStatuses, linuxNegativePIDReuseCrashStatus{Stage: "first_daemon_owner_loss", Status: crash})
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("production owner crash status = %#v, want actual SIGKILL", crash)
	}
	evidence.PIDReuse.Assertions["daemon_owner_sigkill"] = true
	if err := waitForLinuxWitnessMarker(proofReachedPath, 45*time.Second); err != nil {
		t.Fatalf("wait for helper post-reap/pre-proof owner-loss marker: %v", err)
	}
	reachedContents, err := os.ReadFile(proofReachedPath)
	if err != nil || len(reachedContents) == 0 {
		t.Fatalf("read helper post-reap/pre-proof marker (error=%v bytes=%d)", err, len(reachedContents))
	}
	evidence.PIDReuse.OwnerStopBeforeProofReached = true
	evidence.PIDReuse.OwnerStopBeforeProofReachedSHA256 = linuxWitnessHashBytes(reachedContents)
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("exact helper identity at post-reap/pre-proof marker: %v", err)
	}
	if actualIdentity, identityErr := platform.ProcessIdentity(targetPID); identityErr == nil {
		t.Fatalf("original target PID %d remains present after helper's exact wait/reap; identity=%q", targetPID, actualIdentity)
	} else if !errors.Is(identityErr, os.ErrNotExist) && !errors.Is(identityErr, syscall.ESRCH) {
		t.Fatalf("inspect original target PID %d after helper wait/reap: %v", targetPID, identityErr)
	}
	reapedTarget := linuxWitnessProcessSnapshotAt("original_target_reaped_before_group_proof", targetPID, targetIdentity)
	if reapedTarget.Exists {
		t.Fatalf("original target is still present at pre-proof marker: %#v", reapedTarget)
	}
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, reapedTarget, linuxWitnessProcessSnapshotAt("helper_at_post_reap_pre_proof", helperPID, helperIdentity))
	evidence.PIDReuse.Assertions["target_reaped_before_proof"] = true
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after owner-loss wait/reap = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after owner-loss wait/reap = %v, want absent", err)
	}
	if file, found, readErr := readLinuxWitnessJournalForKey(target.StateDir, key); readErr != nil {
		t.Fatalf("read original journal at helper pre-proof marker: %v", readErr)
	} else if !found {
		t.Fatalf("original journal disappeared at helper pre-proof marker")
	} else {
		assertLinuxNegativePIDReuseJournalUnresolved(t, file, key, targetPID, targetIdentity, helperPID, helperIdentity)
		evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	}
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)

	allocatorPath := evidence.PIDReuse.NamespaceAllocatorPath
	allocatorBefore, err := os.ReadFile(allocatorPath)
	if err != nil {
		t.Fatalf("required prerequisite unavailable: read namespace-local %s: %v", allocatorPath, err)
	}
	if targetPID <= 1 {
		t.Fatalf("private namespace allocated original target PID %d, want PID greater than 1", targetPID)
	}
	allocatorResetTo := targetPID - 1
	if err := os.WriteFile(allocatorPath, []byte(strconv.Itoa(allocatorResetTo)), 0); err != nil {
		t.Fatalf("required prerequisite unavailable: private PID namespace-local %s must be writable; host PID allocator was not modified: %v", allocatorPath, err)
	}
	allocatorReadback, err := os.ReadFile(allocatorPath)
	if err != nil {
		t.Fatalf("read back private namespace-local PID allocator: %v", err)
	}
	parsedAllocatorReadback, err := strconv.Atoi(strings.TrimSpace(string(allocatorReadback)))
	if err != nil || parsedAllocatorReadback != allocatorResetTo {
		t.Fatalf("private namespace-local ns_last_pid readback = %q (parsed=%d error=%v), want %d", strings.TrimSpace(string(allocatorReadback)), parsedAllocatorReadback, err, allocatorResetTo)
	}
	evidence.PIDReuse.NamespaceAllocatorBefore = strings.TrimSpace(string(allocatorBefore))
	evidence.PIDReuse.NamespaceAllocatorResetTo = allocatorResetTo
	evidence.PIDReuse.NamespaceAllocatorReadback = strings.TrimSpace(string(allocatorReadback))
	evidence.PIDReuse.Assertions["namespace_local_allocator_only"] = true

	replacement, err = startLinuxNegativePIDReuseVictim()
	if err != nil {
		t.Fatalf("required prerequisite unavailable: start pidfd-owned replacement process in private namespace: %v", err)
	}
	if replacement.pid != targetPID {
		t.Fatalf("private namespace reused PID = %d, want original target PID %d after namespace-local ns_last_pid reset", replacement.pid, targetPID)
	}
	replacementIdentity, err := platform.ProcessIdentity(replacement.pid)
	if err != nil {
		t.Fatalf("read literal PID-reuse replacement identity: %v", err)
	}
	replacementSnapshot := linuxWitnessProcessSnapshotAt("replacement_before_owner_proof_release", replacement.pid, replacementIdentity)
	if !replacementSnapshot.Exists || replacementSnapshot.State == "Z" || replacementSnapshot.PGRP != int64(targetPID) ||
		replacementSnapshot.PGRP != targetBefore.PGRP || replacementSnapshot.Session != targetBefore.Session ||
		replacementSnapshot.StartTime <= targetBefore.StartTime {
		t.Fatalf("replacement did not reuse original PID/PGID/session with a later live identity: original=%#v replacement=%#v", targetBefore, replacementSnapshot)
	}
	if err := assertLinuxWitnessProcessLive(replacement.pid, replacementIdentity); err != nil {
		t.Fatalf("replacement process is not live before owner proof release: %v", err)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("required prerequisite unavailable: replacement pidfd process-group signal-0 failed: %v", err)
	}
	evidence.PIDReuse.PIDFDGroupSignalSupported = true
	evidence.PIDReuse.Assertions["pidfd_group_signal_supported"] = true
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("exact helper identity after literal PID reuse: %v", err)
	}
	if err := assertLinuxNegativePIDReuseIdentityReuse(targetIdentity, replacementIdentity, targetPID, privatePIDNamespace, targetBefore.StartTime, replacementSnapshot.StartTime); err != nil {
		t.Fatalf("verify production PID-reuse identity tuple: %v", err)
	}
	evidence.PIDReuse.Replacement = linuxNegativePIDReuseProcessIdentityFromSnapshot(replacementSnapshot)
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, replacementSnapshot, linuxWitnessProcessSnapshotAt("helper_before_owner_proof_release", helperPID, helperIdentity))
	evidence.PIDReuse.Assertions["same_pid_and_pgrp_reused"] = true
	evidence.PIDReuse.Assertions["replacement_start_time_advanced"] = true
	if absenceErr := platform.ProvePersistedProcessGroupAbsent(context.Background(), targetPID, targetIdentity); !errors.Is(absenceErr, platform.ErrPersistedProcessGroupUnproven) {
		t.Fatalf("numeric persisted-group absence proof with a live reused group = %v, want unresolved", absenceErr)
	}
	evidence.PIDReuse.Assertions["numeric_group_absence_proof_rejected_live_replacement"] = true
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker before owner proof release = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker before owner proof release = %v, want absent", err)
	}
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)

	if err := writeLinuxWitnessReleaseMarker(proofReleasePath); err != nil {
		t.Fatalf("release exact helper pre-proof barrier after literal PID reuse: %v", err)
	}
	evidence.PIDReuse.OwnerStopBeforeProofRelease = true
	if err := waitForLinuxWitnessMarker(proofResultPath, 45*time.Second); err != nil {
		t.Fatalf("wait for sanitized helper owner-stop proof result: %v", err)
	}
	proofResult, err := os.ReadFile(proofResultPath)
	if err != nil {
		t.Fatalf("read sanitized helper owner-stop proof result: %v", err)
	}
	proofCategory, proofResultValid := linuxNegativePIDReuseProofCategory(proofResult)
	if !proofResultValid || proofCategory != "identity_or_anchor_mismatch" {
		if !proofResultValid {
			evidence.PIDReuse.OwnerStopProofResult = "unexpected_or_malformed_result"
		} else {
			evidence.PIDReuse.OwnerStopProofResult = proofCategory
		}
		t.Fatalf("helper owner-stop proof result category = %q (valid=%t), want identity_or_anchor_mismatch", evidence.PIDReuse.OwnerStopProofResult, proofResultValid)
	}
	evidence.PIDReuse.OwnerStopProofResult = proofCategory
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("helper did not retain exact peer identity after anchor mismatch: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(replacement.pid, replacementIdentity); err != nil {
		t.Fatalf("replacement was harmed by original retained-pidfd proof attempt: %v", err)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("replacement pidfd group signal-0 after original owner proof: %v", err)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after original owner proof attempt = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after original owner proof attempt = %v, want absent", err)
	}
	evidence.PIDReuse.Assertions["target_user_code_absent_after_owner_proof"] = true
	proofJournal, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil {
		t.Fatalf("read original journal after helper proof failure: %v", err)
	}
	if !found {
		t.Fatal("original journal disappeared after helper proof failure")
	}
	assertLinuxNegativePIDReuseJournalUnresolved(t, proofJournal, key, targetPID, targetIdentity, helperPID, helperIdentity)
	if !proofJournal.Journal.RecoveryHelperScanSealEligible() {
		t.Fatal("helper proof failure made the original journal ineligible for exact-helper restart recovery")
	}
	evidence.PIDReuse.RecoveryHelperScanSealEligibleAfterProof = true
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(proofJournal))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("helper_after_anchor_mismatch", helperPID, helperIdentity),
		linuxWitnessProcessSnapshotAt("replacement_after_original_pidfd_proof", replacement.pid, replacementIdentity),
	)
	evidence.PIDReuse.Assertions["owner_proof_identity_mismatch"] = true
	evidence.PIDReuse.Assertions["helper_recovery_eligible_after_owner_proof"] = true
	evidence.PIDReuse.Assertions["replacement_alive_after_owner_proof"] = true
	evidence.PIDReuse.Assertions["journal_unreceipted_after_owner_proof"] = true
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)

	recoveryDaemon = startLinuxResumeBarrierDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, "", "slow", "recovery", map[string]string{
		linuxProductionContainmentWitnessEnvironment: "1",
	})
	recoveryDaemonPIDFD, recoveryDaemonIdentity, err = openLinuxNegativeWitnessProcessPIDFD(recoveryDaemon.command.Process.Pid)
	if err != nil {
		t.Fatalf("open exact restarted production daemon pidfd: %v", err)
	}
	recoveryEvent, err := waitForLinuxNegativeWitnessLogEvent(recoveryDaemon.stdout.Name(), []string{
		"recovered_input_process_stop_pending",
		"stop_recovered_process_failed",
		"stop_recovered_cancel_process_failed",
	}, 45*time.Second)
	if err != nil {
		t.Fatalf("restart did not expose its unresolved exact-process recovery attempt: %v", err)
	}
	if err := waitForLinuxWitnessMarker(recoveryAuthenticatedPath, 45*time.Second); err != nil {
		t.Fatalf("restarted daemon recovery did not reach the authenticated exact helper endpoint: %v", err)
	}
	authenticatedRequest, err := os.ReadFile(recoveryAuthenticatedPath)
	if err != nil {
		t.Fatalf("read authenticated helper recovery marker: %v", err)
	}
	expectedAuthenticatedRequest := fmt.Sprintf("operation=stop\ntarget_pid=%d\nhelper_pid=%d\nowner_loss_proof_category=%s\n", targetPID, helperPID, proofCategory)
	if string(authenticatedRequest) != expectedAuthenticatedRequest {
		t.Fatalf("authenticated helper recovery marker = %q, want exact target/helper PID and owner-loss proof category", authenticatedRequest)
	}
	evidence.PIDReuse.Assertions["restart_recovery_authenticated_helper_request"] = true
	evidence.PIDReuse.RecoveryStopFailureObserved = true
	evidence.PIDReuse.RecoveryStopFailureEvent = recoveryEvent
	if _, err := waitForLinuxNegativeWitnessLogEvent(recoveryDaemon.stdout.Name(), []string{"runtime_registered"}, 30*time.Second); err != nil {
		t.Fatalf("restarted production daemon did not complete registration after fail-closed recovery: %v", err)
	}
	evidence.PIDReuse.RecoveryRuntimeRegistered = true
	if err := assertLinuxWitnessProcessLive(recoveryDaemon.command.Process.Pid, recoveryDaemonIdentity); err != nil {
		t.Fatalf("restarted production daemon is not the exact live process: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(helperPID, helperIdentity); err != nil {
		t.Fatalf("exact helper peer was not retained after restarted-daemon recovery: %v", err)
	}
	if err := assertLinuxWitnessProcessLive(replacement.pid, replacementIdentity); err != nil {
		t.Fatalf("replacement was harmed by restarted production-daemon recovery: %v", err)
	}
	if err := unix.PidfdSendSignal(replacement.pidfd, 0, nil, unix.PIDFD_SIGNAL_PROCESS_GROUP); err != nil {
		t.Fatalf("replacement pidfd group signal-0 after restarted recovery: %v", err)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after restarted recovery = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after restarted recovery = %v, want absent", err)
	}
	evidence.PIDReuse.Assertions["target_user_code_absent_after_restart"] = true
	recoveredJournal, found, err := readLinuxWitnessJournalForKey(target.StateDir, key)
	if err != nil {
		t.Fatalf("read original journal after restarted production recovery: %v", err)
	}
	if !found {
		t.Fatal("original journal disappeared after restarted production recovery")
	}
	assertLinuxNegativePIDReuseJournalUnresolved(t, recoveredJournal, key, targetPID, targetIdentity, helperPID, helperIdentity)
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(recoveredJournal))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("helper_after_restart_recovery", helperPID, helperIdentity),
		linuxWitnessProcessSnapshotAt("replacement_after_restart_recovery", replacement.pid, replacementIdentity),
		linuxWitnessProcessSnapshotAt("restarted_daemon_after_recovery", recoveryDaemon.command.Process.Pid, recoveryDaemonIdentity),
	)
	evidence.PIDReuse.Assertions["restart_recovery_attempt_observed"] = true
	evidence.PIDReuse.Assertions["restart_runtime_registered_after_recovery"] = true
	evidence.PIDReuse.Assertions["journal_unreceipted_after_restart"] = true
	evidence.PIDReuse.Assertions["replacement_alive_after_restart"] = true
	evidence.PIDReuse.Assertions["helper_peer_identity_exact"] = true
	evidence.PIDReuse.Notes = append(evidence.PIDReuse.Notes,
		"the exact first-daemon SIGKILL was followed by the helper's post-reap/pre-proof pause; namespace-local ns_last_pid then allocated a replacement with the same PID, PGID, session, boot ID, and PID namespace but a later startTime",
		"the production helper's sanitized proof marker classified the retained original group authority as an identity/anchor mismatch; the exact helper identity and replacement remained live before and after the restarted production daemon's recovery attempt",
		"the restarted daemon's generic stop failure was accepted only after the helper atomically recorded the authenticated stop request with the original target/helper PIDs and identity-mismatch proof category",
		"the original run journal retained its exact process identity and Linux supervisor authority without a stop receipt or process-marker clear after both the helper proof failure and restart recovery",
	)
	writeLinuxNegativeWitnessEvidence(t, evidencePath, evidence)
	t.Logf("production literal PID reuse rejected: subject=%s target=%s helper=%s replacement=%s pidns=%d netns=%d owner_proof=%s restart_recovery=unresolved", metadata.Subject, targetIdentity, helperIdentity, replacementIdentity, privatePIDNamespace, privateNetworkNamespace, proofCategory)
}

type linuxNegativePIDReuseVictim struct {
	command  *exec.Cmd
	stdin    io.WriteCloser
	pidfd    int
	pid      int
	identity string
	reaped   bool
}

type linuxNegativePIDReuseIdentityParts struct {
	bootID            string
	pidNamespaceInode uint64
	pid               int
	startTime         uint64
}

func TestLinuxProductionPIDReuseVictim(t *testing.T) {
	if os.Getenv(linuxNegativePIDReuseVictimEnv) != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatalf("wait for test-owned replacement release: %v", err)
	}
}

func startLinuxNegativePIDReuseVictim() (*linuxNegativePIDReuseVictim, error) {
	testBinary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve nested victim test binary: %w", err)
	}
	command := exec.Command(testBinary, "-test.run=^TestLinuxProductionPIDReuseVictim$", "-test.timeout=180s")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create replacement victim hold pipe: %w", err)
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Env = setLinuxWitnessEnvironment(append([]string(nil), os.Environ()...), linuxNegativePIDReuseVictimEnv, "1")
	pidfd := -1
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL, PidFD: &pidfd}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start replacement victim: %w", err)
	}
	victim := &linuxNegativePIDReuseVictim{command: command, stdin: stdin, pidfd: pidfd, pid: command.Process.Pid}
	if pidfd < 0 {
		_ = stdin.Close()
		victim.stdin = nil
		waitErr := command.Wait()
		victim.reaped = true
		return nil, fmt.Errorf("required prerequisite unavailable: replacement victim launch did not retain a test-owned pidfd (wait error: %v)", waitErr)
	}
	identity, err := platform.ProcessIdentity(victim.pid)
	if err != nil {
		cleanupErr := victim.killAndReap(10 * time.Second)
		return nil, errors.Join(fmt.Errorf("read replacement victim identity: %w", err), cleanupErr)
	}
	victim.identity = identity
	snapshot := linuxWitnessProcessSnapshotAt("replacement_launch", victim.pid, identity)
	if !snapshot.Exists || snapshot.PGRP != int64(victim.pid) {
		cleanupErr := victim.killAndReap(10 * time.Second)
		return nil, errors.Join(fmt.Errorf("replacement victim process group = %#v, want live group leader", snapshot), cleanupErr)
	}
	return victim, nil
}

func (victim *linuxNegativePIDReuseVictim) killAndReap(timeout time.Duration) error {
	if victim == nil || victim.reaped {
		return nil
	}
	if victim.command == nil || victim.command.Process == nil || victim.pidfd < 0 {
		return errors.New("replacement cleanup lacks its test-owned pidfd and process handle")
	}
	signalErr := unix.PidfdSendSignal(victim.pidfd, unix.SIGKILL, nil, 0)
	if signalErr != nil && !errors.Is(signalErr, unix.ESRCH) {
		return fmt.Errorf("signal test-owned replacement pidfd: %w", signalErr)
	}
	if err := waitForLinuxNegativePIDFDExit(victim.pidfd, timeout); err != nil {
		return fmt.Errorf("wait for test-owned replacement pidfd exit: %w", err)
	}
	waitErr := victim.command.Wait()
	victim.reaped = true
	var cleanupErr error
	if victim.stdin != nil {
		if err := victim.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close replacement hold pipe: %w", err))
		}
		victim.stdin = nil
	}
	if victim.pidfd >= 0 {
		if err := unix.Close(victim.pidfd); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close test-owned replacement pidfd: %w", err))
		}
		victim.pidfd = -1
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("wait for test-owned replacement: %w", waitErr))
		}
	}
	return cleanupErr
}

func waitForLinuxNegativePIDFDExit(pidfd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		pollMillis := int((remaining + time.Millisecond - 1) / time.Millisecond)
		if pollMillis > 100 {
			pollMillis = 100
		}
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		count, err := unix.Poll(fds, pollMillis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("poll pidfd: %w", err)
		}
		if count > 0 {
			return nil
		}
	}
}

func validateLinuxNegativeDistinctPaths(paths []string) error {
	if len(paths) < 2 {
		return errors.New("at least two distinct marker paths are required")
	}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			return errors.New("marker paths must be absolute and non-empty")
		}
		clean := filepath.Clean(path)
		if clean != path {
			return errors.New("marker paths must already be clean absolute paths")
		}
		if _, ok := seen[clean]; ok {
			return errors.New("marker paths must be distinct")
		}
		seen[clean] = struct{}{}
	}
	return nil
}

func linuxNegativePIDReuseProcessIdentityFromSnapshot(snapshot linuxWitnessProcessSnapshot) *linuxNegativePIDReuseProcessIdentity {
	return &linuxNegativePIDReuseProcessIdentity{
		PID: snapshot.PID, Identity: snapshot.Identity, PGRP: snapshot.PGRP,
		Session: snapshot.Session, StartTime: snapshot.StartTime,
	}
}

func parseLinuxNegativePIDReuseIdentity(identity string) (linuxNegativePIDReuseIdentityParts, error) {
	parts := strings.Split(identity, ":")
	if len(parts) != 6 || parts[0] != "linux" || parts[1] != "v2" || parts[2] == "" {
		return linuxNegativePIDReuseIdentityParts{}, errors.New("identity is not a Linux v2 process identity")
	}
	namespace, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil || namespace == 0 {
		return linuxNegativePIDReuseIdentityParts{}, errors.New("identity PID namespace inode is invalid")
	}
	pid, err := strconv.Atoi(parts[4])
	if err != nil || pid <= 0 {
		return linuxNegativePIDReuseIdentityParts{}, errors.New("identity PID is invalid")
	}
	startTime, err := strconv.ParseUint(parts[5], 10, 64)
	if err != nil || startTime == 0 {
		return linuxNegativePIDReuseIdentityParts{}, errors.New("identity start time is invalid")
	}
	return linuxNegativePIDReuseIdentityParts{bootID: parts[2], pidNamespaceInode: namespace, pid: pid, startTime: startTime}, nil
}

func assertLinuxNegativePIDReuseIdentityReuse(originalIdentity, replacementIdentity string, pid int, namespace uint64, originalStartTime, replacementStartTime uint64) error {
	original, err := parseLinuxNegativePIDReuseIdentity(originalIdentity)
	if err != nil {
		return fmt.Errorf("parse original identity: %w", err)
	}
	replacement, err := parseLinuxNegativePIDReuseIdentity(replacementIdentity)
	if err != nil {
		return fmt.Errorf("parse replacement identity: %w", err)
	}
	if original.bootID != replacement.bootID || original.pidNamespaceInode != namespace || replacement.pidNamespaceInode != namespace ||
		original.pid != pid || replacement.pid != pid || original.startTime != originalStartTime || replacement.startTime != replacementStartTime ||
		replacement.startTime <= original.startTime {
		return fmt.Errorf("identity tuple changed outside the required later startTime: original=(boot=%s pidns=%d pid=%d start=%d) replacement=(boot=%s pidns=%d pid=%d start=%d)", original.bootID, original.pidNamespaceInode, original.pid, original.startTime, replacement.bootID, replacement.pidNamespaceInode, replacement.pid, replacement.startTime)
	}
	return nil
}

func assertLinuxNegativePIDReuseJournalUnresolved(t *testing.T, file linuxWitnessJournalFile, key state.RunKey, targetPID int, targetIdentity string, helperPID int, helperIdentity string) {
	t.Helper()
	if file.Journal.Key() != key || file.Journal.ContainmentAuthority == nil || file.Journal.ContainmentHandoff != nil ||
		file.Journal.ContainmentAuthority.StopReceipt != nil || !file.Journal.HasProcessDetails() ||
		file.Journal.PID != targetPID || file.Journal.ProcessIdentity != targetIdentity {
		t.Fatalf("production journal did not retain unresolved original process authority: key_match=%t authority=%t handoff=%t receipt=%t process_details=%t target=(%d,%q)", file.Journal.Key() == key, file.Journal.ContainmentAuthority != nil, file.Journal.ContainmentHandoff != nil, file.Journal.ContainmentAuthority != nil && file.Journal.ContainmentAuthority.StopReceipt != nil, file.Journal.HasProcessDetails(), file.Journal.PID, file.Journal.ProcessIdentity)
	}
	gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity := linuxWitnessOwnerIDs(file.Journal)
	if gotTargetPID != targetPID || gotTargetIdentity != targetIdentity || gotHelperPID != helperPID || gotHelperIdentity != helperIdentity {
		t.Fatalf("production journal owner identity changed: target=(%d,%q) helper=(%d,%q), want exact original target=(%d,%q) helper=(%d,%q)", gotTargetPID, gotTargetIdentity, gotHelperPID, gotHelperIdentity, targetPID, targetIdentity, helperPID, helperIdentity)
	}
}

func linuxNegativePIDReuseProofCategory(contents []byte) (string, bool) {
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "proof_attempted" || !strings.HasPrefix(lines[1], "category=") {
		return "", false
	}
	switch strings.TrimPrefix(lines[1], "category=") {
	case "identity_or_anchor_mismatch", "other_error":
		return strings.TrimPrefix(lines[1], "category="), true
	default:
		return "", false
	}
}

func waitForLinuxNegativeWitnessLogEvent(path string, events []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			for _, event := range events {
				if strings.Contains(string(contents), event) {
					return event, nil
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read restarted daemon lifecycle log: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", fmt.Errorf("restarted daemon lifecycle events %q did not appear within %s", events, timeout)
}

func linuxNegativePIDReuseLogDigest(process *productionDaemonWitnessProcess, stdout bool) string {
	if process == nil {
		return "unavailable"
	}
	file := process.stderr
	if stdout {
		file = process.stdout
	}
	if file == nil {
		return "unavailable"
	}
	contents, err := os.ReadFile(file.Name())
	if err != nil {
		return "unavailable"
	}
	return fmt.Sprintf("%d bytes sha256:%s", len(contents), linuxWitnessHashBytes(contents))
}

func openLinuxNegativeWitnessProcessPIDFD(pid int) (int, string, error) {
	if pid <= 0 {
		return -1, "", errors.New("process pid must be positive")
	}
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		return -1, "", fmt.Errorf("capture process %d identity: %w", pid, err)
	}
	pidfd, err := openLinuxWitnessPIDFD(pid, identity)
	if err != nil {
		return -1, "", err
	}
	return pidfd, identity, nil
}

func killLinuxNegativeProductionDaemon(process *productionDaemonWitnessProcess, pidfd int) linuxWitnessExitStatus {
	status := linuxWitnessExitStatus{RequestedSignal: "SIGKILL", ExitCode: -1}
	if process == nil || process.waited || process.command == nil || process.command.Process == nil {
		status.Error = "production daemon process is unavailable"
		return status
	}
	if pidfd < 0 {
		status.Error = "test-owned production daemon pidfd is unavailable; namespace teardown remains the bounded fallback"
		return status
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		status.Error = fmt.Errorf("signal exact production daemon pidfd: %w", err).Error()
	}
	if err := waitForLinuxNegativePIDFDExit(pidfd, 15*time.Second); err != nil {
		status.Error = errors.Join(errorFromString(status.Error), fmt.Errorf("wait for exact production daemon pidfd exit: %w", err)).Error()
		return status
	}
	waitErr := process.command.Wait()
	process.waited = true
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			status.Error = errors.Join(errorFromString(status.Error), waitErr).Error()
		}
	}
	for _, file := range []*os.File{process.stdout, process.stderr} {
		if file != nil {
			if err := file.Close(); err != nil {
				status.Error = errors.Join(errorFromString(status.Error), err).Error()
			}
		}
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

func errorFromString(value string) error {
	if value == "" {
		return nil
	}
	return errors.New(value)
}

func waitForLinuxNegativeWitnessGroupAbsent(pid int, identity string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = platform.ProvePersistedProcessGroupAbsent(context.Background(), pid, identity)
		if lastErr == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("original process group %d did not reach an identity-bound absence proof: %w", pid, lastErr)
}

func killLinuxNegativeWitnessHelperByPIDFD(pid int, identity string, timeout time.Duration) error {
	if pid <= 1 || identity == "" {
		return errors.New("exact helper identity is required for pidfd cleanup")
	}
	pidfd, err := openLinuxWitnessPIDFD(pid, identity)
	if err != nil {
		return fmt.Errorf("open exact helper pidfd after original group proof: %w", err)
	}
	defer unix.Close(pidfd)
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("signal exact helper pidfd after original group proof: %w", err)
	}
	deadline := time.Now().Add(timeout)
	if err := waitForLinuxWitnessIdentityGone(pid, identity, time.Until(deadline)); err != nil {
		return fmt.Errorf("wait for exact helper exit after pidfd cleanup: %w", err)
	}
	for time.Now().Before(deadline) {
		var waitStatus unix.WaitStatus
		waitedPID, waitErr := unix.Wait4(pid, &waitStatus, unix.WNOHANG, nil)
		if errors.Is(waitErr, unix.ECHILD) || errors.Is(waitErr, unix.ESRCH) || waitedPID == pid {
			return nil
		}
		if waitErr != nil {
			return fmt.Errorf("reap exact helper after pidfd exit: %w", waitErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("exact helper did not become waitable after bounded pidfd cleanup")
}

func linuxNegativeProductionDaemonExitFence(process *productionDaemonWitnessProcess, pidfd int) error {
	if process == nil {
		if pidfd >= 0 {
			return errors.New("production daemon pidfd has no owning command")
		}
		return nil
	}
	if process.command == nil || process.command.Process == nil || process.command.Process.Pid <= 0 {
		return errors.New("production daemon command ownership is unavailable")
	}
	if pidfd < 0 {
		return errors.New("exact production daemon pidfd is unavailable")
	}
	if !process.waited || process.command.ProcessState == nil {
		return errors.New("production daemon command has not crossed its Wait ownership fence")
	}
	if err := waitForLinuxNegativePIDFDExit(pidfd, time.Second); err != nil {
		return fmt.Errorf("exact production daemon pidfd has not signaled exit: %w", err)
	}
	return nil
}

func cleanupLinuxProductionPIDReuseWitness(
	t *testing.T,
	evidence *linuxNegativeWitnessEvidence,
	target linuxWitnessTarget,
	targetPrepared bool,
	firstDaemon **productionDaemonWitnessProcess,
	firstDaemonPIDFD int,
	recoveryDaemon **productionDaemonWitnessProcess,
	recoveryDaemonPIDFD int,
	replacement *linuxNegativePIDReuseVictim,
	key *state.RunKey,
	targetPID *int,
	targetIdentity *string,
	helperPID *int,
	helperIdentity *string,
	proofPaths []string,
) []error {
	t.Helper()
	var cleanupErrors []error
	if *recoveryDaemon != nil && !(*recoveryDaemon).waited {
		status := killLinuxNegativeProductionDaemon(*recoveryDaemon, recoveryDaemonPIDFD)
		evidence.PIDReuse.RecoveryDaemonShutdown = &status
		if status.Error != "" || !status.Signaled || status.Signal != "SIGKILL" {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("stop exact restarted production daemon: %#v", status))
		}
	}
	if *firstDaemon != nil && !(*firstDaemon).waited {
		status := killLinuxNegativeProductionDaemon(*firstDaemon, firstDaemonPIDFD)
		if evidence.Crash == nil {
			evidence.Crash = &status
			evidence.PIDReuse.CrashStatuses = append(evidence.PIDReuse.CrashStatuses, linuxNegativePIDReuseCrashStatus{Stage: "first_daemon_owner_loss_during_cleanup", Status: status})
		}
		if status.Error != "" || !status.Signaled || status.Signal != "SIGKILL" {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("SIGKILL first production daemon during cleanup: %#v", status))
		}
	}
	firstDaemonExitFenced := true
	if err := linuxNegativeProductionDaemonExitFence(*firstDaemon, firstDaemonPIDFD); err != nil {
		firstDaemonExitFenced = false
		cleanupErrors = append(cleanupErrors, fmt.Errorf("first production daemon exit/ownership fence: %w", err))
	}
	recoveryDaemonExitFenced := true
	if err := linuxNegativeProductionDaemonExitFence(*recoveryDaemon, recoveryDaemonPIDFD); err != nil {
		recoveryDaemonExitFenced = false
		cleanupErrors = append(cleanupErrors, fmt.Errorf("restarted production daemon exit/ownership fence: %w", err))
	}
	if !firstDaemonExitFenced || !recoveryDaemonExitFenced {
		// A live writer could still clear the original journal. Keep both the
		// replacement and helper proof gate untouched; PID 1 teardown owns fallback cleanup.
		cleanupErrors = append(cleanupErrors, errors.New("refusing replacement cleanup or helper proof-gate release until both exact production daemon pidfd exit/ownership fences pass; private namespace teardown must clean the remaining processes"))
		return cleanupErrors
	}
	if targetPrepared && key.RunID == "" {
		files, err := readLinuxWitnessJournals(target.StateDir)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("locate exact production PID-reuse journal during cleanup: %w", err))
		} else {
			var owners []linuxWitnessJournalFile
			for _, file := range files {
				journal := file.Journal
				if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil || journal.HasProcessDetails() {
					owners = append(owners, file)
				}
			}
			if len(owners) == 1 {
				*key = owners[0].Journal.Key()
			} else if len(owners) > 1 {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failure cleanup found %d possible owner journals; exact helper pidfd cleanup is refused", len(owners)))
			}
		}
	}
	if targetPrepared && key.RunID != "" {
		file, found, err := readLinuxWitnessJournalForKey(target.StateDir, *key)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("read exact journal during PID-reuse cleanup: %w", err))
		} else if found {
			journalTargetPID, journalTargetIdentity, journalHelperPID, journalHelperIdentity := linuxWitnessOwnerIDs(file.Journal)
			if *targetPID == 0 {
				*targetPID, *targetIdentity, *helperPID, *helperIdentity = journalTargetPID, journalTargetIdentity, journalHelperPID, journalHelperIdentity
			} else if journalTargetPID != *targetPID || journalTargetIdentity != *targetIdentity || journalHelperPID != *helperPID || journalHelperIdentity != *helperIdentity {
				cleanupErrors = append(cleanupErrors, errors.New("PID-reuse cleanup journal owner identity changed; refusing to kill a numeric replacement"))
			}
			evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
		}
	}
	if replacement != nil && !replacement.reaped {
		evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, linuxWitnessProcessSnapshotAt("replacement_before_pidfd_cleanup", replacement.pid, replacement.identity))
		if err := replacement.killAndReap(15 * time.Second); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("stop test-owned replacement pidfd: %w", err))
		}
	}
	if len(proofPaths) >= 2 {
		if err := writeLinuxWitnessReleaseMarker(proofPaths[1]); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("release helper pre-proof barrier during cleanup: %w", err))
		} else if evidence.PIDReuse.OwnerStopBeforeProofReached {
			evidence.PIDReuse.OwnerStopBeforeProofRelease = true
		}
	}
	if len(proofPaths) >= 3 && evidence.PIDReuse.OwnerStopBeforeProofReached && evidence.PIDReuse.OwnerStopProofResult == "" {
		if waitErr := waitForLinuxWitnessMarker(proofPaths[2], 5*time.Second); waitErr == nil {
			if contents, readErr := os.ReadFile(proofPaths[2]); readErr == nil {
				if category, valid := linuxNegativePIDReuseProofCategory(contents); valid {
					evidence.PIDReuse.OwnerStopProofResult = category
				} else {
					evidence.PIDReuse.OwnerStopProofResult = "unexpected_or_malformed_result"
				}
			}
		}
	}
	groupAbsent := false
	if *targetPID > 1 && *targetIdentity != "" {
		if err := waitForLinuxNegativeWitnessGroupAbsent(*targetPID, *targetIdentity, 20*time.Second); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("prove original target group physically absent before helper cleanup: %w", err))
		} else {
			groupAbsent = true
		}
	}
	if *helperPID > 1 && *helperIdentity != "" {
		if !groupAbsent {
			cleanupErrors = append(cleanupErrors, errors.New("refusing helper cleanup before the original process group has a physical absence proof"))
		} else {
			evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, linuxWitnessProcessSnapshotAt("helper_before_exact_pidfd_cleanup", *helperPID, *helperIdentity))
			if err := killLinuxNegativeWitnessHelperByPIDFD(*helperPID, *helperIdentity, 15*time.Second); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
	}
	return cleanupErrors
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

func runLinuxWrongHelloSessionWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot string) {
	t.Helper()
	firedPath := filepath.Join(caseRoot, "wrong-session-hello.json")
	releasePath := filepath.Join(caseRoot, "wrong-session-hello.release")
	t.Setenv(linuxProductionContainmentWitnessEnvironment, "1")
	t.Setenv(linuxWrongHelloSessionInjectionEnv, "1")
	t.Setenv(linuxWrongHelloSessionFiredEnv, firedPath)
	t.Setenv(linuxWrongHelloSessionReleaseEnv, releasePath)
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, "wrong-hello-session")
	evidence := linuxNegativeWitnessEvidence{
		Version: 1, Case: "wrong_hello_session_rejected_before_resume_owner_sigkill",
		Status: "initialized", Metadata: metadata, CaseRoot: caseRoot, Assertions: map[string]bool{},
	}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	var daemon *productionDaemonWitnessProcess
	var targetPID, helperPID int
	var targetIdentity, helperIdentity string
	defer func() {
		if daemon != nil && !daemon.waited {
			daemon.stop(t)
		}
		_ = os.WriteFile(releasePath, []byte("release\n"), 0o600)
		cleanupLinuxNegativeWitnessProcess(t, targetPID, targetIdentity, 0, helperPID, helperIdentity)
	}()

	daemon = startProductionDaemonWitness(t, daemonBinary, target.ConfigPath, caseRoot, environment.enrollmentToken, "slow", "first")
	operator := newOperator(t, environment)
	profile, workspace := profileAndWorkspace(t, target.ConfigPath)
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, "linux-wrong-hello-session", "slow")
	hello, err := waitForLinuxWrongHelloSessionMarker(firedPath, 45*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	actual, transmitted := hello.Actual, hello.Transmitted
	if actual.TargetPID <= 0 || actual.TargetIdentity == "" || actual.TargetPGRP != int64(actual.TargetPID) || actual.TargetSession <= 0 || actual.TargetStartTime == 0 {
		t.Fatalf("actual captured helper anchor is incomplete: %#v", actual)
	}
	if transmitted.TargetPID != actual.TargetPID || transmitted.TargetIdentity != actual.TargetIdentity ||
		transmitted.TargetPGRP != actual.TargetPGRP || transmitted.TargetStartTime != actual.TargetStartTime ||
		transmitted.TargetSession <= 0 || transmitted.TargetSession == int64(transmitted.TargetPID) || transmitted.TargetSession == actual.TargetSession {
		t.Fatalf("transmitted hello anchor = %#v, want only a plausible wrong session from %#v", transmitted, actual)
	}
	targetPID, targetIdentity = actual.TargetPID, actual.TargetIdentity
	helperPID, helperIdentity = hello.SupervisorPID, hello.SupervisorIdentity
	liveTargetIdentity, err := platform.ProcessIdentity(targetPID)
	if err != nil || liveTargetIdentity != targetIdentity {
		t.Fatalf("actual target identity = %q, error=%v, want captured %q", liveTargetIdentity, err, targetIdentity)
	}
	targetBefore := linuxWitnessProcessSnapshotAt("target_before_owner_sigkill", targetPID, targetIdentity)
	helperBefore := linuxWitnessProcessSnapshotAt("helper_before_owner_sigkill", helperPID, helperIdentity)
	if !targetBefore.Exists || !helperBefore.Exists || targetBefore.PGRP != actual.TargetPGRP ||
		targetBefore.Session != actual.TargetSession || targetBefore.StartTime != actual.TargetStartTime {
		t.Fatalf("actual helper anchor differs from live procfs target: marker=%#v procfs=%#v helper=%#v", actual, targetBefore, helperBefore)
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker before resume = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker before resume = %v, want absent", err)
	}
	file, err := waitForLinuxWitnessJournal(target.StateDir, "durable wrong-session precommit handoff", 20*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.ContainmentHandoff != nil && value.Journal.ContainmentScans != nil && value.Journal.ContainmentScans.Mirror.UnprovenSequence > 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if file.Journal.HasProcessDetails() {
		t.Fatal("wrong-session launch crossed the durable commit/process-marker boundary")
	}
	if file.Journal.ContainmentScans == nil || file.Journal.ContainmentScans.Mirror.UnprovenSequence == 0 {
		t.Fatal("wrong-session hello did not leave a durable initial mirror scan failure")
	}
	if file.Journal.ContainmentAuthority != nil && file.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatal("wrong-session precommit launch already has a stop receipt")
	}
	if file.Journal.ContainmentHandoff != nil && file.Journal.ContainmentHandoff.StopReceipt != nil {
		t.Fatal("wrong-session precommit handoff already has a stop receipt")
	}
	if file.Journal.ContainmentHandoff == nil || file.Journal.ContainmentHandoff.SupervisorPID != helperPID ||
		file.Journal.ContainmentHandoff.SupervisorIdentity != helperIdentity || file.Journal.ContainmentHandoff.TargetPID != targetPID ||
		file.Journal.ContainmentHandoff.TargetIdentity != targetIdentity {
		t.Fatal("wrong-session failure did not retain the exact bound precommit handoff")
	}
	evidence.Status = "hello_rejected_precommit_before_owner_crash"
	evidence.HelperHello = &hello
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, targetBefore, helperBefore)
	evidence.Assertions["helper_injection_fired"] = true
	evidence.Assertions["actual_target_anchor_matches_procfs"] = true
	evidence.Assertions["transmitted_session_wrong_but_structurally_plausible"] = true
	evidence.Assertions["target_user_code_never_ran_before_crash"] = true
	evidence.Assertions["no_commit_or_process_marker"] = !file.Journal.HasProcessDetails()
	evidence.Assertions["initial_mirror_scan_rejected_hello"] = file.Journal.ContainmentScans.Mirror.UnprovenSequence > 0
	evidence.Assertions["no_stop_receipt_before_crash"] = true
	evidence.Assertions["exact_bound_precommit_handoff_retained"] = true
	evidence.Notes = []string{
		"The production helper captured the actual target anchor unchanged and sent a separately recorded plausible wrong TargetSession; the daemon's initial procfs mirror scan rejected it before commit, lease arm, or resume.",
		"This subject did not create PID reuse: the target startTime was not mutated, and no controlled PID allocator or private namespace was used.",
	}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	crash := killLinuxProductionDaemon(daemon)
	if crash.RequestedSignal != "SIGKILL" || !crash.Signaled || crash.Signal != "SIGKILL" {
		t.Fatalf("daemon crash status = %#v, want SIGKILL", crash)
	}
	evidence.Crash = &crash
	if err := os.WriteFile(releasePath, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release helper wrong-session crash barrier: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(targetPID, targetIdentity, 20*time.Second); err != nil {
		t.Fatalf("helper did not stop exact precommit target after owner SIGKILL: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 20*time.Second); err != nil {
		t.Fatalf("wrong-session helper did not exit after owner SIGKILL: %v", err)
	}
	crashed, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil {
		t.Fatalf("read wrong-session handoff after daemon SIGKILL: %v", err)
	}
	if !found || crashed.Journal.ContainmentHandoff == nil || crashed.Journal.ContainmentHandoff.StopReceipt != nil ||
		crashed.Journal.HasProcessDetails() || !crashed.Journal.ContainmentUnproven || crashed.Journal.ContainmentScans == nil ||
		crashed.Journal.ContainmentScans.Mirror.UnprovenSequence == 0 {
		t.Fatal("wrong-session handoff was cleared, receipted, or committed before exact precommit abort proof")
	}
	evidence.Status = "owner_crashed_unresolved_until_exact_abort_proof"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(crashed))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_helper_precommit_abort", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_owner_sigkill", helperPID, helperIdentity))
	evidence.Assertions["owner_sigkill_observed"] = true
	evidence.Assertions["target_and_helper_absent_before_clear"] = true
	evidence.Assertions["unreceipted_precommit_handoff_retained_until_proof"] = true
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

	handoff := crashed.Journal.ContainmentHandoff.Clone()
	proof, err := platform.ProvePreparedSupervisorAborted(handoff, func() error { return nil })
	if err != nil || !proof.ValidFor(handoff) {
		t.Fatalf("exact precommit abort proof = valid:%t error:%v", err == nil && proof.ValidFor(handoff), err)
	}
	afterProof, stillFound, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil || !stillFound {
		t.Fatalf("read durable journal after exact physical abort proof: found=%t error=%v", stillFound, err)
	}
	if !afterProof.Journal.ContainmentUnproven || afterProof.Journal.ContainmentHandoff == nil ||
		afterProof.Journal.ContainmentHandoff.StopReceipt != nil || afterProof.Journal.HasProcessDetails() ||
		afterProof.Journal.ContainmentScans == nil || afterProof.Journal.ContainmentScans.Mirror.UnprovenSequence == 0 {
		t.Fatal("exact physical absence proof converted the failed initial scan into a receipt or clear")
	}
	if _, err := os.Stat(target.TargetMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target user-code marker after exact abort proof = %v, want absent", err)
	}
	if _, err := os.Stat(target.DescendantMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant marker after exact abort proof = %v, want absent", err)
	}
	evidence.Status = "physical_abort_proven_initial_scan_failure_retained"
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(afterProof))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots,
		linuxWitnessProcessSnapshotAt("target_after_exact_abort_proof", targetPID, targetIdentity),
		linuxWitnessProcessSnapshotAt("helper_after_exact_abort_proof", helperPID, helperIdentity))
	evidence.Assertions["exact_physical_abort_proof_valid"] = true
	evidence.Assertions["failed_initial_scan_remains_unresolved_after_abort_proof"] = afterProof.Journal.ContainmentUnproven &&
		afterProof.Journal.ContainmentScans.Mirror.UnprovenSequence > 0
	evidence.Assertions["no_commit_receipt_or_clear"] = !afterProof.Journal.HasProcessDetails() &&
		afterProof.Journal.ContainmentHandoff != nil && afterProof.Journal.ContainmentHandoff.StopReceipt == nil
	evidence.Assertions["target_user_code_never_ran"] = true
	evidence.Notes = append(evidence.Notes, "The helper and target were proven absent by the exact precommit abort proof, but the initial mirror scan's durable unproven sequence remains fail-closed; no receipt or journal clear was minted from physical absence.")
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)
	_ = task
}

func waitForLinuxWrongHelloSessionMarker(path string, timeout time.Duration) (linuxNegativeHelloSessionWitness, error) {
	deadline := time.Now().Add(timeout)
	for {
		if !time.Now().Before(deadline) {
			break
		}
		contents, err := os.ReadFile(path)
		if err == nil {
			var hello linuxNegativeHelloSessionWitness
			if json.Unmarshal(contents, &hello) == nil && linuxWrongHelloSessionMarkerComplete(hello) && time.Now().Before(deadline) {
				return hello, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return linuxNegativeHelloSessionWitness{}, fmt.Errorf("read helper wrong-session marker: %w", err)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > 25*time.Millisecond {
			remaining = 25 * time.Millisecond
		}
		time.Sleep(remaining)
	}
	return linuxNegativeHelloSessionWitness{}, fmt.Errorf("helper wrong-session marker %q did not contain a complete valid payload within %s", path, timeout)
}

func linuxWrongHelloSessionMarkerComplete(hello linuxNegativeHelloSessionWitness) bool {
	actual, transmitted := hello.Actual, hello.Transmitted
	return actual.TargetPID > 0 && actual.TargetIdentity != "" && actual.TargetPGRP > 0 && actual.TargetSession > 0 && actual.TargetStartTime > 0 &&
		transmitted.TargetPID > 0 && transmitted.TargetIdentity != "" && transmitted.TargetPGRP > 0 && transmitted.TargetSession > 0 && transmitted.TargetStartTime > 0 &&
		hello.SupervisorPID > 0 && hello.SupervisorIdentity != ""
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
		t.Fatalf("setsid escape was not retained fail-closed: %#v", linuxWitnessJournalSnapshotFromFile(after))
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
		t.Fatalf("scan-failure state was not retained fail-closed: %#v", linuxWitnessJournalSnapshotFromFile(after))
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

func runLinuxHelperScanCrashWitness(t *testing.T, environment e2eEnvironment, daemonBinary string, metadata linuxNegativeWitnessMetadata, caseRoot string, pending bool) {
	t.Helper()
	triggerPath := filepath.Join(caseRoot, "scan.trigger")
	firedPath := filepath.Join(caseRoot, "scan.fired")
	releasePath := filepath.Join(caseRoot, "scan.release")
	mode := "children_after_initial"
	caseName := "helper_scan_failure_sigkill_before_cancel"
	if pending {
		mode = linuxScanPendingMode
		caseName = "helper_pending_scan_sigkill"
		t.Setenv(linuxScanPendingReleaseEnv, releasePath)
	}
	t.Setenv("SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS", "1")
	t.Setenv(linuxScanFailureModeEnv, mode)
	t.Setenv(linuxScanFailureTriggerEnv, triggerPath)
	t.Setenv(linuxScanFailureFiredEnv, firedPath)
	target := prepareLinuxWitnessTarget(t, environment, caseRoot, caseName)
	if err := os.WriteFile(filepath.Join(caseRoot, "linux-containment-target.sh"), []byte(linuxNegativeScanFailureTargetScript(target.TargetMarker, target.DescendantMarker)), 0o700); err != nil {
		t.Fatalf("write helper scan crash target: %v", err)
	}
	evidence := linuxNegativeWitnessEvidence{Version: 1, Case: caseName, Status: "initialized", Metadata: metadata, CaseRoot: caseRoot, Assertions: map[string]bool{}}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)

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
	task := submit(t, operator, daemonRun{profile: profile, workspace: workspace}, caseName, "slow")
	file, err := waitForLinuxWitnessJournal(target.StateDir, "helper scan crash authority", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		return value.Journal.LocalState == "running" && value.Journal.ContainmentAuthority != nil && value.Journal.ContainmentScans != nil
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
	beforeTarget := linuxWitnessProcessSnapshotAt("target_before_helper_sigkill", targetPID, targetIdentity)
	beforeChurn := linuxWitnessProcessSnapshotAt("same_group_descendant_before_helper_sigkill", churnPID, "")
	assertLinuxWitnessSameGroup(t, beforeTarget, beforeChurn)
	if err := os.WriteFile(triggerPath, []byte("trigger\n"), 0o600); err != nil {
		t.Fatalf("write helper scan trigger: %v", err)
	}
	if err := waitForLinuxWitnessMarker(firedPath, 20*time.Second); err != nil {
		daemon.stop(t)
		t.Fatalf("wait for helper scan injection marker: %v", err)
	}
	if pending {
		file, err = waitForLinuxWitnessJournal(target.StateDir, "durable helper pending scan", 20*time.Second, func(value linuxWitnessJournalFile) bool {
			scans := value.Journal.ContainmentScans
			return scans != nil && !value.Journal.ContainmentUnproven && scans.Helper.PendingSequence > scans.Helper.CompletedSequence && scans.Helper.UnprovenSequence == 0
		})
	} else {
		file, err = waitForLinuxWitnessJournal(target.StateDir, "latched helper-only scan failure", 20*time.Second, func(value linuxWitnessJournalFile) bool {
			scans := value.Journal.ContainmentScans
			return scans != nil && value.Journal.ContainmentUnproven && scans.Helper.UnprovenSequence > 0 && scans.Helper.FailureKind == "descendant_scan_failure"
		})
	}
	if err != nil {
		daemon.stop(t)
		t.Fatal(err)
	}
	if err := killLinuxWitnessProcess(t, helperPID, helperIdentity); err != nil {
		t.Fatalf("SIGKILL exact containment helper: %v", err)
	}
	crash := linuxWitnessExitStatus{RequestedSignal: "SIGKILL", Signaled: true, Signal: "SIGKILL", ExitCode: -1}
	if err := waitForLinuxWitnessIdentityGone(helperPID, helperIdentity, 15*time.Second); err != nil {
		t.Fatalf("wait for exact helper exit: %v", err)
	}
	// The helper is deliberately killed before cancellation. The surviving
	// daemon mirror must force-stop the original group but may not turn helper
	// failure or its unacknowledged scan into a clean receipt.
	cancelTask(t, operator, task.TaskID)
	condition, err := waitForLinuxWitnessJournal(target.StateDir, "mirror force-stop without scan receipt", 45*time.Second, func(value linuxWitnessJournalFile) bool {
		journal := value.Journal
		if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt != nil || !journal.HasProcessDetails() {
			return false
		}
		if pending {
			return journal.ContainmentScans != nil && journal.ContainmentScans.Helper.PendingSequence > journal.ContainmentScans.Helper.CompletedSequence
		}
		return journal.ContainmentUnproven && journal.ContainmentScans != nil && journal.ContainmentScans.Helper.UnprovenSequence > 0
	})
	if err != nil {
		daemon.stop(t)
		t.Fatalf("wait for unresolved mirror takeover state: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(churnPID, "", 20*time.Second); err != nil {
		daemon.stop(t)
		t.Fatalf("mirror did not force-stop the live original-group descendant: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(targetPID, targetIdentity, 20*time.Second); err != nil {
		daemon.stop(t)
		t.Fatalf("original-group target remains after mirror takeover: %v", err)
	}
	after, found, err := readLinuxWitnessJournalForKey(target.StateDir, file.Journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !found || after.Journal.ContainmentAuthority == nil || after.Journal.ContainmentAuthority.StopReceipt != nil || !after.Journal.HasProcessDetails() {
		t.Fatalf("helper crash minted a receipt or cleared its marker: found=%v journal=%#v", found, linuxWitnessJournalSnapshotFromFile(after))
	}
	if pending {
		if after.Journal.ContainmentScans == nil || after.Journal.ContainmentScans.Helper.PendingSequence == 0 {
			t.Fatalf("pending helper scan disappeared after helper crash: %#v", linuxWitnessJournalSnapshotFromFile(after).ContainmentScans)
		}
	} else if !after.Journal.ContainmentUnproven || after.Journal.ContainmentScans == nil || after.Journal.ContainmentScans.Helper.UnprovenSequence == 0 {
		t.Fatalf("helper scan failure was not retained after helper crash: %#v", linuxWitnessJournalSnapshotFromFile(after))
	}
	afterTarget := linuxWitnessProcessSnapshotAt("target_after_mirror_force_stop", targetPID, targetIdentity)
	afterChurn := linuxWitnessProcessSnapshotAt("same_group_descendant_after_mirror_force_stop", churnPID, "")
	evidence.Status = "unresolved"
	evidence.Crash = &crash
	evidence.JournalSnapshots = append(evidence.JournalSnapshots, linuxWitnessJournalSnapshotFromFile(file), linuxWitnessJournalSnapshotFromFile(condition), linuxWitnessJournalSnapshotFromFile(after))
	evidence.ProcessSnapshots = append(evidence.ProcessSnapshots, beforeTarget, beforeChurn, afterTarget, afterChurn,
		linuxWitnessProcessSnapshotAt("helper_after_sigkill", helperPID, helperIdentity))
	evidence.Assertions["helper_died_before_cancel"] = true
	evidence.Assertions["original_group_descendant_live_before_kill"] = beforeChurn.Exists && beforeChurn.PGRP == beforeTarget.PGRP
	evidence.Assertions["mirror_force_stopped_original_group"] = (!afterTarget.Exists || afterTarget.State == "Z") && (!afterChurn.Exists || afterChurn.State == "Z")
	evidence.Assertions["no_stop_receipt"] = after.Journal.ContainmentAuthority.StopReceipt == nil
	evidence.Assertions["no_clear"] = after.Journal.HasProcessDetails()
	evidence.Assertions["unresolved_scan_retained"] = after.Journal.ContainmentUnproven || (after.Journal.ContainmentScans != nil && after.Journal.ContainmentScans.Helper.PendingSequence != 0)
	if pending {
		evidence.Assertions["pending_scan_crash"] = true
	} else {
		evidence.Assertions["helper_only_failure_latched"] = after.Journal.ContainmentUnproven && after.Journal.ContainmentScans.Helper.UnprovenSequence > 0
	}
	evidence.RawOutput = linuxWitnessLogs(daemon, nil)
	evidence.Notes = []string{"The helper was SIGKILLed after its scan failure or durable pre-scan fence and before task cancellation; the live daemon mirror force-stopped the original pidfd group but refused a receipt and journal clear."}
	writeLinuxNegativeWitnessEvidence(t, target.EvidencePath, evidence)
	daemon.stop(t)
}

func linuxNegativeSetsidTargetScript(targetMarker, descendantMarker string) string {
	return "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' \"$$\" > " + linuxWitnessShellQuote(targetMarker) + "\n" +
		"setsid sh -c 'printf \"%s\\n\" \"$$\" > \"$1\"; trap \"\" TERM; while :; do sleep 1; done' sh " + linuxWitnessShellQuote(descendantMarker) + " &\n" +
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
