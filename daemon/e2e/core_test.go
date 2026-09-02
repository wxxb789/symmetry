package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/app"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	enrollmentToken = "development-enrollment-token"
	operatorToken   = "development-operator-token"
)

func TestCoreDaemonWorkflows(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startDaemon(t, ctx, environment, "core", nil)
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	success := submit(t, operator, daemon, "success", "success")
	waitForTask(t, operator, success.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})

	slow := submit(t, operator, daemon, "capacity-slow", "slow")
	waitForTask(t, operator, slow.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})
	queued := submit(t, operator, daemon, "capacity-queued", "success")
	assertTaskRemainsQueued(t, operator, queued.TaskID, 1500*time.Millisecond)
	if _, err := operator.CancelTask(context.Background(), slow.TaskID); err != nil {
		t.Fatalf("cancel slow task: %v", err)
	}
	waitForTask(t, operator, slow.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "cancelled"
	})
	waitForTask(t, operator, queued.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})

	waiting := submit(t, operator, daemon, "wait-input", "wait_input")
	waitForTask(t, operator, waiting.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "waiting_for_input"
	})
	input := protocol.TaskInputRequest{Input: json.RawMessage(`{"answer":"continue"}`)}
	if _, err := operator.SubmitInput(context.Background(), waiting.TaskID, unique("input"), input); err != nil {
		t.Fatalf("submit input: %v", err)
	}
	waitForTask(t, operator, waiting.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})

	failed := submit(t, operator, daemon, "failure", "fail")
	waitForTask(t, operator, failed.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "failed"
	})
	afterFailure := submit(t, operator, daemon, "after-failure", "success")
	waitForTask(t, operator, afterFailure.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
}

func TestPollingFallbackDispatchesWithoutNotifications(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startDaemon(t, ctx, environment, "poll", silentNotifications{})
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	task := submit(t, operator, daemon, "poll-fallback", "success")
	waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
}

func TestStaleRuntimeEpochCannotOverwriteNewGeneration(t *testing.T) {
	environment := loadEnvironment(t)
	httpClient := localHTTPClient(5 * time.Second)
	enrollment, err := control.NewEnrollmentClient(environment.baseURL, httpClient)
	if err != nil {
		t.Fatalf("new enrollment client: %v", err)
	}
	machine, err := enrollment.Enroll(context.Background(), environment.enrollmentToken, protocol.EnrollRequest{
		Machine: protocol.MachineEnrollment{Name: unique("fence-machine")},
	})
	if err != nil {
		t.Fatalf("enroll machine: %v", err)
	}
	machineClient, err := control.NewClient(environment.baseURL, machine.MachineToken, httpClient)
	if err != nil {
		t.Fatalf("new machine client: %v", err)
	}
	operator := newOperator(t, environment)
	runtimeKey := unique("fence-runtime")
	profile := unique("fence-profile")
	workspace := unique("fence-workspace")
	first := registerRuntime(t, machineClient, runtimeKey, profile, workspace)
	task, err := operator.SubmitTask(context.Background(), unique("fence-task"), protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal:         "prove stale fencing",
		AgentProfile: profile,
		Workspace:    workspace,
		Input:        json.RawMessage(`{"mode":"slow"}`),
	}})
	if err != nil {
		t.Fatalf("submit fenced task: %v", err)
	}
	assignment := waitForAssignment(t, machineClient, first, task.TaskID, 20*time.Second)
	claimID := mustID(t)
	claim, err := machineClient.Claim(context.Background(), assignment.RunID, protocol.ClaimRequest{
		RuntimeID: first.RuntimeID, RuntimeEpoch: first.RuntimeEpoch, Generation: assignment.Generation, ClaimID: claimID,
	})
	if err != nil {
		t.Fatalf("claim first generation: %v", err)
	}

	second := registerRuntime(t, machineClient, runtimeKey, profile, workspace)
	if second.RuntimeEpoch <= first.RuntimeEpoch {
		t.Fatalf("runtime epoch = %d, want greater than %d", second.RuntimeEpoch, first.RuntimeEpoch)
	}
	staleFence := protocol.Fence{
		RuntimeID: first.RuntimeID, RuntimeEpoch: first.RuntimeEpoch, Generation: claim.Generation,
		ClaimID: claim.ClaimID, LeaseToken: claim.LeaseToken,
	}
	err = machineClient.Transition(context.Background(), assignment.RunID, protocol.StateTransitionRequest{
		Fence: staleFence, TransitionID: mustID(t), State: "completed", Payload: json.RawMessage(`{}`),
	})
	if !control.IsOwnershipLost(err) {
		t.Fatalf("stale transition error = %v, want ownership_lost", err)
	}

	newAssignment := waitForAssignment(t, machineClient, second, task.TaskID, 30*time.Second)
	if newAssignment.Generation <= assignment.Generation {
		t.Fatalf("new generation = %d, want greater than %d", newAssignment.Generation, assignment.Generation)
	}
	newClaimID := mustID(t)
	newClaim, err := machineClient.Claim(context.Background(), newAssignment.RunID, protocol.ClaimRequest{
		RuntimeID: second.RuntimeID, RuntimeEpoch: second.RuntimeEpoch, Generation: newAssignment.Generation, ClaimID: newClaimID,
	})
	if err != nil {
		t.Fatalf("claim new generation: %v", err)
	}
	newFence := protocol.Fence{
		RuntimeID: second.RuntimeID, RuntimeEpoch: second.RuntimeEpoch, Generation: newClaim.Generation,
		ClaimID: newClaim.ClaimID, LeaseToken: newClaim.LeaseToken,
	}
	for _, target := range []string{"running", "completed"} {
		if err := machineClient.Transition(context.Background(), newAssignment.RunID, protocol.StateTransitionRequest{
			Fence: newFence, TransitionID: mustID(t), State: target, Payload: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("transition new generation to %s: %v", target, err)
		}
	}
	waitForTask(t, operator, task.TaskID, 10*time.Second, func(task protocol.Task) bool {
		return task.State == "completed" && task.Generation == newAssignment.Generation
	})
}

