//go:build windows || linux

package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type smokeRecorder struct {
	t      *testing.T
	events []harness.Event
}

func (recorder *smokeRecorder) Handle(_ context.Context, event harness.Event) error {
	recorder.events = append(recorder.events, event)
	payload := ""
	if len(event.Payload) != 0 {
		payload = string(event.Payload)
		if len(payload) > 300 {
			payload = payload[:300] + "..."
		}
	}
	recorder.t.Logf("event kind=%s seq=%d diagnostic=%t code=%s message=%q data=%q payload=%s", event.Kind, event.Sequence, event.Diagnostic, event.Code, event.Message, string(event.Data), payload)
	return nil
}

func TestLocalCredentialedTurnSmoke(t *testing.T) {
	if os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE") != "1" {
		t.Skip("set SYMMETRY_CODEX_LIVE_SMOKE=1 to run the credentialed Codex turn")
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
	goal := os.Getenv("SYMMETRY_CODEX_LIVE_SMOKE_GOAL")

	workspace := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = workspace
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("smoke\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.email=smoke@example.test", "-c", "user.name=smoke", "commit", "-qm", "init"}} {
		command = exec.Command("git", args...)
		command.Dir = workspace
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
	}
	revParse := exec.Command("git", "rev-parse", "HEAD")
	revParse.Dir = workspace
	commitOutput, err := revParse.Output()
	if err != nil {
		t.Fatal(err)
	}
	treeSum := sha256.Sum256([]byte("smoke\n"))
	subject := protocol.Subject{
		ResourceID: "11111111-1111-4111-8111-111111111111",
		Commit:     strings.TrimSpace(string(commitOutput)),
		TreeDigest: "sha256:" + hex.EncodeToString(treeSum[:]),
	}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	subjectJSON, err := json.Marshal(subject)
	if err != nil {
		t.Fatal(err)
	}
	if goal == "" {
		goal = fmt.Sprintf(`Work only inside this repository. Create a file named hello.txt whose exact contents are "hello from codex" with no trailing newline. Do not commit.

Report exactly one JSON object and nothing else, with these exact values:
- schema_version: "symmetry.task_result.v1"
- result_id: "22222222-2222-4222-8222-222222222222"
- kind: "candidate_completion"
- subject: %s
- subject_hash: %q
- evidence_refs: []
- blocker: null
- proposed_next_action: null
- proposal: null
- reason: null
- diagnostics: []
- summary: a short description of the created file.`, string(subjectJSON), subjectHash)
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
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	session, err := adapter.Start(ctx, harness.StartRequest{
		AdmissionID:   "smoke",
		LocalHandleID: "smoke",
		Workspace:     workspace,
		Context:       json.RawMessage(`{"task":"credentialed smoke","subject":` + string(subjectJSON) + `,"subject_hash":"` + subjectHash + `"}`),
		Goal:          goal,
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
	handle, err := staged.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Logf("native thread=%s", handle.ID)
	if err := staged.StartTurn(ctx, harness.TurnRequest{Goal: goal, Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if err := staged.WaitTurn(ctx); err != nil {
		t.Fatalf("wait turn: %v", err)
	}
	entries, _ := os.ReadDir(workspace)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".txt") {
			content, _ := os.ReadFile(filepath.Join(workspace, entry.Name()))
			t.Logf("artifact %s = %q", entry.Name(), string(content))
		}
	}
	if err := session.Close(ctx); err != nil {
		t.Logf("close: %v", err)
	}
	result, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	encoded, _ := json.MarshalIndent(result, "", " ")
	t.Logf("task result: %s", encoded)
	if result.Kind != harness.ResultSucceeded {
		t.Fatalf("result kind = %s", result.Kind)
	}
}
