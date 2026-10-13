//go:build linux && amd64

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

type linuxV2OwnerWitnessCommand struct {
	process      *productionDaemonWitnessProcess
	pidfd        *os.File
	identity     string
	responsePath string
	exitErr      error
}

func startLinuxV2OwnerWitnessCommand(t *testing.T, binary string, fixture linuxV2NativeFixture) *linuxV2OwnerWitnessCommand {
	return startLinuxV2OwnerWitnessCommandWithRequestPath(t, binary, fixture, fixture.requestPath)
}

func startLinuxV2OwnerWitnessCommandWithRequestPath(
	t *testing.T,
	binary string,
	fixture linuxV2NativeFixture,
	requestPath string,
) *linuxV2OwnerWitnessCommand {
	t.Helper()
	stdout, err := os.OpenFile(fixture.responsePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open private owner-witness response file: %v", err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open owner-witness diagnostic sink: %v", err)
	}
	command := exec.Command(binary,
		"-linux-v2-native-owner-witness", "-config", fixture.configPath, "-request", requestPath,
	)
	command.Dir = fixture.artifactDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("configure hidden production owner-witness command: %v", err)
	}
	command.Env = linuxV2OwnerWitnessEnvironment(fixture.profile.EnvAllowlist, fixture.markerDir)
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start hidden production owner-witness command: %v", err)
	}
	identity, err := platform.ProcessIdentity(command.Process.Pid)
	if err != nil {
		process := &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
		process.stop(t)
		t.Fatalf("read hidden owner-witness process identity: %v", err)
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		process := &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
		process.stop(t)
		t.Fatalf("pin hidden owner-witness process pidfd: %v", err)
	}
	if fdPID, fdErr := linuxV2PIDFDInfoPID(pidfd); fdErr != nil || fdPID != command.Process.Pid {
		_ = unix.Close(pidfd)
		process := &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
		process.stop(t)
		t.Fatal("hidden owner-witness pidfd did not bind its exact process PID")
	}
	process := &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
	result := &linuxV2OwnerWitnessCommand{
		process: process, pidfd: os.NewFile(uintptr(pidfd), "linux-v2-owner-witness-command-pidfd"),
		identity: identity, responsePath: fixture.responsePath,
	}
	if result.pidfd == nil {
		_ = unix.Close(pidfd)
		process.stop(t)
		t.Fatal("wrap hidden owner-witness command pidfd")
	}
	t.Cleanup(func() {
		if !process.waited {
			process.stop(t)
		}
		_ = result.pidfd.Close()
	})
	return result
}

func linuxV2OwnerWitnessEnvironment(allowlist []string, markerDir string) []string {
	allowed := map[string]struct{}{
		"SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS": {},
		linuxV2NativeHookMarkerChildEnvironment:       {},
	}
	for _, name := range allowlist {
		if linuxV2IsCredentialEnvironmentName(name) {
			continue
		}
		allowed[name] = struct{}{}
	}
	environment := make([]string, 0, len(allowed)+len(linuxV2SafeHostEnvironment()))
	for _, entry := range linuxV2SafeHostEnvironment() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, present := allowed[key]; present && key != linuxV2NativeHookMarkerChildEnvironment && key != "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS" {
				environment = append(environment, entry)
			}
		}
	}
	for _, name := range allowlist {
		key := strings.TrimSpace(name)
		if key == linuxV2NativeHookMarkerChildEnvironment || linuxV2IsCredentialEnvironmentName(key) {
			continue
		}
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, key+"=") {
				environment = append(environment, entry)
				break
			}
		}
	}
	return append(environment,
		"SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1",
		linuxV2NativeHookMarkerChildEnvironment+"="+markerDir,
	)
}

func waitLinuxV2OwnerWitnessCommand(command *linuxV2OwnerWitnessCommand, timeout time.Duration, allowNonzero bool) error {
	if command == nil || command.process == nil || command.pidfd == nil {
		return errors.New("owner-witness command process is unavailable")
	}
	if command.process.waited {
		return nil
	}
	if err := linuxV2WaitPIDFDExited(int(command.pidfd.Fd()), timeout); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.process.command.Wait() }()
	select {
	case err := <-done:
		command.process.waited = true
		command.exitErr = err
		stdoutErr := command.process.stdout.Close()
		stderrErr := command.process.stderr.Close()
		if !allowNonzero && err != nil {
			return fmt.Errorf("hidden owner-witness command exited unsuccessfully: %w", err)
		}
		return errors.Join(stdoutErr, stderrErr)
	case <-time.After(15 * time.Second):
		return errors.New("hidden owner-witness command did not exit after its pidfd signaled")
	}
}

func requireLinuxV2RecoveryExitStatus(t *testing.T, command *linuxV2OwnerWitnessCommand, outcome string) {
	t.Helper()
	if command == nil {
		t.Fatal("recovery command is unavailable for exit-status validation")
	}
	if outcome == "unresolved" {
		var exitErr *exec.ExitError
		if !errors.As(command.exitErr, &exitErr) || exitErr.ExitCode() <= 0 {
			t.Fatalf("unresolved production recovery exit = %v, want deliberate nonzero process exit", command.exitErr)
		}
		return
	}
	if command.exitErr != nil {
		t.Fatalf("%s production recovery exit = %v, want exit 0", outcome, command.exitErr)
	}
}

func linuxV2KillOwnerWitnessCommand(command *linuxV2OwnerWitnessCommand) error {
	if command == nil || command.process == nil || command.pidfd == nil {
		return errors.New("exact owner-witness process is unavailable for SIGKILL")
	}
	if err := unix.PidfdSendSignal(int(command.pidfd.Fd()), unix.SIGKILL, nil, 0); err != nil {
		return fmt.Errorf("SIGKILL exact owner-witness process by pidfd: %w", err)
	}
	if err := waitLinuxV2OwnerWitnessCommand(command, 15*time.Second, true); err != nil {
		return fmt.Errorf("wait for exact owner-witness SIGKILL: %w", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(command.exitErr, &exitErr) {
		return errors.New("owner-witness command did not report the requested signal exit")
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		return errors.New("owner-witness command did not terminate by the exact SIGKILL")
	}
	return nil
}

func linuxV2ReadOwnerWitnessResponse(path string, request linuxV2OwnerWitnessRequest, expectedFailure bool) (linuxV2OwnerWitnessResponse, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return linuxV2OwnerWitnessResponse{}, errors.New("read redacted production owner-witness response")
	}
	var response linuxV2OwnerWitnessResponse
	if err := json.Unmarshal(contents, &response); err != nil {
		return linuxV2OwnerWitnessResponse{}, errors.New("decode redacted production owner-witness response")
	}
	if response.Version != 1 || response.Case != request.Case || response.Harness != request.Harness ||
		strings.TrimSpace(response.NativeVersion) == "" || !response.InstalledOwnerVerified ||
		response.RunKey.RunID != request.RunKey.RunID || response.RunKey.Generation != request.RunKey.Generation ||
		response.Fence.RuntimeID != request.Fence.RuntimeID || response.Fence.RuntimeEpoch != request.Fence.RuntimeEpoch ||
		response.Fence.Generation != request.Fence.Generation || response.Fence.ClaimID != request.Fence.ClaimID ||
		response.Subject != request.Subject {
		return linuxV2OwnerWitnessResponse{}, errors.New("production owner-witness response changed the exact RunKey/fence/Subject")
	}
	if !expectedFailure && strings.TrimSpace(response.Outcome) == "" {
		return linuxV2OwnerWitnessResponse{}, errors.New("production owner-witness response omitted its outcome")
	}
	if request.Claim.ProviderAccess != nil && strings.Contains(string(contents), request.Claim.ProviderAccess.Token) {
		return linuxV2OwnerWitnessResponse{}, errors.New("redacted owner-witness response contains raw provider access")
	}
	if request.Claim.LeaseToken != "" && strings.Contains(string(contents), request.Claim.LeaseToken) {
		return linuxV2OwnerWitnessResponse{}, errors.New("redacted owner-witness response contains the raw lease token")
	}
	stageReplayUnresolved := request.Case == "stage_replay" && response.Outcome == "unresolved" && response.FailureCategory == "containment_unproven"
	helperResponseLost := request.Case == "stage_replay" && response.FailureCategory == "response_lost" &&
		response.HelperDropObserved && response.ResponseLostAfter == response.HelperDropOperation &&
		(response.HelperDropOperation == "stop" || response.HelperDropOperation == "ack-stop-receipt" || response.HelperDropOperation == "launcher-ready") &&
		(response.Outcome == "unresolved" || response.Outcome == "completed")
	stageReplayAckLost := request.Case == "stage_replay" && response.Outcome == "unresolved" &&
		request.DropAckAfter != "" && response.FailureCategory == "response_lost" &&
		response.ResponseLostAfter == request.DropAckAfter
	if response.Outcome != "peer_denied" && !stageReplayUnresolved && !stageReplayAckLost && !helperResponseLost && response.FailureCategory != "" {
		return linuxV2OwnerWitnessResponse{}, errors.New("positive owner-witness response contains a peer failure category")
	}
	return response, nil
}

func linuxV2NativeWitnessOutputPath(t *testing.T, fixture linuxV2NativeFixture) string {
	t.Helper()
	name := strings.NewReplacer(string(filepath.Separator), "_").Replace(t.Name())
	if output := strings.TrimSpace(os.Getenv(linuxV2NativeWitnessOutputEnvironment)); output != "" {
		if !filepath.IsAbs(output) || filepath.Clean(output) != output {
			t.Fatalf("%s must be a canonical absolute path", linuxV2NativeWitnessOutputEnvironment)
		}
		if err := os.MkdirAll(output, 0o700); err != nil {
			t.Fatalf("create redacted v2 witness output directory: %v", err)
		}
		if err := os.Chmod(output, 0o700); err != nil {
			t.Fatalf("secure redacted v2 witness output directory: %v", err)
		}
		return filepath.Join(output, name+".json")
	}
	return filepath.Join(fixture.artifactDir, "witness.json")
}

func waitLinuxV2OwnerWitnessStage(request linuxV2OwnerWitnessRequest, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	path := filepath.Join(request.Barrier.Directory, request.Barrier.HoldAfter+".json")
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			var marker struct {
				Version   int                  `json:"version"`
				Case      string               `json:"case"`
				Stage     string               `json:"stage"`
				HoldAfter string               `json:"hold_after"`
				RunKey    linuxV2WitnessRunKey `json:"run_key"`
			}
			if json.Unmarshal(contents, &marker) == nil && marker.Version == 1 && marker.Case == request.Case &&
				marker.Stage == request.Barrier.HoldAfter && marker.HoldAfter == request.Barrier.HoldAfter && marker.RunKey == request.RunKey {
				return nil
			}
			return errors.New("owner-witness barrier marker does not match its exact request RunKey/stage")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("read owner-witness barrier marker")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("owner-witness command did not reach its requested durable stage")
}

func linuxV2SubjectSnapshotFromRequest(request linuxV2OwnerWitnessRequest) *linuxV2SubjectSnapshot {
	hash, err := request.Subject.Hash()
	if err != nil {
		return nil
	}
	return &linuxV2SubjectSnapshot{
		RunID: request.RunKey.RunID, Generation: request.RunKey.Generation,
		ResourceID: request.Subject.ResourceID, Commit: request.Subject.Commit,
		TreeDigest: request.Subject.TreeDigest, SubjectHash: hash,
		AgentProfile: request.Claim.Work.AgentProfile, WorkspaceBinding: request.Claim.Work.Workspace,
	}
}

func linuxV2ValidateOwnerWitnessResponse(fixture linuxV2NativeFixture, response linuxV2OwnerWitnessResponse) error {
	request := fixture.request
	if response.Version != 1 || response.Case != request.Case || response.Harness != request.Harness ||
		response.NativeVersion != fixture.value.Runtime.HarnessVersion || !response.InstalledOwnerVerified ||
		response.RunKey != request.RunKey || response.Subject != request.Subject ||
		response.Fence.RuntimeID != request.Fence.RuntimeID || response.Fence.RuntimeEpoch != request.Fence.RuntimeEpoch ||
		response.Fence.Generation != request.Fence.Generation || response.Fence.ClaimID != request.Fence.ClaimID {
		return errors.New("response did not preserve exact RunKey/fence/Subject")
	}
	recovered := response.Outcome == "recovered"
	if recovered && request.Case != "daemon_sigkill_helper_survives_detached_child" {
		return errors.New("only the daemon-loss witness may return a recovered outcome")
	}
	if !recovered && response.Outcome != "completed" && !(request.Case == "wsl2_denial" && response.Outcome == "peer_denied") {
		return errors.New("production owner witness returned an unexpected outcome")
	}
	if !response.Stages.ReservationDurable || !response.Stages.CreateStarted || !response.Stages.LeafBound || !response.Stages.HelperBound ||
		response.Stages.LauncherBound == false || response.Stages.GateBound == false || response.Stages.HandoffPrepared == false ||
		response.Stages.HandoffCommitted == false || response.Stages.InitialLeaseAcked == false || response.Stages.CheckoutReady == false ||
		response.NativeReleaseState != "released" || response.Stages.NativeReleased == false ||
		!response.Stages.StopReceiptDurable || !response.Stages.LeafRemoved || !response.Stages.JournalCleared {
		return errors.New("response omitted a required v2 owner/lease/checkout/RELEASE stage")
	}
	if response.Target == nil || response.Target.PID <= 0 || response.Target.Identity == "" ||
		response.Owners.Anchor == nil || response.Owners.Helper == nil || response.Owners.Launcher == nil || response.Owners.Gate == nil ||
		response.Leaf == nil || response.Leaf.Version != authority.LinuxCgroupIdentityVersion || response.Leaf.ParentMountID == 0 ||
		response.Leaf.ParentDevice == 0 || response.Leaf.ParentInode == 0 || response.Leaf.Device == 0 || response.Leaf.Inode == 0 ||
		response.Leaf.NameSHA256 == "" || !response.Leaf.Removed || response.Clone == nil || !response.Clone.CheckoutReady ||
		response.Clone.CheckoutDevice == 0 || response.Clone.CheckoutInode == 0 || response.Clone.IntentSHA256 == "" || response.Clone.SeedDigest == "" {
		return errors.New("response omitted exact production target/owner/leaf/checkout evidence")
	}
	owners := []linuxV2OwnerWitnessProcess{*response.Owners.Anchor, *response.Owners.Helper, *response.Owners.Launcher, *response.Owners.Gate}
	for index, process := range owners {
		if process.PID <= 0 || process.Identity == "" {
			return errors.New("response omitted an exact anchor/helper/launcher/gate process identity")
		}
		for _, previous := range owners[:index] {
			if process.PID == previous.PID || process.Identity == previous.Identity {
				return errors.New("response aliased distinct Linux v2 owner processes")
			}
		}
	}
	if response.Target.PID != response.Owners.Gate.PID || response.Target.Identity != response.Owners.Gate.Identity {
		return errors.New("target did not preserve the exact gate PID and process identity")
	}
	if !recovered {
		if !response.Target.SamePIDExec || response.Target.HostUID != fixture.owner.BUID {
			return errors.New("fresh target did not preserve same-PID exec as the fixed B account")
		}
		if request.Case != "wsl2_denial" && (!response.Target.PIDFDVerified || response.Target.UserNamespace == nil ||
			response.Target.UserNamespace.Device == 0 || response.Target.UserNamespace.Inode == 0) {
			return errors.New("positive target lacks exact launch pidfd and user-namespace evidence")
		}
	}
	if request.Harness == "opencode" && request.Case != "wsl2_denial" {
		if response.Health == nil || !response.Health.Healthy || !response.Health.PeerVerified || !response.Health.NetworkNamespaceVerified ||
			response.Health.PeerPID != response.Target.PID || response.Health.PeerIdentity != response.Target.Identity ||
			response.Target.NetworkNamespace == nil || response.Target.NetworkNamespace.Device == 0 || response.Target.NetworkNamespace.Inode == 0 ||
			response.Health.NetworkNamespace == nil || response.Health.NetworkNamespace.Device != response.Target.NetworkNamespace.Device ||
			response.Health.NetworkNamespace.Inode != response.Target.NetworkNamespace.Inode {
			return errors.New("OpenCode health did not verify the exact target PID, process identity, and network namespace")
		}
	}
	if response.Stop == nil || response.Stop.ReceiptVersion != authority.SupervisorVersionV2 || response.Stop.ReceiptStatus != "stopped" ||
		!response.Stop.KillAcknowledged || !response.Stop.PopulatedZero || !response.Stop.NoMigrationObserved ||
		!response.Stop.ProducerFenced || !response.Stop.DescendantsReaped || response.Stop.Issuer == nil || response.Stop.Issuer.PID <= 0 ||
		response.Stop.Issuer.Identity == "" || response.Stop.Reaper == nil || response.Stop.Reaper.PID <= 0 || response.Stop.Reaper.Identity == "" ||
		response.Stop.LauncherExit == nil || !response.Stop.LauncherExit.Waited || !response.Stop.LauncherExit.Reaped ||
		response.Stop.LauncherExit.PID != response.Owners.Launcher.PID || response.Stop.LauncherExit.Identity != response.Owners.Launcher.Identity ||
		response.Stop.GateExit == nil || !response.Stop.GateExit.Waited || !response.Stop.GateExit.Reaped ||
		response.Stop.GateExit.PID != response.Owners.Gate.PID || response.Stop.GateExit.Identity != response.Owners.Gate.Identity ||
		response.Release == nil ||
		response.Release.Version != authority.SupervisorHandoffVersionV2 || !response.Release.OpaquePlatformRelease ||
		!response.Release.LeafRemoved || !response.Release.ClearPersisted || !response.Release.AnchorRetired {
		return errors.New("response omitted exact versioned physical stop/opaque release/clear proof")
	}
	if request.Case == "helper_sigkill_detached_child" && !response.Stages.DetachedChildLive {
		return errors.New("helper-loss witness did not record its detached child before termination")
	}
	if request.Case == "daemon_sigkill_helper_survives_detached_child" && !recovered && !response.Stages.DetachedChildLive {
		return errors.New("daemon-loss witness did not record its detached child before termination")
	}
	if (request.Case == "helper_sigkill_detached_child" || request.Case == "daemon_sigkill_helper_survives_detached_child") && !recovered &&
		(response.DetachedChild == nil || !response.DetachedChild.PIDFDVerified || !response.DetachedChild.LeafMembershipVerified) {
		return errors.New("successful/crash witness omitted its exact detached-child pidfd/leaf proof")
	}
	return nil
}

func linuxV2JournalSummaryFromOwnerResponse(response linuxV2OwnerWitnessResponse) *linuxV2JournalSnapshot {
	if response.Stop == nil || response.Release == nil {
		return nil
	}
	return &linuxV2JournalSnapshot{
		LocalState: response.Outcome, HasProcessAuthority: response.Release.ClearPersisted,
		HasProcessMarker: response.Target != nil, HasCheckoutReceipt: response.Clone != nil && response.Clone.CheckoutReady,
		HasReleaseProof: response.Stop.ReceiptVersion != 0, ReleaseProofVersion: response.Release.Version,
		ReleaseDisposition: response.Release.LeafDisposition, StopReceiptVersion: response.Stop.ReceiptVersion,
		StopProofKind: response.Stop.ProofKind, StopIssuerPID: processPID(response.Stop.Issuer),
		StopIssuerIdentity: processIdentity(response.Stop.Issuer), StopReaperPID: processPID(response.Stop.Reaper),
		StopReaperIdentity: processIdentity(response.Stop.Reaper), CgroupKillAcknowledged: response.Stop.KillAcknowledged,
		CgroupPopulatedZero: response.Stop.PopulatedZero, NoMigrationObserved: response.Stop.NoMigrationObserved,
		ProducerFenced: response.Stop.ProducerFenced, DescendantsReaped: response.Stop.DescendantsReaped,
		NoUnprovenContainment: response.Release.ClearPersisted,
	}
}

func linuxV2ProviderActionSummaryFromOwnerResponse(request linuxV2OwnerWitnessRequest, response linuxV2OwnerWitnessResponse) []linuxV2ProviderActionSnapshot {
	if response.ProviderAction == nil || response.ProviderAction.CompletedCount == 0 {
		return nil
	}
	return []linuxV2ProviderActionSnapshot{{ResourceID: request.Subject.ResourceID, Operation: "resource.sync", Outcome: response.ProviderAction.Outcome}}
}

type linuxV2ControlActionContext struct {
	Version      int    `json:"version"`
	Case         string `json:"case"`
	WorkItemID   string `json:"work_item_id"`
	TaskID       string `json:"task_id"`
	RunID        string `json:"run_id"`
	Generation   int64  `json:"generation"`
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	ClaimID      string `json:"claim_id"`
	ResourceID   string `json:"resource_id"`
}

type linuxV2ControlActionReceipt struct {
	Version        int             `json:"version"`
	Case           string          `json:"case"`
	WorkItemID     string          `json:"work_item_id"`
	TaskID         string          `json:"task_id"`
	RunID          string          `json:"run_id"`
	Generation     int64           `json:"generation"`
	RuntimeID      string          `json:"runtime_id"`
	RuntimeEpoch   int64           `json:"runtime_epoch"`
	ClaimID        string          `json:"claim_id"`
	ResourceID     string          `json:"resource_id"`
	Operation      string          `json:"operation"`
	State          string          `json:"state"`
	Provider       string          `json:"provider"`
	ResourceKind   string          `json:"resource_kind"`
	CompletedAt    string          `json:"completed_at"`
	MatchedCount   int             `json:"matched_count"`
	IntentIDSHA256 string          `json:"intent_id_sha256"`
	ActionIDSHA256 string          `json:"action_id_sha256"`
	FailurePresent bool            `json:"failure_present"`
	ResultMetadata json.RawMessage `json:"result_metadata"`
}

type linuxV2ControlActionResultMetadata struct {
	Operation  string `json:"operation"`
	ResourceID string `json:"resource_id"`
	Projected  *bool  `json:"projected"`
	Resource   struct {
		Provider     string `json:"provider"`
		Kind         string `json:"kind"`
		Status       string `json:"status"`
		SyncStatus   string `json:"sync_status"`
		LastSyncedAt string `json:"last_synced_at,omitempty"`
	} `json:"resource"`
	Readback string `json:"readback,omitempty"`
}

type linuxV2ControlActionSnapshot struct {
	WorkItemIDSHA256     string `json:"work_item_id_sha256"`
	IntentIDSHA256       string `json:"intent_id_sha256"`
	ActionIDSHA256       string `json:"action_id_sha256"`
	ResultMetadataSHA256 string `json:"result_metadata_sha256"`
	Operation            string `json:"operation"`
	State                string `json:"state"`
	Provider             string `json:"provider"`
	ResourceKind         string `json:"resource_kind"`
	Projected            bool   `json:"projected"`
	ResourceStatus       string `json:"resource_status"`
	ResourceSyncStatus   string `json:"resource_sync_status"`
}

func linuxV2ReadControlActionSnapshot(t *testing.T, fixture linuxV2NativeFixture, response linuxV2OwnerWitnessResponse) *linuxV2ControlActionSnapshot {
	t.Helper()
	if response.ProviderAction == nil || !response.ProviderAction.Attempted || response.ProviderAction.CompletedCount != 1 ||
		response.ProviderAction.Outcome != "succeeded" {
		t.Fatal("Control readback requires the exact local Pi resource.sync success outcome")
	}
	contextRootPath := strings.TrimSpace(os.Getenv(linuxV2ControlContextRootEnvironment))
	receiptRootPath := strings.TrimSpace(os.Getenv(linuxV2ControlReceiptRootEnvironment))
	contextRoot, err := linuxV2OpenControlReceiptRoot(contextRootPath, fixture.owner)
	if err != nil {
		t.Fatalf("open authenticated Control receipt context root: %v", err)
	}
	defer contextRoot.Close()
	receiptRoot, err := linuxV2OpenControlReceiptRoot(receiptRootPath, fixture.owner)
	if err != nil {
		t.Fatalf("open authenticated Control receipt root: %v", err)
	}
	defer receiptRoot.Close()
	contextBytes, err := linuxV2WaitControlReceiptFile(contextRoot, fixture.request.Case, fixture.request.RunKey.RunID,
		fixture.owner, 90*time.Second)
	if err != nil {
		t.Fatalf("read exact sanitized Control WorkItem binding: %v", err)
	}
	receiptBytes, err := linuxV2WaitControlReceiptFile(receiptRoot, fixture.request.Case, fixture.request.RunKey.RunID,
		fixture.owner, 90*time.Second)
	if err != nil {
		t.Fatalf("read independent Control provider_action_intent receipt: %v", err)
	}
	var binding linuxV2ControlActionContext
	if err := linuxV2DecodeStrictJSON(contextBytes, &binding); err != nil {
		t.Fatalf("decode sanitized Control receipt binding: %v", err)
	}
	request := fixture.request
	if binding.Version != 1 || binding.Case != request.Case || binding.WorkItemID == "" ||
		binding.TaskID != request.Claim.TaskID || binding.RunID != request.RunKey.RunID || binding.Generation != request.RunKey.Generation ||
		binding.RuntimeID != request.Fence.RuntimeID || binding.RuntimeEpoch != request.Fence.RuntimeEpoch ||
		binding.Generation != request.Fence.Generation || binding.ClaimID != request.Fence.ClaimID ||
		binding.ResourceID != request.Subject.ResourceID {
		t.Fatal("Control receipt context does not bind the exact admitted WorkItem/Task/Run/fence/resource")
	}
	var receipt linuxV2ControlActionReceipt
	if err := linuxV2DecodeStrictJSON(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode sanitized Control provider-action receipt: %v", err)
	}
	if receipt.Version != 1 || receipt.Case != request.Case || receipt.WorkItemID != binding.WorkItemID ||
		receipt.TaskID != request.Claim.TaskID || receipt.RunID != request.RunKey.RunID || receipt.Generation != request.RunKey.Generation ||
		receipt.RuntimeID != request.Fence.RuntimeID || receipt.RuntimeEpoch != request.Fence.RuntimeEpoch ||
		receipt.ClaimID != request.Fence.ClaimID || receipt.ResourceID != request.Subject.ResourceID ||
		receipt.Operation != "resource.sync" || receipt.State != "succeeded" || receipt.Provider != "github" ||
		receipt.ResourceKind != "repository" || receipt.MatchedCount != 1 || receipt.FailurePresent ||
		!linuxV2IsLowerHex64(receipt.IntentIDSHA256) || !linuxV2IsLowerHex64(receipt.ActionIDSHA256) ||
		len(receipt.ResultMetadata) == 0 {
		t.Fatal("Control provider_action_intent receipt does not bind the exact successful resource.sync claim")
	}
	completedAt, err := time.Parse(time.RFC3339Nano, receipt.CompletedAt)
	if err != nil || completedAt.IsZero() {
		t.Fatal("Control provider_action_intent receipt omitted its terminal completion time")
	}
	var result linuxV2ControlActionResultMetadata
	if err := linuxV2DecodeStrictJSON(receipt.ResultMetadata, &result); err != nil {
		t.Fatalf("decode sanitized Control resource.sync result metadata: %v", err)
	}
	if result.Operation != "resource.sync" || result.ResourceID != request.Subject.ResourceID || result.Projected == nil ||
		result.Resource.Provider != "github" || result.Resource.Kind != "repository" ||
		strings.TrimSpace(result.Resource.Status) == "" || strings.TrimSpace(result.Resource.SyncStatus) == "" {
		t.Fatal("Control result metadata does not prove the exact terminal repository resource.sync result")
	}
	if result.Readback != "" && result.Readback != "applied" && result.Readback != "unconfirmed" && result.Readback != "not_applied" {
		t.Fatal("Control result metadata contains an unsupported redacted readback state")
	}
	return &linuxV2ControlActionSnapshot{
		WorkItemIDSHA256: linuxWitnessHashBytes([]byte(binding.WorkItemID)),
		IntentIDSHA256:   receipt.IntentIDSHA256, ActionIDSHA256: receipt.ActionIDSHA256,
		ResultMetadataSHA256: linuxWitnessHashBytes(receipt.ResultMetadata),
		Operation:            receipt.Operation, State: receipt.State, Provider: receipt.Provider, ResourceKind: receipt.ResourceKind,
		Projected: *result.Projected, ResourceStatus: result.Resource.Status, ResourceSyncStatus: result.Resource.SyncStatus,
	}
}

func linuxV2OpenControlReceiptRoot(path string, owner linuxgate.OwnerConfig) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("Control receipt root is not a canonical absolute path")
	}
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open pinned filesystem root for Control receipt: %w", err)
	}
	relative, err := filepath.Rel(string(filepath.Separator), path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = unix.Close(rootFD)
		return nil, errors.New("Control receipt root escaped filesystem root")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	closeRootErr := unix.Close(rootFD)
	if err != nil {
		return nil, fmt.Errorf("open pinned Control receipt root: %w", err)
	}
	root := os.NewFile(uintptr(fd), "control-receipt-root")
	if root == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap pinned Control receipt root")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != owner.AUID ||
		stat.Gid != owner.AGID || stat.Mode&0o7777 != 0o700 {
		_ = root.Close()
		return nil, errors.New("Control receipt root is not A-owned mode 0700")
	}
	if closeRootErr != nil {
		_ = root.Close()
		return nil, fmt.Errorf("close pinned filesystem root: %w", closeRootErr)
	}
	return root, nil
}

