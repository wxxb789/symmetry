package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	cancelTask(t, operator, slow.TaskID)
	waitForTask(t, operator, slow.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "cancelled"
	})
	waitForTask(t, operator, queued.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})

	waiting := submit(t, operator, daemon, "wait-input", "wait_twice")
	waitingSnapshot := waitForTask(t, operator, waiting.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "waiting_for_input" && task.Waiting != nil && task.Waiting.Question != nil && *task.Waiting.Question == "Confirm the requested input before continuing."
	})
	waitingRunID := taskRunID(t, waitingSnapshot)
	waitingGeneration := taskGeneration(t, waitingSnapshot)
	if _, err := operator.CreateTaskCommand(context.Background(), waiting.TaskID, unique("input"), protocol.TaskCommandRequest{
		Kind: "provide_input", Payload: json.RawMessage(`{"answer":"continue"}`),
	}); err != nil {
		t.Fatalf("submit input: %v", err)
	}
	waitForTask(t, operator, waiting.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed" && task.LatestCommand != nil && task.LatestCommand.State == "acknowledged" &&
			task.LatestCommand.AcknowledgementOutcome != nil && *task.LatestCommand.AcknowledgementOutcome == "applied"
	})
	assertWaitingInputHistory(t, environment, waiting.TaskID, waitingRunID, waitingGeneration)

	failed := submit(t, operator, daemon, "failure", "fail")
	waitForTask(t, operator, failed.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "failed"
	})
	afterFailure := submit(t, operator, daemon, "after-failure", "success")
	waitForTask(t, operator, afterFailure.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
}

func TestJSONValuesRoundTripThroughControl(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startDaemon(t, ctx, environment, "json-values", nil)
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	task := submit(t, operator, daemon, "json-values", "json_values")
	waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
	assertJSONValueEvents(t, environment, task.TaskID)
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

func TestTaskInputDistinguishesOmissionFromEmptyObject(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)
	t.Setenv("SYMMETRY_FAKE_AGENT_MODE", "inspect_input")
	profile := unique("input-presence-profile")
	workspace := unique("input-presence-workspace")
	value := daemonConfig(environment, "input-presence", t.TempDir(), t.TempDir(), profile, workspace)
	profileConfig := value.AgentProfiles[profile]
	profileConfig.EnvAllowlist = []string{"SYMMETRY_FAKE_AGENT_MODE"}
	value.AgentProfiles[profile] = profileConfig

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startConfiguredDaemon(t, ctx, environment, value, nil)
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	omitted := submitWithInput(t, operator, daemon, "input-omitted", nil)
	omittedCompleted := waitForTask(t, operator, omitted.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
	assertTaskInput(t, omittedCompleted, "null")
	assertInputInspectionEvent(t, environment, omitted.TaskID, "input:null")

	empty := submitWithInput(t, operator, daemon, "input-empty", json.RawMessage(`{}`))
	emptyCompleted := waitForTask(t, operator, empty.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
	assertTaskInput(t, emptyCompleted, "{}")
	assertInputInspectionEvent(t, environment, empty.TaskID, "input:object")
}

func TestGitWorktreeCleanupRemovesOwnedArtifacts(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)
	repository := initializeGitRepository(t)
	workspaceRoot := t.TempDir()
	stateDir := t.TempDir()
	profile := unique("worktree-profile")
	workspace := unique("worktree-workspace")
	value := daemonConfig(environment, "worktree-cleanup", stateDir, t.TempDir(), profile, workspace)
	value.Workspaces[workspace] = config.Workspace{
		Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: workspaceRoot,
		Ref: "HEAD", Cleanup: config.CleanupAlways,
	}

	ctx, cancel := context.WithCancel(context.Background())
	daemon := startConfiguredDaemon(t, ctx, environment, value, nil)
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	task := submit(t, operator, daemon, "worktree-cleanup", "success")
	completed := waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "completed"
	})
	key := state.RunKey{RunID: taskRunID(t, completed), Generation: taskGeneration(t, completed)}
	target := filepath.Join(workspaceRoot, "binding-"+workspace, "run-"+key.RunID, fmt.Sprintf("generation-%d", key.Generation))
	reservation := filepath.Join(workspaceRoot, ".symmetry-reservations", "binding-"+workspace, "run-"+key.RunID, fmt.Sprintf("generation-%d.json", key.Generation))
	ownershipJournal := filepath.Join(target, ".symmetry-workspace.json")

	waitForPathsAbsent(t, 20*time.Second, target, reservation, ownershipJournal)
	waitForJournalAbsentOnDisk(t, stateDir, key, 20*time.Second)
}

