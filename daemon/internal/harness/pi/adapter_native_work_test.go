package pi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

const (
	nativeRepositoryTaskEnabledEnv        = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK"
	nativeRepositoryTaskExecutableEnv     = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE"
	nativeRepositoryTaskProviderEnv       = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER"
	nativeRepositoryTaskModelEnv          = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL"
	nativeRepositoryTaskCredentialEnv     = "SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV"
	nativeRepositoryTaskTimeout           = 90 * time.Second
	nativeRepositoryTaskCloseTimeout      = 25 * time.Second
	nativeRepositoryTaskGitTimeout        = 10 * time.Second
	nativeRepositoryTaskArtifact          = "pi-native-evidence.txt"
	nativeRepositoryTaskArtifactContents  = "native pi repository task evidence\n"
	nativeRepositoryTaskResultSummary     = "created native pi repository task evidence"
	nativeRepositoryTaskResultID          = "00000000-0000-4000-8000-000000000106"
	nativeRepositoryTaskSubjectResourceID = "00000000-0000-4000-8000-000000000107"
)

// TestNativeRepositoryTask is deliberately opt-in because it invokes a real
// authenticated model. It proves one bounded repository mutation and the
// native turn/result lifecycle; it does not advertise Pi capabilities or prove
// cancellation, recovery, Control durability, or provider accounting.
func TestNativeRepositoryTask(t *testing.T) {
	configuration := nativePiRepositoryTaskLoadConfiguration(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "sessions")
	processRecord := filepath.Join(root, "process-identity.json")
	sessionRecord := filepath.Join(root, "session-identity.json")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create native Pi session directory: %v", err)
	}
	gitEnvironment := nativePiRepositoryTaskGitEnvironment(t, root)
	gitContext, gitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	nativePiRepositoryTaskInitializeGit(t, gitContext, gitEnvironment, workspace)
	baselineHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, gitContext, gitEnvironment, workspace, "rev-parse", "HEAD"))
	subject := nativePiRepositoryTaskMaterializeSubject(t, gitContext, root, workspace, baselineHead)
	gitCancel()
	environment := nativePiRepositoryTaskEnvironment(t, root, configuration.credentialEnv)

	versionContext, versionCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer versionCancel()
	versionCommand := exec.CommandContext(versionContext, configuration.executable, "--version")
	versionCommand.Dir = workspace
	versionCommand.Env = environment
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatal("run native Pi version check failed")
	}
	if strings.TrimSpace(string(versionOutput)) != TestedVersion {
		t.Fatalf("native Pi version did not equal the tested version %s", TestedVersion)
	}

	var events nativePiRepositoryTaskEvents
	turnContext, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskTimeout)
	defer cancel()
	session, err := NewAdapter(configuration.executable).Start(turnContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Args: []string{
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
			},
			Env: environment,
		},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			return nativePiRepositoryTaskWriteJSON(processRecord, map[string]any{
				"pid":      pid,
				"identity": identity,
			})
		},
	}, harness.EventSinkFunc(events.handle))
	if err != nil {
		t.Fatal("start native Pi repository task transport failed")
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Error("cleanup native Pi repository task session failed")
		}
		waitContext, waitCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
		defer waitCancel()
		if _, err := session.Wait(waitContext); err != nil {
			t.Error("wait for native Pi repository task cleanup failed")
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native Pi adapter did not return a staged session")
	}
	processPID, processIdentity := staged.ProcessDetails()
	var persistedProcess struct {
		PID      int    `json:"pid"`
		Identity string `json:"identity"`
	}
	nativePiRepositoryTaskReadJSON(t, processRecord, &persistedProcess)
	if persistedProcess.PID != processPID || persistedProcess.Identity != processIdentity || processPID <= 0 || processIdentity == "" {
		t.Fatal("native Pi process identity was not persisted before session exposure")
	}

	nativeFramesBeforeOpen := events.nativeFrameCount()
	handle, err := staged.Open(turnContext)
	if err != nil {
		t.Fatal("open native Pi repository task session failed")
	}
	if handle.ID == "" || handle.Filename == "" {
		t.Fatal("native Pi repository task returned an incomplete session handle")
	}
	if err := nativePiRepositoryTaskWriteJSON(sessionRecord, map[string]any{
		"id":       handle.ID,
		"filename": handle.Filename,
	}); err != nil {
		t.Fatalf("persist native Pi session identity: %v", err)
	}
	var persistedSession struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	nativePiRepositoryTaskReadJSON(t, sessionRecord, &persistedSession)
	if persistedSession.ID != handle.ID || persistedSession.Filename != handle.Filename {
		t.Fatal("native Pi session identity was not persisted before StartTurn")
	}
	relativeFilename, err := filepath.Rel(sessionDir, handle.Filename)
	if err != nil || relativeFilename == ".." || strings.HasPrefix(relativeFilename, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativeFilename) {
		t.Fatal("native Pi session filename was not isolated under the repository task session directory")
	}
	nativeFramesAfterOpen := events.nativeFrameCount()
	if nativeFramesAfterOpen <= nativeFramesBeforeOpen {
		t.Fatal("native Pi Open did not produce a decoded native frame")
	}

	expected := nativePiRepositoryTaskExpectedResult(t, subject)
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected native Pi task result: %v", err)
	}
	nativeFramesBeforeTurn := events.nativeFrameCount()
	// StartTurn returns only after the adapter receives the correlated prompt
	// response; WaitTurn below additionally requires native settlement.
	if err := staged.StartTurn(turnContext, harness.TurnRequest{
		Goal:    nativePiRepositoryTaskGoal(string(expectedJSON)),
		Context: json.RawMessage(`{"task":"native_pi_repository_evidence"}`),
	}); err != nil {
		t.Fatal("start native Pi repository task turn failed")
	}
	if err := staged.WaitTurn(turnContext); err != nil {
		t.Fatal("native Pi repository task did not settle with a strict task result")
	}
	if nativeFramesAfterTurn := events.nativeFrameCount(); nativeFramesAfterTurn <= nativeFramesBeforeTurn {
		t.Fatal("native Pi repository task turn did not produce decoded native frames after prompt acknowledgement")
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskCloseTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("close native Pi repository task session failed")
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatal("wait for native Pi repository task session failed")
	}
	closed = true
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || !reflect.DeepEqual(*result.Semantic, expected) {
		t.Fatal("native Pi repository task did not return the expected semantic progress result")
	}
	if !result.Process.Terminated || result.Process.SinkError != nil || result.Process.OutputError != nil || result.Process.TerminationError != nil || result.Process.ContainmentError != nil {
		t.Fatal("native Pi repository task process did not complete a bounded clean close")
	}

	artifact, err := os.ReadFile(filepath.Join(workspace, nativeRepositoryTaskArtifact))
	if err != nil {
		t.Fatalf("read native Pi repository task artifact: %v", err)
	}
	if string(artifact) != nativeRepositoryTaskArtifactContents {
		t.Fatal("native Pi repository task artifact did not contain the exact requested bytes")
	}
	verificationContext, verificationCancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer verificationCancel()
	if currentHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "rev-parse", "HEAD")); currentHead != baselineHead {
		t.Fatal("native Pi repository task changed the repository HEAD")
	}
	status := nativePiRepositoryTaskGitOutput(t, verificationContext, gitEnvironment, workspace, "status", "--porcelain=v1", "--untracked-files=all")
	if status != "?? "+nativeRepositoryTaskArtifact+"\n" {
		t.Fatalf("native Pi repository task changed unexpected repository paths: %q", status)
	}
	if sessionStarted, taskResults := events.semanticCounts(); sessionStarted != 1 || taskResults != 1 {
		t.Fatalf("native Pi repository task events = session_started:%d task_results:%d, want one acknowledged turn and semantic result", sessionStarted, taskResults)
	}
}