func TestDaemonReregistersAfterRestart(t *testing.T) {
	environment := loadEnvironment(t)
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", environment.enrollmentToken)
	operator := newOperator(t, environment)
	stateDir := t.TempDir()
	workspacePath := t.TempDir()
	profile := unique("restart-profile")
	workspace := unique("restart-workspace")
	value := daemonConfig(environment, "restart", stateDir, workspacePath, profile, workspace)

	runOnce := func(name string) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		defer func() {
			cancel()
			waitForDaemon(t, done)
		}()
		go func() { done <- app.Run(ctx, value, app.WithLogWriter(io.Discard)) }()
		task, err := operator.SubmitTask(context.Background(), unique(name), protocol.TaskSubmitRequest{Work: protocol.Work{
			Goal: name, AgentProfile: profile, Workspace: workspace, Input: json.RawMessage(`{"mode":"success"}`),
		}})
		if err != nil {
			t.Fatalf("submit %s task: %v", name, err)
		}
		waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
			return task.State == "completed"
		})
	}

	runOnce("before-daemon-restart")
	identityBefore := loadIdentity(t, stateDir)
	runOnce("after-daemon-restart")
	identityAfter := loadIdentity(t, stateDir)
	if identityAfter != identityBefore {
		t.Fatalf("machine identity changed across daemon restart")
	}
}

func TestDaemonReconnectReclaimsExpiredRunWithNewGeneration(t *testing.T) {
	environment := loadEnvironment(t)
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", environment.enrollmentToken)
	operator := newOperator(t, environment)
	stateDir := t.TempDir()
	workspacePath := t.TempDir()
	profile := unique("reconnect-profile")
	workspace := unique("reconnect-workspace")
	value := daemonConfig(environment, "reconnect", stateDir, workspacePath, profile, workspace)

	start := func(ctx context.Context) chan error {
		done := make(chan error, 1)
		go func() {
			done <- app.Run(ctx, value, app.WithHTTPClient(localHTTPClient(30*time.Second)), app.WithLogWriter(io.Discard))
		}()
		return done
	}

	firstContext, stopFirst := context.WithCancel(context.Background())
	firstDone := start(firstContext)
	firstStopped := false
	t.Cleanup(func() {
		if !firstStopped {
			stopFirst()
			waitForDaemon(t, firstDone)
		}
	})

	firstDaemon := daemonRun{done: firstDone, profile: profile, workspace: workspace}
	task := submit(t, operator, firstDaemon, "reconnect-slow", "slow")
	firstRun := waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})

	stopFirst()
	waitForDaemon(t, firstDone)
	firstStopped = true
	identityBefore := loadIdentity(t, stateDir)
	journalBefore := loadJournal(t, stateDir, state.RunKey{RunID: firstRun.RunID, Generation: firstRun.Generation})
	if journalBefore.LocalState != "running" {
		t.Fatalf("recovered journal state = %q, want running", journalBefore.LocalState)
	}
	if journalBefore.PID <= 0 {
		t.Fatalf("recovered journal PID = %d, want active process identity", journalBefore.PID)
	}

	// A persisted identity must let B start without an enrollment credential.
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "")
	secondContext, stopSecond := context.WithCancel(context.Background())
	secondDone := start(secondContext)
	secondStopped := false
	t.Cleanup(func() {
		if !secondStopped {
			stopSecond()
			waitForDaemon(t, secondDone)
		}
	})

	secondRun := waitForTask(t, operator, task.TaskID, 60*time.Second, func(task protocol.Task) bool {
		return task.State == "running" && task.Generation > firstRun.Generation
	})
	if secondRun.Generation <= firstRun.Generation {
		t.Fatalf("reclaimed generation = %d, want greater than %d", secondRun.Generation, firstRun.Generation)
	}

	if _, err := operator.CancelTask(context.Background(), task.TaskID); err != nil {
		t.Fatalf("cancel reclaimed task: %v", err)
	}
	waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "cancelled" && task.Generation == secondRun.Generation
	})

	stopSecond()
	waitForDaemon(t, secondDone)
	secondStopped = true
	assertJournalMissing(t, stateDir, state.RunKey{RunID: firstRun.RunID, Generation: firstRun.Generation})
	identityAfter := loadIdentity(t, stateDir)
	if identityAfter != identityBefore {
		t.Fatalf("machine identity changed across reconnect")
	}
}

