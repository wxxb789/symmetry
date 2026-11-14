package omp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	nativeAcceptanceModel             = "tencent-litellm/deepseek-v4.1-flash-fast"
	nativeAcceptanceTimeout           = 3 * time.Minute
	nativeAcceptanceStopTimeout       = 30 * time.Second
	nativeAcceptanceHelperExit        = 73
	nativeAcceptanceResourceID        = "00000000-0000-4000-8000-000000000307"
	nativeAcceptanceFirstArtifact     = "omp-native-first.txt"
	nativeAcceptanceResumeArtifact    = "omp-native-resume.txt"
	nativeAcceptanceHandoffArtifact   = "omp-native-handoff.txt"
	nativeAcceptanceCancelledArtifact = "omp-native-cancelled.txt"
	nativeAcceptanceToolTimeout       = 45 * time.Second
)

// TestNativeAcceptance submits real, bounded repository turns to the approved
// relay. Each platform must run this test independently. Its fsynced local
// witness journal is NOT the App/Control journal, an outbox, or evidence of
// production command acknowledgement recovery. Native prices and billing stay
// unknown even if an operator's catalog contains zero-valued prices.
func TestNativeAcceptance(t *testing.T) {
	cfg := nativeAcceptanceConfiguration(t)
	root, err := os.MkdirTemp(cfg.evidenceRoot, runtime.GOOS+"-omp-native-")
	if err != nil {
		t.Fatal("create persistent OMP evidence directory failed")
	}
	witness := nativeAcceptanceNewWitness(t, cfg, root)
	defer witness.close(t)
	witness.manifest(t)
	witness.prepareRepository(t)
	subject := witness.subject(t)
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		t.Fatal("create retained-context challenge failed")
	}
	token := hex.EncodeToString(tokenBytes[:])
	challenge := nativeAcceptanceCheckpoint{Subject: subject, Token: token}
	witness.write(t, "challenge.json", challenge)

	// The helper durably records the real settled result AND physically stops
	// the native process, then dies before returning its acknowledgement. The
	// new Go process reconciles that record instead of replaying the old turn.
	// This deliberately exercises only adapter/witness restart recovery.
	helper, err := os.Executable()
	if err != nil {
		t.Fatal("resolve acceptance helper executable failed")
	}
	helperContext, helperCancel := context.WithTimeout(context.Background(), 2*nativeAcceptanceTimeout)
	defer helperCancel()
	command := exec.CommandContext(helperContext, helper, "-test.run=^TestNativeAcceptanceLostAckHelper$", "-test.count=1")
	command.Env = nativeAcceptanceHelperEnvironment(t, root)
	var helperOutput bytes.Buffer
	command.Stdout, command.Stderr = &helperOutput, &helperOutput
	err = command.Run()
	if witness.journal.check(helperOutput.Bytes()) != nil {
		t.Fatal("acceptance helper output violated credential locality")
	}
	witness.record(t, map[string]any{"class": "witness_process_output", "output": helperOutput.String()})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != nativeAcceptanceHelperExit {
		t.Fatal("acceptance helper did not reach the durable lost-ack boundary; inspect sanitized witness output")
	}
	var completed nativeAcceptanceCheckpoint
	witness.read(t, "first-completed.json", &completed)
	if !completed.Completed || completed.Handle.ID == "" || completed.Handle.Filename == "" || completed.Subject != subject || completed.Token != token {
		t.Fatal("lost-ack witness did not retain the complete native handle and settled result")
	}
	witness.assertArtifacts(t, map[string]string{nativeAcceptanceFirstArtifact: "native OMP repository evidence\n"})
	witness.record(t, map[string]any{
		"class": "adapter_restart_lost_ack", "evidence_class": "supporting_fsynced_witness_not_app_control_recovery",
		"helper_exit": exit.ExitCode(), "durable_completion_reconciled": true, "old_turn_replayed": false,
		"native_handle": completed.Handle, "native_version": TestedVersion,
	})

	resume := &harness.ResumeHandle{
		NativeSessionID: completed.Handle.ID, NativeSessionFilename: completed.Handle.Filename,
		NativeVersion: TestedVersion,
	}
	resumed, resumedHandle := witness.open(t, "retained-resume", resume, "write")
	if resumedHandle != completed.Handle {
		t.Fatal("retained resume did not preserve the exact native session ID and file")
	}
	// Neither this goal nor its Context contains the answer. Only the earlier
	// transcript supplies it, and this turn has no read or shell tool.
	resumeGoal := "Create only " + nativeAcceptanceResumeArtifact + " in the workspace root. Its exact bytes must be the continuity token supplied in the earlier turn of this native session followed by one newline. Do not modify the earlier artifact or any other file. Use the write tool exactly once. "
	witness.turn(t, resumed, "retained-resume", resumeGoal, json.RawMessage(`{"schema_version":"omp-native-continuity.v1","operation":"retained_context_challenge"}`), subject, 2)
	witness.assertWrites(t, resumed, nativeAcceptanceResumeArtifact)
	witness.assertArtifacts(t, map[string]string{
		nativeAcceptanceFirstArtifact: "native OMP repository evidence\n", nativeAcceptanceResumeArtifact: token + "\n",
	})
	witness.record(t, map[string]any{"class": "retained_resume", "same_id_and_file": true, "retained_context_challenge": "passed", "prior_artifact_unchanged": true})

	handoff, handoffHandle := witness.open(t, "fresh-handoff", nil, "write")
	if handoffHandle.ID == completed.Handle.ID || handoffHandle.Filename == completed.Handle.Filename {
		t.Fatal("fresh handoff reused the predecessor's native identity")
	}
	handoffContext, err := json.Marshal(map[string]any{
		"schema_version": "omp-native-handoff.v1", "predecessor": completed.Handle.ID,
		"native_version": TestedVersion, "subject": subject, "continuity_token": token,
		"retained_artifacts": []string{nativeAcceptanceFirstArtifact, nativeAcceptanceResumeArtifact},
	})
	if err != nil {
		t.Fatal("encode versioned handoff context failed")
	}
	witness.turn(t, handoff, "fresh-handoff", "Create only "+nativeAcceptanceHandoffArtifact+" at the workspace root, containing the continuity_token from the supplied versioned context followed by one newline. Preserve all earlier work. Use the write tool exactly once. ", handoffContext, subject, 3)
	witness.assertWrites(t, handoff, nativeAcceptanceHandoffArtifact)
	retained := map[string]string{
		nativeAcceptanceFirstArtifact: "native OMP repository evidence\n", nativeAcceptanceResumeArtifact: token + "\n", nativeAcceptanceHandoffArtifact: token + "\n",
	}
	witness.assertArtifacts(t, retained)
	witness.record(t, map[string]any{"class": "fresh_handoff", "resume_argument": nil, "new_handle": handoffHandle, "predecessor": completed.Handle, "context_schema": "omp-native-handoff.v1", "prior_artifacts_unchanged": true})
	witness.cancel(t, retained)
	witness.record(t, map[string]any{
		"class": "evidence_scope", "platform": runtime.GOOS,
		"live":         []string{"repository_work", "retained_resume", "fresh_handoff", "in_flight_tool_cancel", "stop_and_drain", "artifact_integrity", "subject_integrity", "single_write_per_artifact"},
		"supporting":   []string{"adapter_process_restart", "fsynced_witness_completion_ack_loss"},
		"not_attested": []string{"production_app_control_outbox_recovery", "upstream_served_model", "provider_billing", "server_prices", "cross_machine_handoff"},
		"usage":        harness.UsageUnknown,
	})
	t.Logf("persistent sanitized OMP acceptance receipts: %s", root)
}

