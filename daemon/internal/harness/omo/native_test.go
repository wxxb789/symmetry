package omo

import (
	"bufio"
	"bytes"
	"context"
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
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const nativeAcceptanceResource = "00000000-0000-4000-8000-00000000006b"

type nativeAcceptanceArtifact struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type nativeAcceptanceScenario struct {
	Name                         string                      `json:"name"`
	AdmissionID                  string                      `json:"admission_id"`
	LocalHandleID                string                      `json:"local_handle_id"`
	Handle                       harness.NativeSessionHandle `json:"local_only_handle"`
	SessionDirectory             string                      `json:"local_only_session_directory"`
	WorkspaceFingerprint         string                      `json:"workspace_fingerprint"`
	Argv                         []string                    `json:"argv"`
	EnvironmentNames             []string                    `json:"environment_names"`
	PID                          int                         `json:"pid"`
	ProcessIdentity              string                      `json:"process_identity"`
	ProcessExitCode              int                         `json:"process_exit_code"`
	ProcessStartedAt             time.Time                   `json:"process_started_at"`
	ProcessFinishedAt            time.Time                   `json:"process_finished_at"`
	ProcessTerminated            bool                        `json:"process_terminated"`
	OutputTruncated              bool                        `json:"output_truncated"`
	StderrBytes                  int                         `json:"stderr_bytes"`
	ProcessErrorTypes            map[string]string           `json:"process_error_types"`
	ProcessPersistedBeforeEvents bool                        `json:"process_persisted_before_events"`
	HandleReopenedBeforeTurn     bool                        `json:"handle_reopened_before_turn"`
	PromptAccepted               bool                        `json:"prompt_accepted"`
	CancelOutcome                harness.ControlOutcome      `json:"cancel_outcome,omitempty"`
	IdleAfterCancel              bool                        `json:"idle_after_cancel"`
	ReadToolCompleted            bool                        `json:"read_tool_completed"`
	WriteToolCompleted           bool                        `json:"write_tool_completed"`
	WaitTurn                     string                      `json:"wait_turn"`
	Close                        string                      `json:"close"`
	Wait                         string                      `json:"wait"`
	ResultKind                   harness.ResultKind          `json:"result_kind"`
	SemanticKind                 protocol.TaskResultKind     `json:"semantic_kind,omitempty"`
	Usage                        harness.Usage               `json:"usage"`
	ProcessStopped               bool                        `json:"process_stopped"`
	OutputDrained                bool                        `json:"output_drained"`
	NativeFrames                 int                         `json:"native_frames"`
	LastNativeFrames             []string                    `json:"last_native_frames,omitempty"`
	TaskResults                  int                         `json:"task_results"`
	Artifacts                    []nativeAcceptanceArtifact  `json:"artifacts,omitempty"`
	ErrorClasses                 map[string]string           `json:"error_classes"`
}

type nativeAcceptanceReceipt struct {
	SchemaVersion            int                         `json:"schema_version"`
	Passed                   bool                        `json:"passed"`
	Failures                 []string                    `json:"failures"`
	Command                  string                      `json:"executed_command"`
	Platform                 string                      `json:"platform"`
	Architecture             string                      `json:"architecture"`
	Kernel                   string                      `json:"kernel"`
	Distribution             string                      `json:"distribution"`
	Executable               string                      `json:"binary_path"`
	ExecutableSize           int64                       `json:"binary_size"`
	ExecutableDigest         string                      `json:"binary_digest"`
	NativeVersion            string                      `json:"native_version"`
	Root                     string                      `json:"retained_private_root"`
	AuthorizedOrigin         string                      `json:"authorized_origin"`
	SourceRepository         string                      `json:"local_only_source_repository"`
	Workspace                string                      `json:"local_only_workspace"`
	Subject                  protocol.Subject            `json:"subject"`
	SubjectHash              string                      `json:"subject_hash"`
	CommittedArtifact        nativeAcceptanceArtifact    `json:"committed_artifact"`
	WrongDigestRejected      bool                        `json:"wrong_digest_rejected"`
	RetainedHandlePreserved  bool                        `json:"retained_handle_preserved"`
	FreshHandleDistinct      bool                        `json:"fresh_handle_distinct"`
	FreshPreservedOldSession bool                        `json:"fresh_preserved_old_session"`
	CredentialScan           string                      `json:"credential_scan"`
	Scenarios                []*nativeAcceptanceScenario `json:"scenarios"`
	Limitations              []string                    `json:"limitations"`
}

// TestNativeAcceptance exercises the pinned public adapter and real Runner.
// Its retained files and receipt are machine-local.
func TestNativeAcceptance(t *testing.T) {
	if os.Getenv("SYMMETRY_OMO_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("set SYMMETRY_OMO_NATIVE_ACCEPTANCE=1 for the credentialed native acceptance")
	}
	report := &nativeAcceptanceReceipt{
		SchemaVersion: 1, Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		Command:  "go test ./internal/harness/omo -run '^TestNativeAcceptance$' -count=1 -v -timeout=15m",
		Failures: []string{}, Scenarios: []*nativeAcceptanceScenario{},
		Limitations: []string{
			"Pinned public adapter and Runner lifecycle evidence, restricted to the read/write profile.",
			"Local Runner containment and journals only; no Control admission, stop certificate, or cross-machine resume is asserted.",
			"Journal evidence is file flush plus independent reopen/readback, not a crash or power-loss experiment.",
			"No native steer, pause, approvals, provider broker, authoritative usage, or hard cost claim.",
			"Native rule-driven interventions are not verified; uncorrelated work remains fail-closed.",
			"No delivery-branch commits, fixture commits, network clone, fetch, push, or remote mutation.",
		},
	}
	require := func(ok bool, code string) {
		t.Helper()
		if !ok {
			report.Failures = append(report.Failures, code)
			t.Fatal(code)
		}
	}
	required := func(name string) string {
		t.Helper()
		value := os.Getenv(name)
		require(strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0), "required_environment_missing:"+name)
		return value
	}
	executable := required("SYMMETRY_OMO_NATIVE_EXECUTABLE")
	source := required("SYMMETRY_OMO_NATIVE_REPOSITORY")
	output := required("SYMMETRY_OMO_NATIVE_OUTPUT")
	key := required("SYMMETRY_OMO_PROBE_KEY")
	baseURL := required("SYMMETRY_OMO_PROBE_URL")
	require(filepath.IsAbs(executable) && filepath.IsAbs(source) && filepath.IsAbs(output), "paths_must_be_absolute")
	endpoint, err := url.Parse(baseURL)
	require(err == nil && endpoint != nil && endpoint.Scheme == "https" && endpoint.Hostname() != "" && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == "", "provider_url_must_be_nonsecret_https")
	if endpoint != nil {
		host := strings.ToLower(endpoint.Hostname())
		ip := net.ParseIP(host)
		require(host != "localhost" && !strings.HasSuffix(host, ".localhost") && (ip == nil || !ip.IsLoopback()), "loopback_provider_forbidden")
	}
	require(os.MkdirAll(filepath.Dir(output), 0o700) == nil, "receipt_directory_create_failed")
	_, err = os.Lstat(output)
	require(errors.Is(err, os.ErrNotExist), "receipt_must_be_new")
	root, err := os.MkdirTemp("", "symmetry-omo-native-")
	require(err == nil, "private_root_create_failed")
	require(os.Chmod(root, 0o700) == nil, "private_root_mode_failed")
	report.Root, report.Executable, report.SourceRepository = root, executable, source
	// This defer runs on failed assertions too, but never publishes native errors
	// or a receipt whose bytes (or retained root) contain the credential.
	defer func() {
		report.Passed = !t.Failed()
		report.CredentialScan = "passed"
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil || bytes.Contains(encoded, []byte(key)) || nativeAcceptanceScan(root, []byte(key)) != nil {
			t.Error("credential_scan_or_receipt_encoding_failed")
			return
		}
		if nativeAcceptanceWrite(output, append(encoded, '\n')) != nil {
			t.Error("receipt_write_failed")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	for _, directory := range []string{"agent", "home", "tmp", "sessions", "journals"} {
		require(os.Mkdir(filepath.Join(root, directory), 0o700) == nil, "private_directory_create_failed")
	}
	environment := nativeAcceptanceEnvironment(root, key)
	command := func(program string, args ...string) ([]byte, error) {
		bounded, stop := context.WithTimeout(ctx, 120*time.Second)
		defer stop()
		cmd := exec.CommandContext(bounded, program, args...)
		cmd.Env = nativeAcceptanceWithoutKey(environment)
		return cmd.Output()
	}
	var expectedPath, expectedDigest string
	var expectedSize int64
	switch runtime.GOOS {
	case "windows":
		expectedPath, expectedSize = `C:/Users/lhan/.local/bin/omo.exe`, 202352128
		expectedDigest = "sha256:04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464"
		kernel, commandErr := command("cmd.exe", "/d", "/c", "ver")
		require(commandErr == nil, "windows_kernel_identity_failed")
		report.Kernel, report.Distribution = strings.TrimSpace(string(kernel)), "Windows"
	case "linux":
		expectedPath, expectedSize = "/root/.local/bin/omo", 211998896
		expectedDigest = "sha256:3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444"
		kernel, commandErr := command("uname", "-sr")
		require(commandErr == nil, "linux_kernel_identity_failed")
		report.Kernel = strings.TrimSpace(string(kernel))
		distribution, readErr := os.ReadFile("/etc/os-release")
		require(readErr == nil, "linux_distribution_identity_failed")
		fields := map[string]string{}
		for _, line := range strings.Split(string(distribution), "\n") {
			if name, value, ok := strings.Cut(line, "="); ok {
				fields[name] = strings.Trim(value, `"`)
			}
		}
		require(fields["ID"] == "ubuntu" && fields["VERSION_ID"] == "24.04", "linux_distribution_not_pinned")
		report.Distribution = "Ubuntu-24.04"
	default:
		require(false, "platform_not_pinned")
	}
	actualPath, pathErr := filepath.EvalSymlinks(executable)
	pinPath, pinErr := filepath.EvalSymlinks(filepath.FromSlash(expectedPath))
	require(pathErr == nil && pinErr == nil && nativeAcceptanceSamePath(actualPath, pinPath), "binary_path_not_pinned")
	info, err := os.Stat(actualPath)
	require(err == nil && info.Mode().IsRegular() && info.Size() == expectedSize, "binary_size_not_pinned")
	report.ExecutableSize = info.Size()
	file, err := os.Open(actualPath)
	require(err == nil, "binary_open_failed")
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	require(copyErr == nil && closeErr == nil, "binary_digest_failed")
	report.ExecutableDigest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	require(report.ExecutableDigest == expectedDigest, "binary_digest_not_pinned")
	version, err := command(executable, "--version")
	require(err == nil && strings.TrimSuffix(strings.TrimSuffix(string(version), "\n"), "\r") == "omo 5.1.69 (engine: senpi 2026.11.10-2; scheme nodef)", "binary_version_not_pinned")
	report.NativeVersion = "5.1.69"
	git := func(directory string, args ...string) ([]byte, error) {
		return command("git", append([]string{"--no-replace-objects", "-C", directory}, args...)...)
	}
	origin, err := git(source, "remote", "get-url", "origin")
	require(err == nil && nativeAcceptanceAuthorizedOrigin(strings.TrimSpace(string(origin))), "fixture_origin_not_authorized")
	report.AuthorizedOrigin = "wxxb789/symmetry-0006h-provider-fixture"
	baseline, err := git(source, "rev-parse", "--verify", "HEAD^{commit}")
	require(err == nil, "fixture_baseline_unavailable")
	repository := filepath.Join(root, "repository")
	_, err = command("git", "-c", "protocol.file.allow=always", "clone", "--local", "--no-hardlinks", "--no-checkout", "--", source, repository)
	require(err == nil, "private_fixture_clone_failed")
	manager := workspace.New(map[string]config.Workspace{"native": {
		Policy: config.WorkspacePolicyGitWorktree, Repository: repository,
		Root: filepath.Join(root, "workspaces"), Ref: "HEAD", Cleanup: config.CleanupNever,
	}})
	subject, err := manager.MaterializeSubject(ctx, "native", nativeAcceptanceResource, strings.TrimSpace(string(baseline)))
	require(err == nil, "subject_materialization_failed")
	report.Subject = subject
	report.SubjectHash, err = subject.Hash()
	require(err == nil, "subject_hash_failed")
	readme, err := manager.ReadSubjectArtifact(ctx, "native", subject, "README.md")
	require(err == nil && len(readme.Content) != 0, "committed_readme_unavailable")
	report.CommittedArtifact = nativeAcceptanceArtifact{Path: readme.Path, Digest: readme.ContentDigest, Size: len(readme.Content)}
	wrong := subject
	wrong.TreeDigest = "sha256:" + strings.Repeat("0", 64)
	_, err = manager.ReadSubjectArtifact(ctx, "native", wrong, "README.md")
	report.WrongDigestRejected = errors.Is(err, workspace.ErrSubjectMismatch)
	require(report.WrongDigestRejected, "wrong_subject_digest_accepted")
	prepared, err := manager.PrepareSubject(ctx, "native", workspace.RunRef{RunID: "omo-native-acceptance", Generation: 1}, subject)
	require(err == nil, "subject_workspace_prepare_failed")
	report.Workspace = prepared.Prepared.Path
	fingerprint, err := workspace.Fingerprint(ctx, prepared.Prepared)
	require(err == nil, "workspace_fingerprint_failed")
	modelConfig := map[string]any{"providers": map[string]any{"tencent-litellm": map[string]any{
		"api": "openai-completions", "baseUrl": baseURL, "apiKey": "$SYMMETRY_OMO_PROBE_KEY",
		"models": []any{map[string]any{"id": "deepseek-v4.1-flash-fast", "reasoning": false, "contextWindow": 1000000, "maxTokens": 4096}},
	}}}
	configBytes, err := json.Marshal(modelConfig)
	require(err == nil && nativeAcceptanceWrite(filepath.Join(root, "agent", "models.json"), configBytes) == nil, "isolated_model_configuration_failed")
	contextJSON, err := json.Marshal(map[string]any{"subject": subject, "subject_hash": report.SubjectHash, "committed_readme_digest": readme.ContentDigest, "authorized_origin": report.AuthorizedOrigin})
	require(err == nil, "canonical_context_encode_failed")
	adapter := NewAdapter(executable)
	originalContent := []byte("native OmO acceptance evidence\n")
	resumeContent := append(append([]byte(nil), originalContent...), []byte("retained resume verified\n")...)
	handoffContent := append(append([]byte(nil), resumeContent...), []byte("fresh handoff verified\n")...)
	var original, resumed harness.NativeSessionHandle
	var originalLocal string
	var processIdentities = map[string]bool{}
	run := func(name, artifact string, content []byte, resume *harness.ResumeHandle, readFirst string) harness.NativeSessionHandle {
		t.Helper()
		currentFingerprint, fingerprintErr := workspace.Fingerprint(ctx, prepared.Prepared)
		require(fingerprintErr == nil && currentFingerprint == fingerprint, "workspace_identity_changed:"+name)
		if artifact != "" {
			_, artifactErr := os.Lstat(filepath.Join(report.Workspace, artifact))
			require(errors.Is(artifactErr, os.ErrNotExist), "native_artifact_already_exists:"+name)
		}
		var localID string
		if resume != nil {
			localID = resume.LocalHandleID
		} else {
			var idErr error
			localID, idErr = state.NewGoalSessionLocalHandleID()
			require(idErr == nil, "local_handle_id_failed:"+name)
		}
		scenario := &nativeAcceptanceScenario{Name: name, AdmissionID: "omo-native-" + name, LocalHandleID: localID, SessionDirectory: filepath.Join(root, "sessions"), WorkspaceFingerprint: fingerprint, ErrorClasses: map[string]string{}}
		report.Scenarios = append(report.Scenarios, scenario)
		journal, journalErr := os.OpenFile(filepath.Join(root, "journals", name+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require(journalErr == nil, "local_journal_open_failed:"+name)
		defer journal.Close()
		var journalMutex sync.Mutex
		processPersisted, handlePersisted := false, false
		appendJournal := func(value any) error {
			encoded, encodeErr := json.Marshal(value)
			if encodeErr != nil {
				return encodeErr
			}
			if _, writeErr := journal.Write(append(encoded, '\n')); writeErr != nil {
				return writeErr
			}
			return journal.Sync()
		}
		require(appendJournal(map[string]any{"type": "launch_intent", "admission_id": scenario.AdmissionID, "local_handle_id": localID, "resume": resume != nil, "fingerprint": fingerprint}) == nil, "local_launch_intent_failed:"+name)
		storePath := filepath.Join(root, "state-"+localID)
		store, storeErr := state.New(storePath)
		require(storeErr == nil, "state_open_failed:"+name)
		defer func() { _ = store.Close() }()
		key := state.GoalSessionKey{GoalID: "omo-native-acceptance", LocalHandleID: localID}
		if resume == nil {
			_, storeErr = store.SaveGoalSessionLaunchIntent(state.GoalSessionLaunchIntent{
				GoalID: key.GoalID, GoalRevision: 1, LocalHandleID: localID, TaskID: name,
				AdmissionID: scenario.AdmissionID, OwnerID: "native-acceptance-local-owner",
				HarnessKind: "omo", HarnessVersion: "5.1.69", AdapterVersion: ImplementationVersion,
				AdapterProtocolVersion: 1, WorkspaceFingerprint: fingerprint, WorkspacePath: report.Workspace,
				RepositoryResourceID: subject.ResourceID, SessionMode: state.GoalSessionModeFresh,
			})
			require(storeErr == nil, "goal_launch_intent_failed:"+name)
			_, storeErr = store.MarkGoalSessionLaunchStarted(key)
			require(storeErr == nil, "goal_launch_started_failed:"+name)
		} else {
			retained, loadErr := store.LoadGoalSession(key)
			require(loadErr == nil && retained.NativeSessionID == resume.NativeSessionID && retained.NativeSessionFilename == resume.NativeSessionFilename && retained.WorkspaceFingerprint == fingerprint && retained.HarnessVersion == "5.1.69", "retained_goal_journal_mismatch")
		}
		request := harness.StartRequest{AdmissionID: scenario.AdmissionID, LocalHandleID: localID, Workspace: report.Workspace, Resume: resume,
			Invocation: execution.Invocation{Env: environment, Args: []string{"--session-dir", scenario.SessionDirectory, "--provider", "tencent-litellm", "--model", "deepseek-v4.1-flash-fast", "--thinking", "off"}},
			PersistProcess: func(pid int, identity string) error {
				journalMutex.Lock()
				defer journalMutex.Unlock()
				if pid <= 0 || identity == "" {
					return errors.New("invalid_process_identity")
				}
				if persistErr := appendJournal(map[string]any{"type": "process", "pid": pid, "identity": identity}); persistErr != nil {
					return persistErr
				}
				scenario.PID, scenario.ProcessIdentity, processPersisted = pid, identity, true
				return nil
			},
			StartProcess: func(startCtx context.Context, invocation execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
				scenario.Argv = append([]string{invocation.Program}, invocation.Args...)
				for _, entry := range invocation.Env {
					variable, _, _ := strings.Cut(entry, "=")
					scenario.EnvironmentNames = append(scenario.EnvironmentNames, variable)
				}
				sort.Strings(scenario.EnvironmentNames)
				diagnosticDecoder := NewDecoder(defaultMaxRecordBytes)
				process, startErr := execution.NewRunner().Start(startCtx, invocation, execution.SinkFunc(func(eventCtx context.Context, event execution.Event) error {
					if event.Stream == execution.Stderr {
						journalMutex.Lock()
						scenario.StderrBytes += len(event.Data)
						journalMutex.Unlock()
					}
					if event.Stream == execution.Stdout {
						records, _ := diagnosticDecoder.Feed(event.Data)
						journalMutex.Lock()
						for _, record := range records {
							label := nativeAcceptanceFrameLabel(record)
							if len(scenario.LastNativeFrames) == 32 {
								copy(scenario.LastNativeFrames, scenario.LastNativeFrames[1:])
								scenario.LastNativeFrames = scenario.LastNativeFrames[:31]
							}
							scenario.LastNativeFrames = append(scenario.LastNativeFrames, label)
						}
						journalMutex.Unlock()
					}
					return sink.Handle(eventCtx, event)
				}))
				if process == nil {
					return nil, startErr
				}
				return process, startErr
			},
		}
		session, startErr := adapter.Start(ctx, request, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
			journalMutex.Lock()
			defer journalMutex.Unlock()
			if !processPersisted {
				return errors.New("event_before_process_journal")
			}
			if event.Kind == harness.EventSessionStarted && !handlePersisted {
				return errors.New("turn_before_handle_journal")
			}
			if event.Kind == harness.EventNativeFrame {
				scenario.NativeFrames++
			}
			if event.Kind == harness.EventTaskResult {
				scenario.TaskResults++
			}
			return appendJournal(map[string]any{"type": "event", "kind": event.Kind, "sequence": event.Sequence})
		}))
		scenario.ErrorClasses["start"] = nativeAcceptanceErrorClass(startErr)
		if session != nil {
			defer func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 25*time.Second)
				defer stop()
				if session.Close(cleanupCtx) != nil {
					t.Error("cleanup_close_failed:" + name)
				}
				if _, waitErr := session.Wait(cleanupCtx); waitErr != nil && startErr == nil && !t.Failed() {
					t.Error("cleanup_wait_failed:" + name)
				}
			}()
		}
		require(startErr == nil && session != nil, "native_start_failed:"+name)
		staged, ok := session.(harness.StagedSession)
		require(ok, "native_staging_missing:"+name)
		journalMutex.Lock()
		scenario.ProcessPersistedBeforeEvents = processPersisted
		journalMutex.Unlock()
		require(scenario.ProcessPersistedBeforeEvents, "process_persistence_missing:"+name)
		pid, identity := staged.ProcessDetails()
		require(pid == scenario.PID && identity == scenario.ProcessIdentity && !processIdentities[fmt.Sprintf("%d:%s", pid, identity)], "process_identity_reused:"+name)
		processIdentities[fmt.Sprintf("%d:%s", pid, identity)] = true
		handle, openErr := staged.Open(ctx)
		scenario.ErrorClasses["open"] = nativeAcceptanceErrorClass(openErr)
		require(openErr == nil && handle.ID != "" && filepath.IsAbs(handle.Filename), "native_open_failed:"+name)
		scenario.Handle = handle
		if resume == nil {
			_, storeErr = store.PersistGoalSessionHandle(key, state.GoalSessionHandle{NativeSessionID: handle.ID, NativeSessionFilename: handle.Filename})
			require(storeErr == nil, "goal_handle_persistence_failed:"+name)
		} else {
			require(handle.ID == resume.NativeSessionID && handle.Filename == resume.NativeSessionFilename, "resume_handle_changed")
		}
		require(store.Close() == nil, "goal_store_close_failed:"+name)
		store, storeErr = state.New(storePath)
		require(storeErr == nil, "goal_store_reopen_failed:"+name)
		reopened, loadErr := store.LoadGoalSession(key)
		require(loadErr == nil && reopened.NativeSessionID == handle.ID && reopened.NativeSessionFilename == handle.Filename && reopened.WorkspaceFingerprint == fingerprint, "goal_handle_readback_failed:"+name)
		journalMutex.Lock()
		persistErr := appendJournal(map[string]any{"type": "handle", "id": handle.ID, "filename": handle.Filename, "version": reopened.HarnessVersion, "fingerprint": fingerprint})
		handlePersisted = persistErr == nil
		scenario.HandleReopenedBeforeTurn = handlePersisted
		journalMutex.Unlock()
		require(persistErr == nil, "local_handle_persistence_failed:"+name)
		resultID, resultIDErr := state.NewGoalSessionLocalHandleID()
		require(resultIDErr == nil, "result_id_failed:"+name)
		summary := "Cancelled a streaming native turn without a semantic result."
		switch name {
		case "success":
			summary = "Inspected the fixture README and created the original evidence artifact without changing tracked source."
		case "resume":
			summary = "Reopened the retained native session, read its original artifact, and appended the required resume suffix into a new artifact."
		case "handoff":
			summary = "Used a distinct native session to read retained work and create the handoff artifact while preserving the previous session file."
		}
		expected := protocol.TaskResult{SchemaVersion: protocol.TaskResultSchemaVersion,
			ResultID: resultID, Kind: protocol.TaskResultProgress,
			Summary: summary, Subject: subject,
			SubjectHash: report.SubjectHash, EvidenceRefs: []string{}, Diagnostics: []protocol.Diagnostic{},
		}
		require(expected.Validate() == nil, "expected_result_invalid:"+name)
		expectedJSON, encodeErr := json.Marshal(expected)
		require(encodeErr == nil, "expected_result_encode_failed:"+name)
		goal := "Perform only this bounded task. Do not modify tracked files or run shell commands. "
		if readFirst == "" {
			goal += "Use read to inspect README.md, then use write to create " + artifact + " with exactly the following UTF-8 bytes including its trailing newline:\n" + string(content)
		} else {
			goal += "Use read to read " + readFirst + " from this same workspace. Use write to create " + artifact + " containing exactly those read bytes followed by "
			if name == "resume" {
				goal += "the UTF-8 text retained resume verified and one newline."
			} else {
				goal += "the UTF-8 text fresh handoff verified and one newline."
			}
		}
		goal += " Do not create or modify any other file. Return the following complete JSON as the entire final assistant text, without prose or fences:\n" + string(expectedJSON)
		if name == "cancel" {
			goal = "Do not use tools, write files, or return a TaskResult yet. Produce a long numbered explanation of the integers 1 through 20000, one detailed paragraph per integer, until interrupted."
		}
		turnErr := staged.StartTurn(ctx, harness.TurnRequest{Goal: goal, Context: contextJSON})
		scenario.ErrorClasses["start_turn"] = nativeAcceptanceErrorClass(turnErr)
		require(turnErr == nil, "native_prompt_not_accepted:"+name)
		scenario.PromptAccepted = true
		if name == "cancel" {
			control, controlErr := session.Control(ctx, harness.ControlRequest{CommandID: "omo-native-cancel", Kind: harness.ControlCancel})
			scenario.CancelOutcome, scenario.ErrorClasses["cancel"] = control.Outcome, nativeAcceptanceErrorClass(controlErr)
			require(controlErr == nil && control.Outcome == harness.ControlApplied, "native_cancel_not_applied")
			native, nativeOK := session.(*nativeSession)
			require(nativeOK, "native_cancel_state_unavailable")
			native.mutex.Lock()
			idleState, hasState := native.validator.SessionState()
			scenario.IdleAfterCancel = hasState && idleState.ValidateIdle() == nil && native.validator.idle && native.validator.abortDrained && len(native.pending) == 0
			native.mutex.Unlock()
			require(scenario.IdleAfterCancel, "native_cancel_left_pending_work")
		}
		turnErr = staged.WaitTurn(ctx)
		scenario.WaitTurn = nativeAcceptanceErrorClass(turnErr)
		require(turnErr == nil, "native_turn_failed:"+name+":"+scenario.WaitTurn)
		closeErr := session.Close(ctx)
		scenario.Close = nativeAcceptanceErrorClass(closeErr)
		require(closeErr == nil, "native_close_failed:"+name)
		result, waitErr := session.Wait(ctx)
		scenario.Wait, scenario.ResultKind, scenario.Usage = nativeAcceptanceErrorClass(waitErr), result.Kind, result.Usage
		scenario.ErrorClasses["process_wait"] = nativeAcceptanceErrorClass(result.Process.WaitError)
		scenario.ErrorClasses["containment"] = nativeAcceptanceErrorClass(result.Process.ContainmentError)
		scenario.ErrorClasses["termination"] = nativeAcceptanceErrorClass(result.Process.TerminationError)
		scenario.ErrorClasses["output"] = nativeAcceptanceErrorClass(result.Process.OutputError)
		scenario.ErrorClasses["sink"] = nativeAcceptanceErrorClass(result.Process.SinkError)
		scenario.ProcessExitCode, scenario.ProcessStartedAt, scenario.ProcessFinishedAt = result.Process.ExitCode, result.Process.StartedAt, result.Process.FinishedAt
		scenario.ProcessTerminated, scenario.OutputTruncated = result.Process.Terminated, result.Process.OutputTruncated
		scenario.ProcessErrorTypes = map[string]string{
			"wait":        fmt.Sprintf("%T", result.Process.WaitError),
			"sink":        fmt.Sprintf("%T", result.Process.SinkError),
			"output":      fmt.Sprintf("%T", result.Process.OutputError),
			"termination": fmt.Sprintf("%T", result.Process.TerminationError),
			"containment": fmt.Sprintf("%T", result.Process.ContainmentError),
		}
		scenario.ProcessStopped = !result.Process.FinishedAt.IsZero() && result.Process.PID == pid && result.Process.TerminationError == nil && result.Process.ContainmentError == nil
		scenario.OutputDrained = result.Process.OutputError == nil && result.Process.SinkError == nil && !result.Process.OutputTruncated
		require(waitErr == nil && scenario.ProcessStopped && scenario.OutputDrained, "native_physical_wait_failed:"+name)
		require(result.Usage.State == harness.UsageUnknown && result.Usage.CostMicrousd == "", "native_usage_must_remain_unknown:"+name)
		journalMutex.Lock()
		frames, results := scenario.NativeFrames, scenario.TaskResults
		journalMutex.Unlock()
		require(frames > 0, "native_events_missing:"+name)
		if name == "cancel" {
			require(result.Kind == harness.ResultCancelled && result.Semantic == nil && results == 0 && result.Reason != nil && *result.Reason == protocol.TaskResultReasonCancelled, "cancelled_turn_reported_semantic_success")
		} else {
			require(result.Kind == harness.ResultSucceeded && result.Semantic != nil && results == 1, "native_semantic_result_missing:"+name)
			scenario.SemanticKind = result.Semantic.Kind
			actualJSON, actualErr := json.Marshal(result.Semantic)
			require(actualErr == nil && bytes.Equal(actualJSON, expectedJSON), "native_semantic_contract_mismatch:"+name)
			actual, readErr := os.ReadFile(filepath.Join(report.Workspace, artifact))
			require(readErr == nil && bytes.Equal(actual, content), "native_artifact_bytes_mismatch:"+name)
			untracked, untrackedErr := git(report.Workspace, "ls-files", "--others", "--exclude-standard", "-z", "--", artifact)
			require(untrackedErr == nil && bytes.Equal(untracked, []byte(artifact+"\x00")), "native_artifact_not_untracked:"+name)
			scenario.Artifacts = append(scenario.Artifacts, nativeAcceptanceArtifact{Path: artifact, Digest: nativeAcceptanceDigest(actual), Size: len(actual)})
			readPath := readFirst
			if readPath == "" {
				readPath = "README.md"
			}
			scenario.ReadToolCompleted, scenario.WriteToolCompleted = nativeAcceptanceToolHistory(handle.Filename, report.Workspace, readPath, artifact)
			require(scenario.ReadToolCompleted && scenario.WriteToolCompleted, "native_read_write_history_missing:"+name)
		}
		require(nativeAcceptanceHeader(handle, report.Workspace) == nil, "native_session_header_mismatch:"+name)
		require(appendJournal(map[string]any{"type": "physical_stop", "pid": pid, "identity": identity, "result_kind": result.Kind}) == nil, "local_stop_journal_failed:"+name)
		// Reopen the flushed process/handle journal; in-memory flags alone are
		// deliberately not accepted as durability evidence.
		require(journal.Close() == nil, "local_journal_close_failed:"+name)
		require(nativeAcceptanceJournalReadback(filepath.Join(root, "journals", name+".jsonl"), pid, identity, handle) == nil, "local_journal_readback_failed:"+name)
		status, statusErr := git(report.Workspace, "status", "--porcelain", "--untracked-files=no")
		require(statusErr == nil && len(bytes.TrimSpace(status)) == 0, "tracked_fixture_changed:"+name)
		verified, verifyErr := manager.VerifySubject(ctx, prepared.Prepared, subject)
		require(verifyErr == nil && verified == subject, "baseline_subject_changed:"+name)
		if name == "success" {
			originalLocal = localID
		}
		return handle
	}
	original = run("success", "omo-native-original.txt", originalContent, nil, "")
	resumed = run("resume", "omo-native-resumed.txt", resumeContent, &harness.ResumeHandle{
		LocalHandleID: originalLocal, NativeSessionID: original.ID, NativeSessionFilename: original.Filename,
		NativeVersion: "5.1.69", WorkspaceFingerprint: fingerprint,
	}, "omo-native-original.txt")
	report.RetainedHandlePreserved = reflect.DeepEqual(original, resumed)
	require(report.RetainedHandlePreserved, "retained_native_identity_changed")
	oldSession, err := os.ReadFile(original.Filename)
	require(err == nil, "retained_session_unreadable")
	fresh := run("handoff", "omo-native-handoff.txt", handoffContent, nil, "omo-native-resumed.txt")
	report.FreshHandleDistinct = fresh.ID != original.ID && fresh.Filename != original.Filename
	require(report.FreshHandleDistinct, "fresh_handoff_reused_native_identity")
	oldAfterFresh, err := os.ReadFile(original.Filename)
	report.FreshPreservedOldSession = err == nil && bytes.Equal(oldSession, oldAfterFresh)
	require(report.FreshPreservedOldSession, "fresh_handoff_mutated_retained_session")
	run("cancel", "", nil, nil, "")
	for path, contents := range map[string][]byte{"omo-native-original.txt": originalContent, "omo-native-resumed.txt": resumeContent, "omo-native-handoff.txt": handoffContent} {
		actual, readErr := os.ReadFile(filepath.Join(report.Workspace, path))
		require(readErr == nil && bytes.Equal(actual, contents), "cancel_modified_evidence")
	}
}