func linuxV2WaitControlReceiptFile(root *os.File, caseName, runID string, owner linuxgate.OwnerConfig, timeout time.Duration) ([]byte, error) {
	if root == nil || caseName == "" || filepath.Base(caseName) != caseName || runID == "" || filepath.Base(runID) != runID {
		return nil, errors.New("Control receipt lookup identity is invalid")
	}
	deadline := time.Now().Add(timeout)
	caseFD := -1
	for time.Now().Before(deadline) {
		var err error
		caseFD, err = unix.Openat(int(root.Fd()), caseName, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.ENOENT) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	if caseFD < 0 {
		return nil, errors.New("Control receipt case directory did not appear before its deadline")
	}
	caseDir := os.NewFile(uintptr(caseFD), "control-receipt-case")
	if caseDir == nil {
		_ = unix.Close(caseFD)
		return nil, errors.New("wrap Control receipt case directory")
	}
	defer caseDir.Close()
	var dirStat unix.Stat_t
	if err := unix.Fstat(caseFD, &dirStat); err != nil || dirStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		dirStat.Uid != owner.AUID || dirStat.Gid != owner.AGID || dirStat.Mode&0o7777 != 0o700 {
		return nil, errors.New("Control receipt case directory is not A-owned mode 0700")
	}
	filename := runID + ".json"
	for time.Now().Before(deadline) {
		fd, openErr := unix.Openat(caseFD, filename, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if errors.Is(openErr, unix.ENOENT) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if openErr != nil {
			return nil, fmt.Errorf("open Control receipt %s: %w", filename, openErr)
		}
		file := os.NewFile(uintptr(fd), filename)
		if file == nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("wrap Control receipt %s", filename)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 ||
			stat.Gid != owner.AGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o440 || stat.Size <= 0 || stat.Size > 64<<10 {
			_ = file.Close()
			return nil, errors.New("Control receipt is not root:A 0440 singly-linked bounded regular JSON")
		}
		contents, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(contents) == 0 || len(contents) > 64<<10 {
			return nil, errors.Join(readErr, closeErr, func() error {
				if readErr == nil && len(contents) == 0 {
					return io.ErrUnexpectedEOF
				}
				if readErr == nil && len(contents) > 64<<10 {
					return errors.New("Control receipt exceeded its bounded size")
				}
				return nil
			}())
		}
		return contents, nil
	}
	return nil, errors.New("Control receipt did not arrive before the bounded Ecto-readback deadline")
}

func linuxV2IsLowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func linuxV2ProcessSnapshotFromResponse(target *linuxV2OwnerWitnessProcess, health *linuxV2OwnerWitnessHealth) *linuxV2ProcessSnapshot {
	if target == nil {
		return nil
	}
	userDevice, userInode := uint64(0), uint64(0)
	if target.UserNamespace != nil {
		userDevice, userInode = target.UserNamespace.Device, target.UserNamespace.Inode
	}
	networkDevice, networkInode := uint64(0), uint64(0)
	if target.NetworkNamespace != nil {
		networkDevice, networkInode = target.NetworkNamespace.Device, target.NetworkNamespace.Inode
	}
	return &linuxV2ProcessSnapshot{
		PID: target.PID, Identity: target.Identity, PIDFDOpened: target.PIDFDVerified, PIDFDInfoPID: target.PID,
		UserNamespaceDevice: userDevice, UserNamespaceInode: userInode,
		NetworkNamespaceDevice: networkDevice, NetworkNamespaceInode: networkInode,
		NativeCommand:            target.SamePIDExec,
		NetworkNamespaceVerified: health != nil && health.NetworkNamespaceVerified,
	}
}

func linuxV2OwnerSnapshotFromResponse(owners linuxV2OwnerWitnessOwners) *linuxV2OwnerSnapshot {
	if owners.Anchor == nil || owners.Helper == nil || owners.Launcher == nil || owners.Gate == nil {
		return nil
	}
	return &linuxV2OwnerSnapshot{
		AnchorPID: owners.Anchor.PID, AnchorIdentity: owners.Anchor.Identity,
		HelperPID: owners.Helper.PID, HelperIdentity: owners.Helper.Identity,
		LauncherPID: owners.Launcher.PID, LauncherIdentity: owners.Launcher.Identity,
		GatePID: owners.Gate.PID, GateIdentity: owners.Gate.Identity,
	}
}

func linuxV2LeafSnapshotFromResponse(leaf *linuxV2OwnerWitnessLeaf) *linuxV2LeafSnapshot {
	if leaf == nil {
		return nil
	}
	return &linuxV2LeafSnapshot{
		Version: leaf.Version, ParentMountID: leaf.ParentMountID, ParentDevice: leaf.ParentDevice,
		ParentInode: leaf.ParentInode, LeafNameHash: leaf.NameSHA256,
		LeafDevice: leaf.Device, LeafInode: leaf.Inode,
	}
}

func linuxV2CloneSnapshotFromResponse(clone *linuxV2OwnerWitnessClone) *linuxV2CloneSnapshot {
	if clone == nil {
		return nil
	}
	return &linuxV2CloneSnapshot{
		TargetPathHash: clone.IntentSHA256, TargetDevice: clone.CheckoutDevice,
		TargetInode: clone.CheckoutInode, CheckoutReady: clone.CheckoutReady,
	}
}

func linuxV2DetachedSnapshotFromResponse(child *linuxV2OwnerWitnessDetachedChild) *linuxV2DetachedChildSnapshot {
	if child == nil {
		return nil
	}
	return &linuxV2DetachedChildSnapshot{
		PID: child.PID, Identity: child.Identity, ParentPID: child.ParentPID,
		ProcessGroup: child.ProcessGroup, SessionID: child.SessionID,
		PIDFDOpened: child.PIDFDVerified, PIDFDInfoPID: child.PID,
		LeafMembership: child.LeafMembershipVerified, AnchorReaped: child.AnchorReaped,
	}
}

func processPID(process *linuxV2OwnerWitnessProcess) int {
	if process == nil {
		return 0
	}
	return process.PID
}

func processIdentity(process *linuxV2OwnerWitnessProcess) string {
	if process == nil {
		return ""
	}
	return process.Identity
}

func linuxV2KnownPeerDenial(category string) bool {
	switch category {
	case "pidfd_getfd_denied", "fdinfo_denied", "nsfs_namespace_denied":
		return true
	default:
		return false
	}
}

func releaseLinuxV2OwnerWitnessBarrier(fixture linuxV2NativeFixture) error {
	request := fixture.request
	if request.Barrier.HoldAfter == "none" {
		return nil
	}
	directoryInfo, err := os.Lstat(request.Barrier.Directory)
	if err != nil || !directoryInfo.IsDir() {
		return errors.New("owner-witness barrier directory is unavailable")
	}
	directoryStat, ok := directoryInfo.Sys().(*syscall.Stat_t)
	if !ok || directoryStat.Uid != fixture.owner.AUID || directoryStat.Gid != fixture.owner.AGID || directoryStat.Mode&0o7777 != 0o700 {
		return errors.New("owner-witness barrier directory is not the exact A-private directory")
	}
	fd, err := unix.Open(request.Barrier.ReleaseFile,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create exclusive owner-witness release marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), "owner-witness-release")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap owner-witness release marker")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure owner-witness release marker: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return errors.New("stat owner-witness release marker")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != fixture.owner.AUID || stat.Gid != fixture.owner.AGID ||
		stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 {
		_ = file.Close()
		return errors.New("owner-witness release marker has unsafe owner, link count, or mode")
	}
	contents := []byte("release\n")
	written, err := file.Write(contents)
	if err != nil || written != len(contents) {
		_ = file.Close()
		return errors.New("write exact owner-witness release marker")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync owner-witness release marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close owner-witness release marker: %w", err)
	}
	directoryFD, err := unix.Open(request.Barrier.Directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open owner-witness barrier directory for sync: %w", err)
	}
	defer unix.Close(directoryFD)
	if err := unix.Fsync(directoryFD); err != nil {
		return fmt.Errorf("sync owner-witness barrier directory: %w", err)
	}
	return nil
}

func linuxV2OwnerWitnessReleaseMarkerExists(fixture linuxV2NativeFixture) (bool, error) {
	request := fixture.request
	if request.Barrier.HoldAfter == "none" {
		return true, nil
	}
	fd, err := unix.Open(request.Barrier.ReleaseFile, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("open no-follow owner-witness release marker")
	}
	file := os.NewFile(uintptr(fd), "owner-witness-release")
	if file == nil {
		_ = unix.Close(fd)
		return false, errors.New("wrap owner-witness release marker")
	}
	defer file.Close()
	info, err := file.Stat()
	contents, readErr := io.ReadAll(io.LimitReader(file, 16))
	if err != nil || readErr != nil {
		return false, errors.New("inspect owner-witness release marker")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != fixture.owner.AUID || stat.Gid != fixture.owner.AGID ||
		stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 || info.Size() != int64(len("release\n")) || string(contents) != "release\n" {
		return false, errors.New("owner-witness release marker is not the exact private signal")
	}
	return true, nil
}

func observeLinuxV2OwnerWitnessRun(
	fixture linuxV2NativeFixture,
	command *linuxV2OwnerWitnessCommand,
	beforeChildRelease func(state.RunJournal, *linuxV2ProcessSnapshot) error,
) (state.RunJournal, *linuxV2ProcessSnapshot, error) {
	deadline := time.Now().Add(linuxV2NativeTaskTimeout)
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	var latest state.RunJournal
	var live *linuxV2ProcessSnapshot
	barrierReleased := fixture.request.Barrier.HoldAfter == "none"
	if !barrierReleased {
		var err error
		barrierReleased, err = linuxV2OwnerWitnessReleaseMarkerExists(fixture)
		if err != nil {
			return latest, live, errors.New("inspect owner-witness release marker before live observation")
		}
	}
	for time.Now().Before(deadline) {
		file, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
		if err != nil {
			return latest, live, fmt.Errorf("read exact owner-witness RunJournal: %w", err)
		}
		if found {
			latest = file.Journal
			if live == nil {
				snapshot, ready, captureErr := linuxV2CaptureLiveProcess(latest, fixture, fixture.request.Harness)
				if captureErr != nil {
					return latest, live, captureErr
				}
				if ready {
					live = &snapshot
				}
			}
			if live != nil && !barrierReleased && beforeChildRelease == nil {
				if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
					return latest, live, err
				}
				barrierReleased = true
			}
			if live != nil && live.detachedChild == nil {
				child, ready, captureErr := linuxV2CaptureDetachedChild(latest, fixture, *live)
				if captureErr != nil {
					return latest, live, captureErr
				}
				if ready {
					live.detachedChild = &child
					if beforeChildRelease != nil {
						if err := beforeChildRelease(latest, live); err != nil {
							return latest, live, err
						}
					} else if err := linuxV2WriteBHookReleaseMarker(fixture, "post-commit-child.release"); err != nil {
						return latest, live, fmt.Errorf("release bounded B post-commit hook: %w", err)
					}
				}
			}
			if linuxV2JournalHasPhysicalRelease(latest) {
				return latest, live, nil
			}
		}
		if command != nil && command.pidfd != nil {
			exited, err := linuxV2PIDFDExitedNow(int(command.pidfd.Fd()))
			if err != nil {
				return latest, live, err
			}
			if exited {
				return latest, live, nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return latest, live, errors.New("isolated production owner witness did not reach stop/clear before its bounded deadline")
}

func linuxV2PIDFDExitedNow(pidfd int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: int16(unix.POLLIN)}}
	if _, err := unix.Poll(fds, 0); err != nil {
		if errors.Is(err, unix.EINTR) {
			return false, nil
		}
		return false, fmt.Errorf("poll owner-witness process pidfd: %w", err)
	}
	return fds[0].Revents != 0, nil
}

func linuxV2ReadOwnerWitnessStageMarker(fixture linuxV2NativeFixture, stage string) (linuxV2OwnerWitnessStageMarker, error) {
	path := filepath.Join(fixture.request.Barrier.Directory, stage+".json")
	contents, err := os.ReadFile(path)
	if err != nil {
		return linuxV2OwnerWitnessStageMarker{}, fmt.Errorf("read durable %s marker: %w", stage, err)
	}
	var marker linuxV2OwnerWitnessStageMarker
	if err := json.Unmarshal(contents, &marker); err != nil || marker.Version != 1 || marker.Case != fixture.request.Case ||
		marker.Stage != stage || marker.HoldAfter != fixture.request.Barrier.HoldAfter || marker.RunKey != fixture.request.RunKey {
		return linuxV2OwnerWitnessStageMarker{}, errors.New("stage marker does not bind the exact stage-replay Case and RunKey")
	}
	return marker, nil
}

func linuxV2NativeReleaseStateValid(value linuxV2NativeReleaseState) bool {
	return value == "not_released" || value == "released" || value == "unknown"
}

func linuxV2HasPositiveNativeReleaseMarker(fixture linuxV2NativeFixture) (bool, error) {
	found := false
	for _, stage := range []string{
		"native_release", "native_turn_started", "detached_child_live", "stop_receipt_durable", "leaf_removed", "journal_cleared",
	} {
		marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, stage)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !linuxV2NativeReleaseStateValid(marker.NativeReleaseState) {
			return false, fmt.Errorf("%s marker lacks a valid tri-state native RELEASE assessment", stage)
		}
		if expected, known := linuxV2ExpectedStageMarkerReleaseState(stage); known && marker.NativeReleaseState != expected {
			return false, fmt.Errorf("%s marker release state conflicts with its positive boundary", stage)
		}
		if marker.NativeReleaseState == "released" {
			if !linuxV2MarkerHasNativePeerProof(fixture, marker) {
				return false, fmt.Errorf("%s marker claims RELEASE without exact B peer evidence", stage)
			}
			found = true
		} else if linuxV2MarkerHasBPeerClaims(marker.Target) {
			return false, fmt.Errorf("%s marker claims B peer identity while release state is %q", stage, marker.NativeReleaseState)
		}
	}
	return found, nil
}

func linuxV2StageReplayJournalProvesNoNativeRelease(fixture linuxV2NativeFixture) (bool, error) {
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	file, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found {
		return false, err
	}
	journal := file.Journal
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil && proof.Validate() == nil {
		return true, nil
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 &&
		journal.ContainmentAuthority == nil {
		return true, nil
	}
	if journal.ContainmentCgroup != nil && journal.ContainmentHandoff == nil && journal.ContainmentAuthority == nil {
		return true, nil
	}
	if journal.LinuxCloneIntent == nil && journal.ContainmentCgroup == nil && journal.ContainmentHandoff == nil &&
		journal.ContainmentAuthority == nil && !journal.HasProcessDetails() && !journal.ContainmentUnproven {
		return true, nil
	}
	return false, nil
}

func linuxV2MarkerHasNativePeerProof(fixture linuxV2NativeFixture, marker linuxV2OwnerWitnessStageMarker) bool {
	target, gate := marker.Target, marker.Owners.Gate
	return target.PID > 0 && target.Identity != "" && target.PIDFDVerified && target.HostUID == fixture.owner.BUID &&
		target.SamePIDExec && target.UserNamespace != nil && target.UserNamespace.Device != 0 && target.UserNamespace.Inode != 0 &&
		target.NetworkNamespace != nil && target.NetworkNamespace.Device != 0 && target.NetworkNamespace.Inode != 0 &&
		gate != nil && target.PID == gate.PID && target.Identity == gate.Identity
}

func linuxV2MarkerHasBPeerClaims(target linuxV2OwnerWitnessProcess) bool {
	return target.PIDFDVerified || target.HostUID != 0 || target.SamePIDExec ||
		target.UserNamespace != nil && (target.UserNamespace.Device != 0 || target.UserNamespace.Inode != 0) ||
		target.NetworkNamespace != nil && (target.NetworkNamespace.Device != 0 || target.NetworkNamespace.Inode != 0)
}

func linuxV2ExpectedStageMarkerReleaseState(stage string) (linuxV2NativeReleaseState, bool) {
	switch stage {
	case "reservation_durable", "create_started", "leaf_bound", "helper_bound", "launcher_bound", "gate_bound",
		"handoff_prepared", "handoff_committed", "lease_armed", "checkout_ready":
		return "not_released", true
	case "native_release", "native_turn_started", "detached_child_live":
		return "released", true
	default:
		return "unknown", false
	}
}

func linuxV2StageWasRestored(stages linuxV2OwnerWitnessStages, stage string) bool {
	switch stage {
	case "reservation_durable":
		return stages.ReservationDurable
	case "create_started":
		return stages.CreateStarted
	case "leaf_bound":
		return stages.LeafBound
	case "helper_bound":
		return stages.HelperBound
	case "launcher_bound":
		return stages.LauncherBound
	case "gate_bound":
		return stages.GateBound
	case "handoff_prepared":
		return stages.HandoffPrepared
	case "handoff_committed":
		return stages.HandoffCommitted
	case "lease_armed":
		return stages.InitialLeaseAcked
	case "checkout_ready":
		return stages.CheckoutReady
	case "native_release":
		return stages.NativeReleased
	case "stop_receipt_durable":
		return stages.StopReceiptDurable
	case "leaf_removed":
		return stages.LeafRemoved
	case "journal_cleared":
		return stages.JournalCleared
	default:
		return false
	}
}

func linuxV2ValidateStageReplayResponse(fixture linuxV2NativeFixture, stage string, response linuxV2OwnerWitnessResponse) error {
	expectedRecovery := linuxV2ExpectedStageReplayRecovery(stage)
	if expectedRecovery == "" {
		return fmt.Errorf("unsupported stage replay boundary %q", stage)
	}
	return linuxV2ValidateStageReplayResponseState(fixture, stage, response, expectedRecovery)
}

func linuxV2ExpectedStageReplayRecovery(stage string) string {
	return map[string]string{
		"reservation_durable": "aborted_no_target",
		"create_started":      "unresolved", "leaf_bound": "unresolved", "helper_bound": "aborted_no_target",
		"launcher_bound": "unresolved", "gate_bound": "released", "handoff_prepared": "released",
		"handoff_committed": "released", "lease_armed": "released", "checkout_ready": "released",
		"native_release": "released", "stop_receipt_durable": "released", "leaf_removed": "released", "journal_cleared": "released",
	}[stage]
}

func linuxV2ValidateStageReplayResponseState(fixture linuxV2NativeFixture, stage string, response linuxV2OwnerWitnessResponse, expectedRecovery string) error {
	request := fixture.request
	if response.Version != 1 || response.Case != "stage_replay" || response.Harness != request.Harness ||
		response.NativeVersion != fixture.value.Runtime.HarnessVersion || !response.InstalledOwnerVerified ||
		response.RunKey != request.RunKey || response.Subject != request.Subject ||
		response.Fence.RuntimeID != request.Fence.RuntimeID || response.Fence.RuntimeEpoch != request.Fence.RuntimeEpoch ||
		response.Fence.Generation != request.Fence.Generation || response.Fence.ClaimID != request.Fence.ClaimID {
		return errors.New("stage-replay response changed the exact installed runtime, RunKey, fence, or Subject")
	}
	if !linuxV2StageWasRestored(response.Stages, stage) {
		return errors.New("same-RunKey recovery did not restore the exact durable barrier stage")
	}
	if stage != request.Barrier.HoldAfter {
		return errors.New("stage-replay response does not bind the exact held request boundary")
	}
	heldMarker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, request.Barrier.HoldAfter)
	if err != nil {
		return fmt.Errorf("read exact held stage marker for RELEASE reconciliation: %w", err)
	}
	if heldMarker.HoldAfter != request.Barrier.HoldAfter || !linuxV2NativeReleaseStateValid(heldMarker.NativeReleaseState) {
		return errors.New("held stage marker omitted its exact boundary or explicit native RELEASE state")
	}
	positiveRelease, err := linuxV2HasPositiveNativeReleaseMarker(fixture)
	if err != nil {
		return fmt.Errorf("validate exact positive native RELEASE markers: %w", err)
	}
	if response.Stages.NativeReleased != positiveRelease {
		return errors.New("NativeReleased stage bit disagrees with exact positive release-marker evidence")
	}
	expectedReleaseState := linuxV2NativeReleaseState("unknown")
	switch {
	case positiveRelease:
		expectedReleaseState = "released"
	case heldMarker.NativeReleaseState == "released":
		return errors.New("held marker claims RELEASE without positive native_release/turn/child evidence")
	case heldMarker.NativeReleaseState == "not_released":
		expectedReleaseState = "not_released"
	case heldMarker.NativeReleaseState == "unknown" && response.NativeReleaseState == "not_released":
		proven, proofErr := linuxV2StageReplayJournalProvesNoNativeRelease(fixture)
		if proofErr != nil {
			return fmt.Errorf("inspect exact RunJournal for no-release proof: %w", proofErr)
		}
		if !proven {
			return errors.New("not_released recovery state lacks exact held-boundary, no-target, or durable pre-commit proof")
		}
		expectedReleaseState = "not_released"
	}
	if response.Stages.CheckoutReady && !response.Stages.InitialLeaseAcked {
		return errors.New("Store checkout readiness lacks the earlier initial lease ACK")
	}
	if response.NativeReleaseState == "released" && (!response.Stages.InitialLeaseAcked || !response.Stages.CheckoutReady) {
		return errors.New("positive native RELEASE evidence lacks the preceding lease ACK and Store checkout CAS")
	}
	switch stage {
	case "lease_armed":
		if !response.Stages.InitialLeaseAcked || response.Stages.CheckoutReady {
			return errors.New("lease_armed recovery changed its exact pre-checkout boundary")
		}
	case "checkout_ready", "native_release":
		if !response.Stages.InitialLeaseAcked || !response.Stages.CheckoutReady {
			return errors.New("checkout/native-release boundary lacks the exact prior lease ACK and Store CAS")
		}
	}
	if response.NativeReleaseState != expectedReleaseState {
		return errors.New("stage replay release state disagrees with exact held, positive-release, and no-target evidence")
	}
	if response.NativeReleaseState == "released" {
		if response.Target == nil || !linuxV2MarkerHasNativePeerProof(fixture, linuxV2OwnerWitnessStageMarker{
			Target: *response.Target, Owners: response.Owners,
		}) {
			return errors.New("released App response lacks exact B PIDFD/namespace/SamePIDExec evidence")
		}
	} else if response.Target != nil && linuxV2MarkerHasBPeerClaims(*response.Target) {
		return errors.New("unknown or not-released App response contains B peer PIDFD/HostUID/namespace claims")
	}
	if response.RecoveryState != expectedRecovery {
		return fmt.Errorf("stage %s recovery_state = %q, want %q", stage, response.RecoveryState, expectedRecovery)
	}
	switch response.RecoveryState {
	case "released", "pre_gate_released":
		if response.Outcome != "recovered" || response.JournalUnproven || response.NoTargetAbort || response.Release == nil ||
			!response.Release.OpaquePlatformRelease || !response.Release.LeafRemoved || !response.Release.ClearPersisted || !response.Release.AnchorRetired {
			return errors.New("released stage replay lacks exact recovered opaque-clear proof")
		}
	case "aborted_no_target":
		if response.Outcome != "aborted_no_target" || !response.NoTargetAbort || response.JournalUnproven ||
			response.Release != nil && (response.Release.OpaquePlatformRelease || response.Release.ClearPersisted) ||
			response.Stop != nil && response.Stop.ReceiptVersion != 0 {
			return errors.New("never-created stage replay fabricated a target stop or receipt")
		}
	case "no_native_effects":
		if response.Outcome != "no_native_effects" || response.NoTargetAbort || response.JournalUnproven ||
			response.Release != nil && response.Release.ClearPersisted || response.Stop != nil && response.Stop.ReceiptVersion != 0 {
			return errors.New("no-native-effects replay fabricated containment authority or a stop receipt")
		}
	case "unresolved":
		appResponseLoss := request.DropAckAfter != "" && response.FailureCategory == "response_lost" &&
			response.ResponseLostAfter == request.DropAckAfter
		helperResponseLoss := fixture.helperResponseDrop != "" && response.HelperDropObserved &&
			response.HelperDropOperation == fixture.helperResponseDrop && response.FailureCategory == "response_lost"
		lostClearAck := appResponseLoss && request.DropAckAfter == "journal_cleared"
		if response.Outcome != "unresolved" || !response.JournalUnproven || response.NoTargetAbort ||
			response.FailureCategory != "containment_unproven" && !appResponseLoss && !helperResponseLoss {
			return errors.New("unresolved stage replay lacks exact response-loss or containment-unproven evidence")
		}
		if lostClearAck {
			if response.Release == nil || response.Release.Version != authority.SupervisorHandoffVersionV2 ||
				!response.Release.OpaquePlatformRelease || !response.Release.LeafRemoved || !response.Release.ClearPersisted ||
				response.Release.AnchorRetired || !response.Stages.JournalCleared {
				return errors.New("lost journal-clear ACK lacks its exact durable v2 release tombstone")
			}
		} else if response.Release != nil &&
			(response.Release.OpaquePlatformRelease || response.Release.ClearPersisted || response.Release.LeafRemoved) ||
			response.Stages.JournalCleared {
			return errors.New("unresolved stage replay invented opaque release/clear proof")
		}
		if response.Stop != nil && response.Stop.ReceiptVersion != 0 {
			if response.Stop.ReceiptVersion != authority.SupervisorVersionV2 || response.Stop.ReceiptStatus != "stopped" ||
				response.Stop.ProofKind == "" || response.Stop.Issuer == nil || response.Stop.Reaper == nil ||
				!response.Stop.KillAcknowledged || !response.Stop.PopulatedZero || !response.Stop.NoMigrationObserved ||
				!response.Stop.ProducerFenced || !response.Stop.DescendantsReaped {
				return errors.New("unresolved stage replay stop receipt is not an exact versioned physical-stop proof")
			}
		}
	default:
		return fmt.Errorf("stage replay returned unknown recovery_state %q", response.RecoveryState)
	}
	if response.ProviderAction != nil && response.ProviderAction.CompletedCount != 0 {
		return errors.New("stage-replay health/containment case unexpectedly completed a provider action")
	}
	return nil
}

func linuxV2ValidateStageReplayJournal(fixture linuxV2NativeFixture, response linuxV2OwnerWitnessResponse) (linuxWitnessJournalFile, bool, error) {
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	file, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil {
		return linuxWitnessJournalFile{}, false, fmt.Errorf("read exact stage-replay RunJournal: %w", err)
	}
	if found && file.Journal.Key() != key {
		return linuxWitnessJournalFile{}, false, errors.New("stage-replay Store returned a journal for another RunKey")
	}
	switch response.RecoveryState {
	case "released":
		if !found || !linuxV2JournalHasPhysicalRelease(file.Journal) || file.Journal.ContainmentCgroupPreGateReleaseProof != nil {
			return file, found, errors.New("recovered stage does not have its exact v2 target stop receipt and release tombstone")
		}
		proof := file.Journal.ContainmentCgroupReleaseProof
		if proof.StopReceipt.Version != authority.SupervisorVersionV2 || proof.StopReceipt.LinuxProof == nil ||
			!proof.StopReceipt.LinuxProof.CgroupKillAcknowledged || !proof.StopReceipt.LinuxProof.CgroupPopulatedZero ||
			!proof.StopReceipt.LinuxProof.NoMigrationObserved || !proof.StopReceipt.LinuxProof.ProducerFenced ||
			!proof.StopReceipt.LinuxProof.DescendantsReaped {
			return file, found, errors.New("v2 release tombstone lacks its exact physical stop receipt")
		}
		leafPath := linuxV2LeafPath(fixture.value, *proof.CgroupIdentity)
		if _, err := os.Lstat(leafPath); !errors.Is(err, os.ErrNotExist) {
			return file, found, errors.New("released stage still has the exact pinned cgroup leaf")
		}
	case "pre_gate_released":
		if !found || file.Journal.ContainmentCgroupPreGateReleaseProof == nil ||
			file.Journal.ContainmentCgroupPreGateReleaseProof.Validate() != nil ||
			file.Journal.ContainmentCgroupReleaseProof != nil || file.Journal.HasProcessDetails() ||
			file.Journal.ContainmentAuthority != nil || file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentUnproven {
			return file, found, errors.New("pre-gate stage replay lacks its exact receipt-backed PID-zero release tombstone")
		}
		identity := file.Journal.ContainmentCgroupPreGateReleaseProof.Receipt.CgroupIdentity
		leafPath := linuxV2LeafPath(fixture.value, identity)
		if _, err := os.Lstat(leafPath); !errors.Is(err, os.ErrNotExist) {
			return file, found, errors.New("pre-gate released stage still has the exact pinned cgroup leaf")
		}
	case "aborted_no_target":
		if !response.NoTargetAbort || response.JournalUnproven || found &&
			(file.Journal.ContainmentCgroupReleaseProof != nil || file.Journal.ContainmentCgroupPreGateReleaseProof != nil ||
				file.Journal.HasProcessDetails() || file.Journal.ContainmentAuthority != nil || file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentUnproven) {
			return file, found, errors.New("never-created reservation was represented as a target stop or remains unproven")
		}
		if found {
			if err := linuxV2VerifyNoTargetCloneAbort(fixture, file.Journal); err != nil {
				return file, found, err
			}
		}
		if found {
			var abort *authority.LinuxCgroupReservationAbortProof
			if file.Journal.ContainmentCgroup != nil && file.Journal.ContainmentCgroup.AbortProof != nil {
				copy := file.Journal.ContainmentCgroup.AbortProof.Clone()
				abort = &copy
			}
			if cloneAbort := file.Journal.LinuxCloneAbortProof; cloneAbort != nil {
				if cloneAbort.Validate() != nil || cloneAbort.Intent.RunID != fixture.request.RunKey.RunID ||
					cloneAbort.Intent.Generation != fixture.request.RunKey.Generation || cloneAbort.Intent.ResourceID != fixture.request.Subject.ResourceID {
					return file, found, errors.New("no-target clone tombstone changed the exact RunKey/Subject")
				}
				if cloneAbort.CgroupAbortProof != nil {
					copy := cloneAbort.CgroupAbortProof.Clone()
					abort = &copy
				}
			}
			if abort == nil || abort.Validate() != nil || !abort.CreatorFenced || !abort.NoProducerCanAct {
				return file, found, errors.New("no-target tombstone lacks its exact creator-fenced abort proof")
			}
			if reservation := file.Journal.ContainmentCgroup; reservation != nil && reservation.Mirror != nil &&
				!abort.ValidFor(reservation.LaunchToken, reservation.Parent, reservation.LeafName, reservation.CreateStarted,
					reservation.OwnerUID, reservation.Anchor, *reservation.Mirror, reservation.Helper, reservation.Launcher) {
				return file, found, errors.New("no-target abort proof does not bind its exact durable reservation")
			}
			switch abort.LeafDisposition {
			case authority.LinuxCgroupAbortLeafNeverCreated:
				if abort.CreateStarted || !abort.LeafNeverAppeared || abort.Helper != nil || abort.HelperExit != nil {
					return file, found, errors.New("pre-create abort tombstone reports a target or leaf creation")
				}
			case authority.LinuxCgroupAbortLeafRemovedEmpty, authority.LinuxCgroupAbortLeafAbsentAfterRmdir:
				if !abort.CreateStarted || !abort.LeafEmpty || !abort.LeafAbsent || abort.LeafNeverAppeared || abort.Helper == nil ||
					abort.HelperExit == nil || !abort.HelperExit.Waited || !abort.HelperExit.Reaped {
					return file, found, errors.New("no-gate helper abort lacks exact empty-leaf/helper-reap evidence")
				}
			default:
				return file, found, errors.New("no-target tombstone has an unknown cgroup leaf disposition")
			}
		}
	case "no_native_effects":
		if found && (file.Journal.ContainmentCgroup != nil || file.Journal.ContainmentCgroupReleaseProof != nil ||
			file.Journal.ContainmentCgroupPreGateReleaseProof != nil || file.Journal.HasProcessDetails() ||
			file.Journal.ContainmentAuthority != nil || file.Journal.ContainmentHandoff != nil || file.Journal.ContainmentUnproven) {
			return file, found, errors.New("no-native-effects outcome retains a fabricated v2 process subject")
		}
	case "unresolved":
		if fixture.request.DropAckAfter == "journal_cleared" && response.ResponseLostAfter == "journal_cleared" {
			if !found || !linuxV2JournalHasPhysicalRelease(file.Journal) || file.Journal.ContainmentCgroupPreGateReleaseProof != nil ||
				file.Journal.ContainmentCgroupReleaseProof.StopReceipt.Version != authority.SupervisorVersionV2 {
				return file, found, errors.New("lost clear ACK lacks the exact persisted v2 release tombstone")
			}
		} else if !found || !response.JournalUnproven || file.Journal.ContainmentCgroupReleaseProof != nil ||
			file.Journal.ContainmentCgroupPreGateReleaseProof != nil ||
			!(file.Journal.ContainmentUnproven || file.Journal.ContainmentCgroup != nil || file.Journal.HasProcessDetails() ||
				file.Journal.ContainmentAuthority != nil || file.Journal.ContainmentHandoff != nil || file.Journal.LinuxCloneAbortPending) {
			return file, found, errors.New("unresolved stage replay did not retain its exact pending authority or clear tombstone")
		}
	}
	return file, found, nil
}

func linuxV2VerifyNoTargetCloneAbort(fixture linuxV2NativeFixture, journal state.RunJournal) error {
	proof := journal.LinuxCloneAbortProof
	if proof == nil || journal.LinuxCloneAbortPending || proof.Validate() != nil ||
		proof.Intent.RunID != fixture.request.RunKey.RunID || proof.Intent.Generation != fixture.request.RunKey.Generation ||
		proof.Intent.ResourceID != fixture.request.Subject.ResourceID || proof.Intent.BaseCommit != fixture.request.Subject.Commit ||
		proof.Intent.BaseTreeDigest != fixture.request.Subject.TreeDigest || !proof.TargetAbsent || !proof.SeedAbsent ||
		!proof.BaseRefAbsent || !proof.PromotionRefAbsent || journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
		return errors.New("no-target outcome lacks the exact clone-abort proof and A seed/ref absence attestations")
	}
	for _, path := range []string{proof.Intent.Target, proof.Intent.SeedPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("clone-abort target or A seed still exists on disk")
		}
	}
	for _, reference := range []string{proof.BaseRef, proof.PromotionRef} {
		command := exec.Command("git", "-C", fixture.workspace.Repository, "show-ref", "--verify", "--quiet", reference)
		command.Env = linuxV2SafeHostEnvironment()
		if err := command.Run(); err == nil {
			return errors.New("clone-abort A repository still has the per-run base or promotion ref")
		} else {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				return fmt.Errorf("verify clone-abort ref absence: %w", err)
			}
		}
	}
	return nil
}