// TestNativeAcceptanceLostAckHelper runs only as the parent's current Go test
// executable. Exiting after physical stop avoids leaving a native orphan and
// makes the exact lost acknowledgement boundary inspectable in the journal.
func TestNativeAcceptanceLostAckHelper(t *testing.T) {
	root := os.Getenv("SYMMETRY_OMP_WITNESS_ROOT")
	if root == "" {
		t.Skip("subprocess-only native completion acknowledgement witness")
	}
	cfg := nativeAcceptanceConfiguration(t)
	witness := nativeAcceptanceNewWitness(t, cfg, root)
	var checkpoint nativeAcceptanceCheckpoint
	witness.read(t, "challenge.json", &checkpoint)
	if checkpoint.Completed {
		t.Fatal("helper challenge was already completed")
	}
	session, handle := witness.open(t, "first-turn", nil, "write")
	goal := "Perform only this bounded repository task. Create " + nativeAcceptanceFirstArtifact + " at the workspace root with exactly these UTF-8 bytes including the final newline:\nnative OMP repository evidence\nDo not create, modify or delete any other file. Use the write tool exactly once. Retain the following continuity token in this session's context, but do not put it in the file: " + checkpoint.Token + ". "
	witness.turn(t, session, "first-turn", goal, json.RawMessage(`{"schema_version":"omp-native-work.v1"}`), checkpoint.Subject, 1)
	witness.assertWrites(t, session, nativeAcceptanceFirstArtifact)
	witness.assertArtifacts(t, map[string]string{nativeAcceptanceFirstArtifact: "native OMP repository evidence\n"})
	checkpoint.Handle, checkpoint.Completed = handle, true
	witness.write(t, "first-completed.json", checkpoint)
	witness.record(t, map[string]any{"class": "completion_durable_before_witness_ack", "native_handle": handle, "turn_replay_count": 0, "physical_stop_and_drain_complete": true})
	witness.close(t)
	os.Exit(nativeAcceptanceHelperExit)
}