func TestTerminalTransitionRetriesAfterLeaseExpiry(t *testing.T) {
	environment := loadEnvironment(t)
	operator := newOperator(t, environment)
	proxy, outage := startTerminalTransitionProxy(t, environment.baseURL)
	proxied := environment
	proxied.baseURL = proxy.URL

	stateDir := t.TempDir()
	profile := unique("terminal-outage-profile")
	workspace := unique("terminal-outage-workspace")
	value := daemonConfig(proxied, "terminal-outage", stateDir, t.TempDir(), profile, workspace)
	ctx, cancel := context.WithCancel(context.Background())
	daemon := startConfiguredDaemon(t, ctx, proxied, value, silentNotifications{})
	t.Cleanup(func() {
		cancel()
		waitForDaemon(t, daemon.done)
	})

	releaseFile := filepath.Join(t.TempDir(), "release")
	input, err := json.Marshal(map[string]string{"mode": "exit_on_file", "path": releaseFile})
	if err != nil {
		t.Fatalf("marshal exit-on-file input: %v", err)
	}
	task := submitWithInput(t, operator, daemon, "terminal-outage", input)
	running := waitForTask(t, operator, task.TaskID, 15*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})
	key := state.RunKey{RunID: taskRunID(t, running), Generation: taskGeneration(t, running)}

	outage.beginOutage()
	waitForCondition(t, 10*time.Second, "forwarded daemon RPCs to drain", func() bool {
		return outage.activeForwarded.Load() == 0
	})
	if err := os.WriteFile(releaseFile, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release exit-on-file agent: %v", err)
	}
	pending := waitForJournalOnDisk(t, stateDir, key, 15*time.Second, func(journal state.RunJournal) bool {
		return journal.LocalState == "terminal_pending" && journal.TerminalState == "completed" && !journal.LeaseExpiresAt.IsZero()
	})
	waitForCondition(t, 10*time.Second, "an outbox delivery blocked by the outage proxy", func() bool {
		return outage.blockedOutboxDeliveries.Load() > 0
	})
	leaseExpiresAt := pending.LeaseExpiresAt
	if observed := outage.latestLeaseExpiry(); observed.After(leaseExpiresAt) {
		leaseExpiresAt = observed
	}
	if remaining := time.Until(leaseExpiresAt); remaining > 45*time.Second {
		t.Skipf("lease expires in %s; integration gate requires SYMMETRY_LEASE_DURATION_MS=30000", remaining.Round(time.Second))
	}
	waitForTime(t, leaseExpiresAt.Add(250*time.Millisecond), 45*time.Second)

	outage.allowDelivery()
	completed := waitForTask(t, operator, task.TaskID, 15*time.Second, func(task protocol.Task) bool {
		return task.State == "completed" && taskGeneration(t, task) == key.Generation
	})
	if !time.Now().Before(leaseExpiresAt.Add(8 * time.Minute)) {
		t.Fatalf("terminal transition completed after the terminal grace window")
	}
	if taskRunID(t, completed) != key.RunID {
		t.Fatalf("completed run id = %s, want %s", taskRunID(t, completed), key.RunID)
	}
	assertOutboxDeliveryOrder(t, outage, key.RunID)

	outage.endOutage()
	after := submit(t, operator, daemon, "terminal-outage-after", "success")
	waitForTask(t, operator, after.TaskID, 15*time.Second, func(task protocol.Task) bool {
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
	machineToken := unique("fence-machine-token")
	machine, err := enrollment.Enroll(context.Background(), environment.enrollmentToken, unique("fence-enrollment"), protocol.EnrollRequest{
		Machine:      protocol.MachineEnrollment{Name: unique("fence-machine")},
		MachineToken: machineToken,
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
	first := registerRuntime(t, machineClient, machine.MachineID, runtimeKey, profile, workspace)
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

	second := registerRuntime(t, machineClient, machine.MachineID, runtimeKey, profile, workspace)
	if second.RuntimeEpoch <= first.RuntimeEpoch {
		t.Fatalf("runtime epoch = %d, want greater than %d", second.RuntimeEpoch, first.RuntimeEpoch)
	}
	staleFence := protocol.Fence{
		RuntimeID: first.RuntimeID, RuntimeEpoch: first.RuntimeEpoch, Generation: claim.Generation,
		ClaimID: claim.ClaimID, LeaseToken: claim.LeaseToken,
	}
	err = machineClient.Transition(context.Background(), assignment.RunID, protocol.StateTransitionRequest{
		Fence: staleFence, TransitionID: mustID(t), State: "running", Payload: json.RawMessage(`{}`),
	})
	if !control.IsOwnershipLost(err) {
		t.Fatalf("stale transition error = %v, want ownership_lost", err)
	}

	newAssignment := waitForAssignment(t, machineClient, second, task.TaskID, 60*time.Second)
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
		return task.State == "completed" && taskGeneration(t, task) == newAssignment.Generation
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
	firstRunID := taskRunID(t, firstRun)
	firstGeneration := taskGeneration(t, firstRun)

	stopFirst()
	waitForDaemon(t, firstDone)
	firstStopped = true
	identityBefore := loadIdentity(t, stateDir)
	journalBefore := loadJournal(t, stateDir, state.RunKey{RunID: firstRunID, Generation: firstGeneration})
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
		return task.State == "running" && taskGeneration(t, task) > firstGeneration
	})
	secondGeneration := taskGeneration(t, secondRun)
	if secondGeneration <= firstGeneration {
		t.Fatalf("reclaimed generation = %d, want greater than %d", secondGeneration, firstGeneration)
	}

	cancelTask(t, operator, task.TaskID)
	waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "cancelled" && taskGeneration(t, task) == secondGeneration
	})

	stopSecond()
	waitForDaemon(t, secondDone)
	secondStopped = true
	assertJournalMissing(t, stateDir, state.RunKey{RunID: firstRunID, Generation: firstGeneration})
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
	runningGeneration := taskGeneration(t, running)
	if err := os.WriteFile(marker, []byte(task.TaskID+"\n"), 0o600); err != nil {
		t.Fatalf("write restart marker: %v", err)
	}
	waitForControlRestart(t, environment.baseURL, 90*time.Second)
	restored := waitForTask(t, operator, task.TaskID, 20*time.Second, func(task protocol.Task) bool {
		return task.State == "running"
	})
	restoredGeneration := taskGeneration(t, restored)
	if restoredGeneration != runningGeneration {
		t.Fatalf("task generation changed across control restart: %d -> %d", runningGeneration, restoredGeneration)
	}
	cancelTask(t, operator, task.TaskID)
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
	profile := unique(name + "-profile")
	workspace := unique(name + "-workspace")
	value := daemonConfig(environment, name, t.TempDir(), t.TempDir(), profile, workspace)
	return startConfiguredDaemon(t, ctx, environment, value, notifier)
}

