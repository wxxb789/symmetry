package pi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	nativeSmokeEnabledEnv    = "SYMMETRY_PI_NATIVE_SMOKE"
	nativeSmokeExecutableEnv = "SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE"
	nativeSmokeTimeout       = 20 * time.Second
)

// TestNativeRPCOpenAndClose is deliberately opt-in because it starts the
// locally installed Pi binary. It proves only process/stdio/get_state/close
// transport behavior: it never starts a turn, sends a prompt, or invokes a model.
func TestNativeRPCOpenAndClose(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("native Pi RPC smoke supports Linux and Windows only")
	}
	if os.Getenv(nativeSmokeEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_PI_NATIVE_SMOKE=1 to run the native Pi RPC smoke")
	}

	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" {
		t.Fatalf("%s must name the absolute Pi 0.85.1 executable", nativeSmokeExecutableEnv)
	}
	if !filepath.IsAbs(executable) {
		t.Fatalf("%s must be absolute", nativeSmokeExecutableEnv)
	}
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatalf("stat native Pi executable: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s must name a regular executable file", nativeSmokeExecutableEnv)
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "sessions")
	environment := nativePiSmokeEnvironment(t, root)
	for _, directory := range []string{workspace, sessionDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create smoke directory: %v", err)
		}
	}

	versionContext, versionCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer versionCancel()
	versionCommand := exec.CommandContext(versionContext, executable, "--version")
	versionCommand.Dir = workspace
	versionCommand.Env = environment
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatal("run native Pi version check failed")
	}
	if strings.TrimSpace(string(versionOutput)) != TestedVersion {
		t.Fatalf("native Pi version did not equal the tested version")
	}
	t.Logf("native Pi RPC smoke: platform=%s version=%s", runtime.GOOS, TestedVersion)
	startContext, startCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer startCancel()

	var eventMutex sync.Mutex
	eventCounts := make(map[harness.EventKind]int)
	sink := harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		eventMutex.Lock()
		eventCounts[event.Kind]++
		eventMutex.Unlock()
		return nil
	})
	var persistMutex sync.Mutex
	persistCount := 0
	persistedPID := 0
	persistedIdentity := ""

	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Args: []string{
				"--offline",
				"--no-extensions",
				"--no-skills",
				"--no-prompt-templates",
				"--no-themes",
				"--no-context-files",
				"--no-approve",
				"--no-tools",
				"--session-dir", sessionDir,
				"--provider", "openai",
				"--model", "gpt-4o",
			},
			Env: environment,
		},
		PersistProcess: func(pid int, identity string) error {
			persistMutex.Lock()
			defer persistMutex.Unlock()
			persistCount++
			persistedPID = pid
			persistedIdentity = identity
			return nil
		},
	}, sink)
	if err != nil {
		t.Fatal("start native Pi RPC transport failed")
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Error("cleanup native Pi session failed")
		}
		waitContext, waitCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
		defer waitCancel()
		if _, err := session.Wait(waitContext); err != nil {
			t.Error("wait for native Pi cleanup failed")
		}
	})
	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native Pi adapter did not return a staged session")
	}

	persistMutex.Lock()
	gotPersistCount, gotPersistedPID, gotPersistedIdentity := persistCount, persistedPID, persistedIdentity
	persistMutex.Unlock()
	pid, identity := staged.ProcessDetails()
	if gotPersistCount != 1 || gotPersistedPID != pid || gotPersistedIdentity != identity || pid <= 0 || identity == "" {
		t.Fatal("native Pi process identity was not persisted exactly once before session exposure")
	}

	eventMutex.Lock()
	framesBeforeOpen := eventCounts[harness.EventNativeFrame]
	eventMutex.Unlock()
	handle, err := staged.Open(startContext)
	if err != nil {
		t.Fatal("open native Pi RPC session failed")
	}
	eventMutex.Lock()
	framesAfterOpen := eventCounts[harness.EventNativeFrame]
	eventMutex.Unlock()
	if framesAfterOpen <= framesBeforeOpen {
		t.Fatal("native Pi get_state did not produce a decoded native frame")
	}
	if handle.ID == "" || handle.Filename == "" {
		t.Fatal("native Pi get_state returned an incomplete session handle")
	}
	replayedHandle, err := staged.Open(startContext)
	if err != nil {
		t.Fatal("reopen native Pi RPC session failed")
	}
	if replayedHandle != handle {
		t.Fatal("native Pi session handle changed across Open replay")
	}
	relativeFilename, err := filepath.Rel(sessionDir, handle.Filename)
	if err != nil || relativeFilename == ".." || strings.HasPrefix(relativeFilename, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativeFilename) {
		t.Fatal("native Pi session filename was not isolated under the smoke session directory")
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("close native Pi RPC session failed")
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatal("wait for native Pi RPC session failed")
	}
	closed = true
	if result.Kind == harness.ResultSucceeded || result.Semantic != nil {
		t.Fatal("a transport-only native Pi smoke must not report semantic task success")
	}
	if !result.Process.Terminated {
		t.Fatal("native Pi process was not stopped by Close")
	}

	eventMutex.Lock()
	workEvents := eventCounts[harness.EventSessionStarted] + eventCounts[harness.EventTaskResult] +
		eventCounts[harness.EventOutput] + eventCounts[harness.EventMessageDelta] +
		eventCounts[harness.EventToolStarted] + eventCounts[harness.EventToolFinished] +
		eventCounts[harness.EventUsageObserved] + eventCounts[harness.EventApprovalRequested]
	eventMutex.Unlock()
	if workEvents != 0 {
		t.Fatal("native Pi transport smoke observed work events without starting a turn")
	}
}

func nativePiSmokeEnvironment(t *testing.T, root string) []string {
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
			t.Fatal("create isolated native Pi environment failed")
		}
	}

	environment := []string{
		"PI_CODING_AGENT_DIR=" + agentDir,
		"PI_OFFLINE=1",
		"HOME=" + home,
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
	}
	if runtime.GOOS == "windows" {
		environment = append(environment,
			"USERPROFILE="+home,
			"APPDATA="+appDataDir,
			"LOCALAPPDATA="+localAppDataDir,
		)
		for _, name := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
			value, ok := os.LookupEnv(name)
			if !ok || strings.TrimSpace(value) == "" {
				t.Fatalf("native Pi smoke requires %s in the parent environment", name)
			}
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