// TestNativeAcceptanceAuthenticationFailure is a separate opt-in negative
// credential witness, not evidence that the operator's configured virtual key
// is unavailable. It sends exactly one real native prompt. Opening a retained
// session and a fresh handoff afterward
// proves transport identity only, NOT successful repository/context continuity.
// Provider error text and native output bytes are never persisted by this test.
func TestNativeAcceptanceAuthenticationFailure(t *testing.T) {
	if os.Getenv("SYMMETRY_OMP_NATIVE_AUTH_FAILURE") != "1" {
		t.Skip("set SYMMETRY_OMP_NATIVE_AUTH_FAILURE=1 for the negative credential refusal witness")
	}
	cfg := nativeAcceptanceConfiguration(t)
	root, err := os.MkdirTemp(cfg.evidenceRoot, runtime.GOOS+"-omp-auth-refusal-")
	if err != nil {
		t.Fatal("create persistent OMP refusal evidence directory failed")
	}
	witness := nativeAcceptanceNewWitness(t, cfg, root)
	witness.metadataOnlyFrames = true
	defer witness.close(t)
	witness.manifest(t)
	witness.prepareRepository(t)
	witness.subject(t)
	session, handle := witness.open(t, "auth-refused", nil, "write")
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceTimeout)
	defer cancel()
	startErr := session.StartTurn(ctx, harness.TurnRequest{
		Goal:    "Do not read, write, execute or modify anything. Return the single word READY.",
		Context: json.RawMessage(`{"schema_version":"omp-native-auth-refusal.v1"}`),
	})
	turnErr := session.WaitTurn(ctx)
	if ctx.Err() != nil || startErr != nil || turnErr != nil {
		t.Fatal("real approved relay prompt did not reach its native settlement boundary")
	}
	result := witness.stop(t, session, "auth-refused")
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != protocol.TaskResultReasonAuth || result.Semantic != nil || result.Usage.State != harness.UsageUnknown || result.Usage.CostMicrousd != "" {
		t.Fatal("real authentication refusal was not classified as failed/auth with unknown usage")
	}
	session.mutex.Lock()
	promptErrors, settled, toolStarts := 0, 0, 0
	actualRelay401 := false
	for _, raw := range session.frames {
		var frame struct {
			Type      string `json:"type"`
			Status    string `json:"status"`
			Retryable *bool  `json:"retryable"`
			Error     struct {
				HTTPStatus int   `json:"httpStatus"`
				Retryable  *bool `json:"retryable"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		switch frame.Type {
		case "prompt_result":
			retryable := frame.Retryable
			if retryable == nil {
				retryable = frame.Error.Retryable
			}
			if frame.Status == "error" && retryable != nil && !*retryable {
				promptErrors++
			}
		case "session_settled":
			settled++
		case "tool_execution_start":
			toolStarts++
		}
		// Inspect the actual native refusal in memory only. Do not retain its
		// message, a partial key, an encoded key, or any credential-derived hash.
		if bytes.Contains(raw, []byte("401")) && bytes.Contains(raw, []byte("LiteLLM Virtual Key expected")) {
			actualRelay401 = true
		}
	}
	prompts := session.promptCommands
	session.mutex.Unlock()
	if !actualRelay401 || promptErrors != 1 || settled != 1 || toolStarts != 0 || prompts != 1 {
		t.Fatal("native refusal evidence did not match the actual non-retryable relay 401 and single settled prompt")
	}
	info, err := os.Stat(handle.Filename)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("failed native turn did not retain its session file")
	}
	retained, retainedHandle := witness.open(t, "failed-session-resume", &harness.ResumeHandle{
		NativeSessionID: handle.ID, NativeSessionFilename: handle.Filename, NativeVersion: TestedVersion,
	}, "write")
	if retainedHandle != handle {
		t.Fatal("failed native session transport did not retain its exact identity")
	}
	witness.stop(t, retained, "failed-session-resume")
	fresh, freshHandle := witness.open(t, "failed-session-fresh-handoff", nil, "write")
	if freshHandle.ID == handle.ID || freshHandle.Filename == handle.Filename {
		t.Fatal("fresh transport handoff after auth refusal reused the old native identity")
	}
	witness.stop(t, fresh, "failed-session-fresh-handoff")
	retained.mutex.Lock()
	retainedPrompts := retained.promptCommands
	retained.mutex.Unlock()
	fresh.mutex.Lock()
	freshPrompts := fresh.promptCommands
	fresh.mutex.Unlock()
	if retainedPrompts != 0 || freshPrompts != 0 {
		t.Fatal("failure-path identity checks unexpectedly submitted extra provider prompts")
	}
	witness.assertArtifacts(t, map[string]string{})
	witness.record(t, map[string]any{
		"class": "live_authentication_refusal", "http_status_observed_in_memory": 401,
		"provider_message": "omitted", "error_class": protocol.TaskResultReasonAuth,
		"prompt_result_status": "error", "retryable": false, "session_settled_count": settled,
		"native_prompt_commands": prompts, "native_tool_starts": toolStarts, "usage": harness.UsageUnknown,
		"failed_handle": handle, "retained_transport_handle": retainedHandle, "fresh_transport_handle": freshHandle,
		"additional_native_prompts": retainedPrompts + freshPrompts,
		"evidence_class":            "live_provider_failure_and_supporting_transport_identity_only",
		"not_attested":              []string{"successful_repository_work", "retained_context_continuity", "production_app_control_recovery", "provider_billing", "server_prices"},
	})
	t.Logf("persistent sanitized OMP authentication-refusal receipts: %s", root)
}

type nativeAcceptanceConfig struct {
	executable     string
	repository     string
	evidenceRoot   string
	agentDirectory string
	environment    []string
	secret         []byte
}

func nativeAcceptanceConfiguration(t *testing.T) nativeAcceptanceConfig {
	t.Helper()
	if os.Getenv("SYMMETRY_OMP_NATIVE") != "1" {
		t.Skip("set SYMMETRY_OMP_NATIVE=1 for credentialed exact-binary OMP acceptance")
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Fatal("native OMP acceptance requires Windows or Ubuntu 24.04")
	}
	if TestedVersion != "18.20.0" || ImplementationVersion != "omp-rpc-v2" {
		t.Fatal("native OMP acceptance version binding changed")
	}
	requiredPath := func(name string) string {
		value := os.Getenv(name)
		if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
			t.Fatalf("%s must name an absolute local path", name)
		}
		return filepath.Clean(value)
	}
	cfg := nativeAcceptanceConfig{
		executable: requiredPath("SYMMETRY_OMP_EXECUTABLE"), repository: requiredPath("SYMMETRY_OMP_REPOSITORY"),
		evidenceRoot: requiredPath("SYMMETRY_OMP_EVIDENCE_ROOT"), agentDirectory: requiredPath("PI_CODING_AGENT_DIR"),
	}
	var err error
	cfg.environment, err = execution.BuildEnvironment("PI_CODING_AGENT_DIR", "TENCENT_GLM_API_KEY")
	if err != nil {
		t.Fatal("build explicit production OMP environment failed")
	}
	for _, entry := range cfg.environment {
		name, value, _ := strings.Cut(entry, "=")
		if name == "TENCENT_GLM_API_KEY" {
			cfg.secret = []byte(value)
		}
	}
	if len(cfg.secret) == 0 {
		t.Fatal("TENCENT_GLM_API_KEY must be present in the explicit child environment")
	}
	for _, directory := range []string{cfg.repository, cfg.evidenceRoot, cfg.agentDirectory} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			t.Fatal("configured native OMP directory is unavailable")
		}
	}
	return cfg
}

func nativeAcceptanceHelperEnvironment(t *testing.T, root string) []string {
	t.Helper()
	environment, err := execution.BuildEnvironment("PI_CODING_AGENT_DIR", "TENCENT_GLM_API_KEY", "SYMMETRY_OMP_NATIVE", "SYMMETRY_OMP_EXECUTABLE", "SYMMETRY_OMP_REPOSITORY", "SYMMETRY_OMP_EVIDENCE_ROOT")
	if err != nil {
		t.Fatal("build isolated witness helper environment failed")
	}
	return append(environment, "SYMMETRY_OMP_WITNESS_ROOT="+root)
}

type nativeAcceptanceCheckpoint struct {
	Subject   protocol.Subject            `json:"subject"`
	Token     string                      `json:"continuity_token"`
	Handle    harness.NativeSessionHandle `json:"native_handle"`
	Completed bool                        `json:"completed"`
}

type nativeAcceptanceJournal struct {
	mutex         sync.Mutex
	file          *os.File
	secret        []byte
	escapedSecret []byte
}

func (journal *nativeAcceptanceJournal) check(data []byte) error {
	if bytes.Contains(data, journal.secret) || (len(journal.escapedSecret) != 0 && bytes.Contains(data, journal.escapedSecret)) {
		return errors.New("native evidence violated credential locality")
	}
	return nil
}

func (journal *nativeAcceptanceJournal) append(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("encode witness record failed")
	}
	if err := journal.check(data); err != nil {
		return err
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if _, err := journal.file.Write(append(data, '\n')); err != nil {
		return errors.New("append witness record failed")
	}
	if err := journal.file.Sync(); err != nil {
		return errors.New("sync witness record failed")
	}
	return nil
}

type nativeAcceptanceWitness struct {
	cfg                nativeAcceptanceConfig
	root               string
	journal            *nativeAcceptanceJournal
	metadataOnlyFrames bool
}

func nativeAcceptanceNewWitness(t *testing.T, cfg nativeAcceptanceConfig, root string) *nativeAcceptanceWitness {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(root, "witness.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("open persistent native witness journal failed")
	}
	escaped, err := json.Marshal(string(cfg.secret))
	if err != nil {
		t.Fatal("initialize native evidence locality assertion failed")
	}
	return &nativeAcceptanceWitness{cfg: cfg, root: root, journal: &nativeAcceptanceJournal{file: file, secret: cfg.secret, escapedSecret: escaped[1 : len(escaped)-1]}}
}

func (witness *nativeAcceptanceWitness) close(t *testing.T) {
	t.Helper()
	if err := witness.journal.file.Close(); err != nil {
		t.Error("close native witness journal failed")
	}
}

func (witness *nativeAcceptanceWitness) record(t *testing.T, value any) {
	t.Helper()
	if err := witness.journal.append(value); err != nil {
		t.Fatal("persist credential-safe native witness record failed")
	}
}

func (witness *nativeAcceptanceWitness) write(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil || witness.journal.check(data) != nil {
		t.Fatal("encode credential-safe native witness checkpoint failed")
	}
	file, err := os.OpenFile(filepath.Join(witness.root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("create native witness checkpoint failed")
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("persist native witness checkpoint failed")
	}
}

func (witness *nativeAcceptanceWitness) read(t *testing.T, name string, value any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(witness.root, name))
	if err != nil || witness.journal.check(data) != nil || json.Unmarshal(data, value) != nil {
		t.Fatal("read credential-safe native witness checkpoint failed")
	}
}

func (witness *nativeAcceptanceWitness) manifest(t *testing.T) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(witness.cfg.executable)
	if err != nil {
		t.Fatal("resolve exact native executable failed")
	}
	file, err := os.Open(resolved)
	if err != nil {
		t.Fatal("open exact native executable failed")
	}
	info, statErr := file.Stat()
	hash := sha256.New()
	_, hashErr := io.Copy(hash, file)
	closeErr := file.Close()
	if statErr != nil || hashErr != nil || closeErr != nil || !info.Mode().IsRegular() {
		t.Fatal("measure exact native executable failed")
	}
	wantSize, wantDigest := int64(251502592), "1b6c7bda8241bc7d948495539f90ec47e6f30d1fec72b6ee097a67a1fc8d8e48"
	if runtime.GOOS == "linux" {
		wantSize, wantDigest = 287484080, "c8694be8c6e6a1f45e3636dd8a8e9eaa78878ad629a89000b403ec5e456d3f89"
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if info.Size() != wantSize || digest != wantDigest {
		t.Fatal("native executable is not the owner-approved exact platform blob")
	}
	version := witness.command(t, witness.root, witness.cfg.environment, resolved, "--version")
	if strings.TrimSpace(version) != "omp/18.20.0" {
		t.Fatal("exact native executable returned an unexpected version line")
	}
	probeContext, probeCancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer probeCancel()
	capabilities, probeErr := NewAdapter(witness.cfg.executable).Probe(probeContext)
	if (probeErr != nil && !errors.Is(probeErr, harness.ErrNativeUnverified)) || string(capabilities.Kind) != "omp" || capabilities.NativeVersion != TestedVersion || capabilities.ImplementationVersion != ImplementationVersion || !capabilities.VersionKnown || !capabilities.TransportVerified || capabilities.Usage != harness.UsageUnknown {
		t.Fatal("exact OMP candidate identity or transport probe did not match")
	}
	if errors.Is(probeErr, harness.ErrNativeUnverified) && (capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Handoff || capabilities.ProviderAccess || capabilities.HardCostLimit) {
		t.Fatal("unverified OMP candidate advertised native operations")
	}
	witness.record(t, map[string]any{"class": "candidate_probe", "kind": capabilities.Kind, "native_version": capabilities.NativeVersion, "implementation_version": capabilities.ImplementationVersion, "transport_verified": capabilities.TransportVerified, "capabilities_promoted": capabilities.Verified, "usage": capabilities.Usage})
	agentDirectory, err := filepath.EvalSymlinks(witness.cfg.agentDirectory)
	if err != nil {
		t.Fatal("resolve isolated native catalog directory failed")
	}
	witness.catalog(t, agentDirectory)
	platform := map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH}
	if runtime.GOOS == "linux" {
		distribution, err := os.ReadFile("/etc/os-release")
		if err != nil || !bytes.Contains(distribution, []byte("ID=ubuntu")) || !bytes.Contains(distribution, []byte(`VERSION_ID="24.04"`)) {
			t.Fatal("Linux acceptance must run in explicit wsl.exe -d Ubuntu-24.04")
		}
		platform["os_release"] = string(distribution)
		platform["kernel"] = strings.TrimSpace(witness.command(t, witness.root, nil, "uname", "-a"))
		platform["launch_surface"] = "wsl.exe -d Ubuntu-24.04"
	} else {
		platform["kernel"] = strings.TrimSpace(witness.command(t, witness.root, nil, "cmd.exe", "/d", "/c", "ver"))
	}
	witness.record(t, map[string]any{
		"class": "exact_binary_manifest", "executable_configured": witness.cfg.executable, "executable_resolved": resolved,
		"size": info.Size(), "sha256": digest, "version_line": strings.TrimSpace(version), "implementation": ImplementationVersion,
		"platform": platform, "model_selector": nativeAcceptanceModel, "served_model": "unknown",
		"catalog_directory_configured": witness.cfg.agentDirectory, "catalog_directory_resolved": agentDirectory,
		"environment_builder": "execution.BuildEnvironment(PI_CODING_AGENT_DIR,TENCENT_GLM_API_KEY)",
		"usage":               harness.UsageUnknown, "billing": "unknown", "prices": "unknown", "journal": "fsynced_local_witness_not_production_journal",
	})
}

func (witness *nativeAcceptanceWitness) catalog(t *testing.T, directory string) {
	t.Helper()
	// The parent prepares this isolated JSON-form YAML catalog from the
	// existing approved relay configuration. No auth file is consulted and no
	// credentials, headers, prices or full catalog are copied to the receipt.
	filename := filepath.Join(directory, "models.yml")
	data, err := os.ReadFile(filename)
	if err != nil || witness.journal.check(data) != nil {
		t.Fatal("read isolated credential-free OMP model catalog failed")
	}
	var catalog struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			API     string `json:"api"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		t.Fatal("isolated models.yml must contain the parent's approved JSON-form catalog")
	}
	provider, ok := catalog.Providers["tencent-litellm"]
	if !ok || provider.APIKey != "TENCENT_GLM_API_KEY" || provider.API != "openai-completions" {
		t.Fatal("isolated catalog must reference the approved credential environment name and relay API")
	}
	endpoint, err := url.Parse(provider.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		t.Fatal("approved relay must use a credential-free discovered HTTPS endpoint")
	}
	host := strings.ToLower(endpoint.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) {
		t.Fatal("native provider evidence cannot use a loopback gateway")
	}
	models := 0
	for _, model := range provider.Models {
		if model.ID == "deepseek-v4.1-flash-fast" {
			models++
		}
	}
	if models != 1 {
		t.Fatal("isolated catalog must select the approved relay model exactly once")
	}
	digest := sha256.Sum256(data)
	witness.record(t, map[string]any{
		"class": "approved_catalog_provenance", "filename": filename, "sha256": hex.EncodeToString(digest[:]),
		"provider": "tencent-litellm", "model": "deepseek-v4.1-flash-fast", "api": provider.API,
		"endpoint": endpoint.String(), "credential_reference": "TENCENT_GLM_API_KEY",
		"pricing_semantics": "catalog_prices_not_billing_evidence", "usage": harness.UsageUnknown,
	})
}

func (witness *nativeAcceptanceWitness) command(t *testing.T, directory string, environment []string, program string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer cancel()
	if environment == nil {
		var err error
		environment, err = execution.BuildEnvironment()
		if err != nil {
			t.Fatal("build non-provider command environment failed")
		}
	}
	command := exec.CommandContext(ctx, program, args...)
	command.Dir, command.Env = directory, environment
	output, err := command.CombinedOutput()
	if witness.journal.check(output) != nil {
		t.Fatal("native command output violated credential locality")
	}
	witness.record(t, map[string]any{"class": "command", "program": program, "args": args, "directory": directory, "output": string(output), "success": err == nil})
	if err != nil {
		t.Fatal("native witness command failed; inspect sanitized command receipt")
	}
	return string(output)
}

func (witness *nativeAcceptanceWitness) git(t *testing.T, args ...string) string {
	t.Helper()
	prefix := []string{"-c", "core.hooksPath=" + filepath.Join(witness.root, "disabled-hooks"), "-c", "credential.helper=", "-c", "core.autocrlf=false"}
	return witness.command(t, filepath.Join(witness.root, "workspace"), nil, "git", append(prefix, args...)...)
}

func (witness *nativeAcceptanceWitness) prepareRepository(t *testing.T) {
	t.Helper()
	for _, name := range []string{"sessions", "disabled-hooks", "git-templates"} {
		if err := os.Mkdir(filepath.Join(witness.root, name), 0o700); err != nil {
			t.Fatal("create isolated witness directory failed")
		}
	}
	witness.command(t, witness.root, nil, "git", "-c", "init.templateDir="+filepath.Join(witness.root, "git-templates"), "-c", "core.hooksPath="+filepath.Join(witness.root, "disabled-hooks"), "clone", "--no-hardlinks", "--no-checkout", "--", witness.cfg.repository, filepath.Join(witness.root, "workspace"))
	witness.git(t, "checkout", "--detach", "HEAD")
	if witness.git(t, "status", "--porcelain=v1", "--untracked-files=all") != "" {
		t.Fatal("approved local fixture clone was not clean")
	}
}

func (witness *nativeAcceptanceWitness) subject(t *testing.T) protocol.Subject {
	t.Helper()
	commit := strings.TrimSpace(witness.git(t, "rev-parse", "HEAD"))
	tree := strings.TrimSpace(witness.git(t, "rev-parse", "HEAD^{tree}"))
	manager := workspace.New(map[string]config.Workspace{"native": {
		Policy: config.WorkspacePolicyGitWorktree, Repository: filepath.Join(witness.root, "workspace"),
		Root: filepath.Join(witness.root, "subject-worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
	}})
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer cancel()
	subject, err := manager.MaterializeSubject(ctx, "native", nativeAcceptanceResourceID, commit)
	if err != nil || subject.Commit != commit {
		t.Fatal("derive canonical Subject from actual fixture commit failed")
	}
	canonical, err := subject.CanonicalJSON()
	if err != nil {
		t.Fatal("canonicalize actual repository Subject failed")
	}
	digest := sha256.Sum256(canonical)
	independentHash := "sha256:" + hex.EncodeToString(digest[:])
	subjectHash, err := subject.Hash()
	if err != nil || independentHash != subjectHash {
		t.Fatal("independent canonical Subject SHA-256 disagreed")
	}
	witness.write(t, "baseline.json", map[string]any{"subject": subject, "subject_hash": subjectHash, "git_commit": commit, "git_tree": tree})
	witness.record(t, map[string]any{"class": "repository_subject", "source_local_clone": witness.cfg.repository, "git_commit": commit, "git_tree": tree, "subject": subject, "canonical_subject": json.RawMessage(canonical), "independent_subject_hash": independentHash})
	return subject
}

type nativeAcceptanceSession struct {
	harness.StagedSession
	mutex               sync.Mutex
	frames              []json.RawMessage
	started             int
	results             int
	promptCommands      int
	expectedBashCommand string
	bashStarted         chan struct{}
	bashOnce            sync.Once
	stopped             bool
}

func (witness *nativeAcceptanceWitness) open(t *testing.T, label string, resume *harness.ResumeHandle, tools string) (*nativeAcceptanceSession, harness.NativeSessionHandle) {
	t.Helper()
	sessionDirectory := filepath.Join(witness.root, "sessions")
	args := []string{"--session-dir", sessionDirectory, "--model", nativeAcceptanceModel, "--tools", tools, "--no-ui", "--no-prewalk", "--no-extensions", "--no-skills", "--no-rules", "--no-context-files", "--no-lsp", "--no-pty", "--no-title"}
	if ValidateRPCProfileArgs(args) != nil {
		t.Fatal("native acceptance argv is not audited")
	}
	observed := &nativeAcceptanceSession{bashStarted: make(chan struct{})}
	persistedProcess := false
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceTimeout)
	t.Cleanup(cancel)
	witness.record(t, map[string]any{"class": "witness_launch_intent", "label": label, "resume": resume, "session_directory_configured": sessionDirectory})
	session, err := NewAdapter(witness.cfg.executable).Start(ctx, harness.StartRequest{
		Workspace: filepath.Join(witness.root, "workspace"), Resume: resume,
		Invocation: execution.Invocation{Args: args, Env: witness.cfg.environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || identity == "" {
				return errors.New("native process identity incomplete")
			}
			if err := witness.journal.append(map[string]any{"class": "process_persisted_before_open", "label": label, "pid": pid, "identity": identity}); err != nil {
				return err
			}
			persistedProcess = true
			return nil
		},
		StartProcess: func(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
			if err := witness.journal.append(map[string]any{"class": "native_invocation", "label": label, "program": invocation.Program, "args": invocation.Args, "directory": invocation.Dir}); err != nil {
				return nil, err
			}
			process, err := execution.NewRunner().Start(ctx, invocation, execution.SinkFunc(func(ctx context.Context, event execution.Event) error {
				if err := witness.journal.check(event.Data); err != nil {
					return err
				}
				// Provider errors can contain partial credentials. Raw stdout is
				// represented by the sanitized logical frame receipts instead;
				// raw stderr is intentionally never serialized.
				if err := witness.journal.append(map[string]any{"class": "native_process_output", "label": label, "stream": event.Stream, "sequence": event.Sequence, "at": event.At, "byte_count": len(event.Data), "raw_bytes": "omitted_provider_text"}); err != nil {
					return err
				}
				return sink.Handle(ctx, event)
			}))
			if process == nil {
				return nil, err
			}
			return &nativeAcceptanceProcess{NativeProcess: process, journal: witness.journal, label: label, session: observed}, err
		},
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		if witness.journal.check(event.Data) != nil || witness.journal.check(event.Payload) != nil || witness.journal.check([]byte(event.Message)) != nil {
			return errors.New("native event violated credential locality")
		}
		record := map[string]any{"class": "native_adapter_event", "label": label, "kind": event.Kind, "sequence": event.Sequence, "at": event.At, "stream": event.Stream}
		if event.Kind == harness.EventNativeFrame {
			raw := event.Payload
			if len(raw) == 0 {
				raw = event.Data
			}
			safe, err := nativeAcceptanceSafeFrame(raw, witness.metadataOnlyFrames)
			if err != nil {
				return err
			}
			record["frame"] = safe
		}
		if err := witness.journal.append(record); err != nil {
			return err
		}
		observed.mutex.Lock()
		defer observed.mutex.Unlock()
		switch event.Kind {
		case harness.EventNativeFrame:
			data := event.Payload
			if len(data) == 0 {
				data = event.Data
			}
			var frame struct {
				Type     string `json:"type"`
				ToolName string `json:"toolName"`
				Args     struct {
					Command string `json:"command"`
				} `json:"args"`
			}
			if json.Unmarshal(data, &frame) != nil {
				return errors.New("native acceptance requires exact decoded frame bytes")
			}
			observed.frames = append(observed.frames, append(json.RawMessage(nil), data...))
			if frame.Type == "tool_execution_start" && frame.ToolName == "bash" && observed.expectedBashCommand != "" && frame.Args.Command == observed.expectedBashCommand {
				observed.bashOnce.Do(func() { close(observed.bashStarted) })
			}
		case harness.EventSessionStarted:
			observed.started++
		case harness.EventTaskResult:
			observed.results++
		}
		return nil
	}))
	if session != nil {
		t.Cleanup(func() {
			if observed.stopped {
				return
			}
			stopCtx, stopCancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
			defer stopCancel()
			_ = session.Close(stopCtx)
			if _, err := session.Wait(stopCtx); err != nil {
				t.Error("cleanup native process stop/drain failed")
			}
		})
	}
	if err != nil || session == nil {
		t.Fatal("start real native OMP process failed")
	}
	staged, ok := session.(harness.StagedSession)
	if !ok || !persistedProcess {
		t.Fatal("native process was not persisted before staged session exposure")
	}
	observed.StagedSession = staged
	pid, identity := staged.ProcessDetails()
	if pid <= 0 || identity == "" {
		t.Fatal("staged native process identity incomplete")
	}
	handle, err := staged.Open(ctx)
	if err != nil || handle.ID == "" || handle.Filename == "" {
		t.Fatal("open real native OMP session failed")
	}
	configured, err := filepath.EvalSymlinks(sessionDirectory)
	if err != nil {
		t.Fatal("resolve configured native session directory failed")
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(handle.Filename))
	if err != nil {
		t.Fatal("resolve native session filename provenance failed")
	}
	resolvedFilename := filepath.Join(resolvedParent, filepath.Base(handle.Filename))
	relative, err := filepath.Rel(configured, resolvedFilename)
	if err != nil || !filepath.IsAbs(handle.Filename) || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("native session filename escaped the configured directory")
	}
	witness.write(t, label+"-handle.json", handle)
	witness.record(t, map[string]any{"class": "handle_persisted_before_turn", "label": label, "handle": handle, "native_version": TestedVersion, "session_directory_configured": sessionDirectory, "session_directory_resolved": configured, "session_filename_resolved": resolvedFilename})
	return observed, handle
}