type nativePiRepositoryTaskConfiguration struct {
	executable    string
	provider      string
	model         string
	credentialEnv string
}

func nativePiRepositoryTaskLoadConfiguration(t *testing.T) nativePiRepositoryTaskConfiguration {
	t.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native Pi repository task test supports Linux and Windows only")
	}
	if os.Getenv(nativeRepositoryTaskEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 to run the native Pi repository task test")
	}
	configuration := nativePiRepositoryTaskConfiguration{
		executable: nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskExecutableEnv),
		provider:   nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskProviderEnv),
		model:      nativePiRepositoryTaskRequiredEnv(t, nativeRepositoryTaskModelEnv),
	}
	if !filepath.IsAbs(configuration.executable) {
		t.Fatalf("%s must be absolute", nativeRepositoryTaskExecutableEnv)
	}
	info, err := os.Stat(configuration.executable)
	if err != nil {
		t.Fatalf("stat native Pi executable: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s must name a regular executable file", nativeRepositoryTaskExecutableEnv)
	}
	configuration.credentialEnv = strings.TrimSpace(os.Getenv(nativeRepositoryTaskCredentialEnv))
	if configuration.credentialEnv != "" {
		if !nativePiRepositoryTaskCredentialEnvironmentAllowed(configuration.credentialEnv) {
			t.Fatalf("%s must name one non-reserved ASCII environment variable", nativeRepositoryTaskCredentialEnv)
		}
		if value, ok := os.LookupEnv(configuration.credentialEnv); !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("selected credential environment variable %s is not configured", configuration.credentialEnv)
		}
	}
	return configuration
}

