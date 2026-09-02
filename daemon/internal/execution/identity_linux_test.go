//go:build linux

package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestStartExposesLinuxProcessCreationIdentity(t *testing.T) {
	process := startHelper(t, &recordingSink{}, "wait")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})

	if !strings.HasPrefix(process.Identity, "linux:") {
		t.Fatalf("Identity = %q, want linux identity", process.Identity)
	}
	current, err := platform.ProcessIdentity(process.PID)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	if process.Identity != current {
		t.Fatalf("Identity = %q, current identity = %q", process.Identity, current)
	}
}
