//go:build windows || linux

package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	claudeLiveSmokeEnv           = "SYMMETRY_CLAUDE_LIVE_SMOKE"
	claudeLiveSmokeExecutableEnv = "SYMMETRY_CLAUDE_LIVE_SMOKE_EXECUTABLE"
	claudeLiveSmokeBaseURLEnv    = "SYMMETRY_CLAUDE_LIVE_SMOKE_BASE_URL"
	claudeLiveSmokeTokenEnv      = "SYMMETRY_CLAUDE_LIVE_SMOKE_TOKEN"
	claudeLiveSmokeModelEnv      = "SYMMETRY_CLAUDE_LIVE_SMOKE_MODEL"
	claudeLiveSmokeGoalEnv       = "SYMMETRY_CLAUDE_LIVE_SMOKE_GOAL"
	claudeLiveSmokeFramesEnv     = "SYMMETRY_CLAUDE_LIVE_SMOKE_FRAMES"
	claudeLiveSmokeShimEnv       = "SYMMETRY_CLAUDE_LIVE_SMOKE_SHIM"
)

// claudeLiveRecorder records the native evidence of one credentialed run: the
// event projection, the raw native frames (so a sanitized capture can be
// committed) and the ordering facts the goal's deferred-init rule depends on.
type claudeLiveRecorder struct {
	t *testing.T

	mutex            sync.Mutex
	events           []Event
	initFrames       uint64
	terminalFrames   uint64
	frameFile        *os.File
	frameWriteFailed error
}

func (recorder *claudeLiveRecorder) Handle(_ context.Context, event Event) error {
	recorder.mutex.Lock()
	recorder.events = append(recorder.events, event)
	if event.Kind == EventNativeFrame {
		switch {
		case claudeFrameIsInit(event.Payload):
			recorder.initFrames++
			if recorder.terminalFrames != 0 {
				recorder.mutex.Unlock()
				return fmt.Errorf("Claude native init frame arrived after a terminal frame")
			}
		case claudeFrameIsResult(event.Payload):
			recorder.terminalFrames++
		}
		if recorder.frameFile != nil && recorder.frameWriteFailed == nil {
			if _, err := recorder.frameFile.Write(append(append([]byte(nil), event.Payload...), '\n')); err != nil {
				recorder.frameWriteFailed = err
			}
		}
	}
	recorder.mutex.Unlock()
	recorder.t.Logf("event kind=%s code=%s sequence=%d diagnostic=%t message=%q", event.Kind, event.Code, event.Sequence, event.Diagnostic, event.Message)
	return nil
}

func (recorder *claudeLiveRecorder) identitySnapshot() (events int, frames int, initFrames uint64, terminalFrames uint64) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	frames = 0
	for _, event := range recorder.events {
		if event.Kind == EventNativeFrame {
			frames++
		}
	}
	return len(recorder.events), frames, recorder.initFrames, recorder.terminalFrames
}

func (recorder *claudeLiveRecorder) observedInit() bool {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.initFrames != 0
}

func (recorder *claudeLiveRecorder) openFrameFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	recorder.mutex.Lock()
	recorder.frameFile = file
	recorder.mutex.Unlock()
	return nil
}