func TestNativeRepositoryTaskCredentialEnvironmentRejectsInvalidOrReservedNames(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "OPENAI_API_KEY", want: true},
		{name: "ANTHROPIC_API_KEY", want: true},
		{name: "", want: false},
		{name: "1API_KEY", want: false},
		{name: "API-KEY", want: false},
		{name: "PATH", want: false},
		{name: "path", want: false},
		{name: "SystemRoot", want: false},
		{name: "windir", want: false},
		{name: "ComSpec", want: false},
		{name: "PATHEXT", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativePiRepositoryTaskCredentialEnvironmentAllowed(test.name); got != test.want {
				t.Fatalf("nativePiRepositoryTaskCredentialEnvironmentAllowed(%q) = %t, want %t", test.name, got, test.want)
			}
		})
	}
}

func TestNativeRepositoryTaskSubjectUsesRepositoryBaseline(t *testing.T) {
	root := t.TempDir()
	gitEnvironment := nativePiRepositoryTaskGitEnvironment(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), nativeRepositoryTaskGitTimeout)
	defer cancel()
	repository := filepath.Join(root, "repository")
	nativePiRepositoryTaskInitializeGit(t, ctx, gitEnvironment, repository)
	baselineHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, ctx, gitEnvironment, repository, "rev-parse", "HEAD"))
	baseline := nativePiRepositoryTaskMaterializeSubject(t, ctx, root, repository, baselineHead)
	if baseline.ResourceID != nativeRepositoryTaskSubjectResourceID || baseline.Commit != baselineHead || baseline.TreeDigest == "" {
		t.Fatalf("baseline subject = %#v, want actual baseline commit and tree digest", baseline)
	}
	if result := nativePiRepositoryTaskExpectedResult(t, baseline); result.Subject != baseline {
		t.Fatalf("task result subject = %#v, want materialized baseline %#v", result.Subject, baseline)
	}
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("# Changed Native Pi repository task\n"), 0o600); err != nil {
		t.Fatalf("rewrite subject fixture: %v", err)
	}
	nativePiRepositoryTaskGit(t, ctx, gitEnvironment, repository, "add", "README.md")
	nativePiRepositoryTaskGit(t, ctx, gitEnvironment, repository, "commit", "-m", "Change native Pi repository task fixture")
	changedHead := strings.TrimSpace(nativePiRepositoryTaskGitOutput(t, ctx, gitEnvironment, repository, "rev-parse", "HEAD"))
	changed := nativePiRepositoryTaskMaterializeSubject(t, ctx, root, repository, changedHead)
	if changedHead == baselineHead || changed.Commit != changedHead || changed.TreeDigest == baseline.TreeDigest {
		t.Fatalf("changed subject = %#v, baseline = %#v; want distinct commit and tree digest", changed, baseline)
	}
}

func nativePiRepositoryTaskRequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		t.Fatalf("%s must be configured", name)
	}
	return value
}

func nativePiRepositoryTaskEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index := range name {
		character := name[index]
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}

func nativePiRepositoryTaskCredentialEnvironmentAllowed(name string) bool {
	if !nativePiRepositoryTaskEnvironmentName(name) {
		return false
	}
	_, reserved := nativePiRepositoryTaskReservedEnvironment[strings.ToUpper(name)]
	return !reserved
}

