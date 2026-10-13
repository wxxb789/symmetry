//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"golang.org/x/sys/unix"
)

// The HTTP client is this test process. Its new retained pidfd exercises the
// bridge's v2 binding/verifier, not installed native-owner admission.
func bindPiProviderBridgeTestPeer(t *testing.T, lifecycle harness.ProviderBridgeLifecycle) {
	t.Helper()
	bridge, ok := lifecycle.(*pi.ProviderBridge)
	if !ok {
		t.Fatalf("provider bridge lifecycle = %T, want *pi.ProviderBridge", lifecycle)
	}
	pid := os.Getpid()
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatalf("open new test-owned provider peer pidfd: %v", err)
	}
	pidfd := os.NewFile(uintptr(fd), "app-provider-bridge-test-peer-pidfd")
	namespace, err := platform.CaptureLinuxUserNamespaceIdentity(pid, identity, pidfd)
	if err != nil {
		_ = pidfd.Close()
		t.Fatalf("capture test-owned provider peer namespace: %v", err)
	}
	peer := &execution.LinuxV2Peer{PID: pid, Identity: identity, UserNamespace: namespace, PIDFD: pidfd}
	if err := bridge.BindLinuxV2Process(peer); err != nil {
		_ = peer.Close()
		t.Fatalf("bind retained-v2 provider peer: %v", err)
	}
	t.Cleanup(func() {
		if err := bridge.Close(context.Background()); err != nil {
			t.Errorf("close test-owned retained-v2 provider bridge: %v", err)
		}
		if peer.PIDFD != nil {
			t.Error("provider bridge retained its test-owned peer pidfd after Close")
			_ = peer.Close()
		}
		if _, err := pidfd.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("test-owned provider peer pidfd Stat() error = %v, want closed descriptor", err)
		}
	})
}