func startConfiguredDaemon(t *testing.T, ctx context.Context, environment e2eEnvironment, value config.Config, notifier app.NotificationClient) daemonRun {
	t.Helper()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", environment.enrollmentToken)
	options := []app.Options{app.WithHTTPClient(localHTTPClient(30 * time.Second)), app.WithLogWriter(io.Discard)}
	if notifier != nil {
		options = append(options, app.WithNotificationClient(notifier))
	}
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, value, options...) }()
	return daemonRun{done: done, profile: value.Runtime.AgentProfile, workspace: value.Runtime.Workspace}
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
	return submitWithInput(t, operator, daemon, name, json.RawMessage(fmt.Sprintf(`{"mode":%q}`, mode)))
}

func submitWithInput(t *testing.T, operator *control.OperatorClient, daemon daemonRun, name string, input json.RawMessage) protocol.Task {
	t.Helper()
	task, err := operator.SubmitTask(context.Background(), unique(name), protocol.TaskSubmitRequest{Work: protocol.Work{
		Goal:         name,
		AgentProfile: daemon.profile,
		Workspace:    daemon.workspace,
		Input:        input,
	}})
	if err != nil {
		t.Fatalf("submit %s task: %v", name, err)
	}
	return task
}

func assertTaskInput(t *testing.T, task protocol.Task, want string) {
	t.Helper()
	if task.Work == nil {
		t.Fatalf("task %s has no work", task.TaskID)
	}
	if got := strings.TrimSpace(string(task.Work.Input)); got != want {
		t.Fatalf("task %s work.input = %s, want %s", task.TaskID, got, want)
	}
}