type nativeAcceptanceProcess struct {
	harness.NativeProcess
	journal *nativeAcceptanceJournal
	label   string
	session *nativeAcceptanceSession
}

func (process *nativeAcceptanceProcess) CloseInput() error {
	closer, ok := process.NativeProcess.(interface{ CloseInput() error })
	if !ok {
		return errors.ErrUnsupported
	}
	return closer.CloseInput()
}

func (process *nativeAcceptanceProcess) WriteInputContext(ctx context.Context, data []byte) error {
	if err := process.journal.check(data); err != nil {
		return err
	}
	if err := process.journal.append(map[string]any{"class": "native_command", "label": process.label, "bytes": string(data)}); err != nil {
		return err
	}
	if err := process.NativeProcess.WriteInputContext(ctx, data); err != nil {
		return err
	}
	var command struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &command) == nil && command.Type == "prompt" {
		process.session.mutex.Lock()
		process.session.promptCommands++
		process.session.mutex.Unlock()
	}
	return nil
}

func nativeAcceptanceSafeFrame(raw []byte, metadataOnly bool) (json.RawMessage, error) {
	var frame map[string]json.RawMessage
	if json.Unmarshal(raw, &frame) != nil || frame == nil {
		return nil, errors.New("native frame evidence is not a JSON object")
	}
	var kind string
	_ = json.Unmarshal(frame["type"], &kind)
	providerError := strings.Contains(kind, "error") || bytes.Contains(raw, []byte(`"error"`)) || bytes.Contains(raw, []byte(`"errorMessage"`))
	if !metadataOnly && !providerError {
		return append(json.RawMessage(nil), raw...), nil
	}
	// This allowlist is intentionally lossy for errors. In particular neither
	// message/content fields nor nested error values can enter durable evidence.
	safe := make(map[string]json.RawMessage)
	for _, name := range []string{"type", "status", "retryable", "success", "protocolVersion", "protocol_version"} {
		if value, ok := frame[name]; ok {
			safe[name] = value
		}
	}
	var id string
	if json.Unmarshal(frame["id"], &id) == nil && strings.HasPrefix(id, "symmetry-") {
		safe["id"] = frame["id"]
	}
	if rawError, ok := frame["error"]; ok {
		var details struct {
			HTTPStatus int   `json:"httpStatus"`
			Retryable  *bool `json:"retryable"`
		}
		if json.Unmarshal(rawError, &details) == nil {
			if details.HTTPStatus >= 100 && details.HTTPStatus <= 599 {
				encoded, _ := json.Marshal(details.HTTPStatus)
				safe["observed_http_status"] = encoded
			}
			if details.Retryable != nil {
				encoded, _ := json.Marshal(*details.Retryable)
				safe["retryable"] = encoded
			}
		}
	}
	safe["provider_text"] = json.RawMessage(`"omitted"`)
	return json.Marshal(safe)
}

