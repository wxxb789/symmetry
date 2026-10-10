//go:build linux && amd64

package e2e_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/opencode"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

const (
	linuxV2RootlessComposeWitnessEnvironment            = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS"
	linuxV2RootlessComposeControlEnvironment            = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_CONTROL_URL"
	linuxV2RootlessComposeCAEnvironment                 = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_CA_FILE"
	linuxV2RootlessComposeOperatorEnvironment           = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPERATOR_TOKEN"
	linuxV2RootlessComposeTransitionReleaseEnvironment  = "SYMMETRY_PRODUCTION_LINUX_V2_COMPOSE_TRANSITION_RELEASE_URL"
	linuxV2RootlessComposePIRuntimeKeyEnvironment       = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_RUNTIME_KEY"
	linuxV2RootlessComposeOpenCodeRuntimeKeyEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_RUNTIME_KEY"
	linuxV2RootlessComposePIStateEnvironment            = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_STATE_DIR"
	linuxV2RootlessComposeOpenCodeStateEnvironment      = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_STATE_DIR"
	linuxV2RootlessComposePIExecutableEnvironment       = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_EXECUTABLE"
	linuxV2RootlessComposeOpenCodeExecutableEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_EXECUTABLE"
	linuxV2RootlessComposeOutputEnvironment             = "SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS_OUTPUT_DIR"
)

var (
	rootlessComposeLoginCSRF = regexp.MustCompile(`name="_csrf_token"\s+value="([^"]+)"`)
	rootlessComposePageCSRF  = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)
)

type linuxV2RootlessComposeRuntime struct {
	RuntimeID       string                       `json:"runtime_id"`
	RuntimeKey      string                       `json:"runtime_key"`
	ConnectionEpoch int64                        `json:"connection_epoch"`
	MachineName     string                       `json:"machine_name"`
	RuntimeName     string                       `json:"runtime_name"`
	Status          string                       `json:"status"`
	AgentProfile    string                       `json:"agent_profile"`
	Workspace       string                       `json:"workspace"`
	Capabilities    protocol.RuntimeCapabilities `json:"capabilities"`
}

type linuxV2RootlessComposeExecution struct {
	TaskID     string          `json:"task_id"`
	RunID      string          `json:"run_id"`
	RuntimeID  string          `json:"runtime_id"`
	Generation int64           `json:"generation"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result"`
	Failure    struct {
		Stage   string `json:"stage"`
		Reason  string `json:"reason"`
		Summary string `json:"summary"`
		Error   string `json:"error"`
	} `json:"failure"`
}

type linuxV2RootlessComposePortal struct {
	baseURL string
	csrf    string
	client  *http.Client
}

type linuxV2RootlessComposeTransitionProxy struct {
	baseURL string
	client  *http.Client
}

type linuxV2RootlessComposeTransitionStatus struct {
	State        string `json:"state"`
	RunID        string `json:"run_id"`
	Generation   int64  `json:"generation"`
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	ClaimID      string `json:"claim_id"`
}

type linuxV2RootlessComposeTransitionRelease struct {
	RunID            string `json:"run_id"`
	Generation       int64  `json:"generation"`
	RuntimeID        string `json:"runtime_id"`
	RuntimeEpoch     int64  `json:"runtime_epoch"`
	ClaimID          string `json:"claim_id"`
	LeaseTokenSHA256 string `json:"lease_token_sha256"`
	Nonce            string `json:"nonce"`
}

type linuxV2RootlessComposeEvidence struct {
	Version                 int    `json:"version"`
	Case                    string `json:"case"`
	Harness                 string `json:"harness"`
	NativeVersion           string `json:"native_version"`
	WorkItemIDSHA256        string `json:"work_item_id_sha256"`
	TaskIDSHA256            string `json:"task_id_sha256"`
	RunIDSHA256             string `json:"run_id_sha256"`
	RuntimeIDSHA256         string `json:"runtime_id_sha256"`
	Generation              int64  `json:"generation"`
	ProbeReturnedUnverified bool   `json:"probe_returned_err_native_unverified"`
	RunState                string `json:"run_state"`
	FailureStage            string `json:"failure_stage"`
	ClaimFenceObserved      bool   `json:"claim_fence_observed"`
	NoNativeTaskProcess     bool   `json:"no_native_task_process"`
	NoTaskResult            bool   `json:"no_task_result"`
	NoProcessAuthority      bool   `json:"no_process_authority"`
	NoStopReceipt           bool   `json:"no_stop_receipt"`
	NoProviderAccess        bool   `json:"no_provider_access"`
	NonGoalWorkItem         bool   `json:"non_goal_work_item"`
}

