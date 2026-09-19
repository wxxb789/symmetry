package pi

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	nativePiRetainedArtifact             = "pi-native-retained-evidence.txt"
	nativePiRetainedResultID             = "00000000-0000-4000-8000-000000000108"
	nativePiRetainedResultSummary        = "created retained native pi repository task evidence"
	nativePiRetainedLocalHandleID        = "native-pi-retained-local-handle"
	nativePiRetainedWorkspaceFingerprint = "native-pi-retained-fixture-workspace"
)

// TestNativeRepositoryTaskRetainedResume characterizes one settled native Pi
// process stop followed by a new-process resume in the same local fixture. It
// is not evidence for graceful Pi protocol shutdown, crash recovery, durable
// Control or daemon admission, fsync, exactly-once external effects, or any
// promotion of native capabilities.
func TestNativeRepositoryTaskRetainedResume(t *testing.T) {
	configuration := nativePiRepositoryTaskLoadConfiguration(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "sessions")
	firstProcessRecord := filepath.Join(root, "first-process-identity.json")
	secondProcessRecord := filepath.Join(root, "second-process-identity.json")
	resumeRecord := filepath.Join(root, "resume-handle.json")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create retained native Pi session directory: %v", err)
	}

	gitEnvironment := nativePiRepositoryTaskGitEnvironment(t, root)
	gitContext, gitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer gitCancel()
	nativePiRepositoryTaskInitializeGit(t, gitContext, gitEnvironment, workspace)
	baselineHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, gitContext, gitEnvironment, workspace, "rev-parse", "HEAD"))
	firstSubject := nativePiRepositoryTaskMaterializeSubject(t, gitContext, root, workspace, baselineHead)
	gitCancel()

	var environment []string
	if configuration.loopback {
		environment = nativePiRepositoryTaskLoopbackEnvironment(t, root, configuration.baseURL)
	} else {
		environment = nativePiRepositoryTaskEnvironment(t, root, configuration.credentialEnv)
	}
	nativePiRetainedCheckVersion(t, configuration.executable, workspace, environment)
	arguments := nativePiRetainedArguments(configuration, sessionDir)
	if configuration.loopback {
		t.Logf("native Pi retained-resume loopback request: provider=%s model=%s thinking=%s; served model/effort unknown", nativePiLoopbackProvider, nativePiLoopbackModel, nativePiLoopbackThinking)
	}

	firstExpected := nativePiRepositoryTaskExpectedResult(t, firstSubject)
	firstExpectedJSON, err := json.Marshal(firstExpected)
	if err != nil {
		t.Fatalf("marshal first retained native Pi task result: %v", err)
	}
	token := nativePiRetainedToken(t)
	var firstEvents nativePiRetainedEvents
	firstTurnContext, firstTurnCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer firstTurnCancel()
	firstSession, err := NewAdapter(configuration.executable).Start(firstTurnContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Args: arguments,
			Env:  environment,
		},
		PersistProcess: nativePiRetainedPersistProcess(firstProcessRecord),
	}, harness.EventSinkFunc(firstEvents.handle))
	if err != nil {
		if firstSession != nil {
			_, _ = nativePiRetainedStop(firstSession)
		}
		t.Fatal("start first retained native Pi repository task transport failed")
	}
	firstStopped := false
	t.Cleanup(func() {
		if firstStopped {
			return
		}
		if _, err := nativePiRetainedStop(firstSession); err != nil {
			t.Error("cleanup first retained native Pi process failed")
		}
	})
	firstStaged, ok := firstSession.(harness.StagedSession)
	if !ok {
		t.Fatal("first native Pi adapter did not return a staged session")
	}
	nativePiRetainedAssertPersistedProcess(t, firstProcessRecord, firstStaged)

	firstFramesBeforeOpen := firstEvents.nativeFrameCount()
	firstHandle, err := firstStaged.Open(firstTurnContext)
	if err != nil {
		t.Fatal("open first retained native Pi repository task session failed")
	}
	nativePiRetainedAssertHandleInSessionDirectory(t, firstHandle, sessionDir)
	if firstEvents.nativeFrameCount() <= firstFramesBeforeOpen {
		t.Fatal("first native Pi Open did not produce a decoded native frame")
	}
	resume := harness.ResumeHandle{
		LocalHandleID:         nativePiRetainedLocalHandleID,
		NativeSessionID:       firstHandle.ID,
		NativeSessionFilename: firstHandle.Filename,
		WorkspaceFingerprint:  nativePiRetainedWorkspaceFingerprint,
		NativeVersion:         TestedVersion,
	}
	if err := nativePiRepositoryTaskWriteJSON(resumeRecord, resume); err != nil {
		t.Fatalf("persist retained native Pi resume handle: %v", err)
	}
	var persistedResume harness.ResumeHandle
	nativePiRepositoryTaskReadJSON(t, resumeRecord, &persistedResume)
	if persistedResume != resume || persistedResume.LocalHandleID == "" || persistedResume.WorkspaceFingerprint == "" {
		t.Fatal("native Pi resume handle was not persisted and read back before the first prompt")
	}

	firstFramesBeforeTurn := firstEvents.nativeFrameCount()
	if err := firstStaged.StartTurn(firstTurnContext, harness.TurnRequest{
		Goal:    nativePiRetainedFirstGoal(string(firstExpectedJSON), token),
		Context: json.RawMessage(`{"task":"native_pi_retained_resume_first"}`),
	}); err != nil {
		t.Fatal("start first retained native Pi repository task turn failed")
	}
	if err := firstStaged.WaitTurn(firstTurnContext); err != nil {
		t.Fatal("first retained native Pi repository task did not settle with a strict task result")
	}
	if firstEvents.nativeFrameCount() <= firstFramesBeforeTurn {
		t.Fatal("first retained native Pi turn did not produce decoded native frames after prompt acknowledgement")
	}
	firstResult, err := nativePiRetainedStop(firstSession)
	if err != nil {
		t.Fatal("stop first retained native Pi repository task session failed")
	}
	firstStopped = true
	nativePiRetainedAssertResult(t, firstResult, firstExpected)
	firstEvents.assertTurn(t, firstExpected, "")
	nativePiRetainedAssertRetainedSessionFile(t, firstHandle, sessionDir)

	firstArtifact, err := os.ReadFile(filepath.Join(workspace, nativeRepositoryTaskArtifact))
	if err != nil {
		t.Fatalf("read first retained native Pi artifact: %v", err)
	}
	if string(firstArtifact) != nativeRepositoryTaskArtifactContents {
		t.Fatal("first retained native Pi artifact did not contain the fixed requested bytes")
	}
	firstVerificationContext, firstVerificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer firstVerificationCancel()
	if currentHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, firstVerificationContext, gitEnvironment, workspace, "rev-parse", "HEAD")); currentHead != baselineHead {
		t.Fatal("first retained native Pi task changed the repository HEAD")
	}
	if status := nativePiRepositoryTaskGitOutput(t, firstVerificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching"); status != "?? "+nativeRepositoryTaskArtifact+"\n" {
		t.Fatalf("first retained native Pi task changed unexpected repository paths: %q", status)
	}
	firstVerificationCancel()

	commitContext, commitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer commitCancel()
	nativePiRepositoryTaskGit(t, commitContext, gitEnvironment, workspace, "add", nativeRepositoryTaskArtifact)
	nativePiRepositoryTaskGit(t, commitContext, gitEnvironment, workspace, "commit", "-m", "Record first retained native Pi evidence")
	commitOne := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, commitContext, gitEnvironment, workspace, "rev-parse", "HEAD"))
	if commitOne == baselineHead {
		commitCancel()
		t.Fatal("fixture commit for first native Pi artifact did not advance HEAD")
	}
	secondSubject := nativePiRepositoryTaskMaterializeSubject(t, commitContext, root, workspace, commitOne)
	commitCancel()
	secondExpected := nativePiRetainedExpectedResult(t, secondSubject)
	secondExpectedJSON, err := json.Marshal(secondExpected)
	if err != nil {
		t.Fatalf("marshal second retained native Pi task result: %v", err)
	}

	var secondEvents nativePiRetainedEvents
	secondTurnContext, secondTurnCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer secondTurnCancel()
	secondSession, err := NewAdapter(configuration.executable).Start(secondTurnContext, harness.StartRequest{
		Workspace: workspace,
		Resume:    &persistedResume,
		Invocation: execution.Invocation{
			Args: arguments,
			Env:  environment,
		},
		PersistProcess: nativePiRetainedPersistProcess(secondProcessRecord),
	}, harness.EventSinkFunc(secondEvents.handle))
	if err != nil {
		if secondSession != nil {
			_, _ = nativePiRetainedStop(secondSession)
		}
		t.Fatal("start second retained native Pi repository task transport failed")
	}
	secondStopped := false
	t.Cleanup(func() {
		if secondStopped {
			return
		}
		if _, err := nativePiRetainedStop(secondSession); err != nil {
			t.Error("cleanup second retained native Pi process failed")
		}
	})
	secondStaged, ok := secondSession.(harness.StagedSession)
	if !ok {
		t.Fatal("second native Pi adapter did not return a staged session")
	}
	nativePiRetainedAssertPersistedProcess(t, secondProcessRecord, secondStaged)

	secondFramesBeforeOpen := secondEvents.nativeFrameCount()
	secondHandle, err := secondStaged.Open(secondTurnContext)
	if err != nil {
		t.Fatal("open second retained native Pi repository task session failed")
	}
	if secondHandle != firstHandle {
		t.Fatal("second native Pi Open did not return the exact retained session identity")
	}
	if secondEvents.nativeFrameCount() <= secondFramesBeforeOpen {
		t.Fatal("second native Pi Open did not produce a decoded native frame")
	}
	replayedSecondHandle, err := secondStaged.Open(secondTurnContext)
	if err != nil {
		t.Fatal("reopen second retained native Pi repository task session failed")
	}
	if replayedSecondHandle != firstHandle {
		t.Fatal("second native Pi Open replay changed the retained session identity")
	}

	secondFramesBeforeTurn := secondEvents.nativeFrameCount()
	if err := secondStaged.StartTurn(secondTurnContext, harness.TurnRequest{
		Goal:    nativePiRetainedSecondGoal(string(secondExpectedJSON)),
		Context: json.RawMessage(`{"task":"native_pi_retained_resume_second"}`),
	}); err != nil {
		t.Fatal("start second retained native Pi repository task turn failed")
	}
	if err := secondStaged.WaitTurn(secondTurnContext); err != nil {
		t.Fatal("second retained native Pi repository task did not settle with a strict task result")
	}
	if secondEvents.nativeFrameCount() <= secondFramesBeforeTurn {
		t.Fatal("second retained native Pi turn did not produce decoded native frames after prompt acknowledgement")
	}
	secondResult, err := nativePiRetainedStop(secondSession)
	if err != nil {
		t.Fatal("stop second retained native Pi repository task session failed")
	}
	secondStopped = true
	nativePiRetainedAssertResult(t, secondResult, secondExpected)
	secondEvents.assertTurn(t, secondExpected, nativeRepositoryTaskResultID)

	secondArtifact, err := os.ReadFile(filepath.Join(workspace, nativePiRetainedArtifact))
	if err != nil {
		t.Fatalf("read second retained native Pi artifact: %v", err)
	}
	if string(secondArtifact) != token+"\n" {
		t.Fatal("second retained native Pi artifact did not contain the remembered token")
	}
	finalFirstArtifact, err := os.ReadFile(filepath.Join(workspace, nativeRepositoryTaskArtifact))
	if err != nil {
		t.Fatalf("read committed first retained native Pi artifact: %v", err)
	}
	if string(finalFirstArtifact) != nativeRepositoryTaskArtifactContents {
		t.Fatal("second retained native Pi task changed the first artifact")
	}
	secondVerificationContext, secondVerificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer secondVerificationCancel()
	if currentHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, secondVerificationContext, gitEnvironment, workspace, "rev-parse", "HEAD")); currentHead != commitOne {
		t.Fatal("second retained native Pi task changed fixture commit C1")
	}
	if status := nativePiRepositoryTaskGitOutput(t, secondVerificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=matching"); status != "?? "+nativePiRetainedArtifact+"\n" {
		t.Fatalf("second retained native Pi task changed unexpected repository paths: %q", status)
	}
}

