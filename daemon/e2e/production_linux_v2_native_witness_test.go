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
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxV2NativeWitnessEnvironment                      = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS"
	linuxV2NativeWitnessOutputEnvironment                = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR"
	linuxV2ControlReceiptRootEnvironment                 = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CONTROL_RECEIPT_ROOT"
	linuxV2ControlContextRootEnvironment                 = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CONTROL_RECEIPT_CONTEXT_ROOT"
	linuxV2UnresolvedJournalGuardFreshRequestEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNRESOLVED_JOURNAL_GUARD_FRESH_REQUEST"
	linuxV2WSL2DenialEnvironment                         = "SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS"
	linuxV2PiConfigEnvironment                           = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CONFIG"
	linuxV2PiCrashConfigEnvironment                      = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CRASH_CONFIG"
	linuxV2PiDaemonCrashConfigEnvironment                = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_DAEMON_CRASH_CONFIG"
	linuxV2OpenCodeConfigEnvironment                     = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_OPENCODE_CONFIG"
	linuxV2WSL2ConfigEnvironment                         = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG"
	linuxV2StageReplayRootEnvironment                    = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_STAGE_REPLAY_ROOT"
	linuxV2StageReplayRequestEnvironment                 = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_STAGE_REPLAY_REQUEST"
	linuxV2AckLossReplayRootEnvironment                  = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_ACK_LOSS_ROOT"
	linuxV2NegativeRootEnvironment                       = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_NEGATIVE_ROOT"
	linuxV2UnprovisionedWitnessEnvironment               = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_WITNESS"
	linuxV2UnprovisionedRootEnvironment                  = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_ROOT"
	linuxV2UnprovisionedDaemonEnvironment                = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_DAEMON"
	linuxV2LegacyV1ReplayEnvironment                     = "SYMMETRY_PRODUCTION_LINUX_V2_LEGACY_V1_REPLAY_WITNESS"
	linuxV2PIDReuseHelperDirectory                       = "/opt/symmetry-owner-test/pid-reuse"
	linuxV2PIDReuseHelperExecutable                      = "/usr/libexec/symmetry-test/literal-pid-reuse-helper"

	linuxV2PiRequestEnvironment             = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_REQUEST"
	linuxV2OpenCodeRequestEnvironment       = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_OPENCODE_REQUEST"
	linuxV2CrashPiRequestEnvironment        = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CRASH_REQUEST"
	linuxV2DaemonCrashPiRequestEnvironment  = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_DAEMON_CRASH_REQUEST"
	linuxV2WSL2RequestEnvironment           = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST"
	linuxV2NativeHookMarkerRootEnvironment  = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT"
	linuxV2NativeHookMarkerChildEnvironment = "SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT"
	linuxV2NativeHookMarkerMaxBytes         = 64
	linuxV2NativeTaskTimeout                = 7 * time.Minute
)

var errLinuxV2HelperCrashInjected = errors.New("production v2 helper crash witness injected")
var errLinuxV2DaemonCrashInjected = errors.New("production v2 daemon crash witness injected")

// TestProductionLinuxV2PiNativeProviderAction runs the isolated production
// owner witness with the real B Pi adapter and Control-issued provider access.
// It proves native-owner containment and a real brokered resource sync; it does
// not claim Goal acceptance or promote Pi capabilities.
func TestProductionLinuxV2PiNativeProviderAction(t *testing.T) {
	runProductionLinuxV2NativeOwnerWitness(t, linuxV2NativeWitnessCase{
		harness: "pi", configEnvironment: linuxV2PiConfigEnvironment,
		requestEnvironment: linuxV2PiRequestEnvironment, requestCase: "pi_provider_action",
		providerActionRequired: true,
	})
}

// TestProductionLinuxV2OpenCodeNativeHealth runs the isolated production owner
// witness with the real B OpenCode adapter and requires the exact server's
// launch-pidfd/user-namespace/loopback health verification. It is not Goal
// acceptance or capability evidence.
func TestProductionLinuxV2OpenCodeNativeHealth(t *testing.T) {
	runProductionLinuxV2NativeOwnerWitness(t, linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2OpenCodeRequestEnvironment, requestCase: "opencode_native_health",
	})
}

// TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild exercises the
// production helper-owner loss path with a B-owned detached-child fixture. Its
// marker locates the child but is not treated as hook-authorship proof. The
// external observer binds the exact B leader/child PIDFDs, credentials, session
// separation and pinned-leaf membership, then SIGKILLs only the helper pidfd
// and requires anchor reaping plus receipt-backed release after recovery.
func TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild(t *testing.T) {
	runProductionLinuxV2NativeOwnerWitness(t, linuxV2NativeWitnessCase{
		harness: "pi", configEnvironment: linuxV2PiCrashConfigEnvironment,
		requestEnvironment: linuxV2CrashPiRequestEnvironment, requestCase: "helper_sigkill_detached_child",
		providerActionRequired: true, helperCrashWitness: true, markerDirectory: "pi-crash",
	})
}

// TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild kills only the
// production A daemon while the B-owned detached-child fixture is live.
// The exact A helper must survive, stop the whole pinned leaf, and replay its
// physical receipt through the restarted production daemon.
func TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild(t *testing.T) {
	runProductionLinuxV2NativeOwnerWitness(t, linuxV2NativeWitnessCase{
		harness: "pi", configEnvironment: linuxV2PiDaemonCrashConfigEnvironment,
		requestEnvironment: linuxV2DaemonCrashPiRequestEnvironment, requestCase: "daemon_sigkill_helper_survives_detached_child",
		providerActionRequired: true, daemonCrashWitness: true, markerDirectory: "pi-daemon-crash",
	})
}

// TestProductionLinuxV2WSL2DenialWitness is a manual, opt-in negative witness.
// It needs the complete installed A/B systemd fixture and an authentic
// Control-claimed non-Goal OpenCode request. It passes only when the production
// v2 peer check observes a WSL2 nsfs/fdinfo/pidfd_getfd permission denial, the
// run fails closed, and the exact owner/leaf is stopped and released without fallback.
func TestProductionLinuxV2WSL2DenialWitness(t *testing.T) {
	if os.Getenv(linuxV2WSL2DenialEnvironment) != "1" {
		t.Skip("set " + linuxV2WSL2DenialEnvironment + "=1 to run the production WSL2 denial witness")
	}
	if !linuxV2IsWSL2() {
		t.Fatal("the opt-in WSL2 denial witness requires a WSL2 kernel")
	}
	runProductionLinuxV2NativeOwnerWitness(t, linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2WSL2ConfigEnvironment,
		requestEnvironment: linuxV2WSL2RequestEnvironment, requestCase: "wsl2_denial",
		wsl2DenialRequired: true,
	})
}

func TestProductionLinuxV2StageCrashReplayMatrix(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 stage-replay matrix")
	}
	fixtureRoot := strings.TrimSpace(os.Getenv(linuxV2StageReplayRootEnvironment))
	if !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		!linuxV2PathWithin(fixtureRoot, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical stage-fixture root beneath /opt/symmetry-owner-test", linuxV2StageReplayRootEnvironment)
	}
	for _, stage := range linuxV2StageReplayStages {
		stage := stage
		t.Run(stage, func(t *testing.T) {
			stageRoot := filepath.Join(fixtureRoot, stage)
			configPath := filepath.Join(stageRoot, "config.json")
			requestPath := filepath.Join(stageRoot, "request.json")
			t.Setenv(linuxV2OpenCodeConfigEnvironment, configPath)
			t.Setenv(linuxV2StageReplayRequestEnvironment, requestPath)
			runProductionLinuxV2StageCrashReplay(t, stage)
		})
	}
}

func TestProductionLinuxV2AckLossReplayMatrix(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 ACK-loss replay matrix")
	}
	fixtureRoot := strings.TrimSpace(os.Getenv(linuxV2AckLossReplayRootEnvironment))
	if !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		!linuxV2PathWithin(fixtureRoot, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical ACK-loss fixture root beneath /opt/symmetry-owner-test", linuxV2AckLossReplayRootEnvironment)
	}
	for _, stage := range linuxV2AckLossReplayStages {
		stage := stage
		t.Run(stage, func(t *testing.T) {
			caseRoot := filepath.Join(fixtureRoot, stage)
			t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
			t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
			runProductionLinuxV2AckLossReplay(t, stage, caseRoot)
		})
	}
}

func TestProductionLinuxV2HelperResponseLossReplayMatrix(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production helper response-loss replay witnesses")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical helper-response-loss fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	cases := []linuxV2HelperResponseLossCase{
		{name: "stop", directory: "helper_stop_response_loss", markerDirectory: "helper-stop-response-loss", hold: "native_release", operation: "stop"},
		{name: "ack_stop_receipt", directory: "helper_ack_response_loss", markerDirectory: "helper-ack-response-loss", hold: "stop_receipt_durable", operation: "ack-stop-receipt"},
		{name: "launcher_ready", directory: "helper_launcher_response_loss", markerDirectory: "helper-launcher-response-loss", hold: "launcher_bound", operation: "launcher-ready"},
	}
	for _, responseLoss := range cases {
		responseLoss := responseLoss
		t.Run(responseLoss.name, func(t *testing.T) {
			caseRoot := filepath.Join(root, responseLoss.directory)
			t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
			t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
			runProductionLinuxV2HelperResponseLoss(t, caseRoot, responseLoss)
		})
	}
}

func TestProductionLinuxV2StaleOwnerRequestDenials(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 stale-request denials")
	}
	fixtureRoot := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		!linuxV2PathWithin(fixtureRoot, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	for _, caseName := range []string{"stale_journal_run_key", "stale_journal_fence", "stale_journal_subject"} {
		caseName := caseName
		t.Run(caseName, func(t *testing.T) {
			caseRoot := filepath.Join(fixtureRoot, caseName)
			configPath := filepath.Join(caseRoot, "config.json")
			setupRequestPath := filepath.Join(caseRoot, "setup_request.json")
			staleRequestPath := filepath.Join(caseRoot, "request.json")
			t.Setenv(linuxV2OpenCodeConfigEnvironment, configPath)
			t.Setenv(linuxV2StageReplayRequestEnvironment, setupRequestPath)
			setupCase := linuxV2NativeWitnessCase{
				harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
				requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
				barrierStage: "handoff_committed", markerDirectory: "negative-" + caseName + "-setup",
			}
			fixture, binary := runProductionLinuxV2StageCrashReplayCase(t, "handoff_committed", setupCase)
			if err := linuxV2ValidateRootWitnessFile(staleRequestPath, fixture.owner); err != nil {
				t.Fatalf("validate root-controlled stale request: %v", err)
			}
			contents, err := os.ReadFile(staleRequestPath)
			if err != nil {
				t.Fatalf("read root-controlled stale request: %v", err)
			}
			var staleRequest linuxV2OwnerWitnessRequest
			if err := json.Unmarshal(contents, &staleRequest); err != nil {
				t.Fatalf("decode stale request fixture: %v", err)
			}
			if err := linuxV2AssertStaleRequestVariant(caseName, fixture.request, staleRequest); err != nil {
				t.Fatalf("stale request fixture does not contain the intended mismatch: %v", err)
			}
			if err := linuxV2ValidateNegativeBarrier(staleRequest, fixture.owner); err != nil {
				t.Fatalf("validate stale request barrier directory: %v", err)
			}
			before, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, state.RunKey{
				RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
			})
			if err != nil || !found {
				t.Fatalf("read setup RunJournal before stale request: found=%t error=%v", found, err)
			}
			if !linuxV2StoreJournalOwnerStateFromJournal(before.Journal).isStoppedRetained() {
				t.Fatal("stale-request setup must preserve its exact physically released retained RunJournal before replay-required denial")
			}
			journalSetBefore, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
			if err != nil {
				t.Fatalf("snapshot retained Store journals before stale request: %v", err)
			}
			staleFixture := fixture
			staleFixture.requestPath = staleRequestPath
			staleFixture.request = staleRequest
			staleFixture.responsePath = filepath.Join(fixture.artifactDir, "stale-request-response.json")
			command := startLinuxV2OwnerWitnessCommand(t, binary, staleFixture)
			if err := waitLinuxV2OwnerWitnessCommand(command, 30*time.Second, true); err != nil {
				t.Fatalf("wait for fail-closed stale request rejection: %v", err)
			}
			if command.exitErr == nil {
				t.Fatal("production owner-witness command accepted a stale RunKey/fence/Subject")
			}
			var commandExit *exec.ExitError
			if !errors.As(command.exitErr, &commandExit) || commandExit.ExitCode() == 0 {
				t.Fatalf("stale request rejection did not return a failed production command: %v", command.exitErr)
			}
			responseInfo, err := os.Stat(staleFixture.responsePath)
			if err != nil || responseInfo.Size() != 0 {
				t.Fatal("stale request rejection emitted a success response")
			}
			after, afterFound, err := readLinuxWitnessJournalForKey(fixture.stateDir, before.Journal.Key())
			if err != nil || !afterFound || !bytes.Equal(after.Raw, before.Raw) {
				t.Fatalf("stale request changed the exact setup RunJournal: found=%t error=%v", afterFound, err)
			}
			journalSetAfter, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
			setErr := linuxV2RequireSameStoreJournalSnapshot(journalSetBefore, journalSetAfter)
			if err != nil || setErr != nil {
				t.Fatalf("stale request changed or removed another retained RunJournal: %v", errors.Join(err, setErr))
			}
			entries, err := os.ReadDir(staleRequest.Barrier.Directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("stale request created an owner-witness stage marker before identity rejection")
			}
		})
	}
}

// TestProductionLinuxV2UnresolvedJournalBlocksFreshRun rejects an unrelated
// fresh request while preserving and then recovering the exact unresolved RunKey.
func TestProductionLinuxV2UnresolvedJournalBlocksFreshRun(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 unresolved-journal guard witness")
	}
	assertDaemonServiceStopped := func(t *testing.T) {
		t.Helper()
		command := exec.Command("systemctl", "is-active", "--quiet", "symmetry-daemon.service")
		command.Env = linuxV2SafeHostEnvironment()
		if err := command.Run(); err == nil {
			t.Fatal("symmetry-daemon.service must be stopped to release the shared StateDir Store lock")
		}
	}
	assertDaemonServiceStopped(t)
	fixtureRoot := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		!linuxV2PathWithin(fixtureRoot, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(fixtureRoot, "stale_journal_run_key")
	setupConfigPath := filepath.Join(caseRoot, "config.json")
	setupRequestPath := filepath.Join(caseRoot, "setup_request.json")
	freshConfigPath := filepath.Join(caseRoot, "unresolved-journal-guard.json")
	freshRequestPath := strings.TrimSpace(os.Getenv(linuxV2UnresolvedJournalGuardFreshRequestEnvironment))
	if !filepath.IsAbs(freshRequestPath) || filepath.Clean(freshRequestPath) != freshRequestPath ||
		!linuxV2PathWithin(freshRequestPath, fixtureRoot) {
		t.Fatalf("%s must name a canonical fresh request beneath %s", linuxV2UnresolvedJournalGuardFreshRequestEnvironment, fixtureRoot)
	}
	t.Setenv(linuxV2OpenCodeConfigEnvironment, setupConfigPath)
	t.Setenv(linuxV2StageReplayRequestEnvironment, setupRequestPath)
	setupCase := linuxV2NativeWitnessCase{
		harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
		requestEnvironment: linuxV2StageReplayRequestEnvironment, requestCase: "stage_replay",
		barrierStage: "handoff_committed", markerDirectory: "negative-stale_journal_run_key-setup",
		afterStageProcessKilled: func(t *testing.T, fixture linuxV2NativeFixture, binary string) {
			t.Helper()
			t.Run("fresh_request", func(t *testing.T) {
				t.Helper()
				assertDaemonServiceStopped(t)
				if err := linuxV2ValidateRootWitnessFile(freshConfigPath, fixture.owner); err != nil {
					t.Fatalf("validate root-controlled unresolved-guard config: %v", err)
				}
				freshValue, err := config.Load(freshConfigPath)
				if err != nil {
					t.Fatalf("load root-controlled unresolved-guard config: %v", err)
				}
				if freshValue.StateDir != fixture.stateDir || freshValue.Runtime.HarnessKind != "opencode" ||
					freshValue.Runtime.HarnessVersion != fixture.value.Runtime.HarnessVersion ||
					freshValue.Runtime.AdapterVersion != fixture.value.Runtime.AdapterVersion ||
					freshValue.Runtime.RepositoryResourceID != fixture.value.Runtime.RepositoryResourceID ||
					freshValue.Runtime.Workspace != fixture.value.Runtime.Workspace ||
					freshValue.Runtime.AgentProfile != fixture.value.Runtime.AgentProfile {
					t.Fatal("fresh-run config does not preserve the exact installed OpenCode runtime and shared StateDir")
				}
				freshCase := linuxV2NativeWitnessCase{
					harness: "opencode", configEnvironment: linuxV2OpenCodeConfigEnvironment,
					requestEnvironment: linuxV2UnresolvedJournalGuardFreshRequestEnvironment,
					requestCase:        "stage_replay", barrierStage: "handoff_committed",
					markerDirectory: "negative-stale_journal_run_key-setup",
				}
				freshRequest, err := linuxV2ReadOwnerWitnessRequest(freshRequestPath, freshCase, freshValue, fixture.owner)
				if err != nil {
					t.Fatalf("fresh request is not an authentic, non-replay OpenCode claim: %v", err)
				}
				if freshRequest.RunKey == fixture.request.RunKey || freshRequest.Claim.TaskID == fixture.request.Claim.TaskID ||
					freshRequest.Claim.ClaimID == fixture.request.Claim.ClaimID ||
					freshRequest.Barrier.Directory == fixture.request.Barrier.Directory {
					t.Fatal("fresh request reused the setup RunKey, Claim identity, or handoff barrier")
				}
				marker, err := linuxV2ReadOwnerWitnessStageMarker(fixture, "handoff_committed")
				if err != nil {
					t.Fatalf("read setup handoff marker: %v", err)
				}
				if marker.RunKey != fixture.request.RunKey || marker.Owners.Anchor == nil || marker.Owners.Helper == nil ||
					marker.Owners.Gate == nil || marker.Target.PIDFDVerified || marker.Target.SamePIDExec {
					t.Fatal("setup is not held at the exact pre-B handoff boundary")
				}
				staged, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, state.RunKey{
					RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
				})
				if err != nil || !found {
					t.Fatalf("read unresolved setup RunJournal: found=%t error=%v", found, err)
				}
				if staged.Journal.Fence() != fixture.request.Fence ||
					staged.Journal.ContainmentCgroup == nil || staged.Journal.ContainmentCgroup.Helper == nil ||
					staged.Journal.ContainmentCgroup.Gate == nil ||
					marker.Owners.Anchor.PID != staged.Journal.ContainmentCgroup.Anchor.PID ||
					marker.Owners.Anchor.Identity != staged.Journal.ContainmentCgroup.Anchor.Identity ||
					marker.Owners.Helper.PID != staged.Journal.ContainmentCgroup.Helper.PID ||
					marker.Owners.Helper.Identity != staged.Journal.ContainmentCgroup.Helper.Identity ||
					marker.Owners.Gate.PID != staged.Journal.ContainmentCgroup.Gate.Process.PID ||
					marker.Owners.Gate.Identity != staged.Journal.ContainmentCgroup.Gate.Process.Identity {
					t.Fatal("setup RunJournal does not bind the exact live anchor/helper/gate identities")
				}
				var processPIDFDs []*os.File
				defer func() {
					for _, fd := range processPIDFDs {
						_ = fd.Close()
					}
				}()
				for _, ownerProcess := range []*linuxV2OwnerWitnessProcess{marker.Owners.Anchor, marker.Owners.Helper} {
					fd, err := linuxV2OpenStageOwnerPIDFD(ownerProcess, fixture.owner)
					if err != nil {
						t.Fatalf("pin exact live A anchor/helper PIDFD before fresh request: %v", err)
					}
					processPIDFDs = append(processPIDFDs, fd)
				}
				gatePIDFD, err := linuxV2OpenExactPIDFD(marker.Owners.Gate.PID, marker.Owners.Gate.Identity)
				if err != nil {
					t.Fatalf("pin exact live gate PIDFD before fresh request: %v", err)
				}
				processPIDFDs = append(processPIDFDs, gatePIDFD)
				for _, fd := range processPIDFDs {
					if err := linuxV2RequireLivePIDFD(int(fd.Fd())); err != nil {
						t.Fatalf("A anchor/helper or exact gate was not live before fresh request: %v", err)
					}
				}
				journalsBefore, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
				if err != nil {
					t.Fatalf("snapshot shared State before fresh request: %v", err)
				}
				setupKey := staged.Journal.Key()
				setupBefore, found := journalsBefore[setupKey]
				freshKey := state.RunKey{RunID: freshRequest.RunKey.RunID, Generation: freshRequest.RunKey.Generation}
				_, freshKeyExists := journalsBefore[freshKey]
				if !found || freshKeyExists ||
					!setupBefore.owner.HasCgroupReservation || !setupBefore.owner.HasProcessAuthority ||
					!setupBefore.owner.HasPendingContainment || setupBefore.owner.TerminalState != "" ||
					setupBefore.owner.HasValidV2ReleaseProof {
					t.Fatal("fresh request precondition lacks one exact unresolved setup RunJournal and an unused fresh RunKey")
				}
				assertNoHookMarkers := func() {
					t.Helper()
					markerDirectory, err := linuxV2OpenPinnedHookMarkerDirectory(fixture.markerDir, fixture.owner)
					if err != nil {
						t.Fatalf("pin native hook marker directory: %v", err)
					}
					entries, readErr := markerDirectory.ReadDir(-1)
					closeErr := markerDirectory.Close()
					if readErr != nil || closeErr != nil || len(entries) != 0 {
						t.Fatalf("fresh request created B native hook markers: read=%v close=%v entries=%d", readErr, closeErr, len(entries))
					}
				}
				assertNoHookMarkers()
				verifyLinuxV2ProductionInstall(t, binary, freshConfigPath, fixture.artifactDir)
				profile, ok := freshValue.AgentProfiles[freshValue.Runtime.AgentProfile]
				if !ok {
					t.Fatal("fresh-run config omitted the installed native profile")
				}
				workspace, ok := freshValue.Workspaces[freshValue.Runtime.Workspace]
				if !ok {
					t.Fatal("fresh-run config omitted the installed native workspace")
				}
				// Avoid the fixture preflight here; the production CLI must reject this unresolved row.
				freshFixture := fixture
				freshFixture.value, freshFixture.configPath, freshFixture.requestPath = freshValue, freshConfigPath, freshRequestPath
				freshFixture.request, freshFixture.profile, freshFixture.workspace = freshRequest, profile, workspace
				freshFixture.responsePath = filepath.Join(fixture.artifactDir, "fresh-unresolved-request-response.json")
				command := startLinuxV2OwnerWitnessCommand(t, binary, freshFixture)
				if err := waitLinuxV2OwnerWitnessCommand(command, 30*time.Second, true); err != nil {
					t.Fatalf("fresh production request did not promptly reject the foreign unresolved RunJournal: %v", err)
				}
				var commandExit *exec.ExitError
				if !errors.As(command.exitErr, &commandExit) || commandExit.ExitCode() <= 0 {
					t.Fatalf("production owner-witness accepted a fresh Run while another RunKey is unresolved: %v", command.exitErr)
				}
				responseInfo, err := os.Stat(freshFixture.responsePath)
				if err != nil || responseInfo.Size() != 0 {
					t.Fatal("fresh request rejection emitted a response or task result")
				}
				barrierEntries, err := os.ReadDir(freshRequest.Barrier.Directory)
				if err != nil || len(barrierEntries) != 0 {
					t.Fatalf("fresh request created a B-stage marker before unresolved-journal denial: entries=%d error=%v", len(barrierEntries), err)
				}
				assertNoHookMarkers()
				journalsAfter, err := linuxV2SnapshotStoreJournals(fixture.stateDir)
				if err != nil {
					t.Fatalf("read shared State after fresh request: %v", err)
				}
				if err := linuxV2RequireSameStoreJournalSnapshot(journalsBefore, journalsAfter); err != nil {
					t.Fatalf("fresh request changed or added a shared RunJournal: %v", err)
				}
				setupAfter, found := journalsAfter[setupKey]
				if !found || !bytes.Equal(setupBefore.raw, setupAfter.raw) || setupBefore.owner != setupAfter.owner {
					t.Fatal("fresh request changed the exact unresolved setup RunJournal")
				}
				if _, found := journalsAfter[freshKey]; found {
					t.Fatal("fresh request persisted a new RunJournal before unresolved-journal denial")
				}
				for _, fd := range processPIDFDs {
					if err := linuxV2RequireLivePIDFD(int(fd.Fd())); err != nil {
						t.Fatalf("fresh request changed or lost the exact A anchor/helper or gate process: %v", err)
					}
				}
			})
		},
	}
	fixture, _ := runProductionLinuxV2StageCrashReplayCase(t, "handoff_committed", setupCase)
	final, found, err := readLinuxWitnessJournalForKey(fixture.stateDir, state.RunKey{
		RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation,
	})
	if err != nil || !found || !linuxV2JournalHasPhysicalRelease(final.Journal) {
		t.Fatalf("same-RunKey setup recovery did not persist its exact v2 physical release proof: found=%t error=%v", found, err)
	}
}