func assertInputInspectionEvent(t *testing.T, environment e2eEnvironment, taskID, wantMessage string) {
	t.Helper()
	for _, event := range collectHistory(t, environment, taskID, "events") {
		if historyString(t, event, "kind") == "progress" && historyPayloadString(t, event, "message") == wantMessage {
			return
		}
	}
	t.Fatalf("task %s has no input inspection event %q", taskID, wantMessage)
}

func assertJSONValueEvents(t *testing.T, environment e2eEnvironment, taskID string) {
	t.Helper()
	events := filterHistory(collectHistory(t, environment, taskID, "events"), func(entry map[string]json.RawMessage) bool {
		return historyString(t, entry, "kind") == "agent_event"
	})
	if len(events) != 3 {
		t.Fatalf("agent events = %d, want 3", len(events))
	}

	want := []string{"42", `["progress"]`, "9007199254740993"}
	for index, event := range events {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(historyField(t, event, "payload"), &payload); err != nil {
			t.Fatalf("decode agent event payload %d: %v", index, err)
		}
		if got := string(payload["value"]); got != want[index] {
			t.Fatalf("agent event %d value = %s, want %s", index, got, want[index])
		}
	}
}

func assertWaitingInputHistory(t *testing.T, environment e2eEnvironment, taskID, runID string, generation int64) {
	t.Helper()
	events := collectHistory(t, environment, taskID, "events")
	waitingEvents := filterHistory(events, func(entry map[string]json.RawMessage) bool {
		return historyString(t, entry, "kind") == "waiting_for_input"
	})
	if len(waitingEvents) != 2 {
		t.Fatalf("waiting events = %d, want 2", len(waitingEvents))
	}
	questions := make(map[string]bool, len(waitingEvents))
	for _, event := range waitingEvents {
		questions[historyPayloadString(t, event, "question")] = true
		assertHistoryRunOwnership(t, event, runID, generation)
	}
	for _, wantQuestion := range []string{"Provide the first requested input.", "Confirm the requested input before continuing."} {
		if !questions[wantQuestion] {
			t.Fatalf("waiting questions = %#v, missing %q", questions, wantQuestion)
		}
	}

	transitions := collectHistory(t, environment, taskID, "transitions")
	waitingTransitions := filterHistory(transitions, func(entry map[string]json.RawMessage) bool {
		return historyString(t, entry, "state") == "waiting_for_input"
	})
	if len(waitingTransitions) != 1 {
		t.Fatalf("waiting transitions = %d, want 1", len(waitingTransitions))
	}
	assertHistoryRunOwnership(t, waitingTransitions[0], runID, generation)

	commands := collectHistory(t, environment, taskID, "commands")
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	command := commands[0]
	if got := historyString(t, command, "kind"); got != "provide_input" {
		t.Fatalf("command kind = %q, want provide_input", got)
	}
	assertHistoryRunOwnership(t, command, runID, generation)
	if got := historyString(t, command, "state"); got != "acknowledged" {
		t.Fatalf("command state = %q, want acknowledged", got)
	}
	if got := historyNullableString(t, command, "acknowledgement_outcome"); got != "applied" {
		t.Fatalf("command acknowledgement outcome = %q, want applied", got)
	}
	if historyNullableString(t, command, "acknowledgement_id") == "" || historyNullableString(t, command, "acknowledged_at") == "" {
		t.Fatal("command acknowledgement identity or time is missing")
	}

	assertPagedOrderingWithoutDuplicates(t, environment, taskID, "events")
	assertPagedOrderingWithoutDuplicates(t, environment, taskID, "transitions")
	assertPagedOrderingWithoutDuplicates(t, environment, taskID, "timeline")
}