var nativePiRepositoryTaskReservedEnvironment = map[string]struct{}{
	"APPDATA":             {},
	"COMSPEC":             {},
	"HOME":                {},
	"LOCALAPPDATA":        {},
	"PATH":                {},
	"PI_CODING_AGENT_DIR": {},
	"PATHEXT":             {},
	"SYSTEMROOT":          {},
	"TEMP":                {},
	"TMP":                 {},
	"USERPROFILE":         {},
	"WINDIR":              {},
	"XDG_CACHE_HOME":      {},
	"XDG_CONFIG_HOME":     {},
	"XDG_DATA_HOME":       {},
}

func nativePiRepositoryTaskEnvironment(t *testing.T, root, credentialEnv string) []string {
	t.Helper()
	home := filepath.Join(root, "home")
	agentDir := filepath.Join(root, "agent")
	cacheDir := filepath.Join(root, "cache")
	dataDir := filepath.Join(root, "data")
	tempDir := filepath.Join(root, "tmp")
	appDataDir := filepath.Join(root, "appdata")
	localAppDataDir := filepath.Join(root, "localappdata")
	for _, directory := range []string{home, agentDir, cacheDir, dataDir, tempDir, appDataDir, localAppDataDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("create isolated native Pi repository task environment failed")
		}
	}
	path := nativePiRepositoryTaskRequiredEnv(t, "PATH")
	environment := []string{
		"PI_CODING_AGENT_DIR=" + agentDir,
		"HOME=" + home,
		"PATH=" + path,
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
	}
	if credentialEnv != "" {
		environment = append(environment, credentialEnv+"="+os.Getenv(credentialEnv))
	}
	if runtime.GOOS == "windows" {
		environment = append(environment,
			"USERPROFILE="+home,
			"APPDATA="+appDataDir,
			"LOCALAPPDATA="+localAppDataDir,
		)
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			value := nativePiRepositoryTaskRequiredEnv(t, name)
			environment = append(environment, name+"="+value)
		}
		return environment
	}
	return append(environment,
		"XDG_CONFIG_HOME="+agentDir,
		"XDG_CACHE_HOME="+cacheDir,
		"XDG_DATA_HOME="+dataDir,
	)
}

func nativePiRepositoryTaskInitializeGit(t *testing.T, ctx context.Context, environment []string, workspace string) {
	t.Helper()
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create native Pi repository task workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Native Pi repository task\n"), 0o600); err != nil {
		t.Fatalf("write native Pi repository task seed file: %v", err)
	}
	nativePiRepositoryTaskGit(t, ctx, environment, workspace, "init")
	if err := os.MkdirAll(filepath.Join(workspace, ".git", "hooks-disabled"), 0o700); err != nil {
		t.Fatalf("create disabled Git hooks directory: %v", err)
	}
	for _, arguments := range [][]string{
		{"config", "core.hooksPath", filepath.Join(workspace, ".git", "hooks-disabled")},
		{"config", "commit.gpgSign", "false"},
		{"config", "tag.gpgSign", "false"},
		{"config", "user.name", "Symmetry Native Test"},
		{"config", "user.email", "symmetry-native-test@example.invalid"},
		{"add", "README.md"},
		{"commit", "-m", "Initialize native Pi repository task"},
	} {
		nativePiRepositoryTaskGit(t, ctx, environment, workspace, arguments...)
	}
}

func nativePiRepositoryTaskGitEnvironment(t *testing.T, root string) []string {
	t.Helper()
	home := filepath.Join(root, "git-home")
	tempDir := filepath.Join(root, "git-tmp")
	templateDir := filepath.Join(root, "git-templates")
	globalConfig := filepath.Join(root, "gitconfig")
	appDataDir := filepath.Join(root, "git-appdata")
	localAppDataDir := filepath.Join(root, "git-localappdata")
	for _, directory := range []string{home, tempDir, templateDir, appDataDir, localAppDataDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("create isolated Git environment failed")
		}
	}
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatalf("create isolated Git configuration: %v", err)
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   globalConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TEMPLATE_DIR":    templateDir,
		"GIT_TERMINAL_PROMPT": "0",
	} {
		t.Setenv(name, value)
	}
	for _, name := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_WORK_TREE",
	} {
		nativePiRepositoryTaskUnsetEnvironment(t, name)
	}
	environment := []string{
		"HOME=" + home,
		"PATH=" + nativePiRepositoryTaskRequiredEnv(t, "PATH"),
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
		"GIT_CONFIG_GLOBAL=" + globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TEMPLATE_DIR=" + templateDir,
		"GIT_TERMINAL_PROMPT=0",
	}
	if runtime.GOOS == "windows" {
		environment = append(environment,
			"USERPROFILE="+home,
			"APPDATA="+appDataDir,
			"LOCALAPPDATA="+localAppDataDir,
		)
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			environment = append(environment, name+"="+nativePiRepositoryTaskRequiredEnv(t, name))
		}
	}
	return environment
}