func (witness *nativeAcceptanceWitness) expected(t *testing.T, subject protocol.Subject, ordinal int) protocol.TaskResult {
	t.Helper()
	hash, err := subject.Hash()
	if err != nil {
		t.Fatal("hash canonical result Subject failed")
	}
	result := protocol.TaskResult{
		SchemaVersion: protocol.TaskResultSchemaVersion, ResultID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 300+ordinal),
		Kind: protocol.TaskResultProgress, Summary: "created native OMP repository evidence", Subject: subject, SubjectHash: hash,
		EvidenceRefs: []string{}, Diagnostics: []protocol.Diagnostic{},
	}
	if result.Validate() != nil {
		t.Fatal("validate canonical expected TaskResult failed")
	}
	return result
}

func (witness *nativeAcceptanceWitness) turn(t *testing.T, session *nativeAcceptanceSession, label, goal string, data json.RawMessage, subject protocol.Subject, ordinal int) {
	t.Helper()
	expected := witness.expected(t, subject, ordinal)
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal("encode canonical expected TaskResult failed")
	}
	goal += "After the real file write succeeds, return exactly this JSON object as the entire final assistant text, without prose, Markdown or a code fence:\n" + string(encoded)
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceTimeout)
	defer cancel()
	if session.StartTurn(ctx, harness.TurnRequest{Goal: goal, Context: data}) != nil || session.WaitTurn(ctx) != nil {
		t.Fatal("real OMP repository turn did not settle with a strict TaskResult")
	}
	result := witness.stop(t, session, label)
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || !reflect.DeepEqual(*result.Semantic, expected) {
		t.Fatal("real repository effect did not produce the exact canonical TaskResult")
	}
	if result.Usage.State != harness.UsageUnknown || result.Usage.CostMicrousd != "" {
		t.Fatal("catalog prices or native estimates were promoted to authoritative usage")
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	settled := 0
	for _, raw := range session.frames {
		var frame struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &frame) == nil && frame.Type == "session_settled" {
			settled++
		}
	}
	if session.started != 1 || session.results != 1 || settled != 1 {
		t.Fatal("native turn did not emit exactly one start, result and session_settled boundary")
	}
	witness.record(t, map[string]any{"class": "repository_turn", "label": label, "turn_status": result.Kind, "canonical_result": result.Semantic, "usage": harness.UsageUnknown, "session_settled_count": settled})
}