func linuxV2StageReplayJournalSnapshot(file linuxWitnessJournalFile, found bool) *linuxV2JournalSnapshot {
	if !found {
		return nil
	}
	journal := file.Journal
	summary := &linuxV2JournalSnapshot{
		LocalState: journal.LocalState, TerminalState: journal.TerminalState, TerminalVerdict: journal.TerminalVerdict,
		HasCgroupReservation: journal.ContainmentCgroup != nil,
		HasProcessAuthority:  journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil,
		HasProcessMarker:     journal.HasProcessDetails(), HasCheckoutReceipt: journal.LinuxCloneCheckoutReceipt != nil,
		HasPromotionReceipt:   journal.LinuxClonePromotionReceipt != nil,
		HasReleaseProof:       journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil,
		NoUnprovenContainment: !journal.ContainmentUnproven,
	}
	if proof := journal.ContainmentCgroupReleaseProof; proof != nil {
		summary.ReleaseProofVersion = proof.Version
		summary.ReleaseDisposition = proof.LeafDisposition
		if receipt := proof.StopReceipt; receipt != nil {
			summary.StopReceiptVersion = receipt.Version
			if linuxProof := receipt.LinuxProof; linuxProof != nil {
				summary.StopProofKind = linuxProof.Kind
				summary.StopIssuerPID, summary.StopIssuerIdentity = linuxProof.Issuer.PID, linuxProof.Issuer.Identity
				summary.StopReaperPID, summary.StopReaperIdentity = linuxProof.Reaper.PID, linuxProof.Reaper.Identity
				summary.CgroupKillAcknowledged = linuxProof.CgroupKillAcknowledged
				summary.CgroupPopulatedZero = linuxProof.CgroupPopulatedZero
				summary.NoMigrationObserved = linuxProof.NoMigrationObserved
				summary.ProducerFenced = linuxProof.ProducerFenced
				summary.DescendantsReaped = linuxProof.DescendantsReaped
			}
		}
	}
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil {
		summary.ReleaseProofVersion = proof.Version
		summary.ReleaseDisposition = proof.LeafDisposition
		summary.StopReceiptVersion = proof.Receipt.Version
		summary.StopProofKind = proof.Receipt.Proof.Kind
		summary.StopIssuerPID, summary.StopIssuerIdentity = proof.Receipt.Proof.Issuer.PID, proof.Receipt.Proof.Issuer.Identity
		summary.StopReaperPID, summary.StopReaperIdentity = proof.Receipt.Proof.Reaper.PID, proof.Receipt.Proof.Reaper.Identity
		summary.CgroupKillAcknowledged = proof.Receipt.Proof.CgroupKillAcknowledged
		summary.CgroupPopulatedZero = proof.Receipt.Proof.CgroupPopulatedZero
		summary.NoMigrationObserved = proof.Receipt.Proof.NoMigrationObserved
		summary.ProducerFenced = proof.Receipt.Proof.ProducerFenced
		summary.DescendantsReaped = proof.Receipt.Proof.DescendantsReaped
	}
	return summary
}

func linuxV2ValidateStageJournalBoundary(fixture linuxV2NativeFixture, stage string, marker linuxV2OwnerWitnessStageMarker, journal state.RunJournal) error {
	if journal.Key() != (state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}) ||
		marker.Case != "stage_replay" || marker.Stage != stage || marker.HoldAfter != fixture.request.Barrier.HoldAfter ||
		marker.HoldAfter != stage || marker.RunKey != fixture.request.RunKey {
		return errors.New("stage marker or persisted journal changed the exact request identity")
	}
	if !linuxV2NativeReleaseStateValid(marker.NativeReleaseState) {
		return errors.New("durable stage marker omitted a valid tri-state native RELEASE assessment")
	}
	if expectedState, known := linuxV2ExpectedStageMarkerReleaseState(stage); known && marker.NativeReleaseState != expectedState {
		return errors.New("exact held marker has a native RELEASE state inconsistent with its known boundary")
	}
	positiveRelease, err := linuxV2HasPositiveNativeReleaseMarker(fixture)
	if err != nil {
		return fmt.Errorf("validate positive native RELEASE evidence at held boundary: %w", err)
	}
	if (marker.NativeReleaseState == "released") != positiveRelease {
		return errors.New("held marker state disagrees with exact native_release/turn/child marker evidence")
	}
	if marker.NativeReleaseState == "released" {
		if !linuxV2MarkerHasNativePeerProof(fixture, marker) {
			return errors.New("released held marker lacks the exact fixed-B same-PID pidfd/namespace target")
		}
	} else if marker.Target.HostUID != 0 || marker.Target.PIDFDVerified || marker.Target.SamePIDExec ||
		marker.Target.UserNamespace != nil && (marker.Target.UserNamespace.Device != 0 || marker.Target.UserNamespace.Inode != 0) ||
		marker.Target.NetworkNamespace != nil && (marker.Target.NetworkNamespace.Device != 0 || marker.Target.NetworkNamespace.Inode != 0) {
		return errors.New("non-released marker prematurely labeled the gate as a native B target")
	}
	reservation := journal.ContainmentCgroup
	switch stage {
	case "reservation_durable":
		if reservation == nil || reservation.CreateStarted || reservation.LeafIdentity != nil || reservation.Helper != nil {
			return errors.New("reservation_durable did not stop before the exact leaf creation CAS")
		}
	case "create_started":
		if reservation == nil || !reservation.CreateStarted || reservation.LeafIdentity != nil || reservation.Helper != nil {
			return errors.New("create_started did not persist the creator fence before leaf/helper binding")
		}
	case "leaf_bound":
		if reservation == nil || !reservation.CreateStarted || reservation.LeafIdentity == nil || reservation.Helper != nil {
			return errors.New("leaf_bound did not preserve the exact measured leaf before helper binding")
		}
	case "helper_bound":
		if reservation == nil || reservation.Helper == nil || reservation.LeafIdentity == nil {
			return errors.New("helper_bound did not preserve the exact helper and pinned leaf")
		}
	case "launcher_bound":
		if reservation == nil || reservation.Helper == nil || reservation.Launcher == nil ||
			reservation.Launcher.PID <= 0 || reservation.Launcher.Identity == "" {
			return errors.New("launcher_bound did not preserve exact helper and static launcher identities")
		}
	case "gate_bound":
		if reservation == nil || reservation.Gate == nil || reservation.Gate.Process.PID <= 0 || reservation.Gate.Process.Identity == "" {
			return errors.New("gate_bound did not preserve the exact same-PID gate subject")
		}
	case "handoff_prepared":
		if journal.ContainmentHandoff == nil || journal.ContainmentAuthority != nil {
			return errors.New("handoff_prepared did not preserve only the exact uncommitted handoff")
		}
	case "handoff_committed", "lease_armed", "native_release":
		if journal.ContainmentAuthority == nil || journal.ContainmentHandoff != nil {
			return errors.New("post-commit stage did not preserve the exact committed authority")
		}
		if stage == "native_release" {
			if journal.LinuxCloneCheckoutReceipt == nil || journal.LinuxCloneCheckoutReceipt.Validate() != nil {
				return errors.New("native_release lacks the exact durable Store checkout-ready receipt")
			}
			if err := linuxV2VerifyStageReplayCheckout(fixture, *journal.LinuxCloneCheckoutReceipt); err != nil {
				return fmt.Errorf("native_release Store checkout receipt is not the exact B-owned CAS: %w", err)
			}
		}
	case "checkout_ready":
		if journal.ContainmentAuthority == nil || journal.LinuxCloneCheckoutReceipt == nil || journal.LinuxCloneCheckoutReceipt.Validate() != nil {
			return errors.New("checkout_ready did not preserve its exact Store checkout receipt")
		}
	case "stop_receipt_durable":
		if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil ||
			!journal.ContainmentAuthority.StopReceipt.ValidFor(*journal.ContainmentAuthority) {
			return errors.New("stop_receipt_durable did not preserve its exact versioned receipt")
		}
	case "leaf_removed":
		if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil ||
			marker.Release.Version != authority.SupervisorHandoffVersionV2 || !marker.Release.OpaquePlatformRelease ||
			!marker.Release.LeafRemoved || marker.Release.ClearPersisted || journal.ContainmentCgroup == nil ||
			journal.ContainmentCgroup.LeafIdentity == nil {
			return errors.New("leaf_removed did not bind the opaque release before Journal clear")
		}
		leafPath := linuxV2LeafPath(fixture.value, *journal.ContainmentCgroup.LeafIdentity)
		if _, err := os.Lstat(leafPath); !errors.Is(err, os.ErrNotExist) {
			return errors.New("leaf_removed boundary still has the exact pinned cgroup leaf")
		}
	case "journal_cleared":
		if journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
			journal.HasProcessDetails() || journal.ContainmentUnproven || journal.ContainmentCgroupReleaseProof == nil ||
			journal.ContainmentCgroupReleaseProof.Validate() != nil || !marker.Release.ClearPersisted {
			return errors.New("journal_cleared marker retained process authority or unproven state")
		}
	default:
		return fmt.Errorf("unsupported stage replay boundary %q", stage)
	}
	return nil
}

func linuxV2RequireSameReplayRequest(first, replay linuxV2OwnerWitnessRequest, dropStage string) error {
	firstClaim, err := json.Marshal(first.Claim)
	if err != nil {
		return fmt.Errorf("encode original authenticated StageReplay Claim for comparison: %w", err)
	}
	replayClaim, err := json.Marshal(replay.Claim)
	if err != nil {
		return fmt.Errorf("encode replayed authenticated StageReplay Claim for comparison: %w", err)
	}
	if first.Version != replay.Version || first.Case != replay.Case || first.Harness != replay.Harness ||
		first.ReplayRequired != replay.ReplayRequired || first.RunKey != replay.RunKey || first.Fence != replay.Fence || first.Subject != replay.Subject ||
		first.Barrier != replay.Barrier || first.DropAckAfter != dropStage || replay.DropAckAfter != "" ||
		!bytes.Equal(firstClaim, replayClaim) {
		return errors.New("response-loss replay changed the exact claim, RunKey, fence, Subject, or held boundary")
	}
	return nil
}