func (recorder *claudeLiveRecorder) closeFrameFile() error {
	recorder.mutex.Lock()
	file := recorder.frameFile
	writeErr := recorder.frameWriteFailed
	recorder.frameFile = nil
	recorder.mutex.Unlock()
	if file == nil {
		return writeErr
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func claudeFrameIsInit(payload []byte) bool {
	var record struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(payload, &record) != nil {
		return false
	}
	return record.Type == "system" && record.Subtype == "init"
}

func claudeFrameIsResult(payload []byte) bool {
	var record struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &record) != nil {
		return false
	}
	return record.Type == "result"
}

// claudeLiveEnvironment builds the child environment for a credentialed run:
// launch essentials from the parent, an isolated home and config directory, and
// the provider endpoint from the operator's own environment. Credential values
// are read at run time and are never recorded by this test.
func claudeLiveEnvironment(t *testing.T, home, configDirectory string) []string {
	t.Helper()
	environment, err := execution.BuildEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	isolated := environment[:0]
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH":
			continue
		}
		isolated = append(isolated, entry)
	}
	baseURL := strings.TrimSpace(os.Getenv(claudeLiveSmokeBaseURLEnv))
	token := os.Getenv(claudeLiveSmokeTokenEnv)
	if baseURL == "" || token == "" {
		t.Fatalf("%s and %s are required for the credentialed Claude run", claudeLiveSmokeBaseURLEnv, claudeLiveSmokeTokenEnv)
	}
	isolated = append(isolated,
		"HOME="+home,
		"USERPROFILE="+home,
		"CLAUDE_CONFIG_DIR="+configDirectory,
		"ANTHROPIC_BASE_URL="+baseURL,
		"ANTHROPIC_AUTH_TOKEN="+token,
		"DISABLE_TELEMETRY=1",
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)
	if model := strings.TrimSpace(os.Getenv(claudeLiveSmokeModelEnv)); model != "" {
		isolated = append(isolated, "ANTHROPIC_MODEL="+model, "ANTHROPIC_DEFAULT_SONNET_MODEL="+model)
	}
	return isolated
}

// claudeLiveWorkspace creates a real one-commit repository and returns its exact
// subject identity plus the requested artifact path.
func claudeLiveWorkspace(t *testing.T) (workspace string, subject protocol.Subject, subjectHash string, subjectJSON []byte) {
	t.Helper()
	workspace = t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = workspace
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("claude live smoke\n"), 0o600); err != nil {
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
	treeSum := sha256.Sum256([]byte("claude live smoke\n"))
	subject = protocol.Subject{
		ResourceID: "11111111-1111-4111-8111-111111111111",
		Commit:     strings.TrimSpace(string(commitOutput)),
		TreeDigest: "sha256:" + hex.EncodeToString(treeSum[:]),
	}
	subjectHash, err = subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	subjectJSON, err = json.Marshal(subject)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, subject, subjectHash, subjectJSON
}

func claudeLiveAdapter(t *testing.T) (*ClaudeCandidateAdapter, string) {
	t.Helper()
	if os.Getenv(claudeLiveSmokeEnv) != "1" {
		t.Skip("set " + claudeLiveSmokeEnv + "=1 to run the credentialed Claude Code smoke")
	}
	executable := strings.TrimSpace(os.Getenv(claudeLiveSmokeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute Claude Code %s executable", claudeLiveSmokeExecutableEnv, ClaudeTestedVersion)
	}
	// Every credentialed smoke proves the pinned client, so the executable
	// identity is verified before any turn is launched: same version, same
	// transport and the exact tested bytes. A different binary would otherwise
	// spend credentials while claiming exact-client evidence.
	adapter := NewClaudeCandidateAdapter(executable)
	capabilities, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe %s before the credentialed run: %v", executable, err)
	}
	if capabilities.NativeVersion != ClaudeTestedVersion || !capabilities.TransportVerified {
		t.Fatalf("capabilities = %+v, want the tested %s client with verified transport", capabilities, ClaudeTestedVersion)
	}
	digest, err := HashExecutableFile(context.Background(), executable)
	if err != nil {
		t.Fatalf("hash %s before the credentialed run: %v", executable, err)
	}
	if digest != ClaudeTestedExecutableHash {
		t.Fatalf("resolved executable digest = %q, want the tested blob %q", digest, ClaudeTestedExecutableHash)
	}
	if runtime.GOOS == "windows" && (!capabilities.Verified || !capabilities.Start || !capabilities.Events || !capabilities.Cancel) {
		t.Fatalf("capabilities = %+v, want the promoted Windows lifecycle before a credentialed run", capabilities)
	}
	return adapter, executable
}