func nativePiRetainedCheckVersion(t *testing.T, executable, workspace string, environment []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--version")
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		t.Fatal("run retained native Pi version check failed")
	}
	if strings.TrimSpace(string(output)) != TestedVersion {
		t.Fatalf("native Pi version did not equal the tested version %s", TestedVersion)
	}
}

func nativePiRetainedArguments(configuration nativePiRepositoryTaskConfiguration, sessionDir string) []string {
	arguments := []string{
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
		"--no-approve",
		"--session-dir", sessionDir,
		"--provider", configuration.provider,
		"--model", configuration.model,
		"--tools", "write",
	}
	if configuration.loopback {
		arguments = append(arguments, "--thinking", nativePiLoopbackThinking)
	}
	return arguments
}

func nativePiRetainedPersistProcess(filename string) func(int, string) error {
	return func(pid int, identity string) error {
		if pid <= 0 || strings.TrimSpace(identity) == "" {
			return os.ErrInvalid
		}
		return nativePiRepositoryTaskWriteJSON(filename, map[string]any{
			"pid":      pid,
			"identity": identity,
		})
	}
}

func nativePiRetainedAssertPersistedProcess(t *testing.T, filename string, session harness.StagedSession) {
	t.Helper()
	pid, identity := session.ProcessDetails()
	var persisted struct {
		PID      int    `json:"pid"`
		Identity string `json:"identity"`
	}
	nativePiRepositoryTaskReadJSON(t, filename, &persisted)
	if pid <= 0 || strings.TrimSpace(identity) == "" || persisted.PID != pid || persisted.Identity != identity {
		t.Fatal("native Pi process identity was not persisted before session exposure")
	}
}

