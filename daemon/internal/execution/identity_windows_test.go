//go:build windows

package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestStartExposesWindowsProcessCreationIdentity(t *testing.T) {
	process := startHelper(t, &recordingSink{}, "wait")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})

	pid, identity := process.ProcessDetails()
	if pid != process.PID {
		t.Fatalf("PID = %d, want %d", pid, process.PID)
	}
	if !strings.HasPrefix(identity, "windows:") {
		t.Fatalf("Identity = %q, want windows identity", identity)
	}
	current, err := platform.ProcessIdentity(pid)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	if identity != current {
		t.Fatalf("Identity = %q, current identity = %q", identity, current)
	}
}