func linuxV2OwnerCommandExitCode(command *linuxV2OwnerWitnessCommand) int {
	if command == nil || command.exitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(command.exitErr, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}

func linuxV2RequireSameOwnerIdentities(expected, actual linuxV2OwnerWitnessOwners) error {
	for _, pair := range []struct {
		name     string
		expected *linuxV2OwnerWitnessProcess
		actual   *linuxV2OwnerWitnessProcess
	}{
		{name: "anchor", expected: expected.Anchor, actual: actual.Anchor},
		{name: "helper", expected: expected.Helper, actual: actual.Helper},
		{name: "launcher", expected: expected.Launcher, actual: actual.Launcher},
		{name: "gate", expected: expected.Gate, actual: actual.Gate},
	} {
		if pair.expected != nil && (pair.actual == nil || pair.actual.PID != pair.expected.PID || pair.actual.Identity != pair.expected.Identity) {
			return fmt.Errorf("same-RunKey replay changed exact %s PID/creation identity", pair.name)
		}
	}
	seenPIDs := make(map[int]struct{}, 4)
	seenIdentities := make(map[string]struct{}, 4)
	for _, process := range []*linuxV2OwnerWitnessProcess{actual.Anchor, actual.Helper, actual.Launcher, actual.Gate} {
		if process == nil {
			continue
		}
		if process.PID <= 1 || process.Identity == "" {
			return errors.New("replayed owner set contains an incomplete process identity")
		}
		if _, ok := seenPIDs[process.PID]; ok {
			return errors.New("replayed owner set duplicates a numeric PID")
		}
		if _, ok := seenIdentities[process.Identity]; ok {
			return errors.New("replayed owner set duplicates a creation identity")
		}
		seenPIDs[process.PID] = struct{}{}
		seenIdentities[process.Identity] = struct{}{}
	}
	return nil
}

func linuxV2ValidateLauncherReadyLostAckSummary(
	fixture linuxV2NativeFixture,
	marker linuxV2OwnerWitnessStageMarker,
	response linuxV2OwnerWitnessResponse,
) error {
	if response.Outcome != "unresolved" || response.FailureCategory != "response_lost" ||
		response.ResponseLostAfter != "launcher-ready" || !response.HelperDropObserved ||
		response.HelperDropOperation != "launcher-ready" || response.NoTargetAbort || response.JournalUnproven ||
		response.RecoveryState != "released" && response.RecoveryState != "pre_gate_released" ||
		response.NativeReleaseState != "not_released" || response.Stages.NativeReleased ||
		response.Target == nil || response.Target.PID <= 1 || response.Target.Identity == "" || linuxV2MarkerHasBPeerClaims(*response.Target) {
		return errors.New("launcher-ready loss summary did not preserve raw gate identity without B RELEASE claims")
	}
	if response.Stop == nil || response.Stop.ReceiptVersion != authority.SupervisorVersionV2 || response.Stop.ReceiptStatus != "stopped" ||
		response.Stop.ProofKind == "" || response.Stop.Issuer == nil || response.Stop.Issuer.PID <= 1 || response.Stop.Issuer.Identity == "" ||
		response.Stop.Reaper == nil || response.Stop.Reaper.PID <= 1 || response.Stop.Reaper.Identity == "" ||
		!response.Stop.KillAcknowledged || !response.Stop.PopulatedZero || !response.Stop.NoMigrationObserved ||
		!response.Stop.ProducerFenced || !response.Stop.DescendantsReaped {
		return errors.New("launcher-ready loss summary lacks its exact v2 physical stop receipt")
	}
	if response.Release == nil || response.Release.Version != authority.SupervisorHandoffVersionV2 ||
		!response.Release.OpaquePlatformRelease || !response.Release.LeafRemoved || !response.Release.ClearPersisted ||
		!response.Stages.StopReceiptDurable || !response.Stages.LeafRemoved || !response.Stages.JournalCleared {
		return errors.New("launcher-ready loss summary lacks exact receipt→opaque leaf removal→clear evidence")
	}
	if response.Clone != nil && (response.Clone.CheckoutReady || response.Clone.CheckoutDevice != 0 || response.Clone.CheckoutInode != 0) {
		return errors.New("launcher-ready loss summary fabricated a B checkout before native RELEASE")
	}
	gate := response.Owners.Gate
	if gate == nil || gate.PID != response.Target.PID || gate.Identity != response.Target.Identity {
		return errors.New("launcher-ready loss summary did not preserve its exact raw A gate identity")
	}
	if marker.Owners.Gate != nil && (marker.Owners.Gate.PID != gate.PID || marker.Owners.Gate.Identity != gate.Identity) {
		return errors.New("launcher-ready status changed the already-bound gate identity")
	}
	if fixture.request.Barrier.HoldAfter != "launcher_bound" {
		return errors.New("launcher-ready loss is not bound to the exact launcher_bound request boundary")
	}
	return nil
}

func linuxV2OpenStageOwnerPIDFD(process *linuxV2OwnerWitnessProcess, owner linuxgate.OwnerConfig) (*os.File, error) {
	if process == nil || process.PID <= 0 || process.Identity == "" {
		return nil, errors.New("stage marker omitted the exact A owner identity")
	}
	credentials, err := linuxV2ReadProcessCredentials(process.PID)
	if err != nil || credentials.effectiveUID != owner.AUID || credentials.effectiveGID != owner.AGID {
		return nil, errors.New("stage owner does not have exact A credentials")
	}
	return linuxV2OpenExactPIDFD(process.PID, process.Identity)
}

func linuxV2VerifyStageReplayCheckout(fixture linuxV2NativeFixture, receipt authority.LinuxCloneCheckoutReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if receipt.RunID != fixture.request.RunKey.RunID || receipt.Generation != fixture.request.RunKey.Generation ||
		receipt.ResourceID != fixture.request.Subject.ResourceID || receipt.BaseCommit != fixture.request.Subject.Commit {
		return errors.New("Store checkout receipt changed the exact RunKey/Subject")
	}
	if !linuxV2PathWithin(receipt.Target, fixture.workspace.Root) {
		return errors.New("Store checkout target escaped the fixed B clone Root")
	}
	info, err := os.Lstat(receipt.Target)
	if err != nil || !info.IsDir() {
		return errors.New("B checkout target is unavailable at checkout_ready")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != fixture.owner.BUID || uint64(stat.Dev) != receipt.TargetDevice || stat.Ino != receipt.TargetInode {
		return errors.New("checkout_ready target does not match exact B-owned Store device/inode CAS")
	}
	return nil
}

func runProductionLinuxV2StageCrashReplay(t *testing.T, stage string) (linuxV2NativeFixture, string) {
	t.Helper()
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: stage, markerDirectory: "stage-replay-" + stage,
	}
	return runProductionLinuxV2StageCrashReplayCase(t, stage, testCase)
}

func runProductionLinuxV2StageCrashReplayCase(
	t *testing.T,
	stage string,
	testCase linuxV2NativeWitnessCase,
) (linuxV2NativeFixture, string) {
	t.Helper()
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted stage-replay witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("stage replay requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact durable stage %s: %v", stage, err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, stage)
	if err != nil {
		t.Fatalf("read exact stage-replay marker: %v", err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact RunJournal at stage %s: found=%t error=%v", stage, found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, stage, marker, initial.Journal); err != nil {
		t.Fatalf("stage %s did not expose its exact persisted boundary: %v", stage, err)
	}
	var normalStop *platform.LinuxOwnerHelperV2StopEvidence
	if stage == "stop_receipt_durable" {
		normalStop, err = linuxV2CaptureNormalAnchorStopEvidence(fixture, initial.Journal)
		if err != nil {
			t.Fatalf("read exact helper Status StopEvidence before normal receipt replay: %v", err)
		}
		witness.AnchorStop = linuxV2BuildAnchorStopSnapshot(*normalStop.AnchorStop)
	}
	if initial.Journal.ContainmentCgroup != nil && initial.Journal.ContainmentCgroup.LeafIdentity != nil {
		witness.Leaf = linuxV2BuildLeafSnapshot(*initial.Journal.ContainmentCgroup.LeafIdentity)
	}
	checkoutBefore := initial.Journal.LinuxCloneCheckoutReceipt
	if stage == "checkout_ready" {
		if checkoutBefore == nil || checkoutBefore.Validate() != nil || initial.Journal.LinuxCloneIntent == nil ||
			initial.Journal.LinuxCloneSeedReceipt == nil || checkoutBefore.SeedDigest != initial.Journal.LinuxCloneSeedReceipt.Digest {
			t.Fatal("checkout_ready marker lacks its exact Store checkout CAS and seed receipt")
		}
		if err := linuxV2VerifyStageReplayCheckout(fixture, *checkoutBefore); err != nil {
			t.Fatalf("verify exact B-owned checkout at its Store CAS: %v", err)
		}
		witness.Clone = linuxV2BuildCloneSnapshot(*checkoutBefore, fixture.owner.BUID)
	}
	var anchorPIDFD, helperPIDFD, targetPIDFD *os.File
	defer func() {
		for _, file := range []*os.File{anchorPIDFD, helperPIDFD, targetPIDFD} {
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	ownerWasExpectedLive := stage != "leaf_removed" && stage != "journal_cleared"
	if ownerWasExpectedLive {
		anchorPIDFD, err = linuxV2OpenStageOwnerPIDFD(marker.Owners.Anchor, fixture.owner)
		if err != nil {
			t.Fatalf("pin exact live A anchor at stage %s: %v", stage, err)
		}
		if stage == "helper_bound" || stage == "launcher_bound" || stage == "gate_bound" || stage == "handoff_prepared" ||
			stage == "handoff_committed" || stage == "lease_armed" || stage == "checkout_ready" ||
			stage == "native_release" || stage == "stop_receipt_durable" {
			helperPIDFD, err = linuxV2OpenStageOwnerPIDFD(marker.Owners.Helper, fixture.owner)
			if err != nil {
				t.Fatalf("pin exact live A helper at stage %s: %v", stage, err)
			}
		}
	}
	if stage == "native_release" {
		if marker.Target.PID <= 0 || marker.Target.Identity == "" || !marker.Target.PIDFDVerified ||
			marker.Target.HostUID != fixture.owner.BUID || marker.Target.UserNamespace == nil ||
			marker.Target.UserNamespace.Device == 0 || marker.Target.UserNamespace.Inode == 0 ||
			marker.Target.NetworkNamespace == nil || marker.Target.NetworkNamespace.Device == 0 || marker.Target.NetworkNamespace.Inode == 0 {
			t.Fatal("native_release marker lacks the exact launch-transferred B PIDFD and namespace identities")
		}
		live, ready, captureErr := linuxV2CaptureLiveProcess(initial.Journal, fixture, fixture.request.Harness)
		if captureErr != nil || !ready || live.PID != marker.Target.PID || live.Identity != marker.Target.Identity {
			if live.pidfd != nil {
				_ = live.pidfd.Close()
			}
			t.Fatalf("external observer did not bind the native_release target to the exact B journal: ready=%t error=%v", ready, captureErr)
		}
		witness.Process = &live
		if !live.NoWritableCgroupFDs {
			t.Fatal("native target retained a writable descriptor on a cgroup-v2 mount")
		}
		witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
		witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
		witness.Owners = &live.owners
		witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS = live.postCheckoutAbsentAtCheckoutCAS
		targetPIDFD = live.pidfd
	}
	if err := linuxV2KillOwnerWitnessCommand(command); err != nil {
		t.Fatalf("SIGKILL exact stage-replay App process: %v", err)
	}
	for _, owner := range []*os.File{anchorPIDFD, helperPIDFD} {
		if owner != nil {
			if err := linuxV2RequireLivePIDFD(int(owner.Fd())); err != nil {
				t.Fatalf("A owner exited with the SIGKILLed stage-replay process: %v", err)
			}
		}
	}
	if testCase.afterStageProcessKilled != nil {
		testCase.afterStageProcessKilled(t, fixture, binary)
	}
	command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for same-RunKey production recovery after %s: %v", stage, err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read stage-replay recovery response: %v", err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, response.Outcome)
	witness.Response = &response
	witness.Outcome = response.Outcome
	witness.FailureCategory = response.FailureCategory
	witness.RecoveryState = response.RecoveryState
	witness.JournalUnproven = response.JournalUnproven
	witness.NoTargetAbort = response.NoTargetAbort
	if err := linuxV2ValidateStageReplayResponse(fixture, stage, response); err != nil {
		t.Fatalf("stage %s recovery response failed exact-state assertions: %v", stage, err)
	}
	final, finalFound, err := linuxV2ValidateStageReplayJournal(fixture, response)
	if err != nil {
		t.Fatalf("stage %s recovery did not preserve the correct physical Journal state: %v", stage, err)
	}
	if normalStop != nil {
		initialReceipt := initial.Journal.ContainmentAuthority.StopReceipt
		finalReceipt := final.Journal.ContainmentCgroupReleaseProof.StopReceipt
		if initialReceipt == nil || finalReceipt == nil || !initialReceipt.Equal(*finalReceipt) ||
			initialReceipt.LinuxProof == nil || !initialReceipt.LinuxProof.Equal(normalStop.Proof) {
			t.Fatal("same-RunKey release tombstone changed the exact normal Anchor StopLeaf-backed StopReceipt")
		}
	}
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, finalFound)
	if witness.Journal != nil && response.JournalUnproven {
		witness.Journal.NoUnprovenContainment = false
	}
	witness.HealthPeerOK = response.Health != nil && response.Health.Healthy && response.Health.PeerVerified && response.Health.NetworkNamespaceVerified
	witness.LeafAbsent = response.Release != nil && response.Release.LeafRemoved
	if witness.Clone == nil {
		witness.Clone = linuxV2CloneSnapshotFromResponse(response.Clone)
	}
	if witness.Leaf == nil {
		witness.Leaf = linuxV2LeafSnapshotFromResponse(response.Leaf)
	}
	if stage == "checkout_ready" {
		if !finalFound || final.Journal.LinuxCloneCheckoutReceipt == nil || checkoutBefore == nil ||
			!final.Journal.LinuxCloneCheckoutReceipt.Equal(*checkoutBefore) || response.Clone == nil ||
			response.Clone.CheckoutDevice != checkoutBefore.TargetDevice || response.Clone.CheckoutInode != checkoutBefore.TargetInode {
			t.Fatal("same-RunKey recovery changed or replayed the durable CheckoutReady Store CAS")
		}
	}
	if targetPIDFD != nil {
		if response.RecoveryState == "unresolved" {
			if err := linuxV2RequireLivePIDFD(int(targetPIDFD.Fd())); err != nil {
				t.Fatalf("unresolved recovery lost its exact live target PIDFD while retaining authority: %v", err)
			}
		} else if err := linuxV2WaitPIDFDExited(int(targetPIDFD.Fd()), 30*time.Second); err != nil {
			t.Fatalf("stage recovery did not stop its exact native target: %v", err)
		}
	}
	if response.RecoveryState == "unresolved" {
		if fixture.keepRoot != nil {
			*fixture.keepRoot = true
		}
		t.Logf("unresolved stage-replay RunJournal retained at fixture StateDir %s", fixture.stateDir)
	}
	return fixture, binary
}

func linuxV2CaptureNormalAnchorStopEvidence(
	fixture linuxV2NativeFixture,
	journal state.RunJournal,
) (*platform.LinuxOwnerHelperV2StopEvidence, error) {
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	reservation := journal.ContainmentCgroup
	authorityValue := journal.ContainmentAuthority
	if journal.Key() != key || journal.Fence() != fixture.request.Fence || reservation == nil ||
		reservation.Validate() != nil || reservation.Helper == nil || reservation.Launcher == nil || reservation.Gate == nil ||
		authorityValue == nil || authorityValue.StopReceipt == nil || authorityValue.StopReceipt.LinuxProof == nil ||
		authorityValue.StopReceipt.ValidFor(*authorityValue) == false {
		return nil, errors.New("stop_receipt_durable RunJournal lacks its exact reservation, authority, or valid receipt")
	}
	grant, err := linuxV2OwnerAnchorGrantFromJournal(journal, reservation.Clone())
	if err != nil {
		return nil, fmt.Errorf("build exact RunJournal anchor grant: %w", err)
	}
	client, err := platform.NewLinuxOwnerAnchorClient(fixture.value.LinuxOwnerSocket)
	if err != nil {
		return nil, fmt.Errorf("connect to exact A owner anchor: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	helper, err := client.LookupHelper(ctx, grant)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("lookup exact helper before anchor retirement: %w", err)
	}
	defer helper.Close()
	if helper.PID != reservation.Helper.PID || helper.Identity != reservation.Helper.Identity || helper.PIDFD == nil ||
		helper.Control == nil || linuxV2RequireLivePIDFD(int(helper.PIDFD.Fd())) != nil {
		cancel()
		return nil, errors.New("normal stop receipt no longer has its exact live helper status source")
	}
	status, statusErr := helper.Control.Status(ctx)
	cancel()
	if statusErr != nil {
		return nil, fmt.Errorf("read authenticated helper Status: %w", statusErr)
	}
	defer status.Close()
	if status.RunKey != grant.RunKey || status.Fence != grant.Fence || !status.Helper.Equal(*reservation.Helper) ||
		status.OwnerLost || status.ErrorCode != "" || status.StopEvidence == nil {
		return nil, errors.New("helper Status lacks exact RunKey/fence or authenticated normal StopEvidence")
	}
	evidence := status.StopEvidence
	if err := evidence.ValidateFor(grant, *reservation.Helper, *reservation.Launcher, *reservation.Gate); err != nil {
		return nil, fmt.Errorf("validate exact helper Status StopEvidence: %w", err)
	}
	anchorStop := evidence.AnchorStop
	if anchorStop == nil || anchorStop.HelperExit != nil || anchorStop.Gate == nil ||
		!anchorStop.Gate.Equal(*reservation.Gate) || anchorStop.ProducerFence == nil ||
		!anchorStop.ProducerFence.LeafMembershipVerified || !anchorStop.GateReleaseAuthorized ||
		anchorStop.ValidateFor(grant, *reservation.Helper, reservation.Anchor, nil, false) != nil ||
		!evidence.Proof.Equal(*authorityValue.StopReceipt.LinuxProof) {
		return nil, errors.New("normal StopReceipt differs from the exact cached Anchor StopLeaf result")
	}
	return evidence, nil
}

type linuxV2StoreJournalOwnerState struct {
	RunID                   string
	Generation              int64
	LocalState              string
	TerminalState           string
	TerminalTaskResultKind  string
	HasProcessDetails       bool
	HasCgroupReservation    bool
	HasProcessAuthority     bool
	HasProcessMarker        bool
	HasPendingContainment   bool
	ContainmentUnproven     bool
	RetainWorkspace         bool
	WorkspaceRecoveryNeeded bool
	TargetPID               int
	TargetIdentitySHA256    string
	HelperPID               int
	HelperIdentitySHA256    string
	AnchorPID               int
	AnchorIdentitySHA256    string
	GatePID                 int
	GateIdentitySHA256      string
	LeafDevice              uint64
	LeafInode               uint64
	HasValidV2ReleaseProof  bool
	ReleaseProofVersion     int
	ReleaseDisposition      string
}

type linuxV2StoreJournalSnapshot struct {
	raw     []byte
	owner   linuxV2StoreJournalOwnerState
	journal state.RunJournal
}

func linuxV2SnapshotStoreJournals(stateDir string) (map[state.RunKey]linuxV2StoreJournalSnapshot, error) {
	files, err := readLinuxWitnessJournals(stateDir)
	if err != nil {
		return nil, err
	}
	result := make(map[state.RunKey]linuxV2StoreJournalSnapshot, len(files))
	for _, file := range files {
		key := file.Journal.Key()
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("StateDir contains duplicate RunJournal key %s/%d", key.RunID, key.Generation)
		}
		result[key] = linuxV2StoreJournalSnapshot{
			raw: append([]byte(nil), file.Raw...), owner: linuxV2StoreJournalOwnerStateFromJournal(file.Journal), journal: file.Journal,
		}
	}
	return result, nil
}

func linuxV2StoreJournalOwnerStateFromJournal(journal state.RunJournal) linuxV2StoreJournalOwnerState {
	state := linuxV2StoreJournalOwnerState{
		RunID: journal.RunID, Generation: journal.Generation, LocalState: journal.LocalState,
		TerminalState: journal.TerminalState, TerminalTaskResultKind: string(journal.TerminalTaskResultKind),
		HasProcessDetails: journal.HasProcessDetails(), HasPendingContainment: journal.HasPendingContainment(),
		HasCgroupReservation: journal.ContainmentCgroup != nil,
		HasProcessAuthority:  journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil,
		HasProcessMarker:     journal.HasProcessDetails(), ContainmentUnproven: journal.ContainmentUnproven,
		RetainWorkspace: journal.RetainWorkspace, WorkspaceRecoveryNeeded: journal.WorkspaceRecoveryRequired,
	}
	targetPID, targetIdentity, helperPID, helperIdentity := linuxWitnessOwnerIDs(journal)
	state.TargetPID, state.TargetIdentitySHA256 = targetPID, linuxWitnessHashBytes([]byte(targetIdentity))
	state.HelperPID, state.HelperIdentitySHA256 = helperPID, linuxWitnessHashBytes([]byte(helperIdentity))
	if reservation := journal.ContainmentCgroup; reservation != nil {
		state.AnchorPID = reservation.Anchor.PID
		state.AnchorIdentitySHA256 = linuxWitnessHashBytes([]byte(reservation.Anchor.Identity))
		if reservation.Gate != nil {
			state.GatePID = reservation.Gate.Process.PID
			state.GateIdentitySHA256 = linuxWitnessHashBytes([]byte(reservation.Gate.Process.Identity))
		}
		if reservation.LeafIdentity != nil {
			state.LeafDevice, state.LeafInode = reservation.LeafIdentity.LeafDevice, reservation.LeafIdentity.LeafInode
		}
	}
	if proof := journal.ContainmentCgroupReleaseProof; proof != nil {
		state.ReleaseProofVersion, state.ReleaseDisposition = proof.Version, proof.LeafDisposition
		state.HasValidV2ReleaseProof = proof.Validate() == nil && proof.Version == authority.SupervisorHandoffVersionV2 &&
			proof.StopReceipt != nil && proof.StopReceipt.Version == authority.SupervisorVersionV2 && proof.StopReceipt.LinuxProof != nil
		if proof.CgroupIdentity != nil {
			state.LeafDevice, state.LeafInode = proof.CgroupIdentity.LeafDevice, proof.CgroupIdentity.LeafInode
		}
	}
	return state
}

func (snapshot linuxV2StoreJournalOwnerState) hasUnresolvedOwner() bool {
	return snapshot.HasProcessDetails || snapshot.HasCgroupReservation || snapshot.HasProcessAuthority ||
		snapshot.HasPendingContainment || snapshot.ContainmentUnproven
}

func (snapshot linuxV2StoreJournalOwnerState) isStoppedRetained() bool {
	// The owner witness proves physical release without issuing a task terminal
	// transition. Retaining that checkout must not invent one to admit the next run.
	return !snapshot.hasUnresolvedOwner() && snapshot.HasValidV2ReleaseProof
}

func TestLinuxV2PriorJournalRequiresPhysicalReleaseNotTerminalTransition(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot linuxV2StoreJournalOwnerState
		want     bool
	}{
		{
			name: "released nonterminal checkout remains retained",
			snapshot: linuxV2StoreJournalOwnerState{
				LocalState: "claimed", RetainWorkspace: true, WorkspaceRecoveryNeeded: true,
				HasValidV2ReleaseProof: true,
			},
			want: true,
		},
		{
			name: "released terminal checkout remains retained",
			snapshot: linuxV2StoreJournalOwnerState{
				TerminalState: "failed", RetainWorkspace: true, HasValidV2ReleaseProof: true,
			},
			want: true,
		},
		{
			name:     "terminal transition is not physical release",
			snapshot: linuxV2StoreJournalOwnerState{TerminalState: "failed", RetainWorkspace: true},
		},
		{
			name:     "release cannot hide process details",
			snapshot: linuxV2StoreJournalOwnerState{HasValidV2ReleaseProof: true, HasProcessDetails: true},
		},
		{
			name:     "release cannot hide reservation",
			snapshot: linuxV2StoreJournalOwnerState{HasValidV2ReleaseProof: true, HasCgroupReservation: true},
		},
		{
			name:     "release cannot hide authority",
			snapshot: linuxV2StoreJournalOwnerState{HasValidV2ReleaseProof: true, HasProcessAuthority: true},
		},
		{
			name:     "release cannot hide pending containment",
			snapshot: linuxV2StoreJournalOwnerState{HasValidV2ReleaseProof: true, HasPendingContainment: true},
		},
		{
			name:     "release cannot hide unproven stop",
			snapshot: linuxV2StoreJournalOwnerState{HasValidV2ReleaseProof: true, ContainmentUnproven: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := test.snapshot
			if got := test.snapshot.isStoppedRetained(); got != test.want {
				t.Fatalf("prior journal physically released = %t, want %t", got, test.want)
			}
			if test.snapshot != before {
				t.Fatal("classifying a retained journal changed its state")
			}
		})
	}
}

func linuxV2RequireSafelyReleasedPriorJournal(value config.Config, snapshot linuxV2StoreJournalSnapshot) error {
	journal := snapshot.journal
	if !snapshot.owner.isStoppedRetained() || journal.HasProcessDetails() || journal.HasPendingContainment() ||
		journal.ContainmentUnproven || journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentHandoff != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
		return errors.New("prior RunJournal may still own or require recovery of a process or leaf")
	}
	proof := journal.ContainmentCgroupReleaseProof
	if proof == nil || proof.Validate() != nil || proof.Version != authority.SupervisorHandoffVersionV2 ||
		proof.StopReceipt == nil || proof.StopReceipt.Version != authority.SupervisorVersionV2 ||
		proof.CgroupIdentity == nil ||
		(proof.LeafDisposition != authority.LinuxCgroupLeafRemoved && proof.LeafDisposition != authority.LinuxCgroupLeafAbsentAfterReceipt) {
		return errors.New("prior RunJournal lacks an exact v2 StopReceipt-backed leaf release")
	}
	if _, err := os.Lstat(linuxV2LeafPath(value, *proof.CgroupIdentity)); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return fmt.Errorf("inspect prior RunJournal's exact released cgroup leaf: %w", err)
		}
		return errors.New("prior RunJournal's exact cgroup leaf still exists")
	}
	return nil
}

func linuxV2RequireSameStoreJournalSnapshot(before, after map[state.RunKey]linuxV2StoreJournalSnapshot) error {
	return linuxV2RequireStoreJournalHistoryPreserved(before, after, nil)
}

func linuxV2RequireStoreJournalHistoryPreserved(
	before, after map[state.RunKey]linuxV2StoreJournalSnapshot,
	allowedNew *state.RunKey,
) error {
	for key, oldSnapshot := range before {
		newSnapshot, found := after[key]
		if !found {
			return fmt.Errorf("shared StateDir lost retained RunJournal %s/%d", key.RunID, key.Generation)
		}
		if !bytes.Equal(oldSnapshot.raw, newSnapshot.raw) || oldSnapshot.owner != newSnapshot.owner {
			return fmt.Errorf("shared StateDir changed retained RunJournal %s/%d", key.RunID, key.Generation)
		}
	}
	for key := range after {
		if _, existed := before[key]; existed {
			continue
		}
		if allowedNew == nil || key != *allowedNew {
			return fmt.Errorf("shared StateDir added unrelated RunJournal %s/%d", key.RunID, key.Generation)
		}
	}
	return nil
}

type linuxV2PIDReuseLeafRequest struct {
	MountID      uint64 `json:"mount_id"`
	ParentDevice uint64 `json:"parent_device"`
	ParentInode  uint64 `json:"parent_inode"`
	ParentPath   string `json:"parent_path"`
	Name         string `json:"name"`
	Device       uint64 `json:"device"`
	Inode        uint64 `json:"inode"`
}

type linuxV2PIDReuseHelperRequest struct {
	Version            int                             `json:"version"`
	Operation          string                          `json:"op"`
	Case               string                          `json:"case"`
	RunKey             linuxV2WitnessRunKey            `json:"run_key"`
	Fence              linuxV2OwnerWitnessFenceSummary `json:"fence"`
	DeadTargetPID      int                             `json:"dead_target_pid"`
	OldIdentitySHA256  string                          `json:"old_identity_sha256"`
	BootID             string                          `json:"boot_id"`
	PIDNamespaceDevice uint64                          `json:"pid_namespace_device"`
	PIDNamespaceInode  uint64                          `json:"pid_namespace_inode"`
	Leaf               linuxV2PIDReuseLeafRequest      `json:"leaf"`
}

type linuxV2PIDReuseHelperResponse struct {
	Version                   int                  `json:"version"`
	Operation                 string               `json:"op"`
	Case                      string               `json:"case"`
	RunKey                    linuxV2WitnessRunKey `json:"run_key"`
	Status                    string               `json:"status"`
	Stage                     string               `json:"stage"`
	Errno                     string               `json:"errno,omitempty"`
	Refusal                   string               `json:"refusal,omitempty"`
	DeadTargetPID             int                  `json:"dead_target_pid"`
	OldIdentitySHA256         string               `json:"old_identity_sha256"`
	ReplacementPID            int                  `json:"replacement_pid"`
	ReplacementIdentitySHA256 string               `json:"replacement_identity_sha256"`
	BootID                    string               `json:"boot_id"`
	PIDNamespaceDevice        uint64               `json:"pid_namespace_device"`
	PIDNamespaceInode         uint64               `json:"pid_namespace_inode"`
	PIDFDVerified             bool                 `json:"pidfd_verified"`
	PIDFDAlive                bool                 `json:"pidfd_alive"`
	OutsideOriginalLeaf       bool                 `json:"outside_original_leaf"`
	SeparateServiceCgroup     bool                 `json:"separate_service_cgroup"`
	ServiceCgroupPath         string               `json:"service_cgroup_path"`
}