func claudeLiveStart(t *testing.T, adapter *ClaudeCandidateAdapter, workspace, goal string, contextJSON json.RawMessage, recorder *claudeLiveRecorder) (Session, StagedSession) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configDirectory := filepath.Join(root, "claude-config")
	for _, directory := range []string{home, configDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := claudeLiveEnvironment(t, home, configDirectory)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	session, err := adapter.Start(ctx, StartRequest{
		AdmissionID:   "claude-live-smoke",
		LocalHandleID: "claude-live-smoke",
		Workspace:     workspace,
		Goal:          goal,
		ModelProfile:  "default",
		Context:       contextJSON,
		Invocation:    execution.Invocation{Env: environment},
		PersistProcess: func(pid int, identity string) error {
			t.Logf("native process pid=%d identity=%s", pid, identity)
			return nil
		},
	}, recorder)
	if err != nil {
		t.Fatalf("start credentialed Claude session: %v", err)
	}
	staged, ok := session.(StagedSession)
	if !ok {
		t.Fatalf("session %T does not implement StagedSession", session)
	}
	return session, staged
}

// TestClaudeCandidateLiveRepositoryTurnSmoke drives the composed Claude adapter
// against the exact installed client, a credentialed provider endpoint and a real
// repository. It proves start, the event stream, the deferred system/init
// barrier, the structured-output TaskResult and its repository artifact.
func TestClaudeCandidateLiveRepositoryTurnSmoke(t *testing.T) {
	adapter, executable := claudeLiveAdapter(t)
	t.Logf("credentialed Claude executable: %s", executable)
	workspace, subject, subjectHash, subjectJSON := claudeLiveWorkspace(t)
	goal := strings.TrimSpace(os.Getenv(claudeLiveSmokeGoalEnv))
	if goal == "" {
		goal = fmt.Sprintf(`Work only inside this repository. Create a file named hello.txt whose exact contents are "hello from claude" with no trailing newline. Do not commit, do not modify any other file.

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
	recorder := &claudeLiveRecorder{t: t}
	if path := strings.TrimSpace(os.Getenv(claudeLiveSmokeFramesEnv)); path != "" {
		if err := recorder.openFrameFile(path); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := recorder.closeFrameFile(); err != nil {
				t.Errorf("write native frame capture: %v", err)
			}
		})
	}
	session, staged := claudeLiveStart(t, adapter, workspace, goal, json.RawMessage(`{"task":"claude_live_repository_turn","subject":`+string(subjectJSON)+`,"subject_hash":"`+subjectHash+`"}`), recorder)

	// The deferred system/init rule: Open must return without requiring the
	// native init record, because 2.1.322 and later emit it only after the first
	// user frame.
	openContext, openCancel := context.WithTimeout(context.Background(), 30*time.Second)
	handle, err := staged.Open(openContext)
	openCancel()
	if err != nil {
		t.Fatalf("open credentialed Claude session (no system/init before the first user frame): %v", err)
	}
	if handle.ID == "" || recorder.observedInit() {
		t.Fatalf("open handle = %+v with init observed = %t, want the requested identity and no init before the first user frame", handle, recorder.observedInit())
	}

	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: goal, Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("start credentialed Claude turn: %v", err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("wait credentialed Claude turn: %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Logf("close credentialed Claude session: %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait credentialed Claude session: %v", err)
	}
	encoded, _ := json.MarshalIndent(result, "", " ")
	t.Logf("task result: %s", encoded)

	content, readErr := os.ReadFile(filepath.Join(workspace, "hello.txt"))
	if readErr != nil {
		t.Fatalf("repository artifact: %v", readErr)
	}
	if got, want := string(content), "hello from claude"; got != want {
		t.Fatalf("artifact hello.txt = %q, want %q", got, want)
	}
	if result.Kind != ResultSucceeded || result.Semantic == nil || result.Semantic.Kind != protocol.TaskResultCandidateCompletion {
		t.Fatalf("result = %+v, want a canonical candidate_completion TaskResult", result)
	}
	if result.Semantic.Subject.Commit != subject.Commit || result.Semantic.SubjectHash != subjectHash {
		t.Fatalf("result subject = %+v hash %q, want commit %q hash %q", result.Semantic.Subject, result.Semantic.SubjectHash, subject.Commit, subjectHash)
	}
	if !result.Process.Terminated || result.Process.ContainmentError != nil || result.Process.TerminationError != nil || result.Process.OutputTruncated {
		t.Fatalf("process settlement = %+v, want a contained, untruncated stop", result.Process)
	}
	events, frames, initFrames, terminalFrames := recorder.identitySnapshot()
	t.Logf("observations: events=%d frames=%d init_frames=%d terminal_frames=%d event_count=%d exit=%d", events, frames, initFrames, terminalFrames, result.EventCount, result.Process.ExitCode)
	if initFrames != 1 || terminalFrames != 1 {
		t.Fatalf("native frames: init=%d terminal=%d, want exactly one of each", initFrames, terminalFrames)
	}
	if err := recorder.closeFrameFile(); err != nil {
		t.Fatalf("native frame capture: %v", err)
	}
}

// TestClaudeCandidateLiveCancelSmoke cancels an active native turn and requires
// bounded process termination with a cancelled result. The goal keeps the turn
// open with sequential file writes because the promoted permission policy denies
// every shell command that would prompt.
func TestClaudeCandidateLiveCancelSmoke(t *testing.T) {
	adapter, executable := claudeLiveAdapter(t)
	t.Logf("credentialed Claude executable: %s", executable)
	workspace, _, _, _ := claudeLiveWorkspace(t)
	goal := strings.TrimSpace(os.Getenv(claudeLiveSmokeGoalEnv))
	if goal == "" {
		goal = "Create 60 files named f-01.txt through f-60.txt, one Write call per file, each containing exactly its own file name with no trailing newline. Do not use shell commands. Then set summary to \"60 files created\"."
	}
	recorder := &claudeLiveRecorder{t: t}
	session, staged := claudeLiveStart(t, adapter, workspace, goal, json.RawMessage(`{"task":"claude_live_cancel"}`), recorder)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := staged.Open(ctx); err != nil {
		t.Fatalf("open credentialed Claude session: %v", err)
	}
	if err := staged.StartTurn(ctx, TurnRequest{Goal: goal, Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("start credentialed Claude turn: %v", err)
	}
	time.Sleep(15 * time.Second)
	receipt, err := session.Control(ctx, ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if err != nil {
		t.Fatalf("cancel control: %v (outcome=%s)", err, receipt.Outcome)
	}
	t.Logf("cancel receipt: outcome=%s message=%q", receipt.Outcome, receipt.Message)
	if receipt.Outcome != ControlApplied {
		t.Fatalf("cancel outcome = %s, want applied", receipt.Outcome)
	}
	if err := staged.WaitTurn(ctx); err != nil {
		t.Fatalf("wait cancelled Claude turn: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Logf("close cancelled Claude session: %v", err)
	}
	result, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("wait cancelled Claude session: %v", err)
	}
	encoded, _ := json.MarshalIndent(result, "", " ")
	t.Logf("cancel result: %s", encoded)
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %s, want cancelled", result.Kind)
	}
	if !result.Process.Terminated || result.Process.ContainmentError != nil || result.Process.TerminationError != nil {
		t.Fatalf("process settlement = %+v, want a contained stop", result.Process)
	}
	events, frames, initFrames, _ := recorder.identitySnapshot()
	if initFrames != 1 {
		t.Fatalf("native init frames = %d, want exactly one observed identity", initFrames)
	}
	t.Logf("observations: events=%d frames=%d init_frames=%d", events, frames, initFrames)
}

// TestClaudeCandidateLiveProbePromotesTheTestedBlob runs the promotion gate
// against the real installed client. Version, transport and the resolved
// executable bytes must all match before any operation appears, and no
// unproved operation may appear at all.
func TestClaudeCandidateLiveProbePromotesTheTestedBlob(t *testing.T) {
	adapter, executable := claudeLiveAdapter(t)
	digest, err := HashExecutableFile(context.Background(), executable)
	if err != nil {
		t.Fatalf("hash resolved executable: %v", err)
	}
	t.Logf("resolved executable digest: %s", digest)
	if digest != ClaudeTestedExecutableHash {
		t.Fatalf("resolved executable digest = %q, want the tested blob %q", digest, ClaudeTestedExecutableHash)
	}
	capabilities, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe exact tested blob: %v", err)
	}
	if capabilities.NativeVersion != ClaudeTestedVersion || !capabilities.VersionKnown || !capabilities.TransportVerified {
		t.Fatalf("capabilities = %+v, want the tested version with verified transport", capabilities)
	}
	if capabilities.Resume || capabilities.Handoff || capabilities.ApprovalResponse || capabilities.ProviderAccess || capabilities.HardCostLimit ||
		capabilities.Guidance != GuidanceUnsupported || capabilities.Pause != PauseUnsupported || capabilities.Usage != UsageUnknown {
		t.Fatalf("capabilities advertised unproved operations: %+v", capabilities)
	}
	promoted := capabilities.Verified && capabilities.Start && capabilities.Events && capabilities.Cancel
	if want := runtime.GOOS == "windows"; promoted != want {
		t.Fatalf("promoted = %t on %s, want %t: capabilities = %+v", promoted, runtime.GOOS, want, capabilities)
	}
	if promoted && capabilities.ImplementationVersion != claudeImplementationVersion {
		t.Fatalf("implementation version = %q, want %q", capabilities.ImplementationVersion, claudeImplementationVersion)
	}
	t.Logf("promotion projection: %+v", capabilities)
}

// TestClaudeCandidateLiveProbeRejectsDifferentBlob is the recorded negative
// control against a real executable: the `mise` PATH shim reports the same
// `2.1.323 (Claude Code)` version but is different bytes, so it promotes
// nothing. The observed rejection is the exact-blob gate; under host load the
// shim's own resolution has also exceeded the bounded probe deadline, which fails
// closed the same way, so both rejections are accepted here and
// TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob pins the exact-blob
// reason.
func TestClaudeCandidateLiveProbeRejectsDifferentBlob(t *testing.T) {
	_, executable := claudeLiveAdapter(t)
	shim := strings.TrimSpace(os.Getenv(claudeLiveSmokeShimEnv))
	if shim == "" || !filepath.IsAbs(shim) {
		t.Fatalf("%s must name the absolute shim executable used as the blob-mismatch control", claudeLiveSmokeShimEnv)
	}
	shimDigest, err := HashExecutableFile(context.Background(), shim)
	if err != nil {
		t.Fatalf("hash shim executable: %v", err)
	}
	t.Logf("shim %s digest %s (tested blob %s at %s)", shim, shimDigest, ClaudeTestedExecutableHash, executable)
	if shimDigest == ClaudeTestedExecutableHash {
		t.Fatalf("shim digest equals the tested blob; the control needs different bytes")
	}
	capabilities, err := NewClaudeCandidateAdapter(shim).Probe(context.Background())
	if err == nil {
		t.Fatal("probe of a different blob succeeded, want a fail-closed rejection")
	}
	t.Logf("shim probe rejection: %v", err)
	if capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("capabilities = %+v, want no promoted operation for a different blob", capabilities)
	}
	if !errors.Is(err, ErrNativeUnverified) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe error = %v, want a bounded unverified or deadline rejection", err)
	}
	for _, capability := range []Capability{CapabilityStart, CapabilityEvents, CapabilityCancel} {
		if reason := capabilities.Unsupported[string(capability)]; reason == "" {
			t.Fatalf("%s carries no unsupported reason: %+v", capability, capabilities)
		}
	}

	// Isolate the blob gate itself from shim startup cost: the real client
	// answers version and help, and the resolved digest is the shim's real
	// digest. Nothing may be promoted.
	capabilities, err = probeClaudeAdapterCapabilities(context.Background(), executable, claudeLiveForeignBlobRunner{digest: shimDigest})
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("blob-gate probe error = %v, want ErrNativeUnverified", err)
	}
	if capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("capabilities = %+v, want no promoted operation for foreign bytes", capabilities)
	}
	if reason := capabilities.Unsupported[string(CapabilityStart)]; !strings.Contains(reason, "exact tested executable blob") {
		t.Fatalf("start reason = %q, want the exact-blob reason", reason)
	}
}

// claudeLiveForeignBlobRunner executes the real client but reports another real
// executable's digest, so the exact-blob gate is exercised with recorded bytes
// instead of a synthetic hash.
type claudeLiveForeignBlobRunner struct {
	digest string
}

func (runner claudeLiveForeignBlobRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return osClaudeCommandRunner{}.Run(ctx, executable, args...)
}

func (runner claudeLiveForeignBlobRunner) ExecutableDigest(context.Context, string) (string, error) {
	return runner.digest, nil
}