func nativeAcceptanceEnvironment(root, key string) []string {
	var environment []string
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return append(environment, "HOME="+filepath.Join(root, "home"), "USERPROFILE="+filepath.Join(root, "home"),
		"APPDATA="+filepath.Join(root, "home"), "LOCALAPPDATA="+filepath.Join(root, "home"),
		"TMPDIR="+filepath.Join(root, "tmp"), "TMP="+filepath.Join(root, "tmp"), "TEMP="+filepath.Join(root, "tmp"),
		"OMO_CODING_AGENT_DIR="+filepath.Join(root, "agent"), "SYMMETRY_OMO_PROBE_KEY="+key,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
}

func nativeAcceptanceWithoutKey(environment []string) []string {
	var result []string
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "SYMMETRY_OMO_PROBE_KEY=") {
			result = append(result, entry)
		}
	}
	return result
}

func nativeAcceptanceAuthorizedOrigin(origin string) bool {
	switch origin {
	case "https://github.com/wxxb789/symmetry-0006h-provider-fixture", "https://github.com/wxxb789/symmetry-0006h-provider-fixture.git",
		"git@github.com:wxxb789/symmetry-0006h-provider-fixture.git", "ssh://git@github.com/wxxb789/symmetry-0006h-provider-fixture.git":
		return true
	default:
		return false
	}
}

func nativeAcceptanceSamePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func nativeAcceptanceDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func nativeAcceptanceWrite(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}

func nativeAcceptanceErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, ErrInvalidRecord):
		return "invalid_record"
	case errors.Is(err, ErrCorrelation):
		return "correlation"
	case errors.Is(err, ErrMissingResult):
		return "missing_result"
	case errors.Is(err, ErrUnknownOutcome):
		return "unknown_outcome"
	case errors.Is(err, ErrProtocol):
		return "protocol"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, ErrResumeRejected):
		return "resume_rejected"
	case errors.Is(err, harness.ErrUnsupportedVersion):
		return "unsupported_version"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	default:
		return "native_or_io_failure"
	}
}

func nativeAcceptanceFrameLabel(record Record) string {
	label := func(value string) string {
		if value == "" || len(value) > 64 {
			return "unrecognized"
		}
		for _, character := range value {
			if character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
				return "unrecognized"
			}
		}
		return value
	}
	typeName := label(record.Type)
	if record.Type == "extension_ui_request" {
		var method string
		_ = json.Unmarshal(record.Fields["method"], &method)
		typeName += ":" + label(method)
	}
	return typeName
}

func nativeAcceptanceHeader(handle harness.NativeSessionHandle, directory string) error {
	file, err := os.Open(handle.Filename)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return errors.New("session_header_unreadable")
	}
	var header struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
		ID      string `json:"id"`
		CWD     string `json:"cwd"`
	}
	if json.Unmarshal(line, &header) != nil || header.Type != "session" || header.Version != 3 || header.ID != handle.ID || !nativeAcceptanceSamePath(header.CWD, directory) {
		return errors.New("session_header_mismatch")
	}
	return nil
}