type linuxV2PIDReuseCleanupRequest struct {
	Version                   int                  `json:"version"`
	Operation                 string               `json:"op"`
	Case                      string               `json:"case"`
	RunKey                    linuxV2WitnessRunKey `json:"run_key"`
	ReplacementPID            int                  `json:"replacement_pid,omitempty"`
	ReplacementIdentitySHA256 string               `json:"replacement_identity_sha256,omitempty"`
}

type linuxV2PIDReuseCleanupResponse struct {
	Version                   int                  `json:"version"`
	Operation                 string               `json:"op"`
	Case                      string               `json:"case"`
	RunKey                    linuxV2WitnessRunKey `json:"run_key"`
	ReplacementPID            int                  `json:"replacement_pid"`
	ReplacementIdentitySHA256 string               `json:"replacement_identity_sha256,omitempty"`
	Status                    string               `json:"status"`
	PIDFDVerified             bool                 `json:"pidfd_verified"`
}

type linuxV2PIDReuseCleanupAcknowledgement struct {
	Version   int                  `json:"version"`
	Operation string               `json:"op"`
	Case      string               `json:"case"`
	RunKey    linuxV2WitnessRunKey `json:"run_key"`
	Status    string               `json:"status"`
}

type linuxV2PIDReuseOldProcess struct {
	marker linuxV2OwnerWitnessStageMarker
	pidfd  *os.File
	bootID string
	nsDev  uint64
	nsIno  uint64
	start  uint64
}

type linuxV2PIDReuseOldProcessResult struct {
	process linuxV2PIDReuseOldProcess
	err     error
}

type linuxV2LinuxIdentityParts struct {
	bootID string
	nsIno  uint64
	pid    int
	start  uint64
}

func runProductionLinuxV2LiteralPIDReuse(t *testing.T, caseRoot string) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot || !linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("literal PID-reuse request/config paths do not bind the exact fixture beneath %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "leaf_removed", markerDirectory: "literal-pid-reuse",
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted literal PID-reuse witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("literal PID-reuse witness requires the active A systemd owner anchor: %v", err)
	}
	exchangeDirectory, watcherCgroup, err := linuxV2OpenPIDReuseExchange(fixture.owner)
	if err != nil {
		t.Fatalf("verify root PID-reuse helper and watcher prerequisites: %v", err)
	}
	defer exchangeDirectory.Close()
	delegatedCgroupRoot := filepath.Clean("/" + strings.TrimLeft(filepath.ToSlash(fixture.value.LinuxOwnerDelegatedPath), "/"))
	if delegatedCgroupRoot == "/" || linuxV2CgroupPathWithin(watcherCgroup, delegatedCgroupRoot) {
		t.Fatalf("root PID-reuse watcher cgroup %q is not separate from delegated owner subtree %q", watcherCgroup, delegatedCgroupRoot)
	}
	if err := linuxV2RequireEmptyPIDReuseExchange(exchangeDirectory); err != nil {
		t.Fatalf("PID-reuse helper exchange is not clean for this one-shot witness: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	oldProcessResult := make(chan linuxV2PIDReuseOldProcessResult, 1)
	stopGateWatcher := make(chan struct{})
	gateWatcherDone := make(chan struct{})
	var command *linuxV2OwnerWitnessCommand
	barrierReleased := false
	var oldProcess *linuxV2PIDReuseOldProcess
	var replacementPIDFD *os.File
	requestWritten := false
	cleanupComplete := false
	cleanupRequested := false
	var helperResponse linuxV2PIDReuseHelperResponse
	go func() {
		defer close(gateWatcherDone)
		old, captureErr := linuxV2WaitForPIDReuseNativePIDFD(fixture, 30*time.Second, stopGateWatcher)
		oldProcessResult <- linuxV2PIDReuseOldProcessResult{process: old, err: captureErr}
	}()
	defer func() {
		close(stopGateWatcher)
		if !barrierReleased && command != nil {
			if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
				t.Errorf("release held leaf_removed barrier during PID-reuse cleanup: %v", err)
			}
			barrierReleased = true
		}
		if command != nil && command.process != nil && !command.process.waited {
			if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
				t.Errorf("finish production owner-witness command during PID-reuse cleanup: %v", err)
			}
		}
		if requestWritten && !cleanupComplete {
			if err := linuxV2CleanupPIDReuseReplacement(exchangeDirectory, fixture, helperResponse, &cleanupRequested); err != nil {
				t.Errorf("kill and reap root PID-reuse replacement during cleanup: %v", err)
			}
		}
		if replacementPIDFD != nil {
			_ = replacementPIDFD.Close()
		}
		if oldProcess != nil && oldProcess.pidfd != nil {
			_ = oldProcess.pidfd.Close()
		} else {
			<-gateWatcherDone
			select {
			case result := <-oldProcessResult:
				if result.process.pidfd != nil {
					_ = result.process.pidfd.Close()
				}
			default:
			}
		}
	}()
	command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	aPIDNSDevice, aPIDNSInode, err := linuxV2ReadPIDNamespaceIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("read A guest PID namespace before production owner run: %v", err)
	}
	appPIDNSDevice, appPIDNSInode, err := linuxV2ReadPIDNamespaceIdentity(command.process.command.Process.Pid)
	if err != nil || appPIDNSDevice != aPIDNSDevice || appPIDNSInode != aPIDNSInode {
		t.Fatalf("production App and A do not share the exact guest PID namespace: %v", err)
	}
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact leaf_removed stop-receipt barrier: %v", err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "leaf_removed")
	if err != nil {
		t.Fatalf("read exact leaf_removed marker: %v", err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact leaf_removed RunJournal: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, "leaf_removed", marker, initial.Journal); err != nil {
		t.Fatalf("leaf_removed did not preserve its exact stop receipt and pending Store clear: %v", err)
	}
	if initial.Journal.ContainmentAuthority == nil || initial.Journal.ContainmentAuthority.StopReceipt == nil ||
		initial.Journal.ContainmentAuthority.StopReceipt.Version != authority.SupervisorVersionV2 ||
		initial.Journal.ContainmentAuthority.StopReceipt.Status != "stopped" ||
		!initial.Journal.ContainmentAuthority.StopReceipt.ValidFor(*initial.Journal.ContainmentAuthority) ||
		initial.Journal.ContainmentAuthority.StopReceipt.TargetPID <= 0 || initial.Journal.ContainmentAuthority.StopReceipt.TargetIdentity == "" {
		t.Fatal("leaf_removed lacks the exact durable v2 stop receipt for its former B gate")
	}
	if initial.Journal.ContainmentCgroup == nil || initial.Journal.ContainmentCgroup.LeafIdentity == nil ||
		initial.Journal.ContainmentAuthority.StopReceipt.TargetPID != marker.Target.PID ||
		initial.Journal.ContainmentAuthority.StopReceipt.TargetIdentity != marker.Target.Identity {
		t.Fatal("leaf_removed stop receipt does not bind the exact former B gate and pinned leaf")
	}
	select {
	case result := <-oldProcessResult:
		if result.err != nil {
			t.Fatalf("capture exact old gate pidfd before B RELEASE: %v", result.err)
		}
		oldProcess = &result.process
	case <-time.After(30 * time.Second):
		t.Fatal("exact native-release pidfd watcher did not capture the B process before stop")
	}
	oldIdentity := oldProcess.marker.Target.Identity
	oldPID := oldProcess.marker.Target.PID
	if oldPID != marker.Target.PID || oldIdentity != marker.Target.Identity || oldProcess.marker.Stage != "native_release" ||
		oldProcess.marker.HoldAfter != "leaf_removed" ||
		oldProcess.marker.RunKey != fixture.request.RunKey {
		t.Fatal("captured native-release pidfd does not match the exact released B target and requested RunKey")
	}
	if oldProcess.nsDev != aPIDNSDevice || oldProcess.nsIno != aPIDNSInode {
		t.Fatalf("A gate and native B process do not share the exact guest PID namespace: %v", err)
	}
	if err := linuxV2WaitPIDFDExited(int(oldProcess.pidfd.Fd()), 20*time.Second); err != nil {
		t.Fatalf("old exact B gate pidfd did not become exited before PID reuse: %v", err)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", oldPID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old guest PID %d is not absent after its gate pidfd exited and before clone3 reuse: %v", oldPID, err)
	}
	leafIdentity := *initial.Journal.ContainmentCgroup.LeafIdentity
	checkout := initial.Journal.LinuxCloneCheckoutReceipt
	if checkout == nil || checkout.Validate() != nil {
		t.Fatal("leaf_removed receipt lacks the exact durable B Store checkout CAS")
	}
	if err := linuxV2VerifyStageReplayCheckout(fixture, *checkout); err != nil {
		t.Fatalf("leaf_removed checkout receipt changed its exact B-owned Store CAS: %v", err)
	}
	oldParts, err := linuxV2ParseLinuxIdentity(oldIdentity)
	if err != nil || oldParts.pid != oldPID || oldParts.bootID != oldProcess.bootID || oldParts.nsIno != oldProcess.nsIno {
		t.Fatalf("exact old gate identity does not match the measured guest PID/boot/PID namespace: %v", err)
	}
	request := linuxV2PIDReuseHelperRequest{
		Version: 1, Operation: "create", Case: "literal_pid_reuse", RunKey: fixture.request.RunKey,
		Fence: linuxV2OwnerWitnessFenceSummary{
			RuntimeID: fixture.request.Fence.RuntimeID, RuntimeEpoch: fixture.request.Fence.RuntimeEpoch,
			Generation: fixture.request.Fence.Generation, ClaimID: fixture.request.Fence.ClaimID,
		},
		DeadTargetPID: oldPID, OldIdentitySHA256: linuxWitnessHashBytes([]byte(oldIdentity)),
		BootID: oldProcess.bootID, PIDNamespaceDevice: oldProcess.nsDev, PIDNamespaceInode: oldProcess.nsIno,
		Leaf: linuxV2PIDReuseLeafRequest{
			MountID: leafIdentity.Parent.MountID, ParentDevice: leafIdentity.Parent.Device, ParentInode: leafIdentity.Parent.Inode,
			ParentPath: leafIdentity.Parent.Path, Name: leafIdentity.LeafName, Device: leafIdentity.LeafDevice, Inode: leafIdentity.LeafInode,
		},
	}
	requestWritten = true
	if err := linuxV2WritePIDReuseExchangeJSON(exchangeDirectory, "request.json", request, fixture.owner.AUID, fixture.owner.AGID); err != nil {
		t.Fatalf("publish exact A-owned literal-PID request after durable stop: %v", err)
	}
	helperResponse, err = linuxV2WaitPIDReuseHelperResponse(exchangeDirectory, fixture.owner.AGID, 30*time.Second)
	if err != nil {
		t.Fatalf("root clone3 PID-reuse helper did not return its response: %v", err)
	}
	if helperResponse.Version != 1 || helperResponse.Operation != "create" || helperResponse.Case != request.Case ||
		helperResponse.RunKey != request.RunKey || helperResponse.DeadTargetPID != oldPID ||
		helperResponse.OldIdentitySHA256 != request.OldIdentitySHA256 {
		t.Fatal("root PID-reuse helper response changed the exact operation, old identity hash, PID, or RunKey")
	}
	if helperResponse.Status != "created" {
		t.Fatalf("required literal PID reuse gate refused the one clone3 set_tid attempt: stage=%q errno=%q refusal=%q",
			helperResponse.Stage, helperResponse.Errno, helperResponse.Refusal)
	}
	if helperResponse.ReplacementPID != oldPID || helperResponse.BootID != oldProcess.bootID ||
		helperResponse.PIDNamespaceDevice != oldProcess.nsDev || helperResponse.PIDNamespaceInode != oldProcess.nsIno ||
		!helperResponse.PIDFDVerified || !helperResponse.PIDFDAlive || !helperResponse.OutsideOriginalLeaf || !helperResponse.SeparateServiceCgroup {
		t.Fatalf("root helper replacement did not preserve literal guest PID/namespace/boot and separate live service membership: %#v", helperResponse)
	}
	newIdentity, err := platform.ProcessIdentity(helperResponse.ReplacementPID)
	if err != nil {
		t.Fatalf("compute exact replacement process identity at its literal reused PID: %v", err)
	}
	newParts, err := linuxV2ParseLinuxIdentity(newIdentity)
	if err != nil || newParts.pid != oldPID || newParts.bootID != oldProcess.bootID || newParts.nsIno != oldProcess.nsIno || newParts.start == oldParts.start {
		t.Fatalf("replacement did not reuse the literal PID with same boot/PID namespace and a new start tick: %v", err)
	}
	newIdentityHash := linuxWitnessHashBytes([]byte(newIdentity))
	if helperResponse.ReplacementIdentitySHA256 != newIdentityHash || newIdentityHash == request.OldIdentitySHA256 {
		t.Fatal("replacement identity hash did not differ from the exact former gate identity")
	}
	replacementPIDFD, err = linuxV2OpenExactPIDFD(helperResponse.ReplacementPID, newIdentity)
	if err != nil {
		t.Fatalf("independently opened replacement pidfd did not bind the exact reused PID: %v", err)
	}
	replacementPIDFDInfoPID, replacementPIDFDErr := linuxV2PIDFDInfoPID(int(replacementPIDFD.Fd()))
	if replacementPIDFDErr != nil || replacementPIDFDInfoPID != helperResponse.ReplacementPID {
		t.Fatalf("replacement pidfd info PID = %d, want %d: %v", replacementPIDFDInfoPID, helperResponse.ReplacementPID, replacementPIDFDErr)
	}
	newNSDev, newNSIno, err := linuxV2ReadPIDNamespaceIdentity(helperResponse.ReplacementPID)
	if err != nil || newNSDev != oldProcess.nsDev || newNSIno != oldProcess.nsIno {
		t.Fatalf("replacement process moved outside the exact former guest PID namespace: %v", err)
	}
	newBootID, err := linuxV2ReadBootID()
	if err != nil || newBootID != oldProcess.bootID {
		t.Fatalf("replacement process boot ID differs from the original gate: %v", err)
	}
	credentials, err := linuxV2ReadProcessCredentials(helperResponse.ReplacementPID)
	if err != nil || credentials.realUID != 0 || credentials.effectiveUID != 0 {
		t.Fatalf("literal replacement is not the expected root-owned helper child: %v", err)
	}
	replacementCgroup, err := linuxV2ReadUnifiedCgroupPath(helperResponse.ReplacementPID)
	if err != nil || replacementCgroup != helperResponse.ServiceCgroupPath || replacementCgroup != watcherCgroup ||
		!strings.Contains(replacementCgroup, ".service") {
		t.Fatalf("replacement is not contained by the separate root helper service cgroup: path=%q helper=%q watcher=%q error=%v",
			replacementCgroup, helperResponse.ServiceCgroupPath, watcherCgroup, err)
	}
	oldLeafCgroup := filepath.Clean(filepath.Join(filepath.FromSlash(leafIdentity.Parent.Path), leafIdentity.LeafName))
	if linuxV2CgroupPathWithin(replacementCgroup, oldLeafCgroup) {
		t.Fatalf("literal PID replacement entered the former native leaf cgroup %q", oldLeafCgroup)
	}
	if err := linuxV2RequireLivePIDFD(int(replacementPIDFD.Fd())); err != nil {
		t.Fatalf("replacement pidfd is not alive before App recovery: %v", err)
	}
	if exited, err := linuxV2PIDFDExitedNow(int(oldProcess.pidfd.Fd())); err != nil || !exited {
		t.Fatalf("old gate pidfd no longer identifies its exited task after PID reuse: exited=%t error=%v", exited, err)
	}
	if err := unix.PidfdSendSignal(int(oldProcess.pidfd.Fd()), 0, nil, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("old gate pidfd signal-zero did not reject its already-exited task after PID reuse: %v", err)
	}
	staleLookup, staleLookupErr := linuxV2OpenExactPIDFD(oldPID, oldIdentity)
	if staleLookup != nil {
		_ = staleLookup.Close()
	}
	if staleLookupErr == nil {
		t.Fatal("stale old gate identity unexpectedly opened a pidfd for the same-number replacement")
	}
	witness.PIDReuse = &linuxV2PIDReuseSnapshot{
		OldPID: oldPID, ReplacementPID: helperResponse.ReplacementPID,
		OldIdentitySHA256: request.OldIdentitySHA256, ReplacementIdentitySHA256: newIdentityHash,
		OldStartTime: oldParts.start, ReplacementStartTime: newParts.start,
		BootIDSHA256:       linuxWitnessHashBytes([]byte(oldProcess.bootID)),
		PIDNamespaceDevice: oldProcess.nsDev, PIDNamespaceInode: oldProcess.nsIno,
		OldPIDFDOpened: true, OldPIDFDExitedAndReaped: true, OldPIDAbsentBeforeReplacement: true,
		OldPIDFDRemainsExited: true, ReplacementPIDFDOpened: true, ReplacementPIDFDAlive: true,
		OutsideOriginalLeaf: true, SeparateServiceCgroup: true,
	}
	witness.Owners = linuxV2OwnerSnapshotFromResponse(marker.Owners)
	witness.Leaf = linuxV2BuildLeafSnapshot(leafIdentity)
	witness.Clone = linuxV2BuildCloneSnapshot(*checkout, fixture.owner.BUID)
	witness.Journal = linuxV2StageReplayJournalSnapshot(initial, true)
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		t.Fatalf("release exact leaf_removed barrier after successful literal PID reuse: %v", err)
	}
	barrierReleased = true
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, false); err != nil {
		t.Fatalf("wait for production App to clear the old RunJournal while replacement remains live: %v", err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(fixture.responsePath, fixture.request, false)
	if err != nil {
		t.Fatalf("read exact production recovery response after literal PID reuse: %v", err)
	}
	witness.Outcome, witness.RecoveryState = response.Outcome, response.RecoveryState
	witness.FailureCategory, witness.JournalUnproven, witness.NoTargetAbort = response.FailureCategory, response.JournalUnproven, response.NoTargetAbort
	witness.Response = &response
	if err := linuxV2ValidateStageReplayResponse(fixture, "leaf_removed", response); err != nil {
		t.Fatalf("same-RunKey recovery did not clear only the exact old stopped leaf receipt: %v", err)
	}
	final, finalFound, err := linuxV2ValidateStageReplayJournal(fixture, response)
	if err != nil || !finalFound || final.Journal.ContainmentCgroup != nil || final.Journal.ContainmentAuthority != nil ||
		final.Journal.ContainmentCgroupReleaseProof == nil {
		t.Fatalf("literal PID reuse recovery did not clear the exact old RunJournal after receipt: found=%t error=%v", finalFound, err)
	}
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, true)
	if err := linuxV2RequireLivePIDFD(int(replacementPIDFD.Fd())); err != nil {
		t.Fatalf("App recovery killed or reaped the unrelated root replacement PID: %v", err)
	}
	if actual, err := platform.ProcessIdentity(oldPID); err != nil || actual != newIdentity {
		t.Fatalf("same PID no longer belongs to the exact root replacement after App recovery: %v", err)
	}
	postRecoveryCgroup, err := linuxV2ReadUnifiedCgroupPath(helperResponse.ReplacementPID)
	if err != nil || postRecoveryCgroup != watcherCgroup || linuxV2CgroupPathWithin(postRecoveryCgroup, oldLeafCgroup) {
		t.Fatalf("App recovery migrated the unrelated root replacement into the old leaf/service: path=%q error=%v",
			postRecoveryCgroup, err)
	}
	witness.PIDReuse.OutsideOriginalLeafAfterRecovery = true
	if exited, err := linuxV2PIDFDExitedNow(int(oldProcess.pidfd.Fd())); err != nil || !exited {
		t.Fatalf("old gate pidfd was rebound to the replacement after App recovery: exited=%t error=%v", exited, err)
	}
	witness.PIDReuse.ReplacementSurvivedRecovery = true
	if err := linuxV2CleanupPIDReuseReplacement(exchangeDirectory, fixture, helperResponse, &cleanupRequested); err != nil {
		t.Fatalf("root helper failed to kill and reap its own replacement by pidfd: %v", err)
	}
	cleanupRequested = false
	if err := linuxV2WaitPIDFDExited(int(replacementPIDFD.Fd()), 20*time.Second); err != nil {
		t.Fatalf("root helper cleanup did not exit the exact replacement process: %v", err)
	}
	if err := waitForLinuxWitnessIdentityGone(helperResponse.ReplacementPID, newIdentity, 5*time.Second); err != nil {
		t.Fatalf("root helper cleanup left the exact replacement process live: %v", err)
	}
	witness.PIDReuse.HelperCleanupPIDFDVerified = true
	witness.PIDReuse.HelperCleanupReaped = true
	cleanupComplete = true
}

func linuxV2OpenPIDReuseExchange(owner linuxgate.OwnerConfig) (*os.File, string, error) {
	directory, err := linuxV2OpenPIDReuseExchangeDirectory(owner)
	if err != nil {
		return nil, "", fmt.Errorf("open pinned root PID-reuse helper exchange directory: %w", err)
	}
	helperStat, err := linuxV2StatImmutablePIDReuseHelper()
	if err != nil {
		_ = directory.Close()
		return nil, "", err
	}
	unitPath := "/run/systemd/system/symmetry-ci-pid-reuse-watcher.service"
	unitInfo, err := os.Lstat(unitPath)
	if err != nil || !unitInfo.Mode().IsRegular() {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher unit file is unavailable or not regular")
	}
	unitStat, ok := unitInfo.Sys().(*syscall.Stat_t)
	if !ok || unitStat.Uid != 0 || unitStat.Gid != 0 || unitStat.Mode&0o022 != 0 {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher unit file has unsafe ownership or mode")
	}
	active := exec.Command("systemctl", "is-active", "--quiet", "symmetry-ci-pid-reuse-watcher.service")
	active.Env = linuxV2SafeHostEnvironment()
	if err := active.Run(); err != nil {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher service is not active")
	}
	mainPIDCommand := exec.Command("systemctl", "show", "--property=MainPID", "--value", "symmetry-ci-pid-reuse-watcher.service")
	mainPIDCommand.Env = linuxV2SafeHostEnvironment()
	mainPIDOutput, err := mainPIDCommand.Output()
	if err != nil {
		_ = directory.Close()
		return nil, "", errors.New("read exact root PID-reuse watcher MainPID")
	}
	mainPID, err := strconv.Atoi(strings.TrimSpace(string(mainPIDOutput)))
	if err != nil || mainPID <= 1 {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher has an invalid MainPID")
	}
	watcherExecutableInfo, err := os.Stat(fmt.Sprintf("/proc/%d/exe", mainPID))
	if err != nil {
		_ = directory.Close()
		return nil, "", fmt.Errorf("stat exact root watcher executable: %w", err)
	}
	watcherExecutableStat, ok := watcherExecutableInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(watcherExecutableStat.Dev) != uint64(helperStat.Dev) || watcherExecutableStat.Ino != helperStat.Ino {
		_ = directory.Close()
		return nil, "", errors.New("active root watcher is not executing the exact root-owned static helper file")
	}
	credentials, err := linuxV2ReadProcessCredentials(mainPID)
	if err != nil || credentials.realUID != 0 || credentials.effectiveUID != 0 {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher MainPID does not run as root")
	}
	watcherIdentity, err := platform.ProcessIdentity(mainPID)
	if err != nil {
		_ = directory.Close()
		return nil, "", errors.New("read exact root PID-reuse watcher creation identity")
	}
	watcherPIDFD, err := linuxV2OpenExactPIDFD(mainPID, watcherIdentity)
	if err != nil {
		_ = directory.Close()
		return nil, "", fmt.Errorf("pin exact root PID-reuse watcher pidfd: %w", err)
	}
	watcherLiveErr := linuxV2RequireLivePIDFD(int(watcherPIDFD.Fd()))
	closeWatcherErr := watcherPIDFD.Close()
	if watcherLiveErr != nil || closeWatcherErr != nil {
		_ = directory.Close()
		return nil, "", errors.Join(watcherLiveErr, closeWatcherErr)
	}
	watcherNSDevice, watcherNSInode, err := linuxV2ReadPIDNamespaceIdentity(mainPID)
	if err != nil {
		_ = directory.Close()
		return nil, "", fmt.Errorf("read watcher guest PID namespace: %w", err)
	}
	selfNSDevice, selfNSInode, err := linuxV2ReadPIDNamespaceIdentity(os.Getpid())
	if err != nil || watcherNSDevice != selfNSDevice || watcherNSInode != selfNSInode {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher does not share the exact A/B guest PID namespace")
	}
	watcherCgroup, err := linuxV2ReadUnifiedCgroupPath(mainPID)
	if err != nil || !strings.Contains(watcherCgroup, "symmetry-ci-pid-reuse-watcher.service") {
		_ = directory.Close()
		return nil, "", errors.New("root PID-reuse watcher is not in its separate transient service cgroup")
	}
	if err := linuxV2WaitPIDReuseWatcherReady(directory, owner.AGID, 30*time.Second); err != nil {
		_ = directory.Close()
		return nil, "", err
	}
	return directory, watcherCgroup, nil
}