func linuxV2AssertStaleRequestVariant(caseName string, setup, stale linuxV2OwnerWitnessRequest) error {
	if setup.Version != 1 || stale.Version != 1 || setup.ReplayRequired || !stale.ReplayRequired ||
		setup.Case != "stage_replay" || stale.Case != setup.Case ||
		setup.Harness != "opencode" || stale.Harness != setup.Harness || setup.Subject.ResourceID != stale.Subject.ResourceID ||
		setup.Claim.ProviderAccess != nil || stale.Claim.ProviderAccess != nil || setup.Claim.Work.RequiredCapabilities != nil && len(setup.Claim.Work.RequiredCapabilities) != 0 ||
		len(stale.Claim.Work.RequiredCapabilities) != 0 || stale.Claim.LeaseRemainingMS <= 0 || !stale.Claim.LeaseExpiresAt.After(time.Now()) {
		return errors.New("stale request changed the authenticated OpenCode fixture contract")
	}
	switch caseName {
	case "stale_journal_run_key":
		if stale.RunKey == setup.RunKey || stale.Claim.RunID != stale.RunKey.RunID || stale.Claim.Generation != stale.RunKey.Generation ||
			stale.Fence.Generation != stale.RunKey.Generation || stale.Fence.ClaimID != stale.Claim.ClaimID || stale.Fence.LeaseToken != stale.Claim.LeaseToken {
			return errors.New("stale RunKey request does not carry a distinct internally bound authentic Claim")
		}
	case "stale_journal_fence":
		if stale.RunKey != setup.RunKey || stale.Claim.RunID != setup.Claim.RunID || stale.Claim.Generation != setup.Claim.Generation ||
			stale.Claim.ClaimID != setup.Claim.ClaimID || stale.Claim.LeaseToken != setup.Claim.LeaseToken ||
			stale.Fence.RuntimeID != setup.Fence.RuntimeID || stale.Fence.RuntimeEpoch == setup.Fence.RuntimeEpoch ||
			stale.Fence.Generation != setup.Fence.Generation || stale.Fence.ClaimID != setup.Fence.ClaimID || stale.Fence.LeaseToken != setup.Fence.LeaseToken {
			return errors.New("stale fence request does not differ only at the persisted runtime epoch")
		}
	case "stale_journal_subject":
		if stale.RunKey != setup.RunKey || stale.Claim.RunID != setup.Claim.RunID || stale.Claim.Generation != setup.Claim.Generation ||
			stale.Fence != setup.Fence || stale.Subject == setup.Subject || stale.Subject.ResourceID != setup.Subject.ResourceID || stale.Subject.Validate() != nil {
			return errors.New("stale Subject request does not preserve the Claim/fence/resource while changing a valid revision")
		}
	default:
		return fmt.Errorf("unknown stale request case %q", caseName)
	}
	if stale.Barrier.HoldAfter == "none" || stale.Barrier.TimeoutSeconds <= 0 || stale.Barrier.TimeoutSeconds > 30 {
		return errors.New("stale request must retain a valid bounded stage so rejection is caused by its stale identity")
	}
	return nil
}

func linuxV2ValidateNegativeBarrier(request linuxV2OwnerWitnessRequest, owner linuxgate.OwnerConfig) error {
	if request.Barrier.HoldAfter == "none" || !filepath.IsAbs(request.Barrier.Directory) ||
		filepath.Clean(request.Barrier.Directory) != request.Barrier.Directory || !linuxV2PathWithin(request.Barrier.Directory, "/opt/symmetry-owner-test") {
		return errors.New("negative request barrier is not a canonical fixture path")
	}
	info, err := os.Lstat(request.Barrier.Directory)
	if err != nil || !info.IsDir() {
		return errors.New("negative request barrier directory is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != owner.AUID || stat.Gid != owner.AGID || stat.Mode&0o7777 != 0o700 {
		return errors.New("negative request barrier directory is not A-private mode 0700")
	}
	entries, err := os.ReadDir(request.Barrier.Directory)
	if err != nil || len(entries) != 0 {
		return errors.New("negative request barrier directory must be empty before rejection")
	}
	if !filepath.IsAbs(request.Barrier.ReleaseFile) || filepath.Clean(request.Barrier.ReleaseFile) != request.Barrier.ReleaseFile ||
		request.Barrier.ReleaseFile != filepath.Join(request.Barrier.Directory, "release") {
		return errors.New("negative request release marker must be the fixed barrier child")
	}
	if _, err := os.Lstat(request.Barrier.ReleaseFile); !errors.Is(err, os.ErrNotExist) {
		return errors.New("negative request release marker already exists")
	}
	return nil
}

func TestProductionLinuxV2MigrationDenialRetainsJournal(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production Linux v2 migration denial witness")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, "migration")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
	t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
	runProductionLinuxV2MigrationDenial(t, caseRoot)
}

func TestProductionLinuxV2StaleLeafIdentityRetainsJournal(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the stale-leaf denial witness")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, "stale_leaf")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
	t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
	runProductionLinuxV2StaleLeafIdentity(t, caseRoot)
}

func TestProductionLinuxV2LiteralPIDReuseAfterStopReceipt(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the production literal PID-reuse witness")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, "literal_pid_reuse")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
	t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
	runProductionLinuxV2LiteralPIDReuse(t, caseRoot)
}

func TestProductionLinuxV2StopReceiptPersistenceFailureRetainsJournal(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run the stop-receipt persistence failure witness")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, "receipt_persist_failure")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
	t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
	runProductionLinuxV2StopReceiptPersistenceFailure(t, caseRoot)
}

func TestProductionLinuxV2AlternateCgroupMountRefusal(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to run alternate cgroup mount refusal")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2NegativeRootEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical negative-fixture root beneath /opt/symmetry-owner-test", linuxV2NegativeRootEnvironment)
	}
	caseRoot := filepath.Join(root, "alternate_mount")
	t.Setenv(linuxV2OpenCodeConfigEnvironment, filepath.Join(caseRoot, "config.json"))
	t.Setenv(linuxV2StageReplayRequestEnvironment, filepath.Join(caseRoot, "request.json"))
	runProductionLinuxV2AlternateMountRefusal(t, caseRoot)
}

func TestProductionLinuxV2UnprovisionedHostRefusal(t *testing.T) {
	if os.Getenv(linuxV2UnprovisionedWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2UnprovisionedWitnessEnvironment + "=1 in an uninstalled guest to run the refusal witness")
	}
	refusalDiagnostic := linuxV2RootlessComposeDiagnostic{Version: 1, Case: "unprovisioned", Stage: "rootless_guards"}
	if os.Getenv(linuxV2RootlessComposeWitnessEnvironment) == "1" {
		defer writeLinuxV2RootlessComposeDiagnostic(t, &refusalDiagnostic)
	}
	if os.Geteuid() == 0 {
		t.Fatal("unprovisioned refusal witness must run as an unprivileged host user")
	}
	root := strings.TrimSpace(os.Getenv(linuxV2UnprovisionedRootEnvironment))
	daemonPath := strings.TrimSpace(os.Getenv(linuxV2UnprovisionedDaemonEnvironment))
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !linuxV2PathWithin(root, "/opt/symmetry-owner-test") ||
		!filepath.IsAbs(daemonPath) || filepath.Clean(daemonPath) != daemonPath {
		t.Fatal("unprovisioned fixture root and production daemon path must be canonical absolute paths")
	}
	if err := linuxV2RequireActiveOwnerService(); err == nil {
		t.Fatal("unprovisioned refusal witness unexpectedly has an active symmetry-owner.service")
	}
	if _, err := linuxgate.ReadInstalledOwnerConfig(); err == nil {
		t.Fatal("unprovisioned refusal witness unexpectedly has an installed Linux A/B owner config")
	}
	if _, err := os.Lstat("/etc/symmetry/linux-owner.conf"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixed Linux owner config is present or cannot be inspected: %v", err)
	}
	refusalDiagnostic.Stage = "fixture_files"
	configPath := filepath.Join(root, "config.json")
	requestPath := filepath.Join(root, "request.json")
	for _, path := range []string{configPath, requestPath} {
		if err := linuxV2ValidateRootWitnessFileForGroup(path, uint32(os.Getegid())); err != nil {
			t.Fatalf("validate root-controlled unprovisioned fixture file: %v", err)
		}
	}
	refusalDiagnostic.Stage = "config"
	value, err := config.Load(configPath)
	if err != nil || value.Runtime.HarnessKind != config.RuntimeHarnessOpenCode || value.StateDir == "" {
		t.Fatalf("unprovisioned fixture config is not a valid OpenCode test config: %v", err)
	}
	if !filepath.IsAbs(value.StateDir) || filepath.Clean(value.StateDir) != value.StateDir {
		t.Fatal("unprovisioned fixture StateDir must be canonical and absolute")
	}
	refusalDiagnostic.Stage = "empty_store"
	stateInfo, err := os.Lstat(value.StateDir)
	if err != nil || !stateInfo.IsDir() {
		t.Fatalf("unprovisioned fixture StateDir is unavailable: %v", err)
	}
	stateStat, ok := stateInfo.Sys().(*syscall.Stat_t)
	if !ok || stateStat.Uid != uint32(os.Geteuid()) || stateStat.Gid != uint32(os.Getegid()) || stateStat.Mode&0o7777 != 0o700 {
		t.Fatal("unprovisioned fixture StateDir is not exact-user private mode 0700")
	}
	if journals, err := readLinuxWitnessJournals(value.StateDir); err != nil || len(journals) != 0 {
		t.Fatalf("unprovisioned refusal fixture StateDir is not empty: journals=%d error=%v", len(journals), err)
	}
	refusalDiagnostic.Stage = "request_contract"
	requestContents, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read bounded unprovisioned request: %v", err)
	}
	var request linuxV2OwnerWitnessRequest
	if err := json.Unmarshal(requestContents, &request); err != nil || request.Version != 1 || request.Case != "stage_replay" || request.ReplayRequired ||
		request.Harness != config.RuntimeHarnessOpenCode || request.Barrier.HoldAfter != "native_release" ||
		request.Claim.ProviderAccess != nil || len(request.Claim.Work.RequiredCapabilities) != 0 {
		t.Fatalf("unprovisioned request does not preserve the exact OpenCode refusal contract: %v", err)
	}
	refusalDiagnostic.Stage = "fresh_barrier"
	barrierInfo, err := os.Lstat(request.Barrier.Directory)
	if err != nil || !barrierInfo.IsDir() {
		t.Fatalf("unprovisioned barrier directory is unavailable: %v", err)
	}
	barrierStat, ok := barrierInfo.Sys().(*syscall.Stat_t)
	if !ok || barrierStat.Uid != uint32(os.Geteuid()) || barrierStat.Gid != uint32(os.Getegid()) || barrierStat.Mode&0o7777 != 0o700 {
		t.Fatal("unprovisioned barrier directory is not exact-user private mode 0700")
	}
	if entries, err := os.ReadDir(request.Barrier.Directory); err != nil || len(entries) != 0 {
		t.Fatalf("unprovisioned barrier directory is not fresh: entries=%d error=%v", len(entries), err)
	}
	refusalDiagnostic.Stage = "binary_access"
	daemonInfo, err := os.Lstat(daemonPath)
	if err != nil || !daemonInfo.Mode().IsRegular() {
		t.Fatalf("unprovisioned production daemon binary is unavailable: %v", err)
	}
	daemonStat, ok := daemonInfo.Sys().(*syscall.Stat_t)
	if !ok || daemonStat.Uid != 0 || daemonStat.Nlink != 1 || daemonStat.Mode&0o0022 != 0 || daemonStat.Mode&0o0111 == 0 {
		t.Fatal("unprovisioned daemon path is not a root-owned non-writable production executable")
	}
	if err := unix.Access(daemonPath, unix.X_OK); err != nil {
		t.Fatalf("unprivileged fixture user cannot execute the staged production binary: %v", err)
	}
	refusalDiagnostic.Stage = "command"
	scratch, err := os.MkdirTemp(os.TempDir(), "symmetry-linux-v2-unprovisioned-")
	if err != nil {
		t.Fatalf("create private refusal witness scratch directory: %v", err)
	}
	if err := os.Chmod(scratch, 0o700); err != nil {
		t.Fatalf("secure refusal witness scratch directory: %v", err)
	}
	defer os.RemoveAll(scratch)
	responsePath := filepath.Join(scratch, "response.json")
	responseFile, err := os.OpenFile(responsePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create private unprovisioned response sink: %v", err)
	}
	var diagnostics bytes.Buffer
	command := exec.Command(daemonPath,
		"-linux-v2-native-owner-witness", "-config", configPath, "-request", requestPath,
	)
	command.Dir = scratch
	command.Env = append(linuxV2SafeHostEnvironment(), linuxV2NativeWitnessEnvironment+"=1")
	command.Stdout = responseFile
	command.Stderr = &diagnostics
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = responseFile.Close()
		t.Fatalf("configure unprovisioned production command: %v", err)
	}
	runErr := command.Run()
	closeErr := responseFile.Close()
	refusalDiagnostic.Observation = "command_failed"
	if runErr == nil {
		refusalDiagnostic.Observation = "command_succeeded"
	} else if closeErr != nil {
		refusalDiagnostic.Observation = "response_close_failed"
	}
	if runErr == nil || closeErr != nil {
		t.Fatalf("production command did not fail closed at the missing owner-config boundary: run=%v close=%v", runErr, closeErr)
	}
	refusalDiagnostic.Stage, refusalDiagnostic.Observation = "owner_refusal", "other_refusal"
	if !strings.Contains(diagnostics.String(), "trusted Linux A/B owner config") {
		t.Fatalf("production command failed for an unexpected reason: %s", diagnostics.String())
	}
	refusalDiagnostic.Stage, refusalDiagnostic.Observation = "empty_response", ""
	if responseInfo, err := os.Stat(responsePath); err != nil || responseInfo.Size() != 0 {
		t.Fatal("unprovisioned command emitted a success response")
	}
	refusalDiagnostic.Stage = "store_unchanged"
	if journals, err := readLinuxWitnessJournals(value.StateDir); err != nil || len(journals) != 0 {
		t.Fatalf("unprovisioned refusal created local journal state: journals=%d error=%v", len(journals), err)
	}
	refusalDiagnostic.Stage = "barrier_unchanged"
	if entries, err := os.ReadDir(request.Barrier.Directory); err != nil || len(entries) != 0 {
		t.Fatalf("unprovisioned refusal created a durable stage marker: entries=%d error=%v", len(entries), err)
	}
	refusalDiagnostic.Stage = "evidence_write"
	evidence := linuxV2NativeWitness{
		Version: 1, Case: t.Name(), Harness: config.RuntimeHarnessOpenCode,
		NativeVersion: value.Runtime.HarnessVersion, TaskID: request.Claim.TaskID,
		RunID: request.RunKey.RunID, Generation: request.RunKey.Generation,
		NoNativeFallback: true, Outcome: "unprovisioned_refused",
		Notes: []string{"production hidden command refused before opening the fixture Store or launching a native process"},
	}
	evidence.DaemonSHA256, _ = linuxWitnessSHA256(daemonPath)
	outputFixture := linuxV2NativeFixture{artifactDir: scratch}
	outputPath := linuxV2NativeWitnessOutputPath(t, outputFixture)
	leaseToken := request.Claim.LeaseToken
	if err := writeLinuxV2NativeWitness(outputPath, evidence, leaseToken); err != nil {
		t.Fatalf("write redacted unprovisioned refusal evidence: %v", err)
	}
	refusalDiagnostic.Stage = "complete"
}

func TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries(t *testing.T) {
	if os.Getenv(linuxV2NativeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2NativeWitnessEnvironment + "=1 to test B-writable hook marker safety")
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatalf("read installed Linux A/B identities for marker safety: %v", err)
	}
	if uint32(os.Geteuid()) != owner.AUID || uint32(os.Getegid()) != owner.AGID {
		t.Fatal("hostile marker reader must run as the exact fixed A account")
	}
	if err := linuxV2RequireActiveOwnerService(); err != nil {
		t.Fatalf("marker safety witness requires the installed owner service: %v", err)
	}
	markerRoot := strings.TrimSpace(os.Getenv(linuxV2NativeHookMarkerRootEnvironment))
	if !filepath.IsAbs(markerRoot) || filepath.Clean(markerRoot) != markerRoot || !linuxV2PathWithin(markerRoot, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical A-owned/B-writable root below the fixture", linuxV2NativeHookMarkerRootEnvironment)
	}
	for _, kind := range []string{"symlink", "fifo", "oversized"} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			markerDir := filepath.Join(markerRoot, "hostile-marker-"+kind)
			directory, err := linuxV2OpenPinnedHookMarkerDirectory(markerDir, owner)
			if err != nil {
				t.Fatalf("pin hostile-marker fixture directory: %v", err)
			}
			defer directory.Close()
			if entries, err := directory.ReadDir(-1); err != nil || len(entries) != 0 {
				t.Fatalf("hostile-marker fixture directory is not empty: entries=%d error=%v", len(entries), err)
			}
			markerName := "post-checkout"
			fixture := linuxV2NativeFixture{owner: owner, markerDir: markerDir}
			switch kind {
			case "symlink":
				sentinelDir := t.TempDir()
				sentinel := filepath.Join(sentinelDir, "journal.json")
				if err := os.WriteFile(sentinel, []byte("private-state\n"), 0o600); err != nil {
					t.Fatalf("create private symlink target: %v", err)
				}
				if err := unix.Symlinkat(sentinel, int(directory.Fd()), markerName); err != nil {
					t.Fatalf("plant hostile marker symlink: %v", err)
				}
				if _, _, present, err := linuxV2ReadBHookMarker(fixture, markerName); !errors.Is(err, unix.ELOOP) || present {
					t.Fatal("hook marker reader followed a B-writable symlink")
				}
				if _, err := linuxV2MarkerExists(filepath.Join(markerDir, markerName), owner); err == nil {
					t.Fatal("marker existence check followed or accepted a symlink")
				}
				if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "private-state\n" {
					t.Fatalf("private A sentinel changed through symlink: %q error=%v", contents, err)
				}
			case "fifo":
				if err := unix.Mkfifoat(int(directory.Fd()), markerName, 0o600); err != nil {
					t.Fatalf("plant hostile hook marker FIFO: %v", err)
				}
				writerDone := make(chan struct{})
				writer := time.AfterFunc(500*time.Millisecond, func() {
					defer close(writerDone)
					fd, openErr := unix.Openat(int(directory.Fd()), markerName, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
					if openErr == nil {
						_, _ = unix.Write(fd, []byte("post-checkout\n"))
						_ = unix.Close(fd)
					}
				})
				defer func() {
					if !writer.Stop() {
						<-writerDone
					}
				}()
				started := time.Now()
				if _, _, present, err := linuxV2ReadBHookMarker(fixture, markerName); err == nil || present {
					t.Fatal("hook marker reader accepted a FIFO")
				}
				if time.Since(started) > 400*time.Millisecond {
					t.Fatal("hook marker reader blocked on a B-planted FIFO")
				}
				if _, err := linuxV2MarkerExists(filepath.Join(markerDir, markerName), owner); err == nil {
					t.Fatal("marker existence check accepted a FIFO")
				}
			case "oversized":
				fd, err := unix.Openat(int(directory.Fd()), markerName,
					unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
				if err != nil {
					t.Fatalf("plant oversized hook marker: %v", err)
				}
				file := os.NewFile(uintptr(fd), "oversized-hook-marker")
				payload := strings.Repeat("x", linuxV2NativeHookMarkerMaxBytes+1)
				if written, writeErr := file.Write([]byte(payload)); writeErr != nil || written != len(payload) {
					_ = file.Close()
					t.Fatalf("write oversized hook marker: bytes=%d error=%v", written, writeErr)
				}
				if err := file.Close(); err != nil {
					t.Fatalf("close oversized hook marker: %v", err)
				}
				if _, _, present, err := linuxV2ReadBHookMarker(fixture, markerName); err == nil || present || !strings.Contains(err.Error(), "bounded size") {
					t.Fatalf("hook marker reader did not reject the oversized file before reading: present=%t error=%v", present, err)
				}
				if _, err := linuxV2MarkerExists(filepath.Join(markerDir, markerName), owner); err == nil {
					t.Fatal("marker existence check accepted an oversized file")
				}
			default:
				t.Fatalf("unknown hostile marker kind %q", kind)
			}
			if _, _, _, err := linuxV2ReadNativeHookMarkers(fixture); err == nil {
				t.Fatal("native hook marker evidence reader accepted a hostile symlink, FIFO, or oversized file")
			}
			if err := unix.Unlinkat(int(directory.Fd()), markerName, 0); err != nil {
				t.Errorf("remove hostile marker from isolated fixture directory: %v", err)
			}
		})
	}
}

func TestProductionLinuxV2LegacyV1ReplayNoV2Receipt(t *testing.T) {
	if os.Getenv(linuxV2LegacyV1ReplayEnvironment) != "1" {
		t.Skip("set " + linuxV2LegacyV1ReplayEnvironment + "=1 to run the production legacy-v1 replay witness")
	}
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatal("SYMMETRY_E2E=1 is required for the authentic legacy-v1 Control replay")
	}
	environment := loadControlEnvironment(t)
	runRoot := linuxWitnessRunRoot(t)
	caseRoot := linuxWitnessCaseRoot(runRoot, "v2-legacy-v1-no-v2-receipt")
	buildRoot := filepath.Join(caseRoot, "artifacts")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		t.Fatalf("create legacy-v1 replay binary directory: %v", err)
	}
	daemonBinary := buildProductionDaemonWitness(t, buildRoot)
	metadata := newLinuxReplayMetadata(t, daemonBinary)
	writeLinuxWitnessProvenance(t, runRoot, "legacy-v1-no-v2-receipt", metadata)
	runLinuxStopResponseLostCase(t, environment, daemonBinary, metadata, caseRoot)
}

var linuxV2StageReplayStages = []string{
	"reservation_durable", "create_started", "leaf_bound", "helper_bound", "launcher_bound", "gate_bound",
	"handoff_prepared", "handoff_committed", "lease_armed", "checkout_ready", "native_release",
	"stop_receipt_durable", "leaf_removed", "journal_cleared",
}

var linuxV2AckLossReplayStages = []string{
	"helper_bound", "launcher_bound", "gate_bound", "handoff_prepared", "handoff_committed", "lease_armed",
	"checkout_ready", "stop_receipt_durable", "native_release", "journal_cleared",
}

type linuxV2NativeWitnessCase struct {
	harness                string
	configEnvironment      string
	requestEnvironment     string
	requestCase            string
	barrierStage           string
	markerDirectory        string
	providerActionRequired bool
	ackLossReplay          bool
	helperResponseDrop     string
	helperAction           string
	wsl2DenialRequired     bool
	helperCrashWitness     bool
	daemonCrashWitness     bool
	// afterStageProcessKilled runs before same-RunKey recovery; nil for ordinary replay cases.
	afterStageProcessKilled func(*testing.T, linuxV2NativeFixture, string)
}

type linuxV2HelperResponseLossCase struct {
	name, directory, markerDirectory, hold, operation string
}

type linuxV2WitnessRunKey struct {
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
}

type linuxV2WitnessBarrier struct {
	Directory      string `json:"directory"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HoldAfter      string `json:"hold_after"`
	ReleaseFile    string `json:"release_file"`
}

type linuxV2OwnerWitnessRequest struct {
	Version        int                    `json:"version"`
	Case           string                 `json:"case"`
	Harness        string                 `json:"harness"`
	ReplayRequired bool                   `json:"replay_required,omitempty"`
	RunKey         linuxV2WitnessRunKey   `json:"run_key"`
	Fence          protocol.Fence         `json:"fence"`
	Claim          protocol.ClaimResponse `json:"claim"`
	Subject        protocol.Subject       `json:"subject"`
	Barrier        linuxV2WitnessBarrier  `json:"barrier"`
	DropAckAfter   string                 `json:"drop_ack_after,omitempty"`
}

type linuxV2OwnerHelperWitnessSelector struct {
	Version            int    `json:"version"`
	RunID              string `json:"run_id"`
	Generation         int64  `json:"generation"`
	DropResponse       string `json:"drop_response,omitempty"`
	HelperAction       string `json:"helper_action,omitempty"`
	HoldForReleaseFile bool   `json:"hold_for_release_file,omitempty"`
}

type linuxV2OwnerWitnessFenceSummary struct {
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	Generation   int64  `json:"generation"`
	ClaimID      string `json:"claim_id"`
}

type linuxV2OwnerWitnessProcess struct {
	PID              int                       `json:"pid"`
	Identity         string                    `json:"identity"`
	PIDFDVerified    bool                      `json:"pidfd_verified"`
	HostUID          uint32                    `json:"host_uid,omitempty"`
	UserNamespace    *linuxV2NamespaceSnapshot `json:"user_namespace,omitempty"`
	NetworkNamespace *linuxV2NamespaceSnapshot `json:"network_namespace,omitempty"`
	SamePIDExec      bool                      `json:"same_pid_exec,omitempty"`
}

type linuxV2NamespaceSnapshot struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type linuxV2OwnerWitnessOwners struct {
	Anchor   *linuxV2OwnerWitnessProcess `json:"anchor,omitempty"`
	Helper   *linuxV2OwnerWitnessProcess `json:"helper,omitempty"`
	Launcher *linuxV2OwnerWitnessProcess `json:"launcher,omitempty"`
	Gate     *linuxV2OwnerWitnessProcess `json:"gate,omitempty"`
	Reaper   *linuxV2OwnerWitnessProcess `json:"reaper,omitempty"`
}

type linuxV2OwnerWitnessLeaf struct {
	Version       int    `json:"version"`
	ParentMountID uint64 `json:"parent_mount_id"`
	ParentDevice  uint64 `json:"parent_device"`
	ParentInode   uint64 `json:"parent_inode"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	NameSHA256    string `json:"name_sha256"`
	Removed       bool   `json:"removed"`
}

type linuxV2OwnerWitnessClone struct {
	IntentSHA256   string `json:"intent_sha256"`
	SeedDigest     string `json:"seed_digest"`
	CheckoutReady  bool   `json:"checkout_ready"`
	CheckoutDevice uint64 `json:"checkout_device"`
	CheckoutInode  uint64 `json:"checkout_inode"`
}

type linuxV2OwnerWitnessExit struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
	Waited   bool   `json:"waited"`
	Reaped   bool   `json:"reaped"`
}

type linuxV2OwnerWitnessStop struct {
	ReceiptVersion      int                         `json:"receipt_version"`
	ReceiptStatus       string                      `json:"receipt_status"`
	ProofKind           string                      `json:"proof_kind"`
	Issuer              *linuxV2OwnerWitnessProcess `json:"issuer,omitempty"`
	Reaper              *linuxV2OwnerWitnessProcess `json:"reaper,omitempty"`
	KillAcknowledged    bool                        `json:"kill_acknowledged"`
	PopulatedZero       bool                        `json:"populated_zero"`
	NoMigrationObserved bool                        `json:"no_migration_observed"`
	ProducerFenced      bool                        `json:"producer_fenced"`
	DescendantsReaped   bool                        `json:"descendants_reaped"`
	LauncherExit        *linuxV2OwnerWitnessExit    `json:"launcher_exit,omitempty"`
	GateExit            *linuxV2OwnerWitnessExit    `json:"gate_exit,omitempty"`
	HelperExit          *linuxV2OwnerWitnessExit    `json:"helper_exit,omitempty"`
}

type linuxV2OwnerWitnessRelease struct {
	Version               int    `json:"version"`
	LeafDisposition       string `json:"leaf_disposition"`
	OpaquePlatformRelease bool   `json:"opaque_platform_release"`
	LeafRemoved           bool   `json:"leaf_removed"`
	ClearPersisted        bool   `json:"clear_persisted"`
	AnchorRetired         bool   `json:"anchor_retired"`
}

type linuxV2OwnerWitnessProviderAction struct {
	Attempted      bool   `json:"attempted"`
	CompletedCount int    `json:"completed_count"`
	Outcome        string `json:"outcome"`
}

type linuxV2OwnerWitnessHealth struct {
	Attempted                bool                      `json:"attempted"`
	Healthy                  bool                      `json:"healthy"`
	PeerVerified             bool                      `json:"peer_verified"`
	PeerPID                  int                       `json:"peer_pid"`
	PeerIdentity             string                    `json:"peer_identity"`
	NetworkNamespaceVerified bool                      `json:"network_namespace_verified"`
	NetworkNamespace         *linuxV2NamespaceSnapshot `json:"network_namespace,omitempty"`
}

type linuxV2OwnerWitnessDetachedChild struct {
	PID                    int    `json:"pid"`
	Identity               string `json:"identity"`
	ParentPID              int    `json:"parent_pid"`
	ProcessGroup           int64  `json:"process_group"`
	SessionID              int64  `json:"session_id"`
	PIDFDVerified          bool   `json:"pidfd_verified"`
	LeafMembershipVerified bool   `json:"leaf_membership_verified"`
	AnchorReaped           bool   `json:"anchor_reaped"`
}

type linuxV2OwnerWitnessStages struct {
	ReservationDurable bool `json:"reservation_durable"`
	CreateStarted      bool `json:"create_started"`
	LeafBound          bool `json:"leaf_bound"`
	HelperBound        bool `json:"helper_bound"`
	LauncherBound      bool `json:"launcher_bound"`
	GateBound          bool `json:"gate_bound"`
	HandoffPrepared    bool `json:"handoff_prepared"`
	HandoffCommitted   bool `json:"handoff_committed"`
	InitialLeaseAcked  bool `json:"initial_lease_acked"`
	CheckoutReady      bool `json:"checkout_ready"`
	NativeReleased     bool `json:"native_released"`
	NativeTurnStarted  bool `json:"native_turn_started"`
	DetachedChildLive  bool `json:"detached_child_live"`
	StopReceiptDurable bool `json:"stop_receipt_durable"`
	LeafRemoved        bool `json:"leaf_removed"`
	JournalCleared     bool `json:"journal_cleared"`
}

type linuxV2NativeReleaseState string

type linuxV2OwnerWitnessResponse struct {
	Version                int                                `json:"version"`
	Case                   string                             `json:"case"`
	Harness                string                             `json:"harness"`
	NativeVersion          string                             `json:"native_version"`
	InstalledOwnerVerified bool                               `json:"installed_owner_verified"`
	Outcome                string                             `json:"outcome"`
	NativeReleaseState     linuxV2NativeReleaseState          `json:"native_release_state"`
	FailureCategory        string                             `json:"failure_category"`
	ResponseLostAfter      string                             `json:"response_lost_after,omitempty"`
	HelperDropObserved     bool                               `json:"helper_drop_observed,omitempty"`
	HelperDropOperation    string                             `json:"helper_drop_operation,omitempty"`
	RecoveryState          string                             `json:"recovery_state"`
	JournalUnproven        bool                               `json:"journal_unproven"`
	NoTargetAbort          bool                               `json:"no_target_abort"`
	RunKey                 linuxV2WitnessRunKey               `json:"run_key"`
	Fence                  linuxV2OwnerWitnessFenceSummary    `json:"fence"`
	Subject                protocol.Subject                   `json:"subject"`
	Stages                 linuxV2OwnerWitnessStages          `json:"stages"`
	Target                 *linuxV2OwnerWitnessProcess        `json:"target,omitempty"`
	Owners                 linuxV2OwnerWitnessOwners          `json:"owners"`
	Leaf                   *linuxV2OwnerWitnessLeaf           `json:"leaf,omitempty"`
	Clone                  *linuxV2OwnerWitnessClone          `json:"clone,omitempty"`
	Stop                   *linuxV2OwnerWitnessStop           `json:"stop,omitempty"`
	Release                *linuxV2OwnerWitnessRelease        `json:"release,omitempty"`
	ProviderAction         *linuxV2OwnerWitnessProviderAction `json:"provider_action,omitempty"`
	Health                 *linuxV2OwnerWitnessHealth         `json:"health,omitempty"`
	DetachedChild          *linuxV2OwnerWitnessDetachedChild  `json:"detached_child,omitempty"`
}

type linuxV2OwnerWitnessStageMarker struct {
	Version            int                               `json:"version"`
	Case               string                            `json:"case"`
	Stage              string                            `json:"stage"`
	HoldAfter          string                            `json:"hold_after"`
	NativeReleaseState linuxV2NativeReleaseState         `json:"native_release_state"`
	RunKey             linuxV2WitnessRunKey              `json:"run_key"`
	Target             linuxV2OwnerWitnessProcess        `json:"target"`
	Owners             linuxV2OwnerWitnessOwners         `json:"owners"`
	Leaf               linuxV2OwnerWitnessLeaf           `json:"leaf"`
	Stop               linuxV2OwnerWitnessStop           `json:"stop"`
	Release            linuxV2OwnerWitnessRelease        `json:"release"`
	DetachedChild      *linuxV2OwnerWitnessDetachedChild `json:"detached_child,omitempty"`
}

