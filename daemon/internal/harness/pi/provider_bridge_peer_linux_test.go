//go:build linux

package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	providerBridgePeerChildEnv = "SYMMETRY_PI_PROVIDER_BRIDGE_PEER_CHILD"
	providerBridgePeerDenyEnv  = "SYMMETRY_PI_PROVIDER_BRIDGE_PEER_DENY"
	linuxV2PeerHostGateEnv     = "SYMMETRY_LINUX_V2_ADAPTER_PEER_HOST_GATE"
)

func requireLinuxV2PeerHostGate(t *testing.T) {
	t.Helper()
	if os.Getenv(linuxV2PeerHostGateEnv) != "1" {
		t.Skip("requires the explicitly qualified Linux v2 peer inspection host gate")
	}
}

type providerBridgePeerRequest struct {
	URL   string `json:"url"`
	Nonce string `json:"nonce"`
	Body  string `json:"body"`
}

// The child exercises the bridge's OS-level peer verifier. Its test-owned pidfd
// is not an App-owned Linux v2 capability and does not establish production
// owner acceptance.
func TestProviderBridgeKernelPeerClientProcess(t *testing.T) {
	if os.Getenv(providerBridgePeerChildEnv) != "1" {
		return
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		t.Fatalf("read bridge request: %v", err)
	}
	var request providerBridgePeerRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatalf("decode bridge request: %v", err)
	}
	wantStatus := http.StatusOK
	if os.Getenv(providerBridgePeerDenyEnv) == "1" {
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
			t.Fatalf("disable peer dumpability: %v", err)
		}
		wantStatus = http.StatusForbidden
	}
	httpRequest, err := http.NewRequest(http.MethodPost, request.URL, strings.NewReader(request.Body))
	if err != nil {
		t.Fatalf("create bridge request: %v", err)
	}
	httpRequest.Header.Set(providerBridgeNonceHeader, request.Nonce)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(httpRequest)
	if err != nil {
		t.Fatalf("send bridge request: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != wantStatus {
		t.Fatalf("bridge response status = %d, want %d", response.StatusCode, wantStatus)
	}
}

func TestProviderBridgeCloseReleasesBoundPeerFD(t *testing.T) {
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "run-kernel-peer-close",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			t.Fatal("peer-close test unexpectedly reached provider execution")
			return ProviderBridgeResponse{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	process := startProviderBridgeKernelPeer(t, false)
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	peerClone := duplicateProviderBridgeKernelPeer(t, process.peer)
	peerTransferred := false
	t.Cleanup(func() {
		if !peerTransferred {
			_ = peerClone.Close()
		}
	})
	if err := bridge.BindLinuxV2Process(peerClone); err != nil {
		t.Fatal(err)
	}
	peerTransferred = true
	ownedPIDFD, sourcePIDFD := peerClone.PIDFD, process.peer.PIDFD
	if process.peer.PIDFD == nil || peerClone.PIDFD == nil {
		t.Fatal("binding unexpectedly closed the peer pidfd before Bridge.Close")
	}
	if err := bridge.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if peerClone.PIDFD != nil {
		t.Fatal("bridge Close did not release its owned pidfd")
	}
	if _, err := ownedPIDFD.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("bridge-owned pidfd stat error = %v, want closed descriptor", err)
	}
	if process.peer.PIDFD == nil || sourcePIDFD == nil {
		t.Fatal("bridge Close released the peer owner's separate retained pidfd")
	}
	if _, err := sourcePIDFD.Stat(); err != nil {
		t.Fatalf("source peer pidfd was closed with bridge clone: %v", err)
	}
}

func TestProviderBridgeV1BindingIsUnsupportedOnLinux(t *testing.T) {
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "run-v1-rejected",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			t.Fatal("unsupported v1 binding reached provider execution")
			return ProviderBridgeResponse{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	if err := bridge.BindProcess(os.Getpid(), "legacy"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("BindProcess() error = %v, want explicit unsupported Linux v2 requirement", err)
	}
}

func TestLinuxAdapterRejectsLegacyProcessStubForProviderBinding(t *testing.T) {
	lifecycle := &fakeProviderBridgeLifecycle{}
	err := bindProviderBridgeProcess(newFakeNativeProcess(), lifecycle)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("bindProviderBridgeProcess() error = %v, want explicit unsupported for a process without the v2 owner", err)
	}
	_, _, bindCalls, _ := lifecycle.snapshot()
	if bindCalls != 0 {
		t.Fatalf("legacy BindProcess calls = %d, want 0 for a Linux native run", bindCalls)
	}
}