func linuxV2StatImmutablePIDReuseHelper() (unix.Stat_t, error) {
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return unix.Stat_t{}, fmt.Errorf("open pinned filesystem root for static PID-reuse helper: %w", err)
	}
	defer unix.Close(rootFD)
	openDirectory := func(path string) (int, error) {
		return unix.Openat2(rootFD, path, &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
		})
	}
	for _, path := range []string{"usr", "usr/libexec", "usr/libexec/symmetry-test"} {
		fd, err := openDirectory(path)
		if err != nil {
			return unix.Stat_t{}, fmt.Errorf("open root-controlled helper directory %s: %w", path, err)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 ||
			stat.Gid != 0 || stat.Mode&0o7777 != 0o755 {
			return unix.Stat_t{}, fmt.Errorf("helper parent %s is not root:root mode 0755", path)
		}
	}
	relative, err := filepath.Rel(string(filepath.Separator), linuxV2PIDReuseHelperExecutable)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return unix.Stat_t{}, errors.New("static PID-reuse helper escaped filesystem root")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	if err != nil {
		return unix.Stat_t{}, fmt.Errorf("open immutable root PID-reuse helper: %w", err)
	}
	var stat unix.Stat_t
	statErr := unix.Fstat(fd, &stat)
	closeErr := unix.Close(fd)
	if statErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != 0 ||
		stat.Nlink != 1 || stat.Mode&0o7777 != 0o555 || stat.Size <= 0 {
		return unix.Stat_t{}, errors.New("PID-reuse helper is not a fixed root:root mode 0555 regular executable")
	}
	return stat, nil
}

func linuxV2WaitPIDReuseWatcherReady(directory *os.File, gid uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := linuxV2ReadPIDReuseExchangeFile(directory, "ready", gid)
		if err == nil {
			if string(contents) != "ready\n" {
				return errors.New("root PID-reuse watcher readiness marker has unexpected contents")
			}
			return nil
		}
		if !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("read root PID-reuse watcher readiness marker: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("root PID-reuse watcher did not restore readiness before its bounded deadline")
}

func linuxV2OpenPIDReuseExchangeDirectory(owner linuxgate.OwnerConfig) (*os.File, error) {
	path := linuxV2PIDReuseHelperDirectory
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("PID-reuse exchange directory is not canonical")
	}
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open pinned filesystem root for PID-reuse exchange: %w", err)
	}
	relative, err := filepath.Rel(string(filepath.Separator), path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = unix.Close(rootFD)
		return nil, errors.New("PID-reuse exchange directory escaped filesystem root")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	closeRootErr := unix.Close(rootFD)
	if err != nil {
		return nil, fmt.Errorf("open pinned PID-reuse exchange directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), "pid-reuse-exchange-directory")
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap pinned PID-reuse exchange directory")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 ||
		stat.Gid != owner.AGID || stat.Mode&0o7777 != 0o2730 {
		_ = directory.Close()
		return nil, errors.New("PID-reuse exchange directory is not root:A mode 02730")
	}
	if closeRootErr != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("close pinned filesystem root: %w", closeRootErr)
	}
	return directory, nil
}

func linuxV2RequireEmptyPIDReuseExchange(directory *os.File) error {
	for _, name := range []string{"request.json", "response.json", "cleanup.json", "cleanup_response.json", "cleanup_ack.json"} {
		fd, err := unix.Openat(int(directory.Fd()), name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("PID-reuse exchange contains a stale %s entry", name)
		}
		if !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("inspect PID-reuse exchange entry %s: %w", name, err)
		}
	}
	return nil
}

func linuxV2WritePIDReuseExchangeJSON(directory *os.File, name string, value any, ownerUID, ownerGID uint32) error {
	return linuxV2WritePIDReuseExchangeJSONBeforePublish(directory, name, value, ownerUID, ownerGID, nil)
}

func TestProductionLinuxV2PIDReuseExchangeAtomicPublication(t *testing.T) {
	tempDirectory := t.TempDir()
	if err := os.Chmod(tempDirectory, 0o700); err != nil {
		t.Fatalf("secure atomic-publication fixture directory: %v", err)
	}
	directory, err := os.Open(tempDirectory)
	if err != nil {
		t.Fatalf("open atomic-publication fixture directory: %v", err)
	}
	defer directory.Close()
	type request struct {
		Version int    `json:"version"`
		Case    string `json:"case"`
		Payload string `json:"payload"`
	}
	want := request{Version: 1, Case: "atomic_publish", Payload: strings.Repeat("complete", 1024)}
	ready := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	released := false
	completed := false
	defer func() {
		if !released {
			close(resume)
		}
		if !completed {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("atomic-publication writer remained blocked after cleanup release")
			}
		}
	}()
	go func() {
		done <- linuxV2WritePIDReuseExchangeJSONBeforePublish(directory, "request.json", want,
			uint32(os.Getuid()), uint32(os.Getgid()), func() error {
				close(ready)
				<-resume
				return nil
			})
	}()
	select {
	case <-ready:
	case err := <-done:
		completed = true
		t.Fatalf("writer failed before the deterministic pre-rename barrier: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not reach its complete-temp-file pre-rename barrier")
	}
	if contents, err := linuxV2ReadAOwnedPIDReuseExchangeFile(directory, "request.json", uint32(os.Getuid()), uint32(os.Getgid())); !errors.Is(err, unix.ENOENT) || contents != nil {
		t.Fatalf("consumer observed final request before atomic publication: bytes=%d error=%v", len(contents), err)
	}
	close(resume)
	released = true
	if err := <-done; err != nil {
		completed = true
		t.Fatalf("publish complete request with renameat2 no-replace: %v", err)
	}
	completed = true
	contents, err := linuxV2ReadAOwnedPIDReuseExchangeFile(directory, "request.json", uint32(os.Getuid()), uint32(os.Getgid()))
	if err != nil {
		t.Fatalf("read atomically published request: %v", err)
	}
	var got request
	if err := linuxV2DecodeStrictJSON(contents, &got); err != nil || got != want {
		t.Fatalf("published request = %#v, error = %v; want complete exact payload", got, err)
	}
}

func linuxV2WritePIDReuseExchangeJSONBeforePublish(
	directory *os.File,
	name string,
	value any,
	ownerUID uint32,
	ownerGID uint32,
	beforePublish func() error,
) error {
	if name != "request.json" && name != "cleanup.json" && name != "cleanup_ack.json" {
		return errors.New("PID-reuse publication name is not an approved final exchange file")
	}
	contents, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode PID-reuse exchange %s: %w", name, err)
	}
	contents = append(contents, '\n')
	tempName := strings.TrimSuffix(name, ".json") + "." + linuxWitnessHashBytes(contents) + ".tmp"
	fd, err := unix.Openat(int(directory.Fd()), tempName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return fmt.Errorf("create exclusive PID-reuse temporary %s: %w", tempName, err)
	}
	file := os.NewFile(uintptr(fd), tempName)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("wrap PID-reuse temporary %s", tempName)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != ownerUID ||
		stat.Gid != ownerGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 {
		_ = file.Close()
		return fmt.Errorf("PID-reuse temporary %s is not an A-owned 0600 singly-linked regular file", tempName)
	}
	removeTemp := func() error {
		checkFD, openErr := unix.Openat(int(directory.Fd()), tempName, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		var current unix.Stat_t
		statErr := unix.Fstat(checkFD, &current)
		closeErr := unix.Close(checkFD)
		if statErr != nil || closeErr != nil {
			return errors.Join(statErr, closeErr)
		}
		if current.Dev != stat.Dev || current.Ino != stat.Ino {
			return errors.New("PID-reuse temporary name changed before cleanup")
		}
		return unix.Unlinkat(int(directory.Fd()), tempName, 0)
	}
	written, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(contents) || syncErr != nil || closeErr != nil {
		cleanupErr := removeTemp()
		return errors.Join(writeErr, syncErr, closeErr, cleanupErr, func() error {
			if writeErr == nil && written != len(contents) {
				return io.ErrShortWrite
			}
			return nil
		}())
	}
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return errors.Join(err, removeTemp())
		}
	}
	if err := unix.Renameat2(int(directory.Fd()), tempName, int(directory.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return errors.Join(fmt.Errorf("publish complete PID-reuse exchange %s: %w", name, err), removeTemp())
	}
	return nil
}

func linuxV2ReadPIDReuseExchangeFile(directory *os.File, name string, expectedGID uint32) ([]byte, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap PID-reuse exchange file %s", name)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 ||
		stat.Gid != expectedGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o440 || stat.Size <= 0 || stat.Size > 64<<10 {
		_ = file.Close()
		return nil, fmt.Errorf("PID-reuse response %s is not root:A 0440 singly-linked bounded regular file", name)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) == 0 || len(contents) > 64<<10 {
		return nil, errors.Join(readErr, closeErr, func() error {
			if readErr == nil && len(contents) == 0 {
				return io.ErrUnexpectedEOF
			}
			if readErr == nil && len(contents) > 64<<10 {
				return errors.New("PID-reuse response exceeded its bounded size")
			}
			return nil
		}())
	}
	return contents, nil
}

func linuxV2ReadAOwnedPIDReuseExchangeFile(directory *os.File, name string, expectedUID, expectedGID uint32) ([]byte, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap A-owned PID-reuse exchange file %s", name)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != expectedUID ||
		stat.Gid != expectedGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 || stat.Size <= 0 || stat.Size > 64<<10 {
		_ = file.Close()
		return nil, fmt.Errorf("A-owned PID-reuse exchange %s is not mode 0600 singly-linked bounded regular file", name)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) == 0 || len(contents) > 64<<10 {
		return nil, errors.Join(readErr, closeErr, func() error {
			if readErr == nil && len(contents) == 0 {
				return io.ErrUnexpectedEOF
			}
			if readErr == nil && len(contents) > 64<<10 {
				return errors.New("A-owned PID-reuse exchange exceeded its bounded size")
			}
			return nil
		}())
	}
	return contents, nil
}

func linuxV2EnsureAOwnedPIDReuseExchangeJSON(directory *os.File, name string, value any, ownerUID, ownerGID uint32) error {
	expected, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode PID-reuse exchange %s: %w", name, err)
	}
	expected = append(expected, '\n')
	if err := linuxV2WritePIDReuseExchangeJSON(directory, name, value, ownerUID, ownerGID); err == nil {
		return nil
	} else {
		contents, readErr := linuxV2ReadAOwnedPIDReuseExchangeFile(directory, name, ownerUID, ownerGID)
		if readErr != nil {
			return errors.Join(err, readErr)
		}
		if !bytes.Equal(contents, expected) {
			return fmt.Errorf("existing PID-reuse exchange %s does not match its exact requested payload", name)
		}
		return nil
	}
}

func linuxV2WaitPIDReuseHelperResponse(directory *os.File, gid uint32, timeout time.Duration) (linuxV2PIDReuseHelperResponse, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := linuxV2ReadPIDReuseExchangeFile(directory, "response.json", gid)
		if errors.Is(err, unix.ENOENT) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if err != nil {
			return linuxV2PIDReuseHelperResponse{}, err
		}
		var response linuxV2PIDReuseHelperResponse
		if err := linuxV2DecodeStrictJSON(contents, &response); err != nil {
			return linuxV2PIDReuseHelperResponse{}, fmt.Errorf("decode root PID-reuse helper response: %w", err)
		}
		return response, nil
	}
	return linuxV2PIDReuseHelperResponse{}, errors.New("root PID-reuse helper did not publish a result before the bounded gate timeout")
}

func linuxV2DecodeStrictJSON(contents []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("exchange JSON contains multiple values")
		}
		return err
	}
	return nil
}

func linuxV2WaitForPIDReuseNativePIDFD(fixture linuxV2NativeFixture, timeout time.Duration, stop <-chan struct{}) (linuxV2PIDReuseOldProcess, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return linuxV2PIDReuseOldProcess{}, errors.New("gate pidfd watcher was cancelled before capture")
		case <-ticker.C:
		}
		if !time.Now().Before(deadline) {
			break
		}
		marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "native_release")
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if err != nil {
			return linuxV2PIDReuseOldProcess{}, err
		}
		target := marker.Target
		if marker.HoldAfter != "leaf_removed" || !linuxV2MarkerHasNativePeerProof(fixture, marker) {
			return linuxV2PIDReuseOldProcess{}, errors.New("native_release marker lacks exact B pidfd/namespace/SamePIDExec identity or leaf_removed request")
		}
		parts, err := linuxV2ParseLinuxIdentity(target.Identity)
		if err != nil || parts.pid != target.PID {
			return linuxV2PIDReuseOldProcess{}, errors.New("native_release marker identity does not encode its exact guest PID")
		}
		pidfd, err := linuxV2OpenExactPIDFD(target.PID, target.Identity)
		if err != nil {
			return linuxV2PIDReuseOldProcess{}, fmt.Errorf("pin exact B gate pidfd at native RELEASE: %w", err)
		}
		if err := linuxV2RequireLivePIDFD(int(pidfd.Fd())); err != nil {
			_ = pidfd.Close()
			return linuxV2PIDReuseOldProcess{}, err
		}
		device, inode, err := linuxV2ReadPIDNamespaceIdentity(target.PID)
		if err != nil || inode != parts.nsIno {
			_ = pidfd.Close()
			return linuxV2PIDReuseOldProcess{}, errors.New("released B process PID namespace differs from its exact creation identity")
		}
		bootID, err := linuxV2ReadBootID()
		if err != nil || bootID != parts.bootID {
			_ = pidfd.Close()
			return linuxV2PIDReuseOldProcess{}, errors.New("gate_bound process boot ID differs from its exact creation identity")
		}
		return linuxV2PIDReuseOldProcess{marker: marker, pidfd: pidfd, bootID: bootID, nsDev: device, nsIno: inode, start: parts.start}, nil
	}
	return linuxV2PIDReuseOldProcess{}, errors.New("production owner witness did not publish exact native_release before stop")
}

func linuxV2ParseLinuxIdentity(identity string) (linuxV2LinuxIdentityParts, error) {
	fields := strings.Split(identity, ":")
	if len(fields) != 6 || fields[0] != "linux" || fields[1] != "v2" || fields[2] == "" {
		return linuxV2LinuxIdentityParts{}, errors.New("Linux process identity is not its exact v2 tuple")
	}
	namespace, namespaceErr := strconv.ParseUint(fields[3], 10, 64)
	pid, pidErr := strconv.Atoi(fields[4])
	start, startErr := strconv.ParseUint(fields[5], 10, 64)
	if namespaceErr != nil || namespace == 0 || pidErr != nil || pid <= 1 || startErr != nil || start == 0 {
		return linuxV2LinuxIdentityParts{}, errors.New("Linux process identity has invalid PID namespace, PID, or start tick")
	}
	return linuxV2LinuxIdentityParts{bootID: fields[2], nsIno: namespace, pid: pid, start: start}, nil
}

func linuxV2ReadBootID() (string, error) {
	contents, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(contents))
	if len(bootID) != 36 || bootID == "" {
		return "", errors.New("kernel boot ID is not canonical")
	}
	return bootID, nil
}

func linuxV2ValidateOwnerHelperWitnessSelector(fixture linuxV2NativeFixture) error {
	validDrop := fixture.helperResponseDrop == "stop" || fixture.helperResponseDrop == "ack-stop-receipt" ||
		fixture.helperResponseDrop == "launcher-ready"
	validAction := fixture.helperAction == "crash_after_release_before_register"
	if validDrop == validAction || fixture.helperResponseDrop != "" && !validDrop || fixture.helperAction != "" && !validAction {
		return errors.New("root helper selector must choose exactly one supported response drop or helper action")
	}
	for _, path := range []string{"/etc", "/etc/symmetry"} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("helper selector ancestor %s is not an available directory: %w", path, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Mode&0o022 != 0 {
			return fmt.Errorf("helper selector ancestor %s is not root-owned and non-writable by group/world", path)
		}
	}
	path := "/etc/symmetry/linux-owner-witness.json"
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open pinned filesystem root for helper selector: %w", err)
	}
	relative, err := filepath.Rel(string(filepath.Separator), path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = unix.Close(rootFD)
		return errors.New("helper selector path escaped filesystem root")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	closeRootErr := unix.Close(rootFD)
	if err != nil {
		return fmt.Errorf("open pinned root-owned helper selector: %w", err)
	}
	file := os.NewFile(uintptr(fd), "linux-owner-helper-witness-selector")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap root-owned helper selector")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 ||
		stat.Gid != fixture.owner.AGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o440 || stat.Size <= 0 || stat.Size > 4096 {
		_ = file.Close()
		return errors.New("helper selector is not root:A0440 singly-linked bounded regular JSON")
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) == 0 || len(contents) > 4096 {
		return errors.Join(readErr, closeErr, func() error {
			if readErr == nil && len(contents) > 4096 {
				return errors.New("helper selector exceeds its bounded size")
			}
			return nil
		}())
	}
	if closeRootErr != nil {
		return fmt.Errorf("close pinned filesystem root: %w", closeRootErr)
	}
	var selector linuxV2OwnerHelperWitnessSelector
	if err := linuxV2DecodeStrictJSON(contents, &selector); err != nil {
		return fmt.Errorf("decode exact helper response-loss selector: %w", err)
	}
	if selector.Version != 1 || selector.RunID != fixture.request.RunKey.RunID ||
		selector.Generation != fixture.request.RunKey.Generation || selector.DropResponse != fixture.helperResponseDrop ||
		selector.HelperAction != fixture.helperAction || selector.HoldForReleaseFile != validAction {
		return errors.New("root helper selector changed its exact RunKey, action, or hold contract")
	}
	return nil
}