type linuxV2NativeWitness struct {
	Version                   int                                    `json:"version"`
	Case                      string                                 `json:"case"`
	Harness                   string                                 `json:"harness"`
	NativeVersion             string                                 `json:"native_version"`
	DaemonSHA256              string                                 `json:"daemon_sha256,omitempty"`
	InstalledOwnerVerified    bool                                   `json:"installed_owner_verified"`
	TaskID                    string                                 `json:"task_id,omitempty"`
	RunID                     string                                 `json:"run_id,omitempty"`
	Generation                int64                                  `json:"generation,omitempty"`
	FailureCategory           string                                 `json:"failure_category,omitempty"`
	RecoveryState             string                                 `json:"recovery_state,omitempty"`
	JournalUnproven           bool                                   `json:"journal_unproven,omitempty"`
	NoTargetAbort             bool                                   `json:"no_target_abort,omitempty"`
	HelperCrash               *linuxV2HelperCrashSnapshot            `json:"helper_crash,omitempty"`
	HelperExitAfterRelease    *linuxV2HelperExitAfterReleaseSnapshot `json:"helper_exit_after_release_before_native_register,omitempty"`
	AnchorStop                *linuxV2AnchorStopSnapshot             `json:"anchor_stop,omitempty"`
	DaemonCrash               *linuxV2DaemonCrashSnapshot            `json:"daemon_crash,omitempty"`
	Subject                   *linuxV2SubjectSnapshot                `json:"subject,omitempty"`
	Owners                    *linuxV2OwnerSnapshot                  `json:"owners,omitempty"`
	Process                   *linuxV2ProcessSnapshot                `json:"process,omitempty"`
	DetachedChild             *linuxV2DetachedChildSnapshot          `json:"detached_child,omitempty"`
	AckLoss                   *linuxV2AckLossSnapshot                `json:"ack_loss,omitempty"`
	PIDReuse                  *linuxV2PIDReuseSnapshot               `json:"literal_pid_reuse,omitempty"`
	Leaf                      *linuxV2LeafSnapshot                   `json:"leaf,omitempty"`
	Clone                     *linuxV2CloneSnapshot                  `json:"clone,omitempty"`
	GitHooks                  *linuxV2GitHookSnapshot                `json:"git_hooks,omitempty"`
	Journal                   *linuxV2JournalSnapshot                `json:"journal,omitempty"`
	ProviderActions           []linuxV2ProviderActionSnapshot        `json:"provider_actions,omitempty"`
	ControlAction             *linuxV2ControlActionSnapshot          `json:"control_action,omitempty"`
	MigrationObserved         bool                                   `json:"migration_observed,omitempty"`
	MigrationPathHash         string                                 `json:"migration_path_hash,omitempty"`
	StaleLeafIdentityObserved bool                                   `json:"stale_leaf_identity_observed,omitempty"`
	OriginalLeafInode         uint64                                 `json:"original_leaf_inode,omitempty"`
	ReplacementLeafInode      uint64                                 `json:"replacement_leaf_inode,omitempty"`
	ProviderActionOK          bool                                   `json:"provider_action_succeeded"`
	HealthPeerOK              bool                                   `json:"health_peer_verified"`
	LeafAbsent                bool                                   `json:"leaf_absent_after_receipt"`
	NoNativeFallback          bool                                   `json:"no_native_fallback"`
	Outcome                   string                                 `json:"owner_witness_outcome,omitempty"`
	Response                  *linuxV2OwnerWitnessResponse           `json:"owner_witness_response,omitempty"`
	Notes                     []string                               `json:"notes,omitempty"`
}

type linuxV2PIDReuseSnapshot struct {
	OldPID                           int    `json:"old_pid"`
	ReplacementPID                   int    `json:"replacement_pid"`
	OldIdentitySHA256                string `json:"old_identity_sha256"`
	ReplacementIdentitySHA256        string `json:"replacement_identity_sha256"`
	OldStartTime                     uint64 `json:"old_start_time"`
	ReplacementStartTime             uint64 `json:"replacement_start_time"`
	BootIDSHA256                     string `json:"boot_id_sha256"`
	PIDNamespaceDevice               uint64 `json:"pid_namespace_device"`
	PIDNamespaceInode                uint64 `json:"pid_namespace_inode"`
	OldPIDFDOpened                   bool   `json:"old_pidfd_opened"`
	OldPIDFDExitedAndReaped          bool   `json:"old_pidfd_exited_and_reaped"`
	OldPIDAbsentBeforeReplacement    bool   `json:"old_pid_absent_before_replacement"`
	OldPIDFDRemainsExited            bool   `json:"old_pidfd_remains_exited_after_reuse"`
	ReplacementPIDFDOpened           bool   `json:"replacement_pidfd_opened"`
	ReplacementPIDFDAlive            bool   `json:"replacement_pidfd_alive"`
	ReplacementSurvivedRecovery      bool   `json:"replacement_survived_app_recovery"`
	HelperCleanupPIDFDVerified       bool   `json:"helper_cleanup_pidfd_verified"`
	HelperCleanupReaped              bool   `json:"helper_cleanup_reaped"`
	OutsideOriginalLeaf              bool   `json:"outside_original_leaf"`
	OutsideOriginalLeafAfterRecovery bool   `json:"outside_original_leaf_after_recovery"`
	SeparateServiceCgroup            bool   `json:"separate_service_cgroup"`
}

type linuxV2AckLossSnapshot struct {
	Stage                    string `json:"stage"`
	FirstResponseLost        bool   `json:"first_response_lost"`
	FirstExitCode            int    `json:"first_exit_code"`
	FirstOutcome             string `json:"first_outcome"`
	FirstRecoveryState       string `json:"first_recovery_state"`
	FirstFailureCategory     string `json:"first_failure_category,omitempty"`
	HelperDropObserved       bool   `json:"helper_drop_observed,omitempty"`
	HelperDropOperation      string `json:"helper_drop_operation,omitempty"`
	ReplayExitCode           int    `json:"replay_exit_code"`
	ReplayOutcome            string `json:"replay_outcome"`
	ReplayRecoveryState      string `json:"replay_recovery_state"`
	ExactRunKeyFenceReplay   bool   `json:"exact_run_key_fence_replay"`
	NoDuplicateOwnerIdentity bool   `json:"no_duplicate_owner_identity"`
}

type linuxV2HelperCrashSnapshot struct {
	HelperPID                  int                                     `json:"helper_pid"`
	HelperIdentity             string                                  `json:"helper_identity"`
	HelperPIDFDVerified        bool                                    `json:"helper_pidfd_verified"`
	HelperSIGSTOPVerified      bool                                    `json:"helper_sigstop_verified"`
	LeaderPID                  int                                     `json:"leader_pid"`
	LeaderIdentity             string                                  `json:"leader_identity"`
	ChildPID                   int                                     `json:"child_pid"`
	ChildIdentity              string                                  `json:"child_identity"`
	LeaderExitObserved         bool                                    `json:"leader_exit_observed"`
	ChildLiveAfterLeaderExit   bool                                    `json:"child_live_after_leader_exit"`
	LeaderExitedWhileChildLive bool                                    `json:"leader_exited_while_child_live"`
	HelperSIGKILLVerified      bool                                    `json:"helper_sigkill_verified"`
	AnchorPID                  int                                     `json:"anchor_pid"`
	AnchorIdentity             string                                  `json:"anchor_identity"`
	AnchorReapedChild          bool                                    `json:"anchor_reaped_child"`
	StopProofKind              string                                  `json:"stop_proof_kind,omitempty"`
	StopProofReaperPID         int                                     `json:"stop_proof_reaper_pid,omitempty"`
	StopProofReaperIdentity    string                                  `json:"stop_proof_reaper_identity,omitempty"`
	anchorStop                 *platform.LinuxOwnerAnchorStopEvidence  `json:"-"`
	anchorRunReap              *platform.LinuxOwnerAnchorRunReapResult `json:"-"`
}

type linuxV2DaemonCrashSnapshot struct {
	DaemonPID                     int    `json:"daemon_pid"`
	DaemonIdentity                string `json:"daemon_identity"`
	DaemonPIDFDVerified           bool   `json:"daemon_pidfd_verified"`
	DaemonSIGKILLVerified         bool   `json:"daemon_sigkill_verified"`
	HelperPID                     int    `json:"helper_pid"`
	HelperIdentity                string `json:"helper_identity"`
	HelperPIDFDVerified           bool   `json:"helper_pidfd_verified"`
	HelperSurvivedDaemonSIGKILL   bool   `json:"helper_survived_daemon_sigkill"`
	LeaderPID                     int    `json:"leader_pid"`
	LeaderIdentity                string `json:"leader_identity"`
	DetachedChildPID              int    `json:"detached_child_pid"`
	DetachedChildIdentity         string `json:"detached_child_identity"`
	DetachedChildLiveAtDaemonKill bool   `json:"detached_child_live_at_daemon_kill"`
	StopProofKind                 string `json:"stop_proof_kind,omitempty"`
	StopProofReaperPID            int    `json:"stop_proof_reaper_pid,omitempty"`
	StopProofReaperIdentity       string `json:"stop_proof_reaper_identity,omitempty"`
}

type linuxV2SubjectSnapshot struct {
	RunID            string `json:"run_id"`
	Generation       int64  `json:"generation"`
	ResourceID       string `json:"resource_id"`
	Commit           string `json:"commit"`
	TreeDigest       string `json:"tree_digest"`
	SubjectHash      string `json:"subject_hash"`
	AgentProfile     string `json:"agent_profile"`
	WorkspaceBinding string `json:"workspace_binding"`
}

type linuxV2ProcessSnapshot struct {
	PID                             int    `json:"pid"`
	Identity                        string `json:"identity"`
	ProcessGroup                    int64  `json:"process_group"`
	SessionID                       int64  `json:"session_id"`
	PIDFDOpened                     bool   `json:"pidfd_opened"`
	PIDFDInfoPID                    int    `json:"pidfd_info_pid"`
	RealUID                         uint32 `json:"real_uid"`
	EffectiveUID                    uint32 `json:"effective_uid"`
	RealGID                         uint32 `json:"real_gid"`
	EffectiveGID                    uint32 `json:"effective_gid"`
	UserNamespaceDevice             uint64 `json:"user_namespace_device"`
	UserNamespaceInode              uint64 `json:"user_namespace_inode"`
	NetworkNamespaceVerified        bool   `json:"network_namespace_verified"`
	NetworkNamespaceDevice          uint64 `json:"network_namespace_device"`
	NetworkNamespaceInode           uint64 `json:"network_namespace_inode"`
	NoWritableCgroupFDs             bool   `json:"no_writable_cgroup_fds"`
	LeafMembership                  bool   `json:"exact_leaf_membership"`
	NativeCommand                   bool   `json:"native_command_verified"`
	leafIdentity                    authority.LinuxCgroupIdentity
	owners                          linuxV2OwnerSnapshot
	checkout                        authority.LinuxCloneCheckoutReceipt
	postCheckoutAbsentAtCheckoutCAS bool
	detachedChild                   *linuxV2DetachedChildSnapshot
	pidfd                           *os.File
}

type linuxV2DetachedChildSnapshot struct {
	PID                 int    `json:"pid"`
	Identity            string `json:"identity"`
	ParentPID           int    `json:"parent_pid"`
	ProcessGroup        int64  `json:"process_group"`
	SessionID           int64  `json:"session_id"`
	RealUID             uint32 `json:"real_uid"`
	EffectiveUID        uint32 `json:"effective_uid"`
	RealGID             uint32 `json:"real_gid"`
	EffectiveGID        uint32 `json:"effective_gid"`
	PIDFDOpened         bool   `json:"pidfd_opened"`
	PIDFDInfoPID        int    `json:"pidfd_info_pid"`
	LeafMembership      bool   `json:"exact_leaf_membership"`
	NoWritableCgroupFDs bool   `json:"no_writable_cgroup_fds"`
	AnchorReaped        bool   `json:"anchor_reaped"`
	Detached            bool   `json:"detached_from_leader_group_and_session"`
	Reparented          bool   `json:"reparented_from_native_leader"`
	ReparentedToOwner   bool   `json:"reparented_to_exact_a_owner"`
	AdopterPID          int    `json:"adopter_pid"`
	pidfd               *os.File
}

type linuxV2OwnerSnapshot struct {
	AnchorPID        int    `json:"anchor_pid"`
	AnchorIdentity   string `json:"anchor_identity"`
	HelperPID        int    `json:"helper_pid"`
	HelperIdentity   string `json:"helper_identity"`
	MirrorPID        int    `json:"mirror_pid"`
	MirrorIdentity   string `json:"mirror_identity"`
	LauncherPID      int    `json:"launcher_pid"`
	LauncherIdentity string `json:"launcher_identity"`
	GatePID          int    `json:"gate_pid"`
	GateIdentity     string `json:"gate_identity"`
	OwnerUID         uint32 `json:"owner_uid"`
	LauncherUID      uint32 `json:"launcher_uid"`
}

type linuxV2LeafSnapshot struct {
	Version          int    `json:"version"`
	ParentBootIDHash string `json:"parent_boot_id_sha256"`
	ParentMountID    uint64 `json:"parent_mount_id"`
	ParentDevice     uint64 `json:"parent_device"`
	ParentInode      uint64 `json:"parent_inode"`
	ParentPathHash   string `json:"parent_path_sha256"`
	LeafNameHash     string `json:"leaf_name_sha256"`
	LeafDevice       uint64 `json:"leaf_device"`
	LeafInode        uint64 `json:"leaf_inode"`
}

type linuxV2CloneSnapshot struct {
	TargetPathHash string `json:"target_path_sha256"`
	TargetDevice   uint64 `json:"target_device"`
	TargetInode    uint64 `json:"target_inode"`
	OwnerUID       uint32 `json:"owner_uid"`
	CheckoutReady  bool   `json:"checkout_ready"`
}

type linuxV2GitHookSnapshot struct {
	MarkerRootHash                      string `json:"marker_root_sha256"`
	NoMarkersBeforeRun                  bool   `json:"no_markers_before_run"`
	PostCheckoutAbsentAtCheckoutCAS     bool   `json:"post_checkout_absent_at_checkout_cas"`
	PostCheckoutMarkerObservedAfterExec bool   `json:"post_checkout_marker_observed_after_exec"`
	PostCommitMarkerObserved            bool   `json:"post_commit_marker_observed"`
	PostCommitMarkerOwnerUID            uint32 `json:"post_commit_marker_owner_uid,omitempty"`
}