func TestDaemonSurvivesControlRestart(t *testing.T) {
	marker := os.Getenv("SYMMETRY_E2E_RESTART_MARKER")
	if marker == "" {
		t.Skip("set SYMMETRY_E2E_RESTART_MARKER to run the external control restart scenario")
	}
	if _, err := os.Stat(marker); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart marker must name a new file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(marker) })
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startDaemon(t, ctx, environment, "control-restart", nil)
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})
	task := submit(t, operator, daemon, "control-restart", "slow")
	running := waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})
	if err := os.WriteFile(marker, []byte(task.TaskID+"\n"), 0o600); err != nil {
		t.Fatalf("write restart marker: %v", err)
	}
	waitForControlRestart(t, environment.baseURL, 90*time.Second)
	restored := waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})
	if restored.Generation != running.Generation {
		t.Fatalf("task generation changed across control restart: %d -> %d", running.Generation, restored.Generation)
	}
	if _, err := operator.CancelTask(context.Background(), task.TaskID); err != nil {
		t.Fatalf("cancel task after control restart: %v", err)
	}
	waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "cancelled"
	})
}

func TestRealCodingAgentSmoke(t *testing.T) {
	if os.Getenv("SYMMETRY_E2E_REAL_AGENT") != "1" {
		t.Skip("set SYMMETRY_E2E_REAL_AGENT=1 to run the configured Codex smoke test")
	}
	environment := loadControlEnvironment(t)
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", environment.enrollmentToken)
	agentPath := os.Getenv("SYMMETRY_E2E_REAL_AGENT_PATH")
	if agentPath == "" {
		t.Fatal("SYMMETRY_E2E_REAL_AGENT_PATH is required")
	}
	absoluteAgentPath, err := filepath.Abs(agentPath)
	if err != nil {
		t.Fatalf("resolve real-agent path: %v", err)
	}
	if _, err := os.Stat(absoluteAgentPath); err != nil {
		t.Fatalf("stat real-agent path: %v", err)
	}
	workspacePath := t.TempDir()
	command := exec.Command("git", "init", "--quiet", workspacePath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("initialize smoke workspace: %v: %s", err, output)
	}

	profile := unique("real-agent-profile")
	workspace := unique("real-agent-workspace")
	value := config.Config{
		ControlPlaneURL:   environment.baseURL,
		AllowInsecureHTTP: true,
		StateDir:          t.TempDir(),
		MachineName:       unique("real-agent-machine"),
		AgentProfiles: map[string]config.AgentProfile{
			profile: {
				Command: absoluteAgentPath,
				Args: []string{
					"exec", "--sandbox", "workspace-write", "--ephemeral", "--json", "-",
				},
				InputMode: config.InputModeGoal, EventFormat: config.EventFormatJSONL,
			},
		},
		Workspaces: map[string]config.Workspace{
			workspace: {Policy: config.WorkspacePolicyExistingCheckout, Path: workspacePath, Cleanup: config.CleanupNever},
		},
		Runtime: config.Runtime{
			RuntimeKey: unique("real-agent-runtime"), Name: "real-codex", Capacity: 1,
			AgentProfile: profile, Workspace: workspace,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, value, app.WithHTTPClient(localHTTPClient(30*time.Second)), app.WithLogWriter(io.Discard))
	}()
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, done)
	})

	operator := newOperator(t, environment)
	goal := "Use apply_patch to create symmetry-smoke.txt in the current working directory containing exactly symmetry-smoke followed by a newline. Do not run shell commands or perform extra verification. Do not modify any other files. Stop immediately after the file change."
	task, err := operator.SubmitTask(context.Background(), unique("real-agent-task"), protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal: goal, AgentProfile: profile, Workspace: workspace, Input: json.RawMessage(`{}`),
	}})
	if err != nil {
		t.Fatalf("submit real-agent task: %v", err)
	}
	waitForTask(t, operator, task.TaskID, 2*time.Minute, func(task protocol.Task) bool {
		return task.State == "completed"
	})
	contents, err := os.ReadFile(filepath.Join(workspacePath, "symmetry-smoke.txt"))
	if err != nil {
		t.Fatalf("read Codex output: %v", err)
	}
	if string(contents) != "symmetry-smoke\n" {
		t.Fatalf("symmetry-smoke.txt = %q", contents)
	}
}