func (witness *nativeAcceptanceWitness) stop(t *testing.T, session *nativeAcceptanceSession, label string) harness.TaskResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer cancel()
	if session.Close(ctx) != nil {
		t.Fatal("close real native process failed")
	}
	result, err := session.Wait(ctx)
	if err != nil {
		t.Fatal("wait for real native process and output drain failed")
	}
	session.stopped = true
	process := result.Process
	if (!process.Terminated && !process.Success()) || process.SinkError != nil || process.OutputError != nil || process.TerminationError != nil || process.ContainmentError != nil || process.OutputTruncated {
		t.Fatal("native process did not complete the physical stop and full output-drain barrier")
	}
	if result.Usage.State != harness.UsageUnknown || result.Usage.CostMicrousd != "" {
		t.Fatal("native candidate result did not preserve unknown provider usage and pricing")
	}
	witness.record(t, map[string]any{
		"class": "physical_stop_and_drain", "label": label, "pid": process.PID, "exit_code": process.ExitCode,
		"started_at": process.StartedAt, "finished_at": process.FinishedAt, "terminated": process.Terminated,
		"output_truncated": process.OutputTruncated, "sink_error": false, "output_error": false, "termination_error": false,
		"containment_error": false, "turn_status": result.Kind, "usage": result.Usage.State,
	})
	return result
}

func (witness *nativeAcceptanceWitness) assertWrites(t *testing.T, session *nativeAcceptanceSession, expectedPath string) {
	t.Helper()
	session.mutex.Lock()
	defer session.mutex.Unlock()
	writes := 0
	for _, data := range session.frames {
		var frame struct {
			Type     string `json:"type"`
			ToolName string `json:"toolName"`
			Args     struct {
				Path string `json:"path"`
			} `json:"args"`
		}
		if json.Unmarshal(data, &frame) != nil || frame.Type != "tool_execution_start" {
			continue
		}
		if frame.ToolName != "write" {
			t.Fatal("bounded repository task invoked a non-write tool")
		}
		path := filepath.Clean(frame.Args.Path)
		if filepath.IsAbs(path) {
			var err error
			path, err = filepath.Rel(filepath.Join(witness.root, "workspace"), path)
			if err != nil {
				t.Fatal("native write tool path has invalid workspace provenance")
			}
		}
		if path != expectedPath {
			t.Fatal("native repository task invoked write for an unexpected artifact")
		}
		writes++
	}
	if writes != 1 {
		t.Fatal("native repository artifact was not written exactly once")
	}
	witness.record(t, map[string]any{"class": "native_effect_count", "artifact": expectedPath, "tool_execution_start_count": writes, "duplicate_effects": false})
}