// This proves bridge fail-closed behavior for a test-owned nondumpable kernel
// peer; the 403 does not identify which kernel inspection stage denied it.
func TestProviderBridgeQualifiedNonDumpablePeerFailsClosedBeforeProviderAction(t *testing.T) {
	requireLinuxV2PeerHostGate(t)
	var executed atomic.Int32
	bridge, err := NewProviderBridge(ProviderBridgeOptions{
		RunID:               "run-v2-permission-denied",
		Grants:              testProviderGrants(),
		RequirePeerIdentity: true,
		Execute: func(context.Context, string, ProviderBridgeRequest) (ProviderBridgeResponse, error) {
			executed.Add(1)
			return ProviderBridgeResponse{Outcome: ProviderBridgeOutcomeSucceeded, Result: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := bridge.Start(context.Background())
	if err != nil {
		_ = bridge.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	process := startProviderBridgeKernelPeer(t, true)
	peerClone := duplicateProviderBridgeKernelPeer(t, process.peer)
	ownedPIDFD, sourcePIDFD := peerClone.PIDFD, process.peer.PIDFD
	peerTransferred := false
	t.Cleanup(func() {
		if !peerTransferred {
			_ = peerClone.Close()
		}
	})
	if err := bridge.BindLinuxV2Process(peerClone); err != nil {
		t.Fatal(err)
	}
	peerTransferred = true
	body := `{"resource_id":"11111111-1111-4111-8111-111111111111","operation":"change.upsert","action_key":"peer-v2-denied","input":{"title":"denied"}}`
	request, _ := json.Marshal(providerBridgePeerRequest{URL: endpoint.URL, Nonce: endpoint.Nonce, Body: body})
	if _, err := process.input.Write(append(request, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := process.wait(); err != nil {
		t.Fatalf("permission-denied peer child failed: %v; stdout=%q; stderr=%q", err, process.stdout.String(), process.stderr.String())
	}
	if executed.Load() != 0 {
		t.Fatalf("provider action executed %d times after non-dumpable peer refusal", executed.Load())
	}
	if err := bridge.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if peerClone.PIDFD != nil {
		t.Fatal("bridge Close did not release the permission-denied peer clone")
	}
	if _, err := ownedPIDFD.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("permission-denied bridge pidfd stat error = %v, want closed descriptor", err)
	}
	if process.peer.PIDFD == nil || sourcePIDFD == nil {
		t.Fatal("bridge Close released the peer owner's separate retained pidfd")
	}
	if _, err := sourcePIDFD.Stat(); err != nil {
		t.Fatalf("source peer pidfd was closed with bridge clone: %v", err)
	}
}

func duplicateProviderBridgeKernelPeer(t *testing.T, peer *execution.LinuxV2Peer) *execution.LinuxV2Peer {
	t.Helper()
	fd, err := unix.FcntlInt(peer.PIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate test-owned peer pidfd: %v", err)
	}
	clone := *peer
	clone.PIDFD = os.NewFile(uintptr(fd), "provider-bridge-kernel-peer-clone")
	return &clone
}

type providerBridgeKernelPeerProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	peer    *execution.LinuxV2Peer
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	waited  bool
}

func startProviderBridgeKernelPeer(t *testing.T, deny bool) *providerBridgeKernelPeerProcess {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProviderBridgeKernelPeerClientProcess$")
	environment := []string{}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, providerBridgePeerChildEnv+"=") && !strings.HasPrefix(item, providerBridgePeerDenyEnv+"=") {
			environment = append(environment, item)
		}
	}
	environment = append(environment, providerBridgePeerChildEnv+"=1")
	if deny {
		environment = append(environment, providerBridgePeerDenyEnv+"=1")
	}
	command.Env = environment
	process := &providerBridgeKernelPeerProcess{command: command}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.input = input
	command.Stdout = &process.stdout
	command.Stderr = &process.stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	t.Cleanup(process.close)
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		process.close()
		t.Fatalf("open real child pidfd: %v", err)
	}
	process.peer = captureProviderBridgeKernelPeer(t, command.Process.Pid, pidfd)
	return process
}

func captureProviderBridgeKernelPeer(t *testing.T, pid, pidfd int) *execution.LinuxV2Peer {
	t.Helper()
	pidfdFile := os.NewFile(uintptr(pidfd), "provider-bridge-kernel-peer-pidfd")
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		_ = pidfdFile.Close()
		t.Fatalf("capture real peer process identity: %v", err)
	}
	namespace, err := platform.CaptureLinuxUserNamespaceIdentity(pid, identity, pidfdFile)
	if err != nil {
		_ = pidfdFile.Close()
		t.Fatalf("capture real peer user namespace: %v", err)
	}
	return &execution.LinuxV2Peer{PID: pid, Identity: identity, UserNamespace: namespace, PIDFD: pidfdFile}
}

func (process *providerBridgeKernelPeerProcess) wait() error {
	if process.waited {
		return nil
	}
	process.waited = true
	_ = process.input.Close()
	return process.command.Wait()
}

func (process *providerBridgeKernelPeerProcess) close() {
	if process == nil {
		return
	}
	if !process.waited {
		_ = process.command.Process.Kill()
		process.waited = true
		_ = process.command.Wait()
	}
	if process.peer != nil {
		_ = process.peer.Close()
	}
}
