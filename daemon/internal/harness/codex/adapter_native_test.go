//go:build linux || windows

package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	nativeSmokeEnabledEnv    = "SYMMETRY_CODEX_NATIVE_SMOKE"
	nativeSmokeExecutableEnv = "SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE"
	nativeSmokeTimeout       = 30 * time.Second
)

// This opt-in test starts the real app-server but never submits a model turn.
// Its isolated CODEX_HOME deliberately contains no user credentials or settings.
func TestNativeTransportOpenAndClose(t *testing.T) {
	if os.Getenv(nativeSmokeEnabledEnv) != "1" {
		t.Skip("set SYMMETRY_CODEX_NATIVE_SMOKE=1 to run the native Codex transport smoke")
	}
	executable := strings.TrimSpace(os.Getenv(nativeSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Codex %s executable", nativeSmokeExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("native Codex executable must be an existing regular file")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal("create isolated native Codex workspace failed")
	}
	environment := nativeCodexSmokeEnvironment(t, root)
	probeContext, probeCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer probeCancel()
	version := exec.CommandContext(probeContext, executable, "--version")
	version.Dir, version.Env = workspace, environment
	versionOutput, err := version.Output()
	if err != nil || parseVersion(string(versionOutput)) != TestedVersion {
		t.Fatal("native Codex version did not equal the tested version")
	}
	schemaDirectory := filepath.Join(root, "schema")
	schema := exec.CommandContext(probeContext, executable, "app-server", "generate-json-schema", "--out", schemaDirectory)
	schema.Dir, schema.Env = workspace, environment
	if err := schema.Run(); err != nil {
		t.Fatal("generate isolated native Codex schema failed")
	}
	bundle, err := os.ReadFile(filepath.Join(schemaDirectory, "codex_app_server_protocol.v2.schemas.json"))
	if err != nil {
		t.Fatal("read native Codex schema bundle failed")
	}
	digest := sha256.Sum256(bundle)
	if "sha256:"+hex.EncodeToString(digest[:]) != TestedSchemaHash {
		t.Fatal("native Codex schema did not equal the tested schema")
	}
	t.Logf("native Codex transport smoke: platform=%s version=%s", runtime.GOOS, TestedVersion)

	var mutex sync.Mutex
	persistCount := 0
	persistedPID := 0
	persistedIdentity := ""
	prematureSessionEvent := false
	sink := harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		mutex.Lock()
		defer mutex.Unlock()
		if persistCount != 1 {
			return errors.New("native Codex event preceded process identity persistence")
		}
		if event.Kind == harness.EventSessionStarted {
			prematureSessionEvent = true
		}
		return nil
	})
	startContext, startCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer startCancel()
	session, err := NewAdapter(executable).Start(startContext, harness.StartRequest{
		Workspace: workspace,
		Invocation: execution.Invocation{
			Env: environment,
		},
		PersistProcess: func(pid int, identity string) error {
			if pid <= 0 || strings.TrimSpace(identity) == "" {
				return os.ErrInvalid
			}
			mutex.Lock()
			defer mutex.Unlock()
			persistCount++
			persistedPID, persistedIdentity = pid, identity
			return nil
		},
	}, sink)
	closed := false
	if session != nil {
		t.Cleanup(func() {
			if closed {
				return
			}
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
			defer cleanupCancel()
			if err := session.Close(cleanupContext); err != nil {
				t.Error("cleanup native Codex session failed")
			}
			result, err := session.Wait(cleanupContext)
			if err != nil {
				t.Error("wait for native Codex cleanup failed")
			} else {
				assertNativeCodexSmokeStopped(t, result.Process)
			}
		})
	}
	if err != nil || session == nil {
		t.Fatal("start native Codex transport failed")
	}
	staged, ok := session.(harness.StagedSession)
	if !ok {
		t.Fatal("native Codex adapter did not return a staged session")
	}
	mutex.Lock()
	gotCount, gotPID, gotIdentity := persistCount, persistedPID, persistedIdentity
	mutex.Unlock()
	pid, identity := staged.ProcessDetails()
	if gotCount != 1 || gotPID != pid || gotIdentity != identity || pid <= 0 || identity == "" {
		t.Fatal("native Codex process identity was not persisted exactly once before session exposure")
	}

	openContext, openCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer openCancel()
	handle, err := staged.Open(openContext)
	if err != nil {
		t.Fatalf("open isolated native Codex session failed: %v", err)
	}
	if handle.ID == "" {
		t.Fatal("native Codex thread returned an empty identity")
	}
	replayedHandle, err := staged.Open(openContext)
	if err != nil || replayedHandle != handle {
		t.Fatal("native Codex thread identity changed across Open replay")
	}
	mutex.Lock()
	hadPrematureSessionEvent := prematureSessionEvent
	mutex.Unlock()
	if hadPrematureSessionEvent {
		t.Fatal("native Codex exposed session_started before a turn was admitted")
	}

	closeContext, closeCancel := context.WithTimeout(context.Background(), nativeSmokeTimeout)
	defer closeCancel()
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("close native Codex session failed")
	}
	result, err := staged.Wait(closeContext)
	if err != nil {
		t.Fatal("wait for native Codex session failed")
	}
	if result.Kind != harness.ResultCancelled || result.Semantic != nil ||
		result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult {
		t.Fatalf("transport-only native Codex smoke result: kind=%s semantic_present=%t", result.Kind, result.Semantic != nil)
	}
	assertNativeCodexSmokeStopped(t, result.Process)
	if err := staged.Close(closeContext); err != nil {
		t.Fatal("repeated native Codex Close was not idempotent")
	}
	closed = true
}

func assertNativeCodexSmokeStopped(t *testing.T, result execution.Result) {
	t.Helper()
	if !result.Terminated || result.ContainmentError != nil || result.TerminationError != nil ||
		result.SinkError != nil || result.OutputError != nil || !result.OutputTruncated {
		t.Errorf("native Codex stop: terminated=%t containment_error=%t termination_error=%t sink_error=%t output_error=%t output_truncated=%t",
			result.Terminated, result.ContainmentError != nil, result.TerminationError != nil,
			result.SinkError != nil, result.OutputError != nil, result.OutputTruncated)
	}
	t.Logf("native Codex stop: output_truncated=%t; output delivery ends at the termination barrier, not a full drain", result.OutputTruncated)
}

func nativeCodexSmokeEnvironment(t *testing.T, root string) []string {
	t.Helper()
	directories := map[string]string{
		"CODEX_HOME":      "codex",
		"HOME":            "home",
		"USERPROFILE":     "home",
		"APPDATA":         "appdata",
		"LOCALAPPDATA":    "localappdata",
		"XDG_CONFIG_HOME": "config",
		"XDG_CACHE_HOME":  "cache",
		"XDG_DATA_HOME":   "data",
		"XDG_STATE_HOME":  "state",
		"TEMP":            "tmp",
		"TMP":             "tmp",
	}
	environment := make([]string, 0, len(directories)+5)
	for name, directory := range directories {
		path := filepath.Join(root, directory)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal("create isolated native Codex environment failed")
		}
		environment = append(environment, name+"="+path)
	}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "ComSpec", "PATHEXT"} {
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			if name == "PATH" || runtime.GOOS == "windows" {
				t.Fatalf("native Codex smoke requires %s in the parent environment", name)
			}
			continue
		}
		environment = append(environment, name+"="+value)
	}
	return environment
}