func linuxV2ReadPIDNamespaceIdentity(pid int) (uint64, uint64, error) {
	if pid <= 0 {
		return 0, 0, errors.New("PID namespace observation requires positive PID")
	}
	info, err := os.Stat(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev == 0 || stat.Ino == 0 {
		return 0, 0, errors.New("PID namespace identity is unavailable through nsfs")
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func linuxV2ReadUnifiedCgroupPath(pid int) (string, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" && filepath.IsAbs(fields[2]) {
			return filepath.Clean(fields[2]), nil
		}
	}
	return "", errors.New("process procfs cgroup omitted the unified v2 hierarchy")
}

func linuxV2CgroupPathWithin(candidate, root string) bool {
	candidate, root = filepath.Clean(candidate), filepath.Clean(root)
	return candidate == root || strings.HasPrefix(candidate, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

func linuxV2CleanupPIDReuseReplacement(
	directory *os.File,
	fixture linuxV2NativeFixture,
	replacement linuxV2PIDReuseHelperResponse,
	requested *bool,
) error {
	request := linuxV2PIDReuseCleanupRequest{
		Version: 1, Operation: "kill_reap", Case: "literal_pid_reuse", RunKey: fixture.request.RunKey,
		ReplacementPID: replacement.ReplacementPID, ReplacementIdentitySHA256: replacement.ReplacementIdentitySHA256,
	}
	if (request.ReplacementPID <= 1) != (request.ReplacementIdentitySHA256 == "") {
		return errors.New("root helper cleanup PID and identity hash must be supplied together")
	}
	if !*requested {
		if err := linuxV2EnsureAOwnedPIDReuseExchangeJSON(directory, "cleanup.json", request, fixture.owner.AUID, fixture.owner.AGID); err != nil {
			return err
		}
		*requested = true
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := linuxV2ReadPIDReuseExchangeFile(directory, "cleanup_response.json", fixture.owner.AGID)
		if errors.Is(err, unix.ENOENT) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
		var response linuxV2PIDReuseCleanupResponse
		if err := linuxV2DecodeStrictJSON(contents, &response); err != nil {
			return fmt.Errorf("decode root PID-reuse cleanup response: %w", err)
		}
		if response.Version != 1 || response.Operation != "kill_reap" || response.Case != request.Case || response.RunKey != request.RunKey {
			return errors.New("root helper cleanup response changed the exact operation or RunKey")
		}
		switch response.Status {
		case "reaped":
			if !response.PIDFDVerified || response.ReplacementPID <= 1 || response.ReplacementIdentitySHA256 == "" ||
				request.ReplacementPID > 1 && (response.ReplacementPID != request.ReplacementPID ||
					response.ReplacementIdentitySHA256 != request.ReplacementIdentitySHA256) {
				return errors.New("root helper did not confirm pidfd-backed kill/reap of the exact replacement")
			}
		case "not_created":
			if request.ReplacementPID > 1 || response.PIDFDVerified || response.ReplacementPID != 0 || response.ReplacementIdentitySHA256 != "" {
				return errors.New("root helper claimed no replacement for a request with exact child identity")
			}
		default:
			return errors.New("root helper cleanup response has an unsupported status")
		}
		ack := linuxV2PIDReuseCleanupAcknowledgement{
			Version: 1, Operation: "ack_cleanup", Case: request.Case, RunKey: request.RunKey, Status: "acknowledged",
		}
		if err := linuxV2EnsureAOwnedPIDReuseExchangeJSON(directory, "cleanup_ack.json", ack, fixture.owner.AUID, fixture.owner.AGID); err != nil {
			return fmt.Errorf("acknowledge exact root PID-reuse cleanup response: %w", err)
		}
		for time.Now().Before(deadline) {
			if err := linuxV2RequireEmptyPIDReuseExchange(directory); err == nil {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return errors.New("root PID-reuse watcher did not remove acknowledged exchange files")
	}
	return errors.New("root helper did not publish exact PID-reuse kill/reap response")
}

func runProductionLinuxV2AckLossReplay(t *testing.T, stage, caseRoot string) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot || !linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("ACK-loss fixture config/request paths do not bind the exact case under %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: stage, markerDirectory: "ack-loss-" + stage, ackLossReplay: true,
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted ACK-loss replay witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("ACK-loss replay requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	barrierReleased := false
	var targetPIDFD *os.File
	defer func() {
		if !barrierReleased && command != nil {
			if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
				t.Errorf("release ACK-loss barrier during cleanup: %v", err)
			}
			barrierReleased = true
		}
		if command != nil && command.process != nil && !command.process.waited {
			if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
				t.Errorf("finish hidden production App during ACK-loss cleanup: %v", err)
			}
		}
		if targetPIDFD != nil {
			_ = targetPIDFD.Close()
		}
	}()
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact %s ACK boundary: %v", stage, err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, stage)
	if err != nil {
		t.Fatalf("read exact %s ACK boundary marker: %v", stage, err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact RunJournal at ACK-loss boundary %s: found=%t error=%v", stage, found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, stage, marker, initial.Journal); err != nil {
		t.Fatalf("ACK-loss stage %s lacks its exact durable effect before response drop: %v", stage, err)
	}
	if initial.Journal.LinuxCloneCheckoutReceipt != nil {
		witness.Clone = linuxV2BuildCloneSnapshot(*initial.Journal.LinuxCloneCheckoutReceipt, fixture.owner.BUID)
	}
	if initial.Journal.ContainmentCgroup != nil && initial.Journal.ContainmentCgroup.LeafIdentity != nil {
		witness.Leaf = linuxV2BuildLeafSnapshot(*initial.Journal.ContainmentCgroup.LeafIdentity)
	}
	witness.Owners = linuxV2OwnerSnapshotFromResponse(marker.Owners)
	if stage == "native_release" {
		live, ready, captureErr := linuxV2CaptureLiveProcess(initial.Journal, fixture, fixture.request.Harness)
		if captureErr != nil || !ready || live.PID != marker.Target.PID || live.Identity != marker.Target.Identity {
			if live.pidfd != nil {
				_ = live.pidfd.Close()
			}
			t.Fatalf("native_release ACK-loss target is not the exact B Journal process: ready=%t error=%v", ready, captureErr)
		}
		if !live.NoWritableCgroupFDs {
			_ = live.pidfd.Close()
			t.Fatal("native target retained a writable descriptor on a cgroup-v2 mount")
		}
		witness.Process = &live
		witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
		witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
		targetPIDFD = live.pidfd
	}
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		t.Fatalf("release exact %s callback before dropping its response: %v", stage, err)
	}
	barrierReleased = true
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for lost ACK response after %s: %v", stage, err)
	}
	firstResponse, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read exact response after injected %s ACK loss: %v", stage, err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, firstResponse.Outcome)
	firstExitCode := linuxV2OwnerCommandExitCode(command)
	if firstResponse.Outcome != "unresolved" || firstResponse.RecoveryState != "unresolved" || !firstResponse.JournalUnproven ||
		firstResponse.FailureCategory != "response_lost" || firstResponse.ResponseLostAfter != stage {
		t.Fatalf("first %s ACK-loss response does not identify exact unresolved lost ACK: outcome=%q recovery=%q category=%q after=%q",
			stage, firstResponse.Outcome, firstResponse.RecoveryState, firstResponse.FailureCategory, firstResponse.ResponseLostAfter)
	}
	if err := linuxV2ValidateStageReplayResponseState(fixture, stage, firstResponse, "unresolved"); err != nil {
		t.Fatalf("first %s response-lost state violates the retained Journal contract: %v", stage, err)
	}
	firstJournal, firstFound, err := linuxV2ValidateStageReplayJournal(fixture, firstResponse)
	if err != nil || !firstFound {
		t.Fatalf("first %s response-lost attempt lacks the exact pending Journal/tombstone: found=%t error=%v", stage, firstFound, err)
	}
	replayCase := testCase
	replayCase.ackLossReplay = false
	replayPath := filepath.Join(caseRoot, "replay_request.json")
	replayRequest, err := linuxV2ReadOwnerWitnessReplayRequest(replayPath, replayCase, fixture.value, fixture.owner)
	if err != nil {
		t.Fatalf("read same-RunKey response replay request without drop selector: %v", err)
	}
	if err := linuxV2RequireSameReplayRequest(fixture.request, replayRequest, stage); err != nil {
		t.Fatalf("response replay changed exact Claim/fence/Subject or drop selector: %v", err)
	}
	replayFixture := fixture
	replayFixture.request = replayRequest
	replayFixture.requestPath = replayPath
	command = startLinuxV2OwnerWitnessCommandWithRequestPath(t, binary, replayFixture, replayPath)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for same-RunKey exact response replay after %s: %v", stage, err)
	}
	replayResponse, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, replayRequest, true)
	if err != nil {
		t.Fatalf("read exact same-RunKey response replay after %s: %v", stage, err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, replayResponse.Outcome)
	if err := linuxV2ValidateStageReplayResponse(replayFixture, stage, replayResponse); err != nil {
		t.Fatalf("same-RunKey exact response replay failed %s stage assertions: %v", stage, err)
	}
	replayExitCode := linuxV2OwnerCommandExitCode(command)
	if err := linuxV2RequireSameOwnerIdentities(marker.Owners, replayResponse.Owners); err != nil {
		t.Fatalf("%s ACK-loss replay duplicated or replaced an owner process: %v", stage, err)
	}
	final, finalFound, err := linuxV2ValidateStageReplayJournal(replayFixture, replayResponse)
	if err != nil {
		t.Fatalf("same-RunKey response replay did not converge exact Journal for %s: %v", stage, err)
	}
	witness.Response = &replayResponse
	witness.Outcome, witness.RecoveryState = replayResponse.Outcome, replayResponse.RecoveryState
	witness.FailureCategory, witness.JournalUnproven, witness.NoTargetAbort = replayResponse.FailureCategory,
		replayResponse.JournalUnproven, replayResponse.NoTargetAbort
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, finalFound)
	witness.AckLoss = &linuxV2AckLossSnapshot{
		Stage: stage, FirstResponseLost: true, FirstExitCode: firstExitCode,
		FirstOutcome: firstResponse.Outcome, FirstRecoveryState: firstResponse.RecoveryState,
		FirstFailureCategory: firstResponse.FailureCategory, ReplayExitCode: replayExitCode,
		ReplayOutcome: replayResponse.Outcome, ReplayRecoveryState: replayResponse.RecoveryState,
		ExactRunKeyFenceReplay: true, NoDuplicateOwnerIdentity: true,
	}
	if targetPIDFD != nil {
		if replayResponse.RecoveryState == "unresolved" {
			if err := linuxV2RequireLivePIDFD(int(targetPIDFD.Fd())); err != nil {
				t.Fatalf("unresolved %s replay lost its exact target PIDFD: %v", stage, err)
			}
		} else if err := linuxV2WaitPIDFDExited(int(targetPIDFD.Fd()), 30*time.Second); err != nil {
			t.Fatalf("resolved %s replay did not stop its exact target: %v", stage, err)
		}
	}
	_ = firstJournal
}

func runProductionLinuxV2HelperResponseLoss(t *testing.T, caseRoot string, responseLoss linuxV2HelperResponseLossCase) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot || !linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("helper response-loss paths do not bind the exact fixture beneath %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: responseLoss.hold, markerDirectory: responseLoss.markerDirectory,
		helperResponseDrop: responseLoss.operation,
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	if err := linuxV2ValidateOwnerHelperWitnessSelector(fixture); err != nil {
		t.Fatalf("root helper selector does not bind exact RunKey/op: %v", err)
	}
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted helper response-loss witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("helper response-loss witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	barrierReleased := false
	var targetPIDFD *os.File
	defer func() {
		if !barrierReleased && command != nil {
			if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
				t.Errorf("release helper response-loss barrier during cleanup: %v", err)
			}
			barrierReleased = true
		}
		if command != nil && command.process != nil && !command.process.waited {
			if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
				t.Errorf("finish helper response-loss command during cleanup: %v", err)
			}
		}
		if targetPIDFD != nil {
			_ = targetPIDFD.Close()
		}
	}()
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact helper response-loss %s marker: %v", responseLoss.hold, err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, responseLoss.hold)
	if err != nil {
		t.Fatalf("read exact helper response-loss %s marker: %v", responseLoss.hold, err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact helper response-loss RunJournal: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, responseLoss.hold, marker, initial.Journal); err != nil {
		t.Fatalf("helper response-loss %s marker lacks its exact durable boundary: %v", responseLoss.hold, err)
	}
	var normalStop *platform.LinuxOwnerHelperV2StopEvidence
	if responseLoss.hold == "stop_receipt_durable" {
		normalStop, err = linuxV2CaptureNormalAnchorStopEvidence(fixture, initial.Journal)
		if err != nil {
			t.Fatalf("capture cached Anchor StopLeaf evidence before lost ACK replay: %v", err)
		}
		witness.AnchorStop = linuxV2BuildAnchorStopSnapshot(*normalStop.AnchorStop)
	}
	if responseLoss.hold == "native_release" {
		live, ready, captureErr := linuxV2CaptureLiveProcess(initial.Journal, fixture, fixture.request.Harness)
		if captureErr != nil || !ready || live.PID != marker.Target.PID || live.Identity != marker.Target.Identity {
			if live.pidfd != nil {
				_ = live.pidfd.Close()
			}
			t.Fatalf("helper stop response-loss target differs from exact B journal: ready=%t error=%v", ready, captureErr)
		}
		if !live.NoWritableCgroupFDs {
			_ = live.pidfd.Close()
			t.Fatal("native target retained a writable descriptor on a cgroup-v2 mount")
		}
		witness.Process = &live
		witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
		witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
		witness.Owners = &live.owners
		witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS = live.postCheckoutAbsentAtCheckoutCAS
		targetPIDFD = live.pidfd
	} else if initial.Journal.LinuxCloneCheckoutReceipt != nil {
		witness.Clone = linuxV2BuildCloneSnapshot(*initial.Journal.LinuxCloneCheckoutReceipt, fixture.owner.BUID)
	}
	if responseLoss.hold == "stop_receipt_durable" {
		authorityValue := initial.Journal.ContainmentAuthority
		if authorityValue == nil || authorityValue.StopReceipt == nil || authorityValue.StopReceipt.Version != authority.SupervisorVersionV2 ||
			authorityValue.StopReceipt.Status != "stopped" || !authorityValue.StopReceipt.ValidFor(*authorityValue) {
			t.Fatal("ack-stop-receipt response-loss boundary lacks its exact durable v2 receipt")
		}
		if err := waitForLinuxWitnessIdentityGone(authorityValue.StopReceipt.TargetPID, authorityValue.StopReceipt.TargetIdentity, 5*time.Second); err != nil {
			t.Fatalf("ack-stop-receipt response-loss target remains live after physical stop: %v", err)
		}
	}
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		t.Fatalf("release exact helper response-loss %s barrier: %v", responseLoss.hold, err)
	}
	barrierReleased = true
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for helper %s response-loss result: %v", responseLoss.operation, err)
	}
	firstResponse, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read first helper response-loss summary: %v", err)
	}
	if !firstResponse.HelperDropObserved || firstResponse.HelperDropOperation != responseLoss.operation ||
		firstResponse.ResponseLostAfter != responseLoss.operation {
		t.Fatalf("App summary lacks authenticated %s helper-response-loss evidence: observed=%t op=%q boundary=%q",
			responseLoss.operation, firstResponse.HelperDropObserved, firstResponse.HelperDropOperation, firstResponse.ResponseLostAfter)
	}
	firstExitCode := linuxV2OwnerCommandExitCode(command)
	if responseLoss.operation == "launcher-ready" {
		if firstResponse.Outcome != "unresolved" {
			t.Fatalf("launcher-ready lost reply returned Outcome %q, want unresolved until no-checkout abort replay", firstResponse.Outcome)
		}
		requireLinuxV2RecoveryExitStatus(t, command, firstResponse.Outcome)
		if firstResponse.RecoveryState == "unresolved" {
			if err := linuxV2ValidateStageReplayResponseState(fixture, responseLoss.hold, firstResponse, "unresolved"); err != nil {
				t.Fatalf("launcher-ready failure before durable cleanup is not exact unresolved state: %v", err)
			}
		} else if err := linuxV2ValidateLauncherReadyLostAckSummary(fixture, marker, firstResponse); err != nil {
			t.Fatalf("launcher-ready safe cleanup tombstone is not exact: %v", err)
		}
	} else if firstResponse.Outcome == "unresolved" {
		requireLinuxV2RecoveryExitStatus(t, command, firstResponse.Outcome)
		if err := linuxV2ValidateStageReplayResponseState(fixture, responseLoss.hold, firstResponse, "unresolved"); err != nil {
			t.Fatalf("first helper response-loss attempt is not exact unresolved state: %v", err)
		}
	} else if firstResponse.Outcome == "completed" {
		requireLinuxV2RecoveryExitStatus(t, command, firstResponse.Outcome)
		if err := linuxV2ValidateOwnerWitnessResponse(fixture, firstResponse); err != nil {
			t.Fatalf("helper %s cached result lacks exact production owner evidence: %v", responseLoss.operation, err)
		}
	} else {
		t.Fatalf("helper %s first outcome = %q, want authenticated cached completion or unresolved", responseLoss.operation, firstResponse.Outcome)
	}
	firstJournal, firstFound, err := linuxV2ValidateStageReplayJournal(fixture, firstResponse)
	if err != nil || !firstFound {
		t.Fatalf("first helper %s response-loss attempt lacks its exact journal/tombstone: found=%t error=%v", responseLoss.operation, firstFound, err)
	}
	if normalStop != nil {
		var firstReceipt *authority.StopReceipt
		if proof := firstJournal.Journal.ContainmentCgroupReleaseProof; proof != nil {
			firstReceipt = proof.StopReceipt
		} else if authorityValue := firstJournal.Journal.ContainmentAuthority; authorityValue != nil {
			firstReceipt = authorityValue.StopReceipt
		}
		if firstReceipt == nil || firstReceipt.LinuxProof == nil || !firstReceipt.LinuxProof.Equal(normalStop.Proof) {
			t.Fatal("first helper ACK-loss state did not retain the exact cached Anchor StopLeaf-backed receipt")
		}
	}
	if firstResponse.Outcome == "unresolved" && (firstJournal.Journal.TerminalTaskResultKind != "" ||
		firstJournal.Journal.TerminalState == "completed" || firstJournal.Journal.GoalDeliveryEnabled ||
		firstJournal.Journal.HasPendingGoalDeliveries()) {
		t.Fatal("unresolved helper response loss accepted or enabled delivery of a task result before exact same-RunKey replay")
	}
	if responseLoss.operation == "launcher-ready" {
		journal := firstJournal.Journal
		if journal.LinuxCloneIntent == nil || journal.LinuxCloneSeedReceipt == nil ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil ||
			journal.LinuxClonePromotionIntent != nil || journal.LinuxCloneAbortProof != nil {
			t.Fatal("launcher-ready first response did not retain the exact unpromoted/no-checkout clone state for same-key abort replay")
		}
	}
	replayRequest, err := linuxV2ReadOwnerWitnessReplayRequest(fixture.requestPath, testCase, fixture.value, fixture.owner)
	if err != nil {
		t.Fatalf("read same-RunKey helper replay request: %v", err)
	}
	if err := linuxV2RequireSameReplayRequest(fixture.request, replayRequest, ""); err != nil {
		t.Fatalf("helper replay changed exact Claim/RunKey/fence/Subject: %v", err)
	}
	replayFixture := fixture
	replayFixture.request = replayRequest
	command = startLinuxV2OwnerWitnessCommand(t, binary, replayFixture)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for same-RunKey helper replay after %s: %v", responseLoss.operation, err)
	}
	replayResponse, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, replayRequest, true)
	if err != nil {
		t.Fatalf("read same-RunKey helper replay response: %v", err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, replayResponse.Outcome)
	if responseLoss.operation == "launcher-ready" {
		if replayResponse.RecoveryState != "released" && replayResponse.RecoveryState != "pre_gate_released" {
			t.Fatalf("launcher-ready replay recovery state = %q, want receipt-backed release", replayResponse.RecoveryState)
		}
		if err := linuxV2ValidateStageReplayResponseState(replayFixture, responseLoss.hold, replayResponse, replayResponse.RecoveryState); err != nil {
			t.Fatalf("launcher-ready same-RunKey clone-abort replay is not exact: %v", err)
		}
	} else if err := linuxV2ValidateStageReplayResponse(replayFixture, responseLoss.hold, replayResponse); err != nil {
		t.Fatalf("same-RunKey helper replay response is not exact: %v", err)
	}
	if err := linuxV2RequireSameOwnerIdentities(marker.Owners, replayResponse.Owners); err != nil {
		t.Fatalf("helper response-loss replay duplicated/replaced owner identities: %v", err)
	}
	final, finalFound, err := linuxV2ValidateStageReplayJournal(replayFixture, replayResponse)
	if err != nil {
		t.Fatalf("same-RunKey helper replay journal is not exact: %v", err)
	}
	if normalStop != nil {
		proof := final.Journal.ContainmentCgroupReleaseProof
		if !finalFound || proof == nil || proof.StopReceipt == nil || !proof.StopReceipt.LinuxProof.Equal(normalStop.Proof) {
			t.Fatal("same-RunKey replay did not release from the exact cached Anchor StopLeaf evidence")
		}
	}
	if responseLoss.operation == "launcher-ready" {
		if err := linuxV2VerifyNoTargetCloneAbort(replayFixture, final.Journal); err != nil {
			t.Fatalf("launcher-ready release replay did not finish its no-checkout clone abort: %v", err)
		}
	}
	witness.Response = &replayResponse
	witness.Outcome, witness.RecoveryState = replayResponse.Outcome, replayResponse.RecoveryState
	witness.FailureCategory, witness.JournalUnproven, witness.NoTargetAbort = replayResponse.FailureCategory,
		replayResponse.JournalUnproven, replayResponse.NoTargetAbort
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, finalFound)
	witness.AckLoss = &linuxV2AckLossSnapshot{
		Stage: responseLoss.hold, FirstResponseLost: firstResponse.HelperDropObserved,
		FirstExitCode: firstExitCode, FirstOutcome: firstResponse.Outcome, FirstRecoveryState: firstResponse.RecoveryState,
		FirstFailureCategory: firstResponse.FailureCategory, HelperDropObserved: firstResponse.HelperDropObserved,
		HelperDropOperation: firstResponse.HelperDropOperation, ReplayExitCode: linuxV2OwnerCommandExitCode(command),
		ReplayOutcome: replayResponse.Outcome, ReplayRecoveryState: replayResponse.RecoveryState,
		ExactRunKeyFenceReplay: true, NoDuplicateOwnerIdentity: true,
	}
	if targetPIDFD != nil {
		if replayResponse.RecoveryState == "unresolved" {
			if err := linuxV2RequireLivePIDFD(int(targetPIDFD.Fd())); err != nil {
				t.Fatalf("unresolved helper replay lost exact B target pidfd: %v", err)
			}
		} else if err := linuxV2WaitPIDFDExited(int(targetPIDFD.Fd()), 30*time.Second); err != nil {
			t.Fatalf("resolved helper replay did not stop its exact B target: %v", err)
		}
	}
	_ = firstJournal
}

func runProductionLinuxV2MigrationDenial(t *testing.T, caseRoot string) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot || !linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("migration request/config paths do not bind the exact case fixture under %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "native_release", markerDirectory: "migration",
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted migration-denial witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("migration witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact OpenCode native_release boundary: %v", err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "native_release")
	if err != nil {
		t.Fatalf("read exact native_release marker: %v", err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact migration RunJournal: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, "native_release", marker, initial.Journal); err != nil {
		t.Fatalf("native_release migration boundary is not exact: %v", err)
	}
	live, ready, captureErr := linuxV2CaptureLiveProcess(initial.Journal, fixture, fixture.request.Harness)
	if captureErr != nil || !ready || marker.Target.PID != live.PID || marker.Target.Identity != live.Identity {
		if live.pidfd != nil {
			_ = live.pidfd.Close()
		}
		t.Fatalf("external observer did not verify exact B process/clone before migration: ready=%t error=%v", ready, captureErr)
	}
	witness.Process = &live
	if !live.NoWritableCgroupFDs {
		t.Fatal("migration target retained a writable descriptor on a cgroup-v2 mount")
	}
	witness.DetachedChild = live.detachedChild
	witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
	witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
	witness.Owners = &live.owners
	witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS = live.postCheckoutAbsentAtCheckoutCAS
	targetPIDFD := live.pidfd
	leafPath := linuxV2LeafPath(fixture.value, live.leafIdentity)
	parentPath := filepath.Dir(leafPath)
	delegatedRoot := filepath.Join(fixture.value.LinuxOwnerCgroupMount, filepath.FromSlash(fixture.value.LinuxOwnerDelegatedPath))
	if !linuxV2PathWithin(leafPath, delegatedRoot) {
		_ = targetPIDFD.Close()
		t.Fatal("exact pinned run leaf escaped the configured delegated cgroup parent")
	}
	alternateName := "symmetry-migrate-" + strings.ReplaceAll(fixture.request.RunKey.RunID, "-", "")
	alternatePath := filepath.Join(parentPath, alternateName)
	if _, err := os.Lstat(alternatePath); !errors.Is(err, os.ErrNotExist) {
		_ = targetPIDFD.Close()
		t.Fatalf("alternate cgroup path already exists or cannot be inspected: %v", err)
	}
	if err := os.Mkdir(alternatePath, 0o700); err != nil {
		_ = targetPIDFD.Close()
		t.Fatalf("create A-owned alternate sibling cgroup leaf: %v", err)
	}
	alternateCreated := true
	defer func() {
		if alternateCreated {
			if err := linuxV2WriteCgroupControl(filepath.Join(alternatePath, "cgroup.kill"), "1"); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("stop exact migrated target during test cleanup: %v", err)
			}
			if err := linuxV2WaitPIDFDExited(int(targetPIDFD.Fd()), 20*time.Second); err != nil {
				t.Errorf("wait for migrated target cleanup: %v", err)
			}
			if err := waitForLinuxWitnessIdentityGone(live.PID, live.Identity, 5*time.Second); err != nil {
				t.Errorf("reap migrated target before alternate cgroup cleanup: %v", err)
			}
			if err := os.Remove(alternatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove empty alternate cgroup leaf: %v", err)
			}
		}
		_ = targetPIDFD.Close()
	}()
	if err := linuxV2CgroupContainsPID(leafPath, live.PID); err != nil {
		t.Fatalf("exact target is not in the StageReplay-pinned leaf before migration: %v", err)
	}
	if err := linuxV2WriteCgroupControl(filepath.Join(alternatePath, "cgroup.procs"), fmt.Sprintf("%d\n", live.PID)); err != nil {
		t.Fatalf("move exact B process to the alternate delegated sibling leaf: %v", err)
	}
	if err := linuxV2CgroupContainsPID(alternatePath, live.PID); err != nil {
		t.Fatalf("target did not enter the exact alternate cgroup leaf: %v", err)
	}
	if err := linuxV2RequirePIDOutsideCgroupLeaf(leafPath, live.PID); err != nil {
		t.Fatalf("target remains in its exact RunJournal-pinned leaf after migration: %v", err)
	}
	if identity, err := platform.ProcessIdentity(live.PID); err != nil || identity != live.Identity {
		t.Fatalf("target process identity changed during cgroup migration: %v", err)
	}
	witness.MigrationObserved = true
	witness.MigrationPathHash = linuxWitnessHashBytes([]byte(alternatePath))
	if err := linuxV2KillOwnerWitnessCommand(command); err != nil {
		t.Fatalf("SIGKILL daemon after exact target migration: %v", err)
	}
	command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for fail-closed migration recovery: %v", err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read redacted migration recovery response: %v", err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, response.Outcome)
	witness.Response = &response
	witness.Outcome = response.Outcome
	witness.FailureCategory = response.FailureCategory
	witness.RecoveryState = response.RecoveryState
	witness.JournalUnproven = response.JournalUnproven
	witness.NoTargetAbort = response.NoTargetAbort
	if err := linuxV2ValidateStageReplayResponseState(fixture, "native_release", response, "unresolved"); err != nil {
		t.Fatalf("migration did not fail closed through the production App: %v", err)
	}
	final, found, err := linuxV2ValidateStageReplayJournal(fixture, response)
	if err != nil {
		t.Fatalf("migration denial did not retain the exact unproven RunJournal: %v", err)
	}
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, found)
	if witness.Journal != nil {
		witness.Journal.NoUnprovenContainment = false
	}
	if final.Journal.ContainmentCgroup == nil || final.Journal.ContainmentCgroup.LeafIdentity == nil ||
		!final.Journal.ContainmentCgroup.LeafIdentity.Equal(live.leafIdentity) {
		t.Fatal("migration denial changed or cleared the exact pinned leaf identity")
	}
	exited, err := linuxV2PIDFDExitedNow(int(targetPIDFD.Fd()))
	if err != nil {
		t.Fatalf("poll migrated exact target after fail-closed recovery: %v", err)
	}
	if !exited {
		if err := linuxV2CgroupContainsPID(alternatePath, live.PID); err != nil {
			t.Fatalf("live migrated target left its exact alternate leaf before cleanup: %v", err)
		}
	} else if err := waitForLinuxWitnessIdentityGone(live.PID, live.Identity, 5*time.Second); err != nil {
		t.Fatalf("migrated target PID identity remained after exit: %v", err)
	}
}

func runProductionLinuxV2StaleLeafIdentity(t *testing.T, caseRoot string) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot || !linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("stale-leaf request/config paths do not bind the exact case fixture under %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "stop_receipt_durable", markerDirectory: "stale-leaf",
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted stale-leaf witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("stale-leaf witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact durable stop receipt: %v", err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "stop_receipt_durable")
	if err != nil {
		t.Fatalf("read exact stop_receipt_durable marker: %v", err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact RunJournal at stale-leaf boundary: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, "stop_receipt_durable", marker, initial.Journal); err != nil {
		t.Fatalf("stop-receipt boundary is not exact: %v", err)
	}
	reservation := initial.Journal.ContainmentCgroup
	if reservation == nil || reservation.LeafIdentity == nil || initial.Journal.ContainmentAuthority == nil ||
		initial.Journal.ContainmentAuthority.StopReceipt == nil || !initial.Journal.ContainmentAuthority.StopReceipt.ValidFor(*initial.Journal.ContainmentAuthority) {
		t.Fatal("stale-leaf fixture lacks the exact still-pending v2 receipt and leaf identity")
	}
	leafIdentity := *reservation.LeafIdentity
	leafPath := linuxV2LeafPath(fixture.value, leafIdentity)
	if err := linuxV2VerifyStageReplayEmptyLeaf(leafPath); err != nil {
		t.Fatalf("stop_receipt_durable leaf is not exact and empty before replacement: %v", err)
	}
	oldInfo, err := os.Lstat(leafPath)
	if err != nil || !oldInfo.IsDir() {
		t.Fatalf("stat exact pinned leaf before replacement: %v", err)
	}
	oldStat, ok := oldInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(oldStat.Dev) != leafIdentity.LeafDevice || oldStat.Ino != leafIdentity.LeafInode {
		t.Fatal("on-disk leaf differs from the exact durable RunJournal identity before replacement")
	}
	if receipt := initial.Journal.ContainmentAuthority.StopReceipt; receipt.TargetPID > 0 && receipt.TargetIdentity != "" {
		if err := waitForLinuxWitnessIdentityGone(receipt.TargetPID, receipt.TargetIdentity, 5*time.Second); err != nil {
			t.Fatalf("stop_receipt_durable target remains live before leaf replacement: %v", err)
		}
	}
	if err := os.Remove(leafPath); err != nil {
		t.Fatalf("remove the exact empty cgroup path while its old identity remains pinned: %v", err)
	}
	if _, err := os.Lstat(leafPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed pinned cgroup path is still reachable: %v", err)
	}
	if err := os.Mkdir(leafPath, 0o700); err != nil {
		t.Fatalf("create a replacement cgroup at the stale leaf name: %v", err)
	}
	replacementExists := true
	defer func() {
		if replacementExists {
			if err := os.Remove(leafPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove empty stale-leaf replacement during cleanup: %v", err)
			}
		}
	}()
	replacementInfo, err := os.Lstat(leafPath)
	if err != nil || !replacementInfo.IsDir() {
		t.Fatalf("stat replacement cgroup leaf: %v", err)
	}
	replacementStat, ok := replacementInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(replacementStat.Dev) == leafIdentity.LeafDevice && replacementStat.Ino == leafIdentity.LeafInode {
		t.Fatal("replacement cgroup reused the exact pinned leaf device/inode")
	}
	witness.StaleLeafIdentityObserved = true
	witness.OriginalLeafInode = leafIdentity.LeafInode
	witness.ReplacementLeafInode = replacementStat.Ino
	if err := linuxV2KillOwnerWitnessCommand(command); err != nil {
		t.Fatalf("SIGKILL daemon after exact leaf path replacement: %v", err)
	}
	command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for fail-closed stale-leaf recovery: %v", err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read stale-leaf recovery response: %v", err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, response.Outcome)
	witness.Response = &response
	witness.Outcome = response.Outcome
	witness.FailureCategory = response.FailureCategory
	witness.RecoveryState = response.RecoveryState
	witness.JournalUnproven = response.JournalUnproven
	witness.NoTargetAbort = response.NoTargetAbort
	if err := linuxV2ValidateStageReplayResponseState(fixture, "stop_receipt_durable", response, "unresolved"); err != nil {
		t.Fatalf("stale cgroup leaf identity did not fail closed: %v", err)
	}
	final, found, err := linuxV2ValidateStageReplayJournal(fixture, response)
	if err != nil {
		t.Fatalf("stale leaf mismatch did not retain the exact unproven RunJournal: %v", err)
	}
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, found)
	if witness.Journal != nil {
		witness.Journal.NoUnprovenContainment = false
	}
	if final.Journal.ContainmentCgroup == nil || final.Journal.ContainmentCgroup.LeafIdentity == nil ||
		!final.Journal.ContainmentCgroup.LeafIdentity.Equal(leafIdentity) ||
		final.Journal.ContainmentCgroupReleaseProof != nil || final.Journal.ContainmentCgroupPreGateReleaseProof != nil {
		t.Fatal("stale leaf recovery changed or cleared the exact original leaf identity")
	}
	afterInfo, err := os.Lstat(leafPath)
	if err != nil {
		t.Fatalf("replacement cgroup path disappeared during stale-identity recovery: %v", err)
	}
	afterStat, ok := afterInfo.Sys().(*syscall.Stat_t)
	if !ok || afterStat.Ino != replacementStat.Ino || afterStat.Ino == leafIdentity.LeafInode {
		t.Fatal("recovery followed, removed, or substituted the replacement leaf for the pinned original")
	}
}

