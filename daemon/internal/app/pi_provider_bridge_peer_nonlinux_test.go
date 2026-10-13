//go:build !linux

package app

import (
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func bindPiProviderBridgeTestPeer(t *testing.T, lifecycle harness.ProviderBridgeLifecycle) {
	t.Helper()
	identity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BindProcess(os.Getpid(), identity); err != nil {
		t.Fatal(err)
	}
}