type linuxV2JournalSnapshot struct {
	LocalState             string `json:"local_state"`
	TerminalState          string `json:"terminal_state,omitempty"`
	TerminalVerdict        string `json:"terminal_verdict,omitempty"`
	HasCgroupReservation   bool   `json:"has_cgroup_reservation"`
	HasProcessAuthority    bool   `json:"has_process_authority"`
	HasProcessMarker       bool   `json:"has_process_marker"`
	HasCheckoutReceipt     bool   `json:"has_checkout_receipt"`
	HasPromotionReceipt    bool   `json:"has_promotion_receipt"`
	HasReleaseProof        bool   `json:"has_release_proof"`
	ReleaseProofVersion    int    `json:"release_proof_version,omitempty"`
	ReleaseDisposition     string `json:"release_disposition,omitempty"`
	StopReceiptVersion     int    `json:"stop_receipt_version,omitempty"`
	StopProofKind          string `json:"stop_proof_kind,omitempty"`
	StopIssuerPID          int    `json:"stop_issuer_pid,omitempty"`
	StopIssuerIdentity     string `json:"stop_issuer_identity,omitempty"`
	StopReaperPID          int    `json:"stop_reaper_pid,omitempty"`
	StopReaperIdentity     string `json:"stop_reaper_identity,omitempty"`
	CgroupKillAcknowledged bool   `json:"cgroup_kill_acknowledged"`
	CgroupPopulatedZero    bool   `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool   `json:"no_migration_observed"`
	ProducerFenced         bool   `json:"producer_fenced"`
	DescendantsReaped      bool   `json:"descendants_reaped"`
	NoUnprovenContainment  bool   `json:"no_unproven_containment"`
}

type linuxV2AnchorStopSnapshot struct {
	Version                int                  `json:"version"`
	RunID                  string               `json:"run_id"`
	Generation             int64                `json:"generation"`
	AnchorPID              int                  `json:"anchor_pid"`
	AnchorIdentitySHA256   string               `json:"anchor_identity_sha256"`
	HelperPID              int                  `json:"helper_pid"`
	HelperIdentitySHA256   string               `json:"helper_identity_sha256"`
	Leaf                   *linuxV2LeafSnapshot `json:"leaf"`
	ProducerFenceVerified  bool                 `json:"producer_fence_verified"`
	LeafMembershipVerified bool                 `json:"leaf_membership_verified"`
	GateReleaseAuthorized  bool                 `json:"gate_release_authorized"`
	HelperExitVerified     bool                 `json:"helper_exit_verified"`
	PinnedLeafFD           bool                 `json:"pinned_leaf_fd"`
	KillAttempted          bool                 `json:"kill_attempted"`
	KillAcknowledged       bool                 `json:"kill_acknowledged"`
	CgroupPopulatedZero    bool                 `json:"cgroup_populated_zero"`
	NoMigrationObserved    bool                 `json:"no_migration_observed"`
	ProducerFenced         bool                 `json:"producer_fenced"`
	DescendantsReaped      bool                 `json:"descendants_reaped"`
	ReapCount              uint64               `json:"reap_count"`
	ReapDigest             string               `json:"reap_digest,omitempty"`
}

type linuxV2ProviderActionSnapshot struct {
	ResourceID string `json:"resource_id"`
	Operation  string `json:"operation"`
	Outcome    string `json:"outcome"`
}

type linuxV2NativeFixture struct {
	value              config.Config
	configPath         string
	requestPath        string
	request            linuxV2OwnerWitnessRequest
	stateDir           string
	root               string
	artifactDir        string
	responsePath       string
	profile            config.AgentProfile
	workspace          config.Workspace
	owner              linuxgate.OwnerConfig
	outputPath         string
	markerDir          string
	helperResponseDrop string
	helperAction       string
	keepRoot           *bool
}

func linuxV2ReadOwnerWitnessRequest(path string, testCase linuxV2NativeWitnessCase, value config.Config, owner linuxgate.OwnerConfig) (linuxV2OwnerWitnessRequest, error) {
	return linuxV2ReadOwnerWitnessRequestWithBarrierState(path, testCase, value, owner, true)
}

func linuxV2ReadOwnerWitnessReplayRequest(path string, testCase linuxV2NativeWitnessCase, value config.Config, owner linuxgate.OwnerConfig) (linuxV2OwnerWitnessRequest, error) {
	return linuxV2ReadOwnerWitnessRequestWithBarrierState(path, testCase, value, owner, false)
}

func linuxV2ReadOwnerWitnessRequestWithBarrierState(
	path string,
	testCase linuxV2NativeWitnessCase,
	value config.Config,
	owner linuxgate.OwnerConfig,
	freshBarrier bool,
) (linuxV2OwnerWitnessRequest, error) {
	if err := linuxV2ValidateRootWitnessFile(path, owner); err != nil {
		return linuxV2OwnerWitnessRequest{}, fmt.Errorf("validate root-controlled owner-witness request: %w", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return linuxV2OwnerWitnessRequest{}, errors.New("read root-controlled owner-witness request")
	}
	var request linuxV2OwnerWitnessRequest
	if err := json.Unmarshal(contents, &request); err != nil {
		return linuxV2OwnerWitnessRequest{}, errors.New("decode owner-witness request v1")
	}
	if request.Version != 1 || request.Case != testCase.requestCase || request.Harness != testCase.harness || request.ReplayRequired ||
		request.RunKey.RunID == "" || request.RunKey.Generation <= 0 || request.Subject.Validate() != nil ||
		request.Subject.ResourceID != value.Runtime.RepositoryResourceID ||
		request.RunKey.RunID != request.Claim.RunID || request.RunKey.Generation != request.Claim.Generation ||
		request.Fence.Generation != request.RunKey.Generation || request.Fence.RuntimeID == "" || request.Fence.RuntimeEpoch <= 0 ||
		request.Fence.ClaimID == "" || request.Fence.ClaimID != request.Claim.ClaimID || request.Fence.LeaseToken == "" ||
		request.Fence.LeaseToken != request.Claim.LeaseToken || request.Claim.TaskID == "" ||
		request.Claim.LeaseRemainingMS <= 0 || !request.Claim.LeaseExpiresAt.After(time.Now()) ||
		request.Claim.Work.AgentProfile != value.Runtime.AgentProfile || request.Claim.Work.Workspace != value.Runtime.Workspace ||
		strings.TrimSpace(request.Claim.Work.Goal) == "" || !linuxV2WitnessClaimRequirementsMatch(testCase, request.Claim.Work.RequiredCapabilities) ||
		request.Claim.HarnessSessionID != nil || request.Claim.HarnessBindingID != nil {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness request does not bind the exact live claim, configured runtime, and Subject")
	}
	if _, present, err := protocol.ParseAdmissionInput(request.Claim.Work.Input); err != nil || present {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness claim must be a real non-Goal Control task")
	}
	if testCase.providerActionRequired {
		access := request.Claim.ProviderAccess
		if access == nil || access.Path != "/api/v1/provider-actions" || strings.TrimSpace(access.Token) == "" ||
			!linuxV2ClaimAllowsProviderSync(*access, request.Subject.ResourceID) {
			return linuxV2OwnerWitnessRequest{}, errors.New("Pi owner-witness claim lacks authentic Control provider_access for resource.sync")
		}
	} else if request.Claim.ProviderAccess != nil {
		return linuxV2OwnerWitnessRequest{}, errors.New("OpenCode owner-witness claim unexpectedly carries provider access")
	}
	barrier := request.Barrier
	expectedHold := "none"
	switch request.Case {
	case "pi_provider_action", "helper_sigkill_detached_child", "daemon_sigkill_helper_survives_detached_child":
		expectedHold = "native_turn_started"
	case "opencode_native_health":
		expectedHold = "native_release"
	case "stage_replay":
		expectedHold = testCase.barrierStage
	}
	if !filepath.IsAbs(barrier.Directory) || filepath.Clean(barrier.Directory) != barrier.Directory ||
		!linuxV2PathWithin(barrier.Directory, "/opt/symmetry-owner-test") || barrier.TimeoutSeconds < 1 || barrier.TimeoutSeconds > 30 ||
		barrier.HoldAfter != expectedHold {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier directory or timeout is invalid")
	}
	validHolds := map[string]bool{
		"none": true, "reservation_durable": true, "create_started": true, "leaf_bound": true, "helper_bound": true,
		"launcher_bound": true, "gate_bound": true, "handoff_prepared": true, "handoff_committed": true,
		"lease_armed": true, "checkout_ready": true, "native_release": true, "native_turn_started": true,
		"stop_receipt_durable": true, "leaf_removed": true, "journal_cleared": true,
	}
	if !validHolds[barrier.HoldAfter] {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier hold_after is unsupported")
	}
	if request.Case == "stage_replay" && (barrier.HoldAfter == "none" || barrier.HoldAfter != testCase.barrierStage) {
		return linuxV2OwnerWitnessRequest{}, errors.New("stage-replay request does not name its exact matrix boundary")
	}
	if testCase.ackLossReplay {
		if request.Case != "stage_replay" || request.DropAckAfter == "" || request.DropAckAfter != barrier.HoldAfter ||
			!linuxV2AckLossReplayStageSupported(request.DropAckAfter) {
			return linuxV2OwnerWitnessRequest{}, errors.New("response-loss request must drop exactly one ACK at its exact held StageReplay boundary")
		}
	} else if request.DropAckAfter != "" {
		return linuxV2OwnerWitnessRequest{}, errors.New("ACK-loss injection is only allowed in its dedicated StageReplay fixture")
	}
	barrierInfo, err := os.Lstat(barrier.Directory)
	if err != nil || !barrierInfo.IsDir() {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier directory is unavailable")
	}
	barrierIdentity, ok := barrierInfo.Sys().(*syscall.Stat_t)
	if !ok || barrierIdentity.Uid != owner.AUID || barrierIdentity.Mode&0o7777 != 0o700 {
		return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier directory must be A-owned mode 0700")
	}
	if freshBarrier {
		entries, err := os.ReadDir(barrier.Directory)
		if err != nil || len(entries) != 0 {
			return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier directory must be empty before its request")
		}
	}
	if barrier.HoldAfter != "none" {
		if !filepath.IsAbs(barrier.ReleaseFile) || filepath.Clean(barrier.ReleaseFile) != barrier.ReleaseFile ||
			filepath.Base(barrier.ReleaseFile) != "release" || !linuxV2PathWithin(barrier.ReleaseFile, barrier.Directory) || barrier.TimeoutSeconds != 30 {
			return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier release path is outside its private directory")
		}
		if freshBarrier {
			if _, err := os.Lstat(barrier.ReleaseFile); !errors.Is(err, os.ErrNotExist) {
				return linuxV2OwnerWitnessRequest{}, errors.New("owner-witness barrier release marker already exists")
			}
		} else {
			released, err := linuxV2OwnerWitnessReleaseMarkerExists(linuxV2NativeFixture{request: request, owner: owner})
			if err != nil || !released {
				return linuxV2OwnerWitnessRequest{}, errors.New("replay request barrier is not the exact previously released boundary")
			}
		}
	} else if barrier.ReleaseFile != "" {
		return linuxV2OwnerWitnessRequest{}, errors.New("unheld owner-witness request must not provide a release marker")
	}
	return request, nil
}

func linuxV2AckLossReplayStageSupported(stage string) bool {
	for _, supported := range linuxV2AckLossReplayStages {
		if stage == supported {
			return true
		}
	}
	return false
}

func linuxV2WitnessClaimRequirementsMatch(testCase linuxV2NativeWitnessCase, actual map[string]bool) bool {
	if !testCase.providerActionRequired {
		return len(actual) == 0
	}
	return len(actual) == 1 && actual["provider_access"]
}

func linuxV2ValidateRootWitnessFile(path string, owner linuxgate.OwnerConfig) error {
	return linuxV2ValidateRootWitnessFileForGroup(path, owner.AGID)
}

func linuxV2ValidateRootWitnessFileForGroup(path string, groupID uint32) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("fixture is not a regular root-controlled file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != groupID || stat.Nlink != 1 || stat.Mode&0o0022 != 0 || info.Size() <= 0 || info.Size() > 1<<20 {
		return errors.New("fixture must be bounded, root-owned, A-group-readable, and not group/world-writable")
	}
	if err := unix.Access(path, unix.R_OK); err != nil {
		return errors.New("A cannot read the root-controlled fixture")
	}
	return nil
}

func linuxV2ClaimAllowsProviderSync(access protocol.ProviderAccess, resourceID string) bool {
	for _, grant := range access.Grants {
		if grant.ResourceID != resourceID {
			continue
		}
		for _, operation := range grant.Operations {
			if operation == "resource.sync" {
				return true
			}
		}
	}
	return false
}

func requireLinuxV2ControlEnvironment(t *testing.T) e2eEnvironment {
	t.Helper()
	if os.Getenv("SYMMETRY_E2E") != "1" {
		t.Fatal("SYMMETRY_E2E=1 is required when a production Linux v2 witness is enabled")
	}
	for _, name := range []string{"SYMMETRY_E2E_URL", "SYMMETRY_ENROLLMENT_TOKEN", "SYMMETRY_OPERATOR_TOKEN"} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			t.Fatalf("%s is required when a production Linux v2 witness is enabled", name)
		}
	}
	return loadControlEnvironment(t)
}

func newLinuxV2NativeFixture(t *testing.T, testCase linuxV2NativeWitnessCase) linuxV2NativeFixture {
	t.Helper()
	configPath := strings.TrimSpace(os.Getenv(testCase.configEnvironment))
	if !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath {
		t.Fatalf("%s must name a canonical absolute strict daemon config", testCase.configEnvironment)
	}
	if !linuxV2PathWithin(configPath, "/opt/symmetry-owner-test") {
		t.Fatalf("%s config must remain beneath the root-controlled test fixture", testCase.configEnvironment)
	}
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatalf("read root-installed fixed Linux A/B config: %v", err)
	}
	if err := linuxV2ValidateRootWitnessFile(configPath, owner); err != nil {
		t.Fatalf("validate root-controlled %s config: %v", testCase.configEnvironment, err)
	}
	value, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load %s: invalid production v2 daemon config", testCase.configEnvironment)
	}
	if value.Runtime.HarnessKind != testCase.harness {
		t.Fatalf("%s runtime.harness_kind = %q, want %q", testCase.configEnvironment, value.Runtime.HarnessKind, testCase.harness)
	}
	expectedVersion := map[string]string{"pi": "0.87.1", "opencode": "1.18.32"}[testCase.harness]
	if value.Runtime.HarnessVersion != expectedVersion {
		t.Fatalf("%s runtime.harness_version = %q, want pinned release %q", testCase.configEnvironment, value.Runtime.HarnessVersion, expectedVersion)
	}
	if value.Runtime.HarnessVersion == "legacy" || value.Runtime.AdapterVersion == "legacy" || value.Runtime.AdapterProtocolVersion <= 0 {
		t.Fatalf("%s must pin non-legacy native/adapter versions", testCase.configEnvironment)
	}
	profile, ok := value.AgentProfiles[value.Runtime.AgentProfile]
	if !ok || strings.TrimSpace(profile.Command) == "" {
		t.Fatalf("%s runtime.agent_profile must name a configured real native executable", testCase.configEnvironment)
	}
	if !filepath.IsAbs(profile.Command) || filepath.Clean(profile.Command) != profile.Command {
		t.Fatalf("%s native executable must be an absolute canonical path", testCase.configEnvironment)
	}
	if err := linuxV2VerifyPinnedNativeCLI(profile.Command, expectedVersion); err != nil {
		t.Fatalf("%s does not point to the pinned real CLI release: %v", testCase.configEnvironment, err)
	}
	if testCase.providerActionRequired && !profile.ProviderAccess {
		t.Fatalf("%s Pi profile must enable provider_access for the real provider action witness", testCase.configEnvironment)
	}
	if testCase.harness == "opencode" && profile.ProviderAccess {
		t.Fatalf("%s OpenCode profile must not claim unsupported provider-broker access", testCase.configEnvironment)
	}
	workspace, ok := value.Workspaces[value.Runtime.Workspace]
	if !ok || workspace.Policy != config.WorkspacePolicyGitCloneV2 || workspace.Path != "" || workspace.Ref != "" ||
		workspace.Repository == "" || workspace.Root == "" || workspace.SeedRoot == "" {
		t.Fatalf("%s must provide the exact git_clone_v2 Repository/Root/SeedRoot binding", testCase.configEnvironment)
	}
	if strings.TrimSpace(value.Runtime.RepositoryResourceID) == "" {
		t.Fatalf("%s runtime.repository_resource_id is required", testCase.configEnvironment)
	}
	requestPath := strings.TrimSpace(os.Getenv(testCase.requestEnvironment))
	if !filepath.IsAbs(requestPath) || filepath.Clean(requestPath) != requestPath || !linuxV2PathWithin(requestPath, "/opt/symmetry-owner-test") {
		t.Fatalf("%s must name a canonical request file beneath /opt/symmetry-owner-test", testCase.requestEnvironment)
	}
	markerRoot := strings.TrimSpace(os.Getenv(linuxV2NativeHookMarkerRootEnvironment))
	if !filepath.IsAbs(markerRoot) || filepath.Clean(markerRoot) != markerRoot {
		t.Fatalf("%s must be a canonical absolute A-readable/B-writable marker root", linuxV2NativeHookMarkerRootEnvironment)
	}
	markerSubdirectory := testCase.markerDirectory
	if markerSubdirectory == "" {
		markerSubdirectory = testCase.harness
	}
	markerDir := filepath.Join(markerRoot, markerSubdirectory)
	if !slices.Contains(profile.EnvAllowlist, linuxV2NativeHookMarkerChildEnvironment) {
		t.Fatalf("%s profile EnvAllowlist must include only the non-secret native hook marker root", testCase.configEnvironment)
	}
	markerDirectory, err := linuxV2OpenPinnedHookMarkerDirectory(markerDir, owner)
	if err != nil {
		t.Fatalf("pin A-readable/B-writable marker directory for %s: %v", testCase.harness, err)
	}
	entries, err := markerDirectory.ReadDir(-1)
	closeMarkerDirErr := markerDirectory.Close()
	if err != nil || closeMarkerDirErr != nil || len(entries) != 0 {
		t.Fatalf("native hook marker directory for %s must be readable and empty before the witness: read=%v close=%v", testCase.harness, err, closeMarkerDirErr)
	}
	request, err := linuxV2ReadOwnerWitnessRequest(requestPath, testCase, value, owner)
	if err != nil {
		t.Fatalf("load isolated production owner witness request: %v", err)
	}
	if uint32(os.Geteuid()) != owner.AUID || uint32(os.Getegid()) != owner.AGID || owner.AUID == owner.BUID || owner.AGID == owner.BGID {
		t.Fatalf("production v2 witness must run as the exact non-root A UID/GID with a distinct fixed B identity")
	}
	if err := linuxV2ValidateFixedBSecretFiles(owner, testCase.harness); err != nil {
		t.Fatalf("verify fixed-B native config/credential file boundaries: %v", err)
	}
	stateDir := value.StateDir
	if owner.StateDir == "" || stateDir != owner.StateDir {
		t.Fatalf("%s StateDir %q must equal the root-installed cleanup-verifier StateDir %q", testCase.configEnvironment, stateDir, owner.StateDir)
	}
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		t.Fatal("StateDir must be canonical and absolute")
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil || !stateInfo.IsDir() {
		t.Fatalf("StateDir must already be an A-private directory: %v", err)
	}
	stateIdentity, ok := stateInfo.Sys().(*syscall.Stat_t)
	if !ok || stateIdentity.Uid != owner.AUID || stateIdentity.Gid != owner.AGID || stateIdentity.Mode&0o7777 != 0o700 {
		t.Fatal("StateDir must be A-owned mode 0700, matching the root-controlled config")
	}
	priorJournals, err := linuxV2SnapshotStoreJournals(stateDir)
	if err != nil {
		t.Fatalf("snapshot pre-existing retained RunJournals before the new RunKey: %v", err)
	}
	for key, snapshot := range priorJournals {
		if err := linuxV2RequireSafelyReleasedPriorJournal(value, snapshot); err != nil {
			t.Fatalf("pre-existing RunJournal %s/%d is not physically released and safe for a fresh run: %v", key.RunID, key.Generation, err)
		}
	}
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	if _, reused := priorJournals[key]; reused {
		t.Fatalf("new production witness RunKey %s/%d is already present in shared StateDir", key.RunID, key.Generation)
	}
	root, err := os.MkdirTemp(os.TempDir(), "symmetry-linux-v2-native-witness-")
	if err != nil {
		t.Fatalf("create private A owner-witness scratch root: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("secure private A witness root: %v", err)
	}
	artifactDir := filepath.Join(root, "artifacts")
	if err := os.Mkdir(artifactDir, 0o700); err != nil {
		t.Fatalf("create private v2 production witness artifact directory: %v", err)
	}
	fixture := linuxV2NativeFixture{
		value: value, configPath: configPath, requestPath: requestPath, request: request,
		stateDir: stateDir, root: root, artifactDir: artifactDir, responsePath: filepath.Join(artifactDir, "owner-witness-response.json"), profile: profile,
		workspace: workspace, owner: owner, keepRoot: new(bool),
		markerDir: markerDir, helperResponseDrop: testCase.helperResponseDrop, helperAction: testCase.helperAction,
	}
	t.Cleanup(func() {
		currentJournals, err := linuxV2SnapshotStoreJournals(stateDir)
		if err != nil {
			t.Errorf("read shared StateDir after production witness: %v", err)
		} else if err := linuxV2RequireStoreJournalHistoryPreserved(priorJournals, currentJournals, &key); err != nil {
			t.Errorf("production witness changed a previously retained RunJournal: %v", err)
		}
		if t.Failed() || *fixture.keepRoot {
			t.Logf("production Linux v2 StateDir remains at fixture-provided path %s; private scratch retained at %s", stateDir, root)
			return
		}
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove successful A-private v2 witness root: %v", err)
		}
	})
	return fixture
}

func linuxV2ValidateFixedBSecretFiles(owner linuxgate.OwnerConfig, harnessKind string) error {
	account, err := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
	if err != nil || account.Uid != strconv.FormatUint(uint64(owner.BUID), 10) ||
		account.Gid != strconv.FormatUint(uint64(owner.BGID), 10) || account.HomeDir == "" ||
		!filepath.IsAbs(account.HomeDir) || filepath.Clean(account.HomeDir) != account.HomeDir {
		return errors.New("fixed B HOME account is unavailable")
	}
	paths := []string{}
	switch harnessKind {
	case "pi":
		paths = append(paths, filepath.Join(account.HomeDir, ".pi", "agent", "auth.json"))
	case "opencode":
		paths = append(paths,
			filepath.Join(account.HomeDir, ".local", "share", "opencode", "auth.json"),
			filepath.Join(account.HomeDir, ".config", "opencode", "opencode.json"),
		)
	default:
		return errors.New("native witness harness is unsupported")
	}
	directories := map[string]struct{}{account.HomeDir: {}}
	for _, path := range paths {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			if parent != account.HomeDir && !linuxV2PathWithin(parent, account.HomeDir) {
				return errors.New("fixed-B credential parent escaped passwd HOME")
			}
			directories[parent] = struct{}{}
			if parent == account.HomeDir {
				break
			}
		}
	}
	for directory := range directories {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() {
			return errors.New("fixed B HOME or native credential parent is unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner.BUID || stat.Gid != owner.BGID || stat.Mode&0o7000 != 0 ||
			stat.Mode&0o700 != 0o700 || stat.Mode&0o0060 != 0 || stat.Mode&0o0007 != 0 {
			return errors.New("fixed B HOME or credential parent has unsafe owner/group/mode")
		}
		if err := unix.Access(directory, unix.X_OK); err != nil {
			return errors.New("A lacks search access needed to stat the fixed-B native credential")
		}
		if err := unix.Access(directory, unix.R_OK); !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
			return errors.New("A can list a fixed-B HOME or native credential directory")
		}
		if err := unix.Access(directory, unix.W_OK); !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
			return errors.New("A can modify a fixed-B HOME or native credential directory")
		}
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("fixed-B real native CLI config or credential file is unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner.BUID || stat.Gid != owner.BGID || stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 || info.Size() <= 0 {
			return errors.New("fixed-B real native CLI config or credential file has unsafe ownership")
		}
		if err := unix.Access(path, unix.R_OK); !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
			return errors.New("A can read a fixed-B native CLI config or credential file")
		}
		if err := unix.Access(path, unix.W_OK); !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
			return errors.New("A can modify a fixed-B native CLI config or credential file")
		}
	}
	return nil
}

func linuxV2RequireActiveOwnerService() error {
	command := exec.Command("systemctl", "is-active", "--quiet", "symmetry-owner.service")
	command.Env = linuxV2SafeHostEnvironment()
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Run(); err != nil {
		return errors.New("symmetry-owner.service is not active")
	}
	return nil
}

func linuxV2VerifyPinnedNativeCLI(executable, expectedVersion string) error {
	command := exec.Command(executable, "--version")
	command.Env = linuxV2SafeHostEnvironment()
	output, err := command.Output()
	if err != nil {
		return errors.New("pinned native executable could not run its version command")
	}
	if strings.TrimSpace(string(output)) != expectedVersion {
		return errors.New("native executable version differs from the tested release")
	}
	return nil
}

func linuxV2SafeHostEnvironment() []string {
	allowed := map[string]struct{}{"PATH": {}, "HOME": {}, "LANG": {}, "LC_ALL": {}, "TZ": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {}}
	environment := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; ok {
			environment = append(environment, entry)
		}
	}
	return environment
}

func verifyLinuxV2ProductionInstall(t *testing.T, binary, configPath, workDir string) {
	t.Helper()
	sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open production verifier output sink: %v", err)
	}
	defer sink.Close()
	command := exec.Command(binary, "-verify-linux-owner-install", "-config", configPath)
	command.Dir = workDir
	command.Env = linuxV2SafeHostEnvironment()
	command.Stdout = sink
	command.Stderr = command.Stdout
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("configure installed Linux owner verification: %v", err)
	}
	if err := command.Run(); err != nil {
		t.Fatal("production binary rejected the installed root launcher/static gate/A-B systemd configuration")
	}
}

func startLinuxV2NativeDaemonWitness(t *testing.T, binary, configPath, workDir, enrollmentToken, markerDir string, allowlist []string) *productionDaemonWitnessProcess {
	t.Helper()
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open production daemon output sink: %v", err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open production daemon diagnostic sink: %v", err)
	}
	command := exec.Command(binary, "-config", configPath)
	command.Dir = workDir
	command.Stdout = stdout
	command.Stderr = stderr
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("configure production Linux v2 daemon process: %v", err)
	}
	command.Env = linuxV2DaemonEnvironment(enrollmentToken, allowlist, map[string]string{linuxV2NativeHookMarkerChildEnvironment: markerDir})
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start production Linux v2 daemon as A: %v", err)
	}
	return &productionDaemonWitnessProcess{command: command, stdout: stdout, stderr: stderr}
}

func linuxV2DaemonEnvironment(enrollmentToken string, allowlist []string, overrides map[string]string) []string {
	allowed := map[string]struct{}{"SYMMETRY_ENROLLMENT_TOKEN": {}}
	for _, entry := range linuxV2SafeHostEnvironment() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		allowed[key] = struct{}{}
	}
	for _, name := range allowlist {
		if linuxV2IsCredentialEnvironmentName(name) {
			continue
		}
		allowed[name] = struct{}{}
	}
	environment := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; ok && key != "SYMMETRY_ENROLLMENT_TOKEN" {
			environment = append(environment, entry)
		}
	}
	for key, value := range overrides {
		prefix := key + "="
		filtered := environment[:0]
		for _, entry := range environment {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		environment = append(filtered, prefix+value)
	}
	return append(environment, "SYMMETRY_ENROLLMENT_TOKEN="+enrollmentToken)
}

func linuxV2IsCredentialEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_CREDENTIAL", "_ACCESS_KEY", "_KEY_ID", "_CLIENT_SECRET"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return upper == "SYMMETRY_API_KEY" || upper == "SYMMETRY_CREDENTIAL"
}

func stopLinuxV2NativeDaemonGracefully(t *testing.T, process *productionDaemonWitnessProcess) {
	t.Helper()
	if process == nil || process.waited || process.command == nil || process.command.Process == nil {
		return
	}
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("send SIGTERM to production Linux v2 daemon: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- process.command.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("production Linux v2 daemon did not stop gracefully")
	}
	process.waited = true
	if err := process.stdout.Close(); err != nil {
		t.Fatalf("close production daemon output sink: %v", err)
	}
	if err := process.stderr.Close(); err != nil {
		t.Fatalf("close production daemon diagnostic sink: %v", err)
	}
}

func linuxV2InjectHelperSIGKILL(
	fixture linuxV2NativeFixture,
	journal state.RunJournal,
	live *linuxV2ProcessSnapshot,
) (linuxV2HelperCrashSnapshot, error) {
	if journal.Key() != (state.RunKey{RunID: fixture.request.RunKey.RunID, Generation: fixture.request.RunKey.Generation}) ||
		journal.Fence() != fixture.request.Fence {
		return linuxV2HelperCrashSnapshot{}, errors.New("helper-crash RunJournal differs from the exact requested RunKey/fence")
	}
	if live == nil || live.pidfd == nil || live.detachedChild == nil ||
		journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil || journal.ContainmentCgroup.Gate == nil {
		return linuxV2HelperCrashSnapshot{}, errors.New("exact daemon/helper/leader/child crash subjects are incomplete")
	}
	helper := *journal.ContainmentCgroup.Helper
	helperFD, err := linuxV2OpenExactPIDFD(helper.PID, helper.Identity)
	if err != nil {
		return linuxV2HelperCrashSnapshot{}, fmt.Errorf("pin exact A helper pidfd: %w", err)
	}
	defer helperFD.Close()
	helperCredentials, err := linuxV2ReadProcessCredentials(helper.PID)
	if err != nil || helperCredentials.effectiveUID != fixture.owner.AUID || helperCredentials.effectiveGID != fixture.owner.AGID {
		return linuxV2HelperCrashSnapshot{}, errors.New("recorded Linux v2 helper does not run as A")
	}
	result := linuxV2HelperCrashSnapshot{
		HelperPID: helper.PID, HelperIdentity: helper.Identity, HelperPIDFDVerified: true,
		LeaderPID: live.PID, LeaderIdentity: live.Identity,
		AnchorPID: live.owners.AnchorPID, AnchorIdentity: live.owners.AnchorIdentity,
		ChildPID: live.detachedChild.PID, ChildIdentity: live.detachedChild.Identity,
	}
	helperKilled := false
	defer func() {
		if !helperKilled {
			_ = unix.PidfdSendSignal(int(helperFD.Fd()), unix.SIGKILL, nil, 0)
		}
	}()
	if err := unix.PidfdSendSignal(int(helperFD.Fd()), unix.SIGSTOP, nil, 0); err != nil {
		return result, fmt.Errorf("SIGSTOP exact Linux v2 helper by pidfd: %w", err)
	}
	if err := waitForLinuxWitnessProcessStopped(helper.PID, helper.Identity, 5*time.Second); err != nil {
		return result, fmt.Errorf("wait for exact helper SIGSTOP: %w", err)
	}
	result.HelperSIGSTOPVerified = true
	if err := linuxV2WriteBHookReleaseMarker(fixture, "post-commit-child.release"); err != nil {
		return result, fmt.Errorf("release bounded B detached-child fixture: %w", err)
	}
	if err := linuxV2WaitPIDFDExited(int(live.pidfd.Fd()), 90*time.Second); err != nil {
		return result, fmt.Errorf("wait for the B leader to exit after the detached-child fixture release: %w", err)
	}
	result.LeaderExitObserved = true
	if err := linuxV2RequireLivePIDFD(int(live.detachedChild.pidfd.Fd())); err != nil {
		return result, errors.New("detached B child did not remain live after the native leader exited")
	}
	parentPID, _, _, err := linuxV2ReadProcessGroups(live.detachedChild.PID)
	if err != nil || parentPID != helper.PID {
		return result, errors.New("detached B child was not adopted by the stopped exact helper before helper loss")
	}
	result.ChildLiveAfterLeaderExit = true
	result.LeaderExitedWhileChildLive = result.LeaderExitObserved && result.ChildLiveAfterLeaderExit
	if err := unix.PidfdSendSignal(int(helperFD.Fd()), unix.SIGKILL, nil, 0); err != nil {
		return result, fmt.Errorf("SIGKILL exact Linux v2 helper by pidfd: %w", err)
	}
	if err := linuxV2WaitPIDFDExited(int(helperFD.Fd()), 15*time.Second); err != nil {
		return result, fmt.Errorf("wait for exact Linux v2 helper SIGKILL: %w", err)
	}
	helperKilled = true
	result.HelperSIGKILLVerified = true
	if err := linuxV2WaitPIDFDExited(int(live.detachedChild.pidfd.Fd()), 20*time.Second); err != nil {
		return result, fmt.Errorf("anchor did not stop the exact adopted detached child: %w", err)
	}
	if err := waitForLinuxWitnessIdentityGone(live.detachedChild.PID, live.detachedChild.Identity, 5*time.Second); err != nil {
		return result, fmt.Errorf("anchor did not reap the exact detached child: %w", err)
	}
	grant, err := linuxV2OwnerAnchorGrantFromJournal(journal, journal.ContainmentCgroup.Clone())
	if err != nil {
		return result, fmt.Errorf("construct exact helper-death anchor grant: %w", err)
	}
	anchorClient, err := platform.NewLinuxOwnerAnchorClient(fixture.value.LinuxOwnerSocket)
	if err != nil {
		return result, fmt.Errorf("connect to exact A anchor after helper death: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	runReap, err := anchorClient.WaitReapRun(ctx, grant, helper)
	cancel()
	if err != nil {
		return result, fmt.Errorf("read exact cached anchor WaitReapRun/StopLeaf result: %w", err)
	}
	anchor := journal.ContainmentCgroup.Anchor
	if err := runReap.Stop.ValidateFor(grant, helper, anchor, runReap.Processes, true); err != nil {
		return result, fmt.Errorf("validate anchor StopLeaf evidence and complete exact reap set: %w", err)
	}
	if runReap.Stop.ProducerFence != nil || runReap.Stop.Gate == nil ||
		!runReap.Stop.Gate.Equal(*journal.ContainmentCgroup.Gate) || runReap.Stop.HelperExit == nil ||
		!runReap.Stop.GateReleaseAuthorized || !runReap.Stop.HelperExit.ValidFor(helper, anchor) || !runReap.Stop.DescendantsReaped ||
		!linuxV2AnchorRunReapHasProcess(runReap, live.detachedChild.PID, live.detachedChild.Identity, "descendant") {
		return result, errors.New("anchor cached StopLeaf result does not bind helper death and exact detached-child reaping")
	}
	stopEvidence := runReap.Stop.Clone()
	result.anchorStop = &stopEvidence
	result.anchorRunReap = &runReap
	if err := releaseLinuxV2OwnerWitnessBarrier(fixture); err != nil {
		return result, fmt.Errorf("release hidden owner-witness stage after anchor reaping: %w", err)
	}
	return result, nil
}

func linuxV2AnchorRunReapHasProcess(
	result platform.LinuxOwnerAnchorRunReapResult,
	pid int,
	identity string,
	role string,
) bool {
	for _, evidence := range result.Processes {
		if evidence.Process.PID == pid && evidence.Process.Identity == identity && evidence.ProcessRole == role {
			return true
		}
	}
	return false
}

func linuxV2InjectDaemonSIGKILL(
	daemon *productionDaemonWitnessProcess,
	fixture linuxV2NativeFixture,
	journal state.RunJournal,
	live *linuxV2ProcessSnapshot,
) (linuxV2DaemonCrashSnapshot, error) {
	if daemon == nil || daemon.command == nil || daemon.command.Process == nil || live == nil || live.pidfd == nil || live.detachedChild == nil ||
		journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil {
		return linuxV2DaemonCrashSnapshot{}, errors.New("exact daemon/helper/leader/child crash subjects are incomplete")
	}
	helper := *journal.ContainmentCgroup.Helper
	helperFD, err := linuxV2OpenExactPIDFD(helper.PID, helper.Identity)
	if err != nil {
		return linuxV2DaemonCrashSnapshot{}, fmt.Errorf("pin exact A helper pidfd: %w", err)
	}
	defer helperFD.Close()
	helperCredentials, err := linuxV2ReadProcessCredentials(helper.PID)
	if err != nil || helperCredentials.effectiveUID != fixture.owner.AUID || helperCredentials.effectiveGID != fixture.owner.AGID {
		return linuxV2DaemonCrashSnapshot{}, errors.New("recorded Linux v2 helper does not run as A")
	}
	daemonPID := daemon.command.Process.Pid
	daemonIdentity, err := platform.ProcessIdentity(daemonPID)
	if err != nil {
		return linuxV2DaemonCrashSnapshot{}, errors.New("read production daemon creation identity before SIGKILL")
	}
	daemonFD, err := linuxV2OpenExactPIDFD(daemonPID, daemonIdentity)
	if err != nil {
		return linuxV2DaemonCrashSnapshot{}, fmt.Errorf("pin exact production daemon pidfd: %w", err)
	}
	defer daemonFD.Close()
	if err := linuxV2RequireLivePIDFD(int(live.pidfd.Fd())); err != nil {
		return linuxV2DaemonCrashSnapshot{}, errors.New("native leader is not live before daemon SIGKILL")
	}
	if err := linuxV2RequireLivePIDFD(int(live.detachedChild.pidfd.Fd())); err != nil {
		return linuxV2DaemonCrashSnapshot{}, errors.New("detached B child is not live before daemon SIGKILL")
	}
	if err := unix.PidfdSendSignal(int(daemonFD.Fd()), unix.SIGKILL, nil, 0); err != nil {
		return linuxV2DaemonCrashSnapshot{}, fmt.Errorf("SIGKILL exact production daemon by pidfd: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.command.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		return linuxV2DaemonCrashSnapshot{}, errors.New("SIGKILLed production daemon did not exit")
	}
	daemon.waited = true
	if err := daemon.stdout.Close(); err != nil {
		return linuxV2DaemonCrashSnapshot{}, fmt.Errorf("close crashed daemon output sink: %w", err)
	}
	if err := daemon.stderr.Close(); err != nil {
		return linuxV2DaemonCrashSnapshot{}, fmt.Errorf("close crashed daemon diagnostic sink: %w", err)
	}
	result := linuxV2DaemonCrashSnapshot{
		DaemonPID: daemonPID, DaemonIdentity: daemonIdentity, DaemonPIDFDVerified: true, DaemonSIGKILLVerified: true,
		HelperPID: helper.PID, HelperIdentity: helper.Identity, HelperPIDFDVerified: true,
		LeaderPID: live.PID, LeaderIdentity: live.Identity,
		DetachedChildPID: live.detachedChild.PID, DetachedChildIdentity: live.detachedChild.Identity,
		DetachedChildLiveAtDaemonKill: true,
	}
	if err := linuxV2RequireLivePIDFD(int(helperFD.Fd())); err != nil {
		return result, errors.New("exact A helper did not survive production daemon SIGKILL")
	}
	result.HelperSurvivedDaemonSIGKILL = true
	if err := linuxV2WaitPIDFDExited(int(live.pidfd.Fd()), 30*time.Second); err != nil {
		return result, fmt.Errorf("helper did not stop the exact native leader after daemon loss: %w", err)
	}
	if err := linuxV2WaitPIDFDExited(int(live.detachedChild.pidfd.Fd()), 30*time.Second); err != nil {
		return result, fmt.Errorf("helper did not stop the exact detached child after daemon loss: %w", err)
	}
	return result, nil
}

func linuxV2OpenExactPIDFD(pid int, identity string) (*os.File, error) {
	if pid <= 1 || identity == "" {
		return nil, errors.New("exact pidfd target identity is incomplete")
	}
	actual, err := platform.ProcessIdentity(pid)
	if err != nil || actual != identity {
		return nil, errors.New("numeric PID does not match its exact process creation identity")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "linux-v2-exact-pidfd")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap exact pidfd")
	}
	pidfdPID, err := linuxV2PIDFDInfoPID(fd)
	if err != nil || pidfdPID != pid {
		_ = file.Close()
		return nil, errors.New("exact pidfd fdinfo does not match its process identity")
	}
	if actual, err := platform.ProcessIdentity(pid); err != nil || actual != identity {
		_ = file.Close()
		return nil, errors.New("process creation identity changed across pidfd open")
	}
	return file, nil
}

func linuxV2WaitPIDFDExited(pidfd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: int16(unix.POLLIN)}}
		if _, err := unix.Poll(fds, 100); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("poll exact process pidfd exit: %w", err)
		}
		if fds[0].Revents&int16(unix.POLLIN) != 0 {
			return nil
		}
	}
	return errors.New("exact process pidfd remained live beyond the bounded wait")
}

func linuxV2JournalHasPhysicalRelease(journal state.RunJournal) bool {
	proof := journal.ContainmentCgroupReleaseProof
	return proof != nil && proof.Validate() == nil && proof.Version == authority.SupervisorHandoffVersionV2 &&
		proof.StopReceipt != nil && proof.StopReceipt.LinuxProof != nil &&
		journal.ContainmentCgroup == nil && journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil &&
		!journal.HasProcessDetails() && !journal.ContainmentUnproven && journal.ContainmentScans == nil
}

func assertLinuxV2HelperSurvivedDaemonCrash(t *testing.T, journal state.RunJournal, live *linuxV2ProcessSnapshot, crash *linuxV2DaemonCrashSnapshot) {
	t.Helper()
	if crash == nil || live == nil || live.detachedChild == nil || journal.ContainmentCgroupReleaseProof == nil ||
		journal.ContainmentCgroupReleaseProof.StopReceipt == nil || journal.ContainmentCgroupReleaseProof.StopReceipt.LinuxProof == nil {
		t.Fatal("daemon-loss witness lacks its exact detached child or receipt-backed v2 stop proof")
	}
	proof := journal.ContainmentCgroupReleaseProof.StopReceipt.LinuxProof
	helper := authority.LinuxCgroupProcessIdentity{PID: crash.HelperPID, Identity: crash.HelperIdentity}
	if !crash.DaemonSIGKILLVerified || !crash.HelperSurvivedDaemonSIGKILL ||
		proof.Kind != authority.LinuxCgroupStopProofHelper || !proof.Issuer.Equal(helper) || !proof.Reaper.Equal(helper) ||
		proof.HelperExit != nil || !proof.DescendantsReaped {
		t.Fatal("stop receipt does not prove the live exact helper survived daemon SIGKILL and reaped the detached child")
	}
	crash.StopProofKind = proof.Kind
	crash.StopProofReaperPID = proof.Reaper.PID
	crash.StopProofReaperIdentity = proof.Reaper.Identity
}

func assertLinuxV2AnchorReapedHelperChild(t *testing.T, journal state.RunJournal, live *linuxV2ProcessSnapshot, crash *linuxV2HelperCrashSnapshot) {
	t.Helper()
	if journal.ContainmentCgroupReleaseProof == nil || journal.ContainmentCgroupReleaseProof.StopReceipt == nil ||
		journal.ContainmentCgroupReleaseProof.StopReceipt.LinuxProof == nil || live == nil || live.detachedChild == nil || crash == nil ||
		journal.ContainmentCgroupReleaseProof.StopReceipt.CgroupIdentity == nil ||
		crash.anchorStop == nil || crash.anchorRunReap == nil {
		t.Fatal("anchor adoption/reap witness lacks the exact released RunJournal or detached child")
	}
	proof := journal.ContainmentCgroupReleaseProof.StopReceipt.LinuxProof
	anchorStop := crash.anchorStop
	runReap := crash.anchorRunReap
	anchor := authority.LinuxCgroupProcessIdentity{PID: crash.AnchorPID, Identity: crash.AnchorIdentity}
	helper := authority.LinuxCgroupProcessIdentity{PID: crash.HelperPID, Identity: crash.HelperIdentity}
	if proof.Kind != authority.LinuxCgroupStopProofMirror || !proof.Reaper.Equal(anchor) || proof.HelperExit == nil ||
		!proof.HelperExit.Process.Equal(helper) || !proof.HelperExit.Witness.Equal(anchor) || !proof.HelperExit.Waited || !proof.HelperExit.Reaped ||
		!proof.DescendantsReaped || !crash.LeaderExitedWhileChildLive || !crash.HelperSIGKILLVerified ||
		!anchorStop.Equal(runReap.Stop) || anchorStop.HelperExit == nil || !anchorStop.HelperExit.Equal(*proof.HelperExit) ||
		anchorStop.Gate == nil || anchorStop.Gate.Process.PID != live.PID || anchorStop.Gate.Process.Identity != live.Identity ||
		!anchorStop.Gate.Process.Equal(proof.GateExit.Process) ||
		!anchorStop.Gate.Cgroup.Equal(*journal.ContainmentCgroupReleaseProof.StopReceipt.CgroupIdentity) ||
		!anchorStop.GateReleaseAuthorized || !anchorStop.DescendantsReaped || !anchorStop.KillAttempted || !anchorStop.KillAcknowledged ||
		!anchorStop.PinnedLeafFD || !anchorStop.CgroupPopulatedZero || !anchorStop.NoMigrationObserved || !anchorStop.ProducerFenced ||
		proof.PinnedLeafFD != anchorStop.PinnedLeafFD || proof.CgroupKillAcknowledged != anchorStop.KillAcknowledged ||
		proof.CgroupPopulatedZero != anchorStop.CgroupPopulatedZero || proof.NoMigrationObserved != anchorStop.NoMigrationObserved ||
		proof.ProducerFenced != anchorStop.ProducerFenced || proof.DescendantsReaped != anchorStop.DescendantsReaped ||
		!linuxV2AnchorRunReapHasProcess(*runReap, live.detachedChild.PID, live.detachedChild.Identity, "descendant") {
		t.Fatal("stop receipt does not prove anchor adoption/reaping after exact helper SIGKILL")
	}
	crash.StopProofKind = proof.Kind
	crash.StopProofReaperPID = proof.Reaper.PID
	crash.StopProofReaperIdentity = proof.Reaper.Identity
	crash.AnchorReapedChild = true
}

func linuxV2CaptureDetachedChild(journal state.RunJournal, fixture linuxV2NativeFixture, leader linuxV2ProcessSnapshot) (linuxV2DetachedChildSnapshot, bool, error) {
	if journal.ContainmentCgroup == nil || journal.ContainmentCgroup.LeafIdentity == nil {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	postCommit, err := linuxV2MarkerExists(filepath.Join(fixture.markerDir, "post-commit"), fixture.owner)
	if err != nil || !postCommit {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	contents, markerStat, present, err := linuxV2ReadBHookMarker(fixture, "post-commit-child.pid")
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, fmt.Errorf("read bounded no-follow B child PID marker: %w", err)
	}
	if !present {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if markerStat.Uid != fixture.owner.BUID || markerStat.Gid != fixture.owner.BGID {
		return linuxV2DetachedChildSnapshot{}, false, errors.New("B post-commit child PID marker credentials are invalid")
	}
	fields := strings.Fields(string(contents))
	if len(fields) != 1 {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	childPID, err := strconv.Atoi(fields[0])
	if err != nil || childPID <= 1 {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	pidfd, err := unix.PidfdOpen(childPID, 0)
	if errors.Is(err, unix.ESRCH) {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, fmt.Errorf("open pidfd for exact detached B descendant: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(pidfd), "linux-v2-detached-b-child-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(pidfd)
		return linuxV2DetachedChildSnapshot{}, false, errors.New("wrap exact detached child pidfd")
	}
	keepPIDFD := false
	defer func() {
		if !keepPIDFD {
			_ = pidfdFile.Close()
		}
	}()
	if err := linuxV2RequireLivePIDFD(pidfd); err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	pidfdPID, err := linuxV2PIDFDInfoPID(pidfd)
	if err != nil || pidfdPID != childPID {
		return linuxV2DetachedChildSnapshot{}, false, errors.New("detached child pidfd fdinfo does not identify the marker PID")
	}
	identity, err := platform.ProcessIdentity(childPID)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, fmt.Errorf("read detached child creation identity: %w", err)
	}
	status, err := linuxV2ReadProcessCredentials(childPID)
	if errors.Is(err, os.ErrNotExist) {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	if status.realUID != fixture.owner.BUID || status.effectiveUID != fixture.owner.BUID ||
		status.realGID != fixture.owner.BGID || status.effectiveGID != fixture.owner.BGID {
		return linuxV2DetachedChildSnapshot{}, false, errors.New("post-commit descendant did not retain fixed-B credentials")
	}
	parentPID, processGroup, sessionID, err := linuxV2ReadProcessGroups(childPID)
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	commandLine, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", childPID))
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	arguments := strings.Split(strings.TrimRight(string(commandLine), "\x00"), "\x00")
	if len(arguments) < 2 || filepath.Base(arguments[0]) != "sleep" || arguments[len(arguments)-1] != "120" {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if parentPID == leader.PID || processGroup == leader.ProcessGroup || sessionID == leader.SessionID {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	if parentPID != leader.owners.HelperPID && parentPID != leader.owners.AnchorPID && parentPID != leader.owners.MirrorPID {
		return linuxV2DetachedChildSnapshot{}, false, nil
	}
	leafPath := linuxV2LeafPath(fixture.value, *journal.ContainmentCgroup.LeafIdentity)
	if err := linuxV2CgroupContainsPID(leafPath, childPID); err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	childMountIDs, err := linuxV2Cgroup2MountIDs(childPID)
	if err != nil {
		return linuxV2DetachedChildSnapshot{}, false, fmt.Errorf("inventory detached child cgroup2 mounts: %w", err)
	}
	if err := linuxV2RequireNoWritableCgroupFDs(childPID, identity, childMountIDs); err != nil {
		return linuxV2DetachedChildSnapshot{}, false, fmt.Errorf("audit detached child cgroup2 descriptors: %w", err)
	}
	if current, err := platform.ProcessIdentity(childPID); err != nil || current != identity {
		return linuxV2DetachedChildSnapshot{}, false, errors.New("detached child identity changed during cgroup observation")
	}
	if err := linuxV2RequireLivePIDFD(pidfd); err != nil {
		return linuxV2DetachedChildSnapshot{}, false, err
	}
	keepPIDFD = true
	return linuxV2DetachedChildSnapshot{
		PID: childPID, Identity: identity, ParentPID: parentPID, ProcessGroup: processGroup, SessionID: sessionID,
		RealUID: status.realUID, EffectiveUID: status.effectiveUID, RealGID: status.realGID, EffectiveGID: status.effectiveGID,
		PIDFDOpened: true, PIDFDInfoPID: pidfdPID, LeafMembership: true, NoWritableCgroupFDs: true,
		Detached:   processGroup != leader.ProcessGroup && sessionID != leader.SessionID,
		Reparented: parentPID != leader.PID, ReparentedToOwner: true, AdopterPID: parentPID, pidfd: pidfdFile,
	}, true, nil
}

func linuxV2ReadProcessGroups(pid int) (parentPID int, processGroup, sessionID int64, err error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, 0, err
	}
	closing := strings.LastIndexByte(string(contents), ')')
	if closing < 0 || closing+1 >= len(contents) {
		return 0, 0, 0, errors.New("process stat does not contain a complete command field")
	}
	fields := strings.Fields(string(contents[closing+1:]))
	if len(fields) < 4 {
		return 0, 0, 0, errors.New("process stat omitted parent, process-group, or session fields")
	}
	parent, parentErr := strconv.Atoi(fields[1])
	group, groupErr := strconv.ParseInt(fields[2], 10, 64)
	session, sessionErr := strconv.ParseInt(fields[3], 10, 64)
	if parentErr != nil || groupErr != nil || sessionErr != nil {
		return 0, 0, 0, errors.New("process parent, process-group, or session identity is invalid")
	}
	return parent, group, session, nil
}

func linuxV2OpenPinnedHookMarkerDirectory(path string, owner linuxgate.OwnerConfig) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("native hook marker directory is not canonical")
	}
	rootFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open pinned filesystem root for hook marker: %w", err)
	}
	relative, err := filepath.Rel(string(filepath.Separator), path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = unix.Close(rootFD)
		return nil, errors.New("native hook marker directory escaped filesystem root")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	closeRootErr := unix.Close(rootFD)
	if err != nil {
		return nil, fmt.Errorf("open pinned native hook marker directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), "native-hook-marker-directory")
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap pinned native hook marker directory")
	}
	info, statErr := directory.Stat()
	if statErr != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("stat pinned native hook marker directory: %w", statErr)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != owner.AUID || stat.Gid != owner.BGID || stat.Mode&0o7777 != 0o2770 {
		_ = directory.Close()
		return nil, errors.New("native hook marker directory is not the exact A:B 2770 fixture")
	}
	if closeRootErr != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("close pinned filesystem root: %w", closeRootErr)
	}
	return directory, nil
}

func linuxV2MarkerExists(path string, owner linuxgate.OwnerConfig) (bool, error) {
	directory, err := linuxV2OpenPinnedHookMarkerDirectory(filepath.Dir(path), owner)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		return false, errors.New("native hook marker name is invalid")
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open no-follow native hook marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), "native-hook-marker")
	if file == nil {
		_ = unix.Close(fd)
		return false, errors.New("wrap no-follow native hook marker")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat no-follow native hook marker: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != owner.BUID || stat.Gid != owner.BGID || stat.Nlink != 1 ||
		stat.Mode&0o0022 != 0 || info.Size() <= 0 || info.Size() > linuxV2NativeHookMarkerMaxBytes {
		return false, errors.New("native hook marker has unsafe file type, owner, link count, mode, or size")
	}
	return true, nil
}

func linuxV2ReadBHookMarker(fixture linuxV2NativeFixture, name string) ([]byte, *syscall.Stat_t, bool, error) {
	directory, err := linuxV2OpenPinnedHookMarkerDirectory(fixture.markerDir, fixture.owner)
	if err != nil {
		return nil, nil, false, err
	}
	defer directory.Close()
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, nil, false, errors.New("native hook marker basename is invalid")
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("open no-follow native hook marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), "native-hook-marker")
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, false, errors.New("wrap no-follow native hook marker")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, false, fmt.Errorf("stat no-follow native hook marker: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return nil, nil, false, errors.New("native hook marker is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > linuxV2NativeHookMarkerMaxBytes {
		return nil, nil, false, errors.New("native hook marker exceeds its bounded size")
	}
	if stat.Uid != fixture.owner.BUID || stat.Gid != fixture.owner.BGID || stat.Nlink != 1 || stat.Mode&0o0022 != 0 {
		return nil, nil, false, errors.New("native hook marker must be a bounded B-owned singly-linked regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, linuxV2NativeHookMarkerMaxBytes+1))
	if err != nil || len(contents) > linuxV2NativeHookMarkerMaxBytes {
		return nil, nil, false, errors.New("read bounded native hook marker")
	}
	return contents, stat, true, nil
}

func linuxV2WriteBHookReleaseMarker(fixture linuxV2NativeFixture, name string) error {
	if name != "post-commit-child.release" {
		return errors.New("native hook release marker basename is not fixed")
	}
	directory, err := linuxV2OpenPinnedHookMarkerDirectory(fixture.markerDir, fixture.owner)
	if err != nil {
		return err
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return fmt.Errorf("create exclusive no-follow B hook release marker: %w", err)
	}
	file := os.NewFile(uintptr(fd), "native-hook-release")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap no-follow native hook release marker")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure native hook release marker: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat native hook release marker: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != fixture.owner.AUID || stat.Gid != fixture.owner.AGID ||
		stat.Nlink != 1 || stat.Mode&0o7777 != 0o600 || info.Size() != 0 {
		_ = file.Close()
		return errors.New("native hook release marker has unsafe owner, link count, mode, or initial size")
	}
	contents := []byte("release\n")
	written, err := file.Write(contents)
	if err != nil || written != len(contents) {
		_ = file.Close()
		return errors.New("write exact native hook release marker")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync native hook release marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close native hook release marker: %w", err)
	}
	if err := unix.Fsync(int(directory.Fd())); err != nil {
		return fmt.Errorf("sync native hook marker directory: %w", err)
	}
	return nil
}

func linuxV2CloneTargetAbsent(journal state.RunJournal) bool {
	if journal.LinuxCloneIntent == nil || journal.LinuxCloneIntent.Target == "" {
		return false
	}
	_, err := os.Lstat(journal.LinuxCloneIntent.Target)
	return errors.Is(err, os.ErrNotExist)
}

func linuxV2PreserveRetainedCloneState(fixture linuxV2NativeFixture, journal state.RunJournal) {
	if fixture.keepRoot != nil && (journal.RetainWorkspace || journal.WorkspaceRecoveryRequired || !linuxV2CloneTargetAbsent(journal)) {
		*fixture.keepRoot = true
	}
}

func linuxV2CaptureLiveProcess(journal state.RunJournal, fixture linuxV2NativeFixture, harnessKind string) (linuxV2ProcessSnapshot, bool, error) {
	reservation, authorityValue := journal.ContainmentCgroup, journal.ContainmentAuthority
	if reservation == nil || authorityValue == nil || reservation.LeafIdentity == nil || reservation.Gate == nil ||
		journal.LinuxCloneIntent == nil || journal.LinuxCloneCheckoutReceipt == nil {
		return linuxV2ProcessSnapshot{}, false, nil
	}
	target := authority.LinuxCgroupProcessIdentity{PID: authorityValue.TargetPID, Identity: authorityValue.TargetIdentity}
	if target.PID <= 0 || target.Identity == "" || reservation.Gate.Process.PID != target.PID || reservation.Gate.Process.Identity != target.Identity ||
		!reservation.Gate.BornInLeaf || !reservation.Gate.TrustedImage || reservation.Gate.HostUID != fixture.owner.AUID ||
		!reservation.Gate.Cgroup.Equal(*reservation.LeafIdentity) || reservation.OwnerUID != fixture.owner.AUID || reservation.LauncherUID != 0 ||
		authorityValue.CgroupIdentity == nil || !authorityValue.CgroupIdentity.Equal(*reservation.LeafIdentity) ||
		reservation.Launcher == nil || reservation.LauncherExit == nil || !reservation.LauncherExit.Waited || !reservation.LauncherExit.Reaped ||
		!reservation.LauncherExit.Process.Equal(*reservation.Launcher) || reservation.LauncherExit.Process.PID <= 0 ||
		journal.PID != target.PID || journal.ProcessIdentity != target.Identity {
		return linuxV2ProcessSnapshot{}, false, errors.New("live Linux v2 RunJournal does not bind its exact same-PID gate, authority, and pinned leaf")
	}
	intent := journal.LinuxCloneIntent
	checkout := journal.LinuxCloneCheckoutReceipt
	if intent.Validate() != nil || checkout.Validate() != nil || journal.LinuxCloneSeedReceipt == nil ||
		checkout.RunID != intent.RunID || checkout.Generation != intent.Generation || checkout.BindingKey != intent.BindingKey ||
		checkout.ResourceID != intent.ResourceID || checkout.Target != intent.Target || checkout.Nonce != intent.Nonce ||
		checkout.BaseCommit != intent.BaseCommit || checkout.SeedDigest != journal.LinuxCloneSeedReceipt.Digest {
		return linuxV2ProcessSnapshot{}, false, errors.New("RunJournal clone checkout receipt does not bind the exact admitted clone target")
	}
	postCheckout, err := linuxV2MarkerExists(filepath.Join(fixture.markerDir, "post-checkout"), fixture.owner)
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	if postCheckout {
		return linuxV2ProcessSnapshot{}, false, errors.New("native Git hook marker appeared before the B CLI process witness")
	}
	if err := verifyLinuxV2LeafOnDisk(fixture.value, fixture.owner.AUID, *reservation.LeafIdentity); err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	status, err := linuxV2ReadProcessCredentials(target.PID)
	if errors.Is(err, os.ErrNotExist) {
		return linuxV2ProcessSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	if status.realUID != fixture.owner.BUID || status.effectiveUID != fixture.owner.BUID ||
		status.realGID != fixture.owner.BGID || status.effectiveGID != fixture.owner.BGID {
		return linuxV2ProcessSnapshot{}, false, nil
	}
	owners, err := linuxV2OwnerSnapshotFromReservation(*reservation, fixture.owner.AGID)
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	identity, err := platform.ProcessIdentity(target.PID)
	if err != nil || identity != target.Identity {
		return linuxV2ProcessSnapshot{}, false, errors.New("live v2 target creation identity differs from the exact RunJournal identity")
	}
	pidfd, err := unix.PidfdOpen(target.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return linuxV2ProcessSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("open pidfd for exact B native subject: %w", err)
	}
	pidfdFile := os.NewFile(uintptr(pidfd), "linux-v2-native-target-pidfd")
	if pidfdFile == nil {
		_ = unix.Close(pidfd)
		return linuxV2ProcessSnapshot{}, false, errors.New("wrap exact B native target pidfd")
	}
	keepPIDFD := false
	defer func() {
		if !keepPIDFD {
			_ = pidfdFile.Close()
		}
	}()
	if err := linuxV2RequireLivePIDFD(pidfd); err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	pidfdPID, err := linuxV2PIDFDInfoPID(pidfd)
	if err != nil || pidfdPID != target.PID {
		return linuxV2ProcessSnapshot{}, false, errors.New("test pidfd fdinfo did not identify the exact RunJournal target PID")
	}
	userNamespace, err := linuxV2OpenNamespace(target.PID, "user")
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("open exact B user namespace identity: %w", err)
	}
	defer userNamespace.Close()
	networkNamespace, err := linuxV2OpenNamespace(target.PID, "net")
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("open exact B network namespace identity: %w", err)
	}
	defer networkNamespace.Close()
	userInfo, err := userNamespace.Stat()
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("stat exact B user namespace: %w", err)
	}
	networkInfo, err := networkNamespace.Stat()
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("stat exact B network namespace: %w", err)
	}
	userStat, userOK := userInfo.Sys().(*syscall.Stat_t)
	networkStat, networkOK := networkInfo.Sys().(*syscall.Stat_t)
	if !userOK || !networkOK || userStat.Ino == 0 || networkStat.Ino == 0 ||
		userStat.Mode&unix.S_IFMT != unix.S_IFREG || networkStat.Mode&unix.S_IFMT != unix.S_IFREG {
		return linuxV2ProcessSnapshot{}, false, errors.New("exact B namespace identity is not available through nsfs")
	}
	leafPath := linuxV2LeafPath(fixture.value, *reservation.LeafIdentity)
	clonePath := checkout.Target
	if !linuxV2PathWithin(clonePath, fixture.workspace.Root) {
		return linuxV2ProcessSnapshot{}, false, errors.New("B checkout target escaped the configured native clone Root")
	}
	cloneInfo, err := os.Lstat(clonePath)
	if errors.Is(err, os.ErrNotExist) {
		return linuxV2ProcessSnapshot{}, false, nil
	}
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("stat exact B-owned clone target: %w", err)
	}
	cloneStat, ok := cloneInfo.Sys().(*syscall.Stat_t)
	if !ok || cloneStat.Uid != fixture.owner.BUID || uint64(cloneStat.Dev) != checkout.TargetDevice || cloneStat.Ino != checkout.TargetInode {
		return linuxV2ProcessSnapshot{}, false, errors.New("live native clone does not match the exact B-owned Store checkout receipt")
	}
	if err := linuxV2CgroupContainsPID(leafPath, target.PID); err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	commandLine, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", target.PID))
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("read exact native process command identity: %w", err)
	}
	if !linuxV2NativeCommandMatches(harnessKind, commandLine) {
		return linuxV2ProcessSnapshot{}, false, errors.New("B process command line does not identify the configured native CLI transport")
	}
	noWritableCgroupFDs := false
	if err := linuxV2RequireNoWritableCgroupFDsInLeaf(
		leafPath, target.PID, target.Identity, fixture.owner.BUID, fixture.owner.BGID,
	); err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	noWritableCgroupFDs = true
	_, processGroup, sessionID, err := linuxV2ReadProcessGroups(target.PID)
	if err != nil {
		return linuxV2ProcessSnapshot{}, false, fmt.Errorf("read exact native leader process/session identity: %w", err)
	}
	if actual, err := platform.ProcessIdentity(target.PID); err != nil || actual != target.Identity {
		return linuxV2ProcessSnapshot{}, false, errors.New("exact B process identity changed during namespace and leaf observation")
	}
	if err := linuxV2RequireLivePIDFD(pidfd); err != nil {
		return linuxV2ProcessSnapshot{}, false, err
	}
	keepPIDFD = true
	return linuxV2ProcessSnapshot{
		PID: target.PID, Identity: target.Identity, ProcessGroup: processGroup, SessionID: sessionID,
		PIDFDOpened: true, PIDFDInfoPID: pidfdPID,
		RealUID: status.realUID, EffectiveUID: status.effectiveUID, RealGID: status.realGID, EffectiveGID: status.effectiveGID,
		UserNamespaceDevice: userStat.Dev, UserNamespaceInode: userStat.Ino,
		NetworkNamespaceDevice: networkStat.Dev, NetworkNamespaceInode: networkStat.Ino,
		NoWritableCgroupFDs: noWritableCgroupFDs,
		LeafMembership:      true, NativeCommand: true,
		leafIdentity: *reservation.LeafIdentity,
		owners:       owners, checkout: *checkout,
		postCheckoutAbsentAtCheckoutCAS: true,
		pidfd:                           pidfdFile,
	}, true, nil
}

func linuxV2Cgroup2MountIDs(pid int) (map[uint64]struct{}, error) {
	if pid <= 1 {
		return nil, errors.New("cgroup2 mount inventory requires an exact positive process PID")
	}
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/mountinfo", pid))
	if err != nil {
		return nil, fmt.Errorf("read exact process mountinfo for cgroup2 FD audit: %w", err)
	}
	mountIDs := make(map[uint64]struct{})
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 0 || separator+1 >= len(fields) || fields[separator+1] != "cgroup2" {
			continue
		}
		mountID, parseErr := strconv.ParseUint(fields[0], 10, 64)
		if parseErr != nil || mountID == 0 {
			return nil, errors.New("process mountinfo contains an invalid cgroup2 mount ID")
		}
		mountIDs[mountID] = struct{}{}
	}
	if len(mountIDs) == 0 {
		return nil, errors.New("exact B process has no visible cgroup2 mounts for FD inventory")
	}
	return mountIDs, nil
}

func linuxV2RequireNoWritableCgroupFDs(pid int, identity string, cgroupMountIDs map[uint64]struct{}) error {
	if pid <= 1 || identity == "" || len(cgroupMountIDs) == 0 {
		return errors.New("cgroup descriptor inspection lacks exact target identity or cgroup2 mount IDs")
	}
	current, identityErr := platform.ProcessIdentity(pid)
	if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
		return nil
	}
	if identityErr != nil || current != identity {
		return errors.New("target identity changed before cgroup descriptor inspection")
	}
	directory := fmt.Sprintf("/proc/%d/fdinfo", pid)
	entries, err := os.ReadDir(directory)
	if err != nil {
		current, identityErr := platform.ProcessIdentity(pid)
		if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
			return nil
		}
		if identityErr == nil && current != identity {
			return errors.New("PID was reused during cgroup descriptor inspection")
		}
		return fmt.Errorf("read exact B fdinfo for cgroup-descriptor inspection: %w", err)
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect exact B fdinfo entry %s: %w", entry.Name(), err)
		}
		var flags uint64
		var mountID uint64
		var haveFlags, haveMountID bool
		for _, line := range strings.Split(string(contents), "\n") {
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch name {
			case "flags":
				parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 8, 64)
				if parseErr != nil || haveFlags {
					return errors.New("B fdinfo contains an invalid or duplicate descriptor flags field")
				}
				flags, haveFlags = parsed, true
			case "mnt_id":
				parsed, parseErr := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
				if parseErr != nil || haveMountID {
					return errors.New("B fdinfo contains an invalid or duplicate mount ID field")
				}
				mountID, haveMountID = parsed, true
			}
		}
		if !haveMountID {
			continue
		}
		if _, cgroup2 := cgroupMountIDs[mountID]; !cgroup2 {
			continue
		}
		if !haveFlags {
			return errors.New("B cgroup2-mount descriptor omitted access flags")
		}
		access := flags & uint64(unix.O_ACCMODE)
		if access == uint64(unix.O_WRONLY) || access == uint64(unix.O_RDWR) {
			return errors.New("B process inherited a writable descriptor on a cgroup-v2 mount")
		}
	}
	current, identityErr = platform.ProcessIdentity(pid)
	if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
		return nil
	}
	if identityErr != nil {
		return fmt.Errorf("recheck exact B identity after cgroup descriptor inspection: %w", identityErr)
	}
	if current != identity {
		return errors.New("PID was reused during cgroup descriptor inspection")
	}
	return nil
}

func linuxV2RequireNoWritableCgroupFDsInLeaf(
	leafPath string,
	targetPID int,
	targetIdentity string,
	ownerUID uint32,
	ownerGID uint32,
) error {
	contents, err := os.ReadFile(filepath.Join(leafPath, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("read pinned OpenCode leaf membership for FD audit: %w", err)
	}
	foundTarget := false
	for _, field := range strings.Fields(string(contents)) {
		pid, parseErr := strconv.Atoi(field)
		if parseErr != nil || pid <= 1 {
			return errors.New("pinned cgroup leaf cgroup.procs contains an invalid PID")
		}
		identity, identityErr := platform.ProcessIdentity(pid)
		if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
			continue
		}
		if identityErr != nil {
			return fmt.Errorf("capture cgroup leaf process identity for FD audit: %w", identityErr)
		}
		credentials, credentialErr := linuxV2ReadProcessCredentials(pid)
		if credentialErr != nil {
			current, identityErr := platform.ProcessIdentity(pid)
			if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
				continue
			}
			if identityErr == nil && current != identity {
				return errors.New("leaf PID was reused during B descendant descriptor inspection")
			}
			return fmt.Errorf("read exact B descendant credentials for FD audit: %w", credentialErr)
		}
		if credentials.realUID != ownerUID || credentials.effectiveUID != ownerUID ||
			credentials.realGID != ownerGID || credentials.effectiveGID != ownerGID {
			return errors.New("pinned cgroup leaf contains a process outside fixed-B credentials")
		}
		if pid == targetPID {
			if identity != targetIdentity {
				return errors.New("native target PID identity changed during leaf FD audit")
			}
			foundTarget = true
		}
		mountIDs, mountErr := linuxV2Cgroup2MountIDs(pid)
		if mountErr != nil {
			current, identityErr := platform.ProcessIdentity(pid)
			if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, unix.ESRCH) {
				continue
			}
			if identityErr == nil && current != identity {
				return errors.New("leaf PID was reused during cgroup2 mount inventory")
			}
			return fmt.Errorf("inventory exact process cgroup2 mount IDs: %w", mountErr)
		}
		if err := linuxV2RequireNoWritableCgroupFDs(pid, identity, mountIDs); err != nil {
			return err
		}
	}
	if !foundTarget {
		return errors.New("exact OpenCode target is absent from the pinned leaf during FD audit")
	}
	return nil
}

type linuxV2ProcessCredentials struct {
	realUID, effectiveUID uint32
	realGID, effectiveGID uint32
}

func linuxV2ReadProcessCredentials(pid int) (linuxV2ProcessCredentials, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return linuxV2ProcessCredentials{}, err
	}
	var result linuxV2ProcessCredentials
	var haveUID, haveGID bool
	for _, line := range strings.Split(string(contents), "\n") {
		var target *bool
		var first, second *uint32
		var prefix string
		switch {
		case strings.HasPrefix(line, "Uid:"):
			target, first, second, prefix = &haveUID, &result.realUID, &result.effectiveUID, "Uid:"
		case strings.HasPrefix(line, "Gid:"):
			target, first, second, prefix = &haveGID, &result.realGID, &result.effectiveGID, "Gid:"
		default:
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		if len(fields) < 2 {
			return linuxV2ProcessCredentials{}, errors.New("process status omitted real/effective credentials")
		}
		parsedFirst, firstErr := strconv.ParseUint(fields[0], 10, 32)
		parsedSecond, secondErr := strconv.ParseUint(fields[1], 10, 32)
		if firstErr != nil || secondErr != nil {
			return linuxV2ProcessCredentials{}, errors.New("process status credentials are invalid")
		}
		*first, *second, *target = uint32(parsedFirst), uint32(parsedSecond), true
	}
	if !haveUID || !haveGID {
		return linuxV2ProcessCredentials{}, errors.New("process status omitted UID or GID")
	}
	return result, nil
}

func linuxV2PIDFDInfoPID(pidfd int) (int, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd))
	if err != nil {
		return 0, err
	}
	found := false
	var pid int
	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.HasPrefix(line, "Pid:") {
			continue
		}
		if found {
			return 0, errors.New("pidfd fdinfo has duplicate Pid fields")
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pid:")))
		if err != nil || parsed <= 0 {
			return 0, errors.New("pidfd fdinfo contains an invalid Pid")
		}
		found, pid = true, parsed
	}
	if !found {
		return 0, errors.New("pidfd fdinfo omitted Pid")
	}
	return pid, nil
}

func linuxV2RequireLivePIDFD(pidfd int) error {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: int16(unix.POLLIN)}}
	if _, err := unix.Poll(fds, 0); err != nil {
		return fmt.Errorf("poll exact native process pidfd: %w", err)
	}
	if fds[0].Revents != 0 {
		return errors.New("exact native process pidfd reports an exited target")
	}
	return nil
}

func linuxV2OpenNamespace(pid int, name string) (*os.File, error) {
	return os.Open(fmt.Sprintf("/proc/%d/ns/%s", pid, name))
}

func linuxV2NativeCommandMatches(harnessKind string, commandLine []byte) bool {
	arguments := strings.Split(strings.TrimRight(string(commandLine), "\x00"), "\x00")
	containsPair := func(left, right string) bool {
		for index := 0; index+1 < len(arguments); index++ {
			if arguments[index] == left && arguments[index+1] == right {
				return true
			}
		}
		return false
	}
	switch harnessKind {
	case "pi":
		return containsPair("--mode", "rpc")
	case "opencode":
		return containsPair("serve", "--hostname") && containsPair("--hostname", "127.0.0.1") && containsPair("--port", "0")
	default:
		return false
	}
}

func linuxV2CgroupContainsPID(leafPath string, pid int) error {
	contents, err := os.ReadFile(filepath.Join(leafPath, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("read exact pinned run-leaf membership: %w", err)
	}
	for _, line := range strings.Fields(string(contents)) {
		value, err := strconv.Atoi(line)
		if err == nil && value == pid {
			return nil
		}
	}
	return errors.New("exact B process is not a member of its RunJournal-pinned cgroup-v2 leaf")
}

func verifyLinuxV2LeafOnDisk(value config.Config, ownerUID uint32, identity authority.LinuxCgroupIdentity) error {
	if identity.Validate() != nil {
		return errors.New("RunJournal cgroup-v2 leaf identity is invalid")
	}
	path := linuxV2LeafPath(value, identity)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat exact pinned cgroup-v2 leaf: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != ownerUID || uint64(stat.Dev) != identity.LeafDevice || stat.Ino != identity.LeafInode {
		return errors.New("live cgroup-v2 path differs from the exact RunJournal leaf device/inode/owner")
	}
	return nil
}

func linuxV2LeafPath(value config.Config, identity authority.LinuxCgroupIdentity) string {
	return filepath.Join(value.LinuxOwnerCgroupMount, filepath.FromSlash(value.LinuxOwnerDelegatedPath), identity.LeafName)
}

func linuxV2PathWithin(target, root string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func linuxV2BuildLeafSnapshot(identity authority.LinuxCgroupIdentity) *linuxV2LeafSnapshot {
	return &linuxV2LeafSnapshot{
		Version: identity.Version, ParentBootIDHash: linuxWitnessHashBytes([]byte(identity.Parent.BootID)),
		ParentMountID: identity.Parent.MountID, ParentDevice: identity.Parent.Device, ParentInode: identity.Parent.Inode,
		ParentPathHash: linuxWitnessHashBytes([]byte(identity.Parent.Path)), LeafNameHash: linuxWitnessHashBytes([]byte(identity.LeafName)),
		LeafDevice: identity.LeafDevice, LeafInode: identity.LeafInode,
	}
}

func linuxV2BuildAnchorStopSnapshot(evidence platform.LinuxOwnerAnchorStopEvidence) *linuxV2AnchorStopSnapshot {
	return &linuxV2AnchorStopSnapshot{
		Version: evidence.Version, RunID: evidence.RunKey.RunID, Generation: evidence.RunKey.Generation,
		AnchorPID: evidence.Anchor.PID, AnchorIdentitySHA256: linuxWitnessHashBytes([]byte(evidence.Anchor.Identity)),
		HelperPID: evidence.Helper.PID, HelperIdentitySHA256: linuxWitnessHashBytes([]byte(evidence.Helper.Identity)),
		Leaf: linuxV2BuildLeafSnapshot(evidence.Leaf), ProducerFenceVerified: evidence.ProducerFence != nil,
		LeafMembershipVerified: evidence.ProducerFence != nil && evidence.ProducerFence.LeafMembershipVerified,
		GateReleaseAuthorized:  evidence.GateReleaseAuthorized,
		HelperExitVerified:     evidence.HelperExit != nil, PinnedLeafFD: evidence.PinnedLeafFD,
		KillAttempted: evidence.KillAttempted, KillAcknowledged: evidence.KillAcknowledged,
		CgroupPopulatedZero: evidence.CgroupPopulatedZero, NoMigrationObserved: evidence.NoMigrationObserved,
		ProducerFenced: evidence.ProducerFenced, DescendantsReaped: evidence.DescendantsReaped,
		ReapCount: evidence.ReapCount, ReapDigest: evidence.ReapDigest,
	}
}

func linuxV2BuildCloneSnapshot(receipt authority.LinuxCloneCheckoutReceipt, ownerUID uint32) *linuxV2CloneSnapshot {
	return &linuxV2CloneSnapshot{
		TargetPathHash: linuxWitnessHashBytes([]byte(receipt.Target)), TargetDevice: receipt.TargetDevice,
		TargetInode: receipt.TargetInode, OwnerUID: ownerUID, CheckoutReady: true,
	}
}

func linuxV2OwnerSnapshotFromReservation(reservation state.LinuxCgroupReservation, ownerGID uint32) (linuxV2OwnerSnapshot, error) {
	if reservation.Gate == nil || reservation.Helper == nil || reservation.Launcher == nil || reservation.Mirror == nil || reservation.LauncherExit == nil ||
		reservation.Anchor.Validate() != nil || reservation.Helper.Validate() != nil || reservation.Launcher.Validate() != nil ||
		reservation.Mirror.Validate() != nil {
		return linuxV2OwnerSnapshot{}, errors.New("RunJournal omitted an exact A anchor/helper/launcher/mirror identity")
	}
	for _, process := range []authority.LinuxCgroupProcessIdentity{reservation.Anchor, *reservation.Helper, *reservation.Mirror} {
		identity, err := platform.ProcessIdentity(process.PID)
		if err != nil || identity != process.Identity {
			return linuxV2OwnerSnapshot{}, errors.New("A anchor/helper/mirror creation identity differs from the exact RunJournal owner")
		}
		credentials, err := linuxV2ReadProcessCredentials(process.PID)
		if err != nil || credentials.effectiveUID != reservation.OwnerUID || credentials.effectiveGID != ownerGID {
			return linuxV2OwnerSnapshot{}, errors.New("Linux v2 anchor/helper/mirror does not run as A")
		}
	}
	if reservation.LauncherExit.Process.PID != reservation.Launcher.PID || reservation.LauncherExit.Process.Identity != reservation.Launcher.Identity {
		return linuxV2OwnerSnapshot{}, errors.New("static root launcher exit proof does not bind the exact recorded launcher")
	}
	return linuxV2OwnerSnapshot{
		AnchorPID: reservation.Anchor.PID, AnchorIdentity: reservation.Anchor.Identity,
		HelperPID: reservation.Helper.PID, HelperIdentity: reservation.Helper.Identity,
		MirrorPID: reservation.Mirror.PID, MirrorIdentity: reservation.Mirror.Identity,
		LauncherPID: reservation.Launcher.PID, LauncherIdentity: reservation.Launcher.Identity,
		GatePID: reservation.Gate.Process.PID, GateIdentity: reservation.Gate.Process.Identity,
		OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
	}, nil
}

func linuxV2ReadNativeHookMarkers(fixture linuxV2NativeFixture) (postCheckout, postCommit bool, ownerUID uint32, err error) {
	for _, marker := range []struct {
		name string
		set  *bool
	}{
		{name: "post-checkout", set: &postCheckout}, {name: "post-commit", set: &postCommit},
	} {
		contents, stat, present, readErr := linuxV2ReadBHookMarker(fixture, marker.name)
		if readErr != nil {
			return false, false, 0, fmt.Errorf("read bounded no-follow B Git hook marker: %w", readErr)
		}
		if !present {
			continue
		}
		if string(contents) != marker.name+"\n" {
			return false, false, 0, errors.New("native Git hook marker does not contain its exact hook name")
		}
		*marker.set = true
		if marker.name == "post-commit" {
			ownerUID = uint32(stat.Uid)
		}
	}
	return postCheckout, postCommit, ownerUID, nil
}

func linuxV2IsWSL2() bool {
	contents, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(contents)), "wsl2")
}

func writeLinuxV2NativeWitness(path string, witness linuxV2NativeWitness, secretValues ...string) error {
	contents, err := json.MarshalIndent(witness, "", "  ")
	if err != nil {
		return fmt.Errorf("encode redacted witness summary: %w", err)
	}
	for _, entry := range os.Environ() {
		key, secret, ok := strings.Cut(entry, "=")
		if !ok || secret == "" || !linuxV2IsCredentialEnvironmentName(key) && key != "SYMMETRY_OPERATOR_TOKEN" && key != "SYMMETRY_ENROLLMENT_TOKEN" {
			continue
		}
		if strings.Contains(string(contents), secret) {
			return fmt.Errorf("redacted witness summary contains a value from credential variable %s", key)
		}
	}
	for _, secret := range secretValues {
		if secret != "" && strings.Contains(string(contents), secret) {
			return errors.New("redacted witness summary contains a private claim credential")
		}
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open redacted witness summary: %w", err)
	}
	file := os.NewFile(uintptr(fd), "redacted-linux-v2-witness")
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap redacted witness summary file")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure redacted witness summary: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write redacted witness summary: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync redacted witness summary: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close redacted witness summary: %w", err)
	}
	return nil
}