func nativeAcceptanceJournalReadback(path string, pid int, identity string, handle harness.NativeSessionHandle) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	process, opened, stopped := false, false, false
	for scanner.Scan() {
		var record struct {
			Type     string `json:"type"`
			PID      int    `json:"pid"`
			Identity string `json:"identity"`
			ID       string `json:"id"`
			Filename string `json:"filename"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return errors.New("invalid_local_journal")
		}
		switch record.Type {
		case "process":
			process = record.PID == pid && record.Identity == identity
		case "event":
			if !process {
				return errors.New("event_before_durable_process")
			}
		case "handle":
			opened = process && record.ID == handle.ID && record.Filename == handle.Filename
		case "physical_stop":
			stopped = opened && record.PID == pid && record.Identity == identity
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !process || !opened || !stopped {
		return errors.New("incomplete_local_journal")
	}
	return nil
}

// Read the retained native history locally, retaining only whether correlated
// read/write tool results completed. Neither message text nor tool output is
// copied into the receipt.
func nativeAcceptanceToolHistory(filename, directory, readPath, writePath string) (bool, bool) {
	file, err := os.Open(filename)
	if err != nil {
		return false, false
	}
	defer file.Close()
	readTarget, writeTarget := filepath.Join(directory, readPath), filepath.Join(directory, writePath)
	pending := map[string]string{}
	readDone, writeDone := false, false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role       string          `json:"role"`
				ToolCallID string          `json:"toolCallId"`
				IsError    *bool           `json:"isError"`
				Content    json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			return false, false
		}
		if entry.Type != "message" {
			continue
		}
		if entry.Message.Role == "assistant" {
			var blocks []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Arguments struct {
					Path string `json:"path"`
				} `json:"arguments"`
			}
			if json.Unmarshal(entry.Message.Content, &blocks) != nil {
				continue
			}
			for _, block := range blocks {
				if block.Type != "toolCall" || block.ID == "" || block.Arguments.Path == "" {
					continue
				}
				path := block.Arguments.Path
				if !filepath.IsAbs(path) {
					path = filepath.Join(directory, path)
				}
				if block.Name == "read" && nativeAcceptanceSamePath(path, readTarget) {
					pending[block.ID] = "read"
				}
				if block.Name == "write" && nativeAcceptanceSamePath(path, writeTarget) {
					pending[block.ID] = "write"
				}
			}
		} else if entry.Message.Role == "toolResult" && entry.Message.IsError != nil && !*entry.Message.IsError {
			switch pending[entry.Message.ToolCallID] {
			case "read":
				readDone = true
			case "write":
				writeDone = readDone
			}
			delete(pending, entry.Message.ToolCallID)
		}
	}
	if scanner.Err() != nil {
		return false, false
	}
	return readDone, writeDone
}

func TestNativeToolHistoryAcceptsBatchedCallsOnlyAfterReadCompletion(t *testing.T) {
	for _, readFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("read_first_%t", readFirst), func(t *testing.T) {
			directory := t.TempDir()
			filename := filepath.Join(directory, "session.jsonl")
			calls := map[string]any{"type": "message", "message": map[string]any{
				"role": "assistant", "content": []any{
					map[string]any{"type": "toolCall", "id": "read-1", "name": "read", "arguments": map[string]string{"path": "README.md"}},
					map[string]any{"type": "toolCall", "id": "write-1", "name": "write", "arguments": map[string]string{"path": "artifact.txt"}},
				},
			}}
			var content []byte
			appendRecord := func(record any) {
				encoded, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				content = append(append(content, encoded...), '\n')
			}
			appendRecord(calls)
			order := []string{"read-1", "write-1"}
			if !readFirst {
				order[0], order[1] = order[1], order[0]
			}
			for _, id := range order {
				appendRecord(map[string]any{"type": "message", "message": map[string]any{"role": "toolResult", "toolCallId": id, "isError": false}})
			}
			if err := os.WriteFile(filename, content, 0o600); err != nil {
				t.Fatal(err)
			}
			read, written := nativeAcceptanceToolHistory(filename, directory, "README.md", "artifact.txt")
			if !read || written != readFirst {
				t.Fatalf("history = read %t, write %t; read-first %t", read, written, readFirst)
			}
		})
	}
}

func nativeAcceptanceScan(root string, secret []byte) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("credential_scan_walk_failed")
		}
		if bytes.Contains([]byte(path), secret) {
			return errors.New("credential_in_path")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("credential_scan_symlink_refused")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("credential_scan_nonregular_file")
		}
		file, err := os.Open(path)
		if err != nil {
			return errors.New("credential_scan_open_failed")
		}
		defer file.Close()
		buffer := make([]byte, 64*1024+len(secret))
		carry := 0
		for {
			count, readErr := file.Read(buffer[carry:])
			window := buffer[:carry+count]
			if bytes.Contains(window, secret) {
				return errors.New("credential_found")
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return errors.New("credential_scan_read_failed")
			}
			carry = len(secret) - 1
			if carry > len(window) {
				carry = len(window)
			}
			copy(buffer, window[len(window)-carry:])
		}
	})
}