func collectHistory(t *testing.T, environment e2eEnvironment, taskID, surface string) []map[string]json.RawMessage {
	t.Helper()
	entries := make([]map[string]json.RawMessage, 0)
	cursor := ""
	for {
		page, next := historyPage(t, environment, taskID, surface, cursor, 100)
		entries = append(entries, page...)
		if next == "" {
			return entries
		}
		cursor = next
	}
}

func assertPagedOrderingWithoutDuplicates(t *testing.T, environment e2eEnvironment, taskID, surface string) {
	t.Helper()
	baseline := collectHistory(t, environment, taskID, surface)
	if len(baseline) < 2 {
		t.Fatalf("%s baseline has %d entries, want at least 2 for cursor traversal", surface, len(baseline))
	}
	want := make(map[string]struct{}, len(baseline))
	for _, entry := range baseline {
		identity := historyIdentity(t, surface, entry)
		if _, duplicate := want[identity]; duplicate {
			t.Fatalf("%s baseline contains duplicate entry %q", surface, identity)
		}
		want[identity] = struct{}{}
	}
	seen := make(map[string]struct{})
	cursor := ""
	var previous time.Time
	for {
		page, next := historyPage(t, environment, taskID, surface, cursor, 1)
		if len(page) != 1 {
			t.Fatalf("%s page with cursor %q has %d entries, want 1", surface, cursor, len(page))
		}
		entry := page[0]
		identity := historyIdentity(t, surface, entry)
		if _, duplicate := seen[identity]; duplicate {
			t.Fatalf("%s repeated entry %q across cursor pages", surface, identity)
		}
		seen[identity] = struct{}{}
		recordedAt := historyTime(t, entry, "recorded_at")
		if !previous.IsZero() {
			if surface == "timeline" && recordedAt.After(previous) {
				t.Fatalf("timeline is not newest-first: %s after %s", recordedAt, previous)
			}
			if surface != "timeline" && recordedAt.Before(previous) {
				t.Fatalf("%s is not oldest-first: %s before %s", surface, recordedAt, previous)
			}
		}
		previous = recordedAt
		if next == "" {
			if len(seen) != len(want) {
				t.Fatalf("%s cursor traversal has %d entries, baseline has %d", surface, len(seen), len(want))
			}
			for identity := range want {
				if _, found := seen[identity]; !found {
					t.Fatalf("%s cursor traversal omitted baseline entry %q", surface, identity)
				}
			}
			return
		}
		cursor = next
	}
}

func historyPage(t *testing.T, environment e2eEnvironment, taskID, surface, cursor string, limit int) ([]map[string]json.RawMessage, string) {
	t.Helper()
	query := url.Values{"limit": []string{fmt.Sprintf("%d", limit)}}
	if cursor != "" {
		if surface == "timeline" {
			query.Set("before", cursor)
		} else {
			query.Set("after", cursor)
		}
	}
	document := operatorGetJSON(t, environment, "v1/tasks/"+taskID+"/"+surface, query)
	entryKey, cursorKey := surface, "next_after"
	if surface == "timeline" {
		entryKey, cursorKey = "items", "next_before"
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(historyField(t, document, entryKey), &entries); err != nil {
		t.Fatalf("decode %s entries: %v", surface, err)
	}
	var next string
	rawNext := historyField(t, document, cursorKey)
	if string(rawNext) != "null" {
		if err := json.Unmarshal(rawNext, &next); err != nil {
			t.Fatalf("decode %s cursor: %v", surface, err)
		}
	}
	return entries, next
}

func operatorGetJSON(t *testing.T, environment e2eEnvironment, endpoint string, query url.Values) map[string]json.RawMessage {
	t.Helper()
	requestURL := operatorURL(t, environment.baseURL, endpoint, query)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create operator history request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+environment.operatorToken)
	response, err := localHTTPClient(5 * time.Second).Do(request)
	if err != nil {
		t.Fatalf("get %s: %v", endpoint, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", endpoint, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get %s = HTTP %d: %s", endpoint, response.StatusCode, body)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("decode %s response: %v", endpoint, err)
	}
	return document
}