type e2eEnvironment struct {
	baseURL         string
	agentPath       string
	enrollmentToken string
	operatorToken   string
}

type daemonRun struct {
	done      <-chan error
	profile   string
	workspace string
}

func loadEnvironment(t *testing.T) e2eEnvironment {
	t.Helper()
	value := loadControlEnvironment(t)
	value.agentPath = loadFakeAgentPath(t)
	return value
}

func loadControlEnvironment(t *testing.T) e2eEnvironment {
	t.Helper()
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Skip("set SYMMETRY_E2E=1 to run live control-plane tests")
	}
	value := e2eEnvironment{
		baseURL:         envOr("SYMMETRY_E2E_URL", "http://127.0.0.1:4000"),
		enrollmentToken: envOr("SYMMETRY_ENROLLMENT_TOKEN", enrollmentToken),
		operatorToken:   envOr("SYMMETRY_OPERATOR_TOKEN", operatorToken),
	}
	waitForControl(t, value.baseURL)
	return value
}

func loadFakeAgentPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("SYMMETRY_E2E_AGENT")
	if path == "" {
		t.Fatal("SYMMETRY_E2E_AGENT is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve fake-agent path: %v", err)
	}
	if _, err := os.Stat(absolute); err != nil {
		t.Fatalf("stat fake-agent path: %v", err)
	}
	return absolute
}

func startDaemon(t *testing.T, ctx context.Context, environment e2eEnvironment, name string, notifier app.NotificationClient) daemonRun {
	t.Helper()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", environment.enrollmentToken)
	profile := unique(name + "-profile")
	workspace := unique(name + "-workspace")
	value := daemonConfig(environment, name, t.TempDir(), t.TempDir(), profile, workspace)
	options := []app.Options{app.WithHTTPClient(localHTTPClient(30 * time.Second)), app.WithLogWriter(io.Discard)}
	if notifier != nil {
		options = append(options, app.WithNotificationClient(notifier))
	}
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, value, options...) }()
	return daemonRun{done: done, profile: profile, workspace: workspace}
}

func daemonConfig(environment e2eEnvironment, name, stateDir, workspacePath, profile, workspace string) config.Config {
	return config.Config{
		ControlPlaneURL:   environment.baseURL,
		AllowInsecureHTTP: true,
		StateDir:          stateDir,
		MachineName:       unique(name + "-machine"),
		AgentProfiles: map[string]config.AgentProfile{
			profile: {
				Command: environment.agentPath, InputMode: config.InputModeJSON,
				Interactive: true, EventFormat: config.EventFormatJSONL,
			},
		},
		Workspaces: map[string]config.Workspace{
			workspace: {Policy: config.WorkspacePolicyExistingCheckout, Path: workspacePath, Cleanup: config.CleanupNever},
		},
		Runtime: config.Runtime{
			RuntimeKey: unique(name + "-runtime"), Name: name, Capacity: 1,
			AgentProfile: profile, Workspace: workspace,
		},
	}
}

func newOperator(t *testing.T, environment e2eEnvironment) *control.OperatorClient {
	t.Helper()
	client, err := control.NewOperatorClient(environment.baseURL, environment.operatorToken, localHTTPClient(5*time.Second))
	if err != nil {
		t.Fatalf("new operator client: %v", err)
	}
	return client
}

func submit(t *testing.T, operator *control.OperatorClient, daemon daemonRun, name, mode string) protocol.Task {
	t.Helper()
	task, err := operator.SubmitTask(context.Background(), unique(name), protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal:         name,
		AgentProfile: daemon.profile,
		Workspace:    daemon.workspace,
		Input:        json.RawMessage(fmt.Sprintf(`{"mode":%q}`, mode)),
	}})
	if err != nil {
		t.Fatalf("submit %s task: %v", name, err)
	}
	return task
}