func (witness *nativeAcceptanceWitness) assertArtifacts(t *testing.T, expected map[string]string) {
	t.Helper()
	var baseline struct {
		Commit string `json:"git_commit"`
		Tree   string `json:"git_tree"`
	}
	witness.read(t, "baseline.json", &baseline)
	if strings.TrimSpace(witness.git(t, "rev-parse", "HEAD")) != baseline.Commit || strings.TrimSpace(witness.git(t, "rev-parse", "HEAD^{tree}")) != baseline.Tree {
		t.Fatal("native task changed the baseline repository commit or Git tree")
	}
	paths := make([]string, 0, len(expected))
	for path, contents := range expected {
		data, err := os.ReadFile(filepath.Join(witness.root, "workspace", path))
		if err != nil || string(data) != contents || witness.journal.check(data) != nil {
			t.Fatal("native artifact did not retain the exact requested bytes")
		}
		digest := sha256.Sum256(data)
		witness.record(t, map[string]any{"class": "artifact_bytes", "path": path, "bytes": string(data), "size": len(data), "sha256": hex.EncodeToString(digest[:]), "baseline_commit": baseline.Commit, "baseline_git_tree": baseline.Tree})
		paths = append(paths, "?? "+path)
	}
	sort.Strings(paths)
	status := witness.git(t, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching")
	actual := make([]string, 0)
	if status != "" {
		actual = strings.Split(strings.TrimSuffix(status, "\n"), "\n")
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(actual, paths) {
		t.Fatal("native task changed unexpected repository paths")
	}
}

type nativeAcceptanceToolIdentity struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

type nativeAcceptanceToolReady struct {
	Nonce     string                       `json:"nonce"`
	Tool      nativeAcceptanceToolIdentity `json:"tool"`
	Child     nativeAcceptanceToolIdentity `json:"child"`
	ExpiresAt time.Time                    `json:"expires_at"`
}

type nativeAcceptanceToolConfig struct {
	Endpoint string `json:"endpoint"`
	Nonce    string `json:"nonce"`
	Ready    string `json:"ready"`
	Artifact string `json:"artifact"`
}

func nativeAcceptanceSyncJSON(filename string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

// TestNativeAcceptanceCancelToolHelper is the actual executable requested from
// native bash, not a narrated/prompt-level claim. It owns exactly one child;
// both publish production creation identities before the fsynced handshake.
// Its deadline is a resource bound, never evidence that cancellation worked.
func TestNativeAcceptanceCancelToolHelper(t *testing.T) {
	var role, filename string
	for index, argument := range os.Args {
		if argument == "--" && index+3 == len(os.Args) {
			role, filename = os.Args[index+1], os.Args[index+2]
			break
		}
	}
	if role != "omp-cancel-tool" && role != "omp-cancel-child" {
		t.Skip("subprocess-only cancellation tool identity helper")
	}
	data, err := os.ReadFile(filename)
	var cfg nativeAcceptanceToolConfig
	if err != nil || json.Unmarshal(data, &cfg) != nil || !filepath.IsAbs(cfg.Ready) || !filepath.IsAbs(cfg.Artifact) || len(cfg.Nonce) != 32 {
		t.Fatal("invalid local cancellation helper configuration")
	}
	host, _, err := net.SplitHostPort(cfg.Endpoint)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("cancellation helper handshake must remain loopback-local")
	}
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal("capture cancellation helper creation identity failed")
	}
	self := nativeAcceptanceToolIdentity{PID: os.Getpid(), Identity: identity}
	if role == "omp-cancel-child" {
		if json.NewEncoder(os.Stdout).Encode(self) != nil {
			t.Fatal("publish cancellation descendant identity failed")
		}
		timer := time.NewTimer(nativeAcceptanceToolTimeout)
		defer timer.Stop()
		<-timer.C
		if os.WriteFile(cfg.Artifact, []byte("late mutation\n"), 0o600) != nil {
			t.Fatal("surviving cancellation descendant could not write its artifact")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("resolve cancellation descendant executable failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceToolTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestNativeAcceptanceCancelToolHelper$", "--", "omp-cancel-child", filename)
	command.Env, err = execution.BuildEnvironment()
	if err != nil {
		t.Fatal("build credential-free cancellation descendant environment failed")
	}
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		t.Fatal("start cancellation descendant failed")
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	deadline, _ := ctx.Deadline()
	ready := nativeAcceptanceToolReady{Nonce: cfg.Nonce, Tool: self, ExpiresAt: deadline.UTC()}
	if json.NewDecoder(io.LimitReader(output, 4096)).Decode(&ready.Child) != nil || ready.Child.PID != command.Process.Pid {
		t.Fatal("cancellation descendant did not publish its actual PID")
	}
	if current, err := platform.ProcessIdentity(ready.Child.PID); err != nil || current != ready.Child.Identity {
		t.Fatal("cancellation descendant creation identity did not match the live OS process")
	}
	if nativeAcceptanceSyncJSON(cfg.Ready, ready) != nil {
		t.Fatal("fsync cancellation tool readiness failed")
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", cfg.Endpoint)
	if err != nil {
		t.Fatal("connect cancellation tool readiness handshake failed")
	}
	defer connection.Close()
	if connection.SetDeadline(deadline) != nil || json.NewEncoder(connection).Encode(ready) != nil {
		t.Fatal("publish fsynced cancellation tool readiness failed")
	}
	var acknowledgement string
	if json.NewDecoder(io.LimitReader(connection, 4096)).Decode(&acknowledgement) != nil || acknowledgement != cfg.Nonce || json.NewEncoder(connection).Encode("armed") != nil {
		t.Fatal("cancellation tool readiness was not acknowledged")
	}
	err = command.Wait()
	waited = true
	if err != nil {
		t.Fatal("cancellation helper reached its resource bound or descendant failed")
	}
}

type nativeAcceptanceToolObserver struct {
	process      nativeAcceptanceToolIdentity
	selfIdentity string
	exited       chan struct{}
	waitErr      error
}

func nativeAcceptanceObserveTool(t *testing.T, identity nativeAcceptanceToolIdentity) *nativeAcceptanceToolObserver {
	t.Helper()
	if identity.PID <= 0 || identity.Identity == "" {
		t.Fatal("cancellation helper identity is incomplete")
	}
	selfIdentity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal("capture observer boot/namespace/creation identity failed")
	}
	observer := &nativeAcceptanceToolObserver{process: identity, selfIdentity: selfIdentity}
	if runtime.GOOS == "windows" {
		// Keep the kernel process handle from BEFORE cancellation. Wait observes
		// that exact process object even after PID recycling, not a PID lookup.
		process, err := os.FindProcess(identity.PID)
		if err != nil {
			t.Fatal("open cancellation helper kernel process handle failed")
		}
		t.Cleanup(func() { _ = process.Release() })
		if current, err := platform.ProcessIdentity(identity.PID); err != nil || current != identity.Identity {
			t.Fatal("cancellation helper changed identity while opening its kernel process handle")
		}
		observer.exited = make(chan struct{})
		go func() {
			state, err := process.Wait()
			if err != nil {
				observer.waitErr = err
			} else if !state.Exited() {
				observer.waitErr = errors.New("cancellation helper kernel wait did not report exit")
			}
			close(observer.exited)
		}()
	}
	if current, err := platform.ProcessIdentity(identity.PID); err != nil || current != identity.Identity {
		t.Fatal("cancellation helper readiness does not identify a live exact OS process")
	}
	return observer
}

func (observer *nativeAcceptanceToolObserver) gone() error {
	if observer.exited != nil {
		select {
		case <-observer.exited:
			return observer.waitErr
		default:
			return errors.New("exact cancellation helper kernel process has not exited")
		}
	}
	current, err := platform.ProcessIdentity(observer.process.PID)
	if err == nil {
		if current == observer.process.Identity {
			return errors.New("exact cancellation helper OS creation identity is still present")
		}
		// A different strict boot/namespace/start-time identity proves that the
		// original task is gone even if its numeric PID has been recycled.
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		// Linux's production identity reader found no task/PID namespace entry.
		// Revalidate the observer too: missing shared boot/namespace facilities
		// must not be mistaken for absence of the target task. Permission,
		// parsing, and every other lookup error remain failures.
		if self, selfErr := platform.ProcessIdentity(os.Getpid()); selfErr != nil || self != observer.selfIdentity {
			return errors.New("cancellation observer boot/namespace identity is unavailable or changed")
		}
		return nil
	}
	return fmt.Errorf("cancellation helper identity absence is unproven: %w", err)
}

func (observer *nativeAcceptanceToolObserver) waitGone(ctx context.Context) error {
	if observer.exited != nil {
		select {
		case <-observer.exited:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return observer.gone()
}

type nativeAcceptanceCancelFixture struct {
	config     nativeAcceptanceToolConfig
	filename   string
	executable string
	listener   *net.TCPListener
}

func nativeAcceptanceNewCancelFixture(t *testing.T, root, workspace string) *nativeAcceptanceCancelFixture {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal("listen for local cancellation helper readiness failed")
	}
	t.Cleanup(func() { _ = listener.Close() })
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal("create cancellation helper handshake challenge failed")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("resolve cancellation tool executable failed")
	}
	fixture := &nativeAcceptanceCancelFixture{
		config: nativeAcceptanceToolConfig{
			Endpoint: listener.Addr().String(), Nonce: hex.EncodeToString(nonce[:]),
			Ready: filepath.Join(root, "cancel-tool-ready.json"), Artifact: filepath.Join(workspace, nativeAcceptanceCancelledArtifact),
		},
		filename: filepath.Join(root, "cancel-tool-config.json"), executable: executable, listener: listener,
	}
	if nativeAcceptanceSyncJSON(fixture.filename, fixture.config) != nil {
		t.Fatal("persist local cancellation tool configuration failed")
	}
	return fixture
}

func (fixture *nativeAcceptanceCancelFixture) shellCommand() string {
	quote := func(value string) string {
		return "'" + strings.ReplaceAll(filepath.ToSlash(value), "'", "'\"'\"'") + "'"
	}
	return "exec " + quote(fixture.executable) + " '-test.run=^TestNativeAcceptanceCancelToolHelper$' -- omp-cancel-tool " + quote(fixture.filename)
}

func (fixture *nativeAcceptanceCancelFixture) ready(t *testing.T) (nativeAcceptanceToolReady, []*nativeAcceptanceToolObserver) {
	t.Helper()
	deadline := time.Now().Add(nativeAcceptanceStopTimeout)
	if fixture.listener.SetDeadline(deadline) != nil {
		t.Fatal("bound cancellation helper readiness failed")
	}
	connection, err := fixture.listener.AcceptTCP()
	if err != nil {
		t.Fatal("actual cancellation tool did not complete its bounded local readiness handshake")
	}
	defer connection.Close()
	if connection.SetDeadline(deadline) != nil {
		t.Fatal("bound cancellation helper handshake failed")
	}
	var ready, persisted nativeAcceptanceToolReady
	if json.NewDecoder(io.LimitReader(connection, 4096)).Decode(&ready) != nil || ready.Nonce != fixture.config.Nonce || ready.Tool.PID == ready.Child.PID || !time.Now().Before(ready.ExpiresAt) {
		t.Fatal("cancellation tool readiness challenge or process identities are invalid")
	}
	data, err := os.ReadFile(fixture.config.Ready)
	if err != nil || json.Unmarshal(data, &persisted) != nil || ready != persisted {
		t.Fatal("cancellation tool readiness does not match its fsynced local record")
	}
	observers := []*nativeAcceptanceToolObserver{nativeAcceptanceObserveTool(t, ready.Tool), nativeAcceptanceObserveTool(t, ready.Child)}
	if json.NewEncoder(connection).Encode(fixture.config.Nonce) != nil {
		t.Fatal("acknowledge exact cancellation tool readiness failed")
	}
	var armed string
	if json.NewDecoder(io.LimitReader(connection, 4096)).Decode(&armed) != nil || armed != "armed" {
		t.Fatal("actual cancellation tool did not acknowledge the readiness handshake")
	}
	for _, observer := range observers {
		if current, err := platform.ProcessIdentity(observer.process.PID); err != nil || current != observer.process.Identity {
			t.Fatal("exact cancellation tool or descendant exited before cancellation")
		}
	}
	return ready, observers
}

// TestNativeAcceptanceCancelToolOracle exercises the actual helper/descendant
// and production process owner without a provider, prompt, or native OMP run.
// In particular, absent artifacts must not let either live identity pass.
func TestNativeAcceptanceCancelToolOracle(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("cancellation identity witness requires Windows or Linux")
	}
	root := t.TempDir()
	fixture := nativeAcceptanceNewCancelFixture(t, root, root)
	environment, err := execution.BuildEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	process, err := execution.NewRunner().Start(context.Background(), execution.Invocation{
		Program: fixture.executable, Args: []string{"-test.run=^TestNativeAcceptanceCancelToolHelper$", "--", "omp-cancel-tool", fixture.filename},
		Dir: root, Env: environment,
	}, execution.SinkFunc(func(context.Context, execution.Event) error { return nil }))
	if process != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
			defer cancel()
			if err := process.Terminate(ctx, 0); err != nil {
				t.Errorf("cleanup cancellation oracle process tree: %v", err)
			}
		})
	}
	if err != nil || process == nil {
		t.Fatalf("start cancellation oracle process tree: %v", err)
	}
	ready, observers := fixture.ready(t)
	if pid, identity := process.ProcessDetails(); pid != ready.Tool.PID || identity != ready.Tool.Identity {
		t.Fatal("oracle helper readiness did not bind the production runner's exact process")
	}
	if _, err := os.Lstat(fixture.config.Artifact); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("oracle setup unexpectedly has a late artifact")
	}
	for _, observer := range observers {
		if observer.gone() == nil {
			t.Fatal("absent artifact falsely proved a live exact helper/descendant was gone")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer cancel()
	if err := process.Terminate(ctx, 0); err != nil {
		t.Fatalf("stop cancellation oracle process tree: %v", err)
	}
	result := process.Wait()
	if !result.Terminated || result.SinkError != nil || result.OutputError != nil || result.TerminationError != nil || result.ContainmentError != nil || result.OutputTruncated {
		t.Fatalf("cancellation oracle failed production stop/drain barrier: %+v", result)
	}
	for _, observer := range observers {
		if err := observer.waitGone(ctx); err != nil {
			t.Fatalf("exact cancellation oracle helper/descendant exit is unproven: %v", err)
		}
	}
	if !time.Now().Before(ready.ExpiresAt) {
		t.Fatal("oracle helper resource deadline expired before independent termination proof")
	}
	if _, err := os.Lstat(fixture.config.Artifact); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled oracle helper/descendant produced a late artifact")
	}
}