func operatorURL(t *testing.T, baseURL, endpoint string, query url.Values) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse operator base URL: %v", err)
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if basePath == "" {
		basePath = "/api"
	}
	parsed.Path = basePath + "/" + strings.TrimLeft(endpoint, "/")
	parsed.RawPath = ""
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func historyField(t *testing.T, document map[string]json.RawMessage, field string) json.RawMessage {
	t.Helper()
	value, ok := document[field]
	if !ok {
		t.Fatalf("history response has no %q field", field)
	}
	return value
}

func historyString(t *testing.T, entry map[string]json.RawMessage, field string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(historyField(t, entry, field), &value); err != nil {
		t.Fatalf("decode history %s: %v", field, err)
	}
	return value
}

func historyNullableString(t *testing.T, entry map[string]json.RawMessage, field string) string {
	t.Helper()
	value := historyField(t, entry, field)
	if string(value) == "null" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		t.Fatalf("decode history %s: %v", field, err)
	}
	return decoded
}

func historyPayloadString(t *testing.T, entry map[string]json.RawMessage, field string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(historyField(t, entry, "payload"), &payload); err != nil {
		t.Fatalf("decode history payload: %v", err)
	}
	return historyString(t, payload, field)
}

func historyTime(t *testing.T, entry map[string]json.RawMessage, field string) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339Nano, historyString(t, entry, field))
	if err != nil {
		t.Fatalf("parse history %s: %v", field, err)
	}
	return value
}

func historyIdentity(t *testing.T, surface string, entry map[string]json.RawMessage) string {
	t.Helper()
	if surface != "timeline" {
		switch surface {
		case "events":
			return historyString(t, entry, "event_id")
		case "transitions":
			return historyString(t, entry, "transition_id")
		case "commands":
			return historyString(t, entry, "command_id")
		}
	}
	data := make(map[string]json.RawMessage)
	if err := json.Unmarshal(historyField(t, entry, "data"), &data); err != nil {
		t.Fatalf("decode timeline data: %v", err)
	}
	source := historyString(t, entry, "source")
	for _, field := range []string{"event_id", "transition_id", "command_id"} {
		if _, ok := data[field]; ok {
			return source + ":" + historyString(t, data, field)
		}
	}
	t.Fatalf("timeline %s item has no source identifier", source)
	return ""
}

func assertHistoryRunOwnership(t *testing.T, entry map[string]json.RawMessage, runID string, generation int64) {
	t.Helper()
	if got := historyString(t, entry, "run_id"); got != runID {
		t.Fatalf("history run_id = %q, want %q", got, runID)
	}
	var gotGeneration int64
	if err := json.Unmarshal(historyField(t, entry, "generation"), &gotGeneration); err != nil {
		t.Fatalf("decode history generation: %v", err)
	}
	if gotGeneration != generation {
		t.Fatalf("history generation = %d, want %d", gotGeneration, generation)
	}
}

func filterHistory(entries []map[string]json.RawMessage, keep func(map[string]json.RawMessage) bool) []map[string]json.RawMessage {
	result := make([]map[string]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		if keep(entry) {
			result = append(result, entry)
		}
	}
	return result
}

func initializeGitRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet")
	runGit(t, repository, "config", "user.email", "symmetry-e2e@example.test")
	runGit(t, repository, "config", "user.name", "Symmetry E2E")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("# e2e\n"), 0o600); err != nil {
		t.Fatalf("write worktree source: %v", err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "--quiet", "-m", "initial e2e workspace")
	return repository
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
}

func waitForPathsAbsent(t *testing.T, timeout time.Duration, paths ...string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		missing := true
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				missing = false
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("inspect cleanup path %q: %v", path, err)
			}
		}
		if missing {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("cleanup paths remain after %s: %s", timeout, strings.Join(paths, ", "))
}

