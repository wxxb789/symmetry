package control

import "testing"

// Every harness kind the daemon can be configured with must survive goal
// session attach validation; a kind that is missing here fails locally before
// any Control request is made.
func TestValidateGoalSessionAttachAcceptsEveryConfiguredHarnessKind(t *testing.T) {
	for _, kind := range []string{"codex", "claude_code", "pi", "omp", "opencode"} {
		t.Run(kind, func(t *testing.T) {
			request := goalSessionRequest()
			request.HarnessKind = kind
			if err := validateGoalSessionAttach(goalRunID, request); err != nil {
				t.Fatalf("goal_session_attach with harness_kind %q rejected: %v", kind, err)
			}
		})
	}
	request := goalSessionRequest()
	request.HarnessKind = "unknown-harness"
	if err := validateGoalSessionAttach(goalRunID, request); err == nil {
		t.Fatal("goal_session_attach accepted an unknown harness_kind")
	}
}