func nativePiRetainedAssertHandleInSessionDirectory(t *testing.T, handle harness.NativeSessionHandle, sessionDir string) {
	t.Helper()
	if strings.TrimSpace(handle.ID) == "" || strings.TrimSpace(handle.Filename) == "" {
		t.Fatal("native Pi returned an incomplete retained session handle")
	}
	relativeFilename, err := filepath.Rel(sessionDir, handle.Filename)
	if err != nil || relativeFilename == ".." || strings.HasPrefix(relativeFilename, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativeFilename) {
		t.Fatal("native Pi session filename was not isolated under the retained session directory")
	}
}

func nativePiRetainedAssertRetainedSessionFile(t *testing.T, handle harness.NativeSessionHandle, sessionDir string) {
	t.Helper()
	nativePiRetainedAssertHandleInSessionDirectory(t, handle, sessionDir)
	info, err := os.Stat(handle.Filename)
	if err != nil {
		t.Fatalf("stat retained native Pi session file after process stop: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatal("retained native Pi session file was not a nonempty regular file after process stop")
	}
}

func nativePiRetainedStop(session harness.Session) (harness.TaskResult, error) {
	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	closeErr := session.Close(closeContext)
	closeCancel()
	waitContext, waitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	result, waitErr := session.Wait(waitContext)
	waitCancel()
	if closeErr != nil && waitErr != nil {
		return result, fmt.Errorf("close: %w; wait: %w", closeErr, waitErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close: %w", closeErr)
	}
	if waitErr != nil {
		return result, fmt.Errorf("wait: %w", waitErr)
	}
	return result, nil
}

func nativePiRetainedExpectedResult(t *testing.T, subject protocol.Subject) protocol.TaskResult {
	t.Helper()
	result := nativePiRepositoryTaskExpectedResult(t, subject)
	result.ResultID = nativePiRetainedResultID
	result.Summary = nativePiRetainedResultSummary
	if err := result.Validate(); err != nil {
		t.Fatalf("validate second retained native Pi task result: %v", err)
	}
	return result
}

func nativePiRetainedToken(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 18)
	if _, err := cryptorand.Read(bytes); err != nil {
		t.Fatalf("generate retained native Pi token: %v", err)
	}
	return "retained-" + hex.EncodeToString(bytes)
}

func nativePiRetainedFirstGoal(expectedResult, token string) string {
	return "Perform only this bounded repository task. Create " + nativeRepositoryTaskArtifact +
		" at the workspace root containing exactly this UTF-8 text, including its final newline:\n" +
		nativeRepositoryTaskArtifactContents +
		"Remember this opaque token for the next native turn, but do not write it to a file or include it in the final result:\n" +
		token + "\n" +
		"Do not create, modify, or delete any other workspace file. Use the available write tool only. " +
		"After the file exists, return exactly the following JSON object as the entire final assistant text, with no Markdown, prose, or code fence:\n" +
		expectedResult
}

func nativePiRetainedSecondGoal(expectedResult string) string {
	return "Perform only this bounded repository task. Create " + nativePiRetainedArtifact +
		" at the workspace root containing exactly the opaque token remembered from the immediately prior turn, followed by one newline. " +
		"Do not create, modify, or delete any other workspace file. Use the available write tool only. " +
		"After the file exists, return exactly the following JSON object as the entire final assistant text, with no Markdown, prose, or code fence:\n" +
		expectedResult
}

func nativePiRetainedAssertResult(t *testing.T, result harness.TaskResult, expected protocol.TaskResult) {
	t.Helper()
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || !reflect.DeepEqual(*result.Semantic, expected) {
		t.Fatalf("retained native Pi semantic mismatch: kind=%s reason=%v semantic_present=%t", result.Kind, result.Reason, result.Semantic != nil)
	}
	if !result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil || !result.Process.OutputTruncated || result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatal("retained native Pi process did not complete a bounded termination barrier")
	}
}

