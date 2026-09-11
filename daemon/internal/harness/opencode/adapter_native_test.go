//go:build linux || windows

package opencode

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
	nativeSmokeEnabledEnv    = "SYMMETRY_OPENCODE_NATIVE_SMOKE"
	nativeSmokeExecutableEnv = "SYMMETRY_OPENCODE_NATIVE_SMOKE_EXECUTABLE"
	nativeSmokeTimeout       = 30 * time.Second
)

// TestNativeTransportOpenAndClose is deliberately opt-in because it starts a
// locally installed OpenCode binary. It proves only process/HTTP transport
// startup, session creation, and close behavior; it never starts a turn or
// sends a prompt to a model.
func TestNativeTransportOpenAndClose(t *testing.T) {
	if os.Getenv(nativeSmokeEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_OPENCODE_NATIVE_SMOKE=1 to run the native OpenCode transport smoke")
	}

	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" {
		t.Fatalf("%s must name the absolute OpenCode %s executable", nativeSmokeExecutableEnv, TestedVersion)
	}
	if !filepath.IsAbs(executable) {
		t.Fatalf("%s must be absolute", nativeSmokeExecutableEnv)
	}
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatalf("stat native OpenCode executable: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s must name a regular executable file", nativeSmokeExecutableEnv)
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("create isolated OpenCode workspace: %v", err)
	}
	environment := nativeOpenCodeSmokeEnvironment(t, root)

	versionContext, versionCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer versionCancel()
	versionCommand := exec.CommandContext(versionContext, executable, "--version")
	versionCommand.Dir = workspace
	versionCommand.Env = environment
	versionOutput, err := versionCommand.Output()
	if err != nil {
		t.Fatal("run native OpenCode version check failed")
	}
	if strings.TrimSpace(string(versionOutput)) != TestedVersion {
		t.Fatal("native OpenCode version did not equal the tested version")
	}
	t.Logf("native OpenCode transport smoke: platform=%s version=%s", runtime.GOOS, TestedVersion)

	var eventMutex sync.Mutex
	eventCount := 0
	sink := harness.EventSinkFunc(func(context.Context, harness.Event) error {
		eventMutex.Lock()
		eventCount++
		eventMutex.Unlock()
		return nil
	})

	var persistMutex sync.Mutex
	persistCount := 0
	persistedPID := 0
	persistedIdentity := ""
	startContext, startCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace:  workspace,
		Invocation: execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			persistMutex.Lock()
			persistCount++
			persistedPID = pid
			persistedIdentity = identity
			persistMutex.Unlock()
			return nil
		},
	}, sink)
	if err != nil {
		t.Fatal("start native OpenCode transport failed")
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
		defer cleanupCancel()
		if err := session.Close(cleanupContext); err != nil {
			t.Error("cleanup native OpenCode session failed")
		}
		waitContext, waitCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
		defer waitCancel()
		if _, err := session.Wait(waitContext); err != nil {
			t.Error("wait for native OpenCode cleanup failed")
		}
	})

	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native OpenCode adapter did not return a staged session")
	}
	persistMutex.Lock()
	gotPersistCount, gotPersistedPID, gotPersistedIdentity := persistCount, persistedPID, persistedIdentity
	persistMutex.Unlock()
	pid, identity := staged.ProcessDetails()
	if gotPersistCount != 1 || gotPersistedPID != pid || gotPersistedIdentity != identity || pid <= 0 || identity == "" {
		t.Fatal("native OpenCode process identity was not persisted exactly once before session exposure")
	}

	openContext, openCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open isolated native OpenCode session failed: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("native OpenCode session returned an empty identity")
	}
	replayedHandle, err := staged.Open(openContext)
	if err != nil {
		t.Fatal("reopen native OpenCode session failed")
	}
	if replayedHandle != handle {
		t.Fatal("native OpenCode session identity changed across Open replay")
	}

	eventMutex.Lock()
	gotEventCount := eventCount
	eventMutex.Unlock()
	if gotEventCount == 0 {
		t.Fatal("native OpenCode transport emitted no observable process events")
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("close native OpenCode session failed")
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatal("wait for native OpenCode session failed")
	}
	closed = true
	if result.Kind != harness.ResultUnknown || result.Semantic != nil || !result.Process.Terminated {
		t.Fatal("transport-only native OpenCode smoke reported an unexpected process result")
	}
}

func nativeOpenCodeSmokeEnvironment(t *testing.T, root string) []string {
	t.Helper()
	home := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	cacheDir := filepath.Join(root, "cache")
	dataDir := filepath.Join(root, "data")
	stateDir := filepath.Join(root, "state")
	tempDir := filepath.Join(root, "tmp")
	appDataDir := filepath.Join(root, "appdata")
	localAppDataDir := filepath.Join(root, "localappdata")
	for _, directory := range []string{home, configDir, cacheDir, dataDir, stateDir, tempDir, appDataDir, localAppDataDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("create isolated native OpenCode environment failed")
		}
	}

	environment := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"APPDATA=" + appDataDir,
		"LOCALAPPDATA=" + localAppDataDir,
		"XDG_CONFIG_HOME=" + configDir,
		"XDG_CACHE_HOME=" + cacheDir,
		"XDG_DATA_HOME=" + dataDir,
		"XDG_STATE_HOME=" + stateDir,
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
	}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			if name == "PATH" || runtime.GOOS == "windows" {
				t.Fatalf("native OpenCode smoke requires %s in the parent environment", name)
			}
			continue
		}
		environment = append(environment, name+"="+value)
	}
	return environment
}
