//go:build windows || linux

package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestLocalCredentialedCancelSmoke(t *testing.T) {
	if os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE") != "1" {
		t.Skip("set SYMMETRY_CODEX_LIVE_SMOKE=1 to run the credentialed Codex cancellation")
	}
	home := os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE_HOME")
	if home == "" {
		t.Fatal("SYMMETRY_CODEX_LIVE_SMOKE_HOME is required")
	}
	executable := os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE_EXECUTABLE")
	if executable == "" {
		executable = "codex"
	}
	model := os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE_MODEL")
	provider := os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE_PROVIDER")

	workspace := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = workspace
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("cancel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.email=smoke@example.test", "-c", "user.name=smoke", "commit", "-qm", "init"}} {
		command = exec.Command("git", args...)
		command.Dir = workspace
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Setenv("CODEX_HOME", home); err != nil {
		t.Fatal(err)
	}
	environment, err := execution.BuildEnvironment("CODEX_HOME", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP")
	if err != nil {
		t.Fatal(err)
	}
	writer := &smokeRecorder{t: t}
	adapter := NewAdapterWithNativeIdentity(executable, model, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := adapter.Start(ctx, harness.StartRequest{
		AdmissionID:   "smoke-cancel",
		LocalHandleID: "smoke-cancel",
		Workspace:     workspace,
		Context:       json.RawMessage(`{"task":"cancel smoke"}`),
		Goal:          `Run the shell command that sleeps for 180 seconds before doing anything else. After it finishes, create done.txt.`,
		Invocation:    execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			t.Logf("process pid=%d identity=%s", pid, identity)
			return nil
		},
	}, harness.EventSinkFunc(writer.Handle))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := staged.StartTurn(ctx, harness.TurnRequest{
		Goal:    `Run the shell command that sleeps for 180 seconds before doing anything else. After it finishes, create done.txt.`,
		Context: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("start turn: %v", err)
	}
	time.Sleep(20 * time.Second)
	receipt, err := session.Control(ctx, harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if err != nil {
		t.Fatalf("cancel control: %v (outcome=%s)", err, receipt.Outcome)
	}
	t.Logf("cancel receipt: outcome=%s message=%q", receipt.Outcome, receipt.Message)
	if err := staged.WaitTurn(ctx); err != nil {
		t.Fatalf("wait turn: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Logf("close: %v", err)
	}
	result, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	encoded, _ := json.MarshalIndent(result, "", " ")
	t.Logf("cancel result: %s", encoded)
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("result kind = %s, want cancelled", result.Kind)
	}
}