func TestRootlessComposePortalRequiresTrustedTLSForSecureSession(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		cookie, _ := request.Cookie("fixture-session")
		if request.Method == http.MethodGet && request.URL.Path == "/portal/login" {
			http.SetCookie(writer, &http.Cookie{Name: "fixture-session", Value: "csrf", Secure: true, HttpOnly: true, Path: "/"})
			_, _ = io.WriteString(writer, `<input name="_csrf_token" value="fixture-csrf">`)
			return
		}
		if cookie == nil || cookie.Value != "csrf" {
			http.Error(writer, "secure session missing", http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/portal/login":
			if request.Method != http.MethodPost || request.FormValue("_csrf_token") != "fixture-csrf" || request.FormValue("operator_token") != "fixture-operator" {
				http.Error(writer, "invalid login", http.StatusForbidden)
				return
			}
			http.Redirect(writer, request, "/portal", http.StatusFound)
		case "/portal":
			_, _ = io.WriteString(writer, `<meta name="csrf-token" content="authenticated-csrf">`)
		case "/portal/api/workspace":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	certificate := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newLinuxV2RootlessComposePortal(strings.Replace(server.URL, "https:", "http:", 1), "fixture-operator", certificate); err == nil {
		t.Fatal("HTTP was accepted for a production Secure session")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Unrelated fixture CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	unrelatedDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedCA := filepath.Join(t.TempDir(), "unrelated-ca.pem")
	if err := os.WriteFile(unrelatedCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: unrelatedDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newLinuxV2RootlessComposePortal(server.URL, "fixture-operator", unrelatedCA); err == nil {
		t.Fatal("untrusted TLS fixture certificate was accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("untrusted TLS request reached the HTTP handler")
	}
	portal, err := newLinuxV2RootlessComposePortal(server.URL, "fixture-operator", certificate)
	if err != nil || portal.csrf != "authenticated-csrf" {
		t.Fatalf("trusted TLS Secure session = %#v, error = %v", portal, err)
	}
	if requests.Load() == 0 {
		t.Fatal("trusted TLS session did not reach the HTTP handler")
	}
}

// TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal uses both real
// pinned native adapters and actual non-Goal WorkItem Runs assigned through
// the rootless Compose Control plane. Direct Probe assertions supplement, but
// never substitute for, the durable failed Run and matching local journal.
func TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal(t *testing.T) {
	if os.Getenv(linuxV2RootlessComposeWitnessEnvironment) != "1" {
		t.Skip("set " + linuxV2RootlessComposeWitnessEnvironment + "=1 to run the rootless Compose native WorkItem witnesses")
	}
	if os.Geteuid() == 0 {
		t.Fatal("rootless Compose native witness must run as an unprivileged user")
	}
	if _, err := linuxgate.ReadInstalledOwnerConfig(); err == nil {
		t.Fatal("rootless Compose witness unexpectedly has an installed Linux A/B owner config")
	}
	if _, err := os.Lstat("/etc/symmetry/linux-owner.conf"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rootless Compose witness fixed owner config is present or cannot be inspected: %v", err)
	}

	controlURL := strings.TrimSpace(os.Getenv(linuxV2RootlessComposeControlEnvironment))
	if controlURL == "" {
		controlURL = "https://rootless-portal-edge"
	}
	operatorToken := strings.TrimSpace(os.Getenv(linuxV2RootlessComposeOperatorEnvironment))
	if operatorToken == "" {
		t.Fatalf("%s is required", linuxV2RootlessComposeOperatorEnvironment)
	}
	outputDirectory := strings.TrimSpace(os.Getenv(linuxV2RootlessComposeOutputEnvironment))
	if !filepath.IsAbs(outputDirectory) || filepath.Clean(outputDirectory) != outputDirectory {
		t.Fatalf("%s must name a canonical absolute writable evidence directory", linuxV2RootlessComposeOutputEnvironment)
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		t.Fatalf("create private rootless Compose evidence directory: %v", err)
	}
	if err := os.Chmod(outputDirectory, 0o700); err != nil {
		t.Fatalf("secure rootless Compose evidence directory: %v", err)
	}

	portal, err := newLinuxV2RootlessComposePortal(controlURL, operatorToken, os.Getenv(linuxV2RootlessComposeCAEnvironment))
	if err != nil {
		t.Fatalf("connect to rootless Compose Control portal: %v", err)
	}
	transitionReleaseURL := strings.TrimSpace(os.Getenv(linuxV2RootlessComposeTransitionReleaseEnvironment))
	if transitionReleaseURL == "" {
		t.Fatalf("%s is required; a direct/non-holding Control endpoint cannot prove the App RunJournal before cleanup", linuxV2RootlessComposeTransitionReleaseEnvironment)
	}
	transitionProxy, err := newLinuxV2RootlessComposeTransitionProxy(transitionReleaseURL)
	if err != nil {
		t.Fatalf("connect to the rootless Compose transition-release fixture: %v", err)
	}
	cases := []struct {
		harness, profile, version, runtimeKeyEnvironment, executableEnvironment, stateEnvironment string
	}{
		{
			harness: "pi", profile: "pi-native", version: "0.87.1",
			runtimeKeyEnvironment: linuxV2RootlessComposePIRuntimeKeyEnvironment,
			executableEnvironment: linuxV2RootlessComposePIExecutableEnvironment,
			stateEnvironment:      linuxV2RootlessComposePIStateEnvironment,
		},
		{
			harness: "opencode", profile: "opencode-native", version: "1.18.32",
			runtimeKeyEnvironment: linuxV2RootlessComposeOpenCodeRuntimeKeyEnvironment,
			executableEnvironment: linuxV2RootlessComposeOpenCodeExecutableEnvironment,
			stateEnvironment:      linuxV2RootlessComposeOpenCodeStateEnvironment,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.harness, func(t *testing.T) {
			executable := rootlessComposeRequiredPath(t, testCase.executableEnvironment)
			stateDirectory := rootlessComposeRequiredPath(t, testCase.stateEnvironment)
			runtimeKey := strings.TrimSpace(os.Getenv(testCase.runtimeKeyEnvironment))
			if runtimeKey == "" {
				t.Fatalf("%s is required to select the exact native Compose runtime", testCase.runtimeKeyEnvironment)
			}
			if err := linuxV2VerifyPinnedNativeCLI(executable, testCase.version); err != nil {
				t.Fatalf("verify pinned real %s executable: %v", testCase.harness, err)
			}
			capabilities, probeErr := linuxV2RootlessComposeProbe(testCase.harness, executable)
			if !errors.Is(probeErr, harness.ErrNativeUnverified) {
				t.Fatalf("real %s Probe() error = %v, want ErrNativeUnverified", testCase.harness, probeErr)
			}
			if err := linuxV2ValidateRootlessComposeProbe(capabilities, testCase.harness, testCase.version); err != nil {
				t.Fatalf("real %s Probe() capability projection: %v", testCase.harness, err)
			}

			runtime := portal.awaitRuntime(t, testCase.harness, testCase.profile, testCase.version, runtimeKey)
			if err := linuxV2ValidateRootlessComposeRuntime(runtime, testCase.harness, testCase.profile, testCase.version); err != nil {
				t.Fatalf("Control registered %s runtime: %v", testCase.harness, err)
			}
			nonce, err := linuxV2RootlessComposeUUID()
			if err != nil {
				t.Fatalf("generate private transition-release nonce: %v", err)
			}
			if status, err := transitionProxy.arm(runtime.RuntimeID, nonce); err != nil || status != http.StatusNoContent {
				t.Fatalf("arm exact %s failed-transition acknowledgement hold: status=%d error=%v", testCase.harness, status, err)
			}
			armed, err := transitionProxy.status(runtime.RuntimeID, nonce)
			if err != nil || armed.State != "armed" || armed.RuntimeID != runtime.RuntimeID || armed.RuntimeEpoch != runtime.ConnectionEpoch {
				t.Fatalf("transition proxy did not retain the exact armed %s runtime: state=%q runtime=%q error=%v", testCase.harness, armed.State, armed.RuntimeID, err)
			}
			prematureRelease := linuxV2RootlessComposeTransitionRelease{
				RunID: "unassigned-" + nonce, Generation: 1, RuntimeID: runtime.RuntimeID,
				RuntimeEpoch: runtime.ConnectionEpoch, ClaimID: "unissued-claim",
				LeaseTokenSHA256: linuxWitnessHashBytes([]byte("unissued-lease")), Nonce: nonce,
			}
			if status, err := transitionProxy.release(prematureRelease); err != nil || status != http.StatusConflict {
				t.Fatalf("transition proxy accepted an early non-RunKey release: status=%d error=%v", status, err)
			}
			armed, err = transitionProxy.status(runtime.RuntimeID, nonce)
			if err != nil || armed.State != "armed" || armed.RuntimeID != runtime.RuntimeID || armed.RuntimeEpoch != runtime.ConnectionEpoch {
				t.Fatalf("rejected early release changed armed transition state: state=%q runtime=%q error=%v", armed.State, armed.RuntimeID, err)
			}
			observedKey := state.RunKey{}
			transitionReleased := false
			t.Cleanup(func() {
				if transitionReleased || observedKey.RunID == "" || observedKey.Generation <= 0 {
					return
				}
				status, statusErr := transitionProxy.status(runtime.RuntimeID, nonce)
				if statusErr != nil || status.State != "held" {
					return
				}
				journal, found, readErr := linuxV2RootlessComposeReadRunJournal(stateDirectory, observedKey)
				if readErr == nil && found {
					if _, releaseErr := transitionProxy.release(linuxV2RootlessComposeReleaseForJournal(journal, nonce)); releaseErr != nil {
						t.Errorf("release exact rootless %s terminal transition during cleanup: %v", testCase.harness, releaseErr)
					}
				}
			})

			suffix, err := linuxV2RootlessComposeSuffix()
			if err != nil {
				t.Fatalf("generate isolated %s WorkItem identity: %v", testCase.harness, err)
			}
			workItemID := portal.createAndLaunchWorkItem(t, testCase.harness, testCase.profile, suffix)
			execution := portal.awaitFailedWorkItem(t, workItemID, testCase.harness, runtime.RuntimeID, func(runID string, generation int64) {
				observedKey = state.RunKey{RunID: runID, Generation: generation}
			})
			held, err := transitionProxy.waitForStatus(t, runtime.RuntimeID, nonce, "held", 15*time.Second)
			if err != nil {
				t.Fatalf("rootless %s failed Run has no held exact terminal-transition response: %v", testCase.harness, err)
			}
			if held.RunID != execution.RunID || held.Generation != execution.Generation || held.RuntimeID != runtime.RuntimeID ||
				held.RuntimeEpoch != runtime.ConnectionEpoch || held.ClaimID == "" {
				t.Fatalf("transition proxy held a different %s RunKey/runtime/fence than the Control WorkItem", testCase.harness)
			}
			journal, found, err := linuxV2RootlessComposeReadRunJournal(stateDirectory, observedKey)
			if err != nil || !found {
				t.Fatalf("read exact %s App RunJournal while its Control transition ACK is held: found=%t error=%v", testCase.harness, found, err)
			}
			if journal.RuntimeID != runtime.RuntimeID || journal.RuntimeKey != runtime.RuntimeKey ||
				journal.ClaimedRuntimeEpoch != runtime.ConnectionEpoch || journal.ClaimID == "" || journal.LeaseToken == "" ||
				journal.ClaimID != held.ClaimID || journal.TerminalState != "failed" ||
				journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
				t.Fatalf("rootless %s terminal RunJournal does not bind the held exact Control Claim/failure", testCase.harness)
			}
			if err := linuxV2ValidateRootlessComposeNoProcessAuthority(journal, testCase.profile); err != nil {
				t.Fatalf("rootless %s RunJournal crossed native execution before Linux v2 admission: %v", testCase.harness, err)
			}
			claimFenceObserved := journal.LeaseToken != ""
			for _, invalidRelease := range []struct {
				name   string
				mutate func(*linuxV2RootlessComposeTransitionRelease)
			}{
				{name: "RunID", mutate: func(request *linuxV2RootlessComposeTransitionRelease) { request.RunID = "wrong-" + request.RunID }},
				{name: "generation", mutate: func(request *linuxV2RootlessComposeTransitionRelease) { request.Generation++ }},
				{name: "runtime ID", mutate: func(request *linuxV2RootlessComposeTransitionRelease) {
					request.RuntimeID = "wrong-" + request.RuntimeID
				}},
				{name: "runtime epoch", mutate: func(request *linuxV2RootlessComposeTransitionRelease) { request.RuntimeEpoch++ }},
				{name: "claim ID", mutate: func(request *linuxV2RootlessComposeTransitionRelease) { request.ClaimID = "wrong-" + request.ClaimID }},
				{name: "lease-token hash", mutate: func(request *linuxV2RootlessComposeTransitionRelease) {
					request.LeaseTokenSHA256 = linuxWitnessHashBytes([]byte("wrong-lease-token"))
				}},
				{name: "nonce", mutate: func(request *linuxV2RootlessComposeTransitionRelease) { request.Nonce = "wrong-" + request.Nonce }},
			} {
				invalid := linuxV2RootlessComposeReleaseForJournal(journal, nonce)
				invalidRelease.mutate(&invalid)
				if status, err := transitionProxy.release(invalid); err != nil || status != http.StatusConflict {
					t.Fatalf("transition proxy accepted release with wrong %s: status=%d error=%v", invalidRelease.name, status, err)
				}
				heldAgain, err := transitionProxy.status(runtime.RuntimeID, nonce)
				if err != nil || !linuxV2RootlessComposeTransitionStatusMatches(heldAgain, "held", journal) {
					t.Fatalf("wrong %s release changed the exact held transition: state=%q error=%v", invalidRelease.name, heldAgain.State, err)
				}
			}
			if status, err := transitionProxy.release(linuxV2RootlessComposeReleaseForJournal(journal, nonce)); err != nil || status != http.StatusNoContent {
				t.Fatalf("release exact %s Control transition ACK after RunJournal proof: status=%d error=%v", testCase.harness, status, err)
			}
			transitionReleased = true
			released, err := transitionProxy.waitForStatus(t, runtime.RuntimeID, nonce, "released", 5*time.Second)
			if err != nil || !linuxV2RootlessComposeTransitionStatusMatches(released, "released", journal) {
				t.Fatalf("transition proxy did not release the exact %s RunKey/fence: state=%q error=%v", testCase.harness, released.State, err)
			}

			evidence := linuxV2RootlessComposeEvidence{
				Version: 1, Case: "rootless_compose_native_work_item_refusal", Harness: testCase.harness,
				NativeVersion:    testCase.version,
				WorkItemIDSHA256: linuxWitnessHashBytes([]byte(workItemID)),
				TaskIDSHA256:     linuxWitnessHashBytes([]byte(execution.TaskID)),
				RunIDSHA256:      linuxWitnessHashBytes([]byte(execution.RunID)),
				RuntimeIDSHA256:  linuxWitnessHashBytes([]byte(execution.RuntimeID)),
				Generation:       execution.Generation, ProbeReturnedUnverified: true,
				RunState: execution.State, FailureStage: execution.Failure.Stage,
				ClaimFenceObserved: claimFenceObserved, NoNativeTaskProcess: true, NoTaskResult: true,
				NoProcessAuthority: true, NoStopReceipt: true, NoProviderAccess: true,
				NonGoalWorkItem: true,
			}
			if err := writeLinuxV2RootlessComposeEvidence(filepath.Join(outputDirectory, "rootless-compose-"+testCase.harness+".json"), evidence); err != nil {
				t.Fatalf("write redacted rootless %s witness evidence: %v", testCase.harness, err)
			}
		})
	}
}

func linuxV2RootlessComposeProbe(harnessKind, executable string) (harness.Capabilities, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch harnessKind {
	case "pi":
		result, err := pi.Probe(ctx, executable)
		return result.Capabilities, err
	case "opencode":
		return opencode.NewAdapter(executable).Probe(ctx)
	default:
		return harness.Capabilities{}, fmt.Errorf("unsupported rootless Compose harness %q", harnessKind)
	}
}

func linuxV2ValidateRootlessComposeProbe(capabilities harness.Capabilities, harnessKind, nativeVersion string) error {
	if err := capabilities.Validate(); err != nil {
		return fmt.Errorf("invalid capability projection: %w", err)
	}
	wantKind := harness.Kind(harnessKind)
	if capabilities.Kind != wantKind || capabilities.NativeVersion != nativeVersion ||
		!capabilities.VersionKnown || !capabilities.TransportVerified || capabilities.Verified ||
		capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume ||
		capabilities.Handoff || capabilities.ApprovalResponse || capabilities.ProviderAccess ||
		capabilities.HardCostLimit {
		return errors.New("Probe() did not retain pinned transport evidence with every executable capability false")
	}
	return nil
}

func linuxV2ValidateRootlessComposeRuntime(runtime linuxV2RootlessComposeRuntime, harnessKind, profile, nativeVersion string) error {
	adapter := runtime.Capabilities.Adapter
	if runtime.RuntimeID == "" || runtime.RuntimeKey == "" || runtime.ConnectionEpoch <= 0 || runtime.Status != "online" ||
		runtime.AgentProfile != profile || runtime.Workspace != "native" ||
		runtime.Capabilities.ProviderAccess || adapter == nil || adapter.Kind != harnessKind ||
		adapter.NativeVersion != nativeVersion || adapter.Operations.Start || adapter.Operations.Events ||
		adapter.Operations.Cancel || adapter.Operations.Resume || adapter.Operations.Handoff ||
		adapter.Operations.ApprovalResponse || adapter.Operations.HardCostLimit {
		return errors.New("runtime does not expose the exact online native profile with an unverified no-operation adapter projection")
	}
	return nil
}

func linuxV2ValidateRootlessComposeNoProcessAuthority(journal state.RunJournal, profile string) error {
	if journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() ||
		journal.Work.AgentProfile != profile || journal.Work.Workspace != "native" ||
		len(journal.Work.RequiredCapabilities) != 0 || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired || journal.RetainWorkspace ||
		journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentCgroup != nil ||
		journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven ||
		journal.TerminalState != "" && journal.TerminalState != "failed" ||
		journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil ||
		journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionIntent != nil ||
		journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneAbortPending ||
		journal.LinuxCloneAbortProof != nil || journal.LinuxCloneCleanupPending ||
		journal.LinuxCloneCleanupIntent != nil || journal.LinuxCloneCleanupProof != nil ||
		journal.TerminalTaskResultKind != "" || journal.GoalDeliveryEnabled ||
		len(journal.DeliveredGoalDeliveries) != 0 || len(journal.RetiredGoalDeliveries) != 0 ||
		journal.HasPendingGoalDeliveries() || journal.NativeUsageRecoveryRequired ||
		journal.NativeUsageObservation != nil || journal.NativeUsageTerminalRecovery != nil ||
		len(journal.PendingEvents) != 0 || len(journal.PendingCommandAcknowledgements) != 0 ||
		journal.InputCommandIntent != nil || len(journal.ControlCommandIntents) != 0 ||
		len(journal.ProviderActionIntents) != 0 {
		return errors.New("RunJournal contains native process, v1/v2 containment, clone, workspace, or success-receipt authority")
	}
	return nil
}

func newLinuxV2RootlessComposePortal(controlURL, operatorToken, caFile string) (*linuxV2RootlessComposePortal, error) {
	parsed, err := url.Parse(controlURL)
	if err != nil || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "https" {
		return nil, errors.New("Control Portal URL must be an absolute HTTPS origin")
	}
	baseURL := strings.TrimRight(controlURL, "/")
	certificate, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read rootless Compose fixture CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		return nil, errors.New("rootless Compose fixture CA contains no certificates")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create Control session jar: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client := &http.Client{Timeout: 15 * time.Second, Jar: jar, Transport: transport}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != parsed.Scheme || request.URL.Host != parsed.Host || len(via) >= 10 {
			return errors.New("rootless Compose Portal redirect left its trusted HTTPS origin")
		}
		return nil
	}
	loginPage, status, err := linuxV2RootlessComposeDo(client, http.MethodGet, baseURL+"/portal/login", nil, nil)
	if err != nil || status != http.StatusOK {
		return nil, errors.New("fetch Control operator login page")
	}
	csrf := rootlessComposeLoginCSRF.FindSubmatch(loginPage)
	if len(csrf) != 2 {
		return nil, errors.New("Control operator login page omitted its CSRF form token")
	}
	form := url.Values{"_csrf_token": {string(csrf[1])}, "operator_token": {operatorToken}}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/portal/login", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("create Control operator login request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Referer", baseURL+"/portal/login")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("authenticate to rootless Compose Control")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		return nil, errors.New("Control operator login did not establish a portal session")
	}
	pageCSRF := rootlessComposePageCSRF.FindSubmatch(body)
	if len(pageCSRF) != 2 {
		return nil, errors.New("authenticated Control page omitted its CSRF token")
	}
	portal := &linuxV2RootlessComposePortal{baseURL: baseURL, csrf: string(pageCSRF[1]), client: client}
	if _, status, err := portal.jsonRequest(http.MethodGet, "/portal/api/workspace", nil); err != nil || status != http.StatusOK {
		return nil, errors.New("authenticated Control portal workspace endpoint is unavailable")
	}
	return portal, nil
}

func newLinuxV2RootlessComposeTransitionProxy(releaseURL string) (*linuxV2RootlessComposeTransitionProxy, error) {
	parsed, err := url.Parse(releaseURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.Path != "/__test/release" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("transition release URL must end at /__test/release on an absolute HTTP(S) origin")
	}
	return &linuxV2RootlessComposeTransitionProxy{
		baseURL: strings.TrimSuffix(releaseURL, "/__test/release"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (proxy *linuxV2RootlessComposeTransitionProxy) arm(runtimeID, nonce string) (int, error) {
	return proxy.postJSON(proxy.baseURL+"/__test/arm", map[string]string{"runtime_id": runtimeID, "nonce": nonce})
}

func (proxy *linuxV2RootlessComposeTransitionProxy) release(request linuxV2RootlessComposeTransitionRelease) (int, error) {
	return proxy.postJSON(proxy.baseURL+"/__test/release", request)
}

func (proxy *linuxV2RootlessComposeTransitionProxy) postJSON(endpoint string, value any) (int, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, errors.New("encode transition-proxy fixture request")
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, errors.New("create transition-proxy fixture request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := proxy.client.Do(request)
	if err != nil {
		return 0, errors.New("send transition-proxy fixture request")
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return response.StatusCode, errors.Join(readErr, closeErr)
	}
	return response.StatusCode, nil
}

func (proxy *linuxV2RootlessComposeTransitionProxy) status(runtimeID, nonce string) (linuxV2RootlessComposeTransitionStatus, error) {
	query := url.Values{"runtime_id": {runtimeID}, "nonce": {nonce}}
	request, err := http.NewRequest(http.MethodGet, proxy.baseURL+"/__test/status?"+query.Encode(), nil)
	if err != nil {
		return linuxV2RootlessComposeTransitionStatus{}, errors.New("create transition-proxy status request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := proxy.client.Do(request)
	if err != nil {
		return linuxV2RootlessComposeTransitionStatus{}, errors.New("read transition-proxy status")
	}
	contents, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		return linuxV2RootlessComposeTransitionStatus{}, errors.New("transition-proxy status endpoint is unavailable")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		return linuxV2RootlessComposeTransitionStatus{}, errors.New("decode transition-proxy status")
	}
	for key := range fields {
		if strings.Contains(strings.ToLower(key), "lease") {
			return linuxV2RootlessComposeTransitionStatus{}, errors.New("transition-proxy status exposed lease material")
		}
	}
	var status linuxV2RootlessComposeTransitionStatus
	if err := json.Unmarshal(contents, &status); err != nil || status.RuntimeID != runtimeID {
		return linuxV2RootlessComposeTransitionStatus{}, errors.New("transition-proxy status changed its armed runtime identity")
	}
	return status, nil
}

func (proxy *linuxV2RootlessComposeTransitionProxy) waitForStatus(
	t *testing.T,
	runtimeID, nonce, expected string,
	timeout time.Duration,
) (linuxV2RootlessComposeTransitionStatus, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last linuxV2RootlessComposeTransitionStatus
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = proxy.status(runtimeID, nonce)
		if lastErr == nil && last.State == expected {
			return last, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last, fmt.Errorf("transition proxy did not reach state %q (last=%q error=%v)", expected, last.State, lastErr)
}

func linuxV2RootlessComposeReleaseForJournal(journal state.RunJournal, nonce string) linuxV2RootlessComposeTransitionRelease {
	return linuxV2RootlessComposeTransitionRelease{
		RunID: journal.RunID, Generation: journal.Generation,
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		ClaimID: journal.ClaimID, LeaseTokenSHA256: linuxWitnessHashBytes([]byte(journal.LeaseToken)), Nonce: nonce,
	}
}

func linuxV2RootlessComposeTransitionStatusMatches(status linuxV2RootlessComposeTransitionStatus, expected string, journal state.RunJournal) bool {
	return status.State == expected && status.RunID == journal.RunID && status.Generation == journal.Generation &&
		status.RuntimeID == journal.RuntimeID && status.RuntimeEpoch == journal.ClaimedRuntimeEpoch && status.ClaimID == journal.ClaimID
}

func linuxV2RootlessComposeDo(client *http.Client, method, endpoint string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	request, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	contents, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, response.StatusCode, errors.Join(readErr, closeErr)
	}
	return contents, response.StatusCode, nil
}

func (portal *linuxV2RootlessComposePortal) jsonRequest(method, path string, value any) ([]byte, int, error) {
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(encoded)
	}
	headers := map[string]string{"Accept": "application/json"}
	if value != nil {
		headers["Content-Type"] = "application/json"
	}
	if method != http.MethodGet {
		headers["x-csrf-token"] = portal.csrf
		headers["Origin"] = portal.baseURL
		headers["Referer"] = portal.baseURL + "/portal/"
	}
	return linuxV2RootlessComposeDo(portal.client, method, portal.baseURL+path, body, headers)
}

func (portal *linuxV2RootlessComposePortal) awaitRuntime(t *testing.T, harnessKind, profile, nativeVersion, runtimeKey string) linuxV2RootlessComposeRuntime {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		contents, status, err := portal.jsonRequest(http.MethodGet, "/portal/api/workspace", nil)
		if err == nil && status == http.StatusOK {
			var snapshot struct {
				RegisteredRuntimes []linuxV2RootlessComposeRuntime `json:"registered_runtimes"`
			}
			if json.Unmarshal(contents, &snapshot) == nil {
				var selected []linuxV2RootlessComposeRuntime
				for _, runtime := range snapshot.RegisteredRuntimes {
					if runtime.RuntimeKey == runtimeKey && runtime.AgentProfile == profile &&
						runtime.Workspace == "native" && runtime.Status == "online" {
						selected = append(selected, runtime)
					}
				}
				if len(selected) == 1 {
					if err := linuxV2ValidateRootlessComposeRuntime(selected[0], harnessKind, profile, nativeVersion); err != nil {
						t.Fatalf("Control reported mismatched %s runtime: %v", harnessKind, err)
					}
					return selected[0]
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("Control did not register online native runtime %q for %s profile=%q workspace=native", runtimeKey, harnessKind, profile)
	return linuxV2RootlessComposeRuntime{}
}

func (portal *linuxV2RootlessComposePortal) createAndLaunchWorkItem(t *testing.T, harnessKind, profile, suffix string) string {
	t.Helper()
	keyPrefix := map[string]string{"pi": "RPI", "opencode": "ROC"}[harnessKind]
	projectBody := map[string]string{
		"name":                  "Rootless native " + harnessKind + " " + suffix,
		"key":                   keyPrefix + strings.ToUpper(suffix[:5]),
		"description":           "Isolated non-Goal Linux native refusal witness.",
		"default_agent_profile": profile,
		"default_workspace":     "native",
	}
	contents, status, err := portal.jsonRequest(http.MethodPost, "/portal/api/projects", projectBody)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create isolated non-Goal %s project: status=%d error=%v", harnessKind, status, err)
	}
	var projectResponse struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(contents, &projectResponse); err != nil || projectResponse.Project.ID == "" {
		t.Fatalf("Control created no %s project identity: %v", harnessKind, err)
	}
	workItemBody := map[string]any{
		"title":         "Rootless Linux v2 " + harnessKind + " refusal " + suffix,
		"description":   "Run one ordinary non-Goal native WorkItem; Linux v2 admission must refuse it before native execution.",
		"status":        "ready",
		"priority":      "high",
		"assignee_type": "agent",
		"agent_profile": profile,
		"workspace":     "native",
	}
	contents, status, err = portal.jsonRequest(http.MethodPost,
		"/portal/api/projects/"+projectResponse.Project.ID+"/work-items", workItemBody)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create non-Goal %s WorkItem: status=%d error=%v", harnessKind, status, err)
	}
	var workItemResponse struct {
		WorkItem struct {
			ID string `json:"id"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal(contents, &workItemResponse); err != nil || workItemResponse.WorkItem.ID == "" {
		t.Fatalf("Control created no %s WorkItem identity: %v", harnessKind, err)
	}
	actionID, err := linuxV2RootlessComposeUUID()
	if err != nil {
		t.Fatalf("generate %s WorkItem launch idempotency key: %v", harnessKind, err)
	}
	_, status, err = portal.jsonRequest(http.MethodPost,
		"/portal/api/work-items/"+workItemResponse.WorkItem.ID+"/run", map[string]string{"action_id": actionID})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("launch non-Goal %s WorkItem: status=%d error=%v", harnessKind, status, err)
	}
	return workItemResponse.WorkItem.ID
}

func (portal *linuxV2RootlessComposePortal) awaitFailedWorkItem(
	t *testing.T,
	workItemID, harnessKind, runtimeID string,
	onRunKey func(string, int64),
) linuxV2RootlessComposeExecution {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	assignmentDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		contents, status, err := portal.jsonRequest(http.MethodGet, "/portal/api/work-items/"+workItemID, nil)
		if err == nil && status == http.StatusOK {
			var detail struct {
				WorkItem struct {
					ID        string                           `json:"id"`
					Execution *linuxV2RootlessComposeExecution `json:"execution"`
				} `json:"work_item"`
			}
			if json.Unmarshal(contents, &detail) == nil && detail.WorkItem.ID == workItemID && detail.WorkItem.Execution != nil {
				execution := *detail.WorkItem.Execution
				if execution.RunID != "" && execution.Generation > 0 {
					onRunKey(execution.RunID, execution.Generation)
				}
				if execution.RuntimeID != "" && execution.RuntimeID != runtimeID {
					t.Fatalf("Control assigned %s WorkItem to runtime %q, want exact native runtime %q", harnessKind, execution.RuntimeID, runtimeID)
				}
				if execution.RunID == "" && time.Now().After(assignmentDeadline) {
					t.Fatalf("missing prerequisite: Control did not assign the non-Goal %s WorkItem to its native runtime; it stayed queued, so no App Claim/RunJournal proof exists (standalone Probe is not acceptance)", harnessKind)
				}
				if execution.State == "failed" {
					if execution.TaskID == "" || execution.RunID == "" || execution.Generation <= 0 || execution.RuntimeID != runtimeID ||
						execution.Failure.Stage != "linux_v2_required" ||
						!strings.Contains(execution.Failure.Error, "exact symmetry.admission.v1 RunKey/fence") ||
						rootlessComposeResultPresent(execution.Result) {
						t.Fatalf("Control terminal %s Run is not the exact pre-native Linux v2 refusal: task=%q run=%q generation=%d runtime=%q stage=%q result_present=%t",
							harnessKind, execution.TaskID, execution.RunID, execution.Generation, execution.RuntimeID,
							execution.Failure.Stage, rootlessComposeResultPresent(execution.Result))
					}
					return execution
				}
				if execution.State == "completed" || execution.State == "cancelled" || execution.State == "expired" {
					t.Fatalf("rootless %s WorkItem reached unexpected terminal state %q", harnessKind, execution.State)
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("rootless %s WorkItem did not reach an App refusal; no Control terminal receipt may substitute for the exact failed Run", harnessKind)
	return linuxV2RootlessComposeExecution{}
}

func rootlessComposeResultPresent(result json.RawMessage) bool {
	trimmed := bytes.TrimSpace(result)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
}

func linuxV2RootlessComposeReadRunJournal(stateDirectory string, key state.RunKey) (state.RunJournal, bool, error) {
	if !filepath.IsAbs(stateDirectory) || filepath.Clean(stateDirectory) != stateDirectory {
		return state.RunJournal{}, false, errors.New("rootless daemon StateDir is not canonical and absolute")
	}
	stateInfo, err := os.Lstat(stateDirectory)
	if err != nil || !stateInfo.IsDir() || stateInfo.Mode()&os.ModeSymlink != 0 {
		return state.RunJournal{}, false, errors.New("rootless daemon StateDir is unavailable or not a directory")
	}
	stateStat, ok := stateInfo.Sys().(*syscall.Stat_t)
	if !ok || stateStat.Uid != uint32(os.Geteuid()) || stateStat.Gid != uint32(os.Getegid()) || stateStat.Mode&0o7777 != 0o700 {
		return state.RunJournal{}, false, errors.New("rootless daemon StateDir is not private to the unprivileged witness user")
	}
	runsDirectory := filepath.Join(stateDirectory, "runs")
	runsInfo, err := os.Lstat(runsDirectory)
	if err != nil || !runsInfo.IsDir() || runsInfo.Mode()&os.ModeSymlink != 0 {
		return state.RunJournal{}, false, errors.New("rootless daemon runs directory is unavailable")
	}
	runsStat, ok := runsInfo.Sys().(*syscall.Stat_t)
	if !ok || runsStat.Uid != uint32(os.Geteuid()) || runsStat.Gid != uint32(os.Getegid()) || runsStat.Mode&0o7777 != 0o700 {
		return state.RunJournal{}, false, errors.New("rootless daemon runs directory is not private to the unprivileged witness user")
	}
	files, err := readLinuxWitnessJournals(stateDirectory)
	if err != nil {
		return state.RunJournal{}, false, err
	}
	var result state.RunJournal
	found := false
	for _, file := range files {
		if file.Journal.Key() != key {
			continue
		}
		if found {
			return state.RunJournal{}, false, errors.New("rootless daemon exposed duplicate journals for one RunKey")
		}
		result, found = file.Journal, true
	}
	return result, found, nil
}

func rootlessComposeRequiredPath(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		t.Fatalf("%s must name a canonical absolute path", name)
	}
	return value
}

func linuxV2RootlessComposeSuffix() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(random[:])[:8]), nil
}

func linuxV2RootlessComposeUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func writeLinuxV2RootlessComposeEvidence(path string, evidence linuxV2RootlessComposeEvidence) error {
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return errors.New("encode redacted rootless Compose witness evidence")
	}
	for _, entry := range os.Environ() {
		key, secret, ok := strings.Cut(entry, "=")
		if !ok || secret == "" || !linuxV2IsCredentialEnvironmentName(key) {
			continue
		}
		if strings.Contains(string(encoded), secret) {
			return fmt.Errorf("rootless witness evidence contains a value from credential variable %s", key)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("create private redacted rootless Compose witness evidence")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return errors.New("secure redacted rootless Compose witness evidence")
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return errors.New("write redacted rootless Compose witness evidence")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.New("sync redacted rootless Compose witness evidence")
	}
	if err := file.Close(); err != nil {
		return errors.New("close redacted rootless Compose witness evidence")
	}
	return nil
}