func waitForTask(t *testing.T, operator *control.OperatorClient, taskID string, timeout time.Duration, ready func(protocol.Task) bool) protocol.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last protocol.Task
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = operator.GetTask(context.Background(), taskID)
		if lastErr == nil && ready(last) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach expected state; last state=%q generation=%d error=%v", taskID, last.State, last.Generation, lastErr)
	return protocol.Task{}
}

func getTask(t *testing.T, operator *control.OperatorClient, taskID string) protocol.Task {
	t.Helper()
	task, err := operator.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get task %s: %v", taskID, err)
	}
	return task
}

func assertTaskRemainsQueued(t *testing.T, operator *control.OperatorClient, taskID string, window time.Duration) {
	t.Helper()
	deadline := time.Now().Add(window)
	for {
		task := getTask(t, operator, taskID)
		if task.State != "queued" {
			t.Fatalf("task %s state = %q, want queued while capacity is occupied", taskID, task.State)
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func registerRuntime(t *testing.T, client *control.Client, runtimeKey, profile, workspace string) protocol.RegisteredRuntime {
	t.Helper()
	response, err := client.RegisterSession(context.Background(), protocol.SessionRegistrationRequest{
		DaemonInstanceID: mustID(t),
		Runtimes: []protocol.RuntimeRegistration{{
			RuntimeKey: runtimeKey, Name: runtimeKey, Capacity: 1,
			AgentProfile: profile, Workspace: workspace, Capabilities: json.RawMessage(`{}`),
		}},
	})
	if err != nil {
		t.Fatalf("register runtime: %v", err)
	}
	if len(response.Runtimes) != 1 {
		t.Fatalf("registered runtimes = %d, want 1", len(response.Runtimes))
	}
	return response.Runtimes[0]
}

func waitForAssignment(t *testing.T, client *control.Client, runtime protocol.RegisteredRuntime, taskID string, timeout time.Duration) protocol.Assignment {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snapshot, err := client.Heartbeat(context.Background(), runtime.RuntimeID, protocol.RuntimeHeartbeatRequest{
			RuntimeEpoch: runtime.RuntimeEpoch,
			ActiveRuns:   []protocol.ActiveRun{},
		})
		if err != nil {
			t.Fatalf("heartbeat runtime: %v", err)
		}
		for _, assignment := range snapshot.Assignments {
			if assignment.TaskID == taskID {
				return assignment
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("task %s was not assigned to runtime %s", taskID, runtime.RuntimeID)
	return protocol.Assignment{}
}

func waitForControl(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if controlHealthy(baseURL) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("control plane at %s did not become healthy", baseURL)
}

func waitForControlRestart(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	downDeadline := time.Now().Add(timeout)
	for time.Now().Before(downDeadline) {
		if !controlHealthy(baseURL) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if time.Now().After(downDeadline) {
		t.Fatal("control plane did not stop for restart")
	}
	upDeadline := time.Now().Add(timeout)
	for time.Now().Before(upDeadline) {
		if controlHealthy(baseURL) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("control plane did not recover after restart")
}

func controlHealthy(baseURL string) bool {
	client := localHTTPClient(500 * time.Millisecond)
	response, err := client.Get(baseURL + "/healthz")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func localHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   timeout,
	}
}

func loadIdentity(t *testing.T, stateDir string) state.MachineIdentity {
	t.Helper()
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	defer store.Close()
	identity, err := store.LoadIdentity()
	if err != nil {
		t.Fatalf("load machine identity: %v", err)
	}
	return identity
}

func loadJournal(t *testing.T, stateDir string, key state.RunKey) state.RunJournal {
	t.Helper()
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("load journal %s/%d: %v", key.RunID, key.Generation, err)
	}
	return journal
}

func assertJournalMissing(t *testing.T, stateDir string, key state.RunKey) {
	t.Helper()
	store, err := state.New(stateDir)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	defer store.Close()
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("old journal %s/%d still exists or could not be read: %v", key.RunID, key.Generation, err)
	}
}

func waitForDaemon(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon stopped with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop within 10 seconds")
	}
}

func mustID(t *testing.T) string {
	t.Helper()
	value, err := state.NewDaemonInstanceID()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return value
}

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

type silentNotifications struct{}

func (silentNotifications) Run(ctx context.Context, _ chan<- notification.Hint) error {
	<-ctx.Done()
	return ctx.Err()
}