func linuxV2VerifyStageReplayEmptyLeaf(path string) error {
	contents, err := os.ReadFile(filepath.Join(path, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("read exact leaf cgroup.procs: %w", err)
	}
	if len(strings.Fields(string(contents))) != 0 {
		return errors.New("exact cgroup leaf still has populated process membership")
	}
	events, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
	if err != nil {
		return fmt.Errorf("read exact leaf cgroup.events: %w", err)
	}
	if !strings.Contains("\n"+string(events), "\npopulated 0\n") {
		return errors.New("exact cgroup leaf is not physically populated-zero before replacement")
	}
	return nil
}

func runProductionLinuxV2StopReceiptPersistenceFailure(t *testing.T, caseRoot string) {
	t.Helper()
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "native_release", markerDirectory: "receipt-persist-failure",
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted receipt-persistence witness: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("receipt-persistence witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
		t.Fatalf("wait for exact native_release before Store fault: %v", err)
	}
	marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "native_release")
	if err != nil {
		t.Fatalf("read exact native_release marker: %v", err)
	}
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	initial, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !found || initial.Journal.Key() != key {
		t.Fatalf("read exact RunJournal before receipt-persistence fault: found=%t error=%v", found, err)
	}
	if err := linuxV2ValidateStageJournalBoundary(fixture, "native_release", marker, initial.Journal); err != nil {
		t.Fatalf("native_release boundary before Store fault is not exact: %v", err)
	}
	live, ready, captureErr := linuxV2CaptureLiveProcess(initial.Journal, fixture, fixture.request.Harness)
	if captureErr != nil || !ready || live.PID != marker.Target.PID || live.Identity != marker.Target.Identity {
		if live.pidfd != nil {
			_ = live.pidfd.Close()
		}
		t.Fatalf("external observer did not verify exact B target before receipt fault: ready=%t error=%v", ready, captureErr)
	}
	witness.Process = &live
	witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
	witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
	witness.Owners = &live.owners
	witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS = live.postCheckoutAbsentAtCheckoutCAS
	runsDirectory := filepath.Join(fixture.stateDir, "runs")
	runsInfo, err := os.Lstat(runsDirectory)
	if err != nil || !runsInfo.IsDir() {
		_ = live.pidfd.Close()
		t.Fatalf("inspect A-private Store runs directory: %v", err)
	}
	runsStat, ok := runsInfo.Sys().(*syscall.Stat_t)
	if !ok || runsStat.Uid != fixture.owner.AUID || runsStat.Gid != fixture.owner.AGID || runsStat.Mode&0o7777 != 0o700 {
		_ = live.pidfd.Close()
		t.Fatal("Store runs directory is not exact A-owned mode 0700 before the fault")
	}
	if err := os.Chmod(runsDirectory, 0o500); err != nil {
		_ = live.pidfd.Close()
		t.Fatalf("revoke Store runs-directory write permission to force receipt persistence failure: %v", err)
	}
	modeRestored := false
	defer func() {
		if !modeRestored {
			if err := os.Chmod(runsDirectory, 0o700); err != nil {
				t.Errorf("restore A-private Store runs-directory mode during cleanup: %v", err)
			}
		}
		_ = live.pidfd.Close()
	}()
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		t.Fatalf("release exact native_release boundary into the Store fault: %v", err)
	}
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for receipt persistence failure: %v", err)
	}
	if exited, err := linuxV2PIDFDExitedNow(int(live.pidfd.Fd())); err != nil || !exited {
		t.Fatalf("Store receipt failure was not preceded by exact physical target stop: exited=%t error=%v", exited, err)
	}
	if err := waitForLinuxWitnessIdentityGone(live.PID, live.Identity, 5*time.Second); err != nil {
		t.Fatalf("physical stop did not reap the exact B target before failed receipt persistence: %v", err)
	}
	afterFault, stillFound, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil || !stillFound || !bytes.Equal(initial.Raw, afterFault.Raw) {
		t.Fatalf("failed stop-receipt persistence changed or cleared the exact RunJournal: found=%t error=%v", stillFound, err)
	}
	if afterFault.Journal.ContainmentCgroupReleaseProof != nil || afterFault.Journal.ContainmentCgroupPreGateReleaseProof != nil ||
		afterFault.Journal.ContainmentAuthority == nil || afterFault.Journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatal("Store write failure left a stop receipt, release proof, or clear without durable persistence")
	}
	failedResponse, decodeErr := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if decodeErr != nil || failedResponse.Outcome != "unresolved" || !failedResponse.JournalUnproven ||
		failedResponse.Release != nil && failedResponse.Release.ClearPersisted {
		t.Fatalf("Store write failure response fabricated recovery: outcome=%q unproven=%t error=%v", failedResponse.Outcome, failedResponse.JournalUnproven, decodeErr)
	}
	requireLinuxV2RecoveryExitStatus(t, command, failedResponse.Outcome)
	if err := os.Chmod(runsDirectory, 0o700); err != nil {
		t.Fatalf("restore Store runs-directory permissions before replay: %v", err)
	}
	modeRestored = true
	command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, true); err != nil {
		t.Fatalf("wait for receipt-backed same-RunKey recovery after Store repair: %v", err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, true)
	if err != nil {
		t.Fatalf("read receipt-backed same-RunKey recovery response: %v", err)
	}
	requireLinuxV2RecoveryExitStatus(t, command, response.Outcome)
	witness.Response = &response
	witness.Outcome = response.Outcome
	witness.FailureCategory = response.FailureCategory
	witness.RecoveryState = response.RecoveryState
	witness.JournalUnproven = response.JournalUnproven
	witness.NoTargetAbort = response.NoTargetAbort
	if err := linuxV2ValidateStageReplayResponse(fixture, "native_release", response); err != nil {
		t.Fatalf("same-RunKey receipt recovery response is not exact: %v", err)
	}
	final, finalFound, err := linuxV2ValidateStageReplayJournal(fixture, response)
	if err != nil {
		t.Fatalf("same-RunKey receipt recovery did not persist the physical stop: %v", err)
	}
	witness.Journal = linuxV2StageReplayJournalSnapshot(final, finalFound)
	witness.LeafAbsent = response.Release != nil && response.Release.LeafRemoved
	if err := linuxV2WaitPIDFDExited(int(live.pidfd.Fd()), 30*time.Second); err != nil {
		t.Fatalf("replayed stop receipt did not stop the exact target: %v", err)
	}
}

func runProductionLinuxV2AlternateMountRefusal(t *testing.T, caseRoot string) {
	t.Helper()
	if !filepath.IsAbs(caseRoot) || filepath.Clean(caseRoot) != caseRoot ||
		!linuxV2PathWithin(caseRoot, "/opt/symmetry-owner-test") ||
		strings.TrimSpace(os.Getenv(linuxV2OpenCodeConfigEnvironment)) != filepath.Join(caseRoot, "config.json") ||
		strings.TrimSpace(os.Getenv(linuxV2StageReplayRequestEnvironment)) != filepath.Join(caseRoot, "request.json") {
		t.Fatalf("alternate-mount request/config paths do not bind the exact case fixture under %s", caseRoot)
	}
	testCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "reservation_durable", markerDirectory: "alternate-mount",
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	expectedMount := filepath.Join("/opt/symmetry-owner-test", "alternate_mount_cgroup")
	if fixture.value.LinuxOwnerCgroupMount != expectedMount {
		t.Fatalf("alternate config cgroup mount = %q, want bind-mount path %q", fixture.value.LinuxOwnerCgroupMount, expectedMount)
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(fixture.value.LinuxOwnerCgroupMount, &filesystem); err != nil || uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		t.Fatalf("alternate mount fixture is not an actual cgroup-v2 bind mount: %v", err)
	}
	alternateMountID, err := linuxV2MountIDForPath(fixture.value.LinuxOwnerCgroupMount)
	if err != nil {
		t.Fatalf("read alternate cgroup mount ID: %v", err)
	}
	originalMountID, err := linuxV2MountIDForPath("/sys/fs/cgroup")
	if err != nil || alternateMountID == originalMountID {
		t.Fatalf("alternate cgroup path did not have a distinct mount ID: alternate=%d original=%d error=%v", alternateMountID, originalMountID, err)
	}
	journalSetBefore, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
	if err != nil {
		t.Fatalf("snapshot retained journals before alternate-mount refusal: %v", err)
	}
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
		Outcome:          "alternate_cgroup_mount_refused",
		Notes:            []string{"the hidden production command rejected an alternate cgroup-v2 mount before native release"},
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	leaseToken := fixture.request.Claim.LeaseToken
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, leaseToken); err != nil {
			t.Errorf("write redacted alternate-mount refusal evidence: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("alternate mount refusal requires the installed A owner service: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	markerPath := filepath.Join(fixture.request.Barrier.Directory, "reservation_durable.json")
	deadline := time.Now().Add(20 * time.Second)
	markerReached := false
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(markerPath); err == nil {
			markerReached = true
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect alternate-mount reservation marker: %v", err)
		}
		exited, err := linuxV2PIDFDExitedNow(int(command.pidfd.Fd()))
		if err != nil {
			t.Fatalf("poll alternate-mount App process: %v", err)
		}
		if exited {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if markerReached {
		t.Fatalf("production App accepted the alternate cgroup mount and reached %s", testCase.barrierStage)
	}
	if err := waitLinuxV2OwnerWitnessCommand(command, 60*time.Second, true); err != nil {
		t.Fatalf("wait for alternate-mount refusal: %v", err)
	}
	if _, err := os.Lstat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("production App accepted the alternate cgroup mount after its bounded refusal wait")
	}
	if command.exitErr == nil {
		t.Fatal("production App returned success with an alternate cgroup-v2 mount")
	}
	var exit *exec.ExitError
	if !errors.As(command.exitErr, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("alternate mount refusal did not fail the production command: %v", command.exitErr)
	}
	if responseInfo, err := os.Stat(command.responsePath); err != nil || responseInfo.Size() != 0 {
		t.Fatal("alternate mount refusal emitted a successful witness response")
	}
	if entries, err := os.ReadDir(fixture.request.Barrier.Directory); err != nil || len(entries) != 0 {
		t.Fatal("alternate mount refusal wrote a cgroup reservation stage marker")
	}
	if entries, err := os.ReadDir(fixture.markerDir); err != nil || len(entries) != 0 {
		t.Fatal("alternate mount refusal reached the B native hook marker boundary")
	}
	journalSetAfter, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
	key := state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}
	setErr := linuxV2RequireStoreJournalHistoryPreserved(journalSetBefore, journalSetAfter, &key)
	if err != nil || setErr != nil {
		t.Fatalf("alternate-mount refusal changed retained StateDir journals: %v", errors.Join(err, setErr))
	}
	current, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, key)
	if err != nil {
		t.Fatalf("read alternate-mount RunJournal for exact RunKey: %v", err)
	}
	if found {
		journal := current.Journal
		if journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil || journal.ContainmentAuthority != nil ||
			journal.ContainmentHandoff != nil || journal.HasProcessDetails() {
			t.Fatalf("alternate mount created v2 process authority: %#v", linuxWitnessJournalSnapshotFromFile(current))
		}
		witness.Journal = linuxV2StageReplayJournalSnapshot(current, true)
	}
}

func linuxV2MountIDForPath(path string) (uint64, error) {
	contents, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return 0, fmt.Errorf("read host mountinfo: %w", err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || filepath.Clean(strings.ReplaceAll(fields[4], "\\040", " ")) != filepath.Clean(path) {
			continue
		}
		mountID, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || mountID == 0 {
			return 0, errors.New("mountinfo contains an invalid cgroup mount ID")
		}
		return mountID, nil
	}
	return 0, fmt.Errorf("mountinfo omitted mountpoint %q", path)
}

func linuxV2WriteCgroupControl(path, contents string) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "delegated-cgroup-control")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap delegated cgroup control file")
	}
	written, writeErr := file.Write([]byte(contents))
	closeErr := file.Close()
	if writeErr != nil || written != len(contents) {
		return errors.Join(writeErr, errors.New("short write to delegated cgroup control file"), closeErr)
	}
	return closeErr
}

func linuxV2RequirePIDOutsideCgroupLeaf(leafPath string, pid int) error {
	contents, err := os.ReadFile(filepath.Join(leafPath, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("read original pinned leaf membership: %w", err)
	}
	wanted := fmt.Sprintf("%d", pid)
	for _, member := range strings.Fields(string(contents)) {
		if member == wanted {
			return errors.New("exact target still appears in the original leaf cgroup.procs")
		}
	}
	return nil
}

func runProductionLinuxV2NativeOwnerWitness(t *testing.T, testCase linuxV2NativeWitnessCase) {
	t.Helper()
	if testCase.wsl2DenialRequired {
		if os.Getenv(linuxV2WSL2DenialEnvironment) != "1" {
			t.Skip("set " + linuxV2WSL2DenialEnvironment + "=1 to run the manual WSL2 denial witness")
		}
		if !linuxV2IsWSL2() {
			t.Fatal("the WSL2 denial witness requires the observed denying WSL2 kernel")
		}
	} else if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 owner witness")
	}
	fixture := newLinuxV2NativeFixture(t, testCase)
	witness := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: testCase.harness, NativeVersion: fixture.value.Runtime.HarnessVersion,
		TaskID: fixture.request.Claim.TaskID, RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
		NoNativeFallback: true,
		GitHooks:         &linuxV2GitHookSnapshot{MarkerRootHash: linuxWitnessHashBytes([]byte(fixture.markerDir)), NoMarkersBeforeRun: true},
		Subject:          linuxV2SubjectSnapshotFromRequest(fixture.request),
	}
	fixture.outputPath = linuxV2NativeWitnessOutputPath(t, fixture)
	providerToken, leaseToken := "", fixture.request.Claim.LeaseToken
	if fixture.request.Claim.ProviderAccess != nil {
		providerToken = fixture.request.Claim.ProviderAccess.Token
	}
	defer func() {
		if err := writeLinuxV2NativeWitness(fixture.outputPath, witness, providerToken, leaseToken); err != nil {
			t.Errorf("write redacted production owner-witness summary: %v", err)
		}
	}()
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("production owner witness requires the active A systemd owner anchor: %v", err)
	}
	binary := buildProductionDaemonWitness(t, fixture.artifactDir)
	witness.DaemonSHA256, _ = linuxWitnessSHA256(binary)
	verifyLinuxV2ProductionInstall(t, binary, fixture.configPath, fixture.artifactDir)
	witness.InstalledOwnerVerified = true
	command := startLinuxV2OwnerWitnessCommand(t, binary, fixture)
	if fixture.request.Barrier.HoldAfter != "none" {
		if err := waitLinuxV2OwnerWitnessStage(fixture.request, time.Duration(fixture.request.Barrier.TimeoutSeconds)*time.Second); err != nil {
			t.Fatalf("wait for requested production owner-witness barrier: %v", err)
		}
	}

	var helperCrash *linuxV2HelperCrashSnapshot
	var daemonCrash *linuxV2DaemonCrashSnapshot
	var daemonCrashLive *linuxV2ProcessSnapshot
	var beforeChildRelease func(state.RunJournal, *linuxV2ProcessSnapshot) error
	if testCase.helperCrashWitness {
		beforeChildRelease = func(journal state.RunJournal, live *linuxV2ProcessSnapshot) error {
			snapshot, err := linuxV2InjectHelperSIGKILL(fixture, journal, live)
			if err == nil {
				helperCrash = &snapshot
			}
			return err
		}
	} else if testCase.daemonCrashWitness {
		beforeChildRelease = func(journal state.RunJournal, live *linuxV2ProcessSnapshot) error {
			daemonCrashLive = live
			snapshot, err := linuxV2InjectDaemonSIGKILL(command.process, fixture, journal, live)
			if err != nil {
				return err
			}
			daemonCrash = &snapshot
			if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
				return err
			}
			return errLinuxV2DaemonCrashInjected
		}
	}

	journal, live, observeErr := observeLinuxV2OwnerWitnessRun(fixture, command, beforeChildRelease)
	if live != nil {
		witness.Process = live
		witness.DetachedChild = live.detachedChild
		witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
		witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
		witness.Owners = &live.owners
		witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS = live.postCheckoutAbsentAtCheckoutCAS
	}
	if testCase.daemonCrashWitness {
		if !errors.Is(observeErr, errLinuxV2DaemonCrashInjected) || daemonCrash == nil {
			t.Fatalf("daemon SIGKILL did not occur with the exact detached B child live: %v", observeErr)
		}
		witness.DaemonCrash = daemonCrash
		if err := waitLinuxV2OwnerWitnessCommand(command, 15*time.Second, true); err != nil {
			t.Fatalf("wait after exact daemon SIGKILL: %v", err)
		}
		command = startLinuxV2OwnerWitnessCommand(t, binary, fixture)
		journal, live, observeErr = observeLinuxV2OwnerWitnessRun(fixture, command, nil)
		if observeErr != nil {
			t.Fatalf("recover exact daemon-loss owner witness: %v", observeErr)
		}
		if live != nil {
			witness.Process = live
			witness.DetachedChild = live.detachedChild
			witness.Leaf = linuxV2BuildLeafSnapshot(live.leafIdentity)
			witness.Clone = linuxV2BuildCloneSnapshot(live.checkout, fixture.owner.BUID)
			witness.Owners = &live.owners
		}
		assertLinuxV2HelperSurvivedDaemonCrash(t, journal, daemonCrashLive, daemonCrash)
		linuxV2PreserveRetainedCloneState(fixture, journal)
	}
	if observeErr != nil && !testCase.daemonCrashWitness {
		t.Fatalf("observe isolated production owner-witness RunKey: %v", observeErr)
	}
	if testCase.helperCrashWitness {
		assertLinuxV2AnchorReapedHelperChild(t, journal, live, helperCrash)
		witness.AnchorStop = linuxV2BuildAnchorStopSnapshot(*helperCrash.anchorStop)
	}
	if err := waitLinuxV2OwnerWitnessCommand(command, 90*time.Second, testCase.wsl2DenialRequired); err != nil {
		t.Fatalf("wait for isolated production owner-witness command: %v", err)
	}
	response, err := linuxV2ReadOwnerWitnessResponse(command.responsePath, fixture.request, testCase.wsl2DenialRequired)
	if err != nil {
		t.Fatalf("read redacted production owner-witness response: %v", err)
	}
	witness.Response = &response
	witness.Outcome = response.Outcome
	witness.FailureCategory = response.FailureCategory
	witness.RecoveryState = response.RecoveryState
	witness.JournalUnproven = response.JournalUnproven
	witness.NoTargetAbort = response.NoTargetAbort
	if err := linuxV2ValidateOwnerWitnessResponse(fixture, response); err != nil {
		t.Fatalf("production owner-witness response failed exact-boundary assertions: %v", err)
	}
	witness.Journal = linuxV2JournalSummaryFromOwnerResponse(response)
	witness.ProviderActions = linuxV2ProviderActionSummaryFromOwnerResponse(fixture.request, response)
	witness.ProviderActionOK = response.ProviderAction != nil && response.ProviderAction.CompletedCount > 0 && response.ProviderAction.Outcome == "succeeded"
	witness.HealthPeerOK = response.Health != nil && response.Health.Healthy && response.Health.PeerVerified && response.Health.NetworkNamespaceVerified
	witness.LeafAbsent = response.Release != nil && response.Release.LeafRemoved
	if witness.Process == nil && response.Target != nil {
		witness.Process = linuxV2ProcessSnapshotFromResponse(response.Target, response.Health)
	}
	if witness.Owners == nil {
		witness.Owners = linuxV2OwnerSnapshotFromResponse(response.Owners)
	}
	if witness.Leaf == nil && response.Leaf != nil {
		witness.Leaf = linuxV2LeafSnapshotFromResponse(response.Leaf)
	}
	if witness.Clone == nil && response.Clone != nil {
		witness.Clone = linuxV2CloneSnapshotFromResponse(response.Clone)
	}
	if response.DetachedChild != nil {
		responseChild := linuxV2DetachedSnapshotFromResponse(response.DetachedChild)
		if witness.DetachedChild != nil && (witness.DetachedChild.PID != responseChild.PID || witness.DetachedChild.Identity != responseChild.Identity) {
			t.Fatal("production App and external observer disagreed on the exact detached child")
		}
		if witness.DetachedChild != nil {
			responseChild.NoWritableCgroupFDs = witness.DetachedChild.NoWritableCgroupFDs
		}
		witness.DetachedChild = responseChild
	}
	postCheckoutMarker, postCommitMarker, commitOwner, hookErr := linuxV2ReadNativeHookMarkers(fixture)
	if hookErr != nil {
		t.Fatalf("read redacted native hook outcome: %v", hookErr)
	}
	witness.GitHooks.PostCommitMarkerObserved = postCommitMarker
	witness.GitHooks.PostCommitMarkerOwnerUID = commitOwner
	witness.GitHooks.PostCheckoutMarkerObservedAfterExec = postCheckoutMarker
	if testCase.wsl2DenialRequired {
		if response.Outcome != "peer_denied" || !linuxV2KnownPeerDenial(response.FailureCategory) ||
			response.ProviderAction != nil && response.ProviderAction.CompletedCount != 0 ||
			response.Health == nil || !response.Health.Attempted || response.Health.Healthy || response.Health.PeerVerified {
			t.Fatalf("WSL2 witness did not report a precise fail-closed peer denial: outcome=%q category=%q", response.Outcome, response.FailureCategory)
		}
		if postCommitMarker {
			t.Fatal("WSL2 peer denial unexpectedly observed a post-commit marker")
		}
	} else {
		if response.Outcome != "completed" && response.Outcome != "recovered" {
			t.Fatalf("isolated %s owner witness outcome = %q", testCase.harness, response.Outcome)
		}
		if witness.Process == nil || witness.Process.PID <= 0 || witness.Process.Identity == "" ||
			!witness.Process.PIDFDOpened || witness.Process.PIDFDInfoPID != witness.Process.PID ||
			witness.Process.EffectiveUID != fixture.owner.BUID || !witness.Process.LeafMembership || !witness.Process.NativeCommand ||
			witness.Process.UserNamespaceDevice == 0 || witness.Process.UserNamespaceInode == 0 {
			t.Fatal("external observer did not verify exact B same-PID exec and pinned-leaf membership")
		}
		if !witness.Process.NoWritableCgroupFDs {
			t.Fatal("native target retained a writable descriptor on a cgroup-v2 mount")
		}
		if witness.Clone == nil || !witness.Clone.CheckoutReady || witness.Clone.TargetInode == 0 || witness.Clone.OwnerUID != fixture.owner.BUID {
			t.Fatal("external observer did not verify the B-owned standalone clone at checkout CAS")
		}
		if !witness.GitHooks.PostCheckoutAbsentAtCheckoutCAS {
			t.Fatal("native checkout hook was not absent at the Store checkout CAS")
		}
		if postCommitMarker && commitOwner != fixture.owner.BUID {
			t.Fatal("native post-commit marker is not a singly linked fixed-B file")
		}
		if (testCase.helperCrashWitness || testCase.daemonCrashWitness) && !postCommitMarker {
			t.Fatal("detached-child crash fixture did not publish its bounded B post-commit marker")
		}
		if (testCase.helperCrashWitness || testCase.daemonCrashWitness) &&
			(witness.DetachedChild == nil || !witness.DetachedChild.NoWritableCgroupFDs) {
			t.Fatal("detached B child retained a writable descriptor on a cgroup-v2 mount")
		}
		if testCase.harness == "opencode" && postCommitMarker {
			t.Fatal("OpenCode health-only witness unexpectedly observed a post-commit marker")
		}
		if testCase.providerActionRequired && !witness.ProviderActionOK {
			t.Fatal("Pi owner witness did not record one successful real Control provider action")
		}
		if testCase.providerActionRequired {
			witness.ControlAction = linuxV2ReadControlActionSnapshot(t, fixture, response)
		}
		if !testCase.providerActionRequired && !witness.HealthPeerOK {
			t.Fatal("OpenCode owner witness did not complete the exact B loopback health-peer check")
		}
	}
	if testCase.helperCrashWitness || testCase.daemonCrashWitness {
		if witness.DetachedChild == nil || !witness.DetachedChild.PIDFDOpened ||
			witness.DetachedChild.PIDFDInfoPID != witness.DetachedChild.PID || !witness.DetachedChild.LeafMembership {
			t.Fatal("crash witness did not preserve the exact detached descendant pidfd and pinned-leaf proof")
		}
	}
	if testCase.helperCrashWitness {
		if witness.DetachedChild == nil || !witness.DetachedChild.AnchorReaped || helperCrash == nil || !helperCrash.HelperSIGKILLVerified ||
			!helperCrash.AnchorReapedChild || response.Stop.ProofKind != authority.LinuxCgroupStopProofMirror ||
			response.Stop.Reaper == nil || response.Stop.Reaper.PID != helperCrash.AnchorPID || response.Stop.Reaper.Identity != helperCrash.AnchorIdentity {
			t.Fatal("helper SIGKILL did not produce exact anchor adoption/reap evidence")
		}
		witness.HelperCrash = helperCrash
	}
	if testCase.daemonCrashWitness {
		if daemonCrash == nil || !daemonCrash.DaemonSIGKILLVerified || !daemonCrash.HelperSurvivedDaemonSIGKILL || response.Stop.ProofKind != authority.LinuxCgroupStopProofHelper ||
			response.Stop.Reaper == nil || response.Stop.Reaper.PID != daemonCrash.HelperPID || response.Stop.Reaper.Identity != daemonCrash.HelperIdentity {
			t.Fatal("daemon SIGKILL did not preserve the exact helper's receipt-backed reaper role")
		}
		witness.DaemonCrash = daemonCrash
	}
	if live != nil && live.detachedChild != nil && live.detachedChild.pidfd != nil {
		defer live.detachedChild.pidfd.Close()
	}
	if live != nil && live.pidfd != nil {
		defer live.pidfd.Close()
	}
	if daemonCrashLive != nil && daemonCrashLive != live {
		if daemonCrashLive.detachedChild != nil && daemonCrashLive.detachedChild.pidfd != nil {
			defer daemonCrashLive.detachedChild.pidfd.Close()
		}
		if daemonCrashLive.pidfd != nil {
			defer daemonCrashLive.pidfd.Close()
		}
	}
}