func nativePiRepositoryTaskUnsetEnvironment(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
			return
		}
		_ = os.Unsetenv(name)
	})
}

func nativePiRepositoryTaskGit(t *testing.T, ctx context.Context, environment []string, workspace string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = workspace
	command.Env = environment
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("git %s timed out: %v", arguments[0], ctx.Err())
		}
		t.Fatalf("git %s failed: %v", arguments[0], err)
	}
}

func nativePiRepositoryTaskGitOutput(t *testing.T, ctx context.Context, environment []string, workspace string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = workspace
	command.Env = environment
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("git %s timed out: %v", arguments[0], ctx.Err())
		}
		t.Fatalf("git %s failed: %v", arguments[0], err)
	}
	return string(output)
}

func nativePiRepositoryTaskMaterializeSubject(t *testing.T, ctx context.Context, root, repository, commit string) protocol.Subject {
	t.Helper()
	manager := workspace.New(map[string]config.Workspace{
		"native": {
			Policy:     config.WorkspacePolicyGitWorktree,
			Repository: repository,
			Root:       filepath.Join(root, "subject-worktrees"),
			Ref:        "HEAD",
			Cleanup:    config.CleanupAlways,
		},
	})
	subject, err := manager.MaterializeSubject(ctx, "native", nativeRepositoryTaskSubjectResourceID, commit)
	if err != nil {
		t.Fatalf("materialize native Pi repository task subject: %v", err)
	}
	return subject
}

func nativePiRepositoryTaskWriteJSON(filename string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func nativePiRepositoryTaskReadJSON(t *testing.T, filename string, value any) {
	t.Helper()
	encoded, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read native Pi repository task identity: %v", err)
	}
	if err := json.Unmarshal(encoded, value); err != nil {
		t.Fatalf("decode native Pi repository task identity: %v", err)
	}
}

func nativePiRepositoryTaskExpectedResult(t *testing.T, subject protocol.Subject) protocol.TaskResult {
	t.Helper()
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatalf("hash native Pi repository task subject: %v", err)
	}
	result := protocol.TaskResult{
		SchemaVersion: protocol.TaskResultSchemaVersion,
		ResultID:      nativeRepositoryTaskResultID,
		Kind:          protocol.TaskResultProgress,
		Summary:       nativeRepositoryTaskResultSummary,
		Subject:       subject,
		SubjectHash:   subjectHash,
		EvidenceRefs:  []string{},
		Diagnostics:   []protocol.Diagnostic{},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("validate expected native Pi task result: %v", err)
	}
	return result
}

func nativePiRepositoryTaskGoal(expectedResult string) string {
	return "Perform only this bounded repository task. Create " + nativeRepositoryTaskArtifact +
		" at the workspace root containing exactly this UTF-8 text, including its final newline:\n" +
		nativeRepositoryTaskArtifactContents +
		"Do not create, modify, or delete any other workspace file. Use the available write tool only. " +
		"After the file exists, return exactly the following JSON object as the entire final assistant text, with no Markdown, prose, or code fence:\n" +
		expectedResult
}

type nativePiRepositoryTaskEvents struct {
	mutex          sync.Mutex
	nativeFrames   int
	sessionStarted int
	taskResults    int
}

func (events *nativePiRepositoryTaskEvents) handle(_ context.Context, event harness.Event) error {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	switch event.Kind {
	case harness.EventNativeFrame:
		events.nativeFrames++
	case harness.EventSessionStarted:
		events.sessionStarted++
	case harness.EventTaskResult:
		events.taskResults++
	}
	return nil
}

func (events *nativePiRepositoryTaskEvents) nativeFrameCount() int {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	return events.nativeFrames
}

func (events *nativePiRepositoryTaskEvents) semanticCounts() (sessionStarted, taskResults int) {
	events.mutex.Lock()
	defer events.mutex.Unlock()
	return events.sessionStarted, events.taskResults
}