func (witness *nativeAcceptanceWitness) cancel(t *testing.T, retained map[string]string) {
	t.Helper()
	session, _ := witness.open(t, "in-flight-cancel", nil, "read,write,bash")
	fixture := nativeAcceptanceNewCancelFixture(t, witness.root, filepath.Join(witness.root, "workspace"))
	// Neither native narration nor the start frame alone proves tool liveness.
	// The exact command must also complete the fsynced local helper handshake.
	shell := fixture.shellCommand()
	session.mutex.Lock()
	session.expectedBashCommand = shell
	session.mutex.Unlock()
	goal := "Use the bash tool exactly once to execute this exact bounded command with a 60-second timeout. Do not invoke any other tool or write any other file. The owner will cancel the running command:\n" + shell
	ctx, cancel := context.WithTimeout(context.Background(), nativeAcceptanceTimeout)
	defer cancel()
	started := make(chan error, 1)
	startConsumed := false
	go func() {
		started <- session.StartTurn(ctx, harness.TurnRequest{Goal: goal, Context: json.RawMessage(`{"schema_version":"omp-native-cancel.v1"}`)})
	}()
	select {
	case <-session.bashStarted:
	case err := <-started:
		startConsumed = true
		if err != nil {
			t.Fatal("native cancellation task failed before starting the actual tool")
		}
		select {
		case <-session.bashStarted:
		case <-ctx.Done():
			t.Fatal("no native bash tool_execution_start observed for cancellation")
		}
	case <-ctx.Done():
		t.Fatal("no actual in-flight native tool observed before cancellation deadline")
	}
	ready, observers := fixture.ready(t)
	witness.record(t, map[string]any{"class": "cancel_tool_live_before_control", "readiness": ready, "readiness_fsynced": true, "exact_native_bash_command": shell})
	receipt, err := session.Control(ctx, harness.ControlRequest{CommandID: "native-in-flight-cancel", Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatal("native cancellation was not applied to the observed in-flight tool")
	}
	witness.record(t, map[string]any{"class": "cancel_after_native_tool_start", "control_receipt": receipt, "tool": "bash", "shell_command": shell})
	result := witness.stop(t, session, "in-flight-cancel")
	if result.Kind != harness.ResultCancelled {
		t.Fatal("native cancellation did not yield a cancelled turn")
	}
	proofContext, proofCancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
	defer proofCancel()
	for _, observer := range observers {
		if err := observer.waitGone(proofContext); err != nil {
			t.Fatalf("exact native bash helper/descendant survived stop/drain or exit is unproven: %v", err)
		}
	}
	// An automatic helper/tool timeout is not native cancellation evidence.
	// This guard only rejects expired witnesses; OS identities prove exit.
	if !time.Now().Before(ready.ExpiresAt) {
		t.Fatal("cancellation helper resource deadline expired before independent termination proof")
	}
	witness.record(t, map[string]any{"class": "cancel_tool_exact_identity_terminated", "readiness": ready, "after_control_applied_and_stop_drain": true, "proof": map[string]string{"windows": "pre_cancel_kernel_process_handle_wait", "linux": "production_creation_identity_absent_or_replaced"}[runtime.GOOS]})
	if _, err := os.Lstat(filepath.Join(witness.root, "workspace", nativeAcceptanceCancelledArtifact)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled native tool produced a forbidden late artifact")
	}
	cancel()
	if !startConsumed {
		joinContext, joinCancel := context.WithTimeout(context.Background(), nativeAcceptanceStopTimeout)
		defer joinCancel()
		select {
		case <-started:
		case <-joinContext.Done():
			t.Fatal("cancelled native StartTurn goroutine did not exit after stop/drain")
		}
	}
	witness.assertArtifacts(t, retained)
	witness.record(t, map[string]any{"class": "cancel_no_later_mutation", "exact_tool_and_descendant_terminated_before_artifact_check": true, "cancelled_artifact_absent": true, "retained_artifacts_unchanged": true, "usage": harness.UsageUnknown})
}