type nativePiRetainedEvents struct {
	mutex          sync.Mutex
	frames         int
	sessionStarted int
	taskResults    int
	firstResult    json.RawMessage
}

func (events *nativePiRetainedEvents) handle(_ context.Context, event harness.Event) error {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	switch event.Kind {
	case harness.EventNativeFrame:
		events.frames++
	case harness.EventSessionStarted:
		events.sessionStarted++
	case harness.EventTaskResult:
		events.taskResults++
		if events.taskResults == 1 {
			events.firstResult = append(json.RawMessage(nil), event.Payload...)
		}
	}
	return nil
}

func (events *nativePiRetainedEvents) nativeFrameCount() int {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	return events.frames
}

func (events *nativePiRetainedEvents) assertTurn(t *testing.T, expected protocol.TaskResult, excludedResultID string) {
	t.Helper()
	events.mutex.Lock()
	sessionStarted, taskResults, payload := events.sessionStarted, events.taskResults, events.firstResult
	events.mutex.Unlock()

	if sessionStarted != 1 || taskResults != 1 {
		t.Fatalf("retained native Pi events = session_started:%d task_results:%d, want one acknowledged turn and one semantic payload", sessionStarted, taskResults)
	}
	taskResult, err := DecodeTaskResultJSON(payload)
	if err != nil {
		t.Fatalf("decode retained native Pi task_result payload: %v", err)
	}
	if excludedResultID != "" && taskResult.ResultID == excludedResultID {
		t.Fatal("retained native Pi sink replayed the first task_result payload")
	}
	if !reflect.DeepEqual(taskResult, expected) {
		t.Fatalf("retained native Pi task_result payload = %#v, want exact strict result %#v", taskResult, expected)
	}
}