func waitForJournalOnDisk(t *testing.T, stateDir string, key state.RunKey, timeout time.Duration, ready func(state.RunJournal) bool) state.RunJournal {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last state.RunJournal
	var lastErr error
	for time.Now().Before(deadline) {
		journal, found, err := journalOnDisk(stateDir, key)
		if err == nil && found {
			last = journal
			if ready(journal) {
				return journal
			}
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("journal %s/%d did not reach expected state: last=%#v error=%v", key.RunID, key.Generation, last, lastErr)
	return state.RunJournal{}
}

func waitForJournalAbsentOnDisk(t *testing.T, stateDir string, key state.RunKey, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, found, err := journalOnDisk(stateDir, key)
		if err != nil {
			t.Fatalf("read journal %s/%d: %v", key.RunID, key.Generation, err)
		}
		if !found {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("journal %s/%d remains after %s", key.RunID, key.Generation, timeout)
}

func journalOnDisk(stateDir string, key state.RunKey) (state.RunJournal, bool, error) {
	entries, err := os.ReadDir(filepath.Join(stateDir, "runs"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state.RunJournal{}, false, nil
		}
		return state.RunJournal{}, false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "journal-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(stateDir, "runs", entry.Name()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return state.RunJournal{}, false, err
		}
		var journal state.RunJournal
		if err := json.Unmarshal(contents, &journal); err != nil {
			return state.RunJournal{}, false, err
		}
		if journal.Key() == key {
			return journal, true, nil
		}
	}
	return state.RunJournal{}, false, nil
}

func waitForTime(t *testing.T, target time.Time, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !time.Now().Before(target) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("time %s did not arrive within %s", target, timeout)
}

func waitForCondition(t *testing.T, timeout time.Duration, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

type terminalOutageProxy struct {
	mu                      sync.RWMutex
	leaseMu                 sync.Mutex
	forwardedMu             sync.Mutex
	outage                  bool
	deliveryAllowed         bool
	blockedOutboxDeliveries atomic.Int64
	activeForwarded         atomic.Int64
	forwardedOutbox         []string
	leaseExpiresAt          time.Time
}

func (proxy *terminalOutageProxy) beginOutage() {
	proxy.mu.Lock()
	proxy.outage = true
	proxy.mu.Unlock()
}

func (proxy *terminalOutageProxy) allowDelivery() {
	proxy.mu.Lock()
	proxy.deliveryAllowed = true
	proxy.mu.Unlock()
}

func (proxy *terminalOutageProxy) endOutage() {
	proxy.mu.Lock()
	proxy.outage = false
	proxy.deliveryAllowed = false
	proxy.mu.Unlock()
}

func (proxy *terminalOutageProxy) forwardedOutboxDeliveries() []string {
	proxy.forwardedMu.Lock()
	defer proxy.forwardedMu.Unlock()
	return append([]string(nil), proxy.forwardedOutbox...)
}

func (proxy *terminalOutageProxy) latestLeaseExpiry() time.Time {
	proxy.leaseMu.Lock()
	defer proxy.leaseMu.Unlock()
	return proxy.leaseExpiresAt
}

func startTerminalTransitionProxy(t *testing.T, baseURL string) (*httptest.Server, *terminalOutageProxy) {
	t.Helper()
	upstream, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse proxy upstream: %v", err)
	}
	upstream.Path = ""
	upstream.RawPath = ""
	upstream.RawQuery = ""
	state := &terminalOutageProxy{}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, err error) {
		http.Error(response, "proxy upstream error: "+err.Error(), http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.StatusCode != http.StatusOK || response.Request.Method != http.MethodPatch || !strings.HasSuffix(response.Request.URL.Path, "/lease") {
			return nil
		}
		contents, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		response.Body = io.NopCloser(strings.NewReader(string(contents)))
		var value struct {
			LeaseExpiresAt time.Time `json:"lease_expires_at"`
		}
		if err := json.Unmarshal(contents, &value); err != nil {
			return err
		}
		state.leaseMu.Lock()
		if value.LeaseExpiresAt.After(state.leaseExpiresAt) {
			state.leaseExpiresAt = value.LeaseExpiresAt
		}
		state.leaseMu.Unlock()
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		outboxDelivery := isOutboxDelivery(request)
		state.mu.RLock()
		outage := state.outage
		deliveryAllowed := state.deliveryAllowed
		if outage && !(outboxDelivery && deliveryAllowed) {
			state.mu.RUnlock()
			if outboxDelivery {
				state.blockedOutboxDeliveries.Add(1)
			}
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"error":{"code":"service_unavailable","message":"daemon control RPC temporarily unavailable"}}`))
			return
		}
		state.activeForwarded.Add(1)
		if outage && outboxDelivery && deliveryAllowed {
			state.forwardedMu.Lock()
			state.forwardedOutbox = append(state.forwardedOutbox, request.Method+" "+request.URL.Path)
			state.forwardedMu.Unlock()
		}
		state.mu.RUnlock()
		defer state.activeForwarded.Add(-1)
		proxy.ServeHTTP(response, request)
	}))
	t.Cleanup(server.Close)
	return server, state
}

func isOutboxDelivery(request *http.Request) bool {
	path := request.URL.Path
	return (request.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/runs/") && strings.HasSuffix(path, "/events")) ||
		(request.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/runs/") && strings.Contains(path, "/transitions/")) ||
		(request.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/commands/") && strings.Contains(path, "/acknowledgements/"))
}

func assertOutboxDeliveryOrder(t *testing.T, proxy *terminalOutageProxy, runID string) {
	t.Helper()
	eventRequest := "POST /api/v1/runs/" + runID + "/events"
	transitionPrefix := "PUT /api/v1/runs/" + runID + "/transitions/"
	eventIndex := -1
	transitionIndex := -1
	for index, request := range proxy.forwardedOutboxDeliveries() {
		if request == eventRequest && eventIndex == -1 {
			eventIndex = index
		}
		if strings.HasPrefix(request, transitionPrefix) && transitionIndex == -1 {
			transitionIndex = index
		}
	}
	if eventIndex == -1 || transitionIndex == -1 {
		t.Fatalf("allowed outbox deliveries = %v, want %q before %q", proxy.forwardedOutboxDeliveries(), eventRequest, transitionPrefix+"...")
	}
	if eventIndex >= transitionIndex {
		t.Fatalf("allowed outbox delivery order = %v, want %q before %q", proxy.forwardedOutboxDeliveries(), eventRequest, transitionPrefix+"...")
	}
}

func cancelTask(t *testing.T, operator *control.OperatorClient, taskID string) {
	t.Helper()
	if _, err := operator.CreateTaskCommand(context.Background(), taskID, unique("cancel"), protocol.TaskCommandRequest{Kind: "cancel"}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
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
	t.Fatalf("task %s did not reach expected state; last state=%q run_id=%s generation=%s error=%v", taskID, last.State, optionalTaskRunID(last), optionalTaskGeneration(last), lastErr)
	return protocol.Task{}
}

func taskRunID(t *testing.T, task protocol.Task) string {
	t.Helper()
	if task.RunID == nil || *task.RunID == "" {
		t.Fatalf("task %s has no run_id", task.TaskID)
	}
	return *task.RunID
}

func taskGeneration(t *testing.T, task protocol.Task) int64 {
	t.Helper()
	if task.Generation == nil {
		t.Fatalf("task %s has no generation", task.TaskID)
	}
	return *task.Generation
}

func optionalTaskRunID(task protocol.Task) string {
	if task.RunID == nil {
		return "<nil>"
	}
	return *task.RunID
}

func optionalTaskGeneration(task protocol.Task) string {
	if task.Generation == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *task.Generation)
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

func registerRuntime(t *testing.T, client *control.Client, machineID, runtimeKey, profile, workspace string) protocol.RegisteredRuntime {
	t.Helper()
	response, err := client.RegisterSession(context.Background(), machineID, mustID(t), protocol.SessionRegistrationRequest{
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
