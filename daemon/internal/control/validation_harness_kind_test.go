package control

import (
	"fmt"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// Control's attach gate excludes generic, so the local pre-flight must admit exactly the six
// native wire kinds and refuse generic with the same text as an unknown kind.
func TestValidateGoalSessionAttachAcceptsEveryNativeHarnessKind(t *testing.T) {
	natives := 0
	for _, kind := range protocol.HarnessKinds() {
		if kind == protocol.HarnessKindGeneric {
			continue
		}
		natives++
		t.Run(string(kind), func(t *testing.T) {
			request := goalSessionRequest()
			request.HarnessKind = string(kind)
			if err := validateGoalSessionAttach(goalRunID, request); err != nil {
				t.Fatalf("goal_session_attach with harness_kind %q rejected: %v", kind, err)
			}
		})
	}
	if want := len(protocol.HarnessKinds()) - 1; natives != want {
		t.Fatalf("native attach vocabulary has %d kinds, want %d (generic is the only exclusion)", natives, want)
	}
	for _, kind := range []string{string(protocol.HarnessKindGeneric), "unknown-harness"} {
		request := goalSessionRequest()
		request.HarnessKind = kind
		err := validateGoalSessionAttach(goalRunID, request)
		if err == nil || err.Error() != fmt.Sprintf("harness_kind %q is invalid", kind) {
			t.Fatalf("goal_session_attach with harness_kind %q = %v, want harness_kind %q is invalid", kind, err, kind)
		}
	}
}
